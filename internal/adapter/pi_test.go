package adapter

import (
	"reflect"
	"testing"

	"tmux-agent/internal/event"
)

// TestPiRegistrationTableMatchesParse is a mandatory drift test (see part 2 of the
// contract): Registrations() and Parse() must agree in both directions.
func TestPiRegistrationTableMatchesParse(t *testing.T) {
	AssertTableDriftFree(t, PiAdapter{})
}

func TestPiSessionStart(t *testing.T) {
	got, ok := PiAdapter{}.Parse("session-start", map[string]any{
		"cwd":        "/home/user",
		"source":     "startup",
		"session_id": "sess-pi-1",
	})
	if !ok {
		t.Fatal("want ok=true")
	}
	want := event.Event{
		Kind:      event.SessionStart,
		Agent:     "pi",
		Cwd:       "/home/user",
		Source:    "startup",
		SessionID: "sess-pi-1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestPiSessionStartMissingFieldsDefaultToEmpty(t *testing.T) {
	got, ok := PiAdapter{}.Parse("session-start", map[string]any{})
	if !ok {
		t.Fatal("want ok=true")
	}
	want := event.Event{Kind: event.SessionStart, Agent: "pi"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// Pi's SessionEnd reads the "reason" field, NOT "end_reason" like Claude —
// pin down the correct field name per the pi-hooks docs (differs from
// Claude, easy to typo).
func TestPiSessionEndReadsReasonFieldNotEndReason(t *testing.T) {
	got, ok := PiAdapter{}.Parse("session-end", map[string]any{
		"reason":     "exit",
		"end_reason": "wrong-field-name-must-not-be-read",
	})
	if !ok {
		t.Fatal("want ok=true")
	}
	if got.EndReason != "exit" {
		t.Fatalf("EndReason = %q, want %q (read from \"reason\", not \"end_reason\")", got.EndReason, "exit")
	}
}

func TestPiUserPromptSubmit(t *testing.T) {
	got, ok := PiAdapter{}.Parse("user-prompt-submit", map[string]any{
		"cwd":        "/tmp",
		"prompt":     "hello",
		"session_id": "sess-pi-2",
	})
	if !ok {
		t.Fatal("want ok=true")
	}
	want := event.Event{
		Kind:      event.UserPromptSubmit,
		Agent:     "pi",
		Cwd:       "/tmp",
		Prompt:    "hello",
		SessionID: "sess-pi-2",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// Unlike Codex: Pi's Stop does NOT have a fixed Response — there is no
// documentation showing Pi's hook engine reads stdout back to decide whether
// to continue the turn.
func TestPiStopResponseAlwaysNil(t *testing.T) {
	got, ok := PiAdapter{}.Parse("stop", map[string]any{
		"cwd":                    "/tmp",
		"last_assistant_message": "done",
		"session_id":             "sess-pi-3",
	})
	if !ok {
		t.Fatal("want ok=true")
	}
	if got.Response != nil {
		t.Fatalf("Response = %v, want nil", *got.Response)
	}
	want := event.Event{
		Kind:        event.Stop,
		Agent:       "pi",
		Cwd:         "/tmp",
		LastMessage: "done",
		SessionID:   "sess-pi-3",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestPiActivityLogBashCommand(t *testing.T) {
	got, ok := PiAdapter{}.Parse("activity-log", map[string]any{
		"tool_name":     "Bash",
		"tool_input":    map[string]any{"command": "ls -la"},
		"tool_response": map[string]any{"stdout": "file.txt\n"},
	})
	if !ok {
		t.Fatal("want ok=true")
	}
	if got.ToolName != "Bash" {
		t.Fatalf("ToolName = %q", got.ToolName)
	}
	if got.ToolInput["command"] != "ls -la" {
		t.Fatalf("ToolInput[command] = %v", got.ToolInput["command"])
	}
}

func TestPiActivityLogEmptyToolNameRejected(t *testing.T) {
	_, ok := PiAdapter{}.Parse("activity-log", map[string]any{})
	if ok {
		t.Fatal("want ok=false when tool_name is empty/missing")
	}
}

// Pi LACKS (per what's documented — see PiAdapter's doc comment, pi.go):
// Notification, StopFailure, PermissionDenied, CwdChanged, SubagentStart/
// SubagentStop, TaskCreated/TaskCompleted, TeammateIdle. Parse() must return
// ok=false for all of them, without panicking.
func TestPiUnsupportedEvents(t *testing.T) {
	unsupportedNames := []string{
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
			_, ok := PiAdapter{}.Parse(name, map[string]any{})
			if ok {
				t.Fatalf("Parse(%q) returned ok=true, want false", name)
			}
		})
	}
}

// A nil payload (no stdin) must not make Parse panic, for every external
// name Pi supports.
func TestPiParsePayloadNilDoesNotPanic(t *testing.T) {
	supported := []string{"session-start", "session-end", "user-prompt-submit", "stop", "activity-log"}
	for _, name := range supported {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Parse(%q, nil) panicked: %v", name, r)
				}
			}()
			PiAdapter{}.Parse(name, nil)
		})
	}
}
