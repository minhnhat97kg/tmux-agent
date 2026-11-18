package scrape

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecent(t *testing.T) {
	now := time.Date(2026, 11, 17, 15, 30, 0, 0, time.Local)
	for hhmm, want := range map[string]bool{"15:25": true, "15:20": true, "15:19": false, "23:59": false, "bad": false} {
		if got := recent(hhmm, now); got != want {
			t.Errorf("recent(%q) = %v, want %v", hhmm, got, want)
		}
	}
}

func TestStableScreenIgnoresTimersAndSpinners(t *testing.T) {
	a := stableScreen("✻ Thinking… (12s · esc to interrupt) ⠙")
	b := stableScreen("✶ Thinking… (13s · esc to interrupt) ⠹")
	if a != b {
		t.Errorf("a ticking timer/spinner must not count as activity: %q vs %q", a, b)
	}
	if stableScreen("Edit a.go") == stableScreen("Edit b.go") {
		t.Error("real output changes must count")
	}
}

func TestEndsInterrupted(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(body), 0o600)
		return p
	}
	stopped := write("a.jsonl", `{"type":"assistant"}`+"\n"+`{"type":"user","message":{"content":[{"type":"text","text":"[Request interrupted by user]"}]}}`+"\n")
	if !EndsInterrupted(stopped) {
		t.Error("interrupted transcript not detected")
	}
	resumed := write("b.jsonl", `{"text":"[Request interrupted by user]"}`+"\n"+`{"type":"assistant"}`+"\n")
	if EndsInterrupted(resumed) {
		t.Error("resumed transcript flagged as interrupted")
	}
}
