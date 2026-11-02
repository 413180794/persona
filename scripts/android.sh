#!/bin/bash
# Build the Android app and install it on a connected phone or emulator (docs/android.md).
#   ./scripts/android.sh            # debug build, install, launch
#   ./scripts/android.sh test       # unit tests only
#   ./scripts/android.sh apk        # debug APK only (android/app/build/outputs/apk/debug/app-debug.apk)
#   ./scripts/android.sh shots [类名] # screenshots of the pages with mock_hub.py's data, light and dark → android/app/build/shots/
# The application id comes from ~/.config/jarvis/config.json: android.application_id, else app.bundle_id.
set -euo pipefail
cd "$(dirname "$0")/.."
APP_ID=$(python3 scripts/jarvis_config.py get android.application_id 2>/dev/null || true)
[ -n "$APP_ID" ] || APP_ID=$(python3 scripts/jarvis_config.py get app.bundle_id 2>/dev/null || true)
APP_ID=${APP_ID:-com.example.jarvis}
SDK=${ANDROID_HOME:-$HOME/Library/Android/sdk}
ADB="$SDK/platform-tools/adb"
# Android Studio's bundled JDK when no JDK 17 is on the path.
if [ -z "${JAVA_HOME:-}" ] && [ -d "/Applications/Android Studio.app/Contents/jbr/Contents/Home" ]; then
  export JAVA_HOME="/Applications/Android Studio.app/Contents/jbr/Contents/Home"
fi
cd android
[ -f local.properties ] || echo "sdk.dir=$SDK" > local.properties
case "${1:-install}" in
  test) ./gradlew -q -PjarvisApplicationId="$APP_ID" :app:testDebugUnitTest ;;
  shots)
    rm -rf app/build/reports/paparazzi
    ./gradlew -q -PjarvisApplicationId="$APP_ID" -Pshots :app:testDebugUnitTest --tests "jarvis.android.shots.${2:-*}"
    python3 ../scripts/android_shots.py app/build/reports/paparazzi/debug app/build/shots ;;
  apk) ./gradlew -q -PjarvisApplicationId="$APP_ID" :app:assembleDebug && ls -la app/build/outputs/apk/debug/app-debug.apk ;;
  install)
    ./gradlew -q -PjarvisApplicationId="$APP_ID" :app:assembleDebug
    "$ADB" install -r app/build/outputs/apk/debug/app-debug.apk
    "$ADB" shell am start -n "$APP_ID/jarvis.android.MainActivity" >/dev/null ;;
  *) echo "usage: $0 [install|apk|test|shots [class]]" >&2; exit 2 ;;
esac
