package hook

import (
	"strconv"

	"tmux-agent/internal/clock"
	"tmux-agent/internal/event"
	"tmux-agent/internal/tmux"
)

// Transition moves a pane from status old to next for an integration that
// only knows coarse states (the screen scraper, `tmux-agent set-status`
// from a harness plugin) rather than full hook events. It writes the status
// and does what the full hook handlers do around it: run start time and
// notification run id on running, "done, not yet seen" + a desktop
// notification when a run finishes, a notification when it starts waiting.
// No-op when the status doesn't change.
func Transition(pane, agent string, old, next tmux.PaneStatus) {
	if next == old {
		return
	}
	ApplyPaneStatus(pane, string(next))
	if old == tmux.StatusWaiting { // prompt answered: same cleanup as activityLogStatusTransition
		tmux.UnsetPaneOption(pane, tmux.PaneAttention)
		tmux.UnsetPaneOption(pane, tmux.PaneWaitReason)
	}
	switch {
	case next == tmux.StatusRunning && old != tmux.StatusWaiting:
		checkpointTurn(pane, "") // late by up to a scraper tick, best effort
		tmux.SetPaneOption(pane, tmux.PaneStartedAt, strconv.FormatInt(clock.NowUnix(), 10))
		SetNotificationRunID(pane)
	case next == tmux.StatusWaiting:
		HandleNotification(pane, event.Event{Kind: event.Notification, Agent: agent, WaitReason: "permission_prompt"}, LoadSettings())
	case next == tmux.StatusIdle:
		ClearRunState(pane)
		if old == tmux.StatusRunning {
			MarkUnseenIfUnviewed(pane)
			HandleNotification(pane, event.Event{Kind: event.Stop, Agent: agent}, LoadSettings())
		}
	}
}
