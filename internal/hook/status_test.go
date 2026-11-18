package hook

import (
	"testing"

	"tmux-agent/internal/tmux"
)

// The permission group has exactly four labels. A whitelist, not a prefix.
func TestIsPermissionWaitReasonWhitelistTable(t *testing.T) {
	inList := []string{"permission", "permission_prompt", "permission_denied", "elicitation_dialog"}
	for _, r := range inList {
		if !IsPermissionWaitReason(r) {
			t.Errorf("IsPermissionWaitReason(%q) = false, want true", r)
		}
	}
	outOfList := []string{"auth_success", "rate_limit", "session_resumed", "teammate_idle:alice", "", "permission_"}
	for _, r := range outOfList {
		if IsPermissionWaitReason(r) {
			t.Errorf("IsPermissionWaitReason(%q) = true, want false", r)
		}
	}
}

func TestResolveStopStatus(t *testing.T) {
	if got := ResolveStopStatus(true); got != tmux.StatusBackground {
		t.Errorf("with background shell: = %q, want %q", got, tmux.StatusBackground)
	}
	if got := ResolveStopStatus(false); got != tmux.StatusIdle {
		t.Errorf("without background shell: = %q, want %q", got, tmux.StatusIdle)
	}
}

// Most important rule: the permission group beats a background shell.
func TestResolveNotificationStatusPermissionAlwaysBeatsShell(t *testing.T) {
	for _, r := range []string{"permission", "permission_prompt", "permission_denied", "elicitation_dialog"} {
		for _, bg := range []bool{true, false} {
			if got := ResolveNotificationStatus(r, bg); got != tmux.StatusWaiting {
				t.Errorf("ResolveNotificationStatus(%q, %v) = %q, want %q", r, bg, got, tmux.StatusWaiting)
			}
		}
	}
}

func TestResolveNotificationStatusSoftReasonDowngradesToBackground(t *testing.T) {
	for _, r := range []string{"auth_success", "rate_limit", ""} {
		if got := ResolveNotificationStatus(r, true); got != tmux.StatusBackground {
			t.Errorf("ResolveNotificationStatus(%q, true) = %q, want %q", r, got, tmux.StatusBackground)
		}
		if got := ResolveNotificationStatus(r, false); got != tmux.StatusWaiting {
			t.Errorf("ResolveNotificationStatus(%q, false) = %q, want %q", r, got, tmux.StatusWaiting)
		}
	}
}
