package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// GetOption must NOT TrimSpace: leading/trailing whitespace is real data; it
// only trims the exact "\n" character tmux always appends to the output of
// "show -gv".
func TestGetOptionTrimsTrailingNewlineOnly(t *testing.T) {
	fake := NewFake()
	fake.Set(" right \n", "show", "-gv", SidebarPosition)
	fake.Set("\n", "show", "-gv", SidebarWidth)
	defer SetRunner(fake)()

	got, ok := GetOption(SidebarPosition)
	if !ok || got != " right " {
		t.Fatalf("GetOption = (%q, %v), want (%q, true)", got, ok, " right ")
	}

	if _, ok := GetOption(SidebarWidth); ok {
		t.Fatal("empty option must return ok = false")
	}
}

// GetAllGlobalOptions uses "show -g" to get the list of keys. A value with
// no sign of being escaped (no \, ', or ") is used directly, without an extra command.
func TestGetAllGlobalOptionsUsesRawValueWhenNotEscaped(t *testing.T) {
	fake := NewFake()
	fake.Set("@sidebar_width 40\nstatus on\njunk-with-no-space\n", "show", "-g")
	defer SetRunner(fake)()

	opts := GetAllGlobalOptions()

	if opts["@sidebar_width"] != "40" {
		t.Fatalf("@sidebar_width = %q, want %q", opts["@sidebar_width"], "40")
	}
	if opts["status"] != "on" {
		t.Fatalf("status = %q, want %q", opts["status"], "on")
	}
	if _, ok := opts["junk-with-no-space"]; ok {
		t.Fatal("a line with no space must be skipped")
	}
	// No sign of escaping → no extra "show -gv" command is called.
	if len(fake.Calls()) != 1 {
		t.Fatalf("want exactly 1 command (show -g), got %d: %v", len(fake.Calls()), fake.Calls())
	}
}

// A value with a sign of being escaped (containing \, ', or ") on a "show -g"
// line must be re-queried with "show -gv" — TMUX ITSELF decodes it, we don't
// write our own parser.
func TestGetAllGlobalOptionsRequeriesWhenEscaped(t *testing.T) {
	fake := NewFake()
	fake.Set("@sidebar_filter \"running\"\n", "show", "-g")
	fake.Set("running\n", "show", "-gv", "@sidebar_filter")
	defer SetRunner(fake)()

	opts := GetAllGlobalOptions()

	if opts["@sidebar_filter"] != "running" {
		t.Fatalf("@sidebar_filter = %q, want %q", opts["@sidebar_filter"], "running")
	}
	calls := fake.Calls()
	if len(calls) != 2 {
		t.Fatalf("want 2 commands (show -g then show -gv), got %d: %v", len(calls), calls)
	}
	if calls[1][0] != "show" || calls[1][1] != "-gv" || calls[1][2] != "@sidebar_filter" {
		t.Fatalf("wrong requery command: %v", calls[1])
	}
}

// When the requery command fails (rare — the option was just unset between
// the two commands), keep the raw escaped value as a fallback instead of
// losing the key entirely.
func TestGetAllGlobalOptionsKeepsRawValueWhenRequeryFails(t *testing.T) {
	fake := NewFake()
	fake.Set("@sidebar_filter \"running\"\n", "show", "-g")
	// Deliberately NOT declaring "show -gv @sidebar_filter" → Fake returns an error.
	defer SetRunner(fake)()

	opts := GetAllGlobalOptions()

	if opts["@sidebar_filter"] != `"running"` {
		t.Fatalf("@sidebar_filter = %q, want the raw value %q kept when requery fails",
			opts["@sidebar_filter"], `"running"`)
	}
}

func TestGetAllGlobalOptionsEmptyOnError(t *testing.T) {
	defer SetRunner(NewFake())()

	if got := GetAllGlobalOptions(); len(got) != 0 {
		t.Fatalf("want empty map when tmux errors, got %v", got)
	}
}

func TestSetAndUnsetPaneOptionArgs(t *testing.T) {
	fake := NewFake()
	fake.Set("", "set", "-t", "%2", "-p", PaneStatusOpt, "running")
	fake.Set("", "set", "-t", "%2", "-p", "-u", PaneStatusOpt)
	defer SetRunner(fake)()

	SetPaneOption("%2", PaneStatusOpt, "running")
	UnsetPaneOption("%2", PaneStatusOpt)

	calls := fake.Calls()
	if len(calls) != 2 {
		t.Fatalf("want 2 commands, got %d: %v", len(calls), calls)
	}
	if calls[0][4] != PaneStatusOpt || calls[0][5] != "running" {
		t.Fatalf("wrong set command: %v", calls[0])
	}
	if calls[1][4] != "-u" || calls[1][5] != PaneStatusOpt {
		t.Fatalf("wrong unset command: %v", calls[1])
	}
}

func TestGetPaneOptionValueEmptyOnError(t *testing.T) {
	defer SetRunner(NewFake())()

	if got := GetPaneOptionValue("%2", PaneStatusOpt); got != "" {
		t.Fatalf("want empty string when tmux errors, got %q", got)
	}
}

// TestGetOptionAndGetAllGlobalOptionsAgreeOnRealTmux locks down the
// consistency between the two paths for reading global options on REAL tmux
// (not Fake) — Fake only simulates what we've already assumed, it doesn't
// catch how real tmux 3.7b actually escapes things. Before the fix,
// GetAllGlobalOptions wrote its own unescapeTmuxValue to reverse how
// "show -g" quotes/escapes, but it couldn't reverse C-style control
// character escapes (\n, \t…), so it disagreed with GetOption (show -gv,
// raw value) — exactly the example the reviewer gave:
// "multi\nline" → GetOption yields a real newline, GetAllGlobalOptions
// yields "multinline".
//
// The test sets a batch of tricky values (newline, tab, CR, both quote
// kinds, backslash, "|", leading/trailing whitespace, empty) as global
// options on a real tmux server, then asserts GetOption and
// GetAllGlobalOptions[key] are byte-for-byte equal for every value.
//
// GetOption/GetAllGlobalOptions call bare "tmux" (no separate -L), so to
// avoid touching the real tmux server of whoever is running the test (if
// they're sitting inside a tmux session), the test isolates itself using
// the exact mechanism assets/fixtures/scenarios/common/_lib.sh uses: unset
// TMUX, then point TMUX_TMPDIR at a dedicated temp directory, so that the
// bare "tmux" call from this test process always lands on a fresh,
// isolated server.
//
// Skipped when the machine has no tmux.
func TestGetOptionAndGetAllGlobalOptionsAgreeOnRealTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux on this machine, skipping integration test")
	}

	if tmuxVar, had := os.LookupEnv("TMUX"); had {
		os.Unsetenv("TMUX")
		t.Cleanup(func() { os.Setenv("TMUX", tmuxVar) })
	}
	t.Setenv("TMUX_TMPDIR", t.TempDir())

	defer SetRunner(execRunner{})()

	if out, err := exec.Command("tmux", "new-session", "-d").CombinedOutput(); err != nil {
		t.Fatalf("tmux new-session: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		exec.Command("tmux", "kill-server").Run()
	})

	cases := []string{
		"multi\nline",
		"tab\there",
		"cr\rhere",
		`double "quote"`,
		"single 'quote'",
		`back\slash`,
		"pipe|char",
		"  leading and trailing  ",
		"",
	}

	type row struct {
		value, gotOption, gotAll string
		ok                       bool
	}
	rows := make([]row, 0, len(cases))
	for i, value := range cases {
		key := fmt.Sprintf("@t_case_%d", i)
		if out, err := exec.Command("tmux", "set-option", "-g", key, value).CombinedOutput(); err != nil {
			t.Fatalf("tmux set-option %q: %v\n%s", value, err, out)
		}

		gotOption, ok := GetOption(key)
		all := GetAllGlobalOptions()
		gotAll, foundAll := all[key]

		rows = append(rows, row{value: value, gotOption: gotOption, gotAll: gotAll, ok: ok})

		if !foundAll {
			t.Errorf("original value = %q (key %s): GetAllGlobalOptions is missing this key", value, key)
			continue
		}
		if gotOption != gotAll {
			t.Errorf("original value = %q (key %s): GetOption = %q, GetAllGlobalOptions = %q — MISMATCH",
				value, key, gotOption, gotAll)
		}
	}

	t.Logf("comparison table of GetOption vs GetAllGlobalOptions on real tmux %s:", func() string {
		out, _ := exec.Command("tmux", "-V").Output()
		return strings.TrimSpace(string(out))
	}())
	for _, r := range rows {
		t.Logf("  orig=%-30q GetOption=(%q,ok=%v) GetAllGlobalOptions=%q", r.value, r.gotOption, r.ok, r.gotAll)
	}
}

// Option names are a contract with the agent's hook. A single wrong
// character is a silent breakage, so we pin them down with a test.
func TestOptionNamesAreContract(t *testing.T) {
	cases := map[string]string{
		PaneAgent:        "@pane_agent",
		PaneStatusOpt:    "@pane_status",
		PaneCwd:          "@pane_cwd",
		PanePrompt:       "@pane_prompt",
		PaneBgCmd:        "@pane_bg_cmd",
		PaneRole:         "@pane_role",
		PaneSubagents:    "@pane_subagents",
		SidebarCursor:    "@sidebar_cursor",
		SpawnedOption:    "@agent-sidebar-spawned",
		BGCmdPlaceholder: "(background shell)",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("constant = %q, contract is %q", got, want)
		}
	}
}

// SetOptionResult returns the actual write outcome — success when Runner
// doesn't error, failure when it does (unlike SetOption, which swallows it).
func TestSetOptionResultReturnsWriteOutcome(t *testing.T) {
	fake := NewFake()
	fake.Set("", "set", "-g", SidebarCursor, "3")
	defer SetRunner(fake)()

	if ok := SetOptionResult(SidebarCursor, "3"); !ok {
		t.Fatal("SetOptionResult must return true when the set command succeeds")
	}

	fake.SetErr(fmt.Errorf("no such option"), "set", "-g", SidebarCursor, "bad")
	if ok := SetOptionResult(SidebarCursor, "bad"); ok {
		t.Fatal("SetOptionResult must return false when the set command fails")
	}
}
