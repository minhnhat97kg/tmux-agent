#!/usr/bin/env bash
# Thin wrapper: forwards to the Go binary. Called by Claude Code / Codex
# hooks (settings.json).
#
# Derived from hiroppy/tmux-agent-sidebar (MIT) — see NOTICE.
#
# Why this file exists even though `tmux-agent setup` could write an absolute
# binary path:
#
# 1. Late binding. settings.json only needs to know where hook.sh is. The
#    binary is resolved on every hook run, so rebuilding or moving the plugin
#    doesn't require regenerating the agent config.
# 2. Quiet absence. If the binary isn't there (being rebuilt, freshly
#    cloned), the script exits 0 silently so the agent session sees no hook
#    error.
#
# Keep this wrapper small and side-effect free. All logic that understands
# event semantics belongs to the `hook` subcommand in Go.

# hook.sh lives in assets/, the binary in <repo>/bin/ — go up one level.
PLUGIN_DIR="$(cd "$(dirname "$0")/.." && pwd -P)"
# Fallback when the script runs from a Claude Code plugin install
# (${CLAUDE_PLUGIN_ROOT}/assets/hook.sh). The plugin cache holds no binary,
# so look in the tmux plugin directory where TPM puts it.
TPM_DIR="$HOME/.tmux/plugins/tmux-agent"

if [ -x "$PLUGIN_DIR/bin/tmux-agent" ]; then
  BIN="$PLUGIN_DIR/bin/tmux-agent"
elif [ -x "$TPM_DIR/bin/tmux-agent" ]; then
  BIN="$TPM_DIR/bin/tmux-agent"
elif command -v tmux-agent &>/dev/null; then
  BIN="tmux-agent"
else
  exit 0
fi

exec "$BIN" hook "$@"
