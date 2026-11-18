// Package clipboard generates an OSC 52 escape sequence to copy a string to
// the terminal's clipboard — used for the setup-command copy button in the
// sidebar.
//
// This package ONLY generates the escape string, it does not write to
// stdout/stderr itself. The sidebar runs inside bubbletea (the TUI path);
// writing directly to the terminal here would corrupt the frame currently
// being drawn. The caller (the bottom panel, Task 7) is responsible for
// queuing the returned string and flushing it to the terminal after the
// frame has finished drawing — the same pattern as the reference
// implementation's pending_osc52_copy / render_frame.
//
// The reference implementation (src/clipboard.rs) also has a copy path
// through the arboard library (local OS clipboard). This project does not
// add that dependency, so that path is dropped — ONLY the OSC 52 path is
// ported. OSC 52 is an escape sequence understood directly by the terminal,
// and works even when the sidebar runs over SSH (where arboard could only
// reach the SSH server machine's clipboard, useless to the user at the other
// end).
//
// Limitation: some terminals disable OSC 52 for security reasons (it lets a
// program inside the terminal write to the system clipboard). When
// disabled, the terminal silently drops the sequence — there is no error
// signal to detect it, the user just sees the copy button "not do anything".
// There is no way for the program to know in advance or confirm afterward.
package clipboard

import "encoding/base64"

// OSC52Sequence generates an OSC 52 escape sequence telling the terminal to
// copy text into the "c" (clipboard) selection. A terminal that does not
// understand OSC 52 will silently ignore the whole sequence.
//
// When the sidebar runs inside tmux, tmux automatically recognizes and
// handles OSC 52 emitted from its own pane — no need to additionally wrap it
// in a DCS passthrough (\x1bPtmux;...\x1b\\). Forwarding to an outer
// terminal (including over SSH) depends on tmux's `set-clipboard` option
// being "on" or "external"; when it is "off" tmux just swallows the
// sequence. The DCS wrap is only needed for the rare case of tmux nested
// inside tmux — and the reference implementation doesn't handle that case
// either, so this function skips it too.
//
// There is no length limit at this layer (the reference implementation
// doesn't truncate either); some terminals/tmux may impose their own payload
// size limit and silently drop an oversized sequence, the same kind of
// silence as when OSC 52 is disabled.
func OSC52Sequence(text string) string {
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07"
}
