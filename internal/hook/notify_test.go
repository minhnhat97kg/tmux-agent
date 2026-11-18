package hook

import (
	"os"
	"strings"
	"testing"

	"tmux-agent/internal/clock"
	"tmux-agent/internal/event"
	"tmux-agent/internal/tmux"
)

// freezeClock freezes clock.NowUnixMilli() at a fixed epoch-seconds mark,
// returning a restore function. Used to make the "set" pane option value
// written out predictable.
func freezeClock(t *testing.T, epochSecs int64) {
	t.Helper()
	old, hadOld := os.LookupEnv(clock.EnvNow)
	os.Setenv(clock.EnvNow, itoa64(epochSecs))
	t.Cleanup(func() {
		if hadOld {
			os.Setenv(clock.EnvNow, old)
		} else {
			os.Unsetenv(clock.EnvNow)
		}
	})
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// ─── LoadSettings ──────────────────────────────────────────────────────────

func TestLoadSettingsDefaultsWhenNoOption(t *testing.T) {
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()
	fake.Set("", "show", "-g")

	got := LoadSettings()
	if !got.Enabled {
		t.Fatal("Enabled = false, want true when @sidebar_notifications is unset")
	}
	want := DefaultNotificationEvents()
	if len(got.Events) != len(want) {
		t.Fatalf("Events = %v, want %v", got.Events, want)
	}
	for k := range want {
		if !got.Events[k] {
			t.Errorf("Events is missing %q from the default set", k)
		}
	}
}

func TestLoadSettingsReadsOff(t *testing.T) {
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()
	fake.Set("@sidebar_notifications off\n", "show", "-g")

	got := LoadSettings()
	if got.Enabled {
		t.Fatal("Enabled = true, want false when @sidebar_notifications = off")
	}
}

// The "session_end" token no longer exists — SessionEnd piggybacks on the
// "stop" flag (a fix required by the controller, see enabled()). An unknown
// token ("session_end", "bogus") is silently ignored, just like any other
// unrecognized token.
func TestLoadSettingsExplicitListIgnoresSessionEndToken(t *testing.T) {
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()
	fake.Set("@sidebar_notifications_events stop,session_end,bogus\n", "show", "-g")

	got := LoadSettings()
	if !got.Events[event.Stop] {
		t.Fatalf("Events = %v, want it to contain Stop", got.Events)
	}
	if len(got.Events) != 1 {
		t.Fatalf("len(Events) = %d, want 1 (\"session_end\" and \"bogus\" are both ignored)", len(got.Events))
	}
}

func TestLoadSettingsAllEnablesEveryEvent(t *testing.T) {
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()
	fake.Set("@sidebar_notifications_events all\n", "show", "-g")

	got := LoadSettings()
	for _, k := range NotificationEventKinds() {
		if !got.Events[k] {
			t.Errorf("Events is missing %q when configured with \"all\"", k)
		}
	}
}

// ─── parseNotificationEvents (an empty string disables all events) ───────

func TestParseNotificationEventsEmptyStringDisablesAllEvents(t *testing.T) {
	got := parseNotificationEvents("")
	if len(got) != 0 {
		t.Fatalf("parseNotificationEvents(\"\") = %v, want empty", got)
	}
}

// ─── SetNotificationRunID ───────────────────────────────────────────────────

func TestSetNotificationRunIDWritesMillisecondMark(t *testing.T) {
	freezeClock(t, 1_700_000_000)
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()

	SetNotificationRunID("%PANE_RUN_ID")

	found := false
	for _, call := range fake.Calls() {
		if len(call) >= 6 && call[0] == "set" && call[4] == tmux.PaneNotificationRunID {
			found = true
			if call[5] != "1700000000000" {
				t.Errorf("written value = %q, want %q", call[5], "1700000000000")
			}
		}
	}
	if !found {
		t.Fatal("did not find a set command writing @pane_notification_run_id")
	}
}

// ─── HandleNotification ─────────────────────────────────────────────────────

func defaultEnabledSettings() NotificationSettings {
	return NotificationSettings{Enabled: true, Events: DefaultNotificationEvents()}
}

// Notifications disabled at the @sidebar_notifications level: must never touch tmux.
func TestHandleNotificationNotificationsOff(t *testing.T) {
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()

	settings := NotificationSettings{Enabled: false, Events: DefaultNotificationEvents()}
	got := HandleNotification("%PANE_OFF", event.Event{Kind: event.StopFailure, Error: "boom"}, settings)

	if got {
		t.Fatal("HandleNotification = true, want false when Enabled = false")
	}
	if len(fake.Calls()) != 0 {
		t.Fatalf("Enabled = false must not touch tmux, but there were %d calls: %v", len(fake.Calls()), fake.Calls())
	}
}

// Notifications enabled, event in the default set, no anti-duplicate stamp
// yet, no run id yet (the pane never went through SetNotificationRunID) ->
// must still notify, the fingerprint carries no run-id prefix.
func TestHandleNotificationNotificationsOnWithNoOldStamp(t *testing.T) {
	freezeClock(t, 1_700_000_100)
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()
	// No output declared for show -pv (both PaneNotificationRunID and the
	// stamp key) — the Fake returns an error, GetPaneOptionValue degrades to "".

	got := HandleNotification("%PANE_ON", event.Event{Kind: event.StopFailure, Error: "boom"}, defaultEnabledSettings())

	if !got {
		t.Fatal("HandleNotification = false, want true (valid event, no prior stamp)")
	}
	wantValue := "1700000100000|boom"
	found := false
	for _, call := range fake.Calls() {
		if len(call) >= 6 && call[0] == "set" && call[4] == tmux.PaneOSNotifyTaskFailed {
			found = true
			if call[5] != wantValue {
				t.Errorf("written stamp value = %q, want %q", call[5], wantValue)
			}
		}
	}
	if !found {
		t.Fatalf("did not find a set command writing %s: %v", tmux.PaneOSNotifyTaskFailed, fake.Calls())
	}
}

// An event outside the list (event.CwdChanged isn't in NotificationEventKinds)
// -> false, no tmux touched — Events never contains that Kind.
func TestHandleNotificationEventOutsideList(t *testing.T) {
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()

	got := HandleNotification("%PANE_OUT", event.Event{Kind: event.CwdChanged, Cwd: "/tmp"}, defaultEnabledSettings())

	if got {
		t.Fatal("HandleNotification = true, want false for a Kind outside the list")
	}
	if len(fake.Calls()) != 0 {
		t.Fatalf("a Kind outside the list must not touch tmux, but there were %d calls", len(fake.Calls()))
	}
}

// An anti-duplicate stamp already exists, same fingerprint (same run), still
// within the cooldown window -> must not notify again.
func TestHandleNotificationExistingStampDoesNotNotifyAgain(t *testing.T) {
	freezeClock(t, 1_700_000_200)
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()

	pane := "%PANE_DUP"
	fake.Set("1700000000000", "show", "-t", pane, "-pv", tmux.PaneNotificationRunID)
	// Old stamp: same run id, same fingerprint "boom", written 100s ago (<
	// 120s cooldown) -> still valid.
	fake.Set("1700000100000|1700000000000:boom", "show", "-t", pane, "-pv", tmux.PaneOSNotifyTaskFailed)

	got := HandleNotification(pane, event.Event{Kind: event.StopFailure, Error: "boom"}, defaultEnabledSettings())

	if got {
		t.Fatal("HandleNotification = true, want false because the anti-duplicate stamp is still valid")
	}
	for _, call := range fake.Calls() {
		if len(call) > 0 && call[0] == "set" {
			t.Fatalf("must not write a new stamp when blocked as a duplicate, but got call: %v", call)
		}
	}
}

// An anti-duplicate stamp belonging to an OLD run (run id differs from the
// current one) -> notifies again, even with the exact same base fingerprint.
func TestHandleNotificationStampFromOldRunNotifiesAgain(t *testing.T) {
	freezeClock(t, 1_700_000_300)
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()

	pane := "%PANE_STALE_RUN"
	fake.Set("1700000300000", "show", "-t", pane, "-pv", tmux.PaneNotificationRunID)
	// The old stamp carries the OLD run-id prefix (1600000000000), different from the current run id.
	fake.Set("1600000000100|1600000000000:boom", "show", "-t", pane, "-pv", tmux.PaneOSNotifyTaskFailed)

	got := HandleNotification(pane, event.Event{Kind: event.StopFailure, Error: "boom"}, defaultEnabledSettings())

	if !got {
		t.Fatal("HandleNotification = false, want true because the old stamp belongs to a different run")
	}
	wantValue := "1700000300000|1700000300000:boom"
	found := false
	for _, call := range fake.Calls() {
		if len(call) >= 6 && call[0] == "set" && call[4] == tmux.PaneOSNotifyTaskFailed {
			found = true
			if call[5] != wantValue {
				t.Errorf("written stamp value = %q, want %q", call[5], wantValue)
			}
		}
	}
	if !found {
		t.Fatalf("did not find a set command writing a new stamp: %v", fake.Calls())
	}
}

// A Notification with MetaOnly = true (e.g. idle_prompt) never notifies,
// even though Kind Notification is present in the default set.
func TestHandleNotificationMetaOnlySkipped(t *testing.T) {
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()

	got := HandleNotification("%PANE_META", event.Event{
		Kind: event.Notification, WaitReason: "idle_prompt", MetaOnly: true,
	}, defaultEnabledSettings())

	if got {
		t.Fatal("HandleNotification = true, want false when MetaOnly = true")
	}
	if len(fake.Calls()) != 0 {
		t.Fatalf("MetaOnly must not touch tmux, but there were %d calls", len(fake.Calls()))
	}
}

// SessionEnd with a "soft" reason (not logout/bypass_permissions_disabled)
// does not notify, even though SessionEnd piggybacks on the Stop flag and
// Stop is enabled.
func TestHandleNotificationSessionEndSoftReasonDoesNotNotify(t *testing.T) {
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()

	for _, reason := range []string{"clear", "resume", "prompt_input_exit", "other", ""} {
		got := HandleNotification("%PANE_SE_SOFT", event.Event{Kind: event.SessionEnd, EndReason: reason}, defaultEnabledSettings())
		if got {
			t.Errorf("EndReason = %q: HandleNotification = true, want false", reason)
		}
	}
	if len(fake.Calls()) != 0 {
		t.Fatalf("a soft reason must not touch tmux, but there were %d calls", len(fake.Calls()))
	}
}

// SessionEnd with reason "logout" or "bypass_permissions_disabled" notifies,
// using exactly NotifyTaskCompleted (stamp @pane_os_notify_task_completed) —
// because SessionEnd piggybacks on the Stop flag and defaultEnabledSettings() has
// Stop enabled by default.
func TestHandleNotificationSessionEndNoteworthyReason(t *testing.T) {
	freezeClock(t, 1_700_000_400)
	for _, reason := range []string{"logout", "bypass_permissions_disabled"} {
		fake := tmux.NewFake()
		restore := tmux.SetRunner(fake)

		got := HandleNotification("%PANE_SE_LOGOUT", event.Event{Kind: event.SessionEnd, EndReason: reason}, defaultEnabledSettings())
		if !got {
			t.Errorf("EndReason = %q: HandleNotification = false, want true", reason)
		}
		foundKey := false
		for _, call := range fake.Calls() {
			if len(call) >= 6 && call[0] == "set" && call[4] == tmux.PaneOSNotifyTaskCompleted {
				foundKey = true
				if !strings.Contains(call[5], "session-ended:"+reason) {
					t.Errorf("EndReason = %q: stamp value = %q, want it to contain %q", reason, call[5], "session-ended:"+reason)
				}
			}
		}
		if !foundKey {
			t.Errorf("EndReason = %q: did not find a set command writing %s", reason, tmux.PaneOSNotifyTaskCompleted)
		}
		restore()
	}
}

// A fix required by the controller: SessionEnd piggybacks on the Stop flag,
// with no configuration token of its own. Disabling Stop (leaving it out of
// Events) also disables SessionEnd, even when the end reason is in the
// "worth notifying" group.
func TestHandleNotificationSessionEndPiggybacksOnStop(t *testing.T) {
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()

	settings := NotificationSettings{Enabled: true, Events: map[event.Kind]bool{event.StopFailure: true}}
	got := HandleNotification("%PANE_SE_PIGGYBACK", event.Event{Kind: event.SessionEnd, EndReason: "logout"}, settings)

	if got {
		t.Fatal("HandleNotification = true, want false because Stop is disabled and SessionEnd piggybacks on the Stop flag")
	}
	if len(fake.Calls()) != 0 {
		t.Fatalf("Stop disabled must not touch tmux for SessionEnd, but there were %d calls", len(fake.Calls()))
	}
}

// event.Stop is skipped if an explicit TaskCompleted has already fired for
// this exact run — avoiding notifying twice for the same completion.
func TestHandleNotificationStopSkippedIfTaskCompletedAlreadyFiredInSameRun(t *testing.T) {
	freezeClock(t, 1_700_000_500)
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()

	pane := "%PANE_STOP_AFTER_TASK"
	fake.Set("1700000500000", "show", "-t", pane, "-pv", tmux.PaneNotificationRunID)
	// TaskCompleted already fired: the stamp carries exactly the current run-id prefix.
	fake.Set("1700000499000|1700000500000:task-1", "show", "-t", pane, "-pv", tmux.PaneOSNotifyTaskCompleted)

	got := HandleNotification(pane, event.Event{Kind: event.Stop, LastMessage: "done"}, defaultEnabledSettings())

	if got {
		t.Fatal("HandleNotification = true, want false because TaskCompleted already fired this run")
	}
	for _, call := range fake.Calls() {
		if len(call) > 0 && call[0] == "set" {
			t.Fatalf("must not write a new stamp, but got call: %v", call)
		}
	}
}

// ─── internal helper: truncating long content ────────────────────────────

func TestTruncateBodyCutsStringLongerThanThreshold(t *testing.T) {
	// MUST be longer than notificationBodyMaxChars to actually exercise the
	// truncation branch — an earlier draft used input that never crossed the
	// threshold, so the truncation branch was never tested.
	long := strings.Repeat("a", notificationBodyMaxChars+50)
	got := truncateBody(long)
	gotRunes := []rune(got)
	if len(gotRunes) != notificationBodyMaxChars+1 {
		t.Fatalf("len(rune) = %d, want %d", len(gotRunes), notificationBodyMaxChars+1)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("got = %q, want it to end with …", got)
	}
}

func TestTruncateBodyKeepsStringShorterThanThreshold(t *testing.T) {
	short := "under the threshold"
	if got := truncateBody(short); got != short {
		t.Fatalf("got = %q, want it unchanged as %q", got, short)
	}
}

func TestStopBodyFallsBackToDefaultWhenEmpty(t *testing.T) {
	for _, in := range []string{"", "   \n"} {
		if got := stopBody(in); got != "Task completed" {
			t.Errorf("stopBody(%q) = %q, want %q", in, got, "Task completed")
		}
	}
}
