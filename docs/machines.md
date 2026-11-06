# Machine monitoring

Each computer's OS, current load and curves for the past 24 hours. The node collects, the manager (the one connecting to computers; see "Who connects to computers" below) stores in Postgres, and the hub forwards App requests to it. The App shows these on **Machines** and machine details alongside Agent counts.

| Part | Code |
| --- | --- |
| Collection (parsing /proc, vm_stat, iostat, sw_vers, Win32 responses, etc.) | `server/internal/sysmon/` (`sysmon.go`, `winparse.go` pure parsing; `linux.go` / `darwin.go` / `windows.go` read the OS) |
| Node sending | `server/internal/node/metrics.go` |
| Manager storage, downsampling, cleanup, API (hub forwards `devicewire.MetricsRoutes`) | `server/internal/devices/metrics.go`, `server/internal/db/metrics.go`, migration `0051_machine_metrics.sql` |
| App | `apple/Persona/Views/Machines/` (`MachinesView`, `MachineDetailView`, `MetricCharts`, `OSBadge`), model `apple/Shared/MachineMetrics.swift` |
| Inspect local samples / overhead | `jarvisd sysmon [n]`: prints system info and n+1 samples (one every 10 seconds, n ≤ 8640, one day); final line gives actual sample count, collection duration and personad CPU (Ctrl-C stops immediately) |

## Who connects to computers (2026-11-04)

Product decision: **computers belong to the manager, which connects to them itself**; the hub sees no computers directly, so the bus has nothing to gate between computers and manager (physical separation). During migration there is only one manager, the chief manager (assistant process `personad assistant`), holding all computers.

| Part | Code |
| --- | --- |
| Manager: connections, per-computer state, routing, Persona Link protocol, kind service, tailcat, warm start, engines, herdr auto-start, Claude config directories, permanently listed sessions, node updates | `server/internal/devices/` (`devices.go` entry/reporting, `machine.go`, `conn.go`, `paths.go`, `link.go`, `relay.go`, `service.go`, `tailcat.go`, `warm.go`, `engine.go`, `herdr.go`, `claudedirs.go`, `deploy*.go`, `bus.go`) |
| Shared messages | `server/internal/devicewire/` (method names, parameters, errors, manager-owned route table `Routes`) |
| Hub: forwarding, retained snapshot, its own on-connect work | `server/internal/hub/devices_link.go`, `devices_relay.go`, `machines.go`, `machine.go` (`Machine` is now the manager's report plus hub-owned data) |
| Wiring in the assistant process | `server/internal/assistant/main.go`, `devices.go` (several places in `bus.go` only call it) |

**Manager responsibilities** (`internal/devices`; neither it nor hub imports the other):

- Connect each entry in hub.json `machines`: the hub machine's own node (`node --stdio` child, now the manager's child), SSH, tailcat (hub's tailcat key file, same HOME), kind service (dial the helper; credentials still encrypted by the hub with its key, retrieved over the bus via `devices.connector_cred`, returned for storage after pairing via `devices.connector_paired`),
  kind link (Persona Link still connects to the hub address, forwarded unchanged to the manager; below), and computers reached through member hubs in the linked group (routing, de-duplication and path changes in manager `paths.go`; the hub opens and relays streams through another hub).
- Per-computer state: online, `node.info`, session list and revs, sessions watched by Apps (repeat `session.watch` after node reconnection, resuming from the last item), warm start, engine (settings `machine_engines`), herdr auto-start, Claude config directories (and blocked ones),
  sessions the node must list regardless of inactivity (assistant profile `keep`; when the assistant snapshot arrives, tell each node, `SetKeep`), node updates (`node.deploy.<id>`), node `state.put` / `state.load` (table `node_sessions`).
  Existing keys and tables stay; no data migration.
- Manager-owned routes (`devicewire.Routes`, forwarded by hub as `http.request` like assistant routes): `POST …/reconnect`, `PUT …/engine`, `GET/PUT …/herdr`, `POST …/herdr/start`, `POST …/deploy`, `GET …/claude-dirs`, `PUT …/claude-dirs/blocked`; also subscription quota `GET /api/v1/usage/windows` (`devicewire.UsageRoutes`) and admin quota-source page (`AdminUsageRoutes`, forwarded only from admin, `docs/usage-windows.md`, "Who owns this").
- Node requests: answers `state.load`, assistant requests (`assistant.model`, `assistant.context`) and computer-event requests (`interaction.create`, `assistant.lark`, `mcp.tools`, `mcp.call`; "Who handles computer events" below) in-process; passes the rest (`notify`: `personad notify` on computers) to hub (`devices.request`).
  If that node connection closes before the hub answers, the manager sends `devices.request_end`, so the hub stops waiting.
  One-way node events are first processed by the manager (`devices.Options.Event`), then App-needed events forwarded unchanged in order to the hub (`devices.event`): items, screen, live state, terminal frames, port bytes, `sessions.revs`, `hook.event`, `fs.changed`, plugin events; session lists, interactions, usage and monitoring samples stop at the manager.
- One-way hub-to-node events (`devices.notify`) queue per computer, with independent writers; a slow computer delays only itself, not the bus or others. Reconnect if a write stalls 30 seconds or queued data exceeds 32 MiB.
- When a machine entry changes in hub.json (hub restarts after config edits while manager stays running), create a fresh machine object and reconnect (rewatch App-watched sessions), rather than editing config underneath an active connection.
- Assistant calls to machines (node methods in `device.call`) run directly in-process, no hub round trip; hub-owned methods (`devices.list` including phone online state, send-once `session.send_once`) still ask hub. Since 2026-11-05 the manager answers `interactions.list`, `sessions.list`, `session.get`, channel `session.follow` / `session.unfollow` itself ("Who handles computer events"). Sending "continue" for Agent triage, starting Agents for the assistant and opening scheduled-task chats all call the node directly in-process.

**Hub responsibilities**:

- Hand hub.json `machines` (plus computers added live through admin) to the manager as defaults: `devices` in handshake response, then `devices.config` on changes (computer added, linked group changed, hub addresses or name changed).
- Keep the manager's report (`devices.state`: per-computer snapshots with monitoring `system`, `node.info`, connection state/start, warm deadline and paths), list them to Apps in its order, then add manager extensions' `computer` / `browser` reports (Cua, ego plugin state, `extensions.report`, `docs/extensions.md`, "Who owns what").
- App machine calls and streams still pass through hub: `devices.call` (deadline included), node-bound one-way events (terminal input, forwarded port bytes) via `devices.notify`, node events (items, terminal frames, port bytes…) via `devices.event`,
  then pushed to subscribed Apps. Interactions, state and pushes belong to the manager ("Who handles computer events"). Thus terminal, port forwarding, screen and downloads all use this one bus connection, no extra channel.
  The manager deadline is shortened by one tenth, at most 500 milliseconds, so its timeout arrives first; calls without deadlines (plugin `nodes.call`) aren't given one. Failures after dispatch—node disconnect, deadline or bus disconnect—mean "may have executed":
  message results are `unknown`, and the same message isn't resent (same as when hub directly connected to node); only definite non-dispatch (offline, manager absent) counts as not sent.
- Hub self-update confirmation that previously online computers all reconnected, and subsequent node updates through the manager, use reported `connected` (actual node connection), not App-visible online (also true during warm start after manager restart).
- Every new manager-hub handshake ends both sides of forwarded streams and terminals opened through the previous connection: hub closes its side (terminal ends with "Can’t reach the assistant right now. Try again later", reopened by App), manager closes node-side `stream.close` / `terminal.unwatch`, avoiding abandoned port connections and terminal frames after hub restart.
  Hub asks the new connection to rewatch App-visible sessions, at most 8 concurrently; a slow computer doesn't block others.
- On node connection, the manager first asks the hub for its part (`devices.connected`): additions to `node.configure` (Codex settings) and immediately subsequent sends (managed launchers, remembered messages), then asks for the session list. Hub also starts its own on-connect work (monitoring system info); Cua / ego state, extension programs on computers, connectors and extension Agent filtering added to `node.configure` belong to manager extensions (2026-11-05, `docs/extensions.md`, "Who owns what"), even while hub is away.
  Every new manager-hub connection repeats this step for each connected computer.
- When a machine connection ends (reported `connected` false or `connected_at` changes), hub ends its terminal streams and generating text; manager ends pending non-resumable interactions (`devices.Options.Detached`).

**Stream forwarding**: App ↔ hub ↔ manager ↔ node; hub-manager uses the bus over local loopback. Measured (isolated hub, hub's own computer, 32-byte round trips over a forwarded port, 300 times, 2026-11-04): master median 0.3–1.2 ms, p95 about 1.5 ms; this version median 0.9–1.4 ms, p95 about 1.9 ms, adding about 0.2–0.4 ms (one loopback hop and encode/decode).
Terminal frames use the same path (node event → manager → hub → App; input in reverse).

**Persona Link (active mode)**: still connects to the hub address with unchanged pairing codes and link credentials. Hub authenticates (kind-link machine, TLS), then forwards the WebSocket unchanged:
`devices.accept {id, kind: link, machine_id, token_id}`; the manager dials `GET /api/v1/bus/relay?id=<id>` with its bus token; hub forwards messages individually (text: Persona Link messages, binary: node bytes), preserving close codes and reasons.
hello, `endpoints`, `open`, `node` come from the manager; Persona Link streams (`/api/v1/link/stream`) are likewise forwarded. Heartbeat ping runs between hub and Persona Link; missing pong closes both sides and passes the reason to the manager. Interval and timeout are configurable in hub.json `link`, and the manager tells Persona Link in `endpoints` ([link.md](link.md), "Heartbeat").
Credential revocation closes the relayed link with 4002; absent manager returns 503 on `/api/v1/link`, with normal Persona Link backoff and automatic reconnection when the manager returns.

**Linked hubs (mesh)**: groups, primary hub, records, credential handoff and member App forwarding remain hub-owned (`docs/mesh.md`); computers and paths belong to the manager. Hub provides group state in `devices.config.mesh` (whether this hub is a member, primary name, startup wait, other hubs' reachability and distance, member computers),
which the manager uses for de-duplication, routing and path changes. The primary hub opens the member route (`GET /api/v1/bus/relay?hub=<member>&machine=<id>`, opens a node stream over mesh and relays to manager); a member receiving the primary's node-stream request (`/api/v1/peer/node`) delegates to its own manager (`devices.accept` kind node).
Only after the manager opens the node does the member return `101`; failure returns 502 with the reason, displayed on that machine by the primary (as when members opened nodes themselves).
Persona Link RTT comes from hub heartbeats, reported to manager (`devices.link_rtt`) and used for route distances and records.
Reachability and distance of the hub's own computers in its records come from manager reports (`entries` in `devices.state`).

**While the manager is away** (restarting or crashed):

- All machines show offline, `error` is "Can’t reach the assistant right now. Try again later", sessions become unknown (hub keeps the last list, sessions with pending cards stay **Needs you**); terminal streams end with the same message and forwarded ports close. Hub keeps listing the last cards, but answers fail (`503 assistant_offline`).
- Manager-dependent endpoints (items, send, create, files, git, terminal, ports, manager-owned routes…) return `503 {"code":"assistant_offline"}`; `GET /api/v1/machines` and `GET /api/v1/sessions` return hub's retained snapshot normally.
- When the manager returns (hub restarts it with backoff beginning at 1 second), it reconnects every computer; hub requests rewatch of App-visible sessions automatically. Persona Link reconnects on its next retry.
  Calls to a machine still connecting after startup (hub-held card answers, new App Agents) wait for its first connection attempt to finish (up to 20 seconds), rather than immediately returning offline.
- No new Agents, sending messages or observing Agent turn completion while manager is absent: all need its node connection (previously hub connected computers itself, so Agents could still start without assistant).
- A resident node keeps what its Agents' hooks and `personad notify` send meanwhile, and permission / question hooks wait up to two minutes for the manager instead of dropping to the terminal; the next connection replays it in order, once (`protocol.md` ("When the manager is away")).

**Bus sizes**: other messages, including heartbeats, wait until a hub-manager message finishes writing, so large payloads are chunked: downloads, uploads and node update packages ≤ 1 MiB per chunk, forwarded port bytes 32 KB;
an items page, image or screenshot is one message (same as the former node connection). Maximum bus line is 64 MiB; manager doesn't forward node messages over 60 MiB (drops/logs events, calls return `too_large`), avoiding a bus disconnect.

**Keys**: Persona Link credentials stop at hub (after authentication, manager receives only credential id and bound machine); pairing codes and pairing stay hub-owned. Kind-service helper credentials are needed for manager dialing:
manager generates them during pairing, hands to hub for encrypted storage, then retrieves once and caches in memory (retrieves again only after rejection, re-pairing or pairing to another hub), not on every probe.

**Hub restart**: the manager is a hub child and restarts too. Before stopping, it hands online machines' `node.info` and session lists to its next instance (its settings `hub.warm_machines`), then disconnects; the hub keeps what it saw reported in its own settings (`hub.devices_warm`, [hub-restart.md](hub-restart.md));
the new hub takes its own snapshot to display machines (including engines), and calls to them wait for manager's first report (up to 30 seconds). Manager consumes its snapshot, shows machines online for 30 seconds and reconnects nodes.
Hub listening starts before manager: App requests and manager-owned routes arriving during these seconds wait (up to 30 seconds); Persona Link links and primary node streams arriving at member hubs wait up to 5 seconds (Persona Link gives a handshake 8 seconds),
then return 503 if not ready (normal Persona Link backoff). After this startup window, absent manager returns 503 immediately, without waiting.

Observable differences from before:

- Absent manager means offline machines and 503 assistant_offline (formerly hub connected computers independently of assistant restarts).
- On machine connection, Cua / ego state and extension programs start a little later (after hub answers `devices.connected`); `waitNodeIdle` before node updates uses node-listed sessions and unanswered questions forwarded by manager to hub, no longer **Needs you** on hub cards.
- With `assistant.external` (manager on another computer), that computer makes connections: its SSH config, tailcat key, `node --stdio` binary and node update packages are used;
  hub restart doesn't restart it (computers stay connected), but for the second or two before it reconnects to the bus, hub shows machines from hub.json as offline.

## Who handles computer events (2026-11-05)

Computers belong to the manager, and so does everything they send: the hub's existing code moved unchanged into the manager (`server/internal/assistant/fleet*.go`, monitoring in `server/internal/devices/metrics.go`); the hub only forwards and delivers.

| Manager responsibility | Code |
| --- | --- |
| Interactions: waiting for `interaction.create`, resumable cards (retain for 3 minutes after node disconnect for hook reclaim; recover from `interactions` after manager restart, without another push on reclaim), expiry, always allow for session, screen cards (read, keypress, recheck), Codex cards, plans answered in terminal, history table, assistant questions/cards, questions from hub-owned features (`fleet.ask`: sending mail, Cua apps, jarvis_settings) | `fleet_ix.go` |
| Session lists: unified state, background tasks, model errors, seen (`session_seen`), state start time, to-do / quiet / work state / channel markers / assistant-chat titles | `fleet.go`, `fleet_work.go`, `fleet_text.go` |
| Push decisions: waiting and filtering for done notifications (seen, muted, answered through a channel, Inbox twin, notify_user already pushed, no duplicate push for one Stop), card notifications | `fleet_push.go` |
| Live Activity content (FleetState) and Dynamic Island counts | `fleet_live.go` |
| Node MCP tools (`mcp.tools` / `mcp.call` dispatch) and `assistant.lark` | `fleet_tools.go` |
| Channel-followed chats (`session.follow`: node sends items and generating text to manager, delivered to channels with turn starts/ends in node order) | `fleet_follow.go`, `devices.Follow` |
| Machine monitoring (samples, 24-hour history, downsampling, cleanup) and token usage | `devices/metrics.go` |

**Hub responsibilities** (forwarding and delivery only, `server/internal/hub/fleet_link.go`):

- Keep manager-provided session lists per machine (`fleet.sessions`, machine `sessions`), pending interactions (`fleet.interaction` / `fleet.interaction_resolved`, replaced in full by `fleet.interactions` after each handshake), and Live Activity content (`fleet.live`). App `GET /api/v1/sessions`, `GET /api/v1/interactions`, connection snapshots and pushed events use these; while manager is absent, use the last copy.
- Deliver pushes: manager decides content (`notify.request`, `kind` `assistant` / `done` / `input`); hub selects devices according to their settings, localizes to device language, sends through APNs / relay, sends `notify` to `ws_notify` Apps, and informs extensions (`agent.finished`, `agent.needs_you`). Live Activity delivery cadence (per-device intervals, start/end) also belongs to hub; content is computed by manager.
- Forward App answer, history, seen and monitoring routes to manager (`http.request`): `POST /api/v1/interactions/{id}/resolve` (hub first checks answer-only devices), `GET /api/v1/interactions/history` (hub supplies `X-Persona-Elicitation` for App MCP-form support), `POST /api/v1/sessions/{mid}/{sid}/seen`, `GET /api/v1/metrics`, `GET /api/v1/machines/{id}/metrics`, `GET /api/v1/usage`.
- Tell manager about Apps: any MCP-form-capable App (`fleet.clients`), WebSocket `seen` (`fleet.seen`), mute changes (`fleet.mutes`).
- Run what still belongs to hub for manager: phone tools (`app.call phone`: list tools, run on connected phones, silent push wake-up when needed). Connector / Cua / ego tools, extension descriptions for write-action cards, lark-cli settings and owner actions for channel card answers belong to manager extensions (`internal/extmgr`, queried directly in-process and cancelled when node stops waiting; `docs/extensions.md`, "Who owns what").

While manager is away: lists and cards use the last copy; answering, seen, history and monitoring return `503 assistant_offline`; nothing is lost. On return, it first hands full interaction and session lists to hub, then hooks reclaim resumable cards (same card, no new push).

Observable differences:

- Manager cards (triage cards, assistant questions) previously had answers held by hub until manager returned; now cards live in manager, so answers fail with 503 while absent and work normally after return.
- A newly opened session (`POST /api/v1/sessions`) returns `state` only after hub waits for manager to list it (up to 2 seconds); omitted if not available.

## System information

The node reads this once at startup and sends `system` in `node.info` to the manager, which stores it in `machine_systems` (so OS information survives offline periods and restarts),
includes it in reported `Machine.system`, and the hub sends it to Apps.

| Field | Linux | macOS | Windows |
| --- | --- | --- | --- |
| `distro` / `like` / `name` / `version` | `ID` / `ID_LIKE` / `PRETTY_NAME` / `VERSION_ID` from `/etc/os-release` (fallback `/usr/lib/os-release`) | `macos`, ProductName + ProductVersion and BuildVersion from `sw_vers` | `windows`, ProductName + DisplayVersion from registry `HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion` ("Windows 11 Home 25H2"; Windows 11's ProductName still says Windows 10, corrected when CurrentBuild ≥ 22000); `build` is CurrentBuild.UBR |
| `kernel` | `/proc/sys/kernel/osrelease` | `Darwin ` + `kern.osrelease` | `NT ` + version from `RtlGetVersion` (`NT 10.0.26200`) |
| `model` | DMI `sys_vendor` + `product_name` (strips placeholders such as "To be filled by O.E.M.") | Model Name from `system_profiler SPHardwareDataType` (only this line; same output contains serial number), fallback `hw.model` | SystemManufacturer + SystemProductName from registry `HARDWARE\DESCRIPTION\System\BIOS` (same placeholder removal) |
| `cpu` | First `model name` in `/proc/cpuinfo` (`Hardware` / `Model` on ARM boards) | `machdep.cpu.brand_string` | ProcessorNameString from registry `CentralProcessor\0` |
| `cores` / `memory` / `boot_time` | `runtime.NumCPU`, `MemTotal`, `btime` in `/proc/stat` | `runtime.NumCPU`, `hw.memsize`, `kern.boottime` | `runtime.NumCPU`, total physical memory from `GlobalMemoryStatusEx`, now minus milliseconds since boot |

OS icons (`OSBadge`): macOS uses the SF Symbols Apple logo; Linux distributions use their official logos (Simple Icons vectors, `Theme.Brand.os`),
tinted with distribution colors. Unknown distributions fall back to `ID_LIKE` (Pop!_OS → Ubuntu), then the generic Linux penguin. Vector assets stay clear at every size and in light/dark.
(Before 2026-10-31 these were drawn circles and letters; the user requested official logos.)

## Collection definitions

The node samples every 10 seconds (`proto.MetricEvery`, aligned to whole 10-second boundaries so all machines' points share timestamps), sending a `metrics` event to the manager.
The first sample lacks CPU and network rates (need two readings); fields are omitted, not zero.

| Metric | Linux | macOS | Windows |
| --- | --- | --- | --- |
| CPU | Difference of `/proc/stat` readings: busy = total − idle − iowait (I/O wait is idle, like top id + wa; guest is already in user, not counted again); same per-core calculation, latest sample only (not stored) | Resident `iostat -n 0 -w 10`, latest line's 100 − id (past 10-second average; first line is since-boot average, discarded) | Difference of `GetSystemTimes`: busy = kernel + user − idle (kernel includes idle, DPC and interrupts); per-core via `NtQuerySystemInformation(SystemProcessorPerformanceInformation)`, latest sample only; no per-core above 64 logical processors (processor groups) |
| Load | First column of `/proc/loadavg` | `sysctl vm.loadavg` (binary struct loadavg) | None (Windows has no load average), omitted |
| Memory | Used = MemTotal − MemAvailable (page cache available, like `free` available); old kernels without MemAvailable use free + buffers + cached. In a container (the v2 `memory.max` / v1 `memory.limit_in_bytes` under `/sys/fs/cgroup` has a limit below MemTotal) the total is that limit and used = usage − inactive_file (like `docker stats` and `kubectl top`): without lxcfs, `/proc/meminfo` in a container is the host's; the system info's memory uses the same limit | Resident `vm_stat 10`: used = anonymous pages − purgeable + wired + compressor size, matching Activity Monitor **Memory Used** (App Memory + Wired Memory + Compressed); file cache available | `GlobalMemoryStatusEx`: used = total − available (standby-list file cache is available, like Linux MemAvailable) |
| Swap | SwapTotal − SwapFree | `sysctl vm.swapusage` | Page files: sum size and used amount from `NtQuerySystemInformation(SystemPageFileInformation)` |
| Network | Physical-interface receive/send byte differences in `/proc/net/dev` / elapsed time | Physical-interface differences from routing socket `NET_RT_IFLIST2` (`if_msghdr2` 64-bit counters) / elapsed time | InOctets / OutOctets from `GetIfEntry2Ex`, physical-interface differences / elapsed time; eligible interfaces listed once a minute (~7 ms to list, ~0.2 ms to read counters), discard sample when set changes |
| Disk | Sum `statfs` for local block devices in `/proc/self/mountinfo` (`/dev/…`, excluding loop and read-only squashfs / iso9660 images, counting bind mounts of a device once): data disks often mount at `/data00`, so `/` alone makes a 5 TB machine show 118 GB. A mount whose root is not `/` (one directory of a device) is skipped, except `/` itself and btrfs subvolumes: in a container, Kubernetes / Docker bind directories of the host's data disk in (an emptyDir on `/tmp`, `/etc/hosts`), and that disk is shared by every container on the host; without mountinfo it falls back to `/proc/self/mounts`; used = total blocks − free blocks, total = used + ordinary-user available (like df) | `statfs("/System/Volumes/Data")` (`/` is a read-only system snapshot, only a few GB) | Sum `GetDiskFreeSpaceEx` for fixed drives actually on disks (`GetDriveType` = `DRIVE_FIXED`, volume has a positive-length disk extent, each volume once); used = total − free, total = used + user available (like df); excludes network, USB, optical and sync-client virtual drives such as Google Drive (claims FAT32 fixed drive and copies C capacity, but disk extent length is 0); drive letters listed once a minute |

Why macOS starts two resident processes: builds use `CGO_ENABLED=0`, so Mach calls such as `host_statistics` are unavailable; `vm.page_*` sysctls lack anonymous-page and compressor counts.
Each process prints one line every 10 seconds, much cheaper than starting `vm_stat` / `top` each sample; if they exit, restart after a minute, and stop them with node (`exec.CommandContext`).

Windows starts no processes: also `CGO_ENABLED=0`, but these Win32 calls work in pure Go (`golang.org/x/sys/windows`; `GetSystemTimes` and `GlobalMemoryStatusEx` directly through kernel32).

### Which network interfaces count

Count only traffic actually leaving the machine. Virtual interfaces are either local (lo, containers) or counted again on underlying physical interfaces (tunnels, proxies, bridges):

- **Linux**: exclude `/sys/class/net/<name>` links into `/sys/devices/virtual/net/`. The kernel puts software interfaces there: lo, docker0, veth*, br-*,
  virbr*, tun* (TUN proxies), tailscale0, wg*, bond / VLAN (member interfaces still count, without duplication). Real interfaces, including VM virtio, live under buses and count.
  Without `/sys`, exclude name prefixes (`sysmon.virtualName`: lo, docker, br-, veth, virbr, tailscale, tun, tap, wg, zt, cni, flannel, cali, etc.).
- **macOS**: Ethernet type (IFT_ETHER), `en*` name, non-loopback only: Wi-Fi, wired Ethernet, Thunderbolt. Exclude utun* (Tailscale, VPN, TUN proxies),
  bridge* (Thunderbolt bridge, member en* already counted), awdl / llw (AirDrop), anpi, gif, stf, ap.
- **Windows**: only `MIB_IF_ROW2` with `HardwareInterface` set and `FilterInterface` unset: Wi-Fi, Ethernet, USB tethering.
  Tailscale (wintun), VPN adapters, Hyper-V / WSL vEthernet, Wi-Fi Direct virtual adapters, Bluetooth PAN and loopback are software interfaces and excluded;
  filter layers atop physical interfaces (WFP, QoS) duplicate counters and are excluded too.

If counters decrease between readings (interface vanished or counters reset), discard that sample instead of drawing a huge spike.

### Overhead (measured 2026-10-30, `jarvisd sysmon 30`)

| Machine | Collection time | personad CPU | Other |
| --- | --- | --- | --- |
| Mac Studio (M1 Ultra, 20 cores, macOS 26) | 0.55–0.70 ms | 51 ms / 5 minutes = 0.017% | iostat + vm_stat together used 0.34 s CPU over 2 hours 21 minutes (0.004%) |
| Linux VM (32 cores, Ubuntu 24.04, 6 interfaces) | 0.76 ms | 46 ms / 5 minutes = 0.015% | No subprocesses |
| Windows 11 desktop (16 threads, 4 drive letters, 7 interfaces, 2026-11-04) | 1.8 ms (interface/drive enumeration once a minute ~8 ms, other samples ~0.5 ms) | 47 ms / 5 minutes = 0.016% | No subprocesses |

Five minutes after node starts, it logs average duration once (`metrics: a sample takes …`). Startup transcript scanning can inflate it
(4.7 ms observed on the same Linux machine where standalone `jarvisd sysmon` takes 0.76 ms).

## Storage and retention

On receipt, the manager retains the latest sample in memory (Dashboard **Now**, not current if older than 30 seconds) and writes it to `machine_metrics` (raw `step = 10` row).
Every minute `metricsLoop`:

1. **Downsamples** raw rows older than an hour into one row per machine per minute (`step = 60`): averages values and takes maximum totals (memory, disk, etc.).
   Delete and insert are one statement (`WITH moved AS (DELETE … RETURNING *) INSERT … SELECT … GROUP BY`), so a minute isn't merged twice.
   Raw rows are found through partial index `machine_metrics_raw` (`(ts) WHERE step = 10`, containing only the latest hour's raw rows), avoiding scanning the full 24-hour table each minute; SQL uses constant `step = 10` so cached statements' generic plans can still use it.
2. **Cleans up** all rows older than 24 hours 10 minutes (10-minute margin keeps the left edge full when opening a 24-hour chart).

Queries average both row types weighted by covered duration (raw 10, aggregate 60), keeping curves continuous across the one-hour boundary.

### Volume

Per machine at steady state: 360 raw rows for the latest hour + about 1390 aggregate rows for the preceding 23 hours 10 minutes ≈ **1750 rows**; 6 machines ≈ **10,500 rows**.
Measured row data is 116 bytes (`pg_column_size`), roughly 200 bytes including headers, pointers and primary index; whole table about **2 MB**. Writes per machine per day: 8640 raw + 1440 aggregate rows;
6 machines about 60,000 inserts and 50,000 deletes daily (autovacuum reclaims them; table doesn't grow).

## Gaps

Periods without machine samples are breaks in the chart, not connected lines:

- Manager `points` inserts `{"t": …, "gap": true}` between adjacent points more than 3 steps apart; also appends one when the last point is more than 4 steps old (machine offline now).
  Gaps shorter than 3 steps (occasionally missing one sample) stay connected. Steps: 1 hour = 10 seconds, 6 hours = 1 minute, 24 hours = 4 minutes (about 360 points per chart).
- App draws each segment between gaps as a separate line (detail Swift Charts `series:` uses "metric#segment"; card sparklines split by the same segment numbers), so lines never cross gaps; holding on a gap shows no value.
- Card sparklines use custom `Canvas`, not Swift Charts (`Sparkline` / `SparklineGeometry`: monotone cubic curves like chart `.monotone`, with a light fill beneath a single line):
  with a dozen charts per page, each chart's own layout adds about 130 ms on Mac each time Machines opens; vectorized `LinePlot` / `AreaPlot` is equally slow (overhead per chart, not point count, #390). Appearance is unchanged and compared in screenshots.

## App

Previously **Machines** (Agent counts) and **Machine Monitoring** (Dashboard) were separate pages, and machine details needed another tap into **System & Monitoring** to see curves. Requested 2026-11-01: merge into one **Machines** page,
removing the **Machine Monitoring** name and entry point.

- **Entry points** (all open `MachinesView`): **Machines** in the new shell drawer; **Settings › Machines**; **Machines** at the bottom of the Agents list's top-left menu (`MachineListRoute`, opens in the right column on iPad);
  Mac sidebar **Machines** (⌘5). Top-right ⓘ on Projects / machine projects opens machine detail (`MachineDetailRoute`).
- **Machines page** (`MachinesView`): top line "N/M online · K high-usage machines · J Agents need you". One card per machine (`MachineCard`): OS icon, name, OS · cores · memory,
  online / offline (last online time and SSH error when offline); alert tags ("Alerts" below); Agent counts **Needs you / Working / Idle** (Idle = Unread + Seen + Unknown,
  live sessions only, excluding assistant chats, matching Agents list); current CPU / memory / network values and past-hour sparklines; **Redeploy** when personad and hub aren't from the same deployment.
  iPad / Mac cards form multiple columns according to width. Tap to open machine detail.
  Web deployment entry depends on `can_deploy`: old Link nodes prompt a one-time local upgrade; supported nodes show waiting for idle / transfer / restart / version confirmation; details in [node-update.md](node-update.md).
- **Ordering** (`MachineConcern`): attention-needed first: offline → severe alerts → caution alerts → Agents needing you → version mismatch; within each category and for remaining machines, hub config order.
  Reorder only when something shown on the card changes, with animation.
- **Machine detail** (`MachineDetailView`): everything on one page, common actions first. Same card at top (without metrics, shown below); when offline, next comes **Hub connects through SSH …** and **Reconnect**.
  Then **Agents**: Needs you first, Working next, first 5 shown, **Show all N** for the rest; tap to open chat. Next **Engine** (which engine's sessions this machine shows; tap to choose, `EngineSelectionView`). Then **Monitoring**: current CPU / memory / network / load / disk / swap,
  1 hour / 6 hours / 24 hours selector, four charts for CPU, memory, network (download + upload), load (dashed core-count line); Linux also has per-core bars.
  Hold and drag any chart to inspect a time; all four share the selected timestamp, showing its value and time in the header. Then **System** (OS, kernel, architecture, model, processor,
  cores, memory, hostname, boot time and uptime, herdr / Claude Code / node versions), **Launchers**, and, when online, connection method and **Reconnect** last.
- **Updates**: machine online state and Agents update live through hub pushes; numbers poll only while page is visible and App foreground: Machines every 10 seconds, detail's 1-hour range every 10 seconds, other ranges every minute;
  stops in background (`task(id: scenePhase)`). Pull to refresh (Mac ⌘R, `macRefreshes`) also reloads the machine list.
- **Alerts** (`MetricAlert`): CPU ≥ 75% caution, ≥ 90% severe; memory ≥ 80% / 90%; disk ≥ 80% / 90%; load ≥ 1 / 2 times core count. Cards get a status-colored border,
  and tags use icon (status color) + text (body color), not color alone.
- With old hubs lacking these endpoints (404), machine cards omit metrics (Agent counts remain), and detail monitoring says **Deploy newer hub and node versions to see this**; old nodes sending no samples show **—**.

### Charts

Follow dataviz rules:

- One color per metric, consistent on Machines and details; separate light/dark shades (`MetricColor`), both validated with `validate_palette.js`
  (CPU blue, memory teal, download orange, upload purple, load magenta; network lines' color-blind separation ΔE > 25).
- Lines 2pt (sparklines 1.5pt), one line has 10% same-color fill below, two lines have none; one-pixel grid; right-side y-axis with at most 3 ticks, x-axis at most 4 times.
- Values use body color; series color appears only on lines and legend dots. Two-line charts (network) have dot + name legends; single-line titles name the metric.
- Status colors (caution yellow, severe red) only indicate alerts, not ordinary series.
- Each chart has a VoiceOver description (current value, maximum).

## Doctor: what this computer can do (#2429)

The web Machines page has **What this computer can do** (modeled on Moshi's Doctor page), presenting `personad node doctor` results (`docs/claude-integration.md`, "Doctor") by feature, not a string of check names.
iOS / Mac machine detail (#3018, `apple/Persona/Views/Machines/NodeHealthSection.swift`, grouping in `NodeHealthModel.swift`'s `NodeHealth`) places it after Engine. Grouping, states and copy match web; changing one requires updating the other.

- **Fetch**: hub routes `GET /api/v1/machines/{mid}/doctor` (read-only) and `POST …/doctor/fix` (rewrite Persona-generated files, recheck and return a new report) are **`full` devices only** (reports contain paths and user hook commands; fix writes files). Node RPC: `node.doctor` (read-only unless `{"fix":true}`), `node.doctor.fix` (`node/doctor_rpc.go`); checks run in node with CLI `RunDoctor`, same `DoctorReport` JSON. Hub GET never passes `fix` to node.
- **Group by feature** (`FEATURES` in `web/src/lib/node-health.ts`): phone approval cards (`node.sock`, claude / assistant settings and hooks, `command-paths`), state and notifications (`node.sock`, `command-paths`), terminal and engine (`herdr`), MCP tools (`node.sock`, `mcp.json`, `command-paths`), skills (`agent-plugin`, `claude-version`), runtime integrations (`runtime.sock`).
  Each feature takes its worst check: any `fail` → **Has issues**, any `warn` → **Partly available**, all normal → **Ready**. Terminal/engine and runtime integration are optional: warn-only means **Not enabled** (in **Not set up: …**, not counted as needing attention),
  except when this computer uses herdr (including herdr + Herdr App, and no selection defaults to herdr), unreachable herdr means **Has issues**. Unmapped extra checks reported by node go to **Other checks** if abnormal, never hidden.
  `global-permission-hooks` (user's synchronous PermissionRequest hooks) is not a feature; displayed as an informational line below features, read-only.
- **UI**: **Everything works** or **N feature(s) need attention** at top (Moshi wording), then **Not set up: …**; **View all checks (N)** expands individual state, name, finding, remedy and differing files; **Check again**;
  **Fix** appears for generated files marked `stale` / `missing`, with confirmation listing files to rewrite and stating that user Claude Code settings won't change. Check once on opening, no polling.
- **Old node**: unsupported RPC returns `unknown method`; card says **This computer's node is too old. Update it to run checks here**, beside the existing deployment button (same `POST …/deploy`, same `can_deploy`). Old hub route 404 and non-`full` device 403 each have specific explanations. Offline computers make no request.
- **Connection failures** (`classifyConnection`): offline machine cards/pages classify recorded hub error text as **Authentication failed / Can't reach this computer / Connection timed out / Version mismatch**, each with a next step (different SSH / Persona Link wording);
  unrecognized text adds nothing, no guessing. Planned restarts during deployment don't count.
- Mock-hub screenshots: `MOCK_DOCTOR=healthy|stale|broken|mixed|old|error|slow`, `MOCK_DOCTOR_OLD=<machine id,…>` (`scripts/mock/mock_hub.py`).
- Tests: `node/doctor_rpc_test.go` (temporary directories; no writes without fix, fix only generated files and never user settings.json, check names match web grouping), `hub/node_doctor_test.go` (permissions), `web/src/lib/node-health.test.ts` (grouping, summary, fixing, old node, connection classification), `apple/PersonaTests/NodeHealthTests.swift` (same grouping table, decoding, old node / hub, model check/fix).

## API

See `docs/protocol.md`: `GET /api/v1/metrics` (Machines), `GET /api/v1/machines/{id}/metrics?range=1h|6h|24h` (detail),
node → manager `metrics` event, `system` in `node.info` / `Machine`; hub forwards these routes unchanged to manager (`devicewire.MetricsRoutes`, `503 assistant_offline` if absent).

## Verification

- Go: `sysmon` fixtures (/proc/stat, meminfo, net/dev, os-release, cpuinfo, vm_stat, iostat, sw_vers, system_profiler in `testdata/`),
  `db.TestMachineMetrics` (downsampling, weighted averages, no points in gaps, cleanup), `devices.TestMetricGaps` (gap markers); `./scripts/test/test-server.sh`.
- Real collection: local test hub (fake HOME, separate database through SSH tunnel, one `kind: local` Mac and one SSH Linux), simulator paired to inspect Machines and details.
- Fake data: `scripts/mock/mock_hub.py` has 4 machines: server with high load over latest 25 minutes (CPU / memory / load alerts), Mac Studio disconnected for two hours that morning (gap),
  NAS offline for three hours. UI `MachinesPageTests`: open Machines from machine menu (offline NAS first, server alerts, MacBook **Needs you**), open server detail
  (Agents above charts, no **System & Monitoring** layer), switch to 24 hours, hold network chart, inspect system info, tap Agent into chat; offline detail (**Reconnect** on first screen).
  Ordering unit tests `MachineConcernTests`. **Show all** for 36 Agents in `MachinePagesTests.testMachineProjects`; **Redeploy** in `DeployButtonTests`.
- Screenshots: `-tab dev -openMonitor all` (Machines), `-openMonitor <machine id> [-openMonitorRange 6h|24h] [-openMonitorScrub 0.55]` (detail, holding at 55% of range);
  `-tab settings -openMachines YES [-openMachineDetail <machine id>]` enters through settings.
