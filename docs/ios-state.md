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

前因见 `docs/lanes/live-updates.md`（三次「推送打断操作」）。

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
| 3（本 PR） | 派生数据进模型层：排序、分组、筛选计数、角标在模型里算，结果真变了才发布（`AgentListOrder` 搬进 `AgentsFeed`） | 列表 body 只在顺序 / 分组 / 计数变了时跑；角标只在数字变了时跑 |
| 4 | 「用户正在操作」做成模型层统一策略：拖动、惯性、菜单开着、键盘在输入时，非关键更新合并、延后；关键的立刻；最长延迟有上限。`AgentsFeed` 的扣留抽成通用的 `Hold`，各页登记自己的状态 | 现有的 `LiveUpdateTests`、`ScrollUnderPushTests`、`ScreenTruthTests` 全绿；页面里不再有各自的扣留代码（源码检查） |
| 5 | 护栏：性能测试「一次只改一个会话一个字段的推送，最多 N 个视图重算」，超了就失败；源码检查「列表以外的视图不读 `allSessions`」 | 两个检查进 `JarvisTests` / UI 测试 |
| 6（要改 hub，先算收益） | hub 只在语义变化时推（`rev`、`updated_at` 抖动不推，改走 `revs`），或者推增量 | 用录下的 10 分钟推送回放：推送条数、App 主线程耗时的改前改后 |

### 第 1 步：列表的结构和行分开

原计划是在模型里做 `SessionStore`（每个会话一个对象，字段级 diff）。实际最贵的是 Agents 列表的 body（分组、排序、折叠、计数），先在它的 `AgentsFeed` 里分开：
- 每个 Agent 一个 `AgentsFeed.Row`（`@Observable`），行（`SlotRow` / `CompactSlotRow`）从它读，推送只改变了的那几个 `Row`；
- 列表 body 读的 `sessions` 只在结构变了时换：比较 `AgentsFeed.shape`，即去掉只有行才显示的字段之后的会话（`rev`、最后一句、端口、排队、建议、模式、模型、已读时间；时间按分钟；子 Agent 和后台任务只看有没有在跑）；
- 搜索时列表按最后一句过滤，最后一句算结构（`searching`）；
- 扣留、关键直通照旧：`Row` 和结构一起落地，关键更新也只改列着的行（`landCritical`）；
- 探针 `probe.agents-list` 从各行读（`AgentsListProbe`），量的是画出来的，不是列表结构。

模型里的 `SessionStore` 等 testflight 车道的会话增量（第 6 步）合了再做：增量合回 `sessionsByMachine` 的入口统一成一个函数，换实现只改那里。

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

扣留中来了关键更新，只落关键的部分（`AgentsFeed.landCritical`）：
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

App 端 `apple/Jarvis/App/ScreenProbe.swift`，DEBUG，`-probes YES` 才挂：

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
  - `connection`：`{shown, network}`；
  - `chat`：`{key}`，打开的是哪个对话（page 是它所在的页）；
  - `shell`：live 车道，`{strip, selected, tab, position, moving, drawerOpen, chatRoot, workRoot, trace}`。

测试端 `apple/JarvisUITests/ScreenProbe.swift`：

- `app.probe(area, as:, page:)`、`app.probeSeq`、`app.probeChanged`（`at` 转成 `Date`）；
- `eventually(what, within:, poll:) { 差异 }`：反复读直到没有差异，返回用时，超时失败并带上最后的差异。`within: 0` 就是「现在一致」；
- `differences(shown:truth:deferrable:)`：逐项比，`deferrable` 里的字段不算。

测试：`ScreenTruthTests`（`docs/testing.md`「真实性与及时性」）。
