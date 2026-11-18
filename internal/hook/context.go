package hook

import (
	"strings"

	"tmux-agent/internal/event"
	"tmux-agent/internal/tmux"
)

// AgentContext gathers the hook payload fields shared by 6 event Kinds
// (SessionStart, UserPromptSubmit, Notification, Stop, StopFailure,
// PermissionDenied — see the "Session context" group comment on
// internal/event.Event). Passing a single AgentContext instead of five loose
// parameters keeps the signatures of the functions below short.
type AgentContext struct {
	Agent          string
	Cwd            string
	PermissionMode string
	Worktree       *event.WorktreeInfo
	SessionID      string
}

// NewAgentContext builds an AgentContext from loose fields.
func NewAgentContext(agent, cwd, permissionMode string, worktree *event.WorktreeInfo, sessionID string) AgentContext {
	return AgentContext{
		Agent:          agent,
		Cwd:            cwd,
		PermissionMode: permissionMode,
		Worktree:       worktree,
		SessionID:      sessionID,
	}
}

// SetAgentMeta writes @pane_agent, (conditionally) @pane_permission_mode, then
// calls SyncPaneLocation to sync cwd/worktree/session id.
//
// @pane_permission_mode is DELIBERATELY only written when
// PaneWritesAllowed(pane): this option is owned by the PARENT agent — a child
// subagent may run under a different permission mode than its parent (e.g.
// the parent is in "plan" while the subagent runs "default"), and without
// this guard the subagent's hook would incorrectly flip the parent's
// displayed permission badge mid-run. An empty ctx.PermissionMode is also
// skipped (never overwrite with empty).
func SetAgentMeta(pane string, ctx AgentContext) {
	tmux.SetPaneOption(pane, tmux.PaneAgent, ctx.Agent)
	// A hook firing means this pane has a real integration — take it back
	// from the screen scraper (internal/scrape skips panes without this mark).
	tmux.UnsetPaneOption(pane, tmux.PaneAgentSrc)
	if ctx.PermissionMode != "" && PaneWritesAllowed(pane) {
		tmux.SetPaneOption(pane, tmux.PanePermissionMode, ctx.PermissionMode)
	}
	SyncPaneLocation(pane, ctx.Cwd, ctx.Worktree, ctx.SessionID)
}

// ClearRunState unsets @pane_started_at and @pane_wait_reason.
//
// This package ONLY unsets these two options, never writes real values for
// them (nor for @pane_prompt) — deciding "when did this run start" and "what
// is it waiting for" is a state decision that belongs to the handler (a
// different task), not to this tmux-writing layer.
func ClearRunState(pane string) {
	tmux.UnsetPaneOption(pane, tmux.PaneStartedAt)
	tmux.UnsetPaneOption(pane, tmux.PaneWaitReason)
}

// IsSystemMessage returns true when s is a prompt inserted automatically by
// the system (not actually typed by the user), so the UserPromptSubmit
// handler can skip it instead of displaying it as a real user prompt.
func IsSystemMessage(s string) bool {
	return strings.Contains(s, "<task-notification>") ||
		strings.Contains(s, "<system-reminder>") ||
		strings.Contains(s, "<task-") ||
		strings.Contains(s, "<agent-message ")
}

// clearAllMeta unsets every pane-level option that represents the STATE OF ONE
// AGENT RUN — it does not touch PaneAttention/PaneStatusOpt (those two
// options are unset separately by RunSessionEndTeardown, see pending.go), nor
// PaneRole/PaneName (pane identity/user-assigned label, which outlives a
// single run — see the clearedPaneKeys comment in internal/tmux/query.go).
//
// Not exported: only RunSessionEndTeardown (pending.go) calls this function.
// The original Rust version also keeps clear_all_meta as a module-internal
// function, not exported outside context.rs.
func clearAllMeta(pane string) {
	for _, key := range []string{
		tmux.PaneAgent,
		tmux.PanePrompt,
		tmux.PanePromptSource,
		tmux.PaneBgCmd,
		tmux.PaneBgLive,
		tmux.PaneSubagents,
		tmux.PaneCwd,
		tmux.PanePermissionMode,
		tmux.PaneWorktreeName,
		tmux.PaneWorktreeBranch,
		tmux.PaneSessionID,
		PendingSessionEnd,
		PendingWorktreeRemove,
	} {
		tmux.UnsetPaneOption(pane, key)
	}
	ClearRunState(pane)
}
