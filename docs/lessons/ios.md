# 经验：iOS

踩过的坑、怪异行为和当时的取舍（原来在 `CLAUDE.md`「经验」，索引见 `docs/lessons/README.md`）。新经验写进对应的节，没有合适的就新开一节。

## 聊天列表与滚动

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

## 导航

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

- 「请求」类的状态（别处让一个页面推某页、打开某个浮层）要带序号并记下处理过的那个（2026-11-01 新外壳 `ShellState.request` / `AssistantView.handledRequest`）：
  页面用 `onChange(…, initial: true)` 接它，从推出的页面返回时偶尔又执行了一次，把收件箱又推了回去（3 次里 1 次，UI 测试 `-test-iterations 3` 才抓到）。

- 想让系统导航栏里的东西和自己画的浮动顶栏对齐，别放进 `.toolbar`（2026-11-01 新外壳的「助手 | Agents」）：`.principal` 项会被两边宽度不同的按钮挤离中间，
  而且会被包进导航栏共享的玻璃里（自己的胶囊外面又套一层，选中的那块颜色也变了；`.sharedBackgroundVisibility(.hidden)` 能去掉玻璃，挤偏去不掉）。
  Agents 页的做法：导航栏只留给 `.searchable` 的搜索框（principal 放一个透明的点盖住标题），自己的顶栏（`AgentsTopBar`）盖在上面，和助手页用同一组组件。
  搜索激活时它要让开，见下一条。
- 浮在系统导航栏上的自画顶栏（新外壳的 `AgentsTopBar`，导航栏留着只为 `navigationBarDrawer` 的搜索框）：搜索一激活，系统把搜索框升进导航栏那一行，
  正好在顶栏的按钮下面，两层叠着谁也点不全（TestFlight 70「点击搜索组件直接冲突」）。搜索时不画自画顶栏（`searching` 为 true），让系统的搜索框和「取消」占满那一行；
  这种叠压 XCUITest 的 `exists` 看不出来，要比 frame 是否相交（`ShellSearchTests`）。

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
- `.safeAreaBar(edge: .top)`（Agents 列表的快速筛选条）别和会随滚动收起的 `.searchable`（导航栏抽屉默认 `.automatic`）一起用：
  真机（TestFlight 35）上滚动收起搜索框后，筛选条还停在搜索框展开时的位置，中间空出约 120pt 雾化的列表（「顶部一大坨」）；
  当时改成搜索框 `.navigationBarDrawer(displayMode: .always)` 常显；2026-11-01 用户嫌顶部太挤，改回 `.automatic`（滚动收起、下拉出现），
  手机上筛选条不再放 safeAreaBar，而是列表的第一行、跟着列表滚（`ChatsView` 的 `filterBar`），宽屏分栏里照旧在列表上方。
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
- 量拖动 / 动画每帧的主线程耗时别在 XCUITest 下量：它打开无障碍，SwiftUI 每帧多做很多事。App 里用脚本按显示帧驱动同一条代码路径，
  `simctl launch` 启动后量（`DrawerAutoDrag`，`docs/testing.md`「侧边栏拖动」）。

## Swift 与编译

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
- 发版机器的 Swift（6.3.3）和别的开发机不一样，Release 归档会报「unable to type-check this expression in reasonable time」：
  长修饰符链里的多语句闭包（`.task { … }`）会和整条链一起推断类型，报错的行号还会跟着改动乱跳。把闭包体挪成方法、id 挪成计算属性（`ChatView.prewarmRows`）。
- Swift 按位置先读出参数再调用后面的参数：`emit(s, i, word(…, &i, …))` 里传给 `emit` 的 `i` 是 `word` 改它之前的值（文件查看器的宏 `println!` 少了 `!`）。
  一个参数要改另一个参数用到的变量时，先把结果存进变量。
- 改构建产物 Info.plist 的脚本阶段（设置 › 关于 › 构建 的 commit，`project.yml` 的 `Stamp the commit`）要把 `$(TARGET_BUILD_DIR)/$(INFOPLIST_PATH)`
  声明成输入，否则 Xcode 可能先跑脚本、后重写 Info.plist，写进去的值就丢了（bundle id 一变就会重写，2026-10-31）。脚本要读 `.git`，
  这个 target 关了 `ENABLE_USER_SCRIPT_SANDBOXING`。

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
- 机器页面的计数和列表不算「对话」tab 助手的无头会话（`MachineDetailView.agents`，和 Agents 列表一致）：它们跑在 hub 那台机器上，
  以前「服务器」卡片的「运行中」把它们也算进去了。
- 体验模式（`docs/ios.md`「体验模式」）挂在 `APIClient` / `LiveConnection` 下面（`DemoHub`），页面代码不写 `if demo`：新页面要的接口在
  `DemoHub` 里没有就是 404「体验模式里没有这项数据」，加接口时顺手在 `DemoHub` 回一份、跑 `DemoModeTests` 看 `DemoHub miss` 日志。
  演示数据改 `scripts/mock_hub.py` 后重跑 `scripts/make_demo_data.py`。会碰到电脑 / 飞书 / 邮件的操作一律不真的做（`DemoText.notSent`）。
- 弱网 / 断网不弹横幅，标题位换成「转圈 + 等待网络 / 连接中… / 更新中…」（`docs/ios.md`「连接状态」）。`.toolbar(removing: .title)`
  会连 `.principal` 的工具栏项一起藏掉：没有标题的页面要显示状态，就去掉它、只放一个平时为空的 principal 项（不会露出 navigationTitle）。
  大标题页面的 principal 项平时也要留空，否则标题出现两遍。弱网判断只认及时回来的 pong（晚到的 pong 也算慢），不然标题每次 ping 都闪一下。
- 形象（`docs/persona.md`）可能是第三方角色：角色的图和生成物只放 hub 的 `~/.jarvis/personas`，不进仓库、不进 App 包。
  换形象和换风格一样按 `AppModel.look` 重建整棵视图树，推出来的页面放在 `@State` 的导航路径里会被重建关掉：设置的路径放在 `AppModel.settingsPath`，
  所以选择界面能放在推出来的「设置 › 外观 › 形象 / 界面风格 / 布局」页（2026-11-01），重建后还停在那一页（`PersonaPickerTests` 检查）。别的页面要放会换 look 的东西也照这样把路径放进 AppModel。
