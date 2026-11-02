#!/bin/bash
# Run on a machine (a Mac in its login session, or Linux) to keep a resident jarvisd node there (docs/server.md「常驻 node」):
#   ./scripts/deploy/node-launchd.sh install    # Mac: launchd agent com.jarvis.node; Linux: systemd user service jarvisd-node;
#                                        # started at login / boot, restarted whenever it exits
#   ./scripts/deploy/node-launchd.sh restart    # after a new binary (deploy.sh does this)
#   ./scripts/deploy/node-launchd.sh uninstall  # back to the hub starting the node over ssh
# The hub keeps connecting over ssh as before; `jarvisd node --stdio` relays to this node when it's running, so what
# the node starts runs in the login session and can read the login keychain. Set up for tailcat
# (`jarvisd node tailcat init`), the resident node is also how the hub reaches the machine when ssh cannot.
set -euo pipefail
BIN="$HOME/.jarvis/bin/jarvisd"

if [ "$(uname -s)" = Linux ]; then
  UNIT=jarvisd-node.service
  UNIT_FILE="$HOME/.config/systemd/user/$UNIT"
  case "${1:-install}" in
    install)
      [ -x "$BIN" ] || { echo "no $BIN: deploy this machine first (scripts/deploy/deploy.sh)" >&2; exit 1; }
      mkdir -p "$(dirname "$UNIT_FILE")" "$HOME/.jarvis"
      cat > "$UNIT_FILE" <<UN
[Unit]
Description=Jarvis resident node (docs/server.md)
After=network-online.target

[Service]
ExecStart=$BIN node --attach
Restart=always
RestartSec=10
StandardError=append:$HOME/.jarvis/node-attach.log

[Install]
WantedBy=default.target
UN
      systemctl --user daemon-reload
      systemctl --user enable --now "$UNIT"
      # Without linger the user's services stop when the last session ends (and do not start at boot).
      loginctl show-user "$(id -un)" -p Linger 2>/dev/null | grep -q yes ||
        echo "note: run 'sudo loginctl enable-linger $(id -un)' so it keeps running with nobody logged in" >&2
      echo "installed $UNIT" ;;
    restart)
      if systemctl --user is-enabled "$UNIT" >/dev/null 2>&1; then systemctl --user restart "$UNIT" && echo "restarted $UNIT"; fi ;;
    uninstall)
      systemctl --user disable --now "$UNIT" 2>/dev/null || true
      rm -f "$UNIT_FILE" "$HOME/.jarvis/node-attach.sock"
      systemctl --user daemon-reload 2>/dev/null || true
      echo "removed $UNIT" ;;
    *) echo "usage: $0 [install|restart|uninstall]" >&2; exit 2 ;;
  esac
  exit 0
fi

LABEL=com.jarvis.node
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
DOMAIN="gui/$(id -u)"
case "${1:-install}" in
  install)
    [ -x "$BIN" ] || { echo "no $BIN: deploy this machine first (scripts/deploy/deploy.sh)" >&2; exit 1; }
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
