# 助手（「Persona」tab，原「对话」）

「Persona」tab 的 Persona 是 hub 所在机器上 `~/.persona/assistant` 里**无头运行**的 Claude Code（`claude -p` + stream-json，见
`docs/claude-integration.md`「无头助手」）；启动器跑的是 Codex 时换成 `codex app-server`（下面「Codex」一节）；选的是能当助手的 Agent 运行时的启动器（DSH）时，由那个运行时跑（下面「Agent 运行时（DSH）」一节）。这篇讲它能做什么、怎么做到的，以及 2026-10-26 那次能力升级背后的取舍。

## 2026-10-26 之前它为什么弱（从真实对话记录里看到的）

- **确认机制是坏的**：CLAUDE.md 让它用 AskUserQuestion 确认，但 `-p` 模式下这个工具直接报
  `No such tool available: AskUserQuestion … nobody in this session can answer it`，PreToolUse hook 根本没机会触发。
- **大部分时间在找数据**：一段对话 33 次 `cd`/`ls`；飞书靠 `docker inspect` 抠 token；看 Agent 要 ssh 上去解析 `herdr agent list`。
  hub 手里本来就有这些数据。
- **慢**：一轮 36–136 秒，ssh 命令多次超时（exit 124 / 137）；每段对话冷启动 claude，Persona 工具还要先 ToolSearch。
- **只会被动回答**，开场时也不知道时间、谁在等他、有多少未读。

## 现在的结构

| 能力 | 实现 |
| --- | --- |
| 在手机上问 | MCP `ask_user` → hub 建一个 `question` 交互（`tool_name: ask_user`，session 就是这段对话）→ App 里原有的提问卡片 + 推送（标题「Persona 在问你」）→ 答案作为工具结果返回 |
| 推送 | `notify_user` → APNs（App 关着也到），点开就是这段对话；同一轮结束时不再重复推「完成」；打开了飞书机器人的「推送也发到飞书」时也发一份到飞书、插件渠道开了「推送」的也发（助手自己发，`docs/bots.md`、`docs/channels.md`） |
| 在飞书里聊 | 私聊飞书机器人 = 在主对话里说话，回答发回飞书，提问 / 批准在飞书里点卡片（`docs/bots.md`） |
| 自己定时回来 | `schedule_followup`（`at` 一次；`every: "30m"` 按间隔重复）/ `list_followups` / `cancel_followup`；到点 hub 把 prompt（以「⏰」开头）发进原对话（`assistant.send` 会 `--resume`），失败就新开一段 |
| 定时任务 | 一张表（下面「定时任务」一节），默认「早报」工作日 09:00 开、「晚间回顾」工作日 21:30 关；跑完照常推「完成」。对话里说「每天 / 每周几 X 点…」时助手用 `schedule_routine` 建（App 里标「Persona 建的」），`delete_routine` 删 |
| 运行记录 | 每次运行（成功开起对话 / 没跑起来 / 停机错过而跳过）记进 settings `assistant.runs`（最近 200 条），`GET /api/v1/assistant/runs[?ref=]`；定时任务带上次状态 `last_status` |
| 到点 | 调度按下一个到期时间设定时器（最长睡 30 秒，列表一改就重算），不再是 20 秒轮询；hub 停机期间错过的跟进回来后照跑一次，提示词里写明晚了多久 |
| 后台任务 / Monitor | 助手可以用 Bash 的 `run_in_background` 和 Monitor 盯事件，然后结束这一轮；命令跑完 / 事件来时 claude 自己开新一轮（node 把它当成工作中，跑完照常推送）。node 从 stream-json 的 `system/background_tasks_changed` 知道还有哪些在跑：有任务时不回收进程（最长 12 小时），任务列表记在 `node_sessions.tasks`；personad 重启（部署）会把它们连同进程一起杀掉，node 起来后给这些对话发一条说明，让助手决定要不要重开 |
| 不用批准的工具 | 助手进程带 `--allowedTools mcp__jarvis`：Persona 自己的工具（notify_user、schedule_followup、agents_list、kb_*…）不再每次在手机上要批准；会替用户出面的（mail_send、手机写操作）hub 自己先问，飞书写操作由 Bash 闸门先问（见下「飞书：lark-connector skill」）。其他 Bash / Monitor 仍按会话的权限模式 |
| Agents | `agents_list` / `agent_read` / `agent_send` / `agent_start`：直接用 hub 的会话状态（`decorated`）和 node 的 `session.items` / `session.send` / `session.start`，不走 ssh |
| 代答提问 | `interaction_answer`：主人说「帮我回那个问题，选 A」时，助手替他把别的 Agent 的**提问类**交互答掉。只认 `kind: question` 且不是助手自己的对话——`permission` / `plan` / `screen` 一律拒绝并说明只有主人能在 App 里答（批准类不代答是硬边界）。答案逐题核对：每题都要答、键必须是题目原文、选项必须在列表里、单选只能一个，多选按 App 的格式拼成 `A, B`；自由文本拒收（那要在 App 里答，或用 `agent_send`）。过期、已答、已忽略、已取消、不存在各自给出对应错误（后四种查交互历史）。`Resolution.AnsweredBy = "assistant"` 随交互历史（`GET /api/v1/interactions/history`）和 `interaction_resolved` 广播带出去，App 据此标「Jarvis 代答」。`agents_list` 的「等你」行带上 `interaction_id` 和每道题的选项，助手不必再查一次（工具和这些规则都在助手：`assistant/tools_apps.go` `toolInteractionAnswer`，测试 `assistant/tools_apps_test.go`；hub 只经 `interactions.get` 给出这条交互、不在等了就给交互历史里的结局，经 `interactions.answer` 把核对过的答案记成助手代答，`hub/interaction_answer.go`） |
| 派发任务 | `task_plan`：把要 Agent 干的活交给用户，一个任务一个待办（`origin` = 这段对话）；只读的、用户记住过的直接开，其余在对话里成计划卡等用户「开始」，不阻塞这一轮。任务的进展在实时快照的「从对话开的任务」里，完成回执、答过的问题、直接对任务说的话进对话动态（[unified-entry.md](unified-entry.md)，`assistant/tasks.go`） |
| Telegram | 装了 Telegram 扩展（扩展市场 `nwverse/persona-extensions`，#1704、#2057）才有：只读 MCP `telegram_chats` / `telegram_messages` / `telegram_search`；更灵活的读和写用扩展带的 telegram skill（Bash 里 `tg …`，即 `personad tool --cli telegram`，经 hub，写操作手机上确认），`tg` 只在 Telegram 登录着时在 PATH 上（`docs/connector-protocol.md`「命令行」） |
| 飞书 | 主要用 lark-connector 自带的 lark-connector skill（Bash 里 `ol METHOD PATH [JSON]`，读写都能做），见下「飞书：lark-connector skill」；高频结构化的查询仍有 MCP `lark_chats` / `lark_messages` / `lark_search`（hub 的 lark-connector 客户端）。原来的 `lark_api`（只读）和 `lark_send` 去掉了，和 skill 重复 |
| 操作 Persona 自己 | `jarvis_settings`（先 `list`，助手自己的工具，`assistant/settings_tool.go`：关于我、默认启动器、`main_tasks_quiet` 是助手自己的设置，它自己读写、自己在手机上弹确认卡片；其余是 hub 的设置，清单经 `app.call settings.list` 拿，读写经 `app.tool jarvis_settings` 交给 hub，hub 自己弹卡片）：读写分拣规则（全局的 `inbox.rules`，和设置里声明了 `triage_rules` 的插件来源各自的 `inbox.rules.<插件>`，有就代替全局的）、AI 分身规则、关于我、默认启动器、「主对话派的任务只在主对话里通知」（`assistant.main_tasks_quiet`）、各台机器屏蔽的 Claude 配置目录（`agents.blocked_claude_dirs`），以及添加 / 删除邮箱。修改先弹手机确认卡片；加邮箱的密码 / 授权码在手机卡片里填，不经过模型，也不进对话或交互历史。批准后 hub 测试 IMAP / SMTP，都通才加密保存。Gmail 只能在 App 里用 Google 账号登录；启动器写进统一模型配置，保留已有模型和思考等级 |
| 实时快照 | 每条发给助手的消息开头一个 `<jarvis-context>` 块（不进系统提示，见下面「实时快照和前缀缓存」）：时间（手机时区）、在线电脑、需要你 / 完成未看 / 工作中的 Agent、飞书未读和 @我、今天接下来的飞书会议、已安排的跟进。Agent 和任务各带网页版的链接：hub 的 `public_url`（没写时是默认的 `http://<主机名>:18660`）加 `/s/<机器>/<会话 id>`、`/todos?id=<待办 id>`，助手提到它们时写成 Markdown 链接；hub 没答上地址时不带（#2194）。只带和这段对话上一份比变了的部分（时间每次都带）。hub 每个来源 3 秒超时，45 秒内复用上次读的，消息最多等 1.5 秒新的 |
| 更快 | App 显示新对话页时 `POST /api/v1/assistant/warm`，node 先起一个备用进程（`spare`，10 分钟内有效）；新对话直接用它。助手进程带 `ENABLE_TOOL_SEARCH=false`，Persona 工具不再延迟加载 |
| 记忆 | 就是 Claude Code 的 auto memory（`~/.claude/projects/<编码后的目录>/memory/*.md`）；App 设置里能看、改、删（node `assistant.memory`）。跑在 Codex、DSH 上时读写的也是这一份（下面「Codex」「Agent 运行时（DSH）」两节），换启动器记忆不丢 |
| 关于我 | App 里编辑后存 settings `assistant.notes`，覆盖 hub.json 的 `assistant.notes`，进 CLAUDE.md 末尾 |
| 手机不在前台 | `phone_*` 调用时没有手机连着 → hub 发静默推送（`content-available`，`jarvis_wake`）→ App 在后台连上 WS 执行，25 秒后断开；等最多 20 秒。App 进后台时主动断开 WS，否则挂起的 socket 让 hub 以为手机在线、调用干等 2 分钟；断开后在后台什么都不会再把它打开（网络恢复、刷新结束、重连退避，`LiveConnection.suspended`，2026-11-02），只有这条静默推送（`wake()`）能连 |

说明（`~/.persona/assistant/CLAUDE.md`）里「他的电脑本身」那一段按助手**实际所在的机器**写（2026-10-31 修，原来只认 kind local）：
- 助手所在的那台，不管什么 kind，都写「就是你所在的这台机器，直接执行命令」。
- `ssh_host` 是 hub 那台机器的 ssh 别名，所以只有助手跑在 hub 本机（kind local）上时，才给其他 ssh 机器写 `ssh <别名> '<命令>'`。
- 其余情况（助手在别的电脑上，或目标是 link 机器，根本没有 ssh），不给执行不了的命令，写明不能直接执行，要做事用 `agent_start` / `agent_send`。

这份说明是助手准备自己的目录时写进那台机器的（`assistant.prepare`：新对话、App 打开新对话页的预热、AI 分身；`assistant/start.go` 的 `claudeMD` / `prepare`），
写的依据是 hub 给的事实：`devices.list`（各台机器的 kind、ssh 别名、home、数据目录）、知识库目录和别的笔记目录（2026-11-04 起知识库是助手自己的，以前问 hub 的 `kb.dirs`）、`connectors.signed_in`（登录着的连接器、它们的命令行、hub.json 配没配 lark-cli）、`plugins.assistant`（插件给助手的说明和 skill）。已经在跑的 claude 进程
用的是它启动时读到的那份，要等它下次启动才读新的：闲置 20 分钟被回收后、或那台 node 的连接重建以后，下一条消息会起新进程。

只有助手能拿到这些工具：node 给助手进程的 MCP 配置是内联 JSON，带 `JARVIS_ASSISTANT=1`、`JARVIS_SESSION_ID=<对话 id>`；
`personad mcp` 把这两个值带给 hub，hub 只在 assistant 为真时列出这些工具、把调用交给助手（`assistant/tools.go`）。herdr 里的普通 Agent 看不到。

## 和 hub 的边界（2026-11-04，#1446）

hub 只是通道：助手是自己的一个进程（`personad assistant`），经总线（`docs/protocol.md`「总线」）接到 hub。hub 转发 App 的请求、缓存助手报的快照、投递推送和给 App 的消息，自己不跑助手的代码：
`go list -deps ./internal/hub` 里没有 `internal/assistant`，两边共用的只有消息的类型、方法名、错误码和助手的路由表（`server/internal/assistantwire`），以及写字用的几个小函数（`server/internal/textfmt`）。

助手的业务逻辑都在 `server/internal/assistant`，这个包不 import hub：它自己的设置（在哪台机器、启动器、时区、关于我、`main_tasks_quiet`，`GET/PUT /api/v1/assistant`、`jarvis_settings` 里这几项）、主对话（新建、压缩、旧的留下的背景、标题「Persona」、不能改名）、开对话（准备目录、写 CLAUDE.md、预热、记忆的接口、`POST /api/v1/assistant/conversations`）、每条消息跑在哪个启动器和模型上、对话（归档、对话之间发消息、`conversation*` 工具）、对话标题、定时任务（调度、运行记录、`schedule_*` 工具、`/api/v1/assistant/tasks` 等路由）、派出去的任务（计划卡、对话动态、回执、自动完成的判断、做完的任务关掉 Agent、`今天` 卡片、`/api/v1/task-plans`、`/api/v1/task-trust` 等路由）、待办（2026-11-04 从 hub 挪过来：存储、`/api/v1/todos…` 的全部路由、随项目自动挂和派生跟随、herdr 标记、决策记录，见 `docs/todos.md`「谁管什么」）、Agents 这件事（2026-11-04 从 hub 挪过来：一轮停下后的分诊、自动继续、叫醒、它开的卡片和卡片的答案、四个开关、`session_work` 两张表、统计；新 Agent 的标签名、新建和续上的 Agent 跑在哪个启动器和模型上，`assistant/agents_*.go`，见 `docs/agent-work.md`「谁管什么」）、它的全部 MCP 工具（`ask_user`、`notify_user`、`agents_*`、`task_plan`…；用 hub 的交互队列、收件箱的 `interaction_answer`、`inbox_draft` 也是，参数、规则和模型看到的文字在助手，hub 只给数据，`assistant/tools_apps.go`）、每条消息开头的实时快照，以及渠道（2026-11-04 从 hub 挪过来：飞书机器人和插件渠道的对话那一半，消息进哪段对话、跟踪这一轮、答案和提问发回去、命令、推送和同步，`/api/v1/channels*` 的路由，`assistant/channels*.go`、`feishu*.go`；hub 只留收发，见下「渠道」和 `docs/channels.md`「谁管什么」），
收件箱（2026-11-04 从 hub 挪过来：条目和它们的状态、分拣、事件合成、静音、AI 分身、条目的翻译和通用的翻译接口、`/api/v1/inbox…` 和 `/api/v1/translate*` 的全部路由，hub 只管来源，见 `docs/inbox.md`「谁管什么」），
以及知识库（2026-11-04 从 hub 挪过来：引擎、循环、数据目录里的文件和索引表、`kb_*` 工具、`/api/v1/kb…` 的全部路由，hub 只管来源，见 `docs/knowledge.md`「谁管什么」）。

**助手找 hub 只经一个接口** `assistant.Hub`（`assistant/hub.go`），实现是总线的客户端 `assistant.BusHub`（`assistant/bus.go`）。每个方法就是总线上的一个方法：

| 方法 | 参数 → 结果 | hub 用什么回答（`hub/assistant_link.go` 的 `assistantLink`） |
| --- | --- | --- |
| `manager.hello` | `{}` → `{owner, owner_notes, assistant:{machine_id, launcher?, machines, standby?}, done_delay_ms, persona_view?, kb, agents?}` | hub.json 的 `assistant` 一节，是助手自己的设置的默认值：`owner`、`owner_notes`（`assistant.notes`）、`assistant.machine_id`（hub.json 的 `assistant.machine`，hub 没有这台就是 hub 自己那台，再没有就是第一台，`defaultAssistantMachine`）、`assistant.launcher`；`machines` 是 hub 有的机器（助手存的机器不在里面就不用），`standby` 是 `isMember`（这台 hub 是互联组的成员，定时任务由中心跑），`done_delay_ms` 是 hub.json notify 的完成推送延迟（任务的 Agent 停多久才算停下）；`persona_view` 是 hub.json 的 `assistant.persona_view`（卡片写成 persona-view，`docs/config.md`）；`kb` 是 hub.json 的 `kb` 一节 `{off?, text_days?, attachment_days?, extra_dirs?}`（知识库怎么存，以 hub 的为准，`docs/knowledge.md`「谁管什么」）；助手记住答案，这些值变了 hub 发 `manager.updated`；`agents` 是 hub.json 的 `agents` 一节（分诊的上限和开关的初始值，`docs/agent-work.md`） |
| `device.call` | `{device, method, params}` → 那台机器的结果 | node 自己的方法（`session.items`、`session.rename`、`session.send`、`session.start`、`session.retire`、`todo.sync_session_tokens`、`assistant.prepare`、`assistant.send`、`assistant.warm`、`assistant.memory`、`assistant.compact`、`assistant.handoff`、`launchers.list`、`launchers.probe`）原样交给那台的 node（`Machine.call`）；hub 自己答的：`devices.list` 和 `interactions.list`（`device` 写 `*`：各台机器（id、名字、kind、ssh 别名、home、数据目录、在不在线）、手机在不在线 / 各 Agent 在等用户做什么，`machinesNow`、`phoneClient`、`pendingInteractions`），`sessions.list` 是 App 拿到的那份会话列表（`decorated`），另带 `muted`：哪些会话的推送关了，分诊不给它们开卡片，`session.get` 是 node 列出的那一条（`Machine.session`）。没有这台机器时报 `assistant.ErrNoDevice` |
| `app.call` | `{app_id, method, params, timeout_ms?}` → 应用的结果（`timeout_ms`：助手等多久，hub 到点也停，比如一次模型调用） | `models`（hub 的模型配置，`docs/model-config.md`；助手有了自己的模型配置后去掉）：`models.complete`（`purposeModel(purpose).Complete`）、`models.json`（`…JSON`，自动完成的判断；带 `max_tokens` 时是 `…JSONMax`，知识库的几个用途，`assistant/kb_model.go`）、`models.apply`（`applyModel`）、`models.resolve`（`resolveModel`）、`models.record` / `models.save`（某个用途自己存的那条和它的版本号，带版本号存：主对话的启动器）、`models.apply_picked`（这一条消息选的启动器和模型，和配置的一样检查、算指纹，`applyPickedModel`）、`models.launchers`（某台机器的启动器，和 App 看到的一样：它自己的去掉隐藏的，加上 hub 管的，`machineLaunchers`）、`models.launcher_check`（`{machine_id, launcher, model?}` → `{available, model_ok}`：这个启动器还在不在、模型它还给不给，`launcherAvailable` / `launcherHasModel`，判断不了算在）（2026-11-04 去掉 `kb`：`kb.status`、`kb.prompt`、`kb.dirs`、`kb.search`、`kb.list`、`kb.get`、`kb.update`，知识库是助手自己的了）；`interactions`（hub 的交互队列，`hub/interaction_answer.go`）：`interactions.get {interaction_id}` → `{interaction, ok}`（不在等了 `ok` 是 false，`status` 是交互历史里的结局 resolved / dismissed / expired / cancelled，没有这条是空）、`interactions.answer {machine_id, session_id, interaction_id, answers}` → `{ok, status}`（把那个会话还在等的提问按 `answers` 答掉，记成助手代答；已经不在等了 `ok` 是 false；不是提问、不是那个会话的报错）；`connectors`：`connectors.snapshots`（登录了的连接器的实时状态：等用户的聊天、今天的会，快照和今天卡片用，`connectorSnapshots`）、`connectors.signed_in`（开着且登录着的连接器、它们的命令行、hub.json 配没配 lark-cli），知识库的来源（2026-11-04，`hub/kb_sources.go`，数据用 `internal/kb`、`internal/connector` 的类型）：`connectors.archives {}` → `{archives:[{id, poll_ms?, sources?, media?}], items:[id]}`（现在该读的连接器：登录着、开着「进知识库」的；有文档 / 会议 / 日程的）、`connectors.changes {id, cursor, limit}` → `{batches, next, more?}`、`connectors.archive_media {id, ref}` → `{media:{mime, data}}`（最多 20 MB）、`connectors.items {id}` → `{items}`、`connectors.doc_title {id, url}` → `{title}`；`plugins`：`plugins.assistant`（插件给助手的说明，和给这台机器的 skill：这台已经有的只带名字和摘要）、`plugins.skills_need`（node 说缺这些 skill，下次带上文件）；`settings`：`settings.list`（hub 自己的、`jarvis_settings` 能读写的设置）；`inbox`（收件箱的来源，hub 的 `inbox.go` `inboxAppCall`；条目是助手的）：`inbox.collect {round}`（下拉刷新：hub 马上读一轮来源、经 `inbox.collected` 交过来再回答）、`inbox.kick`（尽快来一轮）、`inbox.config` → `{rules, agent_rules, agent, lark_account, translate, translate_fingerprint, extensions}`（分拣规则、AI 分身的配置、`lark.account`、设备要翻成哪些语言和翻译模型的指纹、插件提供的连接器）、`inbox.translate_fingerprint` → `{fingerprint}`（翻译模型现在是哪个，每次翻译现问，没配是错误）、`inbox.mail_body {account_id, uid, mailbox, readable?}` → `{from, to, cc, date, subject, text}`（读一封的全文，不标已读；`readable` 是翻译要的：只有 HTML 的信保留段落和链接）、`inbox.reply_mail {account_id, uid, mailbox, text, send_id}` → `{already?}`（用收到它的账号回这一封）、`inbox.reply {source, reply, send_id}` → `{already?}`（经连接器或接飞书的插件回复、改文档状态；没有这个连接器回「不能回复」）——同一个 `send_id` hub 只发一次（和 App 发消息同一张 `sent_messages`，留一天），再问是 `already`，上次发到一半断了的回 `assistant_in_doubt`、不再发、`inbox.settle {source, threads}` → `{over}`、`inbox.group_tool {source, call}` → `{result}`（结果去掉头像、截到 8 KB；连接器的 `unknown_method` 原样带回）、`inbox.group_fallback {source, fallback}`、`inbox.group_append {source, append, append_id}` → `{thing}`（同一个 `append_id` 再问，hub 用插件上次的回答，不再写一遍）；`app.tool {name, arguments, machine_id, session_id}` → `{content, is_error}`：只剩 `settings` 的 `jarvis_settings`，只管 hub 自己的设置（分拣规则、屏蔽的配置目录、邮箱），这一半还在 hub，助手自己的几项在助手（`assistant/settings_tool.go`） |
| `notify.request` | `{notification:{id, kind:"assistant", title, body, subtitle, device_id, session_id, asked?}}` → `{ok}` | 推送，和 Agent 的一样按各设备的设置推（`notifyDone`）；`asked` 是 `notify_user` 自己要推的：这段对话这一轮结束时不再推「完成」（`notified`）；发到飞书机器人和插件渠道是助手自己做的（2026-11-04 起，hub 不再 `botPush` / `channelPush`） |
| `inbox.changed` | `{items:[{id, source, kind, title}]}`，单向 | 收件箱的条目变了、存过了（一次最多报 50 件）：hub 给插件发 `inbox.changed` 事件、给 App 推 `{"type":"inbox"}`（没有条目也推：分拣状态变了）；hub 在总线的读循环外面做（一个插件读得慢不挡住助手别的消息），积压超过 256 条时这一批不发给插件，App 照样推 |
| `ws.broadcast` | `{message}`，单向 | 给 App 的实时更新，`message` 原样发出（`broadcast`）：`assistant_schedule`、`assistant_feed`、`task_plan` |
| `manager.snapshot` | `{epoch, seq, at, partial?, sessions:[{machine_id, session_id, title?, archived?, queued?}], interactions:[…], tasks:[…], work:{on, sessions:[{machine_id, session_id, work}]}, profile:{machine_id, main?, launcher, timezone?, keep:[{machine_id, session_id}]}, todos:{links:[{machine_id, session_id, todo_id, title, task?}], quiet:[{machine_id, session_id}]}}` → `{ok, seq}` | hub 只留最新的一份（同一个 epoch 里 seq 不比上一份大的回 `stale_snapshot`），`decorated` 照它给会话列表填标题（主对话的就是「Persona」）、归档、排队的消息，哪台机器的对话变了就推一次那台的会话列表；`interactions` 是助手在手机上问的（`ask_user`、`jarvis_settings` 改助手自己的设置，无头的 Claude 没有 AskUserQuestion），hub 照旧当一张卡片显示、推送、到期（`waitInteraction`），答案经 `manager.action` 回给助手，从列表里消失的就撤下；`tasks` 读不出来时是 `null`：模型配置的「定时任务」用途读它；`profile` 是助手的设置里 hub 自己的功能要用的（读不出来时是 `null`，hub 留着上一份），见下；`todos`（`assistantwire.TodoReport`）是每个 Agent 正在为哪个未完成的待办干活（`task` 是从对话派出去的任务）和安静的 Agent，`decorated` 照它填会话的 `todo`、`quiet`，`withTask` 照它在卡片上标任务名，`quietSession` 照它不推送（`hub/assistant_todos.go`）；读不出来时是 `null`（快照算不完整），hub 留着上一份；哪台机器的这几样变了也推一次那台的会话列表；`work` 是每个 Agent 最新一轮的工作态（`docs/agent-work.md`），`decorated` 只对还停在那一轮上的会话填 `work` 和由它算的 `state`，读不出来时是 `null`（hub 留着上一份）；`interactions` 里还有分诊开的卡片（`tool_name: persona_triage`，问的是那个 Agent 的会话；它没连着时答案 hub 记下，连上后再给） |

**hub 找助手只经** `assistant.Handler`（`assistant/handler.go`；hub 那头是 `assistantRemote`，同一组消息），每个方法是一条消息：App 的请求（助手的路由 `assistantwire.Routes`，经总线是 `http.request`，hub 原样转过去，调用方的角色放在 `X-Persona-Role`，客户端自己带的会被盖掉）、
`assistant.tools`（hub 把它们和自己的工具一起列给助手的对话）、`assistant.tool`、`assistant.context`（node 每条消息前要的实时快照）、`manager.action`（用户回答了助手的提问）、`assistant.todo_handoff`（`POST /api/v1/sessions`、`…/messages` 带 `todo_id` 时：这个待办在不在、完成没有，要交给 Agent 的话前面拼上生效的决策）、`assistant.todo_link`（交出去以后把这个 Agent 挂上待办）、
`assistant.start`（hub 自己的功能要开一段助手的对话；收件箱的 AI 分身、飞书机器人和渠道 2026-11-04 起都是助手自己开，hub 现在不用它，`{text, images?, launcher?, permission_mode?, effort?, purpose?, machine_id?, main?, untitled?}` → `{session}`；AI 分身的带 `untitled`，不按助手的对话起标题）、
`assistant.send_params`（`{machine_id, session_id, params}` → `{params}`：发给助手某段对话的消息跑在哪个启动器和模型上，新主对话空着开、第一条消息前面带上旧主对话留下的；手机发的、node 自己发的（`assistant.model`）都问它（飞书机器人和渠道发的是助手自己算）；这台机器还没列出、又不是主对话的原样返回；请求本身不对的回 `bad_request`，hub 回 400）、
`assistant.rename`（`{machine_id, session_id, params}` → node 的回答：App 给助手的对话改名，主对话不能改名回 `bad_request`，Codex 的只改助手记的标题，node 改名照旧由它发 `session.rename`）、
`agents.start_params`（`{machine_id, params, name_from?}` → `{params}`：App 开的 Agent 跑在哪个启动器和模型上、标签名；配置跑不了回 `bad_request`，hub 回 400）、`agents.resume_params`（`{machine_id, session_id, params}` → `{params}`：续上不知道启动器的会话用的启动器和模型）、`agents.tab_name`（`{prompt}` → `{name}`）、
知识库（2026-11-04，`assistant/kb_bus.go`，hub 那头都经 `hub/kb.go` 的 `kbAsk`）：`kb.context {chat_id}` → `{context}`（收件箱 AI 分身的会话上下文，没有这个会话是 `null`）、`kb.projects {machine_id, cwd}` → `{projects:[{slug, name}]}`（分诊时 Agent 目录所属的项目）、`kb.forget {source}` → `{chats, people}`（删除一个来源的数据，连同索引行、项目里的引用、游标）、插件程序交给 hub 的帖子（hub 看过「进知识库」开着，带上连接器的名字转过来）`kb.posts_put {connector, sources, posts}` → `{statuses}`、`kb.posts_comments {connector, id, count, comments}` → `{}`、`kb.posts_pending {connector, limit}` → `{posts}`、`kb.archived_chats {connector, sources, source}` → `{chats}`，
单向事件 `kb.kick {}`（来源有新东西，5 秒后转一轮）、`kb.agent_turn {machine_id, session_id, cwd, title?, line}`（一个 Agent 一轮做完，记进它目录所属项目的时间线；助手没连着时 hub 排着，连上就发）、
hub 看到的事（`session.user_spoke`、`session.turn_started`、`session.turn_ended`、`sessions.changed`（某台机器的会话列表：对话之间的消息、待办的随项目自动挂 / 派生跟随 / herdr 标记 / Agent 标题都从它来）、`interaction.resolved` 用户处理了某个 Agent 的卡片（任务的对话记一笔，挂在待办上的 Agent 的选项提问记成决策）、`todos.space_renamed` hub 改了工作区的名字、`interaction.raised` 安静的任务的 Agent 在问（#1731）、`session.seen` 用户在 App 里看了一个会话（分诊的统计）、`manager.updated` 握手的回答变了；2026-11-04 去掉 `assistant.tasks`、`assistant.card_sent`、`assistant.cards_sent`、`inbox.message`：收件箱在助手里了），
以及 `manager.resync`（hub 还没有快照或只有一份不完整的：hub 刚起来、数据库没答）。
收件箱的（`assistant/inbox_bus.go`）：`inbox.collected`（hub 读完一轮来源交过来：`{epoch, seq, round, sources:[{id, name, off?, extension?, migrated?, group?, meta?, meta_description?, settles?, kinds?, things}], mail?, names}`，助手合成条目、分拣后回答；`epoch` 是 hub 这次运行、`seq` 是轮次，比已经用过的旧的一轮丢掉；助手读不出存着的收件箱时回 `inbox_unavailable`，hub 下一轮再交）、`inbox.item {id}` → `{item, ok}`（插件详情页插槽要的那一条，存的 JSON）、`inbox.forget {source}` → `{deleted}`（删一个来源的数据）、`inbox.repo_mutes {source, set?, add?, repos?}` → `{repos}`（GitHub 设置页的「静音的仓库」）。快照多一项 `inbox: {agents:[{machine_id, session_id}]}`：AI 分身的对话，hub 照它不推「完成」（读不出来时是 `null`，hub 留着上一份）。
hub 的错误带码过总线（`no_device`、`machine_offline`、`node_closed`、`stale_snapshot`、`model_paused`（带暂停的原因：额度用完还是并发满）、`model_unconfigured`（这个用途没配模型）、`assistant_in_doubt`（收件箱的回复上次发到一半断了）），助手认回原来的错误，判断照旧。App 看到的路由、事件、字段和推送都和以前一样。

**状态**经助手自己的 `assistant.Store`（`assistant/store.go`）：settings 的 `assistant`（它自己的设置，见下）、`assistant.main_handoff`、`assistant.titles`、`assistant.archived`、`assistant.relay`、`assistant.tasks`、`assistant.runs`、`tasks.trust`、收件箱的 `inbox_mutes`（旧的 `assistant.routines` / `assistant.followups` 照旧只在第一次读时搬），渠道的 `channels`、`channels.state`、`bots.feishu.state`（2026-11-04 起；`bots.feishu` 的凭证、配对和功能开关仍是 hub 的），表 `task_plans`、`assistant_feed`、`todos`、`todo_spaces`、`todo_agents`、`todo_decisions`、`inbox_items`。
知识库的状态是数据目录（`~/.persona/data`）里的文件、`~/.persona/handles.key` 和表 `kb_entities`（`docs/knowledge.md`）。
它就是 hub 的 Postgres，键和表都和以前一样、不迁数据；助手的进程自己连这个库（和 hub 读同一个 `database_url`），只有助手读写这些键和表，hub 不碰。
定时任务的循环和关掉做完的任务的 Agent 的循环是助手自己的（`Service.Run`，第一次连上 hub、拿到握手的回答后才开始）。

**助手自己的设置**（settings `assistant`，2026-11-04 起只有助手读写，`assistant/profile.go`）：对话跑在哪台机器、启动器、用户的时区（手机经 `PUT /api/v1/assistant` 报）、关于我、主对话、换下来的旧主对话、`main_tasks_quiet`。
hub.json 的 `assistant` 一节是它们的默认值（握手给）；JSON 和以前 hub 写的一样，不迁数据。助手读一次用 5 秒（只有它写，`manager.updated` 来了马上重读）。
它每份快照都带 `profile`：hub 自己的功能要用的那部分，hub 留最后一份（`assistant_profile.go` 的 `assistantProfile`），变了就存一份在它自己的键 `hub.assistant_profile`（只有 hub 读写）：

- 手机发给主对话的消息认出主对话（`isMainOn`），模型配置「主对话」用途跑在哪台、它的默认启动器（`launcher`：助手自己的，没有就是 hub.json 的，再没有是 claude），收件箱 AI 分身默认的机器，`/api/v1/skills`、启动器测试和额度挑的机器，`assistant.lark` 只给助手那台。
- 用户的时区（`userLocation`，`hub/usertz.go`）：hub 读时间都按它。
- `keep`：各台 node 不管多久没动都要列出的会话（主对话、换下来的旧主对话、定时任务自己的对话）。和上一份不一样的机器 hub 发一次 `node.configure keep_sessions`，第一份来时每台都发；node 连上时 hub 也带上（`configureNode`）。
- **hub 重启后、助手还没报的那一两秒**：hub 按自己存的那份（`loadAssistantProfile`，在插件、连接器起来之前读）。开机就起的插件要用户的时区，
  只在内存里留的话这几秒是 hub 自己的时区：插件一直用 hub 的时区到它重启。
  （知识库和飞书机器人以前也靠它：知识库每次重启都整份按 hub 的时区重排、两分钟后再排回来，飞书这时发来的消息新开一个主对话把原来的顶掉；2026-11-04 起两者都是助手的，知识库启动时直接读助手自己设置里的时区，飞书的对话由助手决定。）
  设置挪进助手后的第一次启动还没有这份：从助手的设置（settings `assistant` 里的机器、主对话、时区）读一次，存成 hub 自己的，以后不再读；这时 `keep` 不知道，node 连上时不带 `keep_sessions`（node 留着它原来的）。
  一份都没有（新装的 hub）：机器按 hub.json（没有就是 hub 自己那台），没有主对话，时区是 hub 自己的，启动器是 hub.json 的。

**渠道**（2026-11-04 从 hub 挪过来，`docs/channels.md`「谁管什么」）：飞书机器人和插件渠道里「和 Persona 对话」的那一半是助手的——消息进哪段对话、命令、发进对话、跟踪这一轮、
进度卡 / 答案、提问和批准（卡片、文字）和它们的回答、推送和同步、来源标记，以及 `/api/v1/channels*` 的路由。hub 只管收发：飞书开放平台的长连接、凭证、配对、只认主人，插件的 `channel` 程序。
总线上多了这些（类型在 `assistantwire` 的渠道一节）：

| 方向 | 方法 / 事件 | 做什么 |
| --- | --- | --- |
| hub → 助手，调用 | `channels.receive {channel, message, action}`、`feishu.message {event}` → `{}` | 主人的一条消息（插件渠道的；飞书私聊里的，原样的事件）。助手收下（按消息 id 去重）就回，后面的在它那儿跑；没连着时 hub 先等它最多 60 秒、断了超时就再交一次，等不到才回主人「助手暂时连不上」；助手一时答不上要的东西回 `try_again`，hub 再交 |
| | `feishu.card_action {event, action}` → 卡片回调的回答 | 主人按了机器人的卡片（hub 认过是主人），飞书等 3 秒：助手 1.5 秒内答不完先回「正在处理…」，做完再改卡片。`action` 是一次性的主人操作，只能答这一张卡 |
| | `feishu.view {}` → `{target, agents}` | `GET /api/v1/bots/feishu` 里「现在发给」和绑定的 Agent |
| hub → 助手，事件 | `interaction.announced {interaction}`、`interaction.ended {interaction, resolution?}` | 新的卡片（安静的任务的除外）、结束的卡片（答了的带回答）：飞书卡片、文字提问 |
| | `session.items`、`session.delta` | 助手跟着的对话（`session.follow`）的新条目和正在写的字：进度卡 |
| | `feishu.owner {paired}`、`channels.noted {channel, machine_id, session_id}` | 机器人配对 / 解除配对了；一条渠道来的消息经 App 的接口进了对话（语音的 `voice-…`） |
| 助手 → hub，`device.call` | `session.follow` / `session.unfollow {session_id}`、`session.send_once {…session.send 的参数}` | 跟一段对话（断开后 hub 全部放掉，连回来助手再跟）；发进对话，和 App 同一个只发一次的保护（`sent_messages`） |
| 助手 → hub，`app.call` | `feishu.status`、`feishu.post`、`feishu.text`、`feishu.update_card`、`feishu.react`、`feishu.unreact`、`feishu.messages`、`feishu.resource`、`feishu.save`、`feishu.user_name`、`feishu.configure`、`feishu.test` | 飞书的传输：发 / 回 / 改卡片和文字（只发给主人的私聊）、表情、读消息、下载图片、文件存到那台电脑、开关和功能 |
| | `channels.list`、`channels.send`、`channels.typing`、`channels.enable`、`channels.unbind`、`interactions.resolve {interaction_id, resolution, action}` | 插件渠道的传输和渠道列表（不带目标、插件渠道的功能）；按渠道里的回答答掉卡片：要带 hub 发的主人操作（批准类不代答），没有就拒绝。绑定（带凭证）是 hub 自己的路由，不经过助手 |
| 快照 | `channels: {marks:[{machine_id, session_id, channels}], turns:[{machine_id, session_id, answered?}]}` | 每段对话从哪些渠道来过（会话列表的 `channels`）、哪段对话的一轮正在 / 刚在渠道里答（hub 不再推「完成」）；`null` 时 hub 留着上一份 |

**为什么照总线来画**：接口就是线上的消息，hub 那头的 `assistantLink` 是唯一的实现，总线上只多了编解码；hub 这边不再算助手的状态（标题、归档、排队、提问都是助手报什么就显示什么），也不再懂助手的语义（工具清单、提示词、提问的意思都在助手那边）。
好处：谁管什么清楚了；助手崩了、卡住了不拖垮 hub（反过来也一样），可以单独重启、单独升级；助手可以跑在别处，以后可以接不止一个；hub 不再随助手的功能长出新的状态。

**还留在 hub 的**（后续再挪）：

- App 的重命名接口（`renameSession`）还是 hub 的：助手的对话交给助手（`assistant.rename`），别的照旧。
- 只给助手用、但做的是 hub 的事的工具：`jarvis_settings` 里 hub 自己的设置（分拣规则、屏蔽的配置目录、邮箱）经 `app.tool` 回到 hub 执行；飞书的工具是飞书插件（连接器）的，本来就不是助手的。
- `findAgent`、`machineByName` 两边各一份（hub 的找 `*Machine`、给操控电脑用，`hub/tools_shared.go`；助手的经总线问 `devices.list` / `sessions.list`）。`conversationName` 只剩助手的一份（飞书机器人挪过去了）。
  `clip`、`oneLine`、`firstLine`、`ago`、`short`、`agentLabel`、`stateNames` 只有一份（`server/internal/textfmt`），两边各自用原来的小写名字指向它。

和以前能看出来的差别：

- 发给一段正在跑的对话的消息在它这一轮结束后送进去时，会话列表马上推一次（排队的那条消失）；以前要等那段对话开始工作的那次推送才一起变。
- 助手没连着的时候（hub 刚起来、助手的进程在重启）：它的接口回 `503 assistant_offline`「助手暂时连不上，稍后再试」，它的工具同样失败；会话列表照常（hub 留着它最后报的快照），只是 hub 刚起来、助手还没报的那一两秒里助手对话没有标题。
- `ws.broadcast` 的消息原样转发（加上 hub 给每条消息编的 `event_seq`），字段的顺序是助手写的顺序（以前 hub 重新编一次，键按字母排）。
- 2026-11-04 设置和主对话挪过去之后，助手没连着时这些也回 `503 assistant_offline`：`GET/PUT /api/v1/assistant`、预热、记忆、新对话、压缩、新建主对话，手机发给助手对话的消息、给助手对话改名；
  飞书机器人和渠道插件发给主对话的（以及开新对话）在飞书 / 渠道里回「助手暂时连不上」，收件箱的 AI 分身这一件记成失败（错误同一句）；node 自己发的消息问不到时用对话自己的启动器（和以前问老 hub 一样）。
- 安卓先空着开新主对话、第一条消息从飞书或渠道发来时，这条也带上旧主对话留下的（以前只有手机发的带，飞书发的漏掉，背景一直挂着等手机的下一条）。
- 定时任务换了自己的对话之后，换下来的旧主对话 node 也照样常驻列出（以前助手自己发的 `keep_sessions` 漏了它们，要等 node 重连）。
- Agents 这件事挪到助手以后（2026-11-04，`docs/agent-work.md`「谁管什么」）：它没连着时新建 Agent 先等它一会儿（hub 刚起来最多 60 秒、它刚断开 10 秒），等不到由 hub 自己套「模型配置 › Agents」，标签名是 `agent-<n>`；它的卡片的答案 hub 记下，它回来报了快照以后再给；分诊暂停，重新连上后的下一次检查（15 秒内）开始把这期间结束、还没看的轮次补上；会话分诊的开关、统计、`POST /api/v1/tabs/autoname` 回 503；会话列表留着它最后报的工作态。hub 刚重启、它还没报的那一两秒，分诊过的会话没有 `work`（显示成完成未看）。
- 知识库挪到助手以后（2026-11-04）：助手没连着时 `/api/v1/kb…` 回 503，`kb_*` 工具失败，删除来源数据的接口回 503、什么都不删；归档等它回来，什么都不丢（游标是它的、插件下一轮重交、Agent 的那一行 hub 排着），见 `docs/knowledge.md`「谁管什么」。
- 待办挪到助手以后（2026-11-04）：助手没连着时待办的接口和带 `todo_id` 的新建 / 发消息回 503；随项目自动挂、派生跟随的 Agent 在下一次推送里才带上待办（以前同一次推送就带上）。细节见 `docs/todos.md`「谁管什么」。
- 收件箱挪到助手以后（2026-11-04）：助手没连着时 `/api/v1/inbox…` 和翻译 `/api/v1/translate*` 都回 503 `assistant_offline`（回复被拒不写回执，回来后能再发）；hub 照常按时读来源，读到的替助手攒着（合并、有上限、只在内存里），助手一连上连同攒着的来一轮完整的采集，邮件最新 40 封、iMessage 最新 30 条这种窗口里滑出去的不会丢。
  回复在路上断了（hub 和助手的连接断了、超时）回 502 `assistant_in_doubt`、`delivered: "unknown"`：可能已经发出去了；不写回执，同一条再发时助手带同一个 `send_id`，hub 发过的不再发。
  插件的收件箱详情页插槽这时画不出、GitHub 的收件箱设置（CI、静音的仓库）整块不显示（只给不带静音仓库的设置，保存时会把静音全清掉）、删连接器数据时收件箱那部分报错。细节见 `docs/inbox.md`「谁管什么」。
  以前 hub 刚起来就采一轮；现在等助手连上（hub 起它的子进程，通常一两秒）。
- 渠道挪到助手以后（2026-11-04）：助手没连着时 `/api/v1/channels*` 回 503（绑定 `POST …/bind` 除外，它是 hub 的）；飞书机器人和插件渠道照常收消息，主人的消息先等助手最多 60 秒，等不到才回「助手暂时连不上」（插件渠道是「没发出去：助手暂时连不上，稍后再试」），
  按飞书卡片回同样一句 toast；飞书机器人自己的设置页（`/api/v1/bots/feishu*`）照常，只是没有「现在发给」和绑定的 Agent。这期间来的提问只在 App 里，连回来后补发到渠道；hub 刚起来、助手还没报的那一两秒里会话没有 `channels`（「完成」推送最多多等 10 秒它的第一份报告）。配对成功时的「已配对」卡片由助手发（带它的帮助），助手不在时 hub 发一张简短的。插件渠道一条消息交给助手的图片和文件加起来最多 40 MB（总线一条消息的上限，以前单个文件 50 MB）。细节见 `docs/channels.md`「助手不在的时候」。

## 进程（2026-11-04，#1446）

`personad assistant` 就是助手：读 hub.json（`~/.persona/hub.json`，环境变量同 hub：`PERSONA_DATABASE_URL`、`PERSONA_LISTEN`）连同一个 Postgres，用总线 token 连 hub 的 `/api/v1/bus`，
握手、报整份快照，然后开始跑定时任务；连接断了就自己重连（1 秒起翻倍到 1 分钟），每次连上再报一次整份快照。

- **默认（用户什么都不用改）**：`personad hub` 启动时用 `createBusToken` 给助手生成一个新 token，从自己的二进制起一个 `personad assistant` 子进程，token 写进它的 stdin（不放命令行，`ps` 看不到），
  `PERSONA_ASSISTANT_HUB` 是 hub 自己的本机地址。子进程的输出进 hub 的日志（stderr，以 `assistant:` 开头）。它退出了 hub 再起：1 秒起翻倍到 1 分钟，跑满 1 分钟后再退出就从 1 秒算。
  hub 停的时候先 SIGTERM，10 秒没退就杀掉；hub 没了（崩了也一样）它的 stdin 就关了，它跟着退出，不会留一个连不上的助手。systemd 单元、Docker 镜像和 `deploy.sh` 都不用改。
- **跑在别处**：hub.json 写 `"assistant": {"external": true}`，hub 就不起它（`docs/config.md`）；在要跑它的机器上用 `personad hub assistant-token` 生成 token，
  再 `PERSONA_ASSISTANT_TOKEN=<token> PERSONA_ASSISTANT_HUB=<hub 地址> personad assistant`。它要能连到 hub 的 Postgres。
  知识库跟着它：在那台机器的数据目录里（hub 机器上原来的 `data/` 和 `handles.key` 要接着用就拷过去），见 `docs/knowledge.md`「谁管什么」。
- 助手没连着的时候 hub 的表现见上一节最后。hub 每次启动都换一次 token，所以 `external` 没打开时别处跑的助手会被挤掉（旧 token 失效）。

## 飞书：lark-connector skill（2026-10-30）

> **2026-11-04 起（#1468）**：闸门是通用的「命令闸门」：命令名、脚本、凭据、规则都由插件的 `assistant.gate` 给（`docs/extensions.md`「助手」），
> node 里不再有 lark-connector 的默认规则；装了经 lark-connector 接飞书的插件时 `ol` 的闸门和凭据来自插件，没装时 hub 自己的 lark-connector 合成同样的闸门（3c 删掉）。
> 下面说的行为（读放行、别人看得到的写在手机上确认、拒绝 token / 直接 curl）不变。3c（#1520）之后 hub 不再合成这个闸门，lark-connector 技能、
> 闸门规则和凭据只来自插件；仓库里的 `skills/lark-connector` 副本和同步脚本已删。

用户要求：「lark-connector 自带 skill……现在只走我们的 MCP lark_* 工具，自由度低」「skill 弄一个副本就行，不搞整个 lark-connector 仓库丢进去」。

- **副本**：仓库的 `skills/lark-connector/` 是 lark-connector 仓库 `.agents/skills/lark-connector` 的原样副本（`SKILL.md`、`references/`、
  `scripts/ol.sh`；不要 `evals/` 和要 Node 的 `*.mjs`，hub 上没有 Node），来源 commit 记在 `skills/SOURCE.md`。
  同步上游：`scripts/deploy/sync_lark-connector_skill.sh <lark-connector checkout>`，看 diff 后提交。
- **怎么到助手手里**（2026-11-02 起，`docs/hub-update.md`「skill 从二进制里拿出来」）：它**不在 personad 里**（不能再分发），
  放在跑助手那台机器的 `~/.persona/skills/lark-connector`（用户自己的 skill 空间），node 每次起助手进程时把它链接到 `~/.persona/assistant/.claude/skills/lark-connector`。
  旧版本 jarvisd 装进去的副本，node 第一次起助手时自动搬到 `~/.persona/skills/`。没有这个 skill 时没有 `ol`，助手的 CLAUDE.md 也改成只用 MCP 的 `lark_*`。
  有的话 `~/.persona/assistant-bin/ol` 是一行包装，放在助手进程的 PATH 最前，助手直接 `ol GET /me`。
- **token**：node 起进程前向 hub 要（RPC `assistant.lark`，只回给 assistant 所在的机器）：hub.json `lark.base_url` 去掉 `/agent/v1` 作为
  `lark-connector_AGENT_BASE`，`lark.token`（没写就照旧从 lark-connector 容器读）作为 `lark-connector_AGENT_TOKEN`，只放进这个进程的环境变量，不落盘、不进日志。
  ol.sh 找 token 的顺序是这个环境变量 → `lark-connector_ENV_FILE` → 所在仓库的 `.env`；副本不在仓库里，只靠环境变量（实测通）。
- **闸门**（`node/larkgate.go`）：助手进程用单独的一组 hooks（插件 `assistant-hooks`，更老的 Claude Code 是 `assistant-settings.json`，`docs/claude-integration.md`「Hooks」），多一个 Bash 的 PreToolUse hook（herdr 里的 Agent 不受影响）。
  hook 读整条命令：
  - 只有 `ol` 调用（和 jq / head / grep / echo / cd / `timeout N` 这类只输出的命令）时逐个判断：读（GET、只读的 POST 如 `/context/harvest`、
    `/search/page`）直接放行；只影响他自己的（标已读、`/todos/*/complete` 带 `scope:self`、找单聊 `POST /conversations {type:p2p}`）放行；
    其余写（发 / 回复 / 撤回 / 转发、表情、文档、日程、审批、邮件、群设置……）在手机上弹「飞书」批准卡片：做什么（发消息 / 表情回复 / 建改日程…）、
    会话名（hub 查）、method、路径和要发的 body（`@file` 会读出来）。拒绝时把用户的话作为原因还给助手；「本会话总是允许」记住这类路由（id 抹掉）。
  - 碰 `lark-connector_AGENT_TOKEN`、`/proc/*/environ`、直接 curl lark-connector、给 ol 加 `-v` / `--trace` / `-w` 这类会把请求头打出来的参数：直接拒绝。
  - 别的命令（接 python、写到 /tmp 以外、`$(…)`）：闸门不管，按权限模式走普通的 Bash 批准；但只要里面有 `ol` 的写（`ol POST …`），
    不管权限模式（包括 bypass）都弹「飞书」卡片，整条命令给他看。
  - 规则是配置：写在 hub.json `connector_gate`，和其他连接器的写操作一个写法，`connector` 写 `lark`，`action` 写「方法 路径」（path 里 `*` 一段、`**` 剩下的全部，
    `"*"` 是任何调用），`match` 按 body 的顶层字段，`decision` 是 `allow|confirm|deny`；排在内置规则前面，第一条匹配的说了算。
    比如以后想让发消息不再确认：`{"connector":"lark","action":"POST /conversations/*/messages","decision":"allow"}`。
    老的 `lark_gate`（`{method, path, body?, action}`）照样生效，排在 `connector_gate` 的飞书规则后面；hub 把两份合成一份交给 node（`connector.LarkRules`，2026-11-03 迁移 M5）。
- 早报、「今天要我处理什么」可以用 `ol POST /context/harvest '{"preset":"today"}'`（@我、日程、待办、我的审批，一次拿到）。

## 飞书：lark-cli（2026-11-03，#273 的 L1）

没有 lark-connector 的 hub 也能让助手读写飞书：用飞书官方的开放平台命令行 lark-cli（`@larksuite/cli`），方案和后面几步见 `docs/plan-lark-cli.md`。
这一步只做助手，收件箱、知识库、`/agent` 还是只认 lark-connector。

- **怎么开**：hub.json `lark.cli`（`docs/config.md`），lark-cli 装在助手所在的机器上，用户自己 `lark-cli auth login --domain all` 登录一次。
  lark-connector 可以同时配着，`lark.prefer` 定提示词里谁优先。
- **怎么到助手手里**：node 起助手进程前向 hub 要配置（RPC `assistant.lark` 多了 `cli`、`prefer`；只配 lark-cli 时不去问 lark-connector，也就没有它的 token），
  在 `~/.persona/assistant-bin/lark-cli` 写一行包装：配置的那个 lark-cli（不是绝对路径时在登录 shell 里找）加上配置的 `--profile`，
  并把 lark-cli 所在目录放进 PATH（npm 的 lark-cli 是 `env node` 的脚本，要找得到 node）。没配 `lark.cli` 或找不到 lark-cli 就不写。
  lark-cli 的凭据留在它自己的 `~/.lark-cli`，hub 和 node 都不碰。
- **说明书**：不往助手目录拷 skill。CLAUDE.md（`prompts/assistant/claude_md.md`）多一段，让它先 `lark-cli skills read lark-<领域>` 读 lark-cli 自带、
  跟版本走的领域说明，参数用 `--help` / `lark-cli schema`。只有 lark-cli 时说明没有未读和公司圈、`lark_*` MCP 工具用不了，找要处理的消息用
  `im +messages-search --is-at-me / --chat-type p2p`；两个都有时按 `lark.prefer` 分工（golden `assistant_claude_md.lark_cli`、`.both`）。
  发消息、回复消息让它用应用的机器人（`--as bot`）：用户身份要应用开 `im:message.send_as_user`，默认的应用没有（2026-11-04 实测）；其余读写用用户身份。
  只配了 lark-cli 时，机器上留着的 lark-connector skill（旧版 jarvisd 拷进助手目录的）不算：没有 lark-connector 可连，CLAUDE.md 里不提 `ol`
  （2026-11-03 部署到 hub 机器时撞上：说明书先推荐 `ol`，又说没有 lark-connector）。
- **闸门**（`node/larkcligate.go`，接在 `ol` 的闸门里，同一个 `larkCheck`，Codex 也走它）：lark-cli 自己知道哪些命令会写，每个命令的 `--help` 都打印
  `Risk: read / write / high-risk-write`，node 第一次见到一个命令时问一次（按命令缓存，约 50 ms）：
  - `read` 直接放行；`write`、`high-risk-write`、问不出来的都在手机上弹「飞书」卡片，写的是谁做（带 `--as bot` 是「以机器人身份」，否则「以你的身份」）、做什么（发消息、回复、表情、撤回…）和整条命令，
    高风险、没给风险等级的另外标出来。拒绝时把他的话还给助手；「本会话总是允许」记住这个命令。
  - 只打印的直接放行：`--help`、`--version`、`--dry-run`、`schema`、`skills`、`whoami`、`doctor`、`auth status / check / scopes / list`、`event list / schema / status`；
    `lark-cli api` 按 HTTP 方法：GET / HEAD 放行，其余确认（卡片上是方法、路径和 `--data`）。
  - 拒绝：`auth login / logout`（登录他自己做）、`config`、`profile`、`update`、自己传 `--profile`、碰 `~/.lark-cli/`。
  - 闸门读不懂的命令（循环、`$(…)`、接 python）里有 lark-cli：只要有一个不是已知的读，就整条命令在手机上确认，不管权限模式。
  - hub.json 的规则先匹配：`connector_gate` 里 `{"connector":"lark","action":"CLI /im/+messages-send","decision":"allow"}`（老的 `lark_gate` 写法
    `{"method":"CLI","path":"/im/+messages-send","action":"allow"}` 也行）。
- **还没做**：runtime 助手（DSH、pi）的提示词和 `lark-connector` 工具还只认 lark-connector；设置页的 lark-cli 状态、缺的权限（L2）。

## 本会话总是允许（2026-11-05，#2202）

助手对话里的授权卡点了「本会话总是允许」，这段对话里就一直有效，进程换了也一样。以前只靠 Agent 自己记：Claude 是 PermissionRequest hook 回的
`addRules … destination: session`，Codex 是 `acceptForSession` / 权限 `scope: session`，飞书闸门记在 node 的内存里。可主对话的进程随时会换
（空闲 20 分钟回收、部署、换启动器 / 模型、bypassPermissions 只能启动时给），`--resume` 起来的新进程没有之前的会话规则，node 重启后闸门也忘了，同一个操作又开始问。

- node 在助手对话的授权卡上写明「总是允许」放行什么（`always_keys`，Agents 页的会话不带，行为不变）：Claude 是 `tool:<工具名>`（和 addRules 一样是整个工具）；
  Codex 是 `codex-command:<命令>`、`codex-edit:<文件>` / `codex-root:<目录>`、`codex-permissions:<权限>`（和 Codex 自己「本会话」的范围一样）；
  飞书闸门是 `lark:<路由>`（id 抹掉；整条命令确认的不记，和以前一样）。
- hub 收到以「总是允许」批准的卡，把这些 key 记进 Postgres 的 `assistant_always`（对话 id + key 的 sha256）。之后这段对话的卡上的 key 都记过，hub 直接回「允许 + 总是」，
  不弹卡、不推送；新进程照样拿到会话规则，这个进程里之后不再问。这段对话里正等着的、被这次覆盖的卡一起答掉（并行的工具调用会同时来几张）。
- 只记放行：拒绝、只允许一次都不记；卡上有一个没记过的 key 就照常问。还没有撤销入口：新建对话即可（或删 `assistant_always` 里这段对话的行）。

## 主对话、知识库和上下文流动（2026-10-30）

提示词都在 `server/internal/prompts/assistant/`（`docs/prompts.md`）：CLAUDE.md 是 `claude_md.md`，快照是 `snapshot.md`（主对话那句在 `context.md`），定时任务到点的消息是 `task_run.md`，默认的早报 / 晚间回顾是 `routine_*.md`。


- **主对话**：第一段对话是主对话（助手的 settings `assistant.main`），Persona tab 默认打开它；快照开头写明「这是主对话」，提示词要求接着之前聊的往下做。
  node 的会话列表里离线会话只列 7 天内最新的 80 个，忙的机器上安静两天的主对话就被挤掉了，侧边栏没有「主对话」（2026-11-01）。
  助手把这几个会话报在 profile 的 `keep` 里，hub 在 node 连上时、它们变了时用 `node.configure` 的 `keep_sessions` 告诉 node，不管多旧都列出来。
  其他页面交给 Persona 的东西（Agents 长按「交给 Persona」、知识库「问 Persona」）都以 `<jarvis-context>` 发进主对话。
- **知识库**（`docs/knowledge.md`）：工具 `kb_search` / `kb_list` / `kb_get` / `kb_update`；快照带重点项目和会话；提示词要求把对话里得出的结论写回
  （项目进展写时间线、项目代码目录写 `dirs` 让 Agents 对上项目、给 Agent 派活前先读项目档案）。
- **AI 分身**：处理一件事时提示词附上这个会话和它前 3 个项目的档案，处理完把新结论写回项目。
- 助手和 AI 分身的默认启动器在 hub.json `assistant.launcher` / `inbox.agent.launcher`（这台机器的具体值见 `CLAUDE.local.md`）。
- 「设置 › 模型配置 › 主对话」选的启动器（存在 `model_config.assistant`，旧的 settings `assistant.launcher` 兼容；#1291 起「设置 › 助手」不再有这个入口）管主对话和新对话：hub 给主对话发消息、App 没指定启动器时用它，
  node 发现和正在跑的不一样就用新启动器 `--resume` 接着原来的会话（2026-11-01 修：以前只影响新对话，主对话一直用最初的启动器，
  看起来「换了不生效」）。主对话每条消息都按这个设置，不看它上一轮在跑什么：#1355 一度改成沿用正在跑的，被一条跟进切到别的启动器后就一直回不来（#1518）。
  助手用 `schedule_followup` 给自己定的跟进发回原对话时走同一套规则（主对话按设置，其他对话沿用自己的），不按 Agents 分类（`assistant/start.go` 的 `sendModel`，hub 经 `assistant.send_params` 问）。定时任务自己的对话（早报等）也按「主对话」分类，在里面发消息、跟进发回来都和主对话一样每条按设置（#1544）。顶栏菜单里的「启动器」只换当前这一段对话的下一条消息；点开启动器还能选它模型列表里的模型（`/model` 那份），
  随消息的 `model` 发出，hub 据此算执行指纹并让 node 重启进程；不选就是该启动器的默认模型。
- **对话在忙时不换**（#1554）：进程正在回答或有后台任务时，启动器 / 模型变了也照样把这条消息交给正在跑的进程（会话状态仍记它实际在跑的），
  等它空闲后的第一条消息再重启换过去；以前直接拒收「模型配置已更新，请等当前任务结束后再发送」，助手又不断起新的后台任务时用户一直发不出去。
  重启后 node 自己告诉对话「后台任务丢了」那条也先问 hub（`assistant.model`，hub 转给助手的 `assistant.send_params`，和手机发的消息同一套），不再按会话上次跑的模型起进程。

## 坑

- 模型会把 ask_user 和别的工具**并行**调用（实测：ask_user 和 schedule_followup 同一毫秒发出），所以 ask_user 不能当「先确认再做」的唯一闸门；
  真正有副作用的工具（`lark_send`、`mail_send`）自己在 hub 里要批准。
- 定时任务错过太久不补跑：hub 停了一上午，不会中午才发早报（晚于计划 30 分钟就跳过，只记录）。
- 定时任务、跟进、运行记录只有从库里读成功才算加载完：读失败时不会退回默认的两条定时任务，相关接口回 503，工具报错，调度这一轮跳过，什么都不写，
  下次调用再读。以前读失败也算加载完，下一次保存就把库里真实的定时任务和跟进覆盖掉了（2026-11-02 评审发现，`TestScheduleReadFailureNeverOverwrites`）。
- 静默推送会被系统限流，强制退出的 App 收不到；唤醒失败时工具结果会告诉模型「请他打开 App」。
- 快照以前放在 `--append-system-prompt`，以为每次重启都会换成新的；其实 Claude Code 续聊时一直用对话第一次启动时记下的系统提示（见下节），
  助手看到的一直是那段对话第一天的时间和未读。现在每条消息都带。
- Go 的零时间 `0001-01-01T00:00:00Z` 会出现在 `last_run` / `next_run` 里，App 用 `Date.realDate` 当作「没有」。
- **无人值守的运行不能卡在批准上**（2026-10-30 实测）：助手默认权限模式下，连 `notify_user`、`schedule_followup` 都要在手机上点「允许」，
  夜里到点的跟进、Monitor 事件就停在批准那一步，推出去的只有一条「需要批准」。现在 Persona 自己的工具预先放行（见上表）。
- **systemd 重启 hub 服务（兼容 unit `jarvisd`）会杀掉整个 cgroup**：助手的 claude 和它的后台命令、Monitor 一起没了，node 来不及做任何收尾，所以「哪些任务在跑」
  要随时落库（`node_sessions.tasks`），不能等关机时再记。
- **起不来的进程会吞掉消息**（2026-11-05）：node 先把消息写进新进程的 stdin、马上回 hub「送出了」；启动器这时已经失败退出（例如 `jarvisd launch opus: no jarvis node`），
  消息就丢了，App 一直停在「正在送达」，日志里只有 zsh 主题插件一行带 `error` 字样的提示。现在新起进程的那一次发送等它第一行输出（最多 `assistantStartWait` 8 秒）：
  先退出就报错「<启动器> 没有启动起来，消息没有送达：<stderr 最后几行>」，App 显示发送失败可以重试；超过 8 秒还没输出按启动处理（慢的 SessionStart hook）。
  stderr 的尾巴去掉颜色码和空行后记在进程上（`watchAssistantStderr`）。
- **node 的 socket 文件被别的进程顶替**（2026-11-05）：另一个 node 在同一路径上监听过、退出时不删文件（`SetUnlinkOnClose(false)`），路径上就剩一个没人听的 socket，
  所有 `jarvisd launch <Persona 管理的启动器>` 和 hook 都 `connection refused`。原来的看门狗只看文件在不在；现在每 15 秒还看它是不是自己那个（`os.SameFile`），
  不是时拨一下，没人应答才重建（`sockGone`），有人应答说明是更新的 node 在用，不抢。
- 中文环境下 `.hour(.twoDigits(amPM: .omitted))` 是 12 小时制（17:00 显示成「05:00」）：时刻一律用 `AssistantScheduleText.hm`（固定 `HH:mm`）。

## 实时快照和前缀缓存（2026-11-01）

**为什么快照不能放系统提示**：提示词缓存是按前缀匹配的，一次请求从前往后依次是系统提示（含工具定义）、项目上下文（CLAUDE.md、记忆）、
整段对话；前面任何一个字节不同，后面的整段历史都读不到缓存，要按「写缓存」的价钱和速度重算一遍。助手进程空闲 20 分钟就回收，下次说话
`--resume` 重新起，所以「每次启动都一样」的东西才能放在前面：快照（时间、未读、会议）每次都不同，只能跟着消息走，放在对话末尾。

**实际的情况比这更绕**（Claude Code 2.1.314 实测，hub 机器、走同一个启动器）：

- Claude Code 续聊时**不用**新给的 `--append-system-prompt`，一直用对话第一次请求时记下的系统提示，压缩后才换（文档「System prompt flags in
  resumed conversations」，实测换了文字后模型答的还是第一次的暗号、缓存全读到）。所以以前的快照并没有让缓存失效，而是**一直是第一天那份**。
- 真正让重启后读不到缓存的是**工具定义变了**：把一段 27.5 万 token 的真实助手对话复制一份（换个会话 id、放在临时目录，Persona 工具用导出的
  同一份定义）后重启，结果见下表。schema 里键的顺序不影响；少一个 MCP 工具就整段重写。hub 上 Persona 的工具定义来自提示词文件（部署时改了就变）
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

### 手动压缩（2026-11-04，#1727）

用户在主对话的菜单里点「压缩上下文」：`POST /api/v1/assistant/conversations/{id}/compact` → node 的 `assistant.compact`（`node/assistant_compact.go`）。

- 和空闲压缩同一条路：进程在就直接写一条 `/compact`，不在就先 `--resume` 起一个（启动器、模型按这段对话上次的）；Codex 是 `thread/compact/start`；runtime 跑的对话不支持。
  正在回答、已经在压的回 409，不排队：排在一轮后面的 `/compact` 用户看不到什么时候开始。
- 进度记在 node 的会话状态里（落 hub 的 Postgres），随会话列表下发 `compaction`：`running` → 看到 `compact_boundary`（Codex 的 `thread/compacted` / `contextCompaction`）是 `done`；
  这一轮的 `result` 先到、没有边界是 `failed`，错误是 Claude Code 的原话（对话太短时是「Not enough messages to compact」之类）；
  10 分钟没结果、进程退出、node 重启都算 `failed`（`reapAssistants` 每分钟查一次）。这一轮里自己触发的自动压缩不改它。
- 它不是一轮对话：会话状态不变成「工作中」、不推送、不取消归档；transcript 里 `/compact` 和边界照旧是「压缩上下文」「上下文已压缩」两条提示，
  各端把一次压缩画成一条分隔线「以上内容已压缩 · 手动 · 压缩前 N 万 token」。

### 新建主对话（2026-11-04，#1727）

主对话用久了上下文很大，用户可以换一段新的：`POST /api/v1/assistant/main/new`（助手的路由，`assistant/start.go`）。

1. 旧主对话正在回答或压缩时拒绝（409 `busy`）；App 带上它看到的主对话 id（`expect`），别的设备已经换过时拒绝（409 `main_changed`），不会把新的又换掉。
2. 带背景（`carry: summary`，默认）：node 的 `assistant.handoff` 从旧对话的 transcript 里取最近一次压缩的摘要（Claude 的 `isCompactSummary` 行、Codex 的 `compacted` 行，最多 1.2 万字）
   和之后的几条文字消息（最多 12 条、6000 字，去掉 `<jarvis-context>`），存进 settings `assistant.main_handoff`。
3. 清掉 `assistant.main`，旧 id 记进 `assistant.former_main`（最多 10 个，在 profile 的 `keep` 里，hub 让 node 一直列出它们，安静多久都找得到），
   改名「旧主对话 · 至 M月D日」（算用户起的，自动命名不覆盖），归档（都在助手里一步做完）。
4. 下一条发给主对话的消息开新的会话（App 的 `POST /api/v1/assistant/conversations` 带 `main:true`、飞书机器人和渠道插件发给主对话的（助手自己开，带 `main`），都走助手的 `startMain`），
   `assistant/main_handoff.md` 把带过去的背景写成 `<jarvis-context kind="context" title="接上旧主对话">` 放在第一条消息前（App 里是一个上下文标签），用完就清掉；
   没带背景（`carry: none`）也写一句旧主对话的 id，助手要翻可以用 `conversation_read`。
   安卓开新主对话是先用空的 `prompt` 建、再单独发消息：这时背景记上新会话的 id（`for`），等发给它的第一条消息（`POST …/messages`，或飞书、渠道发来的，都经 `assistant.send_params`）再带上，不单独成一轮。

为什么这样定：

- **不当场开新会话**：没有用户的第一句话就得替用户编一句，新主对话第一条就成了机器写的。延到第一条消息，和「还没有主对话」的流程一样，各端本来就支持。
- **带压缩摘要，不带知识库**：人、群、项目、记忆、实时快照每个助手对话本来就有（CLAUDE.md、工具、每条消息的快照），再带一遍是重复；
  换对话会丢的是这段对话里的结论和还在进行的事，正是压缩摘要。没压缩过的只带最近几条，确认框里提示可以先压缩再新建。
- **旧的不删**：归档只是不在列表里，用户再给它发消息就自动回到列表（`unarchiveOnSend`）。

iOS / macOS（#1762）：主对话顶栏头像的菜单（Mac 同一个菜单，侧栏主对话那行的右键菜单也有）加「压缩上下文」「新建主对话…」，hub 的 features 带 `assistant_compact` / `assistant_main_new` 才显示。
输入框上方一条状态（`MainConversationActionsView.swift`）：压缩中「正在压缩上下文…」，`done` 显示 4 秒「已压缩上下文」，`failed`「压缩失败：…」可以关掉；
`done` / `failed` 只在这个页面看着它从 `running` 变过来、或刚结束（1 分钟 / 10 分钟内）时显示，打开页面不会提示很久以前的结果（判断在 `MainConversationActions.shown`，和网页一致）。
新建前的确认是一个表单 sheet，「带上旧对话的摘要」默认开；成功后回到空的主对话页，第一条消息照「还没有主对话」的流程开新的。分隔线文案和网页、安卓统一。

## 定时任务（2026-10-31 合成一张表）

以前是两套：定时任务（routines，每天几点）和跟进（follow-ups，助手用 `schedule_followup` 安排的一次性唤醒），分存在两个 settings 里，
App 的「定时任务」页只列前者的开关。用户让助手「每 30 分钟巡检一次」，它只能在每一轮结尾再 `schedule_followup` 下一次接力：
一轮开始时那条跟进就从列表里拿掉了，这一轮卡在 Bash 审批上（没人点）的一个多小时里，页面上什么都没有，没法看、改、停。

现在只有一张表 `Task`（`assistant/schedule.go`，settings `assistant.tasks`），`repeat` 三种：

- `once`：一次性跟进，跑完关掉，再留一天（App 不显示）好让「它自己再安排下一次」接得上，然后清掉；
- `daily`：每天 / 每周几的几点，错过 30 分钟以上跳过并记录；
- `interval`：每 N 分钟（5 分钟到 7 天），`at` 是下一次，跑的时候按原节奏往后挪；停机错过了就回来跑一次，再从现在起算。

**失败和重试**：node 在模型回答前就接受 `assistant.send`，所以一轮以 API 错误收场（额度、模型没了、上游 503）的运行以前一直算「成功」。现在
`watchScheduledRun` 盯着这一轮，结束后读最后一句话，是 API 错误就把运行记成失败、推送「定时任务失败：…」，3 分钟后再跑一次（只一次，
`Retried`，重试再失败不再推）。重试按**那时**表里的任务来，不用启动时的那份副本（#2313 前：失败推送把用户带到任务页，顺手关掉或删掉，
3 分钟后它照样用旧提示词再跑一遍，而且不出声）：任务删了不重试；重复任务关了不重试；一次性跟进到点就已经是关的，照样重试；提示词改了用新的。

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
- App：定时任务页「对话」一节写「在同一个对话里运行」，点开就是这段对话（旧布局和新外壳都回到 Persona 的对话上）；「下次开新对话」
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
- **存在哪**：助手的 store（就是 hub 的 settings `assistant.titles`：会话 id → 标题、谁起的、开始时间），经快照交给 hub，发给 App 的会话列表里替换 `title`（`decorated`）。
  不写进 transcript：Claude 和 Codex 两种对话一样处理，不用动 node。
- **只起一次，用户的优先**：App 里 重命名（抽屉里长按对话 › 重命名…，`POST /api/v1/sessions/{mid}/{sid}/rename`）记成用户起的，以后不再覆盖；
  第一轮结束前就改了名的也不会被替换。Codex 对话改名只记在 hub（node 会往 Codex 的 rollout 里写 Claude 的 custom-title 行）。
- 已有的旧对话不补（没有「待命名」那一笔）。
- **主对话没有标题**（#1313）：发给 App 的会话列表里主对话的 `title` 一律是「Persona」（`mainTitle`，按 settings `assistant.main` 认），
  不是 node 给的（无头跑的 transcript 没有 Claude Code 的 ai-title，node 拿第一条消息当标题）、不起「MMDD | 类型 | 主题」（开主对话时记的「待命名」在第一轮结束时直接丢掉，不调模型），
  App 给它改标题返回 400「主对话没有标题可以改」（`conversation_rename` 本来就拒绝）。各端顶栏在主对话上显示形象：iOS / Mac / 安卓是形象名（默认「Persona」），网页是「Persona」。
- App：抽屉的对话行里 `MMDD | 类型 | ` 用次要色、主题用主色，窄的时候从主题末尾截断（`ConversationTitle`）；别的标题照旧。

## 管理自己的对话（2026-11-01）

用户要求：「jarvis 助手是否支持操作助手本身的会话历史，比如 rename title，delete（archive），new，send message to session/thread」。
助手的 MCP 工具（`assistant/conversations.go`，描述在 `prompts/mcp/assistant/conversation*.md`），Claude 和 Codex 两种对话一样：

| 工具 | 做什么 |
| --- | --- |
| `conversations_list` | 对话列表（侧边栏那些）：id、标题、最后活动时间、是不是主对话 / 这段对话 / 在跑 / Codex、最后一句话；`query` 按标题和最后一句过滤；默认不含已归档的（`include_archived`） |
| `conversation_read` | 读另一段对话最近 N 轮（默认 2，最多 10），和 `agent_read` 同一套截断（`transcriptText`：工具调用一行、只列失败的结果、整体最后 4 万字节） |
| `conversation_rename` | 改标题：给 `kind` + `topic`，hub 拼成「MMDD \| 类型 \| 主题」（MMDD 是对话开始那天，上一节）。用户起的标题不动，除非用户明说要改（`user_asked`，改完算用户的）；助手起的记成 `assistant`，自动命名不再覆盖，用户在 App 里改名照样优先。主对话没有标题可改 |
| `conversation_archive` / `conversation_unarchive` | 归档 / 取消归档（下面） |
| `conversation_new` | 带一条开场消息新开一段对话，立刻返回 id，不等它跑完；第一轮结束后照常自动起标题（开场消息里的来源说明不算） |
| `conversation_send` | 给另一段对话发消息；不能发给自己 |

- **归档只是不显示**：hub 的 settings `assistant.archived`（会话 id → 机器、时间、谁归档的），会话列表里带 `archived: true`；
  `GET /api/v1/assistant` 默认不列，`?archived=1` 才列。不删 transcript，不做硬删除。主对话不能归档（接口 400，工具报错）；要换掉它用「新建主对话」（上面「压缩」一节）。
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
| Persona MCP 工具 | `--mcp-config` + `--allowedTools mcp__jarvis` | `-c mcp_servers.jarvis.*`；Codex 用 `mcpServer/elicitation/request`（`codex_approval_kind: mcp_tool_call`）问能不能调，node 对兼容 MCP server `jarvis` 一律同意；`tool_timeout_sec` 给到一天（默认 60 秒，ask_user 等手机会超时） |
| ask_user | 阻塞的 MCP 工具 | 同一个工具。MCP 进程启动时 thread 还没有 id，环境变量里给的是一个 key（`proc-…`），node 收到调用时换成会话 id（`resolveAssistantKey`） |
| Codex 自己的提问（request_user_input） | — | `item/tool/requestUserInput` → 手机上的提问卡片，答案按题目 id 回去 |
| 命令 / 改文件的审批 | PermissionRequest hook → 手机 | app-server 发 `item/commandExecution/requestApproval` / `item/fileChange/requestApproval` / `item/permissions/requestApproval` 给 node，node 走同一个手机确认（`ask`） |
| 飞书写操作的确认 | Bash PreToolUse hook（`larkgate.go`） | 命令审批里先过同一个闸门（`larkCheck`，去掉 `/bin/zsh -lc '…'` 外壳再读）：读放行、写弹「飞书」卡片、碰 token 拒绝。Codex 的 decline 不带理由，拒绝的理由（手机上写的话、闸门的说明）用 `turn/steer` 交给模型，对话里会显示成一条「（没有执行）…」 |
| 权限模式 | `--permission-mode` / 控制请求 | 每一轮 `turn/start` 带上（`codexPolicy`）：默认 / 自动接受编辑 = `untrusted` + workspace-write（开网络）；自动 = `on-request` + workspace-write（不开网络，出沙箱、上网要升级，升级请求过闸门）；计划 = `untrusted` + read-only；全部放行 = `untrusted` + danger-full-access、node 一律同意（**不用 Codex 的 never**：never 下命令不经过 node，飞书闸门就没了）。模式切换下一轮生效，审批的同意与否按当时的模式；rollout 里记的审批策略 + 沙箱能反推回模式，node 重启后不丢 |
| 思考强度 | `--effort` / 控制请求 | `turn/start` 的 `effort`（原样传 low…max，网关认不认看模型） |
| 图片 | base64 image 块 | 存成文件（`~/.persona/uploads/日期/`），`localImage`；rollout 记路径，App 当缩略图显示 |
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

和 Claude、Codex 一样的：实时快照、`ask_user` 和其他 Persona 工具（只给助手对话）、授权和提问一律先到手机、权限模式、共用的记忆
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

- Persona tab 右上角是 Persona 的头像，点开是资料页（`AssistantProfileView`）：身份和运行在哪台机器、在线 / 工作中、「接下来」要做的 3 件事，
  主动做事（定时任务、运行记录）、了解你（关于我、记忆）、设置（Skills、运行在、工作目录；模型在「设置 › 模型配置 › 主对话」，#1291）、能访问。设置 › 助手打开的是同一页。
- 定时任务页：「接下来」时间线（左边大号时刻，像「提醒事项」）、「定时任务」（每天 / 每隔，规则的人话「每个工作日 上午 9:00」「每 30 分钟」）、
  「一次性跟进」，每行图标、下次、不是默认时的运行方式（「claude-fast · 自动 · 思考 低」，全部放行标红）、上次结果、开关，左滑删除、长按立即运行；
  最近运行。点开是同一个编辑页：重复（一次 / 每天 / 每隔）、提示词、运行方式（模型、权限模式、思考强度）、接着原来的对话（能关，关了每次新开）、
  立即运行、历史记录。
- 头像是 SwiftUI 画的（`AssistantAvatar`），不用位图。2026-11-03 起默认是**团子**（App 图标里招手的白色团子，照图标的配色画，不跟主题色，见 `docs/persona.md`「内置形象」）；
  下面几个跟着主题色的强调色走。之前试过四个方案（`-tab assistant -openAssistantPage avatars` 看对比）：
  **面罩**（2026-11-03 之前的默认，现在是可选项：强调色圆盘 + 深色面罩里两只发光的眼睛，右上一颗火花呼应 App 图标；工作中眼睛左右看）、
  **汇流**（2026-10-31 用户从科技感图标候选里挑的 B2，11-01 定为形象里的一个可选项、不当默认：强调色深底圆里，左边三个焊盘的走线
  汇进一个带两只眼的核心，意思是各台电脑上的 Agent 汇到 Persona；工作中信号沿走线流进核心）、
  火花（App 图标的火花放进深色圆里，和 App 图标太像，小尺寸时像个按钮）、光环（HUD 式断开的光环 + 发光的核心，酷但冷，28pt 下细节糊成一团）。
  没放进消息旁边（助手的回复是整宽的正文，加头像要让出一列）和通知里（要做 Notification Service Extension + communication notification，另做）。
- 2026-10-31 起头像和名字可以换成别的形象（设置 › 外观 › 形象，`docs/persona.md`；形象带的配色在主题色里选「跟随形象」才用）。面罩是默认形象，汇流在形象里紧跟其后，可以选。两个都在顶栏按状态动：
  空闲时眨眼，思考中抬眼加思考气泡，工作中面罩的眼睛左右看、汇流的信号沿三条走线流进核心，等你处理时外圈红色呼吸，离线时变灰。

## Android 的入口（2026-11-02）

Android 的管理都在「设置 › 助手」（`docs/android.md`「助手设置」）：设置列表里的「助手 › Persona」，Persona tab 标题菜单的「助手设置」、右上角头像、抽屉的「资料页」也打开它。内容：在哪运行（机器、工作目录，可以换机器；启动器 / 模型在「模型配置 › 主对话」，#1291）、数据存在哪里、了解你（关于我、记忆）、时区（默认跟随手机）、能访问；主动做事（定时任务、跟进、运行记录）另做，接好后放进这一页。

和 iOS 不同，Android 能换助手所在的机器。换的时候提示「记忆和会话记录在原来那台机器上，不会跟过去」，并在同一个 `PUT /api/v1/assistant` 里带 `"main":"-"` 清掉主对话：hub 换机器时不动 `main`，而主对话的 transcript 在原来那台机器上，不清的话 App 会把 `main` 拼到新机器上，去找一个不存在的会话。

## 验证

`docs/testing.md` 的服务端测试覆盖 `nextRun` / `parseWhen` / 跳过过期任务 / ask_user 往返 / agents_list / 工具只给助手；
2026-10-30 加了 hub 重启前后的调度（`TestScheduleAcrossHubRestart`：不重跑、错过的跟进晚跑一次并说明、错过太久的定时任务跳过、历史保留）、
`schedule_routine`、有后台任务的进程不回收、重启后告诉对话丢了哪些任务；App 单测 `AssistantScheduleTests`，UI 测试 `AssistantProfileTests`
（头像进资料页、定时任务列表和详情、定时任务和跟进的立即运行）。2026-10-31 合表后：`assistant/schedule_test.go`（迁移不丢不重跑、
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
