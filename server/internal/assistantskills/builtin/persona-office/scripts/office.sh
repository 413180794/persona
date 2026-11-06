#!/bin/sh
# Runs `personad office …` (docs/office.md) with this machine's personad: the one the node names (PERSONA_BIN), else
# where Persona installs it, else on PATH. Exit 3 when there is none.
for b in "$PERSONA_BIN" "$HOME/.persona/bin/personad" "$HOME/.persona/bin/personad" /opt/persona/bin/personad \
	"$(command -v personad 2>/dev/null)" "$(command -v personad 2>/dev/null)"; do
	if [ -n "$b" ] && [ -x "$b" ]; then
		exec "$b" office "$@"
	fi
done
echo "persona-office: no personad on this machine (looked in ~/.persona/bin, ~/.persona/bin and PATH)" >&2
exit 3
