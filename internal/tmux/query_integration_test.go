package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestPaneFormatRoundTripsPipeCharacterOnRealTmux sets up a REAL tmux
// session (not Fake), sets @pane_prompt to contain a '|' character, then
// queries through paneFormat() + splitTmuxFields and asserts the pipe
// doesn't shift the fields.
//
// This guards the most silently-breakable part of the query layer:
// paneFormat wraps every field in #{q:…} on the assumption that tmux
// escapes every special character (pipe, backslash, both quote kinds, %)
// as \<char> in user values. Verified for real on tmux 3.7b:
//
//	set -p @pane_prompt 'a|b\c"d'"'"'e'
//	list-panes -F '#{q:@pane_prompt}' → a\|b\\c\"d\'e
//
// If some tmux version doesn't escape '|' this way, a prompt containing '|'
// will shift every field after it — no error is reported, just a silently
// wrong badge/color. The minimum required version lives in README.md.
//
// Skipped when the machine has no tmux.
func TestPaneFormatRoundTripsPipeCharacterOnRealTmux(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux on this machine, skipping integration test")
	}

	socket := fmt.Sprintf("tmux-agent-pipe-test-%d", os.Getpid())
	run := func(args ...string) string {
		t.Helper()
		full := append([]string{"-L", socket}, args...)
		out, err := exec.Command("tmux", full...).Output()
		if err != nil {
			t.Fatalf("tmux %v: %v", args, err)
		}
		return string(out)
	}
	t.Cleanup(func() {
		exec.Command("tmux", "-L", socket, "kill-server").Run()
	})

	paneID := strings.TrimSpace(run("new-session", "-d", "-P", "-F", "#{pane_id}"))
	if paneID == "" {
		t.Fatal("new-session did not return a pane id")
	}

	// Contains a pipe, a backslash, and both quote kinds — every #{q:}
	// character must be escaped so it doesn't break splitTmuxFields' structure.
	const promptWithSpecials = `question | has "quotes" and \a backslash | and 'single quotes'`
	run("set", "-t", paneID, "-p", PanePrompt, promptWithSpecials)
	run("set", "-t", paneID, "-p", PaneWaitReason, "after-pipe")

	out := run("list-panes", "-F", paneFormat())
	line := strings.TrimRight(strings.SplitN(out, "\n", 2)[0], "\r\n")

	parts := splitTmuxFields(line, '|')
	if len(parts) < minSessionFields {
		t.Fatalf("only split into %d fields, want >= %d: %v", len(parts), minSessionFields, parts)
	}

	gotPrompt := parts[paneLineOffset+pfPrompt]
	if gotPrompt != promptWithSpecials {
		t.Fatalf("PanePrompt round-trip = %q, want %q", gotPrompt, promptWithSpecials)
	}
	// The field AFTER prompt in the format: if the pipe in the prompt isn't
	// escaped correctly, this field will read a fragment of the prompt
	// instead of its real value — this is the silent breakage this test
	// must catch.
	gotWaitReason := parts[paneLineOffset+pfWaitReason]
	if gotWaitReason != "after-pipe" {
		t.Fatalf("the field after prompt is shifted because the pipe wasn't escaped correctly: WaitReason = %q, want %q",
			gotWaitReason, "after-pipe")
	}
}
