// Package queue holds messages for busy agents: `tmux-agent send` to an
// agent that's working (or waiting on you) queues the text instead of
// typing into the middle of its turn; the scraper loop delivers the next
// message each time the agent is idle again.
//
// One file per message under Dir()/<pane>/, named by time — concurrent
// senders never share a file and the order is the file order.
package queue

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Dir is the root of every pane's queue.
func Dir() string {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, _ := os.UserHomeDir()
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "tmux-agent", "queue")
}

func paneDir(pane string) string {
	return filepath.Join(Dir(), strings.NewReplacer("%", "p", "/", "_").Replace(pane))
}

// Push queues text for pane and returns its position (1 = next) and an id
// for Pending.
func Push(pane, text string) (int, string, error) {
	dir := paneDir(pane)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 0, "", err
	}
	id := filepath.Join(dir, fmt.Sprintf("%020d.txt", time.Now().UnixNano()))
	if err := os.WriteFile(id, []byte(text), 0o600); err != nil {
		return 0, "", err
	}
	return len(files(pane)), id, nil
}

// Pending reports whether the message id is still queued (not delivered).
func Pending(id string) bool {
	_, err := os.Stat(id)
	return err == nil
}

// List returns pane's queued messages, oldest first.
func List(pane string) []string {
	var out []string
	for _, f := range files(pane) {
		if b, err := os.ReadFile(f); err == nil {
			out = append(out, string(b))
		}
	}
	return out
}

// Pop removes and returns the oldest message.
func Pop(pane string) (string, bool) {
	for _, f := range files(pane) {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if os.Remove(f) != nil {
			continue // someone else took it
		}
		return string(b), true
	}
	return "", false
}

// Clear drops pane's queue and says how many messages it held.
func Clear(pane string) int {
	n := len(files(pane))
	os.RemoveAll(paneDir(pane))
	return n
}

// Panes lists the panes that have messages waiting.
func Panes() []string {
	entries, _ := os.ReadDir(Dir())
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			pane := strings.NewReplacer("p", "%").Replace(e.Name()[:1]) + e.Name()[1:]
			if len(files(pane)) > 0 {
				out = append(out, pane)
			}
		}
	}
	return out
}

func files(pane string) []string {
	f, _ := filepath.Glob(filepath.Join(paneDir(pane), "*.txt"))
	sort.Strings(f)
	return f
}
