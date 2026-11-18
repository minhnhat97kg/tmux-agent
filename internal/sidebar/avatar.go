package sidebar

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"tmux-agent/internal/tmux"
)

// ─── Avatar ──────────────────────────────────────────────────────────────
//
// A small animated character at the top of the sidebar that watches every
// agent and nudges you to act: when an agent waits on you or finishes
// unseen, it changes mood (each mood its own animation) and says who and
// what, with the quick-action key — space in the sidebar, prefix + a
// anywhere (`tmux-agent next`), or a click on it. Quick action: a waiting
// or failed agent → jump there (answering approvals stays with you); a
// finished agent → send @agent_done_prompt if configured, else jump there.

// avatarHeight is the block's height: 3 art lines + a spacer.
const avatarHeight = 4

// Avatar options (set -g … in tmux.conf).
const (
	avatarOption     = "@agent_avatar"             // slime | cat | sprout | ghost | robot | off             // cat | robot | ghost | off
	donePromptOption = "@agent_done_prompt"        // sent to a finished agent on quick action
	sayWaitingOption = "@agent_avatar_say_waiting" // {name} {action}
	sayDoneOption    = "@agent_avatar_say_done"    // {name}
	sayErrorOption   = "@agent_avatar_say_error"
	linesOption      = "@agent_avatar_lines" // speech bubble content lines   // {name} {action}
)

// AvatarConfig is read from tmux options on every refresh.
type AvatarConfig struct {
	Character    string // "" = default (slime)
	LLM          string // command writing the avatar's lines; "off" = built-in phrases only
	Lang         string // vi | en
	DonePrompt   string
	Lines        int // speech bubble content lines
	SayWaiting   string
	SayDone      string
	SayError     string
	Digest       string // last `tmux-agent digest` (digest.go)
	DigestFor    string // digestKey of the state it describes
	DigestAsking string // digestKey a line is being written for
	Personality  string // how the local model talks (@agent_avatar_personality)
}

func avatarConfigFromOptions(opts map[string]string) AvatarConfig {
	or := func(k, def string) string {
		if v := strings.TrimSpace(opts[k]); v != "" {
			return v
		}
		return def
	}
	return AvatarConfig{
		Character:  or(avatarOption, "slime"),
		LLM:        or(llmOption, defaultLLM),
		Lang:       or(langOption, "en"),
		DonePrompt: strings.TrimSpace(opts[donePromptOption]),
		Lines:      bubbleLinesFrom(opts[linesOption]),
		// Empty = the built-in phrase for the language (avatar_llm.go).
		SayWaiting:   strings.TrimSpace(opts[sayWaitingOption]),
		SayDone:      strings.TrimSpace(opts[sayDoneOption]),
		SayError:     strings.TrimSpace(opts[sayErrorOption]),
		Digest:       opts[digestOption],
		DigestFor:    opts[digestForOption],
		DigestAsking: opts[digestAskOption],
		Personality:  or(personalityOption, DefaultPersonality),
	}
}

func (c AvatarConfig) enabled() bool { return c.Character != "off" }

func bubbleLinesFrom(v string) int {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return defaultBubbleLines
	}
	return min(max(n, minBubbleLines), maxBubbleLines)
}

// bubbleLines is the configured content lines (default when unset/zero).
func (c AvatarConfig) bubbleLines() int {
	if c.Lines == 0 {
		return defaultBubbleLines
	}
	return c.Lines
}

// tallHeight is the full avatar block: art + bubble (borders included) +
// a spacer line.
func (c AvatarConfig) tallHeight() int { return artHeight + c.bubbleLines() + 2 + 1 }

// pictureRows is the art area's height: your picture's rows when the image
// avatar is on, else the sprite's.
func (m Model) pictureRows() int {
	if m.images && m.imgSet.rows > 0 {
		return m.imgSet.rows
	}
	return artRows
}

// tallHeight is the full avatar block for the current art area.
func (m Model) tallHeight() int {
	return m.avatar.tallHeight() - m.avatar.bubbleLines() + m.bubbleRows() - artRows + m.pictureRows()
}

// bubbleRows is how many text lines the bubble needs for what the avatar
// says now: the configured lines at least, more when the line is longer
// (up to maxBubbleLines) — the text is never cut.
func (m Model) bubbleRows() int {
	alerts := Alerts(m.sessions)
	sp := m.speech(currentMood(alerts, m.sessions), alerts)
	return fitBubble(m.avatar.bubbleLines(), len(m.speechLines(sp, max(4, m.width-4))))
}

func fitBubble(configured, needed int) int { return min(max(configured, needed), maxBubbleLines) }

// speechLines word-wraps what the avatar says to inner columns: title
// bright and bold, details muted after it.
func (m Model) speechLines(sp avatarSpeech, inner int) []string {
	text := lipgloss.NewStyle().Foreground(m.theme.TextPrimary).Bold(true)
	muted := lipgloss.NewStyle().Foreground(m.theme.TextMuted)
	parts := []string{text.Render(sp.title)}
	for _, d := range sp.detail {
		parts = append(parts, muted.Render(d))
	}
	return strings.Split(lipgloss.NewStyle().Width(inner).Render(strings.Join(parts, muted.Render(" · "))), "\n")
}

// ─── Alerts: what needs the user, most urgent first ───────────────────────

// Alert is one agent the avatar nudges about.
type Alert struct {
	PaneID string
	Name   string
	Kind   string // "waiting" | "error" | "done"
	Action string // ActionLine for waiting/error
	Reply  string // last reply, for done
	Where  string // "session:index window"
}

func alertName(p tmux.PaneInfo) string {
	if p.Name != "" {
		return p.Name
	}
	if p.Path != "" {
		return filepath.Base(p.Path)
	}
	return p.PaneID
}

// Alerts lists the agents needing the user: waiting first, then failed,
// then watchdog flags (file conflict, stuck), then finished-but-unseen.
func Alerts(sessions []tmux.SessionInfo) []Alert {
	var waiting, failed, flagged, done []Alert
	for _, s := range sessions {
		for _, w := range s.Windows {
			for _, p := range w.Panes {
				where := strings.TrimSpace(s.SessionName + ":" + w.WindowIndex + " " + w.WindowName)
				a := Alert{PaneID: p.PaneID, Name: alertName(p), Action: ActionLine(p), Where: where}
				switch {
				case p.Status == tmux.StatusWaiting:
					a.Kind = "waiting"
					waiting = append(waiting, a)
				case p.Status == tmux.StatusError:
					a.Kind = "error"
					failed = append(failed, a)
				case p.Conflict != "":
					a.Kind = "conflict"
					flagged = append(flagged, a)
				case p.Stuck != "" && p.Status == tmux.StatusRunning:
					a.Kind = "stuck"
					flagged = append(flagged, a)
				case p.Status == tmux.StatusIdle && p.Unseen:
					a.Kind = "done"
					if p.PromptIsResponse {
						a.Reply = p.Prompt
					}
					done = append(done, a)
				}
			}
		}
	}
	return append(append(append(waiting, failed...), flagged...), done...)
}

// Act performs the quick action on alert a and says what it did.
func Act(a Alert, cfg AvatarConfig, fromPane string) string {
	if a.Kind == "done" && cfg.DonePrompt != "" {
		tmux.RunTmux("send-keys", "-t", a.PaneID, "-l", cfg.DonePrompt)
		time.Sleep(300 * time.Millisecond) // TUIs treat a burst as paste
		tmux.RunTmux("send-keys", "-t", a.PaneID, "Enter")
		return fmt.Sprintf("sent to %s: %s", a.Name, cfg.DonePrompt)
	}
	tmux.SelectPane(a.PaneID, fromPane)
	return "jumped to " + a.Name
}

// AvatarConfigFromTmux reads the avatar options (for `tmux-agent next`).
func AvatarConfigFromTmux() AvatarConfig {
	return avatarConfigFromOptions(tmux.GetAllGlobalOptions())
}

func currentMood(alerts []Alert, sessions []tmux.SessionInfo) mood {
	if len(alerts) > 0 {
		switch alerts[0].Kind {
		case "waiting":
			return moodAlert
		case "error", "conflict", "stuck":
			return moodSad
		}
		return moodHappy
	}
	for _, s := range sessions {
		for _, w := range s.Windows {
			for _, p := range w.Panes {
				if p.Status.IsActive() {
					return moodWork
				}
			}
		}
	}
	return moodSleep
}

func say(tpl string, a Alert) string {
	return strings.NewReplacer("{name}", a.Name, "{action}", a.Action).Replace(tpl)
}

// avatarListMin is how many lines the agent list keeps under the full
// avatar; a shorter pane gets the compact avatar instead.
const (
	avatarListMin = 18
	// Speech bubble content lines (@agent_avatar_lines): one per sentence
	// (title, then details); details that don't fit join the last line.
	defaultBubbleLines = 1
	minBubbleLines     = 1
	maxBubbleLines     = 8
)

// avatarSpeech is what the avatar says: a headline, up to two detail lines,
// nothing else (space still acts on it: the footer lists the key). Plain sentences — no counters or status icons
// (user feedback: "talk like a person").
type avatarSpeech struct {
	title  string
	detail []string
}

func (m Model) speech(md mood, alerts []Alert) avatarSpeech {
	c := m.avatar
	// Your own line > the local model's line for the current state
	// (digest.go) > the built-in phrases, which fill in until it answers.
	custom := ""
	if len(alerts) > 0 {
		custom = map[string]string{"waiting": c.SayWaiting, "error": c.SayError, "conflict": c.SayError, "stuck": c.SayError, "done": c.SayDone}[alerts[0].Kind]
	}
	if d := c.digestFor(m.sessions, alerts); d != "" && custom == "" {
		return avatarSpeech{title: d}
	}
	switch md {
	case moodAlert, moodSad, moodHappy:
		a := alerts[0]
		sp := avatarSpeech{}
		where := a.Where
		if where == ":" || where == "" {
			where = a.PaneID
		}
		sp.detail = []string{c.phrase("where", "{where}", where)}
		line := say(c.phrase(a.Kind), a)
		if custom != "" {
			line = say(custom, a)
		}
		sp.title = line
		if d, ok := m.dialogs[a.PaneID]; ok && len(d.Options) > 0 {
			sp.detail = append([]string{c.phrase("answer", "{keys}", d.Options[0].Key+"–"+d.Options[len(d.Options)-1].Key)}, sp.detail...)
		}
		return sp
	case moodWork:
		var busy []string
		for _, s := range m.sessions {
			for _, w := range s.Windows {
				for _, p := range w.Panes {
					if p.Status.IsActive() {
						busy = append(busy, alertName(p))
					}
				}
			}
		}
		return avatarSpeech{title: c.busyLine(busy), detail: []string{c.phrase("work2")}}
	}
	return avatarSpeech{title: c.phrase("sleep"), detail: []string{c.phrase("sleep2")}}
}

// busyLine names who is working the way a person would say it: "console is
// on it", "console and api are on it", "console, api and a few others are on
// it" — never a count.
func (c AvatarConfig) busyLine(names []string) string {
	switch len(names) {
	case 0:
		return c.phrase("workN", "{names}", c.phrase("others"))
	case 1:
		return c.phrase("work1", "{name}", names[0])
	case 2:
		return c.phrase("workN", "{names}", names[0]+c.phrase("and")+names[1])
	}
	return c.phrase("workN", "{names}", names[0]+", "+names[1]+c.phrase("and")+c.phrase("others"))
}

// renderAvatar draws the avatar block, exactly avatarH() lines: the tall
// one (art over a speech bubble) when the pane is tall enough, else the
// compact one (head only, speech beside it).
func (m Model) renderAvatar(width int) []string {
	alerts := Alerts(m.sessions) // shared tmux state only: every window agrees
	md := currentMood(alerts, m.sessions)
	tick := avatarTick()
	f := tick / 3 // ~600ms per frame
	art, props := avatarArt(m.avatar.Character, md, f)
	// kitty/Ghostty: the picture instead of braille, once uploaded (full
	// avatar only — the compact one shows just part of the art).
	tall := m.avatarH() == m.tallHeight()
	artLines, margin, artW := art[:], " ", artCols
	if m.images && tall {
		if id, ok := m.imgSet.frameID(md, tick); ok && m.imgHave[id] {
			// the picture alone, centered: no braille props (z ♪ !) beside it
			artLines, props = placeholderRows(id, m.imgSet.cols, m.imgSet.rows), [artRows]string{}
			margin = strings.Repeat(" ", max(1, (width-m.imgSet.cols)/2))
			artW = m.imgSet.cols
		}
	}
	sp := m.speech(md, alerts)

	color := map[mood]lipgloss.Color{
		moodSleep: m.theme.TextDim, moodWork: m.theme.StatusRunning, moodHappy: m.theme.StatusDone,
		moodAlert: m.theme.StatusConfirm, moodSad: m.theme.StatusError,
	}[md]
	artStyle := lipgloss.NewStyle().Foreground(color)
	propStyle := lipgloss.NewStyle().Foreground(m.theme.Accent)
	clip := lipgloss.NewStyle().MaxWidth(width)

	if !tall {
		return m.renderAvatarCompact(width, art, props, sp, color)
	}

	var out []string
	for r := 0; r < m.pictureRows(); r++ { // braille until the picture is uploaded: blank padding below
		line, prop := "", ""
		if r < len(artLines) {
			line = artLines[r]
		}
		if r < artRows {
			prop = props[r]
		}
		out = append(out, clip.Render(margin+artStyle.Render(line)+propStyle.Render(prop)))
	}
	return append(append(out, m.renderBubble(width, len(margin)+artW/2, sp, color)...), "")
}

// renderBubble draws the speech bubble under the art: a tail pointing up at
// the middle of the avatar (column tail), the headline (icon + bold, mood color), detail lines (muted,
// word-wrapped), and the quick-action key set into the bottom border.
func (m Model) renderBubble(width, tail int, sp avatarSpeech, color lipgloss.Color) []string {
	border := lipgloss.NewStyle().Foreground(color)
	inner := max(4, width-4)
	muted := lipgloss.NewStyle().Foreground(m.theme.TextMuted)

	// The spoken line, word-wrapped; the bubble grows to fit it (bubbleRows),
	// only a line past maxBubbleLines ends in "…".
	lines := m.speechLines(sp, inner)
	content := fitBubble(m.avatar.bubbleLines(), len(lines))
	if len(lines) > content {
		lines = lines[:content]
		lines[content-1] = ansi.Truncate(lines[content-1], inner-1, "") + muted.Render("…")
	}
	for len(lines) < content {
		lines = append(lines, "")
	}

	// The tail sits under the middle of the art / picture.
	tail = min(max(tail, 1), max(1, width-2))
	top := border.Render("╭" + strings.Repeat("─", tail-1) + "┴" + strings.Repeat("─", max(0, width-2-tail)) + "╮")

	bottom := border.Render("╰" + strings.Repeat("─", max(0, width-2)) + "╯")

	out := []string{top}
	for _, l := range lines {
		cell := lipgloss.NewStyle().MaxWidth(inner).Render(l)
		pad := max(0, inner-lipgloss.Width(cell))
		out = append(out, border.Render("│")+" "+cell+strings.Repeat(" ", pad)+" "+border.Render("│"))
	}
	return append(out, bottom)
}

// renderAvatarCompact: head only (art rows 1–3) with the speech beside it,
// for short panes.
func (m Model) renderAvatarCompact(width int, art, props [artRows]string, sp avatarSpeech, color lipgloss.Color) []string {
	artStyle := lipgloss.NewStyle().Foreground(color)
	dim := lipgloss.NewStyle().Foreground(m.theme.TextDim)
	head := lipgloss.NewStyle().Foreground(color).Bold(true)
	room := width - artCols - 2

	speech := [3]string{head.Render(truncateEllipsis(sp.title, room)), "", ""}
	if len(sp.detail) > 0 {
		speech[1] = dim.Render(truncateEllipsis(sp.detail[0], room))
	}
	out := make([]string, 0, avatarHeight)
	for i := 0; i < 3; i++ {
		line := artStyle.Render(art[i+1])
		if room > 3 && speech[i] != "" {
			line += " " + lipgloss.NewStyle().MaxWidth(room).Render(speech[i])
		}
		out = append(out, lipgloss.NewStyle().MaxWidth(width).Render(line))
	}
	return append(out, "")
}

// avatarTick is the animation clock: wall-clock time in animInterval steps,
// so the avatars of every window's sidebar show the same frame at the same
// moment (a per-sidebar counter drifts with each sidebar's start time).
func avatarTick() int {
	return int(time.Now().UnixMilli() / animInterval.Milliseconds())
}
