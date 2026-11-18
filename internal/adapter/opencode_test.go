package adapter

import (
	"reflect"
	"testing"

	"tmux-agent/internal/event"
)

// The tests below are ported from tas/src/adapter/opencode.rs (the
// #[cfg(test)] section), plus a few extra edge cases (clearly marked in the
// function names) that confirm the fields OpenCode "hard-codes empty" are
// never overwritten by a foreign payload, and that an empty tool_name is
// safely ignored.

func TestOpenCodeSessionStart(t *testing.T) {
	a := OpenCodeAdapter{}
	got, ok := a.Parse("session-start", map[string]any{
		"cwd":        "/tmp",
		"session_id": "ses-1",
		"source":     "startup",
	})
	if !ok {
		t.Fatal("want ok=true")
	}
	want := event.Event{
		Kind:      event.SessionStart,
		Agent:     "opencode",
		Cwd:       "/tmp",
		SessionID: "ses-1",
		Source:    "startup",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestOpenCodeUserPromptSubmitMissingSessionID(t *testing.T) {
	a := OpenCodeAdapter{}
	got, ok := a.Parse("user-prompt-submit", map[string]any{
		"cwd":    "/tmp",
		"prompt": "hello",
	})
	if !ok {
		t.Fatal("want ok=true")
	}
	want := event.Event{
		Kind:   event.UserPromptSubmit,
		Agent:  "opencode",
		Cwd:    "/tmp",
		Prompt: "hello",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestOpenCodeNotification(t *testing.T) {
	a := OpenCodeAdapter{}
	got, ok := a.Parse("notification", map[string]any{
		"cwd":         "/tmp",
		"session_id":  "ses-1",
		"wait_reason": "permission",
	})
	if !ok {
		t.Fatal("want ok=true")
	}
	want := event.Event{
		Kind:       event.Notification,
		Agent:      "opencode",
		Cwd:        "/tmp",
		SessionID:  "ses-1",
		WaitReason: "permission",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestOpenCodeStop(t *testing.T) {
	a := OpenCodeAdapter{}
	got, ok := a.Parse("stop", map[string]any{
		"cwd":          "/tmp",
		"session_id":   "ses-1",
		"last_message": "",
	})
	if !ok {
		t.Fatal("want ok=true")
	}
	want := event.Event{
		Kind:      event.Stop,
		Agent:     "opencode",
		Cwd:       "/tmp",
		SessionID: "ses-1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestOpenCodeStopFailure(t *testing.T) {
	a := OpenCodeAdapter{}
	got, ok := a.Parse("stop-failure", map[string]any{
		"cwd":        "/tmp",
		"error":      "boom",
		"session_id": "ses-1",
	})
	if !ok {
		t.Fatal("want ok=true")
	}
	want := event.Event{
		Kind:      event.StopFailure,
		Agent:     "opencode",
		Cwd:       "/tmp",
		SessionID: "ses-1",
		Error:     "boom",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestOpenCodeActivityLogNormalizesPlainBash(t *testing.T) {
	a := OpenCodeAdapter{}
	got, ok := a.Parse("activity-log", map[string]any{
		"tool_name":     "bash",
		"tool_input":    map[string]any{"command": "ls"},
		"tool_response": map[string]any{"stdout": "file.txt"},
	})
	if !ok {
		t.Fatal("want ok=true")
	}
	want := event.Event{
		Kind:         event.ActivityLog,
		ToolName:     "Bash",
		ToolInput:    map[string]any{"command": "ls"},
		ToolResponse: map[string]any{"stdout": "file.txt"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestOpenCodeActivityLogPreservesRunInBackgroundFlag(t *testing.T) {
	a := OpenCodeAdapter{}
	got, ok := a.Parse("activity-log", map[string]any{
		"tool_name": "bash",
		"tool_input": map[string]any{
			"command":         "npm run dev",
			"runInBackground": true,
		},
	})
	if !ok {
		t.Fatal("want ok=true")
	}
	want := event.Event{
		Kind:     event.ActivityLog,
		ToolName: "Bash",
		ToolInput: map[string]any{
			"command":         "npm run dev",
			"runInBackground": true,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestOpenCodeActivityLogNormalizesFilePathKeyForRead(t *testing.T) {
	a := OpenCodeAdapter{}
	got, ok := a.Parse("activity-log", map[string]any{
		"tool_name":  "read",
		"tool_input": map[string]any{"filePath": "/home/user/src/main.go"},
	})
	if !ok {
		t.Fatal("want ok=true")
	}
	want := event.Event{
		Kind:     event.ActivityLog,
		ToolName: "Read",
		ToolInput: map[string]any{
			"filePath":  "/home/user/src/main.go",
			"file_path": "/home/user/src/main.go",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestOpenCodeActivityLogUnknownToolNotNormalized(t *testing.T) {
	a := OpenCodeAdapter{}
	got, ok := a.Parse("activity-log", map[string]any{
		"tool_name":  "custom-mcp-tool",
		"tool_input": map[string]any{"foo": "bar"},
	})
	if !ok {
		t.Fatal("want ok=true")
	}
	want := event.Event{
		Kind:      event.ActivityLog,
		ToolName:  "custom-mcp-tool",
		ToolInput: map[string]any{"foo": "bar"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestOpenCodeActivityLogMultieditBecomesEdit(t *testing.T) {
	a := OpenCodeAdapter{}
	got, ok := a.Parse("activity-log", map[string]any{
		"tool_name":  "multiedit",
		"tool_input": map[string]any{"filePath": "/a/b.go"},
	})
	if !ok {
		t.Fatal("want ok=true")
	}
	want := event.Event{
		Kind:     event.ActivityLog,
		ToolName: "Edit",
		ToolInput: map[string]any{
			"filePath":  "/a/b.go",
			"file_path": "/a/b.go",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestOpenCodeActivityLogMissingToolNameIgnored(t *testing.T) {
	a := OpenCodeAdapter{}
	_, ok := a.Parse("activity-log", map[string]any{
		"tool_input": map[string]any{"foo": "bar"},
	})
	if ok {
		t.Fatal("ok = true, want false — an empty tool_name must be ignored, without panicking or creating an empty event")
	}
}

func TestOpenCodeActivityLogIgnoresCwdAndSessionID(t *testing.T) {
	// Documents a mismatch found between the plugin JS and the adapter: the
	// plugin sends cwd and session_id along in the tool.execute.after
	// payload, but ActivityLog has nowhere to put those two fields — they
	// must be ignored, without panicking and without leaking into Cwd/
	// SessionID (which are already zero-valued for this Kind).
	a := OpenCodeAdapter{}
	got, ok := a.Parse("activity-log", map[string]any{
		"cwd":        "/should/be/ignored",
		"session_id": "ses-should-be-ignored",
		"tool_name":  "bash",
	})
	if !ok {
		t.Fatal("want ok=true")
	}
	want := event.Event{
		Kind:     event.ActivityLog,
		ToolName: "Bash",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestOpenCodeUnknownEventsNotRecognized(t *testing.T) {
	a := OpenCodeAdapter{}
	_, ok := a.Parse("does-not-exist", map[string]any{"cwd": "/tmp"})
	if ok {
		t.Fatal("ok = true, want false for an unknown event name")
	}
}

func TestOpenCodeDoesNotReadWorktreeOrAgentIDEvenIfPayloadHasThem(t *testing.T) {
	// The Rust implementation hard-codes worktree=None, agent_id=None for
	// every OpenCode branch — even if the payload stuffs those keys in,
	// Parse must not read them.
	a := OpenCodeAdapter{}
	got, ok := a.Parse("session-start", map[string]any{
		"cwd":      "/tmp",
		"worktree": map[string]any{"name": "wt1"},
		"agent_id": "agent-xyz",
	})
	if !ok {
		t.Fatal("want ok=true")
	}
	want := event.Event{
		Kind:  event.SessionStart,
		Agent: "opencode",
		Cwd:   "/tmp",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestOpenCodeName(t *testing.T) {
	if got := (OpenCodeAdapter{}).Name(); got != "opencode" {
		t.Fatalf("Name() = %q, want %q", got, "opencode")
	}
}

// The most valuable test: the registration table and Parse() must not drift
// apart in either direction. Uses the shared helper already written for the
// adapter helper task.
func TestOpenCodeRegistrationTableDoesNotDriftFromParse(t *testing.T) {
	AssertTableDriftFree(t, OpenCodeAdapter{})
}
