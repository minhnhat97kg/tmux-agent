// The sidebar's bubbletea loop: Model/Update, messages, and building the
// plain row list from tmux.QuerySessions(). View() lives in view.go, and
// goroutine/signal startup lives in run.go — split by responsibility, same
// package.
package sidebar

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tmux-agent/internal/clock"
	"tmux-agent/internal/git"
	"tmux-agent/internal/hook"
	"tmux-agent/internal/scrape"
	"tmux-agent/internal/sharedctx"
	"tmux-agent/internal/tmux"
	"tmux-agent/internal/worktree"
)

const (
	// refreshInterval is the cycle for rebuilding the list from tmux.QuerySessions().
	refreshInterval = time.Second
	// animInterval is the cycle for advancing the spinner frame, and also the
	// debounce cadence for writing global options (cursor/repo filter) that
	// changed locally.
	animInterval = 200 * time.Millisecond
	// footerHeight is the number of key-hint lines at the bottom of the pane
	// (renderKeyHints, view.go) — also drawn outside the viewport, for the same
	// reason as filterBarHeight.
	footerHeight = 1
)

// Row is one line in the list: either a repo group header or an agent pane.
// Icon/color are NOT precomputed here — renderRows looks them up in
// theme.go/Icons at draw time (Status/Agent is raw data, sufficient for the
// lookup).
type Row struct {
	IsHeader bool
	// Header is the displayed repo group name, meaningful only when
	// IsHeader. This IS the group by git repo root (group.go/GroupPanesByRepo),
	// no longer the tmux session name as in version 3a.
	Header string
	// HeaderFocus is true when this group has a pane that is both active and
	// in the active window (RepoGroup.HasFocus) — used to color the header
	// with Theme.Accent.
	HeaderFocus bool

	// Status/Attention/Agent/Base/PaneID are meaningful only when !IsHeader.
	Status    tmux.PaneStatus
	Attention bool
	Agent     tmux.AgentType
	Base      string
	PaneID    string
	// Ports are the TCP ports currently listening in this pane's process tree
	// (scanned every 10 seconds by a background worker — workers.go), sorted
	// ascending. Empty means no ports (or the first scan hasn't finished yet).
	Ports []int
	// Branch is the pane's git worktree branch name (tmux.PaneInfo.Worktree.Branch,
	// i.e. @pane_worktree_branch — written by the AGENT's own hook, NOT the
	// spawn marker @agent-sidebar-spawned-branch), used for display in the
	// delete-confirmation popup — NOT shown on the row itself (part 3 does
	// not draw a branch label on the pane row, out of scope for task 7).
	//
	// KNOWN LIMITATION (found during manual verification, see task 7 report):
	// this field is empty until the REAL agent has actually run in that pane
	// and its SessionStart hook has reported the "worktree" field — during
	// the first few seconds after spawn (before the agent manages to start
	// up), the delete-confirmation popup shows the paneID instead of the
	// branch name (the fallback right below). This does NOT affect the
	// correctness of the delete itself: worktree.Remove re-reads the real
	// spawn marker from tmux at execution time, independent of this field —
	// this is purely display text. Could be fixed by reading the spawn
	// marker (worktree.ParseSpawnMarkers) via a tea.Cmd right when the popup
	// opens instead of reading the already-available PaneInfo, in exchange
	// for one asynchronous round trip before the popup appears — left for
	// later if needed.
	Branch string
	// SidebarSpawned is true when the window containing this pane was
	// created by the sidebar's spawn flow (tmux.PaneInfo.SidebarSpawned) —
	// the ONLY condition under which the "x" key (delete worktree) has any
	// effect on this row.
	SidebarSpawned bool

	// NeedsConfirm/JustDone: two view-only overlays from part 6 (see the
	// StatusConfirm/StatusDone comment in theme.go) — they do NOT change the
	// real Status, only the displayed icon/color (iconForRow/colorForRow,
	// view.go).
	NeedsConfirm bool
	JustDone     bool
	// Dirty is true when this pane's most recent git fetch (bottom summary
	// panel, detail.go — runs ONLY for the pane under the cursor, not a
	// background worker for every pane; see the comment in workers.go about
	// dropping the continuous git worker entirely) found uncommitted changed
	// files. "Last known": a pane the cursor has never visited in this
	// session always has Dirty == false, not because it's actually clean —
	// a deliberate trade-off to avoid spawning an extra git subprocess for
	// EVERY pane on every tick, per the decision already recorded in
	// workers.go.
	Dirty bool
	// AheadBehind is a short "↑N ↓M" badge from the pane's last-known git
	// fetch (same last-known/cursor-only caching trade-off as Dirty above —
	// see rowExtras.AheadBehind). Empty when never fetched or no upstream.
	AheadBehind string
	// Elapsed is the elapsed time of the current run (elapsedLabel), empty
	// when the pane has no run currently being timed (StartedAt == 0).
	Elapsed string
	// Pinned is true when the user pinned this pane to the top of the list
	// (the "p" key) — process-memory only, lost on sidebar restart (see
	// Model.pinnedPanes).
	Pinned bool
	// Selected is true when this pane is part of the current multi-select
	// (the "v" key), driving bulk delete on "x" — see Model.selectedPanes.
	Selected bool

	// IsSubagentChild is true when this row is a subagent branch drawn
	// nested right under its parent pane row, directly in the root list —
	// NO LONGER inside the detail popup (real user feedback: "show the tree
	// at the root", "shouldn't need to press i to see it"). SubagentLabel is
	// the ALREADY-BUILT branch text (including the ├─/└─ connectors — see
	// treeBranch, view.go), meaningful only when IsSubagentChild. Rows of
	// this kind are NOT counted/landed on by the cursor (see paneRowCount/
	// paneRowAt/rowIndexOfCursor) — exactly like how IsHeader is skipped.
	IsSubagentChild bool
	SubagentLabel   string

	// IsInfoChild is true when this row is the "what it's doing" line drawn
	// nested RIGHT under the agent's name (real user feedback: "the current
	// thinking/process should be shown right under the agent's name").
	// InfoLabel is the user's prompt that has NOT yet become a response
	// (tmux.PaneInfo.Prompt when !PromptIsResponse — written by the hook from
	// OnUserPromptSubmit, part 2), meaningful only when Status==Running: a
	// pane that isn't running has no "current" thing to report — see
	// processLine.
	IsInfoChild bool
	InfoLabel   string
	// InfoAction marks the info line as "the user must act" (ActionLine):
	// drawn in the confirm color instead of muted.
	InfoAction bool
	// InfoKey is set on an answer line of a waiting agent's prompt: the key
	// that picks it.
	InfoKey string
	// GitBranch is the branch shown after the agent's name, on its row.
	GitBranch string
}

// rowExtras bundles data that does NOT come from tmux.QuerySessions() but
// needs to be mixed in when building rows: listening ports (portscan, 10s
// worker) and the session name assigned by the agent (session, 10s worker —
// tmux.PaneInfo.SessionName is left empty at the query layer, see that
// field's comment). Bundled instead of adding two separate parameters so
// buildRows's signature doesn't bloat every time another secondary data
// source is added.
type rowExtras struct {
	Ports        map[string][]int
	SessionNames map[string]string
	// Dirty: cache of "most recent git fetch" keyed by pane id — see the
	// Row.Dirty and Model.dirtyByPane comments.
	Dirty map[string]bool
	// AheadBehind: cache of "most recent git fetch" ahead/behind badge keyed
	// by pane id — same last-known/cursor-only trade-off as Dirty, see
	// Model.aheadBehindByPane.
	AheadBehind map[string]string
	// Pinned: the set of pane ids the user pinned to the top of the list —
	// see Row.Pinned and Model.pinnedPanes.
	Pinned map[string]bool
	// Selected: the multi-select set for bulk delete — see Row.Selected and
	// Model.selectedPanes.
	Selected map[string]bool
	// FilterQuery: a case-insensitive substring filter over agent/name/status
	// (the "/" key) — empty means no filter. Applied in buildRows BEFORE
	// grouping, so a repo group with no matching pane doesn't show up at all.
	FilterQuery string
	// CurrentWindow is the window the sidebar sits in — its header is
	// highlighted.
	CurrentWindow string
	// Dialogs are waiting agents' prompts, shown with their answers.
	Dialogs map[string]scrape.Dialog
}

// buildRows builds the flat row list: group by git repo (group.go) → filter
// by repo (popup.go) → rows. ALWAYS shows every pane status (real user
// feedback: "remove the agent filter tab, always show the list without
// filtering" — the earlier version had an additional Running/Waiting/...
// status filter that has been fully removed).
// processLine returns a pane's currently running prompt — only when the
// pane is Running AND the prompt is still the user's REQUEST, not yet
// overwritten by OnStop into a response (tmux.PaneInfo.Prompt/PromptIsResponse
// — written by the hook from OnUserPromptSubmit/OnStop, part 2). This is the
// most recent "what it's doing" signal available without adding any new
// option/hook: OnUserPromptSubmit writes Prompt at the same time it sets
// Status=Running, and it only changes back (into a response, Status other
// than Running) at OnStop — for the entire run, Prompt IS what the agent is
// doing. Empty when the pane isn't running — there's no "current" thing to
// report.
// ActionLine says what the user has to do for a pane that's blocked on them,
// shown inline under its row. Empty when the agent doesn't need anyone.
func ActionLine(p tmux.PaneInfo) string {
	switch p.Status {
	case tmux.StatusError:
		if p.WaitReason != "" {
			return "failed: " + p.WaitReason
		}
		return "failed — check the pane"
	case tmux.StatusWaiting:
	default:
		// Watchdog flags (internal/scrape/watchdog.go) on a working agent.
		switch {
		case p.Conflict != "":
			return "editing " + p.Conflict
		case p.Stuck != "" && p.Status == tmux.StatusRunning:
			return "stuck? no activity for " + p.Stuck + "m"
		}
		return ""
	}
	switch {
	case hook.IsPermissionWaitReason(p.WaitReason) && p.WaitReason != "elicitation_dialog":
		return "needs approval"
	case p.WaitReason == "elicitation_dialog":
		return "has a question for you"
	case p.WaitReason == "idle_prompt":
		return "waiting for your input"
	}
	return "needs your input"
}

// elapsedLabel builds the elapsed time of the current run, kept short so it
// fits the last column of the row (herdr lesson 9.1: a secondary token, tight
// budget) — seconds under 1 minute, minutes+seconds under 1 hour, "1h+" from
// 1 hour onward (no need for finer precision here — an agent running past 1
// hour is already unusual enough to be worth flagging, no need to know
// exactly how long). Empty when StartedAt isn't set yet (pane not running —
// ClearRunState already unset @pane_started_at on Stop, see handlers.go).
func elapsedLabel(p tmux.PaneInfo) string {
	return elapsedLabelFromStartedAt(p.StartedAt)
}

// elapsedLabelFromStartedAt is the shared logic used by elapsedLabel (single
// pane) and groupElapsedLabel (whole repo group) — factored out so the
// seconds/minutes/hours formatting rule isn't duplicated.
func elapsedLabelFromStartedAt(startedAt int64) string {
	if startedAt <= 0 {
		return ""
	}
	secs := clock.NowUnix() - startedAt
	switch {
	case secs < 0:
		return ""
	case secs < 60:
		return fmt.Sprintf("%ds", secs)
	case secs < 3600:
		return fmt.Sprintf("%dm%ds", secs/60, secs%60)
	default:
		return "1h+"
	}
}

// aheadBehindLabel builds the short "↑N ↓M" badge shown on a pane row from
// its git.AheadBehind (nil when the branch has no upstream, or the fetch
// hasn't run yet — both render as empty, no badge). Ahead/behind are shown
// only when non-zero, so a branch that's merely behind doesn't show a
// pointless "↑0".
func aheadBehindLabel(ab *git.AheadBehind) string {
	if ab == nil || (ab.Ahead == 0 && ab.Behind == 0) {
		return ""
	}
	var parts []string
	if ab.Ahead > 0 {
		parts = append(parts, fmt.Sprintf("↑%d", ab.Ahead))
	}
	if ab.Behind > 0 {
		parts = append(parts, fmt.Sprintf("↓%d", ab.Behind))
	}
	return strings.Join(parts, " ")
}

// paneUnitRows builds exactly ONE block of rows for a single pane: the pane
// row, then (if present) the "what it's doing" line and the subagent tree
// nested right below it — SEPARATED from buildRows so it can be called from
// both the priority branch and the group-by-repo branch without duplicating
// code.
func paneUnitRows(p tmux.PaneInfo, extras rowExtras) []Row {
	rows := []Row{{
		Status:         p.Status,
		Attention:      p.Attention,
		Agent:          p.Agent,
		Base:           computeBaseName(p, extras),
		PaneID:         p.PaneID,
		Ports:          extras.Ports[p.PaneID],
		Branch:         p.Worktree.Branch,
		SidebarSpawned: p.SidebarSpawned,
		NeedsConfirm:   needsConfirm(p.Status, p.WaitReason),
		JustDone:       p.Unseen,
		Dirty:          extras.Dirty[p.PaneID],
		AheadBehind:    extras.AheadBehind[p.PaneID],
		Elapsed:        elapsedLabel(p),
		Pinned:         extras.Pinned[p.PaneID],
		Selected:       extras.Selected[p.PaneID],
		GitBranch:      PaneBranch(p),
	}}
	// The line nested under the agent's name: what the user must do
	// (ActionLine) when the agent is blocked on them — nothing otherwise,
	// matching the expanded view (user feedback: "keep the list compact").
	if d, ok := extras.Dialogs[p.PaneID]; ok {
		// Approval inbox: the question and its answers right in the list;
		// with the cursor on this agent, the option's key answers it.
		rows = append(rows, Row{IsInfoChild: true, InfoLabel: stripEmoji(d.Question), InfoAction: true})
		for _, o := range d.Options {
			rows = append(rows, Row{IsInfoChild: true, InfoLabel: stripEmoji(o.Label), InfoKey: o.Key})
		}
	} else if action := ActionLine(p); action != "" {
		rows = append(rows, Row{IsInfoChild: true, InfoLabel: action, InfoAction: true})
	}
	// The subagent branch nested RIGHT after that — "show the tree at the
	// root", not inside the detail popup (real user feedback, part 6). Empty
	// when the pane has no subagents (subagentRows, view.go).
	rows = append(rows, subagentRows(p.Subagents)...)
	return rows
}

// computeBaseName is the display name shown on a pane row: the agent-given
// session name (tmux.PaneInfo.SessionID → extras.SessionNames) if known,
// else the basename of the pane's path. Factored out of paneUnitRows so
// matchesFilterQuery can match against the SAME name the user sees, not the
// raw path.
func computeBaseName(p tmux.PaneInfo, extras rowExtras) string {
	if p.Name != "" {
		return p.Name
	}
	base := filepath.Base(p.Path)
	if p.SessionID != "" {
		if name, ok := extras.SessionNames[p.SessionID]; ok && name != "" {
			base = name
		}
	}
	return base
}

// matchesFilterQuery reports whether a pane matches the "/" text filter — a
// case-insensitive substring match against the displayed name, the agent
// name, or the status string. query is assumed already lowercased/trimmed
// by the caller (buildRows).
func matchesFilterQuery(p tmux.PaneInfo, extras rowExtras, query string) bool {
	if strings.Contains(strings.ToLower(computeBaseName(p, extras)), query) {
		return true
	}
	if strings.Contains(strings.ToLower(string(p.Agent)), query) {
		return true
	}
	return strings.Contains(strings.ToLower(string(p.Status)), query)
}

// buildRows builds the agent list in tmux order — session, window, pane,
// exactly like the status bar (user feedback: "sort by window position") —
// under one small header per window that has agents ("3 window"; prefixed
// with the session name when there are several sessions), so it's clear
// which window each pane is in. The sidebar's own window is highlighted. A
// pane that needs the user stays in place and carries an inline action line
// (ActionLine). The repo filter (r) and text filter (/) still apply.
func buildRows(sessions []tmux.SessionInfo, repoFilter string, extras rowExtras) []Row {
	// Repo of each pane, keyed by path (a pane's repo depends only on it).
	repoOf := map[string]string{}
	if repoFilter != RepoFilterAll {
		for _, g := range GroupPanesByRepo(sessions) {
			for _, p := range g.Panes {
				repoOf[p.Path] = g.Name
			}
		}
	}
	query := strings.ToLower(strings.TrimSpace(extras.FilterQuery))
	var rows []Row
	for _, s := range sessions {
		for _, w := range s.Windows {
			var unit []Row
			for _, p := range w.Panes {
				if repoFilter != RepoFilterAll && repoOf[p.Path] != repoFilter {
					continue
				}
				if query != "" && !matchesFilterQuery(p, extras, query) {
					continue
				}
				unit = append(unit, paneUnitRows(p, extras)...)
			}
			if len(unit) == 0 {
				continue
			}
			title := w.WindowIndex + " " + w.WindowName
			if len(sessions) > 1 {
				title = s.SessionName + ":" + title
			}
			current := extras.CurrentWindow != "" && w.WindowID == extras.CurrentWindow
			rows = append(rows, Row{IsHeader: true, Header: title, HeaderFocus: current})
			rows = append(rows, unit...)
		}
	}
	return rows
}

// focusedPane is the agent pane the user is looking at in window (its active
// pane), "" when that's not an agent pane (e.g. the sidebar itself).
func focusedPane(sessions []tmux.SessionInfo, window string) string {
	for _, s := range sessions {
		for _, w := range s.Windows {
			if w.WindowID != window {
				continue
			}
			for _, p := range w.Panes {
				if p.PaneActive {
					return p.PaneID
				}
			}
		}
	}
	return ""
}

// paneIndexOf is the cursor index of paneID among selectable rows.
func paneIndexOf(rows []Row, paneID string) (int, bool) {
	i := 0
	for _, r := range rows {
		if !isSelectablePaneRow(r) {
			continue
		}
		if r.PaneID == paneID {
			return i, true
		}
		i++
	}
	return 0, false
}

// ─── Cursor ─────────────────────────────────────────────────────────────────
//
// The cursor is an INDEX in the list of pane rows (headers excluded), like
// the reference implementation (state/global.rs `selected_pane_row`), but
// applyRows re-resolves it by pane id on every rebuild: a pane added,
// removed or regrouped above it no longer moves the highlight to another
// pane (the reference only clamps `>= len`).

// isSelectablePaneRow reports whether row r is an actual pane row that the
// cursor can select — headers, subagent branches (IsSubagentChild), and the
// "what it's doing" line (IsInfoChild) are all excluded, for the same reason
// and in the same place, so the three kinds of "not a pane" row never drift
// out of sync across the three functions below.
func isSelectablePaneRow(r Row) bool {
	return !r.IsHeader && !r.IsSubagentChild && !r.IsInfoChild
}

// paneRowCount counts the selectable pane rows — this is the "length" the
// cursor moves/clamps against, corresponding to `pane_row_targets` in the
// reference implementation.
func paneRowCount(rows []Row) int {
	n := 0
	for _, r := range rows {
		if isSelectablePaneRow(r) {
			n++
		}
	}
	return n
}

// paneRowAt returns the FULL Row of the `cursor`-th pane row (counting skips
// headers and subagent branches). ok = false when cursor is out of range or
// the list is empty.
func paneRowAt(rows []Row, cursor int) (Row, bool) {
	i := 0
	for _, r := range rows {
		if !isSelectablePaneRow(r) {
			continue
		}
		if i == cursor {
			return r, true
		}
		i++
	}
	return Row{}, false
}

// paneIDAt returns the pane id of the `cursor`-th pane row — see paneRowAt.
func paneIDAt(rows []Row, cursor int) (string, bool) {
	row, ok := paneRowAt(rows, cursor)
	return row.PaneID, ok
}

// rowIndexOfCursor returns the INDEX WITHIN rows (headers included) of the
// `cursor`-th pane row — this is the "line" that scroll.go needs to keep the
// cursor within the viewport. ok = false when no pane row matches (empty
// list or cursor out of range); in that case there's nothing to scroll to.
func rowIndexOfCursor(rows []Row, cursor int) (int, bool) {
	i := 0
	for idx, r := range rows {
		if !isSelectablePaneRow(r) {
			continue
		}
		if i == cursor {
			return idx, true
		}
		i++
	}
	return 0, false
}

// clampCursor clamps the cursor into a valid range after the list changes.
// Like the reference implementation's `rebuild_row_targets`: it ONLY pulls
// back to `count-1` when the cursor has gone past the end of the list; an
// empty list leaves the old value unchanged (nothing to clamp into, and the
// old value is still useful if the list grows back). There's an added
// lower-bound clamp (`cursor < 0`) that the reference implementation doesn't
// need because Rust's `usize` can't go negative — Go uses int, so it's
// clamped here just to be safe.
func clampCursor(cursor, count int) int {
	if count == 0 {
		return cursor
	}
	if cursor < 0 {
		return 0
	}
	if cursor >= count {
		return count - 1
	}
	return cursor
}

// moveCursor adds delta to cursor, clamping at the edges — no wraparound.
// Returns the new cursor and whether it actually moved (like the reference
// implementation's `move_pane_selection`: false when the list is empty or
// already at the edge, never snapping the cursor back to 0).
func moveCursor(cursor, delta, count int) (int, bool) {
	if count == 0 {
		return cursor, false
	}
	next := cursor + delta
	if next < 0 || next >= count {
		return cursor, false
	}
	return next, true
}

// RefreshMsg carries the session list that just finished being queried from
// tmux — the result of a tmux.QuerySessions() call completed inside a
// tea.Cmd, whether triggered by the 1s tick or by SIGUSR1.
type RefreshMsg struct {
	Sessions []tmux.SessionInfo
	Theme    Theme
	Icons    Icons
	Avatar   AvatarConfig
	// Dialogs: the prompt on screen of each waiting agent (approval inbox).
	Dialogs map[string]scrape.Dialog
	// Image avatar (avatar_image.go): wanted on this terminal, your image
	// options, and the attached-clients signature.
	Images     bool
	ImageOpts  map[string]string
	ClientsSig string
}

// AnimTickMsg advances the animation frame, used for the spinner/colors in
// part 3b, and is also the debounce cadence for any locally changed global
// option write (cursor/filter/repo filter).
type AnimTickMsg struct{}

// refreshTickMsg is the 1-second tick that triggers one rebuild of the list,
// and schedules itself again for the next tick.
type refreshTickMsg struct{}

// refreshRequestMsg is a request to rebuild the list IMMEDIATELY, without
// waiting for the 1s tick — SIGUSR1 sends this kind via p.Send(). Unlike
// refreshTickMsg: after handling it, it does NOT schedule a new tick, so it
// doesn't stack on top of the regular tick cycle already running.
type refreshRequestMsg struct{}

// cursorLoadedMsg carries the @sidebar_cursor value read from tmux at
// startup — so the cursor position survives reopening the sidebar. This
// value COMES FROM tmux (possibly written by another sidebar in a different
// window), so once applied it does NOT mark cursorDirty — otherwise it would
// write the just-read value right back.
type cursorLoadedMsg struct {
	Value int
}

// cursorSavedMsg is the result of one @sidebar_cursor write. Success = false
// (tmux.SetOptionResult failed) means cursorDirty MUST be left set so the
// next AnimTick retries the write — pays off debt 3a Minor 2
// (global.rs:86-101: flush_pending_cursor_save only clears the flag when
// save_cursor() succeeds).
type cursorSavedMsg struct {
	Success bool
}

// repoFilterLoadedMsg carries the @sidebar_repo_filter value read at
// startup — same reasoning as cursorLoadedMsg: once applied it does not mark
// dirty.
type repoFilterLoadedMsg struct{ Value string }

// repoFilterSavedMsg is the corresponding write result — same
// retry-on-failure mechanism as cursorSavedMsg.
type repoFilterSavedMsg struct{ Success bool }

// Model is the sidebar's TUI state.
type Model struct {
	// dialogs: prompts of waiting agents by pane id (approval inbox).
	dialogs map[string]scrape.Dialog

	// Image avatar state (avatar_image.go): on/off, the images for the
	// current settings, which ids the terminal has, upload in flight.
	images       bool
	imgSet       imageSet
	imgHave      map[uint32]bool
	imgClients   string
	imgUploading bool
	imgSetKey    string

	// avatar is the avatar config (avatar.go), refreshed with the theme.
	avatar AvatarConfig

	// windowID is the window this sidebar sits in (current-window header,
	// focus following); lastFocus is the agent pane last seen focused there.
	windowID  string
	lastFocus string
	// digestKicked is the agents' state this sidebar last asked a line for
	// (digestKick, digest.go).
	digestKicked string

	// notes/notesScope: the shared-context board of the project of the pane
	// under the cursor, shown below the list when there's room (view.go).
	notes      []sharedctx.Entry
	notesScope string

	// tmuxPane is the sidebar pane's own $TMUX_PANE — for future operations
	// that need to distinguish "my own pane" (task 3, selecting a pane other
	// than itself).
	tmuxPane string

	// sessions is the most recent raw result of tmux.QuerySessions() — kept
	// around so changing the repo filter can rebuild rows IMMEDIATELY,
	// without waiting for the next 1s refresh tick (buildRows needs the
	// original Path, which is lost once flattened into rows).
	sessions []tmux.SessionInfo
	// repoNames: cache of allRepoNames, refreshed in applyRows.
	repoNames []string

	rows      []Row
	viewport  viewport.Model
	scroll    ScrollState
	width     int
	height    int
	animFrame int

	// cursor is the index of the currently selected pane row (headers
	// excluded) — see the "Cursor" comment above buildRows.
	cursor int
	// cursorDirty marks that the cursor has changed and has NOT YET been
	// SUCCESSFULLY written to @sidebar_cursor — a debounce mechanism: holding
	// down j/k continuously produces only ONE tmux write at the next
	// AnimTick. Only cleared when cursorSavedMsg reports success (see
	// cursorSavedMsg) — a failed write stays dirty and is retried next tick.
	// Ports the debounce+retry idea from state/global.rs.
	cursorDirty bool
	// cursorSaveInFlight is true while a saveCursorCmd is waiting for tmux to
	// respond — blocks the next AnimTick from firing another write on top of
	// it before knowing whether the previous one succeeded or failed.
	cursorSaveInFlight bool

	// repoFilter is the name of the repo currently being filtered, or
	// RepoFilterAll. Changed via the popup (Enter confirms the selection) —
	// debounced write to @sidebar_repo_filter exactly like cursor/filter.
	repoFilter             string
	repoFilterDirty        bool
	repoFilterSaveInFlight bool

	// popup is the "at most one popup open" state machine (popup.go).
	popup Popup

	theme Theme
	icons Icons

	// dirtyByPane is the cache of "most recent git fetch" keyed by pane id —
	// ONLY updated for the pane currently under the cursor (applyDetailMsg);
	// keyed by id so other panes' values stay at their "last known" state
	// when the cursor moves away, and are NOT cleared. Deliberately not a
	// background worker scanning every pane (see the comment on Row.Dirty
	// and workers.go) — this is why the Dirty badge can be stale/missing for
	// a pane the cursor has never visited in this session.
	dirtyByPane map[string]bool

	// portsByPane is the most recent result of the background port-scan
	// worker (workers.go, every 10 seconds), keyed by pane id — recomputed
	// every time a portScanMsg arrives (computePortsByPane, workers.go) from
	// the process snapshot + the current m.sessions.
	portsByPane map[string][]int
	// sessionNames is the most recent result of the background session-name
	// scan worker (workers.go, every 10 seconds), keyed by session id.
	sessionNames map[string]string

	// ─── Worktree-delete confirmation popup (drives worktree.Remove) ────
	removeConfirmOpen   bool
	removeConfirmPaneID string
	removeConfirmBranch string
	removeConfirmError  string
	// removeConfirmPaneIDs is set instead of removeConfirmPaneID/Branch when
	// the popup was opened from a multi-select bulk delete (the "v"/"x"
	// keys) — non-empty means "bulk mode", applying the chosen RemoveMode to
	// every id in this list instead of a single pane.
	removeConfirmPaneIDs []string

	// selectedPanes is the multi-select set (the "v" key) driving bulk
	// delete — process-memory only. Cleared whenever the bulk confirm popup
	// closes, by cancel or by confirm.
	selectedPanes map[string]bool

	// lastRemoved is the most recently removed worktree+branch (captured by
	// removeCmd right before deletion, detail.go), consumed by the "u" key
	// (undoRemoveCmd) — single-slot, process-memory only. A later removal
	// overwrites it; there is no undo history beyond the last one.
	lastRemoved *removedWorktree

	// aheadBehindByPane is the cache of "most recent git fetch" ahead/behind
	// badge keyed by pane id — same last-known/cursor-only trade-off as
	// dirtyByPane above.
	aheadBehindByPane map[string]string

	// pinnedPanes is the set of pane ids pinned to the top of the list (the
	// "p" key) — process-memory only, lost on sidebar restart. A pane that
	// disappears (agent exits) simply falls out of the next rebuilt row list;
	// its stale id staying in this set is harmless (never looked up again).
	pinnedPanes map[string]bool

	// filterQuery/filterEditing: the "/" text filter (name/agent/status
	// substring, case-insensitive). filterEditing is true while the user is
	// actively typing (all non-control keys are consumed as filter text);
	// pressing Enter or Esc while editing exits edit mode. Esc while NOT
	// editing clears the filter entirely. Process-memory only.
	filterQuery   string
	filterEditing bool
}

// NewModel builds the initial Model, with an empty list until the first
// RefreshMsg arrives. repoFilter defaults to "all" until repoFilterLoadedMsg
// arrives; theme/icons default until the first RefreshMsg loads from tmux
// (see refreshCmd).
func NewModel(tmuxPane string) Model {
	return Model{
		tmuxPane:   tmuxPane,
		viewport:   viewport.New(0, 0),
		repoFilter: RepoFilterAll,
		theme:      DefaultTheme(),
		icons:      DefaultIcons(),
	}
}

// Init fires the command to build the list for the first time, starts both
// tick loops, and reads @sidebar_cursor/@sidebar_repo_filter so state
// survives reopening the sidebar.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		refreshCmd(), tickRefreshCmd(), tickAnimCmd(),
		loadCursorCmd(), loadRepoFilterCmd(),
	)
}

// loadCursorCmd reads @sidebar_cursor (a subprocess) inside a tea.Cmd,
// without blocking Update. A value that fails to parse (never written, or
// garbage) is treated as 0.
func loadCursorCmd() tea.Cmd {
	return func() tea.Msg {
		raw, ok := tmux.GetOption(tmux.SidebarCursor)
		if !ok {
			return cursorLoadedMsg{Value: 0}
		}
		n, err := strconv.Atoi(raw)
		if err != nil {
			return cursorLoadedMsg{Value: 0}
		}
		return cursorLoadedMsg{Value: n}
	}
}

// loadRepoFilterCmd reads @sidebar_repo_filter at startup. An unrecognized or
// empty value falls back to RepoFilterAll via ParseRepoFilter (popup.go).
func loadRepoFilterCmd() tea.Cmd {
	return func() tea.Msg {
		raw, _ := tmux.GetOption(tmux.SidebarRepoFilter)
		return repoFilterLoadedMsg{Value: ParseRepoFilter(raw)}
	}
}

// saveCursorCmd writes the cursor to @sidebar_cursor (a subprocess) inside a
// tea.Cmd, returning cursorSavedMsg so Update knows whether the write
// succeeded (see cursorDirty — debt 3a Minor 2).
func saveCursorCmd(cursor int) tea.Cmd {
	return func() tea.Msg {
		ok := tmux.SetOptionResult(tmux.SidebarCursor, strconv.Itoa(cursor))
		return cursorSavedMsg{Success: ok}
	}
}

// saveRepoFilterCmd writes the repo filter to @sidebar_repo_filter, with the
// same retry mechanism as saveCursorCmd.
func saveRepoFilterCmd(name string) tea.Cmd {
	return func() tea.Msg {
		ok := tmux.SetOptionResult(tmux.SidebarRepoFilter, name)
		return repoFilterSavedMsg{Success: ok}
	}
}

// selectPaneCmd calls tmux.SelectPane (a subprocess) inside a tea.Cmd,
// without blocking Update.
func selectPaneCmd(paneID, fromPane string) tea.Cmd {
	return func() tea.Msg {
		tmux.SelectPane(paneID, fromPane)
		return nil
	}
}

// refreshCmd calls tmux.QuerySessions() and tmux.GetAllGlobalOptions() (two
// subprocess calls) inside the same tea.Cmd, so the color palette/icons
// (theme.go) reload "on every refresh" as required by the Theme/Icons
// comment — without blocking Update (runs in the separate goroutine
// bubbletea gives this Cmd).
func refreshCmd() tea.Cmd {
	return func() tea.Msg {
		sessions := tmux.QuerySessions()
		opts := tmux.GetAllGlobalOptions()

		msg := RefreshMsg{
			Sessions: sessions,
			Theme:    ThemeFromOptions(opts),
			Icons:    IconsFromOptions(opts),
			Avatar:   avatarConfigFromOptions(opts),
			Dialogs:  readDialogs(sessions),
		}
		graphics, sig := imageClients()
		msg.Images = imagesWanted(opts[imagesOption], graphics)
		msg.ClientsSig = sig
		msg.ImageOpts = map[string]string{}
		for k, v := range opts {
			if strings.HasPrefix(k, imageOption) {
				msg.ImageOpts[k] = v
			}
		}
		return msg
	}
}

// readDialogs captures the screen of every waiting agent and parses its
// prompt, so the list can offer the answers inline.
func readDialogs(sessions []tmux.SessionInfo) map[string]scrape.Dialog {
	out := map[string]scrape.Dialog{}
	for _, s := range sessions {
		for _, w := range s.Windows {
			for _, p := range w.Panes {
				if p.Status != tmux.StatusWaiting {
					continue
				}
				if screen, ok := tmux.RunTmux("capture-pane", "-p", "-t", p.PaneID); ok {
					if d, ok := scrape.ParseDialog(screen); ok {
						out[p.PaneID] = d
					}
				}
			}
		}
	}
	return out
}

func tickRefreshCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg {
		return refreshTickMsg{}
	})
}

// tickAnimCmd fires on the wall clock's animInterval boundaries (tea.Every),
// the same clock avatarTick() reads — tea.Tick would drift by the update's
// own time and every few ticks skip a GIF frame (a visible jump).
func tickAnimCmd() tea.Cmd {
	return tea.Every(animInterval, func(time.Time) tea.Msg {
		return AnimTickMsg{}
	})
}

// applyRows rebuilds rows from m.sessions + the current repoFilter, clamps
// the cursor, and loads the viewport content — used in every branch that
// changes either of those two things (RefreshMsg, repo filter change, popup
// confirmation).
func (m *Model) applyRows() {
	m.recomputeRepoNames()
	extras := rowExtras{
		Ports:         m.portsByPane,
		SessionNames:  m.sessionNames,
		Dirty:         m.dirtyByPane,
		AheadBehind:   m.aheadBehindByPane,
		Pinned:        m.pinnedPanes,
		Selected:      m.selectedPanes,
		FilterQuery:   m.filterQuery,
		CurrentWindow: m.windowID,
		Dialogs:       m.dialogs,
	}
	// The cursor follows its pane, not its index: a pane added, removed or
	// regrouped above it would otherwise move the highlight to another one.
	prev, hadPrev := paneIDAt(m.rows, m.cursor)
	m.rows = buildRows(m.sessions, m.repoFilter, extras)
	if idx, ok := paneIndexOf(m.rows, prev); hadPrev && ok {
		m.cursor = idx
	}
	m.cursor = clampCursor(m.cursor, paneRowCount(m.rows))
	m.syncViewport()
}

// syncViewport rebuilds the viewport content from the current rows/cursor,
// scrolling to the cursor (scroll.go — pays off debt 3a Minor 3), then
// applies that offset to viewport.YOffset. While the repo popup is open, the
// viewport content is the popup list (view.go), not scrolled to the pane
// cursor.
func (m *Model) syncViewport() {
	if m.removeConfirmOpen {
		// The worktree-delete confirmation popup REPLACES the viewport
		// content, the same way the repo-selection popup below does — the
		// sidebar has no layered compositing. Bulk mode (removeConfirmPaneIDs
		// non-empty) reuses the exact same popup, just with a synthetic
		// "N worktrees" title instead of one branch name — no view.go change
		// needed.
		paneID, branch := m.removeConfirmPaneID, m.removeConfirmBranch
		if n := len(m.removeConfirmPaneIDs); n > 0 {
			paneID, branch = "", fmt.Sprintf("%d worktrees", n)
		}
		m.viewport.SetContent(renderRemoveConfirmPopup(paneID, branch, m.removeConfirmError, m.width, m.theme))
		m.viewport.SetYOffset(0)
		return
	}

	if m.popup.IsRepoOpen() {
		names := RepoNames(m.allRepoNames())
		selected := m.popup.RepoSelected()
		m.viewport.SetContent(renderRepoPopup(names, selected, m.width, m.theme))

		// Important 2 (final review 3b): previously SetContent did not reset
		// YOffset, so the popup was drawn from whatever offset was left over
		// from the pane list (e.g. after scrolling to the bottom) — losing
		// both the title line and the selected line, with no key to recover
		// since the popup has no manual scroll-up. Build a NEW ScrollState
		// matching the popup content (line 0 is the repoPopupTitle title,
		// the selected line is at selected+1) then EnsureVisible — this both
		// fixes the stale-offset overflow and auto-scrolls correctly if the
		// repo list is longer than the pane height.
		m.scroll = ScrollState{}
		m.scroll.EnsureVisible(selected+1, len(names)+1, m.viewport.Height)
		m.viewport.SetYOffset(m.scroll.Offset)
		return
	}

	m.viewport.SetContent(renderRowsAt(m.rows, m.width, m.cursor, m.theme, m.icons, m.animFrame/2))
	if idx, ok := rowIndexOfCursor(m.rows, m.cursor); ok {
		m.scroll.EnsureVisible(idx, len(m.rows), m.viewport.Height)
		m.viewport.SetYOffset(m.scroll.Offset)
	}
}

// detailRefreshCmd builds the detailFetchCmd for the pane CURRENTLY UNDER
// THE CURSOR (paneIDAt(m.rows, m.cursor) — no more popup), reusing the
// activity mtime cache ONLY WHEN that cache belongs to this exact pane —
// switching panes makes the old cache meaningless (matches the same
// condition as version 4a's activityRefreshCmd). Returns nil when there's no
// pane under the cursor (empty list, or cursor sitting on a header) — there
// is nothing to ask for.
func (m *Model) detailRefreshCmd() tea.Cmd {
	id, ok := paneIDAt(m.rows, m.cursor)
	if !ok {
		return nil
	}
	info, _ := paneInfoByID(m.sessions, id)
	return detailFetchCmd(id, info.Path)
}

// applyDetailMsg applies the detailFetchCmd result to Model — ignored if the
// cursor has already moved to a different pane before this Cmd returns
// (stale result; the next detailRefreshCmd round/1s tick will re-ask for the
// correct current pane).
func (m *Model) applyDetailMsg(msg detailMsg) {
	cursorID, ok := paneIDAt(m.rows, m.cursor)
	if msg.PaneID == "" || !ok || msg.PaneID != cursorID {
		return
	}
	if m.dirtyByPane == nil {
		m.dirtyByPane = map[string]bool{}
	}
	m.dirtyByPane[msg.PaneID] = msg.GitData.ChangedFileCount() > 0
	if m.aheadBehindByPane == nil {
		m.aheadBehindByPane = map[string]string{}
	}
	m.aheadBehindByPane[msg.PaneID] = aheadBehindLabel(msg.GitData.AheadBehind)
	m.notes, m.notesScope = msg.Notes, msg.Scope
}

// allRepoNames returns the names of EVERY repo that currently has an agent
// pane, WITHOUT applying the status filter/repo filter — this is the list
// the repo-selection popup must see (matches repo_names() in the reference
// implementation, filter.rs:131-137: iterates state.repo_groups, i.e. the
// raw groups before filtering, not the already-filtered display rows). If
// repo names were derived from m.rows (already filtered), a repo whose
// panes are all hidden by the status filter would disappear from the popup
// entirely — incorrect relative to the reference implementation's behavior.
// Returns the cached version, recomputed in applyRows (the only place
// m.sessions changes). This function used to call GroupPanesByRepo — i.e.
// os.Lstat/os.ReadFile probing for .git for every pane — directly inside
// Update; while the repo popup is open, every j/k press ran it twice
// (handlePopupKey + syncViewport), so I/O scaled with keystroke rate.
func (m *Model) allRepoNames() []string {
	return m.repoNames
}

func (m *Model) recomputeRepoNames() {
	groups := GroupPanesByRepo(m.sessions)
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		names = append(names, g.Name)
	}
	m.repoNames = names
}

// Update handles a message. No branch calls tmux.QuerySessions() directly —
// it always goes through refreshCmd() so the subprocess call stays inside a
// tea.Cmd.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width
		// The key-hint line is drawn separately by view.go, OUTSIDE the
		// viewport — the remainder is the scrollable pane list. (The fixed
		// activity/history panel was removed: user feedback "don't show
		// history in the left pane".)
		m.resizeViewport()
		m.syncViewport()
		return m, nil

	case RefreshMsg:
		m.sessions = msg.Sessions
		m.theme = msg.Theme
		m.icons = msg.Icons
		m.dialogs = msg.Dialogs
		if msg.Avatar != m.avatar {
			m.avatar = msg.Avatar
			m.resizeViewport()
		}
		m.applyRows()
		// Follow focus: when the user switches to another agent pane in this
		// window, the highlight moves to it (the cursor stays free for j/k
		// until focus changes again).
		if f := focusedPane(m.sessions, m.windowID); f != "" && f != m.lastFocus {
			m.lastFocus = f
			if idx, ok := paneIndexOf(m.rows, f); ok && idx != m.cursor {
				m.cursor = idx
				m.cursorDirty = true
				m.syncViewport()
			}
		}

		prevAvatarH := m.avatarH()
		avatarCmd := tea.Batch(m.imageRefresh(msg), m.digestKick())
		if m.avatarH() != prevAvatarH { // a taller picture arrived (@agent_avatar_image_rows)
			m.resizeViewport()
		}

		// The bottom summary panel (part 6, replacing the "i" popup) tracks
		// the pane CURRENTLY UNDER THE CURSOR, always refetching git/activity
		// every 1s tick — no more gating on the popup (the detail popup was
		// removed entirely, real user feedback: "show it directly at the
		// root"). detailRefreshCmd returns nil itself when there's no pane
		// under the cursor.
		return m, tea.Batch(m.detailRefreshCmd(), avatarCmd)

	case imagesUploadedMsg:
		m.imgUploading = false
		if m.imgHave == nil {
			m.imgHave = map[uint32]bool{}
		}
		for _, id := range msg.ids {
			m.imgHave[id] = true
		}
		return m, nil

	case refreshTickMsg:
		// Steady tick: ask for fresh data AND schedule the next tick itself.
		// Kept separate from refreshRequestMsg so a burst of SIGUSR1 doesn't
		// double the number of ticks running.
		return m, tea.Batch(refreshCmd(), tickRefreshCmd())

	case refreshRequestMsg:
		return m, refreshCmd()

	case detailMsg:
		m.applyDetailMsg(msg)
		return m, nil

	case portScanMsg:
		m.portsByPane = computePortsByPane(m.sessions, msg.Listening, msg.Snapshot)
		m.applyRows()
		return m, nil

	case sessionNamesMsg:
		m.sessionNames = msg.Names
		m.applyRows()
		return m, nil

	case removeResultMsg:
		if msg.Success {
			m.removeConfirmOpen = false
			m.removeConfirmError = ""
			if msg.Undo != nil {
				m.lastRemoved = msg.Undo
			}
			// Fetch the new list RIGHT AWAY — the window/worktree just
			// disappeared, don't wait for the next 1s tick to see it.
			return m, refreshCmd()
		}
		// The error stays in the popup so the user sees it and can retry
		// (like the reference implementation's confirm_remove: the popup
		// does NOT auto-close on error).
		m.removeConfirmError = msg.Err
		m.syncViewport()
		return m, nil

	case cursorLoadedMsg:
		// Comes from tmux at startup — apply it directly, WITHOUT setting
		// cursorDirty (otherwise the next AnimTick would write the exact
		// value just read right back). @sidebar_cursor is shared by every
		// sidebar, so once this sidebar knows which pane is focused in its
		// own window (focus following, RefreshMsg), that wins: the shared
		// value is some other window's selection.
		if m.lastFocus != "" {
			return m, nil
		}
		m.cursor = clampCursor(msg.Value, paneRowCount(m.rows))
		m.syncViewport()
		return m, nil

	case repoFilterLoadedMsg:
		m.repoFilter = msg.Value
		m.applyRows()
		return m, nil

	case cursorSavedMsg:
		m.cursorSaveInFlight = false
		if msg.Success {
			m.cursorDirty = false
		}
		return m, nil

	case repoFilterSavedMsg:
		m.repoFilterSaveInFlight = false
		if msg.Success {
			m.repoFilterDirty = false
		}
		return m, nil

	case AnimTickMsg:
		m.animFrame++
		// The bubble's height follows what the avatar says (bubbleRows),
		// which can change between refreshes (the hour turns): keep the
		// list's height in step so the frame never outgrows the pane.
		if m.height > 0 && m.viewport.Height != max(0, m.height-footerHeight-m.avatarH()) {
			m.resizeViewport()
		}
		if m.animFrame%2 == 0 {
			m.syncViewport() // marquee: one step every 2 ticks (~400ms)
		}
		cmds := []tea.Cmd{tickAnimCmd()}
		// Debounce: collapse every change since the previous AnimTick into
		// exactly ONE tmux write here per kind — don't fire another one
		// while the previous write is still waiting for tmux to respond
		// (cursorSaveInFlight/...).
		if m.cursorDirty && !m.cursorSaveInFlight {
			m.cursorSaveInFlight = true
			cmds = append(cmds, saveCursorCmd(m.cursor))
		}
		if m.repoFilterDirty && !m.repoFilterSaveInFlight {
			m.repoFilterSaveInFlight = true
			cmds = append(cmds, saveRepoFilterCmd(m.repoFilter))
		}
		return m, tea.Batch(cmds...)

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		return m.handleMouse(msg)
	}

	return m, nil
}

// OpenSettings opens the settings screen (`tmux-agent config`) in a tmux
// popup over the client looking at pane. The popup blocks until closed, so
// it runs as a command; the sidebar picks changed options up on its next
// refresh.
func OpenSettings(pane string) tea.Cmd {
	return func() tea.Msg {
		bin, err := os.Executable()
		if err != nil {
			bin = "tmux-agent"
		}
		args := []string{"display-popup", "-E", "-w", "84", "-h", "90%"}
		if pane != "" {
			args = append(args, "-t", pane)
		}
		tmux.RunTmux(append(args, shellQuote(bin)+" config")...)
		return refreshRequestMsg{}
	}
}

// imageRefresh keeps the image avatar's pictures in the terminal: rebuilds
// the set when the character or your image options change, re-uploads when
// the attached clients change, uploads what's missing.
func (m *Model) imageRefresh(msg RefreshMsg) tea.Cmd {
	m.images = msg.Images && m.avatar.enabled()
	if !m.images {
		return nil
	}
	if msg.ClientsSig != m.imgClients {
		m.imgClients, m.imgHave = msg.ClientsSig, map[uint32]bool{}
	}
	maxCols := m.width - 1 // renderAvatar's left margin
	key := m.avatar.Character + "\x00" + strconv.Itoa(min(maxCols/2, maxImageRows))
	for _, k := range []string{imageOption, imageOption + "_sleep", imageOption + "_work", imageOption + "_happy", imageOption + "_alert", imageOption + "_sad", imageRowsOpt} {
		key += "\x00" + msg.ImageOpts[k]
	}
	var drop tea.Cmd
	if m.imgSet.data == nil || m.imgSetKey != key {
		widthOnly := m.imgSetKey != "" && strings.SplitN(m.imgSetKey, "\x00", 3)[2] == strings.SplitN(key, "\x00", 3)[2]
		old := m.imgSet
		m.imgSet, m.imgSetKey = buildImageSet(m.avatar.Character, msg.ImageOpts, maxCols), key
		// Your pictures changed: free the old ones in the terminal. Not on a
		// resize alone — a sidebar in another, wider window may still show
		// them (images live in the terminal, shared by every pane).
		if !widthOnly {
			drop = dropImages(old, m.imgSet, m.imgHave)
		}
	}
	if m.imgUploading {
		return drop
	}
	cmd := uploadImages(m.imgSet, m.imgHave)
	m.imgUploading = cmd != nil
	return tea.Batch(drop, cmd)
}

// avatarH is the height the avatar takes at the top (0 when off).
func (m Model) avatarH() int {
	switch {
	case !m.avatar.enabled():
		return 0
	case m.height >= m.tallHeight()+avatarListMin:
		return m.tallHeight()
	}
	return avatarHeight
}

// resizeViewport gives the list what's left under the avatar and above the
// key-hint line.
func (m *Model) resizeViewport() {
	m.viewport.Height = max(0, m.height-footerHeight-m.avatarH())
	m.syncViewport()
}

// quickAction acts on the most urgent alert (avatar.go: Act) — space, or a
// click on the avatar.
func (m Model) quickAction() (tea.Model, tea.Cmd) {
	alerts := Alerts(m.sessions) // same alerts as the avatar shows
	if len(alerts) == 0 {
		return m, nil
	}
	a, cfg, from := alerts[0], m.avatar, m.tmuxPane
	return m, func() tea.Msg {
		Act(a, cfg, from)
		return refreshRequestMsg{}
	}
}

// handleMouse: a click on the expand button (start of the bottom line)
// behaves like the o key; a click on a pane row selects it; the wheel moves
// the cursor like j/k. Everything else is ignored.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Button == tea.MouseButtonWheelDown:
		return m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	case msg.Button == tea.MouseButtonWheelUp:
		return m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	case msg.Button != tea.MouseButtonLeft || msg.Action != tea.MouseActionPress:
		return m, nil
	case msg.Y == m.height-1 && msg.X < lipgloss.Width(expandButton):
		return m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	case msg.Y < m.avatarH():
		return m.quickAction()
	case msg.Y-m.avatarH() < m.viewport.Height && !m.popup.IsRepoOpen() && !m.removeConfirmOpen:
		// Row under the click → its index among selectable pane rows.
		line := m.viewport.YOffset + msg.Y - m.avatarH()
		if line >= len(m.rows) || !isSelectablePaneRow(m.rows[line]) {
			return m, nil
		}
		idx := 0
		for _, r := range m.rows[:line] {
			if isSelectablePaneRow(r) {
				idx++
			}
		}
		if idx != m.cursor {
			m.cursor = idx
			m.cursorDirty = true
			m.syncViewport()
			if _, ok := paneIDAt(m.rows, m.cursor); ok {
				return m, m.detailRefreshCmd()
			}
		}
	}
	return m, nil
}

// handleKey handles a key. While a popup is open, navigation keys belong to
// the popup (clamped at the edges, no wraparound — popup.go); the "r" key
// that opens the popup only works when the popup is CLOSED, like the
// reference implementation (app/input.rs: the is_repo_popup_open() branch
// fully blocks the regular key branch below it).
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.removeConfirmOpen {
		return m.handleRemoveConfirmKey(msg)
	}
	if m.popup.IsRepoOpen() {
		return m.handlePopupKey(msg)
	}
	if m.filterEditing {
		return m.handleFilterKey(msg)
	}

	// Approval inbox: on a waiting agent, its prompt's keys answer it.
	if id, ok := paneIDAt(m.rows, m.cursor); ok {
		if d, ok := m.dialogs[id]; ok {
			for _, o := range d.Options {
				if msg.String() == o.Key {
					key := o.Key
					delete(m.dialogs, id) // gone once answered; refresh confirms
					m.applyRows()
					return m, func() tea.Msg {
						scrape.Answer(id, key)
						return refreshRequestMsg{}
					}
				}
			}
		}
	}

	switch msg.String() {
	case "q", "ctrl+c":
		// cursorDirty still being set means the most recent @sidebar_cursor
		// write is still waiting for the next AnimTick (debounce ~200ms) —
		// quitting right here would lose it. Flush that write first, then
		// Quit.
		if m.cursorDirty {
			return m, tea.Sequence(saveCursorCmd(m.cursor), tea.Quit)
		}
		return m, tea.Quit

	case "j", "down":
		if next, moved := moveCursor(m.cursor, 1, paneRowCount(m.rows)); moved {
			m.cursor = next
			m.cursorDirty = true
			m.syncViewport()
			// The bottom summary panel tracks the cursor (part 6, replacing
			// the "i" popup — real user feedback: "show it directly at the
			// root") — moving panes must refetch git/activity RIGHT AWAY,
			// not wait for the 1s tick.
			if _, ok := paneIDAt(m.rows, m.cursor); ok {
				return m, m.detailRefreshCmd()
			}
		}
		return m, nil

	case "k", "up":
		if next, moved := moveCursor(m.cursor, -1, paneRowCount(m.rows)); moved {
			m.cursor = next
			m.cursorDirty = true
			m.syncViewport()
			if _, ok := paneIDAt(m.rows, m.cursor); ok {
				return m, m.detailRefreshCmd()
			}
		}
		return m, nil

	case " ":
		return m.quickAction()

	case ",":
		return m, OpenSettings(m.tmuxPane)

	case "o":
		// Expand this pane into the full dashboard (expand.go). The respawn
		// replaces this process, so nothing after it matters.
		if m.tmuxPane == "" {
			return m, nil
		}
		pane := m.tmuxPane
		expand := func() tea.Msg { SetExpanded(pane, true); return nil }
		if m.cursorDirty {
			return m, tea.Sequence(saveCursorCmd(m.cursor), expand)
		}
		return m, expand

	case "r":
		m.popup.ToggleRepo(m.allRepoNames(), m.repoFilter)
		m.syncViewport()
		return m, nil

	case "enter":
		// No row at all (empty list or cursor out of range) does nothing.
		// The jump fires tmux's after-select-pane hook, which clears
		// @pane_unseen (agent-sidebar.conf).
		if paneID, ok := paneIDAt(m.rows, m.cursor); ok {
			return m, selectPaneCmd(paneID, m.tmuxPane)
		}
		return m, nil

	case "v":
		// Toggle multi-select on the pane under the cursor (bulk delete via
		// "x" below). Any row can be selected; ineligible ones (not
		// SidebarSpawned) are simply skipped when "x" collects the list —
		// same silent-ignore behavior as the single-row case.
		if row, ok := paneRowAt(m.rows, m.cursor); ok {
			if m.selectedPanes == nil {
				m.selectedPanes = map[string]bool{}
			}
			if m.selectedPanes[row.PaneID] {
				delete(m.selectedPanes, row.PaneID)
			} else {
				m.selectedPanes[row.PaneID] = true
			}
			m.applyRows()
		}
		return m, nil

	case "x":
		// Bulk path: if anything is selected, "x" collects the SELECTED rows
		// eligible for delete (SidebarSpawned) and opens the confirm popup
		// in bulk mode — takes priority over the single-row path below, even
		// if the cursor also happens to sit on an eligible row.
		if len(m.selectedPanes) > 0 {
			var ids []string
			for _, r := range m.rows {
				// Reads m.selectedPanes directly (the source of truth)
				// rather than r.Selected (a copy synced by buildRows) — a
				// pane that was selected then disappeared from the list is
				// simply absent from m.rows, so this loop never has to
				// special-case it.
				if m.selectedPanes[r.PaneID] && r.SidebarSpawned {
					ids = append(ids, r.PaneID)
				}
			}
			if len(ids) > 0 {
				m.removeConfirmOpen = true
				m.removeConfirmPaneIDs = ids
				m.removeConfirmPaneID = ""
				m.removeConfirmBranch = ""
				m.removeConfirmError = ""
				m.syncViewport()
				return m, nil
			}
		}
		// Only a row created by the sidebar's own spawn flow (SidebarSpawned)
		// can open the delete confirmation popup — this is the project's
		// only destructive path, silently ignored on a regular row instead
		// of reporting an error (the reference implementation has a flash
		// message here; the Go sidebar has no flash mechanism yet, see the
		// report).
		if row, ok := paneRowAt(m.rows, m.cursor); ok && row.SidebarSpawned {
			m.removeConfirmOpen = true
			m.removeConfirmPaneID = row.PaneID
			m.removeConfirmBranch = row.Branch
			m.removeConfirmError = ""
			m.syncViewport()
		}
		return m, nil

	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		// Jump straight to the Nth pane row (1-indexed, matching what the
		// user counts on screen) — same cursor-move-and-refetch as j/k
		// above, just landing directly on an index instead of stepping.
		n := int(msg.String()[0] - '1')
		if n < paneRowCount(m.rows) {
			m.cursor = n
			m.cursorDirty = true
			m.syncViewport()
			if _, ok := paneIDAt(m.rows, m.cursor); ok {
				return m, m.detailRefreshCmd()
			}
		}
		return m, nil

	case "p":
		// Pin/unpin the pane under the cursor — process-memory only (see
		// Model.pinnedPanes). Pinned panes float to a "Pinned" group right
		// after "Needs you" (buildRows).
		if row, ok := paneRowAt(m.rows, m.cursor); ok {
			if m.pinnedPanes == nil {
				m.pinnedPanes = map[string]bool{}
			}
			if m.pinnedPanes[row.PaneID] {
				delete(m.pinnedPanes, row.PaneID)
			} else {
				m.pinnedPanes[row.PaneID] = true
			}
			m.applyRows()
		}
		return m, nil

	case "S":
		// Manually clear a stuck subagent tree — the normal cleanup path
		// (OnSubagentStop) only fires when a subagent finishes NORMALLY; a
		// force-killed or crashed subagent leaves its entry orphaned
		// forever with no other way to clear it. ponytail: a real fix would
		// track per-entry liveness/TTL; this manual escape hatch is the
		// lazy version, upgrade if orphaned entries turn out to be common.
		if row, ok := paneRowAt(m.rows, m.cursor); ok {
			return m, clearSubagentsCmd(row.PaneID)
		}
		return m, nil

	case "/":
		m.filterEditing = true
		return m, nil

	case "u":
		// Undo the most recently removed worktree+branch (detail.go:
		// removeCmd/undoRemoveCmd) — single-slot, consumed on use.
		if m.lastRemoved != nil {
			w := *m.lastRemoved
			m.lastRemoved = nil
			return m, tea.Sequence(undoRemoveCmd(w), refreshCmd())
		}
		return m, nil
	}
	return m, nil
}

// clearSubagentsCmd unsets @pane_subagents for paneID (a subprocess) inside
// a tea.Cmd — see the "S" key above. The next refresh tick (already
// scheduled every second) picks up the change; no need to force an
// immediate refresh here.
func clearSubagentsCmd(paneID string) tea.Cmd {
	return func() tea.Msg {
		tmux.UnsetPaneOption(paneID, tmux.PaneSubagents)
		return nil
	}
}

// handleFilterKey handles keys while the "/" text filter is being edited —
// Esc cancels and clears the filter, Enter keeps it and stops editing
// (navigation keys work normally again), Backspace removes the last rune,
// any other printable rune is appended. Runs buildRows again on every
// change so the list narrows live as the user types.
func (m Model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.filterEditing = false
		m.filterQuery = ""
		m.applyRows()
		return m, nil

	case "enter":
		m.filterEditing = false
		return m, nil

	case "backspace":
		if m.filterQuery != "" {
			r := []rune(m.filterQuery)
			m.filterQuery = string(r[:len(r)-1])
			m.applyRows()
		}
		return m, nil
	}
	if msg.Type == tea.KeyRunes {
		m.filterQuery += string(msg.Runes)
		m.applyRows()
	}
	return m, nil
}

// handleRemoveConfirmKey handles keys while the worktree-delete confirmation
// popup is open — ported from the reference implementation's
// is_remove_confirm_open() branch in app/input.rs: Esc/n closes without
// changing anything, c deletes ONLY the window (keeps worktree+branch),
// Enter/y deletes both the window and the worktree+branch.
func (m Model) handleRemoveConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "n":
		m.removeConfirmOpen = false
		m.removeConfirmError = ""
		m.removeConfirmPaneIDs = nil
		m.selectedPanes = nil
		m.syncViewport()
		return m, nil

	case "c":
		return m, m.removeConfirmCmd(worktree.RemoveWindowOnly)

	case "enter", "y":
		return m, m.removeConfirmCmd(worktree.RemoveWindowAndWorktree)
	}
	return m, nil
}

// removeConfirmCmd builds the delete command(s) for the confirm popup —
// a single removeCmd in the normal single-row case, or one removeCmd per id
// batched with tea.Batch in bulk mode (removeConfirmPaneIDs non-empty, set
// by the "v"/"x" multi-select flow in handleKey). Consumes
// removeConfirmPaneIDs/selectedPanes: the selection is spent the moment the
// bulk delete is confirmed, same as it is on cancel (handleRemoveConfirmKey
// above).
func (m *Model) removeConfirmCmd(mode worktree.RemoveMode) tea.Cmd {
	if len(m.removeConfirmPaneIDs) == 0 {
		return removeCmd(m.removeConfirmPaneID, mode)
	}
	ids := m.removeConfirmPaneIDs
	m.removeConfirmPaneIDs = nil
	m.selectedPanes = nil
	cmds := make([]tea.Cmd, len(ids))
	for i, id := range ids {
		cmds[i] = removeCmd(id, mode)
	}
	return tea.Batch(cmds...)
}

// handlePopupKey handles keys while the repo popup is open — ported from the
// is_repo_popup_open() branch in app/input.rs: j/k (clamped at the edges, NO
// wraparound, quite unlike the status filter's left/right — verified
// against the reference implementation), Enter confirms, Esc closes without
// changing anything.
func (m Model) handlePopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	repos := m.allRepoNames()
	switch msg.String() {
	case "esc":
		m.popup.CloseRepo()
		m.syncViewport()
		return m, nil

	case "j", "down":
		m.popup.RepoNavDown(len(RepoNames(repos)))
		m.syncViewport()
		return m, nil

	case "k", "up":
		m.popup.RepoNavUp()
		m.syncViewport()
		return m, nil

	case "enter":
		if filter, ok := m.popup.ConfirmRepo(repos); ok {
			m.repoFilter = filter
			m.repoFilterDirty = true
			m.applyRows()
			return m, nil
		}
		m.syncViewport()
		return m, nil
	}
	return m, nil
}
