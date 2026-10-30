# Meetings and minutes (妙记)

Paths are relative to `/agent/v1`.

- ⚠ marks an action other people can see, or one that cannot be undone. Confirm it first.
- _(unverified)_ marks code that has never been run against the real Lark gateway. Read the result back afterwards.

## Ids

| Id | What it is | Where it comes from |
| --- | --- | --- |
| `historyId` | A meeting in your history | The list and the detail route |
| `meetingId` | A running meeting | `launch` and `cancel` |
| `meetingNumber` | The number in the join link `/j/<n>` | Join links |
| `token` | A minutes record. Matches `^(ob\|mm)[a-z0-9]{8,62}$` | Last path segment of a minutes url, or the minutes list |

## Meeting history

| Call | Notes |
| --- | --- |
| `GET /meetings?count=≤50&cursor=&filter=calls` | Default 20 per page. Preserve `nextCursor` as a 1–32 digit string with the same account and filter; omit it or use an empty cursor for the first page. Invalid non-empty cursors return 400 `INVALID_MEETING_CURSOR` without reading history. `filter=calls` returns 1:1 calls only. Items: `{historyId, meetingId, topic, kind, artifacts[], sortedAt}`, `hasMore` |
| `GET /meetings/HISTORY_ID` | Returns `meetingNumber`, `meetingUrl`, `participants[]`, `documents[{kind:"minutes"\|"recording"\|"follow"\|"notes",url}]`, `recordings[]`, `imRecord.status`, `status`. Missing meeting: 404 `MEETING_NOT_FOUND` |
| `GET /meetings/by-number/NUMBER` | Searches only your newest ~200 meetings and stops at the first match. Returns `{match, occurrences, scanned}` |
| `GET /meetings/join-url/NUMBER` | Returns `{url}`. Only builds the link; does not join |
| `GET /meetings/HISTORY_ID/insights` | Read-only aggregation of existing AI summary, minutes summary/chapters/todos, shared files and calendar agenda. Works on the primary and added accounts with a numeric `historyId`; missing meeting returns 404 `MEETING_NOT_FOUND`. Optional sources can be missing independently; `minutes.access` is `granted`, `denied` or `unavailable`. An absent source does not prove that no content exists |
| `GET /event-center/ongoing-meetings` | Meetings in progress |
| ⚠ `POST /meetings/HISTORY_ID/im-record-doc` | Creates a doc of the in-meeting chat. Asynchronous: poll `GET /meetings/HISTORY_ID` until `imRecord.status` is `succeeded` (it carries `url`) or `failed`. 409 `MEETING_IM_RECORD_UNAVAILABLE` |
| `GET /meetings/settings` | Read-only |
| `PATCH /meetings/settings` | Your VC preferences: 9 switches, strict schema |
| `GET /meetings/missed-calls`, `POST /meetings/missed-calls/confirm {total}`, `GET /meetings/vc-host`, `GET /meetings/incoming-call` | UI helpers. `incoming-call` always returns `{}` on the agent API |

MCP callers can use `list_meetings(accountId?, limit?, cursor?, filter?, raw?, fields?)` (`count` is the deprecated older name of `limit`; giving both with different values is `INVALID_INPUT`; rows come back as a Markdown table plus a JSON line with `hasMore`/`nextCursor`) and `get_meeting_insights(accountId?, historyId, raw?)`. By default both return compact views: history rows drop an empty `artifacts` list and a false `hasCalendarLink`; insights drop person `avatarUrl`s and empty lists or sections but keep every other field, including `minutes.access` and `minutes.url`. `raw=true` returns the HTTP body unchanged and is never sent to the backend. Each makes one fixed read-only GET and remains available in `lark-connector_MCP_READ_ONLY=1`. `limit` must be an integer from 1 to 50; `filter` can only be `calls`; cursor validation is shared with HTTP and happens before any HTTP request. Keep decimal IDs and cursors as strings, including values above `2^53`. Reuse known connection/account state; these tools do not perform a health or catalog lookup before the useful request. Backend account errors are returned without trying another account.

The insights aggregation reads existing sources; it does not generate or refresh a summary. Preserve every returned field and access marker. It does not replace full-transcript or export reads, and upstream permission or availability errors can still limit the result.

Older backends can list this route but omit the primary account's insights dependency, returning 400 `INVALID_MEETING` even for a numeric ID. Report that backend compatibility problem without changing accounts. Regression coverage starts the actual application with an isolated throwaway database and replaces only `MeetingInsightsDomain.insights` with synthetic results. The separate route fixtures inject domain dependencies themselves and do not verify application assembly; neither fixture proves real-account upstream permissions.

## Launching a meeting (primary account only)

| Call | Notes |
| --- | --- |
| ⚠ `POST /meetings/launch {topic? 1–60, onlyIfIdle?: true}` | Returns `launch{meetingId, meetingNumber, state, joinUrl, browserUrl, deadlineAt}`. **Nobody is invited**: send the `joinUrl` (messaging) or put it in an event. Only one launch at a time; calling again returns the one still waiting. Errors: 409 `MEETING_BUSY` and 503 `MEETING_STATUS_UNKNOWN` (both only with `onlyIfIdle`), 502 `MEETING_LAUNCH_ENDED` |
| `GET /meetings/launch` | Returns `{launch}` or `{}`. `state` is `waiting`, `handed-off` (someone else's device took the meeting over), `ended` or `expired` |
| `POST /meetings/launch/cancel {meetingId}` | This device leaves the meeting. Not found: 404 `MEETING_LAUNCH_NOT_FOUND` |

The server device holds the meeting open for 5 minutes, plus up to 2 more while someone is joining. If nobody joins, it leaves and the meeting ends. `GET /calendar/vc-start-authorized` shows whether the account may start meetings.

## A voice agent joining a meeting (registered conditionally)

These routes exist only when all of the following hold:

- The request is on the primary account.
- The server has a meeting room configured (`lark-connector_MEETING_ROOM_PORT` and `lark-connector_MEETING_ROOM_PUBLIC_ORIGIN`).
- A Chromium is configured (`lark-connector_MEETING_BROWSER`).

**Check `ol GET /` for `/meetings/:meetingNumber/participants` first.** If the route is missing, it returns 404.

1. ⚠ `POST /meetings/NUMBER/participants` returns 201 `{participant, offer:{type,sdp}}`. Takes 10–30 s.
2. `POST /meetings/participants/ID/answer {sdp:"v=0…"}`. Must arrive within 60 s.
3. `GET /meetings/participants/ID` returns `state`: `joining`, `awaiting-answer`, `connected` or `left`. `DELETE /meetings/participants/ID` leaves.

How the call works:

- The connection is one audio-only WebRTC link. What you send becomes this account's microphone; what you receive is everyone else, mixed.
- Only one participant can exist at a time (409 `MEETING_PARTICIPANT_BUSY`).
- It leaves on `DELETE`, when the connection fails or stays down for 30 s, when no answer arrives within 60 s, or when the meeting ends.
- Other errors: 502 `MEETING_PARTICIPANT_JOIN_FAILED`, 409 `MEETING_PARTICIPANT_NOT_AWAITING_ANSWER`, 404 `MEETING_PARTICIPANT_GONE`.

Join only the meeting the user named, and only when asked: everyone in it hears the agent as this account.

To use Codex as the voice agent: `node <skill dir>/scripts/codex-meeting.mjs <meeting number>` (or `--launch`), with `lark-connector_AGENT_BASE` and `lark-connector_AGENT_TOKEN` set. It answers through a local `codex app-server` realtime session (v3 over WebRTC, using the local Codex login). Codex keeps its tools, so people in the meeting can ask it to read files on the machine that runs the script.

## Minutes (妙记)

| Call | Notes |
| --- | --- |
| `GET /minutes?scope=all\|owned\|shared\|trash&cursor=` | 20 per page. `cursor` is a ms timestamp. Items: `{token, topic, url, startTime (ms), durationMs, isOwner, ownerName, summary}` |
| `GET /minutes/search?q=&cursor=` | 15 per page. `cursor` is an offset |
| `GET /minutes/TOKEN` | Returns `access`, `summary` (Markdown), `todos`, `chapters`, `keywords`, `participants`, `owner`, `downloadable`, `durationMs`, `subtitleLanguages[]`. 403 `MINUTES_DENIED` |
| `GET /minutes/TOKEN/transcript?lang=default\|<code>` | Returns `paragraphs[{speakerId,startMs,endMs,text}]`, `speakers{}`, `total`. Reads at most 40 pages of 500 paragraphs. `truncated:true` means paging stopped while upstream still reported more; it is absent on a complete read. Paragraphs without text are omitted, so `total` can exceed the paragraph count on a complete read; check `truncated`, not `total`. For the full text use `export?format=txt` |
| `GET /minutes/TOKEN/export?format=txt\|srt&speaker=1&timestamp=1` | The easiest way to get the full text. If the owner disabled export: 403 `MINUTES_DOWNLOAD_DISABLED` |
| `GET /minutes/TOKEN/media` | Audio or video, with Range support. A non-downloadable recording needs Range requests of ≤2 MB each. Prefer the transcript or export when you need text |
| `GET /minutes/TOKEN/abilities` | Returns `{canComment, canModify, exportable, isOwner}`. Check this before any write |
| `PATCH /minutes/TOKEN {topic 1–80}` | Rename |
| `POST /minutes/trash {tokens 1–50}`, `POST /minutes/restore {tokens}` | Move to and from the trash. There is no permanent delete |
| Comments | `GET /minutes/TOKEN/comments`; ⚠ `POST /minutes/TOKEN/comments {paragraphId, content≤1000}`; ⚠ `POST …/comments/CID/replies {content}`; `DELETE /minutes/TOKEN/comment-contents/CONTENT_ID` |
| ⚠ Share | `GET /minutes/TOKEN/share?users=a,b`; `POST /minutes/TOKEN/share {userId, permission:"view"\|"edit", notify}`. `notify:true` sends the person a Lark message |
| ⚠ AI summary | `GET /minutes/TOKEN/summary-items`; `PUT /minutes/TOKEN/summary-items {items:[{contentId,data}]}` overwrites the summary |

## Workflows

1. **Notes and transcript of a meeting**
   1. Find the meeting: `GET /meetings?count=50` with cursor paging, or `by-number` from a join link.
   2. For existing summary, todos, chapters, files or agenda, call `GET /meetings/HISTORY_ID/insights` (MCP `get_meeting_insights`) on the chosen account.
   3. For minutes-specific detail or a transcript, use the minutes URL returned by insights, or read `GET /meetings/HISTORY_ID` and take `documents[kind=minutes].url`; then use `GET /minutes/TOKEN` or its `transcript` route.
   4. For the full text: check `abilities.exportable`, then `GET /minutes/TOKEN/export?format=txt`. If export is not allowed, use `transcript`.
2. **Start a meeting and bring someone in**
   1. `POST /meetings/launch {topic}`.
   2. Confirm with the user, then send the `joinUrl` in a chat.
   3. Poll `GET /meetings/launch` until `handed-off`. It becomes `ended` if nobody joins within 5–7 minutes.
