package sidebar

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"tmux-agent/internal/tmux"
)

func TestAlertsMostUrgentFirst(t *testing.T) {
	sessions := []tmux.SessionInfo{{Windows: []tmux.WindowInfo{{Panes: []tmux.PaneInfo{
		{PaneID: "%1", Name: "done-unseen", Status: tmux.StatusIdle, Unseen: true, Prompt: "all green", PromptIsResponse: true},
		{PaneID: "%2", Name: "busy", Status: tmux.StatusRunning},
		{PaneID: "%3", Name: "broke", Status: tmux.StatusError},
		{PaneID: "%4", Name: "asks", Status: tmux.StatusWaiting, WaitReason: "permission_prompt"},
		{PaneID: "%5", Name: "seen", Status: tmux.StatusIdle},
		{PaneID: "%6", Name: "local-done", Status: tmux.StatusIdle, Unseen: true},
	}}}}}
	got := Alerts(sessions)
	var order []string
	for _, a := range got {
		order = append(order, a.Name+":"+a.Kind)
	}
	want := "asks:waiting broke:error done-unseen:done local-done:done"
	if strings.Join(order, " ") != want {
		t.Fatalf("alerts = %v, want %s", order, want)
	}
	if got[0].Action != "needs approval" || got[2].Reply != "all green" {
		t.Errorf("alert details = %+v", got)
	}
}

func TestAvatarArtAnimatesAndKeepsWidth(t *testing.T) {
	for _, name := range characterNames {
		for md := moodSleep; md <= moodSad; md++ {
			seen := map[string]bool{}
			for f := 0; f < 12; f++ {
				art, props := avatarArt(name, md, f)
				for r := range art {
					if w := lipgloss.Width(art[r]); w != artCols {
						t.Errorf("%s mood %d frame %d row %d: art width %d, want %d: %q", name, md, f, r, w, artCols, art[r])
					}
					if w := lipgloss.Width(props[r]); w != propCols {
						t.Errorf("%s mood %d frame %d row %d: props width %d: %q", name, md, f, r, w, props[r])
					}
				}
				seen[strings.Join(art[:], "|")+strings.Join(props[:], "|")] = true
			}
			if len(seen) < 2 {
				t.Errorf("%s mood %d doesn't animate", name, md)
			}
		}
	}
}

// The avatar block is exactly avatarH() lines and never wider than the
// sidebar, tall or compact, in every mood.
func TestRenderAvatarFitsTallAndCompact(t *testing.T) {
	for _, h := range []int{20, 45} {
		for _, st := range []tmux.PaneStatus{tmux.StatusIdle, tmux.StatusRunning, tmux.StatusWaiting, tmux.StatusError} {
			m := NewModel("%s")
			m.width, m.height = 28, h
			m.sessions = []tmux.SessionInfo{{Windows: []tmux.WindowInfo{{Panes: []tmux.PaneInfo{
				{PaneID: "%1", Name: "a-rather-long-agent-name", Status: st, WaitReason: "permission_prompt"},
				{PaneID: "%2", Name: "other", Status: tmux.StatusWaiting},
			}}}}}
			lines := m.renderAvatar(28)
			if len(lines) != m.avatarH() {
				t.Errorf("h=%d %s: %d lines, want %d", h, st, len(lines), m.avatarH())
			}
			for i, l := range lines {
				if w := lipgloss.Width(l); w > 28 {
					t.Errorf("h=%d %s line %d too wide (%d): %q", h, st, i, w, l)
				}
			}
		}
	}
}

func TestQuickActionOnlyWithAlert(t *testing.T) {
	m := NewModel("%s")
	if _, cmd := m.quickAction(); cmd != nil {
		t.Error("no alert: space must do nothing")
	}
	m.sessions = []tmux.SessionInfo{{Windows: []tmux.WindowInfo{{Panes: []tmux.PaneInfo{
		{PaneID: "%4", Status: tmux.StatusWaiting},
	}}}}}
	if _, cmd := m.quickAction(); cmd == nil {
		t.Error("an agent waits: space must act")
	}
	if m.avatar.enabled() != true || (AvatarConfig{Character: "off"}).enabled() {
		t.Error("avatar on by default, off when configured off")
	}
}

func TestBubbleLinesOption(t *testing.T) {
	for v, want := range map[string]int{"": 1, "6": 6, "0": 1, "99": 8, "x": 1} {
		if got := bubbleLinesFrom(v); got != want {
			t.Errorf("bubbleLinesFrom(%q) = %d, want %d", v, got, want)
		}
	}
	m := NewModel("%s")
	m.width, m.height = 28, 60
	m.avatar = AvatarConfig{Lines: 6}
	if got := len(m.renderAvatar(28)); got != m.avatar.tallHeight() || got != 4+6+2+1 {
		t.Errorf("6-line bubble: block is %d lines", got)
	}
}

func TestWatchdogFlagsBecomeAlerts(t *testing.T) {
	sessions := []tmux.SessionInfo{{Windows: []tmux.WindowInfo{{Panes: []tmux.PaneInfo{
		{PaneID: "%1", Name: "done", Status: tmux.StatusIdle, Unseen: true},
		{PaneID: "%2", Name: "slow", Status: tmux.StatusRunning, Stuck: "12"},
		{PaneID: "%3", Name: "clash", Status: tmux.StatusRunning, Conflict: "go.mod with api"},
		{PaneID: "%4", Name: "ok", Status: tmux.StatusRunning},
	}}}}}
	var order []string
	for _, a := range Alerts(sessions) {
		order = append(order, a.Name+":"+a.Kind+":"+a.Action)
	}
	want := "slow:stuck:stuck? no activity for 12m|clash:conflict:editing go.mod with api|done:done:"
	if strings.Join(order, "|") != want {
		t.Errorf("alerts = %v", order)
	}
}

func TestBubbleJoinsOverflowDetail(t *testing.T) {
	m := Model{theme: DefaultTheme()}
	m.avatar.Lines = 1
	out := strings.Join(m.renderBubble(200, 4, avatarSpeech{title: "T", detail: []string{"answer 1–3", "in qa"}}, lipgloss.Color("2")), "\n")
	if !strings.Contains(out, "answer 1–3") || !strings.Contains(out, "in qa") || !strings.Contains(out, "T") {
		t.Errorf("one-line bubble lost text:\n%s", out)
	}
	if n := strings.Count(out, "\n") + 1; n != 3 { // top + 1 line + bottom
		t.Errorf("bubble is %d lines, want 3:\n%s", n, out)
	}
	m.avatar.Lines = 3
	if n := len(m.renderBubble(200, 4, avatarSpeech{title: "T", detail: []string{"a", "b"}}, lipgloss.Color("2"))); n != 5 {
		t.Errorf("3-line bubble is %d lines, want 5", n)
	}
}

// The bubble's tail points at the middle of the art / picture.
func TestBubbleTailFollowsArt(t *testing.T) {
	m := NewModel("%s")
	top := ansi.Strip(m.renderBubble(30, 13, avatarSpeech{title: "T"}, lipgloss.Color("2"))[0])
	if i := strings.IndexRune(top, '┴'); len([]rune(top[:i])) != 13 || lipgloss.Width(top) != 30 {
		t.Fatalf("tail at %d in %q (width %d), want column 13, width 30", len([]rune(top[:i])), top, lipgloss.Width(top))
	}
}

// Emoji (even ❤️ = ❤ + U+FE0F) wrap as whole graphemes: every bubble line
// keeps the exact width, so the pane never wraps and shifts.
func TestBubbleWithEmojiKeepsWidth(t *testing.T) {
	m := NewModel("%s")
	m.avatar.Lines = 3
	for _, l := range m.renderBubble(20, 5, avatarSpeech{title: "Uống nước nhé ❤️ ✨ code tiếp 💧 nào 👨‍💻 bạn ơi"}, lipgloss.Color("2")) {
		if w := lipgloss.Width(l); w != 20 {
			t.Fatalf("bubble line %q is %d wide, want 20", ansi.Strip(l), w)
		}
	}
}

// A long line grows the bubble instead of being cut; only past
// maxBubbleLines does it end in "…".
func TestBubbleGrowsToFit(t *testing.T) {
	m := NewModel("%s")
	m.avatar.Lines = 1
	long := strings.Repeat("chữ dài ", 12) // ~96 runes → 3 lines at 33
	out := m.renderBubble(37, 5, avatarSpeech{title: long}, lipgloss.Color("2"))
	if body := strings.Join(out, "\n"); len(out) != 3+2 || strings.Contains(ansi.Strip(body), "…") {
		t.Fatalf("want 3 text lines and no ellipsis, got %d lines:\n%s", len(out)-2, ansi.Strip(body))
	}
	if fitBubble(1, 20) != maxBubbleLines || fitBubble(3, 1) != 3 {
		t.Fatal("fitBubble: configured lines are the minimum, maxBubbleLines the cap")
	}
}

// The avatar's line can grow between refreshes (the bubble gets taller):
// the frame stays exactly the pane's height, and the next tick resizes the
// list so the whole bubble shows.
func TestFrameKeepsPaneHeightWhenBubbleGrows(t *testing.T) {
	m := NewModel("%s")
	m.width, m.height = 37, 44
	m.avatar = AvatarConfig{Character: "slime", Lines: 1, Lang: "en"}
	m.resizeViewport()
	before := m.avatarH()

	m.avatar.Digest = strings.Repeat("a long line ", 12)
	m.avatar.DigestFor = digestKey(digestFacts(nil, nil, m.avatar), m.avatar)
	if got := strings.Count(m.View(), "\n") + 1; got != m.height {
		t.Fatalf("frame is %d lines, want the pane's %d", got, m.height)
	}
	updated, _ := m.Update(AnimTickMsg{})
	m = updated.(Model)
	if m.avatarH() <= before || m.viewport.Height != m.height-footerHeight-m.avatarH() {
		t.Fatalf("after a tick the list must make room: avatarH %d→%d, viewport %d", before, m.avatarH(), m.viewport.Height)
	}
	if got := strings.Count(m.View(), "\n") + 1; got != m.height {
		t.Fatalf("frame is %d lines after resize, want %d", got, m.height)
	}
}
