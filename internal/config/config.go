// Package config is tmux-agent's central configuration: one registry of
// every user setting (group, kind, choices, default, help), shared by
// `tmux-agent config list|get|set|reset` and the settings screen (ui.go).
//
// Settings are tmux global options. A change is applied to the running
// server at once (`set -g`) and persisted to ConfigFile — tmux syntax,
// sourced by assets/tmux-agent.tmux before the plugin seeds its defaults —
// so it survives a tmux restart.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"tmux-agent/internal/tmux"
)

// Kind is how a setting is edited.
type Kind int

const (
	Bool   Kind = iota // on / off
	Choice             // one of Choices
	Preset             // one of Choices, or any text
	Int                // a number in [Min, Max]
	Text               // free text
)

// Setting describes one option.
type Setting struct {
	Key, Group, Label, Help string
	Kind                    Kind
	Default                 string
	Choices                 []string
	Min, Max                int
	// Reload: only takes effect after the plugin reloads (key bindings).
	Reload bool
}

// Settings is the registry, in display order.
var Settings = []Setting{
	{Key: "@agent_avatar", Group: "Avatar", Label: "Character", Kind: Choice, Default: "slime",
		Choices: []string{"slime", "cat", "sprout", "ghost", "robot", "off"}, Help: "the sidebar avatar; off hides it"},
	{Key: "@agent_digest_llm", Group: "Avatar", Label: "LLM for its lines", Kind: Preset, Default: "ollama run --nowordwrap --think=false qwen3:0.6b",
		Choices: []string{"ollama run --nowordwrap --think=false qwen3:0.6b", "ollama run --nowordwrap --think=false --keepalive 30m gemma4:e2b", "claude -p --model haiku", "off"},
		Help:    "rewrites the avatar's line on every agent event (prompt = last argument); a small local model is enough; off = built-in phrases"},
	{Key: "@agent_avatar_personality", Group: "Avatar", Label: "Personality", Kind: Preset, Default: "cheerful, cute and encouraging",
		Choices: []string{"cheerful, cute and encouraging", "anime tsundere: grumbles but secretly cares", "lazy office cat, always sleepy",
			"witty and lightly sarcastic", "serious and brief, like a professional assistant", "adventurous pirate", "reliable senpai"},
		Help: "how the avatar talks; pick one or type your own description"},
	{Key: "@agent_avatar_llm", Group: "Avatar", Label: "LLM for dashboard summaries", Kind: Preset, Default: "claude -p --model haiku",
		Choices: []string{"claude -p --model haiku", "claude -p --model sonnet", "codex exec", "gemini -p", "off"},
		Help:    "one-line task summaries in the dashboard (prompt = last argument); off = none"},
	{Key: "@agent_avatar_images", Group: "Avatar", Label: "Picture avatar", Kind: Choice, Default: "auto",
		Choices: []string{"auto", "on", "off"}, Help: "a real image on kitty/Ghostty (auto = when such a client is attached); off = braille"},
	{Key: "@agent_avatar_image", Group: "Avatar", Label: "Your picture / GIF", Kind: Text,
		Help: "image or animated GIF for every mood; the per-mood ones below win"},
	{Key: "@agent_avatar_image_sleep", Group: "Avatar", Label: "Picture: all quiet", Kind: Text, Help: "image / GIF when no agent works"},
	{Key: "@agent_avatar_image_work", Group: "Avatar", Label: "Picture: working", Kind: Text, Help: "image / GIF while agents work"},
	{Key: "@agent_avatar_image_happy", Group: "Avatar", Label: "Picture: done", Kind: Text, Help: "image / GIF when an agent finished"},
	{Key: "@agent_avatar_image_alert", Group: "Avatar", Label: "Picture: needs you", Kind: Text, Help: "image / GIF when an agent waits on you"},
	{Key: "@agent_avatar_image_sad", Group: "Avatar", Label: "Picture: error", Kind: Text, Help: "image / GIF when an agent failed"},
	{Key: "@agent_avatar_image_rows", Group: "Avatar", Label: "Picture height", Kind: Int, Default: "4", Min: 4, Max: 24,
		Help: "lines your picture / GIF takes (2 columns per line); bigger = sharper"},
	{Key: "@agent_avatar_lang", Group: "Avatar", Label: "Language", Kind: Choice, Default: "en", Choices: []string{"vi", "en"}},
	{Key: "@agent_avatar_lines", Group: "Avatar", Label: "Speech lines", Kind: Int, Default: "1", Min: 1, Max: 8},
	{Key: "@agent_done_prompt", Group: "Avatar", Label: "Prompt for a finished agent", Kind: Text,
		Help: "quick action (space / prefix+a) sends this to an agent that finished; empty = jump to it"},
	{Key: "@agent_avatar_say_waiting", Group: "Avatar", Label: "Own line: waiting", Kind: Text, Help: "{name} {action}; overrides the LLM"},
	{Key: "@agent_avatar_say_done", Group: "Avatar", Label: "Own line: done", Kind: Text, Help: "{name}; overrides the LLM"},
	{Key: "@agent_avatar_say_error", Group: "Avatar", Label: "Own line: error", Kind: Text, Help: "{name} {action}; overrides the LLM"},

	{Key: "@sidebar_width", Group: "Sidebar", Label: "Width", Kind: Text, Default: "15%", Help: "columns or % of the window"},
	{Key: "@sidebar_position", Group: "Sidebar", Label: "Position", Kind: Choice, Default: "left", Choices: []string{"left", "right"}},
	{Key: "@sidebar_auto_create", Group: "Sidebar", Label: "Auto-create in new windows", Kind: Bool, Default: "on"},
	{Key: "@agent_theme", Group: "Sidebar", Label: "Theme", Kind: Choice, Default: "dark", Choices: []string{"dark", "light"}},

	{Key: "@agent-sidebar-default-agent", Group: "Agents", Label: "Default agent for `new`", Kind: Preset, Default: "claude",
		Choices: []string{"claude", "codex", "pi", "opencode", "gemini", "copilot", "amp"}},
	{Key: "@agent_max_panes", Group: "Agents", Label: "Max agents per window", Kind: Int, Default: "4", Min: 1, Max: 12},
	{Key: "@agent_scrape", Group: "Agents", Label: "Screen detection", Kind: Bool, Default: "on", Help: "status for agents without hooks"},
	{Key: "@agent_checkpoints", Group: "Agents", Label: "Checkpoint before each turn", Kind: Bool, Default: "on",
		Help: "snapshot the repo before every agent turn so `tmux-agent undo <agent>` can roll it back"},
	{Key: "@agent_stuck_minutes", Group: "Agents", Label: "Stuck after (minutes)", Kind: Int, Default: "10", Min: 1, Max: 240},

	{Key: "@agent_team", Group: "Team", Label: "Workers", Kind: Text,
		Help: "names for `tmux-agent team`, e.g. \"w1 w2 rev@reviewer\" (name@role)"},
	{Key: "@agent_team_autostart", Group: "Team", Label: "Start team in new sessions", Kind: Bool, Default: "off",
		Help: "each new tmux session starts the workers in its directory"},
	{Key: "@agent_team_prompt", Group: "Team", Label: "Worker prompt", Kind: Text,
		Help: "sent to each worker, {name} = its name; empty = work the task board (TODO @name)"},

	{Key: "@sidebar_notifications", Group: "Notifications", Label: "Desktop notifications", Kind: Bool, Default: "on"},
	{Key: "@agent_notify_url", Group: "Notifications", Label: "Phone push URL", Kind: Text,
		Help: "ntfy topic URL or Telegram sendMessage URL; empty = off"},

	{Key: "@sidebar_key", Group: "Keys", Label: "Toggle sidebar (prefix +)", Kind: Text, Default: "e", Reload: true},
	{Key: "@sidebar_key_all", Group: "Keys", Label: "Toggle all sidebars (prefix +)", Kind: Text, Default: "E", Reload: true},
	{Key: "@agent_watch_key", Group: "Keys", Label: "Dashboard (prefix +)", Kind: Text, Default: "A", Reload: true},
	{Key: "@agent_action_key", Group: "Keys", Label: "Quick action (prefix +)", Kind: Text, Default: "a", Reload: true},
}

// Find returns the setting for key: the full option (@agent_avatar), or
// without the "@", "@agent_" or "@sidebar_" prefix (agent_avatar, avatar).
func Find(key string) (Setting, bool) {
	key = strings.TrimPrefix(key, "@")
	for _, s := range Settings {
		k := strings.TrimPrefix(s.Key, "@")
		if k == key || "agent_"+key == k || "sidebar_"+key == k {
			return s, true
		}
	}
	return Setting{}, false
}

// Get returns a setting's current value (its default when unset).
func Get(s Setting) string {
	if v, ok := tmux.GetOption(s.Key); ok {
		return v
	}
	return s.Default
}

// Validate normalizes value for s, or says why it's not allowed.
func Validate(s Setting, value string) (string, error) {
	value = strings.TrimSpace(value)
	switch s.Kind {
	case Bool:
		switch strings.ToLower(value) {
		case "on", "true", "yes", "1":
			return "on", nil
		case "off", "false", "no", "0":
			return "off", nil
		}
		return "", fmt.Errorf("%s: want on or off", s.Key)
	case Choice:
		for _, c := range s.Choices {
			if c == value {
				return value, nil
			}
		}
		return "", fmt.Errorf("%s: want one of %s", s.Key, strings.Join(s.Choices, ", "))
	case Int:
		n, err := strconv.Atoi(value)
		if err != nil || n < s.Min || n > s.Max {
			return "", fmt.Errorf("%s: want a number %d–%d", s.Key, s.Min, s.Max)
		}
		return strconv.Itoa(n), nil
	}
	return value, nil
}

// Set validates, applies to the running tmux, and persists.
func Set(s Setting, value string) (string, error) {
	v, err := Validate(s, value)
	if err != nil {
		return "", err
	}
	if v == "" && s.Kind == Text {
		tmux.RunTmux("set", "-gu", s.Key)
	} else if _, err := tmux.RunTmuxCapture("set", "-g", s.Key, v); err != nil {
		return "", err
	}
	return v, persist(s.Key, &v)
}

// Reset drops the setting back to its default, everywhere.
func Reset(s Setting) error {
	if s.Default != "" {
		tmux.RunTmux("set", "-g", s.Key, s.Default)
	} else {
		tmux.RunTmux("set", "-gu", s.Key)
	}
	return persist(s.Key, nil)
}

// ConfigFile is where settings persist.
func ConfigFile() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "tmux-agent", "config.conf")
}

const fileHeader = "# tmux-agent settings — written by `tmux-agent config` (tmux syntax).\n# Loaded by the plugin before its defaults; edit freely.\n"

// persist rewrites ConfigFile with key set to *value (nil = removed),
// keeping every other line.
func persist(key string, value *string) error {
	path := ConfigFile()
	var lines []string
	if f, err := os.Open(path); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			l := sc.Text()
			if f := strings.Fields(l); len(f) >= 3 && (f[0] == "set" || f[0] == "set-option") && f[2] == key {
				continue
			}
			lines = append(lines, l)
		}
		f.Close()
	}
	if len(lines) == 0 {
		lines = strings.Split(strings.TrimRight(fileHeader, "\n"), "\n")
	}
	if value != nil {
		lines = append(lines, fmt.Sprintf("set -g %s %s", key, quote(*value)))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

// quote makes value one tmux word.
func quote(v string) string {
	return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
}
