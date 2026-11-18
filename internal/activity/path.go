// Package activity manages the activity log file for each pane.
//
// Part 1 only needs the path: the query layer deletes this file when
// cleaning up a dead agent pane. Reading/writing the log belongs to part 2.
package activity

import (
	"os"
	"strings"
)

// logDirEnv is the environment variable that overrides the root directory
// holding log files.
//
// THIS IS NOT AN END-USER CONFIGURATION FEATURE. It exists SOLELY to
// isolate fixtures from real logs: pane ids are a per-tmux-server counter
// (restarting from %0), so an isolated tmux server spun up by a fixture
// (assets/fixtures/scenarios/common/_lib.sh) can perfectly well allocate the
// same pane id (%1, %7, …) as a pane running a real agent on the user's
// default server — producing the SAME log path.
// In the real product nobody sets this variable, so the default behavior
// (/tmp/tmux-agent-activity<pane id>.log) doesn't change one bit.
//
// The hook (writer side) and the sidebar (reader side) are both this same
// binary and both call LogFilePath, so as long as both read this variable
// the path stays consistent between writes and reads.
const logDirEnv = "TMUX_AGENT_LOG_DIR"

// logDir returns the root directory holding log files: defaults to "/tmp",
// overridable via TMUX_AGENT_LOG_DIR (see the comment on logDirEnv — for
// test fixtures only).
func logDir() string {
	if dir := os.Getenv(logDirEnv); dir != "" {
		return dir
	}
	return "/tmp"
}

// LogFilePath returns the log file path for a pane.
//
// Contract with the hook: pane id "%3" → "<logDir>/tmux-agent-activity_3.log".
// The "%" character isn't safely usable in filenames, so it's replaced with
// "_". The filename stays the same no matter what logDir is — only the root
// directory changes.
func LogFilePath(paneID string) string {
	return logDir() + "/tmux-agent-activity" + strings.ReplaceAll(paneID, "%", "_") + ".log"
}
