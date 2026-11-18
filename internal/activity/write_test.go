package activity

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestSanitizeLabel(t *testing.T) {
	cases := map[string]string{
		"clean":        "clean",
		"a|b":          "a b",
		"line1\nline2": "line1 line2",
		"a|b\nc|d":     "a b c d",
		"a\rb":         "a b",
		"":             "",
	}
	for in, want := range cases {
		if got := SanitizeLabel(in); got != want {
			t.Errorf("SanitizeLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

// Calling WriteEntry twice in a row must create the file then append, not
// overwrite — and each line must match the format "HH:MM|Tool|label".
func TestWriteEntryCreatesAndAppends(t *testing.T) {
	t.Setenv(logDirEnv, t.TempDir())
	pane := "%WRITE_TEST"

	WriteEntry(pane, "Read", "main.rs")
	WriteEntry(pane, "Edit", "lib.rs")

	content, err := os.ReadFile(LogFilePath(pane))
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(content), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(lines), content)
	}
	if !strings.HasSuffix(lines[0], "|Read|main.rs") {
		t.Errorf("line 0 = %q, want it to end with |Read|main.rs", lines[0])
	}
	if !strings.HasSuffix(lines[1], "|Edit|lib.rs") {
		t.Errorf("line 1 = %q, want it to end with |Edit|lib.rs", lines[1])
	}
	if lines[0][2] != ':' {
		t.Errorf("character 3 of line 0 must be ':' (HH:MM format), line = %q", lines[0])
	}
}

// A label containing "|" and "\n" must be sanitized before writing: it must
// not produce a fake extra log line, and must not add a fake field to the
// line.
func TestWriteEntrySanitizesLabel(t *testing.T) {
	t.Setenv(logDirEnv, t.TempDir())
	pane := "%WRITE_SANITIZE"

	WriteEntry(pane, "Bash", "cat file | grep foo\nbar")

	content, err := os.ReadFile(LogFilePath(pane))
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(content), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("a newline in the label must not create a new log line, got %d lines: %q", len(lines), content)
	}
	parts := strings.SplitN(lines[0], "|", 3)
	if len(parts) != 3 {
		t.Fatalf("log line has the wrong HH:MM|Tool|label format: %q", lines[0])
	}
	if strings.ContainsAny(parts[2], "|\n") {
		t.Errorf("label = %q still contains | or a newline", parts[2])
	}
}

// Writing past the 210-line threshold must trim down to <=210, keeping the
// NEWEST lines.
func TestWriteEntryTrimsWhenOverThreshold(t *testing.T) {
	t.Setenv(logDirEnv, t.TempDir())
	pane := "%WRITE_TRIM"

	for i := 1; i <= 215; i++ {
		WriteEntry(pane, "Read", fmt.Sprintf("file%d.rs", i))
	}

	content, err := os.ReadFile(LogFilePath(pane))
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(content), "\n"), "\n")
	if len(lines) > 210 {
		t.Fatalf("want <= 210 lines after trimming, got %d", len(lines))
	}
	last := lines[len(lines)-1]
	if !strings.HasSuffix(last, "|Read|file215.rs") {
		t.Errorf("last line = %q, want it to end with |Read|file215.rs (the newest line must be kept)", last)
	}
}

func TestTrimLogFileUnderThresholdUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/trim_under.log"
	if err := os.WriteFile(path, []byte("line1\nline2\nline3\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	trimLogFile(path, 2, 5)

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(strings.Split(strings.TrimRight(string(content), "\n"), "\n")); got != 3 {
		t.Errorf("under threshold must keep all 3 lines, got %d", got)
	}
}

func TestTrimLogFileOverThresholdKeepsNewestLines(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/trim_over.log"
	var b strings.Builder
	for i := 1; i <= 15; i++ {
		fmt.Fprintf(&b, "line%d\n", i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	trimLogFile(path, 5, 10)

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(content), "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("want 5 lines left, got %d: %v", len(lines), lines)
	}
	if lines[0] != "line11" || lines[4] != "line15" {
		t.Errorf("must keep the 5 newest lines (line11..line15), got %v", lines)
	}
}

// Exactly at the threshold (not EXCEEDING it) means no trim yet — the
// condition is "> threshold", not ">= threshold".
func TestTrimLogFileAtThresholdUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/trim_exact.log"
	var b strings.Builder
	for i := 1; i <= 10; i++ {
		fmt.Fprintf(&b, "line%d\n", i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	trimLogFile(path, 5, 10)

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(strings.Split(strings.TrimRight(string(content), "\n"), "\n")); got != 10 {
		t.Errorf("exactly at threshold (not exceeding it) must keep all 10 lines, got %d", got)
	}
}

func TestTrimLogFileMissingFileNoPanic(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/does-not-exist.log"

	trimLogFile(path, 5, 10) // must not panic, must not create the file
	if _, err := os.Stat(path); err == nil {
		t.Error("trimLogFile must not create the file when the source doesn't exist")
	}
}
