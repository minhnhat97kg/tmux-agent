// Two background goroutines for the sidebar: port scanning (10 seconds),
// session names (10 seconds) — port of the reference app/workers.rs, minus
// the git worker (revision 4a had a separate 2s git worker that ran
// CONTINUOUSLY regardless of whether anyone was watching — phase 6 dropped
// it, git only fetches WHEN the detail popup is open, see detail.go:
// detailFetchCmd, fired from RefreshMsg in model.go). The two remaining
// workers still need to run continuously because Ports is shown right on the
// list row, always visible, not waiting for anyone to open the popup.
//
// Each worker lives for the entire lifetime of the program, sending its own
// tea.Msg via p.Send() whenever new data is available, and stops cleanly
// when the done channel closes (the same goroutine pattern already used for
// SIGUSR1 in run.go). NO worker ever blocks Update — every lsof/ps command
// runs in the worker's own goroutine, not inside Update.
package sidebar

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"tmux-agent/internal/portscan"
	"tmux-agent/internal/process"
	"tmux-agent/internal/session"
	"tmux-agent/internal/tmux"
)

const (
	portScanPollInterval = 10 * time.Second
	sessionScanInterval  = 10 * time.Second
)

// startBackgroundWorkers starts the two background goroutines. done closes
// when the program exits (Run(), run.go) — the same channel used by the
// SIGUSR1 goroutine, no need for a separate stop mechanism per worker.
type messageSender interface{ Send(tea.Msg) }

func startBackgroundWorkers(p messageSender, done <-chan struct{}) {
	go portScanLoop(p, done)
	go sessionScanLoop(p, done)
}

// portScanLoop scans listening TCP ports every 10 seconds — it calls Scan()
// ONCE per round (not once per pane); Update maps the results back to each
// pane itself using the already-known PanePID (computePortsByPane). If
// lsof/ps aren't available on the machine (ok=false), this round is skipped,
// leaving the Model's portsByPane unchanged.
func portScanLoop(p messageSender, done <-chan struct{}) {
	ticker := time.NewTicker(portScanPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			listening, ok := portscan.Scan()
			if !ok {
				continue
			}
			snapshot, ok := process.Scan()
			if !ok {
				continue
			}
			p.Send(portScanMsg{Listening: listening, Snapshot: snapshot})
		}
	}
}

// sessionScanLoop scans the session names set by the agent
// (~/.claude/sessions/*.json) every 10 seconds.
func sessionScanLoop(p messageSender, done <-chan struct{}) {
	ticker := time.NewTicker(sessionScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			p.Send(sessionNamesMsg{Names: session.ScanSessionNames("")})
		}
	}
}

// portScanMsg is the result of one portScanLoop pass.
type portScanMsg struct {
	Listening portscan.ListeningPorts
	Snapshot  *process.Snapshot
}

// sessionNamesMsg is the result of one sessionScanLoop pass.
type sessionNamesMsg struct {
	Names map[string]string
}

// computePortsByPane computes the listening ports for EACH pane in sessions,
// from a process snapshot plus an already-scanned port table. A pane with no
// ports is absent from the returned map (no redundant empty entries).
func computePortsByPane(sessions []tmux.SessionInfo, listening portscan.ListeningPorts, snapshot *process.Snapshot) map[string][]int {
	if snapshot == nil {
		return nil
	}
	result := map[string][]int{}
	for _, s := range sessions {
		for _, w := range s.Windows {
			for _, p := range w.Panes {
				if p.PanePID == 0 {
					continue
				}
				if ports := portscan.ForPane(listening, snapshot, p.PanePID); len(ports) > 0 {
					result[p.PaneID] = ports
				}
			}
		}
	}
	return result
}
