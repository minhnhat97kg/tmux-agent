package watch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"tmux-agent/internal/config"
	"tmux-agent/internal/roles"
	"tmux-agent/internal/sharedctx"
	"tmux-agent/internal/sidebar"
	"tmux-agent/internal/tmux"
)

// Layout: one tab at a time — its list on the left, the selected item's
// detail on the right.
//
//	 Agents 5   Board 2   Roles                       o ⇲ collapse
//	╭─ Agents ─────╮╭─ <selected> · claude · 5:agents ── running 1m ─╮
//	│ ▸ 3 window   ││ ▲ NEEDS YOU  needs approval · enter to answer   │
//	│ ▌ lead     ⏱ ││ <live screen of the selected agent>              │
//	│   docs     ✓ │├─ Task ──────────────────────────────────────────┤
//	│              ││ last prompt / reply · tool · git diffstat        │
//
// Below narrowLayout columns the list and the detail stack.
const (
	narrowLayout = 90
	taskBoxH     = 6 // 4 content lines
	noteMaxLines = 2
)

func (m model) fg(c lipgloss.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }

// chip renders " text " on a colored background.
func (m model) chip(text string, bg lipgloss.Color) string {
	return lipgloss.NewStyle().Background(bg).Foreground(lipgloss.Color("0")).Bold(true).Render(" " + text + " ")
}

// fit truncates or pads s to exactly w visible cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = ansi.Truncate(s, w, "…")
	if pad := w - lipgloss.Width(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// spread puts left and right on one line of width w, right-aligned.
func spread(left, right string, w int) string {
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return fit(left, w)
	}
	return left + strings.Repeat(" ", gap) + right
}

// box draws a rounded panel of exactly w×h cells: title (and dim info) in
// the top border, an optional right-aligned status in it too; accent border
// when focused.
func (m model) box(title, info, right string, body []string, w, h int, focused bool) []string {
	border, titleColor := m.theme.TextDim, m.theme.SectionTitle
	if focused {
		border, titleColor = m.theme.Accent, m.theme.Accent
	}
	b := m.fg(border)
	head := m.fg(titleColor).Bold(true).Render(" " + title + " ")
	if info != "" {
		head += m.fg(m.theme.TextDim).Render(info + " ")
	}
	if right != "" {
		right = " " + right + " "
	}
	inner := w - 2
	fill := inner - 1 - lipgloss.Width(head) - lipgloss.Width(right) - 1
	if fill < 0 {
		right = ""
		fill = inner - 1 - lipgloss.Width(head)
	}
	if fill < 0 {
		head, fill = fit(head, inner-1), 0
	}
	top := b.Render("╭─") + head + b.Render(strings.Repeat("─", fill))
	if right != "" {
		top += right + b.Render("─")
	}
	out := []string{top + b.Render("╮")}
	for i := 0; i < h-2; i++ {
		line := ""
		if i < len(body) {
			line = body[i]
		}
		out = append(out, b.Render("│")+" "+fit(line, inner-2)+" "+b.Render("│"))
	}
	return append(out, b.Render("╰"+strings.Repeat("─", inner)+"╯"))
}

// listIcon is the agent list's trailing marker: a clock while it works, a
// check once it finished and you haven't looked yet, nothing otherwise
// (waiting agents get the ▲ action line instead).
func (m model) listIcon(a agentRow) (string, lipgloss.Color) {
	switch {
	case a.Status == tmux.StatusRunning || a.Status == tmux.StatusBackground:
		return m.icons.Clock, m.theme.StatusRunning
	case a.Status == tmux.StatusIdle && a.Unseen:
		return m.icons.Done, m.theme.StatusDone
	}
	return "", ""
}

func (m model) statusIcon(a agentRow) (string, lipgloss.Color) {
	switch {
	case a.Action != "" && a.Status == tmux.StatusWaiting:
		return m.icons.Confirm, m.theme.StatusConfirm
	case a.Status == tmux.StatusIdle && a.Unseen:
		return m.icons.Done, m.theme.StatusDone
	}
	return m.icons.StatusIcon(a.Status), m.theme.StatusColor(a.Status, false)
}

func elapsed(a agentRow) string {
	if !a.Status.IsActive() || a.StartedAt <= 0 {
		return ""
	}
	return (time.Since(time.Unix(a.StartedAt, 0)) / time.Second * time.Second).String()
}

// closeButton is the clickable top-right button (handleMouse, watch.go).
func (m model) closeButton() string {
	if m.docked {
		return "o ⇲ collapse"
	}
	return "q ✕ quit"
}

func (m model) View() string {
	if m.width < 40 || m.height < 12 {
		return "too small — widen the pane"
	}
	w := m.width
	footer := m.footer()
	bodyH := m.height - 2 - len(footer) // header + blank line + footer

	list := func(w, h int) []string {
		switch m.focus {
		case focusNotes:
			return m.notesBox(w, h)
		case focusRoles:
			return m.rolesBox(w, h)
		case focusSettings:
			return m.settingsBox(w, h)
		}
		return m.agentsBox(w, h)
	}
	var body []string
	if w >= narrowLayout {
		lw := min(max(w*30/100, 30), 48)
		left := list(lw, bodyH)
		right := m.previewBox(w-lw, bodyH)
		if m.focus == focusAgents {
			right = append(m.previewBox(w-lw, bodyH-taskBoxH), m.taskBox(w-lw, taskBoxH)...)
		}
		for i := range left {
			body = append(body, left[i]+right[i])
		}
	} else {
		listH := bodyH * 2 / 5
		body = append(list(w, listH), m.previewBox(w, bodyH-listH)...)
	}
	lines := append([]string{m.header(), ""}, body...)
	return strings.Join(append(lines, footer...), "\n")
}

// tabLabels are the header tabs, in focus order; their widths also map a
// header click to a tab (Update, watch.go).
func (m model) tabLabels() []string {
	return []string{fmt.Sprintf("1 Agents %d", len(m.agents)), fmt.Sprintf("2 Board %d", len(m.notes)), "3 Roles", "4 Settings"}
}

func (m model) header() string {
	left := ""
	for i, t := range m.tabLabels() {
		if focus(i) == m.focus {
			left += m.chip(t, m.theme.Accent)
		} else {
			left += m.fg(m.theme.TextMuted).Render(" " + t + " ")
		}
		left += " "
	}
	right := m.fg(m.theme.TextDim).Render(time.Now().Format("15:04:05")+"  ") + m.chip(m.closeButton(), m.theme.TextDim)
	return spread(left, right, m.width)
}

// agentsBox lists agents like the sidebar: a header per window, one line
// per agent (icon, name … timer agent), an action line under any agent that
// waits on the user. Scrolls to keep the selected agent visible.
func (m model) agentsBox(w, h int) []string {
	iw := w - 4
	dim := m.fg(m.theme.TextDim)
	var lines []string
	selLine, lastWin := 0, ""
	for i, a := range m.agents {
		if a.Window != lastWin {
			lastWin = a.Window
			lines = append(lines, dim.Render("▸ "+a.WinTitle))
		}
		selected := i == m.cursor[focusAgents]
		if selected {
			selLine = len(lines)
		}
		icon, color := m.listIcon(a)
		bar, name := "  ", m.fg(m.theme.AgentColor(a.Agent))
		if a.Agent == "" || a.Agent == tmux.AgentUnknown {
			name = m.fg(m.theme.TextPrimary)
		}
		switch a.Status {
		case tmux.StatusWaiting:
			name = m.fg(m.theme.StatusConfirm)
		case tmux.StatusError:
			name = m.fg(m.theme.StatusError)
		}
		if selected {
			bar, name = m.fg(m.theme.Accent).Render("▌ "), name.Bold(true)
		}
		right := ""
		if icon != "" {
			right = m.fg(color).Render(icon)
		}
		lines = append(lines, spread(bar+name.Render(label(a.PaneInfo)), right, iw))
		if br := sidebar.PaneBranch(a.PaneInfo); br != "" {
			lines = append(lines, dim.Render("  "+m.icons.GitBranch+" "+br))
		}
		if a.Action != "" {
			lines = append(lines, "    "+m.fg(m.theme.StatusConfirm).Bold(true).Render("▲ "+a.Action))
		}
	}
	if len(m.agents) == 0 {
		lines = []string{"", dim.Render("no agents — `tmux-agent new`")}
	}
	if rows := h - 2; selLine >= rows {
		lines = lines[selLine-rows+1:]
	}
	return m.box("Agents", fmt.Sprintf("%d", len(m.agents)), "", lines, w, h, true)
}

// project is the human part of a board scope ("calc-1a2b3c4d" → "calc").
func project(scope string) string {
	if i := strings.LastIndexByte(scope, '-'); i > 0 {
		return scope[:i]
	}
	return scope
}

// noteBody wraps a note's text to width, at most noteMaxLines lines.
func noteBody(text string, width int) []string {
	lines := strings.Split(ansi.Wordwrap(text, width, ""), "\n")
	if len(lines) > noteMaxLines {
		lines = lines[:noteMaxLines]
		lines[noteMaxLines-1] = ansi.Truncate(lines[noteMaxLines-1], width-1, "") + "…"
	}
	return lines
}

func (m model) notesBox(w, h int) []string {
	iw := w - 4
	dim := m.fg(m.theme.TextDim)
	focused := m.focus == focusNotes
	var body []string
	selLine := 0
	for i, n := range m.notes {
		bar, text := "  ", m.fg(m.theme.TextMuted)
		if focused && i == m.cursor[focusNotes] {
			bar, text = m.fg(m.theme.Accent).Render("▌ "), m.fg(m.theme.TextPrimary)
			selLine = len(body)
		}
		head := bar + m.fg(m.theme.Branch).Bold(true).Render(n.DisplayAuthor()) + " " + dim.Render(n.Time.Format("15:04"))
		if n.Scope == sharedctx.Global {
			head += " " + dim.Render("[global]")
		}
		body = append(body, spread(head, dim.Render(n.ID), iw))
		for _, l := range noteBody(n.Text, iw-2) {
			body = append(body, bar+text.Render(l))
		}
	}
	if len(m.notes) == 0 {
		body = []string{dim.Render("empty · a project note · G global note")}
	}
	if rows := h - 2; selLine+2 > rows {
		body = body[min(len(body), selLine+2-rows):]
	}
	return m.box("Board · "+project(m.scope), fmt.Sprintf("%d", len(m.notes)), "", body, w, h, focused)
}

// previewBox shows the selected agent's live screen (colors kept), led by a
// NEEDS YOU banner when it waits on the user — or, while the Board panel
// has focus, the selected note in full (the list truncates notes).
func (m model) previewBox(w, h int) []string {
	switch {
	case m.focus == focusNotes && len(m.notes) > 0:
		return m.noteDetailBox(w, h)
	case m.focus == focusNotes:
		return m.box("Note", "", "", []string{m.fg(m.theme.TextDim).Render("no notes yet · a to add one")}, w, h, false)
	case m.focus == focusRoles && len(m.roles) > 0:
		return m.roleDetailBox(w, h)
	case m.focus == focusRoles:
		return m.box("Role", "", "", []string{m.fg(m.theme.TextDim).Render("no roles")}, w, h, false)
	case m.focus == focusSettings:
		return m.settingsDetailBox(w, h)
	}
	a, ok := m.selectedAgent()
	if !ok {
		return m.box("Preview", "", "", []string{m.fg(m.theme.TextDim).Render("select an agent")}, w, h, false)
	}
	iw := w - 4
	var body []string
	if a.Action != "" {
		body = append(body,
			m.chip("▲ NEEDS YOU", m.theme.StatusConfirm)+" "+m.fg(m.theme.StatusConfirm).Bold(true).Render(a.Action)+
				m.fg(m.theme.TextDim).Render("  ·  enter: jump there to answer"),
			"")
	}
	rows := h - 2 - len(body)
	screen := m.preview
	if len(screen) > rows {
		screen = screen[len(screen)-rows:]
	}
	for _, l := range screen {
		// Close the pane's own SGR state before our padding and border.
		body = append(body, ansi.Truncate(l, iw, "")+"\x1b[0m")
	}
	_, color := m.statusIcon(a)
	status := m.fg(color).Bold(true).Render(statusLabel(a.PaneInfo))
	if el := elapsed(a); el != "" {
		status += m.fg(m.theme.TextDim).Render(" " + el)
	}
	info := m.theme.AgentStyle(a.Agent).Render(string(a.Agent)) + " · " + a.Where + " · " + a.PaneID
	return m.box(label(a.PaneInfo), info, status, body, w, h, false)
}

// noteDetailBox renders the selected note in full, word-wrapped.
func (m model) noteDetailBox(w, h int) []string {
	n := m.notes[m.cursor[focusNotes]]
	dim := m.fg(m.theme.TextDim)
	body := []string{
		dim.Render("by ") + m.fg(m.theme.Branch).Bold(true).Render(n.DisplayAuthor()) +
			dim.Render(" · "+n.Time.Format("2006-01-02 15:04")+" · pane "+n.Pane+" · id "+n.ID),
		"",
	}
	text := m.fg(m.theme.TextPrimary)
	for _, l := range strings.Split(ansi.Wordwrap(n.Text, w-4, ""), "\n") {
		body = append(body, text.Render(l))
	}
	flag := ""
	if n.Scope == sharedctx.Global {
		flag = " --global"
	}
	body = append(body, "", dim.Render("d delete · 1 back to agents · tmux-agent ctx"+flag+" rm "+n.ID))
	return m.box("Note", fmt.Sprintf("%d/%d", m.cursor[focusNotes]+1, len(m.notes)), "", body, w, h, true)
}

// taskBox: what the selected agent was asked / answered last, what it's
// doing now, and what it changed (git).
func (m model) taskBox(w, h int) []string {
	a, ok := m.selectedAgent()
	if !ok {
		return m.box("Task", "", "", nil, w, h, false)
	}
	dim := m.fg(m.theme.TextDim)
	var body []string
	switch {
	case a.Prompt == "":
		body = append(body, dim.Render("no prompt yet"))
	case a.PromptIsResponse && m.summary != "":
		body = append(body, dim.Render("done  ")+m.fg(m.theme.StatusDone).Bold(true).Render(m.summary))
	case a.PromptIsResponse:
		body = append(body, dim.Render("reply ")+m.fg(m.theme.TextPrimary).Render(firstLine(a.Prompt)))
	default:
		body = append(body, dim.Render("task  ")+m.fg(m.theme.TextPrimary).Render(firstLine(a.Prompt)))
	}
	if a.Tool != "" {
		body = append(body, dim.Render("now   ")+m.fg(m.theme.StatusRunning).Bold(true).Render(a.Tool)+" "+m.fg(m.theme.TextMuted).Render(a.Doing))
	}
	gitLine := dim.Render("git   ")
	if m.git.Branch != "" {
		gitLine += m.fg(m.theme.Branch).Render(m.git.Branch) + "  "
	}
	switch d := m.git.DiffStat; {
	case d != nil:
		gitLine += m.fg(m.theme.DiffAdded).Render(fmt.Sprintf("+%d", d.Insertions)) + " " +
			m.fg(m.theme.DiffDeleted).Render(fmt.Sprintf("−%d", d.Deletions)) +
			dim.Render(fmt.Sprintf(" · %d files changed", m.git.ChangedFileCount()))
	case m.git.Branch != "":
		gitLine += dim.Render("no uncommitted changes")
	default:
		gitLine += dim.Render("—") // not a repo, or no commit yet
	}
	body = append(body, gitLine)
	if m.hasTok {
		body = append(body, dim.Render("used  ")+m.fg(m.theme.TextMuted).Render(m.tokens.Short()+" tokens"))
	}
	return m.box("Task", "", "", body, w, h, false)
}

func (m model) footer() []string {
	key := m.fg(m.theme.Accent).Bold(true)
	dim := m.fg(m.theme.TextDim)
	if m.mode != inputNone {
		prompt := "note for this project ❯ "
		if a, ok := m.selectedAgent(); ok && m.mode == inputSend {
			prompt = "send to " + label(a.PaneInfo) + " ❯ "
		}
		return []string{fit(" "+key.Render(prompt)+m.input.View(), m.width),
			fit(" "+key.Render("enter")+dim.Render(" submit  ")+key.Render("esc")+dim.Render(" cancel"), m.width)}
	}
	closeKey, closeHint := "q", "quit"
	if m.docked {
		closeKey, closeHint = "o", "collapse"
	}
	var hints []string
	switch m.focus { // only the keys that do something in this panel
	case focusAgents:
		hints = []string{"j/k", "select", "enter", "jump", "s", "send prompt", "a", "add note"}
	case focusNotes:
		hints = []string{"j/k", "select", "a", "project note", "G", "global note", "d", "delete"}
	case focusRoles:
		hints = []string{"j/k", "select", "n", "start agent", "e", "edit", "a", "new role", "d", "delete yours"}
	case focusSettings:
		hints = []string{"j/k", "select", "enter", "toggle", "h/l", "change", "e", "edit", "r", "reset"}
	}
	if m.editor != nil && m.focus == focusSettings && m.editor.Editing() {
		hints = []string{"enter", "save", "esc", "cancel"}
	}
	hints = append(hints, "tab/1-4", "switch tab", closeKey, closeHint)
	var b strings.Builder
	b.WriteString(" ")
	used := 1
	for i := 0; i+1 < len(hints); i += 2 {
		part := key.Render(hints[i]) + dim.Render(" "+hints[i+1]+"   ")
		if used+lipgloss.Width(part) > m.width {
			break
		}
		b.WriteString(part)
		used += lipgloss.Width(part)
	}
	flash, flashBad := m.flash, false
	if m.editor != nil && m.focus == focusSettings {
		flash, flashBad = m.editor.Flash()
	}
	flashLine := ""
	if flash != "" {
		color := m.theme.StatusWaiting
		if flashBad {
			color = m.theme.StatusError
		}
		flashLine = m.fg(color).Render(" " + flash)
	}
	return []string{fit(flashLine, m.width), fit(b.String(), m.width)}
}

// settingsBox lists every setting grouped by group, like the agent list:
// one selectable label per setting, scrolling to keep
// the selection visible.
func (m model) settingsBox(w, h int) []string {
	dim := m.fg(m.theme.TextDim)
	if m.editor == nil {
		return m.box("Settings", "", "", []string{dim.Render("no settings")}, w, h, true)
	}
	body, selLine := m.editor.Body(m.theme, max(1, w-6), false)
	if rows := h - 2; selLine >= rows {
		body = body[min(len(body), selLine-rows+1):]
	}
	return m.box("Settings", fmt.Sprintf("%d", len(config.Settings)), "", body, w, h, true)
}

// settingsDetailBox shows the selected setting in full: where it lives, what
// it is set to, its default, what it means and which keys change it.
func (m model) settingsDetailBox(w, h int) []string {
	dim := m.fg(m.theme.TextDim)
	if m.editor == nil {
		return m.box("Setting", "", "", []string{dim.Render("no settings")}, w, h, true)
	}
	s := m.editor.Selected()
	v := m.editor.Value(s.Key)
	value := m.settingValue(s, v)
	if m.editor.Editing() {
		value = m.editor.InputView()
	}
	iw := w - 4
	body := []string{
		m.chip(s.Group, m.theme.TextDim),
		m.fg(m.theme.TextPrimary).Bold(true).Render(s.Label),
		dim.Render(s.Key),
		"",
		dim.Render("value    ") + value,
		dim.Render("default  ") + m.fg(m.theme.TextMuted).Render(config.Display(s.Default)),
	}
	if len(s.Choices) > 0 {
		body = append(body, dim.Render("choices  ")+m.fg(m.theme.TextMuted).Render(strings.Join(s.Choices, " · ")))
	}
	if s.Kind == config.Int {
		body = append(body, dim.Render("range    ")+m.fg(m.theme.TextMuted).Render(fmt.Sprintf("%d–%d", s.Min, s.Max)))
	}
	if s.Reload {
		body = append(body, dim.Render("reloads the plugin to rebind the key"))
	}
	if s.Help != "" {
		body = append(body, "")
		for _, l := range strings.Split(ansi.Wordwrap(s.Help, iw, ""), "\n") {
			body = append(body, m.fg(m.theme.TextMuted).Render(l))
		}
	}

	key := m.fg(m.theme.Accent).Bold(true)
	actions := []string{"enter/space", "toggle or cycle forward", "h/l ←/→", "cycle back / forward", "e", "edit", "r", "reset"}
	var bar string
	for i := 0; i+1 < len(actions); i += 2 {
		bar += key.Render(actions[i]) + dim.Render(" "+actions[i+1]+"   ")
	}
	usage := dim.Render("tmux-agent config set " + s.Key + " <value>")
	if pad := h - 2 - len(body) - 3; pad > 0 {
		body = append(body, make([]string, pad)...)
	} else {
		body = append(body, "")
	}
	body = append(body, dim.Render(strings.Repeat("─", iw)), bar, usage)
	return m.box("Setting · "+s.Label, fmt.Sprintf("%d/%d", m.editor.Cursor()+1, len(config.Settings)), "", body, w, h, true)
}

// settingValue styles a setting's current value by kind.
func (m model) settingValue(s config.Setting, v string) string {
	switch s.Kind {
	case config.Bool:
		color, text := m.theme.StatusRunning, "● on"
		if v != "on" {
			color, text = m.theme.TextDim, "○ off"
		}
		return m.fg(color).Bold(true).Render(text)
	case config.Choice, config.Preset:
		return m.fg(m.theme.TextPrimary).Bold(true).Render(config.Display(v))
	}
	return m.fg(m.theme.TextPrimary).Render(config.Display(v))
}

// rolesBox lists the roles (`tmux-agent roles`): yours marked, the
// built-ins they override included.
func (m model) rolesBox(w, h int) []string {
	iw := w - 4
	dim := m.fg(m.theme.TextDim)
	var body []string
	for i, r := range m.roles {
		bar, text := "  ", m.fg(m.theme.TextMuted)
		if i == m.cursor[focusRoles] {
			bar, text = m.fg(m.theme.Accent).Render("▌ "), m.fg(m.theme.TextPrimary).Bold(true)
		}
		body = append(body, spread(bar+text.Render(r), dim.Render(roleSource(r)), iw))
	}
	if rows := h - 2; m.cursor[focusRoles]+1 > rows {
		body = body[m.cursor[focusRoles]+1-rows:]
	}
	return m.box("Roles", fmt.Sprintf("%d", len(m.roles)), "", body, w, h, true)
}

// roleSource: "yours" for a file in roles.Dir(), else "built-in".
func roleSource(name string) string {
	if _, err := os.Stat(filepath.Join(roles.Dir(), name+".md")); err == nil {
		return "yours"
	}
	return "built-in"
}

// roleDetailBox shows the selected role's prompt, its markdown rendered
// (bold, `code`, bullets), and what can be done with it.
func (m model) roleDetailBox(w, h int) []string {
	name := m.roles[m.cursor[focusRoles]]
	text, _ := roles.Get(name)
	dim, key := m.fg(m.theme.TextDim), m.fg(m.theme.Accent).Bold(true)
	iw := w - 4

	file := filepath.Join(roles.Dir(), name+".md")
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(file, home) {
		file = "~" + file[len(home):]
	}
	src := m.chip("built-in", m.theme.TextDim) + dim.Render("  edit to override → "+file)
	if roleSource(name) == "yours" {
		src = m.chip("yours", m.theme.StatusDone) + dim.Render("  "+file)
	}
	body := []string{src, ""}
	body = append(body, m.markdown(text, iw)...)

	actions := []string{"n", "start an agent", "e", "edit in $EDITOR", "a", "new role"}
	if roleSource(name) == "yours" {
		actions = append(actions, "d", "delete")
	}
	var bar string
	for i := 0; i+1 < len(actions); i += 2 {
		bar += key.Render(actions[i]) + dim.Render(" "+actions[i+1]+"   ")
	}
	usage := dim.Render("tmux-agent new --role " + name + "   ·   team: <name>@" + name)
	if pad := h - 2 - len(body) - 3; pad > 0 {
		body = append(body, make([]string, pad)...)
	} else {
		body = append(body, "")
	}
	body = append(body, m.fg(m.theme.TextDim).Render(strings.Repeat("─", iw)), bar, usage)
	return m.box("Role · "+name, "", "", body, w, h, true)
}

// markdown renders a role prompt: **bold**, `code`, "- " bullets with a
// hanging indent; everything else as plain wrapped text.
// ponytail: just the subset role files use; a real renderer (glamour) if
// roles grow headings / tables.
func (m model) markdown(text string, w int) []string {
	plain := m.fg(m.theme.TextPrimary)
	bold := plain.Bold(true)
	code := m.fg(m.theme.Branch)
	var out []string
	for _, para := range strings.Split(text, "\n") {
		lead, rest := "", para
		if strings.HasPrefix(para, "- ") || strings.HasPrefix(para, "* ") {
			lead, rest = m.fg(m.theme.Accent).Render("•")+" ", para[2:]
		}
		inBold, inCode := false, false
		for i, l := range strings.Split(ansi.Wordwrap(rest, w-lipgloss.Width(lead), ""), "\n") {
			var b strings.Builder
			for j := 0; j < len(l); j++ {
				switch {
				case !inCode && strings.HasPrefix(l[j:], "**"):
					inBold = !inBold
					j++
					continue
				case l[j] == '`':
					inCode = !inCode
					continue
				}
				// Find the run up to the next marker and style it as one piece.
				k := j
				for k < len(l) && l[k] != '`' && !(!inCode && strings.HasPrefix(l[k:], "**")) {
					k++
				}
				st := plain
				if inCode {
					st = code
				} else if inBold {
					st = bold
				}
				b.WriteString(st.Render(l[j:k]))
				j = k - 1
			}
			prefix := strings.Repeat(" ", lipgloss.Width(lead))
			if i == 0 {
				prefix = lead
			}
			out = append(out, prefix+b.String())
		}
		if lead == "" && para == "" {
			out = append(out, "")
		}
	}
	return out
}
