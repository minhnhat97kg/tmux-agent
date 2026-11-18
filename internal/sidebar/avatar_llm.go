package sidebar

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ─── What the avatar says ────────────────────────────────────────────────
//
// The avatar's line comes from the local digest model (digest.go); the
// built-in phrases below, in the same friendly tone, fill in until it
// answers. @agent_avatar_llm (default `claude -p --model haiku`) still
// writes the dashboard's one-line task summaries (Summary), cached as files
// shared by every sidebar, with a lock file, so a reply costs one call.

const (
	llmOption  = "@agent_avatar_llm"  // command; "off" disables
	langOption = "@agent_avatar_lang" // vi | en
	defaultLLM = "claude -p --model haiku"
	llmTimeout = 40 * time.Second
	llmMaxLine = 110 // runes
)

// phrases are the built-in lines, per language — the "vi" table is the
// Vietnamese localization (user-facing text, intentionally not English).
// {name}, {action}, {n}, {prompt} are filled in.
var phrases = map[string]map[string]string{
	"vi": {
		"waiting":  "Bạn ơi, {name} đang cần bạn xem giúp nè!",
		"error":    "Ối, {name} gặp lỗi rồi, bạn xem giúp nha?",
		"conflict": "Ối, {name} đang đụng file với agent khác rồi!",
		"stuck":    "Bạn ơi, {name} hình như bị kẹt rồi, xem giúp nhé?",
		"done":     "Bạn ơi, {name} xong việc rồi đó, xem thử nhé?",
		"work1":    "{name} đang làm việc nè",
		"workN":    "{names} đang làm việc nè",
		"and":      " với ",
		"others":   "mấy bạn khác",
		"work2":    "mình canh giúp cho, có gì mình gọi",
		"sleep":    "Yên ắng quá…",
		"sleep2":   "cần gì cứ gọi mình nha",
		"where":    "ở {where}",
		"answer":   "trả lời ngay: bấm {keys} ở danh sách",
	},
	"en": {
		"waiting":  "Hey, {name} needs you for a sec!",
		"error":    "Oops, {name} hit an error — take a look?",
		"conflict": "Heads up, {name} is editing a file another agent edits!",
		"stuck":    "Hmm, {name} looks stuck — take a look?",
		"done":     "Hey, {name} is done — want to check?",
		"work1":    "{name} is on it",
		"workN":    "{names} are on it",
		"and":      " and ",
		"others":   "a few others",
		"work2":    "I'll ping you when they need you",
		"sleep":    "All quiet…",
		"sleep2":   "call me if you need anything",
		"where":    "in {where}",
		"answer":   "answer right here: press {keys} in the list",
	},
}

func (c AvatarConfig) phrase(key string, kv ...string) string {
	table, ok := phrases[c.Lang]
	if !ok {
		table = phrases["en"]
	}
	return strings.NewReplacer(kv...).Replace(table[key])
}

func (c AvatarConfig) llmEnabled() bool { return c.LLM != "" && c.LLM != "off" }

func llmCacheDir() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("tmux-agent-avatar-%d", os.Getuid()))
}

// cachedLine returns the LLM line for key, if one was written.
func cachedLine(key string) (string, bool) {
	b, err := os.ReadFile(filepath.Join(llmCacheDir(), key+".txt"))
	if err != nil || len(b) == 0 {
		return "", false
	}
	return string(b), true
}

// askPrompt runs command on prompt once per key across every sidebar and
// dashboard: the answer is cached as a file; a lock file stops others from
// asking the same thing meanwhile (a stale lock is retried).
func askPrompt(key, command, prompt string) func() {
	if _, ok := cachedLine(key); ok {
		return nil
	}
	dir := llmCacheDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil
	}
	lock := filepath.Join(dir, key+".lock")
	if st, err := os.Stat(lock); err == nil && time.Since(st.ModTime()) > 2*llmTimeout {
		os.Remove(lock)
	}
	f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil // someone else is asking
	}
	f.Close()
	return func() {
		defer os.Remove(lock)
		line, err := runLLM(command, prompt)
		if err == nil && line != "" {
			os.WriteFile(filepath.Join(dir, key+".txt"), []byte(line), 0o600)
		}
	}
}

// Summary is a one-line summary of an agent's final reply (dashboard Task
// panel), written by the avatar's LLM. Not cached yet → it's started in
// the background and ok=false; ask again on the next refresh.
func Summary(reply string, c AvatarConfig) (string, bool) {
	if reply == "" {
		return "", false
	}
	sum := sha1.Sum([]byte("summary\x00" + c.Lang + "\x00" + reply))
	key := "sum-" + hex.EncodeToString(sum[:8])
	if line, ok := cachedLine(key); ok {
		return line, true
	}
	if c.llmEnabled() {
		if cmd := askPrompt(key, c.LLM, summaryPrompt(reply, c)); cmd != nil {
			go cmd()
		}
	}
	return "", false
}

func summaryPrompt(reply string, c AvatarConfig) string {
	lang := map[string]string{"vi": "Vietnamese", "en": "English"}[c.Lang]
	if lang == "" {
		lang = "English"
	}
	if r := []rune(reply); len(r) > 1500 {
		reply = string(r[:1500])
	}
	return fmt.Sprintf(`Summarize in ONE short line (max 80 characters, in %s) what this coding agent accomplished, from its final message below. Plain text, no quotes, no markdown, start with a verb.

%s`, lang, reply)
}

// runLLM runs the configured command with the prompt as its last argument.
// TMUX_PANE is dropped so the agent's own hooks don't report this helper
// run as the sidebar pane's agent.
func runLLM(command, prompt string) (string, error) {
	out, err := runLLMRaw(command, prompt)
	if err != nil {
		return "", err
	}
	return cleanLine(out)
}

// runLLMRaw is runLLM without the one-line cleanup.
func runLLMRaw(command, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), llmTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", command+` "$TMUX_AGENT_AVATAR_PROMPT"`)
	cmd.Dir = os.TempDir()
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "TMUX_PANE=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, "TMUX_AGENT_AVATAR_PROMPT="+prompt)
	out, err := cmd.Output()
	return string(out), err
}

// cleanLine keeps the first non-empty line, unquoted and capped.
func cleanLine(out string) (string, error) {
	for _, l := range strings.Split(out, "\n") {
		l = strings.Trim(strings.TrimSpace(l), "\"'“”`*")
		if l == "" {
			continue
		}
		if r := []rune(l); len(r) > llmMaxLine {
			l = string(r[:llmMaxLine-1]) + "…"
		}
		return l, nil
	}
	return "", errors.New("empty answer")
}
