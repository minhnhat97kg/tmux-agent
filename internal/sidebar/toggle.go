// Package sidebar implements the command-line commands controlling the
// observer sidebar pane: toggle, toggle-all, auto-close. This part is purely
// tmux, not touching the TUI — a later task in part 3a adds the bubbletea
// loop (internal/sidebar holds both, split by file).
package sidebar

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"tmux-agent/internal/tmux"
)

// selfBinaryFallback is used when os.Executable() fails (rare) — the
// module's current binary name, not any particular command's name.
const selfBinaryFallback = "tmux-agent"

// Toggle implements the "toggle <window_id> [pane_path] [--create-only]"
// command: turns the observer sidebar pane in ONE window on/off. Already has
// a sidebar → kill it (except --create-only exits silently, used by the
// after-new-window hook so it doesn't touch a window that already has a
// sidebar); no sidebar yet → creates a new sidebar pane running this same
// binary, then returns focus to the pane that was active before.
//
// Always returns 0 — even when window_id is missing — because this command
// runs from a tmux key-binding/hook, not somewhere the user needs to see an
// error exit code. Ported from cmd_toggle in the reference Rust
// implementation (src/cli/toggle.rs).
func Toggle(args []string) int {
	createOnly := false
	var positional []string
	for _, a := range args {
		if a == "--create-only" {
			createOnly = true
		} else {
			positional = append(positional, a)
		}
	}
	if len(positional) == 0 {
		return 0
	}
	windowID := positional[0]
	panePath := "~"
	if len(positional) > 1 {
		panePath = positional[1]
	}

	sidebarWidth := resolveSidebarWidth(windowID)
	position := sidebarPositionFromSetting(tmux.DisplayMessage(windowID, fmt.Sprintf("#{%s}", tmux.SidebarPosition)))

	panesOutput, _ := tmux.RunTmux("list-panes", "-t", windowID, "-F", paneIDRoleFormat())
	if existing, ok := findExistingSidebar(panesOutput); ok {
		if createOnly {
			return 0
		}
		tmux.RunTmux("kill-pane", "-t", existing)
		return 0
	}

	geometryOutput, _ := tmux.RunTmux("list-panes", "-t", windowID, "-F", "#{pane_left} #{pane_width} #{pane_id}")
	targetPane, ok := targetPaneForPosition(geometryOutput, position)
	if !ok {
		targetPane = windowID
	}

	// Remember the currently active pane so focus can be restored after
	// split-window (split-window automatically switches focus to the newly
	// created pane).
	activePane := tmux.DisplayMessage(windowID, "#{pane_id}")

	selfBin, err := os.Executable()
	if err != nil || selfBin == "" {
		selfBin = selfBinaryFallback
	}

	sidebarPane, _ := tmux.RunTmux(
		"split-window",
		splitWindowFlagsFor(position),
		"-l", sidebarWidth,
		"-t", targetPane,
		"-c", panePath,
		"-P", "-F", "#{pane_id}",
		selfBin,
	)
	sidebarPane = strings.TrimSpace(sidebarPane)

	if sidebarPane != "" {
		tmux.SetPaneOption(sidebarPane, tmux.PaneRole, "sidebar")
	}

	if activePane != "" {
		tmux.SelectPane(activePane, windowID)
	} else {
		tmux.RunTmux("select-pane", "-t", windowID, "-l")
	}

	return 0
}

// ToggleAll implements the "toggle-all" command: turns the sidebar on/off in
// EVERY open window, no arguments. At least one sidebar exists somewhere →
// turn off every sidebar; no sidebar exists yet → create one in each window
// (each window gets its own pane path, using --create-only so a duplicate
// isn't created if some window already ended up with a sidebar between the
// two reads).
func ToggleAll(_ []string) int {
	panesOutput, _ := tmux.RunTmux("list-panes", "-a", "-F", paneIDRoleFormat())
	if anySidebarPane(panesOutput) {
		for _, line := range splitLines(panesOutput) {
			id, role, ok := strings.Cut(line, "|")
			if ok && role == "sidebar" {
				tmux.RunTmux("kill-pane", "-t", id)
			}
		}
		return 0
	}

	allPanes, _ := tmux.RunTmux("list-panes", "-a", "-F", "#{window_id}|#{pane_current_path}")
	for _, wp := range uniqueWindowPaths(allPanes) {
		Toggle([]string{"--create-only", wp.windowID, wp.path})
	}
	return 0
}

// AutoClose implements the "auto-close <window_id>" command: closes the
// window when only the sidebar pane is left in it — the exact condition
// lives in shouldKillWindow.
func AutoClose(args []string) int {
	if len(args) == 0 {
		return 0
	}
	windowID := args[0]

	listPanesOutput, ok := tmux.RunTmux("list-panes", "-t", windowID, "-F", fmt.Sprintf("#{%s}", tmux.PaneRole))
	var listPanesPtr *string
	if ok {
		listPanesPtr = &listPanesOutput
	}

	sessionWindows := parseIntOption(tmux.DisplayMessage(windowID, "#{session_windows}"))
	sessionAttached := parseIntOption(tmux.DisplayMessage(windowID, "#{session_attached}"))

	if shouldKillWindow(listPanesPtr, sessionWindows, sessionAttached) {
		tmux.KillWindow(windowID)
	}
	return 0
}

// ─── Pure functions: don't touch tmux, tests don't need Fake ─────────────

// resolveSidebarWidth reads @sidebar_width for a window: a plain integer is
// used as-is (defaults to "30" when unset), or a percentage (e.g. "15%") is
// computed against #{window_width} — the percentage defaults to 15, result
// is at least 1 column.
func resolveSidebarWidth(windowID string) string {
	setting := tmux.DisplayMessage(windowID, fmt.Sprintf("#{%s}", tmux.SidebarWidth))
	if setting == "" {
		setting = "30"
	}
	if !strings.HasSuffix(setting, "%") {
		return setting
	}

	windowWidth, err := strconv.Atoi(tmux.DisplayMessage(windowID, "#{window_width}"))
	if err != nil {
		windowWidth = 0
	}
	pct, err := strconv.Atoi(strings.TrimSuffix(setting, "%"))
	if err != nil {
		pct = 15
	}
	if windowWidth <= 0 || pct <= 0 {
		return setting
	}
	width := windowWidth * pct / 100
	if width < 1 {
		return "1"
	}
	return strconv.Itoa(width)
}

// sidebarPosition is which side tmux places the sidebar pane on, per
// @sidebar_position.
type sidebarPosition int

const (
	positionLeft sidebarPosition = iota
	positionRight
)

// sidebarPositionFromSetting parses the raw value of @sidebar_position. Only
// "right" (case-insensitive, surrounding whitespace trimmed) selects the
// right side; every other value — including empty or misspelled — defaults
// to the left, so a typo doesn't unexpectedly move the sidebar somewhere
// else.
func sidebarPositionFromSetting(setting string) sidebarPosition {
	if strings.EqualFold(strings.TrimSpace(setting), "right") {
		return positionRight
	}
	return positionLeft
}

// paneGeometry is a pane's horizontal position, parsed from a
// "#{pane_left} #{pane_width} #{pane_id}" line from list-panes.
type paneGeometry struct {
	left   int
	width  int
	paneID string
}

// parsePaneGeometry parses one list-panes line into a paneGeometry. ok=false
// for a malformed line, so the caller can simply skip it.
func parsePaneGeometry(line string) (paneGeometry, bool) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return paneGeometry{}, false
	}
	left, err := strconv.Atoi(fields[0])
	if err != nil {
		return paneGeometry{}, false
	}
	width, err := strconv.Atoi(fields[1])
	if err != nil {
		return paneGeometry{}, false
	}
	return paneGeometry{left: left, width: width, paneID: fields[2]}, true
}

// targetPaneForPosition picks which pane the sidebar pane will be split
// from: the leftmost pane for a left sidebar, or the pane with the largest
// right edge (left+width) for a right sidebar — always placing the new pane
// at the outermost edge of the window. On a tie, follows the same rule as
// Rust's min_by_key/max_by_key: a left sidebar keeps the FIRST line to reach
// the minimum, a right sidebar keeps the LAST line to reach the maximum.
// ok=false when none of the output's lines could be parsed.
func targetPaneForPosition(output string, position sidebarPosition) (string, bool) {
	var best paneGeometry
	found := false
	for _, line := range strings.Split(output, "\n") {
		geo, ok := parsePaneGeometry(line)
		if !ok {
			continue
		}
		if !found {
			best = geo
			found = true
			continue
		}
		switch position {
		case positionLeft:
			if geo.left < best.left {
				best = geo
			}
		case positionRight:
			if geo.left+geo.width >= best.left+best.width {
				best = geo
			}
		}
	}
	if !found {
		return "", false
	}
	return best.paneID, true
}

// splitWindowFlagsFor returns the split-window flags for each position:
// "-hfb" inserts the new pane BEFORE the target (to its left), "-hf" inserts
// it AFTER (to its right). Both "f" variants span the full window height.
func splitWindowFlagsFor(position sidebarPosition) string {
	if position == positionRight {
		return "-hf"
	}
	return "-hfb"
}

// windowPath pairs a window with the path of one pane inside it, used by
// toggle-all to create a sidebar in each window with the correct working
// directory.
type windowPath struct {
	windowID string
	path     string
}

// uniqueWindowPaths collapses "#{window_id}|#{pane_current_path}" from
// list-panes -a down to one line per window (keeping the first window_id
// encountered for each window, skipping malformed lines), preserving
// whitespace in the path.
func uniqueWindowPaths(output string) []windowPath {
	seen := map[string]bool{}
	var result []windowPath
	for _, line := range splitLines(output) {
		windowID, path, ok := strings.Cut(line, "|")
		if !ok {
			continue
		}
		if seen[windowID] {
			continue
		}
		seen[windowID] = true
		result = append(result, windowPath{windowID: windowID, path: path})
	}
	return result
}

// anySidebarPane reports true if the output of
// "list-panes -F <pane_id>|<role>" has at least one line with role
// "sidebar".
func anySidebarPane(output string) bool {
	for _, line := range splitLines(output) {
		_, role, ok := strings.Cut(line, "|")
		if ok && role == "sidebar" {
			return true
		}
	}
	return false
}

// findExistingSidebar finds the first pane id with role "sidebar" in the
// output of "list-panes -F <pane_id>|<role>".
func findExistingSidebar(output string) (string, bool) {
	for _, line := range splitLines(output) {
		id, role, ok := strings.Cut(line, "|")
		if ok && role == "sidebar" {
			return id, true
		}
	}
	return "", false
}

// paneIDRoleFormat is the list-panes format used to find the sidebar pane by
// id.
func paneIDRoleFormat() string {
	return fmt.Sprintf("#{pane_id}|#{%s}", tmux.PaneRole)
}

// shouldKillWindow decides whether auto-close should kill-window or not,
// given the raw results of the tmux queries AutoClose performs. Split out
// as a pure function so it's testable without a tmux server.
//
//   - listPanesOutput: stdout of "list-panes -F #{<pane role>}", or nil if
//     the tmux command failed.
//   - sessionWindows: the parsed value of #{session_windows}, or nil if the
//     command failed or couldn't be parsed.
//   - sessionAttached: the parsed value of #{session_attached}, or nil if
//     the command failed or couldn't be parsed.
func shouldKillWindow(listPanesOutput *string, sessionWindows *int, sessionAttached *int) bool {
	// list-panes failed or is empty: the window may have already disappeared,
	// or tmux is busy. Do NOT treat "no output" as "no other panes left" —
	// doing so could kill a still-alive window whose query just happened to
	// race with another tmux command.
	if listPanesOutput == nil {
		return false
	}
	if strings.TrimSpace(*listPanesOutput) == "" {
		return false
	}

	nonSidebar := 0
	for _, line := range splitLines(*listPanesOutput) {
		if line != "sidebar" {
			nonSidebar++
		}
	}
	if nonSidebar != 0 {
		return false
	}

	if sessionWindows == nil {
		return false
	}

	// The session's last window: killing it also kills the session and
	// kicks out every attached client. One attached client is fine — matches
	// tmux's usual behavior when exiting the last pane. Two or more clients
	// means the session is shared (multiple terminal tabs attached to
	// "main") and we can't tell which client "wants" to exit, so keep the
	// sidebar. Missing sessionAttached leans toward keeping it.
	switch *sessionWindows {
	case 0:
		return false
	case 1:
		return sessionAttached != nil && *sessionAttached <= 1
	default:
		return true
	}
}

// parseIntOption parses a display-message value into *int, nil when empty
// or unparseable — used for #{session_windows}/#{session_attached}.
func parseIntOption(s string) *int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return nil
	}
	return &n
}

// splitLines splits tmux output into lines, with the same semantics as
// Rust's str::lines(): a trailing empty line caused by a trailing "\n" in
// the string does NOT count as a line (unlike strings.Split, which always
// produces an extra empty element at the end when the string ends with the
// separator). Real tmux output always has a trailing "\n" (RunTmux doesn't
// trim it), so this difference determines whether shouldKillWindow is
// correct on real tmux, not just on test input.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
