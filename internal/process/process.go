// Package process captures a snapshot of the machine's process tree at a
// point in time.
//
// The query layer needs to answer "is the agent process in this pane still
// alive?" for every pane. Asking ps once per pane is N child-process
// invocations per second; capturing once and looking things up in memory is
// one.
package process

import (
	"os/exec"
	"path"
	"strconv"
	"strings"
)

// Info is the minimal information about a process.
type Info struct {
	// Comm is the short command name reported by ps.
	Comm string
	// Args is the full command line, with internal whitespace preserved.
	Args string
}

// Snapshot is a process tree at a point in time.
type Snapshot struct {
	ChildrenOf map[int][]int
	InfoByPID  map[int]Info
	parentOf   map[int]int
}

// ParentOf returns pid's parent pid, if pid was in the snapshot.
func (s *Snapshot) ParentOf(pid int) (int, bool) {
	p, ok := s.parentOf[pid]
	return p, ok
}

// Scan runs ps and builds a snapshot. ok = false when ps fails — the caller
// treats this as having no process information and takes the degraded path.
func Scan() (*Snapshot, bool) {
	out, err := exec.Command("ps", "-eo", "pid=,ppid=,comm=,args=").Output()
	if err != nil {
		return nil, false
	}
	return FromPSOutput(string(out)), true
}

// FromPSOutput parses the output of `ps -eo pid=,ppid=,comm=,args=`.
//
// The first three fields are split on whitespace; the rest is args and is
// preserved as-is, since a path can contain spaces
// ("/Applications/Codex App/…"). A line that fails to parse is skipped: one
// broken line must not break the whole snapshot.
func FromPSOutput(out string) *Snapshot {
	snap := &Snapshot{
		ChildrenOf: map[int][]int{},
		InfoByPID:  map[int]Info{},
		parentOf:   map[int]int{},
	}

	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}

		snap.ChildrenOf[ppid] = append(snap.ChildrenOf[ppid], pid)
		snap.parentOf[pid] = ppid
		snap.InfoByPID[pid] = Info{
			Comm: fields[2],
			Args: strings.Join(fields[3:], " "),
		}
	}

	return snap
}

// Descendants returns the set of pids consisting of the seeds and all their
// descendants. Breadth-first with a visited marker, so a cycle cannot cause
// a hang.
func (s *Snapshot) Descendants(seeds []int) map[int]bool {
	seen := map[int]bool{}
	queue := append([]int(nil), seeds...)

	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		if seen[pid] {
			continue
		}
		seen[pid] = true
		for _, child := range s.ChildrenOf[pid] {
			if !seen[child] {
				queue = append(queue, child)
			}
		}
	}

	return seen
}

// TreeHasAgent is true when the subtree of seeds contains a process whose
// name exactly matches agentName. Used to know the agent is still alive
// after the pane has dropped back to a shell.
func (s *Snapshot) TreeHasAgent(seeds []int, agentName string) bool {
	for pid := range s.Descendants(seeds) {
		if info, ok := s.InfoByPID[pid]; ok && processMatchesAgent(info, agentName) {
			return true
		}
	}
	return false
}

// CommandLinesForTree returns the command line of every process in the
// subtree, falling back to Comm when there are no args.
func (s *Snapshot) CommandLinesForTree(seeds []int) []string {
	var lines []string
	for pid := range s.Descendants(seeds) {
		info, ok := s.InfoByPID[pid]
		if !ok {
			continue
		}
		if info.Args == "" {
			lines = append(lines, info.Comm)
			continue
		}
		lines = append(lines, strings.TrimSpace(info.Args))
	}
	return lines
}

// CommandBasename takes the final file name in a command path.
func CommandBasename(command string) string {
	if command == "" {
		return ""
	}
	return path.Base(command)
}

// processMatchesAgent is true when the process IS the agent itself, not
// just mentioning the agent's name somewhere in its command line. Only
// compares the command name (comm, and argv[0]) — comparing the full args
// too would misidentify `fish -c opencode` as opencode.
func processMatchesAgent(info Info, agentName string) bool {
	if CommandBasename(info.Comm) == agentName {
		return true
	}
	argv0, _, _ := strings.Cut(strings.TrimSpace(info.Args), " ")
	if argv0 == "" {
		return false
	}
	return CommandBasename(strings.Trim(argv0, `"`)) == agentName
}
