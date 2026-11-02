#!/bin/bash
# Build jarvisd and install it on every machine of the config (docs/config.md); (re)start the hub on its machine.
#   ./scripts/deploy.sh                 # all machines
#   ./scripts/deploy.sh <hub machine>   # only the hub
#   ./scripts/deploy.sh <machine id>    # only one machine (node binary; the hub restarts its connection)
#   ./scripts/deploy.sh web             # only the web client (web/, docs/web.md): no restart
set -euo pipefail
cd "$(dirname "$0")/.."
eval "$(python3 scripts/jarvis_config.py env)"
cd server
# Straight from master, for when a change cannot wait for a release: the build number is the build time (UTC), so an
# older release never replaces it and the next release does (docs/hub-update.md「直接部署」).
BUILD="$(date -u +%Y%m%d%H%M)"
VERSION="$(git describe --always --dirty 2>/dev/null || echo dev)-$BUILD"
TARGETS=("$@")
[ ${#TARGETS[@]} -eq 0 ] && read -ra TARGETS <<< "$JARVIS_MACHINES"

# The web client: built here, served by the hub from ~/.jarvis/web (swapped in whole, so a page never sees half).
deploy_web() {
  [ -f ../web/package.json ] || return 0
  (cd ../web && { [ -d node_modules ] || npm ci --no-audit --no-fund; } && npm run build) >/dev/null
  tar -C ../web/dist -czf - . | ssh "$JARVIS_HUB_SSH" 'set -e; rm -rf ~/.jarvis/web.new; mkdir -p ~/.jarvis/web.new; tar -xzf - -C ~/.jarvis/web.new; rm -rf ~/.jarvis/web.old; [ -d ~/.jarvis/web ] && mv ~/.jarvis/web ~/.jarvis/web.old; mv ~/.jarvis/web.new ~/.jarvis/web; rm -rf ~/.jarvis/web.old'
  echo "web: deployed to $JARVIS_HUB_SSH:~/.jarvis/web"
}
if [ "${TARGETS[*]}" = web ]; then deploy_web; exit 0; fi
HUB="$JARVIS_HUB_MACHINE"
# Other machines first, the hub (restart) last: the hub only picks up a new node binary when it reconnects.
ORDERED=()
for t in "${TARGETS[@]}"; do [ "$t" != "$HUB" ] && ORDERED+=("$t"); done
for t in "${TARGETS[@]}"; do [ "$t" = "$HUB" ] && ORDERED+=("$t"); done
TARGETS=("${ORDERED[@]}")
# machine <id> <field>: a field of a machine in the config (ssh host, os, hostnames).
field() { python3 ../scripts/jarvis_config.py machine "$1" | python3 -c "import json,sys;v=json.load(sys.stdin).get('$2','');print(' '.join(v) if isinstance(v,list) else v)"; }
mkdir -p bin
LDFLAGS="-s -w -X main.version=$VERSION -X main.channel=release -X main.build=$BUILD${JARVIS_PUBLISH_URL:+ -X main.updateURL=$JARVIS_PUBLISH_URL/dl}"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o bin/jarvisd-linux-amd64 ./cmd/jarvisd
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o bin/jarvisd-darwin-arm64 ./cmd/jarvisd
echo "built $VERSION"

# is_here <machine id>: the config lists this computer's host name for it.
is_here() {
  local here=" $(field "$1" hostnames) "
  [[ "$here" == *" $(hostname -s) "* || "$here" == *" $(scutil --get LocalHostName 2>/dev/null) "* ]]
}

install_bin() { # machine-id binary
  local id=$1 bin=$2 host
  host=$(field "$id" ssh); host=${host:-$id}
  # This machine (listed by host name in the config): copy instead of going through ssh.
  via=$(field "$id" via)
  if is_here "$id"; then
    mkdir -p ~/.jarvis/bin && cp "$bin" ~/.jarvis/bin/jarvisd.new && mv ~/.jarvis/bin/jarvisd.new ~/.jarvis/bin/jarvisd
    restart_resident
  elif [ -n "$via" ]; then
    # Only reachable from another machine of the config (keys or routes this one lacks): copy through it; `ssh` is
    # then its name there.
    local vh tmp=".jarvis/bin/jarvisd.for-$id"
    vh=$(field "$via" ssh); vh=${vh:-$via}
    ssh -o ConnectTimeout=10 "$vh" 'mkdir -p ~/.jarvis/bin' && scp -q "$bin" "$vh:$tmp" &&
      ssh "$vh" "ssh -o BatchMode=yes -o ConnectTimeout=15 $host 'mkdir -p ~/.jarvis/bin' &&
        scp -q ~/$tmp $host:.jarvis/bin/jarvisd.new &&
        ssh -o BatchMode=yes $host 'mv ~/.jarvis/bin/jarvisd.new ~/.jarvis/bin/jarvisd && chmod 755 ~/.jarvis/bin/jarvisd'; rc=\$?; rm -f ~/$tmp; exit \$rc"
  else
    ssh -o ConnectTimeout=10 "$host" 'mkdir -p ~/.jarvis/bin' &&
      scp -q "$bin" "$host:.jarvis/bin/jarvisd.new" &&
      ssh "$host" 'mv ~/.jarvis/bin/jarvisd.new ~/.jarvis/bin/jarvisd && chmod 755 ~/.jarvis/bin/jarvisd' &&
      ssh "$host" 'bash -s restart' < ../scripts/node-launchd.sh 2>/dev/null || true
  fi
}

# herdr on a machine that has none (scripts/ensure-herdr.sh; JARVIS_SKIP_HERDR=1 skips it): a first install should
# leave a machine whose Agents show up. Never fails the deploy.
ensure_herdr() { # machine-id
  local id=$1 host via vh
  [ "${JARVIS_SKIP_HERDR:-}" = 1 ] && return 0
  host=$(field "$id" ssh); host=${host:-$id}
  via=$(field "$id" via)
  if is_here "$id"; then
    sh ../scripts/ensure-herdr.sh
  elif [ -n "$via" ]; then
    vh=$(field "$via" ssh); vh=${vh:-$via}
    ssh -o ConnectTimeout=10 "$vh" "ssh -o BatchMode=yes -o ConnectTimeout=15 $host 'sh -s'" < ../scripts/ensure-herdr.sh
  else
    ssh -o ConnectTimeout=10 "$host" 'sh -s' < ../scripts/ensure-herdr.sh
  fi 2>&1 | sed "s/^/$id: /" || true
}

# A resident node (scripts/node-launchd.sh, Mac or Linux) keeps the old binary until it restarts.
restart_resident() { ../scripts/node-launchd.sh restart || true; }

for t in "${TARGETS[@]}"; do
  case "$t" in
    "$HUB")
      ssh "$JARVIS_HUB_SSH" 'bash -s' < ../scripts/db.sh
      # Every node binary of this build stays on the hub machine: the app's 重新部署 sends a machine whose node is not
      # the hub's build its own (server/internal/hub/deploy.go).
      ssh "$JARVIS_HUB_SSH" 'mkdir -p ~/.jarvis/bin/nodes' &&
        scp -q bin/jarvisd-linux-amd64 bin/jarvisd-darwin-arm64 "$JARVIS_HUB_SSH:.jarvis/bin/nodes/"
      install_bin "$t" "bin/jarvisd-$(field "$t" os)-$( [ "$(field "$t" os)" = darwin ] && echo arm64 || echo amd64)"
      ensure_herdr "$t"
      ssh "$JARVIS_HUB_SSH" 'bash -s' <<'EOF'
set -e
mkdir -p ~/.config/systemd/user
cat > ~/.config/systemd/user/jarvisd.service <<'UNIT'
[Unit]
Description=Jarvis hub (jarvisd)
After=network-online.target

[Service]
ExecStart=%h/.jarvis/bin/jarvisd hub
Restart=always
RestartSec=3
Environment=PATH=%h/.local/bin:%h/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin

[Install]
WantedBy=default.target
UNIT
systemctl --user daemon-reload
systemctl --user enable jarvisd >/dev/null 2>&1
systemctl --user restart jarvisd
sleep 1
systemctl --user is-active jarvisd
EOF
      echo "$t: hub restarted"
      deploy_web ;;
    *)
      [[ " $JARVIS_MACHINES " == *" $t "* ]] || { echo "unknown machine $t (not in the config)" >&2; exit 2; }
      os=$(field "$t" os); os=${os:-darwin}
      bin="bin/jarvisd-$os-$( [ "$os" = darwin ] && echo arm64 || echo amd64)"
      if install_bin "$t" "$bin"; then echo "$t: installed"; ensure_herdr "$t"; else echo "$t: unreachable, skipped"; fi ;;
  esac
done
