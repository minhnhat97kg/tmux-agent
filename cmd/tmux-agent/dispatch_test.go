package main

import (
	"bytes"
	"strings"
	"testing"

	"tmux-agent/internal/version"
)

func TestRunVersionPrintsVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"version"}, &out, &errOut)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if got := strings.TrimSpace(out.String()); got != version.Version {
		t.Fatalf("stdout = %q, want %q", got, version.Version)
	}
}

func TestRunUnknownCommandExits127(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"does-not-exist"}, &out, &errOut)

	if code != 127 {
		t.Fatalf("exit code = %d, want 127", code)
	}
	if !strings.Contains(errOut.String(), "does-not-exist") {
		t.Fatalf("stderr = %q, want it to mention the wrong command name", errOut.String())
	}
}

// This table is the list of commands that are STILL STUBS. Each phase that
// implements a command must remove it from here, otherwise the test fails —
// that is the point of this table.
func TestStubCommandsExit70(t *testing.T) {
	stubs := []string{
		"capture",
	}
	for _, cmd := range stubs {
		t.Run(cmd, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := run([]string{cmd}, &out, &errOut)

			if code != 70 {
				t.Fatalf("exit code = %d, want 70", code)
			}
			if !strings.Contains(errOut.String(), "not implemented") {
				t.Fatalf("stderr = %q, want it to contain \"not implemented\"", errOut.String())
			}
		})
	}
}

// run(nil, ...) is not tested here: no arguments now really runs the
// bubbletea TUI (sidebar.Run), which needs a real tmux pane/tty to read
// keys and draw the screen — see the sidebar package's tests (Model/Update/
// View) and the manual verification step in the phase 3a task 2 report.
