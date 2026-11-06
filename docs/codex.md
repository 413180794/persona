# Codex 支持

herdr 里跑的 Codex（`codex` CLI）和 Claude 一样出现在「Agents」tab：列表、对话、实时状态、发消息、终端、关闭都能用，
也能从手机新建（「从手机新建」）。2026-10-26 起支持，`server/internal/node/codex.go` + `server/internal/transcript/codex.go`。

## 识别

- herdr 把 Codex 窗格报成 `agent: "codex"`。装了 herdr 的 codex 集成（`~/.codex/herdr-agent-state.sh`，挂在
  `~/.codex/hooks.json` 的 SessionStart）时 `agent_session.value` 就是 Codex 的会话 id。
- 没有会话 id 的窗格（集成装之前就启动的，或者 herdr 只在启动时记了 id、之后在 Codex 里 `/resume` 换了线程）：
  Codex 把线程名写在终端标题里（「线程名 | 目录」，两段都可能被截成 `...`），按 `session_index.jsonl` 找同名线程（`codexByTitle`），
  同名的有几个时优先 herdr / 启动时记的那个，否则取最近写过的。标题能说明的窗格先认，标题里没有线程名（新线程）的最后才按
  「同一工作目录下最新、还没被认领的 rollout」猜（`codexSession`）。2026-10-31 以前只靠猜：一台机器同目录 5 个 Codex 认错 4 个，
  正在跑的那个显示成别的会话、灰点、新一轮的消息一条都看不到（整个机器 26 个 Codex 窗格认错 9 个）。
- **子 Agent 的 rollout 不算会话**：Codex 的 thread_spawn 子 Agent（TRAEX 还有审核工具调用的 guardian，见下）各写一份自己的 rollout，
  和主对话在同一个目录、同一个 cwd，而且更新。认法只看 `session_meta`：`thread_source` 是 `subagent`，或 `source` 是 `{"subagent": …}`；
  它属于哪个对话看 `session_id`（没有就看 `parent_thread_id`）。猜会话时跳过它们，herdr 报来的 id 如果是子 Agent，换成它的主对话
  （`rolloutMeta`、`mainThread`）。2026-11-01 扫了一台 Mac 的 435 份 Codex rollout，203 份是子 Agent，全都带 `thread_source` 和 `session_id`。
- rollout 放在线程**开始那天**的目录（`sessions/YYYY/MM/DD`），之后一直往里写：六周前开的线程今天还在用。列 rollout 时近两周的目录
  每 5 秒扫一次，更早的目录每分钟扫一次，只留两周内写过的（`rollouts`）。
- herdr 的终端标题是「dots | codex | Ready」，不能当标题：标题用 Codex 自己起的线程名（`~/.codex/session_index.jsonl`
  的 `thread_name`，同一个 id 以最后一条为准），没有就用第一条用户消息。
- `proto.Session.agent` = `codex`；App 列表行、空间页卡片都带 Agent 标签（Claude 和 Codex 同一种描边小标签，`AgentTag`），对话页副标题带 Agent 名，输入框「给 Codex 发消息」，
  分享卡片署名 Codex（深色图标）。

## 会话记录（rollout）

`~/.codex/sessions/YYYY/MM/DD/rollout-<时间>-<会话 id>.jsonl`，每行 `{timestamp, type, payload}`；第一行 `session_meta`
（含 cwd，约 20 KB 的系统指令）。`transcript.ParseLine` 看到带 `payload` 的行就按 Codex 解析（`Meta.Codex`），不用另开文件类型。

| rollout | 对话里 |
| --- | --- |
| `event_msg/item_completed` UserMessage | 用户消息 |
| AgentMessage `phase=commentary` | 思考（工具之间的旁白，折进工作行） |
| AgentMessage `phase=final_answer` | 回复 |
| Reasoning（有 `summary_text` 时） | 思考 |
| CommandExecution | `Bash`（去掉 `/bin/zsh -lc` 外壳），结果是 `aggregated_output`，非 0 退出码记为失败 |
| FileChange | 每个文件一个 `Edit`，输入带 `diff`（unified diff，App 用 `UnifiedDiffView` 画） |
| McpToolCall | `mcp__<server>__<tool>` |
| ImageView | `Read` |
| ContextCompaction | 提示「上下文已压缩」 |
| `event_msg/turn_aborted` | 提示「已中断」 |
| `turn_context` / `token_count` | 模型、审批策略、上下文 token |

UserMessage 里 Codex 自己的 XML（2026-10-26 扫了本机和 hub 机器的全部 rollout）：
`<send_user_message_question_reply>`（回答 Codex 的 request_user_input 提问，里面是 JSON）显示成回答本身，多题时每行「问题：回答」；
`<in-app-browser-context>`（Codex App 自动附带的浏览器状态）去掉；`local_image` 内容变成 `<attached_images>` 路径，App 显示成缩略图。
`<environment_context>`、`<INSTRUCTIONS>`、`<skill>`、`<turn_aborted>`、`<subagent_notification>`、`<realtime_delegation>`、
`<codex_internal_context>` 只出现在 response_item（模型输入），不会进对话。

`response_item` 是同样内容的模型线格式，跳过。Extension 里的 `web.search` 显示成 `WebSearch`（搜索，输入是查询词）/ `WebFetch`（`action.type=openPage`，输入是网址），结果每条「标题  网址」；SubAgentActivity、其他 Extension（生图）、CollabAgentToolCall 保留为可识别的事件提示，完整内容仍以原始会话记录为准。

Persona 助手（「Persona」tab）也能用 Codex 的启动器，走的是 `codex app-server`，见 `docs/assistant.md`「Codex」。

一台电脑多个已有 app-server（桌面 App、独立 `CODEX_HOME`、多客户端读写和审批）已接入现有 Agents 列表，源码研究、配置和能力边界见 [`codex-app-server.md`](codex-app-server.md)。发现历史不需要 herdr；只有提供受保护 Unix/回环 WebSocket 入口的实例能实时接入和控制，外部 stdio 明确只读。

## 守护进程上的会话（app-server 路线）

2026-11-02 起，跑在共享 app-server 守护进程上的 Codex 窗格，Persona 作为第二个客户端接进它的 thread，提问、审批、发消息都走协议，
不再读屏幕、不再往终端里打字。接不上的照旧走下面「提问和审批」的终端路线（#214）。

- **为什么能接**：0.163 起裸 `codex` 默认自动启动或加入共享守护进程（`<CODEX_HOME>/app-server-control/app-server-control.sock`），
  TUI 只是它的一个客户端；同一个 thread 的服务端请求（审批、`request_user_input`）发给每个订阅者，先答的算数，答完所有人收到
  `serverRequest/resolved`，各自撤掉提示（`docs/codex-app-server.md`）。
- **找到守护进程**：node 本来就会发现每个 home 的官方 control socket 和带 `--listen` 的 app-server 进程，并各连一条 WebSocket
  （`codex_remote.go`）。0.163 的 control socket 是个别名：一个符号链接，指向 `<规范化的 /tmp>/codex-daemon-<uid>/<别名规范路径的 SHA-256>`
  （Codex 自己的 `uds/src/daemon_directory.rs`、`unix_socket.rs`）。以前按「不是 socket / 是符号链接」一律不认，所以从来没连上过；
  现在只跟随 Codex 自己会算出来的那一个目标（别名所在目录要属于当前用户、别人不能写；目标目录 0700、socket 0600），别的符号链接照旧拒绝（`codexSocketPath`）。
- **哪些窗格走它**：herdr 里活着的 Codex 窗格，node 认出的 thread id（`agent_session` / 线程名 / rollout，见「识别」）出现在某个已连上的
  app-server 的 `thread/loaded/list` 里，就说明这个 thread 由那个 app-server 运行（同一个 thread 只能有一个写者），窗格里的 TUI 是它的客户端。
  node 每秒检查一次，没接的在后台 `thread/resume`（只带 `threadId` 和 `excludeTurns`，不带模型、目录、沙箱、指令，不改会话的任何设置；
  已经有订阅者时 app-server 也会忽略覆盖），失败的 30 秒后再试，原因记在会话上（`codex_daemon.go`）。
- **接上以后**：
  - 审批（命令、改文件、权限、MCP 工具）和 Plan 模式的 `request_user_input` 是 app-server 的服务端请求，转成和助手同一套的卡片
    （带 `agent: "codex"`、Codex 的理由、「总是同意」= 本会话：`acceptForSession` / 权限 `scope: session`），答案按协议回传；
    拒绝时写了理由，回 `decline` 之后把理由作为一条消息插进这一轮（`turn/steer`，和助手的 `tellModel` 一样）。
  - async 提问（`request_user_input_async`）不是请求而是一条消息，照旧从 rollout 认（「提问和审批」），回答的回复信封作为消息经 app-server 发（`turn/start` / `turn/steer`）。
  - 手机发消息走 `turn/start`（空闲时）或带当前轮次的 `turn/steer`（工作中；接入前就在跑的轮次读一次 `thread/read` 拿到它），
    不往终端打字：电脑输入框里的草稿原样留着，不会被吞掉也不会混进去。
  - 思考强度能在手机上改（权限模式不能），见下面「思考强度」。
  - 电脑先答：卡片随 `serverRequest/resolved` 撤掉（`interaction.dismiss`），手机再答不会有任何动作；手机先答：TUI 的提示随之关掉；
    两边同时答：app-server 只认第一个，命令只跑一次。观察者从不自动回答（`codex_remote_asks.go`）。
  - 终端路线对它关掉：不读它的屏幕找审批、不认它 rollout 里的阻塞提问，同一个问题不会出两张卡、答两次。
- **接不上的**：带 `-c` / `--profile` 等参数起的（0.163 这时不用守护进程，在自己进程里跑）、已经在跑但不在守护进程上的、0.153（只在
  socket 已经在时才自动连）、TRAEX：照旧终端路线。
- **守护进程重启、断开、换版本**：连接断了，接入状态跟着没了，这些窗格立刻退回终端路线（屏幕卡片、打字），卡片撤掉；TUI 自己显示
  「Reconnecting to server…」并在新的守护进程起来后重连、重新加载 thread，node 隔 2–5 秒重连新的守护进程，重新接入，挂着的请求会重放（新的卡）。
  Persona 不会让 TUI 卡住：请求同时发给 TUI，电脑上随时可以答。
- **看得出走哪条路**：会话带 `codex_route`（`app_server` / `terminal`）和 `codex_route_note`（为什么是终端），App 的「会话信息」显示「连接：app-server（结构化）」
  或「终端（读屏幕）」加原因；node 日志有每次接入成功 / 失败。
- **测试**：`node/codex_daemon_test.go`（合成 app-server 上的自动接入、发消息、async 回答、审批卡、拒绝理由、接入失败重试、socket 别名）；
  真实 Codex：`scripts/test/test-codex.sh` 带 `JARVIS_CODEX_DAEMON_BINS`（`docs/testing.md`「Codex 联调」），在独立的 herdr 里起一个只给测试用的 `codex app-server --listen unix://…`
  （不碰正在用的守护进程，node 关掉发现、只认它），TUI 用 `--remote` 挂上去。

## 思考强度

2026-11-04 起（#1709），手机经 app-server 驱动的 Codex 会话能在手机上改思考强度：Agents 里能发送的「Codex 实时」会话
（`source_kind: codex_app_server`），和已经接上共享守护进程的 herdr 窗格（上面「守护进程上的会话」，「连接：app-server」）。
手机发给它们的消息本来就是 `turn/start` / `turn/steer`，思考强度是这个 thread 自己的设置，不用往终端里打字（`node/codex_effort.go`）。

- **不变的**：权限模式在手机上仍然不能改（Codex 的审批策略和沙箱照旧由启动参数和 `config.toml` 定，请求带权限模式回
  「Codex 的会话在手机上只能改思考强度，权限模式要在电脑上改」）；只在终端窗格里跑的（终端路线）、子 Agent、Codex 桌面 App 自己的对话、
  TRAEX 连思考强度也不能改，提示照旧；从手机新建 Codex 仍然不带思考强度（见「从手机新建」）。
- **能选哪些**：这个 thread 的模型在 app-server `model/list` 里报的 `supportedReasoningEfforts`（每条连接读一次，带 `includeHidden`，
  thread 可能用的是隐藏模型）；模型不在列表里、或者读不到列表，用「Codex 设置」页的同一套：minimal / low / medium / high / xhigh / max / ultra；
  模型报的是空列表就是不支持，不显示、不收。会话带上 `efforts`（这些档位，从弱到强）和 `effort`（当前值），客户端只看 `efforts` 非空决定显示
  （`docs/protocol.md`「Codex 来源与能力」）。node 自己校验档位：实测 0.163 的 `thread/settings/update` 给什么字符串都收。
- **当前值**：app-server 说的这个 thread 现在配置的值（每 5 秒 `thread/read` 的 `reasoningEffort`、接入时 `thread/resume` 的回答、
  `thread/settings/updated` 通知）；thread 没设（`null`，新 thread 就是这样）时是它的模型的默认值（`model/list` 的 `defaultReasoningEffort`）；
  老的 app-server 不报这些时用 rollout 最后一轮 `turn_context` 记的。电脑上在 TUI 里改了，手机上跟着变。
- **怎么改**：node 校验后发 `thread/settings/update {threadId, effort}`，从下一轮起生效，不管下一轮是手机发的还是电脑上发的，TUI 也收到
  `thread/settings/updated`；这一轮正在跑时也能改，这一轮不变。这个方法在 0.163 是实验接口（node 连 app-server 时本来就声明了 `experimentalApi`），
  实测：调用成功、随即推 `thread/settings/updated`、`thread/read` 读回新值。
- **app-server 没有这个方法**：只认 JSON-RPC `-32601`（method not found）。这时选择先记在 node 内存里（`sessionModes.Codex`），
  手机下一次 `turn/start` 带上 `effort`（`turn/start` 的 effort 管这一轮和之后），带出去一次就清掉，期间会话显示的就是这个选择；
  `turn/steer`（这一轮在跑时插话）不能带 effort，所以等下一次 `turn/start`。这个选择只对当时那条连接上的那个 thread 有效：
  连接断了又连上（可能已经换成支持这个方法的版本，thread 也可能在电脑上换过思考强度）、thread 在 app-server 上卸载了（会话在那边结束了），
  它就作废、不带出去，会话显示 thread 自己的值，要改就在手机上重新选；带着它的 `turn/start` 被 Codex 拒绝也丢掉，免得之后每条消息都失败。
  node 重启会丢掉还没带出去的选择（重启本来就是新连接）；`thread/settings/update` 成功的存在 app-server 里，不丢。
- **其他失败一律报错、不记**：同时清掉之前还没带出去的选择，免得之后的 `turn/start` 把手机已经被告知没改成的值带出去。Codex 回别的错误
  （-32602 参数不对、thread 不存在等）把原话转给手机；连接断开报「断开了」；15 秒没回应报「可能没有改成」（请求也许已经到了 app-server，
  会话显示的以它下一次读到的为准）。
- **客户端**：网页、iOS、Android 对 `efforts` 非空、能发送的会话只显示思考强度：网页输入框下面的 chip、iOS 旧布局输入框上方的标签和新外壳「模型 · 思考强度」
  按钮的菜单、Android 的「模式」按钮；选项就是 `efforts`（minimal「极低」、ultra「极高」，不认识的照原样显示）。Claude Code 和助手照旧两个都有。
- **测试**：`node/codex_effort_test.go`（合成 app-server：接上前照旧拒绝、接上后只收思考强度、非法档位、`thread/settings/update` 的参数、
  -32601 时下一次 `turn/start` 带上而 `turn/steer` 不带、别的错误码 / 超时 / 断线都报错不记并清掉之前的选择、带着它的 `turn/start` 被拒绝就丢掉、
  换了连接或 thread 卸载就作废、按模型的档位和默认值、Codex 拒绝、模型没有档位、`thread/settings/updated`、终端路线和 TRAEX 照旧拒绝）；
  网页 `session-actions.test.ts`；iOS `SyncEventTests.codexThreadOnItsAppServerSetsItsEffortOnly`；Android `SessionSourceTest`、`SessionModesTest`。
  真实 Codex 只在临时 `CODEX_HOME` 的 stdio app-server 上手动验过 `model/list`、`thread/settings/update`、`thread/read`（没跑模型），
  `scripts/test/test-codex.sh` 还没有这一项。

## Codex 桌面 App 里的会话（App 的本机 IPC）

桌面 App 自己的 app-server 走 stdio，外部接不进去；另起一个 app-server 续接同一个会话会被拒（已有活动写入者）。
但 App 主进程在 `<CODEX_HOME>/ipc/ipc.sock` 开了一条本机消息总线，App 的窗口之间靠它互相请求「在你持有的会话里跑一轮」。
node 作为总线上的又一个客户端（`server/internal/node/codex_desktop.go`）：

- 帧是 4 字节小端长度 + JSON；先 `initialize`，再 `thread-owner-discovery` 找持有会话的窗口，再对它发 `thread-follower-start-turn`。
  App 照常跑这一轮，界面里看得到，回复写进同一个 rollout，手机照旧从 rollout 读。
- **每个请求都要带 App 期望的 `version`**（owner-discovery 1、start-turn 2，表在 App 主进程代码里）；不带或不对，没有客户端认领，总线一律回 `no-client-found`。
- 总线也会问每个客户端「这个请求你能处理吗」（`client-discovery-request`），不回答它要等满超时：node 立刻回 `canHandle: false`。
- 只有 App 本次启动后加载过的会话有持有者。没加载的（刚新建的、App 启动后没打开过的）发送时 node 先让 App 打开它（`open codex://threads/<id>`：App 会到前台并切到这个对话；后台打开 App 不加载），最多等 30 秒等到持有者再发；还是没有就拒绝（`codex_desktop_not_loaded`，没有送达）。
- App 归档 / 恢复对话时把 rollout 在 `sessions/` 和 `archived_sessions/` 之间**移动**（inode 不变）：node 缓存的 `transcript.File` 发现原路径没了就 `Relocate` 到新位置，接着增量读；以前会一直读旧路径失败、给手机旧内容。
- 只有 App 自己的对话走这条路：rollout 第一行 `session_meta` 的 `originator` 是 `Codex Desktop`（`source` 为 `vscode` 是侧栏对话、`exec` 是后台运行，`subagent.thread_spawn` 是子 Agent），Codex CLI 写的是 `codex-tui` / `cli`。App 的对话标成可发送（`capabilities.send`），`agent_name` 是「Codex App」；其他工具借用 App 的 codex 跑出来的会话也写 `Codex Desktop`，但 App 侧栏不列：只有侧栏里的对话（`thread-project-assignments` 或 `projectless-thread-ids` 里有）才算。App 的子 Agent 仍仅查看并带 `parent_thread_id`，归到父对话的项目里、跑完算已看；CLI 的会话 `agent_name` 是「Codex CLI」，照旧。
- **没有父会话挂靠的子 Agent 不列**（#4745）：`session_meta` 标成子 Agent（同上，`thread_source: "subagent"` 或 `source.subagent`）的 rollout，既不在共享的 app-server 上、也不是 Codex App 侧栏父对话下的，不作为独立会话进 Agents，也就不推「完成了」、不显示完成未看。之前这类子线程（比如 skill 派出去审 MR 的 reviewer）每个都是一行，标题回落成 cwd 的目录名，跑完各推一次。它们做的事在父会话的记录里（SubAgentActivity）。app-server 上的子 Agent 照旧仅查看并带 `parent_thread_id`（授权卡要能答）。
- **程序开的一次性会话不列**（#2131）：`session_meta` 的 `source` 是 `exec`（`codex exec`、SDK、App 的后台运行），或 `originator` 是 `codex_exec` / `codex_sdk_*`（它们派生的子 Agent 保留这个 originator）的 rollout，不进 Agents、快照统计和 `agents_list`。这类会话是脚本批量开、跑完就结束的（实测一次评测各用一个临时 `CODEX_HOME` 开出几十个，开发机上两周内有两千多个），不按目录判断。已经在客户端里的那批，下一次拉 Codex 历史时随着「机器上已经没有了」一起移除。herdr 窗格里跑的会话走窗格那条路，不受影响。
- 列表像 App 的侧栏，状态和 herdr 同构（`docs/agent-states.md`）：App 记在 `<CODEX_HOME>/.codex-global-state.json` 里的侧栏状态——`electron-thread-read-state-v1` 的未读对话、`local-projects` 项目、`thread-project-assignments` / `projectless-thread-ids` 归属——文件变了就重读。按 App 的项目分组（没有项目的归「Codex 对话」），组内最新的在前；最近 3 天的对话：最后一轮在跑 → 工作中，跑完且 App 里仍未读 → `done`（完成未看），App 里看过 → `idle`（已看）。手机上看过照常也算已看。
- Codex 回复里新建对话的指令 `::created-thread{threadId="…"}` 换成链接 `jarvis://session/<机器>/<会话>`（App 里对话链接跳转用的格式：Android 点了打开那个对话；iOS 目前只认实时活动的 `jarvis://open?…`，聊天里点这种链接还不跳，要发版），手机上点一下打开那个对话。
- 还不支持：从手机在 App 里新建会话（App 的 `codex_app` MCP 管道校验对端进程，不绕）、steer 正在跑的一轮、审批。

## 从手机新建

2026-11-02 起，手机上的「新对话」选一个 Codex 启动器，就在那台电脑的 herdr 里新开一个标签跑 Codex，第一条消息就是它的第一条消息。
入口和 Claude 的一样（选机器、目录、启动器）；之后的事交给上面「守护进程上的会话」（0.163 的裸 `codex` 自动接入）或下面的终端路线。

- **哪些启动器是 Codex 的**：PATH 上有 `codex` 就多一个叫 `codex` 的启动器；`~/.zshrc` 里只跑一条 `codex` 命令的 alias（前面可以有
  `export A=1;`、`VAR=… codex`、`env` / `command` 这类包装，参数照原样，模型取 `-m` / `--model` / `-c model=…`），排在 Claude 的后面。
  启动器列表里它们带 `agent: "codex"`，App 的启动器页标「Codex」（`sessions.go` 的 `parseCodexAliases`）。不支持的：跑 Codex 的
  shell 函数（拒绝并说明，`codex_launcher`：函数只能在 shell 里跑，第一条消息就得经过 shell）、做了别的事的 alias（`cd x && codex`，不列出来）、TRAEX。
- **第一条消息原样过去**：Codex 收到第一条消息才建 thread，所以新建 Codex 必须带它（App 里必填；没带的回 `codex_needs_prompt`，
  老版本 App 走后台启动也是这个错，提示更新）。启动器的词和消息一起写进 node 目录下只有自己能读的临时文件（0600），窗格里打的只有
  `jarvisd codex-start <文件>`：它读出来、删掉文件，把启动器的词（前面的 `VAR=…` 进环境）、`--`、消息作为参数数组 `exec` 出去，
  消息不经过任何 shell——引号、换行、`$`、反引号、中文、几千字都原样到 Codex（`codex_start.go`）。启动器的词也不上屏、不进 shell 历史
  （alias 里可能带着 Key）。启动器带 `--remote` 时补上 `-C <目录>`（连到别的 app-server 时 TUI 不会自己带目录）。
- **告诉 herdr 这是 Codex**（#3454）：在 herdr 窗格里（Windows 除外），文件里还写着 `agent: "codex"`，`codex-start` 把 `HERDR_AGENT=codex` 设进它 exec 的环境，
  替换 shell 里继承来的值（只追加的话同一个名字有两份，getenv 和 herdr 都取第一份，也就是继承来的），包装成别的名字的 codex herdr 也认得出
  （为什么、herdr 哪个版本起认，见 `docs/launchers.md`「herdr 怎么认出启动器起的 Agent」）；接着已退出的 Codex（`resume <id>`）一样。
  敲进窗格的只是 `'<jarvisd>' codex-start '<文件>'`，不带前缀，所以不挑 shell。启动器前面的 `VAR=…` 照旧追加在继承来的环境后面
  （shell 里已有同名变量时生效的是 shell 里的；这些词没经过 shell 展开，`$HOME`、`~` 原样，不能拿来替换）。pi 从 herdr 恢复用的 `jarvisd tab-start` 共用这段代码，不设 `HERDR_AGENT`。
- **对上这次请求**：新建是同步的，node 等 Codex 的 thread 出现才回答，回答里就是这个 thread 的会话，所以 App 打开的就是 Agents 里
  那一条，不会多出一条「启动中」的占位再换成另一条。认法：这个目录下、启动之后才开始（`session_meta` 的时间）、第一条用户消息和发出去的
  一字不差的 rollout。等的这段时间这个窗格不参与「同目录最新 rollout」的猜测（`codexStarting`），免得先被认成目录里旧的 thread。
  路上遇到 Codex 自己的对话框照用户的意思答：信任这个目录（目录是用户选的）、跳过更新提示。
- **对不上**：node 最多等 60 秒（hub 100 秒、App 110 秒），超时回「Codex 已经在电脑上的新标签里打开了，但 60 秒内没认出它这次的会话……」，
  后面附上那个标签最后几行屏幕；启动器跑不起来（找不到命令）时 `jarvisd codex-start:` 开头的那行一出现就失败，不用等满。
  标签留在电脑上，不替用户关。
- **走哪条路**：0.163 的裸 `codex`（或不带下面这些参数的 alias）跑在共享守护进程上，几秒内自动接入，「会话信息」的「连接」是 app-server；
  带 `-c` / `--profile` / `--enable` / `--disable` / `--oss` / `--strict-config` / `--no-daemon` 的启动器在自己进程里跑，走终端路线（#214），
  「连接」那行写「用启动器「X」起的：它带了 -c，Codex 这时在自己的进程里跑，不在共享的 app-server 上」；0.153 没有守护进程，也是终端路线。
- **不提供的**：新建时选权限模式、思考强度（Codex 按启动器的参数和 `~/.codex/config.toml`；会话接上 app-server 以后能在对话里改思考强度，见「思考强度」）；从手机恢复已有的 Codex 会话（`codex_resume`，在电脑上 `codex resume`）。
- **测试**：`node/codex_start_test.go`（假 herdr：窗格里的命令只有 `codex-start <文件>`、文件里是启动器的词和消息、只有一条会话、路由说明、拒绝、超时和启动失败的报错、信任对话框、
  `codex-start` 子进程逐字节收到消息、`~/.zshrc` 里的 Codex alias）；真实 Codex：`scripts/test/test-codex.sh` 的 `TestLiveCodexStart`
  （0.163 经 `--remote` 挂在测试自己的 app-server 上、0.153、带 `-c` 的 0.163，各连跑多轮：消息带引号 / 换行 / `$` / 反引号 / 中文 / 长文本，
  原样进 rollout，一个窗格一条会话，路由对，手机再发一条也收到）；App：`MachinePagesTests.testNewCodexSessionStartsWithItsFirstMessage`。

## 提问和审批（结构化卡片）

2026-11-02 起，Agents 里的 Codex / TRAEX 提问和审批是和 Claude 一样的提问卡片、授权卡片，不再是通用的「终端在问你」。
Codex 在终端里没有能代答的 hook（见下面「调研」），node 自己读 rollout 和屏幕：

| 来源 | 怎么发现 | 卡片 | 怎么答 | 怎么算答上了 |
| --- | --- | --- | --- | --- |
| `request_user_input_async`（Default 模式；模型目录的 `experimental_supported_tools` 里有 `send_user_message_async` / `request_user_input_async` 才有，现在的 gpt-6 系都有） | rollout 里 `item_completed` 的 AgentMessage 带 `delivery: "async"` 和 `questions`（`title` + 字符串选项；item id 就是 call id）。调用马上回 `{"accepted":true}`，回合照常继续或结束 | 提问卡片（选项 + 其他回答） | 发一条用户消息，内容是 Codex 自己的客户端（desktop、0.163 的 TUI）用的回复信封：`<send_user_message_question_reply>[{"questionItemId":"[\"request_user_input_async\",\"<call id>\",<序号>]","question":…,"answer":…}]</send_user_message_question_reply>`，一行，JSON 不做 HTML 转义 | rollout 出现带这个 `questionItemId` 的用户消息（0.163 的 TUI 也凭它把题从面板上拿掉）；或者屏幕上它排进了「Queued follow-up inputs」 |
| `request_user_input`（Plan 模式；或 `features.default_mode_request_user_input`） | rollout 里 `function_call` name `request_user_input`，参数 `{questions:[{id, header, question, options:[{label, description}]}]}`；回合停在 TUI 的覆盖层上（「Question 1/2」） | 提问卡片（多题、标题、选项说明） | 按键：选项按数字（选中并进下一题，最后一题提交）；自己写的回答是光标移到「None of the above」、Tab 打开备注、粘贴、回车，Codex 记成 `["None of the above", "user_note: …"]` | 这个 call 的 `function_call_output` |
| 审批（命令、补丁、权限、MCP） | 不进 rollout：herdr 报 blocked 时读屏幕，认「Would you like to …?」、编号选项和「Press enter to confirm or esc to cancel」（选项文案在 `tui/src/bottom_pane/approval_overlay.rs`，0.153 / 0.163 一样） | 授权卡片：命令全文或改动、Codex 的理由，同意 / 总是同意（写明会放行什么；Codex 没给这个选项就没有）/ 拒绝（可写理由） | 选项按数字：同意 = Yes, proceed；总是同意 = 本会话那项（don't ask again … in this session / for these files）优先，没有就用前缀那项（commands that start with …）；拒绝 = No, continue without running it，没有这一项或写了理由时 = No, and tell Codex what to do differently（停下这一轮），理由随后作为下一条消息发进去 | 屏幕上这个审批（按 `rev`）消失 |

- async 提问只认 item，不认 `request_user_input_async` 的 `function_call`：模型偶尔给的参数 Codex 解析不了（比如把 `title` 写成了 `question`），
  调用被拒（output 是错误）、模型随即重试；被拒的那次没有 item。以前按 `function_call` 开卡，会留下一张永远答不掉的卡（2026-11-02 实测抓到）。
- 题目在电脑上答了（回复信封，或者任何别的用户消息：0.153 没有提问面板，用户就是直接打字回答）、回合被中断（`turn_aborted`）、Agent 退出，
  卡片都会消失。12 小时前的 async 提问不再拿出来（node 重启会重读 rollout）。
- TRAEX 共用 rollout 解析（`history_mutation` 里的 AgentMessage / UserMessage 也认）和屏幕解析，状态是 node 自己判断的；还没在真的 TRAEX 上测过。
- 实现：node 每秒扫一遍 live 的 Codex / TRAEX（`node/codexask.go`；rollout 只读新增部分，第一次读最后 8 MB），开着的卡片变了就整份发给 hub
  （`codex.interactions`）。管理者给每张卡建一个本地交互（`assistant/fleet_ix.go`）：新的推送，不在单子里的结束，同一会话的通用屏幕卡让位；
  答案经 `codex.answer` 交回 node，node 按键 / 输入之前重读屏幕和 rollout，错误直接回给答题的人（和屏幕卡一样同步）；
  没能输入（草稿、界面不对）时卡片留着，node 说 Codex 已经不问了（`prompt_changed`）时卡片消失。node 重连后重新列出的卡不再响铃。
  hub 给 Codex 会话建通用屏幕卡前多等 3 秒，让 node 的卡先到。推送标题是「Codex 在问你」「Codex 需要你授权」。
  async 提问的卡不代替终端上的对话框：它开着时 herdr 也报 blocked，这时 hub 只在屏幕上真读出别的对话框时才另建通用屏幕卡（光是输入框就不建）。
- 已经接入 app-server 的 thread（上面「守护进程上的会话」，`codex_remote.go` 里 `joined`）：审批和阻塞提问不读 TUI，由 app-server 直接发成卡片；
  async 提问照旧从 rollout 认，回答经 app-server 发出去。

### 电脑上处在各种界面时

答题前 node 先把 TUI 带回能作答的状态；带不回来就什么都不按，把原因告诉答题的人（错误码和发消息相同：`input_draft` / `dialog_open` / `prompt_changed`）。

| 电脑上 | 怎么办 |
| --- | --- |
| 输入框里有打了一半的字 | 拒绝作答，提示里带上那段草稿：Codex 的输入框没有可靠的「先存起来、答完放回去」（图片、粘贴块放不回去）。普通的发消息也一样拒绝——以前 herdr 的 `agent prompt` 会直接拼在草稿后面发出去 |
| 0.153 的对话记录（Ctrl+T 的 pager） | pager 盖住了后来的审批，herdr 却已经报 blocked：node 看到审批被 pager 盖住就按 `q` 关掉它（只读界面，不丢东西；最多 10 秒一次）。pager 里的 `esc` 是「编辑上一条消息」，绝不按。0.163 的 Ctrl+T 只是展开细节，输入框还在 |
| 0.163 的提问面板展开着（⇧←） | 这时打进去的字会成为这道题的「Other」答案：先 ⇧→ 收起，再发回复 |
| `/` 命令菜单、`@` 文件选择 | 只在输入框里有字时出现，按草稿处理。回答一律用括号粘贴（bracketed paste）输入，里面的 `@`、换行不会弹出选择、不会提前提交 |
| 往上翻了（herdr 滚动回看） | `pane read --source visible` 读到的是翻上去的内容：Codex 的屏幕一律读输出末尾（`--source recent`；pager 在备用屏幕上，读到的就是 pager 本身）。按键不受滚动影响，也不会把滚动拉回去 |
| 审批、覆盖层或别的选择框正开着时答 async 题 | 拒绝（`dialog_open`）：先处理审批卡片 |
| 覆盖层的备注框里有人在写 | 拒绝，什么都不按 |
| 在电脑上先答了 | 卡片随 rollout / 屏幕消失；手机随后的回答 node 找不到这张卡，回 `prompt_changed`，什么都没输入 |
| pane 不在前台、herdr 在别的 tab、窗口很小 | 读屏和按键都对 pane 本身，不受影响；窄窗口里标题、命令、选项会折行，解析时接回去 |
| 回复发出时这一轮还在跑 | 0.163 当场插进这一轮（steer）或排进队列，都算收下 |

herdr 在 Codex 有没答的 async 提问时报 blocked，于是 `agent prompt` 拒绝（`agent_blocked`）：Codex 的发消息遇到这个改为自己粘贴 + 回车（输入框已确认是空的）。

### 测试

- 单元测试：`screen/codex_test.go`（录下来的屏幕，0.153.4 / 0.163.0，`screen/testdata/codex-*`）、`transcript/codexask_test.go`（rollout 识别、两种 schema、
  部分回答、直接打字、旧 desktop 回复、TRAEX）、`node/codexask_test.go`（假 herdr 上每种界面状态的按键和输入、拒绝时什么都没按）、
  `hub/codexcards_test.go`（列表增删、答失败卡片回来、重连不响铃、推送标题）。
- 真实 Codex：`scripts/test/test-codex.sh`（`docs/testing.md`「Codex 联调」）在独立的 herdr 里起 Codex，让它提问、请求审批，每种界面状态各一个用例，连跑多轮。
- App：`CodexCardsTests`（mock `MOCK_CODEX=1`），浅色 / 深色截图。

## 还不支持

- 从手机恢复已有的 Codex 会话：只能新建（「从手机新建」）；外部 app-server 实例上的离线 / 归档历史只读，不会偷偷另起一个 app-server 续聊。
- 从手机新建 Codex 必须带第一条消息；跑 Codex 的 shell 函数、做了别的事的 alias、TRAEX 不能用来新建；新建时不能选权限模式和思考强度。
- 权限模式不能在手机上改；思考强度只有经 app-server 的会话能改（「思考强度」），终端路线的 Codex 和 TRAEX 不能。
- 带 `-c` 等参数的启动器新建的会话不在共享守护进程上，只能走终端路线（读屏幕、打字）。
- 认不出来的 Codex 对话框（不是上面「提问和审批」里的格式）照旧走通用的「终端等你处理」屏幕交互。
- 排队消息、子 Agent、后台任务这些 Claude transcript 才有的信息，Codex 会话里都是空的。

## TRAEX

TRAEX（`traex`，公司内部基于 Codex 的分支）2026-10-28 起支持，和 Codex 一样出现在 Agents 里（列表、对话、实时状态、发消息、终端、关闭），
标签「TRAEX」，输入框「给 TRAEX 发消息」。代码：`server/internal/node/traex.go`，rollout 解析和 Codex 共用 `transcript/codex.go`。

- **herdr 不认识它**：traex 窗格在 herdr 里是普通窗格（`agent_status: unknown`，不在 `agent list` 里），herdr 也没有自定义 Agent 的配置。
  node 自己找：`~/.trae/cli/thread-writer-locks/<会话 id>.owner.json` 写着正在写这个会话的 pid；pid 的环境变量里有 herdr 的
  `HERDR_PANE_ID`（Linux 读 `/proc/<pid>/environ`，macOS 用 `ps -E`），于是 窗格 → pid → 会话 是精确的，不靠目录猜。窗格信息来自
  `herdr api snapshot` 的 `panes`。
- **一个进程会写好几个 thread**：TRAEX 执行工具前，会把这一步交给审核模型做自动批准评估（guardian，`session_meta` 里
  `source: {"subagent": {"other": "guardian"}}`、`thread_source: subagent`、`session_id` 和 `forked_from_id` 都是主对话）。
  这个审核对话由同一个进程来写，也有自己的 owner 文件，而且比主对话的新。按「每个 pid 取最新的 owner 文件」认，App 里显示的就是审核对话
  （审核请求和评估结果的 JSON），和终端里看到的不一样（2026-11-01 用户反馈）。现在同一个 pid 取最新的非子 Agent thread；只有子 Agent 的就取它的主对话；
  `session_meta` 里没有这些字段的旧版本还按最新的 owner 文件（`traexOwners`）。
- **状态自己判断**：终端标题前面有盲文转圈字符（`terminal_title`，不是 stripped 的那个）或屏幕上有「esc to interrupt」= 工作中；
  屏幕上有「› 1.」这样的编号选择 = 等你（走「终端在问你」卡片）；其余报 `done`，hub 按看没看过算「完成未看 / 已看」。屏幕最多 2 秒读一次。
- **文件布局和 Codex 一样**，只是根目录是 `~/.trae/cli`：`sessions/YYYY/MM/DD/rollout-*.jsonl`、`session_index.jsonl`（线程名，做标题）。
- **rollout 是更新的格式**：用户消息和回复在 `history_mutation.display_completions[].item` 里（item 结构同 `item_completed`），
  工具调用仍在 `event_msg/item_completed`，同一个 item 可能两处都有（按 id 去重）；`history_mutation` 一行可能上百 KB 到 1 MB
  （压缩后带整个上下文），只解 `display_completions`。`task_started` / `task_complete` 记在 `Meta.TurnOpen`。
  `TerminalInteraction`（向运行中的命令轮询 / 输入）不显示。
- **发消息**：herdr 的 `agent prompt` 只认它识别的 Agent，对 traex 窗格报 not found，于是改成 `pane send-text` + 回车（`Node.prompt`）。
- 已退出的 TRAEX / Codex 会话从手机发消息会报错（不能用 claude 去 resume 它们的会话 id）。

## 调研：Agents 里的 Codex 改走 app-server、审批 hook（2026-11-02，只有结论，没实现）

读的是 codex-rs 源码 `rust-v0.153.4` / `rust-v0.163.0` 两个 tag，再加两个版本的二进制（`--help`、`app-server generate-json-schema`）。
「源码」指在代码里核实过，「推断」指没跑起来验证。

### B. 改走 app-server

- **TUI 能连 app-server，而且本来就是 app-server 的客户端**（源码）：TUI 只和 app-server 说话，进程内、本机守护进程或远端三选一
  （`tui/src/lib.rs`）。`codex`、`resume`、`fork` 都有 `--remote ws://|wss://|unix://`，不用 feature flag。守护进程是 `codex app-server --listen unix://`，
  socket 在 `$CODEX_HOME/app-server-control/app-server-control.sock`，上面跑的是 WebSocket，不是按行的 JSON：Persona 现在助手用的 stdio 客户端
  不能直接复用（`codex app-server proxy` 只是转字节），node 要一个走 unix socket 的 WebSocket 客户端。
- **0.163 默认就在守护进程上**（源码）：`daemon_auto_start` 是 Stable、默认开，裸 `codex` 自动启动或加入共享守护进程（`--no-daemon` 关掉）。
  带 `-c`、`--profile`、`--enable/--disable`、`--oss` 等参数时退回进程内——`docs/assistant.md` 里那种带 `-c model_provider=…` 的启动器起的会话不在守护进程上。
  0.153 只在 socket 已经在时自动连上，没人替你起守护进程。
- **多个客户端共用一个 thread**（源码，两个版本一样）：事件发给所有订阅了这个 thread 的连接；服务端发起的请求（命令 / 补丁审批、
  `item/permissions/requestApproval`、`item/tool/requestUserInput`、MCP elicitation）同一个 request id 发给所有人，先答的算数，后答的只记日志；
  答完后每个订阅者都收到 `serverRequest/resolved {threadId, requestId}`，TUI 收到就收起自己的提示框。第二个客户端用 `thread/resume` 订阅正在跑的
  thread，挂着的请求会补发给它。这正是「电脑上先答了手机要知道」需要的语义，而且不用读屏幕。这些都在不需要 `--experimental` 的稳定 schema 里，
  两个版本的审批、提问、elicitation 参数和 `serverRequest/resolved` 导出来一字不差（0.163 删了 `thread/rollback`，加了 `thread/attachment/*`）。
- **代价**：
  - 只覆盖跑在守护进程上的会话：0.153 要先有人起守护进程；带 `-c` 的启动器、`--no-daemon`、已经以纯 TUI 跑着的会话、TRAEX 都不在上面，
    rollout + 屏幕这条路还得留着。0.163 对同一个 thread 拒绝第二个写入进程（`rollout/src/writer_lock.rs`），已经在跑的会话也不能另起进程接管。
  - 守护进程继承启动它的那个进程的环境（源码）；推断：启动器的环境、herdr 的 `HERDR_PANE_ID` 会变成第一个起守护进程的窗格的，herdr 的
    SessionStart hook 也在守护进程里跑——用之前要先看 `herdr-agent-state.sh` 怎么认窗格。
  - app-server 和守护进程的命令都标着 `[experimental]`；0.163 的 `daemon update` 可能打断正在跑的工作。
- **顺带「从手机新建 Codex 会话」可行**：守护进程上 `thread/start`，再在 herdr 窗格里跑 `codex resume <id>`（0.163 自动连守护进程；否则
  `--remote unix:// -C <cwd>`），之后电脑和手机都能看到、都能答。推断：rollout 在第一条用户消息之后才写，空 thread 可能 resume 不了，
  要先发第一条 `turn/start` 再开窗格；更简单的是直接在窗格里 `codex "<第一条消息>"`，从 `thread/started` 认出 thread。
- **结论**：值得做，但只做 0.163、只管守护进程上的窗格，屏幕卡片留作其余情况的后备。**已做第一步**，见「守护进程上的会话」；
  第二步（从手机新建）没用 `thread/start`，直接在窗格里跑 `codex -- <第一条消息>`，见「从手机新建」。
- **和已有实现的关系**：`codex-app-server.md`（dd83e7b0）已经接入了提供 Unix / 回环 WebSocket 入口的外部实例，0.163 的共享守护进程就是这种入口；
  但 herdr 里活着的 Codex 窗格优先按终端会话处理（`externalCodex` 为假），不会去 join 它在守护进程上的 thread。下一步可以是：herdr 窗格里的
  0.163 Codex 先找守护进程上的 owner（`codexOwner`），有就 join，提问和审批走 app-server（两边谁先答都行，`serverRequest/resolved` 撤卡），
  没有再退回这里的读 rollout / 屏幕。已经 join 的 thread 这里已经跳过（上面「提问和审批」）。

### C. 有没有像 Claude PreToolUse 那样能拦审批的 hook

- **有：`PermissionRequest`，格式和 Claude 的一样**（源码），hook 引擎两个版本代码相同，`hooks` 在两个版本都是 Stable、默认开。配置在
  `$CODEX_HOME/hooks.json` 或 `config.toml` 的 `[hooks]`。事件：PreToolUse、PermissionRequest、PostToolUse、PreCompact、PostCompact、
  SessionStart、SessionEnd、UserPromptSubmit、SubagentStart、SubagentStop、Stop、Interrupt。
- `PermissionRequest` 在审批流程的最前面跑，先于 Guardian 和终端提示，覆盖 shell 命令（`Bash`）、apply_patch（`apply_patch`，也匹配
  `Write` / `Edit`）、`write_stdin`、MCP 工具（`mcp__server__tool`）、网络访问（`Bash`，描述是 `network-access <目标>`）、`request_permissions`。
  回答 `{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow"|"deny","message":…}}}`，或退出码 2 + stderr 理由
  表示拒绝；不支持 `updatedInput` / `updatedPermissions`；多个 hook 时有一个拒绝就拒绝。超时默认 600 秒、可配；hook 失败、超时或不表态，
  照常弹终端提示。输入里有 `session_id`（thread id）、`turn_id`、`cwd`、`transcript_path`，不用再猜是哪个会话。
- **限制**：
  - hook 在终端提示**之前**跑，不是同时：手机还没答时，电脑前的人看不到提示、没法先答，和这次要求的「电脑上先答了手机要感知到」冲突。
    只能给一个很短的超时（几秒到几十秒），超时后交回终端；那样手机晚到的答案就没用了。
  - 用户的 hook 要被信任才跑：`config.toml` 的 `hooks.state` 里记一个 `trusted_hash`，或者走 managed / system 配置，或者
    `--dangerously-bypass-hook-trust`（0.163 上这个参数还会关掉守护进程）。每台机器都要改 Codex 的配置。
  - 异步 hook 不能做决定。
  - `request_user_input` 不触发 `PermissionRequest`，只触发 `PreToolUse`（tool name `request_user_input`）。PreToolUse 只能「拒绝 + 理由」或
    「放行并改写输入」，不能纯放行。推断可用的变通：拿手机的答案当拒绝理由，模型会把它当工具输出读到，但终端上永远看不到这道题。`notify` 只有 `agent-turn-complete`。
- **结论**：审批可以用 `PermissionRequest` 接，两个版本、进程内和守护进程都行；但它和「电脑前的人能先答」二选一，这次按要求做的是读屏幕的卡片，
  电脑和手机谁先答都行。B 做了之后两边都能答，不需要这个 hook；在那之前，如果更看重「手机上答得快」，可以把它作为可选项（短超时）再评估。
