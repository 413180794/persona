#!/bin/bash
# Kept for old notes: jarvis-link looks after its systemd user service itself now (docs/link.md「Linux（headless）」).
#   ./scripts/deploy/link-systemd.sh install     # = ~/.jarvis/bin/jarvis-link install
#   ./scripts/deploy/link-systemd.sh restart     # after a new ~/.jarvis/bin/jarvis-link (install again does the same)
#   ./scripts/deploy/link-systemd.sh uninstall   # = ~/.jarvis/bin/jarvis-link uninstall
#   ./scripts/deploy/link-systemd.sh status      # = ~/.jarvis/bin/jarvis-link status
set -euo pipefail
ROOT=$(python3 "$(dirname "${BASH_SOURCE[0]}")/../persona_identity.py" --service link)
export PERSONA_HOME="$ROOT" JARVIS_HOME="$ROOT"
BIN="$ROOT/bin/jarvis-link"
[ -x "$BIN" ] || { echo "no $BIN: build it (cd server && go build -o ~/.jarvis/bin/jarvis-link ./cmd/jarvis-link)" >&2; exit 1; }
[ $# -gt 0 ] || set -- install
case "$1" in
  install|uninstall|status) exec "$BIN" "$@" ;;
  restart) exec systemctl --user restart jarvis-link.service ;;
  *) echo "usage: $0 [install|restart|uninstall|status]" >&2; exit 2 ;;
esac
