# Lessons: transcript parsing, synchronization, and pagination

Pitfalls, unusual behavior, and the trade-offs made at the time (originally in "Lessons" in `CLAUDE.md`; see the index in `docs/lessons/README.md`). Add new lessons to the relevant section, or start a section if none fits.

- A session's "last edited" time must be the timestamp of its last real conversation entry (user / assistant / tool): Claude Code appends recap, ai-title,
  mode, and other lines to idle sessions, refreshing the file's mtime. Leave the timestamp blank for sessions whose transcript cannot be found; do not fall back to now (it would always show "Just now").
- Queued messages: transcript queue-operation entries (enqueue / dequeue / remove / popAll) reconstruct the queue precisely. In the TUI, ↑ brings **all** queued messages back into the input field.
  ctrl+u clears only the current line (for multiple lines, use ctrl+u + backspace one line at a time). Long text sent through herdr bracketed paste is wrapped in `<pasted_content id=…>`; strip it before display.
- Messages entered while Claude is working (from a computer or phone) are "absorbed mid-turn" and stored in the transcript as `attachment/queued_command` (commandMode=prompt,
  origin.kind=human), not as user messages. The parser must handle them separately, or they will not appear on the phone.
- Newer Claude Code versions write narration between tool calls into nonempty `thinking` blocks, not `text`. The UI must not hide all thinking indiscriminately.
- A new session (a headless assistant or a newly launched herdr Agent) has no transcript until claude writes its first line. The phone requests a backlog as soon as it subscribes to the hub. `session.items`
  must return an empty list rather than not found for these "starting" sessions (`startingSession`). Once watch sees the file appear, it must first replay the lines already written, or the chat page briefly flashes an error.
- Only a response paginated from the newest end knows "whether there are earlier messages" (`has_more`). Subscription catch-up with since_seq used to send `has_more: false`;
  if it arrived before the first HTTP page, the App assumed it had reached the start of history and stopped upward scrolling at the earliest loaded message (2026-10-26). The hub now omits this field in catch-up,
  and the App treats its absence as unknown. A batch that does not cover the earliest local message must not set it to false either. Reproduce with `-showScrollToOldest YES`, scrolling all the way up while logging.
- When the chat page is "not following," the list freezes at the messages present at that moment (`frozen`, so content stays still while scrolling up). Only a **change** in the bottom sentinel's visibility enables following. If the landing point (the last read position,
  or the start of an unread reply) is on the last screen, landing already puts the list at the bottom. The sentinel stays visible and never changes; later replies are all held back, with no "Back to latest" either (2026-10-31, TestFlight 50/51:
  "stuck on the message I sent"). Rule: if the list is at the newest end and no finger is down, it must follow (`landed` / `contentArrived`). Run `ChatFollowTests` when changing follow logic.
- In sessions with many screenshots, one page of earlier messages is 1–5 MB (images embedded as base64). Decode pagination on a background thread, and prefetch the previous page as soon as the earliest dozen or so lines appear.
- Do not implement background sync by "subscribing to every session": subscriptions make the node refresh transcripts every 250ms and read terminals every 400ms (herdr process calls). Use an IM-style push/pull combination instead:
  the hub pushes only each session's `rev` (transcript byte count), and the App pulls increments with `after_seq` (`docs/ios.md`, background sync). The full session list is about 130 KB;
  do not resend it for rev changes—use a separate `revs` event. The Lark gateway has a real-time event WebSocket (`/agent/v1/events`, `message.upserted` /
  `conversation.upserted`); Lark pushes come from there.
- Validate changes to transcript parsing or the sync protocol against a real corpus (`docs/archive/lanes/msg-render-verification.md`). `corpus_test.go` compares the old and new parsers file by file;
  `continuity_corpus_test.go` simulates lost pushes, gap filling, and rewrites using the App's rules. Keep the corpus only in `~/jarvis-corpus`. Active session files grow while being read, so take a snapshot before comparing.
  To view real sessions in the simulator App, use a local test hub (fake HOME, no herdr connection, separate database), not the production hub.
- Cold-opening a large transcript parses only a tail window (`transcript/window.go`). A line's parsed result depends on preceding lines through just three things: background task notifications already displayed
  (duplicates are dropped), the previous "! cmd" input, and Codex recording the same item twice. If a parser change introduces "cross-line state," the window's starting point must also be able to reconstruct it
  (`seeded` / `seeNotifications`), or windowed and full reads will disagree. `TestFixtureWindowedEqualsFull` and the corpus's `TestCorpusWindowed` catch this.
- User images take two paths: ordinary messages contain user image blocks; messages sent while Claude is working are "absorbed mid-turn" as `attachment/queued_command`, with images in the prompt's block array
  (before 2026-10-29, the parser took only text blocks, so bubbles showed no images). In App turns awakened by background tasks (`pickAnswerAcrossWakes`), keep the user's images together with their message in the original position, not in the work trace.
- A long chat's slow first screen is not caused by the hub / node (cold read: 0.12 s), but by the page being too large: one long Codex session's last two turns contained 1412 entries and 1.08 MB, with just 6 reply entries; the rest were tools and thinking already collapsed in the UI.
  The App requests `fold_work`. Pages read backward from the end replace each work segment with one `kind: fold` entry (counts and ranges only). Expanding a work row fetches it with `from_seq` / `through_seq`
  (`docs/protocol.md`, "Folded work"). `after_seq` catch-up and live pushes are not folded. Neither are tool_use entries still running or tool_result entries whose other half is outside the page, or they cannot be paired.
- Claude Code writes `&gt;`, `&lt;`, and `&amp;` itself into the summary / event / result of background task notifications (`<task-notification>`); decode them during parsing. For background commands without a description,
  the summary is the whole command (a heredoc can span dozens of lines). Do not use the summary as notice text: the node parses it into `Item.task`, and the App renders a single tool-style row (`TaskNoteRow`).
  Monitor events carry only task-id; map them back to the call using the tool_result `Monitor started (task <id>` (`TaskIndex`).
- The App merges its on-disk chat cache by item id. If the hub changes the contents of a type of item (for example, adding fields to notices), new pages do not overwrite old cached items. When the item structure changes,
  bump the `ChatCache` directory version (`chats-v3`).
- Backward pagination has a parsing budget, but must not return an empty page while earlier content remains. An empty page + `has_more: true` leaves the App with no seq to fetch next, making earlier history permanently inaccessible (large Codex rollouts can have tens of MB of lines that produce no entries between two turns).
- Parsing `<jarvis-context>` must not assume all attributes are present or in a fixed order (2026-11-03, #876). iOS once recognized it only when all three of `kind`, `title`, and `url` were present. Lark /agent contexts had no `url`, so the tag went unrecognized, leaving the whole prompt and large blank areas in the bubble.
  Truncating user messages for pagination must also avoid cutting through tags: a cut in the middle leaves an unclosed `<jarvis-context`, which every client displays as body text (`lightContexts` in `transcript/contexts.go` trims only the body of each block).

## A tail window may contain no words at all (2026-11-04, #1163)

When listing offline sessions, the node reads only the last 384 KB of a transcript (`quickTail`) to get its title and last words. Claude Code now writes an entire `api-request-blob` on one transcript line, 300–500 KB per line;
a single line can fill the tail window. If the last turn stops between tool calls (thinking → tool_use → tool_result), the tail contains no text at all. `scanOffline` used to treat every session with
"neither a title nor last words" as a hook probe and skip it—even a kept main chat. The main chat disappeared from every list, the App and web could not find it, and the input field became completely unresponsive.
Now it skips only small files that have been read in full (no larger than the tail window) and genuinely contain no output. The main chat is listed regardless of its tail contents. Before "filtering" based on a tail, remember that one large data line can fill it.

The same window also lost the last words of online sessions (#1583): after the Agent said something and then made dozens of tool calls, tool results, hook receipts (`async_hook_response`), and request lines quickly exceeded 384 KB.
`last_text` became empty, leaving task cards and main-chat snapshots showing "No activity yet" for 7 minutes. `readQuickMetaSince` now reuses the previously read text when the window contains none;
if there is no previous value (the node has just started), it searches backward in segments (`lastWordsBefore`, up to 8 MB). For any "last X" value found only in the tail, consider that it may already be outside the window.
- **A Claude Code transcript is a tree** (`uuid` / `parentUuid`): rewinding (`/rewind`) or editing and resending with ↑ writes the next message as a sibling of an earlier one. Rewound turns remain in the file. See
  `transcript/branch.go` (two messages from the user under the same parent, with an assistant reply to the earlier one → the earlier message's entire subtree produces no items) and `docs/protocol.md` ("Rewind to a message");
  **pressing Esc to cancel a message before any reply, then sending again, also writes the same pair of siblings** (#3036; observed: two identical messages under the same "[Request interrupted by user]" line, with no reply to the first).
  This is not a rewind. Older versions treated any "two siblings" as a rewind, added `.r1` to `history`, made all clients discard and refetch local content, and removed the interrupted turn from the chat too.
  To detect a rewind, check whether the earlier message received a reply. Persona's own rewind does not rely on this inference (`File.Rewound` is marked by the node).
  Do not walk backward from the end along parentUuid (parallel tool results are normal branches, and `compact_boundary` has a null parent). Abandoning a branch changes only the `history` token (`.r<n>`),
  not the item structure, so there is no need to bump the App's `ChatCache` directory version. Clients already discard and refetch local content when the token changes. Validate against a real corpus:
  build one old and one new binary with `go test -c ./internal/transcript`, compare item counts file by file with `-test.run 'TestCorpus$' -corpus <directory> -out <file>`, then run `TestCorpusWindowed`.
