#!/usr/bin/env bash
# Fake scenario used to verify the frametest runner itself: it spins up an
# isolated tmux server, prints fixed content into the pane, then lets the
# runner capture it.
#
# $1 = name of the isolated tmux socket.
set -euo pipefail

SOCKET="$1"

# Run `sh -c` directly as the pane's command instead of typing into the
# user's default interactive shell: an interactive shell (zsh/bash +
# starship/oh-my-zsh...) draws an extra prompt line of its own after the
# command finishes, and that line depends on the machine's current
# git/directory state, which makes the golden flaky between runs. `sh -c`
# has no rc file and no prompt, so the content is always fixed.
tmux -L "$SOCKET" new-session -d -s fake -x 40 -y 6 \
    'sh -c '\''printf "LINE ONE\nLINE TWO\n"; sleep 10'\'''

# Wait for the command to finish printing. Short enough to keep the test
# fast, long enough to avoid flakiness.
sleep 1

# Print the pane id so the runner knows where to capture.
tmux -L "$SOCKET" list-panes -t fake -F '#{pane_id}' | head -1
