// Package hook processes events already parsed from internal/event, producing
// side effects on tmux (pane options) and on disk (activity log). This is the
// ONLY package allowed to import both internal/activity and internal/tmux —
// activity must not import tmux (part 1's tmux already imports activity to
// delete logs when cleaning up dead panes; importing the other way would form
// a cycle and Go would refuse to compile).
package hook

import (
	"tmux-agent/internal/activity"
	"tmux-agent/internal/tmux"
	"tmux-agent/internal/toollabel"
)

// HandleActivityLog processes the ActivityLog event: generates a label for
// the tool that just ran, detects a background shell run via Bash and writes
// @pane_bg_cmd, and writes one line to the pane's activity log.
//
// NOT COMPLETE relative to the Rust reference (handle_activity_log in
// src/cli/hook/activity.rs): that version also reads @pane_status/
// @pane_started_at to move the pane from "waiting"/"background" back to
// "running" on a new tool-use, and updates @pane_permission_mode when the
// tool is EnterPlanMode/ExitPlanMode. Those two parts are handled by
// activityLogStatusTransition (handlers.go), called from route() right next
// to HandleActivityLog — see that function's own doc comment. This function
// only does the label + background shell + log-writing part.
func HandleActivityLog(pane, toolName string, toolInput, toolResponse map[string]any) {
	label := toollabel.Label(toolName, toolInput, toolResponse)

	if isBackgroundBash(toolName, toolInput) {
		stored := label
		if stored == "" {
			stored = tmux.BGCmdPlaceholder
		}
		tmux.SetPaneOption(pane, tmux.PaneBgCmd, activity.SanitizeLabel(stored))
		cmd, _ := toolInput["command"].(string)
		rememberBackground(pane, cmd)
	}

	activity.WriteEntry(pane, toolName, label)
}

// isBackgroundBash reports true ONLY when the tool is Bash and tool_input
// marks a background run with either run_in_background (Claude/Codex) or
// runInBackground (OpenCode) — two agents naming the same flag differently.
// Any tool other than Bash is always false, even if its payload happens to
// have a same-named key — only Bash has the notion of "background run" at
// the activity log layer.
func isBackgroundBash(toolName string, toolInput map[string]any) bool {
	if toolName != toollabel.Bash {
		return false
	}
	return boolField(toolInput, "run_in_background") || boolField(toolInput, "runInBackground")
}

func boolField(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	v, _ := m[key].(bool)
	return v
}
