package tmux

import "strings"

// ─── Pane-level options ───────────────────────────────────────────────
//
// Single source of truth for every @pane_* option written by the hook and
// read by the sidebar. The agent's hook, the refresh path, and the query
// layer all go through these constants, so a typo can't silently corrupt
// state.
//
// When adding a new key that represents the state of ONE AGENT RUN, consider
// whether clearedPaneKeys (query.go) needs updating — but not every key
// belongs there; see the comment on clearedPaneKeys for which keys are
// deliberately excluded.
const (
	// PaneAgent — the agent name the hook recognizes ("claude" / "codex" / "opencode").
	PaneAgent = "@pane_agent"
	// PaneName — optional agent name, set with `tmux-agent name`. Shown in
	// place of the directory name, and usable as a target for send/read/wait.
	PaneName = "@pane_name"
	// PaneAttention — attention flag ("notification" / "clear").
	PaneAttention = "@pane_attention"
	// PaneCwd — working directory reported by the hook, takes priority over pane_current_path.
	PaneCwd = "@pane_cwd"
	// PaneBgCmd — the most recent background Bash command (sanitized). Its
	// presence signals "a background shell is still alive".
	PaneBgCmd = "@pane_bg_cmd"
	// PaneBgLive — raw command of each background run still believed alive,
	// one per line; the hook drops the ones gone from the process tree.
	PaneBgLive = "@pane_bg_live"
	// PaneNotificationRunID — epoch-ms id regenerated on every SessionStart.
	PaneNotificationRunID = "@pane_notification_run_id"
	// PaneOSNotifyPermissionRequired — dedup marker for notifications.
	PaneOSNotifyPermissionRequired = "@pane_os_notify_permission_required"
	// PaneOSNotifyTaskCompleted — dedup marker for notifications.
	PaneOSNotifyTaskCompleted = "@pane_os_notify_task_completed"
	// PaneOSNotifyTaskFailed — dedup marker for notifications.
	PaneOSNotifyTaskFailed = "@pane_os_notify_task_failed"
	// PanePendingSessionEnd — legacy marker, definition kept but never set.
	PanePendingSessionEnd = "@pane_pending_session_end"
	// PanePendingWorktreeRemove — marker for a pending worktree removal.
	PanePendingWorktreeRemove = "@pane_pending_worktree_remove"
	// PanePermissionMode — the agent's permission mode.
	PanePermissionMode = "@pane_permission_mode"
	// PanePrompt — the most recent prompt or response.
	PanePrompt = "@pane_prompt"
	// PanePromptSource — "user" or "response".
	PanePromptSource = "@pane_prompt_source"
	// PaneRole — "sidebar" for the sidebar's own pane, so it can exclude itself from the list.
	PaneRole = "@pane_role"
	// PaneSessionID — session id reported by the agent.
	PaneSessionID = "@pane_session_id"
	// PaneStartedAt — epoch seconds when the current run started.
	PaneStartedAt = "@pane_started_at"
	// PaneStatusOpt — high-level status. Named with an Opt suffix to avoid
	// clashing with the PaneStatus type.
	PaneStatusOpt = "@pane_status"
	// PaneSubagents — list of running subagents, comma-separated.
	PaneSubagents = "@pane_subagents"
	// PaneWaitReason — reason currently waiting.
	PaneWaitReason = "@pane_wait_reason"
	// PaneWorktreeBranch — branch name when the pane is attached to a git worktree.
	PaneWorktreeBranch = "@pane_worktree_branch"
	// PaneWorktreeName — worktree directory name.
	PaneWorktreeName = "@pane_worktree_name"
	// PaneUnseen — "1" when the agent finished a run while the user wasn't
	// looking at the pane (herdr's "done" state). Cleared by the
	// after-select-pane/window hooks in agent-sidebar.conf as soon as the
	// pane is viewed, and by any non-idle status write.
	PaneUnseen = "@pane_unseen"
	// PaneAgentSrc — "scrape" when @pane_agent/@pane_status were written by
	// the screen scraper (internal/scrape) rather than an agent hook. Hooks
	// clear it, which hands the pane back to hook-driven state.
	PaneAgentSrc = "@pane_agent_src"
	// PaneStuck — minutes a running agent has shown no activity, set by the
	// watchdog (internal/scrape) past @agent_stuck_minutes; unset when it
	// moves again.
	PaneStuck = "@pane_stuck"
	// PaneConflict — "<file> with <other agent>" when two agents of the same
	// project edited the same file recently (watchdog).
	PaneConflict = "@pane_conflict"
)

// BGCmdPlaceholder is the value written to PaneBgCmd when the hook has no real command.
const BGCmdPlaceholder = "(background shell)"

// SpawnedOption marks a window created by the sidebar's spawn flow. Set at
// the window level so a pane split off from it later inherits it.
const SpawnedOption = "@agent-sidebar-spawned"

// ─── Config options + markers for the spawn/remove worktree flow (part 5) ─────
const (
	// AgentOption — default agent used for spawn when no flag is passed.
	AgentOption = "@agent-sidebar-default-agent"
	// BranchPrefixOption — git branch name prefix when spawn creates a new worktree.
	BranchPrefixOption = "@agent-sidebar-branch-prefix"
	// WorktreeDirOption — directory (relative to the repo) holding worktrees created by spawn.
	WorktreeDirOption = "@agent-sidebar-worktree-dir"
	// SpawnedFromOption — path to the origin repo a worktree was spawned from.
	SpawnedFromOption = "@agent-sidebar-spawned-from"
	// SpawnedWorktreeOption — path to the worktree created by spawn.
	SpawnedWorktreeOption = "@agent-sidebar-spawned-worktree"
	// SpawnedBranchOption — git branch name created by spawn.
	SpawnedBranchOption = "@agent-sidebar-spawned-branch"
)

// ─── Sidebar global options ─────────────────────────────────────────
const (
	SidebarPID                 = "@sidebar_pid"
	SidebarWidth               = "@sidebar_width"
	SidebarPosition            = "@sidebar_position"
	SidebarCursor              = "@sidebar_cursor"
	SidebarRepoFilter          = "@sidebar_repo_filter"
	SidebarPet                 = "@sidebar_pet"
	SidebarNotifications       = "@sidebar_notifications"
	SidebarNotificationsEvents = "@sidebar_notifications_events"
)

// ─── Theme (new UI, borrowed from herdr — see spec part 6) ────────────
//
// Replaces the old scattered 35 "@sidebar_color_*"/"@sidebar_icon_*" options
// (one option per color role): AgentTheme selects ONE built-in palette
// ("dark" by default, "light"), and AgentColorPrefix/AgentIconPrefix let
// callers override INDIVIDUAL tokens when needed (e.g. "@agent_color_accent",
// "@agent_icon_confirm") — the list of valid tokens lives in
// internal/sidebar/theme.go (themeColorTokens/themeIconTokens), not repeated
// here to avoid two sources of truth.
//
// DECISION: we do not read back the old "@sidebar_color_*"/"@sidebar_icon_*"
// options — the new palette has entirely different token roles (adds
// status_confirm/status_done, drops the unused pet/badge/response-arrow),
// with no 1:1 mapping to fall back to safely. Users overriding colors via the
// old options need to migrate to the new names.
const (
	AgentTheme       = "@agent_theme"
	AgentColorPrefix = "@agent_color_"
	AgentIconPrefix  = "@agent_icon_"
)

// rawGlobalOption calls "show -gv <name>" — tmux returns the RAW value as-is,
// with no quoting or escaping — then trims exactly the ONE trailing newline
// tmux always appends to the command output. This is the ONLY place in this
// package that calls "show -gv", so GetOption and the requery path in
// GetAllGlobalOptions can't drift apart (see the comment on
// GetAllGlobalOptions).
//
// Only trims the trailing "\n", does NOT TrimSpace: leading/trailing
// whitespace within the value is real data, not junk to clean up.
func rawGlobalOption(name string) (string, bool) {
	out, ok := RunTmux("show", "-gv", name)
	if !ok {
		return "", false
	}
	return strings.TrimSuffix(out, "\n"), true
}

// GetOption reads a global option. ok = false on error or an empty value —
// the caller falls back to its own default.
func GetOption(name string) (string, bool) {
	value, ok := rawGlobalOption(name)
	if !ok {
		return "", false
	}
	return value, value != ""
}

// GetAllGlobalOptions fetches every global option in a single subprocess
// call, instead of one call per option. The refresh path reads dozens of
// options every second.
//
// MUST return the same value as GetOption (which uses "show -gv") for the
// same option — two paths reading the same tmux data that disagree is a
// silent bug.
//
// "show -g" prints values in a format tmux chooses itself: wrapped in single
// or double quotes when needed, and escaping certain characters (including
// "\" and C-style control characters like "\n", "\t") with "\". We once wrote
// a parser for that format (unescapeTmuxValue, since removed) — it wasn't
// enough to correctly reverse control-character escaping, causing drift from
// GetOption. Rather than keep guessing, we let TMUX ITSELF do the decoding:
// "show -g" here is only used to get the LIST OF KEYS; the value on that line
// is only used directly when there's no sign it was escaped (doesn't contain
// `\`, `'`, or `"`). If there is such a sign, we re-query that exact key with
// "show -gv" (rawGlobalOption) to get the raw value — an extra subprocess
// call, but only for the options that actually need it; everything else
// (numbers, color names, icons, …) costs nothing extra.
func GetAllGlobalOptions() map[string]string {
	opts := map[string]string{}
	out, ok := RunTmux("show", "-g")
	if !ok {
		return opts
	}
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		// Format: "option-name value" or "@user_option value".
		key, value, found := strings.Cut(line, " ")
		if !found {
			continue
		}
		if strings.ContainsAny(value, `\"'`) {
			// tmux has quoted/escaped this value — re-query it with show -gv
			// so TMUX ITSELF returns the raw value, without us decoding it.
			// If the second command fails (rare — e.g. the option was just
			// unset in between the two commands), keep the raw escaped value
			// as a fallback, better than losing the key entirely.
			if raw, ok := rawGlobalOption(key); ok {
				value = raw
			}
		}
		opts[key] = value
	}
	return opts
}

// SetOption writes a global option (e.g. @sidebar_cursor so multiple
// sidebars in multiple windows stay in sync). Errors are swallowed, like
// every other write function in this package — the philosophy is silent,
// graceful degradation.
func SetOption(key, value string) {
	RunTmux("set", "-g", key, value)
}

// SetOptionResult writes a global option like SetOption, but reports whether
// the write succeeded — for callers that need to know so they can retry
// (e.g. the debounced write of @sidebar_cursor in internal/sidebar, which
// only clears its "write still pending" flag once it knows tmux actually
// received it). Added alongside SetOption instead of changing its signature
// because SetOption still has other callers relying on it having no return
// value.
func SetOptionResult(key, value string) bool {
	_, ok := RunTmux("set", "-g", key, value)
	return ok
}

// SetPaneOption writes a pane-level option. Errors are swallowed: the pane
// may have just closed.
func SetPaneOption(pane, key, value string) {
	RunTmux("set", "-t", pane, "-p", key, value)
}

// UnsetPaneOption removes a pane-level option.
func UnsetPaneOption(pane, key string) {
	RunTmux("set", "-t", pane, "-p", "-u", key)
}

// GetPaneOptionValue reads a pane-level option, empty when absent.
func GetPaneOptionValue(pane, key string) string {
	out, ok := RunTmux("show", "-t", pane, "-pv", key)
	if !ok {
		return ""
	}
	return strings.TrimSpace(out)
}
