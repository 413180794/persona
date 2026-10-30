# Chats: finding chats, settings, feed actions, labels

Paths are relative to `/agent/v1`.

- `…` stands for `/conversations/FEED_ID`.
- ⚠ means other people can see the change, or it cannot be undone. Confirm first.
- Group membership and administration live in `groups.md`.

## Contents

1. Find a chat
2. Unread views
3. Chat settings (`PATCH …/settings`)
4. Feed actions and read state
5. Labels and feed groups
6. Favorites, flags, topic groups
7. Batch mute and filters
8. Disambiguation
9. Conversation retention (`…/retention`)

## 1. Find a chat

| Call | Notes |
| --- | --- |
| `GET /conversations?scope=…&limit=1..100&cursor=` | `cursor` is the opaque `nextCursor`. `unreadOnly=true` keeps only chats with `unreadCount>0`. Items carry `feedId`, `name`, `isGroup`, `chatMode` (direct, group or topic), `isMuted`, `isBoxed`, `isDone`, `isDelayed`, `isMarkedUnread`, `isFlagged`, `unreadCount`, `readPosition`, `onTopRankAt`, `memberCount`, `peerUserId`, `isBot`, `isHelpDesk`, `isCalendarGroup`, `isMeetingGroup` |
| `GET /conversations/tail-gaps` | Read-only diagnostic endpoint returning current detected tail gaps: `{gaps:[{feedId,authoritativePosition,localPosition,missing,detectedAt,unresolved}]}` |
| `GET /conversations/FEED_ID` | One chat. **It also pulls the chat into the local store** (as a hidden row). Run it before acting on a feedId that came from search, a link or the list below. Errors: 403 `CONVERSATION_NOT_MEMBER`, 404 `CONVERSATION_DISSOLVED` or `CONVERSATION_NOT_FOUND` |
| `GET /conversations/open-chat/OC_ID` | The same answer as `GET /conversations/FEED_ID`, for a group named by its Open Platform id (`oc_` plus 32 hex digits, as lark-cli and bots use). The response's `feedId` is the id every other route takes. Only groups you have joined are found: it walks `/conversations/groups?kind=join` 50 at a time until one matches, so a miss on a large account takes two gateway calls per 50 groups (measured: 2.4 s for about 240 groups), up to 5 000 groups. A found id is answered from memory afterwards, until the server restarts; a group you have since left then gets the answer `GET /conversations/FEED_ID` gives for it. A miss is kept for one minute, so a group joined in that minute is not found yet. Errors: 400 `INVALID_OPEN_CHAT_ID` (`oc_` plus 32 lowercase hex digits), 404 `CONVERSATION_NOT_FOUND` (not a joined group), plus those of `GET /conversations/FEED_ID`. A gateway refusal during the walk is passed through as Fastify's `{statusCode,error,message}` (for example 400 or 502); it is not a problem with your input |
| `GET /conversations/groups?kind=join\|manage\|administrate&cursor=` | **Every** group you joined, own (`manage`) or administer (`administrate`), 50 per page. This can be far more than the feed shows, often thousands. The response is `{groups:[{feedId,name,description,memberCount,joinedAtMs}],cursor,hasMore,total}`; note `groups` and `cursor`, not `items` |
| `GET …/settings` | Your role (`isOwner`, `isAdmin`), `capabilities`, `labelIds`, `labelOptions`, `chatMenuItems`, `permissions`, `adminUserIds`, `isFrozen` |

`scope` values (an invalid value silently becomes `inbox`):

| scope | Meaning |
| --- | --- |
| `inbox` | Everything except the chat box and folded chats |
| `chats` | The "Messages" tab: inbox filtered by the mute display rule and label display rules |
| `unread` | Unread or marked unread, **excluding** muted, done and boxed chats |
| `muted` | Muted, including auto-muted large groups |
| `done` | Marked done |
| `flagged` | Flagged |
| `mentions` | @me |
| `p2p` | DMs |
| `group` | **Group chats** (not a label) |
| `doc` | Doc chats |
| `thread` | Topics |
| `boxed` | Chat box |
| `later` | Marked "later" |
| `group:<labelId>` | **A label** (feed group) |

To find a chat by name:

1. Page through `scope=inbox` and match `name` case-insensitively.
2. If it is not there, page `/conversations/groups?kind=join`, or use search (`search.md`).
3. Call `GET /conversations/FEED_ID` on the match.
4. If several match, list them and ask.

## 2. Unread views

- `GET /conversations/unread-summary` returns `{mentions,specialFocusFeedIds,totalUnreadCount}`, each wrapped as `{ok,value}`. It fails with 503 only if all three parts fail.
- `GET /conversations/unread-mentions` returns `{conversations:[{feedId,count}],total}`.
- `GET /conversations/unread-special-focus` returns `{feedIds}`.
- `GET /conversations/total-unread-count?includeMuted=true` returns `{count}`.

## 3. Chat settings (`PATCH …/settings`)

The body is strict and needs at least one field. The response is the re-read settings. Fields not listed below return 400 `INVALID_CONVERSATION_SETTINGS`.

| Field | Meaning | Gate |
| --- | --- | --- |
| `isMuted` | Mute | — |
| `isMutedAtAll` | Hide @all for **you only** | `canMuteAtAll` |
| `isAutoTranslate` | Auto-translate this chat | — |
| `isShortcut` | **Pin the chat to the top** of your list | — |
| `isFlagged` | Flag the chat | — |
| `isArchived` | **Mark done** (Done list), not "archive the group" | — |
| `labelIds` | **Replaces** the chat's labels (≤100) | `canManageLabels` (ordinary members have it) |
| ⚠ `name` (1–60), `description` (≤100), `announcement` (≤500) | Group info | `canEditInfo`. Owner or admin by default; any member when the group rule `editInfo` is `all`. A doc-style announcement returns 409 `ANNOUNCEMENT_IS_DOCUMENT`. DMs return 500 |
| `nickname` (≤64) | Your nickname in this group | Groups only |
| ⚠ `isRestrictedMode` | Anti-leak master switch (sub-settings are in `groups.md`) | `canManagePermissions` |
| ⚠ `permissions` | Group rules, any subset. `all` or `managers`: addMember, atAll, chatPin, chatTab, editInfo, pinMessage, topNotice, urgent, userCountVisible, videoConference. `managers`, `all` or `none`: joinMessage, quitMessage. Booleans: addMemberApproval, share. `all` or `new`: historyVisible. Who may post is a separate route: `groups.md` | `canManagePermissions` |

Capabilities (computed from your cached role and the group rules; they can be stale right after a change):

- `canManageMembers` and `canManageAdmins`: you are owner or admin and the group is not frozen.
- `canEditInfo` and `canAddMembers`: follow the group rules.
- `canClearHistory`: the group is not frozen.
- `canLeave`: a group that is not frozen.
- `canUseQrCode`: sharing is allowed and the group is not encrypted.

Auto-translate status: `GET …/auto-translate`. Group-name translation preference: `GET`/`POST …/chat-name-translation`.

## 4. Feed actions and read state

These only change your own list unless marked ⚠.

| Job | Call | Notes |
| --- | --- | --- |
| Mark unread or read | `PUT …/unread {unread:bool}` | ⚠ With `unread:false` and real unread messages, this reports the read position, so the sender sees "read" |
| Chat box | `PUT …/box {boxed}` |  |
| Later | `PUT …/delayed {delayed}` |  |
| Pin to top | `PATCH …/settings {isShortcut:true}`, then optionally `PUT /conversations/pinned-order {feedId,targetFeedId}` | Both chats must be pinned |
| Remove from list | `POST …/remove-from-list` | The chat and its history stay |
| ⚠ Mark **everything** read | `POST /conversations/read-all` | No scope parameter. Includes muted and done chats, sends read receipts, cannot be undone |
| ⚠ Report a read position | `POST …/read-position {maxPosition, messageIds?, maxPositionBadgeCount?}` | Sends read receipts. A missing field returns 500 |
| ⚠ Thread read position | `POST …/read-thread-position {readPosition, threadIds?}` |  |
| Local-only read | `POST …/local-read-position {position?, threadPosition?}` | Clears your badge **without** telling Lark; other devices still show it unread |
| Hover quick action | `GET`/`PUT /feed-quick-action` | UI preference |

How far a "mark read" reaches, smallest first:

1. One chat: `PUT …/unread {unread:false}`.
2. One label: `POST /conversation-labels/ID/clear-unread`.
3. Everything: `read-all`, only when asked.
4. Silent, no read receipts: `local-read-position`.

## 5. Labels and feed groups

Labels ("feed groups") are your private chat folders. None of these routes need extra headers.

| Job | Call | Notes |
| --- | --- | --- |
| List labels | `GET /conversation-labels` | Returns `{items:[{id,name,conversationCount,order}]}`. Use this, not `settings.labelOptions` |
| Chats in a label | `GET /conversations?scope=group:LABEL_ID` |  |
| Create a label | `POST …/labels {name 1–60}` | Puts **this chat** into the new label. Returns `{feedId,groupId,name}`. An empty label cannot be created |
| Add chats in bulk | `POST /conversation-labels/ID/conversations {feedIds:[1–100]}` | Incremental and idempotent. Every feedId must be in the local store, or the whole call returns 404 |
| Set one chat's labels | `PATCH …/settings {labelIds:[…]}` | **Replaces** them: read `settings.labelIds`, edit, write back. `[]` removes the chat from all labels |
| Rename | `PATCH /conversation-labels/ID {name}` | The id must be numeric; `local-label:<uuid>` ids cannot be renamed until synced |
| ⚠ Delete | `DELETE /conversation-labels/ID` | Rules and settings go with it; the chats stay |
| Sort order | `PUT /conversation-labels/ID/order {order:"recent"\|"custom"}` | Custom positions come from the official client |
| ⚠ Mute every chat in a label | `PUT /conversation-labels/ID/mute {muted}` | Up to 500 chats per call. Call again while `remaining>0` |
| Hide @all in a label | `PUT /conversation-labels/ID/mute-at-all {muted}` | Groups only |
| ⚠ Clear unread in a label | `POST /conversation-labels/ID/clear-unread` | Cannot be undone; boxed chats are skipped |
| Display rule | `GET`/`PUT /conversation-labels/ID/display-rule {mainRule:"always"\|"anyNew"\|"specific"\|"never", msgTypes:["atMe","atAll","starContacts"]}` | Whether muted chats in the label show in the `chats` scope |
| Auto-sort rules | `GET`/`PUT /conversation-labels/ID/rules {rules:[{action:"add"\|"remove",match:"all"\|"any",conditions:[…]}]}` | ≤10 rules, ≤20 conditions each. A condition is `{type:"keyword",text≤100}`, `{type:"chatter",chatterId}` or `{type:"chatType",value:p2p\|group\|topicGroup\|helpdesk\|bot\|mute\|flag\|any\|crossTenant}`, each with `negate`. **Saving moves chats right away** |
| Preview rules | `POST /conversation-labels/ID/rules/preview {rules,cursor?}` | Read-only; shows which chats would move |
| Menu counts | `GET /conversation-labels/ID/ops-state` |  |

There is no bulk "remove from label": PATCH each chat's `labelIds`, or save a `remove` rule. The label-ops routes reject the default group.

Bulk tagging:

1. Find the label id with `GET /conversation-labels`, or create one through any chat.
2. `GET /conversations/FEED_ID` for each chat not in the local store.
3. `POST /conversation-labels/ID/conversations` with the batch.
4. Read back with `scope=group:ID`.

## 6. Favorites, flags, topic groups

- Favorites:
  - `GET /favorites?cursor=` returns 50 per page; the cursor is the last item's id.
  - ⚠ `DELETE /favorites/FAVORITE_ID` takes the favorite's own `items[].id`, **not** the messageId. It cannot be undone.
- `GET /flags` returns flagged chats and messages (`{conversations,messages,truncated}`) without pagination.
- `GET /topic-groups?tab=mine|recommended&cursor=` returns 30 per page.
- `GET /info-efficiency` returns daily read and received counts, newest first. Only real `YYYY-MM-DD` dates with both finite, nonnegative safe-integer counts are returned. Missing or malformed statistics omit the entire day; absence means unknown, not zero. Valid zero counts remain zero.
- The messenger title entry honors `show`, including valid zero counts. Its lazy dashboard matches the local calendar date instead of assuming array order, displays missing/invalid reports as unreported, and computes the displayed daily-goal check only from reported `received - read <= 20` values. `read` is the service-reported read count; exact clearing/muting conversion, deduplication and upstream timezone are not independently established. The UI's monthly count covers reported dates only. No new Agent API route or field is introduced.
- The dashboard uses the existing paged unread conversation scope for cleanup. Filters and selections apply only to loaded pages; each confirmed selected id calls `PUT /conversations/ID/unread {unread:false}` once. This can report read positions/read receipts, as documented above. Permission/transport failures stay visible and are not retried automatically; a failed response can still be ambiguous, so inspect state before a manual retry.
- The dashboard plots both reported read and received counts; calendar rings use their ratio while the daily goal continues to use the reported difference threshold. No department/company benchmark or last-read date is inferred. Its separate mute selection contains only loaded unread chats that are not already muted, automatically muted, or document feeds. Confirmed ids call the existing `PATCH /conversations/ID/settings {isMuted:true}` once each; this changes notifications and does not mark messages read. Partial failures remain visible and are not automatically retried. No Agent API route, field, or permission changes.

## 7. Batch mute and filters

Batch mute by category:

- `GET /feed-filters/:filter/mute` shows the count first.
- ⚠ `POST /feed-filters/:filter/mute {muted}` runs synchronously over up to 20 000 chats and keeps running after a client timeout.
- `:filter` is one of `docs`, `groups`, `bots`, `helpDesk`, `topicGroups`, `threads`, `calendarGroups`, `meetingGroups`.
- `muted:false` also undoes mutes you set by hand. Confirm before running.

Other filter routes:

- `GET …/auto-mute` explains why a chat is auto-muted; `POST …/auto-mute/cancel {botId?}` undoes it.
- `PUT …/bot-receiving {stopReceiving}` works on bot DMs only.
- UI state: `GET`/`PUT /feed-filter-setting {usedFilters}` (filter-bar ids), `/feed-expired-fold`, `/navigation/*`, `/app-feed-cards/:id/mute`, `/message-folds/*`.

Filter-bar id to scope:

| Id  | Scope        |
| --- | ------------ |
| 1   | `mentions`   |
| 2   | `unread`     |
| 3   | `later`      |
| 4   | `doc`        |
| 5   | `p2p`        |
| 6   | `group`      |
| 10  | `done`       |
| 12  | `muted`      |
| 13  | `group:<id>` |
| 15  | `flagged`    |
| 16  | `thread`     |
| 26  | `boxed`      |

bots, helpDesk, topicGroups, calendarGroups and meetingGroups have no scope; filter `inbox` by the item flags instead.

## 8. Disambiguation

- **"Remove / delete this chat"** could mean five things. Ask which one:
  1. Mute (`isMuted`).
  2. Done (`isArchived`).
  3. Box (`PUT …/box`).
  4. Remove from list (`remove-from-list`).
  5. ⚠ Leave the group (`groups.md`).
- **"Pin"** could mean three things:
  1. Pin the chat to the top of your list: `isShortcut`.
  2. Pin to the group's top bar: `top-pins` in `groups.md`.
  3. Pin a message: `messaging.md`.

  Each has its own ids.

## 9. Conversation retention (`…/retention`)

- `GET /conversations/FEED_ID/retention`: returns `{feedId, customSetting, effectiveRetention}`. `customSetting` is `{retentionDays, updatedAt}` or `null` (following global rules). `effectiveRetention` contains `{retentionDays, source, sourceDescription, ruleName}`.
- `PUT /conversations/FEED_ID/retention`: sets or clears individual retention duration.
  - Body: `{retentionDays?: number | null, confirm?: boolean}`. Pass `retentionDays: null` for permanent retention, or omit to follow global rules.
  - Shortening effective retention duration requires `confirm: true`. Without `confirm: true`, the call returns HTTP 409 `CONFIRM_REQUIRED` with a dry-run `preview` report (including optional `estimate: true`, `partial: true`, `countedFeeds`, and `targetFeeds` markers).

### Local unread and chat applinks

- `PUT /conversations/F/local-unread` with no body or `{}` restores the read cursor saved before the last read that consumed unread messages. New arrivals since that snapshot remain unread.
- With `{position:"110"}`, it places the local cursor at `109`, counting cached non-deleted incoming messages at or after `110`. The target must exist in this chat, not be deleted, and not exceed its tail. Positions must be positive safe-integer decimal strings (at most 9007199254740991).
- Returns `{feedId,readPosition,unreadCount}`. A malformed body or position returns 400 `INVALID_POSITION`; no snapshot, absent target, topic chat, or missing badge metadata returns 409 `LOCAL_UNREAD_UNAVAILABLE`. Conversation summaries expose optional `canRestoreUnread`.
- These operations change only lark-connector's state, never Feishu's cursor. The web viewer holds read reports until leaving the marked chat; failed requests release that hold. Feed sync does not clear the local override, and partial local reads preserve the remaining unread range until reaching the tail. Already-running receipt transitions complete before local unread is applied.
- `GET /link-targets/resolve?url=...` accepts supported Feishu/Lark `/client/chat/open` links with `chatId`/`chat_id` or `openChatId=oc_...` and optional positive safe-integer `position`. It resolves the visible message at that position, falling back to the chat if none is visible. The optional Chrome extension is configured separately; it redirects only supported chat paths on production Feishu/Lark applink hosts, not login or unsupported paths.
