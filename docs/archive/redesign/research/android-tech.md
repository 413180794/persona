# Persona Android technology research notes

- Research date: **2026-10-30**. Versions were actually checked that day; sources are linked at the end of each section.
- Markers: ✅ Verified (with source); 💭 experience-based judgment / inference (not individually verified; recheck before citing); ⏳ to be added.
- Scope: research and design only, no code or repository changes.

---

## 0. Background: Persona's Android requirements (from README / CLAUDE.md / docs/apple/ios.md / docs/integrations/terminal.md / docs/protocol/protocol.md)

- The phone connects only to a self-hosted hub (Go, `personad hub`, default `:18660`), using HTTP + WebSocket and Bearer tokens (one `jd_…` token per device; Watch has the `answer` role).
  Hub **often uses plaintext HTTP on LAN / VPN**. JSON fields are snake_case; timestamps RFC3339.
- Four tabs: Persona (headless Claude assistant, text streaming) / Development (Agents list + chat) / Inbox (Lark + mail, Lark cards rendered from the gateway, native Lark document reader) / Settings.
- Chat challenges: inverted list (latest at offset 0), hundreds of work steps as independent lazy rows, turn collapsing (work rows, wakes, recap, queued messages),
  Markdown (tables, code), tool cards, red / green diffs (horizontal scrolling), on-demand images + full-screen viewing / editing, long-press action bar, share cards, sub-Agents,
  question / plan / permission / terminal-dialog interaction panels, sending bubbles + offline outbox (`client_msg_id` deduplication, persisted).
- Terminal: herdr observe frames (VT subset: CUP, ED, SGR, DECTCEM, 2026 synchronized output, OSC 8) → custom grid (wide characters occupy two cells, box drawing rendered as lines) → custom drawing.
  Phone takeover at phone columns × rows, key bar (sticky / locked / held modifiers), direct typing (IME, pinyin marked text), wheel forwarding, selection / copy.
- Links: in-App bottom sheet, initially full-screen, draggable to 60% with underlying chat still scrollable / editable. WebView preload pool (first 3 links per reply, 6 retained, LRU).
  Selected web text offers AI edit / Use as context.
- Machine-monitoring charts (1/6/24 hours, gaps not connected, press to inspect time); QR pairing; pushes (buttons, unlock-required Allow, direct feedback input).
  Lock-screen Live Activities / Dynamic Island (Fleet summary: Needs you / Working / Unread), Apple Watch, Siri / Shortcuts (Ask Claude, status, approve first pending item).
- Phone tools `phone_*`: contacts, calendar (read + create), reminders (list / create / complete), one-shot location, health (daily totals: steps, distance, active energy, heart rate, resting heart rate, sleep, weight),
  photos (recent metadata + fetch image), device info, clipboard read / write, local notifications. Writes require confirmation first.
- Five interface styles (Liquid Glass default, Claude, Clay, Island, Lark), tokenized themes; one state-color table independent of style.
- Connection state occupies the title (Waiting for network / Connecting… / Updating…, 1.5-second debounce, ping over 5 seconds means weak network, 45 seconds without messages means dead). Background push / pull sync (`revs` + `after_seq` deltas); show disk cache before refresh.
- Quality: chart-topping App Store standard; inspect light / dark screenshots of every page; DEBUG launch arguments go directly to screenshot pages. Release must contain no debug-switch strings (`release.sh` checks with `strings`).

## Local environment (verified by coordinator 2026-10-30)

- Apple Silicon Mac; Android Studio **2025.3** (bundled JBR 21); SDK platforms android-34 / 36.1 / 37.0 / 37.1; build-tools 37.0.0;
  arm64 system images API 35 / 37.x; several existing AVDs; Homebrew openjdk@17.
- 💭 Note: stable Studio is already **Rabbit 2 (2026.2.2)**, so local 2025.3 is about 3 major versions behind. Command-line Gradle builds are unaffected by IDE version,
  but AGP 9.5 requires Studio Rabbit 2; Panda (2025.3.x) supports only AGP 9.0–9.2 (compatibility table in section 10.4). IDE use requires an upgrade.

---

## 1. Overall approach ✅ (decision made; details below)

### Recommendation

**Use native Kotlin + Jetpack Compose for Android; keep SwiftUI on iOS without introducing Kotlin. Ensure behavioral consistency through shared golden fixtures
(JSON input → expected display rows / state), not shared code.** Android core modules (protocol models, connections, ChatStore, turn collapsing, outbox, sync)
should be **pure Kotlin without android.* dependencies** (kotlinx.serialization / coroutines / kotlinx-datetime / okio). A future KMP move then changes Gradle plugins,
not implementations. Reevaluate sharing with iOS after Swift export reaches Beta and Android is stable.

### Three approaches compared

| Approach | Decision | Rationale |
| --- | --- | --- |
| A. Native Kotlin + Compose (recommended) | ✅ Adopt | Best Android experience (first-party Material / system capabilities / performance tools); leaves 42,000 lines of iOS SwiftUI unaffected; simplest build chain, avoiding contention between parallel AI Agents; Compose and SwiftUI have similar mental models, making translation from Swift inexpensive for AI |
| B. KMP shared core (protocol / models / sync / transcript collapsing) | ⏸ Defer, but design module boundaries for it | Real benefits: tricky collapsing, resume, deduplication, and outbox logic written once. But iOS must replace a tested Swift data layer with Kotlin. Swift export remains **Alpha** (since 2.4.0; officially "breaking changes are expected"). Obj-C export + SKIE works but adds a third-party compiler plugin tied to Kotlin versions. Exported Kotlin types are not Sendable under Swift 6 strict concurrency and need wrappers. Kotlin/Native slows Xcode builds; Watch, Widget / Live Activity extensions must also link K/N frameworks (extensions have memory limits); cross-language debugging |
| C. Compose Multiplatform sharing UI too | ❌ Reject | iOS requires chart-topping App quality and iOS 26 Liquid Glass. CMP has no official Liquid Glass. Overlaying UIKit native glass on Compose (`placedAsOverlay` in 1.10) fails with scrolling content, dialogs, and sheets, requiring the reverse: Compose embedded in SwiftUI. CMP 1.11 native iOS text input is still experimental opt-in. Sharing UI would sacrifice iOS feel, contrary to requirements |

### Swift integration costs for KMP core sharing (✅ as of 2026-10-30)

- **Swift export**: officially **Alpha** since Kotlin 2.4.0 (2026-06). Supports suspend → `async`, Flow → `AsyncSequence` (`.asAsyncSequence()`),
  sealed → Swift enum (`.sealedType()`, added in 2.4.20), enum → native Swift enum, cross-language inheritance. **Generics largely unsupported** (erased to upper bounds),
  `List/Set/Map` subtypes ignored in export, only direct Xcode integration supported, no migration tools. 2.4.20 can generate SwiftPM `Package.swift` automatically.
  No Beta / Stable schedule found.
- **SKIE** (Touchlab, built on Obj-C export): latest **0.10.15** (2026-09-25, Kotlin 2.4.20 support), about 3 weeks after Kotlin 2.4.20 (09-07).
  Each Kotlin upgrade waits for SKIE support, a version lock in the KMP approach.
- **Compose Multiplatform**: 1.11.0 stable (2026-05-13), experimental native iOS text input, concurrent rendering enabled by default; 1.12.x in 2026-08/09. No official Liquid Glass.
- 💭 If sharing later, start with protocol models + pure turn-collapsing functions behind a Swift facade (no Kotlin types leaking into SwiftUI views).
  WebSocket / cache / background behavior remain platform-specific; Watch and extensions keep thin Swift models.

### Risks

- Two implementations drift. Guard with shared golden fixtures (suggested root `fixtures/`, same JSON read by XCTest and JUnit); existing hub transcript fixtures can be reused.
- 💭 Another deduplication option: move UI-state-independent turn segmentation / answer selection (such as pickAnswerAcrossWakes) to hub (Go) and deliver precomputed results, thinning both clients. Cost: protocol changes and old-App compatibility.

Sources:
- Swift export (Alpha, support matrix): https://kotlinlang.org/docs/native-swift-export.html
- Kotlin 2.4.20: https://kotlinlang.org/docs/whatsnew2420.html ; 2.4.0 announcement: https://blog.jetbrains.com/kotlin/2026/06/kotlin-2-4-0-released/
- SKIE releases: https://github.com/touchlab/SKIE/releases
- CMP 1.11.0: https://blog.jetbrains.com/kotlin/2026/05/compose-multiplatform-1-11-0/ ; CMP Liquid Glass layering: https://medium.com/mateedevs/part-2-liquid-glass-components-in-compose-multiplatform-1-10-0-beta01-20f78471d8f7

---

## 2. Project structure and versions ✅

### Current versions (✅ 2026-10-30)

| Component | Version | Notes |
| --- | --- | --- |
| Kotlin | **2.4.21** (2026-10-08) | 2.4.0 language version released 2026-06-03; 2.5.0 planned 2026-12; since 2.4, each line gets 18 months of security support |
| AGP | **9.5.0** (2026-10, current stable; 9.4.0 in 2026-09; 9.6 preview) | Requires Gradle ≥ 9.6.0, JDK 17, Build Tools 36.0.0; supports up to API 37. AGP 10 will require new Variant API. Studio Rabbit 2 required for IDE support of 9.5 (section 10) |
| Gradle | **9.8.2** (2026-10-19) | |
| Compose BOM | **2026.09.00** | Compose ui/foundation/runtime 1.12.1, material3 1.4.0, material3-adaptive 1.3.0, material-icons 1.7.8 |
| material3 (Expressive) | Stable 1.4.0; **1.5.0-beta02** (2026-10-21) | Expressive APIs in 1.4.0 still need `@ExperimentalMaterial3ExpressiveApi`; 1.5.0 alpha20–26 progressively stabilize SplitButton, FloatingToolbar, ButtonGroup, Flexible TopAppBar, list items, SearchBar, BottomAppBar, etc. |
| Android Studio | Stable **Rabbit 2 (2026.2.2)** | Local installation is 2025.3 |
| Android platform | Android 17 = **API 37** (stable 2026-06-16, QPR1 2026-09-15, including 37.1 minor SDK) | |

### Essential AGP 9 changes (✅)

- **Built-in Kotlin**: no longer apply `org.jetbrains.kotlin.android` (incompatible with the new DSL); AGP 9 runtime depends on KGP ≥ 2.2.10.
- **New DSL enabled by default**: replace old `applicationVariants` etc. with `androidComponents.onVariants`; AGP 10 removes the `android.newDsl=false` escape hatch.
- **KMP**: KMP modules must use `com.android.kotlin.multiplatform.library`, not coexist with `com.android.application` in one subproject (the App needs its own module).
- Unspecified `targetSdk` defaults to compileSdk (set explicitly); Java defaults to 11. R8: strict keep rules (`-keep class A` no longer implicitly keeps the no-argument constructor),
  optimized resource shrinking on by default, only `proguard-android-optimize.txt` allowed.

### Modularization (✅ Now in Android + 💭 Persona proposal)

- Now in Android (NiA, official sample): `build-logic/` (convention plugins in an included build), `app`, `app-nia-catalog` (component catalog App), `benchmarks` (Macrobenchmark + Baseline Profile generation),
  `core/*`, `feature/*`, `lint`, `sync`, `ui-test-hilt-manifest`; Hilt; no mocking library (Fakes + Hilt test APIs); Roborazzi screenshots (`recordRoborazziDemoDebug` / `verifyRoborazziDemoDebug`, references recorded on CI Linux).
  `demo` / `prod` flavors (demo uses local static data); Compose compiler reports `-PenableComposeCompilerMetrics=true -PenableComposeCompilerReports=true`; root `compose_compiler_config.conf` and `AGENTS.md`.
- 💭 Suggested Persona modules, split so parallel AI Agents each own separate modules:

| Module | Scope | Android-dependent? |
| --- | --- | --- |
| `build-logic/convention` | Plugins such as `persona.android.application`, `persona.android.library`, `persona.android.compose`, `persona.jvm.library`, `persona.hilt`, `persona.screenshot-test` | — |
| `core:model` | Protocol data classes (Machine / Session / Item / Interaction / FleetState …), kotlinx.serialization | No (pure Kotlin, KMP-ready) |
| `core:protocol` | WebSocket envelopes, resume (since_seq / epoch+seq), capability negotiation | No |
| `core:chat` | ChatStore merging, turn collapsing (work rows, wakes, recap, queue), display-row derivation | No (main target for golden fixtures) |
| `core:terminal` | VT-subset parser → `TerminalGrid` (wide characters, box drawing) | No |
| `core:network` | OkHttp `HubApi` / `HubSocket`, connection state machine, local-network permission checks | Yes |
| `core:data` | Repositories, outbox, background sync, disk cache | Yes |
| `core:database` / `core:datastore` | Room / DataStore (section 5) | Yes |
| `core:designsystem` | Theme tokens, 5 styles, glass / soft-block surfaces, state-color table, Chip / CountBadge | Yes |
| `core:ui` | Markdown, syntax highlighting, diffs, monospace text, image viewer, link sheet, WebView pool | Yes |
| `core:phonetools` | `phone_*` tool registry and data-source implementations | Yes |
| `feature:assistant` / `feature:agents` / `feature:chat` / `feature:terminal` / `feature:inbox` / `feature:lark` / `feature:mail` / `feature:machines` / `feature:settings` / `feature:pairing` | Pages | Yes |
| `app` | Navigation, notifications, FCM, Live Updates, shortcuts, DEBUG direct entry points | Yes |
| `wear` | Wear OS App (section 7) | Yes |
| `benchmarks`, `app-catalog` | Macrobenchmark / Baseline Profile; component + 5-style catalog App, also used for screenshot tests | Yes |

- One version catalog `gradle/libs.versions.toml`; BOM manages Compose versions. Convention plugins centralize compileSdk / minSdk / JVM target / Compose compiler options / lint, leaving only a few lines in each module build file.
- 💭 Feature modules should not depend on each other, only core. Put Nav3 `NavKey` routes in small feature `api` modules or `core:navigation`; NiA api / impl separation was not seen on this page, so adopt as needed.

Source: https://github.com/android/nowinandroid (README)

### SDK recommendations (💭 based on ✅ platform facts)

- **compileSdk 37, targetSdk 37**: design the new App for Android 17 rules from the outset, especially local-network permissions (section 4). Play currently requires only ≥ 36 (since 2026-08-31).
- **minSdk 33 (Android 13)**: AGSL `RuntimeShader` (needed for Liquid Glass refraction), `POST_NOTIFICATIONS`, system photo picker, per-App languages all start at 33.
  `RenderEffect` blur starts at 31; Health Connect is built in from 34 (requires an APK on 33); Live Updates start at 36 (ordinary persistent-notification fallback below).
  For personal use only on a new phone, minSdk 35/36 reduces branches.

Sources:
- Kotlin releases: https://kotlinlang.org/docs/releases.html
- AGP 9.4: https://developer.android.com/build/releases/gradle-plugin ; AGP 9.0 notes: https://developer.android.com/build/releases/agp-9-0-0-release-notes
- Gradle: https://gradle.org/releases/
- Compose BOM mapping: https://developer.android.com/develop/ui/compose/bom/bom-mapping
- Material3 releases: https://developer.android.com/jetpack/androidx/releases/compose-material3
- Android Studio: https://developer.android.com/studio/releases
- Android 17: https://en.wikipedia.org/wiki/Android_17
- Play target API: https://developer.android.com/google/play/requirements/target-sdk

---

## 3. Architecture ✅

### 3.1 State management: UDF (unidirectional data flow), no heavy MVI framework

- Recommendation (💭 design judgment): **Repository / Store (App-wide singleton, source of truth) → ViewModel (derived UiState, `StateFlow`) → Compose (`collectAsStateWithLifecycle`) → event callbacks**.
  - One `ChatStore` per session (deduplicate by ID, order by seq, merge pushes / paging / disk cache) in an **App-wide** `SessionRepository`, not a ViewModel.
    Background sync (`revs` → `after_seq`) must write even when the page is closed, as with iOS `AppModel` `ChatStore`.
  - ViewModels derive display rows only (iOS `buildRows` / `chatLines`), on `Dispatchers.Default`, producing immutable lists (`@Immutable` data classes + `ImmutableList`), cached by `version`.
  - Model one-off effects ("Last action did not take effect" toast, vibration) as state where possible (ID-bearing message queues); minimize `Channel` use.
- Alternatives: Slack Circuit / Molecule (Compose presenters), good for large teams standardizing a pattern; another concept layer for one person + parallel AI. 💭 Not recommended as default.

### 3.2 Dependency injection comparison (✅ versions 2026-10-30)

| Option | Latest version | Nature | Assessment |
| --- | --- | --- | --- |
| **Hilt (Dagger)** | **2.60.1** (2026-07-06) | KSP annotation processing, compile-time validation | **Recommended**. Most mature on Android, official Jetpack integration (ViewModel, WorkManager `@HiltWorker`, Compose / Nav3 ViewModel access), most familiar to AI. Hilt Gradle plugin **requires AGP 9** since 2.59; Hilt minSdk 23 and multidex removed since 2.60. Cost: KSP generation slows incremental builds, Android only |
| **Metro** (Zac Sweers) | **1.5.1** (2026-10-29); 1.0.0 stable 2026-04-27 | Kotlin compiler plugin (FIR/IR), no KSP/KAPT | **Strong alternative**. Fast builds, KMP all platforms, `metrox-android` / `metrox-viewmodel(-compose)`, Dagger / Guice / javax / jakarta interoperability. Experimental Hilt `@InstallIn` / `@EntryPoint` interoperability since 1.2.0 enables gradual migration. Risk: tied to Kotlin compiler versions (runtime target Kotlin 2.3.0, tested 2.5.0-Beta2), smaller ecosystem than Hilt |
| Koin | **4.2.2** (2026-06-15) | Runtime DSL | Not recommended: graph errors surface at runtime; compile-time Koin Compiler Plugin is still **1.0.0-RC1**. Has `koin-compose-navigation3` |
| kotlin-inject | **0.9.0** (2026-01) | KSP | Not recommended: slow updates (one release in 2025), largely one maintainer |

- 💭 For KMP-ready core modules, use constructor injection only (`@Inject` constructors or manual factories); bindings belong in app-layer Hilt modules. Future KMP migration can switch to Metro gradually through Hilt interoperability.
- Latest KSP **2.3.12** (2026-09-09), versions no longer tied to Kotlin; KSP1 is being phased out (`ksp.useKSP2` deprecated).

### 3.3 Navigation: Navigation 3 (recommended) vs type-safe Navigation Compose

- **Navigation 3 stable 1.2.0 (2026-09-23)**; 1.0.0 stable 2025-11-19, 1.1.0 2026-04-08; 1.3.0-alpha03 (2026-10-21). Requires minSdk 23, compileSdk 37.
  - Back stack is an App-owned state list (`rememberNavBackStack`, `@Serializable` `NavKey`, restorable after process death); `NavDisplay` + `entryProvider { entry<Key> {…} }`.
  - Scene / `SceneStrategy` (list-detail, `DialogSceneStrategy`, `OverlayScene`), shared elements, predictive back (improved in 1.2), results via `ResultEventBus` (1.2),
    **deep links (1.2, `UriDeepLinkMatcher`, request construction from Intent)** for notification-opened sessions and DEBUG direct pages.
  - Entry-scoped ViewModels: `rememberViewModelStoreNavEntryDecorator()` in `androidx.lifecycle:lifecycle-viewmodel-navigation3`, part of Lifecycle **2.11.0** (stable 2026-06-17).
  - Persona benefits: iOS pitfalls such as mixed view / value NavigationLinks inserting pages underneath, or search-state pushes forcing navigation bars visible, do not exist in a "back stack = list" model.
    Each tab has its own stack (official recipes include multiple stacks); Chat → Files → Git uses an in-page Pager, not stack entries.
  - Risks: evolving APIs (1.1 deprecated `then` chains and replaced deep-link factories with constructors); UI layer `navigation3-ui` is Android only.
- Alternative: Navigation Compose 2.9+ `@Serializable` type-safe routes, mature but library-owned stack, less direct for bottom sheets, multiple stacks, and custom transitions.

### 3.4 Coroutine and Flow conventions (💭 proposed rules; APIs all stable)

- Versions: kotlinx.coroutines **1.11.0** (2026-05-08); kotlinx.serialization **1.12.0** (2026-10-19, based on Kotlin 2.4.10).
- Scopes: UI uses `viewModelScope`; connection, sync, outbox use App-wide `CoroutineScope(SupervisorJob() + Dispatchers.Default)`,
  following `ProcessLifecycleOwner` (connect on ON_START, disconnect on ON_STOP), matching iOS background WebSocket disconnection and push wakeups.
- Threads: JSON decoding, display-row derivation, Markdown parsing on `Dispatchers.Default`; disk on `Dispatchers.IO`. Inject dispatchers; tests use `StandardTestDispatcher`.
- UI state: `StateFlow` + `stateIn(scope, WhileSubscribed(5_000), initial)`. High-frequency sources (terminal frames, spinner rows) use `conflate()` / 30 ms coalescing.
  Never display `CancellationException` as an error (like iOS `Error.shown` filtering cancellation); `runCatching` must rethrow cancellation.
- Tests: `kotlinx-coroutines-test` + Turbine.

### 3.5 Compose stability and performance conventions

- ✅ Compose 1.12.x (BOM 2026.09.00); **Compose 1.12 requires compileSdk 37 and AGP ≥ 9.2.0**. `LazyLayoutCacheWindow` (prefetch window) began stabilization in 1.11; stable in 1.13 beta, deprecating `LazyListPrefetchStrategy`.
  1.12 `SelectionContainer` auto-scrolls during drag selection; 1.13 deprecates `BasicTextField(value, onValueChange)` in favor of `TextFieldState`.
- 💭 Rules reflecting iOS lessons:
  - Strong skipping is default since Kotlin 2.0.20. Row models use `@Immutable` data classes + `kotlinx.collections.immutable`, or stable types declared in `compose_compiler_config.conf`.
  - Stable `key` and `contentType` for every LazyColumn row; hundreds of work steps remain independent rows, not one large Column, as on iOS.
  - Per-frame values such as gesture-following top-bar collapse live in separate State, read only in `Modifier.graphicsLayer {}` / `offset {}` lambdas (draw / layout only, not whole-page recomposition).
  - Message-derived data (Markdown AST, link detection) lives in an out-of-composition LRU keyed by message ID, warmed in the background. Do not create WebViews while scrolling.
  - Inspect unstable parameters with Compose compiler metrics / reports; guard cold start and scrolling with Baseline Profile + Macrobenchmark (section 9).

Sources:
- Navigation 3: https://developer.android.com/jetpack/androidx/releases/navigation3
- Lifecycle: https://developer.android.com/jetpack/androidx/releases/lifecycle
- Dagger / Hilt: https://github.com/google/dagger/releases
- Metro: https://github.com/ZacSweers/metro ; CHANGELOG https://raw.githubusercontent.com/ZacSweers/metro/main/CHANGELOG.md
- Koin: https://github.com/InsertKoinIO/koin/releases
- kotlin-inject: https://github.com/evant/kotlin-inject/releases
- KSP: https://github.com/google/ksp/releases
- kotlinx.coroutines: https://github.com/Kotlin/kotlinx.coroutines/releases ; kotlinx.serialization: https://github.com/Kotlin/kotlinx.serialization/releases
- Compose Foundation: https://developer.android.com/jetpack/androidx/releases/compose-foundation

## 4. Networking ✅

### Android 17 local-network permission: Persona's most significant platform change (✅)

- targetSdk ≥ 37 requires runtime **`ACCESS_LOCAL_NETWORK`** for **local-network** access (dangerous permission in `NEARBY_DEVICES`;
  no new prompt if another permission in that group, such as Bluetooth, is already granted). targetSdk ≤ 36 still receives access implicitly through INTERNET, and **official guidance says lower targets must not declare / request it**.
- Local network = traffic through broadcast-capable interfaces such as Wi-Fi / Ethernet, **excluding cellular and VPN**. IPv4 ranges: 169.254/16, **100.64/10 (CGNAT)**, 10/8, 172.16/12, 192.168/16,
  plus multicast / broadcast; IPv6 includes link-local and directly connected routes. All network APIs (OkHttp, Cronet, WebView inheriting host permissions) are restricted, as is `.local` resolution.
- Without permission, **TCP times out** rather than immediately rejecting; UDP returns EPERM. It looks like hub unreachability, so check permission before connecting and explain in the UI.
- Persona: direct LAN-hub access on home / office Wi-Fi needs it; VPN access (such as Tailscale) is exempt by definition (💭 confirm on a physical device).
  **Request after scanning during pairing**. On denial, show Needs local-network permission, not Connecting….
- Test on Android 16 with `adb shell am compat enable RESTRICT_LOCAL_NETWORK <pkg>` and restart.
- ✅ **Play policy** (announced 2026-10-29): target 37+ apps requesting `ACCESS_LOCAL_NETWORK` must explain in Play Console why device selection (`NsdManager`) is insufficient. Declaration starts 2027-06, **mandatory from 2027-08-25**.
  Non-Play distribution (adb / Firebase) is exempt from policy, but runtime permission remains required.

### Plaintext HTTP: Network Security Config (✅)

- targetSdk ≥ 28 blocks plaintext by default. `<domain>` supports exact hosts (including a single IP) or host + subdomains, **not CIDR / wildcards**. `debug-overrides` handles trust anchors only and cannot allow plaintext separately.
- Persona learns hub addresses only at QR pairing, requiring **`<base-config cleartextTrafficPermitted="true">`**. 💭 Add App-side validation:
  allow `http://` only for private / VPN addresses (10/8, 172.16/12, 192.168/16, 100.64/10, 169.254/16, `.local`, loopback, explicitly confirmed hostnames),
  otherwise warn during pairing. Do not generate build-time domain-config from `config.json`: changing hubs would require reinstalling and violate the no-hardcoded-personal-values rule.
- HTTPS with self-signed hub certificates: `<trust-anchors>` `@raw/` certificates are build-time only; pairing-scanned certificates need code handling (custom OkHttp `X509TrustManager` / certificate pinning).
  Android 17 enables CT by default but exempts connections with custom trust anchors; ECH uses `<domainEncryption>` and affects only TLS.

### HTTP / WebSocket libraries (✅ versions)

| Library | Latest | Decision |
| --- | --- | --- |
| **OkHttp** | **5.5.0** (2026-08-16; 5.4.0 2026-06-08) | **Recommended**. One REST + WebSocket stack; Coil 3 also uses it (shared pool / cache-directory policy); MockWebServer tests. Optional ECH since 5.5 (Android 17 TLS only), new DNS API. Repository moved to `lysine-dev/okhttp` with Commonhaus in 2026; package names unchanged |
| Ktor client | **3.6.0** (2026-09-16) | Alternative only if sharing networking through KMP later. Android usually wraps the OkHttp engine anyway, adding a layer; OkHttp-engine WebSocket ping fixed only in 3.5.0 (KTOR-4752) |
| Retrofit | Version unchecked (optional) | Hub REST is small and simple; 💭 manual `HubApi` (OkHttp + kotlinx.serialization) is enough |

- **WebSocket heartbeat pitfall (✅ verified)**: OkHttp `pingInterval` is automatic—send ping, and if no pong arrives before the next ping, fail with `SocketTimeoutException` ("sent ping but didn't receive pong…").
  **No public manual-ping API or `WebSocketListener.onPong` callback** (square/okhttp #7370, #3197, #6146 remain unimplemented), so RTT is unavailable. iOS uses `URLSessionWebSocketTask.sendPing` and a 5-second pong threshold for weak networks.
  For the same Connecting… (weak network) behavior on Android, add **client-initiated application-level ping** to the protocol (currently only hub → App `ping` / App → hub `pong`):
  App sends `{"type":"ping","id":n}`, hub echoes `{"type":"pong","id":n}`, timing by ID on both clients. OkHttp `pingInterval` (e.g. 20 s) is only dead-connection fallback.
  Without protocol changes, Ktor CIO raw control frames would be needed, not worth it.
- Reconnect: exponential backoff + jitter, capped at 20 s like iOS. `ConnectivityManager.registerDefaultNetworkCallback` (NWPathMonitor equivalent) reconnects immediately on `onAvailable`,
  pings immediately on network changes. `ProcessLifecycleOwner` connects foreground / disconnects background; resubscribe with `since_seq` after reconnect.
- JSON: kotlinx.serialization, `Json { ignoreUnknownKeys = true; explicitNulls = false }`, snake_case via `@SerialName` (`JsonNamingStrategy.SnakeCase` is still experimental; 💭 avoid).
  Dispatch WebSocket envelopes by `type` (sealed classes with `@JsonClassDiscriminator("type")`, or inspect type then decode; ignore unknown types).
  Large pages (1–5 MB with images) decode from streams using `kotlinx-serialization-json-okio` on `Dispatchers.Default`.

Sources:
- Network Security Config: https://developer.android.com/privacy-and-security/security-config
- OkHttp changelog: https://square.github.io/okhttp/changelogs/changelog , https://github.com/square/okhttp/blob/master/CHANGELOG.md
- Ktor releases: https://github.com/ktorio/ktor/releases
- Persona protocol (WebSocket message table): repository `docs/protocol/protocol.md`

### Other Android 17 (target 37) networking changes: ✅ Certificate Transparency on by default; ECH (new `<domainEncryption>` in Network Security Config).

Sources:
- https://developer.android.com/about/versions/17/behavior-changes-17
- https://developer.android.com/privacy-and-security/local-network-permission
- https://developer.android.com/privacy-and-security/local-network-definition

## 5. Storage ✅

### 5.1 Database: Room 3 (recommended) vs Room 2 vs SQLDelight

| Option | Latest | Key points |
| --- | --- | --- |
| **Room 3** (`androidx.room3`) | **3.0.3** (2026-09-09); 3.0.0 stable 2026-07-01; 3.1.0-alpha03 (2026-10-21) | **Recommended** for a new project without migration baggage. New packages / Maven group; Kotlin generation only, KSP required; **all database operations are coroutine APIs** (DAO suspend or Flow); `SQLiteDriver` only, no SupportSQLite / Cursor; all-platform KMP, including iOS / web; FTS5 (`@Fts5`); InvalidationTracker becomes Flow. Paging requires explicit `@DaoReturnTypeConverters(PagingSourceDaoReturnTypeConverter::class)` |
| Room 2 | 2.8.5 (2026-09-09) | Mature, most examples; KMP since 2.7; minSdk 23 |
| SQLDelight | 2.4.1 (2026-10-06) | SQL-first, type-safe, KMP; AGP 9 new DSL / built-in Kotlin support since 2.3.2. Good for handwritten SQL; less Jetpack integration (Paging / WorkManager examples) than Room |

- 💭 Room 3 risks: stable for only 4 months; 3.0.x patches mostly fix transaction deadlocks / connection state after cancellation. AI training mostly contains Room 2 syntax (packages changed; `@TypeConverter` renamed `@ColumnTypeConverter`).
  State Use Room 3 explicitly in `AGENTS.md` / `CLAUDE.md`. Conservative choice: Room 2.8.5, migrate later (officially, KMP migration mainly changes imports).

### 5.2 What goes where (💭 design proposal, matching iOS StateCache / ChatCache / Outbox)

| Data | Android location | Notes |
| --- | --- | --- |
| Per-session items by seq | Room `items(machine_id, session_id, seq, id, json)` + session cursors (covered / rev / has_more) | Incremental `after_seq` background sync for 40 sessions; show latest two turns from database before server merge; better for deltas than iOS whole JSON files |
| Outbox (pending messages, `client_msg_id`) | Room database under `filesDir` | Must persist; Android may clear `cacheDir` under storage pressure |
| Machines / sessions / pending interactions / Inbox / latest 80 Lark-chat messages | Room or JSON snapshots under `cacheDir` (kotlinx.serialization + okio atomic writes) | Show cache on cold start, silently refresh |
| Settings, style, terminal font size, quick commands | **DataStore 1.3.0** (Preferences or typed with kotlinx.serialization) | |
| Pairing / device tokens (`jd_…`) | DataStore + **`datastore-tink` `AeadSerializer`** (new in 1.3.0), Tink keyset wrapped by an Android Keystore master key | Replaces deprecated EncryptedSharedPreferences |
| Chat images, Lark media | Coil 3 `DiskCache` (`chat-images`, `lark-media`), immutable images keyed by `ref`, valid indefinitely | Settings › Storage reports / clears by directory |
| Web pages | WebView cache (`app_webview`), clear via `WebView.clearCache(true)`; login in cookies / WebStorage, cleared separately | |

### 5.3 Key / token encryption (✅)

- **`androidx.security:security-crypto` 1.1.0 (stable 2025-07-30) deprecates all APIs** (since 1.1.0-alpha07): "Deprecated all APIs in favour of existing platform APIs and direct use of Android Keystore."
  Do not use EncryptedSharedPreferences / EncryptedFile / MasterKey.
- New official path: **DataStore 1.3.0 (2026-10-21) adds `androidx.datastore:datastore-tink`**. `AeadSerializer` wraps the existing Serializer, with filename `associatedData` preventing ciphertext substitution.
  Tink supports JVM / Android only.
- **Tink 1.24.0** (2026-10-16, `com.google.crypto.tink:tink-android`), minSdk 23; `AndroidKeystore` class since 1.16 (recommended Keystore use); 1.18 fixes Keystore AEAD for inputs >128 KB on API ≤ 28.
- 💭 Pitfall: Keystore keys **do not migrate through automatic backup**, so restored encrypted data cannot decrypt on a new phone. Exclude encrypted pairing files from cloud backup / device transfer in `dataExtractionRules`,
  and re-pair after restoration (similar to iOS Keychain's device-only semantics).

Sources:
- Room: https://developer.android.com/jetpack/androidx/releases/room ; Room 3: https://developer.android.com/jetpack/androidx/releases/room3
- SQLDelight: https://github.com/sqldelight/sqldelight/releases
- DataStore: https://developer.android.com/jetpack/androidx/releases/datastore
- security-crypto: https://developer.android.com/jetpack/androidx/releases/security
- Tink: https://github.com/tink-crypto/tink-java/releases

## 6. UI components ✅

### 6.1 Theme system: Material 3 base + custom tokens (5 styles)

- Versions: Compose BOM 2026.09.00 (Compose 1.12.1, material3 1.4.0). Expressive components (SplitButton, ButtonGroup, FloatingToolbar, Flexible TopAppBar, list items, SearchBar…)
  progressively drop experimental annotations only in **material3 1.5.0-beta02** (2026-10-21); 1.4.0 requires `@OptIn(ExperimentalMaterial3ExpressiveApi::class)`.
  💭 Start with stable 1.4.0, switch after 1.5.0 stabilizes (likely 1–2 months at this pace), with limited Expressive opt-ins as needed.
- 💭 Structure: `MaterialTheme` (colors / typography / shapes) supplies system-component defaults only. Persona's `PersonaTheme` supplies CompositionLocals for
  `AppStyle` (glass / claude / clay / island / lark), tokens (five Radius levels, three Fill levels, Font roles, Space, Motion), and style-independent `StatusPalette`.
  Pages call wrappers such as `Modifier.surface(tint, interactive, shape)`, `themedButton(prominent)`, `themedPage()`, corresponding to iOS `Theme`. Style switching rebuilds the tree by style key, as on iOS.
- 💭 **One shared token source**: extract colors, radii, and contrast floors from `Theme.swift` / `StatusPalette.swift` into repository JSON or TOML,
  generating Swift / Kotlin constants to prevent drift. Validate contrast (≥ 3.5 / 4.5 / 3:1) in the generator.
- Android rendering per style:
  - **Clay / Island / Lark**: Compose has CSS-like **`Modifier.dropShadow(shape, Shadow(...))` / `innerShadow`** since 1.9 (upper-left highlight + lower-right colored shadow, large soft shadows),
    hairline `border(Dp.Hairline)`; fully reproducible.
  - **Liquid Glass / Claude (glass surfaces)**: Android has no system Liquid Glass. Options:
    - **Haze 2.0.1** (2026-09-29, Chris Banes, KMP): `haze` + `haze-blur` (**blur on every Android version**: RenderEffect on 31+, RenderScript below),
      experimental `haze-glass`, `HazePerformanceMode` tiers, progressive blur, `haze-blur-material3` presets. Good for frosted top / bottom bars and input pills.
    - **Backdrop (Kyant0/AndroidLiquidGlass) 2.0.1** (Apache-2.0, `io.github.kyant0:backdrop`, about 4.4k stars, KMP): lens refraction / vibrancy / blur on a background copy,
      closest to iOS 26's liquid look. No ready-made components (catalog examples include LiquidButton / Toggle / Slider / BottomTabs). Refraction uses AGSL, **requires API 33+** (community sources, not verified in official docs).
  - 💭 Android default should not be Liquid Glass (not Android users' / system's design language, high GPU cost). Default to a solid style such as Claude / Lark.
    Optional Liquid Glass: Backdrop for small areas (buttons, tab / key bars), Haze frosting for large ones; below API 33, translucent + blur fallback. Avoid glass-on-glass or full-screen blur over long scrolling content; measure frame by frame on midrange devices.

### 6.2 Chat list

- 💭 Based on public Compose behavior, **`LazyColumn(reverseLayout = true)` natively implements iOS-style inverted lists**: index 0 latest at bottom, precise `scrollToItem(0)`.
  Older content appends at the list tail without shifting the viewport. No iOS double `scaleEffect` inversion, misplaced safe areas, or full-page edge blur.
- Compose `scrollToItem(index, offset)` **positions using actual target measurements, not estimated height**, avoiding SwiftUI LazyVStack jumps into unrendered blank regions.
  Only scrollbar position is estimated. Keys retain position: adding new rows on the index-0 side while reading history keeps the same visible row.
  The iOS issue where row IDs change as turns advance (answers fold into work rows, messages merge into `wt-`) still breaks key anchoring; frozen snapshots (`ChatView.Frozen`) remain needed.
- Follow bottom: on new content with `followsEnd`, `scrollToItem(0)`; disable on user dragging (`isScrollInProgress` + drag interaction, not programmatic scrolling). Use `LazyListState.layoutInfo` to detect latest-end position.
- Independent step rows with stable `key` + `contentType`; `LazyLayoutCacheWindow` (stable in 1.13) controls prefetch. A tail sentinel loads two more turns.
- **No Paging 3** (latest 3.5.3, 2026-10-12): Persona pages by turns, collapsing across page boundaries and merging pushes into one store, unlike PagingSource's model.
  Paging 3.5 `asItemSnapshotListFlow` / `Pager.append/prepend` suits ordinary lists such as Git history; chats use custom `ChatStore`.
- Text selection: Compose `SelectionContainer` selects across Text nodes (1.12 auto-scrolls drag selection), so **inline Markdown message selection may work on Android** (iOS SwiftUI Text limitations require a plain-text sheet).
  Selection across lazy rows remains unreliable; retain long-press Select's plain-text fallback page.

### 6.3 Markdown

| Library | Latest | Decision |
| --- | --- | --- |
| **mikepenz/multiplatform-markdown-renderer** | **0.46.0** (2026-10-13; 0.43.0 2026-06-22) | **Recommended**. KMP / Compose, built-in GFM (tables, task lists, strikethrough, autolinks, GitHub alerts), optional `-code` highlighting via Highlights, `-coil3` images. `MarkdownComponents` replaces any node renderer (tool cards, `<persona-context>` cards, file-path labels); async `rememberMarkdownState`, ViewModel preparse `parseMarkdownFlow`, **streaming incremental parsing (`rememberStreamingMarkdownState`, tail-only reparsing, ideal for assistant streaming)**, long-document `LazyMarkdownSuccess` |
| compose-richtext (halilozercan) | 1.0.0-alpha05 (2026-06) | Not recommended: always prerelease, 7–11 months between updates |
| Markwon | 4.x (View / Spannable; no recent release visible on page) | Not recommended: TextView, not Compose |

- 💭 Custom fallback: if mikepenz typography lacks detail (iOS uses MarkdownUI custom `.card` themes), build a renderer over JetBrains `org.jetbrains:markdown`, its underlying parser.
- 💭 Cache parsed results outside composition by message ID (iOS `RowMemo` / `TextMemo` equivalent), warming in the background after message load.

### 6.4 Syntax highlighting

- iOS uses Persona's lightweight rules (comments / strings / numbers / common keywords, `LarkSyntax.highlight`), with no library.
- 💭 Port those rules to Android (tens of lines, identical output for shared golden tests). For richer results, **Highlights 1.1.0** (`dev.snipme:highlights`, KMP, 17 languages, 6 themes, incremental / async)
  **does not support JSON / YAML / diff / SQL / HTML**, which still need lightweight rules.

### 6.5 Images: Coil 3

- **Coil 3.6.3** (2026-09-18, **fixes AGP 9.4.0 + R8 build failures; AGP 9.4 requires ≥ 3.6.3**). Since 3.6.0: Kotlin 2.4.10 / Compose 1.12 / compileSdk 37; minSdk 23 since 3.5.0.
- `coil-network-okhttp` shares the App's OkHttpClient and Bearer token. Use Coil `DiskCache` (official guidance says not to enable OkHttp Cache simultaneously).
- 💭 On-demand Persona images (`ImagePayload.ref`) need custom `Fetcher` / `Keyer`, permanent ref cache, size placeholders preventing jumps, fetched only on scroll into view.

### 6.6 Full-screen image viewing

- **Telephoto 0.19.0** (2026-04-02): `ZoomableImage` (double-tap / pinch zoom, large-image subsampling), `me.saket.telephoto:zoomable-image-coil3` (since 0.14.0),
  `Modifier.zoomable()` for arbitrary content, flick-to-dismiss (available in 0.17.0, officially "not released yet"). Wrap in `HorizontalPager` for all images in a chat.
- Implement editing (rectangle selection / brush / colors / Undo) with custom Canvas, as on iOS.

### 6.7 Monospace text / diffs (horizontal and vertical scrolling)

- 💭 Compose can do what iOS cannot: a `LazyColumn` inside `Box(Modifier.horizontalScroll(h))`, **fixed to the measured longest-line width** (`TextMeasurer`), one `Text(softWrap = false)` per row.
  Vertical lazy rendering, whole-content horizontal scrolling; large files / diffs compose only visible rows. Keep line numbers outside the horizontal region. No iOS `MonoTextView` (UIScrollView wrapping UITextView) needed.

### 6.8 Terminal emulation (✅ licenses / libraries + 💭 approach)

| Option | Status | Decision |
| --- | --- | --- |
| **Custom grid + Compose Canvas (recommended)** | Port iOS `TerminalGrid` (herdr observe VT subset: CUP, ED, SGR, DECTCEM, 2026, OSC 8) into pure Kotlin `core:terminal`, sharing recorded iOS real frames (`ios/PersonaTests/Fixtures/terminal-observe.jsonl`) as fixtures | Same reasoning as iOS: input is an already-emulated herdr grid; herdr did the hard part. Ready-made views cannot display a 172-column grid wrapped at 45 columns |
| ConnectBot **termlib** | 0.3.14, **Apache-2.0** (libvterm MIT, JNI), Compose components, 256 / true color, wide / combining characters, magnifier selection, zoom, complex text shaping; still updated 2026-10 | Consider if raw program bytes from a real PTY become available; emulation unnecessary now, plus native library cost |
| Termux `terminal-emulator` / `terminal-view` | termux-app overall **GPLv3**; LICENSE exceptions say these libraries contain jackpal Apache-2.0 code, **not clearly that the entire libraries are Apache** | Not recommended: license ambiguity, View-based; Termux `TYPE_NULL` raw-key input breaks Chinese IME composition |

- 💭 Render per grid cell, wide characters spanning two; box / block characters as lines / rectangles. Do not rely on font widths; Chinese and box drawing always align, as on iOS. Cache `TextMeasurer` by run; draw only through the last nonempty row / cursor.
- 💭 Direct-input mode: custom `InputConnection` using Compose `PlatformTextInputModifierNode` or a custom AndroidView View's `onCreateInputConnection`.
  Handle `setComposingText` / `finishComposingText` / `commitText` correctly (iOS marked-text equivalent); use `TYPE_CLASS_TEXT | TYPE_TEXT_FLAG_NO_SUGGESTIONS`, not `TYPE_NULL`.
  Implement sticky / locked / held modifiers, wheel forwarding, and two-finger Agent switching in `pointerInput`, not `combinedClickable` (long-press behavior changed in Compose 1.11).

### 6.9 Link sheets and WebView

- **Sheet**: M3 `ModalBottomSheet` has a separate window + scrim and **blocks underlying interaction**. 💭 Draw in the same window: `AnchoredDraggable` with full / 60% / hidden anchors over the chat,
  intercepting only the sheet area at 60%, leaving the upper chat scrollable / editable; or M3 `BottomSheetScaffold` (standard interactive-background sheet), peek at 60%.
  WebView does not implement Compose nested scrolling (💭 test required). Dismiss dragging uses the top handle / title, or JS reports `scrollY == 0` and hands downward drags to the sheet.
- **WebView acceleration (✅ androidx.webkit 1.18.0, 2026-10-21, minSdk 24)**:
  - `WebViewCompat.startUpWebView(context, WebViewStartUpConfig, receiver)` (stable since 1.16) starts asynchronously while idle, avoiding first-open UI blocking.
  - `WebViewCompat.prerenderUrlAsync(...)` (no OptIn since 1.15, UI thread only) prerenders the most likely link, showing instantly on tap.
  - `Profile.prefetchUrlAsync` (experimental), `Profile.preconnect` / `enqueuePreconnect` (1.17, preconnect without WebView startup), `Profile.warmUpRendererProcess` (experimental).
  - `NavigationListener` (stable 1.16, `WebViewFeature.NAVIGATION_LISTENER`) measures FCP / LCP for link-open speed; back-forward cache settings had breaking changes in 1.16.
  - 💭 Pool like iOS: at most 6 WebViews, LRU, shared default Profile cache / cookies. **No WebView creation during scrolling**; wait until stopped, then create next frame.
  - Add AI edit / Use as context to selected-text menus by overriding `startActionMode` and adding `ActionMode` actions; get selection through `evaluateJavascript("getSelection().toString()")`.
- **Custom Tabs tradeoff (✅)**: Partial Custom Tabs (Chrome 107+) support bottom-half opening, **interactive underlying App**, and handle drag to full-screen, minimum height 50%.
  But no JS injection / selected-menu changes, so AI edit / Use as context and preload-pool control are unavailable. 💭 Ordinary links use WebView; **OAuth forbidding embedded WebViews, such as Google login, must use Custom Tabs**.
  Also provide Open in browser.

### 6.10 Charts (machine monitoring)

- **Vico 3.3.1** (2026-08-28; 3.4.0-next.1 2026-10-28, will raise minSdk to 24): Compose / M3, multiple series, touch markers; **release notes do not mention gaps / nulls**.
  💭 Like iOS, make each continuous segment a same-color series so gaps stay disconnected. Or custom Compose Canvas (three lines, press-to-inspect, clipping), with controllable iOS gap / clip / tick-color rules. 💭 Prefer custom drawing.

### 6.11 QR scanning (pairing)

- **Google code scanner** `com.google.android.gms:play-services-code-scanner:16.1.0`: **no camera permission** (Play services scan and return results), minSdk 23, fixed UI, Google Play services required (module downloaded on first use, or at installation via manifest metadata). 💭 First choice: one-time pairing needs only system UI.
- Custom UI / no Play services: **CameraX 1.6.2** (2026-08-26; 1.7.0-rc01 2026-10-21), `camera-compose` `CameraXViewfinder` (pinch zoom / tap focus since 1.7), `camera-mlkit-vision` `MlKitAnalyzer`, bundled ML Kit barcode.
  CameraX 1.7 Compose requires AGP ≥ 9.2.

Sources:
- Material3: https://developer.android.com/jetpack/androidx/releases/compose-material3
- Compose shadows: https://developer.android.com/develop/ui/compose/graphics/draw/shadows
- Haze: https://chrisbanes.github.io/haze/latest/ ; changelog https://chrisbanes.github.io/haze/latest/changelog/
- Backdrop: https://github.com/Kyant0/AndroidLiquidGlass , https://kyant.gitbook.io/backdrop , https://klibs.io/project/Kyant0/AndroidLiquidGlass
- Paging: https://developer.android.com/jetpack/androidx/releases/paging
- Markdown: https://github.com/mikepenz/multiplatform-markdown-renderer (releases); https://github.com/halilozercan/compose-richtext/releases ; https://github.com/noties/Markwon
- Highlights: https://github.com/SnipMeDev/Highlights
- Coil: https://coil-kt.github.io/coil/changelog/
- Telephoto: https://github.com/saket/telephoto/releases
- termlib: https://github.com/connectbot/termlib , http://termlib.connectbot.org/ ; Termux license: https://github.com/termux/termux-app/blob/master/LICENSE.md
- WebView: https://developer.android.com/jetpack/androidx/releases/webkit ; Partial Custom Tabs: https://developer.chrome.com/docs/android/custom-tabs/guide-partial-custom-tabs
- Vico: https://github.com/patrykandpatrick/vico/releases
- Scanning: https://developers.google.com/ml-kit/vision/barcode-scanning/code-scanner ; CameraX: https://developer.android.com/jetpack/androidx/releases/camera

## 7. System capabilities ✅

### 7.1 Push: FCM

- 💭 Requires Google Play services and a Firebase project. Hub (Go) sends through FCM HTTP v1 (`firebase.google.com/go` Admin SDK or REST + service account).
  Service-account JSON is a secret; keep beside hub `hub.json`, mode 0600, never commit. Phones without Play services (China-market / de-Googled) cannot receive it. UnifiedPush with self-hosted ntfy is an alternative; 💭 defer initially.
- ✅ **High priority** wakes sleeping devices and allows brief processing (only enough to render a notification immediately). Further work uses **expedited WorkManager**, with a brief exemption after high-priority messages.
  **High-priority messages without a user-visible notification are downgraded to normal priority**, based on each installation's last 7 days; disabled notifications also cause downgrade.
  Play services can proxy some high-priority notifications (Android Q+, Beta, HTTP v1 only).
- 💭 Persona: interactions / Needs you use high priority and immediate notification. Unread / Live Update refresh also correspond to visible changes and should not downgrade.
  Pure `revs` sync uses normal priority or no push.
- iOS notification counterparts:
  - Channels: Needs you (high importance, interrupting), Agent finished, Fleet (Live Update), phone-tool confirmation, independently adjustable in system settings.
  - Buttons: `NotificationCompat.Action`. **Allow / Approve require unlock** via `Notification.Action.Builder.setAuthenticationRequired(true)` (✅ API 31+, system authenticates before sending PendingIntent), like iOS `.authenticationRequired`; Deny does not.
  - Direct feedback (plan rejection): `RemoteInput`. Buttons resolve via hub in background; failures post local reason notifications, as on iOS.
  - Runtime `POST_NOTIFICATIONS` from Android 13.

### 7.2 Lock-screen Live Activities / Dynamic Island → Live Updates (rules in "Live Updates" at this section's end; mapping here)

- Fleet summary → one promoted ongoing notification. ✅ **Android 17 (API 37) adds `Notification.MetricStyle`**: up to 3 metrics (label + value + optional unit), `setCriticalMetric(index)` for the critical metric, up to 3 buttons,
  promotable to Live Update (once promoted, contentTitle unnecessary; values are primary content). **Exactly fits Needs you / Working / Unread counts**, with Needs you critical.
  Status chip uses `setShortCriticalText("⚙3")`, ≤ 6 characters. Android 16 uses BigText Live Update; older versions ordinary persistent notification.
  💭 NotificationCompat MetricStyle wrapping unverified; use platform API initially (`SDK_INT >= 37`).
- No Android equivalent of iOS push-to-start / push-to-update: FCM data wakes the App to call local `notify()`. Hub's at-most-once-per-device-per-minute throttle remains needed.
- Risks: official usage discourages other-person-triggered activities and alerts; OEMs may restrict; users can disable with one tap. Wear OS 7 bridges Live Updates to Watch.

### 7.3 Background connection: no permanent foreground service (✅ rules + 💭 design)

- ✅ target 35+ `dataSync` / `mediaProcessing` foreground services have **6 background hours total per 24 hours**. Failing `stopSelf()` within seconds of `onTimeout` crashes with `RemoteServiceException`.
  Restart after quota exhaustion throws `ForegroundServiceStartNotAllowedException`; Android 12+ generally prevents background foreground-service starts.
- 💭 Follow iOS: **WebSocket in foreground, disconnect in background**, wake with FCM. Offline `phone_*` calls return Phone offline from hub,
  or high-priority FCM wakes the App for an expedited WorkManager HTTP execution without waiting for WebSocket. 💭 More reliable than iOS silent pushes; a possible Android enhancement.
- **WorkManager 2.12.0** (2026-09-23, minSdk 24; use 2.11.2 for 23): outbox retry, constrained background-sync fallback, post-FCM expedited work; experimental Work Metrics in 2.12.

### 7.4 Widgets: Glance

- **Glance 1.2.0** (2026-08-26): generated previews, M3 (`glance-material3`), LazyColumn; 1.3.0-alpha02 remote compose requires AGP ≥ 9.2.
- ✅ Android 17 target 37: widget bitmap memory cap. If all size buckets total over `6 × width × height` bytes, `updateAppWidget` throws.
- 💭 Fleet widget (Needs you / Working / Unread + Approve first), replacing iOS lock-screen / Smart Stack entry points.

### 7.5 Watch: Wear OS

- ✅ **Compose for Wear OS 1.7.1** (2026-10-07; 1.8.0-alpha02). **Wear Compose Material 3** recommended (M3 Expressive); `TransformingLazyColumn`, Nav3 `SwipeDismissableSceneStrategy` (1.6).
- ✅ **Wear OS 7** (announced 2026-05 I/O, Android 17-based): **Wear Widgets** (Glance + RemoteCompose, 2×1 / 2×2 cards) replace Tiles (Tiles 1.6 / ProtoLayout 1.4 remain supported temporarily; Widgets API supports Wear OS 4+).
  **Live Updates replace Ongoing Activities** on Watch. Play requires Wear target ≥ 35.
- 💭 Standalone Wear App directly connects to hub with `kind: watch`, `answer` token like iOS Watch. Phone sends pairing through **Data Layer** (`MessageClient` / `DataClient`).
  UI: Needs you list → interaction details (question / plan / permission) → session details; one Fleet-count Wear Widget. ⚠ Watch uses phone proxy or Wi-Fi; LAN-hub reachability needs testing.
  AGP 9 removed embedded Wear Apps (`wearApp`); package and release separately.

### 7.6 Siri / Shortcuts alternatives

- ✅ **Google Assistant shut down for phone users on 2026-09-28**, replaced by Gemini. Assistant-dependent **App Actions (built-in intents) are largely obsolete**; do not invest.
- ✅ **AppFunctions** (`androidx.appfunctions` **1.0.0-beta01**, 2026-10-21, Android 16 platform API): expose App functions to on-device AI Agents, like local MCP.
  Callers require `EXECUTE_APP_FUNCTIONS`; Gemini integration was trusted-tester private testing as of 2026-05. 💭 Ask Claude / Persona status / Approve first can become AppFunctions, a future investment, not a dependable current entry.
- 💭 Available now: **dynamic / pinned shortcuts** (`ShortcutManagerCompat`, long-press icon Ask Claude / Approve first), **Quick Settings tiles** (`TileService`, state and one-tap approval),
  **Glance widgets**, notification buttons, **share target** (text / image / link to Persona context), explicit Intents for Tasker / automation Apps (roughly Shortcuts).

### 7.7 Biometrics

- ✅ Stable `androidx.biometric` remains **1.1.0 (2021)**. New APIs (`registerForAuthenticationResult`, `AuthenticationRequest`, `biometric-compose` `rememberAuthenticationLauncher`, up to 4 custom fallbacks from Android 36.1) are in **1.4.0-alpha07** (2026-04-22), still renaming between alphas.
- 💭 Persona needs little: system unlock requirement for notification Allow; optional BiometricPrompt for high-risk in-App Allow all (stable 1.1.0 or alpha Compose).

Sources:
- FCM priority: https://firebase.google.com/docs/cloud-messaging/android-message-priority
- Foreground-service timeouts: https://developer.android.com/develop/background-work/services/fgs/timeout
- WorkManager: https://developer.android.com/jetpack/androidx/releases/work
- Glance: https://developer.android.com/jetpack/androidx/releases/glance
- Wear Compose: https://developer.android.com/jetpack/androidx/releases/wear-compose ; Wear OS 7: https://android-developers.googleblog.com/2026/05/whats-new-wear-os-7.html , https://www.androidauthority.com/google-wear-os-7-wear-widgets-early-partners-availability-features-3670112/
- AppFunctions: https://developer.android.com/jetpack/androidx/releases/appfunctions , https://developer.android.com/ai/appfunctions , https://9to5google.com/2026/02/25/android-appfunctions-gemini/
- Assistant shutdown: https://en.wikipedia.org/wiki/Google_Assistant
- Biometrics: https://developer.android.com/jetpack/androidx/releases/biometric
- MetricStyle: https://developer.android.com/develop/ui/views/notifications/metric-style , https://developer.android.com/reference/android/app/Notification.MetricStyle
- Unlock-required notification actions: https://developer.android.com/reference/android/app/Notification.Action.Builder
- OkHttp ping/pong requests: https://github.com/square/okhttp/issues/7370 , https://github.com/square/okhttp/issues/3197

### Live Updates (Android 16+, replacing lock-screen Live Activities / Dynamic Island) ✅

- Requirements: Standard / BigText / Call / **ProgressStyle** / **MetricStyle**; manifest `POST_PROMOTED_NOTIFICATIONS` (install-time permission);
  `setRequestPromotedOngoing(true)`, ongoing, contentTitle required. **No** custom RemoteViews, group summaries, colorized notifications, or IMPORTANCE_MIN channels.
  OEMs may impose more. Check user opt-out with `NotificationManager.canPostPromotedNotifications()`; settings via `Settings.ACTION_MANAGE_APP_PROMOTED_NOTIFICATIONS`.
- Status chip: small icon + `setShortCriticalText` (≤ 6 characters safest, max 96dp) or `when` countdown / chronometer.
- Intended uses: ongoing, user-initiated, time-sensitive. Explicitly **unsuitable**: ads, chat messages, alerts, calendar events, activities triggered by others, ambient information.
  Persona Fleet (working Agents / Needs you) is a user-initiated long task, 💭 marginally suitable; Needs you is more like an alert and should use normal high-priority notifications.
- **No iOS-like push-to-update channel**: high-priority FCM data wakes the App, which rebuilds in `onMessageReceived` and calls `notify()` with the same ID.
- Also bridged to some Wear OS devices.

Source: https://developer.android.com/develop/ui/views/notifications/live-update

## 8. Phone-tool API counterparts ✅

| iOS tool | Android API | Permissions / limitations | Decision |
| --- | --- | --- | --- |
| `phone_contacts_search` | `ContactsContract` (fuzzy name / company / title / email / number search, `Data` + `PhoneLookup`) | Runtime `READ_CONTACTS`. ✅ Android 17 adds system **Contact Picker**; target 37 removes columns such as `ACCOUNT_NAME` from `Data` (join RawContacts); unpermitted queries use strict mode. ✅ **Play policy**: target 37+ READ_CONTACTS needs a Play Console explanation of why Contact Picker is insufficient (AI assistants are listed as valid uses), mandatory 2027-01-27 | Feasible, full permission needed; picker cannot keyword-search |
| `phone_calendar_events` / `create_event` | `CalendarContract` (`Instances` time-range query, insert `Events`) | `READ_CALENDAR` / `WRITE_CALENDAR`; choose calendar with access ≥ CONTRIBUTOR (synced-account calendars sync to cloud). `ACTION_INSERT` delegates to calendar App without permission but switches away | Feasible, broadly equivalent to iOS |
| `phone_reminders_*` | **No Android system reminders / to-do provider**; `CalendarContract.Reminders` means event alarms, not tasks | ✅ DAVx⁵: write to a task App's own ContentProvider (OpenTasks `TaskContract`, tasks.org, jtx Board); OpenTasks largely inactive | 💭 Use **Google Tasks API via hub** (existing Google OAuth, add `tasks` scope), same tool names, Android routes to hub; optional installed TaskContract providers later |
| `phone_location_current` | Fused Location Provider `getCurrentLocation(Priority.PRIORITY_HIGH_ACCURACY, token)`; reverse geocode `Geocoder.getFromLocation(…, listener)` (async API 33+) | Foreground `ACCESS_FINE_LOCATION`; no background location, like iOS | Feasible |
| `phone_health_summary` | **Health Connect**: `StepsRecord`, `DistanceRecord`, `ActiveCaloriesBurnedRecord`, `HeartRateRecord`, `RestingHeartRateRecord`, `SleepSessionRecord` (stages), `WeightRecord`; all have aggregates (`COUNT_TOTAL`, `DISTANCE_TOTAL`, `BPM_AVG/MIN/MAX`, `SLEEP_DURATION_TOTAL`…), daily `aggregateGroupByPeriod` | Per-type `READ_*`; **grants queryable** (`getGrantedPermissions()`, unlike iOS hidden read grants). Normally only about 30 days before first grant; older needs `READ_HEALTH_DATA_HISTORY`, background needs `READ_HEALTH_DATA_IN_BACKGROUND`. Built-in Android 14+, install Health Connect App on 13 or below; Play health-data declaration | Feasible, one-to-one iOS data, more transparent |
| `phone_photos_recent` / `phone_photo_get` | `MediaStore` recent metadata (time, size, screenshot, duration, coordinates) | `READ_MEDIA_IMAGES` / `READ_MEDIA_VIDEO`; Android 14+ partial grants (`READ_MEDIA_VISUAL_USER_SELECTED`, selected results only, like iOS limited). ✅ **Play policy**: broad access only if core functionality requires it; otherwise Photo Picker mandatory since 2025-05-28, exempt outside Play. Location metadata also needs `ACCESS_MEDIA_LOCATION` | 💭 Personal / non-Play use feasible; on Play, recent-photos tool likely fails review, replace with user Photo Picker selection |
| Input-bar image sending | **Photo Picker** (`PickMultipleVisualMedia(4)`, Android 11+ system component, Play-services fallback on older devices) | No permission | Use it |
| `phone_device_info` | `Build`, `BatteryManager`, `PowerManager` (power saving, `currentThermalStatus`), `StorageStatsManager` / `StatFs`, `ConnectivityManager`, time zone / language | None | Feasible |
| `phone_clipboard_get` / `set` | `ClipboardManager` | ✅ Android 10+: **only focused foreground Apps or default IME can read**. Android 12+ reading content copied from another App shows Pasted from clipboard toast (not iOS permission approval). Sensitive writes can use `EXTRA_IS_SENSITIVE` | 💭 Foreground only; background hub calls return Please open Persona |
| `phone_notify` | `NotificationManager`; delays via WorkManager (Doze may defer) or `AlarmManager.setWindow`; exact alarms need `SCHEDULE_EXACT_ALARM` (not granted by default from Android 14) | `POST_NOTIFICATIONS` | Feasible, timing not guaranteed |

- 💭 **Android-only enhancements unavailable on iOS**, subject to privacy evaluation: `NotificationListenerService` (system user grant reads all App notifications, including WeChat, into Inbox / AI context),
  `UsageStatsManager` (App usage duration), call logs / SMS (`READ_CALL_LOG` / `READ_SMS`, strict Play restrictions; Android 17 delays OTP SMS by 3 hours). All feasible outside Play.
- Writes follow iOS: show an in-App confirmation card before execution; denial returns `The user declined on the phone.`.

Sources:
- Android 17 changes: https://developer.android.com/about/versions/17/behavior-changes-17
- Play policy (photos / contacts / local network): https://support.google.com/googleplay/android-developer/answer/14115180
- Task providers: https://github.com/bitfireAT/davx5-ose/discussions/2740 , https://manual.davx5.com/tasks_notes.html , https://github.com/dmfs/opentasks
- Health Connect types: https://developer.android.com/health-and-fitness/guides/health-connect/plan/data-types
- Photo Picker: https://developer.android.com/training/data-storage/shared/photopicker
- Clipboard: https://developer.android.com/privacy-and-security/risks/secure-clipboard-handling , https://developer.android.com/develop/ui/views/touch-and-input/copy-paste

## 9. Testing and quality ✅

### 9.1 Unit / UI tests

- 💭 Pure Kotlin core (model / protocol / chat / terminal): JUnit + kotlinx-coroutines-test + Turbine, **shared golden fixtures across platforms** (same JSON inputs → expected rows),
  terminal replay from recorded real iOS herdr frames; hub Go goldens remain authoritative for transcripts.
- Compose UI: `createAndroidComposeRule` + semantic assertions. NiA uses no mocking library, with Fake / Test repositories replaced through Hilt test APIs.
- Mock hub: reuse `scripts/mock/mock_hub.py` (existing data, `MOCK_STREAM` / `MOCK_TERMINAL_*`), reached from emulator through its fixed host alias address (allow plaintext in network security config).

### 9.2 Screenshot testing (✅ versions 2026-10-30)

| Tool | Latest | Decision |
| --- | --- | --- |
| **Roborazzi** | **1.77.0** (2026-10-22), nearly weekly releases | **Recommended**. Robolectric JVM screenshots of arbitrary Compose content, including post-interaction states; automatic Compose Preview tests (`ComposePreviewTester`, about 1.6× faster since 1.75), UI-tree dumps; used by NiA |
| Paparazzi | 2.0.0-alpha05.1 (2026-09-28; stable still 1.3.5) | Alternative: layoutlib, no Robolectric; 2.0 remains alpha, AGP 9.0 migration only in alpha05.1 |
| Compose Preview Screenshot Testing (`com.android.compose.screenshot`) | 0.0.1-alpha16 | ✅ **Standalone plugin officially no longer maintained**, replaced by AGP test suites in AGP 9.5+. AGP 9.5 currently still alpha; 💭 defer |

- 💭 Matrix: 5 styles × light / dark × key pages (`docs/operations/testing.md` UI checklist). Robolectric regressions, but **final acceptance still uses real emulator captures**; Robolectric blur / RenderEffect / WebView cannot be trusted.
  Record references in a fixed environment (NiA uses CI Linux); Mac / Linux font rendering differs.

### 9.3 Automated emulator screenshots (iOS DEBUG-launch-argument equivalent)

- 💭 Debug-only Activity / Intent extras: `adb shell am start -n <pkg>/.debug.DebugEntryActivity --es openSession m/s --ez openWork true`,
  or deep link `adb shell am start -W -a android.intent.action.VIEW -d "persona://debug/open?session=m/s" <pkg>`. **Entry only in `src/debug/`**, so release sources contain neither classes nor strings (10.3).
- Pair without QR using `--es pairURL "persona://pair?url=…&token=mock"`.
- Screenshot: `adb exec-out screencap -p > shot.png`; appearance `adb shell cmd uimode night yes|no`; fixed status time / battery through SystemUI demo
  (`adb shell settings put global sysui_demo_allowed 1`, then `am broadcast -a com.android.systemui.demo -e command clock -e hhmm 1430`, etc.).
- Parallel Agents each use their own AVD (`emulator -avd <name> -port <even port>`, distinguished with `adb -s emulator-<port>`), matching iOS separate simulators.

### 9.4 Performance

- ✅ **androidx.benchmark 1.5.0** (2026-09-09; 1.6.0-alpha01): Macrobenchmark (`MacrobenchmarkScope` extends `UiAutomatorTestScope` since 1.5), Baseline Profile Gradle plugin (AGP 9 no longer needs `newDsl=false` since 1.5), Perfetto v56.
- 💭 Metrics: cold start to cached session list, large-chat opening (`turns=2`) to first screen, fast chat-scroll dropped frames (`FrameTimingMetric`), link opening to FCP (`NavigationListener`).
  User convention: quoted numbers must be measured, ≥ 10 samples with min / p50 / p90 / max / mean. Production JankStats can record dropped frames.

Sources:
- Roborazzi: https://github.com/takahirom/roborazzi/releases
- Paparazzi: https://github.com/cashapp/paparazzi/releases
- Compose Preview Screenshot Testing: https://developer.android.com/studio/preview/compose-screenshot-testing
- Benchmark: https://developer.android.com/jetpack/androidx/releases/benchmark
- NiA: https://github.com/android/nowinandroid

## 10. Distribution, signing, and development environment ✅

### 10.1 Distribution (iOS TestFlight internal-testing counterparts)

| Method | Status (✅) | Suitable for |
| --- | --- | --- |
| **Direct adb install** (USB / wireless debugging) | **Exempt from developer verification** (official FAQ: no registration or advanced-flow wait for ADB installs) | Development / own phone. 💭 A `scripts/android-deploy.sh` for build + `adb install -r` + launch, like iOS `sim.sh` |
| **Firebase App Distribution** | Gradle `com.google.firebase.appdistribution` **5.3.1**, `./gradlew assembleRelease appDistributionUploadRelease`; email invites, tester App installation, APK / AAB; builds kept 150 days, invitations accepted within 30 days; service-account / `GOOGLE_APPLICATION_CREDENTIALS` auth | 💭 **Closest to TestFlight internal testing**, sharing the Firebase project with FCM |
| Google Play internal testing | Up to 100 testers; third-party reports say available within minutes without full review (not official). Play Console account, $25 one-time fee, permission declarations, target ≥ 36. Personal accounts registered after 2023-11-13 need 12 testers × 14 days closed testing before production (internal testing does not count) | Later if Play-managed updates desired; note contacts, photos, health, local-network policies (sections 4, 8) |

- ✅ **Android developer verification (affecting direct APK sharing)**: effective 2026-09-30 on certified devices in Brazil, Indonesia, Singapore, Thailand; **all certified devices globally in 2027**.
  Unverified APKs then require advanced flow (developer-option switch, restart, **24-hour wait**, biometric confirmation). **Free limited-distribution accounts** since 2026-08 (students / hobbyists, up to 20 devices, no documents / fees),
  or full Android Developer Console / Play Console registration (99% of Play Apps auto-registered). adb unaffected.
  💭 Register Persona package + signing key for limited distribution before 2027, enough for personal use, to keep Firebase installs smooth on new phones.
- 💭 Build numbers: `versionCode` max 2100000000 cannot hold iOS 12-digit `yyyymmddHHMM`. Use 10-digit `yyyymmddHH` (under 2.1e9 through 2099) or Git commit count; `versionName` may retain iOS format.

### 10.2 Signing

- 💭 Release upload key outside the repository (e.g. `.jks` under `~/.config/persona/`); path / password in `config.json` or macOS Keychain, read by Gradle properties / environment. Personal values remain configurable, like iOS.
  **Losing the key prevents upgrades of installed Apps**, requiring reinstall, so back it up. Use Play App Signing if on Play. Developer verification registers package and signing key.

### 10.3 R8 and keeping debug-switch strings out of Release

- ✅ AGP 9 R8: strict keep rules default, only `proguard-android-optimize.txt`. **New `optimization { enable = true }` DSL since AGP 9.3** enables code optimization + optimized resource shrinking together, without default rule files.
  Custom rules in `src/<variant>/keepRules/*.keep`; use at least 9.3.3 for the DSL, fixing wrong variant `isMinifyEnabled`. `./gradlew :app:analyzeReleaseR8Config` is a fast configuration-only analysis.
- 💭 Debug switches, matching iOS DEBUG-only arguments / Release string checks:
  - All direct-page / mock / probe parsing and entry Activities (`openSession`, `showFoldProbe`, `simulateOffline`…) **only in `src/debug/`**; release has a no-op `DebugHooks` implementation.
    More certain than relying on R8 dead-code deletion of `if (BuildConfig.DEBUG)`. `ui-tooling` / `ui-test-manifest` use only `debugImplementation`.
  - Release script unpacks APK and scans dex / resources with `dexdump` / `apkanalyzer dex` and `aapt2 dump strings`, failing on debug names (like `check_bundle_strings.py`).
  - iOS hit false positives when switches matched Swift identifiers. Kotlin **enum names remain strings in dex** (`name()` returns them), unremovable by R8 obfuscation. Use collision-resistant prefixes such as `debug.open…`.
  - iOS `-ui.style` is also accepted in Release; Android equivalent `am start --es ui.style clay` belongs in main sources.

### 10.4 Local development (macOS Apple Silicon, coordinator-verified facts)

- ✅ **Current stable AGP 9.5.0 (2026-10)**, 9.6 preview, requires Gradle ≥ 9.6.0 / JDK 17, supports API 37. Studio compatibility:
  **Rabbit 2 (2026.2.2) supports AGP 9.5**; Rabbit 1 / Quail 4 up to 9.4; **Panda (2025.3.x) only 9.0–9.2** (Panda 4 = 9.2). API 37.0 needs at least Panda 3 Patch 1 + AGP 9.1.1.
  **Compose 1.12, CameraX 1.7, Glance 1.3 require AGP ≥ 9.2**. 💭 Upgrade local Studio 2025.3 to Rabbit 2; CLI builds are unaffected by IDE version.
- JDK: AGP 9 needs 17 minimum; use bundled JBR 21 (`/Applications/Android Studio.app/Contents/jbr/Contents/Home`) or Homebrew openjdk@17 for `JAVA_HOME`.
  💭 Pin via Gradle daemon JVM criteria (`gradle/gradle-daemon-jvm.properties`) to avoid different Agent shell environments.
- SDK: local android-36.1 / 37.0 / 37.1 and build-tools 37.0.0. AGP defaults to 36.0.0; 💭 explicitly `buildToolsVersion = "37.0.0"` or let Gradle download 36.0.0.
- Emulator: arm64-v8a Hypervisor.Framework acceleration, local API 35 / 37.x. Headless scripting:
  - ✅ **New Android CLI (`android`, for AI Agents)**: `android emulator create/list/start/stop`; `android emulator start --headless <device>` **blocks until boot completes**.
    `android screen capture` / `screen resolve` (hierarchy), `android input tap|text|key|swipe|scroll`, **`android preview render` (Compose Preview images without a device)**,
    `android analyze` (APK size, R8 metrics, keep-rule warnings), `android sdk install`, `android init` (installs `android-cli` skill for Agents, source github.com/android/skills). Disable telemetry with `--no-metrics`.
    Official emulator CLI docs now deprecate old `emulator` in favor of this. 💭 Add to `AGENTS.md` / `CLAUDE.md` for consistent Agent screenshot verification.
  - Old commands still work: `emulator @<avd> -no-window -no-audio -no-boot-anim -no-snapshot-save -gpu swiftshader_indirect -port 5556` (`-read-only` for multiple instances of one AVD),
    `adb -s emulator-5556 exec-out screencap -p > shot.png`. Boot waits need timeouts, with one-line success / failure / timeout conclusions per CLAUDE.md.
- Parallel Agents: separate worktrees (Gradle daemons per directory, shared `~/.gradle/caches` fine), AVDs, and ports; enable Gradle configuration / build caches.

Sources:
- Developer verification: https://developer.android.com/developer-verification , https://www.androidauthority.com/android-sideloading-changes-timeline-3679204/ , https://www.megamobilecontent.com/news/2026/10/01/android-developer-verification-live-brazil-indonesia-singapore-thailand/
- Firebase App Distribution: https://firebase.google.com/docs/app-distribution/android/distribute-gradle
- Play testing: https://support.google.com/googleplay/android-developer/answer/14151465
- AGP 9.5: https://developer.android.com/build/releases/agp-9-5-0-release-notes ; AGP 9.3: https://developer.android.com/build/releases/agp-9-3-0-release-notes ; Studio compatibility: https://developer.android.com/build/releases/about-agp
- Emulator CLI: https://developer.android.com/studio/run/emulator-commandline ; Android CLI: https://developer.android.com/tools/agents/android-cli , https://developer.android.com/tools/agents/android-cli/commands/emulator_start

---

## 11. Summary

### 11.1 Recommended stack (stable versions checked 2026-10-30 unless noted)

| Layer | Choice | Version |
| --- | --- | --- |
| Language / build | Kotlin (K2, AGP built-in), Gradle catalog + `build-logic` convention plugins | Kotlin 2.4.21, AGP 9.5.0, Gradle 9.8.2 (≥ 9.6.0), KSP 2.3.12, JDK 17 (or Studio JBR 21) |
| SDK | compileSdk 37 / targetSdk 37 / minSdk 33 | Android 17 = API 37 |
| UI | Jetpack Compose + Material 3, custom 5-style tokens | BOM 2026.09.00 (Compose 1.12.1), material3 1.4.0 (Expressive waits for 1.5.0, currently beta02) |
| Glass | Haze frosting + Backdrop liquid refraction, API 33+ | Haze 2.0.1, Backdrop 2.0.1 |
| Architecture | UDF, App-wide Store / Repository → ViewModel(StateFlow) → Compose | Lifecycle 2.11.0, coroutines 1.11.0 |
| DI | Hilt, Metro alternative | Dagger/Hilt 2.60.1 (Metro 1.5.1) |
| Navigation | Navigation 3 | 1.2.0 + lifecycle-viewmodel-navigation3 2.11.0 |
| Network | OkHttp REST + WebSocket, kotlinx.serialization | OkHttp 5.5.0, serialization 1.12.0 |
| Storage | Room 3 (conservative Room 2.8.5), DataStore + datastore-tink, Tink | Room 3.0.3, DataStore 1.3.0, Tink 1.24.0 |
| Images | Coil 3 OkHttp + Telephoto | Coil 3.6.3 (AGP 9.4+ needs ≥ 3.6.3), Telephoto 0.19.0 |
| Markdown / highlighting | mikepenz multiplatform-markdown-renderer, iOS lightweight highlighting or Highlights | 0.46.0, Highlights 1.1.0 |
| Terminal | Custom pure-Kotlin VT grid + Compose Canvas + custom InputConnection | — |
| WebView | androidx.webkit async startup / prerender / preconnect / NavigationListener, custom three-state sheet; OAuth Custom Tabs | webkit 1.18.0 |
| Charts | Custom Compose Canvas, Vico alternative | Vico 3.3.1 |
| QR | Google code scanner, CameraX + ML Kit alternative | play-services-code-scanner 16.1.0, CameraX 1.6.2 |
| Push / background | High-priority FCM data, WorkManager; no permanent foreground service | WorkManager 2.12.0 |
| Lock screen / status bar | Live Updates (Android 16+), MetricStyle (Android 17+) | Platform APIs |
| Widgets / entry points | Glance, dynamic shortcuts, Quick Settings tiles, share target; future AppFunctions | Glance 1.2.0, AppFunctions 1.0.0-beta01 |
| Watch | Standalone Wear App: Compose for Wear OS M3 + Data Layer + Wear Widgets | Wear Compose 1.7.1 |
| Tests | JUnit + coroutines-test + Turbine, Roborazzi, Macrobenchmark + Baseline Profile, shared goldens | Roborazzi 1.77.0, benchmark 1.5.0 |
| Distribution | adb development, Firebase releases, developer verification before 2027 (limited account sufficient) | appdistribution plugin 5.3.1 |
| Tools | Android CLI (`android emulator start --headless`, `screen capture`, `input`, `preview render`) | — |

### 11.2 Main risks

1. **Android 17 local-network permission**: target 37 needs runtime `ACCESS_LOCAL_NETWORK` for LAN-hub access on Wi-Fi. Without it, TCP timeouts easily look like hub failure. VPN exempt but needs testing; Play declaration mandatory 2027-08-25.
2. **Developer verification**: from 2027 on certified devices worldwide, unregistered developers' APKs require advanced flow with 24-hour waiting; adb unaffected.
3. **Studio / AGP chain**: local Studio 2025.3 Panda supports at most AGP 9.0–9.2; Compose 1.12 / CameraX 1.7 / Glance 1.3 need ≥ 9.2, AGP 9.5 needs Rabbit 2. Upgrade IDE.
4. **Android can only approximate Liquid Glass**: Backdrop refraction needs API 33+, costly GPU; Haze glass experimental; midrange dropped-frame risk.
5. **Implementation drift**: without shared code, shared goldens and generated design tokens must constrain complex rules such as turn collapsing, or clients gradually diverge.
6. **Library maturity**: Room 3 stable 4 months, Navigation 3 evolving APIs, material3 1.5 beta, new biometric APIs alpha, AppFunctions beta with private Gemini integration.
7. **Google Play services dependency**: de-Googled phones cannot receive FCM; Live Updates only locally posted, no push-to-update; high-priority FCM without notifications downgrades.
8. **WebSocket weak-network detection**: OkHttp cannot measure ping RTT; add client-initiated application ping to hub protocol, small but server-side change.
9. **Play policies**, if distributed there: broad photo / video access generally denied (recent-photos tool must become Photo Picker); READ_CONTACTS from 2027-01-27 and local-network access need declarations.

### 11.3 Unresolved / requiring physical testing

- Actual local-network permission behavior for VPN (Tailscale etc.) access to 100.64/10: official definition excludes VPN but lists 100.64/10 as local. Needs Android 17 physical device.
- WebView nested scrolling / dismiss-drag coordination in custom Compose sheets.
- Exact minimum APIs / performance for Backdrop effects, absent from docs; Haze `haze-glass` maturity.
- Whether NotificationCompat wraps `MetricStyle`; OEM Live Updates behavior (Samsung, Xiaomi, etc.).
- Native Vico gap support, not mentioned in release notes.
- Play internal-testing availability within minutes / no review based only on third-party reports.
- Actual AGP 9.5+ test-suite screenshot usage (official page 404; only standalone plugin discontinuation known).
- Wear OS 7 rollout and direct Watch-to-LAN-hub reachability.
- Kotlin Swift export Beta schedule, not found.
