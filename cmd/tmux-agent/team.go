package main

import (
	"fmt"
	"io"
	"strings"

	"tmux-agent/internal/tmux"
)

// ─── team: start the configured workers ──────────────────────────────────
//
// @agent_team lists workers ("w1 w2 rev@reviewer"); each is started with
// `new` and handed @agent_team_prompt ({name} = its name) — by default the
// task-board convention, so you only post TODOs. With
// @agent_team_autostart on, the session-created hook runs `team --auto`.

const (
	teamOption          = "@agent_team"
	teamPromptOption    = "@agent_team_prompt"
	teamAutostartOption = "@agent_team_autostart"
	defaultTeamPrompt   = "You are {name}. Work the shared task board (tmux-agent skill, task board convention): do every TODO @{name}, then stop."
)

// parseTeam splits "w1 w2 rev@reviewer" into names and roles.
func parseTeam(s string) (names, roles []string) {
	for _, f := range strings.Fields(s) {
		name, role, _ := strings.Cut(f, "@")
		names, roles = append(names, name), append(roles, role)
	}
	return
}

func cmdTeam(args []string, stdout, stderr io.Writer) int {
	auto := len(args) > 0 && args[0] == "--auto"
	if auto {
		args = args[1:]
		if v, _ := tmux.GetOption(teamAutostartOption); v != "on" {
			return exitOK
		}
	}
	spec := strings.Join(args, " ")
	if spec == "" {
		spec, _ = tmux.GetOption(teamOption)
	}
	names, roles := parseTeam(spec)
	if len(names) == 0 {
		fmt.Fprintln(stderr, "usage: tmux-agent team [name[@role]...]   (default: tmux-agent config set team \"w1 w2\")")
		return exitUsage
	}
	prompt, _ := tmux.GetOption(teamPromptOption)
	if strings.TrimSpace(prompt) == "" {
		prompt = defaultTeamPrompt
	}
	code := exitOK
	for i, name := range names {
		if tmux.ResolvePane(name) != name { // names are server-wide
			fmt.Fprintf(stdout, "%s: already running\n", name)
			continue
		}
		pane, warn, err := spawnAgent(spawnOpts{Name: name, Role: roles[i], Wait: true})
		if err != nil {
			fmt.Fprintf(stderr, "team: %s: %v\n", name, err)
			code = exitFailure
			continue
		}
		if warn != "" {
			// Waiting on a startup prompt: typing now would answer it.
			fmt.Fprintf(stderr, "team: %s: %s — send it the prompt yourself\n", name, warn)
			continue
		}
		sendText(pane, strings.ReplaceAll(prompt, "{name}", name))
		fmt.Fprintf(stdout, "%s\t%s\n", name, pane)
	}
	return code
}
