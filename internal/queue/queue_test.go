package queue

import "testing"

func TestPushPopOrder(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	for i, m := range []string{"first", "second", "third"} {
		if n, _, err := Push("%7", m); err != nil || n != i+1 {
			t.Fatalf("Push %q = %d, %v", m, n, err)
		}
	}
	if got := Panes(); len(got) != 1 || got[0] != "%7" {
		t.Errorf("Panes = %v", got)
	}
	if m, ok := Pop("%7"); !ok || m != "first" {
		t.Errorf("Pop = %q %v", m, ok)
	}
	if l := List("%7"); len(l) != 2 || l[0] != "second" {
		t.Errorf("List = %v", l)
	}
	if n := Clear("%7"); n != 2 {
		t.Errorf("Clear = %d", n)
	}
	if _, ok := Pop("%7"); ok {
		t.Error("empty queue pops nothing")
	}
}

func TestPending(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	_, id, _ := Push("%8", "x")
	if !Pending(id) {
		t.Error("just pushed: pending")
	}
	Pop("%8")
	if Pending(id) {
		t.Error("popped: delivered")
	}
}
