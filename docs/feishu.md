# 飞书在 Persona 里的定位

> **2026-11-04 起（#1520，插件第 3 步 3c）**：经 lark-connector 接飞书的整条链路（App 的 `/api/v1/lark/…`、收件箱和知识库的飞书来源、公司圈、
> 助手的 `ol` 闸门和凭据、/agent、个人 Agent）不在开源仓库里了，由接飞书的插件提供（`docs/extensions.md`，`docs/plan-extensions.md`「第 3 步」）。
> 下文描述的行为由插件照原样提供；文中提到的 hub 源文件（`hub/lark*.go`、`internal/lark` 的客户端……）已删。开源版只留官方 lark-cli、飞书机器人、飞书邮箱。

**Persona 不是第二个飞书。** App 的出发点是把生活和工作里的信息收到一起、交给 AI 去处理；飞书只是信息来源之一。
（2026-10-27 用户纠正：之前照着飞书 iOS 客户端做了一整套「工作台」——搜索、云文档、日历、会议、妙记、任务、审批、OKR、通讯录、
公司圈 feed、飞书邮箱、收藏、话题、会话信息、今日——全部删除。）不恢复这些功能页面。

**#392 的明确例外**：用户要求在飞书设置增加默认关闭的「首页显示消息」开关，开启后首页增加「消息」，浏览全部私聊与群聊。
它与收件箱共用账号、聊天页、发件箱和同步，不增加第二套实现；其他飞书功能页仍不迁入。详见 [飞书能力整合](lark-connector-migration.md)。

## 保留什么

| 能力 | 位置 | 说明 |
| --- | --- | --- |
| 飞书消息 | 「收件箱」（`docs/inbox.md`）及可选的「消息」页 | 未读 / @我 / 私聊和邮件混在一个列表，按件聚合，AI 分拣；点开是这段对话（`LarkChatView`） |
| 飞书文档链接 | `FeishuDocReader`（`Views/Feishu/Docs/`） | 任何地方点飞书文档链接都在 App 内原生渲染（链接卡片 `LinkSheet`、飞书消息里的链接）。插件声明 `lark_doc_comments` 时，评论页能回复线程、评论全文、标为已解决 / 重新打开、@ 人（#1290，`FeishuDocsComments.swift`；接口见 `docs/protocol.md` 飞书 API） |
| 公司圈链接 | `LarkMomentsCard` / `LarkMomentsPostView` | 消息里的分享显示预览卡，点开看单条帖子和评论；没有 feed，不能刷 |
| 公司圈里和我有关的 | 收件箱（`hub/inbox_moments.go`）、知识库 | 评论我的帖子、回复我、@ 我：一个帖子一件事，AI 分拣、起草评论，点发送评论回去（`docs/inbox.md`「公司圈」） |
| 其他飞书链接 | `LinkSheet` 的 WebView | 妙记、日程、任务、审批等链接在 App 内的网页卡片里打开 |
| AI 的飞书能力 | hub（`server/internal/lark`、助手的 `lark_api` 工具） | 搜索 / 读取消息、文档、公司圈、日程、妙记…… 全部只读，作为 AI 的上下文 |
| 在飞书里找 Persona | 飞书机器人（`docs/bots.md`） | 私聊自建应用的机器人 = 和主对话说话；不是飞书功能的搬运，是 Persona 多一个入口 |

链接分流在 `FeishuLinks.route(_:)`：文档 → `.doc`，公司圈分享（`…/client/moments/detail?postId=`）→ `.momentsPost`，其余返回 nil 走网页。
`FeishuRoute` 只剩 chat / doc / profile / momentsPost 四种，都是「从一条消息或一个链接点进去」的落点。

## 数据通道

- `GET /api/v1/lark/raw/<path>` = lark-connector `GET /agent/v1/<path>`（只读；JSON 里的 `/api/...` 媒体路径已改写回 hub）。
  `POST` 只放行 `search/page`、`search/mention-history`（只读搜索，POST 是因为 body 大）。助手的 `lark_*` MCP 工具走同一套白名单（`lark.Read`）。
- **助手自己调 lark-connector**（2026-10-30）：用 lark-connector 自带的 lark-connector skill（仓库里的副本 `skills/lark-connector`，装在跑助手那台机器的 `~/.persona/skills/`），
  `ol METHOD PATH [JSON]` 直连 lark-connector 的 agent API，读写都能做；写操作先过 node 的闸门（会被别人看到的在手机上确认）。
  详见 `docs/assistant.md`「飞书：lark-connector skill」。下面的接口速查是这边实测的补充，lark-connector 的 references 更全，以它和 `ol GET /` 为准。
- 会话、消息、已读、发消息、表情回复、卡片按钮走 hub 的专用路由（`docs/protocol.md` 飞书 API）。
- **没有 lark-connector 时用官方 lark-cli**（`docs/plan-lark-cli.md`，#273）：已经做了助手侧（hub.json `lark.cli`，助手的 PATH 上有 `lark-cli`，
  写操作过同一个闸门，`docs/assistant.md`「飞书：lark-cli」）；收件箱、知识库、`/agent`、App 的飞书页面还只认 lark-connector，按方案的 L2–L6 逐步加。

## lark-connector 与 hub 分机部署

lark-connector 的 Agent API 保持只监听源端回环地址。源端能主动访问 hub 时，可用专用 SSH 用户建立反向隧道，
把 hub 的回环端口转发到源端 Agent API；由源端用户级 systemd 服务维护，配置 `Restart=always`、
`ExitOnForwardFailure=yes` 和 SSH keepalive，并开启 linger，避免依赖开发机在线。源端必须固定 hub 的 SSH host key。

hub 上的隧道用户只允许公钥认证和 remote forwarding：`GatewayPorts no`、`PermitListen 127.0.0.1:<隧道端口>`、
`PermitOpen none`、`MaxSessions 0`；禁用 TTY、agent、X11 和 Unix socket 转发。公钥另加
`restrict,port-forwarding,permitlisten="127.0.0.1:<隧道端口>"`，使用 `ssh -N -T` 连接。
用 `sshd -t` 与 `sshd -T -C user=<隧道用户>,host=localhost,addr=127.0.0.1` 核对生效范围，再 reload，避免影响管理账号。

hub 的 `hub.json` 配置 `lark.base_url=http://127.0.0.1:<隧道端口>/agent/v1` 和 `lark.token`，文件保持 0600。
Token 从源端环境安全传递，不打印、不入库；变更前备份配置。验证回环监听、无凭据 health 返回 401、
授权 health 和会话读取，以及 `/events` WebSocket；重启后再次检查。验证过程不需要发送消息或更改已读状态。

lark-connector 和 hub 在同一台机器的 docker 里、又不想把 token 写进配置时，可以只配 `lark.container`（容器名，没有默认值），
hub 从容器环境读 `lark-connector_AGENT_TOKEN`；容器重建换了 token，401 后会重新读。

401 有两种，要分开（2026-10-31 踩过）：lark-connector 自己掉了飞书登录时回 401 + `AUTH_SESSION_EXPIRED` 等 `AUTH_*` 错误码，
token 是好的；以前 hub 把所有 401 都当 token 错，丢掉配置里的 token 去 `docker inspect` 一个默认容器，之后所有飞书请求和实时事件
一直失败到 personad 重启。现在 `AUTH_*` 报「lark-connector 未登录飞书」（`lark.ErrNotLoggedIn`，日志、收件箱和 App 的报错里都能看到），
配置的 token 永远不丢，实时事件按退避（最长 1 分钟）重连、同一个错误只记一次日志，用户重新登录后自己恢复。

## 多账号

lark-connector 一个实例里可以登录多个飞书账号（它的 x-multi-account：`GET /agent/v1/accounts` 列出，请求用 `x-lark-connector-account`
头或 `ol_account` 参数选账号，账号编号是 `/accounts` 的 `id`，不是飞书用户 ID；添加账号的账号的资源 URL 自带 `ol_account`）。
Persona 都读（#1321，之前只读 `lark.account` 一个）：

- **默认账号** = `hub.json` 的 `lark.account`（省略是主账号 1）。不带 `ol_account` 的 `/api/v1/lark/*` 请求是它的，旧客户端（Android、网页）
  照旧只看到它。助手（`lark_*` 工具、`ol`）、知识库、`/agent`、风灵月影设置、文档阅读也只用它；助手的 `lark_messages` 可传 `account`
  读别的账号的会话，`ol` 前加 `lark-connector_ACCOUNT=<编号>`。
- **按请求选账号**：`/api/v1/lark/*` 全部认 `ol_account=<编号>`（`hub/lark_accounts.go` 的 `larkMux`，每个账号一个 `lark.Client`，
  `Client.ForAccount`）；编号不合法回 400，不存在由 lark-connector 拒绝，不会落回默认账号。媒体 URL 里带的 `ol_account` 原样带回来就对了。
  raw 代理把查询原样转给 lark-connector，客户端的 `x-lark-connector-account` 头不透传。不要把查询串塞进 `base_url`。
- **账号列表** `GET /api/v1/lark/accounts`：每分钟最多读一次 lark-connector，用来决定收件箱和实时事件读哪些账号（已登录、没被隔离的）。
  没有 `/accounts` 的旧 lark-connector 当作只有默认账号。
- **实时事件**：默认账号一条连接，其他已登录账号每两分钟按账号列表开 / 关各自的连接。推给 App 的 `lark` 事件带 `account`，
  非默认账号的再带 `ol_account`（App 据此定位会话缓存）。`/agent`、个人 Agent、知识库只跟默认账号的事件。
- **收件箱**：每轮依次读每个已登录账号的未读和 @我；默认账号出错是这一轮的错，其他账号出错只记日志。条目带 `lark.account_id`
  （旧记录为 0 = 账号 1）、`lark.ol_account`（不是默认账号时，App 打开会话、回复要带的那个），同一个群在两个账号里是两件；
  多于一个账号时来源写「飞书 · <名字>」。回复以条目所属的账号发出。不修改飞书已读状态。
- **App（iOS）**：`APIClient.lark(_:)` 给 `/api/v1/lark/` 请求加 `ol_account`；`LarkConversation.account` 跟着会话走（聊天、话题、资料卡、
  卡片按钮、@成员、链接预览都按它），缓存和监听按 `LarkConversation.key`（`a<编号>-<feed>`）分开。「消息」页多于一个账号时
  列表上方一排账号胶囊（带未读数），各账号的列表和置顶分开缓存；聊天页副标题写「飞书 · <名字>」。设置 › 连接器 › 飞书列出所有账号。
- **网页**：`web/src/lib/lark-accounts.ts`（账号列表、选择记在这个浏览器、`larkPath` 加 `ol_account`）。`LarkChatRef.account` 跟着会话走
  （读取、已读上报、发送、回复、话题、Pin 都按它，`lark` 推送只认同一账号的）；「消息」页搜索框下一排账号按钮，每个账号一份列表
  （`larkChatsFor`），会话头写「群聊 · <名字>」；收件箱条目按 `lark.ol_account` 打开；设置 › 连接器 › 飞书列出账号。
  端到端：`scripts/test/web_lark_accounts_e2e.py`（`MOCK_LARK_ACCOUNTS=1` 的 mock）。Android 还没有账号切换，只用默认账号。

## 接口速查（hub 机器上实测过；助手现在用 lark-connector skill，lark-connector 的 references 为准）

### 搜索
- `search/page`（POST）body `{tab,query,cursor,limit,senderIds,chatIds,feedId,startTime,endTime,attachmentTypes,chatType,docTypes,ownerIds,sortType}`
  → `{items,nextCursor,total}`；tab 来自 `search/tabs`（消息 / 云文档 / 应用 / 联系人 / 群组 / 邮箱 / 服务台 / 日程）。
  `query` 不能为空（400）；时间是 Unix 秒；`nextCursor` 是几 KB 的 token。
- 快速搜索：`search?q=`（消息）、`search/people?q=`、`search/documents?q=`、`calendar/search?q=`、`mail/search?q=`、`minutes/search?q=`、
  `todos/search?q=`（翻页参数 `token`）；会话内 `search/in-chat?feedId=&q=`。
- 高亮区间是 UTF-16 偏移；消息命中的 `position` 可能是负数（话题里的回复），要 `GET messages/:id` 拿 `threadId` 再定位。
- 日程命中的 `id` 是 `<eventId>:<实例开始时间>`，`calendar/event/:id` 只认冒号前那段。

### 文档
- `documents/read?url=&format=markdown`；403 `DOCUMENT_EXPORT_FORBIDDEN` / 422 `DOCUMENT_FORMAT_UNSUPPORTED` 时改 `format=json`
  （ProseMirror 风格节点树）。多维表格 / 文件 / 画板给「在飞书中打开」。另有 `documents/header`、`documents/reader/info`、
  `documents/ai-summary`（只读缓存）、`documents/comments`。
- Markdown 导出开头固定是 `# 标题` + `[Source](<url>)`；图片是 lark-connector 的绝对地址，客户端改写到 `/api/v1/lark/raw/documents/…`。
- JSON 树：每个列表项单独一个 `bulletList` / `orderedList`，编号要跨兄弟节点连续数；表格 `colwidth` 是像素，`rowspan` 占位要自己补。
  渲染细节见 `FeishuDocRenderer.swift` 和 `PersonaTests/FeishuDocsTests.swift`。
- `authority.copy=false` 时正文不可选择。

### 日历 / 会议 / 妙记
- `calendar/week?start&end`（UTC `…Z`，带 `+08:00` 会 400）；`calendar/event/:id`（重复日程用 sourceId）。
- `meetings?count&cursor`、`meetings/:historyId`、`meetings/:historyId/insights`（AI 纪要）。
- `minutes?scope=all&cursor`、`minutes/:token`、`/transcript`、`/summary-items`、`/media`（hub 转发 Range，可流式播放）。

### 任务 / 审批 / OKR
- `todos?count`（无游标，上限 200）、`todos/completed`、`todos/:guid/detail`（没有标题，要从列表找）、`task-lists`。
  全天任务截止日是那天的 UTC 零点。
- `approvals?tab=todo|done|initiated|ccUnread|ccRead&cursor&q`、`approvals/detail?instanceId`（外部系统审批 502 `APPROVAL_DETAIL_UNREADABLE`）。
- `okr/people`、`okr/owners/:ownerId/periods`、`okr/:okrId`。

### 人 / 公司圈 / 飞书邮箱
- `users/:id`（资料卡）、`contacts/org`、`contacts/departments/:id`、`search/people?q=`。
- 公司圈：`moments/posts/:id`、`/comments`（hub 专用路由），`moments/feed?tabId=`、`moments/notifications`。
- 飞书邮箱：`mail/threads?label=INBOX&cursor…`（label 必填）、`mail/messages/:id(/html)`；id 是 base64，路径里必须正好编码一次（`%2F`）。

### 会话
- `conversations/:feedId/settings`（公告）、`/pins`、`/resources?kind=docs|files|media`、`/todos`、`/members`；`favorites`；`threads/feed`。
- 会话列表的坑见 `docs/lessons/lark-mail.md`（`isDone` 不可靠、置顶看 `onTopRankAt`、未知 scope 被当成 inbox）。

### 话题分页限制（H06）

2026-10-31 只读核对 lark-connector 源码，版本 `2da814b1da1fd2ddd0cb9df05c419f7c87421739`：
`GET /agent/v1/messages/:messageId/thread` 支持最新页和 `beforePosition` 前翻，**不支持向后读取更新的一页**。依据如下：

- `src/server/routes/api.ts` 的话题路由只读取 `beforePosition`、`limit`，将前者传给 `hydrateThread` 和 `threadPage`；传入 `afterPosition` 会被忽略，无 `beforePosition` 时仍按最新页处理。
- `src/server/lark/domains/im-domain.ts` 的 `pullThread` 只接收 `beforePosition`，从话题末尾或指定位置之前拉取回复；`src/server/services/messenger-service.ts` 的话题读取链路也只传递这个方向。
- `src/server/repositories/messenger-repository.ts` 的 `threadPage` 按 `threadPosition < beforeThreadPosition` 查更早回复，先倒序取页再反转；返回 `items`、`hasOlder`、`nextOlderCursor`，没有 `hasNewer` 或 `nextNewerCursor`。

因此 hub 的话题路由暂不增加 `after`。普通会话消息接口支持 `afterPosition`，但话题回复使用独立的 `threadPosition`，不能借用普通会话游标或把最新页追加成连续页。Android 保留「↓ 最新回复」重新读取最新页并替换当前窗口；前翻与回复后的刷新维持原行为。待上游补齐更新方向的拉取、查询及游标契约后，再接向后分页。本次未连接真实飞书或线上 hub，结论来自上述源码核对。
