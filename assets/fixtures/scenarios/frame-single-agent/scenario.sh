#!/usr/bin/env bash
# Golden scenario 1: exactly ONE running Claude agent pane. Checks the most
# basic layout — window header, one pane row (icon + name + agent).
#
# Harness contract (internal/frametest/frametest.go): $1 = an isolated tmux
# socket name given by the harness; the script builds the server itself and
# prints the sidebar's pane id on stdout. The harness captures it with
# `capture-pane -p` (no colors) and compares against the golden file.
#
# ONLY use `tmux -L "$SOCKET"` for isolation — do NOT set TMUX_TMPDIR: the
# harness captures with the same `-L "$SOCKET"` from the Go process, and an
# extra TMUX_TMPDIR here would point the two sides at different servers
# without tmux reporting any error.
set -euo pipefail

SOCKET="$1"
ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"

# Build the real sidebar binary into this run's own temp dir — never touch
# bin/tmux-agent of a developer working in parallel.
BIN="$(mktemp -d)/tmux-agent"
(cd "$ROOT" && go build -o "$BIN" ./cmd/tmux-agent)

# Fake repo: a directory with a ".git" entry is enough (group.go only checks
# that ".git" exists, it never runs git) — a fixed name keeps the output
# stable on any machine.
REPO_DIR="$(mktemp -d)/repo-alpha"
mkdir -p "$REPO_DIR/.git"

# Sidebar window: exactly one pane filling it — the pane we capture. A fixed
# size (-x/-y) keeps the layout independent of the runner's terminal.
tmux -L "$SOCKET" new-session -d -s main -x 60 -y 34 -c "$REPO_DIR"
SIDEBAR_PANE=$(tmux -L "$SOCKET" list-panes -t main -F '#{pane_id}')
tmux -L "$SOCKET" set-option -t "$SIDEBAR_PANE" -p @pane_role sidebar

# A second, off-screen window holds the agent pane — the sidebar lists EVERY
# pane on the server (list-panes -a), not just the captured window.
tmux -L "$SOCKET" new-window -d -t main -n agents -c "$REPO_DIR"
AGENT_PANE=$(tmux -L "$SOCKET" list-panes -t main:agents -F '#{pane_id}')
tmux -L "$SOCKET" set-option -t "$AGENT_PANE" -p @pane_agent claude
tmux -L "$SOCKET" set-option -t "$AGENT_PANE" -p @pane_status running

# Start the real sidebar with respawn-pane (not send-keys into a running
# shell) — respawn-pane redraws the pane from scratch, so no echoed
# command/prompt lands before the TUI's first frame.
#
# TMUX_AGENT_NOW/TMUX_AGENT_FRAME are read by internal/clock to freeze time
# and the animation frame. WARNING: only code that calls clock.Now()/
# clock.Frame() is frozen by them — anything using time.Now() directly (e.g.
# the avatar animation) is not. If a golden starts flickering, make that code
# use internal/clock; don't look for the fix in this script.
tmux -L "$SOCKET" respawn-pane -k -t "$SIDEBAR_PANE" \
    "TMUX_AGENT_NOW=1700000000 TMUX_AGENT_FRAME=0 exec $BIN"

# Wait for the first frame: the sidebar queries tmux (list-panes -a + show
# -g) in a tea.Cmd at Init — well within 1.5s on an isolated server.
sleep 1.5

echo "$SIDEBAR_PANE"
