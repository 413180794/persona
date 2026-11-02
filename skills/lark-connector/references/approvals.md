# Approvals (审批中心)

Paths are relative to `/agent/v1`.

- ⚠ = visible to others and usually cannot be undone. Confirm first.
- _(unverified)_ = never sent to the real Lark gateway.

**Every approval write here is _(unverified)_**, approve and reject included. Before acting:

1. Repeat the request back to the user: form, applicant, and what you will do.
2. Wait for an explicit yes.
3. After acting, read the detail again and report the new status.

If a write fails in an unexpected way, give the user the `larkUrl` to finish in Lark.

These hosts are supported: feishu.cn, larkoffice.com and larksuite.com. Others return 409 `APPROVAL_UNSUPPORTED_DOMAIN`.

## Read

| Call | Notes |
| --- | --- |
| `GET /approvals/counts` | Returns `{todo,done,initiated,ccUnread,ccRead,toBeSubmitted}` |
| `GET /approvals?tab=todo\|done\|initiated\|ccUnread\|ccRead\|toBeSubmitted\|shared` | Optional parameters:<br>`cursor`<br>`q` (≤100)<br>`status` (running, approved, rejected, canceled, terminated, recalled)<br>`range` (day, week, month)<br>`order` (newest, oldest)<br>`types` (≤20 `sys:code` keys from `/approvals/types`)<br>`locale`<br><br>30 items per page. Items: `{instanceId, taskId, title, status, fields, initiator, actions?[{kind:"approve"\|"reject", commentRequired}], createdAt (s)}`. `actions` only appears on native approvals in the todo tab |
| `GET /approvals/types?tab=` | Approval types with counts and keys |
| `GET /approvals/detail?instanceId=&taskId=&status=&locale=` | Strict query; any unknown parameter returns 400. Returns form, process, timeline, approvers, current node, `shareUrl`. External approvals return 404 `APPROVAL_DETAIL_UNAVAILABLE` |
| `GET /approvals/flowchart?instanceId=&taskId?` | The flow chart |
| `GET /approvals/operations?instanceId=&taskId?` | Operations available to you, excluding approve and reject: `kind`, `label`, `disabled`, `commentRequired`, `methods[].modes[]`, `nodes[]`, `candidates[]`. Also returns `larkUrl` and `handover` |
| `GET /approvals/urge-targets?instanceId=` | Who can be nudged: `[{taskId,name}]` |
| `GET /approvals/comments?instanceId=` | Comments; `createdAt` is in ms |
| `GET /approvals/comments/mentions?instanceId=&q=`, `GET /approvals/phrases` | Mention candidates and saved phrases |
| `GET /approvals/INSTANCE_ID/pdf?locale=zh-CN&timezoneOffset=-480 -o form.pdf` | Starts an export upstream; a repeat within 3 min returns 429. External approvals return 409 |

## Write (all ⚠, all _(unverified)_)

| Job | Call | Notes |
| --- | --- | --- |
| Approve or reject | `POST /approvals/action {action:"approve"\|"reject", instanceId, taskId, comment?≤2000}` | Only when the list item's `actions` offers it. Errors:<br>409 `APPROVAL_NEEDS_SIGNATURE` or `APPROVAL_NEEDS_FORM`: send the user to `larkUrl`<br>400 `APPROVAL_COMMENT_REQUIRED` |
| Other operations | `POST /approvals/operate {kind, …, comment?}` | Kinds:<br>`recall{instanceId}`<br>`transfer{taskId,userId}`<br>`urge{taskIds[1..50]}` (from urge-targets)<br>`addSign{taskId,userIds,method:"pre"\|"parallel"\|"post",mode?}`<br>`rollback{taskId,nodeIds,rollbackType?}`<br>`notice`, `seekAdvice` or `subtract{taskId,userIds}`<br>`terminate`, `advise`, `handle`, `tempSave`, `invalid`, `voteFor`, `voteAgainst` or `withdraw{taskId}`<br><br>Check `/approvals/operations` first. The server re-checks and may return 403 `APPROVAL_OPERATION_NOT_ALLOWED` or 400 `APPROVAL_USER_NOT_ALLOWED` |
| Comment | `POST /approvals/comment {instanceId,text 1–2000}` | Everyone on the approval is notified. 403 `APPROVAL_COMMENT_NOT_ALLOWED` |
| Comment with mentions and files | `POST /approvals/comments {instanceId, segments:[{type:"text",text}\|{type:"mention",id,name}], parentCommentId?, files?[{kind,name,url,size?,thumbUrl?}]≤9}` | **Mentioning someone grants them view access to the approval** |
| Upload a comment file | `POST /approvals/comments/files --upload FILE --url-query instanceId=… --url-query name=…` | ≤20 MB. Returns `{kind,name,size,url,thumbUrl}`; put that in `files[]` |
| Mark CC read | `POST /approvals/cc-read {all:false,instanceIds[1..100]}` or `{all:true,query?,types?}` | Affects only you |
| Phrases, handover | `POST /approvals/phrases {add}\|{remove}`, `POST /approvals/handover/ignore {instanceId,taskId?}` |  |

Do not pass the `comment` kind from operations to `operate`; it returns 400. Use `/approvals/comment(s)`.

## Workflow

1. Run `GET /approvals/counts`, then `GET /approvals?tab=todo`, and pick the item (`instanceId`, `taskId`, `actions`).
2. Run `GET /approvals/detail?instanceId=…&taskId=…` and summarize the form for the user.
3. Once the user confirms, run `POST /approvals/action`. For other operations, run `operations` first and then `operate`.
4. Read the detail again. If you got a 409, or the status did not change, give the user `larkUrl`.

Group join requests are not approvals; they are in `groups.md`.
