#!/bin/bash
# Run every server test, including the Postgres ones, on example-machine (this Mac has no docker).
#   ./scripts/test-server.sh [go test args...]
set -euo pipefail
cd "$(dirname "$0")/.."
ssh example-machine 'bash -s' < scripts/db.sh >/dev/null
ssh example-machine "mkdir -p ~/.cache/jarvis-test"
rsync -a --delete --exclude bin server/ example-machine:.cache/jarvis-test/server/
ssh example-machine "cd ~/.cache/jarvis-test/server && export PATH=/usr/local/go/bin:\$PATH && \
  JARVIS_TEST_DATABASE_URL=\$(python3 -c 'import json,os;print(json.load(open(os.path.expanduser(\"~/.jarvis/hub.json\")))[\"database_url\"].replace(\"/jarvis?\",\"/jarvis_test?\"))') \
  go test -count=1 -p 1 ${*:-./...}"
