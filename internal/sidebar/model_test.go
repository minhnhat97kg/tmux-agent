package sidebar

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"tmux-agent/internal/clock"
	"tmux-agent/internal/git"
	"tmux-agent/internal/scrape"
	"tmux-agent/internal/tmux"
)

// pane3Rows builds 3 ready-made pane rows (no header) shared by cursor
// clamping tests — only counting/clamping matters, no real data needed.
func pane3Rows() []Row {
	return []Row{
		{PaneID: "%1"},
		{PaneID: "%2"},
		{PaneID: "%3"},
	}
}

// paneRowsOnly drops window headers, for tests about pane rows only.
func paneRowsOnly(rows []Row) []Row {
	var out []Row
	for _, r := range rows {
		if !r.IsHeader {
			out = append(out, r)
		}
	}
	return out
}

// ─── buildRows ──────────────────────────────────────────────────────────────
//
// buildRows now groups by git repo (group.go), no longer by tmux session —
// the header is the repo root basename (or the raw path basename if the pane
// isn't inside any git repo), not SessionName. Tests use a path outside any
// real git repo (the basename of that path itself, see
// group.go::groupKeyForPath) so they don't depend on the git repo of the
// machine running the test.

func TestBuildRowsEmptyList(t *testing.T) {
	rows := buildRows(nil, RepoFilterAll, rowExtras{})
	if len(rows) != 0 {
		t.Fatalf("buildRows(nil, ...) = %+v, want empty", rows)
	}
}

func TestBuildRowsOneRepoOnePane(t *testing.T) {
	sessions := []tmux.SessionInfo{{
		SessionName: "work",
		Windows: []tmux.WindowInfo{{WindowID: "@1", WindowIndex: "1", WindowName: "main", Panes: []tmux.PaneInfo{
			{PaneID: "%1", Agent: tmux.AgentClaude, Status: tmux.StatusRunning, Path: "/tmp/not-a-git-repo/tmux-agent"},
		}}},
	}}
	got := buildRows(sessions, RepoFilterAll, rowExtras{CurrentWindow: "@1"})
	want := []Row{
		{IsHeader: true, Header: "1 main", HeaderFocus: true},
		{Status: tmux.StatusRunning, Agent: tmux.AgentClaude, Base: "tmux-agent", PaneID: "%1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildRows = %+v, want %+v", got, want)
	}
}

// Rows follow tmux order (session, window, pane) under one header per
// window, prefixed with the session when there are several (user feedback:
// "sort by window position … make clear which window each pane is in").
func TestBuildRowsWindowOrderWithHeaders(t *testing.T) {
	sessions := []tmux.SessionInfo{
		{SessionName: "s1", Windows: []tmux.WindowInfo{
			{WindowID: "@1", WindowIndex: "1", WindowName: "web", Panes: []tmux.PaneInfo{
				{Agent: tmux.AgentClaude, Status: tmux.StatusRunning, Path: "/z/repo-z"},
			}},
			{WindowID: "@2", WindowIndex: "2", WindowName: "empty"},
		}},
		{SessionName: "s2", Windows: []tmux.WindowInfo{{WindowID: "@3", WindowIndex: "0", WindowName: "api", Panes: []tmux.PaneInfo{
			{Agent: tmux.AgentOpenCode, Status: tmux.StatusError, Path: "/a/repo-a"},
		}}}},
	}
	got := buildRows(sessions, RepoFilterAll, rowExtras{CurrentWindow: "@3"})
	want := []Row{
		{IsHeader: true, Header: "s1:1 web"},
		{Status: tmux.StatusRunning, Agent: tmux.AgentClaude, Base: "repo-z"},
		{IsHeader: true, Header: "s2:0 api", HeaderFocus: true},
		{Status: tmux.StatusError, Agent: tmux.AgentOpenCode, Base: "repo-a"},
		{IsInfoChild: true, InfoLabel: "failed — check the pane", InfoAction: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildRows = %+v, want %+v", got, want)
	}
}

func TestFocusedPaneAndPaneIndexOf(t *testing.T) {
	sessions := []tmux.SessionInfo{{Windows: []tmux.WindowInfo{
		{WindowID: "@1", Panes: []tmux.PaneInfo{{PaneID: "%1"}, {PaneID: "%2", PaneActive: true}}},
		{WindowID: "@2", Panes: []tmux.PaneInfo{{PaneID: "%3", PaneActive: true}}},
	}}}
	if got := focusedPane(sessions, "@1"); got != "%2" {
		t.Fatalf("focusedPane(@1) = %q, want %%2", got)
	}
	rows := buildRows(sessions, RepoFilterAll, rowExtras{})
	if idx, ok := paneIndexOf(rows, "%3"); !ok || idx != 2 {
		t.Fatalf("paneIndexOf(%%3) = %d, %v — want 2 (headers don't count)", idx, ok)
	}
}

func TestBuildRowsFilterKeepsOnlyMatchingGroup(t *testing.T) {
	sessions := []tmux.SessionInfo{
		{SessionName: "s", Windows: []tmux.WindowInfo{
			{Panes: []tmux.PaneInfo{{Agent: tmux.AgentClaude, Status: tmux.StatusRunning, Path: "/a/repo-a"}}},
			{Panes: []tmux.PaneInfo{{Agent: tmux.AgentCodex, Status: tmux.StatusRunning, Path: "/b/repo-b"}}},
		}},
	}
	got := paneRowsOnly(buildRows(sessions, "repo-b", rowExtras{}))
	want := []Row{{Status: tmux.StatusRunning, Agent: tmux.AgentCodex, Base: "repo-b"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildRows filtered to repo-b = %+v, want %+v", got, want)
	}
}

// A pane that needs the user stays in place and carries an inline action
// line (user feedback: "if it requires action, show it right in the list").
func TestBuildRowsPaneNeedingActionStaysInPlaceWithActionLine(t *testing.T) {
	sessions := []tmux.SessionInfo{
		{Windows: []tmux.WindowInfo{
			{Panes: []tmux.PaneInfo{{PaneID: "%1", Agent: tmux.AgentClaude, Status: tmux.StatusRunning, Path: "/a/repo-a"}}},
			{Panes: []tmux.PaneInfo{{PaneID: "%2", Agent: tmux.AgentCodex, Status: tmux.StatusWaiting, WaitReason: "permission_prompt", Path: "/b/repo-b"}}},
		}},
	}
	got := paneRowsOnly(buildRows(sessions, RepoFilterAll, rowExtras{}))
	want := []Row{
		{Status: tmux.StatusRunning, Agent: tmux.AgentClaude, Base: "repo-a", PaneID: "%1"},
		{Status: tmux.StatusWaiting, Agent: tmux.AgentCodex, Base: "repo-b", PaneID: "%2", NeedsConfirm: true},
		{IsInfoChild: true, InfoLabel: "needs approval", InfoAction: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildRows = %+v, want %+v", got, want)
	}
}

func TestActionLine(t *testing.T) {
	cases := []struct {
		p    tmux.PaneInfo
		want string
	}{
		{tmux.PaneInfo{Status: tmux.StatusRunning}, ""},
		{tmux.PaneInfo{Status: tmux.StatusIdle}, ""},
		{tmux.PaneInfo{Status: tmux.StatusWaiting, WaitReason: "permission_prompt"}, "needs approval"},
		{tmux.PaneInfo{Status: tmux.StatusWaiting, WaitReason: "elicitation_dialog"}, "has a question for you"},
		{tmux.PaneInfo{Status: tmux.StatusWaiting, WaitReason: "idle_prompt"}, "waiting for your input"},
		{tmux.PaneInfo{Status: tmux.StatusWaiting}, "needs your input"},
		{tmux.PaneInfo{Status: tmux.StatusError, WaitReason: "rate limited"}, "failed: rate limited"},
	}
	for _, c := range cases {
		if got := ActionLine(c.p); got != c.want {
			t.Errorf("ActionLine(%s/%q) = %q, want %q", c.p.Status, c.p.WaitReason, got, c.want)
		}
	}
}

// ─── elapsedLabel (elapsed time of the current run so far) ──────────────

func TestElapsedLabelUnderOneMinuteShowsSecondsOnly(t *testing.T) {
	t.Setenv(clock.EnvNow, "1000")
	got := elapsedLabel(tmux.PaneInfo{StartedAt: 988})
	if got != "12s" {
		t.Fatalf("elapsedLabel = %q, want %q", got, "12s")
	}
}

func TestElapsedLabelUnderOneHourShowsMinutesAndSeconds(t *testing.T) {
	t.Setenv(clock.EnvNow, "1000")
	got := elapsedLabel(tmux.PaneInfo{StartedAt: 1000 - (3*60 + 12)})
	if got != "3m12s" {
		t.Fatalf("elapsedLabel = %q, want %q", got, "3m12s")
	}
}

func TestElapsedLabelOneHourOrMoreShows1hPlus(t *testing.T) {
	t.Setenv(clock.EnvNow, "10000")
	got := elapsedLabel(tmux.PaneInfo{StartedAt: 10000 - 3600})
	if got != "1h+" {
		t.Fatalf("elapsedLabel = %q, want %q", got, "1h+")
	}
}

func TestElapsedLabelNoStartedAtIsEmpty(t *testing.T) {
	if got := elapsedLabel(tmux.PaneInfo{}); got != "" {
		t.Fatalf("elapsedLabel = %q, want empty", got)
	}
}

// ─── rowExtras.Dirty (last-known cache from detailFetchCmd — see model.go) ──

func TestBuildRowsUsesExtrasDirtyByPaneID(t *testing.T) {
	sessions := []tmux.SessionInfo{
		{Windows: []tmux.WindowInfo{{Panes: []tmux.PaneInfo{
			{PaneID: "%1", Agent: tmux.AgentClaude, Status: tmux.StatusRunning, Path: "/a/repo-a"},
		}}}},
	}

	got := paneRowsOnly(buildRows(sessions, RepoFilterAll, rowExtras{Dirty: map[string]bool{"%1": true}}))
	if len(got) != 1 || !got[0].Dirty {
		t.Fatalf("buildRows = %+v, want the pane row to have Dirty=true", got)
	}
}

// buildRows must nest a subagent branch RIGHT AFTER the parent pane row (part
// 6, real-user feedback: "the tree currently sits at root", "shouldn't need
// to press i to see it") — and cursor/paneRowCount must skip those branches,
// stopping only on real pane rows.
func TestBuildRowsSubagentBranchNestedRightAfterParentCursorSkipsIt(t *testing.T) {
	sessions := []tmux.SessionInfo{
		{Windows: []tmux.WindowInfo{{Panes: []tmux.PaneInfo{
			{PaneID: "%1", Agent: tmux.AgentClaude, Status: tmux.StatusRunning, Path: "/a/repo-a", Subagents: []string{"Explore", "general-purpose"}},
			{PaneID: "%2", Agent: tmux.AgentClaude, Status: tmux.StatusIdle, Path: "/a/repo-a"},
		}}}},
	}

	got := paneRowsOnly(buildRows(sessions, RepoFilterAll, rowExtras{}))
	if len(got) != 4 {
		t.Fatalf("row count = %d, want 4 (pane1 + 2 branches + pane2), content: %+v", len(got), got)
	}
	if got[1].IsSubagentChild != true || got[1].SubagentLabel != "├─ Explore" {
		t.Fatalf("got[1] = %+v, want the first branch \"├─ Explore\"", got[1])
	}
	if got[2].IsSubagentChild != true || got[2].SubagentLabel != "└─ general-purpose" {
		t.Fatalf("got[2] = %+v, want the last branch \"└─ general-purpose\"", got[2])
	}
	if got[3].PaneID != "%2" {
		t.Fatalf("got[3].PaneID = %q, want %q (a real pane row, not a branch)", got[3].PaneID, "%2")
	}

	// Cursor: only 2 REAL pane rows (paneRowCount skips the 2 branches).
	if n := paneRowCount(got); n != 2 {
		t.Fatalf("paneRowCount = %d, want 2", n)
	}
	// cursor=1 must point exactly at %2, not offset by the 2 subagent
	// branches wedged between %1 and %2.
	if row, ok := paneRowAt(got, 1); !ok || row.PaneID != "%2" {
		t.Fatalf("paneRowAt(got, 1) = %+v, %v — want pane %%2", row, ok)
	}
}

// buildRows: the action line nests RIGHT AFTER the parent pane
// row, BEFORE the subagent branch (when both are present) — and neither one
// counts toward the cursor.
func TestBuildRowsInfoLineBeforeSubagentBranchCursorSkipsBoth(t *testing.T) {
	sessions := []tmux.SessionInfo{
		{Windows: []tmux.WindowInfo{{Panes: []tmux.PaneInfo{
			{PaneID: "%1", Agent: tmux.AgentClaude, Status: tmux.StatusWaiting, WaitReason: "idle_prompt", Path: "/a/repo-a",
				Subagents: []string{"Explore"}},
			{PaneID: "%2", Agent: tmux.AgentClaude, Status: tmux.StatusIdle, Path: "/a/repo-a"},
		}}}},
	}

	got := paneRowsOnly(buildRows(sessions, RepoFilterAll, rowExtras{}))
	// pane1, info, subagent branch, pane2 = 4.
	if len(got) != 4 {
		t.Fatalf("row count = %d, want 4, content: %+v", len(got), got)
	}
	if got[1].IsInfoChild != true || got[1].InfoLabel != "waiting for your input" {
		t.Fatalf("got[1] = %+v, want the action line \"waiting for your input\"", got[1])
	}
	if got[2].IsSubagentChild != true || got[2].SubagentLabel != "└─ Explore" {
		t.Fatalf("got[2] = %+v, want the subagent branch \"└─ Explore\"", got[2])
	}
	if n := paneRowCount(got); n != 2 {
		t.Fatalf("paneRowCount = %d, want 2 (skip info + branch)", n)
	}
	if row, ok := paneRowAt(got, 1); !ok || row.PaneID != "%2" {
		t.Fatalf("paneRowAt(got, 1) = %+v, %v — want pane %%2", row, ok)
	}
}

// ─── Update ─────────────────────────────────────────────────────────────────

func TestUpdateRefreshMsgReplacesRowList(t *testing.T) {
	m := NewModel("%sidebar")
	sessions := []tmux.SessionInfo{
		{SessionName: "work", Windows: []tmux.WindowInfo{
			{Panes: []tmux.PaneInfo{
				{Agent: tmux.AgentClaude, Status: tmux.StatusRunning, Path: "/x/proj"},
			}},
		}},
	}

	updated, cmd := m.Update(RefreshMsg{Sessions: sessions, Theme: DefaultTheme(), Icons: DefaultIcons()})
	next := updated.(Model)

	if len(next.rows) != 2 {
		t.Fatalf("row count after RefreshMsg = %d, want 2 (window header + 1 pane)", len(next.rows))
	}
	if next.rows[1].Base != "proj" {
		t.Fatalf("rows[1].Base = %q, want %q", next.rows[1].Base, "proj")
	}
	// RefreshMsg now ALWAYS emits detailRefreshCmd (the summary panel at the
	// bottom of the pane, part 6, replacing the old "i" popup) — the
	// freshly built list has 1 pane, the cursor defaults to 0 pointing right
	// at it, so Cmd is no longer nil like it was for the old popup version.
	if cmd == nil {
		t.Fatalf("RefreshMsg must emit detailRefreshCmd when there's a pane under the cursor, got nil")
	}
}

func TestUpdateKeyQReturnsTeaQuit(t *testing.T) {
	m := NewModel("%sidebar")

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("the q key must emit a Cmd (tea.Quit), got nil")
	}
	// tea.Quit returns a tea.QuitMsg when the Cmd is called — compare
	// indirectly via type.
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("the q key must emit tea.QuitMsg, got %T", msg)
	}
}

func TestUpdateCtrlCReturnsTeaQuit(t *testing.T) {
	m := NewModel("%sidebar")

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c must emit a Cmd (tea.Quit), got nil")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("ctrl+c must emit tea.QuitMsg, got %T", cmd())
	}
}

// Pressing j then q right within the same debounce beat (before AnimTick has
// a chance to write): the q key must flush @sidebar_cursor itself before
// Quitting, otherwise the cursor position just moved to would be lost.
func TestUpdateKeyQFlushesPendingCursorWriteBeforeQuit(t *testing.T) {
	fake := tmux.NewFake()
	restore := tmux.SetRunner(fake)
	defer restore()

	m := NewModel("%sidebar")
	m.rows = pane3Rows()
	m.cursor = 1
	m.cursorDirty = true

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("the q key while cursorDirty must emit a Cmd, got nil")
	}

	msg := cmd()
	seq := reflect.ValueOf(msg)
	if seq.Kind() != reflect.Slice || seq.Len() != 2 {
		t.Fatalf("the q key while cursorDirty must emit a tea.Sequence with 2 cmds (write cursor then quit), got %#v", msg)
	}

	first, ok := seq.Index(0).Interface().(tea.Cmd)
	if !ok {
		t.Fatalf("the first cmd in the Sequence must be a tea.Cmd, got %T", seq.Index(0).Interface())
	}
	first() // run saveCursorCmd

	found := false
	for _, call := range fake.Calls() {
		if len(call) >= 3 && call[0] == "set" && call[2] == tmux.SidebarCursor {
			found = true
		}
	}
	if !found {
		t.Fatal("the q key must flush the @sidebar_cursor write before exiting, no set command seen")
	}

	second, ok := seq.Index(1).Interface().(tea.Cmd)
	if !ok {
		t.Fatalf("the second cmd in the Sequence must be a tea.Cmd, got %T", seq.Index(1).Interface())
	}
	if _, ok := second().(tea.QuitMsg); !ok {
		t.Fatalf("the second cmd in the Sequence must be tea.Quit, got %T", second())
	}
}

// Part 6 dropped the fixed bottom panel (details moved to a popup, see
// detail.go) — the viewport no longer sets aside room for anything else
// besides itself, minus exactly 1 line for filterBarHeight.
func TestUpdateWindowSizeMsgUpdatesDimensions(t *testing.T) {
	m := NewModel("%sidebar")

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 42, Height: 20})
	next := updated.(Model)

	if next.width != 42 || next.height != 20 {
		t.Fatalf("size after WindowSizeMsg = %dx%d, want 42x20", next.width, next.height)
	}
	// 20 - avatarHeight(4) - footerHeight(1) = 15.
	if next.viewport.Width != 42 || next.viewport.Height != 15 {
		t.Fatalf("viewport after WindowSizeMsg = %dx%d, want 42x15 (avatar on top, key hints below)", next.viewport.Width, next.viewport.Height)
	}
}

func TestUpdateWindowSizeMsgHeightTooSmallStaysNonNegative(t *testing.T) {
	m := NewModel("%sidebar")

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 20, Height: 0})
	next := updated.(Model)

	if next.viewport.Height != 0 {
		t.Fatalf("viewport.Height with a 0-height pane = %d, want 0 (not negative)", next.viewport.Height)
	}
}

func TestUpdateAnimTickMsgAdvancesFrameAndReschedules(t *testing.T) {
	m := NewModel("%sidebar")

	updated, cmd := m.Update(AnimTickMsg{})
	next := updated.(Model)

	if next.animFrame != 1 {
		t.Fatalf("animFrame after AnimTickMsg = %d, want 1", next.animFrame)
	}
	if cmd == nil {
		t.Fatal("AnimTickMsg must schedule the next tick itself, got cmd = nil")
	}
}

func TestUpdateRefreshTickMsgDoesNotDoubleTick(t *testing.T) {
	m := NewModel("%sidebar")

	_, cmd := m.Update(refreshTickMsg{})
	if cmd == nil {
		t.Fatal("refreshTickMsg must emit a Cmd (query + next tick)")
	}
}

func TestUpdateRefreshRequestMsgDoesNotScheduleTick(t *testing.T) {
	// refreshRequestMsg (from SIGUSR1) only requests fresh data, it must not
	// schedule an extra tick — otherwise a burst of SIGUSR1 would double up
	// the running 1s tick.
	m := NewModel("%sidebar")

	_, cmd := m.Update(refreshRequestMsg{})
	if cmd == nil {
		t.Fatal("refreshRequestMsg must emit a Cmd requesting data")
	}
	// Once the Cmd resolves it must yield RefreshMsg (data), not refreshTickMsg.
	msg := cmd()
	if _, ok := msg.(RefreshMsg); !ok {
		t.Fatalf("refreshRequestMsg must resolve to RefreshMsg, got %T", msg)
	}
}

// ─── clampCursor / moveCursor / paneIDAt (pure functions) ────────────────────────

func TestClampCursorClampsWhenListShrinks(t *testing.T) {
	// Cursor is at the last row (2) of a 3-row list; the list shrinks to 1.
	got := clampCursor(2, 1)
	if got != 0 {
		t.Fatalf("clampCursor(2, 1) = %d, want 0 (last row of the new list)", got)
	}
}

func TestClampCursorEmptyListKeepsValue(t *testing.T) {
	// Empty list: nothing to clamp against — keep the old value, matching
	// rebuild_row_targets_empty_groups from the reference implementation
	// (state.rs).
	got := clampCursor(5, 0)
	if got != 5 {
		t.Fatalf("clampCursor(5, 0) = %d, want 5 (unchanged, not clamped to 0)", got)
	}
}

func TestClampCursorListGrowsKeepsValue(t *testing.T) {
	// The list grows: the old cursor is still valid, unchanged.
	got := clampCursor(1, 5)
	if got != 1 {
		t.Fatalf("clampCursor(1, 5) = %d, want 1 (still valid, unchanged)", got)
	}
}

func TestClampCursorLowerBoundNotNegative(t *testing.T) {
	got := clampCursor(-3, 5)
	if got != 0 {
		t.Fatalf("clampCursor(-3, 5) = %d, want 0", got)
	}
}

func TestMoveCursorUpperBoundDoesNotOverflow(t *testing.T) {
	next, moved := moveCursor(0, -1, 3)
	if moved || next != 0 {
		t.Fatalf("moveCursor(0, -1, 3) = (%d, %v), want (0, false)", next, moved)
	}
}

func TestMoveCursorLowerBoundDoesNotOverflow(t *testing.T) {
	next, moved := moveCursor(2, 1, 3)
	if moved || next != 2 {
		t.Fatalf("moveCursor(2, 1, 3) = (%d, %v), want (2, false)", next, moved)
	}
}

func TestMoveCursorEmptyListDoesNotMove(t *testing.T) {
	next, moved := moveCursor(0, 1, 0)
	if moved || next != 0 {
		t.Fatalf("moveCursor(0, 1, 0) = (%d, %v), want (0, false)", next, moved)
	}
}

func TestMoveCursorMovesNormally(t *testing.T) {
	next, moved := moveCursor(0, 1, 3)
	if !moved || next != 1 {
		t.Fatalf("moveCursor(0, 1, 3) = (%d, %v), want (1, true)", next, moved)
	}
	next, moved = moveCursor(next, -1, 3)
	if !moved || next != 0 {
		t.Fatalf("moveCursor(1, -1, 3) = (%d, %v), want (0, true)", next, moved)
	}
}

func TestPaneIDAtEmptyListReturnsFalse(t *testing.T) {
	if _, ok := paneIDAt(nil, 0); ok {
		t.Fatal("paneIDAt(nil, 0) must return ok=false")
	}
}

func TestPaneIDAtSkipsHeaderRows(t *testing.T) {
	rows := []Row{
		{IsHeader: true, Header: "work"},
		{PaneID: "%1"},
		{IsHeader: true, Header: "other"},
		{PaneID: "%2"},
	}
	id, ok := paneIDAt(rows, 1)
	if !ok || id != "%2" {
		t.Fatalf("paneIDAt(rows, 1) = (%q, %v), want (%%2, true) — the cursor count skips headers", id, ok)
	}
}

// ─── rowIndexOfCursor (line index within rows, including headers — used for scrolling) ─

func TestRowIndexOfCursorSkipsHeader(t *testing.T) {
	rows := []Row{
		{IsHeader: true, Header: "work"},
		{PaneID: "%1"},
		{PaneID: "%2"},
	}
	idx, ok := rowIndexOfCursor(rows, 1)
	if !ok || idx != 2 {
		t.Fatalf("rowIndexOfCursor(rows, 1) = (%d, %v), want (2, true)", idx, ok)
	}
}

func TestRowIndexOfCursorEmptyListReturnsFalse(t *testing.T) {
	if _, ok := rowIndexOfCursor(nil, 0); ok {
		t.Fatal("rowIndexOfCursor(nil, 0) must return ok=false")
	}
}

// ─── Invariants when RefreshMsg changes the list ──────────────────────────────────

func TestUpdateRefreshMsgClampsCursorWhenListShrinks(t *testing.T) {
	m := NewModel("%sidebar")
	m.rows = pane3Rows()
	m.cursor = 2 // pointing at %3

	sessions := []tmux.SessionInfo{
		{SessionName: "s", Windows: []tmux.WindowInfo{{Panes: []tmux.PaneInfo{
			{PaneID: "%1", Agent: tmux.AgentClaude, Status: tmux.StatusRunning, Path: "/a"},
		}}}},
	}
	updated, _ := m.Update(RefreshMsg{Sessions: sessions})
	m = updated.(Model)

	if m.cursor != 0 {
		t.Fatalf("cursor after the list shrinks to 1 row = %d, want 0", m.cursor)
	}
}

func TestUpdateRefreshMsgEmptyListDoesNotPanic(t *testing.T) {
	m := NewModel("%sidebar")
	m.rows = pane3Rows()
	m.cursor = 2

	updated, _ := m.Update(RefreshMsg{Sessions: nil})
	m = updated.(Model)

	if _, ok := paneIDAt(m.rows, m.cursor); ok {
		t.Fatal("with an empty list, no row should be valid")
	}
	// Enter on an empty list must do nothing (no panic, no Cmd calling tmux).
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("Enter on an empty list must not emit a Cmd")
	}
}

func TestUpdateRefreshMsgListGrowsKeepsCursor(t *testing.T) {
	m := NewModel("%sidebar")
	m.rows = pane3Rows()[:1] // 1 row
	m.cursor = 0

	sessions := []tmux.SessionInfo{
		{SessionName: "s", Windows: []tmux.WindowInfo{{Panes: []tmux.PaneInfo{
			{PaneID: "%1", Agent: tmux.AgentClaude, Status: tmux.StatusRunning, Path: "/a"},
			{PaneID: "%2", Agent: tmux.AgentClaude, Status: tmux.StatusRunning, Path: "/b"},
			{PaneID: "%3", Agent: tmux.AgentClaude, Status: tmux.StatusRunning, Path: "/c"},
		}}}},
	}
	updated, _ := m.Update(RefreshMsg{Sessions: sessions})
	m = updated.(Model)

	if m.cursor != 0 {
		t.Fatalf("cursor after the list grows = %d, want 0 (unchanged)", m.cursor)
	}
}

func TestUpdateRefreshMsgPaneAboveDisappearsKeepsCursorOnPane(t *testing.T) {
	// The pane ABOVE the cursor disappears: the list shifts up, and the
	// cursor follows its pane (applyRows re-resolves by pane id) instead of
	// landing on the next one. Three panes in the SAME repo group (same
	// path) so that "above" only counts panes, not other headers.
	m := NewModel("%sidebar")
	sessions := []tmux.SessionInfo{
		{SessionName: "work", Windows: []tmux.WindowInfo{{Panes: []tmux.PaneInfo{
			{PaneID: "%1", Agent: tmux.AgentClaude, Status: tmux.StatusRunning, Path: "/a"},
			{PaneID: "%2", Agent: tmux.AgentClaude, Status: tmux.StatusRunning, Path: "/a"},
			{PaneID: "%3", Agent: tmux.AgentClaude, Status: tmux.StatusRunning, Path: "/a"},
		}}}},
	}
	updated, _ := m.Update(RefreshMsg{Sessions: sessions})
	m = updated.(Model)
	m.cursor = 1 // pointing at %2

	if id, ok := paneIDAt(m.rows, m.cursor); !ok || id != "%2" {
		t.Fatalf("before %%1 disappears, the cursor must point at %%2, got %q ok=%v", id, ok)
	}

	// %1 (above the cursor) disappears — only %2, %3 remain.
	sessionsAfter := []tmux.SessionInfo{
		{SessionName: "work", Windows: []tmux.WindowInfo{{Panes: []tmux.PaneInfo{
			{PaneID: "%2", Agent: tmux.AgentClaude, Status: tmux.StatusRunning, Path: "/a"},
			{PaneID: "%3", Agent: tmux.AgentClaude, Status: tmux.StatusRunning, Path: "/a"},
		}}}},
	}
	updated, _ = m.Update(RefreshMsg{Sessions: sessionsAfter})
	m = updated.(Model)

	if id, ok := paneIDAt(m.rows, m.cursor); !ok || id != "%2" {
		t.Fatalf("after %%1 disappears the cursor must stay on %%2, got %q ok=%v", id, ok)
	}
}

// ─── j/k/Enter keys and debounced @sidebar_cursor writes ─────────────────────────

func TestUpdateKeyJMovesDownAndMarksDirty(t *testing.T) {
	m := NewModel("%sidebar")
	m.rows = pane3Rows()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	next := updated.(Model)

	if next.cursor != 1 {
		t.Fatalf("cursor after the j key = %d, want 1", next.cursor)
	}
	if !next.cursorDirty {
		t.Fatal("a successful j move must mark cursorDirty so the debounced tmux write fires")
	}
}

func TestUpdateKeyKAtTopBoundDoesNotMarkDirty(t *testing.T) {
	m := NewModel("%sidebar")
	m.rows = pane3Rows()
	m.cursor = 0

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	next := updated.(Model)

	if next.cursor != 0 {
		t.Fatalf("cursor at the top bound after the k key = %d, want 0", next.cursor)
	}
	if next.cursorDirty {
		t.Fatal("the k key at the bound (no movement possible) must not mark dirty")
	}
}

func TestUpdateKeyJAtBottomBoundDoesNotOverflow(t *testing.T) {
	m := NewModel("%sidebar")
	m.rows = pane3Rows()
	m.cursor = 2 // last row

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	next := updated.(Model)

	if next.cursor != 2 {
		t.Fatalf("cursor at the bottom bound after the down key = %d, want 2", next.cursor)
	}
}

func TestUpdateAnimTickCoalescesMultipleCursorMovesIntoOneTmuxWrite(t *testing.T) {
	fake := tmux.NewFake()
	restore := tmux.SetRunner(fake)
	defer restore()

	m := NewModel("%sidebar")
	m.rows = pane3Rows()
	fake.Set("", "set", "-g", tmux.SidebarCursor, "2") // cursor will be at row 2 after 2 j presses

	// Hold the j key twice in a row within the same AnimTick beat — the
	// debounce must coalesce this into exactly one write on the next
	// AnimTickMsg.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(Model)

	if !m.cursorDirty {
		t.Fatal("a cursor change must mark cursorDirty before AnimTick arrives")
	}

	updated, cmd := m.Update(AnimTickMsg{})
	m = updated.(Model)
	if cmd == nil {
		t.Fatal("AnimTickMsg with cursorDirty must emit a Cmd")
	}
	if !m.cursorSaveInFlight {
		t.Fatal("AnimTickMsg firing the cursor write must mark cursorSaveInFlight")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("AnimTickMsg with cursorDirty must emit a tea.Batch with 2 cmds (next tick + cursor write), got %#v", cmd())
	}
	// Only run the cursor-write cmd (batch[1]) — DO NOT run batch[0]
	// (tickAnimCmd), it has a real tea.Tick that would block the test for
	// 200ms.
	saveMsg := batch[1]()

	writes := 0
	for _, call := range fake.Calls() {
		if len(call) >= 3 && call[0] == "set" && call[2] == tmux.SidebarCursor {
			writes++
		}
	}
	if writes != 1 {
		t.Fatalf("number of @sidebar_cursor writes = %d, want exactly 1 despite pressing j twice", writes)
	}

	// A successful write (Fake doesn't declare an error) — cursorSavedMsg
	// must clear cursorDirty, and NOT clear it before knowing the result
	// (debt 3a Minor 2).
	updated, _ = m.Update(saveMsg)
	m = updated.(Model)
	if m.cursorDirty {
		t.Fatal("cursorSavedMsg{Success:true} must clear cursorDirty")
	}
	if m.cursorSaveInFlight {
		t.Fatal("cursorSavedMsg must clear cursorSaveInFlight whether it succeeded or failed")
	}
}

func TestUpdateAnimTickDoesNotWriteWhenCursorUnchanged(t *testing.T) {
	fake := tmux.NewFake()
	restore := tmux.SetRunner(fake)
	defer restore()

	m := NewModel("%sidebar")
	m.rows = pane3Rows()

	m.Update(AnimTickMsg{})

	for _, call := range fake.Calls() {
		if len(call) >= 3 && call[0] == "set" && call[2] == tmux.SidebarCursor {
			t.Fatalf("with no cursor change, there must be no tmux write, got command %v", call)
		}
	}
}

// Debt 3a Minor 2: if writing @sidebar_cursor fails, cursorDirty MUST remain
// set (not cleared before knowing success) so the next AnimTick retries the
// write — porting global.rs:86-101 (flush_pending_cursor_save only clears
// the flag when save_cursor() succeeds).
func TestUpdateCursorSavedMsgFailureKeepsDirtyForRetry(t *testing.T) {
	m := NewModel("%sidebar")
	m.cursorDirty = true
	m.cursorSaveInFlight = true

	updated, _ := m.Update(cursorSavedMsg{Success: false})
	next := updated.(Model)

	if !next.cursorDirty {
		t.Fatal("a failed write must keep cursorDirty=true so the next beat retries it")
	}
	if next.cursorSaveInFlight {
		t.Fatal("cursorSaveInFlight must turn off after receiving the result, whether success or failure")
	}
}

func TestUpdateAnimTickDoesNotFireExtraCommandWhileOneInFlight(t *testing.T) {
	fake := tmux.NewFake()
	restore := tmux.SetRunner(fake)
	defer restore()

	m := NewModel("%sidebar")
	m.cursorDirty = true
	m.cursorSaveInFlight = true // simulate a write already in flight, no result yet

	_, cmd := m.Update(AnimTickMsg{})
	// Only tickAnimCmd, no extra cursor-write command stacked on top. Call
	// cmd() EXACTLY ONCE: here cmd (with only 1 valid cmd, bubbletea returns
	// tickAnimCmd() directly, not wrapped in tea.Batch — see compactCmds) is
	// a tea.Tick, i.e. a one-shot timer; calling it a second time would read
	// the already-drained timer channel and hang forever.
	got := cmd()
	if _, ok := got.(tea.BatchMsg); ok {
		t.Fatalf("AnimTick while in-flight must not fire an extra write command (must be just 1 cmd, not wrapped in Batch), got %#v", got)
	}
	if _, ok := got.(AnimTickMsg); !ok {
		t.Fatalf("AnimTick while in-flight leaves only tickAnimCmd — it must resolve to AnimTickMsg, got %#v", got)
	}
}

func TestUpdateEnterCallsSelectPaneOnCursorRow(t *testing.T) {
	fake := tmux.NewFake()
	restore := tmux.SetRunner(fake)
	defer restore()

	m := NewModel("%sidebar")
	m.rows = pane3Rows()
	m.cursor = 1 // pointing at %2

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter with a row under the cursor must emit a Cmd calling SelectPane")
	}
	cmd() // run selectPaneCmd

	found := false
	for _, call := range fake.Calls() {
		if len(call) == 3 && call[0] == "select-pane" && call[1] == "-t" && call[2] == "%2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("did not see the select-pane -t %%2 command in %v", fake.Calls())
	}
}

func TestUpdateCursorLoadedMsgAppliesValueFromTmux(t *testing.T) {
	m := NewModel("%sidebar")
	m.rows = pane3Rows()

	updated, _ := m.Update(cursorLoadedMsg{Value: 2})
	next := updated.(Model)

	if next.cursor != 2 {
		t.Fatalf("cursor after cursorLoadedMsg = %d, want 2", next.cursor)
	}
	if next.cursorDirty {
		t.Fatal("a value read FROM tmux must not be marked dirty (would write it right back to itself)")
	}
}

func TestUpdateCursorLoadedMsgClampsToCurrentList(t *testing.T) {
	m := NewModel("%sidebar")
	m.rows = pane3Rows()

	// The value read (99) exceeds the current list (3 rows) — it must still be clamped.
	updated, _ := m.Update(cursorLoadedMsg{Value: 99})
	next := updated.(Model)

	if next.cursor != 2 {
		t.Fatalf("cursor after cursorLoadedMsg exceeds the bound = %d, want 2 (clamped to the last row)", next.cursor)
	}
}

// ─── Repo picker popup: open/close, bounded navigation, selection ───────────────

func repoSessions() []tmux.SessionInfo {
	return []tmux.SessionInfo{
		{SessionName: "s", Windows: []tmux.WindowInfo{
			{Panes: []tmux.PaneInfo{{Agent: tmux.AgentClaude, Status: tmux.StatusRunning, Path: "/a/repo-a"}}},
			{Panes: []tmux.PaneInfo{{Agent: tmux.AgentCodex, Status: tmux.StatusRunning, Path: "/b/repo-b"}}},
		}},
	}
}

func TestUpdateKeyROpensRepoPopupAndEscCloses(t *testing.T) {
	m := NewModel("%sidebar")
	updated, _ := m.Update(RefreshMsg{Sessions: repoSessions(), Theme: DefaultTheme(), Icons: DefaultIcons()})
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = updated.(Model)
	if !m.popup.IsRepoOpen() {
		t.Fatal("the r key must open the repo popup")
	}

	// While the popup is OPEN, every key belongs to the popup
	// (handlePopupKey) — 'r' has NO meaning there, matching the reference
	// implementation (app/input.rs: the is_repo_popup_open() branch only
	// accepts Esc/j/k/Enter, not 'r'; 'r' is only defined to open the popup
	// in the plain-key branch below, which never runs once the popup is
	// already open). The popup must STAY OPEN after this second 'r' press.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = updated.(Model)
	if !m.popup.IsRepoOpen() {
		t.Fatal("the r key while the popup is open has no effect — the popup must stay open")
	}

	// Esc is the key that closes the popup.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.popup.IsRepoOpen() {
		t.Fatal("Esc must close the repo popup")
	}
}

func TestUpdatePopupNavigationClampsWithoutWrapping(t *testing.T) {
	// UNLIKE the status filter: j/k in the popup clamp at the bound, they do
	// NOT wrap around.
	m := NewModel("%sidebar")
	updated, _ := m.Update(RefreshMsg{Sessions: repoSessions(), Theme: DefaultTheme(), Icons: DefaultIcons()})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = updated.(Model)
	if m.popup.RepoSelected() != 0 {
		t.Fatalf("opening the popup for the first time (repoFilter=All) must select row 0, got %d", m.popup.RepoSelected())
	}

	// Popup list: ["All", "repo-a", "repo-b"] — 3 rows, the bottom bound is 2.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(Model)
	if m.popup.RepoSelected() != 2 {
		t.Fatalf("after 2 j presses from row 0 = %d, want 2 (last row)", m.popup.RepoSelected())
	}

	// One more j at the bottom bound — must NOT wrap back to 0.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(Model)
	if m.popup.RepoSelected() != 2 {
		t.Fatalf("j at the popup's bottom bound = %d, want 2 (clamped, not wrapped)", m.popup.RepoSelected())
	}

	// k back to 0 then clamped at the top bound.
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	m = updated.(Model)
	if m.popup.RepoSelected() != 0 {
		t.Fatalf("k at the popup's top bound = %d, want 0 (clamped, not wrapped)", m.popup.RepoSelected())
	}
}

func TestUpdatePopupSelectRepoFiltersListAndClosesPopup(t *testing.T) {
	m := NewModel("%sidebar")
	updated, _ := m.Update(RefreshMsg{Sessions: repoSessions(), Theme: DefaultTheme(), Icons: DefaultIcons()})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = updated.(Model)

	// Row 1 = "repo-a" (0="All", 1="repo-a", 2="repo-b").
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)

	if m.popup.IsRepoOpen() {
		t.Fatal("Enter must close the popup")
	}
	if m.repoFilter != "repo-a" {
		t.Fatalf("repoFilter after the selection = %q, want %q", m.repoFilter, "repo-a")
	}
	if !m.repoFilterDirty {
		t.Fatal("selecting a repo must mark repoFilterDirty so @sidebar_repo_filter gets written")
	}
	if paneRowCount(m.rows) != 1 {
		t.Fatalf("after filtering to repo-a: pane row count = %d, want 1", paneRowCount(m.rows))
	}
}

func TestUpdatePopupEscClosesWithoutChangingFilter(t *testing.T) {
	m := NewModel("%sidebar")
	updated, _ := m.Update(RefreshMsg{Sessions: repoSessions(), Theme: DefaultTheme(), Icons: DefaultIcons()})
	m = updated.(Model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)

	if m.popup.IsRepoOpen() {
		t.Fatal("Esc must close the popup")
	}
	if m.repoFilter != RepoFilterAll {
		t.Fatalf("repoFilter after Esc = %q, want %q (unchanged)", m.repoFilter, RepoFilterAll)
	}
}

func TestUpdatePopupReopenStillShowsAllReposWhileFiltered(t *testing.T) {
	// allRepoNames() must read from m.sessions (unfiltered), not from m.rows
	// (already filtered) — otherwise reopening the popup while filtered to
	// one repo would only show that repo, losing the ability to switch to
	// another repo or back to All.
	m := NewModel("%sidebar")
	updated, _ := m.Update(RefreshMsg{Sessions: repoSessions(), Theme: DefaultTheme(), Icons: DefaultIcons()})
	m = updated.(Model)
	m.repoFilter = "repo-a"
	m.applyRows()
	if paneRowCount(m.rows) != 1 {
		t.Fatalf("setup: pane row count after filtering to repo-a = %d, want 1", paneRowCount(m.rows))
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = updated.(Model)

	names := m.allRepoNames()
	if len(names) != 2 {
		t.Fatalf("allRepoNames() while filtered to repo-a = %v, want both repo-a and repo-b", names)
	}
}

// ─── New features: number jump, pin, filter, ahead/behind, manual subagent clear ───

func TestUpdateNumberKeyJumpsToNthPane(t *testing.T) {
	m := NewModel("%sidebar")
	m.rows = pane3Rows()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	next := updated.(Model)

	if next.cursor != 2 {
		t.Fatalf("cursor after pressing 3 = %d, want 2 (0-indexed 3rd pane)", next.cursor)
	}
}

func TestUpdateNumberKeyBeyondListDoesNothing(t *testing.T) {
	m := NewModel("%sidebar")
	m.rows = pane3Rows()
	m.cursor = 1

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("9")})
	next := updated.(Model)

	if next.cursor != 1 {
		t.Fatalf("cursor after pressing 9 with only 3 rows = %d, want unchanged 1", next.cursor)
	}
}

func TestUpdateKeyPTogglesPin(t *testing.T) {
	m := NewModel("%sidebar")
	updated, _ := m.Update(RefreshMsg{Sessions: repoSessions(), Theme: DefaultTheme(), Icons: DefaultIcons()})
	m = updated.(Model)
	m.cursor = 1 // second pane row (repo-b)

	paneID, ok := paneIDAt(m.rows, m.cursor)
	if !ok {
		t.Fatal("setup: expected a pane row at cursor 1")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = updated.(Model)
	if !m.pinnedPanes[paneID] {
		t.Fatal("pressing p must pin the pane under the cursor")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	m = updated.(Model)
	if m.pinnedPanes[paneID] {
		t.Fatal("pressing p again must unpin it")
	}
}

func TestBuildRowsFilterQueryNarrowsToMatchingPanes(t *testing.T) {
	got := buildRows(repoSessions(), RepoFilterAll, rowExtras{FilterQuery: "repo-b"})

	for _, r := range got {
		if !r.IsHeader && r.Base != "repo-b" {
			t.Fatalf("filter %q must exclude non-matching panes, got row %+v", "repo-b", r)
		}
	}
	found := false
	for _, r := range got {
		if r.Base == "repo-b" {
			found = true
		}
	}
	if !found {
		t.Fatalf("filter %q must keep the matching pane, got %+v", "repo-b", got)
	}
}

func TestUpdateSlashEntersFilterModeAndTypingNarrowsRows(t *testing.T) {
	m := NewModel("%sidebar")
	updated, _ := m.Update(RefreshMsg{Sessions: repoSessions(), Theme: DefaultTheme(), Icons: DefaultIcons()})
	m = updated.(Model)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = updated.(Model)
	if !m.filterEditing {
		t.Fatal("pressing / must enter filter-edit mode")
	}

	for _, r := range "repo-a" {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(Model)
	}
	if m.filterQuery != "repo-a" {
		t.Fatalf("filterQuery after typing = %q, want %q", m.filterQuery, "repo-a")
	}
	if paneRowCount(m.rows) != 1 {
		t.Fatalf("row count while filtered to repo-a = %d, want 1", paneRowCount(m.rows))
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.filterEditing || m.filterQuery != "" {
		t.Fatal("Esc while editing must exit filter mode and clear the query")
	}
	if paneRowCount(m.rows) != 2 {
		t.Fatalf("row count after clearing the filter = %d, want 2 (unfiltered)", paneRowCount(m.rows))
	}
}

func TestUpdateKeySClearsSubagentsForCursorRow(t *testing.T) {
	fake := tmux.NewFake()
	restore := tmux.SetRunner(fake)
	defer restore()

	m := NewModel("%sidebar")
	m.rows = pane3Rows()
	m.cursor = 0

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("S")})
	if cmd == nil {
		t.Fatal("pressing S must return a command")
	}
	cmd()

	cleared := false
	for _, call := range fake.Calls() {
		if len(call) >= 3 && call[0] == "set" && call[1] == "-t" && call[2] == "%1" {
			cleared = true
		}
	}
	if !cleared {
		t.Fatalf("pressing S must unset @pane_subagents on the pane under the cursor, calls: %v", fake.Calls())
	}
}

func TestAheadBehindLabelFormatsNonZeroCounts(t *testing.T) {
	cases := []struct {
		ab   *git.AheadBehind
		want string
	}{
		{nil, ""},
		{&git.AheadBehind{}, ""},
		{&git.AheadBehind{Ahead: 2}, "↑2"},
		{&git.AheadBehind{Behind: 3}, "↓3"},
		{&git.AheadBehind{Ahead: 2, Behind: 3}, "↑2 ↓3"},
	}
	for _, c := range cases {
		if got := aheadBehindLabel(c.ab); got != c.want {
			t.Errorf("aheadBehindLabel(%+v) = %q, want %q", c.ab, got, c.want)
		}
	}
}

// ─── Multi-select + bulk delete ────────────────────────────────────────────

func spawnedPane3Rows() []Row {
	return []Row{
		{PaneID: "%1", SidebarSpawned: true, Branch: "agent/one"},
		{PaneID: "%2", SidebarSpawned: true, Branch: "agent/two"},
		{PaneID: "%3", SidebarSpawned: false}, // not eligible for delete
	}
}

func TestUpdateKeyVTogglesSelection(t *testing.T) {
	m := NewModel("%sidebar")
	// "v" calls applyRows() (so the ✓ marker shows immediately) which
	// rebuilds m.rows from m.sessions — set both, not just m.rows directly,
	// or the rebuild would wipe the hand-built rows out from under the test.
	m.sessions = []tmux.SessionInfo{{Windows: []tmux.WindowInfo{{Panes: []tmux.PaneInfo{
		{PaneID: "%1", SidebarSpawned: true, Path: "/tmp/not-a-git-repo/x"},
	}}}}}
	m.applyRows()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m = updated.(Model)
	if !m.selectedPanes["%1"] {
		t.Fatal("pressing v must select the pane under the cursor")
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v")})
	m = updated.(Model)
	if m.selectedPanes["%1"] {
		t.Fatal("pressing v again must deselect it")
	}
}

func TestUpdateKeyXWithSelectionOpensBulkConfirmSkippingIneligibleRows(t *testing.T) {
	m := NewModel("%sidebar")
	m.rows = spawnedPane3Rows()
	m.selectedPanes = map[string]bool{"%1": true, "%2": true, "%3": true}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = updated.(Model)

	if !m.removeConfirmOpen {
		t.Fatal("x with a non-empty selection must open the confirm popup")
	}
	if len(m.removeConfirmPaneIDs) != 2 {
		t.Fatalf("removeConfirmPaneIDs = %v, want exactly the 2 SidebarSpawned rows (%%3 is not eligible)", m.removeConfirmPaneIDs)
	}
}

func TestUpdateRemoveConfirmEnterDeletesAllSelectedAndClearsSelection(t *testing.T) {
	m := NewModel("%sidebar")
	m.removeConfirmOpen = true
	m.removeConfirmPaneIDs = []string{"%1", "%2"}
	m.selectedPanes = map[string]bool{"%1": true, "%2": true}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter in bulk confirm mode must return a command")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("bulk confirm must batch one removeCmd per selected pane, got %#v", cmd())
	}
}

func TestUpdateRemoveConfirmEscClearsBulkSelection(t *testing.T) {
	m := NewModel("%sidebar")
	m.removeConfirmOpen = true
	m.removeConfirmPaneIDs = []string{"%1", "%2"}
	m.selectedPanes = map[string]bool{"%1": true, "%2": true}

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	next := updated.(Model)

	if next.removeConfirmOpen || len(next.removeConfirmPaneIDs) != 0 || len(next.selectedPanes) != 0 {
		t.Fatalf("esc must close the popup and clear bulk state, got %+v", next)
	}
}

// ─── Undo delete ("u" key) ──────────────────────────────────────────────

func TestUpdateKeyUWithNothingToUndoDoesNothing(t *testing.T) {
	m := NewModel("%sidebar")

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	if cmd != nil {
		t.Fatal("u with no lastRemoved must not return a command")
	}
}

func TestUpdateKeyUConsumesLastRemoved(t *testing.T) {
	m := NewModel("%sidebar")
	m.lastRemoved = &removedWorktree{RepoRoot: "/repo", Branch: "agent/do-task", Session: "s"}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	next := updated.(Model)

	if cmd == nil {
		t.Fatal("u with a lastRemoved must return a command")
	}
	if next.lastRemoved != nil {
		t.Fatal("u must consume (clear) lastRemoved — it's single-use")
	}
}

func TestRemoveResultMsgWithUndoStoresLastRemoved(t *testing.T) {
	m := NewModel("%sidebar")
	w := &removedWorktree{RepoRoot: "/repo", Branch: "agent/do-task", Session: "s"}

	updated, _ := m.Update(removeResultMsg{Success: true, Undo: w})
	next := updated.(Model)

	if next.lastRemoved != w {
		t.Fatalf("a successful removal with Undo set must store it as lastRemoved, got %+v", next.lastRemoved)
	}
}

func TestHandleMouseClickSelectsRowAndButtonExpands(t *testing.T) {
	m := NewModel("%sidebar")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 30, Height: 10})
	m = updated.(Model)
	m.rows = []Row{{PaneID: "%1"}, {IsInfoChild: true, InfoLabel: "x"}, {PaneID: "%2"}}

	// Rows start under the avatar (avatarHeight lines).
	updated, _ = m.Update(tea.MouseMsg{X: 5, Y: avatarHeight + 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if got := updated.(Model).cursor; got != 1 {
		t.Fatalf("click on the second pane row: cursor = %d, want 1", got)
	}
	updated, _ = updated.(Model).Update(tea.MouseMsg{X: 5, Y: avatarHeight + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if got := updated.(Model).cursor; got != 1 {
		t.Fatalf("click on an info line must not move the cursor, got %d", got)
	}
	if _, cmd := updated.(Model).Update(tea.MouseMsg{X: 1, Y: 9, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}); cmd == nil {
		t.Fatal("click on the expand button must return the expand command")
	}
}

func TestApprovalInboxRowsAndKeys(t *testing.T) {
	sessions := []tmux.SessionInfo{{Windows: []tmux.WindowInfo{{WindowID: "@1", Panes: []tmux.PaneInfo{
		{PaneID: "%7", Name: "api", Agent: tmux.AgentClaude, Status: tmux.StatusWaiting, WaitReason: "permission_prompt"},
	}}}}}
	dialogs := map[string]scrape.Dialog{"%7": {Question: "Do you want to proceed?", Options: []scrape.Option{{Key: "1", Label: "Yes"}, {Key: "2", Label: "No"}}}}
	rows := paneRowsOnly(buildRows(sessions, RepoFilterAll, rowExtras{Dialogs: dialogs}))
	if len(rows) != 4 || rows[1].InfoLabel != "Do you want to proceed?" || rows[2].InfoKey != "1" || rows[3].InfoLabel != "No" {
		t.Fatalf("rows = %+v", rows)
	}

	m := NewModel("%s")
	m.sessions, m.dialogs = sessions, dialogs
	m.applyRows()
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	if cmd == nil {
		t.Fatal("pressing an option key on a waiting agent must answer it")
	}
}

// Another window's sidebar wrote @sidebar_cursor; once this sidebar follows
// the pane focused in its own window, the shared value must not move it.
func TestCursorLoadedDoesNotOverrideFocus(t *testing.T) {
	m := NewModel("%s")
	m.windowID = "@2"
	sessions := []tmux.SessionInfo{{Windows: []tmux.WindowInfo{
		{WindowID: "@2", Panes: []tmux.PaneInfo{{PaneID: "%9", Agent: tmux.AgentClaude, PaneActive: true, Path: "/a/x"}}},
		{WindowID: "@4", Panes: []tmux.PaneInfo{{PaneID: "%94", Agent: tmux.AgentClaude, Path: "/a/y"}}},
	}}}
	updated, _ := m.Update(RefreshMsg{Sessions: sessions, Theme: DefaultTheme(), Icons: DefaultIcons()})
	updated, _ = updated.(Model).Update(cursorLoadedMsg{Value: 1})
	if id, _ := paneIDAt(updated.(Model).rows, updated.(Model).cursor); id != "%9" {
		t.Fatalf("cursor on %s, want the focused pane %%9", id)
	}
}

// A pane added above the selected one must not move the highlight.
func TestCursorStaysOnPaneWhenRowsChange(t *testing.T) {
	win := func(id string, panes ...string) tmux.WindowInfo {
		w := tmux.WindowInfo{WindowID: id}
		for _, p := range panes {
			w.Panes = append(w.Panes, tmux.PaneInfo{PaneID: p, Status: tmux.StatusIdle})
		}
		return w
	}
	m := Model{repoFilter: RepoFilterAll}
	m.sessions = []tmux.SessionInfo{{Windows: []tmux.WindowInfo{win("@1", "%1"), win("@2", "%2")}}}
	m.applyRows()
	m.cursor = 1 // %2
	m.sessions = []tmux.SessionInfo{{Windows: []tmux.WindowInfo{win("@0", "%9"), win("@1", "%1"), win("@2", "%2")}}}
	m.applyRows()
	if id, _ := paneIDAt(m.rows, m.cursor); id != "%2" {
		t.Fatalf("cursor on %s after a pane was added above, want %%2", id)
	}
}
