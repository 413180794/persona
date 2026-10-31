# 经验：macOS

踩过的坑、怪异行为和当时的取舍（原来在 `CLAUDE.md`「经验」，索引见 `docs/lessons/README.md`）。新经验写进对应的节，没有合适的就新开一节。

- Mac 版（`JarvisMac`，`docs/mac.md`）和 iOS 共用页面：iOS 专用的 SwiftUI 修饰符 / 枚举成员在 `apple/Mac/Compat/` 里写同名的 Mac 版本（被标成 unavailable 的系统符号会让位给模块里的同名声明），
  UIKit 视图在 `apple/Mac/` 下写同名的 AppKit 类型；iOS 文件只加 `#if os(iOS)`。Mac target 和 App 一样默认 MainActor 隔离：Compat 里的纯数据类型、给解码线程用的 `NSImage` 辅助都要标 `nonisolated`。
- 别把页面的分栏（`SplitNavigation`、Jarvis 页的 `NavigationSplitView`）放进另一个 `NavigationSplitView` 的详情栏：里面的详情栏会按外层侧栏还在前面来排，
  对话整个往右挪一个侧栏宽、输入框被裁掉（2026-10-31）。Mac 壳的四项侧栏是 HStack 里的一列。
- Mac 锁屏时 `screencapture` 只拍到锁屏：截图用 `scripts/mac_shot.sh`（App 自己 `cacheDisplay` 出 PNG，`docs/mac.md`「构建、运行、截图」）。
  它画不出毛玻璃 / Liquid Glass；SwiftUI `List` 的行要单独再画一遍；深色下 List 行文字是浅色方案的颜色，还没确认是不是截图的问题。
  深浅色用 `-macAppearance`，`-AppleInterfaceStyle Dark` 当启动参数不生效。
- 直接跑 `Jarvis.app/Contents/MacOS/Jarvis` 能带启动参数（`-demo.on YES -tab dev`…），未签名的 Debug 构建不进沙盒，写 `/tmp` 没问题；`open` 起的 App 拿不到这些参数。
