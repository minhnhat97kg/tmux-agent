package usage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestForSessionDedupesByMessageID(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	os.MkdirAll(filepath.Join(dir, "projects", "-p"), 0o755)
	transcript := `{"type":"user","message":{"content":"hi"}}
{"type":"assistant","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":1,"cache_read_input_tokens":100}}}
{"type":"assistant","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":100}}}
{"type":"assistant","message":{"id":"m2","usage":{"input_tokens":20,"output_tokens":7,"cache_creation_input_tokens":3}}}
not json
`
	os.WriteFile(filepath.Join(dir, "projects", "-p", "s1.jsonl"), []byte(transcript), 0o644)
	got, ok := ForSession("s1")
	want := Tokens{In: 30, Out: 12, CacheRead: 100, CacheWrite: 3}
	if !ok || got != want {
		t.Fatalf("ForSession = %+v %v, want %+v", got, ok, want)
	}
	if got.Short() != "33 in · 12 out · 100 cached" {
		t.Errorf("Short = %q", got.Short())
	}
	if _, ok := ForSession("nope"); ok {
		t.Error("unknown session")
	}
}
