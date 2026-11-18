// Package skill holds the tmux-agent SKILL.md that teaches agents the CLI;
// internal/install writes it into each harness.
package skill

import (
	_ "embed"
	"strings"
)

//go:embed SKILL.md
var template string

// Render fills in the binary path the skill tells the agent to call.
func Render(bin string) string {
	return strings.ReplaceAll(template, "{{BIN}}", bin)
}
