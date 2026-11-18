package sidebar

import (
	"os"
	"os/signal"
	"strconv"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"tmux-agent/internal/tmux"
)

// Run starts the sidebar TUI — invoked when the binary runs with no
// arguments (the sidebar pane created by the toggle command). Does NOT
// enable alt-screen (the sidebar is an ordinary tmux pane, not a full-screen
// app). Mouse is enabled for the expand button / row clicks / wheel. Prints NOTHING to stdout/stderr on this code path — it would wreck
// the sidebar pane's own screen.
func Run() int {
	tmuxPane := os.Getenv("TMUX_PANE")

	// Write this process's pid into its own pane's @sidebar_pid — the tmux
	// hook in agent-sidebar.conf looks up the process via this option to
	// send it SIGUSR1 when pane/window focus changes.
	if tmuxPane != "" {
		tmux.SetPaneOption(tmuxPane, tmux.SidebarPID, strconv.Itoa(os.Getpid()))
	}

	// Mouse: the clickable expand button, row selection, wheel scrolling
	// (handleMouse, model.go).
	model := NewModel(tmuxPane)
	if tmuxPane != "" {
		model.windowID = tmux.DisplayMessage(tmuxPane, "#{window_id}")
	}
	p := tea.NewProgram(model, tea.WithMouseCellMotion())

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGUSR1)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-sigCh:
				p.Send(refreshRequestMsg{})
			case <-done:
				return
			}
		}
	}()

	// The two background workers (port scan every 10s, session name scan
	// every 10s — workers.go) SHARE the same done channel with the SIGUSR1
	// goroutine above: closing it once on exit cleanly stops all three
	// goroutines, no need for a separate mechanism per worker. The git
	// worker was removed in part 6 — see workers.go.
	startBackgroundWorkers(p, done)

	_, err := p.Run()

	signal.Stop(sigCh)
	close(done)

	if err != nil {
		return 1
	}
	return 0
}
