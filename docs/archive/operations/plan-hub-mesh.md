# Plan: linked hubs, computer connections, multi-hub clients, admin UI, and web parity

> **The second version supersedes the peer-to-peer portions of section 2 (ownership, route advertisements, transparent forwarding) and the ssh portion of section 3**: one primary hub, connections through a preinstalled service, and multiple routes to one computer. See [plan-hub-center.md](plan-hub-center.md).

Overview issue #293. On 2026-11-02, the user requested changes to the service-connection architecture. This document defines the overall plan and **contracts between workstreams**, allowing parallel development against those contracts.
Each workstream fills in the details (every protocol field, retry parameters, UI) in its own document. The baseline is integration branch `feat/jolly-cerf-dfb019`; workstream PRs merge back into it.

## Goals

1. **Linked hubs**: hubs discover one another and communicate bidirectionally once connected. A client connected to **any** hub in a linked group can see all computers and terminals connected to every hub in the group,
   with each computer appearing only once and accessed over the best route.
2. **node**: the hub initiates an ssh connection (unchanged).
3. **links**: computers that are not exposed or are inconvenient to connect to initiate a connection to the hub, which reuses that link (current behavior in [link.md](../../operations/link.md)); Mac menu bar, Windows tray, Linux headless.
4. **Clients connect to multiple unrelated hub groups simultaneously**: isolated environments such as work / personal, displayed in merged lists with group labels.
5. **Hub admin UI**: a built-in web admin UI, accessible only from the hub's LAN by default; deployment prompts the user to choose the access scope.
6. **Web app**: roughly the same capabilities as the phone App.

## Topology and terminology

```text
 Phone / web ──► hub A ◄══ mesh link ══► hub B ◄── Phone (separate groups are not linked)
                   │  ▲                    │
  ssh starts node  │  └─ link (computer     └─ ssh / link …
                   ▼      connects out)
                Computer
```

| Term | Meaning |
| --- | --- |
| **Linked-hub group** | Hubs connected directly or indirectly through mesh links. Groups are derived from connectivity; there is no separate "group" configuration. |
| **Direct connection** | The hub itself connects to this computer's node (`local` / `ssh` / `tailcat` / `link`). |
| **Owning hub** (owner) | Only one hub in the group actually connects to a computer at a given time; others access it through that hub (see "Ownership"). |
| **Group** (client) | Paired hubs grouped by their mesh relationships; one connection within each group, a separate connection for each different group. |

## 1. Machine identity `machine_uid`

Deduplication needs to know "whether two hubs connect to the same computer." Machine ids (`machines[].id` in `hub.json`) are names assigned independently by each hub and cannot serve this purpose.

- On its first run, the node generates `~/.persona/machine-uid` (UUIDv4, 0600) and keeps using it. `node.info` includes `machine_uid`.
- Old nodes lack this field: their computers are not deduplicated and are treated as separate machines keyed by "hub_id + machine id."
- Cloned virtual machines inherit the same file: `personad node reset-uid` regenerates it; document this.

## 2. Linked hubs (server)

### 2.1 Discovery

- **Automatic LAN discovery**: each hub broadcasts `_persona-hub._tcp` via mDNS. TXT contains only `hub_id`, name, TLS port, public-key fingerprint `fp`, and version—no credentials.
  Docker bridge networking prevents multicast from getting out, so hubs must be added manually (document this; `network_mode: host` works).
- **Manual addition**: enter the peer address, then request its `GET /api/v1/health` to get `hub_id`, `tls_port`, and `fp` for user verification.
- Discovery only lists hubs; it **does not link them automatically**. Linking requires confirmation in the admin UI and entering the other hub's linking code.

### 2.2 Pairing and trust

- Generate a **linking code** on hub B (admin UI **Linked hubs › Generate linking code**, or `personad hub pair --peer`): single-use, valid for 10 minutes, with B's candidate addresses, `hub_id`, and `fp` in the link.
- Paste the linking code into hub A's admin UI. A connects to B pinned by `fp`, exchanges the code for B-issued mesh credentials for A, and gives B A's `fp` and A-issued mesh credentials for B.
  This establishes **mutual pinning and mutual credentials**, so either side can dial.
- Mesh credentials are a type of device credential: kind `peer`, role `peer`, bound to the peer's `hub_id`, permitted only on `/api/v1/peer*`; every other endpoint returns 403.
  The hub stores only a hash. "Disconnect" on either side's admin UI revokes its issued credentials and notifies the other side, immediately closing the link.
- Mesh relationships and credentials are stored in Postgres (migration **0151**), not `hub.json`.

### 2.3 Mesh links

- WSS `/api/v1/peer`, only on the TLS port (`public.tls_listen`), pinned by `fp` even on a private network. A hub without TLS cannot join the mesh (the admin UI prompts users to enable it).
- Keep only one link per pair of hubs. Either side can dial; if two links connect simultaneously, retain the one dialed by the hub with the smaller `hub_id`.
- Two types of traffic run over the link:
  - **Control messages** (JSON): hello, route advertisements, forwarded events, heartbeat;
  - **Multiplexed streams**: forwarded HTTP requests / WebSockets (reuse `internal/streams` or the link approach). All client operations on remote machines use these streams.
- Follow link's heartbeat and reconnect-backoff behavior ([link.md](../../operations/link.md), "Heartbeat" and "Reconnection").

### 2.4 Routing and deduplication

- Each hub advertises its **directly connected** computers: `machine_uid`, machine id, name, online state, and hub→node RTT. It also forwards routes learned from other hubs, including the traversed hub path (to prevent loops).
  Full pairwise connectivity is not required: with A–B and B–C linked, A can see C's computers through B.
- Deduplicate by `machine_uid`. Route selection: fewer hops → lower RTT → lexicographic `hub_id` (for stability).

### 2.5 Ownership (when two hubs directly connect to the same computer)

A persistent node accepts only one attach at a time; a new one replaces the old one ([link.md](../../operations/link.md), "Forwarding"). If two hubs configure the same computer and both connect, they repeatedly displace each other and disconnect. Therefore:

- Only one **owning hub** in the group actually connects to the node for a given `machine_uid`; others access it through that hub rather than connecting themselves.
- Choose the owner from online hubs configured for the computer, using the lowest hub→node RTT. **Sticky ownership**: do not switch while the current owner is healthy; another hub takes over only after an outage exceeds a configured duration. No flapping.
- `machine_uid` becomes known only on the first connection: if the group already has another owner for that computer, disconnect voluntarily and become a standby.

### 2.6 Client-facing behavior (**all clients develop against this contract**)

The hub lists computers connected to other hubs in the group as its own machines and forwards all client requests for them over the mesh to the owning hub. Clients **do not need to understand mesh internals**;
existing endpoints and pushes continue to work.

- **Machine ids are unique and stable within the group**: a computer uses the configuration id assigned by the configured hub with the smallest `hub_id`. If different computers have conflicting names, the latter gets an `@<hub name slug>` suffix.
  This rule considers configuration, not online state, so route changes do not change ids. Trade-off: if two hubs give the same computer different names, one hub's clients will see its name change.
- `GET /api/v1/machines` (and the WS `machines` push) adds four fields per machine (without colliding with existing keys):

  ```json
  {"id": "macbook", "hub_id": "<owning hub's hub_id>", "hub_name": "Home", "machine_uid": "…",
   "route": {"via": "local|ssh|tailcat|link|peer", "hops": 0, "rtt_ms": 12}}
  ```

  `hops` counts the mesh links between the hub the client connects to and the owning hub (0 = directly connected to this hub). Omit `rtt_ms` if unmeasured.
- Sessions, interactions, items, terminals, files, port forwarding, uploads: unchanged shapes, with machine ids following the above rule. The hub transparently forwards requests for remote machines.
- **Hub name**: new top-level `name` field in `hub.json` (editable in the admin UI). If absent, use the hub machine's short hostname (strip `.local` / domain suffix).
  If the short hostname looks like a Docker container id (12 hex digits), or `PERSONA_LOCAL_NODE=0`, use `hub-<first 6 characters of hub_id>`.
  Code always calls `func (h *Hub) hubName() string` (`server/internal/hub/hub_info.go`, added by L6 first); use `meshHubCount()` for the number of hubs in the group (L6 initially returns 1, L1 wires it up).
- hello and `GET /api/v1/health` add `hub_name`; **authenticated** hello adds `mesh` (unauthenticated health does not):

  ```json
  {"mesh": {"hubs": [{"hub_id": "…", "name": "Home", "online": true, "self": true,
                      "endpoints": ["https://…"], "tls_port": 18662, "fp": "…"}]}}
  ```

  Lists every hub in the group (including itself, `self: true`). Clients use it for grouping and routing. Without mesh links, it contains only itself.
- **Credentials work throughout the group**: a client's device credentials work on any hub in the group. When a hub sees an unknown `jd_` credential, it asks other hubs over the mesh (results cached for 30 seconds, as now),
  with revocation determined by the issuing hub. Clients can thus switch routes between hubs in the same group without pairing each one.
- **Push**: device push registration always resides on the credential-issuing hub (`POST /api/v1/devices` on another hub forwards to the issuer). Interactions and completion events on any hub in the group
  forward over the mesh to that hub for pushing. Each device receives only one push. Push content (both direct APNs payloads and the sealed relay payload) includes top-level `hub_id`: the hub_id of the hub sending the push
  (the issuer), which lets clients identify the group. Machine ids inside it are group ids from that hub's perspective.
- **Not merged across hubs**: the Persona assistant's main chat, knowledge base, Inbox, mail, avatars, model configuration, and connectors belong to each hub. Clients show those of the hub they connect to;
  these "hub-level" pages select by group / hub.

### 2.7 Admin endpoints (the admin UI's "Linked hubs" page develops against these)

Behind admin authentication (section 5), with prefix `/api/v1/admin/peers`:

| Endpoint | Purpose |
| --- | --- |
| `GET /api/v1/admin/peers` | `{"peers":[{hub_id,name,state,address,rtt_ms,since,error,machines}],"discovered":[{hub_id,name,addresses,fp,version}]}`; `state`: `connected` / `connecting` / `offline` / `refused` (the peer rejects our credentials; display as offline + error). `machines` is the number of computers directly connected to that hub. Omit absent fields. |
| `POST /api/v1/admin/peers/probe` | Verification before manual addition: `{"address":"https://<address>:<port>"}` (or `<host>:<port>`) → `{hub_id,name,tls_port,fp,version}`. Requests only the peer's `/api/v1/health`, with no redirect following, a timeout, and a response-size limit. Without TLS, the peer returns `tls_port` 0 and an empty `fp`. |
| `POST /api/v1/admin/peers/code` | Generates this hub's linking code: `{"code":"persona://peer?…","expires_at":"…"}` |
| `POST /api/v1/admin/peers` | `{"code":"…"}` links using the peer's code; returns that peer entry on success. |
| `DELETE /api/v1/admin/peers/{hub_id}` | Disconnects and revokes credentials. |
| `GET /api/v1/admin/routes` | Debugging: all computers in the group, their owning hubs, and candidate routes. |

Wiring: the mesh workstream implements handlers as `func (h *Hub) registerPeerAdmin(mux *http.ServeMux)`; the admin UI workstream calls it from its admin routes.
Whichever workstream merges later wires the two together. If mesh merges before admin routes exist, temporarily expose it through routes restricted to loopback sources + the main token.

## 3. Computer connections

- **ssh node**: unchanged.
- **link**: current behavior in [link.md](../../operations/link.md). This round:
  - **Linux headless**: `persona-link` does not depend on a GUI and installs with one command (`persona-link install`: writes a systemd user service and prompts for `loginctl enable-linger`;
    provides alternatives without systemd), with `status` / `logs` / `uninstall`, and `pair` supporting links from stdin. Runs on servers, NAS devices, and Raspberry Pi.
  - **Mac menu bar**: shows the connected hub (`hub_name`) and the number of hubs in its linked group; otherwise unchanged.
  - **Windows tray**: handed off to a session in a Windows environment (#299). personad currently does not compile on Windows, and herdr has no Windows version; that workstream defines the approach first.
- link connects only to the hub it paired with. Switching to another hub in the group when that hub fails is **outside this round**.

## 4. Multi-hub clients (iOS #256, Android)

- Pair with multiple hubs. Store pairing information, device credentials, and push keys separately per hub; automatically migrate the old single configuration to the first hub.
- **Grouping**: two paired hubs are in the same group if either one's hello lists the other in `mesh.hubs`. Group ids are generated locally by the client and remain stable (recompute membership on merges / splits without changing existing ids).
  Pairing a hub already in a group automatically joins it to that group without duplicate display.
- **Connections**: one connection per group, using the best route. Candidate addresses from all hubs in a group participate in existing routing rules, preferring reachable, low-latency addresses; if the connected hub fails, switch to another in the group (credentials work throughout it).
  Separate connections for separate groups, with no mutual interference.
- **Data**: session keys must distinguish groups (iOS adds a group id to `SessionKey` and bumps the `ChatCache` directory version; Android adds a stable `~<group id>` suffix to non-primary groups' machine ids, stripped by the facade before sending to the hub). **Primary-group** keys (the group migrated from the old single pairing, or the first paired group) remain byte-for-byte identical. Disconnecting it leaves other groups unchanged; do not promote a new primary group.
- The **selected group** controls only which group hub-level pages use, not any session key or machine id. Switching it must not misroute open pages, notification deep links, drafts / pins / caches.
- **UI**: lists such as Agents, Machines, and interactions merge groups, label each entry's source group, and support filtering by group. **With only one group, not a single pixel changes.**
  Hub-level pages (Persona assistant, Inbox, hub-related settings) switch by group. Settings gains a "hub" list for adding, renaming, and disconnecting a group / hub.
  The Settings row leading to this list appears even with one group (users need it to add a second hub), the **only** permitted pixel change in the single-group case.
- **iOS also needs**: push, notification actions, Live Activities, Watch, Siri / Shortcuts must all carry group / hub identifiers and route to the corresponding hub (the #256 plan).
- Demo mode works as before: it is a fake group.

## 5. Hub admin UI

- Address `<hub address>/admin`, endpoint prefix `/api/v1/admin/`, with authentication **separate from** App endpoints. Pages share the web app's `web/` project (code in `web/src/admin/`).
- **Access scope**: `admin.access` in `hub.json`:

  | Value | Who can access |
  | --- | --- |
  | `loopback` | Only the hub machine itself |
  | `lan` (**default**) | Local machine + private addresses on the hub's LAN (+ `security.trusted_cidrs`) |
  | `cidrs` | Local machine + subnets listed in `admin.cidrs` |
  | `any` | Any source, but TLS is required |

  Requests outside the scope (pages and endpoints) return 404, without revealing the admin UI exists. Broadening to `any` requires the CLI on the hub machine or a deployment script; the web UI can only narrow scope.
- **First-time setup**: the first visit from within scope sets an administrator password; subsequent visits log in with it (HttpOnly, Secure session cookie with configured lifetime). Failed logins use existing `security` rate-limiting rules.
  Reset a forgotten password on the hub machine with `personad hub admin reset-password`. Administrator credentials live in Postgres (migration **0161**).
- **Two cases where the source is not trustworthy**: inside a container, its gateway address (connections through docker-proxy, potentially from the public Internet) does not count as LAN unless listed in `admin.cidrs` / `security.trusted_cidrs`;
  for loopback sources (a same-machine reverse proxy without forwarding headers makes public requests appear to come from loopback), first-time setup requires a one-time setup code (in startup logs, or obtained with `personad hub admin setup-code`).
- **Deployment prompts for scope**: `scripts/deploy.sh`, the public `install.sh`, and Docker (`PERSONA_ADMIN_ACCESS` in `.env`; if absent, the entrypoint prints a reminder and uses the default) must let users choose access scope;
  ask interactively with a terminal, otherwise use an argument / environment variable. If none is supplied, use `lan` and print a reminder.
- **Pages**:
  - Overview: version, uptime, listening / TLS / candidate addresses, doctor results;
  - Computers: list and status, add ssh computers, generate pairing links for link computers, reconnect, delete;
  - Linked hubs: discovered hubs, generate / enter linking codes, link state and RTT, disconnect (endpoints in 2.7);
  - Devices: credential list, revoke, generate a phone-pairing QR code;
  - Network and access: public connectivity / TLS / candidate addresses, admin access scope (narrowing only).
- Editing `hub.json`: atomic writes, preserve unknown fields, permissions 0600. Apply changes immediately where possible; clearly indicate when a restart is required.
- Related issues: the admin UI is one future home for #287 (simplified configuration) and #284 (hub version updates); one-click installation and updates are not in this round.

## 6. Web app

- Existing `web/` ([web.md](../../web/web.md)) already has Agents, the Persona assistant, personal Agents, Machines, and Settings. First list gaps against the phone App (iOS is authoritative, Android is a reference), document them in web.md, then close them according to the table.
- The web app is hosted by and connects to one hub, so linking naturally exposes the entire group. Machines and sessions label their hub (`hub_name`, hidden with only one hub). No multiple groups in the web app.
- Do not duplicate others' open PRs on master: #241 (scan a QR code in Settings to connect a new device), #254 (terminal engine switching and Orca launchers).

## 7. Workstreams

| Workstream | Scope | Issue | Worktree (`~/code/self/persona-mesh/`) | Exclusive resources |
| --- | --- | --- | --- | --- |
| L1 | Linked hubs (sections 1, 2) | #294 | `l1-mesh` | Migration 0151, test database `persona_test_mesh` |
| L2 | iOS multi-hub (section 4) | #256 | `l2-ios` | Its own simulator, mock hub ports 18760–18762; adds mesh fields to `scripts/mock/mock_hub.py` |
| L3 | Android multi-hub (section 4) | #295 | `l3-android` | Its own AVD, mock hub ports 18770–18772; does not edit `mock_hub.py` (ask L2 / coordinator for changes) |
| L4 | Admin UI + deployment scope selection (section 5) | #296 | `l4-admin` | Migration 0161, test database `persona_test_admin`, `web/src/admin/`, dev server port 5274 |
| L5 | Web app parity (section 6) | #297 | `l5-web` | Dev server port 5275, mock hub port 18780 |
| L6 | links: Linux headless, Mac menu bar (section 3) | #298 | `l6-link` | — |
| — | links: Windows tray | #299 | Handed off to a session in a Windows environment | — |

- Each workstream depends only on this document's contracts, not other workstreams' implementations. Use stubs or mocks against the contract when another workstream has not yet built what is needed.
- Shared files (`server/internal/hub/http.go`, `config.go`, `web/` routing entry points, `CLAUDE.md`, `docs/` index) get only minimal changes; new code goes in new files. Resolve only conflicting blocks during merges.
- Contract changes must first update this document (a separate commit), then notify other workstreams.

## 8. Outside this round

- link switching to another hub in the group when the owning hub goes down;
- Cross-hub merging of assistant main chats, Inboxes, and knowledge bases;
- Windows tray implementation (handed off);
- One-click installation (#287), hub version updates (#284).
