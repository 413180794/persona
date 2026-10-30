# Groups: create, members, invites, permissions, pins, bots

Paths are relative to `/agent/v1`.

- `…` is `/conversations/FEED_ID`.
- ⚠ means visible to others or cannot be undone. Confirm first.
- _(unverified)_ means the code says this was never run against the real Lark gateway. Read the result back.
- Most routes need the chat in the local store, so call `GET …` first.
- Check `GET …/settings` `capabilities` and `isOwner`/`isAdmin` before admin writes.

## Contents

1. Create DMs and groups
2. Members
3. Admins, owner, leave, dissolve
4. Invites, QR codes, links, join requests
5. Group permissions and appearance
6. Top-bar pins, tabs, menus, bots
7. Group files, tasks, events, threads, mail
8. Who may do what

## 1. Create DMs and groups

| Job | Call | Notes |
| --- | --- | --- |
| DM with someone | `POST /conversations {"type":"p2p","userIds":["USER_ID"]}` | Returns `{created,feedId}`. An existing DM comes back with `created:false`, so this is safe to call to look up a DM |
| ⚠ Group | `POST /conversations {name 1–60, description? ≤100, userIds ≤100}` | Returns `{created,feedId,name}`. Notifies the people added |
| ⚠ Group, full options | `POST /groups {name, description?, isPublic?, mode:"default"\|"topic", userIds ≤100, chatIds ≤20 (whole groups), departmentIds ≤20 (whole departments), fromChatId? (upgrade a DM and carry its history)}` | _(unverified)_ |
| Is a public name taken | `GET /groups/public-name?name=` | Returns `{taken}` |

**Never auto-retry group creation.** Each call creates a new group. After a timeout, look at `GET /conversations/groups?kind=manage` to see whether it was created.

## 2. Members

| Job | Call | Notes |
| --- | --- | --- |
| List | `GET …/members?limit=1..10000` | Default 100, no cursor, slow on big groups. Returns `items[{userId,displayName,groupNickname,role (1 owner, 2 admin),…}]` |
| Search members | `GET …/members/search?q=&limit≤200` | Prefer this for big groups |
| ⚠ Add people | `POST …/members {userIds:[1–100]}` | Needs `canAddMembers`. Returns the number requested, not the number actually added |
| ⚠ Add people, groups, departments | `POST …/members/entities {userIds≤100, chatIds≤20, departmentIds≤20}` | _(unverified)_ |
| ⚠ Kick | `DELETE …/members/USER_ID` | Needs `canManageMembers`; 403 otherwise. See the note below about `?withdraw=true` |
| Member cap | `GET …/member-limit` | Returns `{limit (0 = unknown), allowApply}` |
| Export members to Base | `POST …/members/export-bitable` | Needs `canManageMembers`. Returns `{accepted:true}`; Lark delivers the result and there is no route to poll |

**About `?withdraw=true` on kick.** Older docs called it "kick and recall their messages". The evidence points elsewhere:

- The official client sends `withdraw` only when it revokes a join invitation.
- No test confirms message recall.

Do not promise that it recalls messages. To undo an invitation, use the revoke flow in section 4. No route recalls another member's messages in bulk; a group admin recalls one message with `POST /messages/M/group-recall`.

## 3. Admins, owner, leave, dissolve

| Job | Call | Notes |
| --- | --- | --- |
| ⚠ Make or remove admin | `POST`/`DELETE …/admins/USER_ID` | Needs `canManageAdmins` |
| ⚠ Transfer owner | `POST …/owner {userId}` | Owner only. A non-owner gets **500** "Only the group owner…", not 403. Cannot be undone |
| ⚠ Leave | `POST …/leave` with optional `{newOwnerId}` | **The owner must pass a successor**, or the call fails with 500. Needs `canLeave` |
| ⚠ Dissolve | `POST …/dissolve` with optional `{keepHistory?}` (`true` freezes the group and keeps history readable) | Owner only. Notifies everyone and cannot be undone. The UI asks for the group name; the server does not, so you must confirm. _(unverified)_ |
| ⚠ Clear chat history | `POST …/clear-history` | Returns `{deletedCount}` of local rows. Cannot be undone |

## 4. Invites, QR codes, links, join requests

| Job | Call | Notes |
| --- | --- | --- |
| ⚠ Invite QR or link | `GET …/qr-code?expiredDay=1..365&unlimited=true` | Default 7 days. Returns `{token,sharedUrl,expireTime,isUnlimited}`. **The link is a credential**; anyone holding it can join. Full token only |
| Preview a Lark link (read-only) | `GET /link-targets/resolve?url=` | Returns `{href:"/messenger/FEED?message=&position=&thread="}` or `{reason: not-member\|not-found\|unsupported\|no-access, chatId?, preview?}`. Does not join |
| ⚠ Scan a QR or group link | `POST /scan {content≤4096}` | **A group code or join link joins you immediately**, and the group sees it. Returns `{kind,feedId?,joined?,alreadyInChat?}`. Preview with `resolve` first |
| Calendar or task link | `GET /link-targets/calendar-event?calendarId=&key=&originalTime=(s)`, `GET /link-targets/task/GUID` |  |
| Join requests | `GET …/join-requests?cursor=` | Returns `items[{inviteeId,status}]`. A non-admin gets Lark's 400 passed through |
| ⚠ Approve or refuse | `POST …/join-requests/INVITEE_ID {decision:"approve"\|"refuse"}` | Owner or admin. _(unverified)_ |
| Join and leave history, share history | `GET …/join-leave-history`, `GET …/share-history` (20 per page) | Owner or admin; others get Lark's 400, which means no permission rather than bad input |
| ⚠ Disable a share link | `POST …/share-history/deactivate {id}` | id from share-history. Cannot be undone |
| ⚠ Revoke an invitation | `GET …/revocable-invitations`, then `POST …/revocable-invitations/USER_ID/revoke` | Removes that person from the group. 409 `INVITATION_NOT_REVOCABLE` |

A Feishu sign-in QR payload such as `{"qrlogin":{"token":"…"}}` returns only `{kind:"qr-login"}` from `POST /scan`. It does not scan or confirm the login upstream, mark the other device as scanned, or echo the token. Direct the user to scan and confirm with the official mobile app; do not treat this result as a successful login. Invalid lookalike payloads remain ordinary text. Group invitation codes still join immediately as described above.

## 5. Group permissions and appearance

Basic rules (who can add members, edit info, @all, pin, and so on) are `permissions` in `PATCH …/settings` (see `chats.md`).

| Job | Call | Notes |
| --- | --- | --- |
| ⚠ Who may post | `GET`, then `PUT …/post-permission {audience:"all"\|"managers"\|"allowlist", baseUserIds, userIds}` | Strict. Both lists ≤500 and both required. Put the current list (from GET) in `baseUserIds` so the server can rebase |
| ⚠ Anti-leak sub-settings | `GET`/`PATCH …/restricted-mode` with blockCopyForward, blockDownload, blockScreenshot, `selfDestruct{enabled,aliveSeconds}`, `allowlist{level,baseUserIds,userIds}` | The master switch is `isRestrictedMode` in settings. 409 `RESTRICTED_MODE_UNAVAILABLE` |
| ⚠ Public or private | `PUT …/group-type {isPublic}` | Public groups are searchable company-wide. Check `GET …/group-settings-extras` (`groupType.canChange`) |
| ⚠ Department auto-join | `POST …/auto-join-rules {departmentId,employeeTypes}`, `PATCH`/`DELETE …/auto-join-rules/RULE_ID` | Rules add people in bulk. Only `editable` rules can be changed |
| ⚠ Avatar | `GET`, then `PUT …/avatar` (JSON): `{kind:"preset",presetKey}`, `{kind:"words",color,text,image}` or `{kind:"upload",image}` | `image` is bare base64 PNG/JPEG, 16 to 2 800 000 chars. Needs `canEditInfo`. _(unverified)_ |
| Remove a linked page | `POST …/linked-pages/delete {url}` | `url` must match `settings.linkedPages[].url` exactly |
| ⚠ Edit the group menu | `GET`, then `PUT …/chat-menu` | Replaces the whole tree (≤50 items per level, ≤5 levels, ≤500 nodes). Reading also needs admin. _(unverified)_ |

## 6. Top-bar pins, tabs, menus, bots

Group top-bar pins and message pins are separate systems with separate ids.

| Job | Call | Notes |
| --- | --- | --- |
| Top-bar list | `GET …/top-pins?limit=1..20` | Returns `items[{id,messageId?,type,title,url?}]`. Empty in topic groups |
| ⚠ Pin a message to the top bar | `POST …/top-pins {messageId}` | Needs `canManageChatPins`. A full bar returns Lark's error |
| ⚠ Pin a link | `POST …/top-pins/link {url,title?≤60,replaceOldestPin?}` | 409 `CHAT_PIN_LIMIT_REACHED`. `GET …/chat-pins/oldest` shows what would be replaced |
| Reorder, rename, unpin | `PUT …/top-pins/PIN/order {clientPinIds,prevPinId?}`, `PATCH …/top-pins/PIN {title}` (links only), `DELETE …/top-pins/PIN` |  |
| ⚠ Delete a pin; announcement to pin | `DELETE …/chat-pins/PIN`, `POST …/chat-pins/announcement` |  |
| Message-pin list | `GET …/pins?limit=&cursor=` | Its ids feed `DELETE /messages/M/pin`. **This read clears the unread-pin mark** |
| Tabs | `GET …/chat-tabs`; `POST …/chat-tabs {name≤60,url}`; `PUT …/chat-tabs/order {tabIds}`; `PATCH`/`DELETE …/chat-tabs/TAB` | Writes need `canManageChatTabs`. Built-in tabs return 404 `CHAT_TAB_NOT_EDITABLE` |
| ⚠ Press a menu button | `POST …/chat-menu/MENU_ID` | `MENU_ID` comes from `settings.chatMenuItems[].id`. Triggers the app's action |
| Bots | `GET …/bots`; `GET …/bot-candidates?q=`; ⚠ `POST …/bots {botId}` | Adding a bot notifies the group. _(unverified)_ |
| Bot DM menu | `GET …/bot-menu`; `POST …/bot-menu/trigger {path,version?}` | Only `callback` items trigger. _(unverified)_ |
| Slash commands, widgets, recommended pins | `GET …/slash-commands`, `GET …/widgets`, `GET`/`POST …/recommended-pins` | UI helpers |

## 7. Group files, tasks, events, threads, mail

| Job | Call |
| --- | --- |
| Files, docs, media shared in the group | `GET …/resources?kind=docs\|files\|media&cursor=&q=` (20 per page; bad params return 500). `downloadUrl` starts with `/api`; rewrite it |
| Group tasks | `GET …/todos` returns `{open,closed}`; `GET …/todos/completed?offset=` |
| Events linked to the group | `GET …/calendar-events` |
| Meeting minutes tab, live meeting | `GET …/meeting-minutes`, `GET /chats/FEED_ID/live-meeting` |
| Free-busy favourites | `GET`/`PUT /chats/FEED_ID/free-busy-favor {userIds≤100}` (yours only) |
| Threads | `GET …/threads?beforePosition=&limit≤100` (page with `nextOlderCursor`) |
| Threads you follow | `GET …/threads/following` returns `{items}`: the topic roots in your Lark follow list for this chat, including ones not loaded locally |
| ⚠ Forward a thread | `POST …/threads/THREAD_ID/forward {feedIds}` |
| Follow a thread, change its reminder | `PATCH /threads/THREAD_ID/settings {follow?,remind?}` |
| ⚠ Close or reopen a thread | `PATCH /threads/THREAD_ID/state {closed}` |
| Thread status | `GET /threads/settings?ids=`, `GET /threads/states?ids=` (≤200, comma-joined) |
| Group mailbox | `GET …/mail`, `GET …/mail-address`; ⚠ `POST …/mail/address`, `PUT …/mail/permission {level}` (owner or admin; _(unverified)_) |

## 8. Who may do what

| Gate | Routes |
| --- | --- |
| Owner or admin role | kick, admins, export-bitable, join requests (read and decide), share history and deactivate, revoke invitations, linked pages, chat menu (read too) |
| Group rules (`canEditInfo`, `canAddMembers`); `all` lets ordinary members in | Group name, description, announcement, add members, avatar |
| `canManagePermissions` | `permissions`, post-permission, restricted-mode, group-type |
| Owner only | Dissolve, transfer owner |

Capabilities come from a cache and can be stale for a moment after a role change. A 403 means not allowed. A 500 from owner-only routes usually means you are not the owner, or the chat is not in the local store.
