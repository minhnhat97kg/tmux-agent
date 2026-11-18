package sidebar

import (
	"strings"
	"testing"
	"time"
)

func TestCleanLine(t *testing.T) {
	got, err := cleanLine("\n  \"Bạn ơi, helper xong rồi nè — xem thử nhé?\"  \nsecond line\n")
	if err != nil || got != "Bạn ơi, helper xong rồi nè — xem thử nhé?" {
		t.Fatalf("cleanLine = %q, %v", got, err)
	}
	if _, err := cleanLine(" \n "); err == nil {
		t.Error("empty answer must be an error")
	}
	long, _ := cleanLine(strings.Repeat("x", 300))
	if n := len([]rune(long)); n != llmMaxLine {
		t.Errorf("long line kept %d runes, want %d", n, llmMaxLine)
	}
}

func TestPhraseLanguages(t *testing.T) {
	a := Alert{Name: "api"}
	if got := say((AvatarConfig{Lang: "vi"}).phrase("done"), a); !strings.HasPrefix(got, "Bạn ơi, api") {
		t.Errorf("vi done = %q", got)
	}
	if got := say((AvatarConfig{Lang: "en"}).phrase("done"), a); !strings.Contains(got, "api is done") {
		t.Errorf("en done = %q", got)
	}
	if got := (AvatarConfig{Lang: "xx"}).phrase("sleep"); got != "All quiet…" {
		t.Errorf("unknown language must fall back to vi, got %q", got)
	}
}

func TestSummaryAsksOnceThenCaches(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	c := AvatarConfig{Lang: "vi", LLM: `echo "Sửa xong 4 hàm, test pass" #`}
	if _, ok := Summary("long final reply …", c); ok {
		t.Fatal("first call starts the LLM in the background")
	}
	var line string
	for i := 0; i < 50; i++ {
		var ok bool
		if line, ok = Summary("long final reply …", c); ok {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if line != "Sửa xong 4 hàm, test pass" {
		t.Fatalf("summary = %q", line)
	}
	if _, ok := Summary("", c); ok {
		t.Error("no reply, no summary")
	}
}

// Every phrase the code asks for exists in every language.
func TestPhraseTablesComplete(t *testing.T) {
	keys := []string{"waiting", "error", "conflict", "stuck", "done", "work1", "workN", "and", "others", "work2", "sleep", "sleep2", "where", "answer"}
	for lang, table := range phrases {
		for _, k := range keys {
			if table[k] == "" {
				t.Errorf("phrase %q missing in %q", k, lang)
			}
		}
	}
}

func TestBusyLineReadsLikeASentence(t *testing.T) {
	c := AvatarConfig{Lang: "en"}
	for names, want := range map[string]string{
		"console":         "console is on it",
		"console,api":     "console and api are on it",
		"console,api,web": "console, api and a few others are on it",
	} {
		if got := c.busyLine(strings.Split(names, ",")); got != want {
			t.Fatalf("busyLine(%s) = %q, want %q", names, got, want)
		}
	}
}
