// Package install wires tmux-agent into agent harnesses — `tmux-agent
// install <harness>` / `uninstall <harness>` (herdr's `integration install`).
//
// Each harness gets what it supports, from most to least precise:
//
//   - status integration: hooks merged into its JSON config (claude, codex),
//     an extension/plugin file (pi, opencode) — exact running/idle/waiting.
//     Harnesses without one still get status from the screen scraper.
//   - usage docs for the agent: the tmux-agent skill (claude, codex, pi), or
//     a marked block in its global instructions file (others).
//
// Every write is reversible by uninstall: hook entries are recognized by
// their hook.sh command, files carry a fixed name, instruction blocks sit
// between markers. JSON configs are backed up once before the first edit.
package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/tabwriter"

	"tmux-agent/assets"
	"tmux-agent/internal/setup"
	"tmux-agent/internal/skill"
)

// harness describes one agent CLI and where its integration pieces go.
// Paths are relative to $HOME.
type harness struct {
	name   string
	detect []string // a config dir that exists, or a binary on PATH
	hooks  string   // JSON config to merge hook entries into ("" = none)
	file   string   // extension/plugin file to write ("" = none)
	legacy []string // files older installs wrote; removed on install and uninstall
	skills string   // skills root (<root>/tmux-agent/SKILL.md)
	instr  string   // global instructions file for a marked block
	note   string   // shown after install
}

var harnesses = []harness{
	{name: "claude", detect: []string{".claude", "claude"}, hooks: ".claude/settings.json", skills: ".claude/skills"},
	{name: "codex", detect: []string{".codex", "codex"}, hooks: ".codex/hooks.json", skills: ".codex/skills",
		note: "Codex runs hooks only when its hooks feature is enabled in your version — if status stays 'unknown', the screen scraper still covers it."},
	{name: "pi", detect: []string{".pi", "pi"}, file: ".pi/agent/extensions/tmux-agent.ts", skills: ".pi/agent/skills"},
	{name: "opencode", detect: []string{".config/opencode", "opencode"},
		// OpenCode v2 discovers both directories, but only treats `plugins/`
		// as V2. Older tmux-agent installs wrote the V1 plugin to `plugin/`;
		// remove that file so v2 can't load the stale, rejected copy.
		file:   ".config/opencode/plugins/tmux-agent.js",
		legacy: []string{".config/opencode/plugin/tmux-agent.js"},
		instr:  ".config/opencode/AGENTS.md"},
	{name: "gemini", detect: []string{".gemini", "gemini"}, instr: ".gemini/GEMINI.md", note: "status comes from screen detection"},
	{name: "copilot", detect: []string{".copilot", "copilot"}, instr: ".copilot/copilot-instructions.md", note: "status comes from screen detection"},
	{name: "amp", detect: []string{".config/amp", "amp"}, instr: ".config/amp/AGENTS.md", note: "status comes from screen detection"},
}

// Names lists the supported harnesses.
func Names() []string {
	var out []string
	for _, h := range harnesses {
		out = append(out, h.name)
	}
	return out
}

func find(name string) (harness, bool) {
	for _, h := range harnesses {
		if h.name == name {
			return h, true
		}
	}
	return harness{}, false
}

func (h harness) installed(home string) bool {
	for _, d := range h.detect {
		if strings.Contains(d, "/") || strings.HasPrefix(d, ".") {
			if _, err := os.Stat(filepath.Join(home, d)); err == nil {
				return true
			}
		} else if _, err := exec.LookPath(d); err == nil {
			return true
		}
	}
	return false
}

// env is what the writers need; a struct so tests can point it anywhere.
type env struct {
	home, bin, hookScript string
	dry                   bool
	out                   io.Writer
}

// Dispatch implements `tmux-agent install|uninstall [--dry-run] [harness…|all]`.
// No harness (or "all") means every supported harness found on this machine.
func Dispatch(uninstall bool, args []string, stdout, stderr io.Writer) int {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, "install:", err)
		return 1
	}
	bin, err := os.Executable()
	if err != nil {
		bin = "tmux-agent"
	} else if resolved, err := filepath.EvalSymlinks(bin); err == nil {
		bin = resolved
	}
	e := env{home: home, bin: bin, hookScript: setup.ResolveHookScript().Path, out: stdout}

	var names []string
	for _, a := range args {
		switch {
		case a == "--dry-run" || a == "-n":
			e.dry = true
		case a == "--list":
			return list(home, stdout)
		case a == "all":
		default:
			names = append(names, a)
		}
	}
	explicit := len(names) > 0
	if !explicit {
		for _, h := range harnesses {
			if h.installed(home) {
				names = append(names, h.name)
			}
		}
		if len(names) == 0 {
			fmt.Fprintf(stderr, "no supported harness found; name one: %s\n", strings.Join(Names(), ", "))
			return 1
		}
	}

	code := 0
	for _, n := range names {
		h, ok := find(n)
		if !ok {
			fmt.Fprintf(stderr, "unknown harness %q (supported: %s)\n", n, strings.Join(Names(), ", "))
			code = 2
			continue
		}
		fmt.Fprintf(stdout, "%s:\n", h.name)
		var err error
		if uninstall {
			err = e.uninstall(h)
		} else {
			err = e.install(h)
			if err == nil && h.note != "" {
				fmt.Fprintf(stdout, "  note  %s\n", h.note)
			}
		}
		if err != nil {
			fmt.Fprintf(stderr, "  %s: %v\n", h.name, err)
			code = 1
		}
	}
	if e.dry {
		fmt.Fprintln(stdout, "(dry run — nothing written)")
	}
	return code
}

func list(home string, out io.Writer) int {
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	defer w.Flush()
	fmt.Fprintln(w, "HARNESS\tFOUND\tSTATUS\tDOCS FOR THE AGENT")
	for _, h := range harnesses {
		found := "-"
		if h.installed(home) {
			found = "yes"
		}
		status := "screen detection"
		switch {
		case h.hooks != "":
			status = "hooks → ~/" + h.hooks
		case h.file != "":
			status = "plugin → ~/" + h.file
		}
		docs := "skill → ~/" + h.skills
		if h.skills == "" {
			docs = "block in ~/" + h.instr
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", h.name, found, status, docs)
	}
	return 0
}

func (e env) path(rel string) string { return filepath.Join(e.home, rel) }

func (e env) say(verb, what string) { fmt.Fprintf(e.out, "  %-6s %s\n", verb, what) }

func (e env) install(h harness) error {
	if h.hooks != "" {
		snippet, ok := setup.BuildAgentSnippet(h.name, e.hookScript)
		if !ok {
			return fmt.Errorf("no hook definitions for %s", h.name)
		}
		if err := e.editJSON(e.path(h.hooks), func(cfg map[string]any) { mergeHooks(cfg, snippet, h.name) }); err != nil {
			return err
		}
		e.say("hooks", "~/"+h.hooks)
	}
	if h.file != "" {
		if err := e.write(e.path(h.file), e.pluginSource(h.name)); err != nil {
			return err
		}
		e.say("plugin", "~/"+h.file)
		for _, p := range h.legacy {
			if err := e.remove(p); err != nil {
				return err
			}
		}
	}
	if h.skills != "" {
		if err := e.write(filepath.Join(e.path(h.skills), "tmux-agent", "SKILL.md"), skill.Render(e.bin)); err != nil {
			return err
		}
		e.say("skill", "~/"+h.skills+"/tmux-agent/SKILL.md")
	}
	if h.instr != "" {
		if err := e.editText(e.path(h.instr), func(s string) string { return setBlock(s, skillBody(skill.Render(e.bin))) }); err != nil {
			return err
		}
		e.say("docs", "~/"+h.instr+" (tmux-agent block)")
	}
	return nil
}

func (e env) uninstall(h harness) error {
	if h.hooks != "" {
		if _, err := os.Stat(e.path(h.hooks)); err == nil {
			if err := e.editJSON(e.path(h.hooks), func(cfg map[string]any) { removeHooks(cfg, h.name) }); err != nil {
				return err
			}
			e.say("hooks", "removed from ~/"+h.hooks)
		}
	}
	paths := append([]string{h.file}, h.legacy...)
	paths = append(paths, filepath.Join(h.skills, "tmux-agent"))
	for _, p := range paths {
		if p == "" || p == "tmux-agent" {
			continue
		}
		if err := e.remove(p); err != nil {
			return err
		}
	}
	if h.instr != "" {
		if b, err := os.ReadFile(e.path(h.instr)); err == nil && strings.Contains(string(b), blockBegin) {
			if err := e.editText(e.path(h.instr), func(s string) string { return setBlock(s, "") }); err != nil {
				return err
			}
			e.say("docs", "block removed from ~/"+h.instr)
		}
	}
	return nil
}

// pluginSource is the extension/plugin for a harness, pointing at this
// binary by absolute path (it runs outside the repo, where hook.sh can't be
// found next to it).
func (e env) pluginSource(name string) string {
	switch name {
	case "opencode":
		return strings.Replace(assets.OpenCodePlugin, `{ cmd: "tmux-agent",`, fmt.Sprintf(`{ cmd: %q,`, e.bin), 1)
	case "pi":
		return strings.ReplaceAll(piExtension, "{{BIN}}", e.bin)
	}
	return ""
}

// piExtension reports Pi's agent lifecycle to tmux-agent (same events
// herdr's Pi integration uses). Interactive (tui) sessions only.
const piExtension = `// installed by tmux-agent — ` + "`tmux-agent install pi`" + ` overwrites this file,
// ` + "`tmux-agent uninstall pi`" + ` removes it.
// @ts-nocheck
import { spawn } from "node:child_process";

const BIN = "{{BIN}}";

function report(status) {
  if (!process.env.TMUX_PANE) return;
  try {
    spawn(BIN, ["set-status", status, "--agent", "pi"], { stdio: "ignore", detached: true }).unref();
  } catch {}
}

export default function (pi) {
  let root = false;
  pi.on("session_start", (_event, ctx) => {
    if (ctx?.mode !== "tui") return;
    root = true;
    report(ctx?.isIdle?.() === false ? "running" : "idle");
  });
  pi.on("agent_start", () => {
    if (root) report("running");
  });
  pi.on("agent_settled", (_event, ctx) => {
    if (root && ctx?.isIdle?.() === true) report("idle");
  });
}
`

// ─── JSON hooks ────────────────────────────────────────────────────────

// ourCommand matches the commands tmux-agent generates for agent: `bash
// '<…>/hook.sh' <agent> <event>` — whatever the install path.
func ourCommand(agent string) *regexp.Regexp {
	return regexp.MustCompile(`hook\.sh'? ` + regexp.QuoteMeta(agent) + ` [a-z-]+$`)
}

// mergeHooks replaces tmux-agent's entries in cfg["hooks"] with snippet's
// (so re-running install updates paths instead of duplicating), leaving
// every other hook alone.
func mergeHooks(cfg, snippet map[string]any, agent string) {
	removeHooks(cfg, agent)
	hooks, _ := cfg["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	add, _ := snippet["hooks"].(map[string]any)
	for trigger, entries := range add {
		existing, _ := hooks[trigger].([]any)
		var norm []any
		b, _ := json.Marshal(entries)
		json.Unmarshal(b, &norm)
		hooks[trigger] = append(existing, norm...)
	}
	cfg["hooks"] = hooks
}

// removeHooks drops every hook entry whose commands are all tmux-agent's,
// and triggers left empty.
func removeHooks(cfg map[string]any, agent string) {
	hooks, _ := cfg["hooks"].(map[string]any)
	re := ourCommand(agent)
	for trigger, v := range hooks {
		entries, _ := v.([]any)
		var keep []any
		for _, entry := range entries {
			if !isOurs(entry, re) {
				keep = append(keep, entry)
			}
		}
		if len(keep) == 0 {
			delete(hooks, trigger)
		} else {
			hooks[trigger] = keep
		}
	}
	if hooks != nil && len(hooks) == 0 {
		delete(cfg, "hooks")
	}
}

func isOurs(entry any, re *regexp.Regexp) bool {
	m, _ := entry.(map[string]any)
	list, _ := m["hooks"].([]any)
	if len(list) == 0 {
		return false
	}
	for _, h := range list {
		hm, _ := h.(map[string]any)
		cmd, _ := hm["command"].(string)
		if !re.MatchString(cmd) {
			return false
		}
	}
	return true
}

// editJSON loads a JSON object (missing file = {}), applies fn, and writes
// it back if it changed — backing the original up once as <file>.tmux-agent.bak.
func (e env) editJSON(path string, fn func(map[string]any)) error {
	cfg := map[string]any{}
	orig, err := os.ReadFile(path)
	switch {
	case err == nil:
		if len(strings.TrimSpace(string(orig))) > 0 {
			if err := json.Unmarshal(orig, &cfg); err != nil {
				return fmt.Errorf("%s is not valid JSON, not touching it: %w", path, err)
			}
		}
	case !errors.Is(err, os.ErrNotExist):
		return err
	}
	fn(cfg)
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	if string(out) == string(orig) || e.dry {
		return nil
	}
	if orig != nil {
		bak := path + ".tmux-agent.bak"
		if _, err := os.Stat(bak); errors.Is(err, os.ErrNotExist) {
			if err := os.WriteFile(bak, orig, 0o600); err != nil {
				return err
			}
		}
	}
	return e.write(path, string(out))
}

// ─── text files ────────────────────────────────────────────────────────

const (
	blockBegin = "<!-- tmux-agent:begin (managed by `tmux-agent install`) -->"
	blockEnd   = "<!-- tmux-agent:end -->"
)

// setBlock replaces the tmux-agent block in s with body (removes it when
// body is ""), or appends one.
func setBlock(s, body string) string {
	block := ""
	if body != "" {
		block = blockBegin + "\n" + strings.TrimSpace(body) + "\n" + blockEnd + "\n"
	}
	if i := strings.Index(s, blockBegin); i >= 0 {
		if j := strings.Index(s[i:], blockEnd); j >= 0 {
			end := i + j + len(blockEnd)
			if end < len(s) && s[end] == '\n' {
				end++
			}
			return s[:i] + block + s[end:]
		}
	}
	if block == "" {
		return s
	}
	if s != "" && !strings.HasSuffix(s, "\n\n") {
		if !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
		s += "\n"
	}
	return s + block
}

// skillBody strips the YAML front matter off SKILL.md.
func skillBody(md string) string {
	if strings.HasPrefix(md, "---\n") {
		if i := strings.Index(md[4:], "\n---\n"); i >= 0 {
			return md[4+i+5:]
		}
	}
	return md
}

func (e env) editText(path string, fn func(string) string) error {
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	out := fn(string(b))
	if out == string(b) || e.dry {
		return nil
	}
	return e.write(path, out)
}

// remove deletes rel (relative to $HOME) if it exists. It is used both for
// files the integration owns and for legacy paths an older install left
// behind.
func (e env) remove(rel string) error {
	if rel == "" {
		return nil
	}
	if _, err := os.Stat(e.path(rel)); err != nil {
		return nil
	}
	if !e.dry {
		if err := os.RemoveAll(e.path(rel)); err != nil {
			return err
		}
	}
	e.say("remove", "~/"+rel)
	return nil
}

func (e env) write(path, content string) error {
	if e.dry {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	return os.WriteFile(path, []byte(content), mode)
}
