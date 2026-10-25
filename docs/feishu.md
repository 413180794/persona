# 飞书信息能力（工作台）

目标：把飞书 iOS 客户端里「获取信息」的能力搬进 Jarvis——功能和交互参考飞书官方源码（本机 `florak/`，已 gitignore，
只读参考，**绝不复制代码**），数据全部来自 example-machine 上的 lark-connector（经 hub 的只读代理），UI 按 Jarvis 自己的规范写。

## 架构

- **数据**：`GET /api/v1/lark/raw/<path>` = lark-connector `GET /agent/v1/<path>`（只读；JSON 里的 `/api/...` 媒体路径已改写回 hub，
  直接给 `LarkRemoteImage` / `LarkMedia` 用）；`POST /api/v1/lark/raw/search/page` 是唯一放行的 POST（只读搜索）。
  iOS 用 `api.larkRaw(_:query:)`（Decodable）、`api.larkRawJSON(_:)`（JSONValue）、`api.larkSearchPage(_:)`
  （`Views/Feishu/Foundation/FeishuFoundation.swift`）。**只做读取**：RSVP、完成任务、审批通过、发消息等写操作不做。
- **入口**：底部「工作台」Tab（`WorkbenchView`）按飞书工作台排成宫格；「信息」页右上角搜索；聊天页右上角「…」→ 搜索聊天记录 / 会话信息。
- **路由**：任何页面打开别的内容都用 `@Environment(\.feishuOpen)` 推 `FeishuRoute`（会话、会话信息、文档、个人资料、搜索、日程、会议、
  妙记、任务、审批、removed帖子、部门、知识空间、整页）；拥有 NavigationStack 的页面加 `.feishuRoutes()` 和
  `.environment(\.feishuOpen) { path.append($0) }`；`.feishuHandlesLinks` 让飞书文档链接走原生阅读器。
- **复用**：`LarkAvatarImage`、`LarkRemoteImage`、`LarkNameTag`、`LarkColor`（飞书色板）、`FeishuAppIcon`、`EmptyStateView`、
  `LarkPartsView` / `LarkMessageRow`（消息渲染）、`LarkProfileView`、`LarkImageGallery`、`LarkPlayerView`、`LarkFileOpener`、
  `MarkdownUI` 的 `.larkMessage` 主题、`FeishuLoader`、`FeishuTime`。
- **约定**：每个领域一个目录 `ios/Jarvis/Views/Feishu/<Area>/`，占位 struct 名字不变（路由靠它们）；界面中文；按钮单行；
  深色 / 浅色都截图；DEBUG 启动参数 `-tab workbench -openWorkbench <screen>` 直达页面（`FeishuScreen` 的 rawValue）。

## 领域与接口（全部已在 example-machine 实测）

### 搜索（`Search/`）
- 分类：`search/tabs` → `{tabs[{key,label}]}`（消息 / 云文档 / 应用 / 联系人 / 群组 / 邮箱 / 服务台 / 日程）；空查询首页 `search/home` →
  `{frequent[{id,kind:user|chat,name,avatarUrl,feedId}],history[string]}`。
- 分页 + 筛选：`POST search/page` body `{tab,query,cursor,limit,senderIds,chatIds,feedId,startTime,endTime,attachmentTypes,chatType,
  docTypes,ownerIds,sortType…}` → `{items,nextCursor,total}`（条目按 tab 分型、带高亮区间，见 lark-connector `src/shared/contracts/search.ts`）。
- 快速：`search?q=`（消息）、`search/people?q=`、`search/documents?q=`、`calendar/search?q=`、`mail/search?q=`、`minutes/search?q=`、
  `todos/search?q=`；会话内 `search/in-chat?feedId=&q=`。
- 点击：消息 → 会话定位到那条（`LarkConversation.focus = position`）；文档 → 阅读器；人 → 资料卡；群 → 会话；日程 → 日程详情。

### 云文档 / 知识库 / 文档通知（`Docs/`）
- 列表：`documents/recent?cursor`；`documents/drive?view=mine|shared|starred|pinned`（view 必填）；`documents/drive/discover`。
- 阅读：`documents/read?url=&format=markdown`（403 `DOCUMENT_EXPORT_FORBIDDEN` 时改 `format=json`：ProseMirror 风格节点树
  `doc/heading/paragraph/text/mention/image/columns/bookmark/horizontalRule…`，自己渲染）；`documents/header?url`、`documents/reader/info?url`、
  `documents/ai-summary?url`、`documents/comments?url`。多维表格 / 文件 / 白板返回 422：给「在飞书中打开」。
- 知识库：`wiki/spaces?scope=all|starred|recent`、`wiki/spaces/:id/tree`、`/children?node`、`/updates`。
- 通知：`documents/notifications?cursor` → `{items[{title,url,kind:mention|comment|reply|solve|share|reopen|reaction,summary,atMe,unreadCount,updatedAt}]}`。

### 日历 / 会议 / 妙记（`Calendar/`）
- 日程：`calendar/week?start&end`（ISO，≤62 天）→ `{calendars,events[{id,title,startAt,endAt,allDay,selfAttendeeStatus,calendarId}]}`；
  详情 `calendar/event/:id`（参会人 + 回复状态 1 待定 / 2 接受 / 3 暂定 / 4 拒绝、category 2 = 会议室、`attendeeSummary`、`meetingUrl`、
  会议号、文档、`meetingNotes`、提醒）；搜索 `calendar/search?q`。
- 会议：`meetings?count&cursor` → `{items[{historyId,topic,sortedAt,artifacts}]}`；`meetings/:historyId`（参会人、录制、文档）；
  `meetings/:historyId/insights`（AI 纪要：要点、高亮、待办）。
- 妙记：`minutes?scope=all&cursor`；`minutes/:token`（摘要、关键词、章节、待办）；`minutes/:token/transcript`（段落 + 说话人）；
  `minutes/:token/summary-items`；`minutes/:token/media`（可流式播放）。

#### 实现说明（日历）

- 文件：`CalendarModels.swift`（线上类型 + 纯逻辑，单测 `JarvisTests/FeishuCalendarTests.swift`）、`CalendarShared.swift`（卡片、三态、
  路由兜底）、`FeishuCalendar.swift`（日历首页）、`FeishuEventDetail.swift`（日程详情）。
- `calendar/week` 的 `start` / `end` 用 UTC（`…Z`）：带 `+08:00` 时 `+` 在查询串里被当成空格，直接 400。按周（周一起）取、按周缓存，
  预取前后一周（周条的圆点立刻有）。
- 重复日程的实例 id 是 `<sourceId>:<开始秒>`，`calendar/event/<实例id>` 回 400 `WEB_MGET_EVENTS_WITH_IDS`：详情一律用 sourceId 取，
  再把开始 / 结束改成实例的（路由 `.event(id:)` 直接传实例 id，详情页自己拆）。
- 参与者：`attendees` 里 category 1 人、2 **群**（不是会议室，`groupMemberCount`）、3 会议室（status 2 已订 / 4 预定失败 / 5 已释放）。
  经群邀请的人只在 `calendar/event/:id/guests`（分页 `nextCursor`，含 `groups`）和 `…/guests/groups/:chatId?calendarId=` 里，没有头像。
  `attendeeSummary` 只算本账号可见的人，可能远小于实际名单（见过 total 1、名单 60 人），名单更多时按名单计数。
- 头像：`avatarKey` 走 `/api/v1/lark/media/<key>`（hub 已支持）；会议 / 妙记接口给的是 CDN 完整 URL。
- `description` 常带一行 `AI-translated Title` 前缀，显示前去掉。会议室名形如 `Beijing-Dinghao DH3(鼎好DH3)-F17-04🎦 (12)`，
  拆成「F17-04 · 12 人」+ 楼宇。
- 只读：不做 RSVP / 编辑；「加入会议」只是打开 `meetingUrl`。
- **共享代码的坑**：工作台 NavigationStack 把 `feishuOpen` 设在根视图上（栈内），push 出来的页面拿到的是 nil。本目录用
  `FCRouter`（`.fcRoutes()`）兜底：环境里没有时本页自己 `navigationDestination(item:)`。正确修法是把
  `.environment(\.feishuOpen)` 挪到 `NavigationStack` 外面（leader 改）。
- DEBUG：`-calendarDate 2026-10-22`（打开那天）、`-calendarEvent <id>`（直接推日程详情，可用实例 id）、`-fcBottom YES`（详情页停在底部，截下半页）。

#### 实现说明（会议）

- `FeishuMeetings.swift`：列表 `meetings?count=30&cursor`（`filter=calls` 只看通话），按 `sortedAt` 分天；`call.outcome` 非 accepted 标红。
  `artifacts`（minutes / notes / docs / recording）显示成标签。
- 详情 `meetings/:historyId` 与 `meetings/:historyId/insights` 并发取，纪要失败不影响详情。`recordings` 与 `documents` 里的妙记 / 录制
  是同一个链接，按妙记 token 去重；妙记链接（`…/minutes/<token>`）推原生妙记页。
- insights 的 `aiSummary.todos` 只是「我的待办」；`minutes.access` 常为 `denied`（别人录的会），此时不显示妙记章节。
- 参会人头像是 CDN 完整 URL；`kind=room` 的是会议室设备。DEBUG：`-meetingID <historyId>`（从会议列表直接推详情）。

### 任务 / 审批 / OKR（`Work/`）
- 任务：`todos?count`（我的未完成）、`todos/completed`、`todos/search?q`、`todos/:guid/detail`、`/subtasks`、`/comments`、`task-list-nav/sidebar`。
- 审批：`approvals/counts`、`approvals?tab=todo|done|initiated|cc…&cursor`、`approvals/detail?instanceId`（表单、流程、时间线）。
- OKR：`okr/people`、`okr/owners/:ownerId/periods`、`okr/:okrId`（目标、KR、进度、得分）。

### 通讯录 / removed / 飞书邮箱（`People/`）
- 通讯录：`contacts/org` → 根部门；`contacts/departments/:id`（面包屑、负责人、子部门、成员）；`conversations/groups`（我的群组）；
  `contacts/external`；`contacts/bots`；`contacts/closest/people`；`people/:id/report-line`。
- removed：`moments/tabs`、`moments/feed?tabId=&pageToken`、`moments/notifications`、`moments/badge`、`moments/broadcasts`；帖子用已有的 `LarkMomentsPostView`。
- 飞书邮箱：`mail/labels`、`mail/threads?label=INBOX&cursor…`（label 必填）、`mail/threads/:threadId`、`mail/messages/:id(/html)`、`mail/search?q`。

### 会话信息 / 收藏 / 话题（`Chat/`）
- 会话信息：群公告 `conversations/:feedId/settings`（`announcement` 或 `announcementDoc`）；Pin `conversations/:feedId/pins`、`top-pins`；
  资源 `conversations/:feedId/resources?kind=docs|files|media&cursor`；群待办 `conversations/:feedId/todos`；群妙记 `conversations/:feedId/meeting-minutes`；
  成员数等从 settings 拿；入口「搜索聊天记录」。
- 收藏：`favorites`（完整消息 + `target{messageId,position}`，点开定位）。
- 话题：`threads/feed?cursor` → `{items[{feedId,conversationName,threadId,root,lastReply,replyCount,unreadCount}]}`，点开用 `LarkThreadView`。

#### 实现说明（`Chat/`）

- 文件：`FeishuChatModels.swift`（线上类型 + 纯逻辑，单测在 `JarvisTests/FeishuChatTests.swift`）、`FeishuChatShared.swift`
  （`feishuChatChrome()`：定位到消息、话题弹层、文件 Quick Look、资料卡；消息卡片；分页尾；三态面板）、`FeishuChatInfoView.swift`
  （会话信息 + Pin / 群任务 / 群成员子页）、`FeishuChatResources.swift`（文档 / 文件 / 图片视频行和整页）、`FeishuSavedView.swift`、`FeishuThreadsView.swift`。
- **消息是 lark-connector 原样的 camelCase `MessageView`**（不是 hub 的 snake_case `LarkMessage`）：`FeishuRawMessage.lark` 照
  hub 的 `rawMessage.normalize` 转一遍，就能直接用 `LarkPartsView` / `LarkThreadView`。头像 URL 有的是飞书 CDN 绝对地址，`LarkMedia` 两种都能拉。
- **定位到消息**的规则（`FeishuMessageTarget`）：position 为正 → 打开会话 `focus = position`；position 为负（只存在于话题里的回复，
  例如 `-3`）且有 threadId → 读 `messages/<threadId>`（返回 `{item, attachments}`）作为根消息弹 `LarkThreadView`；只有消息 id → 先读
  `messages/<id>` 再按前两条走。文件 / 图片的 `messageUrl` 是 `/messenger/<feed>?message=&position=&thread=`；**文档没有 messageUrl**，
  只有 `sourceMessageId`，要多读一次消息。收藏的 `target.threadId` 有值时直接开话题。
- `settings`：每个群都有 `announcementDoc`（哪怕公告是空的），没有头像字段（用会话自带的）；单聊的 `name` 是「私聊」，要用会话名；
  `userCount` 是去掉机器人的人数，`memberCount` 含机器人。公告预览：`documents/read?url=&format=markdown` 取前几行，去掉空标题、
  lark-connector 加的 `[Source](…)` 行、markdown 转义和链接语法。
- `pins`：`{items[{id,createdAt,operatorName,operatorGroupNickname,message}],nextCursor,hasMore,total}`，`limit` ≤ 100；`top-pins`
  （群顶栏：消息 / 链接 / 公告，`type 3` = 公告）实测各群都是空的，`total` 却是 Pin 总数，不能当顶栏数量用。
- `resources?kind=docs|files|media&cursor&q&locale`：cursor 是一大段 JSON 字符串，原样回传；`q` 服务端过滤（整页搜索框用它）；
  标题带 HTML 实体（`&amp;`）；文件 `downloadUrl` 已改写成 hub 的 `/api/v1/lark/raw/conversations/.../files/...`，`LarkMedia.file` 直接下载进 Quick Look；
  媒体只有 `thumbnailUrl`（不带 size 即原图），视频没有播放地址 → 点视频定位到消息播放。
- `todos` → `{open, closed}`，只有负责人 id 没有名字；进行中按「我负责 → 截止时间 → 新建时间」排。`meeting-minutes` 只有日程群有。
- `members?limit=` 一次给前 N 个（`hydrateMembers`），搜索用 `members/search?q=&limit≤200`。
- `favorites`：只有 `nextCursor`（最后一个收藏 id）；`kind=merge` 的合并收藏没有 message，只显示来源。老收藏可能没有 `conversationName`，
  单聊（`chatType 1`）用发送者名字兜底。2020 年的图片缩略图已过期，显示破图占位。
- `threads/feed`：cursor 是 `<rankTimeMs>:<threadId>`，`hasMore` 为 false 时停；只读，不上报已读，点开后只在本地清掉未读角标。
- **坑：从工作台推入的页面拿不到 `feishuOpen`**（实测为 nil：WorkbenchView 把它设在栈根内容上，`navigationDestination` 推出的页面不继承），
  所以 `feishuChatChrome()` 在拿不到时用本地 `navigationDestination(item:)` 推 `FeishuRouteView`，并重新注入 `feishuOpen` 给子视图。
- List 的 insetGrouped 行有很大的圆角裁剪：行内放自带圆角描边的卡片会被切角，快捷入口改成一张卡里四个无边框按钮。
- DEBUG 启动参数：`-chatInfo <feedId>`（在收藏 / 话题页上自动打开该会话的会话信息）、`-chatInfoPage pins|docs|files|media|todos|members`
  （再打开子页）、`-chatInfoFrom pins|media|docs|files|todos`（会话信息从该区块开始显示，截下半页用）、`-threadOpen <n>`（话题页打开第 n 个话题）。

### 今日（`Today/`，leader 最后做）
聚合：今天的日程（`calendar/week`）、待办（`todos`）、审批待办数、未读 @我（`conversations/unread-mentions`）、文档通知、关注话题的新回复。
