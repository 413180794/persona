#!/bin/bash
# Runs on the hub machine (deploy.sh pipes it over ssh): make sure the Jarvis Postgres container is up and that
# ~/.jarvis/hub.json has database_url. Idempotent; the password is generated once and lives only in hub.json (0600).
#   container jarvis-postgres · image postgres:18-alpine · 127.0.0.1:18661 · volume jarvis-pgdata
set -euo pipefail
NAME=jarvis-postgres
PORT=18661
CFG="$HOME/.jarvis/hub.json"
mkdir -p "$HOME/.jarvis" && chmod 700 "$HOME/.jarvis"

url() { python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("database_url",""))' "$CFG" 2>/dev/null || true; }

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
for _ in $(seq 1 30); do
  docker exec "$NAME" pg_isready -U jarvis -d jarvis >/dev/null 2>&1 && break
  sleep 1
done
docker exec "$NAME" pg_isready -U jarvis -d jarvis >/dev/null
# Throwaway database for scripts/test-server.sh.
docker exec "$NAME" psql -U jarvis -d jarvis -tAc "SELECT 1 FROM pg_database WHERE datname='jarvis_test'" | grep -q 1 ||
  docker exec "$NAME" createdb -U jarvis jarvis_test
[ -n "$(url)" ] || { echo "hub.json has no database_url" >&2; exit 1; }
echo "postgres ready on 127.0.0.1:$PORT"
