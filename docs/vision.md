# visionOS App

原生 visionOS 版 Persona（`apple/` 里的 `PersonaVision` target，#1687）：和 iOS App 同一套逻辑层（`Shared/`、`Persona/App`）和内容视图，
外面是 visionOS 自己的壳（`apple/Vision/`）。做法照 Mac 版（`docs/mac.md`「做法」），不为 visionOS 改 iOS 和 Mac 的样子。

## 做法

- 编译的源码：`Vision/` + `Shared/` + `Persona/`（去掉 iOS 的 Info.plist 和 entitlements），再加实时活动里 AppModel 要读的形象图
  （`LiveActivity/FleetPersona.swift`、`FleetIslandViews.swift`）。Bundle id 和 iOS 版相同，`TARGETED_DEVICE_FAMILY` 为 7。
- iOS 专用的分支：visionOS 上有同样 API 的（UIKit 视图、WebKit、AVFoundation、PhotosUI…）把 `#if os(iOS)` 扩成 `#if os(iOS) || os(visionOS)`，
  直接用 iOS 的代码。新代码写 `#if os(iOS)` 时要想一下 visionOS 能不能也走这一支，不能就给 visionOS 一个同名替身，别让 PersonaVision 编不过。
- visionOS 标成不可用的 SwiftUI 修饰符（`scrollDismissesKeyboard`、`navigationSubtitle`、`scrollEdgeEffectHidden`、`GlassEffectContainer`、
  `sharedBackgroundVisibility`）在 `Vision/Compat/SwiftUICompat.swift` 里给同名的 visionOS 版本，页面照常写。
- `UIGestureRecognizerRepresentable` 只有 iOS 有：消息的长按手势在 `Vision/Media/MessageGestures.swift` 用 SwiftUI 手势重写同名类型（注视加长捏合）。

## 和 iOS 的对应

| iOS | visionOS |
| --- | --- |
| 实时活动、灵动岛、灵动岛设置 | 没有。`LiveActivityController` 换成只保存 hub 推来的状态的替身（`Vision/App/VisionStandIns.swift`），给「需要你」装饰条用；设置里不显示灵动岛 |
| 拍照、扫码配对 | Vision Pro 不把摄像头给 App：`CameraPicker.isAvailable` 为 false，菜单里不出「拍照」；配对用粘贴链接 |
| 触感反馈 | 空操作 |
| 抽屉、侧滑切换 | 捏合拖动在 visionOS 上是滚动：远程画面的抽屉不能下拉关闭（点把手关），对话的左右分页只用按钮切 |
| VNC 画中画 | 没有（visionOS 不支持 sample buffer 画中画）：`VNCPiP.supported` 为 false，只开完整页 |
| 液态玻璃 | 窗口本身就是玻璃：玻璃容器只做布局，表面用系统材质 |

## 壳

- 主窗口的 tab 用系统 `TabView`，visionOS 画成窗口左侧竖排的装饰条：就是 iOS 旧布局 / iPad 的 `RootTabs`（同样的页面、标题、`Theme.Icon` 图标和角标），
  选中状态绑 `AppModel.selectedTab`。visionOS 上 `RootView` 不建 iPhone 的新外壳（`ShellView`、`ShellState`），所以没有第二个来源：
  通知、收件箱等写 `selectedTab` 的地方直接切 tab（`docs/shell.md` 说的唯一来源在这里就是 `selectedTab`）。外壳是 `Vision/App/VisionShell.swift` 的 `.visionShell()`。
- 对话可以单独开一个窗口（`Vision/App/VisionScenes.swift`：`WindowGroup("对话", id: "chat", for: SessionKey.self)`，和 Mac 的 `MacChatWindow` 同一个做法），
  会话键就是窗口的可恢复值，系统会恢复这些窗口。入口：Agents 列表行的长按菜单「在新窗口打开」。窗口里的对话是第一页（没有返回箭头），对话里推出的页面在这个窗口自己的导航栈里，和 iPad 一样。
  DEBUG：`-openChatWindow <machine>/<session>` 启动时开这个对话的窗口（截图用）。
- 主窗口底部的「需要你」装饰条（`.ornament(attachmentAnchor: .scene(.bottom))`）：个数就是 tab 角标的两个数相加（`AppModel.badges`：Persona 的和 Agents 的，屏蔽的 Agent 不算），
  「去处理」打开等得最久的那个，正开着其中一个时打开下一个；个数为 0 时不显示。玻璃背景是系统的 `glassBackgroundEffect`，收在设计系统的 `.visionOrnamentPanel()`（`Vision/Design/VisionTokens.swift`，visionOS 专用的尺寸 token 也在这里）。

## 构建、运行、截图

```bash
eval "$(python3 scripts/persona_config.py env)"
python3 scripts/persona_config.py xcconfig
(cd apple && xcodegen generate)
xcodebuild -project apple/Persona.xcodeproj -scheme PersonaVision \
  -destination 'generic/platform=visionOS Simulator' -derivedDataPath build/DD-vision \
  CODE_SIGN_IDENTITY=- CODE_SIGNING_ALLOWED=YES DEVELOPMENT_TEAM= build
xcrun simctl install <udid> build/DD-vision/Build/Products/Debug-xrsimulator/Persona.app
xcrun simctl launch <udid> <bundle id> -openWelcome YES -demo.on YES   # 体验模式的演示数据
xcrun simctl io <udid> screenshot vision.png
```

- 模拟器上要用本机自签名（`CODE_SIGN_IDENTITY=-`）：`CODE_SIGNING_ALLOWED=NO` 编出来的读写不了钥匙串，进不了体验模式。
  原因：钥匙串读失败（缺 entitlement）时注册表算「读不出来」（`registryUnreadable`），体验模式只在注册表确定为空时才开。
  DEBUG 的 visionOS 构建上，`-openWelcome YES` 把刚清空的注册表当作确定为空，所以没签名也能进体验模式（没在模拟器上验证过）。
- 改了 iOS / Mac 共用的代码，三个都编一遍：`Persona`、`PersonaMac`、`PersonaVision`。
- visionOS 模拟器上 `simctl terminate` 之后再 `launch`，常常不出窗口（进程在、场景没建）：先 `simctl shutdown` 再 `boot`。
