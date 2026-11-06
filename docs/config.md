# Configuration (so others can use this repo too)

The repo hard-codes nobody's machines, accounts, domains, or preferences. Everything personal lives in two configs, and code and scripts only read the configs:

| Config | Location | Read by | Covers |
| --- | --- | --- | --- |
| Build / deploy config | `~/.config/persona/config.json` on the dev machine (point elsewhere with `PERSONA_CONFIG`; the old `JARVIS_CONFIG` still works), template `config.example.json` at the repo root | `scripts/*` (via `scripts/persona_config.py`), XcodeGen (via the generated `apple/Config.xcconfig`) | The App's bundle id and team, the App Store Connect account, TestFlight testers, which machine / address the hub is on, machines to deploy to, simulators |
| hub runtime config | `~/.persona/hub.json` on the hub machine (0600, contains the token and database password; generated automatically the first time `personad hub` runs) | `personad hub` | Listen address, public address, which machines to connect, whose assistant it is and what it should know, the AI gateway |
| Manager config | `~/.persona/managers/assistant/manager.json` for the built-in manager, `~/.persona/managers/<id>/manager.json` for one made in the app (0600) | the manager (`personad assistant`, `personad manager run`) | Connectors, extensions written by hand, bots, mail sign-ins and the Gmail proxy, Lark and lark-cli, usage sources ("manager.json" below) |

The configs live outside the repo: releases run in a clean worktree (`docs/release.md`), where anything outside the repo would be missing; it also avoids committing them by accident.
`apple/Config.xcconfig` and `CLAUDE.local.md` are in `.gitignore`.

<a id="命名与兼容"></a>

## Naming and compatibility

The product name is **Persona** (formerly Jarvis), and the repo is `nwverse/persona`. The new default names in this document apply to the #277 migration release; updating the docs alone doesn't change installed binaries or scripts, and older versions keep using the old entry points.

| Purpose | New name (preferred) | Compatible name |
| --- | --- | --- |
| Server binary | `personad` | `jarvisd`; existing subcommands mean the same |
| Default runtime directory | `~/.persona` | `~/.jarvis`; upgrades keep the existing config, keys, knowledge base, and pairing identity |
| Default build / deploy config | `~/.config/persona/config.json` | `~/.config/jarvis/config.json` |
| Config file override | `PERSONA_CONFIG` | `JARVIS_CONFIG` |
| Migrated environment variables | The corresponding `PERSONA_*` | `JARVIS_*`; when both are set the new name wins, including an explicit empty value |
| Config script | `scripts/persona_config.py` | `scripts/jarvis_config.py`, a symlink to the same implementation |
| Service runtime root resolver | `scripts/persona_identity.py --service hub\|node\|link` | Recovers the root of the installed service for that role without reading the build config; only HOME is extracted from environment files, and old services without HOME keep their original default storage |
| DEBUG App command tool | `scripts/app/personactl.py` | `scripts/app/jarvisctl.py`, same implementation; the old command and cache location are kept |

For the full list of source paths, Xcode project / schemes, and system identities, see [compatibility migration](rename.md#源码位置构建入口与兼容名字). #773's [recovering the runtime directory per service](rename.md#部署脚本按服务恢复运行目录) explains the boundaries of role, reset / drop-in / XDG, SameFile, and external EnvironmentFile / UnsetEnvironment; the latter two only handle the HOME field and removal rules, and never execute or return other environment fields; it stops when a required file is missing or the root field can't be parsed, and the actual root can also be given explicitly. Accepting new and old URL schemes and generating the old format are verified separately; renaming in the docs is no substitute for client acceptance testing.

Compatibility with and migration from the old directories are handled by the runtime / installer. Before upgrading, back up the data directory and Postgres actually in use; don't manually delete the old directory, merge the two, or regenerate keys just because of the rename. If both directories already exist, or you use custom paths, first confirm which one is actually loaded, so you don't start an empty hub. Historical measurements, signing profiles, source paths, package IDs, protocol fields, cache keys, and systemd / launchd names in the docs may still say Jarvis; they are not the new public product name and should not be migrated by find-and-replace.

Bundle IDs, App Groups, keychain groups, signing profiles, App Store records, database roles / databases / containers / volumes, public domains, and install / relay addresses all keep their existing identities. The avatar (`persona`) is still the concept of name / picture / status dot and is not renamed along with the product.

The old `JARVIS_*`, xcconfig / Gradle identifiers, and protocol names are still listed below for compatible configs and for reading the source; that doesn't mean every code identifier can be mechanically swapped for `PERSONA_*`. The migration release's implementation is authoritative for the actual entry points.

## Quick start: connecting several machines

An always-on Linux machine runs the hub, and every other computer is connected by the hub over SSH. The other computers need no resident service: when the hub connects it runs
`ssh <host> ~/.persona/bin/personad node --stdio` and speaks the node protocol over SSH's stdin/stdout, reconnecting automatically when it drops (`server/internal/hub/machine.go`).

### Prerequisites

| Machine | Needs |
| --- | --- |
| hub machine (Linux) | docker (the Postgres container; without Docker, use the system's Postgres, see `docs/server.md` ("Database")), systemd user services, herdr; the phone must be able to reach the hub: same network, a VPN you set up yourself, or direct public access (IPv6 preferred, with the TLS port open). On a NAS, or when it only serves as a hub without running Agents, it can also run entirely in Docker (`docs/docker.md`); steps 1 and 2 below are then only for installing `jarvisd` on each computer |
| Each computer to connect | Claude Code (or Codex); `~/.persona/bin/personad` and herdr (the deploy script in step 2 copies them over; if herdr is missing, install it into `~/.local/bin` with its official installer, or set `JARVIS_SKIP_HERDR=1` if you use another engine and don't want it) |
| SSH | The hub machine can log in to every computer **without a password** (the hub uses `BatchMode=yes`, so keys only), with an alias for each in the hub machine's `~/.ssh/config`; the dev machine also needs SSH to each one (for deploying) |

For computers behind NAT that the hub can't reach (e.g. a Mac at home, a machine on another private network), use **tailcat** (`docs/server.md` ("tailcat hole punching")):
install the resident node on that computer, run `personad node tailcat init` once, and put the printed address into that machine's `tailcat_addr` in hub.json. Neither side needs a public IP,
open ports, or a Tailscale account; the hub tries ssh first and falls back to tailcat. You can also, as before, open a reverse tunnel from that computer to the hub machine
(`ssh -R 2222:localhost:22 <hub machine>`, kept alive with launchd / autossh) and point the alias at `localhost:2222` in the hub machine's `~/.ssh/config`.

### Steps

1. **Dev machine: write the build / deploy config** `~/.config/persona/config.json` (copy from `config.example.json`; fields in the next section).
   The two sections relevant to connecting machines:

   ```json
   "hub": { "machine": "server", "url": "http://server.example.com:18660" },
   "machines": [
     { "id": "server", "ssh": "server", "os": "linux" },
     { "id": "laptop", "ssh": "laptop", "os": "darwin", "hostnames": ["MyMacBook"] }
   ]
   ```

   `ssh` is the host name the **dev machine** uses to SSH to this one. `os` (`linux` / `darwin`) and the optional `arch` (`amd64` / `arm64`) decide which binary to install; when one or both are omitted, the deploy script asks that machine (`uname -s` / `uname -m`), so leaving them out won't install the wrong one (an Intel Mac gets `darwin/amd64`, ARM Linux gets `linux/arm64`). A machine that can't be asked (powered off, unreachable) is skipped as unreachable, not treated as a wrong platform; only a written or reported platform with no matching build is an error, and the whole deploy exits non-zero. `hostnames` are this machine's own host names (`hostname -s`; on macOS the LocalHostName works too); when the deploy script runs on this machine, it copies locally instead of using SSH.
   **Add `hostnames` for whichever machine you run the deploy on**: any machine without them is treated as remote and installed over SSH (if the machine running the deploy lacks them, it SSHes to itself).
   For a computer the dev machine can't reach but another machine (usually the hub machine) can (the key only lives there, no route), write `"via": "<that machine's id>"`:
   the deploy copies to that machine first and installs from there, and `ssh` is then the host name **on that machine**.

2. **Deploy**: `./scripts/deploy/deploy.sh`. It builds `personad` for four platforms (`linux` / `darwin` × `amd64` / `arm64`, same as releases and the Docker image), and each computer gets the one it runs (after installing, it runs `version` once on that machine as a self-check; if that fails it reports an error and doesn't print installed). On the hub machine it
   starts the Postgres container and installs `personad hub` as a systemd user service and starts it.

3. **hub machine: tell the hub which computers to connect**. The first start generates `~/.persona/hub.json` (0600), containing only itself. Add the other computers and the address the phone uses:

   ```json
   "public_url": "http://server.example.com:18660",
   "machines": [
     { "id": "server", "name": "Server", "kind": "local", "icon": "server" },
     { "id": "laptop", "name": "Laptop", "kind": "ssh", "ssh_host": "laptop", "icon": "laptop" }
   ]
   ```

   `ssh_host` is the host name the **hub machine** uses to SSH to this one (each side has its own `~/.ssh/config`, so it can differ from `ssh` in step 1); `id` should match step 1;
   `name` is the name shown in the App; `icon` is `server` / `laptop` / `desktop`. `remote_bin` is the path of `personad` on that computer; when omitted the hub uses `~/.jarvis/bin/jarvisd` (the `jarvisd` the deploy script installs, with `personad` as a symlink to it); set it if installed elsewhere.
   You can fill in `assistant` while you're at it (see the hub.json section below). For direct public access add `public.tls_listen` and `public.advertise` and open the TLS port per the [network doc](network.md); keep `public_url` as an address old Apps can use.

4. **Restart the hub so it reads the new config**: `./scripts/deploy/deploy.sh <hub machine id>` (or `systemctl --user restart jarvisd` on the hub machine).

5. **Pair the phone**: run `~/.persona/bin/personad hub pair` on the hub machine and scan the QR code with the App. The command gets a temporary token from the hub running locally; it is valid for 10 minutes, can be redeemed for a device token only once, and after redemption has at most a 2-minute grace period for requests still in the middle of pairing. Only `--legacy` prints the main token.

6. **Check**: in the App, **Settings › Machines** shows every machine online; the "Agents" tab shows the sessions in herdr on each computer. If one is offline, run
   `ssh -o BatchMode=yes <ssh_host> '~/.persona/bin/personad node --stdio'` by hand on the hub machine to see whether SSH fails or the binary is missing.

To add a computer later: add a line to `machines` in both configs and run `./scripts/deploy/deploy.sh <new machine id> <hub machine id>`.

**Recommended: install the node service on the other computers** (2026-11-02, [plan-hub-center.md](plan-hub-center.md) section 0): install only one hub for the whole household; the other computers skip SSH and
install a resident node plus the connector jarvis-link, with one command or one App, see [link.md](link.md) ("Installing the node service"):

- Linux: `curl -fsSL <site>/install.sh | sh -s -- --node --pair '<link from personad hub pair --machine <id>>'` (this computer connects to the hub; in `machines` it is
  `{"id":"laptop","name":"Laptop","kind":"link","icon":"laptop"}`, and the hub needs `public.tls_listen`), or `… --node --listen` (the hub connects to it;
  paste the printed link into the hub admin UI under **Computers › Add computer › Hub connects**, and in `machines` it is kind `service`).
- Mac: install the menu bar App "Persona Link" (`./scripts/app/mac-link.sh install`); it bundles personad and installs the resident node on first run; pairing is in its menu.

No `ssh_host`, and no SSH setup on the hub. The SSH setup in steps 1–6 above still works, and is kept only for compatibility from now on.

### Current limitations

- The deploy script only installs the hub on Linux (systemd + docker); the hub can't live on a Mac for now.
- The machine list has to be written in two places: the dev machine's `config.json` covers "where to deploy", the hub's `hub.json` covers "which machines the hub connects".

## Build / deploy config (config.json)

```json
{
  "app": { "bundle_id": "com.example.jarvis", "team_id": "ABCDE12345" },
  "app_store": {
    "app_id": "1234567890",
    "key_id": "ABC123DEFG",
    "issuer_id": "00000000-0000-0000-0000-000000000000",
    "key_path": "~/.appstoreconnect/AuthKey_ABC123DEFG.p8",
    "testers": [{ "email": "you@example.com", "first_name": "You" }],
    "feedback_email": "jarvis@example.com"
  },
  "hub": { "machine": "server", "url": "http://hub.example.com:18660" },
  "machines": [
    { "id": "server", "ssh": "server", "os": "linux" },
    { "id": "laptop", "ssh": "laptop", "os": "darwin", "hostnames": ["MyMacBook"] }
  ],
  "simulator": "",
  "xcode": ""
}
```

| Field | Purpose |
| --- | --- |
| `app.bundle_id` | The iPhone App's bundle id; the watch is `<id>.watchkitapp`, tests are `<id>.tests`. Written to `JARVIS_BUNDLE_ID` in `apple/Config.xcconfig` |
| `app.team_id` | Developer team, written to `DEVELOPMENT_TEAM` in `Config.xcconfig`; the ExportOptions used for export also use it |
| `app.app_group` | The App Group through which the App and the Dynamic Island extension share files (avatar and avatar pack images, `docs/live-activity.md` ("Avatar")), written to `JARVIS_APP_GROUP` in `Config.xcconfig`. Defaults to `group.<app.bundle_id>`; it must be registered on the developer site first (`docs/release.md` ("App Group")) |
| `app.push_relay` | Push relay address (e.g. `https://<relay domain>/relay`), written to `JARVIS_PUSH_RELAY` in `Config.xcconfig` and `JarvisPushRelay` in Info.plist: the App registers with it, and hubs without an APNs key push through it (`docs/push-relay.md`). Without it the App doesn't register, and only hubs with `apns.json` can push |
| `app.carplay_voice` | Boolean, default `false`: enabling adds the CarPlay voice-conversation entitlement (`com.apple.developer.carplay-voice-based-conversation`) to signing, making Persona visible in the car. Apply to Apple first and enable it on the App ID after approval; do not enable before approval or signing fails. `scripts/persona_config.py xcconfig` generates `apple/PersonaCarPlay.entitlements` and writes `PERSONA_APP_ENTITLEMENTS` (see `docs/carplay.md`) |
| `app.meta_wearables` | Meta glasses (`docs/meta-glasses.md`): when `enabled` is true, `persona_config.py env` exports `PERSONA_META_WEARABLES=YES`, and only then does xcodegen merge `apple/meta-wearables.yml` (Meta's DAT SDK and the Info.plist keys it needs) into the project; `app_id` and `client_token` are the app's ID and Client Token from the Wearables Developer Center, written to `PERSONA_META_APP_ID` and `PERSONA_META_CLIENT_TOKEN` in `Config.xcconfig`; they can be left empty while the glasses are in Developer Mode (`app_id` 0). Off by default: it needs Xcode 26.4 or later, and carries risk in external TestFlight / App Store review |
| `app.tunnel_core` | Directory containing the in-App tunnel core (`libpersonatunnel.a`, `module.modulemap`, headers; [network.md](network.md) ("Connecting to the hub through an in-App tunnel")). Only when set does it write `PERSONA_TUNNEL_INCLUDE` and `PERSONA_TUNNEL_LDFLAGS` into `Config.xcconfig`; only hardware builds link it. Missing files cause `xcconfig` to fail immediately. Omitted means no in-App tunnel |
| `app_store.*` | For `release.sh` / `asc*.py` (`asc_cli.py` hands them to the App Store Connect CLI, `docs/release.md` ("Releasing with asc")): the API key (`key_id`, `issuer_id`, `key_path`), the App record's numeric ID, internal testers, the TestFlight feedback email; `external` is external testing (group name, public link switch and tester limit (`public_link_limit`, 0 means unlimited), privacy policy URL, extra testers invited by email, Beta App Review contact and notes, `docs/release.md`). Env vars such as `ASC_KEY_ID` can still override (key file path `ASC_KEY_PATH` or `ASC_PRIVATE_KEY_PATH`) |
| `hub.machine` | Which machine the hub runs on (an id in `machines`): `deploy.sh` starts Postgres and the systemd service on it, `test-server.sh` runs tests on it, `sim.sh` reads the token from it |
| `hub.url` | The address `sim.sh` uses to pair the simulator; the HTTP in the example is only for a private network / VPN; configure the public entry separately in `public` of `hub.json` |
| `machines[]` | Machines where `deploy.sh` installs `personad`: `ssh` is the ssh host name; `os` (`linux` / `darwin`) and `arch` (`amd64` / `arm64`, optional) decide which binary to install, and whichever is missing is asked from the machine itself (`uname`); `hostnames` are the machine's own host names (when the deploy runs on it, it copies directly without ssh; the machine running the deploy must list them, and any machine without them goes through ssh); `via` is another machine's id: when the dev machine can't reach this one, it ssh-es once more through that machine to transfer the binary (leaving no files there), and `ssh` is then the host name on that machine |
| `simulator` | Default simulator UDID for `sim.sh` (override with `SIM=<udid>`) |
| `publish.site_url` | Where `publish.sh` publishes `personad` and `install.sh` (the domain of the `site/` worker, `docs/release.md` ("Publishing personad")); without it nothing is published |
| `publish.getting_started_url` | Public page opened by "No hub yet?" on the iOS / Mac welcome screen (the published address of `docs/getting-started.md`), written to Info.plist `PersonaGettingStartedURL`; without it, this row is hidden (#4195) |
| `publish.wrangler` | The wrangler command used to deploy that worker (e.g. a script that wraps authentication); defaults to `npx wrangler` |
| `xcode` | Optional: which Xcode to build and release with (path to the `.app`); `persona_config.py env` exports `DEVELOPER_DIR` from it (a value already in the environment wins). Supporting iPhone Duo needs Xcode 27 or later; Apps built with older versions don't extend under the status bar and camera on Duo |
| `android.application_id` | The Android App's application id (`scripts/app/android.sh`, `docs/android.md`); defaults to `app.bundle_id` |
| `android.google_oauth_scheme` | The scheme the Android App uses for the Google sign-in callback for Gmail: the reversed client ID of the hub's `google.client_id` (`docs/mail.md` ("Android")); without it Gmail can only be added with an app-specific password |
| `mac.link_bundle_id` | Bundle id of the Mac menu bar App "Persona Link" (`scripts/app/mac-link.sh`, `docs/link.md`); the env var `PERSONA_LINK_BUNDLE_ID` (old `JARVIS_LINK_BUNDLE_ID`) wins; defaults to the placeholder `com.example.jarvis.link` |

`python3 scripts/persona_config.py env` prints shell variables (scripts `eval` it at the top), and `xcconfig` generates `apple/Config.xcconfig`;
`sim.sh` / `release.sh` generate it automatically. Before running `xcodegen` directly or opening Xcode, run `python3 scripts/persona_config.py xcconfig` once.
The committed `project.yml` and `Info.plist` only reference `$(JARVIS_BUNDLE_ID)` / `$(PRODUCT_BUNDLE_IDENTIFIER)`, and the keychain service uses the bundle id at runtime.

## Multiple Codex app-server instances

Each machine in `hub.json` can configure `codex_homes` (an array of directories), `codex_servers` (`id`, `name`, `home`, `endpoint`), and optionally `codex_discovery` (automatic discovery by default; false uses only registered sources). For example:

```json
{
  "codex_homes": ["~/.codex-profile"],
  "codex_servers": [
    {"id": "desktop", "name": "Desktop instance", "home": "~/.codex-profile", "endpoint": "unix://<absolute socket path>"}
  ]
}
```

No application name needs hard-coding: every 30 seconds the node discovers the current user's Codex/app-server processes, `CODEX_HOME`, and enabled listen endpoints; existing official control sockets under known homes can also be discovered. Discovered directories and named instances are remembered in the hub's Postgres settings; process/connection generations are not persisted. Private paths belong only in runtime config; do not commit actual config values.

The Unix socket and its immediate parent directory must belong to the current user and be inaccessible to groups/other users; WebSocket runs inside. TCP endpoints allow only literal loopback IPs, disallow external hosts, userinfo, and query credentials, and do not use the system HTTP proxy. Stdio without an endpoint is read-only; no daemon is started, no desktop app replaced, and no file permissions loosened. More limitations in `codex-app-server.md`.

## hub runtime config (hub.json)

```json
{
  "listen": ":18660",
  "public_url": "http://hub.example.com:18660",
  "public": {
    "tls_listen": ":18662",
    "advertise": ["https://hub.example.com:18662"],
    "advertise_ipv6": true,
    "advertise_lan": true
  },
  "security": { "allow_public_plaintext": false, "trusted_cidrs": [] },
  "name": "Home",
  "admin": { "access": "lan" },
  "token": "…",
  "database_url": "…",
  "machines": [
    { "id": "server", "name": "Server", "kind": "local", "icon": "server" },
    { "id": "laptop", "name": "Laptop", "kind": "ssh", "ssh_host": "laptop", "icon": "laptop" }
  ],
  "assistant": {
    "owner": "Your name",
    "notes": "- Code is in `~/code`.\n- Use `xxx-*` skills for work data.",
    "machine": "server",
    "launcher": "claude"
  },
  "ai": {}
}
```

- First run writes only the local machine (`kind: local`, id is the host name); the generated `public_url` is the HTTP address for that host name, only a private-network compatibility endpoint. TLS is off by default; the example explicitly enables `:18662` (an example port, changeable). Configure other machines and domains yourself.
- `listen` is the retained HTTP listener, default `:18660`; `public_url` goes into the QR code's `url` and the candidate list. Existing iOS / Watch clients still need it to point to a reachable private-network / VPN address.
- For each `public` and `security` setting, its defaults, and how to allow traffic, see [network configuration](network.md#hubjson-网络配置). Candidate addresses are sent only after authentication.
- `security.proxy_protocol` (optional, omitted by default): with same-machine port forwarding, a reverse tunnel, or nginx `stream` passthrough in front of the hub, public connections all look like loopback to the hub.
  Set `{"from": ["127.0.0.1/32"]}`: connections from these upstreams must carry a PROXY header (v1 / v2), and the hub checks the real source in the header; `listeners` defaults to only `tls`.
  Syntax, rules, and self-checks in [network.md](network.md#proxy-protocol).
- `name` (optional): the hub's display name (e.g. "Home"), shown by jarvis-link on computers (Mac menu bar) and by future linked-hub and multi-hub clients.
  Defaults to the short host name of the hub machine; if the host name is a Docker container id or `JARVIS_LOCAL_NODE=0`, it uses `hub-<first 6 chars of hub_id>`. Manual file edits require a hub restart;
  edits in the admin UI's "Overview" take effect immediately ([admin.md](admin.md)).
- `admin` (optional): hub admin UI (`<hub address>/admin`, [admin.md](admin.md)). `access` controls who can open it: `loopback` / `lan` (default) / `cidrs` / `any`
  (requires TLS); `cidrs` lists networks allowed with `access: cidrs`; `session_hours` is the login expiry (default 12); `first_setup: "code"` (written by a fresh `personad hub setup` install) requires a setup code even from LAN sources to set the first password; without it only loopback requires one. Access can only be broadened on the hub machine with
  `personad hub admin access <scope>` (or the deploy script); the web UI can only narrow it. Env vars `JARVIS_ADMIN_ACCESS` / `JARVIS_ADMIN_CIDRS` take precedence.
  The admin UI writes hub.json atomically and preserves unknown fields ([admin.md](admin.md) ("Editing hub.json")).
- `mesh` (optional, linked hubs, [mesh.md](mesh.md)): `discovery` (default true: advertise itself with mDNS on the LAN and list discovered hubs; unavailable inside a Docker bridge network),
  `path_switch_seconds` (default 60: when a computer has a better path, it must stay better this long before the primary switches; broken paths switch immediately),
  `plaintext` (off by default: `allow` lists `*`, networks / addresses, `192.0.2.*`, and host names / `*.example.com` used only for dialing that may pair and link without TLS; `advertise` lists `http://` addresses other hubs use to connect to this one in plaintext; no encryption or pinning, see [mesh.md](mesh.md) ("Plaintext links")). Peers, the group's primary, and its term live in
  Postgres, not here. The first version's `takeover_seconds` is no longer used.
- `link` (optional, computer-link heartbeat, [link.md](link.md) ("Heartbeat")): `ping_seconds` (ping interval, default 10), `ping_timeout_seconds` (time without pong before disconnecting, default 5).
  Omitted or 0 uses the default; setting only one leaves the other at its default. Range 1–600 seconds, capped at 600. Controls the hub's pings on Persona Link connections and streams, and the manager's pings on connector (kind service) streams.
  When set, the manager also tells Persona Link (in `endpoints` for active mode, in stream-opening request headers for passive mode), and the computer pings accordingly; hub-to-hub links are unaffected and remain 10 seconds / 5 seconds.
  On slow links (only tens of KB/s between hub and computer), pings queue behind data being written (a 1 MiB chunk at 30 KB/s takes over 30 seconds); a timeout shorter than writing one chunk always causes a false disconnect. Set
  `ping_timeout_seconds` longer (e.g. `{"ping_seconds": 30, "ping_timeout_seconds": 90}`); a dead link then takes roughly "interval + timeout" to detect. The network library's ping send and pong reply
  themselves wait at most 5 seconds, and forwarding a message writes for at most 1 minute; neither is configurable (link.md ("Heartbeat")).
  Older Persona Link versions do not recognize these settings and still ping at their own 10 seconds / 5 seconds (active mode only pings the connection itself; passive mode also pings every stream); upgrade both together. Manual file edits require a hub restart.
- `machines`: since 2026-11-04 these are defaults handed by the hub to the manager (the assistant process) at handshake, and **the manager connects them** ([machines.md](machines.md) ("Who connects computers")). Where the text below says "the hub dials" or "the hub connects",
  the manager actually dials (on the hub machine it is the hub's child process using the same HOME: ssh config and its keys in `~/.persona/managers/assistant/` are all this machine's). Computers added in the admin UI at runtime are also handed to it and connected immediately.
- `machines[].kind`: `local` (the hub's own machine), `service` (the hub dials this connector's passive port, "Hub connects", see the next item), `link` (this machine's jarvis-link
  actively connects to the hub, "Computer connects", [link.md](link.md)); `ssh` (the hub connects via `ssh_host`) and `tailcat` (tailcat only, see the item after next) are retained only for existing configs,
  no longer added through the admin UI; migration steps in [mesh.md](mesh.md) ("SSH / tailcat: compatibility"). One computer may have several entries (e.g. a service and a link); the hub recognizes
  the same computer by machine_uid, lists it once, and uses the best path ([mesh.md](mesh.md) ("Multiple paths to one computer")).
- `machines[].address`, `machines[].proxy` (kind `service`): the connector's `host:port` (included in the link from `jarvis-link pair --listen`; paste it under **Add computer ›
  Hub connects** in the admin UI to fill it in); optional `proxy`, `socks5://[user:password@]host:port`, lets the hub connect through this SOCKS proxy (when the computer is only reachable through another machine).
  The connector's public-key fingerprint and the credentials the hub gives it are not in hub.json: pairing stores them in Postgres (credentials encrypted with `~/.persona/secret.key`).
- `machines[].tailcat_addr` (optional): the `tc…` address printed by `personad node tailcat init` on that computer; the hub connects through tailcat when ssh fails
  (`docs/server.md` ("tailcat hole punching")). The address is a secret (includes the WireGuard preshared key); keep it only in hub.json, like the token. `kind: "tailcat"` means
  tailcat only, no ssh attempt (no `ssh_host`); `kind: "ssh"` plus `tailcat_addr` means ssh first, tailcat on failure.
- Env vars `JARVIS_DATABASE_URL`, `JARVIS_PUBLIC_URL`, `JARVIS_LISTEN`, `JARVIS_ADMIN_ACCESS` override the corresponding settings (new `PERSONA_*` names win, not written back to the file); Docker deployments use them. A nonempty `JARVIS_ADMIN_ACCESS` replaces the whole `admin` access scope together with `JARVIS_ADMIN_CIDRS`; setting only `JARVIS_ADMIN_CIDRS` has no effect
  (`docs/docker.md` ("Environment variables")); with `JARVIS_LOCAL_NODE=0`, first run does not write the local machine.
- `tailcat.derp_map_url` (optional): the DERP map URL when using a self-hosted DERP relay (derper, see `docs/server.md` ("Self-hosted relay")) instead of Tailscale's public relays; each computer's
  `init` must use the same `--derp-map-url`.
- `machines[].claude_config_dirs` (optional): Claude Code config directories on this machine besides `~/.claude` (`CLAUDE_CONFIG_DIR` set by launchers,
  e.g. `["~/.config/claude-code/profiles/work"]`; `~/` is that machine's home). The node finds directories used by running claude processes itself and remembers them in the database;
  only list directories "not usually running, but whose historical sessions you want in the list" here. Conversely, hide unwanted directories (automation scripts' own `CLAUDE_CONFIG_DIR`)
  in **Settings › Claude Code › Blocked Config Folders** in the App; stored in the database, not hub.json (`docs/claude-integration.md` ("Blocking config directories")).
- `machines[].claude_start_dir` (optional): the `CLAUDE_CONFIG_DIR` passed when Persona starts an Agent with the `claude` launcher on this machine
  (new Agent, to-do dispatch, assistant's agent_start, assistant chats), e.g. `"~/.config/claude-code/profiles/work"` (`~/` is that machine's home). For computers where "claude is signed into a profile directory, not `~/.claude`":
  without it, Persona's claude uses the default directory, which is not logged in and immediately replies `Not logged in · Please run /login` (#1459). Only controls the `claude` launcher;
  aliases / functions in `~/.zshrc` and Persona-managed launchers keep their own settings. This directory is also added automatically to the session-search directories above. The node must also be this version; manual hub.json edits require a hub restart and take effect on reconnect.
- `machines[].botmux` (optional): false means this computer's node neither lists botmux sessions nor runs botmux (no `botmux observe`, `botmux dashboard current`);
  omitted means the old behavior, listing sessions if botmux is installed (`docs/engines.md` ("botmux")). The node must also be this version; manual hub.json edits require a hub restart and take effect on reconnect.
  It is now only a default: "Also Show botmux Sessions" in machine details (the web in the page header, iOS under "Engine") is stored per machine in settings (`botmux/<machine id>`) and wins once set (#2575).
- `machines[].herdr_autostart` (optional): false means this computer's node does not start herdr's server itself when herdr is not running; omitted means on
  (`docs/engines.md` ("When herdr is not running")). **Only a default**: once the user chooses per machine under **Settings › Machines › Engine**, that choice wins
  (stored in the hub's settings, `herdr_autostart/<machine id>`); later edits here do not affect machines already configured.
- `machines[].herdr_autoinstall` (optional, off by default): true means when herdr is missing on this computer, the node installs a pinned version before starting herdr itself
  (at startup, before creating / continuing sessions). Without it, installation happens only when the user taps "Start" in the Agents list (`docs/engines.md` ("When herdr is not installed")). The node must also be this version; manual hub.json edits require a hub restart and take effect on reconnect.
- `machines[].claude_global_hooks` (optional, off by default): true means this computer's node adds **report-only** hooks to `settings.json` in the default Claude config directory (asynchronous, short timeout, never answer approvals),
  so sessions not started by Persona also report turns and question events to Persona; false or removing the key removes Persona's entries on the next connection (if none exist, nothing is written).
  No blocking hooks are added: users' global configs often have other tools' synchronous PermissionRequest hooks; stacking them delays terminal dialogs (`docs/claude-integration.md` ("Global report-only hooks")).
  The node must also be this version; manual hub.json edits require a hub restart and take effect on reconnect.
- `machines[].usage_windows` (optional, on by default): false means this computer's node does not read Codex / Claude subscription usage windows (no `rate_limits` in rollouts,
  no `~/.persona/usage/claude*.json`), and the manager does not ask it either. Omitted means read, local files only, no login credentials touched, every 5 minutes (`docs/usage-windows.md`).
  The node must also be this version; manual hub.json edits require a hub restart and take effect on reconnect.
- `machines[].launchers` (optional): add launchers the node cannot list on this computer (e.g. shell wrapper functions), or override `agent_type`, `base_url`, `model` for same-name launchers the node reports (omitted fields stay unchanged),
  e.g. `[{"name":"example_wrapper","agent_type":"codex"}]`: the wrapper actually runs Codex (`agent_type` is `claude` / `codex` / `traex` or a runtime's agent; `docs/protocol.md`, `docs/model-config.md`).
- `personas.github_login` (optional): whose level on the NWVERSE board determines the tiered avatar (Milk Frog). Defaults to the GitHub connector's logged-in account;
  with neither, shows the default "Unhatched" tier (`docs/persona.md` ("Tiered avatars")).
- `assistant.owner`: how the assistant in the "Chat" tab addresses you; `assistant.notes`: Markdown appended to the assistant instructions (`~/.persona/assistant/CLAUDE.md`),
  describing things specific to your environment: code locations, skills / CLIs for work data, directories it must not touch.
- The everyday entry for model settings is **Settings › Model Providers / Model Configuration** (`docs/model-config.md`): the former manages connections and credentials; the latter manages uses such as chat, classification, translation, knowledge processing, `/agent`, and scheduled tasks. Old `ai`, `inbox.*_ai`, `kb.ai`, and launcher defaults remain as a compatibility baseline; unified-use overrides live in Postgres, not written back to `hub.json`. Without an override the original model is kept; opening settings does not migrate it to another provider.
- `assistant.machine` / `assistant.launcher`: default machine and launcher for the assistant (the hub's own machine, `claude`); App settings take priority once changed.
  `owner`, `notes`, `machine`, `launcher` are all defaults handed by the hub to the assistant at handshake: changed settings belong to the assistant itself (settings `assistant`, only it reads/writes them, `docs/assistant.md` ("Boundary with the hub")).
- `assistant.external` (default `false`): the assistant process (`personad assistant`, `docs/assistant.md` ("Process")) is started externally. By default the hub starts it as a child process and restarts it on exit,
  giving it a fresh bus token at every start (via stdin); with `true` the hub does not start it. On the machine that will run it, first generate a token with `personad hub assistant-token` (printed once; generating again replaces the old one), then
  `PERSONA_ASSISTANT_TOKEN=<token> PERSONA_ASSISTANT_HUB=<hub address> personad assistant`. It reads hub.json and env vars just like the hub: database from `database_url` (`PERSONA_DATABASE_URL` takes priority),
  and without `PERSONA_ASSISTANT_HUB`, connects to the local `listen` port. While the assistant is disconnected, its APIs return `503 assistant_offline`.
  The assistant also carries all computers ([machines.md](machines.md) ("Who connects computers")): when running elsewhere, that machine connects the computers in `machines` (ssh host names, tailcat key,
  and even the hub machine's own `node --stdio` are on that machine); while disconnected, machines show offline and machine APIs return `503 assistant_offline`.
  Other managers created in the App do not read this hub.json section: their configuration is `manager.json` in their own directory ([managers.md](managers.md) ("A second manager")); their database is a Postgres of their own (a Docker container `personad manager install` makes, or `--database-url`), never the hub's.
- `assistant.persona_view` (default `false`): cards in assistant replies use ```` ```persona-view ```` (extension components, `docs/extensions.md` ("UI version 3")) instead of `persona-card`.
  Enable only after all clients (iOS / Mac, Android, web) can render UI version 3; `persona-card` still renders on every client.
- `assistant.html_views` (on by default when omitted; false disables): a card's `html` component can call the hub via `persona.reply` / `persona.open` (`…/views/{key}/html-actions`).
  Only effective with `assistant.persona_view` enabled; when disabled, HTML always shows fallback. Each client must also pass its own HTML execution security acceptance before running it; see `docs/assistant-views.md`.
- `ai` (no default since 2026-10-31): model for small App features calling model APIs directly (not through Claude Code): Inbox triage, drafting replies, recognizing email config from pasted text.
  When omitted, these uses automatically select a directly callable connection on the hub (with a Key in **Settings › Model Providers**, see `docs/model-config.md` ("Editing and inheritance")); only if none exist do they report "No directly callable model yet".
  `api` is `messages` (Anthropic Messages, default) or `responses` (OpenAI Responses, always streamed, compatible with relays accepting only `stream: true`);
  `base_url` gets `/v1/messages` or `/v1/responses` appended; `api_key_file` is the key file on the hub machine (0600, read only at call time); `model`;
  `reasoning_effort` applies only to `responses` (e.g. `low`); `bearer` (default `false`) true sends the Messages API key as `Authorization: Bearer` instead of `x-api-key`, for gateways accepting only Bearer (`responses` always uses Bearer).
- `agents.triage`: triage after a round stops ([agent-work.md](agent-work.md) ("Switches")). Switches are under **Settings › Session Triage** in the App (stored in hub settings, all off by default),
  and only limits live here: `max_retries` (maximum automatic continuations, default 3), `max_wakes` (maximum wakes, default 6), `settle_seconds` (delay after stopping before triage, default 15);
  `enabled` / `auto_continue` / `wake` / `decide` are merely initial values if never saved in the App (omitted means off). Triage is done by the manager (assistant process); this section is handed over at handshake, and changes take effect after restarting the hub.
- `agents.launcher_health`: launcher health checks and automatic repair ([launcher-health.md](launcher-health.md)): `enabled` (off by default; true enables periodic checks and automatic repair), `interval_minutes` (default 30),
  `repair` (repair Agent: no `skill` means no repair Agent, only CLI upgrades; `machine` defaults to the assistant's computer, `cwd` to home, `launcher` to an available one, `permission` is the permission mode, default `bypassPermissions`).
- `agents.delegate` (#3029): the assistant answers Agent questions first ([assistant.md](assistant.md) ("The assistant answers questions first")). **Off by default**, enabled only with `enabled: true`;
  `timeout_seconds` is how long the hub waits for the assistant (default 180); if no result arrives, the question is pushed to the user unchanged. `rules` are the user's own delegation rules (prose shown to the model with built-in rules; they cannot relax red lines).
  Restart the hub after changes. Example enabling it (alongside other keys in `~/.persona/hub.json`):

  ```json
  "agents": {
    "delegate": {
      "enabled": true,
      "timeout_seconds": 180,
      "rules": "If tests are missing, add them without asking me"
    }
  }
  ```

  Model selection is elsewhere: **Settings › Model Configuration › Question Delegation** (purpose `agent.delegate`). If never selected, it uses the "Information Filtering" connection with Opus 5.5 (`claude-opus-5-5`);
  change provider or model there, or use `PATCH /api/v1/model-config/purposes/agent.delegate` (`{"revision": <current>, "model": "claude-sonnet-5-5"}`); "Restore Defaults" returns to Opus.
- `notify`: timing for "Done" pushes (`hub/notify.go`, omitted uses defaults): `done_delay_seconds` (how long after completion to wait for someone to look before pushing, default 60),
  `long_turn_seconds` (how long a round must be to count as a "long task", default 60), `background_hold_minutes` (maximum wait for background tasks to finish, default 120),
  `live_metrics_seconds` (minimum interval between pushes to one device when only Live Activity numbers change, default 60; `docs/live-activity.md` ("Push budget")).
  Each device's push preferences are set separately in the App (**Settings › Notifications**, stored in hub `devices.prefs`), not here.
- `inbox.triage_ai` / `inbox.judge_ai` (optional, same fields as `ai`, omitted fields inherit from `ai`): AI twin model 1 (triage) and model 2 (linking a Needs you item to one of the user's tasks);
  omitted means both use `ai`. `inbox.agent`: model 3, `enabled`, `launcher` (Claude Code launcher, default the assistant's), `machine` (default the assistant's machine),
  `concurrency` (1), `timeout_minutes` (15), `max_age_hours` (24), `rules` (no longer read; what the twin takes on is set by inbox policies, `docs/inbox.md` ("Policies")). See `docs/inbox.md` ("AI twin").

  ```json
  "inbox": {
    "triage_ai": { "base_url": "<gateway address>", "api_key_file": "~/.persona/inbox-ai.key", "model": "<model>" },
    "judge_ai":  { "base_url": "<gateway address>", "api_key_file": "~/.persona/inbox-ai.key", "model": "<model>" },
    "agent": { "enabled": true, "launcher": "<launcher>" }
  }
  ```
- `kb`: knowledge base (`docs/knowledge.md`): `enabled` (on by default), `text_days` (text retention days, 365), `attachment_days` (attachments, 30),
  `ai` (model for compression and tagging, same fields as `ai`; defaults to `inbox.triage_ai`, then `ai`), `extra_dirs` (note directories you maintain on the hub machine,
  e.g. a git repo of facts / lessons: `kb_search` also searches Markdown in these directories (read-only, skips dot directories such as `.git`), and the assistant's CLAUDE.md mentions them; `~/` is that machine's home).
  The knowledge base belongs to the assistant (manager); the hub hands this section over at handshake (except `ai`, read by the hub's model config), and even an assistant running elsewhere follows the hub's copy;
  with `assistant.external`, `extra_dirs` are paths on the assistant's machine (`docs/knowledge.md` ("Who owns what")).
- `inbox.rules`: personal rules for Inbox AI triage (natural language, e.g. "anything from this person needs me", "this alert group is only noise"), appended to the triage prompt and preferred over default judgments (`docs/inbox.md`).
  When a connector item carries its own rules (`rules`, e.g. an extension's `triage_rules` setting), those are used instead of this setting.
- `hook_timeout_minutes` (default 30, 0 or negative also means 30): maximum time Claude Code hooks wait for phone answers (questions, approvals); timeout hands control back to the terminal (`docs/claude-integration.md`).
- `shutdown_timeout_seconds` (default 8): total hub shutdown budget, including closing the database connection pool; external `docker stop -t` must be longer (`docs/server.md` ("Shutdown")).

## manager.json

A manager's own sections of the configuration are in its `manager.json`, not in `hub.json`: the hub reads none of them (`server/internal/managerconf`).

- The built-in manager (the assistant process the hub starts, `personad assistant`): `~/.persona/managers/assistant/manager.json` (0600, its directory 0700), where its own directory goes once it is a service of its own. It holds no identity: the hub hands this manager its token.
- A manager made in the app: the `manager.json` of its own directory (`~/.persona/managers/<id>/manager.json`), next to who it is (`id`, `hub`, `token`…; [managers.md](managers.md) ("Directory and manager.json")).
- **Upgrading**: the first time the built-in manager starts with this version it copies `lark`, `lark_gate`, `connector_gate`, `github`, `imessage`, `google`, `microsoft`, `bots`, `extensions`, `plugins`, `trusted_extension_markets`, `usage` and `sent_ledger_days` out of `hub.json` into its `manager.json`, once: `moved_from_hub_json` in `manager.json` records which sections it took over and when, and `hub.json.before-manager-json-<time>` beside it (0600) is a copy of those sections as `hub.json` had them (only those: the hub's token and database password stay out of the manager's directory; any whole copy an earlier build left in that directory is cut down to the manager's sections at the next start). A section `manager.json` already has keeps its value there, and when `hub.json`'s differs the manager says so at start (`configuration: …: hub.json's copy differs …`); a section `hub.json` doesn't have isn't recorded, so if one appears there later (an older build switched back wrote it) the next start moves it too.
  `hub.json` itself is not changed, so an older build switched back still finds what it read before; changes made after the move (in the app's admin pages or by hand in `manager.json`) are in `manager.json` only, and the older build does not see them. Nothing reads `hub.json`'s copies any more: when one of them is edited by hand after the move, the manager says at start (`configuration: hub.json's … changed after they moved …`) that the change belongs in `manager.json`. They can be deleted from `hub.json` once no older build will run.
- The built-in manager's own files are in its directory beside its `manager.json`: `secret.key`, `tailcat-hub.json` and `handles.key`, copied there once from `~/.persona` (`moved_from_data_dir`). Its knowledge base stays at `~/.persona/data` unless `kb_dir` says where ([managers.md](managers.md) ("The built-in manager's own files")).
- Edits: the admin UI's **Usage sources** writes `manager.json`'s `usage`; hand edits of `manager.json` take effect when the hub restarts (which restarts the built-in manager), or the manager for one made in the app.
- A `lark` section that fails its own check (an account out of range, an unknown `prefer`) no longer stops the hub: the manager says so at start and doesn't use it (lark-cli is then off for the assistant).
- **A `manager.json` that doesn't read stops the manager, not the hub**: when it isn't JSON or is empty, a section the manager reads at start (every moved one but `usage`: a `usage` of the wrong shape is said in the log and no usage source runs) has the wrong shape, a `connector_gate` rule is one the gate can't use, its `moved_from_hub_json` is broken, it, its directory or the `hub.json` beside it is a link to nothing (a volume not mounted yet), that `hub.json` doesn't read, or the move can't be written, the manager doesn't start and says why (naming the file). A `lark_gate` rule's action is not checked: one the gate doesn't know asks for confirmation. Started without its sections it would drop the user's gate rules (`lark_gate`, `connector_gate`), and the computers would fall back to their own, which let every read through. The hub keeps running and starts the built-in manager again with its usual backoff (1 s doubling to a minute), saying the reason each time until the file is fixed; the app shows that manager as offline meanwhile. A manager made in the app does the same, before it opens its database, each time its service starts it again (every few seconds); a `manager.json` that isn't JSON, is empty or isn't there at all (a link to a volume not mounted yet) is the exception: as before, a manager made in the app exits with status 2 on it, which its systemd service doesn't restart on (launchd and Docker do), so on Linux start it again once the file is back.
- **Switching a manager made in the app back to an older build**: the older build reads its sections from a `hub.json` in that manager's directory, not from `manager.json`, so what was moved or changed since is not seen there. Its `personad manager join` and `install` rewrite `manager.json` whole, keeping only who the manager is, so the sections and the marker are gone when this version runs again: copy `manager.json` aside before running either on an older build, and put its sections back afterwards (the `hub.json.before-manager-json-*` copies have them only as they were when they moved, and a manager that never had a `hub.json` has none). The older `personad manager run` and `status` also count `managers/assistant` (the built-in manager's directory) as a manager made in the app: give `--dir` to pick the manager.

```json
{
  "google": { "client_id": "…", "proxy": "" },
  "lark": { "account": 2 },
  "extensions": [{ "id": "voice", "command": "jarvisd", "args": ["voice"] }]
}
```

- `usage` (optional): subscription usage sources from an account-pool gateway (such as a relay), merged into the sidebar's usage bar (`docs/usage-windows.md` ("Account-pool gateways"), #2595).
  `usage.sources[]` (at most 8): `id`, `name` (source name in the sidebar), `type` (`relay` / `generic`; old type names are still read as `relay` and rewritten to the new name the next time this source is edited in the admin UI), `url`, `key` (the gateway's admin Key, **a secret: only in `manager.json` (0600)**, unreadable from the admin UI and APIs, absent from logs), `enabled` (false disables this source).
  `usage.enabled` (false is the master switch), `usage.poll_minutes` (poll interval per source, default 5, 1 to 1440). This section belongs to **the manager** (`personad assistant`, which reads and writes it in its `manager.json`; the hub does not read it): it only reads the gateway's cached numbers (`refresh=false`), requests come from the manager, and nodes never touch the gateway.
  Edit in the admin UI at `/admin/usage` (the hub forwards to the manager) for immediate effect; manual edits require a hub restart (which restarts the manager too), or opening this admin page (the manager adopts the version on disk).
- `microsoft` (optional): Outlook's "Sign In with Microsoft" (`docs/mail.md` ("Outlook integration")). `client_id` is the default client ID
  (usually unnecessary: the user enters their own Microsoft Entra-registered one in the App, encrypted in the hub and preferred over this); `redirect_uri` defaults to
  `jarvis-mail-oauth://outlook`, and changes must match the registration in Entra.
- `lark.token` / `lark.container` / `lark.base_url` / `lark.native_olk_token` (old syntax, **no longer effective**): since #1571, integration through the Lark gateway is provided by an extension
  (start of `docs/feishu.md`, `docs/extensions.md`). The manager still reads these keys, but doesn't connect to the Lark gateway with them; if `token` or `container` is set,
  it logs one warning at startup. Set the Agent token, container, address, and separate encrypted-message authorization (`native_olk_token`) in the Lark extension's settings instead; these keys can be removed.
- `lark.account` (optional integer, still effective): the default Lark gateway account number, `1` for the main account, `2` for the second; omitted or `0` uses the main account, valid range 1–999999999,
  e.g. `"lark": {"account": 2}`. The number comes from the `id` in the Lark gateway's `/accounts`, not the Lark user ID. Operations on Inbox Lark items from the default account omit `ol_account`,
  while other accounts pass their own number; the Inbox Agent also uses this default when handling Lark items (`docs/feishu.md` ("Multiple accounts")). The Lark extension's default account is authoritative (`default` in its messaging source's `GET accounts`, #5098):
  if the extension cannot be queried, use the last retrieved value; only if none has been retrieved since the manager started is this field used. Restart the hub after changing (`systemctl --user restart jarvisd`).
  Server-side Inbox records are isolated and retained per account, available again when switching back.
- `lark.cli` (optional): Lark's official open-platform CLI, lark-cli, as a second Lark source besides the Lark extension (gateway) (`docs/plan-lark-cli.md`):
  `"lark": {"cli": {"bin": "lark-cli", "profile": ""}}`. `bin` is a command name or absolute path, default `lark-cli`; non-absolute paths are found in a login shell
  (global npm installs often live under nvm-like directories absent from systemd's PATH); `profile` is lark-cli's `--profile`, empty uses its default.
  Application and user logins stay with lark-cli (`~/.lark-cli`), never in `manager.json`: run
  `lark-cli auth login --domain all` yourself on the assistant's machine (user permissions for all domains; without a browser add `--no-wait --json` and authorize via the link on the phone); after a week unused, sign in again.
  Currently (L1) only for the assistant: its PATH contains `lark-cli` (with the configured `profile`), and write operations go through the gate (`docs/assistant.md` ("Lark: lark-cli")).
- `lark.prefer` (optional): which source is primary when both the Lark extension (gateway) and lark-cli are available: omitted (default, extension path preferred) or `cli`; other values are the Lark extension's own source names, passed through and validated by it. Currently only affects how the assistant prompt divides the work.
- `lark_gate` (optional, old syntax, still effective; put new rules in `connector_gate` below with `connector: lark`): gate rules for the assistant's Lark writes (the Lark extension's command gate, and lark-cli), after `connector_gate`'s `lark` rules and before built-in rules; first match wins:
  `[{"method":"POST","path":"/conversations/*/messages","action":"allow"}]` (`action` is `allow` / `confirm` / `deny`, optional `body` matches top-level fields;
  `docs/assistant.md` ("Lark: the skill shipped with the Lark extension")). lark-cli commands use `{"method":"CLI","path":"/im/+messages-send","action":"allow"}`
  (`path` joins command words with `/`, `*` / `**` wildcards as above); a match skips lark-cli's self-reported risk level.
- `connector_gate` (optional): gate rules for connector writes (such as `github_comment`); first match wins, no match means confirm each operation on the phone:
  `[{"connector":"github","action":"github_comment","match":{"repo":"example-org/*"},"decision":"allow"}]`
  (`connector`, `action` allow `*` wildcards; `match` matches top-level tool arguments, strings may also use wildcards; `decision` is `allow` / `confirm` / `deny`,
  an invalid list is reported in the manager's log at start; `server/internal/connector/gate.go`, `docs/plan-connectors.md`). Writes matching `confirm_always` in an extension manifest clamp even an `allow` rule to `confirm` (`docs/extensions.md`).
  The assistant's Lark write rules also go here: `connector` is `lark`, `action` is "method path", e.g. `{"connector":"lark","action":"POST /conversations/*/messages","decision":"allow"}`,
  or lark-cli's `{"connector":"lark","action":"CLI /im/+messages-send","decision":"allow"}` (`*` matches one path segment, `**` the rest, `"*"` any call; `match` matches top-level body fields).
  These precede the old `lark_gate`, followed by built-in rules (`docs/assistant.md` ("Lark: the skill shipped with the Lark extension")).
- `sent_ledger_days` (optional): how many days the ledger of what was written as the user is kept (`docs/connectors.md` "Sent as the user"); omitted or 0 means 90, below 0 keeps it for good.
- `github` (optional): GitHub connector. `{"auth": {"from_machine": "<machine id>"}}`: use `gh`'s login on that machine (the hub runs `gh auth token` via the node,
  keeping the token only in hub memory, never in the database or logs, or exposing it to the App or assistant). Keep `gh` signed in there, with the login in `~/.config/gh/hosts.yml`
  (keychain credentials cannot be read via ssh: `gh auth login --insecure-storage`). `auth.host` defaults to `github.com`; put a GitHub Enterprise host name here (not implemented yet).
  Check status under **Settings › Connectors › GitHub**. `from_machine` is only a default: every hub has GitHub, and under "Sign In From" in **Settings › Connectors › GitHub**
  you can switch to another computer's gh login, or use device-code login (the built-in GitHub CLI Client ID, no OAuth App registration needed; the authorization page shows "GitHub CLI", see `docs/connectors.md` ("II · 7"));
  once chosen in the App, that wins (stored in hub settings). The connector needs `repo` scope: gh's default login has it; otherwise the page suggests `gh auth refresh -s repo`.
  `github.proxy` (optional): HTTP proxy for all requests to GitHub (device-code creation and polling at `github.com`, API at `api.github.com`, plus CI logs),
  `http://[user:password@]host:port`, same format as `google.proxy`; omitted means direct (does not read process `HTTPS_PROXY`). Some networks reach only `api.github.com`, not `github.com`,
  so device-code login cannot obtain a code and needs this proxy. An invalid value does not fall back to direct: every GitHub request fails, and the settings page states that `github.proxy` in manager.json is invalid. Actual addresses belong only in manager.json.
  The extension market also uses it to read markets on GitHub and download extension packages from Releases (`github.com`, `*.github.com`, `*.githubusercontent.com`; when unset, uses process `HTTPS_PROXY`, #3277).
  `github.inbox` (optional) supplies default Inbox rules (App changes take priority, `docs/inbox.md` ("Noise reduction")): `ci` (`off` / `default_branch` / `mine` (default) / `all`),
  `muted_repos` (`["owner/name"]`), `ignore_branches` (branches not to notify for, `path.Match` patterns, default `["train/*"]`).
- `imessage` (optional, **old syntax, retained for one version, read-only**): now choose the Mac to read in **Settings › Extensions › Messages** in the App (#1830). If configured here, the manager migrates it once at startup;
  when the extension is off or no Mac is selected, it still reads here. Messages connector. `{"machine": "<machine id>"}`: which Mac's "Messages" appear in the App (that Mac's node reads `~/Library/Messages/chat.db` read-only,
  requiring Full Disk Access; **Settings › Connectors › Messages** explains how). Configure one machine only (Macs on the same Apple ID share the same messages).
  The App can show all chats. With "Hand to Persona" on (**Settings › Connectors › Messages**, on by default, stored in the hub), real chats (members include someone in Contacts or you have spoken there)
  incrementally enter the knowledge base and Inbox by ROWID (triaged like Lark); the assistant gets `imessage_chats` / `imessage_read` / `imessage_search`; verification codes are masked. Turning it off stops everything,
  and existing data on the hub can be deleted separately. Logs contain neither numbers nor bodies (`docs/plan-connectors.md` ("V")).
- `bots.feishu` (optional): application credentials for the bot used to chat with Persona in Lark: `app_id`, `app_secret`, `domain` (`feishu` / `lark` / open-platform address),
  for people who do not want to enter them in the App. Values saved in the App (**Settings › Connectors › Lark Bot**) win; switches, pairing, and all features are in the App (`docs/bots.md`).
- `extensions` (old `plugins` key still read): extensions hand-written in manager.json, started by the manager and forwarded to the App (voice chat, etc.): `id`, `name`, `kind`, `command` + `args` (or only `socket` for an extension running elsewhere),
  `dir` (working directory), `env` (extra environment variables), `disabled` (`true` disables while keeping the config), `config` (the extension's own settings), see `docs/extensions.md` ("Extensions in manager.json");
  extensions installed with `personad extension install` are registered in the database, not here. For personad's built-in voice extension (GPT-Live), use `"command": "jarvisd", "args": ["voice"]`,
  with upstreams in `config.providers` (`docs/extensions.md` ("Built-in voice extension")). Restart the hub after changes.
- `trusted_extension_markets` (optional): signing public keys of additional trusted extension markets (key id, or `ed25519:…` from `market.pub`); extensions installed from these markets default to no sandbox, just like the official market (`docs/extensions.md` ("Sandbox")).

<a id="ddnspublicddns"></a>

## DDNS (public.ddns)

Use a fixed domain to find the hub after reconnecting changes its IPv6 prefix (`docs/network.md`). Omit `public.ddns` to leave it disabled.

```json
"public": {
  "tls_listen": ":18662",
  "advertise": ["https://hub.example.com:18662"],
  "ddns": {"provider": "cloudflare", "record": "hub.example.com", "zone_id": "<zone id>", "token_file": "<token file on the hub machine>"}
}
```

- `provider`:
  - `cloudflare`: updates AAAA / A records, requires `zone_id` and `token_file`; `ttl` is 1 (automatic, default) or 30–86400.
  - `webhook`: POSTs `{"record","ipv6","ipv4"}` to `webhook_url`; with `token_file`, sends it as `Authorization: Bearer`.
  - Alibaba Cloud DNS and DNSPod are not implemented yet.
- `record`: full domain name.
- `token_file`: a file containing only the token, must be a regular file with 0600 permissions. The token does not go into hub.json.
- **Which addresses are published**:
  - IPv6 follows the same rules as the candidate list: only stable local global IPv6 addresses; an already published address is kept as long as it remains stable.
  - IPv4 is published only if a public IPv4 was explicitly entered in `public.advertise`: a locally visible IPv4 does not prove public inbound reachability.
- **When it updates**: once at startup, then whenever addresses change. Failures back off exponentially from 1 second, capped at 5 minutes. Logs record only the failure reason, not the token, URL, or response content.
- **It only updates DNS**: it neither opens a listener nor allows firewall traffic. To make the App use the domain, also put the HTTPS URL including its TLS port in `public.advertise`; only then does it appear in the candidate list and the QR code's `alt`.

## For developers

- When a new feature needs a value "specific to one person" (machine name, domain, account, email, path, launcher, internal company tool), add it to one of the two configs above, never to code,
  UI text, or tests. UI text says "the hub machine" and "your computers", not specific machines.
- Details of this dev machine (available machines, hub address, where accounts live) belong in `CLAUDE.local.md` at the repo root (uncommitted), not `CLAUDE.md`.
