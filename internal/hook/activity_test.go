package hook

import (
	"os"
	"strings"
	"testing"

	"tmux-agent/internal/activity"
	"tmux-agent/internal/tmux"
)

func readLogFile(t *testing.T, pane string) string {
	t.Helper()
	content, err := os.ReadFile(activity.LogFilePath(pane))
	if err != nil {
		t.Fatalf("read log file failed: %v", err)
	}
	return string(content)
}

func TestHandleActivityLogWritesLine(t *testing.T) {
	t.Setenv("TMUX_AGENT_LOG_DIR", t.TempDir())
	defer tmux.SetRunner(tmux.NewFake())()

	HandleActivityLog("%HOOK_READ", "Read", map[string]any{"file_path": "/home/user/src/main.rs"}, nil)

	content := readLogFile(t, "%HOOK_READ")
	if !strings.Contains(content, "|Read|main.rs") {
		t.Errorf("log content = %q, missing line Read|main.rs", content)
	}
}

// A background Bash run (run_in_background) must write @pane_bg_cmd = the
// sanitized command.
//
// IMPORTANT: only assert that the expected SetPaneOption call IS PRESENT in
// fake.Calls(), don't lock down the total call count (don't use
// len(calls) != N). The task handling wait/background state will extend
// HandleActivityLog to also read @pane_status/@pane_started_at (adding two
// tmux.GetPaneOptionValue calls), increasing the total call count even though
// the @pane_bg_cmd-writing behavior here hasn't changed. Locking the total
// count in a test written for the current step would break a later step's
// test even though nothing is actually wrong — check for the expected call's
// content instead of counting calls.
func TestHandleActivityLogSetsPaneBgCmdWhenBashRunsInBackground(t *testing.T) {
	t.Setenv("TMUX_AGENT_LOG_DIR", t.TempDir())
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()

	HandleActivityLog("%HOOK_BG", "Bash",
		map[string]any{"command": "npm run dev", "run_in_background": true}, nil)

	if !hasSetPaneOptionCall(fake, "%HOOK_BG", tmux.PaneBgCmd, "npm run dev") {
		t.Errorf("did not find set @pane_bg_cmd=npm run dev command in %v", fake.Calls())
	}
}

func TestHandleActivityLogBashBackgroundWithoutCommandUsesPlaceholder(t *testing.T) {
	t.Setenv("TMUX_AGENT_LOG_DIR", t.TempDir())
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()

	HandleActivityLog("%HOOK_BG_NOCMD", "Bash", map[string]any{"run_in_background": true}, nil)

	if !hasSetPaneOptionCall(fake, "%HOOK_BG_NOCMD", tmux.PaneBgCmd, tmux.BGCmdPlaceholder) {
		t.Errorf("did not find set @pane_bg_cmd=%q command in %v", tmux.BGCmdPlaceholder, fake.Calls())
	}
}

func TestHandleActivityLogSanitizesCommandBeforeWritingPaneBgCmd(t *testing.T) {
	t.Setenv("TMUX_AGENT_LOG_DIR", t.TempDir())
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()

	HandleActivityLog("%HOOK_BG_PIPE", "Bash",
		map[string]any{"command": "cat a.log | grep foo\nbar", "run_in_background": true}, nil)

	stored := setPaneOptionValue(fake, "%HOOK_BG_PIPE", tmux.PaneBgCmd)
	if stored == "" {
		t.Fatalf("did not find set @pane_bg_cmd command in %v", fake.Calls())
	}
	if strings.ContainsAny(stored, "|\n") {
		t.Errorf("@pane_bg_cmd = %q still contains | or a newline", stored)
	}
}

// runInBackground (camelCase, OpenCode's flag) must also be recognized like
// run_in_background (snake_case, Claude/Codex's flag) — two names for the
// same concept.
func TestHandleActivityLogRecognizesCamelCaseRunInBackground(t *testing.T) {
	t.Setenv("TMUX_AGENT_LOG_DIR", t.TempDir())
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()

	HandleActivityLog("%HOOK_CAMEL", "Bash",
		map[string]any{"command": "go test ./...", "runInBackground": true}, nil)

	if !hasSetPaneOptionCall(fake, "%HOOK_CAMEL", tmux.PaneBgCmd, "go test ./...") {
		t.Errorf("camelCase runInBackground must be recognized, not found in %v", fake.Calls())
	}
}

// Bash NOT running in background (missing both flag keys) must not write
// @pane_bg_cmd.
func TestHandleActivityLogBashNotInBackgroundSkipsPaneBgCmd(t *testing.T) {
	t.Setenv("TMUX_AGENT_LOG_DIR", t.TempDir())
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()

	HandleActivityLog("%HOOK_FG", "Bash", map[string]any{"command": "ls"}, nil)

	if setPaneOptionValue(fake, "%HOOK_FG", tmux.PaneBgCmd) != "" {
		t.Errorf("Bash not running in background must not set @pane_bg_cmd, but found in %v", fake.Calls())
	}
}

// A tool other than Bash must never be treated as a background shell, even
// when its payload happens to have run_in_background=true.
func TestHandleActivityLogNonBashToolIsNeverBackgroundShell(t *testing.T) {
	t.Setenv("TMUX_AGENT_LOG_DIR", t.TempDir())
	fake := tmux.NewFake()
	defer tmux.SetRunner(fake)()

	HandleActivityLog("%HOOK_NOT_BASH", "Monitor",
		map[string]any{"command": "tail -f x.log", "run_in_background": true}, nil)

	if setPaneOptionValue(fake, "%HOOK_NOT_BASH", tmux.PaneBgCmd) != "" {
		t.Errorf("a tool other than Bash must not set @pane_bg_cmd, but found in %v", fake.Calls())
	}
}

func TestHandleActivityLogTaskCreateWithResponse(t *testing.T) {
	t.Setenv("TMUX_AGENT_LOG_DIR", t.TempDir())
	defer tmux.SetRunner(tmux.NewFake())()

	HandleActivityLog("%HOOK_TASK", "TaskCreate",
		map[string]any{"subject": "Fix bug"},
		map[string]any{"task": map[string]any{"id": "42"}})

	content := readLogFile(t, "%HOOK_TASK")
	if !strings.Contains(content, "|TaskCreate|#42 Fix bug") {
		t.Errorf("log content = %q, missing line TaskCreate|#42 Fix bug", content)
	}
}

func TestHandleActivityLogNilToolInputStillWritesLineWithEmptyLabel(t *testing.T) {
	t.Setenv("TMUX_AGENT_LOG_DIR", t.TempDir())
	defer tmux.SetRunner(tmux.NewFake())()

	HandleActivityLog("%HOOK_NIL", "UnknownTool", nil, nil)

	content := readLogFile(t, "%HOOK_NIL")
	if !strings.Contains(content, "|UnknownTool|") {
		t.Errorf("log content = %q, missing line UnknownTool", content)
	}
}

// hasSetPaneOptionCall reports true if fake.Calls() contains exactly the
// command "set -t <pane> -p <key> <value>".
func hasSetPaneOptionCall(fake *tmux.Fake, pane, key, value string) bool {
	for _, call := range fake.Calls() {
		if matchesSetPaneOption(call, pane, key) && call[5] == value {
			return true
		}
	}
	return false
}

// setPaneOptionValue returns the value of the most recent set-pane-option
// call matching pane+key in fake.Calls(), or "" if there's no such call.
func setPaneOptionValue(fake *tmux.Fake, pane, key string) string {
	value := ""
	for _, call := range fake.Calls() {
		if matchesSetPaneOption(call, pane, key) {
			value = call[5]
		}
	}
	return value
}

func matchesSetPaneOption(call []string, pane, key string) bool {
	return len(call) == 6 &&
		call[0] == "set" && call[1] == "-t" && call[2] == pane &&
		call[3] == "-p" && call[4] == key
}

// A background run is forgotten once its shell is gone from the pane's
// process tree, so Stop falls back to idle instead of `background` forever.
func TestLiveMarkers(t *testing.T) {
	claude := `/usr/bin/zsh -c source /home/u/.claude/shell-snapshots/snapshot-zsh-1.sh 2>/dev/null || true && eval 'echo '\''hi there'\'' && sleep 47' < /dev/null && pwd -P >| /tmp/claude-1-cwd`
	tree := []string{"claude", claude, "sleep 47"}
	markers := []string{bgMarker("echo 'hi there' && sleep 47\nmore"), bgMarker("npm run dev")}
	if got := liveMarkers(markers, tree); len(got) != 1 || got[0] != markers[0] {
		t.Errorf("live = %q, want only the echo/sleep run", got)
	}
	if got := liveMarkers(markers, []string{"claude"}); len(got) != 0 {
		t.Errorf("finished runs must be forgotten, got %q", got)
	}
	if m := bgMarker(strings.Repeat("x", 300)); len(m) != 120 {
		t.Errorf("marker length %d", len(m))
	}
}
