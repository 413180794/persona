# 飞书机器人：在飞书里和 Persona 对话

用户在飞书里私聊一个自建应用的机器人，就是在和 Persona 说话：消息进「Persona」tab 的主对话（或者用命令换成别的对话、
某台电脑上的 Claude Code / Codex），这一轮的回答以机器人的身份发回飞书；Persona 和 Agent 要用户回答、批准的，在飞书里发卡片，
点一下就答了；推送和主对话在别处的回答也可以发一份到飞书。每一项单独开关，默认全关（2026-11-02 用户要求：「P1/2/3 都做吧，可选开启」，issue #322）。

代码：`server/internal/feishubot`（长连接、REST、事件和消息内容、卡片）、`server/internal/hub/bots*.go`（配对、路由、跟踪回答、卡片、推送）；
提示词 `server/internal/prompts/bot/message.md`；App 的页面 `apple/Jarvis/Views/Settings/FeishuBotView.swift`（设置 › 连接器 › 飞书机器人）。

## 和飞书 `/agent` 的区别

| | 飞书机器人（这里） | 飞书 `/agent`（`docs/lark-agent.md`） |
| --- | --- | --- |
| 怎么触发 | 私聊机器人 | 在任何会话里**自己**发 `/agent …` |
| 身份 | 机器人自己（不会被当成是你说的） | 以你的名义回复（末尾「> 🤖」，提示词要求不替你承诺） |
| 发到哪 | 主对话（或换到的对话 / Agent），接着之前聊的 | 每个请求 / 话题一段新的无头对话，hub 先收集那个会话的上下文 |
| 依赖 | 飞书开放平台的自建应用（可能要管理员审核） | lark-connector（不需要审批） |
| 交互 | 卡片按钮直接回答提问、批准 | 没有 |

两者互补，各自开关。

## 为什么用开放平台的长连接

- hub 不需要公网地址：`POST /callback/ws/endpoint` 拿到 WebSocket 地址，hub 主动连出去，事件和卡片回调都从这条连接来（开放平台「使用长连接接收事件」）。
  内网、NAS、家里的机器都能跑。
- 官方 Go SDK（`oapi-sdk-go/v3`）整个 4.8 MB，还要 gogo/protobuf 和 gorilla/websocket；协议本身很小（一个 protobuf 的 `Frame`），
  所以 `feishubot/frame.go` 手写编解码，`TestFrameMatchesTheSDK` 用 SDK 生成的字节对过。WebSocket 用仓库已有的 coder/websocket。
- 协议要点（照 SDK 的行为）：客户端按 `ClientConfig.PingInterval`（默认 2 分钟）发 ping 控制帧，pong 里可能带新配置；数据帧的
  header 有 `type`（event / card）、`message_id`、`sum` / `seq`（大消息分片，拼齐再处理）；每个数据帧都要回同一帧、payload 换成
  `{"code":200,"data":<base64 JSON>}`，卡片回调的 data 是要显示的 toast / 新卡片，**3 秒内**不回飞书就重试。两个 ping 周期收不到任何东西就当连接死了重连。
- 同一事件可能重发：消息按 message_id 只处理一次（`bots.feishu.state` 的 `seen`，留最近 300 个）。

## 配对和权限

- 应用凭证：App 里填 App ID / App Secret（先调 `GET /open-apis/bot/v3/info` 试一次，通了才存），Secret 用 `secret.key` AES-GCM 加密后存 settings
  `bots.feishu`（和启动器的 Key 同一个主密钥），任何接口都不返回明文。不想经过 App 的可以写 hub.json 的 `bots.feishu`（0600），App 里存过的优先。
- **只认配对过的那一个人，只在机器人的私聊里**：主对话能用全部数据（飞书、邮件、各台电脑、手机里的日历和位置），群里谁都能 @ 它，所以群消息一律不处理；
  别人私聊只回一句「只为它的主人服务」（同一个人 10 分钟内只回一次）。卡片按钮也只认主人的 open_id。
- 配对：App 里点「配对」拿 6 位数字（10 分钟有效），在飞书里发给机器人，发的人就成了主人（记下 open_id 和私聊的 chat_id）。
  没配对时私聊它会收到怎么配对的提示；猜错 5 次这个码就作废，要重新拿。换了 App ID 就要重新配对（是另一个机器人了）。
- 名字：读通讯录要 `contact:user.base:readonly`，没有这个权限时主人叫 hub.json 的 `assistant.owner`，别人叫「某人」。

## 能做什么（`features`，App 里一个开关一项）

| 开关 | 做什么 |
| --- | --- |
| `main` 和 Persona 对话 | 消息发进主对话；`/new` 开新对话、`/list` 选最近的对话、`/main` 回主对话 |
| `cards` 提问和批准 | 飞书在驱动的对话（当前目标、在等回答的、24 小时内绑过话题的）要回答 / 批准时发卡片 |
| `all_cards` 所有 Agent 的提问 | 各台电脑上所有 Agent 的提问和批准都发卡片（打开它会顺带打开 `cards`） |
| `pushes` 推送也发到飞书 | `notify_user` 推到手机的同时发一份；定时任务自己那段对话的回答也发 |
| `sync` 同步主对话 | 主对话里不是从飞书发起的一轮（App 里问的、定时任务发进主对话的）结束时，把回答发到飞书 |
| `agents` 驱动 Agent | `/agents` 列出各台电脑上的 Agent（需要你 / 工作中 / 完成未看 / 两天内用过的），点「在这里聊」之后的消息发给它；`/start` 开新的 |

默认（第一次打开时）只有 `main` 和 `cards`。

## 一条消息怎么走

1. `im.message.receive_v1` → 去重 → 不是主人 / 不是私聊就停。以 `/` 开头的先当命令（下面）；不认识的命令原样发过去（可能是 Agent 自己的，比如 `/compact`）。
2. 目标：回复的是机器人发过的某条消息（或在它的话题里）就发给那条消息绑定的对话 / Agent（`threads`），否则发给当前目标（`current`：`main` / 某段 Persona 对话 / 某个 Agent）。
3. 内容：文字（富文本转成 Markdown，@ 换回名字）；图片（最多 4 张）作为图片附件；文件、语音、视频和多出来的图片下载后经 node 的
   `file.upload.*` 存到那台电脑的临时目录，路径写进消息；引用的消息、合并转发的聊天记录（逐条读出来，一行一条）放进 `<jarvis-context>`；
   发给 Persona 的再加一段说明「回答会发回飞书，结论先行、不用 notify_user」（提示词 `bot/message.md`）。
4. 发送：Persona 的对话走 `session.send`（主对话照 App 一样跟着设置 › 助手的启动器），`client_msg_id` 是 `feishu-<message_id>`，
   和 App 共用 `sent_messages` 的去重：重发的事件不会打两遍。还没有主对话时新开一段并设为主对话。消息上点「OnIt」表情。
5. 跟踪回答（`botRun`，存在 `bots.feishu.state`，hub 重启后接着等）：hub 像 App 打开对话那样 watch 这段对话；5 秒后还没答完就回一张进度卡
   （在做什么：运行 / 读 / 改 / 搜……最近 4 步，正在写的字，用时），3 秒最多改一次。这一轮结束（不在工作、没有在等你的卡片）时取**这条消息之后**
   的最后一段回答（最后一次工具调用之后的文字），写进进度卡（没有就回复一张新卡片），表情换成「DONE」。
   - 消息发出时对话正在忙：它排在当前这一轮后面。那一轮结束时 transcript 里还没有这条消息，不能把那一轮的结尾当成回答，所以按消息开头的一段字（`probe`）
     找到它在 transcript 里的位置，只取它后面的回答；10 分钟还找不到才退回取最后的话。
   - 同一段对话还没答完又发了一条：两条合成一个回答（两条消息都换「DONE」）。
   - 还有后台任务（`run_in_background`、Monitor、子 Agent）：先答一次并说明「后台还有 N 个任务」，等它们回报的那一轮结束再答一次（最多再等 2 小时）。
   - 4 小时还没完就不等了，说一声到 App 里看。
   - 回答太长按 6000 字一张卡片拆开，不在代码块中间断开。卡片被飞书拒收（Markdown 有它不认的东西）时改发富文本。
6. 从飞书发起、在飞书回答的一轮不再推手机的「完成」通知（`botAnswered`），`sync` 也不会再发一遍。

## 命令

`/help`、`/main`、`/new [第一句话]`、`/list`、`/agents [关键词]`、`/start [要它做什么]`（选机器、填目录的表单）、`/stop`（中断当前目标这一轮）、`/status`（现在发给谁、在等几个回答）。

## 卡片

- 用卡片 JSON 2.0（`schema: "2.0"`）：Markdown 支持标题、表格、代码块，模型的回答原样能显示（1.0 不支持标题和表格）。需要飞书 7.20 以上，旧客户端只显示标题和一句升级提示。
  2.0 的坑：没有 `action` 模块，按钮横排用 `column_set`（`flex_mode: flow` 窄屏自动换行）；`note` 没了，用 `div` + `text_size: notation` + `text_color: grey`；
  按钮的回传在 `behaviors: [{type: callback, value}]`；表单里的提交按钮要 `form_action_type: submit` 和卡片内唯一的 `name`；**多一个不认识的字段整张卡片就被拒**；
  回调里返回的新卡片也必须是 2.0。
- 文字里的 `<` 在代码以外转成 `&#60;`（卡片会把 `<at>`、`<font>` 这类当成自己的标签吃掉）；名字、标题这类数据用 `EscapeMarkdown`。
- 链接按钮只用 http(s)（文档只写了 http(s) 和 AppLink，`jarvis://` 可能让整张卡片被拒），「打开网页版」打开 hub 托管的网页版对应的会话页。
- 交互卡片的选项和 App 的卡片一致：
  - 提问：单个问题、最多 4 个选项、单选时是一排按钮 + 「直接写回答」的输入框；其余是表单（每题一个下拉框，多选的是多选下拉框，加一个输入框），一次提交。
  - 计划：全文（5000 字以内）、「批准并自动执行」（bypassPermissions）/「自动接受编辑」（acceptEdits）/ 写意见「打回修改」。
  - 授权：工具、原因、命令或参数；「允许」「拒绝」，有会话的加「本会话总是允许」。**要在手机上填的（密码、授权码，`fields`）飞书里不能批准**，只给「打开网页版」。
  - 终端对话框：读得出选项的一个选项一个按钮；读不出的给回车 / Esc / 1 / 2 和输入框；要输密码的（`secret`）不能在飞书里输。
- 在任何地方答了（飞书、App、手表、通知按钮、电脑上），飞书的卡片都改成「✓ 已回答：…」；撤回 / 过期的改成「已结束」。终端和 Codex 的卡片按下去要等电脑那边，超过 2 秒先回「正在处理…」，做完再改卡片。

## 配置和接口

```json
"bots": { "feishu": { "app_id": "cli_…", "app_secret": "…", "domain": "feishu" } }
```

`domain`：`feishu`（默认）、`lark`，或私有部署的开放平台地址。开关、功能、配对都在 App 里，存 settings `bots.feishu`；聊天的状态（当前目标、话题绑定、在等的回答、处理过的消息）存 `bots.feishu.state`。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/bots/feishu` | `{enabled, app_id, domain, has_secret, from_config, features{main,cards,all_cards,pushes,sync,agents}, connected, error, last_event_at, bot_name, owner{name,paired_at}, pairing{code,expires_at}, target, agents[{title,machine_id,session_id}]}` |
| PUT | `/api/v1/bots/feishu` | 只改带了的：`enabled`、`app_id` + `app_secret`（先验证）+ `domain`、`features`（整个替换）；`app_id: ""` 清掉 App 里的凭证（回到 hub.json 的） |
| POST | `/api/v1/bots/feishu/pair` | 生成配对码 |
| DELETE | `/api/v1/bots/feishu/owner` | 解除配对（聊天状态一起清掉） |
| POST | `/api/v1/bots/feishu/test` | 给主人发一条测试消息 |

## 在开放平台上要做的

1. 建企业自建应用，开启「机器人」能力。
2. 权限：`im:message`（读写单聊消息）、`im:message:send_as_bot`、`im:message.p2p_msg:readonly`（单聊里发给机器人的消息）、`im:resource`（下载消息里的图片和文件）；
   可选 `contact:user.base:readonly`（名字）。
3. 事件与回调：订阅方式选「使用长连接接收事件」；事件订阅「接收消息 v2.0」（`im.message.receive_v1`），可选「用户进入与机器人的会话」；回调订阅「卡片回传交互」（`card.action.trigger`）。
4. 发布版本（企业里通常要管理员审核），App 里填 App ID / App Secret、打开、配对。

## 还没做 / 限制

- 进度卡只显示步骤和正在写的字，不能在卡片里展开看工具的输入输出（到网页版 / App 里看）。
- Agent（herdr 里的 Claude Code / Codex）没有「正在写的字」，进度卡只有步骤。
- 卡片回调的 token（30 分钟内可以再更新 2 次）没用上：答完以后的卡片都是用「更新消息」接口改的。
- 一个 hub 只接一个飞书机器人、一个主人。

## 验证

服务端：`go test ./internal/feishubot ./internal/hub -run 'Bot|Frame|Conn|TurnAnswer|ResolutionText'`（帧和 SDK 的字节一致、分片、ping、
卡片回调的回答、配对 / 只认主人 / 群消息不处理、发进主对话只发一次、回答取自这条消息之后、提问卡片答题、命令）；提示词 golden `bot_message.*`。
App：`FeishuBotModelTests`（加载、功能整体提交、凭证保存与报错）；截图用 mock hub（`MOCK_BOT=pairing` / `off` 看配对中和没配置时）+ `-openConnectorBot YES`。
