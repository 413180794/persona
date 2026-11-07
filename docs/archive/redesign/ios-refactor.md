# iOS refactoring steps

> Draft v1 (2026-10-30), part of the [redesign plan](README.md). Basis: [research/persona-ios-arch.md](research/persona-ios-arch.md).
>
> **Not executed this round** (decided 2026-10-30: Android and hub only; iOS read-only). Kept for later iOS work. At that point, R0 need not export fixtures from iOS again: use `spec/fixtures` created on Android in this round.

## 1. Target structure

```
ios/
├─ project.yml                 App / Watch / Widgets / test targets (XcodeGen, retained)
├─ PersonaKit/                  Local SPM package (new)
│  ├─ Package.swift
│  ├─ Sources/
│  │  ├─ PersonaContracts/      Wire types, ServerEvent / client messages, Resolution, FleetState, MachineMetrics models, capability bits, error codes, narrow protocols
│  │  ├─ PersonaCore/           Pure logic (R0 / R1 lists in section 3)
│  │  ├─ TerminalCore/         TerminalGrid, TerminalWrap, key encoding, modifiers, frame order, takeover state machine
│  │  ├─ HubClient/            Domain-specific HTTP clients, Realtime (LiveConnection), ConnectionMonitor, error classification
│  │  ├─ PersonaData/           Secure storage, shared operation log (outbox / op / drafts), StateCache, ChatCache, media cache, sync engine, HubScope
│  │  ├─ DesignSystem/         Generated/Tokens.swift, Theme adapter, Styling, Components
│  │  ├─ ChatFeature/  AgentsFeature/  AssistantFeature/  InboxFeature/
│  │  ├─ TerminalFeature/  ReadersFeature/  MachinesFeature/  SettingsFeature/
│  │  └─ PlatformKit/          Push, Live Activity bridge, WatchConnectivity, phone tools, App Intents support, haptics
│  └─ Tests/                   One test target per module; swift test on macOS without launching the app
├─ Persona/                     App target: AppContainer (composition root), Router, DebugHooks, resources, Info.plist
├─ PersonaWatch/                Depends only on PersonaContracts + PersonaCore + part of HubClient + DesignSystem tokens
├─ PersonaWidgets/ LiveActivity/ Depends only on PersonaContracts (FleetState) + tokens
├─ PersonaTests/                Tests needing a host app (gradually emptied)
└─ PersonaUITests/              Unchanged
```

Dependency rules:

- `*Feature` depends only on `PersonaContracts`, `PersonaCore`, `DesignSystem`, `ReadersFeature` (shared readers), and necessary `TerminalCore`. **No `HubClient` / `PersonaData` dependencies**; services injected through narrow protocols.
- `PersonaContracts`, `PersonaCore`, `TerminalCore` import neither UIKit nor SwiftUI; types are `nonisolated` values or pure functions.
- Package targets explicitly set default isolation (Feature MainActor, Core nonisolated), replacing reliance on "whole app defaults MainActor, pure data manually marked nonisolated."

## 2. Constraints (at every step)

1. **Each step independently mergeable**, no wire-protocol change. Hub-dependent changes use capability bits, with old-hub behavior preserved.
2. **Do not change cache directory names** (`chats-v2`, `state/*.json`, `chat-images`, `lark-media`), **UserDefaults keys**, or **DEBUG launch arguments**. If necessary, use new names and read old formats for one version. New-module launch arguments also enter `release.sh` `strings` checks.
3. **Pin behavior before moving**: add tests for existing behavior first, retaining green tests after relocation. Unifying inconsistent rules is a separate commit explicitly describing behavior changes.
4. **Avoid parallel conflicts**:
   - move files in individual PRs; each touches only its own files;
   - only one lane touches `AppModel.swift` / `ChatView.swift` at a time in R3 / R4;
   - inspect `git diff origin/master --stat` before pushing.
5. **Every visible change** gets light / dark simulator screenshots (`docs/operations/testing.md`). Pure moves at least run UI tests and `-openSession` screenshot comparison.

## 3. Stages

### R0 Safety net (alongside Android specification)

| Work | Current location | Destination | Tests to add |
| --- | --- | --- | --- |
| Chat row folding: `chatLines`, `segments`, `workItems`, `photos`, `wakeLines`, `unfoldWork`, `prefixCount`, frozen `Frozen`, `ChatLine` / `WorkItem` / `WorkSummary` | `ChatView.swift` (private, about 285 lines) | `ChatLinesBuilder` (initially `ios/Shared`, then `PersonaCore` in R1) | Property tests using `RebuildTests` random-session generator: expansion / collapse preserves existing row IDs; shown rows unchanged across freezing; ID rules (`w-` / `wt-` / `-h` / `-end` / `wk-` / `img-` / `mu-`) |
| Agent sorting / grouping: `rank`, `ordered`, `stays` (duplicated), `SpaceGroup.build`, `isWaiting`, peer `siblings` | `ChatsView.swift`, `AppModel.swift` | `AgentListSelectors` | Every state combination order, idle folding, mute interactions |
| Inbox filtering / grouping, mixed "Agents waiting for you" | `InboxView.swift` | `InboxSelectors` | Sections, filters, Watch Needs-you-only |
| Question-answer rules (multiple-choice joining, Other override, immediate single-question single-choice submission), three screen-card answers | `InteractionViews.swift` | `InteractionAnswers` | All branches |
| Fixture export | `ChatStoreTests`, `RebuildTests`, recorded `TerminalWrapTests` frames, Go `continuity_corpus_test.go` | `spec/fixtures/{continuity,rows,lines,ranking,inbox,answers,terminal}/` | Tests read fixtures, one JSON per case, `-update` refreshes expected results |

After R0, Android uses `spec/fixtures` as acceptance criteria.

### R1 Create package and move code

- Create `ios/PersonaKit`, add local package dependency in `project.yml`.
- Move bottom-up by dependencies:
  1. `Models` / `DevModels` / `InboxModels` / `FleetState` / `MachineMetrics` / `Knowledge` / `JSONValue` to `PersonaContracts`, including `LiveConnection` event envelopes;
  2. pure `ChatStore` logic (Rows / Blocks / Turn / continuity reducer / `ScreenParser`), R0 selectors, text / tag parsing (`persona-context`, `attached_images`, paths, Watch plain text), formatting (`Fmt`, `IntentText`, `FleetText`, `AssistantScheduleText`) to `PersonaCore`;
  3. `TerminalGrid`, `TerminalWrap`, `TerminalKeyBytes`, `KeyBatcher`, `TerminalModifiers`, `TerminalCompletion`, `BoxDrawing` geometry to `TerminalCore`.
- **Type names unchanged**, callers only add `import`.
- Move unit tests to package test targets, no app launch or simulator (also eliminates "xcodebuild test launches app and prompts for notifications").
- Watch / widgets depend on packages, no longer compile all `Shared/` sources.

### R2 Networking and sync

- Split `APIClient` into small domain clients: sessions, interactions, machines, Files / Git, Inbox, Lark, email, assistant, Knowledge, devices, pin / mute. Move extensions scattered across 9 files to `HubClient`. Each implements its narrow protocol (`SessionsReading`, `MessageSending`, `InteractionResolving`…).
- Replace 79 direct `model.api` / `.shared.api` view uses with services; start with `LarkChatView` (most logic), `AssistantProactiveViews` (most call sites).
- Constructor-inject `AgentSync`, `LarkSync`, `InboxStore`, remove reverse `AppModel.shared` dependencies; add scheduling unit tests for concurrency limits, cooldowns, reconnect fetches.
- `LiveConnection.send` no longer silently discards while disconnected: `tool_result` uses HTTP fallback or queues until reconnect; `seen` already has HTTP fallback.
- Centralize errors in `HubClient`, classify by `code`, not text (`isTimeoutText`). Distinguish "Request not sent" from "Sent with no reply," prerequisite for D3.

### R3 Split `AppModel`

- Extract 11 services per [README.md](README.md) 4.2, placed in `AppContainer` and scoped through `HubScope`.
- Keep `AppModel` as a forwarding facade initially, without view changes; then migrate Features to services individually.
- **Unify during this stage**, each in a separate commit:

| Rule | Today | Unified |
| --- | --- | --- |
| State ordering | `AgentState.priority`, `ChatsView.rank`, Watch, `docs/protocol/agent-states.md` disagree | One `rank` (Needs you → Unread → Working → Waiting → Muted → Seen), used by lists, Watch, shortcuts; fix `docs/protocol/agent-states.md` |
| Live / working detection | Mixed `state.isLive`, `status.isLive`, `status != .offline`, `status == .working` | Only hub `AgentState`; raw herdr `status` only in info cards |
| Plan approval mode | 5 entry points, 3 values | One `ApprovalPolicy` table: in-app after reading full text can choose Auto-execute; notifications, Live Activities, shortcuts, Watch only Automatically accept edits (Siri / Watch currently allow all; explicitly note behavior change in commit) |
| Deep links / open session | `AppModel.open`, `InboxView.open`, `OpenSessionIntent` duplicated (last opens assistant chat in wrong tab) | One `Router.open(_:)` |
| Sending | 1 path through outbox, ≥5 direct | One `MessageSender` use case for every entry |
| Assistant-session exclusion | Duplicated in `MachinesView`, `MachineSpacesView`, `ChatsView` | One selector |
| Monitoring thresholds | `MetricAlert.of` plus literals in two views | Only `MetricAlert` |

### R4 Split `ChatView`

- Separate list engine (inverted, frozen, landing, bottom following) into `ChatList`, shared by Agents, assistant, Lark ([ux.md](ux.md) 3.5).
- Individual files for `ChromeHeader`, `Composer`, accessory bars, `DevInfoCard`, `TerminalCover`, `CameraPicker`.
- Move DEBUG probes (about 150-line `appeared`) into app-target `DebugHooks`, keeping launch-argument names.
- Also split `SessionPanes.swift` (terminal panel, accessory bar, `KeyStream`), `InteractionViews.swift` (four forms + DEBUG samples; replace possibly real work text with placeholders), `AssistantProactiveViews.swift` (one file per each of 4 pages).

### R5 New features and cleanup

- Implement [ux.md](ux.md): status rows, pending states, interaction upgrades, reading tools, drafts, Lark chat, pairing flow. Both clients share specifications; work through "Redesign additions" in [parity.md](parity.md).
- Design system: generated tokens, adoption guarded by grep baseline ([README.md](README.md) D7); component catalog `-openCatalog YES`.
- Storage: move outbox, drafts, reading position to Application Support, new filenames with one-time old-file reads; prefix paths with `hub_id / pairing generation`.
- Public direct-access pinning ([README.md](README.md) D9): replace three URLSessions with one pinned session. Retain Lark video, downloading locally before playback (AVPlayer uses its own networking stack without pinning, decided 2026-10-30).
- Remove dead code: `ChatStore.screen*` / `livePreview` / `spinnerLine`, empty `ServerEvent.hookEvent` branches, UI-test `-terminalEngine`, unused Info.plist microphone / speech-recognition descriptions.
- Fix documentation drift:
  - `docs/apple/ios.md`: 5-second banners, real-time status bar, pre-inverted-list implementation, 90-second Inbox polling;
  - `docs/server/architecture.md`: "Displayed as a real-time screen bar in the app," "Hooks immediately allow when no app is online";
  - `docs/protocol/protocol.md`: missing endpoints / events, remove unimplemented `session` / `ping` / `pong`.
- Consistency test: hub event types ⊆ recognized client types ∪ explicitly ignored types.

## 4. Validation

| Stage | Validation |
| --- | --- |
| R0 | New tests run first against unchanged code, proving existing behavior is pinned; all fixtures green on iOS |
| R1 | All package `swift test` green; App, Watch, widgets compile; UI tests green; typical `-openSession` screenshots unchanged |
| R2 | Sync scheduling tests; `ConnectionTests`, `SendSafetyTests` with mock disconnection, gaps, slow upload |
| R3 | Each unified rule separate commit + tests; approve plans from notification, Live Activity, Siri, Watch, verifying identical mode |
| R4 | Fold / freeze probes (`-showFoldProbe`, `-showFreezeProbe`) displacement is 0; batch cold-open sessions, detect blank screens by chat-area pixel standard deviation; Release archive type-checks |
| R5 | Each new feature reaches "Passed on real device" per parity.md tiers |
