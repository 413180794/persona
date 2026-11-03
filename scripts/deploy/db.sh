#!/bin/bash
# Runs on the hub machine (deploy.sh pipes it over ssh): make sure the Jarvis Postgres container is up and that
# ~/.jarvis/hub.json has database_url. Idempotent; the password is generated once and lives only in hub.json (0600).
#   container jarvis-postgres · image postgres:18-alpine · 127.0.0.1:18661 · volume jarvis-pgdata
set -euo pipefail
NAME=jarvis-postgres
PORT=18661
ROOT="${_PERSONA_STATE_DIR:-$(python3 "$(dirname "${BASH_SOURCE[0]}")/../persona_identity.py" --service hub --prepare)}"
CFG="$ROOT/hub.json"
mkdir -p "$ROOT" && chmod 700 "$ROOT"

url() { python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("database_url",""))' "$CFG" 2>/dev/null || true; }

# No docker on the hub machine: the database is one set up by hand (the distribution's own Postgres, docs/server.md
# 「数据库」) and hub.json's database_url points at it. Nothing to create here; only check that it answers.
if ! command -v docker >/dev/null 2>&1; then
  [ -n "$(url)" ] || { echo "no docker and no database_url in hub.json: install docker, or set up Postgres yourself (docs/server.md)" >&2; exit 1; }
  if command -v pg_isready >/dev/null 2>&1; then
    pg_isready -q -d "$(url)" || { echo "the Postgres at hub.json's database_url does not answer" >&2; exit 1; }
  fi
  echo "postgres from hub.json's database_url (no docker)"
  exit 0
fi

if ! docker ps -a --format '{{.Names}}' | grep -qx "$NAME"; then
  PW=$(python3 -c 'import secrets;print(secrets.token_hex(24))')
  docker volume create jarvis-pgdata >/dev/null
  # postgres 18 keeps PGDATA under /var/lib/postgresql/<major>/docker, so mount the parent.
  docker run -d --name "$NAME" --restart unless-stopped -p 127.0.0.1:$PORT:5432 \
    -v jarvis-pgdata:/var/lib/postgresql -e POSTGRES_USER=jarvis -e POSTGRES_PASSWORD="$PW" -e POSTGRES_DB=jarvis \
    postgres:18-alpine >/dev/null
  [ -f "$CFG" ] || echo '{}' > "$CFG"
  umask 077
  python3 - "$CFG" "postgres://jarvis:$PW@127.0.0.1:$PORT/jarvis?sslmode=disable" <<'PY'
import json, sys
path, url = sys.argv[1], sys.argv[2]
cfg = json.load(open(path))
cfg["database_url"] = url
json.dump(cfg, open(path, "w"), indent=2, ensure_ascii=False)
PY
  chmod 600 "$CFG"
  echo "created $NAME"
fi
docker start "$NAME" >/dev/null
# The port Postgres listens on inside the container, for the pg_isready / psql / createdb run there (they default to
# 5432): 5432 in the container created above (published as 127.0.0.1:$PORT), but one on the host's network
# (--network host, where publishing a port does not work) listens on the port the hub connects to, database_url's.
INNER=5432
if [ "$(docker inspect -f '{{.HostConfig.NetworkMode}}' "$NAME" 2>/dev/null)" = host ]; then
  INNER=$(python3 -c 'import sys, urllib.parse as u
try: print(u.urlsplit(sys.argv[1]).port or 5432)
except ValueError: print(5432)' "$(url)")
fi
in_db() { docker exec -e PGPORT="$INNER" "$NAME" "$@"; }
for _ in $(seq 1 30); do
  in_db pg_isready -U jarvis -d jarvis >/dev/null 2>&1 && break
  sleep 1
done
ready=$(in_db pg_isready -U jarvis -d jarvis 2>&1) ||
  { echo "postgres in $NAME is not ready on port $INNER inside the container after 30s: $ready" >&2; exit 1; }
# Throwaway database for scripts/test/test-server.sh.
in_db psql -U jarvis -d jarvis -tAc "SELECT 1 FROM pg_database WHERE datname='jarvis_test'" | grep -q 1 ||
  in_db createdb -U jarvis jarvis_test
[ -n "$(url)" ] || { echo "hub.json has no database_url" >&2; exit 1; }
echo "postgres ready on 127.0.0.1:$PORT"
