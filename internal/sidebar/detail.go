// Fixed summary panel at the bottom of the pane (part 6 — replaces the old
// fixed bottom panel + tabs, AND replaces the "i" popup that was born partway
// through part 6 and then abandoned — real user feedback: "the detail panel
// (i) is probably unnecessary, show it directly at the root"). Git/Activity
// data for the pane the CURSOR IS ON is fetched every 1s tick AND right when
// the cursor moves to a different pane (a tea.Cmd fired from RefreshMsg/
// handleKey — see model.go), folded into the existing tick instead of a
// separate background worker: cheaper than the old approach (the 2s git
// worker in part 4a ran CONTINUOUSLY for EVERY pane regardless of who was
// looking at it).
package sidebar

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"tmux-agent/internal/git"
	"tmux-agent/internal/hook"
	"tmux-agent/internal/sharedctx"
	"tmux-agent/internal/tmux"
	"tmux-agent/internal/worktree"
)

// detailMsg is the result of detailFetchCmd for ONE pane.
type detailMsg struct {
	PaneID  string
	GitData git.GitData
	// Scope/Notes: the shared-context board of the pane's project.
	Scope string
	Notes []sharedctx.Entry
}

// detailFetchCmd fetches the git state of ONE pane (the one under the
// cursor) inside a tea.Cmd, so the child processes don't block Update. Only
// the dirty dot and ahead/behind badge on the pane row use it now — the
// activity/PR summary panel it used to feed is gone ("don't show history in
// the left pane").
func detailFetchCmd(paneID, path string) tea.Cmd {
	return func() tea.Msg {
		msg := detailMsg{PaneID: paneID}
		if path != "" {
			msg.GitData = git.FetchGitData(path)
			msg.Scope = sharedctx.Scope(path)
			msg.Notes = sharedctx.List(msg.Scope)
		}
		return msg
	}
}

// needsConfirm reports whether a pane is waiting for the USER TO HANDLE IT
// BY HAND — reuses hook.IsPermissionWaitReason (part 2, never displayed
// anywhere before part 6) instead of rewriting the list of permission
// reasons a second time.
func needsConfirm(status tmux.PaneStatus, waitReason string) bool {
	return status == tmux.StatusWaiting && hook.IsPermissionWaitReason(waitReason)
}

// paneInfoByID finds the full PaneInfo by id in the current sessions — used
// when the summary panel needs the Path to fetch git data for the pane
// under the cursor (Row does not keep Path, since the list row doesn't need
// it).
func paneInfoByID(sessions []tmux.SessionInfo, id string) (tmux.PaneInfo, bool) {
	for _, s := range sessions {
		for _, w := range s.Windows {
			for _, p := range w.Panes {
				if p.PaneID == id {
					return p, true
				}
			}
		}
	}
	return tmux.PaneInfo{}, false
}

// ─── Remove worktree (wires up worktree.Remove — part 5) ─────────────────

// removeResultMsg is the result of removeCmd.
type removeResultMsg struct {
	Success bool
	Err     string
	// Undo is set only when a worktree+branch (not window-only) was
	// actually removed — captured from the pane's markers BEFORE removal,
	// since worktree.Remove reads and then destroys them. nil means "this
	// removal cannot be undone" (window-only close, or markers unreadable).
	Undo *removedWorktree
}

// removedWorktree is the minimum needed to respawn an equivalent worktree —
// the "u" key (undo, model.go). Only the single MOST RECENT removal is
// remembered (Model.lastRemoved), process-memory only, lost on sidebar
// restart. ponytail: Agent/Mode aren't tracked (no marker carries them), so
// undo always respawns with worktree.DefaultAgent/DefaultMode — not
// necessarily what the original pane was actually running. Upgrade to
// storing the real agent/mode as extra markers if that turns out to matter.
type removedWorktree struct {
	RepoRoot string
	Branch   string
	Session  string
}

// removeCmd calls worktree.Remove inside a tea.Cmd (git + tmux I/O, does not
// block Update). Captures undo data BEFORE removing, since the markers
// worktree.Remove reads no longer exist afterward.
func removeCmd(paneID string, mode worktree.RemoveMode) tea.Cmd {
	return func() tea.Msg {
		var undo *removedWorktree
		if mode == worktree.RemoveWindowAndWorktree {
			markers := worktree.ParseSpawnMarkers(tmux.DisplayMessage(paneID, worktree.SpawnMarkersTemplate()))
			if session, ok := tmux.PaneSessionName(paneID); ok && markers.IsSpawned() {
				undo = &removedWorktree{RepoRoot: markers.FromRepo, Branch: markers.Branch, Session: session}
			}
		}
		if err := worktree.Remove(paneID, mode); err != nil {
			return removeResultMsg{Err: err.Error()}
		}
		return removeResultMsg{Success: true, Undo: undo}
	}
}

// undoRemoveCmd respawns an equivalent worktree from a captured
// removedWorktree — the "u" key. No flash-message mechanism exists yet in
// this codebase (see the "x" key comment in model.go), so a failed
// worktree.Spawn here is silently swallowed, same convention; the follow-up
// refreshCmd (model.go) just won't show anything new.
func undoRemoveCmd(w removedWorktree) tea.Cmd {
	return func() tea.Msg {
		worktree.Spawn(&worktree.SpawnRequest{
			RepoRoot: w.RepoRoot,
			TaskName: taskNameFromBranch(w.Branch),
			Session:  w.Session,
			Agent:    worktree.DefaultAgent,
			Mode:     worktree.DefaultMode,
		})
		return nil
	}
}

// taskNameFromBranch reverses the branch-prefix step of a spawn (Branch =
// worktree.DefaultBranchPrefix + Slugify(TaskName)) well enough to respawn
// an equivalent worktree — strips the default prefix if present, else
// returns the branch unchanged (a branch with a non-default prefix, e.g. a
// custom @agent_branch_prefix, still respawns, just without a clean name).
func taskNameFromBranch(branch string) string {
	return strings.TrimPrefix(branch, worktree.DefaultBranchPrefix)
}
