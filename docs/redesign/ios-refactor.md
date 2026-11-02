# iOS 重构步骤

> 草案 v1（2026-10-30），属于 [重设计方案](README.md)。依据见 [research/jarvis-ios-arch.md](research/jarvis-ios-arch.md)。
>
> **这一轮暂不执行**（2026-10-30 定：这一轮只做 Android 和 hub，iOS 只读不改）。本文留作以后 iOS 跟进时的参考。到那时，R0 不必再从 iOS 导出夹具：直接接上这一轮在 Android 侧写好的 `spec/fixtures`。

## 1. 目标结构

```
ios/
├─ project.yml                 App / Watch / Widgets / 测试 target（XcodeGen，保留）
├─ JarvisKit/                  本地 SPM 包（新）
│  ├─ Package.swift
│  ├─ Sources/
│  │  ├─ JarvisContracts/      线上类型、ServerEvent / 客户端消息、Resolution、FleetState、MachineMetrics 模型、能力位、错误码、窄协议
│  │  ├─ JarvisCore/           纯逻辑（见第 3 节 R0 / R1 的清单）
│  │  ├─ TerminalCore/         TerminalGrid、TerminalWrap、键编码、修饰键、帧序、接管状态机
│  │  ├─ HubClient/            按领域拆的 HTTP 客户端、Realtime（LiveConnection）、ConnectionMonitor、错误分类
│  │  ├─ JarvisData/           安全存储、通用操作日志（outbox / op / 草稿）、StateCache、ChatCache、媒体缓存、同步引擎、HubScope
│  │  ├─ DesignSystem/         Generated/Tokens.swift、Theme 适配、Styling、Components
│  │  ├─ ChatFeature/  AgentsFeature/  AssistantFeature/  InboxFeature/
│  │  ├─ TerminalFeature/  ReadersFeature/  MachinesFeature/  SettingsFeature/
│  │  └─ PlatformKit/          推送、实时活动桥接、WatchConnectivity、手机工具、App Intents 支撑、触感
│  └─ Tests/                   每个模块一个测试 target，`swift test` 在 macOS 上跑，不启动 App
├─ Jarvis/                     App target：AppContainer（组合根）、Router、DebugHooks、资源、Info.plist
├─ JarvisWatch/                只依赖 JarvisContracts + JarvisCore + HubClient 的一部分 + DesignSystem 的 token
├─ JarvisWidgets/ LiveActivity/ 只依赖 JarvisContracts（FleetState）+ token
├─ JarvisTests/                留给需要宿主 App 的测试（逐步迁空）
└─ JarvisUITests/              不变
```

依赖规则：

- `*Feature` 只依赖 `JarvisContracts`、`JarvisCore`、`DesignSystem`、`ReadersFeature`（阅读器被多个 Feature 复用）和必要的 `TerminalCore`。**不依赖 `HubClient` 和 `JarvisData`**，服务通过窄协议注入。
- `JarvisContracts`、`JarvisCore`、`TerminalCore` 不 import UIKit 和 SwiftUI，类型都是 `nonisolated` 的值类型或纯函数。
- 包里的 target 显式设默认隔离（Feature 是 MainActor，Core 是 nonisolated），不再依赖「整个 App 默认 MainActor、纯数据类型手动标 nonisolated」这条经验。

## 2. 约束（每一步都要满足）

1. **每步单独可合入**，不改线上协议；需要 hub 配合的改动走能力位，新 App 遇到旧 hub 时退回旧行为。
2. **不改缓存目录名**（`chats-v2`、`state/*.json`、`chat-images`、`lark-media`）、**UserDefaults 键**、**DEBUG 启动参数名**。要改就换新名字，并读旧格式一个版本。新模块里的启动参数也要加进 `release.sh` 的 `strings` 检查。
3. **先固定行为再搬**：每步先写测试锁住现有行为，搬完测试仍然全绿。统一不一致的规则要单独提交，并在提交信息里写清行为变化。
4. **避开并行冲突**：
   - 搬家阶段按文件逐个 PR，每个 PR 只碰自己的文件；
   - `AppModel.swift` 和 `ChatView.swift` 在 R3、R4 期间同一时间只有一个车道动；
   - push 前看 `git diff origin/master --stat`。
5. **每个可见的改动**都在模拟器上截深浅色图（`docs/testing.md`）。纯搬家的步骤至少跑一遍 UI 测试和 `-openSession` 截图对比。

## 3. 分阶段

### R0 补安全网（和 Android 的规格阶段一起做）

| 做什么 | 现在在哪 | 搬到 | 补什么测试 |
| --- | --- | --- | --- |
| 聊天行折叠：`chatLines`、`segments`、`workItems`、`photos`、`wakeLines`、`unfoldWork`、`prefixCount`、冻结快照 `Frozen`，以及 `ChatLine` / `WorkItem` / `WorkSummary` 模型 | `ChatView.swift`（private，约 285 行） | `ChatLinesBuilder`（先放 `ios/Shared`，R1 再进 `JarvisCore`） | 用 `RebuildTests` 的随机会话生成器写性质测试：展开收起不改变已有行的 id；冻结前后已显示的行不变；行 id 规则（`w-` / `wt-` / `-h` / `-end` / `wk-` / `img-` / `mu-`） |
| Agents 排序和分组：`rank`、`ordered`、`stays`（现在写了两遍）、`SpaceGroup.build`、`isWaiting`、同伴 `siblings` | `ChatsView.swift`、`AppModel.swift` | `AgentListSelectors` | 每种状态组合的顺序；空闲折叠；屏蔽叠加 |
| 收件箱的过滤和分组、「等你的 Agent」混排 | `InboxView.swift` | `InboxSelectors` | 分段、筛选、手表只看「需要你」 |
| 提问作答规则（多选拼接、「其他」覆盖、单题单选即提交）、屏幕卡三种回答 | `InteractionViews.swift` | `InteractionAnswers` | 全部分支 |
| 导出夹具 | `ChatStoreTests`、`RebuildTests`、`TerminalWrapTests` 的录制帧、Go 的 `continuity_corpus_test.go` | `spec/fixtures/{continuity,rows,lines,ranking,inbox,answers,terminal}/` | 测试改成读夹具：一条用例一个 JSON，`-update` 刷新期望值 |

R0 完成后，Android 拿 `spec/fixtures` 当验收标准。

### R1 建包搬家

- 新建 `ios/JarvisKit`，`project.yml` 加本地包依赖。
- 按依赖从底往上搬：
  1. `Models` / `DevModels` / `InboxModels` / `FleetState` / `MachineMetrics` / `Knowledge` / `JSONValue` 搬进 `JarvisContracts`，`LiveConnection` 里的事件信封也一起搬；
  2. `ChatStore` 的纯逻辑部分（Rows / Blocks / Turn / 连续性 reducer / `ScreenParser`）、R0 抽出的选择器、文本和标签解析（`jarvis-context`、`attached_images`、路径识别、手表纯文本化）、格式化（`Fmt`、`IntentText`、`FleetText`、`AssistantScheduleText`）搬进 `JarvisCore`；
  3. `TerminalGrid`、`TerminalWrap`、`TerminalKeyBytes`、`KeyBatcher`、`TerminalModifiers`、`TerminalCompletion`、`BoxDrawing` 的几何部分搬进 `TerminalCore`。
- **类型名不变**，调用处只加 `import`。
- 单测迁到包的测试 target，从此不启动 App、不占模拟器（顺带消掉「`xcodebuild test` 启动 App 弹通知授权」那条经验）。
- 手表和小组件改成依赖包，不再把整个 `Shared/` 源码编进去。

### R2 网络和同步

- `APIClient` 按领域拆成小客户端：会话、交互、机器、文件和 Git、收件箱、飞书、邮件、助手、知识库、设备、置顶和屏蔽。散在 9 个文件里的 extension 移进 `HubClient`。每个小客户端实现对应的窄协议（`SessionsReading`、`MessageSending`、`InteractionResolving`…）。
- View 里 79 处直接用 `model.api` / `.shared.api` 的地方改走服务。先从 `LarkChatView`（逻辑最多）和 `AssistantProactiveViews`（调用点最多）开始。
- `AgentSync`、`LarkSync`、`InboxStore` 改成构造注入，去掉 `AppModel.shared` 的反向依赖，并补调度单测：并发上限、冷却时间、重连补拉。
- `LiveConnection.send` 在没连上时不再静默丢弃：`tool_result` 走 HTTP 兜底或排队到重连后；`seen` 已经有 HTTP 兜底。
- 错误分类集中到 `HubClient`：按 `code` 判断，不再看文案（`isTimeoutText`）。新增「请求没发出去」和「发出去没回音」两种区分，这是 D3 的前提。

### R3 拆 `AppModel`

- 按 [README.md](README.md) 4.2 节拆出 11 个服务，放进 `AppContainer`，按作用域放进 `HubScope`。
- `AppModel` 先当门面转发，View 暂时不用改；之后逐个 Feature 改成直接依赖服务。
- **在这一阶段统一**，每条单独提交：

| 规则 | 现在 | 统一成 |
| --- | --- | --- |
| 状态排序 | `AgentState.priority`、`ChatsView.rank`、手表、`docs/agent-states.md` 四处不一致 | 一个 `rank`（需要你 → 完成未看 → 工作中 → 待唤醒 → 已屏蔽 → 已看），列表、手表、快捷入口都用它；修正 `docs/agent-states.md` |
| 「活着 / 工作中」的判定 | `state.isLive`、`status.isLive`、`status != .offline`、`status == .working` 混用 | 只用 hub 统一的 `AgentState`；herdr 原始的 `status` 只给信息卡显示 |
| 计划审批模式 | 5 个入口 3 种取值 | 一张 `ApprovalPolicy` 表：App 内看过全文后可以选「自动执行」；通知、实时活动、快捷入口、手表都只能「只自动接受编辑」（Siri 和手表现在是全部放行，这是行为变化，要在提交信息里写明） |
| 深链和打开会话 | `AppModel.open`、`InboxView.open`、`OpenSessionIntent` 三份（后者打开助手会话会进错 tab） | `Router.open(_:)` 一份 |
| 发消息 | 1 条走 outbox，≥5 条直连 | `MessageSender` 一个用例，所有入口都走它 |
| 助手会话排除规则 | `MachinesView`、`MachineSpacesView`、`ChatsView` 各写一次 | 一个选择器 |
| 监控告警阈值 | `MetricAlert.of` 和两个视图里的字面量 | 只用 `MetricAlert` |

### R4 拆 `ChatView`

- 列表引擎（倒置、冻结、着陆、贴底）独立成 `ChatList`，供 Agents、助手、飞书三处共用（[ux.md](ux.md) 3.5）。
- 顶栏 `ChromeHeader`、输入栏 `Composer`、附属条、`DevInfoCard`、`TerminalCover`、`CameraPicker` 各成文件。
- DEBUG 探针（约 150 行的 `appeared`）收进 App target 的 `DebugHooks`，启动参数名不变。
- 同期拆 `SessionPanes.swift`（终端面板、附属条、`KeyStream`）、`InteractionViews.swift`（四种表单 + DEBUG 样例；样例里疑似真实工作内容的文字换成占位文本）、`AssistantProactiveViews.swift`（4 个页面各一个文件）。

### R5 新功能和清理

- 落地 [ux.md](ux.md) 的改动：状态行、待定状态、交互升级、阅读工具、草稿、飞书会话页、配对流程。两端同一份规格，按 [parity.md](parity.md) 的「重设计新增」一节逐项做。
- 设计系统：接上生成的 token，用 grep 基线守住采用率（[README.md](README.md) D7）；组件目录页 `-openCatalog YES`。
- 存储：outbox、草稿、阅读位置搬到 Application Support，换新文件名并读旧文件一次；路径前缀加 `hub_id / 配对代次`。
- 公网直连的证书钉扎（[README.md](README.md) D9）：三个 URLSession 换成同一个带钉扎校验的会话；飞书视频保留，但改成先下载到本地再播（AVPlayer 走系统自己的网络栈，接不上钉扎，2026-10-30 定）。
- 删死代码：`ChatStore.screen*` / `livePreview` / `spinnerLine`、`ServerEvent.hookEvent` 的空分支、UI 测试里遗留的 `-terminalEngine`、Info.plist 里没在用的麦克风和语音识别说明。
- 修文档漂移：
  - `docs/ios.md`：横幅 5 秒、实时状态条、倒置列表之前的实现描述、收件箱 90 秒轮询；
  - `docs/architecture.md`：「App 里显示为实时屏幕条」「没有 App 在线时 hook 立即放行」；
  - `docs/protocol.md`：补上漏写的接口和事件，删掉代码里没有的 `session` / `ping` / `pong`。
- 加一个一致性测试：hub 发出的事件类型 ⊆ 客户端认识的类型 ∪ 明确忽略的类型。

## 4. 验证

| 阶段 | 验证 |
| --- | --- |
| R0 | 新测试先在没改的代码上跑，确认能锁住现有行为；夹具在 iOS 上全绿 |
| R1 | `swift test`（包）全绿；App、手表、小组件三个 target 都能编；UI 测试全绿；`-openSession` 打开几个典型会话，截图和改前一致 |
| R2 | 同步器的调度单测；mock hub 打开断网、断档、慢上传开关跑 `ConnectionTests`、`SendSafetyTests` |
| R3 | 每条统一规则单独提交，并有对应的测试；从通知、实时活动、Siri、手表各批准一次计划，确认用的是同一种模式 |
| R4 | 折叠 / 冻结探针（`-showFoldProbe`、`-showFreezeProbe`）位移都是 0；批量冷启动打开会话，按对话区像素的标准差判断有没有白屏；Release 归档能通过类型检查 |
| R5 | 每个新功能按 parity.md 的分级走到「真机通过」 |
