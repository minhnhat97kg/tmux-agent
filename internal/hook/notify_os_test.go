package hook

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// TestMain replaces runNotifyCommand with a fake that ALWAYS SUCCEEDS for
// every test in the hook package. The older tests (notify_test.go) call
// HandleNotification and expect SendDesktopNotification to "send"
// successfully like the stub from part 2 — they don't know and don't need
// to know that part 4 wired in the real OS command. The dedicated
// SendDesktopNotification tests below swap out and restore runNotifyCommand
// for each of their own scenarios.
func TestMain(m *testing.M) {
	runNotifyCommand = func(name string, args ...string) error { return nil }
	os.Exit(m.Run())
}

// stubRunNotifyCommand replaces runNotifyCommand with fn, returning a
// restore function back to TestMain's default fake (not a real OS-calling
// one — this package must never call the real OS in any test).
func stubRunNotifyCommand(t *testing.T, fn func(name string, args ...string) error) {
	t.Helper()
	old := runNotifyCommand
	runNotifyCommand = fn
	t.Cleanup(func() { runNotifyCommand = old })
}

func setNotifyGOOS(t *testing.T, goos string) {
	t.Helper()
	old := notifyGOOS
	notifyGOOS = goos
	t.Cleanup(func() { notifyGOOS = old })
}

// ─── SendDesktopNotification ────────────────────────────────────────────────

// Has a backend (macOS): osascript is called with the exact AppleScript
// script, title/body embedded verbatim (no escaping needed since there are
// no special characters).
func TestSendDesktopNotificationMacOSHasBackend(t *testing.T) {
	setNotifyGOOS(t, "darwin")
	var gotName string
	var gotArgs []string
	stubRunNotifyCommand(t, func(name string, args ...string) error {
		gotName, gotArgs = name, args
		return nil
	})

	if err := SendDesktopNotification("Task done", "Finished the task"); err != nil {
		t.Fatalf("SendDesktopNotification error = %v, want nil", err)
	}
	if gotName != "osascript" {
		t.Fatalf("called command = %q, want osascript", gotName)
	}
	if len(gotArgs) != 2 || gotArgs[0] != "-e" {
		t.Fatalf("osascript args = %v, want [-e <script>]", gotArgs)
	}
	wantScript := `display notification "Finished the task" with title "Task done"`
	if gotArgs[1] != wantScript {
		t.Fatalf("script = %q, want %q", gotArgs[1], wantScript)
	}
}

// Has a backend (Linux): notify-send is called with fixed app-name/urgency,
// title and body passed as separate parameters (not concatenated into one
// string).
func TestSendDesktopNotificationLinuxHasBackend(t *testing.T) {
	setNotifyGOOS(t, "linux")
	var gotName string
	var gotArgs []string
	stubRunNotifyCommand(t, func(name string, args ...string) error {
		gotName, gotArgs = name, args
		return nil
	})

	if err := SendDesktopNotification("title", "body"); err != nil {
		t.Fatalf("SendDesktopNotification error = %v, want nil", err)
	}
	if gotName != "notify-send" {
		t.Fatalf("called command = %q, want notify-send", gotName)
	}
	wantArgs := []string{"--app-name=tmux-agent-sidebar", "--urgency=normal", "title", "body"}
	if len(gotArgs) != len(wantArgs) {
		t.Fatalf("args = %v, want %v", gotArgs, wantArgs)
	}
	for i, want := range wantArgs {
		if gotArgs[i] != want {
			t.Fatalf("args[%d] = %q, want %q", i, gotArgs[i], want)
		}
	}
}

// No platform matches (e.g. windows) → errors immediately, does NOT call
// runNotifyCommand — no backend is ever tried.
func TestSendDesktopNotificationNoBackendReturnsError(t *testing.T) {
	setNotifyGOOS(t, "windows")
	called := false
	stubRunNotifyCommand(t, func(name string, args ...string) error {
		called = true
		return nil
	})

	err := SendDesktopNotification("t", "b")
	if err == nil {
		t.Fatal("SendDesktopNotification error = nil, want an error since there is no backend")
	}
	if called {
		t.Fatal("must not call runNotifyCommand when there is no backend for this platform")
	}
}

// OS command returns an error (e.g. osascript/notify-send not installed, or
// exits with an error code) → SendDesktopNotification must propagate that
// error, not swallow it, and not panic.
func TestSendDesktopNotificationCommandErrorPropagates(t *testing.T) {
	setNotifyGOOS(t, "linux")
	wantErr := errors.New("exec: \"notify-send\": executable file not found in $PATH")
	stubRunNotifyCommand(t, func(name string, args ...string) error {
		return wantErr
	})

	err := SendDesktopNotification("t", "b")
	if !errors.Is(err, wantErr) {
		t.Fatalf("SendDesktopNotification error = %v, want %v", err, wantErr)
	}
}

// Content containing double quotes and newlines must be escaped in the
// correct order for osascript: newline -> space, \ doubled first, then only
// then " -> \". Tested through SendDesktopNotification (not just
// escapeAppleScript alone) to confirm the final script sent to osascript is
// exactly what osascript will receive.
func TestSendDesktopNotificationEscapesQuotesAndNewlines(t *testing.T) {
	setNotifyGOOS(t, "darwin")
	var gotScript string
	stubRunNotifyCommand(t, func(name string, args ...string) error {
		gotScript = args[1]
		return nil
	})

	title := `Say "hello"`
	body := "Line one\nLine two\rLine three"

	if err := SendDesktopNotification(title, body); err != nil {
		t.Fatalf("SendDesktopNotification error = %v, want nil", err)
	}

	wantBody := `Line one Line two Line three`
	wantTitle := `Say \"hello\"`
	wantScript := `display notification "` + wantBody + `" with title "` + wantTitle + `"`
	if gotScript != wantScript {
		t.Fatalf("script = %q, want %q", gotScript, wantScript)
	}
	// No raw newline should leak into the final script.
	if strings.ContainsAny(gotScript, "\n\r") {
		t.Fatalf("script still contains a raw newline character: %q", gotScript)
	}
}

// escapeAppleScript: the \ must be doubled BEFORE double quotes get their \
// prefix added, otherwise the reverse order would escape on top of the \
// that step just produced from the double-quote step.
func TestEscapeAppleScriptBackslashBeforeQuoteOrder(t *testing.T) {
	got := escapeAppleScript(`a\"b`)
	want := `a\\\"b`
	if got != want {
		t.Fatalf("escapeAppleScript(%q) = %q, want %q", `a\"b`, got, want)
	}
}
