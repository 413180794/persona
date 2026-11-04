#!/bin/bash
# Measures the cases run-ios.sh recorded in a directory: each video decoded frame by frame (vidrows, built on first
# use), then streamrows.py over the case's stream (its STREAM start / end marks). CHROME: the half-row where the
# composer starts on the recording (1150 for a 1206 × 2622 simulator, the shell layout). One RESULT line per case.
#   scripts/diag/stream-scroll/analyze.sh /tmp/stream-shots
set -u
HERE=$(cd "$(dirname "$0")" && pwd); D=${1:?directory}; BIN=${TMPDIR:-/tmp}/stream-scroll-tools; mkdir -p "$BIN"
[ -x "$BIN/vidrows" ] || xcrun swiftc -O "$HERE/vidrows.swift" -o "$BIN/vidrows" || exit 1
for v in "$D"/test*.mp4; do
  c=$(basename "$v" .mp4)
  [ -f "$D/$c.rows.bin" ] || "$BIN/vidrows" "$v" "$D/$c.rows.bin" >/dev/null
  rs=$(cat "$D/$c.recstart"); st=$(awk '$3=="start"{print $4}' "$D/$c.marks"); en=$(awk '$3=="end"{print $4}' "$D/$c.marks")
  from=$(python3 -c "print(round($st - $rs + 1 - 0.5, 2))"); to=$(python3 -c "print(round($en - $rs + 1 + 1.5, 2))")
  chrome=(); case $c in *Following*|*Arrow*) chrome=(--chrome "${CHROME:-1150}");; esac
  echo "== $c ($from - $to s)"
  python3 -I "$HERE/streamrows.py" "$D/$c.rows.bin" --from "$from" --to "$to" --band 0.12,0.85 ${chrome[@]+"${chrome[@]}"} --out "$D/$c.tsv" | grep -v "^  t "
done
