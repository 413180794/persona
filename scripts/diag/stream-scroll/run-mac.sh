#!/bin/bash
# The Mac app (Debug, with MacScrollLab) streaming a long reply into the Persona conversation, with synthetic trackpad
# swipes, its scroll trace and its window drawn every 0.1 s, against a mock hub on PORT (docs/apple/mac.md "Scroll experiments").
# Snapshot mode (-macSnapshot): it reads no keychain and quits at the end, so it works while the Mac is locked.
#   APP=<Persona.app> scripts/diag/stream-scroll/run-mac.sh <out dir> "13:150,17:900,23:-1400" [stream at s] [late ms]
# Gestures: "<seconds after launch>:<points>", negative toward the newest. One RESULT line.
set -u
ROOT=$(cd "$(dirname "$0")/../../.." && pwd)
OUT=${1:?out dir}; GEST=${2:-}; AT=${3:-10}; LATE=${4:-0}; PORT=${PORT:-18832}; APP=${APP:?APP=<path to Persona.app>}
rm -rf "$OUT"; mkdir -p "$OUT"
if lsof -nP -tiTCP:$PORT -sTCP:LISTEN >/dev/null 2>&1; then echo "RESULT port $PORT busy"; exit 3; fi
(cd "$ROOT" && env MOCK_LONG_HISTORY=1 PORT=$PORT python3 scripts/mock/mock_hub.py > "$OUT/mock.log" 2>&1) &
for i in $(seq 1 30); do curl -s -o /dev/null http://127.0.0.1:$PORT/api/v1/machines && break; sleep 0.5; done
MOCKPID=$(lsof -nP -tiTCP:$PORT -sTCP:LISTEN)
pair="persona://pair?url=http%3A%2F%2F127.0.0.1%3A$PORT&token=mock"
"$APP/Contents/MacOS/Persona" -ApplePersistenceIgnoreState YES -macAppearance light -macWindowSize 1000x760 \
  -macSnapshot "$OUT/end.png" -macSnapshotDelay 34 -macSnapshotQuit YES -pairURL "$pair" -openChatsFresh YES \
  -tab assistant -openAssistant server/b2e4d6f8-0000-4000-8000-000000000004 \
  -scrollTrace YES -scrollTracePath "$OUT/trace.txt" -macScrollGestures "$GEST" \
  -macScrollFrames "$OUT/frames" -macScrollFramesFrom $((AT - 2)) -macScrollFramesFor 22 -macScrollFramesEvery 0.1 > "$OUT/app.log" 2>&1 &
PID=$!
sleep "$AT"
python3 -c 'import time; print("%.3f" % time.time())' > "$OUT/stream"
curl -s -o /dev/null "http://127.0.0.1:$PORT/mock/say?preset=markdown&user=%E7%9C%8B%E4%B8%80%E4%B8%8B&stream=1&steps=120&ms=80&late=$LATE"
for i in $(seq 1 90); do kill -0 $PID 2>/dev/null || break; sleep 1; done
kill $PID 2>/dev/null; kill $MOCKPID 2>/dev/null
echo "RESULT run-mac frames $(ls "$OUT/frames" 2>/dev/null | wc -l | tr -d ' ') swipes $(grep -c 'MacScrollLab swipe' "$OUT/app.log")"
