# Agent states and the space model

## Unified states

The App, the watch and push notifications all use this one set of states (`proto.Session.state`, computed by the manager connected to the computers and passed to the App unchanged by the hub, `server/internal/assistant/fleet.go`). The source is the host's state (currently herdr), plus two things Persona knows itself:
whether there's a pending interaction, and whether the user has looked at it in the App.

| State | Meaning | From herdr | Persona adds | Color |
| --- | --- | --- | --- | --- |
| `needs_you` Needs you | Stuck somewhere a person must step in | `blocked` (a question / confirm box in the TUI) | A pending question, plan, permission or terminal interaction (herdr still shows working while a hook holds it) | Red, breathing |
| `working` Working | Running | `working` | Claude: herdr reports `done` / `idle` / `unknown`, but the session has hooks (Persona's, or the global report-only ones) and the hook turn count says the turn isn't over (see "Hook turn signals" below); for sessions without hooks, the terminal title shows Claude Code's working spinner glyph (◐◓◑◒; older versions used braille dots), or the transcript shows this turn has started and not ended (no `turn_duration`, not interrupted) and was written within 45 seconds (#2069; if the transcript shows the turn ended, herdr's state is used); or the turn has stopped but its background commands / sub-Agents / Monitor / workflow are still running (#2228, #2935: when they finish they hand it the result and it carries on; assistant chats excluded) | Amber, breathing |
| `unread` Unread | A turn finished and the result hasn't been seen | `done` (never focused in herdr) | And not viewed in the App since it finished | Green |
| `idle` Seen | Finished and seen, waiting for input | `idle` (seen in herdr) | Or herdr still says `done` but it was viewed in the App | Gray |
| `offline` Not running | Not in the host, only a transcript; sending a message does `--resume` | — | — | Light gray |
| `interrupted` Interrupted by restart | Was running before a restart, and the restart took herdr's panes with it (#1917) | — | The record has a pane, the start time is earlier than this node's start, and there's no live agent now | Light gray, with "Continue" |
| `unknown` Unknown | The host recognized the agent but can't tell its state | `unknown` | — | Gray |

Sort priority: Needs you → Working → Unread → Seen → Unknown → Not running.

### Non-Claude herdr agents (opencode, pi, unrecognized agents)

When the agent herdr reports isn't Claude / Codex / TRAEX, the state is **always what herdr reports** (`blocked` / `working` / `done` / `idle` / `unknown`); there are no hooks, transcripts or on-screen boxes to add to it.
Deciding "Needs you" / "done" and pushing are the hub's existing logic (based on session state changes, regardless of which agent). Per-agent additions:

- **opencode** (`docs/opencode.md`): herdr's opencode plugin reports `working` (turn start, tool execution, after a permission answer), `blocked` (`permission.asked`, `question.asked`, errors) and `idle` (turn end).
  With Persona's plugin installed, the plugin also reports `blocked` while it's waiting on a permission card (or a question that can only be answered in the terminal); node combines both: herdr's word stands, and the plugin saying `blocked` counts as "Needs you".
- **pi** (`docs/pi.md`): same; the extension having a card waiting on the phone counts as "Needs you".
- **Unrecognized agents** (`gemini`, `amp`…): used to be dropped entirely; now listed as a terminal-only row (`docs/opencode.md` ("Terminal rows for other agents")). The state is herdr's, so when it's stuck in a confirm box the phone shows "Needs you",
  but there's no card to answer (the hub doesn't parse screens of TUIs it doesn't know); answer it on the terminal page.

### Hook turn signals

herdr's state is often wrong (`done` while still working, #2069), and the title glyph and transcript modification time are only inference. Claude sessions Persona starts carry Persona's hooks; sessions the user starts themselves, once `claude_global_hooks` is on, get global report-only hooks (`claude-integration.md` ("Global report-only hooks"))
that report the same `UserPromptSubmit` / `Stop`. Both feed the same counter: they say directly when a turn starts and ends, and node decides from that (`judgeHookTurn` in `server/internal/node/hookturn.go`, called by `claudeStatus`).
Borrowing moshi-hook's approach, node keeps per session in memory:

| Field | Source | Purpose |
| --- | --- | --- |
| `prompts` | +1 on each `UserPromptSubmit` | How many turns started |
| `completed` | Set to the current `prompts` on `Stop` | How many turns finished |
| Process pid + start time | The hook process walks up to claude (the ancestor whose `~/.claude/sessions/<pid>.json` `sessionId` matches); on Linux, field 22 of `/proc/<pid>/stat`, stored as `linux:<starttime>`; macOS can't get the start time, so only the pid is stored | Tell a live claude from a reused pid |
| Reply signature | First 8 bytes of SHA-256 of `Stop`'s `last_assistant_message` with whitespace removed | Hub dedups "done" pushes |

The two sources differ in only two ways; everything else is the same. Sessions Persona started (including the assistant) count only the plugin hook's copy; the global copy is dropped **before** counting, otherwise one submit counts twice.
Global events carry `report_only`, and the hub doesn't take them as meaning the session has Persona's hooks (otherwise screen cards would wait in vain for a hook card that never comes).

Decisions (when herdr says `done` / `idle` / `unknown`; `blocked` and `working` follow herdr):

- **A dialog is open in the terminal** (#2553, `node/dialogstatus.go`): for a Claude still `done` / `idle` / `unknown` after the checks above (excluding botmux, those in Claude's agents list, and ones where Persona itself is operating a picker)
  in a herdr pane, node reads its screen in the background every 5 seconds (the list doesn't wait for the read). If the bottom is a dialog (`screen.Choosing`) it's recorded as `blocked`, `attention_reason` is the dialog's first line, and the "last said" in the App list and the assistant's `agents_list` shows that line.
  Why: Claude Code 2.1.309's auto mode billing notice blocked 3 Agents for several minutes; herdr reported done and Persona showed Unread.

- **Has hooks and `prompts > completed`**: Working, overriding herdr's `done`.
- **Has hooks, `prompts == completed`, and a `Stop` was seen**: not working; a leftover spinner glyph in the terminal title doesn't count. Exception: if the transcript has a turn written after the `Stop` (with a 5-second grace for the closing lines) that hasn't ended,
  it's a new turn the hooks didn't report (a background task or `/loop` woke Claude, or a Stop hook made it keep going), and that turn is judged by the no-hook rules.
- **Nothing counted** (node just restarted, only `SessionStart` received): the hooks have nothing to say; judge as a session without hooks. The counter is in memory and becomes valid again from the first `UserPromptSubmit` / `Stop` after a restart. That errs on the safe side,
  so it isn't stored in Postgres and needs no migration.
- **The agent process is gone or its start time doesn't match** (pid reused by another program): drop the session's hook state, so "an unfinished turn" left by a dead process no longer counts as Working; use the no-hook rules instead.
  It isn't judged Not running outright, because the same session may already have been continued by hand with `claude --resume` in the same pane (without hooks then); if the pane really has no agent, herdr doesn't list it anyway.

**Interrupting (Esc) doesn't fire `Stop`**, so `prompts > completed` never balances. The turn ends in this order:

1. The end of the transcript has an end marker written **after** this prompt (`[Request interrupted by user]` or `turn_duration`): ended.
2. The title has no spinner glyph, the transcript has been quiet for 8 seconds since the prompt or the last write, and the title shows Claude's waiting mark ✳: ended (✳ is Claude's own "waiting for input" mark, strong enough evidence).
3. Same but the title isn't recognizable (old version, user-changed title): ends only after 90 seconds of quiet (`2 × claudeFresh`), because long tool calls can go a long time without writing the transcript.
4. The title has a spinner glyph: counts as Working indefinitely, so long silent tool calls aren't misjudged.

A `UserPromptSubmit` without a `Stop` (whether local commands do this hasn't been tested) takes the same path.

**Dedup of done pushes**: the hub sends the push on working → done, and state flapping (working → done → working → done) could push twice for one turn. node puts the `Stop` reply signature into `hook.event` (`stop_sig`),
and before sending a "done" push the hub compares: same session, same signature, no new `UserPromptSubmit` in between, then no second push (`Machine.stopDuplicate`).
Global report-only `Stop`s carry the signature too and are deduped the same way. Without a signature (no hooks, `Stop` without a reply) nothing is blocked; a new prompt clears the record, so two turns that happen to reply the same ("OK") both push. After a hub restart the record is lost; worst case one extra push.

**The same pid-reuse defense is used for "open elsewhere"** (`openelsewhere.go`): the `sessions/<pid>.json` Claude Code writes carries `procStart` (on Linux it's field 22 of `/proc/<pid>/stat`; verified to match).
When a leftover old file's pid is taken by another program, it used to be treated as "this session is open elsewhere" and resuming from the phone was refused; now a start time mismatch means that claude has exited.

Not verified / known limits: whether Claude Code's background-task wakeups and `/loop` timers fire `UserPromptSubmit` hasn't been tested (both cases are judged correctly, see the exception above);
on macOS the pid-reuse defense is only "is the process still alive"; on Windows the ancestor process can't be found, so no pid is recorded; the other signals work as usual.
The `sessions/<pid>.json` Claude Code writes also has `status` (busy / idle) and `statusUpdatedAt`, which could become another signal later (not used this time).

**Work state** (2026-11-04, #1686, [agent-work.md](agent-work.md)): everything above is the host's view; "Unread" only says the turn stopped. After a turn stops, the hub triages it
and adds a work state `work` to the session: working / blocked on a named precondition (waiting for your decision, another Agent, an external event, a transient error) / done, with a line saying "what it's waiting for" or the conclusion.
State computation takes it into account: **done**, or **blocked on something the hub waits on for it** (the hub retries automatically, wakes it at the set time), is no longer `unread` but `idle`;
for a decision you need to make, the hub opens a question card, so it's `needs_you`. Anything not triaged (just stopped, model failure, can't tell) stays `unread`.
There is only one set of colors: `apple/Shared/StatusPalette.swift`, shared by the App, the watch and Live Activities (`docs/ios.md` ("Design language")).
The Agents list also has "Waiting": the state is Seen, but a `/loop` hasn't fired yet (`Session.isWaiting`), so a new message will come. Sessions whose background commands / sub-Agents are still running are counted as Working directly by the hub (#2228, `backgroundState`) and no longer go into Waiting; the assistant chat's background tasks and Monitor are always running and don't count.
A long-running process started in the background (such as a dev server) keeps the Agent Working until it exits (node stops listing background tasks older than 24 hours). Turn-end decisions such as the "done" push and hub self-update look at the host state and aren't affected by this.

### Background workflows and Monitor (#2935)

Claude Code's Workflow tool runs a multi-agent script in the background; the turn ends right away and is woken again by the notification that it finished (`<task-notification>`). Monitor works the same way, waking it when an event arrives.
They count as background work just like background commands: the Agent is Working, the "done" push is held back, and to-do receipts, completion judgment and auto-close (#1696) all wait for them to end. Once a notification wakes the main session, the end of that turn is judged as usual.

- **Where it's read from**: the Workflow tool result in the transcript, "Workflow launched in background. Task ID: … Run ID: wf_…" (`turn_duration`'s `pendingWorkflowCount` also says some are running),
  and Monitor's "Monitor started (task …, expires in 30m …)". The end is the notification for the same task id (with `<status>`) or `TaskStop`. node's background scan reads the whole transcript, so nothing is missed by only reading the tail.
- **Progress is on disk**: `<session>/subagents/workflows/<run>/journal.jsonl` (one `started` line per agent, with label and phase; `result` means it came back), and in the same directory `agent-<id>.jsonl` / `.meta.json` (each agent's conversation, read as a sub-Agent).
  When done it writes `<session>/workflows/<run>.json` (status, error, duration); the `meta` of the script `<session>/workflows/scripts/<name>-<run>.js` has the name and phases.
- **Session field** `workflows` (`docs/protocol.md`): name, status, current phase, finished / started counts, duration, the sub-agent list and each one's last line. While running it's also put into `background` (kind `workflow`), so old clients and old hubs can still tell "there's background work".
- **Fallback**: with no notification and no status file, 30 minutes without any write counts as `stalled` (its claude is gone) and no longer as running; a Monitor past `expires_at` doesn't count; finished ones stop being listed after a day.
- The assistant's live snapshot and `agents_list` spell out running workflows ("this turn stopped to wait for results, it isn't done"), so it won't nudge or close these Agents.
List order (the same for all three groupings): Needs you → Unread → Working → Waiting → Muted → Seen; within a state, by last update time (`docs/ios.md`).

### "Seen"

- Seen in herdr = the tab was focused on the computer (reads via the CLI don't count). Persona **does not** focus the tab to mark it seen (that would steal focus on the computer), so after you look on the phone,
  the green dot in herdr's sidebar on the computer is still there.
- Seen in the App: opening the chat page, watching it finish on the chat page, swiping left in the list to **Mark as Seen**, **Mark All as Read**, opening the session on the watch. The App sends WS `{"type":"seen"}`
  (when the connection is stuck, or the hub hasn't recorded it 5 seconds after the WS send, it sends `POST /api/v1/sessions/{mid}/{sid}/seen` instead), and the hub stores it in Postgres `session_views`.
  WS messages have no acknowledgment and are silently dropped on a half-open connection (#1588), so the App keeps reads the hub hasn't recorded yet in `SeenOutbox`: it counts as recorded only when a list from the hub has this session's
  `seen_at` later than the mark. Meanwhile it still shows as Seen and is resent on reconnect; it stops waiting if the session does another turn, starts working or needs you, disappears from the list,
  or still isn't recorded after 10 minutes.
- Rule: herdr `done` and `seen_at` earlier than the session's last update time (the transcript's modification time, i.e. when the turn ended) → `unread`, otherwise `idle`.
  So if it does another turn after you looked, it becomes Unread again.

### Ordering

- The list is grouped by state, newest first within a group. Normally that's `updated_at`, but a working agent's `updated_at` changes with every step and would jump around within the group,
  so "Working" uses `state_since`: the hub records when each agent entered its current state (in memory; after a hub restart it counts from the restart).

## Spaces / slots (multi-tab collaboration)

"A group of related agents" in a host is abstracted as a **space**, and each position in the group is a **slot**:

| Field | herdr mapping | Other tools later |
| --- | --- | --- |
| `space_kind` | `herdr` | Tool name |
| `space_id` / `space_name` / `space_index` | workspace id / label / number | Project, room, session group |
| `slot_id` / `slot_name` / `slot_index` | tab id / label / number | Role, window, channel |
| `pane_index` (optional, #2320) | The pane's positional index within the tab (from 0, top-left is 0) | Which position within the slot |
| `parent_space_id` (optional, #2073) | Not set | Which space this space sits under (same machine, same `space_kind`): an Orca child workspace sets the parent workspace's `worktreeId` (`worktree.ps`'s `parentWorktreeId`) |

DSH (`docs/dsh.md`): `space_kind` is `dsh`, the space is the session's directory (`space_id` = directory, `space_name` = directory name), no slots;
state comes from the plugin: running → working, a permission / question waiting → blocked, otherwise done (the hub splits it into Unread / Seen by whether it's been seen, as usual).

Paseo (`docs/paseo.md` ("Which sessions are shown")): `space_kind` is `paseo`, the space is a Paseo project (`space_id` = project record id, `space_name` = project name);
a repository's main checkout and its worktrees share a space; the slot is a Paseo workspace (`slot_id` = workspace id, `slot_name` = workspace title).

opencode (`docs/opencode.md`): those in herdr panes are herdr sessions (`space_kind` is `herdr`); those outside herdr (web, other terminals) are runtime sessions,
`space_kind` is `opencode`, the space is the session's directory, no slots, and state comes from the plugin (running → working, waiting on a permission → blocked, otherwise done / idle depending on whether there's an assistant message).

node fills these fields from herdr's `session.snapshot` (workspaces, tabs and agents in one call): once whenever herdr pushes a change via `events.subscribe`
(the list is rebuilt at most once a second; entering / leaving blocked doesn't wait, see `docs/server.md` ("Fast lane for cards")),
and every 10 seconds as a fallback (every second when the event stream is down or there's a TRAEX pane, see `docs/server.md` ("Connection to herdr")). When a tab name is the default number, the App shows `#2`.

### Several panes in one tab

A common pattern: an Agent runs `herdr pane split` in its own tab, then starts Claude / Codex in the new pane to split up the same job. Agents in the same tab (`slot_id`) form **a group**; the Agent doesn't need to write anything extra:

- herdr doesn't record who started a pane, only positions: in the snapshot's `layouts`, each tab has each pane's `rect` and the split tree `splits`. node orders by the split tree
  (`server/internal/herdr/layout.go`) and fills `pane_index` (from 0) for each Agent session:
  each item in `splits` is a tree node `{id, direction, ratio, rect}` without its children; `direction` is `right` (split left/right) or `down` (split top/bottom), and `rect` is the area it splits.
  node starts from the whole tab, finds the node whose `rect` is exactly that area, and splits it in two along its direction (the left / top half first; if several lines in the same direction could split it, it takes the one where each half is exactly one pane or one node), then orders each half the same way.
  So a 2×2 made by splitting top/bottom first and then each half left/right (A, B on top, C, D below) orders as A, B, C, D; looking only at rects, both lines could split it and the order would be ambiguous.
  With no `splits` (old herdr) or no node for an area, it falls back to rects only: find a straight line that crosses no pane, trying left/right first, then top/bottom.
  When zoomed, herdr still gives each pane its original rect; when rects overlap and can't be split, herdr's own listing order is used. If the host gives no layout (herdr-core, Orca, etc.) or the pane isn't in the layout, there's no `pane_index`.
  node also pushes `sessions.changed` when only `pane_index` changes (`herdr pane swap`, closing the top-left shell): it's part of the list's change detection (present or not, and its value).
- Within a group, order by `pane_index`. Number 0 (top-left; every split is cut from it) is the **starter**, the rest are branches it started; without `pane_index`, fall back to the old order (session id).
  If the top-left is a shell rather than an Agent, the group has no number 0 and the Agents are peers (the web doesn't mark a starter).
- Parent/child relationships across tabs (an Agent opening a subtask in another tab / workspace) are not inferred; if needed later, agree on an explicit marker (like to-dos' `persona_from`).

For the web, see `docs/web.md`; Android, `docs/android.md` ("Several Agents in one tab") (#2515). iOS / Mac (#2516; the pure logic is in `apple/Persona/Views/Chats/TabPanes.swift`, same rules as the web's `herdr-tree.ts`):

- In the Agents list (by repository / by project; cards, capsules, compact list), a tab with several Agents first gets a tab row: the status dot of the one most in need of attention, the tab name and its title, "N Agents"; tap it to collapse / expand.
  Below it, ordered by pane position: the starter on top, labeled **Started It**, the rest indented one level beneath it. Each row names its own Agent (when both are Claude they're numbered "Claude 2"; the starter is told apart by its label) and no longer repeats the tab name.
  Expanded by default; the collapsed state is remembered per `machine/space name/tab number` (`tab/…` in `dev.folded`, still recognized after renames). The whole group counts as one item: if any member is busy, it doesn't go into "N more idle".
  Expanding an idle space / repository adds no tab rows; members of the same tab are kept together and the starter is still labeled. The number on a space counts Agents ("N Agents"). Grouping by state / by machine / by to-do doesn't group by tab.
- The peer capsules and ⋯ popover at the top of the chat page, the Mac peer menu, and the space page: members of the same tab are kept together and ordered by position, named "#1 · Codex"; the starter is marked **Starter** (the popover and menu write **Started It**, branches are indented; the Mac menu has one section per tab).

How it looks in the App:

- **The dev list is grouped by space**: each active space (with Needs you / Working / Unread) gets a card whose header shows the space name, machine and per-state counts,
  listing each agent in tab order (tab name, title, state, latest line). Spaces that are all Seen are folded into **Idle Spaces**, one row each, expandable;
  sessions that aren't running are under "Recent (not running)". On Android (since 2026-11-02) it's a computer → space → session tree, with no **Idle Spaces** group or heading:
  idle spaces stay under their computer, after the active spaces, collapsed by default and styled more subtly, showing how many sessions they contain when collapsed (`docs/android-design.md` section 2.4).
- **The peer menu in the chat page's top bar**: when a space has several agents, the leftmost button on the top bar's second line lists them (tab name + state; the button has a red dot when another agent needs you); tap one to switch straight to it without going back to the list.
  The subtitle is "machine · space › tab · state".
- **Space page**: opened from a space card's header; a grid shows each tab's state, title, latest progress and pending count. You can **Open a new tab in this space**,
  which creates a tab in the same herdr workspace (`workspace_id` of `session.start`).
