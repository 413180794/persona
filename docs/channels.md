# 渠道（channel）：所有和 Persona 对话的入口

渠道是「用户从哪儿和 Persona 说话」：Persona 自己的 App、微信机器人、飞书机器人、飞书里的 `/agent`、语音……每个都是一个渠道。
一条消息从渠道进来，hub 把它发进对话，盯着这一轮，答完把答案发回同一个渠道；这段对话里 Agent 要用户回答、批准的，也发到这个渠道，
在别处答过了就撤掉。设置 › 插件 › 渠道 列出所有支持的渠道、哪些连上了，每个都能进去设置（#1466）。

**渠道和连接器是两回事**：

| | 连接器（`docs/connectors.md`） | 渠道（这里） |
| --- | --- | --- |
| 是什么 | 数据来源：进知识库、收件箱，给助手工具 | 对话的入口：用户在那儿说话，Persona 在那儿回答 |
| 数据往哪走 | 外面 → 知识库 / 收件箱（hub 去拉） | 用户 → 对话 → 用户（一来一回） |
| 例子 | GitHub、信息、Telegram（用户自己的账号）、邮件、飞书（lark-connector） | Persona App、微信机器人、飞书机器人、飞书 `/agent`、语音 |

同一个东西可能两类都是：飞书既是连接器（lark-connector 读消息、文档进知识库），又有两个渠道（飞书机器人、飞书 `/agent`）。
微信插件贡献了一个连接器 `wechat`，但那个连接器只管扫码绑定和状态，没有任何数据进知识库，它本身是个渠道。

## 一、现在有哪些入口（2026-11-04 盘点）

| 入口 | 代码在哪 | 进哪个对话 | 答案怎么回去 | 提问 / 批准 | 归到 |
| --- | --- | --- | --- | --- | --- |
| Persona App：iOS、Mac、Android、网页 | 各端的对话页，`POST /api/v1/sessions/{m}/{s}/messages`（`client_msg_id` 是随机 id） | 用户在 App 里选的那段 | 实时流（WebSocket） | 卡片，原地作答 | 渠道 `app` |
| Apple Watch | 手表 App，经 hub 的同一套接口 | 主对话 / 选的那段 | 同上 | 通知按钮、手表上的卡片 | 渠道 `app` 的一个端 |
| Siri / 快捷指令（问 Persona） | `apple/Persona/Intents`（`docs/siri.md`） | 主对话 | App 里轮询 `items` 最多 24 秒，读出最后的话 | 没有 | 渠道 `app` 的一个端（它自己的轮询是第二份「等一轮答完」，见「还没做」） |
| 微信机器人 | 内置插件 `wechat`（`server/internal/wechat`，`docs/wechat.md`，#1447） | 主对话，或 `/new` 开的对话 | 插件经 hub 的 API 每 2 秒看一次会话，取这一轮的答案，发文字 | 文字往返（回复序号、允许 / 拒绝） | 渠道 `wechat`（插件贡献） |
| 飞书机器人 | hub 里（`hub/bots*.go`、`feishubot/`，`docs/bots.md`） | 主对话、`/new` 的对话、`/agents` 选的 Agent、回复绑定了目标的那条消息 | hub 内部 watch 会话，进度卡 + 答案卡 | 卡片按钮 | 渠道 `feishu`（内置） |
| 飞书里的 `/agent` | `hub/lark_agent.go`、`lark_agent_docs.go`（`docs/lark-agent.md`） | 每个请求 / 话题一段新的无头对话 | Agent 调 `lark_agent_reply`，以**我的名义**回复那条消息 / 评论 | 没有 | 渠道 `lark_agent`（内置，只读设置） |
| 语音 | hub.json 的插件 `voice`、内置语音插件（`server/internal/voice`，`docs/plugins.md`「语音」） | 语音模型自己说；要交给主对话的事经 hub 的 API 发进主对话 | 声音 | 没有 | 渠道 `voice`（插件贡献的 `voice` 扩展点，列出来但不走下面的管线） |
| Telegram | 内置插件 `telegram` 的**连接器**（用户自己的账号，MTProto，`docs/plan-telegram.md`） | —— | —— | —— | **不是渠道**：只是数据来源。现在没有 Telegram 的对话机器人；以后做就是一个插件渠道（Bot API），见「还没做」 |
| 邮件 | 连接器 `mail` | 收件箱 | AI 分身起草、点一下发送 | —— | 不是渠道（数据来源） |
| 收件箱的 AI 分身、定时任务、飞书机器人的 `pushes` / `sync` | hub | hub 自己发起的对话 | 推送、飞书 | —— | 不是入口：hub 发起的。它们**往外发**时用渠道的「推送」能力（见「推送和同步」） |
| botmux | node 的宿主（`docs/engines.md`「botmux」） | 飞书话题 ↔ 某台电脑上的 Claude Code | botmux 自己 | botmux 自己 | 不是 Persona 的渠道：那是 Agent 自己的入口，Agents 页照常显示 |
| 插件的 `assistant`、`api`、`events`、`hub_api` | `docs/plugins.md` | —— | —— | —— | 不是渠道，是拼出渠道的零件；微信第一版就是用 `connectors` + `hub_api` + `events` 拼的 |

结论：**真正「外面的消息进对话、答案发回去」的有三份实现**——飞书机器人（hub 里，`bots_runs.go`）、微信机器人（插件里，`wechat/turns.go`，
注释里写着规则照飞书机器人）、Siri（App 里）。`/agent` 和语音的形状不一样（一个以我的名义回复、每次新对话；一个自己就是模型），
列进渠道表，但不进统一管线。

## 二、渠道的接口

`server/internal/channel` 是不依赖 hub 的纯数据和纯计算（hub、插件程序、测试都用）。一个渠道在接口里是这样（`channel.View`）：

```json
{
  "id": "wechat",
  "name": "微信",
  "kind": "im",
  "source": "plugin",
  "plugin": "wechat",
  "installed": true,
  "enabled": true,
  "state": "connected",
  "error": "",
  "account": {"name": "微信用户 a1b2c3"},
  "capabilities": {"images_in": true, "files_in": false, "voice_in": true, "format": "plain", "max_text": 3500,
                   "asks": "text", "push": true, "typing": true, "progress": false, "threads": false},
  "target": {"kind": "main"},
  "features": [{"id": "asks", "title": "提问和批准", "description": "…", "on": true}],
  "bind": "qr",
  "last_event_at": "2026-11-04T10:00:00Z"
}
```

| 字段 | 意思 |
| --- | --- |
| `id` | `^[a-z][a-z0-9_]*$`，全局唯一（内置的和插件贡献的不能撞）。也是这个渠道发进对话的消息的 `client_msg_id` 前缀：`<id>-<消息 id>`（见「来源标记」） |
| `name` | 给人看的名字 |
| `kind` | `app`（Persona 自己的 App 端）、`im`（第三方 IM 里的机器人：微信、飞书机器人、以后的 Telegram / Slack / Discord）、`mention`（在别人的会话里叫它、以我的名义回复：飞书 `/agent`）、`voice`（语音） |
| `source` | `builtin`（hub 自带：`app`、`feishu`、`lark_agent`）、`plugin`（插件贡献，`plugin` 是插件 id）、`market`（插件市场里有、还没装：`installed: false`，只给去安装的入口） |
| `enabled` | 渠道的总开关。插件渠道：插件启用 **且** 它的连接器开着；飞书机器人：`bots.feishu.enabled` |
| `state` | `connected`（绑好了、在收消息）、`not_connected`（没绑 / 没配）、`error`（`error` 写原因，给人看的一句话）、`disabled`（关着）、`not_installed`（市场里的） |
| `account` | 绑定的账号 `{id?, name}`：只给人看（微信是 iLink 给的 id 的末几位，飞书是主人的名字），不放手机号、邮箱 |
| `capabilities` | 见下表 |
| `target` | 消息进哪个对话：`{kind: "main"}`（主对话，默认）、`{kind: "conversation", machine_id, session_id, title}`（某段对话）、`{kind: "new"}`（下一条消息开一段新对话，开了以后变成 `conversation`）。飞书机器人还有 `{kind: "agent", …}`（`/agents` 选的 Agent） |
| `features` | 这个渠道能单独开关的功能，`{id, title, description, on}` 的列表：App 照列表画开关，不认识的也能画。飞书机器人是 `main`、`cards`、`all_cards`、`pushes`、`sync`、`agents`（`docs/bots.md`「能做什么」）；插件渠道统一是 `asks`（提问和批准）、`all_asks`（所有 Agent 的提问）、`pushes`（推送也发到这里）、`sync`（主对话里别处问的也同步过来），按能力出现 |
| `bind` | 怎么绑定：`qr`（扫码）、`code`（App 给一个配对码，在 IM 里发给机器人）、`fields`（填凭据，再可能接一步 `code`）、`none`（不用绑：`app`、`voice`）、`config`（只能在 hub.json 里配：`lark_agent`） |
| `owner` | 不在接口里：**谁能用**永远是绑定的那一个人（扫码的微信、配对的飞书账号）。只有他的消息进对话；别人发来只回一句「这个机器人只为绑定它的人服务」（同一个人 10 分钟一次），群里一律不处理。判断谁是主人是渠道自己的事（只有它认得 IM 里的身份），hub 只收主人的消息 |

**能力**（`channel.Capabilities`）：

| 能力 | 取值 | hub 怎么用 |
| --- | --- | --- |
| 收文字 | 总是有 | —— |
| `images_in` | 布尔 | 收图片（最多 4 张，作为图片附件进对话） |
| `files_in` | 布尔 | 收文件：hub 存到对话所在电脑的临时目录，路径写进消息（飞书机器人一直这样做）；没有就在消息里写明「发来了文件「…」，还不能转交」 |
| `voice_in` | 布尔 | 收语音（渠道给转写好的字） |
| `format` | `markdown` / `plain` | 答案是什么格式到用户那儿。hub 永远发 Markdown，渠道自己转成 IM 认的样子（微信去掉图片、删除线……留粗体和代码；飞书卡片原样）；`plain` 的渠道，hub 给 Persona 的说明里要求「纯文字、结论先行」 |
| `max_text` | 字数，0 是不限 | hub 按它把答案拆成几条，不在代码块中间断开（`channel.Split`） |
| `asks` | `none` / `text` / `buttons` | Agent 的提问和批准：`text` 由 hub 写成文字、把用户下一条消息读成回答（`channel.AskText`、`channel.ReadAnswer`，规则见「提问和批准」）；`buttons` 由渠道自己画（飞书卡片），hub 告诉它问什么、什么时候撤 |
| `push` | 布尔 | 能不能主动给用户发（不是回一条消息）：推送、同步、提问都要它 |
| `typing` | 布尔 | 有没有「正在输入」：一轮进行中 hub 每 5 秒叫一次 |
| `progress` | 布尔 | 能不能发一条再改它（进度卡）：hub 5 秒后还没答完就发进度、每 3 秒最多改一次，答案写进同一条 |
| `threads` | 布尔 | 回复某条机器人发的消息，就发给那条消息绑定的对话（飞书机器人的 `/agents`） |

## 三、hub 的统一管线

一份实现，飞书机器人和插件渠道都走它（`server/internal/hub/channels*.go`，判断规则在 `channel` 包里）。渠道只管「和 IM 说话」：收消息、发消息、画卡片。

### 一条消息怎么走

1. **收**：渠道把主人的一条消息交给 hub（内置的直接调，插件发 `channel.receive`）：`{id, text, images, files, quote, forward, reply_to, unsupported}`。
2. **去重**：按 `<渠道>-<消息 id>` 只处理一次（IM 会重投）；同一个 id 也是 `client_msg_id`，和 App 共用 `sent_messages` 的去重，重发的不会打两遍。
3. **命令**：`/` 开头的先当命令，hub 统一实现 `/help`、`/new [第一句话]`、`/main`、`/stop`、`/status`；渠道可以有自己的（飞书机器人的 `/list`、`/agents`、`/start`）。
   不认识的原样发进对话（可能是对话自己的，比如 `/compact`）。
4. **回答提问**：这个渠道有在等的文字提问（`asks: text`），而且它还在 hub 的待处理列表里，这条消息就是回答。在别处答过了的放掉，这条当普通消息。
5. **目标**：回复的是绑定了目标的那条消息（`threads`）就发给它，否则发给渠道的 `target`。主对话还不存在时新开一段并设为主对话；`new` 开的对话之后就是目标。
6. **组织内容**：文字前面加一段 `<jarvis-context>`，说明这条从哪个渠道来、回答会发回去、用什么格式（提示词 `prompts/channel/message.md`，飞书机器人保留自己的 `bot/message.md`）；
   引用的消息、合并转发的聊天记录各放一段 `<jarvis-context>`；图片作为附件；文件见能力 `files_in`。
7. **发**：Persona 的对话走 `session.send`（主对话跟着设置里的模型），`client_msg_id = <渠道>-<消息 id>`。
8. **跟踪这一轮**（一个 run，存数据库，hub 重启接着等），规则就是原来飞书机器人和微信机器人各写了一遍的那套，现在在 `channel.Check`：
   - 先按 `client_msg_id`（老 node 没有时按消息开头的一段字 `probe`）在 transcript 里找到这条消息，取它后面最后一次工具调用之后的回答；
   - 会话在工作、或在等用户回答就等；20 秒都没看到它工作才开始找；10 分钟还找不到这条消息才退回取最后的话；
   - 同一段对话还没答完又发了一条：两条合成一个回答；
   - 后台还有任务：先答一次并说「后台还有 N 个任务在跑，做完再告诉你」，等后台回报的那一轮结束再答一次（最多再等 2 小时）；
   - 4 小时还没完就不等了，说一声到 App 里看；对话不见了（3 分钟）也说一声。
   - 进行中：有 `progress` 的发进度（最近 4 步、正在写的字、用时），有 `typing` 的叫「正在输入」。
9. **发回**：答案按 `max_text` 拆开发回；有 `progress` 的写进进度那条。这一轮不再推手机的「完成」通知（结果已经在渠道里了），`sync` 也不再发一遍。

### 提问和批准

- **发到哪个渠道**：这个渠道在「驱动」那段对话——它是渠道的目标、有 run 在等它、24 小时内有绑定的话题（`threads`）——而且 `asks` 开着；
  `all_asks` 开着时所有 Agent 的都发。各台电脑上别的 Agent 的提问照旧只在 App 里。
- **`asks: buttons`**（飞书机器人）：渠道画卡片，按钮的回答经 hub 的同一个 `resolve`。
- **`asks: text`**（微信、以后的 Telegram）：hub 写成文字（`channel.AskText`），用户的下一条消息就是回答（`channel.ReadAnswer`）：

  | 卡片 | 怎么答 |
  | --- | --- |
  | 批准工具 | 「允许」（同意、可以、好…）或「拒绝」；别的字再提示一次 |
  | 计划 | `1` 批准并自动执行、`2` 批准且改文件时自动同意、「拒绝」打回；写别的就是打回并附上这段话 |
  | 提问 | 选项序号（多选用空格隔开），或直接写回答；几个问题一个一个问 |
  | 终端里的对话框 | 选项序号 |
  | 要在手机上填写的、表单、读不出选项的终端画面 | 说一声「这个要在 App 里处理」 |

  同时有几个在等时，回复答的是最近发出的那个。渠道还没法主动发（微信：主人还没发过消息，没有 `context_token`）时不记为「在渠道里问了」，App 里照常能答。
- **在别处答过**（App、手表、通知按钮、电脑上、另一个渠道）：`buttons` 的卡片改成「✓ 已回答：…」，撤回 / 过期的改成「已结束」；`text` 的从等待列表里拿掉，
  用户再回复就当普通消息（文字发出去了撤不回，不另发一条打扰）。

### 推送和同步

渠道有 `push` 时可以开：`pushes`（`notify_user` 推到手机的同时发一份；定时任务那段对话的回答也发）、`sync`（主对话里不是从这个渠道发起的一轮结束时，把回答发过来）。
飞书机器人原来就有这两个；插件渠道照同一套开关，默认关。

### 来源标记

- **每条消息**：渠道发进对话的消息 `client_msg_id` 都是 `<渠道 id>-<消息 id>`（飞书机器人一直是 `feishu-…`，微信 `wechat-…`），node 把它带到 transcript 的 item 上（`Item.client_msg_id`）。
  客户端按前缀认出渠道：前缀（第一个 `-` 之前）等于 `GET /api/v1/channels` 里某个渠道的 id 就是从那儿来的，对话页在这条消息下面画一行「来自 微信」。
  App 自己发的是 UUID，第一段是 8 位十六进制，所以渠道 id 不能是 8 位十六进制（注册时拒绝）。语音插件交给主对话的消息带 `voice-<随机 id>`。
- **每段对话**：hub 在会话上加 `channels`（这段对话里从哪些渠道来过消息，按最近排，`proto.Session.Channels`），对话列表画成渠道的小图标；
  `/agent` 开的无头对话是 `lark_agent`。老的 App 不认这个字段，照旧显示。
- 每一轮的 run 记着它是哪个渠道的，`GET /api/v1/channels/{id}` 里有「在等几个回答、几个提问」。

### 排队、去重、并发

- 同一段对话同时只有一个 run：后来的消息并进去（「两条合成一个回答」）。不同对话的 run 各自跑。
- 一个 run 的检查同时只跑一个（watch 循环和「一轮结束」可能同时来，两个检查会把同一个答案发两遍：`chanMutex`）。
- 渠道的消息 id 只处理一次（每个渠道留最近 300 个）；hub 发给渠道的每条消息带 `ref`（run id + 第几段），插件渠道按 `ref` 去重，hub 重发不会发两遍。

## 四、插件怎么贡献渠道：`channel` 扩展点

### 为什么加扩展点，不继续拼

微信第一版是 `connectors`（扫码、状态）+ `hub_api`（发消息、读会话、答提问）+ `events`（`agent.needs_you`、`agent.finished`）拼的，能用，但：

- 每个插件都要自己写一遍「等一轮答完」：找消息、等工作、后台任务、合并、超时……微信的 347 行就是从飞书机器人抄的。写 Slack、Discord 的还要再抄，第三方插件（别的语言）抄都没法抄。
- `hub_api` 给的是 hub 主人的完整 token：一个只想当聊天入口的插件，拿到了 hub 能做的一切。
- 插件每 2 秒轮询一次会话（`GET /sessions` 是整台机器的列表），hub 自己明明知道一轮什么时候结束。
- hub 不知道它是个渠道：设置里列不出来，对话页标不出来源，推送 / 同步接不上。

所以加一个明确的扩展点 `channel`：**插件只管和 IM 说话，其余都是 hub 的**。不需要 `hub_api`，也不用订 `events`。

### 清单

```json
"contributes": {
  "connectors": [{"id": "examplechat", "name": "Example Chat", "capabilities": ["status", "login"]}],
  "channel": [
    {"id": "examplechat", "name": "Example Chat", "kind": "im", "connector": "examplechat",
     "capabilities": {"images_in": true, "format": "markdown", "max_text": 4000, "asks": "text", "push": true, "typing": true}}
  ]
}
```

| 字段 | 规则 |
| --- | --- |
| `id` | `^[a-z][a-z0-9_]{0,31}$`，不能是 8 位十六进制；和内置的（`app`、`feishu`、`lark_agent`）、别的插件的渠道都不能撞 |
| `name` | 1–40 个字符 |
| `kind` | `im`（插件现在只能贡献 IM 机器人） |
| `connector` | 可选：同一个插件贡献的连接器 id。绑定（`login`：扫码、填 token）、状态、账号都来自它；渠道跟着它的开关跑（进程在它开着时才跑）。不写就要 `on_startup`，没有绑定这一步 |
| `capabilities` | 上面那张表；`format`、`asks` 只能是表里的值，`max_text` 不小于 0 |

要求：进程说协议（`NeedsRPC`）。版本 1。

### 协议（`channel.` 命名空间，参数都带 `contribution`）

**插件调 hub**：

| 方法 | 参数 | 回 |
| --- | --- | --- |
| `channel.receive` | `{contribution, message: {id, text?, images?: [{mime, data}], files?: [{name, mime, data}], quote?, forward?, reply_to?, unsupported?: [string]}}` | `{}`：hub 收下了（去重后排进处理，不等这一轮）。只交主人的消息 |
| `channel.status` | `{contribution, state, error?, account?}` | `{}`：不用连接器的渠道报自己的状态；有 `connector` 的不用调 |

`data` 是 base64。图片最多 4 张、每张 3 MB，文件每个 50 MB，超了 hub 丢掉并在消息里写明。

**hub 调插件**：

| 方法 | 参数 | 回 |
| --- | --- | --- |
| `channel.send` | `{contribution, ref, text, kind, reply_to?}`：`text` 是 Markdown（已经按 `max_text` 拆好），`kind` 是 `answer` / `note` / `ask` / `push` | `{message_id?}`；发不出去（还没有可以发的会话）回错误码 `cannot_send` |
| `channel.typing` | `{contribution, on}` | `{}`（有 `typing` 才调） |
| `channel.edit` | `{contribution, message_id, text}` | `{}`（有 `progress` 才调） |

错误码之外的错误只有 `message`，不带正文和密钥。hub 调 `channel.send` 失败（不是 `cannot_send`）隔 1、2、4 秒重试 3 次，同一个 `ref`。

**一致性测试**：`channel/conformance.Check(t, manifest, run, init)` 在插件协议的检查之外：清单里每个渠道的 `channel.send` 回「发了」或 `cannot_send`，
同一个 `ref` 两次回得一样（只发一次）；`channel.typing` 收得下；没有 `ref` 的不收；不认识的渠道回 `no_contribution`，不认识的方法回 `unknown_method`。
微信插件跑它（`hub/wechat_test.go`）。

### 写一个渠道插件

Go 的用 `connector/serve.MainWith`，连接器实现 `channel.Transport`（`Send`、可选的 `Typing`、`Edit`），收到消息调 `serve` 给的 `channel.Hub.Receive`。
别的语言照上面的协议，一行一条 JSON（`docs/plugins.md`「进程插件协议」）。

## 五、兼容和迁移

- **飞书机器人**：配置、凭据、主人、功能开关原样留在 settings `bots.feishu`，`/api/v1/bots/feishu*` 的接口不变（老 App 照用）。聊天状态 `bots.feishu.state` 里的 `runs`
  第一次启动时搬进统一管线的 runs（字段一一对应：`message_id` → `reply_to`，`card` / `reaction` 进 `ext`），`current`、`threads`、`seen` 留在原处。
- **微信**：插件的密封状态（`connector_secrets` 的 `wechat`）里原来有 `target`、`runs`、`asks`：新版本的 hub 启动时、在起插件程序之前读出这三项，
  搬进渠道的设置和 runs，再把它们从那份状态里删掉（token、游标、`context_token`、去重留给插件，`hub/channels_migrate.go`）。已经绑定的不用重新扫码。
  插件和 hub 是同一次构建（`version -v` 不一样就不启动），不存在新插件配老 hub。插件不再要 `hub_api` 权限：hub 不再给它 hub 的 token。
- **Telegram**：不是渠道，什么都不动；连接器的会话、开关、选项都照旧。
- **老 App**：新接口都是新增的；会话上多的 `channels`、设置首页的新入口，老 App 不认就不显示。原来的入口（设置 › 连接器 › 飞书机器人 / 微信）照旧在，
  新版本里两边互相链接（见「界面」）。
- **老插件**：用 `connectors` + `hub_api` + `events` 拼的第三方渠道照旧能跑（hub 不知道它是渠道，列表里没有它）。

## 六、接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/channels` | `{channels: [View]}`：内置的、插件贡献的（装了的，含停用的）、市场里有 `channel` 贡献还没装的。按 `app`、已连接、未连接、市场排 |
| GET | `/api/v1/channels/{id}` | `View` 加 `waiting: {answers, asks}`、`conversations`（这个渠道最近进过的对话，最多 5 段：`{machine_id, session_id, title, at}`）、`settings_url`（飞书机器人这种有自己完整设置页的，App 链过去） |
| PUT | `/api/v1/channels/{id}` | `{enabled?, target?, features?: {id: bool}}`，只改带了的；回 `View`。`target.kind` 是 `main` / `new` / `conversation`（带 `machine_id`、`session_id`，必须是助手的对话） |
| POST | `/api/v1/channels/{id}/bind` | `{step?, values?}` → `connector.Prompt`（`docs/connectors.md` 的分步登录：填字段、扫码 `qr`、配对码 `pair_code`）。`step` 为空是从头开始。插件渠道转给它的连接器；飞书机器人：没凭据时一步填 App ID / Secret，有了以后给配对码 |
| DELETE | `/api/v1/channels/{id}/binding` | 解绑：插件渠道是连接器的退出登录；飞书机器人是解除配对。runs 和在等的提问一起清掉 |
| POST | `/api/v1/channels/{id}/test` | 给**主人自己**发一条测试消息（只能发给绑定的那个人） |

只读设备只能 GET。`/health` 和 hello 的 `features` 有 `channels` 时才有这些接口。

## 七、界面

- **入口**：设置 › 插件 下面一行「渠道」（已连接的个数，有出错的标红），iOS / Mac、Android、网页一样。hub 没有 `channels` 这个 feature 时不显示。
- **列表**：每行是渠道的图标（品牌标志，`Theme.Brand` / 各端同名的 token；没有标志的用名字首字）、名字、绑定的账号（没有就是一句说明），右边是状态：已连接（绿点）/
  未连接（灰）/ 出错（红，带一句原因）/ 已停用。分三节：「Persona」（`app`：iOS、Mac、手表、Android、网页、Siri，只是告诉用户这些都能用）、「已接入」、「可以安装」（市场里的，点进去是插件市场的详情）。
- **单个渠道**：
  - 顶上状态：图标、名字、状态、账号、最近一条消息的时间；出错时的原因和怎么修。
  - 「启用」开关。
  - 绑定：没绑时一个「绑定」按钮，走 `bind` 的分步（扫码时画二维码，可「保存二维码」；配对码时显示 6 位数字、等对方发；填字段时是表单）。绑了以后是账号和「解除绑定」（先确认）。
  - 「消息发到」：主对话 / 新对话（下一条消息开）/ 选一段对话（助手最近的对话列表）。
  - 功能开关：按 `features` 画。
  - 「发一条测试消息」（只发给自己）。
  - 有 `settings_url` 的（飞书机器人）多一行「更多设置」，进原来的页面。
- **连接器页**：微信连接器、飞书机器人原来在设置 › 连接器里的入口留着，页面顶上加一行「这是一个对话渠道，在 设置 › 插件 › 渠道 里设置」链过去；渠道页的绑定和连接器页的登录是同一个接口，哪边做都一样。
- **对话页**：从渠道来的用户消息下面一行小字「来自 微信」（渠道的图标 + 名字，次要色）；App 自己发的不标。
- **对话列表**：会话的 `channels` 不空时，标题旁边画最近那个渠道的小图标。

## 八、分步

| PR | 内容 |
| --- | --- |
| 1 | 这份文档；`channel` 包（类型、`Check` / `Decide`、`Split`、`AskText` / `ReadAnswer`、来源）；hub 的渠道注册表和接口；扩展点 `channel`；统一管线：飞书机器人的 runs 迁过去，微信改成 `channel` 扩展点（插件只剩 iLink 收发、扫码），状态迁移；会话的 `channels`；一致性测试 |
| 2 | 网页：设置 › 插件 › 渠道，对话页的来源 |
| 3 | Android：同上 |
| —— | iOS / Mac / 手表（#1529）：设置 › 插件 › 渠道（`apple/Persona/Views/Settings/ChannelsViews.swift`，绑定复用连接器的分步登录组件，多了 `pair_code`），对话页和手表上的「来自 …」，侧栏的渠道小图标，连接器页（微信、飞书机器人）链到渠道 |

## 九、还没做

- **Telegram 机器人**（Bot API，和现在的 Telegram 连接器是两样东西）：有了 `channel` 扩展点只要写收发，排在后面。
- **Siri 的轮询**：「问 Persona」在 App 里自己等一轮答完（24 秒），没有走 hub 的管线。要走的话 hub 加一个「发一条、等它答完再回」的长轮询接口，Siri 调它。
- **飞书 `/agent`、语音**：形状不一样，列进渠道表，设置在原来的地方（hub.json、插件页），不走这条管线。
- 一个渠道只绑一个主人；渠道里的群聊不处理。
- `text` 的提问在别处答过后撤不回已经发出的文字。
- 插件渠道的 `progress`（发一条进度、再用 `channel.edit` 改它）：协议里有，管线还只给飞书机器人画进度卡，插件渠道现在只有「正在输入」。
