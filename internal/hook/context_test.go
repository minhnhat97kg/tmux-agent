package hook

import (
	"testing"

	"tmux-agent/internal/event"
	"tmux-agent/internal/tmux"
)

func TestNewAgentContextCopiesFields(t *testing.T) {
	ctx := NewAgentContext("claude", "/tmp", "auto", nil, "")
	if ctx.Agent != "claude" || ctx.Cwd != "/tmp" || ctx.PermissionMode != "auto" {
		t.Fatalf("ctx = %+v", ctx)
	}
	if ctx.Worktree != nil {
		t.Error("Worktree must be nil")
	}
	if ctx.SessionID != "" {
		t.Error("SessionID must be empty")
	}
}

func TestNewAgentContextPreservesWorktreeAndSessionID(t *testing.T) {
	wt := &event.WorktreeInfo{
		Name:            "feat",
		Path:            "/tmp/wt",
		Branch:          "feature/x",
		OriginalRepoDir: "/home/user/repo",
	}
	ctx := NewAgentContext("codex", "/src", "plan", wt, "sess-abc")
	if ctx.Agent != "codex" || ctx.Cwd != "/src" || ctx.PermissionMode != "plan" {
		t.Fatalf("ctx = %+v", ctx)
	}
	if ctx.Worktree == nil || ctx.Worktree.Name != "feat" {
		t.Fatalf("Worktree = %+v", ctx.Worktree)
	}
	if ctx.SessionID != "sess-abc" {
		t.Errorf("SessionID = %q, want %q", ctx.SessionID, "sess-abc")
	}
}

func TestSetAgentMetaDoesNotOverwriteParentPermissionWhileSubagentRunning(t *testing.T) {
	fake := tmux.NewFake()
	pane := "%PARENT_PERM"
	// the parent pane is running a subagent — every read of @pane_subagents
	// returns a non-empty list.
	fake.Set("Explore:sub-1", "show", "-t", pane, "-pv", tmux.PaneSubagents)
	defer tmux.SetRunner(fake)()

	// A hook from the SUBAGENT arrives with permission_mode "default" — it
	// must not flip the parent's permission badge (assumed to be "plan", but
	// this test only asserts that NO call writing @pane_permission_mode is
	// made, since SetAgentMeta doesn't itself read the old value to compare).
	ctx := NewAgentContext("claude", "/repo", "default", nil, "")
	SetAgentMeta(pane, ctx)

	for _, call := range fake.Calls() {
		if len(call) >= 5 && call[0] == "set" && call[4] == tmux.PanePermissionMode {
			t.Fatalf("a child subagent hook must not write @pane_permission_mode, got call: %v", call)
		}
	}
}

func TestSetAgentMetaWritesPermissionWhenNoSubagent(t *testing.T) {
	fake := tmux.NewFake()
	pane := "%LONE_PERM"
	fake.Set("", "show", "-t", pane, "-pv", tmux.PaneSubagents)
	defer tmux.SetRunner(fake)()

	ctx := NewAgentContext("claude", "/repo", "plan", nil, "")
	SetAgentMeta(pane, ctx)

	found := false
	for _, call := range fake.Calls() {
		if len(call) == 6 && call[0] == "set" && call[4] == tmux.PanePermissionMode && call[5] == "plan" {
			found = true
		}
	}
	if !found {
		t.Errorf("a normal SessionStart (no subagent) must write @pane_permission_mode=plan; calls: %v", fake.Calls())
	}
}

func TestClearRunStateClearsStartedAtAndWaitReason(t *testing.T) {
	fake := tmux.NewFake()
	pane := "%CLEAR_RUN"
	defer tmux.SetRunner(fake)()

	ClearRunState(pane)

	calls := fake.Calls()
	if len(calls) != 2 {
		t.Fatalf("want 2 unset calls, got %d: %v", len(calls), calls)
	}
	assertUnsetCall(t, calls[0], pane, tmux.PaneStartedAt)
	assertUnsetCall(t, calls[1], pane, tmux.PaneWaitReason)
}

func TestIsSystemMessageTaskNotification(t *testing.T) {
	if !IsSystemMessage("<task-notification><task-id>abc</task-id></task-notification>") {
		t.Error("want true")
	}
}

func TestIsSystemMessageSystemReminder(t *testing.T) {
	if !IsSystemMessage("<system-reminder>some reminder</system-reminder>") {
		t.Error("want true")
	}
}

func TestIsSystemMessageTaskIDPrefix(t *testing.T) {
	if !IsSystemMessage("<task-id>abc</task-id>") {
		t.Error("want true")
	}
}

func TestIsSystemMessageNormalPrompt(t *testing.T) {
	if IsSystemMessage("fix the bug") {
		t.Error("want false")
	}
}

func TestIsSystemMessageEmpty(t *testing.T) {
	if IsSystemMessage("") {
		t.Error("want false")
	}
}

func TestIsSystemMessageMixedContent(t *testing.T) {
	if !IsSystemMessage("hello <system-reminder>noise</system-reminder> world") {
		t.Error("want true")
	}
}

// Real user feedback: the sidebar was displaying the raw
// `<agent-message from="...">...` wrapper verbatim on the "what's it doing"
// line when another agent (teammate/subagent) sends a message to this pane —
// UserPromptSubmit receives evt.Prompt as that raw envelope, and
// IsSystemMessage previously didn't recognize it, so OnUserPromptSubmit wrote
// it straight into @pane_prompt.
func TestIsSystemMessageAgentMessage(t *testing.T) {
	if !IsSystemMessage(`<agent-message from="a1a28f...">some content</agent-message>`) {
		t.Error("want true")
	}
}

// clearAllMeta isn't exported, but it's tested from the same package to lock
// down the exact list of 15 keys it clears — RunSessionEndTeardown
// (pending_test.go) adds 2 more keys (attention, status) to reach the full
// 16, matching clearedPaneKeys in internal/tmux/query.go.
func TestClearAllMetaClearsAll15Keys(t *testing.T) {
	fake := tmux.NewFake()
	pane := "%CLEAR_ALL"
	defer tmux.SetRunner(fake)()

	clearAllMeta(pane)

	wantKeys := []string{
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
		tmux.PaneStartedAt,
		tmux.PaneWaitReason,
	}
	calls := fake.Calls()
	if len(calls) != len(wantKeys) {
		t.Fatalf("want %d unset calls, got %d: %v", len(wantKeys), len(calls), calls)
	}
	for i, key := range wantKeys {
		assertUnsetCall(t, calls[i], pane, key)
	}
}
