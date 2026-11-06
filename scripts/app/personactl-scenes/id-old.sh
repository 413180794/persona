#!/bin/zsh
# id-old.sh <out dir> <sim udid> <bundle> <scene file> <combos…>: the identity shots the way tools/mini/id-shots3.sh
# takes them (one launch per shot, launch arguments, a fixed sleep), for one build, into <out dir>, with the time each
# took in <out dir>/times.tsv. A combo is <style>:<light|dark>. The mock hub must be on PORT (18897).
set -u
out=$1; S=$2; APP=$3; scenes=$4; shift 4; combos=("$@")
mkdir -p $out
: >| $out/times.tsv
shoot() { # shoot <out.png> <style> <wait> <args…>
  local png=$1 style=$2 wait=$3; shift 3
  xcrun simctl terminate $S $APP 2>/dev/null
  xcrun simctl launch $S $APP -pairURL "persona://pair?url=http%3A%2F%2F127.0.0.1%3A${PORT:-18897}&token=mock" -ui.style $style "$@" >/dev/null
  sleep $wait
  xcrun simctl io $S screenshot $png >/dev/null 2>&1
  xcrun simctl terminate $S $APP 2>/dev/null
}
t0=$(date +%s)
shoot $out/warm.png glass 20 -tab dev -dev.openChat ""
ap=""
for combo in $combos; do
  style=${combo%%:*}; appear=${combo##*:}
  [ "$appear" != "$ap" ] && { xcrun simctl ui $S appearance $appear; ap=$appear; sleep 2; }
  while read -r name wait args; do
    [ -z "$name" ] && continue
    eval "a=($args)"
    s=$(date +%s.%N)
    shoot $out/$style-$appear-$name.png $style $wait "${a[@]}"
    printf "%s\t%.2f\n" $style-$appear-$name.png $(( $(date +%s.%N) - s )) >> $out/times.tsv
  done < $scenes
done
echo "DONE id-old $out: $(ls $out/*-*-*.png | wc -l) shots in $(( $(date +%s) - t0 )) s"
