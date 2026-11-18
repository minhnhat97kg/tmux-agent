package adapter

import (
	"reflect"
	"testing"

	"tmux-agent/internal/event"
)

// TestAntigravityRegistrationTableMatchesParse is a mandatory drift test: exactly two
// entries are correct (see "Gating contract" in AntigravityAdapter's doc
// comment, antigravity.go — do NOT add PreToolUse/PreInvocation/PostInvocation
// without weighing the gating risk).
func TestAntigravityRegistrationTableMatchesParse(t *testing.T) {
	AssertTableDriftFree(t, AntigravityAdapter{})
}

func TestAntigravityRegistrationsChiHaiMuc(t *testing.T) {
	regs := AntigravityAdapter{}.Registrations()
	if len(regs) != 2 {
		t.Fatalf("Registrations() has %d entries, want exactly 2 (PostToolUse, Stop) — see the reasoning in the doc comment", len(regs))
	}
}

func TestAntigravityActivityLogReadsToolCallObject(t *testing.T) {
	got, ok := AntigravityAdapter{}.Parse("activity-log", map[string]any{
		"toolCall": map[string]any{
			"name": "run_command",
			"args": map[string]any{"command": "ls -la"},
		},
		"stepIdx": float64(3),
	})
	if !ok {
		t.Fatal("want ok=true")
	}
	if got.ToolName != "run_command" {
		t.Fatalf("ToolName = %q, want %q", got.ToolName, "run_command")
	}
	if got.ToolInput["command"] != "ls -la" {
		t.Fatalf("ToolInput[command] = %v", got.ToolInput["command"])
	}
	if got.ToolResponse != nil {
		t.Fatalf("ToolResponse = %v, want nil — Antigravity's PostToolUse does not document a result field", got.ToolResponse)
	}
}

// Response MUST always be "{}" — this is a gating contract, not a display
// detail (see doc comment). This test pins the value down rather than
// inferring it from anywhere else.
func TestAntigravityActivityLogResponseAlwaysEmptyJSON(t *testing.T) {
	got, ok := AntigravityAdapter{}.Parse("activity-log", map[string]any{
		"toolCall": map[string]any{"name": "view_file"},
	})
	if !ok {
		t.Fatal("want ok=true")
	}
	if got.Response == nil || *got.Response != "{}" {
		t.Fatalf("Response = %v, want pointer to \"{}\"", got.Response)
	}
}

func TestAntigravityActivityLogEmptyToolCallRejected(t *testing.T) {
	_, ok := AntigravityAdapter{}.Parse("activity-log", map[string]any{})
	if ok {
		t.Fatal("want ok=false when toolCall/name is empty or missing")
	}
	_, ok = AntigravityAdapter{}.Parse("activity-log", map[string]any{
		"toolCall": map[string]any{},
	})
	if ok {
		t.Fatal("want ok=false when toolCall.name is empty")
	}
}

func TestAntigravityStopReadsWorkspacePathsAndConversationId(t *testing.T) {
	got, ok := AntigravityAdapter{}.Parse("stop", map[string]any{
		"conversationId":    "conv-42",
		"workspacePaths":    []any{"/home/user/project", "/home/user/other"},
		"terminationReason": "completed",
		"fullyIdle":         true,
	})
	if !ok {
		t.Fatal("want ok=true")
	}
	want := event.Event{
		Kind:      event.Stop,
		Agent:     "antigravity",
		Cwd:       "/home/user/project",
		SessionID: "conv-42",
	}
	// Compare Response (a pointer) separately, then clear it so
	// reflect.DeepEqual can check the rest.
	if got.Response == nil || *got.Response != `{"decision":"continue"}` {
		t.Fatalf("Response = %v, want pointer to %q", got.Response, `{"decision":"continue"}`)
	}
	got.Response = nil
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestAntigravityStopMissingWorkspacePathsEmptyCwd(t *testing.T) {
	got, ok := AntigravityAdapter{}.Parse("stop", map[string]any{"conversationId": "c1"})
	if !ok {
		t.Fatal("want ok=true")
	}
	if got.Cwd != "" {
		t.Fatalf("Cwd = %q, want empty when workspacePaths is missing", got.Cwd)
	}
}

func TestAntigravityStopEmptyWorkspacePathsArrayEmptyCwd(t *testing.T) {
	got, ok := AntigravityAdapter{}.Parse("stop", map[string]any{
		"workspacePaths": []any{},
	})
	if !ok {
		t.Fatal("want ok=true")
	}
	if got.Cwd != "" {
		t.Fatalf("Cwd = %q, want empty when workspacePaths is an empty array", got.Cwd)
	}
}

// Antigravity does NOT have SessionStart/UserPromptSubmit/SessionEnd/
// Notification/etc. — see AntigravityAdapter's doc comment. Parse() must
// return ok=false for all of them, without panicking, including the three
// gating hooks DELIBERATELY NOT registered (pretool-use/pre-invocation/
// post-invocation are not valid external names since KindFromExternalName
// has no corresponding Kind for them).
func TestAntigravityUnsupportedEvents(t *testing.T) {
	unsupportedNames := []string{
		"session-start",
		"session-end",
		"user-prompt-submit",
		"notification",
		"stop-failure",
		"permission-denied",
		"cwd-changed",
		"subagent-start",
		"subagent-stop",
		"task-created",
		"task-completed",
		"teammate-idle",
		"worktree-create",
		"worktree-remove",
		"something-else",
	}
	for _, name := range unsupportedNames {
		t.Run(name, func(t *testing.T) {
			_, ok := AntigravityAdapter{}.Parse(name, map[string]any{})
			if ok {
				t.Fatalf("Parse(%q) returned ok=true, want false", name)
			}
		})
	}
}

// A nil payload (no stdin) must not make Parse panic.
func TestAntigravityParsePayloadNilDoesNotPanic(t *testing.T) {
	supported := []string{"activity-log", "stop"}
	for _, name := range supported {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Parse(%q, nil) panicked: %v", name, r)
				}
			}()
			AntigravityAdapter{}.Parse(name, nil)
		})
	}
}
