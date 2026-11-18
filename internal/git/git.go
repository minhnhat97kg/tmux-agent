// Package git provides git data for the "git" tab of the bottom panel:
// current branch, added/removed line counts, ahead/behind vs. upstream,
// the list of changed files, and the PR number (via `gh` if available).
//
// This package ONLY provides data-fetching functions and data types — it
// does not spawn its own goroutine/ticker. Part 7 (the background worker)
// calls FetchGitData every 2 seconds and FetchPRNumber/PRCache separately
// (gh is much slower than git, so it isn't called on the same cadence).
//
// Ported from src/git.rs (the Rust reference implementation) — see
// docs/superpowers/plans/drafts/4a-task2-report.md to cross-check each
// command.
package git

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// GitFileEntry is one changed file: a status indicator, the display name
// (basename), the full path relative to the repo root, and that file's own
// added/removed line counts.
type GitFileEntry struct {
	Status    byte
	Name      string
	Path      string
	Additions int
	Deletions int
}

// DiffStat is the total added/removed line count, taken from
// `git diff --shortstat`.
type DiffStat struct {
	Insertions int
	Deletions  int
}

// AheadBehind is the number of commits ahead/behind relative to upstream.
type AheadBehind struct {
	Ahead  int
	Behind int
}

// GitData aggregates all the git information read in one FetchGitData scan.
//
// No PR field: `gh pr view` is many times slower than git, so the PR number
// is fetched separately via FetchPRNumber/PRCache — keeping the same
// boundary as the reference implementation, where fetch_git_data never
// fills in pr_number.
type GitData struct {
	DiffStat       *DiffStat
	Branch         string
	AheadBehind    *AheadBehind
	StagedFiles    []GitFileEntry
	UnstagedFiles  []GitFileEntry
	UntrackedFiles []string
	RemoteURL      string
}

// ChangedFileCount returns the total number of staged + unstaged +
// untracked files.
func (d GitData) ChangedFileCount() int {
	return len(d.StagedFiles) + len(d.UnstagedFiles) + len(d.UntrackedFiles)
}

// FetchGitData runs the various git subcommands on path and aggregates the
// results into a GitData. This is a blocking function (it runs
// subprocesses) — call it from a background goroutine, not directly in
// Update.
//
// When path is not a git repo (or git isn't installed), every subcommand
// fails and the returned GitData is left at its zero values (Branch == "",
// nil slices, nil pointers) — no panic, no error propagated outward.
func FetchGitData(path string) GitData {
	var data GitData

	if text, ok := runGit(path, "status", "--short"); ok {
		parseStatusShort(text, &data)
	}

	if text, ok := runGit(path, "diff", "--shortstat"); ok {
		data.DiffStat = parseDiffStat(text)
	}

	if text, ok := runGit(path, "rev-parse", "--abbrev-ref", "HEAD"); ok {
		data.Branch = text
	}

	if text, ok := runGit(path, "rev-list", "--left-right", "--count", "HEAD...@{upstream}"); ok {
		data.AheadBehind = parseAheadBehind(text)
	}

	applyNumstat(path, []string{"diff", "--cached", "--numstat"}, data.StagedFiles)
	applyNumstat(path, []string{"diff", "--numstat"}, data.UnstagedFiles)

	if text, ok := runGit(path, "remote", "get-url", "origin"); ok {
		data.RemoteURL = normalizeGitURL(text)
	}

	return data
}

// FetchPRNumber fetches the current branch's PR number at path via
// `gh pr view`. Returns ok=false when there is no PR, `gh` isn't installed,
// the user isn't logged in, path isn't a git repo, or the command takes
// more than 5 seconds (a blocking deadline so a hung `gh` doesn't hang the
// background git worker).
func FetchPRNumber(path string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gh", "pr", "view", "--json", "number", "-q", ".number")
	cmd.Dir = path
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	cmd.Stdin = nil

	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	num := strings.TrimSpace(string(out))
	if num == "" {
		return "", false
	}
	return num, true
}

// PRCacheTTL is how long a PR lookup stays "fresh" before PRCache calls gh
// again. The PR number only changes when the branch is switched (already
// part of the cache key) or when a new PR was just created for the current
// branch — the TTL bounds how long it can take to detect the latter case.
const PRCacheTTL = 300 * time.Second

// PRCache is a single-slot, TTL'd cache for the PR number, keyed by (path,
// branch). The zero value is ready to use. Not safe for concurrent use from
// multiple goroutines — one background worker owns an instance and calls
// GetOrFetch on every tick.
type PRCache struct {
	entry *prCacheEntry
}

type prCacheEntry struct {
	path     string
	branch   string
	prNumber string
	hasPR    bool
	cachedAt time.Time
}

// GetOrFetch returns the cached PR number for (path, branch) if it's still
// fresh (within PRCacheTTL of the previous fetcher call), otherwise calls
// fetcher, stores the result (even ok=false — "no PR" is cached too), and
// returns it.
//
// Returns ("", false) immediately, without calling fetcher or touching the
// cache, when branch is empty or is "HEAD" — `git rev-parse --abbrev-ref
// HEAD` prints the string "HEAD" when in a detached state. A PR is always
// tied to a branch, so neither case has a meaningful cache key.
func (c *PRCache) GetOrFetch(path, branch string, now time.Time, fetcher func(string) (string, bool)) (string, bool) {
	if branch == "" || branch == "HEAD" {
		return "", false
	}
	if e := c.entry; e != nil && e.path == path && e.branch == branch && now.Sub(e.cachedAt) < PRCacheTTL {
		return e.prNumber, e.hasPR
	}
	pr, ok := fetcher(path)
	c.entry = &prCacheEntry{path: path, branch: branch, prNumber: pr, hasPR: ok, cachedAt: now}
	return pr, ok
}

// parseStatusShort parses the output of `git status --short` into three
// groups: staged/unstaged/untracked.
//
// Each line looks like "XY filename" where:
//   - X is the index status (staged)
//   - Y is the worktree status (unstaged)
//   - "??" means untracked
func parseStatusShort(text string, data *GitData) {
	for _, line := range strings.Split(text, "\n") {
		if len(line) < 3 {
			continue
		}
		x := line[0]
		y := line[1]
		rawName := line[3:]
		fullPath := normalizeGitPath(rawName)
		isDir := strings.HasSuffix(fullPath, "/")
		nameTrimmed := strings.TrimSuffix(fullPath, "/")
		basename := lastSegment(nameTrimmed)
		if isDir {
			basename += "/"
		}

		if x == '?' && y == '?' {
			data.UntrackedFiles = append(data.UntrackedFiles, basename)
			continue
		}

		// Staged: X is M, A, D, R, or C.
		if x == 'M' || x == 'A' || x == 'D' || x == 'R' || x == 'C' {
			status := x
			if x == 'R' || x == 'C' {
				status = 'M' // rename/copy is displayed as modified
			}
			data.StagedFiles = append(data.StagedFiles, GitFileEntry{
				Status: status,
				Name:   basename,
				Path:   fullPath,
			})
		}

		// Unstaged: Y is M or D.
		if y == 'M' || y == 'D' {
			data.UnstagedFiles = append(data.UnstagedFiles, GitFileEntry{
				Status: y,
				Name:   basename,
				Path:   fullPath,
			})
		}
	}
}

// applyNumstat runs `git diff [--cached] --numstat` and fills in
// additions/deletions on the existing entries (matched by full path first,
// then falling back to basename). Modifies the entries' elements in place —
// doesn't add/remove elements, so it doesn't need a pointer to the slice.
func applyNumstat(path string, args []string, entries []GitFileEntry) {
	text, ok := runGit(path, args...)
	if !ok {
		return
	}
	numstat := parseNumstat(text)
	for i := range entries {
		v, found := numstat[entries[i].Path]
		if !found {
			v, found = numstat[entries[i].Name]
		}
		if found {
			entries[i].Additions = v.additions
			entries[i].Deletions = v.deletions
		}
	}
}

type numstatValue struct {
	additions int
	deletions int
}

// parseNumstat parses the output of `git diff --numstat` into a map of
// filename (both full path and basename, so applyNumstat can match either
// form) -> (additions, deletions).
func parseNumstat(text string) map[string]numstatValue {
	m := make(map[string]numstatValue)
	for _, line := range strings.Split(text, "\n") {
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		add, _ := strconv.Atoi(parts[0]) // binary files print "-": Atoi fails -> 0, matching the reference
		del, _ := strconv.Atoi(parts[1])
		path := normalizeGitPath(parts[2])
		basename := lastSegment(path)
		v := numstatValue{additions: add, deletions: del}
		m[path] = v
		if basename != path {
			m[basename] = v
		}
	}
	return m
}

// parseAheadBehind parses the output of
// `git rev-list --left-right --count HEAD...@{upstream}`: two numbers
// separated by a tab, ahead first then behind. Returns nil if there aren't
// exactly two parts.
func parseAheadBehind(text string) *AheadBehind {
	parts := strings.Split(text, "\t")
	if len(parts) != 2 {
		return nil
	}
	ahead, _ := strconv.Atoi(parts[0])
	behind, _ := strconv.Atoi(parts[1])
	return &AheadBehind{Ahead: ahead, Behind: behind}
}

// parseDiffStat parses the output of `git diff --shortstat`, e.g.
// "2 files changed, 4 insertions(+), 2 deletions(-)". Returns nil when the
// text is empty (nothing changed); if insertions or deletions is missing,
// that part is 0.
func parseDiffStat(text string) *DiffStat {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var insertions, deletions int
	for _, part := range strings.Split(text, ",") {
		part = strings.TrimSpace(part)
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}
		n, _ := strconv.Atoi(fields[0])
		switch {
		case strings.Contains(part, "insertion"):
			insertions = n
		case strings.Contains(part, "deletion"):
			deletions = n
		}
	}
	return &DiffStat{Insertions: insertions, Deletions: deletions}
}

// normalizeGitPath handles a rename/copy line from `status --short` and a
// filename from `--numstat`: "old -> new" or "old => new" keeps only the
// "new" part.
func normalizeGitPath(path string) string {
	path = strings.TrimSpace(path)
	if idx := strings.LastIndex(path, " -> "); idx >= 0 {
		return strings.TrimSpace(path[idx+len(" -> "):])
	}
	if idx := strings.LastIndex(path, " => "); idx >= 0 {
		return strings.TrimSpace(path[idx+len(" => "):])
	}
	return path
}

// normalizeGitURL normalizes a remote URL into https form, dropping the
// ".git" suffix, for display purposes (and, potentially later, for opening
// a browser).
func normalizeGitURL(url string) string {
	url = strings.TrimSpace(url)
	if rest, ok := strings.CutPrefix(url, "git@"); ok {
		converted := strings.ReplaceAll(rest, ":", "/")
		cleaned := strings.TrimSuffix(converted, ".git")
		return "https://" + cleaned
	}
	if strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://") {
		return strings.TrimSuffix(url, ".git")
	}
	return url
}

// lastSegment returns the part after the last "/", or the whole string if
// there is no "/".
func lastSegment(s string) string {
	if idx := strings.LastIndex(s, "/"); idx >= 0 {
		return s[idx+1:]
	}
	return s
}

// runGit runs `git -C path <args...>` with GIT_OPTIONAL_LOCKS=0 (avoids
// lock file contention with the user's real git running concurrently).
// Returns ok=false when git isn't installed, path isn't a repo, or the
// command exits non-zero or prints an empty string — the caller doesn't
// need to distinguish between these failure reasons.
//
// ONLY trims trailing whitespace, does NOT trim leading whitespace — unlike
// the Rust reference implementation (which uses .trim() on both ends).
// Reason: the first line of `git status --short` can start with a
// meaningful leading space (an empty X column means the file only changed
// in the worktree, not staged, e.g. " M file"); trimming both ends would
// eat that space when it's the ONLY file in the output, making
// parseStatusShort misread the columns and cut the filename incorrectly —
// discovered via an integration test on a temp repo
// (TestFetchGitDataUnstagedFileChange); the reference implementation has no
// integration test that exposes this bug. Other git commands have no
// leading whitespace with meaning, so they're unaffected.
func runGit(path string, args ...string) (string, bool) {
	full := make([]string, 0, len(args)+2)
	full = append(full, "-C", path)
	full = append(full, args...)

	cmd := exec.Command("git", full...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	cmd.Stdin = nil

	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	s := strings.TrimRight(string(out), " \t\r\n")
	if strings.TrimSpace(s) == "" {
		return "", false
	}
	return s, true
}
