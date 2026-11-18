package sidebar

import (
	"errors"
	"os"
	"reflect"
	"testing"

	"tmux-agent/internal/tmux"
)

// ─── sidebarPositionFromSetting ────────────────────────────────────────────

func TestSidebarPositionFromSettingOnlyAcceptsRight(t *testing.T) {
	cases := map[string]sidebarPosition{
		"right":   positionRight,
		" RIGHT ": positionRight,
		"left":    positionLeft,
		"":        positionLeft,
		"invalid": positionLeft,
	}
	for setting, want := range cases {
		if got := sidebarPositionFromSetting(setting); got != want {
			t.Fatalf("sidebarPositionFromSetting(%q) = %v, want %v", setting, got, want)
		}
	}
}

// ─── parsePaneGeometry / targetPaneForPosition ─────────────────────────────

func TestParsePaneGeometryMalformedLine(t *testing.T) {
	if _, ok := parsePaneGeometry("bad-line"); ok {
		t.Fatal("ok = true for a broken line, want false")
	}
	if _, ok := parsePaneGeometry("0 nope %1"); ok {
		t.Fatal("ok = true for an unparseable width, want false")
	}
	geo, ok := parsePaneGeometry("12 30 %2")
	if !ok || geo != (paneGeometry{left: 12, width: 30, paneID: "%2"}) {
		t.Fatalf("parsePaneGeometry = %+v, %v", geo, ok)
	}
}

func TestTargetPaneForLeftPositionPicksLeftmostPane(t *testing.T) {
	output := "40 80 %3\n0 20 %1\n20 20 %2"

	got, ok := targetPaneForPosition(output, positionLeft)
	if !ok || got != "%1" {
		t.Fatalf("targetPaneForPosition = %q, %v; want %q, true", got, ok, "%1")
	}
}

func TestTargetPaneForRightPositionPicksRightmostEdge(t *testing.T) {
	output := "0 20 %1\n20 20 %2\n40 80 %3"

	got, ok := targetPaneForPosition(output, positionRight)
	if !ok || got != "%3" {
		t.Fatalf("targetPaneForPosition = %q, %v; want %q, true", got, ok, "%3")
	}
}

func TestTargetPaneForPositionSkipsMalformedLines(t *testing.T) {
	output := "bad-line\n0 nope %1\n12 30 %2"

	if got, ok := targetPaneForPosition(output, positionLeft); !ok || got != "%2" {
		t.Fatalf("targetPaneForPosition = %q, %v; want %q, true", got, ok, "%2")
	}
	if _, ok := targetPaneForPosition("", positionRight); ok {
		t.Fatal("ok = true for empty output, want false")
	}
}

func TestSplitWindowFlagsForMatchTmuxSemantics(t *testing.T) {
	if got := splitWindowFlagsFor(positionLeft); got != "-hfb" {
		t.Fatalf("splitWindowFlagsFor(left) = %q", got)
	}
	if got := splitWindowFlagsFor(positionRight); got != "-hf" {
		t.Fatalf("splitWindowFlagsFor(right) = %q", got)
	}
}

// ─── uniqueWindowPaths / anySidebarPane ────────────────────────────────────

func TestUniqueWindowPathsDedupesKeepsBlank(t *testing.T) {
	output := "%1|/Users/me/My Project\n%1|/Users/me/My Project\n%2|/tmp/another project"
	want := []windowPath{
		{windowID: "%1", path: "/Users/me/My Project"},
		{windowID: "%2", path: "/tmp/another project"},
	}
	if got := uniqueWindowPaths(output); !reflect.DeepEqual(got, want) {
		t.Fatalf("uniqueWindowPaths = %+v, want %+v", got, want)
	}
}

func TestUniqueWindowPathsSkipsMalformedLines(t *testing.T) {
	output := "bad-line\n%1|/tmp"
	want := []windowPath{{windowID: "%1", path: "/tmp"}}
	if got := uniqueWindowPaths(output); !reflect.DeepEqual(got, want) {
		t.Fatalf("uniqueWindowPaths = %+v, want %+v", got, want)
	}
}

func TestAnySidebarPaneFindsItAnywhere(t *testing.T) {
	if !anySidebarPane("%1|pane\n%2|sidebar\n%3|pane") {
		t.Fatal("want true, there is a sidebar line")
	}
	if anySidebarPane("%1|pane\n%2|main") {
		t.Fatal("want false, there is no sidebar line")
	}
}

// ─── shouldKillWindow ───────────────────────────────────────────────────────

func strp(s string) *string { return &s }
func intp(n int) *int       { return &n }

func TestShouldKillWindowKillsWhenOnlySidebarLeftAndOtherWindowExists(t *testing.T) {
	// Main path: sidebar alone in the window, session still has another
	// window to fall back to. The number of attached clients doesn't matter
	// because killing this window doesn't end the session.
	if !shouldKillWindow(strp("sidebar"), intp(2), nil) {
		t.Fatal("want true")
	}
	if !shouldKillWindow(strp("sidebar"), intp(2), intp(0)) {
		t.Fatal("want true")
	}
	if !shouldKillWindow(strp("sidebar"), intp(2), intp(5)) {
		t.Fatal("want true")
	}
}

func TestShouldKillWindowKeepsWhenNonSidebarPaneRemains(t *testing.T) {
	// Another pane with a @pane_role other than sidebar (e.g. a pane marked
	// as a spawn) keeps the window alive.
	if shouldKillWindow(strp("sidebar\npane"), intp(5), intp(1)) {
		t.Fatal("want false")
	}
	// An unset @pane_role prints an empty line — that's a regular pane, not
	// a sidebar, so the window must be kept. Real tmux output for [sidebar,
	// regular pane] is "sidebar\n\n" (the sidebar's role, then the regular
	// pane's empty role, followed by the trailing record separator).
	if shouldKillWindow(strp("sidebar\n\n"), intp(5), intp(1)) {
		t.Fatal("want false")
	}
	if shouldKillWindow(strp("\nsidebar\n"), intp(5), intp(1)) {
		t.Fatal("want false")
	}
}

func TestShouldKillWindowKeepsWhenListPanesErrors(t *testing.T) {
	// A failed list-panes must never be treated as "empty window" — this
	// avoids racing with tmux being busy and wrongly killing a still-alive
	// window.
	if shouldKillWindow(nil, intp(5), intp(1)) {
		t.Fatal("want false")
	}
}

func TestShouldKillWindowKeepsWhenListPanesEmpty(t *testing.T) {
	// Output that's only whitespace (e.g. the window already disappeared)
	// must also not trigger a kill.
	if shouldKillWindow(strp(""), intp(5), intp(1)) {
		t.Fatal("want false")
	}
	if shouldKillWindow(strp("   \n"), intp(5), intp(1)) {
		t.Fatal("want false")
	}
}

func TestShouldKillWindowKillsLastWindowWhenOneClientAttached(t *testing.T) {
	// One client attached to a session with only this window: killing the
	// session only kicks out that one client, matching tmux's "exit"
	// behavior on the last pane — the user expects the sidebar to disappear
	// along with it.
	if !shouldKillWindow(strp("sidebar"), intp(1), intp(1)) {
		t.Fatal("want true")
	}
}

func TestShouldKillWindowKillsLastWindowWhenNoClientAttached(t *testing.T) {
	// No client attached: killing the session harms no one, and a sidebar
	// stuck in a detached session is meaningless too.
	if !shouldKillWindow(strp("sidebar"), intp(1), intp(0)) {
		t.Fatal("want true")
	}
}

func TestShouldKillWindowKeepsLastWindowWhenManyClientsAttached(t *testing.T) {
	// Core regression guard (0dc6e99): killing a session's last window
	// kicks out every attached client. Multiple terminal tabs attached to
	// "main" would all die at once. Better to leave the sidebar stuck than
	// destroy the session.
	if shouldKillWindow(strp("sidebar"), intp(1), intp(2)) {
		t.Fatal("want false")
	}
	if shouldKillWindow(strp("sidebar"), intp(1), intp(7)) {
		t.Fatal("want false")
	}
}

func TestShouldKillWindowKeepsLastWindowWhenClientCountUnknown(t *testing.T) {
	// Not knowing the number of attached clients means killing can't be
	// proven safe. Better one stuck sidebar than kicking out a whole batch
	// of clients.
	if shouldKillWindow(strp("sidebar"), intp(1), nil) {
		t.Fatal("want false")
	}
}

func TestShouldKillWindowSkipsWhenSessionWindowsErrors(t *testing.T) {
	// When it can't be proven the session has another window, lean toward
	// keeping it — better a stuck sidebar than destroying a still-alive
	// workspace.
	if shouldKillWindow(strp("sidebar"), nil, intp(1)) {
		t.Fatal("want false")
	}
	if shouldKillWindow(strp("sidebar"), intp(0), intp(1)) {
		t.Fatal("want false")
	}
}

// ─── splitLines ─────────────────────────────────────────────────────────────

func TestSplitLinesDropsTrailingEmptyLineFromNewline(t *testing.T) {
	if got := splitLines(""); got != nil {
		t.Fatalf("splitLines(\"\") = %v, want nil", got)
	}
	if got := splitLines("sidebar\n"); !reflect.DeepEqual(got, []string{"sidebar"}) {
		t.Fatalf("splitLines = %v, want [sidebar]", got)
	}
	if got := splitLines("\nsidebar\n"); !reflect.DeepEqual(got, []string{"", "sidebar"}) {
		t.Fatalf("splitLines = %v, want [\"\" sidebar]", got)
	}
}

// ─── the three commands, using tmux.Fake ───────────────────────────────────

func selfBin(t *testing.T) string {
	t.Helper()
	bin, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable(): %v", err)
	}
	return bin
}

// hasCall reports true if fake.Calls() contains exactly one command with
// this argument list — it doesn't pin down the total number of commands,
// it only checks that the expected command is present.
func hasCall(fake *tmux.Fake, want ...string) bool {
	for _, call := range fake.Calls() {
		if reflect.DeepEqual(call, want) {
			return true
		}
	}
	return false
}

func TestToggleNoWindowIdExitsSilently(t *testing.T) {
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()

	if code := Toggle(nil); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if len(fake.Calls()) != 0 {
		t.Fatalf("without a window_id, tmux must not be called, got %v", fake.Calls())
	}
}

func TestToggleSidebarExistsKillsPane(t *testing.T) {
	fake := tmux.NewFake()
	fake.Set("", "display-message", "-t", "%50", "-p", "#{@sidebar_width}")
	fake.Set("", "display-message", "-t", "%50", "-p", "#{@sidebar_position}")
	fake.Set("%2|sidebar\n%1|\n", "list-panes", "-t", "%50", "-F", "#{pane_id}|#{@pane_role}")
	defer tmux.SetRunner(fake)()

	if code := Toggle([]string{"%50"}); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !hasCall(fake, "kill-pane", "-t", "%2") {
		t.Fatalf("missing kill-pane -t %%2 command in %v", fake.Calls())
	}
}

func TestToggleSidebarExistsAndCreateOnlyDoesNothing(t *testing.T) {
	fake := tmux.NewFake()
	fake.Set("", "display-message", "-t", "%50", "-p", "#{@sidebar_width}")
	fake.Set("", "display-message", "-t", "%50", "-p", "#{@sidebar_position}")
	fake.Set("%2|sidebar\n", "list-panes", "-t", "%50", "-F", "#{pane_id}|#{@pane_role}")
	defer tmux.SetRunner(fake)()

	if code := Toggle([]string{"--create-only", "%50"}); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	for _, call := range fake.Calls() {
		if len(call) > 0 && call[0] == "kill-pane" {
			t.Fatalf("--create-only must not kill-pane, got %v", fake.Calls())
		}
	}
}

func TestToggleNoSidebarCreatesPaneAndRestoresFocus(t *testing.T) {
	bin := selfBin(t)
	fake := tmux.NewFake()
	fake.Set("", "display-message", "-t", "%50", "-p", "#{@sidebar_width}")
	fake.Set("right\n", "display-message", "-t", "%50", "-p", "#{@sidebar_position}")
	fake.Set("", "list-panes", "-t", "%50", "-F", "#{pane_id}|#{@pane_role}")
	fake.Set("0 20 %1\n20 20 %2\n", "list-panes", "-t", "%50", "-F", "#{pane_left} #{pane_width} #{pane_id}")
	fake.Set("%2\n", "display-message", "-t", "%50", "-p", "#{pane_id}")
	fake.Set("%9\n", "split-window", "-hf", "-l", "30", "-t", "%2", "-c", "~", "-P", "-F", "#{pane_id}", bin)
	defer tmux.SetRunner(fake)()

	if code := Toggle([]string{"%50"}); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !hasCall(fake, "split-window", "-hf", "-l", "30", "-t", "%2", "-c", "~", "-P", "-F", "#{pane_id}", bin) {
		t.Fatalf("missing split-window command with the correct arguments in %v", fake.Calls())
	}
	if !hasCall(fake, "set", "-t", "%9", "-p", tmux.PaneRole, "sidebar") {
		t.Fatalf("missing set @pane_role=sidebar command for the new pane in %v", fake.Calls())
	}
	if !hasCall(fake, "select-pane", "-t", "%2") {
		t.Fatalf("missing command to return focus to the previously active pane in %v", fake.Calls())
	}
}

func TestToggleUsesPercentSidebarWidth(t *testing.T) {
	bin := selfBin(t)
	fake := tmux.NewFake()
	fake.Set("15%\n", "display-message", "-t", "%50", "-p", "#{@sidebar_width}")
	fake.Set("100\n", "display-message", "-t", "%50", "-p", "#{window_width}")
	fake.Set("", "display-message", "-t", "%50", "-p", "#{@sidebar_position}")
	fake.Set("", "list-panes", "-t", "%50", "-F", "#{pane_id}|#{@pane_role}")
	fake.Set("0 100 %1\n", "list-panes", "-t", "%50", "-F", "#{pane_left} #{pane_width} #{pane_id}")
	fake.Set("%1\n", "display-message", "-t", "%50", "-p", "#{pane_id}")
	fake.Set("%9\n", "split-window", "-hfb", "-l", "15", "-t", "%1", "-c", "~", "-P", "-F", "#{pane_id}", bin)
	defer tmux.SetRunner(fake)()

	if code := Toggle([]string{"%50"}); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	// 100 * 15 / 100 = 15.
	if !hasCall(fake, "split-window", "-hfb", "-l", "15", "-t", "%1", "-c", "~", "-P", "-F", "#{pane_id}", bin) {
		t.Fatalf("missing split-window -l 15 (15%% of 100) in %v", fake.Calls())
	}
}

func TestToggleAllClosesAllWhenAtLeastOneSidebarExists(t *testing.T) {
	fake := tmux.NewFake()
	fake.Set("%1|pane\n%2|sidebar\n%3|sidebar\n", "list-panes", "-a", "-F", "#{pane_id}|#{@pane_role}")
	defer tmux.SetRunner(fake)()

	if code := ToggleAll(nil); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !hasCall(fake, "kill-pane", "-t", "%2") {
		t.Fatalf("missing kill-pane -t %%2 in %v", fake.Calls())
	}
	if !hasCall(fake, "kill-pane", "-t", "%3") {
		t.Fatalf("missing kill-pane -t %%3 in %v", fake.Calls())
	}
}

func TestToggleAllCreatesInEveryWindowWhenNoSidebarExists(t *testing.T) {
	bin := selfBin(t)
	fake := tmux.NewFake()
	fake.Set("%1|\n%2|\n", "list-panes", "-a", "-F", "#{pane_id}|#{@pane_role}")
	fake.Set("$1|/tmp/a\n$2|/tmp/b\n", "list-panes", "-a", "-F", "#{window_id}|#{pane_current_path}")
	// Toggle is called for each window with --create-only: declare enough
	// so it doesn't panic (scenario where there's no sidebar yet).
	for _, w := range []struct{ window, path string }{{"$1", "/tmp/a"}, {"$2", "/tmp/b"}} {
		fake.Set("", "display-message", "-t", w.window, "-p", "#{@sidebar_width}")
		fake.Set("", "display-message", "-t", w.window, "-p", "#{@sidebar_position}")
		fake.Set("", "list-panes", "-t", w.window, "-F", "#{pane_id}|#{@pane_role}")
		fake.Set("0 20 %1\n", "list-panes", "-t", w.window, "-F", "#{pane_left} #{pane_width} #{pane_id}")
		fake.Set("%1\n", "display-message", "-t", w.window, "-p", "#{pane_id}")
		fake.Set("%9\n", "split-window", "-hfb", "-l", "30", "-t", "%1", "-c", w.path, "-P", "-F", "#{pane_id}", bin)
	}
	defer tmux.SetRunner(fake)()

	if code := ToggleAll(nil); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !hasCall(fake, "split-window", "-hfb", "-l", "30", "-t", "%1", "-c", "/tmp/a", "-P", "-F", "#{pane_id}", bin) {
		t.Fatalf("missing split-window for $1 in %v", fake.Calls())
	}
	if !hasCall(fake, "split-window", "-hfb", "-l", "30", "-t", "%1", "-c", "/tmp/b", "-P", "-F", "#{pane_id}", bin) {
		t.Fatalf("missing split-window for $2 in %v", fake.Calls())
	}
}

func TestAutoCloseNoWindowIdExitsSilently(t *testing.T) {
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()

	if code := AutoClose(nil); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if len(fake.Calls()) != 0 {
		t.Fatalf("without a window_id, tmux must not be called, got %v", fake.Calls())
	}
}

func TestAutoCloseKillsWindowWhenOnlySidebarLeft(t *testing.T) {
	fake := tmux.NewFake()
	fake.Set("sidebar\n", "list-panes", "-t", "@3", "-F", "#{@pane_role}")
	fake.Set("2\n", "display-message", "-t", "@3", "-p", "#{session_windows}")
	fake.Set("1\n", "display-message", "-t", "@3", "-p", "#{session_attached}")
	defer tmux.SetRunner(fake)()

	if code := AutoClose([]string{"@3"}); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !hasCall(fake, "kill-window", "-t", "@3") {
		t.Fatalf("missing kill-window -t @3 in %v", fake.Calls())
	}
}

func TestAutoCloseKeepsWindowWhenOtherPaneRemains(t *testing.T) {
	fake := tmux.NewFake()
	fake.Set("sidebar\n\n", "list-panes", "-t", "@3", "-F", "#{@pane_role}")
	fake.Set("2\n", "display-message", "-t", "@3", "-p", "#{session_windows}")
	fake.Set("1\n", "display-message", "-t", "@3", "-p", "#{session_attached}")
	defer tmux.SetRunner(fake)()

	if code := AutoClose([]string{"@3"}); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	for _, call := range fake.Calls() {
		if len(call) > 0 && call[0] == "kill-window" {
			t.Fatalf("must not kill-window while another pane remains, got %v", fake.Calls())
		}
	}
}

func TestAutoCloseKeepsWindowWhenListPanesErrors(t *testing.T) {
	fake := tmux.NewFake()
	fake.SetErr(errors.New("busy"), "list-panes", "-t", "@3", "-F", "#{@pane_role}")
	fake.Set("2\n", "display-message", "-t", "@3", "-p", "#{session_windows}")
	fake.Set("1\n", "display-message", "-t", "@3", "-p", "#{session_attached}")
	defer tmux.SetRunner(fake)()

	if code := AutoClose([]string{"@3"}); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	for _, call := range fake.Calls() {
		if len(call) > 0 && call[0] == "kill-window" {
			t.Fatalf("must not kill-window when list-panes fails, got %v", fake.Calls())
		}
	}
}
