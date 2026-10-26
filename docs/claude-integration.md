# 在 herdr 里驱动 Claude Code

结论均在 Claude Code 2.1.310（mac）/ 2.1.308（example-machine）、herdr 0.8.0 上实测（2026-10-24）。

## 启动会话

node 的 `session.start`：

1. 找 label 等于目录名的 herdr workspace（用户习惯一个项目一个 workspace），有就在里面 `tab create --label jarvis`，没有就
   `workspace create --label <目录名>`。都带 `--cwd <目录>`、`--no-focus`，不抢电脑上的焦点。
2. `pane run <pane> "<启动器> --session-id <uuid> --settings ~/.jarvis/claude-settings.json --mcp-config ~/.jarvis/mcp.json"`。
   启动器是 `~/.zshrc` 里的函数（`cc_ds`、`cc_max`、`ccc`……），它们最后都 `claude … "$@"`，所以参数能透传。恢复会话用 `--resume <id>`。
3. `waitReady`：每 500ms 读屏幕（`pane read --source visible`）+ `agent get`：
   - 屏幕上有信任目录对话框（"trust this folder"）→ 按光标位置算 `down`/`up` + `enter` 选「Yes, I trust」。
     **herdr 把这个对话框报成 idle**，所以不能只看 agent 状态；之前直接发首条消息，文字被当成对话框输入，选中了 "No, exit"，claude 直接退出。
   - 状态 idle/done 且屏幕出现输入框页脚（`shift+tab` / `? for shortcuts` / `bypass permissions` / `esc to interrupt`），连续两次 → 就绪。
   - 其他 blocked（模型选择等）→ 直接返回，hub 会把屏幕作为 `screen` 交互推给手机。
4. 记录 pane → session，`agent rename j-<id 前 6 位>`，发首条消息（`agent prompt`）。

## 会话 id 与 transcript

- mac 装了 herdr 的 claude integration，`agent list` 带 `agent_session.value`（= Claude session id）。**example-machine 没装**，拿不到 id。
- 兜底顺序：herdr 上报 → 我们 hook 里记下的 `HERDR_PANE_ID`→`session_id`（hook 进程继承 pane 的环境变量，只有 Jarvis 启动的会话有）
  → **窗格里 claude 进程的 `~/.claude/sessions/<pid>.json`**（Claude Code 自己维护，含当前 `sessionId`，`/clear` 后会更新；
  pid 用 `herdr pane process-info --pane <id>` 拿，缓存一分钟）→ 标题匹配 AI 标题 → 按 cwd 推项目目录
  （cwd 里非字母数字都换成 `-`，如 `/Users/me/code/jarvis` → `-Users-me-code-jarvis`）里最新的 `.jsonl`。
- transcript：`~/.claude/projects/<编码后的 cwd>/<session id>.jsonl`，每个内容块完成就追加一行（thinking、text、tool_use 各自一行，
  tool_result 在下一条 user 行里）。不是逐 token，逐字效果靠屏幕采样。
- 需要跳过：`isSidechain`（子代理）、`isMeta`（系统注入的 user 消息）、`attachment` / `api-request*` / `mode` / `permission-mode`
  / `file-history-*` 等非对话行。`ai-title` 行给会话标题。`system` 里只要 `compact_boundary`、`api_error`、`informational`、`away_summary`。

## Hooks（只注入 jarvis 启动的会话）

`~/.jarvis/claude-settings.json`（node 启动时生成，命令是 node 二进制自身的 `hook` 模式）：

| 事件 | matcher | 作用 |
| --- | --- | --- |
| PreToolUse | `AskUserQuestion` | 阻塞等手机回答，timeout 86400 |
| PermissionRequest | `*` | ExitPlanMode → 计划审批；其他 → 工具授权 |
| Notification / Stop / UserPromptSubmit / SessionStart | — | 只上报（记 pane）；「完成了」推送由 hub 按会话状态变化发，不靠 Stop |

实测：

- **AskUserQuestion**：PreToolUse 返回
  `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow","updatedInput":{...原 input, "answers":{"<问题原文>":"<label>"}}}}`
  → 模型收到 `Your questions have been answered: "Which color?"="Blue"`，TUI 不弹框。多选用 `", "` 连接 label；自定义文本直接放字符串。
- **ExitPlanMode 走 PermissionRequest，不走 PreToolUse**（在 bypass 模式下 PreToolUse matcher 写了也不触发）。
  - 批准：`decision: {behavior: "allow", updatedInput: <原 input>, updatedPermissions: [{type: "setMode", mode: "bypassPermissions", destination: "session"}]}`。
    **单纯 `{behavior: "allow"}` 不生效，TUI 仍弹「Would you like to proceed?」**。
  - 打回：`{behavior: "deny", message: "…修改意见"}`，message 作为 is_error 的 tool_result 交给模型，模型会改计划再提交。
- 用户的启动器都带 `--dangerously-skip-permissions`，普通工具不会触发 PermissionRequest；不带的会话每个工具调用都会到手机。
- `--settings` 里的 hooks 只在启动时读，改了要重启 claude；hook 命令本身每次重新执行。
- hook 等待手机超过 30 分钟（`hook_timeout_minutes`）或连不上 node → 不输出，TUI 照常弹框；手机上也可以点「交给电脑上的终端处理」。
  hook 进程被杀（用户在电脑上 Esc）→ node 发现 socket 断开 → 通知 hub 撤销交互。

## 发消息、中断、按键

- 发消息：`agent prompt <pane> <text>`（原子地粘贴 + 回车，尊重 bracketed paste）。claude 正在工作时发，会进入它的排队输入。
- 图片：存到 `~/.jarvis/uploads/<日期>/`，消息末尾附 `<attached_images>` 标签，每行一个路径（Claude 自己会用 Read 打开；包在标签里不和用户打的字混在一起，App 把它显示成文字上方的方形缩略图）。
- 中断：`pane send-keys <pane> esc`。
- 任意 TUI 对话框：手机的终端页 / screen 交互 → `pane send-keys`（`up`/`down`/`enter`/`esc`/`tab`/`shift+tab`/`ctrl+c`/数字）、
  `pane send-text`。

## 实时屏幕

订阅中的会话，agent working/blocked 时 node 每 400ms `pane read --source visible --lines 60`，内容变了才推。App 从屏幕里解析：
- spinner 行（`✻ Cogitating… (12s · ↓ 3.2k tokens · esc to interrupt)`）→ 实时状态条；
- 输入框上方的若干行 → 正在输出的内容预览；
- 编号选项（`❯ 1. Yes` / `2. No`）→ 对话框按钮。

## 无头助手（「对话」tab）

助手不进 herdr：node 直接起 `zsh -ic '<启动器> "$@"' -p --input-format stream-json --output-format stream-json --verbose
--include-partial-messages --session-id|--resume <id> --settings … --mcp-config …`（`node/assistant.go`，2026-10-25 实测）：

- 一个进程能通过 stdin 连续多轮（每行一个 `{"type":"user","message":{…}}`，图片直接放 base64 image block）。
- `stream_event` 里的 `content_block_delta/text_delta` 是逐字输出 → WS `delta`；`result` 表示这轮结束。结构化消息仍从 transcript 读。
- hooks 照常触发，但 **AskUserQuestion 在 -p 模式下不可用**（工具直接报 `No such tool available … nobody in this session can answer it`，
  PreToolUse hook 没机会触发）：助手用 MCP `ask_user` 代替（`docs/assistant.md`）。
- MCP 配置是每个进程一份内联 JSON（`--mcp-config '{…}'`），带 `JARVIS_ASSISTANT=1`、`JARVIS_SESSION_ID`；进程环境带
  `ENABLE_TOOL_SEARCH=false`（工具少且常用，不走 ToolSearch）；`--append-system-prompt` 是 hub 生成的实时快照。
- 备用进程（spare）：claude 起来后在 stdin 上等第一条消息，可以提前起；新对话直接用它的 session id。
- 换启动器 = 杀掉进程，用新启动器 `--resume` 接上；空闲 20 分钟的进程会被回收，下次发消息自动 `--resume`。
- 中断：-p 模式没有 stdin 中断，直接结束进程，下一条消息续上。
- 无头会话的 transcript 里第一条 user 内容是 block 数组；没有 ai-title 时用第一句话当标题。

## 后台任务（`transcript.go` 的 trackBackground，2026-10-25 实测）

- 开始：`Bash` 带 `run_in_background:true`，tool_result 是 `Command running in background with ID: <id>. Output is being written to: <path>.`；
  子代理（`Agent`）异步启动时结果是 `Async agent launched … agentId: <id>`；前台命令超时会变成 `…moved to the background (ID: <id>)`。
- 结束：`<task-notification><task-id>…</task-id><status>completed|failed|killed</status><summary>…</summary></task-notification>`，
  可能出现在 queue-operation（enqueue，**不是用户输入，不能进排队列表**）、`queued_command` 附件（commandMode=task-notification，中途吸收）
  或空闲时的 user 消息里；`TaskStop` / `KillShell` 也算结束。完成通知在对话里显示成一行提示（summary）。
  空闲时的那种（user 消息，前面有一行 `system/turn_duration` 收尾上一轮）带 `wake: true`。App 不在这里切开轮次，
  而是取唤醒前后各段收尾的话里最长的一段作答案，其余收进工作行 / 「后台任务完成」抽屉（`docs/ios.md`，2026-10-26）。
- 会话列表的快速元信息只读 transcript 尾部 384 KB，长轮次里几张截图（每张一行 base64，上 MB）就把开始那行挤出去了。所以 node 对每个
  transcript 单独做增量扫描（`bgScan`：第一次读全文件，之后只读新增部分，只解析含相关关键字、小于 64 KB 的行），结果放进
  `Session.background`。超过 24 小时的视为已经没了（claude 重启后台任务就没了）。

## /loop（2026-10-25 实测，example-machine app 的 loop-check / get-login）

- 动态循环：每轮结束 Claude 调 `ScheduleWakeup {delaySeconds, prompt:"/loop …", reason}`，结果是
  `Next wakeup scheduled for 11:57:00 (in 1859s)`；到点 harness 把 prompt 当成一条 `/loop …` 用户消息重新发进来。`{stop:true}` 结束。
- 固定间隔：`CronCreate {cron, prompt, recurring}` / `CronDelete`。`recurring:false` 或日、月都写死的表达式（`33 17 21 10 *`）是一次性提醒，不算循环。
- node 在后台任务的增量扫描里一起解析，`Session.loop` 给出提示词、间隔、下次唤醒时间；下次唤醒过了 15 分钟还没新的 ScheduleWakeup，视为循环已随 claude 退出。
- 停止：外部没法取消已经安排的唤醒，手机「停止循环」是给 Agent 发一条消息，让它调 `ScheduleWakeup(stop)` / `CronDelete`（`AppModel.stopLoop`）。
- 请求体（api-request 行）里也有这些工具名（工具定义），统计时只看 assistant 的 tool_use。

## 改名（2026-10-25 实测）

- `/rename <名字>`：Claude Code 往 transcript 写 `{"type":"custom-title","customTitle":"<名字>","sessionId":…}`，输入框上方分隔线显示名字，
  `claude --resume` 列表也用它。解析时 custom-title 优先于 ai-title（之后再来的 ai-title 不覆盖）。
- herdr：`herdr workspace rename <workspace_id> <label>`、`herdr tab rename <tab_id> <label>`。
- 手机改名走 hub `…/rename` 接口；空间名是置顶 / 屏蔽的 key，hub 改名时一并迁移。

## 子 Agent（2026-10-25 实测，2.1.310）

- 文件：`~/.claude/projects/<cwd>/<session>/subagents/agent-<id>.jsonl`（每行 `isSidechain:true`、带 `agentId`，格式同主 transcript）
  + `agent-<id>.meta.json`：`{agentType, description, toolUseId, requestShape:"background"|…, finished}`。
- node：会话 id `<session>@<agent id>` 打开子 Agent 的文件（`transcript.NewSubagentFile`，保留 sidechain 行），所以 items / watch / 分页全复用；
  `Session.subagents` 列出子 Agent（未 finished 且 20 分钟内有写入算运行中；完成超过一天的不列）。后台任务列表里不再放 agent，只剩 shell。

## 跨会话消息（2026-10-25 实测）

- 另一个 Claude 会话发来的消息：「Another Claude session sent a message:」+ `<cross-session-message from="uds:…" from-name="<名字>" from-mode="…">正文</cross-session-message>` + 一段英文说明。
  在空闲时是一条 user 消息；工作中进排队（queue-operation 的 content 是同样的文本），被吸收时是 `queued_command` 附件，`origin.kind:"peer"`、`origin.name`、`origin.body`。
- 解析成 user 条目并带 `from`（发送方名字），排队条里显示为「【来自 名字】正文」；手机上画成左侧的灰色卡片「来自 名字」，不当成用户自己的消息。


## transcript 里的特殊 XML（2026-10-26 扫了本机和 example-machine）

| 标签 | 处理 |
| --- | --- |
| `<command-name>` / `<command-message>` / `<command-args>` | 显示成「/命令 参数」 |
| `<local-command-stdout>` / `-stderr` | 提示行 |
| `<local-command-caveat>` | 给模型看的说明，去掉 |
| `<task-notification>` | 后台任务完成提示（`docs/ios.md` 唤醒） |
| `<cross-session-message from-name=…>` | 「来自另一个会话」的消息 |
| `<teammate-message teammate_id=…>` | Agent 团队里队友的汇报，按「来自 teammate_id」显示 |
| `<bash-input>` → `<bash-stdout>` / `<bash-stderr>` | 在 Claude Code 里 `!` 直接跑的命令：用户一条「! 命令」，输出是它下面的一个 Bash 工具 |
| `<system-reminder>` | 去掉 |
| `<attached_images>` | 手机发来的图片路径，App 显示成缩略图 |
| `<jarvis-context>` | App 自己的引用上下文，App 显示成引用卡片 |

后台任务唤醒的那一轮结束时不推「已完成」通知：node 解析时记下最近一轮是不是唤醒开始的（`Meta.LastTurnWake` → `Session.woke_by_task`），
hub 的 `notifyFinished` 看到就跳过；会话照样变成「完成未看」，只是不弹 iOS 通知（2026-10-26 用户要求）。
