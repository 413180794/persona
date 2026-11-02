# macOS App

原生 macOS 版 Jarvis（`apple/` 里的 `JarvisMac` target）：和 iOS App 同一套逻辑层（`Shared/`、`Jarvis/App`）和内容视图，
外面是 Mac 自己的壳（`apple/Mac/`）：窗口、侧边栏、工具栏、菜单、快捷键、设置窗口、菜单栏。目标是 iOS App 的全部功能一个不少，
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
  - **Jarvis**：主对话 + 置顶的对话；「全部对话」进列表栏看按天分组的历史。
  - **Agents**：全部 / 需要你 / 未读 / 工作中（带数量，需要你是红色角标）；下面是置顶的空间和各台机器（点机器 = 按机器筛）。
  - **收件箱**：全部 / 飞书 / 各个邮箱 / @我 / 私聊 / 群聊 / 邮件 / Agent。
  - **知识库**：项目 / 会话 / 人 / 文档 / 会议 / 日程。
  - **机器**：所有机器（Agent 数和监控在一页）+ 每台机器。
  - 底部：连接状态（等待网络 / 连接中…，点开是连接详情）。已做（`MacConnectionFooter`，连上时不显示；`-showConnection connecting` 截图看过）。
- 列表栏：iOS 同一份列表内容（Agents 列表的分组、卡片 / 紧凑、折叠空间、置顶），Mac 上行高更紧，选中用系统高亮，
  键盘 ↑↓ 移动选中、⏎ 打开、空格预览（Quick Look 式：对话最后几条）。
- 详情：对话页。顶部工具栏（`.toolbar`）：标题 + 状态·机器·空间›标签，同空间的标签做成工具栏里的分段 / 下拉；
  右侧「对话 / 终端 / 文件 / Git」分段（⌘1–⌘4 在详情获焦时切换），信息按钮打开 **Inspector**（`.inspector`，⌥⌘I）：会话信息、
  运行 / 用量、后台任务、子 Agent、循环、排队消息。
- 搜索：工具栏 `.searchable`（⌘F 聚焦），作用于当前列表。全局 ⌘K「快速打开」：搜 Agent、对话、空间、收件箱、知识库条目，回车打开。

### 菜单和快捷键（`.commands`）

| 菜单 | 项目 |
| --- | --- |
| 文件 | 新建 Agent 对话 ⌘N、新建 Jarvis 对话 ⇧⌘N、在新窗口打开 ⌥⏎ / ⌘⏎（列表里）、关闭窗口 ⌘W、交给 Jarvis ⌘J |
| 编辑 | 系统的拷贝 / 粘贴 / 全选 / 查找、查找下一个 ⌘G / 上一个 ⇧⌘G；拷贝会话 ID、拷贝恢复命令 |
| 显示 | 刷新 ⌘R、显示 / 隐藏侧边栏 ⌃⌘S、显示 Inspector ⌥⌘I、紧凑列表、分组方式（项目 / 状态 / 机器）、全部折叠 / 展开、字号放大缩小 ⌘+ ⌘- ⌘0 |
| Agent | 发送 ⏎（输入框里，⇧⏎ 换行）、中断 ⌘.、批准当前待处理 ⇧⌘⏎、拒绝 ⇧⌘⌫、回答选项 ⌥⌘1–9、重命名、屏蔽、置顶、标为已读、清空上下文、关闭 Agent… |
| 前往 | Jarvis ⌘1 … 机器 ⌘5（侧边栏获焦时）、上一个 / 下一个 Agent ⌘[ ⌘]、下一个需要你的 ⌃⌘N、快速打开 ⌘K、回到最新 ⌘↓ |
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
  已做（Agent 和 Jarvis 对话的输入框、Jarvis 新对话的输入框，`apple/Mac/Chat/MacComposerDrop.swift`，有测试）：从 Finder 拖进来的图片按「照片」附上（一条最多 4 张，多出的按文件传），
  其余文件按「文件」传到那台机器、路径写进输入框，文件夹不传（提示拖里面的文件）。输入框获焦时 ⌘V：Finder 里 ⌘C 的文件同拖入，
  剪贴板里的图片（截图）附上，文字照常粘贴；只有当前窗口的输入框接（对话可能开在单独的窗口里）。截图：`-openDropDemo a.png,b.md`。
- 拖放：列表里的 Agent 可以拖到侧边栏的「置顶」；文件面板里的文件可以拖出到 Finder（下载）。
- 窗口和列宽、侧边栏开合、选中的项目、每个对话的阅读位置都跨启动记住。
- 外观：跟随系统深浅色和强调色；界面风格（液态玻璃等）照样生效，Mac 上的玻璃用系统材质；不写死尺寸，跟随「设置 › 字号」。
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
  ⌥ 点列表行在新窗口打开也没做：锁屏时发不了真的点击，验不了它会不会同时触发 NavigationLink。

## 构建、运行、截图

```sh
cd apple
python3 ../scripts/jarvis_config.py xcconfig && xcodegen generate
eval "$(python3 ../scripts/jarvis_config.py env)"
xcodebuild build -project Jarvis.xcodeproj -scheme JarvisMac -configuration Debug -destination "platform=macOS" \
  -derivedDataPath ../../build/DerivedDataMac CODE_SIGNING_ALLOWED=NO
open ../../build/DerivedDataMac/Build/Products/Debug/Jarvis.app   # 或直接跑 Contents/MacOS/Jarvis 带启动参数
```

- 配置里没有 `app.bundle_id` 时编出来的 App 一启动就崩（Info.plist 没有 bundle id），本机自用可以在 xcodebuild 后面加 `JARVIS_BUNDLE_ID=<反向域名>`（`docs/lessons/macos.md`）。

- 启动参数和 iOS 一样（`MacLaunchArguments`，只在 DEBUG）：`-pairURL`、`-tab dev|mail|settings|assistant`、`-openSession <machine>/<session>`、
  `-openAssistant`、`-newAssistant`、`-showNewChat`、`-openChatsFresh`、`-openAgentsFresh`、`-lark-connector`、`-simulateTap`；体验模式 `-demo.on YES`。
- 截图（**锁屏也能拍**）：`scripts/mac_shot.sh <名字> [启动参数…]`，浅色、深色各一张写到 `/tmp/jarvis-mac-shots/<名字>-{light,dark}.png`。
  配对后的「选择引擎」一步：`-openEngineStep <机器 id>`（体验模式的 `server` 就行）。看玻璃材质要截真窗口，见 `docs/lessons/macos.md`。
  默认体验模式；`BUILD=1` 先编译，`APP=` 指定 .app，`SIZE=1280x820`，`DELAY=4`，`APPEARANCES="light dark"`。
  例：`scripts/mac_shot.sh agents -tab dev -dev.openChat ""`、`scripts/mac_shot.sh chat -openSession macbook/a1f3c2d4-0000-4000-8000-000000000001`、
  `scripts/mac_shot.sh jarvis -tab assistant`、`scripts/mac_shot.sh inbox -tab mail`、`scripts/mac_shot.sh settings -tab settings`。
  背后是 DEBUG 开关 `-macSnapshot <png> [-macSnapshotDelay 秒] [-macSnapshotQuit YES] [-macWindowSize WxH] [-macAppearance dark|light]`（`Mac/App/MacSnapshot.swift`）：
  App 自己用 `cacheDisplay` 把窗口画进 PNG，不经过屏幕。
- 截图和屏幕上的差别（2026-10-31 试出来的）：
  - Liquid Glass / 毛玻璃画不出模糊，露出的是窗口底色；玻璃按钮只剩边框。看布局和文字够用，看材质要真机。
  - SwiftUI `List`（NSTableView）的行作为窗口的一部分画时是空的：截图时每一行单独再画一次、贴回原处；选中行的材质离屏画成黑块，改成画非激活窗口的选中色。
  - 深色下 List 行里的文字仍是浅色方案的颜色（深灰），`NSApp.appearance`、`preferredColorScheme`、截图时套 appearance、去掉图层滤镜都没用；行的 effectiveAppearance 是 DarkAqua。
    还没确认是截图的问题还是真的这样，要在解锁的 Mac 上看一眼（待办）。
  - `layer.render(in:)` 上下颠倒、`dataWithPDF` 大部分文字丢失，都不如 `cacheDisplay`。
  - `-AppleInterfaceStyle Dark` 作为启动参数对 App 不生效，深浅色用 `-macAppearance`（在 `applicationWillFinishLaunching` 里、窗口建出来之前设）。
  - 分栏的侧栏 / 列表栏浮在玻璃容器（`NSContainerConcentricGlassEffectView`）里，容器的内容层是个空 layer，`cacheDisplay` 跳过了它：Agents 顶上的快速筛选、Jarvis 的历史侧栏原来截出来是空白 / 白卡片。
    现在截图时把容器里的视图逐个画、再叠上工具栏（`0af14af`，只在 DEBUG）。列表栏顶边和左边留着一条约 12pt 的黑边（修之前就有），只在截图里、真窗口不受影响，没再追。
  - 2026-11-01 还画不对的，要解锁看真窗口：深色下工具栏搜索框是白色空胶囊；非激活窗口里着色的进度条（启动器额度）是灰的；白字的醒目按钮（知识库「问 Jarvis」）看不见；
    连接详情浮层；快捷命令面板和终端键栏是深底深字；对话页顶上同空间标签的胶囊条；`-openAssistantSidebar YES` 的 Jarvis 侧栏。
  - 截图的坑：`-showMailAskClaude`、`-openMonitor` 这类 DEBUG 钩子挂在 onAppear 上，每次出现都会再触发（只在 DEBUG、只为截图，不改）；`-showWakeDrawer`、`-showMenu` 要 `DELAY` 11 秒左右；
    共享 UserDefaults 里别的运行留下的草稿（比如一个 `/`）启动时会弹出斜杠菜单；假 hub 换数据重启后缓存合并会多出一张重复的错误卡片，`-openChatsFresh` 能清掉。

## Mac 版现在的样子（阶段 0：能编、能跑）

- 壳（`Mac/App/`）：窗口左边一列六项（Jarvis / Agents / 收件箱 / 知识库 / 机器 / 设置，角标同 iOS 的 tab 栏），右边是 iOS 的页面本身；
  这一列贴着窗口左边、上下到顶（伸到红绿灯后面），侧栏底色，和页面之间一条分隔线；行和 Jarvis 页的历史列表（`AssistantSidebar`）同一套：
  同样的图标框和字号，选中是同样的灰色圆角底（`MacSectionRow`；2026-11-02 用户：系统的源列表放在旁边像没做过样式）。
  窗口工具栏不画底色和分隔线；红绿灯旁边一个按钮收起 / 显示这一列（和「显示 › 隐藏侧边栏」⌃⌘S 是同一个动作）。
  `horizontalSizeClass` 在 Mac 上恒为 `.regular`，所以 Agents、收件箱、设置、Jarvis 走的是 iPad / iPhone Duo 打开时的分栏布局。
  外层不用 `NavigationSplitView`：页面自己是分栏，分栏套在另一个分栏的详情栏里时，里面的详情栏按外层侧栏还在前面排版，对话被挤到右边、输入框被裁掉。
  页面的分栏在 Mac 上是三栏、第一栏空着（`SplitColumns`）：macOS 26 把分栏的侧栏画成离窗口四边一点的玻璃卡片，周围露出窗口底色
  （2026-11-02 用户：像浮在背景板上，有缺口），没有关掉的 API；内容栏是贴边的，列表放在内容栏，内容栏里的 `NavigationLink(value:)` 照样推进详情栏。
  代价：Agents、收件箱、设置的列表栏在 Mac 上不能收起（原来工具栏上的「隐藏列表」在红绿灯旁边，正好压在六项这一列上），Jarvis 页的历史列表照旧用顶栏的按钮收起。
  ⌘1…⌘6 切换；`Settings` 场景（⌘,）放设置；`onOpenURL` 处理 `jarvis://pair`；App 变成活跃时 `ensureConnected` + `refresh`。
  App 在后台不断开 WebSocket（Mac 应用常驻，hub 靠这条连接找到它）。
- 兼容层（`Mac/Compat/`）：`UIKitTypes.swift`（UIColor / UIImage / UIFont / UIEdgeInsets 的别名，UIPasteboard、UIApplication.open、UIDevice（含电池）、
  UIGraphicsImageRenderer、haptics 空操作、UIKit 语义色）、`SwiftUICompat.swift`（iOS 专用修饰符的同名 Mac 版本：`navigationBarTitleDisplayMode` 什么都不做、
  `.topBarTrailing` → `.primaryAction`、`.insetGrouped` → `.inset`、`fullScreenCover` → `sheet`、`.navigationBarDrawer` → `.toolbar`、`for: .tabBar/.navigationBar` 不做事、
  `horizontalSizeClass` 恒 regular、`EditButton` 空、`ImageRenderer.uiImage`…）、`MenuCompat.swift`（`UIAction` / `UIMenu` → `NSMenu`）、`NavigationCompat.swift`。
- 用 AppKit 重写的同名类型：终端（`Mac/Terminal/`，NSScrollView + `NSTextInputClient`，拼音、选择、⌘C/⌘V、滚轮、捏合 / ⌘± 调字号）、代码行与等宽文本
  （`Mac/Code/`，只画可见的行、可选中拷贝）、图片查看 / 缩放（`Mac/Media/`，NSScrollView 放大，←/→ 翻页）、长按菜单 → 右键、可选文本、
  网页（`Mac/Link/`，WKWebView 右键菜单加「AI 修改 / 作为上下文」）、形象帧动画、ChatPin、邮件 HTML、Jarvis 侧边栏抽屉（窄窗口时才用，宽窗口是一栏）。
- Mac 上没有的，及原因：
  - 实时活动 / 灵动岛：macOS 没有 ActivityKit。`LiveActivityController` 的 Mac 版只记下 hub 推来的 fleet 状态（给以后的菜单栏用），设置 › 通知里「实时活动」一节不显示，`IslandSettingsView` 不编译。
  - Apple Watch 同步：Mac 不和手表配对，`WatchSync` 是空的；设置里也不再显示「同步到 Apple Watch」（2026-11-01 前还显示，点了提示已发送）。
  - 拍照（`UIImagePickerController`）：`isSourceTypeAvailable(.camera)` 为 false，输入框不出「拍照」；图片从文件、剪贴板来。
  - 扫码配对（VisionKit `DataScannerViewController`）：换成「粘贴配对链接」按钮（`Mac/Settings/PairingPaste+Mac.swift`），或直接打开 `jarvis://pair?…` 链接。
  - 远程推送：target 还没有 APNs entitlement，`PushRegistrar` 不注册；App 开着时靠 WebSocket，本地通知照常。
  - 健康数据：macOS 有 HealthKit 的接口但没有数据，`phone_health_summary` 在 Mac 上不注册（`PhoneToolRegistry.healthTools`）；设置 › 连接器 › 这台 Mac 里也没有健康开关（`PhoneDataSource.onThisDevice`，有测试），「N 项已打开」同样不算它。页脚的说明改成了 Mac 的说法（问你的是这台 Mac）。待查：截图里设置页的长页脚（这里、通知页）只显示一行带省略号，加 `fixedSize` 也一样，分不清是 `-macSnapshot` 重画 List 行的问题还是真的被截，要解锁后用真截图看。
  - **Mac 是桌面端，不是手机**（2026-11-01 用户定：照微信「Mac 微信已登录」的做法）：以前 Mac 版也发 `register_phone`，hub 只有一个「手机」
    （`hub.phoneClient`），Mac 一直在线时 phone_* 全落到 Mac 上、hub 不再推送唤醒进了后台的 iPhone、Mac 的工具表还把 `phone_health_summary` 冲掉了。
    现在 Mac 发 `register_desktop`（`AppModel.registration`，有测试），phone_* 只认 iPhone；Mac 开着而 iPhone 不在时，Claude 拿不到手机数据，属预期。
    设置 › 连接器 里「这台 Mac」那一行在 Mac 上去掉了（hub 不会再调它的工具），欢迎页的说法改成数据来自 iPhone 上的 Jarvis。
    iPhone 那边：Agents 列表最上面一行「Mac 上的 Jarvis 已登录」，点开能开「手机静音」（Mac 在线时手机不推「需要你处理」「Agent 做完」，
    由 Mac 提醒；助手按你的要求发的提醒照推）、看登录时间、退出那台 Mac（吊销它的设备凭据，要重新配对）。协议见 `docs/protocol.md` 的 `desktops` / `register_desktop`。
    配对时 Mac 用配对 token 换的设备凭据现在要 kind `desktop`（`AppModel.credentialKind`，有测试），hub 存成 `other`、权限不变，设置 › 设备 里显示电脑图标。
    这次改动之前已经换过凭据的 Mac 在列表里还标成 phone，只是标签，撤销后重新配对就对了（不改）。
- 临时实现，等后面打磨：外层侧栏和页面自己的分栏是两层（六项是 HStack 里的一列，页面是第一栏空着的三栏分栏）；分页的 `TabView(.page)` 换成了默认样式 / 箭头翻页；
  `SwipeTo`、Agent 间的双指滑动在 Mac 上不做事；代码行只有整行选择；对话页的玻璃按钮、浮层菜单没有按 Mac 习惯调；
  字号跟随设置里的小 / 标准 / 大，但 Mac 没有动态字体（`UIFontMetrics` 按 iOS 的 body 表换算）。

## 菜单栏、Dock 角标、通知（Mac）

Dock 图标右键（`apple/Mac/App/MacDockMenu.swift`，有测试）：「需要你」「做完了」两组（和菜单栏列表同一个排序，各最多 8 个，点一下打开对话），再是「新建 Agent 对话…」。

`apple/Mac/App/MacAttention.swift`、`MacMenuBar.swift`，测试 `apple/JarvisMacTests`（`JarvisMac` scheme 的 test）。

- Mac 没有 APNs（target 没有推送权限），hub 推不到这里：`MacAttentionWatcher` 盯着 AppModel 的会话列表（App 开着时 WebSocket 一直连着），
  有 Agent 刚变成「需要你」/「完成未看」就发一条本地通知，规则和 hub 给手机推送的一样，读同一份 设置 › 通知（`NotifyPrefs`：需要你处理时、
  Agent 做完时 不推送 / 只推长任务（这一轮干了 `long_turn_seconds`，默认 60 秒以上）/ 每次都推）。不发的：App 在前台时正开着的那个对话、屏蔽了的、
  列表里刚出现的、答完紧接着做完的（同一次停下）、Jarvis 对话做完（它问你时照发）、体验模式、WebSocket 第一次连上后 3 秒内
  （之前的列表是上次退出时的缓存，和第一次刷新的差别是 App 关着时的旧事；按连上算，不按启动算，网络晚到也不会补发一堆）。
  第一次真有通知要发时才申请权限，答应了再发（问的时候加进去的会丢）。屏蔽 / 取消屏蔽立刻改 Dock 角标。
- 通知带和 hub 推送一样的 userInfo（`machine_id` / `session_id` / `interaction_id`）和类别：授权有「允许 / 拒绝」，计划有「批准 / 打回修改」，
  点按钮由 `MacAppDelegate` 直接交给 hub，点通知打开对话；同一个 Agent 的第二条「做完了」替换第一条。
- Dock 角标：需要你的数量（不算屏蔽的），和侧边栏的角标同口径。
- 菜单栏（`MenuBarExtra`，菜单样式，主窗口关了也在）：图标旁是需要你的数量；菜单里分「需要你」（等得最久的在前）、「正在工作」、
  「做完了，还没看」（最近 5 个）。需要你的一项是子菜单：说了什么 + 卡片上一点就能答的那几个（单个问题的选项、允许 / 拒绝、批准计划、
  hub 读出来的终端短选项）+ 打开对话；别的点一下打开对话。最后是「打开 Jarvis」「退出 Jarvis」。

## 侧边栏的「知识库」「机器」（Mac）

选中的 知识库 / 机器 跨启动记住（`mac.section`；tab 由 AppModel 记）。启动参数指定了 tab / 对话（`-tab`、`-openSession`…）时以参数为准、清掉记住的选择。

壳的侧边栏在 iPhone 的四个 tab 之外多两项：知识库（`KnowledgeView`，手机上在 Jarvis 里）、机器（`MachinesView`，手机上在抽屉 / Agents / 设置里），
⌘1…⌘6 依次是 Jarvis、Agents、收件箱、知识库、机器、设置。这两项不是 AppTab：选中时记在 `MacShellState.section`，`AppModel.selectedTab` 不动；
通知、⌘K、链接把 tab 换走（或打开对话）时清掉，回到 tab 对应的那一项（`MacSection.shown / pick`，有测试）。DEBUG：`-macSection knowledge|machines`。

## Agent 菜单（Mac）

`apple/Mac/App/MacAgentCommands.swift`（有测试）。对着屏幕上的对话（`AppModel.visibleSession`）：中断 ⌘.（工作中才能点）、
批准当前待处理 ⇧⌘⏎（允许工具 / 批准计划，计划按「自动接受编辑」批，和通知上的「批准」一样）、拒绝 ⇧⌘⌫（只拒绝工具；打回计划要写理由，在卡片里）、
下一个需要你的 ⌃⌘N（等得最久的在前，循环）、标为已读、置顶 / 取消置顶、屏蔽 / 开启通知、拷贝会话 ID、拷贝恢复命令。
回答 ⌥⌘1…9（子菜单，2026-11-01）：只有一个问题、单选时列它的选项（最多 9 个），终端对话框列不用输入的选项；Claude 标的「(Recommended)」写成「（推荐）」，
回传的答案还是原文（`MacAgentCommands.choices`，有测试；菜单栏图标的快捷回答同样改了）。多个问题、多选、要写字的还在卡片上。
不用 ⌘1…9：⌘1…6 是侧边栏分区。`-openSession <m>/<s> -macMenuLog YES` 把 Agent 菜单当时的样子打进日志（锁屏下没有 key window，
菜单不打开就不刷新，它先替菜单走一遍打开时的刷新）：演示里 `macbook/…0002` 的问题是「Swift Testing（推荐）⌥⌘1、XCTest ⌥⌘2」。
交给 Jarvis ⌘J（和行菜单一样，把 Agent 的状态和最后的回复交给主对话）；重命名… / 清空上下文… / 关闭 Agent…：经 `MacShellState.chatRequest` 让对话页弹它自己的对话框
（和 ⋯ 菜单、输入框的 /clear 是同一个），清空只对 herdr 里的 Claude 可点、关闭只对还在跑的。截图：`-openSession <m>/<s> -macAgentAsk rename|clear|close|inspector`。

快捷键和清单最早写的不一样，因为撞了：⌘⏎ 是输入框的发送（`ComposerParts`），⌘⌫ 在输入框里是删到行首（菜单快捷键会抢走），
⇧⌘N 是新建 Jarvis 对话，⌥⌘N 是收件箱的写邮件。
清单里「在新窗口打开 ⌥⏎ / ⌘⏎（列表里）」没接：菜单快捷键在整个窗口生效，⌘⏎ 是输入框的发送，⌥⏎ 在文本框里也有用；只在列表获焦时才算数得靠列表的键盘处理，要真按键才验得了（锁屏下发不了），先只放在 Agent 菜单和行的右键菜单里。

## 对话单独开窗口（Mac）

`apple/Mac/App/MacChatWindow.swift`：`WindowGroup("对话", id: "chat", for: SessionKey.self)`，里面就是对话页本身，窗口标题是对话标题，
几个 Agent 可以并排或放到另一块屏幕上。入口：Agents 列表行的右键菜单「在新窗口打开」、Agent 菜单「在新窗口打开」。
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

显示 › 隐藏侧边栏 / 显示侧边栏 ⌃⌘S（2026-11-01）：收起窗口最左边的分区栏（`MacShell`），跨启动记住（`mac.sidebar.hidden`），收起后 ⌘1…6 照样换分区；页面自己的列表栏不受影响（它们有自己的按钮）。快捷键测试里有它，截图：`-mac.sidebar.hidden YES`。
侧栏脚的连接状态收起时挪到页面底部（只在连不上 hub 时出现）：知识库 / 机器自己没有显示它的地方（`-macSection machines -simulateOffline YES -mac.sidebar.hidden YES` 截图看过）；Agents 页的标题位置本来也写「等待网络」，收起时两处都有，先这样。
分区上的数量角标也跟着收起：Agents 的需要你还有 Dock 角标和菜单栏图标，收件箱的待处理数收起时就看不到了（review 第 7 轮记下，没改：收起本来就是不要这一栏）。

`apple/Mac/App/MacViewCommands.swift`（有测试）。显示 › 放大字号 ⌘+ / 缩小字号 ⌘- / 标准字号 ⌘0：在 设置 › 字号 的 小 / 标准 / 大 之间走一步，到头就停，
和设置里改是同一个值（整棵视图按新字号重建）。前往 › 上一个 / 下一个 Agent ⌘[ ⌘]：按 Agents 列表的顺序（`AgentListOrder`：需要你 → 完成未看 → 工作中 → 已看），
循环，不含 Jarvis 对话；没开着 Agent 时从第一个开始。
编辑 › 查找… ⌘F（`apple/Mac/App/MacFind.swift`，有测试）：当前窗口的搜索框获焦（工具栏的搜索项优先，再是页面里的；Agents、知识库用 `-macFind YES` 验过），
没有搜索框的页面什么都不做；焦点在自带查找栏的文本视图（diff、输出）里时打开它的查找栏。
文件查看器里的代码也走这个 ⌘F（2026-11-01，`Mac/Code/CodeFindBar.swift`，匹配逻辑 `CodeFind.swift`，有测试）：顺序是 自带查找栏的文本视图 → 响应链上的
`MacFindable`（代码视图）→ 窗口里的搜索框 → 窗口里看得见的 `MacFindable`（文件刚打开、还没点过代码）。代码上方出查找栏：不区分大小写，所有匹配浅黄、
当前的系统查找黄并滚到看得见，「3 / 14」或红色「没有找到」；⏎ 下一个、⇧⏎ 上一个（两头循环），Esc / ✕ 关闭、焦点回到代码。这三个键用本地按键监听在窗口之前接住
（文件查看器的「完成」占着 Esc，不先接就会关掉整个 sheet），拼音组字时让给输入法。截图：`-openFile <路径> -macFileFind <查询>`（体验模式的 hub 在 Mac 上给一个编造的 Swift 文件）。
查找下一个 ⌘G / 查找上一个 ⇧⌘G（2026-11-01，`MacFind.findNext`，`CodeFindNextTests`、`MacFindTests`、`MacMenuShortcutTests`）跟着 ⌘F 走：带查找栏的文本视图（diff、输出）跳它的匹配，
代码查看器跳自己的查找栏，查找栏关着时用上次的词重新打开。没做：区分大小写 / 整词 / 正则、行内部分选择；每次输入在主线程上重搜整个文件，10 万行的文件没量过。真按键没试过（锁屏）。
显示 › 紧凑列表、分组方式（按项目 / 状态 / 机器）、全部折叠 / 全部展开：和 Agents 列表左上 ⋯ 菜单是同一组设置（`dev.compact`、`dev.grouping`、折叠由列表登记的 `AgentsListMenu.Actions` 做）。
会话信息在 Mac 上是对话右侧的一栏（Agent › 显示会话信息 ⌥⌘I，或点标题；`apple/Mac/Chat/MacInspector.swift`），宽度可拖、记在 `mac.inspector.width`，
窗口太窄时盖在对话上。没用 SwiftUI 的 `.inspector`：放在 Agents 分栏的详情页里整栏是空的；`SessionInfoSheet` 在 Mac 上也不再包 NavigationStack（会接管详情栏的导航、对话页消失）。
回到最新 ⌘↓ 是对话页右下角 ↓ 按钮的快捷键（悬停提示同），只在往上翻过、按钮出现时有效；其余时候 ⌘↓ 还是输入框的「移到末尾」。
显示 › 刷新 ⌘R（2026-11-01，`MacViewCommands.swift`、`Mac/App/MacPageRefresh.swift`，`MacViewCommandsTests`）代替 iPhone 的下拉刷新，整个 App 只有这一个 ⌘R：
自己加载数据的页面（知识库、机器和它的监控页、对话的文件 / Git 面板）用 `macRefreshes` 登记自己的加载，⌘R 刷新 key 窗口里最后登记的那个；
没有就按分区刷新 hub 的数据（收件箱 `InboxStore.refresh`，Jarvis `refreshAssistant`，其余 `AppModel.refresh`）。收件箱、文件、Git 工具栏上的刷新按钮不再带 ⌘R。设置的子页面不在内；真按键没试过（锁屏）。

## 快速打开、新建（Mac）

`apple/Mac/App/MacQuickOpen.swift`（搜索、排序，有测试）、`MacQuickOpenPanel.swift`（浮层）。

- ⌘K（菜单「前往 › 快速打开…」）：窗口上方浮起一个像 Xcode「Open Quickly」的面板，焦点在搜索框，↑↓ 选、⏎ 打开、Esc 或点外面关。
  搜 Agent（标题 / 目录 / 机器 / 空间 / 最后一句）、Jarvis 对话、收件箱（标题 / 发件人 / 预览）、窗口的四个分区、两个「新建」。
  标题开头命中 100、包含 80、每个词都在别处出现 40；同分时需要你的在前、再按时间；分组顺序固定（Agent、Jarvis 对话、收件箱、前往、动作），
  除非后面的组命中得更好（搜「设置」时「前往 › 设置」排在只是提到设置的 Agent 前面）。没输入时列出全部，需要你的在前。
  DEBUG：`-openQuickOpen <搜索词>`（`""` 为空）。
- 新建 Agent 对话 ⌘N（菜单「文件」、菜单栏、⌘K）：iOS 的新对话表单放在窗口的 sheet 里，「开始对话」在 Mac 上是 sheet 底部「取消」旁边的默认按钮（⏎），
  不是 iOS 浮在底部的大按钮；开起来后在 Agents 里打开。新建 Jarvis 对话 ⇧⌘N：切到 Jarvis 的新对话。没配对时这几条命令不可用。DEBUG：`-openNewAgent YES`。
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

- 三块独立的视图，给主窗口的三栏接（区域 A）：`MacInboxFilters`（侧边栏「收件箱」「类型」两组行：全部 / 飞书 / 各个邮箱，@我 / 私聊 / 群聊 / 邮件 / Agent，
  带「需要你」的数量；行的 tag 是 `MacInboxFilter`，侧边栏的 selection 用这个类型就能原生选中，或者绑 `MacInboxState.shared.filter`）、
  `MacInboxList`（列表栏）、`MacInboxDetail`（详情栏）。三者共用 `MacInboxState.shared`（筛选沿用 iOS 的 `inbox.source` / `inbox.kind`、搜索词、选中、展开的分组、邮箱列表、要打开的弹层）。
  A 接好三栏之前，`MacInboxPage`（筛选列 | 列表 | 详情，列表宽度可拖、记在 `mac.inbox.listWidth`）放在壳的「收件箱」里。
- 列表：系统选中（⇧ / ⌘ 多选），↑↓ 移动，**E 或 ⌫ 标为已处理**并选中下一件（像 Mail 删信），⏎ / 双击在新窗口打开（Agent 行：打开它的对话），
  右键：在新窗口打开、AI 草拟回复 / 查看草稿、交给 AI、已处理 / 移回（多选时「N 件都标为已处理」）、拷贝内容、在飞书中打开；触控板左滑 = 已处理；悬停有浅底。
  工具栏：刷新（⌘R 是 显示 › 刷新，收件箱在屏幕上时刷新它）、写邮件 ⌥⌘N、管理邮箱；搜索在工具栏（`.searchable(placement: .toolbar)`）。分组标题可点开 / 收起，「全部看过」「全部清除」照旧。
- 详情：飞书会话（从第一条新消息开始）/ 邮件 / 云文档条目，顶上一行 AI 摘要（和「AI 分身正在处理」），iOS 的动作条换成工具栏按钮（AI 草拟回复、交给 AI、已处理 / 移回）。
  窗口标题是条目标题，副标题是来源。人、别的会话这类飞书页面在详情栏自己的栈里推进去；飞书文档、网页链接在右侧的链接面板里开。多选时详情是「已选 N 件 · 全部标为已处理」。
  弹层（草稿、交给 AI、写信、邮箱、添加邮箱）是窗口内的 sheet，`.macSheet()` 给最小尺寸并把 `Form` 换成 `.grouped`（Mac 默认的两列表单会把 iOS 的分组行挤到标签列、截掉）。
  后来 `macLook` 在整棵树上设了 `.formStyle(.grouped)`，sheet 也继承，所以没套 `macSheet` 的表单（新建定时任务等）也是分组的了（截图对比过 `-openAssistantPage newRoutine`）；`macSheet` 只剩给最小尺寸。连接器里「这台 Mac」的图标在 Mac 上是笔记本（`Theme.Icon.thisDevice`）。
  grouped 表单把带标题的 `TextField` 画成「左边标签 + 右边输入」，多行的提示词 / 正文因此只占右半边：Mac 上这几处改成 `prompt:` + `.labelsHidden()`（新建定时任务、添加邮箱的整段粘贴截图看过；新建对话的第一条消息、写信正文同样改了，在截图范围外没看到）。
  知识库详情 › 打标弹出的 sheet 没继承 `macLook` 的 grouped，还是两列（设置里推入页面弹出的新建定时任务却继承了，原因没查清），这类 sheet 自己套 `macSheet()`：打标（`-showKnowledgeEntry projects/weather-app -showKBEditor YES` 截图看过，标签、滑块占满一行）、新建对话（知识库 › 开 Agent 那条路径没截到）。
- 链接面板 `.macLinkPanel($link, key:)`：详情栏右侧并排（页面变窄、不被盖住；窗口太窄放不下两边时盖在上面），宽度拖、记在 `mac.linkPanel.width`；
  头部：后退 / 前进、标题和域名、重新载入、⋯（在浏览器打开、在单独窗口打开、拷贝链接、分享）、关闭（⌘W，面板开着时先关面板）。
  网页来自 `WebViewPool`（和 iOS 同一个池，预加载、复用），右键选中文字有「AI 修改」「作为上下文」；飞书文档 / removed在面板里用原生阅读器（右上「把这篇文档作为上下文」）。
  `key` 是对话（对话页）；收件箱、知识库里没有，「作为上下文」进 Jarvis 主对话的输入框、「AI 修改」直接问 Jarvis。
  `.macOpensLinksInPanel($link)` 把一块视图里的 `openURL` / `openLink` 接到面板上（收件箱详情用；邮件 HTML 里的链接仍交给浏览器）。对话页、知识库在 Mac 上也换成了面板（`#if os(macOS)`）。
- 单独的窗口：`WindowGroup(id: "link", for: URL.self)`（`MacLinkWindow`）、`WindowGroup(id: "inbox-item", for: String.self)`（`MacInboxItemWindow`）。
- DEBUG：`-openInboxFirst YES`、`-openInboxMatch <标题里的字>` 选中一件，`-showInboxAction draft|handoff` 打开弹层，`-openInboxLink <url>` 打开链接面板，`-addMail YES` 管理邮箱。
  例：`scripts/mac_shot.sh inbox-link -tab mail -openInboxMatch 下周的分享 -openInboxLink https://example.com/`。
- 截图补充（2026-11-01）：窗口上的 sheet 是另一个窗口，`-macSnapshot` 现在把它另存成 `<名字>.sheet.png`；sheet 里有文本编辑器时 `terminate` 会被挡住，截完 1 秒后直接退出。
  锁屏时未签名的新构建读另一个构建写的钥匙串条目会一直卡在 securityd（跳过授权界面也一样），截图模式（`-macSnapshot`）下 Mac 的 DEBUG 构建不读钥匙串。

## 验收清单

下面是 iOS App 的全部功能，Mac 版逐条对上（做完一条勾一条）。

2026-11-01 全部过了一遍：分 5 组并行，每组用离屏截图（体验模式和 `scripts/mock_hub.py` 的假数据，深浅色）和单元测试（`JarvisMacTests`）逐条核对，顺手修了能修的；再由一个 reviewer 逐条试着推翻打的勾，推翻了的不勾，写清看到了什么、没看到什么。
核对后 106 条 [x]、118 条 [ ]。没勾的每条下面都写了原因：「要到 Mac 前看」是要解锁后在 Mac 上点、按、看的，「没做」是还没做的，「Mac 上没有」是 Mac 上本来就不该有的。
待你确认：`-showSampleScreen` 的示例「终端在问你」里原来有像内部工具 / 服务的名字，已在提交「chore: the sample terminal dialog uses made-up names only」里全换成编造的名字；以前拍的截图里可能还是旧文字。


## 0. App shell / navigation
- [x] 4 tabs with badges: Jarvis (pending assistant questions), Agents (pending interactions, excluding muted), 收件箱 (inbox action count), 设置 (「体验」 badge in demo) — Jarvis/Views/RootView.swift
  - Mac（2026-11-01 核对）：侧边栏角标和 RootView 同口径，截图里 Agents 2、收件箱 3、设置「体验」；体验数据里没有助手的待答问题，所以 Jarvis 没有角标，是对的。
- [x] Toast overlay (success / info-blue / error), haptics (success/warning/error) — Views/RootView.swift (ToastOverlay), App/AppModel.swift。Mac：浮层照常（拖入、链接拷贝等截图里看过），没有触感
- [x] Rebuild whole tree when style/persona changes, keep tab + open chat — Views/RootView.swift
  - Mac（2026-11-01 核对）读代码：整棵 Mac 树和 RootView 一样挂 `.id(model.look)`，tab、打开的对话、侧栏分区都存在视图外面，重建后还在；没截换风格的图。
- [x] Width-driven layout: compact = single NavigationStack, regular = NavigationSplitView (list | detail), shared path, `.splitDetailPage()`, sidebar width 340–480, SidebarTopFill — Views/SplitNavigation.swift
  - Mac（2026-11-01 核对）：Mac 上 `horizontalSizeClass` 恒为 regular，只走 NavigationSplitView（compact 本来就走不到）；2000pt 宽窗口截图里列表栏约 400pt，对话在详情栏、没有返回箭头。
- [x] Readable width cap (chat 760pt, welcome 560pt) — Views/Chat/ChatView.swift, Shared/Design/Theme.swift
  - Mac（2026-11-01 核对）：2000pt 宽窗口截图：对话栏约 760pt 居中，欢迎页（`-openWelcome`）约 560pt 居中。
- [x] URL handling: `jarvis://pair?...` pairing, `jarvis://open?machine=&session=` (Live Activity) — App/JarvisApp.swift。Mac：`MacOpenURL`（有测试），open 打开那个对话（从别的页面也切过去，`-macOpenURL <链接>` 验过），pair 只在没配对 / 体验模式时接受；对话里的 jarvis:// 链接仍只拷贝（LinkPolicy）。外面来的链接交给已经开着的主窗口（`handlesExternalEvents`）：以前每来一个链接就多开一个主窗口（`open -a Jarvis.app jarvis://…` 两次 → 3 个，`-macWindowLog YES` 记数）
- [ ] Scene phase: reconnect + refresh on active, disconnect socket on background — App/JarvisApp.swift
  - 要到 Mac 前看：切到别的 App 再切回来，列表刷新、断了的连接会重连。Mac 有意不在后台断开 WebSocket（见「阶段 0」），锁屏时场景状态不变，离屏验不了。
- [ ] Remember last tab / last open chat / reading position across cold start (`chat.read.<session>`) — App/AppModel.swift, Views/Chat/ChatView.swift
  - 要到 Mac 前看：在对话里往上翻，⌘Q 后重开，看是不是回到同一行（⌘Q 不一定触发 onDisappear）。只带 `-app.tab chats -dev.openChat …` 启动时回到了 Agents 和那个对话，tab 和对话的恢复看过。
- [x] DEBUG launch args (-tab, -openSession, -openPane, -openTerminal, -lark-connector, etc. list in docs/ios.md) — App/JarvisApp.swift + views
  - Mac 上截图一直靠它们：`-tab`、`-openSession`、`-openTerminal`、`-openTerminalSettings`、`-openAssistantPage`、`-showInboxAction`、`-showNewChat`、`-addMail`、`-openWelcome`、`-showKnowledgeEntry` 都试过；Mac 自己的另见各节（`-macSection`、`-macAgentAsk`、`-openChatWindow`、`-macWindowLog`…）。

## 1. Jarvis tab (assistant)
- [x] Headless Claude assistant conversations (main conversation `assistant.main` + side branches); first launch opens main — Views/Assistant/AssistantView.swift, App/AppModel.swift (assistantMain, refreshAssistant)
  - Mac（2026-11-01 核对）截图：主对话打开，侧栏置顶「主对话」、最近里有几条分支对话；逻辑是共用的 AppModel。
- [x] Home: greeting + available devices + 6 suggestions; launcher/model chip list; StarterComposer (or BeautifulStarterComposer) with photos — Views/Assistant/AssistantView.swift (AssistantHome, StarterComposer, StarterModelChip, AssistantToolsRow)
  - Mac 截图看过（`-tab assistant -assistant.current ""`）：问候、在线机器 + 这台 Mac、6 个建议、启动器 / 权限 / 思考 chip、输入框；图片走拖放 / 粘贴（`MacComposerFiles`）。
- [ ] First message shows bubble + 「正在准备」 immediately, session starts in background; warm assistant — AssistantView.swift, AppModel.warmAssistant
  - 要到 Mac 前看：Jarvis › 新对话里打一句按 ⏎，气泡和「正在准备」应立刻出现。没有能自动发第一句的 DEBUG 开关。
- [x] Thinking hidden; tool calls folded into 「工具 N · 电脑、iPhone…」 — Views/Chat/ChatLines.swift, ChatView.swift
  - Mac（2026-11-01 核对）深浅色截图：没有思考块，工具调用折成回复上方的「工具 3 · 1 分 ›」。
- [x] Floating top bar with fog, center persona dot + name capsule; connection state replaces name — Views/Assistant/AssistantTopBar.swift (AssistantTopBar, TopFog)
  - Mac（2026-11-01 核对）截图：顶栏浮在对话上、带雾，中间是形象和「Jarvis」胶囊；`-showConnection connecting` 时胶囊换成「连接中…」。
- [ ] Avatar menu: 知识库, 详细信息 (profile), 回到主对话, 拷贝目录 / 拷贝信息 / 拷贝会话 ID (「✓ 已拷贝」 3s), 启动器 submenu (switch launcher, applies via --resume next message), 会话信息, 中断, 连接详情 — AssistantTopBar.swift
  - 要到 Mac 前看：点头像逐项点一遍：拷贝类显示「✓ 已拷贝」3 秒，启动器子菜单能切换。离屏打不开菜单，只截到其中「会话信息」的 sheet（`-showInfo`）。
- [x] Left button: history sidebar; right button: new chat (disabled on new-chat page) — AssistantTopBar.swift
  - Mac（2026-11-01 核对）深色截图：左边侧栏开关、右边新对话按钮都在（浅色下玻璃按钮只剩边框）；新对话页上按钮置灰没截，和 iOS 同一份代码。
- [ ] Sidebar: search, new chat, Jarvis card (online/offline + machine → profile), 定时任务 / 知识库 tiles, 置顶 (main pinned, long-press pin, `assistant.pinned`), day groups 今天/昨天/最近7天/更早 (8 + 查看全部), row = title + preview + time + status dot (need-you red / unread blue / working spinner), long-press 拷贝会话 ID / 结束 / 置顶, 设置 row in drawer — Views/Assistant/AssistantSidebar.swift
  - 要到 Mac 前看：右键一行（拷贝会话 ID / 结束 / 置顶）、搜索按钮、查看全部、行上的状态点（连真 hub）。截图修好后（`0af14af`）深浅色都看到标题 + 搜索、新对话 / 定时任务 / 知识库、置顶 / 最近、底部 Jarvis 卡片；这是现在的布局，清单里的磁贴和按天分组是旧设计（iOS 也一样）。
- [ ] Drawer gesture: pan anywhere, 45° rule, yields to horizontally scrollable content, velocity/half settle, page card scale/offset, tab bar fade (UIKit host) — AssistantSidebar.swift (DrawerPan, DrawerPageMotion, DrawerSidebarHost), Views/Assistant/DrawerProbe.swift
  - Mac 上没有：窗口恒为 regular，侧栏是分栏的一列加开关按钮；窄窗口才用的抽屉（`AssistantDrawer+Mac.swift`）没有拖动，`DrawerPan` / `DrawerProbe` 只在 iOS 编译。
- [x] Regular width: sidebar = split view left column, toggle button — AssistantView.swift, SplitNavigation.swift
  - Mac（2026-11-01 核对）截图：Jarvis 侧栏是 NavigationSplitView 的左栏，顶部右侧有开关按钮。
- [ ] Jarvis avatar (mask face, eyes move when working) — Views/Assistant/JarvisAvatar.swift
  - 要到 Mac 前看：给 Jarvis 发一句，看它工作时眼睛动不动。脸在截图里画出来了，眼睛要 display link，锁屏时不走。
- [x] Profile page: identity, machine, online/working, 「接下来」 3 items, 主动做事 (tasks, run history), 了解你 (关于我, 记忆), 设置 (launcher, 管理启动器), capabilities list — Views/Assistant/AssistantProfileView.swift (AssistantProfileView, AssistantUpcomingRow, AssistantRunsView, AssistantRunRow)
  - Mac 截图看到（1280×1500 窗口）：身份、所在机器、在线 + 下一件、接下来 3 项、主动做事（定时任务、运行记录）、了解你（关于我、记忆）、设置（启动器、管理启动器），底部的能力列表 2026-11-01 核对时也看到了。
- [x] 「交给 Jarvis」 from other pages sends `<jarvis-context>` into main conversation — AppModel.askJarvis
  - Mac（2026-11-01 核对）读代码：行菜单和 Agent › 交给 Jarvis ⌘J 都调共用的 `AppModel.askJarvis`（同一条 `<jarvis-context>` 路径），`MacMenuShortcutTests` 里有 ⌘J；没对真 hub 发过。

## 2. Agents tab (dev list)
- [ ] Always-visible search; quick filters 全部/需要你/未读/工作中 with counts; snapshot (`kept`) semantics — Views/Chats/ChatsView.swift (QuickFilterBar, AgentsList)
  - Mac（2026-11-01 核对）：原来那块空白是离屏截图的问题（列表栏浮在玻璃容器里，`cacheDisplay` 跳过了它，`0af14af` 已修），浅色截图里搜索框和 全部 / 需要你 2 / 未读 11 / 工作中 5 都在。要到 Mac 前看：深色截图里工具栏搜索框是一块白色空胶囊，没有占位文字和图标，看真窗口是不是这样。
- [x] Grouping menu: 按项目 / 按状态 / 按机器 (`DevGrouping`, remembered); one rank order — ChatsView.swift, Views/Chats/AgentListOrder.swift, Views/Chats/AgentsMenus.swift (AgentsListMenu)
  - Mac（2026-11-01 核对）截图：按项目、按状态（需要你 / 完成未看）、按机器三种都看过，记住的 `dev.grouping` 生效；显示 › 分组方式 是同一个设置。
- [x] Card vs compact list (`dev.compact`); compact rows on one scale — ChatsView.swift (SessionRow), Views/Chats/CompactList.swift
  - Mac（2026-11-01 核对）截图（`-dev.compact YES`）：一行一条、同一个比例，有「还有 N 个空闲」；卡片模式见默认截图。显示 › 紧凑列表 是同一个设置。
- [x] 「需要你处理」 cards with quick-action buttons (card mode) / CompactInteractionRow — Views/Interactions/InteractionViews.swift (InteractionPreviewCard), CompactList.swift
  - Mac 截图看到：卡片模式的计划等你审批、Claude 在问你 + 选项按钮；紧凑模式的 `CompactInteractionRow` 2026-11-01 核对时也截到了。
- [x] Pinned agents, pinned spaces, busy / just-done / muted-running / idle spaces (folded), 「还有 N 个空闲」 — ChatsView.swift (IdleFoldRow, QuietSpaceRow, SpaceHeader, StateCounts)
  - Mac（2026-11-01 核对）截图：置顶的空间排第一、标题上有状态计数、「还有 2 个空闲」折叠行；置顶的 Agent 和屏蔽中还在跑的空间是同一份行代码，体验数据里没有，没截到。
- [ ] Fold/unfold spaces and section headers (`dev.folded`), 全部折叠/全部展开 — ChatsView.swift, AgentsMenus.swift
  - 要到 Mac 前看：点空间标题的 ⌄ 折叠 / 展开，再试 显示 › 全部折叠 / 全部展开。用 `-dev.folded` 启动没折上（分组 id 猜错了），没看到。
- [ ] Mark all read (filter + per-space, with confirm) — ChatsView.swift, App/AppModel.swift (markSeen)
  - 要到 Mac 前看：未读筛选下全部标为已读（带确认），空间 ⋯ › 全部标为已读（N）。都在菜单和确认框后面，离屏打不开。
- [ ] Swipe: left = pin / interrupt, right = close; long-press row menu (rename, mute/unmute, 交给 Jarvis, 拷贝恢复命令, 关闭 Agent…, stop loop) — ChatsView.swift, AgentsMenus.swift (AgentRowMenu)
  - 要到 Mac 前看：右键一行逐项点（Mac 版第一项是「在新窗口打开」），再在触控板上对一行左右滑。长按在 Mac 上就是右键菜单，离屏打不开。
- [x] Close agent confirmation sheet — DesignSystem/Components.swift (CloseAgentSheet, CloseAgentConfirmation)
  - Mac（2026-11-01 核对）截图（`-showCloseAgent YES`）：「关闭这个 Agent?」、标题、所在位置、两行说明、关闭 Agent / 取消。
- [ ] Space header ⋯ menu: 打开空间, 置顶, 屏蔽/开启通知, 重命名空间, 全部标为已读 — AgentsMenus.swift (SpaceHeaderMenu), ChatsView.swift (SpaceMenuItems, RenameSpaceAlert)
  - 要到 Mac 前看：点空间标题上的 ⋯ 逐项试（重命名空间会弹输入框）。⋯ 按钮截图里都在，菜单离屏打不开。
- [x] Rename agent (herdr tab + Claude session name) — ChatsView.swift (RenameSessionAlert), AppModel.renameSession
  - Mac（2026-11-01 核对）截图（`-macAgentAsk rename`）：重命名框有 herdr 标签名和会话名两栏、取消 / 保存，和 iOS 同一个 `RenameSessionAlert`。
- [x] Pins / mutes synced with hub (/api/v1/pins, /mutes), optimistic, per-agent override of space mute — App/PinsStore.swift
  - Mac（2026-11-01 核对）：共用逻辑（PinsStore、togglePin / toggleMute），Mac 的菜单调同一组函数，`MacAttention` 的测试覆盖屏蔽对 Dock 角标的影响，体验数据的置顶空间显示为置顶；没对真 hub 跑过。
- [x] Badges: 「⟳ 下次 12:20」 loop, 「⚙ 后台 N」, 「👥 子 Agent N」, muted 🔕 styling — ChatsView.swift (SessionRow)
  - Mac（2026-11-01 核对）：清单写旧了：现在列表行（iOS 也一样）不再带循环 / 后台 chip，挪到了对话页顶上的 `ActivityChips`，Mac 上看到「⚙ 1」；屏蔽的 🔕 和淡化在共用的 SlotRow 里（读代码，体验数据没有屏蔽的 Agent），子 Agent 没截。
- [ ] Machine filter menu, 「机器」 entry, 新建会话 button — ChatsView.swift (TopBar), AgentsMenus.swift
  - 要到 Mac 前看：打开工具栏的 ≡ 菜单选一台机器筛，再点「机器」（2026-11-01 起原来的「机器监控」并进了机器页）。菜单按钮和新建会话按钮截图里都在；Mac 上它也是侧栏的「机器」。
- [x] Connection status in title slot — Views/ConnectionTitle.swift
  - Mac（2026-11-01 核对）截图（`-showConnection connecting`）：列表栏标题位置是「连接中…」加转圈，侧栏底部也有。
- [ ] Pull to refresh — ChatsView.swift
  - Mac（2026-11-01 核对）：Mac 上没有下拉手势，换成 显示 › 刷新 ⌘R（见「显示 / 前往菜单」）：自己加载数据的页面登记自己的加载，否则按分区刷新 hub 数据，`MacViewCommandsTests` 有测试。要到 Mac 前看：在 Agents、收件箱、知识库、机器、对话的文件 / Git 各真按一次 ⌘R，看对应内容重新加载。
- [x] Destinations registry (SessionKey, space, machine pages) — ChatsView.swift (AgentsDestinations)
  - Mac（2026-11-01 核对）截图：对话（SessionKey）、空间页（`-openSpaceFromChat`）、机器页（`-openMachinePage`）都推得进去。

## 3. Spaces & machine pages
- [x] Space page: agents in herdr tab order (SlotCard), machine button, menu (rename, mute, mark read, 新标签 / 在这个空间开新标签) — Views/Chats/SpaceView.swift (SpaceView, SlotCard)
  - Mac（2026-11-01 核对）截图（`-openSpaceFromChat YES`）：机器按钮 + 目录、按 herdr 顺序的 SlotCard 带状态、「在这个空间开新标签」、工具栏 ⋯；菜单项离屏没打开。
- [ ] Sibling tabs strip in chat header (state-colored capsules, swipe, ⋯ list with red dot, 「空间 X 的所有 Agent」) — SpaceView.swift (SiblingTabs, SiblingList)
  - 要到 Mac 前看：对话页顶上那排按状态着色的胶囊（横向滚动）和 ⋯ 上的红点。⋯ 的列表（`-showSpaceTabs YES`）截图对：6 个标签页、新标签、空间的所有 Agent、状态色和当前的勾；胶囊条离屏没画出来，分不清是不是截图的问题。
- [x] Machine spaces page: online state, counts, busy projects with latest line, idle projects, loose agents, ⓘ details — Views/Chats/MachineSpacesView.swift (MachineSpacesView, BusySpaceRow)
  - Mac（2026-11-01 核对）截图（`-openMachinePage macbook`）：在线、项目和 Agent 数、状态计数、进行中的项目带最新一句、工具栏 ⓘ；体验数据里这台机器没有空闲项目和散的 Agent，那两块没看到。

## 4. Chat page (agent conversation)
- [ ] Message rendering: user bubble, assistant markdown (MarkdownUI `.jarvis` theme), thinking row, peer 「来自 名字」 card, pending/sending bubble (retry/delete), merged queued bubble, sent photos, attached images — Views/Chat/MessageViews.swift (UserBubble, PeerMessage, PendingBubble, MergedUserBubble, SentPhotos, AttachedImages, AssistantText, ThinkingRow, NoticeRow)
  - 要到 Mac 前看：别人发来的「来自 名字」卡片、合并的排队气泡、发出去的照片。假 hub 截图里用户气泡、回复 markdown、思考行、发送失败的气泡和它的菜单都对，这三种没有数据。
- [ ] Markdown tables fitted / horizontal scroll — Views/Chat/MarkdownTable.swift (FittedTable)
  - Mac（2026-11-01 核对）：截图里两张表都按宽度排开（fitted 对）；「宽表，横着滑」在 1280pt 窗口里也放下了，横向滚动没触发，没看到。
- [x] Line model: one turn → message / work line / answer; work line counts & duration; split merged messages — Views/Chat/ChatLines.swift, ChatView.swift (WorkLine, WorkStepView, WorkCardSlice)
  - Mac（2026-11-01 核对）截图：工作行带步数和时长，大会话的 763 步折成一行；ChatLines 是共用代码。
- [ ] Work trace expand/collapse in place (timeline style for glass, card style for others), 「收起 N 步」, pin tapped row under finger — ChatView.swift, Views/Chat/ChatPin.swift
  - 要到 Mac 前看：点工作行展开 / 收起，点的那一行留在指针下面（Mac 的 ChatPin）。`-openWork` 截图里玻璃的时间线样式、黏土的卡片样式和「收起 N 步」都对。
- [ ] Tool views: tool groups, tool call rows, detail, Bash command/output, full output sheet, TodoWrite list, base64 images, folded work row — Views/Chat/ToolViews.swift
  - 要到 Mac 前看：点开「完整输出」的 sheet（`-showFullOutput sheet` 没打开）。新开关 `-showToolDetails YES`（只在 Mac 的 DEBUG，已加进 release.sh 的列表）把每个工具行展开，截图里 TodoWrite、Bash 输出、搜索 / 读取详情、base64 图片、折叠行都对。
- [ ] Unified DiffView (Edit/MultiEdit/Write + Codex unified diff), +N −N, 40-line truncation, horizontal scroll — ToolViews.swift (DiffView), Views/Session/GitPane.swift (DiffText)
  - Mac（2026-11-01 核对）：截图里只看到 Edit 的标题（+6）和一行红色删除行，在画面底边被截掉，没看到绿色新增行；40 行截断、横向滚动、Codex 的 unified diff 都没跑到。
- [ ] BeautifulUI style: ThinkingState, tool-run chips (ToolRunRow/ToolChipRow) — ToolViews.swift, Views/BeautifulUIBridge.swift, Packages/BeautifulUI
  - 要到 Mac 前看：BeautifulUI 风格下最后一条回复被输入栏盖住一截（截图里看到，像是真 bug，要改 ChatView.swift），以及 ThinkingState。工具运行 chip 和输入栏截图里画出来了。
- [x] Wake (background task) chip + WakeDrawer; recap strip + RecapDrawer (folded multiples) — MessageViews.swift (WakeChip, RecapCard, RecapStrip, RecapDrawer)
  - Mac（2026-11-01 核对）截图：唤醒 chip 和 WakeDrawer（`-showWakeDrawer`，DELAY 要 11 秒左右）、回顾条和 RecapDrawer（`-showRecapDrawer`，「离开期间回顾 · 3 次」）都对。
- [x] Background task notices with outcome — Views/Chat/TaskNoteRow.swift
  - Mac（2026-11-01 核对）假 hub 截图：「✕ 退出码 1」和「✓ · 3 分」两种结果都有。
- [ ] API error card (human explanation, chips, details copyable, folded repeats, 重试) — Views/Chat/APIErrorCard.swift
  - Mac（2026-11-01 核对）：401 卡片截图看过（白话说明、401 / 要处理 chip、请求 ID 和原文、复制原文）；连续 502 折成「连续 3 次」带重试的那张没截到。
- [ ] Claude suggestion pill (填入 / 直接发送) — ChatView.swift (SuggestionPill)
  - 要到 Mac 前看：点建议是否填进输入框，右键 › 直接发送 是否发出。建议胶囊截图里画出来了。
- [ ] Inverted list, follow-end logic, frozen inserts while reading history, ↓ jump button, landing position (landUnderInput / saved position), turn-based pagination (turns=2) with auto-load at top — ChatView.swift (ChatStreamFollow), Shared/ChatStore.swift
  - 要到 Mac 前看：往上翻历史时 ↓ 按钮和 ⌘↓、翻到顶自动加载、加载时位置不跳。都要真滚动，离屏截图驱动不了。
- [ ] Local chat cache (last 2 turns per chat, 6 MB) + chats-v2 disk cache — Shared/StateCache.swift, Shared/ChatStore.swift
  - Mac（2026-11-01 核对）：「重启后缓存的内容又出现、`-openChatsFresh` 能清掉」只有口述，没留下截图或日志；每个对话 2 轮、6 MB 的上限没跑到，只是共用代码。
- [ ] Lazy images on scroll with memory + disk cache — Views/Chat/ChatImageLoader.swift (ChatThumbnail)
  - Mac（2026-11-01 核对）：只看到一张工具行里的图片解码显示；滚动时按需拉取、内存 / 磁盘缓存都没跑到。
- [ ] Collapsing self-drawn header (ChromeHeader: title, subtitle state·machine·space›tab, sibling row, loop/background chips), follows finger, auto-collapse after 3s idle — ChatView.swift
  - 要到 Mac 前看：用触控板 / 滚轮滚时标题跟着收起，点收起后的胶囊能展开。完整标题（标题、副标题、同空间标签、循环 / 后台 chip、⋯）和翻到最新时收成胶囊截图里都对。
- [x] Session info sheet (run / usage / location, copy id, copy resume command) — ChatView.swift (SessionInfoSheet)。Mac：对话右侧的检查器栏（⌥⌘I / 点标题），Jarvis 页是窗口 sheet
- [ ] ⋯ menu: 终端, 文件, Git (N), 空间里的所有 Agent, 重命名…, 屏蔽, 拷贝 session id, 拷贝恢复命令, 清空上下文 /clear, 关闭 Agent… — ChatView.swift
  - 要到 Mac 前看：点对话页的 ⋯ 逐项点。按钮截图里在，菜单离屏打不开；大部分项 Agent 菜单里也有（`-macMenuLog` 看过）。
- [ ] Swipe left chat → Files → Git; swipe-back gesture re-enabled — Views/Session/SessionPanes.swift (SessionPane, SessionPaneBar), Views/Chat/SwipeBack.swift
  - 要到 Mac 前看：从 ⋯ › 文件 / Git 进去再回到对话。Mac 上 SwipeTo 不做事，`-openPane files|git` 截图里两个面板都画出来了。
- [ ] Hide tab bar in single-column chat — Views/Chat/TabBarHiding.swift
  - Mac 上没有：Mac 没有 tab 栏，`hidesTabBarWhenShown` 在兼容层（`Mac/Compat/NavigationCompat.swift`）里什么都不做。
- [ ] Side-vertical toolbar (iPhone Duo) layout — ChatView.swift (sideBarItems, sideHeader)
  - Mac 上没有：这是 iPhone Duo 的布局，`VerticalBarsReader` 只在 iOS 编译。
- [ ] Live status strip (spinner line, output preview, → terminal) — SessionPanes.swift (ActivityChips, ActivitySheet)
  - 要到 Mac 前看：点对话页顶上的状态 chip 打开 ActivitySheet，看转圈行、输出预览和 → 终端。chip 截图里有，浮层要点开（加截图开关得改 ChatView.swift，这次没动）。
- [ ] Loop strip (interval, next wake, task, 停止循环 confirm) + loop tick rows — SessionPanes.swift (LoopStrip, LoopTickRow)
  - 要到 Mac 前看：点循环 chip，在 ActivitySheet 里看间隔、下次唤醒、任务和「停止循环」确认。chip 截图里有。
- [ ] Background tasks strip (N 个后台任务, open output file) — SessionPanes.swift (BackgroundTasksStrip)
  - 要到 Mac 前看：点后台 chip，在 ActivitySheet 里看「N 个后台任务」并打开输出文件。chip 截图里有。
- [ ] Subagents strip + read-only subagent view + footer 回到主 Agent — SessionPanes.swift (AgentStrip, SubagentFooter)
  - 要到 Mac 前看：找一个有子 Agent 的会话，看子 Agent 条、只读视图和底部「回到主 Agent」。假 hub 和体验模式都没有子 Agent 数据。
- [ ] Queue strip (撤回编辑 / 拷贝 / 删除, pull all back) — ChatView.swift (QueueStrip), AppModel.editQueued
  - 要到 Mac 前看：Agent 工作时连发几条，看排队条的撤回编辑 / 拷贝 / 删除和全部拉回。假 hub 没有排队消息的数据。
- [ ] Skeleton / starting note (「正在启动…」, 「没有启动起来」) — ChatView.swift (ChatSkeleton, StartingNote)
  - 要到 Mac 前看：打开一个还没加载过的对话、新起一个 Agent，看「正在启动…」和失败时的「没有启动起来」。本地假 hub 加载太快，`-openChatsFresh` 加 DELAY 2 也没截到。
- [ ] Long-press message action bar: 复制 / 选择 / 引用 / 分享 / 编辑 (own); select-text sheet — Views/Chat/MessageMenu.swift
  - 要到 Mac 前看：右键一条消息打开操作条，自己的消息上有「编辑」，「选择」打开选字 sheet。`-showMenu`（DELAY 11 秒左右）截图里操作条画出来了。
- [ ] Share card image (light/dark, save to Photos, share image/text) — Views/Chat/ShareCard.swift
  - 要到 Mac 前看：分享卡片里存到照片（会弹权限）和分享图片 / 文字。`-showShareCard` 截图里卡片画出来了。
- [ ] Image viewer (all chat images, paging, zoom, save/edit/share/copy) — Views/Chat/ImageViewer.swift
  - 要到 Mac 前看：←/→ 翻页、缩放、保存。`-openFirstImage` 截图里查看器和保存 / 编辑 / 分享 / 拷贝都在。
- [x] Image editor (box / pen, 6 colors, undo/clear, 加到输入框) — Views/Chat/ImageEditor.swift
  - Mac（2026-11-01 核对）：`-openEditor YES` 原来只在 iOS 有钩子，`ImageViewer.swift` 的 macOS 分支补上后截图看到框 / 笔、6 个颜色、撤销 / 清除、加到输入框。用鼠标画、把标记画进图片（macOS 的 NSImage 分支）还要到 Mac 前试。
- [ ] File path chips (verified via /stat), tool-row paths clickable — Views/Chat/FileViewer.swift (FileChips, LookUpFiles), Code/FilePresence.swift
  - Mac（2026-11-01 核对）：读取行的路径显示成链接；存在的路径变卡片、不存在的不变，在 §8 FilePresence 那张截图里看到，这一条自己的截图没拍到不存在的那一边，点卡片 / 链接也没试。
- [ ] Context reference cards in bubbles (`<jarvis-context>`) — Views/Link/LinkSheet.swift (ContextCards, ContextChips)
  - 要到 Mac 前看：在链接面板里选字「作为上下文」后发出，看气泡里的引用卡片。`-openSendMessage` 发了一条带 `<jarvis-context>` 的消息，但截图里没出现这条消息。
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
- [ ] Context chips above input (from web selection / doc / other session) — LinkSheet.swift (ContextChips)
  - 要到 Mac 前看：在 Mac 的链接面板里选一段字点「作为上下文」，输入框上方出 chip。要先选字才有，没截到。
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
- [ ] Terminal cover (bottom-up full page, drag to dismiss, size/handle/settings bar) — Views/Chat/ChatView.swift (TerminalCover), Views/Session/SessionPanes.swift (TerminalPanel)
  - 要到 Mac 前看：终端大 sheet 怎么关（Esc、拖手柄），拖动关闭能不能用或有没有替代。`-openTerminal YES` 截图里手柄、换行 chip、设置按钮、键栏都在。
- [ ] Live stream (terminal_stream, epoch/seq) or 1s ANSI polling fallback — Views/Session/Terminal/TerminalFeed.swift, SessionPanes.swift (TerminalFeedLink)
  - Mac（2026-11-01 核对）：流式这半截图看过（`MOCK_TERMINAL_STREAM=1`：颜色、真彩、反显、下划线）；普通 mock 下只看到一份静态的终端快照，看不出每秒轮询在刷新，回退这半没证实。
- [x] Grid model + cell renderer (wide chars, box drawing as shapes) — Terminal/TerminalGrid.swift, Terminal/TerminalView.swift (TerminalCanvas), Terminal/BoxDrawing.swift
  - Mac（2026-11-01 核对）流式截图：框线里的中文对齐、🚀 占两格、OSC 8 链接带下划线、200 字的长行折行。
- [ ] Phone-size takeover (terminal_control), 接管 / 只看 prompt — SessionPanes.swift, AppModel.terminalSubscribe
  - 要到 Mac 前看：用 `MOCK_TERMINAL_CONTROL=1`（提示用 `MOCK_TERMINAL_BUSY=1`）或真 herdr 打开终端，看窗格按窗口改尺寸、接管 / 只看能切。终端设置里「窗口尺寸 / 电脑尺寸」的说法截图看过。
- [ ] Wrap toggle, themes (Jarvis/Solarized/深夜/ink), font size, pinch zoom — Terminal/TerminalWrap.swift, Terminal/TerminalTheme.swift, TerminalView.swift
  - 要到 Mac 前看：切换行、换配色、⌘+ / ⌘- 和触控板捏合。换行 chip 和设置里的配色列表（带预览）截图里有。
- [ ] Selection (long-press word, drag, 拷贝 / 全选), OSC 8 / URL tap → in-app link — TerminalView.swift (SelectionHandle)
  - 要到 Mac 前看：拖选、双击选词、⌘C / 右键 拷贝 / 全选，点带下划线的 OSC 8 链接在 App 里打开。都要真鼠标。
- [ ] Scrollback loading 200 lines at a time (screen/before), 回到最新, ~1000-line limit notice — SessionPanes.swift
  - 要到 Mac 前看：滚轮 / 触控板往上滚，触发每次 200 行的加载、回到最新、约 1000 行的提示。
- [ ] Link status bar (last update, retry backoff, 重试) — SessionPanes.swift (TerminalLinkStatus)
  - 要到 Mac 前看：mock hub 加 `MOCK_SCREEN_FAIL_AFTER=3:14`，状态条出现（最后更新、退避、重试）后再恢复。体验模式和正常的 mock 都不出这条。
- [ ] Key bar: sticky/locked/held ctrl/alt/shift, esc, tab, ⌃C, D-pad, paste, shortcuts — Views/Chat/TerminalKeys.swift (TerminalKeyBar, KeyCap, DirectionPad)
  - 要到 Mac 前看：修饰键的一次 / 锁定 / 按住、方向键盘、粘贴，以及深色下文字键近乎黑底黑字是不是真的。键栏截图里画出来了。
- [ ] Input: field mode + direct typing mode (UIKeyInput / UITextInput, hardware arrow keys) — Terminal/TerminalInput.swift
  - 要到 Mac 前看：真键盘打字、拼音组字、方向键、⌃ 组合。Mac 走自己的 keyDown（映射有 `MacTerminalKeysTests`）。
- [ ] Gestures: 2-finger swipe sibling agent / adjacent space, double-tap paste (multi-line confirm), scroll in-terminal — TerminalView.swift
  - 要到 Mac 前看：触控板三指轻扫到同空间的 Agent、⌘V 粘贴（多行有确认）、终端里滚动。Mac 上的说法在 `TerminalHelpText`（有测试）。
- [ ] Shortcuts panel (single tap fill, double tap send), terminal settings (keys shown, auto-hide bar, theme, font, wrap), tips, completions from pane contents — Terminal/TerminalExtras.swift (ShortcutsPanel, TerminalSettingsView, TerminalPreview, ThemeRow, SuggestionStrip), SessionPanes.swift (TerminalTips)
  - 终端设置在 Mac 上是 grouped 表单（`macSheet`），「手机尺寸」相关的说法换成窗口（`TerminalSizeText`），截图看过（`-openTerminal YES -openTerminalSettings YES`，内层浮层存成 `<名字>.sheet2.png`）；快捷命令面板、补全没在 Mac 上试。
  - 终端设置 › 键盘 › 「将 Option 键用作 Meta 键」（2026-11-01，`TerminalPrefs.optionAsMeta`，只在 Mac 上有）：默认关，和「终端」App 一样，关着时 Option 照常打出 ™、é、德语布局的 @ [ ] 和走输入法。打开后 Option+可打印 ASCII 发 herdr 的 `alt+<键>`（herdr 编码成 ESC+键），Option+Shift+字母发 `alt+shift+<字母>`，符号的 Shift 已在字符里（`alt+>`）；组字时不管。映射是纯函数 `MacTerminalKeys.name`（有测试）。没拿真 herdr 试过 `alt+shift+a`（herdr 对带 shift 的组合可能编码成 CSI u，而「终端」App 发 ESC A）和 `alt++` 这类符号键名；真按键也没试（锁屏）。
  - 大 sheet（2026-11-01，`Mac/Design/MacLargeSheet.swift`）：终端、图片查看 / 编辑（Compat 的 fullScreenCover）、文件查看器、Git 的改动 / 提交在 Mac 上按主窗口内容区减两边 xxl 打开，最小 640×520；原来终端 640×520、文件查看器 470×280。坑：sheet 只认最小尺寸不认 ideal；固定 frame 设在 NavigationStack 上栈会变矮，要设在栈的内容上。fullScreenCover 统一按有标题栏和工具栏算（`macSheetBars` 116pt），终端也被多减了这一截（够大，没分开处理）。
  - 终端在 Mac 上的说法（`TerminalHelpText`，有测试）：上滑 → 往上滚动、左右滑 → 左右滚动；终端设置的「手势」在 Mac 上叫「操作」（点击、滚轮 / 触控板、拖选、⌘C / ⌘V、右键、⌘± / 捏合、三指轻扫），没有听写、长按、弹出键盘；「只看 · 接管」的图标是窗口。快捷命令在 Mac 上没有编辑模式，每条右键菜单里上移 / 下移 / 删除（`MacShortcutOrder`）。没修：深色截图里键栏的文字键几乎黑底黑字（⚡、粘贴是白的），分不清是不是截图的问题，要解锁看真窗口；链接状态条体验模式里不出现，没看。
  - 要到 Mac 前看：单击填入、双击发送、右键上移 / 下移、补全（2026-11-01）。`-openShortcutsPanel YES` 截图里面板和「编辑」画出来了（离屏是深底深字，同键栏）。

## 8. Files / Git / code viewer
- Mac（2026-11-01）：文件面板路径栏的 ⋯ 原来被 Mac 的弹出按钮挤成两个点，改成纯图标菜单；Git 的 diff / 提交 sheet 用大 sheet（`-macGitSheet diff|commit`，示例改成 `scripts/mock_hub.py` 的 `MOCK_FILES=1` 里有的改动和提交）。连 mock_hub 截到了有数据的列表、diff 和提交。
  文件 / Git 面板在屏幕上时工具栏有「刷新」按钮（`Mac/Chat/MacRefreshButton.swift`，复用各自的 load()），显示 › 刷新 ⌘R 也刷它们（见「显示 / 前往菜单」）。Git diff 最后一行的背景现在铺到边（`MacDiffTextTests`）。
  mock_hub 的 /file 里没有图片（`docs/icon.png` stat 失败），要截查看器的图片 / PDF 得先加一条假数据。
- [ ] Files pane: breadcrumbs, dirs first, size/mtime, git markers, hidden toggle, 拷贝路径, 2000 limit — Views/Session/FilesPane.swift
  - Mac（2026-11-01 核对）：mock hub（`MOCK_FILES=1`）`-openPane files` 截图：面包屑 + ⋯、文件夹在前、大小和修改时间、git 标记，工具栏有刷新按钮（对话面板上没有）。隐藏文件开关只看到 ⋯、没看到开关本身，拷贝路径、2000 条上限没看到。
- [ ] Git pane: branch, upstream ↑↓, stashes, changes (staged/unstaged switch), diff sheet, commit history paging, commit sheet, copy diff/path — Views/Session/GitPane.swift
  - Mac（2026-11-01 核对）mock hub 截图：分支、↑1、暂存、改动 +4 −1、带 ref 的历史；diff sheet 的未暂存 / 已暂存切换和真 diff，提交 sheet 的标题、正文、作者、hash、文件。↓、历史翻页、拷贝 diff / 路径的右键菜单没看到。
- [ ] FileViewer: code (syntax, line numbers, copy line/all), JSON outline (fold, copy value/path, expand/collapse all), Markdown rendered/source, images, PDF, directories, share — Views/Chat/FileViewer.swift
  - 要到 Mac 前看：图片和 PDF（mock 没有图片假数据）、分享、拷贝一行 / 全部、JSON 全部展开 / 收起和拷贝值 / 路径、Markdown 渲染 / 源码切换。代码（语法色、行号，深浅色）、JSON 大纲、渲染的 Markdown、目录截图看过。
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
- [ ] Prefilled new chat (from mail 「让 Claude 处理」) — Views/Mail/MailViews.swift (NewChatPrefilled)
  - 要到 Mac 前看：在收件箱的邮件上点工具栏的 ✦「让 Claude 处理」，滚到第一条消息看是否带着读这封邮件的提示，开始对话后新 Agent 在 Agents 里打开。新开关 `-showMailAskClaude YES`（配 `-tab mail -openInboxMatch <标题里的字>`，已加进 release.sh 的列表）截图里表单完整，第一条消息在 sheet 下面没看到。

## 11. Machines / monitoring
- [ ] 机器 (one page since 2026-11-01): N/M online, per-machine card with agents (需要你 / 工作中 / 空闲), CPU/mem/net now + 1h sparklines, alert rings, version-mismatch 重新部署, what needs a look first, 10s polling — Views/Machines/MachinesView.swift (MachinesView, MachineCard, MachineConcern, DeployRow)（Mac：侧边栏「机器」⌘5，`MacMachinesPage`）
  - 合并前的「机器」列表和「机器监控」总览在 Mac 上都核对过（2026-11-01）；合并后的页面 Mac 只编译通过，没截图。要到 Mac 前看：侧栏「机器」的卡片、排序、⌘R。
- [ ] Machine detail: agents (open the chat), 引擎, 1/6/24h CPU/mem/net/load charts with shared scrub, per-core bars, system facts, gaps, launchers, reconnect — Views/Machines/MachineDetailView.swift, Views/Machines/MetricCharts.swift (Swift Charts)
  - 原来的监控页在 Mac 上看过 1 / 24 小时、告警卡片、CPU 和内存图（`-macSection machines -openMonitor <id>`，现在打开的是合并后的机器页）。要到 Mac 前看：往下滚看网络、负载图、每核条、系统信息、断档，用鼠标拖图看两张图一起动，点一个 Agent 进对话。
- [x] OS badge — Views/Machines/OSBadge.swift; model — Shared/MachineMetrics.swift
  - Mac（2026-11-01 核对）：总览卡片上 Ubuntu、两台 Apple、Debian 的标志，详情和监控页头部也有；`MachineMetricsTests` 在 JarvisMacTests 里通过。

## 12. Knowledge base
- [x] Kind pills (项目/会话/人/文档/会议/日程 with counts), tag filter, project sections (重点/其他进行中/已完成/不是项目) — Views/Knowledge/KnowledgeView.swift (KnowledgeView, KBKindBar, KBRow, KBAvatar, ImportanceBadge, KBPill)
- [x] Full-text search, merged per entry, highlighted hits — KnowledgeView.swift (KBHitRow)
  - Mac（2026-11-01 核对）截图（`-showKnowledgeSearch 小组件`）：按 项目 1 / 会话 1 / 人 1 分组，一条一行带命中数（4 处、6 处），每行命中里「小组件」都加粗。
- [ ] Detail: header, 问 Jarvis / 开 Agent, agents, timeline, profile, relations, code dirs, last 7 days messages, path; search matches — KnowledgeView.swift (KnowledgeDetailView, KBMatchRow, KBTimelineRow, KBLinkRow, KBMessageText)
  - Mac（2026-11-01 核对）：截图里 P0 / 短期项目 / 相关 92、标签、开 Agent、在做的 Agent、时间线、档案、相关removed、搜索匹配行、目录路径都对；「问 Jarvis」按钮的位置是空白（白字加醒目玻璃底，离屏画不出），没看到，要到 Mac 前看一眼。
- [ ] Day view of chat messages with prev/next — KnowledgeView.swift (KBDayView)
  - 要到 Mac 前看：点底部「‹ 前一天」，前一天加载出来、再能翻回来。`-showKnowledgeDay YES` 截图里当天 8 条消息、命中的那条高亮、前一天链接都对。
- [ ] 打标 editor (importance, relevance, tags, project type/status) — KnowledgeView.swift (KBTagEditor)
  - 要到 Mac 前看：打开一个项目等详情加载完，点工具栏的打标按钮，看重要级 / 相关度 / 标签 / 类型状态的预填对不对，改一项保存后头部更新。`-showKBEditor` 在详情加载前就弹 sheet，预填的是占位条目（P2 / 0），只是 DEBUG 的先后问题。
- [x] Models — Shared/Knowledge.swift
  - Mac（2026-11-01 核对）：`KnowledgeTests` 编进 JarvisMacTests 通过；列表、详情、搜索的响应在 Mac 上解出来并画出来了（截图）。
  Mac（2026-11-01 截图）：侧栏「知识库」整页，类型胶囊、项目分组、人列表、条目详情（问 Jarvis / 开 Agent、档案、目录）都对；体验模式的搜索固定回空（DemoHub），搜索命中、日视图还没在 Mac 上看过；打标 sheet 截图看过（见上面「表单」那段的 `-showKBEditor`）。
  搜索命中、条目里的匹配、日视图后来补看了（DEBUG 的编造数据 `App/DemoKBSample.swift`：`-macSection knowledge -showKnowledgeSearch 小组件`，加 `-showKnowledgeEntry chats/weather-team`，再加 `-showKnowledgeDay YES`）；日视图在 Mac 上日期显示了两次，改成标题是日期、副标题是会话名。按天翻页要点击，没试。

## 13. Inbox (收件箱)
- [x] Unified list (Feishu + mail + docs + waiting agents), filters (全部/飞书/each mailbox | @我/私聊/群聊/邮件/Agent), search, sections 需要你 / 知悉即可 (全部看过) / 已折叠 (全部清除) / 已处理, swipe = done, pull refresh — Views/Inbox/InboxView.swift (InboxView, InboxRow, InboxAgentRow)
- [x] AI draft card (发送 / 修改), 「AI 分身正在处理」 — InboxView.swift (InboxDraftCard)
- [x] Detail: Lark chat at first new message or mail; 已处理; action bar (摘要, AI 草拟回复, 交给 AI) — InboxView.swift (InboxDetailView), Views/Inbox/InboxActions.swift (InboxActionBar)
- [x] Draft sheet (edit, 让 AI 改, 重写, doc status changes) — InboxActions.swift (InboxDraftSheet)
- [x] Hand-off sheet (Jarvis new chat / one of 8 agents, presets, mark done) — InboxActions.swift (InboxHandOffSheet)
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
- [x] Reaction picker, removed (Moments) card + post view — Views/Inbox/Lark/LarkReactionsMoments.swift
- [x] Translation line under messages — LarkChatView.swift (LarkTranslationLine)
- [x] 「让 Jarvis 总结」 action — LarkChatView.swift
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
  - Mac（2026-11-01 核对）假 hub 截图（`-openModelLaunchers YES`）：Jarvis 管理（空的提示）、来自 ~/.zshrc 3 个（模型 chip、网关、机器）、已隐藏 ›、工具栏 + 和搜索；`-showModelSearch <词>` 筛到只剩一个。
- [ ] Detail: per-machine params, function source (masked), 测试可用性, delete/hide with popover confirm, restore — ModelLaunchersView.swift (ModelLauncherDetailView, LauncherDetailSections, LauncherCheckRow)
  - 要到 Mac 前看：在真启动器上看函数源码一节（key 打码），点「在 Jarvis 里隐藏」（Jarvis 管理的点删除）看确认浮层，再从已隐藏里恢复显示。来源 / Base URL / 模型 / 电脑、可用性检测和再测一次、恢复显示截图看过。
- [x] Editor (name, base URL, API key, model, small model, args) — ModelLaunchersView.swift (LauncherEditor)
  - Mac（2026-11-01 核对）sheet 截图（`-macLauncherEditor YES`）：名字、网关 + API Key、模型 + 小模型、额外参数和说明、取消和置灰的保存。
- [ ] Quota section (pools/windows, colors ≥70/90%, refresh) — Views/Settings/LauncherQuotaViews.swift
  - 要到 Mac 前看：在激活的窗口里，77% / 86% 的条是橙色、用完的是红色、42% 是默认色，点刷新重新加载。截图里额度池、窗口、数字和红 / 橙文字都对，条全是灰的（离屏窗口不是 key，多半是截图问题，没确认）。
- [x] Config models — Shared/LauncherConfig.swift
  - Mac（2026-11-01 核对）：假 hub 的列表、详情、额度响应在 Mac 上解成这些模型并画出来（截图）；这个文件两边都没有单独的单元测试。

## 17. Personas & styles
- [x] Persona picker / gallery (follow style colors), persona store (download, cache by rev, APNG frames) — Views/Settings/PersonaSettingsView.swift, Persona/PersonaStore.swift, Persona/Persona.swift, Persona/PersonaViews.swift (PersonaAvatar, PersonaDot, PersonaFramesView)
  - Mac（2026-11-01 核对）假 hub（`MOCK_PERSONAS=demo`）截图：形象库里 Jarvis（跟随界面风格）和下载的形象包、两块配色、包里的 5 个状态帧；选中后强调色（用户气泡、工具栏图标）换成形象的颜色，顶栏头像和名字跟着换。
- [ ] Persona dot states idle/thinking/working/needs_you/offline, reduce-motion = frame 0 — PersonaViews.swift, AssistantTopBar.swift
  - 要到 Mac 前看：系统设置 › 辅助功能 › 显示 › 减弱动态效果 打开后，Jarvis 顶栏的形象停在第 0 帧。外观页的 5 个状态截图看过，顶栏的等你处理（红圈）和思考中（…）也截到了（`-showPersonaState`）。
- [x] UI styles 液态玻璃 / Claude / 黏土 / Island / 飞书 / BeautifulUI with preview cards — Views/Settings/SettingsView.swift (StylePicker, StyleCard), Shared/Design/StyleSpec.swift, Shared/Design/Styles/*.swift
  - Mac 截图看过（`-tab settings -openAppearance style`）：6 张风格卡片都画出来，选中的带勾；液态玻璃的说明在 Mac 上是「系统原生，简洁」（`Styles/Glass.swift`）。各风格整套界面在 Mac 上没逐个截。
- [x] Text size 小/标准/大 over Dynamic Type — DesignSystem/TextSize.swift
  - 外观页的 小 / 标准 / 大 截图看过；显示 › 放大 / 缩小 / 标准字号 ⌘+ ⌘- ⌘0 改的是同一个值（有测试）。
- [x] Theme tokens, icons, palettes, status palette — Shared/Design/Theme.swift, Icons.swift, Palette.swift, PersonaPalette.swift, Shared/StatusPalette.swift
  - Mac（2026-11-01 核对）：`Shared/Design` 原样编进 JarvisMac，这一轮所有截图都按它画（状态色、告警 chip、形象着色）；扫源码的 token 测试只在 iOS 的 JarvisTests 里，这次没跑。
- [ ] Components (StatusDot, Chip, CountBadge, EmptyStateView, Monogram…) and surfaces/buttons — DesignSystem/Components.swift, DesignSystem/Styling.swift
  - Mac（2026-11-01 核对）：截图里 Chip、CountBadge、Monogram、themedButton、卡片表面都看到；EmptyStateView 引的那张截图其实开着一封邮件、不是空状态，没看到，StatusDot 也没有对应 Agents 列表的截图。

## 18. Notifications & notification actions
- [ ] Request permission + APNs registration, POST /devices — App/JarvisApp.swift (AppDelegate.registerIfPossible)
  - Mac 上没有：JarvisMac 没有推送权限、Mac 没有 APNs，不向 hub 登记设备 token；对应的是 `MacAttentionWatcher` 的本地通知，第一次要发时才申请权限。
- [x] Categories INTERACTION_PERMISSION (允许 auth-required / 拒绝) and INTERACTION_PLAN (批准 / 打回修改 text input) — App/NotificationActions.swift
- [ ] Background resolve from action, failure local notification — App/JarvisApp.swift (resolveFromNotification)
  - 要到 Mac 前看：有 Agent 等授权时，悬停系统通知 › 选项 › 允许，hub 处理掉、窗口不出来；hub 断开时再来一次，应出一条失败通知。读代码：`MacAppDelegate` 走和 iOS 一样的 `NotificationActions.resolution` / `reportFailure`，`NotificationActionsTests` 在 Mac 上通过。
- [x] Tap opens session (main-queue completion handler), suppress while viewing — JarvisApp.swift（Mac：`MacAppDelegate`，本地通知见「菜单栏、Dock 角标、通知」）
- [ ] Silent push handling (wake / disconnect) — JarvisApp.swift
  - Mac 上没有：没有 APNs 就没有静默推送；App 开着时 WebSocket 一直连着，`MacAttentionWatcher` 按会话列表发本地通知。
- [x] Settings: 需要你处理时, Agent 做完时 (不推送/只推长任务/每次都推), per-device prefs on hub, system-disabled warning + open settings — Views/Settings/NotificationSettingsView.swift
  - Mac（2026-11-01 核对）：截图看到「通知没有打开」+ 系统通知设置、需要你处理时、Agent 做完时三选一；在假 hub 上改这台设备的设置（PUT …/prefs）后页面跟着变，`NotifyPrefsTests` 在 Mac 上通过。在页面上点选写回 hub 没试。

## 19. Live Activity
- [x] Fleet activity (needsYou/working/done), push-to-start + push token upload, start/update/end (1 min grace), stale handling — App/LiveActivityController.swift, LiveActivity/JarvisActivityAttributes.swift, Shared/FleetState.swift（Mac：菜单栏 `MacMenuBar`，从会话列表算，不用 fleet 推送）
- [ ] Lock screen + Dynamic Island views, watch smart stack — JarvisWidgets/FleetLiveActivity.swift, LiveActivity/FleetIslandViews.swift
  - Mac 上没有：macOS 没有 ActivityKit、锁屏实时活动、灵动岛和手表智能叠放；对应的是菜单栏图标（上面 Fleet 那条）。
- [x] Buttons: Allow/Deny permission, Approve plan (LiveActivityIntent run in app) — LiveActivity/FleetIntents.swift, App/FleetActions.swift（Mac：菜单栏的子菜单）
- [ ] Island settings (presets A/B/C/custom, load machine, previews), lock details toggle — Views/Settings/IslandSettingsView.swift, NotificationSettingsView.swift
  - Mac 上没有：通知页的「实时活动」开关和灵动岛入口只在 iOS 编译，Mac 的通知页没有这一节（截图和普通通知页一样）。

## 20. Siri / App Intents
- [ ] 问 Claude (machine param, new session, returns OpenSession) — Intents/AskClaudeIntent.swift
  - 要到 Mac 前看：在 Mac 的「快捷指令」App 里跑「问 Claude」，填一句、选机器，那台机器上开了新会话。`IntentsTests`（机器优先用记住的、退到在线的；发出去的说法）在 Mac 上通过。
- [ ] Jarvis 状态 — Intents/JarvisStatusIntent.swift
  - 要到 Mac 前看：在快捷指令里跑「Jarvis 状态」，读出来的数量和侧栏一致。对应的 `IntentsTests` 在 Mac 上通过。
- [ ] 批准第一个待处理 — Intents/ApproveFirstIntent.swift
  - 要到 Mac 前看：有授权或计划等着时在快捷指令里跑「批准第一个待处理」，等得最久的那个被批准，回复说清做了什么。对应的 `IntentsTests` 在 Mac 上通过。
- [ ] 打开会话 (foreground) + SessionEntity — Intents/OpenSessionIntent.swift
  - 要到 Mac 前看：侧栏选着「机器」时从快捷指令跑「打开会话」，Jarvis 到前台、Agents 里打开那个对话。对应的 `IntentsTests` 在 Mac 上通过。
- [ ] MachineEntity query — Intents/MachineEntity.swift
  - 要到 Mac 前看：在快捷指令里编辑「问 Claude」的机器参数，列表里是你的各台电脑并标在线 / 离线。查询要问 hub，得在快捷指令的选择器里看。
- [x] Shortcuts phrases — Intents/JarvisShortcuts.swift; texts / picker / errors — Intents/IntentSupport.swift
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
  - Mac（2026-11-01 核对）：欢迎页改成 Mac 的说法：链接图标、「在 hub 所在的机器上运行 `jarvisd hub pair`，拷贝它打印的链接」（截图 x-welcome2），`jarvisd hub pair` 现在会打印 `jarvis://pair` 链接（`TestPairPrintsTheLinkForTheMac`）；「连接 hub」sheet（新开关 `-openPairSheet YES`，配 `-openWelcome YES`）是粘贴配对链接 + 手动两栏。要到 Mac 前看：真配对一次：粘贴链接或 ⌘V、手动填地址和 token、从浏览器打开 `jarvis://pair` 链接。
- [ ] Keychain storage — Shared/Keychain.swift
  - 要到 Mac 前看：用真链接配对，退出后不带 `-pairURL` 重开，看是否还是已配对（弹钥匙串授权就允许）。DEBUG 的截图和测试不读钥匙串（锁屏时读别的构建写的条目会卡住），离屏没碰过钥匙串。
- [ ] Device token exchange, 设置 › 设备 list + revoke — App/DeviceCredentials.swift (DevicesView)
  - Mac（2026-11-01 核对）：设备列表、本机 / 只能回答 chip、撤销、已撤销分组截图看过（假 hub）；Mac 原来按 kind "phone" 换凭据、被列成 iPhone，现在要 "desktop"（`MacDeviceCredentialTests`），hub 存成 "other"。要到 Mac 前看：真配对一次，设置 › 设备 里这台 Mac 显示电脑图标、撤销会确认（`-pairURL` 启动不换凭据，截图里看不到）。
- [ ] 重新配对, 断开并忘记服务器 — SettingsView.swift
  - 要到 Mac 前看：点「重新配对」看 sheet 弹出；点「断开并忘记服务器」确认后回到欢迎页。两个按钮在设置 › 服务器 的截图里。
- [x] Sync to Apple Watch (WCSession) — Shared/WatchSync.swift, SettingsView.swift; watch app — JarvisWatch/JarvisWatchApp.swift (macOS: n/a)
  - Mac（2026-11-01 核对）：Mac 不和手表配对，`WatchSync` 是空的；设置里原来还显示「同步到 Apple Watch」、点了提示已发送，现在只在 iOS 显示（深浅色截图看过）。

## 24. Offline / outbox / connection
- [x] Connection status waiting/connecting/updating (NWPathMonitor, ping 5s, debounce 1.5s), title replacement, details popover (立即重试) — App/ConnectionStatus.swift, Views/ConnectionTitle.swift
  - Mac（2026-11-01 核对）：假 hub 真断开时，列表标题、对话标题 chip、侧栏底部都是「连接中…」；`-showConnection` 强制的连接中 / 更新中同样三处，`MacConnectionCopyTests` 查 Mac 的说法。要到 Mac 前看：点「连接中…」标题，看详情浮层的文字和「立即重试」（离屏没画出来）。
- [ ] WebSocket with backoff + since_seq resubscribe — Shared/LiveConnection.swift
  - Mac（2026-11-01 核对）：假 hub 断开再恢复后截图里 App 回到在线、没有横幅；hub 日志里 `since=` 重订阅那一行只有口述，日志没留下、复现不了，`LiveConnection` 只是原样共用。
- [x] Outbox: offline messages persisted, auto-send in order after catch-up — App/Outbox.swift
  - Mac（2026-11-01 核对）：假 hub 断开时发一条，显示待发气泡「连上后自动发送」，退出、hub 恢复后再开，气泡变成正常发出（两张截图）。为此修了 `-pairURL` 启动不恢复 outbox（截图不读钥匙串，init 里没有配置），`MacLaunchArguments` 现在调 `restoreOutbox()`，只影响 DEBUG；hub 只收到一次是口述，没留证据。
- [ ] One-shot actions not queued; blue 「刚才的操作没有生效」 — AppModel.show / resolve
  - 要到 Mac 前看：假 hub `/mock/down?s=20` 时点授权卡的「允许」，出蓝色「刚才的操作没有生效」、不进队列。
- [ ] Background delta sync for agents (rev/after_seq, 40 sessions, concurrency 3) — App/AgentSync.swift
  - Mac（2026-11-01 核对）：只读了代码：`App/AgentSync.swift` 没有 `#if`、编进 JarvisMac、没被替换；Mac 上没量过，没有运行证据。
- [ ] State cache for machines/sessions/interactions/inbox/mail accounts/lark chats — Shared/StateCache.swift
  - Mac（2026-11-01 核对）：共用代码，上面 outbox 跨重启读回用到了它；Mac 写了哪些文件只是口述，没列出来。没签名的 DEBUG 构建没有沙盒，写的是所有这类构建共用的 `~/Library/Caches/state/`，并行的 worktree 会互相覆盖（这次 outbox.json 就被另一个 Agent 的运行覆盖过）；签了名的有沙盒。

## 25. Search
- [ ] Agents search — ChatsView.swift (searchable, SearchCloseProbe)
  - 要到 Mac 前看：⌘F 或点搜索框，输一个会话标题看列表筛选，✕ 能关掉。搜索框在每张 Agents 截图的工具栏里，没有预填的 DEBUG 开关。
- [ ] Assistant sidebar search — AssistantSidebar.swift
  - 要到 Mac 前看：在 Jarvis 侧栏的「搜索对话」里输字看对话筛选。`-openAssistantSidebar YES` 那张截图侧栏是空白（截图问题）。
- [x] Inbox search — InboxView.swift
- [ ] Knowledge search — KnowledgeView.swift
  - Mac（2026-11-01 核对）截图（`-showKnowledgeSearch 发布`）按 项目 / 会话 / 人 分组对；高亮只在「人」那条里看到加粗，项目和会话露出的片段里没有这个词（§12 用「小组件」搜时每条都加粗了）。
- [ ] Launcher search, directory search, translation chat picker search — ModelLaunchersView.swift, NewChatView.swift, TranslationSettingsView.swift
  - 要到 Mac 前看：⌘N 里在目录搜索输字；设置 › 翻译 › 飞书会话 里搜索会话。启动器搜索截图看过（`-showModelSearch <词>`）。

## 26. Settings pages
- [x] Root: header (demo note), 形象, 界面风格 + 字号, 助手, 模型, Claude Code 设置, 机器, 设备, 手机数据, 通知, 翻译, 存储空间, 服务器, 关于, 同步到 Apple Watch — Views/Settings/SettingsView.swift (SettingsDestinations, SettingsPage)
  - Mac（2026-11-01 核对）深浅色截图（假 hub）：hub 地址和已连接、形象、界面风格、模型、助手、Claude Code 设置、连接器、机器、设备、通知、翻译、存储空间、服务器、关于；手机数据按设计不在 Mac 上，「同步到 Apple Watch」原来还显示，现在只在 iOS 显示。
- [ ] 助手 → profile page; 定时任务 (upcoming timeline, daily/interval/once rows, toggle, swipe delete, long-press run now, templates, editor with model/permission/effort, bypass confirm, continue conversation, history) — Views/Settings/AssistantProactiveViews.swift (AssistantRoutinesView, AssistantTaskRow, AssistantTaskEditor, AssistantWeekdayPicker), Views/Settings/AssistantRoutinesModel.swift
  - 要到 Mac 前看：右键定时任务 › 立即运行 / 删除、勾选开关、保存一次编辑、选「绕过权限」看确认框、从运行记录继续对话、打开历史。主页、定时任务时间线、编辑页、新建 sheet 截图看过；一次性跟进的页脚在 Mac 上改成「右键可以立即运行或删除」。
- [ ] 记忆 (frontmatter, type tags, markdown detail, edit, swipe delete, MEMORY.md read-only) — AssistantProactiveViews.swift (AssistantMemoryView, AssistantMemoryDetail, AssistantMemoryEditor)
  - Mac（2026-11-01 核对）：列表（类型标签、只读的「索引」行和 MEMORY.md 页脚）、详情、编辑 sheet 截图看过；左滑删除要触控板，Mac 上加了右键「删除」走同一个确认框，但右键菜单没截到。要到 Mac 前看：右键一条记忆 › 删除 › 确认，触控板左滑也还能用。
- [ ] 关于我 notes — AssistantProactiveViews.swift (AssistantNotesView)
  - 要到 Mac 前看：改一段文字保存，重开看是否保留。页面（`-openAssistantPage about`）截图看过，保存要打字和点击。
- [ ] Claude Code 设置 (settings.json whitelist keys, apply to all machines) — SettingsView.swift (ClaudeSettingsView)
  - 要到 Mac 前看：设置 › Claude Code 设置 改一个白名单里的键，应用到所有机器，看结果。设置根页面上有这一行，没有 DEBUG 开关能直接打开。
- [x] 翻译 (auto translate, mail, chat picker) + Translator (NLLanguageRecognizer, batching) — Views/Settings/TranslationSettingsView.swift, App/Translator.swift
  - Mac（2026-11-01 核对）截图（`-translate.enabled YES -translate.mail YES`）：自动翻译开、目标语言、邮件勾上、飞书会话选择行；假 hub 收件箱里英文邮件显示成中文标题带翻译图标，Mac 上语言识别和翻译跑过。
- [ ] 存储空间 (per category sizes, clear one / all, keep web logins) — Views/Settings/StorageView.swift
  - Mac（2026-11-01）：本机 ad-hoc 签名的调试版没有沙盒，`URL.cachesDirectory` 是所有 App 共用的 `~/Library/Caches`、`libraryDirectory` 是整个 `~/Library`：页面一直「计算中…」（在遍历整个 ~/Library），而「清除全部缓存」会把别的 App 的缓存当「其他缓存」删掉。改成没沙盒时只认 Jarvis 自己写的文件夹（`StorageUsage.owned`，有测试）、临时目录只认 `lark-`、不数 Library 和 Documents；有沙盒（iOS、签了名的 Mac 版）时和原来一样。修后截图算出 127 KB；清除没在 Mac 上点过。
    review（2026-11-01 07:20）后又改两处：缓存目录认 `chats` / `chats-v<数字>`（`ChatCache.isFolder`）（对话缓存现在是 `chats-v3`，原来的名单漏了它）；没沙盒时临时目录一个也不碰（`$TMPDIR` 是整个用户共用的，别的程序也可能写 `lark-` 开头的文件夹）。
    已改（2026-11-01，iOS 一起）：对话缓存的目录名只在 `ChatCache.folder` 一处定义，`StorageUsage` 判断归属和分类都用 `ChatCache.isFolder`，`chats-v3` 和以后的版本都算「Agent 对话」（以前落进「其他缓存」，iOS 也一样）；`chats-vx`、`chats-v3.bak` 这类不算。有 Mac 和 iOS 两边的测试。
  - 要到 Mac 前看：清除一类、再清除全部，看大小变小、网页登录还在（2026-11-01）。截图看到总量、比例条、各类大小和「清除全部缓存」。
- [x] 通知 / 灵动岛 — see §18, §19
  - Mac（2026-11-01 核对）：通知页截图看过（Mac 的说法「把鼠标移到通知上，点『选项』」）；灵动岛部分 Mac 上没有，见 §19。页面上出现两个一样的「系统通知设置」按钮，没查。



## UIKit dependencies (`Jarvis/App` and `Shared`)

| File | What it uses |
| --- | --- |
| `Jarvis/App/AppModel.swift` | `UIImpactFeedbackGenerator`, `UINotificationFeedbackGenerator` (haptics), `UIImage` (`composerInbox`), `UIDevice.current` |
| `Jarvis/App/JarvisApp.swift` | `UIApplicationDelegate` / `UIApplicationDelegateAdaptor`, `UIApplication.shared` (`registerForRemoteNotifications`, `applicationState`, background task id), `UIBackgroundFetchResult` (silent push), `didEnterBackgroundNotification`, `UIDevice.current` |
| `Jarvis/App/LiveActivityController.swift` | `UIApplication.shared` (foreground state) |
| `Jarvis/App/PerfSignpost.swift` | `UIView`, `UIViewRepresentable`, `UITraitCollection` (debug probe) |
| `Jarvis/App/DeviceCredentials.swift` | `UIDevice.current` (device name) |
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
| ActivityKit | `App/LiveActivityController.swift`, `Views/Settings/NotificationSettingsView.swift`, `LiveActivity/JarvisActivityAttributes.swift`, `JarvisWidgets/FleetLiveActivity.swift` (WidgetKit too) |
| WatchConnectivity | `Shared/WatchSync.swift`, `App/DeviceCredentials.swift` |
| PhotosUI (`PhotosPicker`) | `Views/Chat/ChatView.swift`, `Views/Assistant/AssistantView.swift` |
| Photos | `PhoneTools/PhotosTool.swift`, `PhoneAccess.swift`, `PhoneToolRegistry.swift`, `Views/Chat/ImageViewer.swift` (save), `Views/Chat/ShareCard.swift` (save) |
| AVFoundation | `Views/Chat/PromptComposer.swift` (`AVAudioEngine`/`AVAudioSession` for dictation), `PhoneTools/PhoneToolRegistry.swift`. The camera is `UIImagePickerController`, not AVFoundation. |
| QR scanning | VisionKit `DataScannerViewController` in `Views/Settings/SettingsView.swift`, not AVFoundation |
| Speech | `Views/Chat/PromptComposer.swift` |
| CoreLocation | `PhoneTools/LocationTool.swift`, `PhoneAccess.swift`, `PhoneToolRegistry.swift` |
| Contacts | `PhoneTools/ContactsTool.swift`, `PhoneAccess.swift`, `PhoneToolRegistry.swift` |
| EventKit | `PhoneTools/CalendarTool.swift`, `RemindersTool.swift`, `PhoneAccess.swift`, `PhoneToolRegistry.swift` |
| AppIntents | `Jarvis/Intents/*.swift` (7 files), `LiveActivity/FleetIntents.swift` |
| UserNotifications | `App/AppModel.swift`, `App/FleetActions.swift`, `App/JarvisApp.swift`, `App/NotificationActions.swift`, `PhoneTools/NotificationsTool.swift`, `PhoneAccess.swift`, `PhoneToolRegistry.swift`, `Views/Settings/NotificationSettingsView.swift` |
| WebKit | `Views/Link/LinkSheet.swift`, `Views/Mail/MailViews.swift`, `Views/Settings/StorageView.swift` |
| Other | QuickLook and AVKit in `Views/Inbox/Lark/LarkMedia.swift`; PDFKit in `Views/Chat/FileViewer.swift`; Charts in `Lark/LarkCard.swift` and `Machines/MetricCharts.swift`; NaturalLanguage in `App/Translator.swift`; Network (`NWPathMonitor`) in `App/ConnectionStatus.swift` and `PhoneTools/DeviceTool.swift`; ImageIO in `Persona/PersonaStore.swift` and `LarkMedia.swift`; AuthenticationServices via the SwiftUI `webAuthenticationSession` for Google sign-in in `Views/Mail/MailViews.swift`; UniformTypeIdentifiers in `Chat/FileAttach.swift` |

Most of these also exist on macOS. The ones that don't have a direct Mac equivalent:
- ActivityKit and Live Activities.
- WatchConnectivity.
- HealthKit as a data source. It's iOS-first, so check what macOS actually supports before relying on it.
- `UIImagePickerController` and the VisionKit data scanner.
- `UIKeyInput`/`UITextInput` terminal input (needs an `NSTextInputClient` port).
- Notification `.authenticationRequired` actions and other lock-screen-specific behavior.