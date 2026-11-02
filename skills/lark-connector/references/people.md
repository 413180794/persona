# People, org chart, contacts, teams

Paths are relative to `/agent/v1`. ⚠ marks a write others can see, or one that cannot be undone; confirm it first. _(unverified)_ marks a write the code says was never run against the real Lark gateway; read the result back.

## Contents

1. Find a person
2. Profiles, managers, departments
3. Contacts: frequent, external, friend requests, blocks
4. Your own touches: alias, special focus, avatar
5. Teams
6. Pitfalls

## 1. Find a person

1. Run `ol GET /search/people --url-query 'q=张三' --url-query limit=10`. The response is `items[{id,name,subtitle,type}]`.
   - The fields are `id` and `name`.
   - Pick the right person by `subtitle` (department). Skip personal accounts (subtitle "飞书个人用户").
   - When more than one match remains, list them and ask.
2. If you already know the chat, member lists are more precise: `GET /conversations/F/members/search?q=` (see `groups.md`).
3. To look up someone outside the company by email or phone, use `GET /contacts/search-by-contact-point --url-query 'q=a@b.com'`. It accepts one email, or one phone number of 5–20 digits.
   - It returns `{kind,matches[{userId,name,company,token,canAdd,isFriend,requested}]}`.
   - **It returns nothing for colleagues in your own tenant**, so use people search for them.

## 2. Profiles, managers, departments

| Call | Notes |
| --- | --- |
| `GET /home/user[?feedId=F]` | Profile card. Fields:<br>`displayName`, `enName`, `email`, `description` (signature), `timezone`, `status`, `workStatus`<br>`fields[]`: `B-DEPARTMENT`; `B-LEADER`, whose url's `uid=` is the manager's id; `B-EMAIL`<br>`relation{friendStatus,external,canAddContact,contactToken,memo}`<br>Pass `feedId` to also get the person's group nickname.<br>**It does not accept `me`.** Get your own id from `/me` |
| `POST /home/user {userIds:[1–200]}` | Profiles in bulk. Unknown ids are skipped |
| `POST /home/user {userIds≤200}` | Returns `{resigned:[…]}`, cached for 6 h |
| `GET /people/USER_ID/user-page` | `managerId`, `reportLine[]`, `reportLineState` (`ok`, `empty`, `hidden`, `pending`), `departments[].nodes[{id,name}]`, `subordinates`, `team` |
| `GET /people/USER_ID/report-line` | The chain from the top down to the direct manager, at most 12 levels. Use it when `user-page` says `pending` |
| `GET /people/USER_ID/team?cursor=` | **The person's reports**, not their team. 20 per page, with direct, dotted-line and second-level counts |
| `GET /contacts/org` | Root departments and visibility |
| `GET /contacts/departments/DEPT_ID?count=1..100&offset=` | Members and sub-departments. `0` is the virtual root. Page with `offset += count` while `hasMore`; there is no next field. Parent departments are often hidden |

To list everyone in a department, page through its members and then recurse into each `subDepartments[]` entry.

In the web client, clicking a direct chat's header avatar or name opens that person's profile card. The card's OKR field “View details” action resolves the Lark user ID to an OKR owner ID and opens the local `/okr/user/OWNER_ID` page. The field's upstream URL remains available in the profile API response; these UI changes do not alter `/agent/v1` routes.

## 3. Contacts: frequent, external, friend requests, blocks

| Job | Call | Notes |
| --- | --- | --- |
| Frequent people / chats | `GET /contacts/closest/people?offset=`, `GET /contacts/closest/chats?offset=` | 20 per page; follow `next`. For chats, `chatId` is the feedId |
| External contacts | `GET /contacts/external?offset=` | `{items[{userId,name,alias,company,addedAt}],total,next?}` |
| Friend requests I received | `GET /contacts/friend-requests?cursor=` | `{items[{id,userId,name,company,message,status}],nextCursor?}` |
| ⚠ Send a friend request (from a search) | `POST /contacts/friend-requests {kind:"email"\|"phone",userId,token,message?≤200}` | Returns `{status:"sent"}`, or `{status:"notice",notice}` when nothing was sent; relay the notice. _(unverified)_ |
| ⚠ Send a friend request (from a profile) | `POST /contacts/home/user/friend-request {token:relation.contactToken,message?}` | _(unverified)_ |
| ⚠ Accept / ignore | `POST /contacts/friend-requests/ID/accept {userId}`, `POST /contacts/friend-requests/ignore {userId}` | Ignore works per person, not per request |
| ⚠ Block / unblock | `GET`/`PUT /contacts/home/user/block-status {blocked}` | Blocking stops their messages. The blocklist is in `platform.md` |
| ⚠ Delete a contact | `DELETE /contacts/home/user/contact` | Cannot be undone. Becoming contacts again needs a new request and their approval |
| Private memo | `PUT /contacts/home/user/memo {memo≤500}` | Only you can see it |
| My add-friend link | `GET /contacts/my-link`; ⚠ `POST /contacts/my-link/reset` | Reset invalidates every old link and QR code |
| ⚠ Look up a phone number | `POST /home/user/phone` | **May notify the person and uses a daily quota.** Returns `{phone?,todayCount?,status}`. _(unverified)_ |
| Bot directory, related orgs | `GET /contacts/bots?offset=`, `GET /contacts/related-orgs` | Often empty, or hidden by tenant settings |
| Mail name cards | `GET /contacts/namecards`, `GET /contacts/namecards/ID` | These are the **mail** contact book, not IM cards |

## 4. Your own touches: alias, special focus, avatar

| Job | Call | Notes |
| --- | --- | --- |
| Alias for someone | `PUT /home/user/alias {alias≤60 \| null}` | Only you see it. Verified live |
| Special focus | `GET /contacts/special-focus`, `…/ids`; `PUT`/`DELETE /contacts/special-focus/USER_ID`; `POST /contacts/special-focus {userIds:[1–100]}` | Changes your notifications only |
| ⚠ Avatar | `GET /profile/avatar/permission`, then `POST /profile/avatar {data:<base64 or data URL, JPEG/PNG, ≤5 MB>}` | Everyone sees it, and the old image cannot be restored |
| ⚠ Profile background | `POST /profile/background {data}` | JPEG/PNG, ≤5 MB |

头像保存成功后会清除当前账号所有上下文的服务端个人资料缓存；随后读取 `GET /home/user` 触发重新拉取，`GET /me` 仍使用本地投影。

头像排障：Web 编辑器在权限查询失败时显示错误并提供重试，权限未确认或刷新失败时禁止保存；重试只查询权限，不重放头像写入。保存错误会保留弹窗和所选图片。客户端兼容嵌套错误、Fastify 顶层 `message/code` 和字符串错误，避免只显示 HTTP 状态。若仍失败，收集受影响实例的版本、时间、权限查询和头像 POST 的 HTTP 状态与响应、服务端 `PULL_USER_UPDATE_FIELD_PERMISSION` / `PATCH_CHATTER` 错误；不要用真实头像写入作探测。此链路更改当前登录账号的头像，不是应用机器人头像管理接口。现有网关写入协议未通过本次真实写入验证，不能仅凭诊断修复断言所有账号都能成功换头像。

Signature, status and do-not-disturb are covered in `platform.md`.

## 5. Teams

In team responses, a chat's `id` is its feedId. Write permission depends on `role` (owner, admin, member) and on `settings.*` (`all` or `managers`).

| Job | Call | Notes |
| --- | --- | --- |
| My teams | `GET /teams?cursor=` | 20 per page. **An invalid cursor silently restarts at page 1**, so rely on `hasMore` |
| Team detail and team groups | `GET /teams/TEAM` | `chats[{id,joined,visibility,discoverable,isDefault,memberCount}]`, `defaultChatId` |
| Members | `GET /teams/TEAM/members?cursor=` | Direct members, people and groups, 50 per page |
| People | `GET /teams/TEAM/people?cursor=` | Groups expanded into individual people |
| Activity | `GET /teams/TEAM/events?cursor=` |  |
| Name check | `GET /team-name-check?name=&teamId?` |  |
| ⚠ Join a team group | `POST /teams/TEAM/chats/CHAT/join` | The group sees you join |
| ⚠ Create a group in the team | `POST /teams/TEAM/chats {name,privacy:"open"\|"private",discoverable,addTeamMembers,operationId:<UUID>}` | `addTeamMembers:true` adds the whole team |
| ⚠ Create a team | `POST /teams {name,description?,operationId}` | Also creates a default topic group |
| ⚠ Edit / dissolve / leave | `PATCH /teams/TEAM {name?,description?}`, `POST /teams/TEAM/dissolve`, `POST /teams/TEAM/leave {newOwnerId?}` | Dissolving cannot be undone. An owner must hand over before leaving |
| ⚠ Owner, members, admins | `PUT /teams/TEAM/owner {ownerId}`, `POST /teams/TEAM/members {userIds≤200,chatIds≤200}`, `POST …/members/remove`, `PUT …/admins {add,remove}` | Adding a group adds everyone in it |
| ⚠ Team groups | `PUT …/chats/CHAT/privacy {privacy,discoverable}`, `POST …/chats/CHAT/unbind`, `POST …/chats/CHAT/bind {privacy,discoverable,accepted?}` | Bind runs in two steps. The first call may return `bound:false` together with the people who would be added; repeat it with `accepted` to finish |

`operationId` makes a call idempotent, but only within one server process for 10 minutes. Do not rely on it across restarts.

## 6. Pitfalls

- Ids in Moments are Moments identities. Anonymous authors' ids do not work with `/users/:id`.
- Each route paginates differently:
  - Departments: `offset` + `hasMore`.
  - Closest and external contacts: `next`.
  - Friend requests: `nextCursor`.
  - People team: an opaque `cursor`.
  - Teams: a numeric `cursor`.
