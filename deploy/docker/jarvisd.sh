#!/bin/sh
# jarvisd in the hub image: as the jarvis user whoever calls it, so `docker compose exec hub jarvisd hub pair` (root
# by default) reads the hub's files and anything it creates (the tailcat key) stays the hub's.
if [ "$(id -u)" = 0 ]; then
  exec su-exec jarvis /opt/jarvis/jarvisd "$@"
fi
exec /opt/jarvis/jarvisd "$@"
