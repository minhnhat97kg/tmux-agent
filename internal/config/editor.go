package config

import (
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"tmux-agent/internal/sidebar"
)

// Editor is the settings list shared by the standalone settings screen
// (ui.go) and the dashboard's Settings tab: the cursor, the current values,
// inline editing of a setting, and the flash line of the last change.
// Changes go through Set / Reset, so they apply to tmux at once and persist
// to ConfigFile.
type Editor struct {
	cursor   int
	values   map[string]string
	editing  bool
	input    textinput.Model
	flash    string
	flashBad bool
}

// NewEditor loads every setting's current value.
func NewEditor() *Editor {
	e := &Editor{values: map[string]string{}}
	e.input = textinput.New()
	e.input.CharLimit = 500
	e.input.Width = 40
	e.Refresh()
	return e
}

// Selected is the setting under the cursor.
func (e *Editor) Selected() Setting { return Settings[e.cursor] }

// Cursor is the index of the selected setting.
func (e *Editor) Cursor() int { return e.cursor }

// Value is a setting's current value as loaded or last set by the editor.
func (e *Editor) Value(key string) string { return e.values[key] }

// Editing reports whether a value is being edited inline.
func (e *Editor) Editing() bool { return e.editing }

// InputView is the in-progress value for the dashboard detail panel.
func (e *Editor) InputView() string { return e.input.View() }

// Flash is the result of the last change, and whether it is an error.
func (e *Editor) Flash() (msg string, bad bool) { return e.flash, e.flashBad }

// Resize adapts the inline input to the screen.
func (e *Editor) Resize(width int) { e.input.Width = max(10, width-30) }

// Refresh reloads every value, so a change made elsewhere (`tmux-agent
// config set`) shows up when the screen opens.
func (e *Editor) Refresh() {
	for _, s := range Settings {
		e.values[s.Key] = Get(s)
	}
}

// Cancel drops an inline edit; it does nothing when not editing.
func (e *Editor) Cancel() {
	if e.editing {
		e.editing = false
		e.input.Blur()
	}
}

// HandleKey applies a key press. quit reports that the screen should close
// (the dashboard leaves it; the standalone screen quits); cmd is the
// textinput's when editing.
func (e *Editor) HandleKey(msg tea.KeyMsg) (quit bool, cmd tea.Cmd) {
	if e.editing {
		switch msg.String() {
		case "esc":
			e.Cancel()
			return false, nil
		case "enter":
			e.editing = false
			e.input.Blur()
			e.apply(e.Selected(), e.input.Value())
			return false, nil
		}
		e.input, cmd = e.input.Update(msg)
		return false, cmd
	}

	e.flash = ""
	s := e.Selected()
	switch msg.String() {
	case "q", "esc", "ctrl+c":
		return true, nil
	case "j", "down":
		e.cursor = min(e.cursor+1, len(Settings)-1)
	case "k", "up":
		e.cursor = max(e.cursor-1, 0)
	case "enter", " ", "l", "right":
		switch s.Kind {
		case Bool:
			e.apply(s, map[string]string{"on": "off", "off": "on"}[e.values[s.Key]])
		case Choice, Preset:
			e.apply(s, cycle(s.Choices, e.values[s.Key], 1))
		default:
			return false, e.edit(s)
		}
	case "h", "left":
		switch s.Kind {
		case Choice, Preset:
			e.apply(s, cycle(s.Choices, e.values[s.Key], -1))
		case Bool:
			e.apply(s, map[string]string{"on": "off", "off": "on"}[e.values[s.Key]])
		}
	case "e":
		return false, e.edit(s)
	case "r":
		if err := Reset(s); err != nil {
			e.flash, e.flashBad = err.Error(), true
			break
		}
		old := e.values[s.Key]
		e.values[s.Key] = s.Default
		e.flash = s.Label + " reset to default"
		e.afterChange(s, old)
	}
	return false, nil
}

func (e *Editor) edit(s Setting) tea.Cmd {
	e.editing = true
	e.input.SetValue(e.values[s.Key])
	e.input.CursorEnd()
	return e.input.Focus()
}

func (e *Editor) apply(s Setting, value string) {
	old := e.values[s.Key]
	v, err := Set(s, value)
	if err != nil {
		e.flash, e.flashBad = err.Error(), true
		return
	}
	e.values[s.Key] = v
	e.flash, e.flashBad = "saved · "+s.Label+" = "+Display(v), false
	e.afterChange(s, old)
}

func (e *Editor) afterChange(s Setting, old string) {
	if s.Reload {
		if err := ReloadKeys(old); err == nil {
			e.flash += " · key rebound"
		}
	}
}

// Body renders the settings list, grouped by group, and returns the index
// of the selected line for scrolling. labelW is the column the labels take.
func (e *Editor) Body(theme sidebar.Theme, labelW int, showValues bool) (lines []string, selected int) {
	fg := func(c lipgloss.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }
	dim, accent := fg(theme.TextDim), fg(theme.Accent).Bold(true)
	group := ""
	for i, s := range Settings {
		if s.Group != group {
			group = s.Group
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, fg(theme.SectionTitle).Bold(true).Render(group))
		}
		if i == e.cursor {
			selected = len(lines)
		}
		bar, label := "  ", fg(theme.TextPrimary)
		if i == e.cursor {
			bar, label = accent.Render("▌ "), accent
		}
		if !showValues {
			lines = append(lines, bar+label.Render(ansi.Truncate(s.Label, labelW, "…")))
			continue
		}
		v := e.values[s.Key]
		val := fg(theme.TextPrimary).Render(Display(v))
		switch s.Kind {
		case Bool:
			val = fg(map[string]lipgloss.Color{"on": theme.StatusRunning, "off": theme.TextDim}[v]).Bold(true).Render(map[string]string{"on": "● on", "off": "○ off"}[v])
		case Choice, Preset:
			val = dim.Render("‹ ") + fg(theme.TextPrimary).Bold(true).Render(Display(v)) + dim.Render(" ›")
		}
		if i == e.cursor && e.editing {
			val = e.input.View()
		}
		mark := ""
		if v != s.Default && !(v == "" && s.Default == "") {
			mark = fg(theme.StatusDone).Render(" •")
		}
		lines = append(lines, bar+label.Render(fmt.Sprintf("%-*s", labelW, ansi.Truncate(s.Label, labelW, "…")))+val+mark)
	}
	return lines, selected
}
