# 终端：真终端怎么做

「终端」tab 要像一个真终端：完整颜色、光标、备用屏、宽字符 / 中文对齐、断线后接着看，长行在手机上完整显示（换行），
同时**不改电脑上 herdr 窗格的大小**、**会话不离开 herdr**。这里记 2026-10-29 spike 的实测结论和由此定下的方案。

## 一、数据从哪来（herdr 0.9.3 本机 / 0.9.5 hub，协议 22，实测）

| 途径 | 拿到什么 | 结论 |
| --- | --- | --- |
| `pane.read`（socket RPC） | 网格快照：`format` 为 `text` / `ansi`，`source` 为 `visible` / `recent` / `recent_unwrapped` / `detection` | `ansi` 带完整 SGR（粗体、16 / 256 / 真彩色、下划线、反显），中文原样；**没有光标位置**；每行 `\r\n` 结尾、行尾空白去掉。一次读约 0.3 ms（整屏 Claude 4.9 KB）。用来补**滚动历史**。 |
| 事件 `pane.updated` / `pane.scroll_changed` | 窗格元数据变化（标题、滚动范围），`revision` 也只跟元数据走 | **输出本身不触发事件**，不能拿来做推送；`max_offset_from_bottom` 能看出有多少行进了滚动历史。 |
| `herdr terminal attach <terminal>`（隐藏命令） | 交互客户端的字节流 | **不用**：每个终端只有一个 attach 槽位（Ovis 装在同一台电脑上会互相抢），而且交互客户端会把终端改成自己的尺寸 = 改电脑上窗格的宽度。 |
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

**herdr 就是那个「真终端仿真器」**：程序的原始字节进的是 herdr 的仿真器，Jarvis 镜像它算好的网格（observe 流），
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
  App `ios/Jarvis/Views/Session/Terminal/`（`TerminalGrid` 解析、`TerminalFeed` 订阅 / 续传 / 回退、`TerminalView` 渲染、`TerminalWrap` 折行、
  `TerminalInput` 直接打字、`BoxDrawing`、`TerminalTheme`）。
- 配色默认「墨黑」（近黑底 0x0C0C0E，不随 App 浅色 / 深色变；对照 Moshi 默认纯黑）。终端卡片里的按钮、键栏、键盘都按配色的底色取深 / 浅外观。
  另有深夜（深灰）、Jarvis、Solarized（后两套跟随系统）。
- herdr 把反显（SGR 7）直接换成**它自己主题**的前景 / 背景真彩色发出来（`38;2;40;44;52;48;2;255;255;255`），手机上看到的是 herdr 的反显色，不随 Jarvis 主题变。
  普通的 16 / 256 色保持索引，由 Jarvis 主题决定。
- 框线字符（U+2500–257F）和块字符（U+2580–259F）不用字体画：回退字体的这些字形比格子窄，框线会变成虚线。按格子画线（圆角用二次曲线），
  同一格的几段合成一条路径一次描边，否则半透明（dim）的颜色在交叠处会更亮、看起来还是虚线。
- 有 emoji 形式的符号（⏺ ⏵ 等，Claude Code 常用）画的时候加 U+FE0E 要文字形式，否则会画成彩色 emoji，不是格子的颜色。
- 屏幕只画到最后一行有字的地方或光标处：`clear` 后提示符在第一行、下面四十多行是空的，跟着末尾会只看到空白。
- 宽字符宽度按 East Asian Width（Wide / Fullwidth）+ emoji 表现形式算；和 herdr 算的不一致时只错一行的后半段，下一次 CUP 就对齐了。
- 实测数据：真实帧的夹具 `ios/JarvisTests/Fixtures/terminal-observe.jsonl`（草稿窗格跑示例脚本录的，没有个人信息）；
  `go test -tags herdrlive -run TestLiveObserve ./internal/herdr/` 在有 herdr 的机器上验证 observe 本身。

## 六、字体（拍板 D）

默认 **JetBrains Mono**（OFL-1.1，v2.304 的 Regular / Bold 两个 ttf，共约 550 KB，`ios/Jarvis/Resources/Fonts`，许可证全文在
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

