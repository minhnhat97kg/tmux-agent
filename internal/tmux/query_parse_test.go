package tmux

import (
	"reflect"
	"strings"
	"testing"

	"tmux-agent/internal/process"
)

// fullPaneFields returns a valid set of pane-level fields for tests to
// tweak individual cells. Indices match the pf* constants.
func fullPaneFields() []string {
	f := make([]string, minPaneFields)
	f[pfPaneActive] = "1"
	f[pfPaneStatus] = "running"
	f[pfPaneAttention] = ""
	f[pfAgent] = "claude"
	f[pfPaneName] = ""
	f[pfPaneCurrentPath] = "/home/u/proj"
	f[pfCurrentCommand] = "claude"
	f[pfPaneRole] = ""
	f[pfPaneID] = "%3"
	f[pfPrompt] = "fix the bug"
	f[pfPromptSource] = "user"
	f[pfStartedAt] = "1700000000"
	f[pfWaitReason] = ""
	f[pfPanePID] = "4242"
	f[pfSubagents] = ""
	f[pfPaneCwd] = ""
	f[pfPermissionMode] = "plan"
	f[pfWorktreeName] = ""
	f[pfWorktreeBranch] = ""
	f[pfSessionID] = "sess-abc"
	f[pfSidebarSpawned] = ""
	f[pfBgCmd] = ""
	return f
}

func TestParsePaneFieldsHappyPath(t *testing.T) {
	pane, ok := parsePaneFields(fullPaneFields(), nil)
	if !ok {
		t.Fatal("ok = false, want true")
	}

	if pane.PaneID != "%3" {
		t.Errorf("PaneID = %q", pane.PaneID)
	}
	if pane.Agent != AgentClaude {
		t.Errorf("Agent = %q", pane.Agent)
	}
	if pane.Status != StatusRunning {
		t.Errorf("Status = %q", pane.Status)
	}
	if pane.PermissionMode != ModePlan {
		t.Errorf("PermissionMode = %q", pane.PermissionMode)
	}
	if pane.StartedAt != 1700000000 {
		t.Errorf("StartedAt = %d", pane.StartedAt)
	}
	if pane.PanePID != 4242 {
		t.Errorf("PanePID = %d", pane.PanePID)
	}
	if pane.SessionID != "sess-abc" {
		t.Errorf("SessionID = %q", pane.SessionID)
	}
	// SessionName is filled in by the state layer; the query layer leaves it empty.
	if pane.SessionName != "" {
		t.Errorf("SessionName = %q, want empty", pane.SessionName)
	}
}

func TestParsePaneFieldsRejectsTooFewFields(t *testing.T) {
	if _, ok := parsePaneFields(fullPaneFields()[:minPaneFields-1], nil); ok {
		t.Fatal("a line with too few fields must be rejected")
	}
}

func TestParsePaneFieldsRejectsSidebarItself(t *testing.T) {
	f := fullPaneFields()
	f[pfPaneRole] = "sidebar"
	if _, ok := parsePaneFields(f, nil); ok {
		t.Fatal("the sidebar must not list itself")
	}
}

func TestParsePaneFieldsRejectsNonAgent(t *testing.T) {
	f := fullPaneFields()
	f[pfAgent] = ""
	if _, ok := parsePaneFields(f, nil); ok {
		t.Fatal("a pane with no agent must be skipped")
	}
}

func TestParsePaneFieldsPrefersPaneCwdOverCurrentPath(t *testing.T) {
	f := fullPaneFields()
	f[pfPaneCwd] = "/from/hook"
	pane, _ := parsePaneFields(f, nil)
	if pane.Path != "/from/hook" {
		t.Fatalf("Path = %q, want @pane_cwd to take priority", pane.Path)
	}

	f[pfPaneCwd] = ""
	pane, _ = parsePaneFields(f, nil)
	if pane.Path != "/home/u/proj" {
		t.Fatalf("Path = %q, want fallback to pane_current_path", pane.Path)
	}
}

func TestParsePaneFieldsCodexIgnoresPermissionMode(t *testing.T) {
	f := fullPaneFields()
	f[pfAgent] = "codex"
	f[pfCurrentCommand] = "node"
	f[pfPermissionMode] = "auto"

	pane, ok := parsePaneFields(f, nil)
	if !ok {
		t.Fatal("ok = false")
	}
	// Codex's hook doesn't write a permission mode; the value in tmux is stale junk.
	if pane.PermissionMode != ModeDefault {
		t.Fatalf("PermissionMode = %q, want default for codex", pane.PermissionMode)
	}
}

func TestParsePaneFieldsBadNumbersDegradeToZero(t *testing.T) {
	f := fullPaneFields()
	f[pfStartedAt] = "not-a-number"
	f[pfPanePID] = ""

	pane, ok := parsePaneFields(f, nil)
	if !ok {
		t.Fatal("a malformed number must not invalidate the whole line")
	}
	if pane.StartedAt != 0 || pane.PanePID != 0 {
		t.Fatalf("StartedAt = %d, PanePID = %d, want 0", pane.StartedAt, pane.PanePID)
	}
}

func TestParsePaneFieldsAttentionAndBgCmd(t *testing.T) {
	f := fullPaneFields()
	f[pfPaneAttention] = "notification"
	f[pfBgCmd] = "npm run dev"

	pane, _ := parsePaneFields(f, nil)
	if !pane.Attention {
		t.Error("Attention must be true when the field is non-empty")
	}
	if pane.BgShellCmd != "npm run dev" {
		t.Errorf("BgShellCmd = %q", pane.BgShellCmd)
	}
}

// Codex/OpenCode have no reliable exit hook (Codex has no such hook,
// OpenCode runs on Bun so the exit handler never runs). Once a pane has
// fallen back to a shell with no agent process underneath, the query layer
// must clean it up itself.
func TestParsePaneFieldsClearsDeadCodexPane(t *testing.T) {
	f := fullPaneFields()
	f[pfAgent] = "codex"
	f[pfCurrentCommand] = "zsh"

	fake := NewFake()
	for _, key := range clearedPaneKeys {
		fake.Set("", "set", "-t", "%3", "-p", "-u", key)
	}
	defer SetRunner(fake)()

	snap := process.FromPSOutput("4242 1 zsh zsh\n")
	if _, ok := parsePaneFields(f, snap); ok {
		t.Fatal("a dead codex pane must be dropped")
	}
	if len(fake.Calls()) != len(clearedPaneKeys) {
		t.Fatalf("want %d options cleared, got %d commands", len(clearedPaneKeys), len(fake.Calls()))
	}
}

func TestParsePaneFieldsKeepsLiveCodexPaneInShell(t *testing.T) {
	f := fullPaneFields()
	f[pfAgent] = "codex"
	f[pfCurrentCommand] = "zsh"

	// zsh is the pane's process, but codex is still alive underneath it.
	snap := process.FromPSOutput("4242 1 zsh zsh\n4243 4242 codex codex\n")
	if _, ok := parsePaneFields(f, snap); !ok {
		t.Fatal("the pane must be kept while codex is still alive")
	}
}

// Claude has its own SessionEnd hook, so it never goes through this cleanup path.
func TestParsePaneFieldsNeverClearsClaude(t *testing.T) {
	f := fullPaneFields()
	f[pfCurrentCommand] = "zsh"

	snap := process.FromPSOutput("4242 1 zsh zsh\n")
	if _, ok := parsePaneFields(f, snap); !ok {
		t.Fatal("a claude pane must not be cleaned up here")
	}
}

func TestSanitizePrompt(t *testing.T) {
	// An injected system message is not the user's prompt.
	for _, raw := range []string{
		"<task-notification>done</task-notification>",
		"has <system-reminder> in the middle",
		"<task-status>running</task-status>",
	} {
		if got := sanitizePrompt(raw); got != "" {
			t.Errorf("sanitizePrompt(%q) = %q, want empty", raw, got)
		}
	}

	// A prompt with legitimate angle brackets must be kept unchanged.
	code := "if a < b { return }"
	if got := sanitizePrompt(code); got != code {
		t.Errorf("sanitizePrompt(%q) = %q", code, got)
	}

	// Truncated at 200 RUNES, not 200 bytes — accented characters must count correctly.
	long := strings.Repeat("é", 250)
	got := sanitizePrompt(long)
	if n := len([]rune(got)); n != 200 {
		t.Errorf("length after truncation = %d runes, want 200", n)
	}
}

func TestParseSubagents(t *testing.T) {
	cases := map[string][]string{
		"":                 nil,
		"Explore":          {"Explore"},
		"Explore,Plan":     {"Explore", "Plan"},
		" Explore , Plan ": {"Explore", "Plan"},
		"Explore,,Plan":    {"Explore", "Plan"},
		"Explore:abcdef12": {"Explore #abcd"},
		"Explore:ab":       {"Explore #ab"},
		"Explore:":         {"Explore:"},
	}
	for raw, want := range cases {
		if got := parseSubagents(raw); !reflect.DeepEqual(got, want) {
			t.Errorf("parseSubagents(%q) = %v, want %v", raw, got, want)
		}
	}
}

func TestIsShellCommand(t *testing.T) {
	for _, cmd := range []string{"zsh", "bash", "/bin/bash", "FISH", "sh -c x", "pwsh"} {
		if !IsShellCommand(cmd) {
			t.Errorf("IsShellCommand(%q) = false, want true", cmd)
		}
	}
	for _, cmd := range []string{"", "claude", "node", "vim"} {
		if IsShellCommand(cmd) {
			t.Errorf("IsShellCommand(%q) = true, want false", cmd)
		}
	}
}
