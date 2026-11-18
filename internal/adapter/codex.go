// Package adapter — CodexAdapter for the Codex CLI (openai/codex).
package adapter

import "tmux-agent/internal/event"

// codexAgentName is the agent name attached to every Event that Codex
// generates.
const codexAgentName = "codex"

// codexStopResponse is the fixed JSON that the Codex hook engine expects to
// receive back via stdout when the Stop hook runs, telling the Codex CLI to
// continue the current turn instead of stopping the session outright. This
// constant does not depend on the payload: every Codex Stop event carries
// the same response value.
const codexStopResponse = `{"continue":true}`

// CodexAdapter is the Adapter for the Codex CLI.
//
// Reference source: openai/codex:codex-rs/hooks/src/engine/config.rs — the
// hook event enum currently only defines 5 values: PreToolUse, PostToolUse,
// SessionStart, UserPromptSubmit, Stop.
//
// Compared to Claude Code, Codex LACKS:
//
//   - SessionEnd: there's no reliable exit-notification hook. Parse() never
//     generates event.SessionEnd. To know when Codex has exited, the caller
//     must poll the process via internal/process, not via a hook.
//   - Notification, StopFailure, PermissionDenied, CwdChanged,
//     SubagentStart/SubagentStop, TaskCreated/TaskCompleted, TeammateIdle,
//     WorktreeCreate/WorktreeRemove: the Codex hook engine has no
//     equivalent concept. Parse() always returns ok=false for these
//     external names.
//   - Worktree and AgentID: always nil/empty for EVERY Codex event, even
//     SessionStart — Codex doesn't detect git worktrees or subagent IDs at
//     the hook layer.
//   - PreToolUse: Codex supports it, but it's deliberately not wired up yet
//     (reserved for PermissionDenied in a future task).
//
// Special case: PermissionMode is DELIBERATELY always left empty ("") in
// this adapter, even when the payload has a "permission_mode" field. Codex
// doesn't reliably write the permission mode into the hook payload in a
// stable way like Claude Code does; Codex's real permission mode is
// inferred at a DIFFERENT LAYER, by reading the command-line arguments of
// the running codex process — not inside Parse(). This is a deliberate
// difference from the reference Rust implementation (which reads
// "permission_mode" straight from the JSON).
type CodexAdapter struct{}

// init self-registers this adapter into the package registry. This way
// adding an adapter doesn't require editing adapter.go — that is the
// condition that lets three adapter tasks run in parallel without touching
// each other's files.
func init() { register(CodexAdapter{}) }

// Name returns the agent label.
func (CodexAdapter) Name() string { return codexAgentName }

// Registrations returns the Codex CLI's hook registration table — the
// single source of truth for Codex's setup command / README /
// assets/hooks/hooks.json.
func (CodexAdapter) Registrations() []Registration {
	return []Registration{
		{Trigger: "SessionStart", Matcher: "startup|resume", Kind: event.SessionStart},
		{Trigger: "UserPromptSubmit", Matcher: "", Kind: event.UserPromptSubmit},
		{Trigger: "Stop", Matcher: "", Kind: event.Stop},
		{Trigger: "PostToolUse", Matcher: "", Kind: event.ActivityLog},
	}
}

// Parse translates one Codex CLI hook event into an internal event.Event.
// A nil payload or missing fields both degrade gracefully to the zero
// value, never panicking.
func (CodexAdapter) Parse(externalName string, payload map[string]any) (event.Event, bool) {
	switch externalName {
	case "session-start":
		return event.Event{
			Kind:      event.SessionStart,
			Agent:     codexAgentName,
			Cwd:       jsonStr(payload, "cwd"),
			Source:    jsonStr(payload, "source"),
			SessionID: jsonStr(payload, "session_id"),
		}, true

	case "user-prompt-submit":
		return event.Event{
			Kind:      event.UserPromptSubmit,
			Agent:     codexAgentName,
			Cwd:       jsonStr(payload, "cwd"),
			Prompt:    jsonStr(payload, "prompt"),
			SessionID: jsonStr(payload, "session_id"),
		}, true

	case "stop":
		resp := codexStopResponse
		return event.Event{
			Kind:        event.Stop,
			Agent:       codexAgentName,
			Cwd:         jsonStr(payload, "cwd"),
			LastMessage: jsonStr(payload, "last_assistant_message"),
			Response:    &resp,
			SessionID:   jsonStr(payload, "session_id"),
		}, true

	// Codex's PostToolUse currently only fires the hook for Bash (Codex's
	// tool_input is a typed struct {command: string}), so the activity log
	// generated here in practice only ever covers shell commands. The
	// adapter doesn't hardcode "Bash" though: it only rejects when
	// tool_name is empty.
	case "activity-log":
		toolName := jsonStr(payload, "tool_name")
		if toolName == "" {
			return event.Event{}, false
		}
		return event.Event{
			Kind:         event.ActivityLog,
			ToolName:     toolName,
			ToolInput:    jsonObj(payload, "tool_input"),
			ToolResponse: jsonObj(payload, "tool_response"),
		}, true

	default:
		return event.Event{}, false
	}
}
