// Scrolls the viewport to follow the cursor — pays off debt from 3a
// (Minor 3): when the list is taller than the pane's height, the cursor must
// always stay within the visible area.
//
// Ported from the reference Rust implementation: ScrollState + Scroll
// (src/state/scroll.rs, 94 lines) is the shared offset-clamping frame for
// every line-list-style view. The "follow the cursor" mechanism
// (EnsureVisible) doesn't live in scroll.rs — it lives wherever ScrollState
// is invoked for the pane list, compute_scroll_offset (src/ui/panes.rs:
// 426-451): compare the cursor row against [offset, offset+visible) and pull
// the offset back so that row is shown.
package sidebar

// ScrollState is the scroll position of a single-line-list view (each row
// is exactly one line — the sidebar has no multi-line rows like the
// reference implementation does).
type ScrollState struct {
	Offset        int
	TotalLines    int
	VisibleHeight int
}

// maxOffset is the largest still-valid offset: never scroll past the
// remaining content, and never negative when the content is shorter than
// the viewport.
func (s *ScrollState) maxOffset() int {
	max := s.TotalLines - s.VisibleHeight
	if max < 0 {
		return 0
	}
	return max
}

// Scroll shifts the offset by delta (negative scrolls up), clamped to
// [0, maxOffset()]. Ported verbatim from ScrollState::scroll (scroll.rs:8-13).
func (s *ScrollState) Scroll(delta int) {
	next := s.Offset + delta
	max := s.maxOffset()
	switch {
	case next < 0:
		next = 0
	case next > max:
		next = max
	}
	s.Offset = next
}

// EnsureVisible updates TotalLines/VisibleHeight then pulls Offset so that
// row `line` (0-indexed, the current cursor position in the row list)
// always stays within the viewport [Offset, Offset+VisibleHeight). Ports
// compute_scroll_offset (ui/panes.rs:426-451), simplified for the case where
// each row is exactly one line (the reference implementation has multi-line
// rows so it has to track first/last separately; here line is both first
// and last).
//
//   - Cursor moves above the top of the viewport: pull offset back to
//     line-1 (leaving one line of context above) rather than exactly line —
//     matches the reference implementation's `first.saturating_sub(1)`.
//   - Cursor moves past the bottom: push offset so line becomes the last
//     visible line.
//   - Cursor is already inside the viewport: don't change offset, just
//     re-clamp it (in case the list just got shorter and the old offset now
//     points outside it).
func (s *ScrollState) EnsureVisible(line, totalLines, visibleHeight int) {
	s.TotalLines = totalLines
	s.VisibleHeight = visibleHeight
	if visibleHeight <= 0 {
		return
	}
	switch {
	case line < s.Offset:
		s.Offset = line - 1
	case line >= s.Offset+visibleHeight:
		s.Offset = line + 1 - visibleHeight
	}
	s.Scroll(0) // re-clamp into [0, maxOffset()] even though offset was just assigned directly
}
