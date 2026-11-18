package portscan

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"tmux-agent/internal/process"
)

// The tests below must NOT depend on real ports open on the machine running
// the tests: all lsof data is pre-built fixture strings, even when a test
// calls through Scan() (in which case a fake lsof script stands in for the
// real command).

func TestExtractPort_CommonFormats(t *testing.T) {
	cases := []struct {
		name    string
		wantOK  bool
		wantVal int
	}{
		{"127.0.0.1:3000", true, 3000},
		{"*:5173", true, 5173},
		{"[::1]:8080", true, 8080},
		{"0.0.0.0:443", true, 443},
		{"localhost:http", false, 0},
		{"no-colon-here", false, 0},
	}

	for _, tc := range cases {
		got, ok := ExtractPort(tc.name)
		if ok != tc.wantOK || got != tc.wantVal {
			t.Errorf("ExtractPort(%q) = (%d, %v), want (%d, %v)", tc.name, got, ok, tc.wantVal, tc.wantOK)
		}
	}
}

func TestParseLsofOutput_PortsGroupedByPidInOrderOfAppearance(t *testing.T) {
	sample := "p123\nn127.0.0.1:3000\np456\nn*:5173\n"

	got := ParseLsofOutput(sample)
	want := ListeningPorts{
		123: {3000},
		456: {5173},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseLsofOutput(%q) = %v, want %v", sample, got, want)
	}
}

func TestParseLsofOutput_OnePidManyPortsCollectedAndSorted(t *testing.T) {
	sample := "p10\nn*:5173\nn127.0.0.1:3000\nn*:5173\n"

	got := ParseLsofOutput(sample)
	want := ListeningPorts{10: {3000, 5173}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseLsofOutput(%q) = %v, want %v", sample, got, want)
	}
}

func TestParseLsofOutput_NLineWithoutPrecedingPidIgnored(t *testing.T) {
	sample := "n127.0.0.1:3000\np1\nn*:9999\n"

	got := ParseLsofOutput(sample)
	want := ListeningPorts{1: {9999}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseLsofOutput(%q) = %v, want %v", sample, got, want)
	}
}

func TestParseLsofOutput_EmptyInputReturnsEmptyMap(t *testing.T) {
	got := ParseLsofOutput("")
	if len(got) != 0 {
		t.Errorf("ParseLsofOutput(\"\") = %v, want empty", got)
	}
}

func TestForPane_OnlyPortsOfProcessesInPaneTree(t *testing.T) {
	// Tree: 10 (pane, zsh) -> 11 (node, listening on 3000) ; process 99 is
	// not in the pane's tree, so its port must not be counted.
	snapshot := process.FromPSOutput(
		"10 1 zsh zsh\n11 10 node node server.js\n99 1 node node other.js\n",
	)
	listening := ListeningPorts{
		11: {3000},
		99: {4000},
	}

	got := ForPane(listening, snapshot, 10)
	want := []int{3000}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ForPane = %v, want %v", got, want)
	}
}

func TestForPane_PaneListeningOnNoPortReturnsEmpty(t *testing.T) {
	snapshot := process.FromPSOutput("10 1 zsh zsh\n11 10 git git status\n")
	listening := ListeningPorts{99: {4000}}

	got := ForPane(listening, snapshot, 10)
	if len(got) != 0 {
		t.Errorf("ForPane = %v, want empty", got)
	}
}

func TestForPane_NilSnapshotReturnsEmptyDoesNotPanic(t *testing.T) {
	got := ForPane(ListeningPorts{1: {80}}, nil, 1)
	if len(got) != 0 {
		t.Errorf("ForPane with a nil snapshot = %v, want empty", got)
	}
}

// writeFakeLsof builds a script named "lsof", executable, in a temp
// directory, then points PATH at that directory — so Scan() calling "lsof"
// actually runs this fake script instead of the real lsof on the machine.
// This keeps the test independent of real open ports.
func writeFakeLsof(t *testing.T, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake script uses a sh shebang, skipped on windows")
	}

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "lsof")
	if err := os.WriteFile(scriptPath, []byte(body), 0o755); err != nil {
		t.Fatalf("could not write fake lsof script: %v", err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestScan_ReturnsOkFalseWhenLsofFails(t *testing.T) {
	writeFakeLsof(t, "#!/bin/sh\nexit 1\n")

	_, ok := Scan()
	if ok {
		t.Errorf("Scan() ok = true, want false when lsof fails")
	}
}

func TestScan_ParsesFakeScriptOutput(t *testing.T) {
	writeFakeLsof(t, "#!/bin/sh\nprintf 'p321\\nn127.0.0.1:3000\\n'\n")

	got, ok := Scan()
	if !ok {
		t.Fatalf("Scan() ok = false, want true")
	}
	want := ListeningPorts{321: {3000}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Scan() = %v, want %v", got, want)
	}
}
