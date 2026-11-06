#!/usr/bin/env bash
# Regenerate the trusted source-runtime adapter; no extension lifecycle scripts run.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
bun build "$ROOT/server/internal/extension/denohost/adapter.ts" --target=browser --minify --outfile "$ROOT/server/internal/extension/denohost/adapter.mjs"
