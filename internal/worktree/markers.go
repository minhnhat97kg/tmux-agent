package worktree

import (
	"strings"

	"tmux-agent/internal/tmux"
)

// SpawnMarkersTemplate builds the template for tmux "display-message -p -F"
// used to read a pane/window's markers: the spawned flag, source repo,
// worktree path, branch name, and window id — always exactly five lines, in
// order. The delete-confirmation popup and the delete flow itself (task 2)
// both use this one template so field reads never drift out of sync.
func SpawnMarkersTemplate() string {
	return strings.Join([]string{
		"#{" + tmux.SpawnedOption + "}",
		"#{" + tmux.SpawnedFromOption + "}",
		"#{" + tmux.SpawnedWorktreeOption + "}",
		"#{" + tmux.SpawnedBranchOption + "}",
		"#{window_id}",
	}, "\n")
}

// SpawnMarkers holds the parsed marker data of a pane/window. Every field is
// always present (empty when tmux has no such key) — IsSpawned is the one
// canonical check; don't infer it yourself from whether FromRepo is empty.
type SpawnMarkers struct {
	Spawned      bool
	FromRepo     string
	WorktreePath string
	Branch       string
	WindowID     string
}

// IsSpawned reports true only when both the Spawned flag and FromRepo are
// set — the flag alone or the repo alone isn't enough to count this as a
// window created by sidebar spawn.
func (m SpawnMarkers) IsSpawned() bool {
	return m.Spawned && m.FromRepo != ""
}

// ParseSpawnMarkers parses the output of
// "tmux display-message -p -F SpawnMarkersTemplate()". A missing field (tmux
// display-message returns empty for a nonexistent key, or raw has fewer than
// 5 lines) becomes "" / false, not an error.
func ParseSpawnMarkers(raw string) SpawnMarkers {
	lines := strings.Split(raw, "\n")
	get := func(i int) string {
		if i < len(lines) {
			return lines[i]
		}
		return ""
	}
	return SpawnMarkers{
		Spawned:      get(0) == "1",
		FromRepo:     get(1),
		WorktreePath: get(2),
		Branch:       get(3),
		WindowID:     get(4),
	}
}
