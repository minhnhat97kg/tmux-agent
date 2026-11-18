package hook

import (
	"testing"

	"tmux-agent/internal/event"
	"tmux-agent/internal/tmux"
)

// ─── Shared test infrastructure for this file ───────────────────────

// seed pre-declares the value that GetPaneOptionValue(pane, key) must read
// back (tmux.Fake doesn't simulate real tmux state — it just returns the
// output declared for an exact argument list).
func seed(f *tmux.Fake, pane, key, value string) {
	f.Set(value, "show", "-t", pane, "-pv", key)
}

// finalPaneOptions rebuilds the final pane option state from the log of
// executed commands (f.Calls()), simulating real tmux's "later writes
// overwrite earlier ones, unset removes" behavior — because Fake doesn't
// remember an earlier set when GetPaneOptionValue is called again later.
func finalPaneOptions(f *tmux.Fake, pane string) map[string]string {
	state := map[string]string{}
	for _, c := range f.Calls() {
		if len(c) == 6 && c[0] == "set" && c[1] == "-t" && c[2] == pane && c[3] == "-p" && c[4] == "-u" {
			delete(state, c[5])
			continue
		}
		if len(c) == 6 && c[0] == "set" && c[1] == "-t" && c[2] == pane && c[3] == "-p" {
			state[c[4]] = c[5]
		}
	}
	return state
}

// enabledSettings returns a notification configuration that allows EVERY Kind
// (equivalent to @sidebar_notifications_events=all) — used in handler tests
// so HandleNotification isn't blocked by settings.enabled(), letting the
// tests observe its real dedup/fingerprint branch.
func enabledSettings() NotificationSettings {
	events := map[event.Kind]bool{}
	for _, k := range NotificationEventKinds() {
		events[k] = true
	}
	return NotificationSettings{Enabled: true, Events: events}
}

// ─── ApplyPaneStatus ────────────────────────────────────────────────
//
// The single source of truth for the "running/idle => unset attention" rule,
// shared by setStatus (internal handler) and the external "set-status"
// command (cmd/tmux-agent).

func TestApplyPaneStatusWritesStatus(t *testing.T) {
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()

	ApplyPaneStatus("%3", "waiting")

	opts := finalPaneOptions(fake, "%3")
	if opts[tmux.PaneStatusOpt] != "waiting" {
		t.Fatalf("@pane_status = %q, want %q", opts[tmux.PaneStatusOpt], "waiting")
	}
	if _, unset := opts[tmux.PaneAttention]; unset {
		t.Fatalf("\"waiting\" must not touch @pane_attention, got %v", opts)
	}
}

func TestApplyPaneStatusRunningAndIdleClearAttention(t *testing.T) {
	for _, status := range []string{"running", "idle"} {
		t.Run(status, func(t *testing.T) {
			fake := tmux.NewFake()
			defer tmux.SetRunner(fake)()

			ApplyPaneStatus("%3", status)

			opts := finalPaneOptions(fake, "%3")
			if opts[tmux.PaneStatusOpt] != status {
				t.Fatalf("@pane_status = %q, want %q", opts[tmux.PaneStatusOpt], status)
			}
			if _, stillSet := opts[tmux.PaneAttention]; stillSet {
				t.Fatalf("%q must clear @pane_attention, got %v", status, opts)
			}
		})
	}
}

func TestApplyPaneStatusClearClearsBoth(t *testing.T) {
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()

	ApplyPaneStatus("%3", "clear")

	calls := fake.Calls()
	wantUnset := map[string]bool{tmux.PaneStatusOpt: false, tmux.PaneAttention: false}
	for _, c := range calls {
		if len(c) == 6 && c[0] == "set" && c[3] == "-p" && c[4] == "-u" {
			if _, tracked := wantUnset[c[5]]; tracked {
				wantUnset[c[5]] = true
			}
		}
	}
	for key, got := range wantUnset {
		if !got {
			t.Errorf("\"clear\" must unset %q, got %v", key, calls)
		}
	}
}

// ─── OnSessionStart ─────────────────────────────────────────────────

func TestOnSessionStartSetsAgentAndIdleStatus(t *testing.T) {
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%NEW_SESSION"
	evt := event.Event{Kind: event.SessionStart, Agent: "claude", Cwd: "/repo", SessionID: "sess-1"}

	got := OnSessionStart(pane, evt)

	if got != 0 {
		t.Fatalf("OnSessionStart returned %d, want 0", got)
	}
	final := finalPaneOptions(f, pane)
	if final[tmux.PaneStatusOpt] != string(tmux.StatusIdle) {
		t.Errorf("@pane_status = %q, want %q", final[tmux.PaneStatusOpt], tmux.StatusIdle)
	}
	if final[tmux.PaneAgent] != "claude" {
		t.Errorf("@pane_agent = %q, want claude", final[tmux.PaneAgent])
	}
}

func TestOnSessionStartResumeWritesWaitReason(t *testing.T) {
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%RESUME"
	OnSessionStart(pane, event.Event{Kind: event.SessionStart, Source: "resume"})
	if got := finalPaneOptions(f, pane)[tmux.PaneWaitReason]; got != "session_resumed" {
		t.Errorf("@pane_wait_reason = %q, want session_resumed", got)
	}
}

func TestOnSessionStartCompactWritesOwnWaitReason(t *testing.T) {
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%COMPACT"
	OnSessionStart(pane, event.Event{Kind: event.SessionStart, Source: "compact"})
	if got := finalPaneOptions(f, pane)[tmux.PaneWaitReason]; got != "session_resumed_compact" {
		t.Errorf("@pane_wait_reason = %q, want session_resumed_compact", got)
	}
}

func TestOnSessionStartStartupClearsStaleWaitReason(t *testing.T) {
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%FRESH"
	OnSessionStart(pane, event.Event{Kind: event.SessionStart, Source: "startup"})
	if _, ok := finalPaneOptions(f, pane)[tmux.PaneWaitReason]; ok {
		t.Error("@pane_wait_reason must be empty after startup, an old value leaked through")
	}
}

// ─── OnSessionEnd ───────────────────────────────────────────────────

func TestOnSessionEndSkippedWhileSubagentAlive(t *testing.T) {
	// A subagent shares $TMUX_PANE with its parent, so a parent's SessionEnd
	// can't be distinguished from a child's — skip it entirely so the running
	// parent's state doesn't get wrongly cleared.
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%CHILD_END"
	seed(f, pane, tmux.PaneSubagents, "Explore:sub-1")

	OnSessionEnd(pane, "claude", event.Event{Kind: event.SessionEnd}, enabledSettings())

	final := finalPaneOptions(f, pane)
	if _, ok := final[tmux.PaneAgent]; ok {
		t.Error("a child's SessionEnd must not touch the parent's @pane_agent")
	}
}

func TestOnSessionEndNormalReasonSendsNoNotification(t *testing.T) {
	t.Setenv("TMUX_AGENT_LOG_DIR", t.TempDir())
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%END_ROUTINE"
	OnSessionEnd(pane, "claude", event.Event{Kind: event.SessionEnd, EndReason: "clear"}, enabledSettings())
	if _, ok := finalPaneOptions(f, pane)[tmux.PaneOSNotifyTaskCompleted]; ok {
		t.Error("a routine end_reason (clear) must not leave an anti-duplicate stamp")
	}
}

func TestOnSessionEndLogoutSendsNotification(t *testing.T) {
	t.Setenv("TMUX_AGENT_LOG_DIR", t.TempDir())
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%END_LOGOUT"
	OnSessionEnd(pane, "claude", event.Event{Kind: event.SessionEnd, EndReason: "logout"}, enabledSettings())
	if _, ok := finalPaneOptions(f, pane)[tmux.PaneOSNotifyTaskCompleted]; !ok {
		t.Error("end_reason logout must go through HandleNotification (the anti-duplicate stamp must exist)")
	}
}

func TestOnSessionEndBypassPermissionsDisabledSendsNotification(t *testing.T) {
	t.Setenv("TMUX_AGENT_LOG_DIR", t.TempDir())
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%END_BYPASS"
	OnSessionEnd(pane, "claude", event.Event{Kind: event.SessionEnd, EndReason: "bypass_permissions_disabled"}, enabledSettings())
	if _, ok := finalPaneOptions(f, pane)[tmux.PaneOSNotifyTaskCompleted]; !ok {
		t.Error("end_reason bypass_permissions_disabled must go through HandleNotification")
	}
}

// ─── OnUserPromptSubmit ─────────────────────────────────────────────

func TestOnUserPromptSubmitWritesPromptAndRunningStatus(t *testing.T) {
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%PROMPT"
	OnUserPromptSubmit(pane, event.Event{Kind: event.UserPromptSubmit, Prompt: "fix the bug"})

	final := finalPaneOptions(f, pane)
	if final[tmux.PaneStatusOpt] != string(tmux.StatusRunning) {
		t.Errorf("@pane_status = %q, want running", final[tmux.PaneStatusOpt])
	}
	if final[tmux.PanePrompt] != "fix the bug" {
		t.Errorf("@pane_prompt = %q, want %q", final[tmux.PanePrompt], "fix the bug")
	}
	if final[tmux.PanePromptSource] != "user" {
		t.Errorf("@pane_prompt_source = %q, want user", final[tmux.PanePromptSource])
	}
}

func TestOnUserPromptSubmitIgnoresSystemMessages(t *testing.T) {
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%SYS_PROMPT"
	OnUserPromptSubmit(pane, event.Event{
		Kind:   event.UserPromptSubmit,
		Prompt: "<system-reminder>ignore me</system-reminder>",
	})
	final := finalPaneOptions(f, pane)
	if _, ok := final[tmux.PanePrompt]; ok {
		t.Error("a system message must not be saved as the prompt")
	}
	if final[tmux.PaneStatusOpt] != string(tmux.StatusRunning) {
		t.Errorf("status must still switch to running even though the prompt was skipped, got %q", final[tmux.PaneStatusOpt])
	}
}

// ─── OnStop ─────────────────────────────────────────────────────────

func TestOnStopWithShellGoesToBackground(t *testing.T) {
	t.Setenv("TMUX_AGENT_LOG_DIR", t.TempDir())
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%STOP_BG"
	seed(f, pane, tmux.PaneBgCmd, "npm run dev")

	OnStop(pane, event.Event{Kind: event.Stop}, enabledSettings())

	if got := finalPaneOptions(f, pane)[tmux.PaneStatusOpt]; got != string(tmux.StatusBackground) {
		t.Errorf("@pane_status = %q, want background", got)
	}
}

func TestOnStopWithoutShellGoesToIdle(t *testing.T) {
	t.Setenv("TMUX_AGENT_LOG_DIR", t.TempDir())
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%STOP_IDLE"
	OnStop(pane, event.Event{Kind: event.Stop}, enabledSettings())
	if got := finalPaneOptions(f, pane)[tmux.PaneStatusOpt]; got != string(tmux.StatusIdle) {
		t.Errorf("@pane_status = %q, want idle", got)
	}
}

func TestOnStopKeepsSubagentListForAsyncDispatch(t *testing.T) {
	// The parent can Stop while a child subagent is still running in the
	// background (asynchronous dispatch) — OnStop must not touch
	// @pane_subagents (no set, no unset), cleanup belongs to OnSubagentStop
	// when each subagent actually stops. finalPaneOptions can't be used here:
	// it only replays the set/-u commands that ACTUALLY RAN, it doesn't know
	// the initial seed value — so assert directly on the call log, not on
	// inferred state.
	t.Setenv("TMUX_AGENT_LOG_DIR", t.TempDir())
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%STOP_LIVE_SUBAGENTS"
	seed(f, pane, tmux.PaneSubagents, "Explore:sub-1")

	OnStop(pane, event.Event{Kind: event.Stop}, enabledSettings())

	for _, c := range f.Calls() {
		if len(c) == 6 && c[0] == "set" && c[1] == "-t" && c[2] == pane && c[5] == tmux.PaneSubagents {
			t.Errorf("OnStop must not touch @pane_subagents, but it called: %v", c)
		}
	}
}

func TestOnStopWritesPromptFromLastMessage(t *testing.T) {
	t.Setenv("TMUX_AGENT_LOG_DIR", t.TempDir())
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%STOP_MSG"
	OnStop(pane, event.Event{Kind: event.Stop, LastMessage: "done | with pipe"}, enabledSettings())
	final := finalPaneOptions(f, pane)
	if final[tmux.PanePrompt] != "done   with pipe" {
		t.Errorf("@pane_prompt = %q, want the | character sanitized out", final[tmux.PanePrompt])
	}
	if final[tmux.PanePromptSource] != "response" {
		t.Errorf("@pane_prompt_source = %q, want response", final[tmux.PanePromptSource])
	}
}

// ─── OnStopFailure ──────────────────────────────────────────────────

func TestOnStopFailureWritesErrorAndWaitReason(t *testing.T) {
	t.Setenv("TMUX_AGENT_LOG_DIR", t.TempDir())
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%STOP_FAIL"
	OnStopFailure(pane, event.Event{Kind: event.StopFailure, Error: "rate_limit"}, enabledSettings())

	final := finalPaneOptions(f, pane)
	if final[tmux.PaneStatusOpt] != string(tmux.StatusError) {
		t.Errorf("@pane_status = %q, want error", final[tmux.PaneStatusOpt])
	}
	if final[tmux.PaneWaitReason] != "rate_limit" {
		t.Errorf("@pane_wait_reason = %q, want rate_limit", final[tmux.PaneWaitReason])
	}
}

// ─── OnTaskCompleted ────────────────────────────────────────────────

func TestOnTaskCompletedAssignsAgentFromArgvWhenEmpty(t *testing.T) {
	// No adapter assigns Event.Agent for TaskCompleted — OnTaskCompleted must
	// fill it in itself from agentName (argv) before handing off to
	// HandleNotification. SendDesktopNotification is a no-op function (part 4
	// wires in the real OS call), so title/body aren't observable from here —
	// only the OBSERVABLE consequence can be checked: the flow actually runs
	// through to the end and writes the anti-duplicate stamp.
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%TASK_DONE"

	OnTaskCompleted(pane, "codex", event.Event{Kind: event.TaskCompleted, TaskSubject: "Fix bug"}, enabledSettings())

	if _, ok := finalPaneOptions(f, pane)[tmux.PaneOSNotifyTaskCompleted]; !ok {
		t.Error("TaskCompleted must go through HandleNotification and write the anti-duplicate stamp")
	}
}

func TestOnTaskCompletedSetsAttentionNotification(t *testing.T) {
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%TASK_ATTN"
	OnTaskCompleted(pane, "claude", event.Event{Kind: event.TaskCompleted}, enabledSettings())
	if got := finalPaneOptions(f, pane)[tmux.PaneAttention]; got != "notification" {
		t.Errorf("@pane_attention = %q, want notification", got)
	}
}

// ─── OnNotification ─────────────────────────────────────────────────

func TestOnNotificationMetaOnlySkipsStatus(t *testing.T) {
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%NOTIF_META"
	OnNotification(pane, event.Event{Kind: event.Notification, WaitReason: "permission", MetaOnly: true}, enabledSettings())
	final := finalPaneOptions(f, pane)
	if _, ok := final[tmux.PaneStatusOpt]; ok {
		t.Error("meta_only must skip writing status")
	}
	if _, ok := final[tmux.PaneWaitReason]; ok {
		t.Error("meta_only must skip writing wait_reason")
	}
}

func TestOnNotificationWritesWaitingAndWaitReason(t *testing.T) {
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%NOTIF_WAIT"
	OnNotification(pane, event.Event{Kind: event.Notification, WaitReason: "permission"}, enabledSettings())
	final := finalPaneOptions(f, pane)
	if final[tmux.PaneStatusOpt] != string(tmux.StatusWaiting) {
		t.Errorf("@pane_status = %q, want waiting", final[tmux.PaneStatusOpt])
	}
	if final[tmux.PaneWaitReason] != "permission" {
		t.Errorf("@pane_wait_reason = %q, want permission", final[tmux.PaneWaitReason])
	}
}

func TestOnNotificationEmptyWaitReasonClearsOldValue(t *testing.T) {
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%NOTIF_STALE"
	OnNotification(pane, event.Event{Kind: event.Notification, WaitReason: ""}, enabledSettings())
	if _, ok := finalPaneOptions(f, pane)[tmux.PaneWaitReason]; ok {
		t.Error("an empty wait_reason must clear the old value")
	}
}

func TestOnNotificationSoftReasonDowngradesToBackgroundWhenShellPresent(t *testing.T) {
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%NOTIF_BG"
	seed(f, pane, tmux.PaneBgCmd, "cargo test")
	OnNotification(pane, event.Event{Kind: event.Notification, WaitReason: "auth_success"}, enabledSettings())
	if got := finalPaneOptions(f, pane)[tmux.PaneStatusOpt]; got != string(tmux.StatusBackground) {
		t.Errorf("@pane_status = %q, want background", got)
	}
}

func TestOnNotificationPermissionGroupBeatsShell(t *testing.T) {
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%NOTIF_PERM_OVER_BG"
	seed(f, pane, tmux.PaneBgCmd, "cargo test")
	OnNotification(pane, event.Event{Kind: event.Notification, WaitReason: "permission_prompt"}, enabledSettings())
	if got := finalPaneOptions(f, pane)[tmux.PaneStatusOpt]; got != string(tmux.StatusWaiting) {
		t.Errorf("@pane_status = %q, want waiting (permission beats background shell)", got)
	}
}

// ─── OnPermissionDenied ─────────────────────────────────────────────

func TestOnPermissionDeniedWritesWaitingAndReason(t *testing.T) {
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%PD"
	OnPermissionDenied(pane, event.Event{Kind: event.PermissionDenied}, enabledSettings())
	final := finalPaneOptions(f, pane)
	if final[tmux.PaneStatusOpt] != string(tmux.StatusWaiting) {
		t.Errorf("@pane_status = %q, want waiting", final[tmux.PaneStatusOpt])
	}
	if final[tmux.PaneWaitReason] != "permission_denied" {
		t.Errorf("@pane_wait_reason = %q, want permission_denied", final[tmux.PaneWaitReason])
	}
}

// ─── OnTeammateIdle ─────────────────────────────────────────────────

func TestOnTeammateIdleWritesReasonWithName(t *testing.T) {
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%TEAM"
	OnTeammateIdle(pane, event.Event{Kind: event.TeammateIdle, TeammateName: "alice"})
	if got := finalPaneOptions(f, pane)[tmux.PaneWaitReason]; got != "teammate_idle:alice" {
		t.Errorf("@pane_wait_reason = %q, want teammate_idle:alice", got)
	}
}

func TestOnTeammateIdleAppendsReasonWhenPresent(t *testing.T) {
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%TEAM_REASON"
	OnTeammateIdle(pane, event.Event{Kind: event.TeammateIdle, TeammateName: "alice", IdleReason: "tokens_exhausted"})
	if got := finalPaneOptions(f, pane)[tmux.PaneWaitReason]; got != "teammate_idle:alice:tokens_exhausted" {
		t.Errorf("@pane_wait_reason = %q, want teammate_idle:alice:tokens_exhausted", got)
	}
}

// ─── OnSubagentStart / OnSubagentStop ───────────────────────────────

func TestOnSubagentStartAddsToList(t *testing.T) {
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%SUB_START"
	OnSubagentStart(pane, event.Event{Kind: event.SubagentStart, AgentType: "Explore", AgentID: "sub-1"})
	if got := finalPaneOptions(f, pane)[tmux.PaneSubagents]; got != "Explore:sub-1" {
		t.Errorf("@pane_subagents = %q, want Explore:sub-1", got)
	}
}

func TestOnSubagentStartSkippedWhenAgentIDMissing(t *testing.T) {
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%SUB_NO_ID"
	OnSubagentStart(pane, event.Event{Kind: event.SubagentStart, AgentType: "Explore"})
	if _, ok := finalPaneOptions(f, pane)[tmux.PaneSubagents]; ok {
		t.Error("a missing AgentID must not write anything")
	}
}

func TestOnSubagentStopRemovesFromList(t *testing.T) {
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%SUB_STOP"
	seed(f, pane, tmux.PaneSubagents, "Explore:sub-1,Plan:sub-2")
	OnSubagentStop(pane, event.Event{Kind: event.SubagentStop, AgentID: "sub-1"})
	if got := finalPaneOptions(f, pane)[tmux.PaneSubagents]; got != "Plan:sub-2" {
		t.Errorf("@pane_subagents = %q, want Plan:sub-2", got)
	}
}

func TestOnSubagentStopDrainedListUnsetsAndRunsPendingWorktreeRemove(t *testing.T) {
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%SUB_DRAIN_WT"
	seed(f, pane, tmux.PaneSubagents, "Explore:sub-1")
	seed(f, pane, tmux.PanePendingWorktreeRemove, "1")

	OnSubagentStop(pane, event.Event{Kind: event.SubagentStop, AgentID: "sub-1"})

	final := finalPaneOptions(f, pane)
	if _, ok := final[tmux.PaneSubagents]; ok {
		t.Error("@pane_subagents must be cleared once drained")
	}
	if _, ok := final[tmux.PaneWorktreeName]; ok {
		t.Error("a deferred WorktreeRemove teardown must run once the last subagent stops")
	}
}

// ─── OnWorktreeRemove ───────────────────────────────────────────────

func TestOnWorktreeRemoveDefersWhileSubagentAlive(t *testing.T) {
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%WT_DEFER"
	seed(f, pane, tmux.PaneSubagents, "Explore:sub-1")

	OnWorktreeRemove(pane)

	final := finalPaneOptions(f, pane)
	if _, ok := final[tmux.PanePendingWorktreeRemove]; !ok {
		t.Error("must set the deferred marker while a subagent is still present")
	}
	if _, ok := final[tmux.PaneWorktreeName]; ok {
		t.Error("@pane_worktree_name must not be cleared while still deferred")
	}
}

func TestOnWorktreeRemoveRunsImmediatelyWithoutSubagent(t *testing.T) {
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%WT_NOW"
	tmux.SetPaneOption(pane, tmux.PaneWorktreeName, "feat")

	OnWorktreeRemove(pane)

	if _, ok := finalPaneOptions(f, pane)[tmux.PaneWorktreeName]; ok {
		t.Error("@pane_worktree_name must already be cleared")
	}
}

// ─── activityLogStatusTransition ────────────────────────────────────

func TestActivityLogStatusTransitionWaitingToRunning(t *testing.T) {
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%WAIT_TO_RUN"
	seed(f, pane, tmux.PaneStatusOpt, "waiting")

	activityLogStatusTransition(pane, event.Event{Kind: event.ActivityLog, ToolName: "Read"})

	final := finalPaneOptions(f, pane)
	if final[tmux.PaneStatusOpt] != string(tmux.StatusRunning) {
		t.Errorf("@pane_status = %q, want running", final[tmux.PaneStatusOpt])
	}
	if _, ok := final[tmux.PaneAttention]; ok {
		t.Error("@pane_attention must be cleared when leaving waiting")
	}
	if _, ok := final[tmux.PaneWaitReason]; ok {
		t.Error("@pane_wait_reason must be cleared when leaving waiting")
	}
	if _, ok := final[tmux.PaneStartedAt]; !ok {
		t.Error("@pane_started_at must be set when not already present")
	}
}

func TestActivityLogStatusTransitionUnchangedWhenAlreadyRunning(t *testing.T) {
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%ALREADY_RUNNING"
	seed(f, pane, tmux.PaneStatusOpt, "running")
	seed(f, pane, tmux.PaneStartedAt, "1000")

	activityLogStatusTransition(pane, event.Event{Kind: event.ActivityLog, ToolName: "Read"})

	if _, ok := finalPaneOptions(f, pane)[tmux.PaneStatusOpt]; ok {
		t.Error("already running means the status doesn't need to be rewritten")
	}
}

func TestActivityLogStatusTransitionEnterPlanModeBlockedWhileSubagentAlive(t *testing.T) {
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%PARENT_PLAN"
	seed(f, pane, tmux.PaneSubagents, "Explore:sub-1")

	activityLogStatusTransition(pane, event.Event{Kind: event.ActivityLog, ToolName: "EnterPlanMode"})

	if _, ok := finalPaneOptions(f, pane)[tmux.PanePermissionMode]; ok {
		t.Error("a subagent must not change the parent's @pane_permission_mode")
	}
}

func TestActivityLogStatusTransitionExitPlanModeSetsDefault(t *testing.T) {
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	pane := "%EXIT_PLAN"
	activityLogStatusTransition(pane, event.Event{Kind: event.ActivityLog, ToolName: "ExitPlanMode"})
	if got := finalPaneOptions(f, pane)[tmux.PanePermissionMode]; got != "default" {
		t.Errorf("@pane_permission_mode = %q, want default", got)
	}
}
