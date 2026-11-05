#!/usr/bin/env bash
# Regression for a registered overlay whose first traversal races with hiding it.
# Run on a dedicated emulator with the debug app paired, Always selected, and overlay permission granted.
set -euo pipefail
ADB=${ADB:-"${ANDROID_HOME:-$HOME/Library/Android/sdk}/platform-tools/adb"}
SERIAL=${1:?Usage: check-cold-start.sh SERIAL APPLICATION_ID [ROUNDS] [OUTPUT_DIRECTORY]}
APP_ID=${2:?Provide the installed debug application id}
ROUNDS=${3:-10}
OUTPUT=${4:-/tmp/persona-capsule-cold-start}
case "$APP_ID" in *[!a-zA-Z0-9._]*|'') echo "Invalid application id" >&2; exit 2 ;; esac
[[ "$ROUNDS" =~ ^[1-9][0-9]*$ ]] || { echo "ROUNDS must be positive" >&2; exit 2; }
[ "$ROUNDS" -le 100 ] || { echo "ROUNDS must be at most 100" >&2; exit 2; }
mkdir -p "$OUTPUT"
adb_cmd() { "$ADB" -s "$SERIAL" "$@"; }
[ "$(adb_cmd shell getprop ro.kernel.qemu | tr -d '\r')" = 1 ] || { echo "Use a dedicated emulator" >&2; exit 2; }
adb_cmd shell run-as "$APP_ID" cat shared_prefs/persona-capsule.xml > "$OUTPUT/preferences.xml"
grep -Fq '>always<' "$OUTPUT/preferences.xml" || { echo "Precondition: select Always in capsule settings" >&2; exit 2; }
adb_cmd shell appops get "$APP_ID" SYSTEM_ALERT_WINDOW > "$OUTPUT/permission.txt"
grep -q 'allow' "$OUTPUT/permission.txt" || { echo "Precondition: grant overlay permission" >&2; exit 2; }
SINCE=$(adb_cmd shell "date '+%m-%d %H:%M:%S.000'" | tr -d '\r')
for ((round=1; round<=ROUNDS; round++)); do
    adb_cmd shell am force-stop "$APP_ID"
    adb_cmd shell am start -W -n "$APP_ID/persona.android.MainActivity" > "$OUTPUT/launch-$round.txt"
    adb_cmd shell input keyevent KEYCODE_HOME
    sleep 0.8
    # Re-enter and hide again while the overlay may still have a pending first traversal.
    adb_cmd shell am start -n "$APP_ID/persona.android.MainActivity" > /dev/null
    adb_cmd shell input keyevent KEYCODE_HOME
    sleep 0.8
    adb_cmd shell pidof "$APP_ID" > "$OUTPUT/pid-$round.txt" || {
        adb_cmd shell "logcat -b crash -d -T '$SINCE'" > "$OUTPUT/crash.log"
        echo "FAIL: process died in round $round; $OUTPUT/crash.log" >&2
        exit 1
    }
done
adb_cmd shell "logcat -b crash -d -T '$SINCE'" > "$OUTPUT/crash.log"
if grep -Fq "Process: $APP_ID," "$OUTPUT/crash.log"; then
    echo "FAIL: app crash recorded; $OUTPUT/crash.log" >&2
    exit 1
fi
echo "PASS: $ROUNDS cold start/Home cycles; evidence: $OUTPUT"
