package watch

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"tmux-agent/internal/config"
	"tmux-agent/internal/sidebar"
	"tmux-agent/internal/tmux"
)

func row(id, name string, st tmux.PaneStatus, unseen bool, doing string) agentRow {
	tool, text, _ := strings.Cut(doing, " ")
	return agentRow{PaneInfo: tmux.PaneInfo{PaneID: id, Name: name, Status: st, Unseen: unseen}, Tool: tool, Doing: text}
}

func TestDiffEmitsEvents(t *testing.T) {
	m := model{last: map[string]paneState{}}
	m.diff([]agentRow{row("%1", "rev", tmux.StatusIdle, false, "")})
	if len(m.events) != 0 {
		t.Fatalf("first poll must only prime, got %+v", m.events)
	}
	m.diff([]agentRow{row("%1", "rev", tmux.StatusRunning, false, "Read a.go"), row("%2", "", tmux.StatusIdle, false, "")})
	m.diff([]agentRow{row("%1", "rev", tmux.StatusRunning, false, "Edit a.go"), row("%2", "", tmux.StatusIdle, false, "")})
	m.diff([]agentRow{row("%1", "rev", tmux.StatusIdle, true, "")})

	var got []string
	for _, e := range m.events {
		got = append(got, e.who+": "+e.text)
	}
	want := "rev: idle → running|%2: appeared (, idle)|rev: Edit a.go|rev: running → done|%2: gone"
	if strings.Join(got, "|") != want {
		t.Errorf("events =\n%s\nwant\n%s", strings.Join(got, "|"), want)
	}
}

func TestRolesPanel(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	m := model{theme: sidebar.DefaultTheme(), icons: sidebar.DefaultIcons(), width: 120, height: 30, last: map[string]paneState{}}
	nm, _ := m.updateKeys(tea.KeyMsg{Type: tea.KeyTab})
	nm, _ = nm.(model).updateKeys(tea.KeyMsg{Type: tea.KeyTab}) // agents → board → roles
	m = nm.(model)
	if m.focus != focusRoles || len(m.roles) == 0 {
		t.Fatalf("focus=%d roles=%v", m.focus, m.roles)
	}
	nm, _ = m.updateKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	m = nm.(model)
	out := ansi.Strip(m.View())
	sel := m.roles[1]
	for _, want := range []string{"Roles", "Role · " + sel, "built-in", "start an agent", "edit in $EDITOR"} {
		if !strings.Contains(out, want) {
			t.Errorf("view lacks %q:\n%s", want, out)
		}
	}
}

// moveSetting puts the Settings cursor on setting idx.
func moveSetting(m model, idx int) model {
	for m.editor.Cursor() < idx {
		nm, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		m = nm.(model)
	}
	for m.editor.Cursor() > idx {
		nm, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
		m = nm.(model)
	}
	return m
}

func settingIndex(key string) int {
	for i, s := range config.Settings {
		if s.Key == key {
			return i
		}
	}
	return -1
}

func TestSettingsTab(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()
	m := model{theme: sidebar.DefaultTheme(), icons: sidebar.DefaultIcons(), width: 120, height: 100, last: map[string]paneState{}}

	for i := 0; i < 3; i++ { // agents → board → roles → settings
		nm, _ := m.updateKeys(tea.KeyMsg{Type: tea.KeyTab})
		m = nm.(model)
	}
	if m.focus != focusSettings || m.editor == nil {
		t.Fatalf("focus=%d editor=%v", m.focus, m.editor)
	}

	body, _ := m.editor.Body(m.theme, 40)
	out := ansi.Strip(strings.Join(body, "\n"))
	for _, s := range config.Settings {
		if !strings.Contains(out, s.Label) {
			t.Errorf("settings list lacks %q", s.Label)
		}
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "4 Settings") || !strings.Contains(view, "Setting · ") {
		t.Errorf("view lacks the Settings tab or its detail:\n%s", view)
	}

	// enter on a Bool toggles it and persists through config.Set.
	idx := settingIndex("@agent_avatar_toast")
	s := config.Settings[idx]
	fake.Set("", "set", "-g", s.Key, "off")
	m = moveSetting(m, idx)
	nm, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(model)
	if got := m.editor.Value(s.Key); got != "off" {
		t.Errorf("%s = %q after enter, want off", s.Key, got)
	}
	if flash, _ := m.editor.Flash(); !strings.Contains(flash, "saved") {
		t.Errorf("flash = %q, want a saved line", flash)
	}

	// enter on a Text setting edits inline; keys stay in the input; esc
	// leaves editing without leaving the dashboard.
	idx = settingIndex("@agent_avatar_image")
	m = moveSetting(m, idx)
	nm, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(model)
	if !m.editor.Editing() || m.focus != focusSettings {
		t.Fatalf("editing=%v focus=%d after enter on a Text setting", m.editor.Editing(), m.focus)
	}
	nm, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	m = nm.(model)
	if m.focus != focusSettings || !m.editor.Editing() {
		t.Errorf("typing a digit left the input (focus=%d editing=%v)", m.focus, m.editor.Editing())
	}
	nm, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = nm.(model)
	if m.editor.Editing() || m.focus != focusSettings {
		t.Errorf("esc left editing=%v focus=%d, want editing=false on settings", m.editor.Editing(), m.focus)
	}

	// , jumps to the Settings tab from anywhere.
	m = model{theme: sidebar.DefaultTheme(), icons: sidebar.DefaultIcons(), width: 120, height: 30, last: map[string]paneState{}}
	nm, _ = m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(",")})
	m = nm.(model)
	if m.focus != focusSettings || m.editor == nil {
		t.Errorf(", → focus=%d editor=%v", m.focus, m.editor)
	}
}

func TestMarkdownStripsMarkers(t *testing.T) {
	m := model{theme: sidebar.DefaultTheme()}
	got := ansi.Strip(strings.Join(m.markdown("You are **qa**.\n- run `go test ./...` then report", 80), "\n"))
	if got != "You are qa.\n• run go test ./... then report" {
		t.Errorf("got %q", got)
	}
}
