package scrape

import (
	"crypto/sha1"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"tmux-agent/internal/activity"
	"tmux-agent/internal/event"
	"tmux-agent/internal/hook"
	"tmux-agent/internal/roster"
	"tmux-agent/internal/sharedctx"
	"tmux-agent/internal/tmux"
	"tmux-agent/internal/usage"
)

// The watchdog runs inside the scraper loop (one per tmux server) every
// watchEvery and flags two things on the panes (@pane_stuck,
// @pane_conflict), notifying once per occurrence:
//
//   - stuck: a running agent with no activity — no new tool call and no
//     screen change — for @agent_stuck_minutes (default 10).
//   - conflict: two agents of the same project edited the same file within
//     conflictWindow; a note goes on the project's shared context too.
//
// ponytail: files are matched by base name (that's what the activity log
// records), so two different dirs with the same file name can false-alarm.

const (
	watchEvery       = 5 * time.Second
	stuckOption      = "@agent_stuck_minutes"
	defaultStuckMins = 10
	conflictWindow   = 10 * time.Minute
	saveEvery        = 30 * time.Second
)

var editTools = map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true}

type watchdog struct {
	lastWatch  time.Time
	lastSave   time.Time
	screenHash map[string][20]byte
	lastActive map[string]time.Time
	reported   map[string]bool // conflict keys already announced
}

func newWatchdog() *watchdog {
	return &watchdog{screenHash: map[string][20]byte{}, lastActive: map[string]time.Time{}, reported: map[string]bool{}}
}

func (w *watchdog) maybeRun(now time.Time) {
	if now.Sub(w.lastWatch) < watchEvery {
		return
	}
	w.lastWatch = now
	sessions := tmux.QuerySessions()
	w.checkStuck(sessions, now)
	w.checkConflicts(sessions, now)
	checkSubagents(sessions)
	if now.Sub(w.lastSave) >= saveEvery { // for `tmux-agent restore`
		w.lastSave = now
		roster.Save(roster.Collect(sessions))
	}
}

func stuckAfter() time.Duration {
	if v, ok := tmux.GetOption(stuckOption); ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Minute
		}
	}
	return defaultStuckMins * time.Minute
}

func (w *watchdog) checkStuck(sessions []tmux.SessionInfo, now time.Time) {
	limit := stuckAfter()
	for _, p := range allPanes(sessions) {
		if p.Status != tmux.StatusRunning {
			delete(w.lastActive, p.PaneID)
			if p.Stuck != "" {
				tmux.UnsetPaneOption(p.PaneID, tmux.PaneStuck)
			}
			continue
		}
		last := w.lastActive[p.PaneID]
		if p.StartedAt > 0 && time.Unix(p.StartedAt, 0).After(last) {
			last = time.Unix(p.StartedAt, 0)
		}
		if mt, ok := activity.LogMtime(p.PaneID); ok && mt.After(last) {
			last = mt
		}
		if out, ok := tmux.RunTmux("capture-pane", "-p", "-t", p.PaneID); ok {
			// Spinners and timers redraw constantly; only count the screen
			// minus digits and spinner glyphs as a change.
			h := sha1.Sum([]byte(stableScreen(out)))
			if old, seen := w.screenHash[p.PaneID]; !seen || old != h {
				w.screenHash[p.PaneID] = h
				if seen {
					last = now
				}
			}
		}
		if last.IsZero() {
			last = now
		}
		w.lastActive[p.PaneID] = last
		idle := now.Sub(last)
		switch {
		case idle >= limit:
			mins := strconv.Itoa(int(idle.Minutes()))
			if p.Stuck == "" {
				hook.Notify(nameOf(p), fmt.Sprintf("may be stuck — no activity for %sm", mins))
			}
			if p.Stuck != mins {
				tmux.SetPaneOption(p.PaneID, tmux.PaneStuck, mins)
			}
		case p.Stuck != "":
			tmux.UnsetPaneOption(p.PaneID, tmux.PaneStuck)
		}
	}
}

// stableScreen drops what animates on its own (digits, spinner glyphs).
func stableScreen(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= '0' && r <= '9', r >= 0x2800 && r <= 0x28FF, strings.ContainsRune("✻✶✳✢·*●◐◓◑◒|/-\\", r):
			return -1
		}
		return r
	}, s)
}

func (w *watchdog) checkConflicts(sessions []tmux.SessionInfo, now time.Time) {
	type editor struct{ pane, name string }
	byFile := map[string][]editor{} // scope|file → editors
	scopeOf := map[string]string{}
	for _, p := range allPanes(sessions) {
		scope := sharedctx.Scope(p.Path)
		seen := map[string]bool{}
		for _, e := range activity.ReadActivityLog(p.PaneID, 40) {
			if !editTools[e.Tool] || e.Label == "" || seen[e.Label] || !recent(e.Timestamp, now) {
				continue
			}
			seen[e.Label] = true
			k := scope + "|" + e.Label
			byFile[k] = append(byFile[k], editor{p.PaneID, nameOf(p)})
			scopeOf[k] = scope
		}
	}
	conflict := map[string]string{} // pane → "<file> with <others>"
	for k, eds := range byFile {
		if len(eds) < 2 {
			continue
		}
		file := k[strings.IndexByte(k, '|')+1:]
		var names []string
		for _, e := range eds {
			names = append(names, e.name)
		}
		sort.Strings(names)
		for _, e := range eds {
			var others []string
			for _, n := range names {
				if n != e.name {
					others = append(others, n)
				}
			}
			conflict[e.pane] = file + " with " + strings.Join(others, ", ")
		}
		if !w.reported[k] {
			w.reported[k] = true
			msg := fmt.Sprintf("⚠ %s are all editing %s — coordinate before editing it further", strings.Join(names, ", "), file)
			sharedctx.Add(scopeOf[k], "tmux-agent", "", msg)
			hook.Notify("file conflict", msg)
		}
	}
	for _, p := range allPanes(sessions) {
		switch c := conflict[p.PaneID]; {
		case c != "" && c != p.Conflict:
			tmux.SetPaneOption(p.PaneID, tmux.PaneConflict, c)
		case c == "" && p.Conflict != "":
			tmux.UnsetPaneOption(p.PaneID, tmux.PaneConflict)
		}
	}
}

// recent: an activity timestamp ("HH:MM", today) within conflictWindow.
func recent(hhmm string, now time.Time) bool {
	t, err := time.ParseInLocation("15:04", hhmm, now.Location())
	if err != nil {
		return false
	}
	t = time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
	if t.After(now.Add(time.Minute)) { // yesterday's late entry
		t = t.AddDate(0, 0, -1)
	}
	return now.Sub(t) <= conflictWindow
}

func allPanes(sessions []tmux.SessionInfo) []tmux.PaneInfo {
	var out []tmux.PaneInfo
	for _, s := range sessions {
		for _, w := range s.Windows {
			out = append(out, w.Panes...)
		}
	}
	return out
}

func nameOf(p tmux.PaneInfo) string {
	if p.Name != "" {
		return p.Name
	}
	return p.PaneID
}

// checkSubagents drops subagents that were stopped without a SubagentStop
// hook — a background subagent killed by the user or by its parent (Task
// stop) never fires one, and would hang in the sidebar tree forever. Its
// transcript then ends with Claude Code's interrupt marker.
func checkSubagents(sessions []tmux.SessionInfo) {
	for _, p := range allPanes(sessions) {
		if len(p.Subagents) == 0 || p.SessionID == "" {
			continue
		}
		raw := tmux.GetPaneOptionValue(p.PaneID, tmux.PaneSubagents)
		for _, entry := range strings.Split(raw, ",") {
			_, id, ok := strings.Cut(entry, ":")
			if ok && id != "" && subagentInterrupted(p.SessionID, id) {
				hook.OnSubagentStop(p.PaneID, event.Event{AgentID: id})
			}
		}
	}
}

func subagentInterrupted(sessionID, agentID string) bool {
	matches, _ := filepath.Glob(filepath.Join(usage.Root(), "*", sessionID, "subagents", "agent-"+agentID+".jsonl"))
	if len(matches) == 0 {
		return false
	}
	return EndsInterrupted(matches[0])
}

// EndsInterrupted: the transcript's last line is Claude Code's
// "[Request interrupted by user…]" message.
func EndsInterrupted(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return false
	}
	const tail = 4096
	off := st.Size() - tail
	if off < 0 {
		off = 0
	}
	buf := make([]byte, st.Size()-off)
	if _, err := f.ReadAt(buf, off); err != nil && err != io.EOF {
		return false
	}
	lines := strings.Split(strings.TrimRight(string(buf), "\n"), "\n")
	return strings.Contains(lines[len(lines)-1], "[Request interrupted by user")
}
