// Package watch is the agent dashboard, a list + preview layout: on the
// left every agent (grouped by window, like the sidebar) and the shared
// context board of the selected agent's project; on the right a live
// preview of the selected agent's screen, a NEEDS YOU banner when it waits
// on the user, and its task (last prompt/reply, files changed).
//
// It normally runs docked: the sidebar pane expands into it (`o` in the
// sidebar, or prefix + A — sidebar.SetExpanded) and collapses back with
// o/q/esc. `tmux-agent watch` without --docked runs it full-screen.
package watch

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"tmux-agent/internal/activity"
	"tmux-agent/internal/git"
	"tmux-agent/internal/queue"
	"tmux-agent/internal/roles"
	"tmux-agent/internal/sharedctx"
	"tmux-agent/internal/sidebar"
	"tmux-agent/internal/tmux"
	"tmux-agent/internal/usage"
)

// Run starts the dashboard. args: [--docked].
func Run(args []string) int {
	docked := len(args) > 0 && args[0] == "--docked"
	opts := tmux.GetAllGlobalOptions()
	m := model{
		self:   os.Getenv("TMUX_PANE"),
		docked: docked,
		theme:  sidebar.ThemeFromOptions(opts),
		icons:  sidebar.IconsFromOptions(opts),
		last:   map[string]paneState{},
	}
	m.input = textinput.New()
	m.input.CharLimit = 2000
	var popts []tea.ProgramOption
	popts = append(popts, tea.WithMouseCellMotion()) // close button, wheel
	if !docked {
		popts = append(popts, tea.WithAltScreen())
	}
	if _, err := tea.NewProgram(m, popts...).Run(); err != nil {
		return 1
	}
	return 0
}

const (
	pollEvery = time.Second
	maxEvents = 200
)

type focus int

const (
	focusAgents focus = iota
	focusNotes
	focusRoles
)

type inputMode int

const (
	inputNone inputMode = iota
	inputNote
	inputRole // name of a new role
	inputSend
)

// agentRow is one agent, flattened out of the session tree.
type agentRow struct {
	tmux.PaneInfo
	Where    string // session:window
	Window   string // window id, for grouping
	WinTitle string // "3 window" (session-prefixed when several sessions)
	Tool     string // current tool while working, "" otherwise
	Doing    string // tool argument, or last prompt / reply (first line)
	Action   string // what the user must do (sidebar.ActionLine), "" if nothing
}

// doingLine is Tool+Doing as one string, for the event stream.
func (r agentRow) doingLine() string { return strings.TrimSpace(r.Tool + " " + r.Doing) }

// paneState is what the previous poll saw, to turn snapshots into events.
type paneState struct {
	status string
	doing  string
}

type event struct {
	at   time.Time
	who  string
	text string
	kind string // status value, or "tool"
}

type snapshot struct {
	agents []agentRow
	scope  string // board shown in the notes panel
	notes  []sharedctx.Entry
	// Of the selected agent: its screen (ANSI colors kept) and git state.
	preview []string
	git     git.GitData
	tokens  usage.Tokens
	hasTok  bool
	summary string // one-line LLM summary of the last reply
}

type tickMsg time.Time

type model struct {
	self   string
	docked bool
	theme  sidebar.Theme
	icons  sidebar.Icons
	width  int
	height int

	agents  []agentRow
	scope   string
	notes   []sharedctx.Entry
	preview []string
	git     git.GitData
	tokens  usage.Tokens
	hasTok  bool
	summary string
	// events/last/primed feed `tmux-agent events` (Stream) only.
	events []event
	last   map[string]paneState
	primed bool

	roles []string // role names, loaded when the Roles panel opens

	focus  focus
	cursor [3]int
	mode   inputMode
	input  textinput.Model
	flash  string
}

func (m model) Init() tea.Cmd { return m.poll() }

// poll loads a snapshot; the notes panel follows the project of the agent
// under the cursor.
func (m model) poll() tea.Cmd {
	pane := ""
	if a, ok := m.selectedAgent(); ok {
		pane = a.PaneID
	}
	return func() tea.Msg { return load(pane) }
}

// load reads every agent plus, for the selected one (the first agent when
// selected is "" or gone), its screen, git state and project board.
func load(selected string) snapshot {
	sessions := tmux.QuerySessions()
	var rows []agentRow
	for _, s := range sessions {
		for _, w := range s.Windows {
			title := w.WindowIndex + " " + w.WindowName
			if len(sessions) > 1 {
				title = s.SessionName + ":" + title
			}
			for _, p := range w.Panes {
				tool, text := doing(p)
				rows = append(rows, agentRow{
					PaneInfo: p, Where: s.SessionName + ":" + w.WindowName,
					Window: w.WindowID, WinTitle: title,
					Tool: tool, Doing: text, Action: sidebar.ActionLine(p),
				})
			}
		}
	}
	snap := snapshot{agents: rows, scope: sharedctx.Global}
	var sel *agentRow
	for i := range rows {
		if rows[i].PaneID == selected || sel == nil {
			sel = &rows[i]
		}
		if rows[i].PaneID == selected {
			break
		}
	}
	if sel != nil {
		if sel.Path != "" {
			snap.scope = sharedctx.Scope(sel.Path)
			snap.git = git.FetchGitData(sel.Path)
		}
		if out, ok := tmux.RunTmux("capture-pane", "-p", "-e", "-t", sel.PaneID); ok {
			snap.preview = screenLines(out)
		}
		snap.tokens, snap.hasTok = usage.ForSession(sel.SessionID)
		if sel.PromptIsResponse && !sel.Status.IsActive() {
			snap.summary, _ = sidebar.Summary(sel.Prompt, sidebar.AvatarConfigFromTmux())
		}
	}
	snap.notes = sharedctx.List(snap.scope)
	return snap
}

// screenLines splits a captured screen into lines, dropping the blank tail
// (an idle TUI leaves most of the screen empty below its prompt).
func screenLines(out string) []string {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// firstLine keeps one line of possibly multi-line text (prompts, replies,
// shell commands).
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i]) + " …"
	}
	return s
}

// doing is the one-line "what is it doing": the latest tool call while it
// works, otherwise the last prompt / reply.
func doing(p tmux.PaneInfo) (tool, text string) {
	if p.Status.IsActive() {
		if e := activity.ReadActivityLog(p.PaneID, 1); len(e) > 0 && e[0].Tool != activity.TaskResetMarker {
			return e[0].Tool, firstLine(e[0].Label)
		}
	}
	if p.Prompt == "" {
		return "", ""
	}
	if p.PromptIsResponse {
		return "", "‹ " + firstLine(p.Prompt)
	}
	return "", "› " + firstLine(p.Prompt)
}

// label is how an agent is called: its name, else its project directory.
func label(p tmux.PaneInfo) string {
	if p.Name != "" {
		return p.Name
	}
	if p.Path != "" {
		return filepath.Base(p.Path)
	}
	return p.PaneID
}

func statusLabel(p tmux.PaneInfo) string {
	if p.Status == tmux.StatusIdle && p.Unseen {
		return "done"
	}
	return string(p.Status)
}

// diff appends events for everything that changed since the last poll.
func (m *model) diff(rows []agentRow) {
	now := time.Now()
	seen := map[string]bool{}
	for _, r := range rows {
		seen[r.PaneID] = true
		st := statusLabel(r.PaneInfo)
		prev, had := m.last[r.PaneID]
		m.last[r.PaneID] = paneState{status: st, doing: r.doingLine()}
		if !m.primed {
			continue
		}
		switch {
		case !had:
			m.push(event{now, label(r.PaneInfo), fmt.Sprintf("appeared (%s, %s)", r.Agent, st), st})
		case prev.status != st:
			m.push(event{now, label(r.PaneInfo), prev.status + " → " + st, st})
		case r.Status.IsActive() && r.doingLine() != "" && prev.doing != r.doingLine():
			m.push(event{now, label(r.PaneInfo), r.doingLine(), "tool"})
		}
	}
	for id := range m.last {
		if !seen[id] {
			delete(m.last, id)
			m.push(event{now, id, "gone", "gone"})
		}
	}
	m.primed = true
}

func (m *model) push(e event) {
	m.events = append(m.events, e)
	if len(m.events) > maxEvents {
		m.events = m.events[len(m.events)-maxEvents:]
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = msg.Width - 30
	case snapshot:
		m.agents, m.scope, m.notes = msg.agents, msg.scope, msg.notes
		m.preview, m.git = msg.preview, msg.git
		m.tokens, m.hasTok, m.summary = msg.tokens, msg.hasTok, msg.summary
		m.clampCursor()
		return m, tea.Tick(pollEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
	case tickMsg:
		return m, m.poll()
	case flashMsg:
		m.flash = string(msg)
		return m, m.poll()
	case roleEditedMsg:
		m.flash = msg.flash
		m.roles = roles.Names()
		for i, r := range m.roles {
			if r == msg.name {
				m.cursor[focusRoles] = i
			}
		}
		return m, nil
	case tea.KeyMsg:
		if m.mode != inputNone {
			return m.updateInput(msg)
		}
		return m.updateKeys(msg)
	case tea.MouseMsg:
		if m.mode != inputNone {
			return m, nil
		}
		switch {
		case msg.Button == tea.MouseButtonWheelDown:
			return m.updateKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		case msg.Button == tea.MouseButtonWheelUp:
			return m.updateKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
		case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress &&
			msg.Y == 0 && msg.X >= m.width-lipgloss.Width(m.closeButton())-2:
			return m, m.leave()
		case msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress && msg.Y == 0:
			x := 0
			for i, t := range m.tabLabels() {
				x += lipgloss.Width(t) + 3 // " t " + gap
				if msg.X < x {
					return m.setTab(focus(i)), nil
				}
			}
		}
	}
	return m, nil
}

// setTab switches the visible tab; the Roles list loads when it opens.
func (m model) setTab(f focus) model {
	m.focus = f
	if f == focusRoles {
		m.roles = roles.Names()
		m.clampCursor()
	}
	return m
}

func (m *model) clampCursor() {
	n := [3]int{len(m.agents), len(m.notes), len(m.roles)}
	for i := range m.cursor {
		if m.cursor[i] >= n[i] {
			m.cursor[i] = n[i] - 1
		}
		if m.cursor[i] < 0 {
			m.cursor[i] = 0
		}
	}
}

func (m model) selectedAgent() (agentRow, bool) {
	if len(m.agents) == 0 || m.cursor[focusAgents] >= len(m.agents) {
		return agentRow{}, false
	}
	return m.agents[m.cursor[focusAgents]], true
}

// leave closes the dashboard: docked, it collapses back into the sidebar
// list (the respawn ends this process); full-screen, it just quits.
func (m model) leave() tea.Cmd {
	if m.docked && m.self != "" {
		self := m.self
		return func() tea.Msg { sidebar.SetExpanded(self, false); return nil }
	}
	return tea.Quit
}

func (m model) updateKeys(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.flash = ""
	switch k.String() {
	case "q", "esc", "o", "ctrl+c":
		return m, m.leave()
	case ",":
		return m, sidebar.OpenSettings(m.self)
	case "tab", "shift+tab", "1", "2", "3":
		switch k.String() {
		case "tab":
			m = m.setTab((m.focus + 1) % 3)
		case "shift+tab":
			m = m.setTab((m.focus + 2) % 3)
		default:
			m = m.setTab(focus(k.Runes[0] - '1'))
		}
	case "j", "down":
		m.cursor[m.focus]++
		m.clampCursor()
		return m, m.poll() // notes follow the selected agent's project
	case "k", "up":
		m.cursor[m.focus]--
		m.clampCursor()
		return m, m.poll()
	case "enter":
		if a, ok := m.selectedAgent(); ok && m.focus == focusAgents {
			tmux.SelectPane(a.PaneID, m.self)
			return m, m.leave()
		}
	case "s":
		if a, ok := m.selectedAgent(); ok && m.focus == focusAgents {
			if a.Status == tmux.StatusWaiting {
				m.flash = label(a.PaneInfo) + " is waiting for you — answer it in its pane first (enter jumps there)"
				break
			}
			m.mode = inputSend
			m.input.Placeholder = "prompt for " + label(a.PaneInfo)
			m.input.SetValue("")
			return m, m.input.Focus()
		}
	case "e":
		if m.focus == focusRoles && len(m.roles) > 0 {
			return m, editRole(m.roles[m.cursor[focusRoles]])
		}
	case "a":
		if m.focus == focusRoles {
			m.mode = inputRole
			m.input.Placeholder = "name of the new role (opens $EDITOR)"
			m.input.SetValue("")
			return m, m.input.Focus()
		}
		m.mode = inputNote
		m.input.Placeholder = "note for every agent on this project"
		m.input.SetValue("")
		return m, m.input.Focus()
	case "n":
		if m.focus == focusRoles && len(m.roles) > 0 {
			role := m.roles[m.cursor[focusRoles]]
			m.flash = "starting a " + role + "…"
			return m, m.spawnRole(role)
		}
	case "d", "x":
		if m.focus == focusRoles && len(m.roles) > 0 {
			name := m.roles[m.cursor[focusRoles]]
			if err := os.Remove(filepath.Join(roles.Dir(), name+".md")); err != nil {
				m.flash = name + " is built-in — only your own roles can be deleted"
			} else if _, ok := roles.Get(name); ok {
				m.flash = "removed your " + name + " — back to the built-in"
			} else {
				m.flash = "deleted role " + name
			}
			m.roles = roles.Names()
			m.clampCursor()
			return m, nil
		}
		if m.focus == focusNotes && len(m.notes) > 0 {
			if err := sharedctx.Remove(m.scope, m.notes[m.cursor[focusNotes]].ID); err != nil {
				m.flash = err.Error()
			}
			return m, m.poll()
		}
	}
	return m, nil
}

func (m model) updateInput(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.mode = inputNone
		m.input.Blur()
		return m, nil
	case "enter":
		text := strings.TrimSpace(m.input.Value())
		mode := m.mode
		m.mode = inputNone
		m.input.Blur()
		if text == "" {
			return m, nil
		}
		if mode == inputRole {
			if strings.ContainsAny(text, " /\\.") {
				m.flash = "a role name is one word (letters, digits, - _)"
				return m, nil
			}
			return m, editRole(text)
		}
		if mode == inputNote {
			if _, err := sharedctx.Add(m.scope, "user", m.self, text); err != nil {
				m.flash = err.Error()
			}
			return m, m.poll()
		}
		if a, ok := m.selectedAgent(); ok {
			id := a.PaneID
			m.flash = "sent to " + label(a.PaneInfo)
			if a.Status.IsActive() { // busy: queue it, delivered when idle
				if _, _, err := queue.Push(id, text); err == nil {
					m.flash = "queued for " + label(a.PaneInfo) + " (it's busy)"
				}
				return m, m.poll()
			}
			return m, func() tea.Msg {
				tmux.TypeLine(id, text)
				return load(id)
			}
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}

// Stream is `tmux-agent events`: the same event feed as the dashboard's
// Stream panel, one tab-separated line per event (time, agent, kind, text),
// for scripts and agents (`tmux-agent events | grep -m1 'tester.*done'`).
// Runs until the writer fails (e.g. the reader closed the pipe).
func Stream(args []string, w io.Writer) int {
	asJSON := len(args) > 0 && args[0] == "--json"
	m := model{last: map[string]paneState{}}
	for {
		var rows []agentRow
		for _, s := range tmux.QuerySessions() {
			for _, win := range s.Windows {
				for _, p := range win.Panes {
					tool, text := doing(p)
					rows = append(rows, agentRow{PaneInfo: p, Tool: tool, Doing: text})
				}
			}
		}
		m.diff(rows)
		for _, e := range m.events {
			var err error
			if asJSON { // one object per line (NDJSON)
				var b []byte
				b, err = json.Marshal(map[string]string{"time": e.at.Format(time.RFC3339), "agent": e.who, "kind": e.kind, "text": e.text})
				if err == nil {
					_, err = fmt.Fprintf(w, "%s\n", b)
				}
			} else {
				_, err = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.at.Format("15:04:05"), e.who, e.kind, e.text)
			}
			if err != nil {
				return 0
			}
		}
		m.events = m.events[:0]
		time.Sleep(pollEvery)
	}
}

type flashMsg string

// spawnRole starts an agent with role through `tmux-agent new` (same
// binary), in the selected agent's project, or the dashboard's directory.
func (m model) spawnRole(role string) tea.Cmd {
	dir := ""
	if a, ok := m.selectedAgent(); ok {
		dir = a.Path
	}
	return func() tea.Msg {
		bin, err := os.Executable()
		if err != nil {
			return flashMsg(err.Error())
		}
		cmd := exec.Command(bin, "new", "--role", role, "--no-wait")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			return flashMsg(strings.TrimSpace(string(out)))
		}
		return flashMsg("started " + role + " in " + strings.TrimSpace(string(out)) + " (agents window)")
	}
}

type roleEditedMsg struct{ name, flash string }

// editRole opens the role's file in $VISUAL / $EDITOR (vi without either),
// in this pane. A built-in is copied to your roles dir first — your copy
// then overrides it; a new name starts from a template.
func editRole(name string) tea.Cmd {
	file := filepath.Join(roles.Dir(), name+".md")
	if _, err := os.Stat(file); err != nil {
		text, ok := roles.Get(name)
		if !ok {
			text = "You are the **" + name + "** in a team of coding agents coordinated through tmux-agent.\n- What you do:\n- What you don't touch:\n- How you report when done (e.g. `tmux-agent ctx add \"DONE @<you>: …\"`):"
		}
		if err := os.MkdirAll(roles.Dir(), 0o755); err != nil {
			return func() tea.Msg { return flashMsg(err.Error()) }
		}
		if err := os.WriteFile(file, []byte(text+"\n"), 0o644); err != nil {
			return func() tea.Msg { return flashMsg(err.Error()) }
		}
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	// sh -c: $EDITOR may carry arguments ("code -w", "nvim -u …").
	cmd := exec.Command("sh", "-c", editor+` "$1"`, "sh", file)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return roleEditedMsg{name, "editor: " + err.Error()}
		}
		return roleEditedMsg{name, "saved " + file}
	})
}
