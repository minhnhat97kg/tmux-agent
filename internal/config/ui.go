package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"tmux-agent/internal/sidebar"
	"tmux-agent/internal/tmux"
)

// RunUI is the settings screen (`tmux-agent config` with no arguments;
// opened in a popup by `,` in the sidebar or dashboard).
func RunUI() int {
	opts := tmux.GetAllGlobalOptions()
	m := uiModel{theme: sidebar.ThemeFromOptions(opts), values: map[string]string{}}
	for _, s := range Settings {
		m.values[s.Key] = Get(s)
	}
	m.input = textinput.New()
	m.input.CharLimit = 500
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		return 1
	}
	return 0
}

type uiModel struct {
	theme         sidebar.Theme
	width, height int
	cursor        int
	values        map[string]string
	editing       bool
	input         textinput.Model
	flash         string
	flashBad      bool
}

func (m uiModel) Init() tea.Cmd { return nil }

func (m uiModel) sel() Setting { return Settings[m.cursor] }

func (m uiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = max(10, msg.Width-30)
		return m, nil
	case tea.KeyMsg:
		if m.editing {
			switch msg.String() {
			case "esc":
				m.editing = false
				m.input.Blur()
				return m, nil
			case "enter":
				m.editing = false
				m.input.Blur()
				m.apply(m.sel(), m.input.Value())
				return m, nil
			}
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
		m.flash = ""
		s := m.sel()
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "j", "down":
			m.cursor = min(m.cursor+1, len(Settings)-1)
		case "k", "up":
			m.cursor = max(m.cursor-1, 0)
		case "enter", " ", "l", "right":
			switch s.Kind {
			case Bool:
				m.apply(s, map[string]string{"on": "off", "off": "on"}[m.values[s.Key]])
			case Choice, Preset:
				m.apply(s, cycle(s.Choices, m.values[s.Key], 1))
			default:
				return m, m.edit(s)
			}
		case "h", "left":
			switch s.Kind {
			case Choice, Preset:
				m.apply(s, cycle(s.Choices, m.values[s.Key], -1))
			case Bool:
				m.apply(s, map[string]string{"on": "off", "off": "on"}[m.values[s.Key]])
			}
		case "e":
			return m, m.edit(s)
		case "r":
			if err := Reset(s); err != nil {
				m.flash, m.flashBad = err.Error(), true
				break
			}
			old := m.values[s.Key]
			m.values[s.Key] = s.Default
			m.flash = s.Label + " reset to default"
			m.afterChange(s, old)
		}
	}
	return m, nil
}

func (m *uiModel) edit(s Setting) tea.Cmd {
	m.editing = true
	m.input.SetValue(m.values[s.Key])
	m.input.CursorEnd()
	return m.input.Focus()
}

func (m *uiModel) apply(s Setting, value string) {
	old := m.values[s.Key]
	v, err := Set(s, value)
	if err != nil {
		m.flash, m.flashBad = err.Error(), true
		return
	}
	m.values[s.Key] = v
	m.flash, m.flashBad = "saved · "+s.Label+" = "+display(v), false
	m.afterChange(s, old)
}

func (m *uiModel) afterChange(s Setting, old string) {
	if s.Reload {
		if err := ReloadKeys(old); err == nil {
			m.flash += " · key rebound"
		}
	}
}

// ReloadKeys re-runs the plugin so key bindings pick up new keys, after
// unbinding the old one.
func ReloadKeys(oldKey string) error {
	if oldKey != "" {
		tmux.RunTmux("unbind", oldKey)
	}
	bin, ok := tmux.GetOption("@agent_sidebar_bin")
	if !ok {
		return fmt.Errorf("plugin not loaded")
	}
	entry := filepath.Join(filepath.Dir(filepath.Dir(bin)), "assets", "tmux-agent.tmux")
	if _, err := os.Stat(entry); err != nil {
		return err
	}
	_, err := tmux.RunTmuxCapture("run-shell", entry)
	return err
}

func cycle(choices []string, cur string, step int) string {
	for i, c := range choices {
		if c == cur {
			return choices[(i+step+len(choices))%len(choices)]
		}
	}
	return choices[0]
}

func display(v string) string {
	if v == "" {
		return "—"
	}
	return v
}

func (m uiModel) View() string {
	if m.width == 0 {
		return ""
	}
	fg := func(c lipgloss.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }
	dim, accent := fg(m.theme.TextDim), fg(m.theme.Accent).Bold(true)
	title := lipgloss.NewStyle().Background(m.theme.Accent).Foreground(lipgloss.Color("0")).Bold(true).Render(" ⚙ tmux-agent settings ")
	lines := []string{title + dim.Render("  "+ConfigFile()), ""}

	labelW := 32
	var body []string
	selLine := 0
	group := ""
	for i, s := range Settings {
		if s.Group != group {
			group = s.Group
			if len(body) > 0 {
				body = append(body, "")
			}
			body = append(body, fg(m.theme.SectionTitle).Bold(true).Render(group))
		}
		if i == m.cursor {
			selLine = len(body)
		}
		bar, label := "  ", fg(m.theme.TextPrimary)
		if i == m.cursor {
			bar, label = accent.Render("▌ "), accent
		}
		v := m.values[s.Key]
		val := fg(m.theme.TextPrimary).Render(display(v))
		switch s.Kind {
		case Bool:
			val = fg(map[string]lipgloss.Color{"on": m.theme.StatusRunning, "off": m.theme.TextDim}[v]).Bold(true).Render(map[string]string{"on": "● on", "off": "○ off"}[v])
		case Choice, Preset:
			val = dim.Render("‹ ") + fg(m.theme.TextPrimary).Bold(true).Render(display(v)) + dim.Render(" ›")
		}
		if i == m.cursor && m.editing {
			val = m.input.View()
		}
		mark := ""
		if v != s.Default && !(v == "" && s.Default == "") {
			mark = fg(m.theme.StatusDone).Render(" •")
		}
		body = append(body, bar+label.Render(fmt.Sprintf("%-*s", labelW, ansi.Truncate(s.Label, labelW, "…")))+val+mark)
	}

	footerH := 4
	room := m.height - len(lines) - footerH
	if room < 3 {
		room = 3
	}
	start := 0
	if selLine >= room {
		start = selLine - room + 1
	}
	end := min(len(body), start+room)
	lines = append(lines, body[start:end]...)
	for len(lines) < m.height-footerH {
		lines = append(lines, "")
	}

	s := m.sel()
	help := s.Key
	if s.Help != "" {
		help += " — " + s.Help
	}
	if s.Default != "" {
		help += " · default " + s.Default
	}
	if s.Reload {
		help += " · rebinds the key"
	}
	flash := ""
	if m.flash != "" {
		c := m.theme.StatusRunning
		if m.flashBad {
			c = m.theme.StatusError
		}
		flash = fg(c).Render(m.flash)
	}
	keys := "j/k move · enter/space toggle · ←/→ choose · e edit · r reset · q close"
	if m.editing {
		keys = "enter save · esc cancel"
	}
	lines = append(lines, "", dim.Render(ansi.Truncate(help, m.width, "…")), flash, accent.Render(ansi.Truncate(keys, m.width, "…")))
	return strings.Join(lines, "\n")
}
