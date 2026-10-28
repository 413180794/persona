#!/bin/bash
# Archive a Release build (iPhone app with the embedded watch app) and upload it to App Store Connect.
#
#   ./scripts/release.sh [build-number]            # archive + check + upload
#   NO_UPLOAD=1 ./scripts/release.sh               # archive + check + export only (build-release/export)
#
# The App Store Connect API key, bundle id and team come from the config (docs/config.md). Builds into build-release/
# so it never touches the simulator build. The export re-signs manually with the "Jarvis App Store" / "Jarvis Watch
# App Store" profiles (create them first with scripts/asc_signing.py): the App Manager API key can't cloud-sign.
set -euo pipefail
cd "$(dirname "$0")/.."
eval "$(python3 scripts/jarvis_config.py env)"
python3 scripts/jarvis_config.py xcconfig
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
if [ -d "$WATCH" ]; then python3 scripts/check_bundle_strings.py "$WATCH"; fi
# Siri / Shortcuts only see the intents if their metadata was extracted at build time.
[ -f "$APP/Metadata.appintents/extract.actionsdata" ] || { echo "missing AppIntents metadata in $APP"; exit 1; }
# Release must not contain the DEBUG-only simulator switches (ios/Jarvis/App/JarvisApp.swift).
if strings "$APP/Jarvis" | grep -qxE '(pairURL|openSession|showNewChat|openAssistant|newAssistant|openFirstImage|openTerminal|showPad|showInfo|loadAll|fromSeq|lark-connector|lark-connectorName|larkTop|lark-connectorTopic|larkFocus|inboxDrawer|larkProfile|larkPicker|lark-connectorAt|larkMoments|openWorkbench|addMail|dropUsers|openPane|simulateTap|showMenu|openFile|jumpEnd|showOldest|openWork|openAgent|focusComposer|terminalPages|openEditor|openLinkURL|openChatInfo|openChatInfoPage|openChatInfoFrom|openThreadAt|openPeople|openMomentsTab|openMailLabel|openPeopleQuery|openFeishuDoc|openFeishuDocHeading|showFeishuDocSheet|openFeishuWikiSpace|openFeishuWikiNode|showFeishuWikiUpdates|openSearch|openSearchTab|openSearchFeed|openSearchFilter|openSearchFirst|openCalendarDate|openCalendarEvent|showCalendarNow|showDetailBottom|openMeeting|openMinutes|showMinutesTab|showMinutesPlaying|showMinutesSearch|openTask|openTaskScope|openApprovalTab|openApproval|openOKR|showWorkBottom|showFeishuDocTableEnd|openMailThread|showMailAttachment|showMailBottom|showCloseAgent|openSpaceFromChat|showShareCard|showCompactChrome|showWakeDrawer|showSampleQuestion|showRecapDrawer|openAssistantPage|showAutoScroll|showScrollToOldest|openStorage|showFoldProbe|openInboxFirst|showInboxAction|openMachinePage|showAsUnread|showSampleScreen)'; then
  echo "DEBUG switches found in the Release binary" >&2; exit 1
fi

OPTS=$(mktemp -t export).plist
if [ -n "${NO_UPLOAD:-}" ]; then
  python3 scripts/jarvis_config.py export-options "$OPTS" export
  rm -rf "$OUT/export"
else
  python3 scripts/jarvis_config.py export-options "$OPTS" upload
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
  echo "Build $BUILD uploaded. It appears in App Store Connect → TestFlight in ~10–30 min (python3 scripts/asc_testflight.py wait 45 $BUILD)."
fi
