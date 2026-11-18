#!/usr/bin/env bash
# tmux-agent installer.
#
#   curl -fsSL https://raw.githubusercontent.com/minhnhat97kg/tmux-agent/main/install.sh | bash
#
# Clones (or updates) the repo, builds the binary, links it into
# ~/.local/bin, hooks the plugin into tmux.conf (once), loads it into a
# running tmux, and integrates the agent harnesses it finds (`tmux-agent
# install`). Safe to re-run: that is also how you upgrade.
#
# Environment:
#   TMUX_AGENT_REPO     git URL to install from
#   TMUX_AGENT_REF      branch/tag (default: main)
#   TMUX_AGENT_DIR      checkout location (default: ~/.local/share/tmux-agent)
#   TMUX_AGENT_BIN_DIR  where the `tmux-agent` link goes (default: ~/.local/bin)
#   TMUX_AGENT_NO_TMUX=1     don't touch tmux.conf or the running tmux
#   TMUX_AGENT_NO_HARNESS=1  don't run `tmux-agent install` (hooks/skills)
set -euo pipefail

REPO="${TMUX_AGENT_REPO:-https://github.com/minhnhat97kg/tmux-agent.git}"
REF="${TMUX_AGENT_REF:-main}"
DIR="${TMUX_AGENT_DIR:-$HOME/.local/share/tmux-agent}"
BIN_DIR="${TMUX_AGENT_BIN_DIR:-$HOME/.local/bin}"

say() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m!!\033[0m %s\n' "$*" >&2; }
die() { printf '\033[1;31mxx\033[0m %s\n' "$*" >&2; exit 1; }

# ── requirements ─────────────────────────────────────────────────────────
command -v git >/dev/null || die "git is required"
command -v go >/dev/null || die "Go is required to build tmux-agent (https://go.dev/dl/) — need the version in go.mod or newer"
command -v tmux >/dev/null || die "tmux is required"
tmux_version="$(tmux -V | sed -E 's/[^0-9]*([0-9]+)\.([0-9]+).*/\1 \2/')"
read -r tmux_major tmux_minor <<<"$tmux_version"
if [ "${tmux_major:-0}" -lt 2 ] || { [ "$tmux_major" -eq 2 ] && [ "${tmux_minor:-0}" -lt 9 ]; }; then
  die "tmux >= 2.9 is required (found $(tmux -V))"
fi

# ── fetch ────────────────────────────────────────────────────────────────
if [ -d "$DIR/.git" ]; then
  say "Updating $DIR"
  git -C "$DIR" fetch --quiet origin "$REF"
  git -C "$DIR" checkout --quiet "$REF"
  git -C "$DIR" merge --quiet --ff-only "origin/$REF"
else
  say "Cloning $REPO into $DIR"
  mkdir -p "$(dirname "$DIR")"
  git clone --quiet --branch "$REF" "$REPO" "$DIR"
fi

# ── build + link ─────────────────────────────────────────────────────────
say "Building"
(cd "$DIR" && go build -o bin/tmux-agent ./cmd/tmux-agent)
mkdir -p "$BIN_DIR"
ln -sfn "$DIR/bin/tmux-agent" "$BIN_DIR/tmux-agent"
say "Linked $BIN_DIR/tmux-agent"
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) warn "$BIN_DIR is not on your PATH — add: export PATH=\"$BIN_DIR:\$PATH\"" ;;
esac

# ── tmux ─────────────────────────────────────────────────────────────────
if [ "${TMUX_AGENT_NO_TMUX:-}" != 1 ]; then
  conf="$HOME/.tmux.conf"
  if [ ! -f "$conf" ] && [ -f "${XDG_CONFIG_HOME:-$HOME/.config}/tmux/tmux.conf" ]; then
    conf="${XDG_CONFIG_HOME:-$HOME/.config}/tmux/tmux.conf"
  fi
  line="run-shell '$DIR/assets/tmux-agent.tmux'"
  if [ -f "$conf" ] && grep -qF "$DIR/assets/tmux-agent.tmux" "$conf"; then
    say "tmux.conf already loads tmux-agent ($conf)"
  else
    printf '\n# tmux-agent\n%s\n' "$line" >>"$conf"
    say "Added to $conf: $line"
  fi
  if tmux info >/dev/null 2>&1; then
    tmux run-shell "$DIR/assets/tmux-agent.tmux"
    "$DIR/bin/tmux-agent" reload >/dev/null || true
    say "Loaded into the running tmux server"
  fi
fi

# ── agent harnesses ──────────────────────────────────────────────────────
if [ "${TMUX_AGENT_NO_HARNESS:-}" != 1 ]; then
  say "Integrating agent harnesses (hooks / plugins / skills)"
  "$DIR/bin/tmux-agent" install || warn "no supported harness found — later: tmux-agent install <harness>"
fi

cat <<EOF

$(printf '\033[1;32m✓ tmux-agent installed\033[0m')

  Sidebar         prefix + e   (all windows: prefix + E)
  Dashboard       prefix + A   (or o inside the sidebar)
  CLI             tmux-agent list | new | send | wait | ctx | …

  Harnesses installed later:  tmux-agent install <harness>   (--list to see all)
  Remove an integration:      tmux-agent uninstall <harness>

EOF
