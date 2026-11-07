#!/bin/sh
# Install herdr (the default engine, docs/integrations/engines.md) on this computer if it has none. deploy.sh pipes it to each
# machine it installs personad on (`ssh <host> sh -s < scripts/deploy/ensure-herdr.sh`); install.sh has the same steps inline
# (it is served alone): keep the two in step. herdr's own installer puts it in ~/.local/bin, where the node looks.
# PERSONA_SKIP_HERDR=1 skips it. Prints one line; a failed install only warns (exit 0) so the deploy goes on.
HERDR_INSTALL="${PERSONA_HERDR_INSTALL:-https://herdr.dev/install.sh}"

# The places the node looks besides PATH (server/internal/node/node.go: ssh sessions get a minimal PATH).
has_herdr() {
	command -v herdr >/dev/null 2>&1 && return 0
	for d in "$HOME/.local/bin" "$HOME/bin" /opt/homebrew/bin /usr/local/bin; do
		[ -x "$d/herdr" ] && return 0
	done
	return 1
}

[ "${PERSONA_SKIP_HERDR:-}" = 1 ] && exit 0
has_herdr && exit 0
if ! command -v curl >/dev/null 2>&1; then
	echo "herdr: not installed and no curl to install it; install it yourself (https://herdr.dev)" >&2
	exit 0
fi
log="$(mktemp)"
if curl -fsSL --connect-timeout 10 --max-time 60 "$HERDR_INSTALL" | sh >"$log" 2>&1 && has_herdr; then
	echo "herdr: installed"
else
	echo "herdr: could not install it; install it yourself (https://herdr.dev):" >&2
	tail -n 3 "$log" >&2
fi
rm -f "$log"
exit 0
