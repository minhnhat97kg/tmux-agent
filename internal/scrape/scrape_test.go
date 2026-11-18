package scrape

import (
	"strings"
	"testing"

	"tmux-agent/internal/process"
	"tmux-agent/internal/tmux"
)

func TestClassify(t *testing.T) {
	cases := map[string]tmux.PaneStatus{
		"output\n✻ Thinking… (12s · esc to interrupt)\n> \n":                         tmux.StatusRunning,
		"⠙ Generating response\n":                                                    tmux.StatusRunning,
		"Bash command\n  rm -rf build\nDo you want to proceed?\n❯ 1. Yes\n  2. No\n": tmux.StatusWaiting,
		"Apply this change? [y/n]\n":                                                 tmux.StatusWaiting,
		"done, all tests pass\n> \n\n\n":                                             tmux.StatusIdle,
		"":                                                                           tmux.StatusIdle,
	}
	for screen, want := range cases {
		if got := Classify(screen); got != want {
			t.Errorf("Classify(%q) = %s, want %s", screen, got, want)
		}
	}
}

func TestClassifyIgnoresOldOutput(t *testing.T) {
	screen := "Do you want to proceed?\n"
	for i := 0; i < tailLines; i++ {
		screen += "later line\n"
	}
	if got := Classify(screen); got != tmux.StatusIdle {
		t.Errorf("a prompt scrolled above the tail must not count, got %s", got)
	}
}

func TestDetectAgent(t *testing.T) {
	snap := process.FromPSOutput(`
  100     1 zsh   -zsh
  200   100 node  node /home/u/.npm/bin/gemini --yolo
  300     1 zsh   -zsh
  400   300 vim   vim gemini.txt
  500     1 zsh   -zsh
  600   500 claude claude
`)
	for pid, want := range map[int]string{100: "gemini", 300: "", 500: "claude"} {
		if got := DetectAgent(snap, pid); got != want {
			t.Errorf("DetectAgent(%d) = %q, want %q", pid, got, want)
		}
	}
}

func TestIsInterrupted(t *testing.T) {
	interrupted := "● Running tests\n  ⎿  Interrupted · What should Claude do instead?\n\n╭────╮\n│ >  │\n╰────╯\n"
	if !IsInterrupted(interrupted) {
		t.Error("interrupted screen not detected")
	}
	working := interrupted + "✻ Thinking… (esc to interrupt)\n"
	if IsInterrupted(working) {
		t.Error("working screen flagged as interrupted")
	}
	if IsInterrupted("● Done.\n> \n") {
		t.Error("plain idle screen flagged as interrupted")
	}
}

func TestIsInterruptedIgnoresRunningScript(t *testing.T) {
	box := "\n────────\n❯ \n────────\n  ⏵⏵ auto mode on · 1 shell\n"
	// A script printing "interrupted", the footer without "esc to interrupt":
	// Claude's own spinner line still says it's working.
	running := "● Bash(./run.sh)\n  ⎿  job 3 interrupted, retrying\n· Dilly-dallying… (4m 27s · ↓ 17.9k tokens)" + box
	if Classify(running) != tmux.StatusRunning {
		t.Error("Claude spinner line not seen as running")
	}
	if IsInterrupted(running) {
		t.Error("running script flagged as interrupted")
	}
	// An old interrupt far above the input box doesn't count.
	old := "  ⎿  Interrupted by user\n" + strings.Repeat("● done\n", 10) + "✻ Churned for 3m 30s" + box
	if IsInterrupted(old) {
		t.Error("stale interrupt flagged")
	}
}
