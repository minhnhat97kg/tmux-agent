package tmux

import (
	"strings"
	"testing"
)

// makeLine joins the 7 window-level fields (session_attached defaults to "1"
// — most tests in this file don't care whether the session has a client
// attached) with the pane-level tail.
func makeLine(sessionName, windowID, windowName, windowActive string, paneFields []string) string {
	head := []string{sessionName, windowID, "0", windowName, windowActive, "0", "1"}
	return strings.Join(append(head, paneFields...), "|")
}

func queryWith(t *testing.T, output string) []SessionInfo {
	t.Helper()
	fake := NewFake()
	fake.Set(output, "list-panes", "-a", "-F", paneFormat())
	restore := SetRunner(fake)
	t.Cleanup(restore)
	return QuerySessions()
}

func TestQuerySessionsBuildsHierarchy(t *testing.T) {
	p1 := fullPaneFields()
	p1[pfPaneID] = "%1"
	p1[pfPanePID] = "101"

	p2 := fullPaneFields()
	p2[pfPaneID] = "%2"
	p2[pfPanePID] = "102"
	p2[pfAgent] = "codex"
	p2[pfCurrentCommand] = "node"

	out := makeLine("work", "@1", "main", "1", p1) + "\n" +
		makeLine("work", "@2", "side", "0", p2) + "\n"

	sessions := queryWith(t, out)

	if len(sessions) != 1 {
		t.Fatalf("want 1 session, got %d", len(sessions))
	}
	if sessions[0].SessionName != "work" {
		t.Fatalf("SessionName = %q", sessions[0].SessionName)
	}
	if len(sessions[0].Windows) != 2 {
		t.Fatalf("want 2 windows, got %d", len(sessions[0].Windows))
	}
	w := sessions[0].Windows[0]
	if w.WindowID != "@1" || w.WindowName != "main" || !w.WindowActive {
		t.Fatalf("first window is wrong: %+v", w)
	}
	if len(w.Panes) != 1 || w.Panes[0].PaneID != "%1" {
		t.Fatalf("pane of first window is wrong: %+v", w.Panes)
	}
}

func TestQuerySessionsDropsWindowsWithoutAgentPanes(t *testing.T) {
	agentPane := fullPaneFields()
	agentPane[pfPanePID] = "201"

	plainPane := fullPaneFields()
	plainPane[pfPaneID] = "%9"
	plainPane[pfPanePID] = "202"
	plainPane[pfAgent] = "" // a plain pane, not an agent

	out := makeLine("work", "@1", "main", "1", agentPane) + "\n" +
		makeLine("work", "@2", "shell", "0", plainPane) + "\n"

	sessions := queryWith(t, out)

	if len(sessions) != 1 || len(sessions[0].Windows) != 1 {
		t.Fatalf("a window with no agent must be dropped, got %+v", sessions)
	}
}

func TestQuerySessionsDropsSessionsLeftEmpty(t *testing.T) {
	plainPane := fullPaneFields()
	plainPane[pfAgent] = ""
	plainPane[pfPanePID] = "301"

	sessions := queryWith(t, makeLine("work", "@1", "main", "1", plainPane)+"\n")

	if len(sessions) != 0 {
		t.Fatalf("want 0 sessions, got %+v", sessions)
	}
}

// A grouped session shares panes: the same pane_pid appears in multiple
// sessions. Keep only the first occurrence, otherwise the sidebar shows
// duplicate panes.
func TestQuerySessionsDeduplicatesSharedPanes(t *testing.T) {
	pane := fullPaneFields()
	pane[pfPanePID] = "400"

	out := makeLine("work", "@1", "main", "1", pane) + "\n" +
		makeLine("work-copy", "@1", "main", "1", pane) + "\n"

	sessions := queryWith(t, out)

	if len(sessions) != 1 {
		t.Fatalf("want 1 session after deduplication, got %d", len(sessions))
	}
}

// pane_pid = 0 means it couldn't be read, not "the same pane".
func TestQuerySessionsDoesNotDeduplicateZeroPID(t *testing.T) {
	a := fullPaneFields()
	a[pfPaneID] = "%1"
	a[pfPanePID] = "0"
	b := fullPaneFields()
	b[pfPaneID] = "%2"
	b[pfPanePID] = "0"

	out := makeLine("work", "@1", "main", "1", a) + "\n" +
		makeLine("work", "@1", "main", "1", b) + "\n"

	sessions := queryWith(t, out)

	if len(sessions[0].Windows[0].Panes) != 2 {
		t.Fatalf("pid 0 must not be treated as a duplicate: %+v", sessions[0].Windows[0].Panes)
	}
}

func TestQuerySessionsSkipsShortLines(t *testing.T) {
	good := makeLine("work", "@1", "main", "1", fullPaneFields())
	out := "junk|line|too|short\n" + good + "\n"

	sessions := queryWith(t, out)

	if len(sessions) != 1 {
		t.Fatalf("a junk line must be skipped without breaking the whole query: %+v", sessions)
	}
}

func TestQuerySessionsEmptyWhenTmuxFails(t *testing.T) {
	restore := SetRunner(NewFake())
	t.Cleanup(restore)

	if got := QuerySessions(); len(got) != 0 {
		t.Fatalf("want empty slice when tmux errors, got %+v", got)
	}
}

// A pipe inside the prompt must not shift the fields after it.
func TestQuerySessionsHandlesEscapedPipeInPrompt(t *testing.T) {
	pane := fullPaneFields()
	pane[pfPrompt] = `run a \| b`
	pane[pfPanePID] = "500"

	sessions := queryWith(t, makeLine("work", "@1", "main", "1", pane)+"\n")

	got := sessions[0].Windows[0].Panes[0]
	if got.Prompt != "run a | b" {
		t.Errorf("Prompt = %q, want %q", got.Prompt, "run a | b")
	}
	if got.SessionID != "sess-abc" {
		t.Errorf("SessionID = %q — the field after prompt is shifted", got.SessionID)
	}
}

func TestDetectCodexPermissionMode(t *testing.T) {
	cases := map[string]PermissionMode{
		"codex --full-auto": ModeAuto,
		"codex --yolo":      ModeBypassPermissions,
		"codex --dangerously-bypass-approvals-and-sandbox": ModeBypassPermissions,
		"codex": ModeDefault,
	}
	for args, want := range cases {
		if got := detectCodexPermissionMode(args); got != want {
			t.Errorf("detectCodexPermissionMode(%q) = %q, want %q", args, got, want)
		}
	}
}
