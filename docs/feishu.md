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

#### 实现（`Search/`）

- 文件：`FeishuSearch.swift`（页面 + `FeishuSearchStore`）、`FeishuSearchRows.swift`（各分类结果行、高亮文本、骨架屏）、
  `FeishuSearchFilters.swift`（筛选条、选人 / 选会话、自定义时间）、`FeishuSearchMail.swift`（邮件结果的阅读页）、
  `FeishuSearchModels.swift`（线上模型和纯逻辑：请求体、高亮、最近搜索合并、分页合并，单元测试 `JarvisTests/FeishuSearchTests.swift`）。
- 分类：综合（客户端自己并发取 联系人 / 群组 / 消息 / 云文档 / 日程 / 应用 各前几条，「查看全部」切到该分类）+ `search/tabs`
  返回的服务端分类（只渲染认识的 key，去重）+ 妙记（`minutes/search`）+ 任务（`todos/search`）。会话内搜索的分类是固定的：
  消息（`feedId`）、云文档（`tab=docs` + `chatIds=[feedId]`）、图片 / 视频 / 文件 / 链接（`feedId` + `attachmentTypes` 1/2/3/4）。
- 筛选：消息 = 发送者 / 所在会话 / 时间；会话内 = 发送者 / 时间；云文档 = 类型 / 所有者 / 排序。只发当前分类提供的筛选（照 lark-connector
  `pageInput`）。时间是 **Unix 秒**；「最近一周」从选择那一刻算起（翻页时窗口不变）；自定义按本地整天。
- 首页：最近搜索 = 本机搜过的在前 + `search/home.history`（服务端历史只读，「清除」只是在本机隐藏）；常用联系人和群组点开直接进会话
  （user 也带 `feedId`），长按看资料；「搜索指定内容」宫格选分类。
- 输入即搜（350ms 防抖），切分类 / 筛选立即搜；同分类换词时旧结果先变淡不清空（不闪骨架屏）。分页到最后一行自动加载，失败可点重试。

接口怪异行为：
- **高亮区间是 UTF-16 偏移**（JS 字符串下标），`[start, end)`；可能为空数组（群组常见），此时客户端按查询词自己匹配。
  消息 snippet 里有 markdown（`## `、反引号、`**`），去掉标记时要同步平移区间（`FeishuHighlight.plain`），直接 `Fmt.plain` 会高亮错位。
  snippet 可能自带 `...` 前缀。
- **消息命中的 `position` 可能是 -3**：话题里的回复不在主聊天流里。点开时先 `GET messages/:id`（拿 `feedId`、`chatType` 1 单聊 / 2 群 / 3 话题群、
  `threadId`），position 为负就再取 `messages/:threadId` 用话题根的位置定位。`search/page` 的消息命中没有发送者头像。
- `search/page` 的 `query` 不能为空（400）；`nextCursor` 是几 KB 的 token 并且每页变长，所以是 POST。邮箱 / 应用等有时给 `nextCursor` 不给 `total`。
- 日程命中的 `id` 是 `<eventId>:<实例开始时间>`，`calendar/event/:id` 只认冒号前那段（整串 400）；详情返回的是重复日程的母日程时间。
- `minutes/search` 用偏移 cursor（"15"、"30"…），到底后 cursor 可能原样返回：页为空或 cursor 不变即停。`todos/search` 翻页参数叫 `token`
  （返回 `nextToken` + `hasMore`）。
- 应用命中多数只有 `lark://` / `sslocal://` 入口，只有装了飞书 App 才打得开；服务台没有可用入口，只展示。
- 邮件结果没有现成路由，搜索自带阅读页：`mail/messages/:id`（纯文本 body，可能截断）+ `/html`（完整 HTML，newsletter 是固定宽度表格，
  注入样式压到屏宽）。
- `feishuOpen` 设在 NavigationStack 根视图上，**传不到被推入的页面**（工作台 › 搜索里它是 nil）。搜索页自己兜底：没有 `feishuOpen` 时
  用 `navigationDestination(item:)` 推，并给推出的页面注入下一层的 opener（`FeishuSearchPushedRoute`）。根治要在 `WorkbenchView` /
  其他宿主把 `.environment(\.feishuOpen)` 挂到 NavigationStack 外层。
- DEBUG 启动参数（名字按 CLAUDE.md 规则用 `open…` 前缀，需加进 release.sh 的检查列表）：`-openSearch <q>`、`-openSearchTab <key>`
  （all / messages / docs / contacts / groups / calendar / email / apps / helpdesk / minutes / tasks；会话内 chat / chatDocs / chatImages…）、
  `-openSearchFeed <feedId>`（会话内搜索）、`-openSearchFilter sender|chat|owner|time`（打开筛选面板）、`-openSearchFirst YES`（结果加载后点开第一条）。

### 云文档 / 知识库 / 文档通知（`Docs/`）
- 列表：`documents/recent?cursor`；`documents/drive?view=mine|shared|starred|pinned`（view 必填）；`documents/drive/discover`。
- 阅读：`documents/read?url=&format=markdown`（403 `DOCUMENT_EXPORT_FORBIDDEN` 时改 `format=json`：ProseMirror 风格节点树
  `doc/heading/paragraph/text/mention/image/columns/bookmark/horizontalRule…`，自己渲染）；`documents/header?url`、`documents/reader/info?url`、
  `documents/ai-summary?url`、`documents/comments?url`。多维表格 / 文件 / 白板返回 422：给「在飞书中打开」。
- 知识库：`wiki/spaces?scope=all|starred|recent`、`wiki/spaces/:id/tree`、`/children?node`、`/updates`。
- 通知：`documents/notifications?cursor` → `{items[{title,url,kind:mention|comment|reply|solve|share|reopen|reaction,summary,atMe,unreadCount,updatedAt}]}`。

#### 实现说明（`Docs/`）

| 文件 | 内容 |
| --- | --- |
| `FeishuDocsModels.swift` | 线上类型 + 纯逻辑（按天分组、类型筛选、文件夹优先、知识库树展开、Markdown 切块 / 目录、有序列表编号、表格合并单元格布局、错误码），单测在 `JarvisTests/FeishuDocsTests.swift` |
| `FeishuDocsComponents.swift` | `api.larkDocs`（保留 lark-connector 错误码的 GET）、游标分页 `FeishuDocPager`、文档图标 / 行 / 筛选条 / 三态 |
| `FeishuDocs.swift` | 云文档首页（搜索、知识库 / 文档通知入口、快速访问、最近 / 共享给我 / 收藏 / 我的空间 / 推荐）、文件夹页 |
| `FeishuDocReader.swift` | 阅读器：标题、位置、所有者、更新时间、阅读数、AI 摘要、目录、评论、在飞书中打开；多维表格 / 文件 / 画板页 |
| `FeishuDocRenderer.swift` | JSON 节点树原生渲染（段落、标题、列表、待办、引用、高亮块、分栏、代码、表格、图片、书签、嵌入、附件、任务…） |
| `FeishuWiki.swift` / `FeishuDocNotifications.swift` | 知识库首页 / 空间（目录树、最近更新）、文档通知 |

- 阅读顺序：`format=markdown` → 403 `DOCUMENT_EXPORT_FORBIDDEN` 或 422 `DOCUMENT_FORMAT_UNSUPPORTED` 时 `format=json`。
  JSON 里带 `base` 是多维表格（不渲染，给「在飞书中打开」）；`object.kind=file` 是云空间文件（「预览文件」走
  `documents/drive/files/:token/download` + Quick Look）；`whiteboard` 显示 `object.image`。`/base/` 链接直接判定，不去读几 MB 的表数据。
- hub 的 `APIClient.request` 只认 `{"error":"…"}`，lark-connector 的错误是 `{"error":{"code":…}}`，所以 `Docs/` 用自己的 `larkDocs` 拿错误码。
- Markdown 导出的怪处：开头固定是 `# 标题` + `[Source](<url>)`（阅读器自己画标题，要去掉）；图片是**绝对地址**
  `http://<lark-connector>:3333/api/documents/…`，hub 的改写只换 JSON 字符串开头的 `"/api/`，所以客户端把它改成 `/api/v1/lark/raw/documents/…`；
  标题里有 `\-`、`\+` 之类的转义（目录要去转义）；没有 outline，自己按 `#` 行（跳过代码块）切块，每块一个懒加载行，目录按块 id 跳转。
- JSON 树的怪处：docx 的每个列表项都是**单独一个** `bulletList` / `orderedList`（只有一个 `listItem`），有序编号要跨兄弟节点连续数，
  `restart` 时从 `start` 重来；`outline` 的 id 带 `doc-block-` 前缀，和 heading 的 `attrs.id` 一致；文字颜色是 CSS（`#hex` / `rgba()`），
  深色模式下近黑色文字改用系统前景色、背景色降不透明度；表格 `colwidth` 是像素，夹在 72…320，`rowspan` 占位要自己补空格子。
- `authority.copy=false` 时正文不可选择（尊重作者的禁止复制）。
- `documents/drive?view=` 必填；`pinned`（快速访问）不分页；`discover` 的 `updatedAt` 是空串（`JSON.decoder` 会解成 1970，时间字段一律存字符串再解析）；
  最近列表里 type 113 是 `/page/` 链接（飞书页面），按链接认类型。文件夹：`documents/drive?view=mine|shared&folder=<token>`，返回 `path` 面包屑。
- 评论 `documents/comments` 的 `reader/info.comments` 数的是回复总数，线程数更少；回复里的图片只给 token，只显示「N 张图片」。
- AI 摘要只读缓存（`state=ready` 才显示，`missing` 不触发生成）；文档通知接口在当前账号下返回空列表（已按 contract 实现，显示空状态）。
- 知识库：`wiki/spaces?scope=recent` 的 `kind` 全是 `other`；树一次只给根，子级 `children?node=` 展开时整层读完（游标循环）；
  快捷方式节点的子级在 `target.spaceId/target.token` 下。
- 路由坑：工作台把 `feishuOpen` 注入在 NavigationStack 根视图上，`navigationDestination` 推出来的页面**拿不到**（是 nil），
  `Docs/` 的每个页面外面包了 `feishuDocsRouting()`：为 nil 时用本地 `navigationDestination(item:)` 推，并把它重新注入给子视图。
- DEBUG 启动参数（按 CLAUDE.md 用 `open…` / `show…` 命名，需加进 release.sh 的检查列表）：`-openFeishuDoc <url>`（云文档页启动后打开该文档）、
  `-openFeishuDocHeading <n>`（跳到第 n 个目录项）、`-showFeishuDocSheet outline|comments`、`-openFeishuWikiSpace <id>`、
  `-openFeishuWikiNode YES`（展开第一个有子页面的节点）、`-showFeishuWikiUpdates YES`；
  列表页签是 `@AppStorage`（不是 DEBUG 开关）：`-feishu.docs.tab recent|shared|starred|mine|discover`、`-feishu.wiki.scope all|recent|starred`。

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

#### 实现说明（妙记）

- `FeishuMinutes.swift`：列表 `minutes?scope=all|owned|shared&cursor`（`cursor` 为空即最后一页），搜索走 `minutes/search?q&cursor`
  （条目带 `snippet`）；详情 `minutes/:token` 与 `minutes/:token/transcript` 并发取。`access` 为 denied / unavailable 时给空态 +「在飞书中打开」。
- 详情：播放器、总结（关键词点一下 = 在文字记录里搜它）、章节、待办（文本末尾的「@某人」拆成负责人）、文字记录
  （按说话人分段、本地搜索高亮、播放时高亮当前段并跟随滚动，点段落 / 章节 / 待办时间跳到那里播放）。
- **播放**：hub 的 raw 代理不转发 `Range`，`minutes/:token/media` 永远 200 整个文件（97MB 的 71 分钟视频），AVPlayer 拒播。
  客户端用 `MinutesMediaLoader`（`AVAssetResourceLoaderDelegate` + 自定义 scheme）：一次 GET 边下边写临时文件，按 AVPlayer 的
  范围请求从已下载部分回数据；飞书 mp4 索引在前，几秒就能开播，往后拖要等下载到那里。服务端转发 Range 后可直接流式（leader 待办）。
- 只在点「播放」后才开始下载；离开页面停止并删临时文件。
- DEBUG：`-minutesToken <token>`（从妙记列表直接推详情）、`-minutesTab 总结|章节|待办|文字记录`、`-minutesPlay YES`（自动播放）、
  `-minutesSearch <词>`；日历 `-calendarNow <ISO 时间>`（假装现在是那一刻，看「正在进行 / 下一个」横幅和现在线）。

### 任务 / 审批 / OKR（`Work/`）
- 任务：`todos?count`（我的未完成）、`todos/completed`、`todos/search?q`、`todos/:guid/detail`、`/subtasks`、`/comments`、`task-list-nav/sidebar`。
- 审批：`approvals/counts`、`approvals?tab=todo|done|initiated|cc…&cursor`、`approvals/detail?instanceId`（表单、流程、时间线）。
- OKR：`okr/people`、`okr/owners/:ownerId/periods`、`okr/:okrId`（目标、KR、进度、得分）。

#### 实现说明（`Work/`）

文件：`FeishuWorkModels.swift`（模型 + 纯逻辑，单测 `JarvisTests/FeishuWorkTests.swift`）、`FeishuWork.swift`（共享组件）、
`FeishuTasks.swift`、`FeishuApprovals.swift`、`FeishuOKR.swift`。全部只读，不做完成 / 审批 / 评论。

- **任务列表**：`todos?count=` 和 `todos/completed?count=` 没有游标，只有 `count`（上限 200）；「分页」是滚到底时把 count 加 50 重新拉。
  未完成按 已逾期 / 今天 / 之后 / 无截止时间 分组（有截止的按时间升序，无截止的按创建时间倒序）。搜索 `todos/search?q=&token=`
  才有真分页（`hasMore` + `nextToken`），命中项多带 `roles`、`taskLists`（清单名）、`parentSummary`。
- **全天任务的截止日**存成那天的 UTC 零点（`2026-10-09T00:00:00Z`）：要按 UTC 取年月日再放回本地日历，否则 UTC 以西的时区显示成前一天。
  带时间的任务过了时刻就算逾期，全天任务过了那天才算。
- **人只有 id**：`assignees / followers / creator` 都是 `{id,isViewer}`，名字和头像用 `users/:id`（完整资料卡，较重）逐个查，
  `FeishuWorkPeopleCache` 会话内缓存、去重、并发。
- **没有「取一个任务」的接口**：`todos/:guid/detail` 只有编号、applink、来源、所在清单，没有标题 / 截止 / 负责人。详情页先用列表里见过的
  （`FeishuWorkTodoStore`），再依次从它所在清单 `task-lists/:guid`、父任务的 `subtasks`、我的未完成 / 已完成（count=200）里找；都找不到就提示并给
  「在飞书中打开」（applink）。
- `task-list-nav/sidebar` 在这个账号上只返回一个空分组，清单侧栏改用 `task-lists`（`isOwner` 分「我创建的 / 与我共享」），
  点进去 `task-lists/:guid`（分组 + 前若干条任务，`truncated` 表示没拉全）。`task-lists/counts` 必须带 `guids=`（1–60 个），否则 400。
- 评论 `todos/:guid/comments`（`position` 分页，目前只取第一页）、来源消息 `todos/:guid/references`、子任务 `todos/:guid/subtasks`。
- **审批列表**：`approvals?tab=todo|done|initiated|ccUnread|ccRead&cursor=&q=&locale=zh-CN`，`nextCursor` 分页，`q` 搜标题和表单。
  界面的「抄送我」是 `ccUnread` / `ccRead` 两个 tab 合成的二级切换。`approvals/counts` 里 `done` / `initiated` 恒为 0（列表实际有数据），
  只给 待办 / 抄送未读 显示角标。`fields` 第一项常是系统字段 `UserDepartment`，标签常带全角冒号「文章标题：」，展示前去掉。
- `readable=false` 的是外部系统的审批（Kani、BABI、People…，带 `externalUrl` / `source`）：`approvals/detail` 对它们返回 502
  `APPROVAL_DETAIL_UNREADABLE`，所以列表里直接用 Safari 打开外链，不进详情页。
- **审批详情**：`form`（`fieldList` 明细行、`contact` 人、附件、图片、金额、日期区间、关联审批）优先，没有就用 `fields`；
  流程用 `process`（步骤 × 并列节点），名字为空且没有人的旁支节点要丢掉；`phase=forecast` 的「结束」节点 `at` 是个几百年后的假时间
  （33297395130），不显示时间。`approvers` 只有名字，头像从 `process` 的同名 operator 里取。
- **OKR**：`okr/people` 的 `name` 跟账号语言走（英文），显示用 `localeNames.zh`；周期名同理（`periodName` 是英文，用 periods 里的 `localeNames.zh`）。
  `okr/:okrId` 的 `objectives` 可能为空（这个账号 2024 Q4 之后都是空的），空周期给「查看上一周期」。人员切换：我 / 上级 / 虚线上级 / 下属 / 同级；
  点对齐的目标会切到对方那个周期。`okr/progress/kr|objective/:id` 是进展记录（`progressRecordCount > 0` 才给入口）。
- **踩坑（已修）**：`feishuOpen` 曾挂在栈的根视图上，`navigationDestination` 推出来的页面拿不到（是 nil，点什么都没反应）；
  leader 在 800d822 改成挂在 NavigationStack 本身。新页面验证跳转时先确认 `feishuOpen` 不为 nil。
- **命名**：这个目录新增的类型和扩展成员一律 `FeishuWork…` / `feishuWork…` 前缀（曾和 Chat 的 `FeishuPageFooter`、`FeishuTodoRow` 撞名，master 编译失败）。
- DEBUG 启动参数（配合 `-tab workbench -openWorkbench tasks|approvals|okr`）：`-openTask <guid>`（打开任务详情）、`-openTaskScope done|lists`、
  `-openApprovalTab todo|done|initiated|cc`、`-openApproval <instanceId>`、`-openOKR <okrId>`（我的某个周期，例如有数据的 2024 Q3
  `7380825081201295388`）、`-showWorkBottom YES`（详情页停在底部，截下半屏）。都在 `#if DEBUG` 里；release.sh 的检查列表要 leader 补上。

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
- DEBUG 启动参数：`-openChatInfo <feedId>`（在收藏 / 话题页上自动打开该会话的会话信息）、`-openChatInfoPage pins|docs|files|media|todos|members`
  （再打开子页）、`-openChatInfoFrom pins|media|docs|files|todos`（会话信息从该区块开始显示，截下半页用）、`-openThreadAt <n>`（话题页打开第 n 个话题）。

### 今日（`Today/`，leader 最后做）
聚合：今天的日程（`calendar/week`）、待办（`todos`）、审批待办数、未读 @我（`conversations/unread-mentions`）、文档通知、关注话题的新回复。
