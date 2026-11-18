// Package setup generates hook configuration for the user to paste into the
// agent's config file (Claude Code's settings.json, Codex's hooks.json).
//
// The SINGLE SOURCE OF TRUTH for trigger/matcher/external-name is the
// registration table adapter.Adapter.Registrations() (see the doc comment on
// adapter.Registration) — this package does not hardcode the trigger list.
// That same table also locks the README and assets/hooks/hooks.json
// (internal/adapter/hooks_json_test.go). If setup typed triggers by hand, the
// project would end up with a third source of truth and it would silently
// drift.
//
// OpenCode is DELIBERATELY absent from setup's agent list: it doesn't
// configure through a Claude/Codex-style settings.json file but instead uses
// a pre-bundled JavaScript plugin (assets/.opencode/plugins/tmux-agent-sidebar.js)
// that calls the hook itself — there's no JSON snippet to generate and paste.
package setup

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"tmux-agent/internal/adapter"
	"tmux-agent/internal/version"
)

// supportedAgents lists the agents setup knows how to generate a snippet
// for, in the order they appear in the full output. This is NOT trigger
// data — it's just "which agents use the settings.json mechanism", something
// adapter.Adapter doesn't (and shouldn't) know about, so it stays here
// instead of in the registration table.
var supportedAgents = []string{"claude", "codex", "pi"}

// configPaths is the default config file path for each agent, under the
// user's HOME. Used only for display in the output — setup never reads or
// writes these files.
var configPaths = map[string]string{
	"claude": "~/.claude/settings.json",
	"codex":  "~/.codex/hooks.json",
	// ~/.pi/settings.json: not verified against an actual Pi install (unlike
	// claude/codex) — inferred from the PROJECT-level ".pi/settings.json"
	// convention confirmed by packages/coding-agent/docs/settings.md
	// (badlogic/pi-mono), applying that same convention at the global level
	// under HOME. If wrong, it only affects the display line in setup's
	// output, with no impact on the generated JSON snippet (the snippet is
	// correct per Registrations() regardless of this display path).
	"pi": "~/.pi/settings.json",
}

// FallbackHookScript is the fallback path when ResolveHookScript can't
// locate a real assets/hook.sh around the running binary — the location
// where TPM (tmux plugin manager) installs this plugin by default. The
// tilde is deliberately not expanded (the user pastes it directly into
// settings.json; expanding the tilde is the shell's job when the hook runs,
// not setup's).
const FallbackHookScript = "~/.tmux/plugins/tmux-agent/assets/hook.sh"

// maxWalkUpDepth is the maximum number of directory levels to walk up from
// the binary's directory when looking for assets/hook.sh. This repo builds
// the binary at <repo>/bin/tmux-agent and places hook.sh at
// <repo>/assets/hook.sh (walking up exactly 1 level from bin/ is enough) —
// 3 extra levels of headroom for other build layouts (e.g. nested through an
// intermediate directory) without having to change this constant.
const maxWalkUpDepth = 3

// ResolvedHookScript is the result of locating hook.sh.
type ResolvedHookScript struct {
	Path string
	// Detected = false means no real hook.sh was found on disk around the
	// binary — Path is then FallbackHookScript, and the generated commands
	// may point to the wrong place if the user installed elsewhere. Dispatch
	// warns on stderr when this happens.
	Detected bool
}

// ResolveHookScript locates assets/hook.sh relative to the running binary.
//
// Strategy: get the binary path via os.Executable(), then walk up at most
// maxWalkUpDepth directory levels from there, checking at each level whether
// "<level>/assets/hook.sh" exists — matching this repo's actual layout
// (binary in <repo>/bin/, hook.sh in <repo>/assets/, unlike the reference
// implementation which places hook.sh at the root). Not found: returns
// FallbackHookScript, Detected=false. Never panics.
func ResolveHookScript() ResolvedHookScript {
	if path, ok := walkUpForHookScript(maxWalkUpDepth); ok {
		return ResolvedHookScript{Path: path, Detected: true}
	}
	return ResolvedHookScript{Path: FallbackHookScript, Detected: false}
}

func walkUpForHookScript(maxDepth int) (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	dir := filepath.Dir(exe)
	for i := 0; i <= maxDepth; i++ {
		candidate := filepath.Join(dir, "assets", "hook.sh")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
	return "", false
}

// ShellQuote safely wraps s for use as a SINGLE shell argument in the
// generated hook command, equivalent to Python's shlex.quote.
//
// Fast path: a string made up only of characters bash doesn't interpret
// specially is returned verbatim — important because wrapping in single
// quotes would block tilde (~) expansion of FallbackHookScript, breaking it
// exactly when it's needed most (the fallback path).
//
// Slow path: wrap in single quotes, escaping any single quote inside using
// the standard POSIX escape sequence (close the single quote, one
// backslash-escaped single quote, then reopen the single quote) — safe with
// spaces, $, ;, backticks, and other shell special characters.
func ShellQuote(s string) string {
	safe := s != ""
	if safe {
		for _, r := range s {
			if !isShellSafeRune(r) {
				safe = false
				break
			}
		}
	}
	if safe {
		return s
	}

	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('\'')
	for _, r := range s {
		if r == '\'' {
			b.WriteString(`'\''`)
		} else {
			b.WriteRune(r)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

func isShellSafeRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	}
	switch r {
	case '/', '-', '_', '.', '~', '+', '=', ',', '@', ':':
		return true
	}
	return false
}

// formatHookCommand builds the `bash <hook_script> <agent> <event>` command
// with correct POSIX quoting so any install path (even one with spaces)
// stays safe.
func formatHookCommand(hookScript, agent, externalName string) string {
	return fmt.Sprintf("bash %s %s %s", ShellQuote(hookScript), agent, externalName)
}

// HookEntry is one row in the normalized hooks[] of the full output — one
// row per Registration, preserving the order Registrations() returns.
type HookEntry struct {
	Trigger string `json:"trigger"`
	Matcher string `json:"matcher"`
	Event   string `json:"event"`
	Command string `json:"command"`
}

// AgentEntry is the generated configuration result for one agent in the
// full output.
type AgentEntry struct {
	ConfigPath string         `json:"config_path"`
	Hooks      []HookEntry    `json:"hooks"`
	Snippet    map[string]any `json:"snippet"`
}

// BuildAgentSnippet builds the JSON block `{"hooks": {...}}` ready to paste
// directly into the agent's config file. Reads ONLY from
// adapter.Resolve(agent).Registrations() and Kind.ExternalName() — the
// trigger list is not duplicated here.
//
// Returns ok=false when the agent isn't in supportedAgents (either it
// doesn't exist in the adapter registry, or it exists but doesn't use the
// settings.json mechanism — currently only OpenCode falls in this
// category).
func BuildAgentSnippet(agentName, hookScript string) (map[string]any, bool) {
	if _, known := configPaths[agentName]; !known {
		return nil, false
	}
	a, ok := adapter.Resolve(agentName)
	if !ok {
		return nil, false
	}

	hooks := map[string]any{}
	for _, reg := range a.Registrations() {
		entry := map[string]any{
			"matcher": reg.Matcher,
			"hooks": []map[string]any{
				{"type": "command", "command": formatHookCommand(hookScript, agentName, reg.Kind.ExternalName())},
			},
		}
		existing, _ := hooks[reg.Trigger].([]map[string]any)
		hooks[reg.Trigger] = append(existing, entry)
	}
	return map[string]any{"hooks": hooks}, true
}

// buildAgentEntry builds the full AgentEntry for an agent already known to
// be valid (agentName comes from supportedAgents, not from the user).
func buildAgentEntry(agentName, hookScript string) AgentEntry {
	a, ok := adapter.Resolve(agentName)
	if !ok {
		// Doesn't happen for an agentName drawn from supportedAgents (both
		// registered in the adapter package's init()) — but if things ever
		// drift, return an empty entry instead of panicking or indexing
		// wrong.
		return AgentEntry{ConfigPath: configPaths[agentName]}
	}

	regs := a.Registrations()
	hooks := make([]HookEntry, 0, len(regs))
	for _, reg := range regs {
		hooks = append(hooks, HookEntry{
			Trigger: reg.Trigger,
			Matcher: reg.Matcher,
			Event:   reg.Kind.ExternalName(),
			Command: formatHookCommand(hookScript, agentName, reg.Kind.ExternalName()),
		})
	}
	snippet, _ := BuildAgentSnippet(agentName, hookScript)
	return AgentEntry{
		ConfigPath: configPaths[agentName],
		Hooks:      hooks,
		Snippet:    snippet,
	}
}

// BuildSetupOutput builds the full output: binary version, the located (or
// fallback) hook.sh path, and one AgentEntry per agent in supportedAgents.
func BuildSetupOutput(hookScript string) map[string]any {
	agents := map[string]any{}
	for _, agentName := range supportedAgents {
		agents[agentName] = buildAgentEntry(agentName, hookScript)
	}
	return map[string]any{
		"version":     version.Version,
		"hook_script": hookScript,
		"agents":      agents,
	}
}

// runSetup is the pure core: decides the exit code and the JSON to print
// (nil = print nothing, error path). Separated from Dispatch so tests don't
// need to capture real stderr.
func runSetup(args []string, hookScript string, stderr io.Writer) (int, any) {
	switch len(args) {
	case 0:
		return 0, BuildSetupOutput(hookScript)
	case 1:
		snippet, ok := BuildAgentSnippet(args[0], hookScript)
		if !ok {
			fmt.Fprintf(stderr, "error: unknown agent %q (want %s)\n", args[0], strings.Join(supportedAgents, " or "))
			return 2, nil
		}
		return 0, snippet
	default:
		fmt.Fprintln(stderr, "usage: tmux-agent setup [agent]")
		return 2, nil
	}
}

// Dispatch is the entry point for the `tmux-agent setup [agent]` command.
// Prints JSON to stdout; any error is printed clearly to stderr — this is a
// user-initiated action (unlike the background hook path which follows the
// soft-degradation philosophy), so errors must never be swallowed silently.
func Dispatch(args []string, stdout, stderr io.Writer) int {
	resolved := ResolveHookScript()
	if !resolved.Detected {
		fmt.Fprintf(stderr,
			"warning: could not locate hook.sh around the running binary; using the fallback path %q. If you installed elsewhere, manually fix the \"command\" values below before pasting them.\n",
			resolved.Path)
	}

	code, out := runSetup(args, resolved.Path, stderr)
	if out != nil {
		enc, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "error: could not serialize setup output: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(enc))
	}
	return code
}
