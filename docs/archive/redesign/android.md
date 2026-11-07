# Android plan

> **Since 2026-10-31, the appearance, layout, and interaction sections of this document have been superseded by [Android interface styles and design system](../../android/android-design.md)**: the user requested a complete visual and interaction redesign into three interface styles: Glass (default), Morning paper, and Board. This document remains authoritative for functionality, data, protocol, and gating specifications.

> Draft v1 (2026-10-30), part of the [redesign plan](README.md).
> Based on [research/android-tech.md](research/android-tech.md) (technology choices; versions checked on 2026-10-30, with sources), merged PR #84 (`android/`, first version of the Agents feature), and [ux.md](ux.md).

## 1. Starting point: PR #84 and our choices

### 1.1 What PR #84 already does

`android/` is a single `:app` module, about 2.7 thousand lines of Kotlin, with no server changes:

- **Pairing**: QR scanning (zxing), `persona://pair` deep links, manual entry; exchanges for a device token.
- **Agents list**: sorted by state, in three sections: "Needs you / Running / Last 7 days."
- **Chat page**:
  - `turns=2` pagination;
  - WS subscription + continuity gap repair (`history` / `from_seq` / `to_seq` / `covered`);
  - tool calls and results combined into one row, lazy-loaded images, a small custom Markdown renderer;
  - sends with `client_msg_id`, handles `dialog_open` / `input_draft` send rejections;
  - interrupt, suggestions, and a terminal preview that reads the screen once per second.
- **Interaction cards**: all four types, plus a password field and "Handle on computer."
- **Notifications**: no push service; a foreground service (`specialUse`) maintains a WebSocket connection and emits local notifications. Notifications allow / deny, approve / send back (with input), and select the first 3 options of questions and screen dialogs. Withdraw notifications once an interaction is resolved elsewhere; notify "Done" when a turn finishes while the app is not foregrounded.
- **Validation**: tested against a real hub on an Android 14 emulator; a phone without Google services (Android 16) was tested over USB to hub. There are 10 unit tests.

### 1.2 Item-by-item comparison

| Dimension | PR #84 | This plan | Decision |
| --- | --- | --- | --- |
| Overall approach | Native Kotlin + Compose | Same | Agreed |
| Toolchain | AGP 8.7.3, Kotlin 2.0.21, Gradle 8.9, Compose BOM 2024.12, compile / target 34, minSdk 26 | AGP 9.5, Kotlin 2.4.21, Gradle 9.8, BOM 2026.09, compile 37; target 36 first, then 37 after pairing is complete; minSdk 31 (2.2) | **Use this plan**. PR target 34 happens to avoid Android 17 local network permission; preserve that through a two-step rollout |
| Modules | Single module, three packages: `data` / `service` / `ui` | About 18 modules, pure Kotlin core (section 3) | **Use this plan**. Parallel Agent work, JVM tests for pure logic, and a future KMP path all require module boundaries |
| Architecture | Global `Hub` in Application, with Compose reading its StateFlow directly; no ViewModel or DI | Application-level Store → ViewModel(StateFlow) → Compose; Hilt; Navigation 3; `HubScope` | **Use this plan** |
| Continuity and gap repair | Implemented, same rules as iOS; numeric (seq, block index) sort | Put the same rules in `spec/fixtures` | **Keep the PR implementation**, move to `:core:domain` with fixture tests; add subscription reference counting, interaction reconciliation after reconnect, and rereading after event-stream overflow |
| Long-chat folding (`fold_work`) | Unsupported: a session with 650 commands renders 1412 items individually on its first page | Required (hub already supports it, iOS already uses it) | **Add it** (P0) |
| Chat rendering | One row per tool call, no turn folding or work rows | Same `chatLines` as iOS (fixtures ensure agreement) | **Use this plan**. This is central to the iOS experience |
| Markdown | About 120 custom lines; no tables, ordered lists, syntax highlighting, or clickable links | mikepenz multiplatform-markdown-renderer (GFM tables, replaceable nodes, streaming parsing) | **Use this plan** |
| Storage | Pairing stored in plaintext SharedPreferences; no cache or outbox | DataStore + Tink token encryption; Room cache and outbox; storage tiers | **Use this plan** |
| Notification delivery | Persistent foreground-service WebSocket; phone decides "Done" | Originally FCM-first | **Use the PR approach as the only method** (5.8): design for phones without Google services, no FCM; hub determines "Done" and sends it over WS |
| Notification buttons | Allow / deny / approve / send back (with input) / question and screen-card options; withdrawn after resolution | "Allow / Approve" requires unlocking; denial may include a reason | **Combine**: retain the PR option buttons and withdrawal (iOS should learn from this); add unlock requirement (currently allows from lock screen) and denial-reason input |
| QR scanning | zxing-android-embedded (no Google services dependency) | Originally Google code scanner | **Use the PR choice**: Google code scanner requires Google Play services |
| Plaintext HTTP | Globally allowed in network security config `base-config` | Originally "allow only the configured hub address" | **Use the PR approach**: config is static XML without subnet support; hub address is known only at pairing. Add an in-app check (5.1) |
| State colors | Working green, Unread blue | Needs you red, Working amber, Unread green (same as iOS and `docs/protocol/agent-states.md`) | **Use this plan** (generated from tokens). PR colors differ from iOS |
| Plan approval | In-app "Approve (acceptEdits) / Approve and allow all / Send back"; notification approval uses acceptEdits | Approval-policy table | Already matches the table; retain |
| Seen reporting | Report `seen` only while app is foregrounded (RESUMED) | Seen-gating rules | Agreed; retain |
| Configuration and scripts | `android.application_id` from `config.json`; `scripts/android.sh install / apk / test` | — | **Use the PR approach**, extending signing, versioning, debug entry points, and screenshot scripts |
| Tests | 10 JUnit tests | Shared fixtures + Roborazzi + mock hub + debug entry points | **Use this plan** |
| Documentation | `docs/android/android.md` (current state, notifications, builds, device lessons) | This is the target plan | Keep both: `docs/android/android.md` describes the current implementation and lessons, updated at each step; this document describes the target |

### 1.3 How to replace it: reorganize from PR #84, do not rewrite from scratch

The PR is small; retain parts validated on real devices. Reorganize using these three lists, keeping the app usable at every step.

| Keep (unchanged or small edits) | Move (retain logic, move into new modules and add tests) | Replace (rewrite to this plan) |
| --- | --- | --- |
| `HubService` / `Notifier` / `ActionReceiver` / `BootReceiver` (foreground service and notifications) → `:platform:notify`; zxing scanning; `network_security.xml`; `scripts/android.sh`; `android.application_id` in `config.json`; "Lessons" in `docs/android/android.md` | `Models.kt` → `:core:contracts`; `ChatStore` continuity → `:core:domain` reducer (with `spec/fixtures/continuity`); `ApiClient` / `Hub` → `:core:hub` (narrow domain interfaces); `Pairing.parse` → `:core:domain`; `toolVerb` / `toolSubject` → `:core:domain` (match iOS `ToolPresentation`) | `Root` (Navigation 3 + four tabs); `ChatScreen` (shared list engine, status rows, work rows, pending states); `Rows` (`chatLines`); `Markdown`; `Theme` (generated tokens); `AgentsScreen` (iOS grouping, filters, pin / mute, menus); `InteractionCard` (new interaction panel); `LazyImage` (Coil); `TerminalPeek` (real terminal in the second batch); `PairingStore` (encrypted storage) |

### 1.4 What to learn from PR #84 (also for later iOS work)

1. **Select question and screen-card options directly in notifications**: the first 3 options of a single-choice question or screen dialog become notification buttons. (Later on iOS, dynamically register notification categories before pushing, or use a notification content extension.)
2. **Withdraw notifications once interactions resolve**: Android cancels the corresponding notification on `interaction_resolved`. (Later on iOS, use `removeDeliveredNotifications` while running; when not running, hub must send a silent push.)
3. **Report Seen only in foreground**: a background chat page must not swallow "Done" notifications. (Review iOS against this rule later.)
4. **Guidance for Chinese Android ROMs**: Settings offers "Battery optimization whitelist" and "Autostart management" entry points, falling back to app details if unavailable.

## 2. Technology stack

### 2.1 Versions (stable releases checked 2026-10-30)

| Layer | Choice | Version | Notes |
| --- | --- | --- | --- |
| Language / build | Kotlin (AGP built-in Kotlin), version catalog + `build-logic` convention plugins | Kotlin 2.4.21, AGP 9.5.0, Gradle 9.8.2, KSP 2.3.12, JDK 17 (or Android Studio's bundled JBR 21) | AGP 9 no longer applies `org.jetbrains.kotlin.android` separately; new DSL on by default. Local Android Studio 2025.3 supports only through AGP 9.2; CLI builds are unaffected, but the IDE needs Rabbit 2 |
| SDK | compileSdk 37; targetSdk 36 first, then 37 after pairing (5.1); **minSdk 31** | Android 17 = API 37 | Section 2.2 |
| UI | Jetpack Compose + Material 3; own token layer | BOM 2026.09.00 (Compose 1.12.1), material3 1.4.0 | Wait for stable 1.5.0 before using Expressive components (currently beta); Compose 1.12 requires AGP ≥ 9.2 |
| Architecture | Application-level Store / Repository → ViewModel (StateFlow) → Compose | Lifecycle 2.11.0, coroutines 1.11.0 | No MVI framework |
| DI | Hilt | 2.60.1 (requires AGP 9) | Alternative: Metro 1.5.1. Core modules use constructor injection only; bindings in `:app` |
| Navigation | Navigation 3 | 1.2.0 + `lifecycle-viewmodel-navigation3` | Back stack is a list; supports deep links (notifications, debug entry points) and list-detail panes (foldables, tablets) |
| Networking | OkHttp (REST + WebSocket) + kotlinx.serialization | OkHttp 5.5.0, serialization 1.12.0 | No Retrofit: few endpoints, a handwritten layer is enough |
| Storage | Room 2 (conservative), DataStore + `datastore-tink`, Tink | Room 2.8.5, DataStore 1.3.0, Tink 1.24.0 | Room 3 (3.0.3) has been stable for only 4 months and changed package names; AI training material mostly uses Room 2. Start with 2.8; migration mainly changes imports. `security-crypto` is entirely deprecated; do not use |
| Images | Coil 3 (OkHttp network layer) + Telephoto (zoom) | Coil 3.6.3 (AGP 9.4+ requires ≥ 3.6.3), Telephoto 0.19.0 | Custom `Fetcher` retrieves chat images by `ref`, cached permanently |
| Markdown / highlighting | mikepenz multiplatform-markdown-renderer; copy iOS lightweight highlighting rules | 0.46.0 | Tables, task lists, replaceable nodes (tool cards, context-reference cards, path labels), streaming parsing for incremental assistant output |
| Media | No player library (Media3) | — | Lark video / voice messages appear as cards, not in-app playback; taps hand off to Lark (5.14) |
| QR scanning | zxing-android-embedded | 4.3.0 (PR #84 choice) | No Google services dependency; later switch to CameraX + bundled ML Kit model for a native Compose viewfinder |
| WebView | androidx.webkit | 1.18.0 | `startUpWebView` prestart, `prerenderUrlAsync` prerender |
| Charts | Custom Compose Canvas | — | Need control over gaps, clipping, and press-and-hold inspection (Vico does not support discontinuities) |
| Background | WorkManager | 2.12.0 | Outbox retries (network constraint), expedited work |
| Widgets | Glance | 1.2.0 | Second batch |
| Testing | JUnit, kotlinx-coroutines-test, Turbine, Compose UI tests, Roborazzi | Roborazzi 1.77.0 | Record screenshot references in a fixed environment |
| Performance | Macrobenchmark + Baseline Profile, JankStats | androidx.benchmark 1.5.0 | — |

### 2.2 SDK levels

- **minSdk 31** (Android 12), rationale in section 1 of [research/android-cn-roms.md](research/android-cn-roms.md):
  - Coverage: about 91% of Chinese page views in StatCounter 2026-09 (about 78% under Google's global active-Play-device measure). None of these measures the number of phones in mainland China; domestic analytics platforms have no public 2026 data.
  - Includes HarmonyOS 3 / 4 devices that still install APKs: their Android compatibility layer is API 31 (China's Android 12 share is 17%, well above the global 8%, probably reflecting these Huawei phones). minSdk 33 would exclude them and all Android 12 phones.
  - Notification-action unlock requirements (`setAuthenticationRequired`) start at 31, the security prerequisite for lock-screen "Allow / Approve" buttons. Dynamic colors and `RenderEffect` also start at 31.
  - Restrictions on starting foreground services from the background also start at 31; use one rule set for this most failure-prone part of persistent connections.
  - Below 31 (about 9% in China, mostly models around 2020): if actual users need these devices, lower to 29. Cost: no lock-screen "Allow / Approve," no dynamic colors, and another foreground-service startup rule set to test.
- **targetSdk in two steps**:
  - Start at 36. Android 17 requires target-37 apps to request local network permission; for target ≤ 36, INTERNET implicitly grants it, and official guidance says low-target apps must not request it. Staying at 36 initially preserves PR #84's direct LAN-hub connection over Wi-Fi.
  - Raise to 37 together once pairing (5.1) covers permission requests, denied states, and guidance.
  - Outside Play there is no mandatory target requirement, but do not stay on an old version indefinitely.
- **compileSdk 37**: required by Compose 1.12.

## 3. Project structure

```
android/
├─ settings.gradle.kts / gradle/libs.versions.toml / gradle/gradle-daemon-jvm.properties
├─ build-logic/convention/   persona.android.application / .library / .compose / persona.jvm.library / persona.hilt / persona.screenshot-test
├─ core/
│  ├─ contracts/             Pure Kotlin: wire types (@Serializable, explicit @SerialName), WS events, capability bits, error codes, narrow interfaces
│  ├─ domain/                Pure Kotlin: continuity reducer, fold, items → Rows → Blocks → Turn → Lines, frozen snapshots, outbox state machine,
│  │                         sort / group / peers / Inbox selectors, approval-policy table, tag parsing, screen dialogs, formatting, Fleet copy
│  ├─ terminal/              Pure Kotlin: VT-subset grid, wrapping, scroll detection, key encoding, modifiers, frame sequencing / takeover state machine
│  ├─ hub/                   JVM: domain-specific OkHttp clients, WebSocket, connection monitoring (network state injected by Android layer), error classification
│  ├─ data/                  Android: Room (items, cursors, outbox, op log, drafts, cached snapshots), DataStore + Tink, sync engine, HubScope
│  └─ designsystem/          Android: generated Tokens.kt, PersonaTheme, components (ux.md section 4)
├─ feature/                  chat, agents, assistant, inbox, terminal, readers, machines, settings
├─ platform/                 notify (foreground service, notifications, Live Updates), phonetools, shortcuts (tiles, shortcuts, share targets, widgets)
├─ app/                      Composition root (Hilt), NavDisplay, four tabs, deep links; src/debug: entry points, component catalog, mock switches
└─ benchmarks/               Macrobenchmark, Baseline Profile
```

- Same dependency rules as iOS ([README.md](README.md) D2):
  - features depend only on `core:contracts`, `core:domain`, `core:designsystem`, and `feature:readers`;
  - features do not depend on `core:hub` or `core:data`; services are injected through interfaces.
- **Pure Kotlin modules** (`contracts` / `domain` / `terminal`) use the `persona.jvm.library` plugin without `android.*` dependencies. Run `spec/fixtures` tests on the JVM in seconds; a future KMP conversion changes only the plugin.
- **Configuration** follows PR #84: `android.application_id` in `~/.config/persona/config.json` (falls back to `app.bundle_id`). Add two settings, documented in `docs/operations/config.md` and `config.example.json`; actual values are not committed:
  - `android.signing`: keystore path and alias; password from keychain or environment;
  - `android.avd`: AVD name for screenshots.
- **Scripts**:
  - Extend `scripts/android.sh` with `lint` (ktlint / detekt / Android lint), `shots` (start AVD, take light / dark screenshots through debug entry points), `release` (signed package + debug-switch string check).
  - Screenshots and automation use the new Android CLI, or `emulator -no-window` + `adb`.

## 4. Architecture rules

- **Unidirectional data flow**:
  - Application-level Store / Repository owns the truth, such as one `ChatStore` per session. Background sync must write even while the page is closed, so this does not belong in ViewModel.
  - ViewModel only derives page state: call `:core:domain` on `Dispatchers.Default` to compute presentation rows, producing immutable lists cached by `version`.
  - Compose collects with `collectAsStateWithLifecycle`.
- **Scopes**:
  - Connection, sync, and outbox run on `HubScope`'s `CoroutineScope(SupervisorJob())`, canceled together when changing hub or re-pairing;
  - page tasks run on `viewModelScope`.
- **Threads**: JSON decoding, row derivation, Markdown parsing on `Default`; disk IO on `IO`. Inject dispatchers, replacing them with `StandardTestDispatcher` in tests.
- **Cancellation is not an error**: always rethrow `CancellationException`; never display it. `runCatching` must rethrow it first.
- **Compose performance**:
  - row models use `@Immutable` data classes + `kotlinx.collections.immutable`;
  - stable `key` and `contentType` for every LazyColumn row;
  - read per-frame values (header collapse progress) only in `graphicsLayer {}` / `offset {}` lambdas;
  - derived message data (Markdown AST, link recognition) lives outside composition in an ID-keyed LRU cache;
  - run Compose compiler reports regularly to check unstable parameters.
- **Navigation**:
  - Navigation 3; one back stack per tab. Files / Git on the chat page use an in-page segment switch, not the back stack.
  - Deep links (`persona://pair`, `persona://open?machine=&session=`) and notification taps share one `Router`.
  - Wide screens use list-detail panes, corresponding to iOS iPhone Duo panes.

## 5. Key subsystems

### 5.1 Pairing and connection: public direct access, multi-address routing, certificate pinning (P0)

The phone connects directly to hub over the public internet, preferring IPv6. Users configure their own VPN; the app does not manage it (basis: [research/public-direct-ipv6.md](research/public-direct-ipv6.md); hub side in [hub-changes.md](hub-changes.md), section 1).

- **Pairing**:
  - QR format unchanged: `url=` is a plaintext LAN address, `token=` is a short-lived temporary token issued by hub. New parameters: `hub` (hub_id), `fp` (certificate public-key fingerprint), `tls` (TLS port), `alt` (one or two external candidates).
  - After parsing, store `hub_id`, `fp`, and candidates in encrypted storage; exchange the temporary token for a device token.
  - Old hubs (no `tls` feature) support only plaintext LAN access; Settings says "Hub is too old for public direct access."
  - Opening a pairing link while already paired first shows a confirmation page (address, fingerprint), never silently replaces pairing.
- **Candidate addresses**:
  - from QR codes, authenticated `GET /api/v1/endpoints`, and WS `endpoints` events;
  - types: public IPv6, DDNS name, LAN, public IPv4 (only sent after hub confirms reachability);
  - each includes port and protocol.
- **Routing** (inspired by Happy Eyeballs v2 / RFC 8305):
  - order "public IPv6 → domain → LAN → public IPv4," starting another concurrent `GET /api/v1/health` probe every 250ms;
  - **Connected only after TLS public-key pinning passes and `hub_id` matches**. An old prefix may now belong to someone else; plaintext access would give a stranger the token.
  - cache the winner per network (Wi-Fi SSID or network ID / cellular), trying it first next time;
  - re-probe on network changes, disconnects, and new candidates; periodically check for better routes;
  - OkHttp 5 fast fallback races only multiple IPs of the same hostname; race different candidates ourselves.
- **TLS pinning**:
  - a hub-specific `OkHttpClient` with custom `X509TrustManager`, accepting only leaf-certificate SPKI SHA-256 equal to `fp` (or hub-announced `fp_next`). Addresses are changing IPs, so pinning also replaces hostname verification;
  - REST, WS, images, uploads, downloads all use it;
  - `network_security.xml` `<pin-set>` and OkHttp `CertificatePinner` compare only after system trust-chain validation. Self-signed certificates fail that prerequisite, so implement in code;
  - WebView does not load hub resources directly.
- **Plaintext**:
  - globally allow `base-config` (PR #84 approach), retaining LAN and debugging (loopback + plaintext + token, used by `adb reverse` and mock hub);
  - in-app check: `http://` only for LAN, VPN subnets, loopback, and `.local`. **Never send a token over `http://` to a public address**.
- **Local network permission** (when raising target to 37):
  - Official definitions and Android 17 source count the phone's subnet as local network. On home Wi-Fi, connecting to LAN addresses or hub's **public IPv6** in the same /64 both require runtime `ACCESS_LOCAL_NETWORK`; switching to public IPv6 cannot bypass it. Public IPv4 through router hairpin does not count as local. Verify all on real devices.
  - Without permission, TCP appears to **time out**. At home, check whether candidates are local and permission exists before deciding to request permission or try a nonlocal candidate first.
  - Distinguish "Blocked by permission" from "Hub unreachable"; offer "Grant permission" for the former, rather than endless "Connecting…".
  - Simulate on Android 16 using `adb shell am compat enable RESTRICT_LOCAL_NETWORK <package>`.
- **Error classification** ([ux.md](ux.md) 3.7):

  | Exception | Category |
  | --- | --- |
  | `UnknownHostException` | Address not found |
  | `ConnectException` | Connection refused |
  | `NoRouteToHostException` | Unreachable |
  | `SocketTimeoutException` | Timeout (first rule out local network permission at home) |
  | `SSLHandshakeException` / `SSLPeerUnverifiedException` / custom validation failure | Certificate mismatch (scan again) |
  | HTTP 401 / 403 | Pairing no longer valid |
  | `code=incompatible` (standard error body) or missing required features | Hub too old |
  | `code=tls_required` | Public plaintext route (should never occur; treat as a client bug) |

- **WebSocket**:
  - after connecting, first send `client_hello` (platform, version, capabilities, heartbeat negotiation; [hub-changes.md](hub-changes.md) 1.6, 3.3), resubscribe open sessions (`since_seq` = `covered`), then reconcile pending interactions (missing in PR #84);
  - poor-network detection uses **application-layer ping** ([hub-changes.md](hub-changes.md) 3.8): OkHttp has no manual ping API or pong callback;
  - jittered backoff 1→20 seconds; network recovery (`onAvailable` from `registerDefaultNetworkCallback`) immediately reruns routing;
  - stop endless reconnects on 401 and ask to re-pair: currently both clients reconnect forever, causing revoked devices to flood hub authentication failures.
- **Connection state**: iOS's four states plus `needsLocalNetworkPermission` and `certificateMismatch`, debounced 1.5 seconds ([ux.md](ux.md) 5.5).
- **Connection diagnostics** (**Settings › Connection**):
  - current route (public IPv6 / domain / LAN / public IPv4) and round-trip time;
  - latest result for each candidate;
  - phone IPv6 availability (global IPv6 address and default route in `LinkProperties`), VPN use, captive portal status;
  - certificate fingerprint;
  - current heartbeat interval;
  - latest failure category;
  - "Probe again."
- **After an address change**: a home broadband reconnect changes the prefix and immediately drops the connection. The phone recovers in order: new candidates pushed by hub → DDNS name → LAN address. Without a domain and away from home, recovery may fail; diagnostics explicitly says "Consider configuring a DDNS name for hub."

### 5.2 Sync and storage (P0)

- **Session items**:
  - continuity rules in the `:core:domain` reducer, tested with `spec/fixtures/continuity`;
  - requests include `fold_work`: the first page folds long work sequences into fold items, expanded and replaced by fetching `from_seq` / `through_seq`; real-time pushes and catch-up data are not folded;
  - reference-counted subscriptions.
- **Room tables**:
  - `items(scope, machine, session, seq, part, id, json)` and session `cursors(scope, machine, session, history, covered, rev, has_more)`: opening a session first renders the last two cached turns; background sync writes incrementally using `after_seq`;
  - `outbox`: messages and Lark replies;
  - `ops`: one-time operation log;
  - `drafts`: versioned;
  - `snapshots`: machines, session lists, interactions, Inbox, last 80 Lark conversations.
- **Encrypted storage**:
  - device tokens use DataStore + `datastore-tink` `AeadSerializer`, with keysets wrapped by an Android Keystore master key;
  - exclude encrypted files from cloud backup and device transfer in `dataExtractionRules`: Keystore keys are not backed up, so a new phone cannot decrypt restored files; re-pair after restore;
  - retain PR #84's `allowBackup=false`.
- **Scope**: every table has `scope = hub_id + pairing generation`. Delete old data together when switching hub.
- **Background sync**: runs only in foreground while connected to hub, matching iOS `AgentSync`: live Agents + sessions used within 24 hours, at most 40, concurrency 3.

### 5.3 Chat list (P0)

- `LazyColumn(reverseLayout = true)`, newest item at index 0. `scrollToItem` positions using measured rather than estimated height, avoiding iOS-style blank screens when jumping to unrendered regions.
- Presentation rows come from `:core:domain` `chatLines`: every message and work step is its own row, with stable key and contentType.
- Copy iOS frozen snapshots: row IDs change as a turn progresses, breaking key anchoring, so history renders from the item snapshot captured when leaving the latest edge.
- Entry positioning, bottom following, reading position, read gates, and "Back to latest" counts follow [ux.md](ux.md) 3.5:
  - position in two steps: `scrollToItem`, then correct from `layoutInfo` on the next frame;
  - only user dragging changes bottom-follow state; use `interactionSource` to distinguish dragging from programmatic scrolling;
  - read and Seen gates use `snapshotFlow` over visible items, coalesced over 450ms.
- Composer in a Column below the list, with `imePadding()`.
- **No Paging 3**: turn-based pagination, cross-page folding, and real-time merges all need our own `ChatStore`.

### 5.4 Markdown and code (P0)

- mikepenz renderer + custom nodes:
  - code blocks: lightweight highlighting (copy iOS `LarkSyntax` rules, shared fixtures), copy button, horizontal scrolling;
  - tables: horizontal scrolling;
  - links: `Router`, opening in-app;
  - file paths: recognized as tappable labels;
  - `<persona-context>`: reference cards;
  - `<attached_images>`: square thumbnails.
- Cache parsed results by message ID; prewarm in the background after loading messages. Use streaming parsing for incremental assistant output, reparsing only the tail.
- `SelectionContainer` enables in-place selection across paragraphs. No "Select" item needed in the long-press action bar.

### 5.5 Tool cards, diffs, monospaced text (P0)

- Tool cards: icon / verb / subject mappings in `:core:domain`, sharing fixtures with iOS `ToolPresentation`. Merge PR #84's `toolVerb` / `toolSubject`.
- Diffs: row model in `:core:domain` (old / new line numbers, ± markers; color only markers and row backgrounds). Above 40 lines, show the beginning with "Expand all."
- Monospaced text scrolling both ways: outer `horizontalScroll`, inner `LazyColumn` as wide as the longest measured line (`TextMeasurer`), one nonwrapping `Text` per row; keep line numbers fixed outside the horizontal region. Even large files / diffs compose only visible rows.

### 5.6 Link overlay and WebView pool (P1)

- M3 `ModalBottomSheet` is a separate window, blocking the page behind it. Instead, draw a same-window overlay on the chat page with three `AnchoredDraggable` anchors (full / 60% / hidden). At 60%, only the overlay receives touches in its area; the chat above still scrolls and accepts input.
- WebView does not participate in Compose nested scrolling: collapse through the top handle or title bar. Alternatively, when the page reaches the top (JS reports `scrollY == 0`), hand downward drags to the overlay. Test this.
- Preloading:
  - `WebViewCompat.startUpWebView` while the app is idle;
  - `prerenderUrlAsync` for the most likely next link;
  - keep 2–3 WebViews in an LRU pool (fewer than iOS's 6; Android WebView uses more memory);
  - never create a WebView while scrolling;
  - check `WebViewFeature.isFeatureSupported` for these APIs; fall back to normal loading when unsupported. On phones without Google services, vendor-updated WebView may be older (5.18).
- Add "Edit with AI / Use as context" after text selection: override `startActionMode` to add menu items, retrieve selected text with `evaluateJavascript`.
- OAuth pages that prohibit embedded WebViews use Custom Tabs; if no supporting browser exists, fall back to the system browser. Also offer "Open in browser."

### 5.7 Interaction panel and full page (P0)

- Panel above the composer, at most 55% height, scrolling internally. Full page uses a full-screen `ModalBottomSheet`. All rules follow [ux.md](ux.md) 3.4: denial reasons, feedback mutually exclusive with approval, countdown, source, per-request drafts, "Handle on computer."
- PR #84 currently makes interaction cards the first list row (the bottom of the chat). Reorganization pins them above the composer instead, keeping pending requests visible while reading history.

### 5.8 Notifications: persistent connection only (P0)

Design for phones without Google services: no FCM or vendor push (requires developer accounts and real-name verification). One delivery method only:

- **How**: a foreground service (`specialUse`) maintains hub WS and emits phone notifications on events (already in PR #84). Phone-tool calls also arrive directly, without wake-up.
- **Costs**:
  - one persistent notification;
  - battery usage **not measured**; measure in A1;
  - Chinese ROMs require manually enabling autostart and removing battery restrictions (5.18);
  - long background connections after locking have not been tested on devices.

Notification rules:

- **Channels**: Needs you (high), Done (default), Persona (assistant `notify_user`), Live status (low). Users adjust each in system settings.
  - Once created, only the user can change channel importance. PR #84's "Background connection" channel is `IMPORTANCE_MIN`, ineligible for Live Update promotion (5.10). During reorganization, create `IMPORTANCE_LOW` "Live status" (silent, promotable) and delete the old channel.
  - Persistent notifications use `setForegroundServiceBehavior(FOREGROUND_SERVICE_IMMEDIATE)`: Android 12+ defaults to waiting up to 10 seconds before showing a foreground-service notification.
  - Android 14+ lets users swipe away foreground-service notifications without affecting the service. Repost on the next state change.
- **Vendor defaults** ([research/android-cn-roms.md](research/android-cn-roms.md), section 3):
  - Xiaomi: floating and lock-screen notifications default off; the app cannot query either switch (official Xiaomi guidance says no API);
  - Huawei HarmonyOS 4: local notifications from apps without "self-classification entitlement" are treated as "Information / marketing": silent (no banner, lock screen, sound), limited to 2–5 per day. Apply for entitlement in Huawei AppGallery; availability without listing was not established. First version can only guide users to disable "Silent notifications" and enable banners / lock-screen display;
  - vivo: one notification-permission prompt on first launch; denial disables everything, with no second prompt;
  - therefore Needs you also appears as a count in the persistent notification, not only a popup. The readiness checklist guides each setting (5.18).
- **Buttons**:
  - authorization "Allow" and plan "Approve" require unlocking (`setAuthenticationRequired(true)`); "Deny" / "Send back" do not;
  - question and screen-dialog option buttons also require unlocking: like Allow, they let the Agent proceed, and screen-dialog options often approve commands (decided 2026-10-31, `spec/fixtures/approval/unified.json`);
  - both "Deny" and "Send back" allow direct reason / feedback entry with `RemoteInput`;
  - the first 3 options of single-choice questions and screen dialogs are buttons (already in PR #84);
  - notification plan approval can only "Automatically accept edits only" (approval-policy table).
- **Result**: buttons call resolve directly in the background. Failure (already resolved, hub unreachable) emits a notification explaining why, not a Toast, which is unreliable in the background.
- **Withdrawal**: cancel the corresponding notification on `interaction_resolved` (already in PR #84).
- **Who decides "Done"**: PR #84 currently detects "Working → Unread" on the phone, unlike hub's push policy (wait 60 seconds, only long tasks, only after background work finishes, not if seen on the computer, not if the assistant already notified). Move the decision to hub: where it decides to send iPhone APNs, send `{"type":"notify",...}` over WS to persistent-connection devices ([hub-changes.md](hub-changes.md) 3.9). Phone only displays it. Device notification preferences (`/devices/{id}/prefs`) apply equally, with Settings matching iOS.
- **Foreground suppression**: no notification while the app is foregrounded and viewing this session.
- **Chinese ROM guidance**: retain PR #84's two Settings entry points (battery optimization whitelist, autostart management), expanding to OS- and version-specific guidance (5.18) and the post-pairing checklist.

### 5.9 Background operation

- **Foreground service**:
  - `specialUse`, not `dataSync`: target-35+ `dataSync` has a total 6-hour background limit within 24 hours; `specialUse` does not;
  - a process with a foreground service can network and hold locks in Doze (AOSP source); Android 15–17 has no new `specialUse` restriction. Risks are vendor freezing and network-cutoff switches (5.18);
  - immediately reroute and reconnect on network changes.
- **Recovery after process death**: Android 12+ prohibits background foreground-service starts except roughly a dozen exemptions. **`START_STICKY` is not exempt**: a system background restart after process death throws `ForegroundServiceStartNotAllowedException` from `startForeground`. Therefore:
  - always try/catch `startForeground`; on failure `stopSelf()`, waiting for the next exemption: user opens app, taps notification, device boots (`BOOT_COMPLETED`; `specialUse` is not subject to Android 15 boot restrictions), or app upgrades (`MY_PACKAGE_REPLACED`);
  - **Disabling battery optimization is itself an exemption**: background starts are then allowed anytime. This is the most important checklist item, not just for Doze;
  - a periodic WorkManager job (minimum 15 minutes) checks and restarts a missing service; starts work only with battery optimization disabled, and post-kill job execution also depends on vendor autostart settings;
  - if the user taps Stop in "Active apps" (Android 13+), the system gives no callback. On next launch, `ApplicationExitInfo` shows `REASON_USER_REQUESTED`; do not restart secretly. Say "You stopped the background connection" and offer "Restore";
  - record kill reasons (`getHistoricalProcessExitReasons`) in local logs and show them in connection diagnostics.
- **Other platform limits**: Android 16+ applies JobScheduler quotas even while a foreground service runs; do not assume WorkManager outbox retries are unrestricted (normal messages use the persistent connection directly). Android 17+ caps app resources by device memory; control persistent-process caches and image memory.
- **Hub-initiated heartbeats, separate foreground and background settings** ([hub-changes.md](hub-changes.md) 1.6):
  - **Why**: OkHttp client pings do not fire during Android deep sleep (timing uses `System.nanoTime`, which stops then). Background-capable alarms (`AllowWhileIdle`) run at most once per 9 minutes. Thus background heartbeats depend on hub pinging; the phone wakes on network packets and answers pong.
  - **Background**: negotiate starting at 4 minutes, adapt by network, jitter downward only. On every wake, check elapsed time since last hub data; reconnect immediately if timed out.
  - **Foreground**: 20 seconds, plus application-layer ping for poor-network detection.
  - On foreground transitions, send a new `client_hello` to update negotiation. OkHttp `pingInterval` cannot change after client creation; use two client configurations, or disable client pings entirely and rely on hub.
  - Carrier NAT / firewall idle timeouts and battery usage at different intervals are **unmeasured**; A1 must test real devices (section 10).
- **Outbox retries**: WorkManager with network constraint; retry only entries definitely not sent, with the same ID.
- **Self-check**: no Chinese ROM provides APIs for autostart / background-whitelist status; infer settings from outcomes:
  - checklist "Lock-screen test": after 30 seconds locked, hub sends a test notification; ask whether it sounded / lit the screen;
  - hub records each device's WS-disconnected intervals ([hub-changes.md](hub-changes.md) 3.10); diagnostics shows "Background disconnected N times in the last 24 hours, longest X minutes." A1 overnight survival tests use the same records.

### 5.10 Persistent lock-screen notification / Live Updates (P1)

- The foreground-service notification already exists. **Combine it with Fleet summary**: normally "Connected to hub"; when Agents work or need you, show three counts and the first actionable request. Use the existing hub WS `fleet` event; no hub changes.
- Form varies by OS version:
  - **Android 17**: `Notification.MetricStyle`, up to 3 metrics, exactly Needs you / Working / Unread, with Needs you critical. During activity request Live Update promotion (`setRequestPromotedOngoing(true)`); status-bar capsule uses `setShortCriticalText("⚙3")` (within 6 characters is safest). Remove promotion when inactive.
  - **Android 16**: BigText-style Live Update.
  - **Older**: ordinary persistent notification.
  - Chinese ROMs ([research/android-cn-roms.md](research/android-cn-roms.md), section 5): ColorOS 16+ Fluid Cloud officially supports standard APIs; HyperOS 3.1+ Super Island has only community tests; mainland OriginOS and MagicOS are unconfirmed; HarmonyOS 4 is API 31 without these APIs. No material exists on MetricStyle appearance in these ROMs; take device screenshots.
  - Vendor-private APIs (Xiaomi Focus notifications, OPPO / vivo partnerships, Honor / Huawei private extras) require whitelists or nonpublic protocols; exclude from first version.
  - Promotion requires a new `IMPORTANCE_LOW` channel for the persistent notification (5.8).
- Lock-screen privacy: `VISIBILITY_PRIVATE`, plus a counts-only public version, matching iOS "Show lock-screen details."
- Risk: official use guidelines discourage "activities triggered by others" and reminders; vendors may impose limits, and users can disable in one tap. Needs you therefore always uses ordinary high-priority notifications, independent of Live Updates.

### 5.11 Terminal (P1)

- `:core:terminal` ports iOS `TerminalGrid` / `TerminalWrap` / key encoding / modifiers, using existing real recorded frames (`ios/PersonaTests/Fixtures/terminal-*.jsonl`, moved to `spec/fixtures/terminal/`) for shared tests.
- Rendering: Compose Canvas by cells (wide characters take two; box-drawing / block characters become lines and rectangles), `TextMeasurer` cached by run.
- Input: custom `InputConnection` (`PlatformTextInputModifierNode`, or a custom View's `onCreateInputConnection` in AndroidView), correctly handling `setComposingText` / `finishComposingText` / `commitText` (iOS marked text equivalent). Use `TYPE_CLASS_TEXT | TYPE_TEXT_FLAG_NO_SUGGESTIONS`, not `TYPE_NULL`, which breaks Chinese IME composition.
- Modifiers (tap to stick, double-tap to lock, hold), scroll-wheel forwarding, and two-finger Agent switching are detected in `pointerInput`.
- No existing library: Termux terminal licensing is unclear; ConnectBot termlib needs raw program bytes, whereas herdr supplies an already-emulated grid, so it is not useful.
- Retain PR #84's once-per-second screen-read terminal preview as the initial transition.

### 5.12 Phone tools (P1)

| Tool | Android | Notes |
| --- | --- | --- |
| Contacts | `ContactsContract` | Requires `READ_CONTACTS`; system Contact Picker has no keyword search, so cannot be used |
| Calendar (read, create) | `CalendarContract` | Creation needs a writable calendar. Without a Google account there may be none: create a local "Persona" calendar as a sync adapter; test visibility in vendor calendar apps on devices |
| Reminders | **Do not register** | Android has no system reminders API |
| Location | `LocationManagerCompat.getCurrentLocation`, selecting from `getProviders(true)`: on 12+, try platform `fused`, then GPS, network (not Google-dependent Fused Location), with timeout; `Geocoder` checks `isPresent()` first | Mainland network location relies on vendor services and may fail; `Geocoder` may lack a backend, returning coordinates only. State coordinate system (GPS WGS-84; Chinese maps GCJ-02) |
| Health | Health Connect, **not registered on mainland phones in the first version** | Requires Google Play services; mainland health apps (OPPO Health, etc.) do not write to it; Huawei uses Health Kit. Check `HealthConnectClient.getSdkStatus()` first; do not register when unavailable |
| Photos | `MediaStore` (recent metadata) + fetch / resize images | Android 14+ allows access to selected photos only |
| Image picker (sending messages) | AndroidX `PickVisualMedia` | 33+ uses system photo picker; 31–32 (including HarmonyOS 4) requires a Google system-update module, usually absent on mainland devices. Automatically falls back to file picker, functional but visually different |
| Device info | `Build`, `BatteryManager`, `PowerManager`, etc. | — |
| Clipboard | `ClipboardManager` | **Read only in foreground**; background calls return "Open Persona." Xiaomi / ColorOS have additional clipboard permissions; empty reads prompt checking system settings |
| Notifications | `NotificationManager` + WorkManager / AlarmManager | Delayed delivery is not guaranteed punctual |

- Tool names and parameters match iOS; descriptions are Android-specific.
- Writes follow iOS: confirmation before execution. Foreground shows a card; background sends a high-priority notification with Allow / Deny.
- Hub registers and routes tools by device ([hub-changes.md](hub-changes.md) 2.1), or Android and iPhone overwrite each other.
- Background location under persistent connections requires `ACCESS_BACKGROUND_LOCATION` or a `location` foreground-service type; evaluate. Default: foreground location only, background calls return "Open Persona."

### 5.13 Themes (P0)

- Default style is "Native (Material You dynamic colors)," corresponding to iOS Liquid Glass. PR #84 already uses dynamic colors.
- State colors, semantic colors, radii, spacing, and font roles all come from generated `Tokens.kt`. Style-independent state colors match iOS (change PR #84's colors).
- Claude / Clay / Island / Lark styles in the second batch:
  - soft shadows / highlights with Compose 1.9+ `Modifier.dropShadow` / `innerShadow`;
  - hairline outlines with `border(Dp.Hairline)`;
  - frosted effects may use Haze.
- No Liquid Glass: requires AGSL and API 33, costs GPU time, and is not Android's design language.

### 5.14 Images and media (P0 / P1)

- Chat images: Coil 3 custom `Fetcher` + `Keyer`, permanently cached by `ref`. Reserve row space using `width / height` (`aspectRatio`), fetching only when visible. Replace PR #84's full-image decoding on every load, no cache, fixed 120dp placeholder.
- Full-screen viewer (P1): `HorizontalPager` + Telephoto, browsing all images in the session.
- Lark media (second batch): Coil images use the same authenticated OkHttp as chat images; downloaded files open through the system. **No in-app video or voice playback**: show cards (cover, duration), handing taps to Lark. Playing Lark video in-app does not help "understand one thing and hand it to AI," and falls under "Do not build a second Lark."

### 5.15 Inbox and Lark (P0 basics, cards P2)

- Inbox list, sections, filters, Done (full swipe with `SwipeToDismissBox`); merge "Agents waiting for you" into Needs you.
- Lark conversation page uses the shared list engine ([ux.md](ux.md) 3.5):
  - inline emoji via `InlineTextContent`, fixed placeholder dimensions so late images do not relayout;
  - rich-text block tree initially supports paragraphs, lists, quotes, code;
  - cards initially show summaries + "Open in Lark"; full card rendering in the second batch. `column_set` uses custom `Layout`, checking `Constraints.hasBoundedWidth`, matching the iOS unbounded-width pitfall.
- Email detail: WebView with JS disabled, height measured from content.

### 5.16 Demo mode (P1)

Existing iOS demo mode uses an in-app fake hub, with data from `scripts/mock/mock_hub.py`, exported by `scripts/mock/make_demo_data.py` to `Demo.json`, with times relative to now. Android reuses `Demo.json` through a `DemoHub` under `:core:hub`; no demo-mode branches in pages. Three uses:

- preview for people without hub;
- debug screenshots without starting mock hub;
- fixed screenshot-test data.

### 5.17 Large screens and foldables (P2)

- Layout by width, not device type: single pane when narrow; Agents, Inbox, and Settings use list-detail panes when wide (Navigation 3 list-detail scene), matching iOS iPhone Duo.
- Chat body maximum width 760dp, matching iOS.

### 5.18 Chinese Android ROM support (P0)

Based on [research/android-cn-roms.md](research/android-cn-roms.md) (market share, versions, vendor restrictions, persistent connections, Live Updates, distribution, testing, with sources).

- **Coverage**:
  - HyperOS (Xiaomi / Redmi / POCO), ColorOS (OPPO / OnePlus / realme), OriginOS (vivo / iQOO), MagicOS (Honor), HarmonyOS 4.x and earlier (Huawei, API 31 compatibility layer).
  - This year's current major versions (HyperOS 4, ColorOS 17, OriginOS 7, MagicOS 11) are all Android 17-based; installed devices mainly use the Android 16 generation.
  - **HarmonyOS 5 / 6 / 7 cannot install APKs directly**, only into Huawei's EasyAbroad container. Foreground-service and notification behavior inside the container is unknown, so no first-version promise. When detected, the pairing page explicitly says "Allow notifications in EasyAbroad and keep it running in the background; some features may not work."
- **OS detection**: read system properties, not just `Build.MANUFACTURER` (a Xiaomi phone may run another ROM):
  - HyperOS `ro.mi.os.version.name`; ColorOS `ro.build.version.oplusrom`; OriginOS `ro.vivo.os.build.display.id`; MagicOS `msc.config.magic.version`; HarmonyOS 2–4 `hw_sc.build.platform.version`; EasyAbroad `ro.product.anco.devicetype`.
  - Include OS name / version in `client_hello` ([hub-changes.md](hub-changes.md) 3.3), so hub aggregation reveals vendor/version-specific broken settings links.
- **No vendor provides public APIs for autostart / background-whitelist status**, so divide the checklist:
  - **Queryable**: notification master switch, channel importance, battery optimization (`isIgnoringBatteryOptimizations`), background restriction (`isBackgroundRestricted`). Show real status; recheck on return to the app.
  - **Not queryable**: floating / lock-screen notifications, autostart, vendor "Unrestricted," recent-app locking, Huawei silent notifications / "Stay connected while asleep," Honor "Smart data saver." Give OS/version-specific steps + "Open settings"; users tap "Done." Fall back to self-check in 5.9 (lock-screen test notification, hub-recorded disconnects).
- **Settings links as data**: ordered candidates per vendor (component or action), try `startActivity`, catch `ActivityNotFoundException` / `SecurityException`, fall back to app details if all fail.
  - Starting another app's page needs no package-visibility declaration; `resolveActivity()` is visibility-filtered, so a missing result does not mean the Activity is absent. Try directly.
  - Component names are community reverse-engineered and break across versions (ColorOS 15+, OriginOS 6+, and new Honor-device names not found). Log every launch result locally; test each major version on cloud devices.
- **Vendor checklist essentials**:

  | OS | Ask the user to enable | Easily missed |
  | --- | --- | --- |
  | HyperOS | Autostart (off by default); battery use "Unrestricted"; lock in recents | Floating and lock-screen notifications default off and cannot be queried. Battery-optimization exemption uses Xiaomi's own page; wait about 1 second after return before checking |
  | ColorOS | Autostart; "Allow full background activity" in power management; lock | Community reports say ColorOS's whitelist resets native battery-optimization changes |
  | OriginOS | Autostart; "Allow high background power usage"; lock | First-launch notification prompt appears only once; after denial, enable in Settings |
  | MagicOS | App launch management: disable automatic management, enable autostart, secondary launch, background activity; battery optimization "Don't allow"; lock | **"Smart data saver" cuts background mobile data**: process alive, no notifications, works again upon opening—easily mistaken for a hub problem |
  | HarmonyOS 4.x | Three app launch switches; battery optimization "Don't allow"; **"Stay connected while asleep"**; lock | Local notifications default silent and rate-limited (5.8); disable Silent notifications, enable banners / lock screen |

- **Testing**: at least three physical devices: Xiaomi (already available, HyperOS 3 / Android 16), Huawei HarmonyOS 4.x (minSdk floor, hardest notifications), OPPO ColorOS 16 or 17 (friendliest to standard APIs). First test vivo / Honor settings links and prompts on vendor cloud devices; buy if problems arise. Cloud sessions last at most 1 hour, have no SIM, and usually remain plugged in, so cannot test overnight survival, battery, or background restrictions on mobile data. Mandatory tests per device are in section 8.
- **Record lessons**: actual behavior per vendor/version belongs in "Lessons" in `docs/android/android.md`.

## 6. Platform differences and trade-offs

### 6.1 Different forms

| iOS | Android |
| --- | --- |
| APNs push | Persistent connection (5.8) |
| Lock-screen Live Activity + Dynamic Island | Persistent notification / Live Updates, Android 17 MetricStyle (5.10); numeric metrics (TPM, tokens, load) stay in the app, not notifications |
| Silent push wakes phone tools | Persistent connection stays online; no wake-up needed |
| Siri / Shortcuts | Quick Settings tiles (Needs you count, approve first), home-screen shortcuts (ask Claude), Glance widgets, share target. Google Assistant is shut down; no App Actions. AppFunctions remains beta, a long-term option |
| Apple Watch | No Wear OS app in first version: phone notifications (including buttons / `RemoteInput`) automatically bridge to Wear OS watches |
| HealthKit | Health Connect; no health-tool registration on mainland phones initially (5.12) |
| Liquid Glass | Native Material You |
| Swipe left in chat to Files / Git | Top segment switch: system back gesture owns screen edges |
| "Select" opens a plain-text page | In-place selection |
| Gmail Google-account sign-in | **Not on Android**: Google sign-in APIs require Google Play services. Email accounts live on hub and are shared across clients; add once on iPhone, or use ordinary IMAP with an app password. Later, when hub has a domain and public-CA certificate, use "web authorization, callback to hub"; the phone only opens authorization in a browser (P2) |

### 6.2 Not building

- Three reminder tools (no Android system API).
- Wear OS app (first version).
- Liquid Glass style.
- Voice-assistant entry point.

### 6.3 Android-specific

| Capability | Recommendation |
| --- | --- |
| **Share to Persona** | **First version (P0)**: text, links, images, files from any app via Share → Persona, then choose main chat or an Agent; attach as `<persona-context>` or attachments. `ACTION_SEND` intent-filter, low cost, directly supports "bring information together and hand it to AI" |
| Persistent connection | Already default (5.8) |
| Quick Settings tiles, widgets | Second batch |
| Large-screen / foldable panes | P2 (5.17) |
| Read other apps' notifications, SMS, call logs | **Not by default**: too sensitive. Any future support needs separate switches and confirmations; data only for AI |

## 7. First-version scope and slices

First version covers the 76 P0 items in sections 1–24 of [parity.md](parity.md), plus Android-P0 additions in section 25. Three slices from PR #84.

| Slice | Content | Completion criteria |
| --- | --- | --- |
| **A0 Project upgrade** (no behavior change) | Upgrade toolchain (target 36 first); add `build-logic`, version catalog, daemon JVM rules; extract three pure Kotlin modules (contracts / domain / initially empty terminal), move PR `Models`, `ChatStore` continuity, `Pairing.parse`, `Rows`, run `spec/fixtures`; add `lint` / `shots` to `scripts/android.sh` | All 10 PR #84 tests + fixture tests pass; device pairing, sending, notification answers behave as before |
| **A1 Minimum usable slice** | Hilt, Navigation 3, `HubScope`, encrypted storage, Room items / outbox; **public direct access**: candidate racing, TLS pinning, `client_hello`, heartbeat negotiation, diagnostics (5.1, 5.9); chat becomes shared list engine + `chatLines` + `fold_work` + status rows + new interaction panel + mikepenz Markdown; notification Allow / Approve require unlock, denial reason; generated tokens (iOS state colors); debug entry points + mock screenshots | Smooth opening of a 650-command chat (measure first-screen time and scroll jank, at least 10 samples); cellular public-IPv6 hub connection; measured idle timeouts for three carriers, hourly persistent-connection battery drain, lock-screen survival; real-device flow "notification → approve in notification or app → Agent continues" |
| **A2 Complete first version** | Four tabs: Persona (home, main chat, history), Agents (three groupings, filters, pin / mute, long-press menus, close, new session), Inbox (basic Lark rendering + reply, email detail), Settings; connection states (four states + local network permission + certificate change + invalid pairing, raise target to 37 here); pairing step progress and checklist (including Chinese ROMs, 5.18); offline outbox; Share to Persona; in-app APK updates (section 9); hub sends Done over WS | Every Android P0 in parity.md reaches at least "Passed on test hub"; usable daily by the owner and testers |
| A3 Second batch | Terminal, Live Updates, phone tools, Knowledge, Files / Git, link overlay, Lark documents / cards, machine monitoring, other styles, demo mode | Item-by-item per parity.md |

PR #84 coverage of first-version P0 (use during reorganization to identify what need not start from zero):

| Existing | Partial | Missing |
| --- | --- | --- |
| Pairing (QR, deep links, manual), device-token exchange, plaintext HTTP, history pagination, gap repair, send rejection handling, four interaction types + screen cards, Seen gates, notification answers, tap notification to session, unpairing, dynamic colors | WS (no four-state connection or poor-network detection), Agents list (state grouping only), chat (inverted list but no positioning, freezing, bottom return), tool rows (no diff colors), images (lazy but no cache / placeholders), composer (text only), drafts (page-local, not persisted), Done notification (phone decides) | Four tabs, Persona tab, Inbox, Lark, mail, pin / mute, new session, folded work rows, outbox, local cache, in-app links, tokens / component library, debug entry points |

## 8. Tests and tools

- **Pure Kotlin modules**: JUnit + coroutines-test + Turbine, running `spec/fixtures` (continuity, row folding, sorting / grouping, answers, terminal frames, protocol samples). Seconds locally and in CI.
- **Page models**: fakes + `CompletableDeferred` gates to deterministically create late results, cancellation, and scope switches. No mocking library.
- **Screenshots**: Roborazzi for the component catalog and key pages. Matrix: light / dark × text size (default / large / accessibility) × narrow screen, fictional data. Record references in a fixed environment (Mac / Linux font rendering differs). Final acceptance still uses actual emulator captures; Robolectric blur and WebView are unreliable.
- **Flow UI**: Compose UI tests; instrumentation uses `adb reverse tcp:18660 tcp:18660` to connect to `scripts/mock/mock_hub.py` (PR #84 lesson).
- **Debug entry points**: only in `src/debug/`.
  - command: `adb shell am start -n <package>/.debug.DebugEntryActivity --es openSession m/s`;
  - pairing: `--es pairURL "persona://pair?url=…&token=mock"`;
  - dark mode: `adb shell cmd uimode night yes`;
  - fixed status bar: SystemUI demo mode;
  - switch names use collision-free prefixes such as `debug.open…`; Kotlin enum names remain as strings in dex.
- **Release check**: unpack APK, scan with `apkanalyzer dex` / `aapt2 dump strings`; fail on any debug-switch name, matching iOS `strings` checks.
- **Performance**: Macrobenchmark covers cold start to cached list, large-chat open to first screen, and jank during fast long-chat scrolling; guard with Baseline Profile. Persistent-connection mode additionally measures **hourly battery drain** and **connection survival while locked**. Quoted figures must be measured, at least 10 samples, with min / p50 / p90 / max / mean.
- **Physical Chinese-ROM devices** (5.18), mandatory per device:
  1. First-install warning, first-launch notification permission, correct Settings destination for every checklist item;
  2. test notification after 30 seconds locked: sound, screen wake, lock-screen visibility;
  3. locked and unplugged for 8 hours: hub records disconnects, phone records `ApplicationExitInfo`, **at least 10 nights**;
  4. hourly battery drain (system battery page + `dumpsys batterystats`);
  5. service recovery after swiping the card, stopping in "Active apps," rebooting;
  6. one self-update;
  7. active-state screenshots of status bar, lock screen, island, in light / dark;
  8. image picker, location, WebView version, QR scanning.
  - `adb shell dumpsys deviceidle force-idle` simulates only native Doze, not vendor policies.
- **Parallel Agents**: one worktree per lane, own AVD / ports (`emulator -avd <name> -port <even-number>`, distinguish with `adb -s emulator-<port>`); Gradle configuration and build caches enabled.

## 9. Distribution, signing, and configuration

No Play Store or Chinese app-store listing (since 2023, apps require registration before store distribution; sideloading is simpler for a personal tool). Basis: [research/android-cn-roms.md](research/android-cn-roms.md), section 7.

- **Development**: `scripts/android.sh install` via adb (USB or wireless debugging). PR #84 Xiaomi lesson: enable "Install via USB" and "USB debugging (Security settings)" in Developer options; the phone confirmation auto-denies after a few seconds.
- **Owner and testers**: signed APK hosted by hub ([hub-changes.md](hub-changes.md) 2.2).
- **First installation**: no app or device token yet.
  - `personad hub pair --android` prints a LAN download link beside the QR code (same temporary token); open in the phone browser. Alternatively transfer APK via adb or another method.
  - Vendors intervene: all Android 8+ require "Install unknown apps" permission for the browser / file manager; HyperOS "Security protection" needs identity verification to disable; HarmonyOS 4 Pure mode enhanced protection allows only apps checked by Huawei AppGallery; HarmonyOS 5+ only "Open with EasyAbroad."
  - Download page gives brand-specific installation steps, explaining risk warnings (not listed in an app store). This happens only once, acceptable.
- **In-app updates** (parity.md N71):
  - foreground app asks hub for a newer version (`/api/v1/app/android/latest`); Settings shows "Update available" and release notes;
  - download over TLS (same public-key pin), verify SHA-256 first; system also verifies matching installed-app signature;
  - user grants Persona "Install unknown apps" (`canRequestPackageInstalls()`; otherwise open `ACTION_MANAGE_UNKNOWN_APP_SOURCES`);
  - install via `PackageInstaller` session, **system confirmation by default** (`USER_ACTION_REQUIRED`). HyperOS silent-update requests fail directly with `INSTALL_FAILED_ABORTED`, no prompt; treat as a separate error, not user cancellation. Test silent updates only on stock Android; handle `STATUS_PENDING_USER_ACTION` by opening system confirmation;
  - `setRequestUpdateOwnership` applies only on first install, intended for stores; useless for self-update;
  - updates kill the process: persistent service restarts via `MY_PACKAGE_REPLACED` (foreground-service background-start exemption; Chinese-ROM delivery may depend on autostart settings—test).
- **Signing**: release keystore outside the repository; path / alias in `android.signing` in `config.json`; password from keychain or environment. **Losing the key prevents upgrades to installed apps**; back it up.
- **Version**: `versionCode` maximum 2100000000 cannot fit iOS's 12-digit build number; use `yyyymmddHH` (10 digits). `versionName` follows iOS format.
- **Google developer verification** (global from 2027): only on Google-certified devices, verification delivered through Google Play services; adb installation unaffected. Mainland phones lack Google services and certification, so are largely unaffected (inference, not explicitly stated by Google). Only installation on overseas phones with Google services requires registering package name / signing key through a free limited-distribution account (at most 20 devices).
- **Crash logs**: no Firebase. Store `ApplicationExitInfo` and uncaught exceptions locally, uploading to hub on next launch ([hub-changes.md](hub-changes.md) 4.15, P2).
- **Privacy**: `scripts/privacy_check.py` covers `android/` and `spec/`. No keystore or `local.properties` in the repository; `android/.gitignore` already excludes some.

## 10. Risks and validation still needed

| Risk | Validation / response |
| --- | --- |
| Persistent-connection battery use and survival | A1 measures mainstream Chinese-ROM devices: hourly drain, connection after 8 locked hours, restart delay after system kill; tune background heartbeat / checklist by results |
| Local network permission (target 37) | Complete 5.1 before raising target; real-device tests for home public IPv6 in the same /64 and VPN behavior |
| Public direct access fails at home (modem IPv6 firewall, blocked ports, hub TUN proxy) | `personad hub doctor` self-check; app diagnostics explains cause; recommend DDNS ([README.md](README.md) D9) |
| Carrier NAT / firewall idle timeout (no public 4G / 5G data) | A1 tests three carriers; background heartbeat adapts by network (5.9) |
| Long-chat performance | A1 measures open time and scroll jank on a 650-command sample |
| Client drift | Fixtures are a hard constraint; update them before changing rules |
| Chinese-ROM restrictions (notifications off by default, autostart off, `START_STICKY` cannot restart foreground service after kill) | Checklist checks / guides each item (5.18); failed `startForeground` waits for next exemption; disabling battery optimization is itself an exemption (5.9); record vendor behavior in `docs/android/android.md` "Lessons" |
| HarmonyOS 4 local notifications default silent, limited to 2–5 daily | Guide disabling Silent notifications, enabling banners / lock screen; Needs you also counted in persistent notification; test whether persistent notifications count toward quota |
| Process alive but receives nothing (Honor Smart data saver, Huawei Stay connected while asleep, vendor freezing) | Checklist names these settings; hub records disconnects for diagnostics; 8-hour locked-device tests, at least 10 nights |
| Settings component names break across versions | Data-driven ordered attempts, app-details fallback; log results; OS name / version in `client_hello`; cloud-device test each major version |
| Self-update behavior by vendor (HyperOS silent updates confirmed to fail) | System confirmation by default; self-update once per physical device |
| HarmonyOS 5+ cannot install APKs | No first-version promise; explain on pairing page when EasyAbroad detected |
| AGP 9.5 requires newer Android Studio | CLI builds unaffected; IDE users upgrade to Rabbit 2 |
| AI uses old patterns for new Room, Navigation 3, Compose 1.12 APIs | Document versions and idioms in `CLAUDE.md` / `docs/android/android.md`; pin versions in catalog |
| WebView gestures in custom overlay | Prototype link overlay first |
