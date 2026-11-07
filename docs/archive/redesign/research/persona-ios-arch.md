# Current Persona iOS architecture and technical debt (redesign baseline)

> Scope: read-only Persona worktree `ios/`, `docs/apple/ios.md`, `docs/protocol/protocol.md`, `docs/protocol/agent-states.md`, `docs/integrations/terminal.md`, and `CLAUDE.md` "Lessons."
> Method: zoekt / git ls-files / rg (scoped subdirectories) / Read. **No builds, tests, or scripts run.**
> Labels: **[Fact]** includes `relative path:line` evidence; **[Assessment]** is my inference / recommendation, not runtime-verified.
> Performance numbers cite only code comments or documented measurements with sources; my own estimates are marked "Not measured."
> Privacy: no hostnames, IPs, accounts, internal project names, real names / message contents; suspiciously real DEBUG samples are not reproduced.

---

## 0. One-page conclusions (start here)

1. **Source-level sharing across targets, no module boundaries** [Fact]: one XcodeGen project (`ios/project.yml`), with App / Watch / widget targets compiling `Shared/` directly
   (`ios/project.yml:40-44, 104-108, 132-134`); no dependencies except MarkdownUI (`ios/project.yml:13-16`). Production Swift ≈ 37,600 lines, Views 26,900 (72%).
2. **`AppModel` is a god object** [Fact]: 998 lines + 3 extension files (Outbox / SendSafety / DeviceCredentials), with 22 responsibility categories counted (§3.1);
   mutual `.shared` singleton calls among it, `AgentSync`, `LarkSync`, `InboxStore`, and `LiveActivityController` form cycles.
3. **Much platform-independent business logic lives in Views** [Fact]: chat-line folding (`chatLines` etc., about 280 lines) is `private` in `ChatView.swift`, without unit tests;
   Agent `rank` / grouping in `ChatsView.swift`; Inbox grouping in `InboxView.swift`; Lark read reporting / cursors in `LarkChatView.swift` `@State`.
4. **Rules are duplicated and already inconsistent** [Fact]: ordering (`AgentState.priority` vs `ChatsView.rank` vs Watch vs `docs/protocol/agent-states.md:17`), live detection (`state.isLive` vs `status.isLive` vs `status != .offline`),
   plan approval policies (separate App / notification / Live Activity / Siri / Watch implementations with different modes), deep-link routing (`AppModel.open` / `InboxView.open` / `OpenSessionIntent`, the latter treating assistant sessions differently), sending (only `sendNow` uses Outbox + `client_msg_id`, with ≥5 direct paths).
5. **Tests** [Fact]: 24 unit-test files, 2716 lines, covering extracted pure logic such as ChatStore continuity / Rows-Blocks folding / terminal grid / Live Activities / Intent wording;
   but hosted in the App (`TEST_HOST`, `ios/project.yml:167-168`). Unextracted logic (chatLines, sorting, Outbox flush, AgentSync, LarkSync, LiveConnection) depends on UI tests (`scripts/mock/mock_hub.py`) and roughly 80 DEBUG launch-argument probes.
6. **No single protocol source of truth** [Fact]: Go `server/internal/proto/proto.go`, Swift `Shared/Models.swift`, and `docs/protocol/protocol.md` are hand-maintained. Drift exists (hub `assistant_schedule` broadcast is not decoded by the app; documented `session` / `ping` events are not emitted by hub; `hello.features` is absent from the documentation table).
   App `ChatStore.apply` continuity rules are also rewritten for simulation in Go tests (`server/internal/transcript/continuity_corpus_test.go:6-13`); adding Android creates a third production implementation.
7. **Key input for the KMP decision**: pure logic whose Android behavior must match is about **5,000 Swift lines** (A–D in §5 total about 5.1k; plus about 2.5k wire types and 1.1k pure Lark card/document parsing). Complexity centers on
   ① item continuity (covered/history/Gap), ② Rows→Blocks→Turn→Lines folding (wake-and-answer, recap reopening, frozen snapshots), ③ Outbox state machine, ④ terminal VT subset + wrapping + scroll detection.
   Rows/Blocks in ①② and ④ already have unit tests / Go simulation / recorded frames convertible to cross-platform JSON fixtures; Lines in ② and ③ have almost no unit tests. Transcript JSONL parsing is in the Go server (`server/internal/transcript/`), **not the client**.

---

## 1. Project topology

### 1.1 Targets and build settings [Fact]

| Target | Source | Notes | Evidence |
| --- | --- | --- | --- |
| `Persona` (iOS App) | `Persona/`, `Shared/`, `LiveActivity/` | Depends on PersonaWatch, PersonaWidgets, MarkdownUI | `ios/project.yml:37-99` |
| `PersonaWidgets` (Live Activity extension) | `PersonaWidgets/`, `LiveActivity/`, `Shared/FleetState.swift`, `Shared/StatusPalette.swift` | `WIDGET_EXTENSION` compilation condition isolates App code | `ios/project.yml:101-128` |
| `PersonaWatch` | `PersonaWatch/`, **all of `Shared/`** | Uses only a small subset (not `LiveConnection` / `ChatStore`) | `ios/project.yml:129-154` |
| `PersonaTests` | `PersonaTests/` | `@testable import Persona`, **App-hosted** (`TEST_HOST`), nonisolated by default | `ios/project.yml:155-168` |
| `PersonaUITests` | `PersonaUITests/` | Real App + mock / real hub | `ios/project.yml:169-183` |

- Swift 6.0 language mode, `SWIFT_DEFAULT_ACTOR_ISOLATION: MainActor`, `SWIFT_APPROACHABLE_CONCURRENCY: YES` (`ios/project.yml:19-21`).
- Release: stripping, `SWIFT_REFLECTION_METADATA_LEVEL: without-names` (`ios/project.yml:26-35`)—one source of the CLAUDE.md pitfall "DEBUG launch arguments must not share names with Swift identifiers."
- iOS / watchOS 26 (`ios/project.yml:8-10`).

### 1.2 Size [Fact] (`git ls-files … | wc -l`)

| Directory | Files | Lines |
| --- | --- | --- |
| `ios/Persona/App` | 13 | 2,522 |
| `ios/Persona/Intents` | 7 | 519 |
| `ios/Persona/PhoneTools` | 13 | 2,066 |
| `ios/Persona/Views` | 69 | 26,901 |
| `ios/Shared` | 17 | 4,452 |
| `ios/LiveActivity` + `ios/PersonaWidgets` | 5 | 509 |
| `ios/PersonaWatch` | 1 | 594 |
| Production total | | ≈ 37,563 |
| `ios/PersonaTests` | 24 | 2,716 |
| `ios/PersonaUITests` | 16 | 1,537 |

Views subdirectories: Chat 6,354 · Inbox (including Lark message rendering) 4,998 · Session (terminal / Git / files) 4,458 · Feishu Docs 2,336 · Settings 2,258 · Chats 1,081 · Assistant 1,035 · Machines 1,027 · Interactions 812 · Mail 552 · Knowledge 474 · Link 390 · NewChat 364.

Largest files: `ChatView.swift` 2,598 · `SessionPanes.swift` 1,147 · `AssistantProactiveViews.swift` 1,123 · `LarkCard.swift` 1,105 · `AppModel.swift` 998 · `TerminalView.swift` 927 ·
`ChatStore.swift` 877 · `InteractionViews.swift` 812 · `ChatsView.swift` 783 · `APIClient.swift` 755 · `MessageViews.swift` 749 · `FeishuDocsModels.swift` 744 · `LarkChatView.swift` 728 · `FeishuDocReader.swift` 717 · `Models.swift` 712.

### 1.3 Change hotspots [Fact] (`git log --name-only -- ios/`, rewritten history 2026-10-24 ~ 10-30, 243 commits touching ios/)

`ChatView.swift` 93 times · `AppModel.swift` 52 · `ChatsView.swift` 51 · `APIClient.swift` 36 · `SessionPanes.swift` 33 · `MessageViews.swift` 32 · `Models.swift` 31 · `SettingsView.swift` 28 · `InboxView.swift` 27 · `AssistantView.swift` 25 · `ChatStore.swift` 24.

[Assessment] In one week, 38% of iOS commits touched ChatView, consistent with CLAUDE.md lessons about concurrent agents pushing master and rebases overwriting others with stale copies: large files + one module create parallel-development conflicts.

---

## 2. Current module graph

```
@main PersonaApp ─┬─ AppDelegate (notification delegate / silent-push wakeup / DEBUG launch arguments)
                 ├─ PushRegistrar.shared (APNs registration → POST /api/v1/devices)
                 └─ RootView ← .environment(AppModel.shared)
                        │  TabView: Persona(AssistantView) / Agents(ChatsView) / Inbox(InboxView) / Settings
                        ▼
AppModel.shared  (@Observable, MainActor, 998 lines + extensions)
 ├─ LiveConnection (WS: reconnect/heartbeat/stalled) ──decode(background thread)──► ServerEvent
 ├─ ConnectionStatus (NWPathMonitor + 1.5s debounce + catch-up counter)
 ├─ chats: [SessionKey: ChatStore]  (LRU 8) ──► ChatCache(Caches/chats-v2)
 ├─ machines / sessionsByMachine / interactions ──► StateCache("dev")
 ├─ Outbox / drafts ──► StateCache("outbox"/"drafts")
 ├─ pins / mutes ──► UserDefaults + /api/v1/pins|mutes
 ├─ terminal stream routing terminalHandlers ──► TerminalFeed (View-owned)
 ├─ ⇄ AgentSync.shared ──► APIClient / ChatCache      (AgentSync reads AppModel.shared back)
 ├─ ⇄ LarkSync.shared  ──► StateCache("lark-chat-*")  (reads AppModel.shared back)
 ├─ ⇄ InboxStore.shared ──► StateCache("inbox-items") / LarkSync / WatchSync (reads AppModel.shared back)
 ├─ → LiveActivityController.shared ──► ActivityKit / APIClient (reads AppModel.shared.api back)
 ├─ → WatchSync.shared ──► WatchConnectivity
 ├─ → PhoneToolRegistry ──► EventKit/Contacts/HealthKit/Photos/CoreLocation…
 └─ → MarkdownMemo (View-layer cache! AppModel.swift:90-97)

Views: 81 @Environment(AppModel.self) references in 32 files; 79 direct model.api / .shared.api references in 26 files
Intents / NotificationActions / FleetActions: Keychain.serverConfig + APIClient direct hub access (bypass AppModel)
Watch: WatchModel (hub polling every 8s) + WatchSync (iPhone-pushed summaries)
Widgets: FleetLiveActivity consumes only FleetState / StatusPalette
```

### 2.1 Composition root and global state

**[Fact] Composition root**: `PersonaApp` (`ios/Persona/App/PersonaApp.swift:5-38`) holds only `AppModel.shared` for environment injection; `AppDelegate.didFinishLaunching`
(`PersonaApp.swift:41-94`) registers notification categories, dispatches about 12 DEBUG launch arguments, calls `WatchSync.activate`, `AppModel.start`, `LiveActivityController.start`,
and `PushRegistrar.registerIfPossible`. Foreground/background transitions: disconnect WS on backgrounding, `ensureConnected` + `refresh` on foregrounding (`PersonaApp.swift:27-36`).
Silent push `persona_wake`: connect WS, wait 25 seconds, then disconnect (`PersonaApp.swift:104-116`).

**[Fact] Singletons (10 `static let shared`)**: `AppModel`, `AgentSync`, `LarkSync`, `InboxStore`, `LiveActivityController`, `PushRegistrar`, `WatchSync`, `LarkMedia`, `WebViewPool`, `FrameStats`.

**[Fact] `AppModel` responsibilities (`ios/Persona/App/AppModel.swift`)**:

| # | Responsibility | Location |
| --- | --- | --- |
| 1 | Pairing / credentials / device-token exchange | `pair` 123-137, `unpair` 139-152; `DeviceCredentials.swift:15-44` |
| 2 | WS lifecycle and event dispatch | init wiring 89-106, `start` 116-121, `didConnect` 456-474, `handle` 494-557 |
| 3 | Connection state | `connection` 16 |
| 4 | Global entity cache + snapshot persistence | 17-34, `restoreSnapshot/saveSnapshot` 770-793 |
| 5 | Navigation (tab / session to open / current assistant chat / visible sessions) | 36-63, `open(_:)` 243-251, `rememberChat` 49-52 |
| 6 | UI feedback (toast, error display classification, haptics, phone-tool confirmation dialog) | 35-37, `show(_:)` 942-947, `notDone` 953-958, `Haptics` 993-998 |
| 7 | Assistant (configuration, main chat, warmup, launcher switching, askPersona) | 175-240 |
| 8 | ChatStore registry + LRU eviction (retain 8) | `chat(_:)` 257-265, `evictChats` 267-283 |
| 9 | Subscription reference counting / visible-session stack | 285-310, 370-380 |
| 10 | History paging / gap repair / reload | `subscribeMessage…loadOlder` 382-452 |
| 11 | Terminal stream routing and hub capability bits | 312-367 |
| 12 | Phone-tool registration and execution | `registerPhone` 478-492, `runPhoneTool/confirm` 570-583 |
| 13 | Interactions (pending list, resolve) | 171-173, 253-255, 587-603 |
| 14 | Sending (drafts, Outbox, refusal, retry, queued editing, /clear, stop loop) | 605-682, 914-917; `Outbox.swift`, `SendSafety.swift` |
| 15 | Session operations (seen, interrupt, mode, keys, close, launcher cache, rename) | 685-756, 877-908 |
| 16 | Same-space peer computation | `siblings` 699-715 |
| 17 | Pin / mute rules and optimistic updates | 795-865 |
| 18 | Interface style | 760-766 |
| 19 | Cross-page composer image / context insertion | 869-872 |
| 20 | Full refresh (Live Activity preferences and fleet included) | `refresh` 919-940 |
| 21 | Watch summary | `watchSummary` 960-966 (and pushes at 98-103, 514, 544, 547) |
| 22 | Completion haptics | `notifyTransitions` 559-566 |

[Assessment] 22 categories, four persistence mechanisms (Keychain / UserDefaults / StateCache / ChatCache), direct UIKit (`UIDevice`) and SwiftUI (`withAnimation`, lines 822, 863) calls; neither independently testable nor structurally reusable on Android. This is the first refactoring cut.

---
### 2.2 Networking

**[Fact] REST: `APIClient`** (`ios/Shared/APIClient.swift:65-626`, `nonisolated struct`, `Sendable`)
- New instance on every `AppModel.api` access (`AppModel.swift:70`); Bearer token; `X-Client-Request-Id`; 5xx errors include `X-Request-Id` (`APIClient.swift:85-108`, `24-37`).
- Large responses (sessions, items, Inbox) decode in `Task.detached` (`APIClient.swift:188-193, 218-225, 277-281`; `InboxModels.swift:128-132`).
- `uncachedSession`: bytes cached by the app itself (chat images, Lark media) bypass system URLCache (`APIClient.swift:69-76`; CLAUDE.md "Hub APIs default to no-store").
- **Endpoints are spread across extensions in 9 files**: `APIClient.swift` (core + pins/mutes/rename), `DevModels.swift:98-118` (git/fs), `InboxModels.swift:125-166`, `Knowledge.swift:97-112`,
  `Views/Mail/MailViews.swift:5-90`, `Views/Inbox/Lark/LarkModels.swift:176-296`, `Views/Chat/FileViewer.swift:22-26`, `Views/Feishu/Docs/FeishuDocsComponents.swift:6-19`, `Views/Feishu/Foundation/FeishuFoundation.swift:8-30`.
  `"/api/v1…` literals: APIClient 44, LarkModels 21, MailViews 10, InboxModels 6, Knowledge 4…
- Error classification is centralized in `Error` extensions: `isCancellation / isTimeout / isOffline / isQuiet / shown` (`APIClient.swift:634-674`), implementing CLAUDE.md "Cancellation is not an error" and "Do not show timeouts"; **each call site still decides whether to display**.

**[Fact] WebSocket: `LiveConnection`** (`ios/Shared/LiveConnection.swift:135-322`, `@Observable`)
- `ServerEvent` envelope decoding (`LiveConnection.swift:17-133`): hello / machines / sessions / items / screen / delta / interaction / interaction_resolved / tool_call / hook_event / revs / lark / inbox / fleet / terminal / terminal_control / error.
  Decode on the `URLSessionWebSocketTask.receive` callback thread, then hop through `Task { @MainActor }` (`LiveConnection.swift:267-297`).
- Configuration constants (not measurements): ping every 20s, dead after 45s without any message, stalled after 5s without pong and ping every 5s, backoff 1→2→…→20s, `maximumMessageSize` 64 MB (`LiveConnection.swift:167-170, 220, 299-308`).
- `generation` discards old connection callbacks (`LiveConnection.swift:161, 209-213`).
- `send(_:)` **silently discards while disconnected** (`LiveConnection.swift:310-321`). [Assessment] `tool_result`, `terminal_input`, `seen`, etc. may be lost at disconnect without feedback; `markSeen` has its own HTTP fallback (`AppModel.swift:685-690`), others do not.
- Subscription replay after reconnect: `didConnect` sends `subscribe` for every subscribed session with `since_seq = covered` (`AppModel.swift:456-474, 383-388`).

**[Fact] Connection state: `ConnectionStatus`** (`ios/Persona/App/ConnectionStatus.swift`, 151 lines)
- Four states `online / waitingForNetwork / connecting / updating`; pure `phase(network:link:stalled:catchingUp:)` (`ConnectionStatus.swift:77-82`) is testable and unit-tested;
  1.5s debounce, catch-up at most 8s (`35-37`); immediate reconnect on network recovery, immediate ping on network change (`111-123`). Catch-up completion triggers Outbox flush (`AppModel.swift:106`).

### 2.3 Session data and synchronization

**[Fact] `ChatStore`** (`ios/Shared/ChatStore.swift:266-583`, one per session, `@Observable`)
- State: `items` (deduplicated, sorted by `(seq, part)`), derived `rows` / `blocks` / `images`, `version`, `resets`, `hasMore`, `loadedOnce`, `pending` (outgoing messages), `liveText` (assistant streaming), continuity cursors `covered` / `history`.
- `apply(_:span:backlog:hasMore:moreAfter:) -> Gap` (`ChatStore.swift:322-359`): clear on a `history` file change; advance `covered` only for continuous batches with `from <= covered`, otherwise return `.fill`; replace entirely when a tail-relative page does not connect to local data.
  `merge` trusts `hasMore` only for pages covering the earliest local item (`ChatStore.swift:381-384`, corresponding to CLAUDE.md "Only latest-end pagination responses know has_more").
- Derived rebuilding uses background `Task.detached`, reusing results before the last prompt (`SettledRows`, `ChatStore.swift:397-472`); comment reports "Full rebuild costs 30 ms per event at 1000 turns" (`ChatStore.swift:401-402`, sample count unspecified).
- Disk writes: last two turns, 2-second debounce (`ChatStore.swift:474-488`).
- **init synchronously reads disk + decodes + fully builds rows** (`ChatStore.swift:301-314`), and `AppModel.chat(_:)` is called in a computed property used by View body (`ChatView.swift:98`). [Assessment] First opening a large cached session (6 MB cap, `ChatStore.swift:795`) incurs synchronous main-thread decoding; duration not measured.
- `screen / screenStatus / screenUpdatedAt / livePreview / spinnerLine` (`ChatStore.swift:283-287, 526-528`) are written only by `AppModel.handle(.screen)` (`AppModel.swift:533-537`), with **no readers anywhere**; `docs/protocol/protocol.md:164` also says node no longer pushes screens on subscription—dead state.

**[Fact] Subscription / gap repair / paging (in AppModel)**: reference-counted `subscribe` (multiple views of one session do not cancel each other, `AppModel.swift:287-308`);
`settle(.fill)` waits 1.5s then `fill` (at most 8 `after_seq` pages, then `reload` latest, `AppModel.swift:402-433`); `loadOlder` uses `before_seq`, `turns=2` (`AppModel.swift:441-452`, `APIClient.swift:218-225`).
Matches `docs/protocol/protocol.md:240-248`, "Item continuity." (`docs/archive/integrations/msg-render-plan.md` already diagnosed this chain; not repeated here, only logic locations in §6.)

**[Fact] Background synchronization**
- `AgentSync` (`ios/Persona/App/AgentSync.swift`, 148 lines): hub `rev` (session lists / `revs`) differing from local `synced` (UserDefaults `agentSync.revs`) triggers `after_seq` reads;
  scope = nonoffline or updated within 24h, at most 40, concurrency 3, 300ms coalescing (`AgentSync.swift:13-15, 50-80`). Merge into in-memory ChatStore if present, otherwise update disk cache in background (`pullToDisk`, `125-147`).
- `LarkSync` (`ios/Persona/App/LarkSync.swift`, 104 lines): `lark` events → `AsyncStream` to open chats + latest-page background reads into `lark-chat-<id>`; concurrency 2, at most once per 3s per chat, latest 20 on reconnect (`LarkSync.swift:11-15`).
  `LarkChatView.apply` also writes `lark-chat-<id>` (`LarkChatView.swift:247`), two writers coordinated by convention.
- `InboxStore` (`ios/Persona/App/InboxStore.swift`, 149 lines, `@MainActor @Observable`): `reload` on `inbox` events / reconnect (coalesced concurrent requests), optimistic Done with rollback, one-tap AI twin draft sending;
  every list change pushes `AppModel.shared.watchSummary()` to Watch (`InboxStore.swift:54-61`).

### 2.4 Caching and persistence [Fact]

| Storage | Contents | Location |
| --- | --- | --- |
| Keychain | `server` (pairing), `watch` (answer-only watch credentials) | `ios/Shared/Keychain.swift:33-48` |
| `StateCache` (Caches/state/*.json, detached writes) | `dev` (machines/sessions/interactions/assistant snapshot), `outbox`, `drafts`, `inbox-items`, `mail-accounts`, `lark-sync`, `lark-chat-<id>` (latest 80) | `ios/Shared/StateCache.swift:5-24`; callers `AppModel.swift:779-793`, `Outbox.swift:73-94`, `SendSafety.swift:50-63`, `InboxStore.swift:16-19`, `LarkSync.swift:27-30, 90` |
| `ChatCache` (Caches/chats-v2) | Last two turns' items + hasMore/history/covered per session; drop images above 6 MB | `ios/Shared/ChatStore.swift:786-837` |
| `ChatImageLoader` | NSCache (96 MB cost cap) + Caches/chat-images; one download per ref (actor) | `ios/Persona/Views/Chat/ChatImageLoader.swift:6-73` |
| `AttachedImageCache` | Static dictionary, **unbounded** | `ios/Persona/Views/Chat/MessageViews.swift:373-378` |
| `RowMemo` / `TextMemo` / `MarkdownMemo` | Derived results by message id / text (Markdown parsing, paths, links), caps 20,000 / 4,000 / 128, clear whole table when full | `MessageViews.swift:699-723`, `ios/Shared/TextMemo.swift` |
| `LarkMedia`, `WebViewPool` | Lark media memory + disk; at most 6 preloaded WKWebViews (LRU) | `Views/Inbox/Lark/LarkMedia.swift:11`, `docs/apple/ios.md:43` |
| UserDefaults | 132 calls (30 files), about 87 literal keys, roughly 80 DEBUG launch arguments; about 20 `@AppStorage` keys and dynamic `chat.read.<session>`, `agentSync.revs`, etc. | `rg 'UserDefaults\.standard'` |
| Storage page | Categorizes counts / clearing by directory name | `ios/Persona/Views/Settings/StorageView.swift:66-145` |

`TextMemo` comments report derived chat-row work once took "about 15% of main-thread time in long sessions" (`ios/Shared/TextMemo.swift:3-5`, profile result, sample unspecified).

### 2.5 Sending

**[Fact] Standard path (Agent / assistant chat composers)**: `Composer.sendText` → `AppModel.sendNow` (`ChatView.swift:1854-1866`, `AppModel.swift:631-637`) →
`PendingMessage` enters `ChatStore.pending` and appears immediately → online `sendPending` (with `client_msg_id`, `SendSafety.swift:23-42`), otherwise persist `.waiting` (`Outbox.swift:75-83`)
→ flush by session and order after reconnect catch-up; unopened sessions first fetch one page to verify delivery (`Outbox.swift:50-71`) → remove when matching text appears in transcript (`PendingMessage.same`, `ChatStore.swift:850-869`); `sent` remains at most another 60s (`Outbox.swift:32-37`).
Refusal: 409 `input_draft` opens Clear and send dialog; `delivered: unknown` warns it may have arrived (`SendSafety.swift:29-35`).

**[Fact] Paths bypassing Outbox** (no `client_msg_id`, immediate offline failure):
- `AppModel.send` (for `/clear`, `AppModel.swift:672-682, 642-652`);
- `AppModel.askPersona` (Hand to Persona from other pages, `AppModel.swift:207-223`);
- `AssistantView.start` (first message of a new assistant chat, `ios/Persona/Views/Assistant/AssistantView.swift:101-123`);
- `LarkChatView.send` (Lark replies, `LarkChatView.swift:402-416`), `InboxStore.send` (AI twin drafts);
- Siri `AskClaudeIntent.deliver` (`ios/Persona/Intents/AskClaudeIntent.swift:67-77`); Watch `WatchModel.send` (`ios/PersonaWatch/PersonaWatchApp.swift:107-116`).
[Assessment] Sending reliability depends on entry point; copying this into Android inherits inconsistency. Define one Core send use case (idempotent id + network waiting + refusal handling) shared by all entries.

### 2.6 Interactions and approval policies

**[Fact]** `pendingInteractions` sorts by creation time; `resolve` first removes the card locally, restoring on failure except 410; `prompt_changed` gets specific guidance (`AppModel.swift:587-603`).
Screen cards prefer hub-parsed `dialog` (answer by `option`, node checks `rev`); old hubs fall back to app `ScreenParser.dialog` (`Models.swift:469`, `InteractionViews.swift:701-705`).

**[Fact] Plan approval mode is separately implemented at 5 entry points**:
| Entry | Approval mode | Evidence |
| --- | --- | --- |
| In-app plan card | `bypassPermissions` (Approve and run automatically) or `acceptEdits` | `ios/Persona/Views/Interactions/InteractionViews.swift:466-476` |
| Notification button | `acceptEdits` (comment: full plan unseen, commands still require individual approval) | `ios/Persona/App/NotificationActions.swift:42-48` |
| Live Activity button | Reuses notification mapping → `acceptEdits` | `ios/Persona/App/FleetActions.swift:9-15`, `ios/LiveActivity/FleetIntents.swift:44-62` |
| Siri Approve first | `bypassPermissions` | `ios/Persona/Intents/ApproveFirstIntent.swift:36-42` |
| Watch | `bypassPermissions`; Send back uses fixed comment "请重新考虑这个计划" (Please reconsider this plan) | `ios/PersonaWatch/PersonaWatchApp.swift:364-365` |
[Assessment] Comments explain lock-screen limited plan visibility → acceptEdits, yet Siri (also no full plan) and Watch bypass. Define and test such safety policy in one table.

### 2.7 Push, notifications, background wakeup [Fact]
- `PushRegistrar` (`PersonaApp.swift:209-246`): request permission → APNs token → `POST /api/v1/devices` (`kind: iphone`, `apns_env`, `topic`); device id stored in UserDefaults `deviceID`.
- Notification buttons (`NotificationActions.swift`) directly resolve in background; failures send local notifications (`PersonaApp.swift:155-172`). Delegate uses completion-handler version and dispatches to main queue (`PersonaApp.swift:118-150`, corresponding CLAUDE.md lesson).
- Hub push supports only APNs (`server/internal/hub/push.go:28-63`). [Assessment] Android needs an FCM (or other) channel and device type on hub; server-side work.

### 2.8 Phone tools [Fact]
- `PhoneTool { spec, source, confirmation?, run }` (`ios/Persona/PhoneTools/PhoneTool.swift:4-10`); 9 source toggles stored in UserDefaults (`PhoneTool.swift:13-66`).
- `PhoneToolRegistry.call` is the sole entry: unknown tool / disabled source / denial / thrown errors become bilingual Chinese-English text for Claude, **never throwing** (`PhoneToolRegistry.swift:95-119`); writes first `confirm` (`AppModel.confirm` suspends with continuation until dialog response, `AppModel.swift:578-583`).
- On WS connection, `register_phone` declares enabled specs (`AppModel.swift:478-492`); hub sends `tool_call` only to registered phones (`docs/protocol/protocol.md:169, 181`).
- Unit coverage: unique names / valid schema / toggles / denied confirmation / parameter clamping / date parsing (`ios/PersonaTests/PhoneToolsTests.swift`).
[Assessment] Protocol is platform-independent (client declares specs), so Android can declare its tools; shared names / parameters / result JSON shapes require one shared tool contract for assistant prompts usable on both.

### 2.9 Siri / Shortcuts [Fact]
- Three AppShortcuts + `OpenSessionIntent` (`ios/Persona/Intents/PersonaShortcuts.swift`); background intents directly use `Keychain.serverConfig` + `APIClient` (`IntentSupport.swift:133-144`).
- Pure logic extracted into `IntentText / IntentPhrases / IntentFailure / IntentPicker` (`IntentSupport.swift:8-165`), with 14 unit tests (`IntentsTests.swift`)—the repository's best separation of logic from views.
- `OpenSessionIntent.perform` unconditionally sets `selectedTab = .chats` (`OpenSessionIntent.swift:68-74`), while `AppModel.open` routes assistant sessions to Persona (`AppModel.swift:243-251`); `InboxView.open` copies routing again (`InboxView.swift:186-194`).
- `PersonaStatusIntent` counts using `status == .working` (`PersonaStatusIntent.swift:24`); UI uses `AgentState`.

### 2.10 Watch [Fact]
- `WatchSync` (`ios/Shared/WatchSync.swift`): iPhone pushes pairing (applicationContext + transferUserInfo) and summaries (2s throttle); Watch can proactively request pairing through `sendMessage`.
- `WatchModel` (`PersonaWatchApp.swift:21-150`): while open, directly polls hub sessions / interactions / machines / Inbox every 8s; answers, messages, interruption, and Inbox replies also directly call hub.
- Watch ordering: `state.priority` + `sortDate`, with filters differing from Agents (`PersonaWatchApp.swift:56-58`).

### 2.11 Live Activity (Fleet) [Fact]
- Hub computes content (`FleetState`, Unix-second times, tolerant decoding, `ios/Shared/FleetState.swift:1-161`); foreground app applies `fleet` events and reports push-to-start and activity tokens (`ios/Persona/App/LiveActivityController.swift:57-171`).
- Dynamic Island side algorithm `IslandCell.sides/minimal`, wording `FleetText`, device-specific `tailored` (`FleetState.swift:142-364`), 12 unit tests (`FleetStateTests.swift`).
- Settings mirrored in UserDefaults (`LiveActivityController.swift:17-24`), authoritative on hub (`NotifyPrefs`).

### 2.12 Terminal subsystem [Fact] (`ios/Persona/Views/Session/Terminal/`, 2,668 lines, plus `TerminalPanel` in `SessionPanes.swift`)
- `TerminalGrid` (419 lines, pure logic): VT subset for herdr's already-emulated grid (CSI cursor / erase / insert-delete / SGR 256 colors + true color / OSC 8 links), two-cell wide characters and right-half-cell rules, `scrolled(from:to:)` inferring rows scrolled off screen (`TerminalGrid.swift:393-419`).
  Choice rationale (not SwiftTerm / libghostty) in `docs/integrations/terminal.md:58-78`.
- `TerminalWrap` (141 lines, pure logic): phone-width wrapping, hanging indent, CJK line breaks.
- `TerminalFeed` (351 lines, `@MainActor @Observable`, in Views): streaming / polling modes, epoch+seq replay and frame-loss resubscription, control-takeover state machine, 3000-row scrollback, 1/2/4/8/15s backoff (`TerminalFeed.swift:77-106, 206-289`).
  Registers callbacks through `AppModel.terminalSubscribe` (`AppModel.swift:330-338`); **subscription timing depends on columns×rows measured at first view layout** (`TerminalFeed.swift:80-83`; CLAUDE.md "Wait for terminal first layout to measure phone columns × rows before takeover subscription").
- Keys: `KeyBatcher` (coalesces text / rolls back unsent characters), `TerminalKeyBytes` (herdr key names → VT bytes, direct bytes during takeover), `TerminalModifiers` (Moshi-style sticky / locked / held), `TerminalCompletion` (screen-based completion)—all pure logic (`TerminalInput.swift:16-107`, `Views/Chat/TerminalKeys.swift:1-70`, `TerminalExtras.swift:282-320`).
- Rendering `TerminalView` (927 lines, UIScrollView + CoreGraphics cell drawing, selection, edit menu, wheel / click reporting); `UITextInput` (Pinyin marked text).
- Tests: `TerminalGridTests` (10), `TerminalWrapTests` (about 14, including recorded frame fixtures `ios/PersonaTests/Fixtures/terminal-*.jsonl`), `TerminalTests` (12).

### 2.13 View organization [Fact]
Page directories: `Assistant / Chat / Chats / Feishu / Inbox(+Lark) / Interactions / Knowledge / Link / Machines / Mail / NewChat / Session(+Terminal) / Settings`, plus `Components.swift`, `Styling.swift`, `RootView.swift`, `ConnectionTitle.swift`.
- Design system: `Shared/Theme.swift` (5 `AppStyle` values, palette / radius / spacing / font tokens, `Fmt` formatting), `Shared/StatusPalette.swift` (shared App / Watch / widget state colors), `Views/Styling.swift` (`.surface` / `.themedButton` / `.themedPage`).
  Style is mutable global static `AppStyle.current` (`nonisolated(unsafe)`, `Theme.swift:17-18`); `RootView` rebuilds the whole tree using style as id (`RootView.swift:17-18`).
- Inconsistent file placement: terminal key bar `TerminalKeys.swift` in `Views/Chat/`; models `TerminalFeed`, `FileUploads`, `LarkMedia` under Views; API extensions scattered in View files (§2.2).

### 2.14 Dependencies (who references whom) [Fact]

| Module | References | Referenced by |
| --- | --- | --- |
| `AppModel` | LiveConnection, ConnectionStatus, ChatStore, APIClient, Keychain, StateCache, WatchSync, PushRegistrar, LiveActivityController, AgentSync, LarkSync, InboxStore, PhoneToolRegistry, **MarkdownMemo (View layer)**, UIKit, SwiftUI | 32 View files (Environment), AgentSync / LarkSync / InboxStore / LiveActivityController / ChatImageLoader / LarkMedia / OpenSessionIntent / PushRegistrar (`AppModel.shared`) |
| `AgentSync` | `AppModel.shared` (live, api, sessionsByMachine, isSubscribed, existingChat, fill, reload), ChatCache, ChatStore static functions | AppModel |
| `LarkSync` | `AppModel.shared` (api, live.state), StateCache | AppModel, InboxStore, LarkChatView |
| `InboxStore` | `AppModel.shared` (api, show, toast, watchSummary), LarkSync, WatchSync | AppModel, InboxView, RootView badge, Watch summary |
| `ChatStore` | ChatCache, ScreenParser (same file) | AppModel, AgentSync, ChatView, ChatsView, etc. |
| `LiveConnection` / `APIClient` / `Models` | Foundation only | All |
| Intents / FleetActions / NotificationActions | Keychain + APIClient; FleetActions also calls LiveActivityController | System |
| Watch | All Shared sources (actually uses Models, APIClient, WatchSync, Theme, StatusPalette, WatchText, ScreenParser.options) | — |

[Assessment] Only `Shared/` `Models / APIClient / LiveConnection / ChatStore / FleetState / MachineMetrics / JSONValue` are already clean App-independent layers, almost directly extractable into a package; all synchronizers in `App/` are reverse-coupled through `AppModel.shared`.

---

## 3. State management and concurrency

### 3.1 State management [Fact]
- **Primarily Observation**: 12 `@Observable` classes (AppModel, LiveConnection, ConnectionStatus, ChatStore, InboxStore, TerminalFeed, ChatChrome, MessageMenu, FileUploads, FeishuDocsReaderModel, LarkMedia, WatchModel).
  One legacy `ObservableObject`: `InboxAccountState` (`InboxView.swift:3-5, 18`).
- **Global singletons coexist with environment injection**: root `.environment(AppModel.shared)` (`PersonaApp.swift:14`), yet views directly access `InboxStore.shared`, `LarkSync.shared`, etc. (e.g. `RootView.swift:99`, `LarkChatView.swift:138`).
- **Environment closures as dependency injection**: 17 `@Entry` values, mostly action closures (`openFile`, `loadImage`, `loadFullText`, `attachImage`, `openLink`, `docPick`, `openSpace`, `openSubagent`, `feishuOpen`, `larkOpenFile`, `larkRefresh`, `larkOpenProfile`, `larkReact`…, `MessageViews.swift:84-98`, etc.).
  CLAUDE.md "Attach environment values to NavigationStack itself" is a pitfall of this implicit injection.
- **Unobserved bookkeeping**: `ChatScratch` (`ChatView.swift:2534-2572`) holds scrolling writes and derived-row cache, avoiding whole-page reevaluation; `ChatChrome` separately observes header-collapse progress (`ChatView.swift:2487-2498`).
- **Command-as-state navigation**: `ChatsView` consumes `AppModel.openChat` in `onChange`, then sets nil (`ChatsView.swift:274-278`); similarly `selectedTab` and `assistantCurrent`. No central router.
- **Business state in Views**: 15+ `@State` properties in `LarkChatView` (cursors, reads, unread anchors, `LarkChatView.swift:26-48`), about 30 `@State` properties in `ChatView` (`ChatView.swift:16-88`); `TerminalPanel` owns `TerminalFeed` (`SessionPanes.swift:106`).

### 3.2 Concurrency [Fact]
- Default MainActor isolation (`ios/project.yml:20`): every type defaults to main actor; pure data types need explicit `nonisolated` (CLAUDE.md lesson; every `Models.swift` type is marked). Test target conversely defaults nonisolated (`ios/project.yml:165`).
- Background work: 23 `Task.detached` sites (decoding, rows, disk writes, Markdown warming); only one `actor`, `ChatImageLoader.Fetches` (`ChatImageLoader.swift:49`); lock + `@unchecked Sendable` in `TextMemo` (`TextMemo.swift:6`).
- About 213 unstructured `Task {` sites (including detached); 16 `nonisolated(unsafe)` (WCSession callbacks, ActivityKit, static caches, `AppStyle.current`); 2 `MainActor.assumeIsolated` (notification delegates).
- WebSocket decoding on callback thread before main-thread hop (`LiveConnection.swift:269-279`, corresponding CLAUDE.md lesson); ChatStore rebuilding coalesces by revision and publishes only newest snapshot (`ChatStore.swift:449-472`).
[Assessment] Safety relies mainly on default main thread + a few detached pure functions, simple and effective; but 213 fire-and-forget Tasks lack cancellation ownership (e.g. 60s bubble removal in `settleSend`, delayed 1.5s fill in `settle`), making lifecycle reasoning/testing difficult.

---

## 4. Technical debt and pain points

### 4.1 Huge files / mixed responsibilities [Fact + Assessment]
- `ChatView.swift`, 2,598 lines: list engine (inverted, freeze, landing, following), header (gesture-following collapse), Composer (photos / camera / files / slash commands / `/clear`), QueueStrip, DevInfoCard, TerminalCover, CameraPicker,
  **folding algorithms** (`chatLines` 268-329, etc.), `ChatLine` / `WorkItem` / `WorkSummary` models, about 150 lines of DEBUG probes (`appeared` 1374-1526).
  CLAUDE.md Release archive type-check timeouts originate in long modifier chains here (`ChatView.swift:1033` comment).
- `AppModel.swift`: 22 responsibility categories (§2.1).
- `SessionPanes.swift`, 1,147 lines: `TerminalPanel` (keyboard, paste, pinch, two-finger session switching, mode switching) + loop / background-task / subagent strips + `KeyStream`.
- `AssistantProactiveViews.swift`, 1,123 lines (scheduled tasks / follow-ups / memory / About me pages); `InteractionViews.swift`, 812 lines (4 form types + DEBUG samples).

### 4.2 Business logic in Views (hard to test or reuse) [Fact]
| Logic | Location | Tests |
| --- | --- | --- |
| Chat-line folding `chatLines / segments / workItems / photos / wakeLines / unfoldWork / prefixCount`, frozen snapshot `Frozen` | `ChatView.swift:42-47, 238-329, 425-436, 490-531, 593-608` | Private `chatLines`, no unit tests; only `ChatLine.id` shape (`ChatLineTests.swift`) and `prefixCount` (`ChatStoreTests.swift:406`) tested |
| Agent ordering / grouping / folding `rank / ordered / stays / SpaceGroup.build / isWaiting` | `ChatsView.swift:4-49, 100-127, 304-333` | None |
| Inbox filtering / grouping / pending-Agent interleaving | `InboxView.swift:31-47, 95-101` | None |
| Lark: unread anchors, cursor pagination, read reporting (on-screen only), polling fallback, optimistic reactions | `LarkChatView.swift:243-416` | Only `LarkPosition` comparison and `togglingReaction` (`LarkRenderTests.swift`) |
| Question-answer rules (multi-select joining, Other override, immediate single-question/single-choice submission) | `InteractionViews.swift:385-434` | None |
| Screen-card answers (option / preKeys / keys) | `InteractionViews.swift:700-705` | UI `SendSafetyTests` |
| Phone width / space ordering for two-finger session switching | `SessionPanes.swift:567-584` | None |
| New assistant chats, rename, following the new session after /clear | `AssistantView.swift:101-123`, `AppModel.swift:642-652` | None |
| Prompt templates (Hand to Persona, Lark summary, stop /loop, slash commands) | `KnowledgeView.swift:465-474`, `LarkChatView.swift:418-428`, `AppModel.swift:914-917`, `ChatView.swift:1908-1914` | None |

### 4.3 Duplicated / inconsistent rules [Fact]
1. **State ordering**: `AgentState.priority` = Needs you → Unread → Working → Seen → Unknown → Not running (`Models.swift:312-324`); `ChatsView.rank` adds Waiting / Muted (`ChatsView.swift:313-322`);
   Watch uses `priority` (`PersonaWatchApp.swift:56-58`); `docs/protocol/agent-states.md:17` says Needs you → **Working → Unread**, opposite to code (line 20 in the same file agrees with code).
2. **Live / working detection**: `state.isLive` (ChatsView, SpaceView, MachineSpacesView, siblings), `status.isLive` (`MachinesView.swift:104`, `watchSummary` at `AppModel.swift:961`, ChatView menu 1190),
   `status != .offline` (`AgentSync.swift:65`, `IntentSupport.swift:157`), `status == .working` (`PersonaStatusIntent.swift:24`) vs `state == .working` (many places). UI mixes herdr `SessionStatus` and hub-standardized `AgentState`.
3. **Fold-when-idle condition**: identical `stays` twice (`ChatsView.swift:162-165` closure and `305-308` function).
4. **Plan approval mode**: 5 entries, 3 values (§2.6).
5. **Deep-link routing**: `AppModel.open`, `InboxView.open`, `OpenSessionIntent` (§2.9).
6. **Sending**: one Outbox path, ≥5 direct paths (§2.5).
7. **Assistant-session exclusion**: machine-page counts exclude assistants separately in `MachinesView.swift:102-104`, `MachineSpacesView.swift:13`, `ChatsView.swift:102` (one omission caused the corresponding CLAUDE.md lesson).
8. **Monitor thresholds**: `MetricAlert.of` defines 75/90, 80/90 (`MachineMetrics.swift:151-164`), repeated as literals in `MachineDashboardView.swift:129-135`, `MachineMonitorView.swift:154-161`.
9. **Continuity**: Swift `ChatStore.apply` and Go simulation (`continuity_corpus_test.go:6-13`).

### 4.4 Implicit dependencies and lifecycle [Fact + Assessment]
- Synchronizer cycles (AppModel ⇄ AgentSync / LarkSync / InboxStore) require whole-system tests; `AgentSync.plan` reads `AppModel.shared.sessionsByMachine` (`AgentSync.swift:60-80`).
- `AppModel.chat(_:)` has side effects (LRU bookkeeping, store creation, eviction), yet runs in a computed property used by View body (`AppModel.swift:257-265`, `ChatView.swift:98`).
- App layer references View cache: `live.prepareEvent` warms `MarkdownMemo` (`AppModel.swift:90-97`).
- `AgentSync.pullToDisk` and subsequently created ChatStore access the same cache file; `LarkSync` and `LarkChatView` write the same `lark-chat-<id>`: [Assessment] timing conventions may permit overwrite races, unverified.

### 4.5 Test structure [Fact]
- Unit tests are App-hosted (`ios/project.yml:167-168`), launching the App (`AppModel.shared` initialization, notification permission request)—CLAUDE.md "xcodebuild test launches without -pairURL, requests notifications, and subsequent screenshots are blocked by the dialog."
- Good coverage: ChatStore continuity and folding (`ChatStoreTests` 30+, `RebuildTests` incremental = full property tests over 40 seeds, `RebuildTests.swift:50-73`), terminal, Live Activities, Intents, phone-tool registry, Lark document/message parsing.
- UI-only or untested: chatLines, Agent ordering, Inbox grouping, Outbox flush ordering (`ConnectionTests.testMessageWaitsForTheHub`), refusal (`SendSafetyTests`), LiveConnection heartbeat / reconnect, AgentSync, LarkSync, Lark read reporting, `ToolPresentation`, `DiffText.parse`, `Fmt`, `WebLinks`, `PlanText`.
- UI tests: 16 files, mostly using `scripts/mock/mock_hub.py` (1,040-line Python mock, env-controlled offline, refusal, slow upload, etc.; see file headers), a few connect to real test hubs (`MachinePagesLiveTests.swift:1-5`).
- About 80 DEBUG launch arguments (`-openSession`, `-showFoldProbe`, `-fromSeq`, `-showMenu`…, `docs/apple/ios.md:299-305`); probes write results to `Documents/*.txt` (`ChatView.swift:334-421`); 66 `#if DEBUG` blocks, 14 in ChatView alone.
- `TerminalTypingTests.swift:7-9` still passes `-terminalEngine`, with no corresponding code (legacy argument).

### 4.6 Dead code and documentation drift [Fact]
- `ChatStore.screen*` / `livePreview` / `spinnerLine` have no readers (§2.3); decoded `ServerEvent.hookEvent` is ignored with `break` (`AppModel.swift:550-551`).
- Hub broadcasts `assistant_schedule` (`server/internal/hub/assistant_schedule.go:688`), not decoded by App or listed in `docs/protocol/protocol.md`.
- `hello` fields in `docs/protocol/protocol.md:156` omit `features` (actually sent by hub, `server/internal/hub/http.go:616`); documented `session`, `ping` events have no emitters found in non-test hub code.
- `docs/apple/ios.md:85` still says hub-unreachable banner lasts 5 seconds, contradicting `docs/apple/ios.md:273-276` (banner removed, 1.5s debounce); `docs/apple/ios.md:186` live-status spinner row corresponds to dead state above;
  `docs/apple/ios.md:90-101` describes `frozenAt`, bottom-edge jump then sentinel alignment, etc., predating inverted lists; `docs/server/architecture.md:49-50` live-screen bar is likewise stale.
- No code found for Inbox 90-second fallback polling in `docs/apple/ios.md:33` (`InboxStore` refreshes only on events / reconnect / pull-to-refresh); Lark's 60s fallback exists (`LarkChatView.swift:132-135`).

### 4.7 Privacy
- [Fact] DEBUG screenshot screen samples in `InteractionViews.swift:768-811` contain apparently real work text (tool names, ticket-like numbers). Not in Release, but violates CLAUDE.md placeholder-only examples. [Assessment] Replace with placeholders. `scripts/privacy_check.py` scans all `git ls-files` (168-180), but uses patterns (configured real values, long IDs, real domains, emails, 6+ digit numbers, 69-79); natural-language samples escape it, requiring manual review or allowlist-style checks.

### 4.8 CLAUDE.md lesson categories (which stem from unclear architecture)

**A. Unclear architecture / layering (redesign should eliminate structurally)**
| Lesson (summary) | Root cause |
| --- | --- |
| Parallel agents: new types need domain prefixes or master hits naming collisions; stale rebases overwrite half the repository | Single target/namespace + huge hotspot files |
| NavigationStack root `.task` reruns and overwrites a new selection; mixed view/value NavigationLinks insert pages underneath | No central state/navigation router; logic tied to view lifecycle |
| Attach environment values to NavigationStack itself | Implicit dependency injection through environment closures |
| Repeated row body evaluation → RowMemo/TextMemo warming; unobserved scroll bookkeeping; derived rows cached by version | Derived data computed in Views, no presentation layer |
| Release type-check timeout (long modifier chain) | Huge View |
| Machine-page counts incorrectly include assistant sessions | Selectors / domain rules scattered across pages |
| Wait for first layout before takeover subscription (build 21 spins forever) | Protocol state machine (TerminalFeed) coupled to layout |
| Pure enums in `ios/Shared` need nonisolated | Shared code inherits App MainActor default |
| xcodebuild test launches App and requests notification permission | App-hosted tests, no independently testable logic library |
| DEBUG launch arguments cannot match Swift identifiers | Many launch arguments drive tests because logic is hard to unit-test |
| `has_more` trustworthy only at tail; freeze cannot truncate by row id; latest page must detect a lost first push in new sessions | Complex sync semantics live only in ChatStore + Views; need independent cross-platform specification + fixtures |

**B. Platform / framework quirks (Android differs, but expect analogues)**: inverted-list / LazyVStack blank screens and drift, `scrollTo` affecting outer scroll, `ScrollPosition.scrollTo(y:)` resetting x, TapGesture consuming long press, no contextMenu on Section headers,
`onScrollPhaseChange` animating, MarkdownUI theme order, `.searchable` forcing navigation bars, hidden navigation bars disabling swipe-back, `TextField` selection bounds, UITextInput `hasText`, DragGesture missing fast double taps,
`UNUserNotificationCenterDelegate` async crashes, WatchConnectivity dropping preactivation pushes, `URLSessionWebSocketTask.sendPing` callback conditions, ActivityKit Date decoding, Swift Charts gaps, 12-hour time, `dict[k, default:]` failing to write back, `PrimitiveButtonStyle` stack overflow.
**Cross-platform design principles still apply**: exact jumps only to latest end, freeze new content while reading history, one lazy row per step, id-cached background-warmed derived data, Unix-second times, no error popups for cancellation / timeout / offline.

**C. Server / protocol / testing infrastructure** (client redesign inherits conclusions): herdr interactions, transcript parsing (queued_command, thinking narration, two image paths), node disk-read costs, APNs persistent connections, hub memory state, simulator / XCUITest environment problems.

---

## 5. Platform-independent logic (behavior Android must match)

> Line counts estimate type / function ranges (including comments); complexity is my assessment (low / medium / high). Unit tests refer only to `ios/PersonaTests`.
> Prerequisite facts: transcript JSONL → items, authoritative screen-dialog parsing, push policy, and AI sorting are in the Go server (`server/internal/transcript/`, `server/internal/screen/`, `server/internal/hub/`); **clients start at items**.

### A. Synchronization and data core

| # | Logic | Location | Lines | Complexity | Unit tests | Consistency notes |
| --- | --- | --- | --- | --- | --- | --- |
| A1 | Wire types and tolerant decoding (unknown enum fallback, Go zero time, nanosecond truncation, explicit CodingKeys, infer `state` from `status` if absent) | `Shared/Models.swift`, `DevModels.swift`, `InboxModels.swift:5-123`, `FleetState.swift:1-161`, `MachineMetrics.swift`, `Knowledge.swift`, `JSONValue.swift:83-127`, `Views/Inbox/Lark/LarkModels.swift`, `Views/Feishu/Docs/FeishuDocsModels.swift` (types) | ≈2,500 | Low | Yes (SyncEvent / FleetState / NotifyPrefs / Inbox / MachineMetrics / LarkRender / FeishuDocs / `goDates`) | Rules: `Models.swift:3-4, 78-88, 190, 294-310`; `JSONValue.swift:112-121`; `APIClient.swift:743-746` |
| A2 | WS envelopes `ServerEvent` / `TerminalEvent` | `Shared/LiveConnection.swift:4-133` | ≈130 | Low | Partial (`SyncEventTests`) | Align event table with hub (§4.6 drift) |
| A3 | Item continuity: `covered / history / Gap`, latest-page replacement, tail-only `hasMore`, repair (after 1.5s, at most 8 `after_seq` pages then reload), `since_seq` resubscription | `ChatStore.swift:248-264, 316-395, 500-524`; `AppModel.swift:382-452, 456-474` | ≈190 | **High** | About 12 (`ChatStoreTests.swift:218-303, 379-404`) + Go `continuity_corpus_test.go` simulation | Same chain diagnosed in `docs/archive/integrations/msg-render-plan.md`; export Go scenarios as JSON fixtures shared across three platforms |
| A4 | items → Rows (pair tools with results/images, merge reasoning) → Blocks (Turn: queued messages, interrupted endings, compaction not splitting, same-turn wakes, reopening after recap) → `pickAnswerAcrossWakes`; incremental `SettledRows` rebuild | `ChatStore.swift:1-246, 397-447, 490-498, 530-582` | ≈360 | **High** | Good: 8+ `ChatStoreTests` + `RebuildTests` (40-seed property tests, optional real corpus) | Incremental = full is invariant; Android repeats the same property test |
| A5 | Blocks → chat lines: message segmentation, multi-message turn bubbles, work rows (`WorkItem`/`WorkSummary`/timing), wake drawers, expanded steps, `Frozen` snapshots (truncate by `(seq, part)`) | `Views/Chat/ChatView.swift:42-47, 238-329, 425-436, 490-531, 593-608, 2133-2207, 2321-2350, 2426-2432` | ≈285 | **High** | **Almost none** (private `chatLines`; only id shapes and `prefixCount`) | Row-id rules (`w-`/`wt-`/`-h`/`-end`/`wk-`/`img-`/`mu-`) govern expansion and scroll anchors; must match |
| A6 | Disk-cache trimming (last two turns, no half rows, drop images above 6 MB) | `ChatStore.swift:786-837` | ≈50 | Low | Yes (2) | |
| A7 | Sending: `PendingMessage` state, ordered Outbox flush, `client_msg_id` idempotency, 409 `input_draft`/`dialog_open`/`delivered: unknown`, transcript echo matching (normalization + containment) | `Outbox.swift`, `SendSafety.swift:7-48`, `ChatStore.swift:839-870`, `AppModel.swift:605-671` | ≈250 | Medium-high | Partial (2 matching, 1 encoding); flush/transitions UI-only | Unify sending entries (§2.5) |
| A8 | Subscription reference counting, visible-session stack (suppress completion notices), ChatStore LRU (8, never evict pending sends) | `AppModel.swift:257-310, 370-380` | ≈65 | Medium | None | |
| A9 | Connection state machine: heartbeat / stalled / 45s dead detection / backoff, 4-state title + 1.5s debounce + catch-up count | `LiveConnection.swift:135-322`, `ConnectionStatus.swift` | ≈340 | Medium-high | Partial (`ConnectionStatusTests` pure function only) | Platform network APIs differ, but states/debounce must match |
| A10 | Background sync scheduling: `AgentSync` (rev comparison, 24h/40/concurrency 3), `LarkSync` (concurrency 2, 3s cooldown, dirty rescheduling, 20 on reconnect) | `App/AgentSync.swift`, `App/LarkSync.swift` | ≈250 | Medium | None | |
| A11 | Interaction answers: multi-select joining / Other override, screen `option`/`preKeys`/`keys`, resolve rollback (not for 410, guidance for `prompt_changed`) | `InteractionViews.swift:385-434, 700-705`; `AppModel.swift:587-603` | ≈70 | Medium | None (UI `SendSafetyTests`) | |

### B. Domain rules

| # | Logic | Location | Lines | Complexity | Unit tests | Consistency notes |
| --- | --- | --- | --- | --- | --- | --- |
| B1 | Agent state priority, list `rank` (Waiting / Muted), group `sortDate` (`state_since` for Working), space grouping, idle folding, peers (same-pane deduplication) | `Models.swift:135-147, 274-326`; `ChatsView.swift:3-49, 304-333`; `AppModel.swift:699-715` | ≈150 | Medium | **None** | Unify three orderings first (§4.3) |
| B2 | Pin / mute (Agent toggles override space, `allowed` exceptions) | `APIClient.swift:676-709`; `AppModel.swift:795-865` | ≈100 | Low-medium | 1 (`agentOverridesItsMutedSpace`) | Same as hub rules (`APIClient.swift:685` comment) |
| B3 | Inbox: triage fallback Needs you, type labels, AI twin drafts / working state, filtering/grouping, Watch Needs-you only | `InboxModels.swift:53-122`; `InboxView.swift:31-47, 95-101`; `InboxStore.swift:23-26`; `PersonaWatchApp.swift:65-70` | ≈60 | Low | Decoding only | |
| B4 | Approval policies and interaction wording (titles, plan titles, `(Recommended)` → localized Recommended) | `NotificationActions.swift`, `FleetActions.swift`, `ApproveFirstIntent.swift:36-50`, `InteractionViews.swift:4-27, 125-133`, `Models.swift:410-417` | ≈150 | Low (currently inconsistent) | Yes (NotificationActions / FleetIntents / Intents) | Define one policy table first |
| B5 | Live Activity / Dynamic Island: side cells, minimal state, wording, device-specific payload trimming | `FleetState.swift:142-364` | ≈220 | Medium | Good (12) | Android persistent notifications / Live Updates can reuse wording |
| B6 | Siri text and selection rules | `Intents/IntentSupport.swift:8-165` | ≈160 | Low | Good (14) | Android App Actions / shortcuts |
| B7 | Machine monitor thresholds, formatting, OS identification | `MachineMetrics.swift:30-54, 133-198` | ≈90 | Low | Yes (5) | Remove repeated View thresholds |
| B8 | Assistant schedule wording (weekdays, 24-hour time, merging upcoming runs) | `Views/Settings/AssistantProactiveViews.swift:9-126` | ≈120 | Low | Yes (4) | |
| B9 | Permission mode / reasoning-effort tables | `Views/Chat/SessionModes.swift:4-45` | ≈45 | Low | Indirect | Align with `node/modes.go` |
| B10 | Client prompt templates (Hand to Persona, Lark summaries, stop /loop, shortcut text) | `KnowledgeView.swift:465-474`, `LarkChatView.swift:418-428`, `AppModel.swift:914-917`, `ChatView.swift:1908-1914` | ≈60 | Low | None | [Assessment] Move to hub (one endpoint), avoiding duplicated prompts |

### C. Text / message-tag parsing

| # | Logic | Location | Lines | Unit tests |
| --- | --- | --- | --- | --- |
| C1 | `<persona-context>` encoding/decoding (XML escaping) | `Views/Link/LinkSheet.swift:11-52` | ≈40 | `contextTagRoundTrips` |
| C2 | `<attached_images>` / two legacy formats / `[Image #N]` → thumbnails + display text; `UserRowText` | `MessageViews.swift:134-171, 734-749` | ≈55 | `bubbleHidesTheImageNote` |
| C3 | Background-task wake summary (Chinese localization, failure detection) | `MessageViews.swift:484-503` | ≈20 | `wakeSummaryReadsInChinese` |
| C4 | File-path recognition (keep `~`, exclude URLs / model names) | `Views/Chat/FileViewer.swift:36-64` | ≈30 | `keepsTildeAndFindsPaths` |
| C5 | Web-link detection (`NSDataDetector`) | `MessageViews.swift:682-695` | ≈15 | None (Android Linkify / regex will differ) |
| C6 | Insert paths into composer (UTF-16 offsets, single-quote escaping) | `Views/Chat/FileAttach.swift:8-55` | ≈50 | Yes (6) |
| C7 | Watch plain text (strip Markdown / tags) | `Shared/WatchText.swift` | ≈40 | Yes |
| C8 | Tool presentation (icons / verbs / subjects, step summaries), diff line models, simplified `MarkdownHTML` | `ToolViews.swift:4-88, 139-148, 453-517`; `GitPane.swift:376-472` (`parse/split`); `FileViewer.swift:310-380` | ≈300 | `MarkdownHTMLTests` only |
| C9 | Formatting: relative times, shortened paths, model names, loop cadence, work duration, stats | `Theme.swift:320-360`, `Models.swift:221-250`, `ChatView.swift:2324-2350, 2426-2432`, `FleetText.short` | ≈100 | Partial |
| C10 | `ScreenParser`: spinner / activity lines / dialogs (multi-question tabs included) / numbered options; dialog parsing is old-hub fallback (hub has Go `server/internal/screen/dialog.go`) | `ChatStore.swift:585-784` | ≈200 | Yes (4 + 1) |

### D. Terminal (if Android also has a terminal page)

| # | Logic | Location | Lines | Complexity | Unit tests |
| --- | --- | --- | --- | --- | --- |
| D1 | VT subset + grid (two-stage wide-character writes, background-preserving erase, OSC 8), `rows(fromANSI:)`, `scrolled` detection | `Terminal/TerminalGrid.swift` | 419 | High | Yes (10 + recorded frames) |
| D2 | Phone-width wrapping / hanging indent / break rules / incremental layout | `Terminal/TerminalWrap.swift` | 141 | Medium | Yes |
| D3 | Frame ordering (epoch/seq, duplicate/gap), stream/poll switching, takeover state machine, scrollback, backoff | `Terminal/TerminalFeed.swift` | 351 | Medium-high | Partial |
| D4 | Key names → VT bytes, batching, modifier state machine, completion, serial sends | `Terminal/TerminalInput.swift:8-107`, `Chat/TerminalKeys.swift:1-70`, `Terminal/TerminalExtras.swift:282-320`, `SessionPanes.swift:683-696` | ≈230 | Medium | Yes |
| D5 | Box / block-character geometry, terminal palette | `Terminal/BoxDrawing.swift`, `Terminal/TerminalTheme.swift` | ≈205 | Low (data) | None |

### E. Pure Lark / document parsing (UI-heavy, but this part can be shared)

| # | Logic | Location | Lines | Unit tests |
| --- | --- | --- | --- | --- |
| E1 | Message-position comparison (decimal strings), unread anchors, on-screen-only read reporting, cursor paging | `LarkChatView.swift:3-18, 243-400` | ≈190 (mostly in View) | `LarkPosition` only |
| E2 | Card nodes / inline runs / palette / formatting / rich-text blocks / code highlighting / reaction toggles | `LarkCard.swift:6-48, 190-294, 440-474`; `LarkInline.swift:7-114, 231-305`; `LarkMessageContent.swift:217-296, 565-610`; `LarkChatView.swift:603-630` | ≈490 | Partial (`LarkRenderTests` 10) |
| E3 | Cloud documents: type recognition, Markdown export, block trees, table layout / sizing, inline styles/colors | `FeishuDocsModels.swift:66-151, 380-715`; `FeishuDocRenderer.swift:11-154` | ≈550 | Yes (12) |

### 5.1 Summary [Assessment]
- **A–D total about 5,100 lines** of pure logic, E about 1,100 more, wire types about 2,500. High-complexity areas with many real bugs: A3, A4, A5, A7, D1, D3—roughly half of CLAUDE.md's synchronization/rendering lessons land here.
- **Existing test assets**: A3/A4 (ChatStoreTests + RebuildTests + Go simulation), D1/D2 (recorded frames), B5/B6, E3 can become language-independent input JSON → expected JSON fixtures, serving as Android acceptance criteria (KMP or Kotlin rewrite).
- **Biggest gaps**: A5 (chat-line folding), B1 (ordering), A7 flush state machine, A10 lack unit tests. Extract and add property tests / fixtures on iOS before deciding KMP vs independent implementations; otherwise there is no basis for parity.
- **Need not be shared; platform-specific implementations**: list engine (inversion, freezing / landing scroll implementation), navigation, haptics, push (APNs / FCM), Live Activities (Android persistent notification), Watch (Wear OS scope undecided), phone tools (different Android sources: HealthKit→Health Connect, no direct Reminders equivalent),
  WebView pool, Markdown rendering, Lark card/document views, design system (whether to reproduce five styles undecided).
- **Input to KMP shared core vs independent implementations**:
  1) Candidates are pure functions / state machines with almost no platform APIs, modest size (~5k lines), but dense rules;
  2) Go test simulation is already a second implementation, showing one specification + multiple implementations + shared fixtures is an established viable approach here;
  3) iOS currently uses SwiftUI + `@Observable`; KMP replaces the core with Kotlin/Native plus Swift wrappers. ChatStore's per-event background incremental rebuild performance on K/N is **not measured**;
  4) Another route: **move item-only A4 turn / answer / wake grouping to hub** (Go already has transcript parsing and corpus tests), leaving clients expansion state and scrolling, reducing duplication across iOS / Android / Go tests.
     A5 depends on UI state (expansion, freeze, active work) and still belongs on clients.

---

## 6. Refactoring recommendations [Assessment]

### 6.1 Learn from the reference client, do not copy it

Reference-client approach [Fact]: local SPM package `Packages/<client kit>/Package.swift`, layered `*Contracts` (pure types / protocols) → `*Client` (transport, depends only on Contracts + TransportContracts) → `*Feature` (SwiftUI, Contracts + DesignSystem, **not Client**),
plus `SharedKit` (SharedCore / HTTPTransport / CredentialStore / ConnectionStorage), `DesignSystem`. For example, `AgentsFeature` depends on `AgentContracts`, `HerdrContracts`, `TerminalUI`, not `AgentBridgeClient`; App composition root injects implementations.

Borrow: ① separate pure types from transport; ② Features depend only on contracts for fake tests/previews; ③ package unit tests require no App host; ④ one target per Feature removes single-namespace collisions.

Do not copy:
1. The reference client connects a dozen Lark gateway domains, each with three targets (~30); Persona has one backend (hub), so **one Contracts target with domain directories** suffices, keeping total targets around 10.
2. Reference-client composition root makes 40+ journals/stores fields in the `App` struct of its app entry point, becoming another huge file; Persona should construct centrally in a normal `AppContainer` class and inject by Feature.
3. Reference-client terminal uses SwiftTerm; Persona has justified its own VT subset (`docs/integrations/terminal.md:58-78`). Keep it as independent `TerminalCore`, also the easiest part to port line-by-line to Kotlin.
4. Persona contracts have three consumers (Go hub, iOS, Android), so Contracts must provide **language-independent JSON fixtures / specifications** as well as Swift types, a need the reference client lacks.

### 6.2 Target modules

```
PersonaKit (local SPM package; Contracts / Core / TerminalCore do not import UIKit / SwiftUI, allowing swift test on macOS)
├─ PersonaContracts   Wire types, ServerEvent / client messages, Resolution, FleetState, Features; pure Codable, nonisolated
├─ PersonaCore        Pure logic: Rows/Blocks/Turn/ChatLines/Frozen, continuity reducer, Outbox reducer, ordering/grouping/peers/Inbox selectors,
│                    approval policy table, message-tag parsing, ScreenParser, Fleet / Intent text, formatting
├─ TerminalCore      VT grid, wrapping, scroll detection, key encoding, modifiers, frame-order / takeover state machine
├─ PersonaClient      HubHTTP (by domain), HubRealtime (LiveConnection + ConnectionMonitor), Persistence (StateCache / ChatCache /
│                    Outbox / Drafts / Keychain), MediaCache, sync engines (SessionSync / AgentSync / LarkSync)
├─ DesignSystem      Theme / AppStyle / StatusPalette / Components / Styling
├─ Features          Agents, Chat (list engine / header / Composer / work rows), Terminal, Assistant, Inbox (+ LarkUI / FeishuDocsUI / Mail),
│                    Interactions, Settings, Machines
└─ PlatformiOS       Push(APNs), LiveActivity, WatchConnectivity, PhoneTools, Intents, Haptics
App target           AppContainer (composition root), Router (tab / session opening / deep links), DebugHooks (DEBUG launch arguments)
```

Split `AppModel` into `SessionDirectory` (machines / sessions / indexes / pins / mutes / selectors), `ChatRepository` (ChatStore registry + subscription reference counts + repair scheduling),
`InteractionCenter` (pending + resolve + approval policy), `MessageSender` (sole send use case + Outbox), `TerminalStreams`, `AssistantService`, `PhoneToolHost`,
`ConnectionMonitor`, `Router`, `Feedback` (toast / haptics / error-display policy).

### 6.3 Sequence (each step mergeable independently, no wire-protocol change)

| Stage | Work | Why first |
| --- | --- | --- |
| 0 Safety net | ① Move `chatLines` and helpers **unchanged** into pure `ChatLinesBuilder` (blocks / expansion set / active turn / freeze point), called by ChatView; use `RebuildTests` random session generator for properties (expand/collapse preserves existing row ids, freeze preserves displayed rows). ② Extract `rank / ordered / SpaceGroup.build / stays / isWaiting` and Inbox grouping into selectors + unit tests. ③ Export JSON fixtures from ChatStoreTests / RebuildTests / Go simulation | A5, B1 are untested high-risk rules and prerequisites for Android parity |
| 1 Package extraction | Create local SPM package; `Models / DevModels / InboxModels / FleetState / MachineMetrics / Knowledge / JSONValue` → Contracts; pure ChatStore, ScreenParser, text parsing → Core; pure Terminal → TerminalCore; migrate unit tests; Watch / Widgets depend on package rather than compiling all `Shared/` | Names unchanged, `import` additions only, lowest risk; unit tests no longer launch App or occupy simulators |
| 2 Networking / sync | Split `APIClient` by domain and move View-file extensions; remove direct `model.api` calls (79), starting with logic-heavy `LarkChatView` and call-heavy `AssistantProactiveViews`; inject `AgentSync / LarkSync / InboxStore` dependencies, remove reverse `AppModel.shared`, add scheduling tests; stop `LiveConnection.send` silently dropping `tool_result` | Break cycles so synchronizers can be tested |
| 3 Split AppModel | Services as in 6.2; keep `AppModel` as forwarding facade first (Views unchanged), then move Features to direct service dependencies; **unify** sending, deep links, approval table, live/working detection, ordering | Resolve §4.3 inconsistencies together |
| 4 Split ChatView | Separate list-engine component (inversion / freeze / landing / follow), header, Composer, strips, DevInfoCard; move DEBUG probes into `DebugHooks`, keeping argument names | Biggest conflict hotspot; depends on earlier pure logic |
| 5 Cleanup | Remove dead state (`ChatStore.screen*`, `hookEvent`, `-terminalEngine`), fix documentation drift (§4.6), placeholder DEBUG samples; event consistency tests (hub emitted types ⊆ client-recognized ∪ explicitly ignored) | Final cleanup |

Constraints preserving current features:
- Keep disk paths / filenames (`chats-v2`, `state/*.json`, `chat-images`) and UserDefaults keys, avoiding blank first opens after upgrades; if changing, use new names and read old formats for one version.
- Keep DEBUG launch argument names (UI tests, `scripts/sim.sh`, Release `strings` checks depend on them); include new modules in `release.sh` checks.
- Move files in individual PRs, avoiding broad `Shared/` conflicts with other agents (CLAUDE.md rebase lesson).
- Each step: tests pin current behavior → move → tests stay green; unify inconsistent rules in separate commits explicitly describing behavior changes.

### 6.4 Deliverables for Android
- Client behavior specification (continuity, folding and row ids, ordering, sending / Outbox, approval policy, message tags, connection state), each rule citing fixtures.
- Cross-platform fixtures: item-batch sequences → expected items / rows / blocks / lines; recorded terminal frames (existing `ios/PersonaTests/Fixtures/terminal-*.jsonl`) → expected grids; message text → parsing results; FleetState → text.
- `scripts/mock/mock_hub.py` is a platform-independent mock hub (HTTP + WS), directly usable in Android UI tests.
- Hub work: FCM / Android device type (currently APNs only, `server/internal/hub/push.go:28-63`); platform-declared `register_phone` tool sets already supported; moving turn grouping to hub requires optional fields and old-client compatibility.

---

## 7. Unresolved / confirmation needed

- **No performance measurements**: synchronous ChatStore init disk/decode duration, RowMemo 20,000-entry memory usage, K/N incremental rebuild duration, actual unbounded `AttachedImageCache` growth.
- Inbox 90-second fallback polling in `docs/apple/ios.md:33`: additionally searched every `Task.sleep` and `90` literal in `ios/Persona/App` and `ios/Persona/Views/Inbox`; no such timer (Lark 60s fallback at `LarkChatView.swift:132-135`). Considered documentation drift unless hub-side.
- `session` / `ping` in `docs/protocol/protocol.md`: searched only `"type": "…"` literals in `server/internal/hub/*.go`, not node or other construction styles.
- Privacy scan: read only file selection and pattern portions of `scripts/privacy_check.py` (168-180, 69-79), did not run it; conclusions in §4.7.
- Lark message / card / document, mail, and settings View logic reviewed structurally, not line-by-line; E-group lines estimated from type ranges.
- Two cache overwrite races (AgentSync ↔ ChatStore, LarkSync ↔ LarkChatView) are inferences, not reproduced.
- Reference client: only `Package.swift` and composition-root beginning read for layering comparison (parallel research owns deeper evaluation).
- `docs/archive/integrations/msg-render-plan.md` not read (coordinator already read it); §2.3 and A3 / A5 in §5 concern the same diagnosed chain.
