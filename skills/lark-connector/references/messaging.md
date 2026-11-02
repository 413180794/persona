# Messaging: send, act on and read messages

Paths are relative to `/agent/v1`. Calls use the `ol` helper from SKILL.md.

- `F` = feedId, `M` = messageId.
- ⚠ = visible to others or cannot be undone. Confirm first.
- _(unverified)_ = never run against the real Lark gateway. Read it back after use.

## Contents

1. Text, post, markdown, @ and emoji
2. Replies and threads
3. Images
4. Files, video, voice, location
5. Stickers, cards, votes
6. Scheduled messages
7. Urgent (加急)
8. Reading: history, threads, read receipts
9. Message actions
10. Translation
11. Downloading attachments
12. Send gates, retries, pitfalls

## 1. Text, post, markdown, @ and emoji

`POST /conversations/F/messages` returns `202 {messageId}`. On the agent API this waits for Lark's ack and returns the real id.

| Field | Rule |
| --- | --- |
| `text` | Required. 1–100 000 characters after trim |
| `editorType` | One of four values:<br>`simple` (default): plain text; `**` is sent literally.<br>`post`: rich post, allows `title`.<br>`rich`: a Markdown subset (code fences, inline code, lists, `[t](https://…)` links) where mentions, emoji and images still work; `**bold**` is not parsed.<br>`markdown`: the whole message is Lark Markdown (good for code blocks); it **ignores** mentions, emojis and images |
| `title` | ≤256. Only for post, rich, markdown, or messages with images |
| `mentions` | ≤100 `[{userId,name,isOuter?}]`. Each needs `@<name>` written literally in `text`. @all: `{userId:"all",name:"所有人"}` with `@所有人` in the text. A user group: `userId:"usergroup:<id>"` (ids from `GET /mention/user-groups?feedId=F&q=`) |
| `emojis` | ≤100 `[{key,label}]`. Each needs `[label]` in `text`; keys come from `GET /resources/emojis` |
| `images` | ≤10. See section 3 |
| `clientId` | Optional UUID (the server fills one). Re-posting the same `clientId` is idempotent: the first stored intent wins and the new body is ignored |
| `expectedSenderId` | Optional. Set it to `/me` `userId`; if the account was switched, the call fails with 409 `SENDER_CHANGED` instead of sending as someone else |
| `autoRecall` | Optional. Recalls the message after `autoRecallDelaySeconds` (default 5). Needs the FLiNG switch on |

New messages sent with `mentions` use AT element IDs in the internal rich-text index, while the notification list retains user IDs. This corrects the malformed mention payload that could make forwarding fail with upstream error 5115. Existing stored or upstream messages are not rewritten by the fix; the `mentions` request format above is unchanged.

Errors:

- 400 `INVALID_MESSAGE` with field details.
- 403 gates (section 12).

````bash
ol POST /conversations/F/messages '{"text":"@张三 请看一下","mentions":[{"userId":"123","name":"张三"}]}'
ol POST /conversations/F/messages '{"editorType":"markdown","text":"```sql\nselect 1\n```"}'
````

Check the result: run `GET /conversations/F/messages?tail=1&limit=5` and confirm that `parts` contains `kind:"at"` for each mention.

## 2. Replies and threads

`POST /messages/M/replies` takes the same fields as section 1 plus:

- `replyMode?: "direct" | "thread"` (default `direct`).
- `partialReply?{position:"head"|"middle"|"tail",text≤10000}`

The chat and the thread come from the target message `M`; `feedId` and `threadRootId` in the body are ignored. `direct` quotes `M` in its chat. `thread` replies in `M`'s thread: on a reply inside a thread it finds the thread's root, on a top-level message it starts a thread rooted there. In a topic group (`chatMode:"topic"`) every top-level message already opens a topic and Lark refuses reply-in-thread there, so `thread` puts the reply into `M`'s topic the same way `direct` does. The answer is 202 `{messageId, feedId, replyToMessageId, threadRootMessageId?}`.

```bash
ol POST /messages/M/replies '{"text":"收到"}'                       # quote M
ol POST /messages/M/replies '{"text":"收到","replyMode":"thread"}'  # into M's thread
```

Errors:

- 404 `MESSAGE_NOT_FOUND`: not in this account.
- 409 `THREAD_ROOT_UNRESOLVED`: the root of `M`'s thread cannot be resolved safely.
- 409 `REPLY_IN_THREAD_CHAT_UNSUPPORTED`, `REPLY_IN_THREAD_FOLDED_MESSAGE_UNSUPPORTED`, `REPLY_IN_THREAD_SYSTEM_MESSAGE_UNSUPPORTED`, `REPLY_IN_THREAD_ROOT_RECALLED`: Lark refused the thread placement (Lark code 5072–5075 in `error.larkCode`). Nothing was delivered; `error.clientId` names the failed message, which stays in the thread history (`GET /messages/ROOT/thread`) as `local:<clientId>` with `localState:"failed"`. `POST /outbox/CLIENT_ID/retry` resends it with the same placement and `DELETE /outbox/CLIENT_ID` discards it (section 12); to answer anyway, reply again with `replyMode:"direct"` and a new `clientId`.

A message explicitly marked `threadMessageType:"root"` may itself quote an older message (`parentId` is present). Replies inside that thread still resolve to this root. A parent-linked message marked `none` or `reply` is not accepted as a root; conversation and thread identity must still match. This applies to Agent thread replies and web replies whose composer has not specified a root.

To find thread roots, look in the history for `threadMessageType:"root"` or `threadReplyCount>0`. List a chat's threads with `GET /conversations/F/threads` (see `groups.md`).

## 3. Images

**Single image:**

- `POST /conversations/F/images` with `{data:<base64 ≤10 MiB chars, ~7.5 MB>, mimeType:"image/png", name?, clientId?, replyTo?}`.
- No `threadRootId`.
- Errors: 400 `INVALID_IMAGE` or `INVALID_IMAGE_TYPE`; 403 Brave Climb gate.

**Several images with text in one message:**

1. Upload each image:
   - `POST /image-uploads` with `{clientId:<UUID, required>, data:<base64>, mimeType, name}` returns `{key}`.
   - The body can be large; write it to a file and pass `@body.json`.
   - `olkSealed:true` is for the web page's OLK encrypted pictures only: `data` must be a payload the page already encrypted (it starts with `OLKI`); the server only adds a cover and the trailer, and answers `{key, cover:{width,height}}` with the size of that cover. The cover is a sticker picked at random for each picture (240×240 RGBA PNG, one of a fixed set drawn by the server) while the local setting `olkImageStickerCover` is on (the default), else a neutral grey placeholder (400×300); or the optional `cover`: a base64 PNG the sender picked and confirmed as visible to everyone, at most 1 MB, one whole PNG ending at its IEND chunk; anything else, or `cover` without `olkSealed`, answers 400 `INVALID_OLK_COVER`. `coverTopic`, sent by older pages, is ignored. A plain picture, or `olkSealed` together with `braveClimb`, answers 400 `INVALID_OLK_IMAGE`. It is not gated by `braveClimbCodec`. Agents have no way to make the key part of such a message and should not use it.
2. Send:
   - Use `images:[{id:<your own UUID>,key,name,width?,height?}]`.
   - Put U+FFFC, the image `id`, U+FFFC (write `\ufffc<id>\ufffc` in JSON) in `text` where each image goes. **An image whose placeholder is missing is dropped silently.**
3. One image whose placeholder is the whole `text` (and `editorType` is not post) goes out as a plain image message. This is how to send an image into a thread.

## 4. Files, video, voice, location

These take raw bytes, so use `--upload`. They do not go through the outbox: no idempotent retry. After a timeout, read the history before resending.

`threadRootId` sends the message into that thread, with `replyTo` as the message it answers inside it. In a topic group (`chatMode:"topic"`) the message goes into that topic without Lark's reply-in-thread flag, which Lark refuses there; the placement is the same as in a thread.

| Kind | Call | Required query | Limits | Returns |
| --- | --- | --- | --- | --- |
| ⚠ File | `POST /conversations/F/files` | `clientId` (UUID), `name` (1–255); optional `mime`, `replyTo`, `threadRootId` | ≤100 MiB | 201 `{fileKey,messageId?,name,size}` |
| ⚠ Video | `POST /conversations/F/videos` | `clientId`, `name`, `coverBytes` (size of the cover); optional `mime` (default `video/mp4`), `durationMs` (read from mp4 if omitted), `replyTo`, `threadRootId` | Video ≤100 MiB. Cover PNG/JPEG ≤5 MiB | 201 `{durationMs,fileKey,messageId?,name,size}` |
| ⚠ Voice | `POST /conversations/F/voice` | `clientId`; optional `replyTo`, `threadRootId` | Opus in Ogg or WebM, 0.3 s–5 min, ≤20 MiB | 201 `{durationMs,messageId?,size}` |
| ⚠ Location | `POST /conversations/F/location` (JSON) | `{clientId,name≤120,description?≤300,latitude,longitude,mapImage:<base64 PNG/JPEG ≤3 MiB>,replyTo?,threadRootId?}` | — | 201 `{messageId?}` |

```bash
ol POST /conversations/F/files --upload ./季度报告.pdf \
  --url-query "clientId=$(uuidgen)" --url-query 'name=季度报告.pdf' --url-query 'mime=application/pdf'
# Video: the body is the cover bytes followed by the video bytes
cat cover.jpg clip.mp4 > /tmp/ol-video.bin
ol POST /conversations/F/videos --upload /tmp/ol-video.bin --url-query "clientId=$(uuidgen)" \
  --url-query "coverBytes=$(stat -c%s cover.jpg)" --url-query 'name=clip.mp4' -m 900
```

Errors:

- 400: `CHAT_FILE_EMPTY`, `INVALID_FILE_UPLOAD`, `INVALID_VIDEO_UPLOAD` (cover is not PNG/JPEG, or `coverBytes` ≥ body), `VOICE_TOO_SHORT`.
- 413: `CHAT_FILE_TOO_LARGE`, `VOICE_TOO_LARGE`, `VOICE_TOO_LONG`.
- 415: `VOICE_FORMAT_UNSUPPORTED`.

Sending to a person: get their DM `feedId` first (`groups.md`, "Create DM"). Check the result with `GET /conversations/F/messages?tail=1` and look for `messageTypeName:"FILE"`.

## 5. Stickers, cards, votes

| Job | Call | Body |
| --- | --- | --- |
| ⚠ Sticker | `POST /conversations/F/stickers` | `{key, setId?, stickerId?, width?, height?, title?, clientId?, replyTo?}`. Take key, setId and stickerId from `GET /resources/stickers`. A bad body returns 500 |
| ⚠ Person card | `POST /conversations/F/shares/user` | `{userId}` |
| ⚠ Group card | `POST /conversations/F/shares/group` | `{chatId}` (the group''s replies read: `POST /conversations/F/threads/THREAD_ID/read-position {threadPosition}`.
- Read receipts:
  - One message: `GET /messages/M/read-state` returns `{readCount,unreadCount,readUsers,unreadUsers}`.
  - Batch: `POST /messages/read-states {pairs:[{feedId,messageId}]}` (≤100).
- Reactions: `GET /messages/M/reactions` (local). `GET /messages/M/reaction-detail` (upstream, with userIds).
- Recalled messages:
  - A recalled message stays replyable, as in the official client: `capabilities.reply` and `replyInThread` are `true`, and `replyTo`/`threadRootId` on a send accept a recalled parent (the quote renders as the recall notice).
  - Audit: `GET /messages/recalled`. Filters: `conversation` (name match), `feedId`, `date` (`today` or `YYYY-MM-DD`) or `since`/`until` (ISO, not mixed with `date`), `timezone`, `format=json|text`, `limit≤200`, `cursor`. The original content is returned; treat it as private.
  - Which chats had recalls: `GET /messages/recalled/conversations`.
- Encrypted and encoded messages (the UI's 已解密消息 list):
  - Which chats have them: `GET /messages/encrypted/conversations?braveClimb=1|0&limit=1..100&cursor=` (default 20). Returns `{items:[{latest,total}],hasMore,nextCursor}`, newest message first.
  - One chat: `GET /messages/encrypted?feedId=F&braveClimb=1|0&limit=1..200&cursor=` (default 100). `feedId` is required. Returns `{items,hasMore,nextCursor}`; items are `{feedId,kind,message,sentAt,conversationName,conversationIsGroup,conversationChatMode,conversationAvatarUrl}`, newest first.
  - `kind` is `olk` (end-to-end envelope) or `braveClimb` (Brave Climb envelope). `braveClimb=0` leaves out Brave Climb, as the UI does when that codec setting is off.
  - Agent 读取时按上述密钥库权限自动解密 OLK；浏览器的 `/api` 列表仍返回密文，由页面走托管解密流程。Brave Climb 仍使用共享 codec 解码，不由 OLK 密钥库处理。
  - Errors: 400 `INVALID_ENCRYPTED_MESSAGE_QUERY` (missing `feedId`, bad `cursor`).
- Other reads:
  - Time limits: `GET /messages/operation-config` returns `{recallSeconds,editSeconds}` (commonly 86400).
  - `GET /messages/marks`: flagged and saved ids.
  - `GET /messages/transmit-targets`: recent forward targets.
  - `GET /messages/M/cot`: an AI bot's reasoning trace.
  - `GET /message-url-preview?url=&messageId=`
  - `GET /messages/M/group-card`: state of a group card.

## 9. Message actions

These need the message in the local store; if it came from search, load it with `aroundPosition` first. Check `isMine`, `capabilities` and the time limits before editing or recalling.

| Job | Call | Notes |
| --- | --- | --- |
| ⚠ Edit own message | `PATCH /messages/M` `{text, editorType?, mentions?, emojis?, images?, title?}` | **Send `mentions` again** or the @ turns into plain text. Not yours: 500 "Only your own messages can be edited". Recalled messages cannot be edited |
| ⚠ Recall own | `POST /messages/M/recall` | Returns `{recalled:true}`. Cannot be undone |
| ⚠ Recall someone else's (group admin) | `POST /messages/M/group-recall` | 422 `GROUP_RECALL_REFUSED` |
| ⚠ Delete for me | `DELETE /messages/M`; `POST /messages/delete-many {messageIds≤100}` | Only this account's view |
| ⚠ React | `POST /messages/M/reactions {key}`; `DELETE /messages/M/reactions/KEY` | Keys from `GET /resources/emojis` |
| ⚠ Forward | `POST /messages/M/forward {feedIds}`; `POST /messages/forward-many {messageIds≤100,feedIds≤50}` | Targets must be existing feedIds |
| ⚠ Merge-forward | `POST /messages/merge-forward {messageIds 1–100, feedIds 1–50}` |  |
| ⚠ Pin a message | `POST /messages/M/pin` | Does not return the pinId |
| ⚠ Unpin | `DELETE /messages/M/pin '{"pinId":"…"}'` | Take `pinId` from `GET /conversations/F/pins` `items[].id` (match on `messageId`). That GET clears the unread-pin mark |
| Flag / unflag | `POST`/`DELETE /messages/M/flag` | Only affects you |
| Favorite | `POST`/`DELETE /messages/M/favorite`; `POST /messages/favorite-many` | Only affects you |
| Link | `POST /messages/M/link`; `POST /messages/link-many {messageIds≤100}` (same chat) | Returns `{url}` |
| ⚠ Card button | `POST /messages/M/card-actions/ACTION_ID` with no body, or `{params:{input_value\|form_value\|selected_option\|…}}` | Find a card node in `parts[].metadata` with `actionMethod:2` and take its `actionId`. Only those submit. Errors: `CARD_ACTION_NOT_SUBMITTABLE`, `CARD_ACTION_NOT_FOUND` |
| ⚠ Join via a group card | `POST /messages/M/group-card/join` |  |
| Save a file or image to My Space | `POST /messages/M/save-to-space {feedId,fileKey}` | Returns `{fileToken,url?}` |
| ⚠ Export messages to a doc | `POST /messages/export-doc {feedId,messageIds 1–100,timeZone?}` | Creates a doc |
| Doc permissions for linked docs | `GET /messages/doc-permissions?ids=`; ⚠ `POST /messages/doc-permissions {grants:[{messageId,url,role}]}` | Grants permissions to the chat |
| Voice to text | `POST /messages/M/audio-text {feedId}` | Returns `{text}` |
| ⚠ Restricted (confidential) message | `POST /messages/M/restrict {feedId,restricted}` |  |
| Save image as sticker | `POST /messages/M/sticker {imageKey}` |  |
| Link-preview buttons | `GET`/`POST /messages/M/url-preview-actions` |  |
| +1 an auto-folded message | `POST /messages/M/fold-follow` |  |

## 10. Translation

- `POST /messages/M/translate {lang?}` with keys from `GET /translate/languages` (`zh`, `en`, `ja`…). It returns translated `parts` and works only when the local message carries translate metadata, else 404 `MESSAGE_NOT_TRANSLATABLE`.
- `GET /messages/translations?ids=(≤50)&lang=` returns translations that already exist.
- `POST /translate/text {texts[1–100] (total ≤5000), target, source?}` translates any text without sending it.
- Chat auto-translate lives in `chats.md`.

## 11. Downloading attachments

| What | Call |
| --- | --- |
| File in a chat | `GET /conversations/F/files/M/KEY?name=…` (Range supported). KEY is the file part's `refId` (`file_v3_…`), and `name` is the part's `text`. 403 `FILE_RISKY` or `CHAT_DOWNLOAD_RESTRICTED` |
| Is the file still there | `…/files/M/KEY/state` returns `normal`, `recoverable`, `unrecoverable` or `cleaned` |
| Preview as text or PDF | `…/files/M/KEY/preview?ext=pdf\|docx\|xlsx\|txt\|…` (Office becomes PDF; text and code come back as text/plain) |
| Open in Docs | `…/files/M/KEY/open-with-docs?ext=docx\|xlsx\|csv` returns a url |
| Video | `GET /conversations/F/videos/M/KEY` (Range) |
| Image in a message | `GET /messages/M/media/KEY?size=` (prefer this; it decrypts images inside posts) |
| Any resource | `GET /media/KEY?size=` (`size` must be a multiple of 24, ≤624) |
| Attachment record | `GET /attachments/ID` returns **JSON metadata**, not bytes |

Use `-o FILE` for bytes.

Both image routes accept `refresh=1` to bypass the stored original (and thumbnails); the response is `Cache-Control: no-store`. Use this only for an explicit fresh download, such as retrying an OLK picture whose validation failed. The browser also bypasses its HTTP and service-worker media caches; SHA-256 and GCM validation still run on every retry.

Missing outer media decryption metadata returns HTTP 503, JSON `code: "MEDIA_DECRYPTION_PENDING"`, `Retry-After: 2`, and `Cache-Control: no-store`; ciphertext is never returned as a successful image. HTTP 410 means the Lark resource has expired or been deleted and must not be retried. Preserve other HTTP statuses: retry network errors, 408, 429 and 5xx only with a finite budget and respect `Retry-After`.

The OLK browser reader makes at most three attempts per download, with a cancellable 55-second deadline per attempt covering headers and body. Automatic online/foreground recovery applies only to visible failed pictures and allows at most two extra rounds. Retry-After waits over 30 seconds defer to a later retry; manual and recovery retries still respect that timestamp. Missing group keys can be searched again from the message, with separate searching/not-found/budget/network states; history searches stay serial and retain the eight-search per-chat page budget and 20 older + 4 newer pages per search. Online recovery adds at most two searches for a failed key. This changes only history recovery, not key-directory writes.

## 12. Send gates, retries, pitfalls

- Gates (switches in `platform.md`):
  - `AUTO_RECALL_DISABLED` applies to send, reply and retry with `autoRecall:true`.
  - `BRAVE_CLIMB_CODEC_DISABLED` applies to send, reply, edit, retry, `/image-uploads` and `/images`.
  - Scheduled messages check neither.
  - On a gate, report to the user; do not flip the switch yourself.
- Retry:
  - After a failed send, POST again with the **same `clientId`**; it is idempotent.
  - A failed text, reply or single-image send stays in the chat or thread history as `local:<clientId>` with `localState:"failed"`.
  - `POST /outbox/CLIENT_ID/retry` only handles text, reply and single-image sends. Text and reply retries return `local:<clientId>` without waiting for Lark.
  - Files, video, voice and location have no retry.
- Discard:
  - ⚠ `DELETE /outbox/CLIENT_ID` removes a message that never reached Lark: its `local:<clientId>` row and its outbox entry. It takes a local message that failed, or one still pending with no send under way. Nothing goes to Lark; it never recalls or deletes a real message. Answers 200 `{clientId, feedId, messageId}` and emits `message.deleted` for `local:<clientId>`.
  - Errors: 400 `INVALID_CLIENT_ID` (not a UUID), 404 `OUTBOX_ENTRY_NOT_FOUND` (no local message with that `clientId`, or already discarded), 409 `MESSAGE_ALREADY_SENT` (Lark accepted it; recall the real message instead), 409 `MESSAGE_SENDING` (a send is under way; wait until it has failed).
  - The `clientId` stays spent: sending or retrying it again answers 409 `MESSAGE_DISCARDED`. Send with a new `clientId`.
- Auto-recall timers live in process memory; a restart loses pending recalls.
- Many failures are plain 500s with a message ("Message not found", a bad sticker body, a bad reaction key, a bad translate lang). Check the input before assuming an outage.
- Uploads stop if the client disconnects mid-upload. Keep `-m` long for large files.

OLK 旧账号兼容：仅服务端明确返回尚未建库时，网页使用该账号浏览器已有身份和群钥匙；不自动新建浏览器身份。已建库（含锁定）或状态查询失败时不回退。异步浏览器加密、解密和查找群钥匙完成后重新核验建库状态，变化时停止操作；发送保留草稿供重试。浏览器钥匙仍需由主人在设置中迁入密钥库，Agent 不使用浏览器钥匙，也不能代为迁移。

网页 OLK 自动挑选的回放句或语气词有 30% 概率跟一个原生吃瓜表情（`Lark_Emoji_Eating_0`），作为独立表情元素发送，不修改信封编码。用户自填外显、AI 建议和纯图片正文不加；此前单独发送的群钥匙分发消息仍可使用自动外显。回放仅拼文字片段并去掉形如 `[标签]` 的 1–20 位字母、数字或下划线标签，空句不用。接收时表情跟随外显展示一次；未建库、锁定或解密失败时显示已有公开外显和表情，没有外显才保留状态提示。明文发送许可仍匹配完整发送文本，同时精确绑定获准的对外表情列表，额外表情和文本变更不会自动放行。此变更不增加 Agent 路由，不改变 Agent 读取的解密权限。

网页 OLK 输入框的内置表情插入到最近聚焦的正文或“对外显示”光标处，并替换选中文字；对外文字保留字符校验，长度按包含信封的整体传输上限校验。消息气泡在未配置密钥库、锁定、缺钥匙、解密失败或等待解密时，优先以普通内容展示已有对外文字和原生表情片段（纯文本标签保持原样）。无对外文字时保留原状态提示，成功解密后显示正文。此行为只影响网页，不改变 Agent 响应或解密授权。

普通群的 `GET /conversations/F/messages`（尾页、历史页和定位窗口）不再按时间额外混入 `position <= 0` 的话题专属回复；网页实时消息也保持同一边界。使用 `GET /messages/ROOT/thread` 读取这些回复。带正常会话位置的普通引用回复仍在主流，不能只凭 `threadId` 或 `parentId` 判断它应当隐藏。话题回复计数及话题入口保留，路由、分页参数和响应字段不变。

新发 OLK 消息：自填“对外显示”的已知表情标签按表情目录登记到发送请求的 `emojis`，服务端编码成原生 emotion 元素，官方客户端可直接显示。未知标签仍为文字；私密正文里的标签不加入外显表情列表。接收页把原生表情及公开文字统一放入外显行、外显视图或无密钥时的公开内容中，不在私密正文重复展示。无历史数据改写。

手写外显不再限制为 20 字；20 仅保留为自动外显的预留预算。输入框不截断，发送时按外显实际 UTF-16 长度预留信封空间；整条 OLK 消息上限仍为 10000 UTF-16 单位，无法容纳时拒绝发送并保留草稿。owner-only `POST /api/olk/encrypt` 新增可选 `coverLength`（0–9999 整数，实际预留至少 20）；浏览器兼容加密使用相同预算。该接口没有 `/agent/v1` 对应路由，Agent 权限不变。明文发送许可除文本和图片外，也精确匹配本次批准的表情 key/label 列表，不能增添、删除或替换表情。
