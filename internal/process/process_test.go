package process

import (
	"reflect"
	"sort"
	"testing"
)

func TestFromPSOutputPreservesSpacedArgs(t *testing.T) {
	snap := FromPSOutput(
		"100 1 codex /Applications/Codex App/bin/codex --full-auto\n" +
			"101 100 sh sh -c wrapper\n")

	if got := snap.ChildrenOf[1]; !reflect.DeepEqual(got, []int{100}) {
		t.Fatalf("ChildrenOf[1] = %v, want [100]", got)
	}
	info, ok := snap.InfoByPID[100]
	if !ok {
		t.Fatal("missing pid 100")
	}
	if info.Comm != "codex" {
		t.Fatalf("Comm = %q, want %q", info.Comm, "codex")
	}
	// A path with spaces must be preserved as-is, not split into multiple
	// fields.
	if info.Args != "/Applications/Codex App/bin/codex --full-auto" {
		t.Fatalf("Args = %q", info.Args)
	}
}

func TestFromPSOutputSkipsRacLine(t *testing.T) {
	snap := FromPSOutput("not-a-number 1 sh sh\n\n200 abc sh sh\n300 1\n400 2 zsh zsh\n")

	if len(snap.InfoByPID) != 1 {
		t.Fatalf("InfoByPID = %v, want only pid 400 left", snap.InfoByPID)
	}
	if _, ok := snap.InfoByPID[400]; !ok {
		t.Fatal("missing pid 400")
	}
}

func TestDescendantsWalksTree(t *testing.T) {
	snap := &Snapshot{
		ChildrenOf: map[int][]int{1: {2, 3}, 2: {4}, 4: {5}},
		InfoByPID:  map[int]Info{},
	}

	seen := snap.Descendants([]int{1})

	var got []int
	for pid := range seen {
		got = append(got, pid)
	}
	sort.Ints(got)
	if !reflect.DeepEqual(got, []int{1, 2, 3, 4, 5}) {
		t.Fatalf("Descendants = %v, want [1 2 3 4 5]", got)
	}
}

func TestDescendantsHandlesCycle(t *testing.T) {
	// ps shouldn't produce a cycle, but if it does, this must not hang.
	snap := &Snapshot{
		ChildrenOf: map[int][]int{1: {2}, 2: {1}},
		InfoByPID:  map[int]Info{},
	}
	if len(snap.Descendants([]int{1})) != 2 {
		t.Fatal("want to see exactly 2 pids and no hang")
	}
}

func TestTreeHasAgentMatchesDescendant(t *testing.T) {
	snap := FromPSOutput("100 1 fish fish -c opencode\n101 100 opencode opencode\n")

	if !snap.TreeHasAgent([]int{100}, "opencode") {
		t.Error("want to find opencode in the subtree")
	}
	// NOTE: this test does NOT lock down that "opencode" appearing in
	// fish's ARGS (a token after -c) is not counted as a match — the true
	// result above comes from child 101, whose Comm IS EXACTLY "opencode",
	// so it stays true even if processMatchesAgent accidentally scanned all
	// of Args instead of just argv0. The "must not misidentify based on
	// args" property is actually locked down by
	// TestProcessMatchesAgentRequiresCommandName (the case comm="not-
	// opencode" contains "opencode" as a substring but must not match).
	if snap.TreeHasAgent([]int{100}, "codex") {
		t.Error("must not misidentify codex")
	}
}

func TestProcessMatchesAgentRequiresCommandName(t *testing.T) {
	cases := []struct {
		info  Info
		agent string
		want  bool
	}{
		{Info{Comm: "claude", Args: "/opt/homebrew/bin/claude --flag"}, "claude", true},
		// comm is "node" but argv[0] is a path to opencode.
		{Info{Comm: "node", Args: "/usr/local/bin/opencode"}, "opencode", true},
		{Info{Comm: "not-opencode", Args: "/usr/local/bin/not-opencode"}, "opencode", false},
		{Info{Comm: "sh", Args: ""}, "claude", false},
	}
	for _, c := range cases {
		if got := processMatchesAgent(c.info, c.agent); got != c.want {
			t.Errorf("processMatchesAgent(%+v, %q) = %v, want %v", c.info, c.agent, got, c.want)
		}
	}
}

func TestCommandLinesForTreeFallsBackToComm(t *testing.T) {
	snap := &Snapshot{
		ChildrenOf: map[int][]int{1: {2}},
		InfoByPID: map[int]Info{
			1: {Comm: "zsh", Args: ""},
			2: {Comm: "node", Args: "  node server.js  "},
		},
	}

	got := snap.CommandLinesForTree([]int{1})
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{"node server.js", "zsh"}) {
		t.Fatalf("CommandLinesForTree = %v", got)
	}
}

func TestCommandBasename(t *testing.T) {
	cases := map[string]string{
		"/usr/local/bin/claude": "claude",
		"claude":                "claude",
		"":                      "",
	}
	for in, want := range cases {
		if got := CommandBasename(in); got != want {
			t.Errorf("CommandBasename(%q) = %q, want %q", in, got, want)
		}
	}
}
