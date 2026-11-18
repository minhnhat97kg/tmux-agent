// Package event defines the internal event kinds that an agent's hooks
// produce.
//
// This package KNOWS NOTHING about tmux and KNOWS NOTHING about any specific
// agent's JSON. An adapter translates each agent's JSON into this package's
// types; a handler reads from here. This lets the three adapters be written
// independently, and a handler never has to check the agent's name.
package event

// Kind is an internal event kind.
type Kind string

const (
	SessionStart     Kind = "SessionStart"
	SessionEnd       Kind = "SessionEnd"
	UserPromptSubmit Kind = "UserPromptSubmit"
	Notification     Kind = "Notification"
	Stop             Kind = "Stop"
	StopFailure      Kind = "StopFailure"
	PermissionDenied Kind = "PermissionDenied"
	CwdChanged       Kind = "CwdChanged"
	SubagentStart    Kind = "SubagentStart"
	SubagentStop     Kind = "SubagentStop"
	ActivityLog      Kind = "ActivityLog"
	TaskCreated      Kind = "TaskCreated"
	TaskCompleted    Kind = "TaskCompleted"
	TeammateIdle     Kind = "TeammateIdle"
	WorktreeCreate   Kind = "WorktreeCreate"
	WorktreeRemove   Kind = "WorktreeRemove"
)

// All lists all 16 kinds in a stable order, used for iteration.
//
// The ACTUAL protection scope of the two tests that lock down All
// (kind_test.go): TestAllHasAll16Kinds only locks down len(All) == 16;
// TestAllMatchesExternalNames only locks down that All and externalNames
// agree with each other (nothing extra, nothing missing in either
// direction). Neither one — nor any combination of them — can detect a Kind
// constant that got left out of BOTH All and externalNames at the same
// time: Go cannot enumerate a type's constants at runtime, so there is no
// way for a test to know "how many Kinds currently exist in the package"
// other than by reading the source.
//
// The final review proved this empirically: adding a new Kind constant
// WITHOUT adding it to All, WITHOUT adding it to externalNames, and then
// adding a full Parse() branch for it in an adapter — the entire
// `go test ./...` STILL PASSES. This is a structural limitation, not a gap
// that can be patched by adding a test.
//
// So: whoever adds a new Kind MUST manually add it to both All and
// externalNames (and to the relevant adapter's Registrations()/Parse()) —
// don't rely on this test suite to sound the alarm if it's forgotten.
var All = []Kind{
	SessionStart, SessionEnd, UserPromptSubmit, Notification,
	Stop, StopFailure, PermissionDenied, CwdChanged,
	SubagentStart, SubagentStop, ActivityLog, TaskCreated,
	TaskCompleted, TeammateIdle, WorktreeCreate, WorktreeRemove,
}

// externalNames maps an event kind to the name passed on the command line:
//
//	tmux-agent hook <agent> <external-name>
//
// This is the CONTRACT with assets/hook.sh and each agent's hook
// configuration. Kebab-case, deliberately NOT matching the Go constant
// names, so don't auto-generate it from the constant names.
var externalNames = map[Kind]string{
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

// ExternalName returns the event name used on the command line. Empty if the
// kind is invalid.
func (k Kind) ExternalName() string {
	return externalNames[k]
}

// KindFromExternalName looks up a Kind from its command-line name in
// reverse. ok = false for an unknown name, so dispatch skips an unknown
// event instead of guessing.
func KindFromExternalName(name string) (Kind, bool) {
	if name == "" {
		return "", false
	}
	for kind, external := range externalNames {
		if external == name {
			return kind, true
		}
	}
	return "", false
}
