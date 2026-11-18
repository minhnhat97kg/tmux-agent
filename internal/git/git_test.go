package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// ─── parseDiffStat ──────────────────────────────────────────────────

func TestParseDiffStatInsertionsAndDeletions(t *testing.T) {
	got := parseDiffStat("2 files changed, 4 insertions(+), 2 deletions(-)")
	if got == nil || got.Insertions != 4 || got.Deletions != 2 {
		t.Fatalf("got %+v, want {4 2}", got)
	}
}

func TestParseDiffStatInsertionsOnly(t *testing.T) {
	got := parseDiffStat("1 file changed, 5 insertions(+)")
	if got == nil || got.Insertions != 5 || got.Deletions != 0 {
		t.Fatalf("got %+v, want {5 0}", got)
	}
}

func TestParseDiffStatDeletionsOnly(t *testing.T) {
	got := parseDiffStat("1 file changed, 3 deletions(-)")
	if got == nil || got.Insertions != 0 || got.Deletions != 3 {
		t.Fatalf("got %+v, want {0 3}", got)
	}
}

func TestParseDiffStatEmptyString(t *testing.T) {
	if got := parseDiffStat(""); got != nil {
		t.Fatalf("an empty string must return nil, got %+v", got)
	}
}

func TestParseDiffStatWhitespaceOnly(t *testing.T) {
	if got := parseDiffStat("   "); got != nil {
		t.Fatalf("a whitespace-only string must return nil, got %+v", got)
	}
}

// ─── parseAheadBehind ───────────────────────────────────────────────

func TestParseAheadBehindNormal(t *testing.T) {
	got := parseAheadBehind("3\t2")
	if got == nil || got.Ahead != 3 || got.Behind != 2 {
		t.Fatalf("got %+v, want {3 2}", got)
	}
}

func TestParseAheadBehindMalformed(t *testing.T) {
	if got := parseAheadBehind("only-one-part"); got != nil {
		t.Fatalf("a missing tab must return nil, got %+v", got)
	}
}

// ─── normalizeGitURL ────────────────────────────────────────────────

func TestNormalizeGitURLSsh(t *testing.T) {
	got := normalizeGitURL("git@github.com:user/repo.git")
	want := "https://github.com/user/repo"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNormalizeGitURLHttpsWithGitSuffix(t *testing.T) {
	got := normalizeGitURL("https://github.com/user/repo.git")
	want := "https://github.com/user/repo"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNormalizeGitURLHttpsClean(t *testing.T) {
	got := normalizeGitURL("https://github.com/user/repo")
	want := "https://github.com/user/repo"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestNormalizeGitURLUnrecognizedFormatUnchanged(t *testing.T) {
	got := normalizeGitURL("/local/path/repo")
	want := "/local/path/repo"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// ─── parseStatusShort ───────────────────────────────────────────────

func TestParseStatusShortStagedModified(t *testing.T) {
	var data GitData
	parseStatusShort("M  src/app.rs", &data)
	if len(data.StagedFiles) != 1 || data.StagedFiles[0].Status != 'M' ||
		data.StagedFiles[0].Name != "app.rs" || data.StagedFiles[0].Path != "src/app.rs" {
		t.Fatalf("got %+v", data.StagedFiles)
	}
	if len(data.UnstagedFiles) != 0 || len(data.UntrackedFiles) != 0 {
		t.Fatalf("must have no unstaged/untracked: %+v", data)
	}
}

func TestParseStatusShortStagedAdded(t *testing.T) {
	var data GitData
	parseStatusShort("A  new.rs", &data)
	if len(data.StagedFiles) != 1 || data.StagedFiles[0].Status != 'A' || data.StagedFiles[0].Name != "new.rs" {
		t.Fatalf("got %+v", data.StagedFiles)
	}
}

func TestParseStatusShortUnstagedModified(t *testing.T) {
	var data GitData
	parseStatusShort(" M config.toml", &data)
	if len(data.StagedFiles) != 0 {
		t.Fatalf("must have no staged: %+v", data.StagedFiles)
	}
	if len(data.UnstagedFiles) != 1 || data.UnstagedFiles[0].Status != 'M' || data.UnstagedFiles[0].Name != "config.toml" {
		t.Fatalf("got %+v", data.UnstagedFiles)
	}
}

func TestParseStatusShortBothStagedAndUnstaged(t *testing.T) {
	var data GitData
	parseStatusShort("MM src/lib.rs", &data)
	if len(data.StagedFiles) != 1 || data.StagedFiles[0].Status != 'M' {
		t.Fatalf("staged got %+v", data.StagedFiles)
	}
	if len(data.UnstagedFiles) != 1 || data.UnstagedFiles[0].Status != 'M' {
		t.Fatalf("unstaged got %+v", data.UnstagedFiles)
	}
}

func TestParseStatusShortUntracked(t *testing.T) {
	var data GitData
	parseStatusShort("?? tmp/debug.log", &data)
	if len(data.StagedFiles) != 0 || len(data.UnstagedFiles) != 0 {
		t.Fatalf("must have no staged/unstaged: %+v", data)
	}
	if len(data.UntrackedFiles) != 1 || data.UntrackedFiles[0] != "debug.log" {
		t.Fatalf("got %+v, want [debug.log]", data.UntrackedFiles)
	}
}

func TestParseStatusShortUntrackedDirectory(t *testing.T) {
	var data GitData
	parseStatusShort("?? docs/superpowers/specs/", &data)
	if len(data.UntrackedFiles) != 1 || data.UntrackedFiles[0] != "specs/" {
		t.Fatalf("got %+v, want [specs/]", data.UntrackedFiles)
	}
}

func TestParseStatusShortUntrackedTopLevelDirectory(t *testing.T) {
	var data GitData
	parseStatusShort("?? mydir/", &data)
	if len(data.UntrackedFiles) != 1 || data.UntrackedFiles[0] != "mydir/" {
		t.Fatalf("got %+v, want [mydir/]", data.UntrackedFiles)
	}
}

func TestParseStatusShortDeletedStaged(t *testing.T) {
	var data GitData
	parseStatusShort("D  old.rs", &data)
	if len(data.StagedFiles) != 1 || data.StagedFiles[0].Status != 'D' {
		t.Fatalf("got %+v", data.StagedFiles)
	}
}

func TestParseStatusShortDeletedUnstaged(t *testing.T) {
	var data GitData
	parseStatusShort(" D removed.rs", &data)
	if len(data.StagedFiles) != 0 {
		t.Fatalf("must have no staged: %+v", data.StagedFiles)
	}
	if len(data.UnstagedFiles) != 1 || data.UnstagedFiles[0].Status != 'D' {
		t.Fatalf("got %+v", data.UnstagedFiles)
	}
}

func TestParseStatusShortRenamed(t *testing.T) {
	var data GitData
	parseStatusShort("R  old.rs -> new.rs", &data)
	if len(data.StagedFiles) != 1 {
		t.Fatalf("got %+v", data.StagedFiles)
	}
	f := data.StagedFiles[0]
	if f.Status != 'M' { // rename is displayed as modified
		t.Fatalf("status = %c, want M", f.Status)
	}
	if f.Name != "new.rs" || f.Path != "new.rs" {
		t.Fatalf("got name=%q path=%q, want new.rs/new.rs", f.Name, f.Path)
	}
}

func TestParseStatusShortSameBasenameKeepsSeparatePaths(t *testing.T) {
	var data GitData
	parseStatusShort("M  src/app.rs\nM  tests/app.rs", &data)
	if len(data.StagedFiles) != 2 {
		t.Fatalf("got %+v", data.StagedFiles)
	}
	if data.StagedFiles[0].Name != "app.rs" || data.StagedFiles[0].Path != "src/app.rs" {
		t.Fatalf("entry 0 got %+v", data.StagedFiles[0])
	}
	if data.StagedFiles[1].Name != "app.rs" || data.StagedFiles[1].Path != "tests/app.rs" {
		t.Fatalf("entry 1 got %+v", data.StagedFiles[1])
	}
}

func TestParseStatusShortMultipleLines(t *testing.T) {
	var data GitData
	parseStatusShort("M  src/app.rs\nA  src/new.rs\n M config.toml\n?? tmp/log", &data)
	if len(data.StagedFiles) != 2 {
		t.Fatalf("staged = %d, want 2", len(data.StagedFiles))
	}
	if len(data.UnstagedFiles) != 1 {
		t.Fatalf("unstaged = %d, want 1", len(data.UnstagedFiles))
	}
	if len(data.UntrackedFiles) != 1 {
		t.Fatalf("untracked = %d, want 1", len(data.UntrackedFiles))
	}
}

func TestParseStatusShortEmptyString(t *testing.T) {
	var data GitData
	parseStatusShort("", &data)
	if len(data.StagedFiles) != 0 || len(data.UnstagedFiles) != 0 || len(data.UntrackedFiles) != 0 {
		t.Fatalf("an empty string must produce nothing: %+v", data)
	}
}

// ─── parseNumstat ───────────────────────────────────────────────────

func TestParseNumstatPrefersFullPath(t *testing.T) {
	m := parseNumstat("1\t0\tsrc/app.rs\n2\t1\ttests/app.rs")
	if v := m["src/app.rs"]; v.additions != 1 || v.deletions != 0 {
		t.Fatalf("src/app.rs got %+v", v)
	}
	if v := m["tests/app.rs"]; v.additions != 2 || v.deletions != 1 {
		t.Fatalf("tests/app.rs got %+v", v)
	}
}

// ─── PRCache ────────────────────────────────────────────────────────

func TestPRCacheFirstCallInvokesFetcher(t *testing.T) {
	var cache PRCache
	calls := 0
	now := time.Now()
	pr, ok := cache.GetOrFetch("/a", "main", now, func(string) (string, bool) {
		calls++
		return "42", true
	})
	if !ok || pr != "42" {
		t.Fatalf("pr=%q ok=%v, want 42/true", pr, ok)
	}
	if calls != 1 {
		t.Fatalf("calls=%d, want 1", calls)
	}
}

func TestPRCacheSameKeyWithinTTLUsesCache(t *testing.T) {
	var cache PRCache
	calls := 0
	now := time.Now()
	cache.GetOrFetch("/a", "main", now, func(string) (string, bool) {
		calls++
		return "42", true
	})
	pr, ok := cache.GetOrFetch("/a", "main", now.Add(time.Second), func(string) (string, bool) {
		calls++
		return "99", true
	})
	if !ok || pr != "42" {
		t.Fatalf("must return the cached value 42, got %q/%v", pr, ok)
	}
	if calls != 1 {
		t.Fatalf("a cache hit must not call the fetcher again, calls=%d", calls)
	}
}

func TestPRCacheBranchChangeRefetches(t *testing.T) {
	var cache PRCache
	calls := 0
	now := time.Now()
	cache.GetOrFetch("/a", "main", now, func(string) (string, bool) {
		calls++
		return "42", true
	})
	pr, ok := cache.GetOrFetch("/a", "feature", now, func(string) (string, bool) {
		calls++
		return "77", true
	})
	if !ok || pr != "77" || calls != 2 {
		t.Fatalf("pr=%q ok=%v calls=%d, want 77/true/2", pr, ok, calls)
	}
}

func TestPRCachePathChangeRefetches(t *testing.T) {
	var cache PRCache
	calls := 0
	now := time.Now()
	cache.GetOrFetch("/a", "main", now, func(string) (string, bool) {
		calls++
		return "42", true
	})
	pr, ok := cache.GetOrFetch("/b", "main", now, func(string) (string, bool) {
		calls++
		return "77", true
	})
	if !ok || pr != "77" || calls != 2 {
		t.Fatalf("pr=%q ok=%v calls=%d, want 77/true/2", pr, ok, calls)
	}
}

func TestPRCacheExpiredTTLRefetches(t *testing.T) {
	var cache PRCache
	calls := 0
	now := time.Now()
	cache.GetOrFetch("/a", "main", now, func(string) (string, bool) {
		calls++
		return "42", true
	})
	pr, ok := cache.GetOrFetch("/a", "main", now.Add(PRCacheTTL+time.Second), func(string) (string, bool) {
		calls++
		return "77", true
	})
	if !ok || pr != "77" || calls != 2 {
		t.Fatalf("pr=%q ok=%v calls=%d, want 77/true/2", pr, ok, calls)
	}
}

func TestPRCacheEmptyBranchSkippedAndNotCached(t *testing.T) {
	var cache PRCache
	calls := 0
	now := time.Now()
	pr, ok := cache.GetOrFetch("/a", "", now, func(string) (string, bool) {
		calls++
		return "should-not-be-called", true
	})
	if ok || pr != "" {
		t.Fatalf("an empty branch must return false, got %q/%v", pr, ok)
	}
	if calls != 0 {
		t.Fatalf("the fetcher must not be called, calls=%d", calls)
	}
	// The cache must still be empty: the next real branch must still call
	// the fetcher.
	pr, ok = cache.GetOrFetch("/a", "main", now, func(string) (string, bool) {
		calls++
		return "42", true
	})
	if !ok || pr != "42" || calls != 1 {
		t.Fatalf("pr=%q ok=%v calls=%d, want 42/true/1", pr, ok, calls)
	}
}

func TestPRCacheDetachedHeadSkippedAndNotCached(t *testing.T) {
	// `git rev-parse --abbrev-ref HEAD` prints the string "HEAD" when detached.
	var cache PRCache
	calls := 0
	now := time.Now()
	pr, ok := cache.GetOrFetch("/a", "HEAD", now, func(string) (string, bool) {
		calls++
		return "should-not-be-called", true
	})
	if ok || pr != "" {
		t.Fatalf("detached HEAD must return false, got %q/%v", pr, ok)
	}
	if calls != 0 {
		t.Fatalf("the fetcher must not be called, calls=%d", calls)
	}
	pr, ok = cache.GetOrFetch("/a", "main", now, func(string) (string, bool) {
		calls++
		return "42", true
	})
	if !ok || pr != "42" || calls != 1 {
		t.Fatalf("pr=%q ok=%v calls=%d, want 42/true/1", pr, ok, calls)
	}
}

func TestPRCacheNoPRResultAlsoCached(t *testing.T) {
	var cache PRCache
	calls := 0
	now := time.Now()
	// The first call returns false (no PR) — that result must also be cached.
	cache.GetOrFetch("/a", "main", now, func(string) (string, bool) {
		calls++
		return "", false
	})
	pr, ok := cache.GetOrFetch("/a", "main", now.Add(time.Second), func(string) (string, bool) {
		calls++
		return "should-not-be-called", true
	})
	if ok || pr != "" {
		t.Fatalf("must return the cached empty result, got %q/%v", pr, ok)
	}
	if calls != 1 {
		t.Fatalf("the second call must hit the cached None, calls=%d", calls)
	}
}

// ─── FetchGitData on a temp git repo ─────────────────────────────────

// runGitCmd runs real git in dir to build test fixtures, panicking on
// error (test setup, not product code).
func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v error: %v\n%s", args, err, out)
	}
}

// newTempRepo builds a temporary git repo with an initial commit, branch "main".
func newTempRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitCmd(t, dir, "init", "-q", "-b", "main")
	runGitCmd(t, dir, "config", "user.email", "test@example.com")
	runGitCmd(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("line 1\nline 2\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGitCmd(t, dir, "add", "a.txt")
	runGitCmd(t, dir, "commit", "-q", "-m", "initial commit")
	return dir
}

func TestFetchGitDataNonGitRepoReturnsEmpty(t *testing.T) {
	dir := t.TempDir() // no git init
	data := FetchGitData(dir)
	if data.Branch != "" {
		t.Fatalf("branch = %q, want empty", data.Branch)
	}
	if data.DiffStat != nil || data.AheadBehind != nil {
		t.Fatalf("data = %+v, want both pointers nil", data)
	}
	if data.ChangedFileCount() != 0 {
		t.Fatalf("ChangedFileCount = %d, want 0", data.ChangedFileCount())
	}
}

func TestFetchGitDataCleanRepoOnlyBranch(t *testing.T) {
	dir := newTempRepo(t)
	data := FetchGitData(dir)
	if data.Branch != "main" {
		t.Fatalf("branch = %q, want main", data.Branch)
	}
	if data.DiffStat != nil {
		t.Fatalf("clean repo, DiffStat must be nil: %+v", data.DiffStat)
	}
	if data.ChangedFileCount() != 0 {
		t.Fatalf("ChangedFileCount = %d, want 0", data.ChangedFileCount())
	}
	if data.AheadBehind != nil {
		t.Fatalf("no upstream yet, AheadBehind must be nil: %+v", data.AheadBehind)
	}
}

func TestFetchGitDataUnstagedFileChange(t *testing.T) {
	dir := newTempRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("line 1\nline 2 edited\nline 3\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	data := FetchGitData(dir)
	if len(data.UnstagedFiles) != 1 {
		t.Fatalf("unstaged = %+v, want 1 file", data.UnstagedFiles)
	}
	f := data.UnstagedFiles[0]
	if f.Status != 'M' || f.Name != "a.txt" {
		t.Fatalf("got %+v", f)
	}
	if f.Additions != 2 || f.Deletions != 1 {
		t.Fatalf("numstat got +%d/-%d, want +2/-1", f.Additions, f.Deletions)
	}
	if data.DiffStat == nil || data.DiffStat.Insertions != 2 || data.DiffStat.Deletions != 1 {
		t.Fatalf("shortstat got %+v", data.DiffStat)
	}
}

func TestFetchGitDataNewUntrackedFile(t *testing.T) {
	dir := newTempRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("content\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	data := FetchGitData(dir)
	if len(data.UntrackedFiles) != 1 || data.UntrackedFiles[0] != "new.txt" {
		t.Fatalf("got %+v", data.UntrackedFiles)
	}
	if data.ChangedFileCount() != 1 {
		t.Fatalf("ChangedFileCount = %d, want 1", data.ChangedFileCount())
	}
}

func TestFetchGitDataStagedFile(t *testing.T) {
	dir := newTempRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGitCmd(t, dir, "add", "b.txt")
	data := FetchGitData(dir)
	if len(data.StagedFiles) != 1 || data.StagedFiles[0].Status != 'A' || data.StagedFiles[0].Name != "b.txt" {
		t.Fatalf("got %+v", data.StagedFiles)
	}
	if data.StagedFiles[0].Additions != 1 {
		t.Fatalf("staged numstat got %+v, want +1", data.StagedFiles[0])
	}
}

func TestFetchGitDataAheadOfUpstream(t *testing.T) {
	bareDir := t.TempDir()
	runGitCmd(t, bareDir, "init", "-q", "--bare")

	dir := newTempRepo(t)
	runGitCmd(t, dir, "remote", "add", "origin", bareDir)
	runGitCmd(t, dir, "push", "-q", "-u", "origin", "main")

	// Add a local commit to create an "ahead 1" state.
	if err := os.WriteFile(filepath.Join(dir, "c.txt"), []byte("c\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGitCmd(t, dir, "add", "c.txt")
	runGitCmd(t, dir, "commit", "-q", "-m", "add c")

	data := FetchGitData(dir)
	if data.AheadBehind == nil {
		t.Fatalf("must have AheadBehind once an upstream exists")
	}
	if data.AheadBehind.Ahead != 1 || data.AheadBehind.Behind != 0 {
		t.Fatalf("got %+v, want ahead=1 behind=0", data.AheadBehind)
	}
}

func TestFetchGitDataRemoteURLNormalized(t *testing.T) {
	dir := newTempRepo(t)
	runGitCmd(t, dir, "remote", "add", "origin", "git@github.com:user/repo.git")
	data := FetchGitData(dir)
	want := "https://github.com/user/repo"
	if data.RemoteURL != want {
		t.Fatalf("RemoteURL = %q, want %q", data.RemoteURL, want)
	}
}

// ─── FetchPRNumber: safe when gh is missing or unusable ─────────────

// This test must NOT fail regardless of whether the test machine has `gh`
// installed: a directory that isn't a git repo makes `gh pr view` always
// fail (there's no PR to look up), and if `gh` doesn't exist in PATH then
// exec fails similarly — both branches must fall into ok=false, no panic,
// no need for t.Skip.
func TestFetchPRNumberUnusableReturnsFalse(t *testing.T) {
	dir := t.TempDir()
	pr, ok := FetchPRNumber(dir)
	if ok {
		t.Fatalf("a directory that isn't a git repo can't have a PR, but got pr=%q ok=true", pr)
	}
}
