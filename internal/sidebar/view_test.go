package sidebar

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"tmux-agent/internal/sharedctx"
	"tmux-agent/internal/tmux"
)

// rowBackgroundNeverDrops scans a rendered (Render()'d) string containing ANSI
// codes, tracking whether the background is ON or OFF via the SGR codes
// "48"(on)/"49"/"0"(off), and reports false if it finds a VISIBLE character
// while the background is OFF — a regression test for a bug confirmed on
// real tmux: each child Render() closes with its own reset code, so wrapping
// the background just ONCE around the concatenated string (the old approach)
// gets its middle wiped out by an in-between child Render()'s reset code —
// the only safe way is to turn the background on SEPARATELY in each child
// Render() (view.go: current renderPaneText/renderRows).
func rowBackgroundNeverDrops(s string) bool {
	bgOn := false
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\x1b' && i+1 < len(runes) && runes[i+1] == '[' {
			j := i + 2
			for j < len(runes) && runes[j] != 'm' {
				j++
			}
			for _, part := range strings.Split(string(runes[i+2:j]), ";") {
				switch part {
				case "48":
					bgOn = true
				case "49", "0", "":
					bgOn = false
				}
			}
			i = j
			continue
		}
		if !bgOn {
			return false
		}
	}
	return true
}

func TestRenderRowsEmptyListShowsHint(t *testing.T) {
	got := renderRows(nil, 40, -1, DefaultTheme(), DefaultIcons())
	if got != emptyHint {
		t.Fatalf("renderRows(nil, ...) = %q, want %q", got, emptyHint)
	}
}

// Every row is exactly one line — window headers included, with no blank
// spacer — so a mouse click's Y maps straight to a row (handleMouse).
func TestRenderRowsEachRowOneLine(t *testing.T) {
	rows := []Row{
		{IsHeader: true, Header: "1 work"},
		{Status: tmux.StatusRunning, Agent: tmux.AgentClaude, Base: "tmux-agent"},
		{IsHeader: true, Header: "2 api", HeaderFocus: true},
		{Status: tmux.StatusIdle, Agent: tmux.AgentClaude, Base: "api"},
	}
	got := renderRows(rows, 40, -1, DefaultTheme(), DefaultIcons())
	lines := strings.Split(got, "\n")
	if len(lines) != 4 {
		t.Fatalf("line count = %d, want 4, content: %q", len(lines), got)
	}
	if lines[0] != "▸ 1 work" || lines[2] != "▾ 2 api" {
		t.Fatalf("headers = %q / %q, want \"▸ 1 work\" / \"▾ 2 api\" (▾ = the sidebar's window)", lines[0], lines[2])
	}
	if !strings.Contains(lines[1], "tmux-agent") {
		t.Fatalf("pane line = %q, want it to contain the basename", lines[1])
	}
}

func TestRenderRowsTruncatesToWidth(t *testing.T) {
	rows := []Row{
		{Status: tmux.StatusRunning, Agent: tmux.AgentClaude, Base: strings.Repeat("x", 100)},
	}

	got := renderRows(rows, 10, -1, DefaultTheme(), DefaultIcons())
	for _, line := range strings.Split(got, "\n") {
		if len([]rune(line)) > 10 {
			t.Fatalf("line is %d characters long, exceeds width=10: %q", len([]rune(line)), line)
		}
	}
}

func TestRenderRowsDrawsCursorMarkerOnCorrectRow(t *testing.T) {
	rows := []Row{
		{Status: tmux.StatusRunning, Agent: tmux.AgentClaude, Base: "a"},
		{Status: tmux.StatusRunning, Agent: tmux.AgentClaude, Base: "b"},
	}

	got := renderRows(rows, 40, 1, DefaultTheme(), DefaultIcons())
	lines := strings.Split(got, "\n")
	if !strings.HasPrefix(lines[0], noCursorMarker) {
		t.Fatalf("the non-pointed-to row must start with the status border marker %q, got %q", noCursorMarker, lines[0])
	}
	if !strings.HasPrefix(lines[1], cursorMarker) {
		t.Fatalf("the pointed-to row must start with %q, got %q", cursorMarker, lines[1])
	}
}

func TestRenderRowsCursorMarkerDoesNotShiftOtherRows(t *testing.T) {
	rows := []Row{
		{Status: tmux.StatusRunning, Agent: tmux.AgentClaude, Base: "a"},
		{Status: tmux.StatusRunning, Agent: tmux.AgentClaude, Base: "b"},
	}

	withCursor := strings.Split(renderRows(rows, 40, 0, DefaultTheme(), DefaultIcons()), "\n")
	withoutCursor := strings.Split(renderRows(rows, 40, -1, DefaultTheme(), DefaultIcons()), "\n")

	// Both markers (cursorMarker/noCursorMarker) are 2 characters long — the
	// rest of the line (icon/agent/base) must start at the same position
	// regardless of which row is pointed to. Row 0 has no background fill
	// (only the CURRENTLY pointed-to row does), so compare row 1 (not
	// pointed to in either case).
	if withCursor[1][2:] != withoutCursor[1][2:] {
		t.Fatalf("content after the cursor marker on the non-pointed-to row is misaligned: %q vs %q", withCursor[1], withoutCursor[1])
	}
}

func TestRenderRowsCursorOutOfRangeNoRowMarked(t *testing.T) {
	rows := []Row{
		{Status: tmux.StatusRunning, Agent: tmux.AgentClaude, Base: "a"},
	}

	got := renderRows(rows, 40, 99, DefaultTheme(), DefaultIcons())
	if !strings.HasPrefix(got, noCursorMarker) {
		t.Fatalf("with the cursor out of the valid range, no row should be marked, got %q", got)
	}
}

// ─── Debt 3a Minor 4: the "working" (running) icon and the empty/undetermined
// status no longer share tmux.PaneStatus.Icon()'s hardcoded `·` glyph ───────

// Real user feedback: "fix the selected indicator" — verified directly with
// capture-pane -e on real tmux, showing the selected row's background bar
// turning OFF partway through, right after the icon (the reset code of a
// child Render() wiping out the outer wrapping style's background, see
// rowBackgroundNeverDrops). Force lipgloss to EMIT ANSI codes (go test is not
// a TTY by default, so lipgloss disables color entirely) to reproduce the
// exact bytes seen on real tmux.
func TestRenderPaneText_SelectedBackgroundNotBrokenMidRow(t *testing.T) {
	orig := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(orig)

	row := Row{Status: tmux.StatusRunning, Agent: tmux.AgentClaude, Base: "mem-console", Ports: []int{3000}}
	got := renderPaneText(row, 40, DefaultTheme(), DefaultIcons(), true, 0)

	if !strings.Contains(got, "\x1b[") {
		t.Fatalf("with ColorProfile ANSI256, ANSI codes must be emitted, but no escape was found in %q", got)
	}
	if !rowBackgroundNeverDrops(got) {
		t.Fatalf("the selected row's background is BROKEN partway through: %q", got)
	}
}

// ─── truncateEllipsis (part 6, learned from herdr — reference report section 9.1) ──

func TestTruncateEllipsis_ExactFitNoTruncation(t *testing.T) {
	if got := truncateEllipsis("hello", 5); got != "hello" {
		t.Fatalf("got %q, want %q (exact fit, no truncation needed)", got, "hello")
	}
}

func TestTruncateEllipsis_TruncatesAndAddsEllipsis(t *testing.T) {
	got := truncateEllipsis("hello world", 8)
	if got != "hello w…" {
		t.Fatalf("got %q, want %q", got, "hello w…")
	}
	if n := len([]rune(got)); n != 8 {
		t.Fatalf("result width = %d, want exactly 8", n)
	}
}

func TestTruncateEllipsis_MaxOneLeavesOnlyEllipsis(t *testing.T) {
	if got := truncateEllipsis("hello", 1); got != "…" {
		t.Fatalf("got %q, want %q", got, "…")
	}
}

func TestTruncateEllipsis_MaxZeroReturnsEmpty(t *testing.T) {
	if got := truncateEllipsis("hello", 0); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

// ─── renderPaneText: truncation budget (part 6) ────────────────────────────
//
// Learned from herdr (reference report section 9.1-9.2): a fixed token
// (icon) is never truncated; the lowest-priority elastic token (ports) is
// DROPPED ENTIRELY first when space is short; the "name" only gets truncated
// with "…" if dropping ports still isn't enough.

// Name and branch share one line; when they don't fit, the line scrolls
// (marquee) instead of being cut, and keeps the exact budget width.
func TestRenderPaneText_NameAndBranchScrollWhenTooLong(t *testing.T) {
	row := Row{Status: tmux.StatusRunning, Agent: tmux.AgentClaude, Base: "agent-with-a-long-name", GitBranch: "feature/very-long-branch"}
	first := ansi.Strip(renderPaneText(row, 20, DefaultTheme(), DefaultIcons(), false, 0))
	later := ansi.Strip(renderPaneText(row, 20, DefaultTheme(), DefaultIcons(), false, marqueePause+5))
	if strings.Contains(first, "…") || !strings.HasPrefix(first, "agent-with") || first == later {
		t.Fatalf("want a scrolling line, got %q then %q", first, later)
	}
	for _, s := range []string{first, later} {
		if w := lipgloss.Width(s); w != 20 {
			t.Fatalf("width %d, want the budget 20: %q", w, s)
		}
	}
	short := ansi.Strip(renderPaneText(Row{Base: "api", GitBranch: "main"}, 20, DefaultTheme(), DefaultIcons(), false, 0))
	if !strings.HasPrefix(short, "api") || !strings.HasSuffix(short, "main") || lipgloss.Width(short) != 20 {
		t.Fatalf("name left, branch right-aligned, on one line: %q", short)
	}
}

// ─── "uncommitted changes" dot (Row.Dirty) + elapsed time ────────────

// ─── iconForRow/colorForRow: the NeedsConfirm/JustDone overlay (part 6) ───────

// ─── treeBranch / subagentRows (part 6, real user feedback: the subagent
// tree shows up in the root list, no need to press "i") ─────────────────────────

func TestTreeBranch_LastElementUsesClosingBranchOthersUseTee(t *testing.T) {
	if got := treeBranch(0, 3); got != "├─ " {
		t.Fatalf("treeBranch(0,3) = %q, want %q", got, "├─ ")
	}
	if got := treeBranch(1, 3); got != "├─ " {
		t.Fatalf("treeBranch(1,3) = %q, want %q", got, "├─ ")
	}
	if got := treeBranch(2, 3); got != "└─ " {
		t.Fatalf("treeBranch(2,3) = %q, want %q", got, "└─ ")
	}
	// Only one element: it's both first and last — it must still use the closing tee.
	if got := treeBranch(0, 1); got != "└─ " {
		t.Fatalf("treeBranch(0,1) = %q, want %q", got, "└─ ")
	}
}

func TestSubagentRows_EmptyProducesNoRows(t *testing.T) {
	if got := subagentRows(nil); got != nil {
		t.Fatalf("subagentRows(nil) = %+v, want nil", got)
	}
}

func TestSubagentRows_UsesBranchAndMarksIsSubagentChild(t *testing.T) {
	got := subagentRows([]string{"a", "b"})
	if len(got) != 2 {
		t.Fatalf("row count = %d, want 2", len(got))
	}
	for _, r := range got {
		if !r.IsSubagentChild {
			t.Fatalf("a subagent row must have IsSubagentChild=true, got %+v", r)
		}
	}
	if got[0].SubagentLabel != "├─ a" {
		t.Fatalf("SubagentLabel[0] = %q, want %q", got[0].SubagentLabel, "├─ a")
	}
	if got[1].SubagentLabel != "└─ b" {
		t.Fatalf("SubagentLabel[1] = %q, want %q", got[1].SubagentLabel, "└─ b")
	}
}

// renderRows must draw the subagent branch indented deeper than the parent
// pane row, and must NOT count it toward paneIdx (pressing j/k must skip
// over it, not stop at the subagent branch).
func TestRenderRowsIndentsSubagentBranchAndExcludesFromCursor(t *testing.T) {
	rows := []Row{
		{IsHeader: true, Header: "work"},
		{Status: tmux.StatusRunning, Agent: tmux.AgentClaude, Base: "tmux-agent"},
		{IsSubagentChild: true, SubagentLabel: "└─ Explore"},
		{Status: tmux.StatusIdle, Agent: tmux.AgentClaude, Base: "other"},
	}
	got := renderRows(rows, 40, 1, DefaultTheme(), DefaultIcons())
	lines := strings.Split(got, "\n")
	if len(lines) != 4 {
		t.Fatalf("line count = %d, want 4, content: %q", len(lines), got)
	}
	if !strings.HasPrefix(lines[2], nestedIndent+"└─ Explore") {
		t.Fatalf("subagent branch line = %q, want it to start with %q", lines[2], nestedIndent+"└─ Explore")
	}
	// cursor=1 must point to the SECOND pane row ("other", cursor index 1
	// after skipping the header + subagent branch), not be thrown off by the
	// subagent branch sitting in between.
	if !strings.HasPrefix(lines[3], cursorMarker) {
		t.Fatalf("the second pane row (after skipping the subagent branch) must have cursorMarker, got %q", lines[3])
	}
}

// renderRows must draw the "what it's doing" info line indented to the same
// level as the subagent branch, and it must ALSO NOT count toward the
// cursor (exactly like TestRenderRowsIndentsSubagentBranchAndExcludesFromCursor
// above, but for IsInfoChild instead of IsSubagentChild).
func TestRenderRowsIndentsInfoLineAndExcludesFromCursor(t *testing.T) {
	rows := []Row{
		{Status: tmux.StatusRunning, Agent: tmux.AgentClaude, Base: "tmux-agent"},
		{IsInfoChild: true, InfoLabel: "refactor view.go"},
		{Status: tmux.StatusIdle, Agent: tmux.AgentClaude, Base: "other"},
	}
	got := renderRows(rows, 40, 1, DefaultTheme(), DefaultIcons())
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("line count = %d, want 3, content: %q", len(lines), got)
	}
	if !strings.HasPrefix(lines[1], nestedIndent+infoArrow+"refactor view.go") {
		t.Fatalf("info line = %q, want it to start with %q", lines[1], nestedIndent+infoArrow+"refactor view.go")
	}
	// cursor=1 must point to the second pane row, skipping the info line in between.
	if !strings.HasPrefix(lines[2], cursorMarker) {
		t.Fatalf("the second pane row (after skipping the info line) must have cursorMarker, got %q", lines[2])
	}
}

func TestRenderRowsScrollsLongInfoLineWithinWidth(t *testing.T) {
	rows := []Row{
		{Status: tmux.StatusRunning, Agent: tmux.AgentClaude, Base: "tmux-agent"},
		{IsInfoChild: true, InfoLabel: strings.Repeat("x", 100)},
	}
	got := renderRows(rows, 20, -1, DefaultTheme(), DefaultIcons())
	lines := strings.Split(got, "\n")
	if n := len([]rune(lines[1])); n > 20 {
		t.Fatalf("info line is %d characters long, exceeds width=20: %q", n, lines[1])
	}
}

// ─── Repo picker popup ─────────────────────────────────────────────────────────

func TestRenderRepoPopupAllIsAlwaysFirst(t *testing.T) {
	got := renderRepoPopup(RepoNames([]string{"repo-a", "repo-b"}), 0, 40, DefaultTheme())
	lines := strings.Split(got, "\n")
	// Line 0 is the title; line 1 is "All", followed by each repo.
	if len(lines) != 4 {
		t.Fatalf("popup line count = %d, want 4 (title + All + 2 repos), content: %q", len(lines), got)
	}
	if !strings.Contains(lines[1], "All") {
		t.Fatalf("the first line of the popup list must contain \"All\", got %q", lines[1])
	}
	if !strings.Contains(lines[2], "repo-a") || !strings.Contains(lines[3], "repo-b") {
		t.Fatalf("the popup must list all repo names, got %q", got)
	}
}

func TestRenderRepoPopupEmptyListStillHasAll(t *testing.T) {
	got := renderRepoPopup(RepoNames(nil), 0, 40, DefaultTheme())
	if !strings.Contains(got, "All") {
		t.Fatalf("the popup with an empty repo list must still have the All line, got %q", got)
	}
}

func TestContextLinesFitFreeSpace(t *testing.T) {
	m := NewModel("%s")
	m.viewport.Height = 8
	m.rows = []Row{{IsHeader: true, Header: "1 w"}, {PaneID: "%1"}}
	m.notesScope = "calc-1a2b3c4d"
	if got := m.contextLines(30); got != nil {
		t.Fatalf("an empty board must be hidden, got %q", got)
	}
	for i := 0; i < 6; i++ {
		m.notes = append(m.notes, sharedctx.Entry{Author: "a", Text: fmt.Sprintf("note %d", i)})
	}
	got := m.contextLines(30) // free = 6: spacer, divider, 3 notes, "+3 earlier"
	if len(got) != 6 || !strings.Contains(got[1], "board · calc") || !strings.Contains(got[4], "note 5") || !strings.Contains(got[5], "+3 earlier") {
		t.Fatalf("overflowing board = %q", got)
	}
	m.viewport.Height = 30
	got = m.contextLines(30)
	if len(got) != 8 || strings.Contains(strings.Join(got, " "), "note 0") || !strings.Contains(got[7], "+1 earlier") {
		t.Fatalf("board should show five newest notes: %q", got)
	}
	m.viewport.Height = 8
	m.rows = append(m.rows, make([]Row, 5)...)
	if got := m.contextLines(30); got != nil {
		t.Fatalf("no room must hide the section, got %q", got)
	}
}

func TestHorizontalSidebarShowsThreeColumns(t *testing.T) {
	m := NewModel("%sidebar")
	m.width, m.height = 120, 12
	m.rows = []Row{{PaneID: "%1", Base: "agent-one"}, {PaneID: "%2", Base: "agent-two"}}
	m.notes = []sharedctx.Entry{{Author: "worker", Text: "board-message", Scope: sharedctx.Global}}
	m.notesScope = "project-1234"
	m.resizeViewport()
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	if len(lines) != m.height || m.viewport.Width != 40 || m.viewport.Height != 11 {
		t.Fatalf("horizontal size: lines=%d viewport=%dx%d", len(lines), m.viewport.Width, m.viewport.Height)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "agent-one") || !strings.Contains(joined, "board-message") {
		t.Fatalf("missing agent or board: %s", joined)
	}
	for _, line := range lines[:len(lines)-1] {
		if lipgloss.Width(line) != 120 {
			t.Fatalf("line has width %d: %q", lipgloss.Width(line), line)
		}
	}
	updated, _ := m.handleMouse(tea.MouseMsg{X: 45, Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if updated.(Model).cursor != 1 {
		t.Fatal("click in agent column should select second pane")
	}
	updated, _ = m.handleMouse(tea.MouseMsg{X: 100, Y: 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if updated.(Model).cursor != 0 {
		t.Fatal("click in board must not select an agent")
	}
}

func TestBoardShowsOnlyScopeAndText(t *testing.T) {
	m := NewModel("%sidebar")
	m.notes = []sharedctx.Entry{
		{Author: "%78", Pane: "%78", Scope: sharedctx.Global, Text: "global note"},
		{Author: "agent", Text: "project note"},
	}
	got := ansi.Strip(strings.Join(m.boardLines(40, 10), "\n"))
	if !strings.Contains(got, "[G] global note") || !strings.Contains(got, "project note") ||
		strings.Contains(got, "%78") || strings.Contains(got, "agent") {
		t.Fatalf("board = %q", got)
	}
}

// Name first, then only a clock (working) or a check (done, unseen).
func TestRenderPaneText_NameThenStatusMarker(t *testing.T) {
	icons := DefaultIcons()
	cases := []struct {
		row  Row
		want string
	}{
		{Row{Status: tmux.StatusRunning, Base: "lead"}, icons.Clock},
		{Row{Status: tmux.StatusIdle, JustDone: true, Base: "lead"}, icons.Done},
		{Row{Status: tmux.StatusIdle, Base: "lead"}, ""},
		{Row{Status: tmux.StatusWaiting, Base: "lead"}, ""},
	}
	for _, c := range cases {
		got := strings.TrimRight(renderPaneText(c.row, 20, DefaultTheme(), icons, false, 0), " ")
		if lipgloss.Width(renderPaneText(c.row, 20, DefaultTheme(), icons, false, 0)) != 20 {
			t.Fatalf("row must be exactly the budget wide: %q", got)
		}
		if !strings.HasPrefix(got, "lead") || !strings.HasSuffix(got, "lead"+strings.Repeat(" ", max(0, 20-4-lipgloss.Width(c.want)))+c.want) && c.want != "" {
			t.Fatalf("%s: got %q, want name then %q at the right edge", c.row.Status, got, c.want)
		}
		if c.want == "" && got != "lead" {
			t.Fatalf("%s: got %q, want the bare name", c.row.Status, got)
		}
	}
}

func TestMarquee(t *testing.T) {
	if got := marquee("short", 10, 99); got != "short" {
		t.Errorf("fitting text changed: %q", got)
	}
	s := "abcdefghijklmnop"
	if got := marquee(s, 5, 0); got != "abcde" {
		t.Errorf("step 0 = %q, want start", got)
	}
	if got := marquee(s, 5, marqueePause+2); got != "cdefg" {
		t.Errorf("after pause = %q, want scrolled by 2", got)
	}
	if got := marquee(s, 5, marqueePause+len(s)+7); got != "abcde" {
		t.Errorf("full loop = %q, want back at start", got)
	}
}
