#!/bin/bash
# Jarvis Link: the menu bar app around jarvis-link on a Mac (mac/, docs/link.md).
#   ./scripts/app/mac-link.sh build            mac/build/Jarvis Link.app, jarvis-link and jarvisd inside, signed to run locally
#   ./scripts/app/mac-link.sh install          build, put it in /Applications and open it
#   ./scripts/app/mac-link.sh snapshot <dir>   draw the menu and the pairing window, light and dark, as PNGs
# The bundle id is mac.link_bundle_id in ~/.config/jarvis/config.json (or JARVIS_LINK_BUNDLE_ID), else the
# placeholder com.example.jarvis.link. UNIVERSAL=1 builds for Apple silicon and Intel together.
set -euo pipefail
cd "$(dirname "$0")/../.."
ROOT=$PWD
ID=${PERSONA_LINK_BUNDLE_ID-${JARVIS_LINK_BUNDLE_ID:-$(python3 scripts/jarvis_config.py get mac.link_bundle_id 2>/dev/null || true)}}
ID=${ID:-com.example.jarvis.link}
OUT="$ROOT/mac/build"
APP="$OUT/Jarvis Link.app"

build() {
  local version archs=() helpers=() nodes=()
  version="$(git describe --always --dirty)-$(date +%Y%m%d%H%M)"
  if [ "${UNIVERSAL:-}" = 1 ]; then archs=(arm64 x86_64); else archs=("$(uname -m)"); fi
  # xcodebuild builds the Swift package (swift build's default backend can't find the developer directory here).
  (cd mac && xcodebuild -quiet -scheme PersonaLink -configuration Release -derivedDataPath build/dd \
    -destination 'generic/platform=macOS' ARCHS="${archs[*]}" ONLY_ACTIVE_ARCH=NO build)
  local bin="$OUT/dd/Build/Products/Release/PersonaLink"
  mkdir -p "$OUT"
  for a in "${archs[@]}"; do
    local goarch=$a
    [ "$a" = x86_64 ] && goarch=amd64
    (cd server && GOOS=darwin GOARCH=$goarch CGO_ENABLED=0 go build -buildvcs=false -trimpath \
      -ldflags "-s -w -X main.version=$version" -o "$OUT/jarvis-link-$a" ./cmd/jarvis-link)
    helpers+=("$OUT/jarvis-link-$a")
    # The resident node the app sets up when this Mac has none (jarvis-link node install, docs/link.md).
    (cd server && GOOS=darwin GOARCH=$goarch CGO_ENABLED=0 go build -buildvcs=false -trimpath \
      -ldflags "-s -w -X main.version=$version" -o "$OUT/jarvisd-$a" ./cmd/personad)
    nodes+=("$OUT/jarvisd-$a")
  done
  rm -rf "$APP"
  mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Helpers"
  cp "$bin" "$APP/Contents/MacOS/JarvisLink"
  lipo -create "${helpers[@]}" -output "$APP/Contents/Helpers/jarvis-link"
  lipo -create "${nodes[@]}" -output "$APP/Contents/Helpers/jarvisd"
  sed -e "s/__BUNDLE_ID__/$ID/" -e "s/__BUILD__/$(date +%Y%m%d%H%M)/" mac/Info.plist > "$APP/Contents/Info.plist"
  # Sign to run locally (ad-hoc): the helpers first, then the bundle that seals them.
  codesign --force --sign - --identifier "$ID.helper" "$APP/Contents/Helpers/jarvis-link"
  codesign --force --sign - --identifier "$ID.jarvisd" "$APP/Contents/Helpers/jarvisd"
  codesign --force --sign - "$APP"
  codesign --verify --strict "$APP"
  echo "built $APP ($ID, jarvis-link $version)"
}

case "${1:-build}" in
  build) build ;;
  install)
    build
    pkill -x JarvisLink 2>/dev/null && sleep 1 || true
    rm -rf "/Applications/Jarvis Link.app"
    ditto "$APP" "/Applications/Jarvis Link.app"
    open "/Applications/Jarvis Link.app"
    echo "installed /Applications/Jarvis Link.app" ;;
  snapshot)
    [ -n "${2:-}" ] || { echo "usage: $0 snapshot <dir>" >&2; exit 2; }
    build
    "$APP/Contents/MacOS/JarvisLink" --snapshot "$2"
    ls "$2" ;;
  *) echo "usage: $0 [build|install|snapshot <dir>]" >&2; exit 2 ;;
esac
