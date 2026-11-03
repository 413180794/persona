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

- 程序写剪贴板的 OSC 52、响铃到不了手机（herdr 在电脑上处理）。手机上的复制走自己的选择。
- 电脑上翻看历史时，手机看到的也是翻上去的那屏（herdr 只有一个视口）。
- Windows：Ovis 的注释说 herdr 的 attach 只在 Unix 上有，observe 在 Windows 上未核对；起不来就退回轮询读屏。

## 五、实现备忘

- 文件：node `server/internal/node/terminal.go`、hub `server/internal/hub/terminal.go`、herdr `server/internal/herdr/observe.go`；
  App `apple/Jarvis/Views/Session/Terminal/`（`TerminalGrid` 解析、`TerminalFeed` 订阅 / 续传 / 回退、`TerminalView` 渲染、`TerminalWrap` 折行、
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
- 实测数据：真实帧的夹具 `apple/JarvisTests/Fixtures/terminal-observe.jsonl`（草稿窗格跑示例脚本录的，没有个人信息）；
  `go test -tags herdrlive -run TestLiveObserve ./internal/herdr/` 在有 herdr 的机器上验证 observe 本身。

## 六、字体（拍板 D）

默认 **JetBrains Mono**（OFL-1.1，v2.304 的 Regular / Bold 两个 ttf，共约 550 KB，`apple/Jarvis/Resources/Fonts`，许可证全文在
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
  这个测试就失败（干净的 master 上也一样）；没有 Simulator.app 时改不回来，新建一台模拟器跑。

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
