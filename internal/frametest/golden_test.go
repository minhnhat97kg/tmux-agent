package frametest

import (
	"os"
	"path/filepath"
	"testing"
)

// The four golden scenarios of phase 3b (spec §5.3: drops 8 upstream
// scenarios — they take an output-dir as $1 instead of a socket name, don't
// print the pane id to stdout, and end with `capture --frame-out` which
// depends on phase 5's `capture` subcommand). These four scenarios are
// written specifically for this harness, matching the contract above.
//
// scenario.sh lives under assets/fixtures/scenarios/ (not testdata) because
// it builds and runs the real tmux-agent binary itself — it is not static
// data.
func scenarioPath(name string) string {
	return filepath.Join("..", "..", "assets", "fixtures", "scenarios", name, "scenario.sh")
}

func goldenPath(name string) string {
	return filepath.Join("testdata", "frames", name+".txt")
}

func skipIfNoTmux(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/usr/bin/tmux"); err != nil {
		if _, err := os.Stat("/bin/tmux"); err != nil {
			t.Skip("tmux not available")
		}
	}
}

// TestGoldenSingleAgent: exactly one pane with a running Claude agent.
func TestGoldenSingleAgent(t *testing.T) {
	skipIfNoTmux(t)
	RunScenario(t, scenarioPath("frame-single-agent"), goldenPath("single-agent"))
}

// TestGoldenMultiStatus: multiple panes, all six different statuses, same
// repo.
func TestGoldenMultiStatus(t *testing.T) {
	skipIfNoTmux(t)
	RunScenario(t, scenarioPath("frame-multi-status"), goldenPath("multi-status"))
}

// TestGoldenMultiRepo: multiple repos — checks grouping by git repo root.
func TestGoldenMultiRepo(t *testing.T) {
	skipIfNoTmux(t)
	RunScenario(t, scenarioPath("frame-multi-repo"), goldenPath("multi-repo"))
}

// TestGoldenEmpty: empty list — no agent panes at all.
func TestGoldenEmpty(t *testing.T) {
	skipIfNoTmux(t)
	RunScenario(t, scenarioPath("frame-empty"), goldenPath("empty"))
}
