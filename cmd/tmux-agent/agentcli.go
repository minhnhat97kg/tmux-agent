package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"tmux-agent/internal/queue"
	"tmux-agent/internal/scrape"
	"tmux-agent/internal/sharedctx"
	"tmux-agent/internal/sidebar"
	"tmux-agent/internal/tmux"
	"tmux-agent/internal/usage"
)

// Agent-to-agent commands (herdr's `agent list/read/prompt/wait`): let one
// agent — or a script — drive another agent's pane. All of them read the
// same @pane_* options the hooks/scraper write; none keep state of their own.
//
//	tmux-agent list
//	tmux-agent name [<pane>] <name>
//	tmux-agent read <pane> [-n 50]
//	tmux-agent new [--name N] [--agent claude] [--cwd DIR] [--no-wait] [-- args]
//	tmux-agent send <pane> [--wait] [--until …] [--timeout …] <text...>
//	tmux-agent next
//	tmux-agent kill <pane|name>...
//	tmux-agent reload
//	tmux-agent wait <pane> [--until stopped|idle|waiting] [--timeout 30m]
//	tmux-agent ctx [--global] [ls] | add <text...> | rm <id> | clear [--mine] | scope
//
// <pane> is an agent name (see `name`) or any tmux target (%12, sess:1.2, …).

// exitTimeout follows timeout(1)'s convention so shell scripts can tell a
// timeout apart from other failures.
const exitTimeout = 124

// displayStatus folds @pane_unseen into the status shown to scripts: an idle
// pane the user hasn't looked at yet is reported as "done".
func displayStatus(status tmux.PaneStatus, unseen bool) string {
	if status == tmux.StatusIdle && unseen {
		return "done"
	}
	return string(status)
}

// agentJSON is one agent in `list --json` (stable field names for scripts).
type agentJSON struct {
	Pane    string `json:"pane"`
	Name    string `json:"name,omitempty"`
	Agent   string `json:"agent"`
	Status  string `json:"status"`
	Session string `json:"session"`
	Window  string `json:"window"`
	Path    string `json:"path"`
	Elapsed int64  `json:"elapsed_s,omitempty"`
}

func cmdList(args []string, stdout io.Writer) int {
	asJSON := len(args) > 0 && args[0] == "--json"
	var out []agentJSON
	for _, s := range tmux.QuerySessions() {
		for _, w := range s.Windows {
			for _, p := range w.Panes {
				a := agentJSON{Pane: p.PaneID, Name: p.Name, Agent: string(p.Agent), Status: displayStatus(p.Status, p.Unseen),
					Session: s.SessionName, Window: w.WindowIndex + ":" + w.WindowName, Path: p.Path}
				if p.Status.IsActive() && p.StartedAt > 0 {
					a.Elapsed = time.Now().Unix() - p.StartedAt
				}
				out = append(out, a)
			}
		}
	}
	if asJSON {
		if out == nil {
			out = []agentJSON{}
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		enc.Encode(out)
		return exitOK
	}
	for _, a := range out {
		name := a.Name
		if name == "" {
			name = "-"
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s:%s\t%s\n", a.Pane, name, a.Agent, a.Status, a.Session, a.Window, a.Path)
	}
	return exitOK
}

func cmdRead(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("read", flag.ContinueOnError)
	fs.SetOutput(stderr)
	lines := fs.Int("n", 50, "number of lines from the bottom (includes scrollback)")
	pane, ok := parsePaneArg(fs, args, stderr, "read <pane> [-n lines]")
	if !ok {
		return exitUsage
	}
	out, err := tmux.RunTmuxCapture("capture-pane", "-p", "-J", "-t", pane, "-S", fmt.Sprintf("-%d", *lines))
	if err != nil {
		fmt.Fprintln(stderr, "read:", err)
		return exitFailure
	}
	fmt.Fprintln(stdout, out)
	return exitOK
}

// sendEnterDelay separates the typed text from Enter. TUIs like Claude Code
// treat a burst of input as a paste and would swallow an Enter that arrives
// in the same burst as a literal newline.
var sendEnterDelay = 300 * time.Millisecond

func cmdSend(args []string, stdout, stderr io.Writer) int {
	const usage = "usage: tmux-agent send <pane> [--now] [--wait] [--until stopped|idle|waiting] [--timeout 30m] <text...>"
	if len(args) < 2 {
		fmt.Fprintln(stderr, usage)
		return exitUsage
	}
	pane, rest := tmux.ResolvePane(args[0]), args[1:]
	// Options sit between the pane and the text; the text itself may start
	// with "-", so only these exact flags are consumed.
	wait, now, waitArgs := false, false, []string{pane}
	for len(rest) > 1 {
		switch rest[0] {
		case "--wait":
			wait, rest = true, rest[1:]
			continue
		case "--now":
			now, rest = true, rest[1:]
			continue
		case "--until", "--timeout":
			wait, waitArgs, rest = true, append(waitArgs, rest[0], rest[1]), rest[2:]
			continue
		}
		break
	}
	text := strings.Join(rest, " ")
	if text == "" {
		fmt.Fprintln(stderr, usage)
		return exitUsage
	}
	status := tmux.ParsePaneStatus(tmux.GetPaneOptionValue(pane, tmux.PaneStatusOpt))
	busy := status == tmux.StatusRunning || status == tmux.StatusBackground || status == tmux.StatusWaiting
	if now && status == tmux.StatusWaiting {
		// Typing into a pending approval dialog would answer it with
		// whatever the text happens to start with.
		fmt.Fprintf(stderr, "send: %s is waiting for approval; answer it first (or drop --now to queue)\n", pane)
		return exitFailure
	}
	if busy && !now {
		// Don't barge into a turn: queue it; the scraper delivers it when
		// the agent is idle again.
		pos, id, err := queue.Push(pane, text)
		if err != nil {
			fmt.Fprintln(stderr, "send:", err)
			return exitFailure
		}
		fmt.Fprintf(stdout, "queued for %s (#%d, it's %s)\n", args[0], pos, status)
		if !wait {
			return exitOK
		}
		for queue.Pending(id) {
			if _, alive := tmux.PaneSessionName(pane); !alive {
				fmt.Fprintf(stderr, "send: %s is gone\n", pane)
				return exitFailure
			}
			time.Sleep(waitPoll)
		}
		return cmdWait(waitArgs, stdout, stderr)
	}
	if _, ok := tmux.PaneSessionName(pane); !ok {
		fmt.Fprintf(stderr, "send: no pane %q\n", args[0])
		return exitFailure
	}
	tmux.TypeLine(pane, text)
	if wait {
		return cmdWait(waitArgs, stdout, stderr)
	}
	return exitOK
}

// cmdQueue lists or clears queued messages: `queue [pane]`, `queue clear <pane>`.
func cmdQueue(args []string, stdout, stderr io.Writer) int {
	if len(args) == 2 && args[0] == "clear" {
		fmt.Fprintf(stdout, "dropped %d message(s)\n", queue.Clear(tmux.ResolvePane(args[1])))
		return exitOK
	}
	panes := queue.Panes()
	if len(args) == 1 {
		panes = []string{tmux.ResolvePane(args[0])}
	}
	n := 0
	for _, p := range panes {
		for i, m := range queue.List(p) {
			fmt.Fprintf(stdout, "%s\t#%d\t%s\n", p, i+1, firstLineOf(m))
			n++
		}
	}
	if n == 0 {
		fmt.Fprintln(stdout, "no queued messages")
	}
	return exitOK
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}

// cmdReload makes running instances pick up a rebuilt binary: respawns every
// sidebar pane (same command, so the new binary) and starts a fresh scraper
// (the newest scraper takes over, the old one exits on its next tick).
func cmdReload(stdout io.Writer) int {
	out, _ := tmux.RunTmux("list-panes", "-a", "-F", "#{pane_id}|#{"+tmux.PaneRole+"}")
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if id, role, _ := strings.Cut(line, "|"); role == "sidebar" {
			tmux.RunTmux("respawn-pane", "-k", "-t", id)
			n++
		}
	}
	if v, _ := tmux.GetOption("@agent_scrape"); v != "off" {
		tmux.RunTmux("run-shell", "-b", `"#{@agent_sidebar_bin}" scrape >/dev/null 2>&1`)
	}
	fmt.Fprintf(stdout, "reloaded %d sidebar(s) and the scraper\n", n)
	return exitOK
}

var (
	waitPoll  = 500 * time.Millisecond
	waitGrace = 3 * time.Second
)

// waitReached reports whether status satisfies --until.
func waitReached(until string, status tmux.PaneStatus) bool {
	switch until {
	case "idle":
		return status == tmux.StatusIdle || status == tmux.StatusError
	case "waiting":
		return status == tmux.StatusWaiting
	}
	// "stopped": the agent no longer needs time — finished, failed, or
	// needs the user.
	return status == tmux.StatusIdle || status == tmux.StatusError || status == tmux.StatusWaiting
}

func cmdWait(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("wait", flag.ContinueOnError)
	fs.SetOutput(stderr)
	until := fs.String("until", "stopped", "stopped | idle | waiting")
	timeout := fs.Duration("timeout", 30*time.Minute, "give up after this long (exit 124)")
	pane, ok := parsePaneArg(fs, args, stderr, "wait <pane> [--until stopped|idle|waiting] [--timeout 30m]")
	if !ok {
		return exitUsage
	}
	switch *until {
	case "stopped", "idle", "waiting":
	default:
		fmt.Fprintf(stderr, "wait: unknown --until %q\n", *until)
		return exitUsage
	}

	start := time.Now()
	sawActive := false
	for {
		if _, alive := tmux.PaneSessionName(pane); !alive {
			fmt.Fprintf(stderr, "wait: pane %s is gone\n", pane)
			return exitFailure
		}
		status := tmux.ParsePaneStatus(tmux.GetPaneOptionValue(pane, tmux.PaneStatusOpt))
		sawActive = sawActive || status.IsActive() && status != tmux.StatusWaiting
		// Right after `send`, the hook may not have flipped the pane to
		// running yet — an already-idle pane only counts once it has been
		// seen working, or after a short grace period.
		if waitReached(*until, status) && (sawActive || time.Since(start) > waitGrace) {
			fmt.Fprintln(stdout, displayStatus(status, tmux.GetPaneOptionValue(pane, tmux.PaneUnseen) == "1"))
			return exitOK
		}
		if time.Since(start) > *timeout {
			fmt.Fprintf(stderr, "wait: timed out, %s is %s\n", pane, status)
			return exitTimeout
		}
		time.Sleep(waitPoll)
	}
}

// parsePaneArg accepts the pane target before or after the flags.
func parsePaneArg(fs *flag.FlagSet, args []string, stderr io.Writer, usage string) (string, bool) {
	var pane string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		pane, args = args[0], args[1:]
	}
	if err := fs.Parse(args); err != nil {
		return "", false
	}
	if pane == "" && fs.NArg() > 0 {
		pane = fs.Arg(0)
	}
	if pane == "" {
		fmt.Fprintln(stderr, "usage: tmux-agent", usage)
		return "", false
	}
	return tmux.ResolvePane(pane), true
}

// selfPane is the pane this command runs in ($TMUX_PANE), "" outside tmux.
func selfPane() string { return os.Getenv("TMUX_PANE") }

func cmdName(args []string, stderr io.Writer) int {
	var pane, name string
	switch len(args) {
	case 1:
		pane, name = selfPane(), args[0]
	case 2:
		pane, name = tmux.ResolvePane(args[0]), args[1]
	}
	if pane == "" || strings.ContainsAny(name, " \t|") {
		fmt.Fprintln(stderr, "usage: tmux-agent name [<pane>] <name>   (no spaces; \"\" clears)")
		return exitUsage
	}
	if name == "" {
		tmux.UnsetPaneOption(pane, tmux.PaneName)
		return exitOK
	}
	if owner := tmux.ResolvePane(name); owner != name && owner != pane {
		fmt.Fprintf(stderr, "name: %q is already used by %s\n", name, owner)
		return exitFailure
	}
	tmux.SetPaneOption(pane, tmux.PaneName, name)
	return exitOK
}

func cmdCtx(args []string, stdout, stderr io.Writer) int {
	// --global anywhere picks the cross-project board; otherwise the board
	// of the project containing the current directory.
	scope := ""
	var rest []string
	for _, a := range args {
		if a == "--global" {
			scope = sharedctx.Global
		} else {
			rest = append(rest, a)
		}
	}
	if scope == "" {
		wd, _ := os.Getwd()
		scope = sharedctx.Scope(wd)
	}
	sub := "ls"
	if len(rest) > 0 {
		sub, rest = rest[0], rest[1:]
	}
	pane := selfPane()
	author := pane
	label := ""
	if pane != "" {
		if n := tmux.GetPaneOptionValue(pane, tmux.PaneName); n != "" {
			author = n
		} else if agent := tmux.GetPaneOptionValue(pane, tmux.PaneAgent); agent != "" {
			label = agent
			if wd, err := os.Getwd(); err == nil {
				label += " · " + filepath.Base(wd)
			}
		}
	}
	if author == "" {
		author = "user"
	}

	switch sub {
	case "ls":
		for _, e := range sharedctx.List(scope) {
			fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", e.ID, e.Author, e.Time.Format("01-02 15:04"), e.Text)
		}
		return exitOK
	case "add":
		e, err := sharedctx.Add(scope, author, pane, strings.Join(rest, " "), label)
		if err != nil {
			fmt.Fprintln(stderr, "ctx add:", err)
			return exitFailure
		}
		fmt.Fprintln(stdout, e.ID)
		return exitOK
	case "rm":
		code := exitOK
		for _, id := range rest {
			if err := sharedctx.Remove(scope, id); err != nil {
				fmt.Fprintln(stderr, "ctx rm:", err)
				code = exitFailure
			}
		}
		return code
	case "clear":
		only := ""
		if len(rest) > 0 && rest[0] == "--mine" {
			only = author
		}
		fmt.Fprintf(stdout, "removed %d\n", sharedctx.Clear(scope, only))
		return exitOK
	case "scope":
		fmt.Fprintln(stdout, sharedctx.Dir(scope))
		return exitOK
	}
	fmt.Fprintln(stderr, "usage: tmux-agent ctx [--global] [ls] | add <text...> | rm <id...> | clear [--mine] | scope")
	return exitUsage
}

// ─── new: spawn an agent into a pool window ─────────────────────────────

// Agents started with `new` live in "agents" windows (window option
// @agent_pool=1) instead of the caller's window, at most @agent_max_panes
// agent panes each (the sidebar pane doesn't count); a full pool opens a
// new window.
const (
	poolOption     = "@agent_pool"
	maxPanesOption = "@agent_max_panes"
	poolWindowName = "agents"
	defaultMaxPane = 4
)

var newReadyTimeout = 30 * time.Second

// placement is where the next agent pane goes: split splitTarget in
// direction splitDir, or (splitTarget == "") open a new pool window.
type placement struct {
	splitTarget string
	splitDir    string // "-h" side by side, "-v" stacked
}

// planPlacement picks a pool window with room from list-panes output
// ("window_id|pool|role|pane_id|width|height" per line) and splits its
// largest agent pane along its longer side. Fields are "|"-separated: an
// empty option (most panes have no @pane_role) must not shift the columns — a terminal cell is about twice
// as tall as wide, so "longer" means width > 2×height. Filling a window this
// way yields a 2×2 grid at 4 panes without re-laying-out the window.
func planPlacement(listPanes string, maxPanes int) placement {
	type win struct {
		count      int
		largest    string
		area, w, h int
	}
	var order []string
	wins := map[string]*win{}
	for _, line := range strings.Split(strings.TrimSpace(listPanes), "\n") {
		f := strings.Split(line, "|")
		if len(f) < 6 || f[1] != "1" || f[2] == "sidebar" {
			continue
		}
		w, _ := strconv.Atoi(f[4])
		h, _ := strconv.Atoi(f[5])
		x, ok := wins[f[0]]
		if !ok {
			x = &win{}
			wins[f[0]] = x
			order = append(order, f[0])
		}
		x.count++
		if w*h > x.area {
			x.area, x.largest, x.w, x.h = w*h, f[3], w, h
		}
	}
	for _, id := range order {
		if x := wins[id]; x.count < maxPanes {
			dir := "-v"
			if x.w > 2*x.h {
				dir = "-h"
			}
			return placement{splitTarget: x.largest, splitDir: dir}
		}
	}
	return placement{}
}

func cmdNew(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	fs.SetOutput(stderr)
	o := spawnOpts{}
	fs.StringVar(&o.Name, "name", "", "agent name (see `name`); default: the role")
	fs.StringVar(&o.Agent, "agent", "", "agent command (default: @agent-sidebar-default-agent, else claude)")
	fs.StringVar(&o.Cwd, "cwd", "", "working directory (default: the current directory)")
	fs.StringVar(&o.Role, "role", "", "role from `tmux-agent roles` (reviewer, tester, …)")
	fs.BoolVar(&o.Worktree, "worktree", false, "run in its own git worktree on branch agent/<name>")
	noWait := fs.Bool("no-wait", false, "don't wait for the agent to come up")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	o.Args, o.Wait = fs.Args(), !*noWait
	pane, warn, err := spawnAgent(o)
	if err != nil {
		fmt.Fprintln(stderr, "new:", err)
		return exitFailure
	}
	fmt.Fprintln(stdout, pane)
	if warn != "" {
		fmt.Fprintln(stderr, "new:", warn)
	}
	return exitOK
}

// cmdKill closes agent panes by name or pane id. Only panes that report an
// agent (@pane_agent) are killed — a typo must not close a shell or an
// editor — and never the caller's own pane. A pool window left with only
// its sidebar is closed too.
func cmdKill(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: tmux-agent kill <pane|name>...")
		return exitUsage
	}
	code := exitOK
	for _, target := range args {
		pane := tmux.ResolvePane(target)
		id := tmux.DisplayMessage(pane, "#{pane_id}")
		switch {
		case id == "":
			fmt.Fprintf(stderr, "kill: no pane %q\n", target)
			code = exitFailure
		case id == selfPane():
			fmt.Fprintf(stderr, "kill: %s is this pane — refusing\n", target)
			code = exitFailure
		case tmux.GetPaneOptionValue(id, tmux.PaneAgent) == "":
			fmt.Fprintf(stderr, "kill: %s (%s) is not an agent pane — refusing\n", target, id)
			code = exitFailure
		default:
			window := tmux.DisplayMessage(id, "#{window_id}")
			if _, err := tmux.RunTmuxCapture("kill-pane", "-t", id); err != nil {
				fmt.Fprintf(stderr, "kill: %s: %v\n", target, err)
				code = exitFailure
				continue
			}
			// kill-pane doesn't fire pane-exited, so the auto-close hook
			// won't run: close a window left with only its sidebar here.
			sidebar.AutoClose([]string{window})
			fmt.Fprintf(stdout, "killed %s (%s)\n", target, id)
		}
	}
	return code
}

// cmdNext is the avatar's quick action from anywhere (prefix + a): act on
// the most urgent agent — jump to one that waits on you or failed; for one
// that finished, send @agent_done_prompt if set, else jump to it.
func cmdNext(stdout io.Writer) int {
	alerts := sidebar.Alerts(tmux.QuerySessions())
	if len(alerts) == 0 {
		fmt.Fprintln(stdout, "nothing needs you")
		tmux.RunTmux("display-message", "tmux-agent: nothing needs you")
		return exitOK
	}
	from := selfPane()
	if from == "" {
		out, _ := tmux.RunTmux("display-message", "-p", "#{pane_id}")
		from = strings.TrimSpace(out)
	}
	fmt.Fprintln(stdout, sidebar.Act(alerts[0], sidebar.AvatarConfigFromTmux(), from))
	return exitOK
}

// cmdAnswer answers an agent's approval/question prompt from outside its
// pane: the option key (1, 2, … or y/n) must be one of those on screen.
func cmdAnswer(args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 {
		fmt.Fprintln(stderr, "usage: tmux-agent answer <pane> <option>   (see the options with `tmux-agent answer <pane> ?`)")
		return exitUsage
	}
	pane := tmux.ResolvePane(args[0])
	screen, ok := tmux.RunTmux("capture-pane", "-p", "-t", pane)
	if !ok {
		fmt.Fprintf(stderr, "answer: no pane %q\n", args[0])
		return exitFailure
	}
	d, ok := scrape.ParseDialog(screen)
	if !ok {
		fmt.Fprintf(stderr, "answer: %s has no prompt on screen\n", args[0])
		return exitFailure
	}
	for _, o := range d.Options {
		if o.Key == args[1] {
			scrape.Answer(pane, o.Key)
			fmt.Fprintf(stdout, "answered %s: %s\n", args[0], o.Label)
			return exitOK
		}
	}
	fmt.Fprintln(stdout, d.Question)
	for _, o := range d.Options {
		fmt.Fprintf(stdout, "  %s  %s\n", o.Key, o.Label)
	}
	if args[1] != "?" {
		return exitFailure
	}
	return exitOK
}

// cmdTokens prints each Claude agent's token usage (from its transcript)
// and the total.
func cmdTokens(stdout io.Writer) int {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "AGENT\tPANE\tTOKENS")
	var total usage.Tokens
	n := 0
	for _, s := range tmux.QuerySessions() {
		for _, win := range s.Windows {
			for _, p := range win.Panes {
				t, ok := usage.ForSession(p.SessionID)
				if !ok {
					continue
				}
				name := p.Name
				if name == "" {
					name = string(p.Agent)
				}
				fmt.Fprintf(w, "%s\t%s\t%s\n", name, p.PaneID, t.Short())
				total.Add(t)
				n++
			}
		}
	}
	if n == 0 {
		fmt.Fprintln(stdout, "no agent with a readable transcript (token usage is read from Claude Code sessions)")
		return exitOK
	}
	fmt.Fprintf(w, "total\t\t%s\n", total.Short())
	w.Flush()
	return exitOK
}
