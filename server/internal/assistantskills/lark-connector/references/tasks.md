# Tasks: todos, task lists, OKR

Paths are relative to `/agent/v1`.

- ⚠ = visible to others or irreversible. Confirm first.
- _(unverified)_ = the code says this was never run against the real Lark gateway. Read the result back.
- The approval center is in `approvals.md`.

## Contents

1. Read todos
2. Create and change todos
3. Comments, attachments, subtasks
4. Task lists
5. OKR (read-only)
6. Pitfalls

## 1. Read todos

| Call | Notes |
| --- | --- |
| `GET /todos?count=1..200` | **Only open tasks where you are an assignee.** No cursor. An invalid count falls back to 50 |
| `GET /todos/completed`, `GET /todos/documents` | Your completed tasks; checkbox todos inside docs |
| `GET /todos/search?q=&token=` | Title search across every role. 30 per page; returns `tasks[…, roles:[assignee\|assigner\|creator\|follower], taskLists, parentSummary]`, `nextToken` |
| `GET /link-targets/task/GUID` | **The full task** (TodoView). There is no `GET /todos/GUID`. A deleted task is 404 `TASK_NOT_FOUND` |
| `GET /todos/GUID/detail` | Metadata only, no summary. Fields: `num`, `appLink`, `creatorId`, `completeMode` (task/user), `lists[]`, `attachments[].token`, `viewerIsAssignee`, `canEdit*` |
| `GET /todos/GUID/subtasks` | Fixed at 50 |
| `GET /todos/GUID/history?cursor=`, `GET /todos/GUID/activity?cursor=` | Change log and activity, 30 per page |
| `GET /todos/GUID/references` | The messages the task came from |
| `GET /todos/GUID/custom-fields` | Field definitions and values |
| `GET /task-activity?cursor=` | Your task feed |
| `GET /conversations/FEED_ID/todos` | A group's tasks (`groups.md`) |

Task fields (TodoView): `guid`, `summary`, `description`, `dueAt`, `startAt`, `isAllDay`, `remindAt`, `assignees[].id`, `followers`, `completed`, `canEditAssignees`.

## 2. Create and change todos

⚠ **Create.** `POST /todos` returns the TodoView with `guid`. Assigning someone else notifies them.

- `summary`: required, 1–3000 chars.
- `assigneeIds`: up to 50 numeric userIds. `[]` assigns the task to you.
- `description`: up to 10000 chars.
- `dueAt`, `startAt`, `remindAt`: ISO **with an offset or Z**.
- `isAllDay`: for an all-day task, set `dueAt` to that day's UTC midnight, e.g. `2026-10-20T00:00:00Z`.
- `parentGuid`: makes this a subtask.
- `reminderOffsets`: minutes. More than one returns 500, so use compose for several.
- `rrule`: `FREQ=…` with optional UNTIL, COUNT, INTERVAL, BYDAY or BYMONTHDAY.
- `sourceMessage`: `{chatId, messageId}` turns a message into a task.
- Every validation error returns 400 `INVALID_TODO` "summary is required", even when summary is fine.

**Compose.** `POST /todos/compose` takes the same fields, plus:

- `followerIds`, up to 50.
- `mergeResource`: `{chatId, messageIds[1..100]}` or `{threadId}`, to merge messages into one task.

Use compose for followers or several reminders. It returns the first validation issue as 400.

**Change fields.** `PATCH /todos/GUID/fields` sends only the fields you change:

- Accepts `summary`, `description`, `assigneeIds`, `followerIds`, `dueAt`, `startAt`, `remindAt`, `reminderOffsets`, `rrule`, `isAllDay`.
- `null` clears `description`, `dueAt`, `remindAt`, `startAt`, `rrule` or `reminderOffsets`.
- **`assigneeIds` and `followerIds` replace the whole list**, and `[]` here clears it (the opposite of create).
- 400 `INVALID_TODO_TIMES` when start is after due. 403 when you may not change assignees.

**Complete.** ⚠ `POST /todos/GUID/complete {completed, scope:"self"|"all"|"assignee", assigneeId?}`:

- Check `detail.completeMode` first. When it is `user`, each assignee completes separately, so use `self`.
- `assignee` completes the task for one assignee and needs `assigneeId`.
- 403 `TODO_FORBIDDEN` when you are not allowed.

**Other writes:**

| Job | Call |
| --- | --- |
| ⚠ Delete (affects every member; no permission pre-check) | `DELETE /todos/GUID` |
| Follow or unfollow | `POST /todos/GUID/follow {follow}` |
| Set assignees | `PUT /todos/GUID/owners {assigneeIds 1–50}` |
| Make it a subtask, or detach it | `PUT /todos/GUID/parent {parentGuid\|null}` |
| Milestone | `PUT /todos/GUID/milestone {milestone}` |
| Leave the task | `POST /todos/GUID/exit` |
| Add to a list (lands in the default section) | `POST /todos/GUID/lists {listGuid}` |
| Custom field | `PUT /todos/GUID/custom-fields/KEY {value}`. Value is `null` or `{type:"number",number}`, `{type:"datetime",at}`, `{type:"singleSelect",optionGuids:[1]}`, `{type:"multiSelect",optionGuids}`, `{type:"text",text}`. Member fields cannot be written |
| Reorder subtasks | `PUT /todos/GUID/subtask-order` |

`PATCH /todos/GUID` is the old route. It takes either `{completed}` or fields, and when `completed` is present the other fields are silently dropped. Prefer `/fields` and `/complete`.

## 3. Comments, attachments, subtasks

| Job | Call | Notes |
| --- | --- | --- |
| Read comments | `GET /todos/GUID/comments?position=` | 50 per page, oldest first; `nextPosition` |
| ⚠ Comment | `POST /todos/GUID/comments {text≤10000, replyToId?, replyToName?, mentions?[{name,userId}]≤50, imageKeys?≤12}` | Each mention needs `@name` in `text`. For images, upload with `POST /image-uploads` first (`messaging.md`) |
| Edit or delete your comment | `PATCH`/`DELETE /todos/GUID/comments/CID` | Your own comments only. Edits keep the old mentions |
| React | `POST`/`DELETE /todos/GUID/comments/CID/reactions/THUMBSUP` |  |
| Attach a file | `POST /todos/GUID/attachments --upload FILE --url-query name=…` | ≤20 MB |
| Remove an attachment | `DELETE /todos/GUID/attachments/TOKEN` | Token from `detail.attachments[].token` |
| Subtask | `POST /todos` with `parentGuid` |  |

Comment image urls are `/api/media/KEY`. Fetch them at `/agent/v1/media/KEY`.

## 4. Task lists

| Job | Call | Notes |
| --- | --- | --- |
| My lists | `GET /task-lists` | Up to 500 unarchived lists. Returns `{guid,name,isOwner,memberCount,canEdit,canEditTasks}` |
| Find a list | `GET /task-list-nav/search?q=` (includes archived), `GET /task-list-nav/tabs/all\|created\|collaborated\|chats\|docs\|example-cli`, `GET /task-lists/archived` |  |
| List content | `GET /task-lists/GUID` | Returns `sections[{guid,isDefault,name,todos[],total}]` and `truncated`. Loads at most 300 tasks. Missing list: 404 `TASK_LIST_NOT_FOUND` |
| Counts, members, activity | `GET /task-lists/counts?guids=`, `GET …/members` (first 100), `GET …/member-access`, `GET …/activity?cursor=` (numeric cursor) |  |
| Create, rename | `POST /task-lists {name}`, `PATCH /task-lists/GUID {name}` |  |
| ⚠ Delete a list | `DELETE /task-lists/GUID?removeUnassignedTasks=true` | Affects every member |
| Archive | `POST /task-lists/GUID/archive {archived}` | Reversible |
| Sections | `POST …/sections {name}`, `PATCH`/`DELETE …/sections/SECTION` | The default section cannot be changed |
| Task in a list | `POST …/tasks {summary, sectionGuid?}` | Assigned to you only, no due date. For anything richer, create with `POST /todos`, then `POST /todos/GUID/lists` |
| Move or reorder | `POST …/tasks/TASK/move {sectionGuid}`, `POST …/tasks/TASK/reorder {position:"before"\|"after", sectionGuid, targetGuid}` |  |
| ⚠ Invite | `POST …/members {members:[{id,kind:"user"\|"chat"}], role:"editor"\|"viewer", sendNote, note?}` | Notifies the people invited. _(unverified)_ |
| ⚠ Change a role or remove | `PUT …/member-roles {kind, memberId, role:"editor"\|"viewer"\|"inherit"\|"none"}` | `none` removes the member. The owner cannot be transferred |
| ⚠ Activity rules (push to a group) | `GET`/`POST`/`PATCH`/`DELETE …/activity-rules` | Posts into the group |
| Views, gantt, icon, dashboard | `…/views`, `…/gantt`, `PUT …/icon`, `…/dashboard` | UI. A dashboard creates a Base doc |
| Sidebar | `GET /task-list-nav/sidebar`; `POST /task-list-nav/pin\|unpin\|move`; `/task-list-nav/sections` | Your own nav only |

There is no route that removes a task from a list.

## 5. OKR (read-only)

OKR ids are not Lark userIds.

1. Find the owner id:
   - Yourself: `me`.
   - People around you: `GET /okr/people` returns `{self, leader, peers, underlings}`, each with `id`.
   - Anyone: `GET /okr/resolve?larkUserId=&q=<name>` returns `{ownerId}`.
2. `GET /okr/owners/OWNER/periods` returns `{currentOkrId, periods[{okrId,name,startTime,endTime (ms)}]}`.
3. `GET /okr/OKR_ID` returns `objectives[{id,text,progress,score,keyResults[{id,…}],alignedTo,alignedFrom}]`.
4. Progress notes: `GET /okr/progress/objective/ID` or `GET /okr/progress/kr/ID`.

The bundled MCP has matching read tools: `list_okr_people`, `resolve_okr_owner` (`larkUserId`, optional single `keyword`), `list_okr_periods` (`ownerId`, including `me`), `get_okr_detail` (`okrId`) and `get_okr_progress` (`target`, `id`). Each accepts the selected `accountId`. Numeric IDs are 1–24 digits and remain strings. All five belong to the default `core` toolset, return JSON in a text content block and work in MCP read-only mode; they do not add backend routes or OKR writes.

Error codes: 400 `OKR_INVALID`, 403 `OKR_DENIED` (the account may not read OKR), 404 `OKR_NOT_FOUND`, 502 `OKR_UNAVAILABLE` (retry later; also an OKR session token the service refuses even after a refresh). The API has no OKR writes.

## 6. Pitfalls

- Times: write ISO with an offset. Results come back in UTC.
- Pagination differs by route:

  | Routes             | Paging                      |
  | ------------------ | --------------------------- |
  | `/todos`           | `count` only                |
  | search             | `token`                     |
  | history / activity | `cursor`                    |
  | comments           | `position`                  |
  | subtasks           | fixed 50                    |
  | list content       | up to 300 (see `truncated`) |

- Validation errors from the old `PATCH` (start after due, `remindAt` together with offsets) return 500, not 400.
- Never let a silent `[]` clear assignees: always send the full intended list.
