#!/bin/bash
# Publish a signed APK through the shared, validated release entry point.
set -euo pipefail
SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
exec python3 "$SCRIPT_DIR/publish_android_apk.py" "$@"
