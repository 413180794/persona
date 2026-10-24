#!/bin/bash
# Archive a Release build (iPhone app with the embedded watch app) and upload it to App Store Connect.
#
#   ./scripts/release.sh [build-number]            # archive + check + upload
#   NO_UPLOAD=1 ./scripts/release.sh               # archive + check + export only (build-release/export)
#
# Uses the App Store Connect API key (ASC_KEY_ID / ASC_ISSUER_ID / ASC_KEY_PATH). Builds into build-release/ so it
# never touches the simulator build. The export re-signs manually with the "Jarvis App Store" / "Jarvis Watch App
# Store" profiles (create them first with scripts/asc_signing.py): the App Manager API key can't cloud-sign.
set -euo pipefail
cd "$(dirname "$0")/.."
ASC_KEY_ID="${ASC_KEY_ID:-KEYID00001}"
ASC_ISSUER_ID="${ASC_ISSUER_ID:-00000000-0000-0000-0000-000000000000}"
ASC_KEY_PATH="${ASC_KEY_PATH:-$HOME/.appstoreconnect/AuthKey_$ASC_KEY_ID.p8}"
OUT=build-release
BUILD="${1:-$(date +%Y%m%d%H%M)}"
# Keep every archive where Xcode Organizer looks, so each version's dSYMs stay around for symbolicating crashes.
ARCHIVE="$HOME/Library/Developer/Xcode/Archives/$(date +%Y-%m-%d)/Jarvis $BUILD.xcarchive"
AUTH=(-allowProvisioningUpdates -authenticationKeyPath "$ASC_KEY_PATH" -authenticationKeyID "$ASC_KEY_ID" -authenticationKeyIssuerID "$ASC_ISSUER_ID")

mkdir -p "$OUT"
cd ios
xcodegen generate -q
cd ..

xcodebuild -project ios/Jarvis.xcodeproj -scheme Jarvis -configuration Release \
  -destination 'generic/platform=iOS' -archivePath "$ARCHIVE" -derivedDataPath "$OUT/DerivedData" \
  CURRENT_PROJECT_VERSION="$BUILD" "${AUTH[@]}" clean archive | grep -E "error:|ARCHIVE (SUCCEEDED|FAILED)"

APP="$ARCHIVE/Products/Applications/Jarvis.app"
WATCH="$APP/Watch/Jarvis.app"
# Placeholder purpose strings get the build rejected automatically: stop before uploading.
python3 scripts/check_bundle_strings.py "$APP"
[ -d "$WATCH" ] && python3 scripts/check_bundle_strings.py "$WATCH"
# Release must not contain the DEBUG-only simulator switches (ios/Jarvis/App/JarvisApp.swift).
if strings "$APP/Jarvis" | grep -qxE '(pairURL|openSession|showNewChat)'; then
  echo "DEBUG switches found in the Release binary" >&2; exit 1
fi

OPTS=scripts/ExportOptions.plist
if [ -n "${NO_UPLOAD:-}" ]; then
  OPTS=$(mktemp -t export).plist
  sed 's#<string>upload</string>#<string>export</string>#' scripts/ExportOptions.plist > "$OPTS"
  rm -rf "$OUT/export"
fi
# The archive itself is signed automatically with a development profile (that is why its aps-environment reads
# `development`); this step re-signs both bundles with the App Store profiles, which is where the shipped binary gets
# aps-environment=production.
xcodebuild -exportArchive -archivePath "$ARCHIVE" -exportOptionsPlist "$OPTS" -exportPath "$OUT/export" "${AUTH[@]}" \
  | grep -E "error:|EXPORT (SUCCEEDED|FAILED)|Upload"

if [ -n "${NO_UPLOAD:-}" ]; then
  # Check the binary that would have been uploaded: re-signed entitlements must carry production push.
  rm -rf "$OUT/ipa"
  unzip -o -q "$OUT/export/Jarvis.ipa" -d "$OUT/ipa"
  if ! codesign -d --entitlements :- "$OUT/ipa/Payload/Jarvis.app" 2>/dev/null | grep -q "<key>aps-environment</key><string>production</string>"; then
    echo "aps-environment is not production in the exported ipa:" >&2
    codesign -d --entitlements :- "$OUT/ipa/Payload/Jarvis.app" 2>/dev/null >&2
    exit 1
  fi
  rm -rf "$OUT/ipa"
fi
git tag -f "testflight/$BUILD" >/dev/null 2>&1 || true
if [ -n "${NO_UPLOAD:-}" ]; then
  echo "Build $BUILD archived and exported to $OUT/export (not uploaded)."
else
  echo "Build $BUILD uploaded. It appears in App Store Connect → TestFlight in ~10–30 min (python3 scripts/asc_testflight.py status)."
fi
