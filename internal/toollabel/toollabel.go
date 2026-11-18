// Package toollabel generates a short label for a tool call, used when
// writing a pane's activity log line. Pure logic: no dependency on activity,
// tmux, or any other package in the project.
package toollabel

import (
	"path"
	"strings"
)

// Normalized tool names — all three adapters (Claude, Codex, OpenCode)
// normalize to exactly these names before reaching ActivityLog (OpenCode
// maps its lowercase ids to this table). Constants are used instead of
// string literals so a typo in the strategy table below becomes a compile
// error rather than a silent runtime bug.
const (
	Bash             = "Bash"
	Read             = "Read"
	Edit             = "Edit"
	Write            = "Write"
	NotebookEdit     = "NotebookEdit"
	PowerShell       = "PowerShell"
	Monitor          = "Monitor"
	PushNotification = "PushNotification"
	Glob             = "Glob"
	Grep             = "Grep"
	WebFetch         = "WebFetch"
	WebSearch        = "WebSearch"
	ToolSearch       = "ToolSearch"
	Skill            = "Skill"
	SendMessage      = "SendMessage"
	TeamCreate       = "TeamCreate"
	LSP              = "LSP"
	CronCreate       = "CronCreate"
	CronDelete       = "CronDelete"
	EnterWorktree    = "EnterWorktree"
	ExitWorktree     = "ExitWorktree"
	Agent            = "Agent"
	TaskCreate       = "TaskCreate"
	TaskUpdate       = "TaskUpdate"
	TaskGet          = "TaskGet"
	TaskStop         = "TaskStop"
	TaskOutput       = "TaskOutput"
	AskUserQuestion  = "AskUserQuestion"
	// TodoWrite DELIBERATELY has no strategy in strategyTable — see the comment there.
	TodoWrite = "TodoWrite"
)

type strategyKind int

const (
	stratField strategyKind = iota
	stratFilePath
	stratURLStrip
	stratCustom
)

type strategy struct {
	kind strategyKind
	key  string
	fn   func(input, response map[string]any) string
}

// strategyTable maps tool name → how to extract a label. 28 tools are
// present here.
//
// TodoWrite is NOT present — deliberately: TodoWrite writes the entire
// to-do list on every call (not a small change), so extracting a short label
// from it wouldn't carry more meaning than the bare tool name; the reference
// implementation (Rust) also excludes it from STRATEGY_TABLE for the exact
// same reason. Calling Label with TodoWrite returns "".
var strategyTable = map[string]strategy{
	Read:             {kind: stratFilePath, key: "file_path"},
	Edit:             {kind: stratFilePath, key: "file_path"},
	Write:            {kind: stratFilePath, key: "file_path"},
	NotebookEdit:     {kind: stratFilePath, key: "notebook_path"},
	Bash:             {kind: stratField, key: "command"},
	PowerShell:       {kind: stratField, key: "command"},
	Monitor:          {kind: stratField, key: "command"},
	PushNotification: {kind: stratField, key: "message"},
	Glob:             {kind: stratField, key: "pattern"},
	Grep:             {kind: stratField, key: "pattern"},
	WebFetch:         {kind: stratURLStrip, key: "url"},
	WebSearch:        {kind: stratField, key: "query"},
	ToolSearch:       {kind: stratField, key: "query"},
	Skill:            {kind: stratField, key: "skill"},
	SendMessage:      {kind: stratField, key: "to"},
	TeamCreate:       {kind: stratField, key: "team_name"},
	LSP:              {kind: stratField, key: "operation"},
	CronCreate:       {kind: stratField, key: "cron"},
	CronDelete:       {kind: stratField, key: "id"},
	EnterWorktree:    {kind: stratField, key: "name"},
	ExitWorktree:     {kind: stratField, key: "name"},
	Agent:            {kind: stratCustom, fn: labelAgent},
	TaskCreate:       {kind: stratCustom, fn: labelTaskCreate},
	TaskUpdate:       {kind: stratCustom, fn: labelTaskUpdate},
	TaskGet:          {kind: stratCustom, fn: labelTaskID},
	TaskStop:         {kind: stratCustom, fn: labelTaskID},
	TaskOutput:       {kind: stratCustom, fn: labelTaskID},
	AskUserQuestion:  {kind: stratCustom, fn: labelAskUserQuestion},
}

// Label extracts a short label from a tool call's input/response, using the
// strategy registered for toolName. An unknown tool (including TodoWrite)
// returns "".
func Label(toolName string, toolInput, toolResponse map[string]any) string {
	s, ok := strategyTable[toolName]
	if !ok {
		return ""
	}
	switch s.kind {
	case stratField:
		return fieldStr(toolInput, s.key)
	case stratFilePath:
		return basename(fieldStr(toolInput, s.key))
	case stratURLStrip:
		return stripURL(fieldStr(toolInput, s.key))
	case stratCustom:
		return s.fn(toolInput, toolResponse)
	default:
		return ""
	}
}

func fieldStr(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

func nestedFieldStr(m map[string]any, key, subkey string) string {
	if m == nil {
		return ""
	}
	sub, _ := m[key].(map[string]any)
	return fieldStr(sub, subkey)
}

// basename extracts the file name from a path. path.Base("") returns "." in
// stdlib — not what we want (a missing field_path should yield an empty
// label, not a dot) — so empty is guarded against up front.
func basename(p string) string {
	if p == "" {
		return ""
	}
	return path.Base(p)
}

func stripURL(u string) string {
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	return u
}

// labelAgent prefers the subagent's response text (the content[] block with
// type=="text") so the label shows the RESULT, not just the initial request.
// Falls back to input.description when the response is missing, has no text
// block, or the text block is all whitespace — so the label is never
// needlessly empty when a description is still available.
func labelAgent(input, response map[string]any) string {
	text := strings.TrimSpace(firstTextBlock(response))
	if text == "" {
		return fieldStr(input, "description")
	}
	return text
}

func firstTextBlock(response map[string]any) string {
	if response == nil {
		return ""
	}
	content, ok := response["content"].([]any)
	if !ok {
		return ""
	}
	for _, item := range content {
		block, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := block["type"].(string); t != "text" {
			continue
		}
		if text, ok := block["text"].(string); ok {
			return text
		}
	}
	return ""
}

func labelTaskCreate(input, response map[string]any) string {
	taskID := nestedFieldStr(response, "task", "id")
	subject := fieldStr(input, "subject")
	if taskID != "" {
		return "#" + taskID + " " + subject
	}
	return subject
}

func labelTaskUpdate(input, _ map[string]any) string {
	status := fieldStr(input, "status")
	taskID := fieldStr(input, "taskId")
	var parts []string
	if status != "" {
		parts = append(parts, status)
	}
	if taskID != "" {
		parts = append(parts, "#"+taskID)
	}
	return strings.Join(parts, " ")
}

// labelTaskID serves TaskGet/TaskStop/TaskOutput — all three may use taskId
// (camelCase) or task_id (snake_case) depending on the agent. camelCase wins
// when both are present, matching the reference implementation's behavior.
func labelTaskID(input, _ map[string]any) string {
	id := fieldStr(input, "taskId")
	if id == "" {
		id = fieldStr(input, "task_id")
	}
	if id == "" {
		return ""
	}
	return "#" + id
}

func labelAskUserQuestion(input, _ map[string]any) string {
	if input == nil {
		return ""
	}
	questions, ok := input["questions"].([]any)
	if !ok || len(questions) == 0 {
		return ""
	}
	q, ok := questions[0].(map[string]any)
	if !ok {
		return ""
	}
	return fieldStr(q, "question")
}
