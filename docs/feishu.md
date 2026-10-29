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
| 其他飞书链接 | `LinkSheet` 的 WebView | 妙记、日程、任务、审批等链接在 App 内的网页卡片里打开 |
| AI 的飞书能力 | hub（`server/internal/lark`、助手的 `lark_api` 工具） | 搜索 / 读取消息、文档、removed、日程、妙记…… 全部只读，作为 AI 的上下文 |

链接分流在 `FeishuLinks.route(_:)`：文档 → `.doc`，removed分享（`…/client/moments/detail?postId=`）→ `.momentsPost`，其余返回 nil 走网页。
`FeishuRoute` 只剩 chat / doc / profile / momentsPost 四种，都是「从一条消息或一个链接点进去」的落点。

## 数据通道

- `GET /api/v1/lark/raw/<path>` = lark-connector `GET /agent/v1/<path>`（只读；JSON 里的 `/api/...` 媒体路径已改写回 hub）。
  `POST` 只放行 `search/page`、`search/mention-history`（只读搜索，POST 是因为 body 大）。助手的 `lark_api` 工具走同一套白名单（`lark.Read`）。
- 会话、消息、已读、发消息、表情回复、卡片按钮走 hub 的专用路由（`docs/protocol.md` 飞书 API）。

## 接口速查（AI 调 `lark_api` 时有用，全部在 hub 机器上实测过）

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
- 会话列表的坑见 `CLAUDE.md`（`isDone` 不可靠、置顶看 `onTopRankAt`、未知 scope 被当成 inbox）。
