package tmux

import "testing"

func TestParsePaneStatus(t *testing.T) {
	cases := map[string]PaneStatus{
		"running":    StatusRunning,
		"background": StatusBackground,
		"waiting":    StatusWaiting,
		// The old hook wrote "notification"; it must be treated as waiting.
		"notification": StatusWaiting,
		"idle":         StatusIdle,
		"error":        StatusError,
		"":             StatusUnknown,
		"junk-label":   StatusUnknown,
	}
	for label, want := range cases {
		if got := ParsePaneStatus(label); got != want {
			t.Errorf("ParsePaneStatus(%q) = %q, want %q", label, got, want)
		}
	}
}

func TestPaneStatusIsActive(t *testing.T) {
	active := []PaneStatus{StatusRunning, StatusBackground, StatusWaiting}
	for _, s := range active {
		if !s.IsActive() {
			t.Errorf("%q must be active", s)
		}
	}
	for _, s := range []PaneStatus{StatusIdle, StatusError, StatusUnknown} {
		if s.IsActive() {
			t.Errorf("%q must not be active", s)
		}
	}
}

func TestPaneStatusIcon(t *testing.T) {
	cases := map[PaneStatus]string{
		StatusRunning:    "●",
		StatusBackground: "◎",
		StatusWaiting:    "◐",
		StatusIdle:       "○",
		StatusError:      "✕",
		StatusUnknown:    "·",
	}
	for status, want := range cases {
		if got := status.Icon(); got != want {
			t.Errorf("%q.Icon() = %q, want %q", status, got, want)
		}
	}
}

func TestParseAgentType(t *testing.T) {
	for label, want := range map[string]AgentType{
		"claude":   AgentClaude,
		"codex":    AgentCodex,
		"opencode": AgentOpenCode,
		"pi":       AgentType("pi"),
		"copilot":  AgentType("copilot"),
	} {
		got, ok := ParseAgentType(label)
		if !ok || got != want {
			t.Errorf("ParseAgentType(%q) = (%q, %v)", label, got, ok)
		}
	}
	// A pane that isn't an agent must be rejected, so the caller skips the line.
	for _, label := range []string{"", "vim", "unknown"} {
		if _, ok := ParseAgentType(label); ok {
			t.Errorf("ParseAgentType(%q) must return ok = false", label)
		}
	}
}

func TestParsePermissionModeAndBadge(t *testing.T) {
	cases := map[string]struct {
		mode  PermissionMode
		badge string
	}{
		"":                  {ModeDefault, ""},
		"plan":              {ModePlan, "plan"},
		"acceptEdits":       {ModeAcceptEdits, "edit"},
		"auto":              {ModeAuto, "auto"},
		"dontAsk":           {ModeDontAsk, "dontAsk"},
		"bypassPermissions": {ModeBypassPermissions, "!"},
		"defer":             {ModeDefer, "defer"},
		"junk-label":        {ModeDefault, ""},
	}
	for label, want := range cases {
		got := ParsePermissionMode(label)
		if got != want.mode {
			t.Errorf("ParsePermissionMode(%q) = %q, want %q", label, got, want.mode)
		}
		if badge := got.Badge(); badge != want.badge {
			t.Errorf("%q.Badge() = %q, want %q", got, badge, want.badge)
		}
	}
}
