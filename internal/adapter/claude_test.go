package adapter

import (
	"reflect"
	"testing"

	"tmux-agent/internal/event"
)

func TestClaudeAdapterName(t *testing.T) {
	if got := (ClaudeAdapter{}).Name(); got != "claude" {
		t.Fatalf("Name() = %q, want %q", got, "claude")
	}
}

// The most valuable test in this file: locks the registration table and
// Parse() so they cannot drift apart in either direction.
func TestClaudeHookRegistrationsDriftFree(t *testing.T) {
	AssertTableDriftFree(t, ClaudeAdapter{})
}

// Pins down the PostToolUse → ActivityLog entry specifically — the ONLY entry
// in the table whose trigger name differs from its internal Kind name. Also
// pins the trigger count (14) so the table documented in section 1 cannot
// silently drift from the code.
func TestClaudeRegistrationsPostToolUseMapsToActivityLog(t *testing.T) {
	regs := (ClaudeAdapter{}).Registrations()
	if len(regs) != 14 {
		t.Fatalf("len(Registrations()) = %d, want 14", len(regs))
	}
	found := false
	for _, r := range regs {
		if r.Trigger == "PostToolUse" {
			found = true
			if r.Kind != event.ActivityLog {
				t.Fatalf("PostToolUse maps to %q, want %q", r.Kind, event.ActivityLog)
			}
		}
	}
	if !found {
		t.Fatal("PostToolUse trigger not found in the registration table")
	}
}

func TestClaudeSessionStart(t *testing.T) {
	a := ClaudeAdapter{}
	got, ok := a.Parse("session-start", map[string]any{
		"cwd": "/home/user", "permission_mode": "default",
	})
	if !ok {
		t.Fatal("ok = false, want true")
	}
	want := event.Event{
		Kind: event.SessionStart, Agent: "claude",
		Cwd: "/home/user", PermissionMode: "default",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got = %+v\nwant = %+v", got, want)
	}
}

func TestClaudeSessionStartMissingFieldsDefaultToEmpty(t *testing.T) {
	a := ClaudeAdapter{}
	got, ok := a.Parse("session-start", map[string]any{})
	if !ok {
		t.Fatal("ok = false, want true")
	}
	want := event.Event{Kind: event.SessionStart, Agent: "claude"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got = %+v\nwant = %+v", got, want)
	}
}

func TestClaudeSessionStartCapturesSource(t *testing.T) {
	a := ClaudeAdapter{}
	got, ok := a.Parse("session-start", map[string]any{
		"cwd": "/home/user", "permission_mode": "default", "source": "resume",
	})
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if got.Source != "resume" {
		t.Fatalf("Source = %q, want %q", got.Source, "resume")
	}
}

func TestClaudeSessionStartWithWorktreeAndAgentID(t *testing.T) {
	a := ClaudeAdapter{}
	got, ok := a.Parse("session-start", map[string]any{
		"cwd": "/tmp/wt", "permission_mode": "auto", "agent_id": "abc-123",
		"worktree": map[string]any{
			"name": "feat-wt", "path": "/tmp/wt", "branch": "feat",
			"originalRepoDir": "/home/user/repo",
		},
	})
	if !ok {
		t.Fatal("ok = false, want true")
	}
	want := event.Event{
		Kind: event.SessionStart, Agent: "claude",
		Cwd: "/tmp/wt", PermissionMode: "auto", AgentID: "abc-123",
		Worktree: &event.WorktreeInfo{
			Name: "feat-wt", Path: "/tmp/wt", Branch: "feat",
			OriginalRepoDir: "/home/user/repo",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got = %+v\nwant = %+v", got, want)
	}
}

func TestClaudeParseWorktreeEmptyObjectReturnsNone(t *testing.T) {
	a := ClaudeAdapter{}
	got, ok := a.Parse("session-start", map[string]any{
		"cwd": "/tmp", "permission_mode": "default",
		"worktree": map[string]any{},
	})
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if got.Worktree != nil {
		t.Fatalf("Worktree = %+v, want nil when the object is empty", got.Worktree)
	}
}

func TestClaudeParseWorktreeNonObjectReturnsNone(t *testing.T) {
	a := ClaudeAdapter{}
	got, ok := a.Parse("session-start", map[string]any{
		"cwd": "/tmp", "permission_mode": "default",
		"worktree": "not-an-object",
	})
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if got.Worktree != nil {
		t.Fatalf("Worktree = %+v, want nil when worktree is not an object", got.Worktree)
	}
}

func TestClaudeSessionEnd(t *testing.T) {
	a := ClaudeAdapter{}

	t.Run("no_reason", func(t *testing.T) {
		got, ok := a.Parse("session-end", map[string]any{})
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := event.Event{Kind: event.SessionEnd}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got = %+v\nwant = %+v", got, want)
		}
	})

	t.Run("has_reason", func(t *testing.T) {
		got, ok := a.Parse("session-end", map[string]any{"end_reason": "logout"})
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := event.Event{Kind: event.SessionEnd, EndReason: "logout"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got = %+v\nwant = %+v", got, want)
		}
	})
}

func TestClaudeUserPromptSubmit(t *testing.T) {
	a := ClaudeAdapter{}
	got, ok := a.Parse("user-prompt-submit", map[string]any{
		"cwd": "/tmp", "permission_mode": "auto", "prompt": "fix bug",
	})
	if !ok {
		t.Fatal("ok = false, want true")
	}
	want := event.Event{
		Kind: event.UserPromptSubmit, Agent: "claude",
		Cwd: "/tmp", PermissionMode: "auto", Prompt: "fix bug",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got = %+v\nwant = %+v", got, want)
	}
}

func TestClaudeNotification(t *testing.T) {
	a := ClaudeAdapter{}

	t.Run("normal_not_meta_only", func(t *testing.T) {
		got, ok := a.Parse("notification", map[string]any{
			"cwd": "/tmp", "permission_mode": "default", "notification_type": "permission",
		})
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := event.Event{
			Kind: event.Notification, Agent: "claude",
			Cwd: "/tmp", PermissionMode: "default",
			WaitReason: "permission", MetaOnly: false,
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got = %+v\nwant = %+v", got, want)
		}
	})

	t.Run("idle_prompt_is_meta_only", func(t *testing.T) {
		got, ok := a.Parse("notification", map[string]any{
			"cwd": "/tmp", "permission_mode": "default", "notification_type": "idle_prompt",
		})
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.WaitReason != "idle_prompt" || !got.MetaOnly {
			t.Fatalf("WaitReason/MetaOnly = %q/%v, want idle_prompt/true", got.WaitReason, got.MetaOnly)
		}
	})

	t.Run("missing_notification_type", func(t *testing.T) {
		got, ok := a.Parse("notification", map[string]any{
			"cwd": "/tmp", "permission_mode": "default",
		})
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.WaitReason != "" || got.MetaOnly {
			t.Fatalf("WaitReason/MetaOnly = %q/%v, want empty/false", got.WaitReason, got.MetaOnly)
		}
	})
}

func TestClaudeStop(t *testing.T) {
	a := ClaudeAdapter{}

	t.Run("has_last_message", func(t *testing.T) {
		got, ok := a.Parse("stop", map[string]any{
			"cwd": "/tmp", "permission_mode": "default", "last_assistant_message": "done",
		})
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := event.Event{
			Kind: event.Stop, Agent: "claude",
			Cwd: "/tmp", PermissionMode: "default", LastMessage: "done",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got = %+v\nwant = %+v", got, want)
		}
		if got.Response != nil {
			t.Fatalf("Response = %v, want nil (upstream does not derive this field yet)", *got.Response)
		}
	})

	t.Run("missing_last_message", func(t *testing.T) {
		got, ok := a.Parse("stop", map[string]any{"cwd": "/tmp", "permission_mode": "default"})
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.LastMessage != "" {
			t.Fatalf("LastMessage = %q, want empty", got.LastMessage)
		}
	})
}

// Merges 8 error-priority tests from tests.rs into one table — see section 4
// of the docs for why they were merged and which case was dropped.
func TestClaudeStopFailureErrorPriority(t *testing.T) {
	a := ClaudeAdapter{}
	base := map[string]any{"cwd": "/tmp", "permission_mode": "default"}

	cases := []struct {
		name      string
		extra     map[string]any
		wantError string
	}{
		{
			name:      "error_type wins over error/error_message/error_details all at once",
			extra:     map[string]any{"error_type": "rate_limit", "error": "legacy_error", "error_message": "detail msg", "error_details": "legacy detail"},
			wantError: "rate_limit",
		},
		{
			name:      "only error_type and error_message (no legacy error)",
			extra:     map[string]any{"error_type": "rate_limit", "error_message": "too many requests"},
			wantError: "rate_limit",
		},
		{
			name:      "error (legacy) wins over error_message even when error_message is present",
			extra:     map[string]any{"error": "legacy_wins", "error_message": "should_not_win"},
			wantError: "legacy_wins",
		},
		{
			name:      "only error (legacy)",
			extra:     map[string]any{"error": "rate_limit", "error_details": "too many"},
			wantError: "rate_limit",
		},
		{
			name:      "error_message wins over error_details",
			extra:     map[string]any{"error_message": "msg_wins", "error_details": "should_not_win"},
			wantError: "msg_wins",
		},
		{
			name:      "only error_details",
			extra:     map[string]any{"error_details": "something went wrong"},
			wantError: "something went wrong",
		},
		{
			name:      "all four fields absent",
			extra:     map[string]any{},
			wantError: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			payload := map[string]any{"cwd": base["cwd"], "permission_mode": base["permission_mode"]}
			for k, v := range c.extra {
				payload[k] = v
			}
			got, ok := a.Parse("stop-failure", payload)
			if !ok {
				t.Fatal("ok = false, want true (StopFailure has no required field)")
			}
			if got.Error != c.wantError {
				t.Fatalf("Error = %q, want %q", got.Error, c.wantError)
			}
		})
	}
}

func TestClaudePermissionDenied(t *testing.T) {
	a := ClaudeAdapter{}
	// "tool_name" is a field foreign to PermissionDenied — Claude Code sends
	// it along, but the adapter has nowhere to put it; it must be silently
	// dropped.
	got, ok := a.Parse("permission-denied", map[string]any{
		"cwd": "/tmp", "permission_mode": "auto", "tool_name": "Bash",
	})
	if !ok {
		t.Fatal("ok = false, want true")
	}
	want := event.Event{
		Kind: event.PermissionDenied, Agent: "claude",
		Cwd: "/tmp", PermissionMode: "auto",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got = %+v\nwant = %+v", got, want)
	}
}

func TestClaudeCwdChanged(t *testing.T) {
	a := ClaudeAdapter{}

	t.Run("no_worktree", func(t *testing.T) {
		got, ok := a.Parse("cwd-changed", map[string]any{"cwd": "/new/path"})
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := event.Event{Kind: event.CwdChanged, Cwd: "/new/path"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got = %+v\nwant = %+v", got, want)
		}
		// Pin this down explicitly: CwdChanged does not carry
		// permission_mode/agent even though the payload has permission_mode
		// in other tests — this is a field DELIBERATELY not read.
		if got.PermissionMode != "" || got.Agent != "" {
			t.Fatalf("PermissionMode/Agent must be empty for CwdChanged, got %q/%q", got.PermissionMode, got.Agent)
		}
	})

	t.Run("has_worktree", func(t *testing.T) {
		got, ok := a.Parse("cwd-changed", map[string]any{
			"cwd": "/tmp/wt/src",
			"worktree": map[string]any{
				"name": "wt", "path": "/tmp/wt", "branch": "main",
				"originalRepoDir": "/home/user/repo",
			},
		})
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.Worktree == nil || got.Worktree.OriginalRepoDir != "/home/user/repo" {
			t.Fatalf("Worktree = %+v, want OriginalRepoDir = /home/user/repo", got.Worktree)
		}
	})
}

func TestClaudeSubagentStart(t *testing.T) {
	a := ClaudeAdapter{}

	t.Run("no_agent_id", func(t *testing.T) {
		got, ok := a.Parse("subagent-start", map[string]any{"agent_type": "Explore"})
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := event.Event{Kind: event.SubagentStart, AgentType: "Explore"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got = %+v\nwant = %+v", got, want)
		}
	})

	t.Run("has_agent_id", func(t *testing.T) {
		got, ok := a.Parse("subagent-start", map[string]any{"agent_type": "Explore", "agent_id": "sub-42"})
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := event.Event{Kind: event.SubagentStart, AgentType: "Explore", AgentID: "sub-42"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got = %+v\nwant = %+v", got, want)
		}
	})
}

func TestClaudeSubagentStartEmptyTypeIgnored(t *testing.T) {
	a := ClaudeAdapter{}
	_, ok := a.Parse("subagent-start", map[string]any{})
	if ok {
		t.Fatal("ok = true, want false when agent_type is empty")
	}
}

func TestClaudeSubagentStop(t *testing.T) {
	a := ClaudeAdapter{}

	t.Run("minimal", func(t *testing.T) {
		got, ok := a.Parse("subagent-stop", map[string]any{"agent_type": "Plan"})
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := event.Event{Kind: event.SubagentStop, AgentType: "Plan"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got = %+v\nwant = %+v", got, want)
		}
	})

	t.Run("full_payload", func(t *testing.T) {
		got, ok := a.Parse("subagent-stop", map[string]any{
			"agent_type": "Explore", "agent_id": "sub-42",
			"last_assistant_message": "Found the bug at main.rs:42",
			"agent_transcript_path":  "/tmp/sub-transcript.json",
		})
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := event.Event{
			Kind: event.SubagentStop, AgentType: "Explore", AgentID: "sub-42",
			LastMessage: "Found the bug at main.rs:42", TranscriptPath: "/tmp/sub-transcript.json",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got = %+v\nwant = %+v", got, want)
		}
	})
}

func TestClaudeSubagentStopEmptyTypeIgnored(t *testing.T) {
	a := ClaudeAdapter{}
	if _, ok := a.Parse("subagent-stop", map[string]any{}); ok {
		t.Fatal("ok = true, want false when agent_type is empty")
	}
}

func TestClaudeActivityLog(t *testing.T) {
	a := ClaudeAdapter{}

	t.Run("tool_input_as_object", func(t *testing.T) {
		got, ok := a.Parse("activity-log", map[string]any{
			"tool_name": "Read", "tool_input": map[string]any{"file_path": "/a/b.rs"},
		})
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := event.Event{
			Kind: event.ActivityLog, ToolName: "Read",
			ToolInput: map[string]any{"file_path": "/a/b.rs"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got = %+v\nwant = %+v", got, want)
		}
	})

	t.Run("tool_input_as_json_string", func(t *testing.T) {
		got, ok := a.Parse("activity-log", map[string]any{
			"tool_name": "Edit", "tool_input": `{"file_path":"/a/b.rs"}`,
		})
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := map[string]any{"file_path": "/a/b.rs"}
		if !reflect.DeepEqual(got.ToolInput, want) {
			t.Fatalf("ToolInput = %+v, want %+v", got.ToolInput, want)
		}
	})

	t.Run("bad_json_string_returns_nil_without_panic", func(t *testing.T) {
		got, ok := a.Parse("activity-log", map[string]any{
			"tool_name": "Edit", "tool_input": `not-json`,
		})
		if !ok {
			t.Fatal("ok = false, want true — a broken tool_input must not drop the whole event")
		}
		if got.ToolInput != nil {
			t.Fatalf("ToolInput = %+v, want nil when the string fails to parse", got.ToolInput)
		}
	})

	t.Run("has_tool_response", func(t *testing.T) {
		got, ok := a.Parse("activity-log", map[string]any{
			"tool_name":     "TaskCreate",
			"tool_input":    map[string]any{"subject": "Fix bug"},
			"tool_response": map[string]any{"task": map[string]any{"id": "42"}},
		})
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := event.Event{
			Kind: event.ActivityLog, ToolName: "TaskCreate",
			ToolInput:    map[string]any{"subject": "Fix bug"},
			ToolResponse: map[string]any{"task": map[string]any{"id": "42"}},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got = %+v\nwant = %+v", got, want)
		}
	})
}

func TestClaudeActivityLogEmptyToolNameIgnored(t *testing.T) {
	a := ClaudeAdapter{}
	if _, ok := a.Parse("activity-log", map[string]any{}); ok {
		t.Fatal("ok = true, want false when tool_name is empty")
	}
}

func TestClaudeTaskCreated(t *testing.T) {
	a := ClaudeAdapter{}

	t.Run("full", func(t *testing.T) {
		got, ok := a.Parse("task-created", map[string]any{"task_id": "42", "task_subject": "Fix bug"})
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := event.Event{Kind: event.TaskCreated, TaskID: "42", TaskSubject: "Fix bug"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got = %+v\nwant = %+v", got, want)
		}
	})

	t.Run("missing_fields_default_to_empty", func(t *testing.T) {
		got, ok := a.Parse("task-created", map[string]any{})
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := event.Event{Kind: event.TaskCreated}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got = %+v\nwant = %+v", got, want)
		}
	})

	// Stands in for the 3 "full_upstream_payload" tests from tests.rs (task-
	// created/task-completed/teammate-idle) — all three pin down the same
	// behavior: foreign fields in Claude Code's real payload do not break
	// parsing.
	t.Run("foreign_fields_are_ignored", func(t *testing.T) {
		got, ok := a.Parse("task-created", map[string]any{
			"session_id": "sess-1", "transcript_path": "/tmp/transcript",
			"cwd": "/home/user/project", "permission_mode": "auto",
			"hook_event_name": "TaskCreated",
			"task_id":         "99", "task_subject": "Deploy to staging",
			"task_description": "Run deployment pipeline",
			"teammate_name":    "deployer", "team_name": "ops",
		})
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := event.Event{Kind: event.TaskCreated, TaskID: "99", TaskSubject: "Deploy to staging"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got = %+v\nwant = %+v (all foreign fields must be ignored)", got, want)
		}
	})
}

func TestClaudeTaskCompleted(t *testing.T) {
	a := ClaudeAdapter{}
	got, ok := a.Parse("task-completed", map[string]any{"task_id": "42", "task_subject": "Fix bug"})
	if !ok {
		t.Fatal("ok = false, want true")
	}
	want := event.Event{Kind: event.TaskCompleted, TaskID: "42", TaskSubject: "Fix bug"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got = %+v\nwant = %+v", got, want)
	}
}

func TestClaudeTeammateIdle(t *testing.T) {
	a := ClaudeAdapter{}

	t.Run("has_reason", func(t *testing.T) {
		got, ok := a.Parse("teammate-idle", map[string]any{
			"teammate_name": "reviewer", "team_name": "dev", "idle_reason": "tokens_exhausted",
		})
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := event.Event{
			Kind: event.TeammateIdle, TeammateName: "reviewer",
			TeamName: "dev", IdleReason: "tokens_exhausted",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got = %+v\nwant = %+v", got, want)
		}
	})

	t.Run("no_reason", func(t *testing.T) {
		got, ok := a.Parse("teammate-idle", map[string]any{"teammate_name": "reviewer", "team_name": "dev"})
		if !ok {
			t.Fatal("ok = false, want true")
		}
		if got.IdleReason != "" {
			t.Fatalf("IdleReason = %q, want empty", got.IdleReason)
		}
	})

	t.Run("missing_everything", func(t *testing.T) {
		got, ok := a.Parse("teammate-idle", map[string]any{})
		if !ok {
			t.Fatal("ok = false, want true")
		}
		want := event.Event{Kind: event.TeammateIdle}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got = %+v\nwant = %+v", got, want)
		}
	})
}

func TestClaudeUnknownExternalNameIgnored(t *testing.T) {
	a := ClaudeAdapter{}
	got, ok := a.Parse("unknown-event", map[string]any{})
	if ok {
		t.Fatal("ok = true, want false for an unknown externalName")
	}
	if !reflect.DeepEqual(got, event.Event{}) {
		t.Fatalf("got = %+v, want zero-value Event{} when ok=false", got)
	}
}

func TestClaudeInfoNotificationsAreMetaOnly(t *testing.T) {
	a := ClaudeAdapter{}
	for typ, meta := range map[string]bool{"idle_prompt": true, "agent_completed": true, "auth_success": true, "permission_prompt": false, "elicitation_dialog": false} {
		evt, ok := a.Parse("notification", map[string]any{"notification_type": typ})
		if !ok || evt.MetaOnly != meta {
			t.Errorf("%s: MetaOnly = %v, want %v", typ, evt.MetaOnly, meta)
		}
	}
}
