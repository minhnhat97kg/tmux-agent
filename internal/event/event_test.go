package event

import "testing"

// Worktree is a pointer: nil means the agent is NOT running inside a
// worktree, which is different from a worktree with empty fields. A handler
// distinguishes between these two states.
func TestWorktreeNilDiffersFromEmpty(t *testing.T) {
	var e Event
	if e.Worktree != nil {
		t.Fatal("a zero-value Event must have Worktree = nil")
	}

	e.Worktree = &WorktreeInfo{}
	if e.Worktree == nil {
		t.Fatal("assigning an empty worktree must still be non-nil")
	}
}

// Response is a pointer because "the agent replied with an empty string" is
// different from "the agent didn't reply". Losing this distinction makes the
// Stop row display incorrectly.
func TestResponseNilDiffersFromEmptyString(t *testing.T) {
	var e Event
	if e.Response != nil {
		t.Fatal("a zero-value Event must have Response = nil")
	}

	empty := ""
	e.Response = &empty
	if e.Response == nil || *e.Response != "" {
		t.Fatal("Response pointing to an empty string must keep the distinction")
	}
}

func TestEventRetainsAllParsedFields(t *testing.T) {
	resp := "done"
	e := Event{
		Kind:           Stop,
		Agent:          "claude",
		Cwd:            "/home/u/proj",
		PermissionMode: "plan",
		Worktree: &WorktreeInfo{
			Name:            "feat-branch",
			Path:            "/tmp/wt",
			Branch:          "feat",
			OriginalRepoDir: "/home/u/repo",
		},
		AgentID:     "abc-123",
		SessionID:   "sess-1",
		LastMessage: "last message",
		Response:    &resp,
	}

	if e.Kind != Stop {
		t.Errorf("Kind = %q", e.Kind)
	}
	if e.Worktree.OriginalRepoDir != "/home/u/repo" {
		t.Errorf("OriginalRepoDir = %q", e.Worktree.OriginalRepoDir)
	}
	if *e.Response != "done" {
		t.Errorf("Response = %q", *e.Response)
	}
}
