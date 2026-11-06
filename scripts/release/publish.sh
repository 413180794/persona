#!/bin/bash
# Publish jarvisd for anyone to install (docs/release.md "Publishing jarvisd"): build the four binaries with the web client
# inside (no skills in them: those live in each machine's ~/.jarvis/skills), and the connector jarvis-link for the
# same four (the node service, docs/link.md), check them for private values, write the release manifest latest.json
# (every file's sha256, docs/hub-update.md "Publishing"), then deploy the site/ worker with dl/ and install.sh added, at the
# config's publish.site_url. Every hub of the release channel updates itself to it. The worker also runs the push
# relay (/relay/*): it is deployed with it.
#
#   curl -fsSL <publish.site_url>/install.sh | sh
#
#   ./scripts/release/publish.sh            # build, check, deploy
#   NO_DEPLOY=1 ./scripts/release/publish.sh  # build and check only (build-publish/site)
#   SITE_ONLY=1 ./scripts/release/publish.sh  # the pages only: site/ as committed with the release already served
#
# Never deploy site/ with a plain `wrangler deploy`: it serves without dl/ and install.sh, and every hub's update
# check and the install script 404 (docs/lessons/server.md).
#
# The downloads (dl/*.gz) live in the R2 bucket DL_BUCKET, not in the site: a Workers static file is at most 25 MiB
# (#2541). A release goes to one of two slots, <slot>/<name>, the one not served now; the deploy then switches dl/SLOT
# (a static file, deployed with latest.json) to it. The slot left keeps the release before until the next one, so a
# download that started before the deploy finishes. Only the newest version is served. release.sh runs this after
# uploading to TestFlight.
set -euo pipefail
cd "$(dirname "$0")/../.."
eval "$(python3 scripts/jarvis_config.py env)"
[ -n "$JARVIS_PUBLISH_URL" ] || { echo "publish.site_url is not set in the config (docs/config.md)" >&2; exit 2; }
WRANGLER="${JARVIS_WRANGLER:-}"
DL_BUCKET="${JARVIS_DL_BUCKET:-persona-dl}" # the r2_buckets binding DL in site/wrangler.jsonc
# wr <dir> <args…>: wrangler in <dir> (a copy of site/, for its account).
wr() { local dir="$1"; shift; (cd "$dir" && if [ -n "$WRANGLER" ]; then "$WRANGLER" "$@"; else npx wrangler "$@"; fi); }
# check_served <SHA256SUMS> <slot>: what the site serves now is that release: dl/SLOT, and every download at its size.
check_served() {
  local sums="$1" slot="$2" dir name size got
  dir="$(dirname "$sums")"
  [ "$(curl -fsSL "$JARVIS_PUBLISH_URL/dl/SLOT" 2>/dev/null)" = "$slot" ] || { echo "served dl/SLOT is not $slot" >&2; return 1; }
  while read -r _ name; do
    size=""
    [ -f "$dir/$name" ] && size="$(wc -c <"$dir/$name" | tr -d ' ')"
    got="$(curl -fsSI "$JARVIS_PUBLISH_URL/dl/$name" | tr -d '\r' | awk 'tolower($1)=="content-length:"{print $2}')"
    [ -n "$got" ] || { echo "served dl/$name is missing" >&2; return 1; }
    [ -z "$size" ] || [ "$got" = "$size" ] || { echo "served dl/$name is $got bytes, not $size" >&2; return 1; }
  done < "$sums"
}
# settled <what> <command…>: the command passes now, or within about two minutes. Right after a deploy the site can
# still serve the release before for a while (the entry and the edge cache; three releases on 2026-11-06, #5698).
settled() {
  local what="$1" try
  shift
  for try in $(seq 1 12); do
    "$@" 2>/dev/null && return 0
    [ "$try" -lt 12 ] && sleep 10
  done
  "$@" || { echo "$what still differs about two minutes after the deploy" >&2; return 1; }
}
served_is() { [ "$(curl -fsSL "$JARVIS_PUBLISH_URL/dl/$1" 2>/dev/null)" = "$(cat "$2")" ]; }
same_as_served() { curl -fsSL "$JARVIS_PUBLISH_URL/$1" | cmp -s - "$2"; }
# deploy_site <dir>: deploy that copy of site/ (with dl/ and install.sh in it), the log in <dir>/../deploy.log.
deploy_site() {
  local dir="$1" log="$1/../deploy.log"
  # cf's OAuth token lasts about an hour and wrangler does not renew it; any cf call that finds it stale does (twice a
  # release failed here with "Authentication error" until `cf auth whoami` ran).
  command -v cf >/dev/null && cf auth whoami >/dev/null 2>&1 || true
  wr "$dir" deploy >"$log" 2>&1 \
    || { tail -20 "$log" >&2; echo "deploy failed; see $log (only run cf auth login for authentication errors)" >&2; exit 1; }
}

# SITE_ONLY: no new build. The release served now (VERSION, SHA256SUMS, latest.json, SLOT and install.sh) is fetched
# back and deployed again next to site/ as committed; its downloads stay where they are, in the bucket's served slot.
if [ -n "${SITE_ONLY:-}" ]; then
  if [ -z "${NO_DEPLOY:-}" ] && [ -n "$(git status --porcelain -- site)" ]; then
    echo "site/ has changes not committed: commit them first" >&2; exit 1
  fi
  OUT=build-publish-site
  rm -rf "$OUT"
  mkdir -p "$OUT/dl"
  get() { curl -fsSL --retry 5 --retry-all-errors --retry-delay 3 --max-time 60 "$JARVIS_PUBLISH_URL/$1" -o "$2"; }
  for f in VERSION SHA256SUMS latest.json; do get "dl/$f" "$OUT/dl/$f"; done
  get install.sh "$OUT/install.sh"
  get dl/SLOT "$OUT/dl/SLOT" 2>/dev/null || { echo "the site serves no dl/SLOT (a release from before the bucket, #2541): run a full publish" >&2; exit 1; }
  SLOT="$(cat "$OUT/dl/SLOT")"
  case "$SLOT" in a|b) ;; *) echo "unexpected served dl/SLOT: $SLOT" >&2; exit 1 ;; esac
  while read -r _ name; do
    case "$name" in */*|""|.*) echo "unexpected name in the served SHA256SUMS: $name" >&2; exit 1 ;; esac
  done < "$OUT/dl/SHA256SUMS"
  VERSION="$(cat "$OUT/dl/VERSION")"
  cp -R site "$OUT/site"
  cp -R "$OUT/dl" "$OUT/site/public/dl"
  cp "$OUT/install.sh" "$OUT/site/public/install.sh"
  python3 scripts/deploy/check_asset_sizes.py "$OUT/site/public"
  if [ -n "${NO_DEPLOY:-}" ]; then echo "site with jarvisd $VERSION ready in $OUT/site (not deployed)."; exit 0; fi
  deploy_site "$OUT/site"
  for f in VERSION SHA256SUMS latest.json; do
    settled "served dl/$f" same_as_served "dl/$f" "$OUT/dl/$f" || exit 1
  done
  settled "served install.sh" same_as_served install.sh "$OUT/install.sh" || exit 1
  settled "the served downloads" check_served "$OUT/dl/SHA256SUMS" "$SLOT" || exit 1
  echo "site deployed with jarvisd $VERSION unchanged: $JARVIS_PUBLISH_URL"
  exit 0
fi

# The build number orders releases (UTC YYYYMMDDHHMM): hubs install only a larger one.
BUILD="$(date -u +%Y%m%d%H%M)"
VERSION="${VERSION:-$(git describe --always --dirty 2>/dev/null || echo dev)-$BUILD}"
COMMIT="$(git rev-parse HEAD)"
# A release is the committed code: latest.json says which commit and lists every one it contains, and a hub
# installs a release by itself only when its own commit is among them (docs/hub-update.md "No rollback"). Published with
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
# The public registry, whatever this machine's npm is set to: package-lock.json resolves there, and a mirror can miss
# a package (2026-11-06 one lacked elkjs and the release stopped here).
NPM_REGISTRY="${JARVIS_NPM_REGISTRY:-https://registry.npmjs.org/}"
(cd web && { [ -d node_modules ] || npm ci --no-audit --no-fund --registry="$NPM_REGISTRY"; } && npm run build) >/dev/null
cleanup
cp -R web/dist/. "$WEBDIST/"

# Public dependency constants are classified by build-info checksums; verify the actual module cache before building.
(cd server && go mod verify)
LDFLAGS="-s -w -X main.version=$VERSION -X main.channel=release -X main.build=$BUILD -X main.commit=$STAMP -X main.updateURL=$JARVIS_PUBLISH_URL/dl"
# The runtime packages jarvisd carries (the DSH plugin's code, docs/runtimes.md "Auto-update"), from integrations/.
(cd server && go generate ./internal/runtimepkg)
# -tags public changes no code (no file builds differently with it, TestOneBuild): it marks the binary for
# check_bundle_privacy.py, which then knows the dependency constants it may skip.
for t in linux/amd64 linux/arm64 darwin/arm64 darwin/amd64; do
  (cd server && GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go build -tags public -trimpath -ldflags "$LDFLAGS" \
    -o "../$OUT/bin/jarvisd-${t%/*}-${t#*/}" ./cmd/personad)
  (cd server && GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go build -tags public -trimpath -ldflags "$LDFLAGS" \
    -o "../$OUT/bin/jarvis-link-${t%/*}-${t#*/}" ./cmd/jarvis-link)
done
# The programs released with jarvisd (update.ProgramNames): built the same way, on the same platforms; a hub swaps
# them with jarvisd. jarvisd-wechat is the WeChat bot (docs/wechat.md), a plugin of its own. (Telegram's connector is
# an extension now, released from the extension market: #1704.)
for t in linux/amd64 linux/arm64 darwin/arm64 darwin/amd64; do
  for p in wechat; do
    (cd server && GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go build -tags public -trimpath -ldflags "$LDFLAGS" \
      -o "../$OUT/bin/jarvisd-$p-${t%/*}-${t#*/}" "./cmd/personad-$p")
  done
done
cleanup
sed "s|@JARVIS_DL@|$JARVIS_PUBLISH_URL/dl|" scripts/deploy/install.sh > "$OUT/install.sh"

# Anyone can download these: no private values in them (CLAUDE.md), and no copied skill (only the built-in ones
# assistantskills.Redistributable lists; a copied skill lives in the user's ~/.jarvis/skills or comes with its
# extension). install.sh is the repo's (scripts/privacy_check.py covers it) plus the site's address, which is public
# on purpose.
python3 scripts/release/check_bundle_privacy.py --whole-short "$OUT/bin"
python3 scripts/release/check_bundle_skills.py "$OUT/bin/jarvisd-linux-amd64"
host_bin="$OUT/bin/jarvisd-$(uname -s | tr A-Z a-z)-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')"
[ "$("$host_bin" version)" = "$VERSION" ] || { echo "the built jarvisd does not report $VERSION" >&2; exit 1; }
host_link="$OUT/bin/jarvis-link-${host_bin##*/jarvisd-}"
[ "$("$host_link" version)" = "$VERSION" ] || { echo "the built jarvis-link does not report $VERSION" >&2; exit 1; }
"$host_bin" version -v | grep -qx "channel release, build $BUILD" || { echo "the built jarvisd is not release build $BUILD" >&2; exit 1; }
for p in wechat; do
  host_p="$OUT/bin/jarvisd-$p-${host_bin##*/jarvisd-}"
  [ "$("$host_p" version -v)" = "$("$host_bin" version -v)" ] || { echo "jarvisd-$p is not the same build as jarvisd" >&2; exit 1; }
done

# Releases only move forward: a larger build number than the one served, and the served commit an ancestor of this one.
curl -fsSL "$JARVIS_PUBLISH_URL/dl/latest.json" -o "$OUT/served.json" 2>/dev/null || : > "$OUT/served.json"
"$host_bin" release check-next "$OUT/served.json" --build "$BUILD" --commit "$COMMIT" || exit 1

# The privacy scan checked the raw binaries; only the gzipped ones are published (to the bucket, see above).
for bin in "$OUT/bin"/jarvisd-* "$OUT/bin"/jarvis-link-*; do
  gzip -n -9 -c "$bin" > "$bin.gz"
done
(cd "$OUT/bin" && shasum -a 256 jarvisd-*.gz jarvis-link-*.gz > SHA256SUMS && printf '%s\n' "$VERSION" > VERSION)

# The manifest hubs update from: version, build number, commit and every download's sha256 (not signed, the user's
# call: it guards against a broken download over HTTPS, docs/hub-update.md "Security").
"$host_bin" release manifest --dir "$OUT/bin" --version "$VERSION" --build "$BUILD" --commit "$COMMIT" --out "$OUT/bin/latest.json"

# The site with dl/ and install.sh added, deployed from a copy (site/ in the tree stays as committed).
rm -rf "$OUT/site"
cp -R site "$OUT/site"
mkdir -p "$OUT/site/public/dl"
# The slot not served now (a when nothing is): the release before stays in the other one until the next release.
served_slot="$(curl -fsSL "$JARVIS_PUBLISH_URL/dl/SLOT" 2>/dev/null || true)"
SLOT=a
[ "$served_slot" = a ] && SLOT=b
printf '%s\n' "$SLOT" > "$OUT/bin/SLOT"
cp "$OUT/bin/SHA256SUMS" "$OUT/bin/VERSION" "$OUT/bin/latest.json" "$OUT/bin/SLOT" "$OUT/site/public/dl/"
cp "$OUT/install.sh" "$OUT/site/public/install.sh"
python3 scripts/deploy/check_asset_sizes.py "$OUT/site/public"
if [ -n "${NO_DEPLOY:-}" ]; then
  echo "jarvisd $VERSION built and checked in $OUT (not deployed)."
  exit 0
fi
# The downloads first, into the slot no one is served from; then the deploy that points dl/SLOT at it.
# cf's OAuth token lasts about an hour and runs out between uploads too (twice on 2026-11-06, #4202): refresh it before
# each file, and once more before the one retry.
refresh_cf() { command -v cf >/dev/null && cf auth whoami >/dev/null 2>&1 || true; }
# put <file>: upload it into the slot. Absolute paths: wr runs wrangler inside the site copy.
put() { wr "$OUT/site" r2 object put "$DL_BUCKET/$SLOT/${1##*/}" --file "$1" --content-type application/gzip --remote >>"$OUT/upload.log" 2>&1; }
for gz in "$PWD/$OUT/bin/"*.gz; do
  refresh_cf
  put "$gz" || { sleep 5; refresh_cf; put "$gz"; } \
    || { tail -20 "$OUT/upload.log" >&2; echo "upload of ${gz##*/} to R2 failed; see $OUT/upload.log" >&2; exit 1; }
done
deploy_site "$OUT/site"

# What is served now is what was built (allowing the site a moment to switch over).
settled "served dl/VERSION" served_is VERSION "$OUT/bin/VERSION" \
  || { echo "deployed, but $JARVIS_PUBLISH_URL/dl/VERSION says '$(curl -fsSL "$JARVIS_PUBLISH_URL/dl/VERSION" 2>/dev/null)'" >&2; exit 1; }
settled "served dl/SHA256SUMS" same_as_served dl/SHA256SUMS "$OUT/bin/SHA256SUMS" || exit 1
settled "served dl/latest.json" same_as_served dl/latest.json "$OUT/bin/latest.json" || exit 1
settled "the served downloads" check_served "$OUT/bin/SHA256SUMS" "$SLOT" || exit 1
echo "jarvisd $VERSION published: curl -fsSL $JARVIS_PUBLISH_URL/install.sh | sh"
