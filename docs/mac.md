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
  - **机器**：机器监控总览 + 每台机器。
  - 底部：连接状态（等待网络 / 连接中…，点开是连接详情）。
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
| 编辑 | 系统的拷贝 / 粘贴 / 全选 / 查找；拷贝会话 ID、拷贝恢复命令 |
| 显示 | 显示 / 隐藏侧边栏 ⌃⌘S、显示 Inspector ⌥⌘I、紧凑列表、分组方式（项目 / 状态 / 机器）、全部折叠 / 展开、字号放大缩小 ⌘+ ⌘- ⌘0 |
| Agent | 发送 ⏎（输入框里，⇧⏎ 换行）、中断 ⌘.、批准当前待处理 ⌘⏎、拒绝 ⌘⌫、回答选项 1–9、重命名、屏蔽、置顶、标为已读、清空上下文、关闭 Agent… |
| 前往 | Jarvis ⌘1 … 机器 ⌘5（侧边栏获焦时）、上一个 / 下一个 Agent ⌘[ ⌘]、下一个需要你的 ⌘⇧N、快速打开 ⌘K、回到最新 ⌘↓ |
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
| 手机数据（phone_*） | 同一套工具，用 Mac 的通讯录、日历、提醒、照片、位置、剪贴板；健康数据 Mac 上没有 |
| 拍照、扫码配对 | 拖入 / 粘贴图片；配对用粘贴 `jarvis://pair` 链接或在浏览器里点链接 |
| 终端键栏（Ctrl、Esc、方向键） | 真键盘直接打字，键栏不显示；终端获焦时所有键（含 ⌃ 组合）交给终端，⌘ 组合留给 App |
| Siri / 快捷指令 | App Intents 同样提供（Mac 的快捷指令 App、Spotlight） |

### 细节

- 输入框：⏎ 发送、⇧⏎ 换行；拖文件 / 图片进窗口就附上（图片进输入框，文件上传到那台机器）；粘贴图片同样。
- 拖放：列表里的 Agent 可以拖到侧边栏的「置顶」；文件面板里的文件可以拖出到 Finder（下载）。
- 窗口和列宽、侧边栏开合、选中的项目、每个对话的阅读位置都跨启动记住。
- 外观：跟随系统深浅色和强调色；界面风格（液态玻璃等）照样生效，Mac 上的玻璃用系统材质；不写死尺寸，跟随「设置 › 字号」。
- 性能：列表和对话都要在 120 Hz 屏上滚动不掉帧（`List` / `LazyVStack` 懒加载、行的 body 便宜、文字排版缓存），
  打开对话首屏 < 200 ms（有缓存时），窗口拖宽时不卡。用 Instruments（SwiftUI / Animation Hitches）量，量法写进本文。

## 构建、运行、截图

```sh
cd apple
python3 ../scripts/jarvis_config.py xcconfig && xcodegen generate
eval "$(python3 ../scripts/jarvis_config.py env)"
xcodebuild build -project Jarvis.xcodeproj -scheme JarvisMac -configuration Debug -destination "platform=macOS" \
  -derivedDataPath ../../build/DerivedDataMac CODE_SIGNING_ALLOWED=NO
open ../../build/DerivedDataMac/Build/Products/Debug/Jarvis.app   # 或直接跑 Contents/MacOS/Jarvis 带启动参数
```

- 启动参数和 iOS 一样（`MacLaunchArguments`，只在 DEBUG）：`-pairURL`、`-tab dev|mail|settings|assistant`、`-openSession <machine>/<session>`、
  `-openAssistant`、`-newAssistant`、`-showNewChat`、`-openChatsFresh`、`-openAgentsFresh`、`-lark-connector`、`-simulateTap`；体验模式 `-demo.on YES`。
- 截图（**锁屏也能拍**）：`scripts/mac_shot.sh <名字> [启动参数…]`，浅色、深色各一张写到 `/tmp/jarvis-mac-shots/<名字>-{light,dark}.png`。
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

## Mac 版现在的样子（阶段 0：能编、能跑）

- 壳（`Mac/App/`）：窗口左边一列四项（Jarvis / Agents / 收件箱 / 设置，角标同 iOS 的 tab 栏），右边是 iOS 的页面本身；
  `horizontalSizeClass` 在 Mac 上恒为 `.regular`，所以 Agents、收件箱、设置、Jarvis 走的是 iPad / iPhone Duo 打开时的分栏布局。
  外层不用 `NavigationSplitView`：页面自己是分栏，分栏套在另一个分栏的详情栏里时，里面的详情栏按外层侧栏还在前面排版，对话被挤到右边、输入框被裁掉。
  ⌘1…⌘4 切换；`Settings` 场景（⌘,）放设置；`onOpenURL` 处理 `jarvis://pair`；App 变成活跃时 `ensureConnected` + `refresh`。
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
  - Apple Watch 同步：Mac 不和手表配对，`WatchSync` 是空的。
  - 拍照（`UIImagePickerController`）：`isSourceTypeAvailable(.camera)` 为 false，输入框不出「拍照」；图片从文件、剪贴板来。
  - 扫码配对（VisionKit `DataScannerViewController`）：换成「粘贴配对链接」按钮（`Mac/Settings/PairingPaste+Mac.swift`），或直接打开 `jarvis://pair?…` 链接。
  - 远程推送：target 还没有 APNs entitlement，`PushRegistrar` 不注册；App 开着时靠 WebSocket，本地通知照常。
  - 健康数据：macOS 有 HealthKit 的接口但没有数据，`phone_health_summary` 在 Mac 上不注册（`PhoneToolRegistry.healthTools`）；设置里手机数据的健康开关还在，待收掉。
- 临时实现，等后面打磨：外层侧栏和页面自己的分栏是两层（应做成真正的三栏）；分页的 `TabView(.page)` 换成了默认样式 / 箭头翻页；
  `SwipeTo`、Agent 间的双指滑动在 Mac 上不做事；终端的 option 键不当 meta；代码行只有整行选择、没有 ⌘F；对话页的玻璃按钮、浮层菜单没有按 Mac 习惯调；
  字号跟随设置里的小 / 标准 / 大，但 Mac 没有动态字体（`UIFontMetrics` 按 iOS 的 body 表换算）。

## 菜单栏、Dock 角标、通知（Mac）

`apple/Mac/App/MacAttention.swift`、`MacMenuBar.swift`，测试 `apple/JarvisMacTests`（`JarvisMac` scheme 的 test）。

- Mac 没有 APNs（target 没有推送权限），hub 推不到这里：`MacAttentionWatcher` 盯着 AppModel 的会话列表（App 开着时 WebSocket 一直连着），
  有 Agent 刚变成「需要你」/「完成未看」就发一条本地通知，规则和 hub 给手机推送的一样，读同一份 设置 › 通知（`NotifyPrefs`：需要你处理时、
  Agent 做完时 不推送 / 只推长任务（这一轮干了 `long_turn_seconds`，默认 60 秒以上）/ 每次都推）。不发的：App 在前台时正开着的那个对话、屏蔽了的、
  列表里刚出现的、答完紧接着做完的（同一次停下）、Jarvis 对话做完（它问你时照发）、体验模式、启动后 5 秒内（缓存和第一次刷新的差别是旧事）。
  第一次真有通知要发时才申请权限。
- 通知带和 hub 推送一样的 userInfo（`machine_id` / `session_id` / `interaction_id`）和类别：授权有「允许 / 拒绝」，计划有「批准 / 打回修改」，
  点按钮由 `MacAppDelegate` 直接交给 hub，点通知打开对话；同一个 Agent 的第二条「做完了」替换第一条。
- Dock 角标：需要你的数量（不算屏蔽的），和侧边栏的角标同口径。
- 菜单栏（`MenuBarExtra`，菜单样式，主窗口关了也在）：图标旁是需要你的数量；菜单里分「需要你」（等得最久的在前）、「正在工作」、
  「做完了，还没看」（最近 5 个）。需要你的一项是子菜单：说了什么 + 卡片上一点就能答的那几个（单个问题的选项、允许 / 拒绝、批准计划、
  hub 读出来的终端短选项）+ 打开对话；别的点一下打开对话。最后是「打开 Jarvis」「退出 Jarvis」。

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

## 收件箱、飞书、邮件、链接（Mac）

`apple/Mac/Inbox/`、`apple/Mac/Link/MacLinkPanel.swift`。逻辑和行、会话、邮件页都是 iOS 那一份，Mac 只换了外面的组织方式：

- 三块独立的视图，给主窗口的三栏接（区域 A）：`MacInboxFilters`（侧边栏「收件箱」「类型」两组行：全部 / 飞书 / 各个邮箱，@我 / 私聊 / 群聊 / 邮件 / Agent，
  带「需要你」的数量；行的 tag 是 `MacInboxFilter`，侧边栏的 selection 用这个类型就能原生选中，或者绑 `MacInboxState.shared.filter`）、
  `MacInboxList`（列表栏）、`MacInboxDetail`（详情栏）。三者共用 `MacInboxState.shared`（筛选沿用 iOS 的 `inbox.source` / `inbox.kind`、搜索词、选中、展开的分组、邮箱列表、要打开的弹层）。
  A 接好三栏之前，`MacInboxPage`（筛选列 | 列表 | 详情，列表宽度可拖、记在 `mac.inbox.listWidth`）放在壳的「收件箱」里。
- 列表：系统选中（⇧ / ⌘ 多选），↑↓ 移动，**E 或 ⌫ 标为已处理**并选中下一件（像 Mail 删信），⏎ / 双击在新窗口打开（Agent 行：打开它的对话），
  右键：在新窗口打开、AI 草拟回复 / 查看草稿、交给 AI、已处理 / 移回（多选时「N 件都标为已处理」）、拷贝内容、在飞书中打开；触控板左滑 = 已处理；悬停有浅底。
  工具栏：刷新 ⌘R、写邮件 ⌥⌘N、管理邮箱；搜索在工具栏（`.searchable(placement: .toolbar)`）。分组标题可点开 / 收起，「全部看过」「全部清除」照旧。
- 详情：飞书会话（从第一条新消息开始）/ 邮件 / 云文档条目，顶上一行 AI 摘要（和「AI 分身正在处理」），iOS 的动作条换成工具栏按钮（AI 草拟回复、交给 AI、已处理 / 移回）。
  窗口标题是条目标题，副标题是来源。人、别的会话这类飞书页面在详情栏自己的栈里推进去；飞书文档、网页链接在右侧的链接面板里开。多选时详情是「已选 N 件 · 全部标为已处理」。
  弹层（草稿、交给 AI、写信、邮箱、添加邮箱）是窗口内的 sheet，`.macSheet()` 给最小尺寸并把 `Form` 换成 `.grouped`（Mac 默认的两列表单会把 iOS 的分组行挤到标签列、截掉）。
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


## 0. App shell / navigation
- [ ] 4 tabs with badges: Jarvis (pending assistant questions), Agents (pending interactions, excluding muted), 收件箱 (inbox action count), 设置 (「体验」 badge in demo) — Jarvis/Views/RootView.swift
- [ ] Toast overlay (success / info-blue / error), haptics (success/warning/error) — Views/RootView.swift (ToastOverlay), App/AppModel.swift
- [ ] Rebuild whole tree when style/persona changes, keep tab + open chat — Views/RootView.swift
- [ ] Width-driven layout: compact = single NavigationStack, regular = NavigationSplitView (list | detail), shared path, `.splitDetailPage()`, sidebar width 340–480, SidebarTopFill — Views/SplitNavigation.swift
- [ ] Readable width cap (chat 760pt, welcome 560pt) — Views/Chat/ChatView.swift, Shared/Design/Theme.swift
- [ ] URL handling: `jarvis://pair?...` pairing, `jarvis://open?machine=&session=` (Live Activity) — App/JarvisApp.swift
- [ ] Scene phase: reconnect + refresh on active, disconnect socket on background — App/JarvisApp.swift
- [ ] Remember last tab / last open chat / reading position across cold start (`chat.read.<session>`) — App/AppModel.swift, Views/Chat/ChatView.swift
- [ ] DEBUG launch args (-tab, -openSession, -openPane, -openTerminal, -lark-connector, etc. list in docs/ios.md) — App/JarvisApp.swift + views

## 1. Jarvis tab (assistant)
- [ ] Headless Claude assistant conversations (main conversation `assistant.main` + side branches); first launch opens main — Views/Assistant/AssistantView.swift, App/AppModel.swift (assistantMain, refreshAssistant)
- [ ] Home: greeting + available devices + 6 suggestions; launcher/model chip list; StarterComposer (or BeautifulStarterComposer) with photos — Views/Assistant/AssistantView.swift (AssistantHome, StarterComposer, StarterModelChip, AssistantToolsRow)
- [ ] First message shows bubble + 「正在准备」 immediately, session starts in background; warm assistant — AssistantView.swift, AppModel.warmAssistant
- [ ] Thinking hidden; tool calls folded into 「工具 N · 电脑、iPhone…」 — Views/Chat/ChatLines.swift, ChatView.swift
- [ ] Floating top bar with fog, center persona dot + name capsule; connection state replaces name — Views/Assistant/AssistantTopBar.swift (AssistantTopBar, TopFog)
- [ ] Avatar menu: 知识库, 详细信息 (profile), 回到主对话, 拷贝目录 / 拷贝信息 / 拷贝会话 ID (「✓ 已拷贝」 3s), 启动器 submenu (switch launcher, applies via --resume next message), 会话信息, 中断, 连接详情 — AssistantTopBar.swift
- [ ] Left button: history sidebar; right button: new chat (disabled on new-chat page) — AssistantTopBar.swift
- [ ] Sidebar: search, new chat, Jarvis card (online/offline + machine → profile), 定时任务 / 知识库 tiles, 置顶 (main pinned, long-press pin, `assistant.pinned`), day groups 今天/昨天/最近7天/更早 (8 + 查看全部), row = title + preview + time + status dot (need-you red / unread blue / working spinner), long-press 拷贝会话 ID / 结束 / 置顶, 设置 row in drawer — Views/Assistant/AssistantSidebar.swift
- [ ] Drawer gesture: pan anywhere, 45° rule, yields to horizontally scrollable content, velocity/half settle, page card scale/offset, tab bar fade (UIKit host) — AssistantSidebar.swift (DrawerPan, DrawerPageMotion, DrawerSidebarHost), Views/Assistant/DrawerProbe.swift
- [ ] Regular width: sidebar = split view left column, toggle button — AssistantView.swift, SplitNavigation.swift
- [ ] Jarvis avatar (mask face, eyes move when working) — Views/Assistant/JarvisAvatar.swift
- [ ] Profile page: identity, machine, online/working, 「接下来」 3 items, 主动做事 (tasks, run history), 了解你 (关于我, 记忆), 设置 (launcher, 管理启动器), capabilities list — Views/Assistant/AssistantProfileView.swift (AssistantProfileView, AssistantUpcomingRow, AssistantRunsView, AssistantRunRow)
- [ ] 「交给 Jarvis」 from other pages sends `<jarvis-context>` into main conversation — AppModel.askJarvis

## 2. Agents tab (dev list)
- [ ] Always-visible search; quick filters 全部/需要你/未读/工作中 with counts; snapshot (`kept`) semantics — Views/Chats/ChatsView.swift (QuickFilterBar, AgentsList)
- [ ] Grouping menu: 按项目 / 按状态 / 按机器 (`DevGrouping`, remembered); one rank order — ChatsView.swift, Views/Chats/AgentListOrder.swift, Views/Chats/AgentsMenus.swift (AgentsListMenu)
- [ ] Card vs compact list (`dev.compact`); compact rows on one scale — ChatsView.swift (SessionRow), Views/Chats/CompactList.swift
- [ ] 「需要你处理」 cards with quick-action buttons (card mode) / CompactInteractionRow — Views/Interactions/InteractionViews.swift (InteractionPreviewCard), CompactList.swift
- [ ] Pinned agents, pinned spaces, busy / just-done / muted-running / idle spaces (folded), 「还有 N 个空闲」 — ChatsView.swift (IdleFoldRow, QuietSpaceRow, SpaceHeader, StateCounts)
- [ ] Fold/unfold spaces and section headers (`dev.folded`), 全部折叠/全部展开 — ChatsView.swift, AgentsMenus.swift
- [ ] Mark all read (filter + per-space, with confirm) — ChatsView.swift, App/AppModel.swift (markSeen)
- [ ] Swipe: left = pin / interrupt, right = close; long-press row menu (rename, mute/unmute, 交给 Jarvis, 拷贝恢复命令, 关闭 Agent…, stop loop) — ChatsView.swift, AgentsMenus.swift (AgentRowMenu)
- [ ] Close agent confirmation sheet — DesignSystem/Components.swift (CloseAgentSheet, CloseAgentConfirmation)
- [ ] Space header ⋯ menu: 打开空间, 置顶, 屏蔽/开启通知, 重命名空间, 全部标为已读 — AgentsMenus.swift (SpaceHeaderMenu), ChatsView.swift (SpaceMenuItems, RenameSpaceAlert)
- [ ] Rename agent (herdr tab + Claude session name) — ChatsView.swift (RenameSessionAlert), AppModel.renameSession
- [ ] Pins / mutes synced with hub (/api/v1/pins, /mutes), optimistic, per-agent override of space mute — App/PinsStore.swift
- [ ] Badges: 「⟳ 下次 12:20」 loop, 「⚙ 后台 N」, 「👥 子 Agent N」, muted 🔕 styling — ChatsView.swift (SessionRow)
- [ ] Machine filter menu, 「机器监控」 entry, 新建会话 button — ChatsView.swift (TopBar), AgentsMenus.swift
- [ ] Connection status in title slot — Views/ConnectionTitle.swift
- [ ] Pull to refresh — ChatsView.swift
- [ ] Destinations registry (SessionKey, space, machine pages) — ChatsView.swift (AgentsDestinations)

## 3. Spaces & machine pages
- [ ] Space page: agents in herdr tab order (SlotCard), machine button, menu (rename, mute, mark read, 新标签 / 在这个空间开新标签) — Views/Chats/SpaceView.swift (SpaceView, SlotCard)
- [ ] Sibling tabs strip in chat header (state-colored capsules, swipe, ⋯ list with red dot, 「空间 X 的所有 Agent」) — SpaceView.swift (SiblingTabs, SiblingList)
- [ ] Machine spaces page: online state, counts, busy projects with latest line, idle projects, loose agents, ⓘ details — Views/Chats/MachineSpacesView.swift (MachineSpacesView, BusySpaceRow)

## 4. Chat page (agent conversation)
- [ ] Message rendering: user bubble, assistant markdown (MarkdownUI `.jarvis` theme), thinking row, peer 「来自 名字」 card, pending/sending bubble (retry/delete), merged queued bubble, sent photos, attached images — Views/Chat/MessageViews.swift (UserBubble, PeerMessage, PendingBubble, MergedUserBubble, SentPhotos, AttachedImages, AssistantText, ThinkingRow, NoticeRow)
- [ ] Markdown tables fitted / horizontal scroll — Views/Chat/MarkdownTable.swift (FittedTable)
- [ ] Line model: one turn → message / work line / answer; work line counts & duration; split merged messages — Views/Chat/ChatLines.swift, ChatView.swift (WorkLine, WorkStepView, WorkCardSlice)
- [ ] Work trace expand/collapse in place (timeline style for glass, card style for others), 「收起 N 步」, pin tapped row under finger — ChatView.swift, Views/Chat/ChatPin.swift
- [ ] Tool views: tool groups, tool call rows, detail, Bash command/output, full output sheet, TodoWrite list, base64 images, folded work row — Views/Chat/ToolViews.swift
- [ ] Unified DiffView (Edit/MultiEdit/Write + Codex unified diff), +N −N, 40-line truncation, horizontal scroll — ToolViews.swift (DiffView), Views/Session/GitPane.swift (DiffText)
- [ ] BeautifulUI style: ThinkingState, tool-run chips (ToolRunRow/ToolChipRow) — ToolViews.swift, Views/BeautifulUIBridge.swift, Packages/BeautifulUI
- [ ] Wake (background task) chip + WakeDrawer; recap strip + RecapDrawer (folded multiples) — MessageViews.swift (WakeChip, RecapCard, RecapStrip, RecapDrawer)
- [ ] Background task notices with outcome — Views/Chat/TaskNoteRow.swift
- [ ] API error card (human explanation, chips, details copyable, folded repeats, 重试) — Views/Chat/APIErrorCard.swift
- [ ] Claude suggestion pill (填入 / 直接发送) — ChatView.swift (SuggestionPill)
- [ ] Inverted list, follow-end logic, frozen inserts while reading history, ↓ jump button, landing position (landUnderInput / saved position), turn-based pagination (turns=2) with auto-load at top — ChatView.swift (ChatStreamFollow), Shared/ChatStore.swift
- [ ] Local chat cache (last 2 turns per chat, 6 MB) + chats-v2 disk cache — Shared/StateCache.swift, Shared/ChatStore.swift
- [ ] Lazy images on scroll with memory + disk cache — Views/Chat/ChatImageLoader.swift (ChatThumbnail)
- [ ] Collapsing self-drawn header (ChromeHeader: title, subtitle state·machine·space›tab, sibling row, loop/background chips), follows finger, auto-collapse after 3s idle — ChatView.swift
- [ ] Session info sheet (run / usage / location, copy id, copy resume command) — ChatView.swift (SessionInfoSheet)
- [ ] ⋯ menu: 终端, 文件, Git (N), 空间里的所有 Agent, 重命名…, 屏蔽, 拷贝 session id, 拷贝恢复命令, 清空上下文 /clear, 关闭 Agent… — ChatView.swift
- [ ] Swipe left chat → Files → Git; swipe-back gesture re-enabled — Views/Session/SessionPanes.swift (SessionPane, SessionPaneBar), Views/Chat/SwipeBack.swift
- [ ] Hide tab bar in single-column chat — Views/Chat/TabBarHiding.swift
- [ ] Side-vertical toolbar (iPhone Duo) layout — ChatView.swift (sideBarItems, sideHeader)
- [ ] Live status strip (spinner line, output preview, → terminal) — SessionPanes.swift (ActivityChips, ActivitySheet)
- [ ] Loop strip (interval, next wake, task, 停止循环 confirm) + loop tick rows — SessionPanes.swift (LoopStrip, LoopTickRow)
- [ ] Background tasks strip (N 个后台任务, open output file) — SessionPanes.swift (BackgroundTasksStrip)
- [ ] Subagents strip + read-only subagent view + footer 回到主 Agent — SessionPanes.swift (AgentStrip, SubagentFooter)
- [ ] Queue strip (撤回编辑 / 拷贝 / 删除, pull all back) — ChatView.swift (QueueStrip), AppModel.editQueued
- [ ] Skeleton / starting note (「正在启动…」, 「没有启动起来」) — ChatView.swift (ChatSkeleton, StartingNote)
- [ ] Long-press message action bar: 复制 / 选择 / 引用 / 分享 / 编辑 (own); select-text sheet — Views/Chat/MessageMenu.swift
- [ ] Share card image (light/dark, save to Photos, share image/text) — Views/Chat/ShareCard.swift
- [ ] Image viewer (all chat images, paging, zoom, save/edit/share/copy) — Views/Chat/ImageViewer.swift
- [ ] Image editor (box / pen, 6 colors, undo/clear, 加到输入框) — Views/Chat/ImageEditor.swift
- [ ] File path chips (verified via /stat), tool-row paths clickable — Views/Chat/FileViewer.swift (FileChips, LookUpFiles), Code/FilePresence.swift
- [ ] Context reference cards in bubbles (`<jarvis-context>`) — Views/Link/LinkSheet.swift (ContextCards, ContextChips)
- [ ] Send-safety: refusal when computer input has draft → show / clear-and-send — App/SendSafety.swift
- [ ] Frame stats / perf probes (debug) — Views/Chat/FrameStats.swift, App/PerfSignpost.swift

## 5. Composer
- [ ] ComposerRow: + menu (照片 ≤4 / 拍照 / 文件 / 快捷指令), glass input, send / red interrupt — Views/Chat/ComposerParts.swift, ChatView.swift (Composer, CameraPicker)
- [ ] Pending images strip (tap to edit), 1600px compression — ComposerParts.swift (ComposerImages)
- [ ] File upload to the session machine's temp dir, paths inserted at cursor — Views/Chat/FileAttach.swift (UploadsBar)
- [ ] BeautifulUI PromptComposer: @ mentions (session files / other sessions as context), / slash commands (SlashCommand.all, /clear confirm), model (/model alias), dictation — Views/Chat/PromptComposer.swift
- [ ] Dictation (SFSpeechRecognizer zh + AVAudioEngine) — PromptComposer.swift (Dictation)
- [ ] Permission mode + effort chips (SessionModeBar), new-session picker — Views/Chat/SessionModes.swift
- [ ] Drafts persisted per session — AppModel.drafts
- [ ] Context chips above input (from web selection / doc / other session) — LinkSheet.swift (ContextChips)
- [ ] Optimistic send with pending bubble, client_msg_id, retry/discard — Shared/ChatStore.swift, AppModel.send/retry/discard

## 6. Interactions (questions / plans / tool approvals / terminal dialogs)
- [ ] Interaction panel above composer — Views/Interactions/InteractionViews.swift (InteractionPanel)
- [ ] QuestionForm: numbered options, 推荐 tag, 其他回答…, multi-question paging, auto-submit — InteractionViews.swift (QuestionForm, RecommendedTag)
- [ ] PlanForm: markdown preview + fullscreen, 批准并自动执行 / 批准并自动接受编辑 / 打回 with comment — InteractionViews.swift (PlanForm)
- [ ] PermissionForm: 允许 / 拒绝 / 本会话总是允许 — InteractionViews.swift (PermissionForm)
- [ ] ScreenForm 「终端在问你」: parsed dialog → keys, 其他回答 preKeys, 改在对话里说, 查看终端 — InteractionViews.swift (ScreenForm, ScreenSteps, TerminalText)
- [ ] Preview cards in lists with quick buttons — InteractionViews.swift (InteractionPreviewCard, FlowButtons)
- [ ] Phone-tool write confirmation card — AppModel.pendingConfirmation, PhoneTools/PhoneToolRegistry.swift
- [ ] Resolve API — AppModel.resolve

## 7. Terminal
- [ ] Terminal cover (bottom-up full page, drag to dismiss, size/handle/settings bar) — Views/Chat/ChatView.swift (TerminalCover), Views/Session/SessionPanes.swift (TerminalPanel)
- [ ] Live stream (terminal_stream, epoch/seq) or 1s ANSI polling fallback — Views/Session/Terminal/TerminalFeed.swift, SessionPanes.swift (TerminalFeedLink)
- [ ] Grid model + cell renderer (wide chars, box drawing as shapes) — Terminal/TerminalGrid.swift, Terminal/TerminalView.swift (TerminalCanvas), Terminal/BoxDrawing.swift
- [ ] Phone-size takeover (terminal_control), 接管 / 只看 prompt — SessionPanes.swift, AppModel.terminalSubscribe
- [ ] Wrap toggle, themes (Jarvis/Solarized/深夜/ink), font size, pinch zoom — Terminal/TerminalWrap.swift, Terminal/TerminalTheme.swift, TerminalView.swift
- [ ] Selection (long-press word, drag, 拷贝 / 全选), OSC 8 / URL tap → in-app link — TerminalView.swift (SelectionHandle)
- [ ] Scrollback loading 200 lines at a time (screen/before), 回到最新, ~1000-line limit notice — SessionPanes.swift
- [ ] Link status bar (last update, retry backoff, 重试) — SessionPanes.swift (TerminalLinkStatus)
- [ ] Key bar: sticky/locked/held ctrl/alt/shift, esc, tab, ⌃C, D-pad, paste, shortcuts — Views/Chat/TerminalKeys.swift (TerminalKeyBar, KeyCap, DirectionPad)
- [ ] Input: field mode + direct typing mode (UIKeyInput / UITextInput, hardware arrow keys) — Terminal/TerminalInput.swift
- [ ] Gestures: 2-finger swipe sibling agent / adjacent space, double-tap paste (multi-line confirm), scroll in-terminal — TerminalView.swift
- [ ] Shortcuts panel (single tap fill, double tap send), terminal settings (keys shown, auto-hide bar, theme, font, wrap), tips, completions from pane contents — Terminal/TerminalExtras.swift (ShortcutsPanel, TerminalSettingsView, TerminalPreview, ThemeRow, SuggestionStrip), SessionPanes.swift (TerminalTips)

## 8. Files / Git / code viewer
- [ ] Files pane: breadcrumbs, dirs first, size/mtime, git markers, hidden toggle, 拷贝路径, 2000 limit — Views/Session/FilesPane.swift
- [ ] Git pane: branch, upstream ↑↓, stashes, changes (staged/unstaged switch), diff sheet, commit history paging, commit sheet, copy diff/path — Views/Session/GitPane.swift
- [ ] FileViewer: code (syntax, line numbers, copy line/all), JSON outline (fold, copy value/path, expand/collapse all), Markdown rendered/source, images, PDF, directories, share — Views/Chat/FileViewer.swift
- [ ] Paged file fetch (2 MB pages) — Code/CodeFileSource.swift
- [ ] Line splitting, background highlighting (2000-line chunks, caps) — Code/CodeDocument.swift
- [ ] Virtualized code lines view (UILabel reuse, context menu) — Code/CodeLinesView.swift
- [ ] JSON outline — Code/JSONOutline.swift
- [ ] Markdown chunking — Code/MarkdownChunks.swift
- [ ] Syntax languages + lexer — Code/SyntaxLanguage.swift, Code/SyntaxLexer.swift
- [ ] Monospace scrollable text (diffs, output) — Views/Chat/MonoTextView.swift
- [ ] Path presence cache — Code/FilePresence.swift; perf log — Code/FilePerf.swift

## 9. Link viewer (in-app web)
- [x] Bottom sheet opening full height, 60% detent with background interaction — Views/Link/LinkSheet.swift (LinkSheet)
- [x] WKWebView pool (preload first 3 links per reply, LRU 6, shared data store, cache policy) — LinkSheet.swift (WebViewPool, PooledWebView)
- [x] Text selection menu: 「AI 修改这段」, 「作为上下文」; 选择文字… sheet; 在 Safari 打开; share — LinkSheet.swift (SelectableWebView, PickTextSheet, DocPickMenu)
- [x] Feishu docs/wiki links → native reader — Views/Feishu/Docs/FeishuDocReader.swift

## 10. New chat form
- [x] Machine picker, launcher picker (per-machine launchers, 跟随启动器), directory browser with search, 加入空间, title, first prompt, mode picker; remembers machine/launcher/cwd for Siri — Views/NewChat/NewChatView.swift (NewChatView, LauncherPicker, DirectoryBrowser)（Mac：⌘N 打开 sheet，见「快速打开、新建」）
- [ ] Prefilled new chat (from mail 「让 Claude 处理」) — Views/Mail/MailViews.swift (NewChatPrefilled)

## 11. Machines / monitoring
- [ ] 设置 › 机器 list: online, sessions, version, launcher, reconnect; version-mismatch 重新部署 — Views/Machines/MachinesView.swift (MachinesView, MachineCard, DeployRow)
- [ ] Machine detail (系统与监控, running Claude list) — MachinesView.swift (MachineDetailView)
- [ ] Dashboard: N/M online, per-machine card with CPU/mem/net now + 1h sparklines, alert rings, 10s polling — Views/Machines/MachineDashboardView.swift
- [ ] Monitor page: 1/6/24h, CPU/mem/net/load charts with shared scrub, per-core bars, system facts, gaps — Views/Machines/MachineMonitorView.swift, Views/Machines/MetricCharts.swift (Swift Charts)
- [ ] OS badge — Views/Machines/OSBadge.swift; model — Shared/MachineMetrics.swift

## 12. Knowledge base
- [ ] Kind pills (项目/会话/人/文档/会议/日程 with counts), tag filter, project sections (重点/其他进行中/已完成/不是项目) — Views/Knowledge/KnowledgeView.swift (KnowledgeView, KBKindBar, KBRow, KBAvatar, ImportanceBadge, KBPill)
- [ ] Full-text search, merged per entry, highlighted hits — KnowledgeView.swift (KBHitRow)
- [ ] Detail: header, 问 Jarvis / 开 Agent, agents, timeline, profile, relations, code dirs, last 7 days messages, path; search matches — KnowledgeView.swift (KnowledgeDetailView, KBMatchRow, KBTimelineRow, KBLinkRow, KBMessageText)
- [ ] Day view of chat messages with prev/next — KnowledgeView.swift (KBDayView)
- [ ] 打标 editor (importance, relevance, tags, project type/status) — KnowledgeView.swift (KBTagEditor)
- [ ] Models — Shared/Knowledge.swift

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
- [x] Add account: manual IMAP/SMTP form, 「AI 一键添加」 paste recognition (clipboard), Sign in with Google (web auth session) — MailViews.swift (AddMailAccountView, GoogleSignInLabel), Views/Mail/MailAccountForm.swift

## 16. Model launchers
- [ ] Launcher list (Jarvis-managed + ~/.zshrc, sources, hidden) with search — Views/Settings/ModelLaunchersView.swift (ModelLaunchersView, LauncherRow)
- [ ] Detail: per-machine params, function source (masked), 测试可用性, delete/hide with popover confirm, restore — ModelLaunchersView.swift (ModelLauncherDetailView, LauncherDetailSections, LauncherCheckRow)
- [ ] Editor (name, base URL, API key, model, small model, args) — ModelLaunchersView.swift (LauncherEditor)
- [ ] Quota section (pools/windows, colors ≥70/90%, refresh) — Views/Settings/LauncherQuotaViews.swift
- [ ] Config models — Shared/LauncherConfig.swift

## 17. Personas & styles
- [ ] Persona picker / gallery (follow style colors), persona store (download, cache by rev, APNG frames) — Views/Settings/PersonaSettingsView.swift, Persona/PersonaStore.swift, Persona/Persona.swift, Persona/PersonaViews.swift (PersonaAvatar, PersonaDot, PersonaFramesView)
- [ ] Persona dot states idle/thinking/working/needs_you/offline, reduce-motion = frame 0 — PersonaViews.swift, AssistantTopBar.swift
- [ ] UI styles 液态玻璃 / Claude / 黏土 / Island / 飞书 / BeautifulUI with preview cards — Views/Settings/SettingsView.swift (StylePicker, StyleCard), Shared/Design/StyleSpec.swift, Shared/Design/Styles/*.swift
- [ ] Text size 小/标准/大 over Dynamic Type — DesignSystem/TextSize.swift
- [ ] Theme tokens, icons, palettes, status palette — Shared/Design/Theme.swift, Icons.swift, Palette.swift, PersonaPalette.swift, Shared/StatusPalette.swift
- [ ] Components (StatusDot, Chip, CountBadge, EmptyStateView, Monogram…) and surfaces/buttons — DesignSystem/Components.swift, DesignSystem/Styling.swift

## 18. Notifications & notification actions
- [ ] Request permission + APNs registration, POST /devices — App/JarvisApp.swift (AppDelegate.registerIfPossible)
- [x] Categories INTERACTION_PERMISSION (允许 auth-required / 拒绝) and INTERACTION_PLAN (批准 / 打回修改 text input) — App/NotificationActions.swift
- [ ] Background resolve from action, failure local notification — App/JarvisApp.swift (resolveFromNotification)
- [x] Tap opens session (main-queue completion handler), suppress while viewing — JarvisApp.swift（Mac：`MacAppDelegate`，本地通知见「菜单栏、Dock 角标、通知」）
- [ ] Silent push handling (wake / disconnect) — JarvisApp.swift
- [ ] Settings: 需要你处理时, Agent 做完时 (不推送/只推长任务/每次都推), per-device prefs on hub, system-disabled warning + open settings — Views/Settings/NotificationSettingsView.swift

## 19. Live Activity
- [x] Fleet activity (needsYou/working/done), push-to-start + push token upload, start/update/end (1 min grace), stale handling — App/LiveActivityController.swift, LiveActivity/JarvisActivityAttributes.swift, Shared/FleetState.swift（Mac：菜单栏 `MacMenuBar`，从会话列表算，不用 fleet 推送）
- [ ] Lock screen + Dynamic Island views, watch smart stack — JarvisWidgets/FleetLiveActivity.swift, LiveActivity/FleetIslandViews.swift
- [x] Buttons: Allow/Deny permission, Approve plan (LiveActivityIntent run in app) — LiveActivity/FleetIntents.swift, App/FleetActions.swift（Mac：菜单栏的子菜单）
- [ ] Island settings (presets A/B/C/custom, load machine, previews), lock details toggle — Views/Settings/IslandSettingsView.swift, NotificationSettingsView.swift

## 20. Siri / App Intents
- [ ] 问 Claude (machine param, new session, returns OpenSession) — Intents/AskClaudeIntent.swift
- [ ] Jarvis 状态 — Intents/JarvisStatusIntent.swift
- [ ] 批准第一个待处理 — Intents/ApproveFirstIntent.swift
- [ ] 打开会话 (foreground) + SessionEntity — Intents/OpenSessionIntent.swift
- [ ] MachineEntity query — Intents/MachineEntity.swift
- [ ] Shortcuts phrases — Intents/JarvisShortcuts.swift; texts / picker / errors — Intents/IntentSupport.swift

## 21. Phone tools (phone_* MCP)
- [ ] Register enabled tools on connect, tool_call → tool_result, write confirmations — PhoneTools/PhoneToolRegistry.swift, PhoneTool.swift, PhoneToolSupport.swift, AppModel.registerPhone
- [ ] Contacts search — PhoneTools/ContactsTool.swift
- [ ] Calendar events list/create — PhoneTools/CalendarTool.swift
- [ ] Reminders list/create/complete — PhoneTools/RemindersTool.swift
- [ ] Current location — PhoneTools/LocationTool.swift
- [ ] Health summary — PhoneTools/HealthTool.swift
- [ ] Photos recent / get — PhoneTools/PhotosTool.swift
- [ ] Device info — PhoneTools/DeviceTool.swift
- [ ] Clipboard get/set — PhoneTools/ClipboardTool.swift
- [ ] Local notify — PhoneTools/NotificationsTool.swift
- [ ] Authorization status / request — PhoneTools/PhoneAccess.swift
- [ ] Settings › 手机数据 per-source toggles + 授权 — Views/Settings/SettingsView.swift (PhoneDataView)

## 22. Demo mode
- [ ] 「没有 hub？先体验一下」, ServerConfig.demo, local APIClient + local socket, Demo.json with relative times, generated metrics, echo replies, demo errors shown as blue info — App/DemoHub.swift, App/AppModel.swift (enterDemo), Shared/APIClient.swift, Shared/LiveConnection.swift
- [ ] Settings demo section (连接我的 hub / 退出体验模式), 体验 tab badge — SettingsView.swift (DemoSection), RootView.swift

## 23. Pairing / credentials / watch
- [ ] Welcome page, QR scan (VisionKit), manual entry, jarvis://pair link — Views/Settings/SettingsView.swift (WelcomeView, PairView, QRScanner)
- [ ] Keychain storage — Shared/Keychain.swift
- [ ] Device token exchange, 设置 › 设备 list + revoke — App/DeviceCredentials.swift (DevicesView)
- [ ] 重新配对, 断开并忘记服务器 — SettingsView.swift
- [ ] Sync to Apple Watch (WCSession) — Shared/WatchSync.swift, SettingsView.swift; watch app — JarvisWatch/JarvisWatchApp.swift (macOS: n/a)

## 24. Offline / outbox / connection
- [ ] Connection status waiting/connecting/updating (NWPathMonitor, ping 5s, debounce 1.5s), title replacement, details popover (立即重试) — App/ConnectionStatus.swift, Views/ConnectionTitle.swift
- [ ] WebSocket with backoff + since_seq resubscribe — Shared/LiveConnection.swift
- [ ] Outbox: offline messages persisted, auto-send in order after catch-up — App/Outbox.swift
- [ ] One-shot actions not queued; blue 「刚才的操作没有生效」 — AppModel.show / resolve
- [ ] Background delta sync for agents (rev/after_seq, 40 sessions, concurrency 3) — App/AgentSync.swift
- [ ] State cache for machines/sessions/interactions/inbox/mail accounts/lark chats — Shared/StateCache.swift

## 25. Search
- [ ] Agents search — ChatsView.swift (searchable, SearchCloseProbe)
- [ ] Assistant sidebar search — AssistantSidebar.swift
- [x] Inbox search — InboxView.swift
- [ ] Knowledge search — KnowledgeView.swift
- [ ] Launcher search, directory search, translation chat picker search — ModelLaunchersView.swift, NewChatView.swift, TranslationSettingsView.swift

## 26. Settings pages
- [ ] Root: header (demo note), 形象, 界面风格 + 字号, 助手, 模型, Claude Code 设置, 机器, 设备, 手机数据, 通知, 翻译, 存储空间, 服务器, 关于, 同步到 Apple Watch — Views/Settings/SettingsView.swift (SettingsDestinations, SettingsPage)
- [ ] 助手 → profile page; 定时任务 (upcoming timeline, daily/interval/once rows, toggle, swipe delete, long-press run now, templates, editor with model/permission/effort, bypass confirm, continue conversation, history) — Views/Settings/AssistantProactiveViews.swift (AssistantRoutinesView, AssistantTaskRow, AssistantTaskEditor, AssistantWeekdayPicker), Views/Settings/AssistantRoutinesModel.swift
- [ ] 记忆 (frontmatter, type tags, markdown detail, edit, swipe delete, MEMORY.md read-only) — AssistantProactiveViews.swift (AssistantMemoryView, AssistantMemoryDetail, AssistantMemoryEditor)
- [ ] 关于我 notes — AssistantProactiveViews.swift (AssistantNotesView)
- [ ] Claude Code 设置 (settings.json whitelist keys, apply to all machines) — SettingsView.swift (ClaudeSettingsView)
- [ ] 翻译 (auto translate, mail, chat picker) + Translator (NLLanguageRecognizer, batching) — Views/Settings/TranslationSettingsView.swift, App/Translator.swift
- [ ] 存储空间 (per category sizes, clear one / all, keep web logins) — Views/Settings/StorageView.swift
- [ ] 通知 / 灵动岛 — see §18, §19



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