#!/bin/bash
# Run every server test, including the Postgres ones, on the hub machine (it has the Postgres container).
#   ./scripts/test-server.sh [go test args...]
set -euo pipefail
cd "$(dirname "$0")/.."
eval "$(python3 scripts/jarvis_config.py env)"
H="$JARVIS_HUB_SSH"
ssh "$H" 'bash -s' < scripts/db.sh >/dev/null
ssh "$H" "mkdir -p ~/.cache/jarvis-test"
rsync -a --delete --exclude bin server/ "$H":.cache/jarvis-test/server/
ssh "$H" "cd ~/.cache/jarvis-test/server && export PATH=/usr/local/go/bin:\$PATH && \
  JARVIS_TEST_DATABASE_URL=\$(python3 -c 'import json,os;print(json.load(open(os.path.expanduser(\"~/.jarvis/hub.json\")))[\"database_url\"].replace(\"/jarvis?\",\"/jarvis_test?\"))') \
  go test -count=1 -p 1 ${*:-./...}"
