# 功能对齐矩阵（iOS 现有功能 + 重设计新增项）

> 草案 v1（2026-10-30），属于 [重设计方案](README.md)。第 1–24 节是 iOS 现有功能的完整清单（213 项，逐项核对过 Swift 源码），
> 每项写了 Android 的对应做法、复杂度和优先级；第 25 节是这次重设计新增、两端都要做的项；第 26 节是调研基线（77999ab）之后 iOS 新增、Android 也要对齐的功能。原始调研笔记见 [research/jarvis-features.md](research/jarvis-features.md)。
>
> **怎么用**：动手之后，每个表格加两列「iOS 等级 / Android 等级」，按
> 已开发 → 单测通过 → mock 截图通过（深浅色 × 字号）→ 测试 hub 通过 → 真机通过 标级（[README.md](README.md) 第 9 节）。
> 现在 iOS 的 213 项都是「已上线」，Android 全部「未开始」。
> 优先级：P0 = Android 首版必须；P1 = 首版之后第一批；P2 = 可以延后；✕ = 不做或换形态（理由见 [android.md](android.md) 第 6 节）。

## 0. 口径

- **代码规模**：`ios/` 下 189 个文件，Swift 约 4.18 万行（不含测试约 3.76 万行）。target：`Jarvis`（App）、`Shared`（App / 手表 / 扩展共用）、`JarvisWatch`（watchOS）、`JarvisWidgets`（实时活动 Widget 扩展）、`LiveActivity`（实时活动按钮的 App Intent）、单元测试 + UI 测试。iOS 26 / watchOS 26，Swift 6（默认 MainActor），唯一三方依赖 MarkdownUI。
- **服务端契约**：手机只连 hub（REST `/api/v1/...` + 一条 WebSocket）。Android 版原则上复用同一套接口；需要 hub 改动的地方在行内标「需 hub 改动」。注意：第 1–24 节是按「有 Google 服务」调研的，凡是写了 FCM、Google 登录的 Android 做法，都以 [README.md](README.md) D8 和第 25、26 节为准（没有 Google 服务：只走常驻连接，Android 上不做 Google 账号登录）。
- **复杂度**（按一个熟悉 Compose 的人估）：S ≤ 2 人日；M 3–7 人日；L ≥ 1.5 人周。
- **优先级**：P0 = Android 首版必须；P1 = 首版后第一批；P2 = 可延后；✕ = 建议不做或换形态。
- **列含义**：「iOS 专有能力」写实现上依赖的平台特性；「Android 对应做法」写 Compose / 系统 API，没有对应物时写替代方案。

---

## 1. 全局框架与基础设施

| # | 功能 | 一句话描述 | 关键文件 / 出处 | iOS 专有能力 | Android 对应做法 | 复杂度 | 优先级 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 1.1 | 四个 Tab + 角标 | Persona / Agents / 收件箱 / 设置；角标：Persona = 助手的待处理交互数，Agents = 非助手且未屏蔽的待处理交互数，收件箱 = 「需要你」件数；记住上次的 tab 和打开的 Agent 对话（冷启动回到原处） | `Jarvis/Views/RootView.swift`、`Jarvis/App/AppModel.swift`（selectedTab、rememberChat） | SwiftUI `TabView` + `.badge`（iOS 26 液态玻璃 tab bar） | M3 `NavigationBar` + `BadgedBox`（或 `NavigationSuiteScaffold`）；DataStore 记住 tab / 打开的会话 | S | P0 |
| 1.2 | 欢迎页 + 配对 | 未配对时：功能介绍 4 条 +「连接 hub」；扫 `personad hub pair` 的二维码（`jarvis://pair?url=…&token=…`）或手动填地址 + token；连接失败给出原因 | `Jarvis/Views/Settings/SettingsView.swift`（WelcomeView、PairView、QRScanner）、`Jarvis/App/JarvisApp.swift`（onOpenURL） | VisionKit `DataScannerViewController`；自定义 URL scheme `jarvis://` | Google Code Scanner（`play-services-code-scanner`，不需要相机权限）或 CameraX + ML Kit；intent-filter 处理 `jarvis://pair` | S | P0 |
| 1.3 | 设备凭据 | 配对后用配对 token 换本机专属 token（`/api/v1/auth/tokens`），并给手表换一个「只能回答」的 token；旧 hub 不支持就沿用配对 token；设置 › 设备 可逐台撤销（见 13.6） | `Jarvis/App/DeviceCredentials.swift` | Keychain；WatchConnectivity 推给手表 | Android Keystore + Tink 加密的 DataStore（`androidx.security:security-crypto` 已弃用，不要再用） | S | P0 |
| 1.4 | WebSocket 实时连接 | 指数退避重连，重连后按 `since_seq` 重新订阅打开的对话；hello 里带 hub 的 features（`terminal_stream` / `terminal_control` / `terminal_scroll` / `terminal_click` / `file_upload`），按 feature 决定界面能力；**进后台主动断开**（否则 hub 以为手机在线，phone_* 调用干等） | `Shared/LiveConnection.swift`、`Jarvis/App/AppModel.swift`（handle、didConnect）、`Jarvis/App/JarvisApp.swift`（scenePhase） | `URLSessionWebSocketTask`（ping 回调要求 pong 原样带负载 + 有挂着的 receive）；`scenePhase` | OkHttp WebSocket + 协程；`ProcessLifecycleOwner` 判断前后台；可选「常驻连接」前台服务（见 25 节） | M | P0 |
| 1.5 | 全局状态与对话存储 | `AppModel`：机器、按机器分的会话、待处理交互、每个会话一个 `ChatStore`（按 id 去重、按 seq 排序、补洞 fill / 从最新页 reload、内存里最多常驻 8 个对话）；WS 事件：`hello / machines / sessions / revs / items / delta / screen / interaction / interaction_resolved / tool_call / lark / inbox / fleet / terminal / error` | `Jarvis/App/AppModel.swift`、`Shared/ChatStore.swift`、`Shared/Models.swift` | Observation（`@Observable`）、Swift Concurrency | ViewModel + `StateFlow`（或 Compose snapshot state）+ kotlinx.serialization + 协程；`ChatStore` 的补洞 / 去重算法和 `ChatStoreTests` 用例建议 1:1 移植 | L | P0 |
| 1.6 | 本地缓存（先画缓存） | `StateCache`（机器 / 会话 / 待处理交互 / 助手设置、收件箱、邮箱账号、每个飞书会话最近 80 条）+ `ChatCache`（每个会话最后两轮，超过 6 MB 去掉图片）；冷启动、断网、切 tab 都先画缓存再静默刷新，有缓存就不转圈 | `Shared/StateCache.swift`、`Shared/ChatStore.swift`、`docs/ios.md`「本地缓存」 | Caches 目录 JSON | Room（或 cacheDir 下 JSON + kotlinx.serialization）；图片缓存交给 Coil | M | P0 |
| 1.7 | 后台同步（推拉结合） | 只在前台且连着 hub 时：hub 推 `revs`（各会话 transcript 字节数），App 按 `after_seq` 拉增量，内存里有就合并、没有写磁盘缓存（活着的 Agent + 24 小时内用过的，最多 40 个、并发 3）；飞书：hub 转发 lark-connector 实时事件，轮询兜底（列表 90 秒、会话 60 秒），连上时预取最近 20 个会话 | `Jarvis/App/AgentSync.swift`、`Jarvis/App/LarkSync.swift`、`docs/ios.md`「后台同步」 | — | 同样的逻辑：协程 + Room，`repeatOnLifecycle(STARTED)` 期间运行 | M | P1 |
| 1.8 | Toast 与错误策略 | 顶部胶囊 toast（info / success / error，2.5 / 4 秒）；被取消、超时、离线的错误一律不弹（`Error.shown` / `isQuiet`）；断网时用户操作失败只给一条蓝色「连不上 hub / 没有网络，刚才的操作没有生效」（4 秒内最多一条） | `Jarvis/Views/RootView.swift`（ToastOverlay）、`AppModel.show / notDone` | — | 自绘顶部 toast 或 Snackbar；统一的错误分类函数（cancel / timeout / offline / 业务错误） | S | P0 |
| 1.9 | 触感 | tap / success / error / warning 四种：发出、回答成功、新交互、出错 | `AppModel.swift`（Haptics） | `UIFeedbackGenerator` | `LocalHapticFeedback` / `View.performHapticFeedback(CONFIRM / REJECT)`（API 30+） | S | P1 |
| 1.10 | 深链接 / 打开路由 | `jarvis://open?machine=…&session=…`（点实时活动）、点通知、Siri 结果卡片：`AppModel.open` 按是否助手对话分流到 Persona tab 或 Agents tab；冷启动时只设状态，界面出现后再导航 | `JarvisApp.swift`、`AppModel.open` | `onOpenURL` | intent-filter + `onNewIntent` → 导航状态 | S | P0 |
| 1.11 | 手机写操作确认 | Claude 调 phone_* 写操作（建日程 / 提醒、完成提醒）时弹「Claude 想在 iPhone 上执行」+ 摘要，允许 / 拒绝；拒绝返回 `The user declined on the phone.` | `RootView.swift`、`AppModel.confirm`、`docs/phone-tools.md`「确认流程」 | `.alert` | 前台：AlertDialog；后台被唤醒时：高优先级通知 + 允许 / 拒绝动作按钮 | S | P1 |
| 1.12 | 发送安全 | 电脑上 Claude 输入框里有草稿或有对话框占着键盘时，node 拒绝发送：弹「电脑上的输入框里有字」→「清空并发送 / 先不发」；每条消息带 `client_msg_id`，丢响应后重发不会打两遍；「可能已经送到」时提醒先看对话 | `Jarvis/App/SendSafety.swift` | — | 同逻辑 + AlertDialog | S | P0 |
| 1.13 | 草稿持久化 | 每个对话没发出的输入框文字跨启动保留（也是排队消息「撤回编辑」的落点） | `SendSafety.swift`（drafts）、`AppModel.drafts` | — | DataStore / Room | S | P0 |
| 1.14 | 明文 HTTP | hub 在内网走明文 HTTP（Info.plist `NSAllowsArbitraryLoads`） | `ios/Jarvis/Resources/Info.plist` | ATS 例外 | `network_security_config` 允许 cleartext（最好只放行配置里的 hub 域名） | S | P0 |

## 2. 「Persona」助手 tab

| # | 功能 | 一句话描述 | 关键文件 / 出处 | iOS 专有能力 | Android 对应做法 | 复杂度 | 优先级 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 2.1 | 首页 | 头像 + 按时段问候 + 一句能力说明（还没有主对话时说明「这是你的主对话」）+ 在线机器和 iPhone 小标签 + 6 个建议卡片（今天的安排、要处理的邮件、电脑在忙什么、飞书消息、会前准备、这周的健康），点一下直接发出 | `Jarvis/Views/Assistant/AssistantView.swift`（AssistantHome） | — | `LazyVerticalGrid` 两列卡片 | S | P0 |
| 2.2 | 新对话输入 | 底部 `StarterComposer`：照片（≤ 4 张，压到 1600px JPEG）、文件（上传到助手所在机器的临时目录，路径插入光标处）；发出第一句立即显示气泡 +「Persona 正在准备…」，后台起会话 | `AssistantView.swift`（StarterComposer、start）、`Jarvis/Views/Chat/ComposerParts.swift`、`Jarvis/Views/Chat/FileAttach.swift` | PhotosPicker、`fileImporter` | Photo Picker（`PickMultipleVisualMedia(4)`）、SAF `OpenMultipleDocuments`、OkHttp 带进度上传 | M | P0 |
| 2.3 | 首页的启动器 / 模式 / 强度 | 输入框上方三个芯片：启动器（launcher · 模型）、权限模式、思考强度（不选 = 跟随启动器，记住选择） | `AssistantView.swift`（StarterModelChip、NewSessionModePicker） | `Menu` | DropdownMenu / ModalBottomSheet 单选 | S | P1 |
| 2.4 | 预热 | 显示新对话页时 `POST /api/v1/assistant/warm`，hub 先起备用 claude 进程（3 分钟内不重复请求） | `AppModel.warmAssistant`、`docs/assistant.md`「更快」 | — | 同 | S | P1 |
| 2.5 | 主对话 / 旁支对话 | 第一段对话自动成为主对话（hub `assistant.main`），首次启动打开它；右上 🏠 回主对话、✎ 新对话；其他页面的「交给 Persona」「问 Persona」都发进主对话（没有就新建） | `AssistantView.swift`、`AppModel.askJarvis`、`docs/assistant.md`「主对话」 | — | 同 | S | P0 |
| 2.6 | 历史对话 | 左上 🕘：半屏列表（主对话标记、标题、工作中 / 需要你 / 完成未看的状态或相对时间、最后一句），左滑「结束」还活着的对话 | `AssistantView.swift`（AssistantHistory） | `.presentationDetents([.medium, .large])`、swipeActions | ModalBottomSheet + 滑动结束 | S | P0 |
| 2.7 | 助手对话页 | 复用对话页（7 节）的助手样式：系统导航栏、隐藏思考、工具调用折成一行「工具 4 · 电脑、iPhone…」可展开；逐字流式（delta + 闪烁光标）；没有终端 / 文件 / Git / 模式栏里的「全部放行」 | `Jarvis/Views/Chat/ChatView.swift`（style `.assistant`）、`AssistantView.swift`（AssistantToolsRow）、`Jarvis/Views/Chat/MessageViews.swift`（StreamingText） | — | 复用 Android 对话组件 + 样式参数 | M（在 7 节之上） | P0 |
| 2.8 | 标题菜单 | 标题下一行「启动器 · 模型 ⌄」：选下一条消息起用的启动器（hub `--resume` 换过去）；「会话信息」卡片 | `AssistantView.swift`（AssistantTitleMenu）、`ChatView.swift`（DevInfoCard） | `Menu` 当标题 | TopAppBar 标题可点 → DropdownMenu | S | P1 |
| 2.9 | 头像 | SwiftUI 画的面罩头像（强调色圆盘 + 两只发光眼睛 + 火花），颜色跟主题主色，工作中眼睛左右看；点开资料页 | `Jarvis/Views/Assistant/JarvisAvatar.swift`、`docs/assistant.md`「资料页和头像」 | SwiftUI 形状动画 | Compose `Canvas` + `rememberInfiniteTransition` | S | P2 |
| 2.10 | 知识库入口 | 左上 📚 进知识库（3 节） | `AssistantView.swift` | — | 同 | S | P1 |
| 2.11 | ask_user / 通知 | 助手用 MCP `ask_user` 在手机上问（原生提问卡片，标题「Persona 在问你」，推送标题同）；`notify_user` 走推送，点开就是这段对话 | `Jarvis/Views/Interactions/InteractionViews.swift`、`docs/assistant.md`「现在的结构」 | APNs | 复用交互面板（7.30）+ 常驻连接的通知（17 节） | — | P0 |

## 3. 知识库（Persona tab 左上 📚）

| # | 功能 | 一句话描述 | 关键文件 / 出处 | iOS 专有能力 | Android 对应做法 | 复杂度 | 优先级 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 3.1 | 列表 | 分段选 6 类（项目 / 会话 / 人 / 文档 / 会议 / 日程）；常用标签横排筛选；行：重要级徽章 P0–P3、名称、项目类型（长期 / 短期）、一句话、与我相关性短条 + 数字、前 3 个标签、更新时间；项目分「进行中 / 已完成 / 不是项目（折叠）」 | `Jarvis/Views/Knowledge/KnowledgeView.swift`、`Shared/Knowledge.swift`、`docs/knowledge.md`「App 接口」 | `Picker(.segmented)`、DisclosureGroup | `SingleChoiceSegmentedButtonRow` 或 ScrollableTabRow、FilterChip、LazyColumn | M | P1 |
| 3.2 | 全文搜索 | 300ms 防抖调 `kb-search`，结果显示命中行 + 路径 | 同上 | `.searchable` | M3 `SearchBar` | S | P1 |
| 3.3 | 详情 | 头部（重要级、类型、相关性、标签）；「问 Persona」（档案 + 最近时间线 + 目录作为 `<jarvis-context>` 发主对话并给出提问）、「开 Agent」（项目：新建对话预填档案）；在做这个项目的 Agent（点开对话）；档案（Markdown，可选）；时间线；关联（实体 + 代码目录）；最近 7 天的消息（折叠，等宽）；知识库路径 | `KnowledgeView.swift`（KnowledgeDetailView） | MarkdownUI、`textSelection` | Markdown 渲染 + `SelectionContainer` | M | P1 |
| 3.4 | 打标 | 右上「打标」半屏表单：重要级、相关性滑杆（步长 5）、标签（空格分隔）、项目类型（短期需求 / 长期使命）和状态（进行中 / 暂停 / 已完成 / 不是项目）；手动值在 hub 锁住不被覆盖 | `KnowledgeView.swift`（KBTagEditor） | Form + Slider | ModalBottomSheet 表单 | S | P2 |

## 4. 助手资料页 / 主动做事 / 记忆（Persona 头像或 设置 › 助手）

| # | 功能 | 一句话描述 | 关键文件 / 出处 | iOS 专有能力 | Android 对应做法 | 复杂度 | 优先级 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 4.1 | 资料页 | 头像、「运行在 <机器>」、在线 / 工作中 / 下一件；「接下来」3 件；主动做事（定时任务、已安排的跟进、运行记录）；了解你（关于我、记忆）；设置（启动器、运行在、工作目录）；能访问清单 | `Jarvis/Views/Assistant/AssistantProfileView.swift`、`docs/assistant.md`「资料页和头像」 | List(.insetGrouped) | LazyColumn 分组卡片 | S | P1 |
| 4.2 | 定时任务列表 | 「接下来」时间线（左边大号时刻，像「提醒事项」）；每个任务：图标（按标题推断）、「Persona 建的」标签、规则人话（「每个工作日 早上 9:00」）、下次、上次结果、行尾开关直接启停；跟进前 5 条（更多进全部）；最近运行 3 条；＋ 菜单：模板（会前准备 / 周报 / 下班前盘点）+ 空白 | `Jarvis/Views/Settings/AssistantProactiveViews.swift`（AssistantRoutinesView、AssistantRoutineTemplate、AssistantScheduleText） | Toggle、Menu | Switch、DropdownMenu；中文时间文案函数直接移植（注意 24 小时制） | M | P1 |
| 4.3 | 定时任务编辑 | 标题、开启、时间（时 : 分）、星期（7 个圆钮 + 每天 / 工作日 / 周末快捷）、提示词；立即运行（跳到 Persona tab 打开这次的对话）、查看上次结果、历史记录（前 5 条 + 全部）、删除确认；已有的离开页面即保存，新建是「取消 / 添加」浮层（试模板不会误建） | 同上（AssistantRoutineEditor、AssistantWeekdayPicker） | DatePicker(.hourAndMinute)、sheet | M3 `TimePicker`、自绘星期芯片；离开保存用返回拦截 / `DisposableEffect` | M | P1 |
| 4.4 | 已安排的跟进 | 按时间排；左滑取消、右滑或长按立即运行；「跟进 · 新对话 / 接着原来的对话」 | 同上（AssistantFollowupsView、AssistantFollowupRow） | 双向 swipeActions、contextMenu | 自定义双向滑动行 + 长按菜单 | S | P1 |
| 4.5 | 运行记录 | 按天分组；成功 / 跳过 / 失败图标，「准时 / 晚了 N 秒 / 手动运行 / 错误原因」；点开打开那次的对话 | `AssistantProfileView.swift`（AssistantRunsView、AssistantRunRow） | — | LazyColumn + stickyHeader | S | P2 |
| 4.6 | 记忆 | Claude Code auto memory 文件列表：解析 frontmatter、标题优先取 MEMORY.md 索引里的中文标题、类型标签（按类别配色）；详情 Markdown；「编辑」改整个文件；左滑删除（确认）；索引单独一行只读 | `AssistantProactiveViews.swift`（AssistantMemoryView、AssistantMemoryDetail、AssistantMemoryEditor、AssistantMemoryTag） | — | 同 | M | P2 |
| 4.7 | 关于我 | 编辑 notes（离开即保存），写进助手 CLAUDE.md 末尾 | 同上（AssistantNotesView） | — | 多行 TextField | S | P2 |

## 5. 「Agents」tab：列表

| # | 功能 | 一句话描述 | 关键文件 / 出处 | iOS 专有能力 | Android 对应做法 | 复杂度 | 优先级 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 5.1 | 三种分组 | 左上菜单选（记住）：**按项目**（默认，见 5.5）/ **按状态**（需要你 → 完成未看 → 工作中 → 待唤醒 → 已屏蔽 → 已看，「已看」折叠）/ **按机器**（每台一段，标题带在线状态和状态计数，段内按状态排、空闲折叠）；三种分组共用一个排序函数 `rank`（同状态按最后更新时间，工作中按进入状态的时间） | `Jarvis/Views/Chats/ChatsView.swift`、`docs/ios.md`「信息架构 · 开发」、`docs/agent-states.md` | `Menu` + `Picker` | DropdownMenu；排序 / 分组逻辑直接移植 | M | P0 |
| 5.2 | 机器筛选 + 监控入口 | 同一个菜单：全部机器 / 某一台（左上显示机器名）；最下面「机器监控」进 Dashboard（14 节） | `ChatsView.swift`（machineMenu） | — | 同 | S | P0 |
| 5.3 | 「需要你处理」卡片 | 列表最上：每个待处理交互一张卡（类型图标、标题、会话 · 机器、摘要 3 行）；单题单选且 ≤ 4 个选项时可直接点答案，授权可直接允许 / 拒绝；点卡片进对话 | `Jarvis/Views/Interactions/InteractionViews.swift`（InteractionPreviewCard、FlowButtons、RecommendedTag） | — | Card + 横排按钮 | S | P0 |
| 5.4 | 置顶 | 置顶的 Agent 单独一段（只出现这一次）；置顶空间排最前、不折叠；存在 hub（`/api/v1/pins`），本地缓存先排序 | `ChatsView.swift`、`AppModel.pins / togglePin` | — | 同 | S | P0 |
| 5.5 | 按项目：空间分段 | 有动静的空间一段：段头（空间名、📌 / 🔕、机器名、各状态彩点计数、› 进空间页、可见的 ⋯ 菜单：重命名 / 置顶 / 屏蔽）；段内忙的在前，空闲的折成「还有 N 个空闲」+ tab 名芯片；屏蔽但在跑的空间排在后面弱显示；全空闲的空间收进「空闲的空间」（每个一行，可展开） | `ChatsView.swift`（SpaceGroup、SpaceHeader、StateCounts、IdleFoldRow、QuietSpaceRow、SpaceMenuItems） | List Section header（长按菜单触发不了，所以放可见 ⋯） | LazyColumn `stickyHeader` + 可见 ⋯ 按钮 | M | P0 |
| 5.6 | Agent 行 | 状态点（需要你呼吸）+ 标题（完成未看加粗）+ 相对时间（30 秒刷新）；最新一句（2 行）；元信息「需要你 · Claude · 空间 › tab · 机器」（只有「需要你」写成红字）；屏蔽的 🔕 + 0.8 不透明度；行带 `/loop`「⟳ 下次 12:20」、琥珀「⚙ 后台 N」、「👥 子 Agent N」时不折进空闲 | `ChatsView.swift`（SlotRow）、`Jarvis/Views/Components.swift`（StatusDot、Chip） | `TimelineView` | `LaunchedEffect` 定时刷新；`Modifier.alpha` | S | P0 |
| 5.7 | 滑动操作 | 右滑：置顶 / 取消置顶、中断（工作中 / 需要你）、标为已看（完成未看）；左滑：关闭 | `ChatsView.swift`（swipeActions） | 多按钮 `swipeActions` | Compose 没有原生多按钮滑动行：自定义 `AnchoredDraggable` 行或三方库；或首版只做长按菜单 | M | P1 |
| 5.8 | 长按菜单 | 置顶、重命名…、屏蔽 / 开启通知、停止循环（有 /loop 时）、交给 Persona（Agent 的机器 / 会话 / 目录 / 状态 / 最后一句作为上下文发主对话）、拷贝目录、拷贝恢复命令、关闭 Agent… | `ChatsView.swift`（contextMenu）、`KnowledgeView.swift`（AgentHandoff） | `contextMenu` | `combinedClickable(onLongClick)` + DropdownMenu / 底部菜单 | S | P0 |
| 5.9 | 重命名 | 空间：同时改 herdr workspace 名；Agent：一次改 herdr tab 名和 Claude 会话名（`/rename`，Mac 上 `claude --resume` 看到的一致） | `ChatsView.swift`（RenameSpaceAlert、RenameSessionAlert）、`AppModel.renameSpace / renameSession` | `.alert` + TextField | AlertDialog + OutlinedTextField | S | P1 |
| 5.10 | 屏蔽 | 空间 / Agent 两级开关叠加（空间屏蔽时单个 Agent 可「开启通知」，反之亦然）；不推送、列表弱化、tab 角标不计；存 hub（`/api/v1/mutes`） | `AppModel.toggleMute`、`Shared/DevModels.swift`（Pins.togglingMute） | — | 同 | S | P1 |
| 5.11 | 关闭 Agent | 底部确认卡片：哪个 Agent、在哪台机器哪个标签页、关了会怎样，「关闭 Agent / 取消」（herdr 里任何 Agent 都能关） | `Components.swift`（CloseAgentSheet、confirmClose） | sheet | ModalBottomSheet | S | P0 |
| 5.12 | 搜索 | 搜空间名、tab 名、标题、目录、最新一句；搜索时所有折叠展开；从结果进入先关搜索、下一轮再推页面（避开 iOS 导航栏 bug） | `ChatsView.swift`（pushing） | `.searchable` 的怪癖 | M3 `SearchBar`（Android 无此坑） | S | P1 |
| 5.13 | 刷新 / 空状态 / 新建 | 下拉刷新；没有会话时空状态 +「新建会话」；右上 ✎ 新建会话（6.4） | `ChatsView.swift` | `refreshable` | `PullToRefreshBox` | S | P0 |
| 5.14 | 多种 Agent | Claude / Codex / TRAEX 同列表，行和卡片带种类标签；Codex / TRAEX 没有模式栏、/clear、排队、子 Agent、后台任务；等确认时走「终端等你处理」 | `docs/codex.md`、`Shared/Models.swift`（agentLabel、isCodex、isTraex） | — | 同 | S | P0 |

## 6. 空间页 / 机器的项目页 / 新建对话

| # | 功能 | 一句话描述 | 关键文件 / 出处 | iOS 专有能力 | Android 对应做法 | 复杂度 | 优先级 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 6.1 | 空间页 | 顶部机器胶囊（进机器的项目页）+ 目录；两列卡片（tab 名彩色芯片、Agent 种类、状态点、标题、状态 + 待处理数、最新三行、模型）；最后一张「在这个空间开新标签」（同一 herdr workspace 新建 tab）；右上菜单（重命名 / 置顶 / 屏蔽） | `Jarvis/Views/Chats/SpaceView.swift`（SpaceView、SlotCard） | `LazyVGrid` | `LazyVerticalGrid` | S | P1 |
| 6.2 | 同伴菜单 | 对话顶栏第二行最左：叠层图标 + 个数（别的 Agent 需要你时红点）；菜单第一项「空间 X 的所有 Agent」，下面每个 tab（名 + 状态，当前打勾），点了直接切换；从空间页进来的对话直接返回、不叠层 | `SpaceView.swift`（SiblingMenu）、`AppModel.siblings`、`ChatsView.swift`（spaceAt） | `Menu` | DropdownMenu | S | P1 |
| 6.3 | 机器的项目页 | 在线状态、「N 个项目 · N 个 Agent」、状态计数；进行中的项目（带最需要注意的那个 Agent 的最新一句）、空闲的项目；不属于任何空间的 Agent 直接列；右上 ⓘ 进机器详情 | `Jarvis/Views/Chats/MachineSpacesView.swift` | — | 同 | S | P2 |
| 6.4 | 新建 Agent 对话 | 机器卡片横滑（离线变灰不可选）；目录：目录浏览器（上一级、git 仓库标记、「使用『X』」）+ 最近 6 个目录；启动器（来自那台机器 `~/.zshrc` 的 claude 启动函数，可按函数名 / 模型搜，显示网关 host）；权限模式 + 思考强度（默认跟随启动器）；第一条消息（可空）；底部「开始对话」（错误显示在按钮旁，启动中禁止下滑关闭）；按机器记住启动器 / 模式 / 强度 / 目录；从空间页进入时是「加入空间」 | `Jarvis/Views/NewChat/NewChatView.swift`（NewChatView、LauncherPicker、DirectoryBrowser、SpacePreset） | Form、`interactiveDismissDisabled` | 全屏页面 / Dialog + LazyRow + 列表；DataStore 记住默认值 | M | P0 |
| 6.5 | /clear 清空上下文 | 输入框 ＋ 菜单（只对 herdr 里的 Claude）：确认后发 `/clear`，等同一窗格的新会话出现（最多 20 秒）后页面跟过去 | `ChatView.swift`（Composer）、`AppModel.clearContext` | `confirmationDialog` | AlertDialog | S | P1 |

## 7. Agent 对话页

| # | 功能 | 一句话描述 | 关键文件 / 出处 | iOS 专有能力 | Android 对应做法 | 复杂度 | 优先级 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 7.1 | 自绘顶栏 | 返回 / 标题 + 副标题（● 状态（工作中带本轮分钟数）· 机器 · 空间 › tab，放不下先省机器再省位置）/ 终端 / ⋯；第二行只在有同伴或循环 / 后台时出现（同伴菜单 + 小标签）；子 Agent 运行时再一排 Agent 条；断线时副标题换成「转圈 + 连接中…」，点开连接详情 | `Jarvis/Views/Chat/ChatView.swift`（devChrome、titleButton、devSubtitle） | 隐藏系统导航栏、`GlassEffectContainer`、`ViewThatFits` | 自定义顶栏；「放不下就省略」用 `SubcomposeLayout` 或测量 | M | P0 |
| 7.2 | 顶栏跟手收起 | Safari 式：随手指推动收起 / 拉回展开，松手吸附；在最新处收起；跟随新内容自动滚动时收起；停 3 秒自动收起；收起后细条（返回 + ● app › tab；断线时显示连接状态；右侧循环 / 后台标记 + 终端），点细条展开；只认用户拖动和拖后的惯性 | `ChatView.swift`（ChatChrome、ChromeHeader、compactChrome、chromeIdleSeconds）、`docs/ios.md`「顶部自动收起」 | `onScrollPhaseChange`、单独的 `@Observable` 进度 | `NestedScrollConnection` + 吸附动画（或 `TopAppBarScrollBehavior` 改造）；进度放单独 state，只重组顶栏 | M | P1 |
| 7.3 | 会话信息卡 | 点标题下拉：模型 · 启动器、上下文 tokens · 压缩过几次、分支、目录、位置、轮数 · 共生成 tokens、权限、思考强度、Claude Code / Codex 版本、开始时间；「拷贝 ID」「拷贝恢复命令」（按 Agent 种类生成 claude / codex / traex resume） | `ChatView.swift`（DevInfoCard） | — | 下拉卡片 | S | P1 |
| 7.4 | ⋯ 菜单 | 终端、回到对话、文件、Git（改动数）、中断、重命名…、屏蔽 / 开启通知、拷贝恢复命令、拷贝目录、空间里的所有 Agent、关闭 Agent… | `ChatView.swift`（moreMenuItems） | `Menu` | DropdownMenu | S | P0 |
| 7.5 | 倒置列表与定位 | 最新消息在偏移 0（ScrollView 和每行各翻转）；贴底跟随（手指一拖就停，回到底部 / 点 ↓ / 自己发消息再开）；「回到底部」箭头（末尾哨兵判断）；看历史时新行先不插入（按离开最新端时的快照冻结）；进入位置：完成未看的会话顶部只露你最后一条消息的最后一行、下面是做了什么和回复，其他会话回到上次离开时最上面那行；冷启动恢复阅读位置 | `ChatView.swift`（Frozen、followsEnd、landUnderInput、settleAtEnd）、`docs/ios.md`「进入位置」「历史」 | 翻转 ScrollView + 大量 workaround（安全区、边缘模糊、懒加载估算高度白屏） | `LazyColumn(reverseLayout = true)` 原生支持：`scrollToItem(0)` 精确、按 key 保持阅读位置，iOS 的大部分坑不存在；冻结 / 进入位置逻辑仍要做 | L | P0 |
| 7.6 | 历史分页 | 按轮分页（首次和每次到顶取两轮完整的「输入 + 回答」，`turns=2`）；滑到最远端自动加载（只在用户拖过之后）；最早十几行一出现就预取；带图的页在后台解码 | `AppModel.loadOlder`、`Shared/ChatStore.swift`、`docs/ios.md`「历史」 | — | `LazyListState` 监听可见项 + 协程（`Dispatchers.Default` 解码） | M | P0 |
| 7.7 | 展示逻辑：消息 → 工作行 → 回复 | 一轮按「你的消息」切段：消息 → 灰色工作行「工具 5 · 思考 1 · 回复 1 · 4 分 17 秒 ›」（折叠工具、思考、系统提示、中间回复）→ 下一条消息前的最后一段话；进行中的工作行带转圈、计时、终端按钮（「正在工作 39 秒 · 工具 2」）；放不下换短格式；已完成的多消息轮（排队发的）合成一个气泡（浅色分割线），展开后按消息拆开（「已按 N 条消息拆开 · 收起」） | `ChatView.swift`（ChatLine、WorkLine、WorkSummary）、`Shared/ChatStore.swift`（chatLines、Turn）、`docs/ios.md`「展示逻辑」 | — | 纯逻辑 1:1 移植（连同 `ChatStoreTests`、`RebuildTests` 用例）；UI 每段一行 | L | P0 |
| 7.8 | 展开工作过程 | 点工作行原地按序列出每一步（左侧细线，整体像一张卡片，每步是独立懒加载行，各画卡片的一段）；最后一步下「收起 N 步」；收起 / 展开不挪动正在看的内容 | `ChatView.swift`（WorkStepView、WorkCardSlice、toggleWork） | — | LazyColumn 行内画分段卡片背景 | M | P0 |
| 7.9 | 消息渲染（文字类） | 用户气泡（主题色，自己的在右）；助手 Markdown（无气泡，17pt 基准，表格 / 代码块）；思考（折叠一行，新版 Claude Code 的旁白写在 thinking 里不能藏）；流式文本；其他会话发来的消息「来自 名字」灰卡；系统提示胶囊（上下文已压缩、已中断、后台任务完成…）；`/loop` 唤醒一行「⟳ 循环唤醒 21:11」 | `Jarvis/Views/Chat/MessageViews.swift`（UserBubble、AssistantText、ThinkingRow、PeerMessage、NoticeRow、StreamingText）、`Jarvis/Views/Session/SessionPanes.swift`（LoopTickRow） | MarkdownUI（SwiftUI Text 不能跨块选择） | Markdown 渲染库（如 multiplatform-markdown-renderer 或 Markwon，需评估表格 / 代码高亮）；`SelectionContainer` 可跨块选择 | L | P0 |
| 7.10 | 工具渲染 | 工具组（>2 步折叠成「N 个步骤 · 运行 5 · 读取 3」，永远露最后一步）；每种工具的图标 / 动词 / 主语（Bash、Read、Edit、Write、Grep、Glob、WebFetch、WebSearch、Task / Agent、TodoWrite、AskUserQuestion、ExitPlanMode、Skill、ToolSearch、mcp__*、phone_*、mail_*）；Bash 命令 + 输出；「出错了」写在输出上方；完整输出点开再拉（分页里只带前 4 KB） | `Jarvis/Views/Chat/ToolViews.swift`（ToolPresentation、ToolGroupView、ToolCallRow、ToolDetail、CodeBlock、ResultBlock、FullOutputSheet） | — | 映射表 1:1 移植；SF Symbols 换 Material Symbols | M | P0 |
| 7.11 | Diff | 一个 `DiffView`：Claude 的 Edit / MultiEdit / Write（新旧文本）和 Codex 的 unified diff；头部文件名 · 目录 · +N −N（点开文件）；只给 ± 和行底色上色，代码保持正文色；长行横滑 + 右侧渐隐；超过 40 行先显示开头 +「展开全部」 | `ToolViews.swift`（DiffView）、`docs/ios.md` | — | 行模型移植；共享 `horizontalScroll`；渐隐用 `drawWithContent` | M | P0 |
| 7.12 | 待办 | TodoWrite 直接渲染成清单 | `ToolViews.swift`（TodoList） | — | 同 | S | P0 |
| 7.13 | 路径标签 | 消息里的路径、工具卡片里 Read / Edit / Write 的路径显示成可点标签（打开文件查看器），长按拷贝路径；识别结果按消息 id 缓存 | `Jarvis/Views/Chat/FileViewer.swift`（FileChips）、`Shared/TextMemo.swift` | NSDataDetector / 正则 | 正则识别 + 按消息 id 缓存，别在重组里跑 | S | P1 |
| 7.14 | 上下文引用卡片 | 消息里的 `<jarvis-context kind="context\|edit" title url>…</jarvis-context>` 显示成引用卡片（点开回到那个链接），原始标签不显示 | `Jarvis/Views/Link/LinkSheet.swift`（ContextRef、ContextCards） | — | 同 | S | P0 |
| 7.15 | 会话图片 | 缩略图按需加载（`lazy_images`：历史和推送只带尺寸，行上屏才按 ref 取图；内存 + 磁盘永久缓存；先按尺寸占位不跳）；你发的图在气泡上方显示方形缩略图（解析 `<attached_images>`，去掉「[Image #N]」） | `Jarvis/Views/Chat/ChatImageLoader.swift`、`MessageViews.swift`（SentPhotos、AttachedImages） | NSCache、ImageIO 降采样 | Coil 3：自定义 Fetcher（ref → hub file 接口）+ OkHttp 鉴权 + 磁盘缓存 | M | P0 |
| 7.16 | 全屏图片查看器 | 左右滑整个会话的所有图片，顶部「n / m」和关闭，点图或下拉退出，双指 / 双击缩放；底部：保存（只要「添加」权限）、编辑、分享、拷贝 | `Jarvis/Views/Chat/ImageViewer.swift` | UIScrollView 缩放；PhotoKit add-only | `HorizontalPager` + 缩放手势库（如 Telephoto）；MediaStore 保存（API 29+ 不需权限）；Sharesheet | M | P1 |
| 7.17 | 图片编辑 | 框选 / 画笔、6 种颜色、撤销 / 清除，完成「加到输入框」作为下一条消息的附件；输入框里待发的图也能点开先编辑 | `Jarvis/Views/Chat/ImageEditor.swift`、`AppModel.composerInbox` | — | Compose `Canvas` + 手势 | M | P2 |
| 7.18 | 长按消息操作条 | 像微信：在按下位置上方弹深色条（复制 / 选择 / 引用 / 分享，自己的消息多「编辑」），靠近顶部时放下方；点任意处或拖动列表关闭；「选择」打开纯文本页任意选；「引用」以 `> ` 放到输入框前面；「编辑」把旧消息放回输入框 | `Jarvis/Views/Chat/MessageMenu.swift`（MessageMenu、MessageActionBar、SelectableTextSheet） | UIKit 长按拿坐标 + 自绘浮层（SwiftUI Text 不能原地选一段） | `pointerInput` 拿坐标 + `Popup`；Android 用 `SelectionContainer` 可直接原地选择，「选择」页可省 | M | P1 |
| 7.19 | 分享卡片 | 把一条消息排成图片（渐变底 + 卡片：Claude / Codex / 我、来源「app › tab」、时间、正文、Persona 标记），浅 / 深可切；保存图片、分享图片、分享文字；超过 3000 字截断 | `Jarvis/Views/Chat/ShareCard.swift` | `ImageRenderer`（画不出 ScrollView，要专门的卡片 Markdown 主题） | `rememberGraphicsLayer()` + `toImageBitmap()`；MediaStore + Sharesheet | M | P2 |
| 7.20 | 发送中气泡 / 离线发送 | 点发送立即清空输入框、以「发送中」气泡上屏，请求在后台发；transcript 里出现同样的话（或进了排队）就换成真消息；失败变红可重发 / 删除；离线时带时钟「连上后自动发送」（点时钟「现在重试 / 不发了」），存盘、App 被杀不丢 | `MessageViews.swift`（PendingBubble）、`Jarvis/App/Outbox.swift`、`AppModel.sendNow / deliver` | — | Room 持久化 outbox + 协程按序发出 | M | P0 |
| 7.21 | 输入框 | 一行三件同高（约 44pt）：`[+]` 菜单（照片 ≤ 4、拍照、文件；快捷指令：继续、/compact、/context、提交并推送、汇报进展；清空上下文 /clear）、玻璃胶囊输入框、发送；没东西可发时发送键变淡；Agent 工作中且没打字时换成红色中断键；工作中占位「补充说明（会排队发送）」 | `ChatView.swift`（Composer、SlashCommand）、`Jarvis/Views/Chat/ComposerParts.swift`（ComposerRow、ComposerImages） | PhotosPicker、`UIImagePickerController`（相机）、`fileImporter` | Photo Picker、`TakePicture`、SAF；按钮状态机 | M | P0 |
| 7.22 | 文件上传 | 选任意文件（可多选）→ 上传到会话所在机器的临时目录（≤ 200 MB、排队、进度条、可取消）→ 机器上的路径插到光标处（不自动发送）；hub / node 太旧时给明确提示 | `Jarvis/Views/Chat/FileAttach.swift`（FileUploads、UploadsBar、PathInsert） | `NSFileCoordinator`（iCloud 占位文件） | SAF + `ContentResolver.openInputStream` + OkHttp 带进度的 RequestBody | M | P1 |
| 7.23 | Claude 的建议 | 会话有 `suggestion`、Agent 没在工作、输入框为空时，上方胶囊「✦ 建议 … 采用」，占位文字也换成它；点一下填进输入框可改，长按「直接发送」；一打字就消失 | `ChatView.swift`（SuggestionPill） | `contextMenu` 带预览 | 胶囊 + 长按菜单 | S | P1 |
| 7.24 | 权限模式 / 思考强度 | 输入框上方两个中性小标签（只有「全部放行」红）：模式（手动确认 / 自动改文件 / 只做计划 / 自动；「全部放行」只能新开时选）、强度（低 / 中 / 高 / 很高 / 最高），点开改，乐观更新；Codex / TRAEX 不显示 | `Jarvis/Views/Chat/SessionModes.swift`（SessionModes、SessionModeBar）、`docs/ios.md`「思考强度和权限模式」 | `Menu` + inline Picker | DropdownMenu 单选 | S | P1 |
| 7.25 | 排队消息 | Claude 工作中发的消息不进对话记录，显示在输入框上方「排队中 N 条 · Claude 忙完会依次处理」；每条：撤回编辑（回到输入框）/ 拷贝 / 删除（照 TUI：全部拉回再按顺序重排） | `ChatView.swift`（QueueStrip）、`AppModel.editQueued` | `Menu` | DropdownMenu | S | P1 |
| 7.26 | 循环 /loop | 「循环中 · 每 30 分钟 · 下次唤醒 11:56（还有 4 分钟）」，点开看循环的任务；「停止循环」确认后给 Agent 发停止指令（外部无法取消，只能让它自己停） | `SessionPanes.swift`（LoopStrip、ActivityChips、ActivitySheet）、`AppModel.stopLoop` | — | 同 | S | P1 |
| 7.27 | 后台任务 | Claude `run_in_background` 的命令 / 子代理还没回报时显示「N 个后台任务」（轮次结束后也在），展开看描述、命令、已运行时间，点一条打开它的输出文件；顶栏「⚙ 后台 N」 | `SessionPanes.swift`（BackgroundTasksStrip、ActivityChips、ActivitySheet） | — | 同 | S | P1 |
| 7.28 | 唤醒 / 离开回顾 | 后台任务把空闲 Claude 唤醒：仍算同一轮，「真正的回复」取最长那段；回复下面一个小条「🔔 后台命令完成 · <描述> ›」，点开从底部拉起抽屉看唤醒后的过程；离开回顾（recap）：对话里一行「↺ 离开期间回顾 · 21 分钟前 ›」+ 抽屉看全文，回到对话时最新一条在对话末尾展开成全文，和回复一起滚动（点或开始打字收成小条，#1360） | `MessageViews.swift`（WakeChip、WakeDrawer、RecapCard、RecapStrip、RecapDrawer）、`ChatStore.swift`（pickAnswerAcrossWakes、ChatBlock.build） | sheet | ModalBottomSheet | M | P1 |
| 7.29 | 子 Agent | 有子 Agent 在跑时顶栏多一排「主 Agent / 子 Agent」（转圈）；点一个用同一套界面只读看它的对话（会话 id `<主会话>@<agent id>`，历史 / 实时 / 分页同普通会话），底部「子 Agent · Explore · 运行中，只读 · 回到主 Agent」；展开的工作行里每个 Agent 调用下「查看子 Agent」 | `SessionPanes.swift`（AgentStrip、SubagentFooter）、`ChatView.view(_:)` | — | 同 | M | P1 |
| 7.30 | 交互面板（提问 / 计划 / 授权） | 贴在输入框上方，限高（屏高 55%，内部滚动）、可折叠，⋯「交给电脑上的终端处理」：**提问**（分组列表样式：编号圆点、选中打勾、「(Recommended)」显示成紫色「推荐」但回答发原文、「其他回答…」最后一行点了才变输入框；单题单选点一下就提交；多题顶部「标题 · 1/2」、底部上一题 / 下一题 / 提交，单选后自动翻页）；**计划**（Markdown 预览 + 全屏；批准并自动执行 / 自动接受编辑 / 打回写意见）；**授权**（工具名 + 参数；拒绝 / 允许 / 本会话总是允许）；`prompt_changed` 时提示「终端上的对话框已经变了」 | `Jarvis/Views/Interactions/InteractionViews.swift`（InteractionPanel、QuestionForm、PlanForm、PermissionForm）、`AppModel.resolve` | — | 同；这是产品核心价值（在手机上拍板），必须原生、顺手 | M | P0 |
| 7.31 | 终端等你处理（ScreenForm） | hook 没接住的提问 / 权限确认等：从屏幕读出对话框（☐ 标题、│ 引用的问题、编号选项及其说明、Type something、Chat about this、多题的步骤条）排成提问卡「Claude 在问你」；点选项发编号键；「其他回答…」先按 Type something 的编号再输入并回车；「改在对话里说」；需要密码时用安全输入框（不保存）；原始终端 + 按键栏 + 输入框收在「查看终端」；读不出对话框就直接显示终端。Codex / TRAEX 等确认全靠它 | `InteractionViews.swift`（ScreenForm、ScreenSteps、TerminalText）、`Shared/ChatStore.swift`（ScreenParser）、`ios/JarvisTests/ScreenDialogTests.swift` | — | 解析器 1:1 移植（连同测试用例） | M | P0 |
| 7.32 | 完成触感 / 已看 | 打开对话页即上报 seen（WS `seen`，离线走 HTTP），乐观把完成未看改成已看；看着的会话做完时 success 触感 | `AppModel.markSeen / notifyTransitions`、`docs/agent-states.md`「看过」 | — | 同 | S | P0 |
| 7.33 | 文件 / Git 入口 | 没有选项卡：对话往左划 → 文件 → Git（往右划回），或 ⋯ 菜单「文件」「Git（改动数）」；返回键先回到对话；Git 只在仓库里出现，文件在知道目录时出现；助手对话没有 | `ChatView.swift`（swipeablePanes、panes）、`MessageMenu.swift`（SwipeTo） | UIKit 横扫手势（让出滚动） | `HorizontalPager`（要处理和代码横滑、系统返回手势的冲突），或顶部分段切换 | M | P1 |
| 7.34 | 文件页 | 从会话目录开始逐级进入，面包屑跳回任意一级；目录在前、显示大小和修改时间、Git 改动标记（目录里有改动显示圆点）；隐藏文件默认不显示（菜单切换）；点文件用查看器打开 | `Jarvis/Views/Session/FilesPane.swift`、`docs/ios.md`「会话页的终端、文件、Git」 | — | LazyColumn + 面包屑 LazyRow | M | P1 |
| 7.35 | 文件查看器 | 在会话所在机器上读取（`GET /machines/{mid}/file`，最多 2 MB，拒绝 iCloud 目录）：Markdown 渲染（机器上的相对路径图片、常见 HTML 标签转 Markdown）、代码带行号不换行双向滚动、图片、PDF、目录浏览；拷贝路径 / 内容、分享 | `FileViewer.swift`（FileViewer、CodeText、PDFKitView、MachineImageProvider）、`Jarvis/Views/Chat/MonoTextView.swift` | PDFKit；UIScrollView 里放不滚动的 UITextView（避开 TextKit 2 强制折行） | `PdfRenderer`（或 androidx.pdf）；等宽文本用 `horizontalScroll` + `verticalScroll` / 按行 LazyColumn（Compose 没有 TextKit 的折行坑） | M | P1 |
| 7.36 | Git 页 | 分支、上游 ↑↓、暂存数；改动列表（状态字母、+/− 行数、已暂存 / 部分暂存）→ diff（新旧行号、红绿底、不换行横滑，同时有暂存和未暂存时可切换）；提交历史（分页加载更早）→ 提交说明 + 每个文件的 diff；全部只读 | `Jarvis/Views/Session/GitPane.swift` | MonoTextView | 同 7.35 的等宽大文本组件 | M | P1 |
| 7.37 | 链接在 App 内打开 | 对话里的 http(s) 链接都拦截到 App 内链接浮层（11 节），飞书文档 / 公司圈走原生阅读器 | `ChatView.swift`（`openURL` 拦截） | `OpenURLAction` | 统一 `LocalUriHandler` 拦截 | S | P0 |

## 8. 终端（对话页右上角 / 收起细条 / 工作行的终端按钮）

| # | 功能 | 一句话描述 | 关键文件 / 出处 | iOS 专有能力 | Android 对应做法 | 复杂度 | 优先级 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 8.1 | 终端盖层 | 从底部弹出盖住整页（无标题、无选项卡），顶上一条细栏（左：尺寸 / 换行；中：把手；右：设置），按住细栏下拖跟手、拖过 140pt 或快速下划关闭、点把手关闭；终端尺寸只随键盘变（换尺寸 = 给接管的窗格改尺寸） | `ChatView.swift`（TerminalCover）、`SessionPanes.swift`（TerminalPanel、grabBar、TerminalGrab）、`docs/terminal.md` 十四 | `fullScreenCover` + 透明背景 | 全屏 Dialog 或独立 Activity（`imePadding`）；拖动关闭自绘 | M | P1 |
| 8.2 | 实时流 + 降级 | hub 有 `terminal_stream` 时订阅 herdr observe 帧（WS `terminal` 事件），套到窗格尺寸的网格；丢帧 / 换流从整帧重来；重连带 epoch / seq 续上；不能推流时每秒读一次带颜色的屏幕（`format=ansi`） | `Jarvis/Views/Session/Terminal/TerminalFeed.swift`、`docs/terminal.md` 一、二、五 | — | 同协议；`TerminalFeed` 移植 | M | P1 |
| 8.3 | VT 子集仿真 | 自写解析：CUP、ED(2J)、SGR（16 / 256 / 真彩、粗体、下划线、反显）、DECTCEM、同步输出 2026、OSC 8 链接；宽字符两格（右半格单独写，避免「字消失」）；未知序列忽略不崩；接管时按帧比对网格自己攒滚动历史 | `Terminal/TerminalGrid.swift`、`ios/JarvisTests/Fixtures/terminal-*.jsonl`、`docs/terminal.md` 三、九、十一 | — | Kotlin 移植（约 400 行）+ 复用同一批真实帧夹具做单测；输入是 herdr 重画好的网格，不需要完整终端仿真器 | M | P1 |
| 8.4 | 渲染 | 每个字符画在自己的格子（宽字符两格，放大到两格的约 92%）；框线 / 块字符画成线和矩形（同格合成一条路径描边）；⏺ 等符号加 U+FE0E 取文字形式；光标（直接打字时实心块，否则空心框）；只画到最后一行有字处或光标处；JetBrains Mono（打包，OFL）/ SF Mono；行高 ceil(字号 × 1.42) | `Terminal/TerminalView.swift`（TerminalScrollView、TerminalCanvas、CellMetrics）、`Terminal/BoxDrawing.swift`、`docs/terminal.md` 五、六 | UIView 自绘、CoreText、`CTFontManagerRegisterFontsForURL` | 自定义 View（`Canvas.drawText` + 字形缓存）或 Compose `Canvas` + `TextMeasurer`；JetBrains Mono 放 `res/font`，系统等宽字体兜底 | L | P1 |
| 8.5 | 手机宽度换行 | 默认按手机宽度折（在词之间、中文旁断；不在收尾标点前断；续行悬挂缩进到正文起点，竖框线重画；填充（背景空格、横跨的分隔线、Claude 的灰底）不折；光标行不截）；「不换行 · 左右滑」开关给 TUI 框线用 | `Terminal/TerminalWrap.swift`、`ios/JarvisTests/TerminalWrapTests.swift`、`docs/terminal.md` 八 | — | 纯逻辑移植 + 用例 | M | P1 |
| 8.6 | 手机尺寸接管 | 默认「手机尺寸」：订阅带手机的列 × 行（`terminal_control`），Claude 按手机宽度排版，键直接写进终端，表头「手机尺寸 47×35」；被别的程序占着先弹「接管 / 只看」，被抢走后显示「只看 · 接管」；离开、进后台、断线后窗格自动回到电脑尺寸；尺寸变化防抖 250ms；设置可切「电脑尺寸」镜像 | `TerminalFeed.swift`、`SessionPanes.swift`（sizeChip、gridChanged）、`AppModel.terminalSubscribe`、`docs/terminal.md` 十一 | — | 同；`onStop` 时退订 | M | P1 |
| 8.7 | 直接打字 | 终端视图自己当输入目标：拼音 marked text 画在光标处、选定后才发；听写；外接键盘方向键 / esc / ctrl+字母；还没发出的字退格本地删；`KeyStream` + `KeyBatcher` 同一时间一个请求、按序；接管时经 WS 直接写字节（方向键带修饰、ctrl 控制字符、alt 前缀 ESC、shift+enter、多行 bracketed paste） | `Terminal/TerminalInput.swift`、`SessionPanes.swift`（KeyStream）、`AppModel.terminalInput`、`docs/terminal.md` 七、十一 | `UITextInput`（`hasText` 恒真、marked text）、`keyCommands` | 自定义 View 覆写 `onCreateInputConnection`（`BaseInputConnection`：`setComposingText` / `commitText` / `deleteSurroundingText`）+ `onKeyDown` | L | P1 |
| 8.8 | 输入框模式 + 补全 | 设置里切「输入框」：写好一行回车整条发；补全条（之前在提示符后敲过的命令、屏幕上出现过的路径 / 名字，只用窗格内容，不读电脑的 shell 历史） | `SessionPanes.swift`（field）、`Terminal/TerminalExtras.swift`（TerminalCompletion、SuggestionStrip） | — | 同 | S | P2 |
| 8.9 | 键栏 | 照 Moshi 的一条深色胶囊：ctrl / alt / ⇧（单击粘滞、快速双击锁定、按住期间生效；也作用于输入框打的下一个字）、esc、tab、⌃C、快捷命令（闪电）、粘贴、方向键十字（游戏手柄图标浮出，带回车）、键盘按钮（双击 = 回车）；可选显示哪些键（ctrl 一直在）、「不打字时收起键栏」；放不下可横滑 | `Jarvis/Views/Chat/TerminalKeys.swift`（ModifierState、TerminalModifiers、TerminalKeyBar、TouchArea、KeyCap、DirectionPad） | UIView `touchesBegan / Ended`（SwiftUI 手势收不到快速双击的第二下） | `pointerInput` + `awaitPointerEventScope` 自己判断按下 / 抬起 / 双击 / 按住 | M | P1 |
| 8.10 | 快捷命令 | 闪电面板：单击填入（直接打字模式下是输入）、双击带回车执行；设置里增删、排序（默认 git status、git diff --stat、ls -la、/compact、/status） | `TerminalExtras.swift`（ShortcutsPanel、TerminalPrefs） | — | 同 + 可拖拽排序列表 | S | P2 |
| 8.11 | 手势 | 点终端 = 在那一格左键单击（`terminal_click`，如 Claude 的「Jump to bottom」）；上下拖 = 滚轮（`terminal_scroll`，落在手指按下那格、每滑过一行一个事件，惯性也算）；捏合调字号 7–20；双指左右切同空间的上 / 下一个 Agent、上下切同机器的相邻空间；双击粘贴剪贴板（多行先确认）；长按选词 + 拖动扩选 + 系统样式把手 + 拷贝 / 全选菜单（摆在选区旁）；点 OSC 8 链接或网址在 App 内打开 | `TerminalView.swift`（TerminalScrollView、SelectionHandle）、`SessionPanes.swift`、`docs/terminal.md` 十二、十五、十六 | `UIEditMenuInteraction`、多个 UIGestureRecognizer 协作 | `pointerInput` + `detectTransformGestures` + 自绘选区把手 + 自定义浮动菜单 | L | P1 |
| 8.12 | 历史 | 镜像时往上滑到顶附近自动加载前 200 行（`POST …/screen/before`，按「最上面 20 行 + 已加载行数」定位，herdr 约 1000 行封顶，到头提示「更早的过程在『对话』里」）；看历史时屏幕停住、「回到最新」恢复；接管时手指滑动交给 herdr 的滚轮 | `TerminalFeed.swift`、`SessionPanes.swift`（historyStatus、bottomOverlay） | — | 同 | M | P2 |
| 8.13 | 链路状态条 | 连不上 / 流停 / 读屏失败 / 超过 4 秒没新屏幕时顶部状态条：「上次更新 N 秒前」+「N 秒后重试 · 原因」+「重试」；退避 1 / 2 / 4 / 8 秒后每 15 秒；恢复后消失（旧内容不能看起来像还在刷新） | `SessionPanes.swift`（TerminalLinkStatus、TerminalFeedLink） | — | 同 | S | P1 |
| 8.14 | 终端设置 | 尺寸（手机 / 电脑）、配色（墨黑 / 深夜 / Persona / Solarized，带预览）、字体、字号、换行、输入方式（直接打字 / 输入框）、快捷命令编辑、键栏显示哪些键、不打字时收起键栏、手势说明；首次打开出「终端小技巧」 | `TerminalExtras.swift`（TerminalSettingsView、TerminalPreview、ThemeRow）、`Terminal/TerminalTheme.swift`、`SessionPanes.swift`（TerminalTips） | Form、`@AppStorage` | 设置页 + DataStore | S | P2 |
| 8.15 | 无终端 | 会话不在 herdr 里运行时显示「没有终端：恢复会话后就能看到」 | `SessionPanes.swift`（screen） | — | 同 | S | P1 |

## 9. 「收件箱」tab（飞书 + 邮件 + 等你的 Agent）

| # | 功能 | 一句话描述 | 关键文件 / 出处 | iOS 专有能力 | Android 对应做法 | 复杂度 | 优先级 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 9.1 | 统一列表 | 飞书消息和各邮箱邮件在一个列表，一行一件事（一个飞书会话的新消息 / 一个邮件线程的未读）；hub 采集 + AI 分拣，`inbox` 事件到了再拉；本地缓存冷启动先画；下拉刷新 = 立即采集 | `Jarvis/Views/Inbox/InboxView.swift`、`Jarvis/App/InboxStore.swift`、`Shared/InboxModels.swift`、`docs/inbox.md` | — | LazyColumn + Room 缓存 + `PullToRefreshBox` | M | P0 |
| 9.2 | 分段 | 需要你（展开）/ 知悉即可（展开，段头「全部看过」）/ 已折叠（噪音，收起，「全部清除」）/ 已处理（收起，可「移回」，保留 3 天）；段标题点一下展开 / 收起；分拣前的事算「需要你」；tab 角标 = 未处理的「需要你」件数 | `InboxView.swift`（list、sectionHeader） | — | LazyColumn + 可折叠段头 | S | P0 |
| 9.3 | 等你的 Agent 并入「需要你」 | 待处理交互（提问 / 计划 / 授权 / 终端等你，含助手的 ask_user；屏蔽的除外）排在「需要你」最前（圆角方块红底类型图标 + 哪个 Agent + 问什么 + 红字「Claude 在问你 · 机器」），点开跳到那个对话（助手的去 Persona tab）；只在 App 端合并 | `InboxView.swift`（waitingAgents、agentRow、InboxAgentRow） | — | 同 | S | P0 |
| 9.4 | 筛选 + 搜索 | 一行芯片：全部 / 飞书 / 每个邮箱 ｜ @我 / 私聊 / 群聊 / 邮件 / Agent（记住）；搜标题、发件人、内容、摘要 | `InboxView.swift`（filters、matches） | `.searchable`、`@AppStorage` | FilterChip 横排 + SearchBar + DataStore | S | P0 |
| 9.5 | 行 | 头像 40（右下角 18pt 来源小标：飞书蓝 / 邮件灰）、标题 + 类型标签（@我 红）、时间；有摘要时「✦ 摘要」+ 小字「发件人：内容」；件数徽章（@我 主色）；多邮箱时显示账号；分隔线从文字列开始；飞书头像色板不用红橙 | `InboxView.swift`（InboxRow） | — | 同 | S | P0 |
| 9.6 | 已处理 | 左滑到底 = 已处理（已处理的左滑 = 移回）；先改本地再发请求，失败恢复；只记在 Persona，不回写飞书已读 / 邮件已读 | `InboxView.swift`（swipeActions）、`InboxStore.setDone` | `swipeActions(allowsFullSwipe)` | `SwipeToDismissBox`（单向满滑正好对得上） | S | P0 |
| 9.7 | AI 分身草稿卡 | 行下「AI 分身草拟的回复」卡片：一键「发送」（hub 在原会话发飞书 / 按回复发邮件，并标已处理）、「修改」打开草拟浮层；处理中显示「AI 分身正在处理：<任务>」/「排队中」 | `InboxView.swift`（InboxDraftCard）、`docs/inbox.md`「AI 分身」 | — | 同 | S | P1 |
| 9.8 | 详情页 | 飞书：会话页停在这件事的第一条新消息（10 节）；邮件：打开最新一封（12.1）；导航栏下操作条「✦ 摘要 + AI 草拟回复（有草稿时『查看草稿』）+ 交给 AI」；右上「已处理」（处理后返回）/「移回」 | `InboxView.swift`（InboxDetailView）、`Jarvis/Views/Inbox/InboxActions.swift`（InboxActionBar） | `safeAreaInset(.top)` | 顶部吸附操作条 | S | P0 |
| 9.9 | AI 草拟回复 | 半屏浮层：hub 小模型按你的口吻起草（不知道的事实用【】标出）；可直接改；「让 AI 改：语气、要点…」+「重写」；有 AI 分身草稿时上方显示它的依据和「查看 AI 分身的处理过程」（打开那段助手对话）；「发送」走 `/inbox/{id}/reply` 并标已处理 | `InboxActions.swift`（InboxDraftSheet） | `TextEditor`、detents | ModalBottomSheet + 多行 TextField | S | P1 |
| 9.10 | 交给 AI | 浮层：要它做什么（默认「总结一下，告诉我要做什么，给出建议的回复」+ 3 个预设）；交给谁（Persona 新对话 / 最近 8 个 Agent）；「同时标为已处理」；把这件事作为 `<jarvis-context>`（飞书 feedId / 邮件 uid，便于工具读更多）发出并跳过去 | `InboxActions.swift`（InboxHandOffSheet） | inline Picker | ModalBottomSheet + RadioButton 列表 | S | P1 |
| 9.11 | 写邮件 / 管理邮箱入口 | 右上 ⋯：写邮件、管理邮箱（12 节） | `InboxView.swift` toolbar | — | 同 | S | P1 |

## 10. 飞书会话页与消息 / 卡片渲染（照搬 lark-connector）

> **调整**（2026-10-30）：10.5 里飞书视频、语音的 App 内播放，Android 不做：显示成卡片（封面、时长），点开交给飞书；图片、文件照做。iOS 保留现有的播放（这一轮 iOS 不改）；以后 iOS 跟进公网直连、做证书钉扎时，改成先下载到本地再播，因为 AVPlayer 走系统自己的网络栈，接不上钉扎（2026-10-30 定）。

| # | 功能 | 一句话描述 | 关键文件 / 出处 | iOS 专有能力 | Android 对应做法 | 复杂度 | 优先级 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 10.1 | 会话页 | 像飞书：有未读时停在第一条未读上方的「以下为新消息」线，否则在最新；上翻「加载更早的消息」，下翻自动加载更新的；同人 5 分钟内合并头像 / 名字；日期分隔；「N 条新消息」回到最新按钮；「有人@你」跳转；只把上过屏的消息上报已读（离开时 flush）；缓存先画（覆盖到第一条未读时直接用缓存）+ 实时事件刷新 + 60 秒轮询兜底；导航副标题「飞书 / 飞书话题群」 | `Jarvis/Views/Inbox/Lark/LarkChatView.swift`、`docs/ios.md`「收件箱」、`CLAUDE.md` 经验（aroundPosition 规则） | `defaultScrollAnchor`、`onScrollGeometryChange` | LazyColumn + 自定义锚定（打开到某条用 `scrollToItem(index)`）；可见项上报已读 | L | P0 |
| 10.2 | 回复 / 总结 | 底部输入框直接回复这个飞书会话（发送按钮）；右上「让 Persona 总结」（发主对话） | `LarkChatView.swift`（composer、askJarvis） | — | 同 | S | P0 |
| 10.3 | 气泡与布局 | 所有人（包括自己）的消息都在左侧带头像；群聊里连续消息第一条显示名字 + 机器人标；自己的气泡蓝色（跟风格走）；回复引用条；已编辑标记；卡片 / 图片 / 文件无气泡；系统消息 | `LarkChatView.swift`（LarkMessageRow、LarkReplyQuote）、`docs/ios.md`「飞书消息渲染」 | — | 同 | M | P0 |
| 10.4 | 富文本与行内 | 富文本块树（段落、有序 / 无序列表、引用、标题、代码块轻量高亮）；行内 run（粗 / 斜 / 删除线 / 行内代码 / 链接 / 术语虚线 / 内嵌表情图）拼成一个 Text，表情图未加载时先显示 `[标签]`；卡片 `plain_text` 解 `&nbsp;`、`<br>`、`<a>`、`<font color>` | `Lark/LarkMessageContent.swift`、`Lark/LarkInline.swift`、`Lark/LarkModels.swift` | `Text(Image(...))` 行内图片拼接 | `AnnotatedString` + `InlineTextContent`（`appendInlineContent`）放表情图 | L | P1 |
| 10.5 | 媒体 | 带鉴权的图片加载（内存 + 磁盘、ImageIO 降采样、hub 缩略图 24px 一档）；全屏图集（缩放、滑动、保存、分享、拷贝）；视频 / 语音播放（经 hub、带 bearer、转发 Range）；文件下载后 Quick Look 预览（可分享 / 存到文件）；贴纸 | `Lark/LarkMedia.swift`（LarkMedia、LarkRemoteImage、LarkImageGallery、LarkPlayerView、LarkFileOpener） | ImageIO、AVPlayer、Quick Look | Coil（OkHttp 鉴权拦截器）；Media3 ExoPlayer（`DefaultHttpDataSource` 带 header）；下载 + FileProvider + `ACTION_VIEW`（或内置 PDF / 图片预览） | M | P1 |
| 10.6 | 消息卡片 | 卡片元素：header 主题色、markdown、column_set（权重 / 固定 / auto 列宽 + 窄屏 bisect / trisect / stretch 重排）、table、list、collapsible_panel、note、text_tag、icon、按钮（链接 / 回调；回调只在 `actionMethod == 2` 时可按，走 card-actions）、select / input / picker（只读显示当前值）、overflow、img、chart（Swift Charts） | `Lark/LarkCard.swift`（1100 行）、`docs/ios.md`「飞书消息渲染」 | 自定义 `Layout`（注意 proposal 宽度可能是无穷）、Swift Charts | Compose `Layout` / `SubcomposeLayout`（宽度无界时判 `hasBoundedWidth`）；图表用 Vico 或 Canvas | L | P2 |
| 10.7 | 摘要卡 | 合并转发「聊天记录」（点开看全部消息）、群名片 / 个人名片、视频会议（拷贝会议号）、日程、投票、任务、红包等 | `Lark/LarkSummaryCards.swift`（LarkMergeForwardCard、LarkForwardedView、LarkShareCard、LarkGenericSummaryCard） | — | 同 | M | P2 |
| 10.8 | 表情回应 | 回应条（表情图 + 谁 / 几个），点一下切换自己的回应；「☺︎」或长按「表情回复」打开常用 + 全部表情目录（分组） | `Lark/LarkReactionsMoments.swift`（LarkReactionPicker、LarkEmojiImage）、`LarkChatView.swift`（LarkReactionChip） | — | ModalBottomSheet + LazyVerticalGrid | M | P2 |
| 10.9 | 长按菜单 | 常用表情一排、回复、表情回复、拷贝、查看 <某人> 的资料；消息和话题卡片同一个菜单 | `LarkChatView.swift`（LarkMessageMenu） | `contextMenu` | 长按 DropdownMenu | S | P1 |
| 10.10 | 资料卡 | 点头像或名字：封面、头像、名字、状态、签名、「发消息」（打开 / 建私聊）、信息行（前 5 行 +「查看更多信息」，可拷贝），上级行打开上级的资料卡 | `Lark/LarkPeople.swift`（LarkProfileView） | sheet | ModalBottomSheet | M | P2 |
| 10.11 | 话题群 | `is_topic` 的群一个话题一张卡片（头像、名字、时间各自带着，高过 320 pt 收起成「展开」，露出最新两条回复和「查看更早 N 条回复」，底部「表情回复」「回复」，#1087），点正文或回复打开「话题详情 · 来自 群名」看全部回复；底部输入框发的是新话题（占位「发布话题」） | `LarkChatView.swift`、`LarkTopicCard.swift`（LarkThreadView） | — | 同 | M | P2 |
| 10.12 | 公司圈分享 | 消息里的公司圈分享链接渲染成预览卡（标题、作者、摘要、计数），点开看帖子全文、图片、回应和评论（没有 feed） | `Lark/LarkReactionsMoments.swift`（LarkMomentsCard、LarkMomentsPostView）、`docs/feishu.md` | — | 同 | M | P2 |
| 10.13 | 飞书路由 | 链接分流：文档 → 原生阅读器，公司圈帖子 → 帖子页，其余 → 网页；`FeishuRoute` 只有 chat / doc / profile / momentsPost 四种落点（不做第二个飞书） | `Jarvis/Views/Feishu/Foundation/FeishuFoundation.swift`、`docs/feishu.md` | — | 同 | S | P1 |

## 11. 飞书文档阅读器 / 链接浮层 / WebView 池

| # | 功能 | 一句话描述 | 关键文件 / 出处 | iOS 专有能力 | Android 对应做法 | 复杂度 | 优先级 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 11.1 | 链接浮层（Telegram 式） | 会话里的 http(s) 链接在 App 内底部卡片打开：打开即铺满，下拉到 60% 时后面的对话照常可滚动、可输入，再下拉关闭；头部标题 + host、⋯（拷贝链接 / 在 Safari 打开 / 分享）、关闭；飞书文档头部多一个「整篇作为上下文」 | `Jarvis/Views/Link/LinkSheet.swift`（LinkSheet）、`CLAUDE.md` 约定「链接在 App 内打开」 | `presentationDetents([.fraction(0.6), .large])` + `presentationBackgroundInteraction` | `ModalBottomSheet` 不能让背后可操作：用 `BottomSheetScaffold`（非模态）或自绘 `AnchoredDraggable` 两档锚点；备选 Partial Custom Tabs（可设高度、背后可交互，但加不了自定义选区菜单） | L | P1 |
| 11.2 | WebView 池 | 每条回复出现时预加载前 3 个链接（只给 15 分钟内的新回复，翻历史不预加载），最多常驻 6 个 WKWebView（LRU），共用磁盘网站数据（缓存、登录态），请求 `returnCacheDataElseLoad`，打开时直接拿已加载好的视图 | `LinkSheet.swift`（WebViewPool、PooledWebView） | WKWebView、`WKWebsiteDataStore.default()` | Android WebView（CookieManager 进程级共享）；WebView 更吃内存，池子建议 2–3 个；预加载放到空闲时 | M | P2 |
| 11.3 | 选中文字 → AI | 网页里选中文字，系统菜单多「AI 修改」（填修改要求后立即发给 Agent）和「作为上下文」（输入框上方出现上下文标签，随下一条消息发）；消息里是 `<jarvis-context kind="context\|edit" …>` | `LinkSheet.swift`（SelectableWebView、ContextChips、ContextRef）、`ChatView.swift`（Composer.sendText） | 覆写 `buildMenu(with:)` + JS 取选区 | WebView 子类覆写 `startActionMode(callback, type)` 包一层加菜单项 + `evaluateJavascript("getSelection()")` | M | P1 |
| 11.4 | 飞书文档原生阅读器 | 任何地方点飞书文档 / 知识库链接都原生渲染：Markdown 导出，导出被禁时用 JSON 块树（段落、标题、列表跨兄弟连续编号、引用、callout、分栏、代码、公式、@人（点开资料卡）、表格合并单元格、宽表横滑 + 渐隐、链接卡片）；AI 摘要卡（可展开）；「N 条评论」（未解决 / 已解决两页，引用原文 + 回复）；目录（按标题层级，点跳转）；在飞书中打开 / 拷贝链接 / 分享 / 刷新；文档过长提示只显示前面部分；多维表格 / 画板 / 文件：说明 + 预览 + 在飞书中打开；`authority.copy=false` 时不可选；加载骨架屏；图片走 hub 可点开图集 | `Jarvis/Views/Feishu/Docs/FeishuDocReader.swift`、`FeishuDocRenderer.swift`、`FeishuDocsModels.swift`、`FeishuDocsComponents.swift`、`ios/JarvisTests/FeishuDocsTests.swift`、`docs/feishu.md`「文档」 | MarkdownUI、NSAttributedString | Markdown 渲染 + 自写 JSON 块渲染（Compose）；表格自写 | L | P1 |
| 11.5 | 文档段落 → AI | 在链接浮层里的原生文档：长按段落「AI 修改这段 / 这段作为上下文 / 选择文字… / 拷贝」；「选择文字」开可任选的文本页，选区菜单同样带 AI 修改 / 作为上下文 | `LinkSheet.swift`（DocPickMenu、PickTextSheet、PickTextView）、`FeishuDocRenderer.swift` | UITextView 选区菜单 | `SelectionContainer` + 自定义 `TextToolbar`（可加菜单项） | M | P2 |

## 12. 邮件

| # | 功能 | 一句话描述 | 关键文件 / 出处 | iOS 专有能力 | Android 对应做法 | 复杂度 | 优先级 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 12.1 | 邮件详情 | 主题、发件人（首字头像）、收件人、时间；纯文本优先，否则 HTML（禁 JS、自适应高度、链接外部打开、深色适配）；附件只列文件名和大小（不能下载）；打开即标已读；底栏「让 Claude 处理」（新 Agent 对话预填 mail_read 指令）+ 回复 | `Jarvis/Views/Mail/MailViews.swift`（MailDetailView、HTMLView、NewChatPrefilled）、`docs/mail.md`「已知限制」 | WKWebView（JS 关闭） | WebView（`javaScriptEnabled=false`）测高；底部 `BottomAppBar` | M | P0 |
| 12.2 | 写邮件 / 回复 | 发件人（账号选择）、收件人（逗号分隔）、抄送、主题、正文；回复自动「Re:」、In-Reply-To / References、引用原文前 40 行 | `MailViews.swift`（ComposeView、ComposeDraft） | Form | 表单页 | S | P1 |
| 12.3 | 邮箱管理 | 列表（Gmail / 飞书 / IMAP 图标、地址、状态或错误），左滑删除，添加 | `MailViews.swift`（MailAccountsView） | `onDelete` | LazyColumn + 滑动删除 | S | P1 |
| 12.4 | 添加邮箱 | 「AI 一键添加」：整段粘贴（或「从剪贴板识别」）账号 / 密码 / 服务器信息，信息齐全直接连接添加，缺什么填进表单；类型：Gmail（「使用 Google 账号登录」OAuth，不要密码）/ 飞书（只填邮箱 + 第三方客户端密码，服务器固定）/ 其他 IMAP（按域名自动填服务器，可手填 IMAP / SMTP） | `MailViews.swift`（AddMailAccountView）、`docs/mail.md` | `ASWebAuthenticationSession`（拦截 `com.googleusercontent.apps.<id>:/oauth2redirect` 回调） | Gmail 的「使用 Google 账号登录」**Android 上不做**：Google 的登录接口依赖 Google Play 服务（README D8、android.md 第 6 节）。邮箱账号存在 hub 上、两端共用：在 iPhone 上加一次，或者用应用专用密码按普通 IMAP 加；飞书和其他 IMAP 同 iOS | S | P1 |

## 13. 设置 tab（各子页）

| # | 功能 | 一句话描述 | 关键文件 / 出处 | iOS 专有能力 | Android 对应做法 | 复杂度 | 优先级 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 13.1 | 头部 | App 图标、hub 地址（等宽）、连接状态点（已连接 hub / 等待网络 / 连接中…） | `Jarvis/Views/Settings/SettingsView.swift` | — | 同 | S | P0 |
| 13.2 | 界面风格 | 5 张预览卡（每张画成迷你屏幕，3 列网格），点即整个 App 切换（RootView 以风格为 id 整棵重建，当前 tab / 对话记住） | `SettingsView.swift`（StylePicker、StyleCard）、`Shared/Theme.swift`、`docs/ios.md`「设计语言」 | — | 同；见 22 节 | S（框架）/ M（每套风格） | P2 |
| 13.3 | 助手 | 进资料页（4 节），行尾显示当前启动器 | `SettingsView.swift` | — | 同 | — | P1 |
| 13.4 | Claude Code 设置 | 选机器（只列在线的）+「同时改所有在线机器」（默认开）；白名单项（离开回顾 recap、思考强度、总是思考、回复语言等）按类型渲染：开关 / 枚举 / 文本，标「默认」；改的是那台机器的 `~/.claude/settings.json`，失败逐台列出 | `SettingsView.swift`（ClaudeSettingsView） | — | 同 | S | P2 |
| 13.5 | 机器 | 机器卡片列表 → 机器详情；右上仪表图标 → 机器监控（14 节） | `Jarvis/Views/Machines/MachinesView.swift`（MachinesView、MachineCard） | — | 同 | S | P1 |
| 13.6 | 设备 | 每台设备一条凭据（iPhone / 手表 / 其他）：名称、「本机」、「只能回答」、最近使用；逐台撤销（撤销本机 = 退出配对）；已撤销列表 | `Jarvis/App/DeviceCredentials.swift`（DevicesView） | — | 同 | S | P1 |
| 13.7 | 手机数据 | 9 个数据源（通讯录 / 日历 / 提醒 / 位置 / 健康 / 照片 / 设备 / 剪贴板 / 通知）各一行：图标、说明、授权状态（点此授权 / 已授权 / 部分授权 / 已拒绝 · 去设置）、开关；关掉的源不再注册给 hub（模型看不到） | `SettingsView.swift`（PhoneDataView）、`Jarvis/PhoneTools/PhoneAccess.swift`、`PhoneToolRegistry.swift` | 各框架授权 API | 运行时权限 + `ActivityResultContracts.RequestPermission`；「去设置」跳 `ACTION_APPLICATION_DETAILS_SETTINGS` | S | P1 |
| 13.8 | 通知 | 系统没开通知时的提示 + 跳系统设置；「需要你处理时」开关；「Agent 做完时」不推送 / 只推长任务 / 每次都推（说明等多久、长任务多长，来自 hub）；实时活动：锁屏和灵动岛、锁屏显示详情、灵动岛两侧（13.9）；系统里关了实时活动时提示；全部按设备存在 hub（`/devices/{id}/prefs`），乐观更新失败回滚 | `Jarvis/Views/Settings/NotificationSettingsView.swift` | ActivityKit 授权状态 | 通知渠道（NotificationChannel）+ hub 偏好；实时活动项换成「常驻通知 / Live Updates」开关 | S | P0 |
| 13.9 | 灵动岛设置 | 顶部两张预览（平时 / 有 Agent 需要你时）；预设 A（状态数量）/ B（每分钟 token）/ C（今日 token）/ 自定义（左右各选：状态数量 / 每分钟 token / 今日 token / 今日请求数 / Persona 助手 / 机器负载 / 最久那一轮的计时 / 不显示）；负载可选机器（默认最忙那台） | `Jarvis/Views/Settings/IslandSettingsView.swift`、`Shared/FleetState.swift`（IslandSlot、IslandCell）、`docs/live-activity.md`「灵动岛」 | Dynamic Island | 无灵动岛：换成「常驻通知 / 状态栏胶囊显示什么」的设置（18 节） | S | P2 |
| 13.10 | 同步到 Apple Watch | 手动把配对信息和摘要推给手表 | `SettingsView.swift`、`Shared/WatchSync.swift` | WatchConnectivity | Wear OS 才需要（20 节） | — | ✕ |
| 13.11 | 存储空间 | 显示 App 在本机占用（15 节） | `Jarvis/Views/Settings/StorageView.swift` | — | 见 15 节 | — | P2 |
| 13.12 | 服务器 | 重新配对（扫码 / 手动）、断开并忘记服务器（确认；同时结束实时活动、清凭据） | `SettingsView.swift`、`AppModel.unpair` | — | 同 | S | P0 |
| 13.13 | 关于 | 版本号（版本 + build） | `SettingsView.swift` | — | 同 | S | P0 |

## 14. 机器（详情 / 监控 Dashboard / 曲线页）

| # | 功能 | 一句话描述 | 关键文件 / 出处 | iOS 专有能力 | Android 对应做法 | 复杂度 | 优先级 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 14.1 | 机器卡片 | 系统图标（`OSBadge`：macOS 苹果标，Ubuntu 画出来的「朋友圈」，Arch / Alpine / NixOS 相近符号，Debian / Fedora 等发行版颜色字母，按 `ID_LIKE` 兜底，不用位图）、名称、主机名、在线 / 离线；在线时「运行中 / 工作中 / 需要你」三个数、系统 / herdr / claude 版本标签、「这台电脑上的 Persona 和 hub 不是同一次部署」警告；离线时显示错误 | `Jarvis/Views/Machines/MachinesView.swift`（MachineCard）、`Jarvis/Views/Machines/OSBadge.swift`、`docs/machines.md`「系统信息」 | SF Symbols + 自绘 | Compose Canvas / VectorDrawable 画系统图标 | S | P1 |
| 14.2 | 机器详情 | 卡片做段头；运行中的 Claude（不含助手对话，点开对话）；「系统与监控」；启动器列表（函数名 + 模型）；「重新连接」（请求 hub 重连后最多等 15 秒看是否上线，没连上给原因）；脚注说明是 hub 本机还是经 ssh 接入 | `MachinesView.swift`（MachineDetailView） | — | 同 | S | P1 |
| 14.3 | 监控 Dashboard | 「N/M 台在线 · K 台需要注意」；每台一卡：系统图标、名字、系统 · 核数 · 内存、在线（离线显示最后在线时间）、CPU / 内存 / 网络当前值 + 最近 1 小时迷你曲线；异常卡片加状态色边、标签用图标 + 文字（不单靠颜色）；入口：Agents 机器菜单、设置 › 机器 右上、机器详情 | `Jarvis/Views/Machines/MachineDashboardView.swift`、`Shared/MachineMetrics.swift`、`docs/machines.md`「App」 | Swift Charts | Vico（Compose 图表库）或 Canvas 自绘迷你图 | M | P2 |
| 14.4 | 机器曲线页 | 系统、在线和已运行时长；现在的 CPU / 内存 / 网络 / 负载 / 磁盘 / 交换；1 / 6 / 24 小时切换；CPU、内存、网络（下行 + 上行）、负载（虚线标核数）四张图，Linux 另有每核柱状；按住任一张图拖动看某一时刻，四张图共享选中时间、标题行显示那一刻的值；空档处断线（`gap` 点）；系统信息表；VoiceOver 描述 | `Jarvis/Views/Machines/MachineMonitorView.swift`、`Jarvis/Views/Machines/MetricCharts.swift`、`docs/machines.md`「图」 | Swift Charts（`series:` 分段、`chartXSelection`） | Vico 多段线 + 自定义 marker，或 Canvas 自绘 + `pointerInput` 共享选中时间 | L | P2 |
| 14.5 | 刷新与告警规则 | 页面在屏幕上且前台时轮询（Dashboard 10 秒；详情 1 小时范围 10 秒、其余 60 秒），进后台停；告警阈值：CPU ≥75 / 90%、内存 ≥80 / 90%、磁盘 ≥80 / 90%、负载 ≥1 / 2 倍核数；配色经 dataviz 校验（指标固定色，状态色只用于异常）；旧 hub 404 时提示升级 | `MachineMetrics.swift`（MetricAlert、MetricColor）、`docs/machines.md` | `task(id: scenePhase)` | `LaunchedEffect` + `repeatOnLifecycle`；同一套阈值 / 色板常量 | S | P2 |

## 15. 存储空间（设置 › 存储空间）

| # | 功能 | 一句话描述 | 关键文件 / 出处 | iOS 专有能力 | Android 对应做法 | 复杂度 | 优先级 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 15.1 | 按类别占用 | Agent 对话、对话里的图片、飞书图片和文件、列表和飞书会话、网页（不含登录）、网络缓存、其他缓存、App 数据（设置和配对，清除时保留）：大小 + 占比条；设置首页行尾显示总量 | `Jarvis/Views/Settings/StorageView.swift`（StorageKind、StorageUsage） | 扫描 Caches / Library | 扫 `cacheDir` / `filesDir` / Coil 缓存 / WebView 缓存目录；`StorageStatsManager` 取总量 | S | P2 |
| 15.2 | 清除 | 左滑 / 长按清除一类，或「清除全部缓存」；清除后内存副本一起丢，后台同步重新拉；网页只清缓存不清登录 | 同上、`docs/ios.md`「存储空间」 | WKWebsiteDataStore 按类型清 | `WebView.clearCache(true)`（保留 Cookie）+ 删除文件 + Coil `memoryCache.clear()` | S | P2 |

## 16. 连接状态（弱网 / 断网）与离线行为

| # | 功能 | 一句话描述 | 关键文件 / 出处 | iOS 专有能力 | Android 对应做法 | 复杂度 | 优先级 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 16.1 | 三种状态 | 等待网络（系统说没网）/ 连接中…（有网但 WS 没连上、在重连，或 ping 5 秒没回 / 回得慢 = 弱网）/ 更新中…（刚连上，待处理交互和打开的对话 backlog 都到了才算完，最多 8 秒）；没配对、App 在后台不算问题 | `Jarvis/App/ConnectionStatus.swift`、`docs/ios.md`「连接状态」 | `NWPathMonitor` | `ConnectivityManager.registerDefaultNetworkCallback`；同一状态机 | M | P0 |
| 16.2 | 去抖与恢复 | 变坏要持续 1.5 秒才显示（回前台 / 切网络的秒级重连不闪）；恢复立刻换回；弱网只有及时回来的 pong 能解除；网络恢复立刻重连（不等退避，最长 20 秒）；Wi-Fi ↔ 蜂窝切换马上 ping；45 秒没任何消息判死 | 同上、`Shared/LiveConnection.swift` | — | OkHttp 自定义应用层 ping（不要只依赖协议层 ping）；协程计时 | M | P0 |
| 16.3 | 显示位置（Telegram 式） | 不弹横幅：Agents 列表、助手首页、收件箱、设置 › 机器 的导航栏中间换成「小转圈 + 状态」；对话页展开的页头保留会话名、副标题换成状态，收起的细条整个换成状态；点状态弹说明（原因、上次失败、几秒后重试、上次连上、几条消息在等、「立即重试」） | `Jarvis/Views/ConnectionTitle.swift`（ConnectionTitle、ConnectionLabel、ConnectionDetails） | `ToolbarItem(.principal)` | TopAppBar 标题槽位放状态组件 + Popup / BottomSheet 说明 | S | P0 |
| 16.4 | 断网时的操作 | 发消息：不发请求，气泡带时钟「连上后自动发送」，重连且「更新中…」结束后按顺序发出（没打开的对话先拉一页去重），同一条带同一个 `client_msg_id`，存盘；批准 / 回答等一次性操作不排队：卡片原样不动，失败放回并给蓝色提示；离线 / 超时错误不显示在页面里；后台刷新失败什么都不弹 | `Jarvis/App/Outbox.swift`、`AppModel.resolve / notDone`、`SendSafety.swift` | — | Room outbox + 同规则 | M | P0 |

## 17. 推送与通知按钮

> **按 [README.md](README.md) D8 调整**（下表已按此改写）：调研时假设 Android 用 FCM。现在按「手机没有 Google 服务」设计，Android 只走常驻连接（25.4 的 N55），不做 FCM（N60）。所以：
> - 17.1 的 P0 变成「申请通知权限、建通知渠道、启动前台服务」，hub 不用改；
> - 17.6 不需要唤醒，工具调用直接送达；
> - 17.4 的「做完了」由 hub 经 WS 下发（N56）。

| # | 功能 | 一句话描述 | 关键文件 / 出处 | iOS 专有能力 | Android 对应做法 | 复杂度 | 优先级 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 17.1 | 注册推送 | 配对后申请通知权限（含 time-sensitive）并注册 APNs，`POST /api/v1/devices`（设备 id、名称、kind=iphone、token、sandbox / production、topic）；模拟器自动化不注册 | `Jarvis/App/JarvisApp.swift`（PushRegistrar）、`docs/ios.md`「数据流 · 推送」 | APNs | 不注册推送令牌（D8）：Android 13+ 申请 `POST_NOTIFICATIONS`、建通知渠道、启动前台服务常驻 WS；hub 不用改 | S | P0 |
| 17.2 | 交互推送 + 按钮 | 类别 `INTERACTION_<KIND>`：授权「允许 / 拒绝」，计划「批准 / 打回修改」（打回可直接输入意见）；说「是」的按钮要先解锁，「拒绝」不用；从通知批准计划用 `acceptEdits`（没看全计划）；提问、终端没有按钮，点开处理 | `Jarvis/App/NotificationActions.swift` | `UNNotificationCategory`、`.authenticationRequired`、`UNTextInputNotificationAction` | `NotificationCompat.Action` + `setAuthenticationRequired(true)`（API 31+）+ `RemoteInput` 直接回复；建议 hub 发 data 消息、由 App 自己组通知（才能带按钮、做前台抑制） | M | P0 |
| 17.3 | 后台处理按钮 | 按钮在后台直接调 resolve、不打开 App；失败（已被处理 410、连不上 hub）发一条本地通知说明原因 | `JarvisApp.swift`（resolveFromNotification）、`NotificationActions.reportFailure` | `beginBackgroundTask` | `BroadcastReceiver` + `goAsync()` 或 WorkManager 加急任务；失败再发一条通知 | S | P0 |
| 17.4 | 完成推送 | Agent 一轮做完（hub 判定：等 1 分钟、电脑或 App 里看过就不推、后台任务结束才推）推「✅ 标题」，副标题「机器 · 空间 › 标签」，正文是最后一条回复；偏好：需要你时开关、做完时（不推 / 长任务 / 每次） | `docs/ios.md`「推送」、`docs/server.md` 推送策略、`NotificationSettingsView.swift` | — | 同；按类型分 NotificationChannel（需要你 / 做完了 / Persona / 实时状态），用户可在系统里单独调 | S | P0 |
| 17.5 | 点通知打开 | 打开对应会话（助手的去 Persona tab）；冷启动时先设状态；前台且正在看该会话时不弹 | `JarvisApp.swift`（AppDelegate）、`AppModel.visibleSession` | `UNUserNotificationCenterDelegate`（必须用 completion handler 版本，否则冷启动崩） | `PendingIntent` → MainActivity extras → 导航；前台抑制在 App 组通知时判断 | S | P0 |
| 17.6 | 静默唤醒（手机工具） | 助手调 phone_* 而手机不在线：hub 发静默推送（`jarvis_wake`），App 在后台连上 WS 执行工具，25 秒后断开 | `JarvisApp.swift`（didReceiveRemoteNotification）、`docs/assistant.md`「手机不在前台」 | `content-available` 静默推送（会被限流，强退的 App 收不到） | 不需要唤醒：常驻连接直接收到工具调用（D8）；连接断了时 hub 照现有逻辑报「手机不在线」 | S | P1 |
| 17.7 | 助手推送 / 本地通知 | 助手 `notify_user` 推送（点开那段对话）；`phone_notify` 本地通知（可延迟 ≤ 24 小时） | `docs/assistant.md`、`Jarvis/PhoneTools/NotificationsTool.swift` | `UNNotificationRequest` 定时 | 助手的 `notify_user` 经 WS `notify` 事件（`kind=assistant`，hub-changes 3.9）发通知；`phone_notify` 的延迟通知用 AlarmManager（精确要 `SCHEDULE_EXACT_ALARM`）或 WorkManager（不精确） | S | P1 |

## 18. 锁屏 / 灵动岛实时活动「Fleet」

> **按 [README.md](README.md) D8 调整**（下表已按此改写）：前台服务那条通知就是 Fleet 汇总，数据来自 hub 已有的 WS `fleet` 事件，不需要 hub 改动；调研时写的「按设备类型发 FCM」不做（[android.md](android.md) 5.10）。

| # | 功能 | 一句话描述 | 关键文件 / 出处 | iOS 专有能力 | Android 对应做法 | 复杂度 | 优先级 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 18.1 | 汇总内容 | 整台手机一个活动：需要你（红，气泡 + 数）/ 工作中（琥珀，齿轮 + 数）/ 做完未看（绿）；条目按需要你（最早的在前）→ 工作中（最久的在前）→ 做完未看排，推送最多 3 条、界面画 2 条 +「+N 个」；已看、离线、屏蔽、等 /loop 的不算，助手只在问你时算；计时在手机本地走 | `ios/JarvisWidgets/FleetLiveActivity.swift`、`ios/LiveActivity/FleetIslandViews.swift`、`Shared/FleetState.swift`、`docs/live-activity.md` | ActivityKit + WidgetKit 扩展 | **没有灵动岛**：一条常驻通知承载同样的汇总；Android 16 QPR1 起可申请「Live Updates」（推广的常驻通知：通知栏顶部、锁屏突出位置、状态栏小胶囊，胶囊文字 `setShortCriticalText` ≤ 96dp 宽），老版本退化为普通常驻通知；不允许自定义 RemoteViews | M | P1 |
| 18.2 | 灵动岛两侧 / 展开态 | 紧凑态左右各一个数（最急的一类 / 下一类，或 TPM、今日 token、请求数、助手、机器负载、最久一轮计时）；最小态 = 最急那类；展开态：三类数量 + 选的数字 + 列表 + 按钮 | `FleetIslandViews.swift`、`FleetState.swift`（IslandCell.sides） | Dynamic Island | 状态栏小胶囊只放一个短文本（如「💬2」/「⚙3」）；其余放通知标题 / 正文；数字类指标（TPM 等）建议放进 App 内 Agents 页顶部而不是通知 | S | P2 |
| 18.3 | 锁屏详情开关 | 默认锁屏只显示数量和「有个授权等你」这类不带名字的一行；打开「锁屏显示详情」才显示 Agent 名和内容（按设备存 hub） | `docs/live-activity.md`、`NotificationSettingsView.swift` | — | Android 原生：通知 `VISIBILITY_PRIVATE` + `setPublicVersion(...)`（锁屏显示脱敏版本），系统设置也能控制 | S | P1 |
| 18.4 | 按钮 | 第一条能用按钮处理的请求带按钮：授权「拒绝 / 允许」、计划「查看 / 批准」；「允许 / 批准」要先 Face ID；内容过期（hub 15 分钟没推）不画按钮；点了乐观拿掉，失败发本地通知 | `ios/LiveActivity/FleetIntents.swift`、`Jarvis/App/FleetActions.swift` | `LiveActivityIntent`（在 App 进程里跑）、`authenticationPolicy` | 通知 Action + `setAuthenticationRequired(true)`；过期用 `setTimeoutAfter` / App 侧判断 | S | P1 |
| 18.5 | 推送与生命周期 | hub 推 `liveactivity`（新的「需要你」立刻推，其他合并 2.5 秒；数字类每台设备最多每分钟一次；stale 15 分钟、10 分钟续期；只剩做完未看 5 分钟后结束）；push-to-start（App 没开也能起活动）；前台时 App 用 WS `fleet` 事件自己更新 | `Jarvis/App/LiveActivityController.swift`、`docs/live-activity.md`「推送」「推送预算」 | APNs liveactivity push type、push-to-start token | 前台服务收 WS `fleet` 事件，更新同一条常驻通知（同一个通知 id）；hub 不用改（D8） | S | P1 |
| 18.6 | 点开路由 | 只有一个 Agent 时打开它，好几个时回到 App 原处；展开态每行打开各自的会话 | `docs/live-activity.md`「App」、`JarvisApp.swift`（FleetLink） | widgetURL / Link | 通知 contentIntent + 每条 action 的 PendingIntent | S | P1 |
| 18.7 | 手表智能叠放 | `supplementalActivityFamilies([.small])` 给 Apple Watch 智能叠放一个单行版本 | `docs/live-activity.md` | watchOS Smart Stack | Wear OS 自动桥接手机通知（含按钮）；单独的 Tile 要做 Wear 应用 | — | ✕ |

## 19. Siri / 快捷指令（App Intents）

| # | 功能 | 一句话描述 | 关键文件 / 出处 | iOS 专有能力 | Android 对应做法 | 复杂度 | 优先级 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 19.1 | 问 Claude | 给某台机器上最近的活会话发一句话，或按记住的目录 / 启动器新开一个（「新会话」）；机器参数可选（默认新建页记住的那台 → 第一台在线的）；Siri 再问「要跟 Claude 说什么？」；结果卡片带「打开会话」 | `Jarvis/Intents/AskClaudeIntent.swift`、`MachineEntity.swift`、`OpenSessionIntent.swift`、`IntentSupport.swift`、`docs/intents.md` | App Intents、AppEntity、Siri 对话 | 启动器长按快捷方式（`ShortcutManager`）+ 分享 / 输入页；语音入口可走 Google 助手 App Actions 或 Android 16 App Functions（实验性），首版不做 | M | P2 |
| 19.2 | Persona 状态 | 「N 个在工作，M 个等你处理」+ 第一个待处理的内容（后台，不开 App） | `Jarvis/Intents/JarvisStatusIntent.swift` | App Intents | 快速设置磁贴（`TileService`）显示需要你数量；或桌面小组件（Glance） | S | P2 |
| 19.3 | 批准第一个待处理 | 允许最早的一条授权，或批准计划（`bypassPermissions`）；提问 / 终端只回「需要打开 Persona 处理」 | `Jarvis/Intents/ApproveFirstIntent.swift` | App Intents | 磁贴点击 / 通知按钮已覆盖；单独做价值不大 | S | P2 |
| 19.4 | 短语与错误文案 | 3 条中文短语（Spotlight / Siri / 快捷指令 App 都能看到）；中文错误句子（没配对、配对失效、已处理过、连不上 hub、没有在线机器、该机器没活会话、没有待处理） | `Jarvis/Intents/JarvisShortcuts.swift`、`IntentSupport.swift`（IntentFailure） | `AppShortcutsProvider` | 错误文案可复用到磁贴 / 快捷方式 | S | P2 |

## 20. Apple Watch

| # | 功能 | 一句话描述 | 关键文件 / 出处 | iOS 专有能力 | Android 对应做法 | 复杂度 | 优先级 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 20.1 | 配对 | iPhone 经 WatchConnectivity 推配对信息（激活后再推 + transferUserInfo 保底）；手表未配对页「从 iPhone 同步」；手表用「只能回答」的凭据 | `ios/JarvisWatch/JarvisWatchApp.swift`（WatchPairView、WatchModel）、`Shared/WatchSync.swift`、`DeviceCredentials.swift` | WatchConnectivity | Wear OS Data Layer API（`DataClient` / `MessageClient`） | M | ✕ |
| 20.2 | 首页 | 「需要你」（等你的交互 + 收件箱里需要你的事，新的一件震一下）、「运行中」会话（状态点、标题、空间 › tab）；连不上 hub 时最上面「连不上 hub · 下面是 N 分钟前的数据」+ 重试；打开时每 8 秒直连 hub 刷新 | `JarvisWatchApp.swift`（WatchRootView） | watchOS SwiftUI | Compose for Wear OS（`ScalingLazyColumn`） | M | ✕ |
| 20.3 | 处理交互 | 提问（单题单选点即提交、多选 / 多题「提交」、推荐标签）；计划（批准并执行 / 打回）；授权（允许 / 拒绝）；终端（选项、Esc、⏎）；「交给电脑处理」 | `JarvisWatchApp.swift`（WatchInteractionView） | — | 同；**更省的替代**：Android 通知（带按钮和 RemoteInput）会自动桥接到 Wear OS 手表 | M | ✕ |
| 20.4 | 会话 / 收件箱 | 会话：状态、最近 6 条消息气泡、回复（可听写）、继续、中断，打开即标已看；收件箱事项：摘要、最新一条、「AI 回复」先起草再「发送」、「已处理」 | `JarvisWatchApp.swift`（WatchSessionView、WatchInboxItemView）、`Shared/WatchText.swift` | 听写 | Wear OS 语音输入 / RemoteInput | M | ✕ |

## 21. 手机数据工具（phone_*，给 Claude 用）

链路：Claude 调 `phone_*` → `personad mcp` → hub → 在线手机（WS `tool_call`）→ 本地执行 → `tool_result`。连上 hub 就 `register_phone` 声明打开的工具；写操作先在手机上确认；关掉的数据源模型看不到也调不了。

| # | 工具 | 一句话描述 | 关键文件 / 出处 | iOS 专有能力 | Android 对应做法 | 复杂度 | 优先级 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 21.0 | 注册 / 开关 / 授权 | `PhoneToolRegistry`：全部工具、按开关过滤、执行（永不抛错，失败变 `isError`）、授权状态与请求；写操作确认；模拟器自动化不注册 | `Jarvis/PhoneTools/PhoneToolRegistry.swift`、`PhoneTool.swift`、`PhoneToolSupport.swift`、`PhoneAccess.swift`、`docs/phone-tools.md` | — | 同结构（Kotlin 接口 + 注册表）；JSON schema 复用 | M | P1 |
| 21.1 | `phone_contacts_search` | 按姓名 / 公司 / 职位 / 邮箱 / 电话模糊搜，返回带标签的电话和邮箱、生日 | `ContactsTool.swift` | Contacts（可 limited） | `ContactsContract`（`READ_CONTACTS`） | S | P1 |
| 21.2 | `phone_calendar_events` | 默认今天到 7 天后：标题、起止、全天、日历名、地点、备注前 300 字、参与人 | `CalendarTool.swift` | EventKit 完全访问 | `CalendarContract.Instances` + `Attendees`（`READ_CALENDAR`） | S | P1 |
| 21.3 | `phone_calendar_create_event` | 新建日程（按名字选日历，默认日历兜底），需确认 | `CalendarTool.swift` | EventKit | `CalendarContract.Events` 插入（`WRITE_CALENDAR`） | S | P1 |
| 21.4 | `phone_reminders_list / create / complete` | 提醒事项列表 / 新建（优先级）/ 完成，后两者需确认 | `RemindersTool.swift` | EventKit Reminders | **Android 没有系统级提醒事项 API**：首版不提供；可改为 hub 侧的 Google Tasks / 飞书任务工具 | — | ✕ |
| 21.5 | `phone_location_current` | 一次性定位（最多等 15 秒）+ 反向地理编码 + 时区；只有使用期间权限 | `LocationTool.swift` | CoreLocation | `FusedLocationProviderClient.getCurrentLocation` + `Geocoder`（API 33+ 异步）；后台定位同样受限 | S | P1 |
| 21.6 | `phone_health_summary` | 步数 / 距离 / 活动能量 / 心率 / 静息心率 / 睡眠 / 体重，按天汇总，默认近 7 天；系统不告诉 App 读权限是否被授予 | `HealthTool.swift` | HealthKit（需 entitlement + 用途说明） | Health Connect（`androidx.health.connect.client`，Android 14+ 内置）：对应数据类型都有、可用聚合接口、能查到是否已授权；超过 30 天的历史要单独权限 | M | P2 |
| 21.7 | `phone_photos_recent` | 最近照片 / 视频元数据（本地 id、时间、类型、尺寸、截图 / 收藏、时长、经纬度），可只要截图 | `PhotosTool.swift` | PhotoKit（可 limited） | `MediaStore`（`READ_MEDIA_IMAGES/VIDEO`，Android 14 部分访问 `READ_MEDIA_VISUAL_USER_SELECTED`）；截图按目录判断，收藏 `IS_FAVORITE`，位置要 `ACCESS_MEDIA_LOCATION` | S | P1 |
| 21.8 | `phone_photo_get` | 返回 JPEG 图片本身（长边默认 1280、最大 2048）+ 元数据 | `PhotosTool.swift` | PhotoKit 图像请求 | `ContentResolver.loadThumbnail` / 解码缩放 → JPEG | S | P1 |
| 21.9 | `phone_device_info` | 机型、系统版本、电量与充电、低电量模式、存储、网络类型、时区、语言、热状态 | `DeviceTool.swift` | UIDevice、NWPath | `Build`、`BatteryManager`、`PowerManager`（省电 / 热状态）、`StatFs`、`ConnectivityManager` | S | P1 |
| 21.10 | `phone_clipboard_get / set` | 读 / 写剪贴板文本（读别的 App 复制的内容时系统会问） | `ClipboardTool.swift` | UIPasteboard | `ClipboardManager`；**Android 10+ 后台不能读剪贴板**，只能在前台读（13+ 会弹提示） | S | P2 |
| 21.11 | `phone_notify` | 本地通知，可延迟最多 24 小时；第一次调用时自己请求通知权限 | `NotificationsTool.swift` | UNUserNotificationCenter | NotificationManager + WorkManager / AlarmManager | S | P1 |

## 22. 界面风格（5 套主题）与设计系统

| # | 功能 | 一句话描述 | 关键文件 / 出处 | iOS 专有能力 | Android 对应做法 | 复杂度 | 优先级 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 22.1 | 液态玻璃（默认） | 系统蓝 `#007AFF`，表面 / 按钮是 Liquid Glass，自己的气泡浅蓝黑字，页面系统色 | `Shared/Theme.swift`（AppStyle `.glass`）、`Jarvis/Views/Styling.swift`、`docs/ios.md`「设计语言」 | iOS 26 Liquid Glass（`glassEffect`、`GlassEffectContainer`） | **没有对应物**：Android 默认风格建议换成「Material You 动态取色」（M3 / M3 Expressive）；要玻璃感可用 RenderEffect 模糊（Android 12+）或 Haze 库，成本高、低端机掉帧 | M | P0（换形态） |
| 22.2 | Claude | 珊瑚主色，Liquid Glass 表面，深珊瑚白字气泡 | 同上（`.claude`） | Liquid Glass | M3 配色方案 + 普通 tonal surface | S | P2 |
| 22.3 | 黏土 | 薰衣草紫，实心软块（左上高光 + 右下彩色阴影、白描边）、圆体字、圆角 24、淡紫页面 | 同上（`.clay`、`SoftButtonStyle`） | `fontDesign(.rounded)` | 自定义 Shape + 阴影（`Modifier.shadow` 有色阴影 API 28+）+ 圆体字体资源 | M | P2 |
| 22.4 | Island | 青色，白色悬浮卡片 + 大而柔的阴影、圆角 26、雾青页面、墨黑气泡 | 同上（`.island`） | — | 卡片 + 柔阴影 | S | P2 |
| 22.5 | 飞书 | 飞书蓝，白卡片 + 发丝描边、无阴影、圆角 10、bordered 按钮、浅灰页面；飞书页面的框架色跟着风格走 | 同上（`.lark`） | — | `OutlinedCard` / 边框按钮 | S | P2 |
| 22.6 | 可读性底线 | 实心按钮 / 选中胶囊白字 ≥ 3.5:1，自己气泡白字 ≥ 4.5:1，主色当链接对页面 ≥ 3:1；气泡渐变只往下压暗 | `docs/ios.md`「设计语言」 | — | 同样的对比度校验（可写单测） | S | P1 |
| 22.7 | 状态色 | 一张表（App、手表、实时活动共用）：需要你红、工作中琥珀、完成未看绿、已看灰、未运行浅灰；每种 dot / text / tint 三个值；不随风格变；交互都算需要你用红 | `Shared/StatusPalette.swift`、`docs/agent-states.md` | — | Kotlin 常量直接移植 | S | P0 |
| 22.8 | Token 与组件 | 圆角 5 档、背景灰阶 3 档、按角色的动态字体、间距；`Chip`（中性 / 着色 / 描边）、`CountBadge`、`StatusDot`（需要你呼吸、工作中静止外环）；`.surface` / `.themedButton` / `.themedPage` / `.themedRow` 统一出口 | `Shared/Theme.swift`、`Jarvis/Views/Components.swift`、`Styling.swift` | Dynamic Type | M3 `Shapes` / `Typography`（sp 跟随字体缩放）/ 自定义 `CompositionLocal` token | M | P0 |
| 22.9 | 动效 | 状态切换 `.snappy` 0.25、布局 `.smooth` 0.3；一直在动的只有需要你圆点呼吸和系统转圈；「减少动态效果」时都停；计数 `.numericText` 过渡 | `Theme.swift`（Theme.Motion） | `accessibilityReduceMotion` | Compose 动画 spec；减少动效读 `Settings.Global.ANIMATOR_DURATION_SCALE == 0`；`AnimatedContent` 数字过渡 | S | P1 |
| 22.10 | 图标 / 品牌 | 夜色背景 + 珊瑚光环 + 四角星（含 dark / tinted 变体）；珊瑚品牌色只给 Persona 自己的标志；能点的东西一律用当前风格主色 | `ios/Jarvis/Resources/Assets.xcassets/AppIcon.appiconset`、`scripts/assets/make_icon.py` | iOS 18+ dark / tinted 图标 | 自适应图标 + 单色主题图标（Android 13+） | S | P0 |
| 22.11 | 字体 | 终端打包 JetBrains Mono；其余系统字体 | `ios/Jarvis/Resources/Fonts` | — | `res/font` | S | P1 |

## 23. DEBUG 启动参数体系与开发工具

| # | 功能 | 一句话描述 | 关键文件 / 出处 | iOS 专有能力 | Android 对应做法 | 复杂度 | 优先级 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| 23.1 | 启动参数直达页面 | 约 100 个只在 Debug 生效的开关（UserDefaults 参数域 `-key value`），分几类：**配对与导航**（`-pairURL`、`-tab dev\|mail\|settings\|assistant`、`-openSession <machine>/<session>`、`-openAssistant`、`-newAssistant`、`-showNewChat`、`-openMachinePage`、`-openMonitor all\|<id>`（+ `-openMonitorRange`、`-openMonitorScrub`）、`-openMachines`、`-openDevices`、`-openStorage`、`-openNotificationSettings`、`-openIslandSettings`、`-openAssistantPage …`、`-addMail`、`-openInboxFirst`、`-showInboxAction`、`-lark-connector*`、`-larkFocus / Profile / Picker / Moments / Top`、`-openLinkURL`、`-openFilePath`、`-openPane terminal\|git\|files`）；**对话状态**（`-openAllWork`、`-loadAll`、`-showFromSeq`、`-dropUsers`、`-showMenu`、`-showShareCard`、`-showWakeDrawer`、`-showRecapDrawer`、`-showSampleQuestion`、`-showSampleScreen`、`-showClearConfirm`、`-showCloseAgent`、`-focusComposer`、`-openSendMessage`、`-openUploadDemo`、`-showFullOutput`…）；**终端**（`-openTerminalPane`、`-showPad`、`-showKeyMods`、`-terminalWrap`、`-terminalTheme`、`-terminalInputMode`、`-terminalPages`、`-openTerminalSettings`…）；**通知 / 实时活动**（`-simulateTap`、`-showTestNotification`、`-showFleetSample`、`-fleetDetails`、`-fleetIsland`）；**连接**（`-showConnection`、`-openConnectionDetails`、`-simulateOffline`）；**探针**（`-showFoldProbe`、`-showFreezeProbe`、`-showAutoScroll`、`-showChatDump`）；`-ui.style` 在 Release 也认 | `Jarvis/App/JarvisApp.swift` 和各页面的 `#if DEBUG`、`docs/ios.md`「DEBUG 启动参数」、`docs/testing.md` | UserDefaults 参数域；`simctl launch` | debug 构建读 Intent extras（`adb shell am start -n <包名>/.MainActivity --es tab dev --es openSession m/s`），代码放 `src/debug`；或 debug-only 深链接 | M | P0（开发效率） |
| 23.2 | Release 检查 | `release.sh` 用 `strings` 确认 Release 二进制里没有这些开关名；开关名不能和任何 Swift 标识符同名（反射元数据会误报） | `scripts/release.sh`、`CLAUDE.md` 经验 | Swift 反射元数据 | R8 / `BuildConfig.DEBUG` 分支 + 只在 debug source set 里出现；同样可在 CI 扫 release APK | S | P1 |
| 23.3 | 假 hub 与截图 | `scripts/mock/mock_hub.py`（编造数据：多台机器、Agent、助手对话、提问、计划、收件箱；`MOCK_*` 开关模拟断网 / 弱网 / 断档 / 上传 / 流式）；`scripts/sim.sh` 编译安装配对启动；UI 测试 + `JARVIS_*` 环境变量；深浅色截图清单 | `scripts/mock/mock_hub.py`、`scripts/sim.sh`、`docs/testing.md` | XCUITest、`simctl` | mock hub 原样复用（Android 模拟器用它固定的宿主机别名地址访问宿主机）；Compose UI Test + 截图测试（Roborazzi / Paparazzi）；`adb shell cmd uimode night yes` 切深色 | M | P0 |
| 23.4 | 性能探针 | `PERF_SIGNPOST` 编译条件下各页面 body / tab 打点；`FrameStats` 统计滚动掉帧；`-showAutoScroll` 自动滚动测帧 | `Jarvis/App/PerfSignpost.swift`、`Jarvis/Views/Chat/FrameStats.swift`、`docs/testing.md` | os_signpost | Macrobenchmark + JankStats + Perfetto trace；Baseline Profile | M | P1 |

---

## 24. 分区统计

| 分区 | 条目 | P0 | P1 | P2 | ✕ |
| --- | --- | --- | --- | --- | --- |
| 1 全局框架与基础设施 | 14 | 11 | 3 | 0 | 0 |
| 2 Persona 助手 tab | 11 | 6 | 4 | 1 | 0 |
| 3 知识库 | 4 | 0 | 3 | 1 | 0 |
| 4 资料页 / 主动做事 / 记忆 | 7 | 0 | 4 | 3 | 0 |
| 5 Agents 列表 | 14 | 10 | 4 | 0 | 0 |
| 6 空间页 / 机器项目页 / 新建对话 | 5 | 1 | 3 | 1 | 0 |
| 7 Agent 对话页 | 37 | 18 | 17 | 2 | 0 |
| 8 终端 | 15 | 0 | 11 | 4 | 0 |
| 9 收件箱 | 11 | 7 | 4 | 0 | 0 |
| 10 飞书会话与卡片 | 13 | 3 | 4 | 6 | 0 |
| 11 飞书文档 / 链接浮层 / WebView 池 | 5 | 0 | 3 | 2 | 0 |
| 12 邮件 | 4 | 1 | 3 | 0 | 0 |
| 13 设置 | 13 | 4 | 4 | 4 | 1 |
| 14 机器详情 / 监控 | 5 | 0 | 2 | 3 | 0 |
| 15 存储空间 | 2 | 0 | 0 | 2 | 0 |
| 16 连接状态 / 离线 | 4 | 4 | 0 | 0 | 0 |
| 17 推送与通知按钮 | 7 | 5 | 2 | 0 | 0 |
| 18 实时活动 / 灵动岛 | 7 | 0 | 5 | 1 | 1 |
| 19 Siri / 快捷指令 | 4 | 0 | 0 | 4 | 0 |
| 20 Apple Watch | 4 | 0 | 0 | 0 | 4 |
| 21 手机数据工具（14 个工具） | 12 | 0 | 9 | 2 | 1 |
| 22 界面风格与设计系统 | 11 | 4 | 3 | 4 | 0 |
| 23 DEBUG 启动参数与开发工具 | 4 | 2 | 2 | 0 | 0 |
| **合计** | **213** | **76** | **90** | **40** | **7** |

P0 的 76 项按复杂度：S 41、M 29、L 5（L：1.5 状态与对话存储、7.5 倒置列表与定位、7.7 消息 → 工作行展示逻辑、7.9 消息渲染、10.1 飞书会话页）。按档位中值机械相加约 250 人日，这是上限：共用组件很多、纯逻辑（ChatStore、ScreenParser、TerminalGrid / Wrap、排序 / 分组、时间文案）可以连同单测用例直接移植成 Kotlin，实际会明显少。

## 25. 重设计新增项（两端都要做）

「规格」列指向 [ux.md](ux.md)（U）、[README.md](README.md)（R）、[hub-changes.md](hub-changes.md)（H）的章节。「hub」列写需要的服务端改动，`—` 表示不用改 hub。

**这一轮只做 Android 和 hub，iOS 只读不改**：下表的「iOS」列是以后 iOS 跟进时的参考优先级（相对 [ios-refactor.md](ios-refactor.md) 的 R0–R5），这一轮都不做。

### 25.1 基础和规则统一

| # | 项 | 说明 | 规格 | hub | iOS | Android |
| --- | --- | --- | --- | --- | --- | --- |
| N1 | 共享夹具 `spec/fixtures` | 连续性、行折叠、排序分组、收件箱、作答、终端帧、协议样本；两端测试读同一批 | R D1 | H 4.13 | P0（R0） | P0 |
| N2 | 设计 token 生成器 | `spec/tokens` → Swift / Kotlin / CSS / 文档；对比度检查；采用率基线 | R D7、U 7 | — | P0 | P0 |
| N3 | 组件目录页 | DEBUG 直达，人工检查和截图测试都用它 | U 3.8 | — | P1 | P1 |
| N4 | 作用域和存储分级 | `hub_id + 配对代次` 前缀；秘密 / 持久 / 缓存三级；带版本，读不出来就停止写入、绝不清空 | R D6 | H 3.5 | P1（R5） | P0 |
| N5 | 统一发送用例 | 所有发消息的入口走 `MessageSender`（幂等 id、等网、拒发处理） | R 4.2、U 5.2 | — | P0（R3） | P0 |
| N6 | 结果三态 | 区分「请求没发出去」和「发出去没回音」；新 hub 上用同一个 id 自动重试，旧 hub 上显示「结果待确认」 | R D3、U 5.2 | H 3.1 | P1 | P0（分类）/ P1（重试） |
| N7 | 一次性操作的结果待确认 | 回答、中断、改名等带 op id；结果未知时重读权威状态或查 `ops/{id}` | U 5.3、5.4 | H 3.1 | P1 | P1 |
| N8 | 统一错误体 | 客户端只认 `code`，不匹配文案 | R D5 | H 3.4 | P1（R2） | P0 |
| N9 | 客户端上报能力 | `client_hello` + `X-Jarvis-Client` | R D5 | H 3.3 | P1 | P0 |
| N10 | 审批策略表 | 只有 App 内看过全文才能「批准并自动执行」；通知、锁屏、快捷入口、手表一律「只自动接受编辑」 | U 3.4 | — | P0（R3） | P0 |
| N11 | 统一路由 | 深链、通知、快捷入口、收件箱打开会话走同一个 `Router` | R 4.2 | — | P0（R3） | P0 |
| N12 | 统一排序 | 一个 `rank`，列表、手表、快捷入口共用 | U 3.1 | — | P0（R0、R3） | P0 |
| N13 | 手机工具按设备路由 | 两台手机同时在线时不互相覆盖 | R 8 | H 2.1 | — | P0（上真机之前） |

### 25.2 对话页和交互

| # | 项 | 说明 | 规格 | hub | iOS | Android |
| --- | --- | --- | --- | --- | --- | --- |
| N14 | 状态行 | 输入区上方唯一一处状态；顶栏副标题去掉状态字 | U 3.2、5.1 | — | P0（R5） | P0 |
| N15 | 待定状态组件和文案词表 | 发送中 / 连上后发送 / 结果待确认 / 正在核对 / 没能确认 / 没有发出 / 没有生效 | U 4、5.6 | — | P0 | P0 |
| N16 | 授权拒绝原因 | 填了原因就不能点「允许」 | U 3.4 | —（已支持） | P0 | P0 |
| N17 | 计划意见和批准互斥 | 写了意见只能「打回」；「批准并自动执行」加风险说明 | U 3.4 | — | P0 | P0 |
| N18 | 请求倒计时 | 用 `expires_at`；过期显示「已过期，回到终端处理」 | U 3.4 | — | P0 | P0 |
| N19 | 同会话多个请求 | 面板「1 / 3」+ 请求列表 | U 3.4 | — | P1 | P1 |
| N20 | 提问草稿按请求保存 | 关掉再开能恢复，请求被替换就丢掉 | U 3.4 | — | P1 | P1 |
| N21 | MCP 来源、子 Agent 来源 | 请求卡和完整页显示来源 | U 3.4 | H 3.7 | P1 | P1 |
| N22 | MCP 表单 / 链接 | 新交互类型 `elicitation` | U 3.4 | H 3.7 | P1 | P1 |
| N23 | 屏幕卡显示完整命令 | 用对话记录里的 tool_input 代替被截断的屏幕文字 | U 3.4 | H 4.9 | P2 | P2 |
| N24 | 工作行失败数 | 折叠时常显「N 项失败 / N 项无结果记录」 | U 3.3 | — | P1 | P1 |
| N25 | 展开后吸附「收起」 | 展开的工作过程滚出顶部后浮出收起条 | U 3.3 | — | P2 | P2 |
| N26 | 会话内搜索 | 搜正文、思考、工具输入输出、系统事件；高亮 +「返回搜索前位置」 | U 3.3 | H 4.11（可选） | P1 | P1 |
| N27 | 任务进度 | 横条 + 全屏页，按结果顺序重建，写明「可能和此刻不同」 | U 3.3 | — | P1 | P1 |
| N28 | 全文阅读器 | 冻结快照、分页、文内搜索、完整复制 | U 3.3 | — | P2 | P2 |
| N29 | 工具卡原始输入 / 输出、diff 改动导航 | 统一入口 | U 3.3 | — | P2 | P2 |
| N30 | 会话状态浮层 | 待确认操作的核对和人工处置、电脑离线、会话已结束 | U 3.3 | H 3.1 | P1 | P1 |
| N31 | 草稿带附件和版本号 | 回执清草稿前比对版本；列表行显示「草稿 · …」 | U 3.1 | — | P1 | P1 |

### 25.3 列表、收件箱、飞书

| # | 项 | 说明 | 规格 | hub | iOS | Android |
| --- | --- | --- | --- | --- | --- | --- |
| N32 | 跨会话请求收件箱 | Agents 的「需要你处理」升级：倒计时、来源，点开直接是完整页 | U 3.1 | — | P1 | P1 |
| N33 | 列表行待确认标记 | 元信息行最前「⏱ 待确认」 | U 3.1 | — | P1 | P1 |
| N34 | 「历史」范围 | 按项目浏览没在运行的会话，只读打开，点「继续」才恢复 | U 3.1 | — | P2 | P2 |
| N35 | 聊天列表引擎统一 | Agents、助手、飞书共用倒置引擎；飞书页去掉固定 sleep | U 3.5 | — | P1（R4） | P0 |
| N36 | 阅读位置锚点 | （消息 id，行内偏移）+ 展开状态 | U 3.5 | — | P1 | P1 |
| N37 | 已读 / 看过的门禁 | 前台、定位完成、无遮挡、非程序滚动、窗口已核对 | U 3.5 | — | P1 | P1 |
| N38 | 「回到最新」的计数口径 | 只数已加载、确实在下方的；没加载完显示 N+ | U 3.5 | — | P1 | P1 |
| N39 | 飞书发送幂等 | outbox + clientId 透传；确认只认 id | U 3.6 | H 3.2 | P0 | P0 |
| N40 | 飞书刷新的缺口检测 | `after` 追平，追不上整屏换最新；`syncState` 完整才删本地行 | U 3.5、3.6 | H 3.2 | P0 | P0 |
| N41 | 重连后核对已打开的飞书会话 | 不再等 60 秒轮询 | U 3.6 | — | P0 | P0 |
| N42 | 收件箱入口的分界线和缓存先画 | 「这件事从这里开始」；缓存包含起点那条时先画 | U 3.6 | — | P1 | P0（属于 10.1） |
| N43 | 飞书和 AI 草拟回复的草稿持久化 | 按（作用域，飞书会话） | U 3.6 | — | P1 | P1 |
| N44 | 话题回复往前翻 | 现在只能看到最新 60 条 | U 3.6 | — | P2 | P2 |

### 25.4 接入、通知、平台

| # | 项 | 说明 | 规格 | hub | iOS | Android |
| --- | --- | --- | --- | --- | --- | --- |
| N45 | 配对分段进度和错误分类 | 每段有时间戳，按原因给提示 | U 3.7 | — | P1 | P0 |
| N46 | 配对说明和准备清单 | 「配对会保存什么」；配对后检查各机器、版本、权限 | U 3.7 | — | P1 | P1 |
| N47 | 二维码改发短时临时 token | 二维码格式不变，token 换成 10 分钟内有效、只能用一次的临时 token，附加 `hub` / `fp` / `tls` / `alt`；不再印永久有效的主 token（`--legacy` 保留） | U 3.7 | H 1.3 | P2（读附加参数） | P0 |
| N48 | 本地网络权限 | Android 17 的 `ACCESS_LOCAL_NETWORK`：配对时申请，被拒时显示专门的状态 | android.md 5.1 | — | — | P0 |
| N49 | 分享到 Persona | 任何 App 的文字、链接、图片、文件发进主对话或某个 Agent | android.md 6.3 | — | P2（分享扩展） | P0 |
| N50 | 通知里输入原因和意见 | 授权「拒绝」带原因、计划「打回」带意见，都能在通知里直接输入 | U 3.4、9 | — | P2 | P0 |
| N51 | 处理后撤回通知 | 交互在别处处理了，手机上的通知自动消失 | U 9 | H 4.3 | — | P1 |
| N52 | 锁屏常驻通知 / Live Updates | 代替实时活动和灵动岛 | android.md 5.10 | H 3.6 | — | P1 |
| N53 | 无障碍规则 | 辅助字号切布局、减少动态效果、计数读完整数字 | U 8 | — | P1 | P1 |
| N54 | HTML 原型 | 状态行、交互完整页和请求收件箱、会话内搜索、配对进度先出原型，你认可后再写原生 | R 9 | — | P0 | P0 |
| N55 | 常驻连接的通知 | 前台服务一直连着 WS，在手机上发通知；国产 ROM 的自启动和电池白名单引导 | android.md 5.8、5.9 | — | — | P0（PR #84 已有） |
| N56 | 「做完了」由 hub 判断后经 WS 下发 | 手机不再自己算，和 iPhone 的推送策略一致 | android.md 5.8 | H 3.9 | — | P0 |
| N57 | 应用层 ping / pong | Android 判断弱网要用（OkHttp 拿不到 pong） | android.md 5.1 | H 3.8 | — | P0 |
| N58 | 通知上直接选提问和屏幕卡的选项 | 单选提问、屏幕对话框的前 3 个选项做成按钮 | U 3.4 | — | P2 | P0（PR #84 已有） |
| N59 | iOS 撤回已处理交互的通知 | App 运行时 `removeDeliveredNotifications`；不运行时要静默推送 | U 3.4 | H 4.3 | P2 | — |
| N60 | FCM | 按「手机没有 Google 服务」设计，不做 | README D8 | — | — | ✕ |
| N61 | TLS 端口和公钥钉扎 | hub 新开 TLS 端口，用自己的密钥签证书；手机钉 SPKI 指纹，主机名校验也交给钉扎；公网地址绝不用明文发 token | android.md 5.1 | H 1.1 | P2 | P0 |
| N62 | 多地址探测和选路（IPv6 优先） | hub 发现并下发候选（公网 IPv6 / 域名 / 内网 / 公网 IPv4）；手机错峰 250 ms 并发探测，证书和 `hub_id` 都对上才算赢；按网络记住赢家，换网重探 | android.md 5.1 | H 1.2 | P2 | P0 |
| N63 | 连接诊断页 | 当前路径和往返时间、各候选的结果、手机有没有 IPv6 / 是否经 VPN、证书指纹、心跳间隔、最近一次失败的原因、「重新探测」 | U 3.8 | H 1.2 | P2 | P0 |
| N64 | 心跳按连接协商 | 后台心跳由 hub 发、从 4 分钟起按网络调整；前台 20 秒 + 应用层 ping；旧连接保持 20 秒 | android.md 5.9 | H 1.6 | — | P0 |
| N65 | `hub_id` 和本地数据作用域 | hub 生成实例 id，在 health / hello / 二维码里下发；手机按 `hub_id + 配对代次` 分区存数据 | R D6 | H 1.5 | P2 | P0 |
| N66 | 公网加固 | 公网来源拒绝明文（`allow_public_plaintext` 开关）；鉴权失败按 IPv6 /64 限流；主 token 只收回环和内网；新设备配对时通知已有设备 | — | H 1.4 | — | — |
| N67 | `personad hub doctor` | 自检监听端口、防火墙、TUN 代理、候选地址、证书；请外部点测可达性，给出照做的结论 | — | H 1.8 | — | — |
| N68 | DDNS（可选） | Cloudflare / 阿里云 DNS / DNSPod / 回调 URL，只发布稳定地址 | — | H 1.7 | — | — |
| N69 | 收到 401 停止重连 | 提示「配对已失效」+ 重新扫码，不再无限重连刷 hub 的鉴权失败 | U 5.5 | — | P2 | P0 |
| N70 | 国产系统适配 | 读系统属性识别澎湃 OS / ColorOS / OriginOS / MagicOS / 鸿蒙 4；能查的（通知、渠道、电池优化、后台限制）显示真实状态，查不了的（自启动、无限制、加锁、华为静默通知和休眠断网、荣耀智能省流量）按系统给步骤和跳转；跳转写成数据，失败退到应用详情页 | android.md 5.18 | H 3.3（`os`） | — | P0 |
| N71 | APK 分发和 App 内更新 | 没有 Play 商店：APK 放在 hub 上，App 内检查更新、用系统安装器升级（默认弹系统确认，澎湃 OS 上不能静默更新）。测试的人日常用离不开它，所以放首版 | android.md 9 | H 2.2 | — | P0 |
| N72 | 后台自检 | 锁屏 30 秒后的测试通知；hub 记录每台设备的断开时段，诊断页显示「过去 24 小时后台断开 N 次，最长 X 分钟」 | android.md 5.9 | H 3.10 | — | P0 |
| N73 | 崩溃日志上传 | 不接 Firebase：`ApplicationExitInfo` 和未捕获的异常下次启动时传到 hub | android.md 9 | H 4.15 | P2 | P2 |

### 25.5 统计

新增 73 项。两端首版都要的 P0：N1、N2、N5、N10、N11、N12、N14–N18、N39–N41、N54。另外，Android 首版要做而 iOS 排在后面的有 N4、N6（分类）、N8、N9、N13、N35、N42、N45、N47–N50、N55–N58（其中 N55、N58 PR #84 已有）、N61–N65、N69–N72。只改 hub 的有 N66–N68（公网直连时 P0，DDNS 可选）。

## 26. 基线之后 iOS 新增的功能（Android 也要对齐）

调研基于 master 的 77999ab。这之后合入的这些，Android 同样要做：

| # | 功能 | 一句话描述 | 出处 | Android 做法 | 复杂度 | 优先级 |
| --- | --- | --- | --- | --- | --- | --- |
| 26.1 | 长会话折叠（`fold_work`） | 长会话首页把一长串工作折成 fold item，显示成工作行；点开时用 `from_seq` / `through_seq` 取回步骤替换（「正在载入 N 步…」，失败可重试）；实时推送和补发不折叠 | 提交 d264477（服务端）、cfe4818（iOS）；`server/internal/proto` 的 `Fold` | `:core:domain` 处理 fold item，进 `spec/fixtures`；页请求和订阅都带 `fold_work` | M | P0 |
| 26.2 | 体验模式 | 没配对时可以「先体验一下」：App 内的假 hub（`DemoHub`）回 `Demo.json` 里的数据（由 `scripts/mock/make_demo_data.py` 从 mock hub 导出），页面里没有体验模式分支；发往电脑、飞书、邮件的操作提示「体验模式下不会真的发送」 | 提交 3a736a4 | 复用同一份 `Demo.json`，在 `:core:hub` 下接一个 `DemoHub`（android.md 5.16） | M | P1 |
| 26.3 | 宽屏双栏 | 按宽度布局：窄屏单栏；宽屏时 Agents、收件箱、设置是列表 + 详情双栏，折叠展开时保留打开的页面；对话和 Persona 首页最大宽度 760 | 提交 5e91691、4060522 等 | Navigation 3 的列表-详情布局（android.md 5.17） | M | P2 |
| 26.4 | Agents 列表：快速筛选 | 搜索框下「全部 / 需要你 / 未读 / 工作中」带计数；筛选下看过的行留在原位；「未读」可全部标为已读 | 提交 c3c6b4d | 同 | S | P0 |
| 26.5 | Agents 列表：细列表、折叠空间 | 一行一个 Agent 的细列表（记住选择）；点空间标题折叠，「全部折叠 / 全部展开」，状态和机器分组也能折叠；空间页入口挪到标题的 ⋯ | 提交 c3c6b4d | 同 | S | P1 |
| 26.6 | Persona tab 的顶栏和侧边栏 | 浮在对话上的顶栏：左边历史（侧边栏），中间 Persona 的头像和名字（点开菜单：知识库、资料页、拷贝目录和会话信息、启动器），右边新对话；断网时名字胶囊显示连接状态；侧边栏可从按钮或整页右划拉出，列出 Persona、定时任务、知识库、置顶和最近的对话（可搜索），底部新对话和设置 | 提交 794f17c | `ModalNavigationDrawer` + 自绘顶栏；宽屏时侧边栏是常驻的一列 | M | P0（替代原来的历史浮层，2.6） |
| 26.7 | 助手写飞书前的确认卡 | 助手用 lark-connector 的 skill 操作飞书，写操作（发消息、回应、日程、文档…）先在手机上确认：一张 `permission` 卡，`tool_name` 是「飞书」，`summary` 写明做什么、在哪个会话、方法、路径和内容 | 提交 d6c9d62 | 授权卡要能完整显示多行的 `summary`（PR #84 的授权卡已能显示）；通知上同样带「允许 / 拒绝」 | S | P0 |
| 26.8 | 后台任务通知一行显示 | 后台任务的通知用工具行的样子，一行显示 | 提交 7922481 | 属于 `chatLines` 的规则，进夹具 | S | P1 |
| 26.9 | 链接识别和表格适配 | 链接在中文和全角括号处结束；表格适配屏宽 | 提交 3f472c2 | 链接识别规则进夹具，两端一致 | S | P1 |
