#!/bin/bash
# Install the existing hub unit using the same resolved root as the deployed binary.
set -euo pipefail
ROOT="${_PERSONA_STATE_DIR:-$(python3 "$(dirname "${BASH_SOURCE[0]}")/../persona_identity.py" --service hub --prepare)}"
export PERSONA_HOME="$ROOT" JARVIS_HOME="$ROOT"
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
