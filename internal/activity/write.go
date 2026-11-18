package activity

import (
	"os"
	"strings"

	"tmux-agent/internal/clock"
)

const (
	// maxLogLines is the number of lines kept after trimming.
	maxLogLines = 200
	// trimThreshold is the threshold that triggers trimming: only trims
	// when the LINE COUNT EXCEEDS (not >=) this value.
	trimThreshold = 210
)

// SanitizeLabel cleans up a string before it's written into a log line,
// into a free-form tmux option (prompt, response, background command,
// @pane_bg_cmd...), or into an anti-duplicate notification fingerprint —
// all three places share the same "|"-delimited field format. "|" and "\n"
// break the display/field-splitting format ("HH:MM|Tool|label",
// "ts|fingerprint"), and "\r" can create a fake overflowing line in a
// single-line tmux option — all three are replaced with a space. Without
// this, a label like "a|b" would be misread as having an extra field, or a
// multi-line label/value would get split into multiple fake log/display
// lines.
func SanitizeLabel(label string) string {
	return strings.NewReplacer("\n", " ", "|", " ", "\r", " ").Replace(label)
}

// WriteEntry writes one activity line to the pane's log and then trims it if
// it's over the threshold. Line format: "HH:MM|ToolName|label\n" — this is a
// CONTRACT with the reading layer in part 3-4, not to be changed casually.
//
// Open/write errors are deliberately swallowed: the hook runs inside a
// real user agent session — a pane that just closed, wrong file
// permissions, or a full /tmp must not be allowed to break the whole agent
// over a failed log line.
func WriteEntry(paneID, toolName, label string) {
	path := LogFilePath(paneID)
	line := clock.Now().Format("15:04") + "|" + toolName + "|" + SanitizeLabel(label) + "\n"

	if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		f.WriteString(line)
		f.Close()
	}

	trimLogFile(path, maxLogLines, trimThreshold)
}

// trimLogFile trims the log file down to `keep` lines once the line count
// exceeds `threshold`. Reads the whole file back and overwrites it —
// acceptable since the log is at most about `threshold` short lines, not
// worth the complexity of writing incrementally instead.
func trimLogFile(path string, keep, threshold int) {
	content, err := os.ReadFile(path)
	if err != nil {
		return
	}
	trimmed := strings.TrimRight(string(content), "\n")
	if trimmed == "" {
		return
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) <= threshold {
		return
	}
	start := len(lines) - keep
	os.WriteFile(path, []byte(strings.Join(lines[start:], "\n")+"\n"), 0o644)
}
