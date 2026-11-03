# Android 界面风格与设计系统

2026-10-31 起，Android App 的视觉和交互整体重做。按用户的要求，三个方向都做成主题，在 **设置 › 界面风格** 里切换，默认是**玻璃**。
旧的设计（`docs/redesign/` 里讲外观、布局和交互的部分，`prototypes/android/` 的 HTML 原型）已被本文取代。

## 1. 三套风格

页面只写一套。每套风格各自提供设计 token（色板、字体、形状、动效）、组件皮肤，以及 App 外壳和导航。交互上的差别放在按风格渲染的组件里，页面代码不分支。

| | 玻璃（默认） | 晨报 | 看板 |
| --- | --- | --- | --- |
| 一句话 | 一层磨砂玻璃浮在「状态光」上 | Persona 先开口：白纸墨字，一支荧光笔 | 塔台的进程单：每个 Agent 一条 |
| 背景 | 夜蓝 `#060A14` / 晨雾 `#E9EEF6`，上面是随状态变化的光 | 白纸 `#FFFFFF` / 墨夜 `#0E1117` | 看板灰 `#E6E9ED` / 石墨 `#0E1114` |
| 强调色 | 琥珀 = 等你，蓝 = 在跑，薄荷 = 做完 / 已连接 | 只有荧光笔 `#FFE15A`，只标 Persona 的建议和等你的事 | 进程单左边的色块就是状态：琥珀 / 蓝 / 绿 / 灰 |
| 主按钮 | 白色实心（深色）/ 墨色实心（浅色），胶囊形 | 墨色实心胶囊，深色模式反白 | 墨色实心，8dp 小圆角 |
| 字体（拉丁和数字） | Sora（大标题、数字） | Manrope（全部） | Barlow Semi Condensed（数字、字段、计数） |
| 形状 | 卡片 28dp，浮层 30–36dp 胶囊 | 卡片 24dp，其余是无底色的行 | 10dp 小圆角加 1dp 描边 |
| 动效 | 弹簧；状态光约 1 秒渐变，平时缓慢漂移、分合（见 3 节「状态光的动态」） | 平缓的 240ms 过渡 | 干脆的 160ms |
| 导航（四个 tab 一样：Persona、Agents、收件箱、设置，图标加常显的文字） | 底部悬浮的玻璃坞 | 贴底的纸面底栏，顶上一道细线 | 底栏四格，选中的一格顶上一道横线 |
| 要你定的事 | 列表里是玻璃卡片；对话里是浮起的玻璃面板，边缘琥珀色 | 列表里是浅灰卡片，Persona 的建议用荧光笔划出；对话里是贴底的决定面板 | 琥珀色块里的进程单，点一下就地展开；对话里是琥珀横线下的放行条 |
| 根页面标题 | 大标题随内容滚走，顶栏只剩悬浮的圆形按钮，滚动后出现标题胶囊 | 首页是一句问候和一句总结（「早上好，有 3 件事等你定。」），其余页面是大标题 | 紧凑的标题栏，标题一直在栏里 |

深浅色仍然跟随系统，和风格互不影响。中文一律用手机自己的系统字体，拉丁字母和数字用打包的字体（OFL，许可证在 `core/designsystem/src/main/assets/licenses/`）。
图标统一用 Phosphor（MIT），由 `scripts/assets/gen_phosphor_icons.py` 生成 `PhIcons`，`JIcon` 按风格选粗细：玻璃和晨报用 regular，看板用 bold，选中状态用 fill，玻璃里设置行的前导图标用 duotone。

### 1.1 切换和保存

- 入口：设置 › 界面风格（`AppearanceSettings`）。三个选项各带一张用该风格真实组件画的小预览，点一下立刻生效，不用重启。预览是三分之一大小的 Agents 页：一台电脑、它的一个空间和三个会话，底下是该风格的导航（玻璃的悬浮坞用不透明面板画，预览里没有可折射的内容）。
- 选择存在本机（`AppStyleStore`，SharedPreferences `jarvis-appearance`），不跟 hub 走，重新配对也保留。
- 调试构建可以在启动时指定：`am start … --es debug.style glass|brief|board`（截图脚本用）。

### 1.2 玻璃的透明效果和降级

| 情况 | 效果 |
| --- | --- |
| Android 13 及以上，透明效果「自动」 | 折射玻璃（Haze 2 的 glass，`GlassLevel.Liquid`） |
| Android 12 | 模糊玻璃（`GlassLevel.Frosted`） |
| 透明效果选「减少透明」，或手机开了省电模式 | 不透明面板（`GlassLevel.Solid`） |
| 滑动时连续掉帧（`GlassFrameMonitor`：240 帧里超过 30% 的帧超过 1.5 倍帧预算） | 本次运行降一级：折射 → 模糊 → 不透明 |
| 终端页（任何时候） | 不透明面板：终端每一帧都在变（程序的每次重画、滚动的每一行），玻璃压在上面要逐帧重算顶栏、键栏和边缘的模糊（2026-11-01） |

只有浮在滚动内容上的件（顶栏、悬浮坞、输入框、决定面板）用折射或模糊；页面里的卡片是半透明填充加渐变描边，不做实时模糊，滚动时没有额外开销。
顶栏和底部浮层（悬浮坞、输入框）所在的整段高度下面各有一条「滚动边缘」：滑进去的内容在浮层区域里全程模糊并洗向背景，只在靠内容的最后 28dp 渐隐，
浮层之间的空隙（标题胶囊和任务条之间、状态行和输入框之间）不会露出清楚的字。洗的是页面自己的那层背景（同一层状态光，`PageBackdrop`），不是一块纯色：
下面没有内容时边缘看不出来，空页面（比如 Persona 里刚新建的对话）的顶部和底部不会出现一条色带（2026-11-01 用户指出的硬切）。
列表页只在内容真的滑到浮层下面时才画边缘（停在顶上时不画，省一块模糊）；没有顶栏的页面（嵌在链接卡片里的文档）不画顶部边缘。不透明档不模糊，把内容从浓到淡地洗向背景。
嵌在另一页里的页面（Persona 页里的对话）铺满整页，从宿主的顶栏和悬浮坞下面滑过：宿主经 `ChromeInsets(top, bottom)` 告诉它留多少，边缘由它自己画（宿主用 `JPage(edges = false)`）。
实现上要注意：Compose 的 `Canvas` 默认不裁剪，按窗口排布的光圈中心常常落在本页之外，`AmbientLight` 必须 `clipToBounds()`，否则会画到旁边那一栏上。
模糊的输入是四分之一分辨率（`JarvisTheme` 里 `LocalHazePerformanceMode` = `Performance`）：手机实测全分辨率时对话页帧时间翻倍，降下来看不出差别。
性能实测见第 5 节。

## 2. 设计系统（`core:designsystem`）

### 2.1 Token

| 读法 | 内容 |
| --- | --- |
| `JarvisTheme.style` | 当前风格 `AppStyle` |
| `JarvisTheme.palette` | `JarvisPalette`：background、surface（玻璃里半透明）、surfaceOpaque、surfaceStrong、sunken、line、ink / ink2 / ink3、action / onAction、accent、marker、各状态的 `StatusColors(dot, text, tint, onDot)`、diff 和终端色 |
| `JarvisTheme.type` | `JarvisType`：largeTitle、title、headline、body、bodyStrong、callout、caption、micro（等宽数字）、label、labelSmall、number、voice（Persona 说话的那句）、code |
| `JarvisTheme.shapes` | 卡片、面板、控件、chip、按钮、底部面板的圆角 |
| `JarvisTheme.motion` | 时长和曲线（玻璃是弹簧） |
| `JarvisTheme.glass` | 玻璃的实际渲染级别 |

Material 3 的 ColorScheme、Typography、Shapes 也由当前风格生成，没改完的 Material 组件会跟着变色和换字体，但页面应当换成下面的组件。
旧的 `JarvisTypography.*`、`statusTone(...)`、`PrimaryButton` / `SecondaryButton` / `Card` / `EmptyState` 仍然可用，内部已经转到当前风格；新代码直接用新组件。

### 2.2 组件

| 组件 | 用途 |
| --- | --- |
| `JPage` | 自定义内容的页面。把它给的 `PaddingValues` 作为滚动容器的 contentPadding：玻璃里顶栏、输入框、悬浮坞都浮在内容上，要靠这个留出空间 |
| `JListPage` | 列表页面（根页面和大多数二级页面）。自带大标题、下拉刷新、内容边距；`summary` 是 Persona 对这一页的一句话，`greet` 加日期和问候（只给首页） |
| `JTopBar` | 页面顶栏；`JPage` / `JListPage` 已经包含，单独用于特殊页面。玻璃里大标题还在屏幕上时（`showTitle = false`），返回和操作各自是浮着的圆形按钮，大标题滚走后才合成带标题的胶囊 |
| `JFullScreen` | 盖在当前页面上的整页（会话内搜索、全文阅读、任务进度、空间、历史会话）：铺满系统栏、状态栏图标随深浅色。不要直接用 Compose 的 `Dialog` 做整页 |
| `JBottomBar` | 页面底部的一排操作（翻页、加载更多）：玻璃是浮起的胶囊，晨报和看板是贴底的栏 |
| `ChatTopBar` | 对话页顶栏：标题，一行状态（状态、在哪台电脑哪个目录），操作 |
| `JSectionHeader` | 页面里的分节标题，可带计数和操作 |
| `JGroupItem` + `GroupPosition` | 分组里的一项。懒加载列表一项一个 item，组件按位置画出连续的容器（玻璃面板、带分隔线的行、带边框的表格）；`separate = true` 是各自独立的卡片 |
| `JGroup` | 不需要懒加载的小分组（设置页） |
| `JRow` / `JSwitchRow` | 设置式的行：图标、标题、副标题、值、开关、箭头。中间要放多行内容（会折行的地址、标签、测试结果、前面一个状态点）时用插槽写法 `JRow(onClick = …, trailing = …) { … }`：左右边距、最小高度、点击和长按、选中、禁用变淡、尾部控件和箭头都和普通行一样，各风格自动对齐。`rowPadding` 是行的左右边距（玻璃 16、晨报 0、看板 14），挂在行下面、要和行文字对齐的内容（长值、说明）用它 |
| `JCard` | 卡片；`status` 让玻璃描边带状态色、看板加色块 |
| `JNotice` | 页内提示（连接断了、需要权限），带一个操作 |
| `JEmptyState` / `JLoading` | 空状态、加载中 |
| `JButton`（Primary / Secondary / Quiet / Destructive，Large / Medium / Small）、`JTextButton`、`JIconButton` | 按钮。文字永远单行 |
| `Modifier.stableAlpha` | 会变的透明度（禁用变淡）。不要用 `Modifier.alpha`：它在 1 和别的值之间切换会增删一层图层，排在 `clip` + `background` 后面时底色会丢（`docs/android.md` 经验） |
| `JTextField` / `JSearchField` | 输入框 |
| `JSegmented`、`JChip`、`QuickFilterBar` | 互斥筛选、chip、列表顶部的快速筛选（玻璃是 chip，晨报是带下划线的文字 tab，看板是大数字计数板） |
| `JSwitch` / `JCheckbox` / `JRadio` | 开关、勾选、单选 |
| `JBadge` / `JTag` | 等你的计数、静态小标签（草稿、体验） |
| `JStatusDot` / `JStatusMark` / `StatusLabel` / `StatusLine` / `PendingBadge` | 状态：小点、会话行前的标记（玻璃发光、晨报转圈和对勾、看板方块）、状态文字、对话里那一行状态 |
| `JAvatar` | 人、群、来源；角标标出飞书 / 邮件 |
| `JSwipeActions` | 行的左右滑动操作（收件箱：右滑已处理，左滑交给 Persona） |
| `JCodeBlock` | 命令和代码 |
| `JSheet` / `JActionSheet` / `JMenu` / `JDialog` | 底部面板、长按操作面板（代替长下拉菜单）、锚点小菜单、确认对话框 |
| `DecisionCard` | 列表里「要你定的事」 |
| `DecisionDock` | 对话里待处理的请求，代替输入框 |
| `DecisionOption` / `DecisionButtons` / `DecisionAdvice` | 选项（推荐的那个在三种风格里各有突出方式）、回答按钮、Persona 的建议 |
| `MachineChip` | Agents 页顶部的电脑芯片（全部机器 + 每台电脑）：该风格的筛选 chip，设备图标的角上一个在线点，名字后面是计数 |
| `AgentTreeHeader` / `AgentSpaceHeader` / `AgentRow` / `AgentIdleRow` | Agents 树（见 2.4 节）：第一层（电脑，也用于置顶、按状态、历史项目这些段）、空间、会话行（玻璃和晨报是背景上的一行，看板是进程单；行上的标记 `RowFlag`、事实 `RowFact`）、「还有 N 个空闲」。栏头上的小图标按钮（仪表、空间的 `⋯`）是 `HeadingAction`。Agents 页、空间页、组件目录和界面风格的预览用的都是这一套，不另写 |
| `UserBubbleFrame` / `UserBubble` / `WorkGroup` / `WorkStep` / `TimelineMark` | 对话：用户消息、折叠的工作过程、每一步、分隔线 |
| `ComposerFrame` / `ComposerTextField` / `SendButton` / `JumpToLatest` | 输入框的外框、输入、发送 / 停止、回到最新 |
| `MessageItem` / `DraftPreview` | 收件箱的一项、Persona 拟好的回复 |
| `HubCard` | 设置页顶部的 hub 状态 |
| `AppNavigation` / `HubAvatar` / `JarvisOrb` | App 外壳的导航（由 `:app` 使用） |
| `PageBackdrop` | 页面、栏、外壳共用的一层背景：页面底色，玻璃里再加按整个窗口排布的状态光。挨着的栏、套着的页面和它们之间的空隙画出来是同一块，页面脚手架和外壳已经用了；自己要画底色的地方（比如宽屏 Persona 的对话列表）也用它，不要另铺 `surface` 一类的底板 |
| `PaneDivider` | 宽屏两栏之间的把手：玻璃是半透明的墨色药丸，晨报是细墨线，看板是带描边的小格；拖动、悬停光标、TalkBack 的「左栏变宽 / 变窄 / 收起 / 展开」，外壳用（见 3.1 节） |
| `VoiceOrb` / `VoiceStatus` / `VoiceCaption` / `VoiceControl` / `VoiceControlBar` | 语音对话页：光球（连接中、在听、正在说、静音、结束）、状态行、字幕、底部带字圆按钮（`feature:voice` 用，`docs/android.md`「语音对话」） |
| `AmbientLight` / `floatingSurface` / `glassSource` | 玻璃的状态光（`inWindow` 按窗口排布，缩略预览按自己的框）、浮层、浮层采样的内容（由页面脚手架处理，页面一般不直接用） |

### 2.3 写页面的规矩

1. 只用上面的组件拼页面。不要直接用 Material 3 的视觉组件：`Scaffold`、`TopAppBar`、`NavigationBar`、`Button` 系列、`Card`、`OutlinedTextField`、`FilterChip`、`AlertDialog`、`ModalBottomSheet`、多项的 `DropdownMenu`。`Text`、`LazyColumn`、`Box` / `Row` / `Column` 这些基础件照常用。
2. 颜色只取 `JarvisTheme.palette`，字号只取 `JarvisTheme.type`，圆角只取 `JarvisTheme.shapes`；不写 `Color(0x…)`、不写数字字号。图标用 `PhIcons` + `JIcon`，不用 material-icons。
3. 页面结构：根页面 `JListPage`（不传 `onBack`，自动大标题）；二级页面 `JListPage(onBack = …)`；内容不是列表的页面（对话、终端、阅读器）用 `JPage`。
4. 一定要把 `JPage` 给的 padding 用作滚动容器的 contentPadding，不能只给外层套 padding（那样玻璃里内容不会从浮层下面滑过）。
5. 分组用 `JGroupItem(GroupPosition.of(i, n))` 或带 `position` 参数的行组件，一行一个 lazy item；不要把很多行包进一个 item（长列表会卡）。
6. 「要你定的事」：列表里 `DecisionCard`，对话里 `DecisionDock`，选项 `DecisionOption`（推荐项传 `recommended = true`），按钮 `DecisionButtons`，Persona 的建议 `DecisionAdvice`。
7. 长按菜单用 `JActionSheet`，两三项的锚点菜单用 `JMenu`。盖住整个屏幕的页面用 `JFullScreen`，底部的操作栏用 `JBottomBar`，不要让文字按钮直接浮在内容上。
8. 元信息不用「·」拼字符串：机器、目录、Agent 种类、循环、后台这些交给组件参数（`machine`、`folder`、`agent`、`facts`）。
9. 页面里不写 `if (JarvisTheme.style == …)`。需要按风格变化的东西放进组件；组件缺参数时在报告里写明，由设计系统负责人加。
10. 按钮文字单行；空间不够就缩短文案或改成图标（项目规矩）。

### 2.4 Agents 列表（电脑 → 空间 → 会话）

2026-11-02 按用户反馈重做（原话：设备、workspace、会话视觉分割严重，层级看着不对，屏效不好；全部机器不要折叠栏；空闲的空间不知道是干嘛的）。
组件在 `core:designsystem` 的 `Agents.kt`（`MachineChip`、`AgentTreeHeader`、`AgentSpaceHeader`、`AgentRow`、`AgentIdleRow`），树怎么拍平成懒加载的一行一行在 `feature:agents` 的 `agentTreeEntries`（`AgentsTree.kt`，有单测）。组件目录（调试版）的「Agents」一节和设置 › 界面风格的预览也用这套组件画，和真实的列表长得一样（2026-11-02 从 `feature:agents` 挪过来，换掉了原来每层垫底板的旧组件）。

- 页面背景是一整块，不放「不是业务卡片、却把背景切成几块」的底板：树的三层都不垫底板，只有业务卡片（等你定的请求卡、连接提示；看板的进程单，每条就是一个 Agent）浮在背景上。
- 层级靠缩进、字号字重和细线：
  - 电脑最醒目：`title` 加粗，前面是折叠箭头和设备图标（服务器 / 台式 / 笔记本，跟 hub 的 `icon`），名字后面一个在线小点（离线才写「离线」），下一行「N 个空间，M 个会话」，右边是各状态计数和进机器详情的仪表按钮。电脑之间一道细线（看板是 2dp 的墨线）。
  - 空间次之：缩进一级（22dp），`label` 字号，折叠箭头、置顶 / 屏蔽标记、各状态计数、`⋯` 菜单（打开空间、全部标为已读、置顶 / 屏蔽 / 重命名空间）。原来单独的「›」并进了菜单。
  - 会话行紧凑：状态标记在空间箭头那一列，标题从箭头后面开始；第一行标题（需要你、完成未看加粗）+ Agent 种类（只写 Codex / TRAEX）+ 时间，第二行状态字（彩色）+ 最新一句，只有循环、后台、草稿、待发这些事实时才多一行。树里的行不再重复机器和空间（栏头已经说了），置顶段、按状态分组、历史这些树外的行在事实行写「机器 / 空间」。玻璃和晨报的行直接放在背景上，行与行之间一条从标题开始的细线；看板保留进程单（左边状态色块 + 字形），压成两行，条与条之间留 6dp。
- 空闲空间（里面全是已看的会话）不再收进单独的「空闲的空间」分组：留在所属电脑下面，排在有动静的空间后面，默认收起，名字用 `callout` 和 `ink2`，右边写「N 个会话」；搜索时自动展开。
- 只看一台电脑时（芯片选中它），树不再画这台电脑的栏头（标题和芯片已经说了），空间顶到第一层。
- 页面顶部依次是：搜索框、电脑芯片（全部机器 + 每台电脑：设备图标的角上一个在线点、名字、计数，可横滑；再点一次选中的电脑回到全部）、快速筛选。芯片的计数是选中它时列表会显示多少个会话（跟着范围、搜索和快速筛选走）。
- 机器监控：「列表设置」面板里的「机器监控」进机器列表，电脑栏头的仪表按钮进这台电脑的详情。

## 3. App 外壳

`app/…/ui/Root.kt`：

- 底部固定四个 tab：Persona / Agents / 收件箱 / 设置，三套风格顺序相同，每个都是图标加常显的文字；dock / 底栏的样子各按各的风格（`AppNavigation`）。
  设置就是第四个 tab，没有藏在别处的入口；没有不带文字的按钮。
- 每个 tab 有自己的返回栈和角标。访问过的 tab 一直保持组合：切走时只是不放置（不绘制、不接收触摸），返回键也对它关闭
  （`rememberNavigationEventDispatcherOwner(enabled = false)`），切回来时页面原样还在，不会重建、重新加载、闪出空状态或加载圈。
- dock 属于每个 tab 的第一页（`NavEntryDecorator` 包在根页面外面），推进去的页面（对话、文档、二级设置）盖在它上面、没有 dock，
  返回时 dock 跟着根页面一起出现，不会先出现在正在退出的对话页上。玻璃里每一页采样自己的内容，不会模糊到别的页面。
- 转场：推入时新页面整页不透明地从右边滑入，旧页面留在下面；返回时上面的页面滑走，露出下面的页面。不做淡入淡出（两页同时半透明会叠字），
  切 tab 不做动画。
- 在 Agents、收件箱、设置的第一页按返回，回到 Persona tab；在 Persona 首页再按返回才离开 App（2026-11-01 用户改为：返回到 Persona，再按退出）。
  Persona 里开着一个对话时，返回先回到 Persona 首页（助手页自己的处理），再按一次才退出。
- 玻璃的悬浮坞浮在根页面上，通过 `LocalChromeInsets` 告诉页面它占了多高，`JPage` / `JListPage` 自动把这段高度加进内容边距；晨报和看板的底栏占自己的位置。
- 背景只有一层（2026-11-01 用户要求：背景是一整块，靠上面的业务卡片分割）：外壳、每一页、详情占位都画 `PageBackdrop`，玻璃的状态光按整个窗口排布，
  各页在同一位置画出同样的像素，所以栏与栏、页面与嵌在里面的页面之间看不出接缝，也不再各有一块底板；分隔靠会话卡、消息卡、设置分组这些卡片。
  玻璃里顶栏、输入框、悬浮坞都浮在这层背景上，滚动边缘也洗向它（见 1.2 节）。晨报整页是白纸，底栏是纸面加一道细线；看板整页是看板灰，
  白色进程单是卡片，底栏、输入框、页面底部的操作栏也是看板灰加一道线（输入框本身是一张白卡），不再是整条白板。
- 外壳按全局状态给页面一个 `LocalAmbience`（有事等你 → 琥珀；有 Agent 在跑 → 蓝；连不上 → 红），玻璃据此调背景的光。连接状态显示在各页自己的标题位。
- 状态光的动态（2026-11-03 用户要求，#959）：每种状态色的光在静止位置附近慢慢漂移（短边的 12%），大小、亮度轻微起伏，
  颜色向相邻的状态色偏一点（蓝 ↔ 薄荷、琥珀 ↔ 红）；每隔两分钟左右分出一团小光往屏幕中间游，过一阵再游回去并进主光，
  不同颜色的小光在中间碰上就混成一种过渡色。周期都是几十秒到几分钟、互不成倍数，看不出循环；单测（`AmbientLightTest`）卡住速度上限
  （每秒不超过短边的 5%）和小光淡入淡出。实现是绝对帧时间的纯函数（`Pool.blobs`），所以并排的栏、滚动边缘、套着的页面各画一份也对得上；
  每秒只重画 12 次（光太慢，看不出步进，而背景每变一次上面的玻璃都要重新模糊）。系统关掉动画（减少动画）或开省电模式时光停在静止位置，
  和改动前一样；`android.sh shots` 会把动画倍率设成 0，所以批量截图仍是静止的。

## 3.1 平板和折叠屏

布局只看窗口，不看机型：外壳用 material3-adaptive 的 `currentWindowAdaptiveInfo()`（WindowSizeClass，加上 Jetpack WindowManager 的 FoldingFeature
给出的姿态）算出一个 `WindowLayout` 交给设计系统，页面自己不量宽度。分屏、自由窗口、旋转、折叠和展开都只是重新组合
（`MainActivity` 声明了 `configChanges`，不重建 Activity），当前 tab、打开的会话、滚动位置和输入框草稿都在。

| 宽度 | 导航 | 列表和详情 |
| --- | --- | --- |
| compact（手机、折叠屏合上） | 底部 dock，四个带文字的 tab | 单栏 |
| medium（平板竖放、折叠屏展开竖放） | 底部 dock（在列表那一栏下面） | 宽度 ≥ 760dp 分两栏，否则单栏 |
| expanded（平板横放、折叠屏展开横放） | 左侧竖排导航栏，同样四个 tab，三套风格各自的样子 | 两栏 |

- expanded 用左侧导航栏：起初是我们替用户定的（用户明确要的是 compact / medium 保持底部 dock），2026-11-01 用户确认保持这样。以后要改回底部 dock，只改 `Root.kt` 里 `expanded` 时的分支。
- 两栏用 Navigation 3 的 list-detail 场景（`rememberListDetailSceneStrategy`），哪页是列表、哪页是详情由外壳统一给（`Root.kt` 的 `paneMetadata`）：
  Agents 的「电脑 → 空间 → 会话」树 | 对话（终端接管也在右栏）；收件箱列表 | 飞书会话 / 邮件；设置分类 | 设置子页（设置子页因此都走导航）。
  Persona 主对话不分栏。右栏没选东西时是占位页。
- 各栏共用外壳的那一层背景（第 3 节）：左侧导航栏没有自己的面板（玻璃不再是浮起的胶囊柱，晨报和看板也没有竖线和白底），
  栏与栏之间只留 8dp 空隙（`horizontalPartitionSpacerSize`），不画分隔线、不垫第二层底板；右栏的占位页、宽屏 Persona 的对话列表也画同一层背景。
- 双栏时右栏正在显示的那一项在左边的卡片上高亮（`LocalDetailKey` + 组件的 `selected`），由外壳按路由算（`detailOf`），点左栏、深链、通知、调试入口和返回都同步；单栏和双栏之间切换用的是同一个返回栈，选中的不变；
  双栏里返回键先关掉右栏的详情，回到「列表 + 占位」，再按才回到 Persona。list-detail 场景的返回方式要用 `PopUntilContentChange`：
  默认的 `PopUntilScaffoldValueChange` 要等到栏的布局变了才停，而双栏里关掉详情后占位页还在、布局不变，于是整个返回栈被弹空，按一次返回就退出了 App（2026-11-01 修）。
- 两栏中间的分隔条可以拖（`PaneDivider`，`app/…/ui/PaneDividers.kt`）：左栏跟手，经过平放的折痕轻震、松手在 28dp 内吸上去；最窄左 280dp、右 320dp；没拖过时左栏约 38%（320～420dp，右栏尽量留 400dp）；每个 tab 按宽度比例记住位置；Agents 旁边开着东西时拖到最左收起列表。分开的折痕固定分栏、没有把手。细节见 `docs/android.md`「大屏两栏」。
- Agents 的空间页是详情（`SpaceRoute`）：宽屏在列表右边打开，不再盖住两栏。
- 横放时外壳按左右安全区（挖孔、侧边的导航条）留白，背景照样铺满。
- 正文限宽：`pagePadding` 带上 `LocalReadingInset`，宽窗格里内容居中、最宽 `ReadingWidth`（760dp），对话、长文、列表都一样；
  输入框、按键栏这些底部浮层也跟着限宽。图片和表格按内容宽度排。
- 折叠屏书本姿态（竖向半折）：两栏按铰链分开，内容和按钮不压在铰链上（list-detail 场景按 `HingePolicy` 处理）。
- 桌面姿态（横向半折）：有底部浮层的页面（对话、终端）把内容放在铰链上面，输入框 / 按键栏放在铰链下面（`PageFrame`）。
- 键盘、鼠标、触控板：会话、消息、设置行的长按菜单也响应右键；悬停有反馈；列表支持滚轮；启动时软键盘不自己弹出来。
- 玻璃在大屏上模糊的面积更大（两栏各自的顶栏和滚动边缘），性能未实测。

## 4. 截图和验证

- 每个页面三种风格、深浅色都要截图检查；调试启动参数 `debug.style` 和 `debug.theme` 直达。
- 可读性底线（`core:designsystem` 的 `StyleContrastTest`，WCAG 对比度，玻璃的半透明底先叠到页面背景上再算）：正文（ink、ink2）在页面、卡片、面板、浅底、深底上 ≥ 4.5，主按钮的字、荧光笔上的字、角标数字 ≥ 4.5，强调色当链接 ≥ 3，自己的气泡里的字 ≥ 4.5，这些没有例外。状态字、第三级的字（ink3）、状态色块上的字和看板的色块字形也有门槛，但现在有 18 处差一点（主要是浅色下「完成」的绿字、离线的灰字、ink3，深色下危险按钮上的白字），记在测试的 `known` 里；只能减少，改色板时把对应的条目删掉。
- 验证玻璃时看：浮层下的文字是否可读、内容是否被悬浮坞或输入框挡住、折射有没有把背后的文字扭成碎片、浮层之间的空隙有没有露出清楚的字、整页弹层的状态栏和手势条区域是不是一条灰带。

## 5. 性能

在测试手机上实测，见 `docs/android.md`「界面风格的性能」。
