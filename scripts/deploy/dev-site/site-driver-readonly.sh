#!/bin/sh
set -eu
if [ "$#" -ne 1 ] || [ "$1" != inspect ]; then
    printf '%s\n' 'production driver is inspection-only; no verified mutation routes' >&2
    exit 1
fi
exec python3 "$(dirname "$0")/site-readonly.py" --adapter-inspect
