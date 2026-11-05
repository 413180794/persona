# 经验：Android

## 完成边沿要保留本轮运行证据（#2124）

只比较相邻快照的 Working → Unread 会漏掉 Working → NeedsYou → Unread；同一轮工作中途等待提问或授权后，仍可能直接完成。保留本轮运行标记穿过 NeedsYou，在完成时消费；Idle、移除和断线时清除。rev 单独变化不能当作新完成，旧未读版本也不能因解除静音补播。

启用提示时，磁盘缓存中的 Working 不是当前运行证据；但把第一个实时事件一律当基线，又会漏掉已有在线工作恰好第一次完成。复用已有的已验证列表建立基线，订阅屏障先注册事件，再同步读权威快照；不为提示另起轮询或 WebSocket。重连不可观测窗口保持不猜测补播。

新的多模块安卓 App（`android/`，`docs/android.md`、`docs/android-design.md`）踩过的坑。主线旧 App（单模块）的经验在 `docs/android.md`「经验」的「主线旧 App 的经验」一节。

- 插件提醒 `notify.kind=alert` 没有交互编号，同一会话可以有多个不同提醒；通知 tag 要逐条区分，不能沿用完成提醒的 `kind:machine/session` 覆盖规则（#1654）。空的机器或会话标识表示普通提醒，不要拿它刷新会话列表或生成空会话深链。展示前在交付和构建通知两处核对 `input` 偏好，避免异步刷新期间关闭开关后仍弹出提醒。

## 界面与 Compose

- `activity-alias` 不继承目标 Activity 的 metadata（2026-11-03，#758）：保留旧 launcher alias 后，读取 `componentName` 会拿到 alias 的空 metadata，DEBUG 启动适配器因此不加载，假 hub、风格与深浅色参数都被忽略。查询 canonical `MainActivity` 的 metadata，保留原 key 和唯一 launcher；Release 仍不注册 DEBUG 适配器。截图脚本必须核对配对状态、实际 mock 请求和每个子进程退出码，不能把欢迎页或后续 `printf` 的成功退出算作截图验收。

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
- 玻璃风格的 `ComposerFrame`（`floatingSurface`）按 `LocalGlassSource` 采样它下面的内容：放进 `JSheet` 时 `LocalGlassSource` 还是页面的，输入框里透出浮层后面那一页的字（收件箱的「改一改」、邮件回复浮层都这样）。浮层是不透明的，内容外面套 `CompositionLocalProvider(LocalGlassSource provides null)`，输入框就画成实心（`InboxDraftSheet`）。以后要在 `JSheet` 里统一处理可以在组件里置空。
- 晨报的 `surface` 是卡片灰，输入框和 chip 也拿它填底、没有边框：底部面板原来也是这个灰（`surfaceOpaque`），面板里的输入框和 chip 就看不见了（交给 AI、改一改、AI 草拟）。晨报的 `JSheet` 改用纸白（`background`），和页面一样。
- `SaveableStateHolder.SaveableStateProvider(key)` 里面是 `ReusableContent(key)`：key 一变整棵子树重建，焦点、滚动位置、`remember` 都没了。key 里别放随输入变的值：收件箱曾把搜索词和折叠的分组放进 key，搜索框每打一个字就丢焦点（后面的字也丢了），折叠一组就跳回顶部（`InboxScreen`）。
- `SwipeToDismissBox` 的 `backgroundContent` 不滑动时也在画：玻璃风格的半透明行下面会透出滑动操作的颜色（收件箱整行发绿），只在 `dismissDirection` 不是 `Settled` 时画（`JSwipeActions`）。
- Phosphor 线条图标（`X` 等）的 fill 字重是「实心方块里镂空一个 X」：看板风格里 `JIcon(selected = true)` 取 fill，照片角上的移除叉号画成了白方块。叉号这类别给 `selected`，看板自动用 bold。
- 玻璃背景每变一帧，上面所有浮层（顶栏、悬浮坞、输入框、滚动边缘）都要重新模糊，代价远大于画背景本身：状态光动起来以后（#959），模拟器上 Persona 页空闲时
  24 fps 重画占一个核的 p50 12%，降到 12 fps 后 p50 7.4%（各 15 次采样）。慢速的装饰动画按「看不出步进」的最低帧率更新（帧时间量化后写状态，
  只在 draw 里读），不要跟着屏幕刷新率跑；省电模式下停掉。
- `JSegmented` 不能放进 `horizontalScroll`：看板风格的每一格是 `weight(1f, fill = false)`，横向滚动给的宽度是无穷大，格子全缩没了，只剩一条竖线（插件页的 `segmented`，#1451，用 Robolectric 截图才看出来，玻璃和晨报正常）。要横排就让它拿父级的有限宽度。
- 插件画的界面可以在 JVM 上截图：`feature:settings` 的 `PluginSlotsShotsTest` 用 Robolectric 的原生绘制（`@GraphicsMode(NATIVE)`）`captureToImage()` 存 PNG，中文字体正常，不用模拟器；新做的纯 Compose 组件照这个办法先看一遍。
- Compose 测试里 `Modifier.clickable(enabled = false)` 仍带 OnClick 语义动作，`assertHasNoClickAction()` 断言不了「不可点」，要用 `assertIsNotEnabled()`（`ComposerPartsTest`）。

- 匿名 `BaseInputConnection` 里写 `override fun getEditable() = editable`，外层类的字段也叫 `editable`：Kotlin 先找到的是这个对象自己的 `getEditable()`（Java getter 对应的属性），于是自己调自己，输入法一连上（`finishComposingText` → `getEditable`）就栈溢出崩溃（终端键盘，2026-11-02）。外层字段别和框架 getter 同名（改叫 `buffer`）；这种问题 JVM 单测测不出，要在模拟器上真点一次键盘。
- 终端页上方的提示卡（「正在接管…」）一出现、一消失都会改终端的高度，接管时窗格因此多重排两次，攒下的滚出行还会重复：会来回变的状态写在标题下的一行里，终端的高度只随键盘变。

## 终端与假 hub

- 假 hub 的「只看」订阅以前不停掉接管时的那条流：两个 epoch 的帧交错到达，App 每次都判成缺帧重订，假 hub 每次重订又从头放一遍录制，帧越来越多，几十秒后 App OOM（`MOCK_TERMINAL_INTERVAL=0.05` 时很快）。真 hub 一个会话只有一条订阅，新订阅取代旧的；假 hub 现在也这样（`drive.clear()`）。
- 换了假 hub 进程（`HUB_ID` 每次随机），已配对的 App 健康检查对不上 hub 身份，一直「连不上 hub」：重启假 hub 后要 `pm clear` 再走一次 `debug.mockAddress` 配对。
- `./gradlew --stop` 停的是这台机器上同一 Gradle 版本的所有 daemon，几条车道（worktree）共用时会把别人的构建一起停掉；只停自己的构建用 Ctrl-C 或 kill 自己那条命令。

## 测试

- 设置入口迁移的截图不能只手工拼同名的 `JRow`（2026-11-05，#2196）：旧渠道截图自己画了「扩展 → 渠道」，设置页改了也不会发现位置错误。用最小 Fake 服务画真正的 `SettingsScreen`，传入导航回调，核对独立分节、上下顺序、点击目标、双栏选中、能力缺失与体验模式；移除「认识 Persona」入口时先找到实际文案（安卓为「助手」分组里的 `Persona`），不能误删主对话或仍被顶栏／启动器使用的资料页。Robolectric Native 的图片须实际打开检查，并明确不是模拟器截图。清理包装的 imports 后，自动 merge 最新 master 也要复编：另一个 PR 可能让留下的页面新用到原本只供包装用的依赖（#2192 的主对话迁移回调重新使用了 `LocalAssistantHost`），Git 没有文本冲突不代表依赖仍完整。

- 2026-11-05，#1881：只验证契约模块或扫描硬编码 token，不能证明新增页面能编译。Agents 的 hub 提示行同时出现 `Set.takeLast` 和不存在的尺寸 token；修好生产编译后，原测试夹具还因引用旧接口和继承 final 类而不能编译。
  有序去重集合先转成列表再截取最近 8 项，验证重复键不会增长或重排历史；加载圈沿用 `JSpinner` 与已有设计系统尺寸，不造页面里的数值或修改生成的 token 文件。Fake 按当前接口实现，`AgentsPreferences` 用真实构造函数和内存 DataStore，不把正式类型改成 open。
  `WhileSubscribed` 的 StateFlow 要有活跃收集者；测试安装 Main dispatcher，先启动订阅再发送不重放的 hello，并推进调度器后检查状态；结束时取消模型／订阅并恢复 dispatcher。实际模拟器还暴露了尚未收到 hello 时误报「hub 版本太旧」的时序问题：没有 hello 不是协议为零，调用方要等 hello 到达后再判断兼容性；真正收到旧协议仍提示。
  保留原断言，另用原生绘制和实际 App 截图检查三种风格 × 深浅色，最后跑完整 Android 测试和 APK 构建。原失败日志与退出码单独保留，不能把测试编译失败写成已运行的测试失败。

- 读取仓库源码的 JVM 测试不要把 `File(".")` 当仓库根目录（2026-11-04，#1672）：Gradle 的 `:app:testDebugUnitTest` 在 `android/app` 下运行，重复拼接 `android/app/...` 会在文件存在检查处失败，真正的契约断言根本没有执行。沿当前目录的父目录寻找约定的源码文件，用合成目录验证仓库根、Android 根、App 模块三种入口以及缺失文件／同名目录；不全局改测试工作目录、不跳过原契约断言。原发布失败记录保留，修复后真实运行 Gradle 测试和打包。

- `RealInboxRepository` 构造时在作用域里起一个收集当前配对的协程，它第一次运行会把列表清空（换配对时的清场）。`runTest` 里构造完直接 `refresh()`，清场排在第一次挂起之后跑，刚读到的列表又被清掉：先等 `snapshot.first { it.contentSource != null }` 再刷新（`InboxHandOffRepositoryTest`）。
- `RealInboxRepository` 的测试用尾随 lambda 传最后一个参数 `download`：新加的构造参数要放在它前面（带默认值），否则尾随 lambda 对不上。

- Navigation 3 的页面级 ViewModel（`rememberViewModelStoreNavEntryDecorator`）在页面出栈时随 ViewModelStore 一起清掉：页面里 `viewModelScope.launch` 的收尾（比如清掉新建 Agent 的启动记录）如果排在 `navigator.back()` 之后，常常还没跑就被取消，下次打开又说「上次启动的结果待确认」。要先在 `LaunchedEffect` 里把收尾做完（挂起函数），再离开页面（`NewAgentDestination` 的 `acknowledge()`）。

- `JFullScreen` 是一个 Compose `Dialog`，有自己的窗口：App 统一的提示（`LocalAppFeedback` → `Root.kt` 的 `SnackbarHost`）画在 Activity 的窗口里，整页弹层开着时会被盖住，用户看不到。弹层里操作的结果要在弹层自己的窗口里提示（看图的查看器、`SpaceScreen` 都自带 `SnackbarHost`）；只有弹层随即关掉时的那一条（「已加到输入框，补一句话就能发」）走 `LocalAppFeedback`。读代码确认，2026-11-02 写看图时没有上设备看。
- `JFullScreen` 先铺一层不透明的页面背景，弹层里把内容拉走时透不出下面的页面：查看器给它一个 `background` 透明的色板（`LocalPalette`），在里面换回原色板、自己画会变淡的背景。弹层里的玻璃浮层要有自己的 `HazeState`（`LocalGlassSource`），否则用的是从下面那页继承来的，采样的是另一个窗口的内容。
- Compose 的 `Modifier.border(0.dp, …)` 不是没有边：`0.dp` 就是 `Dp.Hairline`，照样画一像素的线（`BorderModifierNode` 里写明的）。「有时有边」要按条件加这个修饰符（编辑器的选色圈）。
- 图片解码用 `ImageDecoder`（按 EXIF 方向转正）而不是 `BitmapFactory`（不管方向，相机原图会横着显示）；要往里画、重新编码的位图用 `ALLOCATOR_SOFTWARE`，硬件位图不能画进软件 Canvas。存 JPEG 前把透明的地方铺白：JPEG 没有透明通道，编码器会把透明处变成黑色（`jpegBytes`）。
- `Offset` 是值类，按位比较：`coerceIn(-0f, 0f)` 会得到 `-0f`，和 `Offset.Zero` 不相等；没有余地时直接给 `0f`（`ImageMath.clampPan`），测试里比较算出来的浮点偏移用容差。

## 系统能力

- hub 的 `fleet` 只在变化时推（`liveactivity.go` 和上次广播的比较），刚连上的客户端什么都收不到：常驻通知、小组件会一直是 0，直到哪个 Agent 状态变了。连接每次连上都要 `GET /api/v1/fleet` 一次（`FleetFeed`）。
- 一个 PendingIntent 是不是「同一个」只看 action、data、组件和类别，不看 extras 和 flags：常驻通知上的「允许」如果和「需要你处理」通知上的用同一个 data URI，系统会把它们当成一个（一个可变带回复框、一个不可变），后创建的覆盖先创建的。常驻通知的按钮用自己的 `jarvis-notify://ongoing/…`。
- 前台服务的 `onStartCommand` 不只跑一次：App 每次回到前台都会再 start 它，每次都要 `startForeground`。那时用的通知要是现在显示着的那条（`HubService.shown`），不能按当前数据重新算：重新算的没有「只剩做完未看满 5 分钟退回连接行」这一步，已经收起的「1 个做完了」又回来了，而收集器看到的状态没变，不会再改回去，一直挂到 fleet 下次变化（2026-11-02 模拟器上复现）。
- 前台服务要停的时候别再更新它的通知：取消配对时连接状态、通知偏好先变（App 跟着发了两次「等待网络」），紧接着服务停下、`stopForeground(REMOVE)` 收走通知；系统处理通知是异步的（模拟器上用调用栈确认：两次 post 都在 onDestroy 之前），最后那次更新落在收走之后，成了一条普通的常驻通知，划不掉、也没人再管它（两次复现两次都留下）。`HubService.show` 在没配对时不再更新。
- 通知上要求解锁的按钮（`setAuthenticationRequired(true)`）：锁屏上点了、输完 PIN，SystemUI 马上就把按钮的 PendingIntent 发出来，这时 `KeyguardManager.isDeviceLocked` 还是 true（Android 15 模拟器单次观察，约 0.8 秒后才变 false）。收到就读它再拒绝的话，锁屏上的「允许 / 批准」每次都回「请先解锁」，一次也成不了；常驻服务的「恢复」也一样被悄悄吞掉，手机工具确认通知上的「允许」被丢掉、90 秒后按拒绝回给 Claude。要给解锁一点时间落定再判断（`lockedAfterUnlockSettles`，`:platform:notify` 和 `:platform:phonetools` 各一份，最多等 3 秒，一直锁着才算锁着）。
- 手机工具：hub 把各台手机同名的工具合并成一份定义（取说明更长的），调用却可能发给任一台声明了它的手机（`phoneClient`）。Android 的定义只要和 iOS 不一样，Claude 就会按 iOS 的参数调用 Android（新版曾经把超范围的 `limit` 当错误拒绝）：工具定义逐字照 iOS，用导出的夹具守着（`PhoneToolCatalogTest`）。

- 用 `startForegroundService` 拉起的服务，在调 `startForeground` 之前被停掉（自己 `stopSelf`、外面 `stopService`），系统直接让 App 崩在 `ForegroundServiceDidNotStartInTimeException`。负载高时这段空档变长，配对时一闪的「未配对」就会撞上：`HubService` 一律先进前台再决定去留，只在已经进了前台时才停它（`ForegroundStarts`，`docs/android.md`）。

## 包名迁移与升级兼容

- 产品 / Kotlin 包名改为 Persona / `persona.*` 时，不只要查显式写死的包名（2026-11-03，#277）：`kotlinx.serialization` 的 sealed 子类和导航 `NavKey` 默认类型名也可能包含包名。旧 `shell-position.json` 中的 `ChatRoute.source` 等值会因此反序列化失败。已经持久化的类型应固定旧 `@SerialName`，回归测试用旧 JSON 验证恢复，不能只测新版本自己序列化再读回。
- WorkManager 会持久化 Worker 的 FQCN；已有 PendingIntent 和桌面启动项会记住 `ComponentName`。迁移要保留旧 Worker / receiver 包装和必要的 activity-alias，让旧组件继续到新实现；常驻服务保持单一系统组件身份，避免新旧服务同时运行。不要仅为包名改动重建、取消已有任务或要求用户重新配对。必须另测旧 APK → 新 APK 的升级、待运行任务、通知按钮、桌面启动、分享入口和已有外壳位置恢复。
- 文档和源码静态回归不等于 Android 构建 / 真机升级验证。JDK 21 不可用时，Gradle toolchain 的 `MAC_OS/aarch64` 下载失败是环境阻塞，不能记成构建通过；以上升级场景仍需在可用的构建环境和设备上核验。

- 从相机、文件选择器这类整页的系统 Activity 回来，hub 连接常还在恢复（`readyToSend` 为 false），这时上传附件会立刻报「连接恢复后重试上传」，拍完照每次都失败。上传前先等 `readyToSend`（`ChatComposer`，最多 15 秒，配置值）。
- 上传别走共用 OkHttp 客户端的超时（读 15 秒、写默认 10 秒）：慢链路上 hub 已经收全了文件，客户端等回包超时，照片照样标「上传失败」（假 hub `MOCK_UPLOAD_KBPS=3` 复现，约 90 KB 的两张都这样）。现在上传单独给 10 分钟（`HttpChatComposerService`，和旧 App 一样）。

## 契约与装配

- 给接口加带默认实现的方法（`ChatActions.editQueue` / `info`、`AgentOperations.attachSession`）时，`by delegate` 的包装会自动转发，手写的转发对象不会：`core:data` 的 `trackActions`、App 的 `AgentsWiringModule` 里那个匿名 `AgentOperations` 都是逐个方法写的，漏一行新方法就悄悄走默认实现（「尚未接入」），编译不报错。加方法时 `git grep ': ChatActions\b\|object : AgentOperations'` 把手写的转发都补上。
- Claude Code 的 transcript 不留 `client_msg_id`（2026-11-03，#895）：用户那一行的 item id 是 Claude 自己的 uuid，hub 和 node 都不改写。hub 有 `client_ops` 时 Android 曾只按 id 认「对话里已经有这条」，发到 Claude Code 的消息全都一直挂着「已送达，等待对话记录」。待发消息按文字找对话里的那一条（`MessageEcho`，规则同 iOS 的 `PendingMessage.arrived`：归一化、电脑时钟最多慢 2 分钟、截断的按开头），只对发出了或结果不明的消息做，一条对话记录只认一条待发消息；聊天仓库每次拿到新 item（事件、翻页、补洞）都交给 outbox 对一遍（`HubChatRepository` 的 `transcript`）。
- 送达不等于对话里一定会出现一条一样的用户消息（2026-11-03，#950）：手机发 `/compact` 在 transcript 里只是一条「压缩上下文」通知，Codex 到轮次结束才写排队的消息。只等回显，气泡就永远挂着；iOS（`settleSend`）和网页都是送达 60 秒后、会话不在工作（最多再等 15 分钟）就收起，Android 同样（`OutboxMessageSender.sentGraceMillis`、`connectActivity`）。给构造函数加参数时别加在 `now` 后面：调用处把时钟写成尾随 lambda。
- 多 hub 下收件箱「交给 AI」的目标会话来自合并列表（带组后缀），要用按组路由的 `ConnectionRuntime.messages`，不能直接用某一组的 `data.messages`（#950）。
- 来源会话的 `read_only_reason` 有的 node 自己就以「仅查看：」开头，客户端再拼「 · 仅查看：原因」会出现两遍（iOS 也是这样拼的）：`SessionSource` 先去掉这个前缀。

## 网络

- OkHttp 对自签证书（我们按公钥钉扎、自己的 `X509TrustManager`）给的 `response.handshake.peerCertificates` 是空的，拿响应去核对「对方出示的是哪台 hub 的指纹」做不到。组内多台 hub 共用一个钉扎客户端时，在主机名校验器里按 host:port 记下实际出示的指纹，再按候选所属的 hub 核对（`HubHttpClient`，`docs/android.md`「多 hub」）。
- 两台 hub 的 id 空间是独立的：交互 id、会话 id 都可能一样（假 hub 的夹具完全一样）。合并列表时只给机器 id 加组后缀不够，「等你定」的 LazyColumn 用 `request:<交互 id>` 当 key，两组同 id 直接崩（`Key … was already used`）；交互 id 也要带后缀，作答时去掉。
- 一组的数据在页面还开着它的对话时关掉（合并、断开），对话流结束时 `SessionItemsCache.release` 在已关闭的 Room 上开事务，抛「Database is closed」，从 LaunchedEffect 里冒出来直接崩（模拟器上合并时踩过）。现在：先把这组从列表里拿掉、等页面换走再关库；`release` 在库已关时什么都不做；门面里组已关时的失败改成取消。以前只有一份配对时重新配对也有同样的竞态，只是很少撞上。
- 多 hub 的会话键：给非主组的机器 id 加 `~<组 id>`，而不是给 `SessionKey` 加字段。从路由、深链、通知、旧数据构造键的地方太多，漏一处就串组；后缀编进机器 id，哪里都带着。代价是每个带机器 id 的接口都要在门面里翻译（`GroupNames`），新接口别忘了（`docs/android.md`「多 hub」）。
- 关 OkHttp 客户端也会联网：`connectionPool.evictAll()` 关 TLS 连接时要写 close_notify，在主线程上就是 `NetworkOnMainThreadException`。配对在主线程的 `finally` 里关 `HubPairingClient`，这个异常在凭据保存之后才抛，盖掉了成功的结果：配对页说「连接未完成」，强制重启后其实已经连上（#955）。明文连接关闭不写网络，只走内网明文地址时一直没暴露。`HubHttpClient.close()` 现在把清池交给调度器自己的线程；自己写的客户端别在调用方线程上 `evictAll`。

## 模拟器与真机

- 测常驻通知时别用 `am force-stop` / `am start -S` 冷启动：系统把它记成用户强行停止（`ApplicationExitInfo.REASON_USER_REQUESTED`），App 下次启动就当成「后台连接被你停止了」，常驻服务不再起来，要在通知上点「恢复」。只想换个新进程用 `adb shell run-as <包名> kill -9 <pid>`（常驻服务会被系统很快拉起，`am start -W` 多半报 WARM）。
- 通知栏里有走着的计时（常驻通知只剩工作中时的 chronometer），`uiautomator dump` 一直拿不到 idle、直接失败：那时只能截图看。
- Pixel（Android 15）默认「锁屏上隐藏静默通知」：常驻通知用的「实时状态」频道是低重要性，默认设置下锁屏上根本不出现；看锁屏效果先 `settings put secure lock_screen_show_silent_notifications 1`，测完 `settings delete`。
- 模拟器在充电状态下 `settings put global low_power 1` 不生效（省电模式开不了）：先 `dumpsys battery unplug`，测完 `dumpsys battery reset`。
- 平板 AVD 拍手机布局：`wm size 1080x2400`、`wm density 420`，再 `settings put system accelerometer_rotation 0`、`user_rotation 0` 固定竖屏。这些写进 AVD 的数据、重启还在，用完要 `wm size reset`、`wm density reset` 并恢复旋转设置。
- `scripts/mock/mock_hub.py` 每次启动的 `HUB_ID` 都是新的：换 `MOCK_*` 开关重启假 hub 后原配对失效，要 `pm clear` 再配对（草稿随配对目录一起没了）。`adb reverse --remove` 断不掉已建好的连接（OkHttp 复用连接），模拟不了上传途中断网。
- `adb shell am start … --es k "jarvis://open?a=1&b=2"`：adb 把参数拼成一行交给设备上的 shell，`&` 会把命令截断，链接要再套一层单引号（`scripts/android.sh` 的 shots）。
- ColorOS 的 logcat 里看不到第三方 App 自己的 `Log.*`（只有系统打的行），真机上查异常拿不到堆栈：在模拟器 + 假 hub 上复现（要走 TLS 就 `mock_hub.py --tls-port`，配对链接带 `fp` 和 `tls`），#955 就是这样找到的。
- ColorOS 上 `adb install` 要在手机上点确认「USB 安装」：手机熄屏或没人点时 adb 一直卡住，几分钟后报失败。装到用户手机上要先让用户在手机旁边。
- `debug.mockAddress` 是静默配对（`platformPairing.pair`，不经过配对页），配对完成之后的步骤（选择引擎、准备清单）都看不到：要看这些就用 `jarvis://pair?url=…&token=mock` 链接打开配对页，再点「确认配对」。
- 想让一次保存失败，别靠 `adb reverse --remove` 断掉假 hub：App 的 OkHttp 连接池里已经建好的长连接照样能用，请求还是成功了。用假 hub 的失败开关（`MOCK_ENGINE_ERROR`、`MOCK_MODEL_CONFLICT`、`MOCK_DEPLOY_ERROR`）。
- **用户已经配对着自己的 hub 时，别给 App 发 `jarvis://pair` 深链**：深链进的是根配对页，确认后 `HubDataStore.pair` 会丢掉原来的配对再写新的。
  加一台测试 hub 走 设置 › hub › 添加 hub（新的一组，原来的不动）。准备清单没走完时，深链和添加 hub 的配对页都会先停在准备清单上，链接被忽略。
- **`adb shell input text` 遇上中文输入法会打乱**：微信输入法、拼音模式的 Gboard 会把字符重排、`?` 变成全角、吞掉 `=` 和 `_`、把字母转成汉字。
  要往输入框里打链接这类 ASCII，临时 `ime set` 到一个不处理按键的输入法（如手机上启用了的 `com.google.android.gms/.autofill.service.AutofillInputMethodServiceProxy`），
  打完马上 `ime set` 回 `settings get secure default_input_method` 记下的原值。配对页的地址框一出现 `jarvis://` 开头就立刻解析，长链接先打后半段、最后在行首补 `jarvis://`。


## 语音对话

- OkHttp 的 WebSocket 收到对方的关闭帧后只调 `onClosing`，监听器自己不 `close()` 的话连接一直半关着，`onClosed` 永远不来；MockWebServer 也因此关不掉（「Gave up waiting for queue to shut down」）。`onClosing` 里要回一个 `close`（`HttpVoicePlugins`），关闭码用 1000：1005 这类保留码传给 `close` 会抛异常。
- 模拟器按规矩用 `-no-audio` 启动，`AudioRecord` 开不了，语音页只会显示「麦克风打不开」：截图用调试参数 `debug.voiceAudio fake`（假音频只在 debug 源码集里）。
- 假 hub 每次启动都换一个 hub_id，重启后 App 的配对按 hub 身份校验就失败了：要换假 hub 的行为，用运行中能切的开关（`/mock/voice?mode=…`），别重启它。
- 模拟器上玻璃一帧上百毫秒，状态变了以后颜色动画要过一两秒才走完：按时间截图时离状态变化留 2 秒以上，否则截到的是上一个状态的颜色。`uiautomator dump` 每次要一两秒，也算进去。
- `uiautomator dump` 只看得到最上面那个窗口：底部浮层（ModalBottomSheet）开着时，下面页面的按钮都找不到，先关浮层。
- Phosphor 的 fill 粗细不全是「实心的同一个图形」：`waveform` 的 fill 是一个圆角方块里挖出波形，放在实心圆盘上像多了一个白方块。圆盘里的图标用 regular。

## 宽屏两栏和 Persona 页

- material3-adaptive 1.3.0 的 Navigation 3 list-detail 场景（`rememberListDetailSceneStrategy`）可以接分隔条：传 `paneExpansionState` 和 `paneExpansionDragHandle`。但库的 `Modifier.paneExpansionDraggable` 松手时调 `settleToAnchorIfNeeded`：有锚点就一定吸到最近的锚点，没锚点就停在松手处，做不到「离折痕 28dp 以内才吸」，`firstPaneWidth` 等读数又是 internal 的。要自定吸附和收起，就自己写把手的 `draggable`，每帧 `setFirstPaneWidth(px)`（只触发两栏重新测量，不重组页面），松手后用 `animate` 一帧帧写。另外 expansion state 一旦设了宽度（不是 unspecified），就盖过 `PaneScaffoldDirective.excludedBounds` 的折痕避让：分开的折痕要 `clear()` 并且不传把手，交还给库按折痕分栏（源码 `ThreePaneScaffold.kt` 的 measure）。读这类库的行为直接下 Google Maven 的 `-sources.jar` 看源码最快。
- 根页底栏和键盘（#867）：外壳给根页留了 dock 的高度（玻璃风格是 `ChromeInsets.bottom`，晨报 / 看板是 Column 里 dock 占的那一行），页面自己又 `imePadding()` 了整个键盘的高度，键盘弹起后输入框悬在键盘上方一个 dock 那么高（vivo 折叠屏实测约 460px）。dock 留出的地方要算成已经消费的 inset：玻璃风格在根页外包 `consumeWindowInsets(PaddingValues(bottom = …))`，Column 里的根页用 `recalculateWindowInsets()`（按自己的位置重算，dock 那一截不再算进它的 IME inset）。这样 `imePadding()` 只补 dock 之外的那一截键盘，随动画连续变化，不用读 IME 高度、也不用在弹起时藏 dock。验收看输入框底边和键盘顶边的距离，三种风格都要看。
- 点空白处收键盘（#2626）：`JPage` 的内容区和 `JSheet` 挂 `dismissKeyboardOnBlankTap()`，在 Final 阶段确认点按未被子控件消费后清焦点并隐藏输入法，自身不消费事件，链接、按钮、长按、滚动照常处理。不经过 `JPage` 的分享弹层单独接入。`ComposerFrame` 三种风格都用 `keepsKeyboardOnTap()` 消费子控件未处理的内边距点按：空的 pointer 修饰符只能挡住下层兄弟，嵌套 `JPage` 的祖先监听器仍能看到未消费事件。回归测试要包含嵌套页、真实链接回调和实际滚动位移。只要长按的东西（消息的长按菜单）别用 `detectTapGestures(onLongPress = …)`：它把每次短点按的 down 和 up 都消费掉，Agent 回复又是整行宽，助手对话页几乎点哪都「已消费」，键盘不收（#3056）；改用设计系统的 `onLongPress(key) { … }`，只在长按触发后才消费。测试要拿真实的消息行（`MessageContent` + 长按菜单），自己画个 `detectTapGestures` 的卡片测不出来。
- `setFirstPaneWidth(0)` 时库只摆第二栏（列表还在组合里、大小为 0），把手放在左边缘（离边 `horizontalPartitionSpacerSize / 2`），所以收起后把手还在，可以拖回来。
- hub 的 pins（`/api/v1/pins` 的 sessions）同时装着 Agents 的置顶会话和 Persona 的置顶对话：Persona 历史里「全部取消置顶」只能取消 Persona 自己的对话，不能把整份 pins 清掉。
- 交接对话的标题要先认 `<jarvis-context … title="X">` 再截断：旧 App 先把最后一句截到 30 个字再认标签，长标题的标签被截断后就认不出来，历史里露出半截原始标签。


## 文件和 Git

- 一行里两个 `weight(1f)` 会平分剩余空间：`JSectionHeader` 曾让标题（`weight(1f, fill = false)`）和后面的 Spacer 各占一份，`trailing` / `action`（收件箱的「收起」「全部看过」）停在中线附近，长标题在半宽处就截断。现在标题和计数包在一个 `weight(1f)` 的 Row 里，右边的东西贴着行尾；要把东西推到最右，只给左边一个 weight。Git 页「改动」的 +N −N 要和标题按基线对齐，自己套 `Row`：标题 `Modifier.weight(1f).alignByBaseline()`，右边的字 `alignByBaseline()`。
- 两个模块都在清单里声明 `androidx.core.content.FileProvider` 会在合并时撞名（provider 按 `android:name` 合并）：每个模块用自己的子类（`FilesShareProvider`、`InboxFileProvider`），authority 各起一个。
- mikepenz Markdown 的 `LazyMarkdownSuccess` 自己建 LazyColumn，前面加不了别的行：长文档要在 `Markdown(success = { … })` 里自己写 LazyColumn，每个顶层块一行 `MarkdownElement(node, components, content, includeSpacer = index > 0)`。三个坑：`MarkdownElement` 把块间距画成自己前面的一个兄弟 Spacer，外面包 `Box` 会把两者叠在一起（间距全没了），要包 `Column`；顶层子节点里的 EOL / 空白和链接引用定义（`[api]: https://…`，什么都不画）要先滤掉，否则平白多出空隙；`ImageTransformer.intrinsicSize` 按像素给，README 里的图会按像素大小画（高密度屏上很小），乘上 density 才和浏览器一样按 dp 显示。
- `PdfRenderer` 只能读文件描述符（base64 先写进 cache），而且同一时间只能开一页：渲染要加锁，关掉之后别再画（`PdfDocument`）。
- 跨模块的属性不能 smart cast（`SessionFileContent.mime` 在 `core:contracts`）：先赋给局部变量再判空。

## 对话内容

- Codex 的文件改动在 hub 里变成只带 `diff` 的 `Edit`（`server/internal/transcript/codex.go`），但 `diff` 不一定是 unified diff：FileChange 没有 `unified_diff` 时（新建文件）hub 放的是整份文件内容，没有 `@@`。按 unified diff 解析会把整份内容丢光（不以 `+` `-` 空格开头的行都被当成文件头跳过），所以先看有没有 `@@`，没有就按 `change` 画成全部新增或全部删除（`patchDiff`）。
- 对话页给每一行的能力（`LocalChatRowActions`）要在流式更新时保持同一个对象：读它的行（几乎每一行）会在它换对象时全部重组。会随消息变的东西（这段对话的图片列表）做成打开时才读的函数，不要放进 `remember` 的键（`ChatScreen` 里的 `pictures`）。

- 逐像素截图（`android.sh shots` 的 `SHOTS_*`，`docs/android.md`「改前改后逐像素比对」）：Android 15 的 SystemUI 演示模式隐藏移动网络后约 5 秒会冒出一个卫星图标，演示命令和 `icon_blacklist` 都去不掉，只能等它出现后再截（每张图都带着它）；`adb root` 会重启 adbd，之前的 `adb reverse` 跟着没了，要先 root 再转发；macOS 的 `sips -Z` 直接改原图，看图前先拷一份；macOS 自带 bash 3.2 在 `set -u` 下展开空数组报错，写成 `${arr[@]+"${arr[@]}"}`。
- 用 Homebrew 装的 Android SDK 在 `/opt/homebrew/share/android-commandlinetools`（不是 `~/Library/Android/sdk`），默认 Java 是 JDK 8：跑 `android.sh` 前设 `ANDROID_HOME` 为它、`JAVA_HOME=/opt/homebrew/opt/openjdk@21/libexec/openjdk.jdk/Contents/Home`。自建 AVD 用单独的 `ANDROID_AVD_HOME`，别动别人在跑的模拟器。准备清单（首次启动）挡着时，先跑一遍 `android.sh shots`，它会清数据、配对、跳过准备，之后用 `DebugEntryActivity --es debug.mockAddress …` 换到自己的假 hub。

- 多 hub（#634）：新接一个按机器 id 调用的服务时，别用「当前配对」的连接（`ConnectionRuntime.scopedData` / `connectionFor`）。页面拿到的机器 id 来自合并列表，别的组的带 `~<组 id>`，发给选中组的 hub 只会「找不到机器」，主组和别的组恰好有同名机器时更糟：改到了另一台。
  按 `GroupStacks.use(GroupKeys.namespaceOf(id))` 找组、在组里用 `inGroup(names)` 去掉后缀、把返回的机器再加上后缀；只有一台 hub 一份的（模型配置、语音、收件箱）才跟选中的组。
- 历史页也要触发 transcript 对账（#899）：末尾 item ID 不变并不等于对话没有新增记录。向前加载旧消息和补洞都会在末尾不变时增加用户消息；outbox 的 transcript 回调须按完整记录变化触发，不能只看最后一条，否则早期已送达气泡要到重开页面后才可能消失。

## 连接和 TLS

- 握手失败不等于证书变了（#1250）：hub 重启那几秒，Docker Desktop 的端口代理和 `adb reverse` 会先接下 TCP 连接、再因为后端不在而断开，握手中途断开也抛 `SSLHandshakeException`。原来凡是它都归成「hub 的证书变了」且永不重试，App 要杀进程才连得上。
  只有拿到了证书、校验没过才算证书不符：`SpkiTrustManager` 拒了（异常链里有 `CertificateException`），或 hostname verifier 拒了（组里别的 hub 的 pin，`SSLPeerUnverifiedException`）。其余握手失败按普通网络错误退避重试（`HubFailure.from`）。
- `OkHttpHubConnection.request` 遇到非 2xx 自己就抛 `HubFailure`，不会把响应交回调用方：在调用方里 `if (!response.isSuccessful)` 再读错误正文，用真实连接时永远走不到（MockWebServer 的单测直接返回响应，测不出来）。hub 错误正文里要用的字段（如插件设置被拒的 `field`、`message`）得进 `ErrorBody` / `HubFailure`（2026-11-04，#1206：被拒的字段在模拟器上显示成了页顶错误）；单测里用一个和连接一样先抛的 request 再测一遍（`HttpPluginManagementTest`）。

## 模块门面别漏掉接口的默认方法（2026-11-04，#1619）

- `InboxModule` 的 `InboxRepository` 门面（`object : InboxRepository`）把调用转发给当前激活组的 `it.inbox`。
- 如果接口声明了带默认实现的方法（例如 `momentsPost` 默认 `error(...)`、`emojis` 默认 `null`），Kotlin 编译器不会报错要求实现，直接落到默认值；页面调用时就表现为功能不可用（如公司圈帖子打不开）。
- 做法：门面实现时完整实现接口的所有方法，并加契约单测检查。


- herdr 的 `herdr_running` / `herdr_running_known` 是 node.info 字段，hub 的普通 `machines` 快照没有转发它们。Android 要从 `GET /api/v1/machines/{mid}/herdr` 读 `{autostart, running, known}`；`known: false` 表示未知。显式启动后用确认的结果更新提示，并防止更早的状态查询覆盖它（#1916）。


## 悬浮窗 addView 后还没有 attach（#2124）

冷启动后快速回桌面，会在窗口注册后、首次绘制 attach 前触发配对重建或隐藏。`WindowManager.addView()` 已经注册成功时，
`View.isAttachedToWindow` 仍可能为 false；只按 attached 决定是否移除，会留下等待 attach 的窗口，然后它带着已 DESTROYED 的 LifecycleOwner
进入 Compose，报 `Cannot configure ComposeView to disposeComposition at Lifecycle ON_DESTROY`。

宿主单独记录注册成功，隐藏时先 `removeViewImmediate` 撤掉已注册的窗口，再 dispose composition 和销毁 owner。清理先清本地注册标记，
避免重入重复移除。模拟器验收应包含保留 always 设置的冷启动、马上 Home、重新打开与关闭，不只测停稳后的拖动和点击。


## Compose 浮层的头部拖动（#2124）

Compose 的可点击内容可能调用 `requestDisallowInterceptTouchEvent`，父 FrameLayout 的 `onInterceptTouchEvent` 不一定能接管拖动；
空的 Compose 拖动检测器还会和窗口手势竞争。窗口层在向 Compose 分发前，用 raw 屏幕坐标识别头部拖动；超过触摸阈值时向子控件发送一次 CANCEL，
再移动 WindowManager 的窗口。拖动期间暂停按旧停靠位置的布局校正，松手才保存新的边和高度比例。验收要核对实际窗口 frame 和持久化位置，
不能只看截图以为拖动成功。


## 浮层 IME inset 的坐标范围（#2124）

WindowInsets 回给浮层 View 的 IME bottom 可能只是该窗口与键盘的重叠量，不能直接从整屏高度减去它。
现场 display 高 2400、键盘 top 1517，旧卡片 bottom 2305，输入框和发送按钮被遮住。
优先使用 WindowMetrics 的全屏 IME inset；View 局部 inset 要先按窗口屏幕位置与高度换算，再用于屏幕边界。
窗口已移到键盘上方时局部重叠会变为零，IME 仍可见时保留已知边界，明确隐藏时才清除。
卡片的任务 / 扩展独立滚动，输入框和发送固定在底部，验收核对整块卡片 bottom 在键盘 top 以上，并实际点击发送。

## 胶囊的窗口尺寸与入口语义（#2124 紧凑化反馈）

活动摘要曾把收起胶囊撑到 248 dp 宽，“优先状态数 / 活跃总数”的比例又容易被误解为未读数或会话位置。收起只保留小形象和提醒，具体数量放在含义明确的“未读 / 主对话 / 收件箱”入口；未读使用现有 seen 版本投影，工作中与完成未看分开。

浮层体积由 WindowManager.LayoutParams 和 Compose 内容共同决定；只缩 Compose 里的头像或 maxWidth 仍可能留下宽大的透明触摸区域。修改收起尺寸时同步调整原生窗口宽度、拖动头部高度、展开最大高度，并在其他 App 上验证实际边界和输入法位置。

## 没有 Android Studio 的 Linux 机器上编译和跑单测（2026-11-04，#984）

- **JDK 21 工具链**：构建要 JDK 21，命令行的 `-Porg.gradle.java.installations.paths=` 只给根构建，`build-logic` 这个包含构建看不到，
  报「Cannot find a Java installation … languageVersion=21」。把它写进 `GRADLE_USER_HOME/gradle.properties`（用一个临时的 Gradle 用户目录，不改机器上的 `~/.gradle`）。
- **SDK**：新的 cmdline-tools 说 `--licenses` 已经不需要了，直接 `sdkmanager "platforms;android-37.0" "build-tools;37.0.0" "platform-tools"`。
  compileSdk 37 对应的包名是 `android-37.0`，不是 `android-37`。
- **Robolectric 运行时自己下 android-all**：不走 Gradle 的仓库，Maven Central 不通时 `AgentRowTest` 这类用例报 `MavenArtifactFetcher` 的 `ConnectException`。
  用 `JAVA_TOOL_OPTIONS=-Drobolectric.dependency.repo.url=<镜像>` 传给测试进程。
- **单测读仓库根目录下的文件**（`jarvis.repo.root`）：`spec/fixtures`、`apple/PersonaTests/Fixtures/terminal-control-scroll.jsonl`。
  只拷 `android/` 过去跑，会多出几条 `FileNotFoundException`，看着像测试坏了。
