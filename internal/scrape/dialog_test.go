package scrape

import (
	"strings"
	"testing"
)

func TestParseDialogClaudeMenu(t *testing.T) {
	screen := `
 │ gofmt -l . ; go vet ./...
 This command requires approval
 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and don't ask again for: gofmt -l .
   3. No
 Esc to cancel · Tab to amend
`
	d, ok := ParseDialog(screen)
	if !ok || d.Question != "Do you want to proceed?" || len(d.Options) != 3 {
		t.Fatalf("dialog = %+v, %v", d, ok)
	}
	if d.Options[0] != (Option{"1", "Yes"}) || d.Options[2] != (Option{"3", "No"}) {
		t.Errorf("options = %+v", d.Options)
	}
}

func TestParseDialogYesNoAndNone(t *testing.T) {
	d, ok := ParseDialog("Apply this change? [y/n]\n")
	if !ok || len(d.Options) != 2 || d.Options[0].Key != "y" {
		t.Errorf("y/n = %+v %v", d, ok)
	}
	if _, ok := ParseDialog("all tests pass\n> \n"); ok {
		t.Error("no prompt, no dialog")
	}
	if got := AnswerKeys("y"); len(got) != 2 || got[1] != "Enter" {
		t.Errorf("y needs Enter: %v", got)
	}
	if got := AnswerKeys("2"); len(got) != 1 || got[0] != "2" {
		t.Errorf("digit picks directly: %v", got)
	}
}

// A prompt near the top of a tall, mostly empty pane must still be found.
func TestParseDialogTallPane(t *testing.T) {
	screen := " Do you want to proceed?\n ❯ 1. Yes\n   2. No\n" + strings.Repeat("\n", 60)
	if d, ok := ParseDialog(screen); !ok || len(d.Options) != 2 {
		t.Fatalf("dialog = %+v %v", d, ok)
	}
}
