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
- 兜底顺序：herdr 上报 → 我们 hook 里记下的 `HERDR_PANE_ID`→`session_id`（hook 进程继承 pane 的环境变量）→ 按 cwd 推项目目录
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
- hooks 照常触发（AskUserQuestion 照样到手机）。
- 换启动器 = 杀掉进程，用新启动器 `--resume` 接上；空闲 20 分钟的进程会被回收，下次发消息自动 `--resume`。
- 中断：-p 模式没有 stdin 中断，直接结束进程，下一条消息续上。
- 无头会话的 transcript 里第一条 user 内容是 block 数组；没有 ai-title 时用第一句话当标题。

## 后台任务（`transcript.go` 的 trackBackground，2026-10-25 实测）

- 开始：`Bash` 带 `run_in_background:true`，tool_result 是 `Command running in background with ID: <id>. Output is being written to: <path>.`；
  子代理（`Agent`）异步启动时结果是 `Async agent launched … agentId: <id>`；前台命令超时会变成 `…moved to the background (ID: <id>)`。
- 结束：`<task-notification><task-id>…</task-id><status>completed|failed|killed</status><summary>…</summary></task-notification>`，
  可能出现在 queue-operation（enqueue，**不是用户输入，不能进排队列表**）、`queued_command` 附件（commandMode=task-notification，中途吸收）
  或空闲时的 user 消息里；`TaskStop` / `KillShell` 也算结束。完成通知在对话里显示成一行提示（summary）。
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

