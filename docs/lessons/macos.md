# 经验：macOS

踩过的坑、怪异行为和当时的取舍（原来在 `CLAUDE.md`「经验」，索引见 `docs/lessons/README.md`）。新经验写进对应的节，没有合适的就新开一节。

- Mac 版（`JarvisMac`，`docs/mac.md`）和 iOS 共用页面：iOS 专用的 SwiftUI 修饰符 / 枚举成员在 `apple/Mac/Compat/` 里写同名的 Mac 版本（被标成 unavailable 的系统符号会让位给模块里的同名声明），
  UIKit 视图在 `apple/Mac/` 下写同名的 AppKit 类型；iOS 文件只加 `#if os(iOS)`。Mac target 和 App 一样默认 MainActor 隔离：Compat 里的纯数据类型、给解码线程用的 `NSImage` 辅助都要标 `nonisolated`。
- 别把页面的分栏（`SplitNavigation`、Jarvis 页的 `NavigationSplitView`）放进另一个 `NavigationSplitView` 的详情栏：里面的详情栏会按外层侧栏还在前面来排，
  对话整个往右挪一个侧栏宽、输入框被裁掉（2026-10-31）。Mac 壳的六项侧栏是 HStack 里的一列。
- Mac 锁屏时 `screencapture` 只拍到锁屏：截图用 `scripts/mac_shot.sh`（App 自己 `cacheDisplay` 出 PNG，`docs/mac.md`「构建、运行、截图」）。
  它画不出毛玻璃 / Liquid Glass；SwiftUI `List` 的行要单独再画一遍；深色下 List 行文字是浅色方案的颜色，还没确认是不是截图的问题。
  深浅色用 `-macAppearance`，`-AppleInterfaceStyle Dark` 当启动参数不生效。
- 直接跑 `Jarvis.app/Contents/MacOS/Jarvis` 能带启动参数（`-demo.on YES -tab dev`…），未签名的 Debug 构建不进沙盒，写 `/tmp` 没问题；`open` 要写成 `open -n Jarvis.app --args -demo.on YES …` 才带得上参数（不带 `--args` 就没有，2026-11-02 实测）。
- Mac 锁屏时 `xcodebuild test -scheme JarvisMac` 报「The test runner hung before establishing connection」（2026-11-01）：测试宿主 Jarvis.app 启动时读钥匙串（`Keychain.serverConfig` → `SecItemCopyMatching`），读别的未签名构建写的条目会一直卡在 securityd。Mac 的 DEBUG 构建在 XCTest 下（有 `XCTestConfigurationFilePath`）和截图模式一样不读钥匙串（`Shared/Keychain.swift`）。
- 共用页面里碰到 `pin.list` 这类滚动视图时，iOS 上是 `UIScrollView`、Mac 上是 `NSScrollView`（`ChatPin+Mac.swift`），`isTracking`、`isDragging`、`isDecelerating` 这些 Mac 没有：用 `#if os(iOS)` 隔开，或者在 `Mac/Compat/` 补等价的。
  2026-11-02 #243 在 `ChatView` 里加了一行 `pin.list?.isTracking`，只跑了 iOS，master 上 `JarvisMac` 编译不过（terminal 车道发现）。改了 `apple/Jarvis/Views` 的共用页面，合入前也跑一次 `JarvisMac` 的构建（`docs/mac.md`「构建、运行、截图」）。
- 构建配置（`~/.config/jarvis/config.json`）没有 `app.bundle_id` 时，`Config.xcconfig` 的 `JARVIS_BUNDLE_ID` 是空的，xcodebuild 照样 BUILD SUCCEEDED，
  但 Info.plist 里没有 `CFBundleIdentifier`，Mac App 一启动就在 `applicationDidFinishLaunching` 的 `UNUserNotificationCenter.current()` 断言崩溃（SIGABRT，2026-11-02）。
  只在本机编来自己用、不发版的 Mac，可以在 xcodebuild 后面加 `JARVIS_BUNDLE_ID=<随便一个反向域名>` 覆盖，不用改配置。
  真配对用 `open -a <Jarvis.app> 'jarvis://pair?…'`（`jarvisd hub pair` 打印的链接）；`-pairURL` 只是 DEBUG 的临时配对，不换设备凭据（`DeviceCredentials`），但会把这条配对写进钥匙串（`MacLaunchArguments.apply`）。
- 窗口的 sheet 不继承主窗口的 `.formStyle(.grouped)`（`macLook` 里设的那个）：iOS 的 Form 在 sheet 里退回 Mac 默认的两栏，
  标题贴着 sheet 左边、输入框拉满右边（2026-11-02「连接 hub」）。sheet 的内容自己加 `macSheet()`。分组表单里 `TextField` 的标题在左、
  输入在右且没有框，要给 `prompt:` 才看得出能输入。
- Mac 上的 `Menu` 画成带下拉箭头的按钮：iOS 那种 `.themedButton().clipShape(.circle)` 的圆形 + 菜单被裁成半个图形；`.buttonStyle(.glass)`
  也不给 `Menu` 画底，`.glassProminent` 裁成圆后连填充都没了。输入栏的圆钮在 Mac 上用 `.menuStyle(.button).menuIndicator(.hidden)`，
  圆底自己画（`Mac/Design/MacRoundButton.swift`）。放在 `.surface` 胶囊里的 `TextField` 要 `.textFieldStyle(.plain)`，否则 Mac 自带的边框在里面再画一层。
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
