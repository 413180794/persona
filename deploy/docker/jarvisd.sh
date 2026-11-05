#!/bin/sh
# jarvisd in the hub image: as the jarvis user whoever calls it, so `docker compose exec hub jarvisd hub pair` (root
# by default) reads the hub's files and anything it creates (the tailcat key) stays the hub's.
#
# The database's password (docs/docker.md): .env's JARVIS_DB_PASSWORD when set (compose puts it into
# JARVIS_DATABASE_URL); otherwise the one the postgres service made up on its first start, in the data folder. Read
# here rather than in the entrypoint, so `docker compose exec hub jarvisd …` gets it too.
PASSWORD_FILE="${JARVIS_DB_PASSWORD_FILE:-$HOME/.jarvis/docker/db-password}"
url="${JARVIS_DATABASE_URL:-}"
rest="${url#*://}"
user="${rest%%@*}"
# A URL with a user and no password (postgres://jarvis:@…, or postgres://jarvis@…): .env did not set one.
if [ -n "$url" ] && [ "$rest" != "$url" ] && [ "$user" != "$rest" ]; then
  case "$user" in
  */*) ;;
  *:?*) ;;
  *)
    if [ -r "$PASSWORD_FILE" ]; then
      password="$(cat "$PASSWORD_FILE")"
      case "$password" in
      '' | *[!0-9A-Za-z]*) echo "warning: $PASSWORD_FILE is not a password of letters and digits: the hub cannot" \
        "reach the database. Set JARVIS_DB_PASSWORD in .env to the database's password (docs/docker.md)" >&2 ;;
      *)
        JARVIS_DATABASE_URL="${url%%://*}://${user%%:*}:$password@${rest#*@}"
        export JARVIS_DATABASE_URL ;;
      esac
    elif [ -e "$PASSWORD_FILE" ]; then
      echo "warning: no database password: JARVIS_DB_PASSWORD is not set in .env and $PASSWORD_FILE cannot be" \
        "read by uid $(id -u) (it belongs to the postgres service's root, mode 0600). Run the hub as root (no compose" \
        "user:, PUID / PGID instead) or set JARVIS_DB_PASSWORD in .env (docs/docker.md)" >&2
    else
      echo "warning: no database password: JARVIS_DB_PASSWORD is not set in .env and $PASSWORD_FILE (made by the" \
        "postgres service on its first start) is not there. Start with docker compose up -d, which starts postgres" \
        "first (docs/docker.md)" >&2
    fi ;;
  esac
fi
unset url rest user password
if [ "$(id -u)" = 0 ]; then
  exec su-exec jarvis /opt/jarvis/jarvisd "$@"
fi
exec /opt/jarvis/jarvisd "$@"
