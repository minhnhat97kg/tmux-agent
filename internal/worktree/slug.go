// Package worktree bundles the pure logic behind the sidebar's git worktree
// spawn/remove flow: generating a safe directory name (slug), reading
// config from tmux options, markers tagging a window/pane as spawn-created,
// and the git/tmux call contract that the spawn/remove flow (part 5, task 2)
// implements.
//
// This file does NOT call git or tmux — every function here is pure,
// taking input and returning output with no side effects. Calling real
// git/tmux is task 2's job (flow.go, spawn.go).
package worktree

import (
	"path/filepath"
	"strconv"
	"strings"
)

// maxCollisionAttempts caps how many times "<slug>-2", "<slug>-3", … are
// tried before pickUniqueSlug gives up.
const maxCollisionAttempts = 99

// Slugify turns an arbitrary string (a branch name, a task description the
// user typed) into a safe directory name: lowercase, only [a-z0-9-], every
// run of non-alphanumeric characters (including control characters,
// whitespace, non-ASCII characters) collapsed into a single "-", with no
// leading or trailing "-".
func Slugify(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	lastHyphen := true
	for _, ch := range s {
		c := toASCIILower(ch)
		if isASCIIAlnum(c) {
			out.WriteRune(c)
			lastHyphen = false
		} else if !lastHyphen {
			out.WriteByte('-')
			lastHyphen = true
		}
	}
	return strings.TrimRight(out.String(), "-")
}

func isASCIIAlnum(c rune) bool {
	return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
}

func toASCIILower(c rune) rune {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

// PickUniqueSlug tries "slug", then "slug-2", "slug-3", … up to
// maxCollisionAttempts extra tries, returning the first candidate for which
// isFree reports true. ok=false when every candidate within the limit is
// taken.
func PickUniqueSlug(slug string, isFree func(string) bool) (result string, ok bool) {
	if isFree(slug) {
		return slug, true
	}
	for n := 2; n <= maxCollisionAttempts+1; n++ {
		candidate := slug + "-" + strconv.Itoa(n)
		if isFree(candidate) {
			return candidate, true
		}
	}
	return "", false
}

// WorktreePathFor joins "<repoRoot>/<worktreeDir>/<slug>". An empty
// worktreeDir means DefaultWorktreeDir is used. ok=false when worktreeDir is
// an absolute path or contains a ".." component — the worktree must stay
// inside the repo.
func WorktreePathFor(repoRoot, slug, worktreeDir string) (path string, ok bool) {
	dir := worktreeDir
	if dir == "" {
		dir = DefaultWorktreeDir
	}
	if filepath.IsAbs(dir) {
		return "", false
	}
	for _, part := range strings.Split(filepath.ToSlash(dir), "/") {
		if part == ".." {
			return "", false
		}
	}
	return filepath.Join(repoRoot, dir, slug), true
}
