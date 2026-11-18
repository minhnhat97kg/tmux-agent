package setup

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"tmux-agent/internal/adapter"
	"tmux-agent/internal/version"
)

const fakeHook = "/fake/hook.sh"

// ─── ShellQuote ────────────────────────────────────────────────────────────

func TestShellQuoteSafeStringUnchanged(t *testing.T) {
	cases := []string{
		"hello",
		"/fake/hook.sh",
		"~/.tmux/plugins/tmux-agent/assets/hook.sh",
		"/Users/alice/.tmux/plugins/tmux-agent/assets/hook.sh",
	}
	for _, s := range cases {
		if got := ShellQuote(s); got != s {
			t.Errorf("ShellQuote(%q) = %q, want it unchanged", s, got)
		}
	}
}

func TestShellQuoteEmptyStringBecomesTwoSingleQuotes(t *testing.T) {
	if got := ShellQuote(""); got != "''" {
		t.Fatalf("ShellQuote(\"\") = %q, want %q", got, "''")
	}
}

func TestShellQuotePathWithSpace(t *testing.T) {
	if got := ShellQuote("/Users/a b/hook.sh"); got != "'/Users/a b/hook.sh'" {
		t.Fatalf("got %q", got)
	}
}

func TestShellQuoteEmbeddedSingleQuote(t *testing.T) {
	if got := ShellQuote("a'b"); got != `'a'\''b'` {
		t.Fatalf("got %q, want %q", got, `'a'\''b'`)
	}
}

func TestShellQuoteShellSpecialChars(t *testing.T) {
	if got := ShellQuote("$(rm -rf /)"); got != "'$(rm -rf /)'" {
		t.Fatalf("got %q", got)
	}
	if got := ShellQuote("a;b|c`d`"); got != "'a;b|c`d`'" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatHookCommandSafePathNotQuoted(t *testing.T) {
	got := formatHookCommand("/fake/hook.sh", "claude", "session-start")
	want := "bash /fake/hook.sh claude session-start"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatHookCommandPathWithSpaceIsQuoted(t *testing.T) {
	got := formatHookCommand("/path with space/hook.sh", "claude", "session-start")
	want := "bash '/path with space/hook.sh' claude session-start"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// ─── ResolveHookScript ─────────────────────────────────────────────────────

// TestResolveHookScriptDoesNotPanicAndFallbackIsConstant can't pin down
// os.Executable() (the test binary doesn't have assets/hook.sh next to it),
// so it only locks in the contract: Detected=false means Path must be
// FallbackHookScript; Detected=true means the file must actually exist on
// disk.
func TestResolveHookScriptDoesNotPanicAndFallbackIsConstant(t *testing.T) {
	resolved := ResolveHookScript()
	if !resolved.Detected {
		if resolved.Path != FallbackHookScript {
			t.Fatalf("Detected=false but Path = %q, want the fallback %q", resolved.Path, FallbackHookScript)
		}
		return
	}
	if resolved.Path == "" {
		t.Fatal("Detected=true but Path is empty")
	}
}

// ─── BuildAgentSnippet: source of truth is Registrations(), not hardcoded ──

func registrationsOrFatal(t *testing.T, agentName string) []adapter.Registration {
	t.Helper()
	a, ok := adapter.Resolve(agentName)
	if !ok {
		t.Fatalf("adapter.Resolve(%q) = false, the agent must exist in the registry for this test to make sense", agentName)
	}
	return a.Registrations()
}

func TestBuildAgentSnippetUnknownAgentReturnsFalse(t *testing.T) {
	if _, ok := BuildAgentSnippet("not-an-agent", fakeHook); ok {
		t.Fatal("ok = true, want false for an unknown agent")
	}
}

// TestBuildAgentSnippetOpenCodeDeliberatelyExcluded locks in a deliberate design
// decision: OpenCode exists in the adapter registry (adapter.Resolve
// succeeds) but does NOT use the settings.json mechanism, so setup refuses
// to generate a snippet for it instead of producing a meaningless JSON
// blob (OpenCode's trigger is the name of a JS hook function, not a real
// settings.json trigger name).
func TestBuildAgentSnippetOpenCodeDeliberatelyExcluded(t *testing.T) {
	if _, ok := adapter.Resolve("opencode"); !ok {
		t.Fatal("opencode must exist in the adapter registry for this test to make sense")
	}
	if _, ok := BuildAgentSnippet("opencode", fakeHook); ok {
		t.Fatal("ok = true, want false — opencode doesn't use the settings.json snippet")
	}
}

func TestBuildAgentSnippetClaudeHasHooksKey(t *testing.T) {
	v, ok := BuildAgentSnippet("claude", fakeHook)
	if !ok {
		t.Fatal("ok = false, want true for claude")
	}
	if _, has := v["hooks"]; !has {
		t.Fatal("missing the top-level \"hooks\" key")
	}
}

// TestBuildAgentSnippetClaudeCoversEveryRegistration locks in that the generated
// trigger set matches Registrations() EXACTLY — pulled from the adapter,
// not hand-typed in this test.
func TestBuildAgentSnippetClaudeCoversEveryRegistration(t *testing.T) {
	regs := registrationsOrFatal(t, "claude")
	v, ok := BuildAgentSnippet("claude", fakeHook)
	if !ok {
		t.Fatal("ok = false")
	}
	hooks, ok := v["hooks"].(map[string]any)
	if !ok {
		t.Fatal("\"hooks\" is not an object")
	}

	wantTriggers := map[string]bool{}
	for _, r := range regs {
		wantTriggers[r.Trigger] = true
	}
	if len(hooks) != len(wantTriggers) {
		t.Fatalf("snippet has %d triggers, Registrations() has %d unique triggers", len(hooks), len(wantTriggers))
	}
	for trigger := range wantTriggers {
		if _, ok := hooks[trigger]; !ok {
			t.Errorf("snippet is missing trigger %q present in Registrations()", trigger)
		}
	}
}

// TestBuildAgentSnippetEntryHasCorrectShape locks in the shape of one
// entry: a single-element array, matcher matching Registrations(), and
// exactly one {type, command} action inside. Uses claude's first
// registration instead of hard-typing "SessionStart"/"session-start".
func TestBuildAgentSnippetEntryHasCorrectShape(t *testing.T) {
	regs := registrationsOrFatal(t, "claude")
	first := regs[0]

	v, ok := BuildAgentSnippet("claude", fakeHook)
	if !ok {
		t.Fatal("ok = false")
	}
	hooks := v["hooks"].(map[string]any)
	entries, ok := hooks[first.Trigger].([]map[string]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("trigger %q: entries = %#v, want a single-element array", first.Trigger, hooks[first.Trigger])
	}
	entry := entries[0]
	if entry["matcher"] != first.Matcher {
		t.Errorf("matcher = %v, want %q", entry["matcher"], first.Matcher)
	}
	inner, ok := entry["hooks"].([]map[string]any)
	if !ok || len(inner) != 1 {
		t.Fatalf("inner hooks = %#v, want a single-element array", entry["hooks"])
	}
	if inner[0]["type"] != "command" {
		t.Errorf("type = %v, want \"command\"", inner[0]["type"])
	}
	wantCmd := formatHookCommand(fakeHook, "claude", first.Kind.ExternalName())
	if inner[0]["command"] != wantCmd {
		t.Errorf("command = %v, want %q", inner[0]["command"], wantCmd)
	}
}

// TestBuildAgentSnippetCodexMatcherMatchesRegistrations locks in that the
// matcher for EVERY codex trigger matches the value in Registrations() —
// not hand-typing "startup|resume" or "" here, read straight from the
// registration table.
func TestBuildAgentSnippetCodexMatcherMatchesRegistrations(t *testing.T) {
	regs := registrationsOrFatal(t, "codex")
	v, ok := BuildAgentSnippet("codex", fakeHook)
	if !ok {
		t.Fatal("ok = false")
	}
	hooks := v["hooks"].(map[string]any)
	for _, reg := range regs {
		entries, ok := hooks[reg.Trigger].([]map[string]any)
		if !ok || len(entries) == 0 {
			t.Fatalf("missing trigger %q in the snippet", reg.Trigger)
		}
		found := false
		for _, e := range entries {
			if e["matcher"] == reg.Matcher {
				found = true
			}
		}
		if !found {
			t.Errorf("trigger %q: no entry has matcher = %q", reg.Trigger, reg.Matcher)
		}
	}
}

// ─── BuildSetupOutput ──────────────────────────────────────────────────────

func TestBuildSetupOutputHasAllTopLevelKeys(t *testing.T) {
	v := BuildSetupOutput(fakeHook)
	if v["version"] != version.Version {
		t.Errorf("version = %v, want %q", v["version"], version.Version)
	}
	if v["hook_script"] != fakeHook {
		t.Errorf("hook_script = %v, want %q", v["hook_script"], fakeHook)
	}
	agents, ok := v["agents"].(map[string]any)
	if !ok {
		t.Fatal("agents is not an object")
	}
	if len(agents) != len(supportedAgents) {
		t.Fatalf("agents has %d entries, want %d (supportedAgents)", len(agents), len(supportedAgents))
	}
	for _, name := range supportedAgents {
		if _, ok := agents[name]; !ok {
			t.Errorf("missing agent %q in the full output", name)
		}
	}
}

// TestBuildSetupOutputHookCountMatchesRegistrationTable locks in that len(hooks[])
// for each agent matches len(Registrations()) exactly — if setup hardcoded
// something and drifted from the registration table, this test catches it
// the moment someone adds/removes a Registration.
func TestBuildSetupOutputHookCountMatchesRegistrationTable(t *testing.T) {
	v := BuildSetupOutput(fakeHook)
	agents := v["agents"].(map[string]any)
	for _, name := range supportedAgents {
		regs := registrationsOrFatal(t, name)
		entry, ok := agents[name].(AgentEntry)
		if !ok {
			t.Fatalf("agents[%q] is not an AgentEntry", name)
		}
		if len(entry.Hooks) != len(regs) {
			t.Errorf("%s: len(Hooks) = %d, want %d (len(Registrations()))", name, len(entry.Hooks), len(regs))
		}
	}
}

// TestBuildSetupOutputNormalizedEntryMatchesRegistrations locks in every field of
// the first HookEntry against the first Registration — trigger, matcher,
// event (via Kind.ExternalName()), and command.
func TestBuildSetupOutputNormalizedEntryMatchesRegistrations(t *testing.T) {
	v := BuildSetupOutput(fakeHook)
	agents := v["agents"].(map[string]any)
	for _, name := range supportedAgents {
		regs := registrationsOrFatal(t, name)
		entry := agents[name].(AgentEntry)
		for i, reg := range regs {
			got := entry.Hooks[i]
			if got.Trigger != reg.Trigger {
				t.Errorf("%s[%d]: Trigger = %q, want %q", name, i, got.Trigger, reg.Trigger)
			}
			if got.Matcher != reg.Matcher {
				t.Errorf("%s[%d]: Matcher = %q, want %q", name, i, got.Matcher, reg.Matcher)
			}
			wantEvent := reg.Kind.ExternalName()
			if got.Event != wantEvent {
				t.Errorf("%s[%d]: Event = %q, want %q", name, i, got.Event, wantEvent)
			}
			wantCmd := formatHookCommand(fakeHook, name, wantEvent)
			if got.Command != wantCmd {
				t.Errorf("%s[%d]: Command = %q, want %q", name, i, got.Command, wantCmd)
			}
		}
	}
}

// TestBuildSetupOutputSnippetMatchesBuildAgentSnippet guards against the two
// snippet-generation paths drifting apart: the snippet nested in the full
// output must be identical to the standalone snippet from BuildAgentSnippet.
func TestBuildSetupOutputSnippetMatchesBuildAgentSnippet(t *testing.T) {
	v := BuildSetupOutput(fakeHook)
	agents := v["agents"].(map[string]any)
	for _, name := range supportedAgents {
		entry := agents[name].(AgentEntry)
		standalone, ok := BuildAgentSnippet(name, fakeHook)
		if !ok {
			t.Fatalf("BuildAgentSnippet(%q, ...) ok = false", name)
		}
		if !reflect.DeepEqual(entry.Snippet, standalone) {
			t.Errorf("%s: snippet in the full output differs from the standalone snippet", name)
		}
	}
}

// ─── runSetup / Dispatch ───────────────────────────────────────────────────

func TestRunSetupNoArgsReturnsFullOutput(t *testing.T) {
	var errOut bytes.Buffer
	code, out := runSetup(nil, fakeHook, &errOut)
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatal("out is not map[string]any")
	}
	if _, has := m["agents"]; !has {
		t.Fatal("missing \"agents\" in the full output")
	}
}

func TestRunSetupOneAgentReturnsItsSnippet(t *testing.T) {
	for _, name := range supportedAgents {
		var errOut bytes.Buffer
		code, out := runSetup([]string{name}, fakeHook, &errOut)
		if code != 0 {
			t.Fatalf("%s: code = %d, want 0", name, code)
		}
		m, ok := out.(map[string]any)
		if !ok {
			t.Fatalf("%s: out is not map[string]any", name)
		}
		if _, has := m["hooks"]; !has {
			t.Fatalf("%s: missing \"hooks\"", name)
		}
		for _, extra := range []string{"version", "hook_script", "agents"} {
			if _, has := m[extra]; has {
				t.Fatalf("%s: a standalone snippet must not have key %q", name, extra)
			}
		}
	}
}

func TestRunSetupUnknownAgentReturns2(t *testing.T) {
	var errOut bytes.Buffer
	code, out := runSetup([]string{"gemini"}, fakeHook, &errOut)
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if out != nil {
		t.Fatal("out must be nil on error")
	}
	if !strings.Contains(errOut.String(), "gemini") {
		t.Fatalf("stderr = %q, want it to mention the unknown agent name", errOut.String())
	}
}

func TestRunSetupTooManyArgsReturns2(t *testing.T) {
	var errOut bytes.Buffer
	code, out := runSetup([]string{"claude", "extra"}, fakeHook, &errOut)
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if out != nil {
		t.Fatal("out must be nil on error")
	}
	if !strings.Contains(errOut.String(), "usage") {
		t.Fatalf("stderr = %q, want a usage message", errOut.String())
	}
}

// TestDispatchPrintsValidJSONSnippet verifies end-to-end through Dispatch (the
// real entry point of the setup command): the output for one agent must
// parse and carry the correct "hooks" key.
func TestDispatchPrintsValidJSONSnippet(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Dispatch([]string{"claude"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr = %q", code, stderr.String())
	}
	var v map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &v); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout.String())
	}
	if _, has := v["hooks"]; !has {
		t.Fatal("missing \"hooks\" in Dispatch output")
	}
}

// TestDispatchWarnsOnStderrWhenHookScriptNotLocated: the test binary
// doesn't have assets/hook.sh next to it, so ResolveHookScript() always
// has Detected=false when running under go test — Dispatch must clearly
// warn on stderr, not swallow it silently (the error philosophy for a
// user-initiated action).
func TestDispatchWarnsOnStderrWhenHookScriptNotLocated(t *testing.T) {
	resolved := ResolveHookScript()
	if resolved.Detected {
		t.Skip("this test environment located a real hook.sh — nothing to lock in here")
	}
	var stdout, stderr bytes.Buffer
	Dispatch(nil, &stdout, &stderr)
	if !strings.Contains(stderr.String(), FallbackHookScript) {
		t.Fatalf("stderr = %q, want it to mention the fallback path %q", stderr.String(), FallbackHookScript)
	}
}

func TestDispatchNoArgsReturnsFullOutputAndVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Dispatch(nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	var v map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &v); err != nil {
		t.Fatalf("stdout is not valid JSON: %v", err)
	}
	if v["version"] != version.Version {
		t.Errorf("version = %v, want %q", v["version"], version.Version)
	}
}
