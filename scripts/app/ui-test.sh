#!/bin/bash
# Run the UI tests (apple/JarvisUITests) in the simulator against the configured hub (docs/config.md).
#   ./scripts/app/ui-test.sh [machine/session to open]   # default: the newest session of the hub's machine
set -euo pipefail
cd "$(dirname "$0")/../.."
eval "$(python3 scripts/jarvis_config.py env)"
python3 scripts/jarvis_config.py xcconfig
SIM="${SIM:-$JARVIS_SIM}"
RESOLVER=$(python3 scripts/persona_identity.py --service hub --prefix -)
TOKEN=$(ssh "$JARVIS_HUB_SSH" "$RESOLVER
python3 -c 'import json,os;print(json.load(open(os.path.join(os.environ[\"PERSONA_HOME\"],\"hub.json\")))[\"token\"])'")
URL=$(python3 -c 'import sys,urllib.parse;print(urllib.parse.quote(sys.argv[1],safe=""))' "$JARVIS_HUB_URL")
SESSION="${1:-}"
if [ -z "$SESSION" ]; then
  SESSION=$(curl -s -H "Authorization: Bearer $TOKEN" "$JARVIS_HUB_URL/api/v1/sessions" | python3 -c '
import json,sys
d=json.load(sys.stdin); ss=d if isinstance(d,list) else d.get("sessions",[])
live=[s for s in ss if s.get("pane_id") and not s.get("assistant")]
print(live[0]["machine_id"]+"/"+live[0]["session_id"] if live else "")')
fi
cd apple && xcodegen generate -q
TEST_RUNNER_JARVIS_PAIR_URL="jarvis://pair?url=$URL&token=$TOKEN" TEST_RUNNER_JARVIS_OPEN_SESSION="$SESSION" \
  xcodebuild test -project Persona.xcodeproj -scheme Persona -destination "id=$SIM" -derivedDataPath ../build/DerivedData \
  -only-testing:PersonaUITests 2>&1 | grep -E "error:|Test Case|✘|✔|TEST (SUCCEEDED|FAILED)|XCTAssert" | sed 's/token=[^ &]*/token=…/g'
