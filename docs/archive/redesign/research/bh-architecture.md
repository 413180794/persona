# Architecture and engineering research on a reference app (for Persona's iOS refactor and new Android version)

> Subject: a reference app (read-only). Comparison: the current Persona worktree (read-only).
> Evidence format: `BH:<relative path>:<line>` refers to the reference repository; `JV:<relative path>:<line>` refers to Persona.
> Labels: **[Fact]** = directly read from code/docs; **[Assessment]** = my inference or recommendation.
> Privacy: no hostnames, IPs, accounts, personal launcher names, internal project names or real messages copied; any such information in the reference app's docs is omitted.
> Performance: this doc cites no unmeasured performance figures. MiB / counts below are hardcoded **limit settings** from code or docs, not measurements.

---

## 0. Size and overall shape [Fact]

- About 90K lines of Swift source (`Packages/ReferenceKit/Sources`), 66K test lines (173 files), an App shell of 1,235 lines (2 files), and a host component (Node) with 5.5K source + 7.1K test lines. The total matches "about 170K lines" (source + tests).
- One local Swift Package `ReferenceKit`: 39 targets + 1 test target (`BH:Packages/ReferenceKit/Package.swift:43-83`); 26 library products (lines 7-34 in the same file).
- The Xcode project has only 1 App target and 1 unit-test bundle, **no UI Test target** (productType counts in `ReferenceApp.xcodeproj/project.pbxproj`). UI acceptance uses simulators + synthetic fixture services + DEBUG launch arguments.
- Runtime shape: the phone connects directly to two backends—a Lark gateway (HTTP + WebSocket, upstream cannot be changed) and a Herdr host (SSH exec, one process per request with an NDJSON bridge + SSH PTY) (`BH:reference-client/docs/ARCHITECTURE.md:11-21`, `BH:reference-client/docs/ADR-001-SSH-EXEC.md:5-11`). **No always-on relay service**; this is the premise behind all its "heavyweight reliability designs".

---

## 1. Module layers and dependency direction

### 1.1 Actual layers (Package.swift is authoritative) [Fact]

| Layer | Examples (targets) | Dependency rule |
| --- | --- | --- |
| Kit / infrastructure | `SharedCore`, `ConnectionStorage`, `CredentialStore`, `HTTPTransport`, `MediaPlayback`, `MarkdownContent` | Foundation / system libraries only; `SharedCore` contains no business entities (`BH:reference-client/docs/ARCHITECTURE.md:107`) |
| Contracts | `LarkMessagingContracts`, `LarkPeopleContracts`, `LarkTransportContracts`, `HerdrContracts`, `AgentContracts`… | Pure value models + narrow protocols + error enums; no SwiftUI, storage or concrete transport libraries (`BH:reference-client/docs/ARCHITECTURE.md:45`). Contracts can depend on each other (e.g. Messaging→People/Resource) |
| Client (implementation) | `LarkMessagingClient`, `LarkPeopleClient`, `LarkAPITransport`, `SSHTransport`, `AgentBridgeClient` | Depends on Contracts + transport; owns DTO→domain model conversion and upstream compatibility (`BH:reference-client/docs/ARCHITECTURE.md:25,224`) |
| Feature | `MessagingFeature`, `PeopleFeature`, `AgentsFeature`, `ConnectionsFeature`, `SearchFeature`, `ApprovalFeature`, `WorkplaceFeature`, `TerminalUI`, `SSHKeyFeature` | **Only Contracts + DesignSystem + a few reusable Features; never any Client target** (checked every Feature dependency in `Package.swift:46-82`: no `*Client`/`SSHTransport`) |
| Shared UI | `DesignSystem` (colors/fonts/state views, no networking), `LarkResourceUI` (avatar/resource views) | |
| App (composition root) | `App/ReferenceApp.swift` | The only place importing both Client and Feature; creates real implementations and injects them (`BH:App/ReferenceApp.swift:1-25,281-311`) |

- Dependency direction: App → Feature → Contracts ← Client; Client/Feature → Kit. SwiftPM target dependencies enforce this at compile time; a Feature that tries to use an implementation class simply won't compile. [Fact]
- Feature→Feature dependencies exist, but are acyclic and only reuse UI capabilities: `MessagingFeature → PeopleFeature, SearchFeature`, `ApprovalFeature/SearchFeature → PeopleFeature`, `AgentsFeature → SSHKeyFeature, TerminalUI` (`Package.swift:46,61,78,81,82`). [Fact]

### 1.2 Why these boundaries (key points from the docs) [Fact]

- Business module families (LarkGatewayKit / HerdrKit / AgentKit) are **organizational boundaries**, "not big Client classes carrying every feature" (`BH:reference-client/docs/ARCHITECTURE.md:25`); "splitting requests across files while sharing one global business Store or database model isn't enough" (line 71).
- Each domain owns its sync rules, paging cursors, cache and failure recovery; account/connection switches cancel tasks in the old scope together (line 107).
- "Each Feature receives only the small interfaces it needs, not a LarkGatewayClient exposing every business method" (line 121).
- Runtime (Herdr) changes are separate from Agent (Claude/Codex) changes: Herdr API upgrades change runtime adaptation; Agent record-format changes affect individual adapters. **Record parsing and hook adaptation live on the host; iOS only consumes the normalized protocol** (lines 125-140).
- Pages don't scatter `if agent == claude` checks; they use declared capabilities to decide whether to show approvals/questions/stop (line 138).
- Start with one local package containing targets; split packages when independent releases/build isolation are needed. "Don't mechanically create three targets for every directory", and don't create empty implementations for future modules (lines 47,111,119).

### 1.3 Feature communication, injection and navigation [Fact]

- **Injection = constructor-injected protocol existentials**: models take `any LarkPeople`, `any LarkDoNotDisturbJournaling`, `isValid: @MainActor () -> Bool`, `onError: (Error) -> Void` (`BH:Packages/ReferenceKit/Sources/Features/PeopleFeature/LarkDoNotDisturbModel.swift:71-76`). No DI container, no TCA.
- **Optional capabilities detected by protocol downcasts**: `people as? any LarkDoNotDisturbReading / Managing` (lines 96,143 in that file); in the App, `workspace.people as? any LarkTranslationSettingsManaging` (`BH:App/ReferenceApp.swift:427`). One implementation class conforms to many narrow protocols: `SSHAgentBridgeClient` conforms to about 20 (`BH:.../AgentKit/BridgeClient/SSHAgentBridgeClient.swift:5`).
- **Cross-Feature communication**: (a) typed results/resource references, such as search result `LarkSearchSelection.person/chat/message(feedID,messageID,position)` (`BH:.../LarkGatewayKit/Search/Contracts/Search.swift:114-116`), resolved by the workspace before navigation; (b) App-injected async closures, such as Contacts' "send message" closure calling `openDirect → showConversation → selectedTab = .messages` (`BH:App/ReferenceApp.swift:255-259`), and a Workplace closure checking `canNavigate()` after async resolution (lines 266-270); (c) the App orchestrates cross-module flows (messages→tasks, etc.), without assuming cross-service transactions (`BH:reference-client/docs/ARCHITECTURE.md:113-117`).
- **Navigation**: the App holds top-level `selectedTab` (`ReferenceApp.swift:40,598`); each Feature uses `NavigationStack` + **value-based, typed route enums** (e.g. `AgentFleetNavigationRoute`, `BH:.../AgentsFeature/AgentFleetNavigation.swift:44-47`). Route state is a unit-testable value type; late restoration callbacks cannot resurrect a popped page (lines 50-80). Sheets use "Identifiable presentation structs wrapping model identity", and close automatically when scope identity changes (`ReferenceApp.swift:221-243,506-619`). Navigation persistence (last-opened session) is stored separately; restoration requires a **unique** full-identity match in the current session list (`BH:.../AgentsFeature/AgentNavigationStore.swift:5-30`).

### 1.4 Gaps between docs and code [Fact]

- `App/CompositionRoot.swift`, `App/Navigation/`, `AgentKit/AgentSession` in the documented tree don't exist; the composition root is the 644-line `ReferenceApp` struct (`BH:reference-client/docs/ARCHITECTURE.md:144-198` versus only 2 Swift files in the actual `App/`). The doc itself says "module descriptions are the target structure" (line 7).
- `HerdrRuntime` has only 10 lines and is imported nowhere (`BH:.../HerdrKit/HerdrRuntime/HerdrRuntimeClient.swift`); runtime logic has actually moved to the host bridge.
- Workspace model growth: `LarkMessagingModel.swift` is 3,846 lines, with about 38 init parameters and ≥26 child models; the App owns about 30 journals and passes them individually (`BH:App/ReferenceApp.swift:46-84,298-308`). Every child-model factory repeats `generation/owner` guards (`LarkMessagingModel.swift:287-449`).
- [Assessment] Package boundaries are clean, but "the workspace within one account scope" has become a new god object. When borrowing the layers, Persona should also design a scope container that lazily creates child models, avoiding the same growth.

---

## 2. Page state and concurrency model

### 2.1 Rules (docs) [Fact]

- SwiftUI pages use Observation models, updating state on MainActor; each feature owns loading/empty/error/draft/navigation state. Views don't create URLSession, SSH connections or database contexts directly (`BH:reference-client/docs/ARCHITECTURE.md:206`).
- Long-lived services own connections and sync, using actors when mutable state needs coordination. Tab switches don't destroy connections; leaving a page stops its subscription. Deleting a connection/switching targets explicitly cancels tasks (line 208).
- Single operations use async/await; continuous events use "AsyncSequence with a lifecycle and bounded buffers". Terminal bytes, message events and transcripts travel separately; high-frequency output cannot queue without bound (line 210).

### 2.2 Concrete code patterns [Fact]

- Counts: 127 `@MainActor @Observable` classes, 52 `public actor`s (rg over Sources). Journals, transports and stores are all actors.
- **Generation guards**: capture `revision`/`readID`/owner before await, then check `current(version)`, `Task.isCancelled`, `isValid()` before applying results (`LarkDoNotDisturbModel.swift:97-123,201`; `AgentWorkspaceModel.swift:2025-2054`; also at the transport layer in `LarkAPITransport.swift:30-40`).
- **Models own cancellable Tasks**: `readTask/writeTask` stored as properties, cancelled in `suspend()/stop()`; caller cancellation propagates via `withTaskCancellationHandler { try await task.value } onCancel: { task.cancel() }` (`LarkDoNotDisturbModel.swift:102-103,156-158,199-206`).
- **Coalesce refreshes while busy**: refreshes during a read/write only set `needsRead`; once finished, `scheduleRead()` runs again (lines 92,221-229).
- **Shared observation task (reference-counted subscribers)**: multiple pages' `observe()` calls share one polling Task, stopped only when the last subscriber cancels (`BH:.../AgentsFeature/AgentWorkspaceModel.swift:1933-1973`).
- **Bounded concurrency**: multi-host refresh runs at most 3 SSH tasks concurrently, filling a slot as soon as one opens (`BH:.../AgentsFeature/AgentFleetModel.swift:74-100`).
- **Lifecycle**: the App calls `pauseUpdates()` on `scenePhase == .background`, `resumeUpdates()` on return (`ReferenceApp.swift:186-191`); `.task(id:)` rebuilds runtime by connection-config identity (lines 183-185,281-311). Multi-host connection-identity diffing only stops old/creates new when config changes (`AgentFleetModel.swift:52-65`).
- **Bounded streams**: WebSocket events use `bufferingNewest(128)`, treating overflow as a network error (`BH:.../SharedKit/HTTPTransport/HTTPTransport.swift:217-229`); realtime changes use `bufferingNewest(32)`, sending `.connected` on overflow so consumers reread everything (`BH:.../LarkGatewayKit/Realtime/LarkRealtimeClient.swift:8-30`). The WebSocket receive loop starts before waiting for the first pong (`HTTPTransport.swift:220-246`, `BH:reference-client/docs/ARCHITECTURE.md:395`), matching Persona CLAUDE.md's lesson that "sendPing callbacks need receive running".
- **Intent + confirmation token, two-step submission**: `prepareSave()` creates a UUID-bearing confirmation and records revision and draft id; `process(confirmation)` dispatches only if id, revision and draft are still current—stale dialogs cannot submit (`LarkDoNotDisturbModel.swift:134-181`, tests `BH:.../Tests/ReferenceKitTests/DoNotDisturbModelTests.swift:28-34`).
- View-owned detail models use `@State private var model` (16 places), avoiding repeated load-task creation on parent redraw (`BH:reference-client/docs/ARCHITECTURE.md:579`); other pages use `@Bindable var model` (54 places).

---

## 3. Reliability patterns

### 3.1 Three outcomes: success / definitely not submitted / unknown; never auto-replay unknown outcomes [Fact]

- Principle: send state distinguishes at least "local preparation, submitting, confirmed, failed, awaiting outcome confirmation". Timeout ≠ failure; without server idempotency guarantees, reconcile history first, never blindly resend (`BH:reference-client/docs/ARCHITECTURE.md:259`; `BH:README.md:17,37`).
- Clients enforce this at the protocol layer: for a set of **side-effecting method names**, any transport error makes the bridge client throw `resultUnknown`; read-only methods throw the original error (`BH:.../AgentKit/BridgeClient/SSHAgentBridgeClient.swift:350-360`). Lark does the same: any transport error on a write → `LarkDoNotDisturbSubmissionError.resultUnknown` (`BH:.../People/Client/LarkDoNotDisturbClient.swift:30-32`).
- Only typed errors **provably occurring before dispatch** permit dropping a journal record as "not submitted" (`LarkDoNotDisturbJournal.swift:147-153`; model-side `isDefinitePreflight`, `LarkDoNotDisturbModel.swift:231`).
- Valid receipt but subsequent account verification fails → `identityUnverified(receipt:)`: keep the receipt, lock writes, require read-only reconciliation (`DoNotDisturb.swift:107-118`, `LarkDoNotDisturbClient.swift:39-43`).

### 3.2 Phone-side write-ahead journal / outbox [Fact]

- Order: **persist** pending state (operation ID, full target identity, draft version) before dispatch; persist a receipt as acknowledged before independently reading to confirm and finish. After restart, only reconcile read-only by the original ID; **no replay API** (`LarkDoNotDisturbJournal.swift:4-32,48-51`: "There is deliberately no retry/dispatch API").
- Agent side: `AgentLocalStateStore`—"One actor per app process. Pending operations are persisted before SSH dispatch. An unreadable journal must never be replaced with an empty one automatically." (`BH:.../AgentsFeature/AgentLocalStateStore.swift:106-108`). Only one unresolved operation per session; session creation is globally exclusive (lines 278-294). On completion, clear a draft only when both text and version match; manual resolutions retain the latest 50 history entries (lines 300-318).
- Drafts use UUID versions + monotonic write sequence (`latestWrites`) to prevent "late saves resurrecting drafts / old receipts clearing new input" (lines 267-277; `BH:reference-client/docs/ARCHITECTURE.md:323,325`).
- Deleted operation IDs leave tombstones (retired); late receipts cannot resurrect completed reminders (`LarkDoNotDisturbJournal.swift:137-138,168-177`).
- Send queue `LarkSendQueue` + `LarkOutgoingJournal`: phase = prepared/queued/failed/unknown + accepted + remoteID; records contain no attachment bytes. Reconcile only by original clientId / exact remote ID, "never guess by text, filename or time" (`BH:.../MessagingFeature/LarkOutgoingJournal.swift:23-59,85-87`; `BH:reference-client/docs/ARCHITECTURE.md:321`). Multi-image sends checkpoint every upload, then persist the "dispatch boundary" `postDispatchStarted` before final dispatch (lines 475-477).
- Size: 31 handwritten journal files, 4,088 lines total (`*Journal*.swift` count).

### 3.3 Host-side durable idempotency receipts (`once()`) [Fact]

- `BH:host/reference-bridge/src/controller.mjs:783-806`: create a receipt file by request ID using `wx` (exclusive creation), containing a `method+params` digest and `completed:false`.
  - Same ID, different digest → `COMMAND_ID_REUSED`; same ID, unfinished → `RESULT_UNKNOWN` ("will not execute again"); completed → return the original response or error unchanged.
  - Operations can write partial checkpoints while running; only allowlisted errors "provably not executed/explicitly rejected" are stored as completed errors. Other exceptions remain unfinished (= unknown).
- `command.status`: read-only receipt access, with partial evidence (e.g. rename applied, original process dead, interaction delivered) able to resolve unknown results (lines 581-631); "Never persist a successful receipt the client cannot validate" (lines 754-760). Files use temporary-file + rename writes (lines 777-781). Don't delete the receipt directory while outcomes await confirmation (`BH:README.md:143`).
- Every session operation binds to Herdr server / pane / native session / foreground process group; the host rechecks before execution and rejects a replaced pane (`BH:reference-client/docs/ADR-001-SSH-EXEC.md:11`; `BH:reference-client/docs/ARCHITECTURE.md:293`). The client also verifies session identity on every response with `hasSameActivityIdentity` (`BH:.../AgentContracts/AgentRuntime.swift:265-270`, `SSHAgentBridgeClient.swift:158-164`).

### 3.4 Scopes and generations [Fact]

- Each connection has a stable `ConnectionID`; changing address/identity increments `generation + 1`, and data from old generations cannot update the new connection's pages (`BH:reference-client/docs/ARCHITECTURE.md:228`; `BH:.../ConnectionsFeature/ConnectionModel.swift:67-77,86`).
- Credential reference = connection ID + address SHA-256 digest, automatically isolated on address changes (`BH:.../SharedKit/SharedCore/Connections.swift:55-73`); SSH credential routing length-prefix-encodes host/port/user/jump hosts (lines 133-137).
- Agent local-state scope = connection ID + host address/username + trusted fingerprint + Herdr server; session key = Agent type + native session ID (avoids cross-wiring reused panes) (`BH:reference-client/docs/ARCHITECTURE.md:325`; `AgentLocalStateStore.swift:139-143,207-209`).
- Transport: `HostCommandRunning.scopedCommands()` captures the "currently authenticated connection"; after reconnection (even to the same host), every request from the old scope is rejected (`BH:.../HerdrKit/HerdrContracts/HostTransport.swift:125-132`).
- The App injects `isValid` into each child model, bound to "workspace still ready and current account still the owner at creation" (`BH:App/ReferenceApp.swift:404-423,425-437`).

### 3.5 Error classification [Fact]

- General connection errors `ConnectionFailure`: network / certificate / untrustedCertificate(certificate details) / timeout / authentication / incompatibleService / server(HTTP code) / storage, each with actionable Chinese guidance (`Connections.swift:273-290`). URLError normalization happens in transport (`HTTPTransport.swift:137-147,180-191`).
- SSH connection errors `HostConnectionError`: nameResolutionFailed / connectionRefused / networkUnreachable / connectionTimedOut / authenticationFailed / hostTrustRequired / forwardingFailed / hopFailed(jump host|target) / terminalStartupTimedOut / timedOut(request)… (`HostTransport.swift:22-49`). Only classified **during connection establishment**; Happy Eyeballs prioritizes actual connection attempts rather than falsely reporting DNS failure in another address family. **No matching arbitrary server strings, no raw underlying text shown to users** (`BH:.../SSHTransport/SSHConnectionFailure.swift:11-53`; `BH:reference-client/docs/ARCHITECTURE.md:783-787`).
- **Connection timeout ≠ request timeout**: the latter may have executed a write and still requires unknown-result reconciliation (line 787; `HostTransport.swift:38`).
- Business errors: `LarkServiceError` (ownerRequired / sessionExpired / invalidResponse / untrustedResource / credentialsUnavailable / server(status,code,message)), mapped by server error codes, not wording (`BH:.../Transport/Contracts/LarkRequesting.swift:82-95`; `LarkAPITransport.swift:46-52`). Agent: `incompatible / invalidResponse / resultUnknown / rejected(code,message)` (`AgentRuntime.swift:300-311`).
- State isn't one isConnected: transport, authentication, service readiness and business sync are separate (`BH:reference-client/docs/ARCHITECTURE.md:235-237`). Host workspace `HostWorkspacePhase = needsKey / connecting / needsTrust(identity) / needsComponent / terminalOnly / ready / failed` (`AgentWorkspaceModel.swift:9`); connection progress `HostConnectionStage` "comes only from actual transport/bridge work, never elapsed-time estimates" (`BH:.../HerdrContracts/HostConnectionStage.swift:3-6`).

### 3.6 Capability negotiation and compatibility [Fact]

- hello returns protocol major version, bridge/Herdr/Claude versions and capabilities. Major mismatch → `incompatible` ("update the bridge"); a missing individual capability only disables its feature, not pretending the network is down (`BH:reference-client/docs/ARCHITECTURE.md:297-309`; `SSHAgentBridgeClient.swift:78-86`; host `controller.mjs:191-198`).
- Three capability layers: host (`hello.capabilities`), session (`AgentDescriptor.capabilities`, new capabilities are optional Bools, `AgentRuntime.swift:3-18`), **client declarations** (reads carry `interactionKinds`, `childInteractions`, `terminalQuestions`… so old clients don't receive new card types they can't decode, `SSHAgentBridgeClient.swift:158-164`, `BH:reference-client/docs/ARCHITECTURE.md:662`).
- Writes without required capabilities are rejected before dispatch with "Update the host component; your draft is saved" (`SSHAgentBridgeClient.swift:192-226`). Operations with new fields (e.g. a denial reason) check capability before journaling, preventing old hosts from silently dropping fields (`BH:reference-client/docs/ARCHITECTURE.md:813`).
- Local storage is versioned: new versions read all old versions; old Apps refuse to overwrite higher versions (`unsupportedVersion`). For example draft v8 reads v1–v7, send journal v12 reads v1–v11 (`BH:reference-client/docs/ARCHITECTURE.md:67`; `LarkOutgoingJournal.swift:109-114`; `AgentLocalStateStore.swift:159`).
- **Tolerant decoding**: optional presentation enhancements decode with `try?`; "Optional presentation metadata must not hide otherwise readable output" (`AgentRuntime.swift:80-112`). Unsupported records retain visible placeholders (`BH:reference-client/docs/ARCHITECTURE.md:281`).
- Cross-language contracts: the same fixed test vectors (queue-item digests) are asserted in JS host tests and Swift tests (`BH:host/reference-bridge/test/prompt-queue.test.mjs:51` and `BH:.../Tests/ReferenceKitTests/AgentPromptQueueTests.swift:53`); host interaction fixtures are named by CLI version (`host/reference-bridge/test/fixtures/*-2.1.3xx.json`).

---

## 4. Local storage [Fact]

| Data | Location | Details |
| --- | --- | --- |
| Lark gateway session Cookie, certificate trust, offline owner binding | Keychain | `KeychainSecretStore` actor, `kSecAttrAccessibleWhenUnlockedThisDeviceOnly`; reference names include connection ID + address digest. Connection deletion revokes by prefix; late writes after revocation fail immediately (`BH:.../CredentialStore/KeychainSecretStore.swift:5-60`). Offline owner records bind credential and certificate digests; either changing invalidates them (`LarkAPITransport.swift:151-205`) |
| SSH private keys/passphrases, host fingerprints (including jump hosts) | Keychain | `reference + ".host-key"`, etc. (`AgentWorkspaceModel.swift:416-429`); key library `SSHKeyLibrary` (`HerdrContracts/SSHKeyLibrary.swift:181`) |
| Connection config | `Application Support/ReferenceApp/connections.json` | Versioned envelope (1/2/3), atomic writes, `completeUntilFirstUserAuthentication` file protection; references credentials, contains no secrets (`BH:.../ConnectionStorage/JSONConnectionRepository.swift:4-27`) |
| Journals, drafts, reading positions, navigation | `Application Support/ReferenceApp/*.json` | One actor each; atomic writes, directories 0700/files 0600, iOS file protection, size/count limits. Queue storage also fsyncs files and directories, detects external changes by digest, and uses lstat to reject symlinks and others' files (`AgentLocalStateStore.swift:197-263`; `LarkDoNotDisturbJournal.swift:77-120`) |
| Message/media cache | `Application Support/ReferenceApp/MessageCache`, `MediaCache` | Namespace = connection ID + service address (`ReferenceApp.swift:292-302`); persists only when server Cache-Control explicitly permits it (`LarkRequesting.swift:21-62`) |
| Draft images | Separate directory + manifest + SHA-256 | Write in a temporary directory, then atomic rename. One-time startup GC only deletes provably unreferenced files; if any reference record is unreadable, skip all cleanup (`BH:reference-client/docs/ARCHITECTURE.md:471,479`) |
| Host side | `~/.local/state/reference-app/commands` (receipts), queue SQLite (WAL, synchronous=FULL) | `BH:README.md:143`; `BH:reference-client/docs/HERDR_PROMPT_QUEUE.md` ("Host state and atomic boundaries") |

- No SwiftData/Core Data: docs list SwiftData as a candidate, but everything is actually JSON files + actors (`BH:reference-client/docs/ARCHITECTURE.md:315`).
- Connection deletion is two-phase: persist "credential scope pending cleanup" in config first, then delete from Keychain; retry on next launch after failure (`ConnectionModel.swift:136-177`; `Connections.swift:204,244-259`).
- Isolation by connection and account: message caches at least by ConnectionID, account, conversation; Agent caches by host connection and native session. Account switches/deletion/logout clean explicitly scoped data (`BH:reference-client/docs/ARCHITECTURE.md:317`).

---

## 5. Logging and recovery [Fact]

- Documented design: each connection controls its own reconnect/backoff/cancellation/reconciliation; user-initiated disconnects don't auto-reconnect. Successful reconnection **doesn't automatically resend input or approvals**; network changes only trigger checks (`BH:reference-client/docs/ARCHITECTURE.md:329`). Logs should include module, connection ID, request ID, phase, duration and error class, not passwords/private keys/Cookies/bodies (line 331).
- Actual code: Sources + App contain only one DEBUG-only `Logger` (authentication path, `HTTPTransport.swift:127-135`). **Structured logging is largely unimplemented**.
- Implemented recovery: realtime stream exponential backoff 1→30 seconds (`LarkRealtimeClient.swift:10-28`); reread authoritative state after reconnection rather than replaying events (`BH:reference-client/docs/ARCHITECTURE.md:257`); recheck loaded thread messages in batches after reconnection (32 per batch, concurrency 4, line 393); "Check the last operation's result" entry + manual resolution (`BH:README.md:145-147,157`).
- Privacy: account-bearing value types override `description/debugDescription/customMirror` with `<redacted>` (`DoNotDisturb.swift:55-57,81-83`, 30 places across 11 files in Sources).
- [Comparison] Persona's logging is more complete: every hub response has `X-Request-Id`; the App can send `X-Client-Request-Id`; the hub only logs failures and slow requests, without query/body/token; App 5xx notices include the request ID (`JV:docs/server/server.md:95-97`). Logging isn't something to copy from the reference app; only its documented field convention—module, connection/scope ID, request ID, phase, duration, error class—is worth borrowing for local client diagnostic logs.
- Push (host directly to APNs, optional): payloads contain only generic text + random subscription/event IDs, no hostname/project/session ID/body. Opening fetches details over an authenticated channel and verifies identity; "done" events undergo read-only verification first (Herdr's summary state may report a session awaiting a question as done) (`BH:reference-client/docs/HERDR_PUSH.md:5-9,72-78`).

---

## 6. Patterns for adding features [Fact]

- Docs table (`BH:reference-client/docs/ARCHITECTURE.md:335-343`): new business = new Feature + domain Contracts + capability Client, reusing auth/transport/realtime rather than adding to MessagingClient; new Agent = new host bridge adapter + protocol fixture + any necessary dedicated UI; multi-host/multi-account = extend config lists and cache selection (scoping exists from version one); system notifications = new system adapter; changing SSH/terminal libraries = replace an adapter. "Add concrete adapters only for real needs", static composition, no dynamic plugins (line 343).
- The actual "standard capability slice" (temporary Do Not Disturb, about 800 lines total):
  1. Contracts: validated `Mutation`, `Snapshot`, `Receipt`, narrow `Reading/Managing` protocols, domain errors + submission errors (`.../People/Contracts/DoNotDisturb.swift:5-118`);
  2. Client: `extension LarkPeopleClient: LarkDoNotDisturbManaging`, reread/compare before write (sourceChanged) → verify account → write → validate receipt → verify account again (`.../People/Client/LarkDoNotDisturbClient.swift:21-44`);
  3. Journal actor (`.../PeopleFeature/LarkDoNotDisturbJournal.swift`);
  4. `@MainActor @Observable` Model (`LarkDoNotDisturbModel.swift`) + View;
  5. App/Workspace injection;
  6. Four test sets: ClientTests / JournalTests / ModelTests / WorkspaceTests;
  7. Fixture-service module (`scripts/fixtures/do-not-disturb.mjs`) + upstream oracle check (`scripts/check-do-not-disturb-fixture.mjs`);
  8. Docs + VALIDATION record + alignment checklist.
- Process: complete one vertical flow before adding the next API set (`BH:reference-client/docs/ARCHITECTURE.md:224,347-354`); validate M0 risks before investing in UI (`BH:reference-client/docs/PHASE1_PLAN.md` ("Development milestones")); scripts generate "upstream API → UI call" mappings from source, with manual annotations—unannotated means unchecked, not unnecessary (`BH:reference-client/docs/audits/API_UI_MAP.md:1-9`, `scripts/audit-*.mjs` (gateway API audits)). Integration records separate "real environment / automation / isolated simulator" evidence; simulator success doesn't imply device success (`BH:reference-client/docs/VALIDATION.md`, each section; `BH:README.md:157,167`).

---

## 7. Test structure [Fact]

- All Swift Testing (`@Test`/`@Suite`, about 4,162 `@Test`s); one test target depends on almost every module (`Package.swift:83`).
- By suffix (files / lines): `*ClientTests` 31 / 9,013 (protocol, DTO, upstream error mapping, fake transport); `*JournalTests` 12 / 1,806 (persistence, versions, corruption, tombstones); `*ModelTests` 23 / 7,073 (page-model state machines, cancellation, late receipts); `*WorkspaceTests` 19 / 4,267 (lifecycle/account switching after models join a workspace); `*PresentationTests` 6 / 2,164 (pure presentation projections); `*QueueTests` 4 / 3,488 (send/scheduled queues); about 78 others named by feature (`AgentSessionTests`, 10,216 lines, and `LarkMessagingTests`, 6,471 lines, are largest).
- Testability comes from Models depending only on Contracts: tests use **actor fakes** with hold/release gates (`CheckedContinuation`) to deterministically produce interleavings like "switch accounts mid-read / late receipt / cancel during write / applied but returns 503", explicitly "without timers or scheduler guesses" (`BH:.../Tests/ReferenceKitTests/DoNotDisturbModelTests.swift:241-300`). Each rig uses a temporary directory with a real journal (lines 241-249).
- Real-protocol integration tests: an in-process NIOSSH loopback server covers host fingerprints before auth, UTF-8, cancellation/timeout isolation, paused PTY handshakes (`BH:.../Tests/ReferenceKitTests/SSHIntegrationTests.swift:21-60`).
- Host component: `node --test`, 62 files (`host/reference-bridge/test`).
- **Test seams (confirmed by further reading)**:
  1. Contracts seam: Model tests inject actor fakes implementing narrow protocols (`DNDModelAPI: LarkPeople, LarkDoNotDisturbManaging` in `DoNotDisturbModelTests.swift:251`).
  2. Transport seam: Client/Workspace tests use **real narrow clients + real journals**, replacing only the bottom `LarkRequesting` with a fake transport, and differing `accountNamespace`s verify scope isolation (`DoNotDisturbWorkspaceTests.swift:13-23`: "Uses the real narrow client and durable journal"). Same for Agents: fake `HostCommandRunning` beneath a real `SSHAgentBridgeClient` (`FixtureHost`/`ReceiptFixtureHost`/`InteractionFixtureHost` in `AgentSessionTests.swift:623,653,1368,3002`, etc.) tests receipts, unknown outcomes and identity verification.
  3. Real-protocol seam: in-process NIOSSH loopback server (above).
  4. Host: `node --test` + real SQLite + process SIGKILL (`reference-client/docs/HERDR_PROMPT_QUEUE.md` ("Verification and next steps")).
- [Assessment] These "three-layer seams" fundamentally make Features testable: models know no concrete classes, clients know nothing beyond networking libraries, and replacing the lowest transport runs the entire business chain.
- Simulator QA: synthetic services in `scripts/fixtures/*.mjs` (with `/__fixture/...` controls, request and "blocked write" counts); `scripts/check-*-upstream.mts` directly imports pinned upstream route/domain code as an oracle. DEBUG launch arguments inject fake recorders, fake Agent hosts and one-time dropped receipts (`BH:App/DebugAgentFixture.swift:9-40`), effective only on simulators + loopback addresses.

---

## 8. Persona today (only facts directly relevant above)

- One App target + `Shared/`, 41.8K Swift lines; `SWIFT_DEFAULT_ACTOR_ISOLATION: MainActor` (`JV:ios/project.yml`). Global singleton `AppModel.shared` owns machines, sessions, interactions, per-session ChatStores, drafts and outbox (`JV:ios/Persona/App/AppModel.swift:8-70`, 998 lines); `APIClient` is one 755-line class (`JV:ios/Shared/APIClient.swift`).
- Outbox / drafts / snapshots are written through `StateCache` in **Caches**, swallowing errors with `try?`, detached writes, treating decode failures as empty, globally keyed without hub isolation (`JV:ios/Shared/StateCache.swift:3-24`; `JV:ios/Persona/App/Outbox.swift:73-93`; `JV:ios/Persona/App/SendSafety.swift:49-62`). About 139 `UserDefaults.standard` references too.
- Sending: every pending message has `client_msg_id`; no network/timeouts (including `networkConnectionLost`) become waiting and **automatically resend** on reconnection (`JV:ios/Persona/App/SendSafety.swift:23-44`, `Outbox.swift:26-71`). Delivery detection uses fuzzy matching of normalized transcript text (`JV:ios/Shared/ChatStore.swift:840-875`).
- node dedup `sentLog` is **in-process memory, 10-minute TTL, no parameter-digest validation** (`JV:server/internal/node/sendsafe.go:126-170`), while hub/node are frequently redeployed (`JV:CLAUDE.md` ("Lessons"): deployment can clear hub memory at any time).
- Capability negotiation is one-way: hub reports `features` in health / WS hello; the App checks field presence (`JV:docs/protocol/protocol.md:16-19`).
- Some timeout detection relies on error text (`APIError.isTimeoutText(api.message)`, `JV:ios/Shared/APIClient.swift:39,646-652`).
- Already right: one-shot operations such as approvals/answers aren't queued (`JV:docs/apple/ios.md:292`); hub pushes only rev, App fetches increments by after_seq (`JV:CLAUDE.md`, lessons); WebSocket heartbeat/weak-network detection; mock hub records send counts (`JV:scripts/mock/mock_hub.py:538,797-799`).
- iOS tests total about 4.7K lines (PersonaTests + PersonaUITests); models depend on the singleton, making page-model interleaving tests like the reference app's difficult.

---

## 9. A) Strong recommendations for Persona (all [Assessment], evidence in parentheses)

Each item: what → reference-app evidence → what it solves in Persona → Android equivalent.

**A1. Compile-time enforced layers: Contracts / Client / Feature / DesignSystem / App composition root**
- What: domain models and narrow protocols in pure Contracts modules; implementations (HTTP/WS, DTO mapping) in Client; page models + views in Feature, which depends only on Contracts. Only App sees both implementation and pages and injects dependencies. Cross-Feature communication uses typed references or App-injected closures.
- Reference app: `Package.swift:43-83`; `reference-client/docs/ARCHITECTURE.md:45,109-121`; `App/ReferenceApp.swift:255-270`.
- Solves in Persona: Views and logic currently hang off `AppModel.shared` and the 755-line `APIClient`. Page models can't be tested independently, and Android has no list of required interfaces. Contracts become the shared iOS/Android interface specification.
- Suggested granularity: don't copy all 39 targets; 8–10 modules suffice for Persona (Core contracts, Hub client, storage, DesignSystem, Agents, Assistant, Inbox, Terminal, Machines, Settings).
- Android: Gradle modules; pure Kotlin/JVM `:core:contracts` (no Android dependency, direct JVM tests), `:core:hub` (Ktor/OkHttp + kotlinx.serialization), `:feature:*` only `implementation(project(":core:contracts"))`, `:app` composition (Hilt or handwritten root). Navigation Compose typed routes (`@Serializable` route classes) correspond to value-based route enums, also avoiding Persona CLAUDE.md's mixed view/value NavigationLink pitfall.

**A2. Narrow capability protocols + runtime capability detection**
- What: not an omnipotent client, but small protocols such as `SessionsReading`, `MessageSending`, `InteractionResolving`, all implemented by one class. Detect optional capabilities with `as? any XManaging`; models decide which buttons appear.
- Reference app: `SSHAgentBridgeClient.swift:5`; `LarkDoNotDisturbModel.swift:96,143`; `ReferenceApp.swift:427`.
- Solves in Persona: breaks up `APIClient`; each page-model fake needs only 2–3 methods, keeping tests focused. Hub-version support fits the same mechanism (implementation conforms/returns unsupported according to hub features).
- Android: Kotlin `interface`s + one `HubClient` implementing several; DI provides interface types.

**A3. Unified writes: three outcomes + write-ahead journal + read-only reconciliation, with one generic journal**
- What: side-effecting writes (send, create/interrupt/end session, change mode, answer approvals, send Lark/email replies, hand to AI) all have client-generated op ids. Persist pending (op id, full target identity, payload digest, draft version) before dispatch; persist receipts before clearing. Transport errors are always unknown; recovery queries only by op id; users can "Dismiss reminder" while keeping history.
- Reference app: `LarkDoNotDisturbJournal.swift:4-51,147-177`; `AgentLocalStateStore.swift:106-108,278-318`; `SSHAgentBridgeClient.swift:350-360`; `reference-client/docs/ARCHITECTURE.md:259,321-325`.
- Solves in Persona: outbox is in Caches (system-purgeable), decode failure means empty, network interruptions auto-resend, delivery is fuzzy-text matched. Checked writes like interrupt/mode switches only show an error on failure (`JV:ios/Persona/App/AppModel.swift:717-739`), without unified unknown semantics (Inbox replies, email/Lark sends, session creation weren't individually checked). Use one generic `OperationJournal<Op>` (31 handwritten journals, 4,088 lines in the reference app, are exactly what to avoid).
- Android: Room `pending_ops(id, scope, kind, target_json, payload_digest, phase, created_at)` or `AtomicFile` in `filesDir`; pages observe Flow. **Don't** use WorkManager to auto-replay unknown operations; schedule retries only for waiting items definitely not sent.

**A4. Durable server idempotency receipts (the reference app's `once()`), allowing safe automatic retries**
- What: atomically create receipts by op id, with method+params digest; reject same id/different digest; unfinished returns unknown and never executes again; completed returns the original result. Only provably unexecuted errors become completed errors. Provide read-only `status(op id)` that can close cases using partial-progress evidence.
- Reference app: `host/reference-bridge/src/controller.mjs:581-631,754-806`; `reference-client/docs/ADR-001-SSH-EXEC.md:11`.
- Solves in Persona: node `sentLog` is memory-only, 10 minutes, no digest; retries after deployment may type the same message twice. With an always-on hub + Postgres, Persona can store receipts in the database (node persists locally too) and add `GET /api/v1/ops/{id}`. **Persona has an advantage here**: the reference app cannot change upstream and must avoid resending unknowns, asking people to reconcile; Persona can establish real server idempotency, letting clients auto-retry unknowns with the same op id for a better, safer experience.
- Android: same protocol, no extra design.

**A5. Scopes + generations across credentials, caches, drafts, journals and async results**
- What: stable connection ID; address/identity change increments generation. Credential reference = ID + address digest; partition all local data by connection → account → session. Validate generation/owner before applying async results; transport `scopedCommands()` rejects stale requests across reconnects; child models receive `isValid`.
- Reference app: `Connections.swift:55-73,133-137`; `ConnectionModel.swift:67-86`; `HostTransport.swift:125-132`; `ReferenceApp.swift:404-437`; `reference-client/docs/ARCHITECTURE.md:228,317`.
- Solves in Persona: `StateCache` keys are global ("dev", "outbox", "drafts"), so pairing to another hub or future multi-hub/device credentials can mix data; old snapshots can masquerade as new data after hub reinstallation. Have hubs generate an instance UUID (`hub_id`), prefix all local data with `hub_id + pairing generation`, and discard old-scope tasks/caches on pairing changes.
- Android: same scope prefix; long-running tasks outside `viewModelScope` use a scope object's `CoroutineScope`, cancelled on scope changes.

**A6. One page-model concurrency standard**
- What: `@MainActor @Observable final class` page models + actor services; capture revision before await and validate on return; models own/cancel Tasks; coalesce busy refreshes into one follow-up read; shared observation task with reference counts; bounded concurrency; background pauses; bounded streams + full reread on overflow.
- Reference app: `LarkDoNotDisturbModel.swift:90-123,199-229`; `AgentWorkspaceModel.swift:1933-1973,2025-2054`; `AgentFleetModel.swift:52-100`; `LarkRealtimeClient.swift:8-30`; `reference-client/docs/ARCHITECTURE.md:204-210`.
- Solves in Persona: scattered lessons already exist (many CLAUDE.md pitfalls about late results overwriting new state and `.task` rerunning on return). What's missing is a standard in templates; encode it as base/helper types (`Revision`, `CoalescedRefresh`, `SharedObservation`).
- Android: `ViewModel` + `StateFlow`; `Job` fields + `cancel()`; `Mutex`/`limitedParallelism(1)` instead of actors; `conflate()` / `buffer(n, DROP_OLDEST)` instead of `bufferingNewest`; `repeatOnLifecycle(STARTED)` instead of scenePhase; same revision guards.

**A7. Two-step submission: intent + confirmation token**
- What: `prepare()` creates a UUID confirmation capturing revision/draft id; `process(confirmation)` doesn't dispatch if either changed. Stale dialogs/notification buttons cannot submit expired decisions.
- Reference app: `LarkDoNotDisturbModel.swift:134-198`; tests `DoNotDisturbModelTests.swift:28-34`.
- Solves in Persona: approvals/plans, "Clear and send" (`SendSafety.swift` sendRefusal), deleting queued messages and notification shortcuts all involve confirmation; state can change while the dialog is open (handled on the computer, draft changed).
- Android: same token in ViewModel; Compose dialogs only hold the token.

**A8. Bidirectional capability negotiation + versioning**
- What: hello carries protocol major version (incompatibility explicitly requests upgrade rather than appearing as a network error), component versions/capabilities; host/session capabilities are separate. **Clients declare understood types in requests**, preventing undecodable new cards from reaching old clients. Missing capability only disables its feature, preserving drafts and requesting upgrade.
- Reference app: `AgentRuntime.swift:3-18,38-47`; `SSHAgentBridgeClient.swift:78-86,158-164,192-226`; `reference-client/docs/ARCHITECTURE.md:297-309,662,813`.
- Solves in Persona: currently only hub→App features exist. With iOS and Android each running multiple versions, the hub needs peer capabilities (new interactions, message blocks, tool cards) to degrade gracefully (e.g. render a new interaction as "Handle in terminal" for old clients). Include `client_platform/client_version/client_features` in WS hello and HTTP headers.
- Android: same protocol.

**A9. Tolerant decoding + visible placeholders**
- What: a broken optional enhancement field is dropped alone (`try?`), not failing the whole message/page; unknown blocks retain visible placeholders and their raw text can be inspected.
- Reference app: `AgentRuntime.swift:80-112`; `reference-client/docs/ARCHITECTURE.md:281`.
- Solves in Persona: transcript outputs have increasingly many fields (tool results, tasks, images); one wrong Codable field type fails a whole page. The new Android client is more likely to meet new hub fields.
- Android: kotlinx.serialization `Json { ignoreUnknownKeys = true; coerceInputValues = true }`; receive enhancements as `JsonElement`, then `runCatching { decode }`.

**A10. Typed errors + multidimensional connection state + real milestones**
- What: normalize network errors to DNS/refused/unreachable/timeout/auth/certificate/incompatible; separate connection and request timeouts (writes may have executed for the latter); business errors use server codes, never text matching. Separate transport/auth/service-ready/sync states, each with actionable entries; report only real progress stages.
- Reference app: `Connections.swift:273-290`; `HTTPTransport.swift:137-147`; `HostTransport.swift:22-49`; `SSHConnectionFailure.swift:11-53`; `HostConnectionStage.swift:3-6`; `AgentWorkspaceModel.swift:9`; `reference-client/docs/ARCHITECTURE.md:235-237,783-787`.
- Solves in Persona: `isTimeoutText(api.message)` relies on wording. Standardize all hub errors as `{code, message, retryable, delivered}`; clients only use code. Beyond ConnectionStatus, add machine states (node offline/version mismatch/herdr not ready/Claude not logged in) and source states (Lark session expired/mail auth invalid).
- Android: `UnknownHostException`→DNS, `ConnectException`→refused, `NoRouteToHostException`→unreachable, `SocketTimeoutException`→timeout, `SSLHandshakeException`→certificate, HTTP 401/403→authentication, normalized into a sealed class.

**A11. Local persistence that fails safely**
- What: unreadable critical records (journals/drafts) keep the original file and block new writes, never automatically replaced with empty data. Versioned envelopes: new reads old, old refuses to overwrite new; atomic writes + file protection + 0600. Delete only provably unreferenced files; unreadable references abort GC.
- Reference app: `LarkOutgoingJournal.swift:61-72,85-87,109-114`; `AgentLocalStateStore.swift:62-73,144-263`; `reference-client/docs/ARCHITECTURE.md:67,323,479`.
- Solves in Persona: `StateCache` uses `try?` everywhere, no versions, stored in Caches. Move drafts/outbox to Application Support (discardable snapshot caches can stay in Caches).
- Android: `AtomicFile` in `filesDir` (critical) / `cacheDir` (discardable); Room transactions/migrations; envelope `version`.

**A12. Testability: four test sets + gated fakes + contract fixtures**
- What: Client / Journal / Model / Workspace tests per capability; actor fakes with continuation hold/release gates deterministically generate late results, cancellation, account switches and "applied but returns 503", without sleep. Real-protocol loopback tests; shared cross-language vectors.
- Reference app: section 7; `DoNotDisturbModelTests.swift:241-300`; `SSHIntegrationTests.swift:21-60`; `prompt-queue.test.mjs:51` ↔ `AgentPromptQueueTests.swift:53`.
- Solves in Persona: about 4.7K iOS test lines, singleton-bound models hard to test. Have hub Go tests produce golden JSON (contract fixtures in `testdata/contract/`), and Swift/Kotlin decode the same files as a hard cross-platform constraint. Add "applied but receipt dropped" fault switches to `mock_hub.py` (matching `DebugAgentReplyFault` and fixture `applied503` in the reference app).
- Android: `runTest` + `StandardTestDispatcher` from `kotlinx-coroutines-test`, fakes use `CompletableDeferred` gates; Gradle reads shared contract fixtures.

**A13. API → UI alignment checklist (generated + manually annotated)**
- What: enumerate server API entry points from source into a generated table, overlaid with maintained client-usage/scope annotations; unannotated = unchecked, not unnecessary.
- Reference app: `reference-client/docs/audits/API_UI_MAP.md:1-9`; `scripts/audit-*.mjs` (gateway API audits); `docs/audits/ios-api-mappings.json`.
- Solves in Persona: the biggest Android parity risk is "present in iOS, missing on Android". Generate a checklist from hub Go routes + WS event types, annotating iOS / Android usage and state as the Android acceptance table.
- Android: another column in the same table.

**A14. Redacted descriptions for sensitive values**
- What: account/token/body value types override `description/debugDescription/customMirror` with `<redacted>`, preventing accidental output in logs, assertion failures and crash reports.
- Reference app: `DoNotDisturb.swift:55-57,81-83` (30 places across 11 source files).
- Solves in Persona: the strict no-intranet/personal-data rule and privacy scan should extend to runtime logs (hub tokens, device credentials, Lark/email bodies).
- Android: override data-class `toString()`.

**A15 (optional). Minimal push payloads + verified state transitions before notification**
- What: push only generic text + random ID, fetch details over auth on open and verify identity; read-only verification before "done" notifications prevents sessions still waiting on answers from appearing complete.
- Reference app: `reference-client/docs/HERDR_PUSH.md:5-9,72-78`.
- Solves in Persona: pushes contain session names and reply bodies (`JV:docs/apple/ios.md:258`), visible on the lock screen; offer a "Lock screen privacy" option. Whether completion verification already exists is pending in section 12.
- Android: FCM data messages likewise carry only IDs; fetch on open.

---

## 10. B) Overdesign in the reference app / poor fits for Persona ([Assessment])

Different premises: the reference phone directly connects to unchangeable upstream (Lark gateway) and user hosts (SSH), with no always-on relay. Persona's phone only connects to its own hub, always on with Postgres, responsible for push and storage; hub/node code is in the same repo and changeable.

- **B1. A second durable queue and manual reconciliation system on the phone**: the reference app's phone prompt outbox + host SQLite queue (queued/claimed/executing/confirming/submitted/cancelled/rejected, leases, tombstones, user-service supervision) (`reference-client/docs/HERDR_PROMPT_QUEUE.md`) exists because there's no relay. Persona only needs to "reliably hand messages to the hub" (database persistence = accepted); queue/delivery/retry/reconciliation belong to hub+node. Phone journals only cover the stage before hub acceptance.
- **B2. A handwritten journal per operation and one huge workspace model**: 31 journals, 4,088 lines; App owns about 30 journals; `LarkMessagingModel` has 3,846 lines and about 38 init parameters (`ReferenceApp.swift:46-84,298-308`). Persona settings writes (notification preferences, pins, modes) are hub-stored; reread after lost receipts, no journal needed. High-risk writes share one generic journal; scope containers lazily create child models.
- **B3. Read the account before and after every write + reread/compare before writes + upstream oracle tests**: needed because upstream lacks CAS, idempotency keys and atomic account binding (`reference-client/docs/ARCHITECTURE.md:430,486,511,527`). Persona can implement CAS (version / If-Match), idempotency keys and device-bound tokens in the hub, avoiding extra client round trips.
- **B4. SSH/PTY/key library/host fingerprints/jump hosts/vendored Citadel patches/host installer and version matrix**: Persona's phone doesn't use SSH (node connects to computers via SSH on the hub side), so none of this is needed. Pairing tokens and device credentials are its equivalents.
- **B5. Polling observation**: session list every 5 seconds, chat every 1 second, multi-host every 8 seconds + shared polling task (`AgentWorkspaceModel.swift:901-905,1933`; `AgentFleetModel.swift:67-72`), because SSH exec has no push channel (`reference-client/docs/ADR-001-SSH-EXEC.md:9`). Persona already has WebSocket + rev + after_seq increments; don't regress to polling. Only borrow "observe in foreground, share one subscription across pages".
- **B6. A strongly typed struct + dedicated view for every tool**: `AgentBlock` has over 20 optional `AgentRecorded*` structs (`AgentRuntime.swift:49-112`), each with an iOS view. Server-side parsing is right (Persona already parses transcripts in node/hub), but implementing 20+ cards twice doubles cost. Have the hub emit fairly generic blocks (title/key-value/code/diff/image/table) rendered by a few shared components; specialize only high-value cards.
- **B7. Overly fine targets, one huge test target, unrealized documentation promises**: 39 targets, unused 10-line `HerdrRuntime`, nonexistent `CompositionRoot.swift`, largely absent structured logging (sections 1.4, 5). Use 8–10 Persona modules, tests per module; document implemented structure only.
- **B8. Extreme defensive validation**: per-field UTF-8 byte limits, control-character filtering, handwritten timestamp parsing, bytewise equality, SHA-256 scope keys, directory fsync, lstat ownership checks (`LarkDoNotDisturbClient.swift:90-116`; `AgentLocalStateStore.swift:197-263`). Reasonable against untrusted hosts/upstream; Persona clients trust their own hub, so centralize validation at hub boundaries + contract tests, retaining size/count limits in clients.
- **B9. Documentation style**: the last 400+ lines of ARCHITECTURE.md are per-feature details and each regression round's counts (e.g. `reference-client/docs/ARCHITECTURE.md:452,462,498,517,535`), burying the architecture. Persona's topic docs + CLAUDE.md lessons are better; borrow the separation of real environment / automation / isolated simulator evidence.

---

## 11. Conclusions directly usable in Persona's new design ([Assessment])

1. Layers: `Contracts` (pure models+narrow protocols, SPM targets on iOS, pure Kotlin modules on Android, matching names) → `HubClient` (only HTTP/WS-aware layer) → `Store` (local persistence) → `Feature*` (Contracts-only dependencies) → `App` (composition root + scope container).
2. Scope: `HubScope(hub_id, pairing_generation)` owns scoped client/store/child models/long-running tasks; re-pairing discards the whole scope.
3. Write protocol: every side-effecting request carries `client_op_id`; hub persists idempotency receipts (digest validation, no replay of unknowns, read-only status API). One generic local client journal only covers the stage before hub confirms accepted.
4. Capability negotiation: retain hub→client features, add client→hub `client_features`; server downgrades according to client capabilities.
5. Errors: hub standardizes `{code, message, retryable, delivered}`; client types classify network/auth/incompatible/business/unknown outcome, never wording.
6. Storage: Keychain/Keystore for tokens/device credentials; Application Support / filesDir for journals/drafts (versioned, atomic, corruption blocks writes); Caches / cacheDir for discardable snapshots/media.
7. Concurrency template: revision guards, model-owned Tasks, coalesced refresh, shared subscriptions, bounded streams rereading on overflow, two-step confirmation tokens.
8. Testing: contract fixtures (Go generates, Swift/Kotlin decode together) + gated page-model fakes + mock hub faults + API→UI alignment checklist.

---

## 12. Unresolved / to confirm

1. Didn't read all of `AgentWorkspaceModel.swift` (2,539 lines), `LarkMessagingModel.swift` (3,846 lines), or `LarkSendQueue`. Send-queue state-machine details rely on docs (`BH:reference-client/docs/ARCHITECTURE.md:321-323,366-368,475-477`); code review only checked `LarkOutgoingJournal` structures/version handling.
2. Didn't inspect TerminalUI's "bounded input queue + irreversible ready/closed states", only the documented description (`BH:reference-client/docs/ADR-001-SSH-EXEC.md:24`). Limited relevance because Persona's phone doesn't directly connect to PTY.
3. Effects on real devices/accounts are unknown: the reference app explicitly leaves many such scenarios pending acceptance (`BH:README.md:3,157`; `BH:reference-client/docs/VALIDATION.md` ("Awaiting acceptance")). This doc only assesses design and code structure.
4. No builds or tests run (as requested, no swift test / xcodebuild); `@Test` counts are static, and parameterized tests produce more actual cases.
5. No measurements of modularization's compile-speed/incremental-build effects; no figures given.
6. Persona-side open questions:
   - Retry/idempotency semantics for Inbox hand-to-AI/replies, mail/Lark sending, session creation, etc. weren't individually checked;
   - hub's completion push `settle` (`JV:server/internal/hub/notify.go:222-255`) checks working/seen/muted/background tasks at the last moment, but whether it marks "a TUI question/permission menu not caught by a hook" as complete needs confirmation against session-state sources (the reference app encountered this with Herdr: summary state called a session waiting on questions done, `BH:reference-client/docs/HERDR_PUSH.md:72-78`).
7. Android mappings use general Android practice (Gradle modules, ViewModel/StateFlow, Room/AtomicFile, Keystore, kotlinx.serialization, coroutines-test), not verified against any Android code.
8. Other parallel notes in this directory (`bh-ui-qa.md`, `persona-ios-arch.md`) weren't read; conclusions may overlap. On consolidation, prefer more specific evidence.
