package watch

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

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

func TestMarkdownStripsMarkers(t *testing.T) {
	m := model{theme: sidebar.DefaultTheme()}
	got := ansi.Strip(strings.Join(m.markdown("You are **qa**.\n- run `go test ./...` then report", 80), "\n"))
	if got != "You are qa.\n• run go test ./... then report" {
		t.Errorf("got %q", got)
	}
}
