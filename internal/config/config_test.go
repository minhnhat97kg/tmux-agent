package config

import (
	"os"
	"strings"
	"testing"

	"tmux-agent/internal/tmux"
)

func TestValidate(t *testing.T) {
	avatar, _ := Find("agent_avatar")
	lines, _ := Find("@agent_avatar_lines")
	notif, _ := Find("@sidebar_notifications")
	if v, err := Validate(avatar, "cat"); err != nil || v != "cat" {
		t.Errorf("avatar cat: %q %v", v, err)
	}
	if _, err := Validate(avatar, "dragon"); err == nil {
		t.Error("unknown character must be rejected")
	}
	if _, err := Validate(lines, "20"); err == nil {
		t.Error("lines out of range must be rejected")
	}
	if v, _ := Validate(notif, "yes"); v != "on" {
		t.Errorf("bool normalizes to on/off, got %q", v)
	}
}

func TestSetPersistsAndReset(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	fake := tmux.NewFake()
	for _, args := range [][]string{
		{"set", "-g", "@agent_avatar", "sprout"},
		{"set", "-g", "@agent_avatar_llm", "gemini -p"},
		{"set", "-g", "@agent_avatar", "robot"},
		{"set", "-g", "@agent_avatar", "slime"},
	} {
		fake.Set("", args...)
	}
	defer tmux.SetRunner(fake)()
	avatar, _ := Find("@agent_avatar")
	llm, _ := Find("@agent_avatar_llm")
	if _, err := Set(avatar, "sprout"); err != nil {
		t.Fatal(err)
	}
	if _, err := Set(llm, "gemini -p"); err != nil {
		t.Fatal(err)
	}
	if _, err := Set(avatar, "robot"); err != nil { // replaces, not duplicates
		t.Fatal(err)
	}
	b, _ := os.ReadFile(ConfigFile())
	s := string(b)
	if strings.Count(s, "@agent_avatar ") != 1 || !strings.Contains(s, "set -g @agent_avatar 'robot'") || !strings.Contains(s, "set -g @agent_avatar_llm 'gemini -p'") {
		t.Fatalf("config file:\n%s", s)
	}
	if err := Reset(avatar); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(ConfigFile())
	if strings.Contains(string(b), "@agent_avatar ") || !strings.Contains(string(b), "@agent_avatar_llm") {
		t.Fatalf("after reset:\n%s", b)
	}
}

func TestCycle(t *testing.T) {
	c := []string{"a", "b", "c"}
	if cycle(c, "c", 1) != "a" || cycle(c, "a", -1) != "c" || cycle(c, "zzz", 1) != "a" {
		t.Error("cycle wraps both ways and starts over on unknown values")
	}
}

func TestFindShortNames(t *testing.T) {
	for _, k := range []string{"@agent_avatar", "agent_avatar", "avatar"} {
		if s, ok := Find(k); !ok || s.Key != "@agent_avatar" {
			t.Errorf("Find(%q) = %v %v", k, s.Key, ok)
		}
	}
	if s, ok := Find("width"); !ok || s.Key != "@sidebar_width" {
		t.Errorf("Find(width) = %v", s.Key)
	}
}
