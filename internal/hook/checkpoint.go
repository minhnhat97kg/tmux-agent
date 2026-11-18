package hook

import (
	"tmux-agent/internal/checkpoint"
	"tmux-agent/internal/tmux"
)

// CheckpointsOption turns the per-turn snapshots off ("off").
const CheckpointsOption = "@agent_checkpoints"

// CheckpointKey is the checkpoint key of a pane (stable while it lives).
func CheckpointKey(pane string) string { return "pane-" + pane }

// checkpointTurn snapshots the pane's repo before a turn starts, for
// `tmux-agent undo`. Silent: no repo, or git failing, just means no undo.
func checkpointTurn(pane, cwd string) {
	if v, _ := tmux.GetOption(CheckpointsOption); v == "off" {
		return
	}
	if cwd == "" {
		cwd = tmux.DisplayMessage(pane, "#{pane_current_path}")
	}
	if cwd != "" {
		checkpoint.Create(cwd, CheckpointKey(pane))
	}
}
