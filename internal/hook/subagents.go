package hook

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// lockPane takes an exclusive per-pane lock (flock on a file in the temp
// dir) for a read-compute-write of a pane option that concurrent hooks
// share; call the returned func to release it. If the lock can't be taken
// it returns at once and the caller proceeds unlocked (as before).
func lockPane(pane string) (unlock func()) {
	name := fmt.Sprintf("tmux-agent-%d-pane-%s.lock", os.Getuid(), strings.TrimPrefix(pane, "%"))
	f, err := os.OpenFile(filepath.Join(os.TempDir(), name), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return func() {}
	}
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX) != nil {
		f.Close()
		return func() {}
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}
}

// AppendSubagent adds an entry to the comma-separated @pane_subagents list.
//
// Format of each entry: "agentType:agentID". The id suffix lets
// RemoveSubagent match the exact instance when it stops, and lets the UI
// display a stable "#<first 4 chars of id>" label that doesn't jump around
// when a sibling of the same type stops first.
//
// Pure string function — does NOT read/write tmux itself. The
// read-compute-write loop (read the current @pane_subagents, call
// AppendSubagent, write it back) lives in the handler, under lockPane:
// parallel subagents fire their hooks at the same moment.
func AppendSubagent(current, agentType, agentID string) string {
	entry := agentType + ":" + agentID
	if current == "" {
		return entry
	}
	return current + "," + entry
}

// RemoveSubagent removes the entry with agentID from the comma-separated
// list.
//
// Returns (new list, true) when found and removed (the new list can be an
// empty string if it was the last entry); returns ("", false) when agentID
// isn't present, current is empty, or agentID is empty. Matches by the
// SUFFIX ":agentID", not by position — two subagents of the SAME TYPE
// running in parallel (two "Explore" for instance) must be distinguished by
// id, not by "the last entry of that type", otherwise stopping the FIRST
// subagent would accidentally remove the WRONG (later) one.
//
// An empty agentID is always a no-op (returns false) — there's nothing to
// match, and an empty ":" key would match arbitrarily against any entry
// ending in a colon. For the same reason, this is a pure string function
// that doesn't read/write tmux itself — see the race limitation documented
// on AppendSubagent.
func RemoveSubagent(current, agentID string) (string, bool) {
	if current == "" || agentID == "" {
		return "", false
	}
	needle := ":" + agentID
	items := strings.Split(current, ",")
	idx := -1
	for i, entry := range items {
		if strings.HasSuffix(entry, needle) {
			idx = i
			break
		}
	}
	if idx == -1 {
		return "", false
	}
	// Three-index slice (items[:idx:idx]) to cap capacity — the append below
	// must not overwrite items[idx] while it's still inside the underlying
	// array.
	filtered := append(items[:idx:idx], items[idx+1:]...)
	return strings.Join(filtered, ","), true
}
