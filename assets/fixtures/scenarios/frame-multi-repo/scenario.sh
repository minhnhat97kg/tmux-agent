#!/usr/bin/env bash
# Golden scenario 3: several repos — panes across git repo roots (group.go)
# and one pane OUTSIDE any repo (keyed by its raw path — group.go:
# groupKeyForPath returns the path when no ".git" is found above it). One
# repo has more than one pane.
#
# Repos sort by name, case-insensitively (GroupPanesByRepo) — "repo-alpha"
# < "repo-beta" < "scratch", so the setup order already matches.
#
# Harness contract: see the header comment of frame-single-agent/scenario.sh.
set -euo pipefail

SOCKET="$1"
ROOT="$(cd "$(dirname "$0")/../../../.." && pwd)"

BIN="$(mktemp -d)/tmux-agent"
(cd "$ROOT" && go build -o "$BIN" ./cmd/tmux-agent)

BASE="$(mktemp -d)"
REPO_ALPHA="$BASE/repo-alpha"
REPO_BETA="$BASE/repo-beta"
LOOSE_DIR="$BASE/scratch"
mkdir -p "$REPO_ALPHA/.git" "$REPO_BETA/.git" "$LOOSE_DIR"

tmux -L "$SOCKET" new-session -d -s main -x 60 -y 34 -c "$REPO_ALPHA"
SIDEBAR_PANE=$(tmux -L "$SOCKET" list-panes -t main -F '#{pane_id}')
tmux -L "$SOCKET" set-option -t "$SIDEBAR_PANE" -p @pane_role sidebar

# One window per agent pane (avoids split-window running out of room in a
# narrow window — see frame-multi-status/scenario.sh).
# spec: pane|dir|status
declare -a PANES=(
    "repo-a-1|$REPO_ALPHA|running"
    "repo-a-2|$REPO_ALPHA|waiting"
    "repo-b-1|$REPO_BETA|idle"
    "loose-1|$LOOSE_DIR|error"
)
i=0
for entry in "${PANES[@]}"; do
    IFS='|' read -r _ dir status <<<"$entry"
    win="agent-$i"
    tmux -L "$SOCKET" new-window -d -t main -n "$win" -c "$dir"
    pane=$(tmux -L "$SOCKET" list-panes -t "main:$win" -F '#{pane_id}')
    tmux -L "$SOCKET" set-option -t "$pane" -p @pane_agent claude
    tmux -L "$SOCKET" set-option -t "$pane" -p @pane_status "$status"
    i=$((i + 1))
done

tmux -L "$SOCKET" respawn-pane -k -t "$SIDEBAR_PANE" \
    "TMUX_AGENT_NOW=1700000000 TMUX_AGENT_FRAME=0 exec $BIN"

sleep 1.5

echo "$SIDEBAR_PANE"
