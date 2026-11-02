# removed 整体架构与工程方法调研（供 Jarvis iOS 重构 + Android 新版参考）

> 调研对象：removed（只读）。对照对象：Jarvis 当前 worktree（只读）。
> 证据格式：`BH:<相对路径>:<行号>` 指 removed 仓库，`JV:<相对路径>:<行号>` 指 Jarvis 仓库。
> 标注：**【事实】** = 读代码/文档直接得到；**【判断】** = 我的推断或建议。
> 隐私：不抄录任何主机名、IP、账号、个人启动器名、内部项目名、真实消息；removed 文档里出现的这类信息一律省略。
> 性能：本文不引用任何未实测的性能数字；文中出现的 MiB / 条数都是代码或文档里写死的**上限配置**，不是实测值。

---

## 0. 规模与总体形态【事实】

- Swift 源码约 9.0 万行（`Packages/removedKit/Sources`），测试约 6.6 万行（173 个文件），App 壳 1,235 行（2 个文件），主机组件（Node）源码 5.5K 行 + 测试 7.1K 行。合计与"约 17 万行"吻合（源码+测试）。
- 一个本地 Swift Package `removedKit`：39 个 target + 1 个测试 target（`BH:Packages/removedKit/Package.swift:43-83`）；26 个 library product（同文件 7-34）。
- Xcode 工程只有 1 个 App target 和 1 个 unit-test bundle，**没有 UI Test target**（`removed.xcodeproj/project.pbxproj` 的 productType 统计）。界面验收靠模拟器 + 合成 fixture 服务 + DEBUG 启动参数。
- 运行形态：手机直连两类后端——lark-connector（HTTP + WebSocket，上游不可改）和 Herdr 主机（SSH exec 一请求一进程的 NDJSON bridge + SSH PTY）（`BH:docs/ARCHITECTURE.md:11-21`，`BH:docs/ADR-001-SSH-EXEC.md:5-11`）。**没有任何常开中转服务**，这是理解它所有"重型可靠性设计"的前提。

---

## 1. 模块分层与依赖方向

### 1.1 实际的分层（以 Package.swift 为准）【事实】

| 层 | 例子（target） | 依赖规则 |
| --- | --- | --- |
| Kit / 基础设施 | `SharedCore`、`ConnectionStorage`、`CredentialStore`、`HTTPTransport`、`MediaPlayback`、`MarkdownContent` | 只依赖 Foundation / 系统库；`SharedCore` 不放任何业务实体（`BH:docs/ARCHITECTURE.md:107`） |
| Contracts | `LarkMessagingContracts`、`LarkPeopleContracts`、`LarkTransportContracts`、`HerdrContracts`、`AgentContracts`… | 纯值模型 + 窄协议 + 错误枚举；不依赖 SwiftUI、存储、具体传输库（`BH:docs/ARCHITECTURE.md:45`）。Contracts 之间可以互相依赖（如 Messaging→People/Resource） |
| Client（实现） | `LarkMessagingClient`、`LarkPeopleClient`、`LarkAPITransport`、`SSHTransport`、`AgentBridgeClient` | 依赖 Contracts + 传输；负责 DTO→领域模型、上游兼容（`BH:docs/ARCHITECTURE.md:25,224`） |
| Feature | `MessagingFeature`、`PeopleFeature`、`AgentsFeature`、`ConnectionsFeature`、`SearchFeature`、`ApprovalFeature`、`WorkplaceFeature`、`TerminalUI`、`SSHKeyFeature` | **只依赖 Contracts + DesignSystem + 少量可复用 Feature，从不依赖任何 Client target**（核对 `Package.swift:46-82` 每个 Feature 的 dependencies 均无 `*Client`/`SSHTransport`） |
| UI 公共件 | `DesignSystem`（颜色/字体/状态视图，不发网络）、`LarkResourceUI`（头像/资源视图） | |
| App（组合根） | `App/removedApp.swift` | 唯一同时 import Client 与 Feature 的地方，负责创建真实实现并注入（`BH:App/removedApp.swift:1-25,281-311`） |

- 依赖方向：App → Feature → Contracts ← Client；Client/Feature → Kit。编译期由 SwiftPM target 依赖强制，Feature 想偷用实现类会直接编不过。【事实】
- Feature→Feature 依赖存在但无环、且只为复用 UI 能力：`MessagingFeature → PeopleFeature, SearchFeature`、`ApprovalFeature/SearchFeature → PeopleFeature`、`AgentsFeature → SSHKeyFeature, TerminalUI`（`Package.swift:46,61,78,81,82`）。【事实】

### 1.2 为什么这样分（文档原话要点）【事实】

- 业务模块族（lark-connectorKit / HerdrKit / AgentKit）是**组织边界**，"不是承载所有功能的大 Client 类"（`BH:docs/ARCHITECTURE.md:25`）；"不能只把所有请求拆进几个文件，却继续共用一个全局业务 Store 或数据库模型"（同 71）。
- 每个领域拥有自己的同步规则、分页游标、缓存和失败恢复；切换账号/连接时统一取消旧作用域任务（同 107）。
- "各 Feature 只注入所需的小接口，不依赖一个暴露全部业务方法的 lark-connectorClient"（同 121）。
- 运行时（Herdr）与 Agent（Claude/Codex）两种变化分开：Herdr API 升级改运行时适配，Agent 记录格式变化改各自 adapter；**记录解析与 hooks 适配集中在主机端，iOS 只消费归一化协议**（同 125-140）。
- 页面不能到处写 `if agent == claude`，而是按能力声明决定是否显示审批/提问/停止（同 138）。
- 先用一个本地 package 承载 target，等需要独立发布/编译隔离时再拆 package；"不为每个目录机械建立三个 target"、不提前建未来模块的空实现（同 47,111,119）。

### 1.3 Feature 之间怎么通信、怎么注入、怎么导航【事实】

- **注入方式 = 构造函数注入协议存在类型**：模型拿 `any LarkPeople`、`any LarkDoNotDisturbJournaling`、`isValid: @MainActor () -> Bool`、`onError: (Error) -> Void`（`BH:Packages/removedKit/Sources/Features/PeopleFeature/LarkDoNotDisturbModel.swift:71-76`）。没有 DI 容器、没有 TCA。
- **可选能力用协议向下转型探测**：`people as? any LarkDoNotDisturbReading / Managing`（同文件 96,143）；App 里 `workspace.people as? any LarkTranslationSettingsManaging`（`BH:App/removedApp.swift:427`）。一个实现类实现很多窄协议：`SSHAgentBridgeClient` 同时符合约 20 个窄协议（`BH:.../AgentKit/BridgeClient/SSHAgentBridgeClient.swift:5`）。
- **跨 Feature 通信**：(a) 有类型的结果/资源引用，如搜索结果 `LarkSearchSelection.person/chat/message(feedID,messageID,position)`（`BH:.../lark-connectorKit/Search/Contracts/Search.swift:114-116`），交给工作区解析后导航；(b) App 注入的异步闭包，如通讯录"发消息"闭包里 `openDirect → showConversation → selectedTab = .messages`（`BH:App/removedApp.swift:255-259`），工作台闭包里异步解析后再检查 `canNavigate()`（同 266-270）；(c) 跨模块流程（消息→任务等）由 App 编排，不假设跨服务事务（`BH:docs/ARCHITECTURE.md:113-117`）。
- **导航**：顶层是 App 持有的 `selectedTab`（`removedApp.swift:40,598`）；各 Feature 内部用 `NavigationStack` + **值式、带类型的路由枚举**（如 `AgentFleetNavigationRoute`，`BH:.../AgentsFeature/AgentFleetNavigation.swift:44-47`），路由状态是可单测的值类型，迟到的恢复回调不能复活已弹出的页面（同 50-80）。sheet 用"包住模型身份的 Identifiable 展示结构体"，作用域身份一变就自动关闭（`removedApp.swift:221-243,506-619`）。导航持久化（上次打开的会话）独立存储，恢复时要求在当前会话列表里**唯一**匹配完整身份（`BH:.../AgentsFeature/AgentNavigationStore.swift:5-30`）。

### 1.4 文档与代码的落差【事实】

- 文档目录树里的 `App/CompositionRoot.swift`、`App/Navigation/`、`AgentKit/AgentSession` 并不存在；组合根就是 644 行的 `removedApp` struct（`BH:docs/ARCHITECTURE.md:144-198` vs 实际 `App/` 只有 2 个 swift 文件）。文档自己声明"模块描述是目标结构"（同 7）。
- `HerdrRuntime` target 只有 10 行、没有任何地方 import（`BH:.../HerdrKit/HerdrRuntime/HerdrRuntimeClient.swift`），运行时逻辑实际已移到主机 bridge。
- 工作区模型膨胀：`LarkMessagingModel.swift` 3,846 行，init 约 38 个参数，持有 ≥26 个子模型；App 持有约 30 个 journal 并逐个往里传（`BH:App/removedApp.swift:46-84,298-308`）。每个子模型工厂都重复写一遍 `generation/owner` 守卫（`LarkMessagingModel.swift:287-449`）。
- 【判断】包级边界很干净，但"一个账号作用域下的工作区"成了新的上帝对象；Jarvis 抄分层时要同时设计"作用域容器"来懒创建子模型，避免同样的膨胀。

---

## 2. 页面状态与并发模型

### 2.1 规则（文档）【事实】

- SwiftUI 页面用 Observation 模型，状态在 MainActor 更新；每个功能自己维护加载/空/错误/草稿/导航；View 不直接创建 URLSession、SSH 连接或数据库上下文（`BH:docs/ARCHITECTURE.md:206`）。
- 连接与同步由长生命周期服务持有，需要协调可变状态时用 actor；切 Tab 不销毁连接，离开页面停止该页订阅；删除连接/切目标时显式取消任务（同 208）。
- 一次操作用 async/await，持续事件用"有生命周期和缓冲上限的 AsyncSequence"；终端字节、消息事件、对话记录分开传，不能为高频输出无限排队（同 210）。

### 2.2 代码里的具体套路【事实】

- 数量：127 个 `@MainActor @Observable` 类、52 个 `public actor`（rg 统计 Sources）。journal、transport、store 一律是 actor。
- **代次守卫**：await 前捕获 `revision`/`readID`/owner，await 后用 `current(version)`、`Task.isCancelled`、`isValid()` 判断再落地（`LarkDoNotDisturbModel.swift:97-123,201`；`AgentWorkspaceModel.swift:2025-2054`；`LarkAPITransport.swift:30-40` 在 transport 层也做）。
- **模型自己持有 Task 并可取消**：`readTask/writeTask` 存属性，`suspend()/stop()` 里取消；调用方取消时用 `withTaskCancellationHandler { try await task.value } onCancel: { task.cancel() }` 把取消传下去（`LarkDoNotDisturbModel.swift:102-103,156-158,199-206`）。
- **忙时合并刷新**：正在读/写时来的刷新只置 `needsRead`，结束后 `scheduleRead()` 再跑一次（同 92,221-229）。
- **共享观察任务（引用计数订阅者）**：多个页面 `observe()` 共用一个轮询 Task，最后一个订阅者取消时才停（`BH:.../AgentsFeature/AgentWorkspaceModel.swift:1933-1973`）。
- **有界并发**：多主机刷新最多 3 个 SSH 任务并发，空出一个槽立刻补（`BH:.../AgentsFeature/AgentFleetModel.swift:74-100`）。
- **生命周期**：App 层 `scenePhase == .background` 时 `pauseUpdates()`，回前台 `resumeUpdates()`（`removedApp.swift:186-191`）；按连接配置身份用 `.task(id:)` 重建运行时（同 183-185,281-311）；多主机按连接身份 diff，配置变化才停旧建新（`AgentFleetModel.swift:52-65`）。
- **有界流**：WebSocket 事件流 `bufferingNewest(128)`，溢出当作网络错误（`BH:.../SharedKit/HTTPTransport/HTTPTransport.swift:217-229`）；实时变化流 `bufferingNewest(32)`，溢出时补发 `.connected` 让消费者整体重读（`BH:.../lark-connectorKit/Realtime/LarkRealtimeClient.swift:8-30`）。WebSocket 先启动接收循环再等首个 pong（`HTTPTransport.swift:220-246`，`BH:docs/ARCHITECTURE.md:395`）——与 Jarvis CLAUDE.md 里"sendPing 回调需要挂着 receive"的经验一致。
- **意图 + 确认 token 两步提交**：`prepareSave()` 生成带 UUID 的确认对象并记下 revision 和草稿 id；`process(confirmation)` 只在 id、revision、草稿都还是当前的时候才派发——旧确认框无法提交（`LarkDoNotDisturbModel.swift:134-181`，测试 `BH:.../Tests/removedKitTests/DoNotDisturbModelTests.swift:28-34`）。
- 视图持有的详情模型用 `@State private var model`（16 处），避免父视图重绘反复创建加载任务（`BH:docs/ARCHITECTURE.md:579`）；其余页面 `@Bindable var model`（54 处）。

---

## 3. 可靠性模式

### 3.1 结果三态：成功 / 明确未提交 / 结果未知，未知绝不自动重放【事实】

- 原则：发送状态至少区分"本地准备、提交中、已确认、失败、结果待确认"；超时≠失败；没有服务端幂等保证时先校准历史，禁止盲目重发（`BH:docs/ARCHITECTURE.md:259`；`BH:README.md:17,37`）。
- 客户端在协议层统一落实：bridge 客户端对一组**有副作用的方法名**，只要传输层出错就抛 `resultUnknown`；只读方法照常抛原错误（`BH:.../AgentKit/BridgeClient/SSHAgentBridgeClient.swift:350-360`）。Lark 侧同样：写请求任何传输错误 → `LarkDoNotDisturbSubmissionError.resultUnknown`（`BH:.../People/Client/LarkDoNotDisturbClient.swift:30-32`）。
- 只有"可证明发生在派发之前"的类型化错误才允许把 journal 记录当作"未提交"丢弃（`LarkDoNotDisturbJournal.swift:147-153`；模型侧 `isDefinitePreflight`，`LarkDoNotDisturbModel.swift:231`）。
- 回执合法但事后账号核对失败 → `identityUnverified(receipt:)`，保留回执、锁住写入、要求只读核对（`DoNotDisturb.swift:107-118`，`LarkDoNotDisturbClient.swift:39-43`）。

### 3.2 手机侧写前日志（journal / outbox）【事实】

- 顺序：先把 pending（操作 ID、目标完整身份、草稿版本）**持久化**，再派发；拿到回执先落盘（acknowledged），再经独立读确认后结束；重启后只按原 ID 只读核对，**没有重放 API**（`LarkDoNotDisturbJournal.swift:4-32,48-51`："There is deliberately no retry/dispatch API"）。
- Agent 侧：`AgentLocalStateStore`——"One actor per app process. Pending operations are persisted before SSH dispatch. An unreadable journal must never be replaced with an empty one automatically."（`BH:.../AgentsFeature/AgentLocalStateStore.swift:106-108`）；同一会话同时只能有一个未决操作、新建会话全局互斥（同 278-294）；结束时只在草稿文字和版本都匹配时才清草稿，人工处置留最近 50 条历史（同 300-318）。
- 草稿用 UUID 版本 + 单调写序号（`latestWrites`）防"迟到保存复活草稿 / 旧回执清空新输入"（同 267-277；`BH:docs/ARCHITECTURE.md:323,325`）。
- 删除过的操作 ID 留墓碑（retired），迟到回执不能复活已结束的提醒（`LarkDoNotDisturbJournal.swift:137-138,168-177`）。
- 消息发送队列 `LarkSendQueue` + `LarkOutgoingJournal`：phase = prepared/queued/failed/unknown + accepted + remoteID；记录不存附件字节；只按原 clientId / 精确远端 ID 核对，"不按文本、文件名或时间猜测"（`BH:.../MessagingFeature/LarkOutgoingJournal.swift:23-59,85-87`；`BH:docs/ARCHITECTURE.md:321`）。多图消息逐张上传都写检查点，最终派发前再持久化"派发边界"`postDispatchStarted`（同 475-477）。
- 规模：共 31 个手写 journal 文件、合计 4,088 行（`*Journal*.swift` 统计）。

### 3.3 主机侧持久幂等回执（`once()`）【事实】

- `BH:host/removed-bridge/src/controller.mjs:783-806`：以请求 ID 用 `wx`（排他创建）写回执文件，内容是 `method+params` 的摘要和 `completed:false`；
  - 同 ID 不同摘要 → `COMMAND_ID_REUSED`；同 ID 未完成 → `RESULT_UNKNOWN`（"不会再次执行"）；已完成 → 原样返回响应或原错误；
  - 操作执行中可写 partial 检查点；只有白名单里"可证明没执行/明确拒绝"的错误码才记为已完成的错误，其他异常保持未完成（=未知）。
- `command.status`：只读读取回执，并能凭 partial 证据（例如改名已生效、原进程已死、交互已投递）把未知结案（同 581-631）；"Never persist a successful receipt the client cannot validate"（同 754-760）。写文件用临时文件 + rename（同 777-781）。回执目录要求在结果待确认时不要删（`BH:README.md:143`）。
- 每个会话操作绑定 Herdr server / pane / 原生 session / 前台进程组，主机执行前重新核对，pane 被替换就拒绝（`BH:docs/ADR-001-SSH-EXEC.md:11`；`BH:docs/ARCHITECTURE.md:293`）；客户端对每个响应也核对会话身份 `hasSameActivityIdentity`（`BH:.../AgentContracts/AgentRuntime.swift:265-270`，`SSHAgentBridgeClient.swift:158-164`）。

### 3.4 作用域与代次（generation）【事实】

- 每个连接有稳定 `ConnectionID`；编辑地址/身份时 `generation + 1`，旧代次的数据不能更新新连接页面（`BH:docs/ARCHITECTURE.md:228`；`BH:.../ConnectionsFeature/ConnectionModel.swift:67-77,86`）。
- 凭据引用 = 连接 ID + 地址 SHA-256 摘要，改地址自动隔离（`BH:.../SharedKit/SharedCore/Connections.swift:55-73`）；SSH 凭据路由按 host/port/user/跳板长度前缀编码（同 133-137）。
- Agent 本地状态作用域 = 连接 ID + 主机地址/用户名 + 已信任指纹 + Herdr server；会话键 = Agent 类型 + 原生 session ID（避免 pane 复用串线）（`BH:docs/ARCHITECTURE.md:325`；`AgentLocalStateStore.swift:139-143,207-209`）。
- 传输层：`HostCommandRunning.scopedCommands()` 把"当前已认证连接"捕获下来，重连（即使连同一主机）之后旧 scope 发出的请求一律拒绝（`BH:.../HerdrKit/HerdrContracts/HostTransport.swift:125-132`）。
- App 给每个子模型注入 `isValid` 闭包，绑定"工作区仍 ready 且当前账号仍是创建时的 owner"（`BH:App/removedApp.swift:404-423,425-437`）。

### 3.5 错误分类【事实】

- 通用连接错误 `ConnectionFailure`：network / certificate / untrustedCertificate(证书详情) / timeout / authentication / incompatibleService / server(HTTP 码) / storage，每个都有可执行的中文说明（`Connections.swift:273-290`）。URLError 归一在 transport 层完成（`HTTPTransport.swift:137-147,180-191`）。
- SSH 建连错误 `HostConnectionError`：nameResolutionFailed / connectionRefused / networkUnreachable / connectionTimedOut / authenticationFailed / hostTrustRequired / forwardingFailed / hopFailed(跳板|目标) / terminalStartupTimedOut / timedOut(请求)…（`HostTransport.swift:22-49`）。只在**建连阶段**分类；Happy Eyeballs 下优先看实际连接尝试，不因另一地址族 DNS 失败误报；**不匹配服务端任意字符串、不把底层原文展示给用户**（`BH:.../SSHTransport/SSHConnectionFailure.swift:11-53`；`BH:docs/ARCHITECTURE.md:783-787`）。
- **建连超时 ≠ 请求超时**：后者可能已执行写，继续走未知结果核对（同 787；`HostTransport.swift:38`）。
- 业务错误：`LarkServiceError`（ownerRequired / sessionExpired / invalidResponse / untrustedResource / credentialsUnavailable / server(status,code,message)），由服务端错误码映射，不靠文案（`BH:.../Transport/Contracts/LarkRequesting.swift:82-95`；`LarkAPITransport.swift:46-52`）。Agent：`incompatible / invalidResponse / resultUnknown / rejected(code,message)`（`AgentRuntime.swift:300-311`）。
- 状态不是一个 isConnected：传输状态、认证、服务就绪、业务同步分开（`BH:docs/ARCHITECTURE.md:235-237`）；主机工作区 `HostWorkspacePhase = needsKey / connecting / needsTrust(identity) / needsComponent / terminalOnly / ready / failed`（`AgentWorkspaceModel.swift:9`）；连接进度 `HostConnectionStage` "只来自真实传输/bridge 工作，从不用耗时估算"（`BH:.../HerdrContracts/HostConnectionStage.swift:3-6`）。

### 3.6 能力协商与新旧兼容【事实】

- hello 返回协议主版本、bridge/Herdr/Claude 版本和能力；主版本不符 → `incompatible`（"请更新 bridge"），个别能力缺失只关闭相关功能，不伪装成断网（`BH:docs/ARCHITECTURE.md:297-309`；`SSHAgentBridgeClient.swift:78-86`；主机端 `controller.mjs:191-198`）。
- 能力分三层：主机级（hello.capabilities）、会话级（`AgentDescriptor.capabilities`，新能力都是可选 Bool，`AgentRuntime.swift:3-18`）、**客户端声明**（读请求带 `interactionKinds`、`childInteractions`、`terminalQuestions`… 旧客户端不会收到解不了的新卡片类型，`SSHAgentBridgeClient.swift:158-164`，`BH:docs/ARCHITECTURE.md:662`）。
- 缺能力的写操作在派发前就拒绝并提示"请更新主机组件，草稿已保留"（`SSHAgentBridgeClient.swift:192-226`）；带新字段（如拒绝原因）的操作在写 journal 之前先查能力，防止旧主机静默丢字段（`BH:docs/ARCHITECTURE.md:813`）。
- 本地存储格式版本化：新版能读所有旧版，旧 App 遇到更高版本拒绝覆盖（`unsupportedVersion`），例如草稿 v8 读 v1–v7、发送日志 v12 读 v1–v11（`BH:docs/ARCHITECTURE.md:67`；`LarkOutgoingJournal.swift:109-114`；`AgentLocalStateStore.swift:159`）。
- **宽容解码**：可选的展示增强字段用 `try?` 解码，"Optional presentation metadata must not hide otherwise readable output"（`AgentRuntime.swift:80-112`）；不支持的记录保留可见占位（`BH:docs/ARCHITECTURE.md:281`）。
- 跨语言契约：同一组固定测试向量（队列项摘要）同时在 JS 主机测试和 Swift 测试里断言（`BH:host/removed-bridge/test/prompt-queue.test.mjs:51` 与 `BH:.../Tests/removedKitTests/AgentPromptQueueTests.swift:53`）；主机端交互夹具按 CLI 版本命名（`host/removed-bridge/test/fixtures/*-2.1.3xx.json`）。

---

## 4. 本地存储方案【事实】

| 数据 | 位置 | 细节 |
| --- | --- | --- |
| lark-connector 会话 Cookie、证书信任、离线 owner 绑定 | Keychain | `KeychainSecretStore` actor，`kSecAttrAccessibleWhenUnlockedThisDeviceOnly`；引用名含连接 ID + 地址摘要；删除连接时按前缀吊销，吊销后迟到的写入直接失败（`BH:.../CredentialStore/KeychainSecretStore.swift:5-60`）。离线 owner 记录绑定凭据和证书的摘要，任一变化即失效（`LarkAPITransport.swift:151-205`） |
| SSH 私钥/口令、主机指纹（含跳板） | Keychain | `reference + ".host-key"` 等（`AgentWorkspaceModel.swift:416-429`）；私钥库 `SSHKeyLibrary`（`HerdrContracts/SSHKeyLibrary.swift:181`） |
| 连接配置 | `Application Support/removed/connections.json` | 版本化 envelope（1/2/3）、原子写、`completeUntilFirstUserAuthentication` 文件保护；只引用凭据，不含秘密（`BH:.../ConnectionStorage/JSONConnectionRepository.swift:4-27`） |
| 各类 journal、草稿、阅读位置、导航 | `Application Support/removed/*.json` | 每个一个 actor；原子写、目录 0700/文件 0600、iOS 文件保护、大小/条数上限；队列相关还 fsync 文件和目录、用 digest 检测外部修改、lstat 拒绝符号链接和他人文件（`AgentLocalStateStore.swift:197-263`；`LarkDoNotDisturbJournal.swift:77-120`） |
| 消息/媒体缓存 | `Application Support/removed/MessageCache`、`MediaCache` | 命名空间 = 连接 ID + 服务地址（`removedApp.swift:292-302`）；只在服务端 Cache-Control 明确允许时持久化（`LarkRequesting.swift:21-62`） |
| 草稿图片 | 独立目录 + manifest + SHA-256 | 临时目录写完原子重命名；启动时一次性 GC 只删可证明无引用的文件，任一引用记录不可读就整体放弃清理（`BH:docs/ARCHITECTURE.md:471,479`） |
| 主机侧 | `~/.local/state/removed/commands`（回执）、队列 SQLite（WAL、synchronous=FULL） | `BH:README.md:143`；`BH:docs/HERDR_PROMPT_QUEUE.md`「主机状态与原子边界」 |

- 没有用 SwiftData/Core Data：文档把 SwiftData 列为候选但实际全是 JSON 文件 + actor（`BH:docs/ARCHITECTURE.md:315`）。
- 删除连接是两阶段：先把"待清理凭据作用域"持久化进配置，再删 Keychain，失败下次启动重试（`ConnectionModel.swift:136-177`；`Connections.swift:204,244-259`）。
- 按连接与账号隔离：消息缓存至少按 ConnectionID、账号、会话隔离；Agent 缓存按主机连接和原生 session 隔离；换账号/删连接/退出按明确范围清理（`BH:docs/ARCHITECTURE.md:317`）。

---

## 5. 日志与恢复【事实】

- 文档设计：每个连接各自控制重连、退避、取消、校准；用户主动断开不自动重连；重连成功**不自动重发输入或审批**；网络变化只触发检查（`BH:docs/ARCHITECTURE.md:329`）。日志应含模块、连接 ID、请求 ID、阶段、耗时、错误分类，不记密码/私钥/Cookie/正文（同 331）。
- 代码现实：Sources + App 里只有一处 DEBUG-only 的 `Logger`（认证路径，`HTTPTransport.swift:127-135`）。**结构化日志基本未落地**。
- 已落地的恢复手段：实时流指数退避 1→30 秒（`LarkRealtimeClient.swift:10-28`）；重连后读权威状态而非重放事件（`BH:docs/ARCHITECTURE.md:257`）；话题重连分批复核已加载消息（每批 32、并发 4，同 393）；"核对上次操作结果"入口 + 人工处置（`BH:README.md:145-147,157`）。
- 隐私防护：含账号的值类型把 `description/debugDescription/customMirror` 重写成 `<redacted>`（`DoNotDisturb.swift:55-57,81-83`，Sources 中 11 个文件 30 处）。
- 【对照】Jarvis 在日志上反而更落地：hub 每个响应带 `X-Request-Id`、App 可传 `X-Client-Request-Id`、hub 只记失败和慢请求且不记 query/body/token、App 的 5xx 提示带请求 ID（`JV:docs/server.md:95-97`）。所以日志不是要从 removed 抄的东西；可借的只有它文档里"日志字段 = 模块、连接/作用域 ID、请求 ID、阶段、耗时、错误分类"的规范，用于客户端本地诊断日志。
- 推送（主机直连 APNs，可选）：载荷只含通用文案 + 随机订阅/事件 ID，不含主机名/项目/会话 ID/正文，点开后经已认证通道取详情并核对身份；"完成"事件发出前先做只读核验（Herdr 的简略状态可能把还在等提问的会话报成 done）（`BH:docs/HERDR_PUSH.md:5-9,72-78`）。

---

## 6. 扩展新功能的套路【事实】

- 文档表（`BH:docs/ARCHITECTURE.md:335-343`）：新业务 = 新 Feature + 领域 Contracts + 能力 Client，复用认证/传输/实时，不往 MessagingClient 里加；新 Agent = 主机 bridge 新 adapter + 协议 fixture + 必要专属 UI；多主机/多账号 = 扩配置列表与缓存选择（作用域从第一版就带）；系统通知 = 新系统适配器；换 SSH/终端库 = 替换适配器。"只有出现真实需求时才增加具体适配器"、静态组装、不搞动态插件（同 343）。
- 代码里实际的"一个能力的标准切片"（以限时勿扰为例，合计约 800 行）：
  1. Contracts：`Mutation`（可校验）、`Snapshot`、`Receipt`、`Reading/Managing` 窄协议、领域错误 + 提交错误（`.../People/Contracts/DoNotDisturb.swift:5-118`）；
  2. Client：`extension LarkPeopleClient: LarkDoNotDisturbManaging`，写前重读比对（sourceChanged）→ 核对账号 → 写 → 校验回执 → 再核对账号（`.../People/Client/LarkDoNotDisturbClient.swift:21-44`）；
  3. Journal actor（`.../PeopleFeature/LarkDoNotDisturbJournal.swift`）；
  4. `@MainActor @Observable` Model（`LarkDoNotDisturbModel.swift`）+ View；
  5. App/Workspace 注入；
  6. 测试四件套：ClientTests / JournalTests / ModelTests / WorkspaceTests；
  7. fixture 服务模块（`scripts/fixtures/do-not-disturb.mjs`）+ 上游 oracle 检查（`scripts/check-do-not-disturb-fixture.mjs`）；
  8. 文档 + VALIDATION 记录 + 对齐清单。
- 流程方法：先跑通一条纵向流程再加下一组接口（`BH:docs/ARCHITECTURE.md:224,347-354`）；先做 M0 风险验证再投入界面（`BH:docs/PHASE1_PLAN.md`「开发里程碑」）；"上游接口 → UI 调用"映射由脚本从源码生成 + 人工注释，未注释=未核查而非不需要（`BH:docs/audits/API_UI_MAP.md:1-9`，`scripts/audit-lark-connector.mjs`）；联调记录区分"真实环境 / 自动化 / 隔离模拟器"三类证据，模拟器通过不等于真机通过（`BH:docs/VALIDATION.md` 各节，`BH:README.md:157,167`）。

---

## 7. 测试结构【事实】

- 全部用 Swift Testing（`@Test`/`@Suite`，约 4,162 个 `@Test`），一个测试 target 依赖几乎所有模块（`Package.swift:83`）。
- 按后缀分类（文件数 / 行数）：`*ClientTests` 31 / 9,013（协议、DTO、上游错误码映射，用假 transport）；`*JournalTests` 12 / 1,806（持久化、版本、损坏、墓碑）；`*ModelTests` 23 / 7,073（页面模型状态机、取消、迟到回执）；`*WorkspaceTests` 19 / 4,267（模型接入工作区后的生命周期/账号切换）；`*PresentationTests` 6 / 2,164（纯展示投影）；`*QueueTests` 4 / 3,488（发送队列/定时队列）；其余约 78 个按功能命名（`AgentSessionTests` 10,216 行、`LarkMessagingTests` 6,471 行最大）。
- 可测性来自：Model 只依赖 Contracts 协议 → 测试里用 **actor 假实现**，带 hold/release 闸门（`CheckedContinuation`）确定性地制造"读到一半切账号 / 回执迟到 / 写入中取消 / 已应用但回 503"等交错，明确写着"without timers or scheduler guesses"（`BH:.../Tests/removedKitTests/DoNotDisturbModelTests.swift:241-300`）；每个 rig 用临时目录装真实 journal（同 241-249）。
- 真实协议集成测试：进程内起 NIOSSH 回环服务器，覆盖主机指纹先于认证、UTF-8、取消与超时隔离、PTY 握手暂停（`BH:.../Tests/removedKitTests/SSHIntegrationTests.swift:21-60`）。
- 主机组件：`node --test` 62 个文件（`host/removed-bridge/test`）。
- **测试接缝（补充阅读后确认）**：
  1. Contracts 协议接缝：Model 测试注入实现窄协议的 actor 假实现（`DoNotDisturbModelTests.swift:251` 的 `DNDModelAPI: LarkPeople, LarkDoNotDisturbManaging`）。
  2. 传输接缝：Client/Workspace 测试用**真实的窄 client + 真实 journal**，只把最底层的 `LarkRequesting` 换成假 transport，并用不同 `accountNamespace` 验证作用域隔离（`DoNotDisturbWorkspaceTests.swift:13-23`："Uses the real narrow client and durable journal"）。Agent 侧同理：真实 `SSHAgentBridgeClient` 之下换 `HostCommandRunning` 假主机（`AgentSessionTests.swift:623,653,1368,3002` 等 `FixtureHost`/`ReceiptFixtureHost`/`InteractionFixtureHost`），测回执、未知结果、身份核对。
  3. 真实协议接缝：进程内 NIOSSH 回环服务器（见上）。
  4. 主机端：`node --test` + 真实 SQLite + 进程 SIGKILL（`docs/HERDR_PROMPT_QUEUE.md`「验证和后续」）。
- 【判断】这个"三层接缝"是 Feature 可测的根本原因：模型不认识任何具体类，client 不认识任何网络库以外的东西，换掉最底层 transport 就能跑完整业务链路。
- 模拟器 QA：`scripts/fixtures/*.mjs` 合成服务（带 `/__fixture/...` 控制端点、请求与"被拦截的写操作"计数），`scripts/check-*-upstream.mts` 直接 import 固定版本上游的路由/领域代码做 oracle；DEBUG 启动参数注入假录音器、假 Agent 主机、一次性丢弃回执的故障注入（`BH:App/DebugAgentFixture.swift:9-40`），且只在模拟器 + 回环地址生效。

---

## 8. Jarvis 现状对照（只列与上面直接相关的事实）

- 单 App target + `Shared/`，41.8K 行 Swift；`SWIFT_DEFAULT_ACTOR_ISOLATION: MainActor`（`JV:ios/project.yml`）。全局单例 `AppModel.shared` 持有机器、会话、交互、每会话 ChatStore、草稿、outbox（`JV:ios/Jarvis/App/AppModel.swift:8-70`，998 行）；`APIClient` 一个 755 行大类（`JV:ios/Shared/APIClient.swift`）。
- outbox / 草稿 / 快照都经 `StateCache` 写在 **Caches** 目录、`try?` 吞错、detached 写、解码失败当空、键名全局不分 hub（`JV:ios/Shared/StateCache.swift:3-24`；`JV:ios/Jarvis/App/Outbox.swift:73-93`；`JV:ios/Jarvis/App/SendSafety.swift:49-62`）。另有约 139 处 `UserDefaults.standard`。
- 发送：每条待发消息带 `client_msg_id`；没网/超时（含 `networkConnectionLost`）转成 waiting，重连后**自动重发**（`JV:ios/Jarvis/App/SendSafety.swift:23-44`，`Outbox.swift:26-71`）；判断"已送达"靠 transcript 文本归一后的模糊匹配（`JV:ios/Shared/ChatStore.swift:840-875`）。
- node 去重 `sentLog` 是**进程内存、10 分钟 TTL、无参数摘要校验**（`JV:server/internal/node/sendsafe.go:126-170`），而 hub/node 常被重新部署（`JV:CLAUDE.md`「经验」：hub 内存状态随时被部署清掉）。
- 能力协商是单向的：hub 在 health / WS hello 报 `features`，App 靠字段在不在判断（`JV:docs/protocol.md:16-19`）。
- 超时判断部分靠错误文案（`APIError.isTimeoutText(api.message)`，`JV:ios/Shared/APIClient.swift:39,646-652`）。
- 已经做对的：审批/回答等一次性操作不排队（`JV:docs/ios.md:292`）；hub 只推 rev、App 按 after_seq 拉增量（`JV:CLAUDE.md` 经验）；WebSocket 心跳/弱网判断；mock hub 记录发送次数（`JV:scripts/mock/mock_hub.py:538,797-799`）。
- iOS 测试约 4.7K 行（JarvisTests + JarvisUITests），模型依赖单例，难以像 removed 那样对页面模型做交错测试。

---

## 9. A) 强烈建议 Jarvis 借鉴（全部为【判断】，证据见括号）

每条格式：是什么 → removed 在哪 → 迁到 Jarvis 解决什么 → Android 怎么对应。

**A1. 编译期强制的分层：Contracts / Client / Feature / DesignSystem / App 组合根**
- 是什么：领域模型和窄协议放纯 Contracts 模块；实现（HTTP/WS、DTO 映射）放 Client；页面模型+视图放 Feature，Feature 只依赖 Contracts；只有 App 同时看得到实现和页面并负责注入。跨 Feature 只走有类型的引用或 App 注入的闭包。
- removed：`Package.swift:43-83`；`docs/ARCHITECTURE.md:45,109-121`；`App/removedApp.swift:255-270`。
- Jarvis 解决：现在 View 和逻辑都挂在 `AppModel.shared` 单例和 755 行的 `APIClient` 上，页面模型无法脱离单例测试，也没法给 Android 一份"该有哪些接口"的清单。分层后 Contracts 本身就是 iOS/Android 共同的接口规格。
- 粒度建议：不要照搬 39 个 target，Jarvis 8–10 个模块足够（Core 契约、Hub 客户端、存储、DesignSystem、Agents、Assistant、Inbox、Terminal、Machines、Settings）。
- Android：Gradle 多模块；`:core:contracts` 用纯 Kotlin/JVM 模块（不依赖 Android，可直接 JVM 单测），`:core:hub`（Ktor/OkHttp + kotlinx.serialization）、`:feature:*` 只 `implementation(project(":core:contracts"))`，`:app` 组装（Hilt 或手写组合根）。导航用 Navigation Compose 的类型安全路由（`@Serializable` 路由类），对应 removed 的值式路由枚举——也顺手避开 Jarvis CLAUDE.md 里"视图式/值式 NavigationLink 混用"的坑。

**A2. 窄能力协议 + 运行时能力探测**
- 是什么：不是一个"全能 client"，而是 `SessionsReading`、`MessageSending`、`InteractionResolving`… 多个小协议，同一个实现类全部实现；可选能力用 `as? any XManaging` 探测，模型据此决定按钮是否出现。
- removed：`SSHAgentBridgeClient.swift:5`；`LarkDoNotDisturbModel.swift:96,143`；`removedApp.swift:427`。
- Jarvis 解决：拆掉 `APIClient` 大类；每个页面模型的 fake 只需实现 2–3 个方法，测试聚焦；"这个 hub 版本支持不支持某功能"也能落在同一机制里（实现类按 hub features 决定是否符合/返回 unsupported）。
- Android：Kotlin `interface` + 一个 `HubClient` 类实现多个接口；DI 按接口类型提供。

**A3. 写操作统一"三态 + 写前日志 + 只读核对"，但用一个通用 journal**
- 是什么：有副作用的写（发消息、新建/中断/结束会话、改模式、回答审批、发飞书/邮件回复、交给 AI）都带客户端生成的 op id；先持久化 pending（op id、目标完整身份、载荷摘要、草稿版本），再派发；回执先落盘再清；传输层错误一律 unknown；恢复只按 op id 查询；用户可"结束提醒"并留历史。
- removed：`LarkDoNotDisturbJournal.swift:4-51,147-177`；`AgentLocalStateStore.swift:106-108,278-318`；`SSHAgentBridgeClient.swift:350-360`；`docs/ARCHITECTURE.md:259,321-325`。
- Jarvis 解决：outbox 现在在 Caches（会被系统清）、解码失败当空、网络中断也自动重发、"是否已送达"靠文本模糊匹配；已核对的中断、切换模式等写操作出错时只弹错误（`JV:ios/Jarvis/App/AppModel.swift:717-739`），没有统一的 unknown 语义（收件箱回复、邮件/飞书发送、新建会话未逐一核对）。改为一个泛型 `OperationJournal<Op>`（removed 手写了 31 个、4,088 行，正是要避免的）。
- Android：Room 表 `pending_ops(id, scope, kind, target_json, payload_digest, phase, created_at)` 或 `AtomicFile` 存 `filesDir`；页面用 Flow 观察；**不要**用 WorkManager 自动重放 unknown，只对"明确未发出"的 waiting 项排重试。

**A4. 服务端持久幂等回执（removed 的 `once()`），让客户端可以安全自动重试**
- 是什么：服务端以 op id 原子建回执（含 method+params 摘要）；同 id 不同摘要拒绝；未完成返回 unknown 且不再执行；已完成原样返回；只有"可证明没执行"的错误记为已完成错误；提供只读 `status(op id)`，可凭部分进度证据结案。
- removed：`host/removed-bridge/src/controller.mjs:581-631,754-806`；`docs/ADR-001-SSH-EXEC.md:11`。
- Jarvis 解决：node 的 `sentLog` 只在内存、10 分钟、无摘要校验，重新部署后重试可能把同一句话打两遍。Jarvis 有常开 hub + Postgres，可以把回执落库（node 侧也落盘），再加 `GET /api/v1/ops/{id}`。**这是 Jarvis 比 removed 有利的地方**：removed 改不了上游，只能"未知就不重发、让人核对"；Jarvis 能把服务端幂等做实，于是客户端可以对 unknown 自动重试同一个 op id，体验更好、也更安全。
- Android：同一协议，零额外设计。

**A5. 作用域 + 代次（generation）贯穿凭据、缓存、草稿、日志和异步结果**
- 是什么：连接有稳定 ID，改地址/身份就 generation+1；凭据引用 = ID + 地址摘要；所有本地数据按"连接 → 账号 → 会话"分区；每个异步结果落地前比对 generation/owner；传输层 `scopedCommands()` 拒绝跨重连的旧请求；子模型带 `isValid` 闭包。
- removed：`Connections.swift:55-73,133-137`；`ConnectionModel.swift:67-86`；`HostTransport.swift:125-132`；`removedApp.swift:404-437`；`docs/ARCHITECTURE.md:228,317`。
- Jarvis 解决：`StateCache` 键名全局（"dev"、"outbox"、"drafts"），重新配对到另一台 hub、或以后多 hub/多设备凭据时会串数据；hub 重装后旧快照也会冒充新数据。建议 hub 生成实例 UUID（`hub_id`），App 用 `hub_id + 配对 generation` 做所有本地数据的前缀，配对变化时整体丢弃旧作用域的任务与缓存。
- Android：同样的 scope 前缀；`viewModelScope` 之外的长任务挂在"作用域对象"的 `CoroutineScope` 上，换作用域时 `cancel()`。

**A6. 统一的页面模型并发规范**
- 是什么：`@MainActor @Observable final class` 页面模型 + actor 服务；await 前捕获 revision，回来先校验；模型持有并负责取消自己的 Task；忙时刷新合并为一次补读；多个页面共享一个观察任务（引用计数）；有界并发；进后台暂停观察；有界流 + 溢出即全量重读。
- removed：`LarkDoNotDisturbModel.swift:90-123,199-229`；`AgentWorkspaceModel.swift:1933-1973,2025-2054`；`AgentFleetModel.swift:52-100`；`LarkRealtimeClient.swift:8-30`；`docs/ARCHITECTURE.md:204-210`。
- Jarvis 解决：Jarvis 已有很多零散经验（CLAUDE.md 里一堆"迟到结果覆盖新状态""返回页面 .task 重跑"的坑），缺的是写进模板的统一规范；新架构把这些固化成基类/辅助类型（如 `Revision`、`CoalescedRefresh`、`SharedObservation`）。
- Android：`ViewModel` + `StateFlow`；`Job` 字段 + `cancel()`；`Mutex`/`limitedParallelism(1)` 代替 actor；`conflate()` / `buffer(n, DROP_OLDEST)` 代替 `bufferingNewest`；`repeatOnLifecycle(STARTED)` 代替 scenePhase；revision 守卫写法一致。

**A7. "意图 + 确认 token"两步提交**
- 是什么：`prepare()` 生成带 UUID 的确认对象并记下当时的 revision/草稿 id；`process(confirmation)` 发现任一已变化就不派发。旧弹窗、旧通知按钮无法提交过期决定。
- removed：`LarkDoNotDisturbModel.swift:134-198`；测试 `DoNotDisturbModelTests.swift:28-34`。
- Jarvis 解决：审批/计划批准、"清空并发送"（`SendSafety.swift` 的 sendRefusal）、删除排队消息、通知里的快捷按钮都是二次确认场景，都可能在弹窗期间状态已变（电脑上已处理、草稿已变）。
- Android：ViewModel 里同样的 token；Compose 对话框只持有 token。

**A8. 双向能力协商 + 版本化**
- 是什么：hello 带协议主版本（不兼容就明确提示升级，而不是表现为网络错误）、组件版本与能力；能力分主机级/会话级；**客户端在请求里声明自己理解的类型**，服务端不给旧客户端发它解不了的卡片；缺能力时只关相关功能，保留草稿并提示升级。
- removed：`AgentRuntime.swift:3-18,38-47`；`SSHAgentBridgeClient.swift:78-86,158-164,192-226`；`docs/ARCHITECTURE.md:297-309,662,813`。
- Jarvis 解决：目前只有 hub→App 的 features。iOS 与 Android 两个客户端、各自多个版本并存后，hub 需要知道对端能力（新交互类型、新消息块、新工具卡）才能降级（比如对旧客户端把新交互渲染成"去终端处理"）。建议 WS hello 与 HTTP 头都带 `client_platform/client_version/client_features`。
- Android：同协议。

**A9. 宽容解码 + 可见占位**
- 是什么：可选增强字段坏了只丢这个字段（`try?`），不让整条消息/整页解码失败；未知块保留可见占位并能看原文。
- removed：`AgentRuntime.swift:80-112`；`docs/ARCHITECTURE.md:281`。
- Jarvis 解决：transcript 解析产物字段越来越多（工具结果、任务、图片），Codable 一个字段类型不符就整页报错；Android 新客户端更容易遇到"新 hub 发了新字段"。
- Android：kotlinx.serialization `Json { ignoreUnknownKeys = true; coerceInputValues = true }`，增强字段先收 `JsonElement` 再 `runCatching { decode }`。

**A10. 类型化错误 + 多维连接状态 + 真实里程碑**
- 是什么：网络层错误归一为 DNS/拒绝/不可达/超时/认证/证书/不兼容；建连超时与请求超时分开（后者可能已执行写）；业务错误只看服务端错误码，不匹配文案；状态分"传输 / 认证 / 服务就绪 / 同步"多维，每个都有可执行入口；进度只报真实阶段。
- removed：`Connections.swift:273-290`；`HTTPTransport.swift:137-147`；`HostTransport.swift:22-49`；`SSHConnectionFailure.swift:11-53`；`HostConnectionStage.swift:3-6`；`AgentWorkspaceModel.swift:9`；`docs/ARCHITECTURE.md:235-237,783-787`。
- Jarvis 解决：`isTimeoutText(api.message)` 靠文案；建议 hub 所有错误统一 `{code, message, retryable, delivered}`，客户端只认 code；ConnectionStatus 之外再加按机器（node 离线/版本不匹配/herdr 未就绪/Claude 未登录）和按数据源（飞书登录过期、邮箱授权失效）的状态。
- Android：`UnknownHostException`→DNS、`ConnectException`→拒绝、`NoRouteToHostException`→不可达、`SocketTimeoutException`→超时、`SSLHandshakeException`→证书、HTTP 401/403→认证，统一成 sealed class。

**A11. 本地持久化"安全失败"规则**
- 是什么：关键记录（journal、草稿）不可读时保留原文件、阻止新的写操作，绝不自动换成空；envelope 带版本，新版读旧版、旧版拒绝覆盖新版；原子写 + 文件保护 + 0600；只删可证明无引用的文件，遇到不可读引用就放弃 GC。
- removed：`LarkOutgoingJournal.swift:61-72,85-87,109-114`；`AgentLocalStateStore.swift:62-73,144-263`；`docs/ARCHITECTURE.md:67,323,479`。
- Jarvis 解决：`StateCache` 全部 `try?`、无版本号、在 Caches；草稿和 outbox 应移到 Application Support（缓存类快照仍可在 Caches）。
- Android：`AtomicFile` 写 `filesDir`（关键数据）/ `cacheDir`（可丢缓存）；Room 自带事务与 migration；envelope 里带 `version`。

**A12. 可测性设计：四件套测试 + 闸门式假实现 + 契约 fixture**
- 是什么：每个能力写 Client / Journal / Model / Workspace 四类测试；假实现是 actor，用 continuation 闸门 hold/release，确定性制造迟到、取消、切账号、"已应用但回 503"，不靠 sleep；真实协议的回环集成测试；跨语言共享同一测试向量。
- removed：第 7 节；`DoNotDisturbModelTests.swift:241-300`；`SSHIntegrationTests.swift:21-60`；`prompt-queue.test.mjs:51` ↔ `AgentPromptQueueTests.swift:53`。
- Jarvis 解决：iOS 测试约 4.7K 行，模型绑单例难测。建议：hub 的 Go 测试产出 golden JSON（契约 fixture，放 `testdata/contract/`），Swift 和 Kotlin 测试都解码同一批文件，作为两端对齐的硬约束；`mock_hub.py` 增加"已应用但丢回执"的故障开关（对应 removed 的 `DebugAgentReplyFault`、fixture 的 `applied503`）。
- Android：`kotlinx-coroutines-test` 的 `runTest` + `StandardTestDispatcher`，假实现里用 `CompletableDeferred` 当闸门；契约 fixture 放共享目录由 Gradle 测试读取。

**A13. 接口 → UI 的对齐清单（由脚本生成 + 人工注释）**
- 是什么：脚本从服务端源码枚举所有 API 入口，生成表格，再叠加人工维护的"客户端在哪里用、范围到哪"注释；未注释 = 未核查，不等于不需要。
- removed：`docs/audits/API_UI_MAP.md:1-9`；`scripts/audit-lark-connector.mjs`；`docs/audits/ios-api-mappings.json`。
- Jarvis 解决：复刻 Android 版最大的风险是"iOS 有、Android 漏了"。从 hub 的 Go 路由表 + WS 事件类型生成清单，每项标注 iOS / Android 的接入位置和状态，就是 Android 对齐的验收表。
- Android：同一张表的另一列。

**A14. 敏感值防泄露的 description**
- 是什么：含账号/令牌/正文的值类型把 `description/debugDescription/customMirror` 重写为 `<redacted>`，日志、断言失败输出、崩溃报告不会意外打印。
- removed：`DoNotDisturb.swift:55-57,81-83`（Sources 内 11 个文件 30 处）。
- Jarvis 解决：Jarvis 有"不提交内网/个人信息"的硬规定和隐私扫描；运行期日志也应同样防护（hub token、设备凭据、飞书/邮件正文）。
- Android：data class 重写 `toString()`。

**A15（可选）. 推送最小载荷 + 状态跃迁核验后再通知**
- 是什么：推送只带通用文案 + 随机 ID，点开后经认证通道拉详情并核对身份；"完成"类通知发出前做一次只读核验，防止把仍在等用户回答的会话报成完成。
- removed：`docs/HERDR_PUSH.md:5-9,72-78`。
- Jarvis 解决：Jarvis 推送里有会话名和回复正文（`JV:docs/ios.md:258`），锁屏可见；可做成"锁屏隐私"选项。完成判定的核验是否已有，见第 12 节待确认。
- Android：FCM data message 同样只带 ID，点开后拉详情。

---

## 10. B) removed 里过度设计 / 不适合 Jarvis 的地方（【判断】）

前提差异：removed 手机直连不可改的上游（lark-connector）和用户的主机（SSH），没有常开中转；Jarvis 手机只连自己写的 hub，hub 常开、有 Postgres、负责推送和状态存储，且 hub/node 代码同仓可改。

- **B1. 手机侧第二套持久队列与人工核对体系**：removed 的手机 prompt outbox + 主机 SQLite 持久队列（queued/claimed/executing/confirming/submitted/cancelled/rejected、lease、墓碑、用户服务监督）（`docs/HERDR_PROMPT_QUEUE.md`）是因为没有中转。Jarvis 只需"把消息可靠交给 hub"（hub 落库即 accepted），排队/投递/重试/核对放 hub+node；手机端 journal 只覆盖"交给 hub 之前"这一段。
- **B2. 每个操作一个手写 journal、一个超大工作区模型**：31 个 journal、4,088 行，App 持有约 30 个 journal，`LarkMessagingModel` 3,846 行、init 约 38 个参数（`removedApp.swift:46-84,298-308`）。Jarvis 的设置类写操作（通知偏好、置顶、模式）由 hub 存，丢回执重读即可，不需要 journal；高风险写操作用一个泛型 journal；子模型由"作用域容器"懒创建。
- **B3. 每次写前后各读一次账号 + 写前重读比对 + 上游 oracle 测试**：因为上游没有 CAS、幂等键和原子账号绑定（`docs/ARCHITECTURE.md:430,486,511,527`）。Jarvis 能改 hub：服务端直接做 CAS（version / If-Match）、幂等键、token 绑定设备，客户端省掉多次往返。
- **B4. SSH/PTY/私钥库/主机指纹/跳板机/vendor 补丁 Citadel/主机组件安装器与版本矩阵**：Jarvis 手机不碰 SSH（node 在 hub 侧经 SSH 接电脑），这些整块不需要；对应物只是配对 token 和设备凭据。
- **B5. 轮询式观察**：会话列表每 5 秒、会话页每 1 秒、多主机每 8 秒轮询 + 共享轮询任务（`AgentWorkspaceModel.swift:901-905,1933`；`AgentFleetModel.swift:67-72`），是因为 SSH exec 没有推送通道（`docs/ADR-001-SSH-EXEC.md:9`）。Jarvis 已有 WebSocket + rev + after_seq 增量，不要退回轮询；只借"前台才观察、多页面共享一个订阅"的思想。
- **B6. 每种工具一个强类型结构 + 一个专用视图**：`AgentBlock` 挂了 20 多种 `AgentRecorded*` 可选结构（`AgentRuntime.swift:49-112`），每种在 iOS 有专门视图。"解析放服务端"的原则对（Jarvis 已在 node/hub 解析 transcript），但两端各实现一遍 20 多种卡片成本翻倍；建议 hub 输出较通用的块模型（标题/键值字段/代码/差异/图片/表格），客户端用少数通用组件渲染，个别高价值卡片再特化。
- **B7. 过细的 target 与单一超大测试 target、文档承诺未落地**：39 个 target，`HerdrRuntime` 10 行未被使用，`CompositionRoot.swift` 不存在，结构化日志基本没实现（第 1.4、5 节）。Jarvis 取 8–10 个模块，测试按模块分 target；文档只写已落地的结构。
- **B8. 极端防御式校验**：逐字段 UTF-8 字节上限、控制字符过滤、手写时间戳解析、逐字节相等、SHA-256 scope key、fsync 目录、lstat 校验文件属主（`LarkDoNotDisturbClient.swift:90-116`；`AgentLocalStateStore.swift:197-263`）。面对不可信主机/上游合理；Jarvis 客户端只信任自己的 hub，校验集中在 hub 边界 + 契约测试，客户端保留大小/数量上限即可。
- **B9. 文档写法**：ARCHITECTURE.md 后半 400 多行是逐功能实现细节和每轮回归数字（如 `docs/ARCHITECTURE.md:452,462,498,517,535`），架构主线被淹没。Jarvis 现在"主题文档 + CLAUDE.md 经验"的组织更好；可借的是"真实环境 / 自动化 / 隔离模拟器"三类证据分开写。

---

## 11. 可直接用于 Jarvis 新设计的结论（【判断】）

1. 分层：`Contracts`（纯模型+窄协议，iOS 用 SPM target、Android 用纯 Kotlin 模块，两端命名一一对应）→ `HubClient`（唯一知道 HTTP/WS 的地方）→ `Store`（本地持久化）→ `Feature*`（只依赖 Contracts）→ `App`（组合根 + 作用域容器）。
2. 作用域：`HubScope(hub_id, pairing_generation)` 持有该作用域内的 client、store、子模型与所有长任务；重新配对 = 丢弃整个 scope。
3. 写操作协议：所有有副作用的请求带 `client_op_id`；hub 落库幂等回执（摘要校验、unknown 不重放、只读 status 接口）；客户端本地一个泛型 journal，只覆盖"hub 确认 accepted 之前"。
4. 能力协商：hub→客户端 features 保留，新增客户端→hub 的 `client_features`；服务端按客户端能力降级。
5. 错误：hub 统一错误体 `{code, message, retryable, delivered}`；客户端错误类型按"网络/认证/不兼容/业务/未知结果"分类，不看文案。
6. 存储：Keychain/Keystore 放 token 与设备凭据；Application Support / filesDir 放 journal、草稿（版本化、原子写、损坏即停写）；Caches / cacheDir 放快照与媒体（可丢）。
7. 并发模板：revision 守卫、模型持有 Task、合并刷新、共享订阅、有界流溢出即重读、两步确认 token。
8. 测试：契约 fixture（Go 生成，Swift/Kotlin 共同解码）+ 每个页面模型的闸门式假实现 + mock hub 故障开关 + 接口→UI 对齐清单。

---

## 12. 没查清 / 待确认

1. 没有通读 `AgentWorkspaceModel.swift`（2,539 行）、`LarkMessagingModel.swift`（3,846 行）和 `LarkSendQueue` 全部代码；发送队列状态机的细节以文档为准（`BH:docs/ARCHITECTURE.md:321-323,366-368,475-477`），代码只核对了 `LarkOutgoingJournal` 的数据结构与版本处理。
2. 没看 TerminalUI 的"有界输入队列 + 不可逆就绪/关闭状态"实现，只有文档描述（`BH:docs/ADR-001-SSH-EXEC.md:24`）。Jarvis 手机不直连 PTY，影响小。
3. 这些机制在真机/真实账号上的效果未知：removed 自己写明大量真机与真实账号场景仍待验收（`BH:README.md:3,157`；`BH:docs/VALIDATION.md`「尚待验收」）。本文只评价设计与代码结构。
4. 未构建、未运行任何测试（按要求不跑 swift test / xcodebuild）；`@Test` 数量是静态计数，参数化测试实际用例会更多。
5. 模块化对编译速度、增量构建的影响：没有实测，不给任何数字。
6. Jarvis 侧待确认：
   - 收件箱"交给 AI"/回复发送、邮件与飞书发送、新建会话等写操作的重试与幂等语义没有逐一核对；
   - hub 的完成推送 `settle`（`JV:server/internal/hub/notify.go:222-255`）在最后时刻检查了 working/seen/muted/后台任务，但是否会把"TUI 里有提问/授权菜单、但没有被 hook 拦下"的会话判成完成，需要结合会话状态来源确认（removed 在 Herdr 上遇到过：简略状态把等待提问的会话报成 done，`BH:docs/HERDR_PUSH.md:72-78`）。
7. Android 对应关系是按通用 Android 实践给出的映射（Gradle 多模块、ViewModel/StateFlow、Room/AtomicFile、Keystore、kotlinx.serialization、coroutines-test），未在任何 Android 代码上验证。
8. 其他并行调研笔记（同目录 `bh-herdr.md`、`bh-ui-qa.md`、`jarvis-ios-arch.md`）未阅读，本文结论可能与之重叠，合并时以证据更具体者为准。
