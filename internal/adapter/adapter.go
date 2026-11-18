// Package adapter translates each agent's raw hook events (Claude Code,
// Codex, OpenCode) into the internal event.Event.
package adapter

import "tmux-agent/internal/event"

// Registration links an agent-side trigger (as it appears in the agent's
// settings.json) to the internal event kind produced when the hook runs.
//
// Each adapter's registration table is the SINGLE SOURCE OF TRUTH for: the
// setup command, the README instructions, and assets/hooks/hooks.json.
type Registration struct {
	// Trigger is the name in the agent's settings.json, e.g. "SessionStart",
	// "PostToolUse".
	Trigger string
	// Matcher is the filter value, e.g. "startup|resume". Empty means
	// registering with an empty matcher (matches everything).
	Matcher string
	// Kind is the internal event that this registration produces.
	Kind event.Kind
}

// Adapter translates a specific agent's events into internal events.
type Adapter interface {
	// Name returns the agent label: "claude" / "codex" / "opencode".
	Name() string
	// Registrations returns this agent's full hook registration table.
	Registrations() []Registration
	// Parse translates one event. externalName is the kebab-case name on
	// argv. payload is the JSON decoded from stdin (nil when absent).
	// ok=false means "skip this event" — NOT an error.
	Parse(externalName string, payload map[string]any) (event.Event, bool)
}

// registry lists adapters keyed by agent label.
//
// It starts EMPTY here, and each adapter SELF-REGISTERS in its own file's
// init(). This way adding an adapter doesn't require editing this file —
// that is precisely the condition that lets three adapter tasks run in
// parallel without touching each other's files.
var registry = map[string]Adapter{}

// register adds an adapter to the registry. Each adapter file calls it in
// init():
//
//	func init() { register(ClaudeAdapter{}) }
func register(a Adapter) {
	registry[a.Name()] = a
}

// Resolve returns the adapter for an agent label. ok=false for an unknown
// agent.
func Resolve(name string) (Adapter, bool) {
	a, ok := registry[name]
	return a, ok
}
