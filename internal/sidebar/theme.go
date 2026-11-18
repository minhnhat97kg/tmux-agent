// The sidebar's color palette and icons — the new UI (part 6, learned from
// herdr: see spec 2026-09-15-phase6-ui-redesign). Unlike the earlier version
// (a verbatim port of colors.rs/icons.rs from the Rust reference), this IS
// its own Go-specific design: a set of prebuilt palettes selected via
// @agent_theme ("dark" default, "light"), with individual tokens overridable
// via @agent_color_<name>/@agent_icon_<name>. It no longer reads the old
// "@sidebar_color_*"/"@sidebar_icon_*" (see the comment in
// internal/tmux/options.go) — this is a DELIBERATE break of the old color
// contract, already signed off by the project owner.
//
// Still tier 3 of the test strategy (spec part 1 §6): table-driven tests for
// colors, fully decoupled from layout — this file has no dependency on
// model.go/view.go.
package sidebar

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"tmux-agent/internal/tmux"
)

// Theme holds all the colors the sidebar uses, reloaded on every refresh
// (refreshCmd, model.go) so that changing @agent_theme/@agent_color_* while
// running takes effect immediately without restarting the sidebar.
type Theme struct {
	// Accent is used for every "active/focused" affordance: a focused
	// group's header, popup border/title, the "All" badge on the filter bar
	// when selected.
	Accent lipgloss.Color

	TextPrimary lipgloss.Color
	TextMuted   lipgloss.Color
	// TextDim is used for "inactive" parts: an icon/count on a disabled
	// filter, the Unknown status.
	TextDim lipgloss.Color

	// SelectionBg paints the background of the row the keyboard cursor is
	// currently pointing at.
	SelectionBg lipgloss.Color

	// ─── Pane status (tmux.PaneStatus) + two view-only overlays ────
	//
	// StatusConfirm/StatusDone are NOT new PaneStatus values (the
	// @pane_status contract with the hook keeps its original six values) —
	// they are overlays computed at the view layer (Row.NeedsConfirm/
	// JustDone, see view.go) when Waiting requires manual handling by the
	// user (hook.IsPermissionWaitReason) or when a pane just switched to
	// Idle and nobody has seen it yet (@pane_unseen).
	StatusAll        lipgloss.Color
	StatusRunning    lipgloss.Color
	StatusBackground lipgloss.Color
	StatusConfirm    lipgloss.Color
	StatusWaiting    lipgloss.Color
	StatusDone       lipgloss.Color
	StatusIdle       lipgloss.Color
	StatusError      lipgloss.Color
	StatusUnknown    lipgloss.Color

	AgentClaude   lipgloss.Color
	AgentCodex    lipgloss.Color
	AgentOpenCode lipgloss.Color

	Branch      lipgloss.Color
	Port        lipgloss.Color
	DiffAdded   lipgloss.Color
	DiffDeleted lipgloss.Color

	SectionTitle      lipgloss.Color
	ActivityTimestamp lipgloss.Color
	PRLink            lipgloss.Color
}

// idx is shorthand for an xterm-256 color index.
func idx(n int) lipgloss.Color {
	return lipgloss.Color(strconv.Itoa(n))
}

// DarkTheme is the default palette (@agent_theme unset, or set to "dark").
// It inherits most indices from the old palette (already working fine on a
// dark background), adding StatusConfirm (orange-red, separated from
// StatusError so an "agent errored" doesn't get confused with "agent
// waiting for approval") and StatusDone (teal, separated from StatusIdle —
// see herdr reference report section 3).
func DarkTheme() Theme {
	return Theme{
		Accent:            idx(153),
		TextPrimary:       idx(255),
		TextMuted:         idx(252),
		TextDim:           idx(244),
		SelectionBg:       idx(239),
		StatusAll:         idx(111),
		StatusRunning:     idx(114),
		StatusBackground:  idx(108),
		StatusConfirm:     idx(203),
		StatusWaiting:     idx(221),
		StatusDone:        idx(80),
		StatusIdle:        idx(110),
		StatusError:       idx(167),
		StatusUnknown:     idx(244),
		AgentClaude:       idx(174),
		AgentCodex:        idx(141),
		AgentOpenCode:     idx(117),
		Branch:            idx(109),
		Port:              idx(246),
		DiffAdded:         idx(114),
		DiffDeleted:       idx(174),
		SectionTitle:      idx(109),
		ActivityTimestamp: idx(109),
		PRLink:            idx(117),
	}
}

// LightTheme is the palette for a light background (@agent_theme light) —
// same token roles, with darker/heavier indices to keep contrast on a light
// background. The sidebar doesn't paint its own pane background (like
// herdr: sidebar_bg defaults to "keep the terminal background"), so this
// palette ONLY changes text/icon colors, and doesn't change any other
// background-painting behavior besides SelectionBg (the selected row's
// background, which needs to stay dark enough for white/bold text to remain
// readable).
func LightTheme() Theme {
	return Theme{
		Accent:            idx(25),
		TextPrimary:       idx(235),
		TextMuted:         idx(238),
		TextDim:           idx(245),
		SelectionBg:       idx(252),
		StatusAll:         idx(26),
		StatusRunning:     idx(28),
		StatusBackground:  idx(29),
		StatusConfirm:     idx(160),
		StatusWaiting:     idx(136),
		StatusDone:        idx(30),
		StatusIdle:        idx(22),
		StatusError:       idx(124),
		StatusUnknown:     idx(245),
		AgentClaude:       idx(95),
		AgentCodex:        idx(54),
		AgentOpenCode:     idx(24),
		Branch:            idx(23),
		Port:              idx(240),
		DiffAdded:         idx(28),
		DiffDeleted:       idx(124),
		SectionTitle:      idx(23),
		ActivityTimestamp: idx(23),
		PRLink:            idx(24),
	}
}

// themeColorTokens maps a token name (the part after "@agent_color_") to the
// corresponding field pointer in t — the SINGLE SOURCE OF TRUTH for color
// overrides, so applyColorOverrides and the table test (theme_test.go) don't
// duplicate the list and drift apart. Returns a new slice on every call
// since it holds pointers INTO t ITSELF — calling it on a different Theme
// gives different pointers.
func (t *Theme) themeColorTokens() []struct {
	name string
	ptr  *lipgloss.Color
} {
	return []struct {
		name string
		ptr  *lipgloss.Color
	}{
		{"accent", &t.Accent},
		{"text", &t.TextPrimary},
		{"text_muted", &t.TextMuted},
		{"text_dim", &t.TextDim},
		{"selection_bg", &t.SelectionBg},
		{"status_all", &t.StatusAll},
		{"status_running", &t.StatusRunning},
		{"status_background", &t.StatusBackground},
		{"status_confirm", &t.StatusConfirm},
		{"status_waiting", &t.StatusWaiting},
		{"status_done", &t.StatusDone},
		{"status_idle", &t.StatusIdle},
		{"status_error", &t.StatusError},
		{"status_unknown", &t.StatusUnknown},
		{"agent_claude", &t.AgentClaude},
		{"agent_codex", &t.AgentCodex},
		{"agent_opencode", &t.AgentOpenCode},
		{"branch", &t.Branch},
		{"port", &t.Port},
		{"diff_added", &t.DiffAdded},
		{"diff_deleted", &t.DiffDeleted},
		{"section_title", &t.SectionTitle},
		{"activity_timestamp", &t.ActivityTimestamp},
		{"pr_link", &t.PRLink},
	}
}

// ThemeFromOptions builds a Theme from an already-read option map (so tests
// don't need a real tmux): pick the prebuilt palette via @agent_theme, then
// override each token present in @agent_color_<name>. An unrecognized token
// name, or a color value that fails to parse, is silently skipped — no
// panic.
func ThemeFromOptions(opts map[string]string) Theme {
	t := themeForName(opts[tmux.AgentTheme])
	for _, f := range t.themeColorTokens() {
		v, ok := opts[tmux.AgentColorPrefix+f.name]
		if !ok {
			continue
		}
		if c, ok := parseTmuxColor(v); ok {
			*f.ptr = c
		}
	}
	return t
}

// themeForName picks a prebuilt palette by name (empty/unrecognized →
// "dark").
func themeForName(name string) Theme {
	if strings.TrimSpace(strings.ToLower(name)) == "light" {
		return LightTheme()
	}
	return DarkTheme()
}

// DefaultTheme is the palette used before the first RefreshMsg arrives
// (NewModel, model.go) — an alias for DarkTheme.
func DefaultTheme() Theme {
	return DarkTheme()
}

// parseTmuxColor accepts an xterm-256 color code (integer 0-255) or
// #RRGGBB (the # is optional). Garbage input returns false, never panics.
func parseTmuxColor(value string) (lipgloss.Color, bool) {
	value = strings.TrimSpace(value)

	if n, err := strconv.ParseUint(value, 10, 8); err == nil {
		return lipgloss.Color(strconv.FormatUint(n, 10)), true
	}

	hex := strings.TrimPrefix(value, "#")
	if len(hex) != 6 {
		return "", false
	}
	if _, err := strconv.ParseUint(hex, 16, 32); err != nil {
		return "", false
	}
	return lipgloss.Color("#" + strings.ToLower(hex)), true
}

// StatusColor maps a RAW pane status to a color — attention=true (a pending
// notification) always overrides to StatusWaiting regardless of the
// underlying status. Knows NOTHING about NeedsConfirm/JustDone (the
// view-only overlays) — see colorForRow in view.go, where that overlay is
// applied on top of this function's result.
func (t Theme) StatusColor(status tmux.PaneStatus, attention bool) lipgloss.Color {
	if attention {
		return t.StatusWaiting
	}
	switch status {
	case tmux.StatusRunning:
		return t.StatusRunning
	case tmux.StatusBackground:
		return t.StatusBackground
	case tmux.StatusWaiting:
		return t.StatusWaiting
	case tmux.StatusIdle:
		return t.StatusIdle
	case tmux.StatusError:
		return t.StatusError
	}
	return t.StatusUnknown
}

// AgentColor maps an agent type to a color.
func (t Theme) AgentColor(agent tmux.AgentType) lipgloss.Color {
	switch agent {
	case tmux.AgentClaude:
		return t.AgentClaude
	case tmux.AgentCodex:
		return t.AgentCodex
	case tmux.AgentOpenCode:
		return t.AgentOpenCode
	}
	return t.StatusUnknown
}

// StatusStyle wraps StatusColor in a lipgloss.Style (fg only).
func (t Theme) StatusStyle(status tmux.PaneStatus, attention bool) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.StatusColor(status, attention))
}

// AgentStyle wraps AgentColor in a lipgloss.Style (fg only).
func (t Theme) AgentStyle(agent tmux.AgentType) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.AgentColor(agent))
}

// SelectionStyle paints the background for the row the cursor has selected.
func (t Theme) SelectionStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(t.SelectionBg)
}

// AccentStyle is used for every "active/focused" affordance — bold, to
// stand out from the regular text around it.
func (t Theme) AccentStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(t.Accent)
}

// Icons holds the status icons, read from @agent_icon_*. Confirm/Done are
// two NEW icons (part 6 UI) for the two view-only overlays NeedsConfirm/
// JustDone — see the StatusConfirm/StatusDone comment on Theme.
type Icons struct {
	All        string
	Running    string
	Background string
	Confirm    string
	Waiting    string
	Done       string
	Idle       string
	Error      string
	Unknown    string

	// Mode/Message/GitBranch/Clock: four icons leading each section of the
	// detail popup (renderDetailPopup, view.go) — added in part 6 after real
	// user feedback "use nerd icons where possible and add color to the
	// text." Unlike the eight icons above (all pane STATUSES), these four
	// icons are SECTION LABELS, each used exactly once per popup — kept
	// separate to make their role clear, so they don't get mixed into the
	// status table.
	Mode      string
	Message   string
	GitBranch string
	Clock     string
}

// DefaultIcons is the default icon set: compact single-cell Unicode for the
// statuses (any font renders them), Nerd Font glyphs only for the section
// labels. Override any of them with @agent_icon_<name> (IconsFromOptions).
func DefaultIcons() Icons {
	return Icons{
		// Status icons: small single-cell Unicode — the earlier Nerd Font
		// circles looked heavy next to the names and could render wide.
		All:        "≡",
		Running:    "●",
		Background: "◐",
		Confirm:    "▲",
		Waiting:    "▲",
		Done:       "✓",
		Idle:       "○",
		Error:      "✕",
		Unknown:    "?",

		Mode:      "\uf013", // nf-fa-cog
		Message:   "\uf075", // nf-fa-comment
		GitBranch: "\uf126", // nf-fa-code_fork
		Clock:     "\uf017", // nf-fa-clock_o
	}
}

// themeIconTokens maps a token name (the part after "@agent_icon_") to the
// corresponding field pointer — the same role as themeColorTokens above.
func (i *Icons) themeIconTokens() []struct {
	name string
	ptr  *string
} {
	return []struct {
		name string
		ptr  *string
	}{
		{"all", &i.All},
		{"running", &i.Running},
		{"background", &i.Background},
		{"confirm", &i.Confirm},
		{"waiting", &i.Waiting},
		{"done", &i.Done},
		{"idle", &i.Idle},
		{"error", &i.Error},
		{"unknown", &i.Unknown},
		{"mode", &i.Mode},
		{"message", &i.Message},
		{"git_branch", &i.GitBranch},
		{"clock", &i.Clock},
	}
}

// IconsFromOptions builds Icons from an already-read option map. An option
// that's unset, or contains only whitespace, falls back to the default.
func IconsFromOptions(opts map[string]string) Icons {
	icons := DefaultIcons()
	for _, f := range icons.themeIconTokens() {
		v, ok := opts[tmux.AgentIconPrefix+f.name]
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		*f.ptr = v
	}
	return icons
}

// AllIcon is the icon for the "All" filter tab on the filter bar.
func (i Icons) AllIcon() string {
	return i.All
}

// StatusIcon maps a RAW pane status to an icon — knows nothing about
// NeedsConfirm/JustDone, see iconForRow in view.go.
func (i Icons) StatusIcon(status tmux.PaneStatus) string {
	switch status {
	case tmux.StatusRunning:
		return i.Running
	case tmux.StatusBackground:
		return i.Background
	case tmux.StatusWaiting:
		return i.Waiting
	case tmux.StatusIdle:
		return i.Idle
	case tmux.StatusError:
		return i.Error
	}
	return i.Unknown
}
