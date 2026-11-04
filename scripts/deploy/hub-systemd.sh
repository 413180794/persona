#!/bin/bash
# Install the existing hub unit using the same resolved root as the deployed binary (Linux: systemd user service;
# Mac: restart the launchd agent that already runs it).
set -euo pipefail
ROOT="${_PERSONA_STATE_DIR:-$(python3 "$(dirname "${BASH_SOURCE[0]}")/../persona_identity.py" --service hub --prepare)}"
export PERSONA_HOME="$ROOT" JARVIS_HOME="$ROOT"
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
  launchctl kickstart -k "gui/$(id -u)/$label"
  echo "hub: restarted launchd agent $label"
  exit 0
fi
python3 - <<'PY'
import os
from pathlib import Path
root = os.environ['PERSONA_HOME']
def quote(value):
    return '"' + value.replace('\\', '\\\\').replace('"', '\\"').replace('%', '%%') + '"'
unit = '\n'.join([
    '[Unit]', 'Description=Persona hub (personad)', 'After=network-online.target', '', '[Service]',
    'ExecStart=' + quote(root + '/bin/jarvisd').replace('$', '$$') + ' hub',
    'Restart=always', 'RestartSec=3',
    'Environment=' + quote('PERSONA_HOME=' + root), 'Environment=' + quote('JARVIS_HOME=' + root),
    'Environment=PATH=%h/.local/bin:%h/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin', '',
    '[Install]', 'WantedBy=default.target', '',
])
path = Path(os.environ.get('XDG_CONFIG_HOME') or str(Path.home() / '.config')) / 'systemd/user/jarvisd.service'
path.parent.mkdir(parents=True, exist_ok=True)
path.write_text(unit)
PY
systemctl --user daemon-reload
systemctl --user enable jarvisd >/dev/null 2>&1
systemctl --user restart jarvisd
systemctl --user is-active jarvisd
