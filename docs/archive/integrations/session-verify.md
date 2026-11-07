# Real-data verification of session reading and message sending (2026-10-31)

User: "The session reading logic doesn't feel right. Use my large session history to test whether the current Claude Code and Codex reading logic is right, and whether the message sending logic is right",
adding "and also the message sending path". The corpus is real sessions from 6 machines, kept only locally in `~/persona-corpus/verify` and not in the repo; this doc has only numbers and made-up reproductions.

## 1. Reading: cross-check

The tools live outside the repo (one-off):

- On the hub machine, fetch a session the way the app does (`turns=2&fold_work=1&lazy_images=1`, then page back with `before_seq` until `has_more: false`,
  expanding each fold by `from_seq/through_seq`), then fetch the whole thing again with `after_seq` from 0; afterwards copy the raw jsonl back (files only grow, so truncate at `to_seq`).
- A reference parser written separately in Python (not reusing the Go parser): Claude's user / assistant / attachment(queued_command) / system lines, and both Codex formats
  (the old response_item + event_msg, the new `item_completed`). It compares, item by item, role, type, body (whitespace stripped, truncated ones by prefix), tool id pairing, seq, timestamp and order;
  it also checks whether the app-style pagination (after expanding folds) and the whole after_seq read match item for item, whether there are duplicate ids, and whether `has_more` is true to fact.

| Item | Count |
| --- | --- |
| Sessions | 315 (Claude 174, sub-Agents 73, Codex 68), 6 machines; on each, the longest / newest / most varied + random picks |
| Raw data | 2.9 GB, 616k lines; node produced 236k items |
| Requests | 1,475 app-style pages, 9,912 fold expansions, 722 after_seq pages |
| Cases covered (number of sessions containing each) | compact 26, queued messages 35, images 42 + Codex images 36, interrupts 15, local commands 18, slash commands 209, thinking 167, background task notifications 151, pastes 134, resume / fork 78, sub-Agents 73, Codex interrupts 21 / compaction 26 |
| User messages, replies, tool pairing, order, timestamps, seq, app pagination vs whole read | **0 mismatches** apart from the table below |

| Mismatch | Count | Conclusion |
| --- | --- | --- |
| The same session id has files in two Claude config directories (resumed under another profile: the old file stops, the new one carries on), and node took the first by directory order | 45 ids on one machine (19 in use in the last 7 days, 36 resolved to the old file); 3 sub-Agent 502 "not found"; 5 ids listed twice in the session list | **Fixed**: take the most recently modified copy; paths not reported by a hook are rechecked after 30 seconds; the offline scan lists each id only once |
| Paging back: lots of large lines that produce no items between two turns (Codex's post-compaction history lines); one page parsed its full 24 MB budget without finding an item, returned an empty page + `has_more: true`, the app had no seq to continue from, and older history was unreachable | 3 of 76 large Codex rollouts on one machine | **Fixed**: when there is earlier content, a page carries at least one item (same for count-based pagination). Rechecked on the real files: 58 / 6 / 23 pages to the start, item counts equal the whole read |
| On one machine (the hub's ssh to it does only ~15 KB/s), an 87 KB page took 5.6 seconds, and a 500-item after_seq page exceeded the hub's 30 seconds → 502 | 1 machine | A network issue; the ssh the hub uses to start node got `Compression=yes` (measured 3.6x faster on transcript JSON, nearly free on a LAN) |
| A fold ends mid-line (there is one more text item after it on that line), so expanding by line brings that item back as well | 4 | The app merges by id and never shows it twice; **not changing** |
| Tool result without a matching call | 1 | That line in the source file was already broken (half-written JSON) |
| Tool call without a result | 4 | The session was killed / is still running; normal |

Intentional transformations (checked, not errors): images inside tool results become a separate `user/image`; Codex FileChange becomes one Edit per file, commentary counts as thinking,
local images are written as `<attached_images>`; cross-session messages are shown by sender; background task notifications are deduplicated. Titles: all 35 sessions in the list with a custom-title / ai-title match.

## 1b. Sessions in progress (added 2026-10-31)

The cross-check above fetches by session id: "is this id's content read correctly" was all correct, but it never asked "does this pane map to this id" or "is it working right now".
Production bug: a running Codex (replied once, ran 3 commands, was compacting) showed nothing for that turn in the app, with a gray dot. The cause was not in reading:

- herdr did not report session ids for these Codex panes, so node guessed "the newest unclaimed rollout in the same directory", and got 4 of 5 Codex in the same directory wrong;
- the running thread was started six weeks earlier, so its rollout was in that day's directory six weeks back; node only scans the date directories of the last two weeks, so it was never a candidate;
- so the pane was matched to another, already finished session in the same directory: the content was that session's, and `codexTurnOver` saw it ended in task_complete and reported it done.

Read by its real id, this turn (user message, reply, 51 tools, compaction notice) was complete and correct. The fix is in `docs/integrations/codex.md` ("Identification").

The new cross-check `scripts/diag/live_panes_check.py` (runs on the hub machine; `pane_probe.py` fetches the ground truth on each machine) compares each herdr pane's ground truth (Codex by the thread name in the title,
Claude by the id herdr reports or `sessions/<pid>.json`) with the session id the hub's list has for that pane. If the Codex turn is still open (the last turn event is task_started)
and was written to in the last two minutes, the list must show it as working. Before the fix: of 70 panes (6 machines), **9 were mismatched**, all Codex on the same machine (9 of 26); Claude were all correct; 4 could not be verified
(no thread name in the title).

Recheck after deploying #110: Codex all correct, but 2 Claude panes on another machine were now mismatched. The panes of sessions Persona started are recorded in the hub's database and loaded back when node restarts;
those two panes had long since switched claude / resumed another session, but the old binding ranked ahead of the process's `sessions/<pid>.json`, so they showed sessions from days earlier
(before the restart, the in-memory bindings had already been corrected by hooks, which is why the pre-fix round did not catch it). Changed to process first; a binding only applies to a freshly started claude that has not written its pid file yet.
Recheck after deploying #111: for about a minute after the restart those two panes were still stale, then all correct (0 of 70 panes mismatched). The cause: right after node starts it does not yet know each claude's config directory,
so it cannot find the pid file, and the "not found" was cached for a minute, during which the old binding decided again. Changed to recheck a not-found after 5 seconds.

## 2. Sending: the path

```
App input field ──PendingMessage(UUID, .sending), stored in outbox──┐
  │ not connected / stuck: .waiting, resent in order once connected (same id)  │
  ▼                                                                 │
POST /api/v1/sessions/{mid}/{sid}/messages  {text, images, client_msg_id}   timeout 100 s
  ▼
hub sendMessage (hub/send.go)
  · client_msg_id dedup: in-flight in memory + Postgres sent_messages (one day)
  · sending is not cancelled when the app disconnects; 90 s deadline
  ▼  JSON-lines RPC over `ssh -C … personad node --stdio` (LAN round trip ~5 ms)
node session.send
  · one at a time per session (typingLocks)
  · session not running: Claude is started with --resume via its original launcher (~5 s); Codex / compatible Codex forks return an error
  · read the screen (ANSI, 60 lines, ~10 ms): dialog → 409 dialog_open; draft → 409 input_draft (our own just-typed text: wait for it to be taken)
  · images saved to ~/.persona/uploads, paths written into <attached_images>
  ▼  herdr unix socket (10 s deadline per call; CLI only if the socket is unreachable)
herdr agent.prompt (Claude, Codex): paste the whole text (bracketed paste) → ~300 ms → Enter; compatible Codex forks: send_text + 150 ms + enter
  ▼
Claude Code: starts a turn right away when idle; queued when working (transcript records queue-operation, writes queued_command when absorbed mid-turn)
Codex: writes UserMessage right away when idle; queued after the current turn when working
  ▼
node reads new lines every 250 ms → session.items pushed to hub → WebSocket items → App ChatStore.merge
  · a newly arrived user message that matches a local bubble by text removes the bubble
```

**Timing** (Claude, one Mac over ssh, idle, median of a few dozen runs): POST 0.31 s, of which hub ↔ node RPC ~5 ms, screen read ~10 ms,
**herdr `agent.prompt` a fixed ~300 ms** (it waits a moment after pasting before pressing Enter; timed directly against the herdr socket at 302 ms). Claude writes the message about 0.17 s after tapping send
(right after Enter, before POST returns); the WebSocket push reaches the phone in ~0.5 s (node's 250 ms polling + push). Long text of 16k / 120k characters: POST 0.46 / 0.63 s,
push 0.72 s, and the line count Claude reports is correct (not truncated, not split into several messages). Resuming an offline session: 5.5 s. Codex: delivered in the same 0.31 s; the user message is written to the rollout right away when idle,
and when queued behind a turn it waits for that turn to end (measured 10–70 s; the model is slow).

## 3. Sending: cases

| Case | Result (before fix) | After fix |
| --- | --- | --- |
| Idle, multi-line, Chinese and emoji, images, slash commands, resend with the same id | Exactly once, in order; in Claude images become `[Image #n]`, which the app's matching already handles | — |
| Sending while working (queued) | Exactly once; Claude queues it and handles it in the next turn | — |
| Sending while a permission dialog is up | 409 dialog_open, nothing typed | — |
| Sending while a question card is up (held by the hook) | Claude queues it and absorbs it in this turn after the answer; exactly once | — |
| **3 in a row (same session)** | Messages 2 and 3 got 409 input_draft (message 1 was still in the input field), and the app offered "Clear and send", which would clear the user's own message 1 | node serializes; messages 2 and 3 are delivered normally |
| Two devices sending at once | Claude: both arrived (by luck they did not collide); Codex / compatible Codex forks have no draft check and could merge into one | Serialized |
| **"Reconnect" this machine in the app** | The hub only closed the RPC; the ssh process stayed alive and the connection loop kept waiting for it to exit: the machine showed online, but **every call returned "rpc: connection closed"** until the hub restarted (measured 4.5 minutes; it only recovered after killing the ssh child process by hand) | Reconnects as soon as it closes (reproduced in tests with a fake node child process) |
| **node disconnects / hub restarts mid-delivery** | The message actually reached the computer, but the app got a plain 502 → tapping resend typed it a second time; node's dedup was in memory, so the new process after reconnecting did not know | hub dedups in the database; returns `delivered: unknown` and the app says "may already have been delivered"; a manual resend uses a new id |
| App goes to background / loses network while sending | When the request was cancelled the hub stopped waiting too, and nobody recorded the result; the app marked it waiting and resent automatically with the same id, which duplicated if node had reconnected | hub is not cancelled with the request; the result is stored |
| Pane gone (session exited) | Claude is resumed and it is delivered; but if Claude stops at a dialog on resume, the message gets typed into the dialog | Reads the screen after resuming too |
| Very long message | Truncated in the page (with `full_size`), so the app could not match the local bubble and **showed it twice** for a minute | Truncated ones are matched by their beginning |
| An earlier message with the same text ("continue") loaded with a history page | Removed the not-yet-sent "continue" bubble as if it had arrived | Only messages that newly arrived this time and are timestamped after the bubble (allowing 2 minutes of clock skew) count; one message clears only one bubble |
| A message queued behind a long task (Codex does not expose its queue) | The "Sent" bubble disappeared after 60 seconds and only showed up in the history tens of seconds later | Kept while the Agent is still working (up to 15 minutes) |
| Failed bubbles | Not persisted; gone after an app restart | Persisted, cleared after a day |
| Slash commands that open a panel, like `/cost` | The panel is only on the computer, nothing appears in the chat, and later sends get blocked by dialog_open | Not changed (see below) |

## Not changed

- Slash commands that open a TUI panel (`/cost`, `/status`, `/model`): the phone cannot see the result, and the panel blocks later messages. node could recognize the panel, read its content back to the phone and press Esc,
  but that needs per-command adaptation; to be done separately.
- On a new machine, claude's first launch stops at the theme picker: `POST /sessions` waits 60 seconds and returns 502, leaving a tab behind in herdr.
- Draft checks for Codex / compatible Codex forks (if the input field has text, the message gets appended to it).
