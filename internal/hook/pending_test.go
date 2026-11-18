package hook

import (
	"os"
	"testing"

	"tmux-agent/internal/activity"
	"tmux-agent/internal/tmux"
)

// The Pending* option names are a contract with clearAllMeta and with an old
// install that used to write PendingSessionEnd (see its doc comment) — locked
// down by this test.
func TestPendingConstantsMatchTmux(t *testing.T) {
	if PendingSessionEnd != tmux.PanePendingSessionEnd {
		t.Errorf("PendingSessionEnd = %q, want %q", PendingSessionEnd, tmux.PanePendingSessionEnd)
	}
	if PendingWorktreeRemove != tmux.PanePendingWorktreeRemove {
		t.Errorf("PendingWorktreeRemove = %q, want %q", PendingWorktreeRemove, tmux.PanePendingWorktreeRemove)
	}
}

func TestMarkPendingWritesFlag1(t *testing.T) {
	fake := tmux.NewFake()
	pane := "%MARK"
	defer tmux.SetRunner(fake)()

	MarkPending(pane, PendingWorktreeRemove)

	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("want 1 call, got %d: %v", len(calls), calls)
	}
	assertSetCall(t, calls[0], pane, PendingWorktreeRemove, "1")
}

func TestDrainPendingTeardownsRunsTeardownWhenDeferMarkerPresent(t *testing.T) {
	fake := tmux.NewFake()
	pane := "%DRAIN"
	fake.Set("1", "show", "-t", pane, "-pv", PendingWorktreeRemove)
	defer tmux.SetRunner(fake)()

	DrainPendingTeardowns(pane)

	calls := fake.Calls()
	// 1 read of PendingWorktreeRemove + (RunWorktreeRemoveTeardown: 2 worktree
	// unsets via SyncWorktreeMeta, 1 cwd unset) + 1 unset of PendingWorktreeRemove.
	if len(calls) != 5 {
		t.Fatalf("want 5 calls, got %d: %v", len(calls), calls)
	}
	assertUnsetCall(t, calls[1], pane, tmux.PaneWorktreeName)
	assertUnsetCall(t, calls[2], pane, tmux.PaneWorktreeBranch)
	assertUnsetCall(t, calls[3], pane, tmux.PaneCwd)
	assertUnsetCall(t, calls[4], pane, PendingWorktreeRemove)
}

func TestDrainPendingTeardownsDoesNothingWhenNoDeferMarker(t *testing.T) {
	fake := tmux.NewFake()
	pane := "%NO_DRAIN"
	fake.Set("", "show", "-t", pane, "-pv", PendingWorktreeRemove)
	defer tmux.SetRunner(fake)()

	DrainPendingTeardowns(pane)

	calls := fake.Calls()
	if len(calls) != 1 {
		t.Fatalf("want exactly 1 call (read-only), got %d: %v", len(calls), calls)
	}
}

func TestRunSessionEndTeardownClearsAll17Keys(t *testing.T) {
	fake := tmux.NewFake()
	pane := "%SESSION_END"
	defer tmux.SetRunner(fake)()

	t.Setenv("TMUX_AGENT_LOG_DIR", t.TempDir())
	logPath := activity.LogFilePath(pane)
	if err := os.WriteFile(logPath, []byte("dummy"), 0o644); err != nil {
		t.Fatalf("create fake log file: %v", err)
	}

	RunSessionEndTeardown(pane)

	// Exactly 17 keys: 15 keys from clearAllMeta + PaneAttention (first) +
	// PaneStatusOpt (last) — matching clearedPaneKeys in internal/tmux/query.go
	// key-for-key.
	wantKeys := []string{
		tmux.PaneAttention,
		tmux.PaneAgent,
		tmux.PanePrompt,
		tmux.PanePromptSource,
		tmux.PaneBgCmd,
		tmux.PaneBgLive,
		tmux.PaneSubagents,
		tmux.PaneCwd,
		tmux.PanePermissionMode,
		tmux.PaneWorktreeName,
		tmux.PaneWorktreeBranch,
		tmux.PaneSessionID,
		PendingSessionEnd,
		PendingWorktreeRemove,
		tmux.PaneStartedAt,
		tmux.PaneWaitReason,
		tmux.PaneStatusOpt,
	}
	calls := fake.Calls()
	if len(calls) != len(wantKeys) {
		t.Fatalf("want %d unset calls, got %d: %v", len(wantKeys), len(calls), calls)
	}
	for i, key := range wantKeys {
		assertUnsetCall(t, calls[i], pane, key)
	}

	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Errorf("log file must be removed, stat returned err=%v", err)
	}
}

// RunSessionEndTeardown must not panic when the log file doesn't exist (no
// activity was ever written) — os.Remove on a non-existent file just returns
// an error, swallowed like every other error in this package.
func TestRunSessionEndTeardownDoesNotPanicWhenNoLogFileYet(t *testing.T) {
	fake := tmux.NewFake()
	pane := "%NO_LOG_FILE"
	defer tmux.SetRunner(fake)()
	t.Setenv("TMUX_AGENT_LOG_DIR", t.TempDir())

	RunSessionEndTeardown(pane)
}

func TestRunWorktreeRemoveTeardownClearsWorktreeAndCwd(t *testing.T) {
	fake := tmux.NewFake()
	pane := "%WT_REMOVE"
	defer tmux.SetRunner(fake)()

	RunWorktreeRemoveTeardown(pane)

	calls := fake.Calls()
	if len(calls) != 3 {
		t.Fatalf("want 3 calls, got %d: %v", len(calls), calls)
	}
	assertUnsetCall(t, calls[0], pane, tmux.PaneWorktreeName)
	assertUnsetCall(t, calls[1], pane, tmux.PaneWorktreeBranch)
	assertUnsetCall(t, calls[2], pane, tmux.PaneCwd)
}
