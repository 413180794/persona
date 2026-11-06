#!/bin/bash
# Install the existing hub unit using the same resolved root as the deployed binary (Linux: systemd user service;
# Mac: restart the launchd agent that already runs it). personad hub setup (server/internal/hub/setup_service.go)
# writes the same units: keep the two in step.
set -euo pipefail
ROOT="${_PERSONA_STATE_DIR:-$(python3 "$(dirname "${BASH_SOURCE[0]}")/../persona_identity.py" --service hub --prepare)}"
export PERSONA_HOME="$ROOT" JARVIS_HOME="$ROOT"
# Blue-green (docs/hub-restart.md "Blue-green switch"): the running hub's edge starts the new binary next to the one serving
# and switches once it is up, nothing restarts. It returns 0 once switched; 1 (no edge here, or one older than the new
# binary's protocol: `hub switch` exits 3; or a binary that cannot run it) to have the caller restart the service;
# it exits the script when the new binary did not come up (exit 1: the hub serves on the old one, back in place).
switch_or_restart() {
  local prev=() rc=0
  [ -x "$ROOT/bin/jarvisd.prev" ] && prev=(--prev "$ROOT/bin/jarvisd.prev")
  "$ROOT/bin/jarvisd" hub switch --timeout 8m ${prev[@]+"${prev[@]}"} || rc=$?
  case $rc in
    0) echo "hub: switched to the new binary (blue-green, nothing restarted)"; return 0 ;;
    1) ;;
    *) echo "hub: no edge to switch on (hub switch: exit $rc): restarting the service"; return 1 ;;
  esac
  if [ ${#prev[@]} -gt 0 ]; then
    cp -p "$ROOT/bin/jarvisd.prev" "$ROOT/bin/jarvisd.new" && mv "$ROOT/bin/jarvisd.new" "$ROOT/bin/jarvisd"
    echo "hub: the new binary did not take over; the old one serves on and is back in place" >&2
  else
    echo "hub: the new binary did not take over; the old one serves on" >&2
  fi
  exit 1
}
if [ "$(uname -s)" = Darwin ] && ! command -v systemctl >/dev/null 2>&1; then
  # A Mac hub runs under a launchd agent the user set up (no systemd): restart the one whose program is this root's
  # jarvisd with "hub" (through ~/.jarvis or ~/.persona, whichever path it names). Nothing is installed here.
  label=$(python3 - <<'PY'
import glob, os, plistlib
want = os.path.realpath(os.path.join(os.environ['PERSONA_HOME'], 'bin/jarvisd'))
for path in sorted(glob.glob(os.path.expanduser('~/Library/LaunchAgents/*.plist'))):
    try:
        with open(path, 'rb') as f:
            job = plistlib.load(f)
    except Exception:
        continue
    args = job.get('ProgramArguments') or ([job['Program']] if job.get('Program') else [])
    if len(args) >= 2 and args[1] == 'hub' and os.path.realpath(os.path.expanduser(args[0])) == want:
        print(job.get('Label', ''))
        break
PY
)
  if [ -z "$label" ]; then
    echo "hub: no launchd agent in ~/Library/LaunchAgents runs $ROOT/bin/jarvisd hub; restart the hub yourself" >&2
    exit 1
  fi
  switch_or_restart && exit 0
  launchctl kickstart -k "gui/$(id -u)/$label"
  echo "hub: restarted launchd agent $label"
  exit 0
fi
# The listening sockets belong to jarvisd.socket (socket activation, docs/hub-restart.md "No gap"): restarting the
# hub never closes the port, connections made meanwhile wait for the next process instead of being refused.
# PERSONA_HUB_SOCKET=0 goes back to the hub listening itself (the rollback): the socket unit is removed.
UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
SOCKET_FILE="$UNIT_DIR/jarvisd.socket"
SERVICE_FILE="$UNIT_DIR/jarvisd.service"
socket_before=$(cat "$SOCKET_FILE" 2>/dev/null || true)
service_before=$(cat "$SERVICE_FILE" 2>/dev/null || true)
PERSONA_HUB_SOCKET="${PERSONA_HUB_SOCKET:-1}" python3 - <<'PY'
import json, os, re
from pathlib import Path
root = os.environ['PERSONA_HOME']
def quote(value):
    return '"' + value.replace('\\', '\\\\').replace('"', '\\"').replace('%', '%%') + '"'
def stream(addr):
    # hub.json's Go address as a ListenStream: ":18660" (every address) is just the port for systemd.
    return addr[1:] if addr.startswith(':') else addr
try:
    cfg = json.loads((Path(root) / 'hub.json').read_text())
except (OSError, ValueError):
    cfg = {}
listens = [cfg.get('listen') or ':18660']
if (cfg.get('public') or {}).get('tls_listen'):
    listens.append(cfg['public']['tls_listen'])
use_socket = os.environ['PERSONA_HUB_SOCKET'] != '0'
unit_dir = Path(os.environ.get('XDG_CONFIG_HOME') or str(Path.home() / '.config')) / 'systemd/user'
# PERSONA_HUB_EDGE=0 runs the hub without its edge (docs/hub-restart.md "Blue-green switch"): what was given once stays in the unit
# across deploys until it is given again (PERSONA_HUB_EDGE=1 goes back to the edge).
edge = os.environ.get('PERSONA_HUB_EDGE')
if edge is None:
    try:
        m = re.search(r'PERSONA_HUB_EDGE=([^"\s]*)', (unit_dir / 'jarvisd.service').read_text())
        edge = m.group(1) if m else ''
    except OSError:
        edge = ''
if edge == '1':
    edge = ''
unit_dir.mkdir(parents=True, exist_ok=True)
unit = [
    '[Unit]', 'Description=Persona hub (personad)', 'After=network-online.target',
]
if use_socket:
    unit += ['Wants=jarvisd.socket', 'After=jarvisd.socket']
unit += [
    '', '[Service]',
    'ExecStart=' + quote(root + '/bin/jarvisd').replace('$', '$$') + ' hub',
    'Restart=always', 'RestartSec=3', 'TimeoutStopSec=20',
    'Environment=' + quote('PERSONA_HOME=' + root), 'Environment=' + quote('JARVIS_HOME=' + root),
    'Environment=PATH=%h/.local/bin:%h/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin',
    *(['Environment=' + quote('PERSONA_HUB_EDGE=' + edge)] if edge else []), '',
    '[Install]', 'WantedBy=default.target', '',
]
(unit_dir / 'jarvisd.service').write_text('\n'.join(unit))
socket = unit_dir / 'jarvisd.socket'
if use_socket:
    socket.write_text('\n'.join([
        '[Unit]', "Description=Persona hub's listening sockets (docs/hub-restart.md)", '', '[Socket]',
        *['ListenStream=' + stream(a) for a in listens],
        # An address-bound listener (personad hub setup binds TLS to the LAN address) comes up even while that
        # address is gone (DHCP) instead of failing the socket and the plaintext port with it. Same as hub/setup_service.go.
        'Backlog=4096', 'NoDelay=true', *(['FreeBind=true'] if any(not a.startswith(':') for a in listens) else []),
        'Service=jarvisd.service', '',
        '[Install]', 'WantedBy=sockets.target', '',
    ]))
elif socket.exists():
    socket.unlink()
PY
socket_after=$(cat "$SOCKET_FILE" 2>/dev/null || true)
service_after=$(cat "$SERVICE_FILE" 2>/dev/null || true)
systemctl --user daemon-reload
systemctl --user enable jarvisd >/dev/null 2>&1
# The units as they were and the hub running: switch to the new binary instead of restarting.
if [ "$socket_before" = "$socket_after" ] && [ "$service_before" = "$service_after" ] && systemctl --user is-active --quiet jarvisd; then
  if switch_or_restart; then
    systemctl --user is-active jarvisd
    exit 0
  fi
fi
if [ -z "$socket_after" ]; then
  # Rolling back (or never used): the hub listens itself again once the socket lets go of the port.
  if systemctl --user is-active --quiet jarvisd.socket 2>/dev/null || [ -n "$socket_before" ]; then
    systemctl --user stop jarvisd
    systemctl --user disable --now jarvisd.socket >/dev/null 2>&1 || true
    systemctl --user start jarvisd
  else
    systemctl --user restart jarvisd
  fi
elif ! systemctl --user is-active --quiet jarvisd.socket || [ "$socket_before" != "$socket_after" ]; then
  # First time, or other addresses: the running hub holds the port itself; one stop to hand it to the socket.
  systemctl --user stop jarvisd
  systemctl --user enable jarvisd.socket >/dev/null 2>&1
  systemctl --user restart jarvisd.socket
  systemctl --user start jarvisd
else
  systemctl --user restart jarvisd # the socket keeps listening: nothing is refused meanwhile
fi
systemctl --user is-active jarvisd
