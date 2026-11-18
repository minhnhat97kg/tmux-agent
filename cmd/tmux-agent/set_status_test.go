package main

import (
	"bytes"
	"testing"

	"tmux-agent/internal/tmux"
)

// The pure status-writing logic (the "clear" branch, the running/idle ⇒
// unset attention rule) has moved to hook.ApplyPaneStatus and has its own
// tests in internal/hook/handlers_test.go. This file now only tests the part
// of cmdSetStatus/run(): reading TMUX_PANE, parsing arguments, forwarding
// down to hook.ApplyPaneStatus.

// hasSetPaneOptionCall reports true if fake.Calls() contains exactly the
// command "set -t <pane> -p <key> <value>" — the pattern taken from
// internal/hook/activity_test.go, so as not to lock down the total number of
// tmux commands (the plan bans len(calls) != N: adding a call down the line
// would break the test even though the behavior being checked hasn't
// changed).
func hasSetPaneOptionCall(fake *tmux.Fake, pane, key, value string) bool {
	for _, call := range fake.Calls() {
		if len(call) == 6 && call[0] == "set" && call[1] == "-t" && call[2] == pane &&
			call[3] == "-p" && call[4] == key && call[5] == value {
			return true
		}
	}
	return false
}

// No TMUX_PANE → tmux must not be called, returns 0 silently.
func TestCmdSetStatusWithoutTmuxPaneDoesNotCallTmux(t *testing.T) {
	t.Setenv("TMUX_PANE", "")
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()

	if code := cmdSetStatus([]string{"running"}); code != exitOK {
		t.Fatalf("exit code = %d, want %d", code, exitOK)
	}
	if len(fake.Calls()) != 0 {
		t.Fatalf("missing TMUX_PANE must not call tmux, got %v", fake.Calls())
	}
}

// No status argument → tmux must not be called, returns 0 silently.
func TestCmdSetStatusWithoutArgsDoesNotCallTmux(t *testing.T) {
	t.Setenv("TMUX_PANE", "%3")
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()

	if code := cmdSetStatus(nil); code != exitOK {
		t.Fatalf("exit code = %d, want %d", code, exitOK)
	}
	if len(fake.Calls()) != 0 {
		t.Fatalf("missing arguments must not call tmux, got %v", fake.Calls())
	}
}

// Real TMUX_PANE → the tmux command must target that exact pane.
func TestCmdSetStatusTargetsCorrectTmuxPane(t *testing.T) {
	t.Setenv("TMUX_PANE", "%7")
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()

	if code := cmdSetStatus([]string{"background"}); code != exitOK {
		t.Fatalf("exit code = %d, want %d", code, exitOK)
	}
	if !hasSetPaneOptionCall(fake, "%7", tmux.PaneStatusOpt, "background") {
		t.Fatalf("did not see the set @pane_status=background command on %%7 in %v", fake.Calls())
	}
}

// End-to-end from run(): "set-status" must forward the status argument
// correctly downstream.
func TestRunSetStatusForwardsStatusArg(t *testing.T) {
	t.Setenv("TMUX_PANE", "%9")
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()

	var out, errOut bytes.Buffer
	code := run([]string{"set-status", "error"}, &out, &errOut)

	if code != exitOK {
		t.Fatalf("exit code = %d, want %d", code, exitOK)
	}
	if !hasSetPaneOptionCall(fake, "%9", tmux.PaneStatusOpt, "error") {
		t.Fatalf("did not see the set @pane_status=error command on %%9 in %v", fake.Calls())
	}
}
