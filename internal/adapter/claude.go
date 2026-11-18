package adapter

import (
	"encoding/json"

	"tmux-agent/internal/event"
)

// init self-registers this adapter into the package registry. This way
// adding an adapter doesn't require editing adapter.go — that is the
// condition that lets three adapter tasks run in parallel without touching
// each other's files.
func init() { register(ClaudeAdapter{}) }

const claudeAgentName = "claude"

// ClaudeAdapter translates Claude Code hook payloads into internal
// event.Event values.
//
// Source of truth for how hooks are wired up: see the mapping table in the
// port document (section 1). Drift between Registrations() and Parse() is
// caught by TestClaudeHookRegistrationsDriftFree (testhelpers_test.go).
//
// NOTE: PostToolUse → event.ActivityLog is the ONLY entry whose trigger
// name differs from the internal Kind name.
type ClaudeAdapter struct{}

func (ClaudeAdapter) Name() string { return claudeAgentName }

func (ClaudeAdapter) Registrations() []Registration {
	return []Registration{
		{Trigger: "SessionStart", Kind: event.SessionStart},
		{Trigger: "SessionEnd", Kind: event.SessionEnd},
		{Trigger: "UserPromptSubmit", Kind: event.UserPromptSubmit},
		{Trigger: "Notification", Kind: event.Notification},
		{Trigger: "Stop", Kind: event.Stop},
		{Trigger: "StopFailure", Kind: event.StopFailure},
		{Trigger: "PermissionDenied", Kind: event.PermissionDenied},
		{Trigger: "CwdChanged", Kind: event.CwdChanged},
		{Trigger: "SubagentStart", Kind: event.SubagentStart},
		{Trigger: "SubagentStop", Kind: event.SubagentStop},
		{Trigger: "PostToolUse", Kind: event.ActivityLog},
		{Trigger: "TaskCreated", Kind: event.TaskCreated},
		{Trigger: "TaskCompleted", Kind: event.TaskCompleted},
		{Trigger: "TeammateIdle", Kind: event.TeammateIdle},
	}
}

func (ClaudeAdapter) Parse(externalName string, payload map[string]any) (event.Event, bool) {
	switch externalName {
	case "session-start":
		return event.Event{
			Kind:           event.SessionStart,
			Agent:          claudeAgentName,
			Cwd:            jsonStr(payload, "cwd"),
			PermissionMode: jsonStr(payload, "permission_mode"),
			Source:         jsonStr(payload, "source"),
			Worktree:       claudeWorktree(payload),
			AgentID:        jsonStr(payload, "agent_id"),
			SessionID:      jsonStr(payload, "session_id"),
		}, true

	case "session-end":
		return event.Event{
			Kind:      event.SessionEnd,
			EndReason: jsonStr(payload, "end_reason"),
		}, true

	case "user-prompt-submit":
		return event.Event{
			Kind:           event.UserPromptSubmit,
			Agent:          claudeAgentName,
			Cwd:            jsonStr(payload, "cwd"),
			PermissionMode: jsonStr(payload, "permission_mode"),
			Prompt:         jsonStr(payload, "prompt"),
			Worktree:       claudeWorktree(payload),
			AgentID:        jsonStr(payload, "agent_id"),
			SessionID:      jsonStr(payload, "session_id"),
		}, true

	case "notification":
		waitReason := jsonStr(payload, "notification_type")
		// (see infoNotifications)
		return event.Event{
			Kind:           event.Notification,
			Agent:          claudeAgentName,
			Cwd:            jsonStr(payload, "cwd"),
			PermissionMode: jsonStr(payload, "permission_mode"),
			WaitReason:     waitReason,
			// Notifications that don't wait on the user only refresh
			// metadata, they don't change status/attention: idle_prompt,
			// and the end-of-turn agent_completed / auth_success ones newer
			// Claude Code sends — treating those as "waiting" left a
			// finished agent stuck on "needs your input".
			MetaOnly:  infoNotifications[waitReason],
			Worktree:  claudeWorktree(payload),
			AgentID:   jsonStr(payload, "agent_id"),
			SessionID: jsonStr(payload, "session_id"),
		}, true

	case "stop":
		return event.Event{
			Kind:           event.Stop,
			Agent:          claudeAgentName,
			Cwd:            jsonStr(payload, "cwd"),
			PermissionMode: jsonStr(payload, "permission_mode"),
			LastMessage:    jsonStr(payload, "last_assistant_message"),
			// Response is always nil: the reference Rust implementation
			// also hard-codes None here, no JSON field has been used to
			// derive it yet. Keep as-is, don't guess further.
			Response:  nil,
			Worktree:  claudeWorktree(payload),
			AgentID:   jsonStr(payload, "agent_id"),
			SessionID: jsonStr(payload, "session_id"),
		}, true

	case "stop-failure":
		return event.Event{
			Kind:           event.StopFailure,
			Agent:          claudeAgentName,
			Cwd:            jsonStr(payload, "cwd"),
			PermissionMode: jsonStr(payload, "permission_mode"),
			Error:          claudeStopFailureError(payload),
			Worktree:       claudeWorktree(payload),
			AgentID:        jsonStr(payload, "agent_id"),
			SessionID:      jsonStr(payload, "session_id"),
		}, true

	case "permission-denied":
		return event.Event{
			Kind:           event.PermissionDenied,
			Agent:          claudeAgentName,
			Cwd:            jsonStr(payload, "cwd"),
			PermissionMode: jsonStr(payload, "permission_mode"),
			Worktree:       claudeWorktree(payload),
			AgentID:        jsonStr(payload, "agent_id"),
			SessionID:      jsonStr(payload, "session_id"),
		}, true

	case "cwd-changed":
		// Do NOT read permission_mode/agent: Claude Code's real payload for
		// this trigger does not carry those two fields.
		return event.Event{
			Kind:      event.CwdChanged,
			Cwd:       jsonStr(payload, "cwd"),
			Worktree:  claudeWorktree(payload),
			AgentID:   jsonStr(payload, "agent_id"),
			SessionID: jsonStr(payload, "session_id"),
		}, true

	case "subagent-start":
		agentType := jsonStr(payload, "agent_type")
		if agentType == "" {
			return event.Event{}, false
		}
		return event.Event{
			Kind:      event.SubagentStart,
			AgentType: agentType,
			AgentID:   jsonStr(payload, "agent_id"),
		}, true

	case "subagent-stop":
		agentType := jsonStr(payload, "agent_type")
		if agentType == "" {
			return event.Event{}, false
		}
		return event.Event{
			Kind:           event.SubagentStop,
			AgentType:      agentType,
			AgentID:        jsonStr(payload, "agent_id"),
			LastMessage:    jsonStr(payload, "last_assistant_message"),
			TranscriptPath: jsonStr(payload, "agent_transcript_path"),
		}, true

	case "activity-log":
		toolName := jsonStr(payload, "tool_name")
		if toolName == "" {
			return event.Event{}, false
		}
		return event.Event{
			Kind:         event.ActivityLog,
			ToolName:     toolName,
			ToolInput:    claudeToolField(payload, "tool_input"),
			ToolResponse: claudeToolField(payload, "tool_response"),
		}, true

	case "task-created":
		return event.Event{
			Kind:        event.TaskCreated,
			TaskID:      jsonStr(payload, "task_id"),
			TaskSubject: jsonStr(payload, "task_subject"),
		}, true

	case "task-completed":
		return event.Event{
			Kind:        event.TaskCompleted,
			TaskID:      jsonStr(payload, "task_id"),
			TaskSubject: jsonStr(payload, "task_subject"),
		}, true

	case "teammate-idle":
		return event.Event{
			Kind:         event.TeammateIdle,
			TeammateName: jsonStr(payload, "teammate_name"),
			TeamName:     jsonStr(payload, "team_name"),
			IdleReason:   jsonStr(payload, "idle_reason"),
		}, true
	}

	return event.Event{}, false
}

// claudeWorktree reads the nested "worktree" field (name/path/branch/
// originalRepoDir — note originalRepoDir is camelCase, unlike every other
// top-level field which is snake_case). Returns nil when missing, of the
// wrong type, or an empty object (every sub-field empty) — all three mean
// "not running in a worktree", not an error.
func claudeWorktree(payload map[string]any) *event.WorktreeInfo {
	obj := jsonObj(payload, "worktree")
	if obj == nil {
		return nil
	}
	name := jsonStr(obj, "name")
	path := jsonStr(obj, "path")
	branch := jsonStr(obj, "branch")
	original := jsonStr(obj, "originalRepoDir")
	if name == "" && path == "" && branch == "" && original == "" {
		return nil
	}
	return &event.WorktreeInfo{
		Name:            name,
		Path:            path,
		Branch:          branch,
		OriginalRepoDir: original,
	}
}

// claudeStopFailureError picks the error field in priority order: error_type
// (new, category) > error (old, legacy) > error_message (new, detailed) >
// error_details (old, detailed). The first non-empty field wins.
func claudeStopFailureError(payload map[string]any) string {
	if v := jsonStr(payload, "error_type"); v != "" {
		return v
	}
	if v := jsonStr(payload, "error"); v != "" {
		return v
	}
	if v := jsonStr(payload, "error_message"); v != "" {
		return v
	}
	return jsonStr(payload, "error_details")
}

// claudeToolField reads tool_input/tool_response: accepts both a JSON
// object and a string containing escaped JSON (some Claude Code hook
// versions send it as a string). A parse failure or a non-object value
// (array, number, bool, ...) both become nil — Event.ToolInput/ToolResponse
// are only declared as map[string]any, so there's nowhere to put a
// non-object JSON value; this is a deliberate soft fallback, not a panic.
func claudeToolField(payload map[string]any, key string) map[string]any {
	v, ok := payload[key]
	if !ok {
		return nil
	}
	switch val := v.(type) {
	case map[string]any:
		return val
	case string:
		var m map[string]any
		if err := json.Unmarshal([]byte(val), &m); err != nil {
			return nil
		}
		return m
	default:
		return nil
	}
}

// infoNotifications are Claude Code notification types that don't need the
// user to act.
var infoNotifications = map[string]bool{"idle_prompt": true, "agent_completed": true, "auth_success": true}
