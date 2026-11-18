package checkpoint

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func setup(t *testing.T) (dir string, git func(...string) string) {
	t.Helper()
	dir = t.TempDir()
	git = func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("v1\n"), 0o644)
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored/\n"), 0o644)
	git("add", ".")
	git("commit", "-q", "-m", "init")
	return dir, git
}

func read(t *testing.T, path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}

func TestUndoRedoUncommittedTurn(t *testing.T) {
	dir, git := setup(t)
	os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("mine\n"), 0o644)
	git("add", "staged.txt") // the user's own staged work
	cachedBefore := git("diff", "--cached", "--name-only")

	ref, err := Create(dir, "%api")
	if err != nil || ref == "" {
		t.Fatalf("Create = %q %v", ref, err)
	}
	if git("diff", "--cached", "--name-only") != cachedBefore {
		t.Fatal("checkpointing must not touch the user's index")
	}
	if again, _ := Create(dir, "%api"); again != "" {
		t.Error("nothing changed: no new checkpoint")
	}

	// The agent's turn: edits a file, adds one, touches an ignored dir.
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("v2 broken\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("junk\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "ignored"), 0o755)
	os.WriteFile(filepath.Join(dir, "ignored", "cache"), []byte("x"), 0o644)

	backup, err := Restore(dir, ref)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(dir, "a.txt")) != "v1\n" {
		t.Error("a.txt must be back to v1")
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(err) {
		t.Error("a file created during the turn must be removed")
	}
	if read(t, filepath.Join(dir, "staged.txt")) != "mine\n" {
		t.Error("the user's file from before the turn must stay")
	}
	if _, err := os.Stat(filepath.Join(dir, "ignored", "cache")); err != nil {
		t.Error("ignored files are left alone")
	}

	if _, err := Restore(dir, backup); err != nil { // redo
		t.Fatal(err)
	}
	if read(t, filepath.Join(dir, "a.txt")) != "v2 broken\n" || read(t, filepath.Join(dir, "new.txt")) != "junk\n" {
		t.Error("restoring the backup must redo the turn")
	}
}

func TestUndoMovesBranchBackAfterAgentCommit(t *testing.T) {
	dir, git := setup(t)
	base := git("rev-parse", "HEAD")
	ref, _ := Create(dir, "api")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("v2\n"), 0o644)
	git("commit", "-q", "-am", "agent commit")
	if _, err := Restore(dir, ref); err != nil {
		t.Fatal(err)
	}
	if head := git("rev-parse", "HEAD"); head != base {
		t.Errorf("HEAD = %s, want the pre-turn %s", head, base)
	}
	if read(t, filepath.Join(dir, "a.txt")) != "v1\n" {
		t.Error("a.txt must be back to v1")
	}
}

func TestKeepAndList(t *testing.T) {
	dir, _ := setup(t)
	for i := 0; i < Keep+3; i++ {
		os.WriteFile(filepath.Join(dir, "a.txt"), []byte{byte('a' + i)}, 0o644)
		if _, err := Create(dir, "api"); err != nil {
			t.Fatal(err)
		}
	}
	list, _ := List(dir, "api")
	if len(list) != Keep || !list[0].Time.After(list[1].Time) {
		t.Errorf("List = %d items, want %d newest first", len(list), Keep)
	}
	if _, err := Create(t.TempDir(), "x"); err != ErrNotRepo {
		t.Errorf("outside a repo: %v", err)
	}
}
