#!/bin/bash
set -euo pipefail
source "$(dirname "$0")/common.sh"
ci_run 'Web install' 600 bash -c 'cd web && npm ci --no-audit --no-fund'
ci_run 'Web lint' 300 bash -c 'cd web && npm run lint'
ci_run 'Web typecheck' 300 bash -c 'cd web && npm run typecheck'
ci_run 'Web tests' 600 bash -c 'cd web && npm test'
ci_run 'Web build and privacy' 600 bash -c 'cd web && npm run build'
