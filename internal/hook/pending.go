package hook

import (
	"os"

	"tmux-agent/internal/activity"
	"tmux-agent/internal/tmux"
)

// PendingSessionEnd is the name of an old option. NOTHING in this package
// writes it anymore — a child subagent's SessionEnd cannot be distinguished
// from its parent's SessionEnd, so this defer-then-clean trick was too
// dangerous to keep (it could clean up the wrong, still-alive parent pane).
// This constant only still exists so clearAllMeta (and a fixture that
// installs the old option before the fix) has somewhere to sweep up a
// leftover marker from a pre-fix install.
const PendingSessionEnd = tmux.PanePendingSessionEnd

// PendingWorktreeRemove is the option flagging that a WorktreeRemove was
// deferred because a subagent is still running on the pane.
// DrainPendingTeardowns pulls it out once @pane_subagents goes back to
// empty (called from the SubagentStop handler, a different task).
const PendingWorktreeRemove = tmux.PanePendingWorktreeRemove

// MarkPending writes the "1" flag for key (one of the two Pending*
// constants above). The handler calls this when a teardown can't run
// immediately (a subagent is still alive) and must be deferred until it's
// safe.
func MarkPending(pane, key string) {
	tmux.SetPaneOption(pane, key, "1")
}

// DrainPendingTeardowns runs the teardown that was deferred by an earlier
// MarkPending call for PendingWorktreeRemove. Called from the SubagentStop
// handler AFTER the subagent list has gone back to empty, so the parent
// pane finally gets cleaned up instead of being stuck with stale metadata
// from a worktree that has already been removed.
//
// PendingSessionEnd is INTENTIONALLY not involved here (see its doc
// comment) — a child's SessionEnd running before the parent's SubagentStop
// should have been blocked earlier (in the handler, not here), not replayed
// here as a teardown aimed at a parent that's still alive.
func DrainPendingTeardowns(pane string) {
	if tmux.GetPaneOptionValue(pane, PendingWorktreeRemove) != "" {
		RunWorktreeRemoveTeardown(pane)
		tmux.UnsetPaneOption(pane, PendingWorktreeRemove)
	}
}

// RunSessionEndTeardown is the body of the SessionEnd handler (only Claude
// emits this Kind — see internal/event.Event). The handler calls this
// function when SessionEnd arrives and NO subagent is currently running
// (if a subagent is still alive, the handler must skip the parent's
// SessionEnd and not call this function — that decision belongs to the
// handler).
//
// Clears EVERY trace of the run: @pane_attention, every option in
// clearAllMeta, @pane_status, and the on-disk activity log file.
//
// NOTE FOR READERS OF THE ORIGINAL RUST VERSION: `set_attention(pane,
// "clear")` and `set_status(pane, "clear")` in pending.rs do NOT write the
// string "clear" as the option value — the string "clear" there is a
// SENTINEL that two helper functions in cli/mod.rs interpret as UNSET (see
// how set_status/set_attention are read in cli/mod.rs: the branch `if
// status == "clear"` calls unset_pane_option, not set_pane_option). This Go
// repo doesn't have (and this task doesn't add) the two sentinel-style
// SetAttention/SetStatus wrapper functions — they're not in the task's list
// of functions to export — so here we call tmux.UnsetPaneOption directly
// for those two options, i.e. exactly the observable behavior of the Rust
// version, not the "SET = the string clear" reading that sounds more
// plausible from the set_attention/set_status names.
//
// Consequence: adding clearAllMeta (14 keys) together with the two
// UnsetPaneOption calls below (2 keys) gives EXACTLY 16 keys — matching
// clearedPaneKeys in internal/tmux/query.go key-for-key (the poll loop that
// cleans up dead Codex/OpenCode panes). The two cleanup paths are NOT
// "deliberately different" between SET and UNSET the way an earlier draft
// of this doc once claimed — both are UNSET, targeting exactly the same
// thing. The REAL difference between them is WHERE THEY'RE TRIGGERED FROM:
//   - clearedPaneKeys (internal/tmux/query.go, clearAgentPaneState): the
//     list-panes poll loop detects that a Codex/OpenCode process has died
//     (these two agents have no reliable SessionEnd hook — Codex has no
//     exit hook, OpenCode runs on Bun so its exit handler isn't guaranteed
//     to run).
//   - RunSessionEndTeardown (here): Claude's SessionEnd hook calls this
//     explicitly, right when the session ends — no need to wait for the
//     poll loop to detect it.
//
// The two paths are not merged into one shared function: internal/tmux
// can't import internal/hook (internal/hook already imports internal/tmux
// — merging would create an import cycle), and they serve two genuinely
// different detection mechanisms (push from the hook vs. pull from the
// poll). The 16-key duplication is real duplication, but it is NOT
// self-fixed here — if a future task needs to consolidate it, that's a
// separate architectural decision (e.g. turning clearedPaneKeys into a
// shared constant both packages read), to be raised and decided, not done
// silently as part of this task.
func RunSessionEndTeardown(pane string) {
	tmux.UnsetPaneOption(pane, tmux.PaneAttention)
	clearAllMeta(pane)
	tmux.UnsetPaneOption(pane, tmux.PaneStatusOpt)
	os.Remove(activity.LogFilePath(pane))
}

// RunWorktreeRemoveTeardown is the body of the WorktreeRemove handler: it
// clears the @pane_worktree_name/branch and @pane_cwd written by the hook,
// so the query layer (part 1) falls back to pane_current_path instead of
// continuing to display the path of a worktree that has been removed.
//
// Does NOT touch @pane_agent/@pane_subagents/... — a removed worktree
// doesn't mean the agent in that pane has stopped running; it may have
// simply changed its cwd back to the base repo and kept working.
func RunWorktreeRemoveTeardown(pane string) {
	SyncWorktreeMeta(pane, nil)
	tmux.UnsetPaneOption(pane, tmux.PaneCwd)
}
