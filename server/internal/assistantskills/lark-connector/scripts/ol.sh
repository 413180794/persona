#!/usr/bin/env bash
# Thin client for the host-only lark-connector agent API.
set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
BASE="${lark-connector_AGENT_BASE:-http://127.0.0.1:8081}"
BASE="${BASE%/}"

usage() {
  cat >&2 <<'EOF'
usage: ol METHOD PATH [JSON | @body.json] [--upload FILE] [curl args]
       ol events [--feed ID] [--types a,b] [--others] [--max N] [--seconds S]

  PATH is relative to /agent/v1 ("/" is the live route catalog).
  JSON is sent as the request body; POST/PUT/PATCH without a body send {}.
  --upload FILE sends FILE as raw application/octet-stream bytes.
  Encode query values with --url-query 'q=中文' (or -G --data-urlencode).
  Writes carry an Origin header derived from the base URL (OL_NO_ORIGIN=1 skips it).
  Exits non-zero on HTTP >= 400 and still prints the error body.

env: lark-connector_AGENT_BASE   default http://127.0.0.1:8081
     lark-connector_AGENT_TOKEN  bearer token (else read from lark-connector_ENV_FILE or the repo .env)
     lark-connector_ENV_FILE     .env file holding lark-connector_AGENT_TOKEN
     lark-connector_ACCOUNT      account id to act as (default: the primary account)
     OL_TIMEOUT             seconds (default 60, 600 with --upload)
EOF
  exit 2
}

token_from_env_file() {
  local file=$1 line value=""
  [[ -n "$file" && -f "$file" ]] || return 1
  while IFS= read -r line || [[ -n "$line" ]]; do
    if [[ "$line" =~ ^[[:space:]]*lark-connector_AGENT_TOKEN=(.*)$ ]]; then
      value=${BASH_REMATCH[1]%$'\r'}
    fi
  done <"$file"
  if [[ "$value" == \"*\" && "$value" == *\" ]] ||
    [[ "$value" == \'*\' && "$value" == *\' ]]; then
    value=${value:1:${#value}-2}
  fi
  [[ -n "$value" ]] || return 1
  printf '%s' "$value"
}

resolve_token() {
  local cwd_root script_root candidate
  if [[ -n "${lark-connector_AGENT_TOKEN:-}" ]]; then
    printf '%s' "$lark-connector_AGENT_TOKEN"
    return
  fi
  cwd_root=$(git rev-parse --show-toplevel 2>/dev/null || true)
  script_root=$(cd "$script_dir/../../../.." 2>/dev/null && pwd -P || true)
  for candidate in \
    "${lark-connector_ENV_FILE:-}" \
    "${cwd_root:+$cwd_root/.env}" \
    "${script_root:+$script_root/.env}"; do
    token_from_env_file "$candidate" && return
  done
  return 0
}

lower() { printf '%s' "$1" | tr '[:upper:]' '[:lower:]'; }

[[ $# -ge 1 ]] || usage

TOKEN=$(resolve_token)

if [[ "$1" == "events" ]]; then
  shift
  lark-connector_AGENT_TOKEN="$TOKEN" lark-connector_AGENT_BASE="$BASE" \
    exec node "$script_dir/ol-events.mjs" "$@"
fi

[[ $# -ge 2 ]] || usage
method=$(printf '%s' "$1" | tr '[:lower:]' '[:upper:]')
path=$2
shift 2

[[ "$path" == /* ]] || path="/$path"
if [[ "$path" == "/" ]]; then
  path="/agent/v1"
elif [[ "$path" != /agent/v1* ]]; then
  path="/agent/v1$path"
fi

body_json=""
body_file=""
if [[ $# -gt 0 && ( "$1" == \{* || "$1" == \[* ) ]]; then
  body_json=$1
  shift
elif [[ $# -gt 0 && "$1" == @* && -f "${1#@}" ]]; then
  body_file=${1#@}
  shift
fi

upload=""
has_data=0
has_ctype=0
has_origin=0
has_get=0
extra=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --upload)
      [[ $# -ge 2 ]] || usage
      upload=$2
      shift 2
      continue
      ;;
    --upload=*)
      upload=${1#--upload=}
      shift
      continue
      ;;
    -H | --header)
      [[ $# -ge 2 ]] || usage
      header=$(lower "$2")
      [[ "$header" == content-type:* ]] && has_ctype=1
      [[ "$header" == origin:* ]] && has_origin=1
      extra+=("$1" "$2")
      shift 2
      continue
      ;;
    --json | --json=*)
      has_data=1
      has_ctype=1
      ;;
    -d | --data | --data-raw | --data-binary | --data-ascii | --data-urlencode | \
      -d?* | --data=* | --data-raw=* | --data-binary=* | --data-ascii=* | --data-urlencode=*)
      has_data=1
      ;;
    -G | --get)
      has_get=1
      ;;
  esac
  extra+=("$1")
  shift
done

timeout=${OL_TIMEOUT:-60}
[[ -n "$upload" ]] && timeout=${OL_TIMEOUT:-600}

# --fail-with-body needs curl >= 7.76. Older curl (e.g. Debian 10 ships 7.64)
# rejects the option outright, so emulate it there: dump the response headers
# to a temp file and turn a final HTTP status >= 400 into curl's exit code 22.
status_file=""
if curl --fail-with-body -V >/dev/null 2>&1; then
  args=(-sS --fail-with-body -m "$timeout" -X "$method")
else
  status_file=$(mktemp "${TMPDIR:-/tmp}/ol-headers.XXXXXX")
  trap 'rm -f "$status_file"' EXIT
  args=(-sS -D "$status_file" -m "$timeout" -X "$method")
fi
[[ -n "$TOKEN" ]] && args+=(-H "Authorization: Bearer $TOKEN")
[[ -n "${lark-connector_ACCOUNT:-}" ]] && args+=(-H "x-lark-connector-account: $lark-connector_ACCOUNT")

if [[ "$method" != "GET" && "$method" != "HEAD" && "${OL_NO_ORIGIN:-}" != "1" && "$has_origin" == 0 ]]; then
  origin=$(printf '%s' "$BASE" | sed -E 's#^([A-Za-z][A-Za-z0-9+.-]*://[^/]+).*#\1#')
  args+=(-H "Origin: $origin")
fi

json_type=(-H "content-type: application/json")
json_stdin=0
if [[ -n "$upload" ]]; then
  [[ -f "$upload" ]] || {
    echo "ol: upload file not found: $upload" >&2
    exit 2
  }
  args+=(-H "content-type: application/octet-stream" --data-binary "@$upload")
elif [[ -n "$body_json" ]]; then
  case "${OSTYPE-}" in
    msys*)
      args+=(--data-binary @-)
      json_stdin=1
      ;;
    *) args+=(--data-raw "$body_json") ;;
  esac
  [[ "$has_ctype" == 1 ]] || args+=("${json_type[@]}")
elif [[ -n "$body_file" ]]; then
  args+=(--data-binary "@$body_file")
  [[ "$has_ctype" == 1 ]] || args+=("${json_type[@]}")
elif [[ "$has_data" == 1 && "$has_get" == 0 ]]; then
  [[ "$has_ctype" == 1 ]] || args+=("${json_type[@]}")
elif [[ "$has_get" == 0 && ( "$method" == "POST" || "$method" == "PUT" || "$method" == "PATCH" ) ]]; then
  args+=(--data-raw '{}')
  [[ "$has_ctype" == 1 ]] || args+=("${json_type[@]}")
fi

run_curl() {
  if [[ "$json_stdin" == 1 ]]; then
    printf '%s' "$body_json" | curl "${args[@]}" "$BASE$path" ${extra[@]+"${extra[@]}"}
  else
    curl "${args[@]}" "$BASE$path" ${extra[@]+"${extra[@]}"}
  fi
}

if [[ -z "$status_file" ]]; then
  if [[ "$json_stdin" == 1 ]]; then
    run_curl
    exit "$?"
  fi
  exec curl "${args[@]}" "$BASE$path" ${extra[@]+"${extra[@]}"}
fi

status=0
run_curl || status=$?
if [[ "$status" == 0 ]]; then
  http_status=$(sed -n -E 's#^HTTP/[0-9.]+ ([0-9]{3}).*#\1#p' "$status_file" | tail -n 1)
  if [[ -n "$http_status" && "$http_status" -ge 400 ]]; then
    echo "curl: (22) The requested URL returned error: $http_status" >&2
    status=22
  fi
fi
exit "$status"
