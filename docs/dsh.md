# DSH（DeepSeek Harness）

DSH 的会话出现在「Agents」里：列表和状态、对话内容（文字、思考、工具调用和结果、图片）、逐字输出、发消息、中断、新建会话、
从手机关闭（归档），手机 / 手表上回答 DSH 的工具授权、提问和计划审批；DSH 的 Agent 能用手机工具（`phone_*`）和邮箱工具。
DSH 没在运行时，伴随进程从磁盘列出它的会话，给其中一个发消息就把 DSH 叫起来接着跑。人回到电脑前，在 DSH 的 Web GUI 里接着用的是同一个会话。

2026-10-31 起支持（issue #169）。DSH 是第一个按「runtime」规范接入的（`docs/runtimes.md`）：node 侧是通用的 `server/internal/node/runtimes.go`，
DSH 侧是插件 `integrations/dsh/`，建在通用的 `integrations/runtime-sdk/` 上，插件里只剩 DSH 自己的部分（列会话、翻页、事件映射、两条应答链）。
2026-11-02 补齐了手机工具、关闭、DSH 没在运行时的列表，助手和 AI 分身也能跑在 DSH 上（issue #282，见下面「助手」）。

## 为什么不走 herdr

DSH 没有 TUI、不发 OSC 7501，会话日志默认 zstd 压缩，它的 Claude Code 兼容 hooks 也答不了提问和计划审批，
所以「herdr 窗格 + transcript + hooks」接不了，也不该做成一个宿主（宿主是终端开在哪，`docs/engines.md`）。它是一个 **runtime**：
自己管会话、没有终端，连 node 的 `~/.persona/runtime.sock`，自己报会话、推内容、问手机（规范 `docs/runtimes.md`，协议 `docs/protocol.md`「Runtime ↔ node」）。
DSH 这边是一个装进 `dsh web` 的插件，用的是 Web GUI 自己用的那套服务，DSH 核心一行不改。

```
 App ── hub ── node ──┬── herdr（Claude / Codex / TRAEX）
                      └── ~/.persona/runtime.sock ◄── jarvis-dsh-runtime 插件（dsh web 进程里）
                                                    ├─ sessionController：list / page / prompt / cancel / create / rename
                                                    ├─ session/event、agent/assistant-stream、api-session/*
                                                    ├─ approval/request、user-questions/request 两条应答链
                                                    └─ tools.register：hub 的工具（phone_*、mail_*）给 DSH 的 Agent
                                        ◄── jarvis DSH 伴随进程 companion.mjs（runtime `dsh-files`，DSH 没在运行时列磁盘上的会话）
```

## 安装

装在跑 `dsh web` 的那台电脑上，这台电脑要在 hub 的机器列表里（hub 经 ssh 拉起它的 node；不需要装 herdr）。插件和 node 必须是同一个系统用户
（`runtime.sock` 是 0600）。

- **装进 profile**（长期用）：在 DSH 里装 `integrations/dsh` 这个 bundle（Web GUI 的插件管理，或让 DSH 的 Agent 用 `plugin_manager` 的
  `install_bundle`，target 写这个目录的绝对路径）。bundle 自带 `cordis.patch.yml`，装上就插入 `jarvis-dsh-runtime` 一行。
  插件按相对路径用仓库里的 `integrations/runtime-sdk/`，所以从 Persona 仓库的检出目录装（本地目录安装是链接，不拷贝），别单独拷走 `integrations/dsh`。
- **之后的更新是自动的**：`index.js` 只是加载器，插件的代码由 node 带来（下面「自动更新」）。装好（或从没有加载器的老版本换上来）时在 DSH 里重新加载一次，
  以后 hub 更新、node 跟着更新时插件自己换上，不用再去 DSH 里点。
- **不装、先试**：`dsh --profile web --patch <文件>`，文件里插一行，`name` 写 `index.js` 的绝对路径：

  ```yaml
  - insert:
      - id: jarvis-dsh-runtime
        name: /<Persona 仓库>/integrations/dsh/index.js
        config:
          interactions: auto
  ```

插件只在 web profile 里有用（要 `sessionController`）；没有 node 在听时它会一直重试（socket 文件在就每秒一次，不在就最长 30 秒一次），不影响 DSH。
常驻 node 的 `runtime.sock` 不跟 hub 连接走：hub 重连时插件不断开，hub 不在时提问直接给 Web GUI。

同一台电脑上接第二个 DSH（另一个 profile 或 `DSH_HOME`）时给它另配 `runtime`、`name`、`launcher`：node 不让两个进程用同一个 runtime id
（后来的那个连不上，日志里说 `runtime_conflict`，`docs/protocol.md`「Runtime ↔ node」），两个都叫 `dsh` 的启动器也只留先连上的那个。

### DSH 没在运行时（伴随进程）

插件跟着 `dsh web` 进程走：DSH 没在跑，它的会话就不在 Persona 里。要一直看得到，在同一台电脑上常驻伴随进程（launchd / systemd 保活，和 pi 的一样）：

```
node <Persona 仓库>/integrations/dsh/companion.mjs --dsh <DSH 的安装目录或编译过的检出目录> [--home <DSH_HOME>] [--wake '<启动 DSH 的命令>']
```

- 用 DSH 自己的会话存储（`@deepseek-ai/dsh-session-persistence-jsonl`，从 `--dsh` 那个 DSH 里加载）**只读**地列出 `$DSH_HOME/sessions` 里最近的会话
  （和插件同样的 7 天 / 80 个、去掉子 Agent 和已归档的），标题和最后一句取 DSH 的投影缓存，一律列成 `offline`；翻页读会话日志，映射和插件同一份（`map.js`），
  `history` 也一样，所以接手前后手机上是同一段对话。它不写 `DSH_HOME` 里的任何东西（`storages/*.json` 没有跨进程锁，第二个写的进程会弄坏它）。
- 插件连着时它列的会话插件也列，node 认正在跑的那个（`prefer_running`），伴随进程的行退到后面。
- **给离线会话发消息**：伴随进程跑一次 `--wake` 的命令（这台电脑怎么起 DSH，比如 `launchctl kickstart gui/<uid>/<服务名>`、`systemctl --user start <服务>`；
  命令应该很快返回，一分钟内不重复跑），然后把消息**交回** node（`handover`）：node 等插件把这个会话列成运行中，或者问到插件有这个会话，就把消息交给插件发出去，
  最多等 45 秒。没配 `--wake` 时也交回：DSH 在跑就照样送到，不在跑就等到点，回一句「没等到它接手」，消息没有发出。
- 伴随进程不新建会话、不中断、不改名（这些等 DSH 跑起来由插件做），图片要等 DSH 跑起来才看得到。需要 node 支持 `prefer_running` 和 `handover`，旧 node 上它不连。

### 配置

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `interactions` | `auto` | 授权和提问先给谁：`phone` 一律先给手机；`browser` 从不给手机（Persona 只看、只发）；`auto` 见下 |
| `socket` | `~/.persona/runtime.sock` | node 的 runtime socket；环境变量 `JARVIS_RUNTIME_SOCKET` 也行 |
| `listDays` | `7` | 列出最近几天动过的会话（正在跑、在等人的总会列出） |
| `maxSessions` | `80` | 最多列多少个；手机发过消息、新建过的会话（最近 50 个）不受这两条限制，一直列着 |
| `runtime` / `name` / `launcher` | `dsh` / `DSH` / `dsh` | 这个 DSH 在 Persona 里的 runtime id、叫法、启动器名。同一台电脑的第二个 DSH 要另配；改了 id 的会话仍按 DSH 的 Agent 显示（`agent: "dsh"`） |
| `phoneTools` | `true` | 把 hub 的工具（`phone_*`、`mail_*`）给 DSH 的 Agent；`false` 不给 |
| `assistant` | `true` | DSH 的启动器能当 Persona 的助手（下面「助手」）；`false` 不提供 |
| `presets` | 见「助手」 | 助手对话里 Persona 的权限模式对应 DSH 的哪个权限预设，比如 `{default: workspace-write, plan: read-only}` |
| `autoUpdate` | `true`（当 daemon 时 `false`） | 接受 node 带来的插件版本（「自动更新」） |
| `listen` | 不填 | 当 daemon（`docs/daemons.md`）：`{port, host?, path?, token \| token_file}`，不连 `socket`，开一个 WebSocket 端点让 node 来连（默认 `127.0.0.1`、路径 `/jarvis/runtime`）。DSH 跑在别的系统用户下、容器里或别的机器上时用；node 那边在 `daemons.json` 里写同一个地址和 token，名字、启动器以那边为准；当 daemon 时没有手机工具，也不能当助手 |

### 自动更新

插件分两层（通用做法见 `docs/runtimes.md`「自动更新」）：DSH 加载的 `index.js` 是加载器，不再改；插件的代码（`lib/plugin.js`、`lib/dsh.js`…和
`runtime-sdk`）每个版本放在 `~/.persona/runtime-packages/jarvis-dsh-runtime/<哈希>/`，加载器从那里 import。personad 构建时把这些文件打进二进制；
插件连上 node 时报自己的哈希，node 带的不一样就让它来取，插件核对后等手机上没有它的卡片，停掉旧的、换上新的，DSH 的会话和 Web GUI 不受影响
（插件只是旁观者，换的几秒里手机上的列表停一下）。整个过程不经过 DSH 的插件管理和热加载：链接安装的插件不在热加载的监视目录里，换文件不会生效；
插件管理在被手机驱动的会话里还会报 `HMR transactions cannot be nested`（插件在插件管理的事务里启动时，事务的上下文跟着它的连接走了，DSH 的问题）。

- 跑的哪个版本：node 日志 `runtime dsh <版本> #N connected`，插件日志 `jarvis: jarvis-dsh-runtime <哈希> (shipped / from the node)`、`switched to …`；
  `current.json` 记着 node 给的那个。
- 手动装了一份新的（换了检出目录里的文件）：它的哈希和 `current.json` 记的不同，下次加载先跑它；node 带的不一样会再换。
- 不想自动更新：`autoUpdate: false`。当 daemon（`listen`）时默认就不更新，换版本要重启 DSH。

## 映射

| Persona | DSH |
| --- | --- |
| 会话列表 | `sessionController.list`；去掉已归档的（`workspaceRegistry.archivedSessionIds`）和还没开始的空会话（手机上新建的除外）。子 Agent 的会话不单列，挂在父会话的 `subagents` 里（见「子 Agent 和后台任务」）。标题用 `title` 投影，没有就用目录名；空间 = 目录 |
| 状态 | 有授权 / 提问等着（插件挂着的，或 `userQuestions` 投影里还开着的）→ `blocked`；在跑 → `working`；其余 `done`（hub 按看没看过分「完成未看 / 已看」） |
| 对话内容 | `sessionController.page`（不会激活冷会话）。`user/message` 只取 `source.kind = user` 的（DSH 注入的时间、技能、指令上下文不显示）；`assistant/message` 的 `reasoning` → 思考、`text` → 回复、`tool-call` → 工具调用；`tool/result` → 结果（compaction 重新挂出来的不重复显示）；中断 / 出错 / 输出上限的轮次、上下文压缩 → 提示。seq = DSH 的 seq + 1，`history` = `dsh:<会话 id>` |
| 工具名 | `bash` → `Bash`、`read` → `Read`、`edit` → `Edit`、`write` → `Write`、`grep`、`glob`、`web_fetch`、`web_search`、`todo_write`、`ask_user_question` 同理；`subagent` / `subagent_fork` → `Agent`（参数 `{description, prompt}`，App 画成子任务，点开看它启动的子 Agent）。参数名本来就和 Claude 一样（`command`、`file_path`、`old_string`…），App 照常画命令、diff、文件。其他工具原名显示 |
| 逐字 | `agent/assistant-stream` 的 `text-delta`，第一段立刻发，之后最多 120 ms 一次（整段文字） |
| 发消息 | `sessionController.prompt`：`requestId` = App 的 `client_msg_id`（DSH 按它去重，重发不会进两遍）；空闲时 `queue`（开新一轮），工作中 `steer`（插进这一轮，像 Claude 的排队输入）。图片直接带 base64 |
| 中断 | `sessionController.cancel` |
| 新建 | App 新建会话时选启动器 `dsh`（DSH 连着时才有）→ `sessionController.create {cwd}` |
| 改名 | 会话名 → `sessionController.rename`；标签页名忽略 |
| 关闭 | `workspaceRegistry.archiveSession(id, {stopActivity: true})`：和 Web GUI 里归档一样，正在跑的这一轮、子 Agent、后台任务、定时任务一起停；之后不再列出，在 DSH 里可以取消归档 |
| 手机工具 | hub 的工具（node 转来的 `mcp.tools`）注册成 DSH 的工具 `mcp__jarvis__<名字>`，调用经 node 的 `mcp.call` 到 hub，带上调用它的会话；结果的文字是工具结果，出错的结果是工具失败 |

## 授权、提问、计划审批

插件在 DSH 的两条应答链 `approval/request`、`user-questions/request` 上排在最前（`prepend`）。轮到它时：

- **给不给手机**（`interactions: auto`）：最近 6 小时手机给这个会话发过消息或在手机上新建的会话 → 先给手机；没有任何浏览器连着 DSH
  （`typertGateway.hasLiveClient()` 为假）→ 先给手机；否则交给下一个应答方（Web GUI），手机上看不到这张卡。
- **授权**：卡片带工具名、参数（从这一步的 `tool-call` 里取）、要跑的命令（`summary`），DSH 给的原因放 `reason`；node 给卡片标上
  `agent: "dsh"`、`agent_label: "DSH"`，App 按「别的 Agent 的卡」画（「DSH 需要你授权」、同意 / 拒绝，和 Codex 的卡一样），
  不出 Claude 的「本会话总是允许」——DSH 只有「允许一次」，做不到。批准 → `allowed-once`，拒绝 → `rejected`；拒绝时写了话，
  先拒绝，再把这句话 steer 进正在跑的这一轮（DSH 的授权没有理由字段；Codex 也是这样做的），Agent 能读到为什么。
- **提问**：`ask_user_question` 的问题、选项、多选原样上手机；答案按选项文字对回 DSH 的 `{id, selected, custom}`，不在选项里的字当 `custom`。
- **计划审批**：`intent.kind = plan-review` 的问题画成计划卡片，计划正文在 `detail`；批准 = 选它的 `approve` 选项，打回 = 选另一个选项，修改意见放 `custom`。
- **交回电脑 / 过期 / 没有 hub**：调 `next()`，Web GUI 照常弹出。手机拿着卡的时候 Web GUI 看不到它（应答链是一条链，不是广播），
  和 Claude hooks 拦下对话框时电脑上 TUI 不显示是同一个取舍。
- **定时提问**：`ask_user_question` 默认只等 45 秒。到点 DSH 先把「还没回答」交给模型，手机上的卡不撤，之后答了走 `userQuestions.answer`（迟到的回答）。
- **node 重启**：交互的 id 由会话 + 工具调用算出，插件连上下一个 node 后用同一个 id 再问（最多等 2 分钟），手机上还是那张卡。

## 手机工具

hub 连着时（hello 之后、`runtime.hub` 说 hub 回来时，之后每 5 分钟看一次）插件向 node 要 hub 的工具，变了才重新注册，所以 DSH 的 Agent 看到的工具列表
不会无故变动；hub 不在时留着上一份，调用会说 hub 没连上。工具在 DSH 里全局注册，DSH 的所有 Agent（包括子 Agent）都能用；DSH 的工具调用不走 DSH 自己的授权，
会替用户出面的（建日程、发邮件）由 hub 在手机上确认，和 Claude 用这些工具时一样。工具结果里的图片不交给 DSH 的 Agent（结果里说一句）。

## 助手

「Persona」tab 的助手、AI 分身、飞书 `/agent`、定时任务都能跑在 DSH 上：在 设置 › 助手（或顶栏、定时任务、模型配置）选 DSH 的启动器 `dsh`
（DSH 连着时才有，`launchers.list` 带 `assistant: true`）。按 runtime 的通用助手约定接入（`docs/runtimes.md`「助手」），node 侧是
`server/internal/node/assistant_runtime.go`，没有 DSH 专用的代码；DSH 这边是插件的助手模式（`config.assistant`，默认开）。

助手所在的电脑（hub 机器，或 `assistant.machine`）要常驻 `dsh web --no-open`（launchd / systemd，需要 Node 22+），装好插件。

| 能力 | DSH |
| --- | --- |
| 流式输出 | 插件已有：`agent/assistant-stream` → `session.delta` |
| 续聊 / 重启后 | 对话是 DSH 的会话（`session-<uuid>`），DSH 持久化；node 重启不影响（会话按目录认出是助手的），DSH 重启后下一条消息自动恢复 |
| 停止 | `session.interrupt` → `sessionController.cancel`；关闭对话只停正在跑的这一轮，会话留在 DSH 里 |
| 回复过程中插话 | 插件已有：工作中 steer 进这一轮 |
| Persona MCP 工具 | hub 的助手工具（`ask_user`、`notify_user`、`schedule_*`、`agents_*`、`kb_*`…）注册成 `mcp__jarvis__<名字>`，只给助手对话（`tools.register` 的 `when`）；调用经 node 带上「是助手」，node 核对会话归 DSH、是助手对话 |
| ask_user | 同一个工具，会话 id 本来就知道 |
| DSH 自己的提问 / 计划 | 插件已有：提问链，含计划卡片和迟到的回答；助手对话一律先给手机 |
| 命令 / 改文件的审批 | `approval/request` 链，助手对话一律先给手机（DSH 只在出沙箱这类情况才问） |
| 飞书写操作的确认 | 插件命令闸门声明的工具（`proto.CommandGate.Tool`，名字由插件定；node 的 `gateTools` 只在技能在这台机器上时列出）：参数就是命令后面的词，node 先过闸门（`larkCheck`：读放行、写在手机上确认、配置的规则拒绝），再在 node 上跑技能里的脚本，凭据只在 node（#1317 起在 node 的闸门代理上）；凭据不进 DSH，shell 里直接跑命令拿不到凭据，也绕不过 node 的闸门代理。#1571 起 node 不再内置这个工具，由接飞书的扩展声明（`docs/lark-agent.md`） |
| 权限模式 | 每条消息带上，插件设 DSH 的权限预设（`permissionPresets.set`）：默认 / 自动接受编辑 / 自动 → `workspace-write`，计划 → `read-only`，全部放行 → `danger-full-access`（飞书闸门仍在：它在 node 上，不靠 DSH 的授权）；`config.presets` 可以改 |
| 思考强度、模型 | 在 DSH 里选（Web GUI，或 DSH 的默认模型）；Persona 的模型配置对 `dsh` 不选具体模型 |
| 图片 | 插件已有：base64 图片块 |
| 指令 | DSH 读助手目录的 `AGENTS.md` / `CLAUDE.md`（`assistant.prepare` 两份都写，内容一样时只加载一份），加上用户自己的 `$DSH_HOME/AGENTS.md` |
| 插件的 skill（如接飞书的扩展带的） | 助手目录的 `.agents/skills/`（DSH 读这里），命令经上面闸门声明的工具跑 |
| 记忆 | 和 Claude、Codex **同一份**：新对话的第一条消息前面带上记忆目录、格式和当前索引（`prompts/assistant/runtime_instructions.md`），改那个目录里的文件在非计划模式下不问 |
| 后台任务 | DSH 自己的后台任务（`job_*`），在跑的列在会话的 `background` 里（见「子 Agent 和后台任务」）；跑完会不会叫醒对话照 DSH 的规则，node 不另记 |
| 预热 | 不需要：`dsh web` 常驻，新对话直接 `sessionController.create` |
| 实时快照 | 和 Claude、Codex 一样随每条消息带（`<jarvis-context>`，只带变了的节），聊天里不显示（`map.js` 去掉不带属性的 `<jarvis-context>` 块） |

和 Claude 版的差别：要常驻 `dsh web`，`dsh web` 重启会打断正在跑的那一轮；助手的对话也出现在 DSH 的 Web GUI 里（可以在电脑上接着看）；
DSH 的授权只有「允许一次」，飞书闸门的「本会话总是允许」照旧由 node 记；拒绝时写的理由 steer 给模型；一段对话换成 Claude / Codex 要新建对话。

## 子 Agent 和后台任务

DSH 的 Agent 常把活交出去再结束这一轮：后台子 Agent（`subagent` 工具，跑完把结果送回父会话叫醒它）、workflow、后台跑的命令。
这一轮结束后父会话本身是「完成」，但事情还没完。插件把这些挂在父会话那一行上，用 Claude 会话同样的字段（`docs/runtimes.md`「子 Agent 和后台任务」）：

| Persona | DSH |
| --- | --- |
| `subagents` | 父会话的子会话：`sessionController.list` 里 `parentSessionId` 是它的（子 Agent、workflow 派出的 Agent）。`id` = 子会话 id，`running` = 子会话在跑（列表的 `running`、`api-session/status`），`description` = 委派时的 `description`（父会话的 `subagent/catalog`，workflow 的是 `tool-workflow/agent-start` 的 `label`），`tool_use_id` = 启动它的那次 `subagent` 调用（结果「started subagent <id>」），`started_at` / `updated_at`、`last_text`。在跑的全列，其余列最近的，最多 20 个。插件启动前就有的子 Agent，名字和调用从父会话的 `subagentCatalog` 投影和事件里补（每个父会话 5 分钟最多读一次） |
| `background` | 父会话名下还在跑的 DSH 后台任务：`job/registered` / `job/settled` 事件，插件启动前就有的从 `ctx.jobs.list(会话)` 补。工具调用还在等结果的不算（那是前台在跑）；`kind`：`bash` → `shell`（`command` 是命令），其余（workflow…）→ `agent` |
| 子 Agent 的内容 | id `<父会话>@<子会话>`：翻页用 DSH 的子 Agent 地址 `{kind: "subagent", parentSessionId, childSessionId, mode: "unknown"}`（DSH 不让按普通会话读子会话），`history` = `dsh:<父>@<子>`；watch、逐字也按这个 id 推 |
| 状态变化 | 子 Agent 开始 / 结束、后台任务登记 / 结束都会重推列表 |

于是：父会话这一轮结束、子 Agent 还在跑时，hub 把它算作工作中（#2228），点开父会话能看到子 Agent 和后台任务，从那次调用点进去看子 Agent 的过程；
hub 的「完成」推送等这些跑完、父会话被叫醒的那一轮结束后再发（`assistant/fleet.go` 的 `backgroundRunning`）。子 Agent 完成时父会话里有一条「子 Agent 完成」的提示（`map.js`）。

子 Agent 只能看，不能发消息、改名（回 `unsupported`）：它归父会话管，要它干什么在父会话里说。

授权和提问：DSH 委派子 Agent 时把子 Agent 的授权策略钉成 `never`（DSH 的 `captureDelegatedPolicyOverrides`）：子 Agent 要授权的操作直接被拒，`ask_user_question` 以
`DELEGATED_CALLER` 失败，这些请求不经过任何应答链，手机和 Web GUI 都收不到，也不会卡住等人。如果在 DSH 里把某个子 Agent 的策略改回 `ask`，
它的授权卡片挂在**父会话**上给手机（手机列的是父会话），`reason` 前面写明是哪个子 Agent（「子 Agent「<名字>」：…」）；拒绝时写的话不 steer（子 Agent 不收插话）。

伴随进程（DSH 没在运行时）列的离线会话不带子 Agent 和后台任务：DSH 不在，它们也不会在跑。

## 限制

- 没有终端（DSH 没有 TUI），App 不显示终端入口。
- 子 Agent 只能看，不能给它发消息、改名（见「子 Agent 和后台任务」）；早于 DSH 给子 Agent 写 `subagent/descriptor` 的老子会话翻不了页（DSH 不认它的地址），列表里照样有。
- 工具结果里的图片不交给 DSH 的 Agent；伴随进程列出的离线会话里的图片，要等 DSH 跑起来才看得到。

## 验证

- `go test ./internal/node -run 'TestRuntime|TestPollAgentsPublishesWithoutHerdr'`、`./internal/hub -run TestRuntimeLauncher`。
- `cd integrations/dsh && npm test`：映射、分页、发送、watch 的连续性、逐字、授权 / 提问 / 计划 / 迟到的回答、重连；归档和关闭、改名后的 hello / 卡片 id、
  手机工具的注册和调用（`dsh.test.js`）；伴随进程的离线列表、翻页、唤醒和交回、旧 node 上不连（`files.test.js`）。
- node：`go test ./internal/node -run 'TestRuntime'`（含 runtime id 冲突、同一进程重连、启动器只有一个主人、交回的三种结果：`runtimes_identity_test.go`；
  子 Agent 和后台任务的行、`<会话>@<子>` 的调用和推送归父会话的 runtime：`TestRuntimeSubagentsAndBackground`；daemon 前缀：`TestDaemonSubagentIDs`）。
  插件这边：子 Agent / workflow / 后台任务进行、结束，按父会话地址翻页和 watch，子 Agent 的授权和提问挂在父会话上，插件之前就有的从目录和任务表补（`dsh.test.js` 最后三个）。
- 端到端：`DSH_CHECKOUT=<编译过的 dsh 源码> JARVISD=<jarvisd> node integrations/dsh/e2e/run.mjs`。在一个临时目录里起真的 `dsh web`
  （独立的 `DSH_HOME`，模型由 DSH 自带的 `llm-mock-server` 扮演）、真的 `personad node --stdio`，脚本自己当 hub：从「手机」新建会话、发消息、
  看思考和回复、翻历史、批准一条命令（真的执行）、回答一个提问、看逐字、中断，核对推送的连续性，再让 DSH 的 Agent 调一个手机工具（hub 收到调用和会话、
  结果回到 Agent）、交给一个后台子 Agent（父会话这一轮结束、它在 `subagents` 里在跑且带着启动它的调用、不单列、按 `<父>@<子>` 翻页和 watch、
  跑完后列表说它完了、父会话被叫醒）、后台跑一条命令（这一轮结束后列在 `background` 里，退出后离开），最后从手机关闭会话（离开列表、DSH 的归档里有它）。
  子 Agent 的请求和父会话的交错，假模型按提示里的标记分开（`scriptedModel().scriptWhen`）。2026-11-02 实测 31/31 通过，daemon 模式（`JARVIS_E2E_DAEMON=1`）31/31。
  脚本给 DSH 装了一个只在测试里用的规则（含 `JARVIS_E2E_ASK` 的命令要授权）和一个「不沙箱但要授权」的权限预设：
  在别的沙箱里（比如 Agent 的 shell）起不了 DSH 自己的沙箱。
- 掉出列表的老会话：`node integrations/dsh/e2e/older-session.mjs`（同样两个环境变量）。插件只列 1 个会话，建 A、B 两个，
  重启 DSH 让插件忘掉手机碰过 A，再给 A 发消息，要求送到 DSH、回复推回来、没有走 herdr。2026-11-01：新版 5/5，部署中的旧版 3/5。

- DSH 没在运行时：`node integrations/dsh/e2e/files.mjs`（同样两个环境变量）。真的 `dsh web` 跑一轮后停掉，伴随进程把这个会话从磁盘列成 offline、翻出内容，
  前后 `DSH_HOME` 里没有任何文件变化；从「手机」再发一条，伴随进程用唤醒脚本起 DSH，node 把消息交给插件，回复回来，会话回到插件手里。2026-11-02 实测 7/7 通过。

- 自动更新：`node integrations/dsh/e2e/update.mjs`（同样两个环境变量）。node 的包来自一个目录（`JARVIS_RUNTIME_PACKAGES`），这样不用另编一个 personad：
  node 带的和插件一样时什么都不提供，插件从存放目录跑随包附带的那份；换成带新版本插件的 node（就像 hub 更新了它），插件被提供、取文件、自己换上，
  新会话从「手机」发消息照常回复；DSH 重启后直接跑 node 给的那个，不再取。2026-11-02 实测 7/7 通过。加载器本身：`cd integrations/runtime-sdk && npm test`
  （`loader.test.js`：换版本、哈希不对不换、起不来回旧的、等手机上的卡、手动装的优先、daemon 不换）。

- 助手：`node integrations/dsh/e2e/assistant.mjs`（同样两个环境变量）。「hub」用 `assistant.send` 选 `dsh` 启动器新建对话（在助手目录里、
  第一条带上快照和记忆，聊天里只显示打的字）、`ask_user` 作为助手的调用到 hub、计划模式设成 DSH 的预设、中断、node 重启后接着发。2026-11-02 实测通过；当时还测了 node 内置的飞书工具，
  #1571 起它由扩展的命令闸门声明，这部分从脚本里去掉了。

## 以后

基于 DSH 二次开发、不跑 `dsh web` 的平台，可以在自己那边用同一个 SDK（`integrations/runtime-sdk`）实现 runtime、复用 `integrations/dsh/lib/map.js` 的映射，
当 daemon 接进来（`docs/daemons.md`），Persona 这边不用再加代码。

当 daemon 的端到端：`JARVIS_E2E_DAEMON=1` 跑同一个 `e2e/run.mjs`（插件配 `listen`，node 从 `daemons.json` 去连）。检查不变，只有两处不同：会话 id 带 `dsh.` 前缀；手机工具一项换成「daemon 拿不到手机工具，调用到不了 hub」。
2026-11-02 实测 21/21 通过（socket 方式同样 21/21）。
