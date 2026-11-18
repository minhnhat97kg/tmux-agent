package roster

import "testing"

func TestSaveLoadMissing(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	saved := []Agent{
		{Name: "api", Agent: "claude", Cwd: "/r", SessionID: "s1", Role: "tester"},
		{Agent: "codex", Cwd: "/r2"},
		{Name: "docs", Agent: "pi", Cwd: "/r"},
	}
	if err := Save(saved); err != nil {
		t.Fatal(err)
	}
	if err := Save(nil); err != nil {
		t.Fatal(err)
	}
	snap, err := Load()
	if err != nil || len(snap.Agents) != 3 {
		t.Fatalf("an empty save must not erase the roster: %+v %v", snap, err)
	}
	running := []Agent{{Name: "docs", Agent: "pi", Cwd: "/r"}, {Agent: "codex", Cwd: "/r2"}}
	miss := Missing(snap.Agents, running)
	if len(miss) != 1 || miss[0].Name != "api" {
		t.Fatalf("missing = %+v", miss)
	}
	if args := ResumeArgs(miss[0]); len(args) != 2 || args[1] != "s1" {
		t.Errorf("claude resumes its session: %v", args)
	}
}
