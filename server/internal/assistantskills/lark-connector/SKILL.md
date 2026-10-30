---
name: lark-connector
description: Drive this machine's lark-connector (a local third-party Feishu/Lark Web client signed in as the user) through its host-only agent API on 127.0.0.1:8081. Use for anything done as the user in Feishu - send, reply, edit, recall or forward messages, files, images, video, voice, scheduled or urgent messages; chats, groups, members, labels, pins, read state; docs, sheets, Base, drive, wiki, comments, import/export; mail; calendar, free-busy, meeting rooms, meetings, minutes; todos, task lists, OKR, approvals; search, people, org chart, Moments, teams, canteen, red packets, AI Q&A. Use it even for casual asks like 发飞书、发文件、撤回、加急、定时发送、建群、踢人、打标签、看文档、改表格、多维表格、云盘、知识库、发邮件、回邮件、看日历、订会议室、妙记、待办、审批、OKR、搜消息、查人、汇报线、公司圈、removed、食堂、红包, or "lark-connector" / "8081". Do not use feishu-cli, lark-im or the official OpenAPI for this instance; those act as a bot or another session.
---

# lark-connector agent

Host storage rotation uses `npm run storage:maintain` (preview by default), with explicit `--apply`, `--docker`, and `--prune-build-cache` options. macOS hourly setup is separately explicit through `npm run setup:storage-maintenance`. Read `references/platform.md` section 13 and `deploy/README.md` before applying; the cache option affects the selected Docker builder across projects. No Agent API routes, fields or gates change.

lark-connector is a local Lark Web client that holds the user's scanned Lark session. Agents drive it through the legacy agent JSON API at `lark-connector_AGENT_BASE` + `/agent/v1` (helper default `http://127.0.0.1:8081/agent/v1`). That API is off unless the service enables it; this skill never enables it or changes the service configuration. Every write acts as the signed-in person, so other people see it as coming from them.

Existing calendar meetings expose per-meeting settings at `GET/PATCH /calendar/meetings/:meetingNumber/settings`. Only explicitly returned supported fields are editable, and writes require the upstream organizer gate plus a fresh read and readback confirmation. A failed confirmation may follow an applied write; refresh before retrying. This is distinct from account-wide meeting preferences and does not create or bind a new event meeting. See `references/calendar.md`.

CI/release operations are documented in `references/platform.md`: all jobs use Node.js 24, and one Linux job runs both desktop common work and server checks/build/release. Linux builds shared desktop code once; macOS/Windows download that Release asset for native Electron checks and packaging. Each run stages server and desktop artifacts in one dated Release (`build-YYYYMMDD-HHMMSS-runID`, Beijing time from the original workflow creation timestamp). `scripts/build-release.mjs` verifies all platforms before publishing a prerelease, then mirrors the same server bundles to the `server-builds` compatibility channel. Default server update discovery prefers completed dated releases and falls back to that channel. Reruns reuse their tag and preserve already published artifacts. The workflow does not restore or save npm, Electron or builder caches. PostgreSQL tests use at most four isolated workers and a disposable, run-local Node compilation cache; slow-file timings appear in the logs and Actions Summary. The real-PostgreSQL owner-reset suite has a local 90-second limit and drains cancelled setup before teardown. Heartbeat, security-audit and file-watch integration fixtures separate service preparation from assertions and drain asynchronous resource creation before disposing services/databases. Round-split invariance tests report each budget schedule separately while preserving known-defect aggregate checks (see the platform reference). Pure model and local-function-fake tests now use domain `unit-*.test` entries importing `.cases` modules; use the entry plus `-t` for focused runs (see `references/platform.md`). See `docs/performance/20261024-test-runtime.md` for the measured speedup and its limits.

This file covers how to call the API, the rules that hold for every task, and which module file to read next. Endpoints, bodies, workflows and pitfalls live in `references/`; read only the module the task needs.

Slow-disk operation: database refreshes skip unchanged row/payload/index rewrites, archive and identity reads use bounded batches, and disposable cache persistence coalesces pending work and compresses larger files. Search rebuilds read current source fields inside the shared per-message queue before updating their projection. Image caches preserve complete files during replacement; thumbnail timestamp writes and Service Worker body persistence have separate bounded admission rules. OLK cache budgets include message-index growth and lazily migrate old totals; their byte estimates are not physical disk usage. Stand-in cleanup retains the run records needed to reclaim directories after message deletion. A partial hash index reduces retention resource-reference scans with measured storage and WAL costs; its guarded migration can defer until a later start. Lane polling coalesces overlapping refreshes, and Agent Hub file reads bound ordinary file prefixes while preserving legacy fallbacks. Hub session state is published in memory only after persistence succeeds; ordinary session files use atomic replacement, while special files retain direct writes. These changes add no Agent route, authorization rule or required header. Database write durability, account isolation and cleanup protections remain in force. See the resource-use section of `references/platform.md` and `docs/performance/20261030-slow-disk.md` for counts, queue limits, compatibility and final local/NAS measurements, including slower cache persistence; do not run cleanup or change PostgreSQL durability settings merely to improve performance.

For bounded work-context requests, use `/context/capabilities` and `/context/jobs` to collect declared chats and mentions server-side, then read a bounded evidence bundle. Read `references/context-collection.md` for fixed windows, idempotency, resume, permissions and coverage. Follow `continuation` even on an empty page; completed collection does not prove a complete month. This uses the existing authenticated account and does not enable the legacy API.

For a quick picture of a time window across sources, `POST /context/harvest` answers in one synchronous call: mentions of me, key group chats with their threads, meetings, calendar, approvals, documents, pages updated in my starred and recently visited wiki spaces, document comment threads, mail, tasks, my OKRs and code links as normalized Markdown items with per-source coverage and resume cursors. To read harvested items in full afterwards, `POST /context/read` takes up to 20 item ids (`msg:`, `doc:`, `wiki:`, `cmt:`, `apv:`, `evt:`, `mtg:`, `min:`, `mail:`, `task:`, `okr:`; `doc:`/`cmt:` need the item's `url` as a hint) and answers each as Markdown with per-item errors. Read `references/context.md`. The `removedctx` command line writes it as a pack whose `INDEX.md` has a token-estimated file table and line-anchored highlights; `removedctx expand --pack DIR <id...>` fetches fuller content for chosen items and `removedctx bench` measures a harvest; `removedctx today` answers "what needs me today" (mentions, calendar, tasks, my approval todos over 24 hours) as Markdown, and `removedctx harvest --format ndjson` streams one item per line per round with a final coverage line. Prefer it for monthly reviews, planning and hand-overs; prefer context jobs when the user wants a durable, idempotent collection of declared chats with source references to re-read later. Repeating a window reuses settled meeting, minutes, calendar, approval, mail and document reads from a per-account local cache (coverage note `本地缓存命中 h/n`); changing or denied content is always read live.

Document harvest resumes preserve pending entries in full legacy cursors. If retry metadata still exceeds the cursor capacity, affected documents return as metadata with a coverage note; do not treat that as a successful body read. Continue any returned cursor and use `/context/read` with the document URL for missing content. See `references/context.md`.

From a shell, the bundled `olk` command line (`plugins/lark-connector/server/olk.mjs`; `bash plugins/lark-connector/install.sh` links `olk` and `removedctx` into `~/.local/bin`) wraps every route as `olk <domain> <verb>`: hand-written commands for the common reads and writes (`olk im list/read/search/mentions/send/reply`, `olk doc get`, `olk cal list`, `olk task list/create`, `olk mail list/thread`, `olk contact search`, `olk approval list`, `olk meet list/insights`, `olk minutes transcript`, …), one generated `[route]` command per remaining read route (query parameters via `--param key=value`), `olk routes` for the route catalog with each route's `file:line` (`plugins/lark-connector/catalog/routes.json`, regenerated by `npm run catalog:plugin` and checked by tests), and `olk api METHOD PATH` for the rest. It uses this agent API with `lark-connector_AGENT_TOKEN`, or, while the legacy API is off, the owner API at a loopback IP literal (`127.0.0.1` or `[::1]`, not `localhost`) through the server's own `lark-connector_DEBUG_LOGIN_TOKEN_FILE` (one session per command, signed out afterwards, also on SIGINT/SIGTERM); it stores no credential. Writes need `--yes` (exit 6 otherwise, nothing sent; `--dry-run` prints the request), `lark-connector_READ_ONLY=1` refuses them all, and `olk api` refuses owner sign-in/security, the OLK vault, the database export and account/update/push/registration writes. In owner mode OLK messages are decrypted only while the vault is unlocked and agent decryption is allowed, retrying missing-key messages once a key handed out in the same read is learned, with the same placeholders as this API. No route, input or response of this API changes; see `plugins/lark-connector/skills/olk/SKILL.md`.

The bundled Codex MCP plugin exposes a subset of this API as named tools, selected by `lark-connector_MCP_TOOLSETS` (default `core,attachments,write`; `jobs` adds the five collection tools; `all` enables everything; names are case-insensitive, unknown names warn on stderr, and a value naming no known toolset falls back to the default). Core tools include `harvest_context`, `read_context`, `read_document`, `list_meetings`, `get_meeting_insights`, `read_minutes`, `connection_status` and `list_accounts`; its `search_messages` keeps local-first quick search; `search_message_page` adds filtered upstream message search with opaque cursor paging through the existing read-only `POST /search/page` (`tab=messages`). Both remain available in MCP read-only mode, where `reply_to_message` stays hidden. See `references/search.md` for limits and cursor handling; this adds an MCP entry point, not a new backend route.

`read_minutes` takes a minutes token or minutes URL and returns the plain-text transcript from the existing `GET /agent/v1/minutes/:token/export?format=txt` route (bounded by `maxBytes`, default 256 KiB, hard cap 4 MiB, with a `truncated` flag) as text with metadata in `structuredContent`; `get_meeting_insights` remains summaries-only. Lists and searches answer a Markdown table of `items` plus one JSON line with cursors (`raw=true` gives server JSON); cursor-paged tools take `limit`/`cursor` and answer `nextCursor`/`hasMore` (`list_meetings` `count` is a deprecated alias of `limit`). Other generic JSON tool results are one compact JSON text block; `read_document`, `read_minutes` and `harvest_context` keep content in the text and metadata in `structuredContent`. JSON tools and `read_document` accept `fields` (dot paths, the same projection as `removedctx --select`; a table keeps `nextCursor`/`hasMore`); `reply_to_message`, `read_minutes`, `harvest_context`, `read_context`, `read_chat_file`, `read_document_attachment`, `collect_context` and `cancel_context_collection` do not. A document, transcript or text attachment over 8,000 estimated tokens goes to a private temp file and the answer gives its path, a line-range reading hint and an outline; non-image binary attachments come back as a temp file `resource_link`. Errors are `{error:{code,message,hint?,status?,recoverable,…}}` from the same classifier as `removedctx`; retry unchanged only when `recoverable` is true (argument errors and rejected 4xx are `false`). `harvest_context` accepts `preset:"today"` (mentions, calendar, tasks and my approval todos over the last day). `openWorldHint` is `true` for tools that read Lark and `false` for local-state tools. `list_conversations`, `search_messages`, `search_message_page`, `get_message_context` and `get_message` additionally return a compact agent projection (ids, positions, thread/root/parent ids, sender id/name, parts, mentions (`mentionKind` hashtag/user-group, `isOuter`), links, attachment keys, card follow-up handles (a share card's `chatId`/`userId`, a merge-forward card's `thread`/`hasThread`), recalled/encrypted/hidden flags plus the `isFlagged` star marker, reaction totals, paging cursors and search `upstream`/`grammar`; avatars, menus, render hints and false/empty defaults are omitted); pass `raw=true` for the uncompacted server object (MCP-side only, never sent upstream). `connection_status`, `list_accounts`, `search_people`, `list_recent_documents`, `list_meetings` and `get_meeting_insights` have the same `raw` escape hatch over compact views that drop avatars, icons, localized name maps and empty defaults but keep every id, token, URL, cursor and access marker (field lists in `plugins/lark-connector/skills/lark-connector/SKILL.md`). The raw HTTP API documented below still returns the full server shapes. Tool and parameter descriptions are deliberately short (when to use, key parameters, returns); the MCP skill and the `references/` files hold limits, defaults and workflows. The presentation guide is the MCP resource `lark-connector://guides/document-presentation`; the former `get_document_presentation_guide` tool still answers direct calls but is not listed in `tools/list`.

The plugin also exposes `context_collection_capabilities`, `collect_context`, `get_context_collection`, `cancel_context_collection` and `read_context_collection` with the `jobs` toolset. Collection returns a job immediately; reads use a strict `request.mode` for bundle/query/read/export. These jobs are IM-only (declared chats and @me mentions). Read-only mode permits these local job operations but still blocks Lark replies. See `references/context-collection.md` for inputs, continuation and coverage semantics.

`POST /search/mention-history` accepts optional `startTime`/`endTime` (nonnegative integer Unix seconds, same rules as `/search/page`) so a fixed window such as "mentions in the last 30 days" needs no full-history paging. Bad input, including `startTime` > `endTime`, returns 400 `INVALID_INPUT`. The opaque cursor carries no window, so paged calls must resend the same `startTime`/`endTime`; message items include `createTime` (ISO 8601) when the upstream hit has one. See `references/search.md` section 3.

`GET /info-efficiency` omits days with missing or invalid statistics. Treat an absent day as unknown; only an explicitly returned zero is a measured zero. See `references/chats.md`.

Document reads share the HTTP query limits: `url` is at most 2048 characters, `sheet` is 1–100 ASCII letters or digits, and the recent-document `cursor` is at most 2048 characters. The MCP adapter rejects invalid values before HTTP, keeps its existing URL-format check and sheet trimming, and requires a supplied cursor to be non-empty. Both `GET /documents/read` and the MCP `read_document` tool accept `format=markdown` (`json` is the default) to get the authorized view rendered as Markdown: `{type, token, title, url, revision?, permission?, authority?, markdown, partial?}` over HTTP (MCP returns the Markdown as text and the rest as `structuredContent`), with the same read, permission, DLP, segment and size checks as the JSON read; Markdown is an export, so it answers 403 `DOCUMENT_EXPORT_FORBIDDEN` unless `authority.export`, read live for that request, is `true` (also when the authority state could not be read); `partial:true` marks missing content (a segment render, a workbook tab not loaded, which the Markdown names with its `sheet` id, or a Mindnote past its node cap), and Base, files, whiteboards and shared records answer 422 `DOCUMENT_FORMAT_UNSUPPORTED`. See `references/docs.md`; host validation and account permissions remain enforced by the backend.

The MCP `list_meetings` and `get_meeting_insights` tools read the existing meeting history and insights routes with one HTTP request each; the MCP `read_minutes` tool reads the full transcript via `GET /minutes/:token/export?format=txt` (bounded, truncation-flagged). Preserve meeting cursors as 1–32 digit strings, including values above JavaScript's safe integer range. Insights work on the primary and added accounts; optional sources retain their own permission and availability boundaries. See `references/meetings.md`.

The MCP also exposes the existing read-only OKR routes as `list_okr_people`, `resolve_okr_owner`, `list_okr_periods`, `get_okr_detail` and `get_okr_progress`. These reads belong to the default `core` toolset and return JSON text blocks. OKR owner IDs are distinct from Lark user IDs; preserve numeric IDs as strings. See `references/tasks.md` for the lookup order and access errors.

Primary-account insights require a backend with the primary runtime wired into the agent API. If a valid numeric `historyId` still returns 400 `INVALID_MEETING`, report the backend compatibility problem; do not switch accounts as a workaround. A catalog entry alone does not prove that the application wired its handler's dependencies.

The web group composer's encryption entry now guides identity creation or vault unlock, group public-key publication, and automatic enablement. Privacy settings can publish the current key to selected recent groups, reporting each result and retrying failures. These UI flows reuse the OLK group directory; they add no agent route or protocol change. See `references/platform.md`.

网页群加密引导在检查期间不展示发布按钮；检查请求失败时只提供重新检查，检查成功且返回未发布时才展示发布入口。重试查到当前公钥时直接开启加密，不重新发布。此 UI 行为不改变目录读取、密钥或 Agent API 契约，详见 `references/platform.md`。

The web composer keeps More tools accessible during Brave Climb encoding, but blocks tools that send outside the encoded draft. This is a UI restriction, not an agent API gate; see `references/platform.md`.

Brave Climb is reversible encoding, not encryption; the web UI tells the user so at its switches, composer and their own encoded messages, and links to OLK encryption. Do not describe Brave Climb messages as encrypted or private to users.

手机更多面板顶部固定显示密钥加密、[removed]和本条消息自动撤回入口及开启状态，不随普通工具列表滚动；自动撤回显示当前秒数，普通工具保留独立滚动区。收起或切换面板后保留当前加密模式提示，可点击关闭。复用现有会话开关与群加密引导，不改变 Agent API。详见 `references/platform.md`。

OLAP agents can ask the owner questions during a run through negotiated `interrupts.question`. These interrupts use the OLAP WebSocket and owner-session UI, not the legacy agent token; see `references/platform.md` for the contract and boundaries.

OLK encrypted pictures: in an encrypted chat the web page encrypts each picture itself and uploads it with `olkSealed:true`; the picture's key travels only inside the message's OLK envelope. Agents cannot produce such a payload and should not set the flag; decrypted agent reads return the text without the picture keys, and the picture parts stay a cover picture (a random sticker, or with the local setting `olkImageStickerCover` off a grey placeholder, or one the sender picked and uploaded in plain as `cover`; older messages show a topic picture or the Brave Climb cover). See `references/messaging.md` sections 3 and 8.

lark-connector shows decrypted OLK text in the current account's conversation previews when available, otherwise an encrypted-message label. Other accounts in the mixed feed keep the label until opened in their own account. This is a UI-only change; agent responses and decryption permissions are unchanged. OLK manual and automatic translation remain disabled. Notification privacy settings still apply; offline Web Push never decrypts. Conversation digests expose optional `hasEncryptedEnvelope`, derived from actual message parts; an removedprefixed filename or card summary alone is not encryption. See `references/messaging.md`.

The web page's search can match OLK messages by their decrypted words while the owner's vault is unlocked, from an owner-only in-memory index. Agent search only ever matches their cover text; no agent route, input or result changes. See `references/search.md`.

Plain-composer mention encoding now matches the rich-text element index used for forwarding. Agent `mentions` inputs are unchanged; existing messages are not repaired retroactively. See `references/messaging.md`.

网页会话摘要对自己发送的消息显示已读状态，复用现有回执数据展示未读圆环、部分已读进度或全部已读勾；收到的消息和草稿不显示。此功能不改变 Agent API，详见 `references/messaging.md`。

The web thread pane preserves message line breaks and blank paragraphs. It shares the message list's bottom-right floating “回到最新” button: scrolling away from the bottom reveals it, and returning to the latest hides it. This is UI-only and changes no agent routes or message fields. See `references/messaging.md`.

网页话题卡片页在右下角提供未读提及跳转，支持 @我 和 @所有人，普通免打扰不影响入口。点击打开对应话题并定位消息或回复，未读提及清除后隐藏；此功能不改变 Agent API。详见 `references/messaging.md`。

OLAP also negotiates `runs.input` / `capabilities.input` for owner additions to a running task. It is separate from answering an interrupt or starting a follow-up run; see `references/platform.md` for retry and delivery semantics.

OLAP Hubs advertising `runs.agent` let registered agents start private runs; `artifacts.v1` lets them upload files to their own active runs. These use OLAP credentials, not the legacy agent token. See `references/platform.md` for creation, upload, owner download, and artifact expiry limits.

Settings → Privacy → Encryption identity offers migration of published visible `olk1:` keys to the existing invisible format. It preserves each key, the user's text and the existing audience, including public signatures; it does not rotate identities. No agent route, input, permission or visibility contract changes. See `references/platform.md` for unsupported scopes and signature freshness.

Frontier WebSocket 握手超过 30 秒未完成会关闭连接并进入现有重连流程；主动停止不触发重连。此期限不保证 30 秒内恢复实时同步，仍须检查 `GET /sync/status`。详见 `references/platform.md`。

网页头像和消息媒体的后台预热使用有界队列、超时和可淘汰缓存，离开消息窗口会取消该窗口的后台媒体预热。预热失败或缓存淘汰不代表附件过期、无权访问或服务端数据丢失；仍按现有媒体接口的响应判断。存储、表格渲染、媒体缓存和构建资源优化未改变 Agent 调用契约、存储格式或保留策略，详见 `references/platform.md` 的资源使用边界。

`GET /maintenance/export` 保留 schemaVersion 3 的 JSON 结构，改为同一只读快照下流式输出所选账号的固定 21 表。完整令牌仍是必需；所有账号在同一进程共享 1 个名额，忙时返回标准 Fastify 503，且无 `Retry-After`。下载连续 60 秒没有网络进展，也没有未受 HTTP 背压阻塞的源数据进展，或总计 30 分钟后终止；中途失败会截断响应，HTTP 200 本身不代表文件完整。`POST /maintenance/backups` 的 `pg_dump` 仍覆盖全数据库所有账号。只在用户要求导出时使用并直接保存到文件，详见 `references/platform.md` 第 13 节。

设置页首次加载同步状态失败时可重试，已有状态在刷新失败时仍保留；数据库和媒体备份下载失败时显示提示，可用原下载按钮重试。这些网页提示不改变 Agent API。

OLK 密钥库现支持恢复码（主记录格式不变，恢复封装单独存放，可回滚到旧版）：主人在消息加密设置中生成并确认保存，锁屏可用恢复码同时重设口令。恢复后自动解锁关闭；不会更换身份或消息密钥。此流程仅限 owner Web API，不增加或改变 `/agent/v1` 路由；Agent 不应索取或记录恢复码。详见 `references/platform.md`。

网页 OLK 自动外显（回放句或语气词）以 30% 概率附带原生“吃瓜”表情；自填外显、AI 建议和纯图片正文不添加。无法解密时也保留公开外显及表情；不改变 Agent API 或解密权限。详见 `references/messaging.md`。

网页 OLK 表情选择器按最近聚焦的正文或“对外显示”输入框插入内置表情；无法解密的消息优先展示已有对外文字，没有对外文字才显示解密状态。仅调整网页行为，不改变 Agent API 或解密权限，见 `references/messaging.md`。

普通群主历史只展示拥有会话位置的消息，话题专属回复不再按时间混入主流。用 `GET /messages/ROOT/thread` 读取话题回复；普通引用回复继续出现在主流。路由与字段不变，见 `references/messaging.md`。

网页新发 OLK 消息把自填对外内容中的已知飞书表情编码成原生表情元素，而非 `[标签]` 文字；正文内表情仍留在密文中。取消手写外显的固定 20 字限制，按实际外显长度预留信封空间，整条消息仍受传输上限约束。明文发送许可同时绑定完整表情列表。只涉及 owner 网页加密接口和显示，Agent 路由与权限不变，见 `references/messaging.md`。

替身模式支持 `traex`、`claude` 和 `codex`。`GET /settings/local` 可读到 `standInConfig.agent: "codex"`，但 Agent 仍不能修改替身设置；Codex 的安装、模型选择和权限由主人网页与服务器配置管理。详见 `references/platform.md` 和 `docs/stand-in-mode.md`。

## Calling the API

加急确认使用 `POST /urgents/ack {ackId,messageId}`，服务端通过 `CONFIRM_URGENT_ACK_BY_ID` 通知发送方确认结果。`GET /urgents/pending?includeClosed=true` 可查已关闭或静音但尚未确认的加急；默认查询仍隐藏这些记录。必须按目标消息和会话筛选，不能把“已读”或“已关闭”当作“已确认”。详见 `references/messaging.md`。

For collection workflows from a shell, prefer the bundled `node plugins/lark-connector/server/removedctx.mjs job collect --chat ID --days 30 --wait --select job.id,job.state` (inline flags instead of a spec file, `--dry-run`, `--select` on every JSON output, one-line JSON errors with hints, `removedctx schema` for flags and exit codes; wait timeout exits 10 and SIGINT 130 without cancelling the job). The older `npm run --silent context -- collect --spec context.json --wait` still works in a repository checkout with Bash and curl. See `references/context-collection.md` for commands, exit codes and single-page export.

Use the bundled helper `scripts/ol.sh`. This shell function finds it from the repo or the user skill directories:

```bash
ol() {
  local root s
  root=$(git rev-parse --show-toplevel 2>/dev/null || true)
  for s in \
    "${root:+$root/.agents/skills/lark-connector/scripts/ol.sh}" \
    "$HOME/.agents/skills/lark-connector/scripts/ol.sh" \
    "$HOME/.claude/skills/lark-connector/scripts/ol.sh"
  do
    [ -f "$s" ] && { bash "$s" "$@"; return; }
  done
  echo "lark-connector helper not found" >&2
  return 1
}
```

```bash
ol GET /                                        # live route catalog (trust it over these docs)
ol GET '/conversations?scope=group&limit=100'
ol GET /search/people --url-query 'q=张三'       # non-ASCII and '#' must be encoded
ol POST /conversations/FEED_ID/messages '{"text":"hi"}'
ol POST /messages/MESSAGE_ID/recall              # a body-less POST sends {} for you
ol POST /conversations/FEED_ID/files --upload ./a.pdf \
  --url-query "clientId=$(uuidgen)" --url-query 'name=a.pdf'
ol GET /media/RESOURCE_KEY -o out.bin           # binary responses need -o
ol events --feed FEED_ID --others --seconds 300  # live events as NDJSON
```

What the helper does for you:

- Pass the body as the third argument (JSON text or `@file.json`). Extra curl flags go after it. `-m 600` at the end overrides the 60 s timeout; long imports and big downloads need it.
- On Windows Git Bash/MSYS, inline JSON is passed to curl through a standard input pipe so Chinese, emoji and newlines retain their UTF-8 bytes. This creates no temporary file and keeps curl's response body and exit status; other hosts retain the direct curl invocation, and `@file.json` still reads the named file.
- POST, PUT, PATCH and DELETE requests get an `Origin` header derived from the base URL. Many write routes require it; `CROSS_ORIGIN_REQUEST` means the header is absent or its scheme, host or port differs from the server's expected origin. If you call `curl` directly, add the origin matching the service address; report proxy mismatches rather than guessing internal headers. Agent `POST /urgents/ack` and `POST /urgents/close` require the agent token but do not require or compare `Origin`; their website `/api` counterparts still require same-origin requests. See `references/messaging.md` for authorization and `ackId` lookup.
- The base URL comes from `lark-connector_AGENT_BASE`. The service's ports are set by `LARK_AGENT_PORT` and `LARK_SERVER_PORT`, so confirm the running service's address instead of assuming the defaults.
- The token comes from `lark-connector_AGENT_TOKEN`, else from `lark-connector_ENV_FILE`, else from the `.env` of the repo you are in or the repo the script lives in. Never print it. OLAP credentials are a different thing and cannot replace it. The host may instead give you `lark-connector_AGENT_READ_TOKEN`: a read-only token for GETs (bar three reserved for the full token), `/events` and the read-only POST allowlist (see `references/platform.md`); use it for read-only work by exporting it as `lark-connector_AGENT_TOKEN`.
- `lark-connector_ACCOUNT=N` acts as account N.
- HTTP ≥ 400 exits non-zero and still prints the error body.

Do not substitute the website's `/api/*` surface (local default `http://127.0.0.1:3333`). It sits behind the owner-session boundary and needs a browser cookie.

The optional bundled MCP adapter has separate response budgets: 16 MiB for JSON and 64 KiB for backend errors. Its `CONTENT_TOO_LARGE` never returns truncated data and never retries writes. Reuse `connection_status.accounts` for account selection; see `references/platform.md`. These adapter limits do not change `/agent/v1` routes or the shell helper.

The MCP adapter shares overlapping JSON GETs only within the same client, exact URL, and selected account. It forgets each result or error on completion; a later call reads again. Writes are never shared and separate subsequent reads from earlier in-flight reads. This does not add an HTTP cache or change tool arguments.

`GET /calendar/week` for a range that had already ended when its local copy was taken, copied within 24h, answers from the stored copy without an upstream read and carries `x-lark-connector-cache: stored`. Current and future ranges, copies older than 24h and partial reads still read upstream; any calendar write (other than the visibility toggle, which rewrites the copies) or change push drops the stored past copies. See `references/calendar.md`.

## Every task

1. When backend connectivity or session/account state is unknown, run `ol GET /health` (or MCP `connection_status`). When the connection, chosen account and route are already known, call that route directly.
   - Look at the status and body (`ol GET /health -i` shows headers). No single result proves the service is down. Report what you saw, stop dependent calls, and do not fall back to other Feishu tools.
   - Connection refused means nothing listens at that address: the service is down, the base URL is wrong, or the legacy agent API is switched off.
   - 404 `AGENT_LEGACY_DISABLED` means the agent API is off. It needs `LARK_AGENT_LEGACY=token` plus `lark-connector_AGENT_TOKEN` or `lark-connector_AGENT_READ_TOKEN` on the host; see `references/platform.md` and the README section [Agent 接口（旧，默认关闭）](../../../README.md#agent-接口旧默认关闭). Tell the user; do not change the host yourself. Any other 404 usually means a wrong base URL.
   - 401 `AGENT_AUTH_REQUIRED` means the token is missing or wrong, typically because you are outside the repo. Export the token the host gave you.
   - 403 `AGENT_READ_FORBIDDEN` (`recoverable:false`) means you hold the read-only token and called a write outside its allowlist, or one of the GETs reserved for the full token (`/links/open`, an invite `qr-code`, `/maintenance/export`). Stop: do not retry or look for another route; ask the host for the full token if the write is genuinely needed.
2. When identity or freshness is unknown or needs refreshing, check `ol GET /accounts`, `ol GET /me` and `ol GET /sync/status`. Reuse a known account inventory, including the one returned by MCP `connection_status`, and pass the chosen account explicitly. Never fall back to another account after a refusal.
   - In `/sync/status`, `state` should be `live` and `mode` should be `frontier`. `polling` means events arrive about a minute late.
   - 实时补偿遇到数据库连接池取连接超时时会自动退避重试；背压暂停后须先补齐历史并排空缓冲，再恢复连接。以 `live / frontier` 和持续推进的 `lastPacketAt` 确认恢复，不要只凭 `backlog: 0` 判断。见 `references/platform.md`。
   - 401 `AUTH_SESSION_EXPIRED` from any call means the Lark session is gone or needs fresh device authentication. This includes explicit gateway session codes and an inner packet status of 401. Tell the user to use the website’s sign-in-again banner; do not start a login yourself. A dropped Frontier socket alone does not prove expiry. See `references/platform.md`.
3. Resolve names to ids before any write.
   - Ids are numeric strings (`feedId`, `userId`, `messageId`), not `oc_` / `ou_` / `om_`. To turn a group's `oc_` id into its feedId, call `GET /conversations/open-chat/OC_ID` (`references/chats.md` section 1).
   - If several chats or people match, list them and ask.
4. Read the module for the task (table below), then act. For an unfamiliar or conditionally registered route, check `ol GET /` first. The catalog lists methods and paths only; read the module or request schema for limits and gates.
5. After a write, read the result back and report what actually happened. Some writes were never exercised against the real Lark gateway; modules mark them _(unverified)_. Read those back and tell the user if the result differs.

## Rules that hold everywhere

- **头像写入与回读。** `POST /profile/avatar` 成功后会清除当前账号的服务端个人资料缓存，随后 `GET /home/user` 可重新拉取资料；`GET /me` 仍读取本地投影。Web 权限读取失败会提示并允许重试，保存失败保留图片；排障需采集受影响实例的请求响应，不用真实写入探测。详见 `references/people.md` 第 4 节。

- **Local store first.** Most chat and message actions only work on rows already in the local store.
  - For a chat id that came from search, a link or `/conversations/groups`, call `GET /conversations/FEED_ID` once; it pulls the chat in.
  - For a message found by search, load it with `GET /conversations/FEED_ID/messages?aroundPosition=POSITION`.
  - Search coverage reports cached messages, not complete history for a time window. Preserve upstream failure flags and pagination; see `references/search.md` before claiming a monthly collection is complete.
- **Resource URLs.** Responses carry UI paths such as `/api/media/KEY`, `/api/documents/media/...` or `/api/moments/videos/...`. Replace `/api` with `/agent/v1` before fetching. Mail inline images are the exception; see `references/mail.md`.
- **Media recovery.** Image reads may return `503 MEDIA_DECRYPTION_PENDING` with `Cache-Control: no-store` and `Retry-After` while outer decryption metadata is missing. Retry only transient failures with a bounded budget and respect `Retry-After`; `410` is terminal. An explicit `?refresh=1` on either media GET bypasses the stored media copy and returns `no-store`. See `references/messaging.md`.
- **Errors come in three shapes.** Check both `body.error.code` and `body.code`.
  - `{error:{code,message,recoverable,traceId}}`
  - Fastify's `{statusCode,error,message[,code]}`
  - A bare `{error:"..."}`

  A 500 is often bad input, not an outage: thrown "not found" errors and zod parse failures both surface as 500. `Gateway HTTP 400 for <COMMAND>` is Lark refusing the call. A reply Lark refuses to place in a thread answers 409 `REPLY_IN_THREAD_*` instead, with `larkCode` and the failed message's `clientId`; `replyMode:"thread"` in a topic group, and `threadRootId` on a file, video, voice or location send there, land in the topic (`references/messaging.md` sections 2 and 4). A failed send stays in the chat as `local:<clientId>`; `DELETE /outbox/CLIENT_ID` discards it locally without touching Lark (`references/messaging.md` section 12).

- **Some GETs change state.** `GET /conversations/FEED_ID/pins` clears the unread-pin mark. Opening a Moments post re-crawls its comments. `GET /approvals/ID/pdf` starts an export. `GET /maintenance/export` streams the selected account's fixed 21 tables with the limits documented in `references/platform.md`; call it only when the user asks for an export.
- **Formats differ per product.** Times may be ISO, Unix seconds or milliseconds; pagination may use cursors, offsets, positions or page tokens. Each module states which one it uses.
- **Gate switches.** `403 AUTO_RECALL_DISABLED` and `403 BRAVE_CLIMB_CODEC_DISABLED` come from FLiNG Trainer switches the user controls. Report them; flip a switch only with the user's consent (`references/platform.md`).
- **Urgent acknowledgement.** `POST /urgents/ack` confirms receipt and notifies the sender; pass the pending item’s `version` with `ackId` and `messageId` (omitted version defaults to `0`). Closing a reminder is not confirmation; use `includeClosed=true` to find closed but unconfirmed records. See `references/messaging.md` section 7.
- **消息密钥库。** 已建库账号的消息加密使用服务端托管身份；未建库账号可继续使用浏览器已有钥匙，或在设置中迁入服务端。聊天读取中的加密消息在当前账号的托管密钥库已解锁、且用户开启了 Agent 明文访问时自动解密；否则换成说明原因的占位文字，普通消息不受影响。完整加密备份的下载与恢复仅在主人页面提供，导出必须重新输入密钥库口令并验证主人身份。飞书云空间身份备份也恢复到服务端，但不包含群密钥和发送状态，不能替代完整备份。主人可在已解锁的设置页切换对外公钥或删除非对外身份，均需重新验证主人身份；删除可能永久失去解密能力。不要索取密码或私钥，也没有 Agent 身份管理、解锁、备份接口；具体范围见 `references/messaging.md` 第 8 节。

实时恢复按补偿顺序重试失败的实时包，避免较新的游标掩盖较早的历史缺口。某条 pipe 的游标过期不会阻塞其它 pipe 分页和尾部重试；过期 pipe 仍保留错误并使同步处于降级状态。接口契约不变，详见 `references/platform.md`。

账号 runtime 关闭时会停止保留策略清理调度及历史补拉，并等待在途任务退出后关闭依赖；清理完成当前删除批次及必要的会话边界更新后退出，不再启动后续批次、会话或媒体条目。关闭期间不会启动排队清理或后续补拉批次。没有新增 Agent 停止接口。详见 `references/platform.md`。

## Confirm before acting

Only act on targets the user named. Show the exact content and recipients before anything other people will see. These need an explicit go-ahead:

- **Messages:** recall, group-recall, delete, urgent (`sms` / `phone` really texts or calls), forward and merge-forward, scheduled `sendNow`, `read-all`, and per-label clear-unread. `read-position` sends read receipts.
- **Groups:** kick, admins, owner transfer, leave, dissolve, clear-history, revoking invitations, deactivating share links, `/scan` (joins the group), invite QR or links (credentials), turning a group public, post permissions, auto-join rules, and batch mute by filter.
- **Docs:** drive purge, wiki destroy, deleting Base records, tables or fields, deleting sheet rows, columns or tabs, and share writes (link to anyone, transfer, restore). Also deleting versions and the legacy upgrade.
- **Mail:** send, schedule, recall, delete-forever, empty, unsubscribe, auto-reply, applying rules, and sharing a thread to a chat. Mail sends immediately and cannot be undone.
- **Calendar and meetings:** deleting an event or a whole calendar, transferring an event, calendar sharing, inviting external emails, holding a room, and launching a meeting.
- **Tasks and approvals:** deleting todos or task lists, removing or inviting list members, list activity rules, and every approval action.
- **People, Moments and workplace:**
  - Moments: posting, commenting, deleting, sharing; everything there is company-wide.
  - Team admin.
  - Contacts: deleting a contact, resetting the add-friend link, friend requests, and the phone lookup (it can notify the person and uses quota).
  - Changing the avatar or background.
  - Canteen: connect, order, cancel. Ordering is a real order.
  - Clearing search history, deleting AI topics, joining public groups.
- **Session and host:**
  - Session: `DELETE /auth/session`; `POST /auth/attempts` (cancels any login in progress, and a different person scanning an added account wipes that slot); device logout; `PUT /settings/local`.
  - `GET /settings/local` also returns `standIn`/`standInConfig` (including `ownerOnly`); `PUT` keeps those stored values, so an agent cannot change the stand-in.
  - Visible profile changes: status, signature (public, private or dynamic), do-not-disturb.
  - `/links/open`, which signs into web apps as the user.
  - Maintenance routes (backups, vacuum, rebuild, media cache): never run them unless asked.

## Modules

| Read | When the task is about |
| --- | --- |
| `references/platform.md` | Turning the agent API on, token and accounts, health and sync, error codes, `/events`, re-login, personal status, public, private and dynamic signatures, do-not-disturb, translation and notification settings, FLiNG Trainer switches, emoji and sticker keys, devices, opening web apps signed in, host maintenance, retention policy and backfill |
| `references/messaging.md` | Sending anything (text, post, markdown, @, images, files, video, voice, location, stickers, cards, votes, scheduled, urgent), editing, recalling, reacting, forwarding, pinning a message, history, threads, read receipts, recalled and encrypted message lists, translation, downloading attachments, card buttons |
| `references/chats.md` | Finding chats, scopes and unread views, chat settings (mute, done, pin to top, flag, labels, nickname), feed actions (mark read or unread, box, later), labels and feed groups, favorites and flags, batch mute by filter, conversation retention settings, tail gaps diagnostic |
| `references/groups.md` | Creating DMs and groups, members, admins, owner, leaving or dissolving, invites, QR codes, join requests, group permissions, top pins and tabs, group menu, bots, group files, tasks and events, threads and the threads you follow |
| `references/docs.md` | Reading any doc, sheet, Base or wiki link, finding docs, comments, drive browse, upload, organize, import Markdown or Office to docs, export to PDF, Word or Excel, versions, sharing and permissions, wiki spaces and pages |
| `references/docs-editing.md` | Changing content: docx block edits, inserting tables or images, sheet cells and structure, Base records, fields, views, attachments |
| `references/mail.md` | Reading, searching, writing, replying to, forwarding, scheduling or recalling mail, attachments, folders and labels, rules, signatures, auto-reply, shared mailboxes |
| `references/calendar.md` | Viewing, creating, changing or deleting events (including recurring ones), RSVP, free-busy, booking meeting rooms, calendar management, event cards in chats |
| `references/meetings.md` | Meeting history and details, launching a meeting, a voice agent joining a meeting, minutes (妙记) transcripts, summaries and exports |
| `references/tasks.md` | Todos, subtasks, comments, attachments, task lists and sections, OKR |
| `references/approvals.md` | The approval center: lists, details, approve, reject, transfer, add sign, comments, PDF export |
| `references/search.md` | Message, people, doc and group search with filters, @me history, platform search (minutes, example-cli and more), enterprise AI Q&A, My AI |
| `references/docs-presentation.md` | Feishu report callouts, diagrams, Mermaid whiteboards and HTML-to-PNG guidance from the MCP's bundled presentation resource |
| `references/context.md` | One call that gathers a time window of my context (mentions of me, key group chats with threads, meetings, calendar, approvals, documents, wiki pages, document comment threads, mail, tasks, my OKRs, code links) as normalized Markdown items with per-source coverage, for monthly reviews, planning and hand-overs |
| `references/people.md` | Profiles, managers and report lines, departments, frequent contacts, external contacts and friend requests, special focus, aliases, teams |
| `references/moments.md` | removed feed, posts, comments, reactions, notifications, profiles, posting |
| `references/workplace.md` | Workplace apps, help desks, canteen ordering, red-packet records, Lingo glossary |

Large modules start with a contents list; jump to the section you need.

## Web client version

The website exposes public `GET /api/version` with only `{ version }` (the build timestamp, `development` in dev, or `null` if build metadata is unavailable), with `Cache-Control: no-store`. When browser local storage is inaccessible, reload recovery is allowed only when there is no unsaved composer state, active reload hold, pending edit request or edit session. Automatic chunk recovery still requires its session-storage loop guard; manual recovery remains available when that guard cannot be stored. This is a browser recovery change with no Agent API effect; see `references/platform.md`. Version requests stay on the main instance even when account pinning adds an added-account selector. The loaded page checks at startup, on focus/visibility and every five minutes, saves message text/edit drafts, and automatically reloads for a changed build or an installed service-worker update. Local image drafts, including those parked during message editing, delay the reload until sent or removed. A failed draft save keeps the page open with a dismissible retry notice. Session storage prevents repeated automatic reloads for the same loaded/target build pair if stale HTML is returned. Settings → About shows the loaded page's build, not the latest server version. Other unfinished forms retain their existing navigation protections. This changes website behavior only; it adds no `/agent/v1` route or agent capability.

## Boundaries

- Not possible from this API:
  - Sending raw schema-2 JSON cards.
  - Video or screen sharing in meetings.
  - Sending, grabbing or opening red packets.
  - Adding or removing local accounts.
  - Checking outbox status. `GET /api/outbox/ID` exists only on the owner surface; retry with the same `clientId` instead.
  - Card callbacks other than POST buttons (`actionMethod: 2`).
- The agent listener must stay host-only; check the deployment's loopback binding or port mapping. Writes act as the signed-in user.
- The modules are not an exhaustive capability list. Route set, limits and gates change with the code. For anything not covered, check `ol GET /` and the route's request schema before using it or calling it unsupported; when this skill and the catalog disagree, trust the catalog and say what differed. Finding a route does not authorize a write, and a website feature is not proof that the agent API exposes it.

MCP generic JSON tool results are one compact JSON text block carrying the same fields (parse it as JSON; there is no duplicated `structuredContent`), list and search tools answer a Markdown table plus a JSON paging line unless `raw=true`, and `read_document`, `read_minutes` and `harvest_context` return content as text with metadata-only `structuredContent`. Text attachment contents remain verbatim. `connection_status` reads instance health and account inventory concurrently, then reads identity and sync state for the selected account; explicit account failures never fall back to another account. These response-format and readiness behaviors do not change HTTP routes or tool arguments.

The MCP adapter coalesces overlapping dedicated `search_message_page` reads only within the same client, account and validated request body. It retains no completed-result cache. Explicit writes clear both GET and search read groups at start and completion, including uncertain failures; no write is retried.

For document and Wiki body research, use MCP `search_document_page` and read the returned sources before drawing conclusions. Preserve the selected account, query, filters and opaque pagination cursor. `openedFrom`/`openedUntil` filter when documents were opened, not their creation or modification dates; verify project dates in source text. See `references/search.md` for the request, optional upstream snippets, trace IDs and failure behavior. For report presentation, read the MCP resource `lark-connector://guides/document-presentation` (or the plugin file it serves); it is bundled Markdown without backend access or document writes.

Desktop app-link routing is a desktop owner UI capability, not an Agent API; see `references/platform.md` for routing and deployment requirements.

`POST /scan` recognizes Feishu sign-in QR payloads as `{kind:"qr-login"}` without approving or marking a login scanned. Ask the user to use the official mobile app; see `references/groups.md`. Group invitation scans still join the group.

OLK 旧账号兼容：仅服务端明确返回尚未建库时，网页使用该账号浏览器已有身份和群钥匙；不自动新建浏览器身份。已建库（含锁定）或状态查询失败时不回退。异步浏览器加密、解密和查找群钥匙完成后重新核验建库状态，变化时停止操作；发送保留草稿供重试。浏览器钥匙仍需由主人在设置中迁入密钥库，Agent 不使用浏览器钥匙，也不能代为迁移。

Storage & Retention settings show enable-preview progress immediately, allow cancellation before confirmation, and ignore late cancelled responses. Preview calculation skips message and attachment queries when the proposed policy affects no conversations. Numeric day fields use a separate localized unit. These fixes do not change Agent routes, payloads, confirmation requirements, or retention rules.

Retention cleanup honors first-activation grace and independent attachment deadlines. Temporary history fetch can restore locally pruned IDs within a scoped expiry; ordinary sync cannot resurrect them. See references/platform.md for confirmation, preview, and no-progress behavior.

`GET /retention/fetch` 的 `running` 包含目标准备阶段，可选 `error` 表示本轮任务失败；`running:false` 本身不代表成功。运行期间重复触发返回 409 `RETENTION_FETCH_IN_PROGRESS`，新一轮会清空上一轮进度及错误。网页触发补拉失败后也会立即刷新任务状态，展示准备阶段错误。详见 `references/platform.md`。

实例登录恢复仅通过宿主机交互式命令 `npm run owner:reset-password` 提供，不新增 Agent API。命令只在服务停止时执行（服务运行时拒绝）；TOTP/Passkey 必须显式选择模式；重置删除所有 Owner 会话，不能恢复 OLK 密钥库口令。操作前保留并验证可恢复的备份，具体限制和停服要求见 `references/platform.md` 的“实例登录恢复”。

Thread replies can resolve an explicitly marked root even when that root quotes another message and therefore has a `parentId`. Conversation and thread identity checks still apply; see `references/messaging.md`.

Reverse proxies may forward bodyless POST requests as chunked without a Content-Type. The server accepts these only when the body is empty; nonempty untyped bodies still return 415. JSON and authentication requirements are unchanged. See `references/platform.md`.

非 WebKit 手机布局使用 12px 安全区下限并保留更大的设备 inset，消息首页与其它页面的底部导航高度保持一致。紧凑平板仍使用侧栏；此 UI 修复不改变 Agent API，详见 `references/platform.md`。

话题详情的普通消息和回复复用主聊天的右键/长按菜单；回复目标按消息所属会话保存，系统消息不提供该菜单。PWA manifest 仍为 `orientation: any`，不按安卓 UA 强制竖屏。此 UI 行为不改变 Agent API，详见 `references/platform.md`。

OLAP「全部运行」Key 面板约束线程、提问卡及长文本的宽度，运行卡和吸底输入框保持在面板内；这只调整网页布局，没有 Agent API 或协议变化。见 `references/platform.md`。

Retention history fetch records upstream 4008 (account not in chat) per item as `RETENTION_FETCH_CHAT_UNREADABLE`, keeps `done:false`, continues other feeds, and reuses the existing seven-day unreadable-chat mark until the tail changes or the mark expires. Unknown failures still populate task/item errors. See `references/platform.md`.

Storage & Retention now shows service-wide database + allocated data-directory bytes, data-filesystem available bytes and percentage, and stored-message distribution. The website-only owner route `/api/maintenance/storage-overview?range=30|180|all` caches measurements for one minute; 30 days groups by day, 180/all by Monday-based week in Asia/Singapore. This adds no Agent route or capability. See `references/platform.md` for scope.

The storage overview keeps both numeric cards. The space bar shows only used and available bytes of the data filesystem; the database/file estimate is separate because PostgreSQL may reside elsewhere or overlap the data directory. All message ranges fit the full time span into the chart viewport (no initial horizontal clipping of recent weeks); each day/week remains its own bucket. Positive counts have a minimum visible 1 px height, while tooltips retain exact counts. This is website presentation only and changes no Agent API.

Local unread: `PUT /conversations/F/local-unread` restores the last saved read point, or `{position}` marks a cached message and later incoming messages unread only in lark-connector. It never sends an upstream receipt. Requires normal-chat badge metadata; unavailable targets return 409. See `references/chats.md`. Chat applinks with `openChatId` and optional safe-integer `position` resolve through `/link-targets/resolve`; the optional Chrome extension redirects supported chat links only (see `docs/browser-extension.md`).
