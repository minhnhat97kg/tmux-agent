package hook

import (
	"strconv"
	"strings"

	"tmux-agent/internal/process"
	"tmux-agent/internal/tmux"
)

// paneFromAncestors finds the pane a process runs in when TMUX_PANE isn't
// set: walk up from pid until a process is some pane's root (pane_pid).
// "" when none is (not in tmux, or ps/tmux unavailable).
func paneFromAncestors(pid int) string {
	out, ok := tmux.RunTmux("list-panes", "-a", "-F", "#{pane_pid} #{pane_id}")
	if !ok {
		return ""
	}
	panes := map[int]string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		p, id, _ := strings.Cut(line, " ")
		if n, err := strconv.Atoi(p); err == nil {
			panes[n] = id
		}
	}
	snap, ok := process.Scan()
	if !ok {
		return ""
	}
	return findPane(pid, panes, snap.ParentOf)
}

// findPane walks parentOf from pid (bounded, cycle-safe) to the first pid
// that is a pane root.
func findPane(pid int, panes map[int]string, parentOf func(int) (int, bool)) string {
	for i := 0; i < 64 && pid > 1; i++ {
		if id, ok := panes[pid]; ok {
			return id
		}
		next, ok := parentOf(pid)
		if !ok || next == pid {
			return ""
		}
		pid = next
	}
	return ""
}
