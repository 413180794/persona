# Docs editing: docx blocks, sheets, Base

Paths are relative to `/agent/v1`.

- ⚠ marks a change other people will see. Confirm the change with the user before running it.
- Every edit needs edit permission. Without it the route returns 403 `DOCUMENT_EDIT_FORBIDDEN`.

All three editors work the same way: read the current state, send a change that includes what you read, and the server rejects the change with **409 `DOCUMENT_EDIT_CONFLICT`** if someone else changed it in between. When that happens, re-read and retry. The upstream has no true conditional write, so watch for `raced`, `unconfirmed` and `dropped` in responses.

To create a new document from Markdown, importing is far simpler than block edits. See `docs.md`, section 5.

## Contents

1. docx block edits
2. Sheets
3. Base: read
4. Base: records
5. Base: fields, views, tables
6. Base: attachments and extras

## 1. docx block edits

This works on docx pages and on wiki pages that are docx.

1. `GET /documents/edit?url=` returns `{token,title,rootId,blocks:{id:{type,parentId,children,fields,runs,editable,version}}}`.
   - The block type names here are text, heading1, bullet and so on. They differ from the node names returned by `/documents/read`.
2. ⚠ `POST /documents/edit {url, commit:{changes:[{id,base?,next?}], commitId?, holdMentions?}}`:
   - `base` is the block exactly as you read it. `next` is the new state. Leave out `base` for a new block, and leave out `next` to delete a block.
   - A new block id is 27 characters, and its characters at 0-based positions 4, 9, 14, 19 and 24 must equal the page token's characters at those positions.
   - Every new block must also appear in its parent's `next.children`.
   - Limits: 500 changes per commit, 20 000 characters per text run, 200 000 characters per commit. Each commit can newly @-mention at most 10 people, and those people are notified.
   - Responses:
     - Success: `{snapshot, unreportedMentions?}`.
     - 422 `DOCUMENT_EDIT_INVALID`: the change itself is malformed.
     - 409 `DOCUMENT_EDIT_CONFLICT`: someone else changed the block; re-read and retry.
     - **504 `DOCUMENT_EDIT_UNCERTAIN`: the change may already be written. Do not retry. Check with `POST /documents/edit/commit-status {url,commitId}` instead.**

**Example: append a paragraph to the end of the page.** The parent is `rootId` (usually the docx token).

```json
{"url":"…","commit":{"commitId":"agent-0001","changes":[
  {"id":"<NEW_27_CHAR_ID>","next":{"type":"text","parentId":"<rootId>","children":[],"fields":{},"runs":[{"text":"新段落","attrs":{}}]}},
  {"id":"<rootId>","base":<root block as read>,"next":<same, with NEW id appended to children>}
]}}
```

- Lists, todos and code blocks need their `fields` (seq, done, language).
- Copy an existing block of the same type to see the exact shape.

**Helpers:**

| Job | Call |
| --- | --- |
| Insert a table | `POST /documents/edit/insert-table {url,place:{parentId,afterId?},rows 1–100,cols 1–30}` |
| Simple table edits | `GET /documents/edit/table?url=&tableId=`, `POST /documents/edit/table` with kind addRow, addColumn, deleteRow, deleteColumn or cellText (deletes need the base text) |
| Advanced table edits | `GET`/`POST /documents/edit/table-tools` (merge, width, header, background, cell content) |
| Insert an image or file | `POST /documents/edit/media --upload FILE --url-query url=… --url-query kind=image\|file --url-query parentId=… [afterId,name,mime,width,height]`. Max 20 MB. Images must be png, jpeg, gif, webp or bmp |
| Insert a special block | `POST /documents/edit/insert-block` with synced, whiteboard, `poll{title,options 2–16}` or `task{summary,assignees≤20}` (assigns the task to those people), or `/insert-embed` (iframe or Base), or `/insert-aa-block` |
| Re-send @ notifications | `POST /documents/edit/mentions {url,uids≤10}` |
| Grant access to an @-mentioned person | `GET`/`POST /documents/edit/mention-access` with `grant{uid,role,notify}` or `askOwner{uid}`. The person must still be mentioned in the doc. ⚠ This grants permissions |
| Review your own suggested edits | `POST /documents/suggestions/review {url,id,action:"accept"\|"reject"}` |
| ⚠ Upgrade a legacy doc | `POST /documents/legacy/upgrade {url,confirm:true}`. Cannot be undone |

## 2. Sheets

1. `GET /documents/read?url=` (add `&sheet=ID` for one tab). Note `spreadsheet.revision`, the cell values, and `protections[].editableByMe`.
2. ⚠ Edit cells: `POST /documents/sheet/edit {url, sheetId, baseRevision, changes:[{row, column, input≤50000, base}]}`
   - Up to 5000 changes per call. `row` and `column` start at 0.
   - `base` is the cell's current value, taken from the first of these that is set: the formula, the input, the joined runs text, the text. Use `""` for an empty cell.
   - Getting `raced` back means someone else wrote at the same time.
3. ⚠ Change structure or style: `POST /documents/sheet/structure {url, sheetId, baseRevision, op}`. Supported `op` values:
   - Rows and columns: insert, delete, hide, unhide; rowHeight, columnWidth.
   - Formatting: style, merge, numberFormat, border, clearFormat, paintFormat.
   - Data: sort, checkbox, choose, validation, conditional formats.
   - View: freeze, add or remove filter.
   - Tabs: addSheet, renameSheet, deleteSheet, hideSheet, moveSheet, tabColor.
   - Protection: protect, unprotect.

   Deleting rows, columns or tabs destroys data.

Sheet AI helpers (they use AI quota): `POST /documents/sheet-ai/tasks {url,kind:"formula"|"explain"|"ask",question}`, then poll `GET …/tasks/ID`.

## 3. Base: read

Ids: tables `tbl…`, views `vew…`, fields `fld…`, records `rec…`.

| Call | Returns |
| --- | --- |
| `GET /documents/base/table?url=&table=&view=` | `{tables[], table:{fields (with options[].id), records (first page), nextOffset, views, rules}, editable, timeZone}` |
| `GET /documents/base/records?url=&table=&view=&offset=` | `{records:[{id,cells}], nextOffset?}`. 200 per page, and **`offset` is required** (start with the `nextOffset` from `table`) |
| `GET /documents/base/history?url=&table=&record=&before=` | One record's edit history |
| `GET /documents/base/comments?url=` | Base comments |
| `GET /documents/base/dashboard?url=&block=` | Dashboard data |

## 4. Base: records

⚠ Call `POST /documents/base/edit {url, edit:{action, …}}`. Actions:

| Action | Body | Notes |
| --- | --- | --- |
| `set` | `{table, view?, record, cells:{fldX:<cell>}, expect:{fldX:<fingerprint>}}` | Every field in `cells` must have an entry in `expect` |
| `add` | `{table, cells?, record?}` | If you supply `record`, it must be `rec` followed by 11 base62 characters |
| `setMany` | `{table, records≤100, cells, expect}` | Writes the same cells to many records |
| ⚠ `delete` | `{table, records≤100, expect:{}}` | Cannot be undone |

Cell values:

| Type | Shape |
| --- | --- |
| Text | `{type:"text",text}` |
| Number | `{type:"number",number}` |
| Single or multi select | `{type:"option",ids:[optionId]}` |
| Date | `{type:"date",value:<ms>}` (a date alone is midnight in the Base `timeZone`) |
| Checkbox | `{type:"checkbox",checked}` |
| User | `{type:"user",ids≤50}` |
| URL, email, phone | `{type:"url"\|"email"\|"phone",…}` |
| Link | `{type:"link",ids≤100}` |
| Attachment | `{type:"attachment",files≤100}` |

A record holds at most 200 fields.

`expect` fingerprints describe each field's **current** value:

| Current value | Fingerprint |
| --- | --- |
| Empty cell | `""` |
| Text | `t:` + JSON of `[[text,href],…]` over runs that have text or a link; `href` is `""` when a run has no link, never `null` |
| Number | `n:[…]` |
| Date | `d:[…]` |
| Checkbox, checked | `c:1` |
| Option | `o:[ids]` |
| User or group | `u:[ids]` |
| Link | `l:[recIds]` |
| Attachment | `a:[tokens]` |

The response is `{records, added?, removed?, dropped?, unconfirmed?}`. Errors:

- 422 `BASE_EDIT_INVALID`: the body is malformed.
- 409: `expect` no longer matches, so re-read with `base/records`, rebuild `expect`, and retry.

Bulk paste: `POST /documents/base/paste {url, paste:{table, view?, rows≤500:[{record,add?,cells}], expect}}`. It accepts option names and returns `{conflicts,unconfirmed,stopped}`.

## 5. Base: fields, views, tables

⚠ Call `POST /documents/base/schema {url, change:{action, …}}`; a body without the `change` wrapper is a 422. For example, `change:{action:"addField",table,name,kind,options?}` where `kind` is `text`, `singleSelect`, `multiSelect`, `date`, `url`, `attachment` and so on. Operations:

| Area | Operations |
| --- | --- |
| Fields | `addField` (text, number, select, date, user, link, formula, lookup, button, stage…), `renameField`, ⚠ `deleteField`, `setFieldHidden` |
| Views | `addView` (grid, kanban, gallery, gantt, calendar), `renameView`, ⚠ `deleteView`, `setRules{filter,sort,group}`, `setRowHeight` |
| Tables | `addTable`, `renameTable`, ⚠ `deleteTable` |

The response is `{table, field?, view?, tables?, unconfirmed?}`. 422 means the change is invalid.

## 6. Base: attachments and extras

| Job | Call |
| --- | --- |
| Upload an attachment | `POST /documents/base/attachments --upload FILE --url-query url=… --url-query table=… --url-query field=… --url-query name=…` (max 50 MB). Then `set` the cell with `{type:"attachment",files:[…existing, new]}` and `expect` `a:[existing tokens]` |
| Export a field's attachments | `POST /documents/base/attachments/export {url,table,view,field?,naming:"original"\|"primary"}` returns `{job}`. Poll `GET …/export/JOB?url=` every 1.5 s, up to 120 times, until `done{token}`, then download with drive `files/TOKEN/download` |
| Forms | `GET`/`POST /documents/base/form`: `create`, `questions`, ⚠ `share{on}` (creates a public link) |
| Automations | `GET`/`POST /documents/base/automations` (create, build, enable, disable, delete). Needs edit permission |
| Buttons | `GET /documents/base/button-status?fields=`; ⚠ `POST /documents/base/button-trigger {table,record,field}` runs whatever workflow is attached to the button |
| Record subscriptions | `POST /documents/base/subscription {table,records}` (a lookup), `POST /documents/base/subscribe {record,subscribe}` |
| Whole-Base history | `GET /documents/base/file-history` |
| AI filter or formula | `POST /documents/base-ai/filter`; `POST /documents/base-ai/formula`, then poll `GET …/formula/SESSION?url=`. These use AI quota |
