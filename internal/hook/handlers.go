package hook

import (
	"strconv"

	"tmux-agent/internal/activity"
	"tmux-agent/internal/clock"
	"tmux-agent/internal/event"
	"tmux-agent/internal/tmux"
)

// setStatus writes @pane_status. There's no "clear" branch — no handler in
// this file calls setStatus intending to unset it: the two paths that clear
// status back to empty (RunSessionEndTeardown and part 1's dead-pane cleanup
// poll loop) call UnsetPaneOption directly, bypassing this function. See the
// RunSessionEndTeardown doc comment (pending.go) for the full reasoning.
func setStatus(pane string, status tmux.PaneStatus) {
	tmux.SetPaneOption(pane, tmux.PaneStatusOpt, string(status))
	switch status {
	case tmux.StatusRunning, tmux.StatusIdle:
		tmux.UnsetPaneOption(pane, tmux.PaneAttention)
	}
	// "Done, not yet seen" only survives while the pane stays idle — any
	// other status supersedes it.
	if status != tmux.StatusIdle {
		tmux.UnsetPaneOption(pane, tmux.PaneUnseen)
	}
}

// MarkUnseenIfUnviewed sets @pane_unseen when a run just finished and nobody
// is looking at the pane. The after-select-pane/window hooks clear it once
// the user switches to the pane. Shared by OnStop and the screen scraper.
func MarkUnseenIfUnviewed(pane string) {
	if !tmux.IsPaneViewed(pane) {
		tmux.SetPaneOption(pane, tmux.PaneUnseen, "1")
	}
}

// ApplyPaneStatus is the single source of truth for the "running/idle ⇒
// unset attention" rule, used both by internal handlers (via setStatus) and
// the external "set-status" command (cmd/tmux-agent), where status can be an
// arbitrary hand-typed string. "clear" is a sentinel specific to that
// external interface — like setAttention(pane, "clear") — it unsets both
// options rather than writing "clear" as a value; no internal handler needs
// this branch (see the setStatus comment).
func ApplyPaneStatus(pane, status string) {
	if status == "clear" {
		tmux.UnsetPaneOption(pane, tmux.PaneStatusOpt)
		tmux.UnsetPaneOption(pane, tmux.PaneAttention)
		return
	}
	setStatus(pane, tmux.PaneStatus(status))
}

// setAttention writes @pane_attention. "clear" is a sentinel meaning UNSET
// (not a value written into the option) — the four handlers below call
// setAttention(pane, "clear") as the first thing they do at the start of a
// new run.
func setAttention(pane, state string) {
	if state == "clear" {
		tmux.UnsetPaneOption(pane, tmux.PaneAttention)
		return
	}
	tmux.SetPaneOption(pane, tmux.PaneAttention, state)
}

// markTaskReset writes a "new task" marker to the activity log. Guarded by
// PaneWritesAllowed: if a subagent is still running, the parent's
// Stop/StopFailure must not write a reset marker — the child is still
// accumulating task progress, and resetting now would wrongly wipe the
// child's progress.
func markTaskReset(pane string) {
	if !PaneWritesAllowed(pane) {
		return
	}
	activity.WriteEntry(pane, activity.TaskResetMarker, "")
}

// agentContextFrom builds an AgentContext from the "session context" fields
// of an Event (see that group's comment on internal/event.Event) — used by
// every handler for the 6 Kinds that carry this group.
func agentContextFrom(evt event.Event) AgentContext {
	return NewAgentContext(evt.Agent, evt.Cwd, evt.PermissionMode, evt.Worktree, evt.SessionID)
}

func OnSessionStart(paneID string, evt event.Event) int {
	SetAgentMeta(paneID, agentContextFrom(evt))
	setAttention(paneID, "clear")
	ClearRunState(paneID)
	SetNotificationRunID(paneID)
	tmux.UnsetPaneOption(paneID, tmux.PanePrompt)
	tmux.UnsetPaneOption(paneID, tmux.PanePromptSource)
	// @pane_subagents is DELIBERATELY NOT touched here. A subagent shares
	// $TMUX_PANE with its parent, so when a subagent fires its own
	// SessionStart (after the parent's SubagentStart has already recorded it
	// in the list), unsetting here would lose the marker that
	// ShouldUpdateCwd/DrainPendingTeardowns rely on.
	tmux.UnsetPaneOption(paneID, PendingSessionEnd)
	tmux.UnsetPaneOption(paneID, PendingWorktreeRemove)
	switch evt.Source {
	case "resume":
		tmux.SetPaneOption(paneID, tmux.PaneWaitReason, "session_resumed")
	case "compact":
		tmux.SetPaneOption(paneID, tmux.PaneWaitReason, "session_resumed_compact")
	default:
		tmux.UnsetPaneOption(paneID, tmux.PaneWaitReason)
	}
	setStatus(paneID, tmux.StatusIdle)
	return 0
}

func OnSessionEnd(paneID, agentName string, evt event.Event, settings NotificationSettings) int {
	// A SessionEnd firing while a subagent is still alive is almost certainly
	// the CHILD's (there's no way to tell parent/child apart from the Event
	// alone) — skip the whole thing rather than risk wiping the state of a
	// parent that's still genuinely running.
	if !PaneWritesAllowed(paneID) {
		return 0
	}
	// Only two "abnormal" end reasons are worth notifying about — the usual
	// end_reason (clear/resume/prompt_input_exit/other) stays silent. Same
	// policy as HandleNotification (see sessionEndReasonNotifies, notify.go)
	// — call that function directly instead of duplicating the literal.
	if sessionEndReasonNotifies(evt.EndReason) {
		// SessionEnd carries no Agent (only ClaudeAdapter produces this Kind,
		// and it only sets EndReason) — fill it in from agentName (argv) so
		// HandleNotification has an agent label for the title.
		evt.Agent = agentName
		HandleNotification(paneID, evt, settings)
	}
	RunSessionEndTeardown(paneID)
	return 0
}

func OnUserPromptSubmit(paneID string, evt event.Event) int {
	checkpointTurn(paneID, evt.Cwd) // before the agent touches anything
	SetAgentMeta(paneID, agentContextFrom(evt))
	setAttention(paneID, "clear")
	setStatus(paneID, tmux.StatusRunning)
	SetNotificationRunID(paneID)
	if evt.Prompt != "" && !IsSystemMessage(evt.Prompt) {
		tmux.SetPaneOption(paneID, tmux.PanePrompt, activity.SanitizeLabel(evt.Prompt))
		tmux.SetPaneOption(paneID, tmux.PanePromptSource, "user")
	}
	tmux.SetPaneOption(paneID, tmux.PaneStartedAt, strconv.FormatInt(clock.NowUnix(), 10))
	tmux.UnsetPaneOption(paneID, tmux.PaneWaitReason)
	return 0
}

func OnStop(paneID string, evt event.Event, settings NotificationSettings) int {
	SetAgentMeta(paneID, agentContextFrom(evt))
	setAttention(paneID, "clear")
	if evt.LastMessage != "" {
		tmux.SetPaneOption(paneID, tmux.PanePrompt, activity.SanitizeLabel(evt.LastMessage))
		tmux.SetPaneOption(paneID, tmux.PanePromptSource, "response")
	}
	bgShellLive := backgroundShellLive(paneID)
	// @pane_subagents is DELIBERATELY NOT cleared here. A previous version
	// assumed Claude Code subagents always run synchronously (finishing
	// before the parent reaches Stop), so it treated any list still left over
	// at Stop as garbage. That assumption is wrong for background/async
	// dispatch (the Agent tool's "fork"/background mode): the parent can
	// reach Stop while a child subagent is still running — clearing here
	// would make the subagent tree disappear from the sidebar before the work
	// is actually done. OnSubagentStop (which runs exactly when each
	// subagent actually finishes) is the right place to clean up
	// @pane_subagents.
	if bgShellLive {
		tmux.UnsetPaneOption(paneID, tmux.PaneWaitReason)
	} else {
		ClearRunState(paneID)
	}
	markTaskReset(paneID)
	setStatus(paneID, ResolveStopStatus(bgShellLive))
	if !bgShellLive {
		MarkUnseenIfUnviewed(paneID)
		HandleNotification(paneID, evt, settings)
	}
	// evt.Response (if any) is NOT printed here — printing to stdout is the
	// Dispatch layer's job, the ONLY place in all of part 2 allowed to do
	// that.
	return 0
}

func OnStopFailure(paneID string, evt event.Event, settings NotificationSettings) int {
	SetAgentMeta(paneID, agentContextFrom(evt))
	setAttention(paneID, "clear")
	ClearRunState(paneID)
	// DELIBERATELY NOT calling UnsetPaneOption(tmux.PaneSubagents) before
	// markTaskReset here, unlike OnStop above: the reference implementation
	// (on_stop_failure in handlers/run.rs) also doesn't clear @pane_subagents
	// for the failure branch, even though its own on_stop does (see the
	// comment on OnStop). This has been checked against upstream and the
	// divergence is kept as-is rather than applying the same logic to both,
	// because the reference implementation itself distinguishes
	// Stop/StopFailure — this isn't a spot upstream simply forgot.
	markTaskReset(paneID)
	if evt.Error != "" {
		tmux.SetPaneOption(paneID, tmux.PaneWaitReason, evt.Error)
	}
	setStatus(paneID, tmux.StatusError)
	HandleNotification(paneID, evt, settings)
	return 0
}

func OnTaskCompleted(paneID, agentName string, evt event.Event, settings NotificationSettings) int {
	// No adapter sets Event.Agent for TaskCompleted — fill it in from
	// agentName (argv) before handing off to HandleNotification, otherwise
	// the notification label would have an empty agent.
	if evt.Agent == "" {
		evt.Agent = agentName
	}
	setAttention(paneID, "notification")
	HandleNotification(paneID, evt, settings)
	return 0
}

func OnNotification(paneID string, evt event.Event, settings NotificationSettings) int {
	SetAgentMeta(paneID, agentContextFrom(evt))
	if evt.MetaOnly {
		return 0
	}
	bgShellLive := backgroundShellLive(paneID)
	setStatus(paneID, ResolveNotificationStatus(evt.WaitReason, bgShellLive))
	setAttention(paneID, "notification")
	if evt.WaitReason == "" {
		tmux.UnsetPaneOption(paneID, tmux.PaneWaitReason)
	} else {
		tmux.SetPaneOption(paneID, tmux.PaneWaitReason, evt.WaitReason)
	}
	HandleNotification(paneID, evt, settings)
	return 0
}

func OnPermissionDenied(paneID string, evt event.Event, settings NotificationSettings) int {
	SetAgentMeta(paneID, agentContextFrom(evt))
	setStatus(paneID, tmux.StatusWaiting)
	setAttention(paneID, "notification")
	tmux.SetPaneOption(paneID, tmux.PaneWaitReason, "permission_denied")
	HandleNotification(paneID, evt, settings)
	return 0
}

func OnTeammateIdle(paneID string, evt event.Event) int {
	setAttention(paneID, "notification")
	reason := "teammate_idle:" + evt.TeammateName
	if evt.IdleReason != "" {
		reason += ":" + evt.IdleReason
	}
	tmux.SetPaneOption(paneID, tmux.PaneWaitReason, reason)
	return 0
}

func OnSubagentStart(paneID string, evt event.Event) int {
	if evt.AgentID == "" {
		return 0
	}
	// Read-compute-write under a per-pane lock: subagents launched together
	// fire their SubagentStart hooks at the same moment, and without it each
	// overwrote the others' entry (3 agents started, 1 listed).
	unlock := lockPane(paneID)
	defer unlock()
	current := tmux.GetPaneOptionValue(paneID, tmux.PaneSubagents)
	tmux.SetPaneOption(paneID, tmux.PaneSubagents, AppendSubagent(current, evt.AgentType, evt.AgentID))
	return 0
}

func OnSubagentStop(paneID string, evt event.Event) int {
	if evt.AgentID == "" {
		return 0
	}
	// Same per-pane lock as OnSubagentStart above.
	unlock := lockPane(paneID)
	defer unlock()
	current := tmux.GetPaneOptionValue(paneID, tmux.PaneSubagents)
	newVal, ok := RemoveSubagent(current, evt.AgentID)
	if !ok {
		return 0
	}
	if newVal == "" {
		tmux.UnsetPaneOption(paneID, tmux.PaneSubagents)
		// Once the LAST subagent stops, replay any deferred teardown
		// (WorktreeRemove firing while a subagent was still around — see
		// OnWorktreeRemove). SessionEnd does NOT participate in this deferral
		// path — see the comment on OnSessionEnd.
		DrainPendingTeardowns(paneID)
	} else {
		tmux.SetPaneOption(paneID, tmux.PaneSubagents, newVal)
	}
	return 0
}

func OnWorktreeRemove(paneID string) int {
	// If a subagent is still active, the worktree being removed could belong
	// to ONE OF THEM — parent/child can't be told apart here, so the safe
	// default is to leave the parent's metadata untouched and record the
	// intent, for OnSubagentStop to replay once the last subagent stops.
	if !PaneWritesAllowed(paneID) {
		MarkPending(paneID, PendingWorktreeRemove)
		return 0
	}
	RunWorktreeRemoveTeardown(paneID)
	return 0
}

// activityLogStatusTransition applies the state-transition part around
// ActivityLog that HandleActivityLog (internal/hook/activity.go, the activity
// task) does NOT do — see that function's own doc comment: label/background
// shell/log-writing are activity.go's job, while "a new tool-use brings the
// pane back to running" and "EnterPlanMode/ExitPlanMode changes
// @pane_permission_mode" are state decisions that belong to this file. Called
// from route() right next to HandleActivityLog, without modifying the
// activity task's file or redefining HandleActivityLog.
func activityLogStatusTransition(pane string, evt event.Event) {
	current := tmux.GetPaneOptionValue(pane, tmux.PaneStatusOpt)
	if current != string(tmux.StatusRunning) && current != "" {
		setStatus(pane, tmux.StatusRunning)
		if current == string(tmux.StatusWaiting) {
			tmux.UnsetPaneOption(pane, tmux.PaneAttention)
			tmux.UnsetPaneOption(pane, tmux.PaneWaitReason)
		}
		if tmux.GetPaneOptionValue(pane, tmux.PaneStartedAt) == "" {
			tmux.SetPaneOption(pane, tmux.PaneStartedAt, strconv.FormatInt(clock.NowUnix(), 10))
		}
	}

	// Same parent-protection rule as SetAgentMeta: a subagent entering/exiting
	// plan mode must not change the parent pane's displayed permission
	// badge.
	if !PaneWritesAllowed(pane) {
		return
	}
	switch evt.ToolName {
	case "EnterPlanMode":
		tmux.SetPaneOption(pane, tmux.PanePermissionMode, "plan")
	case "ExitPlanMode":
		tmux.SetPaneOption(pane, tmux.PanePermissionMode, "default")
	}
}
