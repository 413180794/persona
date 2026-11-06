# Lark bot: chatting with Persona in Lark

When the user DMs the bot of a custom Lark app, they are talking to Persona: messages go into the main chat in the **Persona** tab (or, via commands, another chat or
Claude Code / Codex on one of the computers), and the turn's answer is sent back to Lark as the bot; whenever Persona or an Agent needs the user to answer or approve something, a card is sent in Lark,
and one tap answers it; pushes and answers from the main chat elsewhere can also be copied to Lark. Each item has its own switch, all off by default (requested 2026-11-02: "do P1/2/3, all optional", issue #322).

It is the channel `feishu` (`docs/channels.md`): tracking the turn goes through the shared pipeline (`assistant/channels_runs.go`, rules in `server/internal/channel`, the same as WeChat);
this page covers the Lark side (progress card, answer card, reactions, question cards, commands).

**Who owns what** (since 2026-11-04, `docs/channels.md` ("Who owns what"); since 2026-11-05 the transport also lives in the manager, `server/internal/extmgr`, `docs/extensions.md` ("Who owns what"); the transport parts below that say "hub" are done by the manager): the transport is only the app credentials and feature switches (`bots.feishu`), the Open Platform long connection, pairing, accepting only the owner and only in DMs, sending / replying to / editing messages and cards,
reactions, reading messages and downloading resources; the owner's messages and card taps are handed to the assistant (`feishu.message` and `feishu.card_action` on the bus). Which chat a message goes to, commands, tracking the turn, progress and answer cards,
question cards and button answers, pushes and sync belong to the assistant (`personad assistant`), and so does the chat state `bots.feishu.state`; whatever it needs to send or edit, it asks the hub to do via `app.call feishu.*`.
When the assistant isn't connected, the owner's message first waits up to 60 seconds for it (the hub just started, the assistant is restarting); only if it doesn't come does the bot reply "Couldn't send it: Can’t reach the assistant right now. Try again later"; card taps get the toast "Can’t reach the assistant right now. Try again later";
pairing, binding and the settings page here keep working (see `docs/channels.md` ("When the assistant is away")). When a card tap answers a question / approval, the hub hands the assistant a one-time "owner action" that can only answer that one card;
the assistant can't approve anything on its own (`docs/channels.md` ("Who owns what", "No answering approvals on the user's behalf")). After re-pairing (including while the assistant was away), the chat starts over: it goes back to the main chat, and thread bindings and pending answers are cleared.

Code: `server/internal/feishubot` (long connection, REST, events and message content, cards), `server/internal/hub/bots*.go` (pairing, routing, transport: events on the long connection are handed to the assistant; `assistant_channels.go` is the `feishu.*` the hub gives the assistant),
`server/internal/assistant/feishu*.go` (targets, commands, sending into chats, cards, pushes, sync: Lark's side of the shared pipeline);
prompt `server/internal/prompts/bot/message.md`; the App page `apple/Persona/Views/Settings/FeishuBotView.swift` (**Settings › Connectors › Lark Bot**).

## How it differs from Lark `/agent`

| | Lark bot (this page) | Lark `/agent` (`docs/lark-agent.md`) |
| --- | --- | --- |
| Trigger | DM the bot | **You** send `/agent …` in any chat |
| Identity | The bot itself (never taken as you speaking) | Replies in your name (ends with "> 🤖 AI 代答，仅供参考" (AI reply, for reference only); the prompt forbids making promises for you) |
| Where it goes | The main chat (or the chat / Agent switched to), continuing the earlier conversation | A new headless chat per request / thread; the hub first collects that chat's context |
| Depends on | A custom app on the Lark Open Platform (may need admin review) | The Lark gateway (no approval needed) |
| Interaction | Card buttons answer questions and approvals directly | None |

The two complement each other and have separate switches.

## Why the Open Platform long connection

- The hub doesn't need a public address: `POST /callback/ws/endpoint` returns a WebSocket URL, the hub connects out, and both events and card callbacks come over that connection (Open Platform's "receive events over a long connection").
  It runs on intranet machines, NAS boxes and home machines alike.
- The official Go SDK (`oapi-sdk-go/v3`) is 4.8 MB in total and also pulls in gogo/protobuf and gorilla/websocket; the protocol itself is tiny (one protobuf `Frame`),
  so `feishubot/frame.go` hand-writes the encoding, checked against bytes the SDK generates in `TestFrameMatchesTheSDK`. The WebSocket is the repo's existing coder/websocket.
- Protocol essentials (following the SDK's behavior): the client sends ping control frames every `ClientConfig.PingInterval` (2 minutes by default), and the pong may carry new config; a data frame's
  header has `type` (event / card), `message_id`, `sum` / `seq` (fragments of large messages, reassembled before processing); every data frame must be answered with the same frame, its payload replaced with
  `{"code":200,"data":<base64 JSON>}`; for card callbacks the data is the toast / new card to show, and if Lark gets no answer **within 3 seconds** it retries. Receiving nothing for two ping intervals means the connection is dead, and it reconnects.
- The same event may be redelivered: messages are processed only once per message_id (`seen` in `bots.feishu.state`, keeping the latest 300).

## Pairing and permissions

- App credentials: enter the App ID / App Secret in the App (it tries `GET /open-apis/bot/v3/info` once first and saves only if that works); the Secret is AES-GCM encrypted with `secret.key` and stored in settings
  `bots.feishu` (the same master key as launcher keys), and no endpoint ever returns the plaintext. If you'd rather not go through the App, write `bots.feishu` in hub.json (0600); what's saved from the App takes precedence.
- **Only the one paired person, only in the bot's DM**: the main chat can use all the data (Lark, mail, every computer, the phone's calendar and location), and anyone in a group can @ it, so group messages are never handled;
  anyone else who DMs it gets a single reply "This bot only works for its owner." (at most once per person per 10 minutes). Card buttons also only accept the owner's open_id.
- Pairing: tap **Pair** in the App to get a 6-digit code (valid for 10 minutes) and send it to the bot in Lark; whoever sends it becomes the owner (its open_id and the DM's chat_id are recorded).
  When not paired, DMing it returns instructions on how to pair; 5 wrong guesses void the code, and you have to get a new one. Changing the App ID means pairing again (it's a different bot).
- Names: reading contacts needs `contact:user.base:readonly`; without that permission the owner is called by hub.json's `assistant.owner`, and others "someone".

## What it can do (`features`, one switch each in the App)

| Switch | What it does |
| --- | --- |
| `main` Chat with Persona | Messages go into the main chat; `/new` starts a new chat, `/list` picks a recent chat, `/main` goes back to the main chat |
| `cards` Questions and approvals | Sends a card when a chat Lark is driving (the current target, one waiting for an answer, one bound to a thread within 24 hours) needs an answer / approval |
| `all_cards` Questions from all Agents | Sends cards for the questions and approvals of every Agent on every computer (turning it on also turns on `cards`) |
| `pushes` Also send pushes to Lark | Sends a copy alongside each `notify_user` push to the phone; answers in a scheduled task's own chat are sent too. Both are laid out as structured cards just like the bot's own answers, sent in full (up to 5 cards, tables and link buttons laid out as usual): the header of a push's first card is "🔔 title", the body's own status tag (请验收 / 无变动 / 有变动: please review / no change / changed) and color stay on the header, the body title is the bold first line when it differs from the push title (#4150), and "Open on web" is on the last card (#4003, #3553) |
| `sync` Sync the main chat | When a main-chat turn that didn't start from Lark (asked in the App, sent into the main chat by a scheduled task) ends, the answer is sent to Lark (laid out as structured cards like the bot's own answers, sent in full, up to 5 cards; source and "Open on web" on the last card, #3553; when the first card hits a Lark error it is tried again after 30 seconds, up to 3 more times, #4960) |
| `reports` Task progress | When tasks dispatched from the main chat finish, wait for you, get interrupted, fail to start, or are automatically marked done, several are combined into one card sent to the DM (see "Task progress and review requests" below); on reaching "deployed" it sends "请验收" (please review) |
| `agents` Drive Agents | `/agents` lists the Agents on every computer (Needs you / Working / Unread / used in the last two days); after tapping "Chat here", messages go to it; `/start` starts a new one |

Defaults (when first turned on) are `main`, `cards` and `reports` (`reports` was added later: settings saved earlier don't have it, and it reads as on).

## How a message flows

1. `im.message.receive_v1` → de-dup → stop if it's not the owner / not a DM (up to here it's the hub; the owner's message is handed to the assistant, which de-dups again by message_id and stores it). Anything starting with `/` is first treated as a command (below); unknown commands are passed through as is (they may be the Agent's own, e.g. `/compact`).
2. Target: if it's a reply to a message the bot sent (or in its thread), it goes to the chat / Agent bound to that message (`threads`, kept for 30 days, up to 500 entries, so replying to a card from several days ago still goes back to the original place, #4779); otherwise it goes to the current target (`current`: `main` / a Persona chat / an Agent).
3. Content: text (rich text converted to Markdown, @ mentions turned back into names); images (up to 4) as image attachments; files, voice, video and extra images are downloaded and stored via the node's
   `file.upload.*` in that computer's temp directory, with the path written into the message (a file over 50 MB is skipped: the rest of the message still goes, and a line says which file was too large, #4824); quoted messages and merged-forward chat histories (read out one per line) go into `<jarvis-context>`; images and files in quoted messages are handed over by the same rules (up to 4 together with the message's own images; if they can't be read, only the quoted text is included, without blocking the send, #4728);
   messages to Persona also get a note "the answer will be sent back to Lark; lead with the conclusion, don't use notify_user" (prompt `bot/message.md`).
4. Sending: Persona chats go through `session.send` (the main chat follows the launcher in **Settings › Assistant** just like the App; the assistant sends via the hub's `session.send_once`), with `client_msg_id` `feishu-<message_id>`,
   sharing the `sent_messages` de-dup with the App: redelivered events aren't sent twice. If there's no main chat yet, a new one is started and set as the main chat. The message gets an "OnIt" reaction.
5. Tracking the answer (the shared pipeline's run, stored in `channels.state`, so waiting resumes after the assistant or hub restarts; old versions' data in `bots.feishu.state` is moved over on first read): the assistant follows the chat the way the App does when it opens it (`session.follow`; the hub forwards new items and text being written).
   - Progress: about 1 second after the message is sent, a **progress card** is posted as a reply under the owner's message, without waiting to see the chat working: while the message hasn't shown up in the chat yet it says "Getting ready…"; while the chat is busy with another turn (this one queued behind it) it says "Queued…"; once the message is found it says "working", with the summary "Working · 3m 12s so far", and steps are counted only from this message on (the previous turn's steps aren't listed).
     The card shows, in order: "Now: Run `go test ./...` (40s so far)" (the latest tool call without a result yet and how long it has run; when several are running it adds "and N more"; once all have results this line is dropped);
     the last 5 steps (earlier ones as "…N earlier steps"); when subtasks (Task / Agent) were used, a "Subtasks" section, one line each with description, type and "running" / "done", up to 6, more as "and N more";
     small gray text "N tool calls · read X files · changed Y files" (counting distinct files, zeros omitted, "1000+" from 1000 up); and finally the text being written. These are recorded as new items arrive (`chanLive`), not rescanned, with a cap on entries; the turn after a background task reports back starts from zero.
     With new steps it's edited at most once every 3 seconds, and even without new steps it's edited every 3 seconds, so the elapsed time keeps moving and it never looks stuck. The answer is written into this card, and the process goes into a collapsible panel "Called tools N times". Before a background task reports back (after one answer, while waiting on the background), no new card is sent; it's sent once that turn starts working.
     When editing, if the content is the same as last sent, nothing is sent; when Lark says edits are too frequent (230020, 99991400, HTTP 429), it pauses 5 seconds first, then 10 and 20 seconds on repeats, up to 30 seconds, and then sends the latest state; the answer isn't affected;
     if the card was recalled or deleted (230011, 230110), it forgets the card, the next refresh posts a new one, and the answer goes into the new card.
   - New turns no longer use **agent messages** (the Open Platform's `message_cot`, since 2026-11-06): they can't show elapsed time or "what it's doing now", and they're only created once thinking or a tool call appears, so nothing shows during a text-only stretch.
     A turn that already created an agent message before that (started before an upgrade or restart) keeps appending to it and finishes it (`RUN_FINISHED`), and the answer is sent as a separate card. The hub's `feishu.cot_create` / `cot_append` / `cot_complete` call the API, and events are in AG-UI format (`feishubot/cot.go`).
      When an already-open agent message errors (#2807): rate limits, a busy service or no response from Lark (the hub records error code -1) only mean this attempt wasn't sent, and the next one continues; a batch that may already have arrived isn't resent; if the final append fails, the answer is still sent and the agent message is closed with `cot_complete` (its own 15-second deadline, up to 3 tries) rather than left spinning; tool calls already shown but without results are stored in the run's `cot_calls`, so they're still closed after the assistant restarts.
   - When the turn ends (not working, no card waiting for you), it takes the last stretch of answer **after this message** (the text after the last tool call) and writes it into the progress card
     (for old turns that still have an agent message: that gets closed, and the answer is sent as a separate card), and the reaction changes to "DONE". The card has no buttons.
     If the turn stopped without saying anything (model service error, interrupted): the card says "This turn stopped without a text answer: <reason>"; the only reasons recognized are "API 错误：…" (API error) and "已中断" (interrupted) at the end of the turn (compaction notices after them are skipped; goals, background tasks, Codex events and command output don't count as reasons), squeezed into one line of at most 300 characters; for Codex it takes the session's `turn_error` (`channel.Stopped`, #3340); without a reason it's still "(No text answer this turn)".
      If the end of the turn it reads isn't the assistant's text (the node reports "ended" slightly before the record finishes writing the last stretch), or the answer is empty / only a placeholder like "No response requested.": it doesn't answer yet and rereads within 10 seconds; if there's still nothing, it sends Persona chats one message "write out this turn's answer" (only once; not for Agents) and waits for that turn's answer; if there's still none, it sends "No answer came out of this turn. Why: …" and logs it (#4588).
     The card's last line says how it ended: "Done · took X · N tool calls"; cards that didn't finish normally say the outcome in the title rather than staying at "working / Getting ready":
     interrupted (`/stop`, the App, Esc on the computer) is "Stopped", a model API error is "Failed", not done after 4 hours (2 hours when waiting on the background) is "Timed out, see the app",
     a chat that was closed or deleted is "Conversation closed", and a computer going offline (including mid-task) is "Computer offline"; an ending with nothing to say also turns the progress card into this line. When merged into the next message, the whole card becomes "Answering together with the next message".
     If editing the card fails (the card was recalled), the answer is sent as a new message.
   - The chat is busy when the message is sent: it queues behind the current turn. When that turn ends the message isn't in the transcript yet, and that turn's ending must not be taken as the answer, so it uses the message's `client_msg_id` (`feishu-<message_id>`; for old nodes without it, a stretch of the message's opening text, `probe`)
     to find its position in the transcript and takes only the answer after it; only if it still can't be found after 10 minutes does it fall back to the last words.
   - Another message to the same chat before it finished answering: the two get one combined answer (both messages switch to "DONE"): both go into the chat, the answer comes only when the last turn ends, and each message's own answer is joined in chat order (`docs/channels.md` ("Tracking the turn")).
     The assistant processes the owner's messages one at a time in the order received (adding the reaction, downloading images, reading quotes, sending into the chat), and the next one waits until the previous was sent in: previously they ran in parallel, and a slow one (slow Lark API, images to download) could be overtaken by the next one into the chat, so the combined answer got attached to the earlier message.
     If the previous one is stuck (images downloading slowly, `/new` slow to start a new chat), it waits at most 20 seconds, then the next one stops waiting for it. Messages that weren't processed (duplicate deliveries, commands, not allowed to send) also let the next one proceed.
     The order is set from the long connection on: events on the long connection are handed to the manager in the order received (card buttons aren't queued), and the manager hands them to the assistant one at a time in order (when the assistant is away the previous one waits for it, and the next waits behind it).
     A message that arrives while a turn's answer is just about to be sent (`answering`) is no longer merged into that turn, or the same answer would be sent twice.
   - There are still background tasks (`run_in_background`, Monitor, sub-Agents): it answers once, saying "N background tasks are still running", then answers again when the turn in which they report back ends (waiting up to 2 more hours).
   - If it's not done after 4 hours, it stops waiting and says to check in the App.
   - The answer is laid out as cards by rules (no model call, `feishubot/answer.go`, #3148): a leading H1 becomes the card title; "请验收 / 无变动 / 有变动" (please review / no change / changed) in the title or at the start of a body line becomes a tag next to the title and sets the color (orange / gray / blue; blue when there's no status);
     H2s and short lines like "一、…" ("1. …" in Chinese numbering) start sections, with dividers between sections; Markdown tables become native tables (10 rows per page, up to 100 rows); "备注：" / "注：" (note:) lines are small gray text; web links in the answer become buttons at the bottom of the last card (up to 4). Code blocks are kept verbatim, with no headings or tables recognized inside.
     Long answers are split into cards at 6000 characters, 80 elements or 5 tables, never breaking inside a code block; up to 5 cards, beyond that it says to see the rest in the App. If one in the middle can't be sent (rate-limit retries exhausted, rejected as both card and rich text), the rest are still sent, and finally a line is added under the owner's message: "N parts of this answer couldn't be sent to Lark (parts x); see the whole answer in the app." Already-sent ones aren't resent; the same goes for `notify_user` pushes, main-chat sync and scheduled-task answers (these are sent in the DM, #4766). If Lark rejects the card (the Markdown has something it doesn't accept), it's sent as rich text instead.
     When a stretch of text or a "备注：" line has lots of escaping (`<` takes 5 bytes in a card) and 6000 characters exceed 24000 bytes, it's cut smaller by bytes, so even a single stretch never pushes a card past 30 KB.
6. Turns started from Lark and answered in Lark no longer push a "done" notification to the phone (`botAnswered`), and `sync` doesn't send them again.

## Commands

`/help`, `/main`, `/new [first message]`, `/list`, `/agents [keyword]`, `/start [what it should do]` (a form to pick a machine and fill in a directory), `/stop` (Chinese aliases `/停止`, `/停`; interrupts the turn: sent in a message's thread / as a reply to it, it interrupts the chat or Agent bound to that message, otherwise the current target), `/status` (picks the target the same way from the reply / thread binding: who messages go to now, how many answers are pending).

A newline, Tab or full-width space after the command name all count as separators, and a leading full-width slash `／` counts as `/`; content after `/new` is passed through as is, multiple lines included (#3897).

## Cards

- Cards use JSON 2.0 (`schema: "2.0"`): Markdown supports headings, tables and code blocks, so model answers display as written (1.0 doesn't support headings and tables). Needs Lark 7.20 or later; old clients show only the title and an upgrade notice.
  2.0 pitfalls: there's no `action` module, so lay buttons out horizontally with `column_set` (`flex_mode: flow` wraps automatically on narrow screens); `note` is gone, use `div` + `text_size: notation` + `text_color: grey`;
  button callbacks go in `behaviors: [{type: callback, value}]`; a submit button in a form needs `form_action_type: submit` and a `name` unique within the card; **one unknown extra field rejects the whole card**;
  a new card returned from a callback must also be 2.0.
- `<` in text outside code is turned into `&#60;` (cards swallow things like `<at>` and `<font>` as their own tags); data such as names and titles goes through `EscapeMarkdown`.
  The exception is autolinks the model writes, `<https://…>` and `<mailto:…>` (CommonMark): cards don't recognize them, and escaped they become unclickable text, so they're first rewritten as `[url](url)` (#4727). `(` `)` in the URL and `[` `]` after the hostname are percent-encoded, and `_` `*` `[` `]` in the link text become entities; other schemes (`javascript:` etc.), `\<` and anything in code stay as they are. Test: `go test ./internal/feishubot -run CardMarkdownAutolinks`.
- `persona-card` (chart cards, #886) in assistant answers can't be drawn by Lark, so they're written out as numbers before sending (`PersonaCards` in `feishubot/personacard.go`; it's the first step of `CardMarkdown`, so all text sent through `CardMarkdown` gets this, not just the health space; the bot's answer is converted as a whole before being cut into cards (`answerCards` in `assistant/feishu_answer.go`), otherwise a chart longer than one card would be cut off and unreadable, leaving an empty card):
  `stat` becomes one line (`Steps today: **8,432 steps** · +12% vs yesterday`); `line` / `bar` become the title plus a ▁…█ sparkline, with the numbers on the next line one by one (at most the latest 31) and the unit written once; `stacked_bar` becomes a small two-column table with the total in the header, and beyond 31 rows the rest are combined into one "Other" row.
  Numbers are formatted the same as on every client (`wire.Number`), and text the model wrote always goes through `EscapeMarkdown`; cards that no client can draw keep only the title; `persona-view`, other code blocks and example cards inside code blocks are left alone. In chat list summaries, cards show only their title. Tests: `go test ./internal/feishubot -run PersonaCards`, `go test ./internal/assistant -run TestAnswerCardsWriteChartsAsNumbersBeforeCutting`.
- Link buttons only use http(s) (the docs only mention http(s) and AppLink; `jarvis://` may get the whole card rejected); "Open on web" opens the matching session page in the web version the hub hosts.
- The options on interactive cards match the App's cards:
  - Questions: a single question with at most 4 options and single choice is a row of buttons + a "Write your own answer" input; anything else is a form (one dropdown per question, a multi-select dropdown for multiple choice, plus an input), submitted at once.
  - Plans: the full text (up to 5000 characters), "Approve & run" (bypassPermissions) / "Auto-accept edits" (acceptEdits) / write feedback and "Send back".
  - Permissions: tool, reason, command or arguments; "Allow" / "Deny", plus "Always allow" for this session when there is a session. **Anything that must be filled in on the phone (passwords, authorization codes, `fields`) can't be approved in Lark**; only "Open on web" is offered.
  - An MCP server needs your authorization or a form (`elicitation`): the card says what it wants to do (`message`); link authorization gets an "Authorize" button (only http(s) links without username / password, opened as is; the card has a separate line "Opens: <site>", and after authorizing you go back to the App and confirm); forms only list the names of the fields to fill, which you fill in the App (values may be passwords, which Lark doesn't take), #4234.
  - Terminal dialogs: when the options can be read, one button per option; when not, Enter / Esc / 1 / 2 and an input; ones that need a password (`secret`) can't be typed in Lark.
- Wherever it's answered (Lark, the App, the watch, a notification button, the computer), the Lark card changes to "✓ Answered: …"; recalled / expired ones change to "Ended". Pressing a terminal or Codex card has to wait for the computer; after 2 seconds it first replies "Working on it…", then edits the card when done.

## Configuration and endpoints

```json
"bots": { "feishu": { "app_id": "cli_…", "app_secret": "…", "domain": "feishu" } }
```

`domain`: `feishu` (default), `lark`, or the Open Platform address of a private deployment. Switches, features and pairing are configured in the App and stored in settings `bots.feishu` (read / written by the hub); chat state (current target, thread bindings, processed messages) is stored in `bots.feishu.state` (read / written by the assistant), and pending answers in `channels.state`.

These routes belong to the hub; `target` and `agents` in the response are queried from the assistant on demand (`feishu.view`). When the assistant isn't connected, these two fields are absent; everything else works as usual.

| Method | Path | Description |
| --- | --- | --- |
| GET | `/api/v1/bots/feishu` | `{enabled, app_id, domain, has_secret, from_config, features{main,cards,all_cards,pushes,sync,agents}, connected, error, last_event_at, bot_name, owner{name,paired_at}, pairing{code,expires_at}, target, agents[{title,machine_id,session_id}]}` |
| PUT | `/api/v1/bots/feishu` | Changes only supplied fields: `enabled`, `app_id` + `app_secret` (validated first) + `domain`, `features` (replaced as a whole); `app_id: ""` clears the credentials saved in the App (falls back to hub.json) |
| POST | `/api/v1/bots/feishu/pair` | Generate a pairing code |
| DELETE | `/api/v1/bots/feishu/owner` | Unpair (also clears chat state) |
| POST | `/api/v1/bots/feishu/test` | Send the owner a test message |

## What to do on the Open Platform

1. Create an enterprise custom app and enable its **Bot** capability.
2. Permissions: `im:message` (read / write direct messages), `im:message:send_as_bot`, `im:message.p2p_msg:readonly` (messages sent to the bot in a DM), `im:resource` (download images and files in messages);
   optional `contact:user.base:readonly` (names).
3. Events and callbacks: select **Receive events through a long connection** as the subscription method; subscribe to **Receive message v2.0** (`im.message.receive_v1`), optionally **User enters a chat with the bot** and **Message recalled** (`im.message.recalled_v1`: if the owner recalls a message before it is sent into the chat, it isn't sent, #4880; if already accepted and being answered, a turn answering only that message stops like /stop, changes its progress card to ‘Recalled, this turn stopped’, and sends no answer; if merged with other messages, only the recalled message is removed; already completed answers remain unchanged, #5248); subscribe to **Card interaction callback** (`card.action.trigger`).
4. Publish a version (enterprises usually require admin review), then enter the App ID / App Secret in the App, enable it and pair.

## Not implemented yet / limitations

- Recall: only handles messages not yet sent into the chat (queued, attachments downloading); once sent into the chat, an ongoing turn finishes answering as usual (whether to interrupt is the owner's decision, #4880). The bot can't obtain the new content of an edited message (the Open Platform provides no edit event to bots).
- Agent messages show tool-call titles and outputs, not arguments (tested), so describe the action in the title; progress cards show only steps and text being written.
- Each step is one line, "verb + object" (`stepOf` in `feishu.go`): commands use their description when available ("Run · Run server tests"), background commands say "Run in background"; file reads include the line range, while edits / creates / writes are separate; searches include their scope ("in src (*.go)"); subtasks include their type; to-dos show the item currently being worked on; Codex calls are already mapped to the same tools in the transcript (command → Bash, file change → Edit, create / delete separate), so they use the same wording; Persona's own MCP tools (calendar, contacts, reminders, mail, knowledge base) have their own verbs, while other tools show the tool name and the first useful argument.
- Agents (Claude Code / Codex in herdr) have no "text being written", so progress cards show only steps.
- Card callback tokens (two further updates allowed within 30 minutes) aren't used: after an answer, cards are edited through the update-message endpoint.
- One hub connects one Lark bot and one owner.

## Verification

Server: `go test ./internal/feishubot ./internal/hub ./internal/assistant -run 'Bot|Frame|Conn|TurnAnswer|ResolutionText|Feishu'` (frames byte-for-byte matching the SDK, fragmentation, ping,
card callback responses, pairing / owner-only / ignoring group messages, sending into the main chat only once, taking the answer after this message, answering question cards, commands, replying "temporarily unreachable" while the assistant is away and recovering;
hub tests connect to a real assistant over the real bus); prompt goldens `bot_message.*`.
End to end (2026-11-04): isolated hub + assistant + node + fake claude, with the hub connected to a fake Open Platform (HTTP plus a long-connection WebSocket, protobuf frames); pairing, sending messages, answer cards and reactions, replying "temporarily unreachable" when the assistant is killed, normal operation after it returns and after the hub restarts.
App: `FeishuBotModelTests` (loading, submitting all features together, saving credentials and errors); screenshots use a mock hub (`MOCK_BOT=pairing` / `off` for pairing and unconfigured states) + `-openConnectorBot YES`.

## Task progress and review requests (#2768)

The owner only watches Lark, so the bot proactively sends updates on tasks (to-dos) dispatched from the main chat to the DM, without lark-cli or individual approvals on the phone (requested and authorized 2026-11-05). Sends only when something changes; no change, no message. Code: `server/internal/assistant/reports.go`, `acceptance.go`.

- **Coalescing**: changes within a minute (a turn finished, waiting for you, interrupted, failed to start, automatically marked done) are combined into one card; only the latest state of each task is kept. Check again before sending: don't report questions already answered or to-dos deleted; if a question card is already in the chat (`cards` / `all_cards`), don't repeat "waiting for you"; quiet tasks aren't reported when `sync` is on (the main chat's answer has already been synced), except "automatically marked done", which is still reported (the main chat doesn't mention that).
- **Card**: decisions needed go first (each followed by the usual question card so options can be tapped directly; cards already sent aren't duplicated), then a section per task: status, title, computer, stage bar
  Plan → Development → Review → Merged → Deployed → Verified (✅ completed, 🔵 current, ⚪ upcoming), the Agent's last words (first 300 characters), and buttons **Task page**, **PR**, **Agent chat** (web links; omitted if hub.json has no `public_url`). No bare IDs.
- **Stages** (`Todo.stage`, `Todo.pr_url`, stored in settings `assistant.todo_stages`, only advance): starting work is **Development**; a PR link in the reply means **Review**, also mentioning a merge means **Merged**; saying deployed / live means **Deployed** (doesn't count when preceded in the same clause by "还没" (not yet), "未" (not), "别" (don't), "无需" (no need), "等你确认后再" (after you confirm), "not yet", "will be", etc., or followed by "吗" (a Chinese question particle) or "yet"); saying tested successfully after deployment means **Verified**; tapping **Pass** on a review request also means **Verified**. The App's to-do detail draws the same stage bar from these two fields (#2776).
- **Review request**: as soon as a task reaches **Deployed**, send a three-line card: what changed (one sentence), where to look (a link straight to the page, only one present in a reply, the task page or Agent chat; never one invented by the model), how to check (what to tap, what to expect). A small model (purpose `inbox.judge`, prompts `prompts/assistant/acceptance*.md`) reads the task and replies to write these three lines; if the model isn't available, use the task title, task page and a generic check instruction.
  - Tap **Pass** or reply to the card with "通过" (pass; "没问题" (no problem), "OK", "LGTM" also count): mark the to-do **Verified**, and change the card to **Accepted**.
  - Any other reply: record it in the to-do's decision section (**Review feedback**) and send it to the original Agent to fix; deploying again after the fix triggers another review request.
  - Each task gets one card (stored in settings `assistant.acceptance`). If not accepted the next day, mention it again in the morning brief ("waiting for user acceptance" in the snapshot, item 4 of the morning prompt), and send a **Not yet accepted** card at the morning-brief time (the time of scheduled task `morning` when enabled, 9:00 if disabled or deleted, in the user's time zone), once a day.
- The switch is `reports`; when the bot is disabled or unpaired, nothing is sent and changes during that time are discarded (later cards describe the state at that later time).
