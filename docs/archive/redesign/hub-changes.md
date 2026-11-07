# Hub change list

> Draft v1 (2026-10-30), part of the [redesign plan](README.md). Basis: [research/persona-protocol.md](research/persona-protocol.md).
> Principle: every change is **compatible with old Apps** (new fields optional, old field semantics unchanged); a new App meeting an old hub falls back to the old behavior based on capability flags.
> **iOS is read-only this round**: every change must keep the existing iOS App working without code changes, including plaintext LAN connections, the old pairing QR format, and a single address. After every hub change, run the UI tests and a real-device pairing with the existing iOS App.
> Migration numbers are allocated in ranges per lane (CLAUDE.md); before picking one, check `server/internal/db/migrations/` in all worktrees and `schema_migrations` in the `persona_test` database.

## 1. Public direct connection (required)

> Decided 2026-10-30: the phone must be able to connect directly to the hub over the public internet, probing and preferring IPv6; VPNs are configured by users themselves. Basis: [research/public-direct-ipv6.md](research/public-direct-ipv6.md) (state of IPv6 in China, address discovery, TLS, hardening, keepalive, with sources) and [research/hub-network-current.md](research/hub-network-current.md) (the hub's current network code, with line numbers).
> The original draft's "FCM" section was removed: we design for "the phone has no Google services" and don't do FCM ([README.md](README.md) D8).

**Current state**:
- The hub has a single plaintext listener (`listen`, default `:18660`) with no timeouts at all;
- authentication failures aren't rate-limited or logged by source; unknown tokens also go into the cache and are never cleaned up;
- the QR code carries the **never-expiring master token** and only one address (`public_url`);
- the docs say "only put it on the LAN or a VPN".

Go opens a dual-stack socket for the wildcard address, so IPv6 can already connect in; whether it's reachable from the internet depends on the modem, the router, and the hub machine's firewall.

**Overall rule for staying compatible with existing iOS**:
- Keep the plaintext port as is, rejecting plaintext only from **public sources**;
- keep the QR format (`url=` + `token=`), putting new information in extra parameters;
- connections that don't negotiate heartbeats keep today's 20 seconds;
- candidate addresses never go into unauthenticated endpoints.

### 1.1 TLS listener

- **Port**: open a new, configurable TLS port. Avoid 80 / 443 / 8080 / 8443 (some home broadband ISPs block these) and 18661 (used by Postgres). The plaintext `listen` stays, for the LAN, VPNs, and existing iOS.
  - Decided: two ports (README section 12, question 9). Same-port sniffing (go TLS if the first byte is `0x16`) is kept as a future option: users would only need to open one port on the router, at the cost of about 100 lines of hand-written accept loop.
- **Certificate**:
  - The hub's identity key is ECDSA P-256, 0600, stored in `~/.persona/tls/`, not in the database (CLAUDE.md: "Secrets never go into the repo").
  - It signs a self-signed certificate; when the certificate nears expiry, it re-signs with **the same key**, so the fingerprint doesn't change.
  - The fingerprint is the SHA-256 of the SPKI, printed by `personad hub pair` and included in the QR code.
  - Changing the key means every device rescans (decided); a planned rotation can announce the next key's fingerprint in advance in `hello` (`fp_next`).
- **With a domain (optional)**: use ACME DNS-01 to get a wildcard certificate from a public CA for the same key. A wildcard certificate keeps specific names out of CT logs where they'd get scanned. Clients still pin the public key.
- **No IP address certificates**: Let's Encrypt only started offering IP certificates officially in 2026-01, valid for just 6 days, and only with HTTP-01 / TLS-ALPN-01 validation, which needs 80 / 443 open.
- **Server parameters**: add `ReadHeaderTimeout` and `IdleTimeout` to both servers and shrink `MaxHeaderBytes`. Don't set an overall `ReadTimeout` / `WriteTimeout`; that would cut off WS and large file uploads.
- **Change surface**: new file `tlscert.go`; `Run()` in `hub.go` starts a second listener; `hub.json` gains `tls: {listen, cert_file?, key_file?}`. About 150–250 lines (estimate).

### 1.2 Address discovery and distribution

- **Discovery** (new file `endpoints.go`):
  - Read addresses with `IFA_FLAGS` via netlink. `/proc/net/if_inet6` can't see flags above 0x100.
  - Exclude temporary (privacy) addresses, deprecated addresses, ULA, link-local, documentation prefixes, and virtual interfaces like docker and TUN.
  - Subscribe to address changes, plus a full rescan every few minutes.
- **Candidate types**:

  | Type | Content | When published |
  | --- | --- | --- |
  | `ipv6` | Global, stable IPv6 | When detected |
  | `name` | DDNS name or the configured `public_url` / `advertise` | When configured |
  | `lan` | LAN IPv4 / ULA | When detected |
  | `ipv4` | Public IPv4 | Only once inbound reachability is confirmed: query the WAN port via UPnP / NAT-PMP / PCP, plus STUN or an HTTPS echo, to tell whether there's carrier-grade NAT |

  Each entry carries port, protocol (`https` / `http`), and `seen_at`.
- **The hub can't test reachability by itself**: connecting to its own address from the same machine goes over loopback and never passes through the modem. It takes a phone probing on cellular, or a node away from home helping out.
- **Distribution**:
  - authenticated `GET /api/v1/endpoints`;
  - new fields in the WS `hello` (`hub_id`, `endpoints`, `tls_port`, `fp`). Existing iOS first decodes each WS message whole into one `Envelope` (`ios/Shared/LiveConnection.swift`); if a new field name collides with one of its existing keys with a different type (`id`, `state`, `data`, `name`, `error`, `text` and others are strings), the whole `hello` fails to decode and is dropped. None of the four above collide (checked); check this table before adding fields in the future too;
  - on address changes, push `{"type":"endpoints"}`, only to connections that declared the `endpoints` capability in `client_hello`;
  - `features` gains `endpoints`, `tls`, `heartbeat`, `app_ping`.
- **The unauthenticated `/api/v1/health`** only gains non-sensitive fields (`hub_id`, TLS port, `fp`); **candidate addresses don't go here**, so anyone who scans the port can't get the home's other addresses. It currently also exposes the version and capability flags: to public sources it returns only `ok`, `hub_id`, the TLS port and fingerprint; LAN sources are unchanged, since existing iOS reads it on the LAN (decided, [README.md](README.md) section 12, question 9).
- **TUN proxy pitfall** (not measured): when the hub machine runs a TUN proxy with auto-routing, return packets for IPv6 connections coming in from outside may be routed into the proxy, and the connection never gets established. Test with a phone on cellular before deploying; `hub doctor` (1.8) should check this too.

### 1.3 Pairing: QR codes issue short-lived temporary tokens (format unchanged)

- `personad hub pair` no longer prints the master token by default. It calls the running hub over local loopback to generate a **high-entropy, short-lived (e.g. 10 minutes), single-use** temporary token, stored in the database (deployments wipe the hub's in-memory state, so persistence is required). The QR format stays the same:

  `persona://pair?url=<LAN plaintext address>&token=<temporary token>&hub=<hub_id>&fp=<fingerprint>&tls=<TLS port>&alt=<one or two external candidates>`

- **Parameter format** (all platforms implement this):
  - `hub`: hub instance id (1.5);
  - `fp`: certificate public-key pin: SHA-256 of the certificate's SubjectPublicKeyInfo (DER), encoded as base64url (RFC 4648 §5) without padding, 43 characters;
  - `tls`: TLS port (decimal);
  - `alt`: a complete `https://` address, occurring once or twice, best first;
  - `url` and `token` retain their meanings.
- **Why this works with existing iOS**: iOS's `ServerConfig(pairURL:)` reads only `url` and `token` by name and ignores other parameters. During pairing it first calls `GET /machines`, then connects WS and registers push, and finally exchanges for its own device token with `POST /auth/tokens`. The temporary token must therefore work as a full Bearer during its lifetime; after exchange for a device token, allow a brief grace period before invalidating it.
- New Apps (Android) read the extra parameters: pin `fp`, record `hub_id`, and add `alt` to the candidates. Include only one or two external candidates in the QR code so it doesn't get too dense; fetch the rest from `/endpoints` after pairing.
- Already-paired devices without a fingerprint (Android from PR #84): fetch the fingerprint once over an authenticated LAN channel (`hello` or `/endpoints`) and pin it. This has the same trust level as today's plaintext LAN connection (decided, README section 12, question 9).
- `--legacy` still prints the master token for CLI use and special cases.
- **Once public direct connection is enabled, the master token accepts only loopback and LAN sources**.
- Any `full` device token can currently issue new tokens: send the user a "new device paired" notification during pairing so unexpected activity is visible.

### 1.4 Hardening

- **Reject plaintext from public sources**:
  - Requests from public addresses on the plaintext listener return `403 {"code":"tls_required"}`; only `/api/v1/health` is allowed so clients can say "please use https".
  - Classifying "public" must consider which local interface the connection entered through, not just the address: 100.64/10 is both Tailscale's range and a carrier-grade NAT range. Allow a `trusted_cidrs` allowlist.
  - Users already accessing the hub over public plaintext lose access after upgrading; keep `allow_public_plaintext` (off by default), with warnings in `hub doctor` and logs.
  - Note: the token travels with the plaintext request and is already exposed by the time the hub rejects it. The real protection belongs in the client: **never send a token over `http://` to a public address**.
- **Rate-limit authentication failures**:
  - Count per IPv4 address and **per /64 IPv6 prefix**; limiting individual IPv6 addresses is effectively no limit;
  - apply the limit before querying the database; over the limit, return `429` + `Retry-After`;
  - don't count LAN and VPN sources, so devices in one home network don't penalize each other;
  - cap the cache of unknown tokens;
  - log WS authentication failures too (currently not logged at all), mask sources, and rate-limit the logs themselves.
- **Host firewall as the backstop**: most modems only have an all-or-nothing IPv6 firewall switch, so disabling it exposes the entire home network; Docker-published ports also bypass ufw. Docs must say to allow only the TLS port on the hub machine.
- Use a random IPv6 interface identifier, not an easy-to-guess suffix like `::1` (explain this to users in the docs).

### 1.5 Hub instance id

After `Run()` opens the database, read `hub_id` from the `settings` table; if absent, generate a UUID and write it back. No new migration needed. Send it in `health`, `hello`, and the pairing QR code. Clients use it to confirm they're connected to the same hub (the old prefix may have been reassigned to someone else), and to scope local data ([README.md](README.md) D6). The original section 3.5 is merged here.

### 1.6 Heartbeats negotiated per connection

- **Why the hub must initiate them**: during Android deep sleep, OkHttp's client ping doesn't fire (its timer uses `System.nanoTime`, which stops in deep sleep); background alarms (`AllowWhileIdle`) run at most once every 9 minutes. The hub must send the heartbeat for a persistent background connection, with the interval negotiated per connection.
- **Protocol**:
  - After connecting, the client includes `heartbeat: {interval, pong_timeout}` in `client_hello` (3.3);
  - the hub adjusts this connection's WS ping interval, pong timeout, and TCP keepalive, and returns a `client_hello_ack` specifying the effective values. The hub sends `hello` immediately upon connection, before `client_hello`, so the effective values need a separate reply.
- **Values**:
  - Start at **4 minutes** in the background, below the known carrier NAT timeout of about 5 minutes (that data comes from 2G/3G measurements around 2015; 4G/5G values **weren't found**); adjust automatically for the network, with jitter only downward.
  - Foreground: 20 seconds, plus application-layer ping (3.8) to assess weak networks.
  - Old connections without negotiation (existing iOS) keep 20 seconds / 15 seconds unchanged.
- **Change surface**: concentrated in `http.go` (`client` struct, the write goroutine's ticker, `onClientMessage`), about 50 lines (estimate). Battery cost and carrier idle timeouts both need real measurements ([android.md](android.md) section 10).

### 1.7 DDNS (optional, recommended)

- **Why recommended**: reconnecting the broadband session changes both IPv4 and the IPv6 prefix, immediately breaking old connections. Android without FCM can't receive the new address then; some regions change prefixes twice a day. So "no domain" is only a degraded mode.
- **Supported APIs**: Cloudflare, Alibaba Cloud DNS, DNSPod, or a generic callback URL.
- **Publishing rules**: publish only stable addresses selected by the rules in 1.2; publish A records only after external reachability is confirmed; share the DNS token with ACME, restricting permissions to one zone.
- **How phones use it**: the free tiers of the two Chinese DNS providers have a minimum TTL of 600 seconds, so phones prefer literal addresses sent by the hub and use the DNS name only to find the hub again.

### 1.8 `personad hub doctor`

One self-check command that lists:
- listening ports;
- whether the host firewall allows them;
- TUN proxy risks;
- currently published candidate addresses;
- certificate fingerprint and expiry.

It also asks an external point (a node away from home, or a phone) to test reachability and gives actionable conclusions, e.g. "IPv6 inbound traffic is blocked; allow the TLS port on the modem or router".

### 1.9 Documentation

- Add `docs/operations/network.md` for public direct connection: TLS port and certificates, reading and changing fingerprints, allowing IPv6 on the modem and router, DDNS, TUN proxy return-packet problems, reverse proxies making source addresses appear local, and rate-limit parameters.
- Replace "LAN / VPN only" and "HTTP by default" in README, `docs/operations/config.md`, `docs/server/server.md`, `docs/protocol/protocol.md`, and `docs/server/architecture.md`. The "HTTPS" label in the `docs/server/architecture.md` diagram is already inaccurate. Locations are listed in research/hub-network-current.md section 7.
- Record this user requirement under "Conventions" in CLAUDE.md.

## 2. Required (first Android release)

### 2.1 Register and route phone tools per device

- Currently `register_phone` replaces all of `h.phoneTools` and the `phone_tools` table (`hub.go`), and calls go to the "prefer real devices, most recently registered" phone (`phoneClient` in `http.go`). Two online phones overwrite each other, so the destination is uncertain. Android also lacks `phone_reminders_*`, so routing such a call to Android returns "unknown tool".
- Change:
  - `register_phone` includes `device_id` and `platform`;
  - store the tool table per device (new migration: add `device_id` to `phone_tools`);
  - `mcpTools()` merges and dedups (for the same tool name, use the description with the most capabilities);
  - `callPhone` chooses a device in this order: has the tool → online in the foreground → most recently active → wake a device with the tool.
- Revoking device credentials also removes its push registration and tool registration (part of optional item 4.5).

### 2.2 Android package hosting and update checks

There is no Play Store; the hub hosts the Android package ([android.md](android.md) section 9).

- **Storage**: APK and `latest.json` under `~/.persona/apps/android/` (`version_code`, `version_name`, `sha256`, `size`, `min_sdk`, `notes`). After signing the APK, the release script uploads it via SSH; it doesn't go into the database.
- **Endpoints** (all authenticated, through the TLS port or LAN):
  - `GET /api/v1/app/android/latest`: returns `latest.json`;
  - `GET /api/v1/app/android/apk`: download, supports Range (resuming).
- **First install**: a phone without the App has no device token. `personad hub pair --android` prints a LAN download link beside the QR code, carrying the same temporary token (10 minutes); open it in the phone's browser to download, install, then scan the pairing QR code. adb or other APK transfer methods also work.
- Capability flag: add `android_update` to `features`.

## 3. Recommended (reliability and compatibility, alongside Android)

### 3.1 Persist operation receipts (`client_ops`)

- New migration, `client_ops` table:

  | Field | Description |
  | --- | --- |
  | `id` | Client-generated UUID |
  | `device_id` | Initiating device |
  | `kind` | `message` / `resolve` / `interrupt` / `rename` / `close` / `modes` / `stop_loop` / `clear` / `lark_send` / `inbox_reply` / `mail_send` |
  | `target` | Target JSON |
  | `digest` | `sha256(kind + normalized parameters)` |
  | `state` | `pending` / `done` / `failed` / `unknown` |
  | `result` | JSON |
  | `created_at`, `updated_at` | |

- Semantics (following an internal host service's `once()`, but persisted in Postgres):
  - Same id, same digest, completed → return the original result or error, **don't execute again**;
  - same id, same digest, still `pending` → wait for completion (bounded); on timeout return `202 {state:"pending"}`;
  - same id, different digest → `409 {code:"op_id_reused"}`;
  - only definite errors that **prove nothing was executed** (`input_draft`, `dialog_open`, `invalid`, `stale_session`…) are recorded as completed failures; **other exceptions stay `unknown`**.
- New endpoint `GET /api/v1/ops/{id}`: read-only query for clients when the result is unknown.
- A message's `client_msg_id` is its op id. node's in-memory dedup (`server/internal/node/sendsafe.go`, 10 minutes) stays to block retries between hub and node; this hub layer blocks retries across deploys, restarts, and intervals beyond 10 minutes.
- `delivered: unknown` (node errors halfway through typing): the hub records `unknown` and searches the transcript for the same message (text + time window) as **positive evidence**; if found, change to `done`. If not found, let the client show "Needs verification" ([ux.md](ux.md) 5.2).
- Cleanup: retain for 30 days.
- Capability flag: add `client_ops` to `features`.

### 3.2 Idempotent Lark sends and page completeness

- `lark.Send` (`server/internal/lark/lark.go`) currently generates a random clientId each time. Change it to accept the caller's clientId:
  - sends from a chat page: generated by the phone;
  - Inbox replies, AI twin sends, assistant tool `lark_send`: generated and persisted by the hub per item or tool call.
- The Lark gateway already dedups by clientId (an already-sent request returns the original message id).
- Message projections (`lark.go`) pass through `clientId` and `localState`, letting the App remove its own outgoing bubbles precisely.
- Paginated results pass through upstream `syncState` (`complete` / `local` / `gap`) instead of dropping it. The App deletes local rows only for `complete`.

### 3.3 Clients report their capabilities (two-way negotiation)

- WS: after connecting, the client sends `{"type":"client_hello","device_id","platform","version","build","os":{"name","version","sdk"},"features":[…],"heartbeat":{…}}` (heartbeat negotiation in 1.6). `os` is the detected system (HyperOS, ColorOS, HarmonyOS 4…) and version; aggregated by the hub, it shows which vendor/version's Settings deep links have broken ([android.md](android.md) 5.18).
- HTTP: include the header `X-Persona-Client: <platform>/<version>`.
- The hub records it on the connection for:
  - platform-based routing of push and phone tools;
  - fallback screen cards for clients that don't know a new interaction type (e.g. `elicitation`);
  - log-based troubleshooting.
- Old clients send no `client_hello` and are treated as an old iOS version.

### 3.4 Unified error body

- `{"error":"human-readable message","code":"machine code","retryable":true|false,"delivered":"none|unknown|yes"}`, retaining the existing `{error}` and flattened 409 fields (`http.go`).
- Main API 404 / 405 responses are JSON too. They're currently Go's default plain text; only mail subroutes rewrite them as JSON.
- Clients key only on `code`.

### 3.5 Hub instance id

Merged into 1.5.

### 3.6 Android branch of Fleet (Lock Screen summary)

**No change needed**: Android uses only the persistent connection, and the hub already broadcasts `fleet` events over WS (`liveactivity.go`); the foreground service uses them directly to update its persistent notification. The draft's "send FCM `type=fleet` by device type" is not being done.

### 3.7 Interaction source information and new types

- Sub-Agent source: pass `agent_id` / `agent_type` from hook input through to Interaction when present.
- MCP source: when PermissionRequest targets an `mcp__*` tool, include server name and configuration source. Omit when unavailable; clients show "Not provided".
- New interaction type `elicitation` (MCP forms / links):
  - node handles Claude Code's Elicitation hook;
  - Resolution gains `action: accept|decline|cancel` and `content`;
  - send only to clients declaring `elicitation`; others fall back to "handle in the terminal".
- Plan approval: keep the existing `mode`. The App unifies its approval policy table; no hub change needed.

### 3.8 Application-layer ping / pong (for Android weak-network detection)

- OkHttp's WebSocket heartbeat is fully automatic: no manual ping API, no pong callback, no round-trip time. iOS assesses weak networks by "did pong return within 5 seconds"; Android can't do that.
- Add a client-initiated JSON heartbeat: the App sends `{"type":"ping","id":"<n>"}`, and the hub echoes `{"type":"pong","id":"<n>"}`. Use a string `id`: iOS's `Envelope` has a string `id` (1.2). Both clients time by id and use the same weak-network rules as iOS ([ux.md](ux.md) 5.5). Keep protocol-level ping to detect dead connections.
- Capability flag: add `app_ping` to `features`. With an old hub lacking this capability, Android only knows once the connection breaks.

### 3.9 Notification events sent over WS to persistently connected devices

- In PR #84 the phone decides "done" itself: when it sees "Working → Unread" while the App isn't foregrounded, it notifies. This differs from the hub's push policy for iPhone. The hub policy (`notify.go`) is:
  - wait `done_delay_seconds`;
  - notify only for long tasks;
  - notify only when a background task ends;
  - don't notify if already seen on the computer or in the App;
  - don't notify if muted;
  - don't notify if the assistant just called `notify_user`.
- Change: at the same point where the hub decides to send APNs to iPhone, send connected devices declaring `ws_notify`
  `{"type":"notify","kind":"done|interaction|assistant","machine_id","session_id","interaction_id","title","subtitle","body"}`. Device notification preferences (`prefs.input`, `prefs.done`) still apply. The phone only displays them; it no longer computes the policy.
- Interaction notifications still use the existing `interaction` / `interaction_resolved` events (Android posts a notification on receipt and withdraws it when resolved); no changes needed.
- Share the same "what to send, to whom" decision as APNs; only the delivery channel differs.

### 3.10 Device connection history (for Android background diagnostics)

- Chinese Android systems have no API to "check autostart / background allowlisting"; whether settings are correct can only be judged by the result: how long the persistent connection was down. The hub already knows when each WS connects and disconnects; record it.
- New migration, `device_sessions` table: `device_id`, `connected_at`, `disconnected_at`, disconnect reason (pong timeout / peer closed / replaced by a new connection / hub restart), source prefix (masked), negotiated heartbeat interval. Retain 14 days.
- Other Agents may redeploy the hub at any time (CLAUDE.md), so persist this; mark disconnects caused by hub restarts separately rather than blaming the phone.
- New endpoint `GET /api/v1/devices/{id}/sessions?since=`: the diagnostics page shows "N background disconnects in the past 24 hours, longest X minutes"; A1's overnight survival tests use it too ([android.md](android.md) 5.9, section 8).
- Record only times and reasons, not content.

## 4. Optional

| # | Change | Affects |
| --- | --- | --- |
| 4.1 | ~~One-time pairing code~~: replaced by 1.3's "short-lived temporary token, QR format unchanged", compatible with existing iOS | — |
| 4.2 | **Platform-neutral copy**: replace hardcoded "iPhone" in tool failure messages, the assistant's CLAUDE.md, live snapshots, and the `ask_user` tool description with "phone" or the online phone's platform | `hub.go`, `assistant.go`, `assistant_snapshot.go`, `assistant_tools.go` |
| 4.3 | **Withdraw notifications after interactions are resolved**: `Pusher.clear` is currently empty. Send Android a `NORMAL` `type=interaction_resolved`; the App cancels the matching notification | `push.go`, `hub.go` |
| 4.4 | ~~Tool calls directly in push~~: not doing this. Android's persistent connection needs no wakeup; revisit when iOS follows later | — |
| 4.5 | **Associate device credentials with push devices**: `device_tokens` records platform and push `device_id`; clean both up on revocation | `devicetokens.go`, `db.go`, migration |
| 4.6 | ~~Gmail sign-in on Android~~: not doing this. Google's sign-in API depends on Google Play services; mail accounts live on the hub and are shared across clients, so add them on iPhone or use an app password with ordinary IMAP | — |
| 4.7 | ~~UnifiedPush~~: not doing this. Android uses only the persistent connection | — |
| 4.8 | **Target instance fingerprint**: writes targeting an Agent include "machine + pane + process start identity + session id"; node checks before execution and returns `stale_session` on mismatch | `node/*`, `proto` |
| 4.9 | **Full commands in screen cards**: when herdr reports blocked and the transcript contains an unfinished tool_use, attach the full tool_input to the screen card rather than showing truncated terminal text | `server/internal/screen`, `node` |
| 4.10 | **Move prompt templates to the hub**: "Hand to Persona", "Lark summary", and "Stop loop" prompts currently live in the clients (`KnowledgeView`, `LarkChatView`, `AppModel`); move them to the hub so both clients don't maintain copies | New endpoint or assistant config |
| 4.11 | **Full-text session search**: `GET /api/v1/sessions/{m}/{s}/search?q=`; node searches the transcript and returns seq and excerpt, and the client keeps loading backward to that point | `node`, `transcript` |
| 4.12 | **Align docs**: add missing endpoints (`queue`, `info`, `seen`, `file`, knowledge base, `lark/raw`, etc.) and events (`assistant_schedule`, `hook_event`, `error`, client-sent `seen`) to `docs/protocol/protocol.md`; remove `session` / `ping` / `pong` absent from code; check `docs/server/architecture.md`'s claim "hooks are immediately allowed when no App is online" (no such logic found in code) | Docs |
| 4.13 | **Export protocol samples**: Go tests write representative production JSON (sessions, every items type, four interaction types, Fleet, Inbox, Lark messages) to `spec/fixtures/wire/` (`-update` refreshes); both clients' decode tests read the same set | `server/internal/proto` tests |
| 4.14 | **Align mock hub**: validate `scripts/mock/mock_hub.py` response formats in Go tests; add fault switches for "executed but receipt lost", "address changed", "certificate mismatch", and a TLS listener | `scripts/mock/mock_hub.py`, Go tests |
| 4.15 | **Upload crash logs**: no Firebase. The App stores `ApplicationExitInfo` and uncaught exceptions locally, then calls `POST /api/v1/devices/{id}/crashes` on next launch; the hub persists them and Settings can show them; shared when iOS follows later | New endpoint, migration |

## 5. No changes needed

Sessions, items, interactions, terminal, files, Inbox, Lark, mail (except Gmail sign-in), knowledge base, monitoring, pins / mutes, and per-device notification settings are all platform-independent JSON that Android can use directly. Reuse the `prefs.island` structure unchanged for "what the status bar capsule shows".

## 6. Order

1. Do section 1 (public direct connection) and 3.3, 3.4, 3.8 in the specification phase (P0): Android restructuring and public direct connection both need them. Verify each step leaves the existing iOS App unaffected.
2. Do 2.1, 2.2, and 3.9 with the first Android release (A2): 2.1 prevents overwriting iPhone phone tools, 2.2 lets testers upgrade in the App, and 3.9 makes "done" consistent with iPhone.
3. Do 3.10 with Android A1: overnight survival measurements need it.
4. Do 3.1 and 3.2 with Android's outbox rework (A3).
5. 4.x as needed.
