#!/bin/sh
# The hub image's entrypoint (docs/docker.md): run jarvisd as the jarvis user with the uid / gid the data volume's
# files should have (PUID / PGID, as NAS images do: the files stay yours in the NAS's file manager).
set -eu
PUID="${PUID:-1000}"
PGID="${PGID:-1000}"
DATA="$HOME/.jarvis"
if [ "$(id -u)" = 0 ]; then
  case "$PUID$PGID" in '' | *[!0-9]*) echo "PUID / PGID must be numbers" >&2; exit 2 ;; esac
  if [ "$PUID" -eq 0 ]; then
    echo "PUID=0 would run the hub as root: pick the uid that owns the data folder" >&2
    exit 2
  fi
  # The passwd entry, not just the number: ssh looks the uid up for its home (~/.ssh) and refuses a uid it cannot
  # find. Another entry with the same uid (alpine's guest is 405, nobody 65534) would win that lookup: drop it.
  awk -F: -v u="$PUID" '$3 == u && $1 != "jarvis" { next } { print }' /etc/passwd > /etc/passwd.new
  cat /etc/passwd.new > /etc/passwd && rm -f /etc/passwd.new
  sed -i "s/^jarvis:x:[0-9]*:[0-9]*:/jarvis:x:$PUID:$PGID:/" /etc/passwd
  sed -i "s/^jarvis:x:[0-9]*:/jarvis:x:$PGID:/" /etc/group
  mkdir -p "$DATA"
  # Anything not the hub's (PUID changed, files copied in with sudo) is handed over: a root-owned 0600 hub.json
  # would keep the hub from starting.
  find "$DATA" \( ! -user "$PUID" -o ! -group "$PGID" \) -exec chown -h "$PUID:$PGID" {} +
  chown "$PUID:$PGID" "$HOME"
  chmod 700 "$DATA"
elif ! id -un >/dev/null 2>&1; then
  # Started as another user (compose `user:`): PUID / PGID do not apply, and ssh cannot run without a passwd entry.
  echo "warning: running as uid $(id -u), which has no user in the image: PUID / PGID are ignored and ssh will" \
    "not work. Start the container as root (no user:) and set PUID / PGID instead." >&2
fi
# Who may open the admin UI (docs/admin.md): the deployment's choice, or a reminder that nobody made one.
case "${JARVIS_ADMIN_ACCESS:-}" in '' | lan)
  # Docker Desktop and OrbStack relay every port-mapped connection from the bridge's gateway, which is never LAN.
  if getent hosts host.docker.internal >/dev/null 2>&1; then
    echo "note: this looks like Docker Desktop / OrbStack: every browser reaches the hub from the bridge's gateway," \
      "which the admin UI never counts as LAN. Publish the port on 127.0.0.1 only and list the gateway in" \
      "JARVIS_ADMIN_CIDRS (docs/admin.md), or manage the hub with docker compose exec hub jarvisd hub admin." >&2
  fi ;;
esac
case "${JARVIS_ADMIN_ACCESS:-}" in
'') echo "reminder: JARVIS_ADMIN_ACCESS is not set in .env: the admin UI (/admin) follows hub.json's admin.access," \
  "lan (private addresses) unless set there. Choose loopback, lan, cidrs or any (docs/admin.md)." >&2 ;;
loopback | lan | any) ;;
cidrs) [ -n "${JARVIS_ADMIN_CIDRS:-}" ] || echo "warning: JARVIS_ADMIN_ACCESS=cidrs without JARVIS_ADMIN_CIDRS: the" \
  "admin UI is reachable from this container only" >&2 ;;
*) echo "warning: JARVIS_ADMIN_ACCESS=$JARVIS_ADMIN_ACCESS is not loopback, lan, cidrs or any: the admin UI is limited" \
  "to this container until it is fixed" >&2 ;;
esac
exec jarvisd "$@"
