// Package sidebar: the repo-selection filter popup.
//
// Ported from src/state/popup.rs (the repo popup part) of the reference Rust
// implementation. The reference uses a single popup enum to guarantee "at
// most one popup open at a time" — popup state can never contradict itself.
// PopupKind here preserves that same property: Popup has only ONE "kind"
// field that decides which popup is open; part 4 will add a second
// PopupKind (notices) to this same enum.
package sidebar

// PopupKind is the type of popup currently open. PopupNone means no popup is
// open.
type PopupKind int

const (
	// PopupNone: no popup is open.
	PopupNone PopupKind = iota
	// PopupRepo: the repo-selection popup is open.
	PopupRepo
)

// RepoFilterAll is the value stored in @sidebar_repo_filter when not
// filtering by any repo ("all repos"). This is a contract with tmux.conf —
// copied verbatim from RepoFilter::as_str() / from_label() in
// src/state/filter.rs:83-97.
const RepoFilterAll = "all"

// ParseRepoFilter converts the string value read from @sidebar_repo_filter
// into the repo name to filter by. Both an empty string and "all" mean "no
// filter" (from_label in the reference implementation treats both as All) —
// returns RepoFilterAll; any other value is treated as a literal repo name.
func ParseRepoFilter(s string) string {
	if s == "" || s == RepoFilterAll {
		return RepoFilterAll
	}
	return s
}

// RepoNames builds the list of lines shown in the popup: "All" always comes
// first, followed by the names of repos that currently have agent panes, in
// the same order as the input. An empty repo list still returns ["All"] —
// that's what the popup shows when there are no repos (repo_names() in the
// reference implementation always includes the All sentinel,
// src/state/filter.rs:131-137).
func RepoNames(repos []string) []string {
	names := make([]string, 0, len(repos)+1)
	names = append(names, "All")
	names = append(names, repos...)
	return names
}

// Popup is the sidebar popup's state machine. Its fields are unexported so
// other code can only change state through the functions below — this keeps
// the "at most one popup open" invariant without scattered checks.
type Popup struct {
	kind     PopupKind
	selected int // cursor position in the repo popup; only meaningful when kind == PopupRepo
}

// Kind returns the type of popup currently open.
//
// Minor 3 (final review 3b): nothing outside the package calls Kind() right
// now — DELIBERATELY KEPT, unlike SetRepoSelected/ThemeFromTmux/
// IconsFromTmux which were removed. Part 4 (notice popups) adds a second
// PopupKind to this enum (see the file header comment); at that point we
// need to distinguish "which popup is open", which IsRepoOpen (only reports
// yes/no for PopupRepo specifically) can't do. Deleting it now and adding it
// back later would cost another review round for a one-line function.
func (p *Popup) Kind() PopupKind {
	return p.kind
}

// IsRepoOpen reports whether the repo popup is currently open.
func (p *Popup) IsRepoOpen() bool {
	return p.kind == PopupRepo
}

// RepoSelected returns the cursor position in the repo popup. Returns 0 if
// the repo popup isn't open (matches repo_popup_selected() in the reference
// implementation, popup.rs:111-116).
func (p *Popup) RepoSelected() int {
	if !p.IsRepoOpen() {
		return 0
	}
	return p.selected
}

// ToggleRepo opens or closes the repo popup (the `r` key).
//
// If it's open, closes it. If it's closed, opens it and places the cursor
// at the position of the current repo filter in the list (RepoFilterAll →
// position 0; a specific repo name → that name's position, or 0 if not
// found — e.g. the filtered repo has disappeared from the pane list because
// it was closed). Ported from toggle_repo_popup, popup.rs:131-146.
func (p *Popup) ToggleRepo(repos []string, currentFilter string) {
	if p.IsRepoOpen() {
		p.kind = PopupNone
		return
	}
	names := RepoNames(repos)
	selected := 0
	if currentFilter != RepoFilterAll {
		for i, n := range names {
			if n == currentFilter {
				selected = i
				break
			}
		}
	}
	p.kind = PopupRepo
	p.selected = selected
}

// CloseRepo closes the popup (regardless of its current state), without
// changing the filter. Ported from close_repo_popup, popup.rs:163-165 — the
// Esc key inside the popup.
func (p *Popup) CloseRepo() {
	p.kind = PopupNone
}

// RepoNavDown moves the cursor down one line, clamped at the last line (no
// wraparound). Does nothing if the repo popup isn't open. Ported from
// repo_popup_nav_down, app/input.rs:207-212 (the `j` / Down / Ctrl-n keys
// inside the popup).
func (p *Popup) RepoNavDown(count int) {
	if !p.IsRepoOpen() {
		return
	}
	if p.selected+1 < count {
		p.selected++
	}
}

// RepoNavUp moves the cursor up one line, clamped at the first line (no
// wraparound). Does nothing if the repo popup isn't open. Ported from
// repo_popup_nav_up, app/input.rs:214-218 (the `k` / Up / Ctrl-p keys inside
// the popup).
func (p *Popup) RepoNavUp() {
	if !p.IsRepoOpen() {
		return
	}
	if p.selected > 0 {
		p.selected--
	}
}

// ConfirmRepo confirms the current selection (the Enter key), closes the
// popup, and returns the value to write into @sidebar_repo_filter.
// ok == false if the repo popup isn't open — in that case there's nothing to
// write. The cursor on line 0 ("All") always returns RepoFilterAll;
// subsequent lines return the corresponding repo name. Ported from
// confirm_repo_popup, popup.rs:148-161.
func (p *Popup) ConfirmRepo(repos []string) (filter string, ok bool) {
	if !p.IsRepoOpen() {
		return "", false
	}
	names := RepoNames(repos)
	p.kind = PopupNone
	if p.selected < 0 || p.selected >= len(names) {
		return "", false
	}
	if p.selected == 0 {
		return RepoFilterAll, true
	}
	return names[p.selected], true
}
