package worktree

import (
	"fmt"
	"io"
	"os"
	"strings"

	"tmux-agent/internal/tmux"
)

// Dispatch wires the "spawn" CLI command up to Spawn: reads the current
// pane/session from tmux via the TMUX_PANE env var, derives the git repo
// containing the pane, then calls Spawn. This is a user-initiated action —
// every error is printed to stderr clearly, never silently swallowed.
func Dispatch(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: tmux-agent spawn <task name>")
		return 2
	}

	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		fmt.Fprintln(stderr, "error: TMUX_PANE is not set; spawn must run inside tmux")
		return 1
	}

	cwd := tmux.DisplayMessage(pane, "#{pane_current_path}")
	if cwd == "" {
		fmt.Fprintln(stderr, "error: could not determine the current pane's path")
		return 1
	}

	repoRoot, ok := repoRootOf(cwd)
	if !ok {
		fmt.Fprintf(stderr, "error: %s is not inside a git repository\n", cwd)
		return 1
	}

	session, ok := tmux.PaneSessionName(pane)
	if !ok {
		fmt.Fprintln(stderr, "error: could not determine the current tmux session")
		return 1
	}

	agent := globalOption(tmux.AgentOption)
	if agent == "" {
		agent = DefaultAgent
	}

	req := &SpawnRequest{
		RepoRoot: repoRoot,
		TaskName: strings.Join(args, " "),
		Session:  session,
		Agent:    agent,
		Mode:     DefaultMode,
	}

	branch, err := Spawn(req)
	if err != nil {
		fmt.Fprintf(stderr, "spawn failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "spawned %s\n", branch)
	return 0
}
