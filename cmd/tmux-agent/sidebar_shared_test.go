package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSharedSidebarRealTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	binary := filepath.Join(t.TempDir(), "tmux-agent")
	buildArgs := []string{"build"}
	if os.Getenv("TMUX_AGENT_RACE") == "1" {
		buildArgs = append(buildArgs, "-race")
	}
	buildArgs = append(buildArgs, "-o", binary, ".")
	if out, err := exec.Command("go", buildArgs...).CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, out)
	}
	socket := fmt.Sprintf("sidebar-test-%d", os.Getpid())
	run := func(args ...string) (string, error) {
		cmd := exec.Command("tmux", append([]string{"-L", socket}, args...)...)
		cmd.Env = append(os.Environ(), "TMUX=", "TMPDIR=/tmp", "NO_COLOR=")
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	must := func(args ...string) string {
		t.Helper()
		out, err := run(args...)
		if err != nil {
			t.Fatalf("tmux %v: %v: %s", args, err, out)
		}
		return out
	}
	must("new-session", "-d", "-s", "shared", "-x", "120", "-y", "40")
	defer run("kill-server")
	must("set-option", "-g", "@agent_avatar", "off")
	must("set-option", "-g", "@agent_digest_llm", "off")
	first := must("new-window", "-t", "shared", "-P", "-F", "#{pane_id}", binary)
	second := must("new-window", "-t", "shared", "-P", "-F", "#{pane_id}", binary)
	getPID := func(pane string) int {
		out, err := run("show-options", "-p", "-v", "-t", pane, "@sidebar_server_pid")
		if err != nil {
			return 0
		}
		n, _ := strconv.Atoi(out)
		return n
	}
	wait := func(cond func() bool, message string) {
		t.Helper()
		until := time.Now().Add(12 * time.Second)
		for time.Now().Before(until) {
			if cond() {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal(message)
	}
	paneGone := func(pane string) bool {
		out, err := run("list-panes", "-a", "-F", "#{pane_id}")
		if err != nil {
			return true
		}
		for _, id := range strings.Fields(out) {
			if id == pane {
				return false
			}
		}
		return true
	}
	wait(func() bool { return getPID(first) > 0 && getPID(first) == getPID(second) }, "two panes did not connect to one server")
	pid := getPID(first)
	newPID := 0
	defer func() {
		if newPID > 0 {
			_ = syscall.Kill(newPID, syscall.SIGTERM)
		}
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}()
	wait(func() bool {
		out, err := run("capture-pane", "-p", "-t", second)
		return err == nil && strings.Contains(out, "no agents running")
	}, "sidebar did not render")
	colored := must("capture-pane", "-e", "-p", "-t", second)
	if !strings.Contains(colored, "\x1b[") {
		t.Fatal("shared sidebar must retain terminal colors")
	}
	must("send-keys", "-t", first, "q")
	wait(func() bool { return paneGone(first) }, "closing first sidebar did not close its pane")
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("server died when first pane closed: %v", err)
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	wait(func() bool { n := getPID(second); return n > 0 && n != pid }, "remaining pane did not reconnect after server stopped")
	newPID = getPID(second)
	wait(func() bool {
		out, err := run("capture-pane", "-p", "-t", second)
		return err == nil && strings.Contains(out, "no agents running")
	}, "sidebar did not render after reconnect")
	must("send-keys", "-t", second, "q")
	wait(func() bool { return paneGone(second) }, "reconnected sidebar did not handle input")
}
