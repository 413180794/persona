# removed 的 lark-connector 飞书客户端：对 Jarvis 收件箱 / 飞书会话页（及 Agents 对话页）可迁移的模式

调研日期 2026-10-30。只读调研，未改任何仓库文件。

## 0. 约定

- 路径前缀：
  - `MF/` = removed `Packages/removedKit/Sources/Features/MessagingFeature/`
  - `OLK/` = removed `Packages/removedKit/Sources/lark-connectorKit/`
  - `BH-docs/` = removed `docs/`
  - `J/` = Jarvis 仓库根（worktree）
  - `OL/` = 上游 lark-connector 源码（本机 zoekt 索引里的 `lark-connector` 仓库，即 github.com/Eric-2369/lark-connector）
- 标记：**【事实】** = 代码 / 文档里直接看到的（带出处）；**【判断】** = 我的推断或建议；**【未查清】** = 没有证据下结论。
- 性能数字只引用文档里写明的实测值；没有实测的一律不写数字。
- 隐私：不抄任何真实账号 / 群名 / 人名 / 消息内容 / 内网地址；removed 文档里的验收截图目录、真实群名一律不引用。

## 1. removed 飞书部分的结构总览

【事实】
- `MessagingFeature` 164 个文件、约 2.9 万行（`wc -l`），核心两个大文件：`LarkMessagingModel.swift`（267 KB，含工作区模型 `LarkMessagingModel` 和单聊天模型 `LarkChatModel`，`LarkChatModel` 从第 1811 行起）、`MessagingView.swift`（143 KB，聊天页 `LarkChatView` 在 779–1817 行，气泡 `MessageBubble` 在 1818–1950 行）。
- 文件按能力分组（按文件名归纳）：
  - 阅读 / 滚动：`LarkChatScrollState`、`LarkChatViewportBridge`、`LarkReadingPositionStore`、`LarkUnreadMentionIndex`、`MessageGroupingPresentation`
  - 草稿 / 发送：`LarkDraftStore`、`LarkDraftImageStore`、`LarkSendQueue`、`LarkOutgoingJournal`、`PendingSendReviewView`、`ConversationPreviewPresentation`、`LarkScheduleQueue/Journal`（定时消息）、`LarkPostEditorModel`（多图图文）
  - 渲染：`RichMessageContentView`、`InlineMessageTextView`、`MessageVersionContentView`（编辑历史）、`MergedForwardView`、`ReceivedGroupCardView`、`VideoMessageViews`、`AudioMessageCard`、`ChatFileViews`、`MessageImageGallery`
  - 话题：`LarkThread*`（ListModel / ReaderModel / ReadingModel / ComposerModel / ControlsModel / RealtimeHub / VisibilityModel）、`ThreadViews`
  - 大量「飞书功能对齐」：标签、分组设置、置顶、标记、收藏、加急、定时、翻译、表情库 / 贴纸、群管理、群日历 / 任务 / Tab / 机器人、扫码、加密、语音转文字……（对 Jarvis 基本都不该搬，见 §4）
- `lark-connectorKit` 分层：`Messaging/Contracts`（领域模型 + 窄协议）、`Messaging/Client`（`LarkMessagingClient` + `LarkMessagingResponseCache`）、`Realtime/LarkRealtimeClient`、`Media/`（下载 + 磁盘缓存）、`ResourceUI/`（图片加载器、头像、图集、动图）、`Resources/Contracts`、`Transport/`（认证传输）。另有 `People/`、`Search/`、`Approval/`、`Workplace/`、`Auth/`。
- 设计原则（`BH-docs/ARCHITECTURE.md:69-121`）：每个业务域自有模型 / 窄接口 / 缓存 / 失败恢复，不共用全局 Store；Feature 只注入需要的小协议（`any LarkMessaging`、`LarkMessagingCaching`、`LarkThreadReading`…），能力用 `messaging is any XxxProtocol` 动态判断。

【判断】removed 的价值不在功能面（它的目标是「飞书移动端完全对齐」，和 Jarvis 方向相反），而在**聊天列表的正确性工程**：阅读位置、只报真正看过的已读、发送的写前日志与未知结果核对、窗口完整性校验、迟到结果隔离（代次 / revision）。这些可以抽象成与「飞书」无关的聊天基础设施，Jarvis 的飞书会话页和 Agents 对话页都能用。

## 2. 逐专题

### 2.1 阅读位置恢复

removed【事实】
- 存储：`MF/LarkReadingPositionStore.swift:5-74`，actor + 单个 JSON 文件，按 `account → feedID` 存 `{messageID, position, readPosition}`；话题用 `~thread~feed:root:thread` 作键（:28-31）。写入带单调 `sequence`（`DispatchTime.uptimeNanoseconds`），旧写被丢弃（:50-57）；`readPosition` 只取较大值（:53, :65-68）；「服务端已读回执晚到」只推进 readPosition、不覆盖视口锚点（`advanceReadPosition`，:58-64）。
- 记录什么：可见消息里**第一条**（视口顶）作为锚点、可见里**最后一条**作为已读上报位置（`MF/LarkMessagingModel.swift:2673-2682`）。离开 / 后台 / 弹出 sheet 时立即存（`setReading(false)` → `saveLatestReadingSnapshot`，:2157-2163, :2698-2702）。
- 打开时（`LarkChatModel.open()`，`MF/LarkMessagingModel.swift:2523-2605`）：
  1. 先恢复发送队列、定时队列、草稿（:2532-2535）；
  2. 读本机阅读位置，位置非纯数字或超过 2^53-1 视为失效并提示「已打开最新消息」（:2540-2546）；
  3. 内存里已有、且一直在线收事件的窗口直接复用，不发消息请求（:2549-2561）；否则用磁盘快照里**包含锚点的那一个原始响应窗口**先显示（:2562-2574）；
  4. 网络：有本机锚点 → `around(锚点 position)`，否则 `latest`。注释（:2579-2582）说明为什么不用服务端默认首屏：上游首屏从 readPosition 开始，新进群 / 从没打开的群 readPosition 常是 0，会打开到最早。
  5. 窗口里只有系统消息、但还有更新的页时，改读 latest（`needsLatestWindowRecovery`，:2620-2630）。
- 初始锚点（`restoreInitialAnchor`，:2614-2619）：本机锚点 → 上次锚点 → 第一条未读 → 最后一条。
- 视图层（`MF/MessagingView.swift:1142-1179`）：锚点就是最后一条且无未读、无后页 → 贴底跟随；否则 `measuredTarget`（alignment 0 = 顶对齐）+ `proxy.scrollTo(anchor, .top)`，之后由实测修正；从推入的资料页返回时不重开窗口（:1143-1150）。
- **像素级保持**（重点）：`MF/LarkChatScrollState.swift:51-163` 的 `LarkChatViewportTracker` + `MF/LarkChatViewportBridge.swift:7-133`：
  - 每行背景挂一个不可交互的 `UIView` 标记，按需把行 frame 换算到所属 UIScrollView 坐标（`rowFrames`，Bridge :57-65）；行坐标和 contentOffset **在同一轮里读**（`MessagingView.swift:1499-1503` 注释）。
  - 捕获最多 3 个锚点 `(id, offsetWithinRow, height)`，优先完全可见的行（Tracker :99-111：被裁剪的行自己变高不应推走下面完整可见的行）。
  - 行变高 / 视口尺寸变 / 前插历史时算出目标 offset，`Task.yield()` 收齐本轮测量后再 `setContentOffset`；差 ≤0.75pt 忽略；**用户在拖动 / 减速时绝不修正**（Bridge :116-125）。
  - 「LazyVStack 估算总高度会自己变，这不代表锚点真的动了」（Tracker :94-96），只在视口变尺寸或内容边界可用时才修正。
  - 前插历史时偏好「高度没变的锚点」（分组头 / 日期头会因前插改变样式），校正完成后重新捕获（Tracker :75-76, :88-93, :155-162）。
  - 锚点行全部离开懒加载区时，最多发起一次 `scrollTo(id)` 让它重新实例化（`takeMissingAnchorForRealization`，Tracker :147-151；View :1033-1043）。
  - 用户手势、主动跳转、后台、换会话都让旧修正失效（`resetViewport`，View :1468-1475, :999-1012, :1197-1204）。
  - 不替换 scroll view 的 delegate，也不同时用两个滚动位置所有者（Bridge :5-6；`BH-docs/LARK_CHAT_UI_REVIEW.md:19`）。
- 文档里的实测 / 验收（`BH-docs/LARK_CHAT_UI_REVIEW.md:17-31`）：复现了「上方正文变高后跳进旧消息」「补历史页跳到新插入首行」两个 bug；修复后「80 行增高及三张同时延迟图片完成后保持同一文字位置」；但快速拖动仍有中间空白帧，真机 / 帧率未验。`BH-docs/FEISHU_PARITY.md:62`：「保留消息级锚点，不承诺长消息内部像素偏移恢复」。

Jarvis 现状【事实】
- 飞书会话页 `J/ios/Jarvis/Views/Inbox/Lark/LarkChatView.swift`：**非倒置** `ScrollView + LazyVStack + ScrollPosition`（:54-89），`defaultScrollAnchor` 按「是否打开到某条」选 top / bottom（:89）。
- 不保存阅读位置：每次按 `focus`（收件箱这件事的第一条，`J/ios/Jarvis/App/InboxStore.swift:114-123`）或 `readPosition+1` 打开（:254-276）。
- 跳到某条用**固定等待**：`sleep 80ms → scrollTo → sleep 250ms → scrollTo`（:293-298）。removed 明确把这种做法换成了实测完成（`BH-docs/LARK_CHAT_UI_REVIEW.md:19`「替代固定等待时间」）。
- Agents 对话页（`J/docs/ios.md:66-67, 89-104`）已经是倒置列表 + 按 id 跳再按实测修正 + `chat.read.<会话>` 存屏幕最上面那一行，比飞书页成熟。

【判断】
- 飞书会话页应改成和 Agents 对话页同一套列表引擎（倒置列表、最新端偏移 0），而不是继续维护第二套非倒置实现；两边共用「锚点 = (消息 id, 行内偏移)」的阅读位置存储，键为 `hub / 账号 / 会话（/ 话题）`。
- removed 的「UIKit 标记视图实测行坐标 + 只在非手势时修正」是在**非倒置 LazyVStack** 上解决前插 / 变高漂移的方案；Jarvis 已用倒置列表规避了大部分问题（`J/CLAUDE.md` 倒置列表若干条经验）。可借鉴的是它的**纪律**：修正只在 idle、同一轮读坐标、≤0.75pt 忽略、手势 / 跳转 / 后台 / 换会话使旧修正失效、锚点被懒加载回收时最多重新实例化一次。
- 对收件箱场景，「上次读到哪」不如「这件事从哪开始」重要：入口仍按 `start_position` 打开，但**同一会话在件之外再次打开**（从链接 / 资料卡「发消息」进入）时用本机锚点恢复。

### 2.2 未读分界线

removed【事实】
- 分界位置 = `max(服务端 readPosition, 本次已上报的 lastReadPosition, 本机保存的 readPosition)`，有未读但没有有效阅读位置时从 0 开始（`MF/LarkMessagingModel.swift:2607-2613`；`BH-docs/FEISHU_PARITY.md:701`）。
- `firstUnreadID` = 分界之后第一条「不是自己发的、不是系统消息」（:2489-2492）；分界行是 LazyVStack 里带 `.id("unread-boundary")` 的一行「以下为未读消息」（`MF/MessagingView.swift:889-895`）。
- 分界会**打断发送者连续分组**（`MF/MessageGroupingPresentation.swift:6-7` 的 `unreadBoundary` 参数；调用处 `MessagingView.swift:882-883`）。
- 每批行只算一次分界和 Pin 集合，避免逐行搜索（`BH-docs/LARK_CHAT_UI_REVIEW.md:39`）。
- 局限（`BH-docs/ARCHITECTURE.md:374`）：上游没有精确的服务端已读位置，**不能据 unreadCount 推断跨端未读分界**。

Jarvis 现状【事实】
- 分界 = 打开时冻结的 `anchor = readPosition`（`LarkChatView.swift:38-39, 263-265, 207-211`），文案「以下为新消息」。
- **从收件箱打开（focus 路径）时不设 anchor → 没有分界线**（:257-260 只在 `else if opensAtMessage` 分支设 anchor）；收件箱打开的会话都带 focus（`InboxStore.swift:121`），所以实际最常见的入口没有分界线。
- 分组判断按 `senderName` 而不是 sender id（`LarkChatView.swift:221-225`），且分界不打断分组。

【判断】收件箱入口应在「这件事的第一条」上方显示分界（文案可以是「这件事从这里开始」/「以下为新消息」），分界打断分组；分组改按 sender id。

### 2.3 跳转按钮（最新 / @我 / Pin）

removed【事实】
- 「回到最新」与跟随状态分离：`followsLatest` 是用户的决定，只在**用户滚动中**根据是否到底改变；图片加载等布局变化不能悄悄关掉跟随（`MF/LarkChatScrollState.swift:3-32`）。到底判断扣除底部 inset（:10-18）。
- 按钮上的数字只数「视口下方、已加载、已同步、别人发的、未看过、在已读位置之后」的消息；注释：**协议 position 可能稀疏，差值永远不是条数**（`MF/LarkMessagingModel.swift:1857-1869`）。尾部没加载完显示 `N+`，未知 / 0 只显示到底图标（`LarkChatScrollState.swift:34-37`；`BH-docs/FEISHU_PARITY.md:64`）。
- 回到最新：窗口不含尾部时**整窗替换**为 latest，而不是假装中间已加载（`MF/LarkMessagingModel.swift:2763-2776` 注释 :2771）；滚动目标是列表末尾的实体 footer（`id("bottom")`），不是最后一条消息（可能被出站去重藏掉或布局后变高）（`MF/MessagingView.swift:1083-1084, 1452-1456`）。
- 跳到某条：窗口里有就直接滚；没有就用 position 提示 / 搜索得到的位置 / 读单条消息拿 position，再 `around(position)` 整窗替换（:2778-2806）；居中（alignment 0.5）并高亮 3 秒（`MessagingView.swift:1113-1117, 1185-1189`）。
- **统一导航按钮**（`BH-docs/FEISHU_PARITY.md:683-701`；代码 `MF/LarkMessagingModel.swift:2493-2521, 2446-2464`；`MF/LarkUnreadMentionIndex.swift`）：右下角只有一个按钮，未看过的 Pin 与 @我 按 position 排序、同一消息去重，全部看过后才变成「回到最新」；目标真正出现在屏幕上才算看过（点按本身不消耗）。上游的 `unreadFeedMentions` 每类只给最新一条，所以另建「未读 @ 索引」：从未读分界向后逐页读（每次按钮最多 5 页，显示「继续查找未读提醒」），**不往主列表插行、不滚动、不上报已读**；只有服务端 `syncState == complete` 的页才推进覆盖范围（`LarkUnreadMentionIndex.swift:56-85`）。
- Pin 横幅（`MessagingView.swift:1406-1423`）与 Pin 列表——飞书功能，见 §4。

Jarvis 现状【事实】
- 「回到最新」数字 = `conversation.unread − 已看过的未读条数`（`LarkChatView.swift:166-170`），即用服务端 unread 计数推算；`hasNewer || !atBottom` 时显示（:172-191）。
- 「有人@你」只有一个目标：`conversation.mentionPosition`（:266）；focus 路径不设（:257-260）。跳转用固定 sleep（:346-354）。

【判断】
- 数字改成 removed 的口径（只数已加载且确实在视口下方的；尾部没加载显示 N+），否则会出现「按钮写 12 条、往下只有 3 条」的错觉。
- @ 跳转保留「一个按钮」的思路：收件箱已经知道这件事的 `mention_position`（`J/docs/protocol.md:388`），先覆盖「这件事范围内的 @」；跨页 @ 索引属于锦上添花，可以放在 hub（hub 读消息页更便宜），App 只要一个有序目标列表。
- Pin 不搬。

### 2.4 只上报真正看过的已读

removed【事实】（`MF/LarkMessagingModel.swift:2659-2748`，视图 `MF/MessagingView.swift:992-995, 1434-1436`）
- 可见集合来自 `onScrollTargetVisibilityChange(threshold: 0.01)` 一次性替换（不是每行 onAppear 各写一次 observable）。
- 是否在「阅读」由视图算 `canRecordReading`：页面可见 + 已完成初次定位 + 前台 + 不在跳转 / 定位中 + 没有 sheet / 资料页 / 图片预览 / 设置页遮挡（`MessagingView.swift:1434-1436`）；话题页另有「未被媒体 / 记录页遮挡、完成布局定位后」（`BH-docs/ARCHITECTURE.md:372`）。
- 450 ms 合并（:2708-2716）；**本机阅读位置任何时候都存**，**服务端已读回执**只在窗口是新鲜核对过的（不是缓存快照、不在补洞同步中）时才发（:2718-2727 注释 :2719-2720）；发之前再核一次当前账号（:2737）；慢回执期间的新请求合并为一次补发（:2729-2734）；只发「比已上报更靠后」的位置（:2736）；回执成功后推进本机 readPosition 高水位（:2741-2742）。
- 上报内容 `POST read-position {maxPosition, messageIds}`（`OLK/Messaging/Client/LarkMessagingClient.swift:218-221`），messageIds 只含别人发的、可用的可见消息（`LarkMessagingModel.swift:2678`）。

Jarvis 现状【事实】
- 每行 `.onAppear { markSeen }`，400 ms 后 flush（`LarkChatView.swift:78, 378-388`）；`onDisappear` 再 flush（:140）；上报最高位置 + 该条 badge_count + 其间别人的未读 id（:390-400；协议 `J/docs/protocol.md:408`）。
- 话题群不上报（:381, :391）。
- **focus 路径把 `readCursor` 设成窗口最后一条**（:257-259），flush 只报「比 readCursor 新」的位置（:392-394）→ 从收件箱打开时，初始窗口里的消息不会被上报为已读，只有之后加载的更新页会。和 `J/docs/ios.md:14`「只把上过屏的消息上报为已读」的描述不一致。是否有意（比如故意不让「打开件」改飞书已读）**【未查清】**；`J/docs/inbox.md:64` 只说「已处理不回写飞书已读」，没说打开不回写。
- 没有前台 / 遮挡 / 定位中 / 缓存窗口的门禁（缓存先画的行也会触发 markSeen）。

【判断】
- 先把产品语义定下来：收件箱里「打开件 ≠ 已读」还是「看过就已读」。如果要上报，按 removed 的门禁做（前台、初次定位完成后、无遮挡、非程序跳转中、窗口已核对），可见集合用滚动可见性回调一次性替换。
- 这套门禁同样适用于 Agents 的「完成未看 → 已看」（`J/docs/agent-states.md`）：只有用户真的看到回复才算已看（Agents 现状是否这样做【未查清】，不在本次范围）。

### 2.5 草稿

removed【事实】
- `MF/LarkDraftStore.swift:5-90`：actor + 单文件，键 = 账号作用域（`accountNamespace + ":" + 当前用户 ID`，`MF/LarkMessagingModel.swift:1327` 的 `readingAccount`；发送日志、话题等用同样的组合，:209-224）→ `feedID` 或话题键 `~thread~feed:root:thread`（:86-90）；草稿有 `version: UUID`（每次编辑换新，`LarkMessagingModel.swift:2808-2814`）、单调写序号（:57-58）、已退休版本集合（发送后旧版本不能被晚到的保存复活）。
- 250 ms 防抖保存（`LarkMessagingModel.swift:2815-2831`）；后台、离开聊天、发送前立即提交原生输入框内容（`MessagingView.swift:1203-1204`；`ARCHITECTURE.md:67`）。
- 恢复只在编辑器为空且本次没编辑过时进行（:2842-2861）；引用的原消息异步核对，失效则提示「引用的原消息已不可用，草稿文字已恢复」（:2862-2880）。
- 解码失败把原文件移成 `.unreadable-<uuid>` 备份再从空开始，并给出提示（`LarkDraftStore.swift:77-82`）；版本号白名单 1–8，旧 App 遇到更高版本拒绝覆盖（:69；`ARCHITECTURE.md:67`）。
- 列表行显示 `[草稿]` 前缀；已经提交出去的那一版草稿不再显示为草稿（`MF/ConversationPreviewPresentation.swift:62-64`）。

Jarvis 现状【事实】飞书会话页输入框是 `@State text`，不持久化（`LarkChatView.swift:34, 143-161`）；Agents 输入框有草稿持久化（`J/CLAUDE.md`「UI 测试里草稿会跨次运行保留」），细节不在本次范围。

【判断】飞书会话页 / 收件箱「AI 草拟回复」浮层的编辑内容都应持久化，键 `hub / 账号 / feedID（/ 话题根）`，带版本 UUID + 单调序号；收件箱行在有草稿时显示「草稿」标记（这是收件箱自己的状态，不是飞书功能）。

### 2.6 待发送消息：写前日志、原编号、未知结果

removed【事实】
- 状态（`MF/LarkSendQueue.swift:8`）：`submitting / queued（服务已受理，等确认）/ confirmed / failed / unknown`。持久化阶段 `prepared / queued / failed / unknown`（`MF/LarkOutgoingJournal.swift:25`）。
- 写前顺序（`LarkSendQueue.swift:149-162`）：先写**原草稿版本**，再写**操作记录**（`journal.prepare`，含 clientId、预期发送者、可见文字、样式、表情 / 提及身份、引用、类型；**不存附件字节**），两者都落盘后才 `onPrepared()` 清空编辑器，然后才发网络请求。
- 所有发送都带客户端生成的 `clientID = item.id`（UUID）（:196-277）。拿到 messageId 先进内存（落盘失败也不丢），再写回执，再变 `queued`（:280-291）。
- 失败分类（:293-306）：明确发生在提交前的错误（本地校验、会话过期、4xx 且非 408）= `failed`，可重试；其余（超时、5xx、取消、坏回执）= `unknown`，**禁止自动重发**。
- 确认只认精确身份：`clientID` 或 `remoteID` 匹配本人消息，**从不按文字相等判断**（:418-425）；`checkStatuses()` 打开聊天和每次刷新时按 clientId 查上游 outbox 状态（:498-519；`MessagingView.swift:1178`；`LarkMessagingModel.swift:3162`）。
- 重试只用原不可变快照和原 clientId，不从当前编辑器重建（`ARCHITECTURE.md:321`）；已受理的文字走上游 `/outbox/:clientId/retry`（`LarkSendQueue.swift:236-240`；`OLK/.../LarkMessagingClient.swift:222-229`）。
- 显示：消息列表里出站卡片在消息之后单独一段，文案「提交中 / 服务已接收，等待发送确认（核对记录）/ 结果待确认 · 核对记录 / 失败（重试）/ 已在其他客户端核对…」（`MessagingView.swift:938-970`）；列表去掉已被确认的同一条（按 clientId / remoteID，`LarkMessagingModel.swift:1849-1856`）。
- 人工处置：确认已发送 / 确认未发送（文字放回输入框；输入框已有新草稿时禁用）（`MessagingView.swift:1323-1335`）；首页可在原会话不可访问时打开「核对发送记录」（`MF/PendingSendReviewView.swift`）。
- 会话列表摘要前缀 `[发送中] / [待送达] / [发送失败] / [结果待核对]`（`MF/ConversationPreviewPresentation.swift:46-81`）。
- 日志版本化 v12，读 v1–v11；损坏 / 过新 / 写失败时保留原文件并**阻止新的发送**（`LarkOutgoingJournal.swift:61-71`；`ARCHITECTURE.md:67, 323`）。

上游 lark-connector 的相关能力【事实】
- 发送接口接受 `clientId`，不传则服务端随机生成（`OL/src/server/routes/api.ts:3747-3757`）；`/agent/v1` 前缀走同步 `messenger.send`（:3775-3782）。
- `send` 按 clientId 幂等：已有 intent 复用；`createOutbox` 是 `onConflictDoNothing`；若该 clientId 已经 `sent`，直接返回原 messageId 不再发（`OL/src/server/services/messenger-service.ts:7261-7318`；`OL/src/server/repositories/messenger-repository.ts:6680-6705`）。
- `GET /api/outbox/:clientId`（查状态）**只在 owner UI 的 `/api` 上，`/agent/v1` 没有**（`OL/src/server/routes/api.ts:1396-1415` 注释）；`POST .../outbox/:clientId/retry` 注册在通用域 API 里，`/agent/v1` 也有（:3810-3838）。
- 消息视图带 `clientId`（可选）和 `localState: synced | pending | failed`（`OL/src/shared/contracts/index.ts:776, 807`）。

Jarvis 现状【事实】
- 飞书会话页发送：`await api.larkSend`，成功才清空输入框，失败弹错（`LarkChatView.swift:402-416`）；请求体只有 `text`（`J/ios/Jarvis/Views/Inbox/Lark/LarkModels.swift:258-259`）；无出站气泡、无持久化。
- hub **每次调用都新生成一个随机 clientId**（`J/server/internal/lark/lark.go:497-504`），所以手机超时后重试 = 新 clientId = 可能发两遍。
- hub 投影的消息不带 `clientId` / `localState`（`lark.go:122-143`）。
- Agents 对话页已有出站队列（`J/ios/Jarvis/App/Outbox.swift`：等待中的消息存盘、按序发、同一条重发带同一个 `client_msg_id`；`J/docs/ios.md:288-291`）。

【判断】
- 飞书发送直接复用 Agents 的 Outbox 模型：手机生成 clientId（UUID）并持久化 → hub 把它原样透传给 lark-connector（改 `lark.Send` 签名）→ 上游按 clientId 幂等，「等网络恢复后自动重发」就安全了（已 sent 的直接返回原 id）。
- hub 把 `clientId`、`localState` 透传进 `LarkMessage`，App 用它精确消掉出站气泡（不按文字比）。上游 agent 面没有 outbox 状态查询，必要时 hub 可以在发送时自己记 clientId → messageId，或用「最新页里有没有这个 clientId」核对。
- removed 的「unknown 绝不自动重发、只读核对」是因为它直连、没有可依赖的幂等保证；Jarvis 如果打通了 clientId 幂等，可以比它简单：unknown 也能用同一个 clientId 自动补发。附件（图片 / 文件）不在 Jarvis 飞书页范围内。
- 收件箱 `POST /api/v1/inbox/{id}/reply` 同理需要幂等键（hub 内部是否也走 `lark.Send` 待确认，见 §6）。

### 2.7 媒体

removed【事实】
- 图片尺寸预留：上游消息 part 的 `metadata.width/height` 贯通到所有图片预览（`BH-docs/LARK_CHAT_UI_REVIEW.md:33-39`：旧包「收到 800×500 提示，等待时仍只占小条，加载后把后续消息推下」；修复后「后续文字 5016 像素区域零变化」——这是像素区域比对，不是性能数字）。实现 `OLK/ResourceUI/LarkResourceViews.swift:22-77`：`LarkReservedImageLayout` 按尺寸 fit 进容器，**proposal 为 infinity 时回退到 min(240, w) / min(280, h)**，避免容器测量时变 0 高；聊天里 `frame(maxWidth: 240, maxHeight: 280)`（`MF/MessagingView.swift:1875`）。
- 加载器 `OLK/ResourceUI/LarkImageLoader.swift:7-68`：视图 task 拥有，消失 / 进后台取消（`LarkResourceViews.swift:41-44` 的 task id 带 scenePhase）；**下载成功也要能完整解码才算图片，否则只驱逐这一项缓存**（:28-35, :62-67）；400/401/403/404/410 和认证错误不重试；其他按 2/5/15/60 s 退避，非头像最多 3 次（:44-55）。
- 解码：`LarkNativeImage` 直接 `UIImage(data:)`，**没有降采样**（`LarkResourceViews.swift:93-104`）。
- 图片磁盘缓存只收同源 `/api/media/<key>`、可完整解码、服务端允许缓存的静态图；单项 ≤10 MiB、每服务 ≤64 MiB，有效期取 max-age 与 24 h 的较小值（`BH-docs/FEISHU_PARITY.md:1007`）。
- 视频：认证下载到私有文件后播放，**不支持 Range 流式**；AVURLAsset 禁止外部引用（`BH-docs/ARCHITECTURE.md:356`）。语音：下载后用自封装的 Ogg Opus / CAF 播放器，所有播放器共享音频焦点，离开 / 后台暂停（`ARCHITECTURE.md:65, 360`）。文件：100 MiB 上限、同源下载、系统分享 / 预览（`FEISHU_PARITY.md:81`）。
- 大图集：图片用 `fullScreenCover`，其他附件用 sheet，同一个呈现所有者（`MF/ImagePresentation.swift:3-36`）。

Jarvis 现状【事实】
- 已有尺寸预留：`metadata.width/height` → 240×300 内等比，无尺寸时 200×150（`J/ios/Jarvis/Views/Inbox/Lark/LarkMessageContent.swift:366-399`）；视频用 posterWidth/Height（:479-492）；贴纸先 80×80 占位、加载后按实际比例（:401-415，会引起一次重排）。
- `LarkMedia`（`J/ios/Jarvis/Views/Inbox/Lark/LarkMedia.swift`）：Caches/lark-media 磁盘缓存（sha256(url#size)），ImageIO 降采样（:100-106），NSCache 120 MB（:29），hub 缩略图 24px 步进 ≤624（:183-187）；**落盘前不校验能否解码**（:59-63）；`LarkRemoteImage` 失败一次就显示坏图标，重新出现才重试（:156-181）。视频 / 语音用带 Bearer 头的 AVURLAsset 直接流（:277-306，hub 转发 Range，`J/CLAUDE.md`），文件用 Quick Look（:309-332）。

【判断】
- Jarvis 在解码（降采样）、视频流式播放上比 removed 好，保留。
- 值得搬：①落盘前完整解码校验、坏字节只驱逐这一项；②按错误类型决定是否重试 + 有限退避；③贴纸也用尺寸元数据预留（若上游有）；④自定义 Layout 处理 infinity proposal（Jarvis 已在 `J/CLAUDE.md` 记过这个坑）。

### 2.8 富文本 / 卡片渲染

removed【事实】
- 富文本：`LarkRichPartMetadata` 保留组合 marks、旧 style、行内代码、祖先块链、段落边界、空行、显式 Markdown、代码元数据；树按每个叶子的完整祖先链重建；未知块保留子节点、未知 metadata 不显示成 JSON（`BH-docs/ARCHITECTURE.md:432-440`）。
- 预算：parts ≤4096、节点 ≤8192、深度 ≤24、正文 ≤1 MB、代码 ≤256 KiB / 2048 行 / 8192 token（`OLK/Messaging/Contracts/RichText.swift:56-62`）；超限按原顺序**分页阅读**（6000 字符一页，`MF/RichMessageContentView.swift:444-453`），不截断后续文字和附件。
- 表格 / 分栏：固定投影的 table-row/cell、column 映射成 Grid，坏跨度整个容器回退（`ARCHITECTURE.md:634-636`）。
- 长文本折叠：>2000 字符或 >20 行先显示前 2000 字 / 16 行，「展开长消息」（`MF/MessagingView.swift:1952-1968`）。
- 普通文字里的 Markdown 符号按字面显示，只有明确标了 markdown 的 part 才走 Markdown（`ARCHITECTURE.md:438`）。
- **互动卡片基本没做**：机器人菜单、互动 / 应用 / 投票卡片「待实现；未知内容仅作可读回退」（`BH-docs/FEISHU_PARITY.md:103`），气泡里未知 part 显示成「[kind 消息]」标签（`MessagingView.swift:1898`）。
- 损坏的可选摘要字段（`textPreview`、引用 / 编辑历史摘要）独立容错，不能连累有效正文（`BH-docs/FEISHU_PARITY.md:1086-1101`）。

Jarvis 现状【事实】已照搬 lark-connector 网页渲染器：富文本块树、代码块轻量高亮、系统消息、完整卡片元素（column_set 窄屏重排、table、collapsible_panel、chart、按钮回调……）、合并转发 / 名片 / 日程等摘要卡（`J/docs/ios.md:307-326`）。

【判断】卡片渲染 Jarvis 明显领先，不需要向 removed 学。可搬的是：①解析预算 + 超限分页回退（防止超大消息卡死主线程，和 Jarvis「行 body 反复求值要缓存」的经验一致）；②可选展示字段独立容错（坏的 preview 不应让整条消息变占位）；③长文本折叠。

### 2.9 表情回应

removed【事实】
- 回应条：表情图和「谁回应了」是**两个独立触点**——点表情切换自己的回应，点人名 / 数字看回应人员（`MF/MessagingView.swift:1935-1949`）；最多显示 2 个名字 + `+N`；遵守消息的 `allows("reaction")` 权限。
- 回应人员分批读取资料（501 人分批）、上游名单不足时明确提示（`BH-docs/FEISHU_PARITY.md:70`）。
- 实时：`reaction.updated` 映射为 `messageUpdated`（`OLK/Realtime/LarkRealtimeClient.swift:65-68`）。
- 切换回应是否乐观更新：走 `perform(.reaction(...))`（`MessagingView.swift:901`），实现细节见 §7 补充。

Jarvis 现状【事实】乐观切换 + 失败回滚（`LarkChatView.swift:359-373`）；回应 chip 整体是一个按钮（点了切换），显示 ≤3 个名字否则数字（:700-723）；长按菜单前 4 个常用表情 + 「表情回复」（:540-560）。

【判断】Jarvis 的乐观更新体验更好，保留。「看谁回应了」属于飞书浏览功能，不搬；但 chip 上「名字」和「切换」分开触点这一点可以不做（Jarvis 不提供人员列表，就没必要分）。

### 2.10 话题

removed【事实】（`BH-docs/ARCHITECTURE.md:364-393`）
- 话题有独立的列表 / 阅读模型、独立游标，不借用主聊天窗口、草稿或已读游标；消息保留协议话题 ID、父消息、话题位置；列表缺 root 时显示不可操作占位。
- 话题阅读位置单独存（根消息位置用本机 0 哨兵）；恢复时最多自动再读 5 页，超过要用户明确继续或跳最新；恢复过程中不提交已读。
- 话题已读：450 ms 合并，只调话题自己的 read-position 接口；只有确认读到列表已知最后一条时才清零该话题未读。
- 实时：`LarkThreadRealtimeHub` 复用工作区的同一 WebSocket，每个页面一个 `bufferingNewest(64)` 流；**订阅时先发一次 `.connected` 让页面自己重读一次**；缓冲溢出也转成 `.connected`（`MF/LarkThreadRealtimeHub.swift:12-33`）。
- 断线重连：分批复核已加载的原消息（每批最多 32、并发最多 4），修复撤回 / 删除 / 回应；删除墓碑防止旧请求复活已删消息；新尾页与旧窗口有缺口时保留旧窗口，用户点「查看新回复」才替换（:391-393）。

Jarvis 现状【事实】话题群显示话题列表；「N 条回复」打开 sheet，**一次读回复，只读最新一页（limit 60），没有往前翻**（`LarkChatView.swift:675-679`；`LarkModels.swift:255-257`）；话题不上报已读（:381）。

【判断】话题回复 sheet 至少要能往前翻（否则长话题只看到后 60 条）；话题已读 / 话题阅读位置是飞书细节，Jarvis 的收件箱以「件」为单位，暂不需要。

### 2.11 实时同步与断线补洞

removed【事实】
- 实时客户端（`OLK/Realtime/LarkRealtimeClient.swift:5-74`）：单个 WebSocket，`bufferingNewest(32)`；**事件被丢弃时补发一个 `.connected`**（= 「你可能漏了东西，全量核对」）；断开发 `.disconnected`，1 s 起指数退避到 30 s；事件类型映射到领域变化（会话 / 消息 / 单条更新 / 删除 / Pin / 话题 / 设置 / 认证）。
- WebSocket 启动要先建立接收循环再等初始 pong，否则先到的欢迎帧会阻塞 pong（`BH-docs/ARCHITECTURE.md:395`）。
- 工作区分发（`MF/LarkMessagingModel.swift:1606-1706`）：`.connected` → 所有已打开聊天标记「需要核对」并切回连续更新；`.disconnected` / 后台 → 标记过期、暂停缓存写入（:1401-1428, :1730-1743）；`.messages(feed)` / `.messageUpdated` / `.messageRemoved` → 使该会话快照失效、合并刷新；会话列表刷新 350 ms 合并、处理中来的新请求再跑一轮（:1708-1728）；单聊天刷新 300 ms 合并（:3288-3304）。
- 刷新时**不插缺口**（`refresh()`，:3139-3173）：尾部修复用 `after(newerCursor 或最后一条 position)`，只有确认已到尾部（newerCursor 为空）才再读 latest，且 `mergeTailRepair` 只接受已知 id 或不早于当前窗口首条的行（:3801-3806）。
- 补洞（`revalidateMessageWindow`，:3177-3279）：上游**没有可回放的事件序号 / 游标**，position 也不是编辑版本（`BH-docs/FEISHU_PARITY.md:1005`），所以重连后以当前锚点为中心 `around` 读，向两侧翻页直到覆盖原来显示的区间（预算 8–200 页，游标必须单调、不能循环）；**只有每页 `syncState == complete` 才用核对结果替换原区间（缺的行视为已删）**；不完整就保留旧窗口 + 「消息记录尚未同步完整」+ 禁止已读回执；403 / 404 撤下该会话缓存，超时 / 503 不撤（:3266-3277）。后台刷新时不把 around 读到的更早上下文插进来（会移动视口，:3236-3240）。
- 实测（`BH-docs/FEISHU_PARITY.md:1009`）：隔离环境里「返回重进聊天的消息请求数 1→1」（在线复用不重复读），断线期间被静默编辑 / 删除的消息在重连后正确更新 / 消失。

Jarvis 现状【事实】
- hub 跟随 lark-connector `/agent/v1/events`，每 400 ms 最多推一次 `lark{feed_ids}`（`J/docs/protocol.md:160`）；App 端 `LarkSync`（`J/ios/Jarvis/App/LarkSync.swift`）：每个监听者 `bufferingNewest(1)`（:34-40）；收到就让对应会话页 `refreshLatest`，并在后台给收件箱里的会话取最新一页进缓存（并发 2、同一会话 3 s 内最多一次，:11-15, :83-103）。
- **重连时只通知 feed == nil 的监听者（收件箱），已打开的会话页不收到**（:60-61），后台补拉又跳过打开着的会话（:73-81）→ 打开着的会话页要等 60 s 轮询兜底（`LarkChatView.swift:132-135`）。
- **`refreshLatest` 取最新一页（limit 40）并把「比当前最后一条新」的直接拼到末尾**（`LarkChatView.swift:300-312`）：两次刷新之间来了超过一页的新消息时，中间会**静默缺一段**，没有缺口检测。
- hub 的页投影丢掉了上游的 `syncState`，删除的消息直接过滤掉（`J/server/internal/lark/lark.go:282-305`）→ App 无从判断窗口是否完整。

【判断】
- 修三处：①重连后通知打开着的会话页做一次核对；②刷新改为 `after(最后一条)` 逐页追平（超过 N 页就整窗换成 latest 并给「有更多新消息」提示），绝不拼接不连续的页；③hub 透传 `syncState`（以及被删除行的墓碑），App 只在完整时删除本地行。
- 事件流做成「变化通知 + 溢出即全量核对」两种语义，和 removed 一样；Jarvis 已有的 `revs` 增量协议（Agents 用）证明了推拉结合的路子是对的，飞书侧缺的只是缺口检测。

### 2.12 缓存与离线

removed【事实】
- 消息层显式快照接口（`OLK/Messaging/Contracts/LarkMessagingCaching.swift:3-36`）：会话列表按 scope 存「一次完整游标遍历」的原始页窗口（原子提交，失败不覆盖旧窗口，`BH-docs/FEISHU_PARITY.md:1020`）；消息只恢复**包含阅读锚点的单个完整原始响应窗口**，不拼接不连续页；没有锚点时只恢复 latest 窗口；空的增量页不能覆盖旧视窗（`OLK/Messaging/Client/LarkMessagingClient.swift:117-135`）。
- `LarkMessagingResponseCache`：按 owner 隔离，默认 64 MiB（:95），>512 项按近期使用淘汰（:298-305），原子写 + `completeUntilFirstUserAuthentication` 文件保护 + 排除备份（:130, :269-272）；generation token 拒绝断线前的在途写（:241-260）。
- 缓存显示不确认发送成功、不当成服务端已读（`BH-docs/FEISHU_PARITY.md:1003`）；退出账号清除。
- 离线冷启动：绑定原凭据 / 证书 / 连接代次的「最近验证过的 owner」，冷启动在网络核对完成前就能只读打开缓存会话；只读模式没有发送 / 回执 / 远程下载（`BH-docs/FEISHU_PARITY.md:1104-1112`；`BH-docs/LARK_CHAT_UI_REVIEW.md:51-53`）。实测：打开缓存正文前后 API 请求数不变、0 变更（文档原话，未给样本分布）。

Jarvis 现状【事实】`StateCache`（Caches/state）存每个飞书会话最近 80 条（`J/docs/ios.md:94-95`）；冷启动 / 断网 / 切 tab 先画缓存（同上）；但**飞书会话页只有在 `focus == nil` 时才用缓存先画**（`LarkChatView.swift:121-130`），而收件箱入口总带 focus → 收件箱打开的会话不用缓存先画，`LarkSync` 预取的缓存对这个入口基本用不上（`J/docs/ios.md:33-34` 写的「未读会话的缓存覆盖到第一条未读时也直接用缓存打开」只覆盖非 focus 路径）。

【判断】
- 缓存命中规则改成「缓存里包含要打开的那条（start_position）就先画」，和 removed「包含锚点的窗口」一致。
- 缓存只存连续窗口（一页或连续多页），不存拼接结果；加 owner / hub 作用域（重新配对到另一个 hub 是否清缓存【未查清】）。

### 2.13 消息首页（列表）模式

removed【事实】（`BH-docs/LARK_INBOX_UI_REVIEW.md`、`BH-docs/FEISHU_PARITY.md:32-55, 1014-1075`）
- 未读总数来自服务端汇总，不对已加载分页求和（`MF/LarkMessagingModel.swift:1442` 注释）。
- 置顶前缀会占满第一页：只显示「已确定活动时间边界之前」的完整前缀，边界同时间的行一起等，避免旧会话先于还没取回的新会话出现（`FEISHU_PARITY.md:1016-1022`）。
- 列表行摘要：草稿 / 发送状态前缀（§2.5、§2.6）。
- 离线：本机只读首页，明确显示「上次账号、本机只读、可能过时」（`FEISHU_PARITY.md:1108`）。
- 角标三位数、红点 / 灰数字、标记红旗、置顶网格、筛选吸顶、下拉展开置顶……全是飞书首页对齐。

【判断】Jarvis 的收件箱是 AI 分拣后的「件」列表，和飞书 feed 不是一回事。可搬的只有：①行上显示本机状态（草稿 / 发送中 / 发送失败）；②离线时标题位提示「本机数据」（Jarvis 已有连接状态标题，`J/docs/ios.md:273-297`）；③汇总数字只用服务端口径。

## 3. Jarvis 现状中发现的疑似问题（证据汇总）

| # | 现象 | 证据 | 性质 |
| --- | --- | --- | --- |
| 1 | 收件箱打开会话（focus 路径）：无分界线、无 @ 按钮、不先画缓存、初始窗口不上报已读 | `LarkChatView.swift:121, 257-260`；`InboxStore.swift:121` | 事实；是否有意【未查清】 |
| 2 | 刷新拼接最新一页，超过一页的新消息会静默缺段 | `LarkChatView.swift:300-312`；`LarkModels.swift:252`（limit 40） | 事实（代码路径）；未实测复现 |
| 3 | 重连不通知已打开的会话页，最多 60 s 才补 | `LarkSync.swift:60-61, 73-81`；`LarkChatView.swift:132-135`；`AppModel.swift:455-459` | 事实 |
| 4 | 飞书发送不幂等：hub 每次随机 clientId；手机无出站队列 | `lark.go:497-504`；`LarkChatView.swift:402-416` | 事实 |
| 5 | 定位用固定 sleep | `LarkChatView.swift:293-298` | 事实 |
| 6 | 分组按名字而非 sender id | `LarkChatView.swift:221-225` | 事实 |
| 7 | 话题回复只读最新 60 条 | `LarkChatView.swift:675-679`；`LarkModels.swift:255-257` | 事实 |
| 8 | 媒体字节落盘前不校验能否解码 | `LarkMedia.swift:59-63` | 事实；实际影响取决于 hub 对错误的处理【未查清】 |
| 9 | hub 丢弃 `syncState` / `clientId` / `localState` | `lark.go:122-143, 282-305` | 事实 |

（后续小节：§4 搬 / 不搬清单、§5 Agents 对话页、§6 Android 要点、§7 补充阅读、§8 未查清——见下方追加。）

## 补充阅读（中断后追加）

### 补 2.9：removed 的表情回应不是乐观更新
【事实】`LarkChatModel.perform(.reaction…)`（`MF/LarkMessagingModel.swift:2230-2252`）：先按消息权限 `allows("reaction")` 拦截，`isPerformingAction` 期间一次只做一个动作，**等服务端返回后**才提示「已回应 / 已取消回应」，成功后才把这个表情记进最近使用（`recordConfirmedUse`）；界面靠后续刷新 / `reaction.updated` 事件更新。
【判断】Jarvis 的乐观切换 + 失败回滚（`LarkChatView.swift:359-373`）体验更好，保留；可以学的是「最近使用」只记成功的。

### 补 2.6：收件箱回复同样不幂等
【事实】`POST /api/v1/inbox/{id}/reply` → `replyInbox` → `h.lark.Send(ctx, feedID, text)`（`J/server/internal/hub/inbox_triage.go:213-230`）；会话页发送（`J/server/internal/hub/lark.go:120`）、助手工具 `lark_send`（`J/server/internal/hub/assistant_tools.go:679`）也都走 `lark.Send`，每次随机 clientId（`J/server/internal/lark/lark.go:497-504`）。
【判断】三条路径统一改成「调用方给 clientId（手机生成或 hub 按件 / 按工具调用生成并持久化）」，手表上的「发送」同理（手表直接调 `/reply`，`J/docs/inbox.md:101`）。

### 补 2.12：重新配对不清缓存
【事实】`AppModel.pair()` / `unpair()`（`J/ios/Jarvis/App/AppModel.swift:123-152`）清内存状态和凭据，但没有清 `StateCache`（收件箱 `inbox-items`、`lark-chat-<id>`）和 `Caches/lark-media`；媒体缓存键只是 `sha256(url#size)`（`LarkMedia.swift:108-111`），不含 hub 身份。
【判断】个人自用风险低，但「换 hub 后先看到旧 hub 的收件箱」不符合 removed 的「缓存按连接 / 账号隔离、换号即撤」原则（`BH-docs/ARCHITECTURE.md:317`）；重新设计时缓存目录加 hub 作用域，配对变化时整体清掉。

## 4. 搬 / 不搬清单（受「不做第二个飞书」约束）

**搬进飞书会话页 / 收件箱（都是「聊天体验 / 同步 / 可靠发送」，不是飞书功能）**

| 优先级 | 模式 | removed 出处 | 落在 Jarvis 哪里 |
| --- | --- | --- | --- |
| P0 | 发送幂等：客户端 clientId 透传到 lark-connector；出站气泡 + 持久化 + 断网等待；按 clientId / remoteId 精确消气泡，不按文字比 | §2.6 | App Outbox 复用；hub `lark.Send` 加 clientId 参数；`LarkMessage` 透传 `client_id` / `local_state` |
| P0 | 刷新不拼缺口：`after(最后一条)` 追平，追不上就整窗换 latest + 提示；重连通知打开着的会话 | §2.11 | `LarkChatView.refreshLatest`、`LarkSync.reconnected` |
| P0 | 收件箱入口的分界线 / 缓存先画 / 已读语义（先定产品语义） | §2.2、§2.4、§2.12 | `LarkChatView` focus 路径 |
| P1 | 飞书页换成与 Agents 同一套倒置列表引擎；定位按 id 跳 + 实测修正，去掉固定 sleep | §2.1、§2.3 | 共用聊天列表组件 |
| P1 | 已读上报门禁（前台、定位完成、无遮挡、非程序跳转、窗口已核对；450 ms 合并；只前进） | §2.4 | `LarkChatView.markSeen/flushRead` |
| P1 | 「回到最新」数字只数已加载且在视口下方的，尾部未加载显示 N+；跳最新时整窗替换 | §2.3 | 同上 |
| P1 | 草稿按 hub / 账号 / 会话（/ 话题）持久化，版本 UUID + 单调序号；收件箱行显示草稿 / 发送中 / 失败 | §2.5、§2.13 | 会话页输入框、「AI 草拟回复」浮层、`InboxView` 行 |
| P1 | hub 透传 `syncState`，App 只在窗口完整时删除本地行；403 / 404 撤缓存，超时不撤 | §2.11 | `server/internal/lark` 页投影 |
| P2 | 缓存命中规则「包含要打开的那条」；只存连续窗口；缓存按 hub 作用域 | §2.12 | `StateCache` / `LarkSync` |
| P2 | 媒体：落盘前解码校验、按错误类型重试 + 退避、贴纸尺寸预留 | §2.7 | `LarkMedia` |
| P2 | 富文本解析预算 + 超限分页；可选摘要字段独立容错；长文本折叠 | §2.8 | `LarkMessageContent` |
| P2 | 话题回复 sheet 支持往前翻页 | §2.10 | `LarkThreadView` |
| P2 | 分组按 sender id；未读分界打断分组 | §2.2 | `LarkChatView.continues` |
| P3 | 「@我」统一导航按钮（先只覆盖这件事范围内的 @；跨页索引放 hub） | §2.3 | 会话页右下角按钮 |

**不搬（违背「不做第二个飞书」或对「处理一件事」没有价值）**
- 首页：飞书 feed 本身、置顶网格 / 展开、消息 / 未读 / 标记筛选、标签与分组设置、稍后处理 / 已完成 / 会话盒子、自动免打扰、三位数角标规则、事件中心（`BH-docs/FEISHU_PARITY.md:32-55`；`BH-docs/LARK_INBOX_UI_REVIEW.md`）。
- 通讯录（PeopleFeature 67 个文件：组织树、好友申请、拉黑、联系人隐私、我的联系人链接、个人状态、勿扰、登录设备……）、全局搜索页（SearchFeature 4 个文件）、审批（ApprovalFeature 28 个文件：列表、同意 / 拒绝、评论草稿、常用意见、流程图、抄送已读、扩展操作）、工作台（WorkplaceFeature 6 个文件：应用目录、常用管理、服务台）——这些能力只放在 hub / AI 侧（`J/docs/feishu.md:15` 的 `lark_api`）。
- 消息操作：Pin 横幅 / 列表、标记、收藏、转发 / 合并转发 / 多选批量、加急、定时消息、翻译 / 图片翻译、已读人员列表、回应人员列表、编辑 / 撤回 / 删除、群管理、群 Tab / 日历 / 任务 / 机器人。
- 输入：四种行内样式、post 标题、多图图文编辑器、@成员选择器、表情包 / 贴纸库管理、语音录制、视频拍摄与发送、文件发送（Jarvis 飞书页只回文字；需要附件时交给 AI）。
- 话题已读 / 话题阅读位置 / 话题管理（关注、关闭）。
- 离线冷启动的「原凭据 / 证书代次绑定」那一整套（Jarvis 是配对 token + 自己的 hub，已经能缓存先画）。
- 可能保留但需用户决定（【判断】，不是硬性结论）：引用回复（在群里回答某条具体问题，对「处理一件事」有用）。

## 5. 对 Agents 对话页（同样是聊天列表）的启发

【判断】（Agents 页现状以 `J/docs/ios.md:60-114` 为准，未逐行读 `ChatView.swift`）
- 已经做对、和 removed 同构的：跟随状态只由用户手势改变（`followsEnd` vs `LarkChatScrollState.followsLatest`）；按 id 跳 + 实测修正；离开最新端时冻结新行；出站气泡 + `client_msg_id` 幂等重发；本机缓存先画。
- 值得补的：
  1. 「已看」上报门禁：前台 + 初次定位完成 + 无 sheet 遮挡 + 非程序滚动，才算用户看到回复（对应「完成未看 → 已看」，和推送「看过就不推」相关）。
  2. 修正的纪律：只在 idle 修正、≤0.75pt 忽略、用户手势 / 跳转 / 后台 / 换会话使旧修正作废（removed `LarkChatViewportBridge.correct` 拒绝在 tracking / dragging / decelerating 时改 offset）。
  3. 状态分「提交中 / 已受理待确认 / 未知 / 失败」四档，而不是「发送中 / 失败」两档；「未知」只核对不盲重发（Agents 有 hub 幂等保证时可以自动补发，但 UI 上要能看出「结果待确认」）。
  4. 迟到结果隔离：所有异步回写都带 revision / windowRevision / messageRevision 代次（removed 几乎每个 await 之后都校验，例 `MF/LarkMessagingModel.swift:2547, 2563, 2584`），切会话、跳转、后台都递增代次。
  5. 事件流「溢出即全量核对」：缓冲满了不默默丢，转成一次 catch-up（removed `LarkRealtimeClient` / `LarkThreadRealtimeHub`）。Jarvis 的 WebSocket 自己有 `since_seq` 补发，这条主要用在 App 内部的事件分发（`AsyncStream` 缓冲策略）。

## 6. Android 上实现这些聊天列表模式的要点

【判断】（Jarvis 目前没有 Android 代码；以下是基于 Compose 官方行为的设计建议，未在设备上实测，具体 API 版本需在实现时核对）
- **列表方向**：`LazyColumn(reverseLayout = true)`，数据按「新 → 旧」排，最新消息是 index 0。对应 Jarvis iOS 的倒置列表：跳到最新永远是 `scrollToItem(0)`，精确、不依赖估算高度；加载更早的历史是在列表远端追加，不推动当前视口。
- **位置保持基本是框架默认行为**：LazyColumn 以「第一个可见项的 key + 偏移」为锚，数据变化后按 key 找回该项；不可见项（含上方 / 远端）的高度变化不会移动视口。removed 在 SwiftUI 上用 UIKit 标记视图实测行坐标才解决的「前插历史跳位」「上方图片变高推走正文」，在 Compose 上大部分不需要自己写。剩下要处理的是**锚点项自身变高**——靠图片尺寸预留解决。
  - 反过来，「贴底跟随」需要自己做：在 reverse 布局里新消息插在 index 0，框架会保持原来那条可见，所以只在 `followsLatest` 为真时显式 `scrollToItem(0)`；不在底部时什么都不做，正好就是 Jarvis 的「看历史时新行不打扰」。`followsLatest` 只在 `isScrollInProgress` 且由用户拖动时改变（可用 `interactionSource` / `NestedScrollConnection` 区分用户拖动和程序滚动）。
- **稳定 key + contentType**：`items(rows, key = { it.id }, contentType = { it.kind })`；分界线、日期头、「加载更多」都是独立的 item（各自稳定 key，如 `"unread-boundary"`），不要把一组消息包成一个 item（对应 `J/CLAUDE.md`「长列表里不要把一组可能很多的行包成一个子视图」）。
- **窗口而非 Paging 3**：建议自己维护「消息窗口」（`olderCursor / newerCursor`，跳到某条 = 整窗换成 `around(position)`，跳最新 = 整窗换成 latest，刷新 = `after(最后一条)` 追平），状态放 ViewModel 的 `StateFlow<List<Row>>`，磁盘缓存用 Room（按 hub / 账号 / 会话存连续窗口）。Paging 3 的 `PagingSource` 也能做双向分页，但「整窗替换 + 缺口检测 + 出站行合并」用自定义窗口更直接。
- **初次定位到某条（第一条未读 / 这件事的起点）**：两步——先 `scrollToItem(index)` 到大致位置，等一帧后读 `layoutInfo.visibleItemsInfo` 里该项的实际 offset，再 `scrollBy(差值)` 把它放到视口顶部（removed「先 scrollTo，再按实测修正」的同构做法）；不要用固定延时。
- **阅读位置保存**：存 `(消息 id, 该项 offset)`；reverse 布局下 `firstVisibleItemIndex/ScrollOffset` 对应的是**底部**那一项，恢复用 `scrollToItem(indexOf(id), offset)` 最自然。要「屏幕最上面那一行」语义时用 `visibleItemsInfo` 里 index 最大的那项。在 `onPause`（`LifecycleEventEffect(Lifecycle.Event.ON_PAUSE)`）和离开页面时立即写，平时 250 ms 防抖。
- **已读上报**：`snapshotFlow { listState.layoutInfo.visibleItemsInfo }` → 过滤真正可见（按 offset / size 与 viewport 算可见比例）→ `debounce(450)` → 只上报比已报更靠后的位置；门禁 = 生命周期 RESUMED + 初次定位完成 + 没有 BottomSheet / 对话框遮挡 + 不在程序滚动中 + 窗口不是未核对的缓存。
- **图片占位**：消息里有宽高就 `Modifier.widthIn(max = 240.dp).aspectRatio(w / h)`（再夹最大高度），用 Coil 加载（`ImageRequest.size(...)` 让它按显示尺寸降采样，等价 iOS 的 ImageIO thumbnail），鉴权头用 OkHttp 拦截器加；无尺寸时固定占位。行内表情用 `AnnotatedString` + `InlineTextContent`，`Placeholder` 的宽高是固定的，图片晚到也不会重排（比 iOS 的 `[标签]` 占位再重绘更稳）。
- **视频 / 语音 / 文件**：Media3 ExoPlayer + OkHttp 数据源带 Authorization 头，hub 已转发 Range，可直接流式播放；Ogg Opus 语音 ExoPlayer 有 Ogg 解析，Opus 解码依赖系统 MediaCodec（多数新设备有）或 libopus 扩展——需要真机验证。文件下载到 cache 后经 `FileProvider` + `ACTION_VIEW` 交给系统（对应 Quick Look）。
- **出站队列**：Room 表存 `clientId`、会话、文字、状态（提交中 / 已受理 / 未知 / 失败）、创建时间；写表成功后才清空输入框；网络恢复由 WorkManager（`NetworkType.CONNECTED` 约束）按会话顺序补发，带原 clientId。
- **实时事件**：OkHttp WebSocket，`SharedFlow(extraBufferCapacity = N, onBufferOverflow = DROP_OLDEST)` 分发；检测到丢事件（或重连）时发一个「需要全量核对」信号，各页面自己核对窗口。进入后台断开（与 iOS 一致，`J/CLAUDE.md`「App 进后台不断开 WebSocket…现在进后台就断开」），需要时用 FCM 高优先级消息唤醒。
- **卡片 / 富文本**：column_set 的权重 / 固定 / auto 列宽用自定义 `Layout`（注意 `Constraints.hasBoundedWidth`，对应 iOS 的 infinity proposal 坑）；表格放横向 `Modifier.horizontalScroll`；超大消息按预算分页。

## 7. 没查清 / 需要后续确认

- 收件箱 focus 路径不上报初始窗口已读、不显示分界线：是有意的产品选择还是遗漏？（`LarkChatView.swift:257-260` vs `J/docs/ios.md:14`）
- 「`refreshLatest` 拼接缺口」只是代码路径推断，没有在真实会话里复现；需要一个单测（一次刷新间隔里来 41+ 条）确认。
- lark-connector `/agent/v1` 面没有按 clientId 查发送状态的接口（`OL/src/server/routes/api.ts:1396-1399` 注释明确只在 `/api`），hub 若要「未知结果核对」只能靠消息里的 `clientId` 或自己记录；`send` 在 outbox 状态为 pending（在途）时被同一 clientId 再次调用会怎样，没读完（`OL/src/server/services/messenger-service.ts:7305` 之后的分支）。
- （已核对，移出未查清）上游消息页接口用 `registerDomainApi` 的 `p()` 同时注册在 `/api` 和 `/agent/v1`（`OL/src/server/routes/api.ts:3378-3380`），所以 agent 面同样返回 `syncState`，取值有 `complete` / `local` / `gap`（`OL/src/server/repositories/messenger-repository.ts:4192, 4232, 4304`）。hub 只需把它透传出来（现在被丢弃，`J/server/internal/lark/lark.go:282-293`）。
- removed 的阅读位置 / 已读门禁、像素锚点在真机上的表现：它自己的文档也写着真机触摸、帧率、快速拖动空白帧仍待验（`BH-docs/LARK_CHAT_UI_REVIEW.md:23, 31`），不能把它当成已被验证的最佳实践照抄。
- Agents 对话页是否已有「已看」门禁、`ChatView` 的修正时机——本次没读 `ChatView.swift`，以 `J/docs/ios.md` 描述为准。
- Android 部分全部是设计建议，未实测。
