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

- `pluginLinks` / `macLinkPanel` 只能挂在整页上（2026-11-04，#1517）：Mac 的链接面板用 `GeometryReader` 把内容和面板并排包起来，挂在 `Form` 里的一个
  `Section` 上时，那一行被压成只剩第一行（插件卡片只剩标题，iPhone 上的 sheet 没这个问题）。列表里的一节只接收页面的 `Binding<LinkTarget?>`。
- 改了 Mac 也编的共享页面（`apple/Persona/Views/…`，如助手的任务视图）只编 iOS 不够，合入前 `PersonaMac` 也要 build（2026-11-04，#1083）：
  #1080 在 `TaskViews.swift` 里用了 `contentShape(.contextMenuPreview, …)`，这个成员 `@available(macOS, unavailable)`，iOS 编过、单测和截图都正常，master 上的 Mac 却编不过了。
  长按预览的形状用 `menuPreviewShape(_:)`（iOS 设形状，Mac 什么都不做）。

- Mac 版（`JarvisMac`，`docs/mac.md`）和 iOS 共用页面：iOS 专用的 SwiftUI 修饰符 / 枚举成员在 `apple/Mac/Compat/` 里写同名的 Mac 版本（被标成 unavailable 的系统符号会让位给模块里的同名声明），
  UIKit 视图在 `apple/Mac/` 下写同名的 AppKit 类型；iOS 文件只加 `#if os(iOS)`。Mac target 和 App 一样默认 MainActor 隔离：Compat 里的纯数据类型、给解码线程用的 `NSImage` 辅助都要标 `nonisolated`。
- 别把页面的分栏（`SplitNavigation`、iOS 上 Persona 页的 `NavigationSplitView`）放进另一个 `NavigationSplitView` 的详情栏：里面的详情栏会按外层侧栏还在前面来排，
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
- 这样的三栏分栏收不起内容栏（2026-11-04，#856）：`columnVisibility` 从 `.doubleColumn` 设成 `.detailOnly`，几毫秒后系统经绑定写回 `.all`，
  空的侧栏那一栏冒出来成一条灰栏，列表也还在；去掉动画一样。要收起的列表（Persona 页的历史）不用分栏，用 `MacListBeside`（HStack：列表、
  `MacPanelResizeHandle`、页面），收起时页面的身份不变。查这类问题：在 `.onChange(of: columns)` 里打日志，看系统有没有改写绑定。
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
- `MenuBarExtra` 的标签或菜单读了推送会变的东西（`model.allSessions`），每次推送状态栏按钮都重新设图、量宽、重画，菜单整份重建，
  不管显示的变没变；在 Mac 上停在哪一页都要付，约占每次推送主线程时间的四分之一（#1495）。让它们读一份只在显示的东西变了才换的快照（`MacMenuBarState`）。
  同理，往 `MacShellState` 这种全局对象里放页面的闭包，页面走了要放掉：闭包抓着页面的 `@State`，页面的 feed 在别的分区照样跟着每次推送干活。
  量推送开销用 `ps -M`（第一行是主线程）在推送前后相减；`PerfCount` 的 ms 只算推送到 run loop 空闲那一段，少算很多。
- 想让切换侧栏分区不重建页面，把去过的页面留在 `ZStack` 里藏起来不行：隐藏的视图照样往窗口工具栏注册 `.toolbar` 项和 `.searchable`，
  两个页面各有一个搜索框就直接崩（`NSToolbar … already contains an item with the identifier com.apple.SwiftUI.search`，2026-11-02）。
  系统 `TabView` 在 macOS 26 上是按标签页隔开工具栏的（StackOverflow 76032647 说的是 macOS 13），也保留各页状态；但它在工具栏正中放的
  分段选择器 SwiftUI 藏不掉，而且保活之后切换并没有变快：时间不在建视图，在新页面进窗口后的 AppKit 布局和整批换工具栏项（`docs/mac.md` 性能，#390）。
  先 `sample` 看时间去哪，再决定要不要保活。
- 别用写死的 `listRowInsets` 去对齐 `List` 里的分组标题：iOS 上分组标题的缩进是 20，Mac 上是行自带的缩进（窄得多），iOS 那套数值在 Mac 上把行往里多推一截。
  折起的空间只剩标题、画成普通一行，写死 20 时在 Mac 上比展开的空间标题左右各缩 20pt，名字被截成「weat…」（#405，2026-11-02）。Mac 上传 `nil`，用行自带的缩进。
- 这台 Mac 的 hub 反向隧道（launchd `dev.jarvis.hub-tunnel`）断线后，hub 那台机器上死掉的 sshd 会继续占着 2232 好几小时
  （`ClientAliveInterval 28800`）：重试全是 `remote port forwarding failed for listen port 2232`，hub 一直报 `::1:2232 timed out`，
  打向这台 Mac 的会话、git、发送全 503，主对话在 hub 上所以看起来像「只有部分 503」（2026-11-02、2026-11-04 各一次）。
  两处一起改：hub 那台机器的 sshd `ClientAliveInterval` 改成 60（改完 `sshd -t` 再 reload），launchd 的隧道命令换成
  `~/.local/bin/hub-tunnel.sh`（先 ssh 过去杀掉仍在监听该端口的进程，`sort -u | xargs -r kill`，再建 `-R` 隧道）。
  卡住时 `launchctl kickstart -k gui/$(id -u)/dev.jarvis.hub-tunnel` 让 wrapper 自己清。
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
  按比例缩，「小」会把 11 pt 那几级（caption / label / code）压到约 10.2，违反 #779 用户定的「不小于 11」：`Theme.Font.pointSize` 在 Mac 上取 `max(级别 × scale, 11)`，只有这几级在「小」时停在 11，别的照比例。
- 设置列表在 Mac 上一半的行是灰的，像禁用了（2026-11-04，#1170）：列表在分栏的内容栏里用 `.listStyle(.sidebar)`（`SplitNavigation`），
  侧栏样式把一行里只有一个 `Label` 的行当源列表的行来画，文字是「活力」色，要靠背后的侧栏材质才显白；内容栏没有那层材质，就成了灰的。
  行其实能点。带值的行是 `LabeledContent`，不受影响，所以只有「模型供应商」「设备」这些没有值的行灰。Mac 上没有值的行也包一层 `LabeledContent`（`SettingsRow`）。
- Mac 的 sheet 只能靠底栏那一排的取消 / 完成（Esc）关：不能下滑，点 sheet 外面只响一声（2026-11-04，#1248）。所以一个 sheet 的关闭键放错位置（`.topBarLeading`），
  它就关不掉了：「模型配置」的「关闭」在 `.topBarLeading`、「保存」在 `.topBarTrailing`，Mac 上两个都看不到，改了存不了也关不掉。做法（`DesignSystem/SheetActions.swift`）：
  编辑类 sheet 用 `sheetCancel(unsaved:)`（`.cancellationAction`；Mac 上有未保存的修改先问「放弃未保存的修改？」，iOS 照旧直接关），
  只看的 sheet 用 `macSheetDone`（Mac 上底栏一个「完成」，iOS 什么都不加）。确认框挂在页面上、不挂在底栏的按钮上：底栏和页面不在一棵视图里。
  从待办详情弹出的「交给 Agent」没套 `macSheet()`，表单退回两栏，标签被 sheet 左边裁掉、说明文字超出右边（同上）。
- sheet 里的 `NavigationStack` 推进一页时，sheet 会缩到新页面的最小宽度，表单却按原来的宽度排，右边被裁掉，返回键还是 iPhone 那种圆形玻璃键（同上）。
  Mac 的 sheet 里别推页面：要再填一组东西就在上面再开一个 sheet，自己的「取消」放 `.cancellationAction`，「保存」放 `.confirmationAction`（配对页的「经 SSH 隧道连接…」；
  「模型配置」的分类列表点一类，在上面叠开这一类的编辑 sheet，#1248）。
- 分栏详情栏里推进第二层的页面（设置 › hub › SSH 隧道），系统的返回键不在这一栏，而是在窗口工具栏的最左边、红绿灯旁边，压在分区列上，画成圆形玻璃键（同上）。
  `splitDetailPage()` 在 Mac 上隐藏系统的返回键，在这一栏标题栏那一行的左边画一个 ‹（`MacDetailBack`）。这一行能点：内容延伸到标题栏下面，按钮照常收到点击（真窗口里实测过）。
- 窗口拉到最小时页面被窗口右边裁掉，不是压窄（2026-11-04，#1200）：`HStack` 里定宽的列表（`MacListBeside`、收件箱）不会因为窗口变窄而让位，
  旁边的页面拿到的宽度比它的内容能排开的最小宽度还小，SwiftUI 就按内容的最小宽度排、超出窗口的部分被裁（输入框里的字竖着排、停止键只剩一半）；
  同时它会去压别处能压的东西（分区列的图标被压没了）。定宽的列表要量整页的宽度（`onGeometryChange`），给旁边的页面留出 `macDetailMin` 再定自己的宽度；
  窗口的最小宽度按各列最小值之和定。分栏（`NavigationSplitView`）的内容栏在窗口变窄时不会自己缩到它的 `min`，详情栏的 `navigationSplitViewColumnWidth(min:)` 也压不动它，
  所以窗口最小宽度要按内容栏的理想宽度留余地（920 时 Agents 的对话约 308 宽，排得开）。
- Mac 上 SwiftUI `List` 的分组页脚只排一行，后面是省略号；`.listStyle(.sidebar)` 干脆不显示页脚（2026-11-04，#1212，单独写了个最小程序在真窗口里试过）。
  `fixedSize(horizontal: false, vertical: true)`、`lineLimit(nil)`、`lineLimit(5)`、套一层 `VStack`、`.plain` / `.bordered` / `.inset` 样式都一样。
  只有 `Form` + `.formStyle(.grouped)` 的页脚会换行（或者把说明当成一行内容放进分组里）。设置的子页因此在 Mac 上用 `SettingsForm`（分组表单）。
- Mac 分组表单（`Form` + `.grouped`）里 `.listRowBackground(Color.clear)` 不起作用，那一行的圆角卡片还在（2026-11-04，#1221，真窗口看过；我在第二批里写了「去掉底色」，是错的，第三批改正）。
  单独一个按钮的行在 Mac 上就是卡片里靠右的标准按钮。Mac 表单里输入框的标题画在左边当标签：标题本身就是提示的（「添加一项」「默认：…」「静音仓库：owner/name」）要 `.labelsHidden()`，否则左边一串提示、右边一个空框。
- 窗口工具栏里的 `.primaryAction`（兼容层的 `.topBarTrailing`）在我们这种自己拼的分栏里不跟着栏走（2026-11-04，#1223，真窗口 920 / 1280 / 1800 宽对比）：
  它的位置约是「窗口宽度的一半减几十点」，列表栏的按钮在宽窗口里跑到对话栏中间；页面有 `.searchable` 时右端被搜索框占了，按钮被排到窗口中间。
  改成 `.automatic` 也一样。要钉在某一栏上的按钮，画在那一栏自己的标题行里（`overlay` + `ignoresSafeArea(.container, edges: .top)`，那一行能点），或者放进页面内容。
  另外：macOS 26 上 `.toolbar { }` 的内容在某个平台分支里为空会编不过（`EmptyView` 当 `ToolbarContent` 要 macOS 27），整个 `.toolbar` 用 `#if` 包起来。
- 在 App 里推着滚动视图量帧（`clip.setBoundsOrigin`），SwiftUI 不当成用户在滚（2026-11-04，#1337）：滚动阶段一直是 `.idle`，
  对话页照样「跟着最新」、落到上次读到的位置，和推着它的这边每帧拉扯，量出来 342–356 ms/s，按用户滚动的方式量是 65–86。
  前后发 `NSScrollView.willStartLiveScrollNotification` / `didLiveScrollNotification` / `didEndLiveScrollNotification`，阶段就是 `.interacting`（`-showPerfGlide live`）。
  合成带阶段的滚轮事件交给 `scrollWheel(with:)` 或 `NSWindow.sendEvent` 也不行：AppKit 另有手势跟踪，6 秒只滚 380 pt。量完看移动了多少（`scrolledPt`），到头停住的那一轮不算。
- `NSColor.withAlphaComponent(_:)` 是把透明度换成给的值，不是乘上去（2026-11-04，#1224）：兼容层里 `tertiaryLabelColor.withAlphaComponent(0.18)` 得到的是 18% 的黑 / 白，
  不是 tertiaryLabel 本来的 26% 再乘 0.18，所以 Mac 的 `Theme.Fill.well` / `chip` 比 iPhone 深好几倍（真窗口取像素：卡片 248，选项区 212；深色 54 对 98）。量颜色用真窗口截图取像素，别凭感觉。
  #1169 把兼容层的四档填充改成了 UIKit 的原值：要照搬 iPhone 的颜色，在模拟器里 `simctl spawn` 一个打印 `resolvedColor(with:)` 的小程序取值，别凭记忆。
- 列表往上滚时，行从标题栏那一行透出来，和壳画在那里的分区标题（`MacPageTitle`）叠在一起（2026-11-04，#1243，真窗口实测）：列表的滚动视图自己伸到窗口工具栏下面、内容往下让出工具栏的高度，
  本来有系统的滚动边缘效果（AppKit 的 `NSScrollPocket`）在那一行挡住滚上去的行；壳为了让分区列伸到红绿灯后面隐藏了工具栏的底色
  （`.toolbarBackgroundVisibility(.hidden, for: .windowToolbar)`），这块挡板也跟着没了。设置、收件箱、机器（窄窗口）都能滚出来，
  分区列收起时设置的列表还会盖到红绿灯；Agents 的列表滚到快速筛选那一排底下就被挡住，待办的列表上面是搜索框，这两个碰不到这一行。
  把工具栏底色打开不行：每个滚动视图顶上出现一块不透明的挡板和一条分隔线，静止时也在；Persona 页自己画在那一行的头被盖成模糊的，Agents 滚动后连分区列顶上也变白。
  `scrollEdgeEffectStyle(.soft / .hard)` 改得动挡板的样式值，样子却没区别；窗口和分栏项的 `titlebarSeparatorStyle = .none` 都去不掉那条线（它只受挡板私有的 `allowsSeparator` 管）。
  做法：`MacBelowTitleRow` 让列表从这一行下面开始排，并裁在自己的 frame 上，滚上去的行在这一行的下沿消失；只加 `padding` + `ignoresSafeArea` 不够，
  分栏内容栏里 `List` 的滚动视图照样伸到工具栏下面（收件箱那种 `HStack` 里的列表倒是被裁了），要再 `.clipped()`。
  裁剪要加在页面底色的里面：加在外面连底色一起裁掉，黏土这类带底色的风格里标题那一行成了白的。所以加在设置的列表上（`.themedPage()` 之前）、
  收件箱的筛选栏上（底色在外层的 `HStack`），知识库和机器的首页由壳经 environment `macBelowTitleRow` 请页面自己加（这两页在 Persona 页、设置的详情栏里也会打开，那里不加）。
  收件箱中间的列表不加：标题不在它上面，而它钉住的分组头（「需要你」）靠的就是系统那块挡板，挪到这一行下面后挡板没了，分组头直接压在滚上去的行上。
  Agents 不加：列表栏的标题行里有它自己的按钮（`MacTitleRowButton`，画在列表那一层的 overlay 里），整栏裁掉按钮也没了；它的列表本来也碰不到这一行。
  静止时真窗口截图和改之前逐像素对比，标题栏那一行没有差别，别处只有时间、动画这些本来就会变的地方（各分区，液态玻璃深浅色、黏土、飞书，分区列收起）。
  推进去自己画标题的页（`pageTitle`）这次没加：滚动后只有钉在顶上的分组头的模糊探进这一行几个点，没有行压着标题。
- 截图前后对比之前，先用同一个构建连拍几次，看哪些地方本来就不稳定：Persona 页推进去的档案页（`-tab assistant -openAssistantPage profile`），
  标题下面有时空着一条带分隔线的带、有时内容直接贴着标题行，同一构建连拍三次两次有（2026-11-04，#1243 时看到，和那次改动无关，没追）。
  只拍一次就当成改动的后果，差点得出错误的结论。
- 离屏截图看不出上面这个问题修没修好：列表的行是截图时单独再画、贴回原处的，原来只按所在滚动视图的可见区域裁，`.clipped()` 挡掉的行照样画在标题栏那一行。
  现在按行上面所有裁剪的视图（`clipsToBounds` / `masksToBounds`）裁。要看滚动以后的样子用 `-macSnapshotScroll <点数>`（`mac_shot.sh` 后面加上）。
  没锁屏、终端有屏幕录制权限时，`screencapture -x -o -l <窗口号>` 能直接截体验实例的窗口（窗口号按 pid 从 `CGWindowListCopyWindowInfo` 找，宽高过滤掉菜单栏那个窗口），
  不用 computer-use 工具；实例偶尔会被激活抢一下焦点。
- 子页面改了数据，下面那页在 Mac 上不会自己重拉（2026-11-04，#1433）：iOS 的 NavigationStack 弹回时下面那页重新出现，`.task` 会再跑一次（`ios.md` 里 NavigationStack 根视图那条）；
  Mac 上同一份代码回到列表，`.task` 不再跑，设置 › 模型供应商在启动器详情页隐藏之后列表还是旧的。只在 iOS 上验证会漏掉这个。
  子页面改了列表要用的数据时，在模型上加一个版本号，列表写 `.task(id: 版本号)`（`AppModel.launchersRevision`），不要指望页面重新出现。
- `@AppStorage` 在任何 UserDefaults 写入时都让持有它的视图重算，不管写的是哪个键（2026-11-04，#1337，`_printChanges` 和写入计数确认）。
  Mac 上拖窗口改大小时，`NavigationSplitView` 背后的 `NSSplitView` 每一帧都自动保存一次 `NSSplitView Subview Frames …`，外壳和 Agents 列表每帧重算、137 行整表比对，
  窗口拉起来只有约 4 fps。在常显示、重的视图里用 `@StoredValue`（只在自己那个键的值变了时重算）。
- 这台开发机的 Instruments 录不到 Animation Hitches 的显示数据（开发者模式关着、要 sudo），`CGEvent.postToPid` 发的滚轮事件被系统过滤（发事件的进程没有辅助功能授权），
  看起来都「成功」但什么都没发生。量顺不顺用 App 自己的 display link（`MacPerfHarness`），看主线程用 `sample`。
- sheet 的最小尺寸要放在 sheet 内容的最外层，中间隔着 `GeometryReader` 就传不出去（2026-11-04，#1502）：Mac 的 sheet 按内容的最小尺寸开，
  `GeometryReader` 接受任何尺寸、最小是 0，里面再写 `.frame(minWidth:minHeight:)` 也到不了 sheet。#813 给文件查看器的栈外面套了 `.macLinkPanel`（最外层是 `GeometryReader`），
  `.macLargeSheet` 还留在栈的内容上，sheet 就缩成了工具栏的 470 × 80，什么文件都看不见。`MacFileViewerSheetTests` 用 `NSHostingView.fittingSize` 量 sheet 会开多大，加了外层修饰符时跑它。
  挪到栈外面也还是要 `bars: true`：栈的标题栏和底部工具栏是 sheet 窗口自己加的，量不进 `fittingSize`。写成 `bars: false` 时 sheet 有 1216 × 821，比 820 高的主窗口还高，
  第一次启动就在 SwiftUI 的窗口布局里断言崩了（`EXC_BREAKPOINT`，栈上全是 `_layoutSubtreeWithOldSize`）。
- 插件页面的进度条、额度条在 Mac 上是灰的（2026-11-04，#1512）：只在**非活动窗口**里灰，窗口在前台、是 key window 时按 `.tint` 画（主题色 / 红 / 橙），
  同一个窗口里的开关、按钮底色也一起变灰，这是系统对非活动窗口的统一画法，不是 tint 没生效，不改。结论要用两张图对比：同一个实例分别在活动、非活动时 `screencapture -l <窗口号>`。
  截图别把 App 强行拉到前台（`osascript … set frontmost`、每张都重启 App）：用户在这台 Mac 上干活，一直被打断；要活动窗口的图就请用户自己把窗口切到前面。
- `Button(role: .destructive)` 在 Mac 的 grouped `Form` 里画成主题色（蓝），不像 iPhone 的列表那样自动变红（2026-11-04，#1512 截图发现）。
  要红就 `.tint(Theme.danger)`（Form 里的按钮）或 `.foregroundStyle(Theme.danger)`（borderless 按钮）；确认框（`confirmationDialog`）里的不用管，系统按 role 画。
