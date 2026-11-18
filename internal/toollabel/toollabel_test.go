package toollabel

import "testing"

// 28 tools have a simple label-extraction strategy (Field/FilePath/UrlStrip):
// each case only needs to read exactly one field of tool_input.
func TestLabelSimpleStrategies(t *testing.T) {
	cases := []struct {
		name     string
		toolName string
		input    map[string]any
		want     string
	}{
		{"Read takes the basename", Read, map[string]any{"file_path": "/home/user/project/src/main.rs"}, "main.rs"},
		{"Edit takes the basename", Edit, map[string]any{"file_path": "/tmp/foo.txt"}, "foo.txt"},
		{"Write takes the basename", Write, map[string]any{"file_path": "/a/b/c.json"}, "c.json"},
		{"Read missing file_path yields empty", Read, map[string]any{}, ""},
		{"Read a bare filename is kept as-is", Read, map[string]any{"file_path": "README.md"}, "README.md"},
		{"NotebookEdit takes the notebook_path basename", NotebookEdit, map[string]any{"notebook_path": "/home/user/analysis.ipynb"}, "analysis.ipynb"},
		{"Bash takes the command", Bash, map[string]any{"command": "cargo build"}, "cargo build"},
		{"Bash keeps a long command as-is", Bash, map[string]any{"command": "npm run test -- --watch --coverage --verbose --maxWorkers=4"}, "npm run test -- --watch --coverage --verbose --maxWorkers=4"},
		{"PowerShell takes the command", PowerShell, map[string]any{"command": "Get-Process"}, "Get-Process"},
		{"PowerShell missing command yields empty", PowerShell, map[string]any{}, ""},
		{"Monitor takes the command", Monitor, map[string]any{"command": "tail -f /var/log/server.log"}, "tail -f /var/log/server.log"},
		{"PushNotification takes the message", PushNotification, map[string]any{"message": "Deploy finished"}, "Deploy finished"},
		{"Glob takes the pattern", Glob, map[string]any{"pattern": "**/*.rs"}, "**/*.rs"},
		{"Grep takes the pattern", Grep, map[string]any{"pattern": "fn main"}, "fn main"},
		{"WebFetch strips the https prefix", WebFetch, map[string]any{"url": "https://example.com/docs"}, "example.com/docs"},
		{"WebFetch strips the http prefix", WebFetch, map[string]any{"url": "http://example.com"}, "example.com"},
		{"WebFetch keeps a URL with no prefix as-is", WebFetch, map[string]any{"url": "example.com/path"}, "example.com/path"},
		{"WebSearch takes the query", WebSearch, map[string]any{"query": "rust tutorial"}, "rust tutorial"},
		{"ToolSearch takes the query", ToolSearch, map[string]any{"query": "select:Read"}, "select:Read"},
		{"Skill takes the skill", Skill, map[string]any{"skill": "commit"}, "commit"},
		{"SendMessage takes the to field", SendMessage, map[string]any{"to": "agent-1"}, "agent-1"},
		{"TeamCreate takes the team_name", TeamCreate, map[string]any{"team_name": "reviewers"}, "reviewers"},
		{"LSP takes the operation", LSP, map[string]any{"operation": "hover"}, "hover"},
		{"CronCreate takes the cron", CronCreate, map[string]any{"cron": "*/5 * * * *"}, "*/5 * * * *"},
		{"CronDelete takes the id", CronDelete, map[string]any{"id": "abc123"}, "abc123"},
		{"EnterWorktree takes the name", EnterWorktree, map[string]any{"name": "feature-branch"}, "feature-branch"},
		{"EnterWorktree missing name yields empty", EnterWorktree, map[string]any{}, ""},
		{"ExitWorktree takes the name", ExitWorktree, map[string]any{"name": "feature-branch"}, "feature-branch"},
		{"ExitWorktree missing name yields empty", ExitWorktree, map[string]any{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Label(c.toolName, c.input, nil); got != c.want {
				t.Errorf("Label(%q, %v) = %q, want %q", c.toolName, c.input, got, c.want)
			}
		})
	}
}

func TestLabelAgentPrefersResponseText(t *testing.T) {
	input := map[string]any{"description": "Search codebase"}
	response := map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": "Found the bug at main.rs:42"},
		},
		"status": "completed",
	}
	if got := Label(Agent, input, response); got != "Found the bug at main.rs:42" {
		t.Errorf("Label(Agent, ...) = %q", got)
	}
}

func TestLabelAgentFallbackToDescriptionWhenNoResponse(t *testing.T) {
	input := map[string]any{"description": "Search codebase"}
	if got := Label(Agent, input, nil); got != "Search codebase" {
		t.Errorf("Label(Agent, ...) = %q", got)
	}
}

func TestLabelAgentFallbackWhenResponseHasNoTextBlock(t *testing.T) {
	input := map[string]any{"description": "Deploy to staging"}
	response := map[string]any{"status": "completed"}
	if got := Label(Agent, input, response); got != "Deploy to staging" {
		t.Errorf("Label(Agent, ...) = %q", got)
	}
}

func TestLabelAgentFallbackWhenTextIsAllWhitespace(t *testing.T) {
	input := map[string]any{"description": "Explore repo"}
	response := map[string]any{
		"content": []any{map[string]any{"type": "text", "text": "   "}},
	}
	if got := Label(Agent, input, response); got != "Explore repo" {
		t.Errorf("Label(Agent, ...) = %q", got)
	}
}

func TestLabelAgentPicksTextBlockAmongOtherBlockTypes(t *testing.T) {
	input := map[string]any{"description": "Audit config"}
	response := map[string]any{
		"content": []any{
			map[string]any{"type": "tool_use", "id": "x"},
			map[string]any{"type": "text", "text": "No drift detected"},
		},
	}
	if got := Label(Agent, input, response); got != "No drift detected" {
		t.Errorf("Label(Agent, ...) = %q", got)
	}
}

func TestLabelAgentKeepsMultilineResponseIntact(t *testing.T) {
	// A multiline label is kept as-is at this layer; sanitizing (turning \n
	// into spaces) is activity.WriteEntry's job when writing the log, not
	// toollabel's.
	input := map[string]any{"description": "List files"}
	response := map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": "main.rs\nlib.rs\ntmux.rs"},
		},
	}
	if got := Label(Agent, input, response); got != "main.rs\nlib.rs\ntmux.rs" {
		t.Errorf("Label(Agent, ...) = %q", got)
	}
}

func TestLabelTaskCreateWithId(t *testing.T) {
	input := map[string]any{"subject": "Add feature"}
	response := map[string]any{"task": map[string]any{"id": "1"}}
	if got := Label(TaskCreate, input, response); got != "#1 Add feature" {
		t.Errorf("Label(TaskCreate, ...) = %q", got)
	}
}

func TestLabelTaskCreateWithoutId(t *testing.T) {
	input := map[string]any{"subject": "Add feature"}
	if got := Label(TaskCreate, input, nil); got != "Add feature" {
		t.Errorf("Label(TaskCreate, ...) = %q", got)
	}
}

func TestLabelTaskCreateEmptySubjectWithId(t *testing.T) {
	response := map[string]any{"task": map[string]any{"id": "5"}}
	if got := Label(TaskCreate, map[string]any{}, response); got != "#5 " {
		t.Errorf("Label(TaskCreate, ...) = %q", got)
	}
}

func TestLabelTaskUpdateStatusAndId(t *testing.T) {
	input := map[string]any{"status": "completed", "taskId": "3"}
	if got := Label(TaskUpdate, input, nil); got != "completed #3" {
		t.Errorf("Label(TaskUpdate, ...) = %q", got)
	}
}

func TestLabelTaskUpdateStatusOnly(t *testing.T) {
	input := map[string]any{"status": "in_progress"}
	if got := Label(TaskUpdate, input, nil); got != "in_progress" {
		t.Errorf("Label(TaskUpdate, ...) = %q", got)
	}
}

func TestLabelTaskUpdateIdOnly(t *testing.T) {
	input := map[string]any{"taskId": "7"}
	if got := Label(TaskUpdate, input, nil); got != "#7" {
		t.Errorf("Label(TaskUpdate, ...) = %q", got)
	}
}

func TestLabelTaskUpdateEmpty(t *testing.T) {
	if got := Label(TaskUpdate, map[string]any{}, nil); got != "" {
		t.Errorf("Label(TaskUpdate, {}) = %q, want empty", got)
	}
}

func TestLabelTaskGetWithCamelCaseTaskId(t *testing.T) {
	input := map[string]any{"taskId": "5"}
	if got := Label(TaskGet, input, nil); got != "#5" {
		t.Errorf("Label(TaskGet, ...) = %q", got)
	}
}

func TestLabelTaskStopWithSnakeCaseTaskId(t *testing.T) {
	input := map[string]any{"task_id": "7"}
	if got := Label(TaskStop, input, nil); got != "#7" {
		t.Errorf("Label(TaskStop, ...) = %q", got)
	}
}

func TestLabelTaskGetPrefersCamelCaseWhenBothPresent(t *testing.T) {
	input := map[string]any{"taskId": "1", "task_id": "2"}
	if got := Label(TaskGet, input, nil); got != "#1" {
		t.Errorf("Label(TaskGet, ...) = %q, want #1 (camelCase wins)", got)
	}
}

func TestLabelTaskOutputEmpty(t *testing.T) {
	if got := Label(TaskOutput, map[string]any{}, nil); got != "" {
		t.Errorf("Label(TaskOutput, {}) = %q, want empty", got)
	}
}

func TestLabelAskUserQuestion(t *testing.T) {
	input := map[string]any{"questions": []any{map[string]any{"question": "Which option?"}}}
	if got := Label(AskUserQuestion, input, nil); got != "Which option?" {
		t.Errorf("Label(AskUserQuestion, ...) = %q", got)
	}
}

func TestLabelAskUserQuestionEmptyArray(t *testing.T) {
	input := map[string]any{"questions": []any{}}
	if got := Label(AskUserQuestion, input, nil); got != "" {
		t.Errorf("Label(AskUserQuestion, empty array) = %q, want empty", got)
	}
}

func TestLabelAskUserQuestionMissingQuestionsKey(t *testing.T) {
	if got := Label(AskUserQuestion, map[string]any{}, nil); got != "" {
		t.Errorf("Label(AskUserQuestion, {}) = %q, want empty", got)
	}
}

// A tool not present in strategyTable — including TodoWrite, deliberately
// excluded — must return an empty label, without panicking even when
// tool_input has data.
func TestLabelToolWithoutStrategyReturnsEmpty(t *testing.T) {
	cases := []string{"UnknownTool", "CronList", "ExitPlanMode", "TeamDelete", TodoWrite}
	for _, tool := range cases {
		if got := Label(tool, map[string]any{"anything": "value"}, nil); got != "" {
			t.Errorf("Label(%q, ...) = %q, want empty (no strategy)", tool, got)
		}
	}
}

// A nil tool_input/tool_response (payload missing a field, or JSON null)
// must not panic, and must fall back to an empty label just like when the
// field exists but is missing.
func TestLabelNilInputReturnsEmpty(t *testing.T) {
	if got := Label(Read, nil, nil); got != "" {
		t.Errorf("Label(Read, nil, nil) = %q", got)
	}
	if got := Label(TaskCreate, nil, nil); got != "" {
		t.Errorf("Label(TaskCreate, nil, nil) = %q", got)
	}
	if got := Label(Bash, nil, nil); got != "" {
		t.Errorf("Label(Bash, nil, nil) = %q", got)
	}
	if got := Label(WebFetch, nil, nil); got != "" {
		t.Errorf("Label(WebFetch, nil, nil) = %q", got)
	}
	if got := Label(AskUserQuestion, nil, nil); got != "" {
		t.Errorf("Label(AskUserQuestion, nil, nil) = %q", got)
	}
}
