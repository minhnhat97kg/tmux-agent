package event

import "testing"

// ExternalName is the name passed on the command line
// `tmux-agent hook <agent> <name>`, i.e. the contract with assets/hook.sh
// and each agent's hook configuration. It is kebab-case and does NOT match
// the Go constant name — lock each one down individually.
func TestExternalNameIsContract(t *testing.T) {
	cases := map[Kind]string{
		SessionStart:     "session-start",
		SessionEnd:       "session-end",
		UserPromptSubmit: "user-prompt-submit",
		Notification:     "notification",
		Stop:             "stop",
		StopFailure:      "stop-failure",
		PermissionDenied: "permission-denied",
		CwdChanged:       "cwd-changed",
		SubagentStart:    "subagent-start",
		SubagentStop:     "subagent-stop",
		ActivityLog:      "activity-log",
		TaskCreated:      "task-created",
		TaskCompleted:    "task-completed",
		TeammateIdle:     "teammate-idle",
		WorktreeCreate:   "worktree-create",
		WorktreeRemove:   "worktree-remove",
	}
	if len(cases) != 16 {
		t.Fatalf("table has %d entries, must have all 16 kinds", len(cases))
	}
	for kind, want := range cases {
		if got := kind.ExternalName(); got != want {
			t.Errorf("%q.ExternalName() = %q, contract says %q", kind, got, want)
		}
	}
}

// All must contain all 16 kinds. Adding a Kind constant without adding it to
// All and externalNames leaves len(All) still 16, TestAllDayDuVoiExternalNames
// still green, and the new constant unused. Go cannot enumerate constants at
// runtime — a structural limitation, not something a test can patch. This
// test checks the structural scheme, not the scaffolding.
func TestAllHasAll16Kinds(t *testing.T) {
	if len(All) != 16 {
		t.Fatalf("All has %d elements, want 16", len(All))
	}
	seen := map[Kind]bool{}
	for _, k := range All {
		if seen[k] {
			t.Errorf("All has %q twice", k)
		}
		seen[k] = true
		if k.ExternalName() == "" {
			t.Errorf("%q has no ExternalName", k)
		}
	}
}

func TestExternalNameNoDuplicates(t *testing.T) {
	seen := map[string]Kind{}
	for _, k := range All {
		name := k.ExternalName()
		if other, dup := seen[name]; dup {
			t.Errorf("%q and %q share the ExternalName %q", k, other, name)
		}
		seen[name] = k
	}
}

func TestKindFromExternalNameRoundTrip(t *testing.T) {
	for _, k := range All {
		got, ok := KindFromExternalName(k.ExternalName())
		if !ok || got != k {
			t.Errorf("KindFromExternalName(%q) = (%q, %v), want (%q, true)",
				k.ExternalName(), got, ok, k)
		}
	}
	// An unknown name must be rejected, without panicking, without guessing.
	for _, name := range []string{"", "not-real", "SessionStart"} {
		if _, ok := KindFromExternalName(name); ok {
			t.Errorf("KindFromExternalName(%q) must return ok = false", name)
		}
	}
}

// TestAllDayDuVoiExternalNames checks that All and externalNames are
// symmetric: every key in externalNames must be in All, and vice versa. If
// someone adds a new kind and forgets one side, this test fails.
func TestAllMatchesExternalNames(t *testing.T) {
	// Check that every key in externalNames is in All
	allMap := map[Kind]bool{}
	for _, k := range All {
		allMap[k] = true
	}
	for kind := range externalNames {
		if !allMap[kind] {
			t.Errorf("externalNames has key %q but All does not contain it", kind)
		}
	}

	// Check that every element of All is in externalNames
	for _, kind := range All {
		if _, ok := externalNames[kind]; !ok {
			t.Errorf("All contains %q but externalNames does not have it", kind)
		}
	}
}
