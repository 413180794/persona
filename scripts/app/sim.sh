#!/bin/bash
# Build, install and launch Jarvis in the iPhone simulator, paired with the hub of the config (docs/config.md).
#   ./scripts/app/sim.sh [extra launch args...]
set -euo pipefail
cd "$(dirname "$0")/../.."
eval "$(python3 scripts/jarvis_config.py env)"
python3 scripts/jarvis_config.py xcconfig
cd apple
SIM="${SIM:-$JARVIS_SIM}"
[ -n "$SIM" ] || { echo "set SIM=<udid> or \"simulator\" in the config (docs/config.md)" >&2; exit 2; }
xcodegen generate -q
xcodebuild -project Persona.xcodeproj -scheme Persona -destination "id=$SIM" -derivedDataPath ../build/DerivedData build 2>&1 | grep -E "error:|BUILD (SUCCEEDED|FAILED)" | sort -u
xcrun simctl boot "$SIM" 2>/dev/null || true
xcrun simctl uninstall "$SIM" "$JARVIS_BUNDLE_ID" 2>/dev/null || true
xcrun simctl install "$SIM" ../build/DerivedData/Build/Products/Debug-iphonesimulator/Persona.app
TOKEN=$(ssh "$JARVIS_HUB_SSH" 'python3 -c "import json,os;print(json.load(open(os.path.expanduser(\"~/.jarvis/hub.json\")))[\"token\"])"')
URL=$(python3 -c 'import sys,urllib.parse;print(urllib.parse.quote(sys.argv[1],safe=""))' "$JARVIS_HUB_URL")
xcrun simctl terminate "$SIM" "$JARVIS_BUNDLE_ID" 2>/dev/null || true
xcrun simctl launch "$SIM" "$JARVIS_BUNDLE_ID" -pairURL "jarvis://pair?url=$URL&token=$TOKEN" "$@" >/dev/null
