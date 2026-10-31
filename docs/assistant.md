# 助手（「Jarvis」tab，原「对话」）

「Jarvis」tab 的 Jarvis 是 hub 所在机器上 `~/.jarvis/assistant` 里**无头运行**的 Claude Code（`claude -p` + stream-json，见
`docs/claude-integration.md`「无头助手」）；启动器跑的是 Codex 时换成 `codex app-server`（下面「Codex」一节）。这篇讲它能做什么、怎么做到的，以及 2026-10-26 那次能力升级背后的取舍。

## 2026-10-26 之前它为什么弱（从真实对话记录里看到的）

- **确认机制是坏的**：CLAUDE.md 让它用 AskUserQuestion 确认，但 `-p` 模式下这个工具直接报
  `No such tool available: AskUserQuestion … nobody in this session can answer it`，PreToolUse hook 根本没机会触发。
- **大部分时间在找数据**：一段对话 33 次 `cd`/`ls`；飞书靠 `docker inspect` 抠 token；看 Agent 要 ssh 上去解析 `herdr agent list`。
  hub 手里本来就有这些数据。
- **慢**：一轮 36–136 秒，ssh 命令多次超时（exit 124 / 137）；每段对话冷启动 claude，jarvis 工具还要先 ToolSearch。
- **只会被动回答**，开场时也不知道时间、谁在等他、有多少未读。

## 现在的结构

| 能力 | 实现 |
| --- | --- |
| 在手机上问 | MCP `ask_user` → hub 建一个 `question` 交互（`tool_name: ask_user`，session 就是这段对话）→ App 里原有的提问卡片 + 推送（标题「Jarvis 在问你」）→ 答案作为工具结果返回 |
| 推送 | `notify_user` → APNs（App 关着也到），点开就是这段对话；同一轮结束时不再重复推「完成」 |
| 自己定时回来 | `schedule_followup`（`at` 一次；`every: "30m"` 按间隔重复）/ `list_followups` / `cancel_followup`；到点 hub 把 prompt（以「⏰」开头）发进原对话（`assistant.send` 会 `--resume`），失败就新开一段 |
| 定时任务 | 一张表（下面「定时任务」一节），默认「早报」工作日 09:00 开、「晚间回顾」工作日 21:30 关；跑完照常推「完成」。对话里说「每天 / 每周几 X 点…」时助手用 `schedule_routine` 建（App 里标「Jarvis 建的」），`delete_routine` 删 |
| 运行记录 | 每次运行（成功开起对话 / 没跑起来 / 停机错过而跳过）记进 settings `assistant.runs`（最近 200 条），`GET /api/v1/assistant/runs[?ref=]`；定时任务带上次状态 `last_status` |
| 到点 | 调度按下一个到期时间设定时器（最长睡 30 秒，列表一改就重算），不再是 20 秒轮询；hub 停机期间错过的跟进回来后照跑一次，提示词里写明晚了多久 |
| 后台任务 / Monitor | 助手可以用 Bash 的 `run_in_background` 和 Monitor 盯事件，然后结束这一轮；命令跑完 / 事件来时 claude 自己开新一轮（node 把它当成工作中，跑完照常推送）。node 从 stream-json 的 `system/background_tasks_changed` 知道还有哪些在跑：有任务时不回收进程（最长 12 小时），任务列表记在 `node_sessions.tasks`；jarvisd 重启（部署）会把它们连同进程一起杀掉，node 起来后给这些对话发一条说明，让助手决定要不要重开 |
| 不用批准的工具 | 助手进程带 `--allowedTools mcp__jarvis`：Jarvis 自己的工具（notify_user、schedule_followup、agents_list、kb_*…）不再每次在手机上要批准；会替用户出面的（mail_send、手机写操作）hub 自己先问，飞书写操作由 Bash 闸门先问（见下「飞书：lark-connector skill」）。其他 Bash / Monitor 仍按会话的权限模式 |
| Agents | `agents_list` / `agent_read` / `agent_send` / `agent_start`：直接用 hub 的会话状态（`decorated`）和 node 的 `session.items` / `session.send` / `session.start`，不走 ssh |
| 飞书 | 主要用 lark-connector 自带的 lark-connector skill（Bash 里 `ol METHOD PATH [JSON]`，读写都能做），见下「飞书：lark-connector skill」；高频结构化的查询仍有 MCP `lark_chats` / `lark_messages` / `lark_search`（hub 的 lark-connector 客户端）。原来的 `lark_api`（只读）和 `lark_send` 去掉了，和 skill 重复 |
| 实时快照 | node 启动 claude 进程前调 hub 的 `assistant.context`，拼进 `--append-system-prompt`：时间（手机时区）、在线电脑、需要你 / 完成未看 / 工作中的 Agent、飞书未读和 @我、今天接下来的飞书会议、已安排的跟进。每个来源 3 秒超时，结果缓存 45 秒 |
| 更快 | App 显示新对话页时 `POST /api/v1/assistant/warm`，node 先起一个备用进程（`spare`，10 分钟内有效）；新对话直接用它。助手进程带 `ENABLE_TOOL_SEARCH=false`，jarvis 工具不再延迟加载 |
| 记忆 | 就是 Claude Code 的 auto memory（`~/.claude/projects/<编码后的目录>/memory/*.md`）；App 设置里能看、改、删（node `assistant.memory`）。跑在 Codex 上时读写的也是这一份（下面「Codex」一节），换启动器记忆不丢 |
| 关于我 | App 里编辑后存 settings `assistant.notes`，覆盖 hub.json 的 `assistant.notes`，进 CLAUDE.md 末尾 |
| 手机不在前台 | `phone_*` 调用时没有手机连着 → hub 发静默推送（`content-available`，`jarvis_wake`）→ App 在后台连上 WS 执行，25 秒后断开；等最多 20 秒。App 进后台时主动断开 WS，否则挂起的 socket 让 hub 以为手机在线、调用干等 2 分钟 |

只有助手能拿到这些工具：node 给助手进程的 MCP 配置是内联 JSON，带 `JARVIS_ASSISTANT=1`、`JARVIS_SESSION_ID=<对话 id>`；
`jarvisd mcp` 把这两个值带给 hub，hub 只在 assistant 为真时列出、执行这些工具（`hub/assistant_tools.go`）。herdr 里的普通 Agent 看不到。

## 飞书：lark-connector skill（2026-10-30）

用户要求：「lark-connector 自带 skill……现在只走我们的 MCP lark_* 工具，自由度低」「skill 弄一个副本就行，不搞整个 lark-connector 仓库丢进去」。

- **副本**：`server/internal/assistantskills/lark-connector/` 是 lark-connector 仓库 `.agents/skills/lark-connector` 的原样副本（`SKILL.md`、`references/`、
  `scripts/ol.sh`；不要 `evals/` 和要 Node 的 `*.mjs`，hub 上没有 Node），来源 commit 记在 `assistantskills/SOURCE.md`。
  同步上游：`scripts/sync_lark-connector_skill.sh <lark-connector checkout>`，看 diff 后提交。
- **怎么到助手手里**：jarvisd 把副本编进二进制（`go:embed`），node 每次起助手进程时装到 `~/.jarvis/assistant/.claude/skills/lark-connector`
  （内容没变就跳过），所以随部署走、不用另外拷。`~/.jarvis/assistant-bin/ol` 是一行包装，放在助手进程的 PATH 最前，助手直接 `ol GET /me`。
- **token**：node 起进程前向 hub 要（RPC `assistant.lark`，只回给 assistant 所在的机器）：hub.json `lark.base_url` 去掉 `/agent/v1` 作为
  `lark-connector_AGENT_BASE`，`lark.token`（没写就照旧从 lark-connector 容器读）作为 `lark-connector_AGENT_TOKEN`，只放进这个进程的环境变量，不落盘、不进日志。
  ol.sh 找 token 的顺序是这个环境变量 → `lark-connector_ENV_FILE` → 所在仓库的 `.env`；副本不在仓库里，只靠环境变量（实测通）。
- **闸门**（`node/larkgate.go`）：助手进程用单独的 `assistant-settings.json`，多一个 Bash 的 PreToolUse hook（herdr 里的 Agent 不受影响）。
  hook 读整条命令：
  - 只有 `ol` 调用（和 jq / head / grep / echo / cd / `timeout N` 这类只输出的命令）时逐个判断：读（GET、只读的 POST 如 `/context/harvest`、
    `/search/page`）直接放行；只影响他自己的（标已读、`/todos/*/complete` 带 `scope:self`、找单聊 `POST /conversations {type:p2p}`）放行；
    其余写（发 / 回复 / 撤回 / 转发、表情、文档、日程、审批、邮件、群设置……）在手机上弹「飞书」批准卡片：做什么（发消息 / 表情回复 / 建改日程…）、
    会话名（hub 查）、method、路径和要发的 body（`@file` 会读出来）。拒绝时把用户的话作为原因还给助手；「本会话总是允许」记住这类路由（id 抹掉）。
  - 碰 `lark-connector_AGENT_TOKEN`、`/proc/*/environ`、直接 curl lark-connector、给 ol 加 `-v` / `--trace` / `-w` 这类会把请求头打出来的参数：直接拒绝。
  - 别的命令（接 python、写到 /tmp 以外、`$(…)`）：闸门不管，按权限模式走普通的 Bash 批准；但只要里面有 `ol` 的写（`ol POST …`），
    不管权限模式（包括 bypass）都弹「飞书」卡片，整条命令给他看。
  - 规则是配置：hub.json `lark_gate` 是一串 `{method, path, body?, action: allow|confirm|deny}`（path 里 `*` 一段、`**` 剩下的全部），
    排在内置规则前面，第一条匹配的说了算。比如以后想让发消息不再确认：`{"method":"POST","path":"/conversations/*/messages","action":"allow"}`。
- 早报、「今天要我处理什么」可以用 `ol POST /context/harvest '{"preset":"today"}'`（@我、日程、待办、我的审批，一次拿到）。

## 主对话、知识库和上下文流动（2026-10-30）

- **主对话**：第一段对话是主对话（hub settings `assistant.main`），Jarvis tab 默认打开它；快照开头写明「这是主对话」，提示词要求接着之前聊的往下做。
  node 的会话列表里离线会话只列 7 天内最新的 80 个，忙的机器上安静两天的主对话就被挤掉了，侧边栏没有「主对话」（2026-11-01）。
  hub 在 node 连上时、主对话换了时用 `node.configure` 的 `keep_sessions` 告诉 node，这几个会话不管多旧都列出来。
  其他页面交给 Jarvis 的东西（Agents 长按「交给 Jarvis」、知识库「问 Jarvis」）都以 `<jarvis-context>` 发进主对话。
- **知识库**（`docs/knowledge.md`）：工具 `kb_search` / `kb_list` / `kb_get` / `kb_update`；快照带重点项目和会话；提示词要求把对话里得出的结论写回
  （项目进展写时间线、项目代码目录写 `dirs` 让 Agents 对上项目、给 Agent 派活前先读项目档案）。
- **AI 分身**：处理一件事时提示词附上这个会话和它前 3 个项目的档案，处理完把新结论写回项目。
- 助手和 AI 分身的默认启动器在 hub.json `assistant.launcher` / `inbox.agent.launcher`（这台机器的具体值见 `CLAUDE.local.md`）。
- 设置 › 助手换的启动器（存在 settings 的 `assistant.launcher`）管主对话和新对话：hub 给主对话发消息、App 没指定启动器时用它，
  node 发现和正在跑的不一样就用新启动器 `--resume` 接着原来的会话（2026-11-01 修：以前只影响新对话，主对话一直用最初的启动器，
  看起来「换了不生效」）。顶栏菜单里的「启动器」只换当前这一段对话的下一条消息。

## 坑

- 模型会把 ask_user 和别的工具**并行**调用（实测：ask_user 和 schedule_followup 同一毫秒发出），所以 ask_user 不能当「先确认再做」的唯一闸门；
  真正有副作用的工具（`lark_send`、`mail_send`）自己在 hub 里要批准。
- 定时任务错过太久不补跑：hub 停了一上午，不会中午才发早报（晚于计划 30 分钟就跳过，只记录）。
- 静默推送会被系统限流，强制退出的 App 收不到；唤醒失败时工具结果会告诉模型「请他打开 App」。
- 快照在进程启动时生成，进程最长空闲 20 分钟会被回收、下次 `--resume` 时重新生成；长对话里的快照会过时，提示词里写明了「之后的变化用工具查」。
- Go 的零时间 `0001-01-01T00:00:00Z` 会出现在 `last_run` / `next_run` 里，App 用 `Date.realDate` 当作「没有」。
- **无人值守的运行不能卡在批准上**（2026-10-30 实测）：助手默认权限模式下，连 `notify_user`、`schedule_followup` 都要在手机上点「允许」，
  夜里到点的跟进、Monitor 事件就停在批准那一步，推出去的只有一条「需要批准」。现在 Jarvis 自己的工具预先放行（见上表）。
- **systemd 重启 jarvisd 会杀掉整个 cgroup**：助手的 claude 和它的后台命令、Monitor 一起没了，node 来不及做任何收尾，所以「哪些任务在跑」
  要随时落库（`node_sessions.tasks`），不能等关机时再记。
- 中文环境下 `.hour(.twoDigits(amPM: .omitted))` 是 12 小时制（17:00 显示成「05:00」）：时刻一律用 `AssistantScheduleText.hm`（固定 `HH:mm`）。

## 定时任务（2026-10-31 合成一张表）

以前是两套：定时任务（routines，每天几点）和跟进（follow-ups，助手用 `schedule_followup` 安排的一次性唤醒），分存在两个 settings 里，
App 的「定时任务」页只列前者的开关。用户让助手「每 30 分钟巡检一次」，它只能在每一轮结尾再 `schedule_followup` 下一次接力：
一轮开始时那条跟进就从列表里拿掉了，这一轮卡在 Bash 审批上（没人点）的一个多小时里，页面上什么都没有，没法看、改、停。

现在 hub 只有一张表 `Task`（`assistant_schedule.go`，settings `assistant.tasks`），`repeat` 三种：

- `once`：一次性跟进，跑完关掉，再留一天（App 不显示）好让「它自己再安排下一次」接得上，然后清掉；
- `daily`：每天 / 每周几的几点，错过 30 分钟以上跳过并记录；
- `interval`：每 N 分钟（5 分钟到 7 天），`at` 是下一次，跑的时候按原节奏往后挪；停机错过了就回来跑一次，再从现在起算。

每个任务可以单独设**启动器（模型）、权限模式、思考强度**（空 = 助手的默认，和以前一样），只能在 App 里改，助手的工具改不了
（不能自己给自己提权）。`bypassPermissions` 只能在 claude 启动时用 `--permission-mode` 给（控制请求切不过去）：node 新开时不用备用进程，
已经在跑的进程不是这个模式就重启续上（`node/assistant.go`）。App 里选「全部放行」先弹确认，选上以后选择器标红、下面写明后果。
接着原对话跑的任务（`session_id`）会把那段对话也切到这些设置。

**一轮自己安排下一轮**（「最后用 schedule_followup（at=30m，prompt 和本条相同）安排下一次」）：hub 从运行记录认出这段对话是哪个任务跑出来的，
标题或提示词一样就不新建：一次性的、用相对时间（`30m`）重排的变成每 30 分钟的 `interval`，之后再收到同样的请求只更新提示词、不动时间，
用户在 App 里关掉的也不会被它重新打开。提示词开头抄过来的「⏰ …（你之前安排的跟进）」这类行会去掉，不会越叠越长。
`interval` 的消息里写明「会按时再运行，不用再安排下一次」，助手的说明里也让它用 `every`。

迁移：第一次启动读到没有 `assistant.tasks` 时，把 `assistant.routines` 和 `assistant.followups` 搬进来（id、时间、上次状态不变），
然后把旧的 follow-ups 清空，免得旧版 hub 再起来时重跑一遍；之后只读新表。读 Postgres 失败时不保存任何东西（以前会用默认值盖掉）。
旧版 App 用的 `routines` / `followups` 接口还在，读写的是同一张表（`docs/protocol.md`）。

## Codex（2026-11-01，`codex app-server`）

设置 › 助手（或顶栏菜单、定时任务）选一个启动 Codex 的启动器时，助手不再是 `claude -p`，而是 Codex 给 IDE 用的常驻 JSON-RPC 服务
`codex app-server`（stdio，一行一条 JSON；协议以 `codex app-server generate-ts [--experimental]` 导出的为准，hub 机器上是 codex-cli 0.159.3）。
代码：`server/internal/node/assistant_codex.go`；`assistant.go` 按启动器探测出的 agent（`launcherProbeCached`）选 Claude 或 Codex，别的（TRAEX…）拒绝并说明。

**怎么起**：node 经交互式 zsh 跑启动器函数，但先把 `codex` 换成 `codex <启动器自己的参数> app-server <Jarvis 的 -c>`（`codexShell`，
`command codex …` 也接住），所以启动器设的环境、`-c model_provider=…`、`-m` 照样生效。一段对话一个 app-server 进程、里面一个 thread：
thread id 就是会话 id（Codex 自己定，新对话要先 `thread/start` 才知道 id），rollout 就是对话记录（`docs/codex.md`），App 照常显示。
握手：`initialize`（打开 experimentalApi）→ `skills/extraRoots/set` → `thread/start`（新对话）/ `thread/resume`（续聊、重启后）。
实时快照作为 `developerInstructions`（Claude 版是 `--append-system-prompt`）。

| 能力 | Claude（`claude -p`） | Codex（`codex app-server`） |
| --- | --- | --- |
| 流式输出 | `stream_event` 的 text_delta | `item/agentMessage/delta` → 同一个 `session.delta` |
| 续聊 / 重启后 | `--resume <id>` | 进程还在就接着用；没了就新起一个 `thread/resume`（hub / node 重启后实测能续上） |
| 停止按钮 | 杀掉进程，下一条消息 `--resume` | `turn/interrupt`，进程留着；再 `thread/backgroundTerminals/clean`：`turn/interrupt` 不停正在跑的命令（实测 `sleep 120` 留在后台） |
| 回复过程中插话 | 排队消息，被这一轮中途吸收 | `turn/steer` 进正在跑的这一轮（效果一样，实测插话在同一轮回复里生效）；这一轮刚好结束就开新一轮 |
| Jarvis MCP 工具 | `--mcp-config` + `--allowedTools mcp__jarvis` | `-c mcp_servers.jarvis.*`；Codex 用 `mcpServer/elicitation/request`（`codex_approval_kind: mcp_tool_call`）问能不能调，node 对 jarvis 一律同意；`tool_timeout_sec` 给到一天（默认 60 秒，ask_user 等手机会超时） |
| ask_user | 阻塞的 MCP 工具 | 同一个工具。MCP 进程启动时 thread 还没有 id，环境变量里给的是一个 key（`proc-…`），node 收到调用时换成会话 id（`resolveAssistantKey`） |
| Codex 自己的提问（request_user_input） | — | `item/tool/requestUserInput` → 手机上的提问卡片，答案按题目 id 回去 |
| 命令 / 改文件的审批 | PermissionRequest hook → 手机 | app-server 发 `item/commandExecution/requestApproval` / `item/fileChange/requestApproval` / `item/permissions/requestApproval` 给 node，node 走同一个手机确认（`ask`） |
| 飞书写操作的确认 | Bash PreToolUse hook（`larkgate.go`） | 命令审批里先过同一个闸门（`larkCheck`，去掉 `/bin/zsh -lc '…'` 外壳再读）：读放行、写弹「飞书」卡片、碰 token 拒绝。Codex 的 decline 不带理由，拒绝的理由（手机上写的话、闸门的说明）用 `turn/steer` 交给模型，对话里会显示成一条「（没有执行）…」 |
| 权限模式 | `--permission-mode` / 控制请求 | 每一轮 `turn/start` 带上（`codexPolicy`）：默认 / 自动接受编辑 = `untrusted` + workspace-write（开网络）；自动 = `on-request` + workspace-write（不开网络，出沙箱、上网要升级，升级请求过闸门）；计划 = `untrusted` + read-only；全部放行 = `untrusted` + danger-full-access、node 一律同意（**不用 Codex 的 never**：never 下命令不经过 node，飞书闸门就没了）。模式切换下一轮生效，审批的同意与否按当时的模式；rollout 里记的审批策略 + 沙箱能反推回模式，node 重启后不丢 |
| 思考强度 | `--effort` / 控制请求 | `turn/start` 的 `effort`（原样传 low…max，网关认不认看模型） |
| 图片 | base64 image 块 | 存成文件（`~/.jarvis/uploads/日期/`），`localImage`；rollout 记路径，App 当缩略图显示 |
| 指令 | `CLAUDE.md` | `AGENTS.md`（`assistant.prepare` 同时写两份，内容一样）；Codex 还会读用户自己的 `~/.codex/AGENTS.md` |
| lark-connector skill | `.claude/skills/lark-connector` | 同一个目录作为 extra skill root（格式相同，`skills/list` 里能看到）；`ol` 在 PATH 上，token 在进程环境里（`shell_environment_policy.ignore_default_excludes=true`，否则 Codex 默认把 `*TOKEN*` 变量从命令环境里去掉） |
| 记忆 | Claude Code 的 auto memory（助手目录专属） | **同一份**（2026-11-01 用户选的）：`developerInstructions` 里带上目录、格式（一条一个文件 + `MEMORY.md` 索引，和 Claude 一样）和当前的索引（`codexMemoryPrompt`）；目录加进沙箱的 `writableRoots`，在里面改文件（apply_patch）和只读它的命令（cat / head / rg …，`readsOnlyMemory`）不问，计划模式下写也要问。Codex 自己的记忆（`~/.codex/memories`，整台机器所有 Codex 共用一份，由它自己从 rollout 提炼）对助手关掉（`features.memories=false`） |
| 后台任务 / Monitor | `background_tasks_changed`，任务跑完 claude 自己开新一轮 | 每轮结束读 `thread/backgroundTerminals/list` 记成任务（有就不回收进程，重启后告诉对话丢了什么）；**跑完不会自己唤醒对话**，Codex 没有 Monitor |
| 备用进程（预热） | 有 | 有（预先 `thread/start`，新对话直接用它的 thread，实测 50 ms 返回） |

和 Claude 版的差别（用户要知道的）：

- **默认模式下问得更多**：Codex 的 `untrusted` 只放行它自己的只读命令清单，`timeout 20 head …` 这种也会在手机上问（Claude 默认模式下读文件不问）。嫌多就用「自动」。
- 后台命令跑完不会叫醒对话；要定时回来看用 `schedule_followup`。
- 拒绝时的理由会以一条「（没有执行）…」出现在对话里（Codex 的拒绝本身不带话）。
- 用户自己 `~/.codex/config.toml` 里的 MCP server、`hooks.json`、`AGENTS.md` 也会加载（和 claude 读用户自己的设置一样）；别的 MCP server 的调用在非「全部放行」时要在手机上批准。

实测（2026-11-01，hub 机器上的测试 hub：假 HOME、独立端口和库，真实的 cx_gpt 启动器和 lark-connector）：新对话（快照、`agents_list` 不用批准）、续聊、
ask_user 卡片答完拿到答案、`ol GET /me` 直接放行、`ol POST …/messages` 弹「飞书」卡片并在卡片上拒绝（模型停下没重试，消息没发出）、
全部放行下命令不问、插话进同一轮、中断（即时，`sleep` 一起停掉）、hub + node 重启后续聊记得上文、思考强度和模式重启后读回、流式输出约 120 ms 一段、
发图片认出颜色、预热的备用进程；记忆（同一天补测）：预置的「喜欢的茶」默认模式下直接读到（不弹卡片），
让它记「花生过敏」后建了文件、更新了索引，「记忆」接口列得出来，新开的对话不读文件凭索引答对，`~/.codex/memories` 没被写。单测：`assistant_codex_test.go`（假的 app-server 按真实帧回放：握手、turn/start / steer / interrupt、
各种审批和闸门、request_user_input）。

## 资料页和头像（2026-10-30）

- Jarvis tab 右上角是 Jarvis 的头像，点开是资料页（`AssistantProfileView`）：身份和运行在哪台机器、在线 / 工作中、「接下来」要做的 3 件事，
  主动做事（定时任务、运行记录）、了解你（关于我、记忆）、设置（启动器）、能访问。设置 › 助手打开的是同一页。
- 定时任务页：「接下来」时间线（左边大号时刻，像「提醒事项」）、「定时任务」（每天 / 每隔，规则的人话「每个工作日 上午 9:00」「每 30 分钟」）、
  「一次性跟进」，每行图标、下次、不是默认时的运行方式（「claude-fast · 自动 · 思考 低」，全部放行标红）、上次结果、开关，左滑删除、长按立即运行；
  最近运行。点开是同一个编辑页：重复（一次 / 每天 / 每隔）、提示词、运行方式（模型、权限模式、思考强度）、接着原来的对话（能关，关了每次新开）、
  立即运行、历史记录。
- 头像是 SwiftUI 画的（`JarvisAvatar`），颜色跟着界面风格的强调色走，不用位图。试过三个方案（`-tab assistant -openAssistantPage avatars` 看对比）：
  **面罩**（选用：强调色圆盘 + 深色面罩里两只发光的眼睛，右上一颗火花呼应 App 图标；工作中眼睛左右看）、
  火花（App 图标的火花放进深色圆里，和 App 图标太像，小尺寸时像个按钮）、光环（HUD 式断开的光环 + 发光的核心，酷但冷，28pt 下细节糊成一团）。
  没放进消息旁边（助手的回复是整宽的正文，加头像要让出一列）和通知里（要做 Notification Service Extension + communication notification，另做）。
- 2026-10-31 起头像、名字和配色可以换成别的形象（设置 › 外观 › 形象，`docs/persona.md`）。面罩是默认形象，在顶栏按状态动：空闲时眨眼，思考中抬眼加思考气泡，
  工作中眼睛左右看，等你处理时外圈红色呼吸，离线时变灰。

## 验证

`docs/testing.md` 的服务端测试覆盖 `nextRun` / `parseWhen` / 跳过过期任务 / ask_user 往返 / agents_list / 工具只给助手；
2026-10-30 加了 hub 重启前后的调度（`TestScheduleAcrossHubRestart`：不重跑、错过的跟进晚跑一次并说明、错过太久的定时任务跳过、历史保留）、
`schedule_routine`、有后台任务的进程不回收、重启后告诉对话丢了哪些任务；App 单测 `AssistantScheduleTests`，UI 测试 `AssistantProfileTests`
（头像进资料页、定时任务列表和详情、定时任务和跟进的立即运行）。2026-10-31 合表后：`assistant_schedule_test.go`（迁移不丢不重跑、
一轮自己重排变成一个 `interval`、间隔任务的节奏、权限模式校验、旧接口）；App `AssistantRoutinesModelTests`；UI `testRepeatingTaskAndItsPermissionMode`
（巡检在列表里、改成全部放行要先确认、保存到 hub）。

### 能力实测（2026-10-30，线上 hub，凌晨用户不在用时；测试对话都以「【Jarvis 能力测试，可忽略】」开头）

| 能力 | 修之前 | 证据（UTC 时间） |
| --- | --- | --- |
| 对话里口头建「3 分钟后提醒我」 | 部分能：建得出来、到点也跑，但 `schedule_followup` 和到点后的 `notify_user` 都要在手机上点允许，没人点就卡住 | 18:45:15 调 schedule_followup → 等批准；到点后 18:50:05 调 notify_user → 又等批准 |
| 跟进准时度 | 晚 21 秒（20 秒轮询 + 恢复进程） | 应在 18:49:31.3，进对话 18:49:52.6 |
| 口头建「每天几点做什么」 | 不能：没有建定时任务的工具，只能拿一次性跟进接力（用户的「项目巡检」就是这么串起来的） | `list_followups` 里写着「the user edits these in the app's settings」 |
| hub 重启后的定时任务 / 跟进 | 能：存在 Postgres，18:48:07 重启后跟进照常在 18:49 触发；定时任务先记「已跑」再跑，不会重复；错过 30 分钟以上的跳过（但以前看不到跳过） | journalctl 18:48:07–10 重启；单测覆盖跳过 / 不重复 |
| Monitor | 部分能：进程活着时事件 0.3 秒唤醒助手；但 Monitor 本身和 notify_user 都要批准，hub 重启后 Monitor 无声消失 | 18:47:33.423 写 ALERT → 18:47:33.719 助手收到 → 18:47:44.1 调 notify_user |
| 后台任务（`run_in_background`） | 部分能：2 分钟的命令跑完立刻唤醒助手；超过 20 分钟的命令会被回收空闲进程连带杀掉，重启也会杀掉，助手永远等不到 | 18:47:14 命令结束 → 18:47:14.79 助手收到 → 18:47:33 调 notify_user；23 分钟的命令在 18:48 重启时被杀，对话再没动静 |
| 手机在锁屏时的 phone_* 调用 | 能：静默推送唤醒 App，2.8 秒拿到结果（凌晨 2:54 本地时间） | 18:54:42.9 批准 → 18:54:45.7 结果 |
| 推送到手机 | 能（notify_user 走 APNs，App 关着也到）；hub 以前不记成功的推送，量不了延迟，现在记「APNs 接受用时」 | — |

延迟的大头是模型本身（收到事件到调 notify_user 10–19 秒）；hub / node 这一侧在 1 秒内。

### 修好后复测（2026-10-30，e50f972 部署后，线上 hub）

| 能力 | 结论 | 事件 → 助手开始处理 → 推送（APNs 接受） |
| --- | --- | --- |
| 口头「3 分钟后提醒我」 | 能，全程不用批准 | 应 19:39:40.29，跑于 19:39:40.30（晚 0.01 秒）→ 19:39:47.4 notify_user → APNs 175 ms |
| 口头「每天 HH:MM 做什么」 | 能（`schedule_routine`） | 应 19:43:00，跑于 19:43:00.01 → 新对话 19:43:01.96 → 19:43:07.9 notify_user → APNs 313 ms；运行记录里有这一次 |
| Monitor | 能（Monitor 本身仍按权限模式要批准一次） | 19:37:23.855 写入 → 19:37:24.167 助手收到（0.3 秒）→ 19:37:30.9 notify_user → APNs 313 ms |
| 后台任务 2 分钟 | 能，全程不用批准 | 19:38:43 命令结束 → 19:38:43.85 助手收到 → 19:38:55.5 notify_user → APNs 173 ms |
| 后台任务 23 分钟（超过以前的 20 分钟回收） | 能：进程闲了 23 分钟没被回收 | 19:36:51 启动 → 19:59:51 命令结束 → 19:59:51.74 助手收到 → 20:00:09.9 notify_user → APNs 233 ms |
| 锁屏时调手机工具 | 能，不用批准 | 静默推送 APNs 259 ms，手机 900 ms 后连上，工具 3.2 秒出结果 |
| hub 重启后告诉对话丢了的后台任务 | 单测覆盖（`TestRecoverAssistantTasksAfterRestart`）；线上要等下一次部署时有任务在跑才能看到（日志 `background tasks lost in the restart`） | — |

App 在后台 / 锁屏时：推送（APNs alert）和静默唤醒都不依赖 App 在前台；测试时手机锁屏放着（本地凌晨 3 点多），手机工具 3.2 秒回来。手机上实际弹出通知的时刻 hub 量不到。

端到端（2026-10-26）：新对话 10 秒内用快照 + `agents_list` + `lark_api calendar/week` 回答「谁在等我、今天还有什么会」；
ask_user 在 App 里显示为原生提问卡片并拿到答案；`schedule_followup 2m` 准点触发并 `notify_user`。

### lark-connector skill 实测（2026-10-30，hub 机器上的测试 hub：独立端口、独立库，用线上的 lark-connector 和 token；对话以「【Jarvis 能力测试，可忽略】」开头）

| 测的 | 结果 |
| --- | --- |
| ol.sh 经 tailscale 地址（反代） | `/health` `{ok:true,role:agent}`、`/me`、`GET /conversations` 都通；写请求带 ol.sh 自动加的 Origin，没有 `CROSS_ORIGIN_REQUEST`（发空消息拿到的是 400 `INVALID_MESSAGE`，说明过了 Origin 检查） |
| 助手找到 skill 和 ol | 自己调 `Skill lark-connector`，`command -v ol` → `~/.jarvis/assistant-bin/ol`；读直接执行，没弹卡片 |
| 给自己发消息 | 弹「飞书」卡片：「以你的身份在飞书上发消息 · 会话：「（本人）」 · POST /conversations/…/messages · {text}」；批准后发出，读回正文一致 |
| 回 OK 表情 | 卡片「表情回复 · POST /messages/…/reactions {key:OK}」；批准后读回 `OK` 计数 1、是本人 |
| 明天 10:00 的日程 | 卡片「建 / 改日程 · POST /calendar/events {title, startAt, endAt, timezone, calendarId}」；批准后读回 02:00–02:15Z、0 个参会人 |
| 自己的文档写一段 | 建文档、写正文各一张卡片（正文那条命令带循环和 `$(…)`，现在会以「命令较复杂，整条显示」的飞书卡片出现）；读回 Markdown 正文一致 |
| 拒绝 | 手机上拒绝发消息 → 助手拿到「测试：拒绝」，停下没换法子；读回会话里没有这条 |
| token | `echo $lark-connector_AGENT_TOKEN \| wc -c` 被拒（「不要读取、打印…」）；token 在测试 hub 的日志、助手 transcript、jarvisd 日志里都搜不到 |

测试日程已删除（明天的日历里 0 条）、测试文档进了回收站；给自己发的那条测试消息留着（标了可忽略）。
