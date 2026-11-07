#!/bin/bash
# Live tests of the node against a real herdr, in a herdr session of their own (never the one you work in), with a
# throwaway claude in a fresh directory for the screen checks (docs/operations/testing.md "herdr integration"):
#   ./scripts/test/test-herdr.sh
# Needs herdr and claude on this machine. Go builds locally when there is a Go toolchain, else on the hub machine.
set -euo pipefail
cd "$(dirname "$0")/../.."
SESSION="persona-test-$$"
SOCK="${XDG_CONFIG_HOME:-$HOME/.config}/herdr/sessions/$SESSION/herdr.sock"
WORK=$(mktemp -d /tmp/persona-herdr-XXXX)
H() { HERDR_SOCKET_PATH="$SOCK" herdr "$@"; }
cleanup() {
  H pane send-text "${PANE:-none}" "/exit" >/dev/null 2>&1 || true
  H pane send-keys "${PANE:-none}" enter >/dev/null 2>&1 || true
  sleep 1
  herdr --session "$SESSION" server stop >/dev/null 2>&1 || true # by name: never the server you work in
  rm -rf "$WORK" "$(dirname "$SOCK")"
}
trap cleanup EXIT

# Test binaries (darwin when built remotely: this script drives the herdr on this machine).
build() { # package output
  if command -v go >/dev/null; then
    (cd server && go test -c -tags herdrlive -o "$2" "$1")
  else
    eval "$(python3 scripts/persona_config.py env)"
    local d=".cache/persona-test-$(basename "$PWD")"
    rsync -a --delete --exclude bin server/ "$PERSONA_HUB_SSH":"$d/server/"
    ssh "$PERSONA_HUB_SSH" "cd ~/$d/server && GOOS=$(uname -s | tr A-Z a-z) GOARCH=$(uname -m | sed s/x86_64/amd64/) \
      CGO_ENABLED=0 /usr/local/go/bin/go test -c -tags herdrlive -o /tmp/persona-$$-$(basename "$2") $1"
    scp -q "$PERSONA_HUB_SSH:/tmp/persona-$$-$(basename "$2")" "$2"
  fi
}
build ./internal/herdr/ "$WORK/herdr.test"
build ./internal/node/ "$WORK/node.test"

herdr --session "$SESSION" server >"$WORK/server.log" 2>&1 &
for _ in $(seq 1 40); do [ -S "$SOCK" ] && break; sleep 0.25; done
[ -S "$SOCK" ] || { echo "herdr test session did not start" >&2; cat "$WORK/server.log" >&2; exit 1; }
export HERDR_SOCKET_PATH="$SOCK"

mkdir -p "$WORK/project"
PANE=$(H workspace create --cwd "$WORK/project" --label persona-test --no-focus | python3 -c "import json,sys;print(json.load(sys.stdin)['result']['root_pane']['pane_id'])")
echo "== herdr client (socket vs CLI, events)"
"$WORK/herdr.test" -test.run Live -test.v 2>&1 | grep -E "^(=== RUN|--- |PASS|FAIL|ok)|live_test"

screen() { H pane read "$PANE" --source visible; }
wait_for() { for _ in $(seq 1 60); do screen | grep -q -i -E "$1" && return 0; sleep 0.5; done; echo "timed out waiting for /$1/" >&2; screen >&2; return 1; }
check() { # want [clear]
  PERSONA_LIVE_PANE="$PANE" PERSONA_LIVE_WANT="$1" PERSONA_LIVE_CLEAR="${2:-}" "$WORK/node.test" -test.run 'TestLiveCheckPane$' -test.v 2>&1 | grep -E "^(--- |FAIL)|live_test"
}

echo "== claude in a fresh directory: the trust dialog"
H pane run "$PANE" claude >/dev/null
wait_for "trust|shift\+tab"
if screen | grep -q -i trust; then
  check dialog_open
  PERSONA_LIVE_PANE="$PANE" PERSONA_LIVE_OPTION=2 "$WORK/node.test" -test.run TestLiveAnswerDialog -test.v 2>&1 | grep -E "^(--- |FAIL)|live_test"
fi
wait_for "shift\+tab"
sleep 1
echo "== the input box: empty, a draft, the draft cleared"
check ""
H pane send-text "$PANE" "half typed" >/dev/null
sleep 0.5
check input_draft
check input_draft "something else"
check "" "half typed"
echo "== done"
