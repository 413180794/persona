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
{"name":"cc_ds","base_url":"http://<模型网关>","model":"<模型名>"}

// Session：(machine_id, session_id) 唯一。session_id 就是 Claude Code 的 session uuid。
{"machine_id":"example-machine","session_id":"uuid","title":"AI 生成的标题或目录名","cwd":"/home/x/code/foo",
 "status":"idle|working|blocked|done|unknown|offline",   // offline = 不在 herdr 里运行，只有 transcript，可恢复
 "pane_id":"w1:p2","tab_id":"w1:t2","workspace_id":"w1","agent_name":"j-3f2a",
 "launcher":"cc_ds","model":"deepseek/v4.1-flash-chain","started_by_jarvis":true,"assistant":false,
 "updated_at":"...","last_text":"最后一条 assistant 文本的前 200 字","pending_interactions":1,
 "queued":["工作中输入、还没被取走的消息"],
 "subagents":[{"id":"a849e1f0…","session_id":"<id>@a849e1f0…","type":"Explore","description":"…","tool_use_id":"toolu_…",
                "running":true,"background":true,"started_at":"...","updated_at":"...","last_text":"…"}],   // 子 Agent 当会话读
 "loop":{"kind":"wakeup|cron","prompt":"/loop …","reason":"…","interval_seconds":1800,"next_at":"...","cron":"*/5 * * * *","ticks":12},
 "background":[{"id":"b85polnoj","kind":"shell|agent","description":"…","command":"…",   // 还没回报的后台命令 / 子代理
                "output_file":"/private/tmp/…/b85polnoj.output","started_at":"..."}]}

// Item：transcript 规范化后的一条（一个内容块一条）
{"id":"<entry uuid>#<block idx>","seq":123,            // seq = transcript 行号，单调递增，用于增量和分页
 "ts":"...","role":"user|assistant|system",
 "kind":"text|thinking|tool_use|tool_result|notice|image",
 "text":"…",                                           // text / thinking / notice / tool_result 的文本
 "tool_use_id":"toolu_…",                             // tool_use 与 tool_result 共用，App 据此合并成一张卡片
 "tool_name":"Bash", "tool_input":{...},              // 仅 tool_use
 "is_error":false,                                    // 仅 tool_result
 "queued":true,                                       // 仅 user text：工作中输入、被中途吸收的消息
 "wake":true,                                         // 仅 notice：后台任务完成时 Claude 已空闲，这条通知开启了新的一轮
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
| GET | `/api/v1/sessions/{mid}/{sid}/items?before_seq=&turns=2` | 历史分页 `{"items":[Item],"has_more":bool}`（按 seq 升序）。`turns`：往前取够 N 轮完整的「用户输入 + 回答」，页首一定是一条用户输入（最多 2000 条）；旧客户端的 `limit` 仍按条数。带 `lazy_images=1` 时图片不含数据（`image: {mime, lazy:true, size, width, height}`），显示时再取。带 `after_seq=N` 时返回 seq > N 的全部新内容（后台同步用，最多 500 条；`more_after: true` 表示截断了，App 改取最新一页） |
| GET | `/api/v1/sessions/{mid}/{sid}/items/{item id}/image` | 一条 item 的图片原始字节（Content-Type 为图片类型，可永久缓存）；item id 含 `#`，要百分号编码 |
| POST | `/api/v1/sessions/{mid}/{sid}/messages` | `{"text":"…","images":[{"mime","data"}]?}`；会话 offline 时先用原启动器 `--resume` 拉起 |
| POST | `/api/v1/sessions/{mid}/{sid}/interrupt` | 发 Esc |
| POST | `/api/v1/sessions/{mid}/{sid}/keys` | `{"keys":["esc"],"text":""}` |
| GET | `/api/v1/sessions/{mid}/{sid}/screen?lines=60&source=visible` | `{"text":"…","status":"working"}`；`source=recent` 连同窗格滚动缓冲一起读（lines 最多 20000，返回行数少于 lines 说明到头了） |
| POST | `/api/v1/sessions/{mid}/{sid}/screen/before` | `{"anchor":["屏幕上最上面几行"],"lines":200}` → `{"text","has_more","at_limit"}`：锚点之前的一段滚动历史（按内容定位；herdr 只保留约 1000 行，`at_limit` 表示到头） |
| POST | `/api/v1/machines/{mid}/spaces/{wid}/rename` | `{"name"}`：`herdr workspace rename`，并把置顶 / 屏蔽里的「机器/旧名」改成新名 |
| POST | `/api/v1/sessions/{mid}/{sid}/modes` | `{"permission_mode"?, "effort"?}`：改在跑的会话的权限模式 / 思考强度（`docs/ios.md`「思考强度和权限模式」）；会话列表里的 `permission_mode`、`effort` 是当前值。`POST /api/v1/sessions` 和 `POST /api/v1/assistant/conversations` 也收这两个字段，作为启动时的值 |
| POST | `/api/v1/sessions/{mid}/{sid}/rename` | `{"tab_name"?, "title"?}`：tab 用 `herdr tab rename`；会话名给在跑的 claude 发 `/rename <名字>`（它写 `custom-title`），没在跑就直接往 transcript 追加同样的 `custom-title` 行 |
| GET / PUT | `/api/v1/mutes` | 屏蔽，结构同 pins，另有 `allowed`（空间被屏蔽时单独开启通知的 Agent）。规则：Agent 自己在 sessions 里→屏蔽；在 allowed 里→不屏蔽；否则看它的空间。被屏蔽的 Agent / 空间不推送任何通知（完成、需要你），手机上弱化显示；hub 内存缓存，PUT 时更新 |
| GET / PUT | `/api/v1/pins` | 开发列表置顶：`{"spaces":["example-machine/app"],"sessions":["example-machine/<session id>"]}`，空间用「机器/空间名」（herdr 重启后 workspace id 会变），存 Postgres settings |
| GET | `/api/v1/machines/{mid}/fs?path=&cwd=` | 文件树：`{"path","entries":[{"name","dir","symlink","size","mtime"}],"truncated"}`，目录在前，最多 2000 项；拒绝 iCloud |
| GET | `/api/v1/machines/{mid}/git/status?cwd=` | `{"is_repo","root","branch","head","upstream","ahead","behind","files":[{"path","orig_path","index","worktree","added","deleted","binary"}],"stashes","commits":[最近 30 条]}`（只读，`GIT_OPTIONAL_LOCKS=0`） |
| GET | `/api/v1/machines/{mid}/git/diff?cwd=&path=&staged=0\|1` | `{"path","diff","truncated"}`；未跟踪文件对 /dev/null 出 diff，最多 512 KB |
| GET | `/api/v1/machines/{mid}/git/show?cwd=&hash=` | `{"commit","files","diff","truncated"}` |
| GET | `/api/v1/machines/{mid}/git/log?cwd=&skip=&limit=` | `{"commits":[…]}` 分页 |
| POST | `/api/v1/sessions/{mid}/{sid}/close` | 退出 claude 并关闭 tab（herdr 里的任何 Agent，手动启动的也行；分屏的 tab 只关它自己的窗格） |
| GET | `/api/v1/assistant` | `{"machine_id","launcher","cwd","timezone","notes_effective","conversations":[Session]}` |
| PUT | `/api/v1/assistant` | `{"machine_id"?,"launcher"?,"timezone"?,"notes"?}`，存 Postgres settings（`docs/assistant.md`） |
| POST | `/api/v1/assistant/conversations` | `{"prompt","images"?}` → `{"session"}`：写好 CLAUDE.md 后在助手目录新开会话 |
| POST | `/api/v1/assistant/warm` | `{"launcher"?}`：先起一个备用 claude 进程，新对话直接用 |
| GET / PUT | `/api/v1/assistant/routines` | `{"routines":[{id,title,prompt,time:"HH:MM",weekdays:[1..7],enabled,last_run,last_session_id,next_run}]}`，PUT 整表替换 |
| POST | `/api/v1/assistant/routines/{id}/run` | 立即运行 → `{"session_id","machine_id"}` |
| GET | `/api/v1/assistant/followups` | `{"followups":[{id,at,title,prompt,machine_id,session_id}]}`（助手用 schedule_followup 安排的） |
| DELETE | `/api/v1/assistant/followups/{id}` | 取消 |
| GET | `/api/v1/assistant/memory` | `{"files":[{name,content,updated_at}],"dir"}` |
| PUT / DELETE | `/api/v1/assistant/memory/{name}` | `{"content"}`；删除时顺带删 MEMORY.md 里的索引行 |
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
| `sessions` | `machine_id`, `sessions`（该机器全量，变化时推；每个会话带 `rev` = transcript 字节数，变了就说明有新内容） |
| `revs` | `machine_id`, `revs`（`{session_id: rev}`，只含变了的；列表里看得见的字段没变、只是 transcript 长了时发，推给所有客户端，App 据此后台拉增量） |
| `lark` | `feed_ids`（飞书有新消息 / 会话变化的会话；hub 跟随 lark-connector 的实时事件 `/agent/v1/events`，每 400ms 最多发一次，推给所有客户端） |
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
| `subscribe` | `machine_id`, `session_id`, `since_seq`（可选；有则补发其后的 items，否则补发最后两轮），`lazy_images`（可选；true 后这个连接收到的 items 都不带图片数据） |
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
| `session.screen` | `{session_id, lines, source}` → `{text, status}` |
| `fs.list` / `git.status` / `git.diff` / `git.show` / `git.log` | 同 REST（参数同名） |
| `session.items` | `{session_id, before_seq, limit}` → `{items, has_more}` |
| `session.image` | `{session_id, item_id}` → `Image{mime, data}` |
| `session.watch` / `session.unwatch` | `{session_id, since_seq?}`；watch 后 node 推 `session.items` / `session.screen` 事件 |
| `session.close` | `{session_id}` |
| `interaction.resolve` | `{id, resolution}` |

node → hub 事件：`session.items {session_id, items}`、`session.screen {session_id, text, status}`、`sessions.changed {sessions}`、
`sessions.revs {revs}`（每秒检查一次，只发变了的 rev；列表本身变了时 rev 跟着 `sessions.changed` 走）。

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
| POST | `/api/v1/mail/accounts` | 新增（密码 / 授权码）：测试连接成功才保存 → `{"account"}`；`auth:"oauth2"` 一律 400 |
| POST | `/api/v1/mail/oauth/google/start` | Gmail「使用 Google 账号登录」：`{"login_hint"?}` → `{"auth_url","callback_scheme","state"}`；App 用 ASWebAuthenticationSession 打开 `auth_url`，等 `callback_scheme` 回调；hub 未配置 `google.client_id` 时 400 |
| POST | `/api/v1/mail/oauth/google/finish` | 回调 URL 里的 `{"state","code"}`（或 `{"state","error"}`）→ hub 换 refresh token、验证 IMAP + Gmail API（发信走 Gmail API）后保存 → `{"account"}`；同一邮箱再登录一次 = 更新凭据（ID 不变）；state 一次性、15 分钟过期 |
| DELETE | `/api/v1/mail/accounts/{aid}` | |
| POST | `/api/v1/mail/accounts/{aid}/test` | `{"ok":true}` / `{"error"}` |
| GET | `/api/v1/mail/accounts/{aid}/mailboxes` | `{"mailboxes":[{"name","role","unseen","total"}]}`，role ∈ inbox/sent/drafts/trash/archive/junk/"" |
| GET | `/api/v1/mail/messages?account_id=&mailbox=INBOX&limit=50&before_uid=&q=` | `{"messages":[MailSummary],"has_more"}`；`account_id` 为空 = 所有账号收件箱合并（按日期倒序） |
| GET | `/api/v1/mail/messages/{aid}/{uid}?mailbox=INBOX` | `{"message":MailMessage}` |
| POST | `/api/v1/mail/messages/{aid}/{uid}/flags` | `{"mailbox","seen"?,"flagged"?}` |
| POST | `/api/v1/mail/send` | `{"account_id","to":[],"cc":[],"bcc":[],"subject","text","html"?,"in_reply_to"?,"references"?}` |

```jsonc
// MailAccount
{"id":"acc_…","name":"Gmail","email":"x@gmail.com","provider":"gmail|feishu|imap","auth":"password|oauth2",
 "username":"…","imap_host":"imap.gmail.com","imap_port":993,"smtp_host":"smtp.gmail.com","smtp_port":465,
 "smtp_security":"tls|starttls","status":"ok|error","error":""}
// 新增时额外字段：password。provider 为 feishu 时服务器固定为 imap/smtp.larkoffice.com（993 / 465，SSL）
// MailSummary
{"account_id","mailbox","uid":123,"message_id","subject","from":{"name","email"},"to":[{"name","email"}],
 "date","snippet","seen":false,"flagged":false,"has_attachments":false}
// MailMessage = MailSummary + {"cc":[…],"text":"…","html":"…","attachments":[{"filename","mime","size"}],
//                               "in_reply_to","references":[…]}
```

## 收件箱 API

hub 后台采集飞书和邮件，聚合成「件」（`docs/inbox.md`，`server/internal/hub/inbox.go`）。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/inbox` | `{items:[InboxItem]}`：未处理的 + 3 天内处理过的，按 `updated_at` 倒序；不含 `context` |
| GET | `/api/v1/inbox/{id}` | 一件，带 `context`（这段对话 / 邮件的纯文本，给 AI 用） |
| POST | `/api/v1/inbox/{id}/done` | `{done: bool}`：标已处理 / 移回，返回这件 |
| POST | `/api/v1/inbox/{id}/draft` | `{instruction?}` → `{text}`：按用户口吻起草的回复（不发送） |
| POST | `/api/v1/inbox/{id}/reply` | `{text}`：飞书发到原会话 / 邮件按回复发出，并标为已处理（`done_by=replied`），返回这件 |
| POST | `/api/v1/inbox/refresh` | 立刻采集一次，返回列表 |

`InboxItem`：`id`、`source`（lark / mail）、`kind`（dm / mention / group / bot / mail）、`account`（`feishu` 或邮箱账号 id）、
`account_name`、`title`（会话名 / 邮件主题）、`sender`、`avatar`、`preview`（最新一条）、`count`、`started_at`、`updated_at`、
`tier`（action / fyi / noise，分拣前为空）、`summary`、`reason`、`read`（在别处看过）、`done_at`、`done_by`（user / read / replied）、
`lark{feed_id,is_group,is_topic,start_position,mention_position,at_all}`、`mail{account_id,mailbox,uids,message_id,from,direct,thread}`。
列表变化时 WebSocket 推 `{"type":"inbox"}`（不带内容，App 自己拉）。

## 飞书 API（lark-connector）

hub 经 example-machine 上 lark-connector 的 agent 接口（`/agent/v1`，token 从容器环境变量读）代理飞书数据，见 `server/internal/lark`。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/lark/conversations?scope=&cursor=&limit=` | `{"conversations":[LarkConversation],"next_cursor"}`；scope：`chats`（默认，飞书「消息」：不含已完成 / 折叠，按标签显示规则过滤）、`unread`、`done`（已完成）、`label:<id>`、`inbox`、`mentions`、`muted`、`p2p`、`group`、`thread`、`later` |
| GET | `/api/v1/lark/pinned` | 全部置顶会话（读 inbox + done 两个 scope 的头部合并，按置顶顺序） |
| GET | `/api/v1/lark/labels` | `{"labels":[{"id","name","count"}]}` |
| GET | `/api/v1/lark/unread-count` | `{"count"}`：「未读 N」（不含免打扰） |
| GET | `/api/v1/lark/flags` | 标记：`{"conversations":[LarkConversation],"messages":[{"feed_id","chat_name","chat_avatar","flagged_at","message":LarkMessage}]}` |
| GET | `/api/v1/lark/conversations/{id}/labels` | `{"label_ids":[…],"labels":[{"id","name"}]}`（这个会话在哪些标签里 / 可选标签） |
| POST | `/api/v1/lark/conversations/{id}/settings` | `{"done"?,"pinned"?,"flagged"?,"muted"?,"label_ids"?}` → lark-connector `PATCH settings`（isArchived / isShortcut / isFlagged / isMuted / labelIds，labelIds 是全量） |
| POST | `/api/v1/lark/conversations/{id}/unread` | `{"unread":true}` 标为未读 / `false` 清除未读 |
| GET | `/api/v1/lark/conversations/{id}/messages?before=&after=&around=&limit=&topic=` | `{"messages":[LarkMessage],"has_older","has_newer","older_cursor","newer_cursor"}`，旧→新；不带游标 = 最新一页；`around=<读到的位置+1>` 从第一条未读打开；`topic=1` 时取话题群的话题列表（只能往前翻） |
| GET | `/api/v1/lark/messages/{id}/thread?before=&limit=` | 话题 / 回复串里的消息，格式同上 |
| POST | `/api/v1/lark/conversations/{id}/messages` | `{"text"}` 以本人身份发送 |
| POST | `/api/v1/lark/conversations/{id}/read` | `{"position","badge_count"?,"message_ids"?}`：上报看到的消息（最高位置、该条的 badge_count、其间别人发的未读消息 id），只在消息真正上屏后发 |
| GET | `/api/v1/lark/media/{key}?size=` | 图片 / 贴纸 / 头像 / 视频 / 文件原样转发（`size` 为 24 的倍数、≤624 时返回缩略图） |
| GET | `/api/v1/lark/messages/{id}/media/{key}` | 消息作用域的媒体（同上） |
| GET | `/api/v1/lark/emojis` | `{"emojis":{"<小写 key>":{"label","url"}}}`，含肤色变体 key；hub 缓存一天 |
| POST | `/api/v1/lark/messages/{id}/card-actions/{actionId}` | 以本人身份按下卡片回调按钮（body 原样转给 lark-connector） |

```jsonc
// LarkConversation
{"id":"<feedId>","name","avatar":"/api/v1/lark/media/…","is_group","is_topic","unread","marked_unread","at_me","at_all","muted",
 "pinned","flagged","done","tags":["外部"|"公开"|"会议"|"机器人"|"AI"…],"preview","preview_sender","is_mine","updated_at",
 "read_position","mention_position"}
// LarkMessage
{"id","position","sender_id","sender_name","sender_avatar","sender_type":"user|bot","is_mine",
 "type":"TEXT|POST|IMAGE|CARD|SYSTEM|FILE|MEDIA|STICKER|MERGE_FORWARD…","text":"纯文本预览","created_at",
 "recalled","edited","reply_count","thread_reply_count","badge_count","at_me","is_read",
 "parts":[MessagePart],                       // lark-connector 原样（见下），媒体 URL 已改写到 /api/v1/lark/
 "reply_to":{"id","sender_name","text","recalled","parts"},
 "reactions":[{"key":"THUMBSUP","count":2,"mine":true,"users":["张三","李四"]}]}
// MessagePart（lark-connector 的 MessagePart，不改字段名）
{"kind":"text|at|link|image|file|code|emotion|media|card|docs|interactive|table",
 "text","url","refId","metadata":{…}}   // 卡片的整棵元素树在 metadata.body / header；富文本块祖先在 metadata.blocks
```

媒体改写规则：parts JSON 里所有 `"/api/media/` → `"/api/v1/lark/media/`，`"/api/messages/` → `"/api/v1/lark/messages/`
（合并转发卡片里嵌套的消息也一并改写）。

## 消息里的标签（客户端解析）

| 标签 | 谁写 | 含义 / 显示 |
| --- | --- | --- |
| `<attached_images>` 每行一个路径 `</attached_images>` | hub（手机发图） | 消息附带的图片；气泡上方方形缩略图 |
| `<jarvis-context kind="context\|edit" title="…" url="…">文字</jarvis-context>` | 手机（链接卡片里选中文字） | 关联上下文 / 要改的内容；气泡上方引用卡片 |
| `<cross-session-message from-name="…">…</cross-session-message>` | Claude Code（会话间消息） | 左侧「来自 名字」卡片 |

