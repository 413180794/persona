#!/bin/bash
# Compatibility entry point; implementation lives under scripts/deploy/.
exec "$(dirname "$0")/deploy/deploy.sh" "$@"
