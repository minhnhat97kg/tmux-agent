package tmux

import (
	"errors"
	"reflect"
	"testing"
)

func TestRunTmuxReturnsStdoutOnSuccess(t *testing.T) {
	fake := NewFake()
	fake.Set("one\ntwo\n", "list-panes", "-a")
	defer SetRunner(fake)()

	out, ok := RunTmux("list-panes", "-a")
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if out != "one\ntwo\n" {
		t.Fatalf("out = %q", out)
	}
}

func TestRunTmuxReturnsFalseOnError(t *testing.T) {
	fake := NewFake()
	fake.SetErr(errors.New("no server running"), "list-panes", "-a")
	defer SetRunner(fake)()

	out, ok := RunTmux("list-panes", "-a")
	if ok {
		t.Fatal("ok = true, want false")
	}
	if out != "" {
		t.Fatalf("out = %q, want empty", out)
	}
}

// An undeclared command in Fake is treated as tmux returning an error. This
// way a test that forgets to declare it sees the degraded behavior, not a
// fake empty string pretending to be success.
func TestRunTmuxUnknownCommandIsError(t *testing.T) {
	defer SetRunner(NewFake())()

	if _, ok := RunTmux("undeclared-command"); ok {
		t.Fatal("ok = true, want false")
	}
}

func TestRunTmuxCaptureTrimsAndSurfacesError(t *testing.T) {
	fake := NewFake()
	fake.Set("  %5  \n", "new-window")
	fake.SetErr(errors.New("can't find window"), "kill-window")
	defer SetRunner(fake)()

	out, err := RunTmuxCapture("new-window")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if out != "%5" {
		t.Fatalf("out = %q, want %q", out, "%5")
	}

	if _, err := RunTmuxCapture("kill-window"); err == nil {
		t.Fatal("err = nil, want an error")
	}
}

func TestDisplayMessageTrims(t *testing.T) {
	fake := NewFake()
	fake.Set("main\n", "display-message", "-t", "%1", "-p", "#{session_name}")
	defer SetRunner(fake)()

	if got := DisplayMessage("%1", "#{session_name}"); got != "main" {
		t.Fatalf("DisplayMessage = %q, want %q", got, "main")
	}
}

func TestPaneSessionNameEmptyIsNotOK(t *testing.T) {
	fake := NewFake()
	fake.Set("\n", "display-message", "-t", "%9", "-p", "#{session_name}")
	defer SetRunner(fake)()

	if _, ok := PaneSessionName("%9"); ok {
		t.Fatal("ok = true, want false when tmux returns empty")
	}
}

// SelectPane must jump through both session and window before selecting the
// pane, so a pane in a window/session other than the client's current one
// can still be reached. We don't pin down the total call count (fake.Calls()
// may also contain the two display-message calls reading session_id/window_id)
// — we only check that the three expected commands are present.
func TestSelectPaneSwitchesToDifferentSessionAndWindow(t *testing.T) {
	fake := NewFake()
	fake.Set("$2\n", "display-message", "-t", "%3", "-p", "#{session_id}")
	fake.Set("@1\n", "display-message", "-t", "%3", "-p", "#{window_id}")
	fake.Set("", "list-clients", "-t", "%0", "-F", "#{client_tty}") // no client found for fromPane
	defer SetRunner(fake)()

	SelectPane("%3", "%0")

	want := [][]string{
		{"switch-client", "-t", "$2"},
		{"select-window", "-t", "@1"},
		{"select-pane", "-t", "%3"},
	}
	for _, w := range want {
		found := false
		for _, call := range fake.Calls() {
			if reflect.DeepEqual(call, w) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing command %v in %v", w, fake.Calls())
		}
	}
}

// Real bug, real user report: with several tmux clients each attached to a
// DIFFERENT session, `switch-client -t <session>` with no `-c <client>`
// lets tmux move an unrelated client instead of the one actually looking at
// the sidebar. SelectPane must pin -c to the client attached to fromPane's
// session.
func TestSelectPaneTargetsTheClientViewingFromPane(t *testing.T) {
	fake := NewFake()
	fake.Set("$2\n", "display-message", "-t", "%3", "-p", "#{session_id}")
	fake.Set("@1\n", "display-message", "-t", "%3", "-p", "#{window_id}")
	fake.Set("/dev/ttys001\n", "list-clients", "-t", "%0", "-F", "#{client_tty}")
	defer SetRunner(fake)()

	SelectPane("%3", "%0")

	want := []string{"switch-client", "-c", "/dev/ttys001", "-t", "$2"}
	found := false
	for _, call := range fake.Calls() {
		if reflect.DeepEqual(call, want) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing command %v (with explicit -c) in %v", want, fake.Calls())
	}
}

func TestClientForPaneReturnsFirstClientTTY(t *testing.T) {
	fake := NewFake()
	fake.Set("/dev/ttys001\n/dev/ttys002\n", "list-clients", "-t", "%0", "-F", "#{client_tty}")
	defer SetRunner(fake)()

	if got := ClientForPane("%0"); got != "/dev/ttys001" {
		t.Fatalf("ClientForPane = %q, want %q", got, "/dev/ttys001")
	}
}

func TestClientForPaneNoClientsReturnsEmpty(t *testing.T) {
	fake := NewFake()
	fake.Set("", "list-clients", "-t", "%0", "-F", "#{client_tty}")
	defer SetRunner(fake)()

	if got := ClientForPane("%0"); got != "" {
		t.Fatalf("ClientForPane = %q, want empty", got)
	}
}
