package hook

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAppendSubagentToEmptyList(t *testing.T) {
	if got := AppendSubagent("", "Explore", "sub-1"); got != "Explore:sub-1" {
		t.Errorf("AppendSubagent = %q, want %q", got, "Explore:sub-1")
	}
}

func TestAppendSubagentToExistingList(t *testing.T) {
	got := AppendSubagent("Explore:sub-1", "Plan", "sub-2")
	want := "Explore:sub-1,Plan:sub-2"
	if got != want {
		t.Errorf("AppendSubagent = %q, want %q", got, want)
	}
}

// Two subagents of the SAME TYPE running in parallel must be two separate
// entries — the id distinguishes them, not the type.
func TestAppendSubagentSameTypeRunningInParallel(t *testing.T) {
	list := AppendSubagent("Explore:sub-1", "Explore", "sub-2")
	want := "Explore:sub-1,Explore:sub-2"
	if list != want {
		t.Errorf("AppendSubagent = %q, want %q", list, want)
	}
}

func TestRemoveSubagentEmptyList(t *testing.T) {
	if _, ok := RemoveSubagent("", "sub-1"); ok {
		t.Error("an empty list must return ok = false")
	}
}

func TestRemoveSubagentEmptyIDIsNoOp(t *testing.T) {
	if _, ok := RemoveSubagent("Explore:sub-1", ""); ok {
		t.Error("an empty id must return ok = false, must not match arbitrarily")
	}
}

func TestRemoveSubagentUnknownID(t *testing.T) {
	if _, ok := RemoveSubagent("Explore:sub-1,Plan:sub-2", "sub-9"); ok {
		t.Error("a non-existent id must return ok = false")
	}
}

func TestRemoveSubagentOnlyEntry(t *testing.T) {
	got, ok := RemoveSubagent("Explore:sub-1", "sub-1")
	if !ok || got != "" {
		t.Errorf("RemoveSubagent = (%q, %v), want (\"\", true)", got, ok)
	}
}

func TestRemoveSubagentFirstEntry(t *testing.T) {
	got, ok := RemoveSubagent("Explore:sub-1,Plan:sub-2", "sub-1")
	if !ok || got != "Plan:sub-2" {
		t.Errorf("RemoveSubagent = (%q, %v), want (%q, true)", got, ok, "Plan:sub-2")
	}
}

func TestRemoveSubagentMiddleEntry(t *testing.T) {
	got, ok := RemoveSubagent("Explore:sub-1,Plan:sub-2,Bash:sub-3", "sub-2")
	want := "Explore:sub-1,Bash:sub-3"
	if !ok || got != want {
		t.Errorf("RemoveSubagent = (%q, %v), want (%q, true)", got, ok, want)
	}
}

func TestRemoveSubagentLastEntry(t *testing.T) {
	got, ok := RemoveSubagent("Explore:sub-1,Plan:sub-2", "sub-2")
	if !ok || got != "Explore:sub-1" {
		t.Errorf("RemoveSubagent = (%q, %v), want (%q, true)", got, ok, "Explore:sub-1")
	}
}

// Regression: two subagents of the SAME TYPE running in parallel, stopping
// the FIRST one (sub-1) must remove exactly that entry — matched by id, not
// by position "the last entry of that type".
func TestRemoveSubagentSameTypeMatchesByIDNotPosition(t *testing.T) {
	got, ok := RemoveSubagent("Explore:sub-1,Explore:sub-2", "sub-1")
	if !ok || got != "Explore:sub-2" {
		t.Errorf("RemoveSubagent = (%q, %v), want (%q, true)", got, ok, "Explore:sub-2")
	}
}

func TestRemoveSubagentThreeOfSameTypeRemovesMiddle(t *testing.T) {
	got, ok := RemoveSubagent("Explore:a,Explore:b,Explore:c", "b")
	want := "Explore:a,Explore:c"
	if !ok || got != want {
		t.Errorf("RemoveSubagent = (%q, %v), want (%q, true)", got, ok, want)
	}
}

// The ":id" key must include the colon — a RANDOM type name that happens to
// end with the exact id string must not be matched by mistake.
func TestRemoveSubagentDoesNotConfuseDuplicateIDAcrossTypes(t *testing.T) {
	got, ok := RemoveSubagent("TrailingX:y,Explore:x", "x")
	want := "TrailingX:y"
	if !ok || got != want {
		t.Errorf("RemoveSubagent = (%q, %v), want (%q, true)", got, ok, want)
	}
}

func TestSubagentLifecycleTwoParallelSameTypeStopsCorrectOne(t *testing.T) {
	list := AppendSubagent("", "Explore", "sub-1")
	list = AppendSubagent(list, "Explore", "sub-2")
	if list != "Explore:sub-1,Explore:sub-2" {
		t.Fatalf("list after appending = %q", list)
	}

	remaining, ok := RemoveSubagent(list, "sub-1")
	if !ok || remaining != "Explore:sub-2" {
		t.Fatalf("after stopping sub-1 = (%q, %v), want (%q, true)", remaining, ok, "Explore:sub-2")
	}

	remaining, ok = RemoveSubagent(remaining, "sub-2")
	if !ok || remaining != "" {
		t.Fatalf("after stopping sub-2 = (%q, %v), want (\"\", true)", remaining, ok)
	}
}

func TestSubagentLifecycleMixedTypes(t *testing.T) {
	list := AppendSubagent("", "Explore", "sub-1")
	list = AppendSubagent(list, "Plan", "sub-2")
	if list != "Explore:sub-1,Plan:sub-2" {
		t.Fatalf("list after appending = %q", list)
	}

	remaining, ok := RemoveSubagent(list, "sub-2")
	if !ok || remaining != "Explore:sub-1" {
		t.Fatalf("after Plan finished = (%q, %v), want (%q, true)", remaining, ok, "Explore:sub-1")
	}
}

func TestSubagentLifecycleStopWithUnknownIDIsNoOp(t *testing.T) {
	list := AppendSubagent("", "Explore", "sub-1")
	if _, ok := RemoveSubagent(list, "sub-999"); ok {
		t.Error("an unknown id must be a no-op, ok = false")
	}
}

func TestShouldUpdateCwdSubagentLifecycleStartThenStop(t *testing.T) {
	noSubagents := ""
	oneSubagent := AppendSubagent(noSubagents, "Explore", "sub-1")

	if !ShouldUpdateCwd(noSubagents) {
		t.Error("no subagent must allow cwd updates")
	}
	if ShouldUpdateCwd(oneSubagent) {
		t.Error("one subagent must block cwd updates")
	}

	afterStop, ok := RemoveSubagent(oneSubagent, "sub-1")
	if !ok {
		t.Fatal("must be able to remove sub-1")
	}
	if !ShouldUpdateCwd(afterStop) {
		t.Error("after the last subagent stops, cwd updates must be allowed again")
	}
}

func TestShouldUpdateCwdNestedSubagentsNeedAllStoppedBeforeUpdate(t *testing.T) {
	list := AppendSubagent("", "Explore", "sub-1")
	list = AppendSubagent(list, "Plan", "sub-2")
	if ShouldUpdateCwd(list) {
		t.Fatal("two still-running subagents must block cwd updates")
	}

	list, ok := RemoveSubagent(list, "sub-2")
	if !ok {
		t.Fatal("must be able to remove sub-2")
	}
	if ShouldUpdateCwd(list) {
		t.Fatal("with sub-1 still running, cwd updates must still be blocked")
	}

	list, ok = RemoveSubagent(list, "sub-1")
	if !ok {
		t.Fatal("must be able to remove sub-1")
	}
	if !ShouldUpdateCwd(list) {
		t.Fatal("once no subagents remain, cwd updates must be allowed again")
	}
}

// Records a known limitation, not a bug to fix: if the subagent's
// SessionStart hook runs BEFORE the parent's SubagentStart hook manages to
// write @pane_subagents, the cwd will be wrongly updated to the subagent's
// cwd — because at that moment @pane_subagents is still empty. This is a
// race inherited unchanged from the original Rust implementation, not to be
// patched in this package (see the limitation documented on
// AppendSubagent/RemoveSubagent).
func TestShouldUpdateCwdRaceSessionStartBeforeSubagentStart(t *testing.T) {
	optionBeforeSubagentStart := ""
	if !ShouldUpdateCwd(optionBeforeSubagentStart) {
		t.Error("known limitation: if SessionStart runs before SubagentStart, cwd still gets updated")
	}
}

// lockPane serializes concurrent read-compute-write on one pane: N
// goroutines each append to a shared file (standing in for the pane
// option, shared through the OS like the real hooks) and none is lost.
func TestLockPaneSerializesAppends(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	store := filepath.Join(t.TempDir(), "subagents")
	os.WriteFile(store, nil, 0o600)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			unlock := lockPane("%99")
			defer unlock()
			cur, _ := os.ReadFile(store)
			time.Sleep(time.Millisecond) // widen the race window
			os.WriteFile(store, []byte(AppendSubagent(string(cur), "general-purpose", fmt.Sprint(i))), 0o600)
		}(i)
	}
	wg.Wait()
	list, _ := os.ReadFile(store)
	if n := len(strings.Split(string(list), ",")); n != 20 {
		t.Fatalf("%d of 20 entries survived: %s", n, list)
	}
}
