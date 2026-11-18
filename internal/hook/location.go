package hook

import (
	"tmux-agent/internal/event"
	"tmux-agent/internal/tmux"
)

// ShouldUpdateCwd returns true when @pane_cwd (and the location fields that
// go with it) is allowed to be updated, i.e. when currentSubagents is empty.
//
// A subagent shares $TMUX_PANE with its parent agent. While a subagent is
// running, a hook event arriving at this pane may come from the subagent
// (running in a different worktree, different cwd) rather than from the
// parent agent — overwriting at that point would lose the parent's identity.
func ShouldUpdateCwd(currentSubagents string) bool {
	return currentSubagents == ""
}

// ResolveCwd picks the effective cwd to write into the pane metadata.
//
// When running inside a worktree, prefer OriginalRepoDir so the sidebar
// groups the pane by the original repo, not by the worktree path (which
// usually lives somewhere entirely different, in a .worktrees/<name> form).
func ResolveCwd(rawCwd string, worktree *event.WorktreeInfo) string {
	if worktree != nil && worktree.OriginalRepoDir != "" {
		return worktree.OriginalRepoDir
	}
	return rawCwd
}

// SyncWorktreeMeta syncs @pane_worktree_name/@pane_worktree_branch from the
// hook payload.
//
// worktree == nil: clear both options — the pane is no longer inside any
// worktree. worktree != nil but one field is empty: clear that field alone.
// A payload that explicitly says "no longer in worktree X" (Name empty) must
// not leave the old @pane_worktree_branch from the previous run lying
// around — otherwise the sidebar would keep showing the stale branch even
// though the worktree has changed.
func SyncWorktreeMeta(pane string, worktree *event.WorktreeInfo) {
	if worktree == nil {
		tmux.UnsetPaneOption(pane, tmux.PaneWorktreeName)
		tmux.UnsetPaneOption(pane, tmux.PaneWorktreeBranch)
		return
	}
	if worktree.Name == "" {
		tmux.UnsetPaneOption(pane, tmux.PaneWorktreeName)
	} else {
		tmux.SetPaneOption(pane, tmux.PaneWorktreeName, worktree.Name)
	}
	if worktree.Branch == "" {
		tmux.UnsetPaneOption(pane, tmux.PaneWorktreeBranch)
	} else {
		tmux.SetPaneOption(pane, tmux.PaneWorktreeBranch, worktree.Branch)
	}
}

// SyncPaneLocation syncs @pane_session_id, @pane_cwd and
// @pane_worktree_name/branch from a hook event.
//
// An empty sessionID means "absent" (event.Event uses a flat string and
// doesn't distinguish missing from empty — a decision settled in part 2a),
// so it gets UNSET instead of having an empty string written.
//
// Reads @pane_subagents of THIS SAME pane (not a parameter) to decide
// whether writing is allowed — see ShouldUpdateCwd. This is one extra tmux
// read per call; acceptable since the hook only runs once per event, not in
// a hot loop.
func SyncPaneLocation(pane, cwd string, worktree *event.WorktreeInfo, sessionID string) {
	current := tmux.GetPaneOptionValue(pane, tmux.PaneSubagents)
	if !ShouldUpdateCwd(current) {
		return
	}
	if sessionID != "" {
		tmux.SetPaneOption(pane, tmux.PaneSessionID, sessionID)
	} else {
		tmux.UnsetPaneOption(pane, tmux.PaneSessionID)
	}
	if cwd != "" {
		tmux.SetPaneOption(pane, tmux.PaneCwd, ResolveCwd(cwd, worktree))
	}
	SyncWorktreeMeta(pane, worktree)
}

// PaneWritesAllowed returns true when a pane-level write from the current
// hook event is safe, i.e. when no subagent is currently running on this
// pane. Used to prevent a child hook (subagent) from overwriting the parent
// pane's identity — e.g. @pane_permission_mode in SetAgentMeta.
func PaneWritesAllowed(pane string) bool {
	current := tmux.GetPaneOptionValue(pane, tmux.PaneSubagents)
	return ShouldUpdateCwd(current)
}
