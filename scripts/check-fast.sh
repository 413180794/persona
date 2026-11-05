#!/bin/bash
# Compatibility entry point; implementation lives under scripts/test/.
exec "$(dirname "$0")/test/check-fast.sh" "$@"
