# 计划：终端引擎支持 tmux（#1180）

状态：方案，等维护者定（下面「需要维护者定的点」）。定了以后按「分期」实现，做完把设计部分并进 `docs/engines.md` / `docs/plan-orca-support.md`，删掉这份计划。

## 背景

很多人的 Claude Code / Codex 直接开在 tmux 的窗格里。现在这些会话在 App 里只显示「未运行」：看得到对话记录，看不到状态，
发消息会在 herdr 里再起一个 `claude --resume`。tmux 在 Persona 里只间接出现：botmux 的会话跑在它自己的 tmux 里，
由 botmux 宿主（`server/internal/node/hostbotmux.go`）经 `botmux observe` 列出，不直接读 tmux。

宿主接口已经抽出来了（`server/internal/host/host.go`，设计见 `docs/plan-orca-support.md`「宿主接口」），所以 tmux 就是再实现一个宿主：
类型 `tmux`，宿主放在 node 包里（新文件 hosttmux.go），另加一个薄的 tmux 客户端包（新包 internal/tmux：跑命令、解析 `-F` 输出、控制模式的读写）。
宿主之上的公共层（认会话、定状态、发送前检查、能力上报）照旧，不为 tmux 加 `if`。

## 实测（tmux 3.7c，macOS，只在自己起的 `-L persona-1180-probe` 上，做完 kill-server）

下面的结论都在独立 socket 上跑过；没跑过的写明「未验证」。没有在 tmux 里跑真的 claude（状态那几条要在实现时补测）。

| 测了什么 | 结果 |
| --- | --- |
| `list-panes -a -F` | 一次拿到所有 session / window / pane：`#{session_id}` `$0`、`#{window_id}` `@1`、`#{pane_id}` `%2`、`pane_pid`（窗格里的 shell）、`pane_tty`、`pane_current_command`（前台进程名）、`pane_current_path`、`pane_width`×`pane_height`、`pane_dead`、`pane_in_mode`、`alternate_on`、`pane_title` |
| 窗格里进程的环境 | `TMUX=<socket 路径>,<server pid>,<session 序号>`，`TMUX_PANE=%0` |
| 前台进程 | `ps -t <pane_tty>` 里 stat 带 `+` 的就是前台（`sleep 30` 是 `S+`，shell 是 `Ss`）；`pane_current_command` 也给出前台进程名 |
| `capture-pane` | `-p` 纯文本当前屏；`-e` 带 SGR（tmux 按自己的写法重发，如 `\e[31m…\e[39m`）；`-J` 把软换行接回去（250 个字符的长行在 120 列窗格里读回 250）；`-S -` 带上滚动历史（默认 `history-limit` 2000 行） |
| 括号粘贴 | `set-buffer -b <名>` + `paste-buffer -p -d -b <名>`：程序开了 `?2004h` 时收到 `ESC[200~…ESC[201~`，多行是一整段；`-d` 用完删掉缓冲区（之后 `list-buffers` 为空） |
| 按键 | `send-keys -l '<文字>'` 原样输入（`; Enter C-c $HOME` 都不展开）；`send-keys BTab` 和 `send-keys -H 1b 5b 5a` 都打出 `ESC[Z` |
| **复制模式** | 窗格在复制模式（`pane_in_mode=1`）时，`send-keys` 的文字被复制模式吞掉，程序收不到；`paste-buffer` 照常写给程序（复制模式不退出） |
| **同步输入** | 窗口开了 `synchronize-panes` 时，对一个窗格 `send-keys`，同窗口所有窗格都收到；`paste-buffer` 只写目标窗格 |
| 退出后留着的窗格 | `remain-on-exit on` 时程序退出后窗格还在，`pane_dead=1` |
| 新建 | `new-window -d -t <session>: -c <目录> -n <名> -P -F '#{session_id} #{window_id} #{pane_id}'` 直接回新窗格的 id，`-d` 不切走电脑上当前看的窗口 |
| 标题 | 程序用 OSC 2 设的标题进 `pane_title`（`⠸ busy title` 原样）；没人设时默认是**主机名**（不能当会话名显示，也不能发给 hub） |
| 控制模式不改尺寸 | `tmux -C attach` 不给尺寸时窗口保持 120×40（客户端尺寸为空）；加 `-f ignore-size` 也一样 |
| 控制模式改尺寸 | `refresh-client -C 50x20` 把窗口改成 50×20；**控制客户端退出后，没有别的客户端的 session 停在 50×20**，不会自己恢复 |
| 控制模式的流 | `%output %<pane> <字节>`：是程序**原样的输出**（八进制转义），不是重画的网格；`-f no-output` 关掉 |
| 控制模式的推送 | `refresh-client -B 'st:%*:#{pane_title}\|#{pane_current_command}'` 订阅所有窗格，变了就来 `%subscription-changed st $0 @0 0 %0 : ⠸ working\|sleep`；另有 `%window-add`、`%window-renamed`、`%layout-change`、`%session-changed`。命令里的 `#` 要加引号，否则被当成注释（`parse error`） |
| `pipe-pane -O` | 把程序原样的输出写给一个命令，和 `%output` 同一种字节 |
| socket 目录 | 本机 `/tmp/tmux-<uid>/` 里有两个 socket 文件，但没有任何 tmux 进程：**socket 文件在、server 已经不在是常态**（没连它们，只看了目录和 `ps`） |

实验脚本在 worktree 外（`~/dev/persona-1180/probe/`），不进仓库；实现时改写成 `hosttmux_live_test.go`。

## 逐条回答

### 1. 发现会话，多个 tmux server 怎么找

**找 server**：tmux 没有「列出所有 server」的命令，一个 server 就是一个 socket。三个来源合在一起，按 socket 路径去重：

1. 默认目录 `$TMUX_TMPDIR/tmux-<uid>/`（没设时 `/tmp/tmux-<uid>/`，macOS 解析成 `/private/tmp/…`）下的每个 socket：`-L <名>` 起的都在这里。
2. 进程表里 claude / codex 进程的 `TMUX` 环境变量（第一段就是 socket 路径）：`-S <任意路径>` 起的 server 只能这样找到。
   进程表本来就要扫（认会话），Orca 宿主已经有「先 `ps -o pid=,stat=,tty=,args=` 找 claude / codex，再只对它们 `ps -E`」的两步扫法，照用，环境里只取 `TMUX`、`TMUX_PANE`、`BOTMUX_SESSION_ID`。
3. （可选，见待定 1）hub.json 那台电脑的 `machines[].tmux_sockets` 显式列出。

每个 socket 用 `tmux -S <路径> list-panes -a -F …` 问；连接被拒 / 文件不是 socket 就是死的 socket 文件（上面实测是常态），当作没有 server，不记日志（和 herdr-core 的 `ErrNotRunning` 一样）。
不清理别人的 socket 文件。

**认 Agent**：用 tty 对，不靠环境变量。每个窗格的 `pane_tty` 在整台机器上唯一；进程表里 claude / codex 进程的 tty 等于某个窗格的 `pane_tty`、
stat 带 `+`（前台进程组）且不以 `T` 开头（没被挂起），才算这个窗格里的 Agent。这样嵌套自然分得开：
tmux 开在 herdr 窗格里时，claude 的 tty 是 tmux 窗格的；herdr 开在 tmux 窗格里时，claude 的 tty 是 herdr 窗格的，不会被 tmux 认走
（`docs/lessons/agents.md` 记过 tmux server 继承 `HERDR_PANE_ID` 把 botmux 的会话报进 herdr 窗格的事，靠环境变量判断归属是不可靠的）。
TRAEX 照公共层的做法（owner 文件），宿主只给「所有窗格」。会话 id 照公共层：前台 claude 的 pid → `sessions/<pid>.json`。

**tmux 命令本身**：launchd 常驻的 node 只有系统 PATH，所以先 PATH、再 `/opt/homebrew/bin/tmux`、`/usr/local/bin/tmux`（和 botmux 找 `~/.botmux/bin/botmux` 一样）。
客户端和 server 版本差太多时 tmux 报 `protocol version mismatch`：这时对这个 server 报不兼容并在 `node.info` 带一句 `note`，不猜（未验证具体报错文本，实现时在两个版本间测）。

### 2. 空间 / 槽位怎么对应

| Persona | tmux | 说明 |
| --- | --- | --- |
| `space_kind` | `tmux` | |
| 空间 | session | `space_id` = `<server>/$<n>`，名字是 session 名，序号按 `session_created` 排 |
| 槽位 | window | `slot_id` = `<server>/@<n>`，名字是 window 名，序号是 `window_index` |
| 窗格 | pane | 见下面的 `PaneRef` |

- **窗格 id**：tmux 的 `%N` 只在一个 server 里唯一，server 重启后从 `%0` 重来。所以存成 `tmux:<server>/%N`，`<server>` 是 socket（默认目录里的写名字，如 `default`；别处的写完整路径）加 server pid：
  `tmux:default@4242/%3`。hook 从 `TMUX`（第二段就是 pid）和 `TMUX_PANE` 拼出同一个字符串。server 换了 pid，旧绑定自然失效，不会落到新 server 的 `%3` 上（见待定 3）。
  `host.Kinds` 加 `tmux`：herdr 的 id 长成 `w1:p2`，不会以 `tmux:` 开头，存量数据不用迁移。
- **会话组**（`new-session -t` 共用窗口）：同一个窗格会在组里每个 session 下各列一次，按 pane id 去重，归到组里最早建的 session。
- **多个 server** 的空间各自独立（id 带 server），App 分组键已经是「机器 + space_kind + space_id」那一步要做的事（`docs/plan-orca-support.md`「宿主之上的公共层」）。
- 主机名标题：`pane_title` 等于主机名（tmux 的默认）时当作没有标题。

### 3. 读屏和实时流

**读屏**（`Read`）：`capture-pane -p`，tmux 能给的比 Orca、herdr-core 都多，不声明 `PlainScreen`：

| `ReadOpts` | tmux |
| --- | --- |
| `visible` | `capture-pane -p` |
| `recent` | `capture-pane -p -S -<Lines>` |
| `recent-unwrapped` | 再加 `-J` |
| `ANSI` | 再加 `-e`（tmux 重发的 SGR，App 的 VT 子集要用录下来的真实输出核对一遍） |

所以 Claude 的发送前检查（草稿 / 灰字、`ensureSubmitted`）、带颜色的读屏、`/screen/before` 的往上翻都走完整路径，和 herdr 一样。
注意：程序在备用屏时 `capture-pane` 读的是备用屏，历史是主屏的（Claude 默认不用备用屏）；窗格在复制模式时读的是程序的屏幕，不是电脑上翻到的那屏（未验证，实现时测 `-M`）。

**实时流**（终端 tab，`Streamer`）：三条路比较——

| 途径 | 拿到什么 | 结论 |
| --- | --- | --- |
| `capture-pane` 轮询 | 网格快照，带颜色，没有推送 | 只能 1 秒一刷，就是现在的「退回读屏」 |
| `pipe-pane -O` | 程序原样字节；每个窗格只能有一个 pipe，用户自己的 `pipe-pane`（录屏、日志）会被顶掉 | 不用 |
| 控制模式 `tmux -C attach` | `%output` 推送程序原样字节；不给尺寸就**不改窗格尺寸**（实测）；一个连接管整个 server 的所有窗格 | 用它，但不直接转发字节 |

`%output` 是程序的原始输出，不是 herdr observe 那种按网格重画的帧；App 自写的 VT 子集吃不下任意原始输出（`docs/plan-orca-support.md`「herdr-core 宿主」的同一个问题）。
所以做法是：**`%output` 只当「这个窗格变了」的信号，帧用 `capture-pane -e -p` 现取**，加上 `#{cursor_x},#{cursor_y}`、`#{cursor_flag}` 拼成一帧
（整屏 `ESC[2J` + 逐行 `CUP` + 行内容 + 光标位置），30 ms 内的变化合成一次（node 现在的 `termMinGap`）。node 记住上一帧的每一行，只重画变了的行，就是增量帧。
帧的格式和 herdr observe 一样，hub、App 不用改。尺寸取 `pane_width`×`pane_height`，`%layout-change` 后按新尺寸发一个整帧。

**接管**（`Controller`，手机尺寸）：`refresh-client -C` 能改尺寸，但改的是整个**窗口**（同窗口的其他窗格跟着变），而且实测控制客户端走了以后，没有别的客户端的 session 停在手机尺寸，
不像 herdr 那样自动恢复。要做得自己记下原尺寸、node 退出时恢复，node 被 `kill -9` 时恢复不了。建议第一版不做接管，终端 tab 只有「电脑尺寸」镜像（见待定 6）。

### 4. 发消息和按键

实测的两个坑决定了写法：`send-keys` 在复制模式里被吞、在 `synchronize-panes` 的窗口里会打进同窗口所有窗格；`paste-buffer` 两种情况都只写给目标窗格的程序。
所以**所有输入都走命名缓冲区 + `paste-buffer`，不用 `send-keys`**：

- **`Prompt`**（发消息）：先确认 Agent 在前台（进程表里这个窗格的 tty 上有前台的 claude / codex，窗格不是 `pane_dead`），否则 `ErrNoAgent` / `not_delivered`，什么都没输入。
  再 `load-buffer -b persona-<随机> -`（文字从 stdin 进，不受命令行长度限制）+ `paste-buffer -p -d -b persona-<随机> -t <窗格>`：程序开了括号粘贴就是一整段，多行不会在第一个换行提交。
  等 150 毫秒（每行加 10 毫秒，最多 1 秒，和 Orca 一样），再确认一次 Agent 在前台，单独粘贴一个 `\r` 提交。粘贴之后任何一步失败都回 `delivered:unknown`。
- **`SendText`**（只输入不回车）：同样的缓冲区，不带 `-p`（tmux 默认把 `\n` 换成 `\r`，要原样的加 `-r`）。能做，不声明 `TextTyper` 限制。
- **`SendKeys`**：按键名 → xterm 字节，直接复用 Orca 的表（`hostorca_keys.go` 的 `orcaKeyBytes`，挪到公共位置），字节同样走 `paste-buffer`；每个键一次，间隔 40 毫秒（`esc` 不和后面的序列拼成 alt）。
  表里没有的键在按任何键之前拒绝；会打出字的键先做前台检查，回车、esc、方向键、ctrl+c 不查（卡住的窗格也要能打断）。
- **复制模式**：`paste-buffer` 照样送到，不替用户退出复制模式（会改电脑上的画面）；App 看到的读屏是程序的屏幕。
- 缓冲区用完 `-d` 删掉；名字带随机后缀，和用户自己的缓冲区不冲突。失败路径上补一次 `delete-buffer`。

回答提问 / 计划 / 授权：Persona 自己启动的 claude 带 hook，和 herdr 一样走卡片；外面开的 claude 没有 Persona 的 hook，按读屏回答（`screen.ParseDialog` + 按键），
和 Orca、Herdr App 一样，但因为有颜色，草稿判断是完整的。

### 5. 新开会话

`Launcher` 全部能做：

- `Spaces`：所有 server 的 session。`CreateSpace`：在**默认 socket** 上 `new-session -d -s <名> -c <目录> -P -F …`（没有 server 时这一步会起一个，见下）。
  `CreateSlot`：`new-window -d -t <server>/<session>: -c <目录> -n <名> -P -F …`。都带 `-d`，不切走电脑上正在看的窗口（和 herdr 不抢焦点一样）。
- `Run`：粘贴命令 + `\r`。窗格里是用户的 `default-shell`，交互式，`~/.zshrc` 里的启动器照常展开，`docs/launchers.md` 不用改；`jarvisd launch <名字>` 也一样。
- `NameAgent`：tmux 没有「Agent 名」，写成窗格的用户选项 `set -p -t <窗格> @persona-agent j-xxxxxx`，不改标题（claude 会自己改标题）。
- 新建的会话是 Persona 启动的，带 hook（`--plugin-dir`），状态和卡片和 herdr 里一样完整。
- **node 起 server 的环境**：tmux server 的全局环境来自起它的进程。launchd 常驻的 node 起出来的 server 里只有系统 PATH 和 node 的环境，
  之后用户在里面开的窗格都继承它。所以 node 只在**没有任何 server** 时才起，起的时候用登录 shell 的环境（`$SHELL -lc 'tmux new-session -d …'`），并且去掉 Persona 自己的变量（见待定 5）。

### 6. 状态

tmux 不知道窗格里程序的状态。来源按优先级：

1. **Persona 的 hook**（Persona 启动的 claude）：提问、授权、计划 → 需要你；`UserPromptSubmit` / `Stop` → 工作中 / 做完，和 herdr 机器上一样。
2. **标题**：Claude Code 工作时在终端标题前放转圈字符（`hostherdrcore.go` 的 `stripSpinner` 已经按这个剥），盲文转圈 → 工作中（TRAEX 现在的判断）。
   控制模式订阅 `pane_title`，变了马上推，不用轮询（未在 tmux 里对真的 claude 验证，实现时测）。
3. **读屏**：TRAEX 的做法，最多 2 秒读一次：`esc to interrupt` → 工作中，`screen.ParseDialog` 认出对话框 → 需要你，其余 → 做完。
4. transcript 照常补「最后一句」「做完未看」（hub 按看没看过算 `unread` / `idle`，`docs/agent-states.md`）。

Codex 照公共层（`codexTurnOver` 等纠正）。

**hook 认窗格**：`personad hook` 的 `HookPane` 在没有 `HERDR_PANE_ID`、`HC_TERMINAL_ID`、`ORCA_PANE_KEY` 时读 `TMUX` + `TMUX_PANE`，报 `tmux:<server>/%N`。
tmux 开在 herdr 窗格里时 claude 两个都有（`HERDR_PANE_ID` 是继承来的），所以选了 tmux 引擎的机器上 `TMUX_PANE` 优先；
node 只在引擎是 tmux 的机器上记下 tmux 窗格（和 Orca 的做法一样），herdr 机器不受影响。

### 7. 和 botmux 不重复

botmux 的 tmux 会话由 botmux 宿主列，tmux 宿主跳过：窗格里前台进程的环境有 `BOTMUX_SESSION_ID`（botmux 给它起的每个进程都设，`hostbotmux.go` 的 `botmuxPaneEnv`）就不算 Agent。
退路是 session 名以 `bmx-` 开头（测试夹具里 botmux observe 的 `backend.sessionName` 是这样；未对真 botmux 验证）。
这和 botmux 用 herdr 后端时 botmux 宿主让开 herdr 的窗格是反过来的同一条规则：一个会话只归一个宿主。botmux 被关掉（`machines[].botmux: false`）时怎么办见待定 8。

### 8. 引擎选择

- `proto.EngineTmux = "tmux"`，显示名「tmux」，说明「终端里的 tmux」；iOS `MachineEngine`、Android `Engines.kt` 各加一项，网页照 hub 的 `engines` 显示。
- `node.info` 的 `engines`：找得到 `tmux` 命令、并且至少一个 socket 有 server 在答，才是「检测到可用」；只有死的 socket 文件算没检测到。版本取 `tmux -V`。
- `engineHosts("tmux")` 只注册 tmux 宿主（+ 照旧的 botmux）。
- **并列**：`herdr-both` 已经把「多宿主并行刷新、按宿主隔离事件和在线状态」做好了，加一个 `herdr-tmux` 只是多一个组合；但组合会越来越多，
  更好的形状可能是「一组勾选的宿主」而不是枚举。第一期不做，见待定 9。

## 和宿主接口对齐

| 接口 | tmux 宿主 | 第几期 |
| --- | --- | --- |
| `Host`（`Snapshot`、`Agents`、`Agent`、`Read`、`SendText`、`SendKeys`、`Prompt`、`PaneProcesses`、`PaneEnv` = `TMUX_PANE`） | 上面 1–4 | 一 |
| `Organizer`：`RenameSpace` = `rename-session`，`RenameSlot` = `rename-window`，`Close` = 两次 ctrl+c 再 `kill-pane`（最后一个窗格时 tmux 自己关掉窗口） | 能做 | 一 |
| `Launcher` / `SpaceLister` | 上面 5 | 一 |
| `Watcher` | 每个 server 一个控制模式连接（`-f ignore-size,no-output`），订阅标题和前台命令、窗口增删；第一期每秒 `list-panes` 轮询（一次几毫秒） | 二 |
| `Streamer` | 上面 3：`%output` 触发 + `capture-pane -e` 出帧 | 三 |
| `Controller` | 不做（见待定 6） | — |
| `PlainScreen` / `TextTyper` | 不声明（有颜色、能只打字） | — |
| `MetadataReporter` | 不做（herdr 专有） | — |

错误照 `host.Mark`：前台检查不过、窗格已死、server 不在 → `ErrNotDelivered`（`delivered:none`）；server 不在 → `ErrNotRunning`；对话框开着 → `ErrDialogOpen`。

## 分期

1. **第一期**：发现（三个来源中的前两个）、列出和状态（hook + 标题 + 读屏，每秒轮询）、带颜色读屏、发消息、按键、回答对话框、关闭、改名、新建 / 继续、hook 认窗格、botmux 去重、引擎选项（node + 两个 App）。
   覆盖 issue 的验收，终端 tab 退回读屏。
2. **第二期**：控制模式的 `Watcher`（推送，轮询降到 10 秒兜底）。
3. **第三期**：终端 tab 的实时镜像（`Streamer`）。
4. 之后：并列（herdr + tmux）、接管（如果定了要做）。

## 测试方案

tmux 能在测试里完全隔离地跑真的（`TMUX_TMPDIR` 指到 `t.TempDir()`，`-L` 用随机名，结束 `kill-server`），所以主要测真的 tmux，不写假的：

- **宿主测试**（`hosttmux_test.go`，机器上没有 `tmux` 就 `t.Skip`）：在隔离的 server 上起窗格，窗格里跑一个假的 `claude`（测试写的脚本：开括号粘贴、把收到的输入写进文件、按需打印对话框屏幕），
  断言列出的会话、空间 / 槽位、读屏三种来源和颜色、`Prompt` 收到的是一整段括号粘贴加一个 `\r`、按键字节，以及这些边界：
  复制模式里照样送到、`synchronize-panes` 只写目标窗格、`pane_dead` 的窗格不算 Agent、挂起（`kill -STOP`）的不算、会话组只列一次、
  server 重启后旧 `PaneRef`（pid 变了）不会命中新窗格、死 socket 文件不报错、缓冲区用完不留。
- **node 层**：照 `herdr_*_test.go` 的样子从 node 的请求入口驱动（列会话、发送、关闭、新建），确认公共层在 tmux 宿主上不走 herdr 的分支；`HookPane` 的优先级（herdr 嵌 tmux、tmux 嵌 herdr 两种环境）；
  botmux 去重（假 `claude` 带 `BOTMUX_SESSION_ID`）；`engine.use tmux` 切换和 `node.info` 的检测（有 server / 只有死 socket / 没装 tmux）。
- **CI**：GitHub 的 macOS 和 Ubuntu 镜像都带 tmux（未核对当前镜像），没有就跳过并在日志里写明。
- **联调**（`-tags tmuxlive`）：对用户正在用的 tmux 只做只读调用（白名单：`list-panes`、`list-sessions`、`capture-pane`、`display-message`，别的命令发出前就拒绝），
  打印窗格、认出的 Agent、pid、会话 id，和 `sessions/<pid>.json` 对一遍，不打印屏幕内容。会写的操作只在测试自己起的 server 上做。
- **真 claude**：在隔离 server 里用启动器起一个 claude，确认标题转圈、对话框读屏回答、hook 卡片、新建 / 关闭；结论写进 `docs/lessons/`。
- **App**：引擎选项和「检测到 / 没检测到」，iOS 和 Android 深浅色截图；终端 tab 第三期另测（录真实帧做夹具，和 `capture-pane` 逐行比）。
- **验收**（issue 原文）：一台只装了 tmux 的电脑，选 tmux 引擎后列出 Claude / Codex、状态正确、能看、发消息、回答提问 / 授权、新开会话，终端 tab 可用，botmux 会话不重复。

## 需要维护者定的点

每条给了建议；可逆、影响小的（缓冲区命名、超时、间隔）实现时按建议定，不列在这里。

1. **找哪些 server**：建议默认目录 + 从 Agent 进程的 `TMUX` 反查两个来源就够；要不要再加 hub.json 的 `machines[].tmux_sockets` 给「用 `-S` 起、里面还没有 Agent、但想从手机新建」的情况？建议先不加。
2. **控制模式连接能不能常驻**：一个控制客户端会出现在 `list-clients` 里、让 `session_attached` 变成 1、触发用户配置的 `client-attached` hook。
   推送（第二期）和实时流（第三期）都要它。建议接受，用 `-f ignore-size,no-output` 把影响降到最小，并在 `docs/engines.md` 写明；不接受的话就只有每秒轮询，没有实时终端。
3. **窗格 id 带不带 server pid**：建议带（`tmux:default@4242/%3`），server 重启后旧绑定不会指到新的同号窗格；代价是 server 重启后已绑定的会话要靠进程重新认一次（公共层本来就先看进程）。
4. **空间 = session**：建议按 session / window / pane 对应空间 / 槽位 / 窗格；另一种是像 botmux 那样按工作目录分空间（不管 tmux 的结构），对只开一个 session 塞几十个窗口的人更清楚，但和用户在电脑上看到的对不上。
5. **node 能不能替用户起 tmux server**：没有 server 时从手机新建，要 node 起一个默认 socket 上的 server，而 server 的全局环境会被之后所有窗格继承。
   建议允许，但用登录 shell 的环境起；或者干脆不起，没有 server 时提示「先在电脑上运行一次 tmux」（和 herdr 现在的要求一样）。
6. **终端 tab 只做镜像，不做接管**：tmux 改尺寸是改整个窗口，并且不会自动恢复（实测）。建议第一版只镜像；要接管的话需要接受「node 被强杀时窗口停在手机尺寸」。
7. **复制模式下发消息**：建议照样送（`paste-buffer` 送得到），不替用户退出复制模式；另一种是拒绝并提示「电脑上这个窗格在翻看历史」。
8. **botmux 被关掉时**：`machines[].botmux: false` 的机器上，botmux 的 tmux 会话要不要由 tmux 引擎列出来？建议仍然跳过：往里面打字会和 botmux 自己的投递撞上，回复也不会回到飞书话题。
9. **并列**：要不要第一期就支持「herdr + tmux」？建议不，先单选；之后如果做，考虑把引擎从枚举改成「一组勾选的宿主」，而不是再加 `herdr-tmux`、`tmux-orca` 这类组合。
10. **外面开的 claude 要不要装全局 hook**：Orca 往 `~/.claude/settings.json` 装自己的 hook；Persona 现在不碰用户的全局设置。建议继续不装，外面开的会话靠标题 + 读屏，对话框读屏回答。
