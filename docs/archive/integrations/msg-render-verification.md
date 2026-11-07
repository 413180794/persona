# Real-data verification of the message pipeline lane (stages 2–4)

2026-10-29. Corpus: every Codex rollout on one dev machine, a random sample of Claude transcripts (including subagents), the 15 largest transcripts (60–332 MB),
plus the local transcript snapshot; **435 files, 5.8 GB, 1.27 million lines** in total. The corpus lives only in `~/persona-corpus` on that machine and is not in the repo; only the numbers are here.

All tools are in the repo and are skipped when run without arguments:

| What | How to run |
| --- | --- |
| Parsing: items per file (by type), lines that produced no item (by line type), bad lines, Meta | `server/internal/transcript/corpus_test.go`: `go test -c -o corpus.test ./internal/transcript`, `./corpus.test -test.run 'TestCorpus$' -corpus <dir,…> -out x.jsonl`. For an old version, add one `corpus_shim_test.go` (API differences); the same tool then runs the old and new parsers file by file, and a script compares them by item id |
| Continuity simulation | `continuity_corpus_test.go`: `-test.run CorpusContinuity -sim 3` |
| Cold-open time, resident memory | `TestCorpus -perf -minsize 20000000` |
| App rebuild time per event | Export with `TestCorpusDumpItems -dumpitems <dir>`, then `TEST_RUNNER_PERSONA_ITEMS_DIR=<dir> xcodebuild test` (`RebuildTests.realSessionsRebuildIncrementally`) |

## Numbers

| Item | Result |
| --- | --- |
| Files / lines / items | 435 / 1,271,802 / 477,153 |
| Parse errors (panic) | 0 for both old and new |
| Bad lines (incomplete JSON) | 20 lines in the middle of 8 files (1–5 each, already broken in the source); both old and new skip them |
| **master → this lane**: item differences | **0** (477,153 vs 477,153; texts with different truncation lengths compared by the first 24 KB and the real length: identical) |
| Before the lane (79c5d68) → this lane: item differences | 6 kinds, all intentional stage 1 changes, see below |
| Assistant lines that produced no item | All are empty thinking blocks (signature only, 3,513 / local snapshot); the other lines with no item are metadata such as api-request, mode, ai-title |
| **Continuity simulation**: runs | 1,305 (435 files × 3 seeds): **0 assertion failures** |
| Events in the simulation | 756K chunked writes (including half lines), 13,912 dropped pushes, 12,991 gap fills, 203 full-page restarts, 445 truncate-and-rewrite / file switches, 435 "nothing dropped" control runs |
| The simulation itself catches bugs | Reverting two App rules to the old ones: "don't accept a batch starting at line 0 when covered=0" fails 38 times, "don't restart without a baseline" fails 8 times |
| Cold open (52 files ≥20 MB, 4.6 GB) P50 / P99 / max | master 222 / 1819 / 1819 ms → this lane 218 / 1813 / 1813 ms (unchanged; windowed cold open is the next PR) |
| Resident memory after opening (same 52 files, total) | master 1,271 MB → this lane **569 MB** (−55%); the 15 largest 845 → 410 MB |
| App rebuild per event (10 largest real sessions, last 200 events each arriving one by one) | Full 1.6–899 ms (899 ms for the 78,758-item session) → incremental 0.2–14.7 ms; **2,000 incremental vs full comparisons item by item, 0 mismatches** |
| End to end: first page (turns=2) of the 6 largest sessions (202–332 MB) | Cold 0.66–1.82 s, warm 1–20 ms |
| End to end: fetching the full text of truncated tool output | 31 / 31 byte-identical to the transcript (3 / 3 for subagents too; fetching with the main session id returns 502, which is the bug the review pointed out) |
| End to end: chat in the App vs cold read | 332 MB session: 81 items, equal to the tail of the cold read; session being written (hub stopped for 20 s midway): one reconnect filled in 625 items, ending with 1,490 items equal to the tail of the cold read, 0 duplicates |
| End to end: content stays put while scrolling up | Real session read while being written (a message typed on the computer arrived meanwhile): 1,727 pt before the fix, **0 pt** after (103 items arrived) |

## Differences before the lane → this lane, by kind

| Difference | Count | Why the new one is right |
| --- | --- | --- |
| Fewer background task notifications | 2,041 | The same `<task-notification>` is recorded both as a user line and as a queued_command attachment, and the old parser showed it twice. **For every one removed, an earlier one with identical text is still there** (2,041 / 2,041); multiple notifications for the same task with different content are not merged |
| Items in the same line reordered | 1,316 lines (8,596 items) | A Codex FileChange edits several files at once; the old version iterated a map (random order each time), the new one sorts by path; the sets are identical |
| New Codex web search / open page | 149 + 167 calls, 316 results | Codex's `Extension` (web.search) lines used to be dropped entirely |
| New /loop fire prompts | 173 | `system/scheduled_task_fire` used to be dropped, so you couldn't tell a scheduled task had started a new round |
| New local commands | 5 user messages + 5 outputs | `system/local_command` such as `/model` used to be dropped (this adds a turn, so Meta.Turns changed in 3 files) |
| Notice text changed | 6 + 1 | ANSI color codes removed from local command output (47 → 38 bytes); api_error shows the real error message instead of a generic "request failed" |

## Problems found and fixed during verification

1. **A new session that lost its first batch of pushes had a gap forever** (caught by the simulation: removing this rule fails 8 of 1,305 runs): when the App opens, the transcript does not exist yet, so the backlog carries no baseline. If the first batch of pushes (starting at line 0) was lost,
   later batches never checked continuity because there was "no baseline". Now a batch starting at line 0 is itself the baseline; if a backlog has been received but there is still no baseline, a following batch triggers a restart from the latest page.
2. **A message typed on the computer pulled someone reading history back to the latest** (caught end to end, 1,727 pt): "your own message returns you to the latest" used to apply to every new user message; now it only applies to messages sent from this phone.
3. Review: "view full text" for subagents used the main session id; only tool output had "tap to view full text"; `File.Full` now reports an error when a re-read fails instead of returning a truncated copy;
   the store unfreezes on a full-page replace; a full-page replace keeps newer pushes that arrived while it was in flight.

## Not covered

- Watch: this machine has no watchOS simulator runtime; only the build and the `WatchText` unit tests were verified.
- Cold-open time did not change (this batch was not aimed at it): on one dev machine a 317 MB session takes 22 s to cold-open and the hub times out with 502. Windowed cold open (option C2) is done separately.

## C2: cold open of large transcripts parses only the tail window (same corpus)

| Item | Result |
| --- | --- |
| Window read vs full read (181 files ≥2 MB × 1 MB / 8 MB windows, 240 runs, 190 actually read by window) | **0 mismatches**: first page, items returned by the cold read, every page when scrolling back to the first line, after_seq from a random line, full text of unparsed items, and Meta are all byte-identical |
| Shorter first page (only for a very long turn: parsing back more than 24 MB still doesn't yield two turns, `has_more: true`, scrolling further keeps fetching) | 37 of 240 runs; the short page is always the tail of the full read's first page, and scrolling back joins up to the same result as a full read |
| Continuity simulation (with "node restart" added: half the runs switch to a new File cold read midway, 256 KB window) | 1,305 runs, 393 restarts (190 read by window), **0 assertion failures** |
| Cold open + first page (52 files ≥20 MB, local) | P50 219 → 64 ms, P99 1,790 → 370 ms; 332 MB: 1,790 → 115 ms |
| End to end: local test hub cold-opening the 60 largest sessions (turns=2) | master P50 0.04 / P90 0.69 / P99 1.84 s → C2 0.04 / 0.08 / **0.17 s**, all 200 |
| End to end: App cold-opening the 332 MB session | 81 items, equal to the tail of the full read; view full text 29/29 matches the original; reading while writing + hub stopped for 20 s: one fill of 624 items, ending identical to the full read |

Cold-open time is now mostly line counting (only looking for newlines and two markers, no JSON parsing) plus parsing the last 8 MB; measurements on the dev machine get added with `bench-items.sh` after deployment.
