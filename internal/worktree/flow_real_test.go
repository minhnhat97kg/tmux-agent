package worktree

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"tmux-agent/internal/tmux"
)

// setupTempGitRepo builds a TEMPORARY git repo (t.TempDir(), auto-cleaned
// when the test finishes) with an initial commit — "git worktree add -b"
// needs a valid HEAD. Tests in this file NEVER touch the project's real
// repo.
func setupTempGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitOrFail := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		if err := cmd.Run(); err != nil {
			t.Fatalf("git %v error: %v\nstderr: %s", args, err, errb.String())
		}
	}
	runGitOrFail("init", "-q")
	runGitOrFail("config", "user.email", "test@example.com")
	runGitOrFail("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("writing README.md failed: %v", err)
	}
	runGitOrFail("add", "README.md")
	runGitOrFail("commit", "-q", "-m", "init")
	return dir
}

// TestSpawnThenRemoveWithRealGitAndFakeTmux is an integration test: RealEnv
// runs real git against a temp repo the test itself creates, while tmux is
// replaced with tmux.NewFake() (a fake Runner) — no real tmux server is ever
// touched. This test proves RealEnv wires up correctly to git + tmux,
// complementing the spawnWith/removeWith core tests in flow_test.go (which
// already use a fake SpawnEnv to cover error/rollback branches that can't be
// reliably forced with real git).
func TestSpawnThenRemoveWithRealGitAndFakeTmux(t *testing.T) {
	repo := setupTempGitRepo(t)

	slug := "do-task"
	worktreePath := filepath.Join(repo, DefaultWorktreeDir, slug)
	branch := DefaultBranchPrefix + slug
	paneID, windowID := "%1", "@1"

	fake := tmux.NewFake()
	fake.Set(paneID+" "+windowID,
		"new-window", "-d", "-t", "sess:", "-c", worktreePath, "-n", slug,
		"-P", "-F", "#{pane_id} #{window_id}")
	fake.Set("", "set", "-w", "-t", windowID, tmux.SpawnedOption, "1")
	fake.Set("", "set", "-w", "-t", windowID, tmux.SpawnedFromOption, repo)
	fake.Set("", "set", "-w", "-t", windowID, tmux.SpawnedWorktreeOption, worktreePath)
	fake.Set("", "set", "-w", "-t", windowID, tmux.SpawnedBranchOption, branch)
	fake.Set("", "send-keys", "-t", paneID, "-l", "claude")
	fake.Set("", "send-keys", "-t", paneID, "Enter")
	restore := tmux.SetRunner(fake)
	defer restore()

	req := &SpawnRequest{
		RepoRoot: repo,
		TaskName: "do task",
		Session:  "sess",
		Agent:    "claude",
		Mode:     "default",
	}
	gotBranch, err := Spawn(req)
	if err != nil {
		t.Fatalf("Spawn error: %v", err)
	}
	if gotBranch != branch {
		t.Fatalf("branch = %q, want %q", gotBranch, branch)
	}
	if _, err := os.Stat(worktreePath); err != nil {
		t.Fatalf("worktree must exist on disk after spawn: %v", err)
	}
	if !branchExists(repo, branch) {
		t.Fatalf("branch %q must exist in the repo after spawn", branch)
	}

	// The marker the remove flow will read back via display-message —
	// mirroring exactly what Spawn just set at the window level.
	markerRaw := strings.Join([]string{"1", repo, worktreePath, branch, windowID}, "\n")
	fake.Set(markerRaw, "display-message", "-t", paneID, "-p", SpawnMarkersTemplate())
	fake.Set("", "kill-window", "-t", windowID)

	if err := Remove(paneID, RemoveWindowAndWorktree); err != nil {
		t.Fatalf("Remove error: %v", err)
	}
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Fatalf("worktree must be removed from disk after remove, stat err = %v", err)
	}
	if branchExists(repo, branch) {
		t.Fatalf("branch %q must be deleted after remove", branch)
	}

	foundKill := false
	for _, c := range fake.Calls() {
		if len(c) > 0 && c[0] == "kill-window" {
			foundKill = true
		}
	}
	if !foundKill {
		t.Fatal("missing kill-window call in fake tmux")
	}
}

// TestRemoveRefusesWorktreeCreatedByHandWithoutMarker is the most important
// check of the remove flow: a git worktree the user created by hand (not
// through spawn, no marker) must be REFUSED for removal — recognized only
// via the marker, never guessed from directory/branch name.
func TestRemoveRefusesWorktreeCreatedByHandWithoutMarker(t *testing.T) {
	repo := setupTempGitRepo(t)

	// The user creates a worktree by hand, using the same naming convention
	// as spawn but NOT going through Spawn — so there's no marker at the
	// window level.
	manualPath := filepath.Join(repo, DefaultWorktreeDir, "manual")
	if _, err := runGit(repo, "worktree", "add", manualPath, "-b", "agent/manual"); err != nil {
		t.Fatalf("preparing the manual worktree failed: %v", err)
	}

	fake := tmux.NewFake()
	// display-message returns empty markers — exactly what real tmux
	// returns for a pane/window that never had spawn set any options.
	fake.Set("\n\n\n\n", "display-message", "-t", "%9", "-p", SpawnMarkersTemplate())
	restore := tmux.SetRunner(fake)
	defer restore()

	err := Remove("%9", RemoveWindowAndWorktree)
	if err == nil {
		t.Fatal("Remove MUST refuse a worktree/pane without a spawn marker, but it succeeded")
	}
	if !strings.Contains(err.Error(), "not created by sidebar spawn") {
		t.Fatalf("err = %v, want a clear refusal message", err)
	}
	// The manually created worktree must be left untouched — the remove
	// flow must not touch it.
	if _, statErr := os.Stat(manualPath); statErr != nil {
		t.Fatalf("the manually created worktree must remain intact after the refused removal: %v", statErr)
	}
	if !branchExists(repo, "agent/manual") {
		t.Fatal("the manually created branch must remain intact after the refused removal")
	}
	for _, c := range fake.Calls() {
		if len(c) > 0 && (c[0] == "kill-window") {
			t.Fatalf("kill-window must not be called when Remove refuses: %v", fake.Calls())
		}
	}
}
