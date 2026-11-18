package tmux

import (
	"os"
	"path"
	"sort"
	"strconv"
	"strings"

	"tmux-agent/internal/activity"
	"tmux-agent/internal/process"
)

// Field indices at the window level in a `tmux list-panes -F` line.
// Must be kept in sync with paneFormat().
const (
	sfSessionName     = 0
	sfWindowID        = 1
	sfWindowIndex     = 2
	sfWindowName      = 3
	sfWindowActive    = 4
	sfAutomaticRename = 5
	// sfSessionAttached is #{session_attached}: the number of tmux clients
	// attached to this session. window_active/pane_active are "current"
	// state PER session (tmux remembers it for every session, including
	// sessions no one is looking at) — without sfSessionAttached, a session
	// running in the background with no client attached would still report
	// its pane as "active" exactly like the session the user is actually
	// looking at, causing HeaderFocus (group.go) to highlight the wrong
	// group.
	sfSessionAttached = 6

	// paneLineOffset is where the pane-level field section begins.
	paneLineOffset = 7

	// minSessionFields is the minimum number of fields a valid line must have.
	minSessionFields = 32
)

// Indices within the pane-level tail section. Each value = (absolute index) - paneLineOffset.
const (
	pfPaneActive      = 0  // absolute 6
	pfPaneStatus      = 1  // 7  @pane_status
	pfPaneAttention   = 2  // 8  @pane_attention
	pfAgent           = 3  // 9  @pane_agent
	pfPaneName        = 4  // 10 @pane_name
	pfPaneCurrentPath = 5  // 11 pane_current_path
	pfCurrentCommand  = 6  // 12 pane_current_command
	pfPaneRole        = 7  // 13 @pane_role
	pfPaneID          = 8  // 14 pane_id
	pfPrompt          = 9  // 15 @pane_prompt
	pfPromptSource    = 10 // 16 @pane_prompt_source
	pfStartedAt       = 11 // 17 @pane_started_at
	pfWaitReason      = 12 // 18 @pane_wait_reason
	pfPanePID         = 13 // 19 pane_pid
	pfSubagents       = 14 // 20 @pane_subagents
	pfPaneCwd         = 15 // 21 @pane_cwd
	pfPermissionMode  = 16 // 22 @pane_permission_mode
	pfWorktreeName    = 17 // 23 @pane_worktree_name
	pfWorktreeBranch  = 18 // 24 @pane_worktree_branch
	pfSessionID       = 19 // 25 @pane_session_id
	pfSidebarSpawned  = 20 // 26 @agent-sidebar-spawned
	pfBgCmd           = 21 // 27 @pane_bg_cmd
	pfUnseen          = 22 // 28 @pane_unseen
	pfStuck           = 23 // 29 @pane_stuck
	pfConflict        = 24 // 30 @pane_conflict

	// minPaneFields = minSessionFields - paneLineOffset
	minPaneFields = 25
)

// paneFormat builds the format string for `tmux list-panes -F`.
//
// Every field is wrapped in #{q:…} so that a pipe character occurring inside
// user content (cwd, prompt, branch name) is escaped to \| and doesn't break
// the structure when we split it.
//
// THE ORDER HERE IS A CONTRACT with the two index tables above. Adding,
// removing, or reordering a field without also updating the tables is a
// silent breakage, with no error to catch it.
func paneFormat() string {
	fields := []string{
		q("session_name"),
		q("window_id"),
		q("window_index"),
		q("window_name"),
		q("window_active"),
		q("automatic-rename"),
		q("session_attached"),
		q("pane_active"),
		q(PaneStatusOpt),
		q(PaneAttention),
		q(PaneAgent),
		q(PaneName),
		q("pane_current_path"),
		q("pane_current_command"),
		q(PaneRole),
		q("pane_id"),
		q(PanePrompt),
		q(PanePromptSource),
		q(PaneStartedAt),
		q(PaneWaitReason),
		q("pane_pid"),
		q(PaneSubagents),
		q(PaneCwd),
		q(PanePermissionMode),
		q(PaneWorktreeName),
		q(PaneWorktreeBranch),
		q(PaneSessionID),
		q(SpawnedOption),
		q(PaneBgCmd),
		q(PaneUnseen),
		q(PaneStuck),
		q(PaneConflict),
	}
	return strings.Join(fields, "|")
}

func q(field string) string {
	return "#{q:" + field + "}"
}

// splitTmuxFields splits a line on delim, respecting the backslash escaping
// produced by #{q:…}. Empty fields are kept so indices don't shift.
func splitTmuxFields(line string, delim rune) []string {
	var (
		fields  []string
		current strings.Builder
		escaped bool
	)

	for _, ch := range line {
		if escaped {
			current.WriteRune(ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == delim {
			fields = append(fields, current.String())
			current.Reset()
			continue
		}
		current.WriteRune(ch)
	}

	// A trailing backslash at end of line has nothing to escape; keep it as-is.
	if escaped {
		current.WriteRune('\\')
	}
	fields = append(fields, current.String())

	return fields
}

// clearedPaneKeys are the options cleared when cleaning up a dead agent pane
// — NOT the full set of @pane_* constants in options.go. When adding a new
// @pane_* constant that represents STATE OF ONE AGENT RUN (prompt, cwd,
// subagents, …), it must be added here too. The following 6 constants are
// deliberately excluded because they are not state of a single agent run,
// and clearing them would be wrong:
//   - PaneRole: identifies the pane ("sidebar" or not), independent of
//     whether an agent is currently running — clearing it would make the
//     sidebar think it's an ordinary pane after its own imaginary "agent"
//     gets cleaned up.
//   - PaneName: a user-chosen label for the pane, outliving any single agent
//     run — clearing state from an old run doesn't mean the user's label
//     should be erased.
//   - PaneNotificationRunID, PaneOSNotifyPermissionRequired,
//     PaneOSNotifyTaskCompleted, PaneOSNotifyTaskFailed: dedup markers for OS
//     notifications. Clearing them the moment the poll loop detects a dead
//     agent would reopen a window for duplicate notifications if the cleanup
//     and detection cycles fall out of sync; letting the hook overwrite them
//     itself on the next SessionStart is safer than proactively clearing
//     them here.
var clearedPaneKeys = []string{
	PaneAgent,
	PaneAgentSrc,
	PaneUnseen,
	PaneStuck,
	PaneConflict,
	PanePrompt,
	PanePromptSource,
	PaneBgCmd,
	PaneBgLive,
	PaneSubagents,
	PaneCwd,
	PanePermissionMode,
	PaneWorktreeName,
	PaneWorktreeBranch,
	PaneSessionID,
	PanePendingSessionEnd,
	PanePendingWorktreeRemove,
	PaneStartedAt,
	PaneWaitReason,
	PaneAttention,
	PaneStatusOpt,
}

// maxPromptChars is the max number of prompt characters kept. Counted in
// runes, not bytes, so multi-byte characters aren't cut mid-character.
const maxPromptChars = 200

// parsePaneFields builds a PaneInfo from the pane-level field section of a
// list-panes line.
//
// ok = false means "skip this pane": the line is missing fields, this is the
// sidebar's own pane, there's no agent, or the agent has died and was just
// cleaned up.
func parsePaneFields(parts []string, snap *process.Snapshot) (PaneInfo, bool) {
	if len(parts) < minPaneFields {
		return PaneInfo{}, false
	}
	// The sidebar doesn't list itself.
	if parts[pfPaneRole] == "sidebar" {
		return PaneInfo{}, false
	}

	agent, ok := ParseAgentType(parts[pfAgent])
	if !ok {
		return PaneInfo{}, false
	}

	currentCommand := parts[pfCurrentCommand]
	panePID, _ := strconv.Atoi(parts[pfPanePID])

	// Codex and OpenCode leave stale metadata behind in tmux after the agent
	// exits and the pane falls back to the user's shell. Codex has no exit
	// hook, and OpenCode runs on Bun so the process-exit handler doesn't run.
	// So the poll loop itself must clean up: the first poll after the agent
	// disappears clears the options + log file. Later polls stop early at
	// ParseAgentType because @pane_agent has already been cleared. Claude
	// doesn't go through this path — it has a SessionEnd hook.
	if (agent == AgentCodex || agent == AgentOpenCode) && IsShellCommand(currentCommand) {
		alive := panePID != 0 && snap != nil && snap.TreeHasAgent([]int{panePID}, string(agent))
		if !alive {
			clearAgentPaneState(parts[pfPaneID])
			return PaneInfo{}, false
		}
	}

	// @pane_cwd is written by the hook from the agent's real cwd, more
	// accurate than pane_current_path when the agent changes directory.
	pathValue := parts[pfPaneCwd]
	if pathValue == "" {
		pathValue = parts[pfPaneCurrentPath]
	}

	// Only Claude writes the permission mode via its hook. For
	// Codex/OpenCode, any value present in tmux is leftover junk; Codex's
	// mode is re-derived from the process in applyCodexPermissionModes.
	permissionMode := ModeDefault
	if agent == AgentClaude {
		permissionMode = ParsePermissionMode(parts[pfPermissionMode])
	}

	startedAt, _ := strconv.ParseInt(parts[pfStartedAt], 10, 64)

	return PaneInfo{
		PaneID:           parts[pfPaneID],
		PaneActive:       parts[pfPaneActive] == "1",
		Status:           ParsePaneStatus(parts[pfPaneStatus]),
		Attention:        parts[pfPaneAttention] != "",
		Agent:            agent,
		Path:             pathValue,
		CurrentCommand:   currentCommand,
		Prompt:           sanitizePrompt(parts[pfPrompt]),
		PromptIsResponse: parts[pfPromptSource] == "response",
		StartedAt:        startedAt,
		WaitReason:       parts[pfWaitReason],
		PermissionMode:   permissionMode,
		Subagents:        parseSubagents(parts[pfSubagents]),
		PanePID:          panePID,
		Worktree: WorktreeMetadata{
			Name:   parts[pfWorktreeName],
			Branch: parts[pfWorktreeBranch],
		},
		SessionID:      parts[pfSessionID],
		SessionName:    "",
		SidebarSpawned: parts[pfSidebarSpawned] == "1",
		BgShellCmd:     parts[pfBgCmd],
		Unseen:         parts[pfUnseen] == "1",
		Stuck:          parts[pfStuck],
		Conflict:       parts[pfConflict],
		Name:           parts[pfPaneName],
	}, true
}

// clearAgentPaneState clears every agent option and the log file for a pane.
func clearAgentPaneState(paneID string) {
	for _, key := range clearedPaneKeys {
		UnsetPaneOption(paneID, key)
	}
	os.Remove(activity.LogFilePath(paneID))
}

// sanitizePrompt cleans a prompt read from tmux to make it safe to display.
func sanitizePrompt(raw string) string {
	if raw == "" {
		return ""
	}
	// Filter out messages injected by the system. We don't use a generic
	// brace check, since a legitimate prompt can contain comparisons or code
	// snippets.
	for _, marker := range []string{"<task-notification>", "<system-reminder>", "<task-status>"} {
		if strings.Contains(raw, marker) {
			return ""
		}
	}
	runes := []rune(raw)
	if len(runes) > maxPromptChars {
		return string(runes[:maxPromptChars])
	}
	return raw
}

// idPrefixLen is the number of leading characters of the agent id used as a
// label. Stable per instance, so the label doesn't jump around when a
// sibling subagent stops.
const idPrefixLen = 4

// parseSubagents reads @pane_subagents: comma-separated entries, each either
// "type" (legacy format) or "type:id" (current) → "type #<first 4 chars of id>".
func parseSubagents(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		agentType, id, found := strings.Cut(entry, ":")
		if !found || id == "" {
			out = append(out, entry)
			continue
		}
		idRunes := []rune(id)
		if len(idRunes) > idPrefixLen {
			idRunes = idRunes[:idPrefixLen]
		}
		out = append(out, agentType+" #"+string(idRunes))
	}
	return out
}

// shellCommands are commands treated as "the pane has returned to a shell",
// i.e. the agent no longer occupies the pane.
var shellCommands = map[string]bool{
	"ash": true, "bash": true, "csh": true, "dash": true, "elvish": true,
	"fish": true, "ksh": true, "mksh": true, "nu": true, "oksh": true,
	"pdksh": true, "posh": true, "powershell": true, "powershell.exe": true,
	"pwsh": true, "sh": true, "tcsh": true, "xonsh": true, "zsh": true,
}

func IsShellCommand(command string) bool {
	token, _, _ := strings.Cut(strings.TrimSpace(command), " ")
	if token == "" {
		return false
	}
	return shellCommands[strings.ToLower(path.Base(token))]
}

// QuerySessions returns the session → window → pane tree, containing only
// panes currently running an agent. Returns an empty slice on tmux error.
func QuerySessions() []SessionInfo {
	sessions, _ := QuerySessionsWithSnapshot()
	return sessions
}

// QuerySessionsWithSnapshot does the same as QuerySessions and also returns
// the process snapshot it used, so the caller (part 4: port scanning) doesn't
// have to take another one.
//
// The whole tree is fetched in ONE list-panes -a call, plus at most one ps
// call. The old approach of asking tmux about each pane individually was
// N+1 subprocess calls per second.
func QuerySessionsWithSnapshot() ([]SessionInfo, *process.Snapshot) {
	out, ok := RunTmux("list-panes", "-a", "-F", paneFormat())
	if !ok {
		return nil, nil
	}

	snap := snapshotForPanes(out)
	sessionOrder, windowsBySession, codexPIDs := buildSessionHierarchy(out, snap)
	if len(codexPIDs) > 0 && snap != nil {
		applyCodexPermissionModes(windowsBySession, codexPIDs, snap)
	}
	return finalizeSessions(sessionOrder, windowsBySession), snap
}

// codexPIDEntry is the minimal info needed to later patch the right pane:
// (window id, pane's position within the window, pid).
type codexPIDEntry struct {
	windowID string
	paneIdx  int
	pid      int
}

// windowOrder keeps the window order within a session; Go maps have no
// order, but the sidebar must display a stable order across refreshes.
type windowOrder struct {
	ids      []string
	byID     map[string]*WindowInfo
	attached bool
}

func buildSessionHierarchy(out string, snap *process.Snapshot) ([]string, map[string]*windowOrder, []codexPIDEntry) {
	var sessionOrder []string
	sessions := map[string]*windowOrder{}
	var codexPIDs []codexPIDEntry
	seenPIDs := map[int]bool{}

	for _, line := range strings.Split(out, "\n") {
		parts := splitTmuxFields(line, '|')
		if len(parts) < minSessionFields {
			continue
		}

		sessionName := parts[sfSessionName]
		windowID := parts[sfWindowID]
		// Pass the tail section through directly instead of rejoining it with
		// "|" and splitting again — rejoining would turn a pipe character
		// occurring inside content (cwd, prompt, branch) into a real
		// delimiter and shift every index after it.
		paneFields := parts[paneLineOffset:]

		// Grouped sessions share panes: the same pane_pid can appear in
		// multiple sessions. Keep only the first occurrence. pid 0 means it
		// couldn't be read, so it's not deduplicated.
		if pid, err := strconv.Atoi(paneFields[pfPanePID]); err == nil && pid != 0 {
			if seenPIDs[pid] {
				continue
			}
			seenPIDs[pid] = true
		}

		wo, ok := sessions[sessionName]
		if !ok {
			wo = &windowOrder{byID: map[string]*WindowInfo{}, attached: parts[sfSessionAttached] != "0"}
			sessions[sessionName] = wo
			sessionOrder = append(sessionOrder, sessionName)
		}

		window, ok := wo.byID[windowID]
		if !ok {
			window = &WindowInfo{
				WindowID:     windowID,
				WindowIndex:  parts[sfWindowIndex],
				WindowName:   parts[sfWindowName],
				WindowActive: parts[sfWindowActive] == "1",
				AutoRename:   parts[sfAutomaticRename] == "1",
			}
			wo.byID[windowID] = window
			wo.ids = append(wo.ids, windowID)
		}

		pane, ok := parsePaneFields(paneFields, snap)
		if !ok {
			continue
		}
		if pane.Agent == AgentCodex && pane.PanePID != 0 {
			codexPIDs = append(codexPIDs, codexPIDEntry{
				windowID: windowID,
				paneIdx:  len(window.Panes),
				pid:      pane.PanePID,
			})
		}
		window.Panes = append(window.Panes, pane)
	}

	return sessionOrder, sessions, codexPIDs
}

// finalizeSessions flattens the tree, dropping windows with no panes left
// and sessions with no windows left.
func finalizeSessions(order []string, sessions map[string]*windowOrder) []SessionInfo {
	var out []SessionInfo
	for _, name := range order {
		wo := sessions[name]
		var windows []WindowInfo
		for _, id := range wo.ids {
			if w := wo.byID[id]; len(w.Panes) > 0 {
				windows = append(windows, *w)
			}
		}
		if len(windows) > 0 {
			out = append(out, SessionInfo{SessionName: name, Attached: wo.attached, Windows: windows})
		}
	}
	return out
}

// snapshotForPanes only runs ps when actually needed, i.e. when there's at
// least one Codex or OpenCode pane. Sessions with only Claude don't pay for it.
func snapshotForPanes(out string) *process.Snapshot {
	needed := false
	for _, line := range strings.Split(out, "\n") {
		parts := splitTmuxFields(line, '|')
		if len(parts) < minSessionFields {
			continue
		}
		agent, ok := ParseAgentType(parts[paneLineOffset+pfAgent])
		if ok && (agent == AgentCodex || agent == AgentOpenCode) {
			needed = true
			break
		}
	}
	if !needed {
		return nil
	}
	snap, ok := process.Scan()
	if !ok {
		return nil
	}
	return snap
}

// applyCodexPermissionModes derives Codex's permission mode from its command
// line arguments, since Codex's hook doesn't write it to tmux.
func applyCodexPermissionModes(sessions map[string]*windowOrder, entries []codexPIDEntry, snap *process.Snapshot) {
	for _, wo := range sessions {
		for _, windowID := range wo.ids {
			window := wo.byID[windowID]
			for _, entry := range entries {
				if entry.windowID != windowID || entry.paneIdx >= len(window.Panes) {
					continue
				}
				// Descendants returns a map (iteration order undefined). If
				// the process tree has multiple `codex` processes with
				// different flags (e.g. one --full-auto and one --yolo),
				// iterating the map directly would make the permission badge
				// jump randomly between refreshes even though the process
				// tree hasn't changed. Sort pids first so the result is
				// deterministic.
				descendants := snap.Descendants([]int{entry.pid})
				pids := make([]int, 0, len(descendants))
				for pid := range descendants {
					pids = append(pids, pid)
				}
				sort.Ints(pids)
				for _, pid := range pids {
					info, ok := snap.InfoByPID[pid]
					if !ok || process.CommandBasename(info.Comm) != string(AgentCodex) {
						continue
					}
					mode := detectCodexPermissionMode(info.Args)
					window.Panes[entry.paneIdx].PermissionMode = mode
					if mode != ModeDefault {
						break
					}
				}
			}
		}
	}
}

// detectCodexPermissionMode reads Codex's permission mode from its process arguments.
func detectCodexPermissionMode(args string) PermissionMode {
	if strings.Contains(args, "dangerously-bypass-approvals-and-sandbox") ||
		strings.Contains(args, "--yolo") {
		return ModeBypassPermissions
	}
	if strings.Contains(args, "--full-auto") {
		return ModeAuto
	}
	return ModeDefault
}
