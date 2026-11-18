// Package hook handles agent hook events: it decides pane status, writes
// status to tmux, and writes the activity log.
//
// It receives an event.Event already parsed by the adapter, so it never
// reads raw JSON and never checks the agent name.
package hook

import "tmux-agent/internal/tmux"

// Pane status priority order, highest to lowest:
//
//	running > permission > background > waiting > idle
//
// Handlers look here instead of re-implementing it, so this ordering exists
// in exactly one place.

// permissionWaitReasons is the group of wait reasons that require the user
// to handle them by hand. A whitelist, not a prefix match — an unfamiliar
// label is not counted as part of the group.
var permissionWaitReasons = map[string]bool{
	"permission":         true,
	"permission_prompt":  true,
	"permission_denied":  true,
	"elicitation_dialog": true,
}

// IsPermissionWaitReason is true when the wait reason requires direct user
// action, and therefore must keep showing `waiting` even when a background
// shell is alive.
func IsPermissionWaitReason(waitReason string) bool {
	return permissionWaitReasons[waitReason]
}

// ResolveStopStatus returns the status a Stop event must fall into.
//
// The agent has stopped talking, but if it left a background shell running
// the pane is still working — showing `idle` at that point would be lying
// to the user.
func ResolveStopStatus(bgShellLive bool) tmux.PaneStatus {
	if bgShellLive {
		return tmux.StatusBackground
	}
	return tmux.StatusIdle
}

// ResolveNotificationStatus returns the status a Notification event must
// fall into.
//
// A background shell downgrades `waiting` to `background` for "soft" wait
// reasons. But a reason in the permission group must NOT be downgraded: the
// user still has to answer the prompt by hand, and a running background
// shell doesn't make that any less urgent.
func ResolveNotificationStatus(waitReason string, bgShellLive bool) tmux.PaneStatus {
	if bgShellLive && !IsPermissionWaitReason(waitReason) {
		return tmux.StatusBackground
	}
	return tmux.StatusWaiting
}
