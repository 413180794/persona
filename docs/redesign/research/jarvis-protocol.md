# Persona hub 协议调研（为新写 Android 客户端而整理）

> 范围：只读调研，未改任何仓库文件、未运行构建 / 测试。证据格式 `相对路径:行号`（相对 Persona 仓库根目录）或 `docs/xxx.md「章节」`。
> 隐私：文中不出现主机名、IP、域名、账号、key / 项目 ID，示例一律占位符（`<hub 地址>`、`<token>`）。
> 性能数字：只引用文档里写明是实测的值并标样本数；其余都是代码里的常量 / 超时配置，标「常量」，不是实测。
> 状态：已完成（第 0–12 节）。Android 平台细节标「需核实」的未联网核实。

---

## 0. 结论速览

- 手机只连 hub：HTTP（REST）+ 一条 WebSocket（`/api/v1/ws`），全部 JSON、snake_case，Bearer token 鉴权。协议本身与平台无关，
  Android 客户端可以照 iOS 的 `ios/Shared/APIClient.swift` + `ios/Shared/LiveConnection.swift` + `ios/Shared/Models.swift` 一比一复刻。
- **推送是唯一硬缺口**：hub 只会发 APNs（`server/internal/hub/push.go` 全文），设备注册接口没有平台字段、只认 `apns_token`
  （`server/internal/db/db.go:122-133`、迁移 `0001_init.sql` 的 `devices` 表）。Android 不改 hub 就收不到任何推送、也不能被唤醒执行 `phone_*`。
- **手机工具注册是「全局一份」**：`register_phone` 直接覆盖 hub 的工具表（`hub.go:647-656`），工具调用发给「最近注册的那台真机」
  （`http.go:542-556`）。iPhone 和 Android 同时在线时会互相覆盖、调用落点不确定——两台手机并用就必须改。
- 其他平台相关点（Gmail 登录的 OAuth 客户端是 iOS 类型、助手提示词 / 错误文案写死「iPhone」、实时活动只有 APNs 实现）列在第 11 节。

---

## 1. 认证与配对

| 项 | 结论 | 证据 |
| --- | --- | --- |
| 二维码内容 | `jarvis://pair?url=<public_url>&token=<配对 token>`（query 用 `url.Values` 编码） | `server/internal/hub/config.go:144-147` |
| 谁生成 | hub 机器上 `jarvisd hub pair` 打印地址、token 和终端二维码（qrterminal） | `config.go:149-158`、`server/cmd/jarvisd/main.go:25-56` |
| 配对 token | 就是 `hub.json` 的 `token`：首次运行随机 24 字节 hex，**永久有效、不是一次性**，角色 full（什么都能做） | `config.go:82-92`、`server/internal/hub/devicetokens.go:69-71` |
| 客户端解析 | scheme 必须是 `jarvis`，取 query 的 `url`、`token`（非空） | `ios/Shared/APIClient.swift:7-16` |
| 每设备凭据 | 配对后立即 `POST /api/v1/auth/tokens {"name","kind"}` → `{"id":"dev_…","token":"jd_…","role","name"}`；之后只用新 token，丢弃配对 token | `devicetokens.go:126-168`、`ios/Jarvis/App/DeviceCredentials.swift:15-31` |
| kind → role | `phone` → `full`；`watch` → `answer`；其他一律 `kind:"other"`、`role:"full"`；只有 full 角色能建新 token（否则 403）；没有数据库时 503 | `devicetokens.go:137-152` |
| 默认名字 | name 为空或超 80 字时：phone 叫 "iPhone"、watch 叫 "Apple Watch"、其他叫「设备」（**写死了苹果名字**，Android 要自己传 name） | `devicetokens.go:153-159` |
| 存储 | hub 只存 token 的 sha256（`device_tokens.token_hash`），token 明文只在创建时返回一次 | `devicetokens.go:59-62,160-167`、`server/internal/db/migrations/0031_device_tokens.sql` |
| 校验缓存 | 查库结果缓存 30 秒；`last_seen` 最多 5 分钟写一次 | `devicetokens.go:57,80-107` |
| 撤销 | `DELETE /api/v1/auth/tokens/{id}`：清缓存、立即断开该设备的 WebSocket；`GET /api/v1/auth/tokens` → `{"tokens":[{id,name,kind,role,created_at,last_seen,revoked_at}],"current":"<本机 id>"}` | `devicetokens.go:169-222` |
| 撤销自己 | iOS 撤销本机 = 退出配对（`model.unpair()`） | `DeviceCredentials.swift:127-136` |
| answer 角色 | 所有 GET/HEAD + WebSocket 可用；写操作只放行正则：`interactions/*/resolve`、`sessions/*/*/(messages\|seen\|interrupt)`、`inbox/*/(done\|reply\|draft)`、`inbox/refresh`、`devices(/.*)?`；WS 上 `register_phone`、`tool_result` 被忽略 | `devicetokens.go:111-124`、`server/internal/hub/http.go:699-718` |
| 传 token 的方式 | `Authorization: Bearer <token>`，或 query `?token=`（对**所有** `/api/` 路径生效，WS、媒体都能用） | `http.go:163-176` |
| 免鉴权接口 | 只有 `GET /api/v1/health` → `{"ok":true,"version","features"}` | `http.go:21-23` |
| 旧 hub 兼容 | 没有 `/auth/tokens` 时继续用配对 token，下次启动再试 | `DeviceCredentials.swift:4-7` |
| 重新配对 | 再扫同一个二维码即可（token 不变，除非手改 hub.json）；被撤销的设备要重新扫码 | 同上 |

注意：
- **推送用的 device_id 和凭据 id 是两套**：推送 / 通知设置挂在 App 自己生成的 UUID 上（`PushRegistrar.deviceID`，`ios/Jarvis/App/JarvisApp.swift:239-245`），
  凭据是 hub 发的 `dev_…`（`DeviceCredentials.swift:10-13`）。Android 照做即可，但两者没有关联，hub 不知道某个推送设备属于哪个凭据（撤销凭据不会删推送注册）。
- 安全面：二维码里是永久主 token，任何拿到二维码的人都有全部权限；轮换要改 hub.json 并让所有旧客户端（仍用配对 token 的）重配。可选改进见第 11 节。

---

## 2. 通用约定（新客户端最容易踩的点）

- 字段 snake_case；时间是 RFC3339 UTC（`docs/protocol.md` 开头）。Go 输出**最多 9 位小数秒**（iOS 专门处理，`ios/Shared/JSONValue.swift:100-120`）。
- **零时间**：Go 的 `omitempty` 不省略 `time.Time`，没值的时间字段会是 `0001-01-01T00:00:00Z`（如 `seen_at`、`state_since`、`last_run`、`next_run`）。
  iOS 把早于 2000-01-01 的一律当空（`Date.realDate`，`ios/Shared/APIClient.swift:743-746`）。Android 解析器要同样处理。
- 错误：`{"error":"…"}` + 4xx/5xx。node 返回带 code 的错误时 hub 回 **409** 并把 `code` 和附加字段平铺在顶层（`delivered`、`draft`、`clearable`…）；
  机器离线 503；其他 node 错误 502（`http.go:253-274`）。飞书只读代理可能透传 lark-connector 的 `{"error":{"code","message"}}`（`APIClient.swift:49-61`）。
  **未知路径 / 方法不匹配**时主 API 的 ServeMux 回的是 Go 默认纯文本（只有邮件子路由把 404/405 改写成 JSON，`server/internal/mail/http.go:28-55`），客户端要容忍非 JSON 错误体。
- 请求 ID：每个响应带 `X-Request-Id`；请求可带 `X-Client-Request-Id`（4–32 位十六进制或 `-`），hub 沿用它记日志（`http.go:196-218`）。iOS 在 5xx 报错后面附「（请求 id）」。
- 缓存：所有 API 响应 `Cache-Control: no-store`（`http.go:179-181`）；例外：会话图片 `private, max-age=31536000, immutable`（`http.go:355`），飞书表情 1 小时（`server/internal/hub/lark.go:163`）。
  iOS 的教训：自己做磁盘缓存的字节（对话图片、飞书媒体）用不带 HTTP 缓存的 client 取，避免存两份（`APIClient.swift:69-76`）。
- 功能协商 `features`（`server/internal/proto/proto.go:42-54`）：`client_msg_id`、`send_refusals`、`screen_dialog`、`resumable_interactions`、`secret_prompts`、
  `request_id`、`terminal_stream`、`terminal_control`、`terminal_scroll`、`terminal_click`、`file_upload`。在 `/health` 和 WS `hello` 里下发；客户端按字段在不在决定发不发新请求。
- 体积上限（常量）：POST 转发到 node 的 body 32 MB（`http.go:364`，含消息里的 base64 图片）；WS 单条读上限 64 MB（`http.go:563`，iOS 侧也设 64 MB，`LiveConnection.swift:220`）；
  文件上传 200 MB（`proto.go:57-63`）。
- hub 侧超时（常量，非实测）：转发 GET 30 s、转发 POST 90 s（离线会话要先 `--resume` 拉起）、新建会话 100 s、新建助手对话 110 s（`http.go:283,371,417`、`assistant.go:296`）。
  iOS 客户端对应的请求超时：默认 30 s、发消息 100 s、新建会话 / 对话 110 s、立即运行定时任务 130 s、上传 120 s（`APIClient.swift`）。
- 传输：hub 默认明文 HTTP `:18660`（`config.go:86-88`），iOS 靠 `NSAllowsArbitraryLoads`（`ios/project.yml:84-86`）。**Android 9+ 默认禁止明文**，
  hub 地址是用户配的，需要 network security config 放开 cleartext（或要求 hub 前面挂 HTTPS 反代）。WebSocket 用 `ws://` / `wss://` 随 http/https。

---

## 3. HTTP 接口目录（以路由注册为准）

路由注册：`server/internal/hub/http.go:19-161`（主表）+ 各文件的 `xxxRoutes`（`liveactivity.go:682-720`、`usage.go:128-132`、`metrics.go:224-260`、
`assistant.go:192-312`、`assistant_schedule.go:691-805`、`pins.go:59-120`、`ai.go:38-81`、`kb.go:598-689`、`lark.go:15-217`、`inbox.go:662-734`、
`devicetokens.go:126-198`）+ 邮件子路由 `server/internal/mail/http.go:15-30`。
标 **[未文档]** 的是 `docs/protocol.md` 的 REST 表里没有的（有的在别的文档里提过，已注明）。
`{mid}` = 机器 id，`{sid}` = 会话 id（子 Agent 是 `<父会话>@<agent id>`，`proto.go:429-441`）。转发到 node 的接口（`proxy` / `proxyBody`）把 node 的结果原样返回。

### 3.1 系统 / 鉴权
| 方法 路径 | 用途 / 关键参数 |
| --- | --- |
| GET `/api/v1/health` | 免鉴权：`{ok, version, features}` |
| POST `/api/v1/auth/tokens` | `{name, kind: phone\|watch}` → `{id, token, role, name}`（文档在「鉴权」节，不在 REST 表） |
| GET `/api/v1/auth/tokens` | `{tokens, current}` |
| DELETE `/api/v1/auth/tokens/{id}` | 撤销 + 断开它的 WS |
| GET `/api/v1/ws` | WebSocket（第 4 节） |

### 3.2 机器
| 方法 路径 | 用途 / 关键参数 |
| --- | --- |
| GET `/api/v1/machines` | `{machines:[Machine]}`（含 `system`、`node_version`、`node_mismatch`） |
| POST `/api/v1/machines/{mid}/reconnect` | 重连 node |
| GET `/api/v1/machines/{mid}/launchers` | `{launchers:[{name, base_url, model}]}` |
| GET `/api/v1/machines/{mid}/dirs?path=` | 目录浏览 + 最近目录 |
| POST `/api/v1/machines/{mid}/spaces/{wid}/rename` | `{name}`：改 herdr workspace 名，并迁移 pins / mutes 里的「机器/旧名」（`pins.go:122-169`） |
| GET / PUT `/api/v1/machines/{mid}/claude-settings` | 白名单 Claude Code 设置；PUT `{key, value}`（null 恢复默认），返回 `{settings:[{key,title,detail,type,options,default,value}]}` |
| GET `/api/v1/metrics` | Dashboard：每台最近 1 小时、每分钟一点，`latest` 只在 30 秒内有样本时有 |
| GET `/api/v1/machines/{id}/metrics?range=1h\|6h\|24h` | 详情曲线；404 无此机器、400 range 不对（`metrics.go:238-259`） |
| GET `/api/v1/usage` | `{usage:{tpm, tokens, output, requests, machines:[{id,tpm,tokens,load}]}}`（`usage.go:69-75`） |

### 3.3 会话（Agents）
| 方法 路径 | 用途 / 关键参数 |
| --- | --- |
| GET `/api/v1/sessions?machine_id=` | `{sessions:[Session]}`：运行中的 + 最近 7 天 offline |
| POST `/api/v1/sessions` | `{machine_id, cwd, launcher, prompt?, resume_session_id?, workspace_id?, permission_mode?, effort?}` → `{session}`（`workspace_id` **[未文档]**，`http.go:397-429`） |
| GET `/api/v1/sessions/{mid}/{sid}/items` | 分页：`before_seq`、`turns`（node 侧上限 10 轮 / 2000 条）、`limit`（无 turns 时按条数，默认 80、最大 500）、`after_seq`（增量，500 条一页，`more_after`）、`lazy_images=1`；返回 `{items, has_more, more_after?, history?, from_seq?, to_seq?}`（`http.go:295-323`、`server/internal/node/node.go:367-397`） |
| GET `/api/v1/sessions/{mid}/{sid}/items/{iid}/image` | 图片原始字节，可永久缓存；`iid` 含 `#`、`:` 必须百分号编码（iOS 只放行字母数字和 `-._~`，`APIClient.swift:253-261`） |
| GET `/api/v1/sessions/{mid}/{sid}/items/{iid}/full` | 被截断 item 的全文（≤1 MB，不带图片） |
| POST `/api/v1/sessions/{mid}/{sid}/messages` | `{text, images?:[{mime,data}], client_msg_id?, clear_draft?, launcher?}`；`launcher` **[未文档]**：助手（无头）会话从这条起换模型（`APIClient.swift:283-299`、`node.go:272-281`）；拒绝见第 5 节 |
| POST `/api/v1/sessions/{mid}/{sid}/interrupt` | 发 Esc（助手会话是停止无头进程这一轮，`node.go:304-308`） |
| POST `/api/v1/sessions/{mid}/{sid}/keys` | `{keys, text, pre_keys?}` |
| GET `/api/v1/sessions/{mid}/{sid}/screen?lines=&source=visible\|recent&format=ansi` | 读屏 `{text, status}` |
| POST `/api/v1/sessions/{mid}/{sid}/screen/before` | `{anchor:[…], lines, loaded?, format?}` → `{text, has_more, at_limit}`（`loaded` **[未文档]**，`node.go:309-320`） |
| POST `/api/v1/sessions/{mid}/{sid}/close` | 退出 Agent 并关 tab（助手会话是停掉进程） |
| POST `/api/v1/sessions/{mid}/{sid}/rename` | `{tab_name?, title?}` |
| POST `/api/v1/sessions/{mid}/{sid}/modes` | `{permission_mode?, effort?}` |
| POST `/api/v1/sessions/{mid}/{sid}/queue` | **[未文档]** `{keep:[下标]}` → `{queue:[原排队消息]}`：把排队消息全部拉回再重排（编辑 / 删除排队消息，`node.go:477-483`、`server/internal/node/control.go:544-583`） |
| GET `/api/v1/sessions/{mid}/{sid}/info` | **[未文档]** 会话详情：`session_id, transcript_path, cwd, model, launcher, git_branch, version, permission_mode, effort, context_tokens, output_tokens, compactions, turns, started_at, last_activity`（`node.go:454-476`、`APIClient.swift:351-374`） |
| POST `/api/v1/sessions/{mid}/{sid}/seen` | **[未文档]** 标「看过」（完成未看 → 已看），等价 WS `seen`（`http.go:76-79`、`server/internal/hub/state.go:80-97`） |
| GET / PUT `/api/v1/pins` | 置顶 `{spaces:["机器/空间名"], sessions:["机器/会话"]}` |
| GET / PUT `/api/v1/mutes` | 屏蔽（同结构 + `allowed`），影响推送 |

### 3.4 文件 / Git（都转发到会话所在机器的 node）
| 方法 路径 | 用途 / 关键参数 |
| --- | --- |
| GET `/api/v1/machines/{mid}/file?path=&cwd=` | **[未文档于 protocol.md，`docs/ios.md` 提过]** 读文件：`{path,name,size,mime,text?,data?(base64),truncated,is_dir,entries?}`，≤2 MB，拒绝 iCloud 路径（`server/internal/node/files.go:16-60`） |
| GET `/api/v1/machines/{mid}/fs?path=&cwd=` | 文件树，最多 2000 项 |
| POST `/api/v1/machines/{mid}/files?name=` | 上传：body 是文件本身，**必须有 Content-Length**（否则 411），≤200 MB（413 `too_large`），旧 node 409 `node_outdated` → `{path,name,size}`（`server/internal/hub/upload.go:17-94`） |
| GET `/api/v1/machines/{mid}/git/status?cwd=` | 只读 git 状态 |
| GET `/api/v1/machines/{mid}/git/diff?cwd=&path=&staged=0\|1` | diff，≤512 KB |
| GET `/api/v1/machines/{mid}/git/show?cwd=&hash=` | 提交详情 |
| GET `/api/v1/machines/{mid}/git/log?cwd=&skip=&limit=` | 分页提交 |

### 3.5 交互（需要你）
| 方法 路径 | 用途 |
| --- | --- |
| GET `/api/v1/interactions` | 待处理，按创建时间升序 |
| POST `/api/v1/interactions/{id}/resolve` | body = Resolution；不再待处理 410；屏幕卡对话框变了 409 `prompt_changed`（`http.go:88-103`） |
| GET `/api/v1/interactions/history?machine_id=&session_id=&limit=` | 历史（含 status、resolution） |

### 3.6 推送设备 / 通知设置 / 实时活动
| 方法 路径 | 用途 |
| --- | --- |
| POST `/api/v1/devices` | `{device_id, name, kind:"iphone"\|"watch", apns_token?, apns_env?, topic?}`（**无平台字段**，未知字段被忽略，`http.go:104-112`、`db.go:122-133`） |
| GET / PUT `/api/v1/devices/{id}/prefs` | `{input, done:off\|long\|always, live_activity, lock_details, island:{preset,left,right,machine}}`，返回另带只读 `long_turn_seconds`、`done_delay_seconds`；设备没注册推送也能设（`http.go:116-134`、`server/internal/hub/notify.go:52-137`） |
| POST `/api/v1/devices/{id}/live-activity` | `{push_to_start_token}` / `{activity_id, push_token}` / `{activity_id, ended:true}`（APNs 专用，`liveactivity.go:682-712`） |
| GET `/api/v1/fleet` | 当前 Fleet 汇总（带全部 stats），App 回前台时拉 |

### 3.7 助手（「Persona」tab）
| 方法 路径 | 用途 / 关键参数 |
| --- | --- |
| GET `/api/v1/assistant` | `{machine_id, launcher, timezone, notes?, main?, cwd, conversations:[Session], notes_effective}` |
| PUT `/api/v1/assistant` | `{machine_id?, launcher?, timezone?, notes?, main?}`；`main:"-"` 清除主对话 **[未文档于 protocol.md，`docs/knowledge.md` 提过]**（`assistant.go:196-242`） |
| POST `/api/v1/assistant/conversations` | `{prompt, images?, launcher?, permission_mode?, effort?, main?}` → `{session}`（后四个字段 protocol.md 表里没写全，`assistant.go:283-311`） |
| POST `/api/v1/assistant/warm` | `{launcher?}` 预热 |
| GET / PUT `/api/v1/assistant/routines` | 定时任务整表替换（只认用户可改字段） |
| POST `/api/v1/assistant/routines/{id}/run` | → `{session_id, machine_id}` |
| GET `/api/v1/assistant/followups` | 跟进列表 |
| DELETE `/api/v1/assistant/followups/{id}` | 取消 |
| POST `/api/v1/assistant/followups/{id}/run` | 立即运行 |
| GET `/api/v1/assistant/runs[?ref=]` | 运行记录，新的在前 |
| GET `/api/v1/assistant/memory` | `{files:[{name,content,updated_at}], dir}` |
| PUT / DELETE `/api/v1/assistant/memory/{name}` | `{content}` |
| POST `/api/v1/ai/mail-account` | `{text}` → 识别邮箱配置 |

### 3.8 知识库（**[未文档于 protocol.md]**，`docs/knowledge.md` 有表）
| 方法 路径 | 用途 |
| --- | --- |
| GET `/api/v1/kb/{kind}?tag=` | kind ∈ projects / chats / people / docs / meetings / events → `{items:[Entry], tags}` |
| GET `/api/v1/kb/{kind}/{id}` | `{detail:{kind,id,path,meta,summary,log,recent,links}, agents?}`（项目带在其代码目录里跑的 Agent） |
| PUT `/api/v1/kb/{kind}/{id}` | 字段同 `kb_update`（`server/internal/kb/update.go:102-120`）→ `{id}` |
| GET `/api/v1/kb-search?q=&messages=0` | `{hits:[{path,line}]}` |
知识库关着时 503（`kb.go:600-605`）。

### 3.9 收件箱
| 方法 路径 | 用途 |
| --- | --- |
| GET `/api/v1/inbox` | `{items:[InboxItem]}`（未处理 + 3 天内处理过的） |
| GET `/api/v1/inbox/{id}` | 带 `context` 的一件 |
| POST `/api/v1/inbox/{id}/done` | `{done}` |
| POST `/api/v1/inbox/{id}/draft` | `{instruction?}` → `{text}` |
| POST `/api/v1/inbox/{id}/reply` | `{text}` 发送并标已处理 |
| POST `/api/v1/inbox/refresh` | 立即采集，返回列表 |
InboxItem 还有 protocol.md 没列的 `agent`（AI 分身的处理结果，`server/internal/hub/inbox.go:54-84`）。

### 3.10 飞书代理（lark-connector）
protocol.md 已列：conversations / pinned / labels / unread-count / flags / conversations/{id}/labels / settings / unread / messages（GET、POST）/ read / thread / media / messages/{id}/media / emojis / card-actions。
代码里另有 **[未文档于 protocol.md]**（部分在 `docs/feishu.md`）：

| 方法 路径 | 用途 |
| --- | --- |
| GET `/api/v1/lark/raw/{path...}` | lark-connector agent API 只读窗口（`docs/feishu.md` 提过）；JSON 里媒体路径已改写回 hub |
| POST `/api/v1/lark/raw/{path...}` | 同上的 POST（body ≤1 MB；助手工具说明里只用于 `search/page`） |
| GET `/api/v1/lark/reactions/quick` | `{keys}` 常用表情回应 |
| POST `/api/v1/lark/messages/{id}/reactions` | `{key, remove?}` |
| GET `/api/v1/lark/users/{id}?feed_id=` | 个人资料（lark-connector 原样 JSON） |
| POST `/api/v1/lark/users/{id}/chat` | → `{feed_id}` 打开单聊 |
| GET `/api/v1/lark/messages/{id}/url-preview?url=` | 链接预览 |
| GET `/api/v1/lark/moments/posts/{id}`、`…/comments` | 帖子 / 评论 |
`GET /api/v1/lark/emojis` 实际还返回 `groups`（`lark.go:157-165`）。媒体 URL 是 hub 相对路径（`/api/v1/lark/media/…`），客户端要拼 hub 地址并带鉴权（头或 `?token=`）；
视频 / 音频依赖 Range 透传（`CLAUDE.md` 经验）。

### 3.11 邮件（`server/internal/mail/http.go:15-27`，与 protocol.md 一致）
accounts（GET / POST / DELETE / test / mailboxes）、`oauth/google/start`、`oauth/google/finish`、messages（列表 / 单封 / flags）、send。Gmail 登录的平台问题见第 9 节。

---

## 4. WebSocket `/api/v1/ws`

### 4.1 连接
- URL：`ws(s)://<hub 地址>/api/v1/ws`，鉴权同 HTTP（头或 `?token=`）。服务端 `websocket.Accept(... InsecureSkipVerify: true)`，不校验 Origin（`http.go:558-563`）。
- 连上后服务端立刻按顺序发：`hello{server_version, client_id, features}` → `machines` → 每台机器一条 `sessions` → 每个待处理交互一条 `interaction`（`http.go:616-623`）。
  所以「重连 = 全量状态重放」，客户端不需要另拉列表（iOS 仍会在连上后再 `GET /interactions` 校正，`ios/Jarvis/App/AppModel.swift:456-474`）。
- 心跳：hub 每 20 s 发 WebSocket 协议层 ping（15 s 超时，失败即关连接，`http.go:589-613`）。**没有 JSON 层的 ping/pong**：protocol.md 表里的 `ping` / `pong`
  在代码里不存在（服务端不发 `{"type":"ping"}`，收到 `{"type":"pong"}` 也忽略，`http.go:663-718`）。iOS 自己也每 20 s 发 ping、弱网时 5 s 一次，
  45 s 没收到任何消息判死重连（`ios/Shared/LiveConnection.swift:167-170,230-265`）。OkHttp 用 `pingInterval(20s)` 即可（coder/websocket 自动回 pong）。
- 背压（常量）：每个连接一个 512 条的发送队列。`broadcast` / `toSubscribers` 队列满时**静默丢这一条**；`sendJSON`、终端帧 `sendTerm` 队列满时**直接断开**
  （"too slow"），客户端重连再补（`http.go:448-459,461-501`、`terminal.go:479-491`）。→ 客户端必须及时读，解码放后台线程（iOS 在接收回调线程解码，`LiveConnection.swift:267-277`）。
- 断开清理：服务端在连接结束时退订该连接的所有会话和终端（`http.go:571-587`）。App 进后台时 iOS 主动断开 WS，避免挂起的 socket 让 hub 以为手机在线（`JarvisApp.swift:27-35`）。

### 4.2 服务端 → 客户端（代码里实际发的全部类型）
| type | 字段 | 发给谁 | 证据 |
| --- | --- | --- | --- |
| `hello` | `server_version, client_id, features` | 本连接 | `http.go:616` |
| `machines` | `machines` | 全部 | `hub.go:213-215` |
| `sessions` | `machine_id, sessions`（该机全量；每个带 `state`、`rev`、`suggestion`…） | 全部 | `hub.go:247-251`、`state.go:94-96` |
| `revs` | `machine_id, revs:{sid: rev}`（只含变了的） | 全部 | `machine.go:335-350` |
| `items` | `machine_id, session_id, items` + `history? from_seq? to_seq?`；补发时另有 `backlog:true`、`has_more?`、`more_after?` | 订阅者 | `http.go:503-540,725-771` |
| `delta` | `machine_id, session_id, text`（无头助手正在生成的整段，空串=本轮结束） | 订阅者 | `machine.go:402-409`、`server/internal/node/assistant.go:429` |
| `screen` | `machine_id, session_id, text, status, ts` | 订阅者 | `machine.go:374-384`（**遗留**：现在的 node 不再发 `session.screen` 事件） |
| `terminal` | 帧 `epoch, seq, full, cols, rows, data(base64)`，或 `error` + `unsupported` | 终端订阅者 | `terminal.go:70-73,471-476` |
| `terminal_control` | `state: active\|busy\|taken\|lost\|unsupported`，`cols/rows`（active 时），`error`（给人看的原因） | 驾驶者 | `terminal.go:145-154` |
| `interaction` | `interaction` | 全部 | `hub.go:376-384` |
| `interaction_resolved` | `id` | 全部 | `hub.go:572-580` |
| `tool_call` | `call_id, name, args` | 被选中的那台手机 | `hub.go:711` |
| `fleet` | `fleet`（FleetState + 全部 stats） | 全部 | `liveactivity.go:457-463` |
| `inbox` | 无内容，App 自己拉 | 全部 | `inbox.go:241` |
| `lark` | `feed_ids`（400 ms 节流） | 全部 | `lark_events.go:12-44` |
| `assistant_schedule` | 无内容：定时任务 / 跟进变了 **[未文档]** | 全部 | `assistant_schedule.go:683-689` |
| `hook_event` | `machine_id, session_id, event, tool_name, message, last_assistant_message, cwd` **[未文档]**（iOS 解析后忽略） | 全部 | `machine.go:434-441`、`server/internal/node/hooks.go:201-206` |
| `error` | `machine_id, session_id, error`：订阅补发失败 **[未文档]** | 本连接 | `http.go:743-745` |

protocol.md 里的 `session`（单个状态变化）在代码里**没有发送点**，可以不实现。

### 4.3 客户端 → 服务端
| type | 字段 | 说明 / 证据 |
| --- | --- | --- |
| `subscribe` | `machine_id, session_id, since_seq?, lazy_images?` | 首个订阅者让 node `session.watch`（250 ms 读新行）；`lazy_images` 一旦为 true，**这个连接之后所有 items 都不带图片数据**（连接级粘性，`http.go:673-689`） |
| `unsubscribe` | `machine_id, session_id` | 最后一个退订时 node `session.unwatch` |
| `seen` | `machine_id, session_id` | **[未文档]** 标看过（`http.go:709-712`） |
| `terminal_subscribe` | `machine_id, session_id, epoch?, after_seq?, control?:{cols,rows}, takeover?` | 注意 `takeover` 是**顶层字段**，不在 `control` 里（`http.go:649-651`、`AppModel.swift:330-338`） |
| `terminal_input` | `machine_id, session_id, text?, control?, scroll?, at?:[col,row], click?:[col,row]` | 只有驾驶者发的才转给 node；键按到达顺序以事件转发（`terminal.go:239-298`） |
| `terminal_unsubscribe` | `machine_id, session_id` | |
| `register_phone` | `device_name, tools:[{name,description,input_schema}], simulator` | 只认 full 角色；`tools` 为空数组时不覆盖已有工具表（`http.go:699-708`） |
| `tool_result` | `call_id, content:[{type:text,text}\|{type:image,data,mime_type}], is_error` | 只认 full 角色 |

### 4.4 订阅补发（backlog）细节（`http.go:721-771`）
- 不带 `since_seq`：补最后 2 轮（`turns: 2`），一条 `items`，带 `backlog:true` 和 `has_more`。
- 带 `since_seq`：按 `after_seq` 一页页补（node 每页 500 条），最多 8 页（4000 条）；超出时最后一条带 `more_after:true`，客户端改取最新一页；
  **补发里不带 `has_more`**（只有从末尾往前数的页才知道前面还有没有，2026-10-26 的坑，见 `CLAUDE.md` 经验和 `http.go:751-755`）。
- 补发失败发 `{"type":"error", machine_id, session_id, error}`。

---

## 5. 同步模型（推拉结合）

- **列表层**：`sessions` 全量推（任何字段变化），`revs` 轻量推（只有 transcript 长了、列表可见字段没变时；node 每秒检查一次，protocol.md「hub ↔ node RPC」）。
  `rev` = transcript 字节数（`proto.go:227-229`）。整个会话列表约 130 KB，所以 rev 变化不重发列表（`CLAUDE.md` 经验，数字为文档记录，非本次实测）。
- **内容层**：没打开的会话由客户端按 `after_seq=<本地 covered>` 拉增量（iOS `AgentSync`：活着的 Agent + 24 小时内用过的，最多 40 个，并发 3，均为客户端策略常量，
  `ios/Jarvis/App/AgentSync.swift:13-15`）；打开的会话靠 WS 订阅实时推 + 订阅时 `since_seq` 补洞。**后台同步不能用「订阅所有会话」**：订阅会让 node 250 ms 刷 transcript（`CLAUDE.md` 经验）。
- **连续性**（`docs/protocol.md「items 的连续性」`）：每批带 `history`（文件身份，变了=丢弃本地重取）、`to_seq`（这批读到的最后一行，含无 item 的行）、
  `from_seq`（接续批次才有）。客户端维护 `covered`：`from_seq <= covered` → `covered = to_seq`；否则有洞，等 1.5 s 再 `after_seq=covered` 补，超过 8 页改取最新一页
  （`AppModel.swift:402-439`）。空 `items` 但带 `to_seq` 也要处理（推进 covered）。
- **分页**：历史按轮（`turns=2`，页首一定是用户输入）；iOS 同时带 `limit=80` 兼容旧 hub（`APIClient.swift:218-225`）。`has_more` 只信「从末尾往前数」的页。
- **图片懒加载**：`lazy_images` 后 item 里 `image:{mime, lazy:true, size, width, height}`，显示时取 `/items/{iid}/image`（永久缓存）。本地发出的图片仍是内嵌 base64。
- **截断**：页面 / 推送里工具输出最多 4 KB、其他文字 24 KB；被截断的 item 带 `full_size`，文本末尾一行「…（已截断…）」，客户端去掉这行并提供「看全文」（`ios/Shared/Models.swift:395-399`）。
- **发消息幂等**（`docs/protocol.md「发消息」`、`server/internal/node/sendsafe.go:53-74`、`node.go:272-281`）：每条待发消息一个 UUID 作 `client_msg_id`，重发沿用；node 记 10 分钟。
  结果三种：成功；409 拒绝（`code: dialog_open | input_draft`，`delivered:"none"`，可再发；`input_draft` 带 `draft`、单行时带 `clearable`，用户同意后带 `clear_draft=<clearable>` 重发同一 id）；
  502 `delivered:"unknown"`（打字途中出错，同 id 再发只回同样的错，不会打第二遍）。
- **离线发件箱**：连不上时消息进本地 outbox（存盘），重连且补拉完成后按顺序发；批准 / 回答类一次性操作不排队（`docs/ios.md「连接状态」`）。
- **看过**：打开会话或会话在眼前结束时发 `seen`（WS）或 `POST …/seen`，hub 记 `session_views`，影响 `state`（unread → idle）和「做完了」推送（`state.go:10-35,80-97`）。

---

## 6. 交互（需要你）

- 类型 `question | plan | permission | screen`；结构和 Resolution 见 `docs/protocol.md「数据类型」`、`proto.go:318-381`。
- `screen` 卡：hub 读屏解析出 `dialog{header, question, detail, options[{key,label,detail,kind,keys}], rev}`（`server/internal/screen/dialog.go:16-38`），
  回答用 `option`（`type_something` 再带 `text`）；node 按键前重读屏幕比对 `rev`，变了回 409 `prompt_changed` 并重建新卡。`secret:true` 时用密码框。
  卡片 id 含 `rev`：对话框变了就是新卡。屏幕卡回答是同步的（hub 等 node 按完才回）。
- 过期（常量）：hook 交互默认 30 分钟（`hook_timeout_minutes`，`config.go:131-133`）；屏幕卡 24 小时（`hub.go:311`）；hub 代问的 `mail_send` / `lark_send` 授权 15 分钟（`hub.go:683-691`）。
- `resolve` 返回：成功 `{ok:true}`；已不在待处理 410；屏幕卡 409 `prompt_changed`；其他转发错误 502/503。
- 挺过重启：`resumable` 交互在 node 断开后保留 3 分钟（`hub.go:125-127`），同 id 重新挂回；App 按 id 去重即可。
- 助手的 `ask_user` 是 `question` 交互，`tool_name:"ask_user"`，推送标题「Persona 在问你」（`push.go:316-318`）。
- 通知按钮语义（iOS，`ios/Jarvis/App/NotificationActions.swift`）：授权「允许 `{allow:true}` / 拒绝 `{allow:false}`」，计划「批准 `{approve:true, mode:"acceptEdits"}` /
  打回 `{approve:false, feedback:<输入>}`」；「允许」「批准」「打回修改」要先解锁设备（`.authenticationRequired`），只有授权的「拒绝」不用；提问、终端卡没有按钮（点开处理）。失败（410 已处理、连不上）发本地通知说明。
- 文档说「没有任何 App 在线时 hook 立即放行」（`docs/architecture.md「回答提问 / 审批计划」`），但代码里 node 的 `ask` 直接调 `interaction.create` 等待到过期，
  没看到「没有 App 在线就放行」的判断（`server/internal/node/hooks.go:312-336`、`hub.go:396-429`）——以代码为准，文档可能过时。

---

## 7. 手机工具（phone_*）协议

### 7.1 链路与协议（`docs/phone-tools.md`、`server/internal/hub/hub.go:631-765`）
1. 注册：WS 连上后客户端发 `register_phone{device_name, tools, simulator}`（iOS 在 `didConnect` 里发，`AppModel.swift:456-492`）。`tools` 是 MCP 工具定义
   `{name, description, input_schema}`，**只含用户在设置里打开的数据源**。hub 把它存进内存并整表替换 Postgres `phone_tools`（手机离线时 Claude 也能看到工具，`hub.go:647-656`、迁移 `0001_init.sql`）。
2. 暴露给 Claude：`jarvisd mcp` → node.sock → node → hub `mcp.tools` → `mcpTools()` = 手机工具 + 邮件工具（+ 助手会话额外的助手工具）（`hub.go:658-669`、`machine.go:465-470`）。
3. 调用：`mcp.call` → `callTool`（`phone_` 前缀走 `callPhone`）→ `phoneClient()` 选手机：**真机优先于模拟器，其次最近注册的**（`http.go:542-556`）→
   没有在线手机就 `wakePhone`（静默推送，同一时间 30 s 内只唤醒一次，等最多 20 s、每 300 ms 看一次有没有手机连上，`hub.go:722-753`）→
   发 `tool_call{call_id, name, args}` → 最多等 2 分钟（`hub.go:693-720`，常量）。
4. 结果：`tool_result{call_id, content:[{type:"text",text} | {type:"image",data,mime_type}], is_error}`（`proto.go:383-401`）。失败一律变成 `is_error` + 说明文字给模型，
   客户端「永不抛错」（`ios/Jarvis/PhoneTools/PhoneToolRegistry.swift:95-120`）。
5. 写操作确认：**在手机上做，不经过 hub**。工具带 `confirmation` 摘要，执行前 App 弹确认卡片；用户拒绝 → `is_error`、内容 `The user declined on the phone.`；
   确认只管这一次调用（`PhoneToolRegistry.swift:107-112`、`AppModel.swift:570-583`）。被关掉的数据源即使被旧会话调用也拒绝（`PhoneToolRegistry.swift:103-106`）。
   （hub 自己代问的确认只有 `mail_send`、`lark_send`：建 `permission` 交互，`hub.go:683-691`。）
6. 输出约定：pretty JSON 文本；时间 ISO 8601 带本机时区偏移，日期 `yyyy-MM-dd`；`phone_photo_get` 额外一个 image 块（`docs/phone-tools.md「工具一览」`）。

实测（单次样本，`docs/assistant.md「能力实测」`，样本数 1，不能当基线）：锁屏时调手机工具，静默推送 APNs 接受 259 ms、手机 900 ms 后连上、工具 3.2 秒出结果；另一次 2.8 秒拿到结果。

### 7.2 iOS 现有工具清单（`ios/Jarvis/PhoneTools/*.swift` 的 spec）与 Android 等价性
（Android 一列是平台知识，**本次没有联网核实**，标「需核实」的要在动手前查官方文档 / 真机验证。）

| 工具（数据源） | 参数（schema） | 需确认 | Android 等价 | 差异 / 风险 |
| --- | --- | --- | --- | --- |
| `phone_contacts_search`（通讯录） | `query`（必填，空串=按通讯录顺序取前几个），`limit` 1–50 默认 10 | 否 | ContactsContract（`READ_CONTACTS`），生日在 Event 行 | 基本等价 |
| `phone_calendar_events`（日历） | `start?` `end?`（ISO 8601，默认今天 0 点起 7 天），`query?`，`limit` 1–200 默认 50 | 否 | CalendarContract.Instances（`READ_CALENDAR`），参与人在 Attendees | 等价 |
| `phone_calendar_create_event`（日历） | `title` `start` `end` 必填，`location?` `notes?` `all_day?` `calendar?`（按日历名） | **是** | CalendarContract.Events 插入（`WRITE_CALENDAR`）；默认日历取主账号可写日历 | 全天事件在 Android 上要求 UTC 零点 + `EVENT_TIMEZONE=UTC`（需核实）；「默认日历」概念要自己选 |
| `phone_reminders_list`（提醒事项） | `list?` `include_completed?` `limit` 1–200 默认 50 | 否 | **没有系统统一的提醒事项 API** | 只能：不注册这组工具；或接 Google Tasks（OAuth tasks scope，可放 hub 侧，类似 Gmail）；或 App 自建提醒（本地库 + WorkManager/闹钟通知）。厂商待办 App 无公开接口 |
| `phone_reminders_create`（提醒事项） | `title` 必填，`due?` `notes?` `list?` `priority` 0–9 | **是** | 同上 | 同上 |
| `phone_reminders_complete`（提醒事项） | `id` 必填 | **是** | 同上 | 同上 |
| `phone_location_current`（位置） | 无参数；最多等 15 s；带反向地理编码 | 否 | FusedLocationProviderClient.getCurrentLocation（需 GMS）或 LocationManager.getCurrentLocation（API 30+）；Geocoder 反查地址 | 后台（被推送唤醒时）需要 `ACCESS_BACKGROUND_LOCATION` 才能定位，和 iOS「只有使用期间权限、后台定位失败」同类限制；无 GMS / 部分国行 ROM 上 Geocoder 可能没有后端（需核实），只能回坐标 |
| `phone_health_summary`（健康） | `metrics?` ∈ steps / distance / active_energy / heart_rate / resting_heart_rate / sleep / weight（默认 steps），`days` 1–90 默认 7；按天汇总 | 否 | Health Connect：StepsRecord.COUNT_TOTAL、DistanceRecord.DISTANCE_TOTAL、ActiveCaloriesBurnedRecord、HeartRateRecord BPM_AVG/MIN/MAX、RestingHeartRateRecord、SleepSessionRecord、WeightRecord，按天 `aggregateGroupByPeriod` | ① distance：HealthKit 取「步行+跑步距离」，HC 的 DistanceRecord 不区分活动类型，语义不同；② sleep：iOS 只算 asleep 阶段，HC 的睡眠时长是否扣掉清醒阶段需核实；③ HC 能查询权限是否授予（比 iOS 好，iOS 读权限永远不告诉 App）；④ HC 默认只能读首次授权前 30 天内的数据，更早要 `READ_HEALTH_DATA_HISTORY`（需核实）；⑤ 被推送唤醒、App 不在前台时读 HC 要 `READ_HEALTH_DATA_IN_BACKGROUND`（需核实）；⑥ 数据得有来源 App 写进 HC，国产手机自带健康 App 是否同步到 HC 因厂商而异（需核实） |
| `phone_photos_recent`（照片） | `limit` 1–100 默认 20，`days?`，`screenshots_only?` | 否 | MediaStore（13+ `READ_MEDIA_IMAGES/VIDEO`；14+ 部分授权 `READ_MEDIA_VISUAL_USER_SELECTED` ≈ iOS limited） | 「是否截图」没有字段，只能按相册路径 / bucket 名猜（厂商不一）；收藏 `IS_FAVORITE`（API 30+）；经纬度要 `ACCESS_MEDIA_LOCATION` + EXIF |
| `phone_photo_get`（照片） | `id` 必填，`max_size` 256–2048 默认 1280；JPEG 质量 0.7 | 否 | ContentResolver.loadThumbnail / 解码缩放 | 等价；id 用 MediaStore `_ID` 或 content URI |
| `phone_device_info`（设备） | 无参数 | 否 | Build、BatteryManager、PowerManager（省电 / 温控）、StatFs、ConnectivityManager | 等价；iOS 输出字段 `ios_version`，Android 自己定义字段名 |
| `phone_clipboard_get`（剪贴板） | 无参数 | 否 | ClipboardManager | **Android 10+ 只有前台有焦点（或默认输入法）才能读剪贴板**，被推送唤醒在后台时读不到；12+ 会弹「已粘贴」提示。iOS 是弹「允许粘贴吗」 |
| `phone_clipboard_set`（剪贴板） | `text` 必填 | 否 | setPrimaryClip | 13+ 有系统复制提示；后台写是否受限需核实 |
| `phone_notify`（通知） | `title` `body` 必填，`delay_seconds` 0–86400 | 否 | NotificationManager（13+ `POST_NOTIFICATIONS`）；延迟用 WorkManager（不精确）或精确闹钟（12+/14+ 精确闹钟权限受限） | 基本等价 |

工具的 `description` / `input_schema` 由客户端提供（hub 不改），Android 客户端应写自己的描述（不要照抄「iOS may show an Allow Paste prompt」这类话），
但**工具名和参数最好与 iOS 保持一致**，这样助手提示词、已有会话的习惯都不用变。

Android 能多做、iOS 做不到的（可选，取决于分发渠道：上架 Google Play 时短信 / 通话记录权限只给默认短信 / 拨号 App；侧载或内部分发不受这条政策限制）：
读短信（`READ_SMS`）、通话记录（`READ_CALL_LOG`）、读其他 App 的通知（NotificationListenerService，能看到 IM 消息，敏感）、设闹钟（AlarmClock intent）、
应用使用时长（UsageStatsManager）。正好补 `docs/phone-tools.md「iOS 的限制」` 里列的空白，但都高度敏感，要单独开关和确认。

### 7.3 hub 侧和手机工具有关的问题
- **全局一份工具表**：`register_phone` 整表覆盖 `h.phoneTools` 和 `phone_tools` 表（`hub.go:647-656`）；`register_phone` 的 `tools` 为空时不覆盖（`http.go:706-708`）。
  两台手机（iPhone + Android）都在线时，谁最后连上谁的工具表生效，调用也发给它（`http.go:542-556`）；Android 没有 `phone_reminders_*`，调用落到 Android 就是「未知工具」。
- **唤醒只走 APNs**：`wakePhone` → `push.wake("tool")` → 给**所有**有 APNs token 的设备发 `content-available`（`push.go:227-231`），Android 无从唤醒。
- 文案写死 iPhone：工具失败提示（`hub.go:699,718`）、助手说明（`assistant.go:117,128,164`）、实时快照（`server/internal/hub/assistant_snapshot.go:127-132`）、
  `ask_user` 工具描述（`server/internal/hub/assistant_tools.go:26`）。Android 用户会看到 / 模型会说「iPhone」。

---

## 8. 推送

### 8.1 现状：hub 只支持 APNs（`server/internal/hub/push.go` 全文）
- 配置：`~/.jarvis/apns.json`（`key_id, team_id, key_path, topic`）+ p8 key；没有就关闭推送（`push.go:28-92`）。ES256 JWT 缓存 40 分钟（`push.go:107-126`）。
- 设备表 `devices`：`device_id, name, kind, apns_token, apns_env, topic, prefs(jsonb), updated_at`（`0001_init.sql`、`0021_device_prefs.sql`）；
  注册接口 `POST /api/v1/devices` 直接解码成 `db.Device`（`http.go:104-112`），**没有平台 / 推送服务字段**；注册不覆盖 prefs（`push.go:94-105`）。
  按 `apns_env` 选 sandbox / production 主机，`topic` 按设备（`push.go:133-138,254-262`）。410 / BadDeviceToken / Unregistered 时删设备（`push.go:284-297`）。
  APNs 是一条长连 HTTP/2，出错时 `CloseIdleConnections`（`push.go:276-281`，见 `CLAUDE.md` 经验）。
- 推送种类：

| 种类 | 触发 / 策略 | APNs 负载要点 | 设备筛选 | 证据 |
| --- | --- | --- | --- | --- |
| 需要你（交互） | 新交互立即推；屏蔽的 Agent 不推 | `alert{title, subtitle:"<会话名> · <机器>", body}`，`sound`，`thread-id=<session_id>`，`category=INTERACTION_<KIND>`，`interruption-level=time-sensitive`；自定义键 `interaction_id, machine_id, session_id`；`apns-collapse-id=<interaction id>`，priority 10 | `prefs.input` | `push.go:305-340`、`hub.go:376-384` |
| 做完了 | working → done/idle 后等 `done_delay_seconds`（60）；期间恢复工作算同一轮；看过（herdr idle 或 App `seen`）/ 屏蔽 / 助手 3 分钟内已 `notify_user` / 收件箱 Agent / 由后台任务唤醒的一轮 都不推；还有后台任务时等到它们回报那一轮结束（最多 `background_hold_minutes` 120） | `alert{title:"✅ <标题>", subtitle:"机器 · 空间 › 标签", body:<最后一条回复>}`，`thread-id`；键 `machine_id, session_id` | `prefs.done`：off / long（≥`long_turn_seconds` 60）/ always | `machine.go:500-536`、`notify.go:11-267` |
| 助手 `notify_user` | 助手显式调用 | 同「做完了」格式，副标题「Persona」 | 所有设备 | `assistant_tools.go:116-125` |
| 静默唤醒 | 有 `phone_*` 调用而没有手机连着；30 s 内只发一次 | `{"aps":{"content-available":1},"jarvis_wake":"tool"}`，push-type background，priority 5 | 所有有 token 的设备 | `push.go:227-231`、`hub.go:722-753` |
| 实时活动（Fleet） | 见 8.3 | push-type liveactivity，topic `<bundle>.push-type.liveactivity` | `prefs.live_activity` 且有活动 / push-to-start token | `liveactivity.go` |

- 时间参数都在 `hub.json` 的 `notify`（`notify.go:20-50`，默认值是常量）；每台设备要哪些推送是 `devices.prefs`（`notify.go:52-137`）。
- `Pusher.clear(id)` 是空函数（`push.go:356`）：交互被处理后，已经弹出的通知不会被撤回（iOS 现状）。
- 屏蔽规则：Agent 在 `mutes.sessions` → 屏蔽；在 `allowed` → 不屏蔽；否则看它所在空间（`pins.go:41-57`）。

### 8.2 Android 要什么（FCM 方案）
（FCM / Android 的具体限制是平台知识，**未在本次调研中联网核实**，标「需核实」。）
- **全部用 FCM data message**（不要 notification message）：App 在 `onMessageReceived` 自己建通知，才能加「允许 / 拒绝」按钮（API 31+ `Action.setAuthenticationRequired(true)` ≈ iOS 的
  `.authenticationRequired`）、计划「打回」用 RemoteInput 输入意见、按 `session_id` 分组（≈ `thread-id`）、按 `interaction_id` 作通知 tag（≈ collapse-id），
  并且能在交互被处理后撤掉通知（iOS 做不到，Android 很容易：hub 多推一条 `interaction_resolved`，可选）。
- 优先级：需要你、做完了、notify_user、静默唤醒用 `android.priority=HIGH`；Fleet 数字变化用 `NORMAL`（Doze 时可能延迟，可接受）。对应现有 `urgent` 标志。
  不产生可见通知的高优先级消息会被 FCM 降级（需核实具体规则）——静默唤醒本来就限流 30 s 一次，问题不大。
- TTL 建议：交互 = 交互过期时间；做完了 ~1 小时；唤醒 ~20 s（hub 只等 20 s）；Fleet ~15 分钟（≈ stale-date）。
- 负载：data 的值必须是字符串，单条上限约 4 KB（需核实）；FleetState JSON（最多 3 条）放进一个字符串字段足够。
- **唤醒执行 phone_* 的 Android 对应**：FCM 高优先级 data `{type:"wake", reason:"tool"}` → `onMessageReceived`（处理时间窗口很短，需核实）→ 起一个短时前台服务
  （Android 14 `shortService` 等类型；高优先级 FCM 是后台启动前台服务的豁免条件之一，需核实）→ 连 WS、`register_phone`、执行 `tool_call`、回 `tool_result` → 停服务。
  可选优化（要改 hub）：把 `tool_call` 直接放进推送，手机用 HTTP 回结果（新接口，如 `POST /api/v1/phone/calls/{call_id}/result`），省掉后台建 WS 的时间。
- **没有 GMS 的风险**：国行等不带 Google Play 服务的手机收不到 FCM（需确认用户的 Android 机型）。备选：UnifiedPush（如 ntfy 分发器，hub 向 App 注册的 endpoint URL 发 POST）、
  厂商推送 SDK（小米 / 华为 / 荣耀 / OPPO / vivo，各自一套，重）、或 App 常驻前台服务保持 WS（hub 侧零改动，但耗电、易被 ROM 杀）。
  hub 的推送层应抽象成多 provider（apns / fcm / 以后 unifiedpush）。
- **网络**：hub 机器连 Google 的 IPv4 地址不稳定，Gmail 已经强制走 `google.proxy`（`CLAUDE.md` 经验、`docs/mail.md`）；FCM（fcm.googleapis.com、oauth2.googleapis.com）同样是 Google，
  发送和换 access token 都要走代理（复用 `google.proxy` 或单独配）。
- **凭据**：FCM HTTP v1 用服务账号 JSON（含私钥）换 OAuth2 access token（scope `firebase.messaging`，约 1 小时有效，缓存复用）。服务账号文件放 hub 机器（0600），
  `hub.json` 只写路径和项目 id，不进数据库和仓库（`CLAUDE.md`「密钥绝不入库」）。
- 错误处理：404 `UNREGISTERED`、400 `INVALID_ARGUMENT`（token 无效）→ 删设备；401 → 刷新 access token；429 / 5xx → 按 `Retry-After` 退避（需核实错误码细节）。

### 8.3 实时活动（Fleet）现状与 Android「实时更新」对应
现状（`docs/live-activity.md`、`liveactivity.go`）：
- 内容 `FleetState`（Go `liveactivity.go:39-74,151-159` ↔ Swift `ios/Shared/FleetState.swift`，字段逐一对应）：`phase`（needsYou / working / done / 空）、`needsYou`、`working`、`unread`、
  `items[{id:"机器/会话", title, place, kind, detail, interaction?, since}]`（最多 3 条）、`workingSince`、`details`、`assistant?`、`left?`/`right?`、`stats?{tpm,tokens,requests,loads}`。
  **时间都是 Unix 秒数字**。`FleetItem.interaction`（按钮要 resolve 的交互 id）在 `docs/live-activity.md` 的示例里没写。
- 触发：每次广播 `sessions` / `interaction` / `interaction_resolved` 都重算（`http.go:466-471`）；和上次一样不推；新的「需要你」立即推（priority 10），其他合并 2.5 s（priority 5）；
  `stale-date` = 现在 + 15 分钟，内容没变每 10 分钟续推；空了 1 分钟、或只剩做完未看 5 分钟 → `end`；活动超过 8 小时当作没了；只有数字变时每台设备至少隔 `live_metrics_seconds`（60）；
  每台设备按自己的设置裁剪（`forDevice`），一样的不推（常量，`liveactivity.go:29-37`）。
- 开始：有活动 token 就 update；没有而有 push-to-start token，就在「一轮」开始时推一次 `start`（每轮一次）。
- App 在前台时 hub 还广播 `{"type":"fleet"}`，App 自己更新活动（不占推送预算）；回前台拉 `GET /api/v1/fleet`。
- token 存 `live_activities` 表（`device_id, activity_id, push_token, push_to_start_token, started_at`，`0022_live_activities.sql`），接口 `POST /api/v1/devices/{id}/live-activity`。

Android 对应（Android 16 Live Updates，需核实细节）：
- 形态：「提升为常驻」的进行中通知（`Notification.Builder#setRequestPromotedOngoing(true)` + 标准样式 / ProgressStyle），状态栏胶囊显示短文本（`setShortCriticalText`），
  需要 `POST_PROMOTED_NOTIFICATIONS` 权限；要求 ongoing、有标题、不能 colorized、不能自定义 RemoteViews（需核实）。
- **没有「服务端直推活动」的通道**（不像 APNs 的活动 token / push-to-start）：只能 hub 发 FCM data（`{type:"fleet", state:"<FleetState JSON>"}`），App 收到后本地更新 / 取消这条通知。
- 映射：phase + 计数 → 胶囊短文本（如「💬2」「⚙3」），对应 `island.left`；第一条可操作的请求 → 通知按钮「允许 / 拒绝 / 批准」（同 `FleetIntents`）；
  `workingSince` → `setUsesChronometer(true)` + `setWhen()`，计时在本地走；stale → `setTimeoutAfter(15 分钟)`；`lock_details` → `VISIBILITY_PRIVATE` + 只有计数的 public 版本；
  点击 → `jarvis://open?machine=&session=`（只有一条时）或 `jarvis://open`。
- Android 16 以下：普通进行中通知（低重要性渠道）。三星 / 小米 / OPPO 等厂商的胶囊、焦点通知是否接 Live Updates API 需核实。
- 推送预算：FCM 没有 APNs 活动那种预算，但高优先级会被降级 → 计数变化用 NORMAL，新的「需要你」用 HIGH（沿用 `urgent`）。

---

## 9. 其他平台相关点

### 9.1 图片 / 文件
- **消息里的图片**：`POST …/messages`（和 `POST /assistant/conversations`）的 `images:[{mime, data(base64)}]` 内嵌在 JSON 里，整个请求体 ≤32 MB（`http.go:364`、`assistant.go:292`）。
  node 把图片写进临时文件，在消息末尾追加 `<attached_images>\n路径…\n</attached_images>`（`server/internal/node/control.go:279-286`）；App 解析这个标签，
  用 `GET /machines/{mid}/file?path=` 从会话所在机器取图显示缩略图（`docs/ios.md`「发的图片」）。Android 发图前应先压缩。
- **发文件**：`POST /machines/{mid}/files?name=`，原始字节流、必须带 `Content-Length`（OkHttp 要用已知长度的 RequestBody，不能 chunked，否则 411）；取消请求 = hub 发 `file.upload.abort`
  删半截文件；成功返回绝对路径，App 把路径插进输入框（含空格 / 引号时用单引号包）、**不自动发送**（`docs/protocol.md「发送文件」`、`upload.go`）。
- **消息里的标签（客户端解析）**：`<attached_images>`、`<jarvis-context kind="context|edit" title url>…</jarvis-context>`（App 内网页选中文字「AI 修改 / 作为上下文」）、
  `<cross-session-message from-name>`（`docs/protocol.md「消息里的标签」`）；另外要去掉 Claude 的 `[Image #N]` 占位和长粘贴的 `<pasted_content id=…>` 包装（`CLAUDE.md` 经验）。
- **会话里的文件路径**：点开走 `GET /machines/{mid}/file`（≤2 MB；二进制是 base64，PDF / 图片自己渲染）。

### 9.2 App 内链接
- 纯客户端能力（Telegram 式底部卡片、WebView 池预加载、选中文字菜单），hub 不参与；飞书文档 / 知识库走 `GET /api/v1/lark/raw/documents/…`（`docs/feishu.md`），
  文档里的图片要改写到 hub 的 raw 路径。Android：BottomSheet + WebView 池，WebView 的文字选择菜单用自定义 ActionMode 加「AI 修改」「作为上下文」。
- 加载 hub 上的媒体（飞书图片 / 视频、对话图片）都要鉴权：OkHttp / Coil 拦截器加 `Authorization`；ExoPlayer 可设请求头；实在只能用 URL 的地方用 `?token=`（hub 日志不记 query，`http.go:196-198`）。
  视频依赖 Range 透传（`CLAUDE.md` 经验）。

### 9.3 终端（`docs/terminal.md`、`docs/protocol.md「终端流」`、`server/internal/hub/terminal.go`）
- 依赖 hello 的 `terminal_stream` / `terminal_control` / `terminal_scroll` / `terminal_click`；没有 `terminal_stream` 时退回轮询 `GET …/screen`。
- 帧是 herdr 重画好的 VT 字节，用到的序列很少：CUP、ED(2J)、SGR（16 / 256 / 真彩色）、DECTCEM、2026、OSC 8（`docs/terminal.md` 第一节）。客户端要自己实现这个 VT 子集 + 宽字符两格
  （右半格占位写法的坑见 `CLAUDE.md`「网格里写宽字符要分两步」）。`epoch` 换了且不是 full、或 `seq` 不连续 → 丢网格重订阅（不带 epoch）。
- **接管尺寸**：客户端按字号算出能放下的 cols × rows，量出来之后才发 `terminal_subscribe{control:{cols,rows}}`；之后键盘起落 / 旋转 / 改字号防抖 250 ms 发 `terminal_input{control}`
  （客户端策略，`docs/terminal.md` 第十一节）。node 只在 cols、rows 都 > 0 且 herdr 支持时接管，否则按窗格原尺寸镜像（`server/internal/node/terminal.go:175-186`）。
- 按键：App 把键名换成终端字节（回车 `\r`、方向键 `CSI A–D`、带修饰 `CSI 1;m X`、ctrl+字母=控制字符、alt=前缀 ESC、shift+enter=`ESC \r`），多行粘贴包 bracketed paste；
  滚轮每行一个事件、落在手指那一格（`scroll` + `at`）；点击 `click`。
- 一个终端只有一个驾驶者：最新带 control 订阅的设备接管，之前的收到 `taken`；租约：hub 每 20 s `terminal.keepalive`，node 75 s 没收到就放手（常量）。
- 手机上的滚动历史只能靠客户端逐帧比对自己攒（接管期间 herdr 没有历史，`docs/terminal.md` 第十一节「手机上的滚动历史」）。
- 文档里的实测（`docs/terminal.md` 第十节，2026-10-29，样本数文档未写明）：herdr observe 最快 16–19 ms 一帧；Claude Code 2.1.312 流式时约 110 ms 重画一次；node 把 30 ms 内的帧合成一帧（常量）。
- 输入法：iOS 自己实现 `UITextInput`（拼音靠 marked text，`CLAUDE.md` 经验）；Android 对应是自定义 View 的 `onCreateInputConnection` / composing text。

### 9.4 深链
- `jarvis://pair?url=<hub>&token=<token>`：二维码 / 外部链接配对（`JarvisApp.swift:15-25`）。
- `jarvis://open`（只回到 App）、`jarvis://open?machine=<id>&session=<id>`（打开会话；实时活动用，`ios/LiveActivity/JarvisActivityAttributes.swift:12-31`）。
- 通知点击靠负载里的 `machine_id` / `session_id`（`JarvisApp.swift:195-206`）。
- Android：`jarvis` scheme 的 intent-filter（pair / open 两个 host）；配对二维码在 App 内扫（系统相机对自定义 scheme 的处理不一，需核实）。

### 9.5 手表
- Apple Watch 没有专用接口：用 answer 角色 token（`kind:"watch"`），配对和摘要经 WatchConnectivity 从 iPhone 推；打开时每 8 秒直接拉 hub（sessions / interactions / machines / inbox），
  回答 / 发消息 / 中断 / 收件箱完成和回复 / `seen` / `items?turns=2` 直接调 hub（`ios/JarvisWatch/JarvisWatchApp.swift:75-133,461-469`、`docs/ios.md「数据流」`）。手表不注册推送。
- Wear OS 可以照搬：answer token + Wearable Data Layer 传配对；手机通知（含按钮、RemoteInput）系统会桥接到手表（需核实）。hub 只需把默认设备名改得平台中立（见第 11 节）。

### 9.6 Siri / 快捷指令 → Android
- 4 个 App Intent 只用 REST、不用 WS：问 Claude（发给最近的活会话，或 `POST /sessions` 带 `prompt`）、状态、批准第一个待处理（permission → `{allow:true}`，plan → `{approve:true, mode:"bypassPermissions"}`）、打开会话（`docs/intents.md`）。
- Android 对应：App Shortcuts、快捷设置磁贴、桌面小组件；hub 不用改。

### 9.7 Gmail「使用 Google 账号登录」
- hub 的 Google OAuth 客户端是 **iOS 类型**：PKCE、无 client secret，回调是反向 client ID 的自定义 scheme `<反向 client id>:/oauth2redirect`（`server/internal/mail/oauth.go:23-26,107-154`）；
  `docs/mail.md` 明确写了「Google 对 Android / Chrome 应用停了自定义 scheme，iOS 仍然支持」。刷新 token 只带存下的 `client_id`、不带 secret（`oauth.go:320-326`）。
- Android 的路子（需核实）：用 Google 的 Authorization API 在手机上请求 `https://mail.google.com/` + 离线访问，拿到 server auth code 交给 hub，hub 用 **Web 类型**客户端（有 secret）换 refresh token；
  这要求 hub 新增 Web 客户端配置（secret 放文件）、新接口（如 `POST /api/v1/mail/oauth/google/exchange {server_auth_code}`），并且刷新时按账号存的 client 类型带上 secret。
  不改 hub 的退路：Gmail 用应用专用密码走普通 IMAP 账号（`POST /api/v1/mail/accounts`）；飞书邮箱、其他 IMAP 不受影响。

---

## 10. Android 客户端需要实现的协议清单

**A. 配对与凭据**
1. 解析 `jarvis://pair?url=&token=`（App 内扫码 + 深链）；`GET /api/v1/health` 校验并读 `features`。
2. `POST /api/v1/auth/tokens {name:<机型名>, kind:"phone"}` 换设备 token，存 Keystore 加密存储，丢弃配对 token；404（旧 hub）时继续用配对 token、下次启动再试。
3. 设置 › 设备：`GET/DELETE /api/v1/auth/tokens`；撤销本机 = 退出配对。Wear OS 用 `kind:"watch"`（answer 角色）。

**B. HTTP 基础**
4. Bearer 头、`X-Client-Request-Id`（8 位 hex）、错误解析（`error` 字符串或对象、`code` + 平铺字段、`X-Request-Id`），容忍纯文本 404/405。
5. JSON：显式字段名（`tool_input`、`answers`、`meta`、`args` 等内部键不能被命名策略改写，iOS 因此全用显式 CodingKeys，`ios/Shared/Models.swift:3-4`）；
   时间支持 0–9 位小数秒；早于 2000 年的时间当空；未知枚举值降级（iOS 对 status / state / kind 都有 unknown 兜底）。
6. 允许明文 HTTP（network security config）；各接口超时按 2 节。

**C. WebSocket**
7. 连接 `/api/v1/ws`，按 `type` 分发第 4.2 节全部事件，未知类型忽略；后台线程解码；退避重连（iOS 1→20 s）、网络恢复立即重连；`pingInterval(20s)`、45 s 无消息判死（iOS 策略）。
8. 连上后：`register_phone`（full 角色、开了手机数据时）；对每个打开的会话重新 `subscribe{since_seq:covered, lazy_images:true}`；终端带 `epoch/after_seq` 重订阅；`GET /interactions` 对账。
9. App 进后台断开 WS（除非在为唤醒跑前台服务），让 hub 知道手机不在、改走推送唤醒。

**D. 同步与发送**
10. `covered` / `history` / `from_seq` / `to_seq` 连续性算法、1.5 s 后补洞、超 8 页取最新页、`history` 变了重取；`has_more` 只信从末尾数的页。
11. 后台同步：`sessions` 里的 `rev` + `revs` 事件 → `after_seq` 拉增量，写内存或磁盘缓存（每会话最后 2 轮）；懒加载图片 + 永久缓存；`full_size` 看全文。
12. 发件箱：每条消息一个 `client_msg_id`；409 `dialog_open` / `input_draft`（`clear_draft` 重发）、502 `delivered:"unknown"`；离线排队、重连补拉完再按序发。
13. 打开 / 看着会话时发 `seen`。

**E. 交互**
14. question（多选用 `", "` 连接 label，或自定义文本）、plan（批准带 `mode`：bypassPermissions / acceptEdits / default；打回带 `feedback`）、permission（`allow`、`always`、拒绝带 `message`）、
    screen（`dialog.options` + `option` / `text`，`secret` 用密码框；没有 dialog 时退回原始屏幕 + `keys`）、`dismiss`；处理 410（已处理）和 409 `prompt_changed`。

**F. 手机工具**
15. 工具注册表（名字和参数尽量与 iOS 一致）、数据源开关、写操作确认 UI、`tool_result`（text / image 块）、模拟器带 `simulator:true`。

**G. 推送 / 实时更新**（依赖第 11 节 hub 改动）
16. 拿 FCM token → `POST /api/v1/devices`（新增的平台字段）；通知渠道：需要你（高）、做完了（默认）、Fleet 常驻（低）、静默唤醒（不可见）；交互通知按钮直接调 resolve；点通知按 `machine_id/session_id` 打开会话。
17. `GET/PUT /api/v1/devices/{id}/prefs`（同 iOS 字段，Android 忽略灵动岛专属语义或映射到状态栏胶囊）；前台用 WS `fleet` 事件、后台用 FCM `fleet` 更新实时更新通知，回前台 `GET /api/v1/fleet`。

**H. 终端**
18. VT 子集网格、镜像 / 接管两种模式、尺寸防抖、键名→字节、bracketed paste、滚轮逐行 + `at`、`click`、`busy → takeover` 弹窗、`taken / lost / unsupported` 状态、无 `terminal_stream` 时轮询 `/screen`。

**I. 其他领域**（照第 3 节接口表）
19. 助手（warm、conversations、routines、followups、runs、memory、`delta` 流式文字）、收件箱、飞书（媒体路径拼 hub 地址并鉴权；打开会话用 `around=<readPosition+1>`，只上报真正上屏的消息，`CLAUDE.md` 经验）、
    邮件（Gmail 登录见 9.7）、知识库、机器监控、用量、pins / mutes、Claude Code 设置、文件查看器、git。

---

## 11. 服务端改动清单

### 11.1 必要（不改，Android 就收不到推送 / 不能被唤醒执行手机工具）
| # | 改动 | 涉及文件 |
| --- | --- | --- |
| 1 | **设备注册带平台和推送服务**：`devices` 表加 `platform`（ios / android）、`push_service`（apns / fcm）、`push_token`（或 `fcm_token`）；旧 App 不带 → 默认 ios / apns；`POST /api/v1/devices` 接收新字段 | 新迁移（按 `CLAUDE.md` 的车道规则挑号，现有最大 `0061`）、`server/internal/db/db.go:122-133`（Device + Upsert / 查询）、`server/internal/hub/http.go:104-112` |
| 2 | **推送层按 provider 分发 + FCM 发送器**：`sendAs` 现在只会拼 APNs 请求，而且 `p.key == nil`（没有 apns.json）时直接 return、`ready()` 返回 false——**只有 Android 的部署连唤醒都不会尝试**；新增 FCM HTTP v1（服务账号 JWT 换 access token 并缓存、data message、HIGH / NORMAL、TTL、UNREGISTERED / INVALID_ARGUMENT 删设备、401 刷新、429/5xx 退避），走 `google.proxy` | `server/internal/hub/push.go:28-303`（`newPusher`、`ready`、`sendAs`）、新文件如 `server/internal/hub/fcm.go`、`server/internal/hub/config.go`（Config 加 `fcm{project_id, service_account_file}`）、`docs/config.md`、`docs/server.md` |
| 3 | **各类推送的 Android 负载**：交互（type、interaction_id、machine_id、session_id、kind、title、subtitle、body）、做完了、`notify_user`、唤醒（`{type:"wake"}`，HIGH，短 TTL） | `push.go:227-231,305-354`、`server/internal/hub/assistant_tools.go:116-125`（notify_user 调 `notifyDone`） |
| 4 | **唤醒按平台**：`wakePhone` 对 Android 设备发 FCM 高优先级 data；最好只唤醒有这个工具的那台 | `server/internal/hub/hub.go:722-753`、`push.go:227-231` |
| 5 | **手机工具按设备注册和路由**（iPhone、Android 同时在用时必要；Android 完全取代 iPhone 时可降为可选）：`register_phone` 带 `device_id` / `platform`，工具表按设备存（`phone_tools` 加 `device_id` 或每设备一行），`mcpTools` 合并去重，`callPhone` 按「有这个工具 → 前台在线 → 最近活跃 → 唤醒」选设备 | `hub.go:633-720`、`http.go:542-556,699-708`、`db.go`（ReplacePhoneTools / PhoneTools）、新迁移 |
| 6 | **实时活动的 Android 分支**（要做 Android 实时更新时必要）：`flush` / `metrics` / `endAll` 现在只遍历 `live_activities` 行并走 APNs liveactivity；对 `platform=android` 且 `prefs.live_activity` 的设备发 FCM `fleet` data（同样 `forDevice` 裁剪、同样 urgent 区分、结束时发空状态） | `server/internal/hub/liveactivity.go:483-606,641-664` |

### 11.2 可选（体验 / 一致性 / 安全）
| # | 改动 | 涉及文件 |
| --- | --- | --- |
| 7 | 文案平台中立（或按在线手机的平台说）：工具失败提示、助手说明、实时快照、`ask_user` 描述、设备 token 默认名 | `hub.go:699,718`、`server/internal/hub/assistant.go:117,128,164`、`server/internal/hub/assistant_snapshot.go:127-132`、`assistant_tools.go:26`、`server/internal/hub/devicetokens.go:153-159` |
| 8 | 交互处理后撤回已弹出的通知：`Pusher.clear` 现在是空函数，Android 可推一条 NORMAL 的 `interaction_resolved` data 让 App 取消通知 | `push.go:356`、`hub.go:572-580` |
| 9 | 推送直接携带 `tool_call`，手机用 HTTP 回结果（新接口如 `POST /api/v1/phone/calls/{call_id}/result`，复用 `toolResult`），省掉后台建 WS | `hub.go:693-765`、`http.go` 新路由 |
| 10 | Gmail 的 Android 登录：Web 类型 OAuth 客户端（secret 放文件）、server auth code 交换接口、按账号 client 类型刷新时带 secret | `config.go:32-38`、`server/internal/mail/oauth.go:107-154,320-326`、`server/internal/mail/http.go`、`docs/mail.md` |
| 11 | 一次性配对码代替二维码里的永久主 token（`jarvisd hub pair` 生成短时 code，`POST /api/v1/auth/pair {code}` 换设备 token） | `config.go:144-158`、`devicetokens.go` |
| 12 | 设备凭据和推送设备关联（`device_tokens` 记 platform / 推送 device_id），撤销凭据时顺带删推送注册和工具注册 | `devicetokens.go`、`db.go`、迁移 |
| 13 | 无 GMS 手机：UnifiedPush provider（hub 向 App 注册的 endpoint POST） | `push.go` 抽象层 |
| 14 | 文档对齐：protocol.md 补本文标 **[未文档]** 的接口和 WS 事件（`assistant_schedule`、`hook_event`、`error`、`seen`、`queue`、`info`、`file`、`lark/raw` 等），删掉代码里不存在的 `session` / `ping` / `pong`；核对 architecture.md「没有 App 在线时 hook 立即放行」 | `docs/protocol.md`、`docs/architecture.md` |

不需要改的：会话 / items / 交互 / 终端 / 文件 / 收件箱 / 飞书 / 邮件（除 Gmail 登录）/ 知识库 / 监控 / pins / mutes / 每设备通知设置——全部是平台无关的 JSON，Android 直接用。
`prefs` 的 `island` 结构可以原样复用为「状态栏胶囊显示什么」，不必加新字段。

---

## 12. 没查清 / 待验证

- 第 7、8、9 节标「需核实」的 Android 平台细节（FCM 负载上限 / 降级规则 / 处理时间窗、后台启动前台服务的豁免、Live Updates 的准入条件、Health Connect 的历史和后台读取权限、
  剪贴板后台写、Geocoder 可用性、通知桥接到 Wear OS、厂商胶囊是否接 Live Updates）——本次没有联网核实，是平台知识。
- 用户的 Android 手机有没有 Google Play 服务（决定 FCM 还是 UnifiedPush / 厂商推送 / 常驻连接）；iPhone 和 Android 会不会同时用（决定第 5 项是不是必要）；Android 的分发渠道（决定短信 / 通话记录类工具能不能做）。
- 只读了路由和类型，没细读的：收件箱 AI 分身、飞书客户端 `server/internal/lark`、知识库引擎、node 的无头助手实现（`delta` 语义只确认到 `server/internal/node/assistant.go:429`）。
- `screen` WS 事件：hub 还会转发，但 node 代码里没找到发 `session.screen` 事件的地方，按遗留处理（未逐个分支确认）。
- 本次没有做任何性能实测；文中引用的数字都注明了来源和样本数（文档未写样本数的已注明）。
