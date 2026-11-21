package sharedctx

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddListRemoveClear(t *testing.T) {
	t.Setenv("TMUX_AGENT_CONTEXT_DIR", t.TempDir())
	const sc = "proj-1234"

	a, err := Add(sc, "reviewer", "%1", "  API uses snake_case  ")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Add(sc, "builder", "%2", "editing go.mod, stay out"); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(Global, "x", "%3", "global note"); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(sc, "x", "%3", "   "); err == nil {
		t.Error("empty note must be rejected")
	}

	got := List(sc)
	if len(got) != 2 || got[0].Text != "API uses snake_case" || got[1].Author != "builder" {
		t.Fatalf("List = %+v", got)
	}
	if len(List(Global)) != 1 {
		t.Error("boards must be separate")
	}
	if err := Remove(sc, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := Remove(sc, "../etc"); err == nil {
		t.Error("path-like id must be rejected")
	}
	if n := Clear(sc, "nobody"); n != 0 {
		t.Errorf("Clear(nobody) removed %d", n)
	}
	if n := Clear(sc, ""); n != 1 || len(List(sc)) != 0 {
		t.Errorf("Clear all removed %d, left %d", n, len(List(sc)))
	}
}

func TestScopeIsRepoRoot(t *testing.T) {
	repo := t.TempDir()
	if err := exec.Command("git", "-C", repo, "init", "-q").Run(); err != nil {
		t.Skip("git unavailable")
	}
	sub := filepath.Join(repo, "a", "b")
	os.MkdirAll(sub, 0o755)
	if Scope(sub) != Scope(repo) {
		t.Errorf("subdir scope %q != repo scope %q", Scope(sub), Scope(repo))
	}
	if !strings.HasPrefix(Scope(repo), filepath.Base(repo)+"-") {
		t.Errorf("scope %q should start with the repo name", Scope(repo))
	}
	other := t.TempDir()
	if Scope(other) == Scope(repo) {
		t.Error("different dirs must get different scopes")
	}
}

func TestListVisibleIncludesGlobalAndPreservesScope(t *testing.T) {
	t.Setenv("TMUX_AGENT_CONTEXT_DIR", t.TempDir())
	project, err := Add("project", "a", "%1", "project note")
	if err != nil {
		t.Fatal(err)
	}
	global, err := Add(Global, "b", "%2", "global note")
	if err != nil {
		t.Fatal(err)
	}
	got := ListVisible("project")
	if len(got) != 2 || got[0].ID != project.ID || got[0].Scope != "project" || got[1].ID != global.ID || got[1].Scope != Global {
		t.Fatalf("visible notes = %+v", got)
	}
	if got := ListVisible(Global); len(got) != 1 || got[0].ID != global.ID {
		t.Fatalf("global notes = %+v", got)
	}
}
