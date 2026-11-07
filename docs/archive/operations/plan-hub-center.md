# Plan (v2): a single primary hub, preinstalled services, multiple paths to one computer

Umbrella issue #293. On 2026-11-02, after reviewing the first version ([plan-hub-mesh.md](plan-hub-mesh.md); peer-to-peer linking is already merged into the integration branch, see [mesh.md](../../server/mesh.md)), the user corrected it:

1. **There is always exactly one primary hub**. Hubs still discover and link to each other, but once linked one is chosen as primary; the other hubs degrade into nodes and hand their computers over to the primary.
   A client connecting to any hub in the group is forwarded to the primary.
2. **Hubs and computers connect through a preinstalled service, not SSH** (SSH grants too much). A computer can connect to the hub, or be connected to by the hub.
3. **Dedup and best-route selection** means: when one computer is reachable over several paths, it appears only once, uses the best path, and switches to another when that one breaks.

This document replaces "route announcement, ownership, transparent peer forwarding" in section 2 of plan-hub-mesh.md and the ssh part of its section 3. Its section 1 (`machine_uid`),
2.1–2.3 (discovery, link-code pairing, link channel), and sections 4–6 (multi-hub clients, admin UI, web) stand. Baseline and merge method are unchanged: integration branch `feat/jolly-cerf-dfb019`.

## 0. Recommended deployment: one hub, the node service on every other computer

The user added on 2026-11-02: **recommend that users install only one hub and use the node service on every other computer**.

- **hub**: installed on an always-on machine (a NAS, a home server); one for the whole household. The machine it runs on is also a computer (the local node).
- **node service**: installed once on every other computer. It contains a resident node plus the connector, **installed with one command or one app**, and after pairing it connects to the hub actively or passively (section 4):
  - Linux: choose "node" in `install.sh`; one command installs both systemd user services and pairs;
  - Mac: the Persona Link menu bar app bundles persona-node (the device's program) and installs the resident node on first run (no need to run `scripts/deploy/node-launchd.sh` separately);
  - Windows: #299.
- **Linking multiple hubs** (primary + members) is only for the few cases that need it: e.g. two places each with an always-on machine that can't reach each other, or several hubs already installed that you want to merge.
  Docs, installers, and the admin UI all guide toward "one hub + node services": the admin UI centers on "Add computer" and puts "Linking" under "Advanced";
  when the installer can already find a hub on the same subnet, it suggests "the node service is enough for this machine".

## 1. Roles

```text
 phone / web ──► primary hub ◄══ link channel ══ member hub ◄── phone (connecting to a member works too: it forwards to the primary)
                  │    ▲                           │    ▲
  hub connects    │    │ computer connects         │    │   computers attached to a member: the primary reaches them over the member's link
   (passive)      ▼    │ (active)                  ▼    │
                 service preinstalled on the computer: resident node + connector persona-link
```

| Role | What it does |
| --- | --- |
| **Primary hub** | The only one in the group. Serves all clients and holds all state (session index, interactions, push, device credentials, assistant, Inbox, knowledge base, model config, scheduled tasks…), and connects to every computer (directly, or through member hubs) |
| **Member hub** | Does only two things: forwards client requests to the primary unchanged, and relays the node streams of the computers attached to it to the primary. It no longer drives nodes or serves clients itself; its own assistant / Inbox / scheduled tasks are suspended (the data stays and comes back after it leaves the group) |
| **Computer** | Preinstalled resident node (`personad node`, which holds all the functionality) and the connector persona-link (only discovery, reporting and the link; it passes everything through to `~/.persona/node-attach.sock` unchanged and carries no functionality, per the user's 2026-10-31 requirement) |

With a single hub and no linking, it is the primary and everything works as it does today.

## 2. Choosing the primary

- **A link code is an "invite to join my group"**: when A pastes B's link code, A (together with all hubs in A's group) joins B's group, and the primary is B's group's primary.
- **Switching the primary manually**: "Set as primary" in the admin UI. If the old primary is online, the two hand over; if it's offline, the user must confirm, and is told the old primary becomes a member when it comes back.
- **No automatic switch when the primary is unreachable** (to avoid two primaries at once): members answer clients with `503 center_unreachable` (with the primary's name), and the admin UI suggests a manual "Set as primary".
- **Term**: every hub records the group members, the primary's hub_id, and an increasing term number; a manual switch increments the term. The higher term wins: an old primary that comes back online sees a higher term and steps down to member.
- State is stored in Postgres (new migration; before starting, check all worktrees and branches, take the highest number and add 10).
## 3. Members forward clients

- Client requests a member receives (all of `/api/`, including WebSocket; except the web static files) are forwarded unchanged to the primary over the link channel, keeping `Authorization`.
  Source classification (public / LAN, whether TLS is required, rate limiting) is done on the member, and the verdict is passed to the primary with the request; the primary trusts the member's verdict (the member is paired).
- The admin UI `/admin` is not forwarded: each hub's admin UI manages that hub. A member's admin UI says at the top "This hub is a member; the group is managed by <primary>", and group matters are handled in the primary's admin UI.
- **Device credentials**: when a member joins a group, it hands the device credentials it has issued (hash, name, role, device id) to the primary, which takes them over and from then on issues and revokes them;
  the member no longer validates credentials itself (it forwards everything to the primary). Phones already paired with the member don't need to pair again.
- **Push**: only the primary sends; devices register with the primary; the `hub_id` in pushes is the primary's.

## 4. Connecting computers: preinstalled service, no SSH

- Every computer has two things preinstalled: the resident node (a launchd / systemd service; Windows see #299) and the connector persona-link (what the Mac menu bar app, Linux headless, and the Windows tray carry).
- **One connector, two modes** (chosen at pairing, changeable later):

  | Mode | Who connects to whom | Suited to |
  | --- | --- | --- |
  | **Active** (current, [link.md](../../operations/link.md)) | The connector dials the hub's WSS `/api/v1/link` | Laptops, computers behind NAT or switching networks |
  | **Passive** (new) | The hub dials a TLS port the connector listens on locally | Computers with a fixed address the hub can reach (servers, dev workstations) |

- **Passive mode protocol** (both lanes follow this; the connector lane adds the details to link.md):
  - The connector listens on `listen` (default `:18663`). TLS uses the connector's own ECDSA P-256 identity key and a self-signed certificate (`~/.persona/link-tls/`, 0700 / 0600);
    the fingerprint uses the same algorithm as the hub's (SHA-256 of the SPKI DER, base64url without padding).
  - `GET /api/v1/connector/info`: `{"machine": "<machine id the hub gave it>", "version": "…"}`.
  - `GET /api/v1/connector/stream`: after the WebSocket upgrade, passed through unchanged to `~/.persona/node-attach.sock` (permessage-deflate, both sides ping), same as the active-mode stream.
  - `POST /api/v1/connector/pair` (used for pairing; the only endpoint that doesn't need a long-term credential): `{"code":"<one-time code>","token":"<long-term credential generated by the hub>","machine":"<machine id>","hub_id":"…","hub_name":"…"}`
    → if the code is right, store the token's hash and the hub info, invalidate the code, and answer `{"version":"…"}`; a wrong or expired code gets 401 (counted toward rate limiting). Pairing an already paired connector again replaces the old hub.
  - Both info and stream require `Authorization: Bearer <hub's credential>`; the connector stores only the credential's hash and only accepts the hub that paired it (the group's primary; when the primary changes, the new primary pairs again).
    Authentication failures are rate-limited per source. Nothing beyond these endpoints is offered: no shell, files, or port forwarding; it can only reach node-attach.sock.
  - The hub pins the connector fingerprint recorded at pairing.
- **Passive-mode pairing goes the other direction** (the computer can't necessarily reach the hub):
  1. On the computer, `persona-link pair --listen [:18663]` (in the menu bar / tray, choose "Let the hub connect"): generates a one-time pairing code and prints the link
     `persona://connector?addr=<candidate addresses>&fp=<connector fingerprint>&code=<one-time code>`, valid for 10 minutes.
  2. In the hub admin UI, paste the link into **Computers › Add computer › Hub connects** (you can edit the address and set a proxy): the hub connects with the pinned fingerprint and trades the one-time code for a long-term credential
     (the hub generates a random credential and hands it to the connector, which stores the hash), then opens the stream to node as usual.
  3. The hub stores the credential encrypted in Postgres (the same AES-GCM approach as mail credentials), not in `hub.json`.
- **`machines` in hub.json**: kind `link` (active, current) and a new kind `service` (passive: `address`, optional `proxy` (`socks5://…`, the hub connects through the proxy;
  e.g. a dev workstation only reachable through SOCKS on some machine)).
- **SSH**: no longer recommended; the admin UI no longer offers "Add ssh computer". Existing kind `ssh` / `tailcat` machines keep working (compatibility), and the docs describe migrating to service / link. To be removed later.
- **node updates**: binaries can no longer be pushed over SSH. This round: "Redeploy" on service / link machines becomes a prompt to "update on that computer"; pushing updates through the connector is left to #284 (hub version updates).

## 5. Multiple paths to one computer: dedup and route selection

- The primary dedups by `machine_uid`. One computer can have several paths: several in the config (e.g. both service and link configured), and through each member hub. It appears only once in the machine list.
- Only one path is used at a time (the resident node accepts a single attach; a new one kicks the old one). Route selection: local > direct (service / link / ssh / tailcat) > through a member hub; within a class, by the most recently measured RTT.
  **Sticky**: while the current path is healthy, don't switch; when it breaks, switch to the next immediately; a better path must stay stable for a while (configurable) before switching to it. No flapping.
- **Machine id**: a computer configured in the primary's `hub.json` uses its config id; a computer configured only on a member uses the member's config id, with `@<member name>` appended if it collides with a name on the primary. Old nodes have no `machine_uid`: no dedup.
- **Machine list fields** (client contract; replaces the meaning of `hub_id` / `route` from plan-hub-mesh 2.6, key names unchanged):
  - `hub_id` / `hub_name`: the hub this computer currently goes through (the primary itself for direct connections, the member for connections through a member);
  - `route`: `{"via": "local|service|link|ssh|tailcat|hub", "hops": 0|1, "rtt_ms": 12}`;
  - new optional `paths`: one item per path, `{"via": "…", "hub_id": "…", "ok": true, "rtt_ms": 12, "active": true}`, for the admin UI and machine details.
- Each item of `mesh.hubs[]` in hello gets `"center": true|false`; among hubs in the same group, clients prefer connecting to the primary (connecting to a member also works, with one extra hop).

## 6. Admin API additions (on top of plan-hub-mesh 2.7)

| Endpoint | Purpose |
| --- | --- |
| `GET /api/v1/admin/mesh` | `{"role":"center|member","center":{"hub_id","name","online"},"term":3}` |
| `GET /api/v1/admin/peers` | Adds `role` to each item |
| `POST /api/v1/admin/peers/{hub_id}/center` | Set as primary (requires `{"confirm":true}` when the old primary is offline) |
| `POST /api/v1/admin/machines` | Supports kind `service`: `{"pair_link":"persona://connector?…","id","name","address"?,"proxy"?}` |
| `GET /api/v1/admin/machines` | Each machine includes `paths` |

## 7. Division of work

| Lane | Scope | issue | worktree (`~/code/self/persona-mesh/`) |
| --- | --- | --- | --- |
| H | Hub side: multi-path machine model and route selection; kind `service` (dial the connector, pinning, encrypted credential storage, socks5 proxy); SSH demoted to compatibility; single-primary rework (choosing the primary, terms, members forwarding clients, members handing computers to the primary, suspending members' own services, moving credentials to the primary, set as primary); admin API and the matching admin UI pages (`web/src/admin/`); docs (rewrite `../../server/mesh.md`, `../../server/server.md`, `../../operations/config.md`, `../../protocol/protocol.md`) | #435 | `h-center` |
| C | Connector: passive mode (listening, TLS identity, pairing code, auth and rate limiting, pass-through to node-attach.sock only), `pair --listen`, mode switching; Mac menu bar shows the mode and listen address; Linux headless install supports passive mode; docs `../../operations/link.md`. Doesn't touch `server/internal/hub/` | #436 | `c-connector` |

- The passive-mode handshake follows section 4 of this document; the connector lane writes the details into `../../operations/link.md`, and the hub lane implements the dialing side from it. Protocol changes go into this document first.
- Windows (#299): passive mode applies as well; this has been passed on to the Windows session.
- Clients: "prefer the primary within a group" is a small change, done after H merges (iOS merged, Android #396 pending).

## 8. Not in this round

- Automatic primary failover; pushing node updates through the connector (#284); removing SSH support.
