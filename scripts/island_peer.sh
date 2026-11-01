#!/bin/bash
# Builds the peer app (apple/IslandPeer: another app's Live Activity, "a ride in 8 minutes") and installs it on the
# given simulators, so Jarvis' Fleet activity can be photographed sharing the Dynamic Island
# (docs/live-activity.md「和其他 App 共用灵动岛」). Simulator only, unsigned; never shipped.
#
#   scripts/island_peer.sh <simulator udid>...
#   TEST_RUNNER_JARVIS_ISLAND_PEER=dev.example.islandpeer … -only-testing:JarvisUITests/LiveActivityShots
set -euo pipefail
cd "$(dirname "$0")/.."
eval "$(python3 scripts/jarvis_config.py env)"
DD=build/IslandPeer
(cd apple/IslandPeer && xcodegen generate -q)
xcodebuild -project apple/IslandPeer/IslandPeer.xcodeproj -scheme IslandPeer -destination 'generic/platform=iOS Simulator' \
  -derivedDataPath "$DD" build -quiet
for udid in "$@"; do
  xcrun simctl install "$udid" "$DD/Build/Products/Debug-iphonesimulator/IslandPeer.app"
  echo "installed on $udid"
done
echo "bundle id: dev.example.islandpeer"
