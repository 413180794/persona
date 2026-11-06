# The reference client's Lark gateway client: patterns transferable to Persona's Inbox / Lark chat pages (and Agent chats)

Research date: 2026-10-30. Read-only research; no repository files changed.

## 0. Conventions

- Path prefixes:
  - `MF/` = reference client `Packages/<client kit>/Sources/Features/MessagingFeature/`
  - `OLK/` = the Lark gateway business module in the reference client (called `LarkGatewayKit` below, under `Packages/<client kit>/Sources/`)
  - `BH-docs/` = reference client `docs/`
  - `J/` = Persona repository root (worktree)
  - `OL/` = upstream Lark gateway source (upstream repository in this computer's zoekt index)
- Labels: **[Fact]** = directly observed in code / documentation (with sources); **[Assessment]** = my inference or recommendation; **[Unresolved]** = insufficient evidence for a conclusion.
- Performance figures cite only documented measurements; no numbers without measurements.
- Privacy: copy no real accounts / group names / people / message content / intranet addresses; do not cite acceptance screenshot directories or real group names from reference-client documents.

## 1. Overview of the reference client's Lark structure

[Fact]
- `MessagingFeature`: 164 files, roughly 29,000 lines (`wc -l`); two large core files: `LarkMessagingModel.swift` (267 KB, containing workspace model `LarkMessagingModel` and single-chat model `LarkChatModel`, with `LarkChatModel` starting at line 1811), and `MessagingView.swift` (143 KB, with `LarkChatView` at 779–1817 and `MessageBubble` at 1818–1950).
- Files grouped by capability (inferred from filenames):
  - Reading / scrolling: `LarkChatScrollState`, `LarkChatViewportBridge`, `LarkReadingPositionStore`, `LarkUnreadMentionIndex`, `MessageGroupingPresentation`
  - Drafts / sending: `LarkDraftStore`, `LarkDraftImageStore`, `LarkSendQueue`, `LarkOutgoingJournal`, `PendingSendReviewView`, `ConversationPreviewPresentation`, `LarkScheduleQueue/Journal` (scheduled messages), `LarkPostEditorModel` (multi-image posts)
  - Rendering: `RichMessageContentView`, `InlineMessageTextView`, `MessageVersionContentView` (edit history), `MergedForwardView`, `ReceivedGroupCardView`, `VideoMessageViews`, `AudioMessageCard`, `ChatFileViews`, `MessageImageGallery`
  - Threads: `LarkThread*` (ListModel / ReaderModel / ReadingModel / ComposerModel / ControlsModel / RealtimeHub / VisibilityModel), `ThreadViews`
  - Extensive Lark feature parity: labels, grouping settings, pins, flags, favorites, urgency, scheduling, translation, emoji / sticker libraries, group administration, group calendars / tasks / tabs / bots, scanning, encryption, speech-to-text… (mostly inappropriate to migrate into Persona, see §4)
- `LarkGatewayKit` layers: `Messaging/Contracts` (domain models + narrow protocols), `Messaging/Client` (`LarkMessagingClient` + `LarkMessagingResponseCache`), `Realtime/LarkRealtimeClient`, `Media/` (downloads + disk cache), `ResourceUI/` (image loaders, avatars, galleries, animated images), `Resources/Contracts`, `Transport/` (authenticated transport). Also `People/`, `Search/`, `Approval/`, `Workplace/`, `Auth/`.
- Design principle (`BH-docs/ARCHITECTURE.md:69-121`): each domain owns its models / narrow interfaces / cache / failure recovery, without a global shared Store; Features inject only needed small protocols (`any LarkMessaging`, `LarkMessagingCaching`, `LarkThreadReading`…), testing capabilities dynamically with `messaging is any XxxProtocol`.

[Assessment] The reference client's value is not its feature breadth (it aims at full Lark mobile parity, opposite to Persona), but **chat-list correctness engineering**: reading positions, reporting only genuinely seen messages as read, write-ahead send journals and unknown-result verification, window integrity checks, and late-result isolation (generation / revision). These can become Lark-independent chat infrastructure used by both Persona's Lark and Agent chat pages.

## 2. Topics

### 2.1 Reading-position restoration

Reference client [Fact]
- Storage: `MF/LarkReadingPositionStore.swift:5-74`, actor + single JSON file, keyed by `account → feedID`, storing `{messageID, position, readPosition}`; threads use `~thread~feed:root:thread` (:28-31). Writes carry monotonic `sequence` (`DispatchTime.uptimeNanoseconds`), discarding older writes (:50-57); `readPosition` takes only the larger value (:53, :65-68); late server read receipts advance only readPosition, never overwrite viewport anchors (`advanceReadPosition`, :58-64).
- What is recorded: the **first** visible message (viewport top) anchors reading, the **last** visible message determines read reporting (`MF/LarkMessagingModel.swift:2673-2682`). Save immediately on leaving / backgrounding / opening a sheet (`setReading(false)` → `saveLatestReadingSnapshot`, :2157-2163, :2698-2702).
- On opening (`LarkChatModel.open()`, `MF/LarkMessagingModel.swift:2523-2605`):
  1. Restore send queue, schedule queue, and drafts (:2532-2535);
  2. Read local position; nonnumeric positions or values above 2^53-1 are invalid, with "Opened latest messages" (:2540-2546);
  3. Reuse an in-memory window that has continuously received online events, without a message request (:2549-2561); otherwise first display the **single original response window containing the anchor** from disk (:2562-2574);
  4. Network: local anchor → `around(anchor position)`, otherwise `latest`. Comments (:2579-2582) explain avoiding the default server first page: upstream starts at readPosition, often 0 for newly joined / never-opened groups, which would open the oldest messages.
  5. If the window contains only system messages but newer pages exist, read latest (`needsLatestWindowRecovery`, :2620-2630).
- Initial anchor (`restoreInitialAnchor`, :2614-2619): local anchor → previous anchor → first unread → last message.
- View layer (`MF/MessagingView.swift:1142-1179`): if the anchor is the last message with no unread or newer page, follow end; otherwise `measuredTarget` (alignment 0 = top) + `proxy.scrollTo(anchor, .top)`, then measured correction. Returning from a pushed profile page does not reopen the window (:1143-1150).
- **Pixel-level preservation** (key point): `LarkChatViewportTracker` in `MF/LarkChatScrollState.swift:51-163` + `MF/LarkChatViewportBridge.swift:7-133`:
  - Each row background has a noninteractive `UIView` marker; convert row frames into the owning UIScrollView coordinates on demand (`rowFrames`, Bridge :57-65). Read row coordinates and contentOffset **in the same pass** (`MessagingView.swift:1499-1503` comment).
  - Capture up to 3 anchors `(id, offsetWithinRow, height)`, preferring fully visible rows (Tracker :99-111: a clipped row growing should not displace fully visible rows below).
  - On row growth / viewport resize / prepended history, compute target offset; `Task.yield()` collects the pass's measurements before `setContentOffset`. Ignore differences ≤0.75pt; **never correct while dragging / decelerating** (Bridge :116-125).
  - "LazyVStack's estimated total height may change without the anchor actually moving" (Tracker :94-96); correct only on viewport resize or when content boundaries are available.
  - When prepending history, prefer an anchor with unchanged height (group/date headers can change styling); recapture after correction (Tracker :75-76, :88-93, :155-162).
  - If all anchors leave the lazy realization range, trigger `scrollTo(id)` at most once to realize one (`takeMissingAnchorForRealization`, Tracker :147-151; View :1033-1043).
  - User gestures, explicit jumps, backgrounding, and chat changes invalidate old corrections (`resetViewport`, View :1468-1475, :999-1012, :1197-1204).
  - Do not replace the scroll view delegate or use two scroll-position owners simultaneously (Bridge :5-6; `BH-docs/LARK_CHAT_UI_REVIEW.md:19`).
- Documented measurement / acceptance (`BH-docs/LARK_CHAT_UI_REVIEW.md:17-31`): reproduced jumps into old messages after content above grew and jumps to the first prepended history row; after fixes, the same text position remained after 80 rows grew and three delayed images completed together. Fast dragging still had intermediate blank frames; real-device / frame-rate checks were not done. `BH-docs/FEISHU_PARITY.md:62`: "Preserve message-level anchors; do not promise restoration of pixel offsets within long messages."

Current Persona [Fact]
- Lark page `J/ios/Persona/Views/Inbox/Lark/LarkChatView.swift`: **non-inverted** `ScrollView + LazyVStack + ScrollPosition` (:54-89); `defaultScrollAnchor` chooses top / bottom based on whether a specific message is targeted (:89).
- No saved reading position: every open uses `focus` (first message for this Inbox item, `J/ios/Persona/App/InboxStore.swift:114-123`) or `readPosition+1` (:254-276).
- Message jumps use **fixed waits**: `sleep 80ms → scrollTo → sleep 250ms → scrollTo` (:293-298). The reference client explicitly replaced this with measured completion (`BH-docs/LARK_CHAT_UI_REVIEW.md:19`, "Replace fixed waits").
- Agent chats (`J/docs/ios.md:66-67, 89-104`) already use inverted lists + id jumps followed by measured correction + `chat.read.<session>` storing the topmost row, more mature than the Lark page.

[Assessment]
- Lark should share the Agent chat list engine (inverted, latest at offset 0), rather than maintaining another non-inverted implementation; share reading-position storage with anchor = (message id, within-row offset), keyed by `hub / account / chat (/ thread)`.
- The reference client's UIKit row markers and correction outside gestures solve prepending / growth drift in **non-inverted LazyVStack**; Persona's inverted list already avoids most of this (`J/CLAUDE.md` has several inverted-list lessons). Borrow its **discipline**: idle-only corrections, coordinates from one pass, ignore ≤0.75pt, invalidate after gestures / jumps / backgrounding / chat changes, realize a recycled anchor at most once.
- For Inbox, "where this item starts" matters more than "where I last read": keep `start_position` on entry, but restore local anchors when **opening the same chat outside an Inbox item** (links / Send message from a profile card).

### 2.2 Unread divider

Reference client [Fact]
- Boundary = `max(server readPosition, lastReadPosition reported this time, locally saved readPosition)`; start at 0 if unread exists without a valid reading position (`MF/LarkMessagingModel.swift:2607-2613`; `BH-docs/FEISHU_PARITY.md:701`).
- `firstUnreadID` = first message after the boundary that is neither self-sent nor a system message (:2489-2492); divider is a LazyVStack row with `.id("unread-boundary")` saying "Unread messages below" (`MF/MessagingView.swift:889-895`).
- The divider **breaks consecutive-sender grouping** (`unreadBoundary` argument in `MF/MessageGroupingPresentation.swift:6-7`; call at `MessagingView.swift:882-883`).
- Compute the boundary and Pin set once per row batch, not by searching for every row (`BH-docs/LARK_CHAT_UI_REVIEW.md:39`).
- Limitation (`BH-docs/ARCHITECTURE.md:374`): upstream has no precise server read position; **unreadCount cannot infer cross-device unread boundaries**.

Current Persona [Fact]
- Boundary = opening-time frozen `anchor = readPosition` (`LarkChatView.swift:38-39, 263-265, 207-211`), wording "New messages below."
- **Inbox entry (focus path) sets no anchor → no divider** (:257-260 sets anchor only under `else if opensAtMessage`); Inbox opens always carry focus (`InboxStore.swift:121`), so the most common entry has no divider.
- Grouping uses `senderName`, not sender id (`LarkChatView.swift:221-225`), and the divider does not break grouping.

[Assessment] Inbox entry should show a divider above the item's first message ("This item starts here" / "New messages below"), breaking grouping; group by sender id instead.

### 2.3 Jump buttons (latest / mentions / Pin)

Reference client [Fact]
- Back to latest is separate from follow state: `followsLatest` is a user decision, changed based on bottom position only **during user scrolling**; image loading and other layout changes cannot silently turn following off (`MF/LarkChatScrollState.swift:3-32`). Bottom detection subtracts bottom inset (:10-18).
- Button count includes only messages below the viewport, loaded, synced, sent by others, unseen, and after read position; comment: **protocol positions may be sparse, so their difference is never a count** (`MF/LarkMessagingModel.swift:1857-1869`). Show `N+` if the tail is not fully loaded, bottom icon only for unknown / 0 (`LarkChatScrollState.swift:34-37`; `BH-docs/FEISHU_PARITY.md:64`).
- Back to latest **replaces the whole window** with latest if the tail is absent, never pretending the middle is loaded (`MF/LarkMessagingModel.swift:2763-2776`, comment :2771). Scroll target is a real list footer (`id("bottom")`), not the last message (which may be hidden by outgoing deduplication or grow after layout) (`MF/MessagingView.swift:1083-1084, 1452-1456`).
- Jump to a message: scroll directly if present; otherwise get position from a hint / search result / single-message read, then replace the window with `around(position)` (:2778-2806); center (alignment 0.5) and highlight 3 seconds (`MessagingView.swift:1113-1117, 1185-1189`).
- **Unified navigation button** (`BH-docs/FEISHU_PARITY.md:683-701`; code `MF/LarkMessagingModel.swift:2493-2521, 2446-2464`; `MF/LarkUnreadMentionIndex.swift`): one bottom-right button orders unseen Pins and mentions by position, deduplicates the same message, and becomes Back to latest only after all are seen. Targets count as seen only when actually on screen, not on tap. Upstream `unreadFeedMentions` supplies only the latest per category, so a separate unread-mention index reads pages forward from the boundary (at most 5 per button action, showing "Keep finding unread mentions"), **without inserting main-list rows, scrolling, or reporting read**. Advance coverage only for pages with server `syncState == complete` (`LarkUnreadMentionIndex.swift:56-85`).
- Pin banner (`MessagingView.swift:1406-1423`) and list are Lark features; see §4.

Current Persona [Fact]
- Back-to-latest count = `conversation.unread − seen unread count` (`LarkChatView.swift:166-170`), inferred from server unread count; shown when `hasNewer || !atBottom` (:172-191).
- "Someone mentioned you" has only one target: `conversation.mentionPosition` (:266), unset on focus path (:257-260). Jumps use fixed sleeps (:346-354).

[Assessment]
- Adopt the reference client's count definition (only loaded and truly below the viewport; N+ if tail unloaded), avoiding the illusion of "button says 12, only 3 messages below."
- Keep the single-button idea for mentions: Inbox already knows `mention_position` for the item (`J/docs/protocol.md:388`), so first cover mentions within that item; a cross-page mention index is optional polish and can live on hub (cheaper message-page reads), with the app receiving an ordered target list.
- Do not migrate Pin.

### 2.4 Report only genuinely seen messages as read

Reference client [Fact] (`MF/LarkMessagingModel.swift:2659-2748`, view `MF/MessagingView.swift:992-995, 1434-1436`)
- Replace the visible set once through `onScrollTargetVisibilityChange(threshold: 0.01)` (not one observable write per row onAppear).
- View computes `canRecordReading`: page visible + initial positioning complete + foreground + no jump / positioning in progress + no covering sheet / profile / image preview / settings (`MessagingView.swift:1434-1436`); threads also require no media / history page cover and completed layout positioning (`BH-docs/ARCHITECTURE.md:372`).
- Coalesce 450 ms (:2708-2716); **save local position at any time**, but send **server read receipts** only for freshly verified windows (not cached snapshots or gap-filling sync) (:2718-2727, comment :2719-2720). Recheck current account before sending (:2737); new requests during a slow receipt coalesce into one follow-up (:2729-2734); report only beyond the already-reported position (:2736); successful receipts advance the local readPosition high-water mark (:2741-2742).
- Payload `POST read-position {maxPosition, messageIds}` (`OLK/Messaging/Client/LarkMessagingClient.swift:218-221`); messageIds includes only usable, visible messages from others (`LarkMessagingModel.swift:2678`).

Current Persona [Fact]
- Per-row `.onAppear { markSeen }`, flush after 400 ms (`LarkChatView.swift:78, 378-388`), flush again on `onDisappear` (:140); report highest position + its badge_count + unread ids from others in between (:390-400; protocol `J/docs/protocol.md:408`).
- Thread groups do not report (:381, :391).
- **Focus path sets `readCursor` to the window's last message** (:257-259); flush reports only beyond `readCursor` (:392-394) → initial-window messages opened from Inbox are not reported read, only later loaded newer pages. This contradicts `J/docs/ios.md:14`, "Report only messages shown on screen as read." Whether intentional (e.g. opening an item deliberately not changing Lark read state) is **[Unresolved]**; `J/docs/inbox.md:64` says only that Done does not write Lark read state, not that opening avoids it.
- No foreground / occlusion / positioning / cached-window gates (cache-rendered rows also trigger markSeen).

[Assessment]
- Decide product semantics first: "Opening an Inbox item ≠ read" or "Seen means read." If reporting, follow the reference client's gates (foreground, positioning complete, uncovered, no programmatic jump, verified window), replacing the visible set once via scroll visibility callbacks.
- The same gates apply to Agent Unread → Seen (`J/docs/agent-states.md`): count as Seen only if the user really sees the reply (whether Agents currently do this is [Unresolved], outside this research).

### 2.5 Drafts

Reference client [Fact]
- `MF/LarkDraftStore.swift:5-90`: actor + single file, key = account scope (`accountNamespace + ":" + current user ID`, `readingAccount` in `MF/LarkMessagingModel.swift:1327`; send journals and threads use the same combination, :209-224) → `feedID` or thread key `~thread~feed:root:thread` (:86-90). Drafts have `version: UUID` (new on every edit, `LarkMessagingModel.swift:2808-2814`), monotonic write sequence (:57-58), and retired versions (late saves cannot resurrect sent versions).
- Save with 250 ms debounce (`LarkMessagingModel.swift:2815-2831`); immediately commit native editor contents on backgrounding, leaving chat, or before sending (`MessagingView.swift:1203-1204`; `ARCHITECTURE.md:67`).
- Restore only if editor is empty and unedited this visit (:2842-2861); asynchronously verify quoted original, showing "Quoted message is unavailable; draft text restored" if invalid (:2862-2880).
- Decode failure renames the original to `.unreadable-<uuid>` backup, starts empty, and explains (`LarkDraftStore.swift:77-82`); version allowlist 1–8, with old apps refusing to overwrite newer versions (:69; `ARCHITECTURE.md:67`).
- List previews show `[Draft]`; the submitted draft version no longer appears as a draft (`MF/ConversationPreviewPresentation.swift:62-64`).

Current Persona [Fact] Lark composer uses nonpersistent `@State text` (`LarkChatView.swift:34, 143-161`); Agent composers persist drafts (`J/CLAUDE.md`, "Drafts persist across UI-test runs"), details outside scope.

[Assessment] Persist both Lark composer and Inbox AI draft-reply overlay edits, keyed by `hub / account / feedID (/ thread root)`, with version UUID + monotonic sequence; show a Draft marker on Inbox rows (Inbox's own state, not a Lark feature).

### 2.6 Pending sends: write-ahead journal, original ids, unknown results

Reference client [Fact]
- States (`MF/LarkSendQueue.swift:8`): `submitting / queued (service accepted, awaiting confirmation) / confirmed / failed / unknown`. Persisted phases `prepared / queued / failed / unknown` (`MF/LarkOutgoingJournal.swift:25`).
- Write-ahead order (`LarkSendQueue.swift:149-162`): persist **original draft version**, then **operation record** (`journal.prepare`, including clientId, expected sender, visible text, styles, emoji / mention identities, quote, type; **no attachment bytes**). Only after both persist does `onPrepared()` clear the editor, followed by the network request.
- Every send carries client-generated `clientID = item.id` (UUID) (:196-277). Put returned messageId into memory first (so disk failure does not lose it), then write receipt, then become `queued` (:280-291).
- Failure classification (:293-306): definitively pre-submission errors (local validation, session expiry, 4xx except 408) = `failed`, retryable; others (timeout, 5xx, cancellation, invalid receipt) = `unknown`, **no automatic resend**.
- Confirmation requires exact identity: `clientID` or `remoteID` matching self-sent messages, **never text equality** (:418-425); `checkStatuses()` queries upstream outbox by clientId on opening and every refresh (:498-519; `MessagingView.swift:1178`; `LarkMessagingModel.swift:3162`).
- Retries use the original immutable snapshot and clientId, never rebuild from the current editor (`ARCHITECTURE.md:321`); accepted text uses upstream `/outbox/:clientId/retry` (`LarkSendQueue.swift:236-240`; `OLK/.../LarkMessagingClient.swift:222-229`).
- Display: outgoing cards form a separate section after messages, labeled "Submitting / Service received, awaiting send confirmation (Verify record) / Result unconfirmed · Verify record / Failed (Retry) / Verified in another client…" (`MessagingView.swift:938-970`); hide the corresponding confirmed message by clientId / remoteID (`LarkMessagingModel.swift:1849-1856`).
- Manual resolution: Confirm sent / Confirm not sent (restore text to composer; disabled if it already has a new draft) (`MessagingView.swift:1323-1335`); home can open Verify send records if the original chat is inaccessible (`MF/PendingSendReviewView.swift`).
- Conversation-preview prefixes `[Sending] / [Awaiting delivery] / [Send failed] / [Result needs verification]` (`MF/ConversationPreviewPresentation.swift:46-81`).
- Journal version v12 reads v1–v11; corruption / newer version / write failure retains the original file and **blocks new sends** (`LarkOutgoingJournal.swift:61-71`; `ARCHITECTURE.md:67, 323`).

Relevant upstream Lark gateway capabilities [Fact]
- Send API accepts `clientId`, generating one server-side if omitted (`OL/src/server/routes/api.ts:3747-3757`); `/agent/v1` calls synchronous `messenger.send` (:3775-3782).
- `send` is idempotent by clientId: reuse existing intent; `createOutbox` uses `onConflictDoNothing`; if clientId is already `sent`, return the original messageId without sending again (`OL/src/server/services/messenger-service.ts:7261-7318`; `OL/src/server/repositories/messenger-repository.ts:6680-6705`).
- `GET /api/outbox/:clientId` (status) exists **only on owner UI `/api`, not `/agent/v1`** (`OL/src/server/routes/api.ts:1396-1415` comment); `POST .../outbox/:clientId/retry` is in generic domain APIs, also available under `/agent/v1` (:3810-3838).
- Message views carry optional `clientId` and `localState: synced | pending | failed` (`OL/src/shared/contracts/index.ts:776, 807`).

Current Persona [Fact]
- Lark sends `await api.larkSend`, clears input only on success, shows error on failure (`LarkChatView.swift:402-416`); request has only `text` (`J/ios/Persona/Views/Inbox/Lark/LarkModels.swift:258-259`); no outgoing bubbles or persistence.
- Hub **generates a new random clientId on every call** (`J/server/internal/lark/lark.go:497-504`), so retry after phone timeout = new clientId = possible duplicate send.
- Hub message projection omits `clientId` / `localState` (`lark.go:122-143`).
- Agent chats already have outbox queues (`J/ios/Persona/App/Outbox.swift`: persist waiting messages, send in order, reuse `client_msg_id` on retry; `J/docs/ios.md:288-291`).

[Assessment]
- Reuse Agent Outbox for Lark: phone generates and persists clientId (UUID) → hub forwards unchanged to gateway (change `lark.Send` signature) → upstream deduplicates by clientId. Automatic resend after network recovery becomes safe (already sent returns original id).
- Forward `clientId` and `localState` into `LarkMessage` for exact outgoing-bubble removal (not text comparison). Upstream agent APIs lack outbox status queries; hub may record clientId → messageId at send time, or verify whether a latest-page message has that clientId.
- The reference client's "never auto-resend unknown, verify read-only" reflects direct connection without a dependable idempotency guarantee; with end-to-end clientId idempotency, Persona can be simpler and auto-retry unknown with the same clientId. Attachments (images / files) are outside Persona's Lark-page scope.
- Inbox `POST /api/v1/inbox/{id}/reply` also needs an idempotency key (whether hub internally uses `lark.Send` was pending confirmation, see §6).

### 2.7 Media

Reference client [Fact]
- Reserve image dimensions: upstream part `metadata.width/height` reaches all previews (`BH-docs/LARK_CHAT_UI_REVIEW.md:33-39`: old build received an 800×500 hint but occupied a narrow strip while loading, then pushed later messages down; after fixes, "zero change in the 5016-pixel region of following text"—a pixel-region comparison, not a performance figure). Implementation `OLK/ResourceUI/LarkResourceViews.swift:22-77`: `LarkReservedImageLayout` fits dimensions into the container, **falling back to min(240, w) / min(280, h) for infinity proposals** to avoid zero height during measurement; chat uses `frame(maxWidth: 240, maxHeight: 280)` (`MF/MessagingView.swift:1875`).
- Loader `OLK/ResourceUI/LarkImageLoader.swift:7-68`: owned by view task, canceled on disappearance / backgrounding (task id includes scenePhase in `LarkResourceViews.swift:41-44`); **a successful download counts as an image only if fully decodable, otherwise evict only that cache entry** (:28-35, :62-67). No retry for 400/401/403/404/410 or auth errors; others back off 2/5/15/60 s, at most 3 retries for nonavatars (:44-55).
- Decoding: `LarkNativeImage` directly uses `UIImage(data:)`, **without downsampling** (`LarkResourceViews.swift:93-104`).
- Disk cache accepts only same-origin `/api/media/<key>`, fully decodable static images allowed by server cache policy; ≤10 MiB each, ≤64 MiB per service, expiry = smaller of max-age and 24 h (`BH-docs/FEISHU_PARITY.md:1007`).
- Video: authenticated download to private file, **no Range streaming**; AVURLAsset forbids external references (`BH-docs/ARCHITECTURE.md:356`). Audio: download then play with a custom Ogg Opus / CAF player; all players share audio focus and pause on leaving / backgrounding (`ARCHITECTURE.md:65, 360`). Files: 100 MiB cap, same-origin downloads, system share / preview (`FEISHU_PARITY.md:81`).
- Large galleries: images use `fullScreenCover`, other attachments sheets, under one presentation owner (`MF/ImagePresentation.swift:3-36`).

Current Persona [Fact]
- Already reserves dimensions: `metadata.width/height` → fit within 240×300, fallback 200×150 (`J/ios/Persona/Views/Inbox/Lark/LarkMessageContent.swift:366-399`); video uses posterWidth/Height (:479-492); stickers reserve 80×80 then use actual ratio after loading (:401-415, causing one relayout).
- `LarkMedia` (`J/ios/Persona/Views/Inbox/Lark/LarkMedia.swift`): Caches/lark-media disk cache (sha256(url#size)), ImageIO downsampling (:100-106), NSCache 120 MB (:29), hub thumbnails in 24px steps ≤624 (:183-187); **no decode validation before disk writes** (:59-63). `LarkRemoteImage` shows a broken-image icon after one failure, retrying only on reappearance (:156-181). Video / audio stream directly with Bearer-header AVURLAsset (:277-306, hub forwards Range, `J/CLAUDE.md`); files use Quick Look (:309-332).

[Assessment]
- Persona is better at decoding (downsampling) and video streaming; keep both.
- Worth migrating: ① full decode validation before persistence, evicting only bad entries; ② error-based retries + bounded backoff; ③ dimension reservation for stickers if upstream supplies metadata; ④ custom Layout handling infinity proposals (already documented in `J/CLAUDE.md`).

### 2.8 Rich text / card rendering

Reference client [Fact]
- Rich text: `LarkRichPartMetadata` preserves combined marks, legacy style, inline code, ancestor block chain, paragraph boundaries, blank lines, explicit Markdown, code metadata; rebuild trees from each leaf's full ancestor chain. Unknown blocks retain children, unknown metadata is not shown as JSON (`BH-docs/ARCHITECTURE.md:432-440`).
- Budgets: parts ≤4096, nodes ≤8192, depth ≤24, body ≤1 MB, code ≤256 KiB / 2048 lines / 8192 tokens (`OLK/Messaging/Contracts/RichText.swift:56-62`); exceeding limits falls back to **paginated reading in original order** (6000 characters/page, `MF/RichMessageContentView.swift:444-453`), without dropping later text or attachments.
- Tables / columns: fixed table-row/cell and column projections map to Grid; invalid spans fall back for the whole container (`ARCHITECTURE.md:634-636`).
- Long text: >2000 characters or >20 lines initially shows the first 2000 characters / 16 lines, with Expand long message (`MF/MessagingView.swift:1952-1968`).
- Markdown syntax in ordinary text stays literal; only explicitly markdown-marked parts use Markdown (`ARCHITECTURE.md:438`).
- **Interactive cards largely unimplemented**: bot menus, interactive / application / poll cards are "Pending; unknown content uses readable fallback only" (`BH-docs/FEISHU_PARITY.md:103`); unknown bubble parts show a "[kind message]" label (`MessagingView.swift:1898`).
- Corrupt optional summary fields (`textPreview`, quote / edit-history summaries) fail independently, never breaking valid body content (`BH-docs/FEISHU_PARITY.md:1086-1101`).

Current Persona [Fact] Already ports the Lark gateway web renderer: rich-text block trees, lightweight code highlighting, system messages, full card elements (column_set narrow-screen reflow, table, collapsible_panel, chart, button callbacks…), merged-forward / contact / event summary cards (`J/docs/ios.md:307-326`).

[Assessment] Persona clearly leads in card rendering; no need to learn that from the reference client. Borrow: ① parse budgets + pagination fallback (avoid huge messages freezing the main thread, consistent with caching repeatedly evaluated row bodies); ② independent failure handling for optional display fields (bad previews must not turn the whole message into a placeholder); ③ long-text folding.

### 2.9 Emoji reactions

Reference client [Fact]
- Reaction bar has **two separate touch targets** for emoji and who reacted: emoji toggles the user's reaction, names / count opens participants (`MF/MessagingView.swift:1935-1949`); show at most 2 names + `+N`; honor message `allows("reaction")` permission.
- Fetch reaction participant profiles in batches (501 people batched), explicitly explaining incomplete upstream lists (`BH-docs/FEISHU_PARITY.md:70`).
- Realtime: `reaction.updated` maps to `messageUpdated` (`OLK/Realtime/LarkRealtimeClient.swift:65-68`).
- Whether toggling is optimistic: calls `perform(.reaction(...))` (`MessagingView.swift:901`); implementation details in §7 supplement.

Current Persona [Fact] Optimistic toggle + rollback on failure (`LarkChatView.swift:359-373`); whole reaction chip is one toggle button, showing ≤3 names or otherwise a count (:700-723); long-press menu has 4 common emoji + Emoji reply (:540-560).

[Assessment] Persona's optimistic updates feel better; keep them. Who reacted is Lark browsing functionality, so do not migrate it; separate name/toggle touch targets are unnecessary without participant lists.

### 2.10 Threads

Reference client [Fact] (`BH-docs/ARCHITECTURE.md:364-393`)
- Threads own list / reading models and cursors, not main-chat windows, drafts, or read cursors. Messages retain protocol thread ID, parent message, and thread position; missing roots show noninteractive placeholders.
- Store thread reading positions separately (local 0 sentinel for root position); restore with at most 5 automatic pages, then require explicit continuation or latest jump; no read submissions during restoration.
- Thread reads: 450 ms coalescing, only thread read-position API; clear unread only after confirmed reading through the list's known last item.
- Realtime: `LarkThreadRealtimeHub` reuses the workspace WebSocket, one `bufferingNewest(64)` stream per page; **emit `.connected` on subscription so the page rereads**, also emitting `.connected` on buffer overflow (`MF/LarkThreadRealtimeHub.swift:12-33`).
- Reconnection: revalidate loaded originals in batches (at most 32 each, concurrency 4), repairing recalls / deletions / reactions; deletion tombstones stop old requests resurrecting messages. If new tail and old window have a gap, keep the old window until View new replies is tapped (:391-393).

Current Persona [Fact] Thread groups show a thread list; "N replies" opens a sheet, **one read of the latest page only (limit 60), no backward pagination** (`LarkChatView.swift:675-679`; `LarkModels.swift:255-257`); threads do not report read (:381).

[Assessment] Thread reply sheets need at least backward pagination (otherwise long threads show only the last 60 replies); thread read state / reading position are Lark details, not currently needed by Persona's item-based Inbox.

### 2.11 Realtime synchronization and reconnect gap repair

Reference client [Fact]
- Realtime client (`OLK/Realtime/LarkRealtimeClient.swift:5-74`): one WebSocket, `bufferingNewest(32)`; **emit `.connected` when an event is dropped** (= "You may have missed something; reconcile fully"). Emit `.disconnected` on loss, exponential backoff 1 s to 30 s; map events to domain changes (conversation / messages / single updates / removals / Pins / threads / settings / authentication).
- Establish receive loop before waiting for initial pong on WebSocket startup, otherwise an earlier welcome frame blocks pong (`BH-docs/ARCHITECTURE.md:395`).
- Workspace dispatch (`MF/LarkMessagingModel.swift:1606-1706`): `.connected` → mark all open chats for verification and resume continuous updates; `.disconnected` / background → mark stale, pause cache writes (:1401-1428, :1730-1743); `.messages(feed)` / `.messageUpdated` / `.messageRemoved` → invalidate that chat snapshot and coalesce refreshes. Conversation list refresh coalesces 350 ms, with one extra round for requests arriving during work (:1708-1728); single-chat refresh 300 ms (:3288-3304).
- Refresh **never inserts gaps** (`refresh()`, :3139-3173): tail repair uses `after(newerCursor or last position)`; read latest again only after confirmed tail (empty newerCursor). `mergeTailRepair` accepts only known ids or rows no earlier than the current window's first item (:3801-3806).
- Gap repair (`revalidateMessageWindow`, :3177-3279): upstream **has no replayable event sequence / cursor**, and position is not an edit version (`BH-docs/FEISHU_PARITY.md:1005`). After reconnect, read `around` current anchor and paginate both directions to cover the original displayed range (budget 8–200 pages, monotonic nonlooping cursors). **Replace the original range only if every page has `syncState == complete` (missing rows count as deleted)**; otherwise keep old window + Message history is not fully synchronized + disable read receipts. 403 / 404 removes chat cache, timeout / 503 does not (:3266-3277). Background refresh does not insert earlier context returned by around (would move viewport, :3236-3240).
- Measurement (`BH-docs/FEISHU_PARITY.md:1009`): in isolation, message requests after leaving/reentering stayed 1→1 (online reuse avoided rereads); silent edits/deletions during disconnect updated/disappeared correctly after reconnect.

Current Persona [Fact]
- Hub follows Lark gateway `/agent/v1/events`, pushing `lark{feed_ids}` at most every 400 ms (`J/docs/protocol.md:160`). App `LarkSync` (`J/ios/Persona/App/LarkSync.swift`) gives each listener `bufferingNewest(1)` (:34-40); events trigger chat `refreshLatest` and background prefetch of latest pages for Inbox chats (concurrency 2, at most once per 3 s per chat, :11-15, :83-103).
- **Reconnection notifies only feed == nil listeners (Inbox), not open chats** (:60-61), and background catch-up skips open chats (:73-81) → open chats wait for 60 s polling (`LarkChatView.swift:132-135`).
- **`refreshLatest` reads latest page (limit 40) and appends anything newer than the current last item** (`LarkChatView.swift:300-312`): more than one page arriving between refreshes creates a **silent middle gap**, without detection.
- Hub projection discards upstream `syncState` and filters deleted messages (`J/server/internal/lark/lark.go:282-305`) → app cannot determine window completeness.

[Assessment]
- Fix three areas: ① notify open chats to verify on reconnect; ② refresh with `after(last message)` pages until caught up (above N pages, replace with latest + More new messages), never joining discontinuous pages; ③ forward `syncState` (and deleted-row tombstones), deleting local rows only for complete windows.
- Event streams need two meanings: change notice + overflow requires full reconciliation, like the reference client. Persona's existing `revs` incremental protocol (Agents) validates push/pull combination; Lark mainly lacks gap detection.

### 2.12 Caching and offline use

Reference client [Fact]
- Explicit message snapshot interface (`OLK/Messaging/Contracts/LarkMessagingCaching.swift:3-36`): per-scope conversation list stores original page windows from one complete cursor traversal (atomic commit, failures retain old windows, `BH-docs/FEISHU_PARITY.md:1020`). Restore messages only from **one complete original response window containing the reading anchor**, never joining discontinuous pages; without anchor, restore latest only. Empty incremental pages cannot replace old windows (`OLK/Messaging/Client/LarkMessagingClient.swift:117-135`).
- `LarkMessagingResponseCache`: owner isolation, default 64 MiB (:95), least-recently-used eviction above 512 entries (:298-305), atomic writes + `completeUntilFirstUserAuthentication` file protection + excluded backups (:130, :269-272); generation tokens reject in-flight writes from before disconnect (:241-260).
- Cache display neither confirms sending nor counts as server read state (`BH-docs/FEISHU_PARITY.md:1003`); logout clears it.
- Offline cold start: a recently verified owner bound to original credentials / certificate / connection generation allows read-only cached chats before network verification completes. Read-only mode has no sending / receipts / remote downloads (`BH-docs/FEISHU_PARITY.md:1104-1112`; `BH-docs/LARK_CHAT_UI_REVIEW.md:51-53`). Measurement: unchanged API request count and 0 mutations before/after opening cached body (document wording, no sample distribution).

Current Persona [Fact] `StateCache` (Caches/state) keeps latest 80 messages per Lark chat (`J/docs/ios.md:94-95`); cache renders first on cold start / disconnect / tab switching (same reference). But **Lark page uses cache first only for `focus == nil`** (`LarkChatView.swift:121-130`), while Inbox always supplies focus → Inbox chats do not render cache first, making `LarkSync` prefetch mostly unused there (`J/docs/ios.md:33-34`, "Open unread chats from cache if it covers the first unread," covers only non-focus paths).

[Assessment]
- Change hit rules to "Render cache first if it contains the target message (start_position)," matching the reference client's anchor-containing window.
- Cache only continuous windows (one or multiple contiguous pages), not stitched results; add owner / hub scope (whether pairing another hub clears cache is [Unresolved]).

### 2.13 Messaging home (list) patterns

Reference client [Fact] (`BH-docs/LARK_INBOX_UI_REVIEW.md`, `BH-docs/FEISHU_PARITY.md:32-55, 1014-1075`)
- Total unread comes from server aggregate, not summing loaded pages (`MF/LarkMessagingModel.swift:1442` comment).
- Pinned prefixes can fill page one: display only the complete prefix before the known activity-time boundary, holding equal-time rows together so older chats do not appear ahead of unfetched newer ones (`FEISHU_PARITY.md:1016-1022`).
- Row preview: draft / send-state prefixes (§2.5, §2.6).
- Offline: local read-only home, explicitly Last account, local read-only, possibly stale (`FEISHU_PARITY.md:1108`).
- Three-digit badges, red dots / gray counts, red flags, pinned grid, sticky filters, pull-down pin expansion… all reproduce Lark home.

[Assessment] Persona Inbox is an AI-sorted list of items, not Lark feeds. Borrow only: ① local row state (draft / sending / send failed); ② Local data notice in the title when offline (Persona already has connection titles, `J/docs/ios.md:273-297`); ③ server-only aggregate counts.

## 3. Suspected issues found in current Persona (evidence summary)

| # | Symptom | Evidence | Nature |
| --- | --- | --- | --- |
| 1 | Inbox chat entry (focus): no divider, mention button, cache-first rendering, or initial-window read reporting | `LarkChatView.swift:121, 257-260`; `InboxStore.swift:121` | Fact; intent [Unresolved] |
| 2 | Refresh appends latest page, silently leaving gaps above one page of new messages | `LarkChatView.swift:300-312`; `LarkModels.swift:252` (limit 40) | Fact (code path); not reproduced experimentally |
| 3 | Reconnect does not notify open chats; catch-up can take 60 s | `LarkSync.swift:60-61, 73-81`; `LarkChatView.swift:132-135`; `AppModel.swift:455-459` | Fact |
| 4 | Nonidempotent Lark sending: random clientId on every hub call; no phone outbox | `lark.go:497-504`; `LarkChatView.swift:402-416` | Fact |
| 5 | Fixed sleeps for positioning | `LarkChatView.swift:293-298` | Fact |
| 6 | Grouping by name, not sender id | `LarkChatView.swift:221-225` | Fact |
| 7 | Thread replies limited to latest 60 | `LarkChatView.swift:675-679`; `LarkModels.swift:255-257` | Fact |
| 8 | No decode validation before media persistence | `LarkMedia.swift:59-63` | Fact; actual effect depends on hub error handling [Unresolved] |
| 9 | Hub discards `syncState` / `clientId` / `localState` | `lark.go:122-143, 282-305` | Fact |

(Following sections: §4 migrate / do not migrate, §5 Agent chats, §6 Android notes, §7 supplementary reading, §8 unresolved—appended below.)

## Supplementary reading (appended after interruption)

### Supplement 2.9: reference-client emoji reactions are not optimistic
[Fact] `LarkChatModel.perform(.reaction…)` (`MF/LarkMessagingModel.swift:2230-2252`): gate with message `allows("reaction")`, allow only one action during `isPerformingAction`, show Reacted / Reaction removed only **after server response**, and record the emoji in recent use only on success (`recordConfirmedUse`); UI updates through later refresh / `reaction.updated` events.
[Assessment] Persona's optimistic toggle + rollback (`LarkChatView.swift:359-373`) feels better; keep it. Borrow recording recent use only after success.

### Supplement 2.6: Inbox replies are also nonidempotent
[Fact] `POST /api/v1/inbox/{id}/reply` → `replyInbox` → `h.lark.Send(ctx, feedID, text)` (`J/server/internal/hub/inbox_triage.go:213-230`); chat-page sends (`J/server/internal/hub/lark.go:120`) and assistant tool `lark_send` (`J/server/internal/hub/assistant_tools.go:679`) also use `lark.Send`, with a random clientId each time (`J/server/internal/lark/lark.go:497-504`).
[Assessment] Unify all three paths on caller-supplied clientId (phone-generated, or hub-generated and persisted per item / tool call); same for Watch Send (direct `/reply`, `J/docs/inbox.md:101`).

### Supplement 2.12: re-pairing does not clear caches
[Fact] `AppModel.pair()` / `unpair()` (`J/ios/Persona/App/AppModel.swift:123-152`) clears memory and credentials, not `StateCache` (Inbox `inbox-items`, `lark-chat-<id>`) or `Caches/lark-media`; media key is only `sha256(url#size)` (`LarkMedia.swift:108-111`), without hub identity.
[Assessment] Low risk for personal use, but seeing old-hub Inbox data immediately after switching violates reference-client cache isolation by connection / account and clearing on account change (`BH-docs/ARCHITECTURE.md:317`); add hub-scoped cache directories and clear the whole scope on pairing changes during redesign.

## 4. Migrate / do not migrate (under "Do not build a second Lark")

**Migrate into Lark chats / Inbox (chat experience / sync / reliable sending, not Lark features)**

| Priority | Pattern | Reference-client source | Persona destination |
| --- | --- | --- | --- |
| P0 | Idempotent sending: forward clientId to Lark gateway; outgoing bubbles + persistence + offline waiting; remove bubbles by exact clientId / remoteId, not text | §2.6 | Reuse app Outbox; add clientId to hub `lark.Send`; forward `client_id` / `local_state` in `LarkMessage` |
| P0 | Gap-free refresh: catch up with `after(last message)`, otherwise replace with latest + notice; notify open chats on reconnect | §2.11 | `LarkChatView.refreshLatest`, `LarkSync.reconnected` |
| P0 | Inbox entry divider / cache-first rendering / read semantics (decide product semantics first) | §2.2, §2.4, §2.12 | `LarkChatView` focus path |
| P1 | Share Agent inverted list engine with Lark; id jump + measured correction, remove fixed sleeps | §2.1, §2.3 | Shared chat-list component |
| P1 | Read reporting gates (foreground, positioned, uncovered, no programmatic jumps, verified window; 450 ms coalescing; forward only) | §2.4 | `LarkChatView.markSeen/flushRead` |
| P1 | Latest button counts only loaded messages below viewport, N+ for unloaded tail; whole-window replacement on latest jump | §2.3 | Same as above |
| P1 | Persist drafts by hub / account / chat (/ thread), version UUID + monotonic sequence; Inbox rows show draft / sending / failed | §2.5, §2.13 | Chat composer, AI draft-reply overlay, `InboxView` rows |
| P1 | Hub forwards `syncState`; app deletes local rows only when complete; evict cache on 403 / 404, not timeout | §2.11 | `server/internal/lark` page projection |
| P2 | Cache hit when it contains the target; continuous windows only; hub-scoped cache | §2.12 | `StateCache` / `LarkSync` |
| P2 | Media: decode validation before persistence, error-based retries + backoff, sticker dimensions reserved | §2.7 | `LarkMedia` |
| P2 | Rich-text parse budgets + pagination on overflow; independent optional-summary failures; long-text folding | §2.8 | `LarkMessageContent` |
| P2 | Backward pagination in thread-reply sheet | §2.10 | `LarkThreadView` |
| P2 | Sender-id grouping; unread divider breaks groups | §2.2 | `LarkChatView.continues` |
| P3 | Unified mention navigation button (first within item; cross-page index on hub) | §2.3 | Bottom-right chat button |

**Do not migrate (violates "Do not build a second Lark" or adds no value to handling an item)**
- Home: Lark feeds themselves, pinned grid / expansion, message / unread / flagged filters, label/group settings, Later / Done / conversation box, automatic DND, three-digit badge rules, event center (`BH-docs/FEISHU_PARITY.md:32-55`; `BH-docs/LARK_INBOX_UI_REVIEW.md`).
- Contacts (PeopleFeature 67 files: organization tree, friend requests, blocks, contact privacy, my contact link, personal status, DND, logged-in devices…), global search (SearchFeature 4 files), approvals (ApprovalFeature 28 files: list, approve / deny, comment drafts, common comments, flowchart, CC read state, extension actions), workplace (WorkplaceFeature 6 files: app catalog, favorites management, helpdesk)—these belong only on hub / AI side (`lark_api` in `J/docs/feishu.md:15`).
- Message actions: Pin banner / list, flags, favorites, forwarding / merged forwarding / multi-select bulk actions, urgency, scheduled messages, translation / image translation, read participants, reaction participants, editing / recalling / deleting, group management, group tabs / calendars / tasks / bots.
- Input: four inline styles, post titles, multi-image post editor, member mention picker, emoji / sticker library management, voice recording, video capture/sending, file sending (Persona Lark page replies with text only; hand attachments to AI when needed).
- Thread read state / reading positions / management (follow, close).
- Entire offline cold-start scheme binding original credentials / certificate generations (Persona uses a paired token + its own hub and already renders cache first).
- Possibly keep, subject to user decision ([Assessment], not a firm conclusion): quoted replies (useful for answering a specific group question while handling an item).

## 5. Lessons for Agent chats (also chat lists)

[Assessment] (Current Agent pages follow `J/docs/ios.md:60-114`; `ChatView.swift` was not read line by line.)
- Already correct and structurally equivalent to the reference client: follow state changes only through user gestures (`followsEnd` vs `LarkChatScrollState.followsLatest`); id jumps + measured correction; freeze new rows away from latest; outgoing bubbles + idempotent `client_msg_id` retries; local cache-first rendering.
- Worth adding:
  1. Seen-reporting gates: foreground + initial positioning complete + no sheet covering + no programmatic scroll before counting a reply as seen (Unread → Seen, tied to suppressing notifications after viewing).
  2. Correction discipline: idle only, ignore ≤0.75pt, invalidate old corrections on gestures / jumps / backgrounding / session changes (reference-client `LarkChatViewportBridge.correct` refuses offset changes while tracking / dragging / decelerating).
  3. Four states, Submitting / Accepted awaiting confirmation / Unknown / Failed, rather than Sending / Failed. Unknown verifies rather than blindly resends (Agents can auto-retry with hub idempotency, but UI must show Result unconfirmed).
  4. Late-result isolation: every async writeback carries revision / windowRevision / messageRevision generations (the reference client checks almost every await, e.g. `MF/LarkMessagingModel.swift:2547, 2563, 2584`), incremented by session changes, jumps, and backgrounding.
  5. Event-stream overflow requires full reconciliation: never silently drop; trigger catch-up (reference-client `LarkRealtimeClient` / `LarkThreadRealtimeHub`). Persona WebSocket already replays with `since_seq`; this mainly applies to app-internal event dispatch (`AsyncStream` buffering).

## 6. Android implementation notes for these chat-list patterns

[Assessment] (Persona currently has no Android code; these design suggestions follow official Compose behavior, untested on devices. Verify specific API versions during implementation.)
- **List direction**: `LazyColumn(reverseLayout = true)`, newest → oldest data, latest at index 0. Matches Persona's inverted iOS list: latest jump is always `scrollToItem(0)`, exact and independent of estimated heights; earlier history appends at the far end without shifting the viewport.
- **Position preservation is mostly framework default**: LazyColumn anchors the first visible item's key + offset, finding it again by key after data changes; height changes in invisible items (above / far end included) do not move the viewport. Most prepended-history jumps and image growth above pushing text—the reference client's UIKit marker solution in SwiftUI—need no custom Compose work. Remaining concern is **anchor-item growth itself**, solved by reserving image dimensions.
  - Conversely, **follow end** needs explicit work: reverse layout inserts new messages at index 0, while framework preserves the original visible item. Explicitly `scrollToItem(0)` only if `followsLatest`; otherwise do nothing, matching Persona's nonintrusive history reading. Change `followsLatest` only during user-driven `isScrollInProgress` (`interactionSource` / `NestedScrollConnection` can distinguish gestures from programmatic scrolling).
- **Stable key + contentType**: `items(rows, key = { it.id }, contentType = { it.kind })`; dividers, date headers, and Load more are independent items with stable keys such as `"unread-boundary"`, never a group of messages wrapped in one item (`J/CLAUDE.md`, "Do not wrap a potentially large set of rows in one child view in long lists").
- **Windows rather than Paging 3**: maintain a message window (`olderCursor / newerCursor`, jump to item = replace with `around(position)`, latest = replace with latest, refresh = catch up via `after(last message)`), state in ViewModel `StateFlow<List<Row>>`, disk cache in Room (continuous windows by hub / account / chat). Paging 3 `PagingSource` supports bidirectional paging, but a custom window is more direct for whole-window replacement + gap detection + outgoing-row merging.
- **Initial positioning (first unread / item start)**: two steps—`scrollToItem(index)` roughly, then after one frame read actual offset in `layoutInfo.visibleItemsInfo` and `scrollBy(delta)` to place at viewport top (equivalent to the reference client's scrollTo then measured correction); no fixed delays.
- **Save reading position**: `(message id, item offset)`; reverse-layout `firstVisibleItemIndex/ScrollOffset` refers to the **bottom** item, naturally restored with `scrollToItem(indexOf(id), offset)`. For topmost-screen-row semantics, use the highest index in `visibleItemsInfo`. Save immediately at `onPause` (`LifecycleEventEffect(Lifecycle.Event.ON_PAUSE)`) and on leaving; otherwise debounce 250 ms.
- **Read reporting**: `snapshotFlow { listState.layoutInfo.visibleItemsInfo }` → filter truly visible (ratio from offset / size and viewport) → `debounce(450)` → report forward only. Gates: lifecycle RESUMED + initial positioning complete + no BottomSheet / dialog cover + no programmatic scrolling + not unverified cache.
- **Image placeholders**: with dimensions, `Modifier.widthIn(max = 240.dp).aspectRatio(w / h)` (also clamp max height); load with Coil (`ImageRequest.size(...)` downsamples to display size, like iOS ImageIO thumbnails), add auth headers through OkHttp interceptor; fixed placeholder without dimensions. Inline emoji use `AnnotatedString` + `InlineTextContent` with fixed-size `Placeholder`, so late images do not relayout (more stable than iOS `[label]` placeholder then redraw).
- **Video / audio / files**: Media3 ExoPlayer + OkHttp data source with Authorization header; hub forwards Range, allowing streaming. ExoPlayer parses Ogg Opus; decoding depends on system MediaCodec (available on most newer devices) or libopus extension—real-device verification required. Download files to cache, then open via `FileProvider` + `ACTION_VIEW` (Quick Look equivalent).
- **Outbox**: Room stores `clientId`, chat, text, state (Submitting / Accepted / Unknown / Failed), creation time; clear input only after successful write. WorkManager (`NetworkType.CONNECTED`) resends in chat order after network recovery, keeping original clientId.
- **Realtime events**: OkHttp WebSocket, `SharedFlow(extraBufferCapacity = N, onBufferOverflow = DROP_OLDEST)` dispatch; dropped events or reconnect emit Full reconciliation required, and pages verify their windows. Disconnect in background (same as iOS, `J/CLAUDE.md`, "Keeping WebSocket connected in background… now disconnect on backgrounding"), with high-priority FCM wakeup as needed.
- **Cards / rich text**: custom `Layout` for weighted / fixed / auto column_set widths (watch `Constraints.hasBoundedWidth`, analogous to iOS infinity proposals); horizontal `Modifier.horizontalScroll` for tables; paginate oversized messages within budgets.

## 7. Unresolved / follow-up confirmation needed

- Inbox focus path omits initial-window read reporting and divider: intentional product decision or omission? (`LarkChatView.swift:257-260` vs `J/docs/ios.md:14`)
- `refreshLatest` gap concatenation is inferred from code, not reproduced in real chats; needs a unit test with 41+ messages arriving between refreshes.
- Lark gateway `/agent/v1` has no clientId send-status query (`OL/src/server/routes/api.ts:1396-1399` explicitly says only `/api`); unknown-result verification must use message `clientId` or hub bookkeeping. Behavior of repeating `send` with the same clientId while outbox is pending (in flight) was not fully read (branches after `OL/src/server/services/messenger-service.ts:7305`).
- (Verified, removed from unresolved) Upstream page API uses `registerDomainApi` `p()` to register both `/api` and `/agent/v1` (`OL/src/server/routes/api.ts:3378-3380`), so agent APIs also return `syncState` values `complete` / `local` / `gap` (`OL/src/server/repositories/messenger-repository.ts:4192, 4232, 4304`). Hub only needs to forward it (currently discarded, `J/server/internal/lark/lark.go:282-293`).
- Reference-client reading positions / read gates / pixel anchors on real devices: its own docs still list real touch, frame rate, and fast-drag blank frames as unverified (`BH-docs/LARK_CHAT_UI_REVIEW.md:23, 31`); do not copy them as proven best practice.
- Agent chat Seen gates and `ChatView` correction timing—`ChatView.swift` was not read this round; rely on `J/docs/ios.md` descriptions.
- All Android content is design advice, not measured.
