# 经验：Android

新的多模块安卓 App（`android/`，`docs/android.md`、`docs/android-design.md`）踩过的坑。主线旧 App（单模块）的经验在 `docs/android.md`「经验」的「主线旧 App 的经验」一节。

## 界面与 Compose

- Android 的 `AndroidView` 不会按自己的布局边界裁剪：里面的 View 在 `onDraw` 里 `drawColor` 会盖住上面的兄弟组件（终端的工具栏整条被背景色盖掉），要加 `Modifier.clipToBounds()`。
- Compose 的 `Dialog(usePlatformDefaultWidth = false)` 做整页时，窗口默认不铺到系统栏下面：状态栏和手势条处各露出一条灰带（是背后的遮罩）。
  加 `decorFitsSystemWindows = false` 铺满以后，对话框窗口的状态栏图标又不跟 Activity 走，浅色页面上是白字（历史会话页踩过）。整页弹层一律用 `JFullScreen`。
- 玻璃页面的滚动边缘按「页面顶在屏幕顶上」画：嵌在别的界面下面的 `JPage`（链接卡片里的飞书文档）没有顶栏，顶部模糊会把内容第一行糊掉，所以没有顶栏就不画顶部边缘（`PageFrame`）。
- Haze 默认用约 0.78 倍分辨率的内容做模糊：浮层多的页面（对话页的顶栏、任务条、状态行、输入框）在手机上帧时间翻倍，`LocalHazePerformanceMode`
  设成 `Performance`（0.25 倍）后截图看不出差别（`JarvisTheme`）。模拟器的 GPU 是模拟的，玻璃一帧上百毫秒，性能只能在真机上量（`scripts/diag/android_scroll_perf.py`）。
- Android 切页面闪烁的几个根源（2026-11-01 逐帧抓出来的）：① Nav3 的 `NavDisplay` 只组合当前 tab，`key(selected)` 一换整页重建，空状态、加载圈、
  「还没有消息」轮流闪一遍 —— 访问过的 tab 保持组合、隐藏时不放置，并用 `rememberNavigationEventDispatcherOwner(enabled = false)` 关掉它的返回；
  ② 浮层高度用 `onSizeChanged` 量完再写回 state，第一帧内容没留位置，标题从状态栏下面跳下来 —— 用 `SubcomposeLayout` 一次量完（`PageFrame`）；
  ③ 外壳按「栈里只剩根页」显示底栏，返回时底栏先画在还在退出的对话页上 —— 底栏放进根页面（`NavEntryDecorator`），跟页面一起转场；
  ④ 转场里两页同时半透明会叠字 —— 推入、返回只滑动不淡入淡出。录屏：`adb shell screenrecord` 后 `ffmpeg -vf fps=60` 抽帧，按相邻帧差找转场段。
  模拟器上玻璃一帧上百毫秒，转场只能看对不对，看不出顺不顺。
- Android 平板 / 折叠屏（2026-11-01）：android-35 google_apis 的 Pixel Fold 模拟器 `adb emu posture 2` 后 `device_state` 显示 HALF_OPENED，
  但 `WindowInfoTracker` 给 App 的 displayFeatures 是空的（换 android-37 ps16k 镜像冷启动卡在 offline）：半折截图用 debug 包的 `debug.posture`（假铰链），
  合上 / 展开（`emu posture 1` / `3`）是真的切屏。折叠屏有两块屏，`screencap -p` 先输出一行多屏警告，PNG 要从 `\x89PNG` 开始截。
  带硬件键盘的模拟器不在触摸模式，Compose 把初始焦点给第一个输入框、软键盘自己弹出来：`MainActivity` 用 `stateAlwaysHidden`。
  `NavDisplay` 传单个 `sceneStrategy` 的重载和转场 lambda 一起写类型推断不出来，用 `sceneStrategies = listOf(...)`；这套版本里 `ExitTransition.KeepUntilTransitionsFinished` 是 internal。
- Android 各页面自己套的 `JarvisTheme` 默认读 `isSystemInDarkTheme()`，只在根上传 `darkTheme` 盖不住它们：debug 固定深浅色要改 `LocalConfiguration` 的 uiMode（`MainActivity.AppearanceOverride`），否则深色截图里会混着浅色的页面。
- Kotlin 的尾随 lambda 也只匹配最后一个函数参数：给 `AppNavigator` 新加一个可选的函数参数后，原来 `AppNavigator(...) { 按会话分 tab }` 的调用悄悄传给了新参数。新加的函数参数放最后，调用处一律写参数名。
- Compose 里会变的透明度别用 `Modifier.alpha`：值为 1 时不加图层，切换时修饰符链中间增删一层 graphicsLayer；排在 `clip` + `background` 后面时，Compose UI 1.12.1 删层后不再画那段背景（Android 的 `JButton` 由禁用变可用只剩一行字）。设计系统用 `Modifier.stableAlpha`（`docs/android.md` 经验）。
- material3-adaptive 的 `ListDetailSceneStrategy.detailPane()` 每次返回新的、没有 `equals` 的 metadata，拿它做相等比较永远是 false（Android 双栏左栏的高亮因此从来没亮过）：判断详情页按路由类型（`ShellPanes.kt`）。
- Compose 里给容器加 `height(IntrinsicSize.Min)`（为了让左边的色块撑满高度），里面一旦有懒加载列表或 `BoxWithConstraints` 就直接崩
  （「Asking for intrinsic measurements of SubcomposeLayout layouts is not supported」，看板风格的飞书会话踩过）：贴边的色块用 `drawBehind` 画（`Modifier.holder`）。
- Haze 的 `GlassStyle.regular` 跟随**系统**深浅色，不看 App 自己的深浅色：调试固定深浅色时要连 `LocalConfiguration` 的 uiMode 一起改（`MainActivity.AppearanceOverride`），否则深色页面上出现浅色玻璃。
- `SwipeToDismissBox` 的 `backgroundContent` 不滑动时也在画：玻璃风格的半透明行下面会透出滑动操作的颜色（收件箱整行发绿），只在 `dismissDirection` 不是 `Settled` 时画（`JSwipeActions`）。

## 系统能力

- 用 `startForegroundService` 拉起的服务，在调 `startForeground` 之前被停掉（自己 `stopSelf`、外面 `stopService`），系统直接让 App 崩在 `ForegroundServiceDidNotStartInTimeException`。负载高时这段空档变长，配对时一闪的「未配对」就会撞上：`HubService` 一律先进前台再决定去留，只在已经进了前台时才停它（`ForegroundStarts`，`docs/android.md`）。

## 包名迁移与升级兼容

- 产品 / Kotlin 包名改为 Persona / `persona.*` 时，不只要查显式写死的包名（2026-11-03，#277）：`kotlinx.serialization` 的 sealed 子类和导航 `NavKey` 默认类型名也可能包含包名。旧 `shell-position.json` 中的 `ChatRoute.source` 等值会因此反序列化失败。已经持久化的类型应固定旧 `@SerialName`，回归测试用旧 JSON 验证恢复，不能只测新版本自己序列化再读回。
- WorkManager 会持久化 Worker 的 FQCN；已有 PendingIntent 和桌面启动项会记住 `ComponentName`。迁移要保留旧 Worker / receiver 包装和必要的 activity-alias，让旧组件继续到新实现；常驻服务保持单一系统组件身份，避免新旧服务同时运行。不要仅为包名改动重建、取消已有任务或要求用户重新配对。必须另测旧 APK → 新 APK 的升级、待运行任务、通知按钮、桌面启动、分享入口和已有外壳位置恢复。
- 文档和源码静态回归不等于 Android 构建 / 真机升级验证。JDK 21 不可用时，Gradle toolchain 的 `MAC_OS/aarch64` 下载失败是环境阻塞，不能记成构建通过；以上升级场景仍需在可用的构建环境和设备上核验。

## 网络

- OkHttp 对自签证书（我们按公钥钉扎、自己的 `X509TrustManager`）给的 `response.handshake.peerCertificates` 是空的，拿响应去核对「对方出示的是哪台 hub 的指纹」做不到。组内多台 hub 共用一个钉扎客户端时，在主机名校验器里按 host:port 记下实际出示的指纹，再按候选所属的 hub 核对（`HubHttpClient`，`docs/android.md`「多 hub」）。
- 两台 hub 的 id 空间是独立的：交互 id、会话 id 都可能一样（假 hub 的夹具完全一样）。合并列表时只给机器 id 加组后缀不够，「等你定」的 LazyColumn 用 `request:<交互 id>` 当 key，两组同 id 直接崩（`Key … was already used`）；交互 id 也要带后缀，作答时去掉。
- 一组的数据在页面还开着它的对话时关掉（合并、断开），对话流结束时 `SessionItemsCache.release` 在已关闭的 Room 上开事务，抛「Database is closed」，从 LaunchedEffect 里冒出来直接崩（模拟器上合并时踩过）。现在：先把这组从列表里拿掉、等页面换走再关库；`release` 在库已关时什么都不做；门面里组已关时的失败改成取消。以前只有一份配对时重新配对也有同样的竞态，只是很少撞上。
- 多 hub 的会话键：给非主组的机器 id 加 `~<组 id>`，而不是给 `SessionKey` 加字段。从路由、深链、通知、旧数据构造键的地方太多，漏一处就串组；后缀编进机器 id，哪里都带着。代价是每个带机器 id 的接口都要在门面里翻译（`GroupNames`），新接口别忘了（`docs/android.md`「多 hub」）。

## 模拟器与真机

- 模拟器在充电状态下 `settings put global low_power 1` 不生效（省电模式开不了）：先 `dumpsys battery unplug`，测完 `dumpsys battery reset`。
- `adb shell am start … --es k "jarvis://open?a=1&b=2"`：adb 把参数拼成一行交给设备上的 shell，`&` 会把命令截断，链接要再套一层单引号（`scripts/android.sh` 的 shots）。
- ColorOS 上 `adb install` 要在手机上点确认「USB 安装」：手机熄屏或没人点时 adb 一直卡住，几分钟后报失败。装到用户手机上要先让用户在手机旁边。
- 逐像素截图（`android.sh shots` 的 `SHOTS_*`，`docs/android.md`「改前改后逐像素比对」）：Android 15 的 SystemUI 演示模式隐藏移动网络后约 5 秒会冒出一个卫星图标，演示命令和 `icon_blacklist` 都去不掉，只能等它出现后再截（每张图都带着它）；`adb root` 会重启 adbd，之前的 `adb reverse` 跟着没了，要先 root 再转发；macOS 的 `sips -Z` 直接改原图，看图前先拷一份；macOS 自带 bash 3.2 在 `set -u` 下展开空数组报错，写成 `${arr[@]+"${arr[@]}"}`。
