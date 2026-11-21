package assets

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSidebarLayoutHook(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	socket := fmt.Sprintf("tmux-agent-layout-%d", os.Getpid())
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("tmux", append([]string{"-L", socket}, args...)...)
		cmd.Env = append(os.Environ(), "TMUX=")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("tmux %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("new-session", "-d", "-x", "200", "-y", "40")
	defer exec.Command("tmux", "-L", socket, "kill-server").Run()
	run("set", "-g", "@sidebar_auto_create", "off")
	run("set", "-g", "@agent_sidebar_bin", "/usr/bin/true")
	config, err := filepath.Abs("agent-sidebar.conf")
	if err != nil {
		t.Fatal(err)
	}
	run("source-file", config)
	pane := run("split-window", "-h", "-P", "-F", "#{pane_id}", "-l", "20")
	run("set", "-p", "-t", pane, "@pane_role", "sidebar")
	run("select-layout", "even-horizontal")
	waitWidth := func(want string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if run("display-message", "-p", "-t", pane, "#{pane_width}") == want {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("sidebar did not reach width %s", want)
	}
	waitWidth("30")
	run("resize-pane", "-t", pane, "-x", "40")
	waitWidth("30")
	run("select-layout", "even-vertical")
	waitWidth("200")
}
