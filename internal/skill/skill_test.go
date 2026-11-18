package skill

import (
	"strings"
	"testing"
)

func TestRenderFillsBinary(t *testing.T) {
	got := Render("/opt/bin/tmux-agent")
	if strings.Contains(got, "{{BIN}}") || !strings.HasPrefix(got, "---\nname: tmux-agent") || !strings.Contains(got, "/opt/bin/tmux-agent new") {
		t.Fatalf("bad skill:\n%s", got)
	}
}
