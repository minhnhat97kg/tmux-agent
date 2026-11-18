package sidebar

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"tmux-agent/internal/tmux"
)

func TestDigestSkipsWhileAnotherRunIsActive(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	if err := os.MkdirAll(llmCacheDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(llmCacheDir(), "digest.lock"), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	if got := Digest([]string{"--auto"}, io.Discard, io.Discard); got != 0 {
		t.Fatalf("overlapping run returned %d", got)
	}
}

func TestDigestFor(t *testing.T) {
	c := AvatarConfig{Lang: "vi"}
	sessions := []tmux.SessionInfo{{Windows: []tmux.WindowInfo{{Panes: []tmux.PaneInfo{
		{PaneID: "%1", Name: "lead", Status: tmux.StatusRunning, Prompt: "sửa test"},
	}}}}}
	c.Digest, c.DigestFor = "lead đang sửa test", digestKey(digestFacts(sessions, nil, c), c)
	if got := c.digestFor(sessions, nil); got != c.Digest {
		t.Fatalf("same state: got %q", got)
	}
	sessions[0].Windows[0].Panes[0].Status = tmux.StatusIdle
	if got := c.digestFor(sessions, nil); got != "" {
		t.Fatalf("changed state must drop the stale line, got %q", got)
	}
	c.DigestAsking = digestKey(digestFacts(sessions, nil, c), c)
	if got := c.digestFor(sessions, nil); got != "" {
		t.Fatalf("a stale line must not show while a new one is being written, got %q", got)
	}
	if got := cleanDigest("Uống nước nhé! 💧"); got != "Uống nước nhé! 💧" {
		t.Fatalf("emoji stay in the bubble: %q", got)
	}
	if got := stripEmoji("chọn ❤️ 1"); got != "chọn  1" {
		t.Fatalf("list text drops emoji (the marquee splits them): %q", got)
	}
	if got := cleanDigest("**Tóm tắt:**\n  duyệt \x1b[3D\x1b[K  push\n"); got != "duyệt push" {
		t.Fatalf("cleanDigest: %q", got)
	}
}

func TestDigestKickRetriesAfterFailure(t *testing.T) {
	m := Model{avatar: AvatarConfig{Character: "cat"}}
	if m.digestKick() == nil || m.digestKick() != nil {
		t.Fatal("digest should be requested once, without immediate retries")
	}
	m.digestKickedAt = time.Now().Add(-time.Minute)
	if m.digestKick() == nil {
		t.Fatal("digest should retry when the earlier request did not produce a line")
	}
}

// All quiet: the model is asked for a fun line, and the hour is part of the
// state so the line changes through the day.
func TestPersonalityShapesLine(t *testing.T) {
	a := AvatarConfig{Lang: "vi", Personality: "tsundere"}
	b := AvatarConfig{Lang: "vi", Personality: "pirate"}
	facts := digestFacts(nil, nil, a)
	if !strings.Contains(digestPrompt(facts, a), "tsundere") {
		t.Fatal("the personality must reach the prompt")
	}
	if digestKey(facts, a) == digestKey(facts, b) {
		t.Fatal("a new personality must ask for a new line")
	}
}

func TestDigestQuietIsFunAndHourly(t *testing.T) {
	defer func(f func() time.Time) { digestNow = f }(digestNow)
	c := AvatarConfig{Lang: "vi"}
	digestNow = func() time.Time { return time.Date(2026, 1, 1, 9, 0, 0, 0, time.Local) }
	morning := digestFacts(nil, nil, c)
	if p := digestPrompt(morning, c); !strings.Contains(p, "Topic:") || !strings.Contains(p, "9 o'clock") || !strings.Contains(p, "Answer in Vietnamese") {
		t.Fatalf("quiet prompt = %q", p)
	}
	digestNow = func() time.Time { return time.Date(2026, 1, 1, 15, 0, 0, 0, time.Local) }
	if digestKey(digestFacts(nil, nil, c), c) == digestKey(morning, c) {
		t.Fatal("a new hour must ask for a new line")
	}
}

func TestCommandExists(t *testing.T) {
	if !commandExists("sh -c true") || commandExists("no-such-llm-xyz run model") || !commandExists("FOO=1 whatever") {
		t.Fatal("commandExists: sh exists, no-such-llm-xyz doesn't, VAR=… can't be checked")
	}
}

func TestForeignScript(t *testing.T) {
	if !foreignScript("젠장, tmux-agent은") || !foreignScript("ふん、終わった") || !foreignScript("tối ऑप्टi") || foreignScript("Hừ, xong rồi đó 😒 · 100%") {
		t.Fatal("Korean/Japanese lines must be caught, Vietnamese kept")
	}
}
