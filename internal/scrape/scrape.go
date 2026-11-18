// Package scrape is the fallback status source for agents with no hook
// integration (Copilot CLI, Gemini, Cursor, Aider, …, or Claude/Codex before
// `tmux-agent setup` was run): it polls the bottom of each agent pane with
// capture-pane and guesses running/waiting/idle from what's on screen —
// herdr's "screen detection" path.
//
// Hooks always win: a pane whose @pane_agent was written by a hook (no
// @pane_agent_src=scrape mark) is never touched, and the first hook to fire
// in a scraped pane clears the mark (hook.SetAgentMeta), taking it over.
package scrape

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"tmux-agent/internal/hook"
	"tmux-agent/internal/process"
	"tmux-agent/internal/queue"
	"tmux-agent/internal/tmux"
)

// pidOption holds the pid of the scraper that currently owns the job. The
// newest instance always wins: a config reload or an upgraded binary starts a
// fresh scraper, and the old one exits on its next tick.
const pidOption = "@agent_scrape_pid"

const (
	interval = time.Second
	// maxFailures consecutive tmux errors means the server is gone.
	maxFailures = 5
	// idleTicks: running → idle needs this many idle polls in a row, so a
	// spinner frame that happens to miss the "esc to interrupt" line doesn't
	// flap the status.
	idleTicks = 2
	srcScrape = "scrape"
)

// agents maps a process name (comm, argv0 or argv1 basename) to the label
// written to @pane_agent. Labels must be accepted by tmux.ParseAgentType.
var agents = map[string]string{
	"claude":       "claude",
	"codex":        "codex",
	"opencode":     "opencode",
	"pi":           "pi",
	"copilot":      "copilot",
	"gemini":       "gemini",
	"cursor-agent": "cursor-agent",
	"aider":        "aider",
	"amp":          "amp",
}

// ponytail: one generic pattern set for every agent; per-agent manifests
// (like herdr's) if a specific CLI gets misclassified.
var (
	blockedRe = regexp.MustCompile(`(?i)(do you want to (proceed|make this edit|create|run|allow|continue)|would you like to (run|proceed|allow)|\b(allow|approve)\b.*\b(once|always|session)\b|yes, and don't ask again|\[y/n\]|\(y/n\)|waiting for (your )?(approval|confirmation)|trust (this|the files in this) folder|enter to confirm)`)
	// The agent's own "you stopped me" line, not any "interrupted" in output.
	interruptedRe = regexp.MustCompile(`(?i)(interrupted by user|interrupted · what should|conversation interrupted)`)
	workingRe     = regexp.MustCompile(`(?i)(esc to (interrupt|cancel)|ctrl\+c to (interrupt|cancel|stop))|^\s*[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏⣾⣽⣻⢿⡿⣟⣯⣷]|^\s*[✻✶✳✢·*]\s+\S+…`)
)

// tailLines is how much of the bottom of the screen is searched for a
// permission dialog; workingLines for a "working" indicator, which TUIs draw
// in place right above their input box — further up it's stale output.
const (
	tailLines      = 20
	workingLines   = 6
	interruptLines = 8
)

// Classify guesses an agent's status from the visible screen.
func Classify(screen string) tmux.PaneStatus {
	lines := strings.Split(strings.TrimRight(screen, "\n "), "\n")
	var tail []string
	for i := len(lines) - 1; i >= 0 && len(tail) < tailLines; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			tail = append(tail, lines[i])
		}
	}
	for _, l := range tail {
		if blockedRe.MatchString(l) {
			return tmux.StatusWaiting
		}
	}
	for i, l := range tail {
		if i < workingLines && workingRe.MatchString(l) {
			return tmux.StatusRunning
		}
	}
	return tmux.StatusIdle
}

// DetectAgent finds a known agent in the process tree rooted at panePID.
func DetectAgent(snap *process.Snapshot, panePID int) string {
	for pid := range snap.Descendants([]int{panePID}) {
		info := snap.InfoByPID[pid]
		if a, ok := agents[process.CommandBasename(info.Comm)]; ok {
			return a
		}
		// Node/Python CLIs: argv = [node, /path/to/bin/gemini, …].
		argv := strings.Fields(info.Args)
		for i := 0; i < len(argv) && i < 2; i++ {
			if a, ok := agents[process.CommandBasename(strings.Trim(argv[i], `"`))]; ok {
				return a
			}
		}
	}
	return ""
}

// Run is the `tmux-agent scrape` loop, started in the background by
// agent-sidebar.conf.
func Run() int {
	me := strconv.Itoa(os.Getpid())
	tmux.SetOption(pidOption, me)
	streak := map[string]int{}
	var lastSig string
	failures := 0
	wd := newWatchdog()
	for {
		owner, ok := tmux.GetOption(pidOption)
		if !ok {
			failures++
		} else if owner != me {
			return 0
		}
		if !tick(streak, &lastSig) {
			failures++
		} else if ok {
			failures = 0
		}
		if failures >= maxFailures {
			return 0
		}
		wd.maybeRun(time.Now())
		time.Sleep(interval)
	}
}

const paneFormat = "#{pane_id}|#{pane_pid}|#{pane_current_command}|#{@pane_agent}|#{@pane_agent_src}|#{@pane_status}|#{@pane_role}|#{@pane_unseen}|#{@pane_wait_reason}"

type pane struct {
	id, agent, src, status, waitReason string
	pid                                int
}

func tick(streak map[string]int, lastSig *string) bool {
	out, ok := tmux.RunTmux("list-panes", "-a", "-F", paneFormat)
	if !ok {
		return false
	}
	// Status lines only redraw every status-interval; agent status lives in
	// pane options, which don't trigger a redraw. Status/unseen are the
	// last fields, so the previous tick's raw output is a cheap signature —
	// it lags one tick behind this tick's own writes, which is fine at 1s.
	if out != *lastSig {
		*lastSig = out
		refreshClients()
	}
	deliverQueued(out)
	var candidates, hookedPanes []pane
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.SplitN(line, "|", 9)
		if len(f) < 9 || f[6] == "sidebar" {
			continue
		}
		p := pane{id: f[0], agent: f[3], src: f[4], status: f[5], waitReason: f[8]}
		p.pid, _ = strconv.Atoi(f[1])
		hooked := p.agent != "" && p.src != srcScrape
		if hooked {
			switch {
			case p.status == string(tmux.StatusRunning):
				checkInterrupted(p, streak)
			case p.status == string(tmux.StatusWaiting) && hook.IsPermissionWaitReason(p.waitReason):
				checkAnswered(p, streak)
			}
			if knownAgent(p.agent) {
				hookedPanes = append(hookedPanes, p)
			}
			continue
		}
		// No shortcut on pane_current_command: an agent started through a
		// wrapper script reports its interpreter ("bash", "python3"), so only
		// the process tree tells. DetectAgent also releases panes whose agent
		// exited back to the shell.
		candidates = append(candidates, p)
	}
	if len(candidates) == 0 && len(hookedPanes) == 0 {
		return true
	}
	snap, ok := process.Scan()
	if !ok {
		return true
	}
	for _, p := range hookedPanes {
		checkExited(p, snap, streak)
	}
	for _, p := range candidates {
		update(p, DetectAgent(snap, p.pid), streak)
	}
	return true
}

func update(p pane, agent string, streak map[string]int) {
	if agent == "" {
		if p.src == srcScrape {
			release(p.id)
		}
		return
	}
	screen, ok := tmux.RunTmux("capture-pane", "-p", "-t", p.id)
	if !ok {
		return
	}
	if p.agent != agent || p.src != srcScrape {
		tmux.SetPaneOption(p.id, tmux.PaneAgent, agent)
		tmux.SetPaneOption(p.id, tmux.PaneAgentSrc, srcScrape)
	}

	next := Classify(screen)
	old := tmux.PaneStatus(p.status)
	if next == tmux.StatusIdle && old == tmux.StatusRunning {
		streak[p.id]++
		if streak[p.id] < idleTicks {
			return
		}
	}
	streak[p.id] = 0
	hook.Transition(p.id, agent, old, next)
}

// checkInterrupted covers the one gap hooks leave: an agent interrupted by
// the user (Esc / Ctrl+C) fires no Stop hook, so the pane would stay
// `running` forever. Only flips when the screen shows an "Interrupted" marker
// and no working indicator, for idleTicks polls in a row.
func checkInterrupted(p pane, streak map[string]int) {
	screen, ok := tmux.RunTmux("capture-pane", "-p", "-t", p.id)
	if !ok || !IsInterrupted(screen) {
		streak[p.id] = 0
		return
	}
	if streak[p.id]++; streak[p.id] < idleTicks {
		return
	}
	streak[p.id] = 0
	hook.Transition(p.id, p.agent, tmux.StatusRunning, tmux.StatusIdle)
}

// checkAnswered covers the other gap: a permission prompt answered (from
// the pane or the sidebar) fires no hook until the approved tool finishes
// (PostToolUse), and a "No" fires none at all — the pane would keep saying
// "needs approval". Once the dialog is off screen for idleTicks polls, the
// pane follows the screen: running if the agent works on, idle otherwise.
func checkAnswered(p pane, streak map[string]int) {
	key := "wait:" + p.id
	screen, ok := tmux.RunTmux("capture-pane", "-p", "-t", p.id)
	next := Classify(screen)
	if !ok || next == tmux.StatusWaiting {
		delete(streak, key)
		return
	}
	if streak[key]++; streak[key] < idleTicks {
		return
	}
	delete(streak, key)
	hook.Transition(p.id, p.agent, tmux.StatusWaiting, next)
}

// checkExited covers an agent killed without a SessionEnd hook (SIGKILL,
// crash, closed terminal): its process is gone but the pane still carries
// the hook's state. Two absent polls in a row run the SessionEnd teardown.
func checkExited(p pane, snap *process.Snapshot, streak map[string]int) {
	key := "exit:" + p.id
	if DetectAgent(snap, p.pid) != "" {
		delete(streak, key)
		return
	}
	if streak[key]++; streak[key] < idleTicks {
		return
	}
	delete(streak, key)
	hook.RunSessionEndTeardown(p.id)
}

// knownAgent: only agents DetectAgent can find may be declared exited.
func knownAgent(label string) bool {
	for _, a := range agents {
		if a == label {
			return true
		}
	}
	return false
}

// IsInterrupted: idle screen with an "Interrupted" line near the bottom.
func IsInterrupted(screen string) bool {
	if Classify(screen) != tmux.StatusIdle {
		return false
	}
	lines := strings.Split(strings.TrimRight(screen, "\n "), "\n")
	// The marker sits right above the input box (box + footer ≈ 5 lines);
	// further up it's an old interrupt, the agent has moved on since.
	for i, n := len(lines)-1, 0; i >= 0 && n < interruptLines; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		n++
		if interruptedRe.MatchString(lines[i]) {
			return true
		}
	}
	return false
}

// release drops every option the scraper wrote, once the agent is gone.
func release(id string) {
	for _, k := range []string{tmux.PaneAgent, tmux.PaneAgentSrc, tmux.PaneStatusOpt, tmux.PaneUnseen, tmux.PaneAttention, tmux.PaneStartedAt, tmux.PaneWaitReason} {
		tmux.UnsetPaneOption(id, k)
	}
}

// refreshClients redraws every attached client's status line.
func refreshClients() {
	out, ok := tmux.RunTmux("list-clients", "-F", "#{client_name}")
	if !ok {
		return
	}
	for _, c := range strings.Fields(out) {
		tmux.RunTmux("refresh-client", "-S", "-t", c)
	}
}

// deliveredAt keeps a just-messaged pane from getting a second message
// before its hook/scraper reports it busy.
var deliveredAt = map[string]time.Time{}

// deliverQueued types the next queued message (internal/queue) into every
// agent pane that is idle again. listPanes is this tick's list-panes output.
func deliverQueued(listPanes string) {
	waiting := queue.Panes()
	if len(waiting) == 0 {
		return
	}
	status := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(listPanes), "\n") {
		f := strings.SplitN(line, "|", 8)
		if len(f) >= 8 && f[3] != "" {
			status[f[0]] = f[5]
		}
	}
	for _, p := range waiting {
		st, alive := status[p]
		switch {
		case !alive:
			if _, ok := tmux.PaneSessionName(p); !ok {
				queue.Clear(p) // the pane is gone
			}
		case st == string(tmux.StatusIdle) && time.Since(deliveredAt[p]) > 3*time.Second:
			if msg, ok := queue.Pop(p); ok {
				deliveredAt[p] = time.Now()
				tmux.TypeLine(p, msg)
			}
		}
	}
}
