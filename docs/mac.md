# macOS App

原生 macOS 版 Persona（`apple/` 里的 `PersonaMac` target，旧 `JarvisMac` scheme 指向它）：和 iOS App 同一套逻辑层（`Shared/`、`Persona/App`）和内容视图，
外面是 Mac 自己的壳（`apple/Mac/`）：窗口、侧边栏、工具栏、菜单、快捷键、菜单栏。目标是 iOS App 的全部功能一个不少，
交互按 macOS 原生的方式做，滚动和动画 60 fps 起（ProMotion 屏 120 fps）。

## 做法

- iOS 的代码不为 Mac 改样子：iOS 专用的东西用 `#if os(iOS)` / `#if canImport(UIKit)` 隔开；SwiftUI 里 iOS 专用的修饰符
  （`navigationBarTitleDisplayMode`、`.topBarTrailing`、`.insetGrouped`…）在 `apple/Mac/Compat/` 里给同名的 Mac 版本，页面代码照常写。
- UIKit 写的视图（终端、代码行、等宽文本、图片查看、链接页、长按菜单…）在 Mac 上用 AppKit 重写同名类型，放 `apple/Mac/`。
- Mac 上没有的能力换成 Mac 的做法：灵动岛 / 实时活动 → 菜单栏；Apple Watch 同步、健康数据 → 不做（说明原因）；拍照扫码 → 粘贴配对链接。

## 交互设计（Mac 版的样子，所有 worker 按这个做）

参照 Mail、Messages、Xcode、Things 这类 Mac 原生 App：一个主窗口三栏，菜单和快捷键能完成所有操作，右键菜单代替长按，悬停有反馈，
窗口可以多开。不照搬 iPhone 的 tab 栏、底部弹层、左右滑动。

### 主窗口

- `NavigationSplitView` 三栏：**侧边栏**（来源）→ **列表** → **详情**。侧边栏可收起（⌃⌘S），列表栏宽度可拖、记住。
- 侧边栏（`List(selection:)`，`.sidebar` 样式）分组：
  - **Persona**：主对话 + 置顶的对话；「全部对话」进列表栏看按天分组的历史。
  - **Agents**：全部 / 需要你 / 未读 / 工作中（带数量，需要你是红色角标）；下面是置顶的空间和各台机器（点机器 = 按机器筛）。分组菜单支持「按仓库」「按待办」，没选过时默认按仓库（和 iOS 同一套，#2108）。
  - **待办**：待办列表（左侧）与详情（右侧，⌘7 打开；旧 hub 上不显示），带未完成数量角标；新建待办、完成/重新打开、删除、交给 Agent。
  - **收件箱**：全部 / 飞书 / 各个邮箱 / @我 / 私聊 / 群聊 / 邮件 / Agent。
  - **知识库**：项目 / 会话 / 人 / 文档 / 会议 / 日程。
    列表在左（约 340pt，宽度可拖、记住，`MacListBeside`），点一条在右边打开详情、列表里选中那条（`macSelected`），没选时右边一行提示；搜索结果同样在右边打开，详情里再点进去的（某一天、关联条目、合并到的项目）在右栏里推进，‹ 和标题在右栏的标题行（`MacKnowledgePage`、`KBOpenEntry`，#2318）。手机和 Persona 里推进去的知识库照旧是推页面。
  - **机器**：所有机器（Agent 数和监控在一页）+ 每台机器。
  - 底部：订阅额度的细条（最满的两个账号，点开是每个窗口的浮层，`MacUsageFooter`，#2889，`docs/usage-windows.md`「界面（iOS / Mac）」；没有数据时不画）。
  - 最底下：连接状态（等待网络 / 连接中…，点开是连接详情）。已做（`MacConnectionFooter`，连上时不显示；`-showConnection connecting` 截图看过）。
- 列表栏：iOS 同一份列表内容（Agents 列表的分组、卡片 / 紧凑、折叠空间、置顶），Mac 上行高更紧，选中用系统高亮，
  键盘 ↑↓ 移动选中、⏎ 打开、空格预览（Quick Look 式：对话最后几条）。
- 详情：对话页。顶部工具栏（`.toolbar`）：标题 + 状态·机器·空间›标签，同空间的标签做成工具栏里的分段 / 下拉；
  右侧「对话 / 终端 / 文件 / Git」分段（⌘1–⌘4 在详情获焦时切换），信息按钮打开 **Inspector**（`.inspector`，⌥⌘I）：会话信息、
  运行 / 用量、后台任务、子 Agent、循环、排队消息。
- 搜索：工具栏 `.searchable`（⌘F 聚焦），作用于当前列表。全局 ⌘K「快速打开」：搜 Agent、对话、空间、当前对话目录下的文件、收件箱、知识库条目，回车打开。

### 菜单和快捷键（`.commands`）

| 菜单 | 项目 |
| --- | --- |
| 文件 | 新建 Agent 对话 ⌘N、新建 Persona 对话 ⇧⌘N、在新窗口打开 ⌥⏎ / ⌘⏎（列表里）、关闭窗口 ⌘W、交给 Persona ⌘J |
| 编辑 | 系统的拷贝 / 粘贴 / 全选 / 查找、查找下一个 ⌘G / 上一个 ⇧⌘G；拷贝会话 ID、拷贝恢复命令 |
| 显示 | 刷新 ⌘R、显示 / 隐藏侧边栏 ⌃⌘S、显示 Inspector ⌥⌘I、紧凑列表、分组方式（仓库 / 项目 / 状态 / 机器 / 按待办，默认按仓库）、全部折叠 / 展开、字号放大缩小 ⌘+ ⌘- ⌘0 |
| Agent | 发送 ⏎（输入框里，⇧⏎ 换行，↑↓ 取回发过的、⌃R 搜索）、中断 ⌘.、批准当前待处理 ⇧⌘⏎、拒绝 ⇧⌘⌫、回答选项 ⌥⌘1–9、聚焦输入框 ⌘I（同网页的 Mod+I）、重命名、屏蔽、置顶、标为已读、清空上下文、关闭 Agent… |
| 收件箱 | 全部标为已处理 ⇧⌘E、全部忽略 ⌥⇧⌘E（收件箱在前时；当前筛选和搜索下未处理的，10 件起先确认，见 `docs/inbox.md`「一键清理」） |
| 前往 | Persona ⌘1 … 机器 ⌘5、设置 ⌘6、待办 ⌘7（侧边栏获焦时）、上一个 / 下一个 Agent ⌘[ ⌘]、下一个需要你的 ⌃⌘N、快速打开 ⌘K、回到最新 ⌘↓ |
| 窗口 | 系统的；每个对话可以单独开窗口（`WindowGroup(for: SessionKey.self)`），终端也可以单独开窗口 |

### 和 iOS 的对应

| iOS | Mac |
| --- | --- |
| 底部 tab 栏、角标 | 侧边栏分组 + 数量角标；Dock 图标角标 = 需要你的数量 |
| 长按菜单、左右滑动 | 右键菜单（`contextMenu`），列表行的 `swipeActions` 保留给触控板；悬停显示行内按钮 |
| 底部弹层（sheet / 浮层） | 表单类用 sheet（窗口内），查看类用 Inspector 或 popover；全屏浮层（终端、图片）用单独窗口或详情栏 |
| 链接在 App 内打开（底部卡片） | 详情栏右侧滑出网页面板（可拖宽，⌘W 关闭，「在浏览器打开」），也可以拖成单独窗口 |
| 灵动岛 / 锁屏实时活动 | 菜单栏图标（`MenuBarExtra`）：需要你 / 工作中的数量，点开列出待处理，直接批准 / 拒绝 / 回答 |
| 推送通知 | 系统通知（同样的批准 / 拒绝 / 打回按钮），点开跳到对话 |
| 手机数据（phone_*） | 不提供：Mac 登记成桌面端，phone_* 只认 iPhone（2026-11-01，见「Mac 是桌面端，不是手机」） |
| 拍照、扫码配对 | 拖入 / 粘贴图片；配对用粘贴 `jarvis://pair` 链接或在浏览器里点链接 |
| 终端键栏（Ctrl、Esc、方向键） | 真键盘直接打字；终端获焦时所有键（含 ⌃ 组合）交给终端，⌘ 组合留给 App。键栏暂时还在（⚡ 快捷命令只从那里进），第一次打开时讲触屏手势的提示 Mac 上不显示 |
| Siri / 快捷指令 | App Intents 同样提供（Mac 的快捷指令 App、Spotlight） |

### 细节

- 输入框：⏎ 发送、⇧⏎ 换行；拖文件 / 图片进窗口就附上（图片进输入框，文件上传到那台机器）；粘贴图片同样。
  ⏎ / ⇧⏎ 已做（#785，`apple/Mac/Chat/MacComposerKeys.swift`，有测试）：⏎ 发送（没东西可发时什么都不做），⇧⏎ 在光标处换行，⌥⏎ 同样换行，⌘⏎ 还是发送键的快捷键；输入法有没确认的字时 ⏎ 是确认候选。打满 6 行以后滚轮能在框里滚。2026-11-03 Mac mini 真窗口验过（拼音组字那条只有单测）。
  发送历史（#3094，照网页 #2434 / #2606，`apple/Mac/Chat/MacSendHistory.swift`，`MacSendHistoryTests`）：每个对话最近发出的 100 条（同样的话只留最新一次，空白不记）存在本机的状态缓存
  （`Caches/state/sent-history.json`，不上传）；Agent 对话按会话分开，Persona 的新对话共用一份。输入框是空的、或还显示着刚取回的那条时，↑ 取更早的一条（到最早的停住）、↓ 取更新的一条，
  越过最新的回到空框；多行的那条在光标到第一行（↑）/ 最后一行（↓）之前，箭头还是在行间移动。⌃R（终端的；⌘R 是刷新）在输入框上方弹出搜索：所有对话发过的，按空格分词、每个词都要有、
  不分大小写，新的在前，最多 100 条；↑↓ / ⌃R ⌃P / ⌃S ⌃N 移动，⏎ 或 Tab 放进输入框（不发送），Esc 或 ⌃G 取消，焦点回到输入框。输入法组字时这些键都让给输入法。
  ⏎ 和发送按钮 / ⌘⏎ 发出的都记。截图：`-showSentSamples YES`（几条编造的，只在内存里）加 `-openSentSearch <搜索词>`；搜索是 popover，`-macSnapshot` 另存成 `<名字>.popover.png`，
  离屏截出来的 popover 深浅色和窗口不一致（深色窗口配浅色 popover），没在真窗口上核对。没做：网页的模型选择器；真按键没试过（这台机器不能发合成按键）。
  已做（Agent 和 Persona 对话的输入框、Persona 新对话的输入框，`apple/Mac/Chat/MacComposerDrop.swift`，有测试）：从 Finder 拖进来的图片按「照片」附上（一条最多 4 张，多出的按文件传），
  其余文件按「文件」传到那台机器、路径写进输入框，文件夹不传（提示拖里面的文件）。输入框获焦时 ⌘V：Finder 里 ⌘C 的文件同拖入，
  剪贴板里的图片（截图）附上，文字照常粘贴；只有当前窗口的输入框接（对话可能开在单独的窗口里）。截图：`-openDropDemo a.png,b.md`。
- 拖放：列表里的 Agent 可以拖到侧边栏的「置顶」；文件面板里的文件可以拖出到 Finder（下载）。
- 窗口和列宽、侧边栏开合、选中的项目、每个对话的阅读位置都跨启动记住。
- 外观：跟随系统深浅色和强调色；界面风格（液态玻璃等）照样生效，Mac 上的玻璃用系统材质；不写死尺寸，跟随「设置 › 字号」。
  字号是 Mac 自己的一套（#779）：正文 13（消息、回答、档案）、次要 12、说明 / 标签 / 代码 11、小标题 13 半粗、标题 18、大标题 24，见 `docs/ios.md`「设计语言」。用户消息和回答同一字号。
- 性能：列表和对话都要在 120 Hz 屏上滚动不掉帧（`List` / `LazyVStack` 懒加载、行的 body 便宜、文字排版缓存），
  打开对话首屏 < 200 ms（有缓存时），窗口拖宽时不卡。用 Instruments（SwiftUI / Animation Hitches）量，量法写进本文。
  2026-11-01 夜里这一轮没量成：Mac 一直锁屏，锁屏时屏幕的 display link 不走（试过：一个只开 display link 的小程序 2 秒 0 帧），
  `FrameMeter` / `AutoScroller`（文件查看器的 `-showFilePerf YES -showFileAutoScroll YES`，Mac 上同样能用）和 Instruments 都量不出帧率。
  解锁后先量文件查看器那条现成的路，再给对话页接同样的匀速滚动（对话列表是倒置的 `ScrollPosition`，往 +y 是更早的消息）。
  锁屏时能量的是 hub 推送的主线程开销（和 iOS `LiveUpdateTests` 同一套 `PerfCount`）：本机起 mock hub，App 配对过去、开着 Agents 列表和一个工作中的对话，
  `/mock/churn?n=20&ms=400` 推 20 轮（每轮 5 条），`-perfCounts YES -macPerfLog YES` 每 2 秒打一行（`-macSnapshot … -macSnapshotDelay 60` 让它不读钥匙串）。
  2026-11-01：每轮主线程约 52 ms（约 10 ms 一次），和 iOS 修过之后的「对话，过程卡展开」50 ms 一档；每轮多算的 body：对话页 2、Agents 列表 2、
  空间标题 5、没变的用户气泡 2、回复正文 2.3。Mac 自己的壳（`MacShell` / `MacRootView`）每轮 0 次，菜单栏图标 2 次（只数一遍会话）：
  多出来的都在和 iOS 共用的对话页 / 列表里（没变的行每轮还在重算），要减得在 iOS 一起改、跑 `LiveUpdateTests`，不在 Mac 这边单独动。
  同日 06:00 复测（菜单栏图标上挂了开窗口的 opener 之后）：每轮约 43 ms，壳 0 次、菜单栏图标 2 次，没变。
  ⌥ 点列表行在新窗口打开：2026-11-05 做了（#3223），见下面「窗口」。

  2026-11-02 用户反馈「点起来卡卡的」之后量的（体验模式，真窗口，cua-driver 点侧栏，`ps` 取进程 CPU 时间，`sample` 看主线程）：
  - 切侧栏分区（#390）：每次主线程约 240 ms，Debug 和 Release（`-O` 全模块）一样，不是调试版的问题。
    `⌘1…⌘6` 轮流切、量切过去 1.2 秒内的进程 CPU（不读无障碍树，见下面量法）：设置约 400 ms、收件箱约 460、知识库约 440、Persona 约 560、
    Agents 约 770、机器约 1100；什么都不按的 1.2 秒是 0。
    **2026-11-05 已改成页面常驻（#390，`MacPageStack`，见下面「切换分区：页面常驻」）**，这一段和下面的 `TabView` 结论是改之前的历史。
    当时以为整页重建不是大头：`MacShell` 的 `page` 是 `switch`，换分区就整页重建，但改成系统 `TabView` 保活（去过的页留着，工具栏跟着当前页，
    macOS 26 上实测不崩、状态保留）之后六个分区合计 3750 ms，和原来的 3730 一样：机器页快了约 480 ms（Swift Charts 的迷你曲线不用重建），
    别的页反而慢了一点。时间花在每次都要做的事上：新页面重新进窗口后的 AppKit 布局（`List` 行自动算高、约束、`NSHostingView` 量尺寸）、
    工具栏的项整批换一遍（每个都是一个 SwiftUI 托管视图；Agents ↔ 知识库来回切平均每次约 115 ms）、菜单栏图标重画（`MenuBarExtra`，每次约 25 ms），
    采样里还有一截在查本地化表（`Locale`、`mostPreferredLanguageOf`）。所以没有一个改了就快的地方，要按页、按项减：机器页的迷你曲线（2026-11-04 已改，见下面）、Agents 的工具栏项、菜单栏图标不跟着分区重画。
    保活的几条路都试过：`ZStack` 里藏起来，隐藏页的 `.searchable`、`.toolbar` 照样往窗口工具栏注册，两个搜索框直接崩
    （`NSInternalInconsistencyException`）；每页一个 `NSHostingController` 轮流桥接工具栏，根视图的工具栏项被挤掉、切换时 `NSRangeException`；
    `TabView` 能用但不快（上面），它在工具栏正中的分段选择器 SwiftUI 也藏不掉（`.defaultVisibility(.hidden, for: .tabBar)`、`.hidden()` 都不行，
    要从 AppKit 那边 `NSToolbarItem.isHidden`）。原型在本地分支 `wip/390-tabview-keepalive`，没合。
    量法：cua-driver 点无障碍元素会让 App 一直维护无障碍树（每次点击另有几十毫秒），按快捷键要用 `background` 投递
    （`foreground` 每次把窗口切到前台再切回去，活跃 / 不活跃各重画一遍）；`sample` 主线程看去向。
    **更正（2026-11-04，#1141）**：`background` 发的菜单快捷键（⌘1…⌘6、⌘]）到不了菜单，cua 改走无障碍去按菜单项，
    App 为此把所有菜单「模拟打开」一遍（`NSMenu _simulateOpening`、`_populateWithEventRef`），每按一次多花 60–300 ms，上面的数都偏大。
    现在的量法：先 `bring_to_front` 把副本放到最前，再用 `foreground` 发快捷键（App 已经在前面，不来回切换活跃状态，是真的按键事件），
    `ps -o cputime` 量按键后 1.2–1.5 秒的进程 CPU（按键前后各读一次相减；cua 的命令行 `cua-driver hotkey` 本身要 1 秒多，CPU 不算在 App 上）。
    同一个 Debug 副本、同一个假 hub（2026-11-04，master 28b593fb）：
    - 切分区（真按键 / 无障碍路线）：Persona 约 170 / 290、Agents 约 240 / 370、收件箱约 200 / 330、知识库约 120 / 260、机器约 440 / 520、设置约 140 / 230 ms；
      机器页里迷你曲线（每张卡 3 个 Swift Charts、每个点一个 mark）占约 130–150 ms：换成空视图后切过去约 280 ms。
      改成向量化的 `LinePlot` / `AreaPlot` 不变（约 420 ms，开销在每个图表）；改成 `Canvas` 自己画（#390）后约 290 ms，样子不变。
    - 点开对话（⌘] 下一个 Agent）：真按键有缓存的 120–190 ms、第一次打开 180–360 ms（`MOCK_LONG_HISTORY=1` 的长对话也在内）；无障碍路线 350–450 ms。
      11-02 记的「点开一个对话 450–950 ms」是无障碍路线量的，真按键在「首屏 < 200 ms」的目标以内。没测过真实 hub 上很长的对话。
  - 空闲 CPU（锁屏时起体验实例，`ps -o cputime` 量 20 秒）：停在 Agents 页（`-tab dev`）什么都不动占一个核的 14%；
    停在体验模式默认打开的收件箱是 0。来源是「需要你」圆点的呼吸圈（`StatusDot`）：iOS 用 30 Hz 的 `TimelineView` 画，
    Mac 上每一帧都让托管视图重新算约束、整个窗口重新布局。Mac 改成 Core Animation 画同一个圈（`Mac/Design/MacBreathingRing.swift`，
    同样的缓动、最多 30 帧、所有圆点同相），同一条量法降到 0.1%。只量了 App 进程；锁屏时 WindowServer 不合成窗口，它那边的开销没量。
    同类没改的：Persona 形象（`AssistantAvatar`）在思考中、工作中、需要你时也是 `TimelineView(.animation)`，BeautifulUI 风格下工作中的
    `SpinningArc`、`repeatForever` 动画，在 Mac 上会不会同样整窗重排，没量。
  - 每次推送的主线程开销（#1495，2026-11-04）：优化编译（`SWIFT_OPTIMIZATION_LEVEL=-O`、全模块）的隔离副本 + 假 hub（`MOCK_AGENTS=1`，137 个 Agent、19 个在工作），
    每个分区先重启假 hub、进过一次 Agents 再切过去，`/mock/churn?n=20&ms=400` 推 20 轮（不带 `real=1`：它把工作中的 Agent 翻成做完，连跑几次就没有可推的了），
    `ps -M` 第一行是主线程，推送前后相减。每轮主线程 ms（两次）：

    | 停在 | 改前 | 改后 |
    | --- | --- | --- |
    | 知识库 | 13 / 8 | 8 / 7 |
    | 设置 | 22 / 16 | 6 / 6 |
    | 机器 | 32 / 32 | 33 / 27 |
    | Persona | 48 / 39 | 46 / 33 |
    | Agents | 68 / 66 | 46 / 37 |

    改了三处：菜单栏图标和菜单读 `MacMenuBarState` 的快照（只在个数、标题、快捷回答变了时才换），原来读 `model.allSessions`，
    每次推送状态栏按钮都重新设图、量宽、重画，菜单整份重建；窗口离开 Agents 分区时放掉 `MacShellState.agentsList`（它的闭包抓着列表的状态，
    列表的 `AgentsFeed` 在别的分区照样每轮拷贝、排序全部 Agent；显示 › 全部折叠 / 全部展开因此只在 Agents 分区可用）；`AgentsFeed` 不为机器的心跳（`lastSeen`，列表不显示）重算列表，
    设置页的「机器 N / M 在线」拆成自己的视图。剩下的大头：Agents 页每轮仍要一次 `AgentsList` 的 body（工作中的 Agent 换了顺序）和 `NSTableView` 重新算行高；
    Persona 页是侧栏 `AssistantSidebar`（每轮 2 次，读全部会话）和对话页；机器页是 AppKit 布局（页面的 body 不算在内）。都是和 iOS 共用的页面，没在这里动。
    同一量法、真按键切分区（`bring_to_front` 后 `foreground` 发 ⌘1…⌘6，每次 1.5 秒内的主线程 CPU，4 轮中位数）改前改后没有变化：
    Persona 120、Agents 240、收件箱 190、知识库 110、机器 90、设置 100 ms。Agents 那一下的大头是 AppKit 的布局（采样里约三分之二在
    `NSWindow layoutIfNeeded`：工具栏重排、`NSTableView` 自动行高），去掉工具栏的 Agents 菜单能省约 40 ms，让它常驻、离开时 `.hidden()` 藏起来省不了，没改。
    `PerfCount` 的 ms 只算推送到 run loop 空闲那一段，比 `ps` 量的少得多（推送解码、合并、布局在它之外），比较前后要用 `ps`。

### 切换分区：页面常驻（#390）

每个分区的页面第一次打开时建好，之后一直留在窗口里（`Mac/App/MacPageStack.swift`）。切换分区只是藏起一页、显示另一页，切回去过的页一两帧就能画出来。
原来每切一次都要整页重建：列表、分栏、每行的高度、工具栏项全部重来，Agents 主线程要约 200 ms，收件箱约 160 ms。

- **每页一个 `NSHostingView`**，藏起来就 `isHidden`，藏着时跳过 `layout()`。
  不用 SwiftUI 的 `ZStack` 或 `TabView`：`ZStack` 里藏起来的视图照样跟着整窗布局；`TabView` 会把切走的页从窗口里摘下，再放回来又是整页布局（见上一段的历史）。
  也不要把藏起来的 hosting view 从窗口上摘掉：实测每次推送反而多 30 ms。
- **工具栏晚一帧换**：谁在前台（`tabIsActive`、工具栏项、搜索框）等这一帧提交以后才改（run loop 进入休眠前的观察者，再 `async` 一次）。页面先在第一帧画出来，AppKit 逐项重排工具栏放到下一帧。
  只用 `DispatchQueue.main.async` 时，有时会赶在第一帧之前执行，Agents 切回去三次里有一次是 135 ms。
  所以 Agents、收件箱切回去后还有一帧长约 85–95 ms 的帧（工具栏重排）；要去掉它，得让各分区共用一套稳定的窗口工具栏，那是界面改动，没做。
- **插件页盖在常驻页上面**（`overlay`），这时所有常驻页都在后面（`MacPageStack(shown: nil)`），回来不重建。
- **页面根视图补上窗口那棵树的环境**（`MacPageRoot`、`MacPageChrome`）：`openWindow`（页面里用 `shellOpenWindow`）、`scenePhase`、公式图片、
  标题行的边距和分区标题（`MacTitleRowTaken`）、`tabIsActive`。新加窗口级的环境值时，这里也要补上。
- **藏起来的页要做的事，各页自己负责，靠 `tabIsActive`（藏着时为 false）判断**：
  - 工具栏项和搜索框只由前台页提供，用 `pageToolbar`、`pageSearchable`。两个搜索框同时出现在窗口工具栏会直接崩溃。
  - 分区自己的导航栈（Persona、知识库、机器）里推进去的页不能用系统的返回键：它是窗口工具栏里的一项，藏起来的页的也照样画着，点了退的是看不见的那页（#2318）。栈上设 `macSectionStack`；推进去的页用 `pageTitle` / `detailTitle` 给标题就自动在标题行画 ‹（`MacPushedPage`，只对推进去的页，栈的第一页不画），只有 `navigationTitle` 的页加 `sectionPushedPage()`。
  - 不跟着推送重算：
    - Agents 列表：`Hold` 加了 `.behind`；
    - 待办、机器页：读 `allSessions(inFront:)`、`machines(inFront:)`；
    - 只显示机器名字、主机名的地方（端口链接、收件箱的 Agent 行、侧栏底部）读 `namedMachine`，不跟心跳；
    - 对话页：藏起来就退订，回到前台按 `since_seq` 补齐；也不再算「屏幕上的对话」，期间答完的回复照样是「完成未看」并发通知；
    - 形象的时钟和眨眼停下（藏着时空闲每 10 秒原来要 80 ms）；
    - Persona 侧栏：`assistantConversations` 只在会话列表真的变了时才变。
  - `List` 的行在藏起来的页里拿到的 `tabIsActive` 不一定是新的（行不重新布局），行里别靠它，读不跟心跳的值。
  - 页面里只想知道进出前台、不需要因此重画的，用 `PageFrontWatcher`，不要在 body 里读 `tabIsActive`，否则每次进出前台整页重算。
- **外壳本身**：切分区时记的 `app.tab`、`mac.section` 延后写（`DeferredDefaults`：停下 3 秒后、离开 App 或退出时）。每写一次 UserDefaults，所有常驻页里的 `@AppStorage` 视图都要重算。
  `MacPageStack` 自己实现 `sizeThatFits`：不然每画一次外壳，SwiftUI 都会用 Auto Layout 把所有常驻页量一遍。
- **量法**：用 App 自带的测量工具，不用 cua，也不用真按键。
  - `-showPerfRun sections -showPerfRounds 4` 把每个分区切 4 轮；`-showPerfSections agents,settings` 只来回切这几个；`-perfCounts YES` 记下每一步重算了哪些视图的 body。
  - 第一轮是第一次进入，后面几轮是「切回去过的页」。
  - 看 `toFirstFrameMs`：按下以后，主线程第一次能开始画下一帧要多久。这台开发机的虚拟屏是 50 Hz，一帧 20 ms。
  - 推送开销：先把所有页都去一遍，最后停在知识库，再用 `/mock/churn?n=20&ms=400` 推送，`ps -M` 取主线程 CPU。
  - 查哪一页藏着还在跟推送：在 body 里临时加 `Self._printChanges()`，正常退出（`osascript … quit`）后看 stdout。

实测（2026-11-05，这台开发机，Debug + `-O`，假 hub `MOCK_AGENTS=1`；master 是 `f97caba5`，只加了 `-showPerfRounds`）：

| 分区 | master 切回去 | 常驻 切回去 | 常驻 切回去后最长一帧 | master 第一次进 | 常驻 第一次进 |
| --- | --- | --- | --- | --- | --- |
| Persona | 81 | 31 | 36 | 10 | 16 |
| Agents | 217 | 48 | 86 | 147 | 29 |
| 收件箱 | 159 | 34 | 92 | 195 | 117 |
| 知识库 | 94 | 27 | 50 | 115 | 84 |
| 机器 | 54 | 28 | 50 | 61 | 70 |
| 设置 | 43 | 28 | 20 | 52 | 91 |
| 待办 | 46 | 33 | 49 | 54 | 87 |

（毫秒，`toFirstFrameMs`，3 次各 4 轮的中位数，「切回去」是第 2–4 轮。常驻的切回去在 10 ms 上下和 30–50 ms 两档之间跳；第一次进设置、待办比 master 慢 30–40 ms：新页先按「在后面」建好，下一帧再整页变成前台。）

每次推送主线程（`/mock/churn?n=20&ms=400`，3 次）：停在知识库 master 15–16 ms、常驻且七页都去过 19–20 ms；停在 Agents master 62–63 ms、常驻且都去过 65–68 ms、只开 Agents 58–61 ms。
多出来的几毫秒没有落在我们自己的 body 上（`-perfCounts` 计数为 0），在藏着的页面 hosting view 里 AttributeGraph 的更新上；空闲时都是 0。

## 构建、运行、截图

```sh
cd apple
python3 ../scripts/persona_config.py xcconfig && xcodegen generate
eval "$(python3 ../scripts/persona_config.py env)"
xcodebuild build -project Persona.xcodeproj -scheme PersonaMac -configuration Debug -destination "platform=macOS" \
  -derivedDataPath ../../build/DerivedDataMac CODE_SIGNING_ALLOWED=NO
open ../../build/DerivedDataMac/Build/Products/Debug/Persona.app   # 或直接跑 Contents/MacOS/Persona 带启动参数
```

- 配置里没有 `app.bundle_id` 时编出来的 App 一启动就崩（Info.plist 没有 bundle id），本机自用可以在 xcodebuild 后面加 `JARVIS_BUNDLE_ID=<反向域名>`（`docs/lessons/macos.md`）。

- 启动参数和 iOS 一样（`MacLaunchArguments`，只在 DEBUG）：`-pairURL`、`-tab dev|mail|settings|assistant`、`-openSession <machine>/<session>`、
  `-openAssistant`、`-newAssistant`、`-showNewChat`、`-openChatsFresh`、`-openAgentsFresh`、`-lark-connector`、`-simulateTap`；体验模式 `-demo.on YES`。
- 截图（**锁屏也能拍**）：`scripts/app/mac_shot.sh <名字> [启动参数…]`，浅色、深色各一张写到 `/tmp/jarvis-mac-shots/<名字>-{light,dark}.png`。
  配对后的「选择引擎」一步：`-openEngineStep <机器 id>`（体验模式的 `server` 就行）。看玻璃材质要截真窗口，见 `docs/lessons/macos.md`。
  默认体验模式；`BUILD=1` 先编译，`APP=` 指定 .app，`SIZE=1280x820`，`DELAY=4`，`APPEARANCES="light dark"`。
  例：`scripts/app/mac_shot.sh agents -tab dev -dev.openChat ""`、`scripts/app/mac_shot.sh chat -openSession macbook/a1f3c2d4-0000-4000-8000-000000000001`、
  `scripts/app/mac_shot.sh jarvis -tab assistant`、`scripts/app/mac_shot.sh inbox -tab mail`、`scripts/app/mac_shot.sh settings -tab settings`。
  背后是 DEBUG 开关 `-macSnapshot <png> [-macSnapshotDelay 秒] [-macSnapshotQuit YES] [-macWindowSize WxH] [-macAppearance dark|light] [-macSnapshotScroll 点数]`（`Mac/App/MacSnapshot.swift`）：
  App 自己用 `cacheDisplay` 把窗口画进 PNG，不经过屏幕。`-macSnapshotScroll` 截图前把窗口里能竖着滚的列表都往下滚这么多点（看滚上去的内容和标题栏那一行，#1243）。
- 截图和屏幕上的差别（2026-10-31 试出来的）：
  - Liquid Glass / 毛玻璃画不出模糊，露出的是窗口底色；玻璃按钮只剩边框。看布局和文字够用，看材质要真机。
  - SwiftUI `List`（NSTableView）的行作为窗口的一部分画时是空的：截图时每一行单独再画一次、贴回原处（按行上面所有裁剪的视图裁，`.clipped()` 挡掉的不画）；选中行的材质离屏画成黑块，改成画非激活窗口的选中色。
  - 深色下 List 行里的文字仍是浅色方案的颜色（深灰），`NSApp.appearance`、`preferredColorScheme`、截图时套 appearance、去掉图层滤镜都没用；行的 effectiveAppearance 是 DarkAqua。
    还没确认是截图的问题还是真的这样，要在解锁的 Mac 上看一眼（待办）。
  - `layer.render(in:)` 上下颠倒、`dataWithPDF` 大部分文字丢失，都不如 `cacheDisplay`。
  - `-AppleInterfaceStyle Dark` 作为启动参数对 App 不生效，深浅色用 `-macAppearance`（在 `applicationWillFinishLaunching` 里、窗口建出来之前设）。
  - 分栏的侧栏 / 列表栏浮在玻璃容器（`NSContainerConcentricGlassEffectView`）里，容器的内容层是个空 layer，`cacheDisplay` 跳过了它：Agents 顶上的快速筛选、Persona 的历史侧栏原来截出来是空白 / 白卡片。
    现在截图时把容器里的视图逐个画、再叠上工具栏（`0af14af`，只在 DEBUG）。列表栏顶边和左边留着一条约 12pt 的黑边（修之前就有），只在截图里、真窗口不受影响，没再追。
  - 2026-11-01 还画不对的，要解锁看真窗口：深色下工具栏搜索框是白色空胶囊；非激活窗口里着色的进度条（启动器额度）是灰的；白字的醒目按钮（知识库「问 Persona」）看不见；
    连接详情浮层；快捷命令面板和终端键栏是深底深字；对话页顶上同空间标签的胶囊条；`-openAssistantSidebar YES` 的 Persona 侧栏。
  - 截图的坑：`-showMailAskClaude`、`-openMonitor` 这类 DEBUG 钩子挂在 onAppear 上，每次出现都会再触发（只在 DEBUG、只为截图，不改）；`-showWakeDrawer`、`-showMenu` 要 `DELAY` 11 秒左右；
    共享 UserDefaults 里别的运行留下的草稿（比如一个 `/`）启动时会弹出斜杠菜单；假 hub 换数据重启后缓存合并会多出一张重复的错误卡片，`-openChatsFresh` 能清掉。


### 滚动实验（`MacScrollLab`，锁屏也能做）

锁屏时 `screencapture` 只拍到锁屏、也没有真触控板，滚动的问题就让 App 自己滚、自己画（DEBUG）：
`-macScrollGestures "13:150,17:900"`（启动后第 13 s 两指滑 150 pt、第 17 s 滑 900 pt，手指 120 ms 加惯性约 1 s，负数往最新处），
`-macScrollFrames <目录>`（按 0.1 s 把窗口画成 `m-<时间>.png`，`list.txt` 是对话列表在图里的位置），`-scrollTrace YES -scrollTracePath <文件>`（每帧的偏移、内容高、手指 / 惯性、跟随状态）。
`scripts/diag/stream-scroll/run-mac.sh` 连 mock hub 跑一遍，`pngs2mp4.swift` 把图按时间拼成视频，`vidrows.swift` + `streamrows.py` 量每帧的位移（docs/ios-state.md「流式时的滚动」）。
限制：合成的惯性事件 SwiftUI 不当作惯性，列表也不跟着滑（真触控板的惯性要在解锁的 Mac 上看）；锁屏时投进事件队列（`-macScrollPost YES`）的事件到不了窗口，所以直接交给列表和 `MacWheelWatch`。
## Mac 版现在的样子（阶段 0：能编、能跑）

- 壳（`Mac/App/`）：窗口左边一列六项（Persona / Agents / 收件箱 / 知识库 / 机器 / 设置，角标同 iOS 的 tab 栏），右边是 iOS 的页面本身；
  这一列贴着窗口左边、上下到顶（伸到红绿灯后面），侧栏底色，和页面之间一条分隔线；行和 Persona 页的历史列表（`AssistantSidebar`）同一套：
  同样的图标框和字号，选中是同样的灰色圆角底（`MacSidebarRow`，收件箱的筛选栏也用它；2026-11-02 用户：系统的源列表放在旁边像没做过样式）。
  窗口工具栏不画底色和分隔线，也不显示页面标题（`.toolbar(removing: .title)`：系统把标题排在红绿灯旁边的按钮后面、压在这一列上，长一点就越过分隔线，
  2026-11-02 用户：文字都超出去了；在哪一页看这一列的选中，窗口标题仍在「窗口」菜单里）。标题原来占的位置换成一个 `ToolbarSpacer(.flexible)`，
  不然页面的按钮全挤到左边。红绿灯旁边一个按钮收起 / 显示这一列（和「显示 › 隐藏侧边栏」⌃⌘S 是同一个动作）。
  标题和操作放一行（#830，2026-11-03 用户：去掉标题后顶上空一条、左边一块白色凸起，选了「标题和操作放一行」）：壳在红绿灯那一行、页面这一栏的左边画分区的标题
  （`MacPageTitle`，只是一个标签，那一行照样能拖动窗口；这一列收起时排在红绿灯和按钮后面），页面的按钮和搜索在右边，内容紧接着往下排。
  这一行不画底色，标题下面的列表从这一行下面开始排、裁在这一行的下沿（`MacBelowTitleRow`，#1243）：往上滚的行不再从标题后面透出来
  （工具栏底色隐藏后，系统挡住滚上去内容的那块也没了，见 `docs/lessons/macos.md`）。设置的列表、收件箱的筛选栏、知识库和机器的首页都这样；收件箱中间的列表不在标题下面，分组标题跟行一起滚动（`MacInlineListHeaders`，#3080），不画系统浮动分组的异色横条；
  Agents 的列表滚到快速筛选那一排底下就被挡住，标题行里还有它自己的按钮，不加。
  Persona 页的两栏自己的头（列表的形象名和搜索、对话的顶栏）上移进这一行，不另画标题；这一列收起时从红绿灯后面开始（壳经 environment `macTitleRowLeading` 告诉页面从哪开始）。
  收件箱、知识库的工具栏加一个空的 `.principal` 占住中间（`MacToolbarMiddle`，同 Agents 的 `ConnectionTitle`），按钮靠到搜索框旁边，最窄的窗口里不盖住标题；
  机器的 `ConnectionTitle` 不画玻璃（连着时它是空的，玻璃在这一行中间画出一条细线）。
  Agents 在 Mac 上不放 `ConnectionTitle`（#907，2026-11-03 用户：新建按钮没贴左边）：占住中间的 `.principal` 把列表栏的 `.primaryAction`
  挤到对话那一栏的中间，左边空出一块；去掉后「新建会话」在列表栏的右端、紧挨对话（同「邮件」的写邮件）。连接状态在分区列底部（收起时在页面底部）。
  「个人 Agent」按钮仍在工具栏，挨着「新建会话」（#907 一度挪进 ≡ 菜单，#925 按用户要求放回）。收件箱筛选栏第一组的小标题从「收件箱」改成「来源」。
  知识库的筛选 chip 在 Mac 上不放进 Section（Mac 的 inset 列表给它留了一条空的分组头和一条线）。
  白色凸起：分区列右边的分隔线原来停在这一行下面，页面的白底在那里越过了这一列几个点；分隔线现在画到顶。侧栏按钮也不画系统的玻璃圆底（`.sharedBackgroundVisibility(.hidden)`）。
  还没做：推进去的页面（Persona 里的档案、定时任务，知识库的某一天）的标题，系统的标题去掉以后它们没有地方显示。
  Agents 列表栏的「个人 Agent」「新建会话」在 Mac 上画在列表栏自己的标题行右端（`MacTitleRowButton`，#1223）：放在窗口工具栏里时它们跟着窗口中线走，
  920 宽时在列表中间，1800 宽时跑到对话栏中间。模型供应商页的「+」在 Mac 上是「Persona 管理」那组里的「新增供应商…」：这页有搜索框，工具栏右端被它占了，「+」被排到窗口中间、压在设置列表上。
  在 Agents 分区，它后面紧跟着列表的 ≡ 菜单（分组、显示、折叠、机器）：两个放在壳的同一个 `ToolbarItemGroup` 里。以前 ≡ 是列表自己的工具栏项，
  详情栏打开空间页、机器页（页面带自己的 ⋯ / ⓘ）时就排到了侧栏按钮前面（2026-11-02，`docs/lessons/macos.md`）。
  按钮（`themedButton`）在 Mac 上是自己画的胶囊（`Mac/Design/MacCapsuleButton.swift`）：主要的是主题色底白字，其余是主题色字配一层浅的主题色底，
  和收件箱草稿卡片上的「发送 / 修改」、筛选 chip 一样；系统的玻璃按钮在活跃窗口里会整个填成主题色（见 `docs/lessons/macos.md`）。
  `horizontalSizeClass` 在 Mac 上恒为 `.regular`，所以 Agents、收件箱、设置走的是 iPad / iPhone Duo 打开时的分栏布局；Persona 页是列表在旁边的 `MacListBeside`（见下面）。
  外层不用 `NavigationSplitView`：页面自己是分栏，分栏套在另一个分栏的详情栏里时，里面的详情栏按外层侧栏还在前面排版，对话被挤到右边、输入框被裁掉。
  页面的分栏在 Mac 上是三栏、第一栏空着（`SplitColumns`）：macOS 26 把分栏的侧栏画成离窗口四边一点的玻璃卡片，周围露出窗口底色
  （2026-11-02 用户：像浮在背景板上，有缺口），没有关掉的 API；内容栏是贴边的，列表放在内容栏，内容栏里的 `NavigationLink(value:)` 照样推进详情栏。
  代价：Agents、收件箱、设置的列表栏在 Mac 上不能收起（原来工具栏上的「隐藏列表」在红绿灯旁边，正好压在六项这一列上）。
  Persona 页的历史列表要用顶栏的「历史对话」收起，Mac 上不是分栏，是 `MacListBeside`（列表 | 可拖的分隔线 | 对话，宽度在 `Theme.Size.sidebarMin`–`sidebarMax` 之间、记在 `mac.assistant.listWidth`）：
  三栏分栏收不起内容栏，设成 `.detailOnly` 会被系统马上改回 `.all`，空的侧栏那一栏冒出来、列表还在（#856）。
  窗口最小 920×560（#1200）：分区列 210 + 列表最窄 340 + 旁边的页面最窄 360（`macDetailMin`）。窗口变窄时列表先让：Persona 的 `MacListBeside`、收件箱的列表收到各自的最小宽度，
  分栏的详情栏有最小宽度。原来最小是 820，旁边的页面只剩约 200，对话、收件箱详情被窗口右边裁掉。收件箱多一列筛选，最窄时详情只有约 220，排得开但挤（要更宽得把筛选列收起，见 #1169 的清单）。
  ⌘1…⌘7 切换；Persona › 设置… ⌘, 把主窗口带到前面、选中侧栏的「设置」（#1222，2026-11-04 用户定：原来是一个单独的设置窗口，内容和侧栏的「设置」完全一样，两个入口；没配对时不可用）；`onOpenURL` 处理 `jarvis://pair`；App 变成活跃时 `ensureConnected` + `refresh`。
  App 在后台不断开 WebSocket（Mac 应用常驻，hub 靠这条连接找到它）。
- 兼容层（`Mac/Compat/`）：`UIKitTypes.swift`（UIColor / UIImage / UIFont / UIEdgeInsets 的别名，UIPasteboard、UIApplication.open、UIDevice（含电池）、
  UIGraphicsImageRenderer、haptics 空操作、UIKit 语义色）、`SwiftUICompat.swift`（iOS 专用修饰符的同名 Mac 版本：`navigationBarTitleDisplayMode` 什么都不做、
  `.topBarTrailing` → `.primaryAction`、`.insetGrouped` → `.inset`、`fullScreenCover` → `sheet`、`.navigationBarDrawer` → `.toolbar`、`for: .tabBar/.navigationBar` 不做事、
  `horizontalSizeClass` 恒 regular、`EditButton` 空、`ImageRenderer.uiImage`…）、`MenuCompat.swift`（`UIAction` / `UIMenu` → `NSMenu`）、`NavigationCompat.swift`。
- 用 AppKit 重写的同名类型：终端（`Mac/Terminal/`，NSScrollView + `NSTextInputClient`，拼音、选择、⌘C/⌘V、滚轮、捏合 / ⌘± 调字号）、代码行与等宽文本
  （`Mac/Code/`，只画可见的行、可选中拷贝）、图片查看 / 缩放（`Mac/Media/`，NSScrollView 放大，←/→ 翻页）、长按菜单 → 右键、可选文本、
  网页（`Mac/Link/`，WKWebView 右键菜单加「AI 修改 / 作为上下文」）、形象帧动画、ChatPin、邮件 HTML、Persona 侧边栏抽屉（窄窗口时才用，宽窗口是一栏）。
- Mac 上没有的，及原因：
  - 实时活动 / 灵动岛：macOS 没有 ActivityKit。`LiveActivityController` 的 Mac 版只记下 hub 推来的 fleet 状态（给以后的菜单栏用），设置 › 通知里「实时活动」一节不显示，`IslandSettingsView` 不编译。
  - Apple Watch 同步：Mac 不和手表配对，`WatchSync` 是空的；设置里也不再显示「同步到 Apple Watch」（2026-11-01 前还显示，点了提示已发送）。
  - 拍照（`UIImagePickerController`）：`isSourceTypeAvailable(.camera)` 为 false，输入框不出「拍照」；图片从文件、剪贴板来。
  - 扫码配对（VisionKit `DataScannerViewController`）：换成「粘贴配对链接」按钮（`Mac/Settings/PairingPaste+Mac.swift`），或直接打开 `jarvis://pair?…` 链接。
  - 健康数据：macOS 有 HealthKit 的接口但没有数据，`phone_health_summary` 在 Mac 上不注册（`PhoneToolRegistry.healthTools`）；设置 › 连接器 › 这台 Mac 里也没有健康开关（`PhoneDataSource.onThisDevice`，有测试），「N 项已打开」同样不算它。页脚的说明改成了 Mac 的说法（问你的是这台 Mac）。待查：截图里设置页的长页脚（这里、通知页）只显示一行带省略号，加 `fixedSize` 也一样，分不清是 `-macSnapshot` 重画 List 行的问题还是真的被截，要解锁后用真截图看。
  - **Mac 是桌面端，不是手机**（2026-11-01 用户定：照微信「Mac 微信已登录」的做法）：以前 Mac 版也发 `register_phone`，hub 只有一个「手机」
    （`hub.phoneClient`），Mac 一直在线时 phone_* 全落到 Mac 上、hub 不再推送唤醒进了后台的 iPhone、Mac 的工具表还把 `phone_health_summary` 冲掉了。
    现在 Mac 发 `register_desktop`（`AppModel.registration`，有测试），phone_* 只认 iPhone；Mac 开着而 iPhone 不在时，Claude 拿不到手机数据，属预期。
    设置 › 连接器 里「这台 Mac」那一行在 Mac 上去掉了（hub 不会再调它的工具），欢迎页的说法改成数据来自 iPhone 上的 Persona。
    iPhone 那边：Agents 列表最上面一行「Mac 上的 Persona 已登录」，点开能开「手机静音」（Mac 在线时手机不推「需要你处理」「Agent 做完」，
    由 Mac 提醒；助手按你的要求发的提醒照推）、看登录时间、退出那台 Mac（吊销它的设备凭据，要重新配对）。协议见 `docs/protocol.md` 的 `desktops` / `register_desktop`。
    配对时 Mac 用配对 token 换的设备凭据现在要 kind `desktop`（`AppModel.credentialKind`，有测试），hub 存成 `other`、权限不变，设置 › 设备 里显示电脑图标。
    这次改动之前已经换过凭据的 Mac 在列表里还标成 phone，只是标签，撤销后重新配对就对了（不改）。
- 临时实现，等后面打磨：外层侧栏和页面自己的分栏是两层（六项是 HStack 里的一列，页面是第一栏空着的三栏分栏）；分页的 `TabView(.page)` 换成了默认样式 / 箭头翻页；
  `SwipeTo`、Agent 间的双指滑动在 Mac 上不做事；代码行只有整行选择；对话页的玻璃按钮、浮层菜单没有按 Mac 习惯调；
  字号跟随设置里的小 / 标准 / 大，但 Mac 没有动态字体：`Theme.Font` 每一级（不小于 11：「小」时 caption / label / code 停在 11）、Markdown 的基础字号乘 `Theme.Font.scale`（`AppModel.textSize` 设），图标框、行距、飞书正文按兼容层的 `UIFontMetrics`（Mac 自己的表 `macBody`，正文 12 / 13 / 14）换算，两边同一个比例（#833）。

## 设置子页的样子（Mac）

2026-11-04 用户定（#1169 B1，#1221）：设置的子页在 Mac 上是系统设置那样的分组表单（圆角分组、开关在右边、页脚换行），iPhone 不变。
- 页面的根用 `SettingsForm { … }`（`Persona/DesignSystem/SettingsForm.swift`）：iPhone 上是原来的 `List`，Mac 上是 `Form` + `.grouped`。
  Mac 的 `List` 页脚只排一行、后面是省略号，侧栏样式干脆不显示页脚（#1212，`fixedSize` / `lineLimit` 都没用，见 `docs/lessons/macos.md`），分组表单里才正常换行。
- 表单里的按钮用 `formButton(prominent:)`：iPhone 上是 `themedButton`，Mac 上是系统的标准按钮（`.bordered` / `.borderedProminent`），不画有色胶囊；整行宽的按钮在 Mac 上改成靠右的标准按钮。
- Mac 的表单把输入框的标题画在左边、框在右边：标题写字段名，要填什么写进 `prompt`；标题本身就是提示的（屏蔽目录、Claude 设置的文本项）在 Mac 上 `.labelsHidden()`。
- 单选在 Mac 上用 `.radioGroup`；空的分组在 Mac 上不画（表单里会留一块空白）。
- 页尾单独一个按钮（添加 hub、清除全部缓存、登录某个连接器）在 Mac 上是一张卡片里靠右的标准按钮（同系统设置）；`.listRowBackground(Color.clear)` 在 Mac 的分组表单里不起作用，卡片去不掉（`FormAction`）。
- 已经换的页：通知、Claude Code 设置、Codex 设置、飞书机器人（第一批）；设备、翻译、存储空间、hub、hub 列表、Skills（第二批）；邮箱、飞书、GitHub / Telegram、信息、插件和插件详情（第三批）；模型配置和用途、模型供应商和某个供应商、形象、主题色、界面风格（第四批）。
  助手（`AssistantProfileView`）、机器（卡片）、信息 › 会话 这类不是表单的页面不换。

- 设置子页的标题（#1224 B2）：页面用 `detailTitle("…")`（iPhone 上就是 `navigationTitle`），Mac 上由详情栏的 `MacDetailBack` 画在这一栏标题行的左边，
  第二层前面是 ‹（「‹ SSH 隧道」「‹ MacBook」），同系统设置。不用 `pageTitle`：那个会告诉外壳标题行被占了，左边列表栏的「设置」两个字就没了。

- 设置列表顶上的「体验模式」两项、「服务器」里的「重新配对」「断开并忘记服务器」在 Mac 上是和别的行一样的行（`SettingsButtonLook` 在 Mac 上是 `.plain`），断开 / 退出是红字（#1224 B4）。
- 体验模式下没有演示数据的页（模型供应商、飞书机器人）在 Mac 上用灰色说明「体验模式没有这部分的演示数据」，不显示红字错误（`LoadProblem`，#1224 A4）。
- 系统填充色（#1169，用户定）：兼容层的 `systemFill`、`secondary/tertiary/quaternarySystemFill`（也就是 `Theme.Fill.pressed` / `chip` / `well`、头像底色）
  用和 iPhone 一样的值（UIKit 在模拟器里取出来的：浅色 116–120 灰带一点蓝、8 / 12 / 16 / 20%，深色 18 / 24 / 32 / 36%）。原来是标签色的 18–36%，比 iPhone 深两到四倍，
  chip 和衬底像按下去了。每一档都还比 AppKit 自己同一档的填充（黑 / 白 3–10%）明显，衬底上的次要文字对比度深浅色都比原来高。
  选中的行不跟着变浅：壳的侧栏、收件箱列表、Persona 栏的会话列表用 `Theme.Fill.macSelected`（还是标签色 18%，接近 AppKit 不在焦点时的选中色），悬停是它的一半。

## 卡顿怎么量（Mac，#1337）

这台开发机的 Instruments 录不到显示 / 帧数据（`DevToolsSecurity` 关着、没有免密 sudo：Animation Hitches 的 hitches、display 表都是空的），
从终端发的滚轮事件也会被系统过滤（发事件的进程没有辅助功能授权）。所以从 App 里面量（`Mac/App/MacPerfHarness.swift`，只在 DEBUG / PERF_SIGNPOST 构建）：

- 主线程的 display link 记下每一帧能开始的时刻；主线程忙过一帧，下一帧就晚到。指标和 iOS 的 `FrameStats` 同一口径：晚于 1.5 帧算卡顿，
  hitch 时间 = 超出一帧的部分（ms/s），掉帧数，最长一帧，P95，主线程 / 进程 CPU（ms/s），期间 UserDefaults 写了几次、哪些键变了。
- `-showPerfRun scroll -showPerfPoint x,y -showPerfSpeed 33 -showPerfSeconds 6 [-showPerfGlide YES|live]`：按显示帧推动那一点下面的滚动视图
  （33 pt/帧 = 60 Hz 下 2000 pt/s，同 iOS 的 `ListGlide`；`-showPerfGlide YES` 同 iOS 的走法：一个方向 3 秒、回来 3 秒，到头停；不加是来回弹）。
  - **量对话页用 `-showPerfGlide live`**：来回弹，前后发 `NSScrollView` 的 live scroll 通知，SwiftUI 的滚动阶段是 `.interacting`，和用户用触控板滚一样。
    只挪 clip view，SwiftUI 不当成用户在滚：对话一直「跟着最新」、落到上次读到的地方，和推着它的这边拉扯，每帧跳回去（2026-11-04 的 342–356 ms/s 主要是这个）；
    短对话一个方向 0.5 秒就到头，剩下的时间量的是不动的列表（看结果里的 `scrolledPt`：等于两倍滚动范围就是到头停了）。
  - 用合成的滚轮事件直接交给滚动视图（`scrollWheel(with:)` 或窗口的 `sendEvent`）不行：带阶段的手势 AppKit 另有跟踪，6 秒只滚了 380 pt。
  - 卡顿可能不在主线程的 CPU 上（等渲染服务也会让下一帧晚到）：主线程 CPU 两边差不多、帧数差很多时，看是不是在反复创建 / 拆掉视图（玻璃、材质）。
    WindowServer 的 CPU 从 App 里读不到（没有权限），别指望它。
- `-showPerfRun pushes -perfCounts YES -showPerfChurn 'http://127.0.0.1:<端口>/mock/churn?n=20&ms=300&steps=0'`：页面静止，让 mock 推 20 轮
  （每台机器的会话、机器列表的心跳；`steps=0` 时只有每 4 轮对话里多一句话），报这几秒里每个视图的 body 算了几次（`PerfCount`）、每次推送主线程忙多久。
  对话页为什么重算：加 `-showPerfChanges YES`，日志里有 `ChatView` 每次的 `_printChanges()`（哪个 `@State`、哪个被观察的属性变了）。
- `-showPerfRun sections|chats`：一个一个切侧栏分区 / 打开对话，量动作到下一帧能开始的时间（主线程被占了多久）。
- `-showPerfRun resize`：窗口宽度在最宽和最窄之间来回拉（同拖窗口边；加 `-perfCounts YES` 报这几秒里各个 body 算了几次），`-showPerfRun launch`：进程启动到窗口第一帧。
- `-showPerfOut <json> -showPerfQuit YES`，`-showPerfDelay <秒>`。构建：Release 配置（`-O`、整模块）加 `SWIFT_ACTIVE_COMPILATION_CONDITIONS='DEBUG PERF_SIGNPOST'`
  （带 DEBUG 的启动参数，好配对 mock、打开页面），单独的 derivedData。窗口要在最前面（被挡住的窗口 AppKit 不画，数字会偏好看）。
- 主线程在忙什么：跑的时候 `sample <pid> 5 1 -file x.txt`（系统自带），按主线程的调用树统计；`sample` 会暂停进程，那一轮的帧数据不算。
- 这台 Mac 是几个车道共用的：别的车道在编译时负载上百（2026-11-04 晚上 250–310，20 核），帧数据跟着乱跳（同一个包连跑两次 366 和 1.8 ms/s），
  主线程 CPU 和 body 次数还算稳。量帧之前看 `sysctl vm.loadavg`，等它降到几十以下；比前后用交替跑，别一个包连跑完再跑另一个。

2026-11-04 第一轮（Mac Studio，1920×1080@60Hz；mock：`MOCK_AGENTS=1`（137 个 Agent）`MOCK_LONG_HISTORY=1` `MOCK_LONG_ASSISTANT=1`；各 2 次）：

| 场景 | Persona Mac | iPhone（17 Pro 模拟器，同一 mock、同一走法） |
| --- | --- | --- |
| Agents 列表滑动（2000 pt/s） | 5.6 / 91.4 ms/s，P95 16.7–28 ms | 13.5–14.3 ms/s，P95 16.7 ms |
| 长 Agent 对话滑动 | 342–356 ms/s，P95 50 ms，主线程 590 ms/s | 9.7–18.8 ms/s，P95 16.7 ms |
| Persona 长主对话滑动（60 轮） | 226–368 ms/s，最长 107–210 ms | — |
| 收件箱列表滑动 | 1.5–2 ms/s | 15.6–15.9 ms/s |
| 拖窗口改宽度 | 909–912 ms/s，P95 264–365 ms（约 4 fps） | — |
| 打开对话 | 中位 293–356 ms | — |
| 切侧栏分区 | 中位 132–136 ms（Agents、Persona 约 220 ms，最慢 555–727 ms） | — |
| 冷启动到首帧 | 1.87–2.0 s | — |

找到的原因（主线程采样）：
- `@AppStorage` 在任何 UserDefaults 写入时都让持有它的视图重算：拖窗口时分栏每帧写一次 `NSSplitView Subview Frames …`，打开对话写 `dev.openChat`，
  外壳和 Agents 列表跟着重算，137 行的 SwiftUI List 每次整表重新比对（`OutlineListCoordinator.diffRows`）：拖窗口时约占主线程的一半，打开对话时约 28%。
  热的视图改用 `@StoredValue`（`Persona/App/StoredValue.swift`，用法同 `@AppStorage`，只有自己那个键的值变了才重算）。改后：拖窗口 P95 264–365 → 33–103 ms，
  打开对话中位 293–356 → 196–224 ms。
- 对话页滚动：上面那一轮的数字不算数（见 `-showPerfGlide live`）。按用户滚动的方式量（`live`，2000 pt/s 来回，负载 15–19，交替各 3 次）：
  长 Agent 对话 71–82 ms/s、P95 31–33 ms。顶栏一直开着或一直折着都只有 6–11 ms/s，卡在开 ↔ 折切换的那一下：折起时整个顶栏换成一份 `.hidden()` 的
  （`GoneWhen` 里的 `if`），玻璃按钮、标签条每折一次、开一次就拆了重建（主线程 CPU 两边差不多，帧晚到）。Mac 上改成只对 VoiceOver 隐藏
  （它这时已经透明、也不接点击）。iPhone 不变（UIKit 的玻璃按钮透明了也点得到，要真藏起来）。改后（交替各 6 次，负载 21–30，中位数）：
  78.3 → 33.7 ms/s，P95 31.7 → 24.2 ms；折起时 VoiceOver 照样读不到它的按钮（cua 查无障碍树：开着有「更多」，折起前后都没有）。
  剩下的 30 ms/s 左右还是开 ↔ 折那几帧（高度、透明度、简版条插进来）；一直开着或一直折着是 6–11 ms/s。
  用户定 Mac 上不折叠（2026-11-05）：顶栏一直开着。前后交替各 11 次（负载 15–40）：卡顿中位 56.7 → 41.3 ms/s，P95 25.0 → 24.6 ms，主线程 264 → 229 ms/s。
  比前面安静时量的「一直开着 6–11」高：这一晚每一轮都比前一晚多 20–40 ms/s（两边都是，别的车道在用机器），还有两轮整轮变慢（287、342），原因没查到。
- 推送让对话页整页重算（2026-11-04，#1337）：对话页的副标题、「正在操作」读了 `model.machines`，每台机器每次心跳整页带所有行重算；
  标签条读 `siblings(of:)`，它在里面读 `allSessions`。改成 `namedMachine(_:)`、每个空间一个格子之后，mock 推 20 轮心跳（`steps=0`）：
  `ChatView` 30 → 10 次（剩下的是那 5 句新话），气泡和正文 365 → 125 个，每次推送主线程 4.2 → 1.2 ms；带新步骤的推送 7.9 → 3.9 ms。
  还剩：对话里来了新内容，屏幕上的每一行都重算一遍（每句新话 12 个气泡、13 段正文）。
- 连上 hub 时注册手机工具（2026-11-05，#1337）：`PhoneToolRegistry.enabledSpecs` 在主线程上按工具逐个问 EventKit 的授权，每次都是一次同步的 XPC（第一次还要建连接）。
  改成在后台问 EventKit、每个来源只问一次（`CalendarAccess.statusesOffMain`）：冷启动的主线程采样里 `enabledSpecs` 145–195 → 37–48 ms，
  其中 EventKit 107–157 → 11–16 ms（各 3 次交替跑，负载 75–125，首帧时间跟着负载乱跳，不拿它比）。iPhone 共用这段，同样受益。
- 拖窗口时设置页、收件箱还在每帧整页重算（2026-11-05，#1337）：`-showPerfRun resize -perfCounts YES` 报出 `SettingsView` 的 body 一帧两次，
  因为它和收件箱、View 菜单、检查器、旁边的列表还拿着 `@AppStorage`，分栏每帧写一次 UserDefaults。改成 `@StoredValue` 后（负载 16–42，各 2 次交替）：
  设置页每帧主线程 19.0 / 22.0 → 5.7 / 6.0 ms，卡顿 288–292 ms/s → 0（60 fps）；收件箱 17.7 / 19.6 → 12.9 / 11.7 ms，卡顿 264–338 → 78 ms/s；
  对话页不变（约 28 ms、约 500 ms/s，大头在别处）。还拿着 `@AppStorage` 的文件只能减少（`AppStorageRatchetTests`）。
- 拖窗口时对话页每帧约 20 ms（2026-11-05，#1337）：用 `-perfHide`（临时开关，没进代码）把对话页的几块逐个藏起来量，窗口和侧栏约 6.4 ms、
  顶栏约 5.8、输入框约 4.8、列表约 4.5，每块单独都是 60 fps，加起来超预算、掉到约 30 fps。顶栏副标题和工作行摘要的 `ViewThatFits`
  每个宽度都把两个候选重新量一遍（顶栏里它约占 3 ms），换成在布局缓存里记下理想宽度的 `FirstFitting`（`Components.swift`，和 `ViewThatFits` 像素一样，`FirstFittingTests`）。
  11 次交替跑的中位数（长历史、负载 9–13）：卡顿 492 → 206 ms/s，4 秒里的帧 121 → 189（满帧 240），每帧主线程约 20.4 → 15.5 ms。剩下的在输入框和列表。
- 拖窗口时对话页剩下的（2026-11-05，#1337，#2125 之后约 15.5 ms/帧）：输入框里 AppKit 的文本框约 2 ms（换成单行的也只省 0.4），列表的滚动视图本身约 2 ms
  （行换成固定高度的方块只省 0–1.6 ms，`onScrollTargetVisibilityChange`、`onScrollGeometryChange`、`scrollTargetLayout`、`ChatPin.Finder` 各自都在 ±0.3 ms 内），
  窗口和侧栏约 6 ms（设置页也是这么多）。没有哪一块单独超过量的误差，先停在这里。
- 冷启动时对话页第一次画（2026-11-05，#1337）：主线程采样里 `ChatView.body` 约 108 ms，几乎全是 Swift 运行时在建类型的元数据
  （`lazy protocol witness table accessor for type ModifiedContent<…>`、`swift_getTypeByMangledName`）：页面外层约 60 个修饰符串成一个类型，嵌套越深越贵。
  在 `chatPresentation`、`chatActivity`、`chatLifecycle` 的开头各用一个 `AnyView` 隔开（每次都是同一个类型，下面的身份和状态不受影响）。
  11 次交替冷启动的中位数（负载 10–13）：`ChatView.body` 106 → 49 ms，首帧 1971 → 1713 ms（首帧跟着负载跳，参考）。
- 外壳读 `MacTitleRowTaken` 这个 preference（打开对话时调用树里约 9.4%）：这一块大多是页面自己的更新在合并 preference 时被拉着算，拿掉不一定省这么多，要量过再改。

## SSH 隧道（Mac）

和 iOS 同一套（`apple/Packages/PersonaSSH`，见 [network.md「经 SSH 隧道连 hub」](network.md#经-ssh-隧道连-hub直连不通时的后备)、[ios.md「SSH 隧道」](ios.md#ssh-隧道直连不通时的后备891)）：设置 › hub › SSH 隧道，配对页的「经 SSH 隧道连接」。
沙盒里的 App 调不了 `/usr/bin/ssh`、读不到 `~/.ssh`，所以隧道在进程里实现、密钥由 App 自己生成；本机回环上的监听用已有的 `com.apple.security.network.server`
权限（端口转发也用它）。Mac 不进后台，隧道一直开着，直连通了会在 2 分钟内换回直连。
截图：`mac_shot.sh` 的 `-macSnapshot` 不读钥匙串，用 `-seedSSHKey` 给定测试密钥（`docs/testing.md`「SSH 隧道」）；截图模式下公钥注释里的设备名一律是「Mac」（不露这台 Mac 自己的名字）。
配对页（`-tab settings -openPairSheet YES`）上的「经 SSH 隧道连接」在 Mac 上是一行加「设置…」按钮，点开是盖在配对 sheet 上的第二个 sheet（`-openPairSSH YES`），底栏「取消」「测试连接并保存」；
不在配对 sheet 里推页面（推进去 sheet 会变窄、表单被裁，#1170）。设置 › hub › SSH 隧道 在详情栏里，复制公钥的两个按钮是并排的标准按钮，「测试连接并保存」在页尾右边。

## 菜单栏、Dock 角标、通知（Mac）

Dock 图标右键（`apple/Mac/App/MacDockMenu.swift`，有测试）：「需要你」「做完了」两组（和菜单栏列表同一个排序，各最多 8 个，点一下打开对话），再是「新建 Agent 对话…」。

`apple/Mac/App/MacAttention.swift`、`MacMenuBar.swift`，测试 `apple/PersonaMacTests`（`JarvisMac` scheme 的 test）。

- App 开着、连着 hub 时：`MacAttentionWatcher` 盯着 AppModel 的会话列表（WebSocket 一直连着），
  有 Agent 刚变成「需要你」/「完成未看」就发一条本地通知，规则和 hub 给手机推送的一样，读同一份 设置 › 通知（`NotifyPrefs`：需要你处理时、
  Agent 做完时 不推送 / 只推长任务（这一轮干了 `long_turn_seconds`，默认 60 秒以上）/ 每次都推）。不发的：App 在前台时正开着的那个对话、屏蔽了的、
  列表里刚出现的、答完紧接着做完的（同一次停下）、Persona 对话做完（它问你时照发）、体验模式、WebSocket 第一次连上后 3 秒内
  （之前的列表是上次退出时的缓存，和第一次刷新的差别是 App 关着时的旧事；按连上算，不按启动算，网络晚到也不会补发一堆）。
  通知权限在配对后 / 启动时就问（`PushRegistrar`，和 iPhone 一样：App 关着时来的远程推送只有之前答应过才显示）；还没答应时 watcher 要发通知会再问一次，答应了再发（问的时候加进去的会丢）。
  App 关着或断开时由 hub 推送，见下面「远程推送（Mac）」。屏蔽 / 取消屏蔽立刻改 Dock 角标。
- 通知带和 hub 推送一样的 userInfo（`machine_id` / `session_id` / `interaction_id`）和类别：授权有「允许 / 拒绝」，计划有「批准 / 打回修改」，
  点按钮由 `MacAppDelegate` 直接交给 hub，点通知打开对话；同一个 Agent 的第二条「做完了」替换第一条。
- Dock 角标：需要你的数量（不算屏蔽的），和侧边栏的角标同口径。
- 菜单栏（`MenuBarExtra`，菜单样式，主窗口关了也在）：图标旁是需要你的数量；菜单里分「需要你」（等得最久的在前）、「正在工作」、
  「做完了，还没看」（最近 5 个）。需要你的一项是子菜单：说了什么 + 卡片上一点就能答的那几个（单个问题的选项、允许 / 拒绝、批准计划、
  hub 读出来的终端短选项）+ 打开对话；别的点一下打开对话。最后是「打开 Persona」「退出 Persona」。

## 远程推送（Mac）

2026-11-02 起（#420）。`apple/Persona/App/PushRegistrar.swift`（和 iPhone 共用）、`MacAppDelegate`，hub 在 `hub/push.go`、`hub/desktops.go`。

- **账号**：iPhone 和 Mac 是同一个 bundle id，App ID 是 UNIVERSAL、本来就开着 Push Notifications，开发者账号不用改。
  entitlement 在 macOS 上叫 `com.apple.developer.aps-environment`（`project.yml`）；本机编的不签名的版本没有它，注册失败、只有本地通知。
- **登记**：配对后 / 每次启动问通知权限，答应了就注册 APNs，拿到 token 后 `POST /api/v1/devices`，kind `mac`，topic 是 bundle id（和 iPhone 同一个），
  TestFlight 包 `production`、DEBUG `sandbox`。hub 不分平台，按设备登记的 topic 和环境推（APNs 的地址 macOS 和 iOS 一样）。
- **不重复**：App 开着、连着 hub 时本地通知已经有了（上面的 `MacAttentionWatcher`），所以 `register_desktop` 带上推送用的 `device_id`，
  hub 在这期间不给这台 Mac 推「需要你处理」「Agent 做完」；助手按你的要求发的提醒（`notify_user`）、hub 更新的通知 App 自己不会发，照推。
  App 关了、Mac 睡眠断开（hub 的心跳约 35 秒发现）以后 hub 照常推。
- **静默唤醒**（`jarvis_wake`）不发给 Mac：那是叫醒 iPhone 跑 phone_* 工具的，Mac 是桌面端。
- **按钮**：hub 推送的 category 和 iPhone 一样，Mac 启动时注册的是同一套（`NotificationActions.categories`）：授权「允许 / 拒绝」、计划「批准 / 打回修改」（可输入），
  提问和终端没有按钮，点开到对话。点按钮、点通知都走 `MacAppDelegate`，和本地通知一样。
- **退出**：在 iPhone 上退出这台 Mac（或 设置 › 设备 里撤销）时，hub 连它登记的推送设备一起删掉（登记时记下了是哪个设备凭据），之后不再推；
  它的通知设置也跟着没了，重新配对从默认值开始。iPhone 的撤销也一样。
- **推送中继**（2026-11-02 起，#432）：没有自己 APNs key 的 hub 走中继，正文是密文（`docs/push-relay.md`），由 Mac 的通知服务扩展
  target `PersonaMacNotify`（module 仍为 `JarvisNotify`）解密后再显示（和 iPhone 的 `PersonaNotify` target 同一份代码、同一个 Info.plist、同一个 bundle id `<bundle id>.notify`，App ID 是 UNIVERSAL）。
  - 为什么不在 App 里解密：hub 不给连着的 Mac 推提醒，中继推来的只会在 App 关着或断开时到，系统直接显示，App 改不了内容。
  - 私钥在 App 和扩展共用的钥匙串组 `<team>.<bundle id>.push`（两边的 `keychain-access-groups`）。macOS 上 `PushCrypto` 要加
    `kSecUseDataProtectionKeychain`：默认的老式文件钥匙串不认访问组，条目只属于创建它的 App，扩展读不到，只能显示「有新消息」。
  - Mac 的扩展必须开沙盒；本机不签名编的版本没有这些 entitlement，拿不到 data protection 钥匙串，不向中继登记（照旧只有本地通知）。

## 侧边栏的「知识库」「机器」（Mac）

选中的 知识库 / 机器 跨启动记住（`mac.section`；tab 由 AppModel 记）。启动参数指定了 tab / 对话（`-tab`、`-openSession`…）时以参数为准、清掉记住的选择。

壳的侧边栏在 iPhone 的四个 tab 之外多两项：知识库（`KnowledgeView`，手机上在 Persona 里）、机器（`MachinesView`，手机上在抽屉 / Agents / 设置里），
⌘1…⌘7 依次是 Persona、Agents、收件箱、知识库、机器、设置、待办。这两项不是 AppTab：选中时记在 `MacShellState.section`，`AppModel.selectedTab` 不动；
通知、⌘K、链接把 tab 换走（或打开对话）时清掉，回到 tab 对应的那一项（`MacSection.shown / pick`，有测试）。DEBUG：`-macSection knowledge|machines`。

## Agent 菜单（Mac）

`apple/Mac/App/MacAgentCommands.swift`（有测试）。对着屏幕上的对话（`AppModel.visibleSession`）：中断 ⌘.（工作中才能点）、
批准当前待处理 ⇧⌘⏎（允许工具 / 批准计划，计划按「自动接受编辑」批，和通知上的「批准」一样）、拒绝 ⇧⌘⌫（只拒绝工具；打回计划要写理由，在卡片里）、
下一个需要你的 ⌃⌘N（等得最久的在前，循环）、标为已读、置顶 / 取消置顶、屏蔽 / 开启通知、拷贝会话 ID、拷贝恢复命令。
回答 ⌥⌘1…9（子菜单，2026-11-01）：只有一个问题、单选时列它的选项（最多 9 个），终端对话框列不用输入的选项；Claude 标的「(Recommended)」写成「（推荐）」，
回传的答案还是原文（`MacAgentCommands.choices`，有测试；菜单栏图标的快捷回答同样改了）。多个问题、多选、要写字的还在卡片上。
不用 ⌘1…9：⌘1…7 是侧边栏分区。`-openSession <m>/<s> -macMenuLog YES` 把 Agent 菜单当时的样子打进日志（锁屏下没有 key window，
菜单不打开就不刷新，它先替菜单走一遍打开时的刷新）：演示里 `macbook/…0002` 的问题是「Swift Testing（推荐）⌥⌘1、XCTest ⌥⌘2」。
交给 Persona ⌘J（和行菜单一样，把 Agent 的状态和最后的回复交给主对话）；重命名… / 清空上下文… / 关闭 Agent…：经 `MacShellState.chatRequest` 让对话页弹它自己的对话框
（和 ⋯ 菜单、输入框的 /clear 是同一个），清空只对 herdr 里的 Claude 可点、关闭只对还在跑的。截图：`-openSession <m>/<s> -macAgentAsk rename|clear|close|inspector`。

快捷键和清单最早写的不一样，因为撞了：⌘⏎ 是输入框的发送（`ComposerParts`），⌘⌫ 在输入框里是删到行首（菜单快捷键会抢走），
⇧⌘N 是新建 Persona 对话，⌥⌘N 是收件箱的写邮件。
清单里「在新窗口打开 ⌥⏎ / ⌘⏎（列表里）」没接：菜单快捷键在整个窗口生效，⌘⏎ 是输入框的发送，⌥⏎ 在文本框里也有用；只在列表获焦时才算数得靠列表的键盘处理，要真按键才验得了（锁屏下发不了），先只放在 Agent 菜单和行的右键菜单里。

## 对话单独开窗口（Mac）

`apple/Mac/App/MacChatWindow.swift`：`WindowGroup("对话", id: "chat", for: SessionKey.self)`，里面就是对话页本身，窗口标题是对话标题，
几个 Agent 可以并排或放到另一块屏幕上。入口：Agents 列表行的右键菜单「在新窗口打开」、Agent 菜单「在新窗口打开」、按住 ⌥ 点 Agents 列表的行（含「需要你处理」的细列表行）。
⌥ 点（#3223，`apple/Mac/App/MacOptionClick.swift`，`MacOptionClickTests`）：行上盖一层 AppKit 视图，只有「只按着 ⌥ 的左键点按」命中它（`hitTest` 看正在分发的事件），
别的点按、悬停、滚轮都穿过去，所以行的 NavigationLink、列表的选中和行里的按钮都收不到这一下：主窗口不跟着跳，收件箱的选中不变。收件箱的行 ⌥ 点同 ⏎ / 双击（这件事的窗口），
Agent 的行打开它的对话窗口。测试里经真的 `NSWindow.sendEvent` 分发：普通点按按到下面的按钮、⌥ 点只到这一层；在屏幕上的窗口里真按没试过（这台机器不能发合成点击），列表里的 NavigationLink 也只经这条推断。
菜单（Commands）没有自己的 `openWindow`，经 `MacShellState.chatWindow` 交给主窗口的 `MacWindowOpener` 打开。
DEBUG：`-openChatWindow <machine>/<session>`；`-macSnapshot` 会把这个窗口另存成 `<名字>.chat.png`。
窗口里的对话是这个窗口的第一页（`SplitDetail(isFirstPage: true)`），对话页自己画的返回箭头不显示。
Agent › 在新窗口打开终端：那个 Agent 的终端单独一个窗口（`MacTerminalWindow`，`WindowGroup(id: "terminal", for: SessionKey.self)`，标题「终端 · Agent 名」），
和对话里「终端」浮层是同一个面板，⌘W 关；只有 herdr 里的 Agent 可点。截图：`-openTerminalWindow <m>/<s>`（存成 `<名字>.chat.png`）。
菜单要开的窗口（对话、终端）由菜单栏图标上的 `MacWindowOpener` 打开：主窗口关了、只剩一个对话窗口时也能开（原来挂在主窗口上，主窗口一关就点了没反应，两个截图参数都验过）。
「下一个需要你的」、⌘[ / ⌘] 打开的对话在主窗口里，先把主窗口叫出来（关了就重开，最小化了就还原）。终端窗口顶上的手柄点了关窗口。
手机工具的写操作确认（「Claude 想在这台 Mac 上执行」）挂在主窗口上，弹之前先把主窗口叫出来，并让 Dock 图标跳（`requestUserAttention(.criticalRequest)`：App 在后台时系统可能不让它直接到前台）；没连真 hub，没实际触发过。
只在菜单栏图标上挂一个 opener：主窗口上再挂一个时，两边都响应，同一个对话开出两个窗口（`-macWindowLog YES -openChatWindow … -openTerminalWindow …` 量过：2 个对话窗口；去掉后 1 个对话、1 个终端）。
待查：用户在系统设置里把菜单栏项藏起来时，它的 label 还在不在驱动这个 opener，没试过。窗口号探针除了刚进窗口时读一次，变成 key 时也会再读。

## 显示 / 前往菜单（Mac）

显示 › 隐藏侧边栏 / 显示侧边栏 ⌃⌘S（2026-11-01）：收起窗口最左边的分区栏（`MacShell`），跨启动记住（`mac.sidebar.hidden`），收起后 ⌘1…7 照样换分区；页面自己的列表栏不受影响（它们有自己的按钮）。快捷键测试里有它，截图：`-mac.sidebar.hidden YES`。
侧栏脚的连接状态收起时挪到页面底部（只在连不上 hub 时出现）：知识库 / 机器自己没有显示它的地方（`-macSection machines -simulateOffline YES -mac.sidebar.hidden YES` 截图看过）；Agents 页的标题位置本来也写「等待网络」，收起时两处都有，先这样。
分区上的数量角标也跟着收起：Agents 的需要你还有 Dock 角标和菜单栏图标，收件箱的待处理数收起时就看不到了（review 第 7 轮记下，没改：收起本来就是不要这一栏）。

`apple/Mac/App/MacViewCommands.swift`（有测试）。显示 › 放大字号 ⌘+ / 缩小字号 ⌘- / 标准字号 ⌘0：在 设置 › 字号 的 小 / 标准 / 大 之间走一步，到头就停，
和设置里改是同一个值（整棵视图按新字号重建：`macLook` 的 `.id(model.textSize)`，字体读的是 SwiftUI 不观察的 `Theme.Font.scale`）。前往 › 上一个 / 下一个 Agent ⌘[ ⌘]：按 Agents 列表的顺序（`AgentListOrder`：需要你 → 完成未看 → 工作中 → 已看；排序选了最近更新、且不是按状态分组时只按时间），
循环，不含 Persona 对话；没开着 Agent 时从第一个开始。
编辑 › 查找… ⌘F（`apple/Mac/App/MacFind.swift`，有测试）：当前窗口的搜索框获焦（工具栏的搜索项优先，再是页面里的；Agents、知识库用 `-macFind YES` 验过），
没有搜索框的页面什么都不做；焦点在自带查找栏的文本视图（diff、输出）里时打开它的查找栏。
文件查看器里的代码也走这个 ⌘F（2026-11-01，`Mac/Code/CodeFindBar.swift`，匹配逻辑 `CodeFind.swift`，有测试）：顺序是 自带查找栏的文本视图 → 响应链上的
`MacFindable`（代码视图）→ 窗口里的搜索框 → 窗口里看得见的 `MacFindable`（文件刚打开、还没点过代码）。代码上方出查找栏：不区分大小写，所有匹配浅黄、
当前的系统查找黄并滚到看得见，「3 / 14」或红色「没有找到」；⏎ 下一个、⇧⏎ 上一个（两头循环），Esc / ✕ 关闭、焦点回到代码。这三个键用本地按键监听在窗口之前接住
（文件查看器的「完成」占着 Esc，不先接就会关掉整个 sheet），拼音组字时让给输入法。截图：`-openFilePath <路径> -macFileFind <查询>`（体验模式的 hub 在 Mac 上给一个编造的 Swift 文件）。
查找下一个 ⌘G / 查找上一个 ⇧⌘G（2026-11-01，`MacFind.findNext`，`CodeFindNextTests`、`MacFindTests`、`MacMenuShortcutTests`）跟着 ⌘F 走：带查找栏的文本视图（diff、输出）跳它的匹配，
代码查看器跳自己的查找栏，查找栏关着时用上次的词重新打开。没做：区分大小写 / 整词 / 正则、行内部分选择；每次输入在主线程上重搜整个文件，10 万行的文件没量过。真按键没试过（锁屏）。
显示 › 紧凑列表、分组方式（按仓库 / 按项目 / 状态 / 机器 / 按待办）、排序方式（状态优先 / 最近更新，按状态分组时不可选）、全部折叠 / 全部展开：和 Agents 列表左上 ⋯ 菜单是同一组设置（`dev.compact`、`dev.grouping`、`dev.sort`、折叠由列表登记的 `AgentsListMenu.Actions` 做）。
会话信息在 Mac 上是对话右侧的一栏（Agent › 显示会话信息 ⌥⌘I，或点标题；`apple/Mac/Chat/MacInspector.swift`），宽度可拖、记在 `mac.inspector.width`，
窗口太窄时盖在对话上。没用 SwiftUI 的 `.inspector`：放在 Agents 分栏的详情页里整栏是空的；`SessionInfoSheet` 在 Mac 上也不再包 NavigationStack（会接管详情栏的导航、对话页消失）。
回到最新 ⌘↓ 是对话页右下角 ↓ 按钮的快捷键（悬停提示同），只在往上翻过、按钮出现时有效；其余时候 ⌘↓ 还是输入框的「移到末尾」。
显示 › 刷新 ⌘R（2026-11-01，`MacViewCommands.swift`、`Mac/App/MacPageRefresh.swift`，`MacViewCommandsTests`）代替 iPhone 的下拉刷新，整个 App 只有这一个 ⌘R：
自己加载数据的页面（知识库、机器和它的监控页、对话的文件 / Git 面板）用 `macRefreshes` 登记自己的加载，⌘R 刷新 key 窗口里最后登记的那个；
没有就按分区刷新 hub 的数据（收件箱 `InboxStore.refresh`，Persona `refreshAssistant`，其余 `AppModel.refresh`）。收件箱、文件、Git 工具栏上的刷新按钮不再带 ⌘R。设置的子页面不在内；真按键没试过（锁屏）。

## 快速打开、新建（Mac）

`apple/Mac/App/MacQuickOpen.swift`（搜索、排序，有测试）、`MacQuickOpenPanel.swift`（浮层）。

- ⌘K（菜单「前往 › 快速打开…」）：窗口上方浮起一个像 Xcode「Open Quickly」的面板，焦点在搜索框，↑↓ 选、⏎ 打开、Esc 或点外面关。
  搜 Agent（标题 / 目录 / 机器 / 空间 / 最后一句）、Persona 对话、当前对话目录下的文件、收件箱（标题 / 发件人 / 预览）、窗口的四个分区、两个「新建」。
  标题开头命中 100、包含 80、每个词都在别处出现 40；同分时需要你的在前、再按时间；分组顺序固定（Agent、Persona 对话、收件箱、前往、动作），
  除非后面的组命中得更好（搜「设置」时「前往 › 设置」排在只是提到设置的 Agent 前面）。没输入时列出全部，需要你的在前。
  DEBUG：`-openQuickOpen <搜索词>`（`""` 为空）。
- 文件（#2916，照网页 #2438）：窗口里开着一个对话时，⌘K 打开就按它的目录请求一次 `GET /machines/{mid}/fs/find`（`MacFileIndex`，同一目录 20 秒内复用），
  多一组「文件」（在 Persona 对话之后）。匹配和网页 `web/src/lib/file-find.ts` 同一套算法和权重（`MacFileFind.swift`，有测试）：每个词都要按顺序命中，
  词首、路径分隔后、camelCase 的峰、连续的字符、文件名本身加分，同分时短路径在前，最多 50 条；接着上一次输入往下打时只在上一次的结果里找。
  行的标题是文件名、下面是所在文件夹，命中的字符加粗、用强调色。和别的组比按「标题包含」以下、「只是各处都有这几个词」以上排。⏎ 用文件查看器打开（同对话里点文件）。
  索引还没回来时写「正在建立文件索引…」，读不到写「读不到这个目录的文件」；没开对话时没有这一组、占位文字不变。
  没做：网页的 Mod+Enter 拷贝 `@path`；老的 node 没有 `fs/find` 时网页逐层走 `/fs`，Mac 不走，直接显示读不到。体验模式在 Mac 上给一份编造的文件列表。
  截图：`scripts/app/mac_shot.sh files -openSession <m>/<s> -openQuickOpen fore`；加 `-openQuickOpenPick YES` 在列表回来后对第一个文件按 ⏎（截出 `<名字>.sheet.png`）。
  第二个 `.sheet` 直接挂在根视图上、和「新建 Agent」的挨着时，体验模式打开 Agent 对话就在窗口布局里崩（`_crashOnException`），所以它挂在一个透明的背景视图上。
- 新建 Agent 对话 ⌘N（菜单「文件」、菜单栏、⌘K）：iOS 的新对话表单放在窗口的 sheet 里，「开始对话」在 Mac 上是 sheet 底部「取消」旁边的默认按钮（⏎），
  不是 iOS 浮在底部的大按钮；开起来后在 Agents 里打开。新建 Persona 对话 ⇧⌘N：切到 Persona 的新对话。没配对时这几条命令不可用。DEBUG：`-openNewAgent YES`。
- 截图里 ⌘K 面板后面透出列表：离屏截图把 `List` 的行单独再画一遍贴回原处（见上面「截图和屏幕上的差别」），盖在了浮层上面，屏幕上不会这样。

## 链接交给系统时（Mac）

`apple/Mac/Link/LinkPolicy.swift`。Mac 上 `NSWorkspace.open` 不问就照 URL 去做（`file://` 的 App 直接启动、`smb://` 挂共享、别的 App 的 scheme），
而链接来自 Agent 的回复和别人的消息：App 自己不显示的链接，只有 http / https / mailto 交给系统，别的拷贝到剪贴板并提示。
`jarvis://` 也不放：消息里一条 `jarvis://pair` 链接点一下就会把 App 换到它写的那个 hub。从浏览器、Finder 打开的配对链接照常处理，
但已经连着真 hub 时不换（提示先在 设置 › 服务器 里断开；体验模式照常让位），`MacPairing`。
对话、飞书消息 / 文档、链接面板、定时任务说明里的 `.systemAction`，和兼容层的 `UIApplication.open`，Mac 上都走这里（iOS 不变）。

从网页版 Mac App（PR #162，已关）带过来的几项健壮性修复，在这一版里逐个对过：401 不清配对（iOS 的 AppModel 从不因 401 解除配对，不用改）、
退出体验模式不清钥匙串（体验模式只在钥匙串里没有配对时才开，退出时没有东西可清）、发送途中输入不丢（发送走 ChatStore 的待发气泡和 Outbox，失败可重试）、
对话图片解码不在主线程（`ChatThumbnail` 本来就在 `Task.detached` 里解码）。只有链接这一项需要改。

## 收件箱、飞书、邮件、链接（Mac）

`apple/Mac/Inbox/`、`apple/Mac/Link/MacLinkPanel.swift`。逻辑和行、会话、邮件页都是 iOS 那一份，Mac 只换了外面的组织方式：

- 三块独立的视图，给主窗口的三栏接（区域 A）：`MacInboxFilters`（筛选栏「收件箱」「类型」两组行：全部 / 飞书 / 各个邮箱，@我 / 私聊 / 群聊 / 邮件 / Agent，
  带「需要你」的数量；壳的那种行 `MacSidebarRow`，点一行改 `MacInboxState.shared.filter`。原来是侧边栏样式的 `List`，窗口不在前台时字整个变浅灰，
  2026-11-02 用户：看不太清）、
  `MacInboxList`（列表栏）、`MacInboxDetail`（详情栏）。三者共用 `MacInboxState.shared`（筛选沿用 iOS 的 `inbox.source` / `inbox.kind`、搜索词、选中、展开的分组、邮箱列表、要打开的弹层）。
  A 接好三栏之前，`MacInboxPage`（筛选列 | 列表 | 详情，列表宽度可拖、记在 `mac.inbox.listWidth`）放在壳的「收件箱」里。
- 列表：系统选中（⇧ / ⌘ 多选），↑↓ 移动，选中显示成和壳一样的灰色圆角底（`MacHoverRow`：系统在活跃窗口里把选中行填成主题色，
  行里自带颜色的字和按钮——草稿卡片的标题、「修改」、AI 的星标——被盖住），**E 或 ⌫ 标为已处理**并选中下一件（像 Mail 删信），⏎ / 双击 / ⌥ 点在新窗口打开（Agent 行：打开它的对话），
  右键：在新窗口打开、AI 草拟回复 / 查看草稿、交给 AI、已处理 / 移回（多选时「N 件都标为已处理」）、拷贝内容、在飞书中打开；触控板左滑 = 已处理；悬停有浅底。
  工具栏：刷新（⌘R 是 显示 › 刷新，收件箱在屏幕上时刷新它）、写邮件 ⌥⌘N、管理邮箱；搜索在工具栏（`.searchable(placement: .toolbar)`）。分组标题可点开 / 收起，「全部看过」「全部清除」照旧。
- 详情：飞书会话（从第一条新消息开始）/ 邮件 / 云文档条目；右侧一栏（`Mac/Inbox/MacInboxActions.swift`，#831，2026-11-03 用户：放在工具栏里只有图标、不起眼，要像 iOS 一样醒目）：AI 摘要、「AI 分身正在处理」、「线索」（#3071，见 `docs/inbox.md`「App」），下面是带文字的按钮：AI 草拟回复 / 查看草稿（主按钮；只能在原 App 回的来源换成 在信息里回复 / 在 Telegram 里回复 / 打开）、交给 AI、已处理（已处理的换成 移回，其余照旧，同 iOS）。列表栏默认宽度从 380 改成 340，默认窗口下放得下这一栏。详情窄得放不下这一栏（`Theme.Size.macInboxActionsWidth` 加上留给条目的 `macInboxActionsPageMin`）时，同样的内容变成条目上方的一条，按钮放不下文字就只留图标（悬停有说明）。工具栏里不再放这几个。
  窗口标题是条目标题，副标题是来源。人、别的会话这类飞书页面在详情栏自己的栈里推进去；飞书文档、网页链接在右侧的链接面板里开。多选时详情是「已选 N 件 · 全部标为已处理」。
  弹层（草稿、交给 AI、写信、邮箱、添加邮箱）是窗口内的 sheet，`.macSheet()` 给最小尺寸并把 `Form` 换成 `.grouped`（Mac 默认的两列表单会把 iOS 的分组行挤到标签列、截掉）。
  后来 `macLook` 在整棵树上设了 `.formStyle(.grouped)`，sheet 也继承，所以没套 `macSheet` 的表单（新建定时任务等）也是分组的了（截图对比过 `-openAssistantPage newRoutine`）；`macSheet` 只剩给最小尺寸。连接器里「这台 Mac」的图标在 Mac 上是笔记本（`Theme.Icon.thisDevice`）。
  grouped 表单把带标题的 `TextField` 画成「左边标签 + 右边输入」，多行的提示词 / 正文因此只占右半边：Mac 上这几处改成 `prompt:` + `.labelsHidden()`（新建定时任务、添加邮箱的整段粘贴截图看过；新建对话的第一条消息、写信正文同样改了，在截图范围外没看到）。
  知识库详情 › 打标弹出的 sheet 没继承 `macLook` 的 grouped，还是两列（设置里推入页面弹出的新建定时任务却继承了，原因没查清），这类 sheet 自己套 `macSheet()`：打标（`-showKnowledgeEntry projects/weather-app -showKBEditor YES` 截图看过，标签、滑块占满一行）、新建对话（知识库 › 开 Agent 那条路径没截到）。
- 链接面板 `.macLinkPanel($link, key:)`：详情栏右侧并排（页面变窄、不被盖住；窗口太窄放不下两边时盖在上面），宽度拖、记在 `mac.linkPanel.width`；
  头部：后退 / 前进、标题和域名、重新载入、⋯（在浏览器打开、在单独窗口打开、拷贝链接、分享）、关闭（⌘W，面板开着时先关面板）。
  网页来自 `WebViewPool`（和 iOS 同一个池，预加载、复用），右键选中文字有「AI 修改」「作为上下文」；飞书文档 / 公司圈在面板里用原生阅读器（右上「把这篇文档作为上下文」）。
  `key` 是对话（对话页）；收件箱、知识库里没有，「作为上下文」进 Persona 主对话的输入框、「AI 修改」直接问 Persona。
  `.macOpensLinksInPanel($link)` 把一块视图里的 `openURL` / `openLink` 接到面板上（收件箱详情用；邮件 HTML 里的链接仍交给浏览器）。对话页、知识库在 Mac 上也换成了面板（`#if os(macOS)`）。
- 单独的窗口：`WindowGroup(id: "link", for: URL.self)`（`MacLinkWindow`）、`WindowGroup(id: "inbox-item", for: String.self)`（`MacInboxItemWindow`）。
- DEBUG：`-openInboxFirst YES`、`-openInboxMatch <标题里的字>` 选中一件，`-showInboxAction draft|handoff` 打开弹层，`-openInboxLink <url>` 打开链接面板，`-addMail YES` 管理邮箱。
  例：`scripts/app/mac_shot.sh inbox-link -tab mail -openInboxMatch 下周的分享 -openInboxLink https://example.com/`。
- sheet 的关闭和保存（#1248）：Mac 的 sheet 只能靠底栏的取消 / 完成（Esc）关，编辑类 sheet 用 `sheetCancel(unsaved:)`（有未保存的修改先确认），只看的用 `macSheetDone`，
  见 `docs/lessons/macos.md`。截图：`-showModelConfigSheet <分类 id>|all` 打开「模型配置」sheet（`all` 那一行在「设置 › 助手」列表靠下，窗口要够高它才渲染、才会弹：`SIZE=1280x1500`），
  `-showDiscardConfirm YES` 让编辑类 sheet 一打开就弹出「放弃未保存的修改？」（存成 `<名字>.sheet2.png`）。
- 截图补充（2026-11-01）：窗口上的 sheet 是另一个窗口，`-macSnapshot` 现在把它另存成 `<名字>.sheet.png`；sheet 里有文本编辑器时 `terminate` 会被挡住，截完 1 秒后直接退出。看得见的 popover（输入框的 ⌃R 搜索）另存成 `<名字>.popover.png`（2026-11-05）。
  锁屏时未签名的新构建读另一个构建写的钥匙串条目会一直卡在 securityd（跳过授权界面也一样），截图模式（`-macSnapshot`）下 Mac 的 DEBUG 构建不读钥匙串。
  没锁屏时同样的读会弹「想要使用你储存在钥匙串的 <bundle id> 中的机密信息」、要输登录密码的框，App 卡在启动、一直没有窗口（2026-11-03）：
  每次编出来的未签名构建都算新的身份，上一版存的配对它读不了。真窗口验收的副本重新编译后换一个新的 bundle id（新 id 下钥匙串里没有条目，不会弹框），别去点那个框。

## 验收清单

下面是 iOS App 的全部功能，Mac 版逐条对上（做完一条勾一条）。

2026-11-01 全部过了一遍：分 5 组并行，每组用离屏截图（体验模式和 `scripts/mock/mock_hub.py` 的假数据，深浅色）和单元测试（`JarvisMacTests`）逐条核对，顺手修了能修的；再由一个 reviewer 逐条试着推翻打的勾，推翻了的不勾，写清看到了什么、没看到什么。
核对后 106 条 [x]、118 条 [ ]。没勾的每条下面都写了原因：「要到 Mac 前看」是要解锁后在 Mac 上点、按、看的，「没做」是还没做的，「Mac 上没有」是 Mac 上本来就不该有的。
2026-11-02 晚起在 Mac mini 上用真窗口续核（#403）：隔离的验收副本（独立 bundle id + `CFFIXED_USER_HOME`）+ 体验模式 / `scripts/mock/mock_hub.py`，cua-driver 在真窗口里点、按、看，配合离屏截图和假 hub 的请求日志；新勾的条目下面写了「真窗口核对」看到了什么，部分核对的写了还剩什么。现在 151 条 [x]、71 条 [ ]。
待你确认：`-showSampleScreen` 的示例「终端在问你」里原来有像内部工具 / 服务的名字，已在提交「chore: the sample terminal dialog uses made-up names only」里全换成编造的名字；以前拍的截图里可能还是旧文字。

### 真窗口验收怎么分工（2026-11-03 起）

真窗口验收拆成两个角色。

- **开发会话**（写代码的 Agent），负责：
  - 读 issue、改代码；
  - 准备被测窗口：编译，起隔离的验收副本和假 hub；
  - 写验收单；
  - 复核执行者报「不通过」「存疑」的项：看它交回的截图，必要时等它跑完再亲自看一次。
- **执行者**（另一个更便宜的模型），负责：用 cua 操作、截图、对照预期，按项交回报告和证据截图。
  - 它经本机配置的无头 Agent 通道调用，在独立的上下文里跑，开发会话只读报告。
  - 通道、命令和参数是本机的事，写在 `CLAUDE.local.md`。

为什么这样分：#403 一晚上的 token 约七成花在 cua 操作、截图和读截图上，而且这些全堆在开发会话越来越大的上下文里，每一步都要重读一遍。

**执行者跑的时候**：
- 开发会话不碰界面，同一时间只能有一个 Agent 用 cua；
- 一批放后台跑：5 项左右实测 12–14 分钟（2026-11-03），项目多就拆成几批、在一条后台命令里依次跑。

**开发会话自己看界面时**也要省 token：
- 读文字用带 `query` 的无障碍树；
- 截图只看缩小或裁下来的那一块；
- 能离屏截图的不上真窗口。

**验收单的写法**：执行者只看得到验收单，看不到开发会话的上下文。
- 给出被测窗口的 pid 和 window_id。
- 写清边界：只碰这个窗口和它弹出的菜单；不点窗口外的墙纸（会触发「显示桌面」）；除非某项写明，不用前台投递；不改文件。
- 预期写成能观察到的事实，比如出现了哪个按钮、哪段文字。不写「看起来正常」。
- 有条件才出现的东西写明条件。比如头像菜单里，「中断」只在对话工作中出现，「连接详情」只在连接不在线时出现。
- 辅助功能树和截图对不上时，以截图里画出来的为准。
- 每项存一张能证明这一项结论那一刻的截图（菜单开着、状态已经变了）。
- 报告每项一行：编号、通过 / 不通过 / 存疑、证据。每做完一项就追加写进一个报告文件：通道只在结束时输出，超时被杀就什么都拿不到（2026-11-03 一批卡在最后一项 15 分钟，前两项的结论全丢）。
- 每项给个时限（比如 4 分钟），到时就按当时看到的报「存疑」、做下一项。
- 滚动给固定的量（往上滚 3 次、每次多少），不写「滚到看见某一条为止」：执行者会为找那一条来回滚好几分钟。
- sheet 里往输入框打字写明用前台投递：后台的辅助功能插入会让字显示在框里，但不触发 App 的搜索（见 `docs/lessons/testing.md`）。
  还要写明怎么拿到 sheet 里的框（同一条经验）：没写，执行者在 sheet 上卡了 8 分钟。
- 要看右键菜单的，写明先把副本切到前台（`bring_to_front`）再用前台投递右键：前台投递做完会切回原来的 App，菜单跟着关掉，
  执行者就报「右键没有菜单」（2026-11-03 终端的右键菜单就这样误报过一次）。
- 不起眼的控件写清长什么样、在哪：终端的「关闭终端」是 sheet 顶部正中那根手柄，没写，执行者点了左上角的「窗口尺寸」，报「点了不关」。
- sheet 里的东西不要按 sheet 截图的像素点（2026-11-04）：macOS 26 的 SwiftUI sheet（`SheetPresentationWindow`）报的位置大小和 cua 截到的画面对不上
  （画面里是主窗口那一块缩放进来的），按截图像素换出的屏幕坐标是偏的，点了没反应，像按钮坏了（#1132 就这样误报过）。
  写明用主窗口无障碍树里 `AXSheet` 下那个元素：能按就按（AXPress），要真鼠标就取它的 frame，用屏幕坐标（`scope:"desktop"`）点中心。
- sheet 里靠手势的东西（双击锁定修饰键、快捷命令的单击 / 双击）要真鼠标：按无障碍元素 double_click 只是两次 AXPress，App 当成慢点两下；
  取 `AXSheet` 下那个元素的 frame，用 `scope:"desktop"` 的屏幕坐标点中心（双击用 `count:2`）（2026-11-04 执行者因此报过两项存疑）。
- 测终端翻历史，用不接管的终端（mock 只加 `MOCK_TERMINAL_STREAM=1`）：接管时滚轮按设计发给终端里的程序（`terminal_input.scroll`），
  假 hub 只记下、不翻，执行者就报「翻不上去」（2026-11-03 s5c）。

**切换前的对照**（2026-11-03）：拿 3 项已有结论的让执行者跑：建议胶囊（通过）、消息右键操作条（不通过，#501）、头像菜单（通过）。
- 第一遍：1 项一致，2 项报「存疑」，证据都对。
  - 操作条那项：辅助功能树里有「复制 / 选择 / 引用 / 分享」，但画面上没有。
  - 头像菜单那项：验收单把有条件的菜单项写成了必有。
- 按上面的写法改了验收单，第二遍 3 项全部一致。执行者没有碰副本以外的窗口。


## 0. App shell / navigation
- [x] 4 tabs with badges: Persona (pending assistant questions), Agents (pending interactions, excluding muted), 收件箱 (inbox action count), 设置 (「体验」 badge in demo) — Persona/Views/RootView.swift
  - Mac（2026-11-01 核对）：侧边栏角标和 RootView 同口径，截图里 Agents 2、收件箱 3、设置「体验」；体验数据里没有助手的待答问题，所以 Persona 没有角标，是对的。
- [x] Toast overlay (success / info-blue / error), haptics (success/warning/error) — Views/RootView.swift (ToastOverlay), App/AppModel.swift。Mac：浮层照常（拖入、链接拷贝等截图里看过），没有触感
- [x] Rebuild whole tree when style/persona changes, keep tab + open chat — Views/RootView.swift
  - Mac（2026-11-01 核对）读代码：整棵 Mac 树和 RootView 一样挂 `.id(model.look)`，tab、打开的对话、侧栏分区都存在视图外面，重建后还在；没截换风格的图。
- [x] Width-driven layout: compact = single NavigationStack, regular = NavigationSplitView (list | detail), shared path, `.splitDetailPage()`, sidebar width 340–480, SidebarTopFill — Views/SplitNavigation.swift
  - Mac（2026-11-01 核对）：Mac 上 `horizontalSizeClass` 恒为 regular，只走 NavigationSplitView（compact 本来就走不到）；2000pt 宽窗口截图里列表栏约 400pt，对话在详情栏、没有返回箭头。
- [x] Readable width cap (chat 760pt, welcome 560pt) — Views/Chat/ChatView.swift, Shared/Design/Theme.swift
  - Mac（2026-11-01 核对）：2000pt 宽窗口截图：对话栏约 760pt 居中，欢迎页（`-openWelcome`）约 560pt 居中。
- [x] URL handling: `jarvis://pair?...` pairing, `jarvis://open?machine=&session=` (Live Activity) — App/PersonaApp.swift。Mac：`MacOpenURL`（有测试），open 打开那个对话（从别的页面也切过去，`-macOpenURL <链接>` 验过），pair 只在没配对 / 体验模式时接受；对话里的 jarvis:// 链接仍只拷贝（LinkPolicy）。外面来的链接交给已经开着的主窗口（`handlesExternalEvents`）：以前每来一个链接就多开一个主窗口（`open -a Jarvis.app jarvis://…` 两次 → 3 个，`-macWindowLog YES` 记数）
- [x] Scene phase: reconnect + refresh on active, disconnect socket on background — App/PersonaApp.swift
  - Mac（2026-11-03 Mac mini 真窗口：执行者操作，开发会话看假 hub 的请求日志）：⌘H 隐藏期间一个请求都没有；带回前台后依次 GET machines / interactions / sessions / pins / mutes / fleet（`refresh()`）。在后台不断开 WebSocket 是 Mac 有意的（见「阶段 0」）。断线重连（2026-11-03 第 5 批）：假 hub `GET /mock/down?s=20` 断开所有连接、20 秒不应答，4 秒后侧栏底部和头像下都是转圈加「连接中…」；随即 ⌘H 隐藏，等 25 秒，假 hub 恢复后隐藏期间 App 自己重连上 `/api/v1/ws`，带回前台再拉一遍 pins / interactions / sessions / machines / fleet，「连接中…」消失、内容和隐藏前一样。
- [x] Remember last tab / last open chat / reading position across cold start (`chat.read.<session>`) — App/AppModel.swift, Views/Chat/ChatView.swift
  - 要到 Mac 前看：在对话里往上翻，⌘Q 后重开，看是不是回到同一行（⌘Q 不一定触发 onDisappear）。只带 `-app.tab chats -dev.openChat …` 启动时回到了 Agents 和那个对话，tab 和对话的恢复看过。
  - Mac（2026-11-03 Mac mini 真窗口）：⌘Q 后不带参数重开，回到 Agents 和上次打开的对话；同一次运行里切到别的对话再切回来，回到原来看的那一行。冷启动后的阅读位置原来停在最底部（#597：冷启动只有最近两轮，找不到那一行就放弃），修好后（2026-11-03）离屏截图复验：假 hub 像真 hub 一样分页（`/mock/paging?on=1`），先打开一次写下缓存，再带 `-chat.read.<会话> <「第 5 个问题」的行 id>` 冷启动，落在第 4–9 轮（修之前是第 15 轮和最底部）；iOS 上 `ChatFollowTests` 的冷启动用例管着。
- [x] DEBUG launch args (-tab, -openSession, -openPane, -openTerminalPane, -lark-connector, etc. list in docs/ios.md) — App/PersonaApp.swift + views
  - Mac 上截图一直靠它们：`-tab`、`-openSession`、`-openTerminalPane`、`-openTerminalSettings`、`-openAssistantPage`、`-showInboxAction`、`-showNewChat`、`-addMail`、`-openWelcome`、`-showKnowledgeEntry` 都试过；Mac 自己的另见各节（`-macSection`、`-macAgentAsk`、`-openChatWindow`、`-macWindowLog`…）。

## 1. Persona tab (assistant)
- [x] Headless Claude assistant conversations (main conversation `assistant.main` + side branches); first launch opens main — Views/Assistant/AssistantView.swift, App/AppModel.swift (assistantMain, refreshAssistant)
  - Mac（2026-11-01 核对）截图：主对话打开，侧栏置顶「主对话」、最近里有几条分支对话；逻辑是共用的 AppModel。
- [x] Home: greeting + available devices + 6 suggestions; launcher/model chip list; StarterComposer (or BeautifulStarterComposer) with photos — Views/Assistant/AssistantView.swift (AssistantHome, StarterComposer, StarterModelChip, AssistantToolsRow)
  - Mac 截图看过（`-tab assistant -assistant.current ""`）：问候、在线机器 + 这台 Mac、6 个建议、启动器 / 权限 / 思考 chip、输入框；图片走拖放 / 粘贴（`MacComposerFiles`）。
- [x] First message shows bubble + 「正在准备」 immediately, session starts in background; warm assistant — AssistantView.swift, AppModel.warmAssistant
  - Mac（2026-11-03 Mac mini 真窗口，#536 修过后）：打开新对话页时假 hub 收到 `POST /api/v1/assistant/warm`（预热）；mock 加 `MOCK_ASSISTANT_START_SECONDS=2` 时，发出后先是气泡和「Persona 正在准备…」；POST 回来后对话页一直是这句（`MOCK_ASSISTANT_TRANSCRIPT_SECONDS=3`，transcript 晚到也不出「还没有消息」、不重复），侧栏的新对话排「最近」第一、选中、带转圈。之前发出后是空的「还没有消息」：mock 不像 hub，App 在真 hub 上也有这个空档，见 #511。
- [x] Thinking hidden; tool calls folded into 「工具 N · 电脑、iPhone…」 — Views/Chat/ChatLines.swift, ChatView.swift
  - Mac（2026-11-01 核对）深浅色截图：没有思考块，工具调用折成回复上方的「工具 3 · 1 分 ›」。
- [x] Floating top bar with fog, center persona dot + name capsule; connection state replaces name — Views/Assistant/AssistantTopBar.swift (AssistantTopBar, TopFog)
  - Mac（2026-11-01 核对）截图：顶栏浮在对话上、带雾，中间是形象和「Persona」胶囊；`-showConnection connecting` 时胶囊换成「连接中…」。
- [x] Avatar menu: 知识库, 详细信息 (profile), 回到主对话, 拷贝目录 / 拷贝信息 / 拷贝会话 ID (「✓ 已拷贝」 3s), 启动器 submenu (switch launcher, applies via --resume next message), 会话信息, 中断, 连接详情 — AssistantTopBar.swift
  - Mac（2026-11-02 Mac mini 真窗口逐项点过；11-03 执行验收的 Agent 又跑了一遍、截图复核）：标题「Persona · 启动器」，知识库、详细信息（档案页）、回到主对话（只在侧对话里有，点了回到主对话）、拷贝目录 / 拷贝信息（多行，含启动器）/ 拷贝会话 ID（名字胶囊「✓ 已拷贝会话 ID」）、启动器 › 下一条消息起使用（切换后菜单显示新的启动器）、会话信息（运行 / 用量 / 位置 sheet）。「中断」只在对话工作中、「连接详情」只在连接不在线时出现（`AssistantTopBar` 里的条件），空闲、在线的主对话上没有，是对的。
- [x] Left button: history sidebar; right button: new chat (disabled on new-chat page) — AssistantTopBar.swift
  - Mac（2026-11-01 核对）深色截图：左边侧栏开关、右边新对话按钮都在（浅色下玻璃按钮只剩边框）；新对话页上按钮置灰没截，和 iOS 同一份代码。
- [x] Sidebar: search, new chat, Persona card (online/offline + machine → profile), 定时任务 / 知识库 tiles, 置顶 (main pinned, long-press pin, `assistant.pinned`), day groups 今天/昨天/最近7天/更早 (8 + 查看全部), row = title + preview + time + status dot (need-you red / unread blue / working spinner), long-press 拷贝会话 ID / 结束 / 置顶, 设置 row in drawer — Views/Assistant/AssistantSidebar.swift
  - Mac（2026-11-03 Mac mini 真窗口，执行者跑）：mock 加 `MOCK_ASSISTANT_SIDEBAR=1`。行尾：需要你红点、完成未看蓝点、工作中转圈，别的行没有标记。「最近」显示 30 条，末尾「查看全部（39）」展开到全部、变成「收起」。右键一行：置顶、重命名…、归档、拷贝会话 ID（剪贴板里是会话 id）、结束…（确认 sheet「关闭这个 Agent？」，取消后还在）；主对话只有拷贝会话 ID、结束…。置顶后排在「置顶」里主对话下面，取消置顶回到按时间的位置。搜索（见 §25）、底部 Persona 卡片（资料页）、定时任务、知识库都点过。清单里的按天分组和磁贴是旧设计（iOS 也一样），现在一行是标题加行尾标记。Persona 卡片离线（2026-11-03 离屏截图，假 hub 把助手的机器改成离线的 NAS：`PUT /api/v1/assistant {"machine_id":"nas"}`）：深浅色下底部卡片都是灰点「离线 · NAS」。抽屉底部的「设置」Mac 上没有：Mac 的 `horizontalSizeClass` 恒为 regular（`Mac/Compat/SwiftUICompat.swift`），Persona 页总是分栏、不出抽屉，设置在壳的分区里。「新对话」（第 5 批）：右边换成空的新对话（问候、引导卡片、空输入框「问 Jarvis…」，跟着形象名，假 hub 的形象叫 Jarvis），第二栏原来选中的「主对话」不再高亮；点回「最近」里的对话能回去。
- [ ] Drawer gesture: pan anywhere, 45° rule, yields to horizontally scrollable content, velocity/half settle, page card scale/offset, tab bar fade (UIKit host) — AssistantSidebar.swift (DrawerPan, DrawerPageMotion, DrawerSidebarHost), Views/Assistant/DrawerProbe.swift
  - Mac 上没有：窗口恒为 regular，侧栏是分栏的一列加开关按钮；抽屉（`AssistantDrawer+Mac.swift`）没有拖动，而且主窗口里走不到（`horizontalSizeClass` 恒为 regular，窗口再窄也是分栏），`DrawerPan` / `DrawerProbe` 只在 iOS 编译。
- [x] Regular width: sidebar = split view left column, toggle button — AssistantView.swift, SplitNavigation.swift
  - Mac（2026-11-01 核对）截图：Persona 侧栏是 NavigationSplitView 的左栏，顶部右侧有开关按钮。
  - Mac（2026-11-04，#856）：改成列表在对话左边的 `MacListBeside`（三栏分栏收不起列表那一栏）；真窗口里「历史对话」收起 / 展开，分区列开着、收着，浅色、深色都看过，展开时和原来一样。
- [x] Persona avatar (mask face, eyes move when working) — Views/Assistant/AssistantAvatar.swift
  - Mac（2026-11-03 Mac mini 真窗口，执行者跑）：打开工作中、还没有回复的对话（「思考中」），顶栏头像上方思考泡泡里的三个点轮流动，连拍几张各不相同；空闲的主对话上不动。工作中的眼睛（2026-11-03 离屏截图，`-showPersonaState working`，启动后第 5 / 5.4 / 5.8 / 6.3 秒各拍一张，裁出头像放大）：两只眼睛第 5 秒偏左、5.4 秒偏右、6.3 秒居中，在左右扫。
- [x] Profile page: identity, machine, online/working, 「接下来」 3 items, 主动做事 (tasks, run history), 了解你 (关于我, 记忆), 设置 (launcher, 管理启动器), capabilities list — Views/Assistant/AssistantProfileView.swift (AssistantProfileView, AssistantUpcomingRow, AssistantRunsView, AssistantRunRow)
  - Mac 截图看到（1280×1500 窗口）：身份、所在机器、在线 + 下一件、接下来 3 项、主动做事（定时任务、运行记录）、了解你（关于我、记忆）、设置（启动器、管理启动器），底部的能力列表 2026-11-01 核对时也看到了。
- [x] 「交给 Persona」 from other pages sends `<jarvis-context>` into main conversation — AppModel.askJarvis
  - Mac（2026-11-01 核对）读代码：行菜单和 Agent › 交给 Persona ⌘J 都调共用的 `AppModel.askJarvis`（同一条 `<jarvis-context>` 路径），`MacMenuShortcutTests` 里有 ⌘J；没对真 hub 发过。

## 2. Agents tab (dev list)
- [x] Always-visible search; quick filters 全部/需要你/未读/工作中 with counts; snapshot (`kept`) semantics — Views/Chats/ChatsView.swift (QuickFilterBar, AgentsList)
  - Mac（2026-11-01 核对）：原来那块空白是离屏截图的问题（列表栏浮在玻璃容器里，`cacheDisplay` 跳过了它，`0af14af` 已修），浅色截图里搜索框和 全部 / 需要你 2 / 未读 11 / 工作中 5 都在。
  - Mac（2026-11-02 Mac mini 真窗口核对）：深色（`-macAppearance dark`）真窗口里工具栏搜索框有放大镜和占位文字「搜索空间、标签、会话、内容」，离屏那块白色空胶囊是截图的问题；点「需要你 2」后列表只剩两个需要你的 Agent。
- [x] Grouping menu: 按仓库（默认，#2108）/ 按项目 / 按状态 / 按机器 (`DevGrouping`, remembered); one rank order — ChatsView.swift, Views/Chats/AgentListOrder.swift, Views/Chats/AgentRepoTree.swift, Views/Chats/AgentsMenus.swift (AgentsListMenu)
  - Mac（2026-11-01 核对）截图：按项目、按状态（需要你 / 完成未看）、按机器三种都看过，记住的 `dev.grouping` 生效；显示 › 分组方式 是同一个设置。
- [x] Card vs compact list (`dev.compact`); compact rows on one scale — ChatsView.swift (SessionRow), Views/Chats/CompactList.swift
  - Mac（2026-11-01 核对）截图（`-dev.compact YES`）：一行一条、同一个比例，有「还有 N 个空闲」；卡片模式见默认截图。显示 › 紧凑列表 是同一个设置。
- [x] 「需要你处理」 cards with quick-action buttons (card mode) / CompactInteractionRow — Views/Interactions/InteractionViews.swift (InteractionPreviewCard), CompactList.swift
  - Mac 截图看到：卡片模式的计划等你审批、Claude 在问你 + 选项按钮；紧凑模式的 `CompactInteractionRow` 2026-11-01 核对时也截到了。
- [x] Pinned agents, pinned spaces, busy / just-done / muted-running / idle spaces (folded), 「还有 N 个空闲」 — ChatsView.swift (IdleFoldRow, QuietSpaceRow, SpaceHeader, StateCounts)
  - Mac（2026-11-01 核对）截图：置顶的空间排第一、标题上有状态计数、「还有 2 个空闲」折叠行；置顶的 Agent 和屏蔽中还在跑的空间是同一份行代码，体验数据里没有，没截到。
- [x] Fold/unfold spaces and section headers (`dev.folded`), 全部折叠/全部展开 — ChatsView.swift, AgentsMenus.swift
  - Mac（2026-11-02 真窗口核对，MacBook）：点空间标题折叠 / 展开（辅助功能标签跟着变「已折叠 / 已展开」），显示 › 全部折叠后 7 个空间都折起，全部展开都展开。顺带发现折起的标题没对齐、读屏读三遍，见 #405。
- [x] Mark all read (filter + per-space, with confirm) — ChatsView.swift, App/AppModel.swift (markSeen)
  - Mac（2026-11-02 真窗口核对，MacBook）：空间 ⋯ › 全部标为已读（2）直接标掉（不确认，和 iOS 同一份代码），未读 11 → 9；未读筛选下「全部标为已读」弹确认「把 9 个完成未看的会话标为已读？」，确认后未读 0、列表变成空状态「没有完成未看的会话」。
- [ ] Swipe: left = pin / interrupt, right = close; long-press row menu (rename, mute/unmute, 交给 Persona, 拷贝恢复命令, 关闭 Agent…, stop loop) — ChatsView.swift, AgentsMenus.swift (AgentRowMenu)
  - 要到 Mac 前看：右键一行逐项点（Mac 版第一项是「在新窗口打开」），再在触控板上对一行左右滑。长按在 Mac 上就是右键菜单，离屏打不开。
  - Mac（2026-11-02 Mac mini 真窗口）：右键一行逐项点过：在新窗口打开（开出这个对话的窗口）、置顶 ↔ 取消置顶（列表顶上出现「置顶」一节）、重命名…（herdr 标签名和会话名两栏预填）、屏蔽通知 ↔ 开启通知、交给 Persona（切到主对话，发出带引用卡片的消息）、拷贝目录、拷贝恢复命令（`claude --resume <id>`）、关闭 Agent…（确认 sheet）；循环中的 Agent 多一项「停止循环」（对话里发出停 /loop 的消息）。还剩触控板左右滑：行上挂着对应的辅助功能动作（标为已看 / 置顶 / 关闭），但 cua 只能发滚轮事件，滑不出来，要真触控板。
- [x] Close agent confirmation sheet — DesignSystem/Components.swift (CloseAgentSheet, CloseAgentConfirmation)
  - Mac（2026-11-01 核对）截图（`-showCloseAgent YES`）：「关闭这个 Agent?」、标题、所在位置、两行说明、关闭 Agent / 取消。
- [x] Space header ⋯ menu: 打开空间, 置顶, 屏蔽/开启通知, 重命名空间, 全部标为已读 — AgentsMenus.swift (SpaceHeaderMenu), ChatsView.swift (SpaceMenuItems, RenameSpaceAlert)
  - Mac（2026-11-02 真窗口核对，MacBook）：⋯ 里打开空间（详情栏打开空间页）、取消置顶 / 置顶、屏蔽 / 取消屏蔽（标题旁铃铛斜杠、行变淡、挪到活跃空间最后、侧栏角标减一）、重命名空间（弹框预填旧名、能输中文、保存；体验数据不支持改名，刷新后回原名）都点过；没有未读时不显示「全部标为已读」。顺带发现打开空间页后工具栏两个按钮换了顺序，见 #483。
- [x] Rename agent (herdr tab + Claude session name) — ChatsView.swift (RenameSessionAlert), AppModel.renameSession
  - Mac（2026-11-01 核对）截图（`-macAgentAsk rename`）：重命名框有 herdr 标签名和会话名两栏、取消 / 保存，和 iOS 同一个 `RenameSessionAlert`。
- [x] Pins / mutes synced with hub (/api/v1/pins, /mutes), optimistic, per-agent override of space mute — App/PinsStore.swift
  - Mac（2026-11-01 核对）：共用逻辑（PinsStore、togglePin / toggleMute），Mac 的菜单调同一组函数，`MacAttention` 的测试覆盖屏蔽对 Dock 角标的影响，体验数据的置顶空间显示为置顶；没对真 hub 跑过。
- [x] Badges: 「⟳ 下次 12:20」 loop, 「⚙ 后台 N」, 「👥 子 Agent N」, muted 🔕 styling — ChatsView.swift (SessionRow)
  - Mac（2026-11-01 核对）：清单写旧了：现在列表行（iOS 也一样）不再带循环 / 后台 chip，挪到了对话页顶上的 `ActivityChips`，Mac 上看到「⚙ 1」；屏蔽的 🔕 和淡化在共用的 SlotRow 里（读代码，体验数据没有屏蔽的 Agent），子 Agent 没截。
- [x] Machine filter menu, 「机器」 entry, 新建会话 button — ChatsView.swift (TopBar), AgentsMenus.swift
  - Mac（2026-11-02 Mac mini 真窗口，#483 的修复构建）：≡ 菜单里有分组 / 显示 / 全部折叠 / 全部展开 / 机器 › / 机器；机器 › 选 Mac Studio 后按钮变成「Mac Studio ⌄」、列表只剩它的空间；假 hub 持续推送时菜单开着不跳。还剩：菜单里的「机器」、新建会话按钮。
  - Mac（2026-11-02 Mac mini 真窗口核对，master 含 #483）：≡ 菜单的「机器」在 Agents 的详情栏打开机器页；工具栏的「新建会话」打开「新对话」sheet（机器卡片、目录、启动器、权限与思考）。
- [x] Connection status in title slot — Views/ConnectionTitle.swift
  - Mac（2026-11-01 核对）截图（`-showConnection connecting`）：列表栏标题位置是「连接中…」加转圈，侧栏底部也有。
- [x] Pull to refresh — ChatsView.swift
  - Mac（2026-11-01 核对）：Mac 上没有下拉手势，换成 显示 › 刷新 ⌘R（见「显示 / 前往菜单」）：自己加载数据的页面登记自己的加载，否则按分区刷新 hub 数据，`MacViewCommandsTests` 有测试。
  - Mac（2026-11-02 Mac mini 真窗口核对）：窗口在前时按 显示 › 刷新，看假 hub 的请求：Agents → sessions / machines / interactions / pins / mutes；收件箱 → `POST /inbox/refresh`；知识库 → `GET /kb/projects`；机器 → 同上 + `GET /metrics`；对话的文件面板 → `GET …/fs`；Git 面板 → `GET …/git/status`。App 不在前台（没有 key window）时只按分区刷新 hub 数据，这是 `refreshTarget` 的设计，后台调菜单测会以为收件箱没刷。
- [x] Destinations registry (SessionKey, space, machine pages) — ChatsView.swift (AgentsDestinations)
  - Mac（2026-11-01 核对）截图：对话（SessionKey）、空间页（`-openSpaceFromChat`）、机器页（`-openMachinePage`）都推得进去。

## 3. Spaces & machine pages
- [x] Space page: agents in herdr tab order (SlotCard), machine button, menu (rename, mute, mark read, 新标签 / 在这个空间开新标签) — Views/Chats/SpaceView.swift (SpaceView, SlotCard)
  - Mac（2026-11-01 核对）截图（`-openSpaceFromChat YES`）：机器按钮 + 目录、按 herdr 顺序的 SlotCard 带状态、「在这个空间开新标签」、工具栏 ⋯；菜单项离屏没打开。
- [x] Sibling tabs strip in chat header (state-colored capsules, swipe, ⋯ list with red dot, 「空间 X 的所有 Agent」) — SpaceView.swift (SiblingTabs, SiblingList)
  - Mac（2026-11-02 Mac mini 真窗口核对）：展开的对话顶栏里同空间的标签胶囊按状态着色（当前那个描边），⋯ 上有红点（同空间有 Agent 在等你），点胶囊切到那个 Agent。
- [x] Machine spaces page: online state, counts, busy projects with latest line, idle projects, loose agents, ⓘ details — Views/Chats/MachineSpacesView.swift (MachineSpacesView, BusySpaceRow)
  - Mac（2026-11-01 核对）截图（`-openMachinePage macbook`）：在线、项目和 Agent 数、状态计数、进行中的项目带最新一句、工具栏 ⓘ；体验数据里这台机器没有空闲项目和散的 Agent，那两块没看到。

## 4. Chat page (agent conversation)
- [ ] Message rendering: user bubble, assistant markdown (MarkdownUI `.jarvis` theme), thinking row, peer 「来自 名字」 card, pending/sending bubble (retry/delete), sent photos, attached images — Views/Chat/MessageViews.swift (UserBubble, PeerMessage, PendingBubble, SentPhotos, AttachedImages, AssistantText, ThinkingRow, NoticeRow)
  - 要到 Mac 前看：别人发来的「来自 名字」卡片、发出去的照片。假 hub 截图里用户气泡、回复 markdown、思考行、发送失败的气泡和它的菜单都对，这两种没有数据。
- [x] Markdown tables fitted / horizontal scroll — Views/Chat/MarkdownTable.swift (FittedTable)
  - Mac（2026-11-01 核对）：截图里两张表都按宽度排开（fitted 对）；「宽表，横着滑」在 1280pt 窗口里也放下了，横向滚动没触发，没看到。
  - Mac（2026-11-02 Mac mini 真窗口核对）：隐藏侧边栏、窗口 820 宽时七列表放不下，被截在右边，横向滚轮能滚到「数据来源」那列；三列表按宽度换行排开。
- [x] Line model: one turn → message / work line / answer; work line counts & duration; split merged messages — Views/Chat/ChatLines.swift, ChatView.swift (WorkLine, WorkStepView, WorkCardSlice)
  - Mac（2026-11-01 核对）截图：工作行带步数和时长，大会话的 763 步折成一行；ChatLines 是共用代码。
- [x] Work trace expand/collapse in place (timeline style for glass, card style for others), 「收起 N 步」, pin tapped row under finger — ChatView.swift, Views/Chat/ChatPin.swift
  - Mac（2026-11-02 Mac mini 真窗口核对）：长对话（`MOCK_LONG_HISTORY=1`）里真点工作行：原地展开成时间线，被点的那行留在指针下面不动，再点收起也不动（不满一屏的短对话贴底排，钉不住是预期）。
- [ ] Tool views: tool groups, tool call rows, detail, Bash command/output, full output sheet, TodoWrite list, base64 images, folded work row — Views/Chat/ToolViews.swift
  - 要到 Mac 前看：点开「完整输出」的 sheet（`-showFullOutput sheet` 没打开）。新开关 `-showToolDetails YES`（DEBUG，原先只在 Mac，#677 起 iPhone 也认；已加进 release.sh 的列表）把每个工具行展开，截图里 TodoWrite、Bash 输出、搜索 / 读取详情、base64 图片、折叠行都对。
  - Mac（2026-11-02 Mac mini 真窗口）：「已截断，看全文（22 KB）」打开的「工具输出」sheet 只有 470×113、内容是空的（`GET …/full` 发了），见 #508。
- [ ] Unified DiffView (Edit/MultiEdit/Write + Codex unified diff), +N −N, 40-line truncation, horizontal scroll — ToolViews.swift (DiffView), Views/Session/GitPane.swift (DiffText)
  - Mac（2026-11-01 核对）：截图里只看到 Edit 的标题（+6）和一行红色删除行，在画面底边被截掉，没看到绿色新增行；40 行截断、横向滚动、Codex 的 unified diff 都没跑到。
  - Mac（2026-11-02 Mac mini 真窗口）：Edit 展开后 +6 −3、红色删除行、绿色新增行都在。删除行的底色只铺到文字后面、新增行铺到右边，两种宽度不一致（iOS 没看）。还剩 40 行截断、横向滚动、Codex 的 unified diff。
- [ ] BeautifulUI style: ThinkingState, tool-run chips (ToolRunRow/ToolChipRow) — ToolViews.swift, Views/BeautifulUIBridge.swift, Packages/BeautifulUI
  - 要到 Mac 前看：BeautifulUI 风格下最后一条回复被输入栏盖住一截（截图里看到，像是真 bug，要改 ChatView.swift），以及 ThinkingState。工具运行 chip 和输入栏截图里画出来了。
  - Mac（2026-11-02 Mac mini 真窗口）：BeautifulUI 风格下两个对话的最后一条回复都完整露在输入栏 / 提问卡片上方，没被盖住（原来那张多半是离屏截图的问题）。还剩 ThinkingState（要正在思考的会话，`MOCK_STREAM=1`）。
- [x] Wake (background task) chip + WakeDrawer; recap strip + RecapDrawer (folded multiples) — MessageViews.swift (WakeChip, RecapCard, RecapStrip, RecapDrawer)
  - Mac（2026-11-01 核对）截图：唤醒 chip 和 WakeDrawer（`-showWakeDrawer`，DELAY 要 11 秒左右）、回顾条和 RecapDrawer（`-showRecapDrawer`，「离开期间回顾 · 3 次」）都对。
- [x] Background task notices with outcome — Views/Chat/TaskNoteRow.swift
  - Mac（2026-11-01 核对）假 hub 截图：「✕ 退出码 1」和「✓ · 3 分」两种结果都有。
- [x] API error card (human explanation, chips, details copyable, folded repeats, 重试) — Views/Chat/APIErrorCard.swift
  - Mac（2026-11-01 核对）：401 卡片截图看过（白话说明、401 / 要处理 chip、请求 ID 和原文、复制原文）；连续 502 折成「连续 3 次」带重试的那张没截到。
  - Mac（2026-11-02 核对）假 hub（`MOCK_API_ERRORS=1`）离屏截图：主对话里同样的 502 三次折成一张卡片：「推理服务暂时没有可用的 GPU」、502 / 临时故障 / 连续 3 次 chip、「重试」按钮（没点）。
- [x] Claude suggestion pill (填入 / 直接发送) — ChatView.swift (SuggestionPill)
  - Mac（2026-11-02 Mac mini 真窗口核对，`MOCK_SUGGESTION=1`）：点建议胶囊填进输入框（发送键亮起、胶囊收起）；清空后胶囊回来，右键 › 直接发送 / 填入输入框，直接发送后假 hub 收到 `POST …/messages`。
- [ ] Inverted list, follow-end logic, frozen inserts while reading history, ↓ jump button, landing position (landUnderInput / saved position), turn-based pagination (turns=2) with auto-load at top — ChatView.swift (ChatStreamFollow), Shared/ChatStore.swift
  - 要到 Mac 前看：往上翻历史时 ↓ 按钮和 ⌘↓、翻到顶自动加载、加载时位置不跳。都要真滚动，离屏截图驱动不了。
  - Mac（2026-11-02 Mac mini 真窗口）：往上翻历史出现 ↓，⌘↓ 回到最新。假 hub 一次返回了全部历史，翻到顶的自动加载没触发；读历史时新内容插入（`MOCK_STREAM=1`）没测。
  - Mac（2026-11-03 Mac mini 真窗口）：长对话往上翻了一阵后，最新一轮画到了较早历史的上面，辅助功能树里的顺序却是对的（#585）；冷启动后没有落到保存的位置，停在最底部（#597）。
- [ ] Local chat cache (last 2 turns per chat, 6 MB) + chats-v2 disk cache — Shared/StateCache.swift, Shared/ChatStore.swift
  - Mac（2026-11-01 核对）：「重启后缓存的内容又出现、`-openChatsFresh` 能清掉」只有口述，没留下截图或日志；每个对话 2 轮、6 MB 的上限没跑到，只是共用代码。
- [ ] Lazy images on scroll with memory + disk cache — Views/Chat/ChatImageLoader.swift (ChatThumbnail)
  - Mac（2026-11-01 核对）：只看到一张工具行里的图片解码显示；滚动时按需拉取、内存 / 磁盘缓存都没跑到。
- [x] Collapsing self-drawn header (ChromeHeader: title, subtitle state·machine·space›tab, sibling row, loop/background chips), follows finger, auto-collapse after 3s idle — ChatView.swift
  - Mac（2026-11-02 Mac mini 真窗口核对）：往上滚时顶栏展开成完整版（标题、状态 · 机器 · 空间、同空间胶囊、⋯、循环 / 后台 chip），停下 3 秒收回胶囊，点胶囊展开。
  - Mac 上不再折叠（2026-11-05 用户定，#1337）：顶栏一直是完整版，滚动、停下、落到消息上都不收（`ChatChrome.folds`）。折叠的那几帧滚动时卡（来回滚约 30 ms/s 的卡顿），Mac 窗口也够高。iPhone 照旧。
  - 左边圆形 ‹ 只在有上一页时显示（2026-11-05 用户定，#1337）：从列表打开的对话（分栏详情栏的第一页）没有 ‹，标题照样居中；从空间页等推进来的对话有 ‹，点了回到上一页（`-openOverSpace YES` 配 `-openSession` 截图；点按的逻辑没改，这次屏幕锁着没在真窗口里点）。
    这时标题栏那一行不再另画小 ‹（`splitDetailPage(ownsBack: true)`），只留对话自己的一个。以前第一页上也画着 ‹，点了没反应：见 `lessons/macos.md`「玻璃容器里 `opacity(0)` 藏不住按钮」。iPhone 照旧。
- [x] Session info sheet (run / usage / location, copy id, copy resume command) — ChatView.swift (SessionInfoSheet)。Mac：对话右侧的检查器栏（⌥⌘I / 点标题），Persona 页是窗口 sheet
- [x] ⋯ menu: 终端, 文件, Git (N), 空间里的所有 Agent, 重命名…, 屏蔽, 拷贝 session id, 拷贝恢复命令, 清空上下文 /clear, 关闭 Agent… — ChatView.swift
  - Mac（2026-11-02 Mac mini 真窗口核对）：对话 ⋯：终端、Git（N）、文件、中断（工作中才有）、重命名…、屏蔽通知、拷贝恢复命令、拷贝目录、空间里的所有 Agent、关闭 Agent…；在文件 / Git 面板里多一项「回到对话」。清单写旧了：「清空上下文 /clear」现在在输入框 + › 快捷指令里（能清的时候才有），「拷贝 session id」在会话信息里（iOS 同一份代码）。和行菜单同名的几项走同一组函数，在行菜单里逐项点过（见上面 Swipe 那条）。
- [x] Swipe left chat → Files → Git; swipe-back gesture re-enabled — Views/Session/SessionPanes.swift (SessionPane, SessionPaneBar), Views/Chat/SwipeBack.swift
  - Mac（2026-11-02 Mac mini 真窗口核对）：⋯ › 文件 / Git 进面板（文件：路径栏、文件夹在前、大小 / 时间、git 标记；Git：分支、↑1、暂存、改动、提交历史），左上「返回」或 ⋯ › 回到对话 回到对话。Mac 上没有横滑，入口是 ⋯。
- [ ] Hide tab bar in single-column chat — Views/Chat/TabBarHiding.swift
  - Mac 上没有：Mac 没有 tab 栏，`hidesTabBarWhenShown` 在兼容层（`Mac/Compat/NavigationCompat.swift`）里什么都不做。
- [ ] Side-vertical toolbar (iPhone Duo) layout — ChatView.swift (sideBarItems, sideHeader)
  - Mac 上没有：这是 iPhone Duo 的布局，`VerticalBarsReader` 只在 iOS 编译。
- [x] Live status strip (spinner line, output preview, → terminal) — SessionPanes.swift (ActivityChips, ActivitySheet)
  - 要到 Mac 前看：点对话页顶上的状态 chip 打开 ActivitySheet，看转圈行、输出预览和 → 终端。chip 截图里有，浮层要点开（加截图开关得改 ChatView.swift，这次没动）。
  - Mac 真窗口（2026-11-04，执行者跑）：点「循环与后台任务」chip，弹出的内容里有转圈行「循环中 · 每 30 分钟 正在执行这一轮」、后台任务和「看输出」；对话里的状态折叠条展开是步骤输出预览，「打开终端」打开终端查看器。
- [x] Loop strip (interval, next wake, task, 停止循环 confirm) + loop tick rows — SessionPanes.swift (LoopStrip, LoopTickRow)
  - Mac（2026-11-02 Mac mini 真窗口核对）：点「循环与后台任务」chip → ActivitySheet：循环中 · 每 30 分钟、下次唤醒（还有 N 分钟）、任务、「停止循环」→ 确认框「停止这个循环？」（取消 / 停止循环）；对话里 `/loop …` 的消息画成「⟳ 循环唤醒 时间 任务」小胶囊。
- [x] Background tasks strip (N 个后台任务, open output file) — SessionPanes.swift (BackgroundTasksStrip)
  - Mac（2026-11-02 Mac mini 真窗口核对）：同一个 ActivitySheet 里「1 个后台任务」、标题、命令、运行时长、「看输出」→ 文件查看器大 sheet 打开输出文件（路径、大小、行号）。
- [ ] Subagents strip + read-only subagent view + footer 回到主 Agent — SessionPanes.swift (AgentStrip, SubagentFooter)
  - 要到 Mac 前看：找一个有子 Agent 的会话，看子 Agent 条、只读视图和底部「回到主 Agent」。假 hub 和体验模式都没有子 Agent 数据。
- [ ] Queue strip (撤回编辑 / 拷贝 / 删除, pull all back) — ChatView.swift (QueueStrip), AppModel.editQueued
  - 要到 Mac 前看：Agent 工作时连发几条，看排队条的撤回编辑 / 拷贝 / 删除和全部拉回。假 hub 没有排队消息的数据。
- [ ] Skeleton / starting note (「正在启动…」, 「没有启动起来」) — ChatView.swift (ChatSkeleton, StartingNote)
  - 要到 Mac 前看：打开一个还没加载过的对话、新起一个 Agent，看「正在启动…」和失败时的「没有启动起来」。本地假 hub 加载太快，`-openChatsFresh` 加 DELAY 2 也没截到。
  - Mac（2026-11-02 核对）：`MOCK_DELAY=4` + `-openChatsFresh` 离屏截到骨架屏（几条灰条，标题「对话 · 连接中…」）。还剩「正在启动…」「没有启动起来」（要新起一个 Agent）。
  - Mac（2026-11-02 Mac mini 真窗口）：新对话 › 开始对话后，对话页空状态「还没有消息」，输入框上方「正在启动 Claude，消息会在它就绪后发出」。还剩「没有启动起来」。
- [x] Long-press message action bar: 复制 / 选择 / 引用 / 分享 / 编辑 (own); select-text sheet — Views/Chat/MessageMenu.swift
  - 要到 Mac 前看：右键一条消息打开操作条，自己的消息上有「编辑」，「选择」打开选字 sheet。`-showMenu`（DELAY 11 秒左右）截图里操作条画出来了。
  - Mac（2026-11-02 Mac mini 真窗口）：在消息上真的右键、Control-点击都不出操作条（同一窗口里列表行的右键菜单正常），见 #501。分享卡片（下一条）的入口在操作条上，也跟着挡住。
  - Mac 真窗口（2026-11-04，执行者跑，#854 之后）：右键自己的消息出操作条 复制 / 选择 / 引用 / 分享 / 编辑，右键回答出 复制 / 选择 / 引用 / 分享（没有编辑）；「选择」打开「选择文本」sheet（回答全文，可选中，带「全部拷贝」「完成」），「完成」关掉。
- [ ] Share card image (light/dark, save to Photos, share image/text) — Views/Chat/ShareCard.swift
  - 要到 Mac 前看：分享卡片里存到照片（会弹权限）和分享图片 / 文字。`-showShareCard` 截图里卡片画出来了。
  - Mac 真窗口（2026-11-04，执行者跑）：操作条「分享」打开「分享消息」sheet，卡片画出这段回答和头像，按钮 保存图片 / 分享图片 / 分享文字 / 完成；「分享文字」弹出系统分享菜单（邮件、信息、备忘录、拷贝……），「完成」关掉。还剩「保存图片」（会弹照片权限）和「分享图片」没点。
- [ ] Image viewer (all chat images, paging, zoom, save/edit/share/copy) — Views/Chat/ImageViewer.swift
  - 要到 Mac 前看：←/→ 翻页、缩放、保存。`-openFirstImage` 截图里查看器和保存 / 编辑 / 分享 / 拷贝都在。
  - Mac（2026-11-02 核对）：`MOCK_CHAT_IMAGES=1 -openFirstImage YES` 离屏截图：查看器、✕、保存 / 编辑 / 分享 / 拷贝都在。还剩翻页、缩放、保存（要真窗口）。
  - Mac 真窗口（2026-11-04，执行者跑，`MOCK_CHAT_IMAGES=many`）：单击对话里的竖图打开看图器（1/2，关闭、保存、编辑、分享、拷贝、下一张箭头）；点箭头翻到第 2 张再翻回来；双击放大、再双击还原；Esc 关掉。还剩键盘 ←/→ 和「保存」（会弹照片权限）。
- [x] Image editor (box / pen, 6 colors, undo/clear, 加到输入框) — Views/Chat/ImageEditor.swift
  - Mac（2026-11-01 核对）：`-openEditor YES` 原来只在 iOS 有钩子，`ImageViewer.swift` 的 macOS 分支补上后截图看到框 / 笔、6 个颜色、撤销 / 清除、加到输入框。用鼠标画、把标记画进图片（macOS 的 NSImage 分支）还要到 Mac 前试。
- [x] File path chips (verified via /stat), tool-row paths clickable — Views/Chat/FileViewer.swift (FileChips, LookUpFiles), Code/FilePresence.swift
  - Mac（2026-11-01 核对）：读取行的路径显示成链接；存在的路径变卡片、不存在的不变，在 §8 FilePresence 那张截图里看到，这一条自己的截图没拍到不存在的那一边，点卡片 / 链接也没试。
  - Mac（2026-11-02 Mac mini 真窗口）：BeautifulUI 风格下回复里存在的 6 个路径都成了文件 chip，不存在的两个保持文字。还剩点 chip / 链接。
  - Mac（2026-11-02 Mac mini 真窗口核对）：点回复里的文件 chip、工具行展开后的「查看文件」都打开文件查看器（假 hub `GET …/file`）。
- [ ] Context reference cards in bubbles (`<jarvis-context>`) — DesignSystem/QuoteViews.swift (QuoteBlock), Views/Link/LinkSheet.swift (ContextGroup)
  - 要到 Mac 前看：在链接面板里选字「作为上下文」后发出，看气泡里的引用卡片。`-openSendMessage` 发了一条带 `<jarvis-context>` 的消息，但截图里没出现这条消息。
  - Mac（2026-11-02 Mac mini 真窗口）：行菜单「交给 Persona」发出的消息气泡里有引用卡片（上下文 · Agent · 标题，机器 / 会话 / 目录）。还剩链接面板选字「作为上下文」那条路径。
- [x] Send-safety: refusal when computer input has draft → show / clear-and-send — App/SendSafety.swift
  - Mac（2026-11-01 核对）假 hub（`MOCK_SEND_REFUSE=input_draft`）截图：「电脑上的输入框里有字」对话框，先不发 / 清空并发送。
- [ ] Frame stats / perf probes (debug) — Views/Chat/FrameStats.swift, App/PerfSignpost.swift
  - 要到 Mac 前看：解锁后按上面「细节」里性能那段量帧率。锁屏时 display link 不走，离屏量不了。

## 5. Composer
- [ ] ComposerRow: + menu (照片 ≤4 / 拍照 / 文件 / 快捷指令), glass input, send / red interrupt — Views/Chat/ComposerParts.swift, ChatView.swift (Composer, CameraPicker)
  - 要到 Mac 前看：点 + 看有没有 照片（≤4）/ 文件 / 快捷指令、「拍照」在 Mac 上怎样；点工作中会话的停止键，看它是红色、能中断。+、输入框、发送、停止方块截图里都在。
- [x] Pending images strip (tap to edit), 1600px compression — ComposerParts.swift (ComposerImages)。Mac：拖入 / ⌘V 的图片也进这一排（`-openDropDemo` 截图）
- [ ] File upload to the session machine's temp dir, paths inserted at cursor — Views/Chat/FileAttach.swift (UploadsBar)
  - 要到 Mac 前看：拖一个文件进来等传完，看路径写在光标处。`-openUploadDemo big.bin` 截图里上传条显示 11.5 MB / 25.2 MB 和取消 ✕，截图时还没传完。
- [ ] BeautifulUI PromptComposer: @ mentions (session files / other sessions as context), / slash commands (SlashCommand.all, /clear confirm), model (/model alias), dictation — Views/Chat/PromptComposer.swift
  - 要到 Mac 前看：空闲会话里 /clear 的确认、/model 切换别名。`-ui.style beautiful -showPromptMenu mention|command` 截图里 @ 菜单（列出别的会话）、/ 菜单、模型 chip、麦克风按钮都对。
- [ ] Dictation (SFSpeechRecognizer zh + AVAudioEngine) — PromptComposer.swift (Dictation)
  - 要到 Mac 前看：点麦克风，允许麦克风和语音识别，说中文，看字出来。Mac 走没有 AVAudioSession 的那条路径，按钮截图里在。
- [ ] Permission mode + effort chips (SessionModeBar), new-session picker — Views/Chat/SessionModes.swift
  - 要到 Mac 前看：点每个 chip 选一个值，看 chip 跟着变，再看新会话的选择器。chip 截图里都画出来了。
- [x] Drafts persisted per session — AppModel.drafts
  - Mac（2026-11-01 核对）读代码：`AppModel.drafts` 存 UserDefaults、没有平台限制，Mac 的输入框绑的是同一个 Composer。
- [ ] Quote bars inside the input box (from 引用, web selection / doc / other session) — DesignSystem/QuoteViews.swift (QuoteBar)
  - 要到 Mac 前看：在 Mac 的链接面板里选一段字点「作为上下文」，输入框里出引用条。要先选字才有，没截到。
  - Mac 真窗口（2026-11-04，执行者跑）：右键回答点「引用」，输入框上方出「引用 Claude」条（回答开头），输入框拿到焦点；✕ 去掉。还剩链接面板选字「作为上下文」那条路。
- [ ] Optimistic send with pending bubble, client_msg_id, retry/discard — Shared/ChatStore.swift, AppModel.send/retry/discard
  - Mac（2026-11-01 核对）：只读了代码（`AppModel.send/retry/discard` 和 ChatStore 共用，没有平台限制）；§4 截到过发送失败的气泡、§24 截到过离线的待发气泡，重发 / 删除没点过。

## 6. Interactions (questions / plans / tool approvals / terminal dialogs)
- [x] Interaction panel above composer — Views/Interactions/InteractionViews.swift (InteractionPanel)
- [x] QuestionForm: numbered options, 推荐 tag, 其他回答…, multi-question paging, auto-submit — InteractionViews.swift (QuestionForm, RecommendedTag)
- [x] PlanForm: markdown preview + fullscreen, 批准并自动执行 / 批准并自动接受编辑 / 打回 with comment — InteractionViews.swift (PlanForm)
- [x] PermissionForm: 允许 / 拒绝 / 本会话总是允许 — InteractionViews.swift (PermissionForm)
  - Mac（2026-11-01 核对）截图（`-showSamplePermission YES`）：「需要你授权」卡片带命令、拒绝 / 允许、文字链接「本会话总是允许」、只有 ⋯。
- [x] ScreenForm 「终端在问你」: parsed dialog → keys, 其他回答 preKeys, 改在对话里说, 查看终端 — InteractionViews.swift (ScreenForm, ScreenSteps, TerminalText)
  - Mac（2026-11-01 核对）截图（`-showSampleScreen YES`）：步骤标签、编号选项带推荐、其他回答…、改在对话里说、查看终端都在；点选项发按键没试（走共用的 resolve），查看终端要点开。
- [x] Preview cards in lists with quick buttons — InteractionViews.swift (InteractionPreviewCard, FlowButtons)
- [ ] Phone-tool write confirmation card — AppModel.pendingConfirmation, PhoneTools/PhoneToolRegistry.swift
  - Mac 上没有：Mac 登记成桌面端，phone_* 只调 iPhone，Mac 收不到要确认的手机写操作（见「Mac 是桌面端，不是手机」）。
- [x] Resolve API — AppModel.resolve
  - Mac（2026-11-01 核对）读代码：卡片（同一个 InteractionPanel）和 Agent 菜单的批准 / 拒绝（`MacAgentCommands`，`MacAgentCommandsTests` 通过）都调共用的 `AppModel.resolve`。
  Mac（2026-11-01 体验模式截图）：提问卡（编号选项、推荐、其他回答…）、计划卡（三个按钮）、列表里的预览卡都对；授权卡（`-showSamplePermission YES`）、「终端在问你」（`-showSampleScreen YES`）也看过：
  「本会话总是允许」在 Mac 上原来是第三个推按钮，改成和 iOS 一样的文字链接；卡片标题栏的 ⋯ 原来是带边框、自带 ⌄ 的弹出按钮（旁边还有折叠的 ⌄），改成只有 ⋯。「查看终端」展开后、其他回答…的输入框要点击，没看。离屏截图里玻璃卡片的文字在深色下画成深色（换 `-ui.style lark` 就正常），属截图问题。

## 7. Terminal
- [x] Terminal cover (bottom-up full page, drag to dismiss, size/handle/settings bar) — Views/Chat/ChatView.swift (TerminalCover), Views/Session/SessionPanes.swift (TerminalPanel)
  - Mac 真窗口（2026-11-03，mock `MOCK_TERMINAL_STREAM=1`）：终端是大 sheet，顶部正中的手柄往下拖能关，单击手柄（无障碍名「关闭终端」）也能关；Esc 不关，按设计发给终端里的程序。关掉后点对话页的「打开终端」能再打开。手柄、「窗口尺寸 132×24」、设置按钮、键栏都在。
- [ ] Live stream (terminal_stream, epoch/seq) or 1s ANSI polling fallback — Views/Session/Terminal/TerminalFeed.swift, SessionPanes.swift (TerminalFeedLink)
  - Mac（2026-11-01 核对）：流式这半截图看过（`MOCK_TERMINAL_STREAM=1`：颜色、真彩、反显、下划线）；普通 mock 下只看到一份静态的终端快照，看不出每秒轮询在刷新，回退这半没证实。
- [x] Grid model + cell renderer (wide chars, box drawing as shapes) — Terminal/TerminalGrid.swift, Terminal/TerminalView.swift (TerminalCanvas), Terminal/BoxDrawing.swift
  - Mac（2026-11-01 核对）流式截图：框线里的中文对齐、🚀 占两格、OSC 8 链接带下划线、200 字的长行折行。
- [ ] Phone-size takeover (terminal_control), 接管 / 只看 prompt — SessionPanes.swift, AppModel.terminalSubscribe
  - 要到 Mac 前看：用 `MOCK_TERMINAL_CONTROL=1`（提示用 `MOCK_TERMINAL_BUSY=1`）或真 herdr 打开终端，看窗格按窗口改尺寸、接管 / 只看能切。终端设置里「窗口尺寸 / 电脑尺寸」的说法截图看过。
- [ ] Wrap toggle, themes (Jarvis/Solarized/深夜/ink), font size, pinch zoom — Terminal/TerminalWrap.swift, Terminal/TerminalTheme.swift, TerminalView.swift
  - Mac 真窗口（2026-11-03）：切换「长行换行」后长行在折行和截断之间切换；配色换成 Solarized 后背景和文字色跟着变，再换回墨黑；⌘= 两次字变大（终端从 132×24 变成 116×21），⌘- 两次复原。还剩触控板捏合没试（cua 模拟不了捏合）。
- [ ] Selection (long-press word, drag, 拷贝 / 全选), OSC 8 / URL tap → in-app link — TerminalView.swift (SelectionHandle)
  - Mac 真窗口（2026-11-03）：拖选第一行 ⌘C 拷出「Welcome to Claude Code」，双击选中一个词，⌘A ⌘C 拷出整屏；右键弹出 拷贝（没选中时灰）/ 粘贴 / 全选，系统还会自动加一项「自动填充」。还剩点带下划线的 OSC 8 链接在 App 里打开没试。
  - OSC 8 链接：录制的帧和假 hub 里都没有 OSC 8 链接（2026-11-04 查过），要先给假 hub 加一条才能在真窗口点。
- [x] Scrollback loading 200 lines at a time (screen/before), 回到最新, ~1000-line limit notice — SessionPanes.swift
  - Mac 真窗口（2026-11-04，mock `MOCK_TERMINAL_STREAM=1`，不接管）：滚轮往上翻，接上了更早的输出（Step15 → Step3 → 翻到最前面的 Step1，假 hub 的约 430 行分两页加载），
    底部出现「回到最新」，用鼠标点（按无障碍树的位置点）和 AXPress 都回到最新一屏。还差：翻到头没有「约 1000 行」/「已到最前面」的提示（浮层的细顶栏没带这行字，iOS 同样，#1133）。
  - 接管时（`MOCK_TERMINAL_CONTROL=1`）滚轮按设计发给程序，不翻 App 自己的历史（2026-11-03 执行者因此报过不通过）。
  - #1133 之后（2026-11-04）：翻到头在「回到最新」上面出现「— herdr 只保留最近约 1000 行，更早的过程在「对话」里 —」：真窗口（`-openTerminalPane YES -terminalPages 3`，流式 mock，浅色的 Persona 终端配色，提示条白底）、离屏截图的深色终端和单独的终端窗口（`-openTerminalWindow`）、iPhone 模拟器都看过。接管时不显示。
- [x] Link status bar (last update, retry backoff, 重试) — SessionPanes.swift (TerminalLinkStatus)
  - 要到 Mac 前看：mock hub 加 `MOCK_SCREEN_FAIL_AFTER=3:14`，状态条出现（最后更新、退避、重试）后再恢复。体验模式和正常的 mock 都不出这条。
  - Mac 真窗口（2026-11-04，执行者跑，`MOCK_SCREEN_FAIL_AFTER=100:150`）：读屏失败后终端上方出红色状态条「上次更新 50 秒前」「11 秒后重试 · node macbook: not connected（请求 …）」带「重试」；倒计时在走，点「重试」马上重发；恢复后状态条消失、终端接着刷新。
- [ ] Key bar: sticky/locked/held ctrl/alt/shift, esc, tab, ⌃C, D-pad, paste, shortcuts — Views/Chat/TerminalKeys.swift (TerminalKeyBar, KeyCap, DirectionPad)
  - 要到 Mac 前看：修饰键的一次 / 锁定 / 按住、方向键盘、粘贴，以及深色下文字键近乎黑底黑字是不是真的。键栏截图里画出来了。
  - Mac 真窗口（2026-11-03，接管的终端）：点一下 Ctrl 变成高亮（无障碍值「已按下」），再点一下取消；esc、tab、展开方向键后的上、左都发出去了（终端里显示 `[A[D`）；
    「粘贴」弹出确认「粘贴 21 行到终端？多行文字在 shell 里可能被逐行执行。」带取消 / 粘贴。还剩：锁定和按住两种状态、深色下键的对比。
  - Mac 真窗口（2026-11-04，复核）：快速点两下 Ctrl（屏幕坐标真双击；按无障碍元素双击是两次 AXPress，App 当成慢点两下，锁不上）变成「已锁定」，发 esc 后仍锁定，再点一下解除。还剩：按住、深色下键的对比。
- [ ] Input: field mode + direct typing mode (UIKeyInput / UITextInput, hardware arrow keys) — Terminal/TerminalInput.swift
  - 要到 Mac 前看：真键盘打字、拼音组字、方向键、⌃ 组合。Mac 走自己的 keyDown（映射有 `MacTerminalKeysTests`）。
  - Mac 真窗口（2026-11-03，接管的终端）：点终端底部的输入行后打 hello jarvis，2 秒内出现在「>」后面，回车发出。还剩拼音组字、方向键、⌃ 组合。
- [ ] Gestures: 2-finger swipe sibling agent / adjacent space, double-tap paste (multi-line confirm), scroll in-terminal — TerminalView.swift
  - 要到 Mac 前看：触控板三指轻扫到同空间的 Agent、⌘V 粘贴（多行有确认）、终端里滚动。Mac 上的说法在 `TerminalHelpText`（有测试）。
- [ ] Shortcuts panel (single tap fill, double tap send), terminal settings (keys shown, auto-hide bar, theme, font, wrap), tips, completions from pane contents — Terminal/TerminalExtras.swift (ShortcutsPanel, TerminalSettingsView, TerminalPreview, ThemeRow, SuggestionStrip), SessionPanes.swift (TerminalTips)
  - 终端设置在 Mac 上是 grouped 表单（`macSheet`），「手机尺寸」相关的说法换成窗口（`TerminalSizeText`），截图看过（`-openTerminalPane YES -openTerminalSettings YES`，内层浮层存成 `<名字>.sheet2.png`）；快捷命令面板、补全没在 Mac 上试。
  - 终端设置 › 键盘 › 「将 Option 键用作 Meta 键」（2026-11-01，`TerminalPrefs.optionAsMeta`，只在 Mac 上有）：默认关，和「终端」App 一样，关着时 Option 照常打出 ™、é、德语布局的 @ [ ] 和走输入法。打开后 Option+可打印 ASCII 发 herdr 的 `alt+<键>`（herdr 编码成 ESC+键），Option+Shift+字母发 `alt+shift+<字母>`，符号的 Shift 已在字符里（`alt+>`）；组字时不管。映射是纯函数 `MacTerminalKeys.name`（有测试）。没拿真 herdr 试过 `alt+shift+a`（herdr 对带 shift 的组合可能编码成 CSI u，而「终端」App 发 ESC A）和 `alt++` 这类符号键名；真按键也没试（锁屏）。
  - 大 sheet（2026-11-01，`Mac/Design/MacLargeSheet.swift`）：终端、图片查看 / 编辑（Compat 的 fullScreenCover）、文件查看器、Git 的改动 / 提交在 Mac 上按主窗口内容区减两边 xxl 打开，最小 640×520；原来终端 640×520、文件查看器 470×280。坑：sheet 只认最小尺寸不认 ideal；固定 frame 设在 NavigationStack 上栈会变矮，要设在栈的内容上。fullScreenCover 统一按有标题栏和工具栏算（`macSheetBars` 116pt），终端也被多减了这一截（够大，没分开处理）。
  - 终端在 Mac 上的说法（`TerminalHelpText`，有测试）：上滑 → 往上滚动、左右滑 → 左右滚动；终端设置的「手势」在 Mac 上叫「操作」（点击、滚轮 / 触控板、拖选、⌘C / ⌘V、右键、⌘± / 捏合、三指轻扫），没有听写、长按、弹出键盘；「只看 · 接管」的图标是窗口。快捷命令在 Mac 上没有编辑模式，每条右键菜单里上移 / 下移 / 删除（`MacShortcutOrder`）。没修：深色截图里键栏的文字键几乎黑底黑字（⚡、粘贴是白的），分不清是不是截图的问题，要解锁看真窗口；链接状态条体验模式里不出现，没看。
  - 要到 Mac 前看：单击填入、双击发送、右键上移 / 下移、补全（2026-11-01）。`-openShortcutsPanel YES` 截图里面板和「编辑」画出来了（离屏是深底深字，同键栏）。
  - Mac 真窗口（2026-11-04，复核）：⚡ 打开面板「快捷 · 单击输入，双击执行」，列 git status / git diff --stat / ls -la / /compact / /status；单击 git status 只填入（假 hub 收到 `text: "git status"`、没有回车），面板关上；双击 ls -la 直接发送（`ls -la` 加 `enter`）。还剩补全。

## 8. Files / Git / code viewer
- Mac（2026-11-01）：文件面板路径栏的 ⋯ 原来被 Mac 的弹出按钮挤成两个点，改成纯图标菜单；Git 的 diff / 提交 sheet 用大 sheet（`-macGitSheet diff|commit`，示例改成 `scripts/mock/mock_hub.py` 的 `MOCK_FILES=1` 里有的改动和提交）。连 mock_hub 截到了有数据的列表、diff 和提交。
  文件 / Git 面板在屏幕上时工具栏有「刷新」按钮（`Mac/Chat/MacRefreshButton.swift`，复用各自的 load()），显示 › 刷新 ⌘R 也刷它们（见「显示 / 前往菜单」）。Git diff 最后一行的背景现在铺到边（`MacDiffTextTests`）。
  mock_hub 的 /file 里没有图片（`docs/icon.png` stat 失败），要截查看器的图片 / PDF 得先加一条假数据。
- [ ] Files pane: breadcrumbs, dirs first, size/mtime, git markers, hidden toggle, 拷贝路径, 2000 limit — Views/Session/FilesPane.swift
  - Mac（2026-11-01 核对）：mock hub（`MOCK_FILES=1`）`-openPane files` 截图：面包屑 + ⋯、文件夹在前、大小和修改时间、git 标记，工具栏有刷新按钮（对话面板上没有）。隐藏文件开关只看到 ⋯、没看到开关本身，拷贝路径、2000 条上限没看到。
  - Mac 真窗口（2026-11-04，执行者跑）：⋯ 菜单是「显示隐藏文件」「拷贝路径」，开关切出 / 收起 .gitignore；文件右键「拷贝路径」拷的是完整路径。还剩 2000 条上限（假 hub 的目录没那么多文件）。
- [ ] Git pane: branch, upstream ↑↓, stashes, changes (staged/unstaged switch), diff sheet, commit history paging, commit sheet, copy diff/path — Views/Session/GitPane.swift
  - Mac（2026-11-01 核对）mock hub 截图：分支、↑1、暂存、改动 +4 −1、带 ref 的历史；diff sheet 的未暂存 / 已暂存切换和真 diff，提交 sheet 的标题、正文、作者、hash、文件。↓、历史翻页、拷贝 diff / 路径的右键菜单没看到。
  - Mac 真窗口（2026-11-04，执行者跑）：main、↑1 ↓0、暂存 1、改动 5 个文件；历史底部「加载更早的提交」从 Tweak #26 加载到 Tweak #39。改动行的右键菜单只有「拷贝路径」，「拷贝 diff」在 diff sheet 的 ⋯ 菜单里（代码），这次没点开。
- [ ] FileViewer: code (syntax, line numbers, copy line/all), JSON outline (fold, copy value/path, expand/collapse all), Markdown rendered/source, images, PDF, directories, share — Views/Chat/FileViewer.swift
  - 要到 Mac 前看：图片和 PDF（mock 没有图片假数据）、分享、拷贝一行 / 全部、JSON 全部展开 / 收起和拷贝值 / 路径、Markdown 渲染 / 源码切换。代码（语法色、行号，深浅色）、JSON 大纲、渲染的 Markdown、目录截图看过。
  - Mac 真窗口（2026-11-04，执行者跑 + 复核）：README.md 的 ⋯ 里「查看源文件」/「渲染 Markdown」来回切；「拷贝内容」拷出全文；「分享」弹系统分享菜单。代码和图片看不到：sheet 只有 470 × 80（#1502，#813 之后最小尺寸被链接面板的 `GeometryReader` 挡住），修后 1216 × 705，代码有行号和语法色。还剩拷贝一行、JSON 全部展开 / 收起和拷贝值 / 路径、PDF。
- [x] Paged file fetch (2 MB pages) — Code/CodeFileSource.swift
  - Mac（2026-11-01 核对）截图：2.3 MB 的 forecast.json（超过一页 2 MB）完整打开，大纲数出 6000 项。
- [ ] Line splitting, background highlighting (2000-line chunks, caps) — Code/CodeDocument.swift
  - Mac（2026-11-01 核对）：只截了一个 24 行的 App.swift（分行和高亮都对），到不了 2000 行的分块和上限，这部分只是共用代码，没在 Mac 上跑到。
- [ ] Virtualized code lines view (UILabel reuse, context menu) — Code/CodeLinesView.swift
  - 要到 Mac 前看：右键一行「拷贝这一行」、10 万行的文件滚动、真按 ⌘G / ⇧⌘G。Mac 的代码视图只画脏区（`CodeLinesView+Mac.swift`），截图里画出来了；编辑 › 查找下一个 ⌘G / 查找上一个 ⇧⌘G 是这次加的（`CodeFindNextTests`、`MacFindTests`），查找栏关着时 ⌘G 用上次的词重新打开。
- [x] JSON outline — Code/JSONOutline.swift
  - Mac（2026-11-01 核对）截图：forecast.json 的 {version, generated, count, forecasts [ … ] 6000 项}，根展开、数组折起。
- [ ] Markdown chunking — Code/MarkdownChunks.swift
  - Mac（2026-11-01 核对）：长 README.md 的截图只有第一屏（标题、列表、引用、代码、表格），看不出后面几块有没有加载出来，没滚到后面截。
- [x] Syntax languages + lexer — Code/SyntaxLanguage.swift, Code/SyntaxLexer.swift
  - Mac（2026-11-01 核对）截图：App.swift 的关键字、类型、字符串、数字、注释都着色，Markdown 里的代码块也高亮。
- [x] Monospace scrollable text (diffs, output) — Views/Chat/MonoTextView.swift
  - Mac（2026-11-01 核对）：Git diff sheet 截图（`MonoTextView+Mac`）：hunk 头、新旧行号、−/+ 行的 diff 色；最后一行背景没铺到边的问题已修（`MacDiffTextTests`）。
- [ ] Path presence cache — Code/FilePresence.swift; perf log — Code/FilePerf.swift
  - Mac（2026-11-01 核对）：mock 对话截图里存在的路径变成文件卡片、不存在的不变（presence cache 对）；perf log（`FilePerf`，`-showFilePerf`）这次没读日志。

## 9. Link viewer (in-app web)
- [x] Bottom sheet opening full height, 60% detent with background interaction — Views/Link/LinkSheet.swift (LinkSheet)
- [x] WKWebView pool (preload first 3 links per reply, LRU 6, shared data store, cache policy) — LinkSheet.swift (WebViewPool, PooledWebView)
- [x] Text selection menu: 「AI 修改这段」, 「作为上下文」; 选择文字… sheet; 在 Safari 打开; share — LinkSheet.swift (SelectableWebView, PickTextSheet, DocPickMenu)
- [x] Feishu docs/wiki links → native reader — Views/Feishu/Docs/FeishuDocReader.swift

## 10. New chat form
- [x] Machine picker, launcher picker (per-machine launchers, 跟随启动器), directory browser with search, 加入空间, title, first prompt, mode picker; remembers machine/launcher/cwd for Siri — Views/NewChat/NewChatView.swift (NewChatView, LauncherPicker, DirectoryBrowser)（Mac：⌘N 打开 sheet，见「快速打开、新建」）
  - 清单里的「directory browser with search」是旧描述：目录选择（`DirectoryBrowser`）没有搜索框，iOS 也一样（见 §25）。
- [x] Prefilled new chat (from mail 「让 Claude 处理」) — Views/Mail/MailViews.swift (NewChatPrefilled)
  - Mac（2026-11-03 Mac mini 真窗口，执行者跑）：收件箱打开一封邮件，工具栏「让 Claude 处理」弹出新对话 sheet，第一条消息预填「请用 mail_read 读取邮件（account_id、mailbox、uid、主题），总结要点，并给出建议的回复草稿。」；「开始对话」后切到 Agents，打开新会话，显示「正在启动 Claude，消息会在它就绪后发出」。

## 11. Machines / monitoring
- [x] 机器 (one page since 2026-11-01): N/M online, per-machine card with agents (需要你 / 工作中 / 空闲), CPU/mem/net now + 1h sparklines, alert rings, version-mismatch 重新部署, what needs a look first, 10s polling — Views/Machines/MachinesView.swift (MachinesView, MachineCard, MachineConcern, DeployRow)（Mac：侧边栏「机器」⌘5，`MacMachinesPage`）
  - 合并前的「机器」列表和「机器监控」总览在 Mac 上都核对过（2026-11-01）；合并后的页面 Mac 只编译通过，没截图。
  - Mac（2026-11-02 Mac mini 核对）真窗口 + 假 hub 离屏截图（深浅色）：N/M 台在线、用量偏高、需要你的 Agent 数；离线的排最前（带 ssh 错误），告警的那台红框 + CPU 高 / 内存紧张 / 负载高；每张卡片有需要你 / 工作中 / 空闲、CPU / 内存 / 网络和 1 小时曲线；`MOCK_NODE_MISMATCH=1` 的那台底部是「这台电脑上的 Persona 和 hub 不是同一次部署」+「重新部署」（没点）。⌘R 见上面 Pull to refresh。
- [x] Machine detail: agents (open the chat), 引擎, 1/6/24h CPU/mem/net/load charts with shared scrub, per-core bars, system facts, gaps, launchers, reconnect — Views/Machines/MachineDetailView.swift, Views/Machines/MetricCharts.swift (Swift Charts)
  - 原来的监控页在 Mac 上看过 1 / 24 小时、告警卡片、CPU 和内存图（`-macSection machines -openMonitor <id>`，现在打开的是合并后的机器页）。要到 Mac 前看：往下滚看网络、负载图、每核条、系统信息、断档，用鼠标拖图看两张图一起动，点一个 Agent 进对话。
  - Mac（2026-11-02 核对）：假 hub 离屏截图（1280×1600 被屏幕截到约 970 高）：Agent 4 个、引擎 herdr、监控 CPU / 内存 / 网络 / 负载 / 磁盘 / 交换。下面的图表、每核条、系统信息要滚动，等真窗口。
  - Mac 真窗口（2026-11-04，执行者跑）：服务器详情从上到下 头部、Agent、引擎 herdr、监控数值、1 / 6 / 24 小时切换（切 24 小时图跟着变）、CPU / 内存 / 网络 / 负载图、16 核的每核条、系统信息、启动器、重新连接；在图上拖动，几张图同一时刻一起出竖线和数值；点 Agent 进它的对话。
- [x] OS badge — Views/Machines/OSBadge.swift; model — Shared/MachineMetrics.swift
  - Mac（2026-11-01 核对）：总览卡片上 Ubuntu、两台 Apple、Debian 的标志，详情和监控页头部也有；`MachineMetricsTests` 在 JarvisMacTests 里通过。

## 12. Knowledge base
- [x] Kind pills (项目/会话/人/文档/会议/日程 with counts), tag filter, project sections (重点/其他进行中/已完成/不是项目) — Views/Knowledge/KnowledgeView.swift (KnowledgeView, KBKindBar, KBRow, KBAvatar, ImportanceBadge, KBPill)
- [x] Full-text search, merged per entry, highlighted hits — KnowledgeView.swift (KBHitRow)
  - Mac（2026-11-01 核对）截图（`-showKnowledgeSearch 小组件`）：按 项目 1 / 会话 1 / 人 1 分组，一条一行带命中数（4 处、6 处），每行命中里「小组件」都加粗。
- [x] Detail: header, 问 Persona / 开 Agent, agents, timeline, profile, relations, code dirs, last 7 days messages, path; search matches — KnowledgeView.swift (KnowledgeDetailView, KBMatchRow, KBTimelineRow, KBLinkRow, KBMessageText)
  - Mac（2026-11-01 核对）：截图里 P0 / 短期项目 / 相关 92、标签、开 Agent、在做的 Agent、时间线、档案、相关公司圈、搜索匹配行、目录路径都对；「问 Persona」按钮的位置是空白（白字加醒目玻璃底，离屏画不出），没看到，要到 Mac 前看一眼。
  - Mac 真窗口（2026-11-04，执行者跑）：天气 App 的详情顶部「问 Persona」（主题色底白字）和「开 Agent」（浅主题色底、主题色字）两个按钮都画出来了、看得清；头部 P0 / 短期项目 / 相关 92。
- [x] Day view of chat messages with prev/next — KnowledgeView.swift (KBDayView)
  - 要到 Mac 前看：点底部「‹ 前一天」，前一天加载出来、再能翻回来。`-showKnowledgeDay YES` 截图里当天 8 条消息、命中的那条高亮、前一天链接都对。
  - Mac 真窗口（2026-11-04，执行者跑）：搜「灰度」→ 会话「天气 App 项目群」→ 搜索匹配的第一行，进到 10 月 30 日（3 条，命中的那条高亮）；底部「‹ 10月29日 周四」翻到前一天（3 条），再点「10月30日 周五 ›」回来。
    假 hub 归档的聊天只有 `oc_team` 两天，搜的词要在里面（「灰度」「降雨条」「设计稿」）：搜「小组件」「发布」不出会话。
- [x] 打标 editor (importance, relevance, tags, project type/status) — KnowledgeView.swift (KBTagEditor)
  - 要到 Mac 前看：打开一个项目等详情加载完，点工具栏的打标按钮，看重要级 / 相关度 / 标签 / 类型状态的预填对不对，改一项保存后头部更新。`-showKBEditor` 在详情加载前就弹 sheet，预填的是占位条目（P2 / 0），只是 DEBUG 的先后问题。
  - Mac 真窗口（2026-11-04，执行者跑）：天气 App 详情加载完点工具栏「打标」，预填 重要级 P0、相关度「与我相关 92」、标签「iOS 小组件」、类型「短期（需求）」、状态「进行中」，和头部一致；改成 P1 保存，2 秒内头部变成 P1。
- [x] Models — Shared/Knowledge.swift
  - Mac（2026-11-01 核对）：`KnowledgeTests` 编进 JarvisMacTests 通过；列表、详情、搜索的响应在 Mac 上解出来并画出来了（截图）。
  Mac（2026-11-01 截图）：侧栏「知识库」整页，类型胶囊、项目分组、人列表、条目详情（问 Persona / 开 Agent、档案、目录）都对；体验模式的搜索固定回空（DemoHub），搜索命中、日视图还没在 Mac 上看过；打标 sheet 截图看过（见上面「表单」那段的 `-showKBEditor`）。
  搜索命中、条目里的匹配、日视图后来补看了（DEBUG 的编造数据 `App/DemoKBSample.swift`：`-macSection knowledge -showKnowledgeSearch 小组件`，加 `-showKnowledgeEntry chats/weather-team`，再加 `-showKnowledgeDay YES`）；日视图在 Mac 上日期显示了两次，改成标题是日期、副标题是会话名。按天翻页要点击，没试。

## 13. Inbox (收件箱)
- [x] Unified list (Feishu + mail + docs + waiting agents), filters (全部/飞书/each mailbox | @我/私聊/群聊/邮件/Agent), search, sections 需要你 / 知悉即可 (全部看过) / 已折叠 (全部清除) / 已处理, swipe = done, pull refresh — Views/Inbox/InboxView.swift (InboxView, InboxRow, InboxAgentRow)
- [x] AI draft card (发送 / 修改), 「AI 分身正在处理」 — InboxView.swift (InboxDraftCard)
- [x] Detail: Lark chat at first new message or mail; 已处理; action bar (摘要, AI 草拟回复, 交给 AI) — InboxView.swift (InboxDetailView), Views/Inbox/InboxActions.swift (InboxActionBar)
- [x] Draft sheet (edit, 让 AI 改, 重写, doc status changes) — InboxActions.swift (InboxDraftSheet)
- [x] Hand-off sheet (Persona new chat / one of 8 agents, presets, mark done) — InboxActions.swift (InboxHandOffSheet)
- [x] Cloud doc items: doc icon, rows mentioning me, status menu writes to sheet/base, detail page — Views/Inbox/InboxDocViews.swift
- [x] Store: cache, optimistic done, badge, unsent replies — App/InboxStore.swift; models Shared/InboxModels.swift

## 14. Feishu views
- [x] Lark chat: avatars, sender names, 5-min grouping, date dividers, reply quote, reactions (toggle), edited, new-messages divider, load earlier, topic groups + thread view, focus message, mark read only seen — Views/Inbox/Lark/LarkChatView.swift
- [x] Send with outbox (sending, failed → resend/edit/delete) — Views/Inbox/Lark/LarkOutbox.swift
- [x] Paging — Views/Inbox/Lark/LarkPager.swift; realtime sync + background cache — App/LarkSync.swift
- [x] Rich content: rich-text blocks, images, stickers, file cards, video, voice, code, system msgs — Views/Inbox/Lark/LarkMessageContent.swift
- [x] Inline runs + emoji images — Views/Inbox/Lark/LarkInline.swift
- [x] Interactive cards (header, markdown, columns, table, list, panel, note, tag, icon, buttons with callback, overflow, chart) — Views/Inbox/Lark/LarkCard.swift
- [x] Summary cards (merge forward, share chat/user, meeting, calendar, vote) — Views/Inbox/Lark/LarkSummaryCards.swift
- [x] Media: authenticated image cache, gallery, video player, Quick Look files — Views/Inbox/Lark/LarkMedia.swift
- [x] Profile card (发消息) — Views/Inbox/Lark/LarkPeople.swift
- [x] Reaction picker, 公司圈 (Moments) card + post view — Views/Inbox/Lark/LarkReactionsMoments.swift
- [x] Translation line under messages — LarkChatView.swift (LarkTranslationLine)
- [x] 「让 Persona 总结」 action — LarkChatView.swift
- [x] Time / name tag — Views/Inbox/Lark/LarkTime.swift; models — Views/Inbox/Lark/LarkModels.swift
- [x] Feishu doc reader: blocks, tables, images, files, link cards, outline, comments (resolved/unresolved), AI 摘要, 作为上下文, 在飞书中打开, share — Views/Feishu/Docs/*.swift
- [x] Feishu raw API routing + app icon — Views/Feishu/Foundation/FeishuFoundation.swift

## 15. Mail
- [x] Mail detail (HTML via WKWebView, attachments, mark seen, translation banner/显示原文, reply, 让 Claude 处理) — Views/Mail/MailViews.swift (MailDetailView, HTMLView)
- [x] Compose / reply (account, to, cc, subject, body) — MailViews.swift (ComposeView)
- [x] Accounts management (list, status, provider marks) — MailViews.swift (MailAccountsView, MailProviderMark)
- [x] Add account: manual IMAP/SMTP form, Sign in with Google (web auth session) — MailViews.swift (AddMailAccountView, GoogleSignInLabel), Views/Mail/MailAccountForm.swift

## 16. Model launchers
- Mac（2026-11-01）：新增 / 编辑启动器是表单 sheet 大小（`-macLauncherEditor YES` 截图看过）；体验模式没有 /api/v1/launchers，列表、详情、额度、检测改用假 hub 截图（见下面各条）。
- [x] Launcher list (Jarvis-managed + ~/.zshrc, sources, hidden) with search — Views/Settings/ModelLaunchersView.swift (ModelLaunchersView, LauncherRow)
  - Mac（2026-11-01 核对）假 hub 截图（`-openModelLaunchers YES`）：Persona 管理（空的提示）、来自 ~/.zshrc 3 个（模型 chip、网关、机器）、已隐藏 ›、工具栏 + 和搜索；`-showModelSearch <词>` 筛到只剩一个。
- [ ] Detail: per-machine params, function source (masked), 测试可用性, delete/hide with popover confirm, restore — ModelLaunchersView.swift (ModelLauncherDetailView, LauncherDetailSections, LauncherCheckRow)
  - 要到 Mac 前看：在真启动器上看函数源码一节（key 打码），点「在 Persona 里隐藏」（Persona 管理的点删除）看确认浮层，再从已隐藏里恢复显示。来源 / Base URL / 模型 / 电脑、可用性检测和再测一次、恢复显示截图看过。
  - Mac 真窗口（2026-11-04，复核）：#1281 之前的副本里列表行点不进详情，#1281（分组表单）之后可以；「在 Persona 里隐藏」弹出「确认隐藏」浮层，确认后回到列表——列表没刷新（#1433，已修），修后进「已隐藏」，从详情「恢复显示」回到原来的分组。假 hub 的启动器没有函数源码，那一节没看到。
- [x] Editor (name, base URL, API key, model, small model, args) — ModelLaunchersView.swift (LauncherEditor)
  - Mac（2026-11-01 核对）sheet 截图（`-macLauncherEditor YES`）：名字、网关 + API Key、模型 + 小模型、额外参数和说明、取消和置灰的保存。
- [x] Quota section (pools/windows, colors ≥70/90%, refresh) — Views/Settings/LauncherQuotaViews.swift
  - 要到 Mac 前看：在激活的窗口里，77% / 86% 的条是橙色、用完的是红色、42% 是默认色，点刷新重新加载。截图里额度池、窗口、数字和红 / 橙文字都对，条全是灰的（离屏窗口不是 key，多半是截图问题，没确认）。
  - Mac 真窗口（2026-11-04，复核）：cc_kiro 的额度 62%、64% 默认蓝，77% 橙，「已用完」红；cc_team 42% 蓝、86% 橙。刷新按钮在，没点。
- [x] Config models — Shared/LauncherConfig.swift
  - Mac（2026-11-01 核对）：假 hub 的列表、详情、额度响应在 Mac 上解成这些模型并画出来（截图）；这个文件两边都没有单独的单元测试。

## 17. Personas & styles
- [x] Persona picker / gallery (follow style colors), persona store (download, cache by rev, APNG frames) — Views/Settings/PersonaSettingsView.swift, Persona/PersonaStore.swift, Persona/Persona.swift, Persona/PersonaViews.swift (PersonaAvatar, PersonaDot, PersonaFramesView)
  - Mac（2026-11-01 核对）假 hub（`MOCK_PERSONAS=demo`）截图：形象库里 Persona（跟随界面风格）和下载的形象包、两块配色、包里的 5 个状态帧；选中后强调色（用户气泡、工具栏图标）换成形象的颜色，顶栏头像和名字跟着换。
- [ ] Persona dot states idle/thinking/working/needs_you/offline, reduce-motion = frame 0 — PersonaViews.swift, AssistantTopBar.swift
  - 要到 Mac 前看：系统设置 › 辅助功能 › 显示 › 减弱动态效果 打开后，Persona 顶栏的形象停在第 0 帧。外观页的 5 个状态截图看过，顶栏的等你处理（红圈）和思考中（…）也截到了（`-showPersonaState`）。
- [x] UI styles 液态玻璃 / Claude / 黏土 / Island / 飞书 / BeautifulUI with preview cards — Views/Settings/SettingsView.swift (StylePicker, StyleCard), Shared/Design/StyleSpec.swift, Shared/Design/Styles/*.swift
  - Mac 截图看过（`-tab settings -openAppearance style`）：6 张风格卡片都画出来，选中的带勾；液态玻璃的说明在 Mac 上是「系统原生，简洁」（`Styles/Glass.swift`）。各风格整套界面在 Mac 上没逐个截。
- [x] Text size 小/标准/大 over Dynamic Type — DesignSystem/TextSize.swift
  - 外观页的 小 / 标准 / 大 截图看过；显示 › 放大 / 缩小 / 标准字号 ⌘+ ⌘- ⌘0 改的是同一个值（有测试）。
- [x] Theme tokens, icons, palettes, status palette — Shared/Design/Theme.swift, Icons.swift, Palette.swift, PersonaPalette.swift, Shared/StatusPalette.swift
  - Mac（2026-11-01 核对）：`Shared/Design` 原样编进 JarvisMac，这一轮所有截图都按它画（状态色、告警 chip、形象着色）；扫源码的 token 测试只在 iOS 的 JarvisTests 里，这次没跑。
- [x] Components (StatusDot, Chip, CountBadge, EmptyStateView, Monogram…) and surfaces/buttons — DesignSystem/Components.swift, DesignSystem/Styling.swift
  - Mac（2026-11-01 核对）：截图里 Chip、CountBadge、Monogram、themedButton、卡片表面都看到；EmptyStateView 引的那张截图其实开着一封邮件、不是空状态，没看到。
  - Mac（2026-11-02 核对）：StatusDot「需要你」的呼吸圈改用 Core Animation 画（`MacBreathingRing`），真窗口连拍的几帧里圈从贴着圆点到放大变淡都有，
    浅色、深色（`-macAppearance dark`）都对，圈没被行裁掉；深色下圈取的是深色的红（按像素反推三个通道的透明度一致）。
  - Mac（2026-11-02 核对）：EmptyStateView 在 Agents 列表里截到了（`MOCK_DELAY=4` 下会话列表还没到时「开始第一个开发会话」+「新建会话」）。

## 18. Notifications & notification actions
- [ ] Request permission + APNs registration, POST /devices — App/PushRegistrar.swift (registerIfPossible；iOS 和 Mac 共用)
  - Mac（2026-11-02）：`com.apple.developer.aps-environment`，`MacAppDelegate` 收 token，以 kind `mac` 登记（「远程推送（Mac）」）；也登记推送中继，`JarvisMacNotify` 解密（#432）。要用 TestFlight 包在真 Mac 上验证。
- [x] Categories INTERACTION_PERMISSION (允许 auth-required / 拒绝) and INTERACTION_PLAN (批准 / 打回修改 text input) — App/NotificationActions.swift
- [ ] Background resolve from action, failure local notification — App/PersonaApp.swift (resolveFromNotification)
  - 要到 Mac 前看：有 Agent 等授权时，悬停系统通知 › 选项 › 允许，hub 处理掉、窗口不出来；hub 断开时再来一次，应出一条失败通知。读代码：`MacAppDelegate` 走和 iOS 一样的 `NotificationActions.resolution` / `reportFailure`，`NotificationActionsTests` 在 Mac 上通过。
- [x] Tap opens session (main-queue completion handler), suppress while viewing — PersonaApp.swift（Mac：`MacAppDelegate`，本地通知见「菜单栏、Dock 角标、通知」）
- [ ] Silent push handling (wake / disconnect) — PersonaApp.swift
  - Mac 上没有：静默唤醒是叫醒 iPhone 跑 phone_* 工具的，hub 不发给 Mac；App 开着时 WebSocket 一直连着，`MacAttentionWatcher` 按会话列表发本地通知。
- [x] Settings: 需要你处理时, Agent 做完时 (不推送/只推长任务/每次都推), per-device prefs on hub, system-disabled warning + open settings — Views/Settings/NotificationSettingsView.swift
  - Mac（2026-11-01 核对）：截图看到「通知没有打开」+ 系统通知设置、需要你处理时、Agent 做完时三选一；在假 hub 上改这台设备的设置（PUT …/prefs）后页面跟着变，`NotifyPrefsTests` 在 Mac 上通过。在页面上点选写回 hub 没试。

## 19. Live Activity
- [x] Fleet activity (needsYou/working/done), push-to-start + push token upload, start/update/end (1 min grace), stale handling — App/LiveActivityController.swift, LiveActivity/PersonaActivityAttributes.swift, Shared/FleetState.swift（Mac：菜单栏 `MacMenuBar`，从会话列表算，不用 fleet 推送）
- [ ] Lock screen + Dynamic Island views, watch smart stack — PersonaWidgets/FleetLiveActivity.swift, LiveActivity/FleetIslandViews.swift
  - Mac 上没有：macOS 没有 ActivityKit、锁屏实时活动、灵动岛和手表智能叠放；对应的是菜单栏图标（上面 Fleet 那条）。
- [x] Buttons: Allow/Deny permission, Approve plan (LiveActivityIntent run in app) — LiveActivity/FleetIntents.swift, App/FleetActions.swift（Mac：菜单栏的子菜单）
- [ ] Island settings (presets A/B/C/custom, load machine, previews), lock details toggle — Views/Settings/IslandSettingsView.swift, NotificationSettingsView.swift
  - Mac 上没有：通知页的「实时活动」开关和灵动岛入口只在 iOS 编译，Mac 的通知页没有这一节（截图和普通通知页一样）。

## 20. Siri / App Intents
- [ ] 问 Claude (machine param, new session, returns OpenSession) — Intents/AskClaudeIntent.swift
  - 要到 Mac 前看：在 Mac 的「快捷指令」App 里跑「问 Claude」，填一句、选机器，那台机器上开了新会话。`IntentsTests`（机器优先用记住的、退到在线的；发出去的说法）在 Mac 上通过。
- [ ] Persona 状态 — Intents/PersonaStatusIntent.swift
  - 要到 Mac 前看：在快捷指令里跑「Persona 状态」，读出来的数量和侧栏一致。对应的 `IntentsTests` 在 Mac 上通过。
- [ ] 批准第一个待处理 — Intents/ApproveFirstIntent.swift
  - 要到 Mac 前看：有授权或计划等着时在快捷指令里跑「批准第一个待处理」，等得最久的那个被批准，回复说清做了什么。对应的 `IntentsTests` 在 Mac 上通过。
- [ ] 打开会话 (foreground) + SessionEntity — Intents/OpenSessionIntent.swift
  - 要到 Mac 前看：侧栏选着「机器」时从快捷指令跑「打开会话」，Persona 到前台、Agents 里打开那个对话。对应的 `IntentsTests` 在 Mac 上通过。
- [ ] MachineEntity query — Intents/MachineEntity.swift
  - 要到 Mac 前看：在快捷指令里编辑「问 Claude」的机器参数，列表里是你的各台电脑并标在线 / 离线。查询要问 hub，得在快捷指令的选择器里看。
- [x] Shortcuts phrases — Intents/PersonaShortcuts.swift; texts / picker / errors — Intents/IntentSupport.swift
  - Mac（2026-11-01 核对）：Mac 构建的 `Metadata.appintents` 有 3 条带 App 名的自动短语；IntentSupport 的说法、等待提示、失败和 hub 报错的句子在 `IntentsTests` 里跑。JarvisMacTests 现在也编进 `IntentsTests`、`KnowledgeTests`、`MachineMetricsTests`、`NotificationActionsTests`、`NotifyPrefsTests`（`apple/project.yml`），都通过。
  Mac：同一份代码编进 JarvisMac，构建产物的 `Metadata.appintents` 里有这 4 个动作、3 条自动短语、`MachineEntity` / `SessionEntity`（2026-11-01 查过）；
  「打开会话」在 Mac 上清掉 知识库 / 机器 的侧栏覆盖再打开对话。还没在 Mac 的「快捷指令」App 里实际跑过。

## 21. Phone tools (phone_* MCP)
- Mac（2026-11-01）：这一节在 Mac 上不再适用——Mac 登记成桌面端，hub 不调它的 phone_* 工具（见上面「Mac 是桌面端，不是手机」）。
- [ ] Register enabled tools on connect, tool_call → tool_result, write confirmations — PhoneTools/PhoneToolRegistry.swift, PhoneTool.swift, PhoneToolSupport.swift, AppModel.registerPhone
  - Mac 上没有：Mac 发 `register_desktop`、不带工具（`MacDesktopRegistrationTests`），hub 不给它发 phone_* 调用。
- [ ] Contacts search — PhoneTools/ContactsTool.swift
  - Mac 上没有：phone_* 只调 iPhone，Mac 登记成桌面端、没有工具（`MacDesktopRegistrationTests`）。
- [ ] Calendar events list/create — PhoneTools/CalendarTool.swift
  - Mac 上没有：phone_* 只调 iPhone，Mac 登记成桌面端、没有工具（`MacDesktopRegistrationTests`）。
- [ ] Reminders list/create/complete — PhoneTools/RemindersTool.swift
  - Mac 上没有：phone_* 只调 iPhone，Mac 登记成桌面端、没有工具（`MacDesktopRegistrationTests`）。
- [ ] Current location — PhoneTools/LocationTool.swift
  - Mac 上没有：phone_* 只调 iPhone，Mac 登记成桌面端、没有工具（`MacDesktopRegistrationTests`）。
- [ ] Health summary — PhoneTools/HealthTool.swift
  - Mac 上没有：Mac 没有健康数据，也不登记任何工具（`PhoneDataSourceTests`：Mac 上没有 .health）。
- [ ] Photos recent / get — PhoneTools/PhotosTool.swift
  - Mac 上没有：phone_* 只调 iPhone，Mac 登记成桌面端、没有工具（`MacDesktopRegistrationTests`）。
- [ ] Device info — PhoneTools/DeviceTool.swift
  - Mac 上没有：phone_* 只调 iPhone，Mac 登记成桌面端、没有工具（`MacDesktopRegistrationTests`）。
- [ ] Clipboard get/set — PhoneTools/ClipboardTool.swift
  - Mac 上没有：phone_* 只调 iPhone，Mac 登记成桌面端、没有工具（`MacDesktopRegistrationTests`）。
- [ ] Local notify — PhoneTools/NotificationsTool.swift
  - Mac 上没有：phone_notify 是 iPhone 的工具；Mac 自己的本地通知是 `MacAttentionWatcher`（§18）。
- [ ] Authorization status / request — PhoneTools/PhoneAccess.swift
  - Mac 上没有：没有 phone_* 工具就没有要授权的；原来申请这些权限的设置项在 Mac 上也去掉了（下一条）。
- [ ] Settings › 手机数据 per-source toggles + 授权 — Views/Settings/SettingsView.swift (PhoneDataView)
  - Mac 上没有：连接器里「这台 iPhone / Mac」那一行只在 iOS 编译，假 hub 截图里连接器只有邮箱和飞书。

## 22. Demo mode
- [x] 「没有 hub？先体验一下」, ServerConfig.demo, local APIClient + local socket, Demo.json with relative times, generated metrics, echo replies, demo errors shown as blue info — App/DemoHub.swift, App/AppModel.swift (enterDemo), Shared/APIClient.swift, Shared/LiveConnection.swift
  - Mac（2026-11-01 核对）：截图看到欢迎页的「没有 hub？先体验一下」、编造的 Agent 和「体验」角标、对「你好」的固定回复、生成的 CPU / 内存 / 网络曲线；体验模式下报错用蓝色提示是共用代码，没单独截。
- [x] Settings demo section (连接我的 hub / 退出体验模式), 体验 tab badge — SettingsView.swift (DemoSection), RootView.swift
  - Mac 截图看过：设置顶上的「体验模式 · 数据都是编造的」、连接我的 hub / 退出体验模式，侧栏「设置」旁的「体验」角标。

## 23. Pairing / credentials / watch
- [ ] Welcome page, QR scan (VisionKit), manual entry, jarvis://pair link — Views/Settings/SettingsView.swift (WelcomeView, PairView, QRScanner)
  - Mac（2026-11-01 核对）：欢迎页改成 Mac 的说法：链接图标、「在 hub 所在的机器上运行 `personad hub pair`，拷贝它打印的链接」（截图 x-welcome2），`personad hub pair` 现在会打印 `jarvis://pair` 链接（`TestPairPrintsTheLinkForTheMac`）；「连接 hub」sheet（新开关 `-openPairSheet YES`，配 `-openWelcome YES`）是粘贴配对链接 + 手动两栏。要到 Mac 前看：真配对一次：粘贴链接或 ⌘V、手动填地址和 token、从浏览器打开 `jarvis://pair` 链接。
- [ ] Keychain storage — Shared/Keychain.swift
  - 要到 Mac 前看：用真链接配对，退出后不带 `-pairURL` 重开，看是否还是已配对（弹钥匙串授权就允许）。DEBUG 的截图和测试不读钥匙串（锁屏时读别的构建写的条目会卡住），离屏没碰过钥匙串。
- [ ] Device token exchange, 设置 › 设备 list + revoke — App/DeviceCredentials.swift (DevicesView)
  - Mac（2026-11-01 核对）：设备列表、本机 / 只能回答 chip、撤销、已撤销分组截图看过（假 hub）；Mac 原来按 kind "phone" 换凭据、被列成 iPhone，现在要 "desktop"（`MacDeviceCredentialTests`），hub 存成 "other"。要到 Mac 前看：真配对一次，设置 › 设备 里这台 Mac 显示电脑图标、撤销会确认（`-pairURL` 启动不换凭据，截图里看不到）。
- [x] 重新配对, 断开并忘记服务器 — SettingsView.swift
  - 要到 Mac 前看：点「重新配对」看 sheet 弹出；点「断开并忘记服务器」确认后回到欢迎页。两个按钮在设置 › 服务器 的截图里。
  - Mac 真窗口（2026-11-04，执行者跑）：「重新配对」弹出「连接 hub」sheet（粘贴配对链接、服务器地址、Token、经 SSH 隧道连接、取消 / 连接），取消关掉；「断开并忘记服务器」先确认（「断开后需要重新扫码配对。」，取消 / 断开），断开后回到欢迎页。
- [x] Sync to Apple Watch (WCSession) — Shared/WatchSync.swift, SettingsView.swift; watch app — PersonaWatch/PersonaWatchApp.swift (macOS: n/a)
  - Mac（2026-11-01 核对）：Mac 不和手表配对，`WatchSync` 是空的；设置里原来还显示「同步到 Apple Watch」、点了提示已发送，现在只在 iOS 显示（深浅色截图看过）。

## 24. Offline / outbox / connection
- [x] Connection status waiting/connecting/updating (NWPathMonitor, ping 5s, debounce 1.5s), title replacement, details popover (立即重试) — App/ConnectionStatus.swift, Views/ConnectionTitle.swift
  - Mac（2026-11-01 核对）：假 hub 真断开时，列表标题、对话标题 chip、侧栏底部都是「连接中…」；`-showConnection` 强制的连接中 / 更新中同样三处，`MacConnectionCopyTests` 查 Mac 的说法。要到 Mac 前看：点「连接中…」标题，看详情浮层的文字和「立即重试」（离屏没画出来）。
- [ ] WebSocket with backoff + since_seq resubscribe — Shared/LiveConnection.swift
  - Mac（2026-11-01 核对）：假 hub 断开再恢复后截图里 App 回到在线、没有横幅；hub 日志里 `since=` 重订阅那一行只有口述，日志没留下、复现不了，`LiveConnection` 只是原样共用。
- [x] Outbox: offline messages persisted, auto-send in order after catch-up — App/Outbox.swift
  - Mac（2026-11-01 核对）：假 hub 断开时发一条，显示待发气泡「连上后自动发送」，退出、hub 恢复后再开，气泡变成正常发出（两张截图）。为此修了 `-pairURL` 启动不恢复 outbox（截图不读钥匙串，init 里没有配置），`MacLaunchArguments` 现在调 `restoreOutbox()`，只影响 DEBUG；hub 只收到一次是口述，没留证据。
- [x] One-shot actions not queued; blue 「刚才的操作没有生效」 — AppModel.show / resolve
  - 要到 Mac 前看：假 hub `/mock/down?s=20` 时点授权卡的「允许」，出蓝色「刚才的操作没有生效」、不进队列。
  - Mac 真窗口（2026-11-04，执行者跑）：`/mock/down?s=25` 时点授权卡「允许」，顶部蓝色「连不上 hub，刚才的操作没有生效」，卡片还在；连接恢复后卡片仍在，没有自动补发。
- [ ] Background delta sync for agents (rev/after_seq, 40 sessions, concurrency 3) — App/AgentSync.swift
  - Mac（2026-11-01 核对）：只读了代码：`App/AgentSync.swift` 没有 `#if`、编进 JarvisMac、没被替换；Mac 上没量过，没有运行证据。
- [ ] State cache for machines/sessions/interactions/inbox/mail accounts/lark chats — Shared/StateCache.swift
  - Mac（2026-11-01 核对）：共用代码，上面 outbox 跨重启读回用到了它；Mac 写了哪些文件只是口述，没列出来。没签名的 DEBUG 构建没有沙盒，写的是所有这类构建共用的 `~/Library/Caches/state/`，并行的 worktree 会互相覆盖（这次 outbox.json 就被另一个 Agent 的运行覆盖过）；签了名的有沙盒。

## 25. Search
- [x] Agents search — ChatsView.swift (searchable, SearchCloseProbe)
  - Mac（2026-11-03 Mac mini 真窗口，执行者跑）：⌘F 让工具栏的搜索框获得焦点（窗口在前台时），输「博客」只剩「博客迁移到 Astro」；✕ 清空、列表恢复；再按 Esc 搜索框失焦。
- [x] Assistant sidebar search — AssistantSidebar.swift
  - Mac（2026-11-03 Mac mini 真窗口，执行者跑）：点放大镜出「搜索对话」框和「取消」；输「第 3 周」只剩一条、没有分节，输「火星」显示「没有找到「火星」」；取消后回到完整列表。
- [x] Inbox search — InboxView.swift
- [x] Knowledge search — KnowledgeView.swift
  - Mac（2026-11-01 核对）截图（`-showKnowledgeSearch 发布`）按 项目 / 会话 / 人 分组对；高亮只在「人」那条里看到加粗，项目和会话露出的片段里没有这个词（§12 用「小组件」搜时每条都加粗了）。
  - Mac 真窗口（2026-11-04，执行者跑）：在搜索框里打「灰度」，出「会话」一组（天气 App 项目群），露出的两段聊天里「灰度」都加粗。
- [x] Launcher search, directory search, translation chat picker search — ModelLaunchersView.swift, NewChatView.swift, TranslationSettingsView.swift
  - Mac（2026-11-03 Mac mini 真窗口：执行者跑，开发会话复核了两项「不通过」）：⌘N › 启动器（本次覆盖）› 「函数名或模型」输「s」，codex 被筛掉；设置 › 翻译 › 飞书会话（「要翻译的会话」sheet）输「徒步」只剩「周末徒步群」。执行者用后台的辅助功能插入打字，字显示在框里但不触发搜索，报了不通过；改用前台真按键就筛了（`docs/lessons/testing.md`）。翻译那一项原来是 mock 的飞书会话缺 `is_mine` 等字段，App 解不开（「未能读取数据，因为数据丢失。」），mock 已补上（`scripts/mock/test_lark_mock.py`）。设置 › 模型供应商的启动器搜索（`ModelLaunchersView`）：`-showModelSearch <词>` 预填后截图看到列表筛选（2026-11-01）。清单里的「directory search」是旧描述：目录选择（`DirectoryBrowser`）没有搜索框，iOS 也一样，是逐级点进去。

## 26. Settings pages
- [x] Root: header (demo note), 形象, 界面风格 + 字号, 助手, 模型, Claude Code 设置, 机器, 设备, 手机数据, 通知, 翻译, 存储空间, 服务器, 关于, 同步到 Apple Watch — Views/Settings/SettingsView.swift (SettingsDestinations, SettingsPage)
  - Mac（2026-11-01 核对）深浅色截图（假 hub）：hub 地址和已连接、形象、界面风格、模型、助手、Claude Code 设置、连接器、机器、设备、通知、翻译、存储空间、服务器、关于；手机数据按设计不在 Mac 上，「同步到 Apple Watch」原来还显示，现在只在 iOS 显示。
- [x] 助手 → profile page; 定时任务 (upcoming timeline, daily/interval/once rows, toggle, swipe delete, long-press run now, templates, editor with model/permission/effort, bypass confirm, continue conversation, history) — Views/Settings/AssistantProactiveViews.swift (AssistantRoutinesView, AssistantTaskRow, AssistantTaskEditor, AssistantWeekdayPicker), Views/Settings/AssistantRoutinesModel.swift
  - 要到 Mac 前看：右键定时任务 › 立即运行 / 删除、勾选开关、保存一次编辑、选「绕过权限」看确认框、从运行记录继续对话、打开历史。主页、定时任务时间线、编辑页、新建 sheet 截图看过；一次性跟进的页脚在 Mac 上改成「右键可以立即运行或删除」。
  - Mac 真窗口（2026-11-04，执行者跑）：右键「早报」出 立即运行 / 编辑 / 删除，「立即运行」跳到 Persona 页这次运行的对话；勾选开关能关能开，离开分区再回来保持；编辑页（标题、开关、什么时候、提示词、模型、权限模式、对话运行方式、立即运行 / 上次结果 / 历史 / 删除）改了就自动保存，列表马上显示新时间；
    权限选「全部放行」弹确认「全部放行？…」，取消不变；历史记录列两次运行，点一次跳到它的对话、能接着说。
    右键（iOS 是长按）和左滑的「删除」都是直接删、不确认，和 iOS 一样（共用代码）；只有编辑页底部的删除按钮先确认。记忆那边右键 / 左滑删除都先确认，两处不一致，要不要统一待定。
- [x] 记忆 (frontmatter, type tags, markdown detail, edit, swipe delete, MEMORY.md read-only) — AssistantProactiveViews.swift (AssistantMemoryView, AssistantMemoryDetail, AssistantMemoryEditor)
  - Mac（2026-11-01 核对）：列表（类型标签、只读的「索引」行和 MEMORY.md 页脚）、详情、编辑 sheet 截图看过；左滑删除要触控板，Mac 上加了右键「删除」走同一个确认框，但右键菜单没截到。要到 Mac 前看：右键一条记忆 › 删除 › 确认，触控板左滑也还能用。
  - Mac 真窗口（2026-11-04，执行者跑）：右键一条记忆出「删除」，确认框「删除这条记忆？Persona 以后不再记得「早报格式」。」，确认后从列表消失。触控板左滑没试（cua 做不了）。
- [x] 关于我 notes — AssistantProactiveViews.swift (AssistantNotesView)
  - 要到 Mac 前看：改一段文字保存，重开看是否保留。页面（`-openAssistantPage about`）截图看过，保存要打字和点击。
  - Mac 真窗口（2026-11-04，执行者跑）：末尾加几个字后右上角「保存」变可点，保存后变灰；返回再进来，加的字还在。
- [x] Claude Code 设置 (settings.json whitelist keys, apply to all machines) — SettingsView.swift (ClaudeSettingsView)
  - 要到 Mac 前看：设置 › Claude Code 设置 改一个白名单里的键，应用到所有机器，看结果。设置根页面上有这一行，没有 DEBUG 开关能直接打开。
  - Mac 真窗口（2026-11-04，执行者跑）：列出机器、同时改所有在线机器、离开回顾（recap）、总是深度思考、思考强度、回复语言、屏蔽的配置目录；开着「同时改所有在线机器」切换 recap，提示「已应用到 3 台机器」。
- [x] 翻译 (auto translate, mail, chat picker) + Translator (NLLanguageRecognizer, batching) — Views/Settings/TranslationSettingsView.swift, App/Translator.swift
  - Mac（2026-11-01 核对）截图（`-translate.enabled YES -translate.mail YES`）：自动翻译开、目标语言、邮件勾上、飞书会话选择行；假 hub 收件箱里英文邮件显示成中文标题带翻译图标，Mac 上语言识别和翻译跑过。
- [x] 存储空间 (per category sizes, clear one / all, keep web logins) — Views/Settings/StorageView.swift
  - Mac（2026-11-01）：本机 ad-hoc 签名的调试版没有沙盒，`URL.cachesDirectory` 是所有 App 共用的 `~/Library/Caches`、`libraryDirectory` 是整个 `~/Library`：页面一直「计算中…」（在遍历整个 ~/Library），而「清除全部缓存」会把别的 App 的缓存当「其他缓存」删掉。改成没沙盒时只认 Persona 自己写的文件夹（`StorageUsage.owned`，有测试）、临时目录只认 `lark-`、不数 Library 和 Documents；有沙盒（iOS、签了名的 Mac 版）时和原来一样。修后截图算出 127 KB；清除没在 Mac 上点过。
    review（2026-11-01 07:20）后又改两处：缓存目录认 `chats` / `chats-v<数字>`（`ChatCache.isFolder`）（对话缓存现在是 `chats-v3`，原来的名单漏了它）；没沙盒时临时目录一个也不碰（`$TMPDIR` 是整个用户共用的，别的程序也可能写 `lark-` 开头的文件夹）。
    已改（2026-11-01，iOS 一起）：对话缓存的目录名只在 `ChatCache.folder` 一处定义，`StorageUsage` 判断归属和分类都用 `ChatCache.isFolder`，`chats-v3` 和以后的版本都算「Agent 对话」（以前落进「其他缓存」，iOS 也一样）；`chats-vx`、`chats-v3.bak` 这类不算。有 Mac 和 iOS 两边的测试。
  - 要到 Mac 前看：清除一类、再清除全部，看大小变小、网页登录还在（2026-11-01）。截图看到总量、比例条、各类大小和「清除全部缓存」。
  - Mac 真窗口（2026-11-04，执行者跑，隔离副本的 `CFFIXED_USER_HOME`）：各类大小（Agent 对话 49 KB、列表和飞书会话 61 KB…，共 111 KB）；清除「Agent 对话」先确认「清除「Agent 对话」49 KB？」，清完 0 KB、总计 61 KB。
- [x] 通知 / 灵动岛 — see §18, §19
  - Mac（2026-11-01 核对）：通知页截图看过（Mac 的说法「把鼠标移到通知上，点『选项』」）；灵动岛部分 Mac 上没有，见 §19。页面上出现两个一样的「系统通知设置」按钮，没查。



## UIKit dependencies (`Persona/App` and `Shared`)

| File | What it uses |
| --- | --- |
| `Persona/App/AppModel.swift` | `UIImpactFeedbackGenerator`, `UINotificationFeedbackGenerator` (haptics), `UIImage` (`composerInbox`), `UIDevice.current` |
| `Persona/App/PersonaApp.swift` | `UIApplicationDelegate` / `UIApplicationDelegateAdaptor`, `UIApplication.shared` (`registerForRemoteNotifications`, `applicationState`, background task id), `UIBackgroundFetchResult` (silent push), `didEnterBackgroundNotification`, `UIDevice.current` |
| `Persona/App/LiveActivityController.swift` | `UIApplication.shared` (foreground state) |
| `Persona/App/PerfSignpost.swift` | `UIView`, `UIViewRepresentable`, `UITraitCollection` (debug probe) |
| `Persona/App/DeviceCredentials.swift` | `UIDevice.current` (device name) |
| `Shared/StatusPalette.swift` | `UIColor` dynamic provider + `UIColor(hex:)` extension (`#if !os(watchOS)`) |
| `Shared/Design/Theme.swift` | `Color(uiColor:)` with `.systemBackground`, `.secondarySystemBackground`, `.quaternary/tertiary/secondarySystemFill` |
| `Shared/WatchSync.swift` | WatchConnectivity, not UIKit (`#if os(iOS)` / `os(watchOS)` branches) |
| `Shared/Keychain.swift` | Security only; already portable |

Notable UIKit outside those two folders (the macOS work will hit these too):
- **Terminal:** `Session/Terminal/TerminalView.swift` is a full UIKit view: `UIScrollView`, gestures, `UIEditMenuInteraction`, `UIFont`, `UIGraphicsGetCurrentContext`. `TerminalInput.swift` uses `UIKeyInput`/`UITextInput` and `UIKeyCommand` for arrows and esc. `Chat/TerminalKeys.swift` handles raw `UITouch`.
- **Assistant drawer:** `Assistant/AssistantSidebar.swift` uses `UIPanGestureRecognizer`, `UIHostingController`, `UITabBar` and `UIView.animate`.
- **Code and text views:** `Code/CodeLinesView.swift` (reused `UILabel`s, `UIContextMenuInteraction`), `Chat/MonoTextView.swift` (`UITextView` inside `UIScrollView`), `Chat/MessageMenu.swift` (long-press and swipe recognizers, `UITextView`), `Chat/ImageViewer.swift` (zooming `UIScrollView`).
- **Camera and drawing:** `Chat/ChatView.swift` uses `UIImagePickerController` for the camera. `Chat/ImageEditor.swift` and `PhoneTools/PhotosTool.swift` use `UIGraphicsImageRenderer`.
- **Navigation chrome:** `Chats/ChatsView.swift` (`UISearchController` probe), `Chat/SwipeBack.swift` (`UINavigationController` pop gesture), `Chat/TabBarHiding.swift`.
- **Link sheet:** `Link/LinkSheet.swift` uses `UITextView`, `UIMenuBuilder` and `UIApplication.open`.
- **Everywhere else:**
  - `UIPasteboard.general` appears in about 20 view files.
  - `UIImage` in the media, persona and loader files.
  - `UIColor` in `GitPane`, `TerminalTheme`, `FeishuDocRenderer` and `LarkInline`.
  - `UIFontMetrics` and `UIContentSizeCategory` in `TextSize`.
  - `UIDevice` in `SplitNavigation` and `DeviceTool`.
  - The open-settings URLs in `NotificationSettingsView` and `SettingsView`.
  - `UIImageView` in `PersonaViews`.

There are also many iOS-only SwiftUI modifiers: `navigationBarTitleDisplayMode`, `fullScreenCover`, `topBarLeading`/`topBarTrailing`, `textInputAutocapitalization`, `keyboardType`, `presentationDetents`, `presentationBackgroundInteraction`, `statusBarHidden`, and `toolbarVisibility(.tabBar)`.

## iOS-only frameworks and where they're used

| Framework | Used in |
| --- | --- |
| HealthKit | `PhoneTools/HealthTool.swift`, `PhoneAccess.swift`, `PhoneToolRegistry.swift` |
| ActivityKit | `App/LiveActivityController.swift`, `Views/Settings/NotificationSettingsView.swift`, `LiveActivity/PersonaActivityAttributes.swift`, `PersonaWidgets/FleetLiveActivity.swift` (WidgetKit too) |
| WatchConnectivity | `Shared/WatchSync.swift`, `App/DeviceCredentials.swift` |
| PhotosUI (`PhotosPicker`) | `Views/Chat/ChatView.swift`, `Views/Assistant/AssistantView.swift` |
| Photos | `PhoneTools/PhotosTool.swift`, `PhoneAccess.swift`, `PhoneToolRegistry.swift`, `Views/Chat/ImageViewer.swift` (save), `Views/Chat/ShareCard.swift` (save) |
| AVFoundation | `Views/Chat/PromptComposer.swift` (`AVAudioEngine`/`AVAudioSession` for dictation), `PhoneTools/PhoneToolRegistry.swift`. The camera is `UIImagePickerController`, not AVFoundation. |
| QR scanning | VisionKit `DataScannerViewController` in `Views/Settings/SettingsView.swift`, not AVFoundation |
| Speech | `Views/Chat/PromptComposer.swift` |
| CoreLocation | `PhoneTools/LocationTool.swift`, `PhoneAccess.swift`, `PhoneToolRegistry.swift` |
| Contacts | `PhoneTools/ContactsTool.swift`, `PhoneAccess.swift`, `PhoneToolRegistry.swift` |
| EventKit | `PhoneTools/CalendarTool.swift`, `RemindersTool.swift`, `PhoneAccess.swift`, `PhoneToolRegistry.swift` |
| AppIntents | `Persona/Intents/*.swift` (7 files), `LiveActivity/FleetIntents.swift` |
| UserNotifications | `App/AppModel.swift`, `App/FleetActions.swift`, `App/PersonaApp.swift`, `App/NotificationActions.swift`, `PhoneTools/NotificationsTool.swift`, `PhoneAccess.swift`, `PhoneToolRegistry.swift`, `Views/Settings/NotificationSettingsView.swift` |
| WebKit | `Views/Link/LinkSheet.swift`, `Views/Mail/MailViews.swift`, `Views/Settings/StorageView.swift` |
| Other | QuickLook and AVKit in `Views/Inbox/Lark/LarkMedia.swift`; PDFKit in `Views/Chat/FileViewer.swift`; Charts in `Lark/LarkCard.swift` and `Machines/MetricCharts.swift`; NaturalLanguage in `App/Translator.swift`; Network (`NWPathMonitor`) in `App/ConnectionStatus.swift` and `PhoneTools/DeviceTool.swift`; ImageIO in `Persona/PersonaStore.swift` and `LarkMedia.swift`; AuthenticationServices via the SwiftUI `webAuthenticationSession` for Google sign-in in `Views/Mail/MailViews.swift`; UniformTypeIdentifiers in `Chat/FileAttach.swift` |

Most of these also exist on macOS. The ones that don't have a direct Mac equivalent:
- ActivityKit and Live Activities.
- WatchConnectivity.
- HealthKit as a data source. It's iOS-first, so check what macOS actually supports before relying on it.
- `UIImagePickerController` and the VisionKit data scanner.
- `UIKeyInput`/`UITextInput` terminal input (needs an `NSTextInputClient` port).
- Notification `.authenticationRequired` actions and other lock-screen-specific behavior.
