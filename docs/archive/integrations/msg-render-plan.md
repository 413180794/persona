# Message reading and rendering pipeline: diagnosis and rework plan

Lane: `lane/msg-render` (cut from master cd82927). Researched 2026-10-29, compared against Ovis (a herdr plugin: `shared/protocol.ts`, `server/conversation.ts`,
`server/claude-transcript.ts`, `server/codex.ts`, the ChatView in `src/`).

Markers: **[Confirmed]** = traced through the code path, or verified with real local transcripts; **[Suspected]** = the mechanism holds, needs reproduction.
"Local sample" means statistics over this dev machine's `~/.claude/projects` (208 transcripts, about 60,000 lines) and `~/.codex/sessions` (2 rollouts).
The statistics script is at the end; the samples are not committed (they get redacted before committing, see Stage 0).

---

## 1. Current state: the end-to-end pipeline

```
Each computer (personad node)                     hub (personad hub)                      iPhone / Watch
──────────────────────────                     ─────────────────                      ──────────────
~/.claude/projects/*/<id>.jsonl ─┐
~/.codex/sessions/**/rollout-*.jsonl ┤ transcript.File.Refresh()        RPC session.items     HTTP GET /sessions/{m}/{s}/items
~/.trae/cli/sessions/...  ───────┘   appends whole lines by byte offset  (Page/PageTurns/After) ──▶ (turns=2 / before_seq / after_seq)
                                     ParseLine → []proto.Item
                                     (seq = line number, id = uuid#block index)
                                     all items held in memory n.files
watchLoop: Refresh every 250ms ────▶ event session.items ──────▶ itemsToSubscribers ──▶ WS {"type":"items"}
           read terminal every 400ms ▶ event session.screen ────▶ toSubscribers ──────▶ WS {"type":"screen"} (unused by App)
pollAgents: herdr snapshot every 1s ▶ sessions.changed / revs ─▶ broadcast ──────────▶ WS sessions / revs → AgentSync pulls after_seq
hooks (PreToolUse / PermissionRequest) ▶ interaction ─────────▶ card / push
```

- **Locating**: session id from the herdr claude integration → pane bound by a hook → `~/.claude/sessions/<pid>.json` → newest transcript in the same directory as fallback
  (`node/sessions.go` `setAgents` / `sessionFromProcess` / `guessSession`). Codex uses herdr's id or the newest rollout in the same directory (`node/codex.go`).
- **Parsing**: `server/internal/transcript/transcript.go` (Claude), `codex.go` (Codex / compatible Codex forks). One JSONL line → 0..n `proto.Item`s, append-only, never modified.
- **Paging**: `File.PageTurns` (used by the App, pages by "one question, one answer"), `File.Page`, `File.After` (incremental / backfill, capped at 500).
- **Push**: when the first subscriber arrives, the hub has node `session.watch(since=lastSeq recorded by the hub)`; each subscription additionally starts `sendBacklog(since=the App's lastSeq)`.
- **App**: `ChatStore.merge` dedups by id, append-only; the disk cache only keeps the last two turns (`ChatCache`); rendered with an inverted ScrollView + LazyVStack;
  `ChatBlock.build` groups items into "turns" (work lines + answer).

The fundamental difference from Ovis: Ovis **re-reads the whole latest page** every time (turn/part, no ids), the WS only pushes a "changed" signal, ETag/304 saves traffic,
and `history_id` (file dev-ino + rewrite generation + clear/fork suffix) detects history changes so old pages are discarded wholesale. Persona **streams incremental appends by seq**,
with no history identity and no continuity check, so any single missed delivery is permanent.

---

## 2. Issue list

### A. Incremental updates: lost messages / gaps (most severe)

| # | Issue | Evidence | Repro |
| --- | --- | --- | --- |
| A1 | **[Confirmed] WS backfill over 500 items is silently truncated, leaving a permanent gap**. node's `After(since, 500)` returns `more_after`, but the struct hub's `sendBacklog` decodes into has no such field, so it is dropped; the App merges as usual, `lastSeq` advances, and from then on only increments arrive; AgentSync also skips already-subscribed sessions (`AgentSync.swift:71-72`) | `server/internal/hub/http.go:595-621`; `node/node.go:285-289`; node's own catch-up likewise sends only 500 with no marker (`node/control.go:425-428`, `403-405`) | Open a chat page → send the App to background → have Codex make 250+ tool calls (2 items each) → back to foreground: a large chunk is missing in the middle, and scrolling up does not find it |
| A2 | **[Confirmed] `File.After` splits multiple items from the same line**. `Page`/`PageTurns` handle "don't split a line"; `After` does not. Each Codex tool call = use + result with the same seq; when the cap lands right between them, the result is lost and the tool shows "running" forever (the next batch starts after `last.Seq`) | `transcript/transcript.go:927-941` (compare `869-889`) | Same scenario as A1; a unit test can construct it directly (Stage 0) |
| A3 | **[Confirmed] On cold start there is a gap between the HTTP first page and the disk cache**. Before the WS gets hello, `subscribe` goes through `loadInitial` (only the latest two turns) and merges with the old two turns in the cache; if HTTP returns first, `didConnect` uses the new `lastSeq` and the turns in between are never backfilled; `loadOlder` pages back from the earliest cached item, also skipping the gap | `apple/Persona/App/AppModel.swift:187-194`, `205-213`, `215-226`, `230-241` | Kill the App → have the Agent run 3+ more turns on the computer → cold start by tapping a push: "old two turns + new two turns", the middle is gone |
| A4 | **[Confirmed] After a transcript is truncated / replaced, the server renumbers from line 1 and the App does not know**. There is no history identity (Ovis's `history_id`); when the App's `after_seq` exceeds the new file's line count it gets nothing; fallback ids like `line<seq>#n` can also collide with old ones and get swallowed by dedup. Only "got smaller" is checked; same-size or larger replacements go undetected | `transcript.go:834-836`; `ChatStore.swift:278` | Rare (Claude Code does not rewrite; external tools, `--fork-session`, manual edits trigger it). Construct in a unit test |
| A5 | **[Confirmed] With `watch(since=0)`, new lines parsed by Refresh are not pushed**: backfill only happens `if since > 0`, but Refresh has already consumed those lines, and watchLoop then only pushes newer ones. Reached when `lastSeq` is 0 after a hub restart; the App-side `sendBacklog` usually fills it, but it is timing-dependent | `node/control.go:424-428` | The Agent is producing output at the moment the hub restarts |
| A6 | **[Logic confirmed, likely] With two chat pages open for the same session, the subscription is cancelled by whichever page closes later**. The App's `subscribed` is a Set and the hub has one bool per client per session, with no reference counting; the new page subscribes in onAppear, the old page unsubscribes in onDisappear afterwards, so the page being viewed stops updating live, and `visibleSession` is cleared (pushes pop as usual) | `AppModel.swift:43`, `187-203`; `ChatView.swift:181-186`; `hub/http.go:553-578` | Chat page → top companion bar into the space page → tap the same Agent again; or tap this session's push from the space page |
| A7 | **[Confirmed] Intermediate replies show twice in the assistant tab**. `message_stop` sends the whole text as a delta, `message_start` only clears the buffer without an event, and only `result` sends an empty string; when a message is "text + tool_use", `rows.last` is the tool row, `ChatView`'s dedup check fails, and while the tool runs the text appears both as a real row and again with a cursor | `node/assistant.go:315-350`; `ChatView.swift:734-741` | Ask the assistant "tell me what you're going to do first, then check my calendar" |
| A8 | **[Suspected] The client has no heartbeat**. When switching networks in the foreground, `receive` may hang for a long time while `state` stays `.connected`, and the chat page silently stops updating; the hub disconnects slow consumers outright (`http.go:364-372`), also relying on App reconnect backfill (affected by A1) | `apple/Shared/LiveConnection.swift:123-126`, `158-189` | Switch Wi-Fi ↔ cellular while in the foreground |
| A9 | **[Confirmed] After an in-App reconnect, already-handled interaction cards linger**: `didConnect` does not re-fetch the interaction list; only `refresh` on returning to the foreground corrects it | `AppModel.swift:230-241`; `hub/http.go:521-523` | Lose network in the foreground → answer the question on the computer → restore network: the card is still there, tapping it gives 410 |
| A10 | **[Confirmed, low] Small pitfalls in ordering and cache trimming**: items with the same seq are sorted by id string, so `#10` sorts before `#2` (`ChatStore.swift:295`); `ChatCache.tail` does not "keep a line together" (`ChatStore.swift:607-614`); pending messages are matched with two-way `contains`, so short messages like "ok" or "continue" get cleared by any message containing them (`ChatStore.swift:637-641`) | Same as left | Unit test |

### B. Parsing: missed / misidentified messages

| # | Issue | Evidence |
| --- | --- | --- |
| B1 | **[Confirmed] `system/local_command` is dropped entirely**: newer Claude Code writes slash commands (`/model` etc.) and their output as `type:system, subtype:local_command`, with content `<command-name>…` / `<local-command-stdout>…`; the default of `parseLine`'s system branch returns nil. Ovis handles it specifically (`claude-transcript.ts:20-50`, `317-322`) | `transcript.go:283-299`; 6 in the local sample (2.1.259) |
| B2 | **[Confirmed] `api_error` shows as an empty "API error:"**: the error is in `error.formatted` / `error.message`, not in `content`; and each retry (`retryAttempt` 1..10) writes one, flooding the view with a series of them | `transcript.go:286-287`; 1 in the local sample |
| B3 | **[Confirmed, scope corrected] The same background task notification shows twice**: the same `<task-notification>` is written both as a user record and as `attachment(queued_command, commandMode=task-notification)`, and each becomes a notice. **But the same task-id can also have several real notifications with different content** (a sub-Agent first reports "still waiting for the background task", then "done"; Stage 0's `claude-notifications.jsonl`), so we cannot dedup by task-id like Ovis; only notifications with identical content are merged | `transcript.go:242-245`, `321-327`, `353-357`; 95 task-ids appear more than once in the local sample (on 2026-10-29 Claude Code automatically cleaned up transcripts older than 30 days, so the original sample can no longer be checked one by one for which are true duplicates) |
| B4 | **[Confirmed] `system/scheduled_task_fire` (/loop waking up on schedule) is not shown**: the user cannot see why this turn started | `transcript.go:298`; 6 in the local sample |
| B5 | **[Confirmed] Codex `Extension` (web.search etc.) is not shown**; FileChange numbers items by iterating a Go map, so **two parses of the same rollout map ids ↔ files differently** (after a node restart an id points at a different file) | `transcript/codex.go:260-272`; 20 Extension items in the local sample |
| B6 | **[Confirmed, low] `continued-in` is not followed**: when a session continues under another session id, Ovis reads the last 64 KiB and follows it; Persona stays on the old file | 1 in the local sample |
| B7 | **[Confirmed, low] Both branches of a fork created by /rewind or re-sending after Esc are shown**. Ovis follows the current branch along `parentUuid` and hides the abandoned part | 1 place in the local sample |
| B8 | **[Confirmed, coverage gap] Parsing tests are all hand-written strings, with no real sample fixtures**; Claude Code versions move fast (the local sample already has new types such as `atis-latch`, `mode`, `api-request-blob`, `cost-state`), and there is no regression protection | `transcript/transcript_test.go`, `codex_test.go` |

Already correct (no change needed): messages absorbed by queued_command, showing non-empty thinking, `isSidechain` filtering + subagents in separate files, unwrapping pasted_content,
`<bash-input>` / `<bash-stdout>`, cross-session messages, `[Request interrupted…]`, the compact notice, Codex commentary → thinking, compatible Codex forks `history_mutation` dedup.

### C. Paging and large-session performance

| # | Issue | Evidence |
| --- | --- | --- |
| C1 | **[Confirmed] node memory only grows**: the `n.files` cache holds every item of every session ever opened (including base64 images up to 1.5 MB) and is never evicted; since the App uses `lazy_images`, images are only needed for `session.image`, yet they stay resident | `node/node.go:35`; `node/sessions.go:229-256`; `transcript.go:441-446` |
| C2 | **[Confirmed] Opening a large session for the first time reads and parses the whole file** (`io.ReadAll`; the largest local transcript is 20 MB+, a Codex rollout 21 MB), just to return the last two turns | `transcript.go:822-866` |
| C3 | **[Confirmed] Long tool output (≤24 KB) is sent in full with the page**; a two-turn page can be up to 2000 items. Ovis sends 4000 characters inline + `output_ref` to fetch the full text on demand | `transcript.go:20-22`; `node.go:291-293` |
| C4 | **[Confirmed] While subscribed, node reads the terminal every 400 ms (a herdr process call) and pushes it to the App, which never reads it** | `node/control.go:470-487`; `AppModel.swift:289-293` (only writes `store.screen`; no view reads it) |
| C5 | **[Confirmed] Heavy work on the App's main thread**: every event rebuilds rows / blocks / lines in full; the body runs `store.items.compactMap(\.image)` and a regex for filtering pending messages; `StreamingText` re-parses Markdown every 80 ms; an expanded `CodeBlock` is one giant Text; the `chats` dictionary is never evicted | `ChatStore.swift:304-313`; `ChatView.swift:127-130`, `166`, `720`; `MessageViews.swift:586-593`; `ToolViews.swift:288-292` |

### D. State detection

State comes from herdr (`agent_status`) + hook interactions + the hub's "seen" (`hub/state.go`); the mechanism matches Ovis (Ovis is also herdr-first, plus a CompletionTracker).
**Routing questions / plans / authorization through hooks is where Persona is stronger than Ovis** (Ovis relies entirely on screen recognition + simulated keystrokes); keep it. No confirmed state bugs were found; a few risks are noted:

- D1 [Suspected] `quickMeta` only reads the last 384 KB and skips user lines larger than 8 KB (`node/sessions.go:157-176`): when the last thing in a turn is a large tool result,
  `UpdatedAt` is earlier than the real end time, and the "seen" check (`state.go:24`) counts "seen before it actually finished" as seen, so what should be Unread shows as Seen. Stage 1 verifies with a fixture.
- D2 Codex questions can only use the generic "terminal waiting for you" card; Ovis's dialog reader (`prompt.ts`, `dialogs/*.ts`) could be borrowed for structured cards. **Out of scope for this lane**; noted.
- D3 The chat page's `liveTurnID` and spinner only look at `session.status` in the list (polled every second, pushed only when `listKey` changes), which is a different source from the chat content, so there is a one-beat delay; acceptable.

### E. Rendering stability

| # | Issue | Evidence |
| --- | --- | --- |
| E1 | **[Logic confirmed, UI suspected] The row id of the "freeze" anchor changes**: when a working turn receives a new reply, the previous answer row is folded into the work lines; when the turn ends, `w-*` becomes `mu-` / `wt-`; `firstIndex` cannot find the anchor, the freeze stops working, and the content the user is reading while scrolled up gets pushed away | `ChatView.swift:226-260`, `718`, `833` |
| E2 | **[Confirmed] A saved reading position stays pending forever**: when the saved row is not in the two cached turns, `entry` is never cleared; later, when manually scrolling up loads that page, the list is forcibly yanked there, and following is turned off | `ChatView.swift:498-509`, `836` |
| E3 | **[Suspected] Streaming text / pending bubbles are not bound by the freeze**, and push things around while scrolled up too | `ChatView.swift:728-741` |
| E4 | **[Confirmed] The watch chat page fetches only once on open**: it does not refresh, the state does not change, and `<persona-context>` / `<attached_images>` in the raw text are not stripped | `apple/PersonaWatch/PersonaWatchApp.swift:370-373` |

---

## 3. Direction: keep the incremental stream, add "identity + continuity"

Not copying Ovis's "re-read the whole page + position key". Reason: the phone needs background sync, offline cache and low traffic, which incremental-by-seq suits well; Claude transcripts are append-only,
and an item never changes once sent, so the model is right. Three things are worth borrowing from Ovis:

1. **History identity** (`history_id`): file identity (dev+ino) + rewrite generation; when it changes, the client discards everything and starts over, instead of continuing with wrong seqs.
2. **Verifiable continuity**: each batch of items declares "which seq it follows"; when the client sees it does not connect, it backfills on its own, and if backfill fails it replaces the whole page. Any missed delivery self-heals instead of becoming a permanent gap.
3. **Real-sample tests + incremental read = cold read**: reading a file in one go and reading it by appending line by line yield exactly the same items (Ovis `claude-incremental.test.ts`).

---

## 4. Staged plan

Each stage is committed separately and can be rolled back separately; server changes stay compatible with old Apps (new fields optional, old fields keep their meaning).

**Progress** (2026-10-29): Stage 0 merged (PR #5), Stage 1 merged (PR #6), Stage 2 committed on the lane branch (not merged into master). Where Stage 2 differs from the plan:
the field names are `from_seq` / `to_seq` (`to_seq` includes lines with no items, and empty batches are pushed too); the protocol is in `docs/protocol/protocol.md` ("Continuity of items");
subscription reference counting is done only in the App (the hub already dedups per session for each connection); heartbeats rely on the hub's websocket library answering pong automatically.

Stage 3 committed on the lane branch (not merged into master). Measured (`go test ./internal/transcript -run ColdOpenCost -v -args -cost`, a 20.8 MB transcript with screenshots):
resident memory 18.7 MB → 0.5 MB (items no longer keep image data resident; they are re-read by line offset); node keeps only the 16 most recently used sessions (sessions being watched are not evicted);
tool output in a page is cut to 4 KB, ending with a line "…（已截断，点开看全文，共 N 字节）" ("…(truncated, tap to see the full text, N bytes in total)", matched by the App) + `full_size`; the App fetches `/items/{id}/full` when **Truncated. View full text** is tapped;
the App rebuilds rows / blocks only for the last turn (a 1000-turn session went from 21 ms → 0.4 ms per event, simulator Debug), the chat page no longer recomputes the image list on every item, and only 8 off-screen sessions are kept in memory.
**C2 was done later** (2026-10-29; real data overturned the judgment below: a 317 MB session on one dev machine took 22 seconds to cold-open and the hub timed out with 502):
cold-opening a large file only counts lines and parses just the last 8 MB window; paging back / after_seq / viewing full text parse earlier windows on demand, and Meta is computed over the whole file only the first time it is requested.
The design and "why it is exactly the same as reading the whole file" are at the top of `server/internal/transcript/window.go`; verification numbers in `docs/archive/integrations/msg-render-verification.md`.
(The original judgment:) windowed parsing on cold open (C2) was not done: the first page of 20.8 MB takes 236 ms, spent on per-line JSON parsing (mostly the screenshots' base64),
and session-level Meta (turn count, title, background tasks, queue) needs a full pass anyway; windowing would save at most a few dozen milliseconds, at the cost of incomplete Meta and complex paging logic,
and with the LRU each session pays it only once on cold open. Caching `StreamingText`'s Markdown parsing per paragraph was not done either (it only happens while the assistant streams, and a single paragraph is short).

The number tables and per-category difference notes from the real-data verification (435 files, 5.8 GB) are in `docs/archive/integrations/msg-render-verification.md`.

Stage 4 committed on the lane branch:
- E1: the freeze became "a snapshot of items when leaving the latest end" (seq + block index + the turn running at the time); while scrolled up, the chat is drawn from the items in the snapshot, and later ones appear once you return to the latest end;
  earlier pages and backfilled gaps appear as usual. Measured with `-showFreezeProbe` (`MOCK_STREAM=1`): 12 items arrived while reading history; the row in the middle of the screen moved 319pt before the fix, 0pt after.
- E2: a saved reading position that cannot be found in the first page (the hub's latest page) is abandoned, and also abandoned as soon as the user scrolls.
- E3: while frozen, streaming text, "Thinking" and pending bubbles are not inserted at the latest end (sending a message yourself returns to the latest end first, so it is unaffected).
- E4: the watch chat page re-fetches following the session's rev (the session list refreshed every 8 seconds carries rev), the state refreshes with it, `<persona-context>` shows as 〔title〕, and image captions and `[Image #n]` show as [Photo].
  **No watch screenshots**: this Mac has no watchOS simulator runtime (several GB; not downloaded without asking); only confirmed the watch target compiles and `WatchText` has unit tests.

### Stage 0: test infrastructure (first)

- **What changes**
  - `scripts/diag/redact_transcript.py`: redacts real transcripts / rollouts into fixtures: keeps structure, types, uuid relations and line order; replaces text with same-length placeholders,
    paths / host names / emails with placeholders, base64 images with a 1×1 PNG; prints remaining suspicious strings for manual review.
  - `server/internal/transcript/testdata/`: one each for Claude (with local_command, api_error retries, task-notification in three places, queued_command, compact,
    scheduled_task_fire, tool_result with an image, subagent), Codex 0.15x/0.16x (with Extension, multi-file FileChange, turn_aborted) and compatible Codex forks.
  - Golden tests: fixture → JSON snapshot of the item list (`-update` refreshes); **incremental = cold read** tests (appending byte by byte / line by line gives the same result);
    property tests that `After` / `Page` / `PageTurns` paging has no gaps or overlaps.
  - Write A2, A5 and B1–B5 as failing tests first.
- **Verification**: `./scripts/test/test-server.sh ./internal/transcript/...` (Go is only on the hub machine).
- **Risk**: incomplete redaction brings personal information into the repo. The script replaces by allowlist (only JSON keys and enum values are kept), and we grep manually before committing; each fixture stays < 200 KB.

### Stage 1: server-side parsing and paging correctness (no protocol change)

- **What changes**
  - `After` does not split a line; backfill / increments uniformly return `more_after` (A2). node watch's catch-up loops until done (node's half of A1), and pushes new lines from Refresh even when `since=0` (A5).
  - Parsing: `local_command` (command name as a user line, stdout as a notice, terminal control sequences stripped) (B1); `api_error` takes `error.formatted`,
    and retries of the same request merge into one "API error (retried n times)" (B2); task-notifications merge only when the content is identical (B3); `scheduled_task_fire` shows "/loop woke up on schedule" (B4);
    Codex Extension `web.search` shows as a search tool, FileChange numbered by sorted path (B5).
  - Check D1 with fixtures; if needed, quickMeta's activity time stops skipping large lines and only skips parsing their content.
- **Verification**: all Stage 0 failing tests turn green; review the golden snapshot diff item by item; deploy to the local node, open sessions containing these types with `-openSession`, simulator screenshots (dark / light).
- **Risk**: parsing changes alter the id / seq of existing items → old items in the App cache coexist with new ones as duplicates. Constraint: **the id rules for existing items do not change**; only new items are added;
  dedup (B3) only sends less, and duplicates already in the App cache stay (gone after clearing the cache), which is acceptable.

### Stage 2: identity and continuity in the protocol (server + App, gaps self-heal)

- **What changes**
  - Server: one `history` per session (the transcript's dev:ino + rewrite generation; when "got smaller / same size but mtime changed / file replaced" is detected, the generation goes +1 and it re-reads from the start) (A4).
    `items` events and HTTP paging responses both carry `history` and `after_seq` (which seq this batch follows; present for backfill, increments and live pushes).
    hub `sendBacklog` passes `more_after` through (A1).
  - App `ChatStore`: records `history` and the contiguous range. On receiving a batch: different `history` → clear and start over (cache cleared too); `after_seq` > local `lastSeq` → gap:
    put it in a pending-merge area first and loop HTTP `after_seq` to fill in (continuing while `more_after`); if it cannot be filled (too many), replace with the latest page and keep `hasMore`.
    The same applies when `loadInitial`'s result is not contiguous with the cache (A3). Sorting becomes a numeric `(seq, block index)` comparison (A10).
  - Subscriptions become reference-counted (the App counts per key, the hub also counts per client) (A6).
  - Heartbeat: the App sends a ping every 20 s, the hub answers pong; two missed answers disconnect and reconnect (A8). `didConnect` re-fetches the interaction list (A9).
  - Assistant streaming: send an empty delta on `message_start`; the App clears liveText after receiving the item that contains it (A7).
  - Read the terminal only when someone wants it: the chat page does not use `screen`, watch does not sample by default, and places that need it (the terminal page) subscribe explicitly (C4).
- **Verification**
  - Server: unit tests for paging / backfill / rewrite generation (including "backfill > 500", "rewrite after truncation", "same-size replacement").
  - App: `ChatStoreTests` gets cases for gaps, out-of-order, history change, duplicate batches, cold-start cache + latest page.
  - End to end: `scripts/mock/mock_hub.py` gets switches for "deliberately drop a batch / send only the first 500 / change history"; run through on the simulator and the chat is complete;
    then follow the A1, A3 and A6 repro steps on a real hub and compare screenshots before and after the fix.
- **Risks**
  - Old App + new hub: the old App ignores the new fields and behaves the same; new App + old hub: without `history` / `after_seq`, fall back to the current logic, and do not treat it as a gap and backfill frantically.
  - The gap-filling loop needs a cap and backoff, so it does not keep hammering an offline machine.
  - **The largest conflict surface with the other lane** (`server/internal/hub`, `node`): rebase onto the latest master before this stage starts; changes are concentrated in `transcript/`,
    `hub/http.go`'s sendBacklog / subscriptions, `node/control.go`'s watch and `node/node.go`'s session.items; no other files are touched.

### Stage 3: large-session performance

- **What changes**
  - node: `File` records each line's byte offset, items do not keep image data resident, and `session.image` re-reads that line by offset (C1); `File`s not being watched are evicted by LRU (16 kept).
  - Cold open searches backwards from the end of the file for turn starts and parses only the last window (cf. Ovis's 16 MB window); paging back parses earlier windows on demand; incremental reads must match cold reads (backstopped by the Stage 0 tests) (C2).
  - Long tool output: cut to 4 KB in the page, with `full_size`; new `GET /api/v1/sessions/{m}/{s}/items/{id}/full`, fetched when the App expands it (C3).
  - App: rows / blocks rebuild only the last turn (earlier turns unchanged); `compactMap(\.image)` and pending-message filtering move out of body; `StreamingText` re-parses Markdown only when a paragraph ends;
    very long `CodeBlock`s lazy-load in segments; the `chats` dictionary evicts sessions that are not on screen (C5).
- **Verification**: time + memory on node for a 20 MB transcript (before / after the fix); in the App, Instruments Time Profiler while scrolling and streaming in a 1000+ turn session, comparing main-thread hitches;
  `-showScrollToOldest` pages all the way to the earliest item without stutter or blank screen.
- **Risk**: windowed parsing makes Meta (title, background tasks, queue) incomplete when "parsing starts in the middle". Meta is still computed separately by quickMeta / bgScan and does not depend on the window;
  after long output is truncated, old App versions see truncated text (with a clear "truncated, tap to see the full text" note that old Apps can understand too).

### Stage 4: rendering stability

- **What changes**: the freeze anchor uses an id that does not change, such as "this turn's user message id + offset" (E1); restoring the reading position is only attempted in the first layout after opening, and is void as soon as the user scrolls (E2);
  streaming text / pending bubbles are not inserted into the visible area while frozen (E3); the watch chat page subscribes to rev refreshes, follows state, and strips internal tags (E4).
- **Verification**: simulator screenshots (dark / light, iPhone and watch); a script cold-starts and opens sessions in batch and detects blank screens by pixel standard deviation (the method in `docs/lessons/ios.md`);
  scroll up while an Agent is working and confirm with a screen recording that visible content does not move.
- **Risk**: the inverted list's scroll logic has historically had the most pitfalls (see `docs/lessons/ios.md`); every change needs a regression pass over those entries.

### Stage 5 (optional, decided after seeing the earlier results): following session identity

- Follow `continued-in` to the new file (B6); when `/clear` in a pane switches sessions, the chat page shows a "switched to a new session" line with one tap to jump there; /rewind forks show only the current branch (B7).
- Risk: it changes the session list's identity model (several session ids in one pane) and overlaps with the other lane's node changes; reassess after that lands.

---

## 5. Points for the user to decide

**Decided** (2026-10-29): 1 agreed; 2 agreed, and the truncation point must carry a "truncated, tap to see the full text" note that old Apps can understand too; 3 not in this lane, reassess later.

1. Whether to agree to "keep the incremental stream + add identity and continuity", rather than switching to Ovis's whole-page re-read turn/part model (the latter amounts to rewriting the protocol and the App's rendering layer).
2. Stage 3's cutting long tool output to 4 KB + fetching the full text on demand: it changes what old App versions see (they only see the truncated text).
3. Whether Stage 5 is part of this lane.

## Appendix: how the local sample statistics were gathered

A one-off Python script (not committed): walks `~/.claude/projects/*/*.jsonl` and counts `type/subtype`, content block types, attachment types,
how many times the same `<task-id>` appears in visible records, and prompts re-parented via `parentUuid`; walks `~/.codex/sessions/**/*.jsonl` and counts `type/payload.type/item.type`.
Stage 0 turns it into `scripts/diag/transcript_stats.py`, to run after Claude Code / Codex upgrades to see whether there are new types.
