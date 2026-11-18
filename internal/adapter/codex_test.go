package adapter

import (
	"reflect"
	"testing"

	"tmux-agent/internal/event"
)

// TestCodexRegistrationTableMatchesParse is a mandatory drift test (see part 2 of the
// contract): Registrations() and Parse() must agree in both directions.
func TestCodexRegistrationTableMatchesParse(t *testing.T) {
	AssertTableDriftFree(t, CodexAdapter{})
}

func TestCodexSessionStart(t *testing.T) {
	got, ok := CodexAdapter{}.Parse("session-start", map[string]any{
		"cwd":        "/home/user",
		"session_id": "sess-codex-1",
	})
	if !ok {
		t.Fatal("want ok=true")
	}
	want := event.Event{
		Kind:      event.SessionStart,
		Agent:     "codex",
		Cwd:       "/home/user",
		SessionID: "sess-codex-1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestCodexSessionStartStoresSource(t *testing.T) {
	got, ok := CodexAdapter{}.Parse("session-start", map[string]any{
		"cwd":    "/tmp",
		"source": "resume",
	})
	if !ok {
		t.Fatal("want ok=true")
	}
	if got.Source != "resume" {
		t.Fatalf("Source = %q, want %q", got.Source, "resume")
	}
}

func TestCodexSessionStartHasNoWorktree(t *testing.T) {
	got, ok := CodexAdapter{}.Parse("session-start", map[string]any{"cwd": "/tmp"})
	if !ok {
		t.Fatal("want ok=true")
	}
	if got.Worktree != nil {
		t.Fatalf("Worktree = %+v, want nil", got.Worktree)
	}
	if got.AgentID != "" {
		t.Fatalf("AgentID = %q, want empty", got.AgentID)
	}
}

func TestCodexSessionStartMissingFieldsDefaultToEmpty(t *testing.T) {
	got, ok := CodexAdapter{}.Parse("session-start", map[string]any{})
	if !ok {
		t.Fatal("want ok=true")
	}
	want := event.Event{
		Kind:  event.SessionStart,
		Agent: "codex",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestCodexUserPromptSubmit(t *testing.T) {
	got, ok := CodexAdapter{}.Parse("user-prompt-submit", map[string]any{
		"cwd":        "/tmp",
		"prompt":     "hello",
		"session_id": "sess-codex-2",
	})
	if !ok {
		t.Fatal("want ok=true")
	}
	want := event.Event{
		Kind:      event.UserPromptSubmit,
		Agent:     "codex",
		Cwd:       "/tmp",
		Prompt:    "hello",
		SessionID: "sess-codex-2",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestCodexStopHasResponseContinue(t *testing.T) {
	got, ok := CodexAdapter{}.Parse("stop", map[string]any{
		"cwd":                    "/tmp",
		"last_assistant_message": "done",
		"session_id":             "sess-codex-3",
	})
	if !ok {
		t.Fatal("want ok=true")
	}
	wantResp := `{"continue":true}`
	want := event.Event{
		Kind:        event.Stop,
		Agent:       "codex",
		Cwd:         "/tmp",
		LastMessage: "done",
		Response:    &wantResp,
		SessionID:   "sess-codex-3",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// Realistic payload matching the upstream schema (stop.command.input.schema.json),
// with extra fields the adapter doesn't use. permission_mode IS present in the
// payload but the adapter DELIBERATELY does not read it — see Codex's doc
// comment in codex.go. This is a deliberate difference from the reference Rust
// implementation.
func TestCodexStopReadsRealUpstreamSchemaPayload(t *testing.T) {
	got, ok := CodexAdapter{}.Parse("stop", map[string]any{
		"hook_event_name":        "Stop",
		"cwd":                    "/home/user/project",
		"session_id":             "01HXYZABCDEF0123456789",
		"model":                  "gpt-5-codex",
		"permission_mode":        "default",
		"last_assistant_message": "all tests pass",
		"stop_hook_active":       false,
		"transcript_path":        nil,
		"turn_id":                "turn-42",
	})
	if !ok {
		t.Fatal("want ok=true")
	}
	if got.SessionID != "01HXYZABCDEF0123456789" {
		t.Fatalf("SessionID = %q", got.SessionID)
	}
	if got.LastMessage != "all tests pass" {
		t.Fatalf("LastMessage = %q", got.LastMessage)
	}
	if got.PermissionMode != "" {
		t.Fatalf("PermissionMode = %q, want empty — the adapter does not read this field", got.PermissionMode)
	}
}

func TestCodexStopMissingSessionIdReturnsEmpty(t *testing.T) {
	got, ok := CodexAdapter{}.Parse("stop", map[string]any{})
	if !ok {
		t.Fatal("want ok=true")
	}
	wantResp := `{"continue":true}`
	want := event.Event{
		Kind:     event.Stop,
		Agent:    "codex",
		Response: &wantResp,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestCodexActivityLogBashCommand(t *testing.T) {
	got, ok := CodexAdapter{}.Parse("activity-log", map[string]any{
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

func TestCodexActivityLogEmptyToolNameRejected(t *testing.T) {
	_, ok := CodexAdapter{}.Parse("activity-log", map[string]any{})
	if ok {
		t.Fatal("want ok=false when tool_name is empty/missing")
	}
}

// External names Codex does NOT support: Codex's hook engine either has no
// corresponding concept (SessionEnd, Notification, ...) or the event name
// doesn't exist at all. Parse() must return ok=false for all of them, without
// panicking.
func TestCodexUnsupportedEvents(t *testing.T) {
	unsupportedNames := []string{
		"session-end", // Codex has no reliable exit hook
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
		"something-else", // unknown name, not in event.All
	}
	for _, name := range unsupportedNames {
		t.Run(name, func(t *testing.T) {
			_, ok := CodexAdapter{}.Parse(name, map[string]any{})
			if ok {
				t.Fatalf("Parse(%q) returned ok=true, want false", name)
			}
		})
	}
}

// A nil payload (no stdin) must not make Parse panic, for every external name
// Codex supports.
func TestCodexParsePayloadNilDoesNotPanic(t *testing.T) {
	supported := []string{"session-start", "user-prompt-submit", "stop", "activity-log"}
	for _, name := range supported {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Parse(%q, nil) panicked: %v", name, r)
				}
			}()
			CodexAdapter{}.Parse(name, nil)
		})
	}
}
