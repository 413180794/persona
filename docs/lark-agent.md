# 飞书里的 /agent

> **2026-11-04 起（#1520，插件第 3 步 3c）**：经飞书网关接飞书的整条链路（App 的 `/api/v1/lark/…`、收件箱和知识库的飞书来源、公司圈、
> 助手的 `ol` 闸门和凭据、/agent、个人 Agent）不在开源仓库里了，由接飞书的插件提供（`docs/extensions.md`，`docs/plan-extensions.md`「第 3 步」）。
> 下文描述的行为由插件照原样提供；文中提到的 hub 源文件（`hub/lark*.go`、`internal/lark` 的客户端……）已删。开源版只留官方 lark-cli、飞书机器人、飞书邮箱。

在飞书任何会话里（群聊、私聊、话题群的话题、普通群里的话题回复）或文档评论里，**自己发**一条以 `/agent` 开头的消息，hub 立刻起一个 Agent 处理，
做完以我的身份回复那条消息（话题里就回在话题里），末尾带一行引用「> 🤖 AI 代答，仅供参考」（插件设置 `agent_reply_suffix`，接飞书的扩展 0.11.1 起的默认值，#866；Agent 自己写了这一行会先去掉，只留一行）。（2026-10-31 用户要求：把 herdr 里手动开的
「监听 /Agent …」做成系统功能；那种监听本身就在一个有上下文的会话里，这里的请求可能来自任何群，所以 hub 要先把上下文收集好再交给 Agent。）

代码在接飞书的插件里（原来是 hub 的 lark_agent.go）；配置是插件的 `agent`、`agent_launcher`、`agent_machine`（原来 `hub.json` 的 `lark_agent`）。

## 什么算请求

- 自己的消息（`isMine`，真人发的，不是机器人、没撤回），去掉开头的 @人之后以前缀开头（默认 `/agent`，不分大小写），
  前缀后面是空格、换行、冒号或什么都没有（`/agents` 不算）。前缀后面的文字是要求，可以为空（看上下文和引用的消息自己判断要做什么）。
- **只认自己发的**：别人发 `/agent` 不会触发（Agent 用的是我的全部权限：飞书、邮件、各台电脑）。
- 只看实时事件（飞书网关 `/agent/v1/events` 的 `message.upserted`，事件里带整条消息）；hub 启动前、10 分钟前的消息不处理，
  所以重启、补发不会把旧消息再答一遍。每条消息只处理一次（编辑、话题有新回复时飞书网关会重发同一条）。
- 以当前后缀、默认后缀「🤖 AI 代答，仅供参考」或旧的「🤖」结尾的消息跳过（Agent 的回复，本身也不以前缀开头）；只是在请求里引用了这几个字的照常处理。

## 交给 Agent 的上下文（`larkAgentContext`）

这是和 herdr 里的监听最大的不同：请求可能来自任何地方，Agent 一开始什么都不知道，所以 hub 先收集好、放进第一条消息（`<jarvis-context>` 标签）：

| 内容 | 来源 |
| --- | --- |
| 位置：私聊 / 群聊 / 话题、会话名、feed_id、请求消息 id 和时间 | 知识库的会话 meta |
| 话题里的消息（请求在话题里时），否则请求之前的 40 条（`context_messages`） | 飞书网关 `messages/:id/thread`、`conversations/:feedId/messages?beforePosition=` |
| 请求引用的那条消息 | 消息的 `replyTo` |
| 请求、引用和最近 10 条消息里链接到的飞书文档（最多 3 个，每个 8000 字） | `documents/read?format=markdown` |
| 这个会话的档案、它关联的前 3 个项目的档案 | 知识库 `~/.persona/data` |

聊天记录每行「月-日 时:分 名字：文字」，请求那行标「← 请求」，总长 3 万字以内（留最新的）。不够的 Agent 自己查（lark_*、kb_*、读文档、看 Agent）。

## 在 App 里怎么显示（#876）

交给 Agent 的第一条消息里，**只有末尾、所有 `<jarvis-context>` 标签外面的那句是用户写的**（`/agent` 后面的要求，没写就是前缀本身）；
给 Agent 的说明（`kind="instructions"`）和上面表里的每一项上下文都在标签里。网页、iOS / Mac、Android 的气泡只显示那句，
标签收成气泡旁一行「上下文 · N 项」，默认折叠；展开是只有标题的列表，每项再点开看正文（有链接的可以打开）；手表显示「〔上下文 N 项〕」。

- 2026-11-03 之前的对话是旧格式（说明、`## 请求`、`## 上下文`、`## 怎么做` 都在标签外）：transcript 解析时（`transcript/contexts.go` 的 `legacyLarkAgent`）改写成新格式，旧会话也这样显示。iOS 的 `ChatCache` 因此升到 `chats-v6`。
- 上下文常有几万字，分页时用户消息要截到 24KB：截断只削减各块的正文（`lightContexts`），用户写的那句和每块的标题保持完整，不会截在标签中间。

## 怎么跑

- 和收件箱的 AI 分身一样是助手目录里的无头 Agent（`assistant.send`），所以有助手的全部工具；当前模型在 **设置 › 模型配置 › 飞书 /agent** 里统一选择（`docs/model-config.md`）。没有用途覆盖时保留 `lark_agent.launcher` 的旧默认，不写就用助手的。对话出现在「Persona」tab 的历史里，可以点进去看它查了什么；跑完不推「完成」通知（结果就是飞书里的回复）。
- 收到请求先给那条消息点「OnIt」表情，回复后换成「DONE」。
- Agent 用 MCP 工具 `lark_agent_reply` 交答案（只认为请求启动的那个对话），hub 用飞书网关 `POST /messages/:id/replies` 发出去
  （`editorType: markdown`，话题里 `replyMode: thread`）。Agent 第一次回复前这一轮结束却没调它，hub 把它最后一段回复发出去。
- **长任务和后续进展（#3192）**：`lark_agent_reply` 在请求记录的保留期（3 天）里可以多次调用。第一次照上面的规则回原消息；
  之后的每一次（进展、最终结果）都回在以请求为根的话题里（请求本来就在话题里就回在那个话题里），不在群里刷屏；
  飞书网关拒绝在那里开话题（`REPLY_IN_THREAD_*`）时退回引用原消息回复。过了 30 分钟还没回复，不再停掉：
  在原消息下以话题回一句「正在工作中…」，之后的回复都进这个话题。之前「这次 N 分钟内没处理完，先停在这里」的截停去掉了。
  第一次回复以后，一轮结束没调工具就什么都不发（要不要发、发什么由 Agent 决定）。提示词要它只发有意义的进展和结果。
- **派出去的任务**：插件开对话时带 `report_tasks: true`。这段对话用 `task_plan` / `agent_start` 派出的任务，和主对话的
  「安静的任务」一样（`docs/unified-entry.md`）：不另外推送，做完、在等用户、被打断、没开起来都作为任务通知（`prompts/assistant/task_notice.md`
  的 `Reporting`）发进这段 /agent 对话（在忙就排在这一轮之后）；它再用 `lark_agent_reply` 把结果发到话题里。
  登记存在 settings 表的 `assistant.task_reporters`（14 天），hub 重启不丢。
- **停下来（#3308）**：在请求的话题里，或者直接引用请求（或它的回复）发 `/stop`（也认 `/停止`、`/停`，前面可以带 @），就停掉这次请求，
  刚发出、还在读上下文时也能停。只认我自己发的（和 `/agent` 一样，请求只能是我发的，所以发起人就是我）；别人发的不理，也不回「无权」（以我的名义回同事这句话很怪）。
  插件调 `POST /api/v1/assistant/conversations/{id}/stop`：hub 中断这段对话正在跑的一轮、它派出去还在跑的任务的 Agent（等用户回答的、没开始的计划卡不动，
  那里不经用户什么都不会跑），去掉排着的任务通知，之后它的任务通知直接丢掉（不改发主对话），直到用户再给这段对话发消息（在 App 里接着聊、话题里再发 `/agent` 接着这段对话）。
  请求记成 `stopped`，不再跟踪、不再回复（`lark_agent_reply` 也不收），去掉「OnIt」；在 `/stop` 那条下面回「已停止」并列出停掉了什么（这次的处理、派出去的任务名），
  已经没在做的回「已经结束了」。都带页脚，规则同回复。「正在工作中」那句末尾写了怎么停；请求刚开始不另发提示，免得每次都多一条。
  在 App / 网页里用停止按钮停这段对话（`POST /api/v1/sessions/{mid}/{sid}/interrupt`）：hub 告诉助手（`session.user_stopped`），它照样停掉派出的任务、记成被 App 停的；
  插件每轮扫描读 `GET /api/v1/assistant/reporters` 看到后把请求记成 `stopped`，在话题里说一句「已在 Persona 里停止」，不再把半截的最后一段发出去。
  老 hub 没有 `…/stop`（404）时，插件退回只中断这段对话。文档评论里的 `/agent` 还不能这样停。
- 同一个话题里再发 `/agent`，接着用上次那个对话（`--resume`），它记得之前说过什么。
- 提示词在插件里（原来是 hub 的 prompts/lark，`chat_agent.md` / `doc_agent.md` 和对应的 `*_context.md`）；要求它**不替我做承诺**：回复是以我的名义发的，只写已经做完的事，没做的写成建议。（2026-10-31 第一次真实使用，
  它在回复里写了「我这边下掉这个旧泳道的 orchestrator」，其实没做。）
- 记录存在 settings 表的 `lark_agent.runs`（3 天）。

## 配置

```json
"lark_agent": {
  "enabled": true,
  "launcher": "<最强的启动器>",
  "prefix": "/agent",
  "context_messages": 40
}
```

## 文档评论里的 /agent（插件里；原来是 hub 的 lark_agent_docs.go）

- 自己写的评论没有实时事件，也不进文档通知中心。写评论时要打开文档，文档会排到飞书「最近」的最前面，
  所以 hub 每 20 秒读一次 `documents/recent`：最近 15 分钟有动静的文档（每次最多 5 个）读它们的评论（多维表格走 `documents/base/comments`）。
- 自己的评论回复（飞书网关的 `mine`，作者 id 等于自己也算），去掉开头的 @人以后以前缀开头、10 分钟内、hub 启动后写的，就是请求；按评论回复的 id 只处理一次。
- 上下文：文档类型、标题、链接、评论 id；评论划选的原文；这条评论里到请求为止的讨论；文档正文（Markdown，2 万字以内）。
- 回复在同一条评论里（`documents/comments/:id/replies`）。评论是纯文本，Agent 写的 Markdown 符号会先去掉（`plainText`），末尾加一行「🤖 AI 代答，仅供参考」（纯文本，不带引用符号）；以它或旧的「🤖」结尾的评论回复不当请求。
  同一条评论里再写 `/agent` 接着上次的对话。Agent 不许改文档正文，要改就在回复里给写法。
- 前提：写评论会让文档的 activityAt 变新（用户实测前是推断）。最晚约 20 秒内接到请求。

## 插件接管（#1484）

经飞书网关接飞书的插件启用后，/agent 由插件做：hub 自己的实时事件和 /agent 都停下，`lark_agent_reply` 让给插件的同名连接器工具（多一个参数 `request`：请求的消息 / 评论 id）。
插件经 hub 的公开接口：`POST /api/v1/assistant/conversations` 带 `purpose: "lark.agent"` 和配置的机器（`machine_id`）开对话，`POST /api/v1/sessions/{m}/{s}/messages` 接着同一个话题，
`GET /api/v1/kb/chats/{id}/context` 取知识库对这个会话 / 项目知道的。配置在插件里（`agent`、`agent_launcher`、`agent_machine`）；hub.json 的 `lark_agent` 不再生效。

## 还没做

（想在飞书里随时和 Persona 主对话聊、在飞书里点卡片回答提问：那是飞书机器人，`docs/bots.md`。）

- Someone else's `/agent` (answering for others) is never taken. Answering others goes through guest conversations instead (`docs/assistant.md` ("Guest conversations"), #5195): a Lark extension's auto replies, off by default and turned on in its settings, with only `lark_agent_reply`.
