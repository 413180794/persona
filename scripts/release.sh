#!/bin/bash
# Archive a Release build (iPhone app with the embedded watch app) and upload it to App Store Connect.
#
#   ./scripts/release.sh [build-number]            # archive + check + upload
#   NO_UPLOAD=1 ./scripts/release.sh               # archive + check + export only (build-release/export)
#   NO_PUBLISH=1 ./scripts/release.sh              # TestFlight only, no jarvisd at publish.site_url (publish.sh)
#
# The App Store Connect API key, bundle id and team come from the config (docs/config.md). Builds into build-release/
# so it never touches the simulator build. The export re-signs manually with the "Jarvis App Store" / "Jarvis Watch
# App Store" profiles (create them first with scripts/asc_signing.py): the App Manager API key can't cloud-sign.
set -euo pipefail
cd "$(dirname "$0")/.."
eval "$(python3 scripts/jarvis_config.py env)"
python3 scripts/jarvis_config.py xcconfig
echo "Building with $(xcodebuild -version | head -1) (config \"xcode\" picks another, docs/config.md)"
# No private values in what is about to ship from this tree (CLAUDE.md).
python3 scripts/privacy_check.py
OUT=build-release
BUILD="${1:-$(date +%Y%m%d%H%M)}"
# Keep every archive where Xcode Organizer looks, so each version's dSYMs stay around for symbolicating crashes.
ARCHIVE="$HOME/Library/Developer/Xcode/Archives/$(date +%Y-%m-%d)/Jarvis $BUILD.xcarchive"
AUTH=(-allowProvisioningUpdates -authenticationKeyPath "$ASC_KEY_PATH" -authenticationKeyID "$ASC_KEY_ID" -authenticationKeyIssuerID "$ASC_ISSUER_ID")

mkdir -p "$OUT"
cd apple
xcodegen generate -q
cd ..

xcodebuild -project apple/Jarvis.xcodeproj -scheme Jarvis -configuration Release \
  -destination 'generic/platform=iOS' -archivePath "$ARCHIVE" -derivedDataPath "$OUT/DerivedData" \
  CURRENT_PROJECT_VERSION="$BUILD" "${AUTH[@]}" clean archive | grep -E "error:|ARCHIVE (SUCCEEDED|FAILED)"

APP="$ARCHIVE/Products/Applications/Jarvis.app"
WATCH="$APP/Watch/Jarvis.app"
# Placeholder purpose strings get the build rejected automatically: stop before uploading.
python3 scripts/check_bundle_strings.py "$APP"
if [ -d "$WATCH" ]; then python3 scripts/check_bundle_strings.py "$WATCH"; fi
# Anyone with the TestFlight public link can install the build: no hub address, host, email or path of this person in it.
python3 scripts/check_bundle_privacy.py "$APP"
# Siri / Shortcuts only see the intents if their metadata was extracted at build time.
[ -f "$APP/Metadata.appintents/extract.actionsdata" ] || { echo "missing AppIntents metadata in $APP"; exit 1; }
# Release must not contain the DEBUG-only simulator switches (apple/Jarvis/App/JarvisApp.swift).
if strings "$APP/Jarvis" | grep -qxE '(pairURL|openSession|showNewChat|openAssistant|openAssistantSidebar|newAssistant|openFirstImage|openTerminal|showPad|showKeyMods|focusTerminalInput|openShortcutsPanel|openTerminalSettings|openTerminalTips|showInfo|loadAll|fromSeq|lark-connector|lark-connectorName|larkTop|lark-connectorTopic|larkFocus|inboxDrawer|larkProfile|larkPicker|lark-connectorAt|larkMoments|openWorkbench|addMail|dropUsers|openPane|simulateTap|showMenu|openFile|jumpEnd|showOldest|openWork|openAgent|focusComposer|terminalPages|openEditor|openLinkURL|openChatInfo|openChatInfoPage|openChatInfoFrom|openThreadAt|openPeople|openMomentsTab|openMailLabel|openPeopleQuery|openFeishuDoc|openFeishuDocHeading|showFeishuDocSheet|openFeishuWikiSpace|openFeishuWikiNode|showFeishuWikiUpdates|openSearch|openSearchTab|openSearchFeed|openSearchFilter|openSearchFirst|openCalendarDate|openCalendarEvent|showCalendarNow|showDetailBottom|openMeeting|openMinutes|showMinutesTab|showMinutesPlaying|showMinutesSearch|openTask|openTaskScope|openOKR|showWorkBottom|showFeishuDocTableEnd|openMailThread|showMailAttachment|showMailBottom|showCloseAgent|openSpaceFromChat|showShareCard|showCompactChrome|showWakeDrawer|showSampleQuestion|showRecapDrawer|openAssistantPage|showAutoScroll|showScrollToOldest|openStorage|showFoldProbe|openInboxFirst|showInboxAction|openMachinePage|showAsUnread|showSampleScreen|showTestNotification|openNotificationSettings|openSendMessage|openUploadDemo|showFleetSample|fleetDetails|fleetIsland|showFleetCount|openIslandSettings|openIslandDiagnostics|showConnection|simulateOffline|openConnectionDetails|openSendDelay|openMonitor|openAgentsFresh|openChatsFresh|perfCounts|openWelcome|openMonitorRange|openMonitorScrub|openMachines|openMachineDetail|openDevices|openConnectorMail|openConnectorFeishu|openConnectorGitHub|openConnectorDevice|showFullOutput|showFreezeProbe|showScrollState|showChatDump|showClearConfirm|showPromptMenu|openModelLaunchers|openModelConfiguration|openModelPurpose|showModelSearch|showDeleteConfirm|showLauncherCheck|openModelLauncher|showKnowledgeEntry|showKnowledgeSearch|showKnowledgeLower|showKnowledgeKind|openFromDoneNotice|holdChromeOpen|showErrorDetails|openPersona|showPersonaState|openTranslationSettings|openLauncherPage|openAppearance|showFilePerf|showFileAutoScroll|showFileAutoScrollSpeed|showSpaceTabs|showDrawerProbe|showDrawerAutoDrag|openInboxItem|openShellPage|showShellPagerAt|showDesktopSample|openDesktopSheet|showSamplePermission|showSampleCodexApproval|showSampleCodexQuestion|showKnowledgeDay|showToolDetails|showMailAskClaude|openPairSheet|openEngineStep|showPushKey|showRunsOpen|openQuietSpaces|showHomeState|openCodexSettings|showPagePerf|showPopFrames|openRecording|showRecordingSample|openHubUpdate|openSkills)'; then
  echo "DEBUG switches found in the Release binary" >&2; exit 1
fi
# Nor the runtime CLI (apple/Jarvis/Debug/, docs/runtime-cli.md): its switches, its type, the accessibility hook it turns on.
if strings "$APP/Jarvis" | grep -qE 'runtimeCLI|RuntimeCLI|_AXSSet(Automation|ReduceMotion)Enabled'; then
  echo "the runtime CLI found in the Release binary" >&2; exit 1
fi

# Every bundle signed with its target's keychain groups (the app shares the Live Activity's persona with JarvisWidgets,
# the push key with JarvisNotify): build 92 shipped without them (docs/release.md「entitlements」).
python3 scripts/entitlements.py check "$ARCHIVE"

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
  python3 scripts/entitlements.py check "$OUT/export/Jarvis.ipa"
fi
git tag -f "testflight/$BUILD" >/dev/null 2>&1 || true
if [ -n "${NO_UPLOAD:-}" ]; then
  echo "Build $BUILD archived and exported to $OUT/export (not uploaded)."
else
  echo "Build $BUILD uploaded. It appears in App Store Connect → TestFlight in ~10–30 min (python3 scripts/asc_testflight.py wait 45 $BUILD)."
fi

# The same release of jarvisd for anyone to install (publish.sh, docs/release.md「发布 jarvisd」), versioned with this
# build's number. Not with NO_UPLOAD or NO_PUBLISH, nor when the config has no publish.site_url.
if [ -z "${NO_UPLOAD:-}" ] && [ -z "${NO_PUBLISH:-}" ] && [ -n "${JARVIS_PUBLISH_URL:-}" ]; then
  VERSION="$(git describe --always --dirty 2>/dev/null || echo dev)-$BUILD" ./scripts/publish.sh
fi
