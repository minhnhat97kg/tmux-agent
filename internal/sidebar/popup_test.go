package sidebar

import "testing"

// ─── RepoNames / ParseRepoFilter ────────────────────────────────────

func TestRepoNames_AlwaysHasAllFirst(t *testing.T) {
	got := RepoNames([]string{"alpha", "beta"})
	want := []string{"All", "alpha", "beta"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestRepoNames_EmptyRepoListStillHasAll(t *testing.T) {
	got := RepoNames(nil)
	if len(got) != 1 || got[0] != "All" {
		t.Fatalf("got %v, want only [\"All\"]", got)
	}
}

func TestParseRepoFilter_EmptyAndAllBothMeanAll(t *testing.T) {
	if ParseRepoFilter("") != RepoFilterAll {
		t.Fatalf("empty string must fall back to %q", RepoFilterAll)
	}
	if ParseRepoFilter("all") != RepoFilterAll {
		t.Fatalf("\"all\" must be %q", RepoFilterAll)
	}
}

func TestParseRepoFilter_RepoNameKeptAsIs(t *testing.T) {
	if got := ParseRepoFilter("my-app"); got != "my-app" {
		t.Fatalf("got %q, want %q", got, "my-app")
	}
}

// ─── Opening / closing the popup ────────────────────────────────────

func TestToggleRepo_FirstOpenPutsCursorOnAll(t *testing.T) {
	var p Popup
	p.ToggleRepo([]string{"alpha", "beta"}, RepoFilterAll)
	if !p.IsRepoOpen() {
		t.Fatal("popup must be open")
	}
	if got := p.RepoSelected(); got != 0 {
		t.Fatalf("cursor must be at position 0 (All), got %d", got)
	}
}

func TestToggleRepo_OpenPutsCursorOnActiveFilterRepo(t *testing.T) {
	var p Popup
	// The current filter is "beta" → the cursor must land exactly on "beta"
	// in the list ["All", "alpha", "beta"] = position 2.
	p.ToggleRepo([]string{"alpha", "beta"}, "beta")
	if got := p.RepoSelected(); got != 2 {
		t.Fatalf("got %d, want 2", got)
	}
}

func TestToggleRepo_FilteredRepoNoLongerExistsFallsBackToAll(t *testing.T) {
	var p Popup
	// "deleted-repo" is no longer in the repo list → falls back to position 0.
	p.ToggleRepo([]string{"alpha"}, "deleted-repo")
	if got := p.RepoSelected(); got != 0 {
		t.Fatalf("got %d, want 0", got)
	}
}

func TestToggleRepo_SecondCallClosesPopup(t *testing.T) {
	var p Popup
	p.ToggleRepo([]string{"alpha"}, RepoFilterAll)
	p.ToggleRepo([]string{"alpha"}, RepoFilterAll)
	if p.IsRepoOpen() {
		t.Fatal("calling Toggle a second time must close the popup")
	}
	if p.Kind() != PopupNone {
		t.Fatalf("Kind() must be PopupNone, got %v", p.Kind())
	}
}

func TestCloseRepo_ClosesOpenPopup(t *testing.T) {
	var p Popup
	p.ToggleRepo([]string{"alpha"}, RepoFilterAll)
	p.CloseRepo()
	if p.IsRepoOpen() {
		t.Fatal("popup must be closed after CloseRepo")
	}
}

// ─── Cursor clamps at the boundaries (no wraparound) ──────────────────────

func TestRepoNav_ClampsAtTopAndBottomOfList(t *testing.T) {
	var p Popup
	repos := []string{"alpha", "beta"} // displayed list: All, alpha, beta — 3 rows
	p.ToggleRepo(repos, RepoFilterAll)

	// Up while already at the top: no change.
	p.RepoNavUp()
	if got := p.RepoSelected(); got != 0 {
		t.Fatalf("must clamp at 0, got %d", got)
	}

	p.RepoNavDown(3)
	p.RepoNavDown(3)
	if got := p.RepoSelected(); got != 2 {
		t.Fatalf("got %d, want 2 (bottom of the list)", got)
	}

	// Further down while already at the bottom: no change.
	p.RepoNavDown(3)
	if got := p.RepoSelected(); got != 2 {
		t.Fatalf("must clamp at 2, got %d", got)
	}
}

func TestRepoNav_DoesNothingWhenPopupClosed(t *testing.T) {
	var p Popup
	p.RepoNavDown(5)
	p.RepoNavUp()
	if p.RepoSelected() != 0 || p.IsRepoOpen() {
		t.Fatal("navigation must have no effect while the popup is closed")
	}
}

// ─── Selecting (Enter) ────────────────────────────────────────────────────

func TestConfirmRepo_SelectingAllReturnsRepoFilterAll(t *testing.T) {
	var p Popup
	// Open the popup with the current filter set to "beta" → the cursor is
	// already placed on the "beta" row (position 2 in ["All","alpha","beta"]),
	// step back two positions to row 0 ("All") via RepoNavUp.
	p.ToggleRepo([]string{"alpha", "beta"}, "beta")
	p.RepoNavUp()
	p.RepoNavUp()
	filter, ok := p.ConfirmRepo([]string{"alpha", "beta"})
	if !ok {
		t.Fatal("confirmation must succeed")
	}
	if filter != RepoFilterAll {
		t.Fatalf("got %q, want %q", filter, RepoFilterAll)
	}
	if p.IsRepoOpen() {
		t.Fatal("popup must be closed after confirming")
	}
}

func TestConfirmRepo_SelectingSpecificRepo(t *testing.T) {
	var p Popup
	repos := []string{"alpha", "beta"}
	p.ToggleRepo(repos, RepoFilterAll)
	p.RepoNavDown(3) // All(0) → alpha(1) → beta(2)
	p.RepoNavDown(3)
	filter, ok := p.ConfirmRepo(repos)
	if !ok {
		t.Fatal("confirmation must succeed")
	}
	if filter != "beta" {
		t.Fatalf("got %q, want %q", filter, "beta")
	}
}

func TestConfirmRepo_NotOpenReturnsFalse(t *testing.T) {
	var p Popup
	filter, ok := p.ConfirmRepo([]string{"alpha"})
	if ok {
		t.Fatal("ConfirmRepo must return false when the popup is not open")
	}
	if filter != "" {
		t.Fatalf("filter must be empty, got %q", filter)
	}
}

// ─── Empty repo list ─────────────────────────────────────────────

func TestPopup_EmptyRepoListHasOnlyAll(t *testing.T) {
	var p Popup
	p.ToggleRepo(nil, RepoFilterAll)
	if !p.IsRepoOpen() {
		t.Fatal("the popup must still be able to open even with no repos")
	}
	if got := p.RepoSelected(); got != 0 {
		t.Fatalf("got %d, want 0", got)
	}
	// Navigation has no effect since there is only one row.
	p.RepoNavDown(1)
	if got := p.RepoSelected(); got != 0 {
		t.Fatalf("got %d, want 0 (only one row)", got)
	}
	filter, ok := p.ConfirmRepo(nil)
	if !ok || filter != RepoFilterAll {
		t.Fatalf("got (%q, %v), want (%q, true)", filter, ok, RepoFilterAll)
	}
}
