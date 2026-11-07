#!/bin/bash
# Optionally slim a simulator for UI tests with simslim (https://github.com/mobai-app/simslim): turns off the
# background services the tests don't use (docs/operations/testing.md "UI tests"). Without simslim it changes nothing.
#   scripts/app/slim-sim.sh <udid> [profile]   # profile: default apple/simslim/default.json
# Usually called by scripts/app/sim-boot.sh (the entry that picks the profile and never reboots a booted simulator).
# Run it after taking the ui lock (it boots the simulator) and before the tests. A shut-down simulator ends up
# booted (the read-back needs it). A booted one that already matches the profile is only verified, not rebooted;
# one that drifted (services missing, or more disabled than the profile, e.g. slimmed fully before) is rebooted
# once with exactly the profile's set, since `on --no-reboot` can only disable more.
# SIMSLIM=<path> picks the binary. Usage errors (no udid, unknown udid, a profile that isn't JSON) exit 2 before
# anything is touched; any later error puts the simulator back to stock and exits 0.
# Last line: slim-sim: slim|skipped|failed→stock <udid>  (failed→unknown, exit 1: not even stock; recreate it)
set -uo pipefail
UDID="${1:-}"
[ -n "$UDID" ] || { echo "usage: scripts/app/slim-sim.sh <udid> [profile]" >&2; exit 2; }
PROFILE="${2:-}"
if [ -n "$PROFILE" ] && [ "${PROFILE#/}" = "$PROFILE" ]; then PROFILE="$PWD/$PROFILE"; fi
cd "$(dirname "$0")/../.." || exit 2
PROFILE="${PROFILE:-$PWD/apple/simslim/default.json}"

done_with() { echo "slim-sim: $1 $UDID"; exit "${2:-0}"; }

SIMSLIM="${SIMSLIM:-simslim}"
if ! command -v "$SIMSLIM" >/dev/null 2>&1; then
  echo "slim-sim: skipped (simslim not installed)"
  done_with skipped
fi
python3 -c '
import json, sys
try: json.load(open(sys.argv[1]))
except Exception as e: sys.exit(f"slim-sim: {e}")' "$PROFILE" ||
  { echo "slim-sim: profile $PROFILE is not valid JSON; nothing changed" >&2; exit 2; }

eval "$(python3 scripts/persona_config.py env)"
# simslim's own limit for one boot + reconfigure (a first boot under load took 439s).
export SIMSLIM_BOOT_TIMEOUT="${SIMSLIM_BOOT_TIMEOUT:-15m}"
LIMIT="${SLIM_SIM_TIMEOUT:-1200}"
# timed <seconds> <cmd...>: runs cmd in its own process group and kills the group (TERM, then KILL after 5 s)
# when time is up; exit 124 on timeout. Not `alarm; exec`: simslim is a Go binary and Go ignores SIGALRM.
timed() {
  perl -e '
    my $t = shift; my $p = fork // exit 125;
    if (!$p) { setpgrp(0, 0); exec @ARGV or exit 127 }
    $SIG{ALRM} = sub {
      kill TERM => -$p;
      for (1 .. 10) { exit 124 if waitpid($p, 1) == $p; select undef, undef, undef, 0.5 }
      kill KILL => -$p; waitpid $p, 0; exit 124;
    };
    alarm $t; waitpid $p, 0;
    exit($? & 127 ? 128 + ($? & 127) : $? >> 8);' "$@"
}
run() {
  timed "$LIMIT" "$SIMSLIM" "$@"
  local rc=$?
  [ "$rc" -ne 124 ] || echo "slim-sim: simslim $1 timed out after ${LIMIT}s" >&2
  return "$rc"
}

# "<state> <platform>" of exactly this udid; empty when simctl doesn't know it.
read -r STATE PLATFORM < <(xcrun simctl list devices -j | python3 -c '
import json, sys
for runtime, devices in json.load(sys.stdin)["devices"].items():
    for d in devices:
        if d["udid"] == sys.argv[1]:
            print(d["state"].replace(" ", "_"), runtime.rsplit(".", 1)[-1].split("-")[0])
' "$UDID")
[ -n "${STATE:-}" ] || { echo "slim-sim: no simulator $UDID" >&2; exit 2; }
if [ "$PLATFORM" != iOS ] && [ "$PLATFORM" != watchOS ]; then
  echo "slim-sim: skipped ($PLATFORM simulator; the profiles are for iPhone and Apple Watch)"
  done_with skipped
fi

slim() {
  case "$STATE" in
    Shutdown)
      # writes the overrides while it is off, then boots it slim (one boot)
      run on "$UDID" --profile "$PROFILE" || return 1
      timed 300 xcrun simctl bootstatus "$UDID" -b >/dev/null || return 1 ;;
    Booted)
      run verify "$UDID" --profile "$PROFILE" >/dev/null 2>&1 && return 0
      echo "slim-sim: drifted from the profile, reconfiguring (reboots the simulator)" >&2
      run on "$UDID" --profile "$PROFILE" || return 1 ;;
    *) echo "slim-sim: simulator is $STATE, wait for it to finish" >&2; return 1 ;;
  esac
}

if slim && run verify "$UDID" --profile "$PROFILE"; then
  done_with slim
fi
echo "slim-sim: slimming failed, restoring stock" >&2
# off re-enables every service. --preserve-boot-state keeps the current state: a simulator that is shut down now
# stays shut down; a booted one (a Shutdown start is usually booted by this point) is rebooted and stays booted.
if run off "$UDID" --preserve-boot-state; then
  done_with failed→stock
fi
echo "slim-sim: simslim off failed too; shut down and delete $UDID, then create a new one" >&2
done_with failed→unknown 1
