package hook

import (
	"testing"

	"tmux-agent/internal/event"
	"tmux-agent/internal/tmux"
)

func TestResolveCwdPrefersWorktreeOriginalRepoDir(t *testing.T) {
	wt := &event.WorktreeInfo{
		Name:            "feat",
		Path:            "/tmp/wt",
		Branch:          "feat",
		OriginalRepoDir: "/home/user/repo",
	}
	if got := ResolveCwd("/tmp/wt/src", wt); got != "/home/user/repo" {
		t.Errorf("ResolveCwd = %q, want %q", got, "/home/user/repo")
	}
}

func TestResolveCwdNoWorktreeUsesRawCwd(t *testing.T) {
	if got := ResolveCwd("/tmp/project", nil); got != "/tmp/project" {
		t.Errorf("ResolveCwd = %q, want %q", got, "/tmp/project")
	}
}

func TestResolveCwdWorktreeWithEmptyOriginalRepoDirFallsBackToRawCwd(t *testing.T) {
	wt := &event.WorktreeInfo{Name: "feat", Path: "/tmp/wt", Branch: "feat", OriginalRepoDir: ""}
	if got := ResolveCwd("/tmp/wt/src", wt); got != "/tmp/wt/src" {
		t.Errorf("ResolveCwd = %q, want %q", got, "/tmp/wt/src")
	}
}

func TestShouldUpdateCwdNoSubagent(t *testing.T) {
	if !ShouldUpdateCwd("") {
		t.Error("want true with no subagent")
	}
}

func TestShouldUpdateCwdOneSubagent(t *testing.T) {
	if ShouldUpdateCwd("Explore:sub-1") {
		t.Error("want false with one subagent")
	}
}

func TestShouldUpdateCwdMultipleSubagents(t *testing.T) {
	if ShouldUpdateCwd("Explore:sub-1,Plan:sub-2") {
		t.Error("want false with several subagents")
	}
}

func TestSyncPaneLocationSkipsWorktreeWriteWhileSubagentRunning(t *testing.T) {
	fake := tmux.NewFake()
	pane := "%PARENT"
	// parent pane has a subagent running — every pane-level write must be blocked.
	fake.Set("Explore:sub-1", "show", "-t", pane, "-pv", tmux.PaneSubagents)
	defer tmux.SetRunner(fake)()

	childWt := &event.WorktreeInfo{
		Name:            "child-feat",
		Path:            "/wt/child",
		Branch:          "feat/child",
		OriginalRepoDir: "/repo/child",
	}
	SyncPaneLocation(pane, "/repo/child", childWt, "child-session")

	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("want exactly 1 call (only reading @pane_subagents), got %d: %v", len(calls), calls)
	}
	if calls[0][0] != "show" {
		t.Fatalf("the only call must be a read, got: %v", calls[0])
	}
}

func TestSyncPaneLocationWritesWhenNoSubagent(t *testing.T) {
	fake := tmux.NewFake()
	pane := "%LONE"
	fake.Set("", "show", "-t", pane, "-pv", tmux.PaneSubagents)
	defer tmux.SetRunner(fake)()

	wt := &event.WorktreeInfo{
		Name:            "feat-x",
		Path:            "/wt/feat-x",
		Branch:          "feat-x",
		OriginalRepoDir: "/repo",
	}
	SyncPaneLocation(pane, "/wt/feat-x", wt, "sess-1")

	calls := fake.Calls()
	// 1 read of @pane_subagents + 4 writes (session id, cwd, worktree name, worktree branch).
	if len(calls) != 5 {
		t.Fatalf("want 5 calls, got %d: %v", len(calls), calls)
	}
	assertSetCall(t, calls[1], pane, tmux.PaneSessionID, "sess-1")
	// resolve_cwd puts OriginalRepoDir into @pane_cwd, not the raw cwd.
	assertSetCall(t, calls[2], pane, tmux.PaneCwd, "/repo")
	assertSetCall(t, calls[3], pane, tmux.PaneWorktreeName, "feat-x")
	assertSetCall(t, calls[4], pane, tmux.PaneWorktreeBranch, "feat-x")
}

func TestSyncWorktreeMetaClearsBothWhenWorktreeNil(t *testing.T) {
	fake := tmux.NewFake()
	pane := "%CLEAR"
	defer tmux.SetRunner(fake)()

	SyncWorktreeMeta(pane, nil)

	calls := fake.Calls()
	if len(calls) != 2 {
		t.Fatalf("want 2 unset calls, got %d: %v", len(calls), calls)
	}
	assertUnsetCall(t, calls[0], pane, tmux.PaneWorktreeName)
	assertUnsetCall(t, calls[1], pane, tmux.PaneWorktreeBranch)
}

// Regression: payload Some(worktree) but only has Name, Branch is empty —
// the old @pane_worktree_branch must be cleared separately instead of
// surviving from the previous run.
func TestSyncWorktreeMetaClearsEmptyFieldSeparately(t *testing.T) {
	fake := tmux.NewFake()
	pane := "%PARTIAL"
	defer tmux.SetRunner(fake)()

	SyncWorktreeMeta(pane, &event.WorktreeInfo{
		Name:            "new-name",
		Path:            "/wt/new",
		Branch:          "",
		OriginalRepoDir: "/repo",
	})

	calls := fake.Calls()
	if len(calls) != 2 {
		t.Fatalf("want 2 calls, got %d: %v", len(calls), calls)
	}
	assertSetCall(t, calls[0], pane, tmux.PaneWorktreeName, "new-name")
	assertUnsetCall(t, calls[1], pane, tmux.PaneWorktreeBranch)
}

func TestPaneWritesAllowedFollowsSubagentPresence(t *testing.T) {
	fake := tmux.NewFake()
	pane := "%ALLOWED"
	fake.Set("", "show", "-t", pane, "-pv", tmux.PaneSubagents)
	defer tmux.SetRunner(fake)()

	if !PaneWritesAllowed(pane) {
		t.Error("no subagent must allow writes")
	}

	fake.Set("Explore:sub-1", "show", "-t", pane, "-pv", tmux.PaneSubagents)
	if PaneWritesAllowed(pane) {
		t.Error("a subagent present must block writes")
	}
}

// assertSetCall asserts that a command in fake.Calls() is exactly
// `set -t <pane> -p <key> <value>`.
func assertSetCall(t *testing.T, call []string, pane, key, value string) {
	t.Helper()
	want := []string{"set", "-t", pane, "-p", key, value}
	if !equalStrSlice(call, want) {
		t.Errorf("wrong set call: got %v, want %v", call, want)
	}
}

// assertUnsetCall asserts that a command in fake.Calls() is exactly
// `set -t <pane> -p -u <key>`.
func assertUnsetCall(t *testing.T, call []string, pane, key string) {
	t.Helper()
	want := []string{"set", "-t", pane, "-p", "-u", key}
	if !equalStrSlice(call, want) {
		t.Errorf("wrong unset call: got %v, want %v", call, want)
	}
}

func equalStrSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
