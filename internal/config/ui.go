package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"tmux-agent/internal/sidebar"
	"tmux-agent/internal/tmux"
)

// RunUI is the settings screen (`tmux-agent config` with no arguments;
// opened in a popup by `,` in the sidebar). It wraps the shared Editor.
func RunUI() int {
	opts := tmux.GetAllGlobalOptions()
	m := uiModel{theme: sidebar.ThemeFromOptions(opts), ed: NewEditor()}
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		return 1
	}
	return 0
}

type uiModel struct {
	theme         sidebar.Theme
	width, height int
	ed            *Editor
}

func (m uiModel) Init() tea.Cmd { return nil }

func (m uiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ed.Resize(msg.Width)
	case tea.KeyMsg:
		quit, cmd := m.ed.HandleKey(msg)
		if quit {
			return m, tea.Quit
		}
		return m, cmd
	}
	return m, nil
}

func (m uiModel) View() string {
	if m.width == 0 {
		return ""
	}
	fg := func(c lipgloss.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }
	dim, accent := fg(m.theme.TextDim), fg(m.theme.Accent).Bold(true)
	title := lipgloss.NewStyle().Background(m.theme.Accent).Foreground(lipgloss.Color("0")).Bold(true).Render(" ⚙ tmux-agent settings ")
	lines := []string{title + dim.Render("  "+ConfigFile()), ""}

	body, selLine := m.ed.Body(m.theme, 32, true)
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

	s := m.ed.Selected()
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
	flash, flashBad := m.ed.Flash()
	flashLine := ""
	if flash != "" {
		c := m.theme.StatusRunning
		if flashBad {
			c = m.theme.StatusError
		}
		flashLine = fg(c).Render(flash)
	}
	keys := "j/k move · enter/space toggle · ←/→ choose · e edit · r reset · q close"
	if m.ed.Editing() {
		keys = "enter save · esc cancel"
	}
	lines = append(lines, "", dim.Render(ansi.Truncate(help, m.width, "…")), flashLine, accent.Render(ansi.Truncate(keys, m.width, "…")))
	return strings.Join(lines, "\n")
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

// Display is a value as shown to the user (an em dash when empty).
func Display(v string) string {
	if v == "" {
		return "—"
	}
	return v
}
