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
VERSION="$(git describe --always --dirty 2>/dev/null || echo dev)-$(date +%Y%m%d%H%M)"
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
LDFLAGS="-s -w -X main.version=$VERSION"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o bin/jarvisd-linux-amd64 ./cmd/jarvisd
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o bin/jarvisd-darwin-arm64 ./cmd/jarvisd
echo "built $VERSION"

install_bin() { # machine-id binary
  local id=$1 bin=$2 host here
  host=$(field "$id" ssh); host=${host:-$id}
  here=" $(field "$id" hostnames) "
  # This machine (listed by host name in the config): copy instead of going through ssh.
  if [[ "$here" == *" $(hostname -s) "* || "$here" == *" $(scutil --get LocalHostName 2>/dev/null) "* ]]; then
    mkdir -p ~/.jarvis/bin && cp "$bin" ~/.jarvis/bin/jarvisd.new && mv ~/.jarvis/bin/jarvisd.new ~/.jarvis/bin/jarvisd
    restart_resident
  else
    ssh -o ConnectTimeout=10 "$host" 'mkdir -p ~/.jarvis/bin' &&
      scp -q "$bin" "$host:.jarvis/bin/jarvisd.new" &&
      ssh "$host" 'mv ~/.jarvis/bin/jarvisd.new ~/.jarvis/bin/jarvisd && chmod 755 ~/.jarvis/bin/jarvisd' &&
      ssh "$host" 'bash -s restart' < ../scripts/node-launchd.sh 2>/dev/null || true
  fi
}

# A Mac running a resident node (scripts/node-launchd.sh) keeps the old binary until it restarts.
restart_resident() { [ "$(uname -s)" = Darwin ] && ../scripts/node-launchd.sh restart || true; }

for t in "${TARGETS[@]}"; do
  case "$t" in
    "$HUB")
      ssh "$JARVIS_HUB_SSH" 'bash -s' < ../scripts/db.sh
      # Every node binary of this build stays on the hub machine: the app's 重新部署 sends a machine whose node is not
      # the hub's build its own (server/internal/hub/deploy.go).
      ssh "$JARVIS_HUB_SSH" 'mkdir -p ~/.jarvis/bin/nodes' &&
        scp -q bin/jarvisd-linux-amd64 bin/jarvisd-darwin-arm64 "$JARVIS_HUB_SSH:.jarvis/bin/nodes/"
      install_bin "$t" "bin/jarvisd-$(field "$t" os)-$( [ "$(field "$t" os)" = darwin ] && echo arm64 || echo amd64)"
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
      if install_bin "$t" "$bin"; then echo "$t: installed"; else echo "$t: unreachable, skipped"; fi ;;
  esac
done
