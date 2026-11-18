package tmux

import (
	"fmt"
	"strings"
)

// Fake is a fake Runner for tests: it returns pre-declared output for exact
// argument lists, and records every command that ran.
//
// This file is NOT a _test.go file. Currently Fake is only used by OTHER test
// files within the tmux package itself — `process` can't use it because
// `tmux` imports `process` (a reverse import would create a cycle), and
// `frametest` doesn't use Fake either, since it runs real tmux via exec
// rather than through Runner. Kept as a non-_test.go file because a _test.go
// file can't be imported from another package: if a later part of the
// project (a new package) needs to build a fake Runner for its tests, Fake
// needs to already live here so it can be exported.
type Fake struct {
	outputs map[string]string
	errs    map[string]error
	calls   [][]string
}

func NewFake() *Fake {
	return &Fake{
		outputs: map[string]string{},
		errs:    map[string]error{},
	}
}

// Set declares the stdout for a command.
func (f *Fake) Set(out string, args ...string) {
	f.outputs[fakeKey(args)] = out
}

// SetErr declares the error for a command.
func (f *Fake) SetErr(err error, args ...string) {
	f.errs[fakeKey(args)] = err
}

// Calls returns every command that ran, in order.
func (f *Fake) Calls() [][]string {
	return f.calls
}

func (f *Fake) Run(args ...string) (string, error) {
	f.calls = append(f.calls, append([]string(nil), args...))

	key := fakeKey(args)
	if err, ok := f.errs[key]; ok {
		return "", err
	}
	if out, ok := f.outputs[key]; ok {
		return out, nil
	}
	// An undeclared command is treated as tmux returning an error, so a test
	// that forgot to declare it sees the degraded path instead of a fake
	// empty string pretending to be success.
	return "", fmt.Errorf("fake tmux: undeclared command: %s", key)
}

func fakeKey(args []string) string {
	return strings.Join(args, "\x00")
}
