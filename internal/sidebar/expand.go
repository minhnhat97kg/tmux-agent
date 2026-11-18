package sidebar

import (
	"fmt"
	"io"
	"os"
	"strings"

	"tmux-agent/internal/tmux"
)

// The sidebar pane has two modes: the narrow agent list (this package) and,
// expanded, the dashboard (`tmux-agent watch --docked`) in the same pane.
// Expanding ZOOMS the pane (resize-pane -Z) rather than widening it:
// widening squeezed the other panes of the window down to a column or two,
// while zoom leaves their size alone and unzoom restores the layout exactly.
// Switching respawns the pane with the other program; pane options
// (@pane_role=sidebar, …) survive a respawn.
const (
	// ExpandedOption marks an expanded sidebar pane, so the width re-pin hook
	// in agent-sidebar.conf leaves it alone.
	ExpandedOption = "@sidebar_expanded"
)

// SetExpanded switches sidebar pane between the list and the dashboard.
// Called from inside that very pane, the respawn kills the caller — it is
// the last thing done.
func SetExpanded(pane string, expanded bool) {
	bin, err := os.Executable()
	if err != nil || bin == "" {
		bin = selfBinaryFallback
	}
	windowID := tmux.DisplayMessage(pane, "#{window_id}")
	zoomed := tmux.DisplayMessage(windowID, "#{window_zoomed_flag}") == "1"
	if expanded {
		tmux.SetPaneOption(pane, ExpandedOption, "1")
		// The dashboard has no SIGUSR1 handler; a stale pid must not be
		// signalled by the focus hooks.
		tmux.UnsetPaneOption(pane, tmux.SidebarPID)
		tmux.RunTmux("select-pane", "-t", pane)
		if !zoomed {
			tmux.RunTmux("resize-pane", "-Z", "-t", pane)
		}
		tmux.RunTmux("respawn-pane", "-k", "-t", pane, shellQuote(bin)+" watch --docked")
		return
	}
	tmux.UnsetPaneOption(pane, ExpandedOption)
	if zoomed {
		tmux.RunTmux("resize-pane", "-Z", "-t", pane) // unzoom: layout as it was
	}
	tmux.RunTmux("select-pane", "-t", windowID, "-l") // back to where the user was
	tmux.RunTmux("respawn-pane", "-k", "-t", pane, shellQuote(bin))
}

// ExpandToggle implements `tmux-agent expand [window_id]` (prefix + A):
// expand the window's sidebar — creating it first if there is none — or
// collapse it when already expanded.
func ExpandToggle(args []string, stderr io.Writer) int {
	windowID := ""
	if len(args) > 0 {
		windowID = args[0]
	} else if out, ok := tmux.RunTmux("display-message", "-p", "#{window_id}"); ok {
		windowID = strings.TrimSpace(out)
	}
	if windowID == "" {
		fmt.Fprintln(stderr, "expand: no window")
		return 1
	}
	find := func() string {
		out, _ := tmux.RunTmux("list-panes", "-t", windowID, "-F", paneIDRoleFormat())
		id, _ := findExistingSidebar(out)
		return id
	}
	pane := find()
	if pane == "" {
		Toggle([]string{"--create-only", windowID, tmux.DisplayMessage(windowID, "#{pane_current_path}")})
		if pane = find(); pane == "" {
			fmt.Fprintln(stderr, "expand: could not create the sidebar")
			return 1
		}
	}
	SetExpanded(pane, tmux.GetPaneOptionValue(pane, ExpandedOption) != "1")
	return 0
}

// shellQuote single-quotes s for the respawn-pane shell command.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
