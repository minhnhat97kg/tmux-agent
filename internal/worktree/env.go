package worktree

// SpawnEnv bundles every side effect (git + tmux) the spawn/remove flow
// (task 2, flow.go) needs, so that flow's logic can be tested with a fake,
// without needing real git or tmux. This file ONLY declares the contract —
// the real implementation (RealEnv, calling the internal/git +
// internal/tmux packages) is task 2's job.
type SpawnEnv interface {
	// BranchPrefix reads the tmux option @agent-sidebar-branch-prefix. ""
	// means unset — the caller applies DefaultBranchPrefix itself.
	BranchPrefix() string
	// WorktreeDir reads the tmux option @agent-sidebar-worktree-dir. ""
	// means unset — the caller applies DefaultWorktreeDir itself.
	WorktreeDir() string
	// BranchIsFree reports true when the branch does NOT yet exist in the repo.
	BranchIsFree(repo, branch string) bool
	// BranchExists reports true when the branch already exists in the repo.
	// The remove flow uses this to skip "git branch -D" when a previous
	// partial run already deleted it, letting the remove flow converge
	// (retry-safe) on a re-run.
	BranchExists(repo, branch string) bool
	// WorktreePathIsFree reports true when the worktree path does NOT yet exist on disk.
	WorktreePathIsFree(path string) bool
	// WorktreePathExists reports true when the worktree path currently
	// exists on disk. Differs from WorktreePathIsFree in the default
	// polarity test fakes need: spawn wants "free" (is_free=true) while
	// remove wants "exists" (exists=true).
	WorktreePathExists(path string) bool
	WorktreeAdd(repo, worktreePath, branch string) error
	WorktreeRemove(repo, worktreePath string) error
	BranchDelete(repo, branch string) error
	// NewWindow creates a new window, returning (paneID, windowID).
	NewWindow(session, cwd, name string) (paneID, windowID string, err error)
	KillWindow(windowID string) error
	SetWindowOption(window, key, value string) error
	SendCommand(target, command string) error
	DisplayMessage(paneID, template string) string
}
