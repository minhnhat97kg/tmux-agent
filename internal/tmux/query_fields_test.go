package tmux

import (
	"reflect"
	"strings"
	"testing"
)

func TestSplitTmuxFieldsUnescapesDelimiter(t *testing.T) {
	// tmux wraps every field in #{q:…}, so a pipe inside user content
	// arrives here as \| and must not count as a delimiter.
	got := splitTmuxFields(`one|two\|still-two|three`, '|')
	want := []string{"one", "two|still-two", "three"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitTmuxFields = %v, want %v", got, want)
	}
}

func TestSplitTmuxFieldsKeepsEmptyFields(t *testing.T) {
	got := splitTmuxFields("a||b|", '|')
	want := []string{"a", "", "b", ""}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitTmuxFields = %v, want %v", got, want)
	}
}

func TestSplitTmuxFieldsTrailingBackslash(t *testing.T) {
	got := splitTmuxFields(`a|b\`, '|')
	want := []string{"a", `b\`}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitTmuxFields = %v, want %v", got, want)
	}
}

// The field-index table and the format string must be edited together. A
// single field off by one is a silent breakage — every field after it reads
// the wrong cell. This test locks them together.
func TestPaneFormatFieldCountMatchesIndices(t *testing.T) {
	fields := strings.Split(paneFormat(), "|")

	if len(fields) != minSessionFields {
		t.Fatalf("paneFormat has %d fields, minSessionFields = %d", len(fields), minSessionFields)
	}
	if minPaneFields != minSessionFields-paneLineOffset {
		t.Fatalf("minPaneFields = %d, want %d", minPaneFields, minSessionFields-paneLineOffset)
	}
}

func TestPaneFormatFieldsAreQuotedAndOrdered(t *testing.T) {
	fields := strings.Split(paneFormat(), "|")

	for i, f := range fields {
		if !strings.HasPrefix(f, "#{q:") || !strings.HasSuffix(f, "}") {
			t.Errorf("field %d = %q, every field must be wrapped in #{q:…}", i, f)
		}
	}

	// All 32 positions, in exact order. Locking down only 6 checkpoints
	// would let two fields swap places outside those checkpoints slip past
	// the test — exactly the kind of silent breakage the sf*/pf* index
	// table must catch.
	want := []string{
		"#{q:session_name}",
		"#{q:window_id}",
		"#{q:window_index}",
		"#{q:window_name}",
		"#{q:window_active}",
		"#{q:automatic-rename}",
		"#{q:session_attached}",
		"#{q:pane_active}",
		"#{q:" + PaneStatusOpt + "}",
		"#{q:" + PaneAttention + "}",
		"#{q:" + PaneAgent + "}",
		"#{q:" + PaneName + "}",
		"#{q:pane_current_path}",
		"#{q:pane_current_command}",
		"#{q:" + PaneRole + "}",
		"#{q:pane_id}",
		"#{q:" + PanePrompt + "}",
		"#{q:" + PanePromptSource + "}",
		"#{q:" + PaneStartedAt + "}",
		"#{q:" + PaneWaitReason + "}",
		"#{q:pane_pid}",
		"#{q:" + PaneSubagents + "}",
		"#{q:" + PaneCwd + "}",
		"#{q:" + PanePermissionMode + "}",
		"#{q:" + PaneWorktreeName + "}",
		"#{q:" + PaneWorktreeBranch + "}",
		"#{q:" + PaneSessionID + "}",
		"#{q:" + SpawnedOption + "}",
		"#{q:" + PaneBgCmd + "}",
		"#{q:" + PaneUnseen + "}",
		"#{q:" + PaneStuck + "}",
		"#{q:" + PaneConflict + "}",
	}
	if len(fields) != len(want) {
		t.Fatalf("paneFormat has %d fields, want %d", len(fields), len(want))
	}
	for idx, w := range want {
		if fields[idx] != w {
			t.Errorf("field %d = %q, want %q", idx, fields[idx], w)
		}
	}
}
