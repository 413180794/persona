# Codex 支持

herdr 里跑的 Codex（`codex` CLI）和 Claude 一样出现在「开发」tab：列表、对话、实时状态、发消息、终端、关闭都能用。
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

`response_item` 是同样内容的模型线格式，跳过。SubAgentActivity、Extension（生图）、CollabAgentToolCall 暂不显示。

## 还不支持

- 从手机新建 Codex 会话（启动器只认 claude 系列），离线（已退出）的 Codex 会话也不进列表。
- Codex 没有 Claude 那样的 hook 代答：它在终端里等确认时（herdr 状态 blocked）走通用的「终端等你处理」屏幕交互。
- 排队消息、子 Agent、后台任务这些 Claude transcript 才有的信息，Codex 会话里都是空的。
