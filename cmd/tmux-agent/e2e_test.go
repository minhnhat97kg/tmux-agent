package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"tmux-agent/internal/activity"
	"tmux-agent/internal/tmux"
)

// TestHookClaudeEndToEndOnRealTmux sets up an ISOLATED tmux server, builds
// the REAL tmux-agent binary, runs "hook claude <event>" for 5 consecutive
// events on a pane, then reads back the pane option via internal/tmux
// (calling raw "tmux" — the same server thanks to the shared TMUX_TMPDIR) to
// assert on the state.
//
// Covers 5/5 of the events required by the last task of phase 2b:
// session-start, activity-log, notification, user-prompt-submit, stop — and
// exercises the state chain idle → (running via activity-log) → waiting →
// running → idle.
//
// TWO PITFALLS ALREADY AVOIDED (see internal/tmux/options_test.go,
// TestGetOptionAndGetAllGlobalOptionsAgreeOnRealTmux — the original template,
// reapplied verbatim here):
//  1. A long temp directory breaks the unix socket ("name too long") →
//     use t.TempDir() directly, without appending extra segments.
//  2. NEVER use "tmux -L <name>": it spins up a named server, entirely
//     different from the "bare tmux" server the child binary calls — the two
//     sides would point at two different servers with no error at all.
//     Isolate ONLY via unset TMUX + TMUX_TMPDIR.
func TestHookClaudeEndToEndOnRealTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available on this machine, skipping integration test")
	}

	// ─── Build the real binary BEFORE touching tmux environment variables ───
	binPath := filepath.Join(t.TempDir(), "tmux-agent")
	if out, err := exec.Command("go", "build", "-o", binPath, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	// ─── Isolation: separate tmux server + separate log directory ───
	if tmuxVar, had := os.LookupEnv("TMUX"); had {
		os.Unsetenv("TMUX")
		t.Cleanup(func() { os.Setenv("TMUX", tmuxVar) })
	}
	t.Setenv("TMUX_TMPDIR", t.TempDir())
	t.Setenv("TMUX_AGENT_LOG_DIR", t.TempDir())

	// Do not load the user's tmux hooks or background scraper in this server.
	if out, err := exec.Command("tmux", "-f", "/dev/null", "new-session", "-d", "-x", "80", "-y", "24").CombinedOutput(); err != nil {
		t.Fatalf("tmux new-session: %v\n%s", err, out)
	}
	t.Cleanup(func() { exec.Command("tmux", "kill-server").Run() })

	out, err := exec.Command("tmux", "display-message", "-p", "#{pane_id}").Output()
	if err != nil {
		t.Fatalf("get pane id: %v", err)
	}
	paneID := strings.TrimSpace(string(out))
	if paneID == "" {
		t.Fatal("empty pane id")
	}
	t.Setenv("TMUX_PANE", paneID)

	// runHook runs the real binary: "tmux-agent hook claude <event>" with the
	// payload on stdin, inheriting all the isolated environment variables set
	// above (Env nil = inherit the test process's current os.Environ(),
	// which reflects every t.Setenv/os.Setenv call made before it).
	runHook := func(externalName, payload string) {
		t.Helper()
		cmd := exec.Command(binPath, "hook", "claude", externalName)
		cmd.Stdin = strings.NewReader(payload)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("hook claude %s: %v\n%s", externalName, err, out)
		}
	}

	getOpt := func(key string) string {
		t.Helper()
		return tmux.GetPaneOptionValue(paneID, key)
	}

	const cwd = "/tmp/e2e-repo"
	const sessionID = "sess-e2e-1"

	// 1) session-start → OnSessionStart ends by calling setStatus(StatusIdle)
	// (internal/hook/handlers.go) — the pane just opened, no tool use has run
	// yet.
	runHook("session-start", `{"cwd":"`+cwd+`","permission_mode":"default","source":"startup","session_id":"`+sessionID+`"}`)
	if got := getOpt(tmux.PaneAgent); got != "claude" {
		t.Fatalf("after session-start: @pane_agent = %q, want %q", got, "claude")
	}
	if got := getOpt(tmux.PaneCwd); got != cwd {
		t.Fatalf("after session-start: @pane_cwd = %q, want %q", got, cwd)
	}
	if got := getOpt(tmux.PaneSessionID); got != sessionID {
		t.Fatalf("after session-start: @pane_session_id = %q, want %q", got, sessionID)
	}
	if got := getOpt(tmux.PaneStatusOpt); got != string(tmux.StatusIdle) {
		t.Fatalf("after session-start: @pane_status = %q, want %q", got, tmux.StatusIdle)
	}

	// 2) activity-log — status is currently "idle" (not empty, not "running")
	// so activityLogStatusTransition (internal/hook/handlers.go) moves the
	// pane to running: a new tool use is always treated as "the agent is
	// working". Assert both the state transition and that the isolated log
	// file (TMUX_AGENT_LOG_DIR) got written.
	runHook("activity-log", `{"tool_name":"Read","tool_input":{"file_path":"`+cwd+`/main.go"}}`)
	if got := getOpt(tmux.PaneStatusOpt); got != string(tmux.StatusRunning) {
		t.Fatalf("after activity-log: @pane_status = %q, want %q", got, tmux.StatusRunning)
	}
	logPath := activity.LogFilePath(paneID)
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read activity log %s: %v", logPath, err)
	}
	if !strings.Contains(string(logData), "Read") {
		t.Fatalf("activity log %s does not contain the tool name that was sent: %q", logPath, logData)
	}

	// 3) notification (permission) → running → waiting. The permission group
	// ALWAYS wins even without a background shell
	// (ResolveNotificationStatus("permission", false) == StatusWaiting).
	runHook("notification", `{"cwd":"`+cwd+`","permission_mode":"default","notification_type":"permission","session_id":"`+sessionID+`"}`)
	if got := getOpt(tmux.PaneStatusOpt); got != string(tmux.StatusWaiting) {
		t.Fatalf("after notification(permission): @pane_status = %q, want %q", got, tmux.StatusWaiting)
	}
	if got := getOpt(tmux.PaneWaitReason); got != "permission" {
		t.Fatalf("after notification(permission): @pane_wait_reason = %q, want %q", got, "permission")
	}

	// 4) user-prompt-submit (user replies to the prompt, sends another
	// message) → running.
	runHook("user-prompt-submit", `{"cwd":"`+cwd+`","permission_mode":"default","prompt":"continue","session_id":"`+sessionID+`"}`)
	if got := getOpt(tmux.PaneStatusOpt); got != string(tmux.StatusRunning) {
		t.Fatalf("after user-prompt-submit: @pane_status = %q, want %q", got, tmux.StatusRunning)
	}

	// 5) stop (no background shell) → running → idle
	// (ResolveStopStatus(false) == StatusIdle).
	runHook("stop", `{"cwd":"`+cwd+`","permission_mode":"default","last_assistant_message":"all done","session_id":"`+sessionID+`"}`)
	if got := getOpt(tmux.PaneStatusOpt); got != string(tmux.StatusIdle) {
		t.Fatalf("after stop: @pane_status = %q, want %q", got, tmux.StatusIdle)
	}
}
