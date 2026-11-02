# Docs: read, find, comment, drive, import/export, share, wiki

Paths are relative to `/agent/v1`.

- ⚠ means other people can see the change, or it cannot be undone. Confirm first.
- _(unverified)_ means the code says this was never run against the real Lark gateway. Read it back afterwards.
- Changing document content (docx blocks, sheet cells, Base records) is in `docs-editing.md`.

## Contents

1. Read any document link
2. Find documents
3. Comments
4. Drive: browse, upload, organize
5. Import and export
6. Versions and history
7. Sharing, permissions, security labels
8. Wiki
9. Templates, notifications, misc
10. Pitfalls

## 1. Read any document link

Call `GET /documents/read?url=<full https Lark URL>`. Encode the URL with `--url-query "url=$URL"`.

The `url` is required and must be 1–2048 characters. Invalid query values return 400 `DOCUMENT_INVALID_URL` before document loading. The MCP `read_document` tool applies these limits before HTTP and also checks URL format; the backend still validates supported hosts and the selected account's permissions. MCP returns the JSON view (or, with `format=markdown`, the Markdown) as text and the document metadata (`type`, `token`, `title`, `url`, `revision`, `partial`, `permission`, `authority`, …) as `structuredContent`; `fields` projects the JSON view, and a result over 8,000 estimated tokens is written to a private temp file with a heading outline and a line-range reading hint.

Accepted links:

- Hosts: feishu.cn, larkoffice.com or larksuite.com, with no port.
- Types: wiki, docx, sheets, docs (legacy), base or app, mindnote, slides, file, whiteboard, record.
- A bare token returns 400 `DOCUMENT_INVALID_URL`. Applink wrappers are not unwrapped.

Optional parameters:

- `segment=first` returns the first part of a docx quickly, with `partial:true`.
- `sheet=SHEET_ID` loads one tab of a spreadsheet; the ID must be 1–100 ASCII letters or digits. MCP trims surrounding whitespace before validating the ID; direct HTTP requires the ID as-is.
- `format=markdown` returns the already-authorized view rendered as Markdown instead of the JSON tree (`format=json` or omitted keeps the default response). It runs the identical read, permission, DLP, segment and size path first; nothing about the read changes. Markdown is an export: the client offers the same entry only when `authority.export` is true, so the API answers 403 `DOCUMENT_EXPORT_FORBIDDEN` unless `authority.export` is `true`. That authority is read live for each Markdown request (never from the reader's cache, and also for a `segment=first` read), and the response's `authority` is that live value; a failed authority read also refuses. The response is `{type, token, title, url, revision?, permission?, authority?, markdown, partial?}`. `type` is the rendered kind: `docx` (also wiki and legacy docs), `sheet`, `mindnote` or `slides`. A sheet renders one `## Tab` table per tab; slides render one `## Slide N` section per slide with its speaker notes quoted under `**Speaker notes**`. `partial:true` means the Markdown is missing content the document has: a `segment=first` render (or a workbook first segment) covering only the leading content (re-read without the segment), a workbook tab that was not loaded (rendered as `## Tab` plus `> Not loaded: read it with sheet=<id>`; re-read with that `sheet` for its cells), or a Mindnote cut at its node cap. Base (Bitable), file, whiteboard and shared-record links have no renderable body and answer 422 `DOCUMENT_FORMAT_UNSUPPORTED` — read them with the JSON format and the dedicated routes below.

The result is a **ProseMirror-style JSON tree** `content:{type,attrs,content[],marks[],text}`, not Markdown. Other keys:

- `title`, `url`, `token`, `outline[]`, `blockCount`, `owner`, `createdAt`, `updatedAt`
- `permission`, `editable`, `authority{copy,print,export}`

Block ids look like `doc-block-<blockId>`. Strip the prefix when a route asks for a block id.

What comes back for each type:

- Spreadsheets return `spreadsheet{sheets[],revision,editable}`.
  - A tab with `unloaded:true` needs a second call with `&sheet=<id>`.
  - `totalRows` tells you the first segment was truncated.
- Base returns `base` (tables, fields, first records). Page the rest with `GET /documents/base/records` (`docs-editing.md`).
- file, whiteboard and record return `reader` plus `object`.
  - For files, use `GET /documents/drive/files/TOKEN/preview?ext=EXT` (Office files become PDF), or download it.
  - For whiteboards, use `GET /documents/whiteboard/TOKEN`.

Images in the tree point to `/api/documents/media/...`. Rewrite the prefix to `/agent/v1`. Fetching `GET /documents/media/TOKEN` needs the `document` and `block` query parameters.

Limits: over 100 pages or 20 000 blocks returns 422 `DOCUMENT_TOO_LARGE`. Unsupported types return 400 `DOCUMENT_UNSUPPORTED`. `format=markdown` of a Base, file, whiteboard or shared record returns 422 `DOCUMENT_FORMAT_UNSUPPORTED`, and a document whose `authority.export` is not `true` returns 403 `DOCUMENT_EXPORT_FORBIDDEN`; permission and DLP refusals come back with their usual 403 codes in either format. In Markdown, embedded images still use the same `/api/documents/media/...` paths as the JSON tree (rewrite the prefix to `/agent/v1` and supply the `document` and `block` query parameters); other relative links resolve against the server's configured public URL, not the request's `Host` header.

Small reads:

| Call | Returns |
| --- | --- |
| `GET /documents/revision?url=` | Version and edit time; a cheap change check |
| `GET /documents/header?url=` | Path, last editor |
| `GET /documents/reader/info?url=` | Views and likes (docx) |
| `GET /documents/extras/backlinks?url=&cursor=` | Backlinks |
| `GET /documents/ai-summary?url=` | Cached AI summary only; never generates one |
| `GET /documents/suggestions?url=` | Suggested edits |
| `GET /documents/attachments/TOKEN/download?name=` | File blocks inside a docx |

## 2. Find documents

| Call | Notes |
| --- | --- |
| `GET /search/documents?q=&limit≤50` | Metadata only. For filters, use `POST /search/page` with `tab:"docs"` (`search.md`) |
| `GET /documents/recent?cursor=` | Recently opened. Items carry `updatedAt` (the sort time) and, when upstream reports one, the bare edit time `editedAt`. Preserve `nextCursor` exactly; cursor length is at most 2048 characters. Omit it for the first page; MCP `list_recent_documents` rejects a supplied empty cursor and, unless `raw=true`, omits `icon`, `ownerIdentity`, `ownerAvatarUrl` and empty dates |
| `GET /documents/drive?view=mine\|shared\|starred\|pinned\|trash\|unsorted&folder=&cursor=&sort=edited\|created\|name\|owner&order=` | **`view` is required.** 50 per page. Items: `token`, `title`, `type`, `url`, `mine`, `size`, `extension`, `editedAt` when upstream reports an edit time |
| `GET /documents/drive/chat?chat=FEED_ID&cursor=` | Docs shared in a chat |
| `GET /documents/custom-lists/rows?kinds=&owners=&chats=&folder=&space=` | Combined filters (≤20 values each) |
| `POST /wiki/spaces/SPACE/search {query,cursor?}` | Inside one wiki space |
| `GET /search/related-chats?url=` | Chats a doc was shared to |

Pass any returned `url` to `/documents/read`.

## 3. Comments

Comments work on docx (including docx inside wiki), legacy docs, sheets and Base.

| Job | Call | Notes |
| --- | --- | --- |
| Read | `GET /documents/comments?url=` | Returns `threads[{id,resolved,quote,anchorBlockId,replies[{id,author,content,mine}]}]`. **Base:** use `GET /documents/base/comments?url=` |
| ⚠ New whole-document comment | `POST /documents/comments {url,content≤5000}` | docx and doc only |
| ⚠ Reply | `POST /documents/comments/COMMENT_ID/replies {url,content}` | Notifies the thread |
| ⚠ Resolve or reopen | `PATCH /documents/comments/COMMENT_ID {url,resolved}` |  |
| Edit your reply | `PATCH /documents/comments/CID/replies/RID {url,content}` |  |
| ⚠ Delete your reply | `DELETE /documents/comments/CID/replies/RID?url=` | `url` goes in the query |
| ⚠ Comment on selected text | `POST /documents/comments/inline {url,content,quote≤1000,blockId}` | `blockId` without the `doc-block-` prefix |
| ⚠ Comment with @ or images | `POST /documents/comments/rich {url,action:"add"\|"reply"\|"edit"\|"add-inline",segments:[{kind:"text",text}\|{kind:"mention",id,name}],images?,imageReplyId?,private?}` | An @ notifies the person |
| Comment image | `POST /documents/comments/images?url=&name=` with `--upload` (png/jpeg/gif/webp, ≤20 MB) | Returns `{replyId,token}`; pass `images:[token],imageReplyId:replyId` to `rich` |
| ⚠ Sheet cell comment | `POST /documents/comments/cell {url,sheetId,row,column,content}` | row and column are 0-based |
| ⚠ Base record comment | `POST /documents/comments/record {url,table,record,view?,quote?}` |  |
| Reactions | `POST`/`DELETE /documents/comments/replies/RID/reactions` |  |

## 4. Drive: browse, upload, organize

| Job | Call | Notes |
| --- | --- | --- |
| Browse | `GET /documents/drive?view=…` | See section 2 |
| Download | `GET /documents/drive/files/TOKEN/download -o FILE` | Only uploaded files (type 12) and export results. Export docx or sheets first (section 5) |
| Preview | `GET /documents/drive/files/TOKEN/preview?ext=` | Office files become PDF; supports Range |
| Create | `POST /documents/drive/items {kind:"folder"\|"docx"\|"sheet"\|"bitable"\|"mindnote"\|"slides", name≤200, parent?}` | `parent` must be your own folder. Returns `{token,url}` |
| Upload a file | `POST /documents/drive/upload --upload FILE --url-query name=… [--url-query folder=TOKEN]` | ≤50 MB. Returns 201 `{fileToken,name}` |
| Organize | `POST /documents/drive/items/write` with `{action, …}` | Actions: `rename{token,type,name}`, `move{…,destination:"root"\|folderToken}`, `delete` (to trash), `restore`, `star{on}`, `pin{on}`, `copy{name,destination}`, `shortcut`, `unshortcut`, ⚠ `purge` (permanent), `removeRecent`. rename, move and delete work only on your own items (`mine`) |
| ⚠ Zip a batch | `POST /documents/drive/zip {items:[{token,type:0\|12}]≤200}`, then `GET /documents/drive/zip/CODE` until `ready` or `partial`, then `…/download -o` | Poll every 2 s, at most 40 times. Downloads are limited to 4 at once (429) |
| Risk tags | `GET /documents/drive/risk?tokens=` (≤50) | Shows `canDownload` |
| New board or form | `POST /documents/drive/special {kind:"board"\|"form",name,parent?}` |  |

Explorer `type` codes:

| Code | Type       |
| ---- | ---------- |
| 0    | Folder     |
| 2    | Legacy doc |
| 3    | Sheet      |
| 8    | Bitable    |
| 11   | Mindnote   |
| 12   | File       |
| 22   | Docx       |
| 30   | Slides     |

## 5. Import and export

**Import a local file as an online doc.** This is the easiest way to write a new document:

```bash
ol POST /documents/drive/import --upload ./notes.md --url-query 'name=notes.md' -m 180
# 201 {token,url,target,name}: done
# 202 {pending,ticket,mount,target,name}: poll the line below until it is no longer pending
ol GET "/documents/drive/import/TICKET" --url-query mount=… --url-query target=… --url-query name=…
```

- The extension picks the target type:
  - md, markdown, txt, doc or docx becomes docx.
  - xls, xlsx or csv becomes a sheet.
  - base becomes bitable.
  - xmind, mm or opml becomes mindnote.
  - pptx becomes slides.
- The size limit is 50 MB. Add `folder=TOKEN` to choose the location.
- Error codes:
  - 415 `DRIVE_IMPORT_UNSUPPORTED`: the type cannot be imported.
  - 409 `DRIVE_IMPORT_IN_PROGRESS`: an import is still running.
  - 422 `DRIVE_IMPORT_FAILED`: the import failed.
- Sending the same folder, name and content again within 15 minutes returns the pending ticket instead of importing twice.

**Export:**

- docx to PDF or Word:
  1. `POST /documents/reader/export {url,extension:"pdf"|"docx"}` returns `{ticket}`.
  2. Poll `GET /documents/reader/export/TICKET?url=` (after 1 s, then every 3 s, up to 60 times) until `status:"done"`. You get `{fileToken,fileName}`.
  3. Download with `GET /documents/drive/files/FILE_TOKEN/download -o out.pdf -m 600`.

  If `authority.export` is false, the call returns 403 `DOCUMENT_EXPORT_FORBIDDEN`.

- Sheet or Base to xlsx: `POST /documents/share/export {url,extension:"xlsx"}`, then `GET /documents/share/export/TICKET?url=`. docx also accepts pdf and docx here.

## 6. Versions and history

| Call | Notes |
| --- | --- |
| `GET /documents/reader/history?url=&cursor=` | Edit history, 20 per page. Editors only (403 `DOCUMENT_HISTORY_FORBIDDEN`) |
| `GET /documents/reader/history/version?url=&version=` | The document as it was at that minor version |
| `GET /documents/reader/versions?url=&pageToken=`, `…/versions/view?url=&token=` | Saved (named) versions |
| `POST /documents/reader/versions {url,name≤100}`, `…/rename`, `…/delete {url,token}` | Manage saved versions. Delete moves the version to the version trash |
| `GET …/versions/trash`, `POST …/versions/restore {url,token}` | Restores a **deleted saved version**. It does not roll back the text |
| ⚠ `POST /documents/share/restore {url,version}` | Rolls the text back. docx only. The server polls for up to about 60 s |
| `GET /documents/drive/files/TOKEN/versions`; `POST …/versions {action:"restore"\|"delete",version}`; `GET …/versions/V/download`; `POST …/versions/upload --upload FILE --url-query name=…` | Drive file versions. Your own type-12 files only. Uploading replaces the current version |

## 7. Sharing, permissions, security labels

| Job | Call | Notes |
| --- | --- | --- |
| Share state | `GET /documents/share?url=` | Returns `{owner, can{manageMembers,copy,export}, link, members (first 50), memberTotal}` |
| Collaborators | `GET /documents/share/permissions?url=&cursor=` |  |
| Permission settings | `GET /documents/share/settings?url=` |  |
| ⚠ Link sharing | `POST /documents/share/link {url,value}` | Values: closed, tenantView, tenantEdit, anyoneView, anyoneEdit, partnerView, partnerEdit |
| ⚠ Invite collaborators | `POST /documents/share/members {url,invites:[{id,kind:"user",role:"view"\|"edit"\|"full"}],notify,message?}` | Up to 50 invites |
| ⚠ Change or remove a collaborator | `POST /documents/share/member {url,id,role}` | `role:null` removes the person |
| ⚠ Request access | `POST /documents/share/apply {url,role,message}` |  |
| Copy a doc | `POST /documents/share/copy {url,title?}` | Or `drive/items/write` with action `copy` |
| ⚠ Transfer ownership | `POST /documents/share/transfer {url,id}` | Cannot be undone |
| ⚠ Settings | `POST /documents/share/settings {url,setting,value}` | Settings: comment, copy, download, manageCollaborators, search |
| Move between My Space and Wiki | `POST /documents/move-to-wiki {items,spaceId,parent?}`, `POST /documents/move-to-space {wikiToken}` |  |
| Security labels | `GET /documents/security/labels`, `GET /documents/security?url=`; ⚠ `POST /documents/security {labelId,reason,url}`, `…/security/batch` | Requires `settable`. Downgrading may return 409 `DOCUMENT_SECURITY_NEEDS_APPROVAL` |

## 8. Wiki

Tokens match `[A-Za-z0-9]{10,100}`. Space ids are numeric.

| Job | Call | Notes |
| --- | --- | --- |
| Spaces | `GET /wiki/spaces?scope=all\|starred\|recent&cursor=` |  |
| Tree | `GET /wiki/spaces/SPACE/tree?node=`, `GET …/children?node=&cursor=` |  |
| Where a page lives | `GET /wiki/path?node=TOKEN` | Returns `{space,nodes[]}` |
| Search in a space | `POST /wiki/spaces/SPACE/search {query,cursor?}` |  |
| What you may do | `GET /wiki/spaces/SPACE/permissions?node=` | Returns `node.create`, `rename`, `move`, `delete`. Check before writing |
| Overview and members | `GET /wiki/spaces/SPACE/overview` |  |
| New page | `POST /wiki/spaces/SPACE/nodes {title≤800,parent?,objType: 22 (docx, default) \| 3 \| 8 \| 11}` | Returns the node with its url. Edit it like a docx (`docs-editing.md`) |
| Rename or move | `POST …/nodes/TOKEN/rename {title}`, `POST …/nodes/TOKEN/move {parent?}` |  |
| ⚠ Delete a page | `POST …/nodes/TOKEN/delete` | Deletes the whole subtree into the space trash |
| Trash | `GET …/trash`; `POST …/trash/TOKEN/restore {toTopLevel?}`; ⚠ `POST …/trash/TOKEN/destroy` | destroy is permanent. 409 `WIKI_RESTORE_TOP_LEVEL` means retry with `toTopLevel:true` |
| Copy or shortcut | `POST …/nodes/TOKEN/copy {title}`, `POST …/nodes/TOKEN/shortcut {spaceId,parent?}`, `POST …/nodes/TOKEN/move-across {spaceId,parent?}` |  |
| Space admin | `POST /wiki/spaces {name}`, `POST …/settings`, `POST …/members {ids,role}`, `…/members/ID/role`, `…/members/ID/delete` | Needs the `manage` permission |
| Favorites, categories, updates | `GET …/favorites`, `GET /wiki/space-categories`, `GET …/updates` |  |

## 9. Templates, notifications, misc

- Templates:
  - `GET /documents/templates?kind=docx|sheet|bitable|mindnote|slides` and `…/category?kind=&category=&page=`.
  - `POST /documents/templates/create {kind,template,token,title}` creates the doc in the root of My Space.
- Docs notification center:
  - `GET /documents/notifications?cursor=` lists @mentions, comments, replies and shares.
  - `POST /documents/notifications/read {ids}` marks them read.
- Subscriptions and polls:
  - `GET`/`POST /documents/extras/subscription`
  - `GET /documents/extras/poll`; ⚠ `POST …/poll/vote`
- Likes: ⚠ `POST /documents/reader/like {url,liked}`.
- In-doc AI and translation (no document writes, but they use AI quota):
  - `POST /documents/ai/tasks`, then poll `GET /documents/ai/tasks/ID`.
  - `POST /documents/translate {texts≤40,target}`.
- **Do not use `/documents/presence/*`.** It makes other readers see you online, with a cursor.

## 10. Pitfalls

- Before writing, check what you may do:
  - `editable` and `authority` from `read`
  - share `can` and `owner`
  - security `settable`
  - wiki `permissions`
  - drive `canCreate` and `mine`
- Drive cursors contain `|`; URL-encode them.
- The two "restore" calls do different things:
  - `reader/versions/restore` brings back a deleted saved version.
  - `share/restore` rolls the text back.
- Deletes come in two stages:
  - Drive `delete` moves to trash and `purge` is permanent.
  - Wiki `delete` moves to the space trash and `destroy` is permanent.
- Errors:
  - Lark business errors come back as 502 `DOCUMENT_API_<n>`.
  - A dead session returns 401 `AUTH_SESSION_EXPIRED`.
  - Timeouts return 504 `DOCUMENT_TIMEOUT`.
