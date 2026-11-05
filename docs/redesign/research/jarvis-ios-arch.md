# Persona iOS 客户端现状架构与技术债调研（重设计起点）

> 调研对象：Persona worktree（只读）`ios/`、`docs/ios.md`、`docs/protocol.md`、`docs/agent-states.md`、`docs/terminal.md`、`CLAUDE.md`「经验」。
> 方法：zoekt / git ls-files / rg（限定子目录）/ Read。**没有构建、没有跑任何测试或脚本**。
> 标注约定：**【事实】** 后面带 `相对路径:行号` 证据；**【判断】** 是我的推断 / 建议，未经运行验证。
> 性能数字只引用代码注释或文档里记载的实测值并注明出处；凡是我自己推断的都写「未实测」。
> 隐私：本文不含主机名、IP、账号、内部项目名、真实人名 / 消息内容；代码里 DEBUG 样例里疑似真实样本的内容也不转述。

---

## 0. 一页结论（先看这个）

1. **单 target 源码级共享，没有模块边界**【事实】：一个 XcodeGen 工程（`ios/project.yml`），App / 手表 / 小组件三个 target 直接把 `Shared/` 源码编进去
   （`ios/project.yml:40-44, 104-108, 132-134`），除 MarkdownUI 外零依赖（`ios/project.yml:13-16`）。生产 Swift ≈ 3.76 万行，其中 Views 2.69 万行（72%）。
2. **`AppModel` 是上帝对象**【事实】：998 行 + 3 个 extension 文件（Outbox / SendSafety / DeviceCredentials），我数出 22 类职责（§3.1）；
   它和 `AgentSync` / `LarkSync` / `InboxStore` / `LiveActivityController` 互相通过 `.shared` 单例调用，形成环。
3. **大量平台无关业务逻辑写在 View 里**【事实】：聊天「行」折叠（`chatLines` 等约 280 行）在 `ChatView.swift` 里且是 `private`，没有单测；
   Agents 排序 `rank` / 分组在 `ChatsView.swift`；收件箱分组在 `InboxView.swift`；飞书已读上报 / 游标在 `LarkChatView.swift` 的 `@State` 里。
4. **同一规则多处实现且已经不一致**【事实】：排序（`AgentState.priority` vs `ChatsView.rank` vs 手表 vs `docs/agent-states.md:17`）、「活着」判定（`state.isLive` vs `status.isLive` vs `status != .offline`）、
   计划审批策略（App 内 / 通知 / 实时活动 / Siri / 手表 各写一份，模式不同）、深链路由（`AppModel.open` / `InboxView.open` / `OpenSessionIntent` 三份，后者对助手会话行为不同）、发消息（只有 `sendNow` 走 Outbox + `client_msg_id`，另有 ≥5 条直连路径）。
5. **测试**【事实】：24 个单测文件 2716 行，覆盖了 ChatStore 连续性 / Rows-Blocks 折叠 / 终端网格 / 实时活动 / Intents 文案等「已抽出来的纯逻辑」；
   但单测宿主是 App 本体（`TEST_HOST`，`ios/project.yml:167-168`），没抽出来的逻辑（chatLines、排序、Outbox flush、AgentSync、LarkSync、LiveConnection）只能靠 UI 测试（依赖 `scripts/mock/mock_hub.py`）和约 80 个 DEBUG 启动参数探针。
6. **协议契约没有单一来源**【事实】：Go `server/internal/proto/proto.go`、Swift `Shared/Models.swift`、`docs/protocol.md` 三份手写；已出现漂移（hub 广播的 `assistant_schedule` App 不解码；文档里的 `session` / `ping` 事件 hub 不发；`hello.features` 文档表里没有）。
   App 的 `ChatStore.apply` 连续性规则还在 Go 测试里被重写了一份做仿真（`server/internal/transcript/continuity_corpus_test.go:6-13`）——Android 若再写一份就是第三份生产实现。
7. **对 KMP 决策的关键输入**：真正要「Android 必须行为一致」的纯逻辑约 **5 千行 Swift**（§5 的 A–D 组合计约 5.1k；另有线上类型约 2.5k、飞书卡片 / 文档纯解析约 1.1k），复杂度集中在
   ① items 连续性（covered/history/Gap）② Rows→Blocks→Turn→Lines 折叠（含唤醒取答、recap 重开、冻结快照）③ Outbox 状态机 ④ 终端 VT 子集 + 折行 + 滚动检测；
   ①② 的 Rows/Blocks 部分和 ④ 已有单测 / Go 仿真 / 录制帧，可以转成跨端 JSON 夹具；②的 Lines 部分和 ③ 几乎没有单测。transcript 的 JSONL 解析本身在 Go 服务端（`server/internal/transcript/`），**不在客户端**。

---

## 1. 工程拓扑

### 1.1 Target 与编译设置【事实】

| Target | 源码 | 说明 | 证据 |
| --- | --- | --- | --- |
| `Jarvis`（iOS App） | `Jarvis/`、`Shared/`、`LiveActivity/` | 依赖 JarvisWatch、JarvisWidgets、MarkdownUI | `ios/project.yml:37-99` |
| `JarvisWidgets`（实时活动扩展） | `JarvisWidgets/`、`LiveActivity/`、`Shared/FleetState.swift`、`Shared/StatusPalette.swift` | `WIDGET_EXTENSION` 编译条件隔离 App 代码 | `ios/project.yml:101-128` |
| `JarvisWatch` | `JarvisWatch/`、**整个 `Shared/`** | 手表只用到其中一小部分（没用 `LiveConnection` / `ChatStore`） | `ios/project.yml:129-154` |
| `JarvisTests` | `JarvisTests/` | `@testable import Jarvis`，**宿主是 App**（`TEST_HOST`），默认 nonisolated | `ios/project.yml:155-168` |
| `JarvisUITests` | `JarvisUITests/` | 跑真实 App + mock / 真实 hub | `ios/project.yml:169-183` |

- Swift 6.0 语言模式、`SWIFT_DEFAULT_ACTOR_ISOLATION: MainActor`、`SWIFT_APPROACHABLE_CONCURRENCY: YES`（`ios/project.yml:19-21`）。
- Release：strip、`SWIFT_REFLECTION_METADATA_LEVEL: without-names`（`ios/project.yml:26-35`）——这是 CLAUDE.md「DEBUG 启动参数不能和 Swift 标识符同名」那条坑的来源之一。
- iOS / watchOS 26（`ios/project.yml:8-10`）。

### 1.2 规模【事实】（`git ls-files … | wc -l`）

| 目录 | 文件 | 行数 |
| --- | --- | --- |
| `ios/Jarvis/App` | 13 | 2,522 |
| `ios/Jarvis/Intents` | 7 | 519 |
| `ios/Jarvis/PhoneTools` | 13 | 2,066 |
| `ios/Jarvis/Views` | 69 | 26,901 |
| `ios/Shared` | 17 | 4,452 |
| `ios/LiveActivity` + `ios/JarvisWidgets` | 5 | 509 |
| `ios/JarvisWatch` | 1 | 594 |
| 生产合计 | | ≈ 37,563 |
| `ios/JarvisTests` | 24 | 2,716 |
| `ios/JarvisUITests` | 16 | 1,537 |

Views 子目录：Chat 6,354 · Inbox（含飞书消息渲染）4,998 · Session（终端 / Git / 文件）4,458 · Feishu Docs 2,336 · Settings 2,258 · Chats 1,081 · Assistant 1,035 · Machines 1,027 · Interactions 812 · Mail 552 · Knowledge 474 · Link 390 · NewChat 364。

最大的文件：`ChatView.swift` 2,598 · `SessionPanes.swift` 1,147 · `AssistantProactiveViews.swift` 1,123 · `LarkCard.swift` 1,105 · `AppModel.swift` 998 · `TerminalView.swift` 927 ·
`ChatStore.swift` 877 · `InteractionViews.swift` 812 · `ChatsView.swift` 783 · `APIClient.swift` 755 · `MessageViews.swift` 749 · `FeishuDocsModels.swift` 744 · `LarkChatView.swift` 728 · `FeishuDocReader.swift` 717 · `Models.swift` 712。

### 1.3 改动热点【事实】（`git log --name-only -- ios/`，改写后的历史 2026-10-24 ~ 10-30，共 243 个涉及 ios/ 的提交）

`ChatView.swift` 93 次 · `AppModel.swift` 52 · `ChatsView.swift` 51 · `APIClient.swift` 36 · `SessionPanes.swift` 33 · `MessageViews.swift` 32 · `Models.swift` 31 · `SettingsView.swift` 28 · `InboxView.swift` 27 · `AssistantView.swift` 25 · `ChatStore.swift` 24。

【判断】一周内 ChatView 被 38% 的 iOS 提交改到，和 CLAUDE.md 里「多个 agent 同时往 master 推 / rebase 用旧副本覆盖别人改动」两条经验对应：大文件 + 单模块 = 并行开发的冲突源。

---

## 2. 现状模块图

```
@main JarvisApp ─┬─ AppDelegate（通知代理 / 静默推送唤醒 / DEBUG 启动参数）
                 ├─ PushRegistrar.shared（APNs 注册 → POST /api/v1/devices）
                 └─ RootView ← .environment(AppModel.shared)
                        │  TabView: Jarvis(AssistantView) / Agents(ChatsView) / 收件箱(InboxView) / 设置
                        ▼
AppModel.shared  (@Observable, MainActor, 998 行 + extensions)
 ├─ LiveConnection (WS: 重连/心跳/stalled) ──decode(后台线程)──► ServerEvent
 ├─ ConnectionStatus (NWPathMonitor + 1.5s 去抖 + catch-up 计数)
 ├─ chats: [SessionKey: ChatStore]  (LRU 8) ──► ChatCache(Caches/chats-v2)
 ├─ machines / sessionsByMachine / interactions ──► StateCache("dev")
 ├─ Outbox / drafts ──► StateCache("outbox"/"drafts")
 ├─ pins / mutes ──► UserDefaults + /api/v1/pins|mutes
 ├─ 终端流路由 terminalHandlers ──► TerminalFeed（View 层持有）
 ├─ ⇄ AgentSync.shared ──► APIClient / ChatCache      （AgentSync 反向读 AppModel.shared）
 ├─ ⇄ LarkSync.shared  ──► StateCache("lark-chat-*")  （反向读 AppModel.shared）
 ├─ ⇄ InboxStore.shared ──► StateCache("inbox-items") / LarkSync / WatchSync（反向读 AppModel.shared）
 ├─ → LiveActivityController.shared ──► ActivityKit / APIClient（反向读 AppModel.shared.api）
 ├─ → WatchSync.shared ──► WatchConnectivity
 ├─ → PhoneToolRegistry ──► EventKit/Contacts/HealthKit/Photos/CoreLocation…
 └─ → MarkdownMemo（View 层的缓存！AppModel.swift:90-97）

Views：32 个文件 81 处 @Environment(AppModel.self)；26 个文件 79 处直接 model.api / .shared.api
Intents / NotificationActions / FleetActions：Keychain.serverConfig + APIClient 直连 hub（绕过 AppModel）
Watch：WatchModel（每 8s 轮询 hub）+ WatchSync（iPhone 推摘要）
Widgets：FleetLiveActivity 只吃 FleetState / StatusPalette
```

### 2.1 组合根与全局状态

**【事实】组合根**：`JarvisApp`（`ios/Jarvis/App/JarvisApp.swift:5-38`）只持有 `AppModel.shared` 注入环境；`AppDelegate.didFinishLaunching`
（`JarvisApp.swift:41-94`）里做通知类别注册、约 12 个 DEBUG 启动参数分发、`WatchSync.activate`、`AppModel.start`、`LiveActivityController.start`、
`PushRegistrar.registerIfPossible`。前后台切换：进后台主动断 WS、回前台 `ensureConnected` + `refresh`（`JarvisApp.swift:27-36`）。
静默推送 `jarvis_wake`：连上 WS 等 25 秒再断（`JarvisApp.swift:104-116`）。

**【事实】单例（10 个 `static let shared`）**：`AppModel`、`AgentSync`、`LarkSync`、`InboxStore`、`LiveActivityController`、`PushRegistrar`、`WatchSync`、`LarkMedia`、`WebViewPool`、`FrameStats`。

**【事实】`AppModel` 职责清单（`ios/Jarvis/App/AppModel.swift`）**：

| # | 职责 | 位置 |
| --- | --- | --- |
| 1 | 配对 / 凭据 / 换设备 token | `pair` 123-137、`unpair` 139-152；`DeviceCredentials.swift:15-44` |
| 2 | WS 生命周期与事件分发 | init 接线 89-106、`start` 116-121、`didConnect` 456-474、`handle` 494-557 |
| 3 | 连接状态 | `connection` 16 |
| 4 | 全局实体缓存 + 快照落盘 | 17-34、`restoreSnapshot/saveSnapshot` 770-793 |
| 5 | 导航状态（tab / 要打开的会话 / 当前助手对话 / 可见会话） | 36-63、`open(_:)` 243-251、`rememberChat` 49-52 |
| 6 | UI 反馈（toast、错误分类展示、触感、手机工具确认弹窗） | 35-37、`show(_:)` 942-947、`notDone` 953-958、`Haptics` 993-998 |
| 7 | 助手（配置、主对话、预热、换启动器、askJarvis） | 175-240 |
| 8 | ChatStore 注册表 + LRU 淘汰（保留 8 个） | `chat(_:)` 257-265、`evictChats` 267-283 |
| 9 | 订阅引用计数 / 可见会话栈 | 285-310、370-380 |
| 10 | 历史分页 / 补洞 / 重载 | `subscribeMessage…loadOlder` 382-452 |
| 11 | 终端流路由与 hub 能力位 | 312-367 |
| 12 | 手机工具注册与执行 | `registerPhone` 478-492、`runPhoneTool/confirm` 570-583 |
| 13 | 交互（待处理列表、resolve） | 171-173、253-255、587-603 |
| 14 | 发送（草稿、Outbox、拒发、重试、排队编辑、/clear、停 loop） | 605-682、914-917；`Outbox.swift`、`SendSafety.swift` |
| 15 | 会话操作（已看、中断、模式、按键、关闭、启动器缓存、改名） | 685-756、877-908 |
| 16 | 同空间同伴计算 | `siblings` 699-715 |
| 17 | 置顶 / 屏蔽规则与乐观更新 | 795-865 |
| 18 | 界面风格 | 760-766 |
| 19 | 跨页面给输入框塞图片 / 上下文 | 869-872 |
| 20 | 全量刷新（含实时活动偏好、fleet） | `refresh` 919-940 |
| 21 | 手表摘要 | `watchSummary` 960-966（及 98-103、514、544、547 处推送） |
| 22 | 「做完了」触感 | `notifyTransitions` 559-566 |

【判断】22 类职责、读写 4 种持久化（Keychain / UserDefaults / StateCache / ChatCache）、直接调 UIKit（`UIDevice`）和 SwiftUI（`withAnimation`，822、863 行），无法单独测试也无法在 Android 复用其结构；这是重构第一刀。

---
### 2.2 网络层

**【事实】REST：`APIClient`**（`ios/Shared/APIClient.swift:65-626`，`nonisolated struct`，`Sendable`）
- 每次 `AppModel.api` 访问都新建一个（`AppModel.swift:70`）；Bearer token；`X-Client-Request-Id`；5xx 错误带 `X-Request-Id`（`APIClient.swift:85-108`、`24-37`）。
- 大响应（sessions、items、inbox）在 `Task.detached` 里解码（`APIClient.swift:188-193, 218-225, 277-281`；`InboxModels.swift:128-132`）。
- `uncachedSession`：App 自己做磁盘缓存的字节（对话图片、飞书媒体）不走系统 URLCache（`APIClient.swift:69-76`；CLAUDE.md「hub 的 API 默认回 no-store」一条）。
- **端点分散在 9 个文件的 extension**：`APIClient.swift`（核心 + pins/mutes/rename）、`DevModels.swift:98-118`（git/fs）、`InboxModels.swift:125-166`、`Knowledge.swift:97-112`、
  `Views/Mail/MailViews.swift:5-90`、`Views/Inbox/Lark/LarkModels.swift:176-296`、`Views/Chat/FileViewer.swift:22-26`、`Views/Feishu/Docs/FeishuDocsComponents.swift:6-19`、`Views/Feishu/Foundation/FeishuFoundation.swift:8-30`。
  `"/api/v1…` 字面量：APIClient 44 处、LarkModels 21、MailViews 10、InboxModels 6、Knowledge 4……
- 错误分类集中在 `Error` 扩展：`isCancellation / isTimeout / isOffline / isQuiet / shown`（`APIClient.swift:634-674`）——CLAUDE.md「取消不是错误」「超时不提示」两条的落点，但**是否展示仍由每个调用点自己决定**。

**【事实】WebSocket：`LiveConnection`**（`ios/Shared/LiveConnection.swift:135-322`，`@Observable`）
- `ServerEvent` 信封解码（`LiveConnection.swift:17-133`）：hello / machines / sessions / items / screen / delta / interaction / interaction_resolved / tool_call / hook_event / revs / lark / inbox / fleet / terminal / terminal_control / error。
  在 `URLSessionWebSocketTask.receive` 回调线程上解码，再 `Task { @MainActor }` 回主线程（`LiveConnection.swift:267-297`）。
- 配置常量（不是实测值）：ping 每 20s、无任何消息 45s 判死、pong 超 5s 记为 stalled 并改为每 5s ping、退避 1→2→…→20s、`maximumMessageSize` 64 MB（`LiveConnection.swift:167-170, 220, 299-308`）。
- 用 `generation` 计数丢弃旧连接回调（`LiveConnection.swift:161, 209-213`）。
- `send(_:)` 在未连接时**静默丢弃**（`LiveConnection.swift:310-321`）。【判断】`tool_result`、`terminal_input`、`seen` 等都可能在断线瞬间丢失且无反馈；`markSeen` 自己做了 HTTP 兜底（`AppModel.swift:685-690`），其余没有。
- 重连后订阅重放：`didConnect` 对所有已订阅会话发 `subscribe` 带 `since_seq = covered`（`AppModel.swift:456-474, 383-388`）。

**【事实】连接状态：`ConnectionStatus`**（`ios/Jarvis/App/ConnectionStatus.swift`，151 行）
- 4 态 `online / waitingForNetwork / connecting / updating`；纯函数 `phase(network:link:stalled:catchingUp:)`（`ConnectionStatus.swift:77-82`）可测且有单测；
  1.5s 去抖、catch-up 最长 8s（`35-37`）；网络恢复立即重连、换网络立即 ping（`111-123`）。catch-up 完成回调触发 Outbox flush（`AppModel.swift:106`）。

### 2.3 会话数据与同步

**【事实】`ChatStore`**（`ios/Shared/ChatStore.swift:266-583`，每会话一个，`@Observable`）
- 状态：`items`（去重 + 按 `(seq, part)` 排序）、`rows` / `blocks` / `images`（派生）、`version`、`resets`、`hasMore`、`loadedOnce`、`pending`（发送中的消息）、`liveText`（助手流式）、`covered` / `history`（连续性游标）。
- `apply(_:span:backlog:hasMore:moreAfter:) -> Gap`（`ChatStore.swift:322-359`）：按 `history` 换文件就清空；接续批次 `from <= covered` 才推进 `covered`，否则返回 `.fill`；从末尾数的页不接上本地时整体替换。
  `merge` 里 `hasMore` 只在页能覆盖到最早一条时才信（`ChatStore.swift:381-384`，对应 CLAUDE.md「has_more 只有从最新一端分页的响应才知道」）。
- 派生重建在后台 `Task.detached`，增量重建复用「最后一个 prompt 之前」的结果（`SettledRows`，`ChatStore.swift:397-472`）；代码注释记载「全量重建在 1000 轮时每个事件 30 ms」（`ChatStore.swift:401-402`，未注明样本数）。
- 写盘：最后两轮、2 秒防抖（`ChatStore.swift:474-488`）。
- **init 里同步读盘 + 解码 + 全量 buildRows**（`ChatStore.swift:301-314`），而 `AppModel.chat(_:)` 在 View body 的计算属性里被调用（`ChatView.swift:98`）。【判断】首次打开大缓存（上限 6 MB，`ChatStore.swift:795`）的会话时主线程有一次同步解码，未实测耗时。
- `screen / screenStatus / screenUpdatedAt / livePreview / spinnerLine`（`ChatStore.swift:283-287, 526-528`）只被 `AppModel.handle(.screen)` 写（`AppModel.swift:533-537`），**全仓没有读者**；`docs/protocol.md:164` 也写明 node 已不再随订阅推屏幕——死状态。

**【事实】订阅 / 补洞 / 分页（在 AppModel 里）**：`subscribe` 引用计数（同一会话多个视图不互相取消，`AppModel.swift:287-308`）；
`settle(.fill)` 先等 1.5s 再 `fill`（最多 8 页 `after_seq`，超过就 `reload` 最新页，`AppModel.swift:402-433`）；`loadOlder` 用 `before_seq`、`turns=2`（`AppModel.swift:441-452`，`APIClient.swift:218-225`）。
与 `docs/protocol.md:240-248`「items 的连续性」一致。（`docs/archive/lanes/msg-render-plan.md` 已对这条链路做过诊断，本文不重复，只在 §6 标出相关逻辑的位置。）

**【事实】后台同步**
- `AgentSync`（`ios/Jarvis/App/AgentSync.swift`，148 行）：hub 推 `rev`（会话列表 / `revs` 事件），和本地 `synced`（UserDefaults `agentSync.revs`）不同就拉 `after_seq`；
  范围 = 非离线或 24h 内更新、最多 40 个、并发 3、300ms 合并（`AgentSync.swift:13-15, 50-80`）；内存里有 ChatStore 就合并，否则在后台线程更新磁盘缓存（`pullToDisk`，`125-147`）。
- `LarkSync`（`ios/Jarvis/App/LarkSync.swift`，104 行）：`lark` 事件 → `AsyncStream` 通知打开的会话 + 后台拉最新一页写 `lark-chat-<id>`；并发 2、同一会话最快 3s 一次、重连补最近 20 个（`LarkSync.swift:11-15`）。
  同一个缓存键 `lark-chat-<id>` 也被 `LarkChatView.apply` 写（`LarkChatView.swift:247`）——两个写者靠约定协调。
- `InboxStore`（`ios/Jarvis/App/InboxStore.swift`，149 行，`@MainActor @Observable`）：`inbox` 事件 / 重连时 `reload`（合并并发请求），乐观标已处理失败回滚，AI 分身草稿一键发送；
  每次列表变化把 `AppModel.shared.watchSummary()` 推给手表（`InboxStore.swift:54-61`）。

### 2.4 缓存与持久化【事实】

| 存储 | 内容 | 位置 |
| --- | --- | --- |
| Keychain | `server`（配对）、`watch`（手表只答凭据） | `ios/Shared/Keychain.swift:33-48` |
| `StateCache`（Caches/state/*.json，写盘在 detached task） | `dev`（machines/sessions/interactions/assistant 快照）、`outbox`、`drafts`、`inbox-items`、`mail-accounts`、`lark-sync`、`lark-chat-<id>`（最近 80 条） | `ios/Shared/StateCache.swift:5-24`；调用方见 `AppModel.swift:779-793`、`Outbox.swift:73-94`、`SendSafety.swift:50-63`、`InboxStore.swift:16-19`、`LarkSync.swift:27-30, 90` |
| `ChatCache`（Caches/chats-v2） | 每会话最后两轮 items + hasMore/history/covered，超过 6 MB 去掉图片 | `ios/Shared/ChatStore.swift:786-837` |
| `ChatImageLoader` | NSCache（cost 上限 96 MB）+ Caches/chat-images，同一 ref 只下载一次（actor） | `ios/Jarvis/Views/Chat/ChatImageLoader.swift:6-73` |
| `AttachedImageCache` | 静态字典，**无上限** | `ios/Jarvis/Views/Chat/MessageViews.swift:373-378` |
| `RowMemo` / `TextMemo` / `MarkdownMemo` | 按消息 id / 文本缓存派生结果（Markdown 解析、路径、链接），上限 20,000 / 4,000 / 128 条，满了整表清空 | `MessageViews.swift:699-723`、`ios/Shared/TextMemo.swift` |
| `LarkMedia`、`WebViewPool` | 飞书媒体内存 + 磁盘；最多 6 个预加载 WKWebView（LRU） | `Views/Inbox/Lark/LarkMedia.swift:11`、`docs/ios.md:43` |
| UserDefaults | 132 处调用（30 个文件），约 87 个字面量键，其中约 80 个是 DEBUG 启动参数；还有 `@AppStorage` 约 20 个键、`chat.read.<会话>`、`agentSync.revs` 等动态键 | `rg 'UserDefaults\.standard'` |
| 存储页 | 按目录名归类统计 / 清除 | `ios/Jarvis/Views/Settings/StorageView.swift:66-145` |

`TextMemo` 的注释记载：对话行派生计算曾占「长会话主线程时间约 15%」（`ios/Shared/TextMemo.swift:3-5`，profile 结论，未注明样本）。

### 2.5 发送链路

**【事实】标准路径（Agents 对话 / 助手对话里的输入框）**：`Composer.sendText` → `AppModel.sendNow`（`ChatView.swift:1854-1866`，`AppModel.swift:631-637`）→
`PendingMessage` 进 `ChatStore.pending` 立即上屏 → 在线就 `sendPending`（带 `client_msg_id`，`SendSafety.swift:23-42`），否则 `.waiting` 存盘（`Outbox.swift:75-83`）
→ 重连 catch-up 完成后按会话、按顺序 flush，未打开的会话先拉一页确认是否已送达（`Outbox.swift:50-71`）→ transcript 里出现同样的话（`PendingMessage.same`，`ChatStore.swift:850-869`）就去掉；`sent` 最多再挂 60s（`Outbox.swift:32-37`）。
拒发：409 `input_draft` 弹窗「清空并发送」、`delivered: unknown` 提示可能已送达（`SendSafety.swift:29-35`）。

**【事实】绕过 Outbox 的发送路径**（没有 `client_msg_id`、断网直接失败）：
- `AppModel.send`（用于 `/clear`，`AppModel.swift:672-682, 642-652`）；
- `AppModel.askJarvis`（其他页面「交给 Persona」，`AppModel.swift:207-223`）；
- `AssistantView.start`（助手新对话首句，`ios/Jarvis/Views/Assistant/AssistantView.swift:101-123`）；
- `LarkChatView.send`（飞书回复，`LarkChatView.swift:402-416`）、`InboxStore.send`（AI 分身草稿）；
- Siri `AskClaudeIntent.deliver`（`ios/Jarvis/Intents/AskClaudeIntent.swift:67-77`）；手表 `WatchModel.send`（`ios/JarvisWatch/JarvisWatchApp.swift:107-116`）。
【判断】「发消息」的可靠性语义按入口不同，Android 若照抄会继承这种不一致；应在 Core 里定义一个发送用例（幂等 id + 等网 + 拒发处理），各入口共用。

### 2.6 交互（Interaction）与审批策略

**【事实】** `pendingInteractions` 按创建时间排序；`resolve` 先在本地删卡，失败（非 410）放回，`prompt_changed` 给专门提示（`AppModel.swift:587-603`）。
屏幕卡优先用 hub 解析的 `dialog`（按 `option` 回答，node 核对 `rev`），老 hub 退回 App 自己的 `ScreenParser.dialog`（`Models.swift:469`、`InteractionViews.swift:701-705`）。

**【事实】计划审批的模式在 5 个入口各写一份**：
| 入口 | 批准用的 mode | 证据 |
| --- | --- | --- |
| App 内计划卡 | `bypassPermissions`（批准并自动执行）或 `acceptEdits` | `ios/Jarvis/Views/Interactions/InteractionViews.swift:466-476` |
| 通知按钮 | `acceptEdits`（注释：没看全计划，命令仍要逐个授权） | `ios/Jarvis/App/NotificationActions.swift:42-48` |
| 实时活动按钮 | 复用通知的映射 → `acceptEdits` | `ios/Jarvis/App/FleetActions.swift:9-15`、`ios/LiveActivity/FleetIntents.swift:44-62` |
| Siri「批准第一个」 | `bypassPermissions` | `ios/Jarvis/Intents/ApproveFirstIntent.swift:36-42` |
| 手表 | `bypassPermissions`；「打回」固定意见「请重新考虑这个计划」 | `ios/JarvisWatch/JarvisWatchApp.swift:364-365` |
【判断】注释说明了「锁屏不全看计划 → acceptEdits」的意图，但 Siri（同样看不到全文）和手表却是 bypass；这类安全策略应当在一张表里定义并有测试。

### 2.7 推送、通知、后台唤醒【事实】
- `PushRegistrar`（`JarvisApp.swift:209-246`）：申请权限 → APNs token → `POST /api/v1/devices`（`kind: iphone`、`apns_env`、`topic`）；设备 id 存 UserDefaults `deviceID`。
- 通知按钮（`NotificationActions.swift`）在后台直接 resolve，失败发本地通知（`JarvisApp.swift:155-172`）；代理用 completion-handler 版本切主队列（`JarvisApp.swift:118-150`，CLAUDE.md 对应经验）。
- hub 侧推送只有 APNs（`server/internal/hub/push.go:28-63`）。【判断】Android 需要 hub 增加 FCM（或其他）通道和设备类型，这是服务端工作项。

### 2.8 手机工具【事实】
- `PhoneTool { spec, source, confirmation?, run }`（`ios/Jarvis/PhoneTools/PhoneTool.swift:4-10`）；9 个数据源开关存 UserDefaults（`PhoneTool.swift:13-66`）。
- `PhoneToolRegistry.call` 是唯一入口：未知工具 / 数据源关闭 / 用户拒绝 / 抛错都转成给 Claude 看的中英双语文本，**从不抛**（`PhoneToolRegistry.swift:95-119`）；写操作先 `confirm`（`AppModel.confirm` 用 continuation 挂起等弹窗，`AppModel.swift:578-583`）。
- 连上 WS 就 `register_phone` 声明启用的工具 spec（`AppModel.swift:478-492`）；hub 把 `tool_call` 只发给注册过的手机（`docs/protocol.md:169, 181`）。
- 单测覆盖：名字唯一 / schema 合法 / 开关 / 拒绝确认 / 参数夹紧 / 日期解析（`ios/JarvisTests/PhoneToolsTests.swift`）。
【判断】协议本身与平台无关（spec 由客户端声明），Android 可以声明自己的工具集；但工具名 / 参数 / 返回 JSON 形状若要让同一个助手 prompt 两端通用，需要一份共享的工具契约。

### 2.9 Siri / 快捷指令【事实】
- 3 个 AppShortcut + `OpenSessionIntent`（`ios/Jarvis/Intents/JarvisShortcuts.swift`），后台 intent 直接 `Keychain.serverConfig` + `APIClient`（`IntentSupport.swift:133-144`）。
- 纯逻辑抽在 `IntentText / IntentPhrases / IntentFailure / IntentPicker`（`IntentSupport.swift:8-165`），有 14 个单测（`IntentsTests.swift`）——这是全仓「逻辑 / 视图分离」做得最好的一块。
- `OpenSessionIntent.perform` 无条件 `selectedTab = .chats`（`OpenSessionIntent.swift:68-74`），而 `AppModel.open` 会把助手会话路由到 Persona tab（`AppModel.swift:243-251`）；`InboxView.open` 又复制了一份路由（`InboxView.swift:186-194`）。
- `JarvisStatusIntent` 用 `status == .working` 计数（`JarvisStatusIntent.swift:24`），UI 用 `AgentState`。

### 2.10 手表【事实】
- `WatchSync`（`ios/Shared/WatchSync.swift`）：iPhone → 手表推配对（applicationContext + transferUserInfo）和摘要（2s 节流）；手表可 `sendMessage` 主动要配对。
- `WatchModel`（`JarvisWatchApp.swift:21-150`）：打开时每 8s 直连 hub 轮询 sessions / interactions / machines / inbox；回答、发消息、中断、收件箱回复都直连 hub。
- 手表自己的排序：`state.priority` + `sortDate`，过滤条件和 Agents 列表不同（`JarvisWatchApp.swift:56-58`）。

### 2.11 实时活动（Fleet）【事实】
- 内容由 hub 计算（`FleetState`，时间一律 Unix 秒，宽松解码，`ios/Shared/FleetState.swift:1-161`）；App 前台时自己应用 `fleet` 事件、上报 push-to-start token 和活动 token（`ios/Jarvis/App/LiveActivityController.swift:57-171`）。
- 灵动岛两侧算法 `IslandCell.sides/minimal`、文案 `FleetText`、按设备裁剪 `tailored`（`FleetState.swift:142-364`），12 个单测（`FleetStateTests.swift`）。
- 设置镜像在 UserDefaults（`LiveActivityController.swift:17-24`），真值在 hub（`NotifyPrefs`）。

### 2.12 终端子系统【事实】（`ios/Jarvis/Views/Session/Terminal/` 共 2,668 行，加 `SessionPanes.swift` 的 `TerminalPanel`）
- `TerminalGrid`（419 行，纯逻辑）：herdr 已仿真好的网格的 VT 子集（CSI 光标 / 擦除 / 插删 / SGR 256 色 + 真彩 / OSC 8 链接）、宽字符两格与「右半格」特殊规则、`scrolled(from:to:)` 推断滚出屏幕的行（`TerminalGrid.swift:393-419`）。
  选型理由（不用 SwiftTerm / libghostty）见 `docs/terminal.md:58-78`。
- `TerminalWrap`（141 行，纯逻辑）：按手机宽度折行、悬挂缩进、CJK 断行规则。
- `TerminalFeed`（351 行，`@MainActor @Observable`，放在 Views 目录）：流 / 轮询两种模式、epoch+seq 续传与丢帧重订、接管（control）状态机、回滚缓冲 3000 行、退避 1/2/4/8/15s（`TerminalFeed.swift:77-106, 206-289`）。
  它通过 `AppModel.terminalSubscribe` 注册回调（`AppModel.swift:330-338`），**订阅时机依赖视图第一次布局测出的列×行**（`TerminalFeed.swift:80-83`；CLAUDE.md「接管要等终端视图第一次布局量出手机的列 × 行才订阅」）。
- 按键：`KeyBatcher`（合并文本 / 回退未发出的字符）、`TerminalKeyBytes`（herdr 键名 → VT 字节，接管时直接写字节）、`TerminalModifiers`（Moshi 式粘滞 / 锁定 / 按住）、`TerminalCompletion`（从屏幕内容补全）——均为纯逻辑（`TerminalInput.swift:16-107`、`Views/Chat/TerminalKeys.swift:1-70`、`TerminalExtras.swift:282-320`）。
- 渲染 `TerminalView`（927 行，UIScrollView + CoreGraphics 按格画、选择、编辑菜单、滚轮 / 点击上报），输入 `UITextInput`（拼音 marked text）。
- 测试：`TerminalGridTests`（10）、`TerminalWrapTests`（约 14，含真实录制帧夹具 `ios/JarvisTests/Fixtures/terminal-*.jsonl`）、`TerminalTests`（12）。

### 2.13 Views 组织【事实】
按页面分目录：`Assistant / Chat / Chats / Feishu / Inbox(+Lark) / Interactions / Knowledge / Link / Machines / Mail / NewChat / Session(+Terminal) / Settings`，外加 `Components.swift`、`Styling.swift`、`RootView.swift`、`ConnectionTitle.swift`。
- 设计系统：`Shared/Theme.swift`（5 种 `AppStyle`、色板 / 圆角 / 间距 / 字体 token、`Fmt` 格式化）、`Shared/StatusPalette.swift`（状态色，App / 手表 / 小组件共用）、`Views/Styling.swift`（`.surface` / `.themedButton` / `.themedPage`）。
  风格是全局可变静态量 `AppStyle.current`（`nonisolated(unsafe)`，`Theme.swift:17-18`），换风格靠 `RootView` 以风格为 id 整树重建（`RootView.swift:17-18`）。
- 文件归属不一致：终端键栏 `TerminalKeys.swift` 在 `Views/Chat/`；模型类 `TerminalFeed`、`FileUploads`、`LarkMedia` 在 Views 下；API extension 散在 View 文件里（§2.2）。

### 2.14 依赖关系（谁引用谁）【事实】

| 模块 | 引用 | 被谁引用 |
| --- | --- | --- |
| `AppModel` | LiveConnection、ConnectionStatus、ChatStore、APIClient、Keychain、StateCache、WatchSync、PushRegistrar、LiveActivityController、AgentSync、LarkSync、InboxStore、PhoneToolRegistry、**MarkdownMemo（View 层）**、UIKit、SwiftUI | 32 个 View 文件（Environment）、AgentSync / LarkSync / InboxStore / LiveActivityController / ChatImageLoader / LarkMedia / OpenSessionIntent / PushRegistrar（`AppModel.shared`） |
| `AgentSync` | `AppModel.shared`（live、api、sessionsByMachine、isSubscribed、existingChat、fill、reload）、ChatCache、ChatStore 静态函数 | AppModel |
| `LarkSync` | `AppModel.shared`（api、live.state）、StateCache | AppModel、InboxStore、LarkChatView |
| `InboxStore` | `AppModel.shared`（api、show、toast、watchSummary）、LarkSync、WatchSync | AppModel、InboxView、RootView 角标、Watch 摘要 |
| `ChatStore` | ChatCache、ScreenParser（同文件） | AppModel、AgentSync、ChatView、ChatsView 等 |
| `LiveConnection` / `APIClient` / `Models` | 只依赖 Foundation | 全部 |
| Intents / FleetActions / NotificationActions | Keychain + APIClient；FleetActions 还调 LiveActivityController | 系统 |
| Watch | Shared 全部源码（实际用到 Models、APIClient、WatchSync、Theme、StatusPalette、WatchText、ScreenParser.options） | — |

【判断】只有 `Shared/` 里的 `Models / APIClient / LiveConnection / ChatStore / FleetState / MachineMetrics / JSONValue` 已经是「不依赖 App」的干净层，可以几乎原样抽成包；`App/` 下的同步器全部通过 `AppModel.shared` 反向耦合。

---

## 3. 状态管理与并发模型

### 3.1 状态管理【事实】
- **Observation 为主**：`@Observable` 类 12 个（AppModel、LiveConnection、ConnectionStatus、ChatStore、InboxStore、TerminalFeed、ChatChrome、MessageMenu、FileUploads、FeishuDocsReaderModel、LarkMedia、WatchModel）。
  只有一处遗留 `ObservableObject`：`InboxAccountState`（`InboxView.swift:3-5, 18`）。
- **全局单例 + 环境注入并存**：根上 `.environment(AppModel.shared)`（`JarvisApp.swift:14`），但 `InboxStore.shared`、`LarkSync.shared` 等在 View 里直接用（例：`RootView.swift:99`、`LarkChatView.swift:138`）。
- **环境闭包当依赖注入**：17 个 `@Entry`，多数是动作闭包（`openFile`、`loadImage`、`loadFullText`、`attachImage`、`openLink`、`docPick`、`openSpace`、`openSubagent`、`feishuOpen`、`larkOpenFile`、`larkRefresh`、`larkOpenProfile`、`larkReact`…，`MessageViews.swift:84-98` 等）。
  CLAUDE.md「环境值要挂在 NavigationStack 本身上」就是这种隐式注入的坑。
- **不被观察的簿记对象**：`ChatScratch`（`ChatView.swift:2534-2572`）放滚动中写入的状态和派生行缓存，避免整页重算；`ChatChrome` 单独观察顶栏折叠进度（`ChatView.swift:2487-2498`）。
- **「命令即状态」导航**：`AppModel.openChat` 被 `ChatsView` 的 `onChange` 消费后置 nil（`ChatsView.swift:274-278`）；`selectedTab`、`assistantCurrent` 同理。没有集中路由器。
- **View 内持有业务状态**：`LarkChatView` 的 15+ 个 `@State`（游标、已读、未读锚点，`LarkChatView.swift:26-48`）；`ChatView` 约 30 个 `@State`（`ChatView.swift:16-88`）；`TerminalPanel` 持有 `TerminalFeed`（`SessionPanes.swift:106`）。

### 3.2 并发【事实】
- 默认 MainActor 隔离（`ios/project.yml:20`）：所有类型默认在主 actor，纯数据类型必须手动标 `nonisolated`（CLAUDE.md 经验；`Models.swift` 每个类型都标了）。测试 target 反过来默认 nonisolated（`ios/project.yml:165`）。
- 后台工作手段：`Task.detached` 23 处（解码、重建行、写盘、预热 Markdown）；`actor` 只有 `ChatImageLoader.Fetches` 一个（`ChatImageLoader.swift:49`）；锁 + `@unchecked Sendable` 的 `TextMemo`（`TextMemo.swift:6`）。
- 非结构化 `Task {` 约 213 处（含 detached）；`nonisolated(unsafe)` 16 处（WCSession 回调、ActivityKit、静态缓存、`AppStyle.current`）；`MainActor.assumeIsolated` 2 处（通知代理）。
- WebSocket 消息在回调线程解码后再跳主线程（`LiveConnection.swift:269-279`，CLAUDE.md 对应经验）；ChatStore 重建按 revision 合并、只发布最新快照（`ChatStore.swift:449-472`）。
【判断】并发安全主要靠「默认主线程 + 少量 detached 纯函数」，简单有效；但 213 处 fire-and-forget Task 没有取消归属（例如 `settleSend` 里 60s 后删气泡、`settle` 里 1.5s 后 fill），生命周期难以推理和测试。

---

## 4. 技术债与痛点

### 4.1 超大文件 / 职责混杂【事实 + 判断】
- `ChatView.swift` 2,598 行装了：列表引擎（倒置列表、冻结、着陆、贴底）、顶栏（跟手折叠）、Composer（照片 / 相机 / 文件 / 斜杠命令 / `/clear`）、QueueStrip、DevInfoCard、TerminalCover、CameraPicker、
  **折叠算法**（`chatLines` 268-329 等）、`ChatLine` / `WorkItem` / `WorkSummary` 模型、约 150 行 DEBUG 探针（`appeared` 1374-1526）。
  CLAUDE.md「Release 归档 type-check 超时」就是这里长修饰符链导致的（`ChatView.swift:1033` 注释）。
- `AppModel.swift` 22 类职责（§2.1）。
- `SessionPanes.swift` 1,147 行：`TerminalPanel`（键盘、粘贴、捏合、双指切会话、模式切换）+ 循环 / 后台任务 / 子 Agent 条 + `KeyStream`。
- `AssistantProactiveViews.swift` 1,123 行（定时任务 / 跟进 / 记忆 / 关于我 4 个页面）；`InteractionViews.swift` 812 行（4 种表单 + DEBUG 样例）。

### 4.2 业务逻辑在 View 里（难测、无法复用）【事实】
| 逻辑 | 位置 | 测试 |
| --- | --- | --- |
| 聊天行折叠 `chatLines / segments / workItems / photos / wakeLines / unfoldWork / prefixCount`、冻结快照 `Frozen` | `ChatView.swift:42-47, 238-329, 425-436, 490-531, 593-608` | `chatLines` 是 private，无单测；只测了 `ChatLine.id` 形状（`ChatLineTests.swift`）和 `prefixCount`（`ChatStoreTests.swift:406`） |
| Agents 排序 / 分组 / 折叠 `rank / ordered / stays / SpaceGroup.build / isWaiting` | `ChatsView.swift:4-49, 100-127, 304-333` | 无 |
| 收件箱过滤 / 分组 / 待处理 Agent 混排 | `InboxView.swift:31-47, 95-101` | 无 |
| 飞书会话：未读锚点、游标分页、已读上报（只报上过屏的）、轮询兜底、乐观表情回应 | `LarkChatView.swift:243-416` | 只测了 `LarkPosition` 比较和 `togglingReaction`（`LarkRenderTests.swift`） |
| 提问表单作答规则（多选拼接、「其他」覆盖、单题单选即提交） | `InteractionViews.swift:385-434` | 无 |
| 屏幕卡作答（option / preKeys / keys） | `InteractionViews.swift:700-705` | UI 测试 `SendSafetyTests` |
| 手机宽度 / 双指切换会话的空间顺序 | `SessionPanes.swift:567-584` | 无 |
| 助手新对话、改名、/clear 后跟随新会话 | `AssistantView.swift:101-123`、`AppModel.swift:642-652` | 无 |
| 提示词模板（交给 Persona、飞书总结、停止 /loop、斜杠命令） | `KnowledgeView.swift:465-474`、`LarkChatView.swift:418-428`、`AppModel.swift:914-917`、`ChatView.swift:1908-1914` | 无 |

### 4.3 同一规则多份实现 / 已不一致【事实】
1. **状态排序**：`AgentState.priority` = 需要你 → 完成未看 → 工作中 → 已看 → 未知 → 未运行（`Models.swift:312-324`）；`ChatsView.rank` 另加「待唤醒 / 已屏蔽」（`ChatsView.swift:313-322`）；
   手表用 `priority`（`JarvisWatchApp.swift:56-58`）；`docs/agent-states.md:17` 写的是「需要你 → **工作中 → 完成未看**」，与代码相反（同文件 20 行又与代码一致）。
2. **「活着 / 在工作」判定**：`state.isLive`（ChatsView、SpaceView、MachineSpacesView、siblings）、`status.isLive`（`MachinesView.swift:104`、`watchSummary` `AppModel.swift:961`、ChatView 菜单 1190）、
   `status != .offline`（`AgentSync.swift:65`、`IntentSupport.swift:157`）、`status == .working`（`JarvisStatusIntent.swift:24`）vs `state == .working`（多处）。两套状态（herdr 的 `SessionStatus` 和 hub 统一的 `AgentState`）在 UI 里混用。
3. **「空闲时折叠」条件**：同一个 `stays` 写了两遍（`ChatsView.swift:162-165` 闭包 与 `305-308` 函数）。
4. **计划审批模式**：5 个入口 3 种取值（§2.6）。
5. **深链路由**：`AppModel.open`、`InboxView.open`、`OpenSessionIntent`（§2.9）。
6. **发送**：1 条走 Outbox，≥5 条直连（§2.5）。
7. **助手会话排除规则**：「机器页面不算助手会话」在 `MachinesView.swift:102-104`、`MachineSpacesView.swift:13`、`ChatsView.swift:102` 各写一次（CLAUDE.md 对应经验就是这里漏过一次）。
8. **监控阈值**：`MetricAlert.of` 定义了 75/90、80/90（`MachineMetrics.swift:151-164`），`MachineDashboardView.swift:129-135`、`MachineMonitorView.swift:154-161` 又写了一遍字面量。
9. **连续性规则**：Swift `ChatStore.apply` 与 Go 仿真（`continuity_corpus_test.go:6-13`）。

### 4.4 隐式依赖与生命周期【事实 + 判断】
- 同步器之间的环（AppModel ⇄ AgentSync / LarkSync / InboxStore）只能整体测试；`AgentSync.plan` 读 `AppModel.shared.sessionsByMachine`（`AgentSync.swift:60-80`）。
- `AppModel.chat(_:)` 有副作用（LRU 计数、创建 store、淘汰），却在 View body 的计算属性里调用（`AppModel.swift:257-265`、`ChatView.swift:98`）。
- App 层引用 View 层缓存：`live.prepareEvent` 预热 `MarkdownMemo`（`AppModel.swift:90-97`）。
- `AgentSync.pullToDisk` 和随后创建的 ChatStore 读写同一缓存文件、`LarkSync` 与 `LarkChatView` 写同一 `lark-chat-<id>`：【判断】靠时序约定，存在覆盖竞态的可能，未验证。

### 4.5 测试结构【事实】
- 单测宿主是 App（`ios/project.yml:167-168`），跑单测会启动 App（`AppModel.shared` 初始化、申请通知权限）——CLAUDE.md「xcodebuild test 会启动不带 -pairURL 的 App 申请通知权限，之后截图都被弹窗挡住」。
- 覆盖好的：ChatStore 连续性与折叠（`ChatStoreTests` 30+ 个、`RebuildTests` 含 40 个种子的增量 = 全量性质测试，`RebuildTests.swift:50-73`）、终端、实时活动、Intents、手机工具注册表、飞书文档 / 消息解析。
- 只有 UI 测试或完全没测的：chatLines、Agents 排序、收件箱分组、Outbox flush 顺序（`ConnectionTests.testMessageWaitsForTheHub`）、拒发（`SendSafetyTests`）、LiveConnection 心跳 / 重连、AgentSync、LarkSync、飞书已读上报、`ToolPresentation`、`DiffText.parse`、`Fmt`、`WebLinks`、`PlanText`。
- UI 测试 16 个文件，绝大多数依赖 `scripts/mock/mock_hub.py`（1,040 行 Python 假 hub，可通过环境变量模拟断网、拒发、慢上传等，见各文件头注释），少数连真实测试 hub（`MachinePagesLiveTests.swift:1-5`）。
- 约 80 个 DEBUG 启动参数（`-openSession`、`-showFoldProbe`、`-fromSeq`、`-showMenu`…，`docs/ios.md:299-305`），探针把结果写进 `Documents/*.txt`（`ChatView.swift:334-421`）；66 个 `#if DEBUG` 块，ChatView 独占 14 个。
- `TerminalTypingTests.swift:7-9` 还传 `-terminalEngine`，代码里已无对应实现（遗留参数）。

### 4.6 死代码与文档漂移【事实】
- `ChatStore.screen*` / `livePreview` / `spinnerLine` 无读者（§2.3）；`ServerEvent.hookEvent` 解码后 `break`（`AppModel.swift:550-551`）。
- hub 广播 `assistant_schedule`（`server/internal/hub/assistant_schedule.go:688`），App 不解码，`docs/protocol.md` 没列。
- `docs/protocol.md:156` 的 `hello` 字段漏了 `features`（hub 实际发，`server/internal/hub/http.go:616`）；表里的 `session`、`ping` 事件在 hub 非测试代码里找不到发送点。
- `docs/ios.md:85` 仍写「连不上 hub 横幅 5 秒」，与 `docs/ios.md:273-276`（横幅已删、1.5s 去抖）矛盾；`docs/ios.md:186`「实时状态条 spinner 行」对应的是上面的死状态；
  `docs/ios.md:90-101` 还在描述 `frozenAt`、「先按底部边缘跳再按哨兵对齐」等倒置列表之前的实现；`docs/architecture.md:49-50`「App 里显示为实时屏幕条」同样过时。
- `docs/ios.md:33`「收件箱列表 90 秒轮询兜底」在代码里没找到（`InboxStore` 只在事件 / 重连 / 下拉时刷新）；飞书会话 60s 兜底轮询存在（`LarkChatView.swift:132-135`）。

### 4.7 隐私
- 【事实】`InteractionViews.swift:768-811` 的 DEBUG 截图样例屏幕里有一段看起来像真实工作内容的文字（工具名、单号一类），不在 Release 里，但违反 CLAUDE.md「示例一律用占位符」的约定。【判断】应替换成占位文本。`scripts/privacy_check.py` 虽然扫全部 `git ls-files`（168-180 行），但靠模式识别（配置真实值、长 ID、真实域名、邮箱、6 位以上数字，69-79 行），这类自然语言样本抓不到，需要人工复核或加白名单式检查。

### 4.8 CLAUDE.md「经验」分类（哪些是架构不清导致的）

**A. 架构 / 分层不清导致（重设计应从结构上消除）**
| 经验（摘要） | 根因 |
| --- | --- |
| 多个 agent 并行：新增类型要带领域前缀，否则 master 撞名编译失败；rebase 用旧副本覆盖半个仓库 | 单 target 单命名空间 + 超大热点文件 |
| NavigationStack 根视图 `.task` 重跑覆盖刚选的值；视图式 / 值式 NavigationLink 混用新页面插到下面 | 导航与状态无集中路由，逻辑挂在视图生命周期上 |
| 环境值要挂在 NavigationStack 本身上 | 依赖靠环境闭包隐式注入 |
| 对话行 body 反复求值 → RowMemo/TextMemo 预热；滚动簿记放不被观察对象；派生行按 version 缓存 | 派生数据在 View 里算，没有 presentation 层 |
| Release type-check 超时（长修饰符链） | 巨型 View |
| 机器页面计数误算助手会话 | 选择器 / 业务规则散在各页面 |
| 接管要等视图第一次布局才订阅（build 21 永远转圈） | 协议状态机（TerminalFeed）与视图布局耦合 |
| `ios/Shared` 纯数据 enum 要标 nonisolated | 共享模块沿用 App 的默认 MainActor |
| xcodebuild test 启动 App 弹通知授权 | 单测宿主是 App，逻辑未抽成可独立测试的库 |
| DEBUG 启动参数不能和 Swift 标识符同名 | 用大量启动参数做测试驱动（因为逻辑难单测） |
| `has_more` 只有末尾页可信；冻结不能按行 id 截；新会话首推丢失要靠末页发现 | 同步语义复杂且只在 ChatStore + View 里体现，需要独立的、可跨端复用的规格 + 夹具 |

**B. 平台 / 框架怪癖（Android 不会原样遇到，但同类问题要预期）**：倒置列表 / LazyVStack 白屏与漂移、`scrollTo` 连带外层、`ScrollPosition.scrollTo(y:)` 归零 x、TapGesture 吃长按、Section header 不能 contextMenu、
`onScrollPhaseChange` 的 animating、MarkdownUI 主题顺序、`.searchable` 强制导航栏、隐藏导航栏关掉右滑返回、`TextField` 选区越界、UITextInput `hasText`、DragGesture 收不到快速双击、
`UNUserNotificationCenterDelegate` async 版本崩溃、WatchConnectivity 激活前推送丢弃、`URLSessionWebSocketTask.sendPing` 回调条件、ActivityKit 的 Date 解码、Swift Charts 断线、12 小时制、`dict[k, default:]` 不回写、`PrimitiveButtonStyle` 爆栈。
其中**跨平台仍要遵守的设计原则**：只往最新端精确跳、看历史时冻结新内容、长列表每步一行懒加载、派生数据按 id 缓存且后台预热、时间一律 Unix 秒、取消 / 超时 / 离线不弹错。

**C. 服务端 / 协议 / 测试基础设施**（客户端重设计只需继承结论）：herdr 交互、transcript 解析（queued_command、thinking 叙述、图片两条路）、node 读盘开销、APNs 长连、hub 内存状态、模拟器 / XCUITest 环境问题。

---

## 5. 平台无关业务逻辑清单（Android 必须行为一致的部分）

> 行数是按类型 / 函数的行号区间估的（含注释），复杂度是我的判断（低 / 中 / 高）。「单测」列只看 `ios/JarvisTests`。
> 前提事实：transcript JSONL → items 的解析、屏幕对话框解析（权威版）、推送策略、AI 分拣都在 Go 服务端（`server/internal/transcript/`、`server/internal/screen/`、`server/internal/hub/`），**客户端从 items 开始**。

### A. 同步与数据核心

| # | 逻辑 | 位置 | 行数 | 复杂度 | 单测 | 一致性要点 |
| --- | --- | --- | --- | --- | --- | --- |
| A1 | 线上类型与宽松解码（未知枚举回退、Go 零时间、纳秒时间截断、显式 CodingKeys、`state` 缺失时由 `status` 推） | `Shared/Models.swift`、`DevModels.swift`、`InboxModels.swift:5-123`、`FleetState.swift:1-161`、`MachineMetrics.swift`、`Knowledge.swift`、`JSONValue.swift:83-127`、`Views/Inbox/Lark/LarkModels.swift`、`Views/Feishu/Docs/FeishuDocsModels.swift`（类型部分） | ≈2,500 | 低 | 有（SyncEvent / FleetState / NotifyPrefs / Inbox / MachineMetrics / LarkRender / FeishuDocs / `goDates`） | 规则点：`Models.swift:3-4, 78-88, 190, 294-310`；`JSONValue.swift:112-121`；`APIClient.swift:743-746` |
| A2 | WS 信封解码 `ServerEvent` / `TerminalEvent` | `Shared/LiveConnection.swift:4-133` | ≈130 | 低 | 部分（`SyncEventTests`） | 事件表应与 hub 对齐（§4.6 漂移） |
| A3 | items 连续性：`covered / history / Gap`、末页替换、`hasMore` 只信末页、补洞（1.5s 后 `after_seq` 最多 8 页再 reload）、`since_seq` 重订 | `ChatStore.swift:248-264, 316-395, 500-524`；`AppModel.swift:382-452, 456-474` | ≈190 | **高** | 有，约 12 个（`ChatStoreTests.swift:218-303, 379-404`）+ Go 仿真 `continuity_corpus_test.go` | 与 `docs/archive/lanes/msg-render-plan.md` 诊断的链路相同；建议把 Go 仿真的场景导出成 JSON 夹具三端共用 |
| A4 | items → Rows（工具调用配结果 / 图片、思考归并）→ Blocks（Turn：排队消息并入、中断收尾、compaction 不切、唤醒同一轮、recap 后重开）→ `pickAnswerAcrossWakes`；增量重建 `SettledRows` | `ChatStore.swift:1-246, 397-447, 490-498, 530-582` | ≈360 | **高** | 好：`ChatStoreTests` 8+ 个 + `RebuildTests`（40 种子性质测试、可选真实语料） | 增量 = 全量是不变量，Android 照搬同一性质测试 |
| A5 | Blocks → 聊天「行」：按消息切段、多消息轮合并气泡、工作行（`WorkItem`/`WorkSummary`/计时）、唤醒抽屉、展开步骤、`Frozen` 冻结快照（按 `(seq, part)` 截） | `Views/Chat/ChatView.swift:42-47, 238-329, 425-436, 490-531, 593-608, 2133-2207, 2321-2350, 2426-2432` | ≈285 | **高** | **基本没有**（`chatLines` 为 private；只测了 id 形状和 `prefixCount`） | 行 id 规则（`w-`/`wt-`/`-h`/`-end`/`wk-`/`img-`/`mu-`）决定展开状态和滚动锚点，两端必须一致 |
| A6 | 磁盘缓存裁剪（最后两轮、不切半行、超 6 MB 去图） | `ChatStore.swift:786-837` | ≈50 | 低 | 有（2） | |
| A7 | 发送：`PendingMessage` 状态、Outbox 顺序 flush、`client_msg_id` 幂等、409 `input_draft`/`dialog_open`/`delivered: unknown` 分支、transcript 回显匹配（规范化 + 包含规则） | `Outbox.swift`、`SendSafety.swift:7-48`、`ChatStore.swift:839-870`、`AppModel.swift:605-671` | ≈250 | 中-高 | 部分（匹配 2、编码 1）；flush / 状态迁移只有 UI 测试 | 统一所有发送入口（§2.5） |
| A8 | 订阅引用计数、可见会话栈（抑制完成提示）、ChatStore LRU（8 个，有待发消息的不淘汰） | `AppModel.swift:257-310, 370-380` | ≈65 | 中 | 无 | |
| A9 | 连接状态机：心跳 / stalled / 45s 判死 / 退避、4 态标题 + 1.5s 去抖 + catch-up 计数 | `LiveConnection.swift:135-322`、`ConnectionStatus.swift` | ≈340 | 中-高 | 部分（`ConnectionStatusTests` 只测纯函数） | 平台网络 API 不同，但状态与去抖规则要一致 |
| A10 | 后台同步调度：`AgentSync`（rev 比较、24h/40 个/并发 3）、`LarkSync`（并发 2、3s 冷却、dirty 重排、重连补 20 个） | `App/AgentSync.swift`、`App/LarkSync.swift` | ≈250 | 中 | 无 | |
| A11 | 交互作答：提问多选拼接 / 其他覆盖、屏幕卡 `option`/`preKeys`/`keys` 三种回答、resolve 失败回滚（410 不回滚、`prompt_changed` 提示） | `InteractionViews.swift:385-434, 700-705`；`AppModel.swift:587-603` | ≈70 | 中 | 无（UI `SendSafetyTests`） | |

### B. 领域规则

| # | 逻辑 | 位置 | 行数 | 复杂度 | 单测 | 一致性要点 |
| --- | --- | --- | --- | --- | --- | --- |
| B1 | Agent 状态优先级、列表 `rank`（含待唤醒 / 已屏蔽）、组内 `sortDate`（工作中用 `state_since`）、空间分组、空闲折叠、同伴（同 pane 去重） | `Models.swift:135-147, 274-326`；`ChatsView.swift:3-49, 304-333`；`AppModel.swift:699-715` | ≈150 | 中 | **无** | 现有三套排序要先统一（§4.3） |
| B2 | 置顶 / 屏蔽（Agent 开关叠加在空间上，`allowed` 例外表） | `APIClient.swift:676-709`；`AppModel.swift:795-865` | ≈100 | 低-中 | 1（`agentOverridesItsMutedSpace`） | 与 hub 规则相同（注释 `APIClient.swift:685`） |
| B3 | 收件箱：triage 回退「需要你」、类型标签、AI 分身草稿 / 进行中、过滤与分组、手表只看需要你 | `InboxModels.swift:53-122`；`InboxView.swift:31-47, 95-101`；`InboxStore.swift:23-26`；`JarvisWatchApp.swift:65-70` | ≈60 | 低 | 只有解码 | |
| B4 | 审批策略与交互文案（标题、计划标题、`(Recommended)` → 推荐） | `NotificationActions.swift`、`FleetActions.swift`、`ApproveFirstIntent.swift:36-50`、`InteractionViews.swift:4-27, 125-133`、`Models.swift:410-417` | ≈150 | 低（但现状不一致） | 有（NotificationActions / FleetIntents / Intents） | 先定一张策略表 |
| B5 | 实时活动 / 灵动岛：两侧格子、最小态、文案、按设备裁剪负载 | `FleetState.swift:142-364` | ≈220 | 中 | 好（12） | Android 对应常驻通知 / Live Updates，文案规则可复用 |
| B6 | Siri 文案与选择规则 | `Intents/IntentSupport.swift:8-165` | ≈160 | 低 | 好（14） | Android App Actions / 快捷方式 |
| B7 | 机器监控告警阈值、格式化、OS 识别 | `MachineMetrics.swift:30-54, 133-198` | ≈90 | 低 | 有（5） | 视图里重复的阈值要删掉 |
| B8 | 助手日程文案（周几规则、24 小时制、即将运行合并） | `Views/Settings/AssistantProactiveViews.swift:9-126` | ≈120 | 低 | 有（4） | |
| B9 | 权限模式 / 思考强度表 | `Views/Chat/SessionModes.swift:4-45` | ≈45 | 低 | 间接 | 与 `node/modes.go` 对齐 |
| B10 | 客户端里的提示词模板（交给 Persona、飞书总结、停 /loop、快捷指令文本） | `KnowledgeView.swift:465-474`、`LarkChatView.swift:418-428`、`AppModel.swift:914-917`、`ChatView.swift:1908-1914` | ≈60 | 低 | 无 | 【判断】应下沉到 hub（一个端点），避免两端各维护一份 prompt |

### C. 文本 / 消息标签解析

| # | 逻辑 | 位置 | 行数 | 单测 |
| --- | --- | --- | --- | --- |
| C1 | `<jarvis-context>` 编解码（XML 转义） | `Views/Link/LinkSheet.swift:11-52` | ≈40 | `contextTagRoundTrips` |
| C2 | `<attached_images>` / 两种旧格式 / `[Image #N]` → 缩略图 + 显示文本；`UserRowText` | `MessageViews.swift:134-171, 734-749` | ≈55 | `bubbleHidesTheImageNote` |
| C3 | 后台任务唤醒通知摘要（中文化、失败判定） | `MessageViews.swift:484-503` | ≈20 | `wakeSummaryReadsInChinese` |
| C4 | 文件路径识别（`~` 保留、排除 URL 与模型名） | `Views/Chat/FileViewer.swift:36-64` | ≈30 | `keepsTildeAndFindsPaths` |
| C5 | 网页链接识别（`NSDataDetector`） | `MessageViews.swift:682-695` | ≈15 | 无（Android 用 Linkify / 正则，结果会有差异） |
| C6 | 路径插入输入框（UTF-16 偏移、单引号转义） | `Views/Chat/FileAttach.swift:8-55` | ≈50 | 有（6） |
| C7 | 手表纯文本化（去 Markdown / 标签） | `Shared/WatchText.swift` | ≈40 | 有 |
| C8 | 工具展示（图标 / 动词 / 主语、步骤摘要）、diff 行模型、`MarkdownHTML` 简化 | `ToolViews.swift:4-88, 139-148, 453-517`；`GitPane.swift:376-472`（`parse/split`）；`FileViewer.swift:310-380` | ≈300 | 只有 `MarkdownHTMLTests` |
| C9 | 格式化：相对时间、路径缩写、模型名、循环节奏、工作时长、统计数字 | `Theme.swift:320-360`、`Models.swift:221-250`、`ChatView.swift:2324-2350, 2426-2432`、`FleetText.short` | ≈100 | 部分 |
| C10 | `ScreenParser`：spinner / 活动行 / 对话框（含多题 tab 栏）/ 编号选项；对话框解析已是老 hub 的兜底（hub 有 Go 版 `server/internal/screen/dialog.go`） | `ChatStore.swift:585-784` | ≈200 | 有（4 + 1） |

### D. 终端（若 Android 也做终端页）

| # | 逻辑 | 位置 | 行数 | 复杂度 | 单测 |
| --- | --- | --- | --- | --- | --- |
| D1 | VT 子集解析 + 网格（宽字符两步写、擦除保留背景、OSC 8）、`rows(fromANSI:)`、滚动检测 `scrolled` | `Terminal/TerminalGrid.swift` | 419 | 高 | 有（10 + 录制帧夹具） |
| D2 | 手机宽度折行 / 悬挂缩进 / 断行规则 / 增量布局 | `Terminal/TerminalWrap.swift` | 141 | 中 | 有 |
| D3 | 帧序（epoch/seq、duplicate/gap）、流 / 轮询切换、接管状态机、回滚缓冲、退避 | `Terminal/TerminalFeed.swift` | 351 | 中-高 | 部分 |
| D4 | 键名 → VT 字节、按键批处理、修饰键状态机、补全建议、串行发送 | `Terminal/TerminalInput.swift:8-107`、`Chat/TerminalKeys.swift:1-70`、`Terminal/TerminalExtras.swift:282-320`、`SessionPanes.swift:683-696` | ≈230 | 中 | 有 |
| D5 | 框线 / 块字符几何、终端配色表 | `Terminal/BoxDrawing.swift`、`Terminal/TerminalTheme.swift` | ≈205 | 低（数据） | 无 |

### E. 飞书 / 文档的纯解析（UI 很重，但这部分可共享）

| # | 逻辑 | 位置 | 行数 | 单测 |
| --- | --- | --- | --- | --- |
| E1 | 消息位置比较（十进制字符串）、未读锚点、只上报上过屏的已读、游标分页 | `LarkChatView.swift:3-18, 243-400` | ≈190（多数在 View 里） | 只测 `LarkPosition` |
| E2 | 卡片节点 / 行内 run / 色板 / 格式 / 富文本块 / 代码高亮 / 表情回应切换 | `LarkCard.swift:6-48, 190-294, 440-474`；`LarkInline.swift:7-114, 231-305`；`LarkMessageContent.swift:217-296, 565-610`；`LarkChatView.swift:603-630` | ≈490 | 部分（`LarkRenderTests` 10） |
| E3 | 云文档：类型识别、Markdown 导出、块树、表格布局 / 尺寸、行内样式与颜色 | `FeishuDocsModels.swift:66-151, 380-715`；`FeishuDocRenderer.swift:11-154` | ≈550 | 有（12） |

### 5.1 小结【判断】
- **A–D 组合计约 5.1 千行**纯逻辑，加 E 约 1.1 千行，加线上类型约 2.5 千行。其中「高复杂度、出过很多真实 bug」的是 A3、A4、A5、A7、D1、D3——CLAUDE.md 一半的同步 / 渲染经验都落在这里。
- **已有测试资产**：A3/A4（ChatStoreTests + RebuildTests + Go 仿真）、D1/D2（录制帧夹具）、B5/B6、E3 都可以改写成「输入 JSON → 期望 JSON」的语言无关夹具，作为 Android（无论 KMP 还是 Kotlin 重写）的验收标准。
- **最大缺口**：A5（聊天行折叠）、B1（排序）、A7 的 flush 状态机、A10 没有单测；在决定 KMP 还是两端各写之前，先在 iOS 把它们抽出来补上性质测试 / 夹具，否则两端无从对齐。
- **可以不共享、平台各自实现的**：列表引擎（倒置列表、冻结 / 着陆的滚动实现）、导航、触感、推送（APNs / FCM）、实时活动（Android 常驻通知）、手表（Wear OS 是否要做另议）、手机工具（Android 数据源不同：HealthKit→Health Connect，提醒事项无直接对应）、
  WebView 池、Markdown 渲染、飞书卡片 / 文档的视图层、设计系统（5 种风格是否要复刻另议）。
- **对「KMP 共享核心 vs 两端各写」的输入**：
  1) 共享候选是纯函数 / 状态机，几乎不碰平台 API，按行数看规模不大（5k 级），但规则密度高；
  2) 规则已经有第二份实现（Go 测试仿真），说明「一份规格 + 多份实现 + 共享夹具」在本项目里是可行的既有做法；
  3) iOS 现在是 SwiftUI + `@Observable`，若改 KMP，iOS 端要把核心换成 Kotlin/Native 并做 Swift 包装，ChatStore 的后台增量重建（每个事件一次）在 K/N 上的性能**未实测**；
  4) 另一条路：把 A4 的轮次 / 取答 / 唤醒归组这类只依赖 items 的规则**下沉到 hub**（Go 已有 transcript 解析和语料测试），客户端只保留展开状态和滚动——能同时减少 iOS / Android / Go 测试三处重复。
     A5 依赖 UI 状态（展开、冻结、进行中），仍需客户端实现。

---

## 6. 重构建议【判断】

### 6.1 参考 removed，但不照搬

removed 的做法【事实】：本地 SPM 包 `Packages/removedKit/Package.swift`，按 `*Contracts`（纯类型 / 协议）→ `*Client`（传输实现，只依赖 Contracts + TransportContracts）→ `*Feature`（SwiftUI，依赖 Contracts + DesignSystem，**不依赖 Client**）分层，
另有 `SharedKit`（SharedCore / HTTPTransport / CredentialStore / ConnectionStorage）、`DesignSystem`；例如 `AgentsFeature` 依赖 `AgentContracts`、`HerdrContracts`、`TerminalUI`，不依赖 `AgentBridgeClient`，由 App 组合根注入。

建议吸收的：①纯类型与传输分开；②Feature 只依赖契约，便于用假实现测试 / 预览；③单测跑在包里，不需要宿主 App；④每个 Feature 一个 target，消除单命名空间撞名。

不照搬的：
1. removed 对接飞书网关的十几个领域，每个领域三件套（约 30 个 target）；Persona 只有一个后端（hub），**一个 Contracts target 按目录分领域**即可，整体控制在 10 个左右的 target。
2. removed 的组合根把 40 多个 journal / store 作为 `App` struct 的字段（`removed/App/removedApp.swift:30-90`），自身又成了大文件；Persona 用一个普通的 `AppContainer` 类集中构造、按 Feature 注入。
3. removed 的终端用 SwiftTerm；Persona 已论证自写 VT 子集更合适（`docs/terminal.md:58-78`），保留并独立成 `TerminalCore`——它也是最容易逐行移植到 Kotlin 的部分。
4. Persona 的契约有三个消费方（Go hub、iOS、Android），所以 Contracts 除了 Swift 类型还要产出**语言无关的 JSON 夹具 / 规格**，这是 removed 没有的需求。

### 6.2 目标模块

```
JarvisKit（本地 SPM 包；Contracts / Core / TerminalCore 不 import UIKit / SwiftUI，可在 macOS 上 swift test）
├─ JarvisContracts   线上类型、ServerEvent / 客户端消息、Resolution、FleetState、Features；纯 Codable、nonisolated
├─ JarvisCore        纯逻辑：Rows/Blocks/Turn/ChatLines/Frozen、连续性 reducer、Outbox reducer、排序/分组/同伴/收件箱选择器、
│                    审批策略表、消息标签解析、ScreenParser、Fleet / Intent 文案、格式化
├─ TerminalCore      VT 网格、折行、滚动检测、键编码、修饰键、帧序 / 接管状态机
├─ JarvisClient      HubHTTP（按领域拆）、HubRealtime（LiveConnection + ConnectionMonitor）、Persistence（StateCache / ChatCache /
│                    Outbox / Drafts / Keychain）、MediaCache、同步引擎（SessionSync / AgentSync / LarkSync）
├─ DesignSystem      Theme / AppStyle / StatusPalette / Components / Styling
├─ Features          Agents、Chat（列表引擎 / 顶栏 / Composer / 工作行）、Terminal、Assistant、Inbox（+ LarkUI / FeishuDocsUI / Mail）、
│                    Interactions、Settings、Machines
└─ PlatformiOS       Push(APNs)、LiveActivity、WatchConnectivity、PhoneTools、Intents、Haptics
App target           AppContainer（组合根）、Router（tab / 打开会话 / 深链）、DebugHooks（DEBUG 启动参数）
```

`AppModel` 拆成：`SessionDirectory`（machines / sessions / 索引 / pins / mutes / 选择器）、`ChatRepository`（ChatStore 注册表 + 订阅引用计数 + 补洞调度）、
`InteractionCenter`（待处理 + resolve + 审批策略）、`MessageSender`（唯一发送用例 + Outbox）、`TerminalStreams`、`AssistantService`、`PhoneToolHost`、
`ConnectionMonitor`、`Router`、`Feedback`（toast / 触感 / 错误展示策略）。

### 6.3 顺序（每一步都可单独合入，不改线上协议）

| 阶段 | 做什么 | 为什么先做 |
| --- | --- | --- |
| 0 补安全网 | ①把 `chatLines` 及其辅助函数**原样**搬成纯函数 `ChatLinesBuilder`（输入 blocks / 展开集合 / 进行中轮 / 冻结点），ChatView 改为调用；用 `RebuildTests` 的随机会话生成器补性质测试（展开收起不改变既有行 id、冻结前后已显示行不变）。②把 `rank / ordered / SpaceGroup.build / stays / isWaiting`、收件箱分组搬成选择器 + 单测。③从 ChatStoreTests / RebuildTests / Go 仿真导出 JSON 夹具 | A5、B1 是没有测试的高风险规则，也是 Android 对齐的前提 |
| 1 建包搬家 | 新建本地 SPM 包；`Models / DevModels / InboxModels / FleetState / MachineMetrics / Knowledge / JSONValue` → Contracts；ChatStore 的纯部分、ScreenParser、文本解析 → Core；Terminal 纯部分 → TerminalCore；单测迁进包；Watch / Widgets 改依赖包而不是编整个 `Shared/` | 类型名不变、只加 `import`，风险最低；单测从此不启动 App、不占模拟器 |
| 2 网络 / 同步 | `APIClient` 按领域拆、View 文件里的 extension 移出；View 不再直接 `model.api`（79 处），从 `LarkChatView`（逻辑最多）和 `AssistantProactiveViews`（调用点最多）开始；`AgentSync / LarkSync / InboxStore` 改构造注入，去掉 `AppModel.shared` 反向依赖并补调度单测；`LiveConnection.send` 不再静默丢 `tool_result` | 解开环形依赖，同步器才可测 |
| 3 拆 AppModel | 按 6.2 拆服务，`AppModel` 暂时作为门面转发（View 不用改），再逐个 Feature 改为直接依赖服务；同时**统一**：发送入口、深链路由、审批策略表、「活着 / 工作中」判定、排序 | §4.3 的不一致在这里一次性收口 |
| 4 拆 ChatView | 列表引擎（倒置 / 冻结 / 着陆 / 贴底）独立组件；顶栏、Composer、各 Strip、DevInfoCard 拆文件；DEBUG 探针收进 `DebugHooks`，启动参数名保持不变 | 最大的冲突热点；依赖前几步提供的纯逻辑 |
| 5 清理 | 删死状态（`ChatStore.screen*`、`hookEvent`、`-terminalEngine`），修文档漂移（§4.6），DEBUG 样例换占位文本；协议事件表加一致性测试（hub 发的类型 ⊆ 客户端认识 ∪ 明确忽略） | 收尾 |

不打断现有功能的约束：
- 磁盘缓存目录 / 文件名（`chats-v2`、`state/*.json`、`chat-images`）和 UserDefaults 键不变，否则升级后首开空白；要改就换新名并读旧格式一个版本。
- DEBUG 启动参数名不变（UI 测试、`scripts/sim.sh`、Release 的 `strings` 检查都依赖），新模块也要纳入 `release.sh` 的检查。
- 搬家阶段按文件逐个 PR，避免和其他 agent 在 `Shared/` 上大面积冲突（CLAUDE.md 的 rebase 教训）。
- 每步「先写测试固定现有行为 → 搬 → 测试仍绿」；统一不一致规则时单独提交、在提交信息里写清行为变化。

### 6.4 为 Android 准备的产物
- 一份「客户端行为规格」（连续性、折叠与行 id、排序、发送 / Outbox、审批策略、消息标签格式、连接状态），每条引用夹具文件。
- 跨端夹具：items 批次序列 → 期望 items / rows / blocks / lines；终端录制帧（已有 `ios/JarvisTests/Fixtures/terminal-*.jsonl`）→ 期望网格；消息文本 → 解析结果；FleetState → 文案。
- `scripts/mock/mock_hub.py` 是平台无关的假 hub（HTTP + WS），可直接用于 Android UI 测试。
- hub 侧工作项：FCM / Android 设备类型（现在只有 APNs，`server/internal/hub/push.go:28-63`）；`register_phone` 的工具集按平台声明已经天然支持；若选择「轮次归组下沉到 hub」，需要新增可选字段并保持老客户端兼容。

---

## 7. 未查清 / 待确认

- **性能均未实测**：ChatStore init 同步读盘解码的耗时、RowMemo 20,000 条上限的内存占用、K/N 下增量重建耗时、`AttachedImageCache` 无上限的实际增长。
- `docs/ios.md:33` 的「收件箱列表 90 秒轮询兜底」：补查了 `ios/Jarvis/App` 和 `ios/Jarvis/Views/Inbox` 的全部 `Task.sleep` 与 `90` 字面量，没有这样的定时器（飞书会话 60s 兜底在 `LarkChatView.swift:132-135`）；判定为文档漂移，除非它在 hub 侧。
- `docs/protocol.md` 的 `session` / `ping` 事件：只搜了 `server/internal/hub/*.go` 里的 `"type": "…"` 字面量，node 侧和其他构造方式没查。
- 隐私扫描：只读了 `scripts/privacy_check.py` 的文件选择和模式部分（168-180、69-79 行），没有实际运行；结论见 §4.7。
- 飞书消息 / 卡片 / 云文档、邮件、设置各页的视图逻辑只看了结构，没有逐行审；E 组行数按类型区间估算。
- 两处缓存覆盖竞态（AgentSync ↔ ChatStore、LarkSync ↔ LarkChatView）是推断，没有构造复现。
- removed 只看了 `Package.swift` 和组合根开头，用于对照分层（深入评估由并行调研负责）。
- `docs/archive/lanes/msg-render-plan.md` 没读（协调者已读）；本文 §2.3、§5 的 A3 / A5 与它诊断的是同一条链路。
