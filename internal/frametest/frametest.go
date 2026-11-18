// Package frametest runs a tmux scenario, captures the pane content as
// plain text, then compares it against a golden file.
//
// Captures via `tmux capture-pane -p` WITHOUT -e, so there is no color code.
// This layer catches layout bugs: wrong column width, text truncated in the
// wrong place, wrong wide-character handling, missing/extra lines. Color is
// tested separately via a table, not through this path.
package frametest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// EnvUpdate enables golden-overwrite mode. Set by `make update-frames`.
const EnvUpdate = "UPDATE_FRAMES"

// Capture captures the current content of a pane on an isolated tmux
// server.
func Capture(socket, paneID string) (string, error) {
	out, err := exec.Command("tmux", "-L", socket, "capture-pane", "-p", "-t", paneID).Output()
	if err != nil {
		return "", fmt.Errorf("capture-pane %s: %w", paneID, err)
	}
	return string(out), nil
}

// Normalize strips trailing whitespace on each line and trailing blank
// lines.
//
// tmux pads every line to the full pane width, so a raw comparison would
// fail for reasons unrelated to the content. Blank lines IN THE MIDDLE are
// kept because they are real layout.
func Normalize(raw string) string {
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// RunScenario runs scenario.sh, captures the pane it prints, then compares
// against the golden file. When UPDATE_FRAMES is enabled it writes the
// golden file instead of comparing.
func RunScenario(t *testing.T, scenarioPath, goldenPath string) {
	t.Helper()

	socket := "tmux-agent-test-" + filepath.Base(filepath.Dir(scenarioPath))
	t.Cleanup(func() {
		exec.Command("tmux", "-L", socket, "kill-server").Run()
	})

	out, err := exec.Command("bash", scenarioPath, socket).Output()
	if err != nil {
		t.Fatalf("run %s: %v", scenarioPath, err)
	}
	paneID := strings.TrimSpace(string(out))
	if paneID == "" {
		t.Fatalf("%s did not print a pane id", scenarioPath)
	}

	raw, err := Capture(socket, paneID)
	if err != nil {
		t.Fatal(err)
	}
	got := Normalize(raw)

	if os.Getenv(EnvUpdate) != "" {
		if err := os.WriteFile(goldenPath, []byte(got+"\n"), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote golden %s", goldenPath)
		return
	}

	want, err := loadGolden(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("frame differs from golden %s\n--- got ---\n%s\n--- want ---\n%s",
			goldenPath, got, want)
	}
}

func loadGolden(goldenPath string) (string, error) {
	data, err := os.ReadFile(goldenPath)
	if err != nil {
		return "", fmt.Errorf("read golden %s: %w (generate it the first time with %s=1 or `make update-frames`)",
			goldenPath, err, EnvUpdate)
	}
	return Normalize(string(data)), nil
}
