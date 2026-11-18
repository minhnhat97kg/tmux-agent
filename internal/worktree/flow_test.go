package worktree

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"tmux-agent/internal/tmux"
)

// fakeSpawnEnv is a fake SpawnEnv for testing the spawnWith/removeWith core:
// it never touches real git or tmux, just logs calls and lets each step's
// failure scenario be scripted — ported from FakeEnv in flow.rs
// (mod env_tests).
type fakeSpawnEnv struct {
	calls []string

	worktreeDir string

	// failSetOptionAt = -1 means never fail (equivalent to Rust's None).
	// >=0 is the index (0-based) of the SetWindowOption call that fails.
	failSetOptionAt    int
	failKillWindow     bool
	failWorktreeRemove bool
	failBranchDelete   bool
	failSendCommand    bool
	failNewWindow      bool

	displayOutput string

	// worktreePathAlreadyGone/branchAlreadyGone simulate a previous partial
	// run that already cleaned up part of the state — the remove flow must
	// skip the corresponding git step to converge on a re-run.
	worktreePathAlreadyGone bool
	branchAlreadyGone       bool

	setOptionCalls int
}

func newFakeSpawnEnv() *fakeSpawnEnv {
	return &fakeSpawnEnv{failSetOptionAt: -1}
}

func (f *fakeSpawnEnv) log(s string) { f.calls = append(f.calls, s) }

func (f *fakeSpawnEnv) BranchPrefix() string          { return "" }
func (f *fakeSpawnEnv) WorktreeDir() string           { return f.worktreeDir }
func (f *fakeSpawnEnv) BranchIsFree(_, _ string) bool { return true }
func (f *fakeSpawnEnv) BranchExists(_, _ string) bool {
	return !f.branchAlreadyGone
}
func (f *fakeSpawnEnv) WorktreePathIsFree(_ string) bool { return true }
func (f *fakeSpawnEnv) WorktreePathExists(_ string) bool {
	return !f.worktreePathAlreadyGone
}

func (f *fakeSpawnEnv) WorktreeAdd(repo, path, branch string) error {
	f.log(fmt.Sprintf("worktree_add(%s,%s,%s)", repo, path, branch))
	return nil
}

func (f *fakeSpawnEnv) WorktreeRemove(repo, path string) error {
	f.log(fmt.Sprintf("worktree_remove(%s,%s)", repo, path))
	if f.failWorktreeRemove {
		return errors.New("worktree_remove failed")
	}
	return nil
}

func (f *fakeSpawnEnv) BranchDelete(repo, branch string) error {
	f.log(fmt.Sprintf("branch_delete(%s,%s)", repo, branch))
	if f.failBranchDelete {
		return errors.New("branch_delete failed")
	}
	return nil
}

func (f *fakeSpawnEnv) NewWindow(session, cwd, name string) (string, string, error) {
	f.log(fmt.Sprintf("new_window(%s,%s,%s)", session, cwd, name))
	if f.failNewWindow {
		return "", "", errors.New("new_window failed")
	}
	return "%1", "@1", nil
}

func (f *fakeSpawnEnv) KillWindow(windowID string) error {
	f.log(fmt.Sprintf("kill_window(%s)", windowID))
	if f.failKillWindow {
		return errors.New("kill_window failed")
	}
	return nil
}

func (f *fakeSpawnEnv) SetWindowOption(window, key, _ string) error {
	idx := f.setOptionCalls
	f.setOptionCalls++
	f.log(fmt.Sprintf("set_window_option(%s,%s)", window, key))
	if idx == f.failSetOptionAt {
		return fmt.Errorf("set %s failed", key)
	}
	return nil
}

func (f *fakeSpawnEnv) SendCommand(target, command string) error {
	f.log(fmt.Sprintf("send_command(%s,%s)", target, command))
	if f.failSendCommand {
		return errors.New("send_command failed")
	}
	return nil
}

func (f *fakeSpawnEnv) DisplayMessage(_, _ string) string {
	if f.displayOutput != "" {
		return f.displayOutput
	}
	return "1\n/r\n/r/.worktrees/task\nagent/task\n@1"
}

func sampleReq() *SpawnRequest {
	return &SpawnRequest{
		RepoRoot: "/r",
		TaskName: "task",
		Session:  "sess",
		Agent:    "claude",
		Mode:     "default",
	}
}

func hasCall(calls []string, prefix string) bool {
	for _, c := range calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func TestSpawnHappyPathSetsAllMarkersThenSendsCommand(t *testing.T) {
	env := newFakeSpawnEnv()
	branch, err := spawnWith(env, sampleReq())
	if err != nil {
		t.Fatalf("spawnWith error: %v", err)
	}
	if branch != "agent/task" {
		t.Errorf("branch = %q, want agent/task", branch)
	}
	if !hasCall(env.calls, "worktree_add(/r,/r/.worktrees/task,agent/task)") {
		t.Errorf("missing worktree_add with correct args: %v", env.calls)
	}
	if !hasCall(env.calls, "new_window(sess,/r/.worktrees/task,task)") {
		t.Errorf("missing new_window with correct args: %v", env.calls)
	}
	if env.setOptionCalls != 4 {
		t.Errorf("setOptionCalls = %d, want 4", env.setOptionCalls)
	}
	if !hasCall(env.calls, "send_command(%1,claude") {
		t.Errorf("missing send_command running the agent: %v", env.calls)
	}
	if hasCall(env.calls, "kill_window(") {
		t.Errorf("happy path must not roll back: %v", env.calls)
	}
}

func TestSpawnUsesCustomRepoRelativeWorktreeDir(t *testing.T) {
	env := newFakeSpawnEnv()
	env.worktreeDir = ".worktrees"
	if _, err := spawnWith(env, sampleReq()); err != nil {
		t.Fatalf("spawnWith error: %v", err)
	}
	if !hasCall(env.calls, "worktree_add(/r,/r/.worktrees/task,agent/task)") {
		t.Errorf("calls = %v", env.calls)
	}
	if !hasCall(env.calls, "new_window(sess,/r/.worktrees/task,task)") {
		t.Errorf("calls = %v", env.calls)
	}
}

func TestSpawnRejectsAbsoluteWorktreeDir(t *testing.T) {
	env := newFakeSpawnEnv()
	env.worktreeDir = "/tmp/worktrees"
	_, err := spawnWith(env, sampleReq())
	if err == nil || !strings.Contains(err.Error(), "must be inside the repo") {
		t.Fatalf("err = %v, want a message about worktree needing to be repo-relative", err)
	}
	if hasCall(env.calls, "worktree_add(") {
		t.Error("must not create a worktree when worktreeDir is absolute")
	}
}

func TestSpawnRejectsParentRelativeWorktreeDir(t *testing.T) {
	env := newFakeSpawnEnv()
	env.worktreeDir = "../worktrees"
	_, err := spawnWith(env, sampleReq())
	if err == nil || !strings.Contains(err.Error(), "must be inside the repo") {
		t.Fatalf("err = %v, want a message about worktree needing to be repo-relative", err)
	}
	if hasCall(env.calls, "worktree_add(") {
		t.Error("must not create a worktree outside the repo")
	}
}

func TestSpawnRollsBackWhenFirstMarkerFails(t *testing.T) {
	env := newFakeSpawnEnv()
	env.failSetOptionAt = 0
	_, err := spawnWith(env, sampleReq())
	if err == nil || !strings.Contains(err.Error(), tmux.SpawnedOption) {
		t.Fatalf("err = %v, want it to mention %s", err, tmux.SpawnedOption)
	}
	if !hasCall(env.calls, "kill_window(@1)") {
		t.Errorf("missing rollback kill_window: %v", env.calls)
	}
	if !hasCall(env.calls, "worktree_remove(") {
		t.Errorf("missing rollback worktree_remove: %v", env.calls)
	}
	if hasCall(env.calls, "send_command(") {
		t.Errorf("send_command must not run after a marker fails: %v", env.calls)
	}
}

func TestSpawnRollsBackWhenMiddleMarkerFails(t *testing.T) {
	env := newFakeSpawnEnv()
	env.failSetOptionAt = 1
	_, err := spawnWith(env, sampleReq())
	if err == nil || !strings.Contains(err.Error(), tmux.SpawnedFromOption) {
		t.Fatalf("err = %v, want it to mention %s", err, tmux.SpawnedFromOption)
	}
	if !hasCall(env.calls, "kill_window(@1)") || !hasCall(env.calls, "worktree_remove(") {
		t.Errorf("missing rollback: %v", env.calls)
	}
	if hasCall(env.calls, "send_command(") {
		t.Errorf("send_command must not run: %v", env.calls)
	}
}

func TestSpawnRollsBackWhenSendCommandFails(t *testing.T) {
	env := newFakeSpawnEnv()
	env.failSendCommand = true
	_, err := spawnWith(env, sampleReq())
	if err == nil || !strings.Contains(err.Error(), "send_command failed") {
		t.Fatalf("err = %v, want the underlying send_command failed error", err)
	}
	if !hasCall(env.calls, "kill_window(@1)") {
		t.Error("missing rollback kill_window")
	}
	if !hasCall(env.calls, "worktree_remove(") {
		t.Error("missing rollback worktree_remove")
	}
	if !hasCall(env.calls, "branch_delete(/r,agent/task)") {
		t.Error("missing rollback branch_delete")
	}
}

func TestSpawnRollsBackBranchWhenNewWindowFails(t *testing.T) {
	env := newFakeSpawnEnv()
	env.failNewWindow = true
	_, err := spawnWith(env, sampleReq())
	if err == nil || !strings.Contains(err.Error(), "new_window failed") {
		t.Fatalf("err = %v", err)
	}
	if hasCall(env.calls, "kill_window(") {
		t.Error("no window was ever created, kill_window must not run")
	}
	if !hasCall(env.calls, "worktree_remove(") {
		t.Error("missing rollback worktree_remove")
	}
	if !hasCall(env.calls, "branch_delete(/r,agent/task)") {
		t.Error("missing rollback branch_delete")
	}
}

func TestSpawnSurfacesRollbackFailureWhenKillWindowAlsoFails(t *testing.T) {
	env := newFakeSpawnEnv()
	env.failSetOptionAt = 0
	env.failKillWindow = true
	_, err := spawnWith(env, sampleReq())
	if err == nil || !strings.Contains(err.Error(), "rollback incomplete") {
		t.Fatalf("err = %v, want it to report rollback incomplete", err)
	}
	if !strings.Contains(err.Error(), "kill_window") {
		t.Errorf("err = %v, want it to mention kill_window", err)
	}
}

func TestSpawnSurfacesRollbackFailureWhenWorktreeRemoveAlsoFails(t *testing.T) {
	env := newFakeSpawnEnv()
	env.failSendCommand = true
	env.failWorktreeRemove = true
	_, err := spawnWith(env, sampleReq())
	if err == nil || !strings.Contains(err.Error(), "send_command failed") {
		t.Fatalf("err = %v, want the underlying send_command failed error to still be present", err)
	}
	if !strings.Contains(err.Error(), "rollback incomplete") || !strings.Contains(err.Error(), "worktree_remove") {
		t.Errorf("err = %v, want it to report rollback incomplete mentioning worktree_remove", err)
	}
}

// ─── removeWith ────────────────────────────────────────────────────

func TestRemoveGitStepsRunBeforeKillWindowThenBranchThenWindow(t *testing.T) {
	env := newFakeSpawnEnv()
	if err := removeWith(env, "%1", RemoveWindowAndWorktree); err != nil {
		t.Fatalf("removeWith error: %v", err)
	}
	idx := func(prefix string) int {
		for i, c := range env.calls {
			if strings.HasPrefix(c, prefix) {
				return i
			}
		}
		t.Fatalf("missing call %s in %v", prefix, env.calls)
		return -1
	}
	wt, br, kill := idx("worktree_remove("), idx("branch_delete("), idx("kill_window(")
	if !(wt < br && br < kill) {
		t.Errorf("wrong order, want worktree_remove -> branch_delete -> kill_window: %v", env.calls)
	}
	if !hasCall(env.calls, "branch_delete(/r,agent/task)") {
		t.Error("missing branch_delete with correct args")
	}
}

func TestRemoveDoesNotKillWindowWhenWorktreeRemoveFails(t *testing.T) {
	env := newFakeSpawnEnv()
	env.failWorktreeRemove = true
	err := removeWith(env, "%1", RemoveWindowAndWorktree)
	if err == nil || !strings.Contains(err.Error(), "worktree_remove failed") {
		t.Fatalf("err = %v", err)
	}
	if hasCall(env.calls, "kill_window(") {
		t.Errorf("kill_window must not run when git cleanup fails — the window is the only retry handle: %v", env.calls)
	}
}

func TestRemoveSkipsGitStepWhenWorktreeAlreadyGone(t *testing.T) {
	env := newFakeSpawnEnv()
	env.worktreePathAlreadyGone = true
	env.failWorktreeRemove = true
	if err := removeWith(env, "%1", RemoveWindowAndWorktree); err != nil {
		t.Fatalf("removeWith must converge (succeed) when the worktree is already gone: %v", err)
	}
	if hasCall(env.calls, "worktree_remove(") {
		t.Errorf("worktree_remove must be skipped when the path is already gone: %v", env.calls)
	}
	if !hasCall(env.calls, "kill_window(@1)") {
		t.Error("missing kill_window")
	}
}

func TestRemoveWindowOnlySkipsWorktreeAndBranch(t *testing.T) {
	env := newFakeSpawnEnv()
	if err := removeWith(env, "%1", RemoveWindowOnly); err != nil {
		t.Fatalf("removeWith error: %v", err)
	}
	if !hasCall(env.calls, "kill_window(@1)") {
		t.Error("missing kill_window")
	}
	if hasCall(env.calls, "worktree_remove(") || hasCall(env.calls, "branch_delete(") {
		t.Errorf("RemoveWindowOnly must not touch worktree/branch: %v", env.calls)
	}
}

func TestRemoveRejectsPaneMissingSpawnedMarker(t *testing.T) {
	env := newFakeSpawnEnv()
	env.displayOutput = "\n/r\n/r/.worktrees/task\nagent/task\n@1"
	err := removeWith(env, "%1", RemoveWindowAndWorktree)
	if err == nil || !strings.Contains(err.Error(), "not created by sidebar spawn") {
		t.Fatalf("err = %v", err)
	}
	if hasCall(env.calls, "kill_window(") {
		t.Error("must not kill_window when the pane wasn't created by spawn")
	}
}

func TestRemoveRejectsPaneWithEmptyBranchMarker(t *testing.T) {
	env := newFakeSpawnEnv()
	env.displayOutput = "1\n/r\n/r/.worktrees/task\n\n@1"
	err := removeWith(env, "%1", RemoveWindowAndWorktree)
	if err == nil || !strings.Contains(err.Error(), "missing branch name") {
		t.Fatalf("err = %v", err)
	}
	if hasCall(env.calls, "worktree_remove(") || hasCall(env.calls, "kill_window(") {
		t.Errorf("must not touch anything when the branch marker is missing: %v", env.calls)
	}
}

func TestRemoveDoesNotKillWindowWhenBranchDeleteFails(t *testing.T) {
	env := newFakeSpawnEnv()
	env.failBranchDelete = true
	err := removeWith(env, "%1", RemoveWindowAndWorktree)
	if err == nil || !strings.Contains(err.Error(), "branch_delete failed") {
		t.Fatalf("err = %v", err)
	}
	if !hasCall(env.calls, "worktree_remove(") {
		t.Error("worktree_remove must run first")
	}
	if hasCall(env.calls, "kill_window(") {
		t.Errorf("kill_window must not run when branch_delete fails — the window is the retry handle for the orphaned branch: %v", env.calls)
	}
}

func TestRemoveSkipsBranchDeleteWhenBranchAlreadyGone(t *testing.T) {
	// Simulates re-running after a previous partial run: worktree_remove and
	// branch_delete already succeeded last time, only kill_window remains.
	// The remove flow must converge, not fail because the git target no
	// longer exists.
	env := newFakeSpawnEnv()
	env.worktreePathAlreadyGone = true
	env.branchAlreadyGone = true
	env.failBranchDelete = true // would surface immediately if called again by mistake
	if err := removeWith(env, "%1", RemoveWindowAndWorktree); err != nil {
		t.Fatalf("removeWith must converge: %v", err)
	}
	if hasCall(env.calls, "branch_delete(") || hasCall(env.calls, "worktree_remove(") {
		t.Errorf("both git steps must be skipped: %v", env.calls)
	}
	if !hasCall(env.calls, "kill_window(@1)") {
		t.Error("missing kill_window")
	}
}
