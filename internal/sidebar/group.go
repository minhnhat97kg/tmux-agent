// Groups panes by git repo root — not by tmux session. Multiple panes in the
// same repo (even across different sessions, or in a worktree of the same
// repo) are gathered into a single RepoGroup.
//
// NOT a pure function (Minor 4, final review 3b): GroupPanesByRepo itself
// doesn't call tmux/git, but findRepoRoot walks up the parent directories
// using os.Lstat/os.ReadFile — that IS filesystem I/O, even though it never
// spawns a child process. Model.applyRows calls GroupPanesByRepo (via
// buildRows/computeStatusCounts) SYNCHRONOUSLY right inside Update, not
// wrapped in a tea.Cmd — on a hung filesystem (NFS/sshfs, or any path that
// makes Lstat/ReadFile block) the entire bubbletea loop will freeze along
// with it, not just that one refresh. This is a known limitation, not fixed
// in this version — moving GroupPanesByRepo into a tea.Cmd is a job for
// later (no architecture change within the scope of review 3b).
package sidebar

import (
	"os"
	"path/filepath"
	"strings"

	"tmux-agent/internal/tmux"
)

// RepoGroup is the set of panes working on the same git repo (or the same
// directory, if the pane isn't inside any git repo).
type RepoGroup struct {
	// Name is the display name: the repo root's basename, or the pane's raw
	// path basename if no repo root could be found.
	Name string
	// HasFocus is true when at least one pane in the group is both the
	// active pane and inside the active window, AND the session containing
	// it actually has a real tmux client attached (session.Attached) —
	// window_active/pane_active is state tmux remembers for EVERY session
	// even ones nobody is looking at; without the Attached condition, focus
	// would get highlighted on a session running in the background instead
	// of the one the user is actually looking at right now. Once true, it
	// is never reset back to false.
	HasFocus bool
	// Panes preserves the order encountered while walking session/window/pane.
	Panes []tmux.PaneInfo
}

// GroupPanesByRepo groups every pane across all sessions by the git repo
// root derived from PaneInfo.Path. Groups are returned in OPEN order — i.e.
// the order in which each group was first encountered while walking
// session/window/pane (the order tmux itself returns from list-panes), not
// re-sorted alphabetically.
//
// A pane that isn't inside any git repo (including an empty path) is
// grouped by its own raw path.
func GroupPanesByRepo(sessions []tmux.SessionInfo) []RepoGroup {
	groups := make(map[string]*RepoGroup)
	var order []string
	// keyCache avoids re-probing for .git multiple times for the same path
	// within one refresh cycle (many panes often point at the same directory).
	keyCache := make(map[string]string)

	for _, session := range sessions {
		for _, window := range session.Windows {
			for _, pane := range window.Panes {
				key, cached := keyCache[pane.Path]
				if !cached {
					key = groupKeyForPath(pane.Path)
					keyCache[pane.Path] = key
				}

				g, exists := groups[key]
				if !exists {
					g = &RepoGroup{Name: lastPathSegment(key)}
					groups[key] = g
					order = append(order, key)
				}

				if session.Attached && window.WindowActive && pane.PaneActive {
					g.HasFocus = true
				}
				g.Panes = append(g.Panes, pane)
			}
		}
	}

	result := make([]RepoGroup, len(order))
	for i, key := range order {
		result[i] = *groups[key]
	}
	return result
}

// groupKeyForPath returns the group key for a pane's path: the repo root if
// one is found, or the path itself (even if empty) if the pane isn't inside
// any git repo.
func groupKeyForPath(path string) string {
	if path == "" {
		return ""
	}
	if root, ok := findRepoRoot(path); ok {
		return root
	}
	return path
}

// PaneBranch is the branch the pane works on: the one recorded for a
// tmux-agent worktree, else read from the repo's HEAD file (no git process,
// same walk-up as findRepoRoot). "" when detached or not in a repo.
func PaneBranch(p tmux.PaneInfo) string {
	if p.Worktree.Branch != "" {
		return p.Worktree.Branch
	}
	if p.Path == "" {
		return ""
	}
	for dir := filepath.Clean(p.Path); ; dir = filepath.Dir(dir) {
		gitPath := filepath.Join(dir, ".git")
		if info, err := os.Lstat(gitPath); err == nil {
			gitDir := gitPath
			if info.Mode().IsRegular() { // linked worktree: "gitdir: <path>"
				data, err := os.ReadFile(gitPath)
				if err != nil {
					return ""
				}
				gitDir = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(data)), "gitdir:"))
				if !filepath.IsAbs(gitDir) {
					gitDir = filepath.Join(dir, gitDir)
				}
			}
			head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
			ref := strings.TrimSpace(string(head))
			if err != nil || !strings.HasPrefix(ref, "ref: refs/heads/") {
				return "" // detached HEAD shows nothing
			}
			return strings.TrimPrefix(ref, "ref: refs/heads/")
		}
		if filepath.Dir(dir) == dir {
			return ""
		}
	}
}

// findRepoRoot walks up from path looking for the nearest ".git" entry, the
// same way git itself finds the toplevel repo — without invoking any git
// process.
//
//   - ".git" is a directory: this is the main repo (or a common bare-ish
//     layout), the root is the directory containing it.
//   - ".git" is a file (a worktree created by `git worktree add`, or a
//     submodule): read its content "gitdir: <path>". If that path is inside
//     "<repo>/.git/worktrees/<name>" then this is a linked worktree — the
//     returned root is <repo>, i.e. it's merged into the same group as the
//     main repo instead of forming its own group. If it doesn't match that
//     pattern (e.g. a submodule), the directory containing this ".git" file
//     is treated as its own root.
func findRepoRoot(path string) (string, bool) {
	dir := filepath.Clean(path)
	for {
		gitPath := filepath.Join(dir, ".git")
		info, err := os.Lstat(gitPath)
		if err == nil {
			switch {
			case info.IsDir():
				return dir, true
			case info.Mode().IsRegular():
				if root, ok := mainRepoFromWorktreeGitFile(dir, gitPath); ok {
					return root, true
				}
				return dir, true
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// mainRepoFromWorktreeGitFile reads a linked worktree's ".git" file (content
// "gitdir: <repo>/.git/worktrees/<name>") and returns <repo> — the main repo
// this worktree belongs to. ok = false if the content doesn't match that
// pattern.
func mainRepoFromWorktreeGitFile(dir, gitPath string) (string, bool) {
	data, err := os.ReadFile(gitPath)
	if err != nil {
		return "", false
	}

	const prefix = "gitdir: "
	content := strings.TrimSpace(string(data))
	if !strings.HasPrefix(content, prefix) {
		return "", false
	}

	target := strings.TrimPrefix(content, prefix)
	if !filepath.IsAbs(target) {
		target = filepath.Join(dir, target)
	}
	target = filepath.Clean(target)

	marker := string(filepath.Separator) + "worktrees" + string(filepath.Separator)
	idx := strings.LastIndex(target, marker)
	if idx < 0 {
		return "", false
	}
	gitDir := target[:idx] // .../<repo>/.git
	if filepath.Base(gitDir) != ".git" {
		return "", false
	}
	return filepath.Dir(gitDir), true
}

// lastPathSegment returns everything after the last '/', or the whole string
// if there is no '/'. Deliberately doesn't use filepath.Base since it
// returns "." for an empty string — here an empty string must produce an
// empty display name.
func lastPathSegment(s string) string {
	if idx := strings.LastIndexByte(s, '/'); idx >= 0 {
		return s[idx+1:]
	}
	return s
}
