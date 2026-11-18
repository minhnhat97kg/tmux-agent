#!/usr/bin/env bash
# Golden scenario 2: several panes covering all six statuses, one repo.
# Checks the layout of a longer list: one icon per status (no status shares
# the `·` glyph anymore), and the cursor only on the first row.
#
# Harness contract: see the header comment of frame-single-agent/scenario.sh.
set -euo pipefail

SOCKET="$1"
ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"

BIN="$(mktemp -d)/tmux-agent"
(cd "$ROOT" && go build -o "$BIN" ./cmd/tmux-agent)

REPO_DIR="$(mktemp -d)/repo-status"
mkdir -p "$REPO_DIR/.git"

tmux -L "$SOCKET" new-session -d -s main -x 60 -y 34 -c "$REPO_DIR"
SIDEBAR_PANE=$(tmux -L "$SOCKET" list-panes -t main -F '#{pane_id}')
tmux -L "$SOCKET" set-option -t "$SIDEBAR_PANE" -p @pane_role sidebar

# One WINDOW per status (no splits) — splitting a narrow window repeatedly
# runs out of room ("no space for new pane"); separate windows don't compete,
# and don't affect the captured sidebar window since QuerySessions() lists
# EVERY pane on the server (list-panes -a).
#
# The same agent (claude) everywhere, to avoid the Codex/OpenCode liveness
# probe (query.go: only those two are checked once the pane is back at a
# shell — claude doesn't take that path, so no fake process is needed).
declare -a STATUSES=(running background waiting idle error trang-thai-la)
i=0
for status in "${STATUSES[@]}"; do
    win="agent-$i"
    tmux -L "$SOCKET" new-window -d -t main -n "$win" -c "$REPO_DIR"
    pane=$(tmux -L "$SOCKET" list-panes -t "main:$win" -F '#{pane_id}')
    tmux -L "$SOCKET" set-option -t "$pane" -p @pane_agent claude
    tmux -L "$SOCKET" set-option -t "$pane" -p @pane_status "$status"
    i=$((i + 1))
done

tmux -L "$SOCKET" respawn-pane -k -t "$SIDEBAR_PANE" \
    "TMUX_AGENT_NOW=1700000000 TMUX_AGENT_FRAME=0 exec $BIN"

sleep 1.5

echo "$SIDEBAR_PANE"
