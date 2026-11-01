# 计划：支持 Orca

状态：进行中。第 0、1 步已完成，第 2 步还没开始。做完的步骤在下面「实施顺序」里打勾，全部做完后把设计部分并进
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
  如 `w1:p2`），前缀不在 `host.Kinds`（固定的已知类型：herdr、orca，不看本机注册了哪些）里的都当作 herdr；已知类型但本机没有这个宿主的，查找时报「no <kind> host on this machine」，不会发给 herdr，所以 hub 数据库、`proto.Session`、hook 带的窗格都不用迁移。
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

## 实施顺序

0. [x] **补测试**（2026-11-01 完成，`server/internal/node/herdr_*_test.go`、`herdrfake_test.go`）：给 node 和 herdr 之间的交互补「特征测试」：一个共用的假 herdr socket，按顺序记下 node 发出的每个调用；
   每条测试断言「给定这个 herdr 状态，列出的会话是什么、发给 herdr 的调用序列是什么」。测试通过 node 的请求入口驱动、不碰内部字段，
   第 1 步重构后原样再跑，用来证明行为没变。只加测试，不改业务代码。
1. [x] **只搬 herdr，行为不变**（2026-11-01 合并，`server/internal/host` + `node/hostherdr.go`；单元测试、`scripts/test-herdr.sh` 联调通过；
   部署时 herdr 里没有 Claude 窗格，`live_panes_check.py` 的真实窗格对比还没做，有窗格后补跑；观察一两天再开始第 2 步）：定义接口，把所有 herdr 调用搬到接口后面，`PaneRef` 和带前缀的存储一起做。现有测试、语料测试全过；
   第 0 步的测试只许改 `herdrfake_test.go` 里的「Node adapters」块，三个 `herdr_*_test.go` 一行不动，否则就不算证明了行为没变。
   部署后用 `scripts/live_panes_check.py` 对比真实窗格，确认认会话的结果没变。单独合并、部署，跑一两天再做下一步。
2. [ ] **Orca 第一期**：发现、状态、发消息、纯文本读屏、读屏回答对话框。用一个假的 Orca socket 写测试。
3. [ ] **App**：分组键带宿主类型、按能力隐藏按钮、通用文案。
4. [ ] 之后：Orca 第二期（新建、模式）、第三期（实时终端）。

## 待修（第 1 步合并后再做）

补测试时发现、测试按现状固定下来的问题。现在修会改掉「重构前后行为不变」的基准，所以等第 1 步合并后再单独修，修的时候同时改对应的测试：

- 切到 `bypassPermissions` 时，`tuiSetMode` 按 6 次 shift+tab 还没到就放弃，Claude 停在另一个模式上（不是原来的模式）。应该失败时回到原模式，或者先确认这个模式在循环里（`node/modes.go`）。
- `close()` 在 `pane.list` 失败时直接关整个标签页，分屏里别的窗格一起被关（herdr 宿主的 `Close`，`node/hostherdr.go`）。应该失败时只关这个窗格或报错。
- 原有的 `catchup_test.go` 里 4 条测试在设了 `CLAUDE_CONFIG_DIR` 的环境里失败：测试默认 Claude 配置目录在 HOME 下，应在测试里清掉这个变量（新测试的 `herdrNode` 已经这样做）。
- 其余可疑行为（不一定要修）：herdr 快照失败退回 `agent.list` 时空间 / 槽位名是旧的；`editQueue` 清几行按 transcript 里的队列算，不按 TUI 实际拉回来的。

## 还没定的

- 能力上报放在每个会话上还是每台机器的每个宿主上（倾向后者，会话只带宿主类型）。
- 同一个会话同时出现在两个宿主里（比如 herdr 里 resume 了 Orca 里正开着的会话）怎么显示：现在的设计会列两次，至少要提示「在别处运行」。
- 不在任何宿主里、但进程还活着的会话（普通终端里开的），要不要标成「在别处运行」并禁止发送：只靠 `sessions/<pid>.json` 就能判断，和宿主抽象无关，可以先做。
