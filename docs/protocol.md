# 协议（前后端契约）

所有 JSON 字段 snake_case。时间为 RFC3339（UTC）。hub 默认监听 `:18660`。

## 鉴权

- `Authorization: Bearer <token>`；WebSocket 也可用 `?token=`。token 在 hub 机器 `~/.jarvis/hub.json` 的 `token`，
  `jarvisd hub pair` 打印 URL + token（以及终端二维码，App 扫码配对）。
- **每台设备自己的凭据**：配对用的 token（hub.json）什么都能做，留给 CLI 和旧 App。新 App 配对后立刻
  `POST /api/v1/auth/tokens {"name","kind":"phone"}` 换一个自己的 token（`jd_…`，hub 只存哈希），之后只用它；再给配对的手表换一个
  `kind: watch` 的，手表只拿到这个。角色：`full`（手机）什么都能做；`answer`（手表）能读（所有 GET、WebSocket）、回答交互、发消息、
  标已读 / 中断、收件箱回复和完成、注册推送，别的写操作一律 403，WebSocket 上也不能注册成手机执行 `phone_*` 工具。
  `GET /api/v1/auth/tokens` 列出（`current` 是本机），`DELETE /api/v1/auth/tokens/{id}` 撤销：立即失效（查询结果缓存 30 秒，撤销时清掉）
  并断开它的 WebSocket。App 在「设置 › 设备」里管理。旧 hub 没有这个接口时 App 继续用配对 token，下次启动再试。
- `GET /api/v1/health` 不需要鉴权，返回 `{"ok":true,"version":"..."}`。
- 功能协商：`/api/v1/health` 和 WebSocket 的 `hello` 带 `features`（`proto.Features`：`client_msg_id`、`send_refusals`、
  `screen_dialog`、`resumable_interactions`、`secret_prompts`、`request_id`）。新字段都是可选的，App 目前靠字段在不在判断；
  以后有「旧 hub 会误解」的新请求时先查这里。机器列表里的 `node_version` / `node_mismatch`：那台电脑上的 jarvisd 不是 hub 这次部署的
  （`deploy.sh` 只部署了一部分），App 在「设置 › 机器」里提示重新部署，hub 连上时也记一行日志。
- 每个响应带 `X-Request-Id`；请求可带 `X-Client-Request-Id`（4–32 位十六进制 / `-`），hub 就用它（`docs/server.md`「运维」）。

## 数据类型

```jsonc
// Machine
{"id":"laptop","name":"笔记本","kind":"ssh","ssh_host":"laptop","online":true,"error":"",
 "hostname":"example-machine","os":"darwin","arch":"arm64","herdr_version":"0.8.0","claude_version":"2.1.310",
 "last_seen":"...",
 // 系统信息（docs/machines.md）：node 最近一次连上时报的，存在 hub 的数据库里，离线也有；旧 node 没有这个字段
 "system":{"os":"darwin","distro":"macos","like":"","name":"macOS 26.1","version":"26.1","build":"25B78",
           "kernel":"Darwin 25.1.0","arch":"arm64","model":"MacBook Pro","cpu":"Apple M4 Pro","cores":14,
           "memory":51539607552,"boot_time":1790000000}}

// MetricSample：一次读数（每 10 秒）。没测到的字段缺省（第一次读数没有 cpu / net_*），不是 0；cores 只有 Linux 有
{"t":1790003600,"cpu":42.5,"cores":[40,45],"load":3.1,"mem_used":34359738368,"mem_total":68719476736,
 "swap_used":0,"swap_total":0,"net_rx":1024.5,"net_tx":512,"disk_used":10737418240,"disk_total":107374182400}

// MetricPoint：历史里的一步（该步内的平均）；gap 表示这里断开了，画图不要连线。mem / swap / disk 是已用字节
{"t":1790000000,"cpu":10.2,"load":1.25,"mem":34359738368,"swap":0,"rx":2048,"tx":512,"disk":10737418240}
{"t":1790000240,"gap":true}

// Launcher（来自 zsh 的 _claude_launchers）
{"name":"cc_ds","base_url":"http://<模型网关>","model":"<模型名>"}

// Session：(machine_id, session_id) 唯一。session_id 就是 Claude Code 的 session uuid。
{"machine_id":"server","session_id":"uuid","title":"AI 生成的标题或目录名","cwd":"/home/x/code/foo",
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
 "full_size":4874,                                    // text 被截断了（页和推送里工具输出最多 4 KB、其他文字 24 KB），全文的字节数；
                                                      // 截断的 text 末尾是一行说明：工具输出是「…（已截断，点开看全文，共 N 字节）」（App 有「看全文」按钮），
                                                      // 其他文字是「…（已截断，共 N 字节）」；旧 App 照样能看懂
 "queued":true,                                       // 仅 user text：工作中输入、被中途吸收的消息
 "wake":true,                                         // 仅 notice：后台任务完成时 Claude 已空闲，这条通知开启了新的一轮
 "image":{"mime":"image/png","data":"base64"}}        // 仅 image（tool_result 里的图片也拆成 image item，带 tool_use_id）

// Interaction：需要人处理的事
{"id":"ix_…","machine_id":"server","session_id":"uuid","session_title":"…","cwd":"…",
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
| GET | `/api/v1/sessions/{mid}/{sid}/items?before_seq=&turns=2` | 历史分页 `{"items":[Item],"has_more":bool}`（按 seq 升序）。`turns`：往前取够 N 轮完整的「用户输入 + 回答」，页首一定是一条用户输入（最多 2000 条；一轮特别长、往前要解析几十 MB 时这一页会短一些，`has_more: true`，再往前翻接着取）；旧客户端的 `limit` 仍按条数。带 `lazy_images=1` 时图片不含数据（`image: {mime, lazy:true, size, width, height}`），显示时再取。带 `after_seq=N` 时返回 seq > N 的新内容（后台同步用，约 500 条一页，从不切开同一行的多个 item；`more_after: true` 表示后面还有，接着用最后一条的 seq 再取，或改取最新一页） |
| GET | `/api/v1/sessions/{mid}/{sid}/items/{item id}/image` | 一条 item 的图片原始字节（Content-Type 为图片类型，可永久缓存）；item id 含 `#`，要百分号编码 |
| GET | `/api/v1/sessions/{mid}/{sid}/items/{item id}/full` | 一条 item 的全文（`Item`，不带图片、没有 `full_size`；最多 1 MB）：App 点「看全文」时取。node 重新读那一行解析 |
| POST | `/api/v1/sessions/{mid}/{sid}/messages` | `{"text":"…","images":[{"mime","data"}]?,"client_msg_id"?,"clear_draft"?}`；会话 offline 时先用原启动器 `--resume` 拉起；被拒见下面「发消息」 |
| POST | `/api/v1/sessions/{mid}/{sid}/interrupt` | 发 Esc |
| POST | `/api/v1/sessions/{mid}/{sid}/keys` | `{"keys":["esc"],"text":""}` |
| GET | `/api/v1/sessions/{mid}/{sid}/screen?lines=60&source=visible` | `{"text":"…","status":"working"}`；`source=recent` 连同窗格滚动缓冲一起读（lines 最多 20000，返回行数少于 lines 说明到头了）；`format=ansi` 带颜色（SGR，行尾 `\r\n`；旧 node 不认，照旧回纯文本） |
| POST | `/api/v1/sessions/{mid}/{sid}/screen/before` | `{"anchor":["屏幕上最上面几行"],"lines":200,"format"?:"ansi"}` → `{"text","has_more","at_limit"}`：锚点之前的一段滚动历史（按内容定位，锚点按纯文本比；herdr 只保留约 1000 行，`at_limit` 表示到头） |
| POST | `/api/v1/machines/{mid}/spaces/{wid}/rename` | `{"name"}`：`herdr workspace rename`，并把置顶 / 屏蔽里的「机器/旧名」改成新名 |
| POST | `/api/v1/sessions/{mid}/{sid}/modes` | `{"permission_mode"?, "effort"?}`：改在跑的会话的权限模式 / 思考强度（`docs/ios.md`「思考强度和权限模式」）；会话列表里的 `permission_mode`、`effort` 是当前值。`POST /api/v1/sessions` 和 `POST /api/v1/assistant/conversations` 也收这两个字段，作为启动时的值 |
| POST | `/api/v1/sessions/{mid}/{sid}/rename` | `{"tab_name"?, "title"?}`：tab 用 `herdr tab rename`；会话名给在跑的 claude 发 `/rename <名字>`（它写 `custom-title`），没在跑就直接往 transcript 追加同样的 `custom-title` 行 |
| GET / PUT | `/api/v1/mutes` | 屏蔽，结构同 pins，另有 `allowed`（空间被屏蔽时单独开启通知的 Agent）。规则：Agent 自己在 sessions 里→屏蔽；在 allowed 里→不屏蔽；否则看它的空间。被屏蔽的 Agent / 空间不推送任何通知（完成、需要你），手机上弱化显示；hub 内存缓存，PUT 时更新 |
| GET / PUT | `/api/v1/pins` | 开发列表置顶：`{"spaces":["server/app"],"sessions":["server/<session id>"]}`，空间用「机器/空间名」（herdr 重启后 workspace id 会变），存 Postgres settings |
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
| GET / PUT | `/api/v1/assistant/routines` | `{"routines":[{id,title,prompt,time:"HH:MM",weekdays:[1..7],enabled,created_by,last_run,last_session_id,last_status,last_error,next_run}]}`，PUT 整表替换（只看用户能改的字段，其余保留 hub 的）；`last_status` ok / failed / skipped，`created_by: "assistant"` 是助手在对话里建的 |
| POST | `/api/v1/assistant/routines/{id}/run` | 立即运行 → `{"session_id","machine_id"}` |
| GET | `/api/v1/assistant/followups` | `{"followups":[{id,at,title,prompt,machine_id,session_id}]}`（助手用 schedule_followup 安排的） |
| DELETE | `/api/v1/assistant/followups/{id}` | 取消 |
| POST | `/api/v1/assistant/followups/{id}/run` | 立即运行（从列表里拿掉）→ `{"session_id","machine_id"}` |
| GET | `/api/v1/assistant/runs[?ref=<id>]` | 运行记录，新的在前：`{"runs":[{id,kind:"routine"\|"followup",ref_id,title,due,at,status,error,manual,machine_id,session_id}]}` |
| GET | `/api/v1/assistant/memory` | `{"files":[{name,content,updated_at}],"dir"}` |
| PUT / DELETE | `/api/v1/assistant/memory/{name}` | `{"content"}`；删除时顺带删 MEMORY.md 里的索引行 |
| POST | `/api/v1/ai/mail-account` | `{"text"}` → `{"account":{…AddAccount},"missing":["password"],"note"}`（cc_ds 网关，hub.json `ai` 可改） |
| GET / PUT | `/api/v1/machines/{mid}/claude-settings` | 白名单的 Claude Code 设置；PUT `{"key","value"}`（value 为 null 恢复默认） |
| GET | `/api/v1/interactions/history?machine_id=&session_id=&limit=` | `{"interactions":[{"interaction","status","resolution","resolved_at"}]}` |
| GET | `/api/v1/interactions` | `{"interactions":[Interaction]}`（待处理） |
| POST | `/api/v1/interactions/{id}/resolve` | body 为 Resolution |
| POST | `/api/v1/devices` | `{"device_id","name","kind":"iphone|watch","apns_token"?,"apns_env"?}` |
| POST | `/api/v1/devices/{id}/live-activity` | 实时活动的 token：`{"push_to_start_token"}`、`{"activity_id","push_token"}`（会轮换，重复发）、`{"activity_id","ended":true}`（`docs/live-activity.md`） |
| GET | `/api/v1/fleet` | `{"fleet": FleetState}`：当前的实时活动内容（App 回到前台时拉），`stats` 带全部数字和每台机器的负载 |
| GET | `/api/v1/metrics` | `{"machines": [{machine_id, online, system, latest: MetricSample \| 无, points: [MetricPoint]}], "step": 60}`：机器监控的 Dashboard，按配置顺序的每台机器，`points` 是最近 1 小时、每分钟一个点；`latest` 只在 30 秒内收到过样本时有（`docs/machines.md`） |
| GET | `/api/v1/machines/{id}/metrics?range=1h\|6h\|24h` | `{"metrics": {同上一台}, "range", "step"}`：机器详情的曲线，步长 10 / 60 / 240 秒（约 360 个点）；没有这台机器 404，range 不对 400 |
| GET | `/api/v1/usage` | `{"usage": {tpm, tokens, output, requests, machines:[{id, tpm, tokens, load}]}}`：所有机器的 token 用量（今天按手机时区算，`load` 是 1 分钟负载 / 核数），见 `docs/live-activity.md`「用量」 |
| GET / PUT | `/api/v1/devices/{id}/prefs` | 这台设备要哪些推送：`{"input":true,"done":"off|long|always"}`，另有 `live_activity`（默认 true）、`lock_details`（锁屏显示详情，默认 false）、`island`（灵动岛两侧：`{"preset":"a|b|c|custom","left","right","machine"}`，默认 `a`）；PUT 可以只带要改的字段；返回值另带 hub 的 `long_turn_seconds`、`done_delay_seconds`（只读）。设备还没注册推送时也能设 |

邮件见下文「邮件 API」。错误统一 `{"error":"…"}` + 4xx/5xx。

## WebSocket `/api/v1/ws`

信封 `{"type":"…", ...}`。

服务端 → 客户端：

| type | 字段 |
| --- | --- |
| `hello` | `server_version`, `client_id` |
| `machines` | `machines` |
| `sessions` | `machine_id`, `sessions`（该机器全量，变化时推；每个会话带 `rev` = transcript 字节数，变了就说明有新内容；可选 `suggestion`：Claude 一轮结束后在输入框里给的灰色「下一条」建议，见 `docs/claude-integration.md`） |
| `revs` | `machine_id`, `revs`（`{session_id: rev}`，只含变了的；列表里看得见的字段没变、只是 transcript 长了时发，推给所有客户端，App 据此后台拉增量） |
| `lark` | `feed_ids`（飞书有新消息 / 会话变化的会话；hub 跟随 lark-connector 的实时事件 `/agent/v1/events`，每 400ms 最多发一次，推给所有客户端） |
| `session` | `session`（单个状态变化） |
| `items` | `machine_id`, `session_id`, `items`，以及这批在 transcript 里的位置（见下「items 的连续性」）：`history`, `to_seq`, `from_seq`（接续的批次才有）。只推给订阅了该会话的客户端；没有 item 的行（ai-title 之类）也会推一条空的 `items`，让 App 知道这几行已经看过 |
| `delta` | `machine_id`, `session_id`, `text`（无头助手正在生成的整段文字；空串表示这轮结束） |
| `screen` | `machine_id`, `session_id`, `text`, `status`, `ts`（node 已不再随订阅采样屏幕：终端页自己按需读 `/screen`，旧 App 收不到这条也不影响） |
| `terminal_control` | `machine_id`, `session_id`, `state`：`active`（接管了，带 `cols`, `rows`）、`busy`（电脑上别的程序占着，问用户要不要 `takeover`；先镜像着）、`taken`（被别的设备或程序抢走）、`lost`（node 自己放手了）、`unsupported`（旧 node / herdr，只能镜像），`error` 是给人看的原因 |
| `terminal` | `machine_id`, `session_id`，以及一帧 `epoch`, `seq`, `full`, `cols`, `rows`, `data`（base64 的 VT 字节），或者 `error`（流停了：电脑离线、会话不在跑……）+ `unsupported`（旧 node / herdr 不能推流，改用 `/screen` 轮询）。见下「终端流」 |
| `interaction` | `interaction`（新的待处理，推给所有客户端） |
| `interaction_resolved` | `id` |
| `tool_call` | `call_id`, `name`, `args`（只发给已 `register_phone` 的 iPhone） |
| `ping` | — |

客户端 → 服务端：

| type | 字段 |
| --- | --- |
| `subscribe` | `machine_id`, `session_id`, `since_seq`（可选，App 传本地的 `covered`；有则补发其后的全部 items——每页一条 `items`（`backlog: true`），最多 8 页 / 4000 条，超出时最后一条带 `more_after: true`；否则补发最后两轮），`lazy_images`（可选；true 后这个连接收到的 items 都不带图片数据） |
| `unsubscribe` | `machine_id`, `session_id` |
| `terminal_subscribe` | `machine_id`, `session_id`, `epoch`（可选）, `after_seq`（可选）：开始收这个会话终端的实时帧（hub `features` 里有 `terminal_stream` 才发）；`control: {cols, rows}`（可选，`terminal_control`）：手机按这个尺寸接管终端，`takeover: true` 连别的程序占着的也抢 |
| `terminal_input` | `machine_id`, `session_id`, `text`（终端字节，原样写进去）, `control: {cols, rows}`（可选，新尺寸）, `scroll`（可选，滚轮的行数，> 0 往回、< 0 往新，feature `terminal_scroll`）, `at`（可选，`[column, row]` 从 0 起，滚轮落在屏幕的哪一格：手指按下的地方；不带时 node 用屏幕正中间）, `click`（可选，`[column, row]` 从 0 起，屏幕上那一格左键单击，feature `terminal_click`）：只有正在接管的那个连接发的才转给 node |
| `terminal_unsubscribe` | `machine_id`, `session_id` |
| `register_phone` | `device_name`, `tools`（`[{"name","description","input_schema"}]`，即 MCP 工具定义）, `simulator`（模拟器只在没有真机连着时接工具调用） |
| `tool_result` | `call_id`, `content`（`[{"type":"text","text"}|{"type":"image","data","mime_type"}]`）, `is_error` |
| `pong` | — |

### 终端对话框（screen 交互）

Agent blocked 而没有 hook 交互时，hub 读一次屏幕（`session.screen`，40 行）建一张 `screen` 卡：

- hub 用 `server/internal/screen` 解析对话框，放进 `interaction.dialog`：`header`、`question`、`detail`、
  `options[{key,label,detail,kind,keys}]`（`kind`：`type_something` / `chat` / 空）、`rev`（问题和选项的哈希，不含光标和转圈）。
  有编号的选项按数字；Claude 2.1.312 信任目录那种不带编号的列表按光标算 `up/down` + `enter`。卡片 id 带上 `rev`：对话框换了就是新卡，
  上一张卡里选的不会带过来。
- App 回答带 `option`（`type_something` 再带 `text`），不再自己猜按键；hub 把 `rev` 和 `option` 交给 node 的 `session.keys`，
  node **按键之前重读屏幕、重新解析、比对 `rev`**，不一致就什么都不按，回 409 `{"code":"prompt_changed"}`，hub 随即按当前屏幕再建一张卡。
  按键也在 node 上按当时的屏幕算（光标在电脑上被挪过也不会选错）。
- 兼容：旧 App 不认 `dialog`，照旧自己解析 `screen`、发 `keys`——hub 也会带上 `rev`，同样先核对；旧 node 不认 `rev` / `option`，
  hub 同时传算好的 `keys`，行为同以前。没解析出对话框的卡（只有终端和按键栏）不核对。
- 屏幕卡的回答是同步的：hub 等 node 按完才回 App（以前是先回成功再在后台按）。
- `rev` 由 hub 和 node 各自用同一份解析代码算：改了 `internal/screen` 的解析就要 hub 和所有 node 一起部署，否则新卡永远对不上（每次都 `prompt_changed`）。
- 手表有 `dialog` 时也按 `option` 回答（Siri「批准第一个待处理」不处理屏幕卡）。
- 密码：屏幕最后一行是密码提示（`[sudo] password for …:`、`Password:`、ssh 口令…，规则刻意收窄，正文里提到 password 不算）时卡片带
  `secret: true`，App 用密码框（不进草稿）。不管是不是密码，终端卡回答里的 `text` 在数据库里一律存成 `<redacted>`；
  终端面板的按键接口（`/keys`）本来就不落库，hub 的请求日志也只记路径和状态码。

### 发消息

往别人的终端里打字是这个 App 风险最大的操作。node 在往正在跑的 Agent 窗格里打字之前先读一次屏幕（`server/internal/node/sendsafe.go`、
`server/internal/screen`），以下情况**什么都不打**，返回 409：

| `code` | 什么时候 | 其他字段 |
| --- | --- | --- |
| `dialog_open` | 有对话框占着键盘：权限确认、信任目录、模型选择、rewind…（底部有 `Esc to cancel` / `Enter to confirm`，或输入框下面有带光标的编号选项） | `delivered: "none"` |
| `input_draft` | Claude 的输入框里有电脑上没发出去的字、bash 模式（`!`）、粘贴占位、被窗格截掉的长草稿 | `delivered: "none"`、`draft`（输入框里的字）、`clearable`（只有一行字时才有） |

- 灰色的建议输入（Claude 预测的下一句、新会话的 `Try "…"`）不算草稿：要用带颜色的读屏（`pane.read` 的 `format: ansi`）看 SGR 2（dim），
  纯文本里两者一模一样。读不到颜色时一律当草稿。规则移植自 Ovis 的 `claude-input.ts`（MIT）。夹具：`server/internal/screen/testdata`。
- `clear_draft`：用户在 App 里看到草稿后选「清空并发送」，App 带上收到的 `clearable` 重发同一条（同一个 `client_msg_id`）；
  node 只在输入框里**还是这段字**时按 `ctrl+k ctrl+u` 清掉，再读一次确认空了才发。多行草稿只能在电脑上处理（这两个键一次只清一行）。
- `client_msg_id`：App 每条待发消息一个 UUID，重发沿用。node 记 10 分钟：已送达的重发直接回成功；被拒的（什么都没打）忘掉，可以再发；
  打字过程中出错的回 502 + `delivered: "unknown"`（可能已经到了），同一个 id 再发也只回同样的错误，不会打第二遍。
- Codex / TRAEX 只查对话框（`› 1.` 这样的编号选择）。旧 App 只认 `error` 字段，看到的就是拒绝原因。

### 发送文件

App 对话页输入框的「+」›「文件」（Agents 和「对话」tab 的助手都有）：选的文件传到**会话所在那台电脑**的临时目录（node 的
`os.TempDir()`：Linux 一般 `/tmp`，macOS 是 `$TMPDIR`，ssh 进来的 node 没有这个变量时也是 `/tmp`），把绝对路径插到输入框的光标处，
**不自动发送**。助手跑在它所在的机器上（默认 hub 机器），文件就传到那台。图片照旧走图片附件。

- `POST /api/v1/machines/{mid}/files?name=<原文件名>`，body 是文件本身（必须有 `Content-Length`）→ `{path, name, size}`。
  上限 200 MB（`proto.MaxUploadBytes`）：超过回 413 `code: too_large`（App 选完就先查大小，不会传）。
- hub 边读 body 边转：每 1 MB 一个 `file.upload.chunk`（base64 在一行 JSON 里），整个文件不进内存、也不塞进一条 RPC。
  App 取消上传（或断线）时 hub 发 `file.upload.abort`，node 删掉写了一半的文件；node 2 分钟收不到块也自己删。
- node 先写 `.jarvis-upload-*` 隐藏文件（0600），收齐后核对字节数，再改名为 `原名-YYYYMMDD-HHMMSS.扩展名`（`.tar.gz` 这类整体算扩展名），
  同一秒重名再加 `-xxxx` 随机串。文件名里的 `/`、`\`、控制字符换成 `_`，超长的截短。
- 旧 node 不认 `file.upload.*`：409 `code: node_outdated`（「<机器> 上的 jarvisd 太旧…需要重新部署」）；旧 hub 没有这个接口（404），
  hello 的 `features` 里也没有 `file_upload`：App 提示更新 hub。
- 路径带空格或引号时 App 用单引号包起来，多个路径空格分隔。输入框没在编辑时插到末尾（未聚焦的 TextField 报的选区不可靠）。
- 前面挡着反向代理时注意它的请求体上限（有的 CDN 免费版只放 100 MB）。

### items 的连续性

每批 items（WebSocket `items` 事件、HTTP 的 `/items` 响应、node 的 `session.items`）都说明自己在 transcript 里的位置：

- `history`：这份 transcript 的身份（inode + 第一行的 crc32 + 被改写的次数）。变了说明文件被替换 / 截短重写，App 丢掉本地内容重新取。
- `to_seq`：这批读到的最后一行（包括没有 item 的行）；`from_seq`：这批接在哪一行之后（只有接续的批次——补发、实时推送、`after_seq`——才有）。
- App 记 `covered`（本地从头连续拿到的最后一行）：`from_seq <= covered` 就把 `covered` 推到 `to_seq`；`from_seq > covered` 说明中间丢了一批，
  稍等 1.5 秒（订阅时的补发可能还在路上）再用 `after_seq=covered` 补齐，超过 8 页改取最新一页。重连订阅带 `since_seq=covered`。
- 老 hub 不带这些字段：App 照旧按 id 合并。新 hub 的字段对老 App 只是多出来的键。

### 终端流

终端 tab 的实时画面（`docs/terminal.md`）。node 用 `herdr terminal session observe` 按窗格本身的尺寸镜像 herdr 画好的网格，
每帧是一段 VT 字节：`full` 的帧是整屏（`ESC[2J` 开头），其余只重画变了的格子，最后是光标位置和显隐。App 用一个 `cols × rows`
的网格依次套用即可；网格永远等于电脑上窗格的尺寸，手机的尺寸不回传。

- `epoch` 标识一条流：node 每次（重新）开 observe 都换一个，第一帧一定是 `full`；窗格变大小、herdr 中断、整帧之后的增量超过 256 KB 时 node 会重开。
  同一 `epoch` 里 `seq` 从 1 连续递增。
- hub 给每个被看的终端留「最近一个整帧 + 之后的增量」。`terminal_subscribe` 带上 App 手里的 `epoch` 和最后的 `seq`：
  还在同一条流、而且留着的帧能接上，就只补之后的帧；否则从整帧开始重放。之后的新帧实时推。
- App 收到的帧 `seq` 不连续（中间丢了）或者 `epoch` 变了却不是 `full`，就丢掉本地网格、重新 `terminal_subscribe`（不带 epoch）。
- 收到 `error`：hub 已经丢掉这条流；`unsupported` 时改用 `/screen` 轮询，否则过几秒再订阅。WebSocket 断了重连后重新订阅（带 epoch / seq 续上）。
- 第一个订阅的 App 让 hub 发 `terminal.watch`，最后一个退订（或断开）时发 `terminal.unwatch`。旧 App 不发这些消息，照旧轮询 `/screen`。
- 手机接管（`features` 有 `terminal_control`，`docs/terminal.md` 第十一节）：订阅带 `control` 时网格是手机的尺寸，帧格式不变；
  键和新尺寸走 `terminal_input`；离开（退订 / 断开）后窗格回到电脑上的尺寸，还有别人在看就改回镜像（新 epoch）。

## hub ↔ node RPC

`ssh <host> ~/.jarvis/bin/jarvisd node --stdio`（hub 机器本机直接起子进程）。stdin/stdout 每行一个 JSON。
双向都能发请求：`{"id":"h12","method":"…","params":{}}` → `{"id":"h12","result":{}}` / `{"id":"h12","error":{"message":"…"}}`；
单向事件 `{"event":"…","params":{}}`。hub 发的 id 以 `h` 开头，node 发的以 `n` 开头。stderr 是 node 日志。

hub → node 请求：

| method | params → result |
| --- | --- |
| `node.info` | → `{hostname, os, arch, home, herdr_version, claude_version, version, system}`（`system` 见 Machine，node 启动时读一次） |
| `launchers.list` | → `{launchers}` |
| `dirs.list` | `{path}` → 同 REST |
| `sessions.list` | → `{sessions}` |
| `session.start` | `{cwd, launcher, prompt?, resume_session_id?}` → `{session}` |
| `session.send` | `{session_id, text, images?}` |
| `session.keys` | `{session_id, keys, text}` |
| `session.interrupt` | `{session_id}` |
| `session.screen` | `{session_id, lines, source, format?}` → `{text, status}`（`format: "ansi"` 带颜色） |
| `terminal.watch` / `terminal.unwatch` | `{session_id, control?: {cols, rows, takeover}}`；watch → `{cols, rows, control}`，之后 node 推 `terminal.frame`；已经在推时 watch 会重开一条流（新 epoch、从整帧开始）。herdr 没有 observe 时报 `unsupported: …`。带 `control` 时 node 用 `herdr terminal session control` 接管（回包 `control: true`；旧 node 没有这个字段），槽位被占着又没 `takeover` 时报错 `code: attach_busy` |
| `fs.list` / `git.status` / `git.diff` / `git.show` / `git.log` | 同 REST（参数同名） |
| `session.items` | `{session_id, before_seq, limit, turns, after_seq, lazy_images}` → `{items, has_more, more_after, history, to_seq, from_seq}`（`from_seq` 只在 `after_seq` 请求里有；不带 `lazy_images: true` 时 node 把图片数据读回来，兼容旧 hub） |
| `session.image` | `{session_id, item_id}` → `Image{mime, data}`（node 的 item 不常驻图片数据，按记下的行偏移重读那一行） |
| `session.item` | `{session_id, item_id}` → `Item`（全文，不带图片） |
| `session.watch` / `session.unwatch` | `{session_id, since_seq?}`；watch 后 node 每 250ms 读一次新行，推 `session.items`（带 `history` / `from_seq` / `to_seq`）；有 `since_seq` 时先按 500 条一页补发其后的全部内容 |
| `session.close` | `{session_id}` |
| `file.upload.begin` | `{name, size}` → `{upload_id, chunk_bytes}`（超过上限报 `too_large`） |
| `file.upload.chunk` | `{upload_id, offset, data}`（data 是 base64，offset 必须接着已收到的）→ `{received}`；出错整个上传作废 |
| `file.upload.end` / `file.upload.abort` | `{upload_id}`；end 核对大小、改名 → `{path, name, size}` |
| `interaction.resolve` | `{id, resolution}` |

hub → node 事件（按发出顺序处理）：`terminal.input {session_id, text}`、`terminal.resize {session_id, cols, rows}`、`terminal.scroll {session_id, lines, up, column?, row?}`（滚轮：herdr 交给接鼠标的程序，比如 Claude Code 的全屏渲染、Codex，否则滚 herdr 自己的历史；
node 拆成 `lines` 个一行的事件，落在 `column` / `row` 那一格，没给就落在屏幕正中间，`docs/terminal.md` 第十六节）、`terminal.click {session_id, column, row}`（左键按下 + 松开）、`terminal.keepalive {session_ids}`（每 20 s，接管中的终端；node 75 s 没听到就放手）。

node → hub 事件：`terminal.frame {session_id, epoch, seq, full, cols, rows, data}`（相隔不到 30 ms 的帧由 node 合成一帧：diff 的字节接起来，整帧丢掉它前面的 diff）、`terminal.end {session_id, error, control?, taken?}`（流停了且起不来：会话不在跑、herdr 一直拒绝；`control` 时是接管结束了：`taken` 被别人抢走，否则租约到期）、
`session.items {session_id, items, history, from_seq, to_seq}`、`sessions.changed {sessions}`、
`sessions.revs {revs}`（每秒检查一次，只发变了的 rev；列表本身变了时 rev 跟着 `sessions.changed` 走）、
`usage {buckets:[{minute, model, in, cache_write, cache_read, out, req}], full, load, cpus}`（每 10 秒，只发变了的分钟、每分钟的全量值，
连上后第一次 `full: true` 带最近 26 小时全部；`docs/live-activity.md`「用量」）、
`metrics {sample: MetricSample}`（每 10 秒，对齐到整 10 秒；hub 存进 `machine_metrics`，`docs/machines.md`）。

node → hub 请求：
- `interaction.create {interaction}` → `{resolution}`（阻塞到被处理 / 过期；过期返回 `{"dismiss":true}`）
- `mcp.tools {}` → `{tools}`；`mcp.call {name, args, session_id}` → `{content, is_error}`

## hook / mcp ↔ node（unix socket `~/.jarvis/node.sock`）

一次连接一问一答，各一行 JSON：

- `{"kind":"hook","input":<Claude Code 传给 hook 的 stdin JSON>,"pane":"…","attempt":"<hook 进程自己的 id>","resume":<是不是重连>}`
  → `{"output":<要打印到 stdout 的 JSON 或 null>}`，或 `{"retry":true}`（node 正在退出，去问下一个 node）
- `{"kind":"mcp.tools"}` → `{"tools":[…]}`
- `{"kind":"mcp.call","name":"…","args":{},"session_id":"…"}` → `{"content":[…],"is_error":false}`

连不上 node（hub 不在线）时 hook 立即退出 0、不输出，即 TUI 正常处理；mcp 返回错误文本。

### 交互挺过重启

部署频繁（hub 重启、换 node 二进制），以前手机上正在答的提问 / 计划 / 授权会消失，退回电脑 TUI。现在：

- **hook**：已经连上 node、还没拿到回答时 node 没了（连接断开或回 `retry`），每秒重连 `node.sock`、带同一个 `attempt` 重发，
  最多 2 分钟（`node.ResumeWait`）；一开始就连不上 node 照旧立即交给 TUI。
- **node**：interaction id 是确定的：`session_id + 事件 + 工具名 + tool_use_id` 的哈希（没有 tool_use_id 用 hook 的 attempt），
  重连的 hook 得到同一个 id；带 attempt 的交互标 `resumable: true`，重连的那次不再发 `hook.event`。
- **hub**：`resumable` 的交互在 node 连接断开时不撤销，留 3 分钟（`resumeGrace`）等 hook 重新认领；同 id 的 `interaction.create`
  挂回原来的那张卡（不重复推送，App 按 id 去重），这期间手机上答了的，回来的 hook 直接从数据库拿到答案。启动时数据库里
  pending 的 `resumable` 交互重新载入（同样等 3 分钟），其余标 cancelled。旧 node 不带 `resumable`，行为不变。
- 本机实测（假 hub + 沙箱 node + 真 `jarvisd hook`）：提问时杀掉 node、2 秒后起新 node，hook 3.1 秒后拿到手机的回答，hub 只看到一个 id。

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

hub 经 hub 机器上 lark-connector 的 agent 接口（`/agent/v1`，token 从容器环境变量读）代理飞书数据，见 `server/internal/lark`。

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

