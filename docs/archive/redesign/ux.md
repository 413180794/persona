# Interaction and UI specification (shared by both platforms)

> **Since 2026-10-31, the Android appearance, layout, and interaction sections here have been superseded by [Android interface styles and design system](../../android/android-design.md)**: the user requested a complete visual and interaction redesign into three styles: Glass (default), Morning, and Board. This document remains authoritative for functionality, data, protocol, and gate specifications.

> Draft v1 (2026-10-30), part of the [redesign proposal](README.md). This describes the target design, with iOS and Android implementing the same specification.
> **This round ships only on Android; iOS stays unchanged** (read-only, no edits), with follow-up later. References to "both platforms" or "iOS also…" are future work for iOS.
> Android-specific forms are collected in section 9. P0 / P1 / P2 priorities have the same meanings as in [parity.md](parity.md).

## 1. Principles

1. **Keep the information architecture**: retain the four tabs (Persona / Agents / Inbox / Settings), current state model, grouping, and display logic. These are Persona's strengths over the reference client.
2. **Improve reading and decisions**: make Agent work clear on the phone (search, task progress, full text, visible failures) and decisions precise (denial reasons, plan comments, countdowns, provenance).
3. **State uncertainty explicitly**: "Returned" is not "Executed," "No result yet" is not "Still running," and "Based on loaded records" must be shown. Unknown results are a formal state with a name, icon, and verification entry, not red errors or silent retries.
4. **One place for state**: describe the session's current state only in a line above the composer. The header shows identity only; list rows use dots only.
5. **One rendering per concept**: states, counts, pending states, and confirmation overlays are design-system components with the same names and parameters on both platforms.
6. **Keep existing conventions**: button text never wraps; links open in-app; cancellation, timeout, and offline states do not show error popups; do not build a second Lark.

## 2. Information architecture

| Tab | Unchanged | Additions / changes |
| --- | --- | --- |
| Persona | Home, main chat, history, knowledge base, profile / scheduled tasks / memory | Assistant chats use the unified status line and pending state; first messages use the shared send use case (currently connect directly to hub and fail immediately offline) |
| Agents | Three grouping modes, pin / hide, space page, machine page, new session | Draft previews and verification markers on rows; "Needs your attention" becomes a cross-session request inbox; "History" scope |
| Inbox | Unified list, AI sorting, sections, filters, done state, AI twin drafts | Replace the Lark chat list engine, add reading anchors, dividers, read gates, idempotent sending, and gap detection; show local state (draft / sending / send failed) on rows |
| Settings | Subpages | Redesign pairing and first connection; device page shows platform; Android notification page uses notification channels and Live Updates settings |

## 3. Page specifications

### 3.1 Agents list

- **Rows** (`SlotRow`) retain three lines: "state dot + title + time / latest line / metadata." Add two items at the start of the metadata line:
  - Orange "Draft · <first 20 characters>" when a draft exists, or "Draft · [Attachment]" for attachment-only drafts.
  - "⏱ Awaiting confirmation" for operations with unconfirmed results, using caution color and clock-question icon; entering the chat shows the explanation in the status line.
- **Upgrade "Needs your attention" to a request inbox** (P1):
  - One row per pending request: type icon, question, session name · computer, time remaining (`CountdownLabel`), source tag (subagent / MCP server / terminal).
  - Single-question, single-choice requests with at most 4 options can be answered directly; authorizations can be allowed / denied directly (existing capability).
  - Other requests open **directly into the full interaction page** (3.4), without entering the chat first. The full page includes "Open chat."
  - Above 3 requests, collapse into "N more ›," opening a full-screen request list ordered by expiry.
- **"History" scope** (P2):
  - Beside grouping in the top-left menu, add "Scope: Running / All / History," defaulting to Running, as today.
  - History lists non-running sessions by project (directory), preferring Claude custom titles.
  - Open a **read-only** chat: replace the composer with "Continue this session," calling `--resume` only after tapping. Accidental taps therefore do not start a process on the computer.
- **Stable ordering**: within a state, sort by `state_since` / last update; refreshes must not make rows jump. This existing rule becomes part of the specification and fixtures, with shared `rank` on both platforms.
- **Existing behavior both platforms follow** (recent iOS additions): counted quick filters under search (All / Needs you / Unread / Working); seen rows stay in place after filtering so they can be reviewed one by one; Unread supports marking all seen; compact list (one Agent per row, remembered choice); tap a space heading to collapse it, with Collapse all / Expand all.

### 3.2 Chat page skeleton

Top to bottom:

1. **Header** (retain Persona's gesture-following collapsible `ChromeHeader`):
   - Back / title + identity subtitle (computer · space › tab) / terminal / ⋯.
   - Subtitle **no longer includes state text**: keep one state dot to the title's left; text state belongs in the status line.
   - Second-row rules (peer menu, subagent strip) remain unchanged.
2. **Optional banner**: "Task progress · 3/7 ›," shown only with structured task results (3.3).
3. **Body**: inverted list, work rows, message rows (3.5).
4. **Status line** (`StatusLine`, new, P0): immediately above the composer; **only this redraws every second**. Left shows the definitive state (5.1), right shows turn duration, tool count, and terminal button. Hide the whole line for Seen / Unread, taking no space. Explanatory states (unconfirmed result, computer offline, session ended) make the whole line tappable to open "Session status" (3.3).
5. **Accessory strip**: queued messages, loops, background tasks. Combine the existing three strips into one small-label row: "Queued 2 · Loop · Background 3," opening details on tap. Keep away-period recap and Claude suggestions unchanged.
6. **Interaction panel**: above the composer (3.4).
7. **Composer** (`ComposerRow`): [+] / text field / Send or Interrupt, unchanged; mode and reasoning-effort tags unchanged.

### 3.3 Reading tools

| Capability | Specification | Priority |
| --- | --- | --- |
| **Work row shows failure count** | Always show "· 2 failed" after counts (danger text) and "· 1 without result" (secondary gray; wording is "No result recorded," not "Running"). Duration comes only from recorded turn duration; omit if absent, never estimate from timestamps | P1 |
| **Sticky Collapse after expansion** | When expanded work scrolls above the top, float "Collapse N steps" at the top; tapping collapses and returns to the work row. Android's inverted list cannot directly use `stickyHeader`; draw the overlay using `layoutInfo` | P2 |
| **In-chat search** | ⋯ › Search opens full-screen search over loaded body text, reasoning, tool names, inputs (all JSON strings), outputs, and system events; 200ms debounce, background execution, at most 100 hits (prompt to narrow if exceeded); footer "Load earlier records and keep searching." Tap a hit to close search, expand its work row, scroll there, and outline-highlight for 3 seconds; show a "Return to pre-search position" capsule (`ReturnChip`) at the top. Earlier pagination must remain contiguous, at most 20 pages per action, showing "Loaded N turns" and preserving a continuous list window | P1 |
| **Task progress** | Reconstruct the task table from TodoWrite / TaskCreate / TaskUpdate / TaskList results in transcript order (pure logic in Core, with fixtures). Top banner "Task progress · 3/7"; full-screen page shows each item's state, description, and "View related record" (jump to the call, with `ReturnChip`); footer "Based on loaded records; may differ from the computer's current state" | P1 |
| **Full-text reader** | "Read full text" for long tool outputs (existing `/items/{id}/full`) opens `FullTextReader`: freeze a content snapshot on opening; paginate by 8192 UTF-16 code units without splitting surrogate pairs or graphemes; first / last / previous / next page; forward/backward text search; copy all | P2 |
| **Unified tool-card entries** | Every expanded tool card ends with Raw input / Raw output / Copy; Edit diffs add Previous change / Next change navigation | P2 |
| **Session-status overlay** | Tap explanatory status lines: unconfirmed operations (automatic verification progress and two manual actions: Confirm delivered / Confirm not delivered, keep input), offline computer (last update, Reconnect this computer), ended or replaced session (Open new session / Resume), draft-save failure reason | P1 |

### 3.4 Interactions (questions / plans / authorization / terminal dialogs / MCP forms)

**Presentation hierarchy**:

- **One request**: interaction panel above the composer (current behavior); top-right "⤢" opens the full page.
- **Multiple requests in one session**: "1 / 3" at the top, swipe left/right, and All opens that session's request list.
- **Across sessions**: Agents request inbox (3.1) and "Agents waiting for you" in the Inbox tab (existing).
- **Full page** (`InteractionSheet`) pushes up from the bottom: iOS large sheet detent; Android full-screen `ModalBottomSheet`. Top to bottom:
  1. Type heading;
  2. Source row: session name · computer; subagent "From subagent · Explore"; MCP "From MCP server X (configuration source: …)"; terminal "This request comes from the terminal; do not handle it on the computer at the same time";
  3. Countdown (`CountdownLabel`) or Handled / Expired / Session changed;
  4. Body: parameter fields, diff, original plan, questions, forms, links;
  5. Optional reason/comment field;
  6. Bottom-pinned actions. At accessibility text sizes, actions scroll with the body and buttons stack vertically.

**Rules by type**:

| Type | Rules | Priority |
| --- | --- | --- |
| Authorization | Deny / Allow / Always allow in this chat (existing). **Add optional denial reason** (≤4000 characters), passed to Claude with the denial (`Resolution.message`, supported by hub long ago but never filled by the app); **disable Allow while a reason is entered**, prompting "Clear the reason to allow." MCP tools show server name and configuration source; if absent, say "Not provided," never infer from tool name | P0 (reason) / P1 (MCP source) |
| Plan | Original plan + full screen; Approve and run automatically / Approve, auto-accept edits only / Send back. **With comments entered, only Send back is available**; disable both approval buttons until cleared. Approve and run automatically means allow everything; state the risk below the button. **Every entry uses the same approval policy table** (notifications, Live Activities, quick entries, and Watch can only Approve, auto-accept edits only, since the full plan is not visible there; allow-all can be selected only after reading the full plan in-app) | P0 |
| Question | Existing rules unchanged (single-question single-choice submits on tap; multiple-question pagination; Recommended tag; Other answer…). Add persistence of unsubmitted selections and custom input by request id, restored after closing/reopening; discard if the request is replaced | P1 |
| Terminal dialog (screen card) | Existing rules unchanged (hub parses dialog, compares `rev` before keystrokes, returns `prompt_changed` if changed). Add: command approvals with a matching full tool_use in the transcript show the full command rather than potentially truncated screen text (requires node association, see hub-changes.md) | P2 |
| MCP form / link (new type `elicitation`) | Forms support string / number / boolean / single-choice / multi-choice, validated client-side first; link mode shows server, request description, target host, and full URL. Enable "Opened, continue" only after actually opening the link, explaining "Opening the link does not mean authorization succeeded." Return Submit / Deny / Cancel distinctly. Requires node hook and a new interaction type; hub downgrades for unaware old clients to "Handle in terminal" (capability negotiation) | P1 |

**Shared rules**:

- Before answering, record request id and operation id locally, **not the answer itself**; if the receipt is lost, query by id rather than resending.
- Success wording is "Your choice has been returned"; execution results are in the transcript.
- If handled elsewhere (410), replace the card in place with "Handled on the computer," then fade out.
- Expired requests cannot submit; show "Expired; handle in terminal" and "Open terminal."
- Notification buttons: Allow / Approve require unlocking; Deny / Send back do not.
  - Android allows both denial reasons and plan comments directly in notifications; iOS stays unchanged, with direct input only for Send back.
  - Single-choice questions and the first 3 screen-dialog options also become buttons: Android has these in the first release (already in PR #84); iOS needs dynamically registered notification categories per interaction, scheduled P2.
  - Withdraw notifications after handling elsewhere: Android first release; iOS uses `removeDeliveredNotifications` while the app runs, scheduled P2.

### 3.5 Chat list engine (shared by Agent, assistant, and Lark chats)

Agent chats currently use an inverted list; Lark uses a separate non-inverted implementation (positioning with fixed sleeps). After redesign, **share one list engine** (`ChatList`), differing only in row styling and data source.

| Rule | Description |
| --- | --- |
| Inverted | Newest item at offset 0. Jumping to latest is always exact; history is added at the far end without moving the reading position |
| Follow end (`followsEnd`) | Changed only by user gestures: dragging turns it off; reaching the bottom, tapping ↓, or sending a message turns it on. Image loading and other layout changes cannot silently turn it off |
| Freeze | Away from the latest end, freeze the item snapshot from that moment; new rows appear only after returning to latest (existing E1 / E3 rules); earlier pages and gap fills still appear normally |
| Entry position | Unread completed sessions: show only the last line of the user's last message at the top. Others: return to the topmost visible row when last leaving. Lark opened from Inbox: stop at the first message for this item, with a divider above |
| Reading position | Store anchor (message id, within-row offset) and expanded state by (scope, session / Lark chat). Write immediately on leaving or entering background, otherwise debounce 250ms. Restore only on the first layout after opening; user scrolling invalidates restoration |
| Positioning discipline | Jump by id, then correct using measured position; correct only while idle, ignoring differences within 1pt. User gestures, explicit jumps, backgrounding, and session changes invalidate old corrections; reinstantiate a recycled lazy anchor row at most once |
| Pagination | Agents paginate by turn (`turns=2`), Lark by message window (`olderCursor` / `newerCursor`); reaching the far end auto-loads only after the user has dragged |
| Long-session folding | Agent page requests carry `fold_work`: hub folds long sequences of tool calls, results, and reasoning into one fold item (counts and range), shown as a work row. On expansion, retrieve original steps through `from_seq` / `through_seq` and replace it ("Loading N steps…," retry on failure). Live pushes and catch-up are not folded (already on iOS, `docs/protocol/protocol.md`) |
| Refresh and gaps | Append only to the existing window's end (`after_seq` / `after`); if not contiguous, replace the whole screen with the latest page and show "More new messages"; **never concatenate discontinuous pages** |
| Back to latest button | Count only loaded unread messages from others actually below the screen; show "N+" when more remains unloaded; arrow only when count is unknown |
| Read / seen gates | Count as seen only when the page is foreground + initial positioning complete + no covering overlay + no programmatic scrolling + window is not unverified cache. Coalesce every 450ms and report only forward. Used for Lark read reporting and Agent Unread → Seen |
| Rows | Each message and work step is an independent lazy row with stable key and content type. Do not wrap a potentially large group of rows in one child view (existing lesson) |

### 3.6 Inbox and Lark chat page

- **Inbox rows**: alongside existing avatar, title, type tag, time, and summary, show local "Draft · …," "Sending," or "Send failed ›," using pending-state components.
- **Lark chat page**:
  - Send through the unified outbox: phone generates and persists clientId first, hub forwards it to the Lark gateway, which is idempotent by clientId. Sent messages appear as bubbles; confirmation matches only clientId or server message id, **never text**.
  - Persist drafts by (scope, Lark chat).
  - Thread-reply overlays can paginate backward (currently only the latest 60 replies).
  - Group consecutive messages by sender id, not name; dividers break grouping.
- **Inbox entry semantics** (confirmed 2026-10-31):
  - Opening an item reports seen messages as read through the 3.5 gates: messages actually seen on the first screen after opening from Inbox also count for Lark read reporting; the initial window is no longer exempt.
  - Done still records only in Persona.
- **AI draft-reply overlay**: persist edits. Send through outbox with an idempotency id; hub `/inbox/{id}/reply` must forward it too.

### 3.7 Pairing and first connection

Phone connects directly to the hub over the public internet, preferring IPv6 ([README.md](README.md) D9). This round implements the following only on Android; hub cooperation is in [hub-changes.md](hub-changes.md) section 1.

1. **Welcome page** (existing): Connect hub → scan QR code or enter manually.
   - Manual entry separates address and token; pasting a complete `persona://pair?…` automatically splits it.
   - QR codes come from `personad hub pair`: format unchanged, token replaced with a single-use temporary token valid for 10 minutes, adding `hub` (hub_id), `fp` (public-key fingerprint), `tls` (TLS port), and `alt` (one or two external addresses).
   - Legacy codes (`--legacy` or old hub) still work, but only over plaintext LAN; settings show "Hub version is too old for direct internet connections."
2. **Step progress** (`StepProgress`, P0):
   - Steps: Parse QR code → Probe addresses → Verify certificate and hub → Exchange device token → Read machine list → Complete.
   - Probe addresses expands to show each candidate (public IPv6 / domain / LAN / public IPv4): Probing, Connected (round-trip time), Failed (reason). First connected wins; stop the others.
   - Each step has a timestamp; no fake percentages; stop animations under Reduce Motion.
   - Failure guidance by cause (classification in [android.md](android.md) 5.1):

     | Cause | Guidance | Button |
     | --- | --- | --- |
     | Address not found (DNS) | "Address not found" | Probe again |
     | Connection refused | "Hub is not listening on this port; check that it is running" | Probe again |
     | Timeout on home Wi-Fi | Check local network permission first; if denied, use the next row; if granted, use timeout guidance | — |
     | Timeout | "Cannot connect to hub. At home, make sure the phone and hub computer are on the same network; away from home, make sure your home router allows incoming connections" (ports, IPv6, and other details only in connection diagnostics, #4444) | Connection diagnostics |
     | Android lacks local network permission | "Connecting to hub at home requires allowing Persona local network access" | Authorize |
     | Certificate mismatch | "The service at this address is not your paired hub, or the hub changed certificates. Run `personad hub pair` again on the hub computer and scan the code" | Scan again |
     | 401 / 403 | "Pairing code expired or was already used; run `personad hub pair` again on the hub computer" | Scan again |
     | Incompatible old hub | "Update hub" | — |

3. **Explanation overlay**, "What pairing saves":
   - Phone: device token, hub public-key fingerprint, address list, in Keychain / Keystore-encrypted storage.
   - Hub: device credential (hash only), revocable under Settings › Devices; pairing notifies existing devices that a new device paired.
   - Push: iPhone registers a push token; Android does not and uses a persistent connection.
   - Plain HTTP addresses: "HTTP is unencrypted; use only on LAN or VPN. Outside, always use an encrypted connection."
4. **Readiness checklist** (P1):
   - Online state of each computer; whether node and hub came from the same deployment; herdr and Claude versions;
   - Notification permission; Android local network permission;
   - Android background settings ([android.md](android.md) 5.18): show actual state for queryable items (master notification toggle, channel levels, battery optimization, background restrictions). For unqueryable items (autostart, unrestricted mode, locking recent tasks, Huawei silent notifications and network retention during sleep, Honor smart data saving), give OS-specific steps and Open settings, with the user tapping Done;
   - **Lock-screen test**: after locking for 30 seconds, hub sends a test notification; ask whether it sounded and lit the screen, returning to the previous item if not;
   - **Connect away from home**: ask the user to disable Wi-Fi and test cellular, recording the result in connection diagnostics; suggest DDNS if no public address exists.
   - Every item can be skipped, with the resulting limitations explained.

### 3.8 Settings

- **Devices**: each shows platform (iPhone / Android / Watch), role (full / answer-only), last use. Revoking this device unpairs it and clears all local data in that scope.
- **Connection diagnostics** (Settings › Connection, P0):
  - Current route (public IPv6 / domain / LAN / public IPv4) and round-trip time;
  - Latest probe result and time per candidate;
  - Phone side: IPv6 availability, VPN use, captive-portal Wi-Fi;
  - Hub certificate fingerprint (first 16 characters, tap for full);
  - Current foreground / background heartbeat interval and last disconnect reason;
  - Probe again button.
  - Explain unrecoverable addresses: if home broadband changes prefix while the user is away without a DDNS name, wait until returning to LAN; suggest configuring DDNS for hub.
- **Notifications**:
  - iOS: unchanged.
  - Android: keep When you are needed and When an Agent finishes, also mapped to system notification channels (Needs you / Complete / Persona / Live status), with one-tap system settings. Replace Live Activities settings with Persistent lock-screen notification, What the status-bar capsule shows (reuse the `island` structure), and Show details on lock screen (Android's hidden-on-lock-screen variant).
  - Android also has Background persistence: battery optimization, autostart, vendor background settings, with current state and direct settings navigation ([android.md](android.md) 5.18).
- **Phone data**: list available sources per platform. Android has no Reminders; clipboard notes "Readable only while the app is open."
- **Style**:
  - Keep the five iOS styles.
  - Android first release has only Native (Material You), with Claude / Clay / Island / Lark in the second batch.
  - Component catalog (DEBUG only): browse all components × light/dark × text sizes; also used for screenshot tests.

### 3.9 Persona assistant tab

Structure unchanged. One change: first messages, Hand to Persona, and Ask Persona all use `MessageSender`, queuing offline like ordinary messages instead of failing immediately. Two more benefits come automatically through shared components: status line and pending state.

---

## 4. Component catalog (same names and parameters on both platforms)

| Family | Components | Notes |
| --- | --- | --- |
| Basics | `Surface`, `Card`, `Panel`, `PageBackground`, `Hairline` | Surface treatment follows style tokens (glass / solid / soft / hairline); Android Native uses tonal surfaces |
| Buttons | `PrimaryButton`, `SecondaryButton`, `DestructiveButton`, `IconButton` | Single-line text; shrink, switch to icon, or move into a menu if it does not fit; minimum touch target 44pt / 48dp; `IconButton` requires a screen-reader label; in-progress state such as Saving… disables the button |
| State | `StatusDot`, `StatusLabel`, `Chip` (neutral / tinted / outlined), `CountBadge`, `SessionAvatar`, `MonogramAvatar` | One state-color table; `CountBadge` visually shows 99+ but reads the full number; `MonogramAvatar` hashes names to colors |
| Page state | `EmptyState`, `ServiceState` (whole page: unpaired / hub unreachable / hub too old), `InlineError` (nearby + retry), `Notice` (caution / info), `CachedNote` ("Showing local cache · N minutes ago"), `ConnectionTitle` | Cancellation, timeout, and offline errors do not enter `InlineError`; connection state belongs in the title area |
| Pending state | `PendingBadge`, `PendingBubble`, `VerifySheet` | State and wording in 5.6; read-only verification entry + manual resolution |
| Flows | `StepProgress`, `ConfirmSheet`, `CountdownLabel`, `ReturnChip` | `ConfirmSheet` identifies what, where, and consequences, with red primary action + Cancel (`CloseAgentSheet` approach); two-step confirmation token, so stale confirmation cannot submit after state changes |
| Forms | `FormSection`, `LabeledField`, `SecretField` | Screen-reader labels, keyboard types; `SecretField` hides contents on blur and clears on leaving the page |
| Product components (inside Features, tokens only) | `ChatList`, `ComposerRow`, `StatusLine`, `WorkLine`, `InteractionPanel` / `InteractionSheet` (`QuestionForm`, `PlanForm`, `PermissionForm`, `ScreenForm`, `ElicitationForm`), `RequestInbox`, `DiffView`, `MonoText`, `FullTextReader`, `TaskProgressBanner`, `SearchOverlay`, `TerminalKeyBar` | One specification page per component (dimensions, states, screen-reader labels, minimum touch target, wrapping policy), in `docs/components/` |

---

## 5. State machines and wording

### 5.1 Session status line (`StatusLine`, synthesized by client)

Take the first matching state in priority order:

| State | Condition | Left-side text | On tap |
| --- | --- | --- | --- |
| `ended` | Session closed or replaced with a new one by `/clear` | "Session ended" / "Replaced with a new session" | Open new session / Resume |
| `machineOffline` | Node disconnected on the session computer | "Computer offline; state may be stale · N minutes ago" | Reconnect this computer |
| `submitting` | A write in this session has no receipt yet | "Sending…" / "Returning your choice…" | — |
| `unconfirmed` | An operation has an unconfirmed result | "Last operation's result unconfirmed" | Session-status overlay |
| `needsYou` | Pending request or herdr blocked | "Needs you: allow Bash" / "3 items need you" | Interaction panel / request list |
| `stopping` | Interrupt sent, still Working | "Interrupt sent; waiting for computer confirmation" | — |
| `working` | Working | "Working · 3 min 12 sec · 14 tools" | Open terminal |
| `waiting` | Seen, but `/loop` not due or background tasks running | "Looping · next 11:56" / "Waiting for 2 background tasks" | Details |
| (hidden) | Unread / Seen / Not running | Nothing | — |

Hub-unreachable state appears in the title area (existing `ConnectionTitle`), not again in this line. Claude spinner text (`Cogitating… 12s`) requires screen reading, currently done only when opening terminal (to save node work, `docs/archive/integrations/msg-render-plan.md` C4), so it is not shown in the status line.

### 5.2 Outgoing messages

```
draft ──send──▶ queued (persist in outbox before clearing composer)
queued ──online──▶ sending
queued ──offline──▶ waiting "Sends automatically when connected" ──reconnected and catch-up complete──▶ sending
sending ──hub receives (new hub returns accepted after persistence)──▶ accepted ──node finishes typing──▶ delivered ──same id or text appears in transcript──▶ shown (bubble becomes real message)
sending ──request never sent (DNS / connection refused / offline)──▶ waiting
sending ──sent without response (timeout / disconnect)──▶ unknown
    New hub (client_ops): automatically retry same id; hub returns original result ──▶ accepted / refused …
    Old hub: keep "Result unconfirmed," wait for transcript appearance, then ask user after timeout (Resend / Discard)
sending ──definitive rejection (input_draft / dialog_open / op_id_reused / invalid)──▶ refused "Not sent: <reason>" + optional action (Clear and send / Edit)
Before delivered, node reports delivered:unknown ──▶ verifying "Verifying" ──appears in transcript──▶ shown
                                                    └──deadline exceeded──▶ needsReview "Could not confirm delivery" + Resend / Discard
Cancel: only queued / waiting can be canceled directly; other states race cancellation against dispatch in the same hub transaction
```

### 5.3 Interaction requests

`pending(deadline)` → `answering` → `answered` (hook collected it; wording "Your choice has been returned") | `expired` | `resolvedElsewhere` (handled on computer or another device) | `handedToTerminal` | `unknown` (answer request receipt lost; query hub interaction list by request id: if absent, count as handled).

### 5.4 One-off operations (interrupt, rename, close, change mode / reasoning effort, stop loop, `/clear`)

`pending` → `sent` → `done` | `failed` (definitive failure, show reason) | `unknown` → `verifying` (reread authoritative state or query `GET /api/v1/ops/{id}`) → `done` | `failed`. If a session has an `unknown`, verify before another operation **of the same type**. Interrupt does not cancel queued messages.

### 5.5 Connection (existing, recorded in specification)

- Current iOS: four states `online` / `waitingForNetwork` / `connecting` / `updating`, 1.5-second debounce, weak network if ping gets no reply for 5 seconds, dead connection after 45 seconds without any message, 1→20-second backoff, immediate reconnect when network recovers.
- Android adopts this, plus:
  - Two more states: `needsLocalNetworkPermission` ("Local network permission required" + Authorize), `certificateMismatch` ("Hub certificate changed" + Scan again). Neither retries automatically; wait for user action.
  - Weak-network detection uses application ping ([hub-changes.md](hub-changes.md) 3.8); with unsupported hubs, it is known only after disconnect.
  - Dead-connection detection follows current heartbeat: 45 seconds in foreground; in background, no hub data within "negotiated heartbeat interval + pong timeout" ([hub-changes.md](hub-changes.md) 1.6).
  - A 401 stops reconnection and shows "Pairing no longer valid" + Scan again.
  - Tap "Connecting…" in the status line to open connection diagnostics (3.8).

### 5.6 Wording glossary

| Scenario | Text | Icon | Color |
| --- | --- | --- | --- |
| Sending | Sending | Spinner | Secondary gray |
| Queued offline | Sends automatically when connected | Clock | Secondary gray |
| Sent without response | Result unconfirmed | Clock-question | caution |
| Automatic verification | Verifying | Spinner | caution |
| Unable to verify | Could not confirm delivery | Exclamation circle | caution |
| Rejected | Not sent: <reason> | Exclamation circle | caution (actionable, not an error) |
| One-off operation offline | Cannot connect to hub; the last operation did not take effect | Info | info blue (existing rule, at most one in 4 seconds) |
| Interaction successfully returned | Your choice has been returned | Checkmark | positive |
| Handled elsewhere | Handled on the computer | Checkmark | Secondary gray |
| Expired | Expired; handle in terminal | Clock | Secondary gray |
| Task with unknown result | No result recorded (not Running) | — | Secondary gray |
| Derived view | Based on loaded records; may differ from the computer's current state | — | Secondary gray |

Icons: iOS uses SF Symbols (`clock`, `clock.badge.questionmark`, `exclamationmark.circle`, `info.circle`, `checkmark`); Android uses corresponding Material Symbols (`schedule`, `pending`, `error`, `info`, `check`).

---

## 6. Page model rules (same on both platforms)

1. Page models own and cancel their tasks on leaving, switching sessions, or entering background.
2. Capture `revision` before each await and compare on return; discard mismatches. Switching sessions, navigation, and backgrounding increment it.
3. Refreshes arriving while busy set a flag; run one coalesced follow-up after the current operation finishes.
4. Pages viewing the same session share a reference-counted subscription; unsubscribe only when the last leaves.
5. All internal event streams are bounded: a full buffer signals "Full reconciliation required," causing consumers to reread everything rather than silently dropping events.
6. Confirmation is two-step: `prepare()` creates a versioned confirmation object; `commit(token)` rejects if the version changed.
7. Cache derived data (rows, Markdown parsing, link/path recognition) by id and warm it in the background; no regex work during view evaluation.
8. Cancellation is not an error; connection state displays timeout/offline conditions.

| | iOS | Android |
| --- | --- | --- |
| Model | `@MainActor @Observable final class` | `ViewModel` + `StateFlow<UiState>` |
| Shared mutable state | `actor` | `Mutex` or `limitedParallelism(1)` |
| Bounded stream | `AsyncStream(bufferingPolicy: .bufferingNewest(n))` | `MutableSharedFlow(extraBufferCapacity = n, onBufferOverflow = DROP_OLDEST)` |
| Lifecycle | `scenePhase`, `.task(id:)` | `repeatOnLifecycle(STARTED)`, `LifecycleEventEffect` |
| Test gate | `CheckedContinuation` | `CompletableDeferred` |

---

## 7. Design token structure (`spec/tokens/`)

| File | Contents |
| --- | --- |
| `color.semantic.json` | Values by style × light/dark: `accent`, `userBubble`, `userBubbleText`, `pageBackground`, `card`, `cardStroke`; style-independent: `brand`, `danger`, `positive`, `caution`, `cautionText`, `ai`, `loop`, `countBadge`, `diff.*`, `hue.*`; `fill.well/chip/pressed` (iOS currently uses system fills, Android has no equivalent and needs explicit colors) |
| `color.status.json` | Needs you / Working / Unread / Seen / Not running × `dot` / `text` / `tint` |
| `dimension.json` | `radius` (6 / 10 / 14 / 20 / 22 + capsule), `space` (2…32), `minTouch` (iOS 44, Android 48), watch dimensions |
| `type.json` | Roles: `panelTitle`, `rowTitle`, `body`, `cardBody`, `preview`, `meta`, `chip`, `micro`, `code`, `greeting`; iOS maps to text styles (Dynamic Type), Android to sp `TextStyle` (font scaling) |
| `motion.json` | `state` 0.25s snappy, `layout` 0.3s smooth; all 0 under Reduce Motion |
| `surface.json` | Per-style surface treatment (glass / solid / soft / hairline), shadows, strokes |
| `rules.json` | Contrast rules: `onAccent / accent ≥ 3.5`, `userBubbleText / userBubble ≥ 4.5`, `accent / pageBackground ≥ 3`, `status.text / pageBackground ≥ 4.5`, checked across every style × light/dark |

Fixed-font exceptions keep the existing allowlist: terminal grid, Dynamic Island, share cards, Lark cards, and documents.

---

## 8. Accessibility and motion

- **Accessibility text sizes**: iOS uses `isAccessibilitySize`; Android uses `fontScale` above a threshold (to be determined by testing). When triggered:
  - Horizontal segmented controls become vertical;
  - Hide decorative leading icons;
  - Allow titles and paths more lines;
  - If buttons do not fit, use icons, shorter text, or menus; **never wrap**.
- **Reduce Motion**: iOS reads `accessibilityReduceMotion`, Android the system animation scale (when 0). Design system supplies a `reduceMotion` environment value. Stop Needs-you dot breathing, non-spinner animations, and step-progress light dots.
- **Screen readers**: hide decorative icons; read full counts; step progress reads one text status line; state dots require text (list metadata includes Needs you).
- **Haptics**: success for sending and successful answers, warning for new interactions, error for failures. Android uses `HapticFeedbackConstants.CONFIRM / REJECT`.

---

## 9. Android-specific forms

| Area | iOS | Android |
| --- | --- | --- |
| Back | System back; manually restore edge-right-swipe after a custom chat header | Predictive back (`PredictiveBackHandler`), no custom edge gesture; overlays and full interaction pages dismiss with system back, saving drafts by request id |
| Chat to files / Git | Swipe left | System back owns screen edges: use ⋯ menu and a top three-way switch (Chat / Files / Git), no whole-page horizontal swipe |
| Text selection | Long-press Select opens plain-text page (SwiftUI Text cannot select across blocks) | `SelectionContainer` selects across paragraphs in place; no Select entry in long-press actions |
| Long-press message | Dark action bar (Copy / Select / Quote / Share / Edit) | Same action bar (`Popup`), minus Select |
| Overlays | sheet + detents | `ModalBottomSheet`; links need interaction behind them, so use a nonmodal draggable two-detent overlay (android.md 5.6) |
| Swipe actions | Multi-button row swipe | First release long-press menus, full-swipe Done via `SwipeToDismissBox`; multi-button swipes in second batch |
| Notifications | APNs; categories + buttons; direct input only for Send back | Persistent connection only, no Google-services dependency (android.md 5.8); channels + actions + `RemoteInput`: denial reasons and plan comments both entered in notifications; question/screen-card options tappable; withdraw after handling |
| Live Activities | Lock screen + Dynamic Island | Persistent notification; request Live Update on Android 16+, with only short status-bar capsule text (≤ 6 characters most reliable) |
| Hub connection | Plaintext LAN or VPN, one address (unchanged this round) | Direct internet, IPv6 preferred: multi-address probing, TLS public-key pinning, diagnostics; home Wi-Fi requires local network permission (3.7, 3.8, 5.5) |
| Background | Push wakeup | Foreground service with persistent connection, hub-originated background heartbeat; Chinese vendor systems need autostart and battery restrictions relaxed (readiness checklist, android.md 5.18) |
| Installation and updates | TestFlight | No Play Store: hub-hosted APK, in-app update checks (android.md section 9) |
| Fonts | System font + Dynamic Type | System font + sp; bundled JetBrains Mono for terminal |
| Icons | SF Symbols | Material Symbols (map roughly one hundred icons individually, with a mapping table) |
| Persona tab sidebar | Custom draggable sidebar (like ChatGPT), a column in wide-screen dual-column mode | `ModalNavigationDrawer`; persistent column on wide screens |
| Wide-screen dual columns | iPhone Duo shows list + detail based on width, not device | Navigation 3 list-detail layout, also width-based (tablets, foldables) |
