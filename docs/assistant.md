# 助手（「Jarvis」tab，原「对话」）

「Jarvis」tab 的 Jarvis 是 hub 所在机器上 `~/.jarvis/assistant` 里**无头运行**的 Claude Code（`claude -p` + stream-json，见
`docs/claude-integration.md`「无头助手」）。这篇讲它能做什么、怎么做到的，以及 2026-10-26 那次能力升级背后的取舍。

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
| 自己定时回来 | `schedule_followup` / `list_followups` / `cancel_followup`；到点 hub 把 prompt（以「⏰」开头）发进原对话（`assistant.send` 会 `--resume`），失败就新开一段 |
| 定时任务 | hub 的 routines（Postgres settings `assistant.routines`），默认「早报」工作日 09:00 开、「晚间回顾」工作日 21:30 关；每次跑是一段新对话，跑完照常推「完成」。对话里说「每天 / 每周几 X 点…」时助手用 `schedule_routine` 建（App 里标「Jarvis 建的」），`delete_routine` 删 |
| 运行记录 | 每次运行（成功开起对话 / 没跑起来 / 停机错过而跳过）记进 settings `assistant.runs`（最近 200 条），`GET /api/v1/assistant/runs[?ref=]`；定时任务带上次状态 `last_status` |
| 到点 | 调度按下一个到期时间设定时器（最长睡 30 秒，列表一改就重算），不再是 20 秒轮询；hub 停机期间错过的跟进回来后照跑一次，提示词里写明晚了多久 |
| 后台任务 / Monitor | 助手可以用 Bash 的 `run_in_background` 和 Monitor 盯事件，然后结束这一轮；命令跑完 / 事件来时 claude 自己开新一轮（node 把它当成工作中，跑完照常推送）。node 从 stream-json 的 `system/background_tasks_changed` 知道还有哪些在跑：有任务时不回收进程（最长 12 小时），任务列表记在 `node_sessions.tasks`；jarvisd 重启（部署）会把它们连同进程一起杀掉，node 起来后给这些对话发一条说明，让助手决定要不要重开 |
| 不用批准的工具 | 助手进程带 `--allowedTools mcp__jarvis`：Jarvis 自己的工具（notify_user、schedule_followup、agents_list、kb_*…）不再每次在手机上要批准；会替用户出面的（lark_send、mail_send、手机写操作）hub 自己先问。Bash / Monitor 仍按会话的权限模式 |
| Agents | `agents_list` / `agent_read` / `agent_send` / `agent_start`：直接用 hub 的会话状态（`decorated`）和 node 的 `session.items` / `session.send` / `session.start`，不走 ssh |
| 飞书 | `lark_chats` / `lark_messages` / `lark_search` / `lark_api`（只读，规则同 `/api/v1/lark/raw`）/ `lark_send`（先在手机上批准）：hub 的 lark-connector 客户端，token 不出 hub |
| 实时快照 | node 启动 claude 进程前调 hub 的 `assistant.context`，拼进 `--append-system-prompt`：时间（手机时区）、在线电脑、需要你 / 完成未看 / 工作中的 Agent、飞书未读和 @我、今天接下来的飞书会议、已安排的跟进。每个来源 3 秒超时，结果缓存 45 秒 |
| 更快 | App 显示新对话页时 `POST /api/v1/assistant/warm`，node 先起一个备用进程（`spare`，10 分钟内有效）；新对话直接用它。助手进程带 `ENABLE_TOOL_SEARCH=false`，jarvis 工具不再延迟加载 |
| 记忆 | 就是 Claude Code 的 auto memory（`~/.claude/projects/<编码后的目录>/memory/*.md`）；App 设置里能看、改、删（node `assistant.memory`） |
| 关于我 | App 里编辑后存 settings `assistant.notes`，覆盖 hub.json 的 `assistant.notes`，进 CLAUDE.md 末尾 |
| 手机不在前台 | `phone_*` 调用时没有手机连着 → hub 发静默推送（`content-available`，`jarvis_wake`）→ App 在后台连上 WS 执行，25 秒后断开；等最多 20 秒。App 进后台时主动断开 WS，否则挂起的 socket 让 hub 以为手机在线、调用干等 2 分钟 |

只有助手能拿到这些工具：node 给助手进程的 MCP 配置是内联 JSON，带 `JARVIS_ASSISTANT=1`、`JARVIS_SESSION_ID=<对话 id>`；
`jarvisd mcp` 把这两个值带给 hub，hub 只在 assistant 为真时列出、执行这些工具（`hub/assistant_tools.go`）。herdr 里的普通 Agent 看不到。

## 主对话、知识库和上下文流动（2026-10-30）

- **主对话**：第一段对话是主对话（hub settings `assistant.main`），Jarvis tab 默认打开它；快照开头写明「这是主对话」，提示词要求接着之前聊的往下做。
  其他页面交给 Jarvis 的东西（Agents 长按「交给 Jarvis」、知识库「问 Jarvis」）都以 `<jarvis-context>` 发进主对话。
- **知识库**（`docs/knowledge.md`）：工具 `kb_search` / `kb_list` / `kb_get` / `kb_update`；快照带重点项目和会话；提示词要求把对话里得出的结论写回
  （项目进展写时间线、项目代码目录写 `dirs` 让 Agents 对上项目、给 Agent 派活前先读项目档案）。
- **AI 分身**：处理一件事时提示词附上这个会话和它前 3 个项目的档案，处理完把新结论写回项目。
- 助手和 AI 分身的默认启动器在 hub.json `assistant.launcher` / `inbox.agent.launcher`（这台机器的具体值见 `CLAUDE.local.md`）。

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

## 资料页和头像（2026-10-30）

- Jarvis tab 右上角是 Jarvis 的头像，点开是资料页（`AssistantProfileView`）：身份和运行在哪台机器、在线 / 工作中、「接下来」要做的 3 件事，
  主动做事（定时任务、已安排的跟进、运行记录）、了解你（关于我、记忆）、设置（启动器）、能访问。设置 › 助手打开的是同一页。
- 定时任务页把定时任务和跟进放在一起：「接下来」时间线（左边大号时刻，像「提醒事项」）、每个定时任务一行（图标、规则的人话「每个工作日 上午 9:00」、
  下次、上次结果、开关）、跟进（左滑取消、右滑或长按立即运行）、最近运行；定时任务详情多了历史记录。
- 头像是 SwiftUI 画的（`JarvisAvatar`），颜色跟着界面风格的强调色走，不用位图。试过三个方案（`-tab assistant -openAssistantPage avatars` 看对比）：
  **面罩**（选用：强调色圆盘 + 深色面罩里两只发光的眼睛，右上一颗火花呼应 App 图标；工作中眼睛左右看）、
  火花（App 图标的火花放进深色圆里，和 App 图标太像，小尺寸时像个按钮）、光环（HUD 式断开的光环 + 发光的核心，酷但冷，28pt 下细节糊成一团）。
  没放进消息旁边（助手的回复是整宽的正文，加头像要让出一列）和通知里（要做 Notification Service Extension + communication notification，另做）。

## 验证

`docs/testing.md` 的服务端测试覆盖 `nextRun` / `parseWhen` / 跳过过期任务 / ask_user 往返 / agents_list / 工具只给助手；
2026-10-30 加了 hub 重启前后的调度（`TestScheduleAcrossHubRestart`：不重跑、错过的跟进晚跑一次并说明、错过太久的定时任务跳过、历史保留）、
`schedule_routine`、有后台任务的进程不回收、重启后告诉对话丢了哪些任务；App 单测 `AssistantScheduleTests`，UI 测试 `AssistantProfileTests`
（头像进资料页、定时任务列表和详情、定时任务和跟进的立即运行）。

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

延迟的大头是模型本身（收到事件到调 notify_user 10–19 秒）；hub / node 这一侧在 1 秒内。部署后复测的数字补在下面。

端到端（2026-10-26）：新对话 10 秒内用快照 + `agents_list` + `lark_api calendar/week` 回答「谁在等我、今天还有什么会」；
ask_user 在 App 里显示为原生提问卡片并拿到答案；`schedule_followup 2m` 准点触发并 `notify_user`。
