# Search and AI Q&A

Paths are relative to `/agent/v1`.

**Encode every query.** Use `--url-query 'q=中文'` on GET, or JSON bodies on POST. A raw non-ASCII `q` returns 400 "Client Error", and an unencoded `#` truncates the query.

## Contents

1. Quick search
2. Filtered, paged search (`POST /search/page`)
3. @me history, platform search, related chats
4. Enterprise AI Q&A and My AI
5. Pitfalls

## 1. Quick search

消息搜索（`GET /search`、`GET /search/in-chat`、`POST /search/page` 的 `tab=messages`、`POST /search/mention-history`）命中的加密消息按 `messaging.md` 第 8 节解密或给占位。只解密已经命中的消息，不写入明文索引，因此不能按加密正文的明文关键词搜索。联系人、文档等非消息搜索不受该门禁影响。

| Call | Returns | Notes |
| --- | --- | --- |
| `GET /search --url-query q=… --url-query limit=30` | Message hits: `{messageId,feedId,conversationName,senderName,snippet,position?,source:"local"\|"remote"}` plus `upstream{attempted,failed}` | Searches the local full-text index first, ranks by relevance, and asks Lark only when local results fall short of `limit` (≤100). No paging, no filters, **no timestamps**. Use section 2 for anything more |
| `GET /search/in-chat?feedId=F&q=&limit=` | Same shape, inside one chat | Missing `feedId` returns **500** |
| `GET /search/people?q=&limit≤50` | `items[{id,name,subtitle,avatarUrl,type:"user"}]` | **Fields are `id` and `name`**, not userId and displayName. Use `subtitle` (department) to tell people apart, and skip "飞书个人用户" personal accounts. MCP `search_people` omits `avatarUrl` and `type` unless `raw=true` |
| `GET /search/documents?q=&limit≤50` | `items[{id,title,subtitle (owner),url}]` | Metadata only. Read the body via `docs.md` |
| `GET /search/home`, `GET /search/tabs` | Frequent contacts and history; tab config |  |
| `GET /search/coverage` | Local cache coverage | Counts conversations with any stored message and stored messages, plus backfill progress; it does not prove a chat or time window is complete |

Check `coverage` before answering "nothing found". A conversation with cached messages can still lack older messages or thread pages. Background backfill is bounded (by default at most 2,000 message positions per conversation), and the current coverage response does not certify a requested date range. `POST /search/coverage/backfill` starts a history backfill; that is host work, so run it only when asked.

Message searches hydrate matching messages, senders and conversation names in batches. This changes neither result ordering nor the routes, fields, limits or pagination; use the returned metadata instead of issuing per-hit lookups merely to refresh names or snippets.

## 2. Filtered, paged search (`POST /search/page`)

Body: `{tab, query (1–500), limit? (1–50, default 20), cursor? (the nextCursor value; can be hundreds of KB, hence POST), …filters}`. The response is `{items, nextCursor?, total?, upstream, grammar?, traceId}`. `traceId` is the request ID for backend log correlation. The completion log records tab, result count, pagination and upstream status without logging the query or cursor. An invalid body returns 400 `INVALID_INPUT`.

`tab` and its filters (id arrays take ≤20 entries):

| tab | Filters | Item fields |
| --- | --- | --- |
| `messages` | `senderIds`, `chatIds`, `atUserIds`, `withUserIds`, `feedId` (one chat), `startTime`/`endTime` (**Unix seconds**), `attachmentTypes` (1 image, 2 video, 3 file, 4 link), `chatType` (`group`/`p2p`), `fromType` (`bot`/`user`) | Same as `/search`, plus optional `createTime` (ISO 8601) on upstream hits; still **no senderId**, so read `GET /messages/ID` when you need it. The local-index fallback omits `createTime` |
| `docs` | `ownerIds`, `chatIds`, `sharerIds`, `docTypes` (1/8 doc, 2 sheet, 3 bitable, 4 mindnote, 6/11 slides, 5 file, 9 folder), `startTime`/`endTime` (last opened), `sortType` (2 opened, 4 edited, 256 created), `folderTokens`, `spaceIds` | `{id,docType,title,ownerName,url,snippet?,createdAt?}`; snippet is supplied only when upstream returns a matching summary, with highlight markup removed; `createdAt` (ISO 8601) is present only when the upstream hit carries a valid create time |
| `groups` | `withUserIds`, `groupJoin` (`joined`/`notJoined`), `groupVisibility` (`public`/`private`), `sortType` (2 updated, 3 created, 4 members) | `{id (feedId),name,description,isMember,isPublic,memberCount}` |
| `contacts` | — | `{id,kind:"person",name,subtitle}` |
| `apps`, `helpdesk` | — | apps: `{id,name,url,larkUrl,botId}` |
| `email` | — | `{id (threadId),subject,from,receivedAt}`. The cursor is `offset:session` |
| `calendar` | — | `{id:"eventId:startAt",title,startAt,endAt}`. No paging |

Query grammar: `from:`, `in:` and `with:`. Each value is a name (the first match wins) or an id of 6 or more digits. `messages` accepts all three, `docs` accepts `from`/`in`, and `groups` accepts `with`. A name that does not resolve returns empty `items` with `grammar` explaining why.

`upstream.failed:true` means Lark failed and the results are incomplete; this is not an HTTP error. Only a first-page `messages` search with no filters falls back to the local index.

```bash
ol POST /search/page '{"tab":"messages","query":"上线","senderIds":["123"],"chatIds":["456"],"startTime":1760000000,"endTime":1760600000,"limit":50}'
```

### Codex MCP message paging

The bundled plugin's `search_messages` still uses the local-first quick GET search in section 1 (`limit` 1–100, no cursor). Use `search_message_page` for filtered, paged upstream message search. It accepts `accountId`, `query`, `limit`, `cursor` and only the `messages` filters above; the plugin fixes `tab=messages` and the HTTP path to `/search/page`. It reuses the shared backend input limits: query 1–500 trimmed characters, limit 1–50 (default 20), cursor at most 900000 characters, IDs 1–64 characters, ID arrays at most 20 entries, and `attachmentTypes` at most 5 integers in 1–5. Times are nonnegative integer Unix seconds.

For MCP `tools/call`, start with `{"name":"search_message_page","arguments":{"accountId":"4","query":"上线","senderIds":["123"],"limit":20}}`. The page comes back as a Markdown table of items (cells verbatim except line breaks become spaces and `|` is `\|`) followed by one JSON line with `nextCursor`, `hasMore`, `total`, `upstream` and `grammar`; `raw=true` returns the server JSON and `fields` (dot paths such as `items.messageId`) keeps only the named fields. Message items are a compact projection (`messageId`, `feedId`, `conversationName`, `position`, `createTime`, `senderName`, `snippet`, `source`, `type`); highlight offsets, duplicate `title` and extra sender name variants are omitted. If `nextCursor` exists, pass it back as `cursor` with the same account, query and filters. Do not decode, trim or synthesize cursors; stop when `nextCursor` is absent. Preserve `total`, `upstream` and `grammar` when reporting results. Upstream failures may be returned in a successful HTTP response and mean incomplete results. Pass `raw=true` to get the uncompacted items; the flag is not sent to the backend.

This dedicated POST is a read and remains allowed with `lark-connector_MCP_READ_ONLY=1`; generic POST and `reply_to_message` stay blocked. Search does not write search history or change Lark messages/read state, although the backend may hydrate its local index/cache. The backend route catalog remains unchanged.

### Codex MCP document paging

Use `search_document_page` for server-side document and Wiki body matches. Arguments are `accountId?`, `query` (1–500 trimmed characters), `limit?` (1–50, default 20), `cursor?` (opaque, up to 900000 characters), `ownerIds?`, `docTypes?`, `folderTokens?`, `spaceIds?`, `openedFrom?` and `openedUntil?`. Arrays take at most 20 entries. The plugin fixes `tab=docs` and calls the same `POST /search/page` route. It maps `openedFrom`/`openedUntil` to `startTime`/`endTime` as nonnegative integer Unix seconds; these are opened-time filters, not creation or modification dates. Reversed ranges fail locally with `INVALID_TIME_RANGE` before HTTP.

For example, MCP `tools/call` accepts `{"name":"search_document_page","arguments":{"accountId":"4","query":"项目进展","limit":20,"ownerIds":["123"]}}`. Preserve `nextCursor` and resend the same query, filters and account on subsequent pages; read relevant URLs with `read_document` before summarizing. Returned `snippet` is optional; do not synthesize a missing upstream excerpt. This tool remains available with `lark-connector_MCP_READ_ONLY=1` and does not write search history. Results are a Markdown table plus a JSON line with `nextCursor` and `hasMore`. `upstream.failed:true` becomes an MCP `UPSTREAM_SEARCH_FAILED` error (`recoverable:true`) carrying `traceId` when available, not a successful empty result; direct HTTP clients must still inspect `upstream` themselves.

## 3. @me history, platform search, related chats

- **@me history:**
  - `POST /search/mention-history {limit?, cursor?, startTime?, endTime?}` (the body may be `{}`) lists messages that @mentioned you, newest first.
  - `startTime`/`endTime` are optional nonnegative integer **Unix seconds**, the same units and validation as `/search/page` `tab=messages`. An invalid value or a reversed window (`startTime` > `endTime`) returns **400** `INVALID_INPUT` with `recoverable:true` and a `traceId`, like `/search/page`. Use them to fetch e.g. "everything that @mentioned me in the last 30 days" without paging all history. The read is always upstream-only (the @me filter cannot be applied to the local index), so an upstream failure returns empty `items` even on the first page.
  - The cursor is Lark's opaque pagination token and does **not** encode the window. When paging, resend the exact same `startTime`/`endTime` with every `cursor`; the server cannot verify they match the cursor's window.
  - Items have the message-search shape and include `createTime` (ISO 8601) when the upstream hit carries a valid time; otherwise the field is absent. `position` lets you open the message with `GET /conversations/FEED_ID/messages?aroundPosition=POSITION`.
  - It returns 409 `ACCOUNT_NOT_SYNCED` before the first sync.
- **Platform search** (minutes, tasks, Moments, example-cli, approvals, subscriptions, tech docs, oncall, PAWork):
  - `GET /search/open-tabs` lists the tabs as `tabs[{appId,label}]`.
  - `POST /search/open {appId, query, limit?, cursor?}` returns `items[{id,title,summary,appLink}]`.
- **Where a doc was shared:** `GET /search/related-chats?url=` returns `chats[{feedId,name,isP2p}]`.
- **Public groups:**
  - `GET /search/public-groups/FEED_ID` shows a public group's card.
  - ⚠ `POST /search/public-groups/FEED_ID/join` joins it, and the group sees the join. _(unverified)_
- **Search history:**
  - `POST /search/history {query,tab?}` records an entry.
  - ⚠ `DELETE /search/history` clears all of it; there is no per-entry delete.
- **Feedback:** `GET /search/feedback/reasons`, then `POST /search/feedback`. This sends feedback to Lark.

## 4. Enterprise AI Q&A and My AI

**Ask the enterprise knowledge AI:**

1. `POST /search/ai-answer {query (1–500)}` returns `{answering, topicId?, roundId?}`. If `answering` is false, the AI declined.
2. Every 2–3 s, poll `GET /ai-knowledge/rounds?ids=ROUND_ID` until `status` is `finished`, `stopped` or `error`.
3. Use `answerMarkdown` and `references[{title,url}]`. The `/events` stream also carries `ai-knowledge.updated` with reason `search-stream`.

**History** (last 30 days only):

1. `GET /ai-knowledge/topics?count≤50&offsetTimestamp=` lists topics. Each has `topicId` and `lastRoundPosition`.
2. `GET /ai-knowledge/topics/TOPIC/rounds?lastPosition=<lastRoundPosition>&count=` reads a topic. **`lastPosition` is required.**

**Other:**

- `GET /ai-knowledge/scope` shows the knowledge sources and `GET /ai-knowledge/unread` the unread count.
- `POST /ai-knowledge/answers/feedback {roundId,answerId,dislike}` rates an answer.
- `POST /ai-knowledge/topics/TOPIC/read {lastRoundPosition}` marks a topic read.
- ⚠ `DELETE /ai-knowledge/topics/TOPIC` deletes a topic.

**My AI:**

- `GET /my-ai` returns `{usable, feedId?, …}`. When `usable` is true, ask questions by messaging that `feedId`.
- The answer routes need `usable`: `GET /my-ai/answers?feedId=&ids=`, `POST /my-ai/answers/like {feedId,messageId,kind}`, `POST /my-ai/answers/regenerate {feedId,roundId}` (latest settled round only), and `GET /my-ai/scenes`.
- When `usable` is false, those routes return 403 `MY_AI_SCOPE`. Use the enterprise AI above instead.

## 5. Pitfalls

- `limit` on `/search` and `/search/in-chat` has no lower-bound check. Pass a positive integer.
- `/search/open` and `/search/history` return 500 on bad input (zod parse); `/search/page` and `/search/mention-history` return 400 `INVALID_INPUT` (`recoverable:true`, with `traceId`).
- Times are Unix **seconds** in `/search/page` and `/search/mention-history` filters. Other products use ms or ISO.
- People search may return personal (non-tenant) accounts with the same name. Never act on a person until the `subtitle` matches or the user confirms.
- OLK encrypted messages are matched by Lark's index on their cover text only. Agent search (`/search`, `/search/in-chat`, `/search/page`) never matches their decrypted words; there is no agent route for that, so do not look for one.

## Desktop search presentation

The global search dialog keeps the existing categories, filters, pagination, keyboard navigation and result actions. Its empty query view uses server search history and frequent entities; it does not synthesize contacts or counts. All-category results optionally show a read-only enterprise dictionary column using `GET /lingo/lookup?query=…&locale=…` and, for one additional meaning, `GET /lingo/card?id=…&query=…&locale=…`. Empty or failed dictionary responses omit the column without blocking search. These UI changes do not alter agent routes or their contracts.

While the owner's OLK key vault is unlocked, the All, Messages and Current chat categories also list encrypted messages whose decrypted words match, in their own section first, marked with a lock; opening one jumps to the message. The server decrypts the account's stored OLK messages into an in-memory index for this (owner-only `POST /api/olk/search`, query in the body), never on disk, and drops it when the vault locks, the account signs out or changes, or the server stops. A locked vault shows a hint to unlock. Filtered searches leave encrypted results out, and earlier history is not fetched for them. An "Encrypted only (local)" switch in the search box (on by default, remembered on the device) searches only these encrypted messages and sends nothing to Lark search or its history; in the regular mode the page warns that the keywords also go to Lark. Encrypted results page with their own "load more"; past the index budget only the newest encrypted messages are searched, and the page says so. The agent API, AI suggestions and search AI answers do not see these results.
