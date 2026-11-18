// Package assets holds shell/JSON/JS files copied from upstream
// hiroppy/tmux-agent-sidebar (MIT). See NOTICE.
//
// This package has no runnable code — it exists so that `assets_test.go`
// has a valid package, and so `go build ./...` doesn't report
// "no non-test Go files".
package assets

import _ "embed"

// OpenCodePlugin is the OpenCode bridge plugin, copied into OpenCode's
// global plugin directory by `tmux-agent install opencode`.
//
//go:embed .opencode/plugins/tmux-agent-sidebar.js
var OpenCodePlugin string
