# removed Herdr / Claude Code 客户端调研（对照 Jarvis「Agents」tab）

调研日期 2026-10-30。只读阅读 removed 源码与文档、Jarvis worktree 文档与源码，没有运行任何构建或 App。
路径前缀：`removed/` = removed 仓库根；`jarvis/` = Jarvis worktree 根。行号以调研时的文件为准。
标注：**【事实】** 有出处；**【判断】** 是我的结论 / 建议。所有阈值、间隔都是代码常量或文档写明的配置值，**不是实测性能**；removed 文档自己也写明没有做帧率 / 网络时延的定量实测（`removed/docs/HERDR_PROGRESS.md:754`、`:1159`）。

> 状态：完整版（第 0–12 节）。中断恢复后按节追加写成。

---

## 0. 一句话结论

- 【事实】removed 的 Herdr 端是「手机 SSH 直连主机 → 每次请求起一个 exec 子进程跑主机组件 bridge（NDJSON，一问一答）→ bridge 操作 herdr 窗格、读 Claude transcript、收 hooks」；聊天靠 **1 秒一次的游标轮询**，终端是同一 SSH 连接上的 PTY（SwiftTerm）（`removed/docs/ADR-001-SSH-EXEC.md`；`AgentWorkspaceModel.swift:1933-1973` 的 `observe` 默认 `Task.sleep(.seconds(1))`）。
- 【判断】它最值得 Jarvis 借鉴的不是外观，而是三样东西：①「每个写操作都有持久回执、结果未知只核对不重发」的**操作日志 + 主机幂等回执**体系；②**原生交互的完整度**（拒绝原因、计划意见互斥、倒计时、MCP 表单 / 链接、子 Agent 来源、交回终端、草稿按请求保留）；③**阅读工具**（会话内搜索、跨页定位原始调用并可返回、任务进度聚合、长文本全屏分页阅读）。Jarvis 在推送、实时流、多机器、无头助手、终端（手机尺寸接管）、列表信息架构上明显更强。

---

## 1. removed 架构速览（与 UI 相关的部分）

【事实】
- 分层：SSHTransport（认证 / 指纹 / PTY / 隧道）→ HerdrRuntime（server/workspace/pane）→ AgentSession（会话身份、对话状态、输入提交、聊天与终端一致性）→ BridgeClient（版本协商、请求、快照）→ 主机端 ClaudeCodeAdapter（会话发现、记录解析、hooks、交互适配）。客户端不解析 Claude 原始日志，只消费 bridge 归一化协议；新增 Agent 主要是主机 adapter（`removed/docs/ARCHITECTURE.md:123-140`）。
- 「AgentContracts 包含公共会话标识、对话内容块、工具执行、状态、交互请求和能力声明……客户端根据能力决定是否展示审批、问题或停止按钮，不能在页面各处堆积 `if agent == claude`」（`ARCHITECTURE.md:138`）。能力在会话 descriptor 上逐项给出：`conversation/prompt/interrupt/terminal/approvals/sessionState/permissionFeedback/rename/promptFiles/promptImages/promptAttachments/compact/endSession/childTranscripts`（`removed/host/removed-bridge/src/controller.mjs:73`），hello 里另有主机级能力 `promptQueue/terminalQuestions/terminalPermissions/activityState/remoteNotifications/...`（`controller.mjs:195`）。
- 传输：SSH exec 每次一个子进程、stdin/stdout 一条 NDJSON 请求与响应，单帧上限 8 MiB（`removed/Packages/removedKit/Sources/AgentKit/BridgeClient/SSHAgentBridgeClient.swift:355`）；无监听端口、无隧道凭据（`ADR-001-SSH-EXEC.md`）。页面取消只关闭自己的 exec 子通道，不关共享 SSH（`ARCHITECTURE.md:682-687`）。
- 刷新节奏（代码常量）：会话页 1 s 轮询 `session.read`（`AgentWorkspaceModel.swift:1933`）；多主机任务中心每 8 s 一轮、最多 3 台并发且完成即补位（`AgentFleetModel.swift:67-100`）；主机推送采集 daemon 每 15 s 读一次 herdr（`removed/docs/HERDR_PUSH.md:7`）。没有事件流 / 游标续传（Cindy 对照审计里自己承认「Herdr 当前仍为 SSH 轮询，没有同类会话增量 push」，`removed/docs/HERDR_CINDY_REVIEW.md:47`）。
- 会话身份 = 六字段：`id, paneID, kind, binding(进程身份哈希), nativeSessionID, terminalID`（`removed/docs/HERDR_PROMPT_QUEUE.md:9`；`AgentNavigationStore.swift` 的 `AgentNavigationTarget`）。主机每个写操作前 `checked(binding, paneID)` 重新取进程身份比对哈希，不一致回 `STALE_SESSION`（`controller.mjs:83-89`）。

---

## 2. 会话列表 / 首页（任务中心）

【事实】
- 首页（Cindy 评审后改版，`HERDR_CINDY_REVIEW.md:63-91`，`HERDR_PROGRESS.md:1395-1403`）：顶栏「Herdr」+ 主机与连接 / 通知中心（未读数）/ 新建（`AgentFleetView.swift:123-145`）；一条紧凑「主机 x/y 已连接」连接条，有主机连接中显示转圈、有未就绪显示橙色感叹号，点开是主机面板（大卡片、连接详情、密钥库、外观都收进去）（`AgentFleetView.swift:310-352`）。
- 范围筛选 三段：活跃 / 待处理 / 全部，每段带数量；**活跃 = 工作中 ∪ 待处理 ∪ 本机近 7×24 小时主动打开过**（访问记录只在显式打开、真正回到前台阅读、前台 pane 换进程时记一次，后台轮询不续期）（`AgentFleetModel.swift:29-35`；`AgentSessionBrowser.swift` `recentVisitInterval`；`HERDR_PROGRESS.md:1052-1062`）。
- 排列 二选一：按项目（完整主机 scope + 完整路径分组，同名不同路径不合并，组可折叠、「全部展开 / 收起」，偏好持久化）/ 最近访问（`AgentFleetView.swift:428-449, 469-501`）。排序：待处理 → 工作中 → 其余，再按最近访问，平级用稳定身份字段字典序，**轮询不会让列表乱跳**（`AgentFleetModel.swift:198-206`；`AgentSessionBrowser.swift` `Candidate.precedes`）。
- 行（`AgentFleetView.swift:502-528`）：Agent 标记 / 终端图标 + **真实会话名**（没有主机给的摘要就不编造，`HERDR_CINDY_REVIEW.md:85`）+（最近排列时）主机 · 项目 + 状态标签（待处理 / 状态待核对 / 工作中 / 已完成 / 终端 / 就绪）+ **橙色「草稿 · …」摘要**。
- 「状态待核对」：Herdr 列表的粗粒度 `done` 可能在 Claude 还挂着 AskUserQuestion 时就报出来，所以对「非 done→done」的候选再调一次只读 `session.activity` 核对 transcript 里有没有未答问题；核对中 / 失败显示「状态待核对」，不猜成 blocked 也不算完成；计数、筛选、徽标、通知共用这份投影（`AgentWorkspaceModel.swift:79-148`；`controller.mjs:139-174`；`HERDR_PROGRESS.md:958-989`；`HERDR_PUSH.md:75-83`）。
- 待核对操作卡（工作区页）：「发送 / 交互 / 改名 / 压缩 / 退出 / 停止结果待确认 · 会话名」一行一个，点进原会话（`AgentsView.swift:191-202`）；会话启动 / 恢复结果不明时有「核对会话启动结果」与「已在主机核对启动结果…」人工处置页（显示原会话 ID、操作编号）（`AgentsView.swift:203-218, 345-370`）。
- 新建：先选主机（只有一台就绪时直接进）→ 全屏项目选择（按名称 / 完整目录搜索，确认按钮固定底部，取消自动退回首页、不自动创建）（`AgentFleetView.swift:371-397`；`AgentsView.swift:412-422`；README「新建会话」段）。
- 导航：任务行直接 push 进同一 NavigationStack 的会话页，返回一次回首页；工作区是可选层级（`HERDR_PROGRESS.md:1086-1094, 1395-1403`）。自定义顶栏 + UIKit 手势代理桥接的边缘右滑在真机失败，最终改回**系统导航栏 + 系统返回**（`HERDR_PROGRESS.md:1039-1077`）。

【判断】Jarvis 列表的状态模型（需要你 / 完成未看 / 工作中 / 待唤醒 / 已屏蔽 / 已看）、空间 / 槽位、置顶 / 屏蔽、三种分组比 removed 成熟得多；可借鉴的只有：①行内**草稿摘要**；②「状态待核对」这类**不确定态的显式表达**（Jarvis 现在把 hub 计算结果当确定值）；③排序**稳定性**原则（同状态内不因轮询抖动）。

---

## 3. 会话页（记录渲染）

### 3.1 页面骨架
【事实】`AgentSessionView`（`removed/Packages/removedKit/Sources/Features/AgentsFeature/AgentsView.swift:438-722`，下文 AgentsFeature 目录省略前缀）：
- 系统导航栏：标题 = 会话名，副标题 = 「主机 · 项目目录名」（`AgentsView.swift:602-607`）——**状态不放顶栏**（Cindy 评审后去掉重复状态，`HERDR_CINDY_REVIEW.md:89`）。右侧：键盘弹起时「收起键盘」、终端 / 聊天切换、`…` 菜单（搜索、子任务目录、改名、模拟器预览、网页预览、Git 审查、用量与上下文、待发消息、压缩上下文、结束会话）（`AgentsView.swift:471-496`）。
- 正文上方（有结构化任务结果时）一条「任务进度 · x/y ›」横条（`AgentsView.swift:500-509`）。
- 输入区上方**唯一一处**实时状态行（`AgentLiveActivityView`，只有它每秒重绘，正文不随时钟刷新）+ 旁边「改动」按钮（Git）；有待处理交互时状态行换成交互入口（`AgentsView.swift:538-558`）。再上面是「待发消息 · N 条待处理 ›」（启用主机队列时）。
- 输入框：多行 TextField（1…6 行，辅助字号下更少）+「＋」菜单（相机 / 照片 / 文件）+ 工作中出现的停止键（36pt 圆、44pt 触控，实心方块）+ 发送键（同尺寸）（`AgentsView.swift:570-596`；`HERDR_PROGRESS.md:1105-1111`）。**工作中仍可发送**（进入 Claude 自己的排队），停止和发送是两个独立按钮。

### 3.2 状态机（输入区上方那一行）
【事实】`AgentLiveActivity.resolve`（`AgentLiveActivity.swift`，全文 33 行）的优先级：
`ended`（会话结束或身份变化）> `unavailable`（连接失效）> `submitting`（本机正在回传）> `operationUnconfirmed`（上次操作结果待核对）> 健康检查不是 verified 或 **超过 15 秒没验证** → `checking`「正在同步主机状态」> 有待处理交互或 blocked → `awaitingInput` > working（已发停止则 `stopping`「停止已发送，等待主机确认」）/ idle / done。
- 文案与颜色见 `AgentLiveActivityView.swift`：工作中蓝色转圈「Claude 正在工作」，等待确认橙色手掌，结果待核对橙色感叹号；减少动态效果时转圈换静态图标。
- 状态行带 notice 时可点，打开「会话状态」页，集中放：会话提示、上次操作待核对（「核对上次操作结果」/「已在主机核对结果…」→ 二选一「确认已执行，结束提醒 / 确认未执行，保留输入」）、记录读取暂停（重试 / 打开同一会话终端）、进程已变化、需要在终端处理的交互、本机记录恢复失败、草稿未保存（`AgentSessionStatusView.swift`；`AgentsView.swift:668-678` 的 `statusTitle` 优先级）。
- 刻意**不**从 thinking 块、工具结果或终端转圈文字推断「正在思考」（`HERDR_PROGRESS.md:808-819`）。

【判断】这个「一处状态 + 可点详情 + 结果待核对是一级状态」的做法，比 Jarvis 现在「顶栏副标题状态 + spinner 条 + 工作行计时 + 交互面板」分散在几处更清楚；但 Jarvis 的 spinner 文本（`Cogitating… 12s · ↓3.2k`）信息量更大，两者可以合并：状态行左边是确定状态，右边附 spinner 实况。

### 3.3 正文渲染
【事实】
- 时间线行由 `AgentTimelineRow.make` 生成：只有唯一的 assistant tool_use 和唯一的后续 tool_result 才按 ID 合并成一张工具卡；重复、乱序、缺失调用的结果独立显示（`ARCHITECTURE.md:283`）。
- **处理过程折叠**（`AgentWorkPresentation.swift`，`AgentWorkSummaryView.swift`）：当前窗口内连续的 assistant 工具调用 / 思考 / 中间叙述合成一个可展开的「处理过程 · N 项操作」，最终回复留在外面；折叠时就能看见「N 项执行失败」「N 项尚无结果记录」（明确说「暂无结果不代表仍在执行」）；用户输入、主机通知、图片、孤立结果、未知格式都是分组边界。只有主机记录了 `system/turn_duration` 且严格关联到这一段最终回复时，标题才换成「用时 X 分 X 秒」，否则不用时间戳估算（`HERDR_PROGRESS.md:775-786`）。
- **展开后吸顶收起**：展开的组滚过顶部后，顶部浮出该组摘要栏，点它或「收起」折叠并回到摘要所在行；组尾把它推走（`AgentPinnedWorkSummary.swift`；`HERDR_PROGRESS.md:865-871`）。
- 头像 / 角色标签：连续 assistant 行不重复标题；系统来源的块各有标签：「主机定时触发」「主机命令记录」「上下文压缩」「监听事件」「后台任务通知」「主机提示」「工具输出」（`AgentConversationView.swift:273-281`）。空 thinking 整块隐藏（`HERDR_PROGRESS.md:821-827`）。
- Markdown：独立的 `MarkdownContent` 模块（swift-markdown AST → 值模型，后台解析），标题 / 列表 / 引用 / 表格（横滑 Grid）/ 代码块（头部「复制」按钮、横向滚动）（`removed/Packages/removedKit/Sources/SharedKit/MarkdownContent/MarkdownContentView.swift:115-139, 182-187`）。代码高亮只对 Swift / Python / JS / TS / JSON 做**有界词法扫描**（64 KiB 上限，后台可取消，超限或未知语言原样显示）（`ARCHITECTURE.md:776-780`）。被截断的 Markdown 不当 Markdown 渲染（避免半截代码块的复制按钮），改纯文本 +「内容较长，继续阅读」（`AgentConversationTextView.swift`）。
- 每条消息只有一个「复制」（复制原文，不是重建的 Markdown）；去掉了常驻「全文」按钮（`HERDR_PROGRESS.md:768-772`）。**自己的超长消息在气泡内分页**（每页 ≤8192 UTF-16 / 80 行），不跳全文页（`HERDR_PROGRESS.md:797-806`）。正文排版：系统字重、动态字号、行距 5pt、段距 16pt（同上）。
- 工具卡片（`AgentConversationView.swift:539-594`）：标题行「工具名 + 结果（失败红色）」+ 两行参数摘要；展开后按工具类型渲染：
  - Edit / Write：请求片段的**行级 diff**（UTF-8 字节为行身份，保留 CRLF / 无换行；公共前后缀之外用有上限的 LCS，超大区域整段删增并标「粗粒度」），卡片只画首个修改附近 12 行，独立全屏页 LazyVStack + 「上一处 / 下一处」修改导航，打开时冻结快照（`ARCHITECTURE.md:734-738`）；有 `structuredPatch` 时可在后台**重建整份历史文件**并复制修改前 / 后（`ARCHITECTURE.md:740-746`）。
  - Bash / Read / Glob / Grep / ToolSearch：按字段显示原样参数（命令、说明、超时、后台标志 / 路径、行范围、PDF 页码 / 模式、过滤…），不补默认值、不推断是否生效（`ARCHITECTURE.md:790-794`）。
  - 结构化结果：Bash 标准 / 错误输出、文件读取（含「此前可读结果」跨页定位）、Notebook 读 / 改、PDF 分页阅读、Glob/Grep 结果、Web 结果、MCP 资源、Cron / ScheduleWakeup、Monitor、Worktree、子 Agent 结果、后台任务停止、任务（TaskCreate/Get/List/Update/TodoWrite）（`AgentConversationView.swift:561-577`）。
  - 一律保留「原始输入 / 原始输出」折叠项；预览限 80 行 / 12000 字符，「全屏查看」与「复制完整内容」（`ARCHITECTURE.md:285`；`AgentConversationView.swift:647-701`）。
- 全屏文本阅读器 `AgentFullTextView`：打开时冻结的快照，按 8192 UTF-16 分页（不切开代理对 / 字素），首末 / 前后翻页、全文前后循环搜索（后台、可取消）、完整复制；TextKit 1 按实际字形矩形定位命中（`ARCHITECTURE.md:797-799`）。
- 图片：只解码随会话返回的内嵌 PNG/JPEG/GIF/WebP，后台缩略图，全屏缩放（`ARCHITECTURE.md:721-725`）。
- Hook 非阻塞错误单独显示为「主机 Hook 执行失败」，提示「不会阻止工具继续执行」（`AgentConversationView.swift:621-631`）。

### 3.4 长记录分页、渲染窗口、阅读位置、跳到最新
【事实】
- 主机按字节游标分页：历史页约 1 MiB、实时页约 2 MiB，单条记录上限 6 MiB；超限返回 `TRANSCRIPT_RECORD_TOO_LARGE`，客户端**只暂停那个方向**的读取、保留游标和已加载内容，引导到同一会话终端（`ARCHITECTURE.md:698-718`；`AgentWorkspaceModel.swift:2093-2110`）。文件被替换 / 截短（`reset`）时整窗重读，不把新文件的尾巴拼到旧历史前面（`AgentWorkspaceModel.swift:2043-2050`）。
- 渲染窗口：首次只构建最近 60 行，「加载更早记录」先在本地窗口再展开 60 行，本地到头才向主机要历史页；跟随最新时新内容收拢到最近窗口；**故意用 VStack + 实测行高，而不是 LazyVStack**（后者在高度差异大的混合内容下会落在输出中间甚至空视口）（`ARCHITECTURE.md:705-711`；`AgentTimeline.swift` `AgentTimelineWindow(pageSize: 60)`）。
- `AgentReadingState`：手势状态和「是否在底部」分开跟踪，内容变高不会自己关掉跟随；用户开始拖动时才按是否在底部决定 followsLatest（`AgentTimeline.swift` `AgentReadingState`）。不跟随时右下角浮钮「回到最新」，有新内容时变「有新内容」（`AgentConversationView.swift:401-425`）。
- 阅读位置持久化：按（主机 scope，原生会话）存 `followsLatest / hasNewContent / viewport(rowID + 行内偏移) / 展开的块集合 / 最新一条消息的 SHA-256`，滚动停止后 400 ms 去抖写盘（`AgentConversationView.swift:474-478`；`AgentReadingStore.swift`）。重开或冷启动时：若上次在看历史，**逐页向前读（每轮最多 20 页）找回那一行**，状态页显示「正在查找上次读到的记录，已读取 N 页」，超过 20 页给「继续查找」，到头给「没有找到」，任何时候可「查看最新」；最新一条的签名变了就标「有新内容」（`AgentWorkspaceModel.swift:1833-1898`；`AgentReadingRecoveryView.swift:22-38`；`AgentReadingStore.swift:86-88`）。
- 导航恢复：当前打开的会话（含子任务层级，最多 8 层）跨重启恢复，按六字段身份核对，进程变了就拒绝恢复并提示（`AgentNavigationStore.swift`；`HERDR_PROGRESS.md:28, 1328-1336`）。会话阅读器缓存最近 8 个（有草稿 / 在途操作 / 未保存状态的不淘汰）（`AgentWorkspaceModel.swift:171, 2492-2498`）。

### 3.5 会话内搜索与「跨页定位原始调用」
【事实】
- 搜索页（`AgentTranscriptSearch.swift`）：搜正文、思考、工具名、工具输入（JSON 里所有字符串）和配对输出、主机事件（任务通知、监听事件、定时触发、命令记录、请求错误）；不搜图片 / 附件；200 ms 去抖、后台 detached；最多 100 条命中（超出提示缩小范围）；底部「加载更早记录并搜索」；点命中 → 关闭搜索 → 展开对应块、滚到并**蓝框高亮**，顶部出现「返回搜索前位置」（`AgentConversationView.swift:186-204, 482-515`）。
- 关联定位：任务通知 / 监听事件 / 定时触发 / 「文件未变化」记录上有「查看来源」，客户端逐页向前读（≤20 页一轮）找原始调用，找到后高亮并在顶部给「返回任务通知」；找不到 / 歧义 / 超过页数各有明确提示和「继续查找 / 重试查找 / 取消定位」（`AgentConversationView.swift:150-177, 450-473`；`AgentWorkspaceModel.swift:1993-2017`）。

### 3.6 子 Agent、任务进度
【事实】
- 子 Agent：父会话的 Agent 工具卡可「打开子任务」进独立的子记录阅读页（分页、搜索、独立阅读位置、可逐层进入，最多 8 层，循环引用保护）；`…` 菜单「子任务目录」列出本会话所有 Agent 调用（倒序、可搜索、状态写「（返回记录）」「尚无结果记录」，明确不推断实时状态）（`AgentSubtaskDirectoryView.swift`；`HERDR_PROGRESS.md:26`）。子 Agent 发起的权限请求带来源卡片「请求来自子 Agent · ID · 类型」，在父会话和子页都能处理（README 0.7.40 段）。
- 任务进度聚合（`AgentTaskProgress.swift`，`AgentTaskProgressView.swift`；`ARCHITECTURE.md:760-773`）：从唯一配对、未出错的 TaskCreate/Get/List/Update/TodoWrite 结构化结果**按结果在会话中的顺序**重建任务表（完整列表替换成员、增量更新对应 ID、依赖两侧同步），顶部「任务进度 · x/y」，全屏页显示每项状态、负责人、执行说明、前置 / 后续任务、「查看相关记录」；并明确提示「根据已加载结果整理，可能与主机此刻不同」「尚无完整列表」「存在未确认更新」。

【判断】Jarvis 的展示逻辑（每条消息一段、工作行「工具 5 · 思考 1 · 回复 1 · 4 分 17 秒」、展开后每步是独立懒加载行、倒置列表）在性能与「输入—输出紧挨」上比 removed 更好，**不要换回 VStack 窗口方案**；值得补的是：会话内搜索 + 高亮 + 返回原位、跨页定位原始调用、任务进度聚合页、长文本全屏分页阅读器、折叠时可见的失败数、工具卡「原始输入 / 输出」统一入口、Edit 的 diff 导航。

---

## 4. 原生交互

### 4.1 来源与传输
【事实】
- 两条来源：
  1. **会话专属 hooks**（只给 App 新建的会话，用私有 `--settings` 文件注入；不改全局设置、不写长期权限规则）：PermissionRequest（普通工具 / MCP 工具）、PreToolUse AskUserQuestion、PreToolUse ExitPlanMode（仅 plan 模式且带完整 plan 与 planFilePath）、Elicitation（MCP 表单 / URL）（`removed/README.md:110-116`；`removed/host/removed-bridge/src/interactions/requests.mjs:16-60`；`ARCHITECTURE.md:648-664, 673-695`）。
  2. **终端适配**（给没装 hook 的既有会话）：只认两类——标准 AskUserQuestion 菜单（0.7.53）和标准 Bash 审批菜单（0.7.58）。条件是 transcript 里**恰好一个**未完成的对应 tool_use + 完整、未截断、能逐字核对的实时菜单；布局未知 / 截断 / 软折行无法无损还原 / 多个候选时不出卡片，留在终端（`README.md:124-128`；`removed/host/removed-bridge/src/interactions/terminal-permissions.mjs:18-38, 45-71`）。卡片标 `responseTransport: "terminal"`，提示「此审批来自终端……请勿同时在电脑处理这项审批」（`AgentInteractionView.swift:104-107`）。
- 能力协商：客户端在 `session.read` 里声明 `interactionKinds`、`childInteractions`、`terminalQuestions`、`terminalPermissions`；旧客户端不会收到它不认识的类型（不会因为新类型解码失败把整个会话读坏）（`controller.mjs:91-107`；`ARCHITECTURE.md:662`）。
- 有效期：权限 / 问题 90 s，计划 / MCP 表单 10 min（`requests.mjs:123`）；终端适配卡 24 h（`terminal-permissions.mjs:90`）。过期后**恢复原 CLI 交互**（hook 不输出 = 交回 TUI）。

### 4.2 呈现层级
【事实】
- **就近**：普通工具权限显示在**记录末尾**（`AgentInlineInteractionView`）：标题、「主机 · 项目路径」、子任务来源、工具名、完整参数审阅（Bash 字段 / Edit 差异 / 原始 JSON）、剩余时间倒计时、「拒绝本次操作 / 仅允许这一次」+「查看完整请求」（`AgentInteractionInboxView.swift:94-141`）。已经在完整页写了拒绝原因草稿的，内联卡不给按钮，强制回完整页，避免原因被静默丢掉（同文件 `:114-121`）。没有协议级 tool_use 关联 ID 时**不把请求伪装成挂在某个历史工具卡下**（`HERDR_CINDY_REVIEW.md:89`）。
- 计划、问题、MCP 等复杂请求：输入区上方一个橙色入口「回答问题 / 审阅计划 / N 项待处理交互 ›」，单个直接打开完整页，多个先进「待处理交互」收件箱（按收到时间排，每项有摘要和剩余时间）（`AgentInteractionInboxView.swift:24-90, 143-190`）。
- 完整页 `AgentInteractionView`（`AgentInteractionView.swift:47-335`）：
  - 顶部：类型标题、会话名、终端来源提示、子 Agent 来源卡、「等待回复 + 计时器」或「已处理 / 过期 / 会话已变化」（`:101-122`）。
  - **权限**：MCP 显示服务器名与配置来源（主机没给就明说「未提供」，不从工具名推断来源）；参数审阅；**可选拒绝原因（≤4000 字）**——「原因会随本次拒绝交给 Claude；如需允许，请先清空原因」，有原因时「仅允许这一次」禁用；主机不支持就提示升级或清空（`:157-182, 324-329`）。终端来源的 Bash 只有「拒绝本次 / 仅允许这一次」，**不映射「不再询问」「切换自动模式」**（`:318-322`；`README.md:126`）。
  - **计划**：计划文件路径 + Markdown 原文 +「批准后 Claude 将退出计划模式；后续工具权限由主机上的 Claude Code 决定」+ 可选修改意见；**有意见时只能「退回修改」，清空后才能「批准计划并继续」**（`:147-156, 312-317`）。批准只回传官方 allow + 原 updatedInput，**不附加 setMode**（`ARCHITECTURE.md:660`）。
  - **问题**：每题 header + 问题 + 选项（含说明）+「自己的答案」输入框（填了就清空选项）；单选 / 多选；「提交回答」在全部合法前禁用；hook 来源有「取消回答」，终端来源去掉（危险按键映射）（`:183-226`；`HERDR_PROGRESS.md:841-845`）。
  - **MCP Elicitation**：表单（字符串 / 数字 / 布尔 / 单选 / 多选，含旧 enumNames）或浏览器链接两种；链接模式显示服务器、请求消息、目标主机与完整 URL，只有系统确实接受了打开操作后才允许「已打开，继续」，并说明「打开链接不代表授权成功」；三种结局「提交表单 / 拒绝请求 / 取消本次」分别回传（`:123-146, 302-311`；`ARCHITECTURE.md:689-695`）。schema 校验客户端做一遍、主机再做一遍，服务器给的正则在独立 Node 进程里限时 500 ms、32 MiB 堆执行（同上）。
  - 底部操作栏固定（`safeAreaInset(.bottom)`），键盘弹起时让位；辅助字号下随正文滚动、按钮纵排；键盘工具栏「完成输入」（`:239-266`；`HERDR_PROGRESS.md:91`）。
  - 「交回同一会话的终端」：先回传 `terminal` 决定，成功后才导航到终端；有一个导航票据，关闭题卡 / 离页 / 后台 / 请求变化都会作废它，迟到成功不会把用户硬拽进终端（`AgentInteractionView.swift:11-45, 228-238`；`HERDR_PROGRESS.md:883-887`）。
  - 未提交的输入（选项、自填、原因、表单）按**完整请求快照**保存在当前连接内，关掉再开能恢复；请求被替换就丢弃（`AgentWorkspaceModel.swift:1570-1587`；`AgentInteractionView.swift:272-277`）。
- 目录信任（新开 Claude 弹「信任此目录」）：原生页显示主机、完整路径、「信任后可读写执行此目录、可能加载项目配置」，**由用户选**「信任目录并继续 / 不信任，退出 Claude / 在终端查看」；确认后等 Claude 真正可交互再自动进原生聊天（`AgentPaneView.swift:84-120`；`HERDR_PROGRESS.md:620-626`）。

### 4.3 主机侧单次决策与回执
【事实】
- 每个 hook 请求一个随机 UUID，绑定 pane、进程 binding、原生 session、**存活的 hook PID** 与截止时间；请求 / 答案只短暂存在 0700 目录的 0600 文件里，完成后删除，只留最小 delivery 回执（`ARCHITECTURE.md:652`；`requests.mjs:115-147`）。
- 回答走「独占硬链接」占用唯一决策槽：第二个回答拿到 `INTERACTION_RESOLVED`；hook 侧轮询到决定后**再核对一次会话身份与有效期**，失败写 `expired`；只有写成 `returned` 才算回传（而且注释明确「returned 只代表交给了 hook，不代表工具执行了」）（`requests.mjs:172-215`）。计划在发布和回传前都重读计划文件（拒绝软链接、非普通文件、>256 KiB、内容与展示不符），计划变了旧批准失效（`interactions/plan.mjs:16-26`；`ARCHITECTURE.md:662`）。
- 手机端：回答前先把（requestID, commandID, 目标身份）写进本机 journal（**不写答案本身**），丢回执只用 `command.status` 查原 commandID，绝不重发；主机返回 `INTERACTION_EXPIRED / RESOLVED` 后这张卡永久退役（`AgentWorkspaceModel.swift:2219-2257, 2436-2461`；`ARCHITECTURE.md:654`）。
- 终端适配的回答：先写永久 claim 文件（`wx`），**每发一个方向键 / 回车之前都重新读屏 + 重读 transcript**，核对 tool_use 输入摘要、菜单选项摘要、transcript 文件身份；只要已经按过键又出错就是 `RESULT_UNKNOWN`（`terminal-permissions.mjs:99-122`）。已 claim 或交回终端但菜单还在时，会话保持 blocked，普通消息 / 附件 / 中断 / 压缩 / 退出等写操作一律拒绝，防止打字落进菜单（`controller.mjs:111-134`；`HERDR_PROGRESS.md:845, 1029`）。

### 4.4 与 Jarvis 现状对照
【事实】（Jarvis）
- 交互种类 `question / plan / permission / screen`（`jarvis/ios/Shared/Models.swift:403`）；没有 MCP Elicitation、没有单独的 MCP 权限来源展示、没有子 Agent 来源（zoekt 在 jarvis 仓库搜 `Elicitation` 无结果）。
- 权限表单只有工具名 + 一段等宽摘要 +「拒绝 / 允许 / 本会话总是允许」（`jarvis/ios/Jarvis/Views/Interactions/InteractionViews.swift:495-531`）。**服务端已支持拒绝原因**（`jarvis/server/internal/node/hooks.go:285-290` 的 `res.Message`，`Resolution.message` 也有，`Models.swift:479-501`），但 App 从不填它（全仓 `Resolution(allow: false)` 都不带 message）。
- 计划：「批准并自动执行（bypassPermissions）/ 自动接受编辑（acceptEdits）/ 打回 + 意见」（`InteractionViews.swift:437-493`；`hooks.go:267-277` 用 setMode）。removed 刻意不替用户选模式；Jarvis 让用户选模式，更实用，但「批准并自动执行」= 全部放行，风险提示不足。
- 终端对话框（screen 卡）是**通用解析**任何对话框（信任目录、模型选择……），按编号 / 光标算键，node 按键前重读屏幕比对 `rev`（`jarvis/docs/protocol.md:185-203`）——覆盖面比 removed 的两类适配广；但没有和 transcript 里的 tool_use 交叉核对，命令被截断时卡片上看到的就是截断的屏幕文字。
- 交互挺过重启：确定性 interaction id + hook 重连 2 分钟 + hub 宽限 3 分钟（`protocol.md:322-333`）——这一点 Jarvis 比 removed 强（removed 的 hook 进程依附于 SSH 无关的主机进程，本身不受手机断线影响，但没有「hub 重启后卡片不丢」这类问题，因为没有 hub）。
- Jarvis 新会话启动时由 node **自动按「Yes, I trust」**（`jarvis/docs/claude-integration.md:13-15`）；removed 让用户在原生页确认。

【判断】Jarvis 应补：拒绝原因（服务端现成）、计划意见与批准互斥、倒计时、MCP 来源、Elicitation（表单 + 链接）、子 Agent 来源、交互草稿按请求保留、内联（记录末尾）权限卡、多请求收件箱、终端来源 Bash 审批「只读这一次」的严格适配思路（用 transcript 的完整命令显示，而不是屏幕截断文字）。

## 5. 提示词队列 / 持久化 outbox / 草稿 / 附件

### 5.1 默认路径：单操作 journal（所有主机都有）
【事实】
- 本机 `AgentLocalStateStore`（原子 JSON，文件 + 目录 fsync；版本 1–8 兼容读取，遇到更新版本拒绝覆盖）（`AgentLocalStateStore.swift:150-165, 251-258`）。作用域 = 连接 ID + 主机地址 / 用户名 + 已信任 SSH 指纹 + Herdr server；会话键 = Agent 类型 + 原生 session ID（避免 pane 复用串草稿）（`ARCHITECTURE.md:325`）。
- 发送流程（`AgentWorkspaceModel.swift:2175-2216, 2400-2435`）：
  1. `prepare`：把 `AgentPendingOperation(kind, target 六字段, originalDraft, originalDraftVersion)` 和当前草稿**先原子落盘**，同一会话已有未决操作就拒绝（`operationPending`）（`AgentLocalStateStore.swift:278-284`）；
  2. 调 bridge（每个请求带 commandID = 操作 UUID）；
  3. `finish`：回执校验通过后删 pending；**只有草稿仍是发送时那一版（文本、版本 UUID、编辑序号都没变）才清空草稿**，否则提示「操作已确认，当前草稿已保留」（`:2425-2435`；`AgentLocalStateStore.swift:300-306`）。
  4. 失败：明确失败（主机给了确定错误码）→ 结束记录、显示原因；**结果未知 → 保留 pending，阻止这个会话的下一次发送 / 交互 / 改名 / 停止**，状态行显示「上次操作结果待核对」（`:2436-2461`；`AgentsView.swift:668-669`）。
- 核对：「核对上次操作结果」只调 `command.status(原 commandID)`；仍未知就继续等；用户在主机上确认后可「确认已执行 / 确认未执行，保留输入」，记一条本机人工处置历史（启用队列的主机还要拿到主机端审计回执）（`AgentWorkspaceModel.swift:2284-2309, 2405-2424`；`AgentSessionStatusView.swift`）。
- 停止（interrupt）也是一个带 commandID 的操作：发出后 `interruptSubmitted` 直到主机报非 working，期间停止键置灰（「停止已发送，等待主机更新」）（`AgentWorkspaceModel.swift:2376-2399`；`AgentsView.swift:578-587`）。主机实现是 `ctrl+c`，并撤销同一 pane 上尚未写入 socket 的普通输入（`controller.mjs:533-544`）。
- 工作中继续发：主机允许 working 的 Claude 接收新消息（进 Claude 自己的排队），但先用带颜色读屏确认输入框是空的——Claude 的「Press up to edit queued messages」灰字不算草稿，用户真打的字算（`controller.mjs:404-412`；`HERDR_PROGRESS.md:1116-1120`）。和 Jarvis 的 `input_draft` 判定是同一思路（`jarvis/docs/protocol.md:205-221`）。

### 5.2 可选：主机持久多消息队列（0.7.62–0.7.65，默认关闭）
【事实】（`removed/docs/HERDR_PROMPT_QUEUE.md`；`HERDR_CINDY_REVIEW.md:51-59, 101-129`）
- 动机：Cindy（开源 Agent 客户端）对照审计发现单操作 journal ≠ 多条离线队列；要有「主机明确接管（host-owned）」「逐条取消」「FIFO」「崩溃恢复」的完整契约（`HERDR_CINDY_REVIEW.md:37-59`）。
- 每条不可变项：item UUID、host UUID、clientScope（安装 UUID + 已核对主机 scope 的 SHA-256）、bridgeSession、六字段目标、createdAtMs、正文、图片、文件；SHA-256 摘要按「8 位十六进制字节长度 + 冒号 + 原字节」逐字段拼接，跨语言固定向量测试（`HERDR_PROMPT_QUEUE.md:7-20`）。
- 主机状态（SQLite WAL + synchronous=FULL + fullfsync）：`queued → claimed → executing → confirming / submitted / cancelled / rejected`；**cancel 与 begin 在同一写事务里决出胜负**；executing 之后取消只记「取消待确认」，**不发 Ctrl-C**；`confirming` 永不自动重发；cancelled 删正文留身份墓碑，迟到的 accept 不能复活（`HERDR_PROMPT_QUEUE.md:26-44`）。按「bridgeSession + 六字段目标」FIFO，同目标早的未完成项挡住后面的；派发前再核对实例、交互、输入框（`:40, 54`）。
- 手机 outbox phase（`AgentPromptOutbox.swift`；`AgentPromptQueueView.swift` `statusText`）：已存手机，等待交给主机 / 主机接收结果待确认 / 已进入主机队列 / 输入结果待核对 / 核对结论待确认 / 取消待确认 / 已提交给会话 / 已取消 / 主机未提交此条（或「已核对为未送达」）/ 身份或协议变化，已暂停。
- 手机行为：本地保存成功即可编辑下一条（网络接纳不占编辑器）；第一次 SSH 前持久标记 `enqueueStarted`；未知时先查原身份，只有协商到 v1 才允许**同 UUID、同载荷**重试；同目标前一项接纳未知时后面的本机项等待（`HERDR_PROMPT_QUEUE.md:66`；`AgentPromptQueueModel.swift:120-160`）。取消未开始的项只在本机终结，开始后保留取消意图直到主机回执。已取消 / 被拒的可「恢复为草稿」；终态可「清除本机正文」（保留身份与回执）（`AgentPromptQueueView.swift` `promptRow`）。
- 会话页入口「待发消息 · N 条待处理 ›」，队列页可切「本会话 / 本主机」，工作区「全部记录」即使原会话消失也能找到；关闭队列要求本机和主机都没有未决项（`HERDR_PROMPT_QUEUE.md:68`）。
- 离线编辑：断线后或冷启动（读取私钥之前）可按上次已核对的目录打开原会话继续编辑、排队，只存手机；重连并**重新核对主机实例与六字段身份**后才上传，进程变了就冻结不发给新进程（`HERDR_PROMPT_QUEUE.md:76, 83-101`）。
- 可选的主机后台服务（launchd / systemd --user，用户确认后安装），让队列在手机退出后继续派发（`HERDR_PROMPT_QUEUE.md:103-116`）。

【判断】这套队列是给「SSH 直连、没有常驻服务端」的架构补出来的。Jarvis 有常驻 hub + Postgres，**等价物应放在 hub 上**：hub 持久接纳（落库）后才回 accepted，手机 outbox 只负责「hub 接纳之前」那段；没必要照搬「主机 SQLite + 后台服务」。但它的**状态枚举、取消 / 派发竞争在一个事务里决、confirming 不自动重发、墓碑防复活、同目标 FIFO** 这些契约可以直接搬。

### 5.3 草稿
【事实】
- 每个会话（按原生 session）持久草稿：文本 + 图片 + 文件 + 版本 UUID，250 ms 去抖写盘；离开页面 / 后台 / 缓存淘汰前立即 flush（`AgentWorkspaceModel.swift:1659-1660, 1780-1796`；`AgentsView.swift:653-660`）。
- 同一原生会话换了 reader（重连）时继承尚未落盘的编辑，序号更大，旧回执不能清掉新草稿、迟到保存不能复活旧草稿（`AgentWorkspaceModel.swift:1768-1779`；`ARCHITECTURE.md:325`）。
- 列表行显示「草稿 · 摘要」；只有附件时摘要显示「[附件]」（`AgentFleetView.swift:520-522`；`HERDR_PROGRESS.md:877`）。
- 草稿写失败：状态行「草稿暂未保存」，详情页写原因（`AgentsView.swift:671`）。

### 5.4 附件
【事实】
- 「＋」菜单：相机 / 照片 / 文件（语音入口按用户要求移除）；键盘编辑时也能加（`AgentImageComposerView.swift`；`HERDR_PROGRESS.md:758-766`）。
- 图片：方向校正、首帧、白底转 JPEG、最长边 1600（必要时 1200 / 800）、去掉照片元数据，单张 ≤1 MiB、最多 4 张；文件：合计 4 项 / 4 MiB；图片 + 文件混发要主机能力 `promptAttachments`，否则保留完整草稿并提示升级，**不拆开发送**（`ARCHITECTURE.md:749-757`；`README.md:137`；`AgentWorkspaceModel.swift:2181-2190`）。
- 附件字节随草稿持久化（草稿 v6），点击发送才上传；主机生成私有文件名（目录 0700 / 文件 0600），SHA-256 核对，图片逐张粘贴路径并等待 `[Image #N]` 就绪再提交文本；离线时文案「附件仅保存在手机；重新核对原主机和会话后才能上传」（`AgentImageComposerView.swift` 文案；`ARCHITECTURE.md:753-755`）。
- 混合附件的「回执丢失」用 transcript 证据核对：输入前记下原生日志基线与请求摘要，之后只有出现**唯一且内容 / 图片 / 会话完全一致**的新 user 记录才确认提交（`controller.mjs:418-465`；`removed/docs/HERDR_ROADMAP.md:129-131`）。

### 5.5 与 Jarvis 现状对照
【事实】（Jarvis）
- 发送：点发送立即清空输入框、「发送中」气泡上屏，transcript 里出现同样的话（或进了排队列表）就换成真消息，失败变红可重发 / 删除（`jarvis/docs/ios.md:113-114`）。离线 / 超时 → 「连上后自动发送」，存盘（`Outbox.swift`），重连后**自动按顺序重发**，同一条带同一个 `client_msg_id`（`jarvis/docs/ios.md:288-292`；`jarvis/ios/Jarvis/App/Outbox.swift`）。
- 去重只在 **node 进程内存**里，记 10 分钟（`jarvis/server/internal/node/sendsafe.go:132-171`），node 重启或超过 10 分钟就忘；hub 侧没有按 `client_msg_id` 去重（zoekt：hub 代码里没有 ClientMsgID）。而 App 把**超时**也当「等待」自动重发（`SendSafety.swift` `catch where error.isOffline || error.isTimeout → .waiting`）。
- 「打字中途出错」回 `delivered: unknown`，App 只弹一句「可能已经送到电脑上了：先看看对话里有没有」，气泡变失败、用户可手动重发（`SendSafety.swift`）。
- 排队消息 = Claude TUI 自己的队列，App 从 transcript 的 queue-operation 还原，「撤回编辑 / 拷贝 / 删除」（`jarvis/docs/ios.md:140-141`）；没有主机级持久队列（hub 没有接纳语义）。
- 草稿：只存文本（`SendSafety.swift` `SavedDraft{key,text}`），列表行不显示草稿；图片附件在 `composerInbox`。
- 附件：图片最多 4 张、1600px；文件最多 200 MB 传到会话所在电脑的临时目录，把路径插进输入框、不自动发送（`jarvis/docs/protocol.md:223-238`）——比 removed 的 4 MiB 宽得多。

【判断】Jarvis 的「乐观上屏 + 断网排队自动发」体验更好，应保留；风险点是**超时自动重发 + 去重只在 node 内存**：hub / node 重新部署（文档说曾两分钟内部署三次，`jarvis/CLAUDE.md`「经验」）后，超时的那条再发一次可能打两遍。建议：①把 `client_msg_id` 的接纳记录落到 hub 的 Postgres（hub 先落库再转 node，node 回执也落库），去重窗口按会话长期保留；②区分「请求根本没发出（离线）」与「发出后超时（结果未知）」，后者不自动重发，进入「待核对」，用 transcript 里是否出现同 id / 同文本的用户行来自动核对（removed 的 transcript 证据法）；③草稿持久化附件、列表显示草稿摘要。

## 6. 历史会话浏览

【事实】
- 入口：工作区「会话」标题旁的时钟按钮 →「历史会话」全屏页（`AgentsView.swift:222-228, 404-411`）。结构：①「最近阅读」最多 20 项（历史父 / 子记录的阅读入口，左滑「移除入口」只删快捷入口、不删主机历史与阅读位置）；② 项目选择（按名称或完整目录搜索，同名项目显示路径）；③ 该项目的历史会话（标题、更新时间、「正在运行，继续会打开现有会话」标记）、分页「加载更多历史会话」、搜索已加载会话或 ID、「有 N 条记录无法验证项目或格式」提示（`AgentSessionHistoryView.swift:1-160`）。
- 标题来源优先级：Claude 自定义名称（transcript 最近 256 KiB 或同目录 `custom-title.json`）→ 自动标题 → 首条用户消息 → 会话 ID；只读开头 6 MiB 验证项目、末尾 256 KiB 找名称，中间没读到时不沿用可能已改掉的旧名（`removed/README.md:94`）。
- 历史详情可「查看聊天记录」（**只读**，不需要先恢复会话）：原生文本 / 代码 / 工具记录、按需读更早、已加载内容搜索定位、手动刷新、独立阅读位置（行内偏移、展开状态，跨重启恢复，离开期间有新内容时保留原位置并给「跳到最新」）、子任务逐层阅读；这个页面不提供发送或权限操作（`README.md:92`；`AgentHistoryTranscriptView.swift`）。
- 继续会话：运行中的同一原生会话直接打开，否则 `session.resume`。恢复前先持久化操作；结果不明时只「核对会话启动结果」，并有主机侧**恢复锁**（同一原生会话同时只能有一个恢复操作）；人工处置要求先在主机确认「原启动请求已结束、目标 Claude 已退出、原窗格已回到 shell 或关闭」，主机复查后记审计才解除限制（`README.md:145-147`；`AgentsView.swift:203-218, 345-370`）。
- 自定义 Claude 配置目录（`CLAUDE_CONFIG_DIR`）：安装时显式配置 > SSH 进程继承的环境变量 > `~/.claude`；记录读取与新建 / 恢复的启动环境统一到同一目录，不合并多个目录（`README.md:96-102`）。

【事实】（Jarvis）列表里有「最近（未运行）」会话，状态 `offline`，向它发消息会 `--resume`（`jarvis/docs/agent-states.md:14, 53`）；node 会扫描所有正在跑的 claude 的 `CLAUDE_CONFIG_DIR` 并合并多个目录（`jarvis/CLAUDE.md`「经验」）。我没有找到「按项目浏览全部历史会话 / 只读打开」的专门入口（未逐一核实 hub 返回的未运行会话条数上限）；知识库里有「会话」档案（`jarvis/docs/ios.md:11`）。

【判断】Jarvis 需要的是「历史 = 列表的一个范围」而不是单独的大页面：在 Agents 列表加「历史」范围（按项目 / 时间），点开先只读（同一个对话页，输入框换成「继续这个会话」按钮），确认后再 resume；加「最近阅读」不必（Jarvis 的「完成未看 / 已看」已覆盖大部分回访）。

---

## 7. 终端

【事实】（removed）
- 传输：同一条已认证 SSH 连接上每次附着开一个独立 session 子通道；PTY 和 exec 都要求服务端成功应答（为此在 vendored Citadel 上加了 `withCancellablePTY`）；2026-10-24 起**直接用 PTY + exec 执行 `herdr … terminal attach <terminalID>`**，不再先开登录 shell 再往 stdin 打 attach 命令（去掉欢迎语、命令回显）（`removed/docs/ADR-001-SSH-EXEC.md` PTY 补充段；`HERDR_PROGRESS.md:1149-1159`）。渲染用 SwiftTerm（`TerminalUI/RemoteTerminalView.swift`）。窗口尺寸随手机变化同步给同一 PTY（`HERDR_PROGRESS.md:32`）。
- 生命周期：每个工作区保留最近 3 个有稳定 terminalID 的 pane 的 PTY + 画面，离页后保留 90 s，快速返回复用原 SSH 子通道和 SwiftTerm 画面；缓存画面可先显示，**重新核对 pane 身份后才开放输入**；离页时清空未写出的按键（`HERDR_PROGRESS.md:1149-1159`；`AgentWorkspaceModel.swift:166`；`RemoteTerminalView.swift` `setInputEnabled`）。控制中心这类短暂 inactive 保留 PTY，真正进后台关闭（主机会话不受影响），回前台先核对原 pane / terminal 身份再自动附着（`HERDR_PROGRESS.md:829-839`）。
- 同一 pane 里的进程切换：Claude 退出自动显示 shell；shell 里启动了 Claude 或完成目录信任，周期识别后给「原生聊天已就绪」入口，**不打断正在进行的终端输入**（`HERDR_PROGRESS.md:606`；`AgentPaneView.swift:37-51`）。
- 键栏（`AgentTerminalToolbar.swift`）：一条胶囊工具栏 Ctrl / Esc / Tab / 方向键开关 / 键盘开关；**Ctrl 三态**：点一下只作用于下一个键、400 ms 内双击锁定（显示锁图标与底线）、再点解除（`TerminalUI/TerminalControlLatch.swift`）；长按 Ctrl 展开 ⌃C ⌃D ⌃Z ⌃L ⌃A ⌃E ⌃U ⌃K ⌃W 快捷面板；方向面板（四向、回车、退格、Home、End）以**透明悬浮层**盖在终端内容上，展开收起不改变终端行数；横屏 / 键盘弹起 / 辅助字号时换成单行横滑条（`HERDR_PROGRESS.md:855-863`）。方向键走 SwiftTerm 的公开导航方法，跟随正常 / 应用光标模式（`HERDR_PROGRESS.md:596`）。
- 输入顺序：键盘输入、粘贴、特殊键全部进同一个有界串行队列（容量 256，溢出即断开报错，不丢序）（`TerminalUI/TerminalInputQueue.swift`）。输入法组合文字不按 UTF-8 字节加 Ctrl（`TerminalControlLatch.swift` `keys(for:composing:)`）。
- 独立 SSH 终端：没装主机组件时也能打开登录 shell（关闭页面 / 后台即关闭，明确提示「不保证持久，持久任务用 tmux」）（`removed/docs/HERDR_HOST_COMPONENTS.md:19-21`）。

【事实】（Jarvis，`jarvis/docs/terminal.md`、`jarvis/docs/ios.md:144-177`）：hub 中转的 herdr observe 实时帧 + 自写 VT 子集网格渲染（宽字符 / 框线对齐）；「手机尺寸」接管（`terminal session control`，离开即还原电脑尺寸）；滚轮按行拆事件落在手指那一格；底部弹出全屏盖板、下拉关闭；Moshi 式键栏（ctrl / alt / ⇧ 单击粘滞、双击锁定、按住）、快捷命令、补全、捏合字号、双指切 Agent、链接 App 内打开、状态条「上次更新 N 秒前」。

【判断】终端这块 **Jarvis 全面领先**（实时流、续传、接管、滚动、补全、多手势）。removed 值得借的只有小点：①方向面板做成**透明悬浮、不改终端行数**；②「缓存画面先显示、身份核对后才开放输入、离页清空未发按键」；③同一 pane 内 shell ↔ Claude 切换时给「原生聊天已就绪」入口而不是跳转。

## 8. 安全设计：「结果未知 → 只核对不重发」「操作前校验进程绑定」

### 8.1 身份：什么叫「同一个会话」
【事实】
- 主机端：`binding = sha256(进程身份)`（herdr 报的 agent 进程身份），descriptor 带 `paneID / terminalID / binding / nativeSessionID`（`controller.mjs:62-74`）。每个写操作在主机上先 `checked(binding, paneID)`：重新取当前 agent、重新算身份哈希，不一致回 `STALE_SESSION`（`controller.mjs:83-89`）；prompt 还会再比 nativeSessionID，并在真正输入前 / 每张图片粘贴前重复检查（`controller.mjs:400-489`）。
- 客户端：六字段完全相等才算同一会话（`hasSameActivityIdentity`；`AgentNavigationTarget`）。pane 里换了进程（Claude 退出重开、/clear 之外的替换）→ 旧 reader 停止、草稿和阅读状态落盘，**新 reader 不继承旧进程的草稿、审批和待核对操作**（`HERDR_PROGRESS.md:606`；`AgentWorkspaceModel.swift:1917-1931`）。
- 每个 reader / 页面持有 `revision`，所有 await 之后先比对 `revision`、`isCurrent`、`Task.isCancelled`，迟到的结果直接丢（全文件随处可见，例如 `AgentWorkspaceModel.swift:2041, 2199, 2241`）。连接层还有 connection revision / generation：编辑地址或认证身份就递增，旧 generation 的数据不能写进新连接的页面（`ARCHITECTURE.md:228`；`HERDR_PROGRESS.md:1105-1111` 重连后当前页按持久 scope + pane/terminal 身份核对后换新 lease，不退出页面）。
- 粗粒度列表不能覆盖细粒度状态：会话页开始详细读取后，状态 / 能力只由详细结果维护，列表的 `done` 不能把「等待回答」改回「完成」并重新开放发送（`AgentWorkspaceModel.swift:1918-1931`；`HERDR_PROGRESS.md:943-956`）。
- 通知跳转、恢复导航、离线目录都要核对 主机 generation + 六字段；进程变了就拒绝跳转、明确提示「原会话已退出或被替换，未跳转到新的进程」（`AgentFleetModel.swift:167-179`）。

### 8.2 写操作的三段式
【事实】
1. **派发前持久化**：手机 journal 先写 pending（含原草稿版本），失败就不发（`AgentWorkspaceModel.swift:2400-2404`）。
2. **主机幂等回执**：`once(request)` 以 `commandID.json` 用 `wx` 独占创建，内容是 `{digest: sha256(method+params), completed:false}`；同 ID 不同载荷 → `COMMAND_ID_REUSED`；同 ID 未完成 → `RESULT_UNKNOWN`「上一次命令的结果待确认，不会再次执行」；已完成 → 直接返回原响应 / 原错误；只有一组**确定性错误码**（`STALE_SESSION / COMPOSER_NOT_EMPTY / AGENT_BLOCKED / INVALID_INPUT / INTERACTION_EXPIRED…`）才把记录标成完成（表示「肯定没执行」），其他异常保持未完成（`controller.mjs:783-806`）。过程中用 `checkpoint(partial)` 记下「做到哪一步」（例如是否已经调用了 `agent.prompt`、创建的 paneID 和预分配的 nativeSessionID）（`controller.mjs:430-447, 556`）。
3. **只读核对**：`command.status` 读回执；对未完成的记录按 partial 类型做**只读的正向证明**：混合附件 → transcript 里出现唯一一致的新 user 记录且原发送进程已死；改名 → 连续两次读到目标标签；交互 → hook 的 delivery 回执；创建 → 新 pane 上出现预分配 session ID 的 Claude；退出 → 原进程已结束（`controller.mjs:580-629`）。证明不了就一直是 unknown，**从不重放**。
- 客户端对 unknown 的处理：保留 pending、阻止同会话下一次写操作、状态行和会话状态页给「核对」与「人工处置」两个入口；人工处置只记录结论（「确认已执行 / 确认未执行，保留输入」），不发送任何东西（`AgentSessionStatusView.swift`）。
- 「已回传」≠「工具执行成功」：交互回执的 `returned` 只表示交给了 hook；UI 文案「本次选择已回传」，执行结果仍以 transcript 为准（`requests.mjs:213`；`README.md:116, 128`）。
- 不自动重试的边界也写进了 SSH 层：建立连接超时与已派发请求超时是两种错误；后者关闭故障传输、操作留待核对，不因重连重发（`ARCHITECTURE.md:783-787`）。重连成功也不自动重发输入或审批（`ARCHITECTURE.md:329`）。

### 8.3 Agent 场景特有的防护
【事实】
- 往终端打字之前：输入框必须为空（ANSI 读屏区分灰色提示和真实草稿）、没有待答问题 / 待审批菜单（含已经交回终端但菜单仍在的情况）（`controller.mjs:111-134, 404-412`）。
- 终端菜单回答：claim 文件防重复，每个按键前重读屏幕 + transcript 并比对摘要，按过键之后出错一律 unknown（`terminal-permissions.mjs:99-122`）。
- 停止 ≠ 取消队列项；取消队列项 ≠ 发 Ctrl-C；停止有独立的 control fence，可撤销还没写进 socket 的普通输入（`HERDR_PROMPT_QUEUE.md:56`；`controller.mjs:539-542`）。
- 完成提醒前独立核验没有待答问题（防「题还挂着却通知完成」）（`controller.mjs:139-174`）。
- 推送载荷不含主机名、路径、会话 ID、提示词或正文，只有通用提示 + 随机 ID，详情经 SSH 认证后再取并核对身份（`HERDR_PUSH.md:11`）。

### 8.4 Jarvis 现状对照
【事实】
- 有：`client_msg_id`（但只在 node 内存 10 分钟，`sendsafe.go:132-171`）；输入框草稿 / 对话框检测后拒绝（`protocol.md:205-221`）；screen 卡按键前重读屏幕比对 `rev`（`protocol.md:193-195`）；交互确定性 id、挺过重启（`protocol.md:322-333`）；批准 / 回答类一次性操作断网不排队、失败把卡片放回（`jarvis/docs/ios.md:292-293`）。
- 没有：持久化的命令回执与「unknown」一级状态（超时被当成离线自动重发，见 5.5）；客户端写操作前的本地 journal（批准 / 中断 / 改名 / 关闭 Agent 丢回执后无法核对）；「进程身份」概念——Jarvis 以 Claude session id 为会话身份（这对「同一对话 resume 后继续发」是对的，但 pane 被别的进程占用、同一 session 被两个进程打开时没有保护，未核实）。

【判断】Jarvis 的身份应保持「对话 = Claude session id」（用户关心的是对话，不是进程），但写操作需要带一个**「目标实例指纹」**（machine + pane + 进程 pid / 启动时间 + session id），node 执行前核对，不一致就回确定性错误（等价于 `STALE_SESSION`），由 App 提示「会话已在别处重开，要发到新进程吗？」。回执与核对建议放 hub（Postgres 一张 `commands` 表：id、digest、状态 pending/done/unknown、partial、结果），node 侧把「是否已调用 herdr 写入」作为 checkpoint 回报。

## 9. 对照 Jarvis：removed 有而 Jarvis 没有 / 做得更好

按对设计的价值排序。「Jarvis 现状」一栏是我在 jarvis worktree 里查到的事实；没查到的标「未发现」。

| # | removed 的做法（出处） | Jarvis 现状 | 建议优先级【判断】 |
| --- | --- | --- | --- |
| 1 | 写操作三段式：本机 journal → 主机 `wx` 幂等回执 + 载荷摘要 → `command.status` 只读核对 + 正向证明；unknown 是一级状态并挡住后续写（§8.2；`controller.mjs:580-629, 783-806`） | `client_msg_id` 去重仅 node 内存 10 分钟（`sendsafe.go:132-171`）；超时当离线自动重发（`SendSafety.swift`）；批准 / 中断等无回执核对 | P0 |
| 2 | 拒绝原因（≤4000 字，有原因时禁用允许）（`AgentInteractionView.swift:173-182, 324-329`） | 服务端支持 `message`（`hooks.go:285-290`），App 不填 | P0（几乎零成本） |
| 3 | 计划：意见与批准互斥；批准不替用户改权限模式（`AgentInteractionView.swift:147-156, 312-317`） | 有「打回 + 意见」，批准默认 bypassPermissions（`InteractionViews.swift:466-470`；`hooks.go:267-270`） | P1：保留选模式，但「批准并自动执行」要有风险说明、意见非空时禁用批准 |
| 4 | 交互倒计时、过期 / 已处理 / 会话已变化的明确态、请求快照草稿保留（§4.2） | Interaction 有 `expiresAt` 字段（`Models.swift:453`），UI 未见倒计时；未见按请求保留的草稿 | P1 |
| 5 | MCP 权限显示服务器与配置来源；MCP Elicitation 表单 / 链接（§4.2） | 未发现（仓库无 `Elicitation`） | P1（MCP 越来越多） |
| 6 | 子 Agent 请求带来源（ID / 类型），父页与子页都能处理（`README.md:118`） | 子 Agent 只读查看（`jarvis/docs/ios.md:180-182`），交互未见来源标注 | P2 |
| 7 | 会话内搜索（正文 / 思考 / 工具输入输出 / 主机事件），命中高亮，「返回搜索前位置」，可「加载更早并搜索」（§3.5） | 未发现会话内搜索（只有列表搜索 `ChatsView.swift:245`） | P1 |
| 8 | 关联定位：任务通知 / 监听事件 / 定时触发 / 文件未变化 → 跨页找原始调用并可返回（§3.5） | 未发现 | P2 |
| 9 | 任务进度聚合页（按结果顺序重建 TaskCreate/Update/List/TodoWrite，「可能与主机此刻不同」）（§3.6） | TodoWrite 内联清单（`ToolViews.swift:197-198, 588`），无聚合 | P1（长任务很需要） |
| 10 | 长文本全屏阅读器：冻结快照、分页、全文前后搜索、首末翻页、完整复制（§3.3） | 有「已截断，看全文」+「拷贝全文」（`ToolViews.swift:386-434`），未见文内搜索 / 分页 | P2 |
| 11 | 工具卡结构化字段 + 统一「原始输入 / 原始输出」+ 多种专用结果视图（Notebook、PDF、MCP 资源、Cron、Monitor、Worktree…）（§3.3） | 工具行 44pt、Bash / Edit / 图片 / 待办等常用工具有专门视图（`jarvis/docs/ios.md:19-25`） | P2：只补「原始 JSON」统一入口和 Edit diff 的上一处 / 下一处 |
| 12 | 折叠时就显示「N 项执行失败 / N 项尚无结果」（`AgentWorkSummaryView.swift`） | 工作行只有计数和用时（`jarvis/docs/ios.md:61`） | P1（失败不该被折叠藏起来） |
| 13 | 状态行把「本机正在回传 / 上次结果待核对 / 同步中（>15 s 未验证）」作为一级状态（§3.2） | 连接状态在标题位（`jarvis/docs/ios.md:273-297`），会话级「结果待核对」未见 | P1（配合 #1） |
| 14 | 完成前核验没有待答问题（`controller.mjs:139-174`） | 需要你 = herdr blocked 或有待处理交互（`agent-states.md`）；hook 未接住的终端菜单靠 hub 读屏（`protocol.md:185-203`），未见「done 前核验」 | P2（需确认 Jarvis 是否有同类误报） |
| 15 | 草稿含附件持久化；列表行「草稿 · 摘要」（§5.3） | 草稿只存文本，列表无草稿提示（`SendSafety.swift` `SavedDraft`） | P1 |
| 16 | 目录信任交给用户确认（`AgentPaneView.swift:84-120`） | Jarvis 新开会话时 node 自动选「Yes, I trust」（`claude-integration.md:13-15`） | P2（用户自己选的目录，自动信任可接受；但电脑上手动开的会话遇到信任框时应给原生页，而不是 screen 卡） |
| 17 | 阅读位置连同展开状态持久化；找不回时逐页找（≤20 页）并给「继续查找 / 查看最新」（§3.4） | 记屏幕最上面那一行（`chat.read.<会话>`）+ 完成未看落在最后一条输入下（`jarvis/docs/ios.md:66-67`） | P3（Jarvis 的进入策略更好，只补「展开状态」记忆） |
| 18 | SSH 网页预览（手机本地回环端口 → 主机 127.0.0.1 显式端口）、模拟器只读截图（`HERDR_PROGRESS.md:644-659`） | 未发现 | P3（hub 也能做端口代理预览，是个新功能点） |
| 19 | 主机组件随 App 安装 / 升级（摘要校验、原子切换、只升级已知官方版本）（`HERDR_HOST_COMPONENTS.md`） | `scripts/deploy.sh` 从开发机部署（`jarvis/CLAUDE.md`） | 不适用（Jarvis 是 hub 架构） |
| 20 | 推送载荷零内容（`HERDR_PUSH.md:11`） | 推送正文是最后一条回复（`jarvis/docs/ios.md:258`） | P3：做成设置项「锁屏不显示内容」 |

---

## 10. Jarvis 更好的地方（重新设计时必须保留）

| 方面 | Jarvis（出处） | removed（出处） |
| --- | --- | --- |
| 实时性 | hub 推 `sessions / revs / items / interaction`，订阅中 node 每 250 ms 读新行；`items` 有 `history / from_seq / to_seq` 连续性和补洞（`jarvis/docs/protocol.md:148-248`） | 1 s 轮询、无事件续传（`AgentWorkspaceModel.swift:1933`；`HERDR_CINDY_REVIEW.md:47`） |
| 推送 | hub 直接 APNs；需要你 / 完成两类，可按设备设置；授权 / 计划**通知上直接批准**；实时活动 + 灵动岛（`jarvis/docs/ios.md:258-263`；`jarvis/docs/live-activity.md`） | 可选主机 daemon 每 15 s 采集、默认关、需自己配 APNs 凭据；本地通知只在 App 前台采集（`HERDR_PUSH.md`；`AgentActivityView.swift`） |
| 多机器 | 一个 hub 统管所有电脑，App 只连 hub；机器监控、空间 / 槽位（`jarvis/docs/agent-states.md`；`jarvis/docs/machines.md`） | 每台主机独立 SSH 连接与密钥，手机直连（`AgentFleetModel.swift`） |
| 状态模型 | 需要你 / 工作中 / 完成未看 / 待唤醒 / 已屏蔽 / 已看 / 未运行，hub 统一计算、App / 手表 / 推送共用（`agent-states.md`） | 待处理 / 工作中 / 已完成 / 就绪 / 状态待核对，没有「看过」概念 |
| 列表 IA | 按项目 / 状态 / 机器三种分组，置顶、屏蔽、左右滑操作、同空间同伴菜单、空间页、机器页（`jarvis/docs/ios.md:12, 49-55`） | 活跃 / 待处理 / 全部 + 项目 / 最近（`AgentFleetView.swift`） |
| 对话展示 | 每条消息自成一段、工作行可原地展开为独立懒加载行、多消息合并气泡、唤醒 / 回顾抽屉、倒置列表精确贴底（`jarvis/docs/ios.md:60-104`） | 60 行 VStack 窗口 + 实测行高（`ARCHITECTURE.md:705-711`） |
| 发送体验 | 乐观上屏、离线排队自动发、`input_draft` 时可「清空并发送」（`jarvis/docs/ios.md:113-114, 288-292`；`protocol.md:217-218`） | 发送前必须先落盘，unknown 时整会话停发直到核对 |
| 终端 | hub 实时流 + 续传、手机尺寸接管、滚轮、补全、快捷命令、多手势（§7） | SSH PTY + SwiftTerm，3 个 × 90 s 缓存 |
| 无头助手 | 「Jarvis」tab：无头 `claude -p`、逐字流式、ask_user、定时任务 / 跟进、知识库（`jarvis/docs/assistant.md`） | 无 |
| 其他能力 | 权限模式 / 思考强度标签可在会话里改（`jarvis/docs/ios.md:196-209`）、Claude 建议胶囊、/loop 与后台任务条、文件 / Git 页、链接 App 内打开、分享卡片、手机数据工具、Siri | 无对应（Git 只读审查、用量页较简单） |
| 交互挺过重启 | 确定性 id + hook 重连 + hub 宽限（`protocol.md:322-333`） | 不需要（无 hub），但手机断线时卡片只能等下次轮询 |
| screen 卡通用性 | 任意 TUI 对话框都能原生化，按键前比对 `rev`（`protocol.md:185-203`） | 只有 AskUserQuestion 与 Bash 两类适配 |

## 11. Agents 部分重新设计建议（【判断】为主）

### 11.1 原则
1. **对话身份 = Claude session id**（保留 Jarvis 现有模型，resume 后照常发）；但每个写操作附带**目标实例指纹**（机器 + pane + 进程 pid / 启动时间 + session id），node 执行前核对，不一致回确定性错误，App 问「会话已在别处重开，发到新进程吗？」。不学 removed 那种「进程变了就冻结一切」的严格度。
2. **hub 是唯一权威**：命令回执、消息接纳、交互决定都落 Postgres；手机 outbox 只负责「hub 接纳之前」。removed 的主机 SQLite 队列 + 后台服务是无 hub 架构的补丁，不照搬。
3. **结果未知是一级状态**：任何「发出后没收到确定回执」的写操作进入「待核对」，自动核对（读 hub 回执 / transcript 证据），核对不了就显式交给用户；**绝不因为超时自动重发**。离线（根本没发出）才自动排队重发。
4. **一处状态**：会话页只有输入区上方一条状态行表达「现在怎样」，顶栏只放身份（名字、机器 · 空间 › tab）；本机在途 / 待核对 / 同步陈旧都在这条线上。
5. **能力驱动 UI**：会话 descriptor 带能力位（发图、发文件、拒绝原因、Elicitation、子 Agent 来源、改模式……），hub `features` 带全局能力；UI 不写 `if claude / codex`，Codex / TRAEX 只是能力少一些。
6. 不确定就说不确定：「暂无结果 ≠ 仍在执行」「已回传 ≠ 已执行」「根据已加载记录整理」这些措辞照搬 removed。

### 11.2 信息架构
- **Agents 列表**（保留 Jarvis 三种分组与状态序）：
  - 顶部范围段：需要你（数量）· 进行中 · 全部 · 历史。「历史」= 未运行的会话按项目 / 时间浏览（取代单独的历史页）。
  - 行：状态点 + 标题 + 时间 / 最新一句 / 元信息；新增**草稿摘要**（橙色「草稿 · …」/「[附件]」）与**待核对徽标**（时钟问号）。
  - 「需要你处理」卡片升级为**跨会话请求收件箱**入口：每项一行（类型、会话、摘要、剩余时间），点开直接是完整交互页，不必先进会话。
- **会话页**（自上而下）：
  1. 顶栏（保留 Jarvis 跟手收起的 ChromeHeader）：返回 / 标题 + 身份副标题 / 同伴菜单 / 终端 / ⋯。⋯ 菜单：搜索、任务进度、子 Agent、文件、Git、用量与上下文、模式与思考强度、改名、压缩上下文、关闭 Agent。
  2. 可选横条：「任务进度 · x/y ›」（有结构化任务结果时）。
  3. 正文（保留倒置列表与工作行）：工作行补「N 项失败」常显；展开后顶部吸附「收起」条；记录末尾放**内联权限卡**（只放普通工具权限，其余类型给入口）。
  4. 状态行（唯一）：左侧确定状态（见 11.4-a），右侧附 spinner 实况（Jarvis 现有 `Cogitating… 12s · ↓3.2k`）与「改动」入口；有 notice 时整行可点进「会话状态」页。
  5. 交互区：单个请求 → 面板（保留 Jarvis 贴在输入框上方、可折叠、限高 55% 的面板）；多个 → 「N 项待处理 ›」进收件箱。
  6. 循环 / 后台 / 排队 / 待发条（保留 Jarvis 的 QueueStrip 等），新增「待核对 N」条。
  7. 输入栏（保留 ComposerRow：＋ / 输入 / 发送或停止，模式与思考强度胶囊）。
- **新增页面**：会话内搜索（全屏，命中定位 + 高亮 +「返回搜索前位置」胶囊）、任务进度页（每项「查看相关记录」）、全文阅读器（分页 + 文内搜索）、跨会话请求收件箱、会话状态页（待核对的核对 / 人工处置入口）。
- **交互完整页**（sheet）：类型标题、会话与来源（主机 · 项目、子 Agent、MCP 服务器与来源、终端来源警示）、倒计时、主体（参数字段 + diff / 计划原文 / 问题 / 表单 / 链接）、可选拒绝原因或修改意见、固定底部操作、「交给电脑上的终端」。

### 11.3 组件清单
| 组件 | 要点 |
| --- | --- |
| `StatusLine` | 状态机 11.4-a；只有它每秒重绘；带 notice 时 44pt 可点 |
| `InteractionCard`（内联）/ `InteractionSheet`（完整）/ `RequestInbox` | 按请求快照提交；草稿按请求 id 保存；过期 / 已处理 / 会话变化后禁用并说明；有原因时禁用「允许」，有意见时禁用「批准」 |
| `WorkLine` + `StickyWorkHeader` | 计数「工具 N · 思考 N · 回复 N · 用时」+ 失败数常显；用时只取 transcript 记录的 turn duration，没有就不显示 |
| `ToolCard` | 结构化字段 + 统一「原始输入 / 原始输出 / 复制」；预览上限 + 全文阅读器 |
| `DiffView` | 保留 Jarvis 现有实现，补「上一处 / 下一处」导航与快照冻结 |
| `FullTextReader` | 冻结快照、分页（按字素边界）、文内搜索、首末页、完整复制 |
| `SearchOverlay` + `ReturnChip` | 搜索结果定位后顶部出现「返回搜索前位置」；关联定位同理「返回任务通知」 |
| `TaskProgressBanner` / `TaskProgressPage` | 按结果顺序重建；明示「可能与此刻不同」 |
| `PendingBubble` | 状态见 11.4-b；待核对时气泡带「核对中 / 请在电脑确认」 |
| `VerifySheet`（会话状态页） | 列出待核对操作：自动核对结果、「确认已送达 / 确认未送达（保留输入）」 |
| `DraftStore` | 文本 + 附件 + 版本号；回执清草稿前比对版本 |

### 11.4 状态机
**a. 会话实时状态（StatusLine，客户端合成）**
`ended`（会话结束 / 被替换）> `offline`（连不上 hub，沿用标题位的连接状态，不重复）> `submitting`（本机有写操作在途）> `unconfirmed`（有待核对操作）> `stale`（超过 N 秒没收到 hub 的确认，显示「正在同步」）> `needsYou`（有待处理交互或 herdr blocked）> `working`（含 `stopping`：中断已发出、尚未离开 working）> `unread / idle`。N 取多少需要实测 hub 推送间隔后再定（removed 用 15 s，`AgentLiveActivity.swift`，非实测值）。

**b. 发出的消息**
```
draft ──发送──▶ local(未联网，存盘) ──联网──▶ sending
sending ──hub 落库回 accepted──▶ accepted ──node 回 delivered──▶ delivered ──transcript 出现──▶ shown
sending ──确定性拒绝(input_draft/dialog_open/stale/invalid)──▶ refused（可编辑重发 / 清空电脑草稿并发）
sending ──超时 / 断线且不知道 hub 是否收到──▶ unknown ──查 hub 回执──▶ accepted… | local（hub 确认没收到才回到可自动重发）
accepted/delivered 中 node 报 delivered:unknown ──▶ verifying ──transcript 有同 id/同文──▶ shown | 无法证明 ──▶ needsReview（人工：已送达 / 未送达）
取消：只允许 local / accepted-未派发；派发与取消的竞争在 hub 同一事务里决定
```
**c. 交互请求**
`pending(deadline)` → `answering` → `answered`（hook 已取走）| `expired` | `resolvedElsewhere`（电脑上或别的设备答了）| `handedToTerminal` | `unknown`（回答请求丢回执 → 按 interaction id 查 hub）。answered 之后由 transcript 显示工具实际结果，卡片文案「已回传」而不是「已完成」。

**d. 一次性操作（中断 / 改名 / 关闭 Agent / 压缩 / 改模式 / 改思考强度）**
`pending` → `sent` → `done` | `failed(确定)` | `unknown → verifying`。中断 ≠ 取消排队消息；有 `unknown` 时同一会话的下一次同类操作要先核对。

### 11.5 Android 实现要点
- **列表与对话**：Compose `LazyColumn(reverseLayout = true)` 天然是倒置列表，Jarvis 在 iOS 上为翻转、安全区、边缘模糊做的绕路（`jarvis/CLAUDE.md` 经验）大多不需要；仍要遵守「每个工作步骤是独立 item、给稳定 key 和 `contentType`」。吸顶收起条在 `reverseLayout` 下不能直接用 `stickyHeader`，需按 `LazyListState.layoutInfo` 自绘浮层（待原型验证）。
- **键盘与边距**：edge-to-edge + `WindowInsets.ime`/`imePadding()`；输入区放在列表下方的 Column 里（与 Jarvis iOS 的结论一致）。
- **返回手势**：预测性返回（`PredictiveBackHandler`）；交互完整页用 `ModalBottomSheet`，草稿在 dismiss 时按请求 id 存。
- **推送**：FCM 替代 APNs（hub 要加 FCM HTTP v1 发送端）；「需要你」用高优先级消息；通知操作用 `RemoteInput`，可以在通知里直接输入**拒绝原因 / 计划修改意见**（比 iOS 只能在 App 里输入更顺）；批准类操作要求解锁（`setAuthenticationRequired`，API 级别需核实）。实时活动的对应物是常驻通知；Android 16 的 Live Updates（进度类通知）可考虑，API 与限制需核实；没有灵动岛。
- **后台**：不要在后台保持 WebSocket（Doze / 后台限制），沿用「前台连、后台断、推送唤醒」；离线 outbox 的补发可用 WorkManager（联网约束），但必须带同一个 client id，且只处理「hub 确认没收到」的条目。
- **存储**：outbox / 操作日志 / 草稿用 Room（事务保证「落盘后才发送」「回执清草稿要比对版本」）；配对 token 用 Android Keystore 生成的密钥加密（Jetpack Security Crypto 的维护状态需核实）。
- **终端**：协议是 hub 中转的 VT 帧，把 Jarvis iOS 自写的网格渲染（`TerminalGrid` / 宽字符 / 框线画成线）移植到 Compose Canvas；现成终端库（如 Termux 的 terminal-emulator）许可证与适配度需核实。键栏、Ctrl 三态（单击粘滞 / 双击锁定 / 按住）逻辑可直接复用。
- **Markdown / 代码**：没有 MarkdownUI 对应物，需选型（commonmark-java + 自写 Compose 渲染，或现成 Compose Markdown 库，需评估表格 / 代码块 / 选择复制）；代码高亮照 removed 做有界词法扫描（几种语言、输入上限、后台、可取消）。Compose 的 `SelectionContainer` 支持跨段落选择，可以去掉 iOS 上「选择」要弹纯文本页的限制。
- **按钮不换行**：`maxLines = 1` + `softWrap = false`，空间紧时用自动缩字（Compose 新版 `BasicText` 的 autoSize，版本需核实）或改图标。
- **附件**：Photo Picker（`PickVisualMedia`）、相机 `TakePicture`、文件用 SAF；大文件按 hub 现有分块上传流式读 `ContentResolver`。

### 11.6 落地顺序建议
1. P0（协议 + 小 UI）：hub 侧命令回执表与 `client_msg_id` 持久去重；超时 ≠ 离线；拒绝原因；计划意见与批准互斥 + 「自动执行」风险说明；StatusLine 合并与「待核对」一级状态。
2. P1：会话内搜索；任务进度；跨会话请求收件箱 + 倒计时；草稿含附件 + 列表草稿摘要；工作行失败数常显；MCP 来源与 Elicitation；交互草稿按请求保留。
3. P2：关联定位；全文阅读器文内搜索；子 Agent 请求来源；终端细节（透明方向面板、缓存画面待核对后开放输入）；hub 端口代理网页预览。

## 12. 没查清 / 需要实测的地方

- removed 文档自述：真机网络时延、滚动帧率、真机键盘 / 大字号 / 边缘手势大多没有定量验收（`HERDR_PROGRESS.md:754, 1159`；`HERDR_CINDY_REVIEW.md:91`）；持久队列「真实服务商 + 手机 SSH 全链路」未验收（`HERDR_PROMPT_QUEUE.md:72-78`）。本笔记里的 1 s / 8 s / 15 s / 90 s / 60 行 / 20 页等全是代码常量或文档配置，**不是性能数据**。
- 没逐个读：`AgentHistoryTranscriptView.swift`、`AgentChildTranscriptView.swift`、`AgentElicitationFormView.swift`、`AgentUsageView.swift`、`AgentGitReviewView.swift`、`AgentWebPreviewView.swift`、各 `*ResultView.swift`、主机 `terminal-questions*.mjs`、`queue/*.mjs`——相关结论来自 README / ARCHITECTURE / PROGRESS 的描述。
- Jarvis：只用 zoekt 确认 hub 代码里没有 `ClientMsgID` 去重，没有跑过「node 重启后重发」的实验；是否存在「herdr 报 done 但题还挂着」的误报没有验证；未运行会话在列表里最多列多少条没有核实；电脑上手动开的会话遇到 Bash 审批时，screen 卡显示的命令是否会被截断没有验证。
- Android 相关的 API 级别与库（Live Updates、通知操作解锁要求、Compose 自动缩字、Jetpack Security Crypto 状态、终端 / Markdown 库许可证）全部需要另行核实。
- removed 的「交互卡片挂在对应工具记录下」做不到（协议没有 tool_use 关联 ID，`HERDR_CINDY_REVIEW.md:89`）；Jarvis 的 hook 输入里有 `tool_use_id`（`jarvis/ios/Shared/Models.swift:474`），理论上可以把权限卡挂到对应工具行，需要确认 transcript 落盘时机（tool_use 行在 hook 触发时是否已写出）。
