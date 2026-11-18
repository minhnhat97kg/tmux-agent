package activity

import "testing"

// The log path is a contract with the hook. pane "%3" must produce
// /tmp/tmux-agent-activity_3.log — "%" becomes "_", with NO extra "_".
// TMUX_AGENT_LOG_DIR is not set here: this is precisely the default
// behavior of the real product, and it must resolve to /tmp.
func TestLogFilePath(t *testing.T) {
	cases := map[string]string{
		"%3":  "/tmp/tmux-agent-activity_3.log",
		"%12": "/tmp/tmux-agent-activity_12.log",
		"":    "/tmp/tmux-agent-activity.log",
	}
	for paneID, want := range cases {
		if got := LogFilePath(paneID); got != want {
			t.Errorf("LogFilePath(%q) = %q, want %q", paneID, got, want)
		}
	}
}

// TestLogFilePathRespectsTMUXAgentLogDir checks the branch where the
// environment variable is set: the root directory must change, but the
// filename (the "%" → "_" rule) must stay the same — this is the crux that
// lets fixtures stay isolated from real logs without breaking the path
// contract between the hook and the sidebar.
func TestLogFilePathRespectsTMUXAgentLogDir(t *testing.T) {
	t.Setenv("TMUX_AGENT_LOG_DIR", "/tmp/fixture-xyz")

	got := LogFilePath("%3")
	want := "/tmp/fixture-xyz/tmux-agent-activity_3.log"
	if got != want {
		t.Errorf("LogFilePath(%q) with TMUX_AGENT_LOG_DIR set = %q, want %q", "%3", got, want)
	}
}

// TestLogFilePathEmptyEnvTreatedAsUnset: an empty ("") environment variable
// must be treated as unset, falling back to the default /tmp — this avoids
// the case where a shell exporting an empty variable accidentally makes the
// log get written into "/" itself (logDir() + "/...").
func TestLogFilePathEmptyEnvTreatedAsUnset(t *testing.T) {
	t.Setenv("TMUX_AGENT_LOG_DIR", "")

	got := LogFilePath("%3")
	want := "/tmp/tmux-agent-activity_3.log"
	if got != want {
		t.Errorf("LogFilePath(%q) with TMUX_AGENT_LOG_DIR empty = %q, want %q", "%3", got, want)
	}
}
