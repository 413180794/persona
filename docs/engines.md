# 引擎：herdr、Herdr App、Orca、Paseo

**一机一引擎**：Persona 通过「引擎」看到并驱动电脑上的 Claude Code / Codex。每台电脑选一个引擎，Agents 里只显示这个引擎里开着的会话；
别处开的会话（其他引擎、普通终端）只显示成「未运行」的对话记录。默认是 herdr，以前的机器不选也照旧工作。

例外是 **「herdr + Herdr App（并列）」**：一台电脑同时在用经典 herdr 和 Herdr App（典型场景：人在电脑本地用终端里的 herdr，
另一台电脑上的 Herdr App 通过 remote 连到这台机器开终端）时选它，两边的会话合并显示。下文「并列」一节有它的边界。

另外，装了 **botmux** 的电脑上，botmux 里的会话会和所选引擎里的会话一起显示（下文「botmux」一节）：botmux 不占引擎选项，也不用选。

**Paseo** 是不带终端的那个：它的 Agent 由 Paseo 通过各家的 SDK 来跑，node 经一个桥接上它，见 `docs/paseo.md`。

下文的「引擎」都指这个选择；接口、存储和 node 怎么切换见 `docs/protocol.md`「引擎」，设计和取舍见 `docs/plan-orca-support.md`。

## 引擎

| 引擎 | 在 App 里叫 | 你的 Claude 开在哪 | 怎么认出来 |
| --- | --- | --- | --- |
| `herdr` | herdr | 终端里运行 `herdr`，在它的窗格里开 `claude` / `codex`（没在运行时 node 按需拉起，见「herdr 没在运行时」） | herdr 的 socket 能连上，或装了 `herdr` 命令 |
| `herdr-core` | Herdr App | Herdr 桌面 App 里开的终端（App 背后的 herdr-core） | herdr-core 的 socket 有回应 |
| `herdr-both` | herdr + Herdr App | 上面两个同时在用 | 两个运行时都检测得到 |
| `orca` | Orca | Orca 桌面 App 里开的终端 | Orca 在运行，而且它的版本 Persona 认得 |
| `paseo` | Paseo | Paseo 里的 Agent（它的桌面 App、`paseo` 命令开的；Claude Code、Codex、ACP 都算） | Paseo 的 daemon 在运行：它 home 里的 `paseo.pid` 写的地址有回应 |

herdr 和 Herdr App 是同一家的两个产品，但互不相通：在 Herdr App 里开的会话 herdr 看不到，反过来也一样。只装 / 只用一个时按实际选；
两个都在用、想在手机上同时看到，选「herdr + Herdr App（并列）」。

## 怎么切换

1. **配对时**：扫码或手动配对成功后，会问「这台电脑用哪个引擎」（hub 所在的那台）。每个选项旁边写着「检测到可用」或「没检测到」，
   必须选一个，默认选中 herdr（之前选过就是之前那个），确认就切过去；没有「跳过」。
2. **以后随时改**：设置 › 机器 › 点进那台电脑 › 「引擎」，选另一个。每台电脑单独选，一台用 herdr、另一台用 Orca 也可以。
3. **机器离线时**也能改，连上后才生效。

iOS 和 Android App 都有这两处入口（Android 的机器页在 Agents：列表设置 › 机器监控 › 点进那台电脑，或树里电脑栏头的仪表按钮，页面上「引擎与连接」一组的「引擎」）。选择存在 hub 上（不在手机里），换手机、网页版、Android 看到的都一样。

## 切换后会怎样

- Agents 里马上换成新引擎里的会话；旧引擎里的会话变成「未运行」，对话记录还在。
- 正在看的实时终端画面、手机接管的终端会断开，要在新引擎的会话里重新打开。
- 不会关掉、也不会改动任何引擎里正在跑的 Claude。

## 各引擎能做什么

| 能力 | herdr | Herdr App | Orca | Paseo |
| --- | --- | --- | --- | --- |
| 列出会话、显示状态（需要你 / 工作中 / 完成未看） | ✅ | ✅ | ✅（每秒查一次） | ✅（Paseo 推送变化） |
| 发消息（多行也是一整条） | ✅ | ✅ | ✅ | ✅（工作中发的插进这一轮） |
| 按键（回车、Esc、方向键……）、打断 | ✅ | ✅ | ✅ | 只有打断 |
| 回答对话框（权限确认、选项） | ✅ | ✅（读屏回答） | ✅（读屏回答） | ✅（卡片，Paseo App 也能答） |
| 从手机关闭会话 | ✅ | ✅ | ✅ | 后端支持（在 Paseo 里归档），App 的按钮暂时只给有终端的会话 |
| 从手机新建会话、继续未运行的会话 | ✅ | ❌ | ❌ | ✅（Paseo 的每个 provider 是一个启动器） |
| 给空间 / 标签页改名 | ✅ | ❌ | ❌ | ❌（会话名后端能改，App 的按钮暂时只给有终端的会话） |
| 实时终端画面、手机接管终端 | ✅ | ❌ | ❌ | ❌（没有终端） |
| 带颜色的读屏、草稿识别 | ✅ | ❌（纯文本，只查对话框） | ❌（纯文本，只查对话框） | ❌（没有终端） |
| 切权限模式 / 思考强度 | ✅ | 支持，未实测 | 支持，未实测 | ✅（端到端实测） |

做不到的操作会直接提示，不会只做一半。Herdr App 和 Orca 里，Claude 不在前台（被挂起、已退出）时发消息会被拒绝、什么都不输入，
提示「Claude 不在前台（可能被挂起），没有发送」或者要你在电脑上重新打开那个会话。

## 并列：herdr + Herdr App

选 `herdr-both` 后，node 同时挂 herdr 和 herdr-core 两个宿主：

- Agents 合并两边的会话；空间 / 标签页、状态、读屏、发消息、按键、关闭都按会话所在的窗格各走各的宿主，和单选那个引擎时一样。
- **从手机新建会话、继续未运行的会话、空间改名走经典 herdr**（能力完整的那个）。在 Herdr App 里退出的会话从手机「接着」时，
  会在 herdr 里开一个新窗格 `--resume`（同一个 Claude 会话 id，对话记录连续，只是换了窗口）；herdr-core 窗格里的实时终端画面、手机接管仍不可用。
- 两边互不拖慢：两个宿主的快照并行拉取（各自有自己的超时，一个卡住不挡另一个）；herdr-core 的事件只刷 herdr-core，
  herdr 的事件只刷 herdr；一边挂了只清自己一边的在线状态。
- 检测：只有 herdr 和 herdr-core 都「检测到可用」时这个选项才标可用（也可以在离线机器上预选，连上后生效；只有一边在时，
  在的一边照常工作，另一边的会话为空）。
- Orca 不进并列（它每秒轮询、开销最大），要和 Orca 并用以后再做。

## botmux

[botmux](https://github.com/deepcoldy/botmux) 把每个飞书话题接成一个 Claude Code 会话，跑在它自己的 tmux 里。它不是引擎：
node 所在的电脑 PATH 上有 `botmux` 时，node 在所选引擎的宿主之外再挂一个 botmux 宿主（`server/internal/node/hostbotmux.go`），
典型场景是工作的 Agent 在 botmux、个人的在 herdr，两边一起看。

- **列出和状态**：每次刷新一次 `botmux observe --include-raw`（所有 daemon 的所有会话，结果 5 秒内复用，发出消息后马上重看；本机约 500 个会话时 0.3–0.9 秒；observe 偶尔失败时沿用上一次的列表）。
  只列 Claude Code 的会话：在跑的按 botmux 的状态（对话框 `tuiPromptActive` 或 botmux 的「需要你」→ 需要你，`working` / `starting` / `analyzing` → 工作中，
  其余 → 做完）；挂起的（`not_running`、没关闭、7 天内动过）显示成「未运行」。botmux 会话 id 就是 Claude 会话 id（botmux 用它拉起 claude），对话记录照旧。
  空间是工作目录（和 Herdr App 没放进空间的终端一样），名字是 botmux 的会话标题。
- **发消息**：交给 botmux 投递（它的 dashboard 的 `POST /api/trigger`，目标是这个会话），不往终端里打字：会话忙时 botmux 排队，
  挂起的会话 botmux 自己续上（不会在 herdr 里另起一个 `claude --resume`；挂起超过 7 天、不在列表里的也一样），回答照常发回飞书话题。
  dashboard 的 token 由 node 跑 `botmux dashboard current` 取，只存在内存里，只连本机 127.0.0.1。消息在 Claude 那边带着 botmux 的任务标签和一段「外部事件」说明。
- **做不到**：读屏、按键、回答对话框（在飞书里答）、关闭、改名、新建、实时终端；App 按能力隐藏或提示。
- **为什么等你、在飞书打开**：botmux 举手（`botmux send --attention`）时带的原因，会话在「需要你」时放进 `attention_reason`，
  App 在列表行和对话页顶上显示；observe 原始数据里的话题链接（`feishuThreadLink`，只认 https）放进 `external_url`，对话页的「在飞书打开」用它。
  两个都是 botmux 的原始字段，不是稳定接口：没有就不显示。
- **不重复**：botmux 用 herdr 后端时，它的会话本来就是 herdr 的窗格，botmux 宿主不再列；Codex / TRAEX 的 botmux 会话第一期不列。
- **同一个会话两边都有**：它也开在 herdr 等引擎的窗格里（以前从手机 `--resume` 过）时，以在跑的窗格为准，botmux 的挂起记录让开。
- **检测时机**：node 连上 hub、知道这台要不要列 botmux 时（见下一条），以及切换引擎时找 `botmux`：PATH 上的，没有就是 botmux 安装时写的 `~/.botmux/bin/botmux`（launchd 常驻的 node 只有系统的 PATH）；之后才装 botmux 的，重启那台电脑的 personad 才会显示。
- **关掉**：hub.json 那台电脑的 `machines[]` 里写 `"botmux": false`（`docs/config.md`；老 node 不认这一项）：不再跑 `botmux observe`、`botmux dashboard current`（正在跑的那次跑完作废），Agents 里没有 `botmux:` 窗格的会话。
  hub 连上时在 `node.configure` 里说开不开；说之前 node 不挂 botmux，装了 botmux 的电脑也先不推会话列表（免得 botmux 的会话先消失再回来）；老 hub 不说，来要会话列表时照旧挂上。
  关掉后，botmux 用 tmux 跑的会话照旧凭对话记录按「未运行」列出，在手机上回它会在引擎里 `claude --resume`，可能和 botmux 自己的那个撞上：这类会话去飞书里回。
- **待办**：按会话 id 挂，和别的会话一样；按项目整组挂时，项目是 botmux 会话的工作目录。
- 实测：`go test -tags botmuxlive -run BotmuxLive ./internal/node/ -v`（只读列表，加一次 dry run 投递：botmux 校验并渲染，不派发）。

## herdr 没在运行时

电脑重启后 launchd / systemd 会拉起 node，但没人打开终端运行 `herdr`，经典 herdr 的 server 不在（#1879）。node 会在需要时自己把它拉起来：

- **什么时候**：node 启动时（等 hub 在 `node.configure` 里说了开关，旧 hub 不说就等 10 秒后当作开），以及从手机新建会话、继续未运行的会话之前。
  只在引擎是 herdr 或 herdr + Herdr App（并列）时，只拉经典 herdr；Herdr App 的 herdr-core 有自己的生命周期，不碰。node 不在轮询里拉：
  拉起只发生在上面这两个时机。
- **怎么起**：`herdr server`（v0.9.5 的 headless server，前台运行），node 把它放进单独的会话（`setsid`）里，node 重启不会带走它；
  Linux 上 node 在 systemd 用户实例下时改用 `systemd-run --user --unit=persona-herdr-server`（停 node 的单元会杀整个 cgroup）。
  等 socket 能连上（最多 20 秒）再接着新建 / 继续。
- **环境要干净**：herdr server 的环境会传给每个窗格和里面的 claude。node 用 `env -i` 只带 `HOME`、`USER`、`LOGNAME`、`TMPDIR`、语言、
  `XDG_*`、`HERDR_SOCKET_PATH`，再用用户的登录 shell（`$SHELL -l -c 'exec herdr server'`，launchd 下没有 `SHELL` 时读账户的登录 shell）起：
  PATH 是登录 shell 的，node 和启动器的 `ANTHROPIC_*`、代理变量一个都不带。herdr 命令先在 node 的 PATH 里找，找不到再问登录 shell。
- **只起一个**：node 内一把锁，外加对 herdr 配置目录的 `flock`（同一账户上另一个 node），拿到锁后再确认一次 socket。
- **开关**：hub.json 的 `machines[].herdr_autostart` 写 `false` 时，node 完全不拉（手动 `herdr server stop` 后就一直停着），不写是开。
  App 里「设置 › 机器 › 引擎」下的「需要时自动启动 herdr」开关是后续（#1879 里拆出去的那一项），现在只能改 hub.json。
- **起不来**：仍然是 `engine_missing`。没装 herdr 时文案和以前一样；装了但起不来时说「自动启动也没成功」，原因（herdr 的退出码和输出的最后几行，
  输出在 `$TMPDIR/persona-herdr-server.log`）放在详情里，也写进 node 的日志。Windows 上不拉，行为和以前一样。

### 重启后续上 Persona 起的会话

herdr server 重启后会恢复工作区、标签页、窗格，**客户端 attach 之后**再用它内置的命令续上 agent（`session.resume_agents_on_restore`，默认开），
对 Claude Code 就是裸的 `claude --resume <id>`。Persona 起的 Claude 用的是启动器（`jarvisd launch <名字>`、~/.zshrc 里的函数）加
`--settings`、`--mcp-config`、Persona 的 hooks，裸 `claude --resume` 把这些全丢了：网关换成了用户自己的订阅，Persona 的 MCP 和 hooks 也没了。

所以 Persona 在 herdr 里起 Claude 时，给那个窗格报一个自己的恢复命令（herdr 的 `resume_argv`，只对这个窗格生效）：
`eval "<Persona 起它时的那条命令，--resume <id>>"`。herdr 恢复时把它敲进窗格的 shell，~/.zshrc 的启动器照样能用。

- 用户在终端里自己开的 claude 不报，herdr 照原样恢复；用户的 herdr 配置（`config.toml`）不改。
- 这一版之前起的、node 重启前起的 Persona 会话（node 的会话记录里有它的启动器）：node 刷新时看到它在 herdr 里跑，就补报一次。
- Claude 在窗格里换了会话（`/clear`），Persona 的 SessionStart hook 到时重报一次新 id：herdr 不会自己跟着改（v0.9.5 实测）。
- 恢复命令不带权限模式、思考强度；路径里有单引号的（herdr 不收带 `'` 的参数）不报，退回 herdr 自己的恢复。
- node 拉起的 headless server 没有客户端 attach，herdr 不会自己续 agent：要等有人 attach（电脑上打开 herdr）。从手机「继续」一个未运行的会话照常可用；
  「被重启中断」的标记和「全部继续」是后续（#1879）。

## 选不了某个引擎时

- **显示「没检测到」**：这个引擎在那台电脑上没运行。herdr 要先在终端里运行一次 `herdr`；Herdr App、Orca 要打开 App。打开后过一会儿，
  或者在 App 里对那台机器「重新连接」，检测结果会刷新。
- **「herdr + Herdr App」显示「没检测到」**：两个运行时没有都在跑（herdr 要在终端里运行过，Herdr App 要打开、它的 remote 要连着这台机器）。
  只有一边在用时直接选那一边就行。
- **Orca 显示「这个版本的 Orca 和 Persona 不兼容」**：Orca 和 personad 的版本对不上，把两边都更新到最新版。
- **Paseo 显示「没检测到」**：打开 Paseo 的桌面 App，或者运行 `paseo daemon start`。Paseo 用了自定义 home 时，给 personad 设 `PASEO_HOME`。
  显示可用、下面却写着连不上的原因时，见 `docs/paseo.md`「排错」。
- **选了引擎却看不到会话**：确认 Claude 真的开在这个引擎里（比如开在普通终端或另一个 App 里，Persona 只会把它显示成「未运行」）。
- **hub 机器上的 node 是旧版本**（机器信息里提示版本不一致）：旧 node 不认识引擎切换，照旧只用 herdr；先更新那台电脑的 personad。
