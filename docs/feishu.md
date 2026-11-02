# 飞书在 Jarvis 里的定位

**Jarvis 不是第二个飞书。** App 的出发点是把生活和工作里的信息收到一起、交给 AI 去处理；飞书只是信息来源之一。
（2026-10-27 用户纠正：之前照着飞书 iOS 客户端做了一整套「工作台」——搜索、云文档、日历、会议、妙记、任务、审批、OKR、通讯录、
removed feed、飞书邮箱、收藏、话题、会话信息、今日——全部删除。）以后不要再在 App 里做「浏览飞书」的页面。

## 保留什么

| 能力 | 位置 | 说明 |
| --- | --- | --- |
| 飞书消息 | 「收件箱」（`docs/inbox.md`） | 未读 / @我 / 私聊和邮件混在一个列表，按件聚合，AI 分拣；点开是这段对话（`LarkChatView`） |
| 飞书文档链接 | `FeishuDocReader`（`Views/Feishu/Docs/`） | 任何地方点飞书文档链接都在 App 内原生渲染（链接卡片 `LinkSheet`、飞书消息里的链接） |
| removed链接 | `LarkMomentsCard` / `LarkMomentsPostView` | 消息里的分享显示预览卡，点开看单条帖子和评论；没有 feed，不能刷 |
| removed里和我有关的 | 收件箱（`hub/inbox_moments.go`）、知识库 | 评论我的帖子、回复我、@ 我：一个帖子一件事，AI 分拣、起草评论，点发送评论回去（`docs/inbox.md`「removed」） |
| 其他飞书链接 | `LinkSheet` 的 WebView | 妙记、日程、任务、审批等链接在 App 内的网页卡片里打开 |
| AI 的飞书能力 | hub（`server/internal/lark`、助手的 `lark_api` 工具） | 搜索 / 读取消息、文档、removed、日程、妙记…… 全部只读，作为 AI 的上下文 |

链接分流在 `FeishuLinks.route(_:)`：文档 → `.doc`，removed分享（`…/client/moments/detail?postId=`）→ `.momentsPost`，其余返回 nil 走网页。
`FeishuRoute` 只剩 chat / doc / profile / momentsPost 四种，都是「从一条消息或一个链接点进去」的落点。

## 数据通道

- `GET /api/v1/lark/raw/<path>` = lark-connector `GET /agent/v1/<path>`（只读；JSON 里的 `/api/...` 媒体路径已改写回 hub）。
  `POST` 只放行 `search/page`、`search/mention-history`（只读搜索，POST 是因为 body 大）。助手的 `lark_*` MCP 工具走同一套白名单（`lark.Read`）。
- **助手自己调 lark-connector**（2026-10-30）：用 lark-connector 自带的 lark-connector skill（仓库里的副本 `skills/lark-connector`，装在跑助手那台机器的 `~/.jarvis/skills/`），
  `ol METHOD PATH [JSON]` 直连 lark-connector 的 agent API，读写都能做；写操作先过 node 的闸门（会被别人看到的在手机上确认）。
  详见 `docs/assistant.md`「飞书：lark-connector skill」。下面的接口速查是这边实测的补充，lark-connector 的 references 更全，以它和 `ol GET /` 为准。
- 会话、消息、已读、发消息、表情回复、卡片按钮走 hub 的专用路由（`docs/protocol.md` 飞书 API）。
- 没有 lark-connector 时用官方 lark-cli 接飞书：方案见 `docs/plan-lark-cli.md`（#273，还没实现）。

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
一直失败到 jarvisd 重启。现在 `AUTH_*` 报「lark-connector 未登录飞书」（`lark.ErrNotLoggedIn`，日志、收件箱和 App 的报错里都能看到），
配置的 token 永远不丢，实时事件按退避（最长 1 分钟）重连、同一个错误只记一次日志，用户重新登录后自己恢复。

`hub.json` 的 `lark.account` 可选择默认账号（整数 `1` 为主账号，`2` 为第二账号，省略或 `0` 默认主账号），
重启 hub 后，收件箱采集、专用会话接口、媒体和常驻事件连接统一使用该账号；无需修改 iOS。
助手的新进程会通过 `lark-connector_ACCOUNT` 继承配置，已有进程需要重新启动。账号编号是 `/accounts` 的 `id`，不是飞书用户 ID。
服务端收件箱持久记录带账号编号，不同账号不会混入同一列表；旧记录视为主账号，切回后仍可加载。
不修改飞书已读状态，也不同时聚合多个账号。

raw 代理不透传客户端的 `x-lark-connector-account` 请求头，默认使用配置账号；显式 `ol_account` 查询参数仍可覆盖默认账号，
用于单次只读查询。专用会话接口不接受这个覆盖参数。不要把查询串塞进 `base_url`，客户端会在其后拼接接口路径。

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
  渲染细节见 `FeishuDocRenderer.swift` 和 `JarvisTests/FeishuDocsTests.swift`。
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

### 人 / removed / 飞书邮箱
- `users/:id`（资料卡）、`contacts/org`、`contacts/departments/:id`、`search/people?q=`。
- removed：`moments/posts/:id`、`/comments`（hub 专用路由），`moments/feed?tabId=`、`moments/notifications`。
- 飞书邮箱：`mail/threads?label=INBOX&cursor…`（label 必填）、`mail/messages/:id(/html)`；id 是 base64，路径里必须正好编码一次（`%2F`）。

### 会话
- `conversations/:feedId/settings`（公告）、`/pins`、`/resources?kind=docs|files|media`、`/todos`、`/members`；`favorites`；`threads/feed`。
- 会话列表的坑见 `docs/lessons/lark-mail.md`（`isDone` 不可靠、置顶看 `onTopRankAt`、未知 scope 被当成 inbox）。
