#!/bin/sh
# personad in the hub image: as the persona user whoever calls it, so `docker compose exec hub personad hub pair` (root
# by default) reads the hub's files and anything it creates (the tailcat key) stays the hub's.
#
# The database's password (docs/operations/docker.md): .env's PERSONA_DB_PASSWORD when set (compose puts it into
# PERSONA_DATABASE_URL); otherwise the one the postgres service made up on its first start, in the data folder. Read
# here rather than in the entrypoint, so `docker compose exec hub personad …` gets it too.
if [ -z "${PERSONA_HOME+x}${JARVIS_HOME+x}" ] && grep -q ' /home/jarvis/.jarvis ' /proc/mounts 2>/dev/null; then
  PERSONA_HOME=/home/jarvis/.jarvis
fi
PERSONA_HOME="${PERSONA_HOME-${JARVIS_HOME:-$HOME/.persona}}"
export PERSONA_HOME JARVIS_HOME="$PERSONA_HOME"
PASSWORD_FILE="${PERSONA_DB_PASSWORD_FILE-${JARVIS_DB_PASSWORD_FILE:-$PERSONA_HOME/docker/db-password}}"
url="${PERSONA_DATABASE_URL-${JARVIS_DATABASE_URL:-}}"
export PERSONA_DATABASE_URL="$url" JARVIS_DATABASE_URL="$url"
rest="${url#*://}"
user="${rest%%@*}"
# A URL with a user and no password (postgres://persona:@…, or postgres://persona@…): .env did not set one.
if [ -n "$url" ] && [ "$rest" != "$url" ] && [ "$user" != "$rest" ]; then
  case "$user" in
  */*) ;;
  *:?*) ;;
  *)
    if [ -r "$PASSWORD_FILE" ]; then
      password="$(cat "$PASSWORD_FILE")"
      case "$password" in
      '' | *[!0-9A-Za-z]*) echo "warning: $PASSWORD_FILE is not a password of letters and digits: the hub cannot" \
        "reach the database. Set PERSONA_DB_PASSWORD in .env to the database's password (docs/operations/docker.md)" >&2 ;;
      *)
        PERSONA_DATABASE_URL="${url%%://*}://${user%%:*}:$password@${rest#*@}"
        export PERSONA_DATABASE_URL
        export JARVIS_DATABASE_URL="$PERSONA_DATABASE_URL" ;;
      esac
    elif [ -e "$PASSWORD_FILE" ]; then
      echo "warning: no database password: PERSONA_DB_PASSWORD is not set in .env and $PASSWORD_FILE cannot be" \
        "read by uid $(id -u) (it belongs to the postgres service's root, mode 0600). Run the hub as root (no compose" \
        "user:, PUID / PGID instead) or set PERSONA_DB_PASSWORD in .env (docs/operations/docker.md)" >&2
    else
      echo "warning: no database password: PERSONA_DB_PASSWORD is not set in .env and $PASSWORD_FILE (made by the" \
        "postgres service on its first start) is not there. Start with docker compose up -d, which starts postgres" \
        "first (docs/operations/docker.md)" >&2
    fi ;;
  esac
fi
unset url rest user password
if [ "$(id -u)" = 0 ]; then
  exec su-exec persona /opt/persona/personad "$@"
fi
exec /opt/persona/personad "$@"
