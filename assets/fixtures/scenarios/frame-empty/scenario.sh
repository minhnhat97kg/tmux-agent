#!/usr/bin/env bash
# Golden scenario 4: empty list — no agent pane on the server. Checks the
# empty branch of renderRows: prints only emptyHint (view.go), no panic, no
# group header.
#
# Harness contract: see the header comment of frame-single-agent/scenario.sh.
set -euo pipefail

SOCKET="$1"
ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"

BIN="$(mktemp -d)/tmux-agent"
(cd "$ROOT" && go build -o "$BIN" ./cmd/tmux-agent)

WORKDIR="$(mktemp -d)"

tmux -L "$SOCKET" new-session -d -s main -x 60 -y 34 -c "$WORKDIR"
SIDEBAR_PANE=$(tmux -L "$SOCKET" list-panes -t main -F '#{pane_id}')
tmux -L "$SOCKET" set-option -t "$SIDEBAR_PANE" -p @pane_role sidebar

# Deliberately no other agent pane — the server only has the sidebar pane.
tmux -L "$SOCKET" respawn-pane -k -t "$SIDEBAR_PANE" \
    "TMUX_AGENT_NOW=1700000000 TMUX_AGENT_FRAME=0 exec $BIN"

sleep 1.5

echo "$SIDEBAR_PANE"
