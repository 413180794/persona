#!/bin/bash
# Build jarvisd and install it on every machine; (re)start the hub on example-machine.
#   ./scripts/deploy.sh                 # all machines
#   ./scripts/deploy.sh example-machine          # only the hub
#   ./scripts/deploy.sh example-machine      # only one Mac (node binary; the hub restarts its connection)
set -euo pipefail
cd "$(dirname "$0")/../server"
VERSION="$(git describe --always --dirty 2>/dev/null || echo dev)-$(date +%Y%m%d%H%M)"
TARGETS=("$@")
[ ${#TARGETS[@]} -eq 0 ] && TARGETS=(example-machine example-machine example-machine mac-home)
mkdir -p bin
LDFLAGS="-s -w -X main.version=$VERSION"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o bin/jarvisd-linux-amd64 ./cmd/jarvisd
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o bin/jarvisd-darwin-arm64 ./cmd/jarvisd
echo "built $VERSION"

install_bin() { # host binary
  local host=$1 bin=$2
  if [ "$host" = "$(scutil --get LocalHostName 2>/dev/null)" ] || { [ "$host" = example-machine ] && [ "$(hostname -s)" = example-machine ]; }; then
    mkdir -p ~/.jarvis/bin && cp "$bin" ~/.jarvis/bin/jarvisd.new && mv ~/.jarvis/bin/jarvisd.new ~/.jarvis/bin/jarvisd
  else
    ssh -o ConnectTimeout=10 "$host" 'mkdir -p ~/.jarvis/bin' &&
      scp -q "$bin" "$host:.jarvis/bin/jarvisd.new" &&
      ssh "$host" 'mv ~/.jarvis/bin/jarvisd.new ~/.jarvis/bin/jarvisd && chmod 755 ~/.jarvis/bin/jarvisd'
  fi
}

for t in "${TARGETS[@]}"; do
  case "$t" in
    example-machine)
      ssh example-machine 'bash -s' < ../scripts/db.sh
      install_bin example-machine bin/jarvisd-linux-amd64
      ssh example-machine 'bash -s' <<'EOF'
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
      echo "example-machine: hub restarted" ;;
    mac-*)
      if install_bin "$t" bin/jarvisd-darwin-arm64; then echo "$t: installed"; else echo "$t: unreachable, skipped"; fi ;;
    *) echo "unknown target $t" >&2; exit 2 ;;
  esac
done
