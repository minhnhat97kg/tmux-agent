package install

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func testEnv(t *testing.T) env {
	return env{home: t.TempDir(), bin: "/opt/tmux-agent/bin/tmux-agent", hookScript: "/opt/tmux-agent/assets/hook.sh", out: &bytes.Buffer{}}
}

func readJSON(t *testing.T, path string) map[string]any {
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]any{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// Install replaces stale tmux-agent hooks (older install path), keeps the
// user's own hooks and settings, backs the file up, and uninstall removes
// exactly what install added.
func TestClaudeHooksMergeAndUninstall(t *testing.T) {
	e := testEnv(t)
	cfg := e.path(".claude/settings.json")
	os.MkdirAll(filepath.Dir(cfg), 0o755)
	os.WriteFile(cfg, []byte(`{
  "theme": "dark",
  "hooks": {
    "Stop": [
      {"matcher": "", "hooks": [{"type": "command", "command": "notify-send done"}]},
      {"matcher": "", "hooks": [{"type": "command", "command": "bash '/old/place/assets/hook.sh' claude stop"}]}
    ]
  }
}`), 0o600)
	h, _ := find("claude")

	for i := 0; i < 2; i++ { // twice: must be idempotent
		if err := e.install(h); err != nil {
			t.Fatal(err)
		}
	}
	got := readJSON(t, cfg)
	if got["theme"] != "dark" {
		t.Error("unrelated settings must survive")
	}
	stop := got["hooks"].(map[string]any)["Stop"].([]any)
	if len(stop) != 2 {
		t.Fatalf("Stop hooks = %d, want 2 (user's + ours, stale one replaced): %v", len(stop), stop)
	}
	b, _ := json.Marshal(stop)
	if strings.Contains(string(b), "/old/place") || !strings.Contains(string(b), "/opt/tmux-agent/assets/hook.sh") || !strings.Contains(string(b), "notify-send") {
		t.Errorf("Stop hooks = %s", b)
	}
	if _, err := os.Stat(cfg + ".tmux-agent.bak"); err != nil {
		t.Error("original must be backed up")
	}
	if _, err := os.Stat(e.path(".claude/skills/tmux-agent/SKILL.md")); err != nil {
		t.Error("skill must be installed")
	}

	if err := e.uninstall(h); err != nil {
		t.Fatal(err)
	}
	got = readJSON(t, cfg)
	hooks := got["hooks"].(map[string]any)
	if len(hooks) != 1 || len(hooks["Stop"].([]any)) != 1 {
		t.Errorf("after uninstall only the user's hook must remain, got %v", hooks)
	}
	if _, err := os.Stat(e.path(".claude/skills/tmux-agent")); !os.IsNotExist(err) {
		t.Error("skill must be removed")
	}
}

func TestInstructionBlockSetReplaceRemove(t *testing.T) {
	e := testEnv(t)
	path := e.path(".gemini/GEMINI.md")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("# My rules\nbe nice\n"), 0o644)
	h, _ := find("gemini")

	e.install(h)
	e.install(h)
	b, _ := os.ReadFile(path)
	if strings.Count(string(b), blockBegin) != 1 || !strings.HasPrefix(string(b), "# My rules\nbe nice\n\n") || strings.Contains(string(b), "name: tmux-agent") {
		t.Fatalf("after install:\n%s", b)
	}
	e.uninstall(h)
	b, _ = os.ReadFile(path)
	if strings.Contains(string(b), "tmux-agent") || !strings.Contains(string(b), "be nice") {
		t.Fatalf("after uninstall:\n%s", b)
	}
}

func TestPluginsPointAtBinary(t *testing.T) {
	e := testEnv(t)
	for _, name := range []string{"pi", "opencode"} {
		h, _ := find(name)
		if err := e.install(h); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(e.path(h.file))
		if err != nil || !strings.Contains(string(b), `"/opt/tmux-agent/bin/tmux-agent"`) {
			t.Errorf("%s plugin must call the binary by absolute path:\n%s", name, b)
		}
	}
}

func TestPiReportsLifecycleInOrder(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required to exercise the Pi extension")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, `fake "agent`)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n[ \"$2\" = running ] && sleep 0.2\nprintf '%s\\n' \"$2\" >> \"$REPORT_LOG\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	e := testEnv(t)
	e.bin = bin
	plugin := filepath.Join(dir, "plugin.mjs")
	if err := os.WriteFile(plugin, []byte(e.pluginSource("pi")), 0o600); err != nil {
		t.Fatal(err)
	}
	driver := `import extension from './plugin.mjs';
const handlers = {};
extension({on: (name, fn) => handlers[name] = fn});
handlers.session_start({}, {mode: 'tui', isIdle: () => true});
handlers.agent_start();
handlers.agent_settled({}, {isIdle: () => true});
await new Promise(resolve => setTimeout(resolve, 1000));
`
	driverPath := filepath.Join(dir, "driver.mjs")
	if err := os.WriteFile(driverPath, []byte(driver), 0o600); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "reports")
	cmd := exec.Command(node, driverPath)
	cmd.Env = append(os.Environ(), "TMUX_PANE=%1", "REPORT_LOG="+log)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("extension: %v: %s", err, out)
	}
	got, err := os.ReadFile(log)
	if err != nil || string(got) != "idle\nrunning\nidle\n" {
		t.Fatalf("reports = %q, err = %v", got, err)
	}
}

func TestOpenCodePluginUsesV2DirAndRemovesLegacy(t *testing.T) {
	e := testEnv(t)
	h, _ := find("opencode")
	if h.file != ".config/opencode/plugins/tmux-agent.js" {
		t.Fatalf("opencode plugin path = %q, want the V2 plugins/ directory", h.file)
	}

	// An older tmux-agent wrote the V1 plugin into plugin/ (singular).
	legacy := e.path(".config/opencode/plugin/tmux-agent.js")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("// V1 plugin"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := e.install(h); err != nil {
		t.Fatal(err)
	}
	installed := e.path(".config/opencode/plugins/tmux-agent.js")
	b, err := os.ReadFile(installed)
	if err != nil {
		t.Fatalf("V2 plugin must be installed: %v", err)
	}
	for _, want := range []string{`export default`, `id: "tmux-agent"`, `async setup(ctx)`, `ctx.event.subscribe`, `ctx.session.hook`, `ctx.tool.hook`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("installed plugin missing %q", want)
		}
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Error("legacy V1 plugin at plugin/tmux-agent.js must be removed on install")
	}

	// Uninstall removes both the V2 file and a legacy file that reappeared.
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("// V1 plugin"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.uninstall(h); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{installed, legacy} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s must be removed on uninstall", p)
		}
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	e := testEnv(t)
	e.dry = true
	for _, h := range harnesses {
		if err := e.install(h); err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(e.home)
	if len(entries) != 0 {
		t.Errorf("dry run wrote %v", entries)
	}
}
