# 协议（前后端契约）

所有 JSON 字段 snake_case。时间为 RFC3339（UTC）。hub 默认监听 `:18660`。

## 鉴权

- `Authorization: Bearer <token>`；WebSocket 也可用 `?token=`。token 在 example-machine `~/.jarvis/hub.json` 的 `token`，
  `jarvisd hub pair` 打印 URL + token（以及终端二维码，App 扫码配对）。
- `GET /api/v1/health` 不需要鉴权，返回 `{"ok":true,"version":"..."}`。

## 数据类型

```jsonc
// Machine
{"id":"example-machine","name":"公司 Mac","kind":"ssh","ssh_host":"example-machine","online":true,"error":"",
 "hostname":"example-machine","os":"darwin","arch":"arm64","herdr_version":"0.8.0","claude_version":"2.1.310",
 "last_seen":"..."}

// Launcher（来自 zsh 的 _claude_launchers）
{"name":"cc_ds","base_url":"http://192.0.2.10:18000","model":"deepseek/v4.1-flash-chain"}

// Session：(machine_id, session_id) 唯一。session_id 就是 Claude Code 的 session uuid。
{"machine_id":"example-machine","session_id":"uuid","title":"AI 生成的标题或目录名","cwd":"/home/x/code/foo",
 "status":"idle|working|blocked|done|unknown|offline",   // offline = 不在 herdr 里运行，只有 transcript，可恢复
 "pane_id":"w1:p2","tab_id":"w1:t2","workspace_id":"w1","agent_name":"j-3f2a",
 "launcher":"cc_ds","model":"deepseek/v4.1-flash-chain","started_by_jarvis":true,"assistant":false,
 "updated_at":"...","last_text":"最后一条 assistant 文本的前 200 字","pending_interactions":1}

// Item：transcript 规范化后的一条（一个内容块一条）
{"id":"<entry uuid>#<block idx>","seq":123,            // seq = transcript 行号，单调递增，用于增量和分页
 "ts":"...","role":"user|assistant|system",
 "kind":"text|thinking|tool_use|tool_result|notice|image",
 "text":"…",                                           // text / thinking / notice / tool_result 的文本
 "tool_use_id":"toolu_…",                             // tool_use 与 tool_result 共用，App 据此合并成一张卡片
 "tool_name":"Bash", "tool_input":{...},              // 仅 tool_use
 "is_error":false,                                    // 仅 tool_result
 "image":{"mime":"image/png","data":"base64"}}        // 仅 image（tool_result 里的图片也拆成 image item，带 tool_use_id）

// Interaction：需要人处理的事
{"id":"ix_…","machine_id":"example-machine","session_id":"uuid","session_title":"…","cwd":"…",
 "kind":"question|plan|permission|screen",
 "created_at":"…","expires_at":"…","tool_name":"AskUserQuestion","tool_use_id":"…",
 "questions":[{"question":"…","header":"…","multi_select":false,
               "options":[{"label":"…","description":"…"}]}],   // question
 "plan":"markdown",                                              // plan
 "tool_input":{...}, "summary":"rm -rf build",                   // permission
 "screen":"终端可见区域文本"}                                     // screen（hook 覆盖不到的 TUI 对话框）

// Resolution（按 kind）
{"answers":{"<question 原文>":"所选 label；多选用 \", \" 连接；或自定义文本"}}   // question
{"approve":true,"mode":"bypassPermissions|acceptEdits|default"}               // plan 批准
{"approve":false,"feedback":"改成……"}                                        // plan 打回
{"allow":true,"always":false} / {"allow":false,"message":"…"}                // permission
{"keys":["down","enter"],"text":""}                                         // screen：按键 / 文本发到 pane
{"dismiss":true}                                                            // 任意：放弃，交回电脑 TUI 处理
```

## REST（App → hub）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/machines` | `{"machines":[Machine]}` |
| POST | `/api/v1/machines/{mid}/reconnect` | 重连该机器的 node |
| GET | `/api/v1/machines/{mid}/launchers` | `{"launchers":[Launcher]}` |
| GET | `/api/v1/machines/{mid}/dirs?path=` | 目录浏览：`{"path","parent","entries":[{"name","path","is_git"}],"recent":[{"path","name","last_used"}]}`；path 为空时 entries 是 home |
| GET | `/api/v1/sessions?machine_id=` | `{"sessions":[Session]}`，含运行中的（herdr）和最近 7 天的 offline 会话 |
| POST | `/api/v1/sessions` | `{"machine_id","cwd","launcher","prompt"?,"resume_session_id"?}` → `{"session":Session}` |
| GET | `/api/v1/sessions/{mid}/{sid}/items?before_seq=&limit=50` | 历史分页 `{"items":[Item],"has_more":bool}`（按 seq 升序） |
| POST | `/api/v1/sessions/{mid}/{sid}/messages` | `{"text":"…","images":[{"mime","data"}]?}`；会话 offline 时先用原启动器 `--resume` 拉起 |
| POST | `/api/v1/sessions/{mid}/{sid}/interrupt` | 发 Esc |
| POST | `/api/v1/sessions/{mid}/{sid}/keys` | `{"keys":["esc"],"text":""}` |
| GET | `/api/v1/sessions/{mid}/{sid}/screen?lines=60` | `{"text":"…","status":"working"}` |
| POST | `/api/v1/sessions/{mid}/{sid}/close` | 退出 claude 并关闭 tab（仅 jarvis 启动的会话） |
| GET | `/api/v1/assistant` | `{"machine_id","launcher","cwd","conversations":[Session]}` |
| PUT | `/api/v1/assistant` | `{"machine_id"?,"launcher"?}`，存 Postgres settings |
| POST | `/api/v1/assistant/conversations` | `{"prompt","images"?}` → `{"session"}`：写好 CLAUDE.md 后在助手目录新开会话 |
| POST | `/api/v1/ai/mail-account` | `{"text"}` → `{"account":{…AddAccount},"missing":["password"],"note"}`（cc_ds 网关，hub.json `ai` 可改） |
| GET / PUT | `/api/v1/machines/{mid}/claude-settings` | 白名单的 Claude Code 设置；PUT `{"key","value"}`（value 为 null 恢复默认） |
| GET | `/api/v1/interactions/history?machine_id=&session_id=&limit=` | `{"interactions":[{"interaction","status","resolution","resolved_at"}]}` |
| GET | `/api/v1/interactions` | `{"interactions":[Interaction]}`（待处理） |
| POST | `/api/v1/interactions/{id}/resolve` | body 为 Resolution |
| POST | `/api/v1/devices` | `{"device_id","name","kind":"iphone|watch","apns_token"?,"apns_env"?}` |

邮件见下文「邮件 API」。错误统一 `{"error":"…"}` + 4xx/5xx。

## WebSocket `/api/v1/ws`

信封 `{"type":"…", ...}`。

服务端 → 客户端：

| type | 字段 |
| --- | --- |
| `hello` | `server_version`, `client_id` |
| `machines` | `machines` |
| `sessions` | `machine_id`, `sessions`（该机器全量，变化时推） |
| `session` | `session`（单个状态变化） |
| `items` | `machine_id`, `session_id`, `items`（只推给订阅了该会话的客户端） |
| `delta` | `machine_id`, `session_id`, `text`（无头助手正在生成的整段文字；空串表示这轮结束） |
| `screen` | `machine_id`, `session_id`, `text`, `status`, `ts`（订阅中且 agent working/blocked 时约 400ms 一次，内容不变不推） |
| `interaction` | `interaction`（新的待处理，推给所有客户端） |
| `interaction_resolved` | `id` |
| `tool_call` | `call_id`, `name`, `args`（只发给已 `register_phone` 的 iPhone） |
| `ping` | — |

客户端 → 服务端：

| type | 字段 |
| --- | --- |
| `subscribe` | `machine_id`, `session_id`, `since_seq`（可选；有则补发其后的 items，否则补发最后 80 条） |
| `unsubscribe` | `machine_id`, `session_id` |
| `register_phone` | `device_name`, `tools`（`[{"name","description","input_schema"}]`，即 MCP 工具定义）, `simulator`（模拟器只在没有真机连着时接工具调用） |
| `tool_result` | `call_id`, `content`（`[{"type":"text","text"}|{"type":"image","data","mime_type"}]`）, `is_error` |
| `pong` | — |

## hub ↔ node RPC

`ssh <host> ~/.jarvis/bin/jarvisd node --stdio`（example-machine 本机直接起子进程）。stdin/stdout 每行一个 JSON。
双向都能发请求：`{"id":"h12","method":"…","params":{}}` → `{"id":"h12","result":{}}` / `{"id":"h12","error":{"message":"…"}}`；
单向事件 `{"event":"…","params":{}}`。hub 发的 id 以 `h` 开头，node 发的以 `n` 开头。stderr 是 node 日志。

hub → node 请求：

| method | params → result |
| --- | --- |
| `node.info` | → `{hostname, os, arch, home, herdr_version, claude_version}` |
| `launchers.list` | → `{launchers}` |
| `dirs.list` | `{path}` → 同 REST |
| `sessions.list` | → `{sessions}` |
| `session.start` | `{cwd, launcher, prompt?, resume_session_id?}` → `{session}` |
| `session.send` | `{session_id, text, images?}` |
| `session.keys` | `{session_id, keys, text}` |
| `session.interrupt` | `{session_id}` |
| `session.screen` | `{session_id, lines}` → `{text, status}` |
| `session.items` | `{session_id, before_seq, limit}` → `{items, has_more}` |
| `session.watch` / `session.unwatch` | `{session_id, since_seq?}`；watch 后 node 推 `session.items` / `session.screen` 事件 |
| `session.close` | `{session_id}` |
| `interaction.resolve` | `{id, resolution}` |

node → hub 事件：`session.items {session_id, items}`、`session.screen {session_id, text, status}`、`sessions.changed {sessions}`。

node → hub 请求：
- `interaction.create {interaction}` → `{resolution}`（阻塞到被处理 / 过期；过期返回 `{"dismiss":true}`）
- `mcp.tools {}` → `{tools}`；`mcp.call {name, args, session_id}` → `{content, is_error}`

## hook / mcp ↔ node（unix socket `~/.jarvis/node.sock`）

一次连接一问一答，各一行 JSON：

- `{"kind":"hook","input":<Claude Code 传给 hook 的 stdin JSON>}` → `{"output":<要打印到 stdout 的 JSON 或 null>}`
- `{"kind":"mcp.tools"}` → `{"tools":[…]}`
- `{"kind":"mcp.call","name":"…","args":{},"session_id":"…"}` → `{"content":[…],"is_error":false}`

连不上 node（hub 不在线）时 hook 立即退出 0、不输出，即 TUI 正常处理；mcp 返回错误文本。

## MCP 工具（`jarvisd mcp`）

| 工具 | 执行方 | 说明 |
| --- | --- | --- |
| `phone_*` | iPhone App | 由 App `register_phone` 声明（通讯录、日历、提醒、位置、健康、照片、剪贴板、设备信息、通知……） |
| `mail_accounts` / `mail_search` / `mail_read` | hub | 只读 |
| `mail_send` | hub | 发送前创建 `permission` 交互，手机批准后才发 |

## 邮件 API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/mail/accounts` | `{"accounts":[MailAccount]}`（不含密码） |
| POST | `/api/v1/mail/accounts` | 新增：测试连接成功才保存 → `{"account"}` |
| DELETE | `/api/v1/mail/accounts/{aid}` | |
| POST | `/api/v1/mail/accounts/{aid}/test` | `{"ok":true}` / `{"error"}` |
| GET | `/api/v1/mail/accounts/{aid}/mailboxes` | `{"mailboxes":[{"name","role","unseen","total"}]}`，role ∈ inbox/sent/drafts/trash/archive/junk/"" |
| GET | `/api/v1/mail/messages?account_id=&mailbox=INBOX&limit=50&before_uid=&q=` | `{"messages":[MailSummary],"has_more"}`；`account_id` 为空 = 所有账号收件箱合并（按日期倒序） |
| GET | `/api/v1/mail/messages/{aid}/{uid}?mailbox=INBOX` | `{"message":MailMessage}` |
| POST | `/api/v1/mail/messages/{aid}/{uid}/flags` | `{"mailbox","seen"?,"flagged"?}` |
| POST | `/api/v1/mail/send` | `{"account_id","to":[],"cc":[],"bcc":[],"subject","text","html"?,"in_reply_to"?,"references"?}` |

```jsonc
// MailAccount
{"id":"acc_…","name":"Gmail","email":"x@gmail.com","provider":"gmail|feishu|imap","auth":"password",
 "username":"…","imap_host":"imap.gmail.com","imap_port":993,"smtp_host":"smtp.gmail.com","smtp_port":465,
 "smtp_security":"tls|starttls","status":"ok|error","error":""}
// 新增时额外字段：password。provider 为 feishu 时服务器固定为 imap/smtp.larkoffice.com（993 / 465，SSL）
// MailSummary
{"account_id","mailbox","uid":123,"message_id","subject","from":{"name","email"},"to":[{"name","email"}],
 "date","snippet","seen":false,"flagged":false,"has_attachments":false}
// MailMessage = MailSummary + {"cc":[…],"text":"…","html":"…","attachments":[{"filename","mime","size"}],
//                               "in_reply_to","references":[…]}
```

## 飞书 API（lark-connector）

hub 经 example-machine 上 lark-connector 的 agent 接口（`/agent/v1`，token 从容器环境变量读）代理飞书数据，见 `server/internal/lark`。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/lark/conversations?cursor=&limit=` | `{"conversations":[LarkConversation],"next_cursor"}` |
| GET | `/api/v1/lark/conversations/{id}/messages?before=&limit=&topic=` | `{"messages":[LarkMessage],"has_older"}`，旧→新；`topic=1` 时取话题群的话题列表（每条是话题首帖） |
| GET | `/api/v1/lark/messages/{id}/thread?before=&limit=` | 话题 / 回复串里的消息，格式同上 |
| POST | `/api/v1/lark/conversations/{id}/messages` | `{"text"}` 以本人身份发送 |
| POST | `/api/v1/lark/conversations/{id}/read` | `{"position"}` 标已读 |
| GET | `/api/v1/lark/media/{key}?size=` | 图片 / 贴纸 / 头像 / 视频 / 文件原样转发（`size` 为 24 的倍数、≤624 时返回缩略图） |
| GET | `/api/v1/lark/messages/{id}/media/{key}` | 消息作用域的媒体（同上） |
| GET | `/api/v1/lark/emojis` | `{"emojis":{"<小写 key>":{"label","url"}}}`，含肤色变体 key；hub 缓存一天 |
| POST | `/api/v1/lark/messages/{id}/card-actions/{actionId}` | 以本人身份按下卡片回调按钮（body 原样转给 lark-connector） |

```jsonc
// LarkConversation
{"id":"<feedId>","name","avatar":"/api/v1/lark/media/…","is_group","is_topic","unread","at_me","muted","preview","is_mine","updated_at"}
// LarkMessage
{"id","position","sender_id","sender_name","sender_avatar","sender_type":"user|bot","is_mine",
 "type":"TEXT|POST|IMAGE|CARD|SYSTEM|FILE|MEDIA|STICKER|MERGE_FORWARD…","text":"纯文本预览","created_at",
 "recalled","edited","reply_count","thread_reply_count",
 "parts":[MessagePart],                       // lark-connector 原样（见下），媒体 URL 已改写到 /api/v1/lark/
 "reply_to":{"id","sender_name","text","recalled","parts"},
 "reactions":[{"key":"THUMBSUP","count":2,"mine":true,"users":["张三","李四"]}]}
// MessagePart（lark-connector 的 MessagePart，不改字段名）
{"kind":"text|at|link|image|file|code|emotion|media|card|docs|interactive|table",
 "text","url","refId","metadata":{…}}   // 卡片的整棵元素树在 metadata.body / header；富文本块祖先在 metadata.blocks
```

媒体改写规则：parts JSON 里所有 `"/api/media/` → `"/api/v1/lark/media/`，`"/api/messages/` → `"/api/v1/lark/messages/`
（合并转发卡片里嵌套的消息也一并改写）。
