# 飞书里的 /agent

在飞书任何会话里（群聊、私聊、话题群的话题、普通群里的话题回复），**自己发**一条以 `/agent` 开头的消息，hub 立刻起一个 Agent 处理，
做完以我的身份回复那条消息（话题里就回在话题里），末尾带一行引用「> 🤖」。（2026-10-31 用户要求：把 herdr 里手动开的
「监听 /Agent …」做成系统功能；那种监听本身就在一个有上下文的会话里，这里的请求可能来自任何群，所以 hub 要先把上下文收集好再交给 Agent。）

代码：`server/internal/hub/lark_agent.go`；配置 `hub.json` 的 `lark_agent`。

## 什么算请求

- 自己的消息（`isMine`，真人发的，不是机器人、没撤回），去掉开头的 @人之后以前缀开头（默认 `/agent`，不分大小写），
  前缀后面是空格、换行、冒号或什么都没有（`/agents` 不算）。前缀后面的文字是要求，可以为空（看上下文和引用的消息自己判断要做什么）。
- **只认自己发的**：别人发 `/agent` 不会触发（Agent 用的是我的全部权限：飞书、邮件、各台电脑）。
- 只看实时事件（lark-connector `/agent/v1/events` 的 `message.upserted`，事件里带整条消息）；hub 启动前、10 分钟前的消息不处理，
  所以重启、补发不会把旧消息再答一遍。每条消息只处理一次（编辑、话题有新回复时 lark-connector 会重发同一条）。
- 以「🤖」结尾的消息跳过（Agent 的回复，本身也不以前缀开头）。

## 交给 Agent 的上下文（`larkAgentContext`）

这是和 herdr 里的监听最大的不同：请求可能来自任何地方，Agent 一开始什么都不知道，所以 hub 先收集好、放进第一条消息（`<jarvis-context>` 标签）：

| 内容 | 来源 |
| --- | --- |
| 位置：私聊 / 群聊 / 话题、会话名、feed_id、请求消息 id 和时间 | 知识库的会话 meta |
| 话题里的消息（请求在话题里时），否则请求之前的 40 条（`context_messages`） | lark-connector `messages/:id/thread`、`conversations/:feedId/messages?beforePosition=` |
| 请求引用的那条消息 | 消息的 `replyTo` |
| 请求、引用和最近 10 条消息里链接到的飞书文档（最多 3 个，每个 8000 字） | `documents/read?format=markdown` |
| 这个会话的档案、它关联的前 3 个项目的档案 | 知识库 `~/.jarvis/data` |

聊天记录每行「月-日 时:分 名字：文字」，请求那行标「← 请求」，总长 3 万字以内（留最新的）。不够的 Agent 自己查（lark_*、kb_*、读文档、看 Agent）。

## 怎么跑

- 和收件箱的 AI 分身一样是助手目录里的无头 claude（`assistant.send`），所以有助手的全部工具；启动器用 `lark_agent.launcher`
  （配最强的模型），不写就用助手的。对话出现在「Jarvis」tab 的历史里，可以点进去看它查了什么；跑完不推「完成」通知（结果就是飞书里的回复）。
- 收到请求先给那条消息点「OnIt」表情，回复后换成「DONE」。
- Agent 用 MCP 工具 `lark_agent_reply` 交答案（只认为请求启动的那个对话），hub 用 lark-connector `POST /messages/:id/replies` 发出去
  （`editorType: markdown`，话题里 `replyMode: thread`）。Agent 这一轮结束却没调它，hub 把它最后一段回复发出去；
  超过 `timeout_minutes`（默认 30）没答完，回一句「这次 N 分钟内没处理完」。
- 同一个话题里再发 `/agent`，接着用上次那个对话（`--resume`），它记得之前说过什么。
- 记录存在 settings 表的 `lark_agent.runs`（3 天）。

## 配置

```json
"lark_agent": {
  "enabled": true,
  "launcher": "<最强的启动器>",
  "prefix": "/agent",
  "timeout_minutes": 30,
  "context_messages": 40
}
```

## 还没做

- 文档里的评论以 `/agent` 开头：自己写的评论不进文档通知中心，要另外轮询文档评论，下一步做。
- 别人发的 `/agent`（替别人答）：要先想清楚权限（lark-connector 替身模式那样的访客模式），现在一律不认。
