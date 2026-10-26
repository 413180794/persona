# 助手（「对话」tab）

「对话」tab 的 Jarvis 是 hub 所在机器上 `~/.jarvis/assistant` 里**无头运行**的 Claude Code（`claude -p` + stream-json，见
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
| 定时任务 | hub 的 routines（Postgres settings `assistant.routines`），默认「早报」工作日 09:00 开、「晚间回顾」工作日 21:30 关；每次跑是一段新对话，跑完照常推「完成」 |
| Agents | `agents_list` / `agent_read` / `agent_send` / `agent_start`：直接用 hub 的会话状态（`decorated`）和 node 的 `session.items` / `session.send` / `session.start`，不走 ssh |
| 飞书 | `lark_chats` / `lark_messages` / `lark_search` / `lark_api`（只读，规则同 `/api/v1/lark/raw`）/ `lark_send`（先在手机上批准）：hub 的 lark-connector 客户端，token 不出 hub |
| 实时快照 | node 启动 claude 进程前调 hub 的 `assistant.context`，拼进 `--append-system-prompt`：时间（手机时区）、在线电脑、需要你 / 完成未看 / 工作中的 Agent、飞书未读和 @我、今天接下来的飞书会议、已安排的跟进。每个来源 3 秒超时，结果缓存 45 秒 |
| 更快 | App 显示新对话页时 `POST /api/v1/assistant/warm`，node 先起一个备用进程（`spare`，10 分钟内有效）；新对话直接用它。助手进程带 `ENABLE_TOOL_SEARCH=false`，jarvis 工具不再延迟加载 |
| 记忆 | 就是 Claude Code 的 auto memory（`~/.claude/projects/<编码后的目录>/memory/*.md`）；App 设置里能看、改、删（node `assistant.memory`） |
| 关于我 | App 里编辑后存 settings `assistant.notes`，覆盖 hub.json 的 `assistant.notes`，进 CLAUDE.md 末尾 |
| 手机不在前台 | `phone_*` 调用时没有手机连着 → hub 发静默推送（`content-available`，`jarvis_wake`）→ App 在后台连上 WS 执行，25 秒后断开；等最多 20 秒。App 进后台时主动断开 WS，否则挂起的 socket 让 hub 以为手机在线、调用干等 2 分钟 |

只有助手能拿到这些工具：node 给助手进程的 MCP 配置是内联 JSON，带 `JARVIS_ASSISTANT=1`、`JARVIS_SESSION_ID=<对话 id>`；
`jarvisd mcp` 把这两个值带给 hub，hub 只在 assistant 为真时列出、执行这些工具（`hub/assistant_tools.go`）。herdr 里的普通 Agent 看不到。

## 坑

- 模型会把 ask_user 和别的工具**并行**调用（实测：ask_user 和 schedule_followup 同一毫秒发出），所以 ask_user 不能当「先确认再做」的唯一闸门；
  真正有副作用的工具（`lark_send`、`mail_send`）自己在 hub 里要批准。
- 定时任务错过太久不补跑：hub 停了一上午，不会中午才发早报（晚于计划 30 分钟就跳过，只记录）。
- 静默推送会被系统限流，强制退出的 App 收不到；唤醒失败时工具结果会告诉模型「请他打开 App」。
- 快照在进程启动时生成，进程最长空闲 20 分钟会被回收、下次 `--resume` 时重新生成；长对话里的快照会过时，提示词里写明了「之后的变化用工具查」。
- Go 的零时间 `0001-01-01T00:00:00Z` 会出现在 `last_run` / `next_run` 里，App 用 `Date.realDate` 当作「没有」。

## 验证

`docs/testing.md` 的服务端测试覆盖 `nextRun` / `parseWhen` / 跳过过期任务 / ask_user 往返 / agents_list / 工具只给助手。
端到端（2026-10-26）：新对话 10 秒内用快照 + `agents_list` + `lark_api calendar/week` 回答「谁在等我、今天还有什么会」；
ask_user 在 App 里显示为原生提问卡片并拿到答案；`schedule_followup 2m` 准点触发并 `notify_user`。
