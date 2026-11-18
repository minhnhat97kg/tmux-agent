// Package adapter — PiAdapter for the Pi coding agent (pi.dev,
// github.com/badlogic/pi-mono).
package adapter

import "tmux-agent/internal/event"

// piAgentName is the agent name attached to every Event that Pi generates.
const piAgentName = "pi"

// PiAdapter is the Adapter for the Pi CLI.
//
// Reference source (Pi is NOT installed to verify against a real hook,
// unlike Claude Code — this relies on public documentation, read carefully
// before trusting it):
//   - hsingjui/pi-hooks README (github.com/hsingjui/pi-hooks) — a package
//     "Claude Code-compatible command hooks for Pi", which clearly lists
//     the payload for EACH trigger: SessionStart {session_id,
//     transcript_path, cwd, hook_event_name, source}, SessionEnd {...,
//     reason}, UserPromptSubmit {..., prompt}, Stop {..., stop_hook_active,
//     last_assistant_message}, PreToolUse/PostToolUse {..., tool_name,
//     tool_input[, tool_response], tool_use_id}.
//   - packages/coding-agent/docs/settings.md (badlogic/pi-mono) — confirms
//     Pi uses a plain settings.json JSON file (project overrides global),
//     with nothing further about the hook contract beyond the README
//     above.
//
// Compared to ClaudeAdapter, Pi LACKS (per what the docs say — the absence
// of a field means "not confirmed by documentation", NOT "definitely does
// not exist"): Notification, StopFailure, PermissionDenied, CwdChanged,
// SubagentStart/SubagentStop, TaskCreated/TaskCompleted, TeammateIdle.
// Parse() never generates those Kinds for Pi — a pane monitored via Pi
// will have NO subagent tree (section 6), no detailed "awaiting approval"
// badge, no automatic worktree-removal button.
//
// PermissionMode/Worktree/AgentID: NO payload field is confirmed by the
// docs for any of these three — always left empty/nil, for the same reason
// CodexAdapter leaves PermissionMode empty (better to lack information
// than to guess a field name wrong and silently always read it as empty
// while believing "this agent doesn't use this mode").
//
// SessionStart is registered with an EMPTY matcher (catching every
// "source", like ClaudeAdapter, unlike CodexAdapter which narrows it to
// "startup|resume") — handlers.OnSessionStart itself switches on
// evt.Source ("resume"/"compact"/other), so there's no need to filter at
// the registration layer; the pi-hooks README itself lists
// "startup|resume|compact" as the three valid source values, exactly
// matching the three branches that switch handles.
type PiAdapter struct{}

// init self-registers this adapter into the package registry — adding an
// adapter doesn't require editing adapter.go.
func init() { register(PiAdapter{}) }

// Name returns the agent label.
func (PiAdapter) Name() string { return piAgentName }

// Registrations returns the Pi CLI's hook registration table — the single
// source of truth for the setup command / README / assets/hooks/hooks.json.
func (PiAdapter) Registrations() []Registration {
	return []Registration{
		{Trigger: "SessionStart", Kind: event.SessionStart},
		{Trigger: "SessionEnd", Kind: event.SessionEnd},
		{Trigger: "UserPromptSubmit", Kind: event.UserPromptSubmit},
		{Trigger: "Stop", Kind: event.Stop},
		{Trigger: "PostToolUse", Kind: event.ActivityLog},
	}
}

// Parse translates one Pi CLI hook event into an internal event.Event. A
// nil payload or missing fields both degrade gracefully to the zero value,
// never panicking.
func (PiAdapter) Parse(externalName string, payload map[string]any) (event.Event, bool) {
	switch externalName {
	case "session-start":
		return event.Event{
			Kind:      event.SessionStart,
			Agent:     piAgentName,
			Cwd:       jsonStr(payload, "cwd"),
			Source:    jsonStr(payload, "source"),
			SessionID: jsonStr(payload, "session_id"),
		}, true

	case "session-end":
		return event.Event{
			Kind:      event.SessionEnd,
			EndReason: jsonStr(payload, "reason"),
		}, true

	case "user-prompt-submit":
		return event.Event{
			Kind:      event.UserPromptSubmit,
			Agent:     piAgentName,
			Cwd:       jsonStr(payload, "cwd"),
			Prompt:    jsonStr(payload, "prompt"),
			SessionID: jsonStr(payload, "session_id"),
		}, true

	case "stop":
		return event.Event{
			Kind:        event.Stop,
			Agent:       piAgentName,
			Cwd:         jsonStr(payload, "cwd"),
			LastMessage: jsonStr(payload, "last_assistant_message"),
			// Response is always nil: there is no documentation showing
			// that the Pi hook engine reads the Stop stdout back to
			// decide whether to continue the turn (unlike Codex — see
			// codexStopResponse, codex.go).
			Response:  nil,
			SessionID: jsonStr(payload, "session_id"),
		}, true

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
	}

	return event.Event{}, false
}
