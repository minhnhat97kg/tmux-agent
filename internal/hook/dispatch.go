package hook

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"tmux-agent/internal/tmux"

	"tmux-agent/internal/adapter"
	"tmux-agent/internal/event"
)

func argAt(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return ""
}

// readStdinJSON reads and decodes the hook's JSON payload from stdin. Returns
// nil when stdin is an interactive TTY (nothing was piped in — this check
// only applies when stdin is a *os.File, the real code path via os.Stdin;
// tests use strings.Reader so this branch never triggers), when reading
// fails/is empty, or when the JSON is malformed. All three error cases return
// nil instead of aborting Dispatch early — a nil payload is treated by
// adapter.Parse as "every field is its zero value", consistent with part 2a's
// "missing field → zero value" philosophy.
func readStdinJSON(r io.Reader) map[string]any {
	if f, ok := r.(*os.File); ok {
		if info, err := f.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
			return nil
		}
	}
	data, err := io.ReadAll(r)
	if err != nil || len(data) == 0 {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil
	}
	return payload
}

// Dispatch is the router for the `tmux-agent hook <agent> <event>` command.
// args is the argument slice after "hook"; stdin/stdout are passed in
// (instead of reading os.Stdin/writing os.Stdout directly) so tests don't
// need a real pipe.
//
// Every error path returns 0 silently — the hook runs inside the user's real
// agent session and must never break it with a non-zero exit code or a
// panic.
func Dispatch(args []string, stdin io.Reader, stdout io.Writer) int {
	agentName := argAt(args, 0)
	eventName := argAt(args, 1)
	if agentName == "" || eventName == "" {
		return 0
	}

	a, ok := adapter.Resolve(agentName)
	if !ok {
		return 0
	}

	paneID := os.Getenv("TMUX_PANE")
	if paneID == "" {
		// Some launchers (or an agent restarting itself) drop TMUX/TMUX_PANE
		// from the environment; the agent still runs inside a pane, so find
		// that pane from the process tree.
		paneID = paneFromAncestors(os.Getppid())
	}
	if paneID == "" {
		return 0
	}

	payload := readStdinJSON(stdin)
	evt, ok := a.Parse(eventName, payload)
	if !ok {
		return 0
	}

	route(paneID, agentName, evt)

	// The ONLY place in all of part 2 that prints to stdout: Codex attaches
	// `{"continue":true}` to Response so its hook engine can read it back via
	// stdout (see the event.Event.Response doc comment). Claude/OpenCode
	// always leave Response = nil so this branch never runs for them.
	if evt.Response != nil {
		fmt.Fprintln(stdout, *evt.Response)
	}
	return 0
}

// route maps Kind → handler. CwdChanged, ActivityLog, TaskCreated and
// WorktreeCreate have no dedicated On* function (see the reason at each
// case), so their logic lives right here instead of being indirected through
// an empty function that exists only for form's sake.
func route(paneID, agentName string, evt event.Event) {
	switch evt.Kind {
	case event.SessionStart:
		OnSessionStart(paneID, evt)
	case event.SessionEnd:
		OnSessionEnd(paneID, agentName, evt, LoadSettings())
	case event.UserPromptSubmit:
		OnUserPromptSubmit(paneID, evt)
		startDigest()
	case event.Notification:
		OnNotification(paneID, evt, LoadSettings())
		startDigest()
	case event.Stop:
		OnStop(paneID, evt, LoadSettings())
		startDigest()
	case event.StopFailure:
		OnStopFailure(paneID, evt, LoadSettings())
		startDigest()
	case event.PermissionDenied:
		OnPermissionDenied(paneID, evt, LoadSettings())
	case event.CwdChanged:
		// The Event carries no Agent/PermissionMode for this Kind (see the
		// event.Event doc comment), so there's no full AgentContext to build
		// — call SyncPaneLocation directly, with no dedicated On* function.
		SyncPaneLocation(paneID, evt.Cwd, evt.Worktree, evt.SessionID)
	case event.SubagentStart:
		OnSubagentStart(paneID, evt)
	case event.SubagentStop:
		OnSubagentStop(paneID, evt)
	case event.ActivityLog:
		activityLogStatusTransition(paneID, evt)
		HandleActivityLog(paneID, evt.ToolName, evt.ToolInput, evt.ToolResponse)
	case event.TaskCreated:
		// no-op — nothing reads TaskCreated yet in part 2b (the task-progress
		// reader is parts 3-4).
	case event.TaskCompleted:
		OnTaskCompleted(paneID, agentName, evt, LoadSettings())
	case event.TeammateIdle:
		OnTeammateIdle(paneID, evt)
	case event.WorktreeCreate:
		// no-op — no adapter's Parse() branch produces this Kind yet.
	case event.WorktreeRemove:
		OnWorktreeRemove(paneID)
	}
}

// startDigest has the avatar's line rewritten (`digest --auto`, which skips
// when the agents' state didn't change) on every agent event. Run by the tmux server (run-shell -b) so the
// model call never holds the agent's hook.
func startDigest() {
	tmux.RunTmux("run-shell", "-b", `"#{@agent_sidebar_bin}" digest --auto >/dev/null 2>&1`)
}
