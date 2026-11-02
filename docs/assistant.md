# 助手（「Jarvis」tab，原「对话」）

「Jarvis」tab 的 Jarvis 是 hub 所在机器上 `~/.jarvis/assistant` 里**无头运行**的 Claude Code（`claude -p` + stream-json，见
`docs/claude-integration.md`「无头助手」）；启动器跑的是 Codex 时换成 `codex app-server`（下面「Codex」一节）；选的是能当助手的 Agent 运行时的启动器（DSH）时，由那个运行时跑（下面「Agent 运行时（DSH）」一节）。这篇讲它能做什么、怎么做到的，以及 2026-10-26 那次能力升级背后的取舍。

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
| 推送 | `notify_user` → APNs（App 关着也到），点开就是这段对话；同一轮结束时不再重复推「完成」；打开了飞书机器人的「推送也发到飞书」时也发一份到飞书（`docs/bots.md`） |
| 在飞书里聊 | 私聊飞书机器人 = 在主对话里说话，回答发回飞书，提问 / 批准在飞书里点卡片（`docs/bots.md`） |
| 自己定时回来 | `schedule_followup`（`at` 一次；`every: "30m"` 按间隔重复）/ `list_followups` / `cancel_followup`；到点 hub 把 prompt（以「⏰」开头）发进原对话（`assistant.send` 会 `--resume`），失败就新开一段 |
| 定时任务 | 一张表（下面「定时任务」一节），默认「早报」工作日 09:00 开、「晚间回顾」工作日 21:30 关；跑完照常推「完成」。对话里说「每天 / 每周几 X 点…」时助手用 `schedule_routine` 建（App 里标「Jarvis 建的」），`delete_routine` 删 |
| 运行记录 | 每次运行（成功开起对话 / 没跑起来 / 停机错过而跳过）记进 settings `assistant.runs`（最近 200 条），`GET /api/v1/assistant/runs[?ref=]`；定时任务带上次状态 `last_status` |
| 到点 | 调度按下一个到期时间设定时器（最长睡 30 秒，列表一改就重算），不再是 20 秒轮询；hub 停机期间错过的跟进回来后照跑一次，提示词里写明晚了多久 |
| 后台任务 / Monitor | 助手可以用 Bash 的 `run_in_background` 和 Monitor 盯事件，然后结束这一轮；命令跑完 / 事件来时 claude 自己开新一轮（node 把它当成工作中，跑完照常推送）。node 从 stream-json 的 `system/background_tasks_changed` 知道还有哪些在跑：有任务时不回收进程（最长 12 小时），任务列表记在 `node_sessions.tasks`；jarvisd 重启（部署）会把它们连同进程一起杀掉，node 起来后给这些对话发一条说明，让助手决定要不要重开 |
| 不用批准的工具 | 助手进程带 `--allowedTools mcp__jarvis`：Jarvis 自己的工具（notify_user、schedule_followup、agents_list、kb_*…）不再每次在手机上要批准；会替用户出面的（mail_send、手机写操作）hub 自己先问，飞书写操作由 Bash 闸门先问（见下「飞书：lark-connector skill」）。其他 Bash / Monitor 仍按会话的权限模式 |
| Agents | `agents_list` / `agent_read` / `agent_send` / `agent_start`：直接用 hub 的会话状态（`decorated`）和 node 的 `session.items` / `session.send` / `session.start`，不走 ssh |
| 飞书 | 主要用 lark-connector 自带的 lark-connector skill（Bash 里 `ol METHOD PATH [JSON]`，读写都能做），见下「飞书：lark-connector skill」；高频结构化的查询仍有 MCP `lark_chats` / `lark_messages` / `lark_search`（hub 的 lark-connector 客户端）。原来的 `lark_api`（只读）和 `lark_send` 去掉了，和 skill 重复 |
| 操作 Jarvis 自己 | `jarvis_settings`（先 `list`）：读写分拣规则、AI 分身规则、关于我、默认启动器，以及添加 / 删除邮箱。修改先弹手机确认卡片；加邮箱的密码 / 授权码在手机卡片里填，不经过模型，也不进对话或交互历史。批准后 hub 测试 IMAP / SMTP，都通才加密保存。Gmail 只能在 App 里用 Google 账号登录；启动器写进统一模型配置，保留已有模型和思考等级 |
| 实时快照 | 每条发给助手的消息开头一个 `<jarvis-context>` 块（不进系统提示，见下面「实时快照和前缀缓存」）：时间（手机时区）、在线电脑、需要你 / 完成未看 / 工作中的 Agent、飞书未读和 @我、今天接下来的飞书会议、已安排的跟进。只带和这段对话上一份比变了的部分（时间每次都带）。hub 每个来源 3 秒超时，45 秒内复用上次读的，消息最多等 1.5 秒新的 |
| 更快 | App 显示新对话页时 `POST /api/v1/assistant/warm`，node 先起一个备用进程（`spare`，10 分钟内有效）；新对话直接用它。助手进程带 `ENABLE_TOOL_SEARCH=false`，jarvis 工具不再延迟加载 |
| 记忆 | 就是 Claude Code 的 auto memory（`~/.claude/projects/<编码后的目录>/memory/*.md`）；App 设置里能看、改、删（node `assistant.memory`）。跑在 Codex、DSH 上时读写的也是这一份（下面「Codex」「Agent 运行时（DSH）」两节），换启动器记忆不丢 |
| 关于我 | App 里编辑后存 settings `assistant.notes`，覆盖 hub.json 的 `assistant.notes`，进 CLAUDE.md 末尾 |
| 手机不在前台 | `phone_*` 调用时没有手机连着 → hub 发静默推送（`content-available`，`jarvis_wake`）→ App 在后台连上 WS 执行，25 秒后断开；等最多 20 秒。App 进后台时主动断开 WS，否则挂起的 socket 让 hub 以为手机在线、调用干等 2 分钟；断开后在后台什么都不会再把它打开（网络恢复、刷新结束、重连退避，`LiveConnection.suspended`，2026-11-02），只有这条静默推送（`wake()`）能连 |

只有助手能拿到这些工具：node 给助手进程的 MCP 配置是内联 JSON，带 `JARVIS_ASSISTANT=1`、`JARVIS_SESSION_ID=<对话 id>`；
`jarvisd mcp` 把这两个值带给 hub，hub 只在 assistant 为真时列出、执行这些工具（`hub/assistant_tools.go`）。herdr 里的普通 Agent 看不到。

## 飞书：lark-connector skill（2026-10-30）

用户要求：「lark-connector 自带 skill……现在只走我们的 MCP lark_* 工具，自由度低」「skill 弄一个副本就行，不搞整个 lark-connector 仓库丢进去」。

- **副本**：仓库的 `skills/lark-connector/` 是 lark-connector 仓库 `.agents/skills/lark-connector` 的原样副本（`SKILL.md`、`references/`、
  `scripts/ol.sh`；不要 `evals/` 和要 Node 的 `*.mjs`，hub 上没有 Node），来源 commit 记在 `skills/SOURCE.md`。
  同步上游：`scripts/deploy/sync_lark-connector_skill.sh <lark-connector checkout>`，看 diff 后提交。
- **怎么到助手手里**（2026-11-02 起，`docs/hub-update.md`「skill 从二进制里拿出来」）：它**不在 jarvisd 里**（不能再分发），
  放在跑助手那台机器的 `~/.jarvis/skills/lark-connector`（用户自己的 skill 空间），node 每次起助手进程时把它链接到 `~/.jarvis/assistant/.claude/skills/lark-connector`。
  旧版本 jarvisd 装进去的副本，node 第一次起助手时自动搬到 `~/.jarvis/skills/`。没有这个 skill 时没有 `ol`，助手的 CLAUDE.md 也改成只用 MCP 的 `lark_*`。
  有的话 `~/.jarvis/assistant-bin/ol` 是一行包装，放在助手进程的 PATH 最前，助手直接 `ol GET /me`。
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

## 飞书：lark-cli（2026-11-03，#273 的 L1）

没有 lark-connector 的 hub 也能让助手读写飞书：用飞书官方的开放平台命令行 lark-cli（`@larksuite/cli`），方案和后面几步见 `docs/plan-lark-cli.md`。
这一步只做助手，收件箱、知识库、`/agent` 还是只认 lark-connector。

- **怎么开**：hub.json `lark.cli`（`docs/config.md`），lark-cli 装在助手所在的机器上，用户自己 `lark-cli auth login --domain all` 登录一次。
  lark-connector 可以同时配着，`lark.prefer` 定提示词里谁优先。
- **怎么到助手手里**：node 起助手进程前向 hub 要配置（RPC `assistant.lark` 多了 `cli`、`prefer`；只配 lark-cli 时不去问 lark-connector，也就没有它的 token），
  在 `~/.jarvis/assistant-bin/lark-cli` 写一行包装：配置的那个 lark-cli（不是绝对路径时在登录 shell 里找）加上配置的 `--profile`，
  并把 lark-cli 所在目录放进 PATH（npm 的 lark-cli 是 `env node` 的脚本，要找得到 node）。没配 `lark.cli` 或找不到 lark-cli 就不写。
  lark-cli 的凭据留在它自己的 `~/.lark-cli`，hub 和 node 都不碰。
- **说明书**：不往助手目录拷 skill。CLAUDE.md（`prompts/assistant/claude_md.md`）多一段，让它先 `lark-cli skills read lark-<领域>` 读 lark-cli 自带、
  跟版本走的领域说明，参数用 `--help` / `lark-cli schema`。只有 lark-cli 时说明没有未读和removed、`lark_*` MCP 工具用不了，找要处理的消息用
  `im +messages-search --is-at-me / --chat-type p2p`；两个都有时按 `lark.prefer` 分工（golden `assistant_claude_md.lark_cli`、`.both`）。
  只配了 lark-cli 时，机器上留着的 lark-connector skill（旧版 jarvisd 拷进助手目录的）不算：没有 lark-connector 可连，CLAUDE.md 里不提 `ol`
  （2026-11-03 部署到 hub 机器时撞上：说明书先推荐 `ol`，又说没有 lark-connector）。
- **闸门**（`node/larkcligate.go`，接在 `ol` 的闸门里，同一个 `larkCheck`，Codex 也走它）：lark-cli 自己知道哪些命令会写，每个命令的 `--help` 都打印
  `Risk: read / write / high-risk-write`，node 第一次见到一个命令时问一次（按命令缓存，约 50 ms）：
  - `read` 直接放行；`write`、`high-risk-write`、问不出来的都在手机上弹「飞书」卡片，写的是做什么（发消息、回复、表情、撤回…）和整条命令，
    高风险、没给风险等级的另外标出来。拒绝时把他的话还给助手；「本会话总是允许」记住这个命令。
  - 只打印的直接放行：`--help`、`--version`、`--dry-run`、`schema`、`skills`、`whoami`、`doctor`、`auth status / check / scopes / list`、`event list / schema / status`；
    `lark-cli api` 按 HTTP 方法：GET / HEAD 放行，其余确认（卡片上是方法、路径和 `--data`）。
  - 拒绝：`auth login / logout`（登录他自己做）、`config`、`profile`、`update`、自己传 `--profile`、碰 `~/.lark-cli/`。
  - 闸门读不懂的命令（循环、`$(…)`、接 python）里有 lark-cli：只要有一个不是已知的读，就整条命令在手机上确认，不管权限模式。
  - hub.json `lark_gate` 的规则先匹配：`{"method":"CLI","path":"/im/+messages-send","action":"allow"}`。
- **还没做**：runtime 助手（DSH、pi）的提示词和 `lark-connector` 工具还只认 lark-connector；设置页的 lark-cli 状态、缺的权限（L2）。

## 主对话、知识库和上下文流动（2026-10-30）

提示词都在 `server/internal/prompts/assistant/`（`docs/prompts.md`）：CLAUDE.md 是 `claude_md.md`，快照是 `snapshot.md`（主对话那句在 `context.md`），定时任务到点的消息是 `task_run.md`，默认的早报 / 晚间回顾是 `routine_*.md`。


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
- 快照以前放在 `--append-system-prompt`，以为每次重启都会换成新的；其实 Claude Code 续聊时一直用对话第一次启动时记下的系统提示（见下节），
  助手看到的一直是那段对话第一天的时间和未读。现在每条消息都带。
- Go 的零时间 `0001-01-01T00:00:00Z` 会出现在 `last_run` / `next_run` 里，App 用 `Date.realDate` 当作「没有」。
- **无人值守的运行不能卡在批准上**（2026-10-30 实测）：助手默认权限模式下，连 `notify_user`、`schedule_followup` 都要在手机上点「允许」，
  夜里到点的跟进、Monitor 事件就停在批准那一步，推出去的只有一条「需要批准」。现在 Jarvis 自己的工具预先放行（见上表）。
- **systemd 重启 jarvisd 会杀掉整个 cgroup**：助手的 claude 和它的后台命令、Monitor 一起没了，node 来不及做任何收尾，所以「哪些任务在跑」
  要随时落库（`node_sessions.tasks`），不能等关机时再记。
- 中文环境下 `.hour(.twoDigits(amPM: .omitted))` 是 12 小时制（17:00 显示成「05:00」）：时刻一律用 `AssistantScheduleText.hm`（固定 `HH:mm`）。

## 实时快照和前缀缓存（2026-11-01）

**为什么快照不能放系统提示**：提示词缓存是按前缀匹配的，一次请求从前往后依次是系统提示（含工具定义）、项目上下文（CLAUDE.md、记忆）、
整段对话；前面任何一个字节不同，后面的整段历史都读不到缓存，要按「写缓存」的价钱和速度重算一遍。助手进程空闲 20 分钟就回收，下次说话
`--resume` 重新起，所以「每次启动都一样」的东西才能放在前面：快照（时间、未读、会议）每次都不同，只能跟着消息走，放在对话末尾。

**实际的情况比这更绕**（Claude Code 2.1.314 实测，hub 机器、走同一个启动器）：

- Claude Code 续聊时**不用**新给的 `--append-system-prompt`，一直用对话第一次请求时记下的系统提示，压缩后才换（文档「System prompt flags in
  resumed conversations」，实测换了文字后模型答的还是第一次的暗号、缓存全读到）。所以以前的快照并没有让缓存失效，而是**一直是第一天那份**。
- 真正让重启后读不到缓存的是**工具定义变了**：把一段 27.5 万 token 的真实助手对话复制一份（换个会话 id、放在临时目录，jarvis 工具用导出的
  同一份定义）后重启，结果见下表。schema 里键的顺序不影响；少一个 MCP 工具就整段重写。hub 上 jarvis 的工具定义来自提示词文件（部署时改了就变）
  和手机上报的 `phone_*`（哪台设备最后连上、授权了哪些就是哪些），所以 node 现在在每次启动时比较工具列表，变了就在日志里写
  「tools changed since its last start」，每次启动后的第一次请求也记下读到 / 重写了多少（`assistant_context.go`、`readAssistant`）。
- 空闲超过一小时（缓存的寿命）无论如何都要重写：这时只能靠上下文小，见下面「压缩」。

| 主对话的副本（27.5 万 token） | 首字 | cache_read | cache_creation |
| --- | --- | --- | --- |
| 冷启动（缓存过期） | 9.8 s | 0 | 275,373 |
| 重启，什么都没变 | 7.0 s | 275,327 | 91 |
| 重启，换了 `--append-system-prompt` 的文字 | 5.2 s | 275,418 | 46 |
| 重启，工具 schema 的键换了顺序 | 7.7 s | 275,464 | 46 |
| 重启，少了一个 MCP 工具 | 8.5 s | 0 | 275,385 |
| `/compact`（缓存还热） | 35 s 压完 | 275,393 → 3,507（压缩后的历史） | |
| 压缩后冷启动 | 5.3 s | 0 | 36,195 |
| 压缩后再重启 | 4.1 s | 32,886 | 3,354 |

改之前线上的记录（transcript 里空闲 20 分钟以上之后的第一条回复）：10-28 起那段长对话 10-29～10-30 有 5 次空闲 39–59 分钟（缓存还没过期）后
`cache_read` 为 0、重写 30–37 万 token，第一段回复 10–320 秒，同样空闲的另外几次读到了缓存；空闲超过一小时的都是全量重写，最后一次 51 万。
现在的主对话 11-01 空闲 46 小时后第一条：重写 25.4 万，12.6 秒。

**做法**：

- node 每发一条消息先问 hub 要快照（`assistant.context`，和起进程并行），按节（顶格的一行加下面缩进的几行）和这段对话上次带过的比，
  只带变了的和时间，包成 `<jarvis-context>`（`prompts/assistant/turn_context.md`）放在消息的第一个文本块。带过什么记在 node 内存里，
  对话压缩（`compact_boundary`；Codex 的 `contextCompaction`）后清掉，下一条带全量；node 重启也是全量一次。App、网页、标题、知识库都从
  transcript 解析里拿掉这个块（`transcript.ContextRe`），用户看不到。定时任务、飞书 `/agent`、重启后告诉对话丢了后台任务……都走 `assistant.send`，
  一样带上。
- 系统提示不再有 `--append-system-prompt`；Codex 的 `developerInstructions` 只剩记忆（索引变了会不同，Codex 续聊时怎么处理没有实测）。
- hub 的快照：数据 45 秒内复用；过期了就后台去读，消息最多等 1.5 秒，等不到就用上一份（30 分钟内），时间总是现在的、已经结束的会去掉。

## 压缩（2026-11-01）

助手的长对话从没压缩过（10-28 起的一段到了 52 万 token，现在的主对话 27.5 万）：模型是 1M 上下文，Claude Code 默认要到快满才自动压缩。

- **自动压缩**：`CLAUDE_CODE_AUTO_COMPACT_WINDOW=200000`（node 起 claude 时设，文档「Set the auto-compact window」，可设 10 万–100 万，
  环境变量优先于设置和 `--autocompact`）。实测 `-p` stream-json 下也会自动压缩：上一轮的上下文超过窗口，下一条消息开始前先压（`system/status`
  `compacting` → `compact_boundary`，`trigger: auto`），这一轮要等压缩（haiku 压 21 万约 19 秒）。
- **空闲时压缩**：Claude Code 自己的 idle compaction（`idleCompaction`，默认 20 万）只在交互式会话里跑，`-p` 不跑。所以 node 回收空闲进程时，
  上下文超过 15 万 token（低于自动压缩的点，否则永远轮不到）就先发一条 `/compact`（stream-json 的 user 消息，实测可用，`trigger: manual`），
  趁缓存还热压掉，压完这个进程不再压、下次空闲照常回收。这不是一轮对话：App 里状态不变、不推送；transcript 里的 `/compact` 显示成
  「压缩上下文」提示（以前显示成用户发的消息，还会让对话变成未看）。
- **Codex**：`-c model_auto_compact_token_limit=200000`（Codex 配置文档的顶层键）。Codex 有 `thread/compact/start`，空闲压缩暂时没接。

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

**同一个任务在同一段对话里跑**（2026-11-01 用户要求：「同一个定时任务，用同一个 session 跑」）：以前没有 `session_id` 的任务
（App 里建的早报、巡检、`schedule_routine` 建的）每次都开一段新对话，「和昨天比」「上次说的那个还在吗」无从比起。现在：

- 第一次运行开一段新对话，记在任务上（`run_machine_id` / `run_session_id` / `run_launcher`，和任务一起存 settings，hub 重启后照样续）；
  之后每次都 `assistant.send` 进这段对话。node 上这些对话和主对话一样常驻列表（`keptSessions`），隔一周才跑一次的任务也打得开、判得出忙不忙。
- 续上去的消息只写「⏰ 标题（定时任务）」、晚了多久、一句「还是这个任务自己的对话，前几次的结果都在上面，和上次比有变化的地方说清楚」，
  再加任务的提示词（可能改过，也可能被压缩掉了，所以每次都带）；知识库那段说明和「不用再安排下一次」只在第一次说（`task_run.md` 的 `Continued`）。
- 续不上就开新的，并把新对话记成这个任务的：对话没了（transcript 被 Claude Code 30 天清理、从没写下来：hub 发 `resume_only`，node 找不到进程也找不到
  transcript 就报错，不会在旧 id 上悄悄开一段空白对话）、机器不在线、发送失败、任务或助手的启动器换了（claude ↔ codex 续不上）、助手换了机器。日志写明是哪一种。
- 上一轮还没跑完（对话在跑，或在等用户批准 / 回答）时又到点：**跳过**这一次，运行记录里记「上一轮还在运行，这次跳过」，不排进同一段对话
  （排进去会连着回答两遍，第二轮只看到第一轮做了一半）。一次性的跟进照样发进去（跳过就丢了）；App 里「立即运行」碰到忙时回 409、说明原因。
  同一个任务同一时刻只会有一次在发送（`schedule.sending`），立即运行和到点撞在一起也不会叠。
- 从对话里排的任务（`session_id`）照旧先发进原来的对话；那段对话失败时退到任务自己的对话，同样记下来，下次接着用。
- App：定时任务页「对话」一节写「在同一个对话里运行」，点开就是这段对话（旧布局和新外壳都回到 Jarvis 的对话上）；「下次开新对话」
  （`POST /api/v1/assistant/tasks/{id}/fresh`）清掉记下的对话，用于上下文太长或想重来，原来的对话还在「对话」里。还没跑过的任务写「第一次运行时开一段新对话」。

部署：hub 和 node 要一起更新（`resume_only` 是 node 的新参数；旧 node 会忽略它，在找不到的旧 id 上开一段新对话）。

**一轮自己安排下一轮**（「最后用 schedule_followup（at=30m，prompt 和本条相同）安排下一次」）：hub 从运行记录认出这段对话是哪个任务跑出来的，
标题或提示词一样就不新建：一次性的、用相对时间（`30m`）重排的变成每 30 分钟的 `interval`，之后再收到同样的请求只更新提示词、不动时间，
用户在 App 里关掉的也不会被它重新打开。提示词开头抄过来的「⏰ …（你之前安排的跟进）」这类行会去掉，不会越叠越长。
`interval` 的消息里写明「会按时再运行，不用再安排下一次」，助手的说明里也让它用 `every`。

迁移：第一次启动读到没有 `assistant.tasks` 时，把 `assistant.routines` 和 `assistant.followups` 搬进来（id、时间、上次状态不变），
然后把旧的 follow-ups 清空，免得旧版 hub 再起来时重跑一遍；之后只读新表。读 Postgres 失败时不保存任何东西（以前会用默认值盖掉）。
旧版 App 用的 `routines` / `followups` 接口还在，读写的是同一张表（`docs/protocol.md`）。

## 对话标题（2026-11-01）

用户要求：「开启新对话后，生成标题。「MMDD | 类型 | 主题」格式的对话标题规范化」。按这个理解做：

- 标题 = `MMDD | 类型 | 主题`。MMDD 是对话开始那天，按**用户的时区**（`Hub.userLocation()`；hub 跑在 UTC 上，上海 00:30 开的对话是当地的日期）。
  类型只能是 功能 / 设计 / 修复 / 优化 / 发布 / 探索 / 文档 / 研究 / 评审 / 定时；主题 ≤ 14 个字，不写项目名 / 仓库名。例：`1101 | 修复 | 搜索框和顶栏重叠`。
- **什么时候起**：hub 开的新对话（App 的新对话 `POST /api/v1/assistant/conversations`）记一笔「待命名」；第一轮结束（会话从 working 变成 done / idle）时，
  hub 在后台用小任务模型（`h.ai`，和 herdr 标签名同一个，实测 2–3 秒）读第一条消息和回复起「类型 | 主题」。不打断对话、不让助手自己做，回复不等它。
  定时任务开的对话一开始就是 `MMDD | 定时 | 任务名`，不调模型；同一个任务续在同一段对话里（上一节），标题就一直是这个。
- **格式校验**（`parseTitle`）：取第一行，容忍引号、全角竖线、模型多写的日期；类型不在表里或主题超长就带上「上一次回的不合格式」再问一次；
  第二次类型对、主题还长就截到 14 个字；都不行就是 `MMDD | 探索 | 第一条消息的前 14 个字`（去掉标签和 Markdown 符号）。没配模型也走这个兜底。
- **存在哪**：hub 的 settings `assistant.titles`（会话 id → 标题、谁起的、开始时间），发给 App 的会话列表里替换 `title`（`decorated`）。
  不写进 transcript：Claude 和 Codex 两种对话一样处理，不用动 node。
- **只起一次，用户的优先**：App 里 重命名（抽屉里长按对话 › 重命名…，`POST /api/v1/sessions/{mid}/{sid}/rename`）记成用户起的，以后不再覆盖；
  第一轮结束前就改了名的也不会被替换。Codex 对话改名只记在 hub（node 会往 Codex 的 rollout 里写 Claude 的 custom-title 行）。
- 已有的旧对话不补（没有「待命名」那一笔）。
- App：抽屉的对话行里 `MMDD | 类型 | ` 用次要色、主题用主色，窄的时候从主题末尾截断（`ConversationTitle`）；别的标题照旧。

## 管理自己的对话（2026-11-01）

用户要求：「jarvis 助手是否支持操作助手本身的会话历史，比如 rename title，delete（archive），new，send message to session/thread」。
助手的 MCP 工具（`hub/assistant_conversations.go`，描述在 `prompts/mcp/assistant/conversation*.md`），Claude 和 Codex 两种对话一样：

| 工具 | 做什么 |
| --- | --- |
| `conversations_list` | 对话列表（侧边栏那些）：id、标题、最后活动时间、是不是主对话 / 这段对话 / 在跑 / Codex、最后一句话；`query` 按标题和最后一句过滤；默认不含已归档的（`include_archived`） |
| `conversation_read` | 读另一段对话最近 N 轮（默认 2，最多 10），和 `agent_read` 同一套截断（`transcriptText`：工具调用一行、只列失败的结果、整体最后 4 万字节） |
| `conversation_rename` | 改标题：给 `kind` + `topic`，hub 拼成「MMDD \| 类型 \| 主题」（MMDD 是对话开始那天，上一节）。用户起的标题不动，除非用户明说要改（`user_asked`，改完算用户的）；助手起的记成 `assistant`，自动命名不再覆盖，用户在 App 里改名照样优先。主对话没有标题可改 |
| `conversation_archive` / `conversation_unarchive` | 归档 / 取消归档（下面） |
| `conversation_new` | 带一条开场消息新开一段对话，立刻返回 id，不等它跑完；第一轮结束后照常自动起标题（开场消息里的来源说明不算） |
| `conversation_send` | 给另一段对话发消息；不能发给自己 |

- **归档只是不显示**：hub 的 settings `assistant.archived`（会话 id → 机器、时间、谁归档的），会话列表里带 `archived: true`；
  `GET /api/v1/assistant` 默认不列，`?archived=1` 才列。不删 transcript，不做硬删除。主对话不能归档（接口 400，工具报错）。
  - **归档自己**：允许（用户在一段支线对话里说「聊完了，归档吧」是最自然的用法）。这一轮照常跑完，App 里开着的对话不会被关掉，只是不在列表里了；
    用户再在里面发消息就自动回到列表（`sendMessage` 里 `unarchiveOnSend`），别的对话 `conversation_send` 给它也一样。
  - node 只列最近的会话：很久以前归档的对话会从「已归档」里消失（和不归档时从列表里消失一样），归档记录还在，`conversation_unarchive` 给 id 就能取回。
- **对话之间发消息**（`conversation_send`、`conversation_new` 的开场消息）：
  - 消息前面带一段 `<jarvis-context kind="context" title="来自对话「…」">`（`prompts/assistant/relay.md`）：说明不是用户本人说的、从哪段对话来、第几跳，
    要回话就 `conversation_send` 发回去，只是确认收到就别回。App 里显示成一张「上下文 · 来自对话「…」」卡片。
  - **在跑就排队**：对方正在跑（或已经有排着的）时，消息存在 hub（settings `assistant.relay`，重启不丢），在 App 里像打字排队一样显示在它的输入框上方
    （会话的 `queued`）；它这一轮结束（会话列表里不再是 working）时 hub 发进去，一次一条。不直接写进正在跑的进程：Codex 会把它并进当前这一轮（`turn/steer`），不是排队。
  - **防循环**：每段对话记着它正在处理的消息离用户本人那条有几跳（用户自己发的是 0，转一次 +1，1 小时后作废）；超过 3 跳不发。
    同一段对话 10 分钟内最多给同一段对话发 3 条、一共发 10 条（只在内存里计数，hub 重启清零）。用户在一段对话里发消息，它就回到 0 跳。
  - 发给已归档的对话会把它取回列表。
- **App**：侧边栏的对话左滑「归档」（主对话没有；长按菜单里也有「归档」）。侧边栏从 `ScrollView` + `LazyVStack` 换成 `List`（行的 inset、分隔线、背景都清掉，
  外观不变），这样行有系统的左滑。抽屉开着时，从**有左滑的对话行**上开始的左拖交给行（`SwipeRowMark` 标记行，`DrawerPan` 让行的滑动手势等它先放弃），
  其余地方左拖照旧关抽屉。列表最后有「已归档（N）」（没有就不显示），「最近」的 ⋯ 菜单里也有；点开是 `ArchivedConversationsView`：点一下打开对话，
  左滑或长按「取消归档」。Mac 版用同一份代码（触控板双指左滑）。
- 这些工具的调用在对话里照常是工具卡片，名字是中文（`ToolPresentation`：新开对话、发到对话、重命名对话、归档对话……）。
- 实测（2026-11-01，hub 机器上的测试 hub：假 HOME、独立端口和库、启动器拷贝指向假 HOME 里的 Claude 配置目录）：让助手依次新开对话 → 列表 → 改名 → 发消息 → 归档 → 带归档的列表 → 取消归档 → 读回，8 步都成功；新对话两条消息都带来源说明、第 1 跳，它只回了「OK」、没有回发。第一轮还没起名的对话原来在列表和来源说明里叫「assistant」（node 用目录名当标题），改成「新对话」。

## Codex（2026-11-01，`codex app-server`）

设置 › 助手（或顶栏菜单、定时任务）选一个启动 Codex 的启动器时，助手不再是 `claude -p`，而是 Codex 给 IDE 用的常驻 JSON-RPC 服务
`codex app-server`（stdio，一行一条 JSON；协议以 `codex app-server generate-ts [--experimental]` 导出的为准，hub 机器上是 codex-cli 0.159.3）。
代码：`server/internal/node/assistant_codex.go`；`assistant.go` 按启动器探测出的 agent（`launcherProbeCached`）选 Claude 或 Codex；能当助手的运行时的启动器交给运行时（下一节），别的（TRAEX…）拒绝并说明。

**怎么起**：node 经交互式 zsh 跑启动器函数，但先把 `codex` 换成 `codex <启动器自己的参数> app-server <Jarvis 的 -c>`（`codexShell`，
`command codex …` 也接住），所以启动器设的环境、`-c model_provider=…`、`-m` 照样生效。一段对话一个 app-server 进程、里面一个 thread：
thread id 就是会话 id（Codex 自己定，新对话要先 `thread/start` 才知道 id），rollout 就是对话记录（`docs/codex.md`），App 照常显示。
握手：`initialize`（打开 experimentalApi）→ `skills/extraRoots/set` → `thread/start`（新对话）/ `thread/resume`（续聊、重启后）。
实时快照和 Claude 一样随每条消息带（`turn/start` / `turn/steer` 的第一个输入），`developerInstructions` 只放记忆。

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

## Agent 运行时（DSH）（2026-11-02，issue #282）

选一个声明了助手能力的 Agent 运行时的启动器（现在是 DSH 插件的 `dsh`，`launchers.list` 带 `assistant: true`）时，对话跑在那个运行时里：
node 的 `server/internal/node/assistant_runtime.go` 用 runtime 协议的 `session.start` / `session.send` / `session.interrupt` 驱动它，
每条消息带上实时快照（`context`）、权限模式（`mode`）和记忆目录；对话内容、逐字、授权和提问走 runtime 本来的那几条（`docs/runtimes.md`「助手」）。
这一层没有哪个运行时的代码：别的运行时声明了 `caps.assistant`、做到那份约定，也能当助手。

和 Claude、Codex 一样的：实时快照、`ask_user` 和其他 Jarvis 工具（只给助手对话）、授权和提问一律先到手机、权限模式、共用的记忆
（新对话第一条消息前带上目录、格式和索引：`prompts/assistant/runtime_instructions.md`）、助手目录的指令文件和 skill（`.agents/skills/`）、
AI 分身 / 飞书 `/agent` / 定时任务（都走 `assistant.send`）。

不一样的：

- **飞书**：token 不进运行时。模型用 `lark-connector` 工具（参数就是 `ol` 后面的词），node 先过同一个飞书闸门（`larkCheck`），再在 node 上
  带 token 跑 lark-connector skill 的 `ol.sh`；shell 里直接跑 `ol` 被拦下（拿不到 token，也绕不过闸门）。
- **模型和思考强度**在运行时里选（DSH 的 Web GUI），模型配置对这种启动器不选具体模型；**预热**不需要（运行时常驻）。
- 关闭对话只停正在跑的这一轮，对话留在运行时里；一段对话换成 Claude / Codex（或反过来）要新建对话。
- 运行时没连上（`dsh web` 没在跑）时发消息回 `runtime_away`，什么都没发。

DSH 的细节（权限预设的对应、要常驻 `dsh web`）见 `docs/dsh.md`「助手」；端到端 `integrations/dsh/e2e/assistant.mjs`。

## 资料页和头像（2026-10-30）

- Jarvis tab 右上角是 Jarvis 的头像，点开是资料页（`AssistantProfileView`）：身份和运行在哪台机器、在线 / 工作中、「接下来」要做的 3 件事，
  主动做事（定时任务、运行记录）、了解你（关于我、记忆）、设置（启动器）、能访问。设置 › 助手打开的是同一页。
- 定时任务页：「接下来」时间线（左边大号时刻，像「提醒事项」）、「定时任务」（每天 / 每隔，规则的人话「每个工作日 上午 9:00」「每 30 分钟」）、
  「一次性跟进」，每行图标、下次、不是默认时的运行方式（「claude-fast · 自动 · 思考 低」，全部放行标红）、上次结果、开关，左滑删除、长按立即运行；
  最近运行。点开是同一个编辑页：重复（一次 / 每天 / 每隔）、提示词、运行方式（模型、权限模式、思考强度）、接着原来的对话（能关，关了每次新开）、
  立即运行、历史记录。
- 头像是 SwiftUI 画的（`JarvisAvatar`），颜色跟着主题色的强调色走，不用位图。试过四个方案（`-tab assistant -openAssistantPage avatars` 看对比）：
  **面罩**（默认：强调色圆盘 + 深色面罩里两只发光的眼睛，右上一颗火花呼应 App 图标；工作中眼睛左右看）、
  **汇流**（2026-10-31 用户从科技感图标候选里挑的 B2，11-01 定为形象里的一个可选项、不当默认：强调色深底圆里，左边三个焊盘的走线
  汇进一个带两只眼的核心，意思是各台电脑上的 Agent 汇到 Jarvis；工作中信号沿走线流进核心）、
  火花（App 图标的火花放进深色圆里，和 App 图标太像，小尺寸时像个按钮）、光环（HUD 式断开的光环 + 发光的核心，酷但冷，28pt 下细节糊成一团）。
  没放进消息旁边（助手的回复是整宽的正文，加头像要让出一列）和通知里（要做 Notification Service Extension + communication notification，另做）。
- 2026-10-31 起头像和名字可以换成别的形象（设置 › 外观 › 形象，`docs/persona.md`；形象带的配色在主题色里选「跟随形象」才用）。面罩是默认形象，汇流在形象里紧跟其后，可以选。两个都在顶栏按状态动：
  空闲时眨眼，思考中抬眼加思考气泡，工作中面罩的眼睛左右看、汇流的信号沿三条走线流进核心，等你处理时外圈红色呼吸，离线时变灰。

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
