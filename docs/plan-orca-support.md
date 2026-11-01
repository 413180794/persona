# 计划：支持 Orca

状态：进行中。第 0、1 步已完成，第 2 步还没开始；herdr-core 宿主第一期已写完（「herdr-core 宿主」）。做完的步骤在下面「实施顺序」里打勾，全部做完后把设计部分并进
`docs/architecture.md` / `docs/server.md`，删掉这份计划。

## 背景

Jarvis 目前只能驱动开在 herdr 里的 Agent。开在别处（Orca 的终端、普通终端）的 Claude Code 正在跑，App 里也只显示「未运行」：
看得到对话记录，看不到状态，发消息会在 herdr 里再起一个 `claude --resume`，和原来的进程同时写同一个会话。

想接入 Orca，又不想到处加 `if orca`：先抽出一层「宿主」，herdr 和 Orca 各实现一份，以后接 tmux 之类也只是再实现一份。

## 两个概念

本文和代码里固定用这两个词：

- **宿主（host）**：终端开在哪里，比如 herdr、Orca。宿主负责有哪些窗格、读屏、发输入、开关窗格。
- **Agent**：窗格里跑的是什么，比如 Claude Code、Codex、TRAEX。Agent 决定对话记录在哪、会话 id 怎么认、状态怎么判断、对话框长什么样。

现在两者混在一起：TRAEX 实际上是「herdr 宿主里一种 herdr 认不出的 Agent」，却是在 herdr 的代码路径里打补丁接进来的（`node/traex.go`）。
分开以后，「Orca 里的 Codex」「herdr 里的 TRAEX」都只是宿主和 Agent 两张表的组合。

## 现状：和 herdr 绑死

- node 直接持有 `*herdr.Client`（`node/node.go`），内部的会话模型就是 `map[string]herdr.Agent`，没有任何接口。
- 每个功能都直接调 herdr：快照和事件流（`sessions.go`、`herdrevents.go`）、认会话（`setAgents`、`sessionFromProcess`）、
  启动（`control.go` 的 `start`、`waitReady`）、发送和发送前检查（`prompt`、`checkPane`、`ensureSubmitted`）、
  切模式 / 思考强度（`modes.go`）、改名关闭、终端的 observe / control / scroll（`terminal.go`）。
- 窗格 id 没有类型：内存里的绑定 `n.panes`、hub 数据库 `node_sessions.pane`、hook 带的 `HERDR_PANE_ID` 都默认是 herdr 的窗格。
- `proto.Session.SpaceKind` 已经预留，但 node 只会写 `"herdr"`；App 解码了这个字段但从来不读，分组键只有「机器 + 空间 id」。
- App 里有不少写死 herdr 的文案（新建对话、空间页、终端页的「约 1000 行」等）。

## 宿主接口

代码在 `server/internal/host`（接口、`PaneRef`、按类型存放宿主的 `Registry`），herdr 的实现是 `server/internal/node/hostherdr.go`
（node 里唯一引用 herdr 客户端的文件）。核心部分每个宿主都要实现；可选能力用 Go 的接口断言判断有没有。方法按 node 实际的调用点定，
herdr 实现里每个方法就是一个 herdr 调用（`Close` 是先数标签页里的窗格再关）。

```go
type Host interface {
    Kind() string                                        // "herdr" | "orca"
    Version(ctx) string
    Snapshot(ctx) (*Layout, error)                       // spaces, slots, agent panes, every pane
    Agents(ctx) ([]Agent, error)                         // agent panes only (herdr without snapshot)
    Agent(ctx, PaneRef) (*Agent, error)                  // one pane's agent + state
    Read(ctx, PaneRef, ReadOpts) (string, error)         // visible | recent | recent-unwrapped, plain | ANSI
    SendText(ctx, PaneRef, text string) error
    SendKeys(ctx, PaneRef, keys ...string) error         // Jarvis-wide key names; each host translates
    Prompt(ctx, PaneRef, text string) error              // host's own submit; ErrNoAgent → caller types + enter
    PaneProcesses(ctx, PaneRef) ([]int, error)           // foreground pids, claude first
    PaneEnv() string                                     // env var naming the pane: "HERDR_PANE_ID" / "ORCA_PANE_KEY"
}

// Optional capabilities.
type Watcher    interface { Watch(ctx, panes []PaneRef, changed func()) error }
type Launcher   interface { Spaces(ctx) ([]Space, error); CreateSpace(ctx, cwd, label) (*Created, error)
                            CreateSlot(ctx, space, cwd, label) (*Created, error); Run(ctx, PaneRef, cmd) error
                            NameAgent(ctx, PaneRef, name) error }
type Organizer  interface { RenameSpace(ctx, space, name) error; RenameSlot(ctx, slot, name) error
                            Close(ctx, PaneRef, space, slot string) error } // the whole slot when it is the last pane
type Streamer   interface { CanObserve() bool; PaneSize(ctx, PaneRef) (cols, rows int, err error)
                            Observe(ctx, PaneRef, cols, rows int, fn func(Frame)) error }
type Controller interface { CanControl() bool
                            Control(ctx, PaneRef, cols, rows int, takeover bool, fn func(Frame)) (Control, error) }
```

- **窗格引用 `PaneRef`**：宿主类型 + 宿主自己的 id。存储和传输时写成 `<kind>:<id>`；herdr 的窗格不加前缀（herdr 的 id 本身带冒号，
  如 `w1:p2`），前缀不在 `host.Kinds`（固定的已知类型：herdr、orca、herdr-core，不看本机注册了哪些）里的都当作 herdr；已知类型但本机没有这个宿主的，查找时报「no <kind> host on this machine」，不会发给 herdr，所以 hub 数据库、`proto.Session`、hook 带的窗格都不用迁移。
  node 里的窗格一律存这个字符串（`n.panes`、`Agent.PaneID`），调用时由 `Registry.Pane` 解析出宿主和 id。
- **会话模型**：node 内部是 `map[string]host.Agent`（字段和原来的 herdr Agent 一样，`PaneID` 是 `PaneRef` 字符串），宿主返回的
  `Agent`、`Created` 里的窗格都已经是本宿主的 `PaneRef`。
- **按键名**：沿用 App 现在发的 herdr 写法（`shift+tab`、`ctrl+c`、`enter`……）作为 Jarvis 统一的按键名。herdr 原样转发，
  Orca 换成字节（如 `shift+tab` → `\x1b[Z`）。
- **错误**：宿主用 `host.Mark` 给自己的错误加上 `ErrNoAgent` / `ErrAttachBusy` / `ErrTakenOver`（`errors.Is` 判断），错误文本不变；宿主缺某个能力时 node 返回 `ErrUnsupported`，文本以 `unsupported` 开头（hub 按这个前缀退回读屏）。
- **布局 `Layout`**：空间、槽位、能认出的 Agent 窗格、所有窗格。宿主知道的话顺带给出 Agent 类型、状态、会话 id（herdr 集成的 `agent_session`）。
- **第 1 步还按 herdr 处理的地方**：新建会话、空间改名、`starts` 记下的标签页改名走默认宿主（herdr），因为空间 id 还不带宿主类型
  （第 3 步）；事件流只订阅 herdr（`watchHerdr`，`herdrEvents` 只有一份）；`node.info` 的 `herdr_version` 取 herdr 宿主的版本。
  列表时每个宿主各取一次快照合在一起，某个宿主没响应就沿用它上次的 Agent，全都没响应才算失败。

## 宿主之上的公共层

下面这些现在散在 herdr 的代码路径里，抽出来后所有宿主共用。

| 公共层 | 做什么 |
| --- | --- |
| 认会话 | 宿主给出会话 id 的优先用；否则扫 claude / codex 进程的环境变量，按各宿主的 `PaneEnv()` 找到窗格，再读 `sessions/<pid>.json`（Codex 按线程名，TRAEX 按 owner 文件）。`jarvisd hook` 同时读各宿主的窗格变量 |
| 定状态 | 宿主报了状态就用，没报就按屏幕判断（现在 TRAEX 的做法），已有的纠正（`codexTurnOver` 等）照旧，最后由 hub 换算成统一状态（`docs/agent-states.md`） |
| 发送前检查 | 宿主能读带颜色的屏幕：完整的草稿 / 灰字判断和补回车（`checkPane`、`ensureSubmitted`）；只能读纯文本：按 Codex 现在的方式降级，只查对话框 |
| 能力上报 | 每个会话带上宿主支持哪些能力（实时终端、接管、新建、改名、关闭），App 据此隐藏做不到的按钮 |

node 里的 `n.herdr` 换成按类型存放的一组宿主（`n.hosts`）：列表把每个宿主的窗格合在一起，每个操作按 `PaneRef` 的类型分发。

hub 和 App 要跟着改的：

- `SpaceKind` 写宿主类型；App 的空间分组键、空间改名接口带上宿主类型（否则两个宿主的空间 id 撞上会合成一组、改名改到 herdr）。
- 按能力隐藏按钮；herdr 专用的文案改成通用说法，确实只有 herdr 有的（比如历史约 1000 行）按宿主显示。
- `node_sessions.pane` 存带前缀的 `PaneRef`。

## Orca 来源

以 Orca 1.4.245 的源码为准，对照核实过。

### 怎么连

- 读 `~/Library/Application Support/orca/orca-runtime.json`（0600）：Unix socket 路径和 `authToken`。Orca 每次启动换 token，
  连接失败或报 `unauthorized` 时重读。Orca 没开时文件不存在，宿主报不可用。
- 协议是每行一个 JSON：请求 `{id, authToken, method, params}`，回复 `{id, ok, result | error}`；慢的调用会先来 `{"_keepalive":true}`，跳过。
- 同一个用户就能连，不需要任何 `ORCA_*` 环境变量：经 ssh 起的 node、launchd 常驻的 node 都行。
- 这个 socket 协议没有公开文档，只有内部版本号（`RUNTIME_PROTOCOL_VERSION`，现在是 3）；启动时用 `status.get` 查能力，对不上就不启用。
  直接连 socket，不每次起 `orca` 命令行（轮询时每秒起一个 Node 进程太重）。

### 能力对照

| Jarvis 要的 | Orca 怎么做 | 第一期 |
| --- | --- | --- |
| 列出终端 | `terminal.list`：handle、`tabId`、`leafId`、工作区路径、`agentIdentity` | ✅ |
| 窗格对应会话 | claude 进程环境变量里的 `ORCA_PANE_KEY`（= `tabId:leafId`），再读 `sessions/<pid>.json`。不依赖 Orca 的内部文件 | ✅ |
| 状态 | `worktree.ps` 的 `agents[]`：`paneKey`、`agentType`、`state`（working / waiting / blocked / done），来自 Orca 自己装的 Claude / Codex hook。waiting 当 blocked | ✅ 轮询 |
| 发消息 | `terminal.send {text, enter, agentPrompt, client:{type:"desktop"}}`：认得出的 Agent 停在输入框时自动用括号粘贴 | ✅ |
| 特殊按键 | 只收原始字节，宿主里放一张按键名 → 字节的表 | ✅ |
| 读屏 | `terminal.read {screen:true}`：纯文本、只有当前屏、最多 120 行；不带 `screen` 时可能悄悄换成当前屏，看返回的 `source` | ✅ 纯文本 |
| 回答提问 / 审批 | Orca 里的 claude 没有 Jarvis 的 hook（Jarvis 只在自己启动 claude 时用 `--settings` 带上），走 hub 已有的读屏解析对话框 → 按键回答 | ✅ |
| 新建会话 | `terminal.create {worktree, command}`；普通路径会忽略 `cwd`，用工作区选择器或在命令里 `cd` | 第二期 |
| 切模式 / 思考强度 | 和 herdr 一样驱动 TUI，但读不到颜色，要先确认纯文本够不够认菜单 | 第二期 |
| 实时终端、带颜色读屏 | `terminal.subscribe` / `terminal.multiplex` 只在要配对的加密 WebSocket 上有（Orca 自己的手机 App 用这条），Unix socket 上会报 `requires a streaming transport` | 第三期 |
| 接管尺寸 | `terminal.resizeForClient`（`mobile-fit` / `restore`）在 socket 上能调 | 第三期 |
| 推送变化 | socket 上没有，按 1–2 秒轮询 `worktree.ps` | — |

第三期两条路：给 Orca 提一个 socket 上就能调的「带颜色读屏」方法（倾向这条），或者在 Jarvis 里实现 Orca 的配对和加密 WebSocket。

### 注意

- `orca-runtime.json` 的 token 能完全控制 Orca；claude 进程的环境变量里还有 Orca 的 hook token。node 只读需要的变量，不写日志、不发给 hub。
- `ORCA_TERMINAL_HANDLE` 不一定有（只在预先分配了 handle 时设），认窗格只靠 `ORCA_PANE_KEY`（缺了退回 `ORCA_AGENT_PANE`）。
- Orca 会往全局的 `~/.claude/settings.json` 装它自己的 hook。不在 Orca 里开的 claude 也会执行，只是脚本发现没有 Orca 的变量就退出。
  Jarvis 不碰这些 hook。
- `~/Library/Application Support/orca/agent-hooks/last-status.json` 里也有每个窗格的会话 id（`entries[paneKey].providerSession.id`），
  但这是 Orca 的内部文件，只在进程环境变量读不到时考虑。

## herdr-core 宿主

herdr-core（命令 `hc`）是 Herdr 桌面版背后的终端运行时，和 herdr 同一个作者，但是重写的、不兼容：socket、方法（`terminal.*`）、
环境变量都不同。所以 herdr-core 是一个独立的宿主，类型名 `herdr-core`，窗格存成 `herdr-core:<地址>`（如 `herdr-core:t7`）。
代码：客户端 `server/internal/herdrcore`，宿主 `server/internal/node/hostherdrcore.go`。以 herdr-core 0.1.16 的源码和 `hc schema` 为准核实过。

### 一机一引擎

每台机器只用一个宿主，叫这台机器的「引擎」：herdr（默认）或 herdr-core，用户在 App 里按机器选（配对时、设置 › 机器）。
hub 存下选择，连上 node 时用 `engine.use` 告诉它（接口和存储见 `docs/protocol.md`「引擎」，node 侧 `server/internal/node/engine.go`）。

- **为什么不并列**：最初两个宿主同时注册，评审发现这会拖累 herdr：herdr-core 慢时 herdr 的刷新和发送跟着慢（最多约 4 秒）；
  herdr 挂了而 herdr-core 在时，herdr 的旧 Agent 一直显示在线；herdr-core 的事件引出多余的 herdr 快照；herdr-core 里有 TRAEX 时
  herdr 被迫每秒轮询。只注册选中的那个，这些都不存在。
- **选 herdr**：node 的宿主表、刷新、轮询、事件流和原来完全一样，不建 herdr-core 客户端、不订阅它的事件；唯一的例外是 `node.info`
  问一次 herdr-core 在不在（填 `engines`）。
- **连接时**：hub 对存过选择的机器第一个调用就发 `engine.use`；node 等引擎定下来（`engine.use`、或 hub 直接列会话、或最多 3 秒）
  才开始轮询和订阅事件，所以 herdr-core 的机器连接时不会先闪一下 herdr 的会话。选 herdr 的机器（没选过）不收 `engine.use`。
  选了 herdr-core 时 `node.info` 也不问 herdr，herdr 的版本取 `herdr --version`。
- **运行时切换**：`engine.use` 换掉宿主表，丢掉旧引擎的 Agent、空间名、进程缓存（切换前开始的刷新结果作废），结束所有终端流
  （推 `terminal.end`），停掉旧事件流、起新的，再推一次会话列表。每个事件流有自己的「在线」标记，停掉的旧流不会清掉新流的状态。
- **不带宿主的操作**：新建会话、改空间名这类不从窗格认宿主的操作找这台机器的引擎（`defaultHost`）：选了 herdr-core 时就是 herdr-core，
  它做不到的报 `unsupported`，而不是「这台机器没有 herdr」。选 herdr 的机器不变。
- **嵌套的 herdr**：选了 herdr-core 时，前台是经典 herdr 客户端的终端不算 Agent，即使里面 claude 的 OSC 7501 透了出来。

### 连接方式

- socket 位置和 `hc` 自己的规则一样：`$HC_SOCKET`，否则 `$XDG_CONFIG_HOME/herdr-core/config.toml` 顶层的 `socket`，否则运行目录
  （`$XDG_RUNTIME_DIR/herdr-core`，没有时 `/run/user/<uid>/herdr-core` 存在就用它）下的 `<会话>.sock`，再否则状态目录
  `$XDG_STATE_HOME/herdr-core/<会话>/<会话>.sock`（macOS 一般是 `~/.local/state/herdr-core/default/default.sock`）。会话名 `$HC_SESSION`，默认 `default`。
- 协议是 JSON-RPC 2.0，一行一条。普通调用每次新开一条连接；`terminal.list`、`doc.get` 限 2 秒（每次刷新都调），发送限 20 秒
  （`terminal.send` 要等电脑上 3 秒没人打字），按键限 8 秒，其余 5 秒。错误带稳定的 `data.kind`（`not_found`、`blocked`……）。
- `events.subscribe` 不用分帧模式：普通连接收到回复后只推通知（`event`、每 15 秒一次 `heartbeat`、跟不上时 `missed` 加新快照）。
  45 秒没有任何消息就当连接断了，退避重连。只有 `terminal.attach` 需要 `hello {framed:true}`。
- 只有 socket 文件不在、或连上被拒（没人监听）才算没开（`host.ErrNotRunning`）：立刻答「没有终端」，会话清空，不记日志。
  连接超时之类说明它还在、只是忙，沿用上次的会话。曾经连上过、后来连不上，每段故障只记一行（开始一行、恢复一行）。
  之后打开 Herdr 不用重启 node。

### 映射对照

| Jarvis 要的 | herdr-core 怎么做 | 第一期 |
| --- | --- | --- |
| 列出窗格 | `terminal.list`：只取本机（地址不带 `/`）、`state` 为 `live` 的终端 | ✅ |
| Agent 类型 | `status.app`（程序自己用 OSC 7501 报的词，原样）：`claude-code` → claude，`codex` → codex，其余不算 Agent（留给 TRAEX 认）。还没报过状态时看前台进程 `argv[0]` 是不是 `claude` / `codex`。前台是 shell、经典 herdr 或没有前台进程时不算 Agent：Agent 退出后 herdr-core 会把它最后的状态留在终端上，直到终端自己的程序结束 | ✅ |
| 状态 | `status.state`：working / blocked / done / idle 原样；`error` 和「是 claude 但还没报」记 unknown（error 只说出过错，不说现在在干什么） | ✅ |
| 窗格对应会话 | 前台进程 `foreground.pid` 就是 claude 自己的 pid，读 `sessions/<pid>.json`；herdr-core 不报会话 id | ✅ |
| 空间 / 槽位 | Herdr 打开的终端带标签 `herdr.space`、`herdr.tab`（就是布局文档 `doc.get {name:"herdr-app"}` 里空间和标签页的 id）和 `where`（「空间 / 标签页」显示名）。名字取文档里的名字，空的取 `where`，再空取文件夹名 / 序号；序号按文档顺序。不是 Herdr 开的（`hc run`）按目录归成一个空间 `dir:<目录>`，槽位留空 | ✅ |
| 读屏 | `terminal.read {terminal, lines:0}`：只有纯文本，软换行已接回，只读当前屏。要历史时 herdr-core 会用滚轮事件去滚全屏程序、期间扣住用户的按键，所以 `recent` 也只给当前屏，没有往前翻 | ✅ 纯文本、无历史 |
| 发消息 | 先 `terminal.get` 确认 Agent 还在（退出了就拒绝，免得壳层把消息当命令跑），再 `terminal.send {terminal, text}`：粘贴再回车（程序开了括号粘贴就用），等电脑上 3 秒没人打字。herdr-core 拒收时什么都没输入：`blocked` 回 `dialog_open`，`status_mismatch` / `unavailable` / `finished` / `not_found` 回 `not_delivered`（都是 `delivered:none`，可以重发） | ✅ |
| 特殊按键 | `terminal.keys`：按键名和 Jarvis 的写法一样（`enter`、`esc`、`shift+tab`、`ctrl+u`、`up`……），原样转发 | ✅ |
| 只输入不回车 | 做不到，见下。「文字 + 回车」和 `/effort` 改走 `terminal.send`；别的输入文字（包括要先按键打开输入框的对话框选项）在按任何键之前拒绝（`cannot_type`，中文提示） | 部分 |
| 认 hook 的窗格 | 终端里的进程都有 `HC_TERMINAL_ID`（就是地址）；`jarvisd hook` 在没有 `HERDR_PANE_ID` 时报 `herdr-core:<地址>` | ✅ |
| 推送变化 | `events.subscribe` 一条连接管所有终端，任何变化（大小、keep 和别的机器的除外）都马上取一次快照 | ✅ |
| 实时终端 | 不做，见下；hub 退回读屏 | ❌ |
| 关闭 | 和 herdr 一样先按两次 ctrl+c，再 `terminal.kill`（挂断 → 终止 → 强杀，最多约 3 秒后返回终端当时的样子）。Herdr App 自己关窗格也只调 `terminal.kill`，终端以 `killed` 结束后 App 自己把窗格（只剩它时是标签页）去掉，不调 `terminal.rm`，所以我们也只 kill、留下已结束的记录。已结束或已不存在（`not_found`）的终端算关好了；kill 后还是 `live` 报错 | ✅ |
| 改名 | 空间和标签页的名字在 Herdr App 的布局文档里，归 App 管，不从手机改：`RenameSpace` / `RenameSlot` 报 `unsupported`（「Herdr App 里的空间和标签页暂时不能从手机改名」）。会话标题（`/rename`）照常 | ❌ |
| 新建、接管 | 第一期不做（没有 Launcher / Controller） | 之后 |

### 查清的几件事

- **地址会不会复用**：不会。地址是一个只增不减的计数（`t1`…`t9`、`t10`、`t1b`……），删掉终端也不回收，计数存在状态目录里，重启照旧；
  只有清空状态目录（或它认出是克隆的磁盘）才从 `t1` 重来。所以窗格 id 直接用地址，不加 `created_ms`：清状态是极少的人为操作，
  而且认会话先看进程的 `sessions/<pid>.json`，旧绑定只在进程认不出时才用。
- **能不能读颜色**：不能。`terminal.read` 只有纯文本，带颜色的只有 `terminal.attach` 的首帧。宿主声明 `host.PlainScreen`：
  要颜色的读屏报 `unsupported`；App 要带颜色的屏幕时 node 改给纯文本（行尾换成 `\r\n`）；Claude 的发送前检查只查对话框
  （`screen.Choosing`），不查草稿，因为没有颜色分不清草稿和灰色的提示语。herdr 的路径不变（它不声明 `PlainScreen`）。
  补回车（`ensureSubmitted`）和建议回复（`suggestion.go`）读不到颜色就不做，也用不着：`terminal.send` 自己回车。
- **能不能只粘贴不回车**：不能。`terminal.send` 一定回车，`terminal.keys` 只认按键名（单个字母数字和少数标点，没有任意文本）；
  原始输入只在 `terminal.attach` 的分帧流上有，不带 `observe` 的 attach 会按我们的尺寸改终端大小。所以 `SendText` 报 `unsupported`，
  宿主声明 `host.TextTyper`（`CanTypeText` 为 false）：node 把「文字 + 回车」和 `/effort` 当一条 `terminal.send` 发；其余要输入文字的
  按键请求在按任何键之前就拒绝（自由填写的对话框选项也是：原来会先按键打开输入框、再在输入文字时失败，留下半开的输入框）。
- **流是什么格式**：`terminal.attach {observe:true}` 先发一帧完整重绘（`0x18` 后跟 VT 序列），之后是程序原样的 PTY 输出，没有差量帧，
  只有积压超过 4 MB 才重绘。node 现在的帧是 herdr 按网格重画的子集，App 自写的 VT 子集不一定吃得下程序的原始输出，
  所以第一期不实现 `Streamer`，hub 退回读屏。
- **keep 终端能不能关掉**：能，但只靠 ctrl+c 不行。Herdr App 开的 Agent 终端是 `sh -ic '"$@"; exec "$0" -i' sh <agent>` 并设了 `keep`：
  Agent 退出后壳层接着跑，终端一直是 `live`。`keep` 只在程序「丢失」（被信号杀、holder 没了等）时换成新的 shell，正常退出（有退出码）和
  `terminal.kill` 都是最终结束（herdr-core 源码 `terminals.rs` 的 `keep_preserves_losses…` 测试、`terminal.keep` 的说明）。所以关闭在两次
  ctrl+c 之后一定要 `terminal.kill`。在自己起的实例上实测过：ctrl+c 后终端仍是 `live`、前台是 shell；kill 后是 `finished`，
  过了 keep 的宽限期也没有复活（`TestHerdrCoreLiveCloseKeep`）。

### 注意

- herdr-core 没有 hook，状态只来自程序自己发的 OSC 7501（Claude Code 会发）；不发的程序没有状态。
- herdr-core 不会丢掉调用方已经放弃的 `terminal.send` / `terminal.keys`：它们要等电脑上 3 秒没人打字，Jarvis 超时后仍可能晚些送到。
  按键的超时缩到 8 秒让这段窗口小一点；对话框回答没有在送达前再核对一次版本。
- 已知的小差别（暂不处理）：`config.toml` 写坏时我们的解析和 `hc` 的不一定一致；一次快照最慢约 4 秒（`terminal.list` + `doc.get`，
  只影响选了 herdr-core 的机器）；窗格 id 不带 core 的身份，清空状态后地址从 `t1` 重来（认会话先看进程，影响小）。
- `terminal.list` 也会列出连到这个 core 的别的机器（地址带 `/`），那些机器有自己的 node，这里跳过。
- 联调只用自己起的实例：`HC_SOCKET`、`HC_SESSION`、`XDG_STATE_HOME`、`XDG_CONFIG_HOME` 都指向 `/tmp`，用完 `hc server stop`；
  对默认实例只做只读调用（`terminal.list`、`terminal.get`、`doc.get`），关闭这类会结束终端的调用只在自己的实例上试。见 `server/internal/node/hostherdrcore_live_test.go`（`-tags hclive`）。

## 实施顺序

0. [x] **补测试**（2026-11-01 完成，`server/internal/node/herdr_*_test.go`、`herdrfake_test.go`）：给 node 和 herdr 之间的交互补「特征测试」：一个共用的假 herdr socket，按顺序记下 node 发出的每个调用；
   每条测试断言「给定这个 herdr 状态，列出的会话是什么、发给 herdr 的调用序列是什么」。测试通过 node 的请求入口驱动、不碰内部字段，
   第 1 步重构后原样再跑，用来证明行为没变。只加测试，不改业务代码。
1. [x] **只搬 herdr，行为不变**（2026-11-01 合并，`server/internal/host` + `node/hostherdr.go`；单元测试、`scripts/test-herdr.sh` 联调通过；
   部署时 herdr 里没有 Claude 窗格，`live_panes_check.py` 的真实窗格对比还没做，有窗格后补跑；观察一两天再开始第 2 步）：定义接口，把所有 herdr 调用搬到接口后面，`PaneRef` 和带前缀的存储一起做。现有测试、语料测试全过；
   第 0 步的测试只许改 `herdrfake_test.go` 里的「Node adapters」块，三个 `herdr_*_test.go` 一行不动，否则就不算证明了行为没变。
   部署后用 `scripts/live_panes_check.py` 对比真实窗格，确认认会话的结果没变。单独合并、部署，跑一两天再做下一步。
2. [ ] **Orca 第一期**：发现、状态、发消息、纯文本读屏、读屏回答对话框。用一个假的 Orca socket 写测试。
   另一个宿主：[ ] **herdr-core 第一期**（2026-11-01 写完，未合并；改成一机一引擎）：见上面「herdr-core 宿主」。关闭已支持（2026-11-01）；改名不做（归 Herdr App 管）。之后：只粘贴不回车（等上游加方法）、实时终端、新建。
   宿主表仍能放多个宿主（`PaneRef` 解析、以后的 Orca），但 node 只注册一个引擎，刷新按顺序问；真要并列多个宿主时，要先让刷新不等最慢的那个。
3. [ ] **App**：分组键带宿主类型、按能力隐藏按钮、通用文案。
4. [ ] 之后：Orca 第二期（新建、模式）、第三期（实时终端）。

## 待修（第 1 步合并后再做）

补测试时发现、测试按现状固定下来的问题。现在修会改掉「重构前后行为不变」的基准，所以等第 1 步合并后再单独修，修的时候同时改对应的测试：

- 切到 `bypassPermissions` 时，`tuiSetMode` 按 6 次 shift+tab 还没到就放弃，Claude 停在另一个模式上（不是原来的模式）。应该失败时回到原模式，或者先确认这个模式在循环里（`node/modes.go`）。
- `close()` 在 `pane.list` 失败时直接关整个标签页，分屏里别的窗格一起被关（herdr 宿主的 `Close`，`node/hostherdr.go`）。应该失败时只关这个窗格或报错。
- 原有的 `catchup_test.go` 里 4 条测试在设了 `CLAUDE_CONFIG_DIR` 的环境里失败：测试默认 Claude 配置目录在 HOME 下，应在测试里清掉这个变量（新测试的 `herdrNode` 已经这样做）。
- `terminal_test.go` 的 `TestTerminalControlEndsWhenTakenOrForgotten` 改全局的 `termLease`，同时还有没退出的 `controlLoop` 在读它：`-race` 下偶发报竞争（引擎选择那轮的 worker 跑 `-count=10` 时见过一次，复核时分支和 master 各跑 30 次没复现）。测试结束前要等 `controlLoop` 退出，或把租期改成实例字段。
- 其余可疑行为（不一定要修）：herdr 快照失败退回 `agent.list` 时空间 / 槽位名是旧的；`editQueue` 清几行按 transcript 里的队列算，不按 TUI 实际拉回来的。

## 还没定的

- 能力上报放在每个会话上还是每台机器的每个宿主上（倾向后者，会话只带宿主类型）。
- 同一个会话同时出现在两个宿主里（比如 herdr 里 resume 了 Orca 里正开着的会话）怎么显示：现在的设计会列两次，至少要提示「在别处运行」。
- 不在任何宿主里、但进程还活着的会话（普通终端里开的），要不要标成「在别处运行」并禁止发送：只靠 `sessions/<pid>.json` 就能判断，和宿主抽象无关，可以先做。
