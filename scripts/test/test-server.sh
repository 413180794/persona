#!/bin/bash
# Run every server test, including the Postgres ones, on the hub machine (it has the Postgres container).
#   ./scripts/test/test-server.sh [go test args...]
set -euo pipefail
cd "$(dirname "$0")/../.."
eval "$(python3 scripts/jarvis_config.py env)"
H="$JARVIS_HUB_SSH"
# One copy per checkout: worktrees testing at the same time must not rsync over each other.
D=".cache/jarvis-test-$(basename "$PWD")"
ssh "$H" 'bash -s' < scripts/deploy/db.sh >/dev/null
ssh "$H" "mkdir -p ~/$D"
rsync -a --delete --exclude bin server/ "$H":"$D/server/"
# Wire, continuity and resolution tests read the shared fixtures next to server/ (../../../spec from a package),
# and the resolution ones check the apps' Swift sources against them.
rsync -a --delete spec/ "$H":"$D/spec/"
rsync -a --delete --prune-empty-dirs --include '*/' --include '*.swift' --exclude '*' apple/ "$H":"$D/apple/"
ssh "$H" "cd ~/$D/server && export PATH=/usr/local/go/bin:\$PATH && \
  export JARVIS_TEST_DATABASE_URL=\$(python3 -c 'import json,os;print(json.load(open(os.path.expanduser(\"~/.jarvis/hub.json\")))[\"database_url\"].replace(\"/jarvis?\",\"/jarvis_test?\"))') && \
  ${GOCMD:-go test -count=1 -p 1} ${*:-./...}"
