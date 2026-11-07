#!/bin/bash
set -euo pipefail
CI_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$CI_ROOT"
ci_run() {
  local label="$1" seconds="$2"
  shift 2
  python3 "$CI_ROOT/scripts/ci/run.py" --label "$label" --timeout "$seconds" -- "$@"
}
