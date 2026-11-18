package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"
	"time"

	"tmux-agent/internal/queue"
	"tmux-agent/internal/tmux"
)

// ─── fanout / compare: best-of-N ─────────────────────────────────────────

// cmdFanout runs the same prompt in N agents, each in its own worktree, so
// you can compare the attempts and merge the best one.
func cmdFanout(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("fanout", flag.ContinueOnError)
	fs.SetOutput(stderr)
	n := fs.Int("n", 3, "number of attempts")
	base := fs.String("name", "try", "name prefix: <name>-1, <name>-2, …")
	role := fs.String("role", "", "role for every attempt")
	agent := fs.String("agent", "", "agent command")
	wait := fs.Bool("wait", false, "wait for all, then print the comparison")
	timeout := fs.Duration("timeout", 30*time.Minute, "with --wait: give up after")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	prompt := strings.Join(fs.Args(), " ")
	if prompt == "" || *n < 1 {
		fmt.Fprintln(stderr, `usage: tmux-agent fanout [-n 3] [--name try] [--role r] [--wait] "prompt"`)
		return exitUsage
	}
	var panes []string
	for i := 1; i <= *n; i++ {
		name := fmt.Sprintf("%s-%d", *base, i)
		pane, warn, err := spawnAgent(spawnOpts{Name: name, Agent: *agent, Role: *role, Worktree: true, Wait: true})
		if err != nil {
			fmt.Fprintf(stderr, "fanout: %s: %v\n", name, err)
			return exitFailure
		}
		if warn != "" {
			fmt.Fprintf(stderr, "fanout: %s\n", warn)
		}
		panes = append(panes, pane)
		fmt.Fprintf(stdout, "%s → %s\n", name, pane)
	}
	for _, p := range panes {
		sendText(p, prompt)
	}
	if !*wait {
		fmt.Fprintf(stdout, "running; compare with: tmux-agent compare %s\n", *base)
		return exitOK
	}
	deadline := time.Now().Add(*timeout)
	for _, p := range panes {
		if st := waitStopped(p, time.Until(deadline)); st == "" {
			fmt.Fprintf(stderr, "fanout: timed out waiting for %s\n", p)
			return exitTimeout
		}
	}
	return cmdCompare([]string{*base}, stdout, stderr)
}

// cmdCompare lists the attempts <base>-* side by side: status and what each
// changed, with the commands to keep one.
func cmdCompare(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: tmux-agent compare <name-prefix>")
		return exitUsage
	}
	wd, _ := os.Getwd()
	repo, _, _, err := worktreePaths(wd, "x")
	if err != nil {
		fmt.Fprintln(stderr, "compare:", err)
		return exitFailure
	}
	dirs, _ := filepath.Glob(filepath.Join(repo, ".worktrees", args[0]+"-*"))
	if len(dirs) == 0 {
		fmt.Fprintf(stderr, "compare: no worktrees %s-* in %s\n", args[0], repo)
		return exitFailure
	}
	head, _ := git(repo, "rev-parse", "HEAD")
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ATTEMPT\tSTATUS\tCHANGES")
	for _, d := range dirs {
		name := filepath.Base(d)
		status := "closed"
		if pane := tmux.ResolvePane(name); pane != name {
			status = displayStatus(tmux.ParsePaneStatus(tmux.GetPaneOptionValue(pane, tmux.PaneStatusOpt)), tmux.GetPaneOptionValue(pane, tmux.PaneUnseen) == "1")
		}
		git(d, "add", "-N", ".")
		stat, _ := git(d, "diff", "--shortstat", head)
		if stat == "" {
			stat = "no changes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", name, status, strings.TrimSpace(stat))
	}
	w.Flush()
	fmt.Fprintf(stdout, "\nreview:  tmux-agent diff <attempt>\nkeep:    tmux-agent merge <attempt> --discard\ndrop:    tmux-agent discard <attempt>\n")
	return exitOK
}

// waitStopped waits until pane has run and stopped (idle/done/error/
// waiting); "" on timeout or when the pane is gone.
func waitStopped(pane string, timeout time.Duration) tmux.PaneStatus {
	start := time.Now()
	sawActive := false
	for time.Since(start) < timeout {
		if _, alive := tmux.PaneSessionName(pane); !alive {
			return ""
		}
		st := tmux.ParsePaneStatus(tmux.GetPaneOptionValue(pane, tmux.PaneStatusOpt))
		sawActive = sawActive || st == tmux.StatusRunning || st == tmux.StatusBackground
		if waitReached("stopped", st) && (sawActive || time.Since(start) > waitGrace) {
			return st
		}
		time.Sleep(waitPoll)
	}
	return ""
}

// ─── plan: tasks with dependencies ───────────────────────────────────────

// task is one line of a plan file:
//
//	<name> [@role] [after a,b]: <prompt>
type task struct {
	Name, Role, Prompt string
	After              []string
}

var taskRe = regexp.MustCompile(`^([\w.-]+)(?:\s+@([\w.-]+))?(?:\s+after\s+([\w.,\s-]+?))?\s*:\s*(.+)$`)

// parsePlan reads tasks; # comments and blank lines are skipped. Every
// dependency must name a task of the plan.
func parsePlan(r io.Reader) ([]task, error) {
	var tasks []task
	names := map[string]bool{}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := taskRe.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("line %d: want `name [@role] [after a,b]: prompt`", n)
		}
		t := task{Name: m[1], Role: m[2], Prompt: strings.TrimSpace(m[4])}
		for _, d := range strings.Split(m[3], ",") {
			if d = strings.TrimSpace(d); d != "" {
				t.After = append(t.After, d)
			}
		}
		if names[t.Name] {
			return nil, fmt.Errorf("line %d: task %q twice", n, t.Name)
		}
		names[t.Name] = true
		tasks = append(tasks, t)
	}
	for _, t := range tasks {
		for _, d := range t.After {
			if !names[d] {
				return nil, fmt.Errorf("task %q runs after unknown task %q", t.Name, d)
			}
		}
	}
	return tasks, sc.Err()
}

// cmdPlan runs a plan file: starts each task's agent once its
// dependencies are done, hands it the prompt, reports progress, and stops
// when every task finished (or failed). Prompts waiting on you are
// reported, never answered.
func cmdPlan(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	agent := fs.String("agent", "", "agent command for every task")
	timeout := fs.Duration("timeout", 2*time.Hour, "give up after")
	if len(args) > 0 && args[0] == "run" {
		args = args[1:]
	}
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: tmux-agent plan run <file>   (lines: name [@role] [after a,b]: prompt)")
		return exitUsage
	}
	f, err := os.Open(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, "plan:", err)
		return exitFailure
	}
	tasks, err := parsePlan(f)
	f.Close()
	if err != nil {
		fmt.Fprintln(stderr, "plan:", err)
		return exitUsage
	}

	pane := map[string]string{}
	state := map[string]string{} // "" pending · running · done · failed
	seenRun := map[string]bool{}
	warned := map[string]bool{}
	logf := func(format string, a ...any) {
		fmt.Fprintf(stdout, "%s  %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, a...))
	}
	deadline := time.Now().Add(*timeout)
	for time.Now().Before(deadline) {
		finished := 0
		for _, t := range tasks {
			switch state[t.Name] {
			case "done", "failed":
				finished++
				continue
			case "":
				ready := true
				for _, d := range t.After {
					if state[d] != "done" {
						ready = false
					}
					if state[d] == "failed" {
						state[t.Name] = "failed"
						logf("✕ %s skipped: %s failed", t.Name, d)
					}
				}
				if !ready || state[t.Name] != "" {
					continue
				}
				p, warn, err := spawnAgent(spawnOpts{Name: freeName(t.Name), Agent: *agent, Role: t.Role, Wait: true})
				if err != nil {
					state[t.Name] = "failed"
					logf("✕ %s could not start: %v", t.Name, err)
					continue
				}
				if warn != "" {
					logf("▲ %s: %s", t.Name, warn)
				}
				pane[t.Name], state[t.Name] = p, "running"
				sendText(p, t.Prompt)
				logf("▶ %s started (%s)", t.Name, p)
				continue
			}
			st := tmux.ParsePaneStatus(tmux.GetPaneOptionValue(pane[t.Name], tmux.PaneStatusOpt))
			if _, alive := tmux.PaneSessionName(pane[t.Name]); !alive {
				state[t.Name] = "failed"
				logf("✕ %s: pane closed", t.Name)
				continue
			}
			switch st {
			case tmux.StatusRunning, tmux.StatusBackground:
				seenRun[t.Name], warned[t.Name] = true, false
			case tmux.StatusWaiting:
				if !warned[t.Name] {
					warned[t.Name] = true
					logf("▲ %s is waiting for you — answer it (tmux-agent answer %s ?)", t.Name, t.Name)
				}
			case tmux.StatusError:
				state[t.Name] = "failed"
				logf("✕ %s failed", t.Name)
			case tmux.StatusIdle:
				if seenRun[t.Name] {
					state[t.Name] = "done"
					logf("✓ %s done", t.Name)
				}
			}
		}
		if finished == len(tasks) {
			break
		}
		time.Sleep(2 * time.Second)
	}
	failed := 0
	for _, t := range tasks {
		if state[t.Name] != "done" {
			failed++
		}
	}
	logf("plan finished: %d/%d done", len(tasks)-failed, len(tasks))
	if failed > 0 {
		return exitFailure
	}
	return exitOK
}

// ─── ask: hand a task to whoever is free ─────────────────────────────────

// cmdAsk gives a task to an idle agent working in the same repo (with the
// role, if --role), or starts a new one when nobody is free.
func cmdAsk(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ask", flag.ContinueOnError)
	fs.SetOutput(stderr)
	role := fs.String("role", "", "only agents with this role (a new one gets it too)")
	agent := fs.String("agent", "", "agent command for a new agent")
	wait := fs.Bool("wait", false, "wait until it's done")
	timeout := fs.Duration("timeout", 30*time.Minute, "with --wait: give up after")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	task := strings.Join(fs.Args(), " ")
	if task == "" {
		fmt.Fprintln(stderr, `usage: tmux-agent ask [--role r] [--wait] "task"`)
		return exitUsage
	}
	wd, _ := os.Getwd()
	root := wd
	if r, err := git(wd, "rev-parse", "--show-toplevel"); err == nil {
		root = r
	}
	pane, name := pickIdle(tmux.QuerySessions(), root, *role)
	if pane == "" {
		base := *role
		if base == "" {
			base = "helper"
		}
		var warn string
		var err error
		name = freeName(base)
		if pane, warn, err = spawnAgent(spawnOpts{Name: name, Role: *role, Agent: *agent, Cwd: wd, Wait: true}); err != nil {
			fmt.Fprintln(stderr, "ask:", err)
			return exitFailure
		}
		if warn != "" {
			fmt.Fprintln(stderr, "ask:", warn)
		}
		fmt.Fprintf(stdout, "nobody free — started %s (%s)\n", name, pane)
	} else {
		fmt.Fprintf(stdout, "→ %s (%s)\n", name, pane)
	}
	tmux.TypeLine(pane, task)
	if *wait {
		return cmdWait([]string{pane, "--timeout", timeout.String()}, stdout, stderr)
	}
	return exitOK
}

// pickIdle finds an idle agent under root (its cwd in the repo), with role
// if given and nothing queued for it. Returns its pane and display name.
func pickIdle(sessions []tmux.SessionInfo, root, role string) (pane, name string) {
	for _, s := range sessions {
		for _, w := range s.Windows {
			for _, p := range w.Panes {
				if p.Status != tmux.StatusIdle || !(p.Path == root || strings.HasPrefix(p.Path, root+string(os.PathSeparator))) {
					continue
				}
				if role != "" && tmux.GetPaneOptionValue(p.PaneID, roleOption) != role {
					continue
				}
				if len(queue.List(p.PaneID)) > 0 || p.PaneID == selfPane() {
					continue
				}
				name = p.Name
				if name == "" {
					name = p.PaneID
				}
				return p.PaneID, name
			}
		}
	}
	return "", ""
}
