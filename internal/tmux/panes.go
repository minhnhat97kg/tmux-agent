package tmux

import (
	"strconv"
	"strings"
)

// Default size used when tmux doesn't answer — the pane may have just
// closed. Not 0: the drawing layer divides by these values.
const (
	defaultSidebarWidth  = 28
	defaultSidebarHeight = 24
)

// PaneRef is the minimal info needed to locate any pane, including panes not
// running an agent (shell, editor). Unlike PaneInfo, which is only for agent panes.
type PaneRef struct {
	ID     string
	Active bool
	Path   string
}

// SidebarPaneInfo asks tmux about the sidebar pane itself.
func SidebarPaneInfo(tmuxPane string) (paneActive, windowActive bool, width, height int) {
	out := DisplayMessage(tmuxPane, "#{pane_active} #{window_active} #{pane_width} #{pane_height}")
	parts := strings.SplitN(out, " ", 4)
	if len(parts) < 4 {
		return false, false, defaultSidebarWidth, defaultSidebarHeight
	}

	width, err := strconv.Atoi(parts[2])
	if err != nil {
		width = defaultSidebarWidth
	}
	height, err = strconv.Atoi(parts[3])
	if err != nil {
		height = defaultSidebarHeight
	}
	return parts[0] == "1", parts[1] == "1", width, height
}

// PanePath returns a pane's current working directory.
func PanePath(paneID string) (string, bool) {
	p := DisplayMessage(paneID, "#{pane_current_path}")
	return p, p != ""
}

// ActiveWindowPanes returns every pane in the currently active window.
//
// Does NOT filter by agent: the activity tab and git tab need to work even
// when the focused pane is just an ordinary shell.
//
// IMPLICIT ASSUMPTION: the "list-panes" call here is made WITHOUT "-t", so
// tmux resolves "the current window" from the context of the calling process
// (the attached client / the TMUX_PANE env var) — not the active window of
// some specific session passed in. Any call site of this function (part 3,
// part 5, …) must run in that context (e.g. the sidebar process running
// inside its own pane) for "the current window" to be resolved by tmux the
// way we intend.
func ActiveWindowPanes() []PaneRef {
	out, ok := RunTmux("list-panes", "-F", "#{pane_id}|#{pane_active}|#{pane_current_path}")
	if !ok {
		return nil
	}

	var panes []PaneRef
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "|", 3)
		if len(parts) < 3 {
			continue
		}
		panes = append(panes, PaneRef{
			ID:     parts[0],
			Active: parts[1] == "1",
			Path:   parts[2],
		})
	}
	return panes
}

// FindActivePane returns the currently focused pane that isn't the sidebar.
func FindActivePane(sidebarPane string) (PaneRef, bool) {
	return pickActivePane(sidebarPane, ActiveWindowPanes())
}

// pickActivePane is the pure logic, split out so tests don't need tmux.
//
// ok = false when the sidebar itself is active or no valid pane was found —
// the caller keeps the previously focused pane instead of jumping to empty.
func pickActivePane(sidebarPane string, panes []PaneRef) (PaneRef, bool) {
	for _, p := range panes {
		if p.Active && p.ID != sidebarPane && p.Path != "" {
			return p, true
		}
	}
	return PaneRef{}, false
}

// FocusedPanePath returns the directory of the currently focused pane. The
// background git thread uses this function because it can't access the
// TUI's state.
func FocusedPanePath(sidebarPane string) (string, bool) {
	pane, ok := FindActivePane(sidebarPane)
	if !ok {
		return "", false
	}
	return pane.Path, true
}
