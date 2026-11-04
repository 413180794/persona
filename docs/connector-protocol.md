# 连接器协议（v2）

一个连接器跑在自己的程序里时，hub 和它之间说的话。第一方的 `jarvisd-telegram` 用它；第三方写连接器也照这份来。
为什么这样设计、和在 hub 里跑的连接器什么关系，见 `docs/connectors.md`「二 · 4」。代码：程序一侧 `server/internal/connector/serve`，
hub 一侧 `server/internal/connector/proc`，一致性测试 `server/internal/connector/conformance`。

**它现在是插件协议的 `connector` 命名空间**（#1101，`docs/plugins.md`「进程插件协议」）：连接器程序是一个贡献了 `connectors` 扩展点的插件，
hub 用插件协议的 `initialize` 打开它，下面每个方法名前加 `connector.`（`status` → `connector.status`），参数里多一个 `contribution`（连接器的 id），
参数、结果、硬性规定一个都不变。程序调 hub 的 `secrets.*` 和事件 `changed` 也一样加前缀、带 `contribution`。进程什么时候起、怎么停、
怎么重启、配置和用户填的密钥怎么给，都是插件底座的事（`docs/plugins.md`）。

`connector/serve` 同时认两种开头：插件协议的 `initialize` + `connector.*`（hub 现在只用这种），和下面原来的 `init` + 不带前缀的方法名（单独的连接器协议，
保留一个发布周期，下一次改协议时删）。Go 写的连接器程序用 `serve.MainWith`，它在插件开头里还能拿到用户填的密钥（`Init.Declared`）和数据目录。

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
   {"protocol": 2, "id": "example", "name": "Example", "capabilities": ["status", "tools", "actions", "inbox", "archive", "login"],
    "hosts": ["example.com"], "agent_tools": ["example_read"], "unlisted": ["example_send"], "poll_seconds": 600, "kinds": ["dm", "mention"]}
   ```

   - `protocol` 不一致：程序回错误 `wrong_protocol`，或者 hub 看到清单里的版本不对就停掉它，设置页写明原因。
   - `id` 必须是 hub 注册它用的那个。
3. hub 注册时自己也有一份静态的清单（不依赖程序跑过：关着的连接器也报得出能力、能登录）。程序报的和它不一致时 hub 记日志，以 hub 的为准。

清单的字段：

| 字段 | 意思 |
| --- | --- |
| `capabilities` | `status`、`tools`、`actions`（写操作，见下）、`events`、`inbox`、`archive`、`items`（知识库的条目：文档、会议、日程）、`login`、`options`、`threads`（收件箱的会话在 App 里打开成会话页）；hub 只调声明了的 |
| `hosts` | 链接所在的主机名（App 认链接） |
| `agent_tools` | 哪些工具也给编程 Agent，按名字的通配（不写就只给助手） |
| `unlisted` | 哪些工具和写操作不进模型的工具列表，按名字的通配：连接器自己的前端按名字调（Telegram 的 `tg` 命令行）。不是权限：按名字调照常能调，写的照常过闸门，Agent 能不能调照旧看 `agent_tools` |
| `poll_seconds` | 多久问一次 `archive.changes`，0 用 hub 的默认 |
| `kinds` | 收件箱条目的种类；hub 收到没声明的记一行日志 |

## hub 调程序

| 方法 | 参数 → 结果 | 说明 |
| --- | --- | --- |
| `status` | `{}` → `{state, account?, via?, error?, guide?, last_sync?, last_event?}` | 从内存回答，不联网（hub 每 30 秒问一次）。`state`：`ok`、`not_configured`、`not_signed_in`、`no_permission`、`rate_limited`、`error` |
| `tools.list` | `{}` → `{tools: [{name, description, input_schema}]}` | 只读工具，名字都以 `<id>_` 开头 |
| `tools.call` | `{name, args}` → `{text}` | |
| `thread.page` | `{thread, before?, limit}` → `{chat, messages, before?}` | 收件箱一个会话的一页消息，旧的在前；`before` 为空是最新一页，往前翻带上一页给的 `before`；`limit` 最多 50。字段见 `connector/threads.go`（`ThreadChat`、`ThreadMessage`、`ThreadMedia`…）。`chat.reply` 是能在这里回复的 action，参数 `{chat, text, reply_to}` |
| `thread.media` | `{ref}` → `{mime, data}` | 页面里头像和媒体的 ref 对应的字节（base64），最多 4 MB。一个 ref 永远指同一份字节，hub 存盘、App 永久缓存；会变的（头像）把版本写进 ref |
| `actions.list` | `{}` → `{tools: [{name, description, input_schema}]}` | 写操作，名字都以 `<id>_` 开头 |
| `actions.describe` | `{name, args}` → `{action: {name, visible}, summary}` | 手机卡片上的话：做什么、全文；`visible` 是谁会看到。不联网。出错就是参数不对：hub 把错误回给调用方，不弹卡片 |
| `actions.do` | `{name, args, request_id}` → `{text}` | **只在 hub 的闸门放行之后调**，`args` 就是卡片用的那一份。`request_id` 是 hub 为这一次生成的：同一个 id 程序只做一次（再来就回第一次的结果）；hub 超时后也不重发 |
| `inbox.collect` | `{}` → `{things: [InboxThing]}` | 从内存回答 |
| `inbox.reply` | `{thread, item_id?, last?, text, client_id?, changes?}` → `{}` | 用户在手机上按了发送（发送本身就是确认）。`client_id`：App 这次发送的 id，重试只发一次（没有时连接器自己派生）；`changes: [{ref, field, value}]`：先改文档某一行的状态列（文档条目的「改状态」、AI 分身的提议），这时 `text` 可以为空 |
| `archive.changes` | `{cursor, limit}` → `{batches: [{chat, messages}], next, more}` | 游标是程序自己的，hub 原样存、原样还；第一次是 `""` |
| `archive.media` | `{ref}` → `{mime, data}` | 归档消息 `media` 里的一个 ref 对应的字节（base64），最多 20 MB；知识库把它存在会话的附件里。可选：没有时那一行只写 `[附件]` |
| `items.list` | `{}` → `{items: [{kind, id, title, url?, at?, where?}]}` | 知识库的条目（`kind`：`docs` / `meetings` / `events`）；声明了 `items` 才调 |
| `items.doc_title` | `{url}` → `{title}` | 会话里出现的文档链接的标题，读不到回 `""` |
| `login.step` | `{step, values}` → `Prompt` | 分步登录；`step` 为 `""` 从头开始 |
| `logout` | `{}` → `{}` | |

`InboxThing`：`thread`（必填，这件事的稳定键）、`kind`、`label`、`title`、`sender`、`preview`、`context`、`at`、`last`（最新一步的标记，变了就算新动静）、
`url`、`open_url`、`reply_to`、`can_reply`、`tier`（`action` / `fyi` / `noise`，空的交给模型）、`why`、`done`（`read` / `replied`：这件事结束了）、
`read`、`tags: [{key, value, name}]`（`repo` / `chat` / `sender` 能被用户静音）、`extra`（见下）。

**迁出 hub 的连接器**（`connector.MigratableConnectors`，现在是 `lark`，`docs/plugins.md`「App 接口」）：它的 thing 可以带 `extra`
（`source`、`account`、`account_name`、`legacy_id`、`avatar`、`count`、`started_at`、`rule_tier`，以及原来条目的块之一 `lark` / `doc` / `moment`）。
hub 只对这些连接器读它：条目保持 hub 原来自己做的样子（那个块，没有通用的 `connector` 块），App 照旧打开飞书聊天、文档的行、帖子；thread 由块算出
（`chat:<feed>`，非主账号 `chat:<账号>:<feed>`；`doc:<notice>`；`moment:<post>`），所以迁出之前 hub 自己做的条目接着用、不重复出一条；新条目的 id
用 `legacy_id`。同一步（`last` 没变）块变了（读到的位置、文档重读后的行）也更新条目。这些条目的回复和改状态经 `inbox.reply` 交给连接器，
`client_id` 和 hub 原来自己发时用的一样（`hub/inbox_migrate.go`）。

`messages` 里每条：`id`、`position`、`sender_id`、`sender_name`、`mine`、`type`、`text`、`at`、`media`、
`sender_handles: [{kind, value}]`（身份线索，原值；hub 只存它的 HMAC，`docs/knowledge.md`「身份线索」）。

`Prompt`：`step`、`fields: [{name, label, secret?, placeholder?}]`、`hint`、`error`（用户能改的错，同一步再问一次）、`done`、`account`，
以及不填字段的两种步骤：`kind: "device_code"`（`url`、`user_code`、`interval`：显示验证码、去网页输入）和 `kind: "qr"`（`url`、`interval`：
`url` 是要用别的 App 扫的二维码内容，比如微信，#1351）。这两种 App 都每隔 `interval` 秒带同一个 `step`（不带值）再问一次，直到回来别的步骤或 `done`；
二维码过期了就回一个新的 `url`（同一步）。程序只给 `url`，二维码的点阵由 hub 画（`qr`），程序给了也会被换掉。

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

- **写操作只响应 `actions.do`**：程序可以声明 `actions`，但只在 hub 的闸门（`connector_gate`，默认手机上确认）放行之后才会被调
  `actions.do`；程序不能自己发起写。`actions.describe` 的 summary 由 hub 截断后上卡片（`connector.cardMost`），卡片写明发给谁、全文。
  （2026-11-03 起，#988：Telegram 只能以子进程跑，原来「写操作不在协议里」的规定改成这样，和 ovis 对过。）
- 密钥只能存 `secrets.*`；不写本地文件，不写日志。
- 日志、错误里不出现正文、号码、token。
- `status`、`inbox.collect` 从内存回答，不联网；自己的轮询放在程序自己的循环里，有新东西发 `changed`。

## 版本

现在是 2。改了字段的意思或删了字段就加 1；只加可选的字段不加。hub 只接受和自己同一版本的程序（第一方程序和 jarvisd 同一次构建，必然一致）。

## 一致性测试

`conformance.Check(t, c)` 把一个连接器放在协议的程序一侧（`serve.Serve`，经一对管道），对清单声明的每个能力既直接调、也经协议调，
两边的结果编成 JSON 必须一字不差：字段在线上丢了、类型变了都会挂。两种开头各跑一遍（`init` + 原方法名，`initialize` + `connector.*`）；
插件开头那一遍还查 `initialize` 回的贡献和清单一致、不认识的连接器回 `no_contribution`。程序整体是否守插件协议，由 `plugin/conformance` 查（`docs/plugins.md`「一致性测试」）。GitHub、信息、Telegram 的测试里都跑它；`TestWireTypesAreTagged` 检查
线上的每个字段都有明确的 snake_case 名字。第三方的程序可以照着 `serve.Serve` 的接法，用同一份测试检查自己。
