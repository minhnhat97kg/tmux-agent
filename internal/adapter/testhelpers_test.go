package adapter

import (
	"testing"

	"tmux-agent/internal/event"
)

// minimalPayload returns the minimal payload for Parse to accept a given
// Kind — matching the reference implementation adapter/mod.rs::minimal_payload.
// The three Kinds that have required fields (ActivityLog needs tool_name;
// SubagentStart/Stop need agent_type) must get a payload other than {} —
// otherwise AssertTableDriftFree would report a false failure (Parse
// returning ok=false not because a branch is missing, but because a
// required field is missing).
//
// agentName distinguishes DIFFERENT payload shapes for the SAME Kind:
// Antigravity nests the tool under "toolCall.name" (see antigravity.go),
// not flat "tool_name" like Claude/Codex/Pi/OpenCode.
func minimalPayload(agentName string, kind event.Kind) map[string]any {
	if agentName == antigravityAgentName && kind == event.ActivityLog {
		return map[string]any{"toolCall": map[string]any{"name": "view_file"}}
	}
	switch kind {
	case event.ActivityLog:
		return map[string]any{"tool_name": "Read"}
	case event.SubagentStart, event.SubagentStop:
		return map[string]any{"agent_type": "Explore"}
	default:
		return map[string]any{}
	}
}

// AssertTableDriftFree asserts that the registration table and Parse() agree
// in both directions.
//
//  1. Table → Parse: for every entry in the table, calling
//     Parse(kind.ExternalName(), minimal payload) must return ok=true with
//     the Kind matching what the table declares.
//  2. Parse → Table: for every kind in event.All, if Parse accepts it then
//     it MUST be in the table. This direction catches the "added a parse
//     branch but forgot to update the table" bug — the kind of bug that
//     leaves a hook silently unregistered forever.
func AssertTableDriftFree(t *testing.T, a Adapter) {
	t.Helper()
	name := a.Name()
	table := a.Registrations()

	for _, reg := range table {
		payload := minimalPayload(name, reg.Kind)
		got, ok := a.Parse(reg.Kind.ExternalName(), payload)
		if !ok {
			t.Fatalf("%s: registration table declares %q but Parse() returned ok=false — missing parse branch", name, reg.Kind)
		}
		if got.Kind != reg.Kind {
			t.Fatalf("%s: table declares %q but Parse() produced %q", name, reg.Kind, got.Kind)
		}
	}

	inTable := func(k event.Kind) bool {
		for _, reg := range table {
			if reg.Kind == k {
				return true
			}
		}
		return false
	}

	for _, kind := range event.All {
		_, accepted := a.Parse(kind.ExternalName(), minimalPayload(name, kind))
		if accepted && !inTable(kind) {
			t.Fatalf("%s: Parse() accepts %q but the registration table does not have it — add it to Registrations()", name, kind)
		}
	}
}
