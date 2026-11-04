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
| `capabilities` | `status`、`tools`、`actions`（写操作，见下）、`events`、`inbox`、`archive`、`items`（知识库的条目：文档、会议、日程）、`login`、`options`、`threads`（收件箱的会话在 App 里打开成会话页）、`snapshot`（助手快照里的未读会话和今天的会）、`fallback`（承诺每个收件箱条目都带兜底分级 `extra.rule_tier`，没有方法；要有 `inbox`，见 `docs/plugins.md`「收件箱的通用规则」）；hub 只调声明了的 |
| `hosts` | 链接所在的主机名（App 认链接） |
| `agent_tools` | 哪些工具也给编程 Agent，按名字的通配（不写就只给助手） |
| `unlisted` | 哪些工具和写操作不进模型的工具列表，按名字的通配：连接器自己的前端按名字调（Telegram 的 `tg` 命令行）。不是权限：按名字调照常能调，写的照常过闸门，Agent 能不能调照旧看 `agent_tools` |
| `confirm_always` | 哪些写操作永远要用户在手机上确认，按名字的通配（可选，没写行为不变）：`connector_gate` 里命中它的 `allow` 规则（含 `*`）被钳成 `confirm`，`deny` 不变；用户本人在 App 里点发送（`Registry.Run`）不过闸门，不受影响。只加可选字段，协议版本不变 |
| `poll_seconds` | 多久问一次 `archive.changes`，0 用 hub 的默认 |
| `kinds` | 收件箱条目的种类；hub 收到没声明的记一行日志 |
| `archive_sources` | 除了自己的 id，归档还能放在哪些名字下（最多 4 个，格式同 id，不能是别的连接器的 id）：一类会话和其余的分开存（一个动态 feed 里和用户有关的帖子），或连接器迁出 hub 之前知识库里已有的会话 id（见下面「知识库」） |

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
| `archive.changes` | `{cursor, limit}` → `{batches: [{source?, chat, messages}], next, more}` | 游标是程序自己的，hub 原样存、原样还；第一次是 `""`。每批存成会话 `<source>-<chat.id>`，`source` 空着是连接器的 id，否则必须是清单 `archive_sources` 里的（别的名字整批丢掉、记日志） |
| `archive.media` | `{ref}` → `{mime, data}` | 归档消息 `media` 里的一个 ref 对应的字节（base64），最多 20 MB；知识库把它存在会话的附件里。可选：没有时那一行只写 `[附件]` |
| `items.list` | `{}` → `{items: [{kind, id, title, url?, at?, where?}]}` | 知识库的条目（`kind`：`docs` / `meetings` / `events`）；声明了 `items` 才调 |
| `items.doc_title` | `{url}` → `{title}` | 会话里出现的文档链接的标题，读不到回 `""` |
| `snapshot.get` | `{tz?}` → `{chats?: {at_me: [{name, unread, preview?}], other: [{name, unread}]}, meetings?: [{start, end, title}]}` | 声明了 `snapshot` 才调（#1520）：用户此刻的状态，给助手的实时快照和「今天」卡片——在等用户的未读会话（@ 了用户的带最新一条的开头）、今天（`tz` 里的今天）还没结束、没拒绝的会，按开始时间排。要快：hub 最多等 3 秒，超时就不带它。不知道的部分不给 |
| `login.step` | `{step, values}` → `Prompt` | 分步登录；`step` 为 `""` 从头开始 |
| `logout` | `{}` → `{}` | |

`InboxThing`：`thread`（必填，这件事的稳定键）、`kind`、`label`、`title`、`sender`、`preview`、`context`、`at`、`last`（最新一步的标记，变了就算新动静）、
`url`、`open_url`、`reply_to`、`can_reply`、`tier`（`action` / `fyi` / `noise`，空的交给模型）、`why`、`done`（`read` / `replied`：这件事结束了）、
`read`、`tags: [{key, value, name}]`（`repo` / `chat` / `sender` 能被用户静音）、`rules`、`extra`（见下）。

- `rules`：用户给这件事定的分拣规则，原话（这个来源、这个账号的，比如插件设置里的 `triage_rules`，`docs/plugins.md`「约定的设置项」）。
  有就**只用它**，不用 hub 的全局 `inbox.rules`；空的用全局。hub 的默认判断标准照旧在下面，用户的规则优先。可以有换行；最多 4000 字，多了截掉。和别的字段一样在 thing 的第一层，迁出 hub 的连接器（带 `extra` 的）也是。
  旧 hub 不认这个字段，照旧用全局规则。
- 声明了 `fallback` 的连接器每个条目都带 `extra.rule_tier`：这件事的兜底分级，模型照样先分，分不了时按它进收件箱（不带 `tier` 时也一样；
  普通连接器的 `extra` hub 只读这一项）。没声明 `fallback` 的插件连接器要有能用的分拣模型才能启用（`docs/plugins.md`「收件箱的通用规则」）。
- `tier` 是这件事的兜底：模型分不了（没配、没额度、出错、超时）时按它进收件箱。插件提供的连接器（不是 personad 自带的）`tier` 为空的没有兜底，
  它的新条目先分拣再进收件箱（`docs/inbox.md`「AI 分拣」），模型分不了时**这一轮不进收件箱**，
  hub 也不存；下一轮 `inbox.collect` 再交回来（本来就是从内存回答，不用为此做什么），模型好了就进来。hub 在 `GET /api/v1/inbox` 的 `triage` 里说有几件在等（`docs/inbox.md`「AI 分拣」）。

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
| `posts.put` | `{posts: [{post, comments?}]}` → `{statuses}` | 把一个动态 feed 的公开帖子写进知识库的帖子存档（见下面「知识库」）。`statuses` 和 `posts` 一一对应：`new`、`changed`（评论数变了）、`""`（见过、没变）、`refused`（没有 id，或是别的连接器的帖子） |
| `posts.comments` | `{id, comment_count, comments}` → `{}` | 换掉存档里一个帖子（这个连接器的）的评论，记下读的时候的评论数；帖子本身不动。重读 `posts.pending` 列出的帖子的评论用 |
| `posts.pending` | `{limit?}` → `{posts: [{id, comment_count, at}]}` | 评论数变了、该重读评论的帖子（文字保留期内，新的在前，最多 500） |
| `archive.chats` | `{source?}` → `{chats: [{id, name, cursor, last_at, messages}]}` | 知识库里这个连接器某个名字（空着是 id，或 `archive_sources` 之一）下的会话：来源那边的 id 和读到哪。接续用：连接器第一次归档时从这里接着，不重复、不漏 |
| 事件 `changed` | `{scope}` | `chats`、`items`、`inbox`：有新东西，hub 尽快来取；不认识的 scope 被丢掉并记日志 |

插件同时贡献了 `events`（`docs/plugins.md`「事件」）时，hub 发来的 `event.<名字>` 交给实现了 `serve.Listener` 的连接器（`Hear(ctx, 名字, 参数)`，
在自己的 goroutine 里，不回应）；没实现的照旧丢掉。微信机器人用它听 `agent.needs_you`（`docs/wechat.md`）。

### 知识库

`posts.*` 和 `archive.chats` 只给声明了 `archive` 的连接器，用户在设置里关了它的「进知识库」时回错误。都只碰这个连接器自己的东西。

- **帖子存档**（`docs/knowledge.md`「外部帖子」）：`post` 是 `{id, feed?, author?, author_id?, anonymous?, categories?, text, images?, reactions?, comment_count, at, mine?}`，
  `feed` 是这个 feed 叫什么（存档的标题用，空着是「帖子」）；`comments` 是 `[{author?, reply_to?, text, at}]`，**带了（可以是空数组）就是这次读了评论**，
  hub 记下读的时候的评论数；不带就只更新帖子、存档里的评论原样留着。评论数是 0 的帖子 hub 直接记成读过。评论里的「我」「匿名」由连接器写好。
  一次最多 200 帖、一帖最多 5000 条评论，正文和每条评论最多 64 KB（超出的截断）。
- 帖子和用户有没有关系（`concerns`）由 hub 判断：连接器在自己的某个名字下有这个帖子 id 的会话就算（用户发的、有人回复或 @ 了用户的帖子，连接器照常当会话归档）。
- 索引、按文字保留期清理、把帖子挂到项目上（`kb.LinkMoments`）都是 hub 的事，谁写的帖子都一样。爬多少、多久爬一次、怎么翻页是连接器自己的事，
  它自己的状态（翻到哪）放在自己那里。

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

`conformance.Check(t, c)` 把一个 Go 连接器放在协议的程序一侧（`serve.Serve`，经一对管道），对清单声明的每个能力既直接调、也经协议调，
两边的结果编成 JSON 必须一字不差：字段在线上丢了、类型变了都会挂。两种开头各跑一遍（`init` + 原方法名，`initialize` + `connector.*`）；
插件开头那一遍还查 `initialize` 回的贡献和清单一致、不认识的连接器回 `no_contribution`。程序整体是否守插件协议，由 `plugin/conformance` 查（`docs/plugins.md`「一致性测试」）。GitHub、信息、Telegram 的测试里都跑它；`TestWireTypesAreTagged` 检查
线上的每个字段都有明确的 snake_case 名字。用 Go 写的第三方程序可以照着 `serve.Serve` 的接法，用同一份测试检查自己。

### 别的语言写的程序：`CheckProgram`

不是 Go 写的程序没有 Go 一侧可以对照。`conformance.CheckProgram(t, manifest, run, init, opts...)`（`server/internal/connector/conformance/program.go`，各方法的检查在 `program_wire.go`）
把跑起来的程序放在 hub 的位置：对清单贡献的每个连接器，按 hub 用它的顺序各调一遍声明了的能力，把每个回应按 hub 读的 Go 类型**严格解码**
（`DisallowUnknownFields`：多一个 hub 不认识的字段、键不是 snake_case、时间不是 RFC 3339 都算错；Go 解码键不分大小写，所以键另外查一遍），
再查 hub 依赖的不变量：

| 方法 | 解码成 | 还要满足 |
| --- | --- | --- |
| `initialize` | `plugin.InitializeResult` | id、版本和清单一致；贡献的连接器和清单逐字段一致（能力顺序除外） |
| `status` | `connector.Status` | `state` 是已知状态；`ok` 不带 `error`；除 `not_configured` 外，不是 `ok` 的要有 `error` 或 `guide` |
| `tools.list`、`actions.list` | `serve.ToolsResult` | 名字以 `<id>_` 开头、不重复、有描述，`input_schema` 是 `type: object`；工具和写操作不同名 |
| `tools.call` | `serve.CallResult` | 每个只读工具用空参数调一次：回答，或拒绝（要有一句话） |
| `actions.describe` | `serve.DescribeResult` | 每个写操作用空参数问一次：拒绝，或回答（有 `summary` 和 `action.visible`） |
| `inbox.collect` | `serve.CollectResult` | 调两次；`thread` 非空、不重复、第二次还在；`kind` 在清单的 `kinds` 里；`tier`、`done` 是已知值；没有 `done` 的要有 `at` 和 `last`；`can_reply` 要有 `reply_to`；标签有 `key` 和 `value` |
| `thread.page` | `connector.ThreadPage` | 取第一个收件箱条目的会话：旧的在前、不超过 `limit`；`id` 非空不重复、有 `at`；不是事件、不是自己发的有发送者名；没有文字的要有媒体；`chat.reply` 是自己的写操作，参数有 `chat` 和 `text`；有 `before` 就往前翻一页：没有重复、没有更新的消息 |
| `thread.media` | `connector.Media` | 页面里有 ref 才调一次 |
| `archive.changes` | `serve.ChangesResult` | 沿 `next` 走到 `more` 为假，再用最后的游标多问一次：有数据时 `next` 非空、不回到 `""`、不原地不动；同一会话的消息 id 不重复交付；消息有 `id`、`at`、文字或媒体（知识库只追加、不去重，会丢掉没日期、没内容的）；`sender_handles` 有 `kind` 和 `value` |
| `items.list` | `serve.ItemsResult` | `kind` 是 `docs` / `meetings` / `events`，有 `id` 和 `title` |
| `login.step` | `connector.Prompt` | 要么有 `fields`，要么 `done`，要么 `device_code`；要填的一步有名字（空名字是从头再来）；`done` 要有 `account` |
| `logout` | `{}` | 只在 `WithLogin` 登录之后最后调：登出后状态不再是 `ok`，密钥从 hub 的存放里删掉了（`secrets.delete`） |
| 事件 `connector.changed` | `serve.ChangedEvent` | `contribution` 是自己的连接器，且声明了 `events`；`scope` 是 hub 认得的 |
| 程序调 `secrets.*` | `{contribution, data?}` | 带 `contribution`，是自己的连接器 |

清单也要过：能力都是 hub 能在程序里用到的（`connector/proc.Conn` 实现的那些）、不重复，只有收件箱才有 `kinds`，`kind_descriptions` 的键都是声明过的 `kinds`。**从不调**的：`actions.do`（写只在 hub 的闸门之后）、`inbox.reply`
（那是用户按的发送）。选项：`WithLogin(values)` 按字段名填登录的每一步，之后程序必须是 `ok`（只给装在夹具上的后端用，别给真账号的密钥），回应里出现了给它的密钥就挂；
`RequireData()` 让 `inbox.collect`、`thread.page`、`archive.changes` 回空也算挂，免得什么都不回的程序也过。

给自己的程序用：先用 `plugin/conformance` 查它作为插件的起停和握手，再用一份新起的程序跑 `CheckProgram`（`internal/` 包挡住了别的仓库的导入，所以测试放在这个仓库里）：

```go
m, _ := plugin.Parse(manifestJSON)                                // persona-plugin.json
progconf.Check(t, m, progconf.Program(t, "bin/my-connector"), plugin.InitializeParams{})
conformance.CheckProgram(t, m, progconf.Program(t, "bin/my-connector"), plugin.InitializeParams{},
	conformance.WithLogin(map[string]string{"token": fixtureToken}), conformance.RequireData())
```

**线上夹具**：`server/internal/connector/testdata/wire/<程序>/*.json` 是程序对每个方法回的 JSON，**合成数据**（没有真账号、名字、群名、地址，示例只用
`https://wechat-connector.example.com`、`wxid_example_a`、`127.0.0.1`），连同一份 `persona-plugin.json`。`Testwechat-connectorWireFixtures` 把每个夹具按上表严格解码、过不变量，
一份没在表里的夹具也会挂；`TestCheckProgramCatchesDrift` 拿夹具喂一个纯 Go 的假程序，看改了字段名、顺序、游标时 `CheckProgram` 会不会红。所以即使 CI 里没有 Node，
Persona 一改 Go 类型（改名、删字段）这些测试就红，提醒程序一侧同步夹具；程序一侧的仓库应保留同一批文件的副本，用同样的数据检查自己的输出，新增或改夹具时两边一起改。

**跨仓库活测试**：`Testwechat-connectorProgramLive` 跑任意一个实现了连接器协议的外部程序，对着任意一个兼容 wechat-connector 开发者 API 的夹具服务。
路径由两个环境变量给出：`PERSONA_TEST_wechat-connector_PROGRAM` 指向连接器程序的启动脚本（插件包 `bin/` 下的程序）；`wechat-connector_FIXTURE_SERVER` 指向夹具服务的脚本，
测试用 Node 运行它（`PORT=0`，起在回环地址上）。需要 Node，两个环境变量有一个没设就 `t.Skip`，所以现有的测试和 CI 不受影响：

```sh
cd server
PERSONA_TEST_wechat-connector_PROGRAM=<插件包>/bin/<程序> \
wechat-connector_FIXTURE_SERVER=<夹具服务脚本> \
go test ./internal/connector/conformance -run wechat-connector -v
```

程序所在的包里 `persona-plugin.json` 在 `bin/` 的上一级。夹具服务起来后在 stdout 打印**一行** JSON：`base_url`、`outbound_sends_enabled`，和 `accounts`（每个账号的 `key` 是读取 + 事件的应用密钥，另有带发送范围的 `send_key`）。
测试用 `base_url` 和第一个账号的 `key`，按字段名 `base_url`、`app_key` 填程序的登录那一步；`send_key` 从不用。`base_url` 不在回环地址上，或这一行没说外发是关的（`outbound_sends_enabled` 不是 `false`），测试就拒绝登录。
夹具服务上外发保持关闭，测试只用它给的合成账号和测试密钥，不碰真账号。先对程序跑 `plugin/conformance.Check`（起停、握手、错误码），再用一份新起的程序跑 `CheckProgram`。
