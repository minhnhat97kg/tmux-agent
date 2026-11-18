package worktree

import (
	"slices"
	"testing"
)

func TestModesForClaudeReturnsClaudeModesByDefault(t *testing.T) {
	got := ModesFor("claude")
	if !slices.Equal(got, ClaudeModes) {
		t.Errorf("ModesFor(claude) = %v, want %v", got, ClaudeModes)
	}
}

func TestModesForCodexReturnsCodexModes(t *testing.T) {
	got := ModesFor("codex")
	if !slices.Equal(got, CodexModes) {
		t.Errorf("ModesFor(codex) = %v, want %v", got, CodexModes)
	}
}

func TestModesForOpencodeReturnsOpencodeModes(t *testing.T) {
	got := ModesFor("opencode")
	if !slices.Equal(got, OpenCodeModes) {
		t.Errorf("ModesFor(opencode) = %v, want %v", got, OpenCodeModes)
	}
}

func TestModesForUnknownAgentFallsBackToClaudeModes(t *testing.T) {
	if !slices.Equal(ModesFor("gemini"), ClaudeModes) {
		t.Error("ModesFor(gemini) must fall back to ClaudeModes")
	}
	if !slices.Equal(ModesFor(""), ClaudeModes) {
		t.Error("ModesFor(empty) must fall back to ClaudeModes")
	}
}

func TestAgentCommandClaudeVariants(t *testing.T) {
	cases := map[[2]string]string{
		{"claude", ""}:            "claude",
		{"claude", "default"}:     "claude",
		{"claude", "plan"}:        "claude --permission-mode plan",
		{"claude", "acceptEdits"}: "claude --permission-mode acceptEdits",
	}
	for in, want := range cases {
		if got := AgentCommand(in[0], in[1]); got != want {
			t.Errorf("AgentCommand(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestAgentCommandCodexVariants(t *testing.T) {
	cases := map[[2]string]string{
		{"codex", "default"}:           "codex",
		{"codex", ""}:                  "codex",
		{"codex", "auto"}:              "codex --full-auto",
		{"codex", "bypassPermissions"}: "codex --dangerously-bypass-approvals-and-sandbox",
	}
	for in, want := range cases {
		if got := AgentCommand(in[0], in[1]); got != want {
			t.Errorf("AgentCommand(%q, %q) = %q, want %q", in[0], in[1], got, want)
		}
	}
}

func TestAgentCommandOpencodeIgnoresMode(t *testing.T) {
	// OpenCode currently only has the default mode; every mode falls back to the bare binary.
	for _, mode := range []string{"default", "plan", ""} {
		if got := AgentCommand("opencode", mode); got != "opencode" {
			t.Errorf("AgentCommand(opencode, %q) = %q, want opencode", mode, got)
		}
	}
}

func TestAgentCommandUnknownAgentPassesThrough(t *testing.T) {
	if got := AgentCommand("gemini", "default"); got != "gemini" {
		t.Errorf("AgentCommand(gemini, default) = %q, want gemini", got)
	}
}

// Regression: an unknown/stale Claude mode (e.g. left over from a previous
// build, mode list has since changed) used to be forwarded straight into
// --permission-mode, causing `claude` to exit with an argument error right
// on spawn. Contract: fall back to running the bare agent.
func TestAgentCommandClaudeUnknownModeFallsBackToBareClaude(t *testing.T) {
	if got := AgentCommand("claude", "unknown"); got != "claude" {
		t.Errorf("AgentCommand(claude, unknown) = %q, want claude", got)
	}
	if got := AgentCommand("claude", "legacy-mode-from-v0"); got != "claude" {
		t.Errorf("AgentCommand(claude, legacy-mode-from-v0) = %q, want claude", got)
	}
}

func TestAgentCommandClaudeAllWhitelistedModesForwarded(t *testing.T) {
	for _, mode := range ClaudeModes {
		got := AgentCommand("claude", mode)
		want := "claude"
		if mode != "default" {
			want = "claude --permission-mode " + mode
		}
		if got != want {
			t.Errorf("mode %s: AgentCommand = %q, want %q (doc contract)", mode, got, want)
		}
	}
}

func TestAgentsListMatchesModesForDispatch(t *testing.T) {
	// Every agent in the public Agents list must return a non-empty mode
	// list, otherwise the mode-cycling popup would divide by zero.
	for _, agent := range Agents {
		if len(ModesFor(agent)) == 0 {
			t.Errorf("agent %s returned an empty mode list", agent)
		}
	}
}
