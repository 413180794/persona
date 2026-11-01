#!/bin/bash
# Live tests of the Codex question / approval cards against real Codex TUIs, in a herdr session of their own (never
# the one you work in):
#   JARVIS_CODEX_BINS="codex /path/to/other/codex" JARVIS_CODEX_DAEMON_BINS="/path/to/codex-0.163" \
#     JARVIS_CODEX_ARGS="-m <model>" ./scripts/test-codex.sh
# JARVIS_CODEX_BINS: the terminal route (docs/codex.md「提问和审批」). JARVIS_CODEX_DAEMON_BINS: the app-server route
# (「守护进程上的会话」) — a `codex app-server` of the test's own on a private socket, a TUI attached with --remote, and the
# first of JARVIS_CODEX_BINS beside it to check it stays on the terminal route. Every case runs JARVIS_CODEX_ROUNDS
# times (default 2); JARVIS_CODEX_COLS=<n> adds a run of each in a window that many columns wide; JARVIS_CODEX_CASES=a,b
# runs only those. The Codex use your ~/.codex login and send
# real (small) model requests. Trusting the scratch folders writes them into ~/.codex/config.toml and each test thread
# leaves a rollout in ~/.codex/sessions: both are removed again on exit. Go builds locally when there is a Go
# toolchain, else on the hub machine. Prints one RESULT line per run and a last one for all.
set -uo pipefail
cd "$(dirname "$0")/.."
SESSION="jarvis-codex-$$"
SOCK="${XDG_CONFIG_HOME:-$HOME/.config}/herdr/sessions/$SESSION/herdr.sock"
WORK=$(mktemp -d /tmp/jarvis-codex-XXXX)
REAL_WORK=$(cd "$WORK" && pwd -P)
cleanup() {
  herdr --session "$SESSION" server stop >/dev/null 2>&1 || true # by name: never the server you work in
  python3 - "$REAL_WORK" <<'PY'
import json, os, re, sys, glob, time
work = sys.argv[1]
home = os.path.expanduser("~/.codex")
cfg = os.path.join(home, "config.toml")
if os.path.exists(cfg):
    text = open(cfg).read()
    # [projects."<work>/…"] blocks the trust dialogs wrote, up to the next table
    kept = re.sub(r'(?m)^\[projects\."' + re.escape(work) + r'[^"]*"\]\n(?:(?!\[).*\n?)*', "", text)
    if kept != text:
        open(cfg, "w").write(kept)
for p in glob.glob(os.path.join(home, "sessions", "*", "*", "*", "rollout-*.jsonl")):
    if time.time() - os.path.getmtime(p) > 86400:
        continue
    try:
        with open(p) as f:
            meta = json.loads(f.readline())
    except Exception:
        continue
    if meta.get("type") == "session_meta" and str(meta.get("payload", {}).get("cwd", "")).startswith(work):
        os.remove(p)
PY
  rm -rf "$WORK" "$(dirname "$SOCK")"
}
trap cleanup EXIT

if command -v go >/dev/null; then
  (cd server && go test -c -tags codexlive -o "$WORK/node.test" ./internal/node/) || exit 1
else
  eval "$(python3 scripts/jarvis_config.py env)"
  d=".cache/jarvis-test-$(basename "$PWD")"
  ssh "$JARVIS_HUB_SSH" "mkdir -p ~/$d"
  rsync -a --delete --exclude bin server/ "$JARVIS_HUB_SSH":"$d/server/" || exit 1
  ssh "$JARVIS_HUB_SSH" "cd ~/$d/server && GOOS=$(uname -s | tr A-Z a-z) GOARCH=$(uname -m | sed s/x86_64/amd64/) \
    CGO_ENABLED=0 /usr/local/go/bin/go test -c -tags codexlive -o /tmp/jarvis-$$-node.test ./internal/node/" || exit 1
  scp -q "$JARVIS_HUB_SSH:/tmp/jarvis-$$-node.test" "$WORK/node.test" && ssh "$JARVIS_HUB_SSH" "rm -f /tmp/jarvis-$$-node.test"
fi

( exec herdr --session "$SESSION" server ) >"$WORK/server.log" 2>&1 </dev/null &
for _ in $(seq 1 40); do [ -S "$SOCK" ] && break; sleep 0.25; done
[ -S "$SOCK" ] || { echo "herdr test session did not start" >&2; cat "$WORK/server.log" >&2; exit 1; }

failed=0
run() { # test bin [cols] [env…]
  local log="$WORK/run-$RANDOM.log" test=$1 bin=$2 cols=${3:-}
  shift 3 2>/dev/null || shift $#
  env HERDR_SOCKET_PATH="$SOCK" JARVIS_CODEX_BIN="$bin" JARVIS_CODEX_COLS="$cols" JARVIS_CODEX_WORK="$WORK" \
    JARVIS_CODEX_ROUNDS="${JARVIS_CODEX_ROUNDS:-2}" "$@" "$WORK/node.test" -test.run "$test\$" -test.v -test.timeout 3h >"$log" 2>&1
  local rc=$?
  grep -E "LIVE |^--- |^(ok|FAIL|PASS)" "$log"
  [ $rc -ne 0 ] && { failed=1; tail -60 "$log"; }
  grep "SERVICE-RETRY" "$log" >>"$WORK/service-retries.log"
  echo "RESULT $test $bin${cols:+ cols=$cols}: exit $rc, $(grep -c ': ok in' "$log") ok, $(grep -c ': FAIL after' "$log") failed, $(grep -c 'SERVICE-RETRY' "$log") retried after a model service error"
}
first_bin=""
for bin in ${JARVIS_CODEX_BINS-codex}; do
  first_bin=${first_bin:-$bin}
  run TestLiveCodexCards "$bin"
  [ -n "${JARVIS_CODEX_COLS:-}" ] && run TestLiveCodexCards "$bin" "$JARVIS_CODEX_COLS"
done
for bin in ${JARVIS_CODEX_DAEMON_BINS:-}; do
  run TestLiveCodexDaemon "$bin" "" JARVIS_CODEX_DAEMON=1 JARVIS_CODEX_FALLBACK="$first_bin"
  [ -n "${JARVIS_CODEX_COLS:-}" ] && run TestLiveCodexDaemon "$bin" "$JARVIS_CODEX_COLS" JARVIS_CODEX_DAEMON=1 JARVIS_CODEX_FALLBACK="$first_bin"
done
echo "RESULT all: $([ $failed -eq 0 ] && echo passed || echo FAILED)"
exit $failed
