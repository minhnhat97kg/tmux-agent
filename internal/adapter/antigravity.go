// Package adapter — AntigravityAdapter for the Google Antigravity CLI
// (antigravity.google).
package adapter

import "tmux-agent/internal/event"

// antigravityAgentName is the agent name attached to every Event that
// Antigravity generates.
const antigravityAgentName = "antigravity"

// antigravityPostToolUseResponse/antigravityStopResponse are the fixed JSON
// responses that must be returned via stdout — see AntigravityAdapter's doc
// comment, "Gating contract" section.
const (
	antigravityPostToolUseResponse = `{}`
	antigravityStopResponse        = `{"decision":"continue"}`
)

// AntigravityAdapter is the Adapter for the Antigravity CLI.
//
// ANTIGRAVITY IS NOT INSTALLED FOR SELF-VERIFICATION (unlike Claude Code) —
// this relies entirely on public documentation (antigravity.google/docs/
// hooks/, read on 2026-09-15). Read this ENTIRE comment carefully before
// making changes — the risk of getting this adapter wrong is DIFFERENT
// from Pi/Codex/OpenCode, not just "missing info displayed incorrectly".
//
// # Payload structure differences (Claude/Pi logic cannot be reused)
//
// Antigravity does NOT use the flat "session_id"/"tool_name"/"tool_input"
// fields like Claude Code/Pi/Codex. Fields common to every hook:
// conversationId, workspacePaths (array), transcriptPath,
// artifactDirectoryPath, modelName. The tool lives in a nested "toolCall"
// object: {name, args}.
//
// # Antigravity has NO concept of SessionStart/UserPromptSubmit/SessionEnd
//
// The documentation only lists 5 hooks: PreToolUse, PostToolUse,
// PreInvocation, PostInvocation, Stop — nothing equivalent to
// SessionStart. Consequence: there is NO point at which to proactively
// write "@pane_agent" before any work has happened — a pane monitored via
// Antigravity is only RECOGNIZED by the sidebar (a query filtering on
// @pane_agent, internal/tmux/query.go) starting from the FIRST Stop
// (OnStop calls SetAgentMeta — see handlers.go), not right when the agent
// starts up like the other three adapters. There is nothing to fix at the
// adapter layer for this — it would need a new Kind, something like a
// "synthetic SessionStart", if we wanted to address it, which is out of
// scope for this consideration.
//
// # Gating contract (WHY PreToolUse/PreInvocation/PostInvocation ARE NOT REGISTERED)
//
// Antigravity, UNLIKE Claude/Pi/Codex, uses hooks to MAKE DECISIONS, not
// just observe:
//   - PreToolUse expects {"decision": "allow"/"deny"/"ask"/...} — returning
//     the wrong thing, or nothing, can BLOCK the user's real tool call.
//   - PreInvocation/PostInvocation expect injectSteps/terminationBehavior —
//     the more complex the response, the easier it is to get wrong in a way
//     that directly affects the run.
//
// Registrations() DELIBERATELY registers ONLY PostToolUse and Stop — the
// only two hooks for which the documentation shows a "safe, no-opinion"
// response is CLEAR and UNAMBIGUOUS (PostToolUse: "{}"; Stop: decision
// "continue" — equivalent to Codex's {"continue":true}, see
// codexStopResponse). The remaining three gating hooks are not registered
// — Antigravity will never call hook.sh for them, so there is no risk of
// returning the wrong decision. Adding Pre* hooks later MUST first involve
// installing real Antigravity to verify behavior when the hook returns
// empty/errors, not further guessing from documentation.
type AntigravityAdapter struct{}

// init self-registers this adapter into the package registry.
func init() { register(AntigravityAdapter{}) }

// Name returns the agent label.
func (AntigravityAdapter) Name() string { return antigravityAgentName }

// Registrations returns the hook registration table — see the "Gating
// contract" section above for why there are only two entries.
func (AntigravityAdapter) Registrations() []Registration {
	return []Registration{
		{Trigger: "PostToolUse", Kind: event.ActivityLog},
		{Trigger: "Stop", Kind: event.Stop},
	}
}

// Parse translates one Antigravity hook event into an internal event.Event.
// A nil payload or missing fields both degrade gracefully to the zero
// value, never panicking.
func (AntigravityAdapter) Parse(externalName string, payload map[string]any) (event.Event, bool) {
	switch externalName {
	case "activity-log":
		toolCall := jsonObj(payload, "toolCall")
		toolName := jsonStr(toolCall, "name")
		if toolName == "" {
			return event.Event{}, false
		}
		resp := antigravityPostToolUseResponse
		return event.Event{
			Kind:      event.ActivityLog,
			ToolName:  toolName,
			ToolInput: jsonObj(toolCall, "args"),
			// Response is always nil: Antigravity's PostToolUse does not
			// document any field carrying the tool result (only
			// toolCall/stepIdx/error).
			Response: &resp,
		}, true

	case "stop":
		resp := antigravityStopResponse
		return event.Event{
			Kind:  event.Stop,
			Agent: antigravityAgentName,
			// Cwd/SessionID are derived from the two fields COMMON to
			// every Antigravity hook (workspacePaths, conversationId) —
			// Stop has no dedicated field directly matching
			// "cwd"/"session_id" like Claude/Pi/Codex.
			// LastMessage is always empty: the Stop input only carries
			// executionNum/terminationReason/error/fullyIdle, no reply
			// content.
			Cwd:       antigravityFirstWorkspacePath(payload),
			SessionID: jsonStr(payload, "conversationId"),
			Response:  &resp,
		}, true
	}

	return event.Event{}, false
}

// antigravityFirstWorkspacePath reads "workspacePaths" (an array of strings
// per the docs), returning the first element. Returns "" when missing, of
// the wrong type, or empty — a graceful degradation, not an error (some
// hooks may not carry this field).
func antigravityFirstWorkspacePath(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	raw, ok := payload["workspacePaths"]
	if !ok {
		return ""
	}
	arr, ok := raw.([]any)
	if !ok || len(arr) == 0 {
		return ""
	}
	s, ok := arr[0].(string)
	if !ok {
		return ""
	}
	return s
}
