package main

import (
	"strings"
	"testing"
	"tmux-agent/internal/tmux"
)

func TestParsePlan(t *testing.T) {
	tasks, err := parsePlan(strings.NewReader(`
# demo
api: Implement calc.go per the contract
tester @tester after api: Write tests: table-driven, run go test
docs @docs: Write README.md
review @reviewer after api, tester: Review everything
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 4 {
		t.Fatalf("tasks = %+v", tasks)
	}
	if tasks[1].Role != "tester" || tasks[1].After[0] != "api" || tasks[1].Prompt != "Write tests: table-driven, run go test" {
		t.Errorf("tester = %+v", tasks[1])
	}
	if len(tasks[3].After) != 2 || tasks[3].After[1] != "tester" {
		t.Errorf("review = %+v", tasks[3])
	}
	for _, bad := range []string{"no colon here", "a: x\na: y", "b after ghost: x"} {
		if _, err := parsePlan(strings.NewReader(bad)); err == nil {
			t.Errorf("%q must be rejected", bad)
		}
	}
}

func TestPickIdle(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	sessions := []tmux.SessionInfo{{Windows: []tmux.WindowInfo{{Panes: []tmux.PaneInfo{
		{PaneID: "%1", Name: "busy", Status: tmux.StatusRunning, Path: "/r"},
		{PaneID: "%2", Name: "elsewhere", Status: tmux.StatusIdle, Path: "/other"},
		{PaneID: "%3", Name: "free", Status: tmux.StatusIdle, Path: "/r/sub"},
	}}}}}
	if p, n := pickIdle(sessions, "/r", ""); p != "%3" || n != "free" {
		t.Errorf("pickIdle = %s %s, want the idle agent in the repo", p, n)
	}
	if p, _ := pickIdle(sessions, "/rr", ""); p != "" {
		t.Errorf("a sibling dir /rr is not inside /r, got %s", p)
	}
}
