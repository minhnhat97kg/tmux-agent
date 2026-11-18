package sidebar

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"tmux-agent/internal/tmux"
)

// `tmux-agent digest [--auto]`: what the avatar says, written by a small
// local model from the agents' state — who needs you, who is working on
// what, or that all is quiet. Agent hooks run it on every event (prompt
// submitted, stop, failure, notification); the text lands in @agent_digest
// with @agent_digest_for = the state it describes, and the bubble shows it
// while that state holds (the built-in phrases only fill the gap until the
// model answers).
//
// --auto skips when the state is the one already described or being asked.

const (
	digestLLMOption   = "@agent_digest_llm"
	digestOption      = "@agent_digest"
	digestForOption   = "@agent_digest_for"
	digestAskOption   = "@agent_digest_asking"
	defaultDigestLLM  = "ollama run --nowordwrap --think=false qwen3:0.6b"
	digestMaxRunes    = 240
	digestDetailRunes = 150
	personalityOption = "@agent_avatar_personality"
	// DefaultPersonality is the avatar's character when none is set.
	DefaultPersonality = "cheerful, cute and encouraging"
	digestQuietWait    = 10 * time.Second
)

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		s = string(r[:n])
	}
	return s
}

// digestFacts states the agents' situation as plain sentences (small
// models echo list-shaped input and copy few-shot examples, so neither is
// used). Only things that hold for a whole run go in — not the current
// tool — so the facts, and their key, change only on agent events.
func digestFacts(sessions []tmux.SessionInfo, alerts []Alert, c AvatarConfig) string {
	state := map[string]string{"waiting": "is waiting for your approval", "error": "hit an error", "conflict": "edits a file another agent edits",
		"stuck": "looks stuck", "done": "finished, last reply"}
	var b strings.Builder
	alerted := map[string]bool{}
	for _, a := range alerts {
		alerted[a.PaneID] = true
		detail := a.Action
		if a.Kind == "done" {
			detail = a.Reply
		}
		fmt.Fprintf(&b, "Agent %q %s: %s\n", a.Name, state[a.Kind], clip(detail, digestDetailRunes))
	}
	for _, s := range sessions {
		for _, w := range s.Windows {
			for _, p := range w.Panes {
				if !p.Status.IsActive() || alerted[p.PaneID] {
					continue
				}
				task := ""
				if !p.PromptIsResponse {
					task = p.Prompt
				}
				if task = clip(task, digestDetailRunes); task == "" {
					fmt.Fprintf(&b, "Agent %q is working.\n", alertName(p))
					continue
				}
				fmt.Fprintf(&b, "Agent %q is working on: %s\n", alertName(p), task)
			}
		}
	}
	if b.Len() == 0 {
		// The hour is in the facts so a quiet afternoon gets a new line
		// every hour, and the model can greet the time of day.
		return fmt.Sprintf("%sIt is %d o'clock.\n", quietFacts, digestNow().Hour())
	}
	return b.String()
}

const quietFacts = "No agent is running. "

var digestNow = time.Now

// One is picked at random for each quiet line, so they don't all sound alike.
var quietTopics = []string{
	"greet the time of day correctly", "a joke about bugs", "a short tmux tip", "a reminder to drink water or stretch",
	"praise the user for working hard", "a joke about deadlines", "invite them to give the agents new work", "a fun programming fact",
	"a joke about coffee and code", "encouragement for a long day",
}

func digestKey(facts string, c AvatarConfig) string {
	sum := sha1.Sum([]byte(c.Lang + "\x00" + c.Personality + "\x00" + facts))
	return hex.EncodeToString(sum[:8])
}

// digestPrompt asks for one line in the avatar's language (the facts and
// instructions are English; the answer is not).
func digestPrompt(facts string, c AvatarConfig) string {
	lang := map[string]string{"vi": "Vietnamese", "en": "English"}[c.Lang]
	if lang == "" {
		lang = "English"
	}
	who := "You are a tiny mascot in a tmux sidebar watching the user's coding agents. Your personality: " + c.Personality + ". Stay in character.\n"
	rules := " At most one emoji, no quotes. Answer in " + lang + " only."
	if strings.HasPrefix(facts, quietFacts) {
		return who + facts + "Say exactly 1 short, fun sentence (under 25 words) to the user. Topic: " + quietTopics[rand.IntN(len(quietTopics))] +
			". Don't start with a greeting unless the topic is one." + rules
	}
	return who + "Agent status:\n" + facts + "\nWrite exactly 1 short sentence (under 30 words) to the user about this, most urgent first. No list, no bullets." + rules
}

// cleanDigest joins the answer into one line, drops markdown, caps it.
// Emoji stay: the bubble wraps by grapheme width, which tmux agrees with.
func cleanDigest(out string) string {
	s := strings.Join(strings.Fields(strings.NewReplacer("*", "", "`", "", "#", "").Replace(ansi.Strip(out))), " ")
	for _, label := range []string{"Summary:", "Tóm tắt:"} { // Vietnamese "Summary:"
		s = strings.TrimSpace(strings.TrimPrefix(s, label))
	}
	s = strings.Trim(s, `"'“”`)
	if r := []rune(s); len(r) > digestMaxRunes {
		s = string(r[:digestMaxRunes-1]) + "…"
	}
	return s
}

// digestFor returns the stored digest if it still describes the agents —
// only while it describes the current state.
func (c AvatarConfig) digestFor(sessions []tmux.SessionInfo, alerts []Alert) string {
	key := digestKey(digestFacts(sessions, alerts, c), c)
	if c.Digest == "" || c.DigestFor != key {
		return ""
	}
	return c.Digest
}

// Digest is the `digest` command.
func Digest(args []string, stdout, stderr io.Writer) int {
	auto := len(args) > 0 && args[0] == "--auto"
	// ponytail: one writer across tmux servers; use per-server locks if needed.
	dir := llmCacheDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 1
	}
	f, err := os.OpenFile(filepath.Join(dir, "digest.lock"), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return 1
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return 0
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	for {
		changed, result := digestOnce(auto, stdout, stderr)
		if !changed {
			return result
		}
	}
}

func digestOnce(auto bool, stdout, stderr io.Writer) (bool, int) {
	opts := tmux.GetAllGlobalOptions()
	c := avatarConfigFromOptions(opts)
	sessions := tmux.QuerySessions()
	facts := digestFacts(sessions, Alerts(sessions), c)
	key := digestKey(facts, c)
	if opts[digestForOption] == key && c.Digest != "" {
		if !auto {
			fmt.Fprintln(stdout, c.Digest)
		}
		return false, 0
	}
	command := strings.TrimSpace(opts[digestLLMOption])
	if command == "" {
		command = defaultDigestLLM
	}
	if command == "off" || !commandExists(command) {
		return false, 0 // the built-in phrases stay; no failing call per event
	}
	tmux.SetOption(digestAskOption, key) // the bubble keeps the old line meanwhile
	// All quiet: wait a bit first — an agent that starts again right away
	// (the next prompt, a background task) would make the fun line moot.
	if strings.HasPrefix(facts, quietFacts) {
		time.Sleep(digestQuietWait)
		now := tmux.QuerySessions()
		if digestKey(digestFacts(now, Alerts(now), c), c) != key {
			tmux.SetOption(digestAskOption, "")
			return true, 0
		}
	}
	// A line in the wrong script (foreignScript) is dropped and asked
	// once more.
	var text string
	var err error
	for try := 0; try < 2 && (try == 0 || foreignScript(text)); try++ {
		var out string
		out, err = runLLMRaw(command, digestPrompt(facts, c))
		text = cleanDigest(out)
	}
	if foreignScript(text) {
		text = ""
	}
	if err != nil || text == "" {
		tmux.SetOption(digestAskOption, "")
		fmt.Fprintln(stderr, "digest: no answer from", command, err)
		return false, 1
	}
	// Runs overlap when events come fast: only the one whose state is still
	// current gets to write, so the text never pairs with another state.
	now := tmux.QuerySessions()
	if digestKey(digestFacts(now, Alerts(now), c), c) == key {
		tmux.SetOption(digestOption, text)
		tmux.SetOption(digestForOption, key)
	} else {
		return true, 0
	}
	fmt.Fprintln(stdout, text)
	return false, 0
}

// digestKick asks for a new line when the agents' state changed without an
// agent event (you looked at a finished pane, the hour turned, a reload):
// the hooks cover the rest. Once per state per sidebar; `digest --auto`
// itself makes sure several sidebars still cost one model call.
func (m *Model) digestKick() tea.Cmd {
	if !m.avatar.enabled() {
		return nil
	}
	k := digestKey(digestFacts(m.sessions, Alerts(m.sessions), m.avatar), m.avatar)
	if k == m.avatar.DigestFor || (k == m.digestKicked && time.Since(m.digestKickedAt) < time.Minute) {
		return nil
	}
	m.digestKicked = k
	m.digestKickedAt = time.Now()
	return func() tea.Msg {
		bin, err := os.Executable()
		if err != nil {
			return nil
		}
		exec.Command(bin, "digest", "--auto").Run()
		return refreshRequestMsg{}
	}
}

// commandExists: the command's program is on PATH (a leading VAR=value or
// shell syntax can't be checked — assume it works).
func commandExists(command string) bool {
	prog := strings.Fields(command)[0]
	if strings.ContainsAny(prog, "=$(") {
		return true
	}
	_, err := exec.LookPath(prog)
	return err == nil
}

// stripEmoji drops emoji from text the list scrolls (marquee): the marquee
// measures rune by rune, which splits sequences like ❤️ (❤ + U+FE0F) and
// gets their width wrong — a line one column too wide wraps and shifts the
// sidebar. (The bubble wraps whole graphemes, so emoji are fine there.)
func stripEmoji(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.So, r) || unicode.Is(unicode.Sk, r) && r > 0x2000 || r == 0xFE0F || r == 0x200D || r >= 0x1F000 {
			return -1
		}
		return r
	}, s)
}

// foreignScript: the line has a letter outside the Latin script (Hangul,
// kana, Devanagari… — an anime personality or a 2-bit model slips into
// them); never right for the avatar's languages, vi and en.
func foreignScript(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) && !unicode.Is(unicode.Latin, r) {
			return true
		}
	}
	return false
}
