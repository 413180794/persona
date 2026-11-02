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
