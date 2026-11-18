package main

import (
	"fmt"
	"io"
	"os"

	"tmux-agent/internal/config"
	"tmux-agent/internal/hook"
	"tmux-agent/internal/install"
	"tmux-agent/internal/roles"
	"tmux-agent/internal/scrape"
	"tmux-agent/internal/setup"
	"tmux-agent/internal/sidebar"
	"tmux-agent/internal/version"
	"tmux-agent/internal/watch"
	"tmux-agent/internal/worktree"
)

// Exit codes, following the sysexits convention:
//
//	0   success
//	1   runtime failure (tmux error, pane gone, agent waiting)
//	2   argument error
//	124 wait timed out (timeout(1) convention)
//	70  EX_SOFTWARE — known command but not yet implemented
//	127 command does not exist
const (
	exitOK             = 0
	exitFailure        = 1
	exitUsage          = 2
	exitNotImplemented = 70
	exitUnknownCommand = 127
)

// notImplemented lists commands that will be added later. Remove entries as
// they get implemented; TestStubCommandsExit70 keeps this list from being
// forgotten.
var notImplemented = map[string]string{
	"capture": "phase 5",
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		// No subcommand means run the sidebar TUI. Print nothing to
		// stdout/stderr on this path — sidebar.Run() owns the entire screen.
		return sidebar.Run()
	}

	cmd := args[0]
	rest := args[1:]
	switch cmd {
	case "version", "--version":
		fmt.Fprintln(stdout, version.Version)
		return exitOK
	case "set-status":
		return cmdSetStatus(rest)
	case "toggle":
		return sidebar.Toggle(rest)
	case "toggle-all":
		return sidebar.ToggleAll(rest)
	case "auto-close":
		return sidebar.AutoClose(rest)
	case "hook":
		// Pure wiring: hook.Dispatch (the "handlers" task) reads TMUX_PANE
		// itself, reads/parses the JSON on stdin via the adapter itself, and
		// decides on its own whether to print Response to stdout (only Stop
		// prints — see the doc comment on event.Event.Response). No router
		// logic lives here.
		return hook.Dispatch(rest, os.Stdin, stdout)
	case "setup":
		// Pure wiring: setup.Dispatch generates config from the adapter
		// registry itself and reports errors/warnings to stderr itself —
		// this is a user-initiated action, not a hook, so it does not follow
		// the hook's silent soft-degradation philosophy.
		return setup.Dispatch(rest, stdout, stderr)
	case "list":
		return cmdList(rest, stdout)
	case "new":
		return cmdNew(rest, stdout, stderr)
	case "name":
		return cmdName(rest, stderr)
	case "ctx":
		return cmdCtx(rest, stdout, stderr)
	case "watch":
		return watch.Run(rest)
	case "expand":
		return sidebar.ExpandToggle(rest, stderr)
	case "digest":
		return sidebar.Digest(rest, stdout, stderr)
	case "events":
		return watch.Stream(rest, stdout)
	case "read":
		return cmdRead(rest, stdout, stderr)
	case "send":
		return cmdSend(rest, stdout, stderr)
	case "config":
		return config.Dispatch(rest, stdout, stderr)
	case "roles":
		if len(rest) > 0 { // roles <name>: print its prompt
			text, ok := roles.Get(rest[0])
			if !ok {
				fmt.Fprintf(stderr, "roles: no role %q (have: %v)\n", rest[0], roles.Names())
				return exitFailure
			}
			fmt.Fprintln(stdout, text)
			return exitOK
		}
		for _, n := range roles.Names() {
			fmt.Fprintln(stdout, n)
		}
		return exitOK
	case "checkpoints":
		return cmdCheckpoints(rest, stdout, stderr)
	case "undo":
		return cmdUndo(rest, stdout, stderr)
	case "ask":
		return cmdAsk(rest, stdout, stderr)
	case "queue":
		return cmdQueue(rest, stdout, stderr)
	case "tokens":
		return cmdTokens(stdout)
	case "save":
		return cmdSave(stdout, stderr)
	case "restore":
		return cmdRestore(rest, stdout, stderr)
	case "fanout":
		return cmdFanout(rest, stdout, stderr)
	case "compare":
		return cmdCompare(rest, stdout, stderr)
	case "team":
		return cmdTeam(rest, stdout, stderr)
	case "plan":
		return cmdPlan(rest, stdout, stderr)
	case "diff":
		return cmdDiff(rest, stdout, stderr)
	case "merge":
		return cmdMerge(rest, stdout, stderr)
	case "discard":
		return cmdDiscard(rest, stdout, stderr)
	case "answer":
		return cmdAnswer(rest, stdout, stderr)
	case "next":
		return cmdNext(stdout)
	case "kill":
		return cmdKill(rest, stdout, stderr)
	case "reload":
		return cmdReload(stdout)
	case "wait":
		return cmdWait(rest, stdout, stderr)
	case "install":
		return install.Dispatch(false, rest, stdout, stderr)
	case "uninstall":
		return install.Dispatch(true, rest, stdout, stderr)
	case "scrape":
		return scrape.Run()
	case "spawn":
		// Pure wiring: worktree.Dispatch creates the git worktree + tmux
		// window + runs the agent itself, and reports clear errors to
		// stderr itself (a user-initiated action, not a silent
		// soft-degradation).
		return worktree.Dispatch(rest, stdout, stderr)
	}

	if phase, ok := notImplemented[cmd]; ok {
		fmt.Fprintf(stderr, "tmux-agent: %s not implemented (%s)\n", cmd, phase)
		return exitNotImplemented
	}

	fmt.Fprintf(stderr, "tmux-agent: unknown command %q\n", cmd)
	return exitUnknownCommand
}
