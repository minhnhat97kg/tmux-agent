package hook

import (
	"testing"

	"tmux-agent/internal/process"
)

func TestFindPaneWalksUpToPaneRoot(t *testing.T) {
	snap := process.FromPSOutput(`
  85435  9146 zsh    -zsh
  85480 85435 claude claude
  90001 85480 node   node hook
  90002 90001 bash   bash hook.sh claude stop
`)
	panes := map[int]string{85435: "%9", 1234: "%1"}
	if got := findPane(90002, panes, snap.ParentOf); got != "%9" {
		t.Errorf("findPane = %q, want %%9", got)
	}
	if got := findPane(4242, panes, snap.ParentOf); got != "" {
		t.Errorf("a process outside tmux has no pane, got %q", got)
	}
}
