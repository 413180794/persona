#!/bin/bash
# Publish jarvisd for anyone to install (docs/release.md「发布 jarvisd」): build the four binaries with the web client
# inside (no skills in them: those live in each machine's ~/.jarvis/skills), check them for private values, write the
# release manifest latest.json (every file's sha256, docs/hub-update.md「发布」), then deploy the site/ worker with dl/
# and install.sh added, at the config's publish.site_url. Every hub of the release channel
# updates itself to it. The worker also runs the push relay (/relay/*): it is deployed with it.
#
#   curl -fsSL <publish.site_url>/install.sh | sh
#
#   ./scripts/release/publish.sh            # build, check, deploy
#   NO_DEPLOY=1 ./scripts/release/publish.sh  # build and check only (build-publish/site)
#
# Only the newest version is served (dl/ is replaced on every deploy). release.sh runs this after uploading to TestFlight.
set -euo pipefail
cd "$(dirname "$0")/../.."
eval "$(python3 scripts/jarvis_config.py env)"
[ -n "$JARVIS_PUBLISH_URL" ] || { echo "publish.site_url is not set in the config (docs/config.md)" >&2; exit 2; }
# The build number orders releases (UTC YYYYMMDDHHMM): hubs install only a larger one.
BUILD="$(date -u +%Y%m%d%H%M)"
VERSION="${VERSION:-$(git describe --always --dirty 2>/dev/null || echo dev)-$BUILD}"
COMMIT="$(git rev-parse HEAD)"
# A release is the committed code: latest.json says which commit and lists every one it contains, and a hub
# installs a release by itself only when its own commit is among them (docs/hub-update.md「防退回」). Published with
# changes not committed, every hub running it would wait for a person from then on. The binaries say which commit
# they are (STAMP, "-dirty" only in a NO_DEPLOY build).
STAMP="$COMMIT"
if [ -n "$(git status --porcelain -- server web)" ]; then
  [ -n "${NO_DEPLOY:-}" ] || { echo "server/ or web/ has changes not committed: commit them first" >&2; exit 1; }
  STAMP="$COMMIT-dirty"
fi
OUT=build-publish
rm -rf "$OUT"
mkdir -p "$OUT/bin"

# The web client goes into the binary (server/internal/hub/webdist, embedded); the placeholder stays.
WEBDIST=server/internal/hub/webdist
cleanup() { find "$WEBDIST" -mindepth 1 ! -name README.md -exec rm -rf {} + 2>/dev/null || true; }
trap cleanup EXIT
(cd web && { [ -d node_modules ] || npm ci --no-audit --no-fund; } && npm run build) >/dev/null
cleanup
cp -R web/dist/. "$WEBDIST/"

# Public dependency constants are classified by build-info checksums; verify the actual module cache before building.
(cd server && go mod verify)
LDFLAGS="-s -w -X main.version=$VERSION -X main.channel=release -X main.build=$BUILD -X main.commit=$STAMP -X main.updateURL=$JARVIS_PUBLISH_URL/dl"
# The runtime packages jarvisd carries (the DSH plugin's code, docs/runtimes.md「自动更新」), from integrations/.
(cd server && go generate ./internal/runtimepkg)
# -tags public changes no code (no file builds differently with it, TestOneBuild): it marks the binary for
# check_bundle_privacy.py, which then knows the dependency constants it may skip.
for t in linux/amd64 linux/arm64 darwin/arm64 darwin/amd64; do
  (cd server && GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go build -tags public -trimpath -ldflags "$LDFLAGS" \
    -o "../$OUT/bin/jarvisd-${t%/*}-${t#*/}" ./cmd/jarvisd)
done
# The programs released with jarvisd (update.ProgramNames): built the same way, on the same platforms; a hub swaps
# them with jarvisd (docs/plan-telegram.md「架构」). jarvisd-telegram is Telegram's connector, too big to go into jarvisd.
for t in linux/amd64 linux/arm64 darwin/arm64 darwin/amd64; do
  (cd server && GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go build -tags public -trimpath -ldflags "$LDFLAGS" \
    -o "../$OUT/bin/jarvisd-telegram-${t%/*}-${t#*/}" ./cmd/jarvisd-telegram)
done
cleanup
sed "s|@JARVIS_DL@|$JARVIS_PUBLISH_URL/dl|" scripts/deploy/install.sh > "$OUT/install.sh"

# Anyone can download these: no private values in them (CLAUDE.md), and no copied skill (lark-connector is the user's, in
# ~/.jarvis/skills). install.sh is the repo's (scripts/privacy_check.py covers it) plus the site's address, which is
# public on purpose.
python3 scripts/release/check_bundle_privacy.py --whole-short "$OUT/bin"
if strings "$OUT/bin/jarvisd-linux-amd64" | grep -q "name: lark-connector"; then
  echo "the lark-connector skill is in the public build" >&2
  exit 1
fi
host_bin="$OUT/bin/jarvisd-$(uname -s | tr A-Z a-z)-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')"
[ "$("$host_bin" version)" = "$VERSION" ] || { echo "the built jarvisd does not report $VERSION" >&2; exit 1; }
"$host_bin" version -v | grep -qx "channel release, build $BUILD" || { echo "the built jarvisd is not release build $BUILD" >&2; exit 1; }
host_tg="$OUT/bin/jarvisd-telegram-${host_bin##*/jarvisd-}"
[ "$("$host_tg" version -v)" = "$("$host_bin" version -v)" ] || { echo "jarvisd-telegram is not the same build as jarvisd" >&2; exit 1; }

# Releases only move forward: a larger build number than the one served, and the served commit an ancestor of this one.
curl -fsSL "$JARVIS_PUBLISH_URL/dl/latest.json" -o "$OUT/served.json" 2>/dev/null || : > "$OUT/served.json"
"$host_bin" release check-next "$OUT/served.json" --build "$BUILD" --commit "$COMMIT" || exit 1

# 隐私扫描检查未压缩的原始二进制；只把压缩包发布到 Workers，避免单资源超过 25 MiB。
for bin in "$OUT/bin"/jarvisd-*; do
  gzip -n -9 -c "$bin" > "$bin.gz"
done
(cd "$OUT/bin" && shasum -a 256 jarvisd-*.gz > SHA256SUMS && printf '%s\n' "$VERSION" > VERSION)

# The manifest hubs update from: version, build number, commit and every download's sha256 (not signed, the user's
# call: it guards against a broken download over HTTPS, docs/hub-update.md「安全」).
"$host_bin" release manifest --dir "$OUT/bin" --version "$VERSION" --build "$BUILD" --commit "$COMMIT" --out "$OUT/bin/latest.json"

# The site with dl/ and install.sh added, deployed from a copy (site/ in the tree stays as committed).
rm -rf "$OUT/site"
cp -R site "$OUT/site"
mkdir -p "$OUT/site/public/dl"
cp "$OUT/bin/"*.gz "$OUT/bin/SHA256SUMS" "$OUT/bin/VERSION" "$OUT/bin/latest.json" "$OUT/site/public/dl/"
cp "$OUT/install.sh" "$OUT/site/public/install.sh"
python3 scripts/deploy/check_asset_sizes.py "$OUT/site/public"
if [ -n "${NO_DEPLOY:-}" ]; then
  echo "jarvisd $VERSION built and checked in $OUT (not deployed)."
  exit 0
fi
WRANGLER="${JARVIS_WRANGLER:-}"
# cf's OAuth token lasts about an hour and wrangler does not renew it; any cf call that finds it stale does (twice a
# release failed here with "Authentication error" until `cf auth whoami` ran).
command -v cf >/dev/null && cf auth whoami >/dev/null 2>&1 || true
(cd "$OUT/site" && if [ -n "$WRANGLER" ]; then "$WRANGLER" deploy; else npx wrangler deploy; fi) >"$OUT/deploy.log" 2>&1 \
  || { tail -20 "$OUT/deploy.log" >&2; echo "deploy failed; see $OUT/deploy.log (only run cf auth login for authentication errors)" >&2; exit 1; }

# What is served now is what was built.
served="$(curl -fsSL "$JARVIS_PUBLISH_URL/dl/VERSION" 2>/dev/null || true)"
if [ "$served" != "$VERSION" ]; then
  echo "deployed, but $JARVIS_PUBLISH_URL/dl/VERSION says '${served:-nothing}' (cache?)" >&2
  exit 1
fi
curl -fsSL "$JARVIS_PUBLISH_URL/dl/SHA256SUMS" | cmp -s - "$OUT/bin/SHA256SUMS" || { echo "served SHA256SUMS differs" >&2; exit 1; }
curl -fsSL "$JARVIS_PUBLISH_URL/dl/latest.json" | cmp -s - "$OUT/bin/latest.json" || { echo "served latest.json differs" >&2; exit 1; }
echo "jarvisd $VERSION published: curl -fsSL $JARVIS_PUBLISH_URL/install.sh | sh"
