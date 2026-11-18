package adapter

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Paths relative to internal/adapter (where the test runs) to the repo root.
const (
	claudeHooksJSONPath  = "../../assets/hooks/hooks.json"
	opencodePluginJSPath = "../../assets/.opencode/plugins/tmux-agent-sidebar.js"
	hooksAssetsDir       = "../../assets/hooks"
)

// ─── Real JSON structure of assets/hooks/hooks.json ───────────────────────

type hooksJSONFile struct {
	Hooks map[string][]hooksJSONMatcherGroup `json:"hooks"`
}
type hooksJSONMatcherGroup struct {
	Matcher string           `json:"matcher"`
	Hooks   []hooksJSONEntry `json:"hooks"`
}
type hooksJSONEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

type hooksJSONRow struct{ Matcher, ExternalName string }

// loadClaudeHooksJSON reads and extracts assets/hooks/hooks.json into a
// map[trigger] -> (matcher, external name). The file currently only holds
// Claude Code's configuration (the agent is always "claude" on every command
// line) — if a trigger calls a different agent, the test deliberately FAILS
// instead of silently skipping it, since that's a signal hooks.json has grown
// beyond this test's assumptions.
func loadClaudeHooksJSON(t *testing.T) map[string]hooksJSONRow {
	t.Helper()
	data, err := os.ReadFile(claudeHooksJSONPath)
	if err != nil {
		t.Fatalf("read %s: %v", claudeHooksJSONPath, err)
	}
	var f hooksJSONFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("parse %s: %v", claudeHooksJSONPath, err)
	}

	got := map[string]hooksJSONRow{}
	for trigger, groups := range f.Hooks {
		if len(groups) != 1 {
			t.Fatalf("trigger %q has %d matcher groups, want exactly 1", trigger, len(groups))
		}
		g := groups[0]
		if len(g.Hooks) != 1 {
			t.Fatalf("trigger %q has %d commands, want exactly 1", trigger, len(g.Hooks))
		}
		fields := strings.Fields(g.Hooks[0].Command)
		if len(fields) < 2 {
			t.Fatalf("trigger %q: command %q doesn't have enough tokens", trigger, g.Hooks[0].Command)
		}
		agentTok := fields[len(fields)-2]
		externalName := fields[len(fields)-1]
		if agentTok != "claude" {
			t.Fatalf("trigger %q calls agent %q — hooks.json has grown, this test needs updating accordingly (see Part C, 2b-dispatch.md)", trigger, agentTok)
		}
		got[trigger] = hooksJSONRow{Matcher: g.Matcher, ExternalName: externalName}
	}
	return got
}

// TestClaudeHooksJSONMatchesRegistrationTable is a mandatory ANTI-DRIFT test per spec
// part 2 (internal/adapter/adapter.go: "The registration table ... is the
// SINGLE SOURCE OF TRUTH for ... assets/hooks/hooks.json"). It cross-checks
// in both directions:
//
//  1. The trigger counts match (no extras, no missing).
//  2. For each trigger: the matcher matches verbatim, and the external name
//     derived from the bash command in hooks.json matches
//     Kind.ExternalName() from Registrations().
//
// Changing Registrations() and forgetting hooks.json (or vice versa) makes
// this test fail.
func TestClaudeHooksJSONMatchesRegistrationTable(t *testing.T) {
	fromJSON := loadClaudeHooksJSON(t)
	regs := (ClaudeAdapter{}).Registrations()

	if len(regs) != len(fromJSON) {
		t.Fatalf("Registrations() has %d entries, hooks.json has %d triggers — MISMATCH", len(regs), len(fromJSON))
	}

	seen := map[string]bool{}
	for _, reg := range regs {
		seen[reg.Trigger] = true
		row, ok := fromJSON[reg.Trigger]
		if !ok {
			t.Errorf("Registrations() has trigger %q but hooks.json does not", reg.Trigger)
			continue
		}
		if row.Matcher != reg.Matcher {
			t.Errorf("trigger %q: hooks.json matcher = %q, Registrations() = %q", reg.Trigger, row.Matcher, reg.Matcher)
		}
		wantExternal := reg.Kind.ExternalName()
		if row.ExternalName != wantExternal {
			t.Errorf("trigger %q: hooks.json calls external name %q, Kind.ExternalName() = %q", reg.Trigger, row.ExternalName, wantExternal)
		}
	}
	for trigger := range fromJSON {
		if !seen[trigger] {
			t.Errorf("hooks.json has trigger %q but Registrations() does not", trigger)
		}
	}
}

// ─── Codex: confirm it remains OUT OF SCOPE (no static config file) ──────

// TestCodexHasNoStaticConfigFile is a cheap tripwire: if someone adds a
// static config file for Codex under assets/hooks/ (today it only has
// Claude's hooks.json) but forgets to write a test cross-checking
// CodexAdapter{}.Registrations() against it, this test raises the alarm
// instead of silently letting new debt pile onto old debt.
func TestCodexHasNoStaticConfigFile(t *testing.T) {
	entries, err := os.ReadDir(hooksAssetsDir)
	if err != nil {
		t.Fatalf("read directory %s: %v", hooksAssetsDir, err)
	}
	for _, e := range entries {
		if strings.Contains(strings.ToLower(e.Name()), "codex") {
			t.Fatalf("found a static config file for Codex (%s) — Part C of 2b-dispatch.md needs to be extended to cross-check it", e.Name())
		}
	}
}

// ─── OpenCode: cross-check as text against the plugin JS (LIMITED, see above) ──

// TestOpenCodeRegistrationsMatchPluginJS matches TEXT STRINGS — Go cannot run
// JavaScript. For each Registration of OpenCodeAdapter:
//
//   - The Trigger must appear as-is in the JS. These are V2 API paths
//     (ctx.event.subscribe, ctx.session.hook, ctx.tool.hook), not quoted keys
//     like the V1 plugin's "chat.message".
//   - The Matcher part before ':' (if any — "session.status:busy" only has
//     "session.status" as a standalone literal in JS, "busy" is a sub-branch
//     inside a switch) must appear as a quoted string in the JS
//     ("session.created", "session.status", …). The part after ':' (if any)
//     must also appear as a quoted string ("busy", "idle").
//
// LIMITATION: this is NOT proof the JS logic is correct — it's only a net for
// catching typo'd event strings. Changing control flow in the JS (e.g.
// removing a case entirely but leaving the string in a comment) will NOT be
// caught by this test.
func TestOpenCodeRegistrationsMatchPluginJS(t *testing.T) {
	data, err := os.ReadFile(opencodePluginJSPath)
	if err != nil {
		t.Fatalf("read %s: %v", opencodePluginJSPath, err)
	}
	content := string(data)

	for _, reg := range (OpenCodeAdapter{}).Registrations() {
		if !strings.Contains(content, reg.Trigger) {
			t.Errorf("trigger %q (Kind %q) not found in the plugin JS", reg.Trigger, reg.Kind)
		}

		if reg.Matcher == "" {
			continue
		}
		base, suffix, hasSuffix := strings.Cut(reg.Matcher, ":")
		if !strings.Contains(content, `"`+base+`"`) {
			t.Errorf("matcher %q (Kind %q): string %q not found in the plugin JS", reg.Matcher, reg.Kind, base)
		}
		if hasSuffix && !strings.Contains(content, `"`+suffix+`"`) {
			t.Errorf("matcher %q (Kind %q): string %q not found in the plugin JS", reg.Matcher, reg.Kind, suffix)
		}
	}
}
