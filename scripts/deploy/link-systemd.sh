#!/bin/bash
# Kept for old notes: persona-link looks after its systemd user service itself now (docs/operations/link.md "Linux (headless)").
#   ./scripts/deploy/link-systemd.sh install     # = ~/.persona/bin/persona-link install
#   ./scripts/deploy/link-systemd.sh restart     # after a new ~/.persona/bin/persona-link (install again does the same)
#   ./scripts/deploy/link-systemd.sh uninstall   # = ~/.persona/bin/persona-link uninstall
#   ./scripts/deploy/link-systemd.sh status      # = ~/.persona/bin/persona-link status
set -euo pipefail
_env=$(python3 "$(dirname "${BASH_SOURCE[0]}")/../persona_identity.py" --shell --service link) || exit $?
eval "$_env"; unset _env
ROOT="$_PERSONA_STATE_DIR"
BIN="$_PERSONA_LINK_BIN"
[ -x "$BIN" ] || { echo "no $BIN: build it (cd server && go build -o ~/.persona/bin/persona-link ./cmd/persona-link)" >&2; exit 1; }
[ $# -gt 0 ] || set -- install
case "$1" in
  install|uninstall|status) exec "$BIN" "$@" ;;
  restart) exec systemctl --user restart "$_PERSONA_LINK_UNIT.service" ;;
  *) echo "usage: $0 [install|restart|uninstall|status]" >&2; exit 2 ;;
esac
