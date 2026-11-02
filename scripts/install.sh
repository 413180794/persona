#!/bin/sh
# Install jarvisd (docs/release.md「发布 jarvisd」). scripts/publish.sh serves this file as <site>/install.sh with
# JARVIS_DL filled in:
#
#   curl -fsSL <site>/install.sh | sh
#
# Puts the binary for this system at ~/.jarvis/bin/jarvisd (where the hub looks for it on every computer it reaches
# over ssh), after checking it against the published SHA256SUMS. JARVIS_DL=<url> installs from somewhere else.
set -eu

DL="${JARVIS_DL:-@JARVIS_DL@}"
DEST="$HOME/.jarvis/bin"

fail() {
	echo "jarvisd: $*" >&2
	exit 1
}

case "$(uname -s)" in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) fail "no build for $(uname -s) (Linux and macOS only)" ;;
esac
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) fail "no build for $(uname -m)" ;;
esac
name="jarvisd-$os-$arch"
archive="$name.gz"
command -v gzip >/dev/null 2>&1 || fail "needs gzip to unpack the download"

if command -v curl >/dev/null 2>&1; then
	fetch() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
	fetch() { wget -q "$1" -O "$2"; }
else
	fail "needs curl or wget"
fi
if command -v sha256sum >/dev/null 2>&1; then
	sum() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
	sum() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
	fail "needs sha256sum or shasum to check the download"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fetch "$DL/SHA256SUMS" "$tmp/SHA256SUMS" || fail "could not download $DL/SHA256SUMS"
fetch "$DL/$archive" "$tmp/$archive" || fail "could not download $DL/$archive"
want="$(awk -v n="$archive" '$2 == n || $2 == "*" n { print $1 }' "$tmp/SHA256SUMS")"
[ -n "$want" ] || fail "$archive is not in SHA256SUMS"
[ "$(sum "$tmp/$archive")" = "$want" ] || fail "checksum mismatch for $archive: not installed"
gzip -dc "$tmp/$archive" > "$tmp/$name" || fail "could not unpack $archive: not installed"
[ -s "$tmp/$name" ] || fail "$archive contains an empty binary: not installed"

mkdir -p "$DEST"
chmod 755 "$tmp/$name"
mv "$tmp/$name" "$DEST/jarvisd"
version="$("$DEST/jarvisd" version 2>/dev/null || echo unknown)"
echo "jarvisd $version installed at $DEST/jarvisd"
case ":$PATH:" in
*":$DEST:"*) ;;
*) echo "(not on PATH: run it as $DEST/jarvisd, or add $DEST to PATH)" ;;
esac
echo "On the machine that runs the hub: jarvisd hub run (the first run writes ~/.jarvis/hub.json), then jarvisd hub pair."
echo "Other computers only need the binary: the hub reaches them over ssh."
