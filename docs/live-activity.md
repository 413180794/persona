# 锁屏 / 灵动岛实时活动（Fleet）

整台手机只有一个实时活动「Fleet」：把所有需要你、在工作、做完未看的 Agent 汇总到锁屏和灵动岛。连电脑的管理者算内容（`server/internal/assistant/fleet_live.go`，经总线 `fleet.live` 给 hub），hub 给每台设备填它那几项、按节奏经 APNs 推送；
App 负责开始活动、上报 token；Widget 扩展（源码 `apple/PersonaWidgets`、module 仍为 `JarvisWidgets`）负责画。设计依据见车道笔记（ChatGPT Codex / Sand 的活动分析），
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

用户的截图：另一个 App 也有实时活动（右边单独一个圆「8分」），Persona 只剩左边一截「⚙4📊⁚」，后半截被截掉。

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

**做法**（`IslandGlyph`、`IslandSide`，`apple/LiveActivity/FleetIslandViews.swift`、`apple/PersonaWidgets/FleetLiveActivity.swift`）

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
- 设置页加了「和其他 App 共用时」的预览（`IslandSharedPreview`）：贴着摄像头的是 Persona 的图形，右边灰色的圆代表别的 App。

**实测**（iOS 27 模拟器，每台各拍单独 / 并存 / 横屏）

| 机型 | 单独（紧凑态每侧） | 和别的 App 并存 | 横屏 |
| --- | --- | --- | --- |
| iPhone 18 Pro / Pro Max | 随内容变宽，100pt 也放得下 | 紧凑态限宽，每侧 28×28 | 限宽 28 |
| iPhone 17 / 17 Pro | 随内容变宽，最多约 94pt | 最小态，内容最多 35pt | 限宽 28 |
| iPhone Duo（iOS 27.1） | — | 限宽 28，两块上下叠 | — |

**验证**：`IslandFitTests` 把最小态、限宽、紧凑一侧在 1 / 2 / 3 / 4 位数时按各自的尺寸画出来，检查画出的像素都在给的范围里
（字不缩、不省略，放不下就会溢出被查到）。模拟器上用另一个 App 的活动复现：`scripts/app/island_peer.sh <模拟器>` 装一个只有实时活动的
示例 App（`apple/IslandPeer`，「8分」），`LiveActivityShots` 带 `JARVIS_ISLAND_PEER=dev.example.islandpeer` 时多拍「并存」和「横屏」两张，
`JARVIS_FLEET_COUNT=12` 把编造的数量改成两位数。

- 不算进来：已看（idle）、离线、屏蔽的 Agent、`/loop` 等下次唤醒的；助手（对话 tab）只有在问你时才算。规则和 Agents 列表一致（`hub/liveactivity.go buildFleet`）。
- 条目顺序：需要你（最早的请求在前）→ 工作中（跑得最久的在前）→ 做完未看（最新的在前）；推送里最多 3 条，界面画 2 条，其余「+N 个」。
- 计时在手机本地走（`Text(timerInterval:)`），起点是 hub 记的这一轮开始时间（`turnTracker`），不为计时推送。
- 锁屏默认只显示数量和「有个授权等你」这类不带名字的一行；设置 › 通知 › 锁屏显示详情 打开后才显示 Agent 名字和内容（按设备存，`prefs.lock_details`，
  hub 推给每台设备时填进 `details`）。灵动岛只在解锁时可见，总是显示详情。
- 视觉：Persona 的状态色（不跟随界面风格，扩展读不到）；描边和「允许 / 批准」按钮是系统蓝，和 App 的主色一致。锁屏和展开态都限高约 160pt，所以最多两条。
  左边是 Persona 的形象（见下面「形象」），2026-11-02 以前是 App 图标的珊瑚四角星。

### 形象（2026-11-02 用户要求：「像 Codex 那样」）

锁屏横幅左上、灵动岛紧凑态左侧、展开态左上角，画的是设置 › 外观 › 形象里选的那个形象，随状态变：

```
紧凑态  ( 🙂2    ▬▬▬▬   ⚙3 )     左：形象 + 最急那一类的数量（只剩数字，形象已经说了是哪类）；右：照旧
展开态  🙂             💬2 ⚙3 ●1    左上：形象；右上：三类数量，下面一行是选了的数字
最小态 / 限宽  ⚙3                   和别的 App 共用时只有 28–35pt，一张脸会把数量挤掉：有数量时照旧是最急那一类（上一节）；
                                    没有可数的（只有助手在回答）时放形象，不留一个空岛（`IslandMinimal`）
```

| Fleet | 形象的状态 | 样子 |
| --- | --- | --- |
| 有需要你 | 等你处理 | 红色的圈 |
| 在工作 | 工作中 | 眼睛看向一边、橙色弧线停在圆边上（团子）/ 眼睛看向一边（面罩）/ 看向走线（汇流） |
| 只剩做完未看，或没有 | 空闲 | 睁眼 |
| 没有 Agent 在忙、只有助手在回答（`assistant`） | 思考中 | 抬眼、思考气泡 |
| 内容过期（hub 15 分钟没推，`isStale`） | 离线 | 灰色、闭眼 |

- 「出错」：Fleet 没有单独的出错状态，最接近的是 hub 断了导致内容过期，所以过期用离线（灰）。
- 「思考中」：Fleet 分不出 Agent 在思考还是在写，只有助手在回答时才用；`assistant` 只推给灵动岛显示助手的设备，别的设备看到的是空闲。
- **不动，只在状态变时淡入淡出**：实时活动里 TimelineView、重复的 symbol 动画都不跑（见「坑」），所以画的是每个状态的静止帧（和「减弱动态效果」时 App 里的一样，`PersonaFace(moving: false)`），状态变时 `.transition(.opacity)`。不用位图序列帧，不为动画加推送。
- **画法共用**：团子、面罩、汇流的绘制在 `apple/Shared/Design/PersonaFace.swift`，App 的 `AssistantAvatar` 在外面套 TimelineView 让它动，扩展直接画静止帧；扩展只多编译这一个文件。
- **选的哪个、什么颜色**：扩展读不到 App 的设置。`FleetLook` 是形象的画法 + 主色的浅色 / 深色值，形象包再加 `<id>/<rev>`。
  它放在实时活动的内容里（`FleetState.look`，build 99 之后）：
  - App 自己更新活动时带上它；
  - App 把它告诉 hub（设备 prefs 的 `look`，单独 PUT），hub 原样放进这台设备的推送；
  - 换了形象，App 立刻用带新形象的内容 update 一次活动，内容变了系统一定重画。

  扩展优先画内容里的形象；内容里没有（旧 hub）才读 App 存下的。App 把形象和形象包的图存进和扩展共享的 App Group 容器（`FleetLookStore`，`Fleet/` 下的文件，
  组名默认 `group.<bundle id>`，见 `docs/release.md`「App Group」），形象包的图只能走这条路，内容里放不下（整个活动的数据 4 KB）。
  不用钥匙串：实时活动的扩展在手机上读写不了钥匙串（build 103 实测，见下面「为什么 build 103 的形象包还不对」）。
  写了以后立刻 update 一次活动，不等下一次推送：内容不变，过期时间往后挪 1 秒，免得和正在显示的一模一样被系统当成没变（模拟器实测 3 秒内换过来，不用重开活动）。
  App 每次回到前台也比一次，之前没写进去的这时再写。
  没写过或读不到时画内容里的形象，形象包画默认面罩换成它的颜色；签名里没有 App Group（免费个人团队、漏了 entitlements）就是这样。
- **形象包（hub 的 `~/.persona/personas`，位图）**：App 从 hub 下载了形象包以后，每个状态取一张图，用点状动画的第一帧（「减弱动态效果」时顶栏那个点停住的样子，包里是 144px），
  按活动里每种形态的脸的大小各出一张 @3x：紧凑态 22pt（66px）、最小态 / 限宽 24pt（72px）、展开态和锁屏 26pt（78px），从包里的原图直接缩，用高质量插值，PNG 无损。
  不再为字节数降像素（#349：4 KB 的上限是钥匙串时期定的，那时 6 张图挤在一个条目里；build 106 上 36–42px 的图按 26pt 画「还是有点糊」）。头像只在有状态没有帧时才存，用不小于 78px 的那张头像文件缩。
  每张一个文件：`Fleet/faces/<id>_<rev>_3x/<状态>_<pt>.png`、`avatar_<pt>.png`（目录名的 `_3x`：旧版本存的 4 KB 小图会重做），只留当前这个包，换包时整个目录换掉（先写在旁边再挪进来，扩展不会读到两个包各一半）。
  扩展按当前形态的大小只读那一张（`FleetLookStore.face`），按它的大小、3 倍解码（`FleetFaces.image`），裁成圆。为什么要这样，见下面「为什么 build 98 还不对」。
  包里没有某个状态的帧时用头像，再按内置形象的样子加提示：需要你加红圈，离线变灰。图片只在手机上（App 下载的包里和 App Group 容器里），不进仓库，也不进安装包；测试用纯色占位图。
  读不到图（还没写、写失败）时画面罩（团子不跟颜色，面罩能换成这个形象包的主色），颜色用这个形象包的主色，不画别的包的图（目录名就是包的 `<id>/<rev>`）。
- **放进 ContentState**（build 98 之前没放：当时以为钥匙串就够了，而 hub 的推送会整个替换 content-state、hub 又不知道手机选了哪个形象）：
  所以要 App 把形象告诉 hub。没有 `look` 的内容（旧 hub）照常解码，扩展退回读 App 存下的 `look.json`；读不懂的 `look`（以后的格式）只丢掉这一项。
- 设置 › 通知 › 灵动岛的预览（`IslandPreview`）左侧也是形象，读的是扩展读的同一份，和紧凑态一致。

#### 为什么 build 92 不跟随（2026-11-02）

用户反馈「灵动岛的形象没有跟随设置」，截图是只有 Persona 一个活动的紧凑态（右边的「1.7B」是 Persona 自己的「今天的 token」，不是别的 App），左边是默认形象 + 系统蓝，也就是扩展读不到钥匙串时的样子。

- **根因**：发 TestFlight 的流程先无签名归档（`CODE_SIGNING_ALLOWED=NO`），再 ad hoc 签名、导出时云签名。无签名时 `$(AppIdentifierPrefix)` 是空的，
  ad hoc 签名又没给扩展带 entitlements：装到手机上的小组件扩展没有任何钥匙串组，App 的组名少了 bundle id，Info.plist 里的组名少了团队前缀。
  App 写不进共享组（报 missing entitlement，当成免费团队静默放弃），扩展永远画默认形象。推送中继的 `.push` 组同样坏了（通知扩展解不开加密推送，只显示兜底文字）。
- **另一半**：#219 有意把 hub 的形象包画成默认形象（换个颜色），所以就算钥匙串通了，选了形象包也看不到它。
- **修法**：Info.plist 的组名改成 `$(DEVELOPMENT_TEAM).<bundle id>.…`（无签名也有值）；`scripts/release/entitlements.py sign` 给无签名归档里的每个 bundle 带上
  它自己的 entitlements（展开构建变量，有展不开的就报错）再 ad hoc 签名；`scripts/release/entitlements.py check` 查 ipa / 归档里每个 bundle 的钥匙串组和 Info.plist 里的组名，
  `release.sh` 归档后跑，发 TestFlight 的流程导出后也要跑（对 build 92 的 ipa 报出 9 处）。形象包画它自己的图（上一条）。
- Apple Watch：不单独开发，`supplementalActivityFamilies([.small])` 给智能叠放一个单行版本（最急那一类的符号 + 「2 个需要你」+ 下一类的数量）。

#### 为什么 build 98 还不对（2026-11-02）

build 98 的签名对了：App 和小组件扩展的钥匙串组、Info.plist 里的组名都对，`entitlements.py check` 也通过。但用户截图还是默认形象 + 系统蓝，
换成内置的「汇流」也一样。同一份代码在模拟器上用 hub 上真实的形象包（拷到本机、不进仓库）跑，岛上画的是形象包的图，所以只有手机上会这样。

已经排除的：
- 签名和组名：上面说的 ipa 检查。
- 数据保护：存的是 `AfterFirstUnlock`，开机解锁过一次以后锁屏也能读。

还剩两种解释，在没有真机的情况下分不开：
- **扩展在实时活动里读不到钥匙串**。Apple 只说实时活动「跑在自己的沙盒里」，不能联网、不能定位，没提钥匙串。如果是这样，岛上任何形象都画不出来，内置的也一样。
- **重画没发生**。98 换形象后用一模一样的内容 update 活动，系统可能当成没变；hub 又只在内容变了时推（#248 以后最多 30 秒一次）。
  这样岛上一直留着写进形象之前画的样子。

修法（build 99 之后）：
- **形象放进实时活动的内容**（`FleetState.look`），两种解释都绕开了：
  - 画内置形象和形象包的颜色不再靠钥匙串；
  - 换形象时内容变了，系统一定重画；
  - App 在后台时，hub 的推送里也带着这台手机的形象。
- **形象包的图**仍然放钥匙串，内容里放不下（整个活动的数据 4 KB）。build 103 的诊断证明扩展读不到钥匙串，于是改放 App Group 容器（下一节）。
- **图的大小**：形象包的图原来是 78px 的 PNG，`UIImage(data:)` 按 1× 解码，也就是 78pt，比紧凑态、最小态的整个呈现区域都大。
  Apple 的说法是实时活动里的图不能大于它所在的呈现，否则系统可能不显示这个活动；几个实时活动的第三方库还把单张图限在 4 KB 以内（Apple 没写，库是踩出来的），
  而真实形象包的帧在 iOS 上缩到 78px 是 11–15 KB、42px 还有 4.4–5.4 KB。当时按 26pt 解码，单张超过 4 KB 就少用一些像素（36–42px 左右放得下）。模拟器两种都照画。
  改走 App Group 文件以后（#346、#349）去掉了 4 KB，按每种形态的大小各存一张 @3x（22 / 24 / 26pt）；手机上能不能画出这个大小的图，以装上 #349 之后的构建实测为准。

上次（#227）的验证为什么没发现：
- 截图用的是 mock_hub 的占位形象包，图是纯色圆片，PNG 只有几百字节，碰不到任何大小限制。
- 只在模拟器上看了岛：模拟器的扩展读得到钥匙串，也不管实时活动的图片限制，和手机不一样的地方都看不出来。
- 签名检查只证明组名对，证明不了手机上的扩展真的读到了。当时也没有任何办法看到它读到了什么。

#### 为什么 build 103 的形象包还不对（2026-11-02）

用户反馈「为啥支持汇流，但是不支持 claude，GPT 的形象头像」：内置形象（换了主题色的面罩、汇流）都对了，说明内容里的形象这条路是通的；hub 形象包只画出默认面罩换成它的颜色。
诊断页（下一节）：设置、hub、实时活动内容里的形象三处一致；App 写进钥匙串的形象（88 字节）和形象包的图（21.8 KB）都成功、能读回；「扩展最近一次」是「没有」。

- **根因**：实时活动的扩展在手机上读写不了钥匙串。签名里有组、App 那边一切正常，扩展连自己的报告都写不进去；模拟器上同一个扩展读写都行。
  Apple 只说实时活动「跑在自己的沙盒里」、不能联网和定位，没提钥匙串；这是实测的结论，是数据保护钥匙串、访问组还是沙盒挡住的，从外面看不出来，诊断只能证明「读写都不行」。
- **21.8 KB**：不是单张图超了，是一个钥匙串条目里放了 6 张（5 个状态 + 头像），每张 3.6 KB 左右，都在 4 KB 以内。现在每张一个文件，扩展每次只读要画的那一张；
  5 个状态都有帧时不再存头像。
- **修法**：形象包的图、兜底的形象、扩展的报告都改成 App Group 容器里的文件（`FleetLookStore`），钥匙串那条路去掉；旧版本留在钥匙串里的两条，App 启动后删掉
  （App 的 entitlements 为此暂时留着 `.look` 组，扩展的去掉了）。中间考虑过构建时把图标打进安装包，因为不想让第三方角色的图进 TestFlight 的公开包而作罢。

#### 诊断

- **日志**：两边写 App Group 的每个文件、读失败的每次都记进系统日志（子系统 `jarvis`，类别 `island`），包括文件名、字节数、错误。扩展画的东西变了（或离上次记录过了 10 分钟）记一条
  「extension drew … container … saved … face …」：画的是内容里的哪个形象，有没有容器，读到了哪张图。在 Mac 的「控制台」里选中手机搜 `category:island`，也可以抓 sysdiagnose。
- **诊断页**：设置 › 通知 › 灵动岛，长按第一个预览。按形象传过去的顺序列出来：
  - 结论：第一步出问题的地方；
  - 形象：设置里的、hub 记下的、正在显示的实时活动内容里的；
  - App Group 的组名、容器路径（没有就是签名里没有这个组），这次启动写的 `look.json` 和形象包的图（成功或错误、字节数）；容器里 `Fleet/` 下的每个文件（大小、修改时间）；
  - 扩展最近一次画的形象、有没有容器、读到的存下的形象、读到的形象包的图（文件、字节数）。扩展把这些写进容器的 `seen.json`，变了才写，最多十分钟一次。
  - 按钮：「重新写入并重画」（容器里的文件、hub、活动的内容全部重来），「复制全部」（把全文发给开发者）。

  形象对了而「扩展最近一次」一直是「没有」，就是扩展写不了容器（岛上是形象包的图就说明还读得到）；App 那边「容器」是「没有」，就是签名里没有 App Group。
  DEBUG 构建可以直接打开它：`-openIslandDiagnostics YES -openIslandSettings YES -openNotificationSettings YES -tab settings`。

## ContentState（`FleetState`）

Go `hub.FleetState` 和 Swift `FleetState`（`apple/Shared/FleetState.swift`）逐字段对应，`liveactivity_test.go TestFleetStateJSON` 和
`FleetStateTests` 两边钉住同一段 JSON：

```json
{"phase":"working","needsYou":0,"working":1,"unread":0,
 "items":[{"id":"<machine>/<session>","title":"…","place":"机器 · 空间","kind":"working","detail":"","since":1761700000},
          // 这一轮停着、后台还在跑（Session.background_working，#2935）：background=true，detail 是在跑的东西（「parity-audit · Verify · 169/185」「后台 2 个任务在跑」）；旧 App 不认 background，照常当工作中
          {"id":"…","title":"…","place":"…","kind":"working","background":true,"detail":"parity-audit · Verify · 169/185","since":1761700000}],
 "workingSince":1761700000,"details":false}
```

- **时间一律是 Unix 秒（数字）**：ActivityKit 用默认的 JSONDecoder 解推送里的 `content-state`，`Date` 会被当成 2001 年起的秒数。
- 后台工作条目（`background: true`）的 `since` 取最早一个在跑且开始时间已知的 workflow 的 `started_at`（#3433），以免把已结束的主轮等待时间算成 workflow 耗时。没有这样的 workflow 时沿用轮次开始时间，再回退会话更新时间；前台轮次仍按自己的开始时间计时，`detail` 的一行文案不变。
- Swift 侧每个字段都宽松解码（未知的 phase / kind 不报错），hub 加字段不会让活动坏掉。
- `attributes-type` 是 `JarvisActivityAttributes`，`attributes` 是 `{"kind":"fleet"}`（push-to-start 用），两个名字都不能改。
- 灵动岛的字段：`left` / `right`（这台设备的两侧，`status|tpm|tokens|requests|assistant|load|timer|none`）、`assistant`（助手在回答）、
  `stats`（`{tpm, tokens, requests, loads:[{id, name, percent}]}`）。hub 推给每台设备时只填它显示的那几项（`FleetState.forDevice`），
  这样它不显示的东西变了不会占推送；App 自己收到的 `fleet` 事件带全部数字，按自己的设置裁（`FleetState.tailored`）。

## 推送（`hub/liveactivity.go`）

- 触发：hub 每次广播 `sessions` / `interaction` / `interaction_resolved` 时重算快照，和上次推的一样就不推。
- 节奏见下面「推送节奏」：「需要你」的变化立刻推（priority 10），其他变化合并 2.5 秒、每台设备最多 30 秒一次（priority 5），
  App 开着的设备不推。不带 alert、不带 category（交互推送已经响过）。
- `stale-date` = 现在 + 15 分钟，内容没变也每 10 分钟重推一次续期；hub 断了活动会变「过期」，界面显示「等待 hub 更新…」（P3 的按钮也会隐藏）。
- 结束：没有可显示的超过 1 分钟，或只剩做完未看的超过 5 分钟，推 `end`（`dismissal-date` = 现在）并忘掉活动 token。
- 开始：有活动 token 就 `update`；没有（App 没开过、上一个结束了）而有 push-to-start token，就在「一轮」开始时推一次 `start`
  （从空 / 只剩做完的变成有需要你或工作中算新的一轮）。用户划掉活动后，这一轮不会再推开。活动超过 8 小时（系统上限）当作已经没了，下一轮重新开始。
- APNs：`apns-push-type: liveactivity`、`apns-topic: <bundle>.push-type.liveactivity`，目标是活动自己的 token（或 push-to-start token），
  环境跟设备注册时一样（Debug 构建是 sandbox）。410 / BadDeviceToken 时删掉那个 token。
- 每台设备一行 `live_activities`（`device_id`、`activity_id`、`push_token`、`push_to_start_token`、`started_at`），内存里有一份，每次变化写库。
- 开着 App 时 hub 还广播 `{"type":"fleet","fleet":{…}}`，App 自己更新活动（比推送快，模拟器上也能用）。

## 推送节奏（2026-11-02，耗电排查）

用户真实 hub 上，灵动岛的内容一小时变约 180 次，以前每次都推一条，手机每收到一次就重画一遍灵动岛和锁屏。现在：

| 变化 | 例子 | 怎么推 | 灵动岛最多落后 hub |
| --- | --- | --- | --- |
| 关键 | 有 Agent 开始需要你（提问、审批、终端对话框），或不再需要你（答完了、在电脑上答了、走了） | 立刻推，priority 10 | 0（hub 一侧；之前的屏幕卡判断仍有 1.5 / 3 秒） |
| 普通 | 在工作 / 做完的个数、条目标题、助手在回答、轮次开始的时间 | 合并 2.5 秒；同一台设备距上次推送不到 `fleetMinGap`（30 秒）就等到那时，推当时最新的 | 30 + 2.5 秒 |
| 只有数字 | token、TPM、负载 | 照旧：每台设备至少隔 `live_metrics_seconds`（60 秒），取整后变了才推 | 60 秒 |

- 上限由 `TestFleetPacingBound`（每 3 秒一变、10 分钟，夹着提问和答完）和 `TestPushReplay`（真实 hub 录下的 19 分钟）守住：
  「灵动岛显示的和 hub 不一样」连续最长 29.2 秒；改 30 秒为 60 秒是 58.8 秒（`docs/testing.md`「推送回放」）。
- **App 开着时不推**：App 在 `register_phone` 里带上推送用的设备 id（`device_id`），连着的这段时间 hub 不给它的活动发 APNs，
  只记下「它已经画了这一版」；App 自己按 `fleet` 事件更新活动。App 进后台主动断开（close 1000 / 1001）时，hub 认为活动停在最后广播的那一版；
  网络断、被杀、挂起后被 ping 超时踢掉（不是自己关的），或者最后一版已经超过 10 分钟（可能过期了），hub 不知道它显示什么，立刻补推当前状态。
  老 App 不带 `device_id`，照旧推。
- 回放（录下的 19 分钟，App 不开时）：每小时 188 条 → 115 条（30 秒）；60 秒时是 60 条。App 开着时 0 条。

## 用量（token / TPM / 负载）

- 数据源是 transcript：node（`node/usage.go`）每 10 秒扫一次本机所有 transcript 新增的部分——Claude Code 各配置目录下的主会话和子 Agent 文件
  （assistant 行的 `message.usage`，同一条回复的每个内容块重复同一份 usage，按 message id 去重；`<synthetic>` 不算），
  Codex / TRAEX 的 rollout（`token_count` 事件的 `last_token_usage`，总量没涨的重复事件跳过）。按「分钟 × 模型」累计最近 26 小时。
- 只读新增的字节：每个文件记偏移，没写完的半行留到下次；第一次见到的大文件从尾部往前找到 26 小时前的位置再读，
  超过 16 MB 的单行（图片）跳过。实测 hub 机器一天的 transcript 第一次扫 85 ms，之后每次约 1 ms。
- node 把变了的分钟报给 hub（`usage` 事件，每个分钟报全量值，重复报只是覆盖）；连上后第一次带 `full: true` 发整份。
  不落库：node 跟 hub 连接同生共死，重启任一边都会从文件重算出同样的数。
- 管理者（`devices/metrics.go`）汇总所有机器：TPM = 最近 2–3 分钟（从两分钟前那一分钟的开头到现在）的输出 token ÷ 分钟数；今日 token 含缓存读写，
  「今天」按手机的时区（助手设置里报的）算；负载 = 1 分钟 load average ÷ 核数，2 分钟没报的机器不算负载。
- 推给灵动岛前取两位有效数字（12345 → 12000，负载取整到 5%），数字小幅波动不会变成新的内容。

## 推送预算

- 状态（需要你 / 在工作 / 做完、条目、按钮）的变化：「需要你」的变化立刻推（priority 10），其他合并 2.5 秒、每台设备最多 30 秒一次（priority 5），见「推送节奏」。
- 只有数字变了：hub 每 15 秒看一次，对每台有活动的设备，距它上次收到推送超过 `notify.live_metrics_seconds`（默认 60 秒）、
  而且它显示的（取整后的）数字确实变了，才推一次 priority 5 的 update。状态推送也会顺带最新的数字。
- 每台设备记着上次推给它的 content-state，一样的就不推（比如助手开始回答，但这台设备的灵动岛不显示助手）。
- App 在前台时自己用 `fleet` 事件更新活动（不占推送预算），数字跟着每次变化走；这时 hub 不给这台设备推（新 App 才行，见「推送节奏」）。

## App（`LiveActivityController`）

- 启动就监听 `pushToStartTokenUpdates`、`activityUpdates`（hub 推开的活动），每个活动监听 `pushTokenUpdates`（会轮换）和结束 / 划掉，
  都上报 `POST /api/v1/devices/{id}/live-activity`。
- 前台收到 `fleet` 事件或回到前台拉 `GET /api/v1/fleet`：有内容就开始（只有前台能 `Activity.request`）或更新；空了等 1 分钟再结束（轮次之间会闪）。只有助手在回答、灵动岛又有一侧显示助手时不算空，开着的活动留着（「思考中」），但不为它新开一个；助手答完再按 1 分钟结束，和 hub 一致（#4628 / #4590，`LiveActivityController.step`）。
  和活动正在显示的一样（这台设备不显示的东西变了）就不 update，除非过期时间不到 5 分钟了（每次 update 续 15 分钟，`worthUpdating`）：每次 update 都要重画灵动岛和锁屏。
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
  每次回到前台从 hub 同步。系统设置里关掉了 Persona 的实时活动时，这一节提示并给「打开系统设置」。
- 断开 hub（忘记服务器）时结束活动：它的按钮连的是旧 hub。
- 没做「盯着一个 Agent」的单独活动：Fleet 已经包含每个在跑的 Agent，再开第二个活动会在锁屏上堆两条；有需要再加 `kind: agent`。

## 验证

- 布局：`-showFleetSample needsYou|working|done|thinking|assistant|offline`（`thinking` 只有助手在回答（旁边还有一个做完未看），`assistant` 只有助手、没有可数的（最小态这时画形象），`offline` 是开出来就标成过期的工作中；`-fleetDetails YES` 看锁屏详情，`-fleetIsland b|c|custom:assistant,load` 选灵动岛两侧，`-openPersona jarvis-circuit` 换形象）用编造的数据开一个活动，不需要 hub。
  UI 测试 `LiveActivityShots` 读 `JARVIS_FLEET_ISLAND`、`JARVIS_PERSONA`（形象包要 `JARVIS_MOCK_HUB`：`MOCK_PERSONAS=demo` 的 mock_hub，App 先从它下载），
  `testSwitchingPersonaRedrawsTheIsland` 读 `JARVIS_PERSONA_SWITCH=demo,jarvis-circuit,jarvis`：活动开着时在设置 › 外观 › 形象里依次换，每次回桌面 3 秒后拍灵动岛；紧凑态隔 0.4 秒拍两张（`compact` / `compact2`），用来比对有没有动画，3 秒后再拍一张（`compact3`，刚回桌面时岛还在动画里，可能是空的）；
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
