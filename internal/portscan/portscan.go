// Package portscan scans the TCP ports that processes on this machine are
// listening on (LISTEN), then attributes each port to a pane's process tree.
//
// Data source: the `lsof -iTCP -sTCP:LISTEN -nP -F pn` command, matching the
// Rust reference implementation (scratchpad/tas/src/port.rs, function
// scan_session_process_snapshot, line 163). We don't read /proc/net/tcp
// because that only exists on Linux; lsof exists on both Linux and macOS so
// the degradation path is simpler: on a machine without lsof (missing
// command, or unsupported platform), Scan returns ok=false and the package
// treats it as having no port data at all — no panic, no logging.
//
// "Localhost port" here means a port on the machine that's actually running
// (as opposed to a port on some other machine over the network), not just
// ports bound specifically to the 127.0.0.1 address: dev servers (vite,
// next, …) usually bind 0.0.0.0, and filtering to loopback-only would miss
// most real-world cases. The reference implementation also doesn't filter
// by address (port.rs, extract_port lines 86-94 only extract the port
// number, ignoring the host part).
//
// This package ONLY provides the data type and a single-scan function;
// repeating the scan every 10 seconds in a background goroutine is the
// caller's job (part 7), not this package's.
package portscan

import (
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"tmux-agent/internal/process"
)

// ListeningPorts maps a process pid to the set of TCP ports it's listening
// on, each list already sorted ascending with no duplicates.
type ListeningPorts map[int][]int

// Scan runs `lsof -iTCP -sTCP:LISTEN -nP -F pn` and parses the result into
// ListeningPorts. ok=false when lsof errors or isn't present on the machine
// — the caller treats this scan round as having no port data (no panic, no
// logging).
func Scan() (ListeningPorts, bool) {
	out, err := exec.Command("lsof", "-iTCP", "-sTCP:LISTEN", "-nP", "-F", "pn").Output()
	if err != nil {
		return nil, false
	}
	return ParseLsofOutput(string(out)), true
}

// ParseLsofOutput parses the output of `lsof -iTCP -sTCP:LISTEN -nP -F pn`.
//
// lsof's "-F pn" format: each line starts with a single field-type
// character followed immediately by the value, with no whitespace in
// between. 'p' is the process pid, and the 'n' lines that follow it (up to
// the next 'p') are the address:port of an open socket — each 'n' line is
// associated with the nearest preceding 'p' pid.
//
// A pure function that never calls lsof — it uses pre-built fixture data
// for tests, independent of whichever real ports happen to be open on the
// machine running the tests.
func ParseLsofOutput(output string) ListeningPorts {
	result := ListeningPorts{}
	currentPID := 0
	havePID := false

	for _, line := range strings.Split(output, "\n") {
		if rest, ok := strings.CutPrefix(line, "p"); ok {
			pid, err := strconv.Atoi(rest)
			havePID = err == nil
			if havePID {
				currentPID = pid
			}
			continue
		}

		rest, ok := strings.CutPrefix(line, "n")
		if !ok || !havePID {
			continue
		}
		port, ok := ExtractPort(rest)
		if !ok {
			continue
		}
		result[currentPID] = insertSorted(result[currentPID], port)
	}

	return result
}

// ExtractPort extracts the port number from the "host:port" address string
// lsof prints (e.g. "127.0.0.1:3000", "*:5173", "[::1]:3000"). Only the
// part after the last ':' is treated as the port, and only the consecutive
// digits from the start of that part are taken — a non-numeric port name
// (e.g. "localhost:http") returns false.
func ExtractPort(name string) (int, bool) {
	trimmed := strings.TrimSpace(name)
	idx := strings.LastIndexByte(trimmed, ':')
	if idx < 0 {
		return 0, false
	}

	tail := trimmed[idx+1:]
	digits := 0
	for digits < len(tail) && tail[digits] >= '0' && tail[digits] <= '9' {
		digits++
	}
	if digits == 0 {
		return 0, false
	}

	port, err := strconv.Atoi(tail[:digits])
	if err != nil {
		return 0, false
	}
	return port, true
}

// insertSorted inserts port into an ascending-sorted list, skipping it if
// already present.
func insertSorted(ports []int, port int) []int {
	i := sort.SearchInts(ports, port)
	if i < len(ports) && ports[i] == port {
		return ports
	}
	ports = append(ports, 0)
	copy(ports[i+1:], ports[i:])
	ports[i] = port
	return ports
}

// ForPane returns the ports that the process tree rooted at panePID (plus
// all its descendants) is listening on, ascending sorted. Reuses
// process.Snapshot.Descendants (part 1) to walk the tree — it doesn't
// discover child processes itself.
func ForPane(listening ListeningPorts, snapshot *process.Snapshot, panePID int) []int {
	if snapshot == nil || len(listening) == 0 {
		return nil
	}

	var ports []int
	for pid := range snapshot.Descendants([]int{panePID}) {
		for _, port := range listening[pid] {
			ports = insertSorted(ports, port)
		}
	}
	return ports
}
