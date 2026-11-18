// Package checkpoint snapshots a repo's files before each agent turn so a
// bad turn can be undone (`tmux-agent undo <agent>`).
//
// A checkpoint is a commit of every non-ignored file in the working tree —
// tracked or not — built through a throwaway index, so the user's index,
// branch and files are never touched. It's kept under
// refs/tmux-agent/checkpoints/<agent>/<time> (the newest Keep per agent);
// its parent is HEAD at the time.
package checkpoint

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Keep is how many checkpoints each agent keeps.
const Keep = 20

const refRoot = "refs/tmux-agent/checkpoints/"

// Checkpoint is one snapshot.
type Checkpoint struct {
	Ref  string
	Time time.Time
}

// ErrNotRepo: the directory isn't in a git repo — nothing to snapshot.
var ErrNotRepo = errors.New("not a git repo")

type repo struct{ root string }

func open(dir string) (repo, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return repo{}, ErrNotRepo
	}
	return repo{strings.TrimSpace(string(out))}, nil
}

func (r repo) git(env []string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", r.root}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// Key makes an agent name safe for a ref.
func Key(agent string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		}
		return '_'
	}, strings.TrimPrefix(agent, "%"))
}

// snapshot commits the working tree (via a temp index) and returns the
// commit and its tree.
func (r repo) snapshot(msg string) (commit, tree string, err error) {
	idx, err := os.CreateTemp("", "tmux-agent-index-*")
	if err != nil {
		return "", "", err
	}
	idx.Close()
	defer os.Remove(idx.Name())
	env := []string{"GIT_INDEX_FILE=" + idx.Name(),
		"GIT_AUTHOR_NAME=tmux-agent", "GIT_AUTHOR_EMAIL=tmux-agent@localhost",
		"GIT_COMMITTER_NAME=tmux-agent", "GIT_COMMITTER_EMAIL=tmux-agent@localhost"}
	head, headErr := r.git(nil, "rev-parse", "-q", "--verify", "HEAD")
	if headErr == nil {
		if _, err = r.git(env, "read-tree", head); err != nil { // fast path: only changed files get hashed
			return "", "", err
		}
	}
	if _, err = r.git(env, "add", "-A", "."); err != nil {
		return "", "", err
	}
	if tree, err = r.git(env, "write-tree"); err != nil {
		return "", "", err
	}
	args := []string{"commit-tree", tree, "-m", msg}
	if headErr == nil {
		args = append(args, "-p", head)
	}
	commit, err = r.git(env, args...)
	return commit, tree, err
}

// Create snapshots dir's repo for agent, unless nothing changed since its
// last checkpoint. Returns the new ref ("" when skipped).
func Create(dir, agent string) (string, error) {
	r, err := open(dir)
	if err != nil {
		return "", err
	}
	key := Key(agent)
	commit, tree, err := r.snapshot("tmux-agent checkpoint: " + agent)
	if err != nil {
		return "", err
	}
	if list := r.list(key); len(list) > 0 {
		if last, err := r.git(nil, "rev-parse", list[0].Ref+"^{tree}"); err == nil && last == tree {
			return "", nil
		}
	}
	ref := fmt.Sprintf("%s%s/%d", refRoot, key, time.Now().UnixNano())
	if _, err := r.git(nil, "update-ref", ref, commit); err != nil {
		return "", err
	}
	for _, old := range r.list(key)[min(Keep, len(r.list(key))):] {
		r.git(nil, "update-ref", "-d", old.Ref)
	}
	return ref, nil
}

// List returns agent's checkpoints in dir's repo, newest first.
func List(dir, agent string) ([]Checkpoint, error) {
	r, err := open(dir)
	if err != nil {
		return nil, err
	}
	return r.list(Key(agent)), nil
}

func (r repo) list(key string) []Checkpoint {
	out, err := r.git(nil, "for-each-ref", "--format=%(refname)", refRoot+key+"/")
	if err != nil || out == "" {
		return nil
	}
	var cps []Checkpoint
	for _, ref := range strings.Split(out, "\n") {
		var ns int64
		fmt.Sscan(filepath.Base(ref), &ns)
		cps = append(cps, Checkpoint{Ref: ref, Time: time.Unix(0, ns)})
	}
	sort.Slice(cps, func(i, j int) bool { return cps[i].Time.After(cps[j].Time) })
	return cps
}

// Restore puts dir's repo back to ref: files as they were (files created
// since are removed, ignored files untouched) and, if the branch moved since
// (the agent committed), the branch back to where it was. The current state
// is saved first; its ref is returned so the undo can itself be undone.
func Restore(dir, ref string) (backup string, err error) {
	r, err := open(dir)
	if err != nil {
		return "", err
	}
	if _, err := r.git(nil, "rev-parse", "-q", "--verify", ref+"^{commit}"); err != nil {
		return "", fmt.Errorf("no checkpoint %s", ref)
	}
	commit, _, err := r.snapshot("tmux-agent backup before undo")
	if err != nil {
		return "", err
	}
	backup = fmt.Sprintf("refs/tmux-agent/backup/%d", time.Now().UnixNano())
	if _, err := r.git(nil, "update-ref", backup, commit); err != nil {
		return "", err
	}

	if parent, err := r.git(nil, "rev-parse", "-q", "--verify", ref+"^1"); err == nil {
		if head, _ := r.git(nil, "rev-parse", "HEAD"); head != parent {
			if _, err := r.git(nil, "reset", "-q", "--mixed", parent); err != nil {
				return backup, err
			}
		}
	}
	now, _ := r.git(nil, "ls-files", "-co", "--exclude-standard", "-z")
	then, _ := r.git(nil, "ls-tree", "-r", "-z", "--name-only", ref)
	keep := map[string]bool{}
	for _, f := range strings.Split(then, "\x00") {
		keep[f] = true
	}
	for _, f := range strings.Split(now, "\x00") {
		if f != "" && !keep[f] {
			os.Remove(filepath.Join(r.root, f))
		}
	}
	if _, err := r.git(nil, "restore", "--source="+ref, "--worktree", "--", "."); err != nil {
		return backup, err
	}
	return backup, nil
}
