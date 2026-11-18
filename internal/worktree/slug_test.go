package worktree

import "testing"

// Control characters and whitespace collapse into a single "-".
func TestSlugifyHandlesControlAndZeroWidthChars(t *testing.T) {
	if got := Slugify("hello\tworld\n"); got != "hello-world" {
		t.Errorf("Slugify(hello\\tworld\\n) = %q, want hello-world", got)
	}
	if got := Slugify("tab\ttab"); got != "tab-tab" {
		t.Errorf("Slugify(tab\\ttab) = %q, want tab-tab", got)
	}
}

func TestPickUniqueSlugStopsAtLimit(t *testing.T) {
	// Artificial "nothing free": must try all maxCollisionAttempts+1 times
	// then give up.
	_, ok := PickUniqueSlug("slug", func(string) bool { return false })
	if ok {
		t.Error("PickUniqueSlug must give up when no candidate is free")
	}
}

func TestPickUniqueSlugPrefersFirstFreeCandidate(t *testing.T) {
	got, ok := PickUniqueSlug("slug", func(c string) bool { return c == "slug-3" })
	if !ok || got != "slug-3" {
		t.Errorf("PickUniqueSlug = (%q, %v), want (slug-3, true)", got, ok)
	}
}

func TestWorktreePathForRejectsAbsoluteWorktreeDir(t *testing.T) {
	if _, ok := WorktreePathFor("/repo", "foo", "/tmp/wt"); ok {
		t.Error("WorktreePathFor must reject an absolute worktreeDir")
	}
}

func TestWorktreePathForRejectsParentRelativeWorktreeDir(t *testing.T) {
	if _, ok := WorktreePathFor("/repo", "foo", "../wt"); ok {
		t.Error("WorktreePathFor must reject a worktreeDir containing \"..\"")
	}
}

func TestWorktreePathForUsesDefaultDirWhenEmpty(t *testing.T) {
	got, ok := WorktreePathFor("/repo", "foo", "")
	want := "/repo/.worktrees/foo"
	if !ok || got != want {
		t.Errorf("WorktreePathFor(empty) = (%q, %v), want (%q, true)", got, ok, want)
	}
}

func TestWorktreePathForUsesGivenDir(t *testing.T) {
	got, ok := WorktreePathFor("/repo", "foo", "wt")
	want := "/repo/wt/foo"
	if !ok || got != want {
		t.Errorf("WorktreePathFor(wt) = (%q, %v), want (%q, true)", got, ok, want)
	}
}
