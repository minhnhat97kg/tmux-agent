// Package roles holds agent roles for `tmux-agent new --role <name>`: a
// short instruction block telling a fresh agent what it's for. Built-ins
// ship embedded; files in Dir() add roles or override them by name.
package roles

import (
	"embed"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed builtin/*.md
var builtin embed.FS

// Dir is where your own roles live: <name>.md.
func Dir() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "tmux-agent", "roles")
}

// Get returns a role's instructions; yours win over the built-in.
func Get(name string) (string, bool) {
	if strings.ContainsAny(name, `/\`) || name == "" {
		return "", false
	}
	if b, err := os.ReadFile(filepath.Join(Dir(), name+".md")); err == nil {
		return strings.TrimSpace(string(b)), true
	}
	if b, err := builtin.ReadFile("builtin/" + name + ".md"); err == nil {
		return strings.TrimSpace(string(b)), true
	}
	return "", false
}

// Names lists every role, built-in and yours.
func Names() []string {
	seen := map[string]bool{}
	entries, _ := builtin.ReadDir("builtin")
	for _, e := range entries {
		seen[strings.TrimSuffix(e.Name(), ".md")] = true
	}
	mine, _ := filepath.Glob(filepath.Join(Dir(), "*.md"))
	for _, p := range mine {
		seen[strings.TrimSuffix(filepath.Base(p), ".md")] = true
	}
	var out []string
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
