# Codex 支持

herdr 里跑的 Codex（`codex` CLI）和 Claude 一样出现在「Agents」tab：列表、对话、实时状态、发消息、终端、关闭都能用。
2026-10-26 起支持，`server/internal/node/codex.go` + `server/internal/transcript/codex.go`。

## 识别

- herdr 把 Codex 窗格报成 `agent: "codex"`。装了 herdr 的 codex 集成（`~/.codex/herdr-agent-state.sh`，挂在
  `~/.codex/hooks.json` 的 SessionStart）时 `agent_session.value` 就是 Codex 的会话 id。
- 集成装之前就启动的 Codex 没有会话 id：取同一工作目录下最新、还没被别的窗格认领的 rollout（`codexSession`）。
  同目录开多个 Codex 时可能认错，重启一下 Codex 就会带上 id。
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

`response_item` 是同样内容的模型线格式，跳过。Extension 里的 `web.search` 显示成 `WebSearch`（搜索，输入是查询词）/ `WebFetch`（`action.type=openPage`，输入是网址），结果每条「标题  网址」；SubAgentActivity、其他 Extension（生图）、CollabAgentToolCall 暂不显示。

## 还不支持

- 从手机新建 Codex 会话（启动器只认 claude 系列），离线（已退出）的 Codex 会话也不进列表。
- Codex 没有 Claude 那样的 hook 代答：它在终端里等确认时（herdr 状态 blocked）走通用的「终端等你处理」屏幕交互。
- 排队消息、子 Agent、后台任务这些 Claude transcript 才有的信息，Codex 会话里都是空的。

## TRAEX

TRAEX（`traex`，公司内部基于 Codex 的分支）2026-10-28 起支持，和 Codex 一样出现在 Agents 里（列表、对话、实时状态、发消息、终端、关闭），
标签「TRAEX」，输入框「给 TRAEX 发消息」。代码：`server/internal/node/traex.go`，rollout 解析和 Codex 共用 `transcript/codex.go`。

- **herdr 不认识它**：traex 窗格在 herdr 里是普通窗格（`agent_status: unknown`，不在 `agent list` 里），herdr 也没有自定义 Agent 的配置。
  node 自己找：`~/.trae/cli/thread-writer-locks/<会话 id>.owner.json` 写着正在写这个会话的 pid；pid 的环境变量里有 herdr 的
  `HERDR_PANE_ID`（Linux 读 `/proc/<pid>/environ`，macOS 用 `ps -E`），于是 窗格 → pid → 会话 是精确的，不靠目录猜。窗格信息来自
  `herdr api snapshot` 的 `panes`。
- **状态自己判断**：终端标题前面有盲文转圈字符（`terminal_title`，不是 stripped 的那个）或屏幕上有「esc to interrupt」= 工作中；
  屏幕上有「› 1.」这样的编号选择 = 等你（走「终端在问你」卡片）；其余报 `done`，hub 按看没看过算「完成未看 / 已看」。屏幕最多 2 秒读一次。
- **文件布局和 Codex 一样**，只是根目录是 `~/.trae/cli`：`sessions/YYYY/MM/DD/rollout-*.jsonl`、`session_index.jsonl`（线程名，做标题）。
- **rollout 是更新的格式**：用户消息和回复在 `history_mutation.display_completions[].item` 里（item 结构同 `item_completed`），
  工具调用仍在 `event_msg/item_completed`，同一个 item 可能两处都有（按 id 去重）；`history_mutation` 一行可能上百 KB 到 1 MB
  （压缩后带整个上下文），只解 `display_completions`。`task_started` / `task_complete` 记在 `Meta.TurnOpen`。
  `TerminalInteraction`（向运行中的命令轮询 / 输入）不显示。
- **发消息**：herdr 的 `agent prompt` 只认它识别的 Agent，对 traex 窗格报 not found，于是改成 `pane send-text` + 回车（`Node.prompt`）。
- 已退出的 TRAEX / Codex 会话从手机发消息会报错（不能用 claude 去 resume 它们的会话 id）。
