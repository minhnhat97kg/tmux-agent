package adapter

import "tmux-agent/internal/event"

// init self-registers this adapter into the package registry. This way
// adding an adapter doesn't require editing adapter.go — that is the
// condition that lets three adapter tasks run in parallel without touching
// each other's files.
func init() { register(OpenCodeAdapter{}) }

// opencodeAgentName is the agent name attached to every Event that
// OpenCode generates.
const opencodeAgentName = "opencode"

// OpenCodeAdapter translates OpenCode events into event.Event.
//
// Unlike Claude/Codex, OpenCode doesn't configure hooks via a
// settings.json. It uses a JavaScript plugin
// (assets/.opencode/plugins/tmux-agent-sidebar.js, running inside
// OpenCode's own Bun runtime) as the bridge. `tmux-agent install opencode`
// writes that file to ~/.config/opencode/plugins/tmux-agent.js — OpenCode v2
// discovers both `plugin/` and `plugins/`, but treats only `plugins/` as V2.
// The plugin default-exports a V2 definition (id + setup(ctx)), registers
// `ctx.session.hook("prompt", …)`, `ctx.tool.hook("execute.after", …)`, and a
// `ctx.event.subscribe(…)` loop, and inside each callback spawns
// "tmux-agent hook opencode <event>" with JSON on stdin, without waiting for
// the child to exit (fire-and-forget).
//
// Consequence: OpenCode has no reliable "session-end" — Bun doesn't
// guarantee that a process.on("exit")-style handler finishes running when
// the process is killed/crashes. Session end can only be inferred
// indirectly via the "stop" event.
//
// PermissionMode: this adapter NEVER sets it (always the zero value "") —
// OpenCode has no concept of permission mode at the JS plugin layer.
// PermissionMode: "" is not assigned explicitly in Parse() (that's already
// the zero value, writing it out would just add noise); same approach as
// CodexAdapter for the same situation.
type OpenCodeAdapter struct{}

// Name returns the agent label used in tmux options and on the hook's
// argv.
func (OpenCodeAdapter) Name() string { return opencodeAgentName }

// Registrations lists the source of each externalName on the JS plugin
// side. Unlike Claude/Codex: there's no settings.json, so Trigger/Matcher
// here describe the V2 JS API path + event.type, not a real settings.json
// trigger. This table isn't ported from the Rust version (the reference
// implementation has no registration table for OpenCode) — it was rebuilt
// directly from the hook() calls in
// assets/.opencode/plugins/tmux-agent-sidebar.js.
func (OpenCodeAdapter) Registrations() []Registration {
	return []Registration{
		{Trigger: "ctx.event.subscribe", Matcher: "session.created", Kind: event.SessionStart},
		{Trigger: "ctx.session.hook", Matcher: "prompt", Kind: event.UserPromptSubmit},
		{Trigger: "ctx.event.subscribe", Matcher: "session.status:busy", Kind: event.UserPromptSubmit},
		{Trigger: "ctx.event.subscribe", Matcher: "session.status:idle", Kind: event.Stop},
		{Trigger: "ctx.event.subscribe", Matcher: "session.idle", Kind: event.Stop},
		{Trigger: "ctx.event.subscribe", Matcher: "session.error", Kind: event.StopFailure},
		{Trigger: "ctx.event.subscribe", Matcher: "permission.asked", Kind: event.Notification},
		{Trigger: "ctx.tool.hook", Matcher: "execute.after", Kind: event.ActivityLog},
	}
}

// normalizeOpenCodeToolName maps OpenCode's lowercase tool IDs (bash,
// read, …) to the PascalCase vocabulary used by Claude/Codex, so the
// activity log and the label-strategy table share one vocabulary across
// agents. Unknown tools (custom MCP, …) are left unchanged — a silent soft
// fallback.
func normalizeOpenCodeToolName(raw string) string {
	switch raw {
	case "bash":
		return "Bash"
	case "read":
		return "Read"
	case "write":
		return "Write"
	case "edit", "multiedit":
		return "Edit"
	case "glob":
		return "Glob"
	case "grep":
		return "Grep"
	case "webfetch":
		return "WebFetch"
	case "websearch":
		return "WebSearch"
	case "task":
		return "Agent"
	case "skill":
		return "Skill"
	case "lsp":
		return "LSP"
	case "todowrite":
		return "TodoWrite"
	default:
		return raw
	}
}

// normalizeOpenCodeToolInput renames OpenCode's camelCase keys to the
// snake_case that the Claude-style label extractor needs, only for
// Read/Write/Edit. The original key is kept alongside the new one — raw
// data isn't deleted, so any consumer that needs the original payload can
// still see it.
func normalizeOpenCodeToolInput(toolName string, input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	var rewrites [][2]string
	switch toolName {
	case "Read", "Write", "Edit":
		rewrites = [][2]string{{"filePath", "file_path"}}
	}
	for _, pair := range rewrites {
		src, dst := pair[0], pair[1]
		if _, exists := input[dst]; exists {
			continue
		}
		if value, ok := input[src]; ok {
			input[dst] = value
		}
	}
	return input
}

// Parse translates an OpenCode externalName + JSON payload into an
// event.Event. ok=false means the event is skipped — not an error, and it
// never panics.
func (OpenCodeAdapter) Parse(externalName string, payload map[string]any) (event.Event, bool) {
	switch externalName {
	case "session-start":
		return event.Event{
			Kind:      event.SessionStart,
			Agent:     opencodeAgentName,
			Cwd:       jsonStr(payload, "cwd"),
			Source:    jsonStr(payload, "source"),
			SessionID: jsonStr(payload, "session_id"),
		}, true

	case "user-prompt-submit":
		return event.Event{
			Kind:      event.UserPromptSubmit,
			Agent:     opencodeAgentName,
			Cwd:       jsonStr(payload, "cwd"),
			Prompt:    jsonStr(payload, "prompt"),
			SessionID: jsonStr(payload, "session_id"),
		}, true

	case "notification":
		return event.Event{
			Kind:       event.Notification,
			Agent:      opencodeAgentName,
			Cwd:        jsonStr(payload, "cwd"),
			WaitReason: jsonStr(payload, "wait_reason"),
			MetaOnly:   false,
			SessionID:  jsonStr(payload, "session_id"),
		}, true

	case "stop":
		return event.Event{
			Kind:        event.Stop,
			Agent:       opencodeAgentName,
			Cwd:         jsonStr(payload, "cwd"),
			LastMessage: jsonStr(payload, "last_message"),
			Response:    nil,
			SessionID:   jsonStr(payload, "session_id"),
		}, true

	case "stop-failure":
		return event.Event{
			Kind:      event.StopFailure,
			Agent:     opencodeAgentName,
			Cwd:       jsonStr(payload, "cwd"),
			Error:     jsonStr(payload, "error"),
			SessionID: jsonStr(payload, "session_id"),
		}, true

	case "activity-log":
		rawName := jsonStr(payload, "tool_name")
		if rawName == "" {
			return event.Event{}, false
		}
		toolName := normalizeOpenCodeToolName(rawName)
		toolInput := normalizeOpenCodeToolInput(toolName, jsonObj(payload, "tool_input"))
		return event.Event{
			Kind:         event.ActivityLog,
			ToolName:     toolName,
			ToolInput:    toolInput,
			ToolResponse: jsonObj(payload, "tool_response"),
		}, true

	default:
		return event.Event{}, false
	}
}
