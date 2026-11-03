# 新外壳的状态模型（哪一页在前）

新外壳（`docs/ios.md`「新外壳」）的「助手 | Agents」切换、两页横条、抽屉、各页的导航栈，背后谁说了算、怎么同步、怎么测。
2026-11-02 重做，起因是用户报的问题和一连串补丁。

## 起因

用户原话（2026-11-02）：「在查看完 Agent 的内容之后，退出回来想要切回助手，点击顶部的助手与 Agent 的切换 Tab，发现点到 Agent
无法真正切换到 Agent 的界面……感觉现在这个 UI 状态管理上面存在底层的设计不是很合理的问题，导致好像一直在缝缝补补」。

之前补过的：#174 搜索框和顶栏重叠、#182 知识库搜索右滑返回后导航栏残留、#190 左右滑切页、#178 角标导致重建、#203 头像、
56b2ead 筛选条搬进列表、`ShellBarAtRest` / `NoRowSwipe`。每次修的是一个症状，「当前是哪页」本身没有一个说了算的地方。

## 复现（改之前）

`ShellRaceTests` 用坐标点击模拟手指（`ShellFinger`，不点无障碍元素）：Agents → 打开一个 Agent 的对话 → 返回（返回按钮 / 右滑，
返回动画没结束就点、0.15 s、0.4 s、1 s 后点；对话正在流式输出时退出）→ 点助手 → 点 Agents。每一步读 DEBUG 探针
（`probe.shell`：横条停在哪页、选中的段、App 的 tab、抽屉、两页是否在根部、状态行）和 `ShellTrace`（最近的点击、路由、滑动事件）。

- 12 轮里 4 轮复现：探针是「横条停在助手，tab=agents」，Agents 段加粗，横条不动。
- 追踪是这样的：`switch chats` → `tab assistant->chats`，之后再没有外壳的 body，也没有 `route`。也就是说，点击到了切换段，
  `AppModel.selectedTab` 也改了，但外壳靠的 `.onChange(of: model.selectedTab)` 没触发（SwiftUI 没有重新求外壳的 body），横条没收到要滑的指令。
- 分叉以后点不回来：切换段按 `selectedTab` 决定哪一段是「已选中」，已选中的那段是一个 Menu（再点一下打开这页的选项）。
  助手顶栏传给 Agents 段的菜单是空的，所以之后每次点 Agents 都是打开一个空菜单，看上去就是点了没反应。
- 拖动也会分叉：`ShellPagerTests` 在 10 次矩阵里 7 次随机挂，探针是「横条停在 Agents，tab=assistant」，追踪里拖动 settle 之后同样没有 `route`。
- 快速连点：`slide(to:)` 静止时下一轮才开始动；这期间反方向的点击被「位置已经在那」挡掉，等它开始动又滑去前一次点的那页，tab 和横条反了。
- 滑动中（约 0.4 s）点击会丢：两页自己的顶栏这时透明，顶替的 `ShellMovingBar` 整条不接触摸。

## 改之前：「当前是哪页」有五个来源

```
 通知 / Siri / 收件箱 / 抽屉 … (≈30 处)            切换段 (Binding)
                 │                                        │
                 ▼                                        ▼
        AppModel.selectedTab  ──── ShellView.onChange ────►  route ──► ShellPager.slide(to:)
          ▲        │        （会漏：没有重新求 body 就不触发）               │
          │        │                                                     ▼
          │        └─► 切换段的选中态、哪一段是 Menu           ShellPager.position / page
          │                                                  │  药丸、两页的位置 / 透明 / 接不接触摸、
   pager.settled ◄─── 拖动松手 ◄──────────────────────────────┘  顶栏、DrawerPan 的方向、tabIsActive(#211)
```

| 来源 | 谁写 | 谁读 | 和别的怎么同步 |
| --- | --- | --- | --- |
| `AppModel.selectedTab` | 约 30 处路由、切换段的 Binding、拖动 settle | 切换段（选中态、Menu 还是按钮）、外壳的 `onChange` | `onChange` → `route` → `pager.slide`；会漏 |
| `ShellPager.position`（`page` 由它算） | 拖动、`slide`、settle 的动画 | 药丸、两页位置 / 透明 / 接触摸 / 层级、顶栏、`DrawerPan`、`tabIsActive` | settle 时回调写 `selectedTab` |
| 各页的导航栈 | `AssistantView`、`ChatsView` 自己的 `path` | 只报给外壳「在不在根部」（`chatAtRoot` / `workAtRoot`） | `onChange(of: path.isEmpty)` |
| 抽屉 `AssistantDrawer` | 拖动、头像按钮、抽屉里的行 | `DrawerPan`、布局 | 和页无关，只看「当前页在不在根部」 |
| `tabIsActive`（#211） | `ShellPageSlide` 按 `pager.page` | 轮询、已读上报 | 跟着横条，不跟着选中 |

不一致或者有竞态的地方：前两行之间只有一条会漏的 `onChange`；切换段读 tab、药丸读 position，分叉时同一个控件上两种说法；
`slide` 的异步启动和「已在目标」判断；滑动中点击无处落地。

**不在前面的那页**一直留在窗口里（两页是一个 ZStack，Agents 第一次用到后就不拆）：透明、`allowsHitTesting(false)`、
`accessibilityHidden`。但它的 UIKit 部分（玻璃按钮、Menu 的 UIButton、导航栏）在推出页面右滑返回后又回到了无障碍树，
XCUITest 能点到它（`ShellSwitchTests` 曾因此点到隐藏的切换段）；真实手指的点击没有落到它上面过。所以现在不在前面那页的顶栏干脆不画（`ShellBarAtRest`）。
页面被推出的页盖住时（助手推着收件箱、机器，Agents 推着对话），它的顶栏在窗口外，接了滑动的动画也跑不完，横条的 `settle`（`.removed`）就一直等，
两页都停在「滑动中」不接点按（2026-11-03，从收件箱的需要你进 Agent 对话，`ShellInboxAgentTests`）。所以各页顶栏的出现消失不带动画，
切换器的药丸只有 `ShellMovingBar` 那条跟着 position 走（`shellPillFollowsStrip`），各页顶栏里的停在 `shell.page`；切换器选中段开不开菜单也只看 `moving`，不看 `pager.page`。
各页顶栏里的东西一律不读 `position` 和由它算出的 `pager.page`。`settle` 另有 1.2 秒兜底（`settle lost`）。

**顶栏**：每页各画一个（`AssistantTopBar`、`AgentsTopBar`），滑动中外壳画一个 `ShellMovingBar` 顶替；同一时刻只有一条在。
三条都用 `.shellTopBarScaled()`，字号到 `Theme.Shell.topBarLargestTypeSize`（xxxLarge）就不再变大（#340）。顶替的那条要是还用 `.shellScaled()`，辅助功能大字号下会比两页的顶栏大，滑动开始和停下时顶栏会跳一下。`ShellTopBarScaleTests` 检查这三条。

## 现在：`ShellState.page` 是唯一来源

```
 通知 / Siri / 收件箱 / 抽屉 …        切换段（含滑动中顶替的那条）      拖动松手（pager.settled）
            │                                   │                            │
   AppModel.selectedTab = x                     │                            │
            │ didSet，当场（tabRouter）          │                            │
            ▼                                   ▼                            ▼
   ShellState.route(x)  ───────────────►  ShellState.select(page)  ◄─────────┘
                                          │  page = p（唯一来源）
                                          │  pager.slide(to: p)（幂等；没开始的被替换，进行中的掉头）
                                          │  AppModel.selectedTab = 对应的 tab（给别处读）
                                          ▼
     读 page 的：切换段的选中态和 Menu、哪页接触摸 / 被 VoiceOver 读、tabIsActive、抽屉能不能拉（当前页在不在根部）、探针
     读 position 的：药丸、两页滑动的位置、顶栏在滑动中让位
```

- **一个来源**：`ShellState.page`（0 助手、1 Agents、2 为用户开启后的消息）。改它只有两个方法：`select`（切换段、拖动 settle）和 `route`（任何地方写了 `AppModel.selectedTab`）。
  两个都是**当场**做完，不等视图重画：`AppModel.selectedTab` 的 `didSet` 直接调 `tabRouter`（外壳出现时 `attach`，消失时 `detach`），
  不再用 `onChange`。`.mail` 推收件箱回到助手页，`.settings` 开设置浮层、tab 回到在前的那页；打开「旧布局」时外壳不再接路由，交给 tab 栏。
- **横条跟着 page 走**：`ShellPager.slide(to:)` 记着要去哪（`goal`），同一个目标再来一次什么都不做；还没开始的滑动被新的替换，进行中的掉头；
  没有位移可动画时直接停下（不会因为等不到动画结束而一直 `moving`）。拖动时手指说了算，松手 settle 到哪页就 `select` 哪页。
- **切换段只读 page**：选中态是 page；只有横条静止在这一页时，这一段才是 Menu（再点打开选项），滑动中两段都是普通按钮。
  点击直接 `select`，不经过 Binding。滑动中顶替的 `ShellMovingBar` 上的切换段也接点击：动画中来的点击不丢，最后一次说了算。
  横条在手指按着时停下（点击刚好赶上动画结束）时，`ShellMovingBar` 留到这根手指抬起（`ShellPager.lingering`，窗口上的 `Watcher` 报手指按下 / 抬起）：
  原来停下就拿掉，按在它切换段上的那一下没有着落，BeautifulUI 下动画更慢，`testQuickTapsEndOnTheLast` 几乎每次都碰上（第三下点击丢了）。
- **不在前面的页**：只有「横条静止在这页而且 page 就是这页」时接触摸、被 VoiceOver 读；`tabIsActive` 跟 page（拖到一半不算换页）。
  顶栏只画在前面那页的，滑动中只有外壳那条。
- **导航栈和抽屉**留在各自的页里：两页的 `path` 有各自的规矩（搜索结果先收起搜索再推、空间页的深度表…），外壳只需要「当前页在不在根部」，
  `ShellState.atRoot` 按 page 取。这个值是 SwiftUI 状态传过来的，偶尔落后：Agents 上推进的对话里从左边缘右滑，横条把手势接走、
  翻到了助手，对话还推着（矩阵 24 次里 3 次）。所以横条和抽屉的拖动在开始时再问一次 UIKit：手指下的导航栈推了页，就让给它的返回手势
  （`DrawerPan.pushed`）。

### 为什么顶栏还是每页一条

主会话的方向是「外壳统一画一份」。没有这么做：两条顶栏的内容是各页自己的（助手的对话菜单要读当前对话和连接详情，Agents 的列表选项绑在
`ChatsView` 的状态上，搜索时 Agents 的顶栏要让位给系统搜索框），挪到外壳要把这些状态都往上提，改动大、又会碰到 #178 的重建问题。
现在同一时刻只有一条顶栏在：静止时是在前那页的，滑动中是外壳的。用户看到的、能点到的都只有一条，效果一样。

## 启动

ovis 车道发现启动时对话页的 `appeared()` 连着跑了几次（挂在里面的 DEBUG 开关重复发了消息）。按 `-openSession`、恢复上次的对话、
连着 hub（`-pairURL`）、连接提示开合这几种启动方式数了一遍（`probe.chat` 的 `appeared` 和 `ShellTrace`）：打开的那个对话在 master
和这个分支上都只出现一次，导航也没有来回切（路由一次、横条不动画地到位）。多出来的一次是**另一个对话**：外壳两页都建，从 Agents
启动时助手页也建了，它的对话在后面「出现」了一次：订阅、`show`、标为已看，挂在 `onAppear` 上的东西都跑了，用户却没看到它。旧布局的
TabView 只在显示时建 tab，没有这个问题。

现在两页都是第一次选中或拖进来时才建（`ShellState.assistantBuilt` / `agentsBuilt`），外壳一开始就停在 App 的 tab 那页
（`ShellState(page:)`，不等 `onAppear` 里的第一次路由）。`ShellLaunchTests` 按上面几种启动方式断言：启动后只有在前那页的对话出现、
只出现一次，后面那页没有对话。

## 状态行（连接提示）和 toast

原来弱网的「连接中…」药丸浮在顶栏下面，正好压在 Agents 的筛选条和拉出来的搜索框上；全局 toast 浮在窗口顶部，新外壳里盖住头像、切换和新建按钮，
点到它就把它关掉，点切换的那一下被吞了（2026-11-02）。当时两样都进了状态栏下面的同一行（`ShellStatus.swift`）。
后来用户反馈（2026-11-04，#1053）：在侧栏归档一个对话，「已归档」进了这一行，把侧栏和页面往下推一下再推回来，「需要用覆盖在上层的 toast 实现」。现在按这条分：

- **持续的状态进状态行**：hub 的连接（等待网络 / 连接中… / 更新中…）。它会持续一阵、反映的是现在的情况，让位置、不压控件是对的。
- **一次性的结果是 toast**：刚做完一件事的结果（已归档、已删除、已置顶、已复制、已提交、发送失败、部署失败……，也就是所有 `AppModel.toast`）。
  它短（info / success 2.5 s，error 4 s），自己消失，**不占位、不推动任何布局**，浮在最上层：外壳把它画在自己的一个窗口里（`ToastWindow`，
  在外壳窗口上面一层），所以抽屉、两页、推出的页面、外壳上弹出的 sheet 都在它下面；状态行开着时它在状态行下面，不叠在连接提示上。
  键盘在底部，toast 在顶部，不会被挡。
- **toast 不吞点击**：那个窗口不接任何触摸（`hitTest` 返回 nil），toast 下面的切换、按钮照常能点；所以它也不能点掉，到时间自己走。
  因为 VoiceOver 不会移到一个不接触摸的窗口上，toast 出现时直接朗读出来（`AccessibilityNotification.Announcement`）。
  同一时间只有一条，新的换掉旧的（和旧布局一样）。样子是设计系统的 `ToastCapsule`：液态玻璃的胶囊，BeautifulUI 风格下是它凸起的胶囊。

状态行本身：

- **占位置，不浮**：这一行出现时，外壳把窗口根控制器的 `additionalSafeAreaInsets.top` 加上这一行的高度（`ShellWindowHooks`），所有页面、顶栏、
  系统导航栏、列表、推出的页面、打开的抽屉一起往下让；行画在让出来的空当里。只用 SwiftUI 的 `safeAreaInset` 不行：外壳自己画的顶栏让了，
  底下 UIKit 的导航栏和列表没让，顶栏反而压到了筛选条上。
- **手指在动时不展开、不收起**：窗口上一个只看不抢的手势识别器（不开始、不延迟、不取消任何触摸）记着有没有手指按着，松手后等到所有
  滚动视图停下来。这期间展开和收起推迟（内容会在手指下平移），最多等 3 s；等待网络不等。
- 旧布局、iPad、Mac：toast 是 `ToastOverlay`（同一个 `ToastCapsule`，点它关掉），连接状态在标题里。外壳在的时候 `ConnectionStatus.shown`
  一设就转给这一行（`onShown`），和 tab 一样当场做，不靠视图重画。行的高度也是当场推给窗口（`onRoom`），不经过外壳的 body：经过 body 时
  慢了一秒多，药丸先压在了切换上。
- 让位置时，停在顶端的列表保持在顶端（跟着整页一起移）：offset 按变化之前的值加减这一行的高度来定。
- **还没解决（#413）**：这一行收起时，导航栈里的列表有几帧读到 inset 为 0（导航栈重新排版，四种做法都有：SwiftUI 动画、
  不加动画、`UIView.animate`、不在动画里排版），行跳约 210pt，导航栏偶尔（实测 6 次里 2 次）把这当成一次滚动，把 Agents 上拉出来的搜索框收了。
  现在 0.6 s 后把列表放回原来停的位置，搜索框跟着出来，但看得到收起又出来的那一下。
  - 逐帧量：`probe.agents-scroll` 的 `room`；`ShellRoomScrollTests` 断言开合中搜索框高度不变、行一帧不跳超过 12pt、收起后回到原处。修好之前它会挂，
    只在 `TEST_RUNNER_JARVIS_KNOWN_ISSUES=1` 时跑，不进验收。
  - 试过、不行的做法（数据在 #413）：从 UIKit 改 SwiftUI 的导航控制器（safe area、transform）、开合期间自己管列表 inset、关 `hidesSearchBarWhenScrolling`，
    都会被 SwiftUI 改回去或带出别的跳动；Agents 根页的导航栏也没法为它变高（紧凑导航栏只有 54 / 88 两档，88 已经给顶栏的 clearance 用了）。
- 这一行出现、消失只改透明度，不插入、不移除视图：用 `.transition` 插入时，第二次起停在透明（看不见，VoiceOver 也读不到）。

DEBUG：`-showConnectionCycle connecting:7:4`（第 7 秒起显示连接中… 4 秒，然后恢复；`waiting` / `connecting` / `updating`）、
`-showToasts "5:info:文字;5.4:error:文字"`（这些时刻弹 toast）。

| 项 | 定义 | 测试 |
| --- | --- | --- |
| 不压控件 | 连接提示和头像、切换、新建、筛选条、搜索框、输入框都不相交；三种连接状态都测 | `testTheLinkTakesItsRoomInEveryState`、`ShellSearchTests` |
| 不跳 | 展开时顶上的控件和屏幕上的行都往下移同一个距离（这一行的高度），行的顺序不变；收起后回到原处 | `testTheLinkTakesItsRoomInEveryState` |
| toast 不推布局 | toast 出现、消失时 Agents 列表第一行（`probe.agents-scroll`）、新建按钮、抽屉里的行都不动，状态行不展开 | `ShellToastTests` |
| toast 不吞点击 | toast 显示时点它下面的切换、抽屉里的行照常生效 | `ShellToastTests` |
| toast 不被键盘挡 | 键盘弹着时 toast 整个在屏幕里、和键盘不相交 | `ShellToastTests.testAToastWithTheKeyboardUpIsAboveIt` |
| 手指优先 | 手指按着时连接中…不展开，松手后才展开；等待网络立刻展开 | `testTheRowWaitsForTheFingerUnlessUrgent` |

## 一致和及时（验收标准）

这块的「真实状态」在 App 里（不是 hub 的数据）：用户最后一次点的段、拖动 settle 的页、最后一次路由。

| 项 | 定义 | 上限 | 测试 |
| --- | --- | --- | --- |
| 一致 | 静止时 横条停的页 == 切换段选中的页（`page`）== App 的 tab；没有字段允许延后 | — | `agentsDrawn` / `assistantDrawn` 三项都要对上 |
| 及时 | 点击、路由以后横条静止在目标页 | 动画约 0.4 s；测试连探针读取的开销给 1.5 s | `ShellFinger.switchTo`、`expectShell` |
| 不吞点击 | 动画中、连点、返回动画中的点击都算数，结束在最后一次点的那页 | 同上 | `testQuickTapsEndOnTheLast`、`testBackThenSwitchAtEveryPace` |
| 收敛 | 点击、拖动、路由、进出对话任意组合以后，回到上面三项一致 | 同上 | `ShellRaceTests` 各项、`ShellPagerTests` |

返回动画还没结束就点，点击可能落在正在滑走的对话页上（和手指一样），这时测试允许 2.5 s，并要求随后的点击正常。

和 msg-render 车道共用一套探针和断言（ScreenProbe，`docs/ios-state.md`「探针」）：外壳的状态是 `probe.shell`，启动时要 `-probes YES`；
测试端 `ShellPageProbe.swift` 的 `shellPage` / `shellValue` / `shellRoom` / `shellTrace` 从它读（#499，原来的 `shell-pager-probe` 已删掉）。

## 测试

| 测试 | 场景 |
| --- | --- |
| `ShellRaceTests` | 用户这次的流程：看完 Agent 的对话返回（按钮 / 右滑，0–1 s 后点）、流式输出中退出、快速连点、点了马上拖、从空对话的「需要你」路由过去再切 |
| `ShellInboxAgentTests` | 收件箱（推在助手上）的「需要你」点进 Agent 对话 → 返回 → 切到助手（还是收件箱，自己的导航栏）→ 返回 → 切回 Agents：每步探针一致、切换器不在推出的页上、收件箱能滑能点、没有 `settle lost` |
| `ShellSwitchTests` | #182 等：抽屉里的页面（知识库、收件箱、定时任务、机器）推出再返回以后切换，坐标点击 |
| `ShellSearchTests` | #174：搜索让位、取消后切换 |
| `ShellPagerTests` | #190 左右滑翻页、56b2ead 后筛选条上右滑翻回 |
| `ShellAvatarTests` | #203：两页的头像和抽屉，之间用坐标点切换 |
| `ShellTests` | 切换、抽屉、旧布局开关 |
| `ShellLaunchTests` | 启动进对话（`-openSession`、恢复上次的、连着 hub、连接提示开合中）和启动在助手：只有在前的那个对话出现，且只出现一次 |

切换段一律用 `ShellFinger`（`PersonaUITests/ShellPageProbe.swift`）点：启动时从助手页的切换段读一次两个段的位置，之后都点这两个坐标，
点完断言三项一致和时限；失败时打印探针和最近的 `ShellTrace`。

### 可选的消息页（#392）

飞书设置的「首页显示消息」默认关闭，打开后外壳增加第三页，复用同一个 `ShellState.page`、路由器、顶栏切换和拖动模型。消息页首次选中或拖入才创建；设置浮层不会把当前消息页切回助手。关闭开关时如果正在消息页，立即回到助手，过期的 `.feishu` 路由也回到助手，不留下不可见页面。`lark-connectorShellTests` 覆盖三页往返、拖动反向取消、关闭与重新开启、消息页启动及设置返回。
