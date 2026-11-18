package frametest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeStripsTrailingWhitespace(t *testing.T) {
	got := Normalize("one   \ntwo\t\n\n\n")
	want := "one\ntwo"
	if got != want {
		t.Fatalf("Normalize = %q, want %q", got, want)
	}
}

func TestNormalizeKeepsInteriorBlankLines(t *testing.T) {
	got := Normalize("one\n\ntwo   \n")
	want := "one\n\ntwo"
	if got != want {
		t.Fatalf("Normalize = %q, want %q", got, want)
	}
}

// Verifies the runner itself: spins up a real tmux, captures it, compares
// against the golden file. Phase 3 will use this exact mechanism for the 8
// real scenarios.
func TestFakeScenarioMatchesGolden(t *testing.T) {
	if _, err := os.Stat("/usr/bin/tmux"); err != nil {
		if _, err := os.Stat("/bin/tmux"); err != nil {
			t.Skip("tmux not available")
		}
	}

	scenario := filepath.Join("testdata", "fake-scenario", "scenario.sh")
	golden := filepath.Join("testdata", "fake-scenario", "expected.txt")

	RunScenario(t, scenario, golden)
}

func TestRunScenarioReportsMissingGolden(t *testing.T) {
	// The golden file doesn't exist yet and UPDATE_FRAMES is not enabled →
	// must report a clear error instead of silently passing.
	t.Setenv(EnvUpdate, "")

	if _, err := loadGolden(filepath.Join("testdata", "does-not-exist.txt")); err == nil {
		t.Fatal("want an error when the golden file is missing")
	} else if !strings.Contains(err.Error(), "UPDATE_FRAMES") {
		t.Fatalf("error message must mention how to generate the golden file, got: %v", err)
	}
}
