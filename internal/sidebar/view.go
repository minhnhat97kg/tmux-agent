package sidebar

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"tmux-agent/internal/sharedctx"
	"tmux-agent/internal/tmux"
)

// emptyHint is shown when no agent pane is running anywhere on the tmux
// server. The text the user sees is in English (comments/names stay in
// Vietnamese) — this is a public plugin, not just for Vietnamese users.
const emptyHint = "(no agents running)"

// fallbackWidth is used before tea.WindowSizeMsg arrives (the first frame).
const fallbackWidth = 30

// cursorMarker/noCursorMarker are both 2 characters long — changing the
// cursor marker must not shift the column of the other rows.
const (
	cursorMarker   = "▌ "
	noCursorMarker = "  "
)

// repoPopupTitle is the name shown on the title line of the repo-picker
// popup — drawn with renderHeaderText (the same style as the repo group
// header in the list, for visual consistency — previously the popup used a
// plain line of text that looked completely different from the rest of the
// style).
const repoPopupTitle = "Select repo"

// ─── Pane list: one line per agent (part 6) ─────────────────────────

// Each pane gets ONE line in the list, optionally followed by an action /
// "what it's doing" line and its subagent tree.

// renderRows builds the text content of the list: one line per row.
func renderRows(rows []Row, width, cursor int, theme Theme, icons Icons) string {
	return renderRowsAt(rows, width, cursor, theme, icons, 0)
}

// renderRowsAt is renderRows at marquee step `step`: message lines too long
// for the sidebar scroll instead of being cut off.
func renderRowsAt(rows []Row, width, cursor int, theme Theme, icons Icons, step int) string {
	if width <= 0 {
		width = fallbackWidth
	}
	line := lipgloss.NewStyle().MaxWidth(width)

	if len(rows) == 0 {
		return line.Render(emptyHint)
	}

	var b strings.Builder
	paneIdx := 0
	for i, row := range rows {
		if i > 0 {
			b.WriteString("\n")
		}

		if row.IsHeader {
			// Window header — exactly one line per row (no blank spacer), so
			// a mouse click's Y maps straight to a row (handleMouse).
			b.WriteString(renderWindowHeader(row, width, theme))
			continue
		}

		if row.IsInfoChild && row.InfoKey != "" {
			key := lipgloss.NewStyle().Background(theme.StatusConfirm).Foreground(lipgloss.Color("0")).Bold(true).Render(" " + row.InfoKey + " ")
			label := marquee(row.InfoLabel, width-lipgloss.Width(nestedIndent)-4, step)
			b.WriteString(line.Render(nestedIndent + key + " " + lipgloss.NewStyle().Foreground(theme.TextPrimary).Render(label)))
			continue
		}
		if row.IsInfoChild {
			arrow, labelStyle := infoArrow, lipgloss.NewStyle().Foreground(theme.TextMuted).Italic(true)
			if row.InfoAction {
				arrow, labelStyle = actionArrow, lipgloss.NewStyle().Foreground(theme.StatusConfirm).Bold(true)
			}
			label := marquee(row.InfoLabel, width-lipgloss.Width(nestedIndent+arrow), step)
			styled := lipgloss.NewStyle().Foreground(theme.TextDim).Render(nestedIndent) +
				labelStyle.Render(arrow+label)
			b.WriteString(line.Render(styled))
			continue
		}

		if row.IsSubagentChild {
			styled := lipgloss.NewStyle().Foreground(theme.TextDim).Render(nestedIndent + row.SubagentLabel)
			b.WriteString(line.Render(styled))
			continue
		}

		marker := noCursorMarker
		selected := paneIdx == cursor
		markerStyle := lipgloss.NewStyle()
		if selected {
			marker = cursorMarker
			markerStyle = markerStyle.Background(theme.SelectionBg).Foreground(theme.Accent)
		}
		text := markerStyle.Render(marker) + renderPaneText(row, width-lipgloss.Width(marker), theme, icons, selected, step)
		b.WriteString(line.Render(text))
		paneIdx++
	}
	return b.String()
}

// horizontalView gives the avatar, agent list and board their own columns.
func (m Model) horizontalView(width int) string {
	height := max(0, m.height-footerHeight)
	leftWidth, middleWidth := width/3, width/3
	rightWidth := width - leftWidth - middleWidth
	var avatar []string
	if m.avatar.enabled() {
		avatar = m.renderAvatar(leftWidth)
	}
	list := strings.Split(m.viewport.View(), "\n")
	board := m.boardLines(rightWidth, height)
	column := func(lines []string, row, w int) string {
		line := ""
		if row < len(lines) {
			line = lines[row]
		}
		return lipgloss.NewStyle().Width(w).MaxWidth(w).Render(line)
	}
	lines := make([]string, 0, height+1)
	for i := 0; i < height; i++ {
		lines = append(lines, column(avatar, i, leftWidth)+column(list, i, middleWidth)+column(board, i, rightWidth))
	}
	return strings.Join(append(lines, renderKeyHints(m, width)), "\n")
}

// renderHeaderText draws a bold title line — the repo-picker popup title.
// (The pane list itself no longer has group headers: it's a flat agent list.)
func renderHeaderText(row Row, width int, theme Theme) string {
	if width <= 0 {
		width = fallbackWidth
	}
	style := lipgloss.NewStyle().Foreground(theme.TextPrimary).Bold(true)
	if row.HeaderFocus {
		style = style.Foreground(theme.Accent)
	}
	return style.Render(truncateEllipsis(row.Header, width))
}

// renderWindowHeader draws a window header in the list: "▾ 3 window" in the
// accent color for the sidebar's own window, muted bold otherwise — bold so
// it stands apart from the branch lines under each name.
func renderWindowHeader(row Row, width int, theme Theme) string {
	style := lipgloss.NewStyle().Foreground(theme.TextMuted).Bold(true)
	mark := "▸ "
	if row.HeaderFocus {
		style = lipgloss.NewStyle().Foreground(theme.Accent).Bold(true)
		mark = "▾ "
	}
	return style.Render(truncateEllipsis(mark+row.Header, width))
}

// renderPaneText builds one pane row, exactly budget columns wide:
//
//	name              branch ⏱
//
// The name (yellow when the agent waits on you, red on error) on the left,
// its git branch right-aligned; when both don't fit the line scrolls
// (marquee, at step) instead of being cut. Then
// one trailing marker: a clock while it works, a check once it finished and
// nobody has looked yet (@pane_unseen) — nothing otherwise.
//
// selected pushes Background(SelectionBg) into EACH sub-style (and the
// padding) rather than wrapping the joined string once: every sub-Render
// ends with its own SGR reset, which would also cut off an outer background
// halfway along the row (verified with capture-pane -e on a real tmux).
func renderPaneText(row Row, budget int, theme Theme, icons Icons, selected bool, step int) string {
	sel := func(s lipgloss.Style) lipgloss.Style {
		if selected {
			return s.Background(theme.SelectionBg)
		}
		return s
	}
	icon, iconColor := "", theme.TextDim
	switch {
	case row.Status == tmux.StatusRunning || row.Status == tmux.StatusBackground:
		icon, iconColor = icons.Clock, theme.StatusRunning
	case row.Status == tmux.StatusIdle && row.JustDone:
		icon, iconColor = icons.Done, theme.StatusDone
	}
	right := 0
	if icon != "" {
		right = 1 + lipgloss.Width(icon)
	}

	prefix := ""
	if row.Selected {
		prefix += "✓ "
	}
	if row.Pinned {
		prefix += "★ "
	}
	// Emoji go: the marquee steps rune by rune and would split them.
	name := stripEmoji(prefix + row.Base)
	plain := name
	if row.GitBranch != "" {
		plain += "  " + icons.GitBranch + " " + stripEmoji(row.GitBranch)
	}

	nameStyle := lipgloss.NewStyle().Foreground(theme.AgentColor(row.Agent))
	if row.Agent == "" || row.Agent == tmux.AgentUnknown {
		nameStyle = nameStyle.Foreground(theme.TextPrimary)
	}
	switch row.Status {
	case tmux.StatusWaiting:
		nameStyle = nameStyle.Foreground(theme.StatusConfirm)
	case tmux.StatusError:
		nameStyle = nameStyle.Foreground(theme.StatusError)
	}
	if selected {
		nameStyle = nameStyle.Bold(true)
	}
	branchStyle := sel(lipgloss.NewStyle().Foreground(theme.Branch))
	nameStyle = sel(nameStyle)

	room := max(0, budget-right)
	space := sel(lipgloss.NewStyle())
	if row.GitBranch != "" && lipgloss.Width(plain) <= room {
		branch := icons.GitBranch + " " + stripEmoji(row.GitBranch)
		gap := room - lipgloss.Width(name) - lipgloss.Width(branch)
		out := nameStyle.Render(name) + space.Render(strings.Repeat(" ", gap)) + branchStyle.Render(branch)
		if icon != "" {
			out += space.Render(" ") + sel(lipgloss.NewStyle().Foreground(iconColor)).Render(icon)
		}
		return out
	}
	runes := []rune(plain)
	var idx []int
	if lipgloss.Width(plain) <= room {
		for i := range runes {
			idx = append(idx, i)
		}
	} else {
		runes = marqueeRunes(plain)
		idx = marqueeWindow(runes, room, step)
	}
	nameLen := len([]rune(name))
	var out strings.Builder
	shown := 0
	for k := 0; k < len(idx); {
		j := k
		for j < len(idx) && (idx[j] < nameLen) == (idx[k] < nameLen) {
			j++
		}
		var run []rune
		for _, i := range idx[k:j] {
			run = append(run, runes[i])
		}
		st := branchStyle
		if idx[k] < nameLen {
			st = nameStyle
		}
		out.WriteString(st.Render(string(run)))
		shown += lipgloss.Width(string(run))
		k = j
	}
	if gap := budget - shown - right; gap > 0 {
		out.WriteString(space.Render(strings.Repeat(" ", gap)))
	}
	if icon != "" {
		out.WriteString(space.Render(" ") + sel(lipgloss.NewStyle().Foreground(iconColor)).Render(icon))
	}
	return out.String()
}

// truncateEllipsis truncates s to at most max display columns
// (lipgloss.Width, which respects wide characters like CJK), appending "…"
// (1 column) at the end if truncated. s must be RAW text, not yet passed
// through Style.Render — ANSI escapes would make lipgloss.Width count
// wrong, so call this function BEFORE coloring.
func truncateEllipsis(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	var b strings.Builder
	for _, r := range s {
		if lipgloss.Width(b.String()+string(r)) > max-1 {
			break
		}
		b.WriteRune(r)
	}
	return b.String() + "…"
}

// marqueePause is how many steps a scrolling line rests at its start.
const marqueePause = 6

// marquee shows s in max columns: as is when it fits, otherwise a window
// that scrolls one rune per step through s and loops, resting at the start
// so the beginning can be read.
func marquee(s string, max, step int) string {
	if max <= 0 || lipgloss.Width(s) <= max {
		return truncateEllipsis(s, max)
	}
	runes := marqueeRunes(s)
	var b strings.Builder
	for _, i := range marqueeWindow(runes, max, step) {
		b.WriteRune(runes[i])
	}
	return b.String()
}

const marqueeGap = "   ·   "

// marqueeRunes is the loop marquee scrolls through: s, then a gap.
func marqueeRunes(s string) []rune { return []rune(s + marqueeGap) }

// marqueeWindow returns the indices into runes visible at step, filling at
// most max columns.
func marqueeWindow(runes []rune, max, step int) []int {
	pos := step%(len(runes)+marqueePause) - marqueePause
	if pos < 0 {
		pos = 0
	}
	var idx []int
	for i, w := 0, 0; i < len(runes); i++ {
		j := (pos + i) % len(runes)
		rw := lipgloss.Width(string(runes[j]))
		if w+rw > max {
			break
		}
		idx = append(idx, j)
		w += rw
	}
	return idx
}

// ─── Repo picker popup ───────────────────────────────────────────────────

// renderRepoPopup builds the content of the repo picker popup: a title, then
// each repo name, with the currently selected line getting a cursor marker
// and the SelectionStyle background.
func renderRepoPopup(names []string, selected, width int, theme Theme) string {
	if width <= 0 {
		width = fallbackWidth
	}
	line := lipgloss.NewStyle().MaxWidth(width)

	var b strings.Builder
	b.WriteString(renderHeaderText(Row{Header: repoPopupTitle, HeaderFocus: true}, width, theme))
	for i, name := range names {
		b.WriteString("\n")
		marker := noCursorMarker
		text := name
		if i == selected {
			marker = cursorMarker
			text = theme.SelectionStyle().Render(name)
		}
		b.WriteString(line.Render(marker + text))
	}
	return b.String()
}

// treeBranch returns the connector "├─ "/"└─ " for element i out of n
// elements — shared by every place that draws a tree-style child list
// (learned from herdr: reference report section 4a/9.6). Currently only one
// caller: subagentRows below, factored into its own function so a second
// caller (if one appears) doesn't have to reimplement the "last element"
// edge-case logic.
func treeBranch(i, n int) string {
	if i == n-1 {
		return "└─ "
	}
	return "├─ "
}

// nestedIndent is the shared indent for the "what it's doing" line
// (IsInfoChild) and the subagent branch (IsSubagentChild) in the root list —
// 2 columns deeper than the parent pane row (those 2 columns being
// cursorMarker/noCursorMarker), so both are clearly nested under the right
// pane.
const nestedIndent = "    "

// infoArrow leads the "what it's doing" line under a pane row; actionArrow
// leads the "you need to act" line (Row.InfoAction).
const (
	infoArrow   = "↳ "
	actionArrow = "▲ "
)

// subagentRows builds the subagent branch Rows nested under ONE parent pane
// row — called right after buildRows (model.go) appends that pane row.
// Empty when paneAgent has no subagents at all (herdr lesson 9.2: show
// nothing at all, not even a bare header, when the child list is empty).
func subagentRows(names []string) []Row {
	if len(names) == 0 {
		return nil
	}
	rows := make([]Row, len(names))
	for i, name := range names {
		rows[i] = Row{IsSubagentChild: true, SubagentLabel: treeBranch(i, len(names)) + name}
	}
	return rows
}

// ─── Delete worktree confirmation popup ─────────────────────────────────────────

// renderRemoveConfirmPopup builds the content of the delete confirmation
// popup — REPLACES the pane list viewport content while open, the same way
// the other popups replace it.
func renderRemoveConfirmPopup(paneID, branch, errMsg string, width int, theme Theme) string {
	if width <= 0 {
		width = fallbackWidth
	}
	line := lipgloss.NewStyle().MaxWidth(width)

	title := branch
	if title == "" {
		title = paneID
	}

	rows := []string{
		theme.AccentStyle().Render("Delete worktree: " + title),
		"[enter/y] delete worktree + branch",
		"[c] close window only (keep worktree)",
		"[esc/n] cancel",
	}
	if errMsg != "" {
		rows = append(rows, lipgloss.NewStyle().Foreground(theme.StatusError).Render(errMsg))
	}

	var b strings.Builder
	for i, r := range rows {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(line.Render(r))
	}
	return b.String()
}

// renderKeyHints builds the fixed key-hint line at the BOTTOM of the pane
// (footerHeight, model.go), which changes depending on which popup is open.
func renderKeyHints(m Model, width int) string {
	hints := []string{"j/k", "move", "enter", "jump", "space", "act", ",", "settings", "/", "filter", "1-9", "jump", "r", "repo", "p", "pin", "v", "select", "x", "delete wt", "S", "clear subagents"}
	if m.lastRemoved != nil {
		hints = append([]string{"u", "undo"}, hints...)
	}
	switch {
	case m.removeConfirmOpen && len(m.removeConfirmPaneIDs) > 0:
		hints = []string{"enter/y", fmt.Sprintf("delete %d worktrees", len(m.removeConfirmPaneIDs)), "c", "close windows", "esc/n", "cancel"}
	case m.removeConfirmOpen:
		hints = []string{"enter/y", "delete", "c", "close window", "esc/n", "cancel"}
	case m.popup.IsRepoOpen():
		hints = []string{"enter", "select", "esc", "close"}
	case m.filterEditing:
		filter := lipgloss.NewStyle().Foreground(m.theme.Accent).Render("/ " + m.filterQuery + "▏")
		return lipgloss.NewStyle().MaxWidth(width).Render(filter + lipgloss.NewStyle().Foreground(m.theme.TextDim).Render("  enter keep · esc clear"))
	}
	// The clickable expand button leads the line (handleMouse, model.go);
	// then hints in priority order while they fit — dropping whole hints
	// reads better than cutting the line mid-word.
	key := lipgloss.NewStyle().Foreground(m.theme.Accent)
	desc := lipgloss.NewStyle().Foreground(m.theme.TextDim)
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Background(m.theme.Accent).Foreground(lipgloss.Color("0")).Bold(true).Render(expandButton) + "  ")
	used := lipgloss.Width(expandButton) + 2
	for i := 0; i+1 < len(hints); i += 2 {
		w := lipgloss.Width(hints[i]) + 1 + lipgloss.Width(hints[i+1])
		if used > 0 {
			w += 2
		}
		if used+w > width {
			break
		}
		if used > 0 {
			b.WriteString("  ")
		}
		b.WriteString(key.Render(hints[i]) + " " + desc.Render(hints[i+1]))
		used += w
	}
	return b.String()
}

// expandButton is drawn at the start of the key-hint line and names its key:
// o (or prefix + A from any pane, or a click) expands the sidebar into the
// dashboard.
const expandButton = " o ⇱ expand "

// View draws the current frame: the scrolling agent list (or the
// repo-picker/delete-confirmation popup when one is open — built ahead of
// time in Update/syncViewport) plus the fixed key-hint line at the bottom.
func (m Model) View() string {
	width := m.width
	if width <= 0 {
		width = fallbackWidth
	}
	if m.horizontal() {
		return m.horizontalView(width)
	}
	list := m.viewport.View()
	if ctx := m.contextLines(width); len(ctx) > 0 {
		lines := strings.Split(list, "\n")
		if len(lines) >= len(ctx) {
			list = strings.Join(append(lines[:len(lines)-len(ctx)], ctx...), "\n")
		}
	}
	if m.avatar.enabled() {
		av := m.renderAvatar(width)
		// Exactly the rows the layout gave the avatar: a frame taller than
		// the pane scrolls it, and rows the renderer doesn't repaint end up
		// shifted (resizeViewport catches up within a tick).
		if m.height > 0 {
			rows := max(0, m.height-footerHeight-m.viewport.Height)
			for len(av) < rows {
				av = append(av, "")
			}
			av = av[:rows]
		}
		list = strings.Join(av, "\n") + "\n" + list
	}
	return list + "\n" + renderKeyHints(m, width)
}

// contextMinLines: the Context section only appears when at least this many
// blank lines are left under the list (spacer + divider + one note).
const contextMinLines = 3
const contextMaxNotes = 5

// contextLines renders the shared-context board of the selected agent's
// project into the free space under the list: a divider with the project
// name, one line per note (author · text), and "+N more · o" when they don't
// all fit (o expands into the dashboard, which shows them in full). Empty
// when the board is, a popup is open, or there's no room.
func (m Model) contextLines(width int) []string {
	if m.popup.IsRepoOpen() || m.removeConfirmOpen || len(m.notes) == 0 {
		return nil
	}
	free := m.viewport.Height - len(m.rows)
	if free < contextMinLines {
		return nil
	}
	return m.boardLines(width, free)
}

func (m Model) boardLines(width, free int) []string {
	if m.popup.IsRepoOpen() || m.removeConfirmOpen || len(m.notes) == 0 || free < contextMinLines {
		return nil
	}
	dim := lipgloss.NewStyle().Foreground(m.theme.TextDim)
	project := m.notesScope
	if i := strings.LastIndexByte(project, '-'); i > 0 {
		project = project[:i]
	}
	head := "── board · " + project + " "
	out := []string{"", dim.Render(truncateEllipsis(head+strings.Repeat("─", max(0, width-lipgloss.Width(head))), width))}
	room := free - len(out)
	notes := m.notes
	count := min(len(notes), contextMaxNotes, room)
	more := len(notes) - count
	if more > 0 && room > 1 {
		count = min(count, room-1) // reserve a line for the hidden count
		more = len(notes) - count
	}
	notes = notes[len(notes)-count:]
	author := lipgloss.NewStyle().Foreground(m.theme.Branch)
	text := lipgloss.NewStyle().Foreground(m.theme.TextMuted)
	for _, n := range notes {
		a := truncateEllipsis(n.DisplayAuthor(), width/2)
		body := strings.ReplaceAll(n.Text, "\n", " ")
		if n.Scope == sharedctx.Global {
			body = "[global] " + body
		}
		out = append(out, author.Render(a)+" "+text.Render(truncateEllipsis(body, width-lipgloss.Width(a)-1)))
	}
	if more > 0 && room > 1 {
		out = append(out, dim.Render(truncateEllipsis(fmt.Sprintf("+%d earlier · o expand", more), width)))
	}
	return out
}
