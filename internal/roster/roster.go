// Package roster remembers which agents were running — name, kind, cwd,
// session id, role — so `tmux-agent restore` can bring them back after a
// tmux or machine restart (Claude resumes its own session).
package roster

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"tmux-agent/internal/tmux"
)

// Agent is one remembered agent.
type Agent struct {
	Name      string `json:"name,omitempty"`
	Agent     string `json:"agent"`
	Cwd       string `json:"cwd"`
	SessionID string `json:"session_id,omitempty"`
	Role      string `json:"role,omitempty"`
}

// Snapshot is what's on disk.
type Snapshot struct {
	Saved  time.Time `json:"saved"`
	Agents []Agent   `json:"agents"`
}

// File is where the roster lives.
func File() string {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, _ := os.UserHomeDir()
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "tmux-agent", "agents.json")
}

// Collect lists the running agents (role from the @agent_role pane option).
func Collect(sessions []tmux.SessionInfo) []Agent {
	var out []Agent
	for _, s := range sessions {
		for _, w := range s.Windows {
			for _, p := range w.Panes {
				out = append(out, Agent{Name: p.Name, Agent: string(p.Agent), Cwd: p.Path, SessionID: p.SessionID,
					Role: tmux.GetPaneOptionValue(p.PaneID, "@agent_role")})
			}
		}
	}
	return out
}

// Save writes agents — never an empty list: right after a restart nothing
// runs yet, and that must not erase what restore needs.
func Save(agents []Agent) error {
	if len(agents) == 0 {
		return nil
	}
	b, err := json.MarshalIndent(Snapshot{Saved: time.Now(), Agents: agents}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(File()), 0o755); err != nil {
		return err
	}
	tmp := File() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, File())
}

// Load reads the last snapshot.
func Load() (Snapshot, error) {
	var s Snapshot
	b, err := os.ReadFile(File())
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(b, &s)
}

// Missing returns the remembered agents not running now (matched by name,
// else by session id, else by kind+cwd).
func Missing(saved, running []Agent) []Agent {
	var out []Agent
	for _, a := range saved {
		found := false
		for _, r := range running {
			switch {
			case a.Name != "" && a.Name == r.Name,
				a.SessionID != "" && a.SessionID == r.SessionID,
				a.Name == "" && a.SessionID == "" && a.Agent == r.Agent && a.Cwd == r.Cwd:
				found = true
			}
		}
		if !found {
			out = append(out, a)
		}
	}
	return out
}

// ResumeArgs are the agent arguments that continue its old session.
func ResumeArgs(a Agent) []string {
	if a.SessionID != "" && a.Agent == "claude" {
		return []string{"--resume", a.SessionID}
	}
	return nil
}
