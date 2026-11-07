# Current hub networking audit (direct public connectivity / IPv6 priority / TLS / pairing)

> Subject: Persona worktree (master, after ec11652), read-only audit.
> Labels: **[Fact]** = directly from code / docs, with `relative path:line`; **[Assessment]** = inference or recommendation based on facts.
> Privacy: no real hostnames, IPs, domains, accounts, or tokens; placeholders only (`<hub address>`, `<token>`).

> Paths: the first reference in a section uses the full path, later references use filenames. `hub.go`, `config.go`, `http.go`, `devicetokens.go`, `terminal.go`, and `push.go` are under `server/internal/hub/`. `APIClient.swift`, `LiveConnection.swift`, `Keychain.swift`, and `WatchSync.swift` are under `ios/Shared/`. Android `data/`, `ui/`, and `service/` are under `android/app/src/main/java/persona/android/`.
> Status: complete (sections 1→9 in order; section 0 is the summary).

## 0. Summary

**Current state [Fact]**
- Listener: `listen` in `hub.json` defaults to `:18660`, with a single plaintext `http.Server` (`server/internal/hub/hub.go:189-197`), no TLS, timeouts, or header-size settings (Go defaults: no timeouts, 1 MB headers). Go wildcard listeners use a dual-stack socket, accepting IPv4 + IPv6 by default (not measured on the hub machine). `public_url` (default `http://<hostname>:18660`) appears only in QR codes and startup logs, not responses.
- Pairing: `personad hub pair` is a separate process that reads hub.json and prints `public_url`, the **main token**, and QR `persona://pair?token=…&url=…` (`server/internal/hub/config.go:149-162`). Main token never expires; no rotation command. Device `jd_…` tokens store only sha256, are revocable, and have a 30-second lookup cache (`server/internal/hub/devicetokens.go:57`).
- Authentication: `Bearer` header or `?token=` on **any `/api/` route** (neither client uses it). Failures have no rate limiting or source-IP logging; `/api/v1/ws` failures have no logs at all. Unknown tokens are cached too, and the cache is never cleaned.
- health: `{ok, version, features}`, unauthenticated. hello: `{server_version, client_id, features}`, no `client_hello`. Hub pings every 20 seconds (hardcoded 15-second timeout). Each connection has a 512-message queue: full direct-send queues disconnect; full broadcast queues **silently drop messages**.
- iOS: Keychain stores `{url, token}`; three URLSessions have no delegates (cannot pin); `NSAllowsArbitraryLoads` on phone + Watch. Watch tokens are in Watch `UserDefaults`. Intents, notification buttons, and Live Activity buttons independently read Keychain and connect directly. `persona://pair` links pair without confirmation.
- Android: plaintext `{url, token}` in `SharedPreferences`; REST and WS share one OkHttp client (`pingInterval` 20 seconds); `network_security.xml` allows plaintext globally; `health()` exists but has no callers.

**Changes [Assessment]** (line counts are estimates, not measurements)
- 4a TLS: new `tlscert.go`, ECDSA P-256 self-signed certificate under `~/.persona/tls/` (key 0600), SPKI pinning. Add a TLS listener to `Run()`, or sniff one port. Keep plaintext for private-network / VPN only. About 150–250 lines.
- 4b Multiple addresses: new `endpoints.go`, discover global unicast IPv6 (exclude ULA, link-local, temporary, TUN), plus configured `advertise`. Send via hello, `GET /endpoints`, QR, and broadcast `endpoints` on change. health includes only `hub_id`. About 150–250 lines.
- 4c Pairing codes: new `pair_codes` table. `hub pair` calls the running hub over loopback to generate codes; unauthenticated `POST /auth/pair` exchanges them, with mandatory rate limiting; `--legacy` for old Apps. About 200 lines.
- 4d Reject public plaintext + rate limiting: classify by `RemoteAddr`, limit IPv6 by /64 before database lookup, log WS authentication failures, bound the negative cache, add `ReadHeaderTimeout`. About 150 lines.
- 4e `client_hello` heartbeat negotiation: only `http.go`, about 40–60 lines.
- 4f `hub_id`: stored in `settings`, no migration, about 20 lines.
- Clients: unify into one session with a pinning delegate; concurrent candidate probes (IPv6 first); download before AVPlayer playback; Android custom `X509TrustManager` (`pin-set` and `CertificatePinner` cannot bypass self-signed trust rejection).

## 1. How the hub listens

### 1.1 Configuration sources [Fact]

| Item | Location | Current behavior |
| --- | --- | --- |
| Listening address | `server/internal/hub/config.go:50` (`Config.Listen`, json `listen`) | Default `":18660"` (`config.go:90`) |
| Advertised address | `config.go:52` (`Config.PublicURL`, json `public_url`) | Default `fmt.Sprintf("http://%s:18660", host)`, with the **full** `os.Hostname()` as `host` (`config.go:87-92`) |
| Main token | `config.go:51` (json `token`) | Default `randomHex(24)` = 48 hex digits (`config.go:91`, `config.go:142-146`) |
| Configuration file | `~/.persona/hub.json` | Directory 0700 (`config.go:101`), defaults written on first run, file 0600 (`config.go:108-112`). If an existing hub.json lacks `token` (`scripts/db.sh` writes only `database_url`), write the generated token back (`config.go:119-130`). |

- Server-side `public_url` has **only three uses**: startup log `listening on %s (%s)` (`server/internal/hub/hub.go:196`), QR `PairURL()` (`config.go:149-151`), and `hub pair` output (`config.go:158`).
  It **never appears in HTTP / WS responses** or push. zoekt searches for `public_url` / `PublicURL` match only these locations + tests + docs.
- Meaning: "hub address for the phone", one string, one scheme (currently always `http://`).

### 1.2 HTTP server [Fact]

- `srv := &http.Server{Addr: cfg.Listen, Handler: h.routes()}` (`hub.go:189`), `srv.ListenAndServe()` (`hub.go:197`); `Shutdown` on ctx completion, with a 5-second timeout (`hub.go:190-195`).
- **No TLS**: no `crypto/tls`, `ListenAndServeTLS`, or certificate code in the hub package (zoekt `crypto/tls` matches only `server/internal/mail/*`).
- **No timeout / header-size settings**: `ReadTimeout`, `ReadHeaderTimeout`, `WriteTimeout`, `IdleTimeout`, `MaxHeaderBytes` all unset (zoekt `ReadHeaderTimeout` / `MaxHeaderBytes`: zero matches).
  Go defaults: no timeout, `http.DefaultMaxHeaderBytes` = 1 MB.
- **Client addresses unused**: zero `RemoteAddr` or `X-Forwarded-*` matches across `server/`, no source-IP logic.
- Only this TCP listener; node `~/.persona/node.sock` is a Unix socket (`server/internal/node/hooks.go:80`), unrelated to phones.

### 1.3 Dual stack [Fact + Assessment]

- [Fact] One `Listen` string, one listener.
- [Fact, Go standard library] `net.Listen("tcp", ":18660")`: an empty or unspecified host (`0.0.0.0` / `::`) on systems supporting IPv4-mapped addresses (Linux / macOS) creates an `AF_INET6` socket with `IPV6_V6ONLY=0` (`favoriteAddrFamily` in `net/ipsock_posix.go`). The default is **already dual-stack**, with IPv4 connections represented as `::ffff:a.b.c.d`.
  A specific address (for example, private IPv4) uses only that family. **Not tested with `ss -ltnp` on the hub machine.**
- [Assessment] No need to change dual-stack listening itself. Direct public IPv6 reachability depends on the hub host firewall (`ip6tables` / `nft`) and router / ONT inbound IPv6 policy (many home ONTs block by default). These are outside code and need clear documentation.

### 1.4 Routing and authentication entry points [Fact]

- Outer mux registers only two entries (`server/internal/hub/http.go:19-23`, `http.go:159-160`):
  - `GET /api/v1/health`: **unauthenticated**;
  - `/api/`: all pass through `h.auth`. Other paths use Go's default plaintext 404.
- Outermost `logRequests` (`http.go:160`, `http.go:199-210`) logs only ≥400 or >2-second requests, format `[<id>] method path -> status (duration)`, **without query, body, token, or source IP**.
  **All `/api/v1/ws` requests are excluded** (`http.go:206`), so WS 401s leave no logs.

### 1.5 Request-body / message-size limits [Fact]

| Location | Limit | Code |
| --- | --- | --- |
| `proxyBody` (POSTs forwarding messages, keys, renames, etc. to nodes) | 32 MB | `http.go:371` |
| Assistant endpoints | 32 MB | `server/internal/hub/assistant.go:297` |
| Small AI features | 64 KB | `server/internal/hub/ai.go:43` |
| Knowledge-base updates | 1 MB | `server/internal/hub/kb.go:664` |
| Mail endpoints | 1 MB | `server/internal/mail/http.go:125` |
| File upload | `Content-Length` ≤200 MB (`proto.MaxUploadBytes`), missing length returns 411 | `server/internal/hub/upload.go:23-34`, `upload.go:62`, `server/internal/proto/proto.go` (`MaxUploadBytes = 200 << 20`) |
| Single WS read | 64 MB | `http.go:571` |
| Unlimited JSON decoding | resolve (`http.go:90`), `/devices` (`http.go:106`), prefs (`http.go:122`), new session (`http.go:415`), `/auth/tokens` (`devicetokens.go:133`) | All after authentication |

[Assessment] Missing limits after authentication are less concerning. Public exposure most urgently needs **pre-authentication** `ReadHeaderTimeout` (slow-client attacks), smaller `MaxHeaderBytes`, and `IdleTimeout`. Do not set global `ReadTimeout` / `WriteTimeout` for WS and uploads (breaks persistent connections and large files); use only `ReadHeaderTimeout`, or per-route deadlines with `http.ResponseController`.

## 2. Pairing and authentication

### 2.1 `personad hub pair` [Fact]

- Entry: `server/cmd/personad/main.go:52-53` → `hub.Pair()` (`server/internal/hub/config.go:153-162`).
- `Pair()` **does not connect to the running hub**. It independently calls `LoadConfig()` on `~/.persona/hub.json` (creates defaults if missing, `config.go:108-112`: a side effect), then prints:
  ```
  Server: <public_url>
  Token:  <main token>

  用 Persona App 扫码配对：
  <terminal QR code>
  ```
  The Chinese line means "Scan with the Persona App to pair." (`config.go:158`); QR uses `qrterminal.GenerateHalfBlock(cfg.PairURL(), qrterminal.L, os.Stdout)`, error correction L (`config.go:159`).
- QR content: `"persona://pair?" + url.Values{"url": {PublicURL}, "token": {Token}}.Encode()` (`config.go:149-151`), namely
  `persona://pair?token=<main token>&url=<percent-encoded public_url>` (`Values.Encode` sorts by key). **Only one address: `public_url`**, no local-address discovery.
- Tests: `server/internal/hub/hub_test.go:164-177` (special-character encoding), `hub_test.go:33-34` (default public_url contains `:18660`).

### 2.2 Main token [Fact]

- Permanent: no expiry or rotation command; manually edit hub.json and restart. Comparison uses `subtle.ConstantTimeCompare` (`server/internal/hub/devicetokens.go:69`), role `full`, empty `ID`.
- It is the token in QR and `hub pair` output. Apps exchange for device tokens after pairing and stop storing the main token (5.1 / 6.1), but the main token itself remains valid.

### 2.3 Device tokens [Fact]

| Action | Endpoint | Location | Details |
| --- | --- | --- | --- |
| Issue | `POST /api/v1/auth/tokens {name, kind}` | `devicetokens.go:128-168` | Caller must be `full` (main or phone token). `kind=watch` → `answer`; `phone` → `full`; others stored as `other`, `full`. token = `"jd_" + randomHex(24)` (`:160`), only sha256 in database (`:59-62`, `:162`); id = `dev_` + 6 bytes of hex; returns `{id, token, role, name}`. |
| List | `GET /api/v1/auth/tokens` | `:169-179` | `{tokens, current}` (`current` is caller's own id) |
| Revoke | `DELETE /api/v1/auth/tokens/{id}` | `:180-197` | Mark revoked in database → `revoked()` (`:200-222`) clears cache and `CloseNow`s all this device's WS connections. |
| Lookup | `authenticate()` | `:65-109` | Compare main token first, then query by hash; **cache results 30 seconds** (`tokenCacheFor`, `:57`, `:80`), write `last_seen` at most every 5 minutes (`:99-107`). |
| Permissions | `allowed()` | `:113-124` | `full`: all. `answer`: GET/HEAD + writes in `answerWrites` regex (resolve, messaging, seen, interrupt, Inbox done/reply/draft/refresh, `devices`). |

[Assessment] Any `full` device token can issue new tokens and revoke others (all devices except the main token). After public exposure, one leaked phone token can perpetuate access by issuing new tokens. When implementing one-time pairing codes, consider limiting issuance of `full` tokens to main tokens / pairing codes.

### 2.4 Authentication failures: no rate limiting, almost no logs [Fact]

- `h.auth` (`server/internal/hub/http.go:163-184`): `Authorization: Bearer`, falling back to **`?token=`** (`:165-168`). Unknown → `401 {"error":"invalid token"}` (`:169-172`); `answer` exceeding permissions →403 (`:174-177`).
- **No rate limiting, lockout, or backoff**: zoekt `rate.Limit` / `golang.org/x/time` across `server/`: zero matches; no dependency in `go.mod`.
- **No source logging**: zero `RemoteAddr` matches. `logRequests` records only `[<id>] GET /api/v1/... -> 401 (duration)`; **WS failures entirely excluded** (`http.go:206`).
- **Unbounded negative cache** [Fact + Assessment]: unknown database tokens are cached with `c.p == nil` in `devtok.cache` (`devicetokens.go:85-94`). Entries are deleted only on revocation (`:203-208`), with no expired-entry cleanup. Random-token flooding from the Internet costs one Postgres query and a permanent map entry per new token: memory only grows. Rate-limit **before** database lookup, and bound or periodically clean the cache.

### 2.5 `?token=` query parameter [Fact]

- Server: applies to **all** `/api/` routes, not just WS (`http.go:165-168`). `docs/protocol/protocol.md:7` says "WebSocket also supports `?token=`."
- **No current clients use it**:
  - iOS REST, uploads, images, full text, Lark docs, and WS all use `Authorization` (`ios/Shared/APIClient.swift:116`, `:150`, `:278`, `:291`; `ios/Shared/LiveConnection.swift:236`; `ios/Persona/Views/Feishu/Docs/FeishuDocsComponents.swift:12`). Video uses `AVURLAssetHTTPHeaderFieldsKey` (`ios/Persona/Views/Inbox/Lark/LarkMedia.swift:299`).
  - Android REST and WS also use headers (`android/app/src/main/java/persona/android/data/ApiClient.kt:43`, `:113`, `:122`; `data/Hub.kt:103`).
  - zoekt `token=` matches only pairing links, tests, scripts.
- [Assessment] For public exposure, restrict it to WS handshake or remove entirely (both clients already use headers), avoiding token leakage in reverse-proxy / access logs. Verify old-App dependencies before removal (none in current code).

## 3. Health checks and WebSocket

### 3.1 `/api/v1/health` [Fact]

- `GET /api/v1/health`, unauthenticated, `http.go:21-23`: `{"ok": true, "version": <hub version>, "features": proto.Features}`.
- `proto.Features` (`server/internal/proto/proto.go:40-54`): `client_msg_id`, `send_refusals`, `screen_dialog`, `resumable_interactions`, `secret_prompts`, `request_id`, `terminal_stream`, `terminal_control`, `terminal_scroll`, `terminal_click`, `file_upload`.
- No `hub_id`, address list, or TLS information. `docs/protocol/protocol.md:15` shows `{"ok":true,"version":"..."}` without `features` (line 16 mentions features separately).
- [Assessment] Unauthenticated health exposes version and capabilities. Whether to hide the version publicly is open to discussion (multi-address probes need a cheap unauthenticated endpoint, 4b).

### 3.2 WS handshake and `hello` [Fact]

- `GET /api/v1/ws` is behind authentication (`http.go:154`), handled by `serveWS` (`http.go:566-640`).
- `websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})` (`http.go:567`) disables coder/websocket Origin checks (cross-site browser WS accepted if it can supply a token).
- `conn.SetReadLimit(64 << 20)` (`:571`).
- After connection, sends in order (`:624-631`):
  1. `{"type":"hello","server_version":…,"client_id":<6 random bytes in hex>,"features":[…]}`;
  2. `{"type":"machines",…}`;
  3. One `sessions` per machine;
  4. One `interaction` per pending interaction.
- Accepted client types (`onClientMessage`, `http.go:642-731`): `terminal_subscribe`, `terminal_input`, `terminal_unsubscribe`, `subscribe`, `unsubscribe`, `register_phone`, `seen`, `tool_result`.
  **No `client_hello` or JSON `ping` / `pong`** (`docs/protocol/protocol.md:176`, `:189` document ping / pong absent from code; recorded in hub-changes.md 4.12).

### 3.3 Heartbeat, send queues, slow connections [Fact]

| Item | Value | Location |
| --- | --- | --- |
| Protocol-ping interval | 20 seconds (`time.NewTicker(20 * time.Second)`, hardcoded in `serveWS` writer goroutine) | `http.go:598` |
| Ping timeout | 15 seconds; failure calls `cancel()` to disconnect | `http.go:612-620` |
| Single-write timeout | 20 seconds; disconnect on failure | `http.go:604-611` |
| Per-connection send queue | `make(chan []byte, 512)` (message count, not bytes) | `http.go:573` |
| Full queue (`sendJSON`) | Disconnect with `conn.Close(StatusPolicyViolation, "too slow")`; App catches up after reconnect | `http.go:456-467` |
| Full queue (terminal `sendTerm`) | Same, asynchronous close | `server/internal/hub/terminal.go:481-491` |
| Full queue (`broadcast` / `toSubscribers` / `itemsToSubscribers`) | **Silently drop the message, no disconnect** | `http.go:469-488`, `:490-509`, `:512-548` |
| Disconnect cleanup | Remove client, unsubscribe (`removeWatcher`), `terminalDrop`, `CloseNow` | `http.go:579-595` |

- [Assessment] Silent broadcast drops are the least visible slow-connection failure: lost `interaction` / `interaction_resolved` / `sessions` leave the connection open and client unaware until the next full snapshot after reconnect hello. Persistent Android foreground-service connections on weak networks are especially exposed. Disconnect on full broadcast queue, as `sendJSON` does, or number events so clients detect gaps.
- Client side (iOS): `sendPing` every 20 seconds, every 5 seconds when unhealthy; no pong for 5 seconds marks weak networking, no received data for 45 seconds marks dead (`ios/Shared/LiveConnection.swift:178-181`, `:249-284`), section 5.

### 3.4 Per-connection heartbeat: change points [Assessment]

- Current ping interval is hardcoded in a local `ticker` in `serveWS`'s writer (`http.go:598`); `client` (`http.go:440-454`) has no heartbeat fields.
- Approach (small scope, concentrated in `http.go`):
  1. Add `pingEvery time.Duration` and `setPing chan time.Duration` to `client`;
  2. Use this ticker in the writer, add `case d := <-c.setPing: ping.Reset(d)` to `select`;
  3. Add `case "client_hello"` to `onClientMessage`, read `heartbeat_seconds` (clamp, for example, 15–300 seconds), write to `setPing`; also record `platform` / `device_id` / `version` (hub-changes.md 3.3);
  4. Dead-connection detection relies only on ping failure (15-second timeout); longer background intervals can also have longer ping timeouts;
  5. Add `client_heartbeat` to `features`; return the effective interval separately (`hello` is sent before `client_hello`, 4e). Old hubs ignore unknown `client_hello` (unmatched switch after `json.Unmarshal`, `http.go:669-672`), preserving compatibility.
- Hub ping is hub-initiated, phone-replied; background power use mostly comes from waking the phone to receive / send. Adjust Android OkHttp `pingInterval` too (section 6). NAT / carrier gateways expire idle TCP connections; intervals exceeding the network-specific timeout are cut by middleboxes. **Not measured; no concrete timeout given.**

## 4. Server changes for direct public connectivity

> Scope here is estimated line-count scale from reading code, **not measured**. Numeric settings (rate limits, heartbeat seconds, certificate lifetimes) are proposed values, to be finalized after real-device / hub-machine testing.

### 4a. TLS listener, self-signed certificate, SPKI fingerprint

**Current state [Fact]**: one plaintext listener, `srv.ListenAndServe()` (`server/internal/hub/hub.go:189-197`); no TLS fields in `Config` (`server/internal/hub/config.go:49-77`).
Comparable private files already live under `~/.persona/`: `secret.key`, `apns_key.p8` (`docs/server/server.md:66-74`). Postgres occupies `127.0.0.1:18661` (`docs/server/server.md:48`).

**Proposal [Assessment]**:
- New `server/internal/hub/tlscert.go`: `loadOrCreateTLS(dir) (tls.Certificate, pin string, error)`.
  - Storage: `~/.persona/tls/hub.key` (PKCS#8, 0600) + `hub.crt`, directory 0700. Follows CLAUDE.md's "Secrets never in the database": a file like `secret.key`, not a JSON state file.
  - Algorithm: ECDSA P-256 + SHA-256 signature (meets iOS ATS key / signature requirements, 5.5); `MinVersion: tls.VersionTLS12`.
  - SAN: hostname and configured domain are enough. Clients pin SPKI rather than verify hostname, so IP addresses are fine.
  - Fingerprint: `base64(sha256(cert.RawSubjectPublicKeyInfo))` (same format as HPKP `pin-sha256`). **Pin the public key, not certificate**: renew with the same key before expiry, preserving the fingerprint. A new private key requires all devices to re-pair (or optionally announce `next_pin` in hello first).
  - Either process (`personad hub`, `personad hub pair`) may generate first. Write with `O_EXCL`; on failure, read the other process's file.
  - Optional `tls.cert_file` / `tls.key_file` for user-provided trusted certificates (domain + Let's Encrypt). Clients may use system trust, but renewals often change keys, so pin the CA or disable pinning. First version can be self-signed only.
- New hub.json `tls: {listen, cert_file?, key_file?}` (`config.go:49-77`, `defaultConfig()` at `:86-96`). **Avoid port 18661** (Postgres).
- Two approaches to `Run()` (`hub.go:189-200`):
  1. **Two listeners** (simple, recommended first version): retain plaintext `listen`, add `tlsSrv.ListenAndServeTLS("", "")`, share `h.routes()`, `Shutdown` both. About 30 lines.
  2. **Same-port sniffing**: custom accept loop; per-connection goroutine peeks at the first byte with a timeout, wraps `0x16` in `tls.Server(conn, cfg)` and leaves others unchanged, then feeds a channel-backed listener to `srv.Serve(ln)` (never peek synchronously in `Accept`; silent connections would block it). About 100 lines. One port and firewall rule support old `http://<hub>:18660` and new `https://<hub>:18660`.
- Add `ReadHeaderTimeout` (for example, 10 seconds), `IdleTimeout` (120 seconds), `MaxHeaderBytes` (64 KB) to both servers. No global `ReadTimeout` / `WriteTimeout` (breaks WS / uploads).
- HTTP/2: `ListenAndServeTLS` enables h2 by default. WS uses HTTP/1.1 Upgrade (coder/websocket `Accept` requires `http.Hijacker`); iOS `URLSessionWebSocketTask` and OkHttp WS use HTTP/1.1, REST h2 is fine. To remove a variable, initially disable h2 with `TLSNextProto: map[string]func(...){}`.
- **Keep plaintext?** Yes, but only nonpublic sources (4d). Old Apps cannot pin self-signed certificates (iOS `URLSession.shared` / Android system CAs reject the handshake). Private-network / VPN devices, CLI, `sim.sh`, and `mock_hub.py` tests depend on it. Once new Apps are widespread, consider narrowing default `listen` to loopback / VPN. New Apps can use TLS + pinning on private networks too.
- `hub pair` (`config.go:153-162`) adds `Fingerprint: sha256/<pin>` output and `pin=` to QR (`config.go:149-151`).
- **Scope**: about 150–250 Go lines + tests (generation / reuse / fingerprint stability / sniffing), `docs/operations/config.md`, `docs/server/server.md`, `docs/protocol/protocol.md`.

### 4b. Multiple advertised addresses (IPv6 discovery, configured public addresses / domains)

**Current state [Fact]**: only one `public_url`, used only in QR and logs (1.1); no address discovery. `golang.org/x/sys` is already a dependency (`server/go.mod`).
`server/internal/sysmon/linux.go:95-103` detects physical interfaces, but counts bridges, bonds, and VLANs as virtual. Hub global IPv6 is often on a bridge, so **do not reuse it directly for address filtering**.

**Proposal [Assessment]**:
- Configuration: retain `public_url` (old QR / Apps); add `advertise: ["https://<domain>:<port>", …]` (manual domains / DDNS / router-forwarded IPv4), `advertise_ipv6` (automatic discovery, default on), `advertise_lan` (private addresses, default on).
- Discovery (new `server/internal/hub/endpoints.go`, pure functions unit-testable):
  - `net.Interfaces()` + `Addrs()`, retain IPv6 **global unicast** on up, non-loopback interfaces; exclude `netip.Addr.IsPrivate()` (ULA, including Tailscale IPv6), link-local, documentation prefix `2001:db8::/32`;
  - On Linux, use netlink (`unix.NetlinkRIB(RTM_GETADDR)`, x/sys already present) for `IFA_F_TEMPORARY` / `IFA_F_DEPRECATED` / `IFA_F_TENTATIVE`. **Do not advertise temporary privacy addresses**, which change roughly daily;
  - Exclude TUN proxy-interface addresses (hub uses a TUN proxy, CLAUDE.md lessons; these commonly use ULA or RFC 2544 benchmarking range 198.18/15, **not measured on the hub machine**);
  - Public IPv4 behind NAT is undiscoverable and must come from `advertise`; private IPv4 is a `scope: lan` candidate;
  - Rediscover every 60 seconds or subscribe to netlink address changes; log changes once.
- Each candidate: `{"url": "https://[<v6>]:<port>", "family": "ipv6|ipv4|name", "scope": "global|lan|configured", "tls": true}`, ordered global IPv6 → configured domain → private network → public_url (clients still probe concurrently, 5.5 / 6.2).
- Distribution:
  - WS `hello` (`http.go:624`) adds `hub_id`, `endpoints`, `tls_pin`;
  - New authenticated `GET /api/v1/endpoints` for HTTP-only Watch / Intents;
  - Unauthenticated `/api/v1/health` adds only `hub_id` to confirm identity during probes, **not addresses**, so port scanners cannot obtain private / other addresses;
  - QR: multiple `url=` or new `alt=`, plus `pin=`, `hid=` (4f), `code=` (4c). Error correction L; several IPv6 addresses add one or two hundred characters and enlarge terminal QR. Include only 2–3, get the rest from hello after connection.
- Change event: `h.broadcast({"type":"endpoints","endpoints":[…]})` (`http.go:469-488`). Background iPhones cannot receive WS; optionally send a silent push (existing `wake` in `push.go`) with the new list, written back to Keychain by the App.
- **Pitfall [Assessment]**: carrier PPPoE redial may periodically change residential IPv6 prefixes. If all saved addresses expire, the phone cannot ask for new ones. Recommend DDNS AAAA as fallback, retaining VPN / private addresses.
- **Pitfall [Assessment, unmeasured]**: TUN proxy auto-routing on the hub may send inbound IPv6 replies into the proxy, preventing connection establishment. Test on the hub before public use (phone on cellular accesses `https://[<hub IPv6>]:<port>/api/v1/health`). ONT / router default blocking is also common; document allowances.
- **Scope**: about 150–250 Go lines + tests; client parsing and persistence in sections 5, 6.

### 4c. One-time pairing code instead of permanent main token in QR

**Current state [Fact]**: QR contains the main token (`config.go:149-151`). `Pair()` is a separate process reading hub.json, not the running hub (`config.go:153-162`).
Deployment clears hub memory (CLAUDE.md: cross-request flows must be self-contained or persisted). `POST /api/v1/auth/tokens` accepts only `full` (`devicetokens.go:137-140`).

**Proposal [Assessment]**:
- New migration `pair_codes(hash text primary key, expires_at timestamptz, used_at timestamptz, used_by text, created_at timestamptz)`. Choose its number by workstream rules (existing `0002`, `0022`, `0031`, `0041`, `0051`, `0061`; inspect all worktrees and `persona_test.schema_migrations` before choosing, CLAUDE.md).
- `personad hub pair` (`server/cmd/personad/main.go:52-53`, `config.go:153-162`):
  - Default: use hub.json's main token to call the **running hub over local loopback**: `POST http://127.0.0.1:<listen port>/api/v1/auth/pair-codes`. Hub generates, persists, and returns `{code, short_code, expires_at, endpoints, pin, hub_id}`. `hub pair` prints QR `persona://pair?v=2&code=…&pin=…&hid=…&url=…&url=…`, a short manual-entry code, fingerprint, expiry.
    Address discovery, certificates, and hub_id thus have one implementation in the hub process, while `hub pair` also checks it is running.
  - `--legacy`: old format (`url=` + main token) for old Apps;
  - Hub not running: ask to start it, or fall back to direct database writes (`Pair()` has `database_url`).
- Hub: `POST /api/v1/auth/pair {code, name, kind}` **outside** `h.auth`, like health (`http.go:21`), with mandatory rate limiting (4d).
  - Atomic consumption: `UPDATE pair_codes SET used_at = now(), used_by = $2 WHERE hash = $1 AND used_at IS NULL AND expires_at > now() RETURNING …`;
  - Share issuance with `POST /auth/tokens` (extract `devicetokens.go:145-167` into `issueToken(ctx, name, kind)`), return `{id, token, role, name, hub_id}`;
  - Code length: 128 random bits in QR, 8 Crockford base32 characters (about 40 bits) for manual entry + 5-minute expiry + rate limiting;
  - Add `pair_code` to `features`.
- Main token: retained for CLI / old Apps. Add `personad hub rotate-token` (old Apps re-pair after rotation), and consider accepting main tokens only from loopback / private sources in public mode.
- **Scope**: about 200 Go lines (migration, db helpers, two routes, `Pair()` rewrite, tests); both clients' parsing and manual entry page ("Address + pairing code", token field under "Advanced").

### 4d. Reject public plaintext; rate-limit authentication failures

**Current state [Fact]**: server never inspects sources (zero `RemoteAddr` matches). Authentication failures are unbounded, logged without source only on non-WS paths (2.4); negative cache unbounded (`devicetokens.go:85-94`).

**Proposal [Assessment]**:
- Source helper `clientIP(r)`: `netip.ParseAddrPort(r.RemoteAddr)` followed by `Unmap()` (IPv4 on dual-stack sockets appears as `::ffff:`, 1.3).
  Local = `IsLoopback() || IsPrivate() || IsLinkLocalUnicast()` + `100.64.0.0/10` (CGNAT / Tailscale IPv4, not covered by `IsPrivate`) + configured `trusted_cidrs` (VPN ranges).
- Plaintext `r.TLS == nil && !local` → `403 {"error":"…","code":"tls_required"}`; outermost `routes()` (`http.go:159-160`) or a plaintext-listener wrapper. Optionally permit unauthenticated `/api/v1/health` so clients can say "Use https."
- **Caution**: by the time the hub rejects a request, its token has already traveled in plaintext. Real protection is client-side: **never send tokens over `http://` to public addresses** (5.5 / 6.2). Hub rejection makes misconfiguration fail immediately and visibly.
- **Caution**: a reverse proxy / tunnel such as frp makes sources appear as `127.0.0.1`, classified local. Document that proxies should connect only to TLS, or read `X-Forwarded-For` only with configured `trusted_proxies`. Router forwarding (DNAT) generally retains real sources.
- Rate limiter (new file such as `ratelimit.go`, no new dependency needed):
  - Key: IPv4 /32, **IPv6 /64** (attackers usually control an entire /64);
  - Example: 10 failures in 10 minutes per key →15-minute ban, `429` + `Retry-After`; add a global failure-rate cap to protect the database. **Unmeasured numbers**, tune before launch;
  - In `h.auth` (`http.go:163-184`), **check bans before `authenticate()` database lookup**, count failures; same for `POST /auth/pair`;
  - Bound or periodically clean negative cache (`devicetokens.go:85-94`);
  - Failure logs include redacted source (/64 or /24), path, count, **including `/api/v1/ws`**: change `http.go:206` to log WS 401 / 429, with log rate limits too.
- Also: `websocket.AcceptOptions{InsecureSkipVerify: true}` (`http.go:567`) disables Origin validation. WS needs a token; cross-site browsers cannot supply `Authorization`, only `?token=`, so risk is low and mostly removed by restricting `?token=` (2.5).
- **Scope**: about 150 lines + tests.

### 4e. Heartbeat negotiation in `client_hello`

See 3.4. Changes concentrated in `http.go` (`client` `:440-454`, writer `:597-622`, `onClientMessage` `:642-731`, `hello` `:624`), capability in `proto.Features` (`server/internal/proto/proto.go:42-54`), and `docs/protocol/protocol.md`. About 40–60 lines.
Combine `client_hello` (platform, version, capabilities) from hub-changes.md 3.3 with application ping / pong from 3.8: client sends `{"type":"client_hello","device_id","platform","version","features":[…],"heartbeat_seconds":N}`, hub confirms with effective interval.
Ordering: hub `hello` is sent on connection (`http.go:624`), before `client_hello`. Return the effective value separately, for example `{"type":"client_hello_ack","heartbeat_seconds":N}`, or send client settings in handshake headers / query.

### 4f. `hub_id`

**Current state [Fact]**: no server `hub_id` (zero zoekt matches). `settings` is a generic key-value table (`server/internal/db/migrations/manager/0002_settings.sql`, `Setting` / `PutSetting` in `server/internal/db/db.go:444-468`, `Store` in `hub.go:103-104`).
**Proposal [Assessment]**: after `db.Open` in `Run()` (`hub.go:140-145`), read `settings["hub_id"]`; if absent, generate UUIDv4 with `crypto/rand`, write it back, and keep it on `Hub`. No new migration, about 20 lines.
Publish in `/api/v1/health` (`http.go:21-23`), WS `hello` (`http.go:624`), pairing exchange response, QR `hid=`.
Semantics: same database, same id (including migration to another machine); a cleared database means a new hub and clients clear all local data (`docs/archive/redesign/README.md`, D6 scoping).
TLS fingerprint also identifies a hub, but private plaintext has no fingerprint and data scope should not track certificates, so keep both.

## 5. iOS client

### 5.1 Pairing storage [Fact]

- Type: `ServerConfig { url: URL, token: String }`, `Codable` (`ios/Shared/APIClient.swift:3-27`). `init?(pairURL:)` recognizes only `persona://pair?url=…&token=…` (`:7-16`); demo mode uses `https://demo.invalid` (`:25-26`).
- Keychain (`ios/Shared/Keychain.swift`): generic password, `service` = App bundle id (strip `.watchkitapp` on Watch, `:6`), `kSecAttrAccessibleAfterFirstUnlock` (`:14`), **no access group**.
  - account `server`: phone `ServerConfig`, JSON-encoded whole structure (`:42-48`);
  - account `watch`: exchanged answer-only Watch token (`:34-40`).
- Token exchange: `AppModel.pair()` (`ios/Persona/App/AppModel.swift:140-156`) validates with `client.machines()`, saves Keychain, clears device credentials, reconnects, sends to Watch.
  `upgradeCredentials()` (`ios/Persona/App/DeviceCredentials.swift:15-39`) uses pairing token for `POST /api/v1/auth/tokens kind=phone`, **overwrites Keychain `server` with the new token** (`:22-28`), and records device id in UserDefaults `auth.deviceID` (`:10`, `:25`), then exchanges `kind=watch` (`:33-37`). Old hubs lacking the endpoint keep using pairing token (`catch { return }`, `:29-31`).
- Pairing entry points:
  - Scan / manual: `PairView` (`ios/Persona/Views/Settings/SettingsView.swift:367-428`, `connect()` at `:415`), manual fields "Server Address" and "Token";
  - Link: `onOpenURL` parses `ServerConfig` and **directly** calls `model.pair(cfg)` (`ios/Persona/App/PersonaApp.swift:15-25`), without confirmation, replacing existing pairing too;
  - DEBUG: `-pairURL` launch argument (`PersonaApp.swift:46-49`).

### 5.2 URL construction [Fact]

| Purpose | Construction | URLSession | Location |
| --- | --- | --- | --- |
| REST | `config.url.appending(path:)` + query | `APIClient.session`, default `URLSession.shared` | `APIClient.swift:89`, `:100-105`, `:107-123` |
| Upload | Same, `/machines/{mid}/files?name=` | `APIClient.uncachedSession` | `:143-155`, `:93-98` |
| Lazy images / full text | `itemURL()` builds `percentEncodedPath` manually (ids contain `#`, `:`) | `uncachedSession` | `:273-285`, `:288-298`, `:301-308` |
| Lark raw-doc endpoint | `url("/api/v1/lark/raw/…")` | `self.session` | `ios/Persona/Views/Feishu/Docs/FeishuDocsComponents.swift:10-17` |
| Lark media | Relative paths via `api.request` (`uncachedSession`); absolute http(s) directly via `uncachedSession.data(from:)`, **without token** | `uncachedSession` | `ios/Persona/Views/Inbox/Lark/LarkMedia.swift:37-60` |
| Lark video | `AVURLAsset(url: cfg.url.appending(path:), options: [AVURLAssetHTTPHeaderFieldsKey: Authorization])` | AVFoundation networking | `LarkMedia.swift:296-302` |
| WS | `config.url.appending(path: "/api/v1/ws")`, `https` →`wss`, otherwise `ws`; `Authorization`; handshake timeout 15 seconds; maximum message 64 MB | `LiveConnection`'s own `URLSession(configuration: .default)` | `ios/Shared/LiveConnection.swift:174`, `:222-247` |

- All requests use `Authorization: Bearer`, not `?token=` (2.5).
- Heartbeat (`LiveConnection.swift:178-181`, `:249-284`): `sendPing` every 20 seconds; no pong for 5 seconds means weak networking (then ping every 5 seconds); no data for 45 seconds disconnects; backoff 1→20 seconds (`:340-349`).
  `connected` only after the first message (hello) (`:300-306`); hello reads only `features` (`AppModel.swift:572-573`).
- Network changes: `ConnectionStatus` uses `NWPathMonitor` (`ios/Persona/App/ConnectionStatus.swift:43`, `:65-66`); on recovery `reconnectNow()`, interface change `probe()` (`:118-120`).
- Backgrounding disconnects WS (`PersonaApp.swift:27-35`). Silent push `persona_wake` calls `ensureConnected()`, disconnects 25 seconds later (`PersonaApp.swift:106-120`); hub waits only 20 seconds (`server/internal/hub/hub.go:737`).

### 5.3 ATS [Fact]

- iPhone App: `NSAppTransportSecurity.NSAllowsArbitraryLoads = true` (`ios/project.yml:84-86`, comment "hub usually uses plaintext HTTP on a private network"; generated `ios/Persona/Resources/Info.plist:38-42`).
- Watch App: also `NSAllowsArbitraryLoads = true` (`ios/project.yml:144-145`, `ios/PersonaWatch/Info.plist:23-27`).
- Widget / Live Activity extension `PersonaWidgets`: no ATS settings (`ios/project.yml:101-128`), and no networking. Live Activity button intents run in the App process (`ios/LiveActivity/FleetIntents.swift:4-6`, comments in `project.yml:127-128`).
- `ITSAppUsesNonExemptEncryption: false` (`project.yml:75`). [Assessment] Pinning with system TLS (URLSession) remains exempt; no change needed. Reassess only for custom cryptographic protocols.

### 5.4 Other direct hub callers [Fact]

| Caller | Configuration source | Location |
| --- | --- | --- |
| Watch | Phone pushes JSON `ServerConfig` through WatchConnectivity (`applicationContext` + `transferUserInfo`, `ios/Shared/WatchSync.swift:35-45`). Watch saves it in **UserDefaults `config`**, not Keychain (`ios/PersonaWatch/PersonaWatchApp.swift:34-43`), then uses `APIClient` (`URLSession.shared`) directly (`:31`, `:75-145`). | Receives `Keychain.watchConfig ?? serverConfig` (`AppModel.swift:113-117`, `:136`). |
| Siri / Shortcuts | `IntentSupport.client()`: `Keychain.serverConfig` →`APIClient` (`ios/Persona/Intents/IntentSupport.swift:133-141`); error says "Check that your phone can reach the hub (same network or VPN)" (`:116-126`). | |
| Notification buttons | `resolveFromNotification`: `Keychain.serverConfig` →`APIClient.resolve` (`PersonaApp.swift:159-176`); errors also use `host` (`ios/Persona/App/NotificationActions.swift:58`). | |
| Live Activity buttons | `FleetActions.run`: `Keychain.serverConfig` →`APIClient.resolve` (`ios/Persona/App/FleetActions.swift:17-21`) | |
| Push registration | `PushRegistrar.didRegister` →`AppModel.shared.api` →`POST /api/v1/devices` (`PersonaApp.swift:230-241`) | |
| Settings address display | `model.config?.url.absoluteString` (`SettingsView.swift:27`) | |
| WKWebView | `LinkSheet` (external links), `MailViews` (mail HTML); **no direct hub-address loads found** | `ios/Persona/Views/Link/LinkSheet.swift`, `ios/Persona/Views/Mail/MailViews.swift:189-224` |

### 5.5 iOS changes for multiple candidates + certificate pinning [Assessment]

1. **Data model**: add backward-compatible optional `Codable` fields to `ServerConfig` (`APIClient.swift:3-27`):
   - `addresses: [URL]` (prioritized candidates; retain `url` as last successful address to preserve old code paths);
   - `pin: String?` (certificate SPKI sha256, base64);
   - `hubID: String?` (4f).
   `init?(pairURL:)` parses new parameters (multiple `url=`, `pin=`, `code=`, 4a–4c). Keychain stores the entire JSON; old data simply decodes with empty new fields.
2. **Unified URLSession with a delegate**: three current sessions (`URLSession.shared`, `uncachedSession`, `LiveConnection.session`) all lack delegates and **cannot pin**.
   Use the same delegate, implementing `urlSession(_:didReceive:completionHandler:)` for `NSURLAuthenticationMethodServerTrust`, computing leaf-public-key SPKI sha256, comparing to `pin`, and using `.useCredential` only on equality. Change `APIClient.session` default (`:89`), `uncachedSession` (`:93-98`), `LiveConnection.session` (`LiveConnection.swift:174`). Upload task delegate `UploadProgress` (`:154`, defined `:795-802`) handles only progress; authentication falls through to session delegate, requiring no change.
3. **AVPlayer cannot use a URLSession delegate**: video in `LarkMedia.swift:296-302` uses AVFoundation directly, failing self-signed TLS. Download locally before playback (existing `file(_:name:)`, `LarkMedia.swift:68`), or use `AVAssetResourceLoaderDelegate` with custom-scheme forwarding.
4. **Candidate selection**: shared new `HubEndpoint` type supplies current address to `APIClient.url()` (`:100-105`), `itemURL()` (`:301-308`), `LiveConnection.open()` (`:222-247`), `LarkMedia` (`:298`).
   - Probe unauthenticated cheap `GET /api/v1/health` concurrently, with new hub `hub_id` identity check. IPv6 first, IPv4 delayed (RFC 8305 recommends 250 ms attempt delay; design parameter, not measurement). First pin + `hub_id` success wins;
   - Probe on cold start, `NWPathMonitor` changes (`ConnectionStatus.swift:65-120`), before WS reconnect, and on silent-push wake (only about 25 seconds, `PersonaApp.swift:116`; concurrent probes, not serial 15-second timeouts);
   - Watch, Intents, notification buttons, and Live Activity buttons run outside the App or in background, so cannot rely on App-memory current address. Write the selected address back to Keychain or probe quickly each time.
5. **ATS** [Assessment, real-device verification needed]: custom trust for self-signed certificates still checks TLS 1.2+, forward-secret suites, certificate keys (RSA ≥2048 / ECC ≥256), and SHA-256 signatures. Hub ECDSA P-256 + SHA-256 meets these.
   Retain `NSAllowsArbitraryLoads` initially (plaintext old hubs on private networks / VPN must still work). Add an App-level rule: `http://` only for private / VPN / loopback addresses and `.local`; public addresses require `https` + pinning (android.md 5.1).
6. **Watch**: `ServerConfig` sent by `WatchSync` automatically includes new fields; Watch `APIClient` must also use a pinned session. Current plaintext token storage in Watch UserDefaults (`PersonaWatchApp.swift:34-43`) should move to Watch Keychain for public use.
7. **Pairing links**: direct `onOpenURL` pairing (`PersonaApp.swift:15-25`) is riskier publicly: any web / message `persona://pair?url=<someone else's hub>` can redirect the phone, allowing that hub to call phone tools. Add confirmation showing address / fingerprint; never silently replace existing pairing.
8. **Error categories**: add "Certificate mismatch" to Intents (`IntentSupport.swift:116-126`), Lark docs (`FeishuDocsComponents.swift:20-30`), and PairView errors; remove "same network or VPN" wording.

## 6. First Android version (PR #84)

### 6.1 Current state [Fact]

(Paths beginning `data/`, `ui/`, `service/` below are under `android/app/src/main/java/persona/android/`.)

| Item | Current behavior | Location |
| --- | --- | --- |
| Pairing data | `data class Pairing(val url: String, val token: String)` | `android/app/src/main/java/persona/android/data/Pairing.kt:7` |
| Storage | `PairingStore`: `SharedPreferences("pairing", MODE_PRIVATE)`, **plaintext** `url`, `token` (no Keystore) | `Pairing.kt:9-19` |
| Backup | `android:allowBackup="false"` | `android/app/src/main/AndroidManifest.xml:19` |
| Parsing | `PairingStore.parse()`: scheme `persona`, host `pair`, `url` begins with `http`, nonempty `token` | `Pairing.kt:22-30` |
| Link pairing | Manifest `persona://pair` (`AndroidManifest.xml:36-42`) →`MainActivity.handle` sets `pendingPair` (`android/app/src/main/java/persona/android/MainActivity.kt:49-54`, `:60`) →`PairScreen` automatically pairs (`ui/PairScreen.kt:82-85`, `LaunchedEffect(link)` at `:83`). `PairScreen` appears only when unpaired (`ui/Root.kt:16-17`), so **links do not silently replace existing pairing**. | |
| Pairing flow | Validate with `ApiClient(url, token).machines()` →`createToken(deviceName)` (404 = old hub, retain pairing token) →store `Pairing(url.trimEnd('/'), own)` →`PersonaApp.connect` | `ui/PairScreen.kt:55-76` |
| Pairing errors | 401 →"Invalid token"; others →"Cannot connect … Phone and hub must be on the same network or VPN". Manual placeholder `http://<hub address>:18660`; button requires `url.startsWith("http")`. | `PairScreen.kt:69-73`, `:108-120` |
| Connection object | `PersonaApp.connect(p)` creates `Hub(ApiClient(p.url, p.token), …)` on each pairing / launch and starts the foreground service. | `android/app/src/main/java/persona/android/PersonaApp.kt:29-45` (`connect` at `:39`) |
| HTTP client | `ApiClient(val baseUrl: String, val token: String)`, one `OkHttpClient`: connect 10 seconds, read 110 seconds, `pingInterval(20 s)`, shared REST / WS | `data/ApiClient.kt:23-28` |
| URLs | `url()`: `baseUrl.toHttpUrl()` then `addPathSegment` per segment. `wsUrl()`: `replaceFirst("http", "ws")` (also maps `https` →`wss`). | `ApiClient.kt:32-38` |
| Authentication | All `Authorization: Bearer` (REST `:43`, images `:113`, full text `:122`, WS `data/Hub.kt:103`), no `?token=` | |
| health | `health()` exists (decodes only `ok`, `version`), **no callers** | `ApiClient.kt:67-68` |
| WS | `api.http.newWebSocket`; Online on onOpen, registers phone tools and resubscribes; backoff 1→30 seconds | `Hub.kt:72-88`, `:102-122` |
| hello | Reads only `server_version`, ignores `features` | `Hub.kt:146` |
| JSON ping | Replies `pong` to `{"type":"ping"}` (`Hub.kt:147`), but hub never sends JSON ping (3.2) | |
| Network changes | Foreground service `registerDefaultNetworkCallback`, `onAvailable` →`kick()` (`service/HubService.kt:47-50`); `kick()` reconnects **only when not Online** (`Hub.kt:97-100`). | |
| Notification buttons | `ActionReceiver` uses current `hub.api.resolve` (`service/ActionReceiver.kt:19`, `:39`) | |
| Plaintext | `network_security.xml` only has `<base-config cleartextTrafficPermitted="true" />`, comment "hub uses plaintext HTTP on private networks or VPN"; no `trust-anchors` / `pin-set` | `android/app/src/main/res/xml/network_security.xml`, manifest reference at `:22` |
| SDK | `compileSdk 34`, `targetSdk 34`, `minSdk 26`; OkHttp 4.12.0; no other networking such as Coil / Ktor | `android/app/build.gradle.kts:13-18`, `:57` |

### 6.2 Changes needed [Assessment]

1. **`Pairing` / `PairingStore`** (`Pairing.kt`): add `addresses`, `pin`, `hubId`, parse multiple `url=` / `pin=` / `code=`. Encrypt tokens with an Android Keystore AES key before storing (android.md already specifies Keystore). Old records without fields mean single address, no pin.
2. **Pinning must be implemented in code**: `network_security.xml` `<pin-set>` and OkHttp `CertificatePinner` compare **after system trust-chain verification**; self-signed certificates fail before that. `<trust-anchors>` needs bundled certificate resources, unsuitable for each user's own certificate.
   Hub-specific `OkHttpClient` with custom `X509TrustManager` (accept leaf SPKI sha256 only when equal to `pin`) + `hostnameVerifier` (changing IPs; pinning supplies identity). Use only for hub, not future Gmail login / FCM requests. Change `ApiClient.kt:24-28`.
3. **Candidates**: `ApiClient.baseUrl` and `Hub.api` are `val` (`Hub.kt:44`); changing address means rebuilding `Hub` (losing subscriptions), or extracting mutable `HubEndpoint`.
   Use existing unused `health()` (`ApiClient.kt:67-68`, add `hub_id`, `addresses`). `kick()` (`Hub.kt:97-100`) must reroute on network changes even while Online (Wi-Fi→cellular old socket stays "Online" until OkHttp ping fails).
4. **Plaintext**: retain global permission in `network_security.xml` for old private hubs, but enforce App-level `http://` only for private / VPN / loopback / `.local`; public addresses require `https` + pinning (android.md 5.1).
5. **Heartbeat**: OkHttp `pingInterval` is client-wide and immutable after WS establishment. Change by `http.newBuilder().pingInterval(x)` and reconnect. No pong before the next ping means failure, so interval is also timeout.
   Coordinate with hub `client_hello.heartbeat_seconds` (3.4): foreground 20 seconds, screen-off / background persistent service potentially 60–120 seconds. **Power use and NAT timeouts are unmeasured**; test devices.
6. **Errors**: `SSLHandshakeException` / `SSLPeerUnverifiedException` →"Certificate mismatch, scan again"; revise "same network or VPN" in `PairScreen.kt:69-73`.
7. **Local-network permission** (when targeting 37, android.md 5.1): whether global IPv6 counts as local is **unverified**. If not, IPv6 priority could also bypass this permission on the same Wi-Fi; worth real-device testing.

## 7. Documentation

### 7.1 Existing "private network / VPN only" and "HTTP by default" text [Fact]

| Location | Original point | Revision after public-connectivity support [Assessment] |
| --- | --- | --- |
| `README.md:53` | Preparation table: "Phone can reach port 18660 (same network or VPN)" | "Phone can reach the hub: same network, VPN, or direct public connection (IPv6 preferred, allow TLS port)." |
| `README.md:68`, `:71` | Step 3: enter the address the phone uses; step 5: scan | Explain `public_url` / `advertise` / `tls` in step 3; step 5's QR is a one-time code valid 5 minutes, old Apps use `--legacy`. |
| `README.md:79` | "Hub defaults to HTTP; private network or VPN only, never expose directly to the Internet." | TLS (self-signed + App fingerprint pinning) may be exposed publicly; plaintext only private / VPN sources, public plaintext rejected, authentication failures rate-limited. |
| `docs/operations/config.md:22` | Same preparation table as README | Same change |
| `docs/operations/config.md:35`, `:88`, `config.example.json:27` | Build `hub.url` is `http://…:18660` | Used by `sim.sh` to pair simulators; keep private plaintext, add explanation. |
| `docs/operations/config.md:50`, `:118-119`, `:138` | hub.json `listen`, `public_url` examples are `http://`; first-run advertised URL `http://<hostname>:18660` | Add `tls`, `advertise`, `advertise_ipv6`, `advertise_lan`, `trusted_cidrs` to examples / field table ("Hub runtime configuration" at `:114`). |
| `docs/server/server.md:43` | "Listens on `:18660`" | Add TLS port, host / ONT firewall IPv6 allowances, TUN reply-routing issue. |
| `docs/server/server.md:71`, `:77` | hub.json fields; pairing prints address, token, QR `persona://pair?url=…&token=…` | Add fields; pairing-code format and `--legacy`. |
| `docs/server/server.md:94` | `curl -H "Authorization: Bearer $TOKEN" http://<hub address>:18660/…` | Add https: `curl --pinnedpubkey 'sha256//<pin>' -k https://…` (`-k` skips CA validation, pinning still applies). |
| `docs/server/server.md`, "Operations" (around `:90-100`) | Log failures / slow requests only, no query/body/token | Add rate limits, bans, WS failure-log format, source redaction. |
| `docs/protocol/protocol.md:3` | "Hub listens on `:18660` by default" | Add TLS port |
| `docs/protocol/protocol.md:5-21` | `Bearer`, WS `?token=`, health `{"ok":true,"version":"..."}` | Define new `?token=` scope (2.5); health `features` (already implemented), `hub_id`; new pairing-code / certificate-pinning / candidate-address subsections; rate-limit `429`. |
| `docs/protocol/protocol.md:154-190` | WS hello only `server_version, client_id`; ping / pong documented but absent from code | Add hello `features`, `hub_id`, `endpoints`, `tls_pin`; `endpoints` event and `client_hello`; remove or implement ping / pong (hub-changes.md 3.8 / 4.12). |
| `docs/server/architecture.md:17-18`, the former architecture diagram | Diagram: "HTTPS/WS (Bearer token)", "hub machine :18660" | **Already inaccurate** (actually plaintext HTTP). Use "HTTPS (self-signed + pinning, public) / HTTP (private network, VPN)". |
| `docs/android/android.md:13` | Pairing `persona://pair?url=…&token=…` | New format |
| `docs/android/android.md:47` | HTTP private / VPN default, `network_security.xml` allows plaintext; do not expose publicly | Private / VPN plaintext only, public TLS + pinning; explain why code-based pinning is required (6.2). |
| `docs/android/android.md:84-85` | USB `adb reverse` uses `http://127.0.0.1:18660` | Keep loopback plaintext allowed |
| `docs/apple/intents.md:51`, `:56` | "Phone must be on the company network or VPN"; "hub uses plaintext HTTP on the company intranet" | Generalize; `.background` rationale still holds (do not launch the App when networking is unreachable). |
| `docs/apple/ios.md:306`, `:318-320`, `:343` | Keychain pairing data, Watch pairing, `persona://pair` links | Include candidates / fingerprint / hub_id; Watch token to Keychain; link-pairing confirmation. |
| `docs/operations/testing.md:50`, `scripts/mock/mock_hub.py:6`, UI tests | `-pairURL "persona://pair?url=http%3A%2F%2F127.0.0.1…&token=mock"` | Continue allowing loopback + plaintext + token, or all UI tests fail. |

### 7.2 Equivalent UI text / comments in code [Fact]

- `ios/project.yml:84` (comment "hub usually uses plaintext HTTP on a private network"), `android/app/src/main/res/xml/network_security.xml:2` (equivalent comment).
- "Same network or VPN": `ios/Persona/Intents/IntentSupport.swift:123`, `:127`; `ios/Persona/Views/Settings/SettingsView.swift:424`; `android/app/src/main/java/persona/android/ui/PairScreen.kt:72`.
- "Check that your phone can reach the hub": `ios/Persona/Views/Feishu/Docs/FeishuDocsComponents.swift:28`.
- Android manual placeholder `http://<hub address>:18660`: `PairScreen.kt:110`.

### 7.3 Additions [Assessment]

- New `docs/operations/network.md` for direct public connectivity: TLS port / certificate files, checking / changing fingerprints, IPv6 allowances (hub firewall, ONT / router), DDNS fallback, TUN reply-routing issues, reverse-proxy source addresses, rate-limit settings. Add to CLAUDE.md index.
- Record the user requirement in CLAUDE.md conventions (2026-10-30: phone must connect directly to the hub over the Internet, probing and preferring IPv6; user manages VPN), following the rule to capture lessons as they arise.
- `docs/archive/redesign/ux.md` 3.7 item 3 ("HTTP is unencrypted, use only on private networks or VPN") and `docs/archive/redesign/android.md` 5.1 ("Plaintext HTTP") align with this plan. One-time pairing codes in ux.md 3.7 item 5 / hub-changes.md 4.1 become a prerequisite for public connectivity, not optional.

## 8. Old-client compatibility concerns

1. **Old Apps cannot connect to self-signed TLS** [Fact + Assessment]: iOS's three URLSessions have no delegates (`ios/Shared/APIClient.swift:89`, `:93-98`, `ios/Shared/LiveConnection.swift:174`); Android uses system CAs (no trust-anchors in `network_security.xml`). Keep plaintext for private / VPN devices, and retain `public_url` as one old-App-compatible address.
2. **QR format**: old iOS `ServerConfig(pairURL:)` and Android `PairingStore.parse()` **require `token=`** (`APIClient.swift:12`, `Pairing.kt:28`). Code-only QR produces no response on old iOS and "Not a Persona pairing QR code" on old Android. Keep old format with `hub pair --legacy`; new Apps must also recognize it (`mock_hub.py`, `sim.sh`, all UI tests use `http://127.0.0.1…&token=mock`).
3. **Multiple `url=`**: old clients take the first (iOS `queryItems.first`, `APIClient.swift:11`; Android `getQueryParameter`). Old-App compatibility would require first URL to be private plaintext and include `token=` (main token), conflicting with removing main tokens from QR. Conclusion: separate old / new QR formats.
4. **Upgrading already-paired new Apps to TLS**: send `tls_pin` + `endpoints` in hello; save the fingerprint over the existing authenticated channel. This is TOFU on private plaintext / VPN (same-LAN attackers can inject fake fingerprints). Stricter approach: re-scan. **Decision required.**
5. **New WS fields / events**: old iOS drops unknown types by returning nil (`LiveConnection.swift:130`); old Android `when` has no else (`data/Hub.kt:145-211`), so both ignore them. But iOS first decodes the whole `Envelope` (`LiveConnection.swift:42-85`). **Do not add differently typed fields whose names collide with existing keys** (`error`, `id`, `state`, `data`, `name`, `status`, `text`, etc. are strings), or the whole message fails decoding and is dropped (an object-valued `error` in hello would lose hello).
6. **`client_hello`**: old hubs ignore unknown types (`onClientMessage` switch without default, `http.go:672-730`). New clients use `client_heartbeat` in `features` to determine negotiation support, not wait for a receipt.
7. **Watch**: WatchConnectivity sends whole `ServerConfig` JSON (`ios/Shared/WatchSync.swift:35-45`); old Watch decoders ignore new fields and keep using `url`. If a new phone replaces `url` with self-signed TLS, old Watches fail. Watch / iPhone Apps generally update through the same TestFlight build, but timing differs: keep `url` usable by old code and put new addresses in new fields.
8. **Rate-limit collateral damage** [Assessment]: both WS clients retry indefinitely on 401 (iOS max backoff 20 seconds, `LiveConnection.swift:340-349`; Android 30 seconds, `data/Hub.kt:76-86`), no stop-on-401 logic (only Intents says pairing expired for 401/403, `ios/Persona/Intents/IntentSupport.swift:112-113`). Those backoff limits imply hundreds of failures per hour from one revoked device (**not measured**, inferred only from 20 / 30 seconds).
   IPv4 /32 limits would ban other devices behind the same home NAT too. Exclude private / VPN sources, key public limits by "source prefix + token hash", and stop client auto-reconnection on 401 with a re-pair prompt (both new clients must change).
9. **`?token=`**: current clients do not use it (2.5), so restricting is safe. Earlier App builds **not checked** (current master only).
10. **Debug paths**: `scripts/sim.sh:16-19` reads the main token from hub.json and pairs with configured `hub.url`; `adb reverse` in `docs/android/android.md:84-85` uses `http://127.0.0.1:18660`. New clients must allow loopback + plaintext + token, and the hub must treat loopback as local.
11. **Push, node (SSH)**: unaffected. APNs does not traverse the hub listener; nodes use SSH stdin / stdout.

## 9. Unresolved / awaiting confirmation

- **Not measured** (under user rules, no numbers can be given for these): whether `:18660` is actually dual-stack on the hub (`ss -ltnp`), phone-cellular IPv6 reachability, TUN effects on inbound replies, ONT / router inbound IPv6 policies, carrier / NAT TCP idle timeouts, power use at different heartbeat intervals.
- **Real-device verification**: exact iOS restrictions on accepting self-signed certificates through custom trust with ATS on or `NSAllowsArbitraryLoads` retained; watchOS public IPv6 connectivity (phone proxy / Wi-Fi / cellular differ); Android local-network permission classification of global IPv6 on the same LAN.
- **Not checked**: `?token=` use in earlier builds, added public-exposure risks for `mail` subroutes, Lark gateway proxy, Gmail OAuth callback (all behind `h.auth`, OAuth state encrypted and self-contained, `docs/extensions/mail.md:52`, but not individually audited). hub ↔ node SSH is outside scope.
- **Design decisions required**: two ports vs same-port sniffing (4a), TOFU fingerprint trust over existing private channels for paired devices (8.4), retaining public version / features in `/api/v1/health` (3.1), limiting main tokens to loopback / private sources in public mode (4c).
- **Existing source line references have drifted**: `config.go:144-147` cited by `docs/archive/redesign/research/persona-protocol.md:26` is now `config.go:148-151`.
