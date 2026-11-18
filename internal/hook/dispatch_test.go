package hook

import (
	"bytes"
	"strings"
	"testing"

	"tmux-agent/internal/tmux"
)

// Every error path in Dispatch must exit 0 silently, never panic, never print
// anything to stdout — the hook runs inside the user's real agent session and
// must never break it.

func TestDispatchMissingArgsExits0(t *testing.T) {
	t.Setenv("TMUX_PANE", "%1")
	var out bytes.Buffer
	if got := Dispatch(nil, strings.NewReader(""), &out); got != 0 {
		t.Errorf("Dispatch(nil) = %d, want 0", got)
	}
	if got := Dispatch([]string{"claude"}, strings.NewReader(""), &out); got != 0 {
		t.Errorf("Dispatch with only agent = %d, want 0", got)
	}
	if out.Len() != 0 {
		t.Errorf("missing args must not print anything to stdout, got %q", out.String())
	}
}

func TestDispatchUnknownAgentExits0(t *testing.T) {
	t.Setenv("TMUX_PANE", "%1")
	var out bytes.Buffer
	got := Dispatch([]string{"unknown-agent", "session-start"}, strings.NewReader(""), &out)
	if got != 0 {
		t.Errorf("Dispatch with unknown agent = %d, want 0", got)
	}
	if out.Len() != 0 {
		t.Error("unknown agent must not print anything to stdout")
	}
}

func TestDispatchUnknownEventExits0(t *testing.T) {
	t.Setenv("TMUX_PANE", "%1")
	var out bytes.Buffer
	got := Dispatch([]string{"claude", "nonexistent-event"}, strings.NewReader("{}"), &out)
	if got != 0 {
		t.Errorf("Dispatch with unknown event = %d, want 0", got)
	}
}

func TestDispatchMissingTmuxPaneExits0(t *testing.T) {
	t.Setenv("TMUX_PANE", "")
	var out bytes.Buffer
	got := Dispatch([]string{"claude", "session-start"}, strings.NewReader("{}"), &out)
	if got != 0 {
		t.Errorf("Dispatch with missing TMUX_PANE = %d, want 0", got)
	}
}

func TestDispatchEmptyStdinStillRunsWithZeroValue(t *testing.T) {
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	t.Setenv("TMUX_PANE", "%STDIN_EMPTY")
	var out bytes.Buffer

	got := Dispatch([]string{"claude", "session-start"}, strings.NewReader(""), &out)

	if got != 0 {
		t.Errorf("Dispatch with empty stdin = %d, want 0", got)
	}
	// Empty payload -> every field is zero value, but the handler must still
	// run: status must switch to idle like a normal SessionStart.
	if s := finalPaneOptions(f, "%STDIN_EMPTY")[tmux.PaneStatusOpt]; s != string(tmux.StatusIdle) {
		t.Errorf("@pane_status = %q, want idle even with empty stdin", s)
	}
}

func TestDispatchInvalidJSONExits0WithoutPanic(t *testing.T) {
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	t.Setenv("TMUX_PANE", "%JSON_BROKEN")
	var out bytes.Buffer

	got := Dispatch([]string{"claude", "session-start"}, strings.NewReader("{not valid json"), &out)

	if got != 0 {
		t.Errorf("Dispatch with broken JSON = %d, want 0", got)
	}
	// Broken JSON is treated as no payload — the handler must still run with
	// zero values, it must not silently swallow the whole event.
	if s := finalPaneOptions(f, "%JSON_BROKEN")[tmux.PaneStatusOpt]; s != string(tmux.StatusIdle) {
		t.Errorf("@pane_status = %q, want idle even with broken JSON", s)
	}
}

func TestDispatchCodexPrintsResponseToStdout(t *testing.T) {
	// The ONLY place in all of part 2 that prints to stdout: Codex attaches
	// `{"continue":true}` to Response so its hook engine reads it back. The
	// printing happens at the Dispatch LAYER, not inside OnStop.
	t.Setenv("TMUX_AGENT_LOG_DIR", t.TempDir())
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	t.Setenv("TMUX_PANE", "%CODEX_STOP")
	var out bytes.Buffer

	got := Dispatch([]string{"codex", "stop"}, strings.NewReader(`{"last_assistant_message":"done"}`), &out)

	if got != 0 {
		t.Errorf("Dispatch = %d, want 0", got)
	}
	if strings.TrimSpace(out.String()) != `{"continue":true}` {
		t.Errorf("stdout = %q, want {\"continue\":true}", out.String())
	}
}

func TestDispatchClaudeStopPrintsNothingToStdout(t *testing.T) {
	t.Setenv("TMUX_AGENT_LOG_DIR", t.TempDir())
	f := tmux.NewFake()
	defer tmux.SetRunner(f)()
	t.Setenv("TMUX_PANE", "%CLAUDE_STOP")
	var out bytes.Buffer

	Dispatch([]string{"claude", "stop"}, strings.NewReader(`{"last_assistant_message":"done"}`), &out)

	if out.Len() != 0 {
		t.Errorf("Claude Stop must not print anything to stdout, got %q", out.String())
	}
}
