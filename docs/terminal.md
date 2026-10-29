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
