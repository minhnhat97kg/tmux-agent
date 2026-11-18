package worktree

// Default worktree configuration values — a CONTRACT with users running the
// upstream (Rust) build. Changing a value here changes observable behavior,
// which must not happen when porting to Go. See report p5-task1-report.md
// to cross-check against the source lines.
const (
	// DefaultBranchPrefix — branch name prefix when the tmux option
	// @agent-sidebar-branch-prefix is empty or unset.
	DefaultBranchPrefix = "agent/"
	// DefaultWorktreeDir — directory (repo-relative) holding worktrees when
	// the tmux option @agent-sidebar-worktree-dir is empty or unset.
	DefaultWorktreeDir = ".worktrees"
	// DefaultAgent — agent used for spawn when the tmux option
	// @agent-sidebar-default-agent is empty or unset.
	DefaultAgent = "claude"
	// DefaultMode — default mode for the spawn command (agent_command uses
	// it to derive the agent's command-line flag).
	DefaultMode = "default"
)

// ClaudeModes, CodexModes, OpenCodeModes — the list of valid modes per
// agent, used to derive the command-line flag in AgentCommand. Order and
// values are identical to the reference implementation (Rust), forming a
// contract with the mode-selection popup.
var (
	ClaudeModes   = []string{"default", "plan", "acceptEdits", "dontAsk", "bypassPermissions"}
	CodexModes    = []string{"default", "auto", "bypassPermissions"}
	OpenCodeModes = []string{"default"}
)

// Agents — the list of agents the sidebar supports, used for the
// agent-selection popup.
var Agents = []string{"claude", "codex", "opencode"}

// ModesFor returns the list of valid modes for the agent. An unknown agent
// (including empty) falls back to ClaudeModes — the same behavior as the
// reference implementation, so the popup never receives an empty list.
func ModesFor(agent string) []string {
	switch agent {
	case "codex":
		return CodexModes
	case "opencode":
		return OpenCodeModes
	default:
		return ClaudeModes
	}
}

// AgentCommand assembles the shell command that runs the agent in a new
// pane, from agent + mode. An unrecognized combination falls back to running
// the bare agent, no flags — a stale mode left over from a previous build
// (an un-cleaned tmux option) must never reach the agent and make it exit
// with an argument error right when it opens.
func AgentCommand(agent, mode string) string {
	switch agent {
	case "claude":
		switch mode {
		case "", "default":
			return "claude"
		case "plan", "acceptEdits", "dontAsk", "bypassPermissions":
			return "claude --permission-mode " + mode
		default:
			return "claude"
		}
	case "codex":
		switch mode {
		case "auto":
			return "codex --full-auto"
		case "bypassPermissions":
			return "codex --dangerously-bypass-approvals-and-sandbox"
		default:
			return "codex"
		}
	case "opencode":
		return "opencode"
	default:
		return agent
	}
}

// RemoveMode — the cleanup level when the user presses the delete key on a
// pane created by spawn.
type RemoveMode int

const (
	// RemoveWindowOnly only kills the window; the worktree and git branch
	// are left untouched.
	RemoveWindowOnly RemoveMode = iota
	// RemoveWindowAndWorktree kills the window AND runs git worktree remove
	// --force (and deletes the branch — see flow.go, task 2).
	RemoveWindowAndWorktree
)
