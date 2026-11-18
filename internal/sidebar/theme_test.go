package sidebar

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

	"tmux-agent/internal/tmux"
)

// ─── Built-in tables: Dark/Light + @agent_theme ───────────────────────────

func TestDefaultThemeIsDarkTheme(t *testing.T) {
	if DefaultTheme() != DarkTheme() {
		t.Fatalf("DefaultTheme() must be an alias of DarkTheme()")
	}
}

// ThemeFromOptions without @agent_theme set must yield exactly DarkTheme.
func TestThemeFromOptionsEmptyFallsBackToDark(t *testing.T) {
	got := ThemeFromOptions(map[string]string{})
	if got != DarkTheme() {
		t.Fatalf("ThemeFromOptions(empty) = %+v, want DarkTheme %+v", got, DarkTheme())
	}
}

func TestThemeFromOptionsSelectsLight(t *testing.T) {
	got := ThemeFromOptions(map[string]string{tmux.AgentTheme: "light"})
	if got != LightTheme() {
		t.Fatalf("ThemeFromOptions(light) = %+v, want LightTheme %+v", got, LightTheme())
	}
	// Case-insensitive, with surrounding whitespace.
	got2 := ThemeFromOptions(map[string]string{tmux.AgentTheme: "  LIGHT  "})
	if got2 != LightTheme() {
		t.Fatalf("ThemeFromOptions(\"  LIGHT  \") must yield LightTheme, got %+v", got2)
	}
}

// An unknown name falls back to Dark, without panicking.
func TestThemeFromOptionsUnknownNameFallsBackToDark(t *testing.T) {
	got := ThemeFromOptions(map[string]string{tmux.AgentTheme: "nebula"})
	if got != DarkTheme() {
		t.Fatalf("ThemeFromOptions(unknown name) must fall back to DarkTheme, got %+v", got)
	}
}

// DarkTheme/LightTheme must differ on EVERY color token — if one token is
// forgotten when the pair is edited, someone who picks "light" on a bright
// background still sees the dark color (a silent bug, no panic, just hard to
// read). Test by comparing the two tables field by field via
// themeColorTokens itself (the single source of truth).
func TestDarkAndLightDifferInEveryToken(t *testing.T) {
	dark := DarkTheme()
	light := LightTheme()
	darkTokens := dark.themeColorTokens()
	lightTokens := light.themeColorTokens()
	if len(darkTokens) != len(lightTokens) {
		t.Fatalf("themeColorTokens() of Dark/Light have different lengths: %d vs %d", len(darkTokens), len(lightTokens))
	}
	for i, f := range darkTokens {
		if *f.ptr == *lightTokens[i].ptr {
			t.Errorf("token %q is identical between Dark and Light (%v)", f.name, *f.ptr)
		}
	}
}

func TestDefaultIconsAreCompact(t *testing.T) {
	icons := DefaultIcons()
	for name, got := range map[string]string{
		"All": icons.All, "Running": icons.Running, "Background": icons.Background, "Confirm": icons.Confirm,
		"Waiting": icons.Waiting, "Done": icons.Done, "Idle": icons.Idle, "Error": icons.Error, "Unknown": icons.Unknown,
	} {
		if lipgloss.Width(got) != 1 || []rune(got)[0] >= 0xE000 && []rune(got)[0] <= 0xF8FF {
			t.Errorf("%s = %q: want one plain (non Nerd Font) cell", name, got)
		}
	}
}

func TestIconsFromOptionsEmptyFallsBackToDefault(t *testing.T) {
	got := IconsFromOptions(map[string]string{})
	if got != DefaultIcons() {
		t.Fatalf("IconsFromOptions(empty) = %+v, want defaults %+v", got, DefaultIcons())
	}
}

// ─── parseTmuxColor: 256-color codes and #RRGGBB ──────────────────────────────────

func TestParseTmuxColor256CodeAndHex(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  lipgloss.Color
	}{
		{"ordinary 256 index", "42", idx(42)},
		{"256 index upper bound", "255", idx(255)},
		{"256 index lower bound", "0", idx(0)},
		{"hex with # prefix", "#1a2b3c", lipgloss.Color("#1a2b3c")},
		{"hex without # prefix", "d0e7ff", lipgloss.Color("#d0e7ff")},
		{"uppercase hex normalized to lowercase", "#ABCDEF", lipgloss.Color("#abcdef")},
		{"surrounded by whitespace", "  42  ", idx(42)},
	}
	for _, c := range cases {
		got, ok := parseTmuxColor(c.input)
		if !ok {
			t.Errorf("%s: parseTmuxColor(%q) = (_, false), want true", c.name, c.input)
			continue
		}
		if got != c.want {
			t.Errorf("%s: parseTmuxColor(%q) = %v, want %v", c.name, c.input, got, c.want)
		}
	}
}

// Garbage input must return ok=false (falling back to defaults at the call
// site), without panicking.
func TestParseTmuxColorGarbageValue(t *testing.T) {
	cases := []string{
		"not a color",
		"#GGG",
		"",
		"   ",
		"256",   // out of the xterm-256 range (0-255)
		"-1",    // negative, invalid
		"#1234", // wrong hex length
		"#12345g",
	}
	for _, in := range cases {
		if _, ok := parseTmuxColor(in); ok {
			t.Errorf("parseTmuxColor(%q) = (_, true), want false (garbage value)", in)
		}
	}
}

// ─── ThemeFromOptions: overriding each token via @agent_color_<name> ─────────────

// Exhaustive: for each color token (themeColorTokens is the source of
// truth), set a valid value different from the default, confirm exactly that
// field changed. The token list is never hardcoded a second time — it is
// pulled straight from themeColorTokens() so it can't drift when tokens are
// added/removed (the same "drift" lesson the project already learned from
// the hooks.json drift test).
func TestThemeFromOptionsOverridesEachToken(t *testing.T) {
	base := DarkTheme()
	tokens := base.themeColorTokens()
	if len(tokens) == 0 {
		t.Fatal("themeColorTokens() is empty")
	}
	for i, f := range tokens {
		value := 100 + i%150 // avoid accidentally matching the default value
		opts := map[string]string{tmux.AgentColorPrefix + f.name: intToStr(value)}
		th := ThemeFromOptions(opts)
		got := *th.themeColorTokens()[i].ptr
		if got != idx(value) {
			t.Errorf("token %q: field = %v, want %v", f.name, got, idx(value))
		}
		// Hex must also work for the same token.
		thHex := ThemeFromOptions(map[string]string{tmux.AgentColorPrefix + f.name: "#010203"})
		if got := *thHex.themeColorTokens()[i].ptr; got != lipgloss.Color("#010203") {
			t.Errorf("token %q = #010203: field = %v, want #010203", f.name, got)
		}
	}
}

func intToStr(n int) string {
	if n > 255 {
		n = n % 256
	}
	digits := []byte{}
	if n == 0 {
		return "0"
	}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// A garbage value on a color token must fall back to the default of the
// currently selected table, without panicking.
func TestThemeFromOptionsGarbageValueFallsBackToDefault(t *testing.T) {
	garbage := []string{"not a color", "#GGG", "", "256", "-1"}
	def := DarkTheme()
	for _, g := range garbage {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("ThemeFromOptions panicked with garbage value %q: %v", g, r)
				}
			}()
			got := ThemeFromOptions(map[string]string{
				tmux.AgentColorPrefix + "accent":       g,
				tmux.AgentColorPrefix + "agent_claude": g,
				tmux.AgentColorPrefix + "status_error": g,
			})
			if got.Accent != def.Accent || got.AgentClaude != def.AgentClaude || got.StatusError != def.StatusError {
				t.Errorf("garbage value %q did not fall back to the default: %+v", g, got)
			}
		}()
	}
}

// An unknown token name (not present in themeColorTokens) is silently ignored.
func TestThemeFromOptionsUnknownTokenIgnored(t *testing.T) {
	got := ThemeFromOptions(map[string]string{
		tmux.AgentColorPrefix + "does_not_exist": "5",
	})
	if got != DarkTheme() {
		t.Fatalf("unknown token must be entirely ignored, got %+v", got)
	}
}

// ─── IconsFromOptions: overrides + garbage ────────────────────────────────────

func TestIconsFromOptionsOverridesEachIcon(t *testing.T) {
	cases := []struct {
		key    string
		want   string
		getter func(Icons) string
	}{
		{tmux.AgentIconPrefix + "all", "∀", func(i Icons) string { return i.All }},
		{tmux.AgentIconPrefix + "running", "◉", func(i Icons) string { return i.Running }},
		{tmux.AgentIconPrefix + "background", "⊙", func(i Icons) string { return i.Background }},
		{tmux.AgentIconPrefix + "confirm", "!!", func(i Icons) string { return i.Confirm }},
		{tmux.AgentIconPrefix + "waiting", "◒", func(i Icons) string { return i.Waiting }},
		{tmux.AgentIconPrefix + "done", "√", func(i Icons) string { return i.Done }},
		{tmux.AgentIconPrefix + "idle", "◌", func(i Icons) string { return i.Idle }},
		{tmux.AgentIconPrefix + "error", "!", func(i Icons) string { return i.Error }},
		{tmux.AgentIconPrefix + "unknown", "∎", func(i Icons) string { return i.Unknown }},
	}
	for _, c := range cases {
		icons := IconsFromOptions(map[string]string{c.key: c.want})
		if got := c.getter(icons); got != c.want {
			t.Errorf("option %s: icon = %q, want %q", c.key, got, c.want)
		}
	}
}

// An empty or whitespace-only icon must fall back to the default, without panicking.
func TestIconsFromOptionsEmptyOrWhitespaceFallsBackToDefault(t *testing.T) {
	def := DefaultIcons()
	garbage := []string{"", "   ", "\t"}
	for _, g := range garbage {
		icons := IconsFromOptions(map[string]string{
			tmux.AgentIconPrefix + "all":     g,
			tmux.AgentIconPrefix + "running": g,
		})
		if icons.All != def.All || icons.Running != def.Running {
			t.Errorf("empty/whitespace icon %q did not fall back to the default: %+v", g, icons)
		}
	}
}

// ─── Status/agent → color and icon mapping (raw — not yet through the view overlay layer) ──

func TestStatusColorMapsEachStatusCorrectly(t *testing.T) {
	th := DefaultTheme()
	cases := []struct {
		status tmux.PaneStatus
		want   lipgloss.Color
	}{
		{tmux.StatusRunning, th.StatusRunning},
		{tmux.StatusBackground, th.StatusBackground},
		{tmux.StatusWaiting, th.StatusWaiting},
		{tmux.StatusIdle, th.StatusIdle},
		{tmux.StatusError, th.StatusError},
		{tmux.StatusUnknown, th.StatusUnknown},
		{tmux.PaneStatus("unknown label"), th.StatusUnknown},
	}
	for _, c := range cases {
		if got := th.StatusColor(c.status, false); got != c.want {
			t.Errorf("StatusColor(%q, false) = %v, want %v", c.status, got, c.want)
		}
	}
}

// attention=true always overrides to status_waiting, regardless of the original status.
func TestStatusColorAttentionOverridesAnyStatus(t *testing.T) {
	th := DefaultTheme()
	for _, s := range []tmux.PaneStatus{
		tmux.StatusRunning, tmux.StatusBackground, tmux.StatusWaiting,
		tmux.StatusIdle, tmux.StatusError, tmux.StatusUnknown,
	} {
		if got := th.StatusColor(s, true); got != th.StatusWaiting {
			t.Errorf("StatusColor(%q, true) = %v, want status_waiting %v", s, got, th.StatusWaiting)
		}
	}
}

func TestAgentColorMapsEachAgentCorrectly(t *testing.T) {
	th := DefaultTheme()
	cases := []struct {
		agent tmux.AgentType
		want  lipgloss.Color
	}{
		{tmux.AgentClaude, th.AgentClaude},
		{tmux.AgentCodex, th.AgentCodex},
		{tmux.AgentOpenCode, th.AgentOpenCode},
		{tmux.AgentUnknown, th.StatusUnknown},
		{tmux.AgentType("unknown label"), th.StatusUnknown},
	}
	for _, c := range cases {
		if got := th.AgentColor(c.agent); got != c.want {
			t.Errorf("AgentColor(%q) = %v, want %v", c.agent, got, c.want)
		}
	}
}

func TestStatusIconMapsEachStatusCorrectly(t *testing.T) {
	icons := DefaultIcons()
	cases := []struct {
		status tmux.PaneStatus
		want   string
	}{
		{tmux.StatusRunning, icons.Running},
		{tmux.StatusBackground, icons.Background},
		{tmux.StatusWaiting, icons.Waiting},
		{tmux.StatusIdle, icons.Idle},
		{tmux.StatusError, icons.Error},
		{tmux.StatusUnknown, icons.Unknown},
		{tmux.PaneStatus("unknown label"), icons.Unknown},
	}
	for _, c := range cases {
		if got := icons.StatusIcon(c.status); got != c.want {
			t.Errorf("StatusIcon(%q) = %q, want %q", c.status, got, c.want)
		}
	}
	if got := icons.AllIcon(); got != icons.All {
		t.Errorf("AllIcon() = %q, want %q", got, icons.All)
	}
}

// ─── lipgloss.Style: fg/bg/bold ───────────────────────────────────────────

func TestStatusStyleSetsCorrectForeground(t *testing.T) {
	th := DefaultTheme()
	style := th.StatusStyle(tmux.StatusError, false)
	if got := style.GetForeground(); got != th.StatusError {
		t.Errorf("StatusStyle(Error).GetForeground() = %v, want %v", got, th.StatusError)
	}
	if style.GetBold() {
		t.Error("StatusStyle must not bold text on its own")
	}
}

func TestAgentStyleSetsCorrectForeground(t *testing.T) {
	th := DefaultTheme()
	style := th.AgentStyle(tmux.AgentCodex)
	if got := style.GetForeground(); got != th.AgentCodex {
		t.Errorf("AgentStyle(Codex).GetForeground() = %v, want %v", got, th.AgentCodex)
	}
}

func TestSelectionStyleSetsCorrectBackground(t *testing.T) {
	th := DefaultTheme()
	style := th.SelectionStyle()
	if got := style.GetBackground(); got != th.SelectionBg {
		t.Errorf("SelectionStyle().GetBackground() = %v, want %v", got, th.SelectionBg)
	}
}

func TestAccentStyleIsBoldWithCorrectColor(t *testing.T) {
	th := DefaultTheme()
	style := th.AccentStyle()
	if got := style.GetForeground(); got != th.Accent {
		t.Errorf("AccentStyle().GetForeground() = %v, want %v", got, th.Accent)
	}
	if !style.GetBold() {
		t.Error("AccentStyle() must be bold")
	}
}

// A custom color theme must be correctly reflected in the Style — not just the default.
func TestStatusStyleWithCustomColorFromOption(t *testing.T) {
	th := ThemeFromOptions(map[string]string{
		tmux.AgentColorPrefix + "status_error": "#ff0000",
	})
	style := th.StatusStyle(tmux.StatusError, false)
	if got := style.GetForeground(); got != lipgloss.Color("#ff0000") {
		t.Errorf("StatusStyle(Error) after changing the option = %v, want #ff0000", got)
	}
}
