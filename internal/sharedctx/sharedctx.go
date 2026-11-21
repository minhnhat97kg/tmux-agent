// Package sharedctx is a context board shared by the agents working on the
// same project: any agent can post a note (a decision, a convention, "I'm
// editing X, stay out"), read everyone's notes, and delete them.
//
// There is one board per project — the git repo root containing the
// caller's directory, or the directory itself outside git (Scope) — plus a
// "global" board for notes that span projects.
//
// One JSON file per entry: concurrent writers from several agents never
// touch the same file, so no locking is needed.
package sharedctx

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Entry is one note on the board.
type Entry struct {
	ID     string    `json:"id"`
	Author string    `json:"author"` // agent name, or pane id when unnamed
	Pane   string    `json:"pane"`
	Time   time.Time `json:"time"`
	Text   string    `json:"text"`
	Scope  string    `json:"-"`
}

// Global is the scope name of the cross-project board.
const Global = "global"

// Scope names the board for a directory: "<repo>-<hash>" of the git repo
// root containing dir (or of dir itself outside git). The hash keeps two
// checkouts with the same basename apart.
func Scope(dir string) string {
	root := dir
	if out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output(); err == nil {
		root = strings.TrimSpace(string(out))
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	sum := sha1.Sum([]byte(root))
	return filepath.Base(root) + "-" + hex.EncodeToString(sum[:4])
}

// Dir is where a scope's entries live: <root>/<scope>, root being
// $TMUX_AGENT_CONTEXT_DIR, else $XDG_STATE_HOME/tmux-agent/context, else
// ~/.local/state/tmux-agent/context.
func Dir(scope string) string {
	return filepath.Join(rootDir(), scope)
}

func rootDir() string {
	if d := os.Getenv("TMUX_AGENT_CONTEXT_DIR"); d != "" {
		return d
	}
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, _ := os.UserHomeDir()
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "tmux-agent", "context")
}

// Add posts a note to scope and returns it.
func Add(scope, author, pane, text string) (Entry, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Entry{}, errors.New("empty note")
	}
	now := time.Now()
	e := Entry{
		// Base36 of the microsecond clock: short, sortable, unique enough for
		// notes typed by a handful of agents.
		ID:     strconv.FormatInt(now.UnixNano()/1000, 36),
		Author: author,
		Pane:   pane,
		Time:   now,
		Text:   text,
	}
	if err := os.MkdirAll(Dir(scope), 0o755); err != nil {
		return Entry{}, err
	}
	b, _ := json.Marshal(e)
	// O_EXCL: never overwrite another agent's note on an id collision.
	f, err := os.OpenFile(path(scope, e.ID), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return Entry{}, err
	}
	defer f.Close()
	_, err = f.Write(b)
	return e, err
}

// List returns every note in scope, oldest first. Unreadable files are
// skipped.
func List(scope string) []Entry {
	files, _ := filepath.Glob(filepath.Join(Dir(scope), "*.json"))
	var out []Entry
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var e Entry
		if json.Unmarshal(b, &e) == nil {
			e.Scope = scope
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out
}

// ListVisible includes global notes alongside the selected project, oldest first.
func ListVisible(scope string) []Entry {
	if scope == "" || scope == Global {
		return List(Global)
	}
	entries := append(List(scope), List(Global)...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Time.Before(entries[j].Time) })
	return entries
}

// Remove deletes one note in scope by id.
func Remove(scope, id string) error {
	if id == "" || strings.ContainsAny(id, `/\.`) {
		return fmt.Errorf("invalid id %q", id)
	}
	err := os.Remove(path(scope, id))
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("no note %q", id)
	}
	return err
}

// Clear deletes every note in scope, or only those by author when
// author != "".
func Clear(scope, author string) int {
	n := 0
	for _, e := range List(scope) {
		if author == "" || e.Author == author {
			if os.Remove(path(scope, e.ID)) == nil {
				n++
			}
		}
	}
	return n
}

func path(scope, id string) string { return filepath.Join(Dir(scope), id+".json") }
