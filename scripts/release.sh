#!/bin/bash
# Compatibility entry point; implementation lives under scripts/release/.
exec "$(dirname "$0")/release/release.sh" "$@"
