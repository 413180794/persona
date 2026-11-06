# To-dos (Todos)

Record something to do, hand it to one or several Agents, and see in To-dos which Agents are working on it, what each is doing and how far the overall task has progressed; Agents can also be grouped by to-do on the Agents page.

## Why this is needed

One task often needs several Agents (different computers, workspaces and tabs). An Agents list arranged by project, state or computer does not show which ones are working on the same task, its overall progress, where it is blocked or who it is waiting for. To-dos collect scattered Agents under one goal for progress tracking and coordination.

## Who owns what (since 2026-11-04)

To-dos belong to the assistant (`todos.go`, `todo_decisions.go`, `todo_sessions.go`, `tasks_close.go` in `server/internal/assistant`; formerly in hub, see `docs/assistant.md`, "Boundary with the hub"):

- **Assistant**: storage (the four tables below, still in the hub's Postgres, no migration, read/written only by the assistant process), every to-do API in "API overview" below, automatic project linking and following spawned sessions, reconciliation of herdr markers, snapshots of Agent titles/start errors, decision records, automatic completion and closing Agents for completed tasks.
  When hub receives a computer's session list it emits `sessions.changed`; when the user handles a card it emits `interaction.resolved`. These behaviors come from those two events.
- **Hub**: only forwards to-do APIs (`assistantwire.Routes`, carrying caller role; read-only roles still get 403), and uses the assistant's snapshot report for App session lists, question cards and pushes (`todos`: which unfinished to-do each Agent works on and which Agents are quiet, `hub/assistant_todos.go`; quiet reports only cover Agents of unfinished quiet tasks or those completed less than 7 days ago (`quietKeep`), keeping the report small; Agents from older tasks still push normally if they work again);
  `POST /api/v1/sessions` and `POST /api/v1/sessions/{mid}/{sid}/messages` with `todo_id` first ask the assistant (`assistant.todo_handoff`): does the to-do exist, is it done (if handoff is disallowed, it supplies 404/409 and the message, echoed by hub), prepend decisions to the Agent's message and project context for new Agents, all in one call. After handoff, ask assistant to link it (`assistant.todo_link`; assistant determines both linked title and to-do title); workspace renames through hub notify assistant (`todos.space_renamed`). Protocol in `docs/protocol.md`, "Bus".
- **When assistant is disconnected** (hub just started, assistant process restarting): to-do APIs return `503 assistant_offline`, "The assistant is temporarily unavailable. Try again later"; create/send with `todo_id` also return 503 (to-do existence unknown). Session-list to-do/quiet markers keep hub's last report (absent in the first second or two after hub starts before assistant reports). Hub emits no events during this period, so automatic linking, marker reconciliation and decision records wait for the next session list or answer after reconnection; workspaces renamed through hub are not migrated.

## Data model

Server state is stored in the hub's Postgres database (read/written by the assistant, above):

### 1. todos table
- `id` (text, PK): opaque string identifier (e.g. `todo_3f2a1b9c`).
- `title` (text, NOT NULL): to-do title.
- `notes` (text, NOT NULL, DEFAULT ''): detailed description and requirements, sent as context when handed to an Agent.
- `done_at` (timestamptz): completion time; `NULL` while unfinished.
- `created_at` (timestamptz, NOT NULL): creation time.
- `updated_at` (timestamptz, NOT NULL): last modification time.
- `origin` (text, NOT NULL, DEFAULT ''): assistant chat that created the task (session id); empty for manual creation ([unified-entry.md](unified-entry.md)).
- `plan_id` (text, NOT NULL, DEFAULT ''): plan that created it (`task_plans.id`).
- `done_by` (text, NOT NULL, DEFAULT ''): who marked it done: empty = user tapped (or launch failed, see `start_error`), `auto` = assistant automatically completed it after judging it finished ("Automatic completion"), `review` = supervisor completed it after acceptance (#3124).
- `done_reason` (text, NOT NULL, DEFAULT ''): judgment model's reason for automatic completion (e.g. "PR merged and TestFlight released"), cleared when user marks done or reopens.
- `reopened_at` (timestamptz): last time user reopened a completed to-do; assistant only judges automatic completion again after a new turn of work follows it.
- `thread` (jsonb, NULL): the chat thread the to-do is bound to, `{"channel", "chat_id", "thread_id", "title"}` (#5279, "Chat thread" below); `NULL` while it has none. A `CHECK` requires all three keys, and a unique index `todos_thread` over `(thread->>'channel', thread->>'chat_id', thread->>'thread_id') WHERE thread IS NOT NULL` lets a thread belong to one to-do only. One value, read and written as a unit, hence a jsonb column like the ledger's `reviews` rather than four more columns on every to-do read; the binding goes with the row when the to-do is deleted.
- (Not in the table) `stage`, `pr_url`, `delivery`: stage strip, PR and delivery-card location, stored in settings `assistant.todo_stages`, merged when reading to-dos ("Delivery").

### 2. todo_spaces table
Records project workspaces (herdr workspaces) linked as a whole to a to-do:
- `todo_id` (text, NOT NULL, FK → `todos(id)` ON DELETE CASCADE): linked to-do ID.
- `machine_id` (text, NOT NULL): machine ID.
- `space` (text, NOT NULL): project name (session `space_name`, or `space_id` if absent).
- `linked_at` (timestamptz, NOT NULL): link time.
- Constraints:
  - Primary key: `(todo_id, machine_id, space)`.
  - Unique index: `UNIQUE (machine_id, space)`, ensuring a project follows only one to-do at a time.

### 3. todo_agents table
Records associations between to-dos and Agent sessions:
- `todo_id` (text, NOT NULL, FK → `todos(id)` ON DELETE CASCADE): linked to-do ID.
- `machine_id` (text, NOT NULL): machine ID.
- `session_id` (text, NOT NULL): session ID.
- `linked_at` (timestamptz, NOT NULL): link time.
- `released_at` (timestamptz): time active association was released; `NULL` while active.
- `title` (text, NOT NULL, DEFAULT ''): session-title snapshot (still displayable beyond the session-list time window).
- `cwd` (text, NOT NULL, DEFAULT ''): session working-directory snapshot.
- `agent` (text, NOT NULL, DEFAULT ''): launcher/Agent-type snapshot (`claude` / `codex`, etc.).
- `start_error` (text, NOT NULL, DEFAULT ''): asynchronous creation failure reason (viewable/removable in To-dos).
- `source` (text, NOT NULL, DEFAULT 'manual'): association source:
  - `manual`: manually linked or handed to an existing Agent;
  - `space`: linked with the entire workspace project;
  - `spawned`: spawned by a parent Agent linked to the to-do.
- `via` (text, NOT NULL, DEFAULT ''): association path:
  - For source `space`, project key `<machine_id>/<space>`;
  - For source `spawned`, parent Agent identifier `<machine_id>/<session_id>`.
- `dismissed_at` (timestamptz): manual removal timestamp. Removal sets both `released_at` and `dismissed_at`. APIs exclude removed records, and subsequent session updates do not automatically relink them.
- Constraints:
  - Primary key: `(todo_id, machine_id, session_id)`.
  - Partial unique index: `UNIQUE (machine_id, session_id) WHERE released_at IS NULL`, ensuring an Agent actively works for only one to-do at a time.

### 4. todo_decisions table
Decisions the user has made for this task ("Decision records"):
- `id` (text, PK): opaque identifier starting with `dec_`.
- `todo_id` (text, NOT NULL, FK → `todos(id)` ON DELETE CASCADE).
- `question` / `choice` / `reason` (text): question, selected choice, reason (reason may be empty).
- `source` (text, NOT NULL, DEFAULT 'manual'): `manual`, recorded manually in the to-do; `card`, recorded automatically when user answers an Agent question card; `outcome`, conclusions, decisions and lessons extracted from the Agent's last reply when the task finishes ("Record conclusions on completion" below); `agent`, noted by an Agent on the to-do while it works (`todo_decision_add`, below).
- `source_machine` / `source_session` / `source_message` (text): provenance; for `card`, the asking Agent and card (interaction id), empty for manual records.
- `supersedes_id` (text, FK → `todo_decisions(id)` ON DELETE SET NULL): earlier decision this one supersedes.
- `created_at` (timestamptz, NOT NULL).
- Constraint: partial unique index `(todo_id, source_message, question) WHERE source_message <> ''`, recording each question on the same card only once.

### 5. todo_supervision table
Ledger for tasks supervised by the main chat (#3124, `docs/unified-entry.md`, "Supervision"), one row each; unsupervised to-dos have no row:
- `todo_id` (text, PK, FK → `todos(id)` ON DELETE CASCADE).
- `supervisor` (text, NOT NULL): supervisor, the chat that created it.
- `done_when` (jsonb, NOT NULL, DEFAULT '[]'): acceptance criteria (string array); `expect_minutes` (integer, NOT NULL, DEFAULT 0): expected minutes.
- `state` (text, NOT NULL, DEFAULT ''): `''` in progress, `pending_review`, `rework`, `needs_user`, `accepted`, `cancelled`; `round` (integer): rework round.
- `reviews` (jsonb, NOT NULL, DEFAULT '[]'): each review (`verdict`, per-item `checks`, `note`), at most 20 kept; `updated_at` (timestamptz).

Link/release semantics:
- When an Agent is linked to a new to-do and is active on another, the same transaction marks the original association `released` (records `released_at`); the old to-do shows it as "Previously involved".
- If an Agent previously involved in this to-do is manually linked back, clear `released_at`/`dismissed_at`, and refresh `linked_at` and snapshot information.
- When the user removes an Agent from a to-do, set that row's `released_at`/`dismissed_at` (soft removal, excluded from API results).

## API overview

`/api/v1/todos…` are assistant APIs, forwarded unchanged by hub (`503 assistant_offline` when assistant is disconnected); the last two rows are hub's own APIs, asking assistant for the `todo_id` part.

| Method | Path | Description |
| --- | --- | --- |
| GET | `/api/v1/todos` | All to-dos (`created_at` descending), each with `spaces` and `agents` lists (removed rows filtered out; active first by `linked_at` descending, previously involved after by `released_at` descending) |
| POST | `/api/v1/todos` | Create `{"title", "notes"?}` → `{"todo": Todo}`; nonempty `title` required (400); read-only role (watch) 403 |
| PATCH | `/api/v1/todos/{id}` | Edit `{"title"?, "notes"?, "done"?}` → `{"todo": Todo}`; only supplied fields change; user completion-state changes clear `done_by` / `done_reason`, and reopening records `reopened_at`; missing 404; read-only role 403 |
| POST | `/api/v1/todos/retitle` | Give short titles to task to-dos previously named from the prompt's first line ("To-do titles") → `{"dry_run", "todos": [{"id", "old_title", "new_title"}]}`; `?dry_run=1` lists without changes; read-only role 403 |
| POST | `/api/v1/todos/{id}/done-check` | Run an "Automatic completion" judgment read-only → `{"verdict", "reason", "would_close", "skip", "replies", "signals"}`, no database write ("Automatic completion"); missing 404; read-only role 403 |
| DELETE | `/api/v1/todos/{id}` | Delete to-do and associations → `{"ok": true}`; Agent sessions untouched; missing 404; read-only role 403 |
| PUT | `/api/v1/todos/{id}/spaces/{mid}/{space...}` | Link an entire workspace project → `{"ok": true, "linked": N}`; `space` is URL-encoded project name (wildcard path); completed to-do cannot be linked (409); missing 404; read-only role 403 |
| DELETE | `/api/v1/todos/{id}/spaces/{mid}/{space...}` | Unlink the project (physically delete this project's space-linked Agents under the to-do) → `{"ok": true}`; missing 404; read-only role 403 |
| PUT | `/api/v1/todos/{id}/agents/{mid}/{sid}` | Link an existing Agent (no message) → `{"ok": true}`; clears `dismissed_at`; completed to-do cannot be linked (409); missing 404; read-only role 403 |
| DELETE | `/api/v1/todos/{id}/agents/{mid}/{sid}` | Remove Agent (soft removal: sets `released_at` and `dismissed_at`, excluded from API and never automatically relinked) → `{"ok": true}`; missing 404; read-only role 403 |
| PUT | `/api/v1/todos/{id}/thread` | Bind the to-do to a chat thread `{"channel", "chat_id", "thread_id", "title"?}` (replaces the one it had) → `{"todo": Todo}` with `thread`; the three ids are trimmed, required and at most 256 characters (400), the title is trimmed and clipped to 200; another to-do holds the thread 409; missing 404; read-only role 403 ("Chat thread") |
| DELETE | `/api/v1/todos/{id}/thread` | Unbind it → `{"todo": Todo}`; a to-do with no thread stays as it is; missing 404; read-only role 403 |
| GET | `/api/v1/todos/{id}/decisions` | Decision list → `{"decisions": [...]}`, chronological; effective only by default, `?all=1` includes superseded decisions (with `superseded_by`); `[]` when none; missing 404 |
| POST | `/api/v1/todos/{id}/decisions` | Record manually `{"question", "choice", "reason"?, "supersedes_id"?}` → `{"decision": Decision}`; empty question/choice 400, `supersedes_id` not an earlier decision in this to-do 400; read-only role 403 |
| PATCH | `/api/v1/todos/{id}/decisions/{did}` | Mark supersession after the fact `{"supersedes_id"}` (empty string undoes) → `{"decision": Decision}`; same rules; missing 404; read-only role 403 |
| POST | `/api/v1/sessions` | Create session, optional `todo_id`: after successful creation, automatically link within the same request; missing to-do 404, completed 409, read-only role 403 |
| POST | `/api/v1/sessions/{mid}/{sid}/messages` | Send message, optional `todo_id`: automatically link after success; missing to-do 404, completed 409, read-only role 403 |

### Session decoration fields
In session lists (`GET /api/v1/sessions` and WebSocket `sessions` pushes), hub injects to-do fields using the assistant snapshot's report (`todos.links`):
```jsonc
"todo": {
  "id": "todo_weather",
  "title": "Release weather App 1.3"
}
```
Only present when the session is currently associated (`released_at` and `dismissed_at` both `NULL`) and the to-do is unfinished. On release, transfer or completion, the field automatically clears; clients need no frontend join.
The internal `spawn_from` field carried in session reports is stripped before hub pushes to clients.

### Live WebSocket pushes
- `{"type":"todos"}`: broadcast on to-do creation, edit, deletion, whole-project link/unlink, Agent association changes or a change of its chat thread; clients refetch `GET /api/v1/todos`.
- Session-change push: when to-do/association changes affect sessions, assistant reports a new snapshot; hub compares which computer's sessions changed and re-pushes that computer's `sessions` event.

### Status codes and roles
- Missing title or malformed parameters: 400 Bad Request.
- Missing to-do: 404 Not Found. Old hubs without to-do APIs return 404; clients hide the entry point without an error popup.
- Linking to a completed to-do: 409 Conflict. Binding a chat thread another to-do holds: 409 Conflict.
- Assistant disconnected: 503 `assistant_offline` (to-do APIs and create/send with `todo_id`).
- Read-only role (e.g. paired Apple Watch, `role: "answer"`): every to-do write API (POST / PATCH / DELETE / PUT), plus create/send with `todo_id`, returns 403 Forbidden.

## Whole-project linking and automatic linking

### Whole-project linking
A herdr workspace can be linked as a whole to an unfinished to-do:
- On linking, batch-link every currently online Agent in the project (state not offline), `source = "space"`, `via = "<machine_id>/<space>"`.
- If the project is already linked elsewhere, unlink it first (remove the old to-do's space-linked Agents belonging to this project), then transfer to the new to-do.
- If an Agent in the to-do was dismissed, whole-project linking clears that dismissed state too.
- Unlinking deletes the workspace association and physically deletes Agent rows under this to-do with `source = "space"` and this project's `via` (manual/spawned Agents remain).

### Automatic linking with a project
When assistant receives a computer's session list (`sessions.changed`), it checks online Agents in whole-linked projects:
1. Agent currently has no other active to-do association (automatic rules do not take Agents away from other to-dos);
2. Target to-do has never had any record of this Agent (active, previously involved or manually removed all count as previously present, so no automatic relinking);
When both hold, automatically link to the to-do (`source = "space"`).
Automatic linking first updates assistant memory and reports to hub immediately (hub pushes that computer's session list again); database writes happen in background and roll back if they fail.
Previously hub linked it itself and included the association in that same session-list push; now it waits for the next push after assistant reports back (one bus round trip).
Automatic linking stops when a to-do is completed and resumes when reopened.

For renames through hub (shared with pin/mute's rename API), hub tells assistant (`todos.space_renamed`), migrating the space name in `todo_spaces` and corresponding `via` in `todo_agents` together. Assistant writes database first, then memory and broadcasts `{"type":"todos"}`. If the new name is already linked to another to-do (unique-index conflict), this project's old to-do link is invalidated and cleaned up, with a log entry. No migration if assistant is disconnected (same as "Known limitations", item 1).

## Following spawned Agents

Child Agents spawned by a linked Agent automatically join the same to-do, tracking their parent hierarchy:
1. **Parent marker**: an active Agent on an unfinished to-do has a `persona_todo_ref` marker on its herdr pane, value `<todo_id>@<machine_id>/<session_id>`.
2. **Dispatcher passes the marker**: after launching a child, the dispatcher reads its own pane marker and writes the value to the child's pane as `persona_from`.
3. **node reports, assistant associates**: when discovering sessions, node copies `persona_from` into `spawn_from` and reports to hub; hub passes the session list unchanged to assistant (`sessions.changed`). Assistant parses to-do ID/parent Agent; if unfinished, child has no active association and no history in that to-do, automatically link it (`source = "spawned"`, `via = "<parent_machine>/<parent_session>"`). Following does not depend on parent being online or on the same computer.
4. **Grandchildren follow**: after the child joins, node also sets `persona_todo_ref` on its pane; further generations follow the same mechanism.
5. **Consume the marker**: after child gets an active association, node's periodic reconciliation automatically clears its pane's `persona_from`, preventing mistaken relinking later.

### Dispatcher contract and example command
After launching a child Agent (on the same or another machine), the dispatch script should copy the marker as follows:
```sh
REF=$(herdr pane get "$HERDR_PANE_ID" | jq -r '.result.pane.tokens.persona_todo_ref // .result.pane.tokens.persona_from // empty')
[ -n "$REF" ] && herdr pane report-metadata "$CHILD_PANE" --source persona --token "persona_from=$REF"
```
Note: for a child on another machine, read the parent pane marker locally, then send the command that writes `$CHILD_PANE` to the target machine's herdr.

### botmux following
botmux sessions have no herdr pane or `persona_from`. node reads each session's Lark group (`raw.chatId`) and thread-root message (`raw.rootMessageId`) from `botmux observe --include-raw`, then reads `orchestrate-dispatch.json` in the botmux data directory (`SESSION_DATA_DIR`, otherwise the directory referenced by `~/.botmux/.data-dir`, otherwise `~/.botmux/data`): keyed by dispatched thread-root messages, with `orchSessionId` identifying the dispatching session (`botmux dispatch --session-id`). These three values report to hub with the session (`chat`, `topic`, `dispatched`) and are stripped before pushing to clients. After the first two rules, assistant tries two more, only for Agents with no active to-do and no previous record in the target to-do:
1. **Follow dispatch**: if the session's thread was dispatched with `botmux dispatch` by a session (on any computer; offline counts if still in its computer's list, i.e. offline no longer than `offlineWindow`) linked to an unfinished to-do, join that same to-do (`source = "spawned"`, `via = "<machine>/<session>"`).
2. **Follow the group**: among unfinished to-dos linked by other online botmux Agents in the same Lark group, if the largest count is at least twice the runner-up (automatic when only one to-do exists), join it (`source = "chat"`, `via` is group id; client shows **Same Group**). Ties or insufficient margin do not link; wrong links can be changed manually. Agents linked by this rule do not vote (otherwise one Agent could spread a to-do across the entire group), nor do Agents on computers with no list report for 10 minutes.

## herdr markers and reconciliation

Under herdr, to-do associations map to pane markers (shown in the sidebar):
- **Written markers**: for Agents linked to unfinished to-dos, node maintains two metadata tokens on their pane:
  - `persona_todo`: to-do title;
  - `persona_todo_ref`: `<todo_id>@<machine_id>/<session_id>`.
- **Clearing markers**: when a to-do completes, is deleted or an Agent is unlinked, node clears its `persona_todo` and `persona_todo_ref`.
- **Source/namespace protection**: all writes/cleanup use `--source persona`. Reconciliation only touches `persona_todo`, `persona_todo_ref` and `persona_from`, never user or other plugin metadata.
- **Periodic reconciliation**: assistant sends expected markers by session ID (node RPC `todo.sync_session_tokens`, via `device.call`): immediately for computers whose associations changed, no resend otherwise; on a new node/hub connection (`connected` in `sessions.changed` changes: node may have restarted, do not touch markers until received), resend all with the first session list on that connection, like hub's former immediate resend after node reconnect. node resolves live panes dynamically only from currently living sessions and host-reported current Agent sessions, compares herdr's markers with expectations, fills missing and removes surplus markers. If two sessions map to one pane, only the live one counts. node reconciles every 30 seconds; after herdr restart, pane-ID change or hot handoff, markers restore on the correct new pane without old exited sessions contaminating it.

## State calculation rules

Clients calculate to-do runtime state live from linked active Agents; it is not persisted. Use the first matching state in this priority order:

| Priority | State | Condition |
| :--- | :--- | :--- |
| 0 | Didn't Start | Agent for a task created from chat failed to launch; assistant ends the to-do (`done_at` + `start_error`, #1077); client shows failure/removal, not active |
| 1 | Done | User marked done, or assistant judged a dispatched task complete (`done_at != null`; client shows **Auto-completed** for `done_by = "auto"`) |
| 2 | Needs You | A linked active Agent is `needs_you` (waiting for question, plan approval or authorization) |
| 2.5 | Interrupted | A linked active Agent is stopped (`unread` / `idle`) with `turn_error`: model-service failure interrupted the turn (disconnect, overload, rate limit, expired login…), unfinished (#1516); red, **Continue** on the card sends "继续" ("continue") |
| 3 | Unread | A linked active Agent is `unread` (task finished, not viewed yet) |
| 4 | Active | A linked active Agent is `working` (working or starting) |
| 5 | Follow up | All linked active Agents are stalled (`idle` seen, `offline` not running, or launch failed) |
| 6 | Not started | No linked active Agents |

- To-do list ordering: unfinished first (same state by `created_at` descending), completed collapsed at bottom.
- Completion means the task is over: its Agents' sessions no longer say they currently work for it; **Reopen** to continue.
- User marks done while Agent is still launching (can take minutes on a busy machine; `done_at` written first): not **Didn't Start**, to-do stays done with no `start_error`; Agents that do launch join this completed to-do and are reclaimed by "Close Agents for completed tasks" under existing rules (only after idle long enough) (#2954).

## To-do titles

To-dos created by `task_plan` and `agent_start` (#2775) formerly used the prompt's first line as title, often starting with instructions to the Agent ("ultrathink ultracode 主人 11-05 19:47 的反馈，已授权直接开工：…", meaning feedback from the owner, authorized to start; "秘书派活（主人授权过…）", meaning assistant dispatch, owner already authorized), obscuring the task itself. Now two steps (`assistant/todo_titles.go`):

1. **On creation**, synchronously supply a cleaned title: remove leading keywords such as ultrathink / ultracode / think hard, provenance/timestamps such as "主人 11-05 19:47 的反馈" (owner's feedback), parenthetical notes such as "秘书派活（…）" (assistant dispatch) / "【…】", and "已授权直接开工" (authorized to start); truncate to 20 Chinese characters (20 Japanese, 40 characters for other languages). No model call, no delay to task launch.
2. **In background**, the small-task model (**To-do Title** `todo.title` in model configuration, inheriting the knowledge-organization category, through `models.complete` like chat titles `assistant.title`) reads the prompt and produces a short title explaining the task, replacing step 1. Keep step 1 if model unavailable, errors or empty result. Before replacement, verify title is still step 1's (conditional `UPDATE … WHERE title = old title` in database); never overwrite an intervening user edit. Prompts: `prompts/todo/title.md`, `title_input.md`.

Assistant-supplied titles in `task_plan`: if within the limit and without those prefixes, use as-is without a model; only first-line, overlong or prefixed titles use the two steps.

**Batch retitle existing to-dos**: `POST /api/v1/todos/retitle` selects only task-created to-dos (nonempty `origin`) whose title is still the prompt's first line truncated at dispatch: over the limit and a prefix of `notes` (truncated, with or without "…"). Short titles merely resembling prompts ("【紧急】修登录", "[Urgent] fix login"; "11/05 反馈：修登录", "11/05 feedback: fix login") count as user-created and stay untouched (to-dos do not record who named them; this is the most conservative possible judgment). Use model for each (4 concurrent), or step 1 cleanup without a model; again verify title unchanged before writing.

```sh
curl -X POST '<hub address>/api/v1/todos/retitle?dry_run=1' -H 'Authorization: Bearer <token>'   # Preview changes first
curl -X POST '<hub address>/api/v1/todos/retitle' -H 'Authorization: Bearer <token>'             # Then apply
```

## Automatic completion

Tasks dispatched from assistant chats (to-dos created by `task_plan`, nonempty `origin`) are automatically marked done by assistant, without individual user taps (except tasks supervised by main chat: completion judgment moves to "Pending review", and supervisor must accept with `task_review`, `done_by = review`, see `docs/unified-entry.md`, "Supervision"). Manually created to-dos (empty `origin`) never auto-complete; only user marking done counts.

**When judged**: all task Agents stop (none working, waiting for user or carrying pending cards), at the same point assistant sends the done receipt to chat (`taskTurnEnded` in `assistant/tasks.go`; one receipt and one judgment per stretch of work).

**How judged**: stopped is not finished; "not all changes done…" or "waiting for review" must not close. Assistant's judgment model (through hub model configuration, `app.call models.json`; **AI Twin Judge** `inbox.judge`, i.e. hub.json `inbox.judge_ai`, inheriting `ai` if unset) sees three things: task title, dispatch requirements (to-do `notes`), each active Agent's latest turn (read via `session.items`: user's message and last complete reply; fallback truncated `last_text` in session list if unreadable). Later new requirements from user override original ones (already handled in earlier turns); if later messages merely confirm/answer a question ("OK", "👌", "Use A"), original dispatch requirements still apply (#1116). Model chooses one of five:

| Verdict | Meaning | Auto-complete |
| --- | --- | --- |
| `done` | Requested work finished, reply gives results, user need do nothing more, no waiting for others | Yes |
| `not_done` | Partial, failed, blocked, or only reporting progress/plan | No |
| `waiting` | Own work finished, awaiting external result (review, CI, someone else's reply, release queue) | No |
| `needs_user` | User must decide, supply information, approve, act manually or accept | No |
| `unsure` | Cannot tell | No |

- Caveats do not prevent completion: platforms without screenshots, unmeasured environments and reminders to user are not unfinished work. Routine waits after delivery (TestFlight external-group Beta review, CI rerun) do not count as waiting either; addressing App Store / Mac App Store review feedback then awaiting review outcome does count as `waiting`.
- Prefer leaving open: unavailable model, timeout, invalid JSON or verdict other than `done` leave to-do untouched and only log; receipt still sends.
- No judgment for a turn interrupted by model service (#1516): if an Agent stops on an API error (`turn_error`: Claude Code's last message such as "API Error: …", Codex reported by node from this turn's error, #2185), receipt has `state: "interrupted"` (summary "Model-service error interrupted the task", original error in `error`); no judgment-model call or closing to-do. `done-check` directly returns `not_done` when the last reply is such an error.
- No judgment for a turn with no output either (#2349): latest stopped Agent's turn (node `session.items`, `turns: 1`) contains no assistant text, tool calls or images and no `turn_error` (model service returns 200 with empty content; Claude Code adds an invisible "no visible output" notice, retries, then ends silently). Receipt is `interrupted` (error "这一轮没有任何输出：…", "This turn had no output: …"), notification uses interrupted, no judgment or to-do close. Unreadable items (node unreachable/no items) or user pressing Esc ("已中断" notice, "Interrupted") do not count.
- Cheap signals: final reply has a GitHub PR link and mentions merging (merged / 已合并 / 合入, the latter two meaning merged), or mentions TestFlight plus a build number. Assistant includes these as hints only, never enough alone for completion.
- Prompts: `server/internal/prompts/assistant/task_done.md`, `task_done_input.md` (`docs/prompts.md`).

**Visible and reversible**: auto-completion writes `done_at`, `done_by = "auto"`, `done_reason`. iOS / Mac / web list/detail show **Auto-completed** beside Done; detail shows reason. **Reopen**, like manual completion, returns to unfinished, clears `done_by`/`done_reason`, records `reopened_at`.

**After reopening**: same stretch of work cannot auto-close again; only when an Agent begins a new turn after `reopened_at` (user sends again, Agent resumes) is completion judged again. After assistant restart the turn's start time is unknown; reopened to-dos are not judged then (prefer leaving open). Database write rechecks (`done_at IS NULL`, `reopened_at` before work start); user completion/reopen during judgment wins.

**After completion**: same path as user marking done: sessions lose to-do field, node reconciliation clears `persona_todo` / `persona_todo_ref`, whole-project linking/spawn following stop, Inbox AI twin no longer associates messages with it; assistant live snapshot marks task "Done (automatically closed after hub judged complete)", omitted after 24 hours.

**Diagnosis**: `POST /api/v1/todos/{id}/done-check` judges the same inputs, returns verdict only without database writes (read-only role 403):

```jsonc
{"verdict": "waiting", "reason": "Waiting for App review", "would_close": false, "skip": "", "replies": 1, "signals": {"merged_pr": false, "build": false}}
```

Nonempty `skip` means automatic flow would not judge this to-do (`not_a_task` manual, `done` completed, `reopened` no new work after reopen), but API still returns model verdict for inspection.
With **Delivery** enabled (next section), response also has `delivery`; `verdict` may be hub's own `undelivered`.

## Delivery

(#3306, lessons in `docs/lessons/delivery.md`) A code-change task is "done" only when live and tested, not when a PR is opened or merged. Judging only whether an Agent says finished leaves many tasks at "PR opened", "merged but off by default" or "not deployed" while showing Done.
Assistant setting `done_needs_delivery` is **off by default** (off retains the old verdict-only behavior). Enable either way:
tell assistant "Turn on 'only done when live and tested'" (`assistant.done_needs_delivery` in `jarvis_settings`, confirmation card on phone), or `PUT /api/v1/assistant` with `{"done_needs_delivery": true}`;
effective from the next judgment and next dispatched task. When on:

- **State criteria at dispatch**: for non-read-only tasks created by `task_plan`, hub appends delivery criteria to the Agent's first message (dispatch prompt, `prompts/assistant/task_delivery.md`): done = live and tested, with measurements;
  if rules permit self-merge/deploy/enable, finish the whole path; if current user approval is required (merging a repo not owned by user, production deploy, enabling an off-by-default feature, outbound messaging), stop one step before approval and state what needs approval;
  first try to resolve CI/other-person/environment blocks yourself; if impossible, state where and who can unblock. Fixed last line: `交付：<已上线并实测 | 停在 PR | 已合未上线 | 默认关待启用 | 不涉及上线>；卡点：…；谁来解：…` ("Delivery: <live and tested | at PR | merged, not live | off by default, awaiting enablement | no deployment involved>; blocker: …; who can resolve: …"). Read-only tasks omit it.
- **Judge one more thing**: the "Automatic completion" model also returns `delivery` (`live` / `pr` / `merged` / `off` / `none`), `gate` (blocking gate), `unblock` (`user` / `agent` / `other`), taking the Agent's line as authoritative unless contradicted by the body.
  If model omits it, use Agent's line; if neither exists, `none`. If `live` (or not judged) but reply mentions a PR without deployment or TestFlight build-number wording, treat as `pr` / `merged`, unless the Agent's line says live.
- **Only `live` and `none` auto-complete**: if `verdict` is `done` but delivery incomplete, change to `undelivered` and keep to-do open.
  Main-chat-supervised tasks (#3124, already not closed by hub) do not enter pending review; supervision message says "Finished but not live yet: …(at PR, blocked on '…')", for main chat to follow up or ask user approval.
- **To-do shows the blocker**: judgment results except `not_done` are recorded in to-do `delivery` (`{"state", "gate", "unblock", "at"}`, settings `assistant.todo_stages` alongside stage strip, no new table); stage advances at least to its corresponding step (PR → review, merged → merge, off by default/live → deploy).
  Web to-do rows add "At PR · Blocked on '…' · Needs your approval" while open and `state` is `pr` / `merged` / `off`; Android to-do rows (when the Agent is neither working nor waiting for you) and details show the same line (`Todo.hold` in `core:domain`); iOS / Mac do not render it yet (optional field, old clients unaffected).
- **Batch user approvals**: assistant live snapshot lists delivery blockers after each task; open tasks with `unblock = user` get a separate "Waiting for user approval to go live" group (oldest ten).
  Assistant CLAUDE.md tells it to present these together in one message; after user approves in one reply, use `agent_send` to notify the corresponding Agents separately, rather than asking anew for every item.

## Close Agents for completed tasks

After a task dispatched from assistant chat (nonempty `origin`) completes—manually or automatically—its Agent no longer occupies a herdr tab:
assistant checks every minute (`assistant/tasks_close.go`, reading hub's App-facing session list through `sessions.list`); once task completion and Agent inactivity both reach 3 minutes (`taskCloseIdle`, measured from the later of completion time and last activity), ask node to close it (`session.retire`, `node/retire.go`):
close its pane (whole tab if it is the only pane, same as **Close** on phone), exiting the Agent process with it.

**What stays**: to-do Agent record (machine, session id, directory) remains, not released; node's launch information (launcher, directory, model, hub `node_sessions`) and transcripts remain.
Results still read from transcripts (receipts, `agent_read`, chat page); Unread Agents may also close.

**How to reuse**: send to a closed Agent—`agent_send`, directly to task from its card (`route`), or chat page—and node first resumes in a new tab using original launcher, directory and workspace (tab named from session title), then sends the message:

| Agent | Resume method |
| --- | --- |
| Claude Code | `<launcher> --resume <id>`, with original permission mode, thinking effort and model |
| Codex | Launcher arguments (with original model substituted) `resume <id> -- <message>`, through `personad codex-start`; message passed as an argument, not through shell or on screen |
| Compatible Codex forks | `<launcher> resume <id> <message>` |

Resume waits for launcher readiness; like chat sends, `route` is not cancelled by phone disconnection, waiting at most 5 minutes. Completed to-do does not reopen for this message; after another 3 minutes idle, Agent closes again.

**Do not close**: working, waiting for user (questions, plans, permissions, any dialog, pending cards), unknown engine state; queued messages, background shells, running sub-Agents, `/loop`, listening ports (development servers); linked to another unfinished to-do; assistant chats; sessions managed by other tools (external Codex app-server, runtime, daemon, pi); not started by Persona (unknown resume method); engines unable to create tabs (Orca, herdr-core); Claude without transcript, Codex / compatible Codex forks without known launcher.
node rejects these (`not_retirable`), assistant records them and does not ask again for 10 minutes if Agent unchanged (`taskCloseRetry`, then retries because node may have upgraded). Old nodes not recognizing `session.retire` are likewise treated as refusals, so hub-first/node-later upgrades do not close wrongly; after node upgrade, closure resumes within at most 10 minutes.
Agents linked to no task (user-created, `agent_start`, inspection sessions such as herdr-leader) are not managed here.

**`/exit` from the assistant**: `agent_send` with only `/exit` or `/quit` closes a running Agent as **Close** does (its tab goes too) instead of typing it in; to an Agent that is not running it does nothing, so a closed Agent is not resumed in a new tab only to quit there.

**Tabs left with a bare shell** (`node/shelltabs.go`): an Agent Persona started can also end on its own (`/exit` typed by hand, an API error it gives up on, a launcher that quits), leaving its tab with only the login shell and `Resume this session with: claude --resume <id>`.
Every tab Persona opens for an Agent carries the herdr pane token `persona_tab` (the session id when known). Once a minute node looks at such panes with no Agent in them: when only shells are in front and the screen has not changed for 10 minutes (`shellTabIdle`; any output or typing restarts the wait), it closes the pane (the whole tab when it is the only one). The session is untouched: the next message resumes it as above.
Never closed: panes without the token (tabs the user opened, the pane picked for **Run Agent Here**), panes where another program is in front, a session being started or resumed, assistant chats. herdr does not keep pane tokens across a herdr server restart, so tabs opened before one are left alone.

## Decision records

The costliest long-task mistake is an Agent forgetting a decision already made: asking again or silently overturning it (#1618). Decisions scattered across Agent chats disappear when switching Agents or compacting, so collect them by to-do:

- **One decision** = question + choice + reason + time + provenance (asking Agent, answering card).
- **Automatic**: when user answers Claude Code's choice question in App (`question` card, `AskUserQuestion`) and the Agent is linked to an unfinished to-do, record each answered question (`source = "card"`; hub emits `interaction.resolved`, assistant records; nothing recorded if assistant disconnected). Do not record authorization, plan approvals, terminal dialogs or ignored cards; duplicate reports of a card record once.
- **Manual**: **Decisions › Record one** in to-do details.
- **From the Agent** (#5279): an Agent on an open to-do has two MCP tools of its own (`mcp/todo/`, `agentToolNames` in `assistant/agent_control.go`). `todo_decisions` gives the effective decisions with who made each (`by`: `user`, or `agent` for `agent` / `outcome`), to reread after compaction; `todo_decision_add {question, choice, reason?, supersedes_id?}` notes a call the Agent made (`source = "agent"`, its machine and session as provenance), at most 10 in 10 minutes per Agent; it may replace an Agent's decision, never the user's (it is told to ask them). The to-do is always the one the calling Agent works on (node passes its pane's session, as for `report_to_supervisor`); an Agent cannot read or write another to-do's decisions, and outside an open to-do both say there is none. Handoff context marks these as the Agent's, not the user's.
- **Supersession**: a new decision can **Replace** an earlier one (`supersedes_id`), keeping the old one struck through; only unsuperseded decisions are effective. PATCH can mark/undo later corrections. Only earlier decisions in the same to-do can be replaced, preventing cycles.
- **Hand to Agent**: when `POST /api/v1/sessions` `prompt` or `POST …/messages` `text` carries `todo_id` (all clients' handoff does), hub asks assistant (`assistant.todo_handoff`); assistant prepends effective decisions as `<jarvis-context kind="context" title="待办 · <title> · 已定的决策">` ("To-do · <title> · Established decisions", prompt `server/internal/prompts/todo/decisions.md`), no client change needed; send unchanged without decisions. On read failure (database error), still send and only log.
- Recording/superseding updates to-do `updated_at` and broadcasts `{"type":"todos"}` so clients refetch decisions.

Not yet: Android / iOS decision lists.

## Chat thread

(#5279, step 2: the binding itself) A to-do can be bound to a chat thread, a topic in a group chat or a conversation, so that the thread and the to-do are known to belong together. Posting to the thread, taking its replies back as decisions and `todo.*` events come in later steps; this step writes nothing to any channel and changes no bot routing.

- **The binding**: `thread` on the to-do, `{channel, chat_id, thread_id, title}` (the first three name the thread, the title is a label). `channel` is a channel id as `internal/channel` names them (`channel.Feishu` = `"feishu"`); it is trimmed and lower-cased before it is kept, so `Feishu` and `feishu` are one channel. For `feishu`, `chat_id` is the chat's id (`oc_…`) and `thread_id` is the `thread_id` the message events carry (`omt_…`), not the thread's root message id (`om_…`). The server does not check them against the channel (it cannot know a thread exists), so the caller must pass the real ones. At most one thread per to-do and one to-do per thread: binding a thread another to-do holds is refused (409, from memory first, finally by the unique index, so a second assistant on the same database cannot slip past it). Binding again replaces the thread; the old one is free for another to-do. A done to-do can be bound or unbound too (it may be reopened, and the user's tidying should not depend on its state).
- **API**: `PUT` / `DELETE /api/v1/todos/{id}/thread` (full role only), "API overview" above. Both move the to-do's `updated_at` and broadcast `{"type":"todos"}`, unless nothing changed (unbinding a to-do that has no thread, or binding exactly what it already has: nothing is written); `GET /api/v1/todos` returns `thread` with each to-do that has one. Editing a to-do leaves its thread alone.
- **Reverse lookup** for the next step (a reply in a thread finds its to-do): in memory, every `Todo` carries `Thread` and `sameThread(a, b)` (`assistant/todo_thread.go`) compares two bindings; from the store, `DB.TodoOfThread(channel, chat_id, thread_id)` (`internal/db/todo_thread.go`).
- **From the Agent**: an Agent on an open to-do has `todo_bind_thread {channel, chat_id, thread_id, title?, todo_id?}` (`mcp/todo/`, with the decision tools; the node resolves its caller the same way, `callerTools`). It binds the to-do the Agent works on and no other: `todo_id`, if given, must be that to-do's own id. A to-do that already has another thread keeps it (changing it is the user's call, the tool says to ask them); the same thread again only updates its title. Outside an open to-do it says there is none.
- Web: `Todo.thread` is typed (`web/src/lib/todos.ts`); no screen shows it yet.

## Record conclusions on completion

Step 3 of the three-loop process (#2820): Agent conclusions automatically settle into the to-do/knowledge base, without user opening Agent; next dispatch for the same project includes them (`server/internal/assistant/todo_outcome.go`).

- **When**: main-chat-dispatched task (nonempty `origin`) becomes done: main-chat acceptance (`accepted` in `task_review`, `reviewDoneTodo`; all main-chat tasks are supervised, awaiting acceptance rather than self-closing), automatic completion (`autoDoneTodo`, unsupervised tasks), or user marks done (`done: true` in `PATCH /api/v1/todos/{id}`). Excludes manual to-dos, tasks ended by launch failure and review-cancelled tasks (`cancelled`). Runs in background, not slowing completion.
- **Extract**: read last-turn replies from all still-linked Agents (fallback session-list `last_text`; ignore model-service-interrupted turns), use model (`kb.project`, prompts `server/internal/prompts/todo/outcome.md`, `outcome_input.md`) to extract one-sentence conclusion (at most 120 characters), PR/deploy links (at most 3), decisions and lessons (at most 5 each); invent nothing not in replies. Reply reads get at most 1 minute, model 90 seconds, decision/timeline writes another 45 seconds, so a slow model does not delay writes.
  Without model (unconfigured, quota, unparsable reply), conclusion falls back to receipt text (latest Agent reply compressed to one line, at most 120 characters), links only GitHub PRs from replies, no extracted decisions/lessons.
- **Decision section**: record conclusion ("Completion conclusion" + conclusion/links), each decision (question/choice/reason), each lesson ("Lessons learned") separately, `source = "outcome"`, provenance latest Agent (`source_machine` / `source_session`).
  Once per completion: `source_message` is `outcome:<to-do id>:<completion time>#<index>`; duplicate triggers (two paths together, again after hub restart) check database first. Write conclusion first; only successful writer proceeds with decisions, lessons and timeline (one wins with two assistants sharing database); deleted to-do gets no writes. Reopen then complete is a new occurrence. Web provenance says "Recorded when Agent finished"; handoff includes these in established decisions, marked "Recorded when Agent finished, not decided by user".
- **Knowledge base**: project for each Agent working directory (`kb.Engine.ProjectsForWork`: first registered code `dirs`; if none match, match directory name to projects with no registered code dirs by name/slug/alias, never name-match projects with registered dirs),
  append `结论：<conclusion> <at most two links>` ("Conclusion: …") to `log.md` (at most 200 characters per line: keep links whole, truncate conclusion to remaining space), source `待办 · <title>` ("To-do · <title>", remove ` · ` from title, at most 60 characters), one line per project. Not limited by `kb.agent_turn`'s one-per-Agent-per-30-minutes rule. If knowledge base not open, retry once after 30 seconds; otherwise only record decisions.
- **Project context at dispatch**: for a new Agent, resolve project by target directory as above, prepend project summary and latest 5 `待办 ·` conclusions (newest first; only source beginning `待办 · ` and body beginning `结论：`, not same-named group-chat records) as `<jarvis-context kind="context" title="项目 · <name> · 背景">` ("Project · <name> · Background", prompt `server/internal/prompts/todo/project.md`); at most two projects per directory. Send unchanged without project, both summary/conclusions absent, or knowledge base closed.
  - Assistant `task_plan` (`startTaskAgent`, retry includes decisions too), `agent_start`: prepend directly; tab names still use original title/prompt.
  - App **Hand to Agent → New** through hub: `POST /api/v1/sessions` with `todo_id`/`prompt` asks assistant (`assistant.todo_handoff` with `new: true`, `machine_id`, `cwd`; expand `~/` and empty cwd using that computer's home), project context before decisions.
    Clients' Claude starts in background then sends first message (`async`, empty prompt): hub remembers "session newly created for this to-do" (memory, 15 minutes), treats its first message as new Agent's first input with context/decisions; recognizes iOS / Android first messages even without `todo_id`. Forget only after successful send; failed sends (rejected/nothing entered) retain context on retry; answer-only devices do not count. If hub restarted between, send unchanged.
  - Handoff to an existing Agent (message) includes decisions only, not project context.

Not yet: manual editing/undo of conclusions (currently **Replace** in decisions), Lark reporting (#2768), iOS / Android decision lists.

## Interaction design across clients

### 1. Sidebar and main navigation
- Sidebar has a **To-dos** entry with unfinished-count badge.

### 2. To-do list and details
- List: search bar, unfinished list, collapsed completed group at bottom. Each row shows title, state dot, linked-Agent count and latest message from the highest-priority Agent. Tap circle or swipe left to mark done.
- Details: edit title/notes directly; current state; linked projects (whole-project links) and active Agents (state dot, title, machine/workspace labels, latest-message summary, update time, tap to chat; beyond session time window use snapshot and mark "Ended"; launch failures show reason/removal); collapsed "Previously involved" list; persistent bottom **Hand to Agent** and **Done / Reopen** buttons; delete in top-right more menu.
- **Decisions** section under notes (web): effective decisions chronologically (question, choice, reason, time, provenance), each can **Change decision** (write a replacement) or ⋯ **Which decision did this replace?** / **Undo replacement**; when superseded records exist, **View full history**, with old records struck through. Hide whole section if hub unsupported (route 404).
- **Projects** section above Agents: each row "project · machine", with **Remove**; projects without online Agents show "No Agents currently": only sessions in current hub group, on connected computers, not offline count (same-ID computers in other groups do not; disconnected computer sessions marked unknown by hub rather than offline do not count either, matching web). Agent source says "With project <name>" or "Dispatched by <parent title>"; missing project source shows only "With project", missing parent only "Spawned", manual links unlabelled.

### 3. Hand to Agent
- **Start a new one** or **Hand to an existing one**.
- Context follows Inbox's hand-to-AI format: `<jarvis-context kind="context" title="待办 · <title>" url="">notes</jarvis-context>` (title means "To-do · <title>"), immediately followed by user's task instructions.
- New session carries `todo_id`; message to existing session carries `todo_id` and `client_msg_id`.

### 4. Agents view integration
- Agents grouping supports **By To-do** (#1836; iOS / Mac defaults by repository when no grouping chosen, #2108): each unfinished to-do with active Agents is a separate section; header shows title/state and opens details. Unlinked manually started herdr Agents (`started_by_jarvis` false) have a separate expanded **Manually started** section (web: **Started in terminal**, #3057), before **Unassigned** (web: **Not linked to a to-do**; web #2608, iOS / Mac #2917: without spawn relationships or linked space no automatic rule recognizes them; link through **Link to To-do…** in row menu; spawned herdr Agents whose parent is not linked cannot be distinguished on any client and count here). Remaining unlinked Agents go in final **Unassigned** section (collapsed by default when to-dos exist, expanded if any Needs you). Web remembers each browser's grouping, keeping whatever was chosen (default By To-do if never chosen, #2812; By Machine before, #1990). When list contains multiple host types, non-herdr rows label their host.
- Web **By To-do** is a three-level tree (#2526): project › to-do › Agent. Project comes from linked Agents' repository (same `repoOf` as By Repository, merged across computers; choose repository with most Agents if several); spawned Agents (`source = "spawned"`, `via` points to another Agent in same to-do) nest beneath parent, staying top-level if parent absent or cycle. Each node has state "Needs you / Working / Done", highest priority below it (Needs you > Working > Done, interrupted counts Needs you). Completed to-dos and done branches with last activity over 1 day ago default collapsed; Manually started/Unassigned last, grouped by project inside. Unfinished to-dos on left of To-dos page also group by project, no-Agent items last. Android By To-do also uses project › to-do › Agent (#3156), without spawned indentation/one-day folding yet.
- Source label before every Agent title (#2526): botmux **Lark Group**, tapping opens a thread link in Lark when present; herdr **Terminal**; other hosts unlabelled. Quick filters add Lark Group/Terminal for just that type. iOS / Mac have labels/filters too; Android too (filters #3154, labels #3132, labels under time, #3043).
- Other grouping modes add to-do-title labels to linked Agents.
- By-project headers show linked to-do labels opening details; menu **Link to To-do…**, **Move to Another To-do…**, **Remove from To-do**; picker selects unfinished to-do or creates then links. Agent groups outside herdr workspaces (no workspace id) have no labels or these menu items.
- Hide whole-project entry before list loads or when nonempty list has no `spaces` arrays; successful empty list shows it. Preserve prior support state during refresh/ordinary error; reassess after successful refresh/reconnect. Hide entry and ask for hub upgrade on whole-project API route-not-found 404; other 404s are ordinary errors. Old hub responses/local caches still decode. Picker resets input whenever opened or target project changes; session's own `todo` marker is authoritative.
- Agent quick menu (web, iOS / Mac, Android): unlinked **Link to To-do…**; linked **Move to Another To-do…** and **Unlink**. Relink directly to new to-do; hub releases old association simultaneously (#2348).
- Chat top-right more menu and session-info popup show "To-do: X", tap to open; web also shows it below top title with **Change To-do…** (#1836).

### 5. Siri and Shortcuts (iOS / macOS)
- **New To-do** (`AddTodoIntent`): say "Create a to-do with Persona" or use in Shortcuts / Action button; title required, notes optional, writes directly in background to the App's currently selected hub group, without opening App. See `docs/siri.md`.

## Design trade-offs and reasons

1. **Not another Lark Tasks or Reminders**: Persona to-dos drive and track Agent workflows, focusing on which Agents share one task and their progress, not complex subtasks, deadlines, priority trees, calendar reminders or multi-party collaboration.
2. **No stored state**: overall state derives entirely from linked Agents' live runtime states (questioning, working, finished unread, etc.). Agent state changes frequently; derivation avoids dual-write consistency problems and large synchronization overhead.
3. **One active to-do**: `UNIQUE (machine_id, session_id) WHERE released_at IS NULL` ensures each Agent serves only one current to-do at a time. Assigning other work automatically marks the previous task "Previously involved", keeping context and execution goal clear.
4. **Automatic completion only for chat-dispatched tasks, judged by model**: these have explicit requirements (dispatch prompt) and deliverables (Agent's final reply), so requiring individual user completion taps is a burden; manual to-dos often represent the user's own plans, whose completion hub cannot know. Stopped is not finished, so do not close by state alone; judgment model decides, leaving open when uncertain. Wrong auto-close (user thinks finished) costs much more than missed close (one extra tap), hence reversible, reasoned, and not reclosed by the same work after reopening.

## Known limitations

1. **Direct renames inside herdr**: renaming workspace in herdr terminal rather than hub API acts as a new project; previously linked Agents remain, but later Agents in that workspace will not auto-link.
2. **Marker overwrite across multi-hub groups**: a computer's persistent node serves only one long-lived connection at a time. If independent hubs in different linked-hub groups connect alternately to one node, later hub's expected markers overwrite earlier hub's.
3. **Empty-workspace rename does not migrate**: when renaming through hub with no active sessions, hub cannot resolve old workspace name; `todo_spaces` does not automatically migrate, like pin/mute.
4. **`/clear` recreates session**: Agent `/clear` creates a new session uuid underneath; new session does not auto-link to old to-do, and original session row stays listed.
5. **Assistant disconnected**: to-do APIs and create/send with `todo_id` return 503; workspace renames through hub do not migrate and answered cards are not recorded as decisions ("Who owns what").
