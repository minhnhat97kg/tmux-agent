---
name: tmux-agent
description: Coordinate other AI coding agents (Claude Code, Codex, Pi, Copilot, Gemini…) running in other tmux panes — list them and their status, name them, read their screen, send them a prompt, wait until they finish or need approval, and share notes with all of them through a common context board. Use when the user asks to delegate to, check on, hand off to, or orchestrate agents in other tmux panes/windows, or to share/read context between agents.
---

# tmux-agent: drive agents in other tmux panes

`{{BIN}}` reads the status every agent pane reports to tmux. Only works inside tmux. Your own pane is `$TMUX_PANE`; never `send` to it.

| Command | Does |
|---|---|
| `{{BIN}} list` | One line per agent pane: `pane_id  name  agent  status  session:window  cwd` |
| `{{BIN}} new --name <name> [--role r] [--worktree] [--agent claude] [-- args]` | Starts a new agent in an "agents" window (max panes per window is configured), prints its pane id, waits until it's up. `--role` gives it a role (`{{BIN}} roles`), `--worktree` its own git worktree on branch `agent/<name>` |
| `{{BIN}} name [<pane>] <name>` | Names a pane (yours by default); the name then works anywhere `<pane>` does |
| `{{BIN}} read <pane> [-n 50]` | Last N lines of that pane's screen (plain text) |
| `{{BIN}} send <pane> [--now] [--wait] [--timeout 30m] <text…>` | Types the text and presses Enter; to a busy agent it's queued and delivered when the agent is idle (`--now` interrupts). `--wait` then blocks like `wait` |
| `{{BIN}} ask [--role r] [--wait] "<task>"` | Hands a task to an idle agent of the current repo, or starts one |
| `{{BIN}} undo <agent>` | Rolls the repo back to before that agent's last turn (prints how to redo) |
| `{{BIN}} wait <pane> [--until stopped\|idle\|waiting] [--timeout 30m]` | Blocks until the status matches, prints it. Exit 124 on timeout |
| `{{BIN}} kill <pane…>` | Closes agent panes you started once their work is done (refuses non-agent panes and your own) |
| `{{BIN}} fanout -n 3 [--role r] [--wait] "<prompt>"` | Same task to N agents in separate worktrees (best-of-N); `{{BIN}} compare <prefix>` shows what each changed |
| `{{BIN}} diff <name> [--stat]` · `merge <name> [--discard]` · `discard <name>` | Review / keep / drop a worktree agent's changes |
| `{{BIN}} plan run <file>` | Runs tasks in dependency order; file lines: `name [@role] [after a,b]: prompt` |
| `{{BIN}} list --json` · `events --json` · `tokens` | Machine-readable list / event stream; token usage per agent |
| `{{BIN}} events [--json]` | Long-running stream of observed status / tool changes; `--json` emits one JSON object per line |

`<pane>` is an agent name or a tmux target (`%12`, `sess:1.2`).

Statuses: `running` (working), `background` (agent stopped, background shell still running), `waiting` (needs approval/answer from a human), `idle` (finished, already seen), `done` (finished, not yet viewed), `error`.
`--until stopped` (default) returns on `idle`, `done`, `error`, or `waiting`.

## Delegating a task

```sh
{{BIN}} name lead                              # name yourself once
{{BIN}} new --name tester                      # or reuse an idle agent from `list`
{{BIN}} send tester "Run the test suite and fix any failures"
```

## Listening without blocking the conversation

- To wait for one agent, send its task without `--wait`, then run `{{BIN}} wait <pane> --timeout 30m` once using your harness's background command tool, if it delivers command completion notifications to the conversation. Keep the handle and continue other work. On completion inspect the exit code and output: `waiting` needs user attention, `idle` / `done` may have results, and exit 124 means timeout; verify with `list` and `read`.
- `wait` observes pane status, not a specific task or queued message. A fast task or queued follow-up can race with status changes; inspect `queue <pane>` and the agent's output before treating the task as complete.
- For a continuous view of several agents, use `{{BIN}} events --json` only if the harness can deliver streaming output to the conversation. Start it before sending work, keep its handle, and stop it when it is no longer needed.
- Events have `time`, `agent`, `kind`, and `text`. On a relevant `done`, `idle`, `waiting`, or `error` event, use `{{BIN}} list --json` and `{{BIN}} read <pane> -n 80` to verify the current state and inspect the result or question. `agent` is a display name when available, otherwise a pane ID; a `gone` event uses the pane ID.
- The stream samples pane state about once per second, starts with a silent snapshot, and has no replay or ready acknowledgement. It can miss short transitions and startup races; reconcile with `list` rather than treating the stream as a complete history.
- Do not run `wait` or `send --wait` in a foreground tool call when chat must remain responsive. Do not use `sleep` loops, repeated status checks, or messages to your own pane to simulate notifications. The `wait` command checks status internally and returns after the state matches or the timeout expires.
- A background process or redirected file alone cannot resume a finished chat turn. If your harness does not deliver background completion or stream output as conversation events, say that automatic follow-up is unavailable and return control to the user; check the current status when asked. Never claim you will be notified automatically without that harness capability.

- `new` may report the agent waiting on a startup prompt (e.g. folder trust): that is the user's call — tell them.
- `{{BIN}} answer <pane> <option>` exists for the user; don't use it to approve on someone's behalf unless the user explicitly told you to.
- If an agent is `waiting`, it needs approval or an answer. Tell the user which pane (`read` it to quote the question). Do not answer approval prompts yourself unless the user told you to.
- Prompts are typed as one line. Keep them self-contained; the other agent can't see your conversation.
- Don't `send` to a `running` pane unless you mean to queue a follow-up.

## Shared context board

Notes every agent working on the same project (git repo of your current directory) can read; `--global` for notes that span projects. Use it for facts other agents need: decisions, conventions, "I'm editing X, don't touch it", where results are. Not for chat.

```sh
{{BIN}} ctx                                    # read all notes: id  author  time  text
{{BIN}} ctx --global                           # read cross-project notes
{{BIN}} ctx add "auth refactor lives in internal/auth; don't edit until I post done"
{{BIN}} ctx --global add "shared note for all projects"
{{BIN}} ctx rm <id>                            # remove a note that is no longer true
{{BIN}} ctx clear --mine                       # remove all your notes when your task ends
```

- Read `ctx` before starting work that other agents may be touching.
- Remove your notes when they stop being true. Don't delete other agents' notes unless the user asks or the note is clearly obsolete.

### Task board convention

The same board doubles as a task list. Lines start with a status and the agent the task is for:

```
TODO @<name>: <task>
DOING @<name>: <task>
DONE @<name>: <task> → <one-line result: what changed, where>
```

Lead agent (splitting work):

```sh
{{BIN}} ctx add "TODO @w1: write tests for internal/auth"
{{BIN}} ctx add "TODO @w2: update the README install section"
{{BIN}} new --name w1 && {{BIN}} send w1 "You are w1. Work the task board (tmux-agent skill, task board convention): do every TODO @w1."
{{BIN}} new --name w2 && {{BIN}} send w2 "You are w2. Work the task board (tmux-agent skill, task board convention): do every TODO @w2."
# Start a background `{{BIN}} wait w1` and `{{BIN}} wait w2` if supported.
# On completion notifications, inspect each worker and read `{{BIN}} ctx`.
```

Worker (you were told to work the board):

1. `{{BIN}} ctx` — find the TODOs with your name (the lead tells you your name).
2. Starting one: `{{BIN}} ctx rm <todo id>` and `{{BIN}} ctx add "DOING @<you>: <task>"`.
3. Finished: `{{BIN}} ctx rm <doing id>` and `{{BIN}} ctx add "DONE @<you>: <task> → <result>"`.
4. Repeat until no TODO has your name, then stop.

- `{{BIN}} team` starts the user's configured workers (`config set team "w1 w2"`) already told to work the board; prefer them over `new` when they exist.
- Always put a name on a TODO: there is no atomic claim, so two workers could grab the same unnamed task.
- Agents in `--worktree` checkouts see a different board (each worktree is its own project): use `ctx --global` for the whole team then.
- Leave DONE lines for the lead; the lead clears them (`ctx rm`) after reading.
