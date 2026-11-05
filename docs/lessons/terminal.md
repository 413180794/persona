# 经验：终端

踩过的坑、怪异行为和当时的取舍（原来在 `CLAUDE.md`「经验」，索引见 `docs/lessons/README.md`）。新经验写进对应的节，没有合适的就新开一节。

- herdr `pane read --source recent` 最多返回约 1000 行（--lines 再大也一样），没有偏移或滚动接口，也不落盘：终端历史只能分段读这 1000 行，全量在 transcript。
- 要分清单击 / 快速双击 / 按住的键（终端键栏的修饰键）别用 `DragGesture(minimumDistance: 0)` 记按下松开：快速双击的第二下它收不到
  （XCUITest 的 doubleTap 只来一对 down / up）。用一个覆盖在上面的 UIView 重写 touchesBegan / Ended（`TerminalKeys.swift` 的 `TouchArea`）。
- 网格里写宽字符要分两步：左半照常写，右半（宽 0 的占位格）不能走「覆盖宽字符右半就清掉左半」的规则。
  否则在同一位置重写一个宽字符时，刚写的字会被清掉。herdr 流式重画中文正是这样写的，手机上整段缺字（`TerminalGrid.putRightHalf`）。
  改终端解析一律用录制的真实帧回放，和 `herdr pane read` 逐行比（`TerminalWrapTests`）。
- 自定义视图直接收键盘（`UITextInput`）时，`hasText` 要恒为 true，否则空文档时键盘会自动切大写；拼音靠 marked text。
  选定候选时可能来 `insertText`，也可能来 `setMarkedText` + `unmarkText`，两种都要当提交处理（`TerminalInput.swift`）。
- 终端流的帧率上限是 Agent 自己的重画节奏，不在我们的链路上。Claude Code 2.1.312 流式时约 110 ms 重画一次（主要是转圈那一行），
  正文分几次大块整屏出；herdr observe 最快每 16 ms 一帧；node / hub 原样转发，只把 30 ms 内的帧合成一帧（`termMinGap`）。
  分段实测见 `docs/terminal.md` 第十节。
- 手机接管终端用 `herdr terminal session control <pane> --cols --rows`（管道，不要 PTY，和 `terminal attach` 共用一个槽位）：
  stdin 关掉、进程被 kill（包括 -9）herdr 都会把窗格放回原尺寸，所以接管进程的生死跟着 node 走就不会卡住电脑。
  herdr 从不给程序的原始字节（attach 也是它重画的网格），接管期间也没有滚动历史：历史只能手机按帧比对自己攒（`TerminalGrid.scrolled`），
  别指望换一个终端仿真库解决。键要走 RPC 的事件（method 在对端并发处理，会乱序）。`docs/terminal.md` 第十一节。
- Claude Code（2.1.312，默认模式）窗口一改尺寸就清掉终端的滚动历史、只重画当前屏；接管期间它的输出也不进 herdr 的历史（回复 70 行，herdr 历史仍是 0），
  用户认为接管前的历史被清掉是正常的，别再去拼 transcript（试过，用户不要）。滚动照 Moshi：手指滑 = 滚轮，经 hub → node → herdr `terminal.scroll`，
  herdr 交给开了鼠标上报的程序（Claude 的全屏渲染会自己往回画对话、出「Jump to bottom」；Claude 2.1.312 的 fullscreen trial 不用配置就可能打开），否则滚 herdr 自己的历史。
  别跟用户说「要配 fullscreen」：Moshi 什么都不用配，是 herdr 客户端在接鼠标。`docs/terminal.md` 第十二节。
  herdr 的 `terminal.scroll` 对开了鼠标上报的程序**一条命令只发一个滚轮事件（`lines` 不管用）**，不给 `column` / `row` 就落在 1;1：
  Codex 把本轮提问钉在第 0 行、那里不接滚轮，所以手机滑了也纹丝不动。node 按行拆成多条、落在手指按下的那一格（没有就屏幕中间），第十六节。
  接管要等终端视图第一次布局量出手机的列 × 行才订阅：第一帧到之前也必须把终端视图放上去（转圈只能盖在上面），
  build 21 用转圈顶替了视图，已连上 hub 时点「终端」永远转圈；UI 测试用 `-openPane terminal` 直接开时先走了 HTTP 读屏，测不出来
  （`TerminalTypingTests/testOpeningTheTerminalWhenConnectedDrivesIt` 先进会话、连上后再点「终端」）。
- herdr 的 observe 流要按窗格本身的尺寸开（`--cols/--rows` 小了是裁左上角，不是重排），窗格变尺寸后要重开；`terminal attach` 会改窗格尺寸、占唯一槽位，别用（`docs/terminal.md`）。
- node 往 hub 写帧和回包共用一个同步写（`rpc.Peer.write` 拿着锁写 stdout）：链路慢时写会卡住，卡住的地方不能拿着别的锁、不能堵住 herdr 的读取。
  慢链路上该合并的是「正在写的时候来的帧」，不只是「30 ms 内来的帧」（`docs/terminal.md` 第十七节）。
- 会话有真实宿主窗格，却报 `has no terminal`，先查 `isAssistantSession`，不要只查 WebSocket 或 Herdr 版本。
  工作目录 `~/.jarvis/assistant` 和 transcript 目录只是无头助手的兜底线索，用户也能在这些目录里恢复一个终端会话；
  当前宿主快照中非空的 `PaneID` 应优先于目录推断，但 node 明确持有的无头助手进程仍优先（避免陈旧窗格覆盖所有权）。
  验证要经 hub 订阅拿到完整终端帧，不能只看 WebSocket 握手成功或会话列表带 `pane_id`。
  Hub 的 `decorated` 也会重新判断 `assistant`，手机 Agents 又按这个字段过滤；只修 node 能打开终端，但列表仍会消失。
  Hub 的列表、发送和重命名分类要共用含 `PaneID` 的判断，验收同时检查 HTTP、普通 WebSocket 和增量 WebSocket 快照中的 `assistant:false`，无窗格助手仍为 true。
- 网页终端里的应用快捷键（⌘K / Ctrl+K 等，#2330）要在 xterm 之前放行：`attachCustomKeyEventHandler` 返回 `false` 时 xterm 在 `_keyDown` 里直接返回，**不** `preventDefault` 也不 `stopPropagation`，
  事件继续冒泡到 window，页面的 keydown 监听才收得到；返回 `true` 的键 xterm 自己处理后会 `stopPropagation`，页面永远看不到，Ctrl+K 因此一直给程序发 `\x0b`。
  放行只认「带 Mod 且已登记」的键（`passesToApp`）；Mac 的 ⌥ 组合会打出别的字符（⌥B → ∫），页面分发时必须 `preventDefault`，否则字符还是会进 xterm 的 textarea。
  按物理键（`event.code`）认数字和标点，字母认 `event.key`，⌥ 组出非字母时才退回 `code`；AltGr（Windows 上是 Ctrl+Alt）要排除，否则打 `@` 会触发 Mod+Alt。
  验证要在真浏览器里看假 hub 的 `/mock/terminal_input`：按 Ctrl+K 后里面不能有 `\x0b`，同时 Ctrl+C / Ctrl+R / Alt+B 要照旧到达。
- `@xterm/addon-search` 带 decorations（才有匹配数和 `onDidChangeResults`）要 `allowProposedApi: true`，否则 `findNext` 抛错；它在比较前先把新选项写进自己的状态，
  所以只切「区分大小写」、搜索词没变时不会重新算高亮，新搜索前先 `clearDecorations()`（`docs/terminal.md` 第二十节）。
- xterm 有焦点时会吞掉它处理的按键（`stopPropagation`）：页面上 window 级的 Esc / 方向键监听（弹出菜单、浮层）收不到，要先让 xterm 失焦；
  接管时 Ctrl+F 是程序的（readline、vim），查找快捷键不能抢它。
- 网页终端拖放（#2333）：`dragover` 里把 `dropEffect` 设成 `none` 浏览器就不再触发 `drop`，放下时的提示（「接管终端后才能粘贴文件」）出不来、只显示禁止光标。要在放下时说明原因的拖动（文件面板的条目）`dragover` 照样给 `copy`，由 `drop` 决定敲不敲。
  独立终端页上文件面板和终端并排时，终端只剩约 100px 宽会一直停在「正在连接终端」（算不出可用的网格）：端到端用 1800px 宽的窗口。
- 完整 herdr 界面（`herdr session attach` 在 node 的 PTY 里，#2357）：herdr 的布局、窗格里程序的尺寸、当前聚焦的标签页都是**整个会话一份**，谁最近有动作（连上、改尺寸、打字）谁说了算——浏览器连着时电脑上的 Claude 按浏览器的大小重排版，电脑上敲一个键又换回去；
  测试这类东西只能在自己建的临时会话（`herdr --session <名字>`，测完 `session stop` + `session delete`）里做，在用户的会话里 attach 会改他窗格的尺寸。`docs/terminal.md` 第二十四节。
- PTY 里的客户端要「视图一结束就没有进程」：关 PTY 主端（挂断）→ TERM 进程组 → KILL 进程组，三步都要等；`yes` 这样的洪流不 ack 时读 PTY 的线程要在 ack 窗口满时停读，否则 node 里无限攒。测试里 `pgrep` 查不到残留才算过。
- xterm 的 DOM 渲染器下行高大于 1 时 herdr 画的框线（`│`）会断成虚线（框线字符画不到行间的缝）；完整界面用行高 1。单窗格的行高 1.15 是老样子，没动。
- Playwright 截切换中间态：一张截图的时间和 220 ms 的过渡差不多长，要在点击后立刻截、事后确认两个界面还都在页面上，没抓到就换个方向再试（`web_herdr_full_e2e.py` 的 `transition_shot`）。
- xterm.js 在 Windows / Linux 上把 Ctrl+V 当成 `^V` 发出去并取消按键，浏览器不粘贴、也没有 `paste` 事件：剪贴板里的截图到不了页面（⌘V 和 Ctrl+Shift+V 才有）。要在 `attachCustomKeyEventHandler` 里自己拦下，先读 `navigator.clipboard.read()`，没有文件再补发 `^V`（`terminal-conflicts.ts` 的 `isCtrlV`，#2700）。
- 完整界面里右键必须 `preventDefault`：xterm 的鼠标上报只管把按下发给 herdr，浏览器自己的右键菜单照样弹出来盖在 herdr 的菜单上（e2e 里看最后一个 `contextmenu` 事件的 `defaultPrevented`）；xterm 的 `rightClickSelectsWord` 在 Mac 上默认开，要关掉。herdr 的前缀键 Ctrl+B 和 Persona 的「右侧面板」（Mod+B，Windows / Linux 上就是 Ctrl+B）是同一个键，全局快捷键派发和 xterm 的按键处理都要放行。（`docs/terminal.md` 第二十六节）
