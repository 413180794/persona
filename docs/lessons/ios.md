# 经验：iOS

踩过的坑、怪异行为和当时的取舍（原来在 `CLAUDE.md`「经验」，索引见 `docs/lessons/README.md`）。新经验写进对应的节，没有合适的就新开一节。

- 共享视图引用的辅助扩展也必须编进每个目标：watchOS 10 兼容把 `Color.mix` 换成 `blended` 后，扩展若仍留在 `Theme.swift`，
  只编入 `PersonaFace` 的灵动岛小组件会在 Release 归档失败。辅助扩展随 `PersonaFace.swift` 编入每个目标；不要把整套 App 主题拖进扩展，也不要在两个文件里重复声明。

## 聊天列表与滚动

- 新外壳的侧边栏菜单、会话名和正文用同一档 Dynamic Type，不给整个侧边栏套 `.shellScaled()`：它会额外放大一档（标准设置下 17pt → 19pt），
  即使两边都写 `Theme.Font.body` 也不一样。「Jarvis」标题保留 `display` 层级，字号仍跟随系统和设置的小 / 标准 / 大。

- `List` 换序、上面插行或改行高后保持的是滚动偏移，不是读者看着的那一行（2026-11-02，#339）：停在 Agents 列表中间时一次推送让上面的空间换了名次，
  屏幕中间的空间整个换成了另一个，测试里那一行直接不见了。要保住就自己做：行用 `onGeometryChange` 报窗口里的位置，改数据前记下中间那一行，
  排完按差值改 `contentOffset`（`ReaderAnchor`）；在最顶上、手指按着或正在滑时不做。两个坑：换序后 `List` 过一会儿才按真实行高重排上面的行，要在一小段时间里
  每次报位置都校；那一行被挪出屏幕时格子被回收、`onGeometryChange` 不再来，只有 `onDisappear`，要先 `ScrollViewReader.scrollTo` 把它滚回来再校。
- `ScrollView` + `defaultScrollAnchor(.bottom)` + `LazyVStack`：跳到很远的 id 会因为上方懒加载视图实际高度与估算不同而漂移，验证历史渲染用 `-fromSeq`。
- 翻转的聊天列表别让它贴安全区：上方垫 1pt 空视图、输入框放在下方的 VStack 里；用 `ignoresSafeArea` + 按 inset 补边距会在键盘弹起时白屏、顶部错位。
- 聊天列表用倒置（ScrollView 和每行各 `scaleEffect(x:1,y:-1)`）：最新消息在偏移 0，跳到最新永远精确。倒置后要自己处理安全区
  （`ignoresSafeArea(.container, edges: .vertical)` + `contentMargins` 按实际 inset），并关掉 iOS 26 的 `scrollEdgeEffect`（否则整页发雾）。
  只往偏移 0（最新端）跳；往远端用代码跳、或加 `defaultScrollAnchor(..., for: .sizeChanges)`，懒加载估算不准时照样白屏。
- 聊天列表（LazyVStack + 底部锚定）偶发白屏：按估算高度跳到底会落在没渲染的区域。跳转要按哨兵 id、等布局完成再跳，并在哨兵真正可见前重试；
  `atBottom` 不要默认 true（白屏时就没有箭头可点）。验证：脚本批量冷启动打开会话，按对话区像素标准差判断白屏。
- 长列表里不要把一组可能很多的行包成一个子视图（LazyVStack 里的一个 VStack）：几百行一次布局会卡死主线程、页面空白，要拆成独立的懒加载行。
- SwiftUI 的 `onScrollPhaseChange` 在内容流入、偏移被程序调整时也会进 `.animating`：只想响应用户滚动就判断 `.tracking` / `.interacting`。
- 跟手的收起 / 展开（Safari 式工具栏）不要做成「切换状态 + 动画」：把进度（0…1）放在单独的 `@Observable` 里、只给那一小块视图读，
  滚动回调每帧写它；动起来的那块浮在内容上（overlay），别让内容的 frame 跟着变，否则每帧重排整页、消息跟着跳。松手再吸附。
- 对话行的 body 会被反复求值（滚动、流式更新）：从消息文字推出来的东西（Markdown 解析、路径 / 链接识别、去掉标签后的文字）
  按消息 id 缓存（`RowMemo`，`TextMemo`），并在消息加载后放到后台预热；别在 body 里跑正则 / NSDataDetector（实测是主线程最大的热点）。
- 滚动时别在主线程建 WKWebView：链接预加载只给刚出来的回复（15 分钟内），翻历史时每条旧回复都去预加载曾造成 1–2 秒的冻结。
- 滚动中写入的簿记（可见行、手指是否按着）放在不被观察的对象里（`ChatScratch`），写 @State 会让整页重算；派生的行列表按
  `ChatStore.version` 等缓存。WebSocket 消息在回调线程上解码，别回到主线程再解。
- 聊天列表用 `ScrollPosition.scrollTo(y:)` 会把横向偏移也归零：左右边距是 `contentMargins(.horizontal)`，整页会贴到屏幕边。
  竖向跳转用 `scrollTo(point:)` 带上当前的 x（`ChatView.scrollList`）。`scrollTo(id:anchor:)` 跳到没画过的行只按估算高度落点，
  要精确停在某处就跳过去后按实测位置再修正一两次。倒置列表里的行，XCUITest 拿到的 frame 不对（报在屏幕外），量位置用 App 内的 DEBUG 探针。
- 倒置聊天列表往上翻时的「冻结」不能按行 id 截：行 id 会随轮次推进变（答案收进工作行、多条消息的轮合并成 `wt-`），
  找不到 id 冻结就失效、可见内容被顶走（修前实测 319pt）。按「离开最新端时的 item 快照」重建行（`ChatView.Frozen`），验证用 `-showFreezeProbe`。
- BeautifulUI 的组件放进聊天列表前，先看它每次出现时做了什么。懒加载列表滚动时会反复重建行，每次出现都起 `.task`、挂入场动画（`buiEntrance`）的组件，
  在长工作卡里滑动会明显掉帧：ThinkingState 原样放进 650 步的工作卡，Debug 模拟器上卡顿时长从约 11 涨到约 21 ms/s，去掉这两样后约 13.7。
  这类问题改在 BeautifulUI 仓库里，再整个拷进 `apple/Packages/BeautifulUI`（`VENDORED.md`），不在 Jarvis 里改库代码。
- 倒置聊天列表里点开 / 收起的东西要按住表头（`ChatPin`，2026-10-31）：长出来的内容会把表头和它上面的一切往上推（思考跳 40pt，工作卡跳约 440pt、第二下点到别的步骤上）。
  表头按「同一次布局里的屏幕位置 + 内容坐标」报位置（内容坐标不随滚动变），偏移 = 点下时的偏移 + 表头在内容里挪了多少；别用回报的 offset 算（晚一帧就过冲、来回跳）。
  `ScrollPosition.scrollTo(point:)` 先落在要的点、过一会儿又差一个内容边距（10pt）、再回来；存着的 `ScrollPosition`（`edge: .top`）在内容变化时会被重新套用，
  工作步骤从 hub 到达时列表跳回最新端一帧。按住期间直接设列表 `UIScrollView` 的 `contentOffset`（`ChatPin.Finder`），开始时清掉存着的位置。
  `scrollTo(id:anchor:)` 的 anchor 对的是整行，打开的工作卡整张算一行，钉不住表头。只有比屏幕高的对话能完全按住（`MOCK_LONG_HISTORY=1`）。
- hub 一直在推（会话列表、机器、对话的新步骤），每次推送都会重算读了这些数据的视图，正在用的东西别挂在它们上面（`docs/lanes/live-updates.md`）：
  工具栏写在跟着数据重算的视图上，每次推送都重新交给 UIKit，打开的 ⋯ 菜单滚动位置被重置（菜单内容不变也一样）：`ChatsView` 只管导航和工具栏，列表是 `AgentsList`
  （拆开后 `ChatsView` 的 body 要读一下 `searching`，否则从搜索结果打开的对话又顶着系统导航栏，`SearchOpenTests`）；
  body 里写的闭包环境值（`.environment(\.openFile) { … }`）每次都是新值，读它的行全部重算（每次推送重算所有可见的过程步骤），用一次建好的 `ChatActions`；
  `model.session(key)` 按会话单独观察（`SessionCell`），别让一个会话的页面跟着所有机器的列表重算；全屏查看器、行的展开状态放在页面上，别放在懒加载的行里。
  量法：`LiveUpdateTests`（mock 的 `/mock/churn` 连续推，`-perfCounts YES` 报每轮的 body 次数和主线程耗时）。
- 角标（等你处理的数字、收件箱的「需要你」）几乎每次推送都变，读它的视图别装着页面或菜单（2026-11-01，TestFlight 70「又回来了」，`docs/lanes/live-updates.md`）：
  `Tab` 的 `.badge` 一变，`Tab` 就把里面的页面重建一遍（页面没有参数也一样，body 再跑），开着的 ⋯ 菜单回到顶部、开着的对话整页重算：
  `Tab` 里的页面一律包 `TabPage { … }.equatable()`。`Menu` 的 label 一变，开着的菜单就重新布局、回到顶部：数字、红点别放在菜单的 label 上，
  做成自己读数据的小视图放在旁边（`WaitingBadge`），装菜单的视图不读每次推送都变的东西。mock 要推 `inbox` 和 `interaction`
  （`/mock/churn?inbox=1&waiting=1`）才测得出来；新外壳是 iPhone 的默认布局，这类测试两种布局都跑（`TEST_RUNNER_JARVIS_LAYOUT=shell`）。
- 行视图的参数里有闭包或带字符串、数组的结构体（`Item`），SwiftUI 就认为它每次都变了：对话页每次推送重算一次，屏幕上加懒加载缓冲里的每一行
  （长的 Jarvis 主对话约 45 行）都跟着重跑 body（2026-11-01）。对话行（气泡、回复、工作行、输入框）一律 `Equatable` + `.equatable()`，只比消息本身；
  一直建着、平时看不见的东西（关着的侧边栏）别读 `allSessions` 这类每次推送都换的数据，看不见时用上次的值。
  流式回复每 80 ms 来一次全文：写完的块切开各自缓存（`StreamChunks`），只重画最后一块。量法见 `docs/testing.md`「Jarvis 页（长主对话）」。
- 列表别按「最近一次更新」排，滚动中也别让推送碰到它（2026-11-01，TestFlight 77，`docs/lanes/live-updates.md`「滚动时被迫刷新」）。
  真实 hub 每 2 秒推一次某台机器的会话，每次都带新的 `updated_at`；Agents 列表按状态加最近更新排空间，36% 的推送让空间换位置，滚动中整屏跳。
  - 排序键用不随每句输出变的时间：工作中的按开始工作的时间（`Session.sortDate`、`SpaceGroup.sortDate`）。
  - 手指在、惯性在、被页面盖住、在另一页的时候扣住推送，停下 0.5 秒后一次落地，一直滚也最多扣 5 秒；
    要用户处理的（需要你、提问、离线、出错）不扣（`docs/ios-state.md`「及时性」）。列表只读一个中转对象（`AgentsFeed`，
    `withObservationTracking` 跟着模型），扣住是它上面一个没人观察的开关：用列表自己的 `@State` 存快照，开始滚动那一下就让列表重画。
  - 扣住要真的挡住推送。这几处漏了，列表照样每次推送都重算：
    - 在 `onChange` / `onScrollPhaseChange` 里读模型，算作 body 读过：之后每次推送都让 body 重跑。
    - 往 UserDefaults 写东西，App 里每个 `@AppStorage` 视图都当作变了、body 重跑。`AgentSync` 每拉一次对话就写一次，忙的 hub 上几秒一次：
      常变的状态别放 UserDefaults，放 `StateCache`。
    - `List` 的每一行单独记着自己上次读过什么：body 换成读快照了，没重画的行还记着上次读的 `model.machine`、`model.session`，
      推送一来自己重画。行里不直接读模型：body 每次把要用的值填进一个不被观察的查找表（`AgentsList.Lookup`），行从表里取；
      子视图要的值由列表传进去（`InteractionPreviewCard(machine:agentLabel:)`，`model.isMuted(session)` 而不是经 `model.session(key)` 的 `isMuted(key)`）。
  - 就算数据一模一样，`List` 只要被更新一次，就会按估算高度重新量行（估算和实际差 70 pt 一行），屏幕外的行一变高矮，内容就跳几百 pt。
    在两次滑动之间落地也一样：下一次手指落下时才跳。这一下是按设计落地，`PerfScroll` 记为 `landed`，不算打断；
    别为了躲它把落地拖长，屏幕上的东西会旧太久。
  - `onScrollPhaseChange` 在 `List` 上也有效（iOS 27 模拟器实测）。滚动阶段这种只在回调里用的状态放不被观察的对象里，放 `@State` 每个阶段重算一次列表。
- 一个会变尺寸的小东西（输入框里的 TextField）和长列表在同一个视图树里：它每变一下，SwiftUI 从根重新排版整页，懒加载列表把可见行重新量一遍
  （打字每个字约 10 ms）。把它放进自己的 hosting controller（`LayoutIsland`）：里面自己量高度报出来，页面只在高度变时排版。
  别用 `sizingOptions = .intrinsicContentSize` 让页面跟着它变高：试过，打字时输入框不长高（2026-11-01，`ComposerGrowTests`）。
  新外壳的 `ShellComposerBar` 从一行展开成卡片，不用这个隔离：长文字下反复请求 UI 快照超时（新模拟器也复现），保留原视图树，只在旧布局启用。
  画到自己边界外面的内容不能放进 island：hosting controller 的视图只接边界里的点击，溢出去的部分看得见、点不到，点击落到下面的页面上。
  BeautifulUI 风格的输入框（`PromptBar`）的 `/`、`@`、+、模型菜单就挂在输入框上方，放进去后全都点不了（2026-11-02，`BeautifulStyleTests.testCommandMenuTakesTaps`），所以这个风格也不用 island。
- `List` 的行别为了 `Equatable` 包成自定义视图、再把 `.swipeActions` / `.contextMenu` 放进它的 body：试过（2026-11-01），推送和滑动都更慢
  （UIKit 那边每次都要重新配置 cell 的滑动操作）。Agents 列表推送的开销靠合并推送降下来（`AppModel.applyPendingSessions`）。

## 导航

- 侧边栏的「主页面盖在上面」和「两页相邻一起滑」是不同的空间关系（2026-11-02，#341）：底层侧栏横向应完全固定，只有很轻的纵向上浮；居中缩放会连文字的横向位置一起改变，强淡入又像侧栏刚被创建，不用它们代替上浮。动画验收看拖动的中间帧，完全打开的截图两种做法一样；单测同时守住 x 位移为 0、缩放为 1、透明度为 1。保留侧栏独立 hosting controller，进度只改 transform，不让每帧重新布局列表。
  抽帧拼图也要检查产物：`ffmpeg` 的 `-t` 与 `tile` 组合可能退出 0 却没写出 PNG；用 `-ss` 定位、`-frames:v 1` 限制拼图数量，并确认文件存在、真的读到图，不能把命令成功当成截图验收。

- NavigationStack 根视图上的 `.task` / `onAppear` 在 push 子页面再返回时会重新跑：别在里面无条件写回保存的默认值，否则会覆盖刚在子页面选的值（新对话选了启动器又被改回上次的）。
- SwiftUI：给 NavigationStack 推出页面用的环境值（如 `feishuOpen`）要挂在 NavigationStack 本身上，挂在栈根内容上推出的页面拿不到。
- 隐藏系统导航栏（`.toolbarVisibility(.hidden, for: .navigationBar)`）会顺带关掉边缘右滑返回，要自己把 `interactivePopGestureRecognizer` 打开。
- 根页面藏了系统导航栏、推出去的页面有搜索框时，搜索框开着就右滑返回，UIKit 会把导航栏留在根页面上（带根页面的标题，约 106pt 高），
  SwiftUI 以为它还藏着，不会再藏一次（2026-11-01 新外壳：从知识库搜索中右滑回助手页，外壳顶栏被挤下去约 106pt，点切换器原来的位置点在空导航栏上）。
  推出去的页面自己关搜索（`isPresented` 变 false 时）来不及。回到根时在 UIKit 里补一次 `setNavigationBarHidden(true)`（`AssistantView.RootBarHidden`）。
  这种测试要看顶栏的 frame，`exists` 看不出来（`ShellSwitchTests`）。
- 两页横着滑（新外壳的 `ShellPager`）时，各页自己画在页里的顶栏会被另一页盖住一截：两页是 ZStack 的兄弟，前面那页整页盖在后面那页的顶栏上，
  反向偏移让顶栏「不动」也没用（2026-11-01 截图：滑到七成时 Agents 的 ☰ 不见了）。拖动时把两页的顶栏藏起来，在两页之上画一条只给看的顶栏（`ShellMovingBar`），静止时再换回各页自己的。
  截图要拖到一半停住才看得出来：`-showShellPagerAt 0.3`。
- 动画结束后才换的东西，用 `withAnimation(…, completionCriteria: .removed)` 等弹簧真的停下，别用默认的 `.logicallyComplete`（2026-11-02 新外壳「切换顶部 tabs，动画完毕后页面抖一下」）：
  默认的完成回调在弹簧「看起来差不多到了」时就来，`ShellPager` 的 0.32 秒弹簧这时还差约 5pt；`moving` 一变 false，各页自己的顶栏接回来，骑在还在走的页面偏移上，
  顶栏横着跳 3–5pt 再慢慢回去（两种风格都有）。XCUITest 看不出来：元素的 frame 是布局值，不含偏移，截一张图也比这 170ms 慢；
  录屏逐帧量（`simctl io recordVideo` + ffmpeg 抽帧比对顶栏那一条），单测在宿主 App 的 scene 里挂一个窗口、`drawHierarchy` 读像素（`ShellPagerSettleTests`；不挂 scene 的 `UIWindow` 不绘制，读出来是空的）。
- 新外壳的两页一直建着、静止时只用 opacity 藏另一页，藏起来的那一页仍在无障碍树里（玻璃按钮、列表都是 UIKit），而且两页的切换器 id 相同：
  `exists` / `isHittable` / `firstMatch` 分不出哪一页在显示，要按像素判断（`ShellSwitchTests.agentsDrawn`），点切换器要挑可点的那个。
- 列表的 `.searchable` 处于搜索状态时推出的页面，系统导航栏会被强制显示（隐藏导航栏的对话页出现两行标题）；推出之后再关搜索也没用。
  要先关搜索（`searchable(isPresented:)` 置 false）、下一轮 run loop 再推（`ChatsView.pushing`），搜索词和结果保留。关搜索会清空系统搜索框里显示的字（`query` 不变，没人写回去），
  返回列表时结果还在、框是空的：推之前记下搜索词，回到列表时重新赋一次（`searchedBeforePush`）。`SearchOpenTests` 覆盖（含框里的字）。
  「关了搜索再推」不能按固定时间等：关搜索只是开始收起，收起途中推的页面被导航栈丢掉，而 path 里已经有这个对话（屏幕上还是列表，
  状态和画面对不上）；`isSearching` 在点下后几毫秒就变 false，不能当「收完了」。列表为关掉的搜索重画慢时（2026-10-31 实测点下后主线程忙约 440ms），
  #128 的 350ms 就不够，「从搜索结果点开会话打不开」。现在看系统自己的 `UISearchController`：不再 active、不在 dismiss、没有转场了再推
  （`SearchCloseWatcher`，最多等 2 秒）。搜索时折叠的空间 / 分组也要展开（`isFolded`），否则匹配的 Agent 被折在标题下面，搜得到点不到。
- XCUITest 的边缘右滑在 iOS 27 模拟器上要按手指的速度拖（按住 0.1 秒、`.slow`、末尾停 0.1 秒）：0.05 秒的快甩不被当成边缘手势，页面不动，
  看起来像「右滑返回坏了」（手动是好的，`SearchOpenTests.back`）。列表里的标题 / 行在小一点的手机上可能在屏幕外、懒加载还没建出来：
  测试先滚到它（`AgentsListTests.reach`），并在 6.3 寸的机型（iPhone 17 Pro）上也跑一遍，不能只在 Pro Max 上跑。
- 同一个 NavigationStack 里，用 `NavigationLink { 目标 }`（视图式）推出来的页面再用 `NavigationLink(value:)`（值式）推下一页，
  新页面会被插到它**下面**：点了像没反应，返回才看到（设置 › 机器 › 卡片、机器详情 › 会话都踩过；知识库从搜索结果点进条目也是，返回时还露出一个黄色 ⚠️，2026-10-31）。一条链上要么全用值式（`MachineListRoute`、
  `MachineDetailRoute` + `navigationDestination(for:)`），要么全用视图式。`navigationDestination(isPresented:)` 也算视图式。
- `NavigationPath([x])` 里的 `x` 如果是 `AnyHashable`，路径记的类型就是 AnyHashable，`navigationDestination(for: 具体类型)` 找不到，
  页面是一个黄色 ⚠️：要按具体类型 append（`SettingsView.openDebugPage`）。
- `NavigationSplitView` 右栏里推出的页面，是用左栏登记的 `navigationDestination` 在左栏的环境里建的：给右栏页面的环境值
  要挂在整个分栏视图上，挂在右栏的 NavigationStack 上拿不到（`SplitNavigation`）。
- iPhone Duo（`docs/ios.md`「iPhone Duo」）：按宽度（size class）分布局，不看机型；模拟器开合只能在 Device Hub 里切，
  截图要 `--display=1/3` 指定屏幕，否则可能拍到熄灭的那块（全黑）。
- 推出的页面别用 `.toolbar(.hidden, for: .tabBar)`（`.toolbarVisibility` 一样）藏 tab 栏：它在 push 的第一帧就把 tab 栏拿掉，
  底下列表的导航栏和搜索框同一帧跟着消失，点进对话「闪一下」（2026-10-31）。用 UIKit 的 `setTabBarHidden(_:animated:)` 跟着页面出现 / 消失
  （`hidesTabBarWhenShown`）；`hidesBottomBarWhenPushed` 在 SwiftUI 里设得太晚，不生效。这种闪动 XCUITest 看不出来：
  `xcrun simctl io <udid> recordVideo` 录下来，用 ffmpeg 把点击后 0.3 秒的帧拼成一排看。

## 布局与组件

- 换布局（新外壳、新风格）重写一个页面时，先把旧页面上的每一块列出来，逐块决定带不带过去，别只照新设计稿画（2026-11-02）：
  新外壳的新对话页只画了输入框和「需要你」，旧布局的欢迎页（问候、能看到什么、可以问的事）没搬过来，没有待办时整页空白，一天后用户才发现。
  共用的内容抽成一个视图两边用（`AssistantWelcome`），再加一个 UI 测试按「元素在屏幕上、没被盖住」守住，不能只看编译通过。
  `safeAreaInset` 里的东西不带底色时，滚动内容长了会从它后面透出来（叠在「需要你」下面）：给这块垫上页面底色。
- 固定在底部的一块（`safeAreaInset` 里的「需要你」+ 输入框）按普通 iPhone 的高度定行数，到了矮屏（iPhone Duo 外屏约 678pt、SE）会吃掉半屏，
  把滚动内容拦腰截断（2026-11-02，#283）。按实际空间定：量出不该被盖住的那一行在滚动内容里的下沿（`WelcomeFold`，在内容自己的坐标系里量，
  滚动不变）和页面高度（键盘不算），剩下的高度给底部那块；输入框按静止时的高度算，否则一聚焦、一打字行数就跳。键盘弹起时 iPhone 上是整个页面的 frame
  变矮、不是安全区变了，背景加 `ignoresSafeArea(.keyboard)` 量不回原来的高度：取同一宽度下量到过的最大值（转屏、Duo 开合会改宽度）。
  这种「打字时不变」的 UI 测试要在原来就显示多行的机型上跑（17 Pro）：Duo 外屏本来就只剩 1 行，掉无可掉，测试白过。垫了底色的那块上沿加一条渐隐，别硬切。
- `ViewThatFits` 要在一个高度预算里挑：外面套 `.frame(maxHeight:)` 不行，它自己会撑到 maxHeight；用一个「只给这么高、自己按孩子的大小」的
  `Layout`（`AtMostHeight`），而且不要再和父视图给的高度取小：键盘弹起时 `safeAreaInset` 里的 VStack 给的高度变小，一聚焦行数就掉。一个都放不下时 `ViewThatFits` 拿最后一个孩子、按给的（更小的）高度排，BeautifulUI 的列表行会被压扁：
  每个孩子加 `.fixedSize(horizontal: false, vertical: true)`。
- iPhone Duo 外屏（和内屏横放）的状态栏竖在侧边，安全区左右有 inset：按 `g.size.width` 画的背景（浮动顶栏下的雾）停在状态栏旁边，
  深色下是一条硬边。铺满的背景宽度加上 `safeAreaInsets.leading + trailing`、往左挪 leading（`BarFog`）。

- XCUITest 里，容器上的 `accessibilityIdentifier` 会盖掉它里面按钮自己的（侧边栏的 `assistant-sidebar` 让底部的「新对话」药丸查不到 `sidebar-new`）：
  按 label 找，再用 identifier 排除别处同名的按钮。

- 「请求」类的状态（别处让一个页面推某页、打开某个浮层）要带序号并记下处理过的那个（2026-11-01 新外壳 `ShellState.request` / `AssistantView.handledRequest`）：
  页面用 `onChange(…, initial: true)` 接它，从推出的页面返回时偶尔又执行了一次，把收件箱又推了回去（3 次里 1 次，UI 测试 `-test-iterations 3` 才抓到）。

- 想让系统导航栏里的东西和自己画的浮动顶栏对齐，别放进 `.toolbar`（2026-11-01 新外壳的「助手 | Agents」）：`.principal` 项会被两边宽度不同的按钮挤离中间，
  而且会被包进导航栏共享的玻璃里（自己的胶囊外面又套一层，选中的那块颜色也变了；`.sharedBackgroundVisibility(.hidden)` 能去掉玻璃，挤偏去不掉）。
  Agents 页的做法：导航栏只留给 `.searchable` 的搜索框（principal 放一个透明的点盖住标题），自己的顶栏（`AgentsTopBar`）盖在上面，和助手页用同一组组件。
  搜索激活时它要让开，见下一条。
- 浮在系统导航栏上的自画顶栏（新外壳的 `AgentsTopBar`，导航栏留着只为 `navigationBarDrawer` 的搜索框）：搜索一激活，系统把搜索框升进导航栏那一行，
  正好在顶栏的按钮下面，两层叠着谁也点不全（TestFlight 70「点击搜索组件直接冲突」）。搜索时不画自画顶栏（`searching` 为 true），让系统的搜索框和「取消」占满那一行；
  这种叠压 XCUITest 的 `exists` 看不出来，要比 frame 是否相交（`ShellSearchTests`）。
- 搜索未激活、只是下拉展开时，系统导航栏仍只给标准按钮留高，放大一档的外壳按钮和头像的收件箱角标会压住搜索框（2026-11-02；模拟器复现头像 frame 底部 125pt、搜索框顶部 117pt）。
  `safeAreaPadding` 不会移动 UIKit 的搜索栏。让位要做在这一页自己的导航项上：Agents 根页的 `navigationItem.prompt` 放一个空格（`ShellNavigationClearance`），
  这一页的导航栏高一行（34pt，不随字号变），搜索框和列表跟着往下；搜索激活时去掉，旧布局不加。外壳顶栏的按钮因此在 `Theme.Shell.topBarLargestTypeSize`
  （xxxLarge，1.35 倍）封顶（`shellTopBarScaled()`），否则辅助功能大字号下头像角标压住搜索框。`subtitleView`（不长高）、两行 prompt（还是一行）都试过，没用。
  `ShellSearchTests` 覆盖小 / 标准 / 大字号的展开、输入、取消、搜索结果返回，深浅色都看截图。
- **别在导航容器上做按页面开关的让位**（#340，2026-11-02）：先前的做法是导航容器外按 `path.isEmpty` 加 padding，推入对话时拿掉、返回时加回。
  UINavigationController 在推入 / 返回的转场途中不会重新摆它的导航栏：返回开始时栏还停在对话那一刻的位置（容器 88 + 旧安全区 62 = y 150，应为 98），
  列表的 inset 跟着错，`navigationBarDrawer` 的搜索框被当成滚到顶而展开（+54），转场结束 0.1–0.6 秒后才重排，整页上弹 52pt，最后停的位置也变了（搜索框开着）。
  在返回开始那一帧（根页 `viewWillAppear`）就无动画地切、再 `setNeedsLayout` + `layoutIfNeeded` 都一样：容器的 safe area 当帧就对了，栏的 frame 不动。
  旧布局没有让位，全程不动。规矩：导航容器的 frame 和 safe area 在推入、返回时一律不变（外壳状态行改的是窗口根控制器的 `additionalSafeAreaInsets`，整页一起动），
  某一页要多的空间放在这一页自己的导航项上，UIKit 会在转场里原生地过渡。`AgentsReturnTests` 用 `probe.agents-scroll` 逐帧量（两种返回 × 搜索框收 / 开 × 两种布局）。
- 逐帧查「动了一下」：DEBUG `-showPopFrames YES` 每帧把导航栏、搜索栏、列表（大于 300pt 的 collection / scroll view）在窗口里的位置和 inset 变了就打一行 `POPF`，
  配合 `simctl io recordVideo` 和 `log stream --predicate 'eventMessage CONTAINS "POPF"'`。注意 UIKit 转场动画只改 presentation layer，日志里是 model 值（终点）；
  SwiftUI 驱动的 frame 动画逐帧都在 model 上。

- 不要写「PrimitiveButtonStyle 里 `Button(configuration).buttonStyle(.glass)`」来包装系统按钮样式：玻璃样式下递归到爆栈，
  打开对话页就崩（崩溃栈全在 Swift 运行时的类型解析里，看不到业务代码）。按风格切样式用 View 扩展在调用处直接选（`.themedButton()`）。
- 横竖都能滑的等宽文本别用 SwiftUI「横向 ScrollView + LazyVStack」（内容宽度算不出来，只能竖滑），也别用自己滚动的 UITextView
  （TextKit 2 无论容器多宽都会折行）：用 UIScrollView 包一个不滚动、按最长行实测宽度的 UITextView（`MonoTextView`）。
- `ScrollViewReader.scrollTo` 会连带滚动外层容器：在顶部横排条里做 `initial: true` 的 scrollTo 把整个顶部区域推上了导航栏。
- 列表上挂的 `.simultaneousGesture(TapGesture())`（点空白关闭浮层）会把长按松手也认成一次点击（按住超过约 2 秒才不认）：
  长按刚打开的消息操作条一松手就被关掉。打开它的那次按压在按住和松手后 0.3 秒内不许关闭（`MessageMenu.pressChanged`）。
- List 的分组标题（Section header）不是单元格，挂在上面的 `.contextMenu` 长按基本触发不了：标题上的操作要给一个看得见的按钮 / Menu。
- MarkdownUI 的 `.markdownTextStyle` 要写在 `.markdownTheme` 里面（先调用），否则被主题的文字样式盖掉。
- 链接在哪结束只有一个规则（`Links`）：空白、任何非 ASCII 字符（中文、全角标点）处结束，去掉结尾的 `.,;:!?` 和不成对的 `)` `]`。
  MarkdownUI 用的 cmark-gfm 自动链接只在空白处结束，中文里一个链接会吞掉后面整段（2026-10-30）：所有交给 MarkdownUI 的文字先过
  `Links.markdown`（把裸链接写成 `<url>`），回复、计划、文件、飞书消息、知识库都一样；路径识别（`FilePaths`）也按 ASCII 结束。
- 回复里的表格不能按内容撑宽（MarkdownUI 的 Grid 每列取最宽的格子，长中文列直接出屏幕）：`FittedTable` 按可用宽度分配列宽，
  短列保持原宽、长列折行，只有最小宽度都放不下才横滑，右边渐隐提示还能滑（`TablePlan`）。
- 自定义 SwiftUI `Layout` 的 `proposal.width` 可能是 `.infinity`，`Int(...)` 前不判 `isFinite` 会直接崩（飞书卡片列布局踩过）。
- List 的 Section header 会把层级样式（`.primary` / `.secondary`）再调淡一级：放在 header 里的卡片名字变灰、说明文字几乎看不见。
  要和别处一样的颜色就写 `Color.primary` / `Color.secondary`（`MachineCard`）。
- List 里空的 Section（折叠后只剩标题）照样占一个标题的上下边距加段间距，「全部折叠」一屏只放得下五六个：折叠的空间改成一行
  （标题当普通行，`listRowBackground(.clear)`，`listSectionSpacing(.custom(0))`，`ChatsView.spaceSection`）。
- 每行自己画卡片 / 胶囊的列表别放在 `insetGrouped` 里：分组卡的内缩、行的 `listRowInsets`、分组标题的边距叠在一起，搜索框、chip、标题、胶囊四种左边距；
  没有标题的第一个分组上面还多约 35pt 空白，`DisclosureGroup` 展开后标题画在分组卡上、子行另成一排缩进的胶囊（2026-10-31 用户在手机上报）。
  BeautifulUI 风格的 Agents 列表是 plain 列表，每一行都落在 `Theme.Space.page` 上，分组标题是一行，空闲的空间用 `TaskRow` 的 `.list` 拼成一张卡。
- `BUIListRow` 放进原生 `List` 的行里会双倍留白：行自己有 8pt 上下 padding 和最小高度，`List` 还再加一圈行内边距，设置页的行量出来 77.7pt，
  而同一页没换成 BUI 行的连接器那组、液态玻璃的行都是 52.3pt（2026-11-01 用户在手机上报「上下两组行高不一样」）。BeautifulUI 风格下给 List 的行用
  `bareListRow()`（`SettingsView.swift`）：`listRowInsets(.vertical, 0)`；它要加在行的 `Group` 上，不能加在 `Section`（会把分组标题的上边距也吃掉），
  也不能加在 `NavigationLink` 的 label 里（到不了 List）。同一页的每一组都要用同一种行（`SettingsRow`），别留一组系统行。
- `.safeAreaBar(edge: .top)`（Agents 列表的快速筛选条）别和会随滚动收起的 `.searchable`（导航栏抽屉默认 `.automatic`）一起用：
  真机（TestFlight 35）上滚动收起搜索框后，筛选条还停在搜索框展开时的位置，中间空出约 120pt 雾化的列表（「顶部一大坨」）；
  当时改成搜索框 `.navigationBarDrawer(displayMode: .always)` 常显；2026-11-01 用户嫌顶部太挤，改回 `.automatic`（滚动收起、下拉出现），
  手机上筛选条不再放 safeAreaBar，而是列表的第一行、跟着列表滚（`ChatsView` 的 `filterBar`），宽屏分栏里照旧在列表上方。
  打开时就收起（用户要的，和「邮件」一样）：SwiftUI 没有开关，第一次出现时把列表往下滚一个搜索框的高度（`SearchCloseWatcher.foldOnce`，
  UIKit 直接设 contentOffset、保留 x），只做一次，从对话返回不再动；列表比屏幕矮时滚不动、会弹回展开，要在底部补刚好够滚一个搜索框高度的 contentInset。
  程序折叠后 UIKit 会收短导航栏的 top inset，并可能把同一个差值再加到 contentOffset（iOS 27 实测：inset 168→116，代码设 offset -116，系统又变成 -64），
  首行因此压到导航按钮后面。初次折叠等完整 inset 变化后，只在 offset 正好等于「请求值 + inset 差值」时归齐新的顶部；用户滚动或激活搜索就停止。
  不用加 padding 补，`AgentsListTests` 同时按 frame 和截图像素查重叠与空档。
  搜索激活时还有另一种空档：`insetGrouped` 的首行 content y 从 0 变成 35，offset 仍等于 -topInset，是原生首段间距。
  手机的列表首行是筛选控件，显式用 `Theme.Space.xs` 的顶部 content margin；常规宽度分栏仍用系统默认值，测试不放宽空档上限。
  系统搜索框里的输入框贴在这一条的上沿（52pt 的条、42pt 的框），离自画顶栏只有几 pt；`searchFieldBackgroundPositionAdjustment`、给整条或输入框设
  transform 都没用（真机上量过：值设上了，展开时系统重新摆放、屏幕位置不变，2026-11-01）。要改这个间距只能自己画搜索栏。
  再改这里要在真机上来回滚动、下拉看顶部有没有空白。同样的「一大坨」还有一个来源，见下面横向 `ScrollView` 那条。
- 列表上方的横向 `ScrollView`（Agents 的筛选条，放在 safeAreaBar 或 VStack 里都一样）可能被导航栏当成这一页的滚动视图，内容整体下移
  整个顶部（状态栏 + 导航栏 + 搜索框，约 170pt），中间露出雾化的列表：`NavigationSplitView` 左栏（Duo 打开 / 半折）一定出；普通 iPhone 上
  冷启动恢复上次打开的对话、列表第一次布局在对话下面时也出，返回列表就是 TestFlight 35 / 38 截图的「顶部一大坨」。**XCUITest 读到的 frame
  仍是没下移的位置**（是 UIKit 挪的内容，SwiftUI 的布局和无障碍 frame 都不知道），以前「模拟器复现不了」就是只看了 frame：
  这种问题要按截图像素量（`AgentsListTests.drawn` 看选中的「全部」是不是真画在 frame 那里）。导航栈下面的页面顶部**别放横向 ScrollView**，筛选条放不下先收窄，
  再换行（`QuickFilterBar`，`ViewThatFits`）。`testFilterBarHugsTheTop` / `testFilterBarHugsTheTopBehindARestoredChat` 覆盖。
- `contentMargins` 作用于它下面所有的滚动视图，包括 `safeAreaInset` 里的：Jarvis 页给悬浮顶栏留的上边距写在输入框的 inset 之后，
  输入框上面那行横向滚动的「权限 / 思考」也多了顶栏的高度，约 66pt 页面色挡住建议卡片，所有风格都有（2026-10-31，#114）。边距写在要它的滚动视图上、inset 之前。
- UI 测试的「返回」别用 `navigationBars.buttons.firstMatch` 兜底：已经回到列表时导航栏上第一个按钮是「新建会话」，点了就弹出新建浮层，
  后面的查找全跑进浮层里，报的是「找不到某一行」，像是演示数据过时（2026-10-31 `DemoModeTests`）。先等目标页可点，再排除页面自己的栏按钮。
- `TextField(text:selection:)` 的选区：没在编辑（未聚焦）时也会报一个选区，冷启动恢复草稿后它指在草稿中间；而且它的
  `String.Index` 可能来自另一份字符串，越界的 `utf16Offset(in:)` 直接崩。插入文字（发送文件的路径）只在聚焦时用选区，先夹到 `endIndex` 再换算（`PathInsert`）。
  UI 测试里草稿会跨次运行保留，测输入框内容前先清掉。
- Swift Charts 的线要在空档处断开：给每段一个不同的 `series:` 值（「指标#段号」），同一个 series 的点总会连起来。
  `AxisValueLabel` 不显式给颜色时，日期刻度会染成 App 的 tint（蓝色），要写 `.foregroundStyle(Color(.secondaryLabel))`。
  落在 `chartXScale(domain:)` 之外的点照样画（线会伸出纵轴、卡片边），要 `.chartPlotStyle { $0.clipped() }`。
- SwiftUI `Menu` 的 label 上挂的东西（`.overlay`、`.surface` 玻璃）都会变成菜单按钮的一部分：玻璃铺满整个 label（头像连名字套进一个大玻璃框），
  overlay 里的文字 XCUITest 也找不到（按钮只报它第一次的 label）。要临时盖在按钮上的提示放在和 Menu 并列的 ZStack 里（`AssistantTopBar.face`）。
- 自己画的浮动顶栏要盖住状态栏的雾，背景用 GeometryReader 读 `safeAreaInsets.top` 往上偏移，直接 `ignoresSafeArea` 的背景高度会被算短。
  整页横向拖拽（Jarvis 的侧边栏）用 UIKit pan + `gestureRecognizerShouldBegin` 在开始时判方向、让给能横向滚动的子视图，SwiftUI 的 DragGesture 做不到这种让位。
- `Chip` 是 `fixedSize` 的，不会缩：放进窄的网格列（空间页一行两张卡）里，长标签名会把卡片撑得比列宽，整个 `LazyVGrid` 挤出屏幕、
  第二行错位（2026-10-31）。窄处的一排 Chip 用 `ViewThatFits` 逐级去掉次要的、最后让主标签截断（`SlotCard.header`），验证用 mock 的 `MOCK_LONG_SLOTS=1`。
- 几万行的文本别放进一个 UITextView（`MonoTextView` 整篇排版，随大小线性变慢）：文件查看器用 `CodeLinesView`，只把屏幕上的行做成 label、滚动时复用，
  行号固定在左边，2 万行的 Go 打开约 0.3 秒、滚动 60 fps。MarkdownUI 渲染很长的文档要切成段放进 LazyVStack（`MarkdownChunks`），
  而且别开 `.textSelection(.enabled)`：实测打开慢约 40%，每段进屏的长帧从约 40 ms 变成约 60 ms（`docs/ios.md`「文件查看器」）。
- 消息里的路径只在那台机器上真的有时才做成可点的文件块（`FilePresence` 批量问 `GET …/stat`，不确定就不链接）：以前别的机器的路径（hub 的
  `~/.jarvis/…`）也能点，点开只说「打不开这个文件」。

- 自己判断方向的 `UIPanGestureRecognizer`：`gestureRecognizerShouldBegin` 被调用时 `translation` 还是 0，`velocity` 是最后两个采样的猜测
  （一次 30° 的斜划读成 (1, 68)，几乎竖直向下），按它判断就会把斜着的拖动交给列表。要在识别器子类里记下按下的位置，按实际位移判断
  （`DrawerPan.Recognizer`，2026-10-31「划了没反应」）。外层的 pan 和里面的 ScrollView 是赛跑：斜一点（40°）的拖动，竖直分量先过滚动视图的门槛，
  外层连问都没被问（日志里什么都没有，测试时好时坏）；横向滚动的代码块在边缘也会抢去做回弹。让滚动视图的 pan 等外层先判断
  （`gestureRecognizer(_:shouldBeRequiredToFailBy:)`），外层在同样的几个点内判断完，滚动不会变晚。
- 跟手的拖动里别切 tab 栏（`.toolbarVisibility(_, for: .tabBar)`）：它改页面的安全区，整页（导航栈、输入框、每个 UITextField）在拖动第一帧
  重新布局一次（Jarvis 侧边栏 20–40 ms）。要让它让开就淡出：UIKit 的 tab 栏控制器会在 SwiftUI 刷新 tab 栏时把 `tabBar.alpha` 改回 1，
  淡出 tab 栏外面只装着它的那层容器（`AssistantDrawer.applyToUIKit`）。
- SwiftUI 的 `.accessibilityHidden(true)` 藏不住里面由 UIKit 承载的部分（TextField、ScrollView 和其中的行）：XCUITest 照样找得到，
  VoiceOver 也会走进去；`opacity(0)` 也一样。一直建着、平时藏起来的大块界面（Jarvis 侧边栏）放进自己的 `UIHostingController`，用 UIKit 的
  `isHidden` 藏。SwiftUI 会在每次更新时重设 representable 给它的那个视图的 frame、transform、alpha、isHidden，自己要动的是里面一层
  （`DrawerSidebarHost.Container`）；整套环境用 `.environment(\.self, context.environment)` 带进去。
  Agents 对话的顶栏（`ChromeHeader`）是同一类坑（2026-11-02）：全开的和折起的叠在一个 ZStack 里，只靠透明度和 `allowsHitTesting` 切换，
  两层的「返回」一直都在树上；折起后全开那层上移进了裁切区，它的「返回」落在状态栏那一条，XCUITest 说点得到，点了是状态栏，不返回。
  `.accessibilityHidden` 加在层上、甚至直接加在 plain 按钮上，XCUITest 都照样找得到。做法：看不见的那层不在树上（折起的细条半程前不建；
  全开的那层淡完后 `.hidden()`，还占位、还能量高度）。代价是它的身份会变，它弹出的 sheet、菜单会跟着消失：所以有东西弹在页面上时顶栏不自己折起
  （`ChatView.somethingPresented`），`ChatHeaderPresentationTests` 守着。
- 量拖动 / 动画每帧的主线程耗时别在 XCUITest 下量：它打开无障碍，SwiftUI 每帧多做很多事。App 里用脚本按显示帧驱动同一条代码路径，
  `simctl launch` 启动后量（`DrawerAutoDrag`，`docs/testing.md`「侧边栏拖动」）。
- 行要有系统的左滑（`.swipeActions`）就得是 `List`，`ScrollView` + `LazyVStack` 里没有（2026-11-01 侧边栏的「归档」）。换成 `List` 时要外观不变：
  `listStyle(.plain)`、每行 `listRowInsets` / `listRowSeparator(.hidden)` / `listRowBackground(.clear)`、`scrollContentBackground(.hidden)`、
  `defaultMinListRowHeight` 设 0。**单独一个 `Divider()` 当一行会竖着画**（一根小竖线）：包一层 `VStack`。
- 抽屉开着时左拖本来是关抽屉；行的左滑要先赢：`DrawerPan` 在 `shouldBeRequiredToFailBy` 里让列表（UICollectionView）的 pan 等它，
  它在 `gestureRecognizerShouldBegin` 里看手指下的 cell 有没有 `SwipeRowMark`（一个空的 UIView 标记）再放手。
- 一组子视图（`HStack` 里的图标、文字、计数）只在外面挂 `.accessibilityLabel` 不够：标签会落到每个子视图上，读屏连读几遍
  （「还有 N 个空闲」那行读三遍「展开 N 个空闲的 agent」，iOS 和 Mac 一样，#405，2026-11-02）。先 `.accessibilityElement(children: .ignore)`
  合成一个元素再给标签；包在 `Button` 里也照样能按。

## Swift 与编译

- 闭包属性别和调用它的方法同名：`func close(_ s: Session) { close(s) }` 会解析成递归调用方法，点「关闭 Agent」后栈溢出；重命名同理。
  回调字段使用 `closeHandler` / `renameHandler`，保留对外方法，并检查编译器的 `function call causes an infinite recursion` 警告。
- 被取消的请求 / 任务（离开页面、切换）会抛 URLError.cancelled（「已取消」）或 CancellationError，展示错误前先过滤（`Error.shown`）。
- Swift 的 `dict[k, default: X].method()`：值是 class（`CGMutablePath`）时只是读，默认值不会存进字典，改动全丢（终端框线整条消失过）。
- Swift 的尾随闭包按位置匹配第一个闭包参数：一个类型有两个可选闭包属性（`SpaceMenuItems` 的 `rename`、`open`），
  新加的那个放在前面，原来 `SpaceMenuItems(group:) { 重命名 }` 的调用就悄悄变成了另一个。新加的可选闭包放最后，调用处写标签。
- 中文环境下 SwiftUI 的 `.dateTime.hour(.twoDigits(amPM: .omitted))` 是 12 小时制，17:00 显示成「05:00」：要 24 小时的时刻用固定 `HH:mm` 的格式（`AssistantScheduleText.hm`）。
- `apple/Shared` 里的类型默认是 MainActor 隔离：新加的纯数据 enum（被 nonisolated 的 struct 用到）要标 `nonisolated`，否则报 isolated conformance 错。
  JarvisTests 目标没有默认隔离：测试里用到 App 的类型（静态方法、属性、key path）时整个测试 struct 标 `@MainActor`（和 `ChatStoreTests` 等一样），
  别靠在 App 里去掉隔离来迁就测试；提交前跑一遍整个 JarvisTests，一个文件编译不过整个目标都跑不起来（2026-10-31 `KnowledgeTests`）。
- App 目标默认 MainActor 隔离：交给系统、由系统在自己队列上调用的闭包（`PHPhotoLibrary.performChanges`、`SFSpeechRecognizer.requestAuthorization`、
  `fetchReminders`、`PHImageManager`、`WCSession.sendMessage` 的 reply / error……），SDK 没标 Sendable 的，编译器不报错，闭包被当成主线程代码，
  运行时隔离检查一失败就崩（`dispatch_assert_queue_fail`，2026-10-31 保存图片必崩）。这类闭包一律写成 `{ @Sendable … in`，`BackgroundCallbackTests` 按源码检查；
  SDK 已标 `NS_SWIFT_SENDABLE` 的（HealthKit 查询、URLSession）和在主线程回调的代理不用管。真点系统功能的 UI 测试（`PhotoSaveTests`）才测得出来。
### 最低兼容编译器

- **最低兼容的编译器是 Xcode 26.0（Swift 6.2），固定不动**（2026-11-02）：本机和不少开发机还在 26.0，master 必须在它上面编得过（iOS、Mac、手表、各扩展）；发版机器的 Xcode 更新（Swift 6.3.x），能编不代表 26.0 能编。
  踩过三次：#216 `isDynamicIslandLimitedInWidth`（iOS 27 SDK，#217 修）、`toolbarVerticalEdge`（iOS 27.1 SDK）、#164 `ShowsSnippetView` / `view:`（#430 修）。
- 新 SDK 才有的符号用 `#if compiler(>=6.3)` 包起来，`#else` 走旧行为（旧 SDK 上功能退回去，不要空着）；`#available` / `@available` 只管运行时，符号编译时找不到照样报错。
- 改了用到新 API 的代码，合入前在 Xcode 26.0 上编一遍（`eval "$(python3 scripts/jarvis_config.py env)"` 取 `DEVELOPER_DIR`），PR 里写明编过；编不了新 SDK 的分支，在 PR 里写明没编。
- 要抬高最低版本是产品决定：先问用户，同步改 `CLAUDE.md` 的那一条和这一节。

- 发版机器的 Swift（6.3.3）和别的开发机不一样，Release 归档会报「unable to type-check this expression in reasonable time」：
  长修饰符链里的多语句闭包（`.task { … }`）会和整条链一起推断类型，报错的行号还会跟着改动乱跳。把闭包体挪成方法、id 挪成计算属性（`ChatView.prewarmRows`）。
- Swift 按位置先读出参数再调用后面的参数：`emit(s, i, word(…, &i, …))` 里传给 `emit` 的 `i` 是 `word` 改它之前的值（文件查看器的宏 `println!` 少了 `!`）。
  一个参数要改另一个参数用到的变量时，先把结果存进变量。
- 改构建产物 Info.plist 的脚本阶段（设置 › 关于 › 构建 的 commit，`project.yml` 的 `Stamp the commit`）要把 `$(TARGET_BUILD_DIR)/$(INFOPLIST_PATH)`
  声明成输入，否则 Xcode 可能先跑脚本、后重写 Info.plist，写进去的值就丢了（bundle id 一变就会重写，2026-10-31）。脚本要读 `.git`，
  这个 target 关了 `ENABLE_USER_SCRIPT_SANDBOXING`。

## 录音与 WatchConnectivity

- 接收 `WCSessionFile` 必须在代理回调返回前同步移动音频，系统随后会删除临时文件。传输成功不等于索引已保存；手机原子写入索引后发应用层确认，手表确认后才删原件。
- 分段写入队列的回调切主线程有延迟。停止录音时不能只数主线程已经登记的分段，先停引擎、同步取全部定稿分段再写结束总数；停止恰好在边界上时也要单独发结束消息。
- watchOS 上的 Picker 放进紧凑的 VStack 可能被压成一条细线，编译和无障碍存在性都测不出来。少量录音语言用单行切换按钮，并实际检查手表截图。
- 本机录音和输入框听写共用麦克风所有权；失败或迟到的停止回调不能关闭另一个使用者的音频会话。删除录音保留墓碑，防止尚未完成的手表回传把条目复活。

## 系统能力与页面约定

- 开了 HealthKit entitlement 就必须有 `NSHealthUpdateUsageDescription`（哪怕只读），否则上传报 90683。
- WatchConnectivity 在 activate 完成前 updateApplicationContext 会被丢弃：配对信息要在 activationDidComplete 后再推。
- `UNUserNotificationCenterDelegate` 不要用 async 版本的 didReceive / willPresent：返回时在后台执行器上，UIKit 收到非主线程的完成回调，
  点通知进 App 会崩。用 completion handler 版本，`DispatchQueue.main.async` 里处理完再调 completion。
- DEBUG 启动参数的名字不能和任何 Swift 标识符同名：枚举 case 名会以反射元数据进 Release 二进制，`release.sh` 的 `strings` 检查会误报
  （`Tab.workbench` 撞了 `-workbench`，已改名 `-openWorkbench`）。新开关用 `open…` / `show…` 这类不会是类型成员的名字，并加进 release.sh 的列表。
  存储属性名也一样（2026-10-31：`AgentsListMenu.Actions` 的 `openMonitor` 属性撞了 `-openMonitor`，TestFlight 50 停在这一步）。改了带新属性的代码，
  先本地 `xcodebuild build -configuration Release -destination generic/platform=iOS CODE_SIGNING_ALLOWED=NO`，对 `Release-iphoneos/Jarvis.app/Jarvis` 跑一遍 release.sh 里的 strings 检查。
- 实时活动（`docs/live-activity.md`）：ActivityKit 用默认 JSONDecoder 解推送的 `content-state`，`Date` 会按 2001 年起算，时间一律传 Unix 秒数字；
  锁屏 / 展开态限高约 160pt，超出会被静默裁掉；`simctl io screenshot` 拍不到灵动岛和锁屏上的活动（用 UI 测试 `LiveActivityShots`），
  `simctl push` 不支持实时活动推送，但模拟器能收真实的 APNs sandbox 推送（`TestManualLiveActivityPush`）。
  重复的 symbol 动画（`.rotate` / `.pulse` + `.repeating`）在活动里不动（像素比对验证过），别设计「转动的齿轮」这种效果。
  灵动岛上的数字（TPM、token、负载）会占推送预算：hub 只推设备显示的项、取两位有效数字、每台最多每分钟一次。
  和别的 App 的活动共用灵动岛时，iPhone 18 Pro 上两个都是限宽 28pt 的紧凑态（左右两块挤在一起，第二块被截），17 Pro 等是最小态：
  限宽靠 iOS 27 的 `isDynamicIslandLimitedInWidth` 知道，其余（哪种呈现、有没有别的 App）App 都感知不到；紧凑 / 最小态不给宽度，`ViewThatFits` 没用。
  两种机型都要拍（`scripts/island_peer.sh` + `LiveActivityShots`，`docs/live-activity.md`「和其他 App 共用灵动岛」）。
- App 和扩展共享的钥匙串组（推送中继的私钥 `.push`；灵动岛的形象 build 92–103 也放过 `.look`）读不到时一律静默退回默认值，模拟器上又总是好的：
  build 92 的扩展签名里一个组都没有（无签名归档 + ad hoc 签名没带 entitlements，`$(AppIdentifierPrefix)` 为空），灵动岛永远是默认形象 + 系统蓝。
  Info.plist 里的组名用 `$(DEVELOPMENT_TEAM).`；动了 entitlements 或签名流程，对导出的 ipa 跑 `scripts/entitlements.py check`（`docs/release.md`「entitlements」）。
- 实时活动在模拟器上画得出来不算数（2026-11-02，build 98 签名对了，手机上还是默认形象，换成内置形象也一样；`docs/live-activity.md`「为什么 build 98 还不对」）：
  - 实时活动要画的东西放进它的内容（ContentState），内容里放不下的（图片）放 App Group 容器里的文件。**实时活动的扩展在手机上读写不了钥匙串**：
    build 103 的诊断里 App 写进共享组、读回都成功，扩展一次报告都没写出来（模拟器上读写都行；Apple 只说实时活动跑在自己的沙盒里）。
    内容不变的 update 系统可能不重画：要它重画，就让内容真的变。
  - App Group 要先在开发者网站上注册、加到 App ID 上，API key 做不了（没有这个接口，`-allowProvisioningUpdates` 的导出也不会替你建），
    没注册就加 entitlements 会让导出失败（`docs/release.md`「App Group」）。
  - 模拟器不管实时活动的图片限制。图不能比它所在的呈现大：`UIImage(data:)` 按 1× 解码，78px 就是 78pt。按显示尺寸给 scale（`FleetFaces`）。几个实时活动的第三方库还守着单张 4 KB（Apple 没写）；放钥匙串时照做过，图糊，改走 App Group 文件后按每种形态的大小各存一张 @3x、从原图高质量缩（#349）。
  - 测试数据要和真实的一样复杂：纯色占位图只有几百字节，任何大小限制都碰不到。拿真实形象包在本机测，包不进仓库。
  - 手机上扩展读到了什么，只能靠它自己报：日志（`category:island`）加 设置 › 通知 › 灵动岛 长按预览打开的诊断页（`docs/live-activity.md`「诊断」）。
- 机器页面的计数和列表不算「对话」tab 助手的无头会话（`MachineDetailView.agents`，和 Agents 列表一致）：它们跑在 hub 那台机器上，
  以前「服务器」卡片的「运行中」把它们也算进去了。
- 体验模式（`docs/ios.md`「体验模式」）挂在 `APIClient` / `LiveConnection` 下面（`DemoHub`），页面代码不写 `if demo`：新页面要的接口在
  `DemoHub` 里没有就是 404「体验模式里没有这项数据」，加接口时顺手在 `DemoHub` 回一份、跑 `DemoModeTests` 看 `DemoHub miss` 日志。
  演示数据改 `scripts/mock_hub.py` 后重跑 `scripts/make_demo_data.py`。会碰到电脑 / 飞书 / 邮件的操作一律不真的做（`DemoText.notSent`）。
- 引擎选项来自 hub 的 `engines`，名称和 `note` 原样保留；客户端不能用固定枚举筛掉 Orca 或后续引擎，也不能因为不认识当前 `engine` 就丢掉检测列表。
  已知引擎允许离线配置；未知引擎只有 hub 回报 `available: true` 才能选择。重新生成演示数据时检查结构差异，只保留当前任务的变化，避免顺带改动收件箱和定时任务。
- 弱网 / 断网不弹横幅，标题位换成「转圈 + 等待网络 / 连接中… / 更新中…」（`docs/ios.md`「连接状态」）。`.toolbar(removing: .title)`
  会连 `.principal` 的工具栏项一起藏掉：没有标题的页面要显示状态，就去掉它、只放一个平时为空的 principal 项（不会露出 navigationTitle）。
  大标题页面的 principal 项平时也要留空，否则标题出现两遍。弱网判断只认及时回来的 pong（晚到的 pong 也算慢），不然标题每次 ping 都闪一下。
- 形象（`docs/persona.md`）可能是第三方角色：角色的图和生成物只放 hub 的 `~/.jarvis/personas`，不进仓库、不进 App 包。
  换形象、主题色、界面风格都按 `AppModel.look` 重建整棵视图树，推出来的页面放在 `@State` 的导航路径里会被重建关掉：设置的路径放在 `AppModel.settingsPath`，
  所以选择界面能放在推出来的「设置 › 外观 › 形象 / 主题色 / 界面风格 / 布局」页（2026-11-01），重建后还停在那一页
  （`PersonaPickerTests`、`LookPickerTests` 检查）；iPhone 新外壳里设置是 sheet（`AppModel.shellSettings`）：RootView 的 `.id(look)` 换了，已经弹出的 sheet 不会跟着重建，没有输入的视图（主题色页的小样）还是旧颜色，所以 sheet 的内容自己也挂 `.id(model.look)`（`LookPickerTests.testShellSheetStaysOnAppearance` 比较选色前后小样气泡的像素）。别处弹出的 sheet 里要是也有会换 look 的东西，照这样做。别的页面要放会换 look 的东西也照这样把路径放进 AppModel。DEBUG 启动参数打开某页（`-openAppearance` 等）的「已经打开过」标记同理不能放 `@State`：重建会清掉它，路由再跑一遍，就把用户后来进的页面换回启动参数那页（`SettingsView.debugOpened` 是 static，一次启动只跑一次）。
- 外观的三个选择互不影响（2026-10-31 用户要求）：形象不改颜色、主题色不改组件、界面风格不改主色。以前形象的颜色直接叠在风格上（`StyleSpec.tinted`），
  选了黏土再选形象，紫色就被盖掉。新加的视觉东西先想清楚属于哪一个：画法归界面风格，颜色归主题色（`docs/ios.md`「设计语言」）。

- 手动配对成功会替换 RootView 中承载配对 sheet 的根页面；旧 sheet 不能再调用 dismiss，否则新引擎页可能可见却点不动（XCUITest 的 hit point 为 -1）。
  只有原本已配对、没有进入引擎步骤、外观根标识也没变化时，原来的 sheet 宿主仍在，才需要显式 dismiss。
- **按 item id 做 key 的全局缓存会串会话**（2026-11-02，#372）：`RowMemo`（用户 / 助手消息的解析结果）和 `ToolTextMemo.input` 是所有聊天共用的，
  原来只按 `item.id` 做 key，前提是「同一个 id 下文本不变」。Claude 的 id 是记录行的 uuid，全局唯一，一直没出事；DSH / pi 的 id 是 `<seq>#<块>`，
  Codex 没有 id 的行是 `line<seq>#n`，只在会话内唯一。DSH 里从同一处分出的两个分支，第一条分叉的消息序号相同、id 相同：iPad 先看了一个分支，
  再打开另一个，后者那条显示成了前者的（网页版没有这层缓存，所以正常；服务端推送和分页都对）。现在 key 是「机器/会话 + id」（`MemoScope`），
  聊天页用 environment `rowMemoScope` 往下传、挂在 body 最外层（它弹出的 sheet 也在里面），预热按同一个 key 填。新加按 id 的缓存照此办理。

