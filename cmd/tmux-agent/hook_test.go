package main

import (
	"bytes"
	"os"
	"testing"

	"tmux-agent/internal/tmux"
)

// The test here ONLY locks down the "hook" wiring in run() to
// hook.Dispatch — it does not duplicate the correctness of Dispatch (unknown
// agent, unknown event, missing TMUX_PANE, broken JSON, …): those cases
// already have dedicated tests in internal/hook/dispatch_test.go, which owns
// the real router logic.
//
// Uses tmux.Fake: Run records EVERY command into Calls() regardless of
// whether Set() declared it beforehand (an unknown command only makes Fake
// return a fake error, which RunTmux swallows) — so there is no need to
// pre-declare the exact content the handler will call, just count Calls() to
// confirm the wiring is not broken.
func TestRunHookValidEventReachesDispatch(t *testing.T) {
	t.Setenv("TMUX_PANE", "%1")
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()

	restore := setStdinContent(t, `{"cwd":"/tmp","permission_mode":"default","session_id":"s1"}`)
	defer restore()

	var out, errOut bytes.Buffer
	code := run([]string{"hook", "claude", "session-start"}, &out, &errOut)

	if code != exitOK {
		t.Fatalf("exit code = %d, want %d", code, exitOK)
	}
	if len(fake.Calls()) == 0 {
		t.Fatal("a valid event must reach hook.Dispatch and call at least one tmux command — wiring is broken")
	}
}

// Unknown agent/unknown event/missing TMUX_PANE: hook.Dispatch soft-degrades
// to 0 on its own and does not call tmux — the wiring just needs to not
// break that.
func TestRunHookUnknownAgentDoesNotCallTmux(t *testing.T) {
	t.Setenv("TMUX_PANE", "%1")
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()

	restore := setStdinContent(t, `{}`)
	defer restore()

	var out, errOut bytes.Buffer
	code := run([]string{"hook", "bogus-agent", "session-start"}, &out, &errOut)

	if code != exitOK {
		t.Fatalf("exit code = %d, want %d", code, exitOK)
	}
	if len(fake.Calls()) != 0 {
		t.Fatalf("an unknown agent must not call tmux, got %v", fake.Calls())
	}
}

// setStdinContent replaces os.Stdin with a pipe containing content, and
// returns a restore function. Necessary because the "hook" wiring in
// dispatch.go calls os.Stdin directly (matching how the real binary runs),
// so the test must replace os.Stdin itself to feed in the payload.
func setStdinContent(t *testing.T, content string) func() {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	if _, err := w.WriteString(content); err != nil {
		t.Fatalf("write to pipe: %v", err)
	}
	w.Close()

	prevStdin := os.Stdin
	os.Stdin = r
	return func() {
		os.Stdin = prevStdin
		r.Close()
	}
}
