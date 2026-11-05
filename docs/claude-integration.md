# 在 herdr 里驱动 Claude Code

结论均在 Claude Code 2.1.310（mac）/ 2.1.308（Linux 的 hub 机器）、herdr 0.8.0 上实测（2026-10-24）。

## 启动会话

node 的 `session.start`：

1. 找 label 等于目录名的 herdr workspace（用户习惯一个项目一个 workspace），有就在里面 `tab create --label jarvis`，没有就
   `workspace create --label <目录名>`。都带 `--cwd <目录>`、`--no-focus`，不抢电脑上的焦点。
2. `pane run <pane> "<启动器> --session-id <uuid> --mcp-config ~/.persona/mcp.json --plugin-dir ~/.persona/agent-plugin --plugin-dir ~/.persona/claude-hooks"`
   （本机 Claude Code ≥ 2.1；更老的或读不到版本时是 `<启动器> --session-id <uuid> --settings ~/.persona/claude-settings.json --mcp-config ~/.persona/mcp.json`，
   见下「Hooks」）。启动器是 `~/.zshrc` 里的函数（`cc_ds`、`cc_max`、`ccc`……），它们最后都 `claude … "$@"`，所以参数能透传。恢复会话用 `--resume <id>`。
   给没在运行的会话发消息时先恢复它，用哪个启动器按这个顺序选（`node/sendsafe.go` 的 `resumeLauncher`，#411）：
   Persona 启动它时记下的启动器 → 按 transcript 里的网关地址和模型推断出的（`inferLauncher`，外部开的会话）→
   hub 带来的「新建 Agent」的启动器和模型（hub 只给没在运行、启动器不明的 Claude 会话带，`hub/send.go`）→ 裸 `claude`。
   前两种沿用它原来的模型。以前推断不出就用裸 `claude`，平时靠启动器登录的机器上会话只显示「Not logged in」，消息发不出去。
3. `waitReady`：每 500ms 读屏幕（`pane read --source visible`）+ `agent get`：
   - 屏幕上有信任目录对话框（"trust this folder"）→ 按光标位置算 `down`/`up` + `enter` 选「Yes, I trust」。
     **herdr 把这个对话框报成 idle**，所以不能只看 agent 状态；之前直接发首条消息，文字被当成对话框输入，选中了 "No, exit"，claude 直接退出。
   - 状态 idle/done 且屏幕出现输入框页脚（`shift+tab` / `? for shortcuts` / `bypass permissions` / `esc to interrupt`），连续两次 → 就绪。
   - 其他 blocked（模型选择等）→ 直接返回，hub 会把屏幕作为 `screen` 交互推给手机。
4. 记录 pane → session，`agent rename j-<id 前 6 位>`，发首条消息（`agent prompt`）。
5. 确认首条消息进了 transcript（`node/firstprompt.go`，#2423）：输入框出现时 Claude 不一定画完了（启动时自动更新的提示、Auto mode 的说明），
   这时打进去的字会被吞掉，Agent 一直空闲。所以发完要看到 transcript 里出现这条 user 消息才算发出去：过了 `submitGrace` 还停在输入框里就补一次回车；
   15 秒还没进（Claude 也没在工作）就重新 `waitReady`、确认输入框是空的再发，最多 3 次。还不行、开着对话框或者输入框里有别的东西，
   就把会话标成「需要你」（`blocked` + `attention_reason` 写明原因），不静默空闲；之后有消息进了 transcript（手机发的、电脑上打的）就恢复原样。
   确认在后台做，启动照旧马上回；这期间发给这个会话的消息排在后面（同一把打字锁），不会和重发的首条消息混在一起。
   这个标记只在 node 内存里，node 重启后会话按 herdr 的状态显示。
   没有改成把首条消息当 `claude` 的参数传：启动器可能是 zsh 函数 / alias / `jarvisd launch`，参数怎么传过去不统一，`--allowedTools` 等变长参数还会吞掉
   后面的位置参数，长消息还得打进 shell 命令行；校验 transcript 对所有启动器都成立。

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
- 最后两步是猜，对刚起的 claude 一定猜错：它几秒后才写出 `sessions/<pid>.json`（Windows 上四五秒），第一句话之前也没有自己的
  `.jsonl`，「目录里最新的」是上一个会话，新标签页于是先显示成旧会话（#1980）。所以 node 启动之后才出现的 claude 窗格有 20 秒宽限
  （`freshPaneGrace`）：这期间 pid 文件和窗格绑定都没有就不猜、先不列，pid 文件每次刷新都重看（平时没找到要隔 5 秒）；宽限过了照旧猜
  （不写 pid 文件的老版本 Claude Code）。node 启动时已经在的窗格不算新的，照旧马上猜。
- herdr 上报的 id 来自 claude integration 的 SessionStart hook，它按进程环境里的 `HERDR_PANE_ID` 记到窗格上。不在这个窗格里、却继承了
  这个变量的 claude 也会报进来：在 herdr 窗格里第一次起的 tmux 服务器（botmux 就是这样）把 `HERDR_*` 传给它之后起的每个 claude，
  herdr 的 main 标签于是显示成一个 botmux 会话，窗格里真正的 claude 列成离线（2026-11-04）。所以有终端里的 claude 跑着一个没有窗格认领的会话时，
  node 也读窗格里 claude 自己的 `sessions/<pid>.json`，和 herdr 报的不一样就以窗格里的为准（`setAgentsAt`、`unplaced`）。
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
  一样认为 parked 的进程不算开着这个会话：手机不往这个窗格打字，状态也不用 herdr 报的，claude 回到它后恢复。
- agents 视图会打开会话转去的那个后台 job（后台会话的 `sessions/<pid>.json` 带 `jobId`，等于 `parkedJobId`），这时窗格显示的是它的
  对话、输入框，终端标题是它的 `name`。窗格标题等于这个 job 的 `name` 时，node 把窗格认成这个后台会话：列在对应的 herdr 标签里，
  按窗格收发，而不是另列一条只读的 `claude_bg`（2026-11-04：herdr 有 4 个标签，手机上少一个）。
- 停在 agents 列表时（标题不是 job 的 `name`：job 已结束 / 停止，或选了别的），窗格仍列成原会话所在的标签：状态 idle，`capabilities`
  是 `read` + `terminal`、带 `read_only_reason`。手机在终端里像电脑上一样选（方向键、回车），进去后窗格就是那个会话的；
  消息不往列表里打，node 拒绝（`open_elsewhere`），什么都不输入——列表的输入框会新建一个后台会话。
  之前这种窗格哪儿都不列（2026-11-05：两个 job 结束后，手机上少了 tab 2 和 4）。

## 屏蔽配置目录（#676）

和自己的会话共用配置目录的平台会话（目录分不开）由接那个平台的扩展按会话里的标记过滤，见 `docs/extensions.md`「Agent 过滤」。

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
- **别的 PermissionRequest hook 先答了**（#2648）：Claude Code 同时跑所有 PermissionRequest hook，第一个 `allow` 就放行，其余的**不会被结束**，
  一直跑到各自的超时（2.1.308 实测）。Persona 的 hook 那时还在等手机，手机上就留着一张过期的卡。作答的 hook 可以告诉 node：往 node.sock
  （`~/.persona/node.sock`，`personad hook` 连的那个）写一行 `{"kind": "hook.decided", "input": <Claude Code 给它的 PermissionRequest 输入，原样>}`，
  回 `{"ok": true}`。node 按会话、子 Agent、工具和输入认出是哪一次调用：卡还没发就不发，已经发了就撤掉（`node/hookdecided.go`）。
  这台电脑上一旦有 hook 这样说过（node 启动以来），工具授权的卡先等 1 秒再发，免得手机先收到推送、马上又撤；没有的电脑不等，和以前一样。
  扩展 claude-bypass-allow（bypass 模式下自动允许 Bash 的安全确认）就是这样做的。

### 自检：`personad node doctor`

hooks、MCP 和技能插件都是 node 启动时写出来的文件，坏了没有任何提示（hook 连不上 node 就放行给 TUI，见 `docs/lessons/server.md` node.sock 被删那条）。
`personad node doctor [--json] [--fix]` 在这台电脑上只读检查（`node/doctor.go`），每项一行（`正常｜名字：发现了什么；怎么办`，和 `hub doctor` 同一种写法，三档 `正常` / `注意` / `有问题` 对应 JSON 里的 `ok` / `warn` / `fail`），有 `fail` 时退出码非 0（`hub doctor` 没有 `--json` 也不看退出码）：

| 检查 | 内容 | 状态 |
| --- | --- | --- |
| `node.sock` | 文件在，且发 `{"kind":"ping"}` 能收到应答（真走一遍 `serveSock`） | 不存在或连不上 `fail` |
| `runtime.sock` | 文件在且能连上 | 不存在 `warn`（没用 runtime 可忽略），连不上 `fail` |
| `claude-settings.json`、`claude-hooks`、`assistant-settings.json`、`assistant-hooks`、`mcp.json`、`agent-plugin` | 和**当前这个二进制现在会写的**逐字节比：`current` / `stale`（有差异或插件缺文件，`items` 列出哪些）/ `missing` | `stale` 为 `warn`；hooks / settings / MCP `missing` 为 `fail`，`agent-plugin` `missing` 为 `warn`（只是没有 persona-office 技能） |
| `command-paths` | 这些文件里 hook 和 MCP 的命令（按 `shellQuote` 的写法取第一个词）指向的程序存在且可执行 | 不可用 `fail` |
| `claude-version` | 本机 claude 的版本是否 ≥ `pluginMinVersion`（能不能 `--plugin-dir`） | 读不到或太旧 `warn`（hooks 退回 `--settings`，仍然有效） |
| `herdr` | herdr 的 API socket 能连上 | 连不上 `warn`：引擎由 hub 连上时选定，不存在本机，不是 herdr 时可忽略 |
| `global-permission-hooks` | 用户自己的 `$CLAUDE_CONFIG_DIR/settings.json`（默认 `~/.claude`）里同步（非 `async`）的 `PermissionRequest` 钩子个数和命令（截断到 80 字） | 有就 `warn`：别的工具的审批钩子会和手机审批同时拦着请求；**只看不改** |

- 比较和 `--fix` 共用 `generatedFiles`：node 启动时的 `writeClaudeConfig` 与 `--fix` 是同一条写入路径（`writeGeneratedConfig`），所以 doctor 说 `current` 就是 node 重启后会写出的样子。
  `agent-plugin` 的比较在 `assistantskills.PluginIssues`；`WritePlugin` 也改成只在它报告无差异时才跳过（以前只看戳记，文件被删而戳记还在就一直不补）。
- `--fix` 只重写 Persona 自己生成的文件（发现有 `stale` / `missing` 才写，输出 `fixed`），不碰用户的全局配置，也不会替 node 重建 socket（那要重启 node）。
  **要用 node 实际在跑的那个二进制来跑**：生成文件里写的是「当前二进制」的路径，换了二进制跑 `--fix` 会把 hook 指到它上面。
- `--json`：`{"ok", "dir", "checks": [{"name", "status", "state", "detail", "hint", "items"}], "fixed": []}`，字段一直都在，列表不会是 `null`，检查的顺序和名字固定（`TestDoctorJSONIsStable`）。
- 网页机器页的「这台电脑能做什么」（#2429）经 node RPC `node.doctor` / `node.doctor.fix`（`node/doctor_rpc.go`）拿同一份报告，只有 `full` 设备能调；归并成功能的规则见 `docs/machines.md`「自检」。
- `hub doctor` 管 hub 的配置和网络，不管这些；node 这边的检查另起 `node doctor`，没有往 `hub doctor` 里塞。
- 测试在临时目录里跑（`node/doctor_test.go`）；手动试 `--fix` 用临时 `HOME`，别对真实的 `~/.persona` 跑。

## 全局只报告的 hooks（可选，默认关）

Persona 只在自己启动的 claude 上有 hooks（上一节）。用户自己在 herdr 里起的会话没有，状态和卡片靠读屏幕和 transcript 推断（脆弱：TUI 卡片、多选、预览）。
`hub.json` 的 `machines[].claude_global_hooks: true` 让那台机器的 node 往**默认配置目录**（`$CLAUDE_CONFIG_DIR` 或 `~/.claude`）的 `settings.json` 里加一组只报告的 hooks，
这样别的会话也把回合和提问事件告诉 Persona。不写就是关；写 `false` 或删掉这个键，下次 node 连上时把 Persona 加的条目拿掉。
手改 `hub.json` 要重启 hub，node 重连时生效（和 `botmux`、`herdr_autostart` 一样走 `node.configure`）。

**为什么不能直接把 Persona 的 hooks 装到全局**：全局 `settings.json` 里常常已经有别的工具的同步 `PermissionRequest` hook（本机的 moshi-hook、Orca）。
再加一个会阻塞的就会叠加审批：第一个阻塞的 hook 拖住后面的和终端对话框；而 Persona 的提问 / 授权 hook 要等手机（`timeout` 86400），
装进全局之后坐在电脑前的人在终端里也答不了，直到手机回答或超时。这和 `codex.md` 不采用 Codex 的 `PermissionRequest` hook 是同一个原因。所以全局这一组：

- **只报告**：SessionStart、UserPromptSubmit、Stop、Notification，以及 AskUserQuestion / ExitPlanMode 的 PreToolUse、PostToolUse（卡片出现和被回答的信号）。
  每一条都是 `async: true`、`timeout: 10`；**没有** PermissionRequest、Elicitation，也没有会等手机的 PreToolUse。
- **不回答**：这些会话的手机回答照旧走屏幕卡片和按键（`多选提问的屏幕卡` 一节）。
- **hook 进程**：命令是 `<personad> hook --persona-global`（`runHook`）。没有 herdr / Orca 窗格的环境变量（`HookPane` 为空）或 `node.sock` 不存在就**立刻退出、什么也不输出**，不连 socket、不等；
  连上了最多等 3 秒，输出永远为空。和插件的 hook 一样带上它所在 claude 的 pid 和启动时刻，计数才防得住 pid 复用。
- **不重复报告**：Persona 自己启动的会话已经加载 `persona-hooks` 插件，全局这组不能再报一遍。node 按会话 id 认：`n.state` 里有的（Persona 启动的、恢复的、fork 的）和助手会话一律不处理，也不进轮次计数
  （`takesReport`，`node/hooks.go`）。用环境变量标记要改 Persona 的启动命令（在用户的 shell 函数前面加变量赋值，fish 和 zsh 行为不同），所以没有这样做。
- **不绑窗格**：全局这组不调 `bindPane`。tmux 服务器（botmux）会把 `HERDR_PANE_ID` 传给它后来起的每个 claude，按这个绑会把窗格从真正在里面的 claude 手里抢走（见「会话 id 与 transcript」）。
  窗格对哪个会话由 node 自己找。
- **hub 不当成 Persona 的 hooks**：node 的 `hook.event` 带 `report_only: true`，hub 不把这个会话记进 `hooked`（`hasHooks`）。否则屏幕卡片会为一张永远不来的 hook 卡片白等 1.5 秒。
  `hook_event` 事件照旧广播（用于刷新会话运行状态，`protocol.md`），`Stop` 的 `stop_sig` 照样去重。
- **照样驱动轮次计数**：用户自己起的会话只有这组信号可用，`UserPromptSubmit` / `Stop` 进 node 的轮次计数，状态怎么判见 `agent-states.md`「hook 的轮次信号」。

**写 `settings.json` 的规矩**（`node/globalhooks.go`）：

- 只加 / 只删**带标记的条目**。标记是命令本身以 `hook --persona-global` 结尾，别的工具的命令不会这样写。同一个 matcher 组里用户自己加了别的 hook 时，只拿掉我们那条。
- 幂等：已经是想要的内容就**不写文件**（也不备份、不动排版）；Persona 二进制换了路径，旧条目被替换，不会重复。
- 其他键、其他工具的 hooks 原样保留，顺序不变；因为是经 JSON 编码器写回，排版统一成文件自己的缩进（取第一行缩进，没有就两个空格），原文件的行内数组会展开。
  `&`、`<` 不转义。文件权限不变，经符号链接（dotfiles 管理）写到真实文件，链接还在。
- 改之前先备份成 `settings.json.persona-bak-<UTC 时间>`（只在真的要改的时候）。文件不是 JSON 对象、`hooks` 不是对象、某个事件不是列表时一律不碰，只记日志。
- 配置目录不存在（这台机器没装 claude）不创建。只管默认配置目录；用 `CLAUDE_CONFIG_DIR` 起的别的 profile 目录不动（那是启动器的文件）。
- 和 Claude Code 自己同时写 `settings.json` 有极小的竞争窗口（先写临时文件再改名，不会写出半个文件，但可能丢掉另一边刚写的那一下）。

**为什么没选 `CLAUDE_CODE_PLUGIN_DIRS`**（2026-11-04，Claude Code 2.1.318 实测）：这个环境变量**真实存在**。`claude --help` 里没有，但二进制里有读取它的代码：
值是绝对路径的列表，分隔符是平台的 PATH 分隔符（`path.delimiter`，Linux / macOS 是 `:`），不接受 `~` 开头的和非绝对路径（跳过并警告），
和 `--plugin-dir` 合并（重复的去掉）；报错提示里说「在环境里，或在设置的 `env` 块里」设置，也就是 `settings.json` 的 `env` 也行。
没选它是因为它只有**一个变量**：本机的 moshi-hook 已经把自己的插件目录写在全局 `settings.json` 的 `env.CLAUDE_CODE_PLUGIN_DIRS` 里，
Persona 要用就得去改别人写的那个值（moshi 重装时多半又会改回去）；而 `hooks` 是按事件叠加的，互不覆盖。
shell 里 `export` 能不能被 `settings.json` 的 `env` 盖掉，这里没有验证。没有这个冲突的机器，也可以自己让一个 Persona 写的插件目录进这个变量。

**没验证的**：真实 Claude Code 对这组条目的触发（`async` 在 `settings.json` 里的 hooks 已被本机别的工具使用，字段本身没问题，但这组条目没有在真实会话里跑过）；
`PreToolUse` 的 matcher 写成 `AskUserQuestion` / `ExitPlanMode` 两个组而不是一个 `|` 的写法，是照别的工具的写法；Windows 上命令的引号。

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
默认不替用户往他自己的配置目录里装 hooks（那是用户的文件；可选的只报告 hooks 见「全局只报告的 hooks」，它也答不了题）；所以改成屏幕卡认得多选、答得了。

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

## 带预览的提问（2026-11-04，2.1.303 实测）

AskUserQuestion 的选项带 `preview` 时，选项排在左边，右边一个方框显示光标所在选项的预览，底部提示是
「Enter to select · ↑/↓ to navigate · n to add notes · Tab to switch questions」，没有「Type something」，「Chat about this」不带编号。
这里**按数字只挪光标、不选**，要再按 enter：只按数字时卡片显示答过了，电脑上的问题还停着（2026-11-04 用户在手机上答第二题没生效）。

- 读屏（`server/internal/screen`）认出选项行右边的方框（`┌` `│` `└`）就当成带预览的提问：方框和「Notes: press n to add notes」不进任何选项的
  标签和说明（以前整行进了标签，预览还会跟着光标变，卡片一挪光标就 `prompt_changed`），每个选项的按键是「数字 + enter」。
- 是最后一题时按完照常提交确认页（上面「多选提问的屏幕卡」的 `submitReview`）。
- 改了解析，hub 和 node 要一起部署：带预览的提问两边算出的 `rev` 不一样，只更新一边会一直 `prompt_changed`。没有预览的对话框 `rev` 不变。

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
- 解析成 user 条目并带 `from`（发送方名字），排队条里显示为「【来自 名字】正文」；手机上画成左侧的灰色卡片「来自 名字」，正文按 Markdown 画（同回答），不当成用户自己的消息。


## transcript 里的特殊 XML（2026-10-26 扫了本机和 hub 机器）

| 标签 | 处理 |
| --- | --- |
| `<command-name>` / `<command-message>` / `<command-args>` | 显示成「/命令 参数」 |
| `<local-command-stdout>` / `-stderr` | 提示行 |
| `<local-command-caveat>` | 给模型看的说明，去掉 |
| `<task-notification>` | 后台任务完成提示（`docs/ios.md` 唤醒） |
| `<cross-session-message from-name=…>` | 「来自另一个会话」的消息 |
| `<teammate-message teammate_id=…>` | Agent 团队里队友的汇报，按「来自 teammate_id」显示。正文是 Agent 团队的 JSON 通知时 hub 转成 Markdown（`transcript/teammate.go`）：`idle_notification` 是加粗的 `summary`、汇报正文 `result`、出错停下的原因；`task_assignment` 是「分配任务 #编号：标题」和说明；结束请求 / 同意 / 拒绝、计划提交 / 批准 / 退回各一行。不认识的类型、不是 JSON 的原样 |
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

## 安全拦截与自动降级（方案，#2282，待拍板）

用户问「触发了 Claude 的安全拦截，App 里看得到吗？模型降级了我们知道吗？」：现在两种都看不到或说错了。这一节是方案，**还没实现**；
标「待拍板」的由用户定，拍板后按文末的拆分做。

**原则**：只把发生了什么如实告诉用户，由用户决定怎么办。不绕过、不减少拦截：不改写用户的消息，不自动换说法重试，不自动「继续」被拦下的一轮，
也不让管家的判断模型替用户决定（被拦下的一轮只走规则，不进模型分诊）。

### 现状

Claude Code（2.1.318 实测，见 issue）的消息被安全检查（safeguards）拦下时有两种结果：

| 结果 | transcript 里 | Persona 现在 |
| --- | --- | --- |
| 自动降级（默认） | 一行 `system/model_refusal_fallback`：原模型、新模型、类别、范围；`scope: "session"` 时之后整个会话都用新模型 | `transcript.go` 的 `system` 分支只认几种 subtype，其余 `default: return nil`，这行被丢掉；之后的 assistant 行带新模型，`Meta.Model` → `Session.model` 悄悄变了，输入框的模型按钮（`SessionModes.modelName`、网页 `launcher-models.ts` 的 `modelName`，#2200 / #2201）跟着显示新模型，没有任何说明 |
| 拦下未降级 | 一行 `system/model_refusal_no_fallback`，加一条 `stop_reason: "refusal"`、模型 `<synthetic>` 的合成回复「API Error: …safeguards flagged this message…」 | system 行同样被丢；合成回复是普通 assistant 文本，`entry.Message` 不读 `stop_reason`。它以 `API Error:` 开头，于是按 #1516 当成模型服务出错（下表） |

拦下未降级现在在各处的说法（全是「模型服务出错」那一套）：

- 对话：iOS `Views/Chat/APIErrorCard.swift`、网页 `web/src/lib/api-errors.ts`、安卓 `core/domain/.../lines/ApiErrors.kt` 认 `API Error:` 前缀，
  认不出状态码时是「模型请求失败」；`transient` 不是 false 就给「重试」，发的是「继续」（`ChatView.swift`）。上下文没变，多半再被拦一次。
- hub：`hub/state.go` 的 `turnError` 用 `assistantwire.CutOff`（`APIErrorMarkers`）把最后一句认成 `TurnError`；推送（`hub/machine.go`）是
  「⚠️ 标题」+「模型服务出错，中断了：<原文>」。
- 任务：回执 `state: "interrupted"`、摘要「模型服务出错，中断了」（`assistant/tasks.go`、`assistantwire.InterruptedSummary`），
  卡片上「继续」（`continueTask`，发「继续」）；主对话的提示词 `prompts/assistant/task_notice.md` 说「被模型服务出错打断了」。
- 管家分诊（`assistant/agents_work.go` 的 `triageRules`）：`transientError` 判成临时的走 `WaitTransient`，开着「自动继续」就**自动发「继续」**
  （最多 `maxRetries` 次）；否则是「模型服务出错，需要你处理」的卡片。拦截原文里有没有 `transientError` 的关键词（`upstream`、`network`、`api_error`……）
  没有核对过：**如果有，现在就在自动重试被拦下的消息**，第一个 PR 就要堵上（见下「待验证」）。
- 定时任务、跟进（`assistant/schedule.go` 的 `watchScheduledRun`）：这一轮结束后 `lastTurnAPIError` 按 `APIErrorMarkers` 前缀认最后一句，
  合成回复以 `API Error:` 开头就**一定**命中，和关键词无关：运行记成失败，推送「定时任务失败：<原文>」，**3 分钟后用同样的提示词再跑一次**
  （`docs/assistant.md`「失败和重试」）。这是现在确定存在的自动重发，第一个 PR 一起堵上。
- 任务（待办）卡片：网页 `web/src/lib/todos.ts` 的 `interruptedAgent` / `interruption()`（`views/assistant/Tasks.tsx`）、安卓 `Tasks.kt` 的
  `Interruption.of` 和 `Models.kt` 的 `Session.interrupted` 从 `session.turn_error` 推出「中断了」，`parseAPIError` 认不出时原因是「模型请求失败」、
  `canContinue` 为真，给「继续」。客户端现在都不读 `turn_error_kind`。

本机（Claude Code 2.1.310）的二进制里没有 `model_refusal_*` 和 `CLAUDE_CODE_DISABLE_REFUSAL_FALLBACK` 这些字符串，本机和 `CLAUDE_CONFIG_DIR`
目录里的 transcript 也没有这两种 system 行，下面的字段表来自 issue 的描述，**字段名要用脱敏的真实片段核对**后再写解析。

### 解析（node：`server/internal/transcript`）

- `entry` 加 `model_refusal_*` 行的字段（原模型、新模型、类别、范围，按核对出的真实键名），`Message` 加 `stop_reason`。
- `system/model_refusal_fallback` → 一条 `KindNotice`，带新的 `refusal` 结构：
  `{"outcome": "fallback", "from": "<原模型>", "to": "<新模型>", "category": "<类别原文>", "scope": "session" | <其他原文>, "raw": "<行的 content 原文，可空>"}`。
  `text` 照样填一句中文（下面「各端怎么说」），旧 App 不认 `refusal` 也能显示成一条普通提示。
- `system/model_refusal_no_fallback` → 同样一条 notice，`outcome: "blocked"`。紧跟的 `stop_reason: "refusal"` 合成回复：文本 item 带
  `refusal: {"outcome": "blocked", …}`（和前一行合并成一张卡，不再是 API 错误卡），**不再进 `Meta.LastText` 当成回答**，改记
  `Meta.TurnError` = 原文、`Meta.TurnErrorKind = "refusal"`（新值，和 `transient` / `user` 并列，`proto.Session.TurnErrorKind`）。
  判断靠 `stop_reason` 和 system 行，不靠匹配「safeguards」之类的英文（措辞会变）。
- 合成回复 item 的 `text` **去掉开头的 `API Error:` 前缀**（原文完整留在 `refusal.raw`）：各端的 API 错误卡（iOS `APIErrorCard`、网页 `api-errors.ts`、
  安卓 `ApiErrors.kt`）只认这个前缀，text 由 node 的解析器产出，去掉后**旧客户端也只画一段普通文字、不给「重试」**（不用等它们更新）；
  hub 的 `CutOff` / `lastTurnAPIError` 也不会再按前缀把它当 API 错误。代价是旧客户端看不出这是一张拦截卡，只是一句英文说明，比给一个会再被拦的「重试」好。
- `TurnError` 什么时候清掉：Claude 这条路现在不设 `Meta.TurnError`（hub 从 `LastText` 推，后面的回答一来就自然没了），新加的 `TurnErrorKind = "refusal"`
  必须照 Codex 在 `task_started` 清空的做法，**在下一条真实的用户消息或下一条非合成的 assistant 回复时把 `TurnError` / `TurnErrorKind` 清空**；
  否则拦截行还在 node 快速 meta 的尾部时，换了说法、答得好好的一轮照样被 hub 推「被拦下」、分诊留着卡片、任务一直是「中断了」。
- 认不出的：subtype 以 `model_refusal` 开头但不是这两种、或者缺了模型 / 类别字段，一律出一条 notice，`text` 是行里的 `content` 原文
  （没有就是「Claude 的安全检查对这条消息做了处理（Persona 认不出具体情况）」），`refusal.outcome` 留空。类别不在已知表里就显示原文（`cyber` → 「网络安全」，其余先原文）。
- 会话级：`proto.Session` 加 `model_fallback`（`{from, to, category, scope, at}`）：最近一次降级是 `scope: "session"`，且最新的 assistant 行还是 `to` 时才有；
  用户 `/model` 换走了就没了。只在 node 的增量扫描里记（和 `bgScan` 一起），不另读文件。
- 改了 item 结构：iOS `ChatCache` 目录版本号加一（现在 `chats-v8`，做的时候以 master 上最新的为准），`docs/lessons/transcript.md` 那条规矩。
  hub 的 golden / `protocol.md` 的 Item、Session 字段表一起改。

### 各端怎么说

文案先定这些，iOS / 网页 / 安卓用同一套（文案不放 hub 的 Go 代码里：notice 的 `text` 是给旧客户端的兜底，新客户端按 `refusal` 自己画）：

| 地方 | 自动降级 | 拦下未降级 |
| --- | --- | --- |
| 对话里 | 一行提示（不是错误红色，用 `caution`）：「Claude 的安全检查拦下了这条消息，改用 <新模型> 回答（类别：<类别>）」；`scope: "session"` 时加「之后这个会话都用 <新模型>」 | 一张卡（`caution`，不用 `danger`、不叫「错误」）：标题「被 Claude 的安全检查拦下了，没有回答」，副标题「类别：<类别>」；折叠里是原文和时间。**不给「重试」**；说明一行「同样的内容再发多半还会被拦，可以改一下再发」 |
| 输入框模型按钮 | 显示实际在用的新模型（沿用 #2200 / #2201 的友好名），旁边一个小标「已降级」（新的设计系统 token / 组件，不写死）；菜单里多一项「切回 <原模型>」 | 不变 |
| 切回原模型 | herdr 里的 Claude：发 `/model <原模型的别名>`（`SessionModes.swift` 现有的换模型路径，`PromptItems.claudeModelID`）；只读会话（`claude_bg`）、无头助手不给这一项（助手的模型来自 hub 的模型配置，下一轮按配置起，#1562，要核对降级会不会跟着 `--resume` 留下） | — |
| 推送 | 不单独推（回答照常出来，「完成了」推送照常） | 「⛔ <标题>」+「被 Claude 的安全检查拦下了，没有回答（<类别>）」；不带原文里的消息内容 |
| 任务回执 | 回执照常（做完 / 没做完按原规则判）；摘要后加一句「其中 N 轮被安全检查拦下，改用 <新模型> 回答」 | `state: "interrupted"` + 新字段 `cause: "refusal"`，摘要「被 Claude 的安全检查拦下了」；新客户端不画「继续」，画「去对话」。不另起 `state`：旧网页的 `receiptState` 把认不出的 state 当成「done」 |
| 主对话的任务提示（`task_notice.md`） | 不变 | 「被 Claude 的安全检查拦下了（等用户决定）」；提示词里写明不要替用户换说法重发 |
| 任务（待办）卡片 | 不变 | 网页 `todos.ts` 的 `interruption` / `todoStatus`、安卓 `Tasks.kt` 的 `Interruption` / `Session.interrupted` 先看 `turn_error_kind == "refusal"`：显示「被 Claude 的安全检查拦下了」，**不给「继续」**，给「去对话」 |
| 定时任务、跟进 | 不变 | `watchScheduledRun` 看这一轮是不是以拦截结束（`Item.refusal` / 会话的 `TurnErrorKind == "refusal"`，不看文字前缀）：记成被拦下，**不重试**；推送「定时任务被拦下：<标题>」+「被 Claude 的安全检查拦下了，没有回答（<类别>）」，不带原文 |
| 管家分诊 | 不影响 | `triageRules` 在 `transientError` **之前**看 `TurnErrorKind == "refusal"`：`WaitUser` 卡片「被 Claude 的安全检查拦下了：<类别>」，选项「我来处理」（打开对话）/「先不管」；不进 `WaitTransient`、不自动继续、不调判断模型 |

旧客户端：notice 的中文 `text` 能看；合成回复的 text 去掉了 `API Error:` 前缀（见「解析」），旧 App 画成普通文字，不给「重试」。
旧网页 / 旧安卓的任务卡片不读 `turn_error_kind`，仍会把它当「中断了」并给「继续」，这一处只能等更新。
hub 的 `continueTask` 对 `cause: "refusal"` 不拒绝（用户自己点的「继续」是用户的决定），但新客户端不给这个按钮。

### 默认策略（待拍板）

`CLAUDE_CODE_DISABLE_REFUSAL_FALLBACK` 只认进程环境，不能写进 `--settings` 的 `env`（issue 实测）。三个选项：

- **A. 保持 Claude Code 的默认（自动降级），只是显示出来。** 什么都不用改启动方式；回答不断，但用户可能不知道之后都在用较弱的模型（上面的「已降级」标就是为这个）。
- **B. Persona 起的 Claude 一律关掉自动降级。** 被拦就停下，用户自己决定换说法、换模型还是放弃；最近拦截变多，停下的次数会明显增加，任务会更常卡住。
- **C. 设置项（推荐，待拍板）**：设置 › Claude Code「被安全检查拦下时」：「换个模型接着答」（默认，= A）/「停下等我」（= B）。存 hub 的 `settings`，经 `node.configure` 下发；
  主对话、herdr 里的 Agent 共用一项（以后要分再分）。

关掉时变量怎么进去（实现时的注意点）：

- herdr 窗格：`pane run` 的命令和 herdr 的 `resume_argv`（`node/herdrresume.go` 的 `claudeCommandLine`）前面加 `CLAUDE_CODE_DISABLE_REFUSAL_FALLBACK=1 `。
  启动器多是 shell 函数，不能用 `env` 包（`env` 跑不了函数）；zsh / bash 里「赋值 + 函数」会把变量带给函数里起的 claude，fish 行为不同（「全局只报告的 hooks」那节也碰到过），要分别验证。
- Persona 管理的启动器：和 `CLAUDE_CODE_DISABLE_ARTIFACT` 一样放进 `managedEnv`（`node/managed.go`），`jarvisd launch` 带上。
- 无头助手：`node/assistant.go` 的 `cmd.Env`。
- 用户自己在电脑上起的会话不管（那是他自己的终端），只显示。改了设置只对之后起 / 恢复的进程生效，界面上要说。

### 多 Agent 协作时

- 一个 Agent 被拦下（未降级）：它停在那里，任务显示「被安全检查拦下了」，**不算做完、不算模型服务出错**；同一任务里的其他 Agent 照常跑，
  在等它的（`WaitAgent`）继续等；管家不给它发「继续」，也不把它的活改派给别的 Agent 或换个说法交给另一个 Agent。主对话收到任务提示，由用户决定。
- 自动降级：不打断流程，只在对话和回执里记下来；`scope: "session"` 的会话在 Agents 列表的模型名上带「已降级」。
- 主对话自己被拦：主对话里出同样的卡；定时任务、跟进（`docs/assistant.md`）的这一轮记为被拦下，不重试，下一次到点照常跑。

### 兼容和待验证

- 这些 system 行是新加的、部分字段是内部的，可能改名：解析只在拿得到时用字段，认不出就显示原文，不报错、不丢行。
- **待验证**（实现前用脱敏的真实片段，放进 `server/internal/transcript/testdata`，文字换成占位符）：
  1. 两种 system 行的真实键名、`scope` 和类别的取值；合成回复的完整形状（是否总有 `stop_reason: "refusal"`、模型是否总是 `<synthetic>`）。
  2. 拦截原文会不会命中 `transientError`（现在会不会被管家自动「继续」）。
  3. 降级后 `--resume` 会不会留在新模型上；`/model` 切回后下一条会不会马上又被拦、又降级。
  4. 自定义网关（`ANTHROPIC_BASE_URL`）、API Key、Bedrock / Vertex 的启动器下会不会降级、降级到的模型网关有没有（没有就是一条普通 API 错误）。
  5. `CLAUDE_CODE_DISABLE_REFUSAL_FALLBACK` 从哪个版本起有、在 zsh / bash / fish 的「赋值 + 函数」下是否生效。
  6. 无头助手的 stream-json 里有没有对应的事件（现在结构化内容仍从 transcript 读，有的话可以更早显示）。
- Codex、TRAEX、DSH、pi 不涉及；它们自己的拒答以后有了再按同一个 `refusal` 结构接。

### 实现的 PR 拆分（拍板后）

1. 服务端：transcript 解析和夹具、`proto` 的 `Item.refusal`、`Session.model_fallback`、`TurnErrorKind: "refusal"`；hub 的推送、任务回执 `cause`、
   `triageRules`（先堵上自动继续）、`watchScheduledRun` 不重试被拦下的运行和它的推送文案、`TurnError` 的清空、`task_notice.md` 和 golden；
   `protocol.md`、本节改成现状。
2. iOS / Mac：降级提示、拦截卡、模型按钮的「已降级」和「切回」，`ChatCache` 版本号加一；深浅色截图。
3. 网页：同上（`api-errors.ts` 之外单独的拦截卡，`feed.ts` 的回执，`todos.ts` 的任务卡片读 `turn_error_kind`）。
4. 安卓：同上（含 `Tasks.kt` / `Models.kt` 的任务卡片），三套界面风格 × 深浅色截图。
5. 默认策略（按拍板结果）：设置项、`node.configure`、三处注入变量。

### 验收

- 两种情况的脱敏 transcript 片段都有解析测试；认不出的 subtype 显示原文；拦截之后跟一轮正常回答时没有 `TurnError`（有测试）。
- 被拦下的定时任务、跟进记为被拦下、不重试（有测试）；任务卡片显示「被安全检查拦下」，没有「继续」。
- 拦下未降级：对话里是拦截卡，没有「重试」；推送、回执、主对话提示、分诊卡片都说「被安全检查拦下」，不说「模型服务出错」；管家不自动发任何消息（有测试）。
- 自动降级：对话里有提示；`scope: "session"` 时模型按钮带「已降级」，「切回」发出 `/model`。
- iOS、网页、安卓深浅色都截图看过；改了 item 结构的 `ChatCache` 版本号加了一。
