package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"tmux-agent/internal/tmux"
)

func TestWorktreeDiffMergeDiscard(t *testing.T) {
	defer tmux.SetRunner(tmux.NewFake())()
	repo := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	run(repo, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644)
	run(repo, "add", ".")
	run(repo, "commit", "-q", "-m", "init")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@t")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@t")
	t.Chdir(repo)

	_, branch, path, err := addWorktree(repo, "t1")
	if err != nil {
		t.Fatal(err)
	}
	if branch != "agent/t1" {
		t.Errorf("branch = %s", branch)
	}
	os.WriteFile(filepath.Join(path, "b.txt"), []byte("b\n"), 0o644)

	var out, errb bytes.Buffer
	if cmdDiff([]string{"t1", "--stat"}, &out, &errb) != 0 || !strings.Contains(out.String(), "b.txt") {
		t.Fatalf("diff: %s %s", out.String(), errb.String())
	}
	if st, _ := git(repo, "status", "--porcelain"); st != "" {
		t.Errorf(".worktrees must not dirty the main checkout: %q", st)
	}
	out.Reset()
	if cmdMerge([]string{"t1", "--discard"}, &out, &errb) != 0 {
		t.Fatalf("merge: %s", errb.String())
	}
	if _, err := os.Stat(filepath.Join(repo, "b.txt")); err != nil {
		t.Error("the agent's file must be merged into main")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("--discard must remove the worktree")
	}
	if b, _ := git(repo, "branch", "--list", "agent/t1"); b != "" {
		t.Error("--discard must delete the branch")
	}
}
