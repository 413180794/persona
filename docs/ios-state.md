# App 的状态和重绘（iOS）

hub 一直在推（真实 hub 实测：某台机器的会话每 2 秒一次），界面要做到三件事：
- 只重画变了的那一点；
- 用户正在操作时不被打断；
- 屏幕上说的永远是真的，而且来得及时。

这份文档讲：
- 现在的数据怎么从推送走到屏幕；
- 一次推送实际让多少视图重算（量出来的）；
- 根因是什么；
- 分几步治，每步怎么验收；
- 「真实、及时」怎么定义、怎么测。

前因见 `docs/archive/lanes/live-updates.md`（三次「推送打断操作」）。

## 现在的数据流

```
WebSocket ──解码（后台线程）──▶ AppModel.handle(_:)（主线程）
   sessions   ─▶ sessionsByMachine[机器] = 整份列表（old != new 才赋值）
                   didSet ─▶ allSessions（全部拼起来）、sessionIndex、每个会话的 SessionCell、saveSnapshot（2 秒去抖）
   machines   ─▶ machines（不同才赋值）─▶ machineChoices（名字、图标变了才变）
   interaction / interaction_resolved ─▶ interactions[id]
   revs       ─▶ AgentSync（后台拉对话，写对话缓存）
   items / delta ─▶ 那个对话的 ChatStore
   lark / inbox / fleet ─▶ LarkSync、InboxStore、灵动岛
                                 │
                                 ▼  @Observable：读过哪个属性，哪个属性一变就重算
  视图 body ── 自己读模型（model.allSessions、model.machines、model.session(key)…），在 body 里排序、分组、计数
```

- `AppModel` 是一个 1300 行的 `@Observable`，会话、机器、等你处理的事、当前 tab、草稿、风格都挂在上面。
- 会话推送是「一台机器的整份列表」。
  - `Session` 一个 struct 里既有身份和布局字段（id、标题、空间、槽位），也有每次都变的字段（`rev`、`updated_at`、`last_text`、子 Agent）。
  - 真实 hub 上每条 `sessions` 推送都带新的 `rev` / `updated_at`（10 分钟 255 条，`rev` 变了 320 次、`updated_at` 243 次），所以 `old != new` 几乎总成立。
- `SessionCell`：每个会话一个可观察的格子。看一个会话的视图（对话页）通过 `model.session(key)` 只跟着那一个会话。
  看全部会话的视图读 `allSessions`，任何一台机器的任何一个会话变了都重算。
- 派生数据在 body 里算：
  - Agents 列表的分组、排序（`SpaceGroup.build`、`AgentListOrder`）、筛选条的计数，每次 body 都从 `allSessions` 重算一遍；
  - 角标（`agentsWaiting`）是计算属性，读的人每次推送都重算。
- 副作用：`machines`、`sessionsByMachine`、`interactions`、`assistantConfig` 的 `didSet` 都调 `saveSnapshot()`，已经 2 秒去抖（每次推送只是取消、重排一个 Task）。
  真正的风暴是 `AgentSync` 每拉一次对话就写一次 UserDefaults，App 里每个 `@AppStorage` 视图都重算，#215 已改。

### 谁读了什么

| 属性 | 读它的视图（文件） | 每次推送都变？ |
| --- | --- | --- |
| `allSessions` | Agents 列表（#215 后经 `AgentsFeed`）、对话页、助手页、空间页、机器的空间页、收件箱动作、会话面板（7 个文件） | 是 |
| `sessionsByMachine` | 助手资料页、机器详情、端口在线检测（`PortPresence`）、`AgentSync` | 是 |
| `pendingInteractions` / `interactions` | Agents 列表、收件箱、旧布局的 tab 角标、新外壳首页 | 有人提问 / 答完时 |
| `machines` | 设置、机器页、新对话、灵动岛设置、助手页 | 连上 / 断开 / `last_seen` |
| `session(key)`（`SessionCell`） | 对话页、行菜单 | 只在那个会话变时 |

### 一次推送的代价

`RenderBudgetTests`（mock，页面静止，每种推送 5 次取平均）：每次推送是那台机器的整份会话列表，其中只有一个会话的一个字段变了。
- `rev`：对话变长了，屏幕上什么都没变（真实 hub 上大多数推送是这种）；
- `last`：一个 Agent 的最后一句变了。

| 页面 | 布局 | `rev` 一次 | `last` 一次 | 重算的视图 |
| --- | --- | --- | --- | --- |
| Agents 列表 | 新外壳 | 5 | 5 | `AgentsList`、`SpaceHeader`、`SlotRow`、`StateCounts`、`AssistantSidebar` |
| Agents 列表 | 旧布局 | 4 | 4 | 同上，没有侧栏 |
| Agent 对话 | 新外壳 | 5 | 5 | `ChatView`、`ChromeHeader`、`SessionModeBar`、`Composer`、`AssistantSidebar` |
| Agent 对话 | 旧布局 | 4 | 4 | 同上，没有侧栏 |
| Persona 对话 | 两种 | 1 | 1 | `AssistantSidebar`（助手的会话侧栏，读全部会话；新外壳里在 Agents 页上它也在） |

读法：
- 只数打了 `PerfTrace.body` 的视图，`List` 里的 cell、系统控件不在里面，实际更多；
- `rev` 和 `last` 一样贵：屏幕上没变的推送和变了的推送代价相同，这就是「粒度太粗」和「易变和稳定混在一起」；
- 对话页：推送改的不是这个会话时它也重算（`ChatView` 为提及、引用读了 `allSessions`，下面几层跟着）；
- `SlotRow` 每次 1 个：行已经是 `Equatable`，只有变了的那行重画，贵的是它上面那几层；
- 第 1–3 步的目标：`rev` 推送 0 个，`last` 推送只剩显示那句话的行。

第 1 步之后（同一测试）：

| 页面 | 布局 | `rev` 一次 | `last` 一次 | 重算的视图 |
| --- | --- | --- | --- | --- |
| Agents 列表 | 新外壳 | 2–2.6 | 2–2.6 | 变了的那行 `SlotRow`、`AssistantSidebar`；跨过整分钟的那次推送列表重排一次（下面） |
| Agents 列表 | 旧布局 | 1–1.6 | 1–1.6 | 变了的那行；跨过整分钟的那次推送列表重排一次 |

- 剩下的列表重排：mock 每次推送 `updated_at` 都是「现在」，跨过整分钟时空间标题的「n 分钟前」要变，算结构（见「第 1 步」）。
  5 次里正好有 1 次，落在 `rev` 还是 `last` 看那次跑到哪一秒，所以两列都是 2–2.6（旧布局 1–1.6）。
- `AssistantSidebar` 读全部会话，在新外壳的每一页都在，留给第 3 步。
- 对话页和第 0 步一样（新外壳 5、旧布局 4，第 2 步的事）。有一次单独跑量到 `rev` 推送时 Agent 对话重画了 7 个气泡（旧布局 1.4 个），
  完整矩阵重跑是 0，没再出现；对话页不读 `AgentsFeed`，第 2 步给气泡数加上限再看。

第 2 步之后（同一测试，量之前先等页面不再重画，见下「第 2 步」；两种布局各跑 2 次）：

| 页面 | 布局 | `rev` 一次 | `last` 一次 | 重算的视图 |
| --- | --- | --- | --- | --- |
| Agents 列表 | 两种 | 1–1.6 | 2–2.6 | `rev`：只有那一行的时间 `AgentTimeText`；`last`：那一行 `SlotRow` 和它的时间；跨整分钟那次列表重排一次（0.6） |
| Agent 对话 | 两种 | 0 | 4 | `last`：`ChatView`、`ChromeHeader`、`SessionModeBar`、`Composer`（这个会话的最后一句变了，`SessionCell` 发布） |
| Persona 对话 | 两种 | 0 | 0 | |

- 「7 个气泡」是测试量早了：每页第一个量的是 `rev`，App 刚启动、对话还在加载历史和排版，气泡的 body 算在了第一次推送头上。
  现在 `RenderBudgetTests` 先等计数 1 秒不变再量，两种布局各两次都是 0。
- `AssistantSidebar` 不再出现：收起时连「已归档」的计数也不读了（它也遍历全部会话）。
- 对话页的 `last` 还是 4：最后一句是会话的内容，`SessionCell` 照样发布；那 4 个视图里没有一个显示最后一句，第 3 步把它们读的东西拆细。

第 3 步之后（同一测试，两种布局）：

| 页面 | 布局 | `rev` 一次 | `last` 一次 | 重算的视图 |
| --- | --- | --- | --- | --- |
| Agents 列表 | 两种 | 1 | 2 | `rev`：那一行的时间 `AgentTimeText`；`last`：那一行 `SlotRow` 和它的时间；跨整分钟不再重排 |
| Agent 对话 | 两种 | 0 | 0 | |
| Persona 对话 | 两种 | 0 | 0 | |

到这里一次只改一个会话一个字段的推送，重算的只剩显示这个字段的叶子视图：第 1–3 步的目标达到了。

## 根因

| 类 | 现在 | 后果 |
| --- | --- | --- |
| 粒度太粗 | 一台机器的整份会话列表一起替换；读 `allSessions` 的视图跟着任何一个会话变 | 一个会话换一句话，所有列表页、助手页都重算 |
| 易变和稳定混在一起 | `rev`、`updated_at`、`last_text` 和标题、状态、空间在同一个 struct | 只想知道「状态变没变」的视图也被 `rev` 抖动叫醒 |
| 派生数据在 body 里算 | 排序、分组、计数、角标在 body 里从全量重算 | 重算本身贵；结果没变也算一遍；排序键用了易变字段（#215 前空间按 `updated_at` 排） |
| 副作用在 didSet 里 | `saveSnapshot` 去抖了；`AgentSync` 写 UserDefaults（已改） | 一次写全 App 重算 |
| 多份真相 | 当前页：`AppModel.selectedTab` 和外壳的 `ShellState` 各一份，靠 `onChange` 同步（live 车道在改成唯一来源） | 会漏，界面和状态对不上 |
| 「用户正在操作」各页自己处理 | 菜单、长按、滚动各有各的补丁（#131、#178、#215） | 漏一处就回来一次 |

## 方案（分步，每步一个 PR，每步用数字验收）

| 步 | 做什么 | 验收（数字） |
| --- | --- | --- |
| 0（#243，已合） | 屏幕探针 + 真实性 / 及时性测试 + 关键更新不扣留 + 落地 0.5 s / 最长 5 s；量出现状 | `ScreenTruthTests` 两种布局 × 两种风格连跑 3 轮全绿；本文「一次推送的代价」有数 |
| 1（#338，已合） | Agents 列表的结构和行分开发布（见下「第 1 步」）：每行跟着自己的 Agent（`AgentsFeed.Row`），列表只在结构变了时重排 | 只改一个会话一个字段的推送：重算的视图数 ≤ 读这个会话的叶子视图 + 1；列表 body 0 次（结构没变时） |
| 2（#620，已合） | 易变字段拆出去：`rev`、`updated_at`、token、端口放进单独的小对象，只给显示它的叶子视图（相对时间、最后一句）读，照 `WaitingBadge` 的做法 | `rev` / `updated_at` 抖动的推送：除了显示时间的那个 `Text`，0 个视图重算 |
| 3（#639，已合） | 派生数据进模型层：排序、分组、筛选计数、角标在模型里算，结果真变了才发布（`AgentListOrder` 搬进 `AgentsFeed`） | 列表 body 只在顺序 / 分组 / 计数变了时跑；角标只在数字变了时跑 |
| 4（#880，已合） | 「用户正在操作」做成模型层统一策略：拖动、惯性、菜单开着、键盘在输入时，非关键更新合并、延后；关键的立刻；最长延迟有上限。`AgentsFeed` 的扣留抽成通用的 `Hold`，各页登记自己的状态 | 现有的 `LiveUpdateTests`、`ScrollUnderPushTests`、`ScreenTruthTests` 全绿；页面里不再有各自的扣留代码（源码检查） |
| 5（#913，已合） | 护栏：性能测试「一次只改一个会话一个字段的推送，最多 N 个视图重算」，超了就失败；源码检查「列表以外的视图不读 `allSessions`」 | 两个检查进 `JarvisTests` / UI 测试 |
| 6（#270，已合，testflight 车道） | hub 只在语义变化时推（`rev`、`updated_at` 抖动不推，改走 `revs`），或者推增量。做成了增量协议 `sessions_delta`：hub 按设备记住发过的每个会话，只推变了的和删了的 | 会话推送每分钟 2605 KB → 79 KB |

### 第 1 步：列表的结构和行分开

原计划是在模型里做 `SessionStore`（每个会话一个对象，字段级 diff）。实际最贵的是 Agents 列表的 body（分组、排序、折叠、计数），先在它的 `AgentsFeed` 里分开：
- 每个 Agent 一个 `AgentsFeed.Row`（`@Observable`），行（`SlotRow` / `CompactSlotRow`）从它读，推送只改变了的那几个 `Row`；
- 列表 body 读的 `sessions` 只在结构变了时换：比较 `AgentsFeed.shape`，即去掉只有行才显示的字段之后的会话（`rev`、最后一句、端口、排队、建议、模式、模型、已读时间；时间按分钟；子 Agent 和后台任务只看有没有在跑）；
- 搜索时列表按最后一句过滤，最后一句算结构（`searching`）；
- 扣留、关键直通照旧：`Row` 和结构一起落地，关键更新也只改列着的行（`landCritical`）；
- 探针 `probe.agents-list` 从各行读（`AgentsListProbe`），量的是画出来的，不是列表结构。

模型里的 `SessionStore` 原定等 testflight 车道的会话增量（第 6 步）合了再做；增量已由 #270 做完（`sessions_delta`，App 里合回 `sessionsByMachine` 的入口是 `SessionPatch`），第 1–5 步之后列表和对话页的重算已经只剩叶子视图，`SessionStore` 没有再做。

### 第 2 步：`rev`、`updated_at` 只叫醒显示时间的那个视图

一个干活的 Agent 每走一步，hub 推一次整份会话列表，其中这个会话只有 `rev`（transcript 变长了）和 `updated_at` 变了：
- `Session.sameApartFromTicks`：除了这两个字段都一样，就算没变；
- 模型的 `SessionCell`（`AppModel.session(_:)`，对话页、顶栏、各种菜单读它）只在别的字段变了时发布。所以它里面的 `rev`、`updatedAt`
  可能比列表里的旧：要这两个值的地方（`AgentSync` 按 `rev` 拉 transcript、排序、侧栏的时间）一律读整份列表（`allSessions`），不读格子；
- Agents 列表的 `AgentsFeed.Row.session` 同理；行的时间单独放在 `AgentsFeed.Row.time`（`AgentTime`），只有 `AgentTimeText` 读它，
  每 30 s 自己刷新「n 分钟前」；
- 新外壳的侧栏收起时不读「已归档」的计数（和会话列表一样，拉出来才读）。

真实性：时间每次推送都更新（`AgentTime` 跟着 `show()` 落地，扣留时和列表一起扣着）；状态、最后一句、提问照常走格子和行，
`ScreenTruthTests` 3 轮 × 2 种布局 × 2 种风格全绿。

### 第 3 步：派生数据进模型层

- 对话页不跟着最后一句重算：会话格子（`AppModel.session(_:)`）把最后一句也当成「没变」（`Session.sameForItsPage`），和 `rev`、`updated_at` 一样，
  对话页、顶栏、菜单都不显示它。要发出「这个会话现在怎样」的动作（交给 Persona 带上最后一句）点下去时读 `AppModel.latest(_:)`：
  不观察、直接取模型里最新的整个会话。格子里的值就是可能旧的，**显示用格子，发送用 `latest`**；
- 列表不跟着时间重排：`AgentsFeed.shape` 不再把时间按分钟算进结构，改成比较时间决定的顺序（`Shape.byTime`：按 `sortDate` 排好的 id）。
  行和空间的先后只由这个顺序决定，一分钟过去、顺序没变时列表 body 不跑；干活的 Agent 按开始干活的时间排，它的每一步都不改顺序。
  列表里的会话的 `updatedAt` 于是可能是旧的：唯一显示它的空闲空间那一行（`QuietSpaceRow`、`QuietSpaceCardHead`、`CompactQuietSpaceRow`）
  从各行的时间（`AgentsFeed.time`，经环境拿到 feed）取最新的；
- 角标在模型里算：`AppModel.badges`（`TabBadges`：Persona、Agents 两个数）在待处理、会话、屏蔽变了时数一遍，数字变了才发布；
  `RootView`、`MacRootView` 和新外壳的 `WaitingBadge` 只读这两个数。`LiveUpdateTests.testTheAgentsBadgeIsTheHubs` 核对角标和 hub 一致、
  来一个提问 2 s 内加一、答掉减一。
- 原计划把 `AgentListOrder` 整个搬进 `AgentsFeed`；量下来列表 body 已经只在结构变了时跑，搬过去只是让那一次便宜一点，没有做。

### 第 4 步：「用户正在操作」只有一套：`Hold`

- `Hold`（`apple/Persona/App/Hold.swift`）是唯一的扣留策略，和页面无关：
  - 理由两种：在滚（`scrolling`）、看不见（`outOfSight`）；
  - 在滚从第一下开始，到列表静止 `Hold.settle`（0.5 s）才算完，中间手指又落下就接着算；
  - 在滚时扣着的最多等 `Hold.cap`（5 s），到了照样落地；看不见没有上限，回来立刻落地；
  - 关键与否由数据方定：`Hold.wait()` 只说「这次要不要等」，等的时候关键的那部分照样立刻落（Agents 的 `critical` / `landCritical` 不变）。
- 页面只登记状态：Agents 列表把滚动阶段（`feed.hold.set(.scrolling, phase != .idle)`）和看不见（`feed.hold.set(.outOfSight, covered)`）报给 feed 的 hold，
  自己不再记滚动阶段、不再等 0.5 s。「看不见」读的是唯一的页状态：`AppModel.selectedTab`（新外壳翻页也写它）和外壳 pager 是否在动。
- `AgentsFeed` 只剩数据：什么算关键、关键的怎么只落一部分；什么时候落地交给 `Hold`。
- 一处行为变化（看不到）：在滚的时候页面被盖住（列表里点开一个对话），停下以后 5 s 上限的计时停掉，回到列表时立刻落地；原来计时照走，到点在看不见的页上落一次。
- 菜单、键盘：原计划把「菜单开着、键盘在输入」也做成扣留的理由，看下来没有一处需要：行的长按菜单是 `Equatable` 的（`AgentRowMenu`），推送不重建它；
  Agents 列表搜索时打字要立刻看到过滤结果；对话页的输入框不跟着推送重画。以后真有页面要，在 `Hold.Reason` 加一种，页面登记。
- 对话页的 `frozen` 不是扣留：往上读历史时新内容不插进来，是读者的位置（回到最新处才放进来，期间有 ↓），不归 `Hold`。
- 检查：`HoldTests`（静止才放、5 s 上限、看不见不计上限）；`HoldSourceTests` 扫源码：`.onScrollPhaseChange` 里不许自己等（`Task.sleep`、`asyncAfter`），
  `Views/` 里跟着模型的 feed（`withObservationTracking`）不许有计时器。

### 第 5 步：护栏

第 1–3 步的结果用两个检查守住，回退了测试就红：
- `RenderBudgetTests` 从只报数改成超了就失败：页面静止时只改一个会话一个字段的推送（各 5 次取平均），每次重算的视图数不能超过预算。
  预算就是第 1–3 步之后的数：Agents 列表 `rev` 1（那一行的时间）、`last` 2（那一行和它的时间），Agent 对话、Jarvis 对话都是 0；
  允许 5 次里多出 1 次（0.2，某一行的时间正好跨过刷新），多 2 次就算回退。
- `AllSessionsReadersTests`（JarvisTests，扫源码）：`Jarvis/Views` 和 `Mac` 里只有列 Agent 的视图能读 `allSessions`，每个文件读几次、为什么都登记在测试里，
  多一处、少一处都要改登记：
  - 只显示一个会话的读格子 `model.session(_:)`；
  - 点下去才要全部会话的动作（@ 菜单、引用另一个会话、下一个 / 上一个 Agent、终端换空间）读 `model.sessionsNow`：和 `allSessions` 同一份，不被观察，
    和第 3 步的 `latest(_:)` 一样，**显示用观察的，动作用不观察的**；
  - 新的列表要读，在登记里加一行写清楚为什么。

每步都要过本文「真实性与及时性」的测试，用户看得到的交互不变。和 live 车道的 `ShellState`（当前页唯一来源）对齐：第 4 步的「用户正在操作」读它的页状态，不自己再存一份。

## 真实性与及时性（每一步的验收标准）

用户的要求：减少重绘不能拿正确性换，屏幕上说的必须是真的，而且来得及时。

### 真实（一致）

- 真实状态以 **hub** 的数据为准，不以 App 的模型为准：
  - mock 的 `GET /mock/truth`：每个 Agent 的状态、最后一句、标题，等你处理的事；
  - 真实 hub 用 REST 接口。
- 屏幕上的状态由探针导出（`probe.<区域>`，见下）：
  - Agents 列表：每个 Agent 的状态、最后一句，列着的提问；
  - 连接：标题上的「等待网络 / 连接中… / 更新中…」。
- 一致：静止时，探针导出的每一项都等于 hub 的。允许延后的只有「非关键」字段，而且延后有上限（下表）。

### 及时（有上限）

| 更新 | 例子 | 上限 | 滚动、拖动中 |
| --- | --- | --- | --- |
| 关键 | 某个 Agent 开始 / 不再需要你、提问或审批来了 / 答完了、Agent 离线、机器离线或出错 | 推送后 1 s 内上屏 | 不扣留，立刻上屏，而且只上关键的这部分（见下）。列表仍可能在手指下挪动：顶上插进一张卡、空间升上去，这是马上显示的代价 |
| 非关键 | 最后一句话、顺序调整、时间、token | 手指停下后 0.5 s 内；一直在滚也最多 5 s | 合并、延后 |
| 页面看不见时 | 被对话盖住、在另一页 | 回到这页时立刻落地 | 不重画 |

谁扣、扣多久都是 `Hold` 的（见上「第 4 步」）。扣留中来了关键更新，只落关键的部分（`AgentsFeed.landCritical`）：
- 落什么：开始 / 不再需要你、离线的那几个 Agent 的状态，改在列着的行上；提问；机器；
- 不落什么：最后一句、时间、顺序，照常等手指停下或 5 s 上限；
- 为什么：一开始是连同扣着的全部一起落，最后一句改了行高，滑动中整表重排，旧布局实测跳了 104 pt（`ScrollUnderPushTests` 失败，列表在滚动中重画 10 次）。

为什么停下后等 0.5 s：
- 连着甩的几下中间只停半秒到一秒；
- 在两次之间落地，`List` 要到下一次手指落下时才排版，那一下会跳。

- `PerfScroll` 把这种跳另记为 `landed`：页面放开后 0.3 s 内，或静止时放开后的下一次滚动的前 0.5 s 内。它是更新按设计在静止时落地，不算滚动中被打断。
- 但用户看得到：停下、更新落地，下一次手指落下时内容挪一下（mock 上偶尔几百 pt）。
- 原因在 `List` 的行高估算：落地改了屏幕外行的高矮，UIKit 到下一次滚动才按实际高度重排。之前用 1.5 s 是为了躲开它，现在改回 0.5 s，
  治本放在第 1–3 步：落地时只改变了的行，列表结构和行高不跟着整体重来。

### 收敛

任意推送序列加任意操作结束后，屏幕在上限内回到 hub 的状态。

- 操作包括：滚动、拖动、开菜单、切页、进出对话、前后台、断网重连。
- 漏掉的更新有兜底：重连后重新拉会话列表（`hello` 之后 `refresh`）。断网期间 hub 上发生的事，重连后同样要出现。

### 不吞操作

- 延后换序时，点到的必须是用户看到的那一行：扣留期间列表不换序，点击作用在看到的行上。
- 落地发生在静止 0.5 s 后，不在手指按下的那一下。

### 不挪读者正看着的那一行

- 列表停在中间时落地（顺序变、空间升降、顶上插进一张卡、上面某行的最后一句变成两行），读者正看着的那一行留在原处，变化发生在它上下。
- 做法：`ReaderAnchor`。每行报自己在窗口里的位置；`AgentsFeed` 要改列表之前，记下正在读的那一行（中线落在的那一行）和它的位置，排完后差多少就把列表滚回多少：
  - 之后 1 s 内它每次报位置都校一次：`List` 换序后过一会儿才按真实行高排上面的行，只校第一次会差几十 pt；
  - 换序把它整个挪出屏幕（它的空间沉到很下面）时，它的格子没了、不报位置：先用 `ScrollViewReader` 滚到它，再按报上来的位置校准；
  - 正在读的那一行一直是同一行，直到用户自己滚动：连续几次换序时每次重新挑中间那行，挑到邻居，列表就被一路带走。
- 在最顶上时不保：新来的就该在顶上看到。手指按着或正在滑时也不保（这时只落关键的，见上）。
- 为什么（#339）：`List` 换序后保持的是滚动偏移，不是内容。上面的空间换了名次，读者那一屏整个换成了另一个空间；顺序是真的，可读者看着的东西没了。
- 验收：`LiveUpdateTests.testScrolledListHoldsStillThroughUpdates` 打印 `TRUTH reader row moved … pt`，两种布局都要在 2 pt 以内（修之前旧布局那一行整个不见了）。

### 陈旧可见

断网、重连、补数据的时候，标题显示「等待网络 / 连接中… / 更新中…」（`ConnectionStatus`，1.5 s 后显示，免得一闪）。

## 探针和断言（msg-render 和 live 共用）

App 端 `apple/Persona/App/ScreenProbe.swift`，DEBUG，`-probes YES` 才挂：

- `view.screenProbe("<区域>", page:, value)`：`value` 是 `Encodable & Equatable`。
  - 挂一个 1×1 元素，`accessibilityIdentifier("probe.<区域>")`；
  - `accessibilityValue` 是一行 JSON：`{"area","seq","at","page","v"}`。
- 字段：
  - `seq`：内容变一次加一；
  - `at`：最近一次变化的时刻，毫秒。模拟器和 Mac 同一个钟，测试能直接和自己发推送的时刻比，手指还按着时发生的变化也量得到；
  - `page`：新外壳里这块在哪一页。两页都建着，测试按它挑。
- 区域：
  - `agents-list`：`{states, last, asks}`；
  - `agents-scroll`：Agents 列表顶部的位置，UIKit 每帧读（`AgentsScrollProbe`），窗口坐标：
    - `now`：现在的位置 `{nav, search, inset, row, container, safe}`，依次是导航栏 y、搜索框高度（收起为 0）、列表顶部 inset、第一行 y、导航容器 y 和顶部 safe area；
    - `left`：推入别的页之前那一帧的位置；
    - `back`：最近一次返回的逐帧记录，从返回开始到之后 1 秒，每项 `{min, max}`，`rowStep` 是第一行相邻两帧最大的跳动，`n` 是第几次返回、`frames` 是帧数。
    - 用法见 `AgentsReturnTests`：返回前后 `row`、`nav`、`search` 都等于 `left`，范围不超过 1pt；`container`、`safe` 不变。
    - `room`：外壳状态行最近一次展开或收起的逐帧记录，从开始到之后 1.2 s，字段同 `back`，另有 `to`（让出的高度，收起为 0）；`row` 跟着开始那一刻屏幕上第一行走，上面新露出来的行不算跳。
      用法见 `ShellRoomScrollTests`（#413）。
  - `connection`：`{shown, network}`；
  - `chat`：`{key}`，打开的是哪个对话（page 是它所在的页）；
  - `shell`：新外壳（`ShellShown`，`docs/shell.md`），`{strip, selected, tab, position, moving, lingering, drawerOpen, chatRoot, workRoot, status, opened, folded, room, trace}`：
    横条停在哪页（滑动中为 `moving`）、切换段选中哪页、App 的 tab，状态行显示什么（`none` / `link` / `toast`）、开合了几次、让出的高度，`trace` 是最近的外壳事件。

测试端 `apple/PersonaUITests/ScreenProbe.swift`：

- `app.probe(area, as:, page:)`、`app.probeSeq`、`app.probeChanged`（`at` 转成 `Date`）；
- `eventually(what, within:, poll:) { 差异 }`：反复读直到没有差异，返回用时，超时失败并带上最后的差异。`within: 0` 就是「现在一致」；
- `differences(shown:truth:deferrable:)`：逐项比，`deferrable` 里的字段不算。

测试：`ScreenTruthTests`（`docs/testing.md`「真实性与及时性」）。

## 回到最新：对话页的「跟随最新处」（2026-11-04，梳理，未改行为）

Agents 对话和 Jarvis 对话是同一个 `ChatView`：列表倒着放（最新的在偏移 0），右下角的 ↓（「回到最新」）和「跟随最新处」都在这里。
下面是现状、实测出的问题，和一个对照 Vercel `useScrollToBottom` 的单一真相方案。复现用 `ScrollToBottomTests`（每种情况记下
`probe.chat-rows` 的时间线：按钮、跟随、扣住、哨兵、离最新处多远、手指、pin）。

### 现在有几份真相

| 状态 | 是什么 | 谁写 |
| --- | --- | --- |
| `atBottom` | 最新那一端 1 pt 的哨兵在屏幕上（`onScrollVisibilityChange`，阈值 0.01） | 哨兵；`landed()` 在离最新处超过一屏时强制 false（#597） |
| `scratch.atNewest` / `newestGap` | 滚动几何：偏移是否在最新处、差多少 pt | `onScrollGeometryChange` |
| `followsEnd` | 意图：新内容来了要不要带着列表走 | 见下表，十来处 |
| `frozen` | 扣住：往上读时，快照之后的条目、流式文字、待发气泡都不进列表 | `followsEnd` 变 false 时拍快照，变 true 时清掉；`store.resets` 清掉 |
| `settled` | 进对话后 0.4 s 内不出 ↓（免得落地时闪） | `settleAtEnd`；切页、换会话时重置 |
| `userScrolled` / `scratch.userDragging` | 用户拖过 / 手指正按着或惯性中 | 滚动阶段 |
| `scratch.landingInFlight`、`entry` | 正在落到「上次读到的地方」或「最新回答的开头」 | 进对话 |
| `pin.holding` | 点开 / 折叠的标题正被保持在手指下（1.5 s） | `ChatPin` |

↓ 只看 `!atBottom && settled`，不看 `followsEnd`；扣不扣看 `followsEnd`。两件事由两份不同的状态决定。
（Agents 列表的 `Hold` 不管对话页：对话页的「用户正在操作」是上表这些。）

### `followsEnd` 的迁移

| 从 | 到 | 触发 |
| --- | --- | --- |
| 跟随 | 不跟随 | 手指拖动（`tracking` / `interacting`，哪怕 1 pt）；落到保存的位置（`landEntry`）；点开或折叠工作行（`changeWork`，#862） |
| 不跟随 | 跟随 | 哨兵出现且 pin 没在保持；新内容来时 `atBottom` 且几何也在最新处、没拖、没在落地、pin 没保持（`contentArrived`）；滚动停下且在最底（`userScrolled`）；落地结束时在最底（`landed`）；点 ↓（`jumpToEnd`）；发出消息（`store.pending` 变多，或自己的消息到了 transcript 且发出后没滚过）；看子 Agent；从文件 / Git 切回对话；换会话；折叠后内容一屏放得下（#862） |
| 跟随时新内容到 | — | `contentArrived` → `scrollToEnd()`（已在最新处就不动；`ChatStreamFollow` 每 40 个字一次） |
| 键盘弹起 / 收起 | — | 列表变矮，偏移跟着变：`scrolled(by:)` 按高度变化跳过，不算用户滚动；倒着的列表自己留在最新处 |

### 实测（两种布局，mock；`ScrollToBottomTests`）

| # | 情况 | 结果 |
| --- | --- | --- |
| 1 | 流式输出时手指往上拖 40 pt 松开 | **不对**：立刻不跟随，`frozen`，↓ 出现，离最新处 34 pt。之后流式文字和新步骤全被扣住，直到手动拖回精确的最底或点 ↓。两种布局一样 |
| 2 | 往上翻几屏后往下甩 | 甩停在离最新处 1332 pt，↓ 在、不跟随：对。没测到「甩到快到底」：mock 的历史太长，一甩到不了 |
| 3 | 在最新处点开运行中的工作行，再折叠 | 点开：标题保持在手指下，步骤把最新处推出 1820 pt，↓ 出现、不跟随、**扣住**——这一轮后面的步骤不再进列表，打开的工作行停在点开那一刻（**不对**：正在看的就是这一轮）。折叠：回到最新处、跟随，对 |
| 4 | 键盘弹起 / 收起 | 新外壳：一直在最新处、跟随，↓ 不出不闪：对。旧布局 + 液态玻璃：输入框不在无障碍树里（#1006），没测到 |
| 5 | 往上读时发消息 | **没测到**：往上翻之后测试在 Agents 对话里找不到输入框（两种布局），没查原因 |
| 6 | 往上读时推送进来，再点 ↓ | 读的时候 ↓ 一直在、一直不跟随、条目在后台进来但被扣住：对。点 ↓ 0.11 s 后回到最新、跟随、↓ 消失：对 |
| 7 | 退出对话再进来 | **没测到**：返回列表后测试找不到能点的 Agent 行（选择器问题），跳过 |
| 8 | 切后台 6 s 再回来 | 回来后在最新处、跟随，↓ 不出、不闪：对 |
| 9 | 一屏都不满（新对话），手指拖一下 | ↓ 从不出现，跟随不断：对（拖不动，滚动阶段不进 `tracking`） |
| 10 | 好几张待处理卡片（#939） | 静止时不出 ↓、跟随：对。那个对话只有两条，没法往上翻，「↓ 被卡片挡住」没测到 |

### 问题清单

1. **没有「接近最底」的容差**：只要哨兵（1 pt）离开屏幕就算不在最底，手指一碰（`tracking`）就不跟随并扣住。往上拖几十 pt 看一眼，流式回答就停了，
   只能精确拖回最底或点 ↓。Vercel 用 100 pt 的容差：在容差内算在最底，内容变长时照样带着走。（实测 #1）
2. **↓ 和「跟随」是两份真相**：↓ 看哨兵，扣不扣看 `followsEnd`。正常情况下两者一致，但只靠一处又一处补丁对齐（`landed` 的 #597、`contentArrived` 的 #754、
   哨兵在 pin 保持期间不算）。哨兵在懒列表里会被回收，可见性也比 pin 直接改的偏移晚一帧。
3. **点开运行中的工作行会冻住它**：点开是「用户接过列表」，不跟随就扣住，于是正在看的这一轮不再长新步骤。点开的人恰恰想看新步骤。（实测 #3）
   改法候选：扣住只扣「最新处之后插进来、会把正在读的东西推走」的条目；或者点开工作行只停止跟随、不扣住（pin 已经负责标题不动）。
4. **「用户在滚」只有手指那一刻**：`tracking` 一来就定成不跟随，没有 Vercel 那种「滚动结束后再判」：轻碰一下没真滚也算接过。可以改成在滚动停下时，
   按停下的位置（在不在容差内）决定跟不跟随。
5. **几处「回到跟随」的条件都在各自判断**（上表右列十来处）：新加一处很容易漏一个（`pin.holding`、`landingInFlight`、`userDragging`）。

### 方案：一份真相 + 一个函数

照 Vercel 的思路，但用我们已有的手势信息区分用户滚动和程序滚动（Vercel 分不清这两种）：

- **唯一真相 `follow: Follow`**（`@State`，同时放一份在 `scratch` 给回调同步读，免得闭包读到旧值，同 Vercel 的 state + ref）：
  `.following`（带着走）、`.reading(since: Date)`（用户在看别处，扣住）、`.landing`（落地中）、`.holding`（pin 保持标题中）。
- **在不在最底由几何决定**：`nearNewest = newestGap ≤ 100 pt`（容差同 Vercel）。哨兵只作为几何未就绪时的兜底，不再单独驱动 ↓。
- **迁移只在三个入口**：
  - 用户滚动结束（滚动阶段从 `tracking`/`decelerating` 回到 `idle`，即我们的「手指」信号，Vercel 没有）：`nearNewest` → `.following`，否则 → `.reading`；
    手指按着时不迁移（等松手），不管内容怎么变；
  - 程序动作：点 ↓、发消息、换会话 / 切回对话 → `.following`；落地 → `.landing` → 结束时按 `nearNewest`；点开 / 折叠工作行 → `.holding` → 结束时按 `nearNewest`；
  - 内容变长（新条目、流式文字）：只有 `.following` 才 `scrollToEnd`；`.holding` 交给 pin。
- **派生**：↓ = `follow != .following && !nearNewest && settled`；扣住 = `follow == .reading`（`.holding` 不扣：问题 3）。
- 放在一个小的纯类型里（`ChatFollow`），所有迁移是它的方法，单元测试覆盖上面每一条；`ChatView` 只把手势、几何、程序动作报给它。

验收：`ScrollToBottomTests` 从记录改成断言（每种情况的期望写在上面的表里），两种布局 × 两种风格；现有 `ChatFollowTests`、`RunningChatTests`、
`ScreenTruthTests` 不变红。
