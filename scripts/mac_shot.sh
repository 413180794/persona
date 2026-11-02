#!/bin/bash
# Compatibility entry point; implementation lives under scripts/app/.
exec "$(dirname "$0")/app/mac_shot.sh" "$@"
