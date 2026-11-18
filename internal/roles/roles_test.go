package roles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinAndOverride(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if r, ok := Get("reviewer"); !ok || !strings.Contains(r, "reviewer") {
		t.Fatalf("built-in reviewer: %q %v", r, ok)
	}
	os.MkdirAll(Dir(), 0o755)
	os.WriteFile(filepath.Join(Dir(), "reviewer.md"), []byte("my own reviewer\n"), 0o644)
	os.WriteFile(filepath.Join(Dir(), "security.md"), []byte("security auditor"), 0o644)
	if r, _ := Get("reviewer"); r != "my own reviewer" {
		t.Errorf("your file must override the built-in, got %q", r)
	}
	if got := strings.Join(Names(), ","); got != "docs,planner,reviewer,security,tester" {
		t.Errorf("Names = %s", got)
	}
	if _, ok := Get("../etc/passwd"); ok {
		t.Error("path-like names must be rejected")
	}
}
