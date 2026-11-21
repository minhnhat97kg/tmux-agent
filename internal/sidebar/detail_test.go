package sidebar

import (
	"strings"
	"testing"

	"tmux-agent/internal/git"
	"tmux-agent/internal/sharedctx"
	"tmux-agent/internal/tmux"
)

func TestBoardRefreshShowsGlobalNote(t *testing.T) {
	t.Setenv("TMUX_AGENT_CONTEXT_DIR", t.TempDir())
	path := t.TempDir()
	m := NewModel("%sidebar")
	m.viewport.Height = 10
	m.rows = []Row{{PaneID: "%1"}}
	if _, err := sharedctx.Add(sharedctx.Global, "worker", "%1", "new global note", ""); err != nil {
		t.Fatal(err)
	}
	msg := detailFetchCmd("%1", path)().(detailMsg)
	m.applyDetailMsg(msg)
	if got := strings.Join(m.contextLines(50), "\n"); !strings.Contains(got, "[global] new global note") {
		t.Fatalf("board does not show new global note: %s", got)
	}
}

// ─── dirtyByPane (badge "uncommitted changes" on the pane row, real user
// feedback: "see it right away without clicking to view the panel below") —
// ONLY updated for the pane currently under the cursor, see comment on
// Model.dirtyByPane. ──────────

func TestApplyDetailMsgChangedFilesMarkDirty(t *testing.T) {
	m := Model{rows: []Row{{PaneID: "%1"}}, cursor: 0}
	m.applyDetailMsg(detailMsg{PaneID: "%1", GitData: git.GitData{UnstagedFiles: []git.GitFileEntry{{Name: "a.go"}}}})
	if !m.dirtyByPane["%1"] {
		t.Fatal("has unstaged file but dirtyByPane[%1] = false")
	}
}

func TestApplyDetailMsgCleanMeansNotDirty(t *testing.T) {
	m := Model{rows: []Row{{PaneID: "%1"}}, cursor: 0, dirtyByPane: map[string]bool{"%1": true}}
	m.applyDetailMsg(detailMsg{PaneID: "%1", GitData: git.GitData{}})
	if m.dirtyByPane["%1"] {
		t.Fatal("GitData empty (no changed files) but dirtyByPane[%1] is still true")
	}
}

func TestApplyDetailMsgCursorMovedToOtherPaneDoesNotWriteDirty(t *testing.T) {
	// msg for pane %1 arrives LATE, cursor has already moved to %2 —
	// applyDetailMsg ignores this whole msg (including detailGitData, see
	// code), so dirtyByPane must not be written either, for the same reason.
	m := Model{rows: []Row{{PaneID: "%2"}}, cursor: 0}
	m.applyDetailMsg(detailMsg{PaneID: "%1", GitData: git.GitData{UnstagedFiles: []git.GitFileEntry{{Name: "a.go"}}}})
	if m.dirtyByPane["%1"] {
		t.Fatal("stale msg (pane %1, cursor now at %2) must not write dirtyByPane")
	}
}

func TestNeedsConfirm_OnlyWhenWaitingAndReasonInPermissionGroup(t *testing.T) {
	if !needsConfirm(tmux.StatusWaiting, "permission_denied") {
		t.Fatal("Waiting + permission_denied must need confirmation")
	}
	if needsConfirm(tmux.StatusWaiting, "user_input") {
		t.Fatal("Waiting + a soft wait reason outside the permission group must not need confirmation")
	}
	if needsConfirm(tmux.StatusBackground, "permission_denied") {
		t.Fatal("only Status == Waiting needs confirmation, Background does not")
	}
}

func TestTaskNameFromBranchStripsDefaultPrefix(t *testing.T) {
	if got := taskNameFromBranch("agent/do-task"); got != "do-task" {
		t.Fatalf("taskNameFromBranch(%q) = %q, want %q", "agent/do-task", got, "do-task")
	}
}

func TestTaskNameFromBranchNonDefaultPrefixUnchanged(t *testing.T) {
	// A branch with a non-default prefix (custom @agent_branch_prefix) is
	// returned unchanged — still respawns, just without a clean task name.
	if got := taskNameFromBranch("custom/do-task"); got != "custom/do-task" {
		t.Fatalf("taskNameFromBranch(%q) = %q, want unchanged", "custom/do-task", got)
	}
}
