package event

// WorktreeInfo is git worktree information taken from the hook payload.
// Only present when the agent is running inside a worktree; nil otherwise.
type WorktreeInfo struct {
	Name            string
	Path            string
	Branch          string
	OriginalRepoDir string
}

// Event is an event that has already been parsed out by the adapter.
//
// A handler NEVER reads raw JSON and NEVER checks the agent name — every
// difference between agents has already been flattened by the adapter
// before it reaches here.
//
// Go has no sum type, so this is a flat struct instead of an enum carrying
// data. A field that doesn't belong to the current event's kind carries its
// zero value. The comment to the right of each field states which kind(s)
// it belongs to.
//
// Only 7 out of 16 Kinds carry part of the "session context" group below:
// SessionStart, UserPromptSubmit, Notification, Stop, StopFailure,
// PermissionDenied, CwdChanged. The other 8 Kinds — SessionEnd, ActivityLog,
// TaskCreated, TaskCompleted, TeammateIdle, WorktreeCreate, WorktreeRemove —
// carry NONE of that group's fields; this is deliberate, not an oversight
// (see the individual field comments below). SubagentStart and SubagentStop
// reuse AgentID but with different semantics — the subagent's ID, not the
// session's (see the comment on the AgentID field below). As for
// WorktreeCreate and WorktreeRemove specifically: NONE of the three existing
// adapters has a Parse() branch that produces these two Kinds — they exist
// in Kind/All/externalNames but no hook has actually produced a real Event
// for them yet. Do not write a handler that assumes they already work.
type Event struct {
	Kind Kind

	// Session context — but NOT uniform across Kinds nor across the three
	// adapters. Read each field carefully before a phase-2b handler uses it
	// to write a tmux pane option:
	//
	//   - Agent: present on SessionStart, UserPromptSubmit, Notification,
	//     Stop, StopFailure, PermissionDenied (whichever adapter produces
	//     that event kind sets it to "claude"/"codex"/"opencode"). ABSENT
	//     (empty) on CwdChanged — ClaudeAdapter is the only adapter that
	//     produces this Kind and DELIBERATELY does not set Agent because
	//     Claude Code's real payload for the CwdChanged trigger does not
	//     carry that field (see claude.go, case "cwd-changed"). Also ABSENT
	//     on SessionEnd — Claude is the only adapter that produces
	//     SessionEnd and it only sets EndReason. A handler must NOT
	//     interpret Agent == "" on these two Kinds as "unknown agent" and
	//     overwrite an already-correct @pane_agent value.
	//   - PermissionMode: present (though possibly empty) on the same 6
	//     Kinds as Agent, except CwdChanged (same reason as Agent above). Of
	//     those 6 Kinds, ONLY ClaudeAdapter reads the real permission mode
	//     value from the payload; CodexAdapter always leaves it empty
	//     (Codex's permission mode is inferred at a different layer, by
	//     reading process argv — see CodexAdapter's doc comment) and
	//     OpenCodeAdapter also always leaves it empty (OpenCode has no
	//     concept of this at the JS plugin layer). An empty value here could
	//     mean either "Claude says the agent is in default mode" OR "this
	//     adapter doesn't support the field" — two different reasons that
	//     cannot be told apart from the Event alone.
	//   - Cwd: present on the same 6 Kinds, all three adapters set it from
	//     the real payload.
	//   - Worktree: ONLY ClaudeAdapter sets it (it can still be nil if the
	//     session isn't running inside a git worktree). Codex and OpenCode
	//     don't detect worktrees at the hook layer, so this field is always
	//     nil for those two agents.
	//   - AgentID: ONLY ClaudeAdapter sets it for the 6 Kinds in this group;
	//     it is also reused by ClaudeAdapter for SubagentStart/SubagentStop
	//     (see below, where the semantics there are the subagent's ID, not
	//     the session's). Codex/OpenCode always leave it empty.
	//   - SessionID: present on these 6 Kinds (SessionStart,
	//     UserPromptSubmit, Notification, Stop, StopFailure,
	//     PermissionDenied); whichever adapter produces that event kind sets
	//     it.
	Agent          string
	Cwd            string
	PermissionMode string
	Worktree       *WorktreeInfo
	AgentID        string
	SessionID      string

	Source string // SessionStart — the session's start source; all three adapters set it.
	// EndReason: ONLY ClaudeAdapter produces the SessionEnd Kind — Codex has
	// no reliable exit hook, neither does OpenCode (Bun doesn't guarantee
	// the exit handler finishes running when the process is killed/crashes).
	// A SessionEnd event does not carry Agent (see the session-context group
	// above).
	EndReason string
	Prompt    string // UserPromptSubmit — all three adapters set it.
	// WaitReason: Notification — set by ClaudeAdapter and OpenCodeAdapter;
	// CodexAdapter does not support the Notification Kind (no parse branch).
	WaitReason string
	// MetaOnly is true when the event only refreshes metadata and does NOT
	// change status or attention. Used for idle_prompt-style events: it
	// carries information but should not make the sidebar row blink.
	// ClaudeAdapter computes this value (true when
	// notification_type == "idle_prompt"); OpenCodeAdapter ALWAYS sets it to
	// false — it does not yet distinguish which "waiting" kind is
	// metadata-only.
	MetaOnly bool // Notification
	// LastMessage: on Stop, all three adapters set it (from each agent's
	// corresponding field name). On SubagentStop, ONLY ClaudeAdapter sets
	// it — Codex/OpenCode do not support the SubagentStop Kind.
	LastMessage string // Stop, SubagentStop
	// Response is the content the Stop handler prints to stdout to reply to
	// the hook. This is the ONLY PLACE in phase 2 that prints to stdout —
	// nowhere else prints. It has two different meanings depending on the
	// agent:
	//
	//   - The Codex hook engine reads stdout back to know whether to
	//     continue the turn; CodexAdapter always sets `{"continue":true}`
	//     here.
	//   - Claude and OpenCode don't control the turn from the hook's
	//     stdout, so they set nil.
	//
	// A handler must NOT interpret Response as "the agent's reply" for
	// Codex — it is CONTROL JSON for the hook engine, not displayable data.
	Response *string // Stop
	// Error: StopFailure — set by ClaudeAdapter and OpenCodeAdapter;
	// CodexAdapter does not support the StopFailure Kind.
	Error string

	// AgentType, AgentID (reused, see above): SubagentStart, SubagentStop —
	// ONLY ClaudeAdapter produces these two Kinds. They don't carry
	// Agent/Cwd/SessionID/Worktree/PermissionMode — these two Kinds do not
	// belong to the session-context group.
	AgentType      string // SubagentStart, SubagentStop
	TranscriptPath string // SubagentStop — set only by ClaudeAdapter.

	// ToolName, ToolInput, ToolResponse: ActivityLog — all three adapters
	// set them. Does not carry Agent/Cwd/SessionID (ActivityLog does not
	// belong to the session-context group) — to know which agent produced
	// this log entry, look it up by pane/agent ID at the calling layer, not
	// on the Event.
	ToolName     string         // ActivityLog
	ToolInput    map[string]any // ActivityLog
	ToolResponse map[string]any // ActivityLog

	// TaskID, TaskSubject: TaskCreated, TaskCompleted — ONLY ClaudeAdapter
	// produces these two Kinds.
	TaskID      string // TaskCreated, TaskCompleted
	TaskSubject string // TaskCreated, TaskCompleted

	// TeammateName, TeamName, IdleReason: TeammateIdle — ONLY ClaudeAdapter
	// produces this Kind.
	TeammateName string // TeammateIdle
	TeamName     string // TeammateIdle
	IdleReason   string // TeammateIdle

	// WorktreePath: WorktreeRemove — NO adapter actually sets this field in
	// practice: no agent currently has a Parse() branch that produces the
	// WorktreeCreate or WorktreeRemove Kind (see the comment at the top of
	// Event). The field exists ready for future work; a phase-2b handler
	// should not assume it already works at this phase.
	WorktreePath string // WorktreeRemove
}
