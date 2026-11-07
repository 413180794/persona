#!/bin/bash
# Builds the peer app (apple/IslandPeer: another app's Live Activity, "a ride in 8 minutes") and installs it on the
# given simulators, so Persona' Fleet activity can be photographed sharing the Dynamic Island
# (docs/apple/live-activity.md "Sharing the Dynamic Island with other apps"). Simulator only, unsigned; never shipped.
#
#   scripts/app/island_peer.sh <simulator udid>...
#   TEST_RUNNER_PERSONA_ISLAND_PEER=dev.example.islandpeer … -only-testing:PersonaUITests/LiveActivityShots
set -euo pipefail
cd "$(dirname "$0")/../.."
eval "$(python3 scripts/persona_config.py env)"
DD=build/IslandPeer
(cd apple/IslandPeer && xcodegen generate -q)
xcodebuild -project apple/IslandPeer/IslandPeer.xcodeproj -scheme IslandPeer -destination 'generic/platform=iOS Simulator' \
  -derivedDataPath "$DD" build -quiet
for udid in "$@"; do
  xcrun simctl install "$udid" "$DD/Build/Products/Debug-iphonesimulator/IslandPeer.app"
  echo "installed on $udid"
done
echo "bundle id: dev.example.islandpeer"
