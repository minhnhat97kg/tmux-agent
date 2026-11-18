package tmux

// Agent labels written by the hook into PaneAgent. This is a contract, do not change.
const (
	AgentClaude   AgentType = "claude"
	AgentCodex    AgentType = "codex"
	AgentOpenCode AgentType = "opencode"
	AgentUnknown  AgentType = "unknown"
)

// otherAgents are agent labels accepted besides the three above: agents
// with a hook adapter but no special-casing in the query layer (pi,
// antigravity), and agents only the screen scraper detects (internal/scrape
// — keep in sync with its manifest list).
var otherAgents = map[string]bool{
	"pi": true, "antigravity": true,
	"copilot": true, "gemini": true, "cursor-agent": true, "aider": true, "amp": true,
}

// AgentType is the kind of AI agent running in a pane.
//
// Go has no enums that carry data, so we use a named string type: its value
// is exactly the label stored in tmux, making conversion in both directions
// trivial.
type AgentType string

// ParseAgentType reads an agent label. ok = false for an unknown or empty
// label, so the caller can skip panes that aren't running an agent.
func ParseAgentType(label string) (AgentType, bool) {
	switch AgentType(label) {
	case AgentClaude, AgentCodex, AgentOpenCode:
		return AgentType(label), true
	}
	if otherAgents[label] {
		return AgentType(label), true
	}
	return AgentUnknown, false
}

// PaneStatus is a pane's high-level status.
type PaneStatus string

const (
	StatusRunning    PaneStatus = "running"
	StatusBackground PaneStatus = "background"
	StatusWaiting    PaneStatus = "waiting"
	StatusIdle       PaneStatus = "idle"
	StatusError      PaneStatus = "error"
	StatusUnknown    PaneStatus = "unknown"
)

// ParsePaneStatus reads a status label. An unknown label becomes
// StatusUnknown rather than an error — the sidebar still needs to be able to
// render that pane.
func ParsePaneStatus(label string) PaneStatus {
	switch label {
	case "running":
		return StatusRunning
	case "background":
		return StatusBackground
	case "waiting", "notification":
		// "notification" is a legacy hook label, still present in tmux for
		// users on an old running session. Treated as waiting.
		return StatusWaiting
	case "idle":
		return StatusIdle
	case "error":
		return StatusError
	}
	return StatusUnknown
}

// Icon returns the single character representing a status.
func (s PaneStatus) Icon() string {
	switch s {
	case StatusRunning:
		return "●"
	case StatusBackground:
		return "◎"
	case StatusWaiting:
		return "◐"
	case StatusIdle:
		return "○"
	case StatusError:
		return "✕"
	}
	return "·"
}

// IsActive is true when the agent (or its background shell) is still doing
// work the user expects to see updates and elapsed time for.
func (s PaneStatus) IsActive() bool {
	return s == StatusRunning || s == StatusBackground || s == StatusWaiting
}

// PermissionMode is the permission mode an agent is using.
type PermissionMode string

const (
	ModeDefault           PermissionMode = ""
	ModePlan              PermissionMode = "plan"
	ModeAcceptEdits       PermissionMode = "acceptEdits"
	ModeAuto              PermissionMode = "auto"
	ModeDontAsk           PermissionMode = "dontAsk"
	ModeBypassPermissions PermissionMode = "bypassPermissions"
	ModeDefer             PermissionMode = "defer"
)

// ParsePermissionMode reads a permission-mode label; an unknown label falls
// back to ModeDefault.
func ParsePermissionMode(label string) PermissionMode {
	switch PermissionMode(label) {
	case ModePlan, ModeAcceptEdits, ModeAuto, ModeDontAsk, ModeBypassPermissions, ModeDefer:
		return PermissionMode(label)
	}
	return ModeDefault
}

// Badge returns the short label shown on the row. Empty means show nothing.
func (m PermissionMode) Badge() string {
	switch m {
	case ModePlan:
		return "plan"
	case ModeAcceptEdits:
		return "edit"
	case ModeAuto:
		return "auto"
	case ModeDontAsk:
		return "dontAsk"
	case ModeBypassPermissions:
		return "!"
	case ModeDefer:
		return "defer"
	}
	return ""
}

// WorktreeMetadata describes the git worktree attached to a pane, if any.
type WorktreeMetadata struct {
	Name   string
	Branch string
}

// PaneInfo is everything the sidebar knows about a pane running an agent.
type PaneInfo struct {
	PaneID           string
	PaneActive       bool
	Status           PaneStatus
	Attention        bool
	Agent            AgentType
	Path             string
	CurrentCommand   string
	Prompt           string
	PromptIsResponse bool
	// StartedAt is epoch seconds; 0 means not set yet.
	StartedAt      int64
	WaitReason     string
	PermissionMode PermissionMode
	Subagents      []string
	// PanePID is the pane's root process pid; 0 means it couldn't be read.
	PanePID   int
	Worktree  WorktreeMetadata
	SessionID string
	// SessionName is left empty at the query layer; the state layer fills it in from SessionInfo.
	SessionName string
	// SidebarSpawned is true when the window containing the pane was created by the sidebar's spawn flow.
	SidebarSpawned bool
	// BgShellCmd is the most recent background Bash command; empty means none.
	BgShellCmd string
	// Stuck is the minutes without activity while running (watchdog), "" if
	// fine; Conflict is "<file> with <agent>" when another agent of the same
	// project edits the same file.
	Stuck    string
	Conflict string
	// Name is the user/agent-given label (@pane_name, `tmux-agent name`);
	// empty when unnamed.
	Name string
	// Unseen is true when the agent finished while the user wasn't looking
	// (@pane_unseen) — herdr's "done" state.
	Unseen bool
}

// WindowInfo is a tmux window together with the agent panes inside it.
type WindowInfo struct {
	WindowID     string
	WindowIndex  string // tmux window index as shown in the status bar
	WindowName   string
	WindowActive bool
	AutoRename   bool
	Panes        []PaneInfo
}

// SessionInfo is a tmux session together with the windows that have agents.
type SessionInfo struct {
	SessionName string
	// Attached is true when at least one tmux client is attached to this
	// session (#{session_attached} > 0). window_active/pane_active within a
	// session is state tmux remembers for EVERY session regardless of
	// whether anyone is viewing it — Attached is the missing piece needed to
	// know which session the user is ACTUALLY looking at (group.go uses it
	// to compute RepoGroup.HasFocus).
	Attached bool
	Windows  []WindowInfo
}
