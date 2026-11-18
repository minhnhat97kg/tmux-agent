package hook

import (
	"strconv"
	"strings"

	"tmux-agent/internal/process"
	"tmux-agent/internal/tmux"
)

// Background shells: @pane_bg_cmd says "the agent left a background Bash
// running", but nothing tells the hook when that shell ends — so the flag
// used to stick until SessionEnd and every later Stop resolved to
// `background` (the avatar kept saying "X is on it"). @pane_bg_live keeps
// the raw command of each background run (one per line); on Stop /
// Notification we look for it in the pane's process tree and drop the ones
// that finished.

// bgMarker is what we search the process tree for: the command's first
// line, trimmed, at most 120 bytes (a long command is still a unique prefix).
func bgMarker(command string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(command), "\n")
	line = strings.TrimSpace(line)
	if len(line) > 120 {
		line = line[:120]
	}
	return line
}

// rememberBackground adds a background run's command to @pane_bg_live.
func rememberBackground(pane, command string) {
	m := bgMarker(command)
	if m == "" {
		return
	}
	cur := tmux.GetPaneOptionValue(pane, tmux.PaneBgLive)
	if cur != "" {
		m = cur + "\n" + m
	}
	tmux.SetPaneOption(pane, tmux.PaneBgLive, m)
}

// liveMarkers keeps the markers still found in some command line. Claude
// runs the command as `zsh -c … eval '<command>'`, quoting ' as '\” —
// undone before matching.
func liveMarkers(markers, cmdlines []string) []string {
	var live []string
	for _, m := range markers {
		for _, l := range cmdlines {
			if strings.Contains(strings.ReplaceAll(l, `'\''`, `'`), m) {
				live = append(live, m)
				break
			}
		}
	}
	return live
}

// backgroundShellLive reports whether the pane still has a background shell
// running, forgetting the ones that ended. Unknown (no markers recorded by
// an older hook, ps or tmux unavailable) keeps the old answer: the flag.
func backgroundShellLive(pane string) bool {
	if tmux.GetPaneOptionValue(pane, tmux.PaneBgCmd) == "" {
		return false
	}
	raw := tmux.GetPaneOptionValue(pane, tmux.PaneBgLive)
	if raw == "" {
		return true
	}
	out, ok := tmux.RunTmux("display-message", "-p", "-t", pane, "#{pane_pid}")
	pid, err := strconv.Atoi(strings.TrimSpace(out))
	if !ok || err != nil || pid == 0 {
		return true
	}
	snap, ok := process.Scan()
	if !ok {
		return true
	}
	live := liveMarkers(strings.Split(raw, "\n"), snap.CommandLinesForTree([]int{pid}))
	if len(live) == 0 {
		tmux.UnsetPaneOption(pane, tmux.PaneBgCmd)
		tmux.UnsetPaneOption(pane, tmux.PaneBgLive)
		return false
	}
	tmux.SetPaneOption(pane, tmux.PaneBgLive, strings.Join(live, "\n"))
	return true
}
