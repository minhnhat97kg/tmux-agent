package worktree

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"tmux-agent/internal/tmux"
)

// SpawnRequest bundles every input of a spawn call: the repo, the task name
// the user typed (the slug source), the current tmux session, and the agent
// and mode to run in the new worktree.
type SpawnRequest struct {
	RepoRoot string
	TaskName string
	Session  string
	Agent    string
	Mode     string
}

// Spawn creates a new git worktree + a new tmux window, then runs the agent
// in it. Returns the newly created git branch name on success.
func Spawn(req *SpawnRequest) (string, error) {
	return spawnWith(RealEnv{}, req)
}

// spawnWith is Spawn's core, taking a SpawnEnv so tests don't need real
// git/tmux.
//
// Order: slugify the task name → validate worktreeDir (repo-relative) →
// pick a free slug (not colliding with a branch or an on-disk path) →
// "git worktree add -b" → create a new tmux window → set four markers at
// the window level → send the command that runs the agent. Any step failing
// after "git worktree add" triggers a rollback (see rollbackSpawn): kill the
// window (if created), remove the worktree, delete the branch — because
// "git worktree add -b" already created the branch, and not deleting it
// would leave an orphaned branch that collides with the next spawn.
func spawnWith(env SpawnEnv, req *SpawnRequest) (string, error) {
	slug := Slugify(req.TaskName)
	if slug == "" {
		return "", errors.New("name is empty after slugifying")
	}
	repo := req.RepoRoot

	prefix := env.BranchPrefix()
	if prefix == "" {
		prefix = DefaultBranchPrefix
	}
	worktreeDir := env.WorktreeDir()

	// Early check: worktreeDir must be repo-relative. Uses WorktreePathFor
	// itself as the check (it already encodes the right rule: empty →
	// default, absolute or containing ".." → reject) so there's no second
	// rule duplicated here.
	if _, ok := WorktreePathFor(repo, "probe", worktreeDir); !ok {
		return "", errors.New("worktree directory must be inside the repo (relative path)")
	}

	unique, ok := PickUniqueSlug(slug, func(candidate string) bool {
		branch := prefix + candidate
		if !env.BranchIsFree(repo, branch) {
			return false
		}
		path, ok := WorktreePathFor(repo, candidate, worktreeDir)
		return ok && env.WorktreePathIsFree(path)
	})
	if !ok {
		return "", fmt.Errorf("could not find a free branch name after %d attempts", maxCollisionAttempts+1)
	}

	branch := prefix + unique
	worktreePath, ok := WorktreePathFor(repo, unique, worktreeDir)
	if !ok {
		return "", errors.New("worktree directory must be inside the repo (relative path)")
	}

	if err := env.WorktreeAdd(repo, worktreePath, branch); err != nil {
		return "", fmt.Errorf("git: %w", err)
	}

	paneID, windowID, err := env.NewWindow(req.Session, worktreePath, unique)
	if err != nil {
		rb := rollbackSpawn(env, repo, worktreePath, branch, "")
		return "", composeSpawnError(fmt.Errorf("tmux: %w", err), rb)
	}

	// Markers are set at the window level so a child pane split off later
	// (e.g. a Claude Code subagent) inherits them via tmux's option
	// fallthrough mechanism.
	markers := []struct{ key, value string }{
		{tmux.SpawnedOption, "1"},
		{tmux.SpawnedFromOption, repo},
		{tmux.SpawnedWorktreeOption, worktreePath},
		{tmux.SpawnedBranchOption, branch},
	}
	for _, m := range markers {
		if err := env.SetWindowOption(windowID, m.key, m.value); err != nil {
			rb := rollbackSpawn(env, repo, worktreePath, branch, windowID)
			return "", composeSpawnError(fmt.Errorf("tmux: failed to set %s: %w", m.key, err), rb)
		}
	}

	if err := env.SendCommand(paneID, AgentCommand(req.Agent, req.Mode)); err != nil {
		rb := rollbackSpawn(env, repo, worktreePath, branch, windowID)
		return "", composeSpawnError(fmt.Errorf("tmux: %w", err), rb)
	}

	return branch, nil
}

// rollbackSpawn performs best-effort cleanup after a partial spawn: kill the
// window (if created), remove the worktree, delete the branch that
// "git worktree add -b" created. Each step collects its own error so the
// caller sees the full picture of what's left on disk/tmux — it doesn't
// stop partway even if one step fails.
func rollbackSpawn(env SpawnEnv, repo, worktreePath, branch, windowID string) []string {
	var errs []string
	if windowID != "" {
		if err := env.KillWindow(windowID); err != nil {
			errs = append(errs, fmt.Sprintf("kill_window: %v", err))
		}
	}
	if err := env.WorktreeRemove(repo, worktreePath); err != nil {
		errs = append(errs, fmt.Sprintf("worktree_remove: %v", err))
	}
	if err := env.BranchDelete(repo, branch); err != nil {
		errs = append(errs, fmt.Sprintf("branch_delete: %v", err))
	}
	return errs
}

// composeSpawnError joins the primary error with any rollback errors into a
// single string, so the user sees both the cause and whatever state is left
// behind.
func composeSpawnError(primary error, rollbackErrs []string) error {
	if len(rollbackErrs) == 0 {
		return primary
	}
	return fmt.Errorf("%w (rollback incomplete: %s)", primary, strings.Join(rollbackErrs, "; "))
}

// Remove cleans up a pane previously created by spawn: depending on mode,
// removes the git worktree + branch, then kills the tmux window.
func Remove(paneID string, mode RemoveMode) error {
	return removeWith(RealEnv{}, paneID, mode)
}

// removeWith is Remove's core.
//
// Runs the ENTIRE git cleanup (worktree remove --force, then branch -D)
// BEFORE killing the tmux window: the window is the ONLY UI handle the
// retry path still relies on, so if git fails partway through, the window
// (and its markers) must be left alive — killing it first would make any
// leftover git state impossible to clean up further from the sidebar. Each
// git step is skipped once its target is already gone (WorktreePathExists /
// BranchExists false) — letting a re-run of a partial operation converge
// (retry-safe) instead of erroring because the target no longer exists.
func removeWith(env SpawnEnv, paneID string, mode RemoveMode) error {
	markers := ParseSpawnMarkers(env.DisplayMessage(paneID, SpawnMarkersTemplate()))
	if !markers.IsSpawned() {
		return errors.New("this pane was not created by sidebar spawn")
	}
	if markers.WorktreePath == "" {
		return errors.New("marker missing worktree path")
	}
	if markers.Branch == "" {
		return errors.New("marker missing branch name")
	}
	if markers.WindowID == "" {
		return errors.New("could not determine window id")
	}

	if mode == RemoveWindowAndWorktree {
		if env.WorktreePathExists(markers.WorktreePath) {
			if err := env.WorktreeRemove(markers.FromRepo, markers.WorktreePath); err != nil {
				return fmt.Errorf("git: %w", err)
			}
		}
		// "git worktree remove" leaves the branch behind — delete it here,
		// BEFORE kill_window, so an error at this step still leaves the
		// window as a retry handle.
		if env.BranchExists(markers.FromRepo, markers.Branch) {
			if err := env.BranchDelete(markers.FromRepo, markers.Branch); err != nil {
				return fmt.Errorf("git: %w", err)
			}
		}
	}

	if err := env.KillWindow(markers.WindowID); err != nil {
		return fmt.Errorf("tmux: %w", err)
	}
	return nil
}

// ─── RealEnv: the real SpawnEnv implementation, calling git + tmux ────

// RealEnv implements worktree.SpawnEnv using real git commands (via
// os/exec) and real tmux (via the internal/tmux package). This is the
// implementation used for actual spawn/remove commands; tests use a fake
// SpawnEnv to avoid touching git/tmux.
type RealEnv struct{}

func (RealEnv) BranchPrefix() string {
	return globalOption(tmux.BranchPrefixOption)
}

func (RealEnv) WorktreeDir() string {
	return globalOption(tmux.WorktreeDirOption)
}

func (RealEnv) BranchIsFree(repo, branch string) bool {
	return !branchExists(repo, branch)
}

func (RealEnv) BranchExists(repo, branch string) bool {
	return branchExists(repo, branch)
}

func (RealEnv) WorktreePathIsFree(path string) bool {
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}

func (RealEnv) WorktreePathExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func (RealEnv) WorktreeAdd(repo, worktreePath, branch string) error {
	_, err := runGit(repo, "worktree", "add", worktreePath, "-b", branch)
	return err
}

func (RealEnv) WorktreeRemove(repo, worktreePath string) error {
	_, err := runGit(repo, "worktree", "remove", "--force", worktreePath)
	return err
}

func (RealEnv) BranchDelete(repo, branch string) error {
	_, err := runGit(repo, "branch", "-D", branch)
	return err
}

// NewWindow calls tmux.NewWindow directly — it already returns an error
// with a message (RunTmuxCapture) from part 1, no need to wrap it again.
func (RealEnv) NewWindow(session, cwd, name string) (string, string, error) {
	return tmux.NewWindow(session, cwd, name)
}

// KillWindow, SetWindowOption, SendCommand call tmux.RunTmuxCapture directly
// instead of using the existing tmux.KillWindow/SetWindowOption/SendCommand
// — those functions use RunTmux (silently swallowing errors) for the
// background refresh path. spawn/remove are user-initiated actions, so
// errors must be reported clearly.
func (RealEnv) KillWindow(windowID string) error {
	_, err := tmux.RunTmuxCapture("kill-window", "-t", windowID)
	return err
}

func (RealEnv) SetWindowOption(window, key, value string) error {
	_, err := tmux.RunTmuxCapture("set", "-w", "-t", window, key, value)
	return err
}

// SendCommand sends the content with "-l" (literal) then Enter as a separate
// command — this avoids a command containing a string matching a tmux key
// name (e.g. "Tab") being interpreted by tmux as a keypress instead of text.
func (RealEnv) SendCommand(target, command string) error {
	if _, err := tmux.RunTmuxCapture("send-keys", "-t", target, "-l", command); err != nil {
		return err
	}
	_, err := tmux.RunTmuxCapture("send-keys", "-t", target, "Enter")
	return err
}

func (RealEnv) DisplayMessage(paneID, template string) string {
	return tmux.DisplayMessage(paneID, template)
}

// globalOption reads a global tmux option via "tmux show -g". This is a
// config read, not a destructive operation — it uses RunTmux (soft-fail):
// an unset option is just "", not an error worth reporting to the user.
func globalOption(key string) string {
	out, ok := tmux.RunTmux("show", "-g")
	if !ok {
		return ""
	}
	for _, line := range strings.Split(out, "\n") {
		k, v, found := strings.Cut(line, " ")
		if found && k == key {
			return strings.Trim(v, `"`)
		}
	}
	return ""
}

// runGit runs git in directory dir, returning trimmed stdout or an error
// with a message (stderr, or the exec error when git isn't found).
// GIT_OPTIONAL_LOCKS=0 disables optimistic locking so it doesn't contend
// with the sidebar's background refresh.
func runGit(dir string, args ...string) (string, error) {
	cmdArgs := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", cmdArgs...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", errors.New(msg)
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// branchExists reports true when "refs/heads/<branch>" exists in the repo.
func branchExists(repo, branch string) bool {
	_, err := runGit(repo, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	return err == nil
}

// repoRootOf returns the toplevel of the git repo containing cwd. ok=false
// when cwd is not inside any git repo (or git isn't found).
func repoRootOf(cwd string) (string, bool) {
	out, err := runGit(cwd, "rev-parse", "--show-toplevel")
	if err != nil || out == "" {
		return "", false
	}
	return out, true
}
