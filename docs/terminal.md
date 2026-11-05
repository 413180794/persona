# 终端：真终端怎么做

「终端」tab 要像一个真终端：完整颜色、光标、备用屏、宽字符 / 中文对齐、断线后接着看，长行在手机上完整显示（换行），
**会话不离开 herdr**。这里记 2026-10-29 spike 的实测结论和由此定下的方案。

起初的约束是「不改电脑上窗格的大小」，一到十节都按它做（镜像窗格、手机上折行）。同日用户改了主意：要 Moshi 那样真正的手机尺寸，
打开终端 tab 时手机接管窗格（第十一节）；镜像保留为「电脑尺寸」模式和接管不了时的降级。
同晚一度改成「只看」（abc18a9：去掉键栏、打字和接管），随后用户拍板改回手机尺寸接管 + 打字 + 键栏，往上滑的含义见第十二节。

## 一、数据从哪来（herdr 0.9.3 本机 / 0.9.5 hub，协议 22，实测）

| 途径 | 拿到什么 | 结论 |
| --- | --- | --- |
| `pane.read`（socket RPC） | 网格快照：`format` 为 `text` / `ansi`，`source` 为 `visible` / `recent` / `recent_unwrapped` / `detection` | `ansi` 带完整 SGR（粗体、16 / 256 / 真彩色、下划线、反显），中文原样；**没有光标位置**；每行 `\r\n` 结尾、行尾空白去掉。一次读约 0.3 ms（整屏 Claude 4.9 KB）。用来补**滚动历史**。 |
| 事件 `pane.updated` / `pane.scroll_changed` | 窗格元数据变化（标题、滚动范围），`revision` 也只跟元数据走 | **输出本身不触发事件**，不能拿来做推送；`max_offset_from_bottom` 能看出有多少行进了滚动历史。 |
| `herdr terminal attach <terminal>`（隐藏命令） | 交互客户端的字节流：**也是 herdr 重画的网格**（备用屏 + 逐格 CUP），不是程序的原始输出；要一个 PTY | **不用**：每个终端只有一个 attach 槽位（Ovis 装在同一台电脑上会互相抢），而且交互客户端会把终端改成自己的尺寸 = 改电脑上窗格的宽度。手机接管用的是同一个槽位的管道版 `session control`（第十一节）。 |
| `herdr terminal session control <pane> --cols C --rows R [--takeover]` | 占 attach 槽位，窗格里的程序看到的尺寸变成 C × R；stdin 每行一个命令（`terminal.input {text\|bytes}`、`terminal.resize`、`terminal.scroll`、`terminal.mouse`、`terminal.release`），stdout 和 observe 一样的帧 | **手机接管用这个**（第十一节）。 |
| `herdr terminal session observe <pane> --cols W --rows H` | **观察者流**：每行一个 JSON `{"type":"terminal.frame","seq","full","width","height","encoding":"ansi","bytes":<base64>}`，bytes 是 herdr 把窗格网格重新画出来的 VT 序列 | **用这个。** 不占 attach 槽位、不改窗格尺寸（观察时窗格仍是 172×45），多个观察者可以同时开；有输出就推（不用轮询）；不需要 PTY，普通管道就行。 |

`observe` 流的细节：

- 第一帧 `full: true`（`ESC[2J` + 整屏），之后是增量：只重画变了的格子（`CSI 行;列 H` + SGR + 文字）。整屏滚动时也是把各行重画一遍（不用滚动区域），
  所以接收端的仿真器不会自己产生滚动历史。
- 每帧包在同步输出里（`ESC[?2026h … ESC[?2026l`），末尾带光标位置和显隐（`ESC[45;16H ESC[?25h`）：**光标有了**，打字的回显实时可见。
- 用到的序列很少：CUP、ED(2J)、SGR（`0;1;38;5;N;49m` 这种全量写法）、DECTCEM、2026、OSC 8 超链接（`ESC]8;;url ESC\`，原样透传）。
- **`--cols/--rows` 必须等于窗格尺寸**：小于窗格时是左上角裁一块（不是重排），大于时多出的地方空着。不带参数时用的是调用者终端的尺寸。
  窗格尺寸从 `pane.layout` 的 `rect` 取；窗格变大小时 herdr 会再发一个 `full` 帧但尺寸还是观察时给的，所以 node 要在 `layout.updated` 后
  按新尺寸重开一个 observe。
- 观察者看到的是窗格当前显示的那一屏：电脑上有人往上翻（herdr 的滚动偏移），观察者也跟着看到翻上去的内容；备用屏同理（herdr 显示哪屏就是哪屏）。
- 程序发出的 OSC 52（写剪贴板）和响铃被 herdr 自己处理掉，**不会出现在流里**；标题从 `pane.get` 的 `terminal_title` 取。
  2026-11-04 在 herdr 0.9.5 上对 observe、control、`terminal attach` 三种流都实测过，见第十八节。
- 量：工作中的 Claude 窗格（spinner 在转）约 11 帧 / 秒、首帧 13.5 KB 之后约 3 KB/s；空闲时没有帧。

## 二、方案

**herdr 就是那个「真终端仿真器」**：程序的原始字节进的是 herdr 的仿真器，Persona 镜像它算好的网格（observe 流），
在窗格尺寸上原样还原，再决定手机上怎么摆（换行 / 不换行）。不另起 PTY、不 attach、不改窗格尺寸。

```
herdr 窗格 ──observe(窗格尺寸)──▶ node（每窗格一个 observe 进程，有人看才开）
                                   │ terminal.frame {epoch, seq, full, cols, rows, data}
                                   ▼
                                  hub（每会话留「最近一个整帧 + 之后的增量」做续传）
                                   │ WebSocket terminal 事件
                                   ▼
                                  App：TerminalGrid（VT 子集 → 格子）→ TerminalGridView（换行 / 不换行、选择复制）
```

- **续传**：每条流有 `epoch`（node 每开一次 observe 换一个）和递增的 `seq`。App 重连时带上自己的 `epoch` + 最后的 `seq`，
  hub 补发之后的增量；对不上（hub 重启、node 重开了 observe、增量太多）就从最近的整帧重放。整帧之后的增量超过 256 KB，node 重开 observe 换一个新整帧。
- **尺寸**：仿真网格永远等于窗格尺寸；手机的宽度、字号只影响手机怎么画，永远不回传。
- **滚动历史**：observe 只有当前一屏。往上翻仍走 `/screen/before`（按内容锚点分页，herdr 约 1000 行封顶），改成 `format=ansi` 带颜色。
  全屏的 Claude（备用屏）本来就没有窗格历史，历史在「对话」里。
- **换行**：默认按手机宽度把每一行折开显示（去掉行尾的默认底色空格后再折），中文按两格算；「不换行 · 左右滑」开关给 TUI 框线用，
  这时按窗格宽度画、左右滑动。
- **兼容**：旧 App 不发新的订阅，照旧用 `/screen` 轮询；旧 node / 旧 herdr 不认 `terminal.watch`（或 observe 起不来）时 hub 回
  `unsupported`，App 退回轮询读屏（`format=ansi` 的快照也有颜色，只是没有光标、1 秒一刷）。

## 三、iOS 端：仿真器选型

| | SwiftTerm | libghostty（GhosttyKit） | 自己写 VT 子集 |
| --- | --- | --- | --- |
| 许可证 | MIT | MIT | — |
| 接入 | SPM；1.99.1 的 Package.swift 带 swift-argument-parser、swift-docc-plugin、swift-png 三个依赖（都要做本地镜像）和一个 build tool plugin（Xcode / xcodebuild 要信任插件） | 要用 Zig 编出 xcframework（本机没有 Zig），嵌入 API 还没稳定（只有 libghostty-vt 有稳定的 C API） | 无依赖 |
| 体积 | 源码 3.2 MB，二进制增加约数 MB | 数十 MB（含 Metal 渲染器） | 几十 KB |
| CJK / 宽字符 | 有（wcwidth 表） | 有 | 自己按 East Asian Width 算两格 |
| reflow | 有（xterm.js 移植） | 有 | 不需要：网格固定为窗格尺寸，换行是显示层的事 |
| 能吃的输入 | 任意 VT | 任意 VT | herdr observe 用到的子集 + 常见 CSI，未知的忽略 |

**选自己写**（`TerminalGrid`）：我们的输入不是任意程序的字节，而是 herdr 已经仿真好、再按固定几种序列画出来的网格；
真正难的部分（解析程序输出、备用屏、滚动区域、宽字符的光标语义）herdr 已经做了。现成仿真器的 reflow 用不上（网格必须等于窗格尺寸），
它们的视图也不支持「网格 172 列、显示时按 45 列折行」，渲染和选择本来就要自己写。代价是 herdr 将来换了输出写法要跟着补，
所以解析器对未知序列一律忽略、不崩，并用真实 observe 录下来的帧做单测夹具。如果以后拿到程序原始字节（需要 herdr 给观察者开原始流），
再换 SwiftTerm 的 `Terminal`（无界面的引擎）也只动这一层。

渲染（`TerminalGridView`）按格子画：每个字符放在自己的格子位置（宽字符占两格），不靠字体本身的字宽，所以中文和框线永远对齐，
不需要为对齐打包巨大的 CJK 等宽字体；字体只影响好不好看（D：可以打包一款 OFL 等宽拉丁字体，中文用系统字体画在两格里）。

## 四、限制

- 程序写剪贴板的 OSC 52、响铃到不了手机（herdr 在电脑上处理）。手机上的复制走自己的选择（网页版见第十八节）。
- 电脑上翻看历史时，手机看到的也是翻上去的那屏（herdr 只有一个视口）。
- Windows：Ovis 的注释说 herdr 的 attach 只在 Unix 上有，observe 在 Windows 上未核对；起不来就退回轮询读屏。

## 五、实现备忘

- 文件：node `server/internal/node/terminal.go`、hub `server/internal/hub/terminal.go`、herdr `server/internal/herdr/observe.go`；
  App `apple/Persona/Views/Session/Terminal/`（`TerminalGrid` 解析、`TerminalFeed` 订阅 / 续传 / 回退、`TerminalView` 渲染、`TerminalWrap` 折行、
  `TerminalInput` 直接打字、`BoxDrawing`、`TerminalTheme`）。
- 配色默认「墨黑」（近黑底 0x0C0C0E，不随 App 浅色 / 深色变；对照 Moshi 默认纯黑）。终端卡片里的按钮、键栏、键盘都按配色的底色取深 / 浅外观。
  另有深夜（深灰）、Persona、Solarized（后两套跟随系统）。
- herdr 把反显（SGR 7）直接换成**它自己主题**的前景 / 背景真彩色发出来（`38;2;40;44;52;48;2;255;255;255`），手机上看到的是 herdr 的反显色，不随 Persona 主题变。
  普通的 16 / 256 色保持索引，由 Persona 主题决定。
- 框线字符（U+2500–257F）和块字符（U+2580–259F）不用字体画：回退字体的这些字形比格子窄，框线会变成虚线。按格子画线（圆角用二次曲线），
  同一格的几段合成一条路径一次描边，否则半透明（dim）的颜色在交叠处会更亮、看起来还是虚线。
- 有 emoji 形式的符号（⏺ ⏵ 等，Claude Code 常用）画的时候加 U+FE0E 要文字形式，否则会画成彩色 emoji，不是格子的颜色。
- 屏幕只画到最后一行有字的地方或光标处：`clear` 后提示符在第一行、下面四十多行是空的，跟着末尾会只看到空白。
- 宽字符宽度按 East Asian Width（Wide / Fullwidth）+ emoji 表现形式算；和 herdr 算的不一致时只错一行的后半段，下一次 CUP 就对齐了。
- 实测数据：真实帧的夹具 `apple/PersonaTests/Fixtures/terminal-observe.jsonl`（草稿窗格跑示例脚本录的，没有个人信息）；
  `go test -tags herdrlive -run TestLiveObserve ./internal/herdr/` 在有 herdr 的机器上验证 observe 本身。

## 六、字体（拍板 D）

默认 **JetBrains Mono**（OFL-1.1，v2.304 的 Regular / Bold 两个 ttf，共约 550 KB，`apple/Persona/Resources/Fonts`，许可证全文在
`THIRD_PARTY_NOTICES.md`），设置里可换回系统的 SF Mono。第一次用到时 `CTFontManagerRegisterFontsForURL` 注册，不用改 Info.plist。
中文、符号用系统回退字体，每个字放进自己的两格。

2026-10-29 以前没打包，理由是按格子渲染以后对齐不靠字体。改主意是因为对照 Moshi（默认 JetBrains Mono 14pt）：
「像终端」的观感主要来自字形，而 Latin 字体只有几百 KB，不必等含中文的等宽字体（那种要 10–20 MB）。

- 默认字号 13（原来 11）。行高统一为 `ceil(字号 × 1.42)`，不用字体自己的行距，所以换字体时行数不变。
- 中文字形约 1 em 宽，两格是 1.2 em，原样画字和字之间会空出一截（tab4 报的「字距不均」之一）。
  现在宽字符放大到填满两格的约 92%，上限是行高的 86%、1.25 倍。

## 七、直接在终端里打字（2026-10-29）

- 终端视图（`TerminalScrollView`）自己实现 `UITextInput`（`TerminalInput.swift`），点一下就成为 first responder，键盘直接作用在终端上。
  - 文档里只放输入法正在拼的 marked text（拼音），画在光标处，选定以后才发出去。
  - `insertText` / `replace(range:)`（听写）都当作打字。
  - 回车、tab 发键名，退格发 `backspace`。
  - 外接键盘的方向键、esc、ctrl+字母走 `keyCommands`。
- 键栏的 ctrl / alt / ⇧ 对键盘打的下一个键生效（ctrl 然后 c 就是 `ctrl+c`）。键盘弹起时键栏贴在键盘上方。
  键盘按钮在键栏最右，收起键盘只能点它。
- 发送用 `KeyStream` + `KeyBatcher`：同一时间只有一个 `/keys` 请求在路上，在途时打的字攒进下一个请求。
  - 一个请求的格式是「先文本、后键」，所以键后面再打的字进下一个请求。
  - 还没发出去的字，退格在本地直接删掉。
  - 结果是快速连打只发几个请求，顺序不乱。
- 光标跟着 herdr 的光标画：在终端上打字时是实心块，没在打字时是空心框。
- 页面头部（标题、空间条、tab）在键盘弹起时收成窄条，这一项由 `ChatView` 实现。
  iPhone 17 Pro、13pt 下，键盘弹起时能看到约 20 行（原来 13 行），不弹键盘时约 27 行。
- 设置 › 输入方式可以换回原来的「输入框」：写好一行再整条发出去，输入框有补全。

## 八、手机宽度换行（`TerminalWrap.swift`）

窗格按电脑的宽度排（常见 170 列），手机一行四十多格，要再折一次。以下规则都有真实帧夹具测试（`TerminalWrapTests`）：

- **在词之间折**：在空格后、中文字旁边断行，不在 `，。）` 等收尾标点前、`（「` 等开头标点后断。
  只在后半行找断点，一个词比半行还长时才在词中间断。断点处的空格不放到下一行开头。
- **悬挂缩进**：续行缩进到这一行正文的起点，也就是前导空格 + 列表标记之后。
  - 列表标记包括 `⏺ ⎿ ❯ • - *`、`2.`、`3)`。
  - 缩进里的竖框线 `│ ┃ ║` 会再画一次，缩进格保留这一行的背景色。
  - 最多缩进半行。
- **填充不折行**：一行剩下的部分只是填充时，到手机边缘就停，不再折成几行虚线或空色块。填充指：
  - 任何背景色的空格；
  - 横跨窗格的分隔线：`─ ━ ═ ╌ ┄` 等，例如 Claude 输入框上下的线、权限框里的 `╌`；
  - Claude 给用户消息铺的灰底（48;2;55;55;55）。
- 光标所在行不截，打字的位置要能看到。

## 九、herdr 重画宽字符的坑（「字消失」）

herdr 增量帧里，流式输出的每个中文字都单独用 CUP 定位，常常写在原来同一个宽字符的位置上（`ESC[35;7H无`）。
原来的写法先写左半，再写右半（宽 0 的占位格）。写右半时触发了「覆盖宽字符的右半就清掉它的左半」规则，刚写的字被清掉了。
tab4 截图里整段中文缺字、看起来字距大，根源在这里。

现在右半格单独写（`putRightHalf`），只清掉被它覆盖的下一个宽字符。验证：两份 lab 窗格的真实录制，回放结果和
`herdr pane read` 逐行一致（`terminal-observe-mixed` / `-permission` 的 `.jsonl` + `.txt`）。

## 十、实测（2026-10-29，lab 窗格 w9:p5，Claude 2.1.312）

| 项 | 数值 | 怎么量 |
| --- | --- | --- |
| 回显（发键 → 带着这个字的帧到达） | 中位 37 ms，最快约 20 ms，偶发 150–175 ms（两轮各 12 次） | 本机通过线上 hub：`POST /keys`（keep-alive）后，在 WebSocket 的终端流里等到含这个字的帧。链路是本机 → hub → node（就是本机）→ herdr → observe → hub → 本机。手机要再加自己的网络往返和一帧渲染 |
| herdr 本身 | 45–48 ms | tab4：`pane send-text` 后轮询 `pane read`（CLI 每次起进程，比流慢） |
| 流式帧率 | 输出期间帧间隔中位 113 ms、p95 239 ms，约 9 帧/秒；整段含思考停顿平均 6 帧/秒 | 让 Claude 写 500 字，记录每帧到达时间 |
| 帧大小 | 中位 117 字节，最大 18 KB（整屏） | 同上 |
| 手机处理一帧（解析 + 折行 + 画） | 中位 11.8 ms，p95 16 ms，首个整帧 44 ms | `TerminalFrameCostTests`，模拟器 Debug 构建，录制的 76 帧 |
| 键盘弹起时可见行数 | 约 20 行（不弹键盘约 27 行） | iPhone 17 Pro 模拟器，13pt |

### 流式帧间隔拆解（同日追加）

帧间隔中位 113 ms 的来源，逐段量过：

| 段 | 结果 | 怎么量 |
| --- | --- | --- |
| Claude Code 自己的重画节奏 | 流式时约每 105–125 ms 一帧。几乎每帧只有转圈和计时那一行在变，正文分几次大块整屏重画（约 20 KB、800 多个汉字一帧） | 本机 observe，逐帧看内容 |
| herdr observe | 屏幕变得快时每 16–19 ms 出一帧（约 60 帧/秒），不是瓶颈；也没有可调参数（只有 `--cols/--rows`） | 在 shell 窗格里每 10 ms 打一行，本机 observe 计时 |
| node → hub（SSH） | 单跳约 3 ms（`ssh -T` 往返两跳，中位 5.6 ms；20 KB 一行中位 12.5 ms） | 本机经 hub 的 `ssh -T` 绕回本机，同一个时钟 |
| 本机到 hub 的网络 | RTT 约 4 ms | ping |
| 从 herdr 出帧到客户端收到，全程 | 单程中位 24 ms，p90 78 ms；两边帧数一样（67 帧对 67 帧），中间不丢帧也不攒帧 | 本机 observe 和经 hub 的 WebSocket 同时收、按顺序配对 |

全程 24 ms 的组成：
- 两个独立 observer 的出帧相位差：0–16 ms。这是测法带来的，真实链路上只有 node 一个 observer。
- SSH 约 3 ms，网络约 2 ms。
- 其余是 hub 的 JSON 处理和 WebSocket，几毫秒。

结论：113 ms 是 Claude Code 自己的重画节奏，node、hub、WebSocket 都没有攒帧或节流，所以对 Claude 调不到 30 ms 一帧。
能调的是另一头：屏幕变得快的窗格（Codex 一类 60 帧/秒重画的 TUI、`!` 跑的长日志、vim 翻页），herdr 每 16 ms 就出一帧，
原来原样转发，超过手机每帧约 12 ms 的处理能力。现在 node 加了合帧窗口 `termMinGap = 30 ms`：
- 安静之后的第一帧立刻发（打字回显不等）。
- 30 ms 内跟着来的帧合成一帧：diff 的字节接起来，整帧丢掉它前面的 diff。等满 30 ms 再发。
- 流结束时把攒着的发掉。
- 空闲时什么都不多发。Claude 约 110 ms 一帧的节奏不受影响。

部署 #48 以后复测（同日 16:20 以后，lab 窗格 w9:p5）：
- 回显：四轮中位 44 / 35 / 30 / 36 ms，和部署前（37 ms）一样，安静之后的第一帧没有被合帧拖慢。44 那一轮是 node 刚重启后跑的。
- Claude 流式：直接看 herdr 中位 110 ms，经 hub 中位 112 ms。71 帧到客户端是 69 帧，有两次 30 ms 内的连帧被合成一帧，节奏和以前一样。
- Claude 里用 `!` 跑每 5 ms 打一行的循环：帧间隔中位 101 / 107 ms。Claude 显示命令输出也按自己约 100 ms 的节奏重画，到不了 herdr 的 60 帧/秒。
- 所以手头的会话（都是 Claude）里，没有一个能把合帧窗口真正用满。合帧的正确性由 `TestFrameGateMergesBurstsOnly` 保证；
  等有每秒几十帧重画的 Agent（Codex 一类）时，再用 `/tmp` 下的测量办法（本机 observe 对比经 hub 的 WebSocket）复核。

## 十一、手机接管：真正的手机尺寸（2026-10-29 用户拍板方案 A）

镜像窗格时 Claude Code 以为终端 172 列宽，按电脑排版，手机再折行，体验不对。现在打开终端 tab 时手机**接管**终端：
窗格里的程序看到的是手机按当前字号算出的列 × 行，Claude 按手机宽度排版（权限框、diff、spinner 都是它自己按手机宽度画的），
键直接写进终端。设置 › 终端尺寸可以切回「电脑尺寸」（镜像，第二节）。

### herdr 给什么（实测，herdr 0.9.3）

- `herdr terminal session control <pane> --cols C --rows R`：不要 PTY，stdin / stdout 走管道，**和 `terminal attach` 共用一个槽位**。
  开起来窗格立刻变成 C × R（`pane get` 的 `viewport_rows`、Claude 的分隔线宽度都变了），stdout 是和 observe 一样的 `terminal.frame`。
  stdin 一行一个 JSON：`{"type":"terminal.input","text":"…"}`（或 `bytes`：base64），`{"type":"terminal.resize","cols":C,"rows":R}`，`{"type":"terminal.release"}`。
- **怎么断都会自动恢复**：stdin 关掉（EOF）、`kill -TERM`、`kill -9`、`release`，herdr 都把窗格放回原来的尺寸（172×45），Claude 重画回电脑宽度。
  所以 node 崩了、hub 崩了（node 是它的子进程，stdin 跟着断）都不会把窗格卡在手机宽度。
- 槽位被占着（Ovis、另一个 herdr 客户端）：不带 `--takeover` 时立刻 `{"type":"terminal.closed","reason":"terminal attach failed: … already has an attached client; retry with --takeover"}`；
  带 `--takeover` 抢过来，原来那个收到 `terminal attach taken over` 后退出。我们被别人抢走时也是收到这个 reason。
- resize 后约 6 ms 来一个整帧；**同尺寸的 resize 也回一个整帧**，而程序收不到 SIGWINCH（尺寸没变）：node 用它在增量攒太多（256 KB）时要一个新整帧，
  不用像 observe 那样重开（重开会让窗格先回电脑尺寸再变回来，Claude 重画两次）。
- `terminal attach` 的输出也是 herdr 重画的网格（`ESC[?1049h` 备用屏 + 逐格 CUP），**herdr 不给程序的原始字节**。所以「换 SwiftTerm 吃原始字节流」不成立：
  SwiftTerm 收到的也是原地重画，不会自己长出滚动历史；手机上现有的 VT 子集已经和 `herdr pane read` 逐行一致，就不引入新的 Swift 包了（也就不用补 `/tmp/jarvis-spm` 镜像）。
- **接管期间 herdr 没有滚动历史**：`pane read --source recent` 只返回当前一屏（31 行），`max_offset_from_bottom` 一直是 0，Claude 写了 60 行也一样；
  放回电脑尺寸后，原来 172 列的历史又回来了。

### 链路

```
App ──terminal_subscribe {control:{cols,rows}, takeover?}──▶ hub ──terminal.watch {control}──▶ node ──session control──▶ herdr
App ──terminal_input {text, control?}────────────────────▶ hub ──terminal.input / terminal.resize（事件，保序）──▶ node ──stdin──▶ herdr
App ◀──terminal（帧，同镜像）、terminal_control {state}──── hub ◀──terminal.frame / terminal.end {control, taken}── node
```

- 键走 WebSocket → hub → node 的**事件**（RPC 的 method 在对端是并发处理的，事件按到达顺序处理），node 写进 control 进程的 stdin，不再每批起一次 `herdr pane send-keys`。
  App 把键名换成终端字节（`TerminalKeyBytes`：回车 `\r`，方向键 `CSI A–D`、带修饰 `CSI 1;m X`，ctrl+字母是控制字符，alt 是前缀 ESC，shift+enter 是 `ESC \r`），
  多行粘贴包成 bracketed paste（`ESC[200~ … ESC[201~`）。
- 尺寸：终端视图按字号算出能放下的列 × 行（`onGrid`），第一次立即用（订阅要等它），之后键盘起落、旋转、捏合字号都防抖 250 ms 再发（`terminal_input` 带 `control`）。
- 一个终端只有一个「驾驶」的设备：最新订阅带 control 的那台接管，之前那台收到 `taken`、改为只看。别的设备照样能看（看到的是手机尺寸的帧）。
- hub 对每个会话的 watch / unwatch 一次一个（`callLock`），每次按当时想要的状态问 node，最后一次就是 hub 要的样子。
- 放手：App 离开终端 tab / 进后台 / 被杀 → `terminal_unsubscribe` 或 WebSocket 断开 → hub 没人驾驶了：还有人看就改回镜像，没人看就 `terminal.unwatch` → node 关 stdin → herdr 恢复。
  手机直接没了（不断开 TCP）：hub 的 WebSocket ping（20 s 一次、15 s 超时）断掉它。node 侧再有个租约：hub 每 20 s 发 `terminal.keepalive`，75 s 没收到就自己放手。
- 槽位被占：node 回 `attach_busy`，hub 告诉 App `busy` 并先镜像着；App 弹窗「电脑上另一个程序正在使用这个终端」→「接管」重新订阅带 `takeover`，「只看」就镜像。
  接管中途被抢走：node 发 `terminal.end {control, taken}`，hub 告诉 App `taken` 并改回镜像，App 显示「只看 · 接管」按钮可以抢回来。
- 旧 node（不认 `control`，照样镜像、回包里没有 `control: true`）/ 旧 herdr（没有 `session control`）：hub 告诉 App `unsupported`，App 镜像。旧 hub 没有 `terminal_control` feature，App 不发 control。

### 手机上的滚动历史

herdr 接管期间没有历史，帧又是原地重画，所以历史只能手机自己攒：每帧前后比一下网格，新屏顶上的几行等于旧屏第 k 行起的那几行，
就是往上滚了 k 行，旧屏顶上 k 行进历史（`TerminalGrid.scrolled`：至少对上剩下的三分之一、其中至少两行不是空行或分隔线，因为底部的 spinner 和输入框一直在变）。
一帧里滚过整屏的部分找不回来（herdr 那边也没有），完整的过程在「对话」里。
接管前的内容不带过来（以前读一次电脑宽度的窗格历史放在最上面，手机上折得没法看，已去掉）：往上滑看到的就是接管以后终端打出来的东西，见第十二节。
真实帧验证：`terminal-control-scroll.jsonl`（44×30 时 Claude 写 60 行）回放后历史 + 屏幕正好是 1. 到 60. 各一次，逐帧、按 node 的 30 ms 合帧、按 250 ms 粗合帧都对（`TerminalControlTests`）。

### 实测（本机测试 hub → 本机 node → lab 窗格 w9:p5，部署前）

| 项 | 结果 |
| --- | --- |
| 接管耗时（订阅 → `active`） | 25 ms（node 起 control 到第一帧约 70 ms，含在 watch 里） |
| 回显（发键 → 带这个字的帧） | 接管后经 WebSocket 中位 24 ms、最慢 37 ms；同一条链路上走 `POST /keys` 中位 23 ms、最慢 122 ms。本机没有网络，差别要部署后经线上 hub 再量 |
| node 处直接写 stdin 到出帧 | 13–23 ms（herdr 每 16 ms 出一帧是下限） |
| 恢复 | 离开 tab 立即；App 进后台约 5 s；杀 App 约 2 s；手机不断开连接地消失 27 s（hub ping 超时）；hub `kill -9` 0.1 s；node `kill -9` 立即 |
| 被占 / 抢占 | 占着时 `busy`、镜像着看；`takeover` 后原客户端退出、窗格变手机尺寸 |

部署 c6bec7b 以后经线上 hub 复测（同日 10:50 以后，本机 macstudio 的 lab 窗格 w9:p5，各两轮、每轮 12 次）：

| 项 | 结果 |
| --- | --- |
| 接管耗时（订阅 → `active`） | 75 / 95 ms |
| 回显，接管后经 WebSocket 打字 | 中位 46 / 40 ms，最慢 80 / 83 ms |
| 回显，`POST /keys`（keep-alive） | 中位 44 / 47 ms，最慢 157 / 147 ms |

中位差不多（大头是 herdr 16 ms 一帧的出帧节奏和 hub 来回），慢的那头减半：打字不再排在一个个 HTTP 请求和 herdr 进程后面。
本机到 hub 是局域网、连接常驻；手机上蜂窝网络、请求要重新排队时差别会更大，等 TestFlight 后真机再量。

herdr 版本：六台机器（0.9.5 一台，0.9.3 五台）都有 `terminal session control`，参数一样（`--takeover`、`--cols`、`--rows`）。
0.9.5 上按窗格原尺寸开一次 control（不改尺寸、不抢槽位）：整帧、同尺寸 resize 回整帧、关 stdin 得 `detached`，和 0.9.3 一致；
接管改尺寸、恢复、被占和抢占的完整测试都在 0.9.3（本机）上做过。没有 `session control` 的 herdr：node 照样 observe 并回 `control: false`，
App 收到 `unsupported`，只镜像（`TestTerminalControlOnAnOldNodeOnlyWatches`）。

## 十二、往上滑看到什么（2026-10-29 晚，用户拍板）

用户要的是：终端就是手机尺寸的 pty，往上滑就是在这个终端里往上翻；Claude Code 用默认模式，不配环境变量、不开 fullscreen，
**改尺寸 / resume 时历史被清掉是正常的、符合预期**。所以往上滑看到的是接管以后终端打出来的内容（手机宽度），接管前的不带过来，也不另外拼对话记录。

实测（lab 窗格，herdr 0.9.3，Claude 2.1.312，接管成 45×30）：

| 情况 | herdr 的滚动历史 | `terminal.scroll {lines, direction}`（herdr 的滚轮） |
| --- | --- | --- |
| shell（`seq 1 100`） | 正常增长（579 → 680 行） | 滚 herdr 自己的历史，帧里就是滚过去的画面（offset 0 → 20 → 0） |
| Claude 默认模式，回复 70 行 | **一直是 0**（`pane read recent` 只有一屏 31 行） | 没东西可滚，画面不变 |
| Claude fullscreen 模式（`"tui": "fullscreen"` 或 `CLAUDE_CODE_NO_FLICKER=1`） | 0（备用屏） | 转给 Claude，它在自己的界面里按 45 列往回翻；PageUp 46 次翻到会话开头。SGR 滚轮字节 `ESC[<64;x;yM` 也行 |

另外：Claude Code 改尺寸时清掉终端的滚动历史、只重画当前屏（主会话实测），所以接管过的窗格回到电脑上也只剩一屏左右，对话本身不受影响。
herdr 自己改尺寸时不丢历史（shell 窗格接管前后都是 300 行）。

**更正（2026-10-30）**：同一个 lab 的 Claude（同一进程）后来就接滚轮了——用户在电脑的 herdr 里往上滚，Claude 自己画出钉在顶上的提问和「Jump to bottom (click) ↓」，
手机这边经 herdr 的 `terminal.scroll` 也一样能滚它。Claude 2.1.312 有个「fullscreen trial」，不用用户配置就会切到全屏渲染（默认 profile 的 settings.json 里也早有 `"tui": "fullscreen"`），
上面 23:00 那次没反应的原因没查清。Moshi 不用任何配置就能滚，是因为它里面跑的是 herdr 客户端：herdr 客户端一直接管鼠标，滚轮给 herdr，
herdr 看程序有没有开鼠标上报，开了（Claude 全屏渲染）就转给程序、它自己往回画对话，没开（shell、经典渲染的 Claude）就滚 herdr 自己的历史。
所以手机照 Moshi 做：接管时手指上下滑 = 滚轮，经 hub（`terminal_input.scroll`）→ node → herdr 的 `terminal.scroll`，由 herdr 决定给谁（feature `terminal_scroll`；旧 hub / node 没有就只滚手机自己的）。

接管以前（2026-10-29 晚）手机上的历史是手机自己攒的：每帧比对网格，滚出屏幕顶的行留在手机上（第十一节「手机上的滚动历史」，真实帧回放验证过）。
现在接管时手指滑动一律是 herdr 的滚轮（上一段），手机自己攒的历史只在不接管（镜像）时用；滚轮落在哪一格、一次发几个事件见第十六节。

试过、没采用：在手机上按 Claude 的样式把 transcript 排出来接在上面（分支 `lane/terminal-transcript`，`TerminalHistory.swift`，带脱敏长会话夹具和接缝对齐）。
用户不要：终端里就该只有终端打出来的东西。

## 十四、终端从底部弹出，键栏照 Moshi（2026-10-30，用户要求）

- 原来终端是会话页的一个选项卡，上面压着页头（标题、同伴条、选项卡）。页头随滚动 / 键盘收起展开时终端的高度跟着变，**接管时这就是给窗格改尺寸**，Claude 每次都重排。
- 现在点「终端」从底部弹出盖住整页（`TerminalCover`，fullScreenCover + 透明背景）：顶上只有一条细栏（尺寸、把手、设置），按住往下拖跟手，
  拖过 140pt 或预计落点过 420pt 就关，否则弹回；点把手也关（「关闭终端」，测试用 `terminal-close`）。终端尺寸只随键盘变。实测 47×35（原来在选项卡里 45×29）。
- 键栏照 Moshi 的 Toolbar（量过它设置页预览的尺寸）：一条玻璃胶囊（`TerminalKeyBar`）高 44pt、左右 5 / 上下 4 内边距，键 36pt、间距 6，
  Ctrl / Esc / Tab 用 JetBrains Mono，其余是 SF Symbols 图标（Moshi 用 Lucide，没引入）；双击键盘键 = 回车。原来是一排 40pt 的玻璃圆键。
- 会话页顶栏不再有「对话 / 终端 / Git / 文件」选项卡：终端是右上角的按钮，文件 / Git 往左划或 ··· 菜单（`docs/ios.md`）。
- 键盘弹起时终端让出行数（`testDrivingTheTerminalSendsItsBytes`）。注意：模拟器会被 XCUITest 的打字切成「硬件键盘」模式，之后软键盘不再出现、终端不让行，
  这个测试就失败（干净的 master 上也一样）；没有 Simulator.app 时改不回来，用 `scripts/app/sim-boot.sh "<新名字>"` 新建一台跑。

## 十五、点终端 = 点在终端里；键盘只从键栏的按钮出来（2026-10-30，用户要求）

- Claude Code 的全屏界面里很多地方能点：顶上钉住的提问（点了跳到那条）、「Jump to bottom」（点了到底）。以前手机上点终端一律弹键盘。
- 现在照 Moshi：接管中点终端 = 在那一格左键单击（`terminal_input.click` → hub → node `terminal.click` → herdr `terminal.mouse` 按下 + 松开，列和行从 0 起；
  实测点「Jump to bottom」Claude 就回到底部）。链接照旧打开、双击照旧粘贴；键盘只从键栏右边的键盘按钮出来（双击它是回车），不会误触弹键盘。feature `terminal_click`。
- 默认字号 15（Moshi 在 6.9 寸屏上一行约 46 个字；原来 13 是 56 个），键栏的键平时只有字和图标，按住 / 锁定时才有底色（同 Moshi）。
- 选择 / 拷贝（同 Moshi）：长按选一个词，不松手拖动扩选；松手后两端有系统样式的把手（`SelectionHandle`：一根线 + 起点上方 / 终点下方的圆点，44pt 宽的触摸区），
  拖哪个把手就移哪一端，菜单（拷贝 / 全选）在拖完后回来，摆在选区旁边不盖住它（`editMenuInteraction(_:targetRectFor:)` 返回整个选区）。
  从把手开始的拖动既不滚视图、也不当成终端的滚轮（`gestureRecognizerShouldBegin`）。


## 十六、Codex 往上滑滚不动：滚轮要落在一格上、一行一个事件（2026-10-30，用户反馈）

用户：手机接管 Codex 的终端时往上滑完全滚不动，Claude 的能滚。实测（一台 Linux 开发机：herdr 0.9.3、Codex 0.163.0；hub 机器：herdr 0.9.5、同一个 Codex，
lab 窗格接管成 45×30；Codex 的配置一律用 `-c` 临时覆盖，没改用户的 config）：

**Codex 是什么样的终端程序**（`script` 录下的原始输出）：开备用屏（`?1049h`）、开鼠标上报（`?1000h ?1002h ?1003h ?1006h`，SGR 编码），
关掉 alternate scroll（`?1007l`）。它的主界面本身就是能滚的对话：滚上去以后底部出现「↓ Back to bottom · esc」，`enter/esc` 回到最新。
**发过一轮对话以后，它把这一轮的提问钉在屏幕第 0 行**（和 Claude 全屏渲染钉在顶上的提问一样），下面是对话，最底下 5–6 行是输入框和状态栏。
没有单独的 transcript 浮层：Ctrl+T 只是把折叠的工具输出（「… +71 lines (^t to view transcript)」）原地展开。

| 往 Codex 发什么（接管中） | 结果 |
| --- | --- |
| `terminal.scroll {lines, direction}`（我们原来发的） | **不动**（0.9.3，有钉住的提问时）。0.9.5 那台的 Codex 只跑过 `!` 命令、没有钉住的提问，所以能滚 1 行 |
| SGR 滚轮 `ESC[<64;x;yM`，落在对话区（第 1–23 行） | 每个事件滚 1 行 |
| 同上，落在第 0 行（钉住的提问）或输入框 / 状态栏 | 不动 |
| PageUp / PageDown | 翻约一屏（22 行），能回来 |
| Shift+↑ / Shift+↓ | 不是滚动（输入框的历史 / 编辑） |

**herdr 的 `terminal.scroll` 实际做什么**（0.9.3、0.9.5 完全一样，用一个只记录收到哪些字节的小程序验证）：

- 程序开了鼠标上报（1000/1002/1003 任一，带不带 1006）：**一条命令就是一个滚轮事件，不管 `lines` 是几**；位置是命令里的 `column` / `row`（从 0 起），
  不给就是左上角 1;1。另有 `modifiers`（数字，shift = 1 → `ESC[<68;…`）。
- 备用屏 + 1007（alternate scroll）没开鼠标上报：一条命令一个 ↑ / ↓。
- 都没有（shell）：滚 herdr 自己的历史，`lines` 有效，N 条 1 行 = 1 条 N 行；位置不起作用。

所以原因有两层：herdr 不给位置时滚轮落在 1;1，正好是 Codex 钉住提问的那一行，它不接；就算落对了地方，手机滑过 N 行也只发出一个事件，Codex 只动 1 行。
Claude 能滚是因为它在哪一行都接滚轮（第 0 行、输入框上都行），而且有加速（1 / 2 / 4 / 8 个事件分别滚 0 / 1 / 3 / 10 行）。

**改法**（照真终端 / Moshi：Ghostty 在开了鼠标上报的程序上，手指每滑过一行发一个滚轮事件，落在手指下面那一格）：

- App：拖动开始时记下手指按下的那一格（`TerminalScrollView.wheelAt`，屏幕坐标、从 0 起），这一次拖动和松手后的惯性都用它；
  `terminal_input` 带 `at: [column, row]`。
- hub：原样转给 node（`terminal.scroll {session_id, lines, up, column, row}`），App 没说位置就不带。
- node（`herdr.Control.Scroll`）：`lines` 拆成 N 条 `lines: 1` 的命令，都落在那一格；没有位置（旧 App、旧 hub）就落在屏幕正中间，绝不是 1;1。
  所以 **TestFlight 28 这样的旧 App 只要部署 hub / node 就能滚 Codex**，新 App 再加上「落在手指下面」。每次最多 120 个事件。
- 验证：`TestLiveControlWheel`（`-tags herdrlive`，`JARVIS_LIVE_WHEEL_PANE=<窗格>`）用 node 的代码接管真窗格、往上 5 行、屏幕要变，再往下 5 行、要回到原样；
  那台开发机上钉着提问的 Codex（0.9.3）、hub 机器上的 Codex（0.9.5）、本机 lab 的 Claude 都过。

还剩的差别（和 Moshi、真终端一样）：手指从 Codex 的输入框 / 状态栏或钉住的提问上开始滑，Codex 不滚（它只在对话区接滚轮）；Claude 在哪都滚。

## 十七、慢链路上接管很卡（2026-11-01，用户反馈）

用户：一台经 hub 机器本机 SOCKS5 代理接入的开发机（`docs/server.md` NAS 一节第 7 步），接管它的终端后滑动和操作都很卡。

**链路实测**（在 hub 机器本机请求 hub，不含手机到 hub 这一段；`fs.list` 一个小目录代表 hub → node → hub 的往返，读一个文件的前 512 KB 代表吞吐）：

| 机器 | 往返（ms，n=11） | 吞吐（KB/s，n=5，不够 10 个，不作基线） |
| --- | --- | --- |
| 经代理的开发机 | min 158 / p50 159 / p90 198 / max 210 / mean 167 | min 75 / p50 520 / max 626 / mean 443 |
| 同一局域网的 Mac（jarvis-link） | min 6 / p50 7 / p90 9 / max 10 / mean 8 | min 1477 / p50 1999 / max 2432 / mean 1916 |

接管时每个按键、每一下滚动都要等这一个往返才看得到回显，这部分是链路本身，省不掉。另外三处是我们自己加的：

1. **node 发帧是同步的**：`frameGate` 拿着锁调 `peer.Notify`，它直接往 stdout 写。链路一慢写就卡住，herdr 的读取也跟着停，
   帧一帧接一帧排着队过去，手机一直在画过去的画面。30 ms 的合并只按时间算，只是把差分字节拼起来，字节数一个没少。
   现在发送在单独的 goroutine 上：正在写的时候新来的帧先合并，写完马上发合并后的；等着的差分超过一整屏（上一个整帧的大小，至少 16 KB），
   就让 herdr 重画一整帧（`Control.Redraw`：同尺寸 resize，程序看不到变化）来顶替，慢链路上传的是最新的一屏，而不是中间的每一步。
   流结束时 `flush` 先等正在写的帧写完、再发剩下的，然后才报 `terminal.end`（`TestFrameGateWhileTheLinkIsBusy`、`TestFrameGateFlushWaitsForTheLink`）。
2. **App 滑过 N 行发 N 条消息**：`TerminalWheel` 每行回调一次，`wheel` 又每行发一条 `terminal_input`。hub 和 node 本来就收 `scroll: N`，
   node 在 herdr 旁边拆成 N 个滚轮事件（第十六节）。现在一次移动只发一条。
3. **玻璃压在终端上**：玻璃风格里终端是模糊的采样源，顶栏、键栏和上下两条边缘四块模糊，终端每变一帧都要重算。
   终端页现在任何风格都用不透明面板（`LocalGlassLevel provides GlassLevel.Solid`）。终端画布也不再给默认背景上的空格画背景矩形和字形，
   一帧的绘制量只和看得见的字有关。

手机上的渲染（`dumpsys gfxinfo`）和真实会话里按键 / 滚动的往返没有实测：前者要用户的手机连着 adb，后者要往用户正在用的会话里发键。

## 十八、网页版剪贴板：粘贴图片 / 文件、复制、OSC 52（2026-11-04）

网页终端的剪贴板两个方向：往里贴（截图、文件，照 Moshi 的网页端）和往外复制（选中的文字、程序的 OSC 52）。两个方向共用 `lib/terminal-clipboard.ts`（纯逻辑）和
`session/terminal/clipboard.ts`（浏览器那一侧），提示都走页面的 toast，传文件时的状态条在终端顶部。

### 粘贴图片 / 文件

在终端里 ⌘V / Ctrl+V 粘贴截图、从文件管理器拖文件进来，文件传到**窗格所在的那台电脑**，再把路径敲进窗格，Claude Code / Codex 按路径把图片当附件读。

**流程**（`lib/terminal-clipboard.ts`、`session/terminal/clipboard.ts`、`feed.ts` 的 `uploadFiles`）：

1. 面板容器上抓 `paste`（捕获阶段）：`clipboardData.files`，没有就取 `items` 里 `kind=file` 的；拖放走 `drop`，同一条路。
   键栏的「粘贴」键先试 `navigator.clipboard.read()` 里的图片（浏览器允许时），没有再读文字。⌘V（Mac）和 Ctrl+Shift+V 会带着文件触发 `paste`；
   **Windows / Linux 上的 Ctrl+V 不会**：xterm 把它当 `^V` 发出去并取消按键，浏览器不粘贴（2026-11-05 在 #2700 里实测、读 xterm 源码确认；这一条原先写错了）。
   单窗格里这一条还是这样（用 Ctrl+Shift+V 或键栏的「粘贴」）；完整界面里 Ctrl+V 已经处理，见第二十六节。剪贴板只有文字时走原来的文字粘贴，一个字没变。
2. 只有**正在接管**时才收：只看（或被别处接管）时提示「接管终端后才能粘贴文件」，什么都不传。
3. 每个文件 `POST /api/v1/machines/{mid}/files?name=<文件名>&dest=terminal`（原始字节，`application/octet-stream`，和对话页「发送文件」、新建 Agent 的附件是同一个接口，
   `docs/protocol.md`「发送文件」；网页端请求也共用 `lib/attachments.ts` 的 `sendFile`）。hub 每 1 MB 一块转给那台电脑的 node（`file.upload.begin / chunk / end`，`begin` 带 `dest`），
   整个文件不进 hub 内存。超时 60 秒 + 每 100 KB 加 1 秒。剪贴板里的图片没有文件名时按类型起名（`image.png`、`image.jpg`……）。
4. node 存到 `<node 目录>/uploads/terminal/<YYYY-MM-DD>/<16 位随机十六进制>-<清理后的文件名>`（目录 0700、文件 0600），返回绝对路径。
   和对话里的图片附件（`uploads/<日期>`）分开放，清理不会碰对方。
5. 全部传完，页面用 xterm 的 `term.paste()` 把路径敲进窗格：`paths.map(escape).join(" ") + " "`，程序开了括号粘贴时由 xterm 自己加 `ESC[200~ … ESC[201~`。
   `escape` 在每个不是字母、数字、`_@%+=:,./-` 和非 ASCII 的字符前加反斜杠（`escapePastePath`），所以带空格、引号、`$` 的路径也是 shell 里的一个词。
   不自动回车。
6. **守卫**：传的时候用户可能已经换了会话、关了面板、被别处接管或切到只看。结束时再查一遍（`pasteVerdict`）：feed 已销毁提示「上传期间终端已切换，没有粘贴」，
   不再接管提示「上传期间不再接管终端，没有粘贴」，两种都不往窗格里敲（文件已经在电脑上了，只是不粘贴）。
7. 界面：传的时候终端顶部有「正在上传 xxx.png…」的状态条，多个文件显示「等 N 个文件」；失败、超限、守卫拦下用 toast（错误 5 秒后自己消失）；文件拖在面板上时面板描一圈强调色。

**安全（粘贴 / 拖入）**

- 没有新接口、没有新的免登录入口：还是 hub 已有的 `POST /machines/{mid}/files`，走同样的设备 token 认证。`dest` 只认 `terminal`，别的值 400。
- 文件名只当「名字」用：node 把 `/`、`\`、控制字符换成 `_`，`..` 和空名换成 `file`，超长截短；最终路径由 node 自己拼在日期目录里，客户端传不进路径。
  测试里试了 `../../../etc/passwd`、绝对路径、`..\..\x`、NUL、`a/../../b.png`，都落在当天目录里。
- 上限 50 MB（`proto.MaxTerminalUploadBytes`，hub 看 `Content-Length` 就拒，node `begin` 再拒一次；网页选完先查）。对话页的「发送文件」仍是 200 MB。
- 文件 0600、目录 0700、随机前缀：同机别的用户读不到，也猜不出文件名。没有执行位。
- 类型不设白名单：截图、PDF、日志、CSV 都是用户有意拖进来的，但文件只落盘不执行；图片类型只用来给没有扩展名的剪贴板图片起名。
- 清理：node 启动时删掉 `uploads/terminal/` 下早于 7 天的日期目录（只认名字是日期的目录，别的东西不动）。常驻的 node 不到重启不会再扫，所以最长可能多留几天。
- 旧 hub 或旧 node 不认 `dest`，直接忽略：照旧能传，文件落在系统临时目录、没有清理（旧 node 前面的 hub 是新的时，50 MB 上限仍在）。

**iOS / Android 能不能用同一个接口**：能，不用改服务端。它们已经在用 `POST /machines/{mid}/files`（对话页「发送文件」，传到电脑临时目录），
加 `dest=terminal` 就落到 Persona 的目录；返回的路径同样要按上面的规则转义后经 `terminal_input` 的 `text`（括号粘贴 `ESC[200~ … ESC[201~` 自己包）敲进窗格。
Android 还没做。

### iOS / Mac（#3083）

和网页同一条路、同样的规则（UI 测试 `TerminalFilesUITests`：按住粘贴出菜单、点「文件」弹出选择器），纯逻辑在 `apple/Persona/Views/Session/Terminal/TerminalFiles.swift`（`TerminalPaste`：转义、命名、守卫、文案；`TerminalUploads`：上传和敲路径），单测 `TerminalFilesTests`。

- **入口**：iOS 键栏的粘贴键点一下照旧粘贴文字，**按住**出菜单「粘贴文字 / 照片 / 文件」：照片图库（可多选，含视频）或文件 App。
  也可以在设置 › 键栏里打开单独的回形针键「照片 / 文件」（默认不显示：手机宽度放不下，多一个键整条键栏就要左右滑）。
  Mac 上终端里 ⌘V 粘贴 Finder 拷贝的文件、截图（剪贴板只有图片时；有文字时照旧是文字粘贴，判断同输入框的 `MacPaste`），也可以把文件拖到终端上（拖着时终端描一圈强调色，只在接管时描），键栏同样能用。
- **上传**：`APIClient.upload(…, dest: "terminal")`，一个一个传；50 MB、空文件先拦下；文件夹不传，提示「文件夹传不了，拖里面的文件进来」。
  照片图库里的 HEIC / TIFF 等转成 JPEG（`image.jpg`），PNG / JPEG / GIF / WebP 和视频原样传；没有名字的按类型起名（`image.png`、`video.mov`）。
- **敲路径**：传完的路径按 `escapePastePath` 同样的规则转义（`TerminalPaste.escape`），空格隔开、末尾一个空格，整段包成括号粘贴（`ESC[200~ … ESC[201~`，App 不知道程序开没开 `?2004`，和多行粘贴一样总是包）经 `terminal_input.text` 发出去，不回车。
- **守卫**：只在接管时收（否则提示「接管终端后才能粘贴文件」）；传的时候换了会话、关了终端，提示「上传期间终端已切换，没有粘贴」；不再接管，提示「上传期间不再接管终端，没有粘贴」。
  传的时候终端顶部有「正在上传 xxx…」的状态条，失败用 toast。

### 把文件面板的条目拖进终端 / 输入框（#2333）

照 Moshi 网页端（拖动类型 `application/x-moshi-file`，插入 `@相对路径 `）：文件面板里的文件和文件夹行可以拖，拖到终端里敲进 `@路径 `，拖到对话输入框里插到光标处。
行的样子不变，只多了拖动。纯逻辑在 `lib/entry-drag.ts`，终端那一侧是 `feed.ts` 的 `dropEntry`（放在 `drop` 里上传之前），输入框那一侧是 `Composer.tsx` 的 `dropEntry`。

1. **拖的内容**：自定义类型 `application/x-persona-file`，JSON `{mid, path, dir}`（机器 id、绝对路径、是不是文件夹）；不放 `text/plain`，所以拖到别的输入框不会粘出一条绝对路径。
2. **终端**：拖动经过时和拖入桌面文件一样描一圈强调色；放下时**不上传**，按窗格的工作目录算路径：
   - 在工作目录里：相对路径（`src/a.ts`），文件夹以 `/` 结尾（`src/`），工作目录自己是 `./`；
   - 在工作目录外、或不知道工作目录：绝对路径；
   - 文件在另一台电脑上：提示「这个文件在另一台电脑上，不能拖进这个终端」，什么都不敲；
   - 只在接管时敲，否则提示和上传一样的「接管终端后才能粘贴文件」（只看时也接受放下，为了能说明原因，而不是显示一个禁止光标）；
   - 路径经 `escapePastePath` 转义（和粘贴文件同一个函数，带空格、括号、`$` 的路径是 shell 里的一个词），前面加 `@`、后面加一个空格，用 `term.paste()` 敲进去（程序开了括号粘贴时由 xterm 包 `ESC[200~ … ESC[201~`），不回车。
3. **工作目录从哪来**：独立终端页用窗格列表里的 `cwd`（herdr 报的窗格目录，8 秒刷新一次），没有就用对应会话的目录（`TerminalPanel` 的 `cwd` 属性 → `TerminalFeed.cwd`）。
4. **怎么同时看到两边**：侧边面板一次只开一个，所以独立终端页的窗格头里有「文件」按钮（宽屏才有，触屏拖不了），在终端旁边打开这个窗格目录的文件面板。
   文件面板开着的时候换窗格，面板留在原来的目录，所以会出现「窗格目录之外」「另一台电脑」的情况，就是上面的第二、三条。
5. **输入框**：同一个拖动，`@路径 ` 插到光标处（前面需要时补一个空格），和 @ 菜单一样相对会话目录、不转义（输入框里不是 shell；带空格的路径 Claude Code 读不成一个 `@`，菜单也是这样，没有改）；另一台电脑的文件提示不能引用。

端到端检查：`scripts/test/web_files_drag_e2e.py`（假 hub 带 `MOCK_TERMINAL_STREAM=1 MOCK_TERMINAL_CONTROL=1 MOCK_TERMINAL_PANES=1`，用法见脚本开头），断言假 hub 收到的 `terminal_input`；单元测试 `web/src/lib/entry-drag.test.ts`。

### 复制、OSC 52、输入法、字体

复制的起因：Moshi 的浏览器端在用户这台机器上复制 herdr 里的内容是通的。原因是它本身就是一个真 herdr 客户端（herdr 认为自己在远程时发 OSC 52，
用户在 systemd drop-in 里用 `SSH_CONNECTION` 强制了这一点，否则 herdr 在电脑上调 `wl-copy`），页面里的 `@xterm/addon-clipboard` 收到后写 `navigator.clipboard`。
Persona 网页看的是 observe / control 流，「实测」一节是它们给不给这些东西。网页版做了：

- **选中文字复制**（`lib/terminal.ts` 的 `keyDecision`，`lib/terminal-clipboard.ts` 的 `copyWith`、`copyNotice`，`terminal/clipboard.ts`）：有选中时 ⌃C / ⌘C / ⌃⇧C 复制，没有选中时 ^C 照旧发给程序；
  复制成功后清掉选中（否则下一次 ⌃C 还是复制，打断不了程序）。先 `navigator.clipboard.writeText`，被拒绝或页面不是 HTTPS 时退回隐藏 textarea + `execCommand("copy")`，并在提示里说明
  「这个页面不是 HTTPS，浏览器没开放剪贴板接口，用了兼容方式」；两条路都不行就说没复制上。键栏有「复制选中的文字」键（有选中时可点）。
  触屏上 xterm.js 没有拖选，手机上要选文字目前靠换行模式（只看时页面自己画屏幕，可以用系统的长按选择）。
- **OSC 52**：载入 `@xterm/addon-clipboard` 0.2.0（对应 `@xterm/xterm` 6.0.0，固定版本）。程序写剪贴板时直接写；浏览器要手势（Safari、Firefox）写不进去就弹提示「终端里的程序要复制一段文字」加「复制」按钮。
  **读查询（`OSC 52;c;?`）一律拒绝**：provider 的 `readText` 返回空串，程序读不到用户的剪贴板（回复是空剪贴板）。空内容或超过 1 MiB 的写入忽略。
  现在这条路在 herdr 的流里走不到（下面「实测」），先备着，herdr 或别的路径哪天透传了就能用。
- **输入法**：`isComposing` 或 `keyCode === 229` 时页面的按键处理（shift+enter、复制）一律放手，交给 xterm 自己的输入法处理。
- **字体**：打开 xterm 前等 `document.fonts.load("<字号>px <字体栈>", "W中")`，最多 1.5 秒，避免字体晚到、格子量错。`--font-mono` 的 JetBrains Mono 现在没有 `@font-face`，是本机字体，
  所以今天这一步几乎立即返回；页面以后带网页字体时才有实际作用。
- **Shift+Enter → `ESC CR`** 原来就有（`encodeKey` 和 `feed.ts` 的按键处理），这次只是挪进 `keyDecision` 并加了输入法的例外。

### 实测：herdr 的三种流里都没有程序的 OSC 52，也没有颜色查询

用一个新建的临时标签页（`herdr tab create --no-focus`，测完 `tab close`；没碰任何已有窗格），herdr 服务端进程环境里有 `SSH_CONNECTION`，
在窗格里跑 `printf '\033]52;c;<base64>\007'` 和一个发查询、读回复的小脚本，同时从三条路收字节：

| 流 | 程序的 OSC 52 | `OSC 10;?` / `CSI ? 996 n` 这类查询 |
| --- | --- | --- |
| `session observe`（带 `--cols/--rows`） | 没有（流里的 OSC 只有 OSC 8） | 没有 |
| `session control --takeover`（Persona 接管用的） | 没有 | 没有 |
| `terminal attach <terminal_id> --takeover`（PTY 里读） | 没有 | 没有 |

三条路都是 herdr 把窗格网格**重画**出来的 VT，不是程序的原始输出。herdr 内部（基于 libghostty-vt）收到程序的 OSC 52 后走自己的「剪贴板写」
（`src/pane.rs` 里的 `OSC 52 clipboard write` 队列、`src/selection.rs` 的 `write_osc52_bytes`），只会发给完整的 herdr TUI 客户端（`herdr` / `herdr session attach`，Moshi 就是这种），
不进窗格级别的流。

没验证的一条：完整 TUI 客户端（`herdr session attach`）是否一定带出 OSC 52。要 attach 到用户正在用的会话，会多一个客户端，可能影响布局和焦点，没有做。
**（2026-11-05 更新：在自己建的临时会话里量过了，带出；见第二十四节。）**

### herdr 自己的复制能不能送到网页

不能，现在 herdr 0.9.5（协议 22）没有口子：

- `herdr api schema --json` 里没有 clipboard / OSC 相关的请求、事件或订阅；复制相关只有 `pane.copy_motion`、`pane.copy_search`（复制模式里移动光标和搜索，不取文字）。
- 事件只有 `layout_updated`、`pane_*`、`tab_*`、`workspace_*`、`worktree_*`、`pane_output_changed`（不带内容）。
- 可能的做法是让 node 作为一个隐藏的完整 herdr 客户端（PTY 里 `herdr session attach`）常驻、解析它的输出里的 OSC 52。代价大：多一个客户端的尺寸和焦点、占资源，也要用户那边设 `SSH_CONNECTION`。这次不做。
  如果 herdr 以后加「剪贴板」事件，再接：node 订阅 → hub 转发 → 网页弹「已复制到剪贴板」+ 「复制」按钮（浏览器要手势）。网页这边的按钮已经有（程序 OSC 52 到达时用，见上）。
  **（2026-11-05：第二十四节做了这条路——「完整界面」就是 node 在 PTY 里跑的完整客户端，OSC 52 在那里走通；单窗格的流里依旧没有。）**

### 颜色查询：没做，因为到不了浏览器

实测里 `CSI ? 996 n` 得到 `CSI ? 997 ; 1 n`、`OSC 10;?` 和 `OSC 11;?` 得到 `rgb:c8c8/c8c8/cece` 和 `rgb:1b1b/1e1e/1c1c`：**herdr 自己回答了**，用的是 herdr 主题的颜色，
查询本身不在三种流里（所以浏览器的 xterm 永远看不到），control 开着和没开都一样。后果：

- 网页要回答这些查询没有输入，写了也是死代码；就算哪天透传了，herdr 已经回了一份，网页再回会让程序多收到一段无人要的输入（出现在 shell 或 Claude 的输入框里）。
- 程序看到的深浅色是 herdr 主题的，不是网页当前的深浅色。网页切到浅色，Claude Code 等仍认为终端是深色。要改只能在电脑上改 herdr 的主题，或等 herdr 提供按客户端报告颜色的方式。
- DEC 2031（颜色方案变化通知）同理，没有人收。

没验证：herdr 在 DEC 2031 打开后，主题变化时会不会主动发 `CSI ? 997 ; n n`（只测了一问一答）。

## 十九、独立终端：不开 Agent 也能开终端（2026-11-04，网页）

起因：用户问「Persona 没有终端功能吗？」。终端一直有，但只在 Agent 会话的侧边面板里（`sessionCan(s, "terminal")`），
没有 Agent 的普通 shell 窗格、一台电脑上新开的 shell、herdr 里的空间 / 标签页 / 窗格怎么摆，都看不到。Moshi 的网页端有「机器 → herdr 工作区 → 标签页 → 窗格」的树，
任何窗格都能开成终端、能新开标签页。这里照着做，**复用同一条终端流和同一个 `TerminalPanel`，不另写终端**。

### 窗格地址：`pane:<窗格 id>`

终端流（第二、十一节）原来按会话 id 找窗格（`paneFor`：`agents[id].PaneID`）。没有 Agent 的窗格没有会话 id，所以流的 id 多一种写法：
`pane:w1:p2`（herdr 的窗格 id 前面加 `pane:`；别的宿主的窗格已经带自己的前缀，如 `pane:orca:…`）。**只有三个终端调用认这个地址**：
`terminal.watch` / `terminal.unwatch`（含接管、键、滚轮、点击、keepalive，它们按流的 id 找，不管是会话还是地址）、`session.screen` 和 `session.screen.before`
（没有实时流的老 node / herdr 退回轮询读屏、往上翻历史）。别的 `session.*`（`send`、`interrupt`、`keys`……）仍然只认会话 id：
**不能借地址往 shell 里发消息**（`TestPaneAddressReachesOnlyTheTerminal`）。

node 收到地址先查：窗格必须在宿主最近一次快照里（不在就拍一次再查），否则报 `pane … is not running`，不会把流指向快照以外的东西。
有 Agent 会话的窗格网页按**会话 id** 打开（和聊天页的终端面板共用 hub 的同一条流，接管 / 被抢的规则一样）；只有没有会话的窗格才用地址。

### 列出窗格：`panes.list`

不另起轮询：`lookAt`（node 每次 herdr 有变化拍的 `session.snapshot`）本来就拿到全部窗格（`Layout.Panes`，Agent 或 shell），现在连同空间 / 标签页一起留在 node 的 `layouts` 里。
`panes.list` 读这份缓存；缓存超过 3 秒（或这台宿主还没拍过）才拍一次。回包按宿主分组：
`{hosts: [{kind, stream, control, new_tab, workspaces, tabs, panes}]}`，窗格带 `agent` / `status`（宿主说的状态）/ `title` / `cwd` / `session_id`（有 Agent 会话时）/ `terminal`（上面的地址）。
hub：`GET /api/v1/machines/{mid}/panes`，feature `terminal_panes`。

### 新建终端：`panes.new`（可选，显式操作）

`POST /api/v1/machines/{mid}/panes {workspace_id, kind?}` → node 用宿主的 `CreateSlot`（herdr：`tab create --no-focus`，电脑上的视图不跳）在那个空间开一个标签页，
名字 `shell`（重名加 `-2`），目录是**空间自己的目录**（宿主报的，没有就是 home）；**请求里不能指定目录**。回 `{pane, terminal, tab_id}`，网页接着把它打开。只有用户点了空间旁边的「+」才会调用。

### 权限

窗格终端等于那台电脑上的完整 shell，所以比 Agent 会话的终端更严：

- 必须是已登录的设备令牌，没有新的免登录入口；hub 的 `allowed()` 里 `GET/POST /machines/{mid}/panes` 不在 `answer`（手表）的白名单里，扩展令牌的 `routeScopes` 里也没有（`route_scopes.txt` 记为 `-`，只有完整凭据）。
- WebSocket：`terminal_subscribe` 带 `pane:` 地址时，**只要不是 `full` 角色就拒绝，连「只看」也不行**（Agent 会话的终端 `answer` 设备仍能看，接管才要 `full`）；`panes.new` 同样只给 `full`。
- 地址也不能绕到 HTTP：`GET …/sessions/{mid}/{sid}/screen`（扩展有 `agents.read` 时可读 Agent 的屏）遇到 `pane:` 的 sid 要 `full`（`paneAddressFullOnly`）。
- 接管、被占（`attach_busy`）、被抢（`taken`）、租约（75 秒）、放手后窗格回到电脑尺寸，全部是第十一节的原样。

### 网页

- 侧边栏「终端」（`/terminal`，hub 有 `terminal_panes` 才显示）；`/terminal/<机器>/<终端 id>` 是打开着的那个，id 里的冒号编码（`pane%3Aw1%3Ap2`）。
- 宽屏：左边是机器 → 空间 → 标签页 → 窗格（Agent 带状态点，用会话自己的状态，没有会话就用宿主的 `status`；shell 是终端图标），右边是终端；窄屏列表和终端各占一页，终端头上的 × 回到列表。
  选了「整页」而不是侧边面板：侧边面板是「挨着一段对话」的东西，没有对话时它没有可挨的；整页有地址，可以收藏、前进后退。
- 列表上面的输入框是 Jump To：按标题、目录、分支、标签页、机器名过滤，↑ ↓ 选、回车打开、Esc 清空。
- 每台机器最近打开的 2 个终端保持挂着（藏起来但保持原尺寸，流不断），切回去立刻有画面（`keepRecent`）。注意它们还「接管」着窗格，电脑上的窗格保持这个网页的尺寸，直到被挤出这 2 个、点 ×、
  离开这个页面（组件卸载，`terminal_unsubscribe`）或浏览器标签页进后台（只留正在看的那个，`keepOnlyShown`）。
- 旧 hub（没有 `terminal_panes`）不显示入口；node 旧（不认 `panes.list`）时列表读不出，显示错误和「重试」。
- **应用快捷键不进程序**（#2330，`lib/shortcuts.ts`、`feed.ts` 的 `key`）：焦点在终端里时，带 ⌘（Mac）/ Ctrl（Windows、Linux）的、已登记的快捷键（⌘K 面板、⌘J 终端 ⇄ 对话、⌘B 面板……）
  由 `passesToApp` 先判断，`attachCustomKeyEventHandler` 返回 `false`，xterm 什么都不发，页面的 keydown 接手；所以 Ctrl+K 在终端里打开面板，不再给程序发 `^K`（`\x0b`）。
  没登记的键照旧进程序：单个按键、Shift+Enter、Ctrl+C / R / Z / D、Alt+B……；Mac 上 ⌃（真正的 Ctrl）从来不算 Mod，readline 的 ^K ^E ^J 都在。Windows / Linux 上 Ctrl 组合键会盖过程序里同名的键，在设置 › 快捷键里改键即可。

### 没做

- iOS 还没有独立终端（只有会话里的终端，`terminal_subscribe` 只用会话 id）。协议已经够用：同一个接口、同一个地址。Android 有了（#2972）：Agents「列表设置」里的「终端」，见 `docs/android.md`「第二批模块入口」。
- 关闭窗格 / 标签页、重命名、分屏：不在这一版，需要时在电脑上做。
- 没有会话也没有 herdr 的宿主（Paseo、botmux）：没有窗格，列表为空。
- ⌘K 搜索面板现在也搜窗格（#2330，见 `docs/web.md`「快捷键」）：打开时读一次各在线电脑的 `panes.list`，只列普通 shell 和没有会话的窗格（有会话的 Agent 窗格已经是一条会话结果）；终端页自己的 Jump To 照旧。

### 验证

`TestPanesListHasShellsAndAgents`、`TestPanesListRefreshesAStaleLayout`、`TestPaneAddressReachesOnlyTheTerminal`、`TestTerminalWatchStreamsAShellByPaneAddress`、
`TestNewShellOpensATabInTheWorkspace`（node）；`TestPaneTerminalNeedsAFullDevice`、`TestPaneScreenReadsNeedAFullDevice`、`TestNewShellIsAFullDevicesAndNamesNoDirectory`、
`TestPaneRoutesAreNotForAnswerOnlyDevices`（hub）；`web/src/lib/terminals.test.ts`。截图用 `MOCK_TERMINAL_STREAM=1 MOCK_TERMINAL_CONTROL=1 MOCK_TERMINAL_PANES=1` 的假 hub（编造的窗格树，`pane:` 地址是一个编造的 zsh）。
真 herdr 上用 `PERSONA_LIVE_PANES=1 go test -tags herdrlive -run TestLivePanes ./internal/node/`（`panes_live_test.go`）：列出它的空间 / 标签页 / 窗格（快照里确实有没有 Agent 的窗格，herdr 0.9.5），
用 `tab create --no-focus` 临时开一个标签页、按地址 `terminal.watch` 拿到帧、再关掉这个标签页；不碰已有的窗格，没有往里打字。

## 二十、网页版终端的细节：网址、右键菜单、搜索、光标选项、焦点（2026-11-05，#2331）

对照 Moshi 网页版补的一批交互，代码都在网页：`web/src/lib/terminal-details.ts`、`web/src/lib/terminal-search.ts`（纯逻辑，有测试）、`web/src/views/session/terminal/`（`feed.ts`、`FindBar.tsx`、`focus.ts`、`TerminalOptions.tsx`）、`TerminalPanel.tsx`。协议和 hub / node 没有改动。

### 能搜到什么

xterm 在网页里 `scrollback: 0`（herdr 在原地重画网格，没有东西会滚进 xterm 的历史），所以搜索分两处找：

| 状态 | 搜得到 | 怎么找 |
| --- | --- | --- |
| 接管 | 当前屏幕（面板的列 × 行） | `@xterm/addon-search`，匹配用边框标出、当前这个选中 |
| 只看（不换行） | 已加载的 herdr 历史 + 当前屏幕 | 历史是页面自己画的行，用 `findInLines` 找、整行着色；屏幕交给 addon |
| 只看（换行，页面自己画屏幕） | 已加载的历史 + 屏幕，都是页面画的行 | 全部用 `findInLines`，整行着色（xterm 此时是藏起来的） |
| 轮询（hub / node 不能推送） | 同「只看」 | 同上 |

- 复用「往上翻历史」的加载器（`loadOlder`），但搜索不会替你加载：只搜已经加载的页。滚到最上面时会像平常一样自动加载下一页，新页里的匹配接着算进总数，当前匹配不跳。
- 接管时程序自己的滚动历史（herdr 的 `terminal.scroll`、Claude / Codex 的全屏界面往回翻）搜不到：这些内容不在网页里，网页只看得到当前这一屏。要搜更早的输出，切到「只看」再加载历史（herdr 只保留约 1000 行）。
- 顺序：历史在前、屏幕在后，Enter 向下一个、Shift+Enter 向上一个，首尾相接。刚输入搜索词时落在最靠下的匹配（最新的输出）。
- 历史里的匹配是整行着色，不是逐字高亮（`DrawnRows` 按行画，没有拆 run）。
- 快捷键：⌘F（Mac）；其他系统 Ctrl+Shift+F，没接管时 Ctrl+F 也行（接管时 ^F 是 readline 的向前一格、vim 的下一页，不能抢）。只在终端面板内有焦点时生效（面板根节点 `tabIndex=-1`，点一下面板就有焦点），在 `TerminalPanel` 的 `onKeyDownCapture` 里处理，没有注册全局快捷键，查找键也不经过 xterm 的自定义按键处理（那里只有 Mac 行编辑键的一行）。
- 实测的坑：`@xterm/addon-search` 0.16 的 `findNext / findPrevious` 在设置了 decorations 时要 `allowProposedApi: true`，否则抛错、查找条整个坏掉；
  只切换「区分大小写」、搜索词不变时 addon 不会重新计算高亮（它先把新选项写进状态再比较），所以每次新搜索前先 `clearDecorations()`。

### 接管时的「回到最新」：没做

只看时已有「回到最新」（读历史时画面停住）。接管时，滚轮是发给 herdr 的（`terminal_scroll`），由 herdr 决定给谁：shell 滚 herdr 自己的历史，开了鼠标上报的程序（Claude 全屏渲染、Codex）自己往回画。
页面没办法知道「程序的视图现在是不是滚上去了」：

- 帧里只有 `cols / rows / data`，没有滚动位置（`TerminalFrame`）；herdr 的 `pane.scroll_changed` 事件带 `max_offset_from_bottom`，但 node 没有转给 hub，hub 的 `terminal` 推送里也没有。
- 靠自己发出去的滚轮行数估算不可靠：程序可能到顶就不动、一个事件滚几行不定（Claude 有加速）、键入或新输出会把视图拉回底部。
- 靠屏幕文字猜（Codex 的「Back to bottom」之类）只对某个程序某个版本成立。

要做得对，需要 node 把 herdr 的滚动偏移带进帧（或另发一个事件），hub 转给客户端，网页再按偏移显示「回到最新」并发回 End / 滚到底；这是服务端改动，不在这一批。

### 其他

- 输出里的网址：`@xterm/addon-web-links`，只认 http(s)，点击走 `openLink`；这一下不再同时发 `terminal_click`（`linkHit`）。
- 右键菜单：复制 / 粘贴 / 全选 / 搜索… / 清除选区，用 `ui/atoms.tsx` 的 `Menu`，没有改它；`Menu` 只认元素当锚点，所以在鼠标位置放了一个零大小的固定定位元素。菜单打开时让 xterm 失焦（xterm 会吞掉它有焦点时的按键，Esc 会发给程序、关不掉菜单）。
- Mac 行编辑键：⌘⌫ → `\x15`、⌘← → `\x01`、⌘→ → `\x05`，只在接管时、只对只按了 ⌘ 的组合（`lineEditBytes`）。
- 选项：光标样式、光标闪烁、WebGL 渲染（`@xterm/addon-webgl`，第一次打开才动态加载；关掉、context 丢失、浏览器没有 WebGL 时都释放并退回 DOM 渲染）；`jarvis.terminal.cursorStyle / cursorBlink / webgl`，默认值不写键。
- 焦点：接管时点面板、窗口重新获得焦点，键盘还给终端，除非有选中文字、有对话框 / 菜单 / 浮层、焦点在别的输入框（含查找条）或面板外的控件、或这个终端是被盖在别的终端下面保持挂着的那个（`inert`）。

### 验证

`web/src/lib/terminal-details.test.ts`（Mac 键、链接过滤、右键菜单、焦点决定、选项）、`web/src/lib/terminal-search.test.ts`（行匹配、两份匹配的顺序、查找条状态、快捷键）；
截图和交互用 `MOCK_TERMINAL_STREAM=1 MOCK_TERMINAL_CONTROL=1 MOCK_TERMINAL_PANES=1` 的假 hub（接管时 shell 窗格会回显打进去的字，网址就是这样打进屏幕的；只看时有 430 行编造的历史）。
没有在真 herdr 上试：这批改动不碰协议。

## 二十一、节点菜单：关闭、拆分、重命名、在 shell 里启动 Agent、Git（2026-11-05，网页，#2329）

终端页和侧边栏树上的节点菜单（`docs/web.md`「节点菜单」）里改电脑的那几项。都是 hub 上只有 `full` 设备能调的路由（`hub/panes.go` 的 `nodeAction`：`answer` 设备和扩展令牌 403，`route_scopes.txt` 里是 `-`），
node 里对应 `panes.close` / `panes.split` / `panes.agent` / `panes.git` / `tabs.close` / `tabs.rename`（`node/panes_actions.go`）。共同的规矩：只认宿主列出的窗格 / 标签页 / 工作区（找不到先拍一次快照），目录是窗格自己的，请求里没有地方写目录或命令。

- **宿主能力**：`host.SlotCloser`（关整个标签页）、`host.Splitter`（拆分），只有 herdr 宿主实现；Orca、herdr-core、botmux 报 `unsupported`，网页也不显示这些项。herdr 侧是 `tab close` / `pane split` / `tab rename` / `pane close`（socket 方法 `pane.split` 等，CLI 退路 `herdr pane split <pane> --direction right --no-focus --cwd …`）。
- **关闭窗格**：Agent 的窗格走 `session.close`（先 ^C 两次请它退出，再关）；shell 窗格走宿主的 `Close`——标签页只剩它就关标签页，有别的窗格只关它。**关闭标签页**直接关整个标签页。
- **新建窗格**：`pane split <窗格> --direction right|down`，不抢电脑上的焦点，新窗格在被拆窗格的目录；返回 `pane:<id>`。
- **改标签页名**：1–80 个字，空白合并成一个空格；有 Agent 的标签页网页走原来的 `session.rename`，没有的走 `tabs.rename`。
- **在这里运行 Agent…**：只给 shell 提示符下的窗格（窗格没有 Agent、没有绑定的会话、前台程序全是 shell，读不到前台程序也拒绝）。`panes.agent {pane, launcher}` 复用 `start` 的整条流程——
  同一个启动器命令、同样的信任对话框处理、等会话就绪、绑定窗格、续接命令——只是不新开标签页，用 `context` 里的「已选窗格」（`withAdoptedPane`）换掉 `newTab`；启动失败也**不**关这个窗格（新开标签页的启动失败会关那个标签页）。
  网页只列启动 Claude Code 的启动器（Codex / TRAEX 的启动要第一条消息，菜单里没有输入框）。
- **Git**：在窗格的目录（工作区则是它自己的目录）里跑，不是 Git 仓库就拒绝（「不是 Git 仓库」）。**拉取**是现成的 `gitPull`：只快进，有未提交的改动就拒绝；**获取**是 `git fetch --no-recurse-submodules`；
  **推送**要有分支和上游，且有要推的提交（没有就说「没有要推送的提交」），用显式的 `refs/heads/<分支>:<上游分支>`（没有 `+`），所以不会强推，配置里的 `remote.<名>.push = +…` 也不起作用；落后时被远端拒绝，原因原样回到菜单里。
  全部有 60 秒超时，不问密码（`GIT_TERMINAL_PROMPT=0`、askpass 指向 `false`），输出只留最后 2 KB。
- **iOS / Mac**（#3098）：Agents 列表的 Agent 行长按菜单有拆分、Git、关闭标签页，空间的 ⋯ 有在这里运行 Agent…、Git，同一组路由（`docs/ios.md`「节点菜单」）；没有独立终端页，shell 窗格从「在这里运行 Agent…」的 sheet 里选。
- **实测**（herdr 0.9.5，本机一个临时标签页）：`go test -tags herdrlive -run TestLiveMenuActions ./internal/herdr/`（`HERDR_LIVE_WORKSPACE=<工作区 id>`）建标签页、改名、拆分两次、关窗格、关标签页，结果和 node 单测里的假 herdr 一致。
- **测试**：`node/panes_actions_test.go`（假 herdr：关 / 改名 / 拆分的调用、被拒绝时不碰 herdr、shell 窗格里运行、拒绝和失败不关窗格；真 git 仓库：fetch / pull / push、强推的配置不起作用、非仓库 / 脏工作区 / 分离 HEAD 拒绝）、
  `hub/panes_test.go`（每条路由 `answer` / 扩展令牌被拒、node 没听到；只转发点名的字段；不在 `answer` 的允许列表、route scopes 里是 `-`）。

## 二十二、网页版：终端和对话在会话页里原地切换（2026-11-05，#2419）

用户反馈网页版「终端和聊天的切换感觉还是怪怪的 … moshi 的交互形态好一些」。以前终端是对话页右边的侧边面板（`togglePanel({kind:"terminal"})`），或单独的 `/terminal` 页，⌘J 在开 / 关面板和两个页面之间来回。
Moshi 是同一个会话页，中间区域在 Term 和 Chat 之间原地切换（顶栏的分段控件和切换键，地址里 `?view=chat|term`，每个节点记住上次的视图，从侧边栏打开 Agent 窗格进这个页面）。网页版照这个改（细节、地址、记忆、保活见 `docs/web.md`「终端 ⇄ 对话」）：

- 终端还是同一个 `TerminalPanel`（props 不变：`mid`、`sid`、`onClose`、`label`、`cwd`、`onFiles`），不是第二个终端；会话页第一次切到终端才挂上它。
- 切到对话时终端**不卸载**：盒子大小不变、只是看不见（`invisible` + `inert`，不用 `display:none`），所以窗格不会被缩成 100px 的盒子的列数、流不断、切回来不重新订阅（`terminal_subscribe` 只有第一次）。离开会话页或标签页进后台且在看对话时才 `dispose`、放手窗格。
- 接管的尺寸就是会话页中间区域的大小；右边开着文件 / Git 面板时中间变窄，终端按新的宽度防抖后重发尺寸（和以前侧边面板被拖窄一样）。
- 验证：假 hub（`MOCK_TERMINAL_STREAM=1 MOCK_TERMINAL_CONTROL=1 MOCK_TERMINAL_PANES=1`）上数 WebSocket 里发出的 `terminal_subscribe` / `terminal_unsubscribe`：开页面 0 条，切到终端 1 条，来回切 ⌘J 和分段控件仍是 1 条、没有 unsubscribe，换页后有 unsubscribe。

## 二十三、新建 worktree…：从工作区的仓库开一个 Git worktree（2026-11-05，网页，#2431）

工作区的右键菜单 / ⋯ 里的「新建 worktree…」（W），命令面板里同名命令。弹出一个对话框：**分支**和**检出路径**，点「创建并打开」后电脑上多出一个 worktree，并作为 herdr 工作区（分组在原工作区下）打开，网页跳到它的第一个终端。
和 Moshi 的 `worktree/prepare` + `worktree/create` 同一个形状。

- **路由**（`hub/panes.go` 的 `nodeActions`，和菜单里别的动作一样**只有 `full` 设备**，`answer` 设备和扩展令牌 403，`route_scopes.txt` 里是 `-`）：
  - `POST /machines/{mid}/worktree/prepare {workspace_id, kind?}` → `{root, repoName, branch, path}`：`root` 是仓库的 git 顶层，`repoName` 是仓库名（linked worktree 里也是主仓库的名字），`branch` 是工作区当前的分支（分离 HEAD 时空），`path` 是 herdr 放这个仓库的检出的目录 `<worktrees 目录>/<仓库名>`。
  - `POST /machines/{mid}/worktree/create {workspace_id, branch, path, kind?}` → `{workspace_id, tab_id?, pane?, terminal?, path, branch}`。
  - 只认这几个字段，请求里没有地方写基准提交、`--force` 或命令。node 方法 `worktree.prepare` / `worktree.create`（`node/worktree.go`）。
- **交给 herdr**：herdr 0.9 自己有 `worktree create`（socket 方法 `worktree.create {workspace_id, branch, path, focus}`，CLI 退路 `herdr worktree create --workspace … --branch … --path … --no-focus`）：
  分支已有就检出，没有就从工作区的 HEAD 新建；开出的工作区带 worktree 来历，分组在原工作区下，侧边栏里 herdr 自己的 worktree 分组也认它。不抢电脑上的焦点（和别的菜单动作一样），网页自己导航过去。
  `host.Worktreer`（`CanWorktree` 看 `herdr worktree --help`、`WorktreesDir`、`CreateWorktree`）只有 herdr 宿主实现。
- **没有 `worktree` 的旧 herdr**：node 退回 `git worktree add`（已有的本地分支原样检出，否则 `-b` 从仓库的 HEAD 新建；没有 `--force`），再用 `workspace create` 在检出里开一个普通工作区（没有 herdr 的分组）。
  检出成功但工作区没开起来时检出留着，错误里说明。其他宿主（Orca、herdr-core、botmux）不提供，菜单项不出现。
- **能力**：`GET /machines/{mid}/panes` 的每个宿主多一个 `worktree` 布尔值（和 `new_tab` 并列）；没有这个字段的旧 node 不显示菜单项。
- **默认检出路径**：沿用 herdr 的约定 `<worktrees.directory>/<仓库名>/<分支 slug>`，目录读 herdr 配置（`~/.config/herdr/config.toml` 的 `[worktrees] directory`，默认 `~/.herdr/worktrees`，`~` 和相对路径按用户主目录展开）。
  分支 slug 和 herdr 一致：转小写，连续的非 ASCII 字母数字合成一个 `-`，两头不留（`Feat/A_b.c` → `feat-a-b-c`，`fix/中文` → `fix`），这是 2026-11-05 在 herdr 0.9.5 上试出来的，`herdr.WorktreeSlug` 和网页的 `branchSlug` 各有测试。
- **node 的把关**（`worktreeCreate`，都在碰 herdr / git 之前）：工作区必须是宿主列出的；必须在 Git 仓库里；分支名要过 `git check-ref-format --branch`，且不能以 `-` 开头、不能是 `@{-1}` 这类修订表达式；
  检出路径要是绝对路径（`~/` 会展开），不能是已有的文件，已有的目录必须是空的（空目录 git 会直接用），不能放进仓库的 `.git` 里。git 拒绝的（分支已在别处检出、路径不行……）原样把 `fatal:` 那一行回给用户，不会强制、不会删东西、失败不留半成品。
- **对话框**（`views/NewWorktree.tsx`，逻辑在 `lib/worktree.ts`）：打开时先读仓库（「正在读取仓库…」）；检出路径跟着分支名走，手改之后不再跟，清空后恢复；分支名当场校验（空：「请填写分支名」）；
  创建中按钮转圈、对话框不能关；失败显示在对话框里（「没能新建 worktree：…」），旧 hub / 旧 node 没有这个接口时说「这台电脑上的 Persona 版本太旧，更新后才能新建 worktree」；成功后 toast、读一遍树、跳到新工作区的终端。
- **iOS / Mac**（#3099）：Agents 列表空间的 ⋯「新建 worktree…」，同样的两条路由和表单规则（`WorktreeForm`）；建好后不是跳终端，而是打开预填了新工作区的「新对话」（`docs/ios.md`「新建 worktree…」）。
- **测试**：`node/worktree_test.go`（真 git 仓库 + 假 herdr：prepare 的仓库名 / 分支 / 配置的目录 / linked worktree / 分离 HEAD / 非仓库；经 herdr 创建新分支和已有分支；各种被拒绝的路径和分支名不碰 herdr；git 的拒绝原因；退回 git 的路径和重复检出；`panes.list` 带 `worktree`）、
  `herdr/worktrees_test.go`（配置解析、slug、socket 参数和 CLI 退路）、`hub/panes_test.go`（权限、只转发点名的字段）、`web/src/lib/worktree.test.ts`、`node-actions.test.ts`、`node-palette.test.ts`、`views/NewWorktree.test.tsx`。

## 二十四、完整 herdr 界面：PTY 里的 herdr 客户端，和单窗格并排可切换（2026-11-05，#2357）

起因：Moshi 的网页端默认就是一个完整的 herdr 客户端（herdr 自己的侧边栏、标签页、分屏），它的「Control mode」才是只看选中的一个窗格——那就是 Persona 一直以来的做法（observe / control，第一到二十节）。用户要 Persona 两种都有、方便切换、有一点动画，
**并且 2026-11-05 拍板：打开终端默认用完整界面**（单窗格留作可切换的另一种，和机器 / herdr / 系统不支持、或客户端起不来时的自动回退）。

### 为什么当初没用 `terminal attach` / `session attach`，现在为什么可以

第一节和第十一节拒绝它们的理由是：`terminal attach` 占着窗格唯一的 attach 槽位，还会把窗格改成客户端的尺寸；而且它输出的是 herdr 重画的网格，不是程序的原始字节。
**这一节用的是另一个东西**：`herdr session attach <会话名>`，就是在电脑上敲 `herdr` 看到的那个完整 TUI 客户端，不占任何窗格的槽位；
它的字节流是客户端自己画的整个界面（备用屏 + 光标定位），正是浏览器里 xterm 要原样显示的。代价是它和电脑上的 herdr 窗口共用同一份布局，见下面的实测。
第十八节说的「OSC 52 只到得了完整客户端」也在这里兑现。

### 链路

```
浏览器 xterm ──herdr_open / herdr_input / herdr_resize / herdr_ack / herdr_close──▶ hub ──herdr.client.open（RPC）+ .input / .resize / .ack / .close / .keepalive（事件）──▶ node
浏览器 xterm ◀──herdr_state / herdr_data（base64）/ herdr_end───────────────────────── hub ◀──herdr.client.data / .end（事件）────────────────────────────────── node ◀─PTY─ herdr session attach <会话名>
```

**为什么不复用 `terminal_subscribe`**：那条中继是按（机器，会话）共用一路流、留最近的整帧给重连的 App 补发（epoch / seq）；完整客户端是每个浏览器视图一个进程、没有可补发的帧（重连就是新开一个视图）、
流控靠浏览器的 ack。硬塞进同一套机制要给每个分支加条件，另起一组 `herdr_*` 消息更清楚，也不会影响旧 App（它们不发这些消息）。

### node（`server/internal/node/fullclient.go`、`server/internal/herdr/fullclient*.go`）

- **启动**：`herdr session attach <会话名>`，在一个新的 PTY 里（`github.com/creack/pty` v1.1.24：本来就在 go.sum 里（tailscale 的间接依赖），这次把它升成直接依赖，没有新增下载）。
  会话名不让用户填：node 的 herdr 客户端有自己的 socket 路径，按 `herdr session list --json` 里的 `socket_path` 找出对应的会话（默认会话就是 `default`）；socket 不属于任何会话时回 `unsupported`。
  `session attach` 对没在跑的会话也会把它起起来，所以 node 只对「host 已经在用的那个 socket」起客户端。
- **环境**：继承 node 的，去掉 `HERDR_*`、`TMUX*`、`STY`、`SSH_*`（免得 herdr 以为自己在窗格里 / 另一个复用器里），再加 `TERM=xterm-256color`、`COLORTERM=truecolor`、
  `SSH_CONNECTION=127.0.0.1 1 127.0.0.1 2`（回环占位，让 herdr 把自己当成远程、复制时走 OSC 52 而不是电脑上的剪贴板工具；做法同有人给 moshi-hook 配的 systemd drop-in）。
- **初始焦点**：打开时带 `focus`，node 先聚焦那个窗格（herdr 0.9.5 有 socket 方法 `pane.focus {pane_id}`，更老的只有 `workspace.focus` / `tab.focus`，那就聚焦窗格所在的空间和标签页，窗格在标签页里是 herdr 上次聚焦的那个；第二十八节）。
  **焦点是整个 herdr 会话共享的**（实测见下），所以电脑上的窗口也跟着跳——和用户在完整客户端里点一下标签页是同样的效果，所以默认就这样做。Moshi 的网页端不选，显示的是 herdr 当前聚焦的窗格。
- **上限**：每台电脑最多 4 个完整客户端（`fullMaxClients`），第 5 个回 `too_many`；一个浏览器连接在 hub 上最多 4 个视图。
- **流控**：node 给 hub 发出去、浏览器还没 ack 的字节超过 256 KB（`fullWindow`）就不再读 PTY——herdr 客户端写满终端缓冲区就自己停住，node 里不攒东西。ack 是累计的字节数，超过已发出的不算。
  浏览器 60 秒一个 ack 都没有（窗口一直满着）就当它不读了，结束这个视图。hub 这一侧另有上限：给某个浏览器的未 ack 字节超过 1 MiB 就结束视图并让 node 关掉客户端（node 或浏览器不守规矩时 hub 也不会无限攒）；浏览器的发送队列满了结束这个视图，不断整条连接。
- **输入**：hub 转来的键由单独的 goroutine 按顺序写进 PTY（队列 256 条）；客户端不收输入时 node 的事件循环不会被堵住，队列满了就结束它。
- **关掉就不留进程**：视图的每一种结局都会结束客户端进程——hub 说关（`herdr.client.close`）、hub 连接断了（`Serve` 返回时 `closeFulls`）、租约到期（hub 每 20 秒 `herdr.client.keepalive`，75 秒没听到就关）、浏览器不读、客户端自己退出（`herdr client exited: N`，会告诉 hub）。
  关法：先关 PTY 主端（客户端收到挂断就退，**只退客户端，herdr 服务和窗格都在**——实测），2 秒不退发 TERM（给它的整个进程组），再 2 秒发 KILL。Windows 返回 `unsupported`。
- **测试**：`TestFullClient…`（node：流、改尺寸、打字、关、窗口满了停读、乱 ack 不开窗口、浏览器不读被结束、租约、客户端退出、上限、`closeFulls` 后没进程）和 herdr 包的 PTY 生命周期（含忽略 HUP / TERM 的客户端会被 KILL），用假的 `herdr` 脚本；
  `-tags herdrlive` 的 `TestLiveFullClient` / `TestLiveFocusPane` 对着自己建的临时会话跑真 herdr（`PERSONA_LIVE_FULL_SESSION=<会话名>`，绝不连默认会话）。

### hub（`server/internal/hub/herdrclient.go`）

| 浏览器 → hub | 字段 |
| --- | --- |
| `herdr_open` | `id`（浏览器自己取，≤ 64 字符）, `machine_id`, `session_id`（可选，会话 id 或 `pane:` 地址：先聚焦它）, `cols`, `rows`, `focus` |
| `herdr_input` | `id`, `data`（文字，原样写进去）或 `b64`（字节）；一条最多 1 MiB（网页自己拆成 32K 一条） |
| `herdr_resize` | `id`, `cols`, `rows`（1–500 × 1–300） |
| `herdr_ack` | `id`, `bytes`：浏览器已经解析了的总字节数（累计） |
| `herdr_close` | `id` |

hub → 浏览器：`herdr_state {id, state: "open", cols, rows}`、`herdr_data {id, data}`（base64，一条最多 32 KB）、`herdr_end {id, error?, code?, unsupported?}`（`code`：`forbidden`、`too_many`、`bad_request`、`offline`）。

- **权限**：只有 `full` 角色的设备能开（完整客户端能在那台电脑的 herdr 里做任何事：在任何窗格里跑命令、关窗格）；手表这类只能回答的设备回 `forbidden`，**连看也不行**；扩展令牌连 WebSocket 都进不去（`GET /api/v1/ws` 在 `route_scopes.txt` 里本来就是 `-`），所以没有新的路由要登记。
  每个视图只属于开它的连接：别的连接猜到 id 也碰不到（`fullOf` 按连接找）。node 发来的 data / end 只认自己机器上的视图。
- **hub 里的生命周期**：连接断了 → `fullDrop`，对每个视图让 node 关客户端；电脑掉线 → `fullsOffline`，浏览器收到 `herdr_end {code: "offline"}`；hub 每 20 秒给 node 发 `herdr.client.keepalive`（node 的租约 75 秒）。
- **兼容**：feature `herdr_client`。旧 hub 没有它，网页不显示切换、设置里也不出这一项，终端照旧是单窗格；hub 新而 node 旧（`unknown method herdr.client.open`）或 herdr / 系统不行时，视图以 `unsupported` 结束，网页自动回退到单窗格并提示原因。

### 网页（`web/src/views/session/TerminalPanel.tsx`、`terminal/FullTerminal.tsx`、`terminal/full.ts`、`terminal/Surface.tsx`、`lib/terminal-mode.ts`）

- **切换**：终端面板（侧边面板和 /terminal 页是同一个组件）头上的分段控件「单窗格 / 完整界面」，窄的时候只剩图标；记住的只有最近一次选择（`jarvis.terminal.mode`）。Mod+Alt+F 也能切（快捷键注册表的 `terminal-mode`，面板在前面时才生效）。
  面板的 props 没变（`mid, sid, onClose, label, cwd, onFiles`），两个界面各自把自己的头部按钮画进面板头的同一个槽里，切换控件始终在原处。
  会话页里终端是原地的一个视图（第二十二节，页面自己已有标题和「终端 | 对话」），那里面板多收一个 `embedded`：自己的头收成一条 40 px 的工具栏（左边是这个切换，右边是状态、文件、键栏、重新连接），不再有第二个标题栏。
- **设置 › 终端界面**：完整界面（默认，不写任何键）/ 单窗格 / 记住上次（新终端从上次的选择开始）。hub 没有 `herdr_client` 时这一项不显示。
- **动画**：两个界面一个往里滑 + 淡入、一个往外滑 + 淡出（14 px、220 ms，`--ease-out-strong`），全是 CSS transition，没有新的动画库；`styles.css` 里已有的 reduced-motion 规则把时长压到 1 ms，面板另外在该设置下立刻卸掉退出的那个。
  滑动结束后元素上没有 transform（有 transform 的盒子会困住 fixed 定位的菜单）。单窗格 → 完整界面时，完整界面等单窗格淡出后才开（淡出期间单窗格还在接管自己的窗格）；完整界面 → 单窗格时，完整界面立刻放手。
- **完整界面本身**：一个普通的 xterm（滚动历史 5000 行、`@xterm/addon-clipboard`（OSC 52 写进剪贴板，读一律拒绝）、`addon-web-links`、`addon-search`），行高 1（herdr 用框线字符画边框和条，行与行之间有缝就变成虚线）。
  键栏、查找条（⌘F / Ctrl+Shift+F）、右键菜单（复制 / 粘贴 / 全选 / 搜索… / 清除选区）、存好的命令和单窗格共用同一批组件；herdr 开着鼠标上报时右键是 herdr 自己的，Persona 的菜单是 Shift + 右键（第二十六节，原先写的「有选中文字时才出网页菜单」已去掉）。按住 Shift 拖选是 xterm 自己的选择。
- **应答 herdr 的提问**（Moshi 同样）：herdr 客户端启动时问 `CSI ? 996 n`（配色）、开 DEC 2031（配色变化通知）、问 `OSC 10;? / 11;?`（前景 / 背景色）、`CSI ? u`（kitty 键盘）、DA1、`CSI 16 t`。网页回答 `CSI ? 997 ; 1|2 n`（深 / 浅）、`CSI ? 2031 ; 1|2 $ y`，
  页面切深浅色时若 herdr 开了 2031 就主动再报一次颜色和配色；OSC 10/11、DA1 由 xterm 自己答。（第十八节说「颜色查询到不了浏览器」是单窗格流的事，这里它们到得了。）
- **流控**：xterm 每解析完一段（`write` 的回调）累计字节数，满 64 KB 或距上次告诉 hub 过了 250 ms 就发一条 `herdr_ack`（Moshi 同样的数）。
- **状态**：头上一个小圆点 + 「连接中 / 已连接 / 已断开 / 出错了 / 不支持 / 没有权限」和格数，鼠标放上去是原因；视图结束时原因以暗色一行写在终端里（同 Moshi 的 `close.reason`），底部浮出「重新连接」。
  hub 的 WebSocket 断了又连上：旧视图随连接没了，页面清屏后自动开新的。
- **停用**：终端页「挂着但不在前面」的终端（`inert`）和正在淡出的界面，会放掉电脑上的客户端（不占那 4 个名额、不抢布局），回到前面时重连。
- **自动回退到单窗格**：视图在**还没连上过**时就结束（`unsupported`、`forbidden`、`too_many`、客户端一起来就退出……电脑离线除外），面板提示「完整界面没能打开：原因。改用单窗格」并切过去；
  这台电脑在这个页面存活期间记为不支持，它的其他终端直接用单窗格（用切换控件手动选完整界面会再试一次）。
- **首次提示**：第一次连上时提示一次「完整界面是 herdr 自己的客户端：在这里操作时，电脑上的窗格会按这个面板的大小排版；回到电脑上操作又会换回去。」（`jarvis.terminal.fullHint`）。

### 实测（herdr 0.9.5，自己建的临时会话 `persona-fullui-test`，没碰默认会话；测完 `session stop` + `session delete`）

用 Python `pty` 起两个 `herdr --session persona-fullui-test` 客户端（A 模拟电脑上的窗口 120×34，B 模拟浏览器），用 `herdr pane layout` 看布局、用 pyte 看各自的屏幕：

| 情形 | 窗格布局区域（`pane layout` 的 `area`） |
| --- | --- |
| 没有客户端 | 120×40（停在上次的尺寸） |
| A（120×34）连上 | 94×33（= 终端宽 − 侧边栏 26，高 − 1 行状态栏） |
| A + B（80×24）连上 | 54×23（**B 的**） |
| B 改成 100×30 | 74×29 |
| A 改成 140×40 | 114×39（**A 的**） |
| B 改成 60×20 | 60×18（窄于 64 列走 herdr 的手机单列布局，不再留侧边栏） |
| B 退出 | 114×39（回到 A 的） |
| A 在 B 之后打一个字 | 回到 A 的尺寸；B 再打一个字又回到 B 的 |

- **谁说了算：最近一个有动作的客户端**（连上、改尺寸、打字），不是最小的也不是最大的。布局和窗格里程序看到的尺寸是整个会话一份——**浏览器连着完整界面时，电脑上正在跑的 Claude 会按浏览器的尺寸重排版，直到在电脑上敲一个键又换回去**；
  两边轮流敲键就会来回换。这和单窗格的「接管」是同一类代价（第十一节），所以首次提示里说了，默认值也因此是用户拍板的而不是我们自己定的。
- 电脑上那个窗口（A）在 B 连上后的一秒里只收到 55 个字节，它画的窗格几何没变（pyte 里看；pyte 不是完整的终端仿真，只能说没看到整屏重画），直到它自己下一次有动作。没有量到「电脑上的窗口布局被改乱」，量到的是窗格里程序的尺寸变了。
- **焦点（当前的空间 / 标签页）是整个会话共享的**：`tab focus` 一下，A 和 B 两边都跳；B 连上时显示的就是当前聚焦的标签页。所以「用打开它的那个窗格作初始焦点」只能通过改共享焦点实现（上面「初始焦点」）。
- **OSC 52**：herdr 里鼠标拖选（SGR 鼠标事件按下、拖、松开）选中后，客户端输出里有 `ESC ] 52 ; c ; <base64> BEL`；窗格里的程序自己发的 OSC 52 也原样出现在客户端输出里。
  这台机器上没有显示环境（或给一个不存在的 `WAYLAND_DISPLAY`）时，带不带 `SSH_CONNECTION` 都发 OSC 52；有显示环境和剪贴板工具时的行为没量（怕写到用户真的剪贴板），所以仍然设置 `SSH_CONNECTION`。
  **真 herdr → 浏览器剪贴板**：`scripts/test/web_herdr_full_live.py`（假 hub 的 `MOCK_TERMINAL_FULL_EXEC` 在 PTY 里跑真的 `herdr session attach`，Chrome 里鼠标拖选 herdr 屏幕上的 `LIVE_COPY_MARK`，剪贴板读出来就是 `LIVE_COPY_MARK`）通过。
- **客户端启动时发给终端的序列**（`TERM=xterm-256color`）：`?1049h` 备用屏、`>7u` 和 `?u`（kitty 键盘）、DA1、`?1016$p`、`?1000/1002/1003/1015/1006h`（鼠标）、`?2004h`（括号粘贴）、`?1004h`（焦点事件）、`?2031h`、`OSC 10;? / 11;?`、`CSI ? 996 n`、`CSI 16 t`、`?2026h`（同步输出）。
- herdr 配置 `[ui] mobile_width_threshold = 64`：终端宽度 ≤ 64 列时是单列布局，所以手机上（390 px 约 46 列）完整界面没有侧边栏，只是一个窗格加标签页条。
- `sidebar_collapsed_mode` 是 herdr 的**共享配置**：Moshi 的「Hide collapsed Herdr sidebar」改的就是它（网关 `POST /gateway/v1/pty?mux=herdr&hideSidebar=…`，并提示 "Changes Herdr's shared config"）。Persona 不改用户的 herdr 配置；侧边栏用 herdr 自己的 prefix+b 收起。

### 和 Moshi 的差别

| Moshi | 这里 | 为什么 |
| --- | --- | --- |
| `GET /gateway/v1/pty?mux=herdr…` 的 WebSocket，二进制帧 | 复用已有的应用 WebSocket，JSON + base64 | 不用再开一条连接、一套鉴权；多约 33% 字节，局域网 / 公网上都不是瓶颈，有 ack 兜底 |
| 默认完整界面，Control mode（`herdrPaneStreams`）是另一个设置 | 默认完整界面（用户拍板），头上的开关随时换，设置里选默认 | 开关放在要用的地方；记住上次可选 |
| ack：每 64 KB 或 250 ms，累计字节 | 一样 | |
| 重连：先踢掉本机最近 2 个分离的终端再重试 | 每台电脑 4 个名额，满了回 `too_many`；终端页挂着的不在前面的会放掉客户端 | 名额是 node 管的，不靠客户端互相踢 |
| Hide collapsed Herdr sidebar（改 herdr 共享配置） | 不做 | 不改用户的 herdr 配置 |
| 选哪个会话 / 窗格：不选（显示 herdr 当前聚焦的） | 打开时把那个窗格所在的标签页聚焦 | 从 /terminal 里点了一个窗格，期望看到它 |
| 关页面（`pagehide`）时销毁所有终端 | 连接断了 hub 关；组件卸载发 `herdr_close`；租约兜底 | node 侧不留进程，是测试过的 |

### 验证

- Go：`go test -count=1 ./internal/herdr ./internal/host ./internal/node ./internal/hub ./internal/proto`；对真 herdr：`PERSONA_LIVE_FULL_SESSION=<临时会话> go test -tags herdrlive -run 'TestLiveFullClient|TestLiveFocusPane' ./internal/herdr/`。
- 网页：`cd web && npm test && npm run build`（`terminal-mode.test.ts`、`Surface.test.tsx`、快捷键表）。
- 端到端：`scripts/test/web_herdr_full_e2e.py`（假 hub 的 `MOCK_TERMINAL_FULL=1`，用法见脚本开头：默认完整界面、ack、配色应答、打字、OSC 52 到剪贴板、切换 / 动画中间态 / 记住选择 / 快捷键 / 改尺寸 / 关闭、设置、减弱动态、结束后重连、不支持时回退、旧 hub 没有开关；截图深浅色 × 宽 / 390 px）和 `scripts/test/web_herdr_full_live.py`（真 herdr，见上）。

## 二十五、鼠标：点击、拖动、按键和修饰键（2026-11-05，网页，#2441）

用户：「旧版终端鼠标点击都不支持」。以前接管时只有「不拖动的左键单击」（`terminal_input.click`），没有拖动、没有右键 / 中键、没有修饰键。现在整条鼠标都转给 herdr。

### herdr 的 `terminal.mouse`（0.9.5 实测，control 会话的 stdin 协议）

来源：herdr 二进制里的 serde 报错（`unknown variant … expected one of …`）、`herdr api schema`（只有 socket API，没有终端流的消息）、
以及在一个临时 herdr 标签里用 `cat -v` + `printf '\e[?1000h\e[?1002h\e[?1006h'`、vim（`set mouse=a`）逐个发消息看程序收到的字节。

```json
{"type":"terminal.mouse","action":"down","button":"left","column":10,"row":5,"modifiers":0}
```

| 字段 | 取值（实测） |
| --- | --- |
| `action` | `down`、`up`、`drag`（按住键移动）、`move`（不按键移动）。**没有** double / triple：双击三击就是程序按时间间隔数的几次 `down`/`up`，实时发过去它自己认（vim 的双击选词可用） |
| `button` | `left`、`right`、`middle`。滚轮不在这里，是 `terminal.scroll`（第十六节） |
| `modifiers` | 数字（u8），写成数组或字符串会被拒（`invalid type … expected u8`）。位：shift 1、ctrl 2、alt 4；组合相加（`3` = shift+ctrl 收到 `<20`，`5` = shift+alt 收到 `<12`）。没有 meta，别的位被忽略；不给就是 0 |
| `column`、`row` | u16，从 0 起；程序收到的是 1 起（`ESC[<0;11;6M`）。落在窗格外（200、65535）herdr 直接丢掉（程序没收到），所以我们只发 `0 ≤ 值 < 4096` |

herdr 照程序要的编码转发：程序开了 `?1006`（SGR）收到 `ESC[<b;x;yM` / `m`（松开）；`down` = `<0`、`drag` = `<32`、`move` = `<35`，右键 `<2`、中键 `<1`，shift +4、alt +8、ctrl +16。
程序没开鼠标上报（`?1000/1002/1003` 都关）时事件被**丢掉**，herdr 不会自己选择文字或滚动；`move` 只有程序开了 `?1003`（任意移动）才收到。
未知的 `action` / `button` / 字段类型：herdr 在 stderr 记一行 `terminal session control input ignored: invalid json command`，会话不断。

### herdr 不告诉我们程序开没开鼠标

找遍了：control 流的帧（`terminal.frame` 的字段只有 seq / full / width / height / encoding / bytes，bytes 是 herdr 重画的网格，里面没有 `?1000h` 之类的模式序列）、
`pane get` / `pane process-info` / `pane layout`、`herdr api schema` / `snapshot`、`pane read --raw`（空）、`terminal attach`（要 tty；没有 tty 时只写一串 `?…l` 复位就退出）。都没有鼠标模式。
herdr 二进制里有 `mouse_tracking_enabled`（它内部的终端），只是没有暴露。所以网页没法按程序切换「本地选择 / 交给程序」。

### 选择 vs 程序（默认交给程序，Shift 留在页面）

照 Moshi（它里面跑的是 herdr 客户端，鼠标一律先给 herdr，herdr 决定程序收不收）：

| 设置（终端设置 › 鼠标，`jarvis.terminal.mouse`） | 不按 Shift | 按住 Shift |
| --- | --- | --- |
| 交给程序（默认，不写键） | 按下 / 拖动 / 松开都发给 herdr；左 / 中 / 右键，ctrl、alt 作为修饰键；页面不选字、右键不出菜单 | xterm 本地选字（拖动）、右键出 Persona 菜单 |
| 本地选择 | 和以前一样：拖动选字、右键出菜单、不拖动的单击仍是 `terminal_input.click` | 交给程序（Shift 是路由键，不再当修饰键发出） |

- ⌘（meta）和触屏一律留在页面：⌘ 点链接照旧能打开，手指仍是滚动 / 长按选字、轻点 = `click`。
- 只在接管时发（driving），只看时什么都不发；Shift 选字后再点一次给程序的按下，会先清掉选区。
- 副作用要知道：**默认下 shell 里（程序没开鼠标）直接拖动不会选字**，要按住 Shift，或在设置里选「本地选择」。这是没有模式信息下的取舍；
  以后 herdr 暴露了鼠标模式，就可以只在程序开了鼠标时才交给程序。
- 拖动：每进入新的一格发一次 `drag`（`CellThrottle`），指针移出终端后仍跟着，夹在最近的边缘格，松开一定发 `up`（窗口失焦也补一个）。
- 悬停（`move`）**没做**：不知道程序开没开 `?1003`，每次鼠标移动都发一条消息要白白占链路；需要时可以在知道模式后再加。
- 双击三击：不特殊处理，实时的 `down`/`up` 保留了间隔，程序自己数。

### 协议和代码

- 网页 → hub：`terminal_input` 多一个 `mouse: {action, button, at: [column, row], mods}`（feature `terminal_mouse`，旧的 `click` 保留给旧客户端和本地选择模式）。
  hub 只认正在接管的那个连接（`terminalMouse`，和键、滚轮同一条顺序），校验 action / button / 格子后转成 node 的 `terminal.mouse` 事件；`mods` 只留三位。
- node → herdr：`host.Mouser`（`herdr.Control.Mouse`）写 `terminal.mouse`，同样先校验。老 node 没有这个事件就丢掉，网页看 hub 的 `terminal_mouse` 决定发不发。
- 网页：纯逻辑在 `web/src/lib/terminal-mouse.ts`（按钮 / 修饰键映射、路由、右键菜单决定、按格节流），`feed.ts` 里「the mouse」一节只做事件接线；右键菜单的判断经 `feed.wantsMenu`。
- 文件拖放（上传、Files 条目）不受影响：它们走 `dragenter` / `drop`，鼠标转发只处理 `mousedown` / `mouseup` / `mousemove`。

### Mac（#3336）

和网页同一套路由和消息：`apple/Mac/Terminal/MacTerminalMouse.swift`（纯逻辑：按钮、路由、修饰键、按格节流、出界夹边），`TerminalView+Mac.swift` 的
`mouseDown` / `rightMouseDown` / `otherMouseDown` 及对应的拖动、松开先问 `forwardDown` / `forwardDrag` / `forwardUp`，发给 `AppModel.terminalMouse`（`terminal_input.mouse`）。

- 只在接管、hub 有 `terminal_mouse` 时转发；旧 hub 或只看时和以前一样（拖动选字、右键菜单、单击是 `terminal_input.click`）。
- 终端设置 › 鼠标（`terminalMouseMode`，只在 Mac 上有）：交给程序（默认）/ 本地选择，Shift 换路由、不当修饰键发出；⌘ 一律留在本地（⌘ 点链接）。ctrl、option 是修饰键（2、4）。
- 按钮按事件类型认（左、右），其余按 `buttonNumber`（2 是中键）；按住一个键时按下别的键被吞掉；拖动每进一格发一次，出了窗口夹在最近的边缘格，松开一定发 `up`（AppKit 把松开交给按下的那个视图）。
- 格子按画出来的行算：feed 不画末尾的空行，点在最后一行下面按行数往下数，夹在请求的网格里；点在接管后留下的历史（屏幕上面）不算程序的，照旧本地选字。
- 滚轮不变，还是 `terminal_input.scroll`。悬停（`move`）同网页没做。
- 单测 `PersonaMacTests/MacTerminalMouseTests`（路由、位、节流、夹边）和 `MacTerminalMouseViewTests`（给视图喂 AppKit 事件：按下 / 拖动 / 出界 / 松开、右键、Shift 选字、本地选择、不转发时的旧单击）。

### 验证

`go test ./internal/herdr ./internal/hub ./internal/node`（编码、只有 driver 能发、顺序）、`npm test`（`terminal-mouse.test.ts`）、
`scripts/test/web_terminal_mouse_e2e.py`（假 hub 记录 `terminal_input.mouse`，Playwright 点 / 拖 / 右键 / Shift / 设置）、
`go test -tags herdrlive -run TestLiveControlMouse`（`JARVIS_LIVE_MOUSE_PANE`，真 herdr 临时标签里的 vim：单击把光标移到格子上，拖动选出可视选区）。

## 二十六、完整界面里的冲突：herdr 的右键、前缀键、鼠标和粘贴（2026-11-05，网页，#2700）

用户反馈（部署后的完整界面）：「右键好像有冲突啊在 herdr 里，而且也不能粘贴图片」，并要求把类似的冲突都查一遍。原则同 Moshi（把整个终端区域交给 herdr）：
**完整界面里 herdr 说了算；Persona 只留 herdr 不用的东西，或者需要 herdr 不绑的修饰键的东西。** 单窗格的行为一个都没改。

### herdr 0.9.5 自己的绑定和鼠标（实测）

来源：`herdr --default-config`、`herdr config check`、客户端里的 `prefix+?` 键位表（逐页翻完）、`herdr --skill`，以及一个自己建的临时会话（`herdr --session persona-conflict-test`，
在 PTY 里发 SGR 鼠标和按键，用 `pane read` / OSC 52 看结果；测完 `session stop` + `session delete`）。用户的 `config.toml` 只有 `[ui]` 的外观项，没有改键。

- **前缀键** `ctrl+b`，**除它之外没有别的直接键**（`remote_image_paste = ctrl+v` 只在 `herdr --remote` 里生效，这里不是）。其余全是「前缀 + 键」：
  `?` 键位表 · `s` 设置 · `q` 分离 · `shift+r` 重载配置 · `o` 打开通知目标 · `w` 空间导航 · `g` 会话导航 · `shift+n` 新空间 · `shift+g` 新 worktree · `shift+w` 重命名空间 · `shift+d` 关闭空间 ·
  `c` 新标签页 · `shift+t` 重命名标签页 · `p` / `n` 上 / 下一个标签页 · `1..9` 切标签页 · `shift+x` 关标签页 · `v` 竖分 · `-` 横分 · `x` 关窗格 · `shift+p` 重命名窗格 · `e` 编辑回滚 ·
  `[` 复制模式（键位表里有，默认配置里没写）· `z` 缩放 · `r` 调整大小模式 · `b` 收起侧边栏 · `h j k l` 聚焦窗格 · `tab` / `shift+tab` 轮换窗格。
  导航模式里：`esc` 返回、上下选空间、`h j k l` / 左右移动焦点、`tab` 轮换、`enter` 打开、`1..9` 切空间。
  默认没绑（用户自己才会绑，可以绑成 `ctrl+alt+…` 之类的直接键）：上 / 下一个空间、上 / 下一个 Agent、移动标签页、调整窗格大小、清屏、最近窗格。
- **鼠标**（客户端一启动就开 `?1000/1002/1003/1006`）：
  - 窗格上**右键**：herdr 自己的菜单「Rename pane / Split right / Split down / Zoom / Send right-clicks to pane / Close pane」；侧边栏的空间行上右键是「Rename」。
    `right_click_passthrough_modifier` 可以把右键交给窗格里的程序（不支持 Shift，herdr 说终端常把 Shift+鼠标留给自己）。
  - **左键拖动**：herdr 自己画选区，松开时发 OSC 52 复制（`copy_on_select`，默认开）；**双击**选词并立刻复制（实测选出 `bash` 并发了 OSC 52）。三击没有量到复制（探针里点得太快，没有结论）。
  - **中键**：herdr 不处理（实测什么都没发生）。**滚轮**：滚窗格的回滚，每格 `mouse_scroll_lines`（默认 3 行），或交给开了鼠标的程序。
  - 括号粘贴：客户端开了 `?2004`；窗格里的程序没开括号粘贴时 herdr 自己去掉 `ESC[200~ … ESC[201~`（实测 `cat -v` 里只看到粘进去的字），开了就原样转给程序。

### 冲突表

| Persona 的绑定 / 行为 | herdr 的绑定 / 行为 | 决定（完整界面里） |
| --- | --- | --- |
| 右键 → Persona 终端菜单（复制 / 粘贴 / 全选 / 搜索… / 清除选区） | 窗格 / 空间的右键菜单 | **herdr**：右键作为鼠标事件发给 herdr（xterm 的鼠标上报），并 `preventDefault` 压掉浏览器自己的菜单；**Shift + 右键**才是 Persona 的菜单（同单窗格 #2441）。herdr 没开鼠标时（还没启动、`mouse_capture = false`）没有冲突，普通右键仍是 Persona 的菜单。触屏长按留给手机选字 |
| 右键时 xterm 选中光标下的词（xterm 在 Mac 上的默认 `rightClickSelectsWord`） | 右键是 herdr 的菜单，不该有本地选区 | 关掉（`rightClickSelectsWord: false`） |
| 有 xterm 选区时普通右键也出 Persona 菜单（原先的例外） | 同上 | 去掉：只认 Shift |
| Shift + 拖动 → xterm 本地选区 | 左键拖动是 herdr 自己的选区 + OSC 52 复制 | 都留：普通拖动是 herdr 的（复制由 OSC 52 到剪贴板，提示「已复制」）；Shift + 拖动是页面的 |
| Ctrl/⌘ + C（有 xterm 选区时复制，没有选区仍是 `^C`） | `copy_on_select = false` 时 herdr 用 Ctrl+C 复制自己的选区 | 不冲突：Persona 只拦 xterm 本地选区；herdr 的选区不在 xterm 里，`^C` 照常给 herdr |
| 双击 / 三击 | 双击选词并复制 | herdr（xterm 在鼠标上报时不自己选词） |
| 中键 | herdr 不处理 | 不动：鼠标报告照发，Linux 上浏览器的 PRIMARY 粘贴照旧（是浏览器 + xterm 的行为，不是 Persona 的） |
| 滚轮（单窗格里有自己的整行滚动处理） | 滚回滚 / 交给程序 | herdr：完整界面没有任何滚轮处理，xterm 只发 SGR 滚轮报告（e2e 里量了：只有 `ESC[<64;…M`，没有方向键，也没有第二个处理者） |
| 点击网址（xterm 的链接，原先任意点击都打开） | 点击 = 聚焦窗格 / 选择；双击网址会选词 | herdr 开着鼠标时要 **⌘ / Ctrl + 点击**才打开（否则双击一个网址会开两次）；触屏轻点和 herdr 没开鼠标时照旧点一下就开 |
| **Mod+B（右侧面板）**：Windows / Linux 上就是 **Ctrl+B** | **前缀键 `ctrl+b`** | **herdr**：完整界面里 Ctrl+B 原样发给 herdr，不触发右侧面板（`herdrOwnsKey`，xterm 的按键处理和全局 `dispatchKey` 都认）。右侧面板仍可以用命令面板（Mod+Shift+P）开，或者在完整界面外按 Ctrl+B；Mac 上 Mod 是 ⌘，⌘B 不冲突 |
| 其余 Mod 键：Mod+K 跳转 · Mod+Shift+P 命令面板 · Mod+/ 快捷键 · Mod+, 设置 · Mod+1..9 电脑 · Mod+Alt+B 侧边栏 · Mod+Shift+Enter 最大化 · Mod+J 终端⇄对话 · Mod+Alt+F 界面 · Mod+I 输入框 · Mod+Alt+←/→ 标签页 · Mod+E · Mod+Alt+↑/↓ | 默认都没绑（herdr 只有前缀键，其余是「前缀 + 裸键 / Shift 键」，Persona 的键都带 Mod） | 留给 Persona（`terminal-conflicts.test.ts` 逐个核对了：非 Mac 上和 herdr 默认直接键重合的只有 Mod+B）。herdr 里用户自己绑了直接键（如 `ctrl+alt+…`）时 Persona 不知道，见「没做的」 |
| ⌘⌫ / ⌘← / ⌘→ → `^U` / `^A` / `^E` | 浏览器不会把 ⌘ 键给终端，herdr 也不绑 ⌘ | 留：发给 herdr，由聚焦的窗格里的程序处理 |
| Shift+Enter → `ESC CR`（Claude Code 的换行） | 不绑，原样转给窗格 | 留 |
| 查找 ⌘F / Ctrl+Shift+F / 只看时 Ctrl+F | 不绑（`prefix+?` 键位表里的 `/` 是表里的过滤） | 留；只搜 xterm 屏幕上 herdr 画出来的字（herdr 用备用屏，没有回滚可搜） |
| 键栏 Ctrl / Alt / Shift 修饰 + 一个键 | 先 Ctrl 再点 B 就是前缀键 | 不变（键栏没有专门的前缀键，见「没做的」） |
| 点击面板 / 窗口重新获得焦点时把键盘还给 xterm（`shouldRefocus`） | herdr 的弹窗、菜单、对话框都画在终端屏幕里，没有 DOM 焦点；客户端开了 `?1004`，焦点真变了才发 `CSI I/O`（`redraw_on_focus_gained` 会整屏重画） | 不冲突：已经聚焦时 `focus()` 不会发新的焦点事件；有选区、浮层、别的输入框在用时本来就不抢 |
| 粘贴文字（⌘V / Ctrl+Shift+V / 键栏 / 菜单） | 客户端开了括号粘贴 | 不变：xterm 的 `paste`，括号包起来，herdr 按窗格里的程序决定去不去括号 |
| **Windows / Linux 的 Ctrl+V**（xterm 发 `^V` 并取消按键，浏览器不会粘贴） | `remote_image_paste = ctrl+v` 只在 `--remote` 里 | **完整界面里 Persona 先看剪贴板**：有图片 / 文件就上传并粘贴路径；没有就还是 `^V`（窗格里的程序要用：quoted-insert、vim 块选择）。文字粘贴用 Ctrl+Shift+V 或键栏 |
| **⌘V（Mac）**：`paste` 事件带着文件，完整界面原先只认文字 | — | 捕获阶段先看 `clipboardData.files`：有就上传（下面），没有交给 xterm |
| 拖放文件 / Files 面板的条目（`@path`）：完整界面原先没接 | herdr 不处理拖放 | 接上，同单窗格（下面） |

### 粘贴和拖放（完整界面）

共用模块 `views/session/terminal/transfer.ts`（`FileTransfer`，单窗格的 `feed.ts` 也改成用它，上传 / 提示 / 括号粘贴的逻辑只有一份）：

- 来源：`paste` 事件的 `clipboardData.files`、键栏 / 菜单「粘贴」里的 `navigator.clipboard.read()` 图片、Windows / Linux 的 Ctrl+V（同样先读剪贴板）、拖进来的 OS 文件、Files 面板拖来的条目。
- 文件 `POST /machines/{mid}/files?dest=terminal`（窗格所在的那台电脑，同第十八节），然后把转义后的路径加一个空格用 `term.paste` 敲进去：herdr 开了括号粘贴，所以到 herdr 的是 `ESC[200~路径 ESC[201~`（`herdr_input`），herdr 再按窗格里的程序决定。
- 状态条（「正在上传…」）和拖入时的高亮圈用同一个组件（`UploadStatus.tsx`，`TerminalPanel` 也换成它，样子不变）；失败、太大、空文件的提示同单窗格。客户端不在线时提示「完整界面连上后才能粘贴文件」；上传期间界面断开或换了终端就不粘贴并提示。
- Files 面板的条目写成**绝对路径**（`@/abs/path `）：完整界面里聚焦的是 herdr 的哪个窗格不由页面决定，页面不知道它的工作目录，所以不用相对路径。别的电脑的条目拒绝。

### 没做的（有意留下）

- **Persona 不读 herdr 的 `config.toml`**：用户改了前缀键，或者绑了 `ctrl+alt+…` 之类的直接键，Persona 的快捷键仍会先抢（在 Persona 的「快捷键」设置里改掉冲突的那个即可）。以后可以让 node 在 `herdr_state` 里报告 herdr 的键位，网页据此放行。
- **单窗格里 Windows / Linux 的 Ctrl+V 还是发 `^V`**（xterm 的行为，上面第十八节已改正文档）；不在 #2700 范围，同样两行可以修。
- 键栏没有「前缀」专用键（手机上要先点 Ctrl 再敲 B）。
- Persona 的终端菜单在触屏上够不到（长按留给手机选字）；完整界面的头上也没有「⋯」菜单。
- 中键粘贴（Linux 的 PRIMARY）、三击是否复制：没有改动，也没有量到结论。
- 设置里的「鼠标：本地选择」只管单窗格；完整界面的鼠标一律先给 herdr，Shift 留给页面。

### 验证

- `cd web && npm test`：`terminal-conflicts.test.ts`（右键 / 链接 / 前缀键 / Ctrl+V 的决定、Persona 的默认键里只有 Mod+B 与 herdr 重合）、`transfer.test.ts`（上传、路径转义、断开 / 换终端不粘贴、拖放、Files 条目）、`shortcuts.test.ts`（完整界面里 Ctrl+B 不触发右侧面板，单窗格和页面里照旧）。
- `scripts/test/web_herdr_full_conflicts_e2e.py`（假 hub：`MOCK_TERMINAL_FULL=1`，假 herdr 现在会像真的一样开鼠标上报和括号粘贴、右键出菜单、把括号粘贴打在提示符后，`herdr_input` 完整记在 `/mock/herdr_client`）：
  右键 → 鼠标报告 + herdr 菜单、无 Persona 菜单、浏览器菜单被压掉（改动前这一条不过）；Shift + 右键 → Persona 菜单、herdr 收不到右键；Mac 上同样；Ctrl+B → `\x02` 给 herdr；Ctrl+K 仍开命令面板；
  滚轮只有 SGR 滚轮报告；左键拖动是报告、Shift + 拖动不发；Ctrl+Shift+V 文字、Ctrl+V 只有文字时仍是 `^V`；Ctrl+V 图片 → 上传 + 括号粘贴的路径；键栏粘贴图片 / 文字；拖入文件（高亮圈）；Files 条目；别的电脑的条目被拒；Mac 的 `paste` 事件带文件。
- `scripts/test/web_herdr_full_conflicts_live.py`（真 herdr，临时会话 `persona-conflict-test`，测完 stop + delete）：右键出 herdr 自己的窗格菜单、没有 Persona 菜单；Shift + 右键出 Persona 菜单、herdr 的不出；Ctrl+V 粘贴图片，路径到了真 herdr 窗格的提示符后。

## 二十七、字符宽度与 CJK 对齐：完整界面里中文表格的右边框歪了（2026-11-05，网页，#2751）

现象（用户截图）：完整界面里 Claude 画的表格，含中文和全角标点的行右边框向右错开 1–2 格，旁边 fish 窗格的左边框成了锯齿状的 `||`。Moshi 的网页端没有这个问题，所以先看了它怎么做：

- Moshi 没有注册任何 Unicode 宽度插件（xterm 默认的 Unicode 6），也没有处理 CJK 字体；它用 WebGL 渲染器（每个格子画在固定的网格位置），字体是随站点自带的 JetBrainsMono Nerd Font Mono，`document.fonts.load` 之后才打开终端。
- Persona 完整界面用 DOM 渲染器（单窗格里的 WebGL 是设置里的可选项，默认关），字体栈是 `"JetBrains Mono", "SF Mono", ui-monospace, …`，字体和 CJK 回退（`cjk.ts`，按 `size-adjust` 缩成正好两格）早就在，`terminalFontReady` 也在打开前等字体。

所以没有照搬「换 WebGL」，先在临时会话里量。

### 量到了什么

环境：herdr 0.9.5 的临时会话（`herdr --session <名字>`，两个并排窗格，左边 `cat` 一张含中文、全角标点、符号、日文、韩文、emoji 的框线表格），假 hub 的 `MOCK_TERMINAL_FULL_EXEC="herdr session attach <名字>"` 把真 herdr 客户端的字节转给浏览器，Chrome 156 无头，字体 JetBrainsMono Nerd Font。测完 `session stop` + `session delete`。

1. **herdr 自己对每个宽字符都先移动光标再写**：抓 `herdr session attach` 的字节，`开` 是 `ESC[6;32H开`，下一格 `ESC[6;34H关`，emoji 后面也是 `ESC[14;38H`。所以 xterm 对宽度的判断**不会让缓冲区的列错位**（逐行比 xterm 缓冲区里所有 `│` 的列：每行都在 25 / 26 / 27 / 56 / 83 / 100 / 101 / 174，和 herdr 一致）。错位不是缓冲区里的，是渲染出来的。
2. **DOM 渲染器下含全角标点的行，边框真的画歪了**：用 Range 量每个 `│` 的屏幕位置（单位：格，减去缓冲区的列）：

   | 行 | 内容 | 第 56 / 83 / 100 / 174 列的边框 |
   | --- | --- | --- |
   | 纯 ASCII、普通汉字、日文、韩文、符号 | `Ctrl+B 是 herdr 的前缀键`… | +0.04 … +0.27（所有行都一样，是单格宽度的取整） |
   | `「开关右侧面板」（快捷键）` | 全角括号 | **−2.84 / −3.81 / −3.78 / −3.67**（向右偏） |
   | `、：；！？ 逗号顿号` | 全角标点 | **−0.85 / −0.84 / −0.82 / −0.70** |

   逐字符看：`「` `」` `）` `，` 各占 **2.97 格**（该是 2），`（` `：` 占 1.97，汉字都是 2.00。
3. **原因是浏览器的 `text-spacing-trim: normal`（Chrome 123+，默认就开）**：全角标点挨着别的全角标点或行首行尾时被「压成半角」。xterm 的 DOM 渲染器给每个字符补 `letter-spacing = 格宽 × 单元数 − 该字符量出来的宽度`，量的方法是把这个字符重复 32 次放进一个隐藏元素（`WidthCache._measure`）：`「「「「…` 里每个都被压成约 1 格（量出 8.03px），于是补了 +7.57px 的字距；真正渲染时 `「` 左右是汉字，没被压，是整整 2 格，加上字距成了 2.97 格。这个偏差随上下文变（挨着另一个标点就又被压），所以才是锯齿，而且偏差大小取决于一行里有多少个全角标点。把 `.xterm` 设成 `text-spacing-trim: space-all` 后，同样的两行回到 +0.04 … +0.27，和 ASCII 行一样。
4. **字体回退不是原因**：`cjk.ts` 把系统中文字体缩成正好两格，汉字（没有标点相邻问题）一直是 2.00 格；`term.options.fontFamily` 重设（清宽度缓存）也不改变偏差。
5. **xterm 的宽度表和 herdr 不一致，但表现不是列错位而是脏格子**：扫了 8266 个字符（Latin-1、Hangul Jamo、2000–2BFF 全部、CJK 符号 / 假名 / 兼容区、PUA 前 512 个、FE10–FE6F、FF00–FFEF、1F000–1FAFF 的已分配字符，另有 ZWJ 家庭、国旗、变体选择符、带声调的字母等三十多个序列），用 herdr 的输出（每个字符后面 `a` 的列距）得到 herdr 的宽度（窄 5268、宽 2997），再用 xterm 的各个宽度表算同一批字符：

   | xterm 宽度表 | 和 herdr 不同的字符数（共 8266） |
   | --- | --- |
   | Unicode 6（xterm 默认） | 1283（几乎都是 herdr 2 格、xterm 1 格：😀 🚀 ✅ ⭐ ⚡ ⌚ 等 emoji） |
   | Unicode 11（`addon-unicode11`） | 160 |
   | Unicode 15 | 99 |
   | Unicode 15 + 字素簇（`addon-unicode-graphemes`） | 88，其中 85 个是 Unicode 15 之后才加的（☰ 等八卦符号、2FFC–2FFF、1FA75 起的新 emoji），另有 3 个是我读字节时的误差（`ㄷ`、`💪` 单独重画一次都是 2 格，和 xterm 一致）；ZWJ 家庭、国旗、`❤️` 之类序列都一致 |

   表现：herdr 以为 emoji 占 2 格，不会去写第 2 格；xterm 按 1 格，那一格保留着之前画过的东西，截图里就是 emoji 后面一块深色底（那一格的背景色是 `rgb(24, 24, 37)`，不是页面的浅色）。换成 Unicode 15 + 字素簇后那一块黑没有了。herdr 没有询问 DEC 2027（字素簇模式），所以它的输出方式不变。
6. **WebGL 渲染器的位置是准的，但不是这里的修法**：同一页临时加载 `WebglAddon` 看了截图：边框是直的、emoji 正常，格宽被取整成 7px（DOM 渲染器是 7.8px），字更挤；没有量像素位置。DOM 渲染器改完 CSS 后位置已经对，WebGL 的外观又和默认不同，按「默认外观不变」不改默认；完整界面里也没有 WebGL 开关，没有为这件事新加。

### 改了什么

- `terminal.css`：`.xterm { text-spacing-trim: space-all; text-autospace: no-autospace }`。前者是量到的原因；后者（中英文之间自动加间距）在 Chrome 156 上量到的默认值已经是 `no-autospace`，没有观察到影响，写上是因为它和前者一样会改变字符宽度、不同浏览器版本默认值不同，这一条**没有在会出问题的浏览器上验证过**。两个 xterm 界面（完整界面和单窗格）共用这份样式，所以单窗格里的中文也一样受益。Safari 对 `text-spacing-trim` 的支持程度没有在真实 Mac 上测过。
- `widths.ts`（经 `xterm.ts` 导出的 `matchHerdrWidths`）：给两个界面的 xterm 加载 `@xterm/addon-unicode-graphemes`（精确锁 0.4.0，MIT，34 KB 的 mjs，随 xterm 一起动态加载，不进主包），它激活 `15-graphemes`。
- 没有动字体栈、行高、`document.fonts.load` 和 `cjk.ts`：量下来它们不是原因，bundled 字体和 CJK 回退也没有必要新加（Moshi 带 JetBrainsMono Nerd Font，Persona 的 `--font-mono` 在没装的机器上回退到系统等宽，字宽由 xterm 量出来的格宽决定，边框列不依赖字体）。

### 没有解决 / 注意

- 「格宽取整」带来的 0.04 … 0.27 格的缓慢漂移（175 列时最右边偏 0.27 格）是 xterm DOM 渲染器量格宽的取整，所有行一样，不会锯齿，没有处理。
- 比 Unicode 15 新的字符（上面的 85 个）xterm 仍按 1 格，herdr 按 2 格，会留下脏格子；等 xterm 出新表再换。
- `a🏿`（非 emoji 后面接肤色修饰符）herdr 给 3 格、xterm 给 2 格，没有处理。

### 验证

- `cd web && npm test`：新加 `widths.test.ts`（默认表里 😀 ✅ 是 1 格，加了之后 emoji / ZWJ 家庭 / 国旗 / `❤️` 是 2 格，汉字和全角标点 2 格，拉丁字母、框线、`⌘ ✓ ★ →` 1 格）。
- `scripts/test/web_herdr_full_cjk_e2e.py`（假 hub 跑它自己当「herdr 客户端」，按真 herdr 的方式逐宽字符定位）：每一行的四条 `│` 和纯 ASCII 行的位置相差不超过 0.25 格（改动后最大 0.08；去掉 CSS 后最大 4.77），emoji 后的第二格没有上一帧的残留（去掉宽度表后出现 `Z`）；浅色、深色各一遍。
- `scripts/test/web_herdr_full_live.py` / `web_herdr_full_conflicts_live.py` 的做法同上（真 herdr 的临时会话），本次的实测过程就是这么做的。

## 二十八、焦点联动：herdr 自己的侧边栏和 Persona 的侧边栏一起动（2026-11-05，网页，#3021）

用户：「persona 的 herdr 界面侧边栏选的会话和最外层侧边栏选的会话好像没联动，moshi 是联动的，你也学一下」。两个方向都没通：
在完整界面里用 herdr 自己的侧边栏 / 标签页切窗格，Persona 的页面和侧边栏不动；在 Persona 的侧边栏、命令面板、/terminal 的树里选另一个窗格，herdr 里的焦点不一定跟过去
（每个会话页各开一个客户端，从 /terminal 里「保持挂着」的另一个终端切回来时，那个客户端的焦点早被别处改走了）。

### Moshi → Persona（读 Moshi 的网页包得到，只读、没运行）

| 问题 | Moshi（`/gateway/events` 与路由代码） | Persona |
| --- | --- | --- |
| 怎么知道 herdr 聚焦的窗格 | 网关的 WebSocket `/gateway[/hosts/<主机>]/events`：页面发 `{watch:{workspaces:true, agent, context, usage, mux}}`，网关推整棵树 `{workspaces:{groups:[…]}}`，树里每个空间（group）、标签页（child）、窗格（pane）带 `focused` 布尔。**没有单独的焦点事件，也没有「谁改的」** | node 订阅 herdr 的 `workspace.focused` / `tab.focused` / `pane.focused`（`events.subscribe`），等一阵事件过去后读一次快照里的 `focused_*`，变了才发 `herdr.focus.changed`；多了一个 Moshi 没有的「谁改的」（下面） |
| 侧边栏怎么跟 | 终端视图里（`view=term`）算出「有效节点」：`rz(状态, URL 里的 node, 树里聚焦的 node)`：URL 变了就偏向 URL，树里的焦点变了（`n !== treeFocus`）就偏向树，`iz` 取其一。侧边栏的高亮 `activeNodeId` 是这个有效节点，**URL 不变**；`_I` 在高亮行不完全可见时把它滚到列表中间（减弱动态时瞬移）。对话视图里不跟 | 跟的是**路由**：整页导航到那个窗格的地址（Agent 的会话页终端视图 / 终端页的 `pane:` 地址），侧边栏本来就按路由高亮、按路由滚到可见行（`Sidebar.tsx` 的 `useGlide`），不另写 |
| 侧边栏点一下怎么让 herdr 聚焦 | 行的 `onClick` 里 `POST /gateway[/hosts/<主机>]/v1/workspaces/focus?mux=…`，body 是 `{workspaceId, tabId?, paneId?, allowParentFallback:true}`（tmux 是 `{session, window?, paneId?}`），发了就不管（不等结果、失败返回 false）；跳转不等它 | 路由变了之后，**打开着的完整界面**（一台电脑一个，见下）经已有的 WebSocket 发 `herdr_focus {id, session_id}`，hub → node → herdr `pane.focus`；同样不等结果，跳转不等它 |
| 防回环 | 靠状态机：点击让 URL 变 → `preferUrl` 为真；点击发出的焦点让树里的焦点等于 URL 的节点，`treeFocus` 没有「变」，不会把页面拉回去。**不区分是谁改的焦点**：电脑上的窗口、别的浏览器改了 herdr 的焦点，终端视图里的页面也会被拉过去（Moshi 没有隔离） | 显式的：node 标出每次变化的来源（`persona` 我们要求的 / `client` 这个 App 的完整客户端刚被敲过 / `other`），hub 对每个视图说「是不是你的」；页面只跟自己的，别处的只出一个小提示；再加一个只在页面里的守卫（`lib/herdr-focus.ts` 的 `FocusGuard`）。下面「防回环」 |

### herdr 0.9.5 给了什么（自己建的临时会话 `persona-focus-test` 里实测，测完 `session stop` + `session delete`）

- **`pane.focus {pane_id}`**（socket 方法，CLI 的 `herdr pane focus` 只有 `--direction` 邻格）：对任何窗格都行（同标签页、别的标签页、别的空间），整条链上的空间 / 标签页 / 窗格都聚焦。第二十四节原先写的「herdr 没有按窗格 id 聚焦」对 0.9.5 不成立；更老的 herdr 回 `unknown variant`，退回 `workspace.focus` + `tab.focus`（`FocusPane`）。
- **事件**：`events.subscribe` 可订 `workspace.focused`、`tab.focused`、`pane.focused`，事件名是下划线的（`workspace_focused`），数据只有 id：`{workspace_id}`、`{tab_id, workspace_id}`、`{pane_id, workspace_id}`。**一次聚焦总是三个一起发**，即使空间和标签页没变（同标签页里换窗格也是三个）；焦点没变（再 `pane.focus` 同一个）一个都不发。事件、快照、`events.subscribe` 的 schema（`herdr api schema --json`）里**没有任何客户端字段**：herdr 不说是哪个客户端改的。
- **快照**（`session.snapshot`）顶层有 `focused_workspace_id` / `focused_tab_id` / `focused_pane_id`，空间 / 标签页 / 窗格各自也有 `focused`。

### 方案

**node**（`server/internal/node/focus.go`，herdr 宿主实现 `host.Focuser`）：

- 另开一条只订这三个事件的 `events.subscribe`（一个连接只认一次订阅，不和生命周期那条挤），事件一到等 30 ms（一次聚焦的三个事件），读一次快照；和上次报的不同才发 `herdr.focus.changed {workspace_id, tab_id, pane_id, session_id?, source, client_id?}`。第一次读只是基线，不发。`session_id` 是那个窗格里的 Agent 会话（没有就是普通 shell）。
- **谁改的**（herdr 不说，所以是启发式，按优先级）：① 我们刚替某个视图要求过聚焦这个窗格（3 s 内，`herdr.client.focus`）→ `persona`，带那个视图的 client id；② 这台 node 上某个完整客户端在 1.5 s 内被**真的敲过 / 点过**（`herdr.client.input` 的字节去掉鼠标移过去（SGR 里无按键的移动，herdr 开着任意移动上报）、`CSI I/O` 焦点事件和页面回给 herdr 的应答（配色、kitty 键盘、DA1、尺寸）之后还有东西）→ `client`，带最近那一个的 id；③ 其他 → `other`（电脑上的窗口、另一个工具）。
  代价和限度：电脑上的窗口和浏览器恰好在 1.5 s 内同时有动作时，会算成浏览器的；两个浏览器各有一个视图时，最近敲过的那个得到它。
- **要求聚焦**：`herdr.client.focus {client_id, session_id}`（会话 id 或 `pane:` 地址）。同一个视图的请求互相替换（用户在侧边栏里连点，只有最后一个值得聚焦），按顺序一个一个做；窗格必须是宿主列出的（`terminalPane`），视图必须是这台 node 上还开着的；先记下「这是我们要的」再调 `pane.focus`，失败就撤掉记号。

**hub**（`hub/herdrclient.go`）：

- 浏览器 → hub：`herdr_focus {id, session_id}`。和视图本身一样**只有 `full` 设备**，且只能是开这个视图的那条连接（`fullOf`）；`session_id` 空或超过 256 字符不转。**是电脑上的状态变更**（herdr 的焦点是整个会话共享的，电脑上的窗口也会跳），所以没有新的 HTTP 路由，`route_scopes.txt` 不变，扩展令牌连 WebSocket 都进不去，`answer` 设备开不了视图所以也发不了。
- node → hub：`herdr.focus.changed`。hub 对这台电脑上**每个视图**发一条 `herdr_focus {id, machine_id, workspace_id, tab_id, pane_id, session_id?, source, mine}`；`mine` = 这是这个视图的客户端（或要求）干的。不认识的 `source` 当成 `other`。feature `herdr_focus`。

**网页**：

- **一台电脑一个客户端**（`terminal/full-registry.ts`）。以前每个会话页 / 每个保持挂着的终端各有一个 `FullFeed`（一个 herdr 客户端），换会话 = 关一个开一个。现在 `FullFeed` 按电脑共用：`FullTerminal` 挂上时 `acquireFeed(mid)`，在前面的那一个 `claim`（xterm 的 DOM 搬进它的盒子、按需 `herdr_resize`、`herdr_focus` 到它的地址），让出时 `yield`（1.2 s 内没人接手才放手客户端；换成单窗格是立刻放手），最后一个页面卸载后再留 1.5 s 才销毁。
  所以会话页 → 会话页、会话页 ⇄ 终端页（都是整个页面换掉）之间**xterm 和屏幕都是同一个**，客户端不重开，屏幕不闪，切换就是一条 `herdr_focus`。
- **上限和保活的变化**：每台电脑 4 个客户端的上限（`fullMaxClients`）还在，但一个浏览器页面对一台电脑只用 1 个，不再随打开过的会话数涨；终端页「每台电脑保持挂着最近 2 个」对完整界面不再多开客户端（只有一个在用，另一个只占着位置），对单窗格还是各自一条流。换到另一台电脑会为它开自己的客户端，原来那台在 1.2 s 后放手。
- **herdr → 页面**（`lib/herdr-focus.ts`，`decideFocus`）：这个视图在前面且连着时，`source=client` 且 `mine` 的变化**整页导航**（`replace`，不堆历史）：Agent 窗格 → 它的会话页终端视图（`terminalTarget`，进不了终端的会话和普通 shell 走 `pane:` 地址的终端页）；侧边栏按路由高亮并滚到可见。
  **别处的变化不拖页面**：电脑上的窗口（`other`）、别的浏览器的视图（`client` 但 `mine` 为假）、别的浏览器的侧边栏（`persona` 但不是我的）、这个视图没在前面时，只在工具栏出一个小标「焦点在别处」（橙点），点一下才跟过去。
- **页面 → herdr**：路由换到同一台电脑的另一个会话 / 窗格、并且那一页显示着完整界面时，`claim` 里发 `herdr_focus`。从侧边栏 / 命令面板 / 终端页的树选都一样，因为它们都是导航。选的会话上次是在对话视图（每个会话记住上次的视图）就不显示完整界面，也就不聚焦。
  hub 没有 `herdr_focus`（旧 hub）时不发，客户端和屏幕照样跨会话保留。

### 防回环

- **我们发的焦点回来了**：node 把它标成 `persona` + 请求视图的 id，hub 给那个视图 `mine=true` → `decideFocus` 得 `echo`，不导航；页面另外记着 3 s 内自己发过哪个地址（`FocusGuard.wasSent`），node 丢了记号（重启）时 `source` 不是 `persona` 也当成回声。
- **跟随导致的导航不再发焦点**：`followTo` 先 `FocusGuard.noteFollow(地址)` 再导航，新页面 `claim` 时 `consumeFollow(地址)` 为真就只记下「herdr 已经在这儿」，不发 `herdr_focus`（一次性，2 s 内有效）。
- 页面已经在那个地址上（`same`）什么都不做；`mine` 的变化只在视图在前面且已连上时跟。

### 实测

- **真 herdr 0.9.5，临时会话**：`PERSONA_LIVE_FULL_SESSION=persona-focus-test go test -tags herdrlive -run TestLiveFocusSync ./internal/node/`（`focus_live_test.go`：node 的焦点监视连着临时会话；`herdr tab focus` 来自另一个 API 客户端 → `other`；`onFocusRequest` → `persona` 且带请求视图的 id；一个**真的完整客户端**（`session attach` 在 PTY 里）里发 herdr 的前缀键 `ctrl+b` 加 `1` → `client`、带那个客户端的 id、落在第 1 个标签页；每一步之后 500 ms 内没有多余的事件）通过；测完会话已停掉并删除，没碰默认会话。
- **假 hub + 浏览器**（`scripts/test/web_herdr_focus_e2e.py`，用法见脚本开头）：侧边栏选另一个会话 → 一条 `herdr_focus`（约 160 ms 内发出），没有 `herdr_open` / `herdr_close`，xterm 元素还是原来那个，回声不导航；`/mock/herdr_focus` 模拟「这个视图的客户端改了焦点」→ 页面到那个 Agent 的终端视图、侧边栏高亮并在可见范围、页面没有发 `herdr_focus`；普通 shell → `/terminal/<机器>/pane%3A…`；别处的改动（`other`、别的浏览器）不动页面并出小标，点小标跟过去；对话再回终端不重开；换电脑开自己的客户端、原来的放手；没有 `herdr_focus` 的 hub 不发。深浅色 × 宽 / 390 px 截图。

### 测试

- Go：`node/focus_test.go`（输入活动的判定、每种来源的归属、最近敲过的视图、鼠标移过不算、请求聚焦 / 回声、请求被替换、没有视图 / 窗格不在时拒绝、老 herdr 的退路）、`hub/herdrclient_test.go`（权限：只有 full 设备、只能是自己的视图、坏参数不转；每个视图的 `mine`、别的电脑的 node 到不了）、`-tags herdrlive` 的 `TestLiveFocusSync`。
- 网页：`lib/herdr-focus.test.ts`（跟随判定、回声、守卫、路由）、`terminal/full-registry.test.ts`（一台电脑一个、换页不销毁、最后一个走了才销毁）。

### 没做的

- 精确到「哪个客户端」：herdr 没有这个字段，所以是启发式；herdr 以后在事件里带客户端 id 就可以把 `focusSource` 换成精确的。
- 对话视图里的页面跟随 herdr（Moshi 也只在终端视图里跟）。
- 一个标签页里分了多个窗格、而 Persona 的会话页只显示其中一个 Agent 时，herdr 里在这几个窗格间切换会换页面（每个 Agent 窗格一页）；没有 Agent 的窗格走终端页。
