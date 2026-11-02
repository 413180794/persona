#!/bin/sh
# Install jarvisd (docs/release.md「发布 jarvisd」). scripts/release/publish.sh serves this file as <site>/install.sh with
# JARVIS_DL filled in:
#
#   curl -fsSL <site>/install.sh | sh
#
# Puts the binary for this system at ~/.jarvis/bin/jarvisd (where the hub looks for it on every computer it reaches
# over ssh), after checking it against the published SHA256SUMS. JARVIS_DL=<url> installs from somewhere else.
# Then herdr (the default engine, docs/engines.md) if this computer has none: herdr's own installer puts it in
# ~/.local/bin, where the node looks. JARVIS_SKIP_HERDR=1 leaves it out (Orca / Herdr App machines); a failed herdr
# install only warns. scripts/deploy/ensure-herdr.sh does the same for deploy.sh: keep the two in step.
set -eu

DL="${JARVIS_DL:-@JARVIS_DL@}"
HERDR_INSTALL="${JARVIS_HERDR_INSTALL:-https://herdr.dev/install.sh}"
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

# get <name> <installed as>: downloads <name>.gz, checks it against SHA256SUMS and puts it in $DEST.
get() {
	archive="$1.gz"
	fetch "$DL/$archive" "$tmp/$archive" || fail "could not download $DL/$archive"
	want="$(awk -v n="$archive" '$2 == n || $2 == "*" n { print $1 }' "$tmp/SHA256SUMS")"
	[ -n "$want" ] || fail "$archive is not in SHA256SUMS"
	[ "$(sum "$tmp/$archive")" = "$want" ] || fail "checksum mismatch for $archive: not installed"
	gzip -dc "$tmp/$archive" > "$tmp/$1" || fail "could not unpack $archive: not installed"
	[ -s "$tmp/$1" ] || fail "$archive contains an empty binary: not installed"
	mkdir -p "$DEST"
	chmod 755 "$tmp/$1"
	mv "$tmp/$1" "$DEST/$2"
}

get "$name" jarvisd
version="$("$DEST/jarvisd" version 2>/dev/null || echo unknown)"
echo "jarvisd $version installed at $DEST/jarvisd"
case ":$PATH:" in
*":$DEST:"*) ;;
*) echo "(not on PATH: run it as $DEST/jarvisd, or add $DEST to PATH)" ;;
esac

# On the computer that runs the hub, JARVIS_WITH=telegram also installs jarvisd-telegram, the Telegram connector's
# program, next to jarvisd (docs/plan-telegram.md). Other computers do not need it; a hub that updates itself gets it
# with its next update anyway.
case ",${JARVIS_WITH:-}," in
*,telegram,*)
	get "jarvisd-telegram-$os-$arch" jarvisd-telegram
	echo "jarvisd-telegram installed at $DEST/jarvisd-telegram"
	;;
esac

# The places the node looks besides PATH (server/internal/node/node.go: ssh sessions get a minimal PATH).
has_herdr() {
	command -v herdr >/dev/null 2>&1 && return 0
	for d in "$HOME/.local/bin" "$HOME/bin" /opt/homebrew/bin /usr/local/bin; do
		[ -x "$d/herdr" ] && return 0
	done
	return 1
}
if [ "${JARVIS_SKIP_HERDR:-}" = 1 ]; then
	:
elif has_herdr; then
	echo "herdr: already installed"
else
	echo "herdr: not found, installing it (JARVIS_SKIP_HERDR=1 skips this)"
	if fetch "$HERDR_INSTALL" "$tmp/herdr-install.sh" && sh "$tmp/herdr-install.sh" && has_herdr; then
		echo "herdr: installed"
	else
		echo "jarvisd: warning: could not install herdr; install it yourself (https://herdr.dev) before using Agents on this computer" >&2
	fi
fi
echo "On the machine that runs the hub: jarvisd hub run (the first run writes ~/.jarvis/hub.json), then jarvisd hub pair."
echo "(With JARVIS_WITH=telegram there it also installs the Telegram connector.)"
echo "Other computers only need the binary: the hub reaches them over ssh."
