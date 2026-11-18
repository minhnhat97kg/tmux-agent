package adapter

import (
	"testing"

	"tmux-agent/internal/event"
)

// fakeAdapter is a minimal adapter used only to test the registration and
// lookup mechanism, since no real adapter exists for this task.
type fakeAdapter struct{ name string }

func (a fakeAdapter) Name() string                  { return a.name }
func (a fakeAdapter) Registrations() []Registration { return nil }
func (a fakeAdapter) Parse(string, map[string]any) (event.Event, bool) {
	return event.Event{}, false
}

func TestRegisterAndResolve(t *testing.T) {
	// Keep the real registry aside and restore it afterward, so this test
	// doesn't leak state into other tests in the same package.
	original := registry
	registry = map[string]Adapter{}
	t.Cleanup(func() { registry = original })

	register(fakeAdapter{name: "fake"})

	got, ok := Resolve("fake")
	if !ok || got.Name() != "fake" {
		t.Fatalf("Resolve(\"fake\") = (%v, %v), want found", got, ok)
	}
	// An unknown agent must be rejected, without panicking and without
	// returning some arbitrary adapter.
	for _, name := range []string{"", "gemini", "FAKE"} {
		if _, ok := Resolve(name); ok {
			t.Errorf("Resolve(%q) should return ok = false", name)
		}
	}
}
