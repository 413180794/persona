# Server: personad

`personad` is the new name; `jarvisd` remains a compatibility alias. New default runtime directory: `~/.persona`; old `~/.jarvis` remains supported. Existing systemd / launchd, database / container / volume, and release-file identities in this document are retained rather than rebuilt for the rename. Migration-version differences: [Configuration and compatibility](config.md#命名与兼容).

Go, single binary, `server/cmd/personad`. Modes: `docs/architecture.md`; protocol: docs/protocol.md.

## Code

| Package | Content |
| --- | --- |
| `internal/proto` | Wire types |
| `internal/rpc` | Bidirectional hub↔node JSON-lines RPC |
| `internal/transcript` | Claude Code JSONL → `proto.Item`, incremental reading and pagination |
| `internal/herdr` | Herdr socket API client (CLI fallback) and event subscription |
| `internal/herdrcore` | JSON-RPC client and events for herdr-core (hc behind Herdr desktop), `docs/plan-orca-support.md`, “herdr-core host” |
| `internal/node` | Sessions, launch, send, screen sampling, hook / MCP Unix sockets, `runtime.sock` (`runtimes.go`, `docs/runtimes.md`) |
| `internal/hub` | HTTP / WebSocket, machine connections, interaction queue, phone-tool forwarding, APNs |
| `internal/assistant` | personad assistant for Persona tab; separate process connected through bus, hub child by default; `docs/assistant.md`, “Process” |
| `internal/assistantwire` | Assistant / hub messages: types, methods, error codes, assistant route table; both import it, hub does not import `internal/assistant` |
| `internal/bus` | Service-to-hub bus client: dialing, handshake, reconnect, keepalive; `docs/protocol.md`, “Bus” |
| `internal/netaddr` | Local addresses, Linux netlink flags and address-change notifications; not proof of public reachability |
| `internal/db` | Postgres connection pool, embedded SQL migrations in two sets (`migrations/hub/`, `migrations/manager/`; “Migrations” below), table access |
| `internal/mail` | IMAP/SMTP, encrypted account storage, mail HTTP APIs and MCP tools (`docs/mail.md`) |
| `internal/mcpserver` | personad mcp: stdio MCP server for Claude |
| `internal/apns` | APNs client (.p 8 key, provider token, single push), shared by direct hub and relay |
| `internal/pushcrypto` | Push-body encryption with phone public key (HPKE), `docs/push-relay.md` |
| `internal/relay` | Publisher-run personad relay, `docs/push-relay.md` |
| `internal/tunnel` | Tailcat node key / endpoint, hub dialing, new-binary transfer over tailcat (“Tailcat hole punching” below) |

## Connecting to herdr

- Node directly uses herdr Unix socket (`$HERDR_SOCKET_PATH`, otherwise `$XDG_CONFIG_HOME/herdr/herdr.sock`, default `~/.config/herdr/herdr.sock`). Each call opens a connection, writes one `{"id","method","params"}` line, reads one response; 64 MB line cap, 10-second timeout. Herdr CLI is a thin client for the same API, with an extra ping per call; result shape identical. Socket failure or old herdr unknown method (`invalid_request` + `unknown variant`) falls back to CLI. Method names follow `herdr api schema --json`; pane run = `pane.send_input` + `keys:["Enter"]`; `pane.process_info` requires `pane_id`, since target would return focused pane.
- `events.subscribe` uses a persistent connection: workspace / tab / pane add, remove, update events plus `pane.agent_status_changed` per Agent pane (herdr subscribes to status by pane only). Events trigger `pollAgents` snapshot after 50 ms coalescing, at most one list rebuild per second. Entering / leaving blocked bypasses that limit, snapshot after 50 ms (“Card fast lane” below). Normally 10-second fallback poll. Without event stream, or with an internal Codex-variant pane whose status changes only spinner characters in title without `pane.updated`, poll each second.
- Subscription pitfalls: a second subscribe on the same connection is silently ignored; reopen with full pane set when it changes. Herdr closes lagging subscribers; `events_lost` may not arrive, so reconnect on closure with exponential backoff up to 30 seconds, snapshot first to fill gaps. Herdr 0.9 event names use dots like subscription (`pane.agent_status_changed`); older versions use underscores (`pane_agent_status_changed`); accept both.
- Local measurements (herdr 0.9.3, 7 Agents): no events over 30 stable seconds; tab rename → `sessions.changed` 80–140 ms, previously up to 1 second. Integration: `./scripts/test/test-herdr.sh`, isolated herdr session, `docs/testing.md` (“Herdr integration testing”).

### Card fast lane (#751)

Question, permission, and plan cards, including appearance, answer, replacement, disappearance, pass straight through every hop without throttling, coalescing, or batching. Other changes remain throttled.

| Hop | Fast path |
| --- | --- |
| Herdr detects Agent waiting for user | Herdr itself, measured 0–320 ms |
| Node reports hub | Enter / leave blocked events bypass one-per-second list rebuild; snapshot and `sessions.changed` after 50 ms coalescing |
| Hub creates card | No delay for unhooked sessions; hooked sessions wait 1.5 s (`docs/protocol.md`, “Terminal dialogs”). Read screen, parse, broadcast interaction immediately |
| Push to app | WebSocket immediately queues per connection; APNs sends immediately; Needs you Dynamic Island changes are not coalesced or limited to 30 s interval |
| App renders | Apply to model on receipt; Needs you / questions land even while Agents list updates are held (`landCritical`, `docs/ios-state.md`); chat reads model directly |

Measurements (2026-11-03, isolated herdr + real Claude Code 2.1.317, no Jarvis hook; node / hub in same process linked like SSH, `hub/cardlatency_live_test.go`):

| | Before | After |
| --- | --- | --- |
| Terminal dialog appears → app receives card | 4.0–10.4 s, 6 runs | 0–0.35 s, 8 runs; screen sampled every 25 ms, similar uncertainty |
| Herdr detection → hub knows blocked | 2.2–8.9 s | 0–0.16 s |
| Hub card creation | 1.5 s | 0 ms |
| Answer on computer → card disappears | 8.5 s | 0.17–0.59 s |
| Answer one question → next card, two questions in one AskUserQuestion | No new card | 0.61–0.72 s, including keys |

Sources of delay:
- After #611 merged herdr / Herdr App engines, the select waking `pollAgents` consumed the herdr event flag, then used it to find changed engines. None found, no snapshot; only 10-second fallback remained.
- Herdr 0.9 dotted events were not recognized by underscore-only node code, losing status-event identification.
- Hub always waited 1.5 s per screen card for a hook card that unhooked sessions would never send.

Measurement: `HERDR_SOCKET_PATH=<isolated herdr socket> JARVIS_LIVE_CARD_PANE=<Claude pane> go test -tags herdrlive -run TestLiveCardLatency ./internal/hub/`
(`JARVIS_LIVE_CARD_TWO=1` asks two questions; `JARVIS_LIVE_CARD_TRACE=1` prints hop timeline). Without local Go, cross-compile test binary on hub and copy back.
App hop guarded by UI tests: `ScreenTruthTests.testWhatNeedsTheUserShowsUnderTheFinger` (within 1 s even with finger held on list), `LiveUpdateTests.testAScreenCardInTheOpenConversation` (appears within 1 s, disappears within 1 s of computer answer).

## Connecting to herdr-core

Each machine's node uses one engine (`node/engine.go`): herdr default, herdr-core (`node/hostherdrcore.go`), or Orca (`node/hostorca.go` below). User chooses per machine in app; hub sends `engine.use` on connection (`docs/protocol.md`, “Engines”). Herdr mode is unchanged and does not touch herdr-core, except `node.info` checks availability once. Selected but stopped herdr-core returns No Terminal immediately, without dialing / logging. Socket, methods, mapping, and first-phase omissions: `docs/plan-orca-support.md` (“herdr-core host”).
Integration: `go test -tags hclive -run HerdrCoreLive ./internal/node/ -v`, own isolated herdr-core plus read-only default-instance listing.

## Connecting to Orca

For Orca machines, node controls terminals through desktop runtime Unix socket (`internal/orcart` client, `node/hostorca.go` host). Read `orca-runtime.json` for socket / token; Orca changes token on every launch, so reread on connection failure / unauthorized; `status.get` checks protocol version first.
No socket push: node polls `terminal.list` + `worktree.ps` each second, mapping Claude panes via `ORCA_PANE_KEY` in process environment.
Stopped Orca returns No Terminal immediately, no log. Mapping / first-phase omissions: `docs/plan-orca-support.md` (“Orca source”).
Read-only integration: `go test -tags orcalive -run OrcaLive ./internal/node/ -v`.

## Deployment

Two equivalent hub deployment modes: **native binary** (below, `systemd --user` + Postgres container) and **Docker** (hub + Postgres containers, for NAS / hub-only computers, `docs/docker.md`). Nodes remain `~/.persona/bin/personad` regardless of hub deployment.

New users without repository: `curl -fsSL <website>/install.sh | sh -s -- --hub`, then personad hub setup ([onboarding.md](onboarding.md), 4.1). Writes `hub.json` (new-install defaults only for new files), creates same-name / same-volume Postgres as `db.sh` or uses existing configured database, installs same socket-activation units as `hub-systemd.sh` (Mac LaunchAgent `com.jarvis.hub`), waits for hub, prints only problematic self-checks, then phone QR code and admin URL with #setup=. Safe to rerun: retain existing values and do not restart unchanged services. `--json` gives one line per step for Persona Link.

```sh
./scripts/deploy/deploy.sh                    # Build linux / darwin × amd64 / arm64, install native binary on each machine, restart hub
./scripts/deploy/deploy.sh <machine id> <machine id> # Install selected machines only
```

- Binary: each machine `~/.persona/bin/personad`. Hub launches node through `ssh <alias> ~/.persona/bin/personad node --stdio`; no persistent Mac service required. Platform from configured os / arch; if absent, query machine `uname -s` / uname -m. Run version after installation; failure reports error, not installed. All four binaries also live in hub `bin/nodes/`; app Redeploy selects machine's platform (`docs/config.md`).
- Hub: `systemd --user` jarvisd on hub machine; HTTP listen default `:18660`, TLS via `public.tls_listen`, e.g. :18662. Expose only TLS publicly. IPv6 inbound, firewall, TUN return-path troubleshooting: [Network](network.md). Logs: journalctl --user -u jarvisd -f. Docker hub is not managed by `deploy.sh`: omit hub machine from machines, or deploy only computers with `deploy.sh` <machine id>. Upgrade pulls image; Redeploy uses image's node builds.
- Replacing node binary does not reconnect existing hub links automatically. `deploy.sh` <hub machine id> restarts hub, or Reconnect on app Machines page.
- Admin scope (`<hub address>/admin`, [admin.md](admin.md)): `deploy.sh --admin-access <loopback|lan|cidrs|any>`, plus `--admin-cidrs a,b` for cidrs. If omitted and never chosen on hub, ask in terminal; without terminal default lan with warning. Later use personad hub admin access to view / change, `personad hub admin reset-password` to reset admin password. First-run local setup code in hub logs, `admin: first-run setup code …`, or `personad hub admin setup-code` ([admin.md](admin.md), “First-run setup”).

### Lightweight hub and predeployment checks

A lightweight hub, e.g. 2 cores / 2 GB, can run hub + Postgres only, with Agents on execution computers. Before adding those, explicitly set `"machines": []` in `hub.json` to avoid default local node when omitted. Initially disable `inbox.agent.enabled` / `kb.enabled`; enable after data-source integration as needed. Deployment guidance only, not a load-tested minimum hardware requirement.

For Linux user services to survive SSH logout, administrator runs loginctl enable-linger <user>.
After Docker installation, verify `postgres:18-alpine` can pull; reachable OS repositories do not prove Docker Hub reachable. Failed pulls must not count as initialized DB / deployed hub, and never print `hub.json` tokens / database URLs.
If Docker Hub is unreachable in China, try official Docker image from AWS public registry, `public.ecr.aws/docker/library/postgres:18-alpine`; after successful pull retag `postgres:18-alpine` before database script.

Use HTTPS reverse proxy on a separate public domain; hub / database listen only on loopback. Nginx must forward WebSocket Upgrade / Connection, disable response buffering, extend read timeout, and avoid token-bearing query strings in access logs. For existing-domain subpaths (location /<prefix>/ strips prefix before hub), add proxy_set_header X-Forwarded-Prefix /<prefix> so web works ([web.md](web.md), “Subpaths”). Immediate requests after Nginx reload may hit old workers; wait for config activation before verifying cert, auth APIs, WSS 101. Free cloud-console certificates must be reapplied / installed before expiry; existing certbot timers do not renew them.

PowerShell piping shell scripts may append CR, causing remote `$'\r': command not found`. Upload UTF-8 files or base64-encode and decode remotely; do not rely on pipeline newline encoding.

## NAS (Docker) deployment and upgrades (compatibility example)

This records existing deployment operations. `~/.jarvis`, jarvisd, container / image names, and backup filenames retain old compatibility identities, not new-install defaults. Upgrade / rollback must use actual deployed paths / services; do not globally replace names and run commands.

Another option is `deploy/docker` compose ([docker.md](docker.md)); this section records two manually run containers on the current NAS.

Synology DSM cannot use `deploy.sh` (no `systemd --user`; Docker requires sudo); hub and Postgres both run in Docker.
`<nas>` is developer-machine SSH alias, `<LAN address>` NAS private IPv4, `<commit>` deployment commit. Run NAS commands as hub user with passwordless sudo.

DSM pitfalls: full path `sudo /usr/local/bin/docker`, absent from noninteractive SSH PATH; `/tmp` noexec, no binaries there. System `pg_dump` 11 cannot dump Postgres 18; back up inside container. Without SFTP, scp fails; upload via ssh … 'cat > file'.

### Build and upload (developer machine, repository)

```sh
git worktree add --detach /tmp/jarvis-build <commit>
(cd /tmp/jarvis-build/server && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -X main.version=$(git describe --always)-$(date +%Y%m%d%H%M)" -o /tmp/jarvisd-linux-amd64 ./cmd/personad)
git worktree remove /tmp/jarvis-build
ssh <nas> 'umask 077; mkdir -p ~/.jarvis/bin && cat > ~/.jarvis/bin/jarvisd.new && chmod 755 ~/.jarvis/bin/jarvisd.new' < /tmp/jarvisd-linux-amd64
```

### Initial deployment (NAS)

1. Postgres: `jarvis-postgres` container, `jarvis-pgdata` volume, published only at 127.0.0.1:18661. Creation password is only for initialization; generate a separate random actual password, set with ALTER ROLE, write only to hub.json.

   ```sh
   D="sudo /usr/local/bin/docker"
   $D volume create jarvis-pgdata
   $D run -d --name jarvis-postgres --restart unless-stopped -p 127.0.0.1:18661:5432 \
     -v jarvis-pgdata:/var/lib/postgresql -e POSTGRES_USER=jarvis -e POSTGRES_DB=jarvis \
     -e POSTGRES_PASSWORD="$(python3 -c 'import secrets;print(secrets.token_hex(24))')" \
     --log-driver json-file --log-opt max-size=20m --log-opt max-file=3 postgres:18-alpine
   until $D exec jarvis-postgres pg_isready -h 127.0.0.1 -U jarvis -d jarvis >/dev/null; do sleep 1; done
   export PW=$(python3 -c 'import secrets;print(secrets.token_hex(24))')
   echo "ALTER ROLE jarvis WITH PASSWORD '$PW';" | $D exec -i jarvis-postgres psql -U jarvis -d jarvis -q
   ```

2. `~/.jarvis/hub.json` (0600; machines initially empty, later fill per `docs/config.md`):

   ```sh
   python3 - <<'PY'
   import json, os, secrets
   cfg = {
       "listen": ":18660",
       "token": secrets.token_hex(24),
       "public_url": "http://<LAN address>:18660",
       "hook_timeout_minutes": 30,
       "database_url": "postgres://jarvis:%s@127.0.0.1:18661/jarvis?sslmode=disable" % os.environ["PW"],
       "machines": [],
       "public": {"tls_listen": ":18662", "advertise_ipv6": True, "advertise_lan": True},
       "security": {"trusted_cidrs": [], "allow_public_plaintext": False},
   }
   fd = os.open(os.path.expanduser("~/.jarvis/hub.json"), os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
   with open(fd, "w") as f:
       json.dump(cfg, f, indent=2)
   PY
   unset PW
   ```

3. Runtime image `jarvisd-runtime:alpine3.22`: no bundled jarvisd (mount binary from `~/.jarvis/bin`), includes SSH client for computers and `netcat-openbsd` for SSH ProxyCommand via SOCKS (step 7; busybox nc lacks proxy support). `~/.jarvis/docker/Dockerfile`:

   ```dockerfile
   FROM alpine:3.22
   ARG UID
   ARG GID=100
   ARG USER_NAME
   ARG HOME_DIR
   RUN apk add --no-cache openssh-client ca-certificates tzdata netcat-openbsd \
    && adduser -D -H -u "$UID" -G users -h "$HOME_DIR" -s /bin/sh "$USER_NAME" \
    && mkdir -p "$HOME_DIR" && chown "$UID:$GID" "$HOME_DIR"
   ENV HOME=$HOME_DIR
   WORKDIR $HOME_DIR
   USER $UID:$GID
   ENTRYPOINT ["/opt/jarvis/bin/jarvisd"]
   CMD ["hub"]
   ```

   ```sh
   cd ~/.jarvis/docker && $D build --network host --build-arg UID=$(id -u) --build-arg GID=$(id -g) \
     --build-arg USER_NAME=$(id -un) --build-arg HOME_DIR=$HOME -t jarvisd-runtime:alpine3.22 .
   ```

4. Jarvisd container (save as `~/.jarvis/docker/run-jarvisd.sh` for recreation): host network, `--init`, current user's UID. Read/write `~/.jarvis`, read-only `~/.jarvis/bin` → `/opt/jarvis/bin`, mounted `~/.ssh`; container HOME matches NAS.

   ```sh
   mv ~/.jarvis/bin/jarvisd.new ~/.jarvis/bin/jarvisd
   REAL_HOME=$(readlink -f "$HOME")
   $D run -d --name jarvisd --restart unless-stopped --network host --init \
     --user "$(id -u):$(id -g)" -e TZ="$(readlink /etc/localtime | sed 's#.*/zoneinfo/##')" \
     --log-driver json-file --log-opt max-size=20m --log-opt max-file=5 \
     -v "$REAL_HOME/.jarvis:$HOME/.jarvis" -v "$REAL_HOME/.jarvis/bin:/opt/jarvis/bin:ro" \
     -v "$REAL_HOME/.ssh:$HOME/.ssh" \
     jarvisd-runtime:alpine3.22 hub
   ```

5. Checks:

   ```sh
   $D exec jarvis-postgres psql -U jarvis -d jarvis -tAc "select max(version), count(*) from schema_migrations"
   curl -s http://127.0.0.1:18660/api/v1/health    # ok, version, fp
   openssl s_client -connect 127.0.0.1:18662 </dev/null 2>/dev/null | openssl x509 -pubkey -noout |
     openssl pkey -pubin -outform der | openssl dgst -sha256 -binary | openssl base64 -A | tr '+/' '-_' | tr -d '='   # Must equal fp
   (umask 077; $D exec jarvisd /opt/jarvis/bin/jarvisd hub pair > ~/.jarvis/pair-$(date +%Y%m%d-%H%M%S).txt)  # Contains token, valid 10 minutes
   ```

6. Public direct connection: DSM firewall permits `18662/tcp`, not 18660; router permits inbound IPv6; [Network](network.md).

7. Proxy-only computers, e.g. an internal development computer reachable through NAS SOCKS5: keep `kind: ssh`, proxy in NAS `~/.ssh/config` mounted into container, `hub.json` alias only. Address, account, proxy port are private configuration, only on NAS:

   ```
   Host <alias>
     HostName <computer LAN address>
     User <account>
     HostKeyAlias <alias>
     IdentityFile ~/.ssh/<hub-specific key>
     IdentitiesOnly yes
     ProxyCommand nc -X 5 -x 127.0.0.1:<SOCKS port> %h %p
   ```

   - This nc is container `netcat-openbsd`; NAS nc may not support -X. Test inside container too, e.g. temporary same-image `--entrypoint sh`, running hub's `ssh -T -o BatchMode=yes … <alias> '~/.jarvis/bin/jarvisd node --stdio'`.
   - Hub uses `BatchMode=yes`; prepopulate ~/.ssh/known_hosts. Prefer public key from an already trusted connection to that computer, indexed by `HostKeyAlias`; do not blindly accept first connection.
   - Restrict key on computer `authorized_keys` to node only: `restrict,command="~/.jarvis/bin/jarvisd node --stdio" ssh-ed25519 …`.
   - Dockerfile changes require image and container rebuild. Timestamp-tag old image for rollback, build, then `$D stop jarvisd && $D rm jarvisd && ~/.jarvis/docker/run-jarvisd.sh`. Docker restart still uses old image.

### Upgrade

Follow Build and Upload to transfer new binary as `~/.jarvis/bin/jarvisd.new`, then on NAS:

```sh
~/.jarvis/docker/upgrade.sh ~/.jarvis/bin/jarvisd.new
```

Script steps:

1. Verify new binary runs on NAS with version.
2. Stop container.
3. Copy current binary to ~/.jarvis/bin/jarvisd.bak-<time>.
4. Container `pg_dump -Fc` → ~/.jarvis/backups/jarvis-<time>.dump, 0600. Dump after stopping service to match old binary's final state.
5. Replace binary with mv; start container.
6. Wait for `/api/v1/health`; verify new version and unchanged TLS public-key fingerprint.
7. Print latest three migrations and logs since start, hiding known Lark gateway errors.
8. Print rollback command. Failed health / wrong version / changed fingerprint returns nonzero.

### Rollback

```sh
~/.jarvis/docker/rollback.sh --check ~/.jarvis/bin/jarvisd.bak-<time> ~/.jarvis/backups/jarvis-<time>.dump   # Verify only, leave hub untouched
~/.jarvis/docker/rollback.sh ~/.jarvis/bin/jarvisd.bak-<time> ~/.jarvis/backups/jarvis-<time>.dump
sudo /usr/local/bin/docker exec jarvis-postgres dropdb -U jarvis jarvis_before_rollback_<time>   # After confirming rollback succeeded
```

Script steps:

1. Restore dump into a new database; restoration failure changes nothing.
2. Stop container.
3. Rename original database jarvis_before_rollback_<time>, retaining writes made after dump only there.
4. Rename restored database jarvis.
5. Restore old binary, start container, wait for health.

### Logs

```sh
sudo /usr/local/bin/docker logs -f jarvisd
sudo /usr/local/bin/docker restart jarvisd    # After hub.json changes
```

Old hub versions log repeated Lark gateway token-read failures when `hub.json` lacks lark; operation unaffected. Lark gateway integration now belongs to its extension: enable agent API on gateway, fill extension address / token per extension instructions. Hub container has no Docker CLI and cannot read other containers' environment.

## Computers hub cannot reach (jarvis-link)

For laptops with changing networks, kind link in `hub.json`: hub does not SSH; jarvis-link (Mac menu-bar Persona Link) connects outbound to TLS port, then streams to persistent node, which must be installed (next section). Pair with `personad hub pair --machine <id>`; NAS: sudo /usr/local/bin/docker exec jarvisd /opt/jarvis/bin/jarvisd hub pair --machine <id>. Details: [link.md](link.md).

## Hub connects inbound (kind service)

For fixed-address servers / workstations hub can reach, kind service: computer connector in passive `jarvis-link pair --listen` mode listens on TLS. Hub dials with paired fingerprint pinning and streams to persistent node, no SSH, optionally `socks5://` proxy. Paste printed link into admin Computers › Add Computer › Hub Connects; effective immediately. Credentials encrypted in Postgres. [mesh.md](mesh.md) (“kind service”), [link.md](link.md) (“Passive mode”).
Existing SSH computers remain usable, but are no longer recommended or addable in admin; migration: [mesh.md](mesh.md) (“SSH / tailcat: compatibility”).

## Linked hubs

Prefer one hub and node services elsewhere. If several hubs are needed, pair into a group (`personad hub pair --peer` / personad hub peer add, or admin Advanced › Linked Hubs). Exactly one **primary** serves all apps / computers; **members** forward app requests and hand their computers to primary. personad hub peer list shows primary; peer center changes it. Each computer appears once via best route. Links use TLS only. Design, protocol, verification: [mesh.md](mesh.md).

## Restart without interrupting clients ([hub-restart.md](hub-restart.md))

- `jarvisd.socket` owns listener (socket activation, `hub-systemd.sh`); `systemctl --user restart jarvisd` leaves port open, queues connections for new process. Without `LISTEN_FDS` (Docker, launchd, manual), hub listens itself. Rollback: `PERSONA_HUB_SOCKET=0 scripts/deploy/hub-systemd.sh` **before** restoring old binary without `LISTEN_FDS` support, otherwise port conflict. Full stop: `systemctl --user stop jarvisd.socket jarvisd`.
- SIGTERM planned shutdown sends `hub_restarting`, hands machines / sessions as the managers reported them to next hub (`hub_settings` `hub.devices_warm`), suppresses shutdown-induced offline broadcasts, then hands off event log (settings `hub.event_log`) so next hub continues sequence.
- Persistent node on hub machine (next section) keeps assistant Claude outside hub cgroup, surviving restart. Deploy with `node-launchd.sh restart-idle`; replace binary only when node idle.

## Persistent node (Mac, `scripts/deploy/node-launchd.sh`)

Hub launches node via SSH (`personad node --stdio`), which launches assistant Claude. SSH cannot read macOS **login keychain**; Claude keys stored there through `apiKeyHelper` `security find-generic-password` give assistant 401. Herdr Agents do not, since herdr runs in login session.

- Run `scripts/deploy/node-launchd.sh install` on Mac: installs user LaunchAgent `com.jarvis.node`, persistent `personad node --attach` after login, listening ~/.persona/node-attach.sock.
- Hub still SSH-launches `personad node --stdio`; detecting persistent listener, it forwards SSH I/O only (Relay in `internal/node/attach.go`). Persistent node handles connection; its assistant Claude / key helper inherit login session and keychain access. Hub unchanged, cannot distinguish.
- Without persistent node (not installed, exited, stale socket), stdio handles itself as before.
- One connection at a time: new hub reconnect replaces old connection's node; waiting hooks ask new one, as with replaced SSH node. Only one persistent node.
- `deploy.sh` replaces binary then `node-launchd.sh restart`; SSH can control login-session launchd. Hub reconnect needed for new node (restart hub / app Reconnect).
- Agent runtime `~/.persona/runtime.sock` belongs to persistent process, not individual hub connection (`node/runtimehost.go`). Opens at persistent startup; runtimes such as DSH plugin need not wait for hub. Hub connection replacement does not disconnect runtimes; new connection immediately lists sessions (`docs/protocol.md`, “Runtime ↔ node”).
- `scripts/deploy/node-launchd.sh uninstall` returns to SSH-launched node. Log ~/.persona/node-attach.log.

## Tailcat hole punching (`internal/tunnel`, `devices/tailcat.go`)

Computers unreachable by hub SSH (NAT, other LAN, no public IP) use [tailcat](https://github.com/tailscale/tailcat): Tailscale data plane (WireGuard + DERP relay + hole punching), no control plane, account, root, TUN, or exposed ports; all in personad userspace netstack.

- **Fallback order**: machines with `ssh_host` and `tailcat_addr` try SSH first (`ConnectTimeout=15`), then immediately tailcat. For 5 minutes after SSH failure, reconnect tries tailcat first, avoiding SSH timeout. While on tailcat, test `ssh <host> true` every 5 minutes; restored SSH disconnects / reconnects to SSH, except while questions / plans / permissions await phone because reconnect cancels them. If return fails immediately (SSH login works, node silent, e.g. `node.info` not received within 40 seconds on slow link), next probe interval doubles up to 32×, reset after 10 stable SSH minutes, avoiding five-minute disconnect loops. `kind: "tailcat"` uses tailcat only. Machine transport shows current route; `tailcat_path` direct / relay, queried each minute, also encouraging direct-path rediscovery.
- **Disconnection detection**: userspace TCP lacks keepalive (SSH has `ServerAliveInterval`), so sleeping / disconnected peers do not close automatically. Hub pings node each minute, reconnects after two missed replies; old unknown method counts as response. Any traffic during minute (read data, or each 32 KB chunk of large writes) counts alive, suppressing ping: large pages on slow relays take long and queued ping could misdiagnose. Ping waits in separate goroutine; blocked dead-connection write cannot delay decision. Closing underlying connection on disconnect causes blocked writes to fail.
- **Node**: only persistent node (node-launchd.sh install: Mac launchd, Linux systemd user service) opens tailcat. With `~/.persona/tailcat.json`, listens on tailcat address at 18662 (node protocol, same one-connection/new-replaces-old rule as SSH) and 18663 (binary upload). Network / DERP startup failures retry with background backoff; SSH forwarding continues.
- **Pairing**: hub `personad hub tailcat-key` prints public key, first creating ~/.persona/tailcat-hub.json. Computer personad node tailcat `init --hub` <public key> generates `~/.persona/tailcat.json` (node private key, preshared key, chosen DERP region, allowed hub keys) and prints address for hub.json tailcat_addr. Restart persistent node and hub. Rerun init adds hubs without address change; node tailcat addr reprints.
- **Security**: node answers only listed hub keys. Tailcat empty allowlist permits everyone, so files with no hub key are refused. Other keys cannot handshake. Only ports 18662–18663 allowed. Address includes preshared key: secret like hub token, only in hub.json, never logs. Both key files 0600; broader permissions repaired.
- **Revocation / rotation**: allowlist read only at node startup; tailcat does not expel already connected peers. Remove hub from computer `tailcat.json` hubs, `scripts/deploy/node-launchd.sh restart` the persistent node. Leaked address: delete `tailcat.json`, reinit to rotate preshared / node private key, update hub address, restart hub. Rotate hub key: delete `tailcat-hub.json`, restart hub, obtain new `hub tailcat-key`, `init --hub` on every computer, remove old keys from hubs, restart persistent nodes.
- **Stable address**: address stores DERP region ID, not embedded relay server. Choose nearest region once during init and persist; choosing every startup would change address.
- **Deployment**: Redeploy over tailcat sends binary to 18663: one `{"size","sha256"}` line + gzip. Node verifies size / SHA-256 and local version execution before replacing, replies ok, exits for launchd / systemd restart. One upload at a time; second returns Uploading immediately. Abort after 60 seconds without data; hub restart mid-upload cannot block later deployment. Initial `deploy.sh` still requires SSH or manual binary transfer.
- **Cost**: binary approximately 19 → 36 MB, gzip 8 → 15 MB; cold start ~7 seconds fetching DERP map / selecting region, first dial ~6.5 seconds. Public relays have no SLA and limit relay bandwidth; custom derper via `--derp-map-url` / `tailcat.derp_map_url` below. Tailcat API stability not guaranteed; run integration tests on dependency upgrades.
- **Self-hosted relay (derper)**: WireGuard already encrypts end-to-end, but public relay sees communicating keys, peer IPs, volume. When computers / hub share LAN, run LAN-bound derper on hub machine so discovery / fallback never leave LAN:
  1. Build: GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go install tailscale.com/cmd/derper@<version>; match `tailscale.com` version in `server/go.mod` / tailcat.
  2. IP-only certificate: self-signed, pinned by map `CertName` `sha256-raw:<certificate DER sha256>`, no CA. Generate long-lived openssl cert with subjectAltName=IP:<LAN IP>, store <certdir>/<LAN IP>.crt / .key. Do not use derper's automatically generated missing-cert certificate (`docs/lessons/server.md`).
  3. Systemd user service: `derper -a <LAN IP>:<DERP port> -certmode manual -hostname <LAN IP> -certdir <certdir> -c <private key file> -http-port -1 -stun-port <STUN port> -home blank`. IP in -a also binds STUN there; always supply -c.
  4. DERP map not served by derper; tailcat fetches HTTP GET. Separate static server, e.g. `python3 -I -m http.server <port> --bind <LAN IP> --directory <directory>`, serves `derpmap.json`: one custom region ID 900–999, one node, `HostName` / IPv4 LAN IP, `IPv6: "none"`, `CertName` / DERPPort / STUNPort. Before switching, fetch map on every computer and compare `openssl s_client` certificate SHA-256 to CertName.
  5. Switch hub `tailcat.derp_map_url`; computer node tailcat `init --hub` <key> `--derp-map-url` <map URL>. Keys unchanged but new-map region reselection changes address; update each `tailcat_addr`, restart node / hub. Map certificate / port changes also require both restart.
  6. Verify machine `tailcat_path` still direct; node log DERP region 9xx; personad peers connect only to derper port, not public relays (`ss -tnp` / `lsof -i`).
- **Hub ↔ node only**: phone / web still need reachable hub public_url.
- Integration (internet required): `JARVIS_TEST_TAILCAT=1 go test ./internal/tunnel -run EndToEnd`, real relay dialing / authentication / upload, plus commands in `internal/devices/tailcat_e2e_test.go` header (two real personad versions: SSH fails → tailcat online → deploy new version and reconnect). `JARVIS_TAILCAT_DEBUG=1` enables otherwise muted Tailscale logs, several lines per second during hole punching.

## Database (Postgres)

Hub Docker container `jarvis-postgres`, `postgres:18-alpine`, listening only `127.0.0.1:18661`, `jarvis-pgdata` volume, --restart unless-stopped. `scripts/deploy/db.sh` idempotently creates / starts it, initially generates password into `hub.json` `database_url`; `deploy.sh` <hub machine id> runs it first each time. Hub startup applies outstanding migrations, `schema_migrations` records, advisory lock prevents concurrency.

Without Docker, use system Postgres (Ubuntu postgresql, version 16 tested, localhost only). Create jarvis user and jarvis / `jarvis_test` databases, both owned by jarvis; test database for test-server.sh. Put `postgres://jarvis:<password>@127.0.0.1:5432/jarvis?sslmode=disable` in 0600 hub.json. Without Docker, `db.sh` only checks configured address with `pg_isready`; `deploy.sh` / `test-server.sh` unchanged. Replace docker exec … psql below with `psql "<database_url>"`.

| Table | Content |
| --- | --- |
| `live_activities` | Per-device activity / push-to-start tokens and start time, `docs/live-activity.md` |
| devices | APNs devices; 410 / BadDeviceToken deletes only if current token still equals failed one, not a newly registered token. JSONB prefs for device notifications (app Settings › Notifications), not overwritten on registration |
| `phone_tools` | iPhone's last declared phone_* tools, visible to Claude even offline |
| `node_sessions` | Persona-started sessions per machine: pane, launcher, directory. Node uses hub `state.load` / `state.put`, Mac no direct DB. `state.load` reads last 60 days via `node_sessions_started` index (machine_id, started_at), not entire machine history |
| `mail_accounts` | Nonsensitive JSONB + AES-GCM ciphertext; `~/.persona/secret.key` excluded from DB |
| interactions | Questions / plans / permissions / terminal interactions and outcomes resolved / dismissed / expired / cancelled. Hub restart cancels leftover pending through partial `interactions_pending` index, no history scan. Completed rows currently retained indefinitely; retention #2615 |
| `hub_settings` | The hub's own settings: persistent `hub_id`, the user's locale, look, mutes and pins, the managers' default and machine owners, mesh records, update settings, the event log and machines handed to the next process |
| settings | A manager's settings (the assistant, inbox rules, models and launchers, extensions, the computers' engines and warm start) |
| `pair_tokens` | Temporary token hashes, expiration / redemption records, surviving restart |

Connect: `ssh <hub machine> 'docker exec -it jarvis-postgres psql -U jarvis -d jarvis'`.
Tests: `./scripts/test/test-server.sh` syncs to hub and runs all; local without Docker skips DB tests. `JARVIS_TEST_DATABASE_URL` pointing `jarvis_test` is only a starting point: each test binary creates adjacent private database and deletes afterward (`server/internal/db/dbtest`), isolating concurrent runs. Jarvis role needs database creation (container superuser; system Postgres grant CREATEDB). Without privilege, fallback queues on jarvis_test.

Legacy `phone_tools.json`, `devices.json`, `mail_accounts.json`, `sessions.json` auto-import on first start, renamed *.migrated.

### Migrations

The migrations are two sets, one per side (`server/internal/db/migrations/`):

- `hub/`: the hub's own tables: devices and their tokens, pairing, phone tools, Live Activities, device sessions, client operations, lock tests, peers and the mesh, connector credentials, the admin page, bus identities and the managers' install codes, machine trust.
- `manager/`: a manager's: interactions, mail, the computers' sessions and metrics, session views and work, the inbox, the knowledge base, todos, task plans and the assistant's feed and views, extensions and their markets, secrets and metrics, the workspace board.

`db.Open(ctx, url, sets)` applies the pending migrations of the sets it is given: the hub opens its database with `db.HubSet`, a manager with `db.ManagerSet` (tests that use both sides' tables: `db.AllSets`). While the default manager shares the hub's database, both sets run there, each when its side starts. Non-default managers have databases of their own (`docs/managers.md`) and get only the manager set.

`schema_migrations` has a row per migration a part applied, with `part` `hub` or `manager`. Rows from before the split have `part` NULL and stand for both parts, so a database migrated by an older build runs nothing again, whichever side opens it first, and a build from before the split still reads and adds records there. Three early migrations are in both sets and frozen that way: `0001_init.sql` made tables for both sides and is split into each side's part; `0071_sent_messages.sql` and `0241_sent_echo.sql` make `sent_messages`, which each side keeps in its own database (the hub's sends from the apps, the manager's from the inbox). On a database both sides share, that table is one.

Settings are each side's own too: the hub's in `hub_settings` (`0421_hub_settings.sql`, which on a database shared with the default manager copies the hub's keys out of `settings` once and leaves the rows there for an older build), a manager's in `settings`. `db.DB.Setting` and the rest use the table of the side the database was opened for (`db.HubSet` alone: `hub_settings`). Neither side reads the other's keys; what one needs of the other comes over the bus: the mutes travel with `fleet.mutes`, the computers waiting to be confirmed with the devices handover, and the hub hands its next process the machines as the managers reported them (`hub.devices_warm`) instead of reading the manager's warm start. The inbox's rules (`inbox.rules`, `inbox.agent_rules`) are the manager's settings (listed among its `jarvis_settings`). `TestSettingsKeysAreOneSidesOwn` (`internal/hub`) fails when the hub and a manager package use the same literal key.

Adding a migration:

1. Decide whose table it is: the side whose store reads and writes it. A table both sides need is a sign that one side should ask the other over the bus instead (as the manager learns the computers waiting to be confirmed from the hub's handover, `devicewire.Config.Pending`, rather than reading `machine_trust`).
2. Put the file in exactly that set's directory, named `NNNN_what.sql` with a number neither set has: look in both `hub/` and `manager/`, on master and in every worktree (`docs/lessons/workflow.md`). Two lines once took 421 at the same time (`hub/0421_hub_settings.sql`, `manager/0421_machine_usage.sql`); that pair is frozen in the guard, harmless since each set records its own part. Write it idempotently (`IF NOT EXISTS`) where you can.
3. Guards: `TestMigrationSets` (`internal/db`) fails for a file in both sets or at the top level; `TestStoresTouchTheirOwnTables` (`internal/hub`) fails when a store interface of the hub reaches a table outside the hub set, or one of the managers' (assistant, devices, extmgr, extension, mail, agents) a table outside the manager set.

## Configuration (hub `~/.persona/`, 0600, never committed)

| File | Content |
| --- | --- |
| `hub.json` | listen, token, `public_url`, public (TLS / candidate addresses), security (trust / auth rate limits, [network.md](network.md#hubjson-网络配置)), name display name, admin access scope ([admin.md](admin.md)), `hook_timeout_minutes`, `database_url`, machines; `google.client_id` (Gmail iOS OAuth client), `google.proxy` (Gmail HTTP proxy, e.g. http://127.0.0.1:<port>, `docs/mail.md`); `github.proxy` (all GitHub connector requests, same format, `docs/config.md`); notify (notification timing, `docs/config.md`) |
| `tls/hub.key`, `tls/hub.crt` | Automatically managed ECDSA P-256 identity key / self-signed cert, directory 0700, files 0600, private key outside DB. Rotation requires device re-pairing |
| `secret.key` | 32-byte mail-credential encryption key |
| `apns.json`, `apns_key.p8` | Push config / key. Present → direct APNs; absent → per-phone registered relay (`docs/push-relay.md`); no relay → no push |

Each computer ~/.persona also contains node-generated hooks (`claude-hooks/` / `assistant-hooks/` plugins, `claude-settings.json` / `assistant-settings.json` for older Claude), `mcp.json`, `node.sock`, uploads/.

Pairing: hub `~/.persona/bin/personad hub pair` calls running hub via loopback HTTP, prints temporary link, QR, expiry, TLS public-key fingerprint if enabled. Listen must accept IPv4 loopback. Token valid 10 minutes, single device-token redemption, at most 2-minute post-redemption grace; protocol policies. `--legacy` prints main token / old QR offline, not for public networks. New QR parameters: [Protocol](protocol.md#临时配对).

## Push policy (`hub/push.go`, `hub/notify.go`)

- Needs you (question / plan / permission / terminal): immediate, `time-sensitive`, `category: INTERACTION_<KIND>` with app-registered buttons. Hook-blocked dialogs do not appear in computer TUI and cannot be answered there, so no delay.
- Completed: follows Ovis (`github.com/devswha/ovis` `server/push.ts`, MIT); push is for users away from the computer:
  - Working → done / idle waits `notify.done_delay_seconds`, 60 seconds, before deciding. Resumed work remains same turn with original start time; idle (seen in herdr) or app `seen_at` suppresses push.
  - Running background commands / sub-Agents defer push until reports are processed and that Claude turn ends; no standalone `WokeByTask` turn push.
  - Duration = first working to final completion. Per-device `prefs.done` off / long / always; long receives turns at least `long_turn_seconds`; `prefs.input` off suppresses Needs you.
  - Hidden Agents and assistant turns already pushed by `notify_user` are skipped. Hub restart measures ongoing turns from first post-restart observation.
- Extension notifications (`POST /api/v1/notify`, `hub/notify_api.go`): extension picks active / `time-sensitive`, filtered by device input and hidden status. Decision / rate limits belong to extension; `docs/extensions.md` (“Sending notifications”).

<a id="关停hubshutdowngo"></a>

## Shutdown (`hub/shutdown.go`)

This investigation confirmed exit-path defects, not the NAS database-wait cause. BaseContext propagated cancellation to WebSockets, but HTTP Shutdown does not wait for hijacked connections. Device-session cleanup and WebSocket handlers had no wait relationship. Final pgxpool.Close waited indefinitely for borrowed connections. Idle WebSockets do not continuously borrow DB connections; online phones / timeout logs alone do not prove row locks or connection leaks.

Now close plaintext / TLS listeners first, send registered WebSockets 1001 going away. Allow up to 500 ms handshake, then cancel contexts, force-close nonresponders, wait for handlers / write loops. Connections registering device sessions are tracked too; no new wait objects after accepting stops. Then finish HTTP, device sessions, lock-screen tests, wait node connections, close pool. Device disconnects due to hub shutdown record hub_stopped, not phone-disconnection count.

`shutdown_timeout_seconds` defines total budget, default 8 seconds for absent / nonpositive. Each phase 2 seconds bounded by total; handshake 500 ms. Exit policy, not performance estimate: total allows four full phase budgets without unbounded accumulation. NAS / real phone networks unmeasured. External terminator grace must cover configured total.

Phase timeout logs phase / error and continues cleanup. Even noncancellable cleanup cannot prevent Run returning with error at total budget; command entry exits process. Pool cannot force-reclaim borrowed connections; after CloseContext timeout, background close may wait but no longer blocks command exit.

SSH side has no pool. Hub CommandContext already terminates local node / SSH subprocess on cancellation; this change adds process / pipe wait bounds, without node protocol / remote changes. App messages / capability negotiation unchanged; existing iOS needs no change.

Tests use dedicated database, temporary config, local fake Lark service, real Run. Client stays connected after hello then hub context cancels. Assert shutdown return, device stop record, 1001 close, no surviving WebSocket handlers / write loops. Separate nonresponsive-handshake test. Pool test holds borrowed connection, verifies CloseContext timeout, then returns it and observes close completion. Total-budget test uses noncancellable cleanup.

Local measurement 2026-10-31, milliseconds from context cancellation to Run return. Five samples per scenario, fewer than 10—not a performance baseline. Nearest-rank percentiles.

| Client | Samples | min | p50 | p90 | max | mean |
| --- | --- | --- | --- | --- | --- | --- |
| Reads and responds to close | 5 | 3.063 | 3.868 | 7.107 | 7.107 | 4.441 |
| Does not read or respond | 5 | 506.592 | 508.780 | 510.670 | 510.670 | 508.478 |

Validation passed: server build, relevant hub / db tests, full hub-package tests. Sandbox Go could not obtain worktree VCS status, so GOFLAGS=-buildvcs=false disabled only build-version embedding. First test accidentally triggered existing read-only Lark-container probing; subsequently isolated with local fake service. Container unchanged.

## Traffic pushed to the app (2026-11-02, battery investigation)

Foreground app keeps WebSocket to hub, receiving change broadcasts. A 19-minute real-hub recording (`docs/testing.md`, “Push replay”) showed 105 messages / 2.6 MB per minute; sessions full-machine lists accounted for 37 messages / 2.6 MB, almost all bytes. Continuous cellular reception prevents radio idle.

- New apps receive `sessions_delta` (`hub/sessions_push.go`, `docs/protocol.md`, “Session deltas”): initial snapshot excluding Codex history, then changed sessions only. Replay: session push 2605 → 79 KB/min, -97%; all messages 2616 → 90 KB; initial connection 602 → 227 KB.
- Stop identical full-list broadcasts for old apps (`broadcastSessions`). Questions appearing / answered, viewing sessions, Codex cards often rebroadcast unchanged machine lists. `sessionsSent` stores last emitted encoded bytes per session; `sameSessions` skips identical. Recording / enqueue share lock, so remembered list equals last app received. Replay 37 → 30 messages/min but only 6% fewer bytes; remaining changes are real rev / `updated_at` / last-text changes needing delta protocol.
- Disconnect lagging connections: full send queue, 512 messages behind, previously dropped one message; now disconnect like `sendJSON` and reload full snapshot after reconnect. Once duplicates suppressed, dropped message cannot rely on a later identical list to repair it.
- Live Activity cadence: `docs/live-activity.md` (“Push cadence”).

## Self-update (`hub/selfupdate.go`, `docs/hub-update.md`)

`channel=release` hubs check release site's `latest.json` every 6 hours. New version waits until assistant / Agents idle, replaces binary / restarts, rolls back if unable to start.
- Files under `~/.persona/update/`: `<build>/` downloads, node binaries in `nodes/` later copied to `~/.persona/bin/nodes/`; `pending.json` ongoing update, deleted after confirmation; `rolledback.json` latest rollback, recorded by old version, marked bad then deleted; `watch.log` watchdog output, `hub.log` rollback hub output without service manager.
- Previous binary ~/.persona/bin/jarvisd.prev. Manual restore: `cp ~/.persona/bin/jarvisd.prev ~/.persona/bin/personad && systemctl --user restart jarvisd`; launchd uses launchctl kickstart -k. Disable auto-update in app afterward to avoid reinstall (rolled-back builds already excluded automatically).
- Disable in app Settings or `PUT /api/v1/hub/update/settings {"auto": false}`.
- Notifications: confirmed auto-update pushes Hub Updated to X; recorded rollback pushes Hub Update Failed + reason (`updater.notify`, all devices, thread `hub_update`). Manual updates no separate push.
- Direct `deploy.sh` / main-session scripts use compile-time build number newer than current public release, so no downgrade. Direct deployment during update is not undone by watchdog.

## Operations

- Offline machine: app shows last SSH error line. NAT computers need reverse tunnel first (`docs/config.md`).
- Online but every task immediately `rpc: connection closed`: check launcher / assistant APIs both returning 502. RPC closure does not close SSH stdout automatically. Connection loop must also await `peer.Done()`, cancel old SSH subprocess and reconnect, or stale closed peer / online status persist. `TestMachineReconnectReplacesClosedPeer` uses real subprocess.
- Manual API: LAN `curl -H "Authorization: Bearer $TOKEN" http://<hub address>:18660/api/v1/machines`; public uses device token, HTTPS, key pinning; [Network](network.md#命令行验证).
- Public auth failures including WS handshake log auth source=… path=… status=… code=… failures=… suppressed=…. Mask sources IPv4 /24, IPv6 /64; fixed API paths only, no tokens. Rate limit 429 / `Retry-After`; `security.auth_log_interval_seconds` controls log throttling.
- Network issues: read-only personad hub doctor on hub machine, [Network troubleshooting](network.md#网络排查).
- Admin failed login: admin: wrong password from <masked source>. Password set / change, scope changes, computer add / remove, device revocation, pairing-code generation each `admin: …` log, no passwords / codes. Admin 401 / 404 uses request IDs too.
- Request IDs: `X-Request-Id` on every response, reusing app `X-Client-Request-Id` if present. Hub logs only ≥400 failures / >2-second slow requests, `[<id>] method path -> status (duration)`, no query / body / token. App 5xx adds “(Request <id>)”; find corresponding `journalctl --user -u jarvisd` line.
- Compression (#2149, `gzip.go`): `Accept-Encoding: gzip` gets gzip for >1 KB JSON / JS / CSS / HTML / SVG / plain text (Lark chat 2.9 MB → 70 KB). App, web, Go clients decompress automatically. WebSocket upgrade, persistent connections, Range, HEAD, event-stream, images / binaries, already encoded / cookie-setting responses unchanged. Admin `/admin` and `/api/v1/admin/` uncompressed to avoid cookie / CSRF size leakage (BREACH). Layer outside `logRequests`; handlers still receive logging statusWriter. Debug `curl --compressed` or `-H 'Accept-Encoding: gzip'` and inspect Content-Encoding.
- Unreachable herdr socket / restart: node logs `herdr: … failed` at most once per 30 seconds, with failure count, then reachable again; SSH stderr enters hub log.
