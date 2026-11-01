# 锁屏 / 灵动岛实时活动（Fleet）

整台手机只有一个实时活动「Fleet」：把所有需要你、在工作、做完未看的 Agent 汇总到锁屏和灵动岛。hub 算内容、经 APNs 推送；
App 负责开始活动、上报 token；Widget 扩展（`apple/JarvisWidgets`）负责画。设计依据见车道笔记（ChatGPT Codex / Sand 的活动分析），
这里只写实现和约定。

## 显示什么

| 阶段 `phase` | 条件 | 颜色 | 符号 |
| --- | --- | --- | --- |
| `needsYou` | 有待处理交互（提问 / 计划 / 授权 / 终端），或 herdr blocked | 红 | 气泡 + 数量 |
| `working` | 没有需要你，有工作中 | 琥珀 | 齿轮 + 数量 |
| `done` | 只剩做完未看的 | 绿 | 圆点 + 数量 |

### 灵动岛（2026-10-29 改版）

以前紧凑态是「左边一颗星、右边一个计时器」，看不出在说什么：计时器只对单个 Agent 有意义，Fleet 汇总的是一群。现在两侧都是一眼能读的数：

```
预设 A（默认）  ( ⚙3    ▬▬▬▬   ●1 )     左边最急的一类，右边下一类；需要你时 ( 💬2  ▬▬▬▬  ⚙3 )
预设 B          ( ⚙3    ▬▬▬▬   ⚡12k )   右边：所有 Agent 每分钟输出的 token（TPM）
预设 C          ( ⚙3    ▬▬▬▬   📊42M )   右边：今天用掉的 token
自定义          左右各选：状态数量 / 每分钟 token / 今日 token / 今日请求数 / Jarvis 助手 / 机器负载 / 最久那一轮的计时 / 不显示
```

- 「需要你」永远占左侧（盖过自定义的选择），红色；右侧选了「状态数量」时显示左侧之后的下一类。规则在 `IslandCell.sides`（`apple/Shared/FleetState.swift`），单元测试钉住。
- 和别的 App 的活动并存时（最小态、限宽的紧凑态）：只显示最急的那一类，见下一节。
- 展开态：左上是三类数量，右上是选了的数字（如「12k/分」），下面照旧是 Agent 列表和按钮。锁屏头部同样：数量 + 选了的数字；选了两个数字时数量只留符号和数字，免得挤成「3 在…」。
- 齿轮不转：实时活动里 `symbolEffect(.rotate, options: .repeating)` 不动（隔 0.4 秒拍两张，齿轮区域像素差 ≤1，和静止区域一样），所以用静态齿轮；脉冲之类的重复动画同理。
- 不显示花费（用户决定，2026-10-29）：订阅用户看「等价 API 花费」会误导，只做 token 类指标。
- 设置：设置 › 通知 › 实时活动 › 灵动岛（`IslandSettingsView`），顶部是「平时 / 有 Agent 需要你时 / 和其他 App 共用时」三张预览；存在 hub 上按设备分开（`prefs.island`，`notify.go IslandPrefs`）。
  选「机器负载」时可以选看哪台，不选就是最忙的那台。

### 和其他 App 共用灵动岛（2026-11-02）

用户的截图：另一个 App 也有实时活动（右边单独一个圆「8分」），Jarvis 只剩左边一截「⚙4📊⁚」，后半截被截掉。

**规则**（iOS 27；Apple 的 Displaying live data with Live Activities 和 HIG Live Activities 2026-10 更新，模拟器实测）

- 只有一个活动：紧凑态，摄像头两侧各一块。系统不给宽度，内容多宽岛就多宽：iPhone 17 / 17 Pro 每侧最多约 94pt，
  iPhone 18 Pro / Pro Max 的摄像头窄（岛本体约 95pt，17 是 125pt），每侧 100pt 也放得下；再宽就截掉。
- 几个 App 都有活动：系统挑两个（有的机型三个）上岛，谁贴着岛、谁单独一圈由系统定，App 管不了。
  - iPhone 18 Pro / Pro Max：两个都用**紧凑态，但限宽**：每侧 28×28pt，左右两块并排在一处（贴岛的在摄像头左边，另一个单独一个胶囊）。
    用户的截图就是这样：「⚙4」和「📊42M」两块挤在 56pt 里，第二块被截。
  - 其他有灵动岛的机型（17、17 Pro、16 Pro…）：两个都用**最小态**，一个贴着岛（左），一个单独的圆（右）。宽 36.67–45pt，
    内容最多显示 35pt，多的截掉；系统同样不给宽度。
  - 横屏、iPhone Duo（岛竖着，两块上下叠）：只有自己一个活动也是限宽的紧凑态，每侧 28pt。
- 同一个 App 的几个活动：只有 `relevanceScore` 最高的上岛（Fleet 只有一个，不涉及）。

**App 能知道什么**

- 能：iOS 27 的 `isDynamicIslandLimitedInWidth`（WidgetKit 的环境值）。扩展画某一块时知道它被限宽了（上面三种限宽的情况实测都是 true）。
  Apple 的建议：限宽时 leading 放最小态那样的一个图形，trailing 给空视图，系统会把空的那一块藏起来。
- 不能：当前是哪种呈现（紧凑 / 最小 / 展开由系统分别渲染，视图只知道自己是哪一块）、有没有别的 App 的活动、自己是贴着岛还是单独一圈。
  App 进程里更拿不到；`relevanceScore` 只排自家的活动。
- 所以每种呈现都要自己就能看懂，不能指望另一半。

**做法**（`IslandGlyph`、`IslandSide`，`apple/LiveActivity/FleetIslandViews.swift`、`apple/JarvisWidgets/FleetLiveActivity.swift`）

- 最小态和限宽时的 leading 是同一个图形：最急的那一类（需要你 > 在工作 > 做完未看），完整显示，不受「样式」设置影响；限宽时 trailing 为空，系统把它藏掉。
  非限宽的紧凑态照旧按设置显示两侧。

```
            1 位        2 位         3 位及以上
最小态      ⚙3          ⚙12          99+（状态色）      内容 ≤ 35pt
限宽 28pt   ⚙3          (12) 状态色圈  99+（状态色）      在 28pt 的圆里
```

- 按数量选样子，不用 `ViewThatFits`：非限宽时系统不给宽度（视图拿到的是「要多宽给多宽」），`ViewThatFits` 永远选第一个。
- 尺寸都在 `IslandMetrics`（`apple/Shared/Design/IslandMetrics.swift`，扩展也编译）：限宽 28、图形可用 24（圆里一行字的宽度）、
  最小态 45 / 内容 35、紧凑一侧最多约 94，以及符号和数字的字号。
- 设置页加了「和其他 App 共用时」的预览（`IslandSharedPreview`）：贴着摄像头的是 Jarvis 的图形，右边灰色的圆代表别的 App。

**实测**（iOS 27 模拟器，每台各拍单独 / 并存 / 横屏）

| 机型 | 单独（紧凑态每侧） | 和别的 App 并存 | 横屏 |
| --- | --- | --- | --- |
| iPhone 18 Pro / Pro Max | 随内容变宽，100pt 也放得下 | 紧凑态限宽，每侧 28×28 | 限宽 28 |
| iPhone 17 / 17 Pro | 随内容变宽，最多约 94pt | 最小态，内容最多 35pt | 限宽 28 |
| iPhone Duo（iOS 27.1） | — | 限宽 28，两块上下叠 | — |

**验证**：`IslandFitTests` 把最小态、限宽、紧凑一侧在 1 / 2 / 3 / 4 位数时按各自的尺寸画出来，检查画出的像素都在给的范围里
（字不缩、不省略，放不下就会溢出被查到）。模拟器上用另一个 App 的活动复现：`scripts/island_peer.sh <模拟器>` 装一个只有实时活动的
示例 App（`apple/IslandPeer`，「8分」），`LiveActivityShots` 带 `JARVIS_ISLAND_PEER=dev.example.islandpeer` 时多拍「并存」和「横屏」两张，
`JARVIS_FLEET_COUNT=12` 把编造的数量改成两位数。

- 不算进来：已看（idle）、离线、屏蔽的 Agent、`/loop` 等下次唤醒的；助手（对话 tab）只有在问你时才算。规则和 Agents 列表一致（`hub/liveactivity.go buildFleet`）。
- 条目顺序：需要你（最早的请求在前）→ 工作中（跑得最久的在前）→ 做完未看（最新的在前）；推送里最多 3 条，界面画 2 条，其余「+N 个」。
- 计时在手机本地走（`Text(timerInterval:)`），起点是 hub 记的这一轮开始时间（`turnTracker`），不为计时推送。
- 锁屏默认只显示数量和「有个授权等你」这类不带名字的一行；设置 › 通知 › 锁屏显示详情 打开后才显示 Agent 名字和内容（按设备存，`prefs.lock_details`，
  hub 推给每台设备时填进 `details`）。灵动岛只在解锁时可见，总是显示详情。
- 视觉：Jarvis 的状态色（不跟随界面风格，扩展读不到）；描边和「允许 / 批准」按钮是系统蓝，和 App 的主色一致。锁屏和展开态都限高约 160pt，所以最多两条。
  左边是 Jarvis 的形象（见下面「形象」），2026-11-02 以前是 App 图标的珊瑚四角星。

### 形象（2026-11-02 用户要求：「像 Codex 那样」）

锁屏横幅左上、灵动岛紧凑态左侧、展开态左上角，画的是设置 › 外观 › 形象里选的那个形象，随状态变：

```
紧凑态  ( 🙂2    ▬▬▬▬   ⚙3 )     左：形象 + 最急那一类的数量（只剩数字，形象已经说了是哪类）；右：照旧
展开态  🙂             💬2 ⚙3 ●1    左上：形象；右上：三类数量，下面一行是选了的数字
最小态 / 限宽  ⚙3                   不放形象：和别的 App 共用时只有 28–35pt，一张脸会把数量挤掉，照旧是最急那一类（上一节）
```

| Fleet | 形象的状态 | 样子 |
| --- | --- | --- |
| 有需要你 | 等你处理 | 红色的圈 |
| 在工作 | 工作中 | 眼睛看向一边（面罩）/ 看向走线（汇流） |
| 只剩做完未看，或没有 | 空闲 | 睁眼 |
| 没有 Agent 在忙、只有助手在回答（`assistant`） | 思考中 | 抬眼、思考气泡 |
| 内容过期（hub 15 分钟没推，`isStale`） | 离线 | 灰色、闭眼 |

- 「出错」：Fleet 没有单独的出错状态，最接近的是 hub 断了导致内容过期，所以过期用离线（灰）。
- 「思考中」：Fleet 分不出 Agent 在思考还是在写，只有助手在回答时才用；`assistant` 只推给灵动岛显示助手的设备，别的设备看到的是空闲。
- **不动，只在状态变时淡入淡出**：实时活动里 TimelineView、重复的 symbol 动画都不跑（见「坑」），所以画的是每个状态的静止帧（和「减弱动态效果」时 App 里的一样，`PersonaFace(moving: false)`），状态变时 `.transition(.opacity)`。不用位图序列帧，不为动画加推送。
- **画法共用**：面罩、汇流的绘制在 `apple/Shared/Design/PersonaFace.swift`，App 的 `JarvisAvatar` 在外面套 TimelineView 让它动，扩展直接画静止帧；扩展只多编译这一个文件。
- **选的哪个、什么颜色**：扩展读不到 App 的设置，App 在形象 / 界面风格 / 主题色变化时把 `FleetLook`（形象的画法 + 主色的浅色 / 深色值）写进和扩展共享的钥匙串组
  `<bundle id>.look`（和推送中继共享 `.push` 的做法一样），扩展每次画的时候读；写了以后立刻用当前内容 update 一次活动，不等下一次推送。
  没写过或读不到（免费个人团队签名没有共享组）时画默认形象 + 系统蓝。
- **不放进 ContentState**：hub 的推送整个替换 content-state，hub 也不知道手机选了哪个形象；所以 ContentState 没变，旧版本的活动照常解码。
- hub 上的形象包（位图）：活动不播位图，画成默认形象（面罩），颜色用这个形象的主色（`Theme.persona`）。
- 设置 › 通知 › 灵动岛的预览（`IslandPreview`）左侧也是形象，和紧凑态一致。
- Apple Watch：不单独开发，`supplementalActivityFamilies([.small])` 给智能叠放一个单行版本（最急那一类的符号 + 「2 个需要你」+ 下一类的数量）。

## ContentState（`FleetState`）

Go `hub.FleetState` 和 Swift `FleetState`（`apple/Shared/FleetState.swift`）逐字段对应，`liveactivity_test.go TestFleetStateJSON` 和
`FleetStateTests` 两边钉住同一段 JSON：

```json
{"phase":"working","needsYou":0,"working":1,"unread":0,
 "items":[{"id":"<machine>/<session>","title":"…","place":"机器 · 空间","kind":"working","detail":"","since":1761700000}],
 "workingSince":1761700000,"details":false}
```

- **时间一律是 Unix 秒（数字）**：ActivityKit 用默认的 JSONDecoder 解推送里的 `content-state`，`Date` 会被当成 2001 年起的秒数。
- Swift 侧每个字段都宽松解码（未知的 phase / kind 不报错），hub 加字段不会让活动坏掉。
- `attributes-type` 是 `JarvisActivityAttributes`，`attributes` 是 `{"kind":"fleet"}`（push-to-start 用），两个名字都不能改。
- 灵动岛的字段：`left` / `right`（这台设备的两侧，`status|tpm|tokens|requests|assistant|load|timer|none`）、`assistant`（助手在回答）、
  `stats`（`{tpm, tokens, requests, loads:[{id, name, percent}]}`）。hub 推给每台设备时只填它显示的那几项（`FleetState.forDevice`），
  这样它不显示的东西变了不会占推送；App 自己收到的 `fleet` 事件带全部数字，按自己的设置裁（`FleetState.tailored`）。

## 推送（`hub/liveactivity.go`）

- 触发：hub 每次广播 `sessions` / `interaction` / `interaction_resolved` 时重算快照，和上次推的一样就不推。
- 合并：新的「需要你」立刻推（priority 10）；其他变化等 2.5 秒合并后推（priority 5）。不带 alert、不带 category（交互推送已经响过）。
- `stale-date` = 现在 + 15 分钟，内容没变也每 10 分钟重推一次续期；hub 断了活动会变「过期」，界面显示「等待 hub 更新…」（P3 的按钮也会隐藏）。
- 结束：没有可显示的超过 1 分钟，或只剩做完未看的超过 5 分钟，推 `end`（`dismissal-date` = 现在）并忘掉活动 token。
- 开始：有活动 token 就 `update`；没有（App 没开过、上一个结束了）而有 push-to-start token，就在「一轮」开始时推一次 `start`
  （从空 / 只剩做完的变成有需要你或工作中算新的一轮）。用户划掉活动后，这一轮不会再推开。活动超过 8 小时（系统上限）当作已经没了，下一轮重新开始。
- APNs：`apns-push-type: liveactivity`、`apns-topic: <bundle>.push-type.liveactivity`，目标是活动自己的 token（或 push-to-start token），
  环境跟设备注册时一样（Debug 构建是 sandbox）。410 / BadDeviceToken 时删掉那个 token。
- 每台设备一行 `live_activities`（`device_id`、`activity_id`、`push_token`、`push_to_start_token`、`started_at`），内存里有一份，每次变化写库。
- 开着 App 时 hub 还广播 `{"type":"fleet","fleet":{…}}`，App 自己更新活动（比推送快，模拟器上也能用）。

## 用量（token / TPM / 负载）

- 数据源是 transcript：node（`node/usage.go`）每 10 秒扫一次本机所有 transcript 新增的部分——Claude Code 各配置目录下的主会话和子 Agent 文件
  （assistant 行的 `message.usage`，同一条回复的每个内容块重复同一份 usage，按 message id 去重；`<synthetic>` 不算），
  Codex / TRAEX 的 rollout（`token_count` 事件的 `last_token_usage`，总量没涨的重复事件跳过）。按「分钟 × 模型」累计最近 26 小时。
- 只读新增的字节：每个文件记偏移，没写完的半行留到下次；第一次见到的大文件从尾部往前找到 26 小时前的位置再读，
  超过 16 MB 的单行（图片）跳过。实测 hub 机器一天的 transcript 第一次扫 85 ms，之后每次约 1 ms。
- node 把变了的分钟报给 hub（`usage` 事件，每个分钟报全量值，重复报只是覆盖）；连上后第一次带 `full: true` 发整份。
  不落库：node 跟 hub 连接同生共死，重启任一边都会从文件重算出同样的数。
- hub（`hub/usage.go`）汇总所有机器：TPM = 最近 2–3 分钟（从两分钟前那一分钟的开头到现在）的输出 token ÷ 分钟数；今日 token 含缓存读写，
  「今天」按手机的时区（助手设置里报的）算；负载 = 1 分钟 load average ÷ 核数，2 分钟没报的机器不算负载。
- 推给灵动岛前取两位有效数字（12345 → 12000，负载取整到 5%），数字小幅波动不会变成新的内容。

## 推送预算

- 状态（需要你 / 在工作 / 做完、条目、按钮）的变化照旧：新的「需要你」立刻推（priority 10），其他合并 2.5 秒（priority 5）。
- 只有数字变了：hub 每 15 秒看一次，对每台有活动的设备，距它上次收到推送超过 `notify.live_metrics_seconds`（默认 60 秒）、
  而且它显示的（取整后的）数字确实变了，才推一次 priority 5 的 update。状态推送也会顺带最新的数字。
- 每台设备记着上次推给它的 content-state，一样的就不推（比如助手开始回答，但这台设备的灵动岛不显示助手）。
- App 在前台时自己用 `fleet` 事件更新活动（不占推送预算），数字跟着每次变化走。

## App（`LiveActivityController`）

- 启动就监听 `pushToStartTokenUpdates`、`activityUpdates`（hub 推开的活动），每个活动监听 `pushTokenUpdates`（会轮换）和结束 / 划掉，
  都上报 `POST /api/v1/devices/{id}/live-activity`。
- 前台收到 `fleet` 事件或回到前台拉 `GET /api/v1/fleet`：有内容就开始（只有前台能 `Activity.request`）或更新；空了等 1 分钟再结束（轮次之间会闪）。
- 点活动本身（紧凑态、锁屏）：只有一个 Agent 时打开它（`jarvis://open?machine=…&session=…`），有好几个时只是回到 App（`jarvis://open`），停在离开时的页面和位置（列表就是列表，对话页还是那个对话、同一行；App 被系统结束过的话，冷启动恢复上次的对话页，进后台时记下的阅读位置）；展开态每一行打开各自的会话。2026-10-29 以前一律打开第一条，用户反馈「点灵动岛进了某个 Agent」。
- 模拟器自动化（`-pairURL`）不上报 token，免得 hub 把真机的活动当成模拟器的。

## 按钮（`apple/LiveActivity/FleetIntents.swift`）

- 第一条能用按钮处理的请求（授权、计划）带按钮：授权「拒绝 / 允许」，计划「查看（打开会话）/ 批准」。打回计划要写意见，留给 App。
  锁屏和展开态都只放这一条 + 按钮 + 「+N 个」（高度放不下第二条）。
- 「允许」「批准」`authenticationPolicy = .requiresAuthentication`（锁屏要先 Face ID），「拒绝」`.alwaysAllowed`，和通知按钮一样。
  计划从锁屏批准用 `acceptEdits`（没看全计划，命令仍要逐个授权）。
- `LiveActivityIntent` 的 `perform` 在 App 进程里跑（需要时系统在后台拉起 App），所以扩展里只编译类型、`perform` 是空的
  （`WIDGET_EXTENSION` 编译条件）；App 里的实现在 `FleetActions`：调 resolve、立刻把这一条从活动上拿掉（乐观更新，hub 的推送随后到），
  失败发一条本地通知说明原因（410「已经处理过了」时照样拿掉）。
- 内容过期（`isStale`，hub 15 分钟没推）时不画按钮，免得对已经被处理的请求点「允许」。

## 设置（设置 › 通知 › 实时活动）

- 「锁屏和灵动岛」（`prefs.live_activity`，默认开）：关掉时 App 立刻结束活动，hub 不再推这台设备。
- 「锁屏显示详情」（`prefs.lock_details`，默认关）：改了以后 App 立刻改正在显示的活动，hub 下一次推送也按新的值填 `details`。
- 两项都存在 hub 上按设备分开（`PUT /api/v1/devices/{id}/prefs`），App 在 UserDefaults 里留一份（`LiveActivityController.settings`），
  每次回到前台从 hub 同步。系统设置里关掉了 Jarvis 的实时活动时，这一节提示并给「打开系统设置」。
- 断开 hub（忘记服务器）时结束活动：它的按钮连的是旧 hub。
- 没做「盯着一个 Agent」的单独活动：Fleet 已经包含每个在跑的 Agent，再开第二个活动会在锁屏上堆两条；有需要再加 `kind: agent`。

## 验证

- 布局：`-showFleetSample needsYou|working|done|thinking|offline`（`thinking` 只有助手在回答，`offline` 是开出来就标成过期的工作中；`-fleetDetails YES` 看锁屏详情，`-fleetIsland b|c|custom:assistant,load` 选灵动岛两侧，`-openPersona jarvis-circuit` 换形象）用编造的数据开一个活动，不需要 hub。
  UI 测试 `LiveActivityShots` 读 `JARVIS_FLEET_ISLAND`、`JARVIS_PERSONA`，紧凑态隔 0.4 秒拍两张（`compact` / `compact2`），用来比对有没有动画，3 秒后再拍一张（`compact3`，刚回桌面时岛还在动画里，可能是空的）；
  `JARVIS_ISLAND_PEER`（另一个 App 的活动并存、横屏）和 `JARVIS_FLEET_COUNT`（`-showFleetCount`，数量改成几位数）见上面「和其他 App 共用灵动岛」。
  模拟器上过期的活动只在锁屏上画（灰色形象 + 「等待 hub 更新…」），灵动岛上不显示。
  设置页：`-tab settings -openNotificationSettings YES -openIslandSettings YES`。
  `simctl io screenshot` 拍不到灵动岛和锁屏，用 UI 测试 `LiveActivityShots` 拍紧凑 / 展开 / 锁屏三张（`docs/testing.md`）。
- 推送：`xcrun simctl push` 不支持实时活动；但模拟器能收真实的 APNs sandbox 推送。DEBUG 构建在系统日志里打印活动的 push token
  （`subsystem == "jarvis"`），在 hub 机器上 `JARVIS_LA_TOKEN=<token> JARVIS_LA_TOPIC=<bundle id> go test ./internal/hub -run TestManualLiveActivityPush`
  用 hub 的 APNs key 推一条 update，再用 `JARVIS_FLEET_SAMPLE=current` 拍下来（2026-10-29 实测通过：Go 生成的 JSON → APNs → 模拟器活动正确显示）。
- 按钮：UI 测试 `LiveActivityActionTests` 连 `mock_hub.py`，锁屏点「拒绝」，再读 `GET /mock/resolved` 确认 hub 收到 `allow:false`（2026-10-29 通过）。
- 真机才能验证的：push-to-start（App 被杀后由 hub 开活动）、更新预算、手表智能叠放。

## 坑

- 示例里先 `endAll()` 再 `request` 会撞上：正在结束的活动仍然报 `.active`，新的就没开出来。直接用 `apply`（有就更新）。
- 锁屏 / 展开态超过约 160pt 会被裁掉（顶部或底部），而且不报错：加内容前先拍图看。
- `Text(timerInterval:)` 会占满可用宽度，要给固定宽度，否则把旁边的文字挤成「3…」。
- 灵动岛的紧凑态和最小态不给宽度（`GeometryReader` 量到的是它的默认大小），内容多宽岛就多宽，到上限才截；只有限宽时给 28pt。
  所以 `ViewThatFits` 在这里没用，按内容自己选样子，用 `IslandFitTests` 量。
- 重复的 symbol 动画（`.rotate`、`.pulse` 加 `.repeating`）在实时活动里不动，App 里的预览却会动：两边都用静态符号，免得预览骗人。
- `sips -c 高 宽 --cropOffset 0 0` 会被当成居中裁切（偏移 0 等于没给）；从顶部裁要用 `--cropOffset 1 0` 之类。
- UI 测试把模拟器锁屏后要解锁，否则下一次 `xcodebuild test` 报「test runner hung before establishing connection」。
