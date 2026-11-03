# 连接器协议（v2）

一个连接器跑在自己的程序里时，hub 和它之间说的话。第一方的 `jarvisd-telegram` 用它；第三方写连接器也照这份来。
为什么这样设计、和在 hub 里跑的连接器什么关系，见 `docs/connectors.md`「二 · 4」。代码：程序一侧 `server/internal/connector/serve`，
hub 一侧 `server/internal/connector/proc`，一致性测试 `server/internal/connector/conformance`。

## 传输

- 一行一条 JSON（`internal/rpc`，和 hub↔node 是同一套）：
  - 请求 `{"id":"h1","method":"status","params":{}}`
  - 回应 `{"id":"h1","result":{…}}` 或 `{"id":"h1","error":{"message":"…","code":"…"}}`
  - 单向事件 `{"event":"changed","params":{"scope":"inbox"}}`
- 现在只有 stdin / stdout：hub 起程序，程序读 stdin、写 stdout；stderr 是程序的日志，hub 收着（**不写正文、号码、token**）。
  hub 关掉 stdin 时程序退出。常驻服务的 WebSocket 传输等第一个外部连接器再做（`docs/connectors.md` 拍板 3）。
- 字段一律 snake_case；时间是 RFC 3339；没有的字段可以不写。

## 握手

1. hub 发 `init`：`{"protocol": 2, "config": {…}}`。`config` 是 hub.json 里这个连接器的那一节（可能还有 hub 运行时补的，比如用户时区）。
2. 程序回清单：

   ```json
   {"protocol": 2, "id": "example", "name": "Example", "capabilities": ["status", "tools", "inbox", "archive", "login"],
    "hosts": ["example.com"], "agent_tools": ["example_read"], "poll_seconds": 600, "kinds": ["dm", "mention"]}
   ```

   - `protocol` 不一致：程序回错误 `wrong_protocol`，或者 hub 看到清单里的版本不对就停掉它，设置页写明原因。
   - `id` 必须是 hub 注册它用的那个。
3. hub 注册时自己也有一份静态的清单（不依赖程序跑过：关着的连接器也报得出能力、能登录）。程序报的和它不一致时 hub 记日志，以 hub 的为准。

清单的字段：

| 字段 | 意思 |
| --- | --- |
| `capabilities` | `status`、`tools`、`actions`（只在 hub 里的连接器有，见下）、`events`、`inbox`、`archive`、`login`、`options`；hub 只调声明了的 |
| `hosts` | 链接所在的主机名（App 认链接） |
| `agent_tools` | 哪些工具也给编程 Agent，按名字的通配（不写就只给助手） |
| `poll_seconds` | 多久问一次 `archive.changes`，0 用 hub 的默认 |
| `kinds` | 收件箱条目的种类；hub 收到没声明的记一行日志 |

## hub 调程序

| 方法 | 参数 → 结果 | 说明 |
| --- | --- | --- |
| `status` | `{}` → `{state, account?, via?, error?, guide?, last_sync?, last_event?}` | 从内存回答，不联网（hub 每 30 秒问一次）。`state`：`ok`、`not_configured`、`not_signed_in`、`no_permission`、`rate_limited`、`error` |
| `tools.list` | `{}` → `{tools: [{name, description, input_schema}]}` | 只读工具，名字都以 `<id>_` 开头 |
| `tools.call` | `{name, args}` → `{text}` | |
| `inbox.collect` | `{}` → `{things: [InboxThing]}` | 从内存回答 |
| `inbox.reply` | `{thread, item_id?, last?, text}` → `{}` | 用户在手机上按了发送（发送本身就是确认） |
| `archive.changes` | `{cursor, limit}` → `{batches: [{chat, messages}], next, more}` | 游标是程序自己的，hub 原样存、原样还；第一次是 `""` |
| `login.step` | `{step, values}` → `Prompt` | 分步登录；`step` 为 `""` 从头开始 |
| `logout` | `{}` → `{}` | |

`InboxThing`：`thread`（必填，这件事的稳定键）、`kind`、`label`、`title`、`sender`、`preview`、`context`、`at`、`last`（最新一步的标记，变了就算新动静）、
`url`、`open_url`、`reply_to`、`can_reply`、`tier`（`action` / `fyi` / `noise`，空的交给模型）、`why`、`done`（`read` / `replied`：这件事结束了）、
`read`、`tags: [{key, value, name}]`（`repo` / `chat` / `sender` 能被用户静音）。

`messages` 里每条：`id`、`position`、`sender_id`、`sender_name`、`mine`、`type`、`text`、`at`、`media`、
`sender_handles: [{kind, value}]`（身份线索，原值；hub 只存它的 HMAC，`docs/knowledge.md`「身份线索」）。

`Prompt`：`step`、`fields: [{name, label, secret?, placeholder?}]`、`hint`、`error`（用户能改的错，同一步再问一次）、`done`、`account`。

## 程序调 hub

| | 参数 → 结果 | 说明 |
| --- | --- | --- |
| `secrets.load` | `{}` → `{data}` | 这个连接器自己的密钥（会话、token），hub 加密存在数据库；没有时 `data` 为空 |
| `secrets.save` | `{data}` → `{}` | |
| `secrets.delete` | `{}` → `{}` | |
| 事件 `changed` | `{scope}` | `chats`、`items`、`inbox`：有新东西，hub 尽快来取；不认识的 scope 被丢掉并记日志 |

## 错误

`error.code`：`not_initialized`（`init` 之前调了别的）、`no_capability`（没声明的能力）、`unknown_method`、`wrong_protocol`。
其余错误只有 `message`，是给人看的一句话，**不能带密钥和正文**。

## 硬性规定

- **写操作不在协议里**：程序不提供 `actions`。要写（发消息、评论）就把连接器做进 hub，写操作做成工具、过 `connector_gate` 的闸门，卡片写明发给谁、全文。
- 密钥只能存 `secrets.*`；不写本地文件，不写日志。
- 日志、错误里不出现正文、号码、token。
- `status`、`inbox.collect` 从内存回答，不联网；自己的轮询放在程序自己的循环里，有新东西发 `changed`。

## 版本

现在是 2。改了字段的意思或删了字段就加 1；只加可选的字段不加。hub 只接受和自己同一版本的程序（第一方程序和 jarvisd 同一次构建，必然一致）。

## 一致性测试

`conformance.Check(t, c)` 把一个连接器放在协议的程序一侧（`serve.Serve`，经一对管道），对清单声明的每个能力既直接调、也经协议调，
两边的结果编成 JSON 必须一字不差：字段在线上丢了、类型变了都会挂。GitHub、信息、Telegram 的测试里都跑它；`TestWireTypesAreTagged` 检查
线上的每个字段都有明确的 snake_case 名字。第三方的程序可以照着 `serve.Serve` 的接法，用同一份测试检查自己。
