package sidebar

import (
	"os"
	"path/filepath"
	"testing"

	"tmux-agent/internal/tmux"
)

// ─── GroupPanesByRepo ───────────────────────────────────────────────────────

func TestGroupPanesByRepoEmptyList(t *testing.T) {
	groups := GroupPanesByRepo(nil)
	if len(groups) != 0 {
		t.Fatalf("GroupPanesByRepo(nil) = %+v, want empty", groups)
	}
}

func TestGroupPanesByRepoTwoPanesSameRepoDifferentSession(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("create fake .git: %v", err)
	}

	sessions := []tmux.SessionInfo{
		{
			SessionName: "alpha",
			Windows: []tmux.WindowInfo{{
				WindowID: "@1",
				Panes:    []tmux.PaneInfo{{PaneID: "%1", Path: repoRoot}},
			}},
		},
		{
			SessionName: "beta",
			Windows: []tmux.WindowInfo{{
				WindowID: "@1",
				Panes:    []tmux.PaneInfo{{PaneID: "%2", Path: repoRoot}},
			}},
		},
	}

	groups := GroupPanesByRepo(sessions)
	if len(groups) != 1 {
		t.Fatalf("two panes same repo different session must group into 1, got %d groups: %+v", len(groups), groups)
	}
	if len(groups[0].Panes) != 2 {
		t.Fatalf("group must have 2 panes, got %d", len(groups[0].Panes))
	}
	if want := filepath.Base(repoRoot); groups[0].Name != want {
		t.Fatalf("group name = %q, want %q", groups[0].Name, want)
	}
}

func TestGroupPanesByRepoWorktreePaneGroupsWithMainRepo(t *testing.T) {
	mainRepo := t.TempDir()
	if err := os.Mkdir(filepath.Join(mainRepo, ".git"), 0o755); err != nil {
		t.Fatalf("create main repo .git: %v", err)
	}

	worktreeDir := t.TempDir()
	gitFileContent := "gitdir: " + filepath.Join(mainRepo, ".git", "worktrees", "feature-x") + "\n"
	if err := os.WriteFile(filepath.Join(worktreeDir, ".git"), []byte(gitFileContent), 0o644); err != nil {
		t.Fatalf("create worktree .git file: %v", err)
	}

	sessions := []tmux.SessionInfo{{
		SessionName: "s",
		Windows: []tmux.WindowInfo{{
			WindowID: "@1",
			Panes: []tmux.PaneInfo{
				{PaneID: "%1", Path: mainRepo},
				{PaneID: "%2", Path: worktreeDir},
			},
		}},
	}}

	groups := GroupPanesByRepo(sessions)
	if len(groups) != 1 {
		t.Fatalf("pane in a worktree must group with the main repo, got %d groups: %+v", len(groups), groups)
	}
	if len(groups[0].Panes) != 2 {
		t.Fatalf("merged group must have 2 panes, got %d", len(groups[0].Panes))
	}
	if want := filepath.Base(mainRepo); groups[0].Name != want {
		t.Fatalf("group name must be the main repo name %q, got %q", want, groups[0].Name)
	}
}

func TestGroupPanesByRepoPaneOutsideRepoUsesRawPath(t *testing.T) {
	sessions := []tmux.SessionInfo{{
		SessionName: "s",
		Windows: []tmux.WindowInfo{{
			Panes: []tmux.PaneInfo{{PaneID: "%1", Path: "/tmp/no-git-here-xyz"}},
		}},
	}}

	groups := GroupPanesByRepo(sessions)
	if len(groups) != 1 {
		t.Fatalf("pane outside a repo must still create 1 group, got %d", len(groups))
	}
	if groups[0].Name != "no-git-here-xyz" {
		t.Fatalf("group name = %q, want the raw path basename", groups[0].Name)
	}
}

func TestGroupPanesByRepoPaneWithEmptyPath(t *testing.T) {
	sessions := []tmux.SessionInfo{{
		SessionName: "s",
		Windows: []tmux.WindowInfo{{
			Panes: []tmux.PaneInfo{{PaneID: "%1", Path: ""}},
		}},
	}}

	groups := GroupPanesByRepo(sessions)
	if len(groups) != 1 {
		t.Fatalf("pane with empty path must still create 1 group, got %d", len(groups))
	}
}

func TestGroupPanesByRepoHasFocusWhenPaneAndWindowActive(t *testing.T) {
	sessions := []tmux.SessionInfo{{
		SessionName: "s",
		Attached:    true,
		Windows: []tmux.WindowInfo{{
			WindowActive: true,
			Panes:        []tmux.PaneInfo{{PaneID: "%1", Path: "/tmp/no-git-focus-1", PaneActive: true}},
		}},
	}}

	groups := GroupPanesByRepo(sessions)
	if !groups[0].HasFocus {
		t.Fatalf("an active pane in an active window of an attached session must make the group has_focus")
	}
}

func TestGroupPanesByRepoNoFocusWhenSessionNotAttached(t *testing.T) {
	// A background session, with no client attached, can still carry
	// window_active/pane_active true (tmux remembers this for every
	// session) — but that is not what the user is actually looking at.
	sessions := []tmux.SessionInfo{{
		SessionName: "s",
		Attached:    false,
		Windows: []tmux.WindowInfo{{
			WindowActive: true,
			Panes:        []tmux.PaneInfo{{PaneID: "%1", Path: "/tmp/no-git-focus-3", PaneActive: true}},
		}},
	}}

	groups := GroupPanesByRepo(sessions)
	if groups[0].HasFocus {
		t.Fatalf("a session with no client attached must not make the group has_focus, even if window/pane are active")
	}
}

func TestGroupPanesByRepoNoFocusWhenWindowInactive(t *testing.T) {
	sessions := []tmux.SessionInfo{{
		SessionName: "s",
		Attached:    true,
		Windows: []tmux.WindowInfo{{
			WindowActive: false,
			Panes:        []tmux.PaneInfo{{PaneID: "%1", Path: "/tmp/no-git-focus-2", PaneActive: true}},
		}},
	}}

	groups := GroupPanesByRepo(sessions)
	if groups[0].HasFocus {
		t.Fatalf("an active pane in a window that is NOT active must not make the group has_focus")
	}
}

func TestGroupPanesByRepoMultipleSessionsDifferentRepoCreateMultipleGroups(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("create fake .git: %v", err)
	}

	sessions := []tmux.SessionInfo{
		{
			SessionName: "alpha",
			Windows: []tmux.WindowInfo{{
				Panes: []tmux.PaneInfo{{PaneID: "%1", Path: repoRoot}},
			}},
		},
		{
			SessionName: "beta",
			Windows: []tmux.WindowInfo{{
				Panes: []tmux.PaneInfo{{PaneID: "%2", Path: "/tmp/no-git-other-repo"}},
			}},
		},
	}

	groups := GroupPanesByRepo(sessions)
	if len(groups) != 2 {
		t.Fatalf("two different repos in two sessions must create 2 separate groups, got %d", len(groups))
	}
}

func TestGroupPanesByRepoOrdersByFirstSeen(t *testing.T) {
	sessions := []tmux.SessionInfo{{
		SessionName: "s",
		Windows: []tmux.WindowInfo{{
			Panes: []tmux.PaneInfo{
				{PaneID: "%1", Path: "/tmp/no-git-zzz"},
				{PaneID: "%2", Path: "/tmp/no-git-Aaa"},
				{PaneID: "%3", Path: "/tmp/no-git-mmm"},
				{PaneID: "%4", Path: "/tmp/no-git-zzz"},
			},
		}},
	}}

	groups := GroupPanesByRepo(sessions)
	if len(groups) != 3 {
		t.Fatalf("want 3 groups, got %d: %+v", len(groups), groups)
	}
	if groups[0].Name != "no-git-zzz" || groups[1].Name != "no-git-Aaa" || groups[2].Name != "no-git-mmm" {
		t.Fatalf("group order must follow first-seen order (zzz, Aaa, mmm), not alphabetical, got %+v", groups)
	}
	if len(groups[0].Panes) != 2 {
		t.Fatalf("group zzz must have 2 panes (seen at that path twice), got %d", len(groups[0].Panes))
	}
	if groups[0].Panes[0].PaneID != "%1" || groups[0].Panes[1].PaneID != "%4" {
		t.Fatalf("pane order within a group must follow encounter order, got %+v", groups[0].Panes)
	}
}

func TestPaneBranchReadsHEAD(t *testing.T) {
	repo := t.TempDir()
	os.MkdirAll(filepath.Join(repo, ".git", "worktrees", "wt"), 0o755)
	os.WriteFile(filepath.Join(repo, ".git", "HEAD"), []byte("ref: refs/heads/main\n"), 0o644)
	sub := filepath.Join(repo, "a", "b")
	os.MkdirAll(sub, 0o755)
	if got := PaneBranch(tmux.PaneInfo{Path: sub}); got != "main" {
		t.Fatalf("subdir of repo: got %q, want main", got)
	}
	// linked worktree: .git is a file pointing at .git/worktrees/wt
	wt := t.TempDir()
	os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+filepath.Join(repo, ".git", "worktrees", "wt")+"\n"), 0o644)
	os.WriteFile(filepath.Join(repo, ".git", "worktrees", "wt", "HEAD"), []byte("ref: refs/heads/feat/x\n"), 0o644)
	if got := PaneBranch(tmux.PaneInfo{Path: wt}); got != "feat/x" {
		t.Fatalf("worktree: got %q, want feat/x", got)
	}
	os.WriteFile(filepath.Join(repo, ".git", "HEAD"), []byte("0123abcd\n"), 0o644)
	if got := PaneBranch(tmux.PaneInfo{Path: repo}); got != "" {
		t.Fatalf("detached: got %q, want empty", got)
	}
	if got := PaneBranch(tmux.PaneInfo{Path: t.TempDir()}); got != "" {
		t.Fatalf("no repo: got %q, want empty", got)
	}
}
