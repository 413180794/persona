# 经验：iOS

踩过的坑、怪异行为和当时的取舍（原来在 `CLAUDE.md`「经验」，索引见 `docs/lessons/README.md`）。新经验写进对应的节，没有合适的就新开一节。

- 共享视图引用的辅助扩展也必须编进每个目标：watchOS 10 兼容把 `Color.mix` 换成 `blended` 后，扩展若仍留在 `Theme.swift`，
  只编入 `PersonaFace` 的灵动岛小组件会在 Release 归档失败。辅助扩展随 `PersonaFace.swift` 编入每个目标；不要把整套 App 主题拖进扩展，也不要在两个文件里重复声明。

## HTML 沙箱的异步加载与销毁（#2025）

规则表编译是异步的；只在 `dismantleUIView` 调 `stopLoading()`，不能阻止尚未完成的编译稍后重新加载页面。每次加载取消旧任务并更新代次，编译返回后同时检查取消状态和代次；销毁时撤掉脚本、message handler 与 delegate，再清空页面。用可控暂停的规则表加载器，分别验证旧编译晚完成和销毁后才完成，不靠偶然的编译速度。

卡片位于保留页面的外壳中时，`onDisappear` 也不代表已滚出屏幕。执行条件同时检查滚动可见性、`tabIsActive` 和 `scenePhase`，条件不成立就移除 WebView。bridge 每次加载换实例标记，旧页面排队的消息不能在更新后生效；这不等同于对脚本提供硬性 CPU／内存配额。

## 无内容的平台替身用 EmptyView（#2001）

visionOS 没有 iOS 的抽屉手势，`DrawerPan.NoRowSwipe` 只需作为无内容背景。用 `Color.clear.frame(width: 0, height: 0)` 会引入不必要的固定尺寸；iOS 的 token 检查扫描共享源码，不会因为它位于 visionOS 分支就忽略。直接返回 `EmptyView()`，既不绘制内容，也不占布局空间；不为这个空替身新增零尺寸 token，不提高基线或添加豁免。

## 对话输入区的收键盘按钮与模型显示（#2200）

- 2026-11-05：App 的收键盘图标有两个来源：新外壳的 `ShellComposerBar`（主对话和 Agents 共用）以及旧布局的 `KeyboardDismissChip`。按用户要求删除时要同时移除旧布局仅为按钮创建的条件行，不能用透明占位替代；`composerTyping` 仍控制侧边栏布局的页头折叠，`Keyboard.dismiss` 与 `Theme.Icon.dismissKeyboard` 仍供手势和终端键栏使用，不能全局删除。
- GPT 名称格式化只发生在显示层，版本和后缀都保留。模型选择项用计算属性提供友好名称，`AgentModel.id`、`name`、配置值和请求编码保持原样；独立的自定义名称优先。不要修改通用 `Fmt.model`，也不要让原本只去掉供应商前缀的非 GPT 标签被顺手缩写。UI 回归在旧实现上先确认失败，再验证两种布局、主对话／Agents、键盘展开／手势收起及长标签下的按钮尺寸。

## 刷新任务不能在请求完成前改变自己的键（#1860）

扩展页收到 `invalidate` 后把 `stale` 作为 `.task(id:)` 的键，刷新一开始就清掉 `stale`，SwiftUI 会取消这个任务正在等待的网络请求。现象是 mock 已收到重画请求，App 却一直保留旧页面；四种外观下都失败，单测直接调用 `refresh()` 则测不出来。

任务键改用失效版本，请求结束后再处理标记。取消的请求不消耗失效事件；请求期间收到新事件不能被旧请求清掉。操作忙碌时暂缓，在操作结束后重试。用可控的异步响应测试取消和并发事件，再用真实 App 的 WebSocket → 重画 UI 测试验证完整链路。多 hub 的失效事件还必须带所属组，不能只按扩展 id 匹配。

## 扩展管理的确认与草稿（#1861）

- `TimelineView` 直接放在 `List` 中，内部暂时没有内容，也可能生成一条空白行。预览有效期不需要每秒重绘：整页任务等待主包和成员的最早到期时间，到点更新确认状态。取消后不更新，提交前仍检查实际时间；用真正的等待到期 UI 测试验证按钮不可用。
- `List` 里一个初始为空的视图若只返回 `EmptyView`，挂在它身上的 `.task` 可能根本不启动。模板菜单依赖请求结果才出现时，应由非空的整页加载，再把模板传给菜单；真实 UI 测试能发现「接口可读、单测通过，入口却始终不出现」的差别。
- `confirmationDialog` 可能先把 `isPresented` 的绑定写回 `false`，再调用按钮动作。按钮不能重新读取已被清掉的可选项来决定是否执行：模板动作捕获选中的模板，连带停用动作由确认按钮明确传入 `cascade`。取消不会产生第二个请求。
- 外观变更通过 `RootView.id(look)` 重建视图；把设置导航留在 `AppModel` 只能保住所在页面，不能保住页面自己的 `@State` 表单。未保存草稿放按 hub／扩展隔离的内存存储，重新读取保留草稿，成功保存后清掉。未解析成数值的输入也算草稿，不只比较配置 JSON。
- 动态 `Color(light:dark:)` 的 UIKit 提供器每次创建都有不同身份，颜色数值相同也不代表 SwiftUI 的 `Color ==` 相等。把静态角色色改成计算属性时，默认色仍用静态缓存，扩展角色色每次组合 `StyleSpec` 只创建一次；不能靠把现有颜色测试改成 RGB 比较来掩盖持续创建颜色对象的问题。
- 合集的主预览没过期不代表成员都有效：检查每个成员的有效期、未解决依赖及权限摘要。卸载返回 `removed_members` 时只报告实际成功的成员，不能从主包卸载成功推断全部成员都卸载了。

## 视频导出

- 2026-11-04，#1045：生成的灰色视频在真实压缩测试中触发 `EXC_BREAKPOINT / SIGTRAP`，调用栈位于 Swift AVFoundation 的 `AVAssetExportSession.ProgressStates.Iterator.StateInternal.init`。
  不要把进度观察器的崩溃当成编码校验失败，也不要跳过 H.264／时长断言。改为可取消地读取 session 的标量 `progress`，导出成功或失败后都取消并等待观察任务结束，再报告完成或抛出错误；已经取消的操作不启动导出。
  验证真实 MP4／H.264、时长、成功时最终进度、取消时不写文件，以及失败后不再回调。`progress` 在新 SDK 上虽已弃用，但仍可用，保留现有 Xcode 26.0 兼容政策；实际崩溃环境是 Xcode 27.1。
  轮询也要保持 session 的 actor 所有权：只把可 Sendable 的文件 URL 传给内部 `ExportJob` actor，由它创建并独占 asset／session；观察任务访问 `self.session`，与异步导出使用同一隔离。不要把整个非 Sendable SDK 对象强行标成 `unchecked Sendable`。

## 聊天列表与滚动

- 新外壳的侧边栏菜单、会话名和正文用同一档 Dynamic Type，不给整个侧边栏套 `.shellScaled()`：它会额外放大一档（标准设置下 17pt → 19pt），
  即使两边都写 `Theme.Font.body` 也不一样。「Persona」标题保留 `display` 层级，字号仍跟随系统和设置的小 / 标准 / 大。

- `List` 换序、上面插行或改行高后保持的是滚动偏移，不是读者看着的那一行（2026-11-02，#339）：停在 Agents 列表中间时一次推送让上面的空间换了名次，
  屏幕中间的空间整个换成了另一个，测试里那一行直接不见了。要保住就自己做：行用 `onGeometryChange` 报窗口里的位置，改数据前记下中间那一行，
  排完按差值改 `contentOffset`（`ReaderAnchor`）；在最顶上、手指按着或正在滑时不做。两个坑：换序后 `List` 过一会儿才按真实行高重排上面的行，要在一小段时间里
  每次报位置都校；那一行被挪出屏幕时格子被回收、`onGeometryChange` 不再来，只有 `onDisappear`，要先 `ScrollViewReader.scrollTo` 把它滚回来再校。
- `ScrollView` + `defaultScrollAnchor(.bottom)` + `LazyVStack`：跳到很远的 id 会因为上方懒加载视图实际高度与估算不同而漂移，验证历史渲染用 `-fromSeq`。
- 翻转的聊天列表别让它贴安全区：上方垫 1pt 空视图、输入框放在下方的 VStack 里；用 `ignoresSafeArea` + 按 inset 补边距会在键盘弹起时白屏、顶部错位。
- 聊天列表用倒置（ScrollView 和每行各 `scaleEffect(x:1,y:-1)`）：最新消息在偏移 0，跳到最新永远精确。倒置后要自己处理安全区
  （`ignoresSafeArea(.container, edges: .vertical)` + `contentMargins` 按实际 inset），并关掉 iOS 26 的 `scrollEdgeEffect`（否则整页发雾）。
  只往偏移 0（最新端）跳；往远端用代码跳、或加 `defaultScrollAnchor(..., for: .sizeChanges)`，懒加载估算不准时照样白屏。
- 倒置的聊天列表上 `.scrollDismissesKeyboard(.interactively)` 不管用（2026-11-05，#1823）：键盘开着上下滑动对话，键盘一直不收。
  在 `onScrollPhaseChange` 里进入 `.interacting`（真的在拖）时 `Keyboard.dismiss()`；`.tracking` 不收（可能还是一次点击，列表里的「其他回答…」输入框会被收掉），
  流式内容把列表顶动是 `.animating`，也不收。正着的列表（助手首页、新外壳首页、GitHub 会话）不受影响，不用加。
- 聊天列表（LazyVStack + 底部锚定）偶发白屏：按估算高度跳到底会落在没渲染的区域。跳转要按哨兵 id、等布局完成再跳，并在哨兵真正可见前重试；
  `atBottom` 不要默认 true（白屏时就没有箭头可点）。验证：脚本批量冷启动打开会话，按对话区像素标准差判断白屏。
- 冷启动回到保存的阅读位置（2026-11-03，#597）：冷启动时手里只有最近两轮（磁盘缓存 `ChatCache.tail` 两轮，hub 的第一页 `turns=2`、订阅的 backlog 也一样），
  往上翻过的那一行不在里面；原来找不到就放弃、停在最底部（同一次运行里切回来是好的，翻过的页还在内存）。现在找不到又还有更早的历史就往上翻页找（每页 10 轮，是 node 的上限；最多 10 页，约 100 轮以外的就放弃、停在最新）。
  找的过程中离开不删保存的位置（屏幕上还是缓存的最底部），一页失败或没带来新条目就停。
  判断「不在」要等行建好：`ChatStore.loadedOnce` 在页合并时就变 true，行要等后台重建完，看 `backlogBuilt`；翻一页后等 `isRebuilding` 变 false 再看。
  mock 默认一次给全部历史，测不出这件事：`/mock/paging?on=1` 让它像 hub 一样分页（`ChatFollowTests` 的冷启动用例开着它，再用 `/mock/delay` 让缓存先上屏）。
- 用 `scrollTo(id)` 跳到离最新很远的地方后，底部哨兵的 `onScrollVisibilityChange` 不一定报「不可见」（懒加载把它整个移走了），`atBottom` 停在 true：
  落地完以为在最底部、重新跟随，下一条回复把人拽回最新（#597）。落地结束时离最新端超过一屏就把 `atBottom` 置 false（一屏以内哨兵还活着，回调可信）。
- 长列表里不要把一组可能很多的行包成一个子视图（LazyVStack 里的一个 VStack）：几百行一次布局会卡死主线程、页面空白，要拆成独立的懒加载行。
- 行能自己展开收起的列表（话题卡片的「展开」）别放进 `LazyVStack` + `scrollPosition`（2026-11-04，#1087）：点「展开」后 App 再也不空闲
  （iPhone 模拟器上 60 秒查不到界面；iPad、矮一点的卡片碰巧能停），只在聊天先打开过一次（有缓存、一开就是全部话题）时出现。
  逐项去掉排查：动画、底部锚定、跟随到底、`.alignment` 锚点、写回位置都不是，去掉 `scrollPosition` 绑定或换成 `VStack` 才好；
  推测是懒加载按估算高度摆屏幕外的行、量出真实高度再摆，停不下来（没看到 SwiftUI 内部）。话题群改用 `VStack`（`LarkChatStack`），
  要「上屏才做」的事改用 `onScrollVisibilityChange`。上游后来让列表按屏幕上的行保位置（`scrollTargetLayout`、`LarkHistoryHold`）之后，单跑这条测试不卡了，
  全套里接在别的测试后面照样卡（11/04 晚），所以不能靠它。和上一条不冲突：话题群一页 40 个，加载更早每次再加 40，到几百个才会撞上它。
- SwiftUI 的 `onScrollPhaseChange` 在内容流入、偏移被程序调整时也会进 `.animating`：只想响应用户滚动就判断 `.tracking` / `.interacting`。
- 跟手的收起 / 展开（Safari 式工具栏）不要做成「切换状态 + 动画」：把进度（0…1）放在单独的 `@Observable` 里、只给那一小块视图读，
  滚动回调每帧写它；动起来的那块浮在内容上（overlay），别让内容的 frame 跟着变，否则每帧重排整页、消息跟着跳。松手再吸附。
- 对话行的 body 会被反复求值（滚动、流式更新）：从消息文字推出来的东西（Markdown 解析、路径 / 链接识别、去掉标签后的文字）
  按消息 id 缓存（`RowMemo`，`TextMemo`），并在消息加载后放到后台预热；别在 body 里跑正则 / NSDataDetector（实测是主线程最大的热点）。
- 列表行的 `onAppear` / `onDisappear` 里别写页面的 `@State`，哪怕是界面上不显示的记账（2026-11-03，#674）：
  飞书会话页每进来一行都写「已看到」和上报用的计时任务，每出去一行都写回执的记账，滑动时整页 body 一直重算，掉帧多了一倍。
  不显示的记账放进 `@State` 里的一个 class（`LarkChatBook`），写它不会让视图失效；会显示的（新消息计数、回执）仍然放 state，只在真的变了时写。
- 滚动时别在主线程建 WKWebView：链接预加载只给刚出来的回复（15 分钟内），翻历史时每条旧回复都去预加载曾造成 1–2 秒的冻结。
- 滚动中写入的簿记（可见行、手指是否按着）放在不被观察的对象里（`ChatScratch`），写 @State 会让整页重算；派生的行列表按
  `ChatStore.version` 等缓存。WebSocket 消息在回调线程上解码，别回到主线程再解。
- 聊天列表用 `ScrollPosition.scrollTo(y:)` 会把横向偏移也归零：左右边距是 `contentMargins(.horizontal)`，整页会贴到屏幕边。
  竖向跳转用 `scrollTo(point:)` 带上当前的 x（`ChatView.scrollList`）。`scrollTo(id:anchor:)` 跳到没画过的行只按估算高度落点，
  要精确停在某处就跳过去后按实测位置再修正一两次。倒置列表里的行，XCUITest 拿到的 frame 不对（报在屏幕外），量位置用 App 内的 DEBUG 探针。
- 正着放、输入框在 `safeAreaInset(edge: .bottom)` 里的聊天列表（飞书会话页、话题浮层，2026-11-03，#672）：输入框的 inset 是算进去了的，挡住最后一条的是这几件事：
  - 打开在某条消息时用的是 `defaultScrollAnchor(.top)`，键盘、引用条把底部空间占掉时 offset 不动，最后一屏压到输入框下面。
  - 发送后 `scrollTo(id: 新行, anchor: .bottom)` 对齐的是行顶处一个约 7pt 高的框，不是整行：56pt 和 78pt 的行停在同一个 offset。
  - `ScrollPosition.scrollTo(y:)` 从内容顶部算起，`ScrollGeometry.contentOffset` 从 inset 顶部算起（顶部是 −上 inset），两者差一个上 inset。
  - 键盘改 inset 的同时发起的带动画 `scrollTo` 会被吞掉：phase 进了 `.animating`，offset 却不动。
  - `LazyVStack` 报的内容高度在两个估计值之间来回跳（实测差 39pt），按它算出来的 offset 会差一截。
  - 做法（`LarkFollow.swift`）：跟随时用 `defaultScrollAnchor(.bottom, for: .sizeChanges)`，滚动视图自己知道行的真实高度；不在底部时补一个不带动画的 `scrollTo(edge: .bottom)`。原来的锚点按 role 拆开（`.initialOffset`、`.alignment` 不变）。
- 正着放的聊天列表（`LazyVStack` + `scrollTargetLayout` + `ScrollPosition`）在顶上插一页（2026-11-04，#1033，模拟器实测）：
  滚动位置按 id 保持的是第一个滚动目标，那是列表顶上的「历史」头而不是消息，正在读的消息被推下去整整一页（这一页的高度，像素对得上），还立刻又挨着顶、再要一页；
  `defaultScrollAnchor(.bottom, for: .sizeChanges)` 也没用（和插入同一次更新里切换的锚点只管之后的变化，提前切也一样）；把头挪出 `LazyVStack` 包一层 `VStack` 直接白屏。
  做法（`LarkHistoryHold`）：页落下时从 `onScrollTargetVisibilityChange` 取屏幕中间的一条当锚，只给这一行挂 `onGeometryChange` 报它在内容坐标里的位置，
  每变一次就按差值直接改 `UIScrollView.contentOffset`（同 `ChatPin`）；内容坐标不随滚动变，手指和惯性照常，下面新到的消息不影响。别按整个内容高度的变化补：锚那一行自己的时间分隔条挪走了也算在里面，差 58pt。
- `onScrollGeometryChange` 只在值变了时回调，第一次的值不报：内容不够一屏的会话一开始就「接近顶部」，只用一个 Bool 永远等不到回调，一次都不自动加载（#1033）。值里带上内容高度，首次布局就会报。
  反过来，首屏落地、账号确认、回到前台这几个时刻列表还没到它打开的位置：带着待发消息重新打开会话时，列表先在最顶上（offset 等于上 inset）停了一下，立刻要了一页历史（日志实测）。这些时刻都等列表落定再按滚动视图的实际几何判断（`requestHistoryWhenSettled`）。
- 列表底部「加载更新」的转圈靠 `onAppear` 翻页（2026-11-05，#1034）：加载完一页它还在屏幕上，`onAppear` 不会再来，停在中途；
  换成 `.task(id: 游标)` 又走到另一头：懒加载列表把屏幕外不远的转圈也建出来，任务照跑，从第一条未读打开会一路加载到最新。
  做法：`onScrollVisibilityChange` 记下它是否真在屏幕上（不被观察的 `LarkChatBook`），在屏幕上才要，要完一页等它排好还在屏幕上再要下一页。
- 换了整个窗口（回到最新换回本机尾页）再 `scrollTo(edge: .bottom)`，滚动停下那一刻 `larkFollowsEnd` 按当时的几何重算「是否跟随」，
  新窗口还没排好就算成不在底部。要判断「用户有没有自己离开底部」，看手指（`onScrollPhaseChange` 进入 `.tracking` 或 `.interacting` 计一次：有的拖动不经过 `.tracking`），别看那一刻的几何。
- 倒置聊天列表往上翻时的「冻结」不能按行 id 截：行 id 会随轮次推进变（答案收进工作行、多条消息的轮合并成 `wt-`），
  找不到 id 冻结就失效、可见内容被顶走（修前实测 319pt）。按「离开最新端时的 item 快照」重建行（`ChatView.Frozen`），验证用 `-showFreezeProbe`。
- BeautifulUI 的组件放进聊天列表前，先看它每次出现时做了什么。懒加载列表滚动时会反复重建行，每次出现都起 `.task`、挂入场动画（`buiEntrance`）的组件，
  在长工作卡里滑动会明显掉帧：ThinkingState 原样放进 650 步的工作卡，Debug 模拟器上卡顿时长从约 11 涨到约 21 ms/s，去掉这两样后约 13.7。
  这类问题改在 BeautifulUI 仓库里，再整个拷进 `apple/Packages/BeautifulUI`（`VENDORED.md`），不在 Persona 里改库代码。
- 倒置聊天列表里点开 / 收起的东西要按住表头（`ChatPin`，2026-10-31）：长出来的内容会把表头和它上面的一切往上推（思考跳 40pt，工作卡跳约 440pt、第二下点到别的步骤上）。
  表头按「同一次布局里的屏幕位置 + 内容坐标」报位置（内容坐标不随滚动变），偏移 = 点下时的偏移 + 表头在内容里挪了多少；别用回报的 offset 算（晚一帧就过冲、来回跳）。
  `ScrollPosition.scrollTo(point:)` 先落在要的点、过一会儿又差一个内容边距（10pt）、再回来；存着的 `ScrollPosition`（`edge: .top`）在内容变化时会被重新套用，
  工作步骤从 hub 到达时列表跳回最新端一帧。按住期间直接设列表 `UIScrollView` 的 `contentOffset`（`ChatPin.Finder`），开始时清掉存着的位置。
  `scrollTo(id:anchor:)` 的 anchor 对的是整行，打开的工作卡整张算一行，钉不住表头。只有比屏幕高的对话能完全按住（`MOCK_LONG_HISTORY=1`）。
- hub 一直在推（会话列表、机器、对话的新步骤），每次推送都会重算读了这些数据的视图，正在用的东西别挂在它们上面（`docs/archive/lanes/live-updates.md`）：
  工具栏写在跟着数据重算的视图上，每次推送都重新交给 UIKit，打开的 ⋯ 菜单滚动位置被重置（菜单内容不变也一样）：`ChatsView` 只管导航和工具栏，列表是 `AgentsList`
  （拆开后 `ChatsView` 的 body 要读一下 `searching`，否则从搜索结果打开的对话又顶着系统导航栏，`SearchOpenTests`）；
  body 里写的闭包环境值（`.environment(\.openFile) { … }`）每次都是新值，读它的行全部重算（每次推送重算所有可见的过程步骤），用一次建好的 `ChatActions`；
  `model.session(key)` 按会话单独观察（`SessionCell`），别让一个会话的页面跟着所有机器的列表重算；全屏查看器、行的展开状态放在页面上，别放在懒加载的行里。
  量法：`LiveUpdateTests`（mock 的 `/mock/churn` 连续推，`-perfCounts YES` 报每轮的 body 次数和主线程耗时）。
- 角标（等你处理的数字、收件箱的「需要你」）几乎每次推送都变，读它的视图别装着页面或菜单（2026-11-01，TestFlight 70「又回来了」，`docs/archive/lanes/live-updates.md`）：
  `Tab` 的 `.badge` 一变，`Tab` 就把里面的页面重建一遍（页面没有参数也一样，body 再跑），开着的 ⋯ 菜单回到顶部、开着的对话整页重算：
  `Tab` 里的页面一律包 `TabPage { … }.equatable()`。`Menu` 的 label 一变，开着的菜单就重新布局、回到顶部：数字、红点别放在菜单的 label 上，
  做成自己读数据的小视图放在旁边（`WaitingBadge`），装菜单的视图不读每次推送都变的东西。mock 要推 `inbox` 和 `interaction`
  （`/mock/churn?inbox=1&waiting=1`）才测得出来；新外壳是 iPhone 的默认布局，这类测试两种布局都跑（`TEST_RUNNER_JARVIS_LAYOUT=shell`）。
- 行视图的参数里有闭包或带字符串、数组的结构体（`Item`），SwiftUI 就认为它每次都变了：对话页每次推送重算一次，屏幕上加懒加载缓冲里的每一行
  （长的 Persona 主对话约 45 行）都跟着重跑 body（2026-11-01）。对话行（气泡、回复、工作行、输入框）一律 `Equatable` + `.equatable()`，只比消息本身；
  一直建着、平时看不见的东西（关着的侧边栏）别读 `allSessions` 这类每次推送都换的数据，看不见时用上次的值。
  流式回复每 80 ms 来一次全文：写完的块切开各自缓存（`StreamChunks`），只重画最后一块。量法见 `docs/testing.md`「Persona 页（长主对话）」。
- 列表别按「最近一次更新」排，滚动中也别让推送碰到它（2026-11-01，TestFlight 77，`docs/archive/lanes/live-updates.md`「滚动时被迫刷新」）。
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
  往自己的 hosting controller 里传环境别用 `.environment(\.self, context.environment)` 整份覆盖（2026-11-04，#995、#1006）：整份里还带着外面那个 hosting controller 自己的项
  （渲染到哪个根视图 `RenderingRootViewKey`、当前的 hosting controller、工具栏上下文），island 自己的被盖掉。在对话页里（导航栈的 hosting controller 下面）
  island 里的东西整个不在无障碍树里：输入框、+、发送都是一个没有子元素的 `Other`，VoiceOver 和 UI 测试都够不到（每次启动都这样，`ComposerAccessibilityTests`）。
  现在按值传（`IslandEnvironment`：AppModel、shell、shellChat、buiTheme、字号、配色、语言、方向、tint），island 里的视图要读页面设的别的值就加进去。
  侧栏（`DrawerSidebarHost`）也是整份传，但它在根上，树里正常。查法：两边各把 `String(describing: env)` 写到文件，比 key 和值（只有宿主相关的几项不同）；
  试过不行的：把 hosting controller 放进一个容器控制器、去掉 `EquatableView`、去掉量高度的 `fixedSize` / `frame`、在根上加 `.accessibilityElement(children: .contain)`。
- 输入框「只显示最后一行」先量截图里卡片的高度（2026-11-03，#909）：真机上换行后卡片已经是两行高，字只有一行、下面空一行，
  是 TextField 底下的文本视图（SwiftUI 的 `VerticalTextView`，一个 `UITextView`）还停在往下滚了一行的位置，不是没长高。
  模拟器上系统键盘、拼音的 marked text、自己写的第三方键盘扩展都复现不了；`ShellComposerBar` 在高度变化后把有焦点的文本视图
  超出内容末端的偏移拉回来（`TextViewScroll`，偏移在内容范围内时什么都不做）。别用隐藏的 `Text` 量行数来定高度：
  `Text` 不压缩全角标点，输入框压缩，折行位置差一两个字。
- 发送时别只读 TextField 的绑定，先确认输入法正在拼的字（2026-11-05，#2203）：用微信输入法在主对话打一段话点发送，发出去只有前一半，
  换别的键盘就没事。第三方键盘常把还没确认的字（往往已经是汉字，带下划线，看着像打完了）留成 marked text，SwiftUI 绑定不保证跟上键盘扩展的每一步；
  发送一清空输入框，键盘还可能把没确认的字再放回空框。现在三个输入框（`ShellComposerBar`、`ComposerRow`、`PromptComposer`）发送前都走
  `TypedText.commitFocused`：有焦点时先 `unmarkText()`（和点别处一样确认），再拿文本视图里的字更新绑定，然后才发。
  模拟器上复现不了微信输入法本身：照 `testing.md` 的办法做了一个只有键盘扩展的 App（按钮调 `setMarkedText` / `unmarkText` / `insertText`），
  一步一步来时绑定都跟得上（marked text 也在绑定里）；同一个按钮里连着发十几次 `setMarkedText` 时，SwiftUI 的输入框和普通 `UITextView` 的结果都乱，
  是键盘扩展通道本身的问题，不能当成复现。单测（`TypedTextTests`）直接用 `UITextView` 的 marked text 和「绑定落后」两种情况。
  键盘列表可以不进设置：`simctl spawn <udid> defaults write -g AppleKeyboards -array <扩展 bundle id> …` 和 `com.apple.Preferences KeyboardsCurrentAndNext` 写上，App 一启动就是它；
  系统拼音写 `zh_Hans-Pinyin@sw=Pinyin-Simplified;hw=Automatic`，候选时空格键的标签是「选定」。
- `List` 的行别为了 `Equatable` 包成自定义视图、再把 `.swipeActions` / `.contextMenu` 放进它的 body：试过（2026-11-01），推送和滑动都更慢
  （UIKit 那边每次都要重新配置 cell 的滑动操作）。Agents 列表推送的开销靠合并推送降下来（`AppModel.applyPendingSessions`）。

## 导航

- 文件预览 `.sheet` 里的 Markdown 会继承会话页的 `openURL`（2026-11-03，#796）：点链接只改了底层会话的链接状态，链接抽屉被文件抽屉挡住，关文件后才露出来。当前展示层必须自己拥有链接状态和 presenter，并覆盖 `openURL` / `openLink`；不要先关闭文件或给底层 presenter 延时重试。传递原会话 key，关闭链接保留文件与阅读位置，两种文件入口、普通网页和端口转发都要点测。

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
- `.removed`（`.logicallyComplete` 也一样）的完成回调要等**所有**接了这次动画的视图都跑完；接了动画的视图如果在窗口外（导航栈里被推出页面盖住的根页，包括弹簧还没停就被盖住的），
  动画不跑、也就不结束，回调一直等到它回到窗口（2026-11-03 用户报：从收件箱的「需要你」点进 Agent 对话后，「助手页面变收件箱，划不动点不动」，对话顶上卡着切换器）。
  横条一直 `moving`，两页都不接点按，`ShellMovingBar` 的切换器一直盖在对话上；返回收件箱、根页回到窗口那一刻回调才来（trace 里晚了 3.7 秒）。
  接了动画的是各页自己的顶栏：它随 `pager.page`（由 position 算出）出现 / 消失，切换器的药丸读 position。改成：各页顶栏的出现消失不带动画（`ShellBarAtRest` 包一层
  `.transaction { $0.animation = nil }`；包在 `if` 里面没用，要包住 `if`），药丸只有滑动时顶替的那条跟着 position 走，各页顶栏里的停在 `shell.page`（滑动时它们本来就透明）。
  只在被盖住时关掉、或者只给药丸套 `.transaction` 都不够：直接观察 position 的子视图不吃祖先的 transaction 改写。`ShellPager.settle` 另有 1.2 秒兜底（trace 记 `settle lost`）。
  切换器自己也读了 `pager.page`（选中的那段是 Menu 还是按钮），一样接了动画：模拟器负载高时（并行跑另一台）还是 `settle lost`，兜底一结束 `moving` 一变、切换器重画，回调 20ms 后才来。
  现在切换器只读 `moving`（动画外设置），不读 position 算出来的任何东西。规矩：各页顶栏里的东西只读 `moving` / `lingering` / `shell.page`，不读 `position` / `pager.page`。
  试过不行的：一个 0 尺寸的 `Animatable` 视图逐帧报位置（不在屏上画就不逐帧算 body）。查法：trace 里 `slide` 之后很久才 `settled`，或者有 `settle lost`（`ShellInboxAgentTests` 断言没有）。
- 浮在最上层、又不能挡点击的东西（toast）放进 scene 里自己的一个 `UIWindow`：`windowLevel = .normal + 1`，`hitTest` 返回 nil，点击全落到下面的窗口；
  外壳的 sheet、抽屉、推出的页面都在它下面（2026-11-04，#1053，`ToastWindow`）。放在 SwiftUI 的 overlay 里会被 sheet 盖住，`allowsHitTesting(false)` 也挡不住 UIKit 部分。
  要自己带上的：深浅色（建窗口时抄外壳窗口的 `overrideUserInterfaceStyle`，之后 `ColorSchemeChoice` 改 scene 的所有窗口）、App 自己的字号
  （`textSize.applied(to:)`，新窗口只跟系统）、`AppModel` 和 BeautifulUI 主题；VoiceOver 不会移到不接触摸的窗口上，出现时 `AccessibilityNotification.Announcement` 读出来。
  XCUITest 照样能在 `app` 的树里找到它。位置别贴着状态栏：那里是外壳的顶栏，会盖住切换段；放在顶栏下面（`ShellNavigationClearance.needed(at:)`）。
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
- 分栏里左栏点另一条，换的是右栏那一页的参数，同一种页面不重建（2026-11-03，#885）：`@State`、`.task`、订阅都还是上一条的，
  只有直接读参数的标题、占位字跟着变。收件箱从一个飞书会话点到另一个，标题换了，中间还是上一个会话的消息，框里没发的草稿也留着。
  左栏打开的页面要按条目挂 `.id`（`InboxDetailView(…).id(item.id)`，Agents 的 `ChatView(key:).id(key)`、Mac 收件箱早就这样）；
  只给 `.task` 加 `id:` 不够，草稿、引用、已读位置这些 `@State` 不会重置。`InboxSplitTests` 覆盖（iPad、iPhone Duo 打开时跑）。
- iPhone Duo（`docs/ios.md`「iPhone Duo」）：按宽度（size class）分布局，不看机型；模拟器开合只能在 Device Hub 里切，
  截图要 `--display=1/3` 指定屏幕，否则可能拍到熄灭的那块（全黑）。
- 推出的页面别用 `.toolbar(.hidden, for: .tabBar)`（`.toolbarVisibility` 一样）藏 tab 栏：它在 push 的第一帧就把 tab 栏拿掉，
  底下列表的导航栏和搜索框同一帧跟着消失，点进对话「闪一下」（2026-10-31）。用 UIKit 的 `setTabBarHidden(_:animated:)` 跟着页面出现 / 消失
  （`hidesTabBarWhenShown`）；`hidesBottomBarWhenPushed` 在 SwiftUI 里设得太晚，不生效。这种闪动 XCUITest 看不出来：
  `xcrun simctl io <udid> recordVideo` 录下来，用 ffmpeg 把点击后 0.3 秒的帧拼成一排看。
- 飞书聊天从隐藏系统导航栏的消息列表推入时，iOS 27 的自动 scroll-edge 背景会在动画结束后才加上模糊，缓存图片会先清晰透到标题后面（#952）。
  单独 `.toolbarBackground(.visible, for: .navigationBar)` 不够；聊天页同时指定 `.toolbarBackground(.bar, for: .navigationBar)`，保留系统磨砂材质。
  用已有缓存、导航栏下面有彩色图片的会话录制中间帧，空白加载页的截图看不出差异。底栏继续用 `hidesTabBarWhenShown`，并回归取消右滑时仍留在聊天、完整右滑回到原入口。

## 布局与组件

- 横向滚动里给 chip 限宽，别在背景外面套 `frame(maxWidth:)` / `scaledFrame(maxWidth:)`（2026-11-05，#3010）：`ScrollView(.horizontal)`
  不给宽度建议，可变 frame 把这个「不限」原样传给里面，chip 按完整标题量出宽度、背景也按这个画，frame 只把自己的占位夹到上限；
  于是背景和占位对不上：短一点时 chip 之间的空隙变大，系统字号调大后标题不截断，capsule 溢出占位、压到相邻 chip 上。
  限宽用 `scaledWidthCap(_:)`（`WidthCap` 布局：先给里面上限宽度的建议，自己的大小就是里面报的大小），放在背景外面。
  `DockPillLayoutTests` 用 `ImageRenderer` 在 chip 两边留透明的槽、按像素查背景没画进槽里，几档字号都跑。

- 品牌标志大小不一（2026-11-03，#1003）：同一列里 `Image(…).resizable().scaledToFit()` 撑满一样的方框，看起来却差一圈。
  原因在资源：飞书的 SVG 画布 88×88 里鸟只占 70% × 56%、还偏右，Telegram、Microsoft 是满版的；`scaledToFit` 只对齐画布不对齐图形。
  做法：裁掉资源的留白（viewBox），统一经 `BrandMark` 画，满版正方形再按光学大小缩一点；量法是 `rsvg-convert` 渲染后找不透明像素的范围，
  截图后按 @3x 像素量方框和中心，不靠肉眼。
- 按「菜单里有没有项」决定画不画 ⋯ 时，别用 `items is EmptyView`（2026-11-03，#778）：ViewBuilder 里写 `if 条件 { Button … }`，
  条件为假时得到的是值为 nil 的 `Optional<…>`，不是 `EmptyView`，判断落空，画出一个点开什么都没有的 ⋯（Persona 侧栏「置顶」只置顶了主对话时）。
  `HeadingMenu.isEmpty` 只认这两种：`EmptyView`、单个为假的 `if`（nil 的 `Optional`）；几个 `if`、`if` / `else`、`ForEach`、`Group` 即使什么都不画也算有项，
  菜单这样写时要自己传「有没有项」。藏掉 ⋯ 的那一行保留 ⋯ 的高度（`HeadingMenu.minHeight`，两种界面风格都要），免得下面的内容跟着跳。
- `converter.localLocation` 是相对挂手势的那个视图的；拿它去摆放在上层容器里的东西（操作条挂在整行上，手势挂在气泡上）会差出气泡在行里的偏移，
  自己的气泡靠右，操作条就跑到左边（Mac 和 iOS 一样，#501，2026-11-03）。给容器设命名坐标系，手势用 `converter.location(in: .named(…))` 报位置。
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
- `contentMargins` 作用于它下面所有的滚动视图，包括 `safeAreaInset` 里的：Persona 页给悬浮顶栏留的上边距写在输入框的 inset 之后，
  输入框上面那行横向滚动的「权限 / 思考」也多了顶栏的高度，约 66pt 页面色挡住建议卡片，所有风格都有（2026-10-31，#114）。边距写在要它的滚动视图上、inset 之前。
- UI 测试的「返回」别用 `navigationBars.buttons.firstMatch` 兜底：已经回到列表时导航栏上第一个按钮是「新建会话」，点了就弹出新建浮层，
  后面的查找全跑进浮层里，报的是「找不到某一行」，像是演示数据过时（2026-10-31 `DemoModeTests`）。先等目标页可点，再排除页面自己的栏按钮。
- `TextField(text:selection:)` 的选区：没在编辑（未聚焦）时也会报一个选区，冷启动恢复草稿后它指在草稿中间；而且它的
  `String.Index` 可能来自另一份字符串，越界的 `utf16Offset(in:)` 直接崩。插入文字（发送文件的路径）只在聚焦时用选区，先夹到 `endIndex` 再换算（`PathInsert`）。
  UI 测试里草稿会跨次运行保留，测输入框内容前先清掉。
- Swift Charts 的线要在空档处断开：给每段一个不同的 `series:` 值（「指标#段号」），同一个 series 的点总会连起来。
  `AxisValueLabel` 不显式给颜色时，日期刻度会染成 App 的 tint（蓝色），要写 `.foregroundStyle(Color(.secondaryLabel))`。
  落在 `chartXScale(domain:)` 之外的点照样画（线会伸出纵轴、卡片边），要 `.chartPlotStyle { $0.clipped() }`。
- 一页上十几个小图表时别用 Swift Charts（2026-11-04，#390）：每个 `Chart` 自己的布局（比例尺、绘图区）就很贵，Mac 上机器页 12 个迷你曲线每次进页面多花约 130 ms；
  换成向量化的 `LinePlot` / `AreaPlot` 一点没省（开销按图表算，不按点数）。没有坐标轴、不用交互的小图在 `Canvas` 上自己画（`Sparkline`）。
- SwiftUI `Menu` 的 label 上挂的东西（`.overlay`、`.surface` 玻璃）都会变成菜单按钮的一部分：玻璃铺满整个 label（头像连名字套进一个大玻璃框），
  overlay 里的文字 XCUITest 也找不到（按钮只报它第一次的 label）。要临时盖在按钮上的提示放在和 Menu 并列的 ZStack 里（`AssistantTopBar.face`）。
- 自己画的浮动顶栏要盖住状态栏的雾，背景用 GeometryReader 读 `safeAreaInsets.top` 往上偏移，直接 `ignoresSafeArea` 的背景高度会被算短。
  整页横向拖拽（Persona 的侧边栏）用 UIKit pan + `gestureRecognizerShouldBegin` 在开始时判方向、让给能横向滚动的子视图，SwiftUI 的 DragGesture 做不到这种让位。
- `Chip` 是 `fixedSize` 的，不会缩：放进窄的网格列（空间页一行两张卡）里，长标签名会把卡片撑得比列宽，整个 `LazyVGrid` 挤出屏幕、
  第二行错位（2026-10-31）。窄处的一排 Chip 用 `ViewThatFits` 逐级去掉次要的、最后让主标签截断（`SlotCard.header`），验证用 mock 的 `MOCK_LONG_SLOTS=1`。
- 几万行的文本别放进一个 UITextView（`MonoTextView` 整篇排版，随大小线性变慢）：文件查看器用 `CodeLinesView`，只把屏幕上的行做成 label、滚动时复用，
  行号固定在左边，2 万行的 Go 打开约 0.3 秒、滚动 60 fps。MarkdownUI 渲染很长的文档要切成段放进 LazyVStack（`MarkdownChunks`），
  而且别开 `.textSelection(.enabled)`：实测打开慢约 40%，每段进屏的长帧从约 40 ms 变成约 60 ms（`docs/ios.md`「文件查看器」）。
- 视频预览不能把「识别成视频」和「完整传输」当成一件事（2026-11-03）：旧文件接口给二进制最多 2 MiB，MP4 的索引又可能在末尾，只给播放器前一段仍然打不开。最初用有界完整下载恢复可播，随后改成鉴权 Range + `AVAssetResourceLoader`：直接读取尾部索引和目标关键帧，分片写入稀疏缓存；不能把稀疏文件的逻辑长度当成已下载量。跨请求用 `If-Match` 固定版本，不能用文件变更后会退回完整 200 响应的 `If-Range`。验收必须用长视频验证首播未全量、实际 seek 区间、播放继续与关闭清理；`seek` 的 completion 和时钟瞬间跳到目标不代表对应画面已解码，区间断言要等恢复播放后再检查。
- 消息里的路径只在那台机器上真的有时才做成可点的文件块（`FilePresence` 批量问 `GET …/stat`，不确定就不链接）：以前别的机器的路径（hub 的
  `~/.jarvis/…`）也能点，点开只说「打不开这个文件」。

- 自己判断方向的 `UIPanGestureRecognizer`：`gestureRecognizerShouldBegin` 被调用时 `translation` 还是 0，`velocity` 是最后两个采样的猜测
  （一次 30° 的斜划读成 (1, 68)，几乎竖直向下），按它判断就会把斜着的拖动交给列表。要在识别器子类里记下按下的位置，按实际位移判断
  （`DrawerPan.Recognizer`，2026-10-31「划了没反应」）。外层的 pan 和里面的 ScrollView 是赛跑：斜一点（40°）的拖动，竖直分量先过滚动视图的门槛，
  外层连问都没被问（日志里什么都没有，测试时好时坏）；横向滚动的代码块在边缘也会抢去做回弹。让滚动视图的 pan 等外层先判断
  （`gestureRecognizer(_:shouldBeRequiredToFailBy:)`），外层在同样的几个点内判断完，滚动不会变晚。
- 跟手的拖动里别切 tab 栏（`.toolbarVisibility(_, for: .tabBar)`）：它改页面的安全区，整页（导航栈、输入框、每个 UITextField）在拖动第一帧
  重新布局一次（Persona 侧边栏 20–40 ms）。要让它让开就淡出：UIKit 的 tab 栏控制器会在 SwiftUI 刷新 tab 栏时把 `tabBar.alpha` 改回 1，
  淡出 tab 栏外面只装着它的那层容器（`AssistantDrawer.applyToUIKit`）。
- SwiftUI 的 `.accessibilityHidden(true)` 藏不住里面由 UIKit 承载的部分（TextField、ScrollView 和其中的行）：XCUITest 照样找得到，
  VoiceOver 也会走进去；`opacity(0)` 也一样。一直建着、平时藏起来的大块界面（Persona 侧边栏）放进自己的 `UIHostingController`，用 UIKit 的
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

- 新外壳两页叠在一起（`ShellPageSlide`），藏起来的那页只靠 `accessibilityHidden` 出不了无障碍树：它的顶栏切换是 Menu、按钮是玻璃，
  列表是 UICollectionView，都是 UIKit。在助手上推一页再右滑返回之后，Agents 的整页（导航栏、列表、顶栏）又回到树里，盖在助手的顶栏上面，
  看不见却「可点」：VoiceOver 会落到它上面，UI 测试按 id 取到的第一个 `shell-agents` 是它，点它的中心哪一段都不是（2026-11-01，
  `ShellSwitchTests` 四个用例）。藏起来那页的顶栏现在不在树里（`ShellBarAtRest(page:)`：横条停在另一页时不画，一开始滑就放回来、透明）；
  列表还会漏进树里，判断哪页在显示用状态探针（`probe.shell`），别用「某个控件 exists / isHittable」。
- 把一个控件搬进 List 当第一行（Agents 的筛选条，56b2ead，为了搜索框能收起）之后，它就是一个 UICollectionViewCell：凡是「从行上起拖就让给行」
  的手势判断（`DrawerPan.onRow`，行的右滑是置顶 / 中断 / 标为已看）都会把它当成行，沿筛选条右拖翻回助手就失效了。没有自己右滑的行要显式标出来
  （`DrawerPan.NoRowSwipe`）。

- 同一件事别留两个来源再靠 `.onChange` 同步：新外壳「哪页在前」原来是 `AppModel.selectedTab` 和横条的位置两份，靠外壳的
  `onChange(of: selectedTab)` 去滑横条。SwiftUI 有时不重新求那个 body（追踪里 tab 改了、之后没有 body 也没有 onChange），
  横条不动，两份就此分叉，之后的点击全错（2026-11-02，`docs/shell.md`）。要跟着一个值做事，就在写它的地方当场做（`didSet` 里调
  `tabRouter`），或者只留一个来源让大家都读它。
- 「已选中的段再点打开菜单」这类按状态变成另一种控件的按钮：状态一旦和屏幕对不上，按钮就变成别的东西（空菜单），用户只看到点了没反应。
  决定它是什么的状态必须就是屏幕上显示的那个（新外壳：横条静止在这页才是菜单）。

- 反复出现又消失的小条别用 `if … { view.transition(.opacity) }` 插入：外面同时有根控制器 `additionalSafeAreaInsets` 的 UIKit 动画时，
  第二次起插进来的视图停在透明，body 照常跑、页面也让出了位置，就是看不见，VoiceOver 也读不到（透明的不进无障碍树）（2026-11-02，新外壳的状态行）。
  一直留着它、只动 opacity（收起时画上一次的内容），每次都能显示。
- 按状态插入 / 拿掉的可点视图（`if moving { 顶替的顶栏 }`）：状态在手指按着的时候变了，视图被拿掉，按在它上面的那一下就没了，
  Button 的 action 不会调，下面露出来的同一个按钮也收不到（触摸开始时落在被拿掉的那个上）。新外壳的横条刚好在点击中途停下时就是这样，
  动画慢的 BeautifulUI 下几乎每次连点都丢一下（2026-11-02，`testQuickTapsEndOnTheLast`）。拿掉要等手指抬起（`ShellPager.lingering`）。
- 新外壳两页都建着，藏在后面那页的 `onAppear` 照样会跑：从 Agents 启动时助手页的对话「出现」了，订阅、标为已看、
  DEBUG 开关都跑了，用户却没看到它（2026-11-02，ovis 车道看到 `appeared()` 连着跑、消息重复发）。这类有副作用的 `onAppear` 别指望「出现 = 看到」：
  页面没选中就别建（`ShellState.assistantBuilt`），或者改用 `.onTabVisibility`。DEBUG 开关也别挂在 `onAppear` 上做一次性的事。

- SwiftUI 的 `NavigationStack` 底下的 `UINavigationController` 和 `List` 底下的 `UICollectionView` 归 SwiftUI 管：从 UIKit 改它们的
  `additionalSafeAreaInsets`、`view.transform`、`contentInsetAdjustmentBehavior` / `contentInset`，SwiftUI 下一次排版就改回去，或者用它自己记的位置把 offset 设回去
  （行跳 150–180pt）。SwiftUI 里给它 `ignoresSafeArea` 或负的 `safeAreaPadding`，它改用容器的 frame 来让位，开合中 frame 在 0 和 90 多之间来回跳（2026-11-02，#413
  状态行收起时搜索框闪动，六种做法的逐帧数据在 issue 里）。先用 `probe.agents-scroll` 这类逐帧探针量清楚是谁在动，再决定改哪一层。

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
- `[String: Any]` 字面量里写 `arr.last ?? 0`，Swift 6.2（Xcode 26.0）会把 `arr.last` 当成方法 `last(where:)`，存进去的是一个闭包；
  `JSONSerialization` 碰到它就抛 `Invalid type in JSON write (__SwiftValue)`，App 直接崩。`FrameStats.stop` 因此在 Xcode 26.0 的构建里每次都崩，
  `-showPagePerf` 一轮都量不出来（2026-11-03，#674）。#343 用新版 Xcode（27.x）量的时候没遇到。放进 `Any` 的值先赋给有类型的常量
  （`let busyMax: Double = busy.last ?? 0`），`FrameStatsTests` 检查写出来的 JSON。
- App 开了 `SWIFT_APPROACHABLE_CONCURRENCY`：`APIClient` 是 nonisolated，但它的 async 方法跑在调用方的 actor 上，
  所以在视图里 `await api.get(…)` 时 JSON 解码在主线程上。飞书会话每次推送都重拉 40 条带卡片的消息，sample 里主线程解码占了 43 个采样
  （2026-11-03，#674 P-19）。大的回复用 `getDecodedOffMain`，或者像 `items` 那样先拿 `Data` 再 `Task.detached` 解码；返回的类型要是 `Sendable`。
### 最低兼容编译器

- **最低兼容的编译器是 Xcode 26.0（Swift 6.2），固定不动**（2026-11-02）：本机和不少开发机还在 26.0，master 必须在它上面编得过（iOS、Mac、手表、各扩展）；发版机器是 Xcode 27.1（Swift 6.4.0），能编不代表 26.0 能编。
  踩过三次：#216 `isDynamicIslandLimitedInWidth`（iOS 27 SDK，#217 修）、`toolbarVerticalEdge`（iOS 27.1 SDK）、#164 `ShowsSnippetView` / `view:`（#430 修）。
- 新 SDK 才有的符号用 `#if compiler(>=6.4)` 包起来（Xcode 27.1 是 Swift 6.4.0；Xcode 26.4–26.6 带 Swift 6.3 但 SDK 还是 iOS 26，用 6.3 判断会在它们上面编不过，#453），`#else` 走旧行为（旧 SDK 上功能退回去，不要空着）；`#available` / `@available` 只管运行时，符号编译时找不到照样报错。
- 改了用到新 API 的代码，有可用 Xcode 26.0 环境时合入前编一遍（`eval "$(python3 scripts/jarvis_config.py env)"` 取 `DEVELOPER_DIR`）。**没有该环境时不强制实编、不因此阻塞合并或发布**（2026-11-03 用户明确要求）：用现有工具链完成构建和测试，PR 写明实际版本及「26.0 未验证」，不用逐次询问豁免；兼容目标和新 SDK 的编译保护仍要保留。
- 要抬高最低版本是产品决定：先问用户，同步改 `CLAUDE.md` 的那一条和这一节。

- 发版机器的 Swift（6.3.3）和别的开发机不一样，Release 归档会报「unable to type-check this expression in reasonable time」：
  长修饰符链里的多语句闭包（`.task { … }`）会和整条链一起推断类型，报错的行号还会跟着改动乱跳。把闭包体挪成方法、id 挪成计算属性（`ChatView.prewarmRows`）。
- Xcode 26.3（Swift 6.2.4）也会在测试夹具的长数组拼接链上类型检查超时（`RFBClientTests.sessionFollowsDesktopSize`）。
  按协议头、矩形字段、编码分段构造 `[UInt8]`，保持原来的字节和断言；不要排除测试来绕过原始 scheme 的构建。
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

- 挂在「可能什么都不画」的视图上的 `.task` 不会跑（2026-11-04，#1517）：`Group { ForEach(空列表) }`、`if 有内容 { … }` 这样的视图没有子视图时，
  修饰符没有东西可挂，加载它的任务永远不开始，列表就一直是空的。负责加载的任务挂在页面上一定在的视图上（或者页面持有的模型里，由页面的 `.task` 触发）；
  依赖 hub 能力（`features`）的任务把能力放进 `task(id:)`：从启动参数直达的页面可能在 hello 之前就出现了。

- 开了 HealthKit entitlement 就必须有 `NSHealthUpdateUsageDescription`（哪怕只读），否则上传报 90683。
- WatchConnectivity 在 activate 完成前 updateApplicationContext 会被丢弃：配对信息要在 activationDidComplete 后再推。
- `UNUserNotificationCenterDelegate` 不要用 async 版本的 didReceive / willPresent：返回时在后台执行器上，UIKit 收到非主线程的完成回调，
  点通知进 App 会崩。用 completion handler 版本，`DispatchQueue.main.async` 里处理完再调 completion。
- 动态通知类别不能在 App 启动时被 `setNotificationCategories(静态集合)` 覆盖。NSE 交付前和 App 前台展示前先注册，静态类别与已注册的扩展类别按 identifier 合并。
  异步注册前将通知内容复制成只读快照；SDK 的通知中心只在注册 actor 内取得，静态类别用 Sendable 工厂在 actor 内创建，不能把非 Sendable 的中心或类别集合从调用方直接传进去。
  NSE 的正常结束和到期回调用锁领取同一个 completion，避免重复交付（#1862）。
- 通知中的动作不能先渲染页面再执行：渲染可能返回新的 `state`，覆盖原通知的状态。系统入口在 AppModel 中保存领取标志，先用原始状态执行一次，再画结果或只读加载；去重键区分 hub、通知 id 和可空的按钮 id，默认点按不等于名为 `open` 的按钮。
  根视图临时显示完整扩展页面，避免与设置 sheet 争夺呈现。`simctl push` 不运行 NSE，本地通知测试不等于真机 APNs 首条送达验证（#1862）。
- 本地通知 UI 夹具先在前台完成动态类别注册、装好后台监听，再向专用 mock 报准备就绪；测试等就绪后才按 Home。不能让测试猜异步注册什么时候完成，或者在 App 已进入后台后还依赖长时间的初始化。
  SpringBoard 通知的无障碍标签可能把标题和正文合在一起，按标题匹配用 `CONTAINS`，不要求整个 label 等于标题。
  系统回复栏在新版本上是 `TextView`，并且通知正文也有 `TextView`；等「发送」出现后选择最下方的编辑器，不能固定按 `TextField` 查找（#1862）。
- 根视图临时换成完整扩展页面时，Shell 的 `ShellWindowHooks` 会拆除并关闭独立的 toast 窗口；RootView 必须在系统扩展页面打开时启用 `ToastOverlay`。只把结果写进 `AppModel.toast` 不足以证明用户能看见，前台动作的 UI 回归要断言 toast 实际出现（#1862）。
- DEBUG 启动参数的名字不能和任何 Swift 标识符同名：枚举 case 名会以反射元数据进 Release 二进制，`release.sh` 的 `strings` 检查会误报
  （`Tab.workbench` 撞了 `-workbench`，已改名 `-openWorkbench`）。新开关用 `open…` / `show…` 这类不会是类型成员的名字，并加进 release.sh 的列表。
  存储属性名也一样（2026-10-31：`AgentsListMenu.Actions` 的 `openMonitor` 属性撞了 `-openMonitor`，TestFlight 50 停在这一步）。改了带新属性的代码，
  先本地 `xcodebuild build -configuration Release -destination generic/platform=iOS CODE_SIGNING_ALLOWED=NO`，对 `Release-iphoneos/Jarvis.app/Jarvis` 跑一遍 release.sh 里的 strings 检查。
- 启动参数的值在参数域里是字符串（`-dev.compact YES` 是 "YES"）：自己读 UserDefaults 时布尔、数字用 `bool(forKey:)` / `integer(forKey:)` / `double(forKey:)`（和 `@AppStorage` 一样认字符串），
  别用 `object(forKey:) as? Bool`，那样启动参数悄悄退回默认值（#1473 换成 `StoredValue` 后 `-dev.compact YES` 失效，`CompactListShots` 拍的细列表其实是卡片，#2147）。
- 实时活动（`docs/live-activity.md`）：ActivityKit 用默认 JSONDecoder 解推送的 `content-state`，`Date` 会按 2001 年起算，时间一律传 Unix 秒数字；
  锁屏 / 展开态限高约 160pt，超出会被静默裁掉；`simctl io screenshot` 拍不到灵动岛和锁屏上的活动（用 UI 测试 `LiveActivityShots`），
  `simctl push` 不支持实时活动推送，但模拟器能收真实的 APNs sandbox 推送（`TestManualLiveActivityPush`）。
  重复的 symbol 动画（`.rotate` / `.pulse` + `.repeating`）在活动里不动（像素比对验证过），别设计「转动的齿轮」这种效果。
  灵动岛上的数字（TPM、token、负载）会占推送预算：hub 只推设备显示的项、取两位有效数字、每台最多每分钟一次。
  和别的 App 的活动共用灵动岛时，iPhone 18 Pro 上两个都是限宽 28pt 的紧凑态（左右两块挤在一起，第二块被截），17 Pro 等是最小态：
  限宽靠 iOS 27 的 `isDynamicIslandLimitedInWidth` 知道，其余（哪种呈现、有没有别的 App）App 都感知不到；紧凑 / 最小态不给宽度，`ViewThatFits` 没用。
  两种机型都要拍（`scripts/app/island_peer.sh` + `LiveActivityShots`，`docs/live-activity.md`「和其他 App 共用灵动岛」）。
- App 和扩展共享的钥匙串组（推送中继的私钥 `.push`；灵动岛的形象 build 92–103 也放过 `.look`）读不到时一律静默退回默认值，模拟器上又总是好的：
  build 92 的扩展签名里一个组都没有（无签名归档 + ad hoc 签名没带 entitlements，`$(AppIdentifierPrefix)` 为空），灵动岛永远是默认形象 + 系统蓝。
  Info.plist 里的组名用 `$(DEVELOPMENT_TEAM).`；动了 entitlements 或签名流程，对导出的 ipa 跑 `scripts/release/entitlements.py check`（`docs/release.md`「entitlements」）。
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
  演示数据改 `scripts/mock/mock_hub.py` 后重跑 `scripts/mock/make_demo_data.py`。会碰到电脑 / 飞书 / 邮件的操作一律不真的做（`DemoText.notSent`）。
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
- 换 App 图标在 Xcode 26.0 + iOS 26.0 模拟器上验证不了（2026-11-03 实测，master 和改动后的分支都一样）：`supportsAlternateIcons` 为 false，
  设置 › 外观 里不显示「App 图标」那一行，从 `-openAppearance icon` 进去点别的图标没反应，`AppIconTests` 在第 44 行失败；
  构建出的 Info.plist 有 `CFBundleAlternateIcons`、Assets.car 里有各图标集。#313 的验证不是在这套环境做的。在 26.0 上只能验证网格和预览，切换要换新的模拟器运行时或真机。
  2026-11-06 在 Xcode 26.3 + iOS 26.3.1 模拟器上也一样（#3203）：编出的 Info.plist 里四个备用图标都在，`supportsAlternateIcons` 仍是 false，
  `AppIconTests` 看到 设置 › 外观 › 形象 那行小字的值是 `unavailable` 就跳过。主屏图标的切换只能真机看；模拟器上能验证的是 App 里的标志、
  `AppIconChoiceTests` 里换形象 → 切到哪张图标、迁移的几种情形（假的 `AppIconSetting`）。


- 可关闭的首页页签不能自己持有设置 sheet（#392）：在该 sheet 里关闭页签后，页签视图会被移除，设置也会一起消失。复用外壳持有的设置 sheet，先设置目标路径，再显示；关闭页签只调整首页选择，设置保持打开。回归必须从当前页签进入设置后关闭自身，并跨重启验证偏好。
- 首页从两页扩为三页时，松手的速度只能决定完成或取消当前方向的滑动。中间页往左拖到一半再向右甩，应回到中间页，不能越过起点跳到左边页；页数、选中胶囊、边界阻尼和抽屉手势要用同一份页数。
- 加页之后再查一遍「哪个方向没有邻页」的假设（#2822）：Agents 行的左滑「关闭」能用，只是因为它原来是最后一页、左拖没页可翻；开了「消息」页后左拖全被翻页拿走。现在左拖看行上有没有 `SwipeRowMark`（有左滑操作的行才让），不再靠「这一页往左没有页」碰巧成立。

## `APIClient.get` 的路径里不能带 `?`（2026-11-03，连接器迁移 M6）

`get("/api/v1/connectors?all=1")` 发出去的是 `/api/v1/connectors%3Fall=1`：路径整个按路径编码，`?` 被转义，hub 回 404，`try?` 吞掉错误，列表就空了（设置 › 连接器 少了 GitHub、信息，编译和单元测试都看不出来）。
查询参数一律走 `get(_:query:)`。是 UI 测试截图对比时看 mock 日志（`MISSING GET …%3F…`）才发现的。

## 多 hub（docs/multi-hub.md）

- 按会话发的请求一律用 `api(key.group)`，别用当前选中组的 `api`（#950）：`sendPending`（outbox 发出 / 重发）曾用 `api`，别的组的会话的消息发到了选中的那组 hub。`api` 只给 hub 级的页面（Persona、收件箱、hub 的设置）用。
- 给线上类型（`Session`、`Machine`、`Interaction`、`SessionKey`）加客户端自己的字段：合成的 `Decodable` 对**有默认值的非可选属性**也要求 JSON 里有这个键，
  旧数据和 hub 的 JSON 一解就失败。存成可选（`hubGroup: String?`）再给一个非可选的计算属性（`group`），nil 时合成的 `Encodable` 也不写这个键：
  默认组的快照、草稿、待发消息的 JSON 和以前逐字节一样。
- `apple/Shared` 里给数据类型写的 `extension` 默认也是 MainActor：`APIClient`、解码路径这些非隔离的地方一用就报「main actor-isolated」，写成 `nonisolated extension`。
- `apple/Shared` 整个编进手表（`JarvisWatch` 的 sources 是 `Shared`）：那里的代码不能用只在 App 里的类型（`IntentFailure` 在 `Jarvis/Intents`），
  改了 Shared 要连手表一起编（`Jarvis` scheme 会带上）。Mac 目标也编 `Jarvis/` 下的大部分文件：只有 UIKit 有的属性（`UIScrollView.isTracking`）在 Mac 上要有兼容
  （`Mac/Compat`），#243 之后 Mac 就因为这个编不过，到 #256 才发现——改了共用代码顺手编一下 `JarvisMac`。
- 组的名字不能跟着「socket 当前连着哪台」走：选路每次可能挑组里另一台，名字就在「公司」「公司 2」之间来回跳；按用户起的名字 → 组里第一台的 `hub_name` → 连着那台的。
- 「只有一组时一像素不变」：多组才出现的工具栏项要在 `.toolbar` 里按条件生成整个 `ToolbarItem`（`if model.isMultiHub { ToolbarItem … }`），
  不要放一个在一组时返回空视图的 `ToolbarItem`——空的项也可能占位、画出玻璃底。列表里的组名靠 `Machine.groupLabel`（一组时为 nil，`title == name`），页面不用各自判断。
- 几台假 hub 各自按自己的启动时间算相对时间：同样的交互在两组里排序不同，UI 测试别按列表位置找另一组的卡片，先用 Agents 的「hub」筛选只看那一组（`MultiHubTests`）。
- 钥匙串读失败不等于没有：`SecItemCopyMatching` 在开机后第一次解锁前返回 `errSecInteractionNotAllowed`（后台推送唤醒、通知扩展都可能遇到），
  以前 `Keychain.load` 把它和 `errSecItemNotFound` 一样当 nil。存配对这类丢了就要重新扫码的东西，要分开「没有」和「读不出」，读不出时别写回、
  更别因为「空」去删；迁移旧项先写新的、读回确认再删旧的（`HubStore`，`HubStoreTests` 用假钥匙串测写失败和读出错）。

## 临时码配对后一会儿就一直「连接中」（2026-11-05，#2766）

- 配对后换设备 token：以前只在 socket 的 `currentConfig` 和配对的 config **整个相等**、按兑换前记下的组找得到 link 时才换。
  socket 连在候选地址、mesh 里另一台、SSH 隧道上，或者兑换期间 hello 把 hub 重新分了组，socket 就一直用临时 token；hub 兑换后 2 分钟断掉它，
  之后每次重连都 401，界面无限「连接中…」。现在按 **token** 换：`adoptDeviceToken` 记下「临时 token → 设备 token」，所有还用旧 token 的 socket
  `renewCredential` 原地址换 token；`LiveConnection.credential` 在每次握手前再换一次，选路读到的旧配置也带不回旧 token。
- 同一个配对链接处理两次（扫码 + 系统相机打开链接、扫码回调连发）：第二次 `pair()` 会把已经兑换过的临时 token 写回注册表，再兑换又是 401。
  兑换过的链接再来时保留设备 token（`DeviceCredentialTrade.keepsDevice`）。
- 握手 401（`URLSessionWebSocketTask.response` 的状态码）不是「连不上」：`LiveConnection.rejected`，标题显示「配对已失效」、详情里「重新配对」，重试放慢到 60 秒（公网会按地址数失败次数封禁）。
- 复现：`MOCK_PAIR_TOKEN=jp_…` 起假 hub，`-openWelcome YES -openPairLink <链接> -openPairLinkTimes 2`；走另一个地址的那条见 `docs/testing.md`「临时码配对」。

## 会话格子显示用，发送别用（2026-11-03，#624）

`model.session(key)`（会话格子）为了少重画，故意不跟 `rev`、`updated_at`、最后一句（`docs/ios-state.md`「第 3 步」）：
拿它显示没问题（读它的页面本来就不显示这几样），拿它**发出去**就会发旧的。点下去要把会话现在的样子发出去（交给 Persona 带最后一句、按 `rev` 拉 transcript）
一律读 `model.latest(key)` 或整份列表（`allSessions`），别读格子。

## 任务卡片停在几分钟前那句话（2026-11-04，#1108）

任务卡片原来经 `TasksStore.sessions` → `model.session(key)` 读会话，格子不跟最后一句，卡片上的文字要等状态变了才换；再加上 Agent 只跑工具时
`last_text` 本来就不动，看着像卡住了。**显示「它现在在说 / 在做什么」的视图读 `model.sessionWithLine(key)`**（只忽略 `rev`、`updated_at`），
这一步取 `activity`（`TaskNow`，`docs/unified-entry.md`「任务卡」）。格子 `session(_:)` 和 `sameForItsPage` 照旧不跟 `activity`，对话页不会每一步都重画。

## 飞书草稿与发件箱（2026-11-03，#688）

- Swift 6.2 对较长的 SwiftUI modifier 链会出现类型检查超时；把聊天内容和导航/生命周期拆成独立的 `@ViewBuilder` 属性。新编译器能过不能代替 Xcode 26.0 验证。
  2026-11-05 又踩了一次：`ChatView.chatScroll()` 在 Xcode 26.3（Swift 6.2.4）上超时，master 也编不过（#2420 顺手修）。拆成 `chatRows`（列表的行）和两段语句（`let list = ScrollView {…}…` 再 `return list.…`）后能过；机器忙时更容易超时，但这次空闲时也稳定复现。
- 草稿不能按 URL / 令牌命名：同一 Hub 组选路或换设备令牌就会丢入口。用组与实际飞书用户 ID，发送前再读账号；旧版没有账号信息的记录由用户确认归属后迁入。
- 不要用不可访问、无限累积的备份来包装“恢复输入”。明确让用户选择清除本机草稿；断开组先使内存队列失效，再删文件，晚到的请求完成不能重建目录。
- 解锁输入框要在本机提交后立刻完成，不能把它包在等待网络结束的 defer 里；第二次发送可能已开始，前一次 defer 会错误解锁第二次事务。
- DEBUG 附件注入必须晚于异步草稿恢复，否则刚选的测试附件会被恢复快照覆盖。聊天读取可与账号确认并行，避免首屏等待账号请求。

## 对话列表：工作行一折叠就白屏，杀掉 App 才好（2026-11-03，#686、#754）

一个新的 Jarvis 对话（折叠起来一屏放得下），一轮还在跑，点开工作行、看一眼工具输出，再从标题折叠：内容区整片空白，拖也拖不动，切页回来也一样，只有杀掉 App 才恢复。
mock 和真 hub 上试了很多种都不出，最后是拿那一轮的真实条目回放（只放 `/tmp`，不进仓库）才看清，再用 `MOCK_ASSISTANT_RUN=alone` 稳定复现：

- **倒着的懒列表在内容一下子变短到一屏以内时，可能什么都不画**：折叠拿走两千多点的内容，滚动视图在 SwiftUI 背后把偏移从很高收回到最新处，
  `LazyVStack` 还按原来的位置排，屏幕上一行都没有（`probe.chat-rows`：3 行、偏移在最新处、内容 203 pt、可见 0 行）。内容比屏幕短，拖不动；
  对话在往上读（扣住新内容）时也不会有新的行来让它重排，于是一直白。跟着最新处时下一次推送会让它重排，所以平时看不到。
  修法：折叠后剩下的一屏放得下时，把列表重建一次（`listBuild`），反正全都看得见，不丢位置。只滚到某一行或某一端（`scrollTo`）没用：它以为已经在那儿了。
- **`ChatPin` 用 `contentSize` 限位，而 SwiftUI 晚一轮才更新它**：工作行刚展开时内容还按展开前算，偏移被限在原地一步没挪，标题之后也不再动，
  没人再纠正：那一帧列表停在还在淡入的步骤上（看着是白的），接着标题被顶出屏幕。修法：放不到位就在保持期内的后几轮接着放。
- **点开、折叠就是用户接过了列表**，跟拖一下一样不再跟着最新处；结束标记的可见性和 `onScrollGeometryChange` 都比 pin 直接改的偏移慢一帧，
  pin 保持期间别拿它们把「跟着最新处」重新打开（以前保持一结束，下一次推送就把列表拉回最新处，标题没了；录屏里「没碰屏幕也来回跳」就是这个）。
- **在最新处展开，步骤不要从透明淡入**（#754）：倒着的列表在最新处时，新行先出现在最新那一端，pin 下一帧才把标题拉回来；那一帧屏幕上只有正从 0 淡入的步骤，看着是白的。
  pin 的上限改成「标题在内容里的远端」之后（`contentSize` 晚一轮），12 次里还剩 1 次；最新处展开时不淡入、直接显示之后 12/12。
- **在最新处展开，标题不能原地钉死**（2026-11-04）：比一屏长的对话里，工作行在最下面，pin 把标题按在原处，步骤全长在输入框后面，点了像没反应；
  点开又算用户接过列表（不再跟随、冻结），这期间到的内容都扣着，再点一下收起、回到最新处时一起进来，整页往上一窜。
  修法：最新处展开时 `ChatPin.hold(rise:)` 允许标题先往上走到顶栏下面，步骤露出来，超出的部分才由列表滚动吸收；中间、上方展开照旧原地不动。
  XCUITest 认为输入框后面的步骤也 `isHittable`，量「标题往上走了多少」才分得出来（`RunningChatTests.testOpeningAtTheNewestShowsTheSteps`，修前 0 pt、修后 436 pt）。
- 查法：XCUITest 给倒着的列表里的行报的位置是错的，点行会点到别处（打开了侧栏），只点标题这类在顶上的、或者用启动参数（`-showFullOutput card`、`-openAllWork YES`）；
  第二个模拟器之间来回测时，关着的模拟器上 `simctl uninstall` 会静默失败，先 `boot`。

## SSH 隧道（2026-11-03，#891，docs/network.md「经 SSH 隧道连 hub」）

- **DEBUG 启动参数传 JSON 会被吃掉**：`-key '{"a":1}'` 这种以 `{` 开头的值，UserDefaults 的参数域按 plist 解析，`string(forKey:)` 拿到 nil。要传结构化的值就 base64（`-seedSSHTunnel`）。
- **配对页带 SecureField 时，iOS 会弹「Save Password?」**：页面一离开（推到「经 SSH 隧道连接」）就弹，挡住后面的点按（UI 测试里表现为「Neither element nor any descendant has keyboard focus」）。token 是一次性凭据，SecureField 标 `.textContentType(.oneTimeCode)` 就不弹。
- **URLSessionWebSocketTask 的 `sendPing` 要有一个挂着的 `receive()` 才会回调**：只发 ping、不再 receive，pong 永远等不到（测试里卡了十几分钟，以为是隧道）。App 的 `LiveConnection` 一直挂着 receive，不受影响。
- **加新的 Swift package 后 xcodebuild 解析失败**：`Revision … for swift-markdown-ui … does not match previously recorded value`。上游把 2.4.1 的 tag 移了，而本机 `~/Library/org.swift.swiftpm/security/fingerprints` 记着旧的；只要不重新解析就没事，一加 package 就全部重新解析。不动全机共用的指纹记录：把已有的 `Package.resolved`（别的 worktree 生成的工程里有）拷进 `Persona.xcodeproj/project.xcworkspace/xcshareddata/swiftpm/`，合并上新 package 的 pin，再用 `xcodebuild -disableAutomaticPackageResolution`。`xcodegen generate` 会重建工程，拷的文件要再放回去。
- **swift-nio-ssh 的连接要等 `UserAuthSuccessEvent` 才算好**：TCP 连上、握手完都不代表能开通道；登录结果靠这个 user event，失败从 `errorCaught` 来。主机密钥校验失败、公钥被拒的原因 NIOSSH 包得看不出来，在自己的 delegate 里记下 verdict 再分类。
- **Glue 两边必须在同一个 event loop**：本机监听的子连接和 SSH 的子通道互相写，隧道用一个单线程的 `NIOTSEventLoopGroup`；listener 的 `childChannelInitializer` 返回的 future 完成前不会读这个连接，所以可以在里面等 SSH 拨号和开通道，不会丢请求的头几个字节。

## 浮层里嵌一个对话页：内容比浮层高时整列被居中、头部被顶出去（2026-11-03，#930）

任务详情（`TaskDetailSheet`）在浮层里放了自己的头部 + `ChatView(style: .dev)`。Agent 同时有两张很高的待处理卡片时，对话页底部那一块
（卡片 + 输入框）本身就比浮层高，`VStack` 整列被浮层居中，上面的头部和关闭按钮、下面的输入框各被裁掉一截；`frame(maxHeight: .infinity,
alignment: .top)` 也拦不住（外层按内容的高度排）。做法：`GeometryReader` 读出浮层的高度，把整列 `frame(height:)` 钉死、顶部对齐、`clipped()`，
放不下时只裁对话页的底部。浮层里嵌页面都这样做，别指望对齐参数。

## 对话页的待处理卡片把顶栏顶出屏幕（2026-11-03，#926）

- 现象：同一个 Agent 有三张待处理卡片（BeautifulUI 的卡片是展开的，一张约 320 pt）时，对话页整页约 1300 pt，屏幕 956 pt：
  对话区被压到 110 pt，顶栏和返回键在 y ≈ -144，点不到。根因是 `mainFooter` 把卡片原样叠在输入框上，没有高度上限。
- **内容比给的高时，`VStack` 报的是内容的高度**，父视图把它居中，于是上下一起出屏幕（界面树里容器是 `{{0, -157}, {440, 1299}}`）。
  要按「页面给了多高」排版时，量 `.frame(minHeight: .zero, maxHeight: .infinity)` 之后的尺寸（两头都给了就等于给的高度，不管里面多高；只给 max 还是报内容的高度），别量 `VStack` 自己，不然是循环。
- 量出来的高度放进一个只有读它的视图才观察的 `@Observable`（`FooterRoom`），别放 `ChatView` 的 `@State`：键盘滑动时每帧都在变。
- **`containerRelativeFrame` 认最近的滚动视图当容器**：卡片自己用它把表单限在「页面的 55%」，一放进卡片区的 `ScrollView`，
  容器就成了这个滚动区，每张卡把自己限在滚动区的一半，按钮被裁掉。外面量出原来的容器长度（`Color.clear.containerRelativeFrame(.vertical)` 放在
  滚动区外的 `background` 里量），用环境值（`cardRoom`）传进去。
- 卡片自己的 `@State` 存着打了一半的回答：放得下 / 放不下用同一个 `ScrollView`，别分两个分支，不然跨过上限时（新到一张卡）打的字没了。
- 「放得下时一像素不变」的滚动区：放得下时 `.scrollDisabled`、`.scrollClipDisabled`（不裁卡片的阴影），超出时才加 `contentMargins` 给裁开的边留阴影。改前改后同一个截图测试逐像素比（`PendingCardsTests.testOneCardAsBefore`，只差时钟和计时），测试里查卡片区的元素都在它的框里（裁掉的会露出来）。

## 发给 Jarvis 的图片在对话里看不到（2026-11-03，#987）

数据一路都在（claude -p 收到 image 块，transcript 和 hub 的条目里都有这张图），丢在 App 归位上：claude -p 写的那一行是**先图后字**
（`[image, text]`），Agents 里粘贴的图是先字后图。App 按用户的文字切每一轮，排在文字前面的图被分进了上一轮、折进上一轮的过程，看不见；
发送中的气泡带着图，transcript 那条一到气泡就被替换，图跟着消失。修法：同一行里用户的图排到文字后面（`ChatStore.picturesAfterTheirText`）。
要放在 `rebuild` 按最后一条消息切开**之前**：只放在 `buildRows` 里不够，切的时候图还在前面，照样落进已经建好的上一段。
另：mock 的 `MOCK_ECHO_SENT=1` 按 claude -p 的顺序把发出的消息写回对话；UI 测试里全新安装的通知权限框什么时候弹不一定，弹着的时候下面的元素都不在树里，用 `addUIInterruptionMonitor` 接住。

## 视频压缩的进度：先拿 states 再进 Task（2026-11-03，#981）

`AVAssetExportSession` 不是 Sendable：`Task { for await s in session.states(updateInterval:) … }` 在 Swift 6 下报
「passing closure as a 'sending' parameter risks causing data races」。在 Task 外面先 `let states = session.states(updateInterval: 0.25)`，
Task 里只遍历这个序列，就能编过。选择器用 `preferredItemEncoding: .current`，否则系统可能先把 HEVC 转一遍才交给 App（4K 视频要等很久），
要不要转码由 App 自己按大小决定（`VideoPrep`，docs/protocol.md「发送视频」）。
## 引用改成 Telegram 式的回复气泡（2026-11-03，#986）

- **气泡和最宽的一块一样宽**：引用块用 `frame(maxWidth: .infinity)` 铺满，放进普通 `VStack` 会把气泡撑到整行宽。
  `WidestColumn`（`DesignSystem/QuoteViews.swift`）的做法：先按各块不受限的理想宽度取最大的，不超过给的宽度；再按这个宽度排每一块。
  没有引用的气泡照旧走原来的 `Text.padding.background`：长文字换行后，新布局会让气泡比原来宽几个点，违反「默认样子一像素不变」。
- **辅助功能的 combine 会改掉测试找得到的东西**：
  - 引用条要是整条 `.accessibilityElement(children: .combine)`，× 就不再是单独的按钮，标题也不再是单独的 static text，
    `LarkSendTests` 用的 `staticTexts["回复 我"]` 就找不到了。
  - 所以只给气泡里整块可点的引用设 label（它本来就是一个按钮），输入框上方的引用条让标题、摘要、× 各自是元素。
- **飞书的草稿和发件箱跨 UI 测试留在模拟器上**：
  - 上一轮没取消的回复存成会话草稿，下一轮一打开回复条就在。它的摘要和原消息文字一样，于是 `label BEGINSWITH` 先匹配到回复条里那一行，长按落空。
  - 上一轮的「#fail」消息留在发件箱里，排在新发的那条下面，「刚发的在输入框正上方」就差了 141 pt。
  - 用例自己收尾（取消回复）；查失败前先 `simctl uninstall` 换一份干净数据，再和 master 比。
- 后台等 `gh pr checks` 的脚本在这台机器上会提前报「完成」，输出却是空的：等待脚本单独写成文件，自己写 log，用 perl 的 `select` 代替 `sleep`。

## Persona 自己的 MCP 工具要在 `ToolPresentation` 里登记（2026-11-03，#677）

对话里的工具行按工具名取标签、参数（`ToolPresentation.verb` / `subject`）和展开的样子（`ToolDetail`），不认识的名字走默认：
标签是原名，参数取输入里第一个字符串值。`ask_user` 的输入只有 `questions` 数组，参数就是空字符串，BeautifulUI 的 `ToolChipRow` 照样画出参数框，
于是成了「ask_user + 一个空灰框」；卡片答完一收，对话里再也看不到问了什么、选了什么，看着像「没显示问题」。
当时线上 hub 的交互记录里问题建了、推了、21 秒后答了，问题出在 App 怎么画。给助手加只给它用的工具（`assistant/tools.go`）时，
顺手在 `ToolPresentation` 里给它起中文名、挑参数；它的结果是给模型看的英文时，展开后换成用户看得懂的（`AskUserDetail` 从结果里读回选了哪项）。

## 「两页共用一个组件」要从调用处查到底（2026-11-04，#1079 → #1091）

#1079 要 Agent 对话页的听写、停止按钮和助手页一样，只把两个按钮抽成了共用组件，以为两页的输入框本来就是同一个 `ShellComposerBar`。
其实 `ChatView` 的 `Composer` 只在环境值 `shellChat` 为真时才用它，而这个值只有 `AssistantView` 给自己的 `ChatView` 设；Agents 页推进来的是同一个
`ChatView`、默认值 false，走的是 `ComposerRow` / `PromptComposer`。结果按钮一样了，位置不一样（麦克风在胶囊外面），TestFlight 上一眼就看出来。
说两处「共用」之前，从两个页面各自的调用处往下看一遍走的是哪个分支（环境值、风格、旧布局开关都可能分叉），截图也要两页并排拍同一个状态。

### 「有没有在线的 Agent」不能只看会话状态（#1110）

电脑断连时 hub 不把它的会话标成 offline，而是 unknown，`AgentState.isLive` 仍为真；待办详情的「当前没有 Agent」只看状态，电脑断了还以为有人在做，网页却会显示（它还要求电脑在线）。
另外 `model.allSessions` 是所有 hub 组的会话，只比电脑 id 和项目名会把别的组里同 id、同名项目的会话算进来。凡是「这台电脑 / 这个项目现在有没有 Agent」的判断：先 `isOnTodosHub`（或对应的组判断）筛组，再看 `model.machine(...)?.online`，最后才看会话状态。

## 消息左滑引用：横向手势和列表滚动、返回手势怎么共存（2026-11-04，#1058）

- **SwiftUI 的 `DragGesture` 放在列表的行上会抢列表的滚动**，加 `simultaneousGesture` 又会两边一起动。
  - 做法：UIKit 的 `UIPanGestureRecognizer` 子类（`LeftDragRecognizer`），经 `UIGestureRecognizerRepresentable` 挂上。
  - 在 `touchesMoved` 里，走过 10 pt 还不是往左的横向拖动，就把 `state` 设成 `.failed`。
  - 覆盖 `shouldBeRequiredToFail(by:)`：对 `UIScrollView` 的 pan 返回 true，让滚动等它失败了再开始。
  - 竖着滑时，它在 10 pt 内就失败了，滚动只晚开始这一点。这只碰 recognizer 子类自己的方法，不用设 `delegate`（SwiftUI 管着它）。
- **从左边缘往右滑返回**是往右的手势，在第一次判断时就让它失败，所以不受影响。
- **UI 测试里的边缘返回**：要从 x = 0 起、用 `.fast`。master 上从 1 % 处慢慢拖，系统也不认。
  - 回到列表后，搜索框是收起的（#1052），不能用 `searchFields` 判断回到了列表，看 `agents.filter.` 的 chip。
- **在列表还在滚（刚打开对话时跟到最新）时拖一条消息**：第一下触摸只会让列表停下，这是 iOS 的正常行为。测试里先等列表停稳再拖。
- **顶栏闲着几秒就收成细条**（`chromeIdleSeconds`）：顶栏里加了按钮，细条里也要有，不然 UI 测试和用户在细条状态下都找不到。
- **行上的手势要等子视图的手势失败**：`UIGestureRecognizerRepresentable` 只能用 `.gesture` 挂（`.simultaneousGesture` 要的是 SwiftUI 的 `Gesture`，编不过）。
  - SwiftUI 让它等子视图上的 `.gesture`（消息的长按 `LongPressAt`）先失败。拖动偶尔输掉这场竞争，压根不开始（旧布局 + BeautifulUI 4 次里挂 2 次）。
  - 做法：在 recognizer 子类里，对 `UILongPressGestureRecognizer` 不 `require(toFail:)`，`shouldRequireFailure(of:)` 和 `canBePrevented(by:)` 都返回 false。往左一拖就超出了长按允许的移动，两者不会同时成立。改完 4 次全过。
  - UI 测试别用 `.slow` 拖：慢到 0.35 s 内没走出 10 pt，长按就先成立了（真人拖得快）。
- **视图读的开关别标 `@ObservationIgnored`**：`MessageMenu.swipeQuotes` 在对话页的 `onAppear`（`bindMenu`）里才打开。
  - 一开始标了 `@ObservationIgnored`，`onAppear` 之前画好的行不会重画，于是没有挂上手势，十次里挂两三次。
  - 手势的日志显示失败那次的 recognizer 一个触摸都没收到，这才看出来。改成被观察以后十次全过。

## 压缩上下文画成一条分隔线（2026-11-04，#1064）

Claude Code 压缩一次写三样：`system/compact_boundary`（`compactMetadata.trigger` 是 `manual` 或 `auto`，空闲压缩也记 `auto`）、紧跟一条 `isCompactSummary` 的用户行、
空闲压缩再多一条 `informational`（"Compacted while idle…"）。hub 转成两条提示加原样的说明，App 以前各画各的，还因为排在回答之后被折进这一轮的工作行，看不见。
现在 `ChatStore.compactions` 把它们并成一条（`Item.compact`），`ChatLines` 把回答之后的压缩放在回答下面、只有压缩的一段不再包成工作行，`CompactDivider` 画细线。
踩到的坑：一轮里除了回答，别的都进工作行，所以「回答之后才来的」提示会被收进折叠的工作行里，单元测试只造了没有工具的一轮就没发现，要连工具一起造。
另一个坑：点开时 `ChatPin` 把标题按在原处，分隔线是对话最后一行时，卡片往下长就到了输入框后面（矮屏上露一半）。点开后再调 `ChatPin.show` 把卡片底边滚到列表底边以内，标题不出屏；行在最后、点开往下长的都该这样。

## 换了主题色，设置的行图标还是系统蓝：List 行图标取窗口的 tintColor（2026-11-04，#1188）

除 BeautifulUI 以外的风格，`SettingsRow` 是系统的 `Label`，不读传进来的颜色，图标跟着 tint 走。RootView 的 `.tint(Theme.accent)` 只写进 SwiftUI 的环境；
`List` 行图标是经 UIKit 画的，有时取的是所在窗口的 UIKit `tintColor`，窗口没设过就是资源里的 AccentColor（系统蓝 `#007AFF`）。
设置 sheet 开着时换主题色（整树按 `look` 重建，sheet 留着），或换完后进出二级页，行图标就成了系统蓝，同一页的文字按钮和 × 却是新颜色。
现在 RootView 在 `look` 变化时也把各窗口的 `tintColor` 设成主题色（`WindowTint`）；默认的液态玻璃主色本来就是系统蓝，默认外观一像素不变。
复现：设置首页在屏幕上时 `scripts/app/personactl.py set-style --color coral`，取行图标的像素看是不是 `#007AFF`。经界面进「主题色」页选色再返回这条路在模拟器上不一定复现。

## iOS 27：套了玻璃效果的视图，形状外面点不到（2026-11-03，#1005）

- 现象：BeautifulUI 风格的输入框（`PromptComposer` → BeautifulUI `PromptBar`）的「+」「/」「@」、模型菜单画出来了，点菜单项没反应；
  #519 加的 `BeautifulStyleTests/testCommandMenuTakesTaps` 在 iOS 27 模拟器上失败（iOS 26 上当时是过的）。
- 根因：`PromptComposer` 给整个 `PromptBar` 套了 `.surface(…)`，在玻璃风格下就是 `glassEffect`；菜单画在输入框上方的零高度槽里、溢出到玻璃形状外面。
  iOS 27 上一个视图套了 `glassEffect`（不管 `interactive` 与否），它形状外面的子内容就收不到点击。排查时依次试过：footer 加 `zIndex`（没用）、
  `interactive: false`（没用）、去掉玻璃（好了）。
- 修法：玻璃画在后面（`.background { Color.clear.surface(…) }`），不包住会溢出的内容；改前改后同一个画面逐像素比，输入框只有抗锯齿级的差别。
- 推广：会往外溢出的菜单、气泡别放进 `glassEffect` / `.surface` 包住的视图里；玻璃用 background 画。

## 在 body 里写 `@Observable` 的存储属性，这个 body 就依赖上了它（2026-11-04，#1204）

DEBUG 构建启动后主线程常常一直 100%：RootView 的 body 每次新建 `ShellView()`，`@State` 的初始值 `ShellState(...)` 每次 init 都会算一遍
（SwiftUI 只留第一份），它让自己的 pager `slide`，`slide` 在 DEBUG 下 `ShellTrace.shared.note(...)`。`events.append` 走的是宏生成的 `_modify`，
里面先 `access(keyPath:)` 再改，于是 RootView 的 body 一边写 `events` 一边登记了读取，写完就失效、重算，再新建一个 `ShellView`……永远停不下来
（runtime CLI ping 不通、XCUITest「Timed out while evaluating UI query」都是它）。`sample <pid>` 里能看到这条链。
只给别人看、自己不读的记录（trace、计数）：存储标 `@ObservationIgnored`，读取的计算属性里手动 `access`，写入时包在 `withMutation` 里，
调用它的 body 就不会依赖它。另外，`@State` 的初始值表达式不要有副作用，它不只算一次。

## 侧栏展开时卡片圆角外侧发灰：`.mask` 之后的 `.overlay` 不受遮罩（2026-11-04，#1247）

页面推开成圆角卡片时，卡片上那层「压暗 + 点一下关闭」写的是 `Color.black.opacity(0.06)`，放在 `.mask { card }` 后面的 `.overlay` 里。
modifier 是按顺序包的：遮罩只裁它前面的内容，后加的 overlay 还是整块矩形，于是四个圆角外侧的侧栏底色也被压暗了一层，像露出了别的灰色底。
画在卡片上的东西（压暗、描边）一律用卡片自己的形状（`card.fill(…)`、`card.strokeBorder(…)`），点按区域可以另用 `.contentShape(.rect)`。
核对时别只看整张截图：裁出圆角附近放大、取圆角外侧和侧栏同一行的像素比（`-openAssistantSidebar YES|0.5`，浅色 / 深色）。

## QuickLook 看 Office 文件（2026-11-04，#1300）

- **xlsx 只显示文件里存的公式值**，不会自己算：没存值的公式格子是空的（合计行一片空白）。生成工作簿时要把值一起写进去（`personad office` 写的时候就算好，`docs/office.md`「公式的值」）。
- docx 里只写在表格样式里的边框、`startOverride` 起始编号、分页符（显示成方块），QuickLook 都不认；做法见 `docs/office.md`「怎么验证的」。

## 快捷指令运行「问 Persona」，App 意外退出：Release 去掉了反射名字（2026-11-04，#990）

- 现象：TestFlight 包里，快捷指令跑「问 Persona」（问题已经写死）就「“Persona” App 意外退出」；Debug 包、模拟器上一直好的。
  崩溃栈：AppIntents → `AskJarvisIntent.perform()` → `ask()` → `question.getter` → AppIntents 的 `_assertionFailure`。
- 原因：Release 配置为了防篡改设了 `SWIFT_REFLECTION_METADATA_LEVEL: without-names`。App Intents 靠反射里的属性名（`_question`）
  把快捷指令 / Siri 给的值放进 `@Parameter`；名字没了，值进不来，读非可选的参数就断言失败。所有带参数的 intent 都受影响
  （问 Claude、让 Agent 去做、灵动岛的允许 / 拒绝 / 批准）。在 intent 里打印 `Mirror(reflecting: self).children` 能看到 label 是 nil。
- 怎么复现：模拟器上要用 **Release** 配置编（Debug 永远有名字），装上后用 `xcrun simctl openurl … "shortcuts://run-shortcut?name=…"` 跑一个写死参数的快捷指令；
  崩溃记录在 `~/Library/Logs/DiagnosticReports/Persona-*.ips`，dSYM 在 Release 产物旁边，`atos -i` 能解到行。
- 做法：Release 保留反射名字（`all`）。代价是存储属性名进了二进制，和 DEBUG 启动参数同名就会被 release.sh 拦下：
  这次改了 `-fromSeq` → `-showFromSeq`、`-openFile` → `-openFilePath`、`-openTerminal` → `-openTerminalPane`、`-openWork` → `-openAllWork`，侧边栏的 `openMachines` → `onMachines`。
- 走过的弯路：先以为是参数没给值（改成可选 + `needsValueError`，结果在快捷指令里变成「内部错误」），又以为是 iOS 27 的结果卡片
  （`ShowsSnippetView`）；这两条在 Release 下都复现过、都不是原因，已撤回。崩溃查不清时先在模拟器上用 Release 配置复现，再用 dSYM 解栈。

## 藏起来的 WKWebView 截图：透明、错位、字不出来（2026-11-04，#1295）

公式和图（`MarkdownRenderer`，`docs/ios.md`「公式和图」）靠一个看不见的 WKWebView 画完再 `takeSnapshot`，前后踩了三个坑，症状都是「图是空的」：
- **视图的 alpha 会进截图**：为了藏起来设 `alpha = 0.01`，截出来的图几乎全透明，当模板图用就什么都看不见。改成 alpha 1、放在窗口最底层、frame 挪到屏幕外。不在任何窗口里的 web view 截不出内容。
- **在窗口里会被加安全区的 content inset**：页面整体往下挪了约 80 pt，按 `getBoundingClientRect` 算的截图框里只剩公式顶上一截。`scrollView.contentInsetAdjustmentBehavior = .never`。
- **`file://` 页面加载不了包里的字体**：字体请求走 CORS，file 页面的来源是不透明的，KaTeX 的 woff2 一直没加载，字体的 block 期间字是隐形的（Mermaid 用系统字体所以正常）。把字体以 data: URL 内联进 CSS（`font-src data:`）。另外 KaTeX 用相对定位把分子、上下标挪到自己的框外面，截图框要取所有子元素框的并集，内容也别贴着页面左上角画。
- **Mac 上屏幕外的窗口里没有 `requestAnimationFrame`**：页面等两帧再量尺寸，在 Mac 上永远等不到，每张图都转圈。等帧要和一个短的 `setTimeout` 赛跑；截图也不等屏幕刷新（`afterScreenUpdates = false`）。
- 截图缓存要带版本号（`MarkdownRenderer.version`）：调这些的时候，坏图已经存进了模拟器的缓存目录，不改键就一直读到旧的空图。

## 看画面 invalid token：`URLComponents.fragment` 会把 `%` 再编一次（2026-11-04，#1498）

- **现象**：App 打开实时画面页，页面说「invalid token」，hub 记一条 401。网页上登录后打开同一页是好的。
- **原因**：token 先用 `addingPercentEncoding(.alphanumerics)` 编码，再赋给 `URLComponents.fragment`。
  - 不带 `percentEncoded` 的 setter 会再编一次：`jd_` 先成了 `jd%5F`，再成了 `jd%255F`。
  - 页面只解码一次，拿到的是 `jd%5F…`。
- **为什么没测出来**：mock 的 token 是 `mock`，没有要编码的字符。
- **以后**：
  - 自己编过的值放进 `percentEncodedFragment` / `percentEncodedQuery`，没编过的才用 `fragment` / `queryItems`，两样不要叠。
  - 只放过 RFC 3986 的非保留字符（`A–Z a–z 0–9 - . _ ~`），`+` 要编（页面的 URLSearchParams 把裸的 `+` 读成空格）。
  - 测试用和真的一样形状的 token（`jd_…`），并按页面的读法读回来比对。

## 后台执行的 Intent 参数一律声明为可选（2026-11-04，#1144）

- 后台执行的 intent（实时活动按钮、快捷指令、控制中心控件）参数若声明为非可选（如 `@Parameter var interactionID: String`），系统在后台拉起 App 进程执行 `perform()` 时如果由于跨构建升级、扩展与 App 元数据不一致或系统未还原参数，Swift AppIntents 会在读取 `wrappedValue` 时直接触发断言失败（`EXC_BREAKPOINT (SIGTRAP) SIGNAL 5`），导致 App 退出崩溃。
- 后台 intent 参数一律声明为可选（`@Parameter var interactionID: String?`），提供无参 `init()`，并在 `perform()` 中先判空安全返回，切勿在锁屏或后台按钮上追问参数或抛出未捕获错误。

## `@Observable` 的 `init`：存储属性初始化完前别读另一个可观察属性（2026-11-04，#1509）

- 在 `@Observable` 类的 `init` 里，如果一个存储属性的计算读取了另一个刚刚赋值的存储属性（或调用了读取它们的方法），Swift 编译器可能会报错或在运行时触发未初始化的可观察状态访问。
- 做法：在 `init` 里先用局部变量算好各个值，再分别赋给各个存储属性；所有存储属性初始化完成之前，不要读取自身的可观察属性或调用依赖它们的方法。

## 任务卡片展示子 Agent 进度：补充行只认 running 状态（2026-11-04，#1609）

- 任务卡片展示主 Agent 进度的同时，在下方以次级字号补充在跑的子任务。
- 子 Agent 的标题优先取 `description`，没有取 `type`，兜底取「子任务」；补充行只列 `running == true` 的项，最多 3 个顿号连接，超出部分显示「等 N 个」。
- 主行 `TaskNow` 的计算规则完全独立，子 Agent 仅作为补充展示，绝不覆盖主 Agent 本身的话语或执行步骤；`Session.sameApartFromTicks` 包含 `subagents` 比较，保证状态实时刷新。

## 提问卡片回答结局与状态：200 already 与 410 status 友好映射（2026-11-04，#1688）

- hub 的 `POST /api/v1/interactions/{id}/resolve` 针对已结束的提问，若已回答则幂等返回 200 `already: true` 并带上当时的 `resolution`；若过期、撤回或交回电脑则返回 410 `code: gone` 并带上 `status`。
- 客户端解析 200 的 `already` 回应，展示当时的答案，切勿误判为未完成；410 按 `status`（`expired`、`cancelled`、`dismissed`）展示中文说明，不弹原始英文错误。
- 本地记录已结束的 interaction ID，`replaceInteractions` 合并快照时不放回已结束项，杜绝快照竞态复活。



## 隔离预览 HTML：规则表和 loadHTMLString 的地址（2026-11-04，#1297）

- **内容规则的 `url-filter` 不认 `|`**：写成 `^(about|data):` 整张表编译失败，没有报错（`try?`），预览按设计退回「无法隔离网络」页。一个协议写一条 `ignore-previous-rules`。
- **`loadHTMLString(_, baseURL: nil)` 的页面地址是 `applewebdata://…`，不是 about:**：只放行 about: 的规则和导航判断把页面自己挡了。`baseURL` 给 `about:blank`，两个协议都放行。

## 填 token / 密钥的框弹「保存密码？」：`.oneTimeCode` 挡不住（2026-11-04，#1512）

- 现象：插件市场的 token、插件页面的 secret 字段、插件的密钥，输完离开页面（返回、关 sheet）系统弹「保存密码？」。iOS 27 模拟器上能稳定复现，
  条件是**旁边有一个填了字的普通输入框**（市场的地址、插件页面的备注）：AutoFill 把它当成用户名、把隐藏框当成密码。只有一个隐藏框的 sheet（插件密钥）在模拟器上不弹。
- `.textContentType(.oneTimeCode)` 不够：SwiftUI 的 `SecureField` 加上它照样弹，连 UIKit 的 `UITextField` 直接设 `textContentType = .oneTimeCode` 也弹（同一个 UI 测试试过）。
  配对页那次（上面「SSH 隧道」）管用，是因为那里是推下一页、隐藏框还在；这里是框被移出界面。
- 管用的是**框离开界面时是空的**：设计系统的 `SecretField`（`DesignSystem/SecretEntry.swift`）在 iOS 上是一个 `UITextField`，交出键盘（`resignFirstResponder`）或离开窗口前先把自己清空，
  值留在绑定里，框里写「已填写…」。用 `.onChange(of: focus)` 在 SwiftUI 里清不行：键盘还在时直接点返回，清空赶不上，照样弹。
- 以后凡是 token、API Key、别人的密钥（不是这个 App 的登录密码）都用 `SecretField`，不直接写 `SecureField`。
  UI 测试 `ExtensionMarketTests.testASecretFieldIsNotAPassword`、`testManageMarkets` 用 `PasswordPrompt.assertNone` 查系统的「保存密码？」sheet（标题「保存密码？」、按钮「以后」）。
- #1598 把其余的也换了：配对的 Token（iOS）、模型供应商的 API Key（自己填写 / 从插件添加）、飞书机器人的 App Secret（iOS）、连接器登录步骤里的隐藏字段（iOS；Telegram 的 api_hash、两步验证密码，渠道的令牌）、
  权限表单的隐藏字段、终端要的密码（sudo）、邮箱的密码 / 授权码。邮箱密码虽然是真的登录密码，但它属于邮件服务器、只在添加时填一次、hub 加密保存，
  存进「密码」只会挂在 Persona 名下成一条孤儿记录，国内邮箱填的还多是给单个客户端生成的授权码，所以也不让系统保存。
  故意没换的：飞书加密消息的解锁框（飞书网关登录密码 / 密钥库密码）每次解锁都要输、hub 不保存，`.textContentType(.password)` 是有意让用户从「密码」里自动填充；
  sheet 里只有这一个框、点「解锁」时先清空，不像登录表单（`LarkNativeEncryptionFlowTests` 解锁后也查 `PasswordPrompt.assertNone`）。Mac 上各处照旧是 `SecureField`。
  UI 测试 `SecretFieldTests`（供应商 Key × 2、机器人、邮箱、终端密码），以及 `PermissionInputUITests`、`ChannelSettingsTests.testBindWithAToken`、`TelegramConnectorTests`、`EngineChoiceTests.testManualPairingShowsEngineStep` 末尾的 `PasswordPrompt.assertNone`。
- `SecretField` 是 UIKit 的框，SwiftUI 的 `.font`、`.multilineTextAlignment` 不会自己传过去：对齐在 `updateUIView` 里按环境的 `multilineTextAlignment` 设（表单行里值靠右），字体固定 `Theme.Font.uiBody`；
  `accessibilityIdentifier` 用它的 `identifier` 参数给，UI 测试按 `app.secureTextFields[<id>]` 找。
- 真机：同样的启发式，行为应该一致；真机上还取决于 iCloud 钥匙串 / 密码 App 的「自动填充密码」开关，关着的设备本来就不弹，没在真机上复现过。
- iOS 截图里正在输入的隐藏框整块是空白（连占位文字都没有），改动前后一样，不是 bug。

## 「全部标为已读」过一会儿全部变回未读：WS 消息没有回执（2026-11-04，#1588）

- 现象：点「全部标为已读」，列表先清空，过一会儿又全部回到未读；hub 的 `session_views` 里一条也没有，而 HTTP 和 WS 单独标一个都正常。
- 原因：`sendSeen` 只看 `state == .connected` 就往 WebSocket 里写，不看 `stalled`；`URLSessionWebSocketTask.send` 的失败和 HTTP 的 `try?`
  都被丢掉。hub 刚重启、手机刚从后台回来时连接是半开的，写进去的消息全丢了；重新连上后 hub 的列表里它们还是未读，本地的乐观改动就被盖回去。
- 做法：发出去不等于送到。会改 hub 状态、又没有回执的 WS 消息，要么连接卡住时改走 HTTP，要么记下来，等 hub 推来的状态证明它收到了
  （这里用 hub 自己的 `seen_at`，不拿手机的时钟去比 hub 的时间），没证明就补发；等的这段时间按本地的改动显示，别让推送盖回去。
  WS 发普通消息的路径早就这样做了（`sendPending` 前面同时看 `connected` 和 `stalled`），新加这类消息时照着做。

## 从 sheet 里的页面点 VNC 没反应：根视图上的 fullScreenCover 被吞了（2026-11-04，#1604）

- **现象**：机器页本身在 sheet 里（或新外壳推出来的页面里）时，点「看画面」没反应；回到首页从别处点又能打开。
- **原因**：画面挂在 `RootView` 的 `fullScreenCover(item:)` 上。根视图已经在 present 一个 sheet 时，SwiftUI 不再 present 第二个，什么也不报。Mac 的 `MacRootView` 干脆没挂。UI 测试是从设置 tab 里 push 进机器页的，没有 sheet，所以没测出来。
- **以后**：
  - 从任何地方都要能弹出的全屏层（画面这种），放进一个自己的窗口（iOS 的 `UIWindow`，level 在 shell 之上、toast 之下；Mac 的独立窗口），由模型的一个值驱动显示，不挂在某个视图的 present 链上。
  - 测试要从 sheet 或新外壳的页面里点一次。


## 改窗口的 safe area 让位，SwiftUI 的导航栈会跟着重排（2026-11-04，#413）

- 外壳靠 `rootViewController.additionalSafeAreaInsets.top` 给状态行让位，收起时 Agents 的导航栈重新排版，列表有 100–200 ms 读到 inset 和 offset 都是 0，一帧跳约 210pt，
  导航栏偶尔把这当成一次滚动，把拉出来的搜索框收了。从 UIKit 改 SwiftUI 托管的导航控制器、负的 safeAreaPadding、开合时关 `hidesSearchBarWhenScrolling`，都会被 SwiftUI 改回去或带出别的跳动。
- 结论：常驻、会来去的提示别让位，浮在一个不接触摸的窗口里（`ToastWindow`）；要让位的只能是不会频繁变的东西。判断用录屏逐帧看加探针逐帧记，不要只看最后停在哪。

## 模拟器没有画中画；发 RFB 消息别每条开一个 Task（2026-11-04，#1343）

- **画中画**：`AVPictureInPictureController.isPictureInPictureSupported()` 在 iOS 27.0、27.1 的模拟器上（iPhone、iPhone Duo）都是 false，`AVSampleBufferDisplayLayer` 的小窗在模拟器上起不来。App 这时退回整页（`AppModel.openVNC`），UI 测试照这个分支走（`VNCTests` 看到整页就跳过小窗那段）。小窗的录屏和验收只能用真机。
  不支持的设备上 `AVPictureInPictureController(contentSource:)` 实际返回 nil，Swift 却把它当成非空的对象，第一次用（这里是 KVO 的 `observe`）就崩在 `swift_dynamicCast`。先看 `isPictureInPictureSupported()` 再建；DEBUG 的 `-showPiPSource` 在模拟器上只摆源、不建控制器（#1839）。
- **发送顺序**：原生 RFB 客户端起先每条指针、按键、剪贴板消息各开一个 `Task { try await transport.send(…) }`。这些 Task 谁先跑没有保证，按下和松开、移动和点击会乱序到 Mac，快速打字时最明显。改成一个发件队列（`AsyncStream`），握手完后由一个任务按顺序发；握手前来的也排在握手之后，不会插进握手中间。别的「多条消息、顺序有意义」的通道（终端按键、WS）同理。
- **测试读剪贴板**：UI 测试的 runner 写了 `UIPasteboard.general`，App 去读时没弹「允许粘贴」，送到 mock 的剪贴板却是空的（runner 那时在后台，写的没有生效或读不到）。剪贴板的 UI 测试改成在 App 里来回：先「复制到 iPhone」，再「只放进 Mac 的剪贴板」送回 mock，比对 mock 收到的。

## 在几何回调里直接改 UIScrollView 的偏移，别的行的几何回调可能收不到（2026-11-04，#1046）

`ChatPin` 在标题的 `onGeometryChange` 里直接设 `contentOffset`，把标题按在原处。屏幕上是对的，标题自己的回调也报了按住后的位置；
但同一行外层记位置的 `onGeometryChange`（`debugLineY`）停在挪之前的值，直到下一条内容进来：
在最新处点开的工作行一直按着，流式的回复写进对话时，`probe.chat-rows` 报「顶部那行移了 56 pt」，下一条进来又「移回来」。
`RunningChatTests` 因此失败，可测试期间从外面每 0.5 s 截一张屏，60 s 里的 120 张内容区逐像素相同（两种布局都是）。

- 探针里工作行的标题用它自己报的位置（`scratch.workHeadY`，pin 按住它用的就是这个）。
- 拿 UIKit 的偏移去补 SwiftUI 的位置不行：落后的方向不定，一种布局补对了另一种就补反了；改完偏移再 `setNeedsLayout()` 也不会让那个回调再报一次。
- 报「跳了」又对不上的时候，先拿截图比像素（`simctl io screenshot`，比较相邻两张的行亮度曲线），再去查代码。

## 流式时「不稳」要录屏逐帧量，而且要按真 hub 的事件顺序（2026-11-04，#1046）

用户说流式时滚动不稳。截图看不出来，探针也会报假的跳动；录屏逐帧解码（`scripts/diag/stream-scroll/`）才看到几件真事，每件都只有一两帧或者几百毫秒：

- **只在一帧里发生的**：回复写成正式消息的那一帧整段不见。原因是两个状态不在同一轮更新：流式文本当场清掉，行要后台重排、下一轮才到。
  凡是「一个东西换成另一个东西」，旧的要留到新的到了的那一轮再拿掉。
- **mock 的事件顺序比真 hub 规矩**：mock 先发正式消息再清空，真 hub 是一轮结束就清空，transcript 那行可能更晚。改 mock 加上 `&late=` 才复现出来，
  修之前先读服务端是按什么顺序发的。
- **倒着的列表里，最新那一端一变，手指下面的东西就动**：扣住新内容时把流式文本拿掉（少了几百 pt）、手指按着时流式照常长（推走一两行），都在手里跳。
  手指按着就扣住，扣住时照原样留着已经画出来的东西。
- **懒列表里换分支不一定换掉那一行**：扣住时用 `if let f = frozen { StreamingText(f.text).id("live") } else { ChatLiveText(store) }`，
  两个分支里那一行的 id 都是 `live`，`LazyVStack` 留着原来的 `ChatLiveText`、它照样跟着 store 长，扣住的文本根本没画出来（`ScrollTrace` 里扣住的文本一直 309 字、
  内容高却一直涨，加 `NSLog` 看 body 才确认）。同一行要换内容，就让同一个视图拿不同的值（`ChatLiveText(held:)`），别用两个分支各画一个。
- **Mac 的滚动阶段只管手指**：两指离开触控板 SwiftUI 就报 idle，惯性还在走；要按滚动事件的 phase / momentumPhase 自己判断（`MacWheelWatch`）。

## 分栏的路径，换树时带不回页面（2026-11-04）

- **分栏左栏的 `NavigationLink(value:)` 不写进绑定的 `path`**（iOS 26，2026-11-04）：`NavigationSplitView` 把值推进右栏时，绑定的 `path`
  先被清空（`NavigationColumnState.ejectAll`），打开的页面不在 `path` 里。整棵树被换掉时（`RootView` 按宽度换新外壳和 tab），
  别指望 `path` 能把页面带回来，要按另外记下的（`dev.openChat`）重开；要走的那棵树的分栏也会清空它的 `path`，所以把 `path` 放进 model 共享也没用。

## 画中画的源别放在会关的窗口里，也别太小；开它要在点按里（2026-11-05，#1839）

- **现象**：VNC 小窗在 TestFlight 161 的真机上起不来，模拟器上看不出（模拟器本来就没有画中画）。
- **原因**：
  - 源（`AVSampleBufferDisplayLayer`）只有 2×2pt；从整页的「…」› 小窗开时，它被加到了当时的 key window，也就是整页自己的 `ScreenWindow` 上，紧接着整页关掉、窗口释放，源跟着没了。
  - 从机器页点 VNC 时，连上、有画面以后才在一个 `Task` 里调 `startPictureInPicture`，已经不在用户点按的那一刻；失败只写系统日志，用户看到的就是「什么都没发生」或「直接进了整页」。
- **以后**：
  - 源用正常尺寸（这里宽 192pt），放在一直在的那个窗口（App 的主窗口）里、内容后面；不要 `isHidden`、不要零尺寸。
  - 能提前准备就提前准备（这里整页一连上就准备），让「开小窗」在点按的同一个事件里调 `startPictureInPicture`；需要后台自动开的设 `canStartPictureInPictureAutomaticallyFromInline`。
  - 带 `controlTimebase` 的层不要再收 DisplayImmediately 的样本（头文件里明说不建议），给它单独做带时间的样本。
  - 开不了一定要让用户看得到原因（提示 + 一个能复制的诊断页），真机问题才能一次说清卡在哪。
  - 源里还没有画面时别开：`isPictureInPicturePossible` 在层一进窗口就是 true，可空的源上调 `startPictureInPicture` 系统直接忽略，不开也不调 `failedToStart`（TestFlight 161 的小窗诊断：机器页点 VNC 每次都这样，整页里有画面时点「小窗」就开了）。等第一帧到了再开，没反应就隔一会儿再试几次。
  - 不想等连接的话，自己往源里先放一帧（#1839 跟进：用户嫌「连上了才有小窗」迟钝）：`ImageRenderer` 把 SwiftUI 画的卡片（机器名、转圈的「连接中…」）
    画进 `CVPixelBuffer`，按主机时钟做成样本塞给源层，再在点按里开；之后 Mac 的帧（时间更晚）自然盖过它。卡片停掉时把最新一帧重新塞一次，
    免得最后一张卡片排在真画面后面：VNC 只在屏幕变化时才发帧，静止的 Mac 会一直停在卡片上。

## 一个页面换成另一个页面，内容要在同一个位置交接（2026-11-05，#1911）

新对话发出第一条：「新对话」页把消息画在顶部，对话开起来后换成对话页，倒置列表把它放在底部，于是先在上面、再跳下去。录屏逐帧看还有两件：
发出后键盘先收后起（模拟器）、对话页一开始还没有「正在工作」那一行，消息低一行再被顶上去；本地那份待发消息在转录那份到之前一轮就被拿掉，空了一帧。

- 两个页面接力显示同一个东西时，前一个页面要按后一个页面的排法画：同样的组件（`UserBubbleBody`、`DayLine`）、同样的边距（`ChatView.newestGap`），
  贴在同样的位置（`FirstMessagePending`）；后一个页面在真内容到之前也先占着那一行（「正在准备…」，`ChatView.startingTurn`）。
- 「旧的拿掉、新的放上」要在同一轮：行在后台重排、下一轮才发布，本地那份就留到那一轮（`ChatStore.arriving`，和 #1046 的流式文本是同一个坑）。
- `onScrollGeometryChange` 在值变之前不回调，第一次布局拿不到高度；要内容贴底用 `containerRelativeFrame(.vertical, alignment: .bottom)`。
- 量「消息有没有跳」量它到输入框的距离：键盘起落时两个一起动，那不是跳（模拟器用硬件键盘打字，发出后才弹软键盘）。


## 键盘弹起后提问卡片被裁掉，选项点不到；`scrollDisabled` 会传给里面的 ScrollView（2026-11-05，#1912）

- **现象**：
  - Agents 对话里有「Claude 在问你」时点输入框，卡片只剩一条，露出问题和前两个选项的一半。
  - 小屏 iPhone（SE 尺寸）上更糟：输入框被键盘盖住 30 pt，后面的选项怎么滑都到不了。
- **原因**：
  - 待处理卡片的上限是页面的一半（#926）。键盘弹起后页面只剩三四百 pt，一半就只够两个选项。
  - 对话列表有自己的最小高度（压在页头下面的部分加上边距）。卡片再高，页脚就超出页面，输入框跟着被推到键盘下面。
  - 卡片的表单自己也会滚（上限 `room * 0.55`）。卡片比卡片区还高时，滑动被里面的表单接走，外面的卡片区不动，最后几个选项露不出来。
  - 卡片放得下时，外面的卡片区用了 `.scrollDisabled(true)`。这个修饰符经环境传给里面所有 ScrollView，所以卡片自己的表单也滚不了。
- **怎么修的**：
  - 键盘弹起时（`KeyboardState`），卡片区可以占到对话列表让出的高度。页头按它此刻的高度算（Agents 页头会折成一条），对话列表的高度允许压到 0。
  - 单张卡片的表单收到「整张卡片放得进卡片区」，卡片区本身不再滚，只有卡片的表单滚。
  - 键盘弹起时卡片区始终按「滚动」排版（数字、边距、裁剪不变），免得卡片缩进去以后排版来回跳。
  - 卡片区放得下时用 `.scrollBounceBehavior(.basedOnSize)` 让它不动，不再用 `scrollDisabled`。
- **以后**：
  - 想让外层 ScrollView 不滚、里层照常滚，用 `scrollBounceBehavior(.basedOnSize)`，别用 `scrollDisabled`。
  - 嵌套的两个 ScrollView，内层比外层高时滑动全给内层：要么让内层放得进外层，要么让内层不滚。
  - 键盘相关的界面要在 SE 尺寸上看。iOS 27.1 的模拟器运行时没有 SE，用 27.0 的建。
  - UI 测试别用 `swipeUp` 找小区域里的选项：快的一下就甩过头，从卡片标题行开始的又不滚。用按住、慢拖、末尾停一下的拖动（`press(forDuration:thenDragTo:withVelocity:thenHoldForDuration:)`），起点放在要滚的那块里面。

## 从列表行弹出的 `confirmationDialog` 没有「取消」（2026-11-05，#1830）

- **现象**：扩展设置里打开一台电脑的开关前要先问一句，用的是 `confirmationDialog`，iPhone 上弹出来只有「打开」一个按钮，`role: .cancel` 的「取消」不见了；标题也只能写字段名，看不出是哪台。
- **原因**：iOS 27.1（模拟器上看到的）的 `confirmationDialog` 以弹出框（popover）的形式挂在触发它的行上，弹出框里系统不画 cancel 按钮（点外面就是取消）。用户不知道点外面能取消。
- **做法**：「要不要做这件事」这种二选一用 `.alert`，「取消」「打开」都在；标题写上对象的名字（「打开「Mac Studio」？」）。名字单独存一份 `@State`，关弹窗时别清，否则消失动画里标题变成「打开「」？」。
- **以后**：只有真的是「从几项里选一项」才用 `confirmationDialog`；需要明确取消的确认一律 `alert`，UI 测试里断言「取消」按钮存在并点一次。

## 时间敏感推送一直按普通通知显示：少了 entitlement（2026-11-04，#1572）

- 现象：hub 的「需要你」推送从一开始就带 `interruption-level: time-sensitive`，通知服务扩展也照样设了 `interruptionLevel`，但开着专注模式时横幅不出来（专注模式会同步到手表，所以手表上也收不到）。
- 原因：App 的 entitlements 里没有 `com.apple.developer.usernotifications.time-sensitive`。缺了它，系统静默把时间敏感当 `active`，日志和 API 都不报错；`requestAuthorization` 里的 `.timeSensitive` 选项早已废弃，不起作用。
- 做法：entitlements 加这一项，App ID 开 `USERNOTIFICATIONS_TIMESENSITIVE` 并重建描述文件（`scripts/release/asc_signing.py`），发版前 `scripts/release/entitlements.py check` 会查。模拟器上看不出差别，要真机开着专注模式验证。

## 量掉帧时测试自己就在制造掉帧；补偿动画跨过一帧卡顿会一下走完（2026-11-05，#1123）

- 第一轮量展开 / 折叠，每次点击都晚到 15–20 帧，连什么都没重画的点击也一样：是 UI 测试每 0.1 s 查一次 `probe.unfold`，每查一次无障碍树都占 App 主线程。
  测试点完安静地等，再读一次结果；`probe.chat-rows` 这种每秒 10 次的探针也要关掉。先量一段什么都不点的（0/96 帧晚到）作对照，才知道数字能不能信。
- 按时间算的动画（`exp(-dt/τ)`）碰上一帧卡顿（步骤刚排出来那一帧 80–100 ms），dt 很大，一帧就走了大半，看起来还是「窜」：dt 封顶一帧（1/60 s）。
- 一个状态被「上一个动作」借走：在最新处打开的工作行可以往上升到顶栏下面，再点里面的思考，思考展开的高度就把标题剩下的上升量用掉了，被点的行跟着上去 46 pt；
  BeautifulUI 的工具胶囊则把工作行的「一直按住」换成了普通的 1.5 s，1.5 s 后跟随又起来，把列表拉到最新处、胶囊上去 119–381 pt。
  里面的东西被点了，外面的那个就从此原地不动、状态也不变（`ChatPin.hold` 重新以当前位置按住，`changeWork` 对 run 不动跟随状态）。

## 纯逻辑挪进 SPM 包，测试才能离开 App（2026-11-05，#2095）

- **现象**：跑一个纯逻辑的单测（笔记本解析、路径识别）也要先编完整个 App、再起模拟器：新 worktree 第一次约 140 秒，改一个文件后约 45 秒，几个 Agent 同时编时还要排队。
- **做法**：只依赖 Foundation 的类型挪进 `apple/Packages/PersonaKit`，测试跟着挪，`swift test` 冷的约 10 秒、改一个文件后约 2 秒。挪的时候：
  - App 用到的类型、成员都要 `public`，`private(set)` 写成 `public private(set)`，App 里构造的结构体补 `public init`。不然 App 编不过，而包自己的 `swift test` 照样全过，因为测试是 `@testable import`。所以每挪一批都要编一遍三个 App。
  - 一个测试文件里混着 App 层的断言（视图、`FilePaths`）时，拆开：纯逻辑的进包，App 层的留在 `PersonaTests`，加 `import PersonaKit`。
  - 包的默认隔离是 nonisolated，App 是 MainActor：原来就标了 `nonisolated` 的值类型直接挪，不用改。
  - 挪 `apple/Shared` 里的文件（第 2 批的 `Links`）之前，先查手表、小组件、通知扩展有没有用它（`grep -rn '\bLinks\b' Shared PersonaWatch PersonaWidgets LiveActivity`）：它们不依赖这个包，用了就编不过。
  - 给 App 文件补 `import PersonaKit` 时按名字 grep，会把只在注释里提到它的文件也算进去；只给代码里真用到的加（第 2 批多加了 8 个，又删掉了）。
  - 从视图文件里往外抽纯函数（第 2 批的 `StreamChunks`、`ZoomMath` 等）时，连同它上面的 `///` 文档注释和只给它用的 private 扩展一起抽（`ConversationTitle` 的 `Character.isASCIIDigit`）。那个扩展进了包以后保持 internal，不改成 `public`：App 原来就没有它。
  - 一个类型里只有一两个纯函数可测（`AgentSync` 的 `heldUntil` / `workingGap`）时，抽一个纯的 `enum`（`AgentSyncPacing`）进包，原来的成员改成转调它，App 里别处一行不动。视图上的 `static` 帮手（`ConnectorSignInSteps.cannotDrawNote`）挪到它算的那个值类型上（`ConnectorPrompt.cannotDrawNote`）。
  - 测试里用 App 的 `JSON.decoder` 时，先看它做了什么：它只加了日期策略，没有日期的类型在包里用 `JSONDecoder()`，解出来一样。
  - `swift test` 把编译产物写在包目录下的 `.build/`，仓库根的 `.gitignore` 要盖住它（`/apple/Packages/*/.build/`）。第一次提交时 `git add -A` 把 1,500 多个产物带了进去，里面有本机的路径；推到远端后发现，删了分支。提交前看一眼 `git diff --stat` 的文件数。

## 回调里捕获的 key，对象改名后就作废了（2026-11-05，#2379）

- **现象**：配对时经 SSH 隧道连上的 hub，之后隧道断了，连接详情里看不到「上次失败」原因。
- **原因**：隧道的 `onPhase` 回调捕获的是拨号时的 id（`pairing`），配对成功后 `adopt` 把隧道挪到 hub 的 id 下，回调里 `tunnels[id]` 再也对不上，状态全被丢掉。
- **做法**：回调按对象本身去表里找它现在的 key（`SSHTunnels.key(of:in:)`），不捕获 key。另外，移除一组 hub 时要在改 registry **之前**取它的成员：改完之后成员已经不在 registry 里，id 就拿不到了（`AppModel.tunnelIDs(removing:from:)`）。

## HTML 子帧能借父页面的 bridge，不能只检查主帧标记（2026-11-05，#2025）

- 真实 WKWebView 验证中，直接从子帧发消息被 `isMainFrame` 拒绝，但同源 `srcdoc` 子帧借父页面 bridge 发出的消息会被识别为主帧消息；当前实例 nonce 也不能区别这两种调用。该用例中，CSP 的 `frame-src 'none'` 没有阻止子帧执行。
- 助手卡片不需要子帧，导航代理不再放行 `about:`／`data:` 子帧，统一拒绝。回归必须保留借父 bridge 的真实请求用例，不能只测伪造 handler 参数。修复后九项常规 WebKit 安全用例通过；无限循环及宿主恢复单独验证，不能由常规测试推导。
- 内容进程退出时撤销 bridge 实例、加载票据和脚本，转为原生 fallback；同一 HTML 的重试必须创建新实例，不能被 `loaded == html` 去重挡住。调用公开 delegate 回调能验证这条恢复逻辑，但不等于实际触发了 OS 终止。

## 返回手势还没走完就改导航路径：栈里留下一页空白（2026-11-05，#1941）

TestFlight 上新外壳的 Agents 页偶发只剩系统导航栏、下面一片空白（深色下是全黑）。复现靠 `AgentsPushPopRaceTests`：
用 runtime CLI 的 `navigate … keep` 在返回手势途中从外面开一个对话（和点通知一样走 `model.openChat`），约 300 次里中 2 次
（BeautifulUI，从左边缘一直划到底、开的时机在手指抬起前后）。那时 SwiftUI 的 `path` 已经是空的（外壳的顶栏只在根上才出来），
UIKit 的栈却还推着一页，上面只有系统的返回按钮「Agents」，内容是空的。

- 在 `UINavigationController` 还有 `transitionCoordinator`（推进 / 返回的动画、手势还在走）时改 `NavigationPath`，
  SwiftUI 收下了，栈却没跟上，两边从此对不上。
- 从列表外面开页面（通知、Siri、启动）一律先等栈停下：`StackStillWatcher.whenStill`（`ChatsView.takeOpenChat`）。
  栈本来就停着就马上推，启动和列表上点通知照旧不等。
- 这种时机靠手点复现不了：写 UI 测试，从测试进程直接连 App 的 runtime CLI 端口在指定的毫秒发命令，按几十毫秒一档扫时机，
  每档多跑几轮；判断「空白」要看导航栏上有没有系统的 `BackButton`，不能只看列表的元素在不在（有一次它们还在无障碍树里）。

## 列表的结构一变，每一行的修饰链都重新构造（2026-11-05，#343）

- **现象**：Agents 列表停着时，hub 每次推送都有一帧主线程 16–30 ms（模拟器，140 个 Agent）。行本身已经是 `Equatable`、跟着自己的 `AgentsFeed.Row`，
  可是空间的顺序一变（真实 hub 上约一半的推送），列表 body 重算，`ForEach` 给每一行重新构造 `NavigationLink`、`swipeActions`、`contextMenu`
  和里面捕获的整份 `Session`，占每次推送的五分之一。
- **做法**：行包进一个只比较 key 的 `Equatable` 视图（`KeyedRow` + `SlotRowKey`），key 里放行的参数和列表自己的状态（搜索、细列表、风格、分组、机器名），
  跟着推送变的字段（最后一句、时间）去掉：它们由行自己观察的对象读。key 漏了列表 body 里的非观察值（`@State`、`Binding`、`Lookup`），行就会停在旧的样子。
- 这样的行别再套 `.equatable()`：包进 `EquatableView` 后，行里写的列表 trait（`listRowSeparator`、`listRowBackground`、细列表的 `listRowInsets`）
  没有传到 `List`，胶囊风格多出分隔线、细列表的行矮了一截（截图对比才发现，测试都过）。视图自己实现了 `==`，SwiftUI 就按它比较，不需要 `.equatable()`。
- body 里的计算属性别在一次 body 里算两遍：筛选条的 `counts(base)` 又调了一次 `base`，`TopBar` 的 `@ViewBuilder` 参数即使不显示也会被构造。
- 泛型 `@Observable` 类（`StoredValueBox<Value>`）的属性每读一次都要在运行时实例化 key path（`swift_getKeyPath`），放在每个元素的闭包里读很贵，先读到局部变量。
- 先用 Time Profiler 看清时间花在哪（`docs/testing.md`「Agents 列表的结构推送」）：只看 body 次数会以为是 body 贵，这次 body 只占五分之一。

