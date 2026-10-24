#!/bin/bash
# Build, install and launch Jarvis in the iPhone simulator, paired with the example-machine hub.
#   ./scripts/sim.sh [extra launch args...]
set -euo pipefail
cd "$(dirname "$0")/../ios"
SIM="${SIM:-42860FEB-A909-4D4A-8898-E03921F29F3E}"   # iPhone 17 Pro
xcodegen generate -q
xcodebuild -project Jarvis.xcodeproj -scheme Jarvis -destination "id=$SIM" -derivedDataPath ../build/DerivedData build 2>&1 | grep -E "error:|BUILD (SUCCEEDED|FAILED)" | sort -u
xcrun simctl boot "$SIM" 2>/dev/null || true
xcrun simctl uninstall "$SIM" com.chirpet.jarvis 2>/dev/null || true
xcrun simctl install "$SIM" ../build/DerivedData/Build/Products/Debug-iphonesimulator/Jarvis.app
TOKEN=$(ssh example-machine 'python3 -c "import json,os;print(json.load(open(os.path.expanduser(\"~/.jarvis/hub.json\")))[\"token\"])"')
xcrun simctl terminate "$SIM" com.chirpet.jarvis 2>/dev/null || true
xcrun simctl launch "$SIM" com.chirpet.jarvis -pairURL "jarvis://pair?url=http%3A%2F%2Fhub.example.com%3A18660&token=$TOKEN" "$@" >/dev/null
