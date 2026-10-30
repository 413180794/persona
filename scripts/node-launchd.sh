#!/bin/bash
# Run on a Mac (in its login session) to keep a resident jarvisd node there (docs/server.md「常驻 node」):
#   ./scripts/node-launchd.sh install    # launchd agent com.jarvis.node, started at login, restarted if it exits
#   ./scripts/node-launchd.sh restart    # after a new binary (deploy.sh does this)
#   ./scripts/node-launchd.sh uninstall  # back to the hub starting the node over ssh
# The hub keeps connecting over ssh as before; `jarvisd node --stdio` relays to this node when it's running, so what
# the node starts runs in the login session and can read the login keychain.
set -euo pipefail
LABEL=com.jarvis.node
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
BIN="$HOME/.jarvis/bin/jarvisd"
DOMAIN="gui/$(id -u)"
case "${1:-install}" in
  install)
    [ -x "$BIN" ] || { echo "no $BIN: deploy this machine first (scripts/deploy.sh)" >&2; exit 1; }
    mkdir -p "$HOME/Library/LaunchAgents" "$HOME/.jarvis"
    cat > "$PLIST" <<PL
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>$LABEL</string>
  <key>ProgramArguments</key><array><string>$BIN</string><string>node</string><string>--attach</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>10</integer>
  <key>StandardErrorPath</key><string>$HOME/.jarvis/node-attach.log</string>
</dict></plist>
PL
    launchctl bootout "$DOMAIN/$LABEL" 2>/dev/null || true
    launchctl bootstrap "$DOMAIN" "$PLIST"
    echo "installed $LABEL" ;;
  restart)
    if launchctl print "$DOMAIN/$LABEL" >/dev/null 2>&1; then launchctl kickstart -k "$DOMAIN/$LABEL" && echo "restarted $LABEL"; fi ;;
  uninstall)
    launchctl bootout "$DOMAIN/$LABEL" 2>/dev/null || true
    rm -f "$PLIST" "$HOME/.jarvis/node-attach.sock"
    echo "removed $LABEL" ;;
  *) echo "usage: $0 [install|restart|uninstall]" >&2; exit 2 ;;
esac
