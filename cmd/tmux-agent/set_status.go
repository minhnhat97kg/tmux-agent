package main

import (
	"os"

	"tmux-agent/internal/hook"
	"tmux-agent/internal/tmux"
)

// cmdSetStatus implements the "set-status <status>" command: writes
// @pane_status for the current pane, read from the TMUX_PANE environment
// variable (always set when the command runs inside a tmux pane — e.g. from
// a key-binding or a script the user calls themselves).
//
// Ported from cmd_set_status in the reference Rust version (src/cli/mod.rs).
// Does not read stdin. Always returns 0 — even when arguments are missing or
// it's not running inside tmux — because this is a manually invoked command,
// not a hook: pressing the wrong key should not surface a meaningless error
// exit code.
//
// The status-writing logic (including the "clear" branch and the
// "running/idle ⇒ unset attention" rule) lives in hook.ApplyPaneStatus — the
// single source of truth, shared with the internal handlers of the hook
// package. It is not re-encoded here.
func cmdSetStatus(args []string) int {
	if len(args) == 0 {
		return exitOK
	}
	status := args[0]

	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		return exitOK
	}

	// `--agent NAME`: the call comes from a harness integration (e.g. the
	// Pi extension installed by `tmux-agent install pi`) — register the
	// agent and go through the full transition (start time, unseen,
	// notifications) like the scraper does.
	if len(args) >= 3 && args[1] == "--agent" && status != "clear" {
		agent := args[2]
		tmux.SetPaneOption(pane, tmux.PaneAgent, agent)
		tmux.UnsetPaneOption(pane, tmux.PaneAgentSrc)
		old := tmux.PaneStatus(tmux.GetPaneOptionValue(pane, tmux.PaneStatusOpt))
		hook.Transition(pane, agent, old, tmux.PaneStatus(status))
		return exitOK
	}

	hook.ApplyPaneStatus(pane, status)
	return exitOK
}
