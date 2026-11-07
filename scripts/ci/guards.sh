#!/bin/bash
set -euo pipefail
source "$(dirname "$0")/common.sh"
ci_run 'Repository privacy' 180 python3 scripts/privacy_check.py
if [ -n "${CI_BASE_SHA:-}" ]; then
  ci_run 'Commit and changed-file privacy' 180 python3 scripts/privacy_check.py --range "${CI_BASE_SHA}..HEAD"
fi
ci_run 'Case-sensitive paths and external references' 180 bash -c 'python3 scripts/test/test_path_case.py && python3 scripts/test/test_external_service_refs.py'
