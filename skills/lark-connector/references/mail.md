# Mail: read, search, write, reply, organize, settings

Paths are relative to `/agent/v1`.

- ⚠ marks an action that is visible to others or cannot be undone. Confirm it first.
- _(unverified)_ marks a write the code says was never sent to the real Lark gateway. Read the result back.

## Contents

1. Ids and encoding
2. Read and search
3. Write, send, reply, forward
4. After sending: status, recall
5. Organize: actions, delete, folders and labels
6. Share, invitations, unsubscribe, AI
7. Settings: signatures, rules, auto-reply, aliases
8. Shared mailboxes
9. Pitfalls

## 1. Ids and encoding

- Received mail uses 28-character base64 thread and message ids, such as `Y/V+oCrWBw+a15UvAd4XwkgC5sM=`.
  - **Percent-encode them** in paths and queries (`/` → `%2F`, `+` → `%2B`, `=` → `%3D`). Unencoded ids return 404 "Route not found". In a query, a raw `+` becomes a space.
  - The pattern is `^[\w+/=-]{1,128}$`.
- Drafts and mail sent from this client use UUIDs.
- System labels are named: `INBOX`, `IMPORTANT`, `OTHER`, `FLAGGED`, `DRAFT`, `SENT`, `OUTBOX`, `SCHEDULED`, `ARCHIVED`, `SPAM`, `TRASH`.
- User labels, folders, rules, signatures and auth codes use decimal ids.

```bash
enc() { python3 -c 'import sys,urllib.parse;print(urllib.parse.quote(sys.argv[1],safe=""))' "$1"; }
ol GET "/mail/threads/$(enc "$THREAD_ID")"
```

## 2. Read and search

| Job | Call | Notes |
| --- | --- | --- |
| Folders and counts | `GET /mail/labels` | Returns `labels[{id,isSystem,kind:system\|label\|folder,name,unreadCount}]` |
| List threads | `GET /mail/threads?label=INBOX&count=30&unread=true` | **`count` really maxes out at 30.** Past 30 threads in the mailbox, a higher count returns 400. Page with `cursorMessage` (the encoded `cursor.messageId`) plus `cursorTimestamp` (ms), always together. Items carry `threadId`, `messageId` (latest), `subject`, `snippet`, `from`, `receivedAt`, `unread`, `hasAttachments`, `messageCount` |
| **Read a thread (preferred)** | `GET /mail/threads/<enc T>` | `messages[]` oldest first. Each has `{messageId,from,to,cc,bcc,replyTo,subject,body,bodyFromHtml,bodyIsTruncated,attachments[{fileKey,name,size,large}],unread,hasHtml}`. The full body is up to 200k chars. Reading does **not** mark mail read |
| Message metadata | `GET /mail/messages/<enc M>` | The body is **cut at 1000 chars with no flag**, and attachments are missing. Use it for metadata only |
| HTML body | `GET /mail/messages/<enc M>/html` | Returns `{html (sanitized),inlineImages,externalImages,blockedImages}`. Image URLs point to `/api/mail/assets/*`, which the agent port lacks. Take the `fileKey` and call `GET /mail/compose/images/FILE_KEY` |
| Download an attachment | `GET /mail/messages/<enc M>/attachments/FILE_KEY -o FILE` | M must be the message that lists the attachment |
| Preview an attached .eml | `GET …/attachments/FILE_KEY/eml` | Returns `{subject,from,to,date,text,html,attachments}` (≤15 MiB) |
| Search | `GET /mail/search?q=&attachments=true&label=&count=≤100` | Needs `q` or `attachments=true`. Page with `offset` plus `session` from `next` |
| Advanced search | `GET /mail/search/advanced?q=&from=&to=&subject=&exclude=&after=&before=&attachments=true&label=&scene=trashSpam` | List fields take values separated by `,` or `;`. `after` and `before` are ms |
| Thread from an IM mail card | `GET /mail/locate-thread?locate=<subject>&locateFrom=<sender>` | Returns `{status:"found",threadId}` or `ambiguous` / `none` |
| Translate | `GET /mail/messages/<enc M>/translation?target=zh\|en\|ja\|th&threadId=` |  |
| Smart replies | `GET /mail/messages/<enc M>/smart-replies` |  |

## 3. Write, send, reply, forward

**Draft body** (one schema shared by drafts, send and schedule; unknown fields are dropped silently):

| Field | Rule |
| --- | --- |
| `to`, `cc`, `bcc` | `[{address, name?}]`, ≤100 each. Send needs at least one recipient |
| `subject` | ≤998 |
| `html` | ≤1 000 000 chars. **Plain text is generated from it; there is no text field.** Use `<p>` / `<br>` for line breaks |
| `attachments` | `[{fileKey,name,size?,large:false}]`, ≤50 |
| `images` | `[{cid,fileKey,name}]`, ≤50. The html must contain `<img src="cid:CID">` for each one, or it is dropped |
| `threadId`, `replyToMessageId` | For replies |
| `draftId` | To update an existing draft |
| `from` | Must be one of `compose/account.addresses` |
| `priority` | `high`, `normal` or `low` |
| `readReceipt`, `sendSeparately` | Booleans |
| `docs` | `[{url,permission:"keep"\|"read"\|"edit"}]` (≤20) grants recipients doc access _(unverified)_ |

HTML is sanitized:

- Kept tags: a, b, blockquote, br, code, del, div, em, h1–h4, hr, i, li, ol, p, pre, s, span, strike, strong, u, ul, table, tbody, td, th, thead, tr.
- Styles survive only if they are on an allow-list.

| Job | Call | Notes |
| --- | --- | --- |
| Sender identities | `GET /mail/compose/account` | Returns `{address,addresses[],name,signatures,undoSend}` |
| Look up recipients | `GET /mail/compose/contacts?q=`, `GET /mail/at-contacts?q=` |  |
| Recipients' access to a linked doc | `GET /mail/doc-auth?addresses=a,b&docToken=&docType=22` | Check before sending with `docs` |
| Upload an attachment or inline image | `POST /mail/attachments --upload FILE --url-query name=FILE` | ≤20 MiB each. Returns `{fileKey,large:false,name,size}` |
| Save a draft | `POST /mail/drafts` | Returns `{draftId,threadId,savedAt}`. Only you can see it |
| Read a draft | `GET /mail/drafts/<threadId>?draftId=` |  |
| Delete a draft | `DELETE /mail/drafts/DRAFT_ID` |  |
| ⚠ **Send** | `POST /mail/drafts/send` with the full draft body | Returns `{messageId,threadId}`. **Delivered immediately; nothing can undo it** (`/mail/sent/ID/undo` does nothing). There is no "send draft by id", so re-submit the whole body. A failed send leaves the mail in DRAFT; delete it if the user gives up |
| ⚠ Schedule | `POST /mail/drafts/schedule` with the draft body plus `scheduledAt` (ISO **with offset**, 60 s to 366 days ahead) | The mail moves to `SCHEDULED` |
| Cancel a scheduled mail | `POST /mail/drafts/<enc M>/cancel-schedule` | Back to drafts |

Workflows:

1. **New mail.**
   1. Resolve addresses (`compose/contacts`).
   2. Upload attachments.
   3. Save a draft and show the user recipients, subject, body and attachments.
   4. On approval, `send`.
   5. Read back the thread and `send-status`.
2. **Reply or reply-all.**
   1. Read the thread and pick message M.
   2. Choose recipients:
      - Reply goes to M's `replyTo`, else to `from`. If M is your own mail, it goes to M's `to`.
      - Reply-all adds M's `to` and `cc`, minus your own addresses.
   3. Send with `{threadId:T, replyToMessageId:M, subject:"Re: <original>", …}`.

   **`threadId` is required**, or the call returns 404 `MAIL_REPLY_TARGET_NOT_FOUND`. **Keep the subject**: a changed subject splits the reply into a new thread. Quoting the original is up to you.

3. **Forward.** Send a new mail with no `threadId`, subject `Fwd: …`, and the original `attachments` copied over (`fileKey`, `name`, `size`, `large`). Put a forwarded header and the original text in `html`.

## 4. After sending: status, recall

| Job | Call | Notes |
| --- | --- | --- |
| Delivery status | `GET /mail/messages/<enc M>/send-status` | Each recipient's `state` is one of `unknown`, `delivering`, `retry`, `deferred`, `delivered`, `awaitApproval`, `approvalRejected`. 400 here is a bare `{error:"…"}` |
| Recall status | `GET /mail/messages/<enc M>/recall` | Returns `{state:none\|recalling\|done, recipients[{address,state,reason}]}`. `reason` is one of `read`, `notLarkMail`, `notSameTenant`, `invalidAddress` |
| ⚠ Recall | `POST /mail/messages/<enc M>/recall` | Only your own mail, within 24 h. 409 `MAIL_RECALL_TOO_LATE` or `MAIL_RECALL_ALREADY`. If `state` is still `none` afterwards, it did not take. External or already-read recipients cannot be recalled. _(unverified)_ |

## 5. Organize: actions, delete, folders and labels

| Job | Call | Notes |
| --- | --- | --- |
| Bulk action | `POST /mail/actions {action, threads:[{threadId, messageIds?}] (1–100), labelId?}` | Actions: `read`, `unread`, `flag`, `unflag`, `archive`, `inbox`, `move`, `label`, `unlabel`, `trash`, `untrash`, `spam`, `notSpam`. `move`, `label` and `unlabel` need `labelId`. `trash` is reversible |
| Mark a folder read | `POST /mail/labels/LABEL/read-all` |  |
| Select across pages | `POST /mail/select-all {action:"read"\|"unread"\|"flag"\|"unflag"\|"archive",labelId,maxTimestampMs,excludeThreadIds?,unreadOnly?}` | Whole mailbox. _(unverified)_ |
| ⚠ Delete forever | `POST /mail/actions/delete-forever {mailbox:"TRASH"\|"SPAM",threads:[{threadId}]}` | Trash the mail first. Fails with 409 `MAIL_NOT_IN_MAILBOX` if any thread is elsewhere. _(unverified)_ |
| ⚠ Empty trash or spam | `POST /mail/actions/empty {mailbox,newestMs}` | Repeat while `more:true`. **Currently fails when the mailbox holds more than 30 threads** (it lists 100 at once). In that case, list 30 at a time and `delete-forever` in batches. _(unverified)_ |
| Folder and label tree | `GET /mail/organize` |  |
| Create a label or folder | `POST /mail/organize/labels` or `/mail/organize/folders` with `{name≤64 (no "/"), color?, parentId?}` |  |
| Edit | `PATCH /mail/organize/:kind/ID {name?,color?}` |  |
| Delete | `DELETE /mail/organize/:kind/ID` | A folder must be empty first |
| Reorder labels | `POST /mail/labels/order {labelIds}` |  |

## 6. Share, invitations, unsubscribe, AI

| Job | Call | Notes |
| --- | --- | --- |
| ⚠ Share a thread to chats | `POST /mail/threads/<enc T>/share {chatIds:[feedId…≤20], messageIds?, note?≤1000}` | Posts a mail card as you. For a person, get or create the DM feedId first (`groups.md`). A 403 `MAIL_SHARE_TARGET_REFUSED` lists `refusals[{chatId,reason}]` |
| Calendar invitations in a thread | `GET /mail/threads/<enc T>/invitations` | Pick one with `replyable:true` |
| ⚠ Answer an invitation | `POST …/invitations/<enc M>/reply {reply:"accept"\|"tentative"\|"decline"}` | Notifies the organizer. _(unverified)_ |
| ⚠ Unsubscribe | `GET /mail/threads/<enc T>/unsubscribe` (offers), then `POST /mail/unsubscribe {threadId,messageId,moveToSpam?}` | May send an unsubscribe mail. _(unverified)_ |
| Ask AI about a thread | `POST /mail/ai/ask {threadId,question,lang:"zh_cn"}`, then poll `GET /mail/ai/tasks/ID` until `state` is no longer `processing` |  |
| AI writing | `POST /mail/ai/tasks {kind:"prompt",lang,prompt}` | `kind:"action"` needs `GET /mail/ai/actions`, which currently returns 500 upstream |

## 7. Settings: signatures, rules, auto-reply, aliases

| Job | Call | Notes |
| --- | --- | --- |
| Read settings | `GET /mail/settings` |  |
| Change settings | `PATCH /mail/settings` | Strict, one or more keys: `conversationMode`, `externalImages`, `undoSendSeconds`, `notificationChannels`, `smartReply`, `strangerInbox`, … |
| ⚠ Auto-reply | `PUT /mail/settings/vacation {enabled,text≤5000,onlyInsideTenant,startsAt?,endsAt? (ms)}` | Replaces the whole setting. Replies to senders automatically |
| Signatures | `GET /mail/signatures`; `POST {name,text}`; `PUT`/`DELETE /mail/signatures/ID`; `PUT /mail/signatures/usage {address,newMailId,replyId}` | Tenant signatures are read-only (409) |
| Rules | `GET /mail/rules`; `POST`/`PUT /mail/rules/ID {name,enabled,matchAll,stopAfter,conditions 1–10,actions 1–10}`; `POST …/ID/enabled`; `DELETE …/ID`; `PUT /mail/rules/order` | Conditions: `from`, `to`, `cc`, `toOrCc`, `anyAddress`, `subject`, `body`, `attachmentName`, `hasAttachment`, `isExternal`. Actions: `archive`, `delete`, `markRead`, `markSpam`, `neverSpam`, `markImportant`, `neverImportant`, `applyLabel`, `flag`, `noNotification`, `moveToFolder`. Rules that forward or send to chat cannot be rewritten |
| Preview a rule | `POST /mail/rules/preview {conditions,matchAll}` | Read-only |
| ⚠ Run a rule on existing mail | `POST /mail/rules/ID/apply` | _(unverified)_ |
| Default address and aliases | `PUT /mail/settings/default-address {address}`, `POST /mail/settings/aliases {address,name?}`, `POST …/aliases/remove {address}` | Aliases must be in `granted` |

Not for agents:

- `/mail/auth-codes` writes need a browser Origin check (403 `CROSS_ORIGIN_WRITE`) and create mailbox passwords.
- `/mail/accounts/bind*` binds third-party mailboxes with passwords.

## 8. Shared mailboxes

`GET /mail/accounts` lists mailboxes with `kind:"shared"`. To read one, add `-H 'Cookie: removed-secret-value'`. The cookie works on labels, threads, messages (including HTML and attachments), search and organize.

Any non-GET mail call with that cookie returns 409 `MAIL_ACCOUNT_READ_ONLY`. This cookie is unrelated to `x-lark-connector-account`, which picks the Lark login.

## 9. Pitfalls

- **Timestamps:**
  - `receivedAt` is ISO.
  - `cursorTimestamp`, `after`, `before`, `newestMs`, `maxTimestampMs`, and the vacation `startsAt` / `endsAt` are milliseconds.
  - `scheduledAt` is ISO with an offset.
- Reading never marks mail read. Use `POST /mail/actions {action:"read"}`.
- Errors: Lark's 4xx comes back as `{statusCode,error,message:"Gateway HTTP 400 for …"}`.
