# 在 herdr 里驱动 Claude Code

结论均在 Claude Code 2.1.310（mac）/ 2.1.308（Linux 的 hub 机器）、herdr 0.8.0 上实测（2026-10-24）。

## 启动会话

node 的 `session.start`：

1. 找 label 等于目录名的 herdr workspace（用户习惯一个项目一个 workspace），有就在里面 `tab create --label jarvis`，没有就
   `workspace create --label <目录名>`。都带 `--cwd <目录>`、`--no-focus`，不抢电脑上的焦点。
2. `pane run <pane> "<启动器> --session-id <uuid> --mcp-config ~/.persona/mcp.json --plugin-dir ~/.persona/agent-plugin --plugin-dir ~/.persona/claude-hooks"`
   （本机 Claude Code ≥ 2.1；更老的或读不到版本时是 `<启动器> --session-id <uuid> --settings ~/.persona/claude-settings.json --mcp-config ~/.persona/mcp.json`，
   见下「Hooks」）。启动器是 `~/.zshrc` 里的函数（`cc_ds`、`cc_max`、`ccc`……），它们最后都 `claude … "$@"`，所以参数能透传。恢复会话用 `--resume <id>`。
3. `waitReady`：每 500ms 读屏幕（`pane read --source visible`）+ `agent get`：
   - 屏幕上有信任目录对话框（"trust this folder"）→ 按光标位置算 `down`/`up` + `enter` 选「Yes, I trust」。
     **herdr 把这个对话框报成 idle**，所以不能只看 agent 状态；之前直接发首条消息，文字被当成对话框输入，选中了 "No, exit"，claude 直接退出。
   - 状态 idle/done 且屏幕出现输入框页脚（`shift+tab` / `? for shortcuts` / `bypass permissions` / `esc to interrupt`），连续两次 → 就绪。
   - 其他 blocked（模型选择等）→ 直接返回，hub 会把屏幕作为 `screen` 交互推给手机。
4. 记录 pane → session，`agent rename j-<id 前 6 位>`，发首条消息（`agent prompt`）。

Persona 启动的会话，启动器记在 node 状态里。在电脑上手动用启动器函数开的 claude 没有这条记录，启动器靠推断
（`server/internal/node/sessions.go` 的 `matchLauncher`）：node 每 30 秒扫描一次运行中 claude 的环境变量（同一次扫描还读 `CLAUDE_CONFIG_DIR`，
见 `claudedirs.go`），记下 `ANTHROPIC_BASE_URL` / `ANTHROPIC_MODEL`，再用 `sessions/<pid>.json` 对到会话。匹配启动器列表（`_claude_launchers` 的函数和调用 claude 的 alias）：
base URL 和模型都相同的启动器只有一个就是它；否则如果这个 base URL 只有一个启动器（模型是运行时选的、显示成 `(claude-choose)` 的那种）也算它；
其余情况（多个启动器共用 base URL 又对不上模型、没设 base URL）留空，不在候选之间猜。列会话不等启动器列表（它要跑登录 shell）：用缓存，过期就后台刷新，
所以 node 刚启动时第一次列表可能还没有推断出的启动器。

## 会话 id 与 transcript

- mac 装了 herdr 的 claude integration，`agent list` 带 `agent_session.value`（= Claude session id）。**hub 机器没装**，拿不到 id。
- 兜底顺序：herdr 上报 → 我们 hook 里记下的 `HERDR_PANE_ID`→`session_id`（hook 进程继承 pane 的环境变量，只有 Persona 启动的会话有）
  → **窗格里 claude 进程的 `~/.claude/sessions/<pid>.json`**（Claude Code 自己维护，含当前 `sessionId`，`/clear` 后会更新；
  pid 用 `herdr pane process-info --pane <id>` 拿，缓存一分钟）→ 标题匹配 AI 标题 → 按 cwd 推项目目录
  （cwd 里非字母数字都换成 `-`，如 `/Users/me/code/jarvis` → `-Users-me-code-jarvis`）里最新的 `.jsonl`。
- transcript：`~/.claude/projects/<编码后的 cwd>/<session id>.jsonl`，每个内容块完成就追加一行（thinking、text、tool_use 各自一行，
  tool_result 在下一条 user 行里）。不是逐 token，逐字效果靠屏幕采样。
- 需要跳过：`isSidechain`（子代理）、`isMeta`（系统注入的 user 消息）、`attachment` / `api-request*` / `mode` / `permission-mode`
  / `file-history-*` 等非对话行。`ai-title` 行给会话标题。`system` 里只要 `compact_boundary`、`api_error`、`informational`、`away_summary`、
  `local_command`（新版把斜杠命令和输出写成 system 行：`<command-name>` 是用户输入，`<local-command-stdout>` 去掉终端转义后当提示）、
  `scheduled_task_fire`（/loop 到点，算唤醒）。`api_error` 每次重试写一行（`retryAttempt` / `maxRetries`），原因在 `error.formatted`，
  只显示第一次和放弃时那次；`error` 在别的行里可能是字符串，解析时按原始 JSON 取。同一条 `<task-notification>` 常同时写成 user 行和
  queued_command，内容完全相同的只显示一次（同一任务内容不同的多次通知都是真事件）。

## 后台会话（/fork、转到后台）

2026-11-04 在 Claude Code 2.1.303 上实测（代码 `server/internal/node/claudebg.go`）：

- `/fork <话>` 起的会话、转到后台的会话不在终端里跑：`claude daemon` 起一个 claude 进程跑它，它的 `sessions/<pid>.json` 是
  `kind: "bg"`，`status` 是 `busy` / `idle` / `waiting`（等提问或对话框）。没有窗格，herdr 也收不到它的 SessionStart hook。
  transcript 照常在 `projects/<cwd>/<会话 id>.jsonl`（fork 把历史抄过来换成新 id，每行带 `sessionKind: "bg"`），子 Agent 在它旁边。以前它一直列成离线。
- node 把活着的后台会话（pid 还在、不是没认领的预热进程 `spare`、已经有 transcript）列成运行中：busy → working、idle → idle、
  waiting → blocked，其余 unknown。来源 `source_kind: "claude_bg"`，`capabilities` 只有 `read`，带 `read_only_reason`：App 能看（对话流式更新、
  子 Agent、后台任务照常），不能发消息、答卡片、看终端，hub 也不给它建屏幕卡片。万一发了，node 照样拒绝（跑它的 claude 开着这个会话，
  `open_elsewhere`），什么都不输入。Claude 自己的 peer socket 协议没有接。
- 把自己的会话转到后台的交互式 claude：`sessions/<pid>.json` 的 `sessionId` 不变、多一个 `parkedJobId`（原 transcript 末尾写
  `continued-in`），窗格里换成 Claude 的 agents 视图，回到会话时 `parkedJobId` 去掉。herdr 仍按 SessionStart 记下的 id 认这个窗格、
  按终端标题（agents 视图里打开的后台会话的）报状态，于是原会话显示「工作中」，手机发给它的消息会打进 agents 视图。node 和 Claude Code
  一样认为 parked 的进程不算开着这个会话：只有 parked 的进程开着的会话不占这个窗格，列成离线，claude 回到它后恢复。

## 屏蔽配置目录（#676）

有的机器上跑着自动化脚本，用单独的 `CLAUDE_CONFIG_DIR` 起 claude，每隔几分钟一条新会话。node 每 30 秒扫描运行中 claude 的环境变量，
会把这些目录找出来上报，hub 记进 `settings` 的 `claude_dirs/<机器 id>`，于是 Agents 里堆满离线的自动化会话；手动删记录没用（下次扫描又找回来），
免打扰（`/api/v1/mutes`）按空间 / 单个会话，管不到这种没有空间、数量不停增长的会话。所以按机器**屏蔽**配置目录：

- 规则存在 hub 的 `settings`：`claude_dirs_blocked/<机器 id>`，一个字符串数组。每条是路径或 `filepath.Match` 通配（`~/.automation/*`），
  必须以 `/` 或 `~/` 开头（`~` 是那台机器的家目录）；匹配的是目录本身或它的上级（屏蔽 `~/.automation` 也屏蔽它下面的）。匹配函数在
  `proto.ClaudeDirBlocked`，hub 和 node 共用。
- node（`node/claudedirs.go`）：`node.configure` 带 `claude_dirs_blocked`。`claudeConfigDirs()` 跳过匹配的目录（原路径和真实路径都比），
  所有按目录找东西的地方（离线会话列表、按 id 找 transcript、用量、记忆、设置页）都经过它，所以屏蔽的目录不会被读；扫描到的目录照样记在内存里（取消屏蔽立刻生效），
  但**不上报**；取消屏蔽时把之前压着的目录补报一次。node 自己的默认目录（`$CLAUDE_CONFIG_DIR` 或 `~/.claude`）永远不屏蔽：Persona 启动的会话在那里。
- hub（`hub/claudedirs.go`）：发给 node 的 `claude_dirs`（hub.json 配的 + 记住的）先去掉屏蔽的；`claude.dirs` 上报里屏蔽的不记（旧 node 照样会报）；
  改规则时删掉记录里被屏蔽的，再对在线的 node 重新 `node.configure` 并拉一次 `sessions.list`，App 随会话列表的广播更新。不在线就等下次连上时的 configure。
- 只是不显示，**不删文件**。取消屏蔽后会话按原来的规则（最近 7 天、最多 80 个离线会话）回到列表。
- 局限：herdr 窗格里正在跑的 claude 即使配置目录被屏蔽，窗格本身照样在列表里（那是你正在用的终端）；旧版 node 不认 `claude_dirs_blocked`，
  只能靠 hub 不记、不下发，扫描到的照样列出来，部署新 node 后才完整生效。知识库里已经归档的会话不动。
- 接口：`GET /api/v1/machines/{mid}/claude-dirs`（node 的 `claude.dirs.list`：每个目录、来源、会话数、正在运行的数量、被哪条规则屏蔽；
  机器不在线或 node 较旧时用 hub 的记录，`live: false`），`PUT /api/v1/machines/{mid}/claude-dirs/blocked {"blocked": [...]}`（整份替换，回新的状态）。
- 界面：设置 › Claude Code（iOS `ClaudeDirsSection`、网页 `settings/ClaudeDirs.tsx`）的「屏蔽的配置目录」：每个目录一行「屏蔽 / 取消屏蔽」，
  被通配屏蔽的行写「由规则 … 屏蔽」，取消就是删掉那条规则（它盖住的目录一起恢复）；下面可以手填规则。助手用 `jarvis_settings` 的
  `agents.blocked_claude_dirs`（get / add / remove，改之前手机确认）。
- 为什么不在 Agents 列表里对单个会话「屏蔽此目录」：会话没有带配置目录（要给 `proto.Session` 加字段、各端都改），而且要屏蔽的往往是整批
  会话的来源，在设置里按目录看得更清楚（会话数、是否在跑）。以后需要可以在会话菜单里加一个入口，复用同一个接口。

## Hooks（只注入 Persona 启动的会话）

node 启动时把同一组 hooks 写两份（命令是 node 二进制自身的 `hook` 模式，`node/hooksplugin.go`）：

- **插件** `~/.persona/claude-hooks/`（`.claude-plugin/plugin.json` + `hooks/hooks.json`，插件名 `persona-hooks`）：本机 Claude Code ≥ 2.1
  （和 `agent-plugin` 用同一个判断，`node/agentplugin.go`）时启动传 `--plugin-dir`，只在这次会话加载。`--plugin-dir` 可以传多个，和启动器自己的
  `--settings`、`--plugin-dir` 叠加；Claude Code 却只认最后一个 `--settings`，启动器（alias 或函数）自己的 `--settings` 里常有网关、Key 和模型，
  Persona 再传一个就把它顶掉，起来的 claude 要求登录（#1411）。所以这时 Persona 只给自己管理的启动器传 `--settings`（只有网关的环境变量，`docs/launchers.md`）。
- **settings 文件** `~/.persona/claude-settings.json`：给更老的 Claude Code（或读不到版本、插件没写出来时）用 `--settings` 传，alias 自带的合并进去。

2026-11-04 在 Claude Code 2.1.318 上实测，插件里的 hooks 和 `--settings` 里的一样：SessionStart、UserPromptSubmit、Stop 触发，PreToolUse 返回
`deny`、PermissionRequest 返回 `allow` 都生效（交互模式和 `-p` 都试过）。

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
- 插件和 `--settings` 里的 hooks 都只在启动时读，改了要重启 claude；hook 命令本身每次重新执行。
- hook 等待手机超过 30 分钟（`hook_timeout_minutes`）或连不上 node → 不输出，TUI 照常弹框；手机上也可以点「交给电脑上的终端处理」。
  等的过程中 node 没了（部署重启 hub / node）→ hook 重连下一个 node，手机上的卡片不丢（`docs/protocol.md`「交互挺过重启」）。
  hook 进程被杀（用户在电脑上 Esc）→ node 发现 socket 断开 → 通知 hub 撤销交互。

## MCP elicitation hooks

2026-10-31 在本机只读核对：`claude --version` 输出 **2.1.306（Claude Code）**，安装包的 `package.json` 版本同为 `2.1.306`。这是本次实际运行的版本，和本文其他章节此前记录的版本分开看。

本机原生可执行文件中包含 `Elicitation`、`ElicitationResult` 事件的输入／输出 schema、`executeElicitationHooks`／`executeElicitationResultHooks` 和 `parseElicitationHookOutput`。输入构造按 MCP 服务器名匹配 hook；`Elicitation` 在终端对话框前执行，`ElicitationResult` 在答案回传前执行。当前版本具备接入入口，无需升级后才开发；更早版本的支持下限未核实。

复核方法：运行 `claude --version`，解析 `command -v claude` 指向的安装目录，读取包元数据和原生可执行文件中的上述字符串。核对时未修改安装包、Claude 配置或真实会话，也未发起模型请求。以下格式来自本机包内的 schema 和输入构造，**尚未通过真实 MCP 服务器触发 hook 验证**。

`Elicitation` 的 stdin 格式：

```json
{
  "hook_event_name": "Elicitation",
  "session_id": "<session id>",
  "transcript_path": "<transcript path>",
  "cwd": "<project path>",
  "permission_mode": "default",
  "mcp_server_name": "example-mcp",
  "message": "请选择",
  "mode": "form",
  "requested_schema": {
    "type": "object",
    "properties": {"choice": {"type": "string", "enum": ["yes", "no"]}},
    "required": ["choice"]
  }
}
```

`mode` 可省略，省略按 `form` 处理。URL 模式用 `"mode":"url"`、`"url":"https://example.com/confirm"` 和 `"elicitation_id":"request_example"` 代替 `requested_schema`。注意：MCP 请求中的 `requestedSchema`／`elicitationId` 到 hook 输入后变为 `requested_schema`／`elicitation_id`。公共字段还可包含 `agent_id`、`agent_type` 和 `prompt_id`。

hook 以退出码 0 向 stdout 输出：

```json
{"hookSpecificOutput":{"hookEventName":"Elicitation","action":"accept","content":{"choice":"yes"}}}
```

拒绝、取消分别用 `"action":"decline"`、`"action":"cancel"`，不带 `content`。URL 模式接受也不带 `content`。`ElicitationResult` 输入包含 `mcp_server_name`、可选 `elicitation_id`／`mode`、`action`、可选 `content`，输出使用同名 `hookEventName`；Persona 只注册前置 `Elicitation`，避免把已作答的请求再问一遍。Claude 对特定服务器可能限制 hook 代答，最终行为由 Claude 自身决定。

Persona 的接入约定：

- node 生成的普通会话和无头助手配置均注册 `Elicitation`，matcher 为 `*`。已运行的 Claude 需重启才能读到新注册项。
- node 将原始 schema／URL 放进 `Interaction.elicitation`，透传 MCP 服务器名和子 Agent 来源。`tool_name`／`tool_use_id` 已有可选字段继续保留；只有上游提供时才透传。本机此事件的输入构造**不提供工具名、工具调用 ID 或配置来源**，不能从最近一次调用或服务器名猜。其他工具权限 hook 的 `mcp_server:{name,source}` 可以提供配置来源。
- hub 只给 `client_hello.features` 包含 `elicitation` 的连接发新类型；握手前不发，首次声明后补发仍待处理的请求。没有兼容连接时立即返回 dismiss，hook 不输出，Claude 按原终端流程处理。
- 手机的 `accept`／`decline`／`cancel` 分别原样回传；`dismiss` 表示交回终端，与 MCP 的 `cancel` 不同。超时和 node／hub 中断沿用现有 hook 的终端兜底、重连和交互恢复逻辑。请求 ID 按 hook 进程的 attempt 区分，同一工具多次提问不会合成一个请求。
- Android 已声明能力并可接收两种模式；本工作树的 `ElicitationFields.kt` 仍只支持 form 提交，URL 的打开后继续门禁需由界面车道补齐，本车道未改 `feature/chat`。真实 MCP 往返、URL 授权完成和旧 iOS 的设备验收尚未进行。

## 多选提问的屏幕卡（2026-11-02，2.1.316 实测）

用户自己用启动器起的 claude（比如 profile 目录下的 `--settings <profile>/settings.json`）不加载 Persona 的 hooks（上一节只注入 Persona 启动的会话），
AskUserQuestion 就弹在终端里，手机上只有屏幕卡。以前屏幕卡不认多选，把 `[ ] 甲` 当成选项文字、按数字只是勾一下、也找不到提交，所以「提交不了」。
不替用户往他自己的配置目录里装 hooks（那是用户的文件）；改成屏幕卡认得多选、答得了。

多选的样子（`server/internal/screen/testdata/claude-2.1.316-*`，在隔离的 herdr 里录的，文字换成了占位符）：

```
←  ☒ 环境  ☐ 平台  ✔ Submit  →          标签栏：每个问题一个标签，☒ 答过；当前是哪个只有颜色能看出来
示例二：哪些平台要一起发？
❯ 1. [✔] 手机                            [ ] / [✔] 是勾选状态，不是文字
  2. [ ] 手表
  3. [ ] Type something                    自由回答：写了字就显示写的字并自动勾上，清空就取消勾
     Next                                 只有一个问题或最后一个问题时是 Submit
──────
  4. Chat about this
```

- 数字键（或 enter）只勾 / 取消勾，光标不动；**光标在自由回答那一行时按数字会被当成文字写进去，在 Submit 行上按数字没反应**（要先挪回选项上）。
- ↓ 从最后一个选项到 Submit 行，再往下是 Chat about this；在 Submit 行按 enter（或按 →）进确认页「Review your answers」：
  每题的回答、「Ready to submit your answers?」、`1. Submit answers` / `2. Cancel`；有题没答时多一行 `⚠ You have not answered all questions`。
- 回答是选项按原顺序用 `, ` 连起来，自由回答放最后。
- 几个键一次发过去会丢键（`send-keys 1 3` 只勾上一个）：一次一个键，按完等它画好。
- 多个问题里的单选题：没有勾选框，`Type something.` 带句号，按数字就选中并跳到下一题；回头看答过的题，选中的后面有 ` ✔`。
- 只有一个单选问题时没有标签栏，顶上是 ` ☐ 环境`；写了字的 `Type something.` 那行显示写的字，enter 就提交。

node 的答法（`node/dialogmulti.go`）：每一步重读屏幕，算下一步只按一个键——先把勾选和想要的不一样的选项按它的数字（光标在自由回答或 Submit 行上就先 ↑ 回到选项上），
再到自由回答那行清空（`ctrl+e` `ctrl+u`）、写字，最后挪到 Submit / Next 按 enter；确认页上所有题都答了就按「Submit answers」并确认确认页关了。
电脑上已经勾了的、光标停在中间或停在 Submit 上，都按当时的样子算。多个问题时单选题照旧按数字，答完最后一题出现的确认页也会提交。

## 发消息、中断、按键

- 发消息：`agent prompt <pane> <text>`（原子地粘贴 + 回车，尊重 bracketed paste）。claude 正在工作时发，会进入它的排队输入。
- 图片：存到 `~/.persona/uploads/<日期>/`，消息末尾附 `<attached_images>` 标签，每行一个路径（Claude 自己会用 Read 打开；包在标签里不和用户打的字混在一起，App 把它显示成文字上方的方形缩略图）。
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
--include-partial-messages --session-id|--resume <id> --plugin-dir ~/.persona/assistant-hooks --mcp-config …`（`node/assistant.go`，2026-10-25 实测；
hooks 和上面一样是插件，比 agent 的多一个 Bash 上的飞书闸门，更老的 Claude Code 改传 `--settings ~/.persona/assistant-settings.json`）：

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
  或空闲时的 user 消息里；`TaskStop` / `KillShell` 也算结束。
- 通知的几种形状（2.1.3xx 语料里约 10 万条）：`Background command "<description 或整条命令>" completed (exit code N)` / `failed with exit code N` /
  `was stopped`；Monitor 每个事件一条 `Monitor event: "<description>"`（事件内容在 `<event>`，只有 task-id）、结束时 `Monitor "<description>" stream ended`
  （带 tool-use-id、output-file，`<event>` 是最后几行）；子 Agent `Agent "<description>" finished` / `failed: …`（`<result>` 是它的结果）。
  Monitor 启动的 tool_result 是 `Monitor started (task <id>, timeout …ms)`，App 靠它把只带 task-id 的事件对回那次调用。
- **Claude Code 自己把 summary / event / result 里的 `>` `<` `&` 写成实体**（`cat &gt; f &lt;&lt;'EOF'`），不是我们转了两次：解析时 `html.UnescapeString`。
  其他 notice（`local-command-stdout` 等）语料里没有实体。没写 description 的后台命令，summary 里是整条命令（heredoc 几十行）：
  node 解析成 `Item.task`（`transcript/tasknote.go`），文字只留一行（「后台命令完成 · <命令第一行>」），整条命令放在 `task.detail`，App 点开再看。
  空闲时的那种（user 消息，前面有一行 `system/turn_duration` 收尾上一轮）带 `wake: true`。App 不在这里切开轮次，
  而是取唤醒前后各段收尾的话里最长的一段作答案，其余收进工作行 / 「后台任务完成」抽屉（`docs/ios.md`，2026-10-26）。
- 会话列表的快速元信息只读 transcript 尾部 384 KB，长轮次里几张截图（每张一行 base64，上 MB）就把开始那行挤出去了。所以 node 对每个
  transcript 单独做增量扫描（`bgScan`：第一次读全文件，之后只读新增部分，只解析含相关关键字、小于 64 KB 的行），结果放进
  `Session.background`。超过 24 小时的视为已经没了（claude 重启后台任务就没了）。

## /loop（2026-10-25 实测）

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
- 嵌套（2026-11-04 看 2.1 写的 meta）：子 Agent 再起的子 Agent 也在同一个 `subagents/` 目录，meta 多了 `parentAgentId`（起它的子 Agent 的 id，主会话起的没有这个字段）、
  `spawnDepth`（1 = 主会话起的）、`isFork`（`agentType: "fork"` 的为 true），还有 `launchedWithDefaults`、`requestNonInteractive`、`model`。
- node：会话 id `<session>@<agent id>` 打开子 Agent 的文件（`transcript.NewSubagentFile`，保留 sidechain 行），所以 items / watch / 分页全复用；
  `Session.subagents` 列出子 Agent（未 finished 且 20 分钟内有写入算运行中；完成超过一天的不列，每个单独判断：父的没列、子的照列）。后台任务列表里不再放 agent，只剩 shell。
  meta 只用 `agentType`、`description`、`toolUseId`、`requestShape`、`finished`、`parentAgentId`（→ `parent_id`）、`isFork`（→ `fork`）；层级由客户端按 `parent_id` 搭，不用 `spawnDepth`。

## 跨会话消息（2026-10-25 实测）

- 另一个 Claude 会话发来的消息：「Another Claude session sent a message:」+ `<cross-session-message from="uds:…" from-name="<名字>" from-mode="…">正文</cross-session-message>` + 一段英文说明。
  在空闲时是一条 user 消息；工作中进排队（queue-operation 的 content 是同样的文本），被吸收时是 `queued_command` 附件，`origin.kind:"peer"`、`origin.name`、`origin.body`。
- 解析成 user 条目并带 `from`（发送方名字），排队条里显示为「【来自 名字】正文」；手机上画成左侧的灰色卡片「来自 名字」，不当成用户自己的消息。


## transcript 里的特殊 XML（2026-10-26 扫了本机和 hub 机器）

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

## 建议的下一条消息（composer hint）

一轮结束后 Claude Code 可能在空输入框里用灰字（SGR 2）画一条建议的下一句：Tab 采用、Enter 直接发。它**不在 transcript 里**，只在屏幕上：
`pane read --format ansi` 里是 `❯` 后紧跟 `ESC[2m` 的文字（光标在时第一个字符是反显）。用户打的草稿不是 dim；新会话的 `Try "…"` 也是灰字，不算建议。

- 解析：`server/internal/screen` 的 `ClaudeInput(...).Suggestion`（多行按空格连起来），夹具 `claude-2.1.312-suggestion-171.*`。
- node（`node/suggestion.go`）：Claude 会话从 working / blocked 变成 idle / done 时，在 0 / 2 / 5 / 10 秒读可见屏（2.1.312 实测一般几秒内出现），
  读到就停、记在会话上（`proto.Session.Suggestion`）；有草稿就停；再 working、或者从 Persona 发消息 / 按键时清掉。不额外高频读屏。
- 一条简短的回答（「ok」）后不一定有建议；给出几个下一步的回答后基本都有（实测「add all three to the README」）。
