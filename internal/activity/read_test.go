package activity

import (
	"os"
	"testing"
	"time"
)

// TestReadActivityLogNewestFirst checks basic log reading: the returned
// entries must be in the reverse of the order written in the file (the last
// line in the file = the first entry in the slice).
func TestReadActivityLogNewestFirst(t *testing.T) {
	t.Setenv(logDirEnv, t.TempDir())
	pane := "%READ_BASIC"
	writeRawLog(t, pane, "10:30|Read|package.json\n10:31|Edit|sidebar.go\n10:32|Bash|go build\n")

	entries := ReadActivityLog(pane, 50)

	if len(entries) != 3 {
		t.Fatalf("want 3 entries, got %d: %+v", len(entries), entries)
	}
	if entries[0].Tool != "Bash" || entries[0].Label != "go build" {
		t.Errorf("entry 0 = %+v, want Bash/go build (newest)", entries[0])
	}
	if entries[2].Tool != "Read" || entries[2].Label != "package.json" {
		t.Errorf("entry 2 = %+v, want Read/package.json (oldest)", entries[2])
	}
}

// TestReadActivityLogLimitsToMaxEntries: maxEntries must trim down to
// exactly the last N lines, still in newest-first order.
func TestReadActivityLogLimitsToMaxEntries(t *testing.T) {
	t.Setenv(logDirEnv, t.TempDir())
	pane := "%READ_MAX"
	var log string
	for i := 0; i < 10; i++ {
		log += "10:" + padTwo(i) + "|Read|file" + padTwo(i) + ".go\n"
	}
	writeRawLog(t, pane, log)

	entries := ReadActivityLog(pane, 3)

	if len(entries) != 3 {
		t.Fatalf("want 3 entries, got %d", len(entries))
	}
	if entries[0].Label != "file09.go" || entries[1].Label != "file08.go" || entries[2].Label != "file07.go" {
		t.Errorf("want the last 3 lines newest-first, got %+v", entries)
	}
}

// TestReadActivityLogZeroMaxEntriesMeansUnlimited: maxEntries=0 means take
// everything, no limit.
func TestReadActivityLogZeroMaxEntriesMeansUnlimited(t *testing.T) {
	t.Setenv(logDirEnv, t.TempDir())
	pane := "%READ_ALL"
	writeRawLog(t, pane, "10:00|Read|a.go\n10:01|Read|b.go\n")

	entries := ReadActivityLog(pane, 0)

	if len(entries) != 2 {
		t.Fatalf("want 2 entries (unlimited), got %d", len(entries))
	}
}

// TestReadActivityLogMissingFileReturnsEmpty: a pane that never wrote a log
// — returns an empty slice, no panic, no error.
func TestReadActivityLogMissingFileReturnsEmpty(t *testing.T) {
	t.Setenv(logDirEnv, t.TempDir())

	entries := ReadActivityLog("%DOES_NOT_EXIST", 50)

	if len(entries) != 0 {
		t.Errorf("want empty slice when the file doesn't exist, got %+v", entries)
	}
}

// TestReadActivityLogSkipsBrokenLine: a line missing the "|" field
// (a partial write, e.g. a mid-write crash) must be silently skipped, no
// panic, and must not corrupt the other valid entries. A broken line still
// counts toward one slot of maxEntries (matching the reference
// implementation: take the last N lines, then parse).
func TestReadActivityLogSkipsBrokenLine(t *testing.T) {
	t.Setenv(logDirEnv, t.TempDir())
	pane := "%READ_BROKEN"
	writeRawLog(t, pane, "10:00|Read|a.go\nbroken-line-no-pipe\n10:02|Bash|go test\n")

	entries := ReadActivityLog(pane, 50)

	if len(entries) != 2 {
		t.Fatalf("want 2 valid entries (broken line skipped), got %d: %+v", len(entries), entries)
	}
	if entries[0].Tool != "Bash" || entries[1].Tool != "Read" {
		t.Errorf("wrong order/entries after skipping the broken line: %+v", entries)
	}

	// The broken line falls within the last 2 lines: only 1 valid entry is
	// returned even though maxEntries=2 was requested, because the broken
	// line already took up one slot.
	entries2 := ReadActivityLog(pane, 2)
	if len(entries2) != 1 || entries2[0].Tool != "Bash" {
		t.Errorf("want exactly 1 entry (Bash) when maxEntries=2 and the broken line takes one slot, got %+v", entries2)
	}
}

// TestReadActivityLogEmptyLabel: an empty label (only 2 fields, e.g.
// "11:02|ToolSearch|" with the trailing "|" removed by TrimRight from the
// split) must still parse successfully, with the label treated as empty.
func TestReadActivityLogEmptyLabel(t *testing.T) {
	t.Setenv(logDirEnv, t.TempDir())
	pane := "%READ_EMPTY_LABEL"
	writeRawLog(t, pane, "11:02|ToolSearch\n")

	entries := ReadActivityLog(pane, 50)

	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d", len(entries))
	}
	if entries[0].Tool != "ToolSearch" || entries[0].Label != "" {
		t.Errorf("entry = %+v, want Tool=ToolSearch Label=\"\"", entries[0])
	}
}

// TestLogMtimeExistingFile: an existing file must return the real mtime and true.
func TestLogMtimeExistingFile(t *testing.T) {
	t.Setenv(logDirEnv, t.TempDir())
	pane := "%MTIME_OK"
	writeRawLog(t, pane, "10:00|Read|a.go\n")

	mtime, ok := LogMtime(pane)
	if !ok {
		t.Fatal("want ok=true when the file exists")
	}
	if time.Since(mtime) > time.Minute {
		t.Errorf("mtime %v looks wrong (too far from now)", mtime)
	}
}

// TestLogMtimeMissingFile: a file that was never written must return
// ok=false, no panic.
func TestLogMtimeMissingFile(t *testing.T) {
	t.Setenv(logDirEnv, t.TempDir())

	_, ok := LogMtime("%MTIME_MISSING")
	if ok {
		t.Error("want ok=false when the file doesn't exist")
	}
}

// TestLogMtimeChangesOnAppend: appending a line must change the mtime —
// this is the mechanism the refresh loop uses to know whether the log needs
// to be re-read.
func TestLogMtimeChangesOnAppend(t *testing.T) {
	t.Setenv(logDirEnv, t.TempDir())
	pane := "%MTIME_CHANGE"
	writeRawLog(t, pane, "10:00|Read|a.go\n")

	mtime1, ok := LogMtime(pane)
	if !ok {
		t.Fatal("want ok=true after the first write")
	}

	// Push the modification time forward to simulate the append happening
	// later — avoids test flakiness from low-resolution system clocks on
	// some filesystems.
	future := time.Now().Add(time.Second)
	if err := os.Chtimes(LogFilePath(pane), future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	mtime2, ok := LogMtime(pane)
	if !ok {
		t.Fatal("want ok=true after changing the mtime")
	}
	if !mtime2.After(mtime1) {
		t.Errorf("want mtime2 (%v) after mtime1 (%v)", mtime2, mtime1)
	}
}

// ─── parse_task_progress ──────────────────────────────────────────────

func taskEntry(tool, label string) Entry {
	return Entry{Timestamp: "10:00", Tool: tool, Label: label}
}

// TestParseTaskProgressBasic ported from test_parse_task_progress_basic:
// entries newest first, must yield 2 tasks with the correct status per id.
func TestParseTaskProgressBasic(t *testing.T) {
	entries := []Entry{
		taskEntry("TaskUpdate", "completed #1"),
		taskEntry("TaskUpdate", "in_progress #2"),
		taskEntry("TaskUpdate", "in_progress #1"),
		taskEntry("TaskCreate", "#2 Wire RepoGroup"),
		taskEntry("TaskCreate", "#1 Add RepoGroup"),
	}

	progress := ParseTaskProgress(entries)

	if progress.Total() != 2 {
		t.Fatalf("want 2 tasks, got %d", progress.Total())
	}
	if progress.CompletedCount() != 1 || progress.InProgressCount() != 1 {
		t.Errorf("want 1 completed, 1 in_progress, got completed=%d in_progress=%d",
			progress.CompletedCount(), progress.InProgressCount())
	}
	if progress.Tasks[0].Subject != "Add RepoGroup" || progress.Tasks[0].Status != TaskCompleted {
		t.Errorf("task 0 = %+v", progress.Tasks[0])
	}
	if progress.Tasks[1].Subject != "Wire RepoGroup" || progress.Tasks[1].Status != TaskInProgress {
		t.Errorf("task 1 = %+v", progress.Tasks[1])
	}
}

// TestParseTaskProgressEmpty: a log with no TaskCreate/TaskUpdate at all
// must yield an empty TaskProgress.
func TestParseTaskProgressEmpty(t *testing.T) {
	entries := []Entry{
		taskEntry("Read", "file.go"),
		taskEntry("Bash", "go build"),
	}

	progress := ParseTaskProgress(entries)

	if !progress.IsEmpty() {
		t.Errorf("want empty, got %+v", progress.Tasks)
	}
}

// TestParseTaskProgressDeletedTaskRemoved ported from
// test_parse_task_progress_deleted: "deleted" must remove the task from the
// list entirely, not just set a status.
func TestParseTaskProgressDeletedTaskRemoved(t *testing.T) {
	entries := []Entry{
		taskEntry("TaskUpdate", "deleted #3"),
		taskEntry("TaskUpdate", "in_progress #3"),
		taskEntry("TaskUpdate", "in_progress #2"),
		taskEntry("TaskUpdate", "completed #1"),
		taskEntry("TaskCreate", "#3 Temp task"),
		taskEntry("TaskCreate", "#2 Real task B"),
		taskEntry("TaskCreate", "#1 Real task A"),
	}

	progress := ParseTaskProgress(entries)

	if progress.Total() != 2 {
		t.Fatalf("want 2 remaining tasks (not all_completed), got %d", progress.Total())
	}
	if progress.Tasks[0].Subject != "Real task A" || progress.Tasks[1].Subject != "Real task B" {
		t.Errorf("wrong task order/content: %+v", progress.Tasks)
	}
}

// TestParseTaskProgressResetsWhenAllCompleted ported from
// test_parse_task_progress_resets_when_all_completed: once the old batch is
// fully completed and a new batch starts (TaskCreate again), the old batch
// must be cleared entirely.
func TestParseTaskProgressResetsWhenAllCompleted(t *testing.T) {
	entries := []Entry{
		taskEntry("TaskUpdate", "in_progress #3"),
		taskEntry("TaskCreate", "#3 New batch task"),
		taskEntry("TaskUpdate", "completed #2"),
		taskEntry("TaskUpdate", "completed #1"),
		taskEntry("TaskUpdate", "in_progress #2"),
		taskEntry("TaskUpdate", "in_progress #1"),
		taskEntry("TaskCreate", "#2 Old task B"),
		taskEntry("TaskCreate", "#1 Old task A"),
	}

	progress := ParseTaskProgress(entries)

	if progress.Total() != 1 {
		t.Fatalf("want 1 task (old batch cleared), got %d: %+v", progress.Total(), progress.Tasks)
	}
	if progress.Tasks[0].Subject != "New batch task" || progress.Tasks[0].Status != TaskInProgress {
		t.Errorf("task = %+v", progress.Tasks[0])
	}
}

// TestParseTaskProgressNoResetWhileInProgress ported from
// test_parse_task_progress_no_reset_while_in_progress: if the old batch
// still has an unfinished task, a new TaskCreate must NOT clear the old
// batch.
func TestParseTaskProgressNoResetWhileInProgress(t *testing.T) {
	entries := []Entry{
		taskEntry("TaskCreate", "#3 Extra task"),
		taskEntry("TaskUpdate", "completed #1"),
		taskEntry("TaskUpdate", "in_progress #2"),
		taskEntry("TaskUpdate", "in_progress #1"),
		taskEntry("TaskCreate", "#2 Task B"),
		taskEntry("TaskCreate", "#1 Task A"),
	}

	progress := ParseTaskProgress(entries)

	if progress.Total() != 3 {
		t.Fatalf("want to keep all 3 tasks (task #2 still in_progress), got %d", progress.Total())
	}
}

// TestParseTaskProgressResetsOnMarker ported from
// test_parse_task_progress_resets_on_marker: TaskResetMarker must clear
// everything before it, preventing state from a previous run leaking into
// the new one.
func TestParseTaskProgressResetsOnMarker(t *testing.T) {
	entries := []Entry{
		taskEntry("TaskUpdate", "in_progress #3"),
		taskEntry("TaskCreate", "#3 Fresh task"),
		taskEntry(TaskResetMarker, ""),
		taskEntry("TaskUpdate", "in_progress #2"),
		taskEntry("TaskUpdate", "completed #1"),
		taskEntry("TaskUpdate", "in_progress #1"),
		taskEntry("TaskCreate", "#2 Stale task B"),
		taskEntry("TaskCreate", "#1 Stale task A"),
	}

	progress := ParseTaskProgress(entries)

	if progress.Total() != 1 {
		t.Fatalf("want 1 task (everything before the marker cleared), got %d: %+v", progress.Total(), progress.Tasks)
	}
	if progress.Tasks[0].Subject != "Fresh task" || progress.Tasks[0].Status != TaskInProgress {
		t.Errorf("task = %+v", progress.Tasks[0])
	}
}

// TestParseTaskProgressResetsOnDuplicateID ported from
// test_parse_task_progress_resets_on_new_session: a new session reusing id
// #1 (without having completed the whole old batch) must still clear the
// old batch because of the duplicate id.
func TestParseTaskProgressResetsOnDuplicateID(t *testing.T) {
	entries := []Entry{
		taskEntry("TaskUpdate", "in_progress #1"),
		taskEntry("TaskCreate", "#1 New task"),
		taskEntry("Read", "file.go"),
		taskEntry("TaskUpdate", "completed #3"),
		taskEntry("TaskUpdate", "completed #2"),
		taskEntry("TaskUpdate", "completed #1"),
		taskEntry("TaskCreate", "#3 Old task C"),
		taskEntry("TaskCreate", "#2 Old task B"),
		taskEntry("TaskCreate", "#1 Old task A"),
	}

	progress := ParseTaskProgress(entries)

	if progress.Total() != 1 {
		t.Fatalf("want 1 task (old batch cleared due to duplicate id), got %d: %+v", progress.Total(), progress.Tasks)
	}
	if progress.Tasks[0].Subject != "New task" || progress.Tasks[0].Status != TaskInProgress {
		t.Errorf("task = %+v", progress.Tasks[0])
	}
}

// TestParseTaskProgressAllCompleted checks the AllCompleted helper: empty
// doesn't count as "all done", having an uncompleted task also doesn't,
// only true when every task is completed.
func TestParseTaskProgressAllCompleted(t *testing.T) {
	empty := TaskProgress{}
	if empty.AllCompleted() {
		t.Error("an empty list must not count as AllCompleted")
	}

	mixed := TaskProgress{Tasks: []TaskItem{{Status: TaskCompleted}, {Status: TaskInProgress}}}
	if mixed.AllCompleted() {
		t.Error("AllCompleted must be false while a task is not yet completed")
	}

	done := TaskProgress{Tasks: []TaskItem{{Status: TaskCompleted}, {Status: TaskCompleted}}}
	if !done.AllCompleted() {
		t.Error("AllCompleted must be true when every task is completed")
	}
}

// ─── raw log writer helper for tests ───────────────────────────────────

// writeRawLog writes the log content directly to the pane's LogFilePath
// path (with TMUX_AGENT_LOG_DIR already set to a temp dir) — used when a
// test needs precise control over line content (including broken lines),
// instead of going through WriteEntry.
func writeRawLog(t *testing.T, pane, content string) {
	t.Helper()
	if err := os.WriteFile(LogFilePath(pane), []byte(content), 0o644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}
}

// padTwo pads a 0-9 digit to 2 digits, used to build fake timestamps in tests.
func padTwo(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}
