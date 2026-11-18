package activity

import (
	"os"
	"strings"
	"time"
)

// Entry is one parsed activity log line: "HH:MM|ToolName|label".
type Entry struct {
	Timestamp string
	Tool      string
	Label     string
}

// LogMtime returns the last modification time of a pane's log file, and true
// if the file exists and its metadata could be read. A missing file (the
// pane never ran the hook, or the log was deleted) returns false, not an
// error.
//
// The log is written by appending lines and occasionally overwritten when
// truncated (write.go, trimLogFile once past 210 lines) — both update the
// mtime, so the refresh loop (once per second, part 7) can compare the mtime
// against the last read to skip re-reading the whole file when nothing has
// changed.
func LogMtime(paneID string) (time.Time, bool) {
	info, err := os.Stat(LogFilePath(paneID))
	if err != nil {
		return time.Time{}, false
	}
	return info.ModTime(), true
}

// parseEntry parses a line in the format "HH:MM|ToolName|label". A missing
// timestamp or tool field (a line broken midway, e.g. a partial write) is
// treated as unparseable — returns false, never panics. A missing label
// (only 2 fields) is still valid, with the label treated as empty.
func parseEntry(line string) (Entry, bool) {
	parts := strings.SplitN(line, "|", 3)
	if len(parts) < 2 {
		return Entry{}, false
	}
	label := ""
	if len(parts) == 3 {
		label = parts[2]
	}
	return Entry{Timestamp: parts[0], Tool: parts[1], Label: label}, true
}

// ReadActivityLog reads a pane's log file and returns entries in NEWEST
// FIRST order (the reverse of the order they were written in the file — the
// last line in the file is the newest).
//
// maxEntries limits how many of the file's LAST lines are examined (0 means
// take everything); a broken line still counts toward this limit (matching
// the reference implementation: "take the last N lines, then parse" rather
// than "take N parseable entries") but is excluded from the returned
// results, so the returned slice can be shorter than maxEntries.
//
// A missing or empty file returns an empty slice, not an error — the
// sidebar is an observation tool and must not panic just because a pane
// never wrote a log.
func ReadActivityLog(paneID string, maxEntries int) []Entry {
	content, err := os.ReadFile(LogFilePath(paneID))
	if err != nil {
		return nil
	}
	trimmed := strings.TrimRight(string(content), "\n")
	if trimmed == "" {
		return nil
	}
	lines := strings.Split(trimmed, "\n")

	var entries []Entry
	taken := 0
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i] == "" {
			continue
		}
		if maxEntries > 0 && taken >= maxEntries {
			break
		}
		taken++
		if entry, ok := parseEntry(lines[i]); ok {
			entries = append(entries, entry)
		}
	}
	return entries
}

// TaskStatus is the status of a task extracted from TaskCreate/TaskUpdate in
// the activity log.
type TaskStatus int

const (
	TaskPending TaskStatus = iota
	TaskInProgress
	TaskCompleted
)

// TaskItem is a parsed task: its subject and current status.
type TaskItem struct {
	Subject string
	Status  TaskStatus
}

// TaskProgress is the aggregated task progress of a pane.
type TaskProgress struct {
	Tasks []TaskItem
}

func (p TaskProgress) Total() int { return len(p.Tasks) }

func (p TaskProgress) CompletedCount() int {
	n := 0
	for _, t := range p.Tasks {
		if t.Status == TaskCompleted {
			n++
		}
	}
	return n
}

func (p TaskProgress) InProgressCount() int {
	n := 0
	for _, t := range p.Tasks {
		if t.Status == TaskInProgress {
			n++
		}
	}
	return n
}

func (p TaskProgress) IsEmpty() bool { return len(p.Tasks) == 0 }

// AllCompleted returns true when there is at least one task and all of them
// are completed. An empty list does NOT count as "all done" — there's
// nothing to complete.
func (p TaskProgress) AllCompleted() bool {
	if len(p.Tasks) == 0 {
		return false
	}
	for _, t := range p.Tasks {
		if t.Status != TaskCompleted {
			return false
		}
	}
	return true
}

// taskInternal is the internal parsing state — it needs to keep the id so
// TaskUpdate/deletion can match the right task; the id is not exposed
// outside via TaskItem.
type taskInternal struct {
	id      string
	subject string
	status  TaskStatus
}

// splitTaskID splits a TaskCreate label of the form "#<id> <subject>" into
// (id, subject). A label that doesn't start with "#" is treated as having no
// id, with the whole string as the subject. A "#" with no following space
// yields an empty subject.
func splitTaskID(label string) (id, subject string) {
	if !strings.HasPrefix(label, "#") {
		return "", label
	}
	rest := label[1:]
	if pos := strings.IndexByte(rest, ' '); pos >= 0 {
		return rest[:pos], rest[pos+1:]
	}
	return rest, ""
}

func allTasksCompleted(tasks []taskInternal) bool {
	for _, t := range tasks {
		if t.status != TaskCompleted {
			return false
		}
	}
	return true
}

func hasTaskID(tasks []taskInternal, id string) bool {
	for _, t := range tasks {
		if t.id == id {
			return true
		}
	}
	return false
}

func setTaskStatus(tasks []taskInternal, id string, status TaskStatus) {
	for i := range tasks {
		if tasks[i].id == id {
			tasks[i].status = status
			return
		}
	}
}

func removeTaskID(tasks []taskInternal, id string) []taskInternal {
	out := tasks[:0]
	for _, t := range tasks {
		if t.id != id {
			out = append(out, t)
		}
	}
	return out
}

// ParseTaskProgress extracts task progress from a pane's activity log.
//
// entries must be in NEWEST FIRST order (as returned by ReadActivityLog) —
// the function walks backwards to process them in real chronological order
// (oldest → newest).
//
// Rules (copied from the reference implementation `parse_task_progress` in
// activity.rs):
//   - TaskResetMarker (TaskResetMarker, not a real tool): clears the
//     accumulated task list entirely — a barrier that keeps state from the
//     previous run from leaking into the new one.
//   - TaskCreate, label "#<id> <subject>" or just "<subject>": if the id
//     already exists (a restarted session reusing an id) or every existing
//     task is already completed (a fresh batch of tasks), clear the list
//     before adding — prevents an old batch from mixing with the new one.
//   - TaskUpdate, label "<completed|in_progress|deleted|...> #<id>": updates
//     the status of the task matching the id; "deleted" removes it from the
//     list entirely instead of changing its status.
func ParseTaskProgress(entries []Entry) TaskProgress {
	var tasks []taskInternal

	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		switch entry.Tool {
		case TaskResetMarker:
			tasks = nil

		case "TaskCreate":
			id, subject := splitTaskID(entry.Label)
			allDone := len(tasks) > 0 && allTasksCompleted(tasks)
			idExists := id != "" && hasTaskID(tasks, id)
			if idExists || allDone {
				tasks = nil
			}
			tasks = append(tasks, taskInternal{id: id, subject: subject, status: TaskPending})

		case "TaskUpdate":
			parts := strings.SplitN(entry.Label, " ", 2)
			if len(parts) != 2 {
				continue
			}
			statusStr := parts[0]
			id := strings.TrimPrefix(parts[1], "#")
			switch statusStr {
			case "completed":
				setTaskStatus(tasks, id, TaskCompleted)
			case "in_progress":
				setTaskStatus(tasks, id, TaskInProgress)
			case "deleted":
				tasks = removeTaskID(tasks, id)
			default:
				setTaskStatus(tasks, id, TaskPending)
			}
		}
	}

	result := TaskProgress{Tasks: make([]TaskItem, 0, len(tasks))}
	for _, t := range tasks {
		result.Tasks = append(result.Tasks, TaskItem{Subject: t.subject, Status: t.status})
	}
	return result
}
