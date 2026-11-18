package sidebar

import "testing"

// ─── Scroll (direct port of scroll.rs's tests) ─────────────────────────────

func TestScrollForwardWithinBounds(t *testing.T) {
	s := ScrollState{Offset: 0, TotalLines: 20, VisibleHeight: 5}
	s.Scroll(3)
	if s.Offset != 3 {
		t.Fatalf("Offset after Scroll(3) = %d, want 3", s.Offset)
	}
}

func TestScrollClampsToMaxOffset(t *testing.T) {
	// max = total_lines - visible_height = 15
	s := ScrollState{Offset: 10, TotalLines: 20, VisibleHeight: 5}
	s.Scroll(100)
	if s.Offset != 15 {
		t.Fatalf("Offset after Scroll(100) = %d, want 15 (clamped to total-visible)", s.Offset)
	}
}

func TestScrollClampsToZeroWhenGoingNegative(t *testing.T) {
	s := ScrollState{Offset: 2, TotalLines: 20, VisibleHeight: 5}
	s.Scroll(-50)
	if s.Offset != 0 {
		t.Fatalf("Offset after Scroll(-50) = %d, want 0", s.Offset)
	}
}

func TestScrollVisibleHeightExceedsTotalClampsToZero(t *testing.T) {
	// total_lines.saturating_sub(visible_height) = 0 — scrolling anywhere lands
	// on 0, no negative "underflow".
	s := ScrollState{Offset: 0, TotalLines: 3, VisibleHeight: 10}
	s.Scroll(5)
	if s.Offset != 0 {
		t.Fatalf("Offset after Scroll(5) with visible>total = %d, want 0", s.Offset)
	}
}

// ─── EnsureVisible (follows the cursor — debt 3a Minor 3) ──────────────────────

func TestEnsureVisibleLongListCursorBelowBottomScrolls(t *testing.T) {
	var s ScrollState
	s.EnsureVisible(0, 20, 5) // startup, cursor on the first row — no scroll needed yet
	if s.Offset != 0 {
		t.Fatalf("initial Offset = %d, want 0", s.Offset)
	}

	// j keeps moving the cursor down to row 7 — beyond the bottom of the
	// current viewport [0,5) — the offset must follow so row 7 stays visible.
	s.EnsureVisible(7, 20, 5)
	if s.Offset != 3 {
		t.Fatalf("Offset after cursor moves to row 7 = %d, want 3 (viewport [3,8))", s.Offset)
	}
	if !(7 >= s.Offset && 7 < s.Offset+s.VisibleHeight) {
		t.Fatalf("row 7 must be within the viewport [%d, %d)", s.Offset, s.Offset+s.VisibleHeight)
	}
}

func TestEnsureVisibleCursorAboveTopScrollsBack(t *testing.T) {
	s := ScrollState{Offset: 3, TotalLines: 20, VisibleHeight: 5} // viewport [3,8)

	// k moves the cursor up to row 1 — above the top of the viewport.
	s.EnsureVisible(1, 20, 5)
	if s.Offset != 0 {
		t.Fatalf("Offset after cursor moves to row 1 = %d, want 0", s.Offset)
	}
	if !(1 >= s.Offset && 1 < s.Offset+s.VisibleHeight) {
		t.Fatalf("row 1 must be within the viewport [%d, %d)", s.Offset, s.Offset+s.VisibleHeight)
	}
}

func TestEnsureVisibleShrinkingListClampsOffset(t *testing.T) {
	// Offset is at the end of a long list (20 lines, 5-line viewport: max = 15).
	s := ScrollState{Offset: 15, TotalLines: 20, VisibleHeight: 5}

	// The list shrinks to 8 lines, clampCursor moves the cursor to the last row (7).
	s.EnsureVisible(7, 8, 5)
	if s.Offset != 3 {
		t.Fatalf("Offset after the list shrinks = %d, want 3 (clamped to the new total-visible)", s.Offset)
	}
	if !(7 >= s.Offset && 7 < s.Offset+s.VisibleHeight) {
		t.Fatalf("row 7 must be within the viewport [%d, %d)", s.Offset, s.Offset+s.VisibleHeight)
	}
}

func TestEnsureVisibleCursorInsideViewportKeepsOffset(t *testing.T) {
	s := ScrollState{Offset: 2, TotalLines: 20, VisibleHeight: 5} // viewport [2,7)
	s.EnsureVisible(3, 20, 5)
	if s.Offset != 2 {
		t.Fatalf("Offset while the cursor is already within the viewport = %d, want 2 (unchanged)", s.Offset)
	}
}
