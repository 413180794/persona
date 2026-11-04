# 经验：macOS

踩过的坑、怪异行为和当时的取舍（原来在 `CLAUDE.md`「经验」，索引见 `docs/lessons/README.md`）。新经验写进对应的节，没有合适的就新开一节。

- 同名的系统文字样式在 Mac 上比 iPhone 小一截，级差也不一样（2026-11-03，#779）：Mac 的 body 13、callout 12、subheadline 11、footnote 和 caption 都是 10。
  照搬 iPhone 的选法（说明用 footnote、标签用 caption），时间、来源都只有 10pt；而 Markdown 的基础字号写死 17（iPhone 的 body），回答和档案比用户消息大一截，
  同一页上忽大忽小。`Theme.Font` 在 Mac 上按自己的字号表画系统字体，`Theme.Level.size` 跟着分两套，凡是按「正文大小」算的地方（Markdown、表格、飞书的 `LarkTextBase`）取 `Theme.Level`。
  另外 Mac 上系统文字样式和 `@ScaledMetric` 都不跟 `dynamicTypeSize` 变，靠它实现的「小 / 标准 / 大」在 Mac 上不改字号（#833）。

- SwiftUI 的竖向 `TextField`（`axis: .vertical`）在 Mac 上底下是 AppKit 的字段编辑器，回车和滚轮都不按聊天输入框的习惯来（2026-11-03，#785）：
  ⏎ 和 ⇧⏎ 都是「结束编辑」，还会把框里的字全选，接着打的字就把原来的覆盖了；只有 ⌥⏎ 换行。超过 `lineLimit` 的行数以后，框只跟着光标滚，滚轮滚不动。
  输入框获焦时用本地事件监听（`MacComposerEventMonitor`，`Mac/Chat/MacComposerKeys.swift`，和 ⌘V 同一个）：⏎ 发送、⇧⏎ 调字段编辑器的
  `insertNewlineIgnoringFieldEditor`，输入法有没确认的字（`hasMarkedText`）时 ⏎ 交给输入法；滚轮滚字段编辑器外面那层 `NSClipView`，滚到头就交还给页面。
  cua 的 `type_text` 是直接插字，测不到拼音组字时的 ⏎，这一条只有单测。

- Persona Link 的界面和 helper 必须共用运行根（2026-11-03，#277）：只让 Go helper 支持 `PERSONA_HOME`，而界面继续固定读 `~/.jarvis/link.json`，会出现 helper 已配对、菜单却显示未配对。两端按新旧环境与默认软链解析同一目录；登录启动没有显式环境时，只从既有 `com.jarvis.node` 的环境字段恢复自定义根，并把解析结果同时传给常驻与一次性 helper。显式空的新变量不回退到旧环境或服务记录。解析器只查询路径，不读取凭据、不迁移文件、不启动真实 launchd 服务。

- 改了 Mac 也编的共享页面（`apple/Persona/Views/…`，如助手的任务视图）只编 iOS 不够，合入前 `PersonaMac` 也要 build（2026-11-04，#1083）：
  #1080 在 `TaskViews.swift` 里用了 `contentShape(.contextMenuPreview, …)`，这个成员 `@available(macOS, unavailable)`，iOS 编过、单测和截图都正常，master 上的 Mac 却编不过了。
  长按预览的形状用 `menuPreviewShape(_:)`（iOS 设形状，Mac 什么都不做）。

- Mac 版（`JarvisMac`，`docs/mac.md`）和 iOS 共用页面：iOS 专用的 SwiftUI 修饰符 / 枚举成员在 `apple/Mac/Compat/` 里写同名的 Mac 版本（被标成 unavailable 的系统符号会让位给模块里的同名声明），
  UIKit 视图在 `apple/Mac/` 下写同名的 AppKit 类型；iOS 文件只加 `#if os(iOS)`。Mac target 和 App 一样默认 MainActor 隔离：Compat 里的纯数据类型、给解码线程用的 `NSImage` 辅助都要标 `nonisolated`。
- 别把页面的分栏（`SplitNavigation`、Persona 页的 `NavigationSplitView`）放进另一个 `NavigationSplitView` 的详情栏：里面的详情栏会按外层侧栏还在前面来排，
  对话整个往右挪一个侧栏宽、输入框被裁掉（2026-10-31）。Mac 壳的六项侧栏是 HStack 里的一列。
- Mac 锁屏时 `screencapture` 只拍到锁屏：截图用 `scripts/app/mac_shot.sh`（App 自己 `cacheDisplay` 出 PNG，`docs/mac.md`「构建、运行、截图」）。
  它画不出毛玻璃 / Liquid Glass；SwiftUI `List` 的行要单独再画一遍；深色下 List 行文字是浅色方案的颜色，还没确认是不是截图的问题。
  深浅色用 `-macAppearance`，`-AppleInterfaceStyle Dark` 当启动参数不生效。
- 直接跑 `Jarvis.app/Contents/MacOS/Jarvis` 能带启动参数（`-demo.on YES -tab dev`…），未签名的 Debug 构建不进沙盒，写 `/tmp` 没问题；`open` 要写成 `open -n Jarvis.app --args -demo.on YES …` 才带得上参数（不带 `--args` 就没有，2026-11-02 实测）。
- Mac 锁屏时 `xcodebuild test -scheme JarvisMac` 报「The test runner hung before establishing connection」（2026-11-01）：测试宿主 Jarvis.app 启动时读钥匙串（`Keychain.serverConfig` → `SecItemCopyMatching`），读别的未签名构建写的条目会一直卡在 securityd。Mac 的 DEBUG 构建在 XCTest 下（有 `XCTestConfigurationFilePath`）和截图模式一样不读钥匙串（`Shared/Keychain.swift`）。
- 这台开发机上 `swift build` 默认的构建系统报「Could not determine path to developer directory」，换 `xcodebuild -scheme … -destination 'generic/platform=macOS'`
  编 Swift 包就好（`scripts/app/mac-link.sh`）。菜单栏 App 的截图不靠屏幕录制：App 带 `--snapshot <目录>`，把自己的视图放进离屏窗口用 `cacheDisplay` 画成 PNG。
- Mac 的推送 entitlement 叫 `com.apple.developer.aps-environment`，不是 iOS 的 `aps-environment`。App ID 开过推送就够（iPhone 和 Mac 同一个 UNIVERSAL 的 App ID），归档里写 `development`，App Store 导出时变成 `production`；本机不签名编的版本签名里没有任何 entitlement，收不到远程推送，只有本地通知（2026-11-02，`docs/mac.md`「远程推送（Mac）」）。
- 要和扩展共用钥匙串条目，macOS 上的 `SecItem` 查询得加 `kSecUseDataProtectionKeychain: true`：不加时走老式的文件钥匙串，`kSecAttrAccessGroup` 不起作用，条目只属于创建它的那个 App，扩展读不到（iOS 一直是 data protection 钥匙串，同样的代码在手机上是好的）。Mac 的推送私钥就是这样（`PushCrypto`，#432）；App 自己用的 `Keychain.swift` 还在文件钥匙串里，没动。
- 共用页面里碰到 `pin.list` 这类滚动视图时，iOS 上是 `UIScrollView`、Mac 上是 `NSScrollView`（`ChatPin+Mac.swift`），`isTracking`、`isDragging`、`isDecelerating` 这些 Mac 没有：用 `#if os(iOS)` 隔开，或者在 `Mac/Compat/` 补等价的。
  2026-11-02 #243 在 `ChatView` 里加了一行 `pin.list?.isTracking`，只跑了 iOS，master 上 `JarvisMac` 编译不过（terminal 车道发现）。改了 `apple/Persona/Views` 的共用页面，合入前也跑一次 `PersonaMac` 的构建（`docs/mac.md`「构建、运行、截图」）。
- 构建配置（`~/.config/jarvis/config.json`）没有 `app.bundle_id` 时，`Config.xcconfig` 的 `JARVIS_BUNDLE_ID` 是空的，xcodebuild 照样 BUILD SUCCEEDED，
  但 Info.plist 里没有 `CFBundleIdentifier`，Mac App 一启动就在 `applicationDidFinishLaunching` 的 `UNUserNotificationCenter.current()` 断言崩溃（SIGABRT，2026-11-02）。
  只在本机编来自己用、不发版的 Mac，可以在 xcodebuild 后面加 `JARVIS_BUNDLE_ID=<随便一个反向域名>` 覆盖，不用改配置。
  真配对用 `open -a <Jarvis.app> 'jarvis://pair?…'`（`jarvisd hub pair` 打印的链接）；`-pairURL` 只是 DEBUG 的临时配对，不换设备凭据（`DeviceCredentials`），但会把这条配对写进钥匙串（`MacLaunchArguments.apply`）。
- 窗口的 sheet 不继承主窗口的 `.formStyle(.grouped)`（`macLook` 里设的那个）：iOS 的 Form 在 sheet 里退回 Mac 默认的两栏，
  标题贴着 sheet 左边、输入框拉满右边（2026-11-02「连接 hub」）。sheet 的内容自己加 `macSheet()`。分组表单里 `TextField` 的标题在左、
  输入在右且没有框，要给 `prompt:` 才看得出能输入。
- sheet 里的 `NavigationStack`：最小尺寸（`macSheet()`）要加在栈上，不能加在栈的根内容上。加在根上时，推入的页面（新对话的启动器 / 目录）
  按自己的高度撑 sheet，`List` 还没量出行高就是 0：从 Agents 列表的「新建会话」打开时，一点启动器 sheet 缩成只剩标题和搜索框的一条，
  看上去像选不了（#303，2026-11-02）。⌘N 那条外面还套了一层带 `macSheet()` 的栈，所以没坏。
- Mac 上的 `Menu` 画成带下拉箭头的按钮：iOS 那种 `.themedButton().clipShape(.circle)` 的圆形 + 菜单被裁成半个图形；`.buttonStyle(.glass)`
  也不给 `Menu` 画底，`.glassProminent` 裁成圆后连填充都没了。输入栏的圆钮在 Mac 上用 `.menuStyle(.button).menuIndicator(.hidden)`，
  圆底自己画（`Mac/Design/MacRoundButton.swift`）。只要标签本身的（⋯、自己画了 ⌃⌄ 的 chip）用 `.labelOnlyMenu()`（`DesignSystem/Styling.swift`，iOS 上原样返回）：对话顶栏的 ⋯、输入框上方和新对话页的 chip、侧栏分组和 Agents 列表空间行的 ⋯ 原来都画成带 ⌄ 的灰色弹出按钮（#502）。工具栏里和带边框的按钮并排的（文件查看器的 ⋯，挨着「完成」）用 `.arrowlessMenu()`：只去掉 ⌄，留着系统画的按钮。放在 `.surface` 胶囊里的 `TextField` 要 `.textFieldStyle(.plain)`，否则 Mac 自带的边框在里面再画一层。
- 主窗口的 `.unified` 工具栏有底色和一条分隔线：伸到红绿灯后面的侧栏被它横着切断（2026-11-02）。壳上
  `.toolbarBackgroundVisibility(.hidden, for: .windowToolbar)` 去掉。壳不是分栏，工具栏的 `.navigation` 项排在红绿灯旁边、压在壳的侧栏上，
  页面自己放在那里的按钮（原来的「隐藏列表」）会被当成壳的按钮；标题栏高度随页面的工具栏变（28 / 52pt），侧栏顶上的留白用固定的 `macToolbarHeight`。
- macOS 26 的 `NavigationSplitView` 把侧栏那一栏画成离窗口四边一点的玻璃卡片（`NSContainerConcentricGlassEffectView`），周围露出窗口底色，
  看上去像浮在背景板上（2026-11-02 用户：非常丑）；SwiftUI 和 `NSSplitViewItem` 都没有关掉它的 API，`.listStyle`、`.background` 只改卡片里面。
  内容栏和详情栏是贴边的：Mac 上分栏写成三栏，侧栏一栏放 `Color.clear.toolbar(removing: .sidebarToggle)`、可见性 `.doubleColumn`，
  列表放内容栏（`SplitColumns`）。内容栏里的列表要自己 `.listStyle(.sidebar)` + `.scrollContentBackground(.hidden)`，否则有行分隔线、页脚被截断、底色发灰。
- 离屏截图画不出玻璃，看材质要截真窗口：解锁时另起一个体验模式实例（`open -n Jarvis.app --args -demo.on YES -ApplePersistenceIgnoreState YES …`），
  用有屏幕录制权限的 computer-use 工具（比如 cua-driver 的 `get_window_state` 加 `screenshot_out_file`）截这个实例的窗口，窗口不在当前屏幕上也截得到；
  这是 `CLAUDE.md`「截图验证不用 computer-use」的 Mac 例外：只截图、不操作，后台发的快捷键到不了菜单（窗口不是 key），要点按就别用这条路。
  别动用户自己开着的那个实例，`-pairURL` 会改写钥匙串里的配对。
- 只截不在前台的窗口会漏掉一类问题：系统控件在活跃窗口里换颜色。玻璃按钮（`.buttonStyle(.glass)`）在活跃窗口里整个填成主题色，
  按钮里紫色的「推荐」标签看不见；后台窗口里它是灰的，之前的截图全是后台窗口，一直没发现（2026-11-02 用户：文字被背景色盖住都看不清）。
  `.glassProminent` 的字是青色、`.foregroundStyle` 改不动。Mac 的 `themedButton` 因此自己画胶囊（`MacCapsuleButton`）。
  截活跃窗口：cua-driver 的 `bring_to_front`（带 `window_id`）把那个体验实例提到前台再截，会抢几秒焦点，实例退出后焦点回到原来的 App。
- 侧边栏样式的 `List`（`.listStyle(.sidebar)`）放在分栏的侧栏以外，窗口不在前台时行的字整个变浅灰，像禁用了一样；选中在活跃窗口里是主题色，和壳的灰色选中不是一套。
  收件箱的筛选栏改用壳的行（`MacSidebarRow`）。
- `List(selection:)` 的选中行在活跃窗口里是主题色实底，系统只把 `.primary` / `.secondary` 和 `Color.primary` 换成白色，行里自己定的颜色
  （草稿卡片的紫色标题、主题色的「修改」、星标）照原样画在蓝底上，看不见。`.environment(\.backgroundProminence, .standard)` 拦不住这次变白。
  做法（`MacHoverRow`）：`.listRowBackground(页面底色)` 盖住系统的选中底，选中的灰底自己画；行上 `.foregroundStyle(Color(nsColor: .labelColor))`，
  层级样式（`.primary`、`.secondary`）就跟着它走、不再变白；但子视图里写死的 `Color.primary` 还是会变白，要改成 `.primary`（iOS 上两者一样）。
- `.toolbar(removing: .title)` 之后，`.primaryAction` 的按钮排到工具栏最左边（Mac 上它本来就靠前，原来是标题把它们推到了右边）：
  去掉标题的同时补一个 `ToolbarSpacer(.flexible)`。
- 去掉工具栏的标题以后，那一行只剩按钮，页面上面就是一条空带（2026-11-03，#830）：自己画的分栏外壳（不是 `NavigationSplitView`）里系统不知道页面从哪一栏开始，
  标题只会排在红绿灯旁边、压在侧栏上。做法：壳在页面那一栏左上角叠一个不接点击的标题，`ignoresSafeArea(.container, edges: .top)` 放进标题栏那一行。
  对一个容器加 `ignoresSafeArea` 不会让 `NavigationSplitView` 里各栏的内容跟着上移，要加在每一栏自己的根视图上（Persona 页的列表栏和对话栈的根）；
  推进去的页面不加，它们照旧在标题栏下面。只有 `.primaryAction` 的工具栏，按钮会停在中间偏左，窄窗口里盖住左边的标题：加一个空的 `.principal` 项占住中间。
  Mac 的 `.inset` 列表给第一个 `Section` 留一条空的分组头和一条分隔线，放在最上面的 chip 行不要包 `Section`。
- 一直在动的 `TimelineView(.animation)` 在 Mac 上可能很贵（实测的只有 `StatusDot` 这一处）：每一帧都让 `NSHostingView` 申请更新约束，
  一路报到窗口，整个窗口重新布局，锁屏时也照跑。「需要你」圆点的呼吸圈这样让 Agents 页空闲占一个核的 14%（2026-11-02；iOS 上没量过）。
  Mac 上一直重复的动画交给 Core Animation（`NSViewRepresentable` 里的 `CAShapeLayer` + 无限重复的 `CAAnimationGroup`，`MacBreathingRing`），
  App 主线程不参与；记得在动画组上（不是子动画上）设 `preferredFrameRateRange`，否则按屏幕刷新率跑（内建屏 120 Hz）。几个圆点要同相，就按 Core Animation 的时钟对齐
  （`beginTime` 取整秒）；按墙钟对齐不行，Mac 睡眠时 Core Animation 的时钟停、墙钟照走，睡醒后新加的圆点就错开了。
  每秒一次的 `.periodic` 计时按同样的代价算是 1/30，没单独量。查空闲开销：锁屏时起体验实例，`ps -o cputime` 量 20 秒，再 `sample` 看主线程。
- 想让切换侧栏分区不重建页面，把去过的页面留在 `ZStack` 里藏起来不行：隐藏的视图照样往窗口工具栏注册 `.toolbar` 项和 `.searchable`，
  两个页面各有一个搜索框就直接崩（`NSToolbar … already contains an item with the identifier com.apple.SwiftUI.search`，2026-11-02）。
  系统 `TabView` 在 macOS 26 上是按标签页隔开工具栏的（StackOverflow 76032647 说的是 macOS 13），也保留各页状态；但它在工具栏正中放的
  分段选择器 SwiftUI 藏不掉，而且保活之后切换并没有变快：时间不在建视图，在新页面进窗口后的 AppKit 布局和整批换工具栏项（`docs/mac.md` 性能，#390）。
  先 `sample` 看时间去哪，再决定要不要保活。
- 别用写死的 `listRowInsets` 去对齐 `List` 里的分组标题：iOS 上分组标题的缩进是 20，Mac 上是行自带的缩进（窄得多），iOS 那套数值在 Mac 上把行往里多推一截。
  折起的空间只剩标题、画成普通一行，写死 20 时在 Mac 上比展开的空间标题左右各缩 20pt，名字被截成「weat…」（#405，2026-11-02）。Mac 上传 `nil`，用行自带的缩进。
- 两个视图各往窗口工具栏的 `.navigation` 放一项（壳的侧栏按钮、Agents 列表的过滤菜单），先后顺序靠不住：详情栏里的页面一带上自己的工具栏项
  （空间页的 ⋯、机器页的 ⓘ，放在什么位置都一样），SwiftUI 重排整个工具栏，列表那项就排到了壳那项前面（#483，2026-11-02）。
  要固定顺序就放进同一个 `ToolbarItemGroup`：Mac 上过滤菜单由壳画在侧栏按钮后面（`MacShellState.agentsList`），ChatsView 自己那项只留给 iOS。
  离屏截图看得出这个顺序（`scripts/app/mac_shot.sh … -openSession <对话> -openSpaceFromChat YES`），菜单本身只能在真窗口里点开。
- 自己处理鼠标事件的 `NSGestureRecognizer` 子类（覆盖 `rightMouseDown` 等直接改 `state`）不会记录事件位置：`location(in:)` 一直是 (0, 0)，
  即窗口左下角，SwiftUI 的 `context.converter` 也是从它换算的。Mac 上消息右键的操作条就这样被摆到左下角、压在输入框底下看不见（#501，2026-11-03）。
  子类要在事件里记下 `event.locationInWindow`，覆盖 `location(in:)` 返回它（`SecondaryClickRecognizer`）。单元测试要经 `window.sendEvent(_:)` 发事件：
  直接调 `rightMouseDown(with:)` 时 AppKit 不认状态变化，`state` 停在 `.possible`；按住 Control 的左键在测试里送不到识别器（窗口不是 key，视图 `acceptsFirstMouse` 返回 true 也一样），所以单测只测了右键，Control 点击在真窗口里验。
- 窗口工具栏里一个 `.principal` 项（哪怕是空的）会把分栏里列表栏声明的 `.primaryAction` 按钮挤出这一栏，摆到右边那一栏它的前面，左边空出一大块（#907）。
  想让按钮留在列表栏的右端，就别在这一栏放 `.principal`。在详情栏的 `NavigationStack` 或它的根视图上加 `.navigation` 项没用：对话页隐藏了导航栏
  （`.toolbarVisibility(.hidden, for: .navigationBar)`），推进对话后这些项一起消失；`ToolbarSpacer(.flexible)` 只和壳里的那个平分空白，也贴不过去。
- Persona Link 的面板是老样子（直角、一圈浅色细边框，不是系统别的菜单栏面板那种圆角玻璃，2026-11-03，#956）：`xcodebuild` 编 SwiftPM 包的可执行文件时，
  `LC_BUILD_VERSION` 里的 sdk 记成了部署目标（`vtool -show-build` 看到 `sdk 15.0`），虽然实际用的是 Xcode 27 的 SDK；直接 `swiftc` 编同样的 target 是对的。
  系统按这个 sdk 版本决定给不给新外观，记成 15.0 就一直是老样子，离屏截图（`mac-link.sh snapshot`）看不出来，因为截的只是内容不是窗口。
  `scripts/app/mac-link.sh` 编完用 `vtool -set-build-version macos <最低版本> <SDK 版本> -replace` 改回真实的 SDK（胖二进制两个架构一起改），再签名，并检查改对了。
- Mac 的 sheet 没有导航栏，只画底栏那一排动作（2026-11-04，#743，`mac_shot.sh` 离屏实测）：`.topBarLeading`（兼容层映射成 `.navigation`）画不出来；
  `.confirmationAction` 只画一个按钮，放两个 `ToolbarItem` 或一个 `ToolbarItemGroup` 都只出第一个，另一个消失。
  要两个按钮时，「完成」放 `.cancellationAction`（Esc 关闭），另一个放 `.primaryAction`，两个都画在底栏右侧、都不是回车的默认按钮（文件查看器、「工具输出」都这样）。
- Mac 窗口工具栏去掉了系统标题以后（#830），推进去的页面的 `navigationTitle` 没地方显示（#855）：推入页面用 `pageTitle(_:subtitle:)`（iOS 上就是 `navigationTitle`），
  Mac 上它把标题画在这一页顶上的标题栏那一行，并用 `MacTitleRowTaken` 告诉外壳别再画分区名。左边距是标题自己量的（这一页是否贴着窗口左边缘），
  不读 `macTitleRowLeading`：分栏详情栏里推进去的页面拿到的是整页的值，环境值加在 `navigationDestination` 外面、`NavigationStack` 外面都试过，没用。
- Mac 上 SwiftUI 的系统文字样式（`.body`、`.subheadline`…）和 `@ScaledMetric` 都不跟 `.dynamicTypeSize(...)` 变（#833，2026-11-03 实测：套 `.medium` 到 `.xxxLarge`，`Font.body` 行高一直 16，ScaledMetric 一直 13），
  `Font.system(size:relativeTo:)` 也一样。所以 小 / 标准 / 大 在 Mac 上只能自己乘：`Theme.Font` 的每一级在 Mac 上是读 `Theme.Font.scale` 的计算属性，
  Markdown 主题的基础字号也乘它（主题是点数，`PerTextScale` 按比例缓存），改设置后 `macLook` 用 `.id(model.textSize)` 让整窗重画（SwiftUI 不观察这个静态值）。
  在别处把 `Theme.Font.xxx` 存进 `static let` 会停在第一次的字号（`CompactList` 改成了计算属性）；兼容层的 `UIFontMetrics` 原来按 iOS 的 body 表（16 / 17 / 19）换算，一档 6–12%，和 Mac 的一点一档对不上，改成 Mac 自己的表。
