# hub 网络现状核查（公网直连 / IPv6 优先 / TLS / 配对）

> 调研对象：Persona worktree（master，ec11652 之后），只读核查。
> 约定：**[事实]** = 直接读代码 / 文档得到，带 `相对路径:行号`；**[判断]** = 基于事实的推断或建议。
> 隐私：本文不出现任何真实主机名、IP、域名、账号、token，一律用占位符（`<hub 地址>`、`<token>`）。

> 路径：同一节里第一次出现写全路径，之后简写文件名。`hub.go` `config.go` `http.go` `devicetokens.go` `terminal.go` `push.go` 都在 `server/internal/hub/`；`APIClient.swift` `LiveConnection.swift` `Keychain.swift` `WatchSync.swift` 在 `ios/Shared/`；Android 的 `data/` `ui/` `service/` 在 `android/app/src/main/java/jarvis/android/`。
> 状态：已完成（各节按 1→9 排列；第 0 节是摘要）。

## 0. 摘要

**现状 [事实]**
- 监听：`hub.json` 的 `listen` 默认 `:18660`，只有一个明文 `http.Server`（`server/internal/hub/hub.go:189-197`），没有 TLS，也没设任何超时和头大小（Go 默认：无超时、头 1 MB）。Go 对通配地址开双栈 socket，默认就收 IPv4 + IPv6（未在 hub 机器实测）。`public_url`（默认 `http://<主机名>:18660`）只进二维码和启动日志，不出现在任何响应里。
- 配对：`jarvisd hub pair` 是独立进程，读 hub.json 打印 `public_url`、**主 token** 和二维码 `jarvis://pair?token=…&url=…`（`server/internal/hub/config.go:149-162`）。主 token 永久有效、没有轮换命令。设备 token `jd_…` 只存 sha256，可撤销，查询缓存 30 秒（`server/internal/hub/devicetokens.go:57`）。
- 鉴权：`Bearer` 头，或**任意 `/api/` 路由**上的 `?token=`（两端客户端都没用）。失败不限流、不记来源 IP，`/api/v1/ws` 的失败连日志都没有；查不到的 token 也进缓存，而且缓存不清理。
- health：`{ok, version, features}`，不鉴权。hello：`{server_version, client_id, features}`，没有 `client_hello`。hub 每 20 秒 ping（15 秒超时，写死），每个连接一条 512 条消息的队列：单发时满了就断开，广播时满了**静默丢消息**。
- iOS：Keychain 存 `{url, token}`；三个 URLSession 都没有 delegate（做不了钉扎）；`NSAllowsArbitraryLoads`（手机 + 手表）；手表 token 存在手表的 `UserDefaults`；Intents、通知按钮、实时活动按钮各自读 Keychain 直连 hub；`jarvis://pair` 链接不确认就配对。
- Android：`SharedPreferences` 明文存 `{url, token}`；REST 和 WS 共用一个 OkHttp 客户端（`pingInterval` 20 秒）；`network_security.xml` 整体放开明文；`health()` 写了但没人调。

**改动 [判断]**（行数是估计，不是实测）
- 4a TLS：新文件 `tlscert.go`，`~/.jarvis/tls/` 下放 ECDSA P-256 自签证书（key 0600），钉 SPKI。`Run()` 加一个 TLS listener，或同端口嗅探。明文端口保留，只给内网 / VPN。约 150–250 行。
- 4b 多地址：新文件 `endpoints.go`，探测全局单播 IPv6（排除 ULA、链路本地、临时地址、TUN），加上配置的 `advertise`。经 hello、`GET /endpoints`、二维码下发，变化时广播 `endpoints`。health 只放 `hub_id`。约 150–250 行。
- 4c 配对码：新表 `pair_codes`。`hub pair` 经回环调运行中的 hub 生成配对码；不鉴权的 `POST /auth/pair` 兑换，必须限流；`--legacy` 给旧 App。约 200 行。
- 4d 拒绝公网明文 + 限流：按 `RemoteAddr` 判断来源，IPv6 按 /64 限流，限流放在查库之前；补 WS 鉴权失败的日志、给负缓存加上限、补 `ReadHeaderTimeout`。约 150 行。
- 4e `client_hello` 协商心跳：只改 `http.go`，约 40–60 行。
- 4f `hub_id`：存 `settings` 表，不用迁移，约 20 行。
- 客户端：统一一个带钉扎 delegate 的会话；候选地址并发探测（IPv6 先发）；AVPlayer 要先下载再播；Android 用自定义 `X509TrustManager`（`pin-set` 和 `CertificatePinner` 都绕不过自签）。

## 1. hub 怎么监听

### 1.1 配置来源 [事实]

| 项 | 位置 | 现状 |
| --- | --- | --- |
| 监听地址 | `server/internal/hub/config.go:50`（`Config.Listen`，json `listen`） | 默认 `":18660"`（`config.go:90`） |
| 对外地址 | `config.go:52`（`Config.PublicURL`，json `public_url`） | 默认 `fmt.Sprintf("http://%s:18660", host)`，`host` 是 `os.Hostname()` 的**完整**主机名（`config.go:87-92`） |
| 主 token | `config.go:51`（json `token`） | 默认 `randomHex(24)` = 48 位十六进制（`config.go:91`、`config.go:142-146`） |
| 配置文件 | `~/.jarvis/hub.json` | 目录 0700（`config.go:101`），首次运行按默认值写入，文件 0600（`config.go:108-112`）；别人先写的 hub.json（`scripts/db.sh` 只写 `database_url`）缺 `token` 时把生成的 token 回写（`config.go:119-130`） |

- `public_url` 在服务端**只有三处用到**：启动日志 `listening on %s (%s)`（`server/internal/hub/hub.go:196`）、二维码 `PairURL()`（`config.go:149-151`）、`hub pair` 打印（`config.go:158`）。
  它**不出现在任何 HTTP / WS 响应里**，也不参与推送。zoekt `public_url` / `PublicURL` 只命中这几处 + 测试 + 文档。
- 含义：给手机用的「hub 的访问地址」，单个字符串、单个 scheme（目前一律 `http://`）。

### 1.2 HTTP 服务器 [事实]

- `srv := &http.Server{Addr: cfg.Listen, Handler: h.routes()}`（`hub.go:189`），`srv.ListenAndServe()`（`hub.go:197`）；ctx 结束时 `Shutdown`，5 秒超时（`hub.go:190-195`）。
- **没有 TLS**：hub 包里没有 `crypto/tls`、`ListenAndServeTLS`、证书相关代码（zoekt `crypto/tls` 只命中 `server/internal/mail/*`）。
- **没有任何超时 / 头大小设置**：`ReadTimeout`、`ReadHeaderTimeout`、`WriteTimeout`、`IdleTimeout`、`MaxHeaderBytes` 都没设（zoekt `ReadHeaderTimeout` / `MaxHeaderBytes` 零命中）。
  按 Go 默认：无超时；请求头上限 `http.DefaultMaxHeaderBytes` = 1 MB。
- **没有用到客户端地址**：整个 `server/` 里 `RemoteAddr`、`X-Forwarded-*` 零命中，没有任何按来源 IP 的逻辑。
- 只有这一个 TCP 监听；node 的 `~/.jarvis/node.sock` 是 unix socket（`server/internal/node/hooks.go:80`），与手机无关。

### 1.3 双栈 [事实 + 判断]

- [事实] `Listen` 只有一个字符串、只起一个 listener。
- [事实，Go 标准库行为] `net.Listen("tcp", ":18660")`：host 为空或是未指定地址（`0.0.0.0` / `::`）时，Go 在支持 IPv4-mapped 的系统（Linux / macOS）上开一个 `AF_INET6` socket 并设 `IPV6_V6ONLY=0`（`net/ipsock_posix.go` 的 `favoriteAddrFamily`），所以默认值**本来就是双栈**，IPv4 连接以 `::ffff:a.b.c.d` 的形式进来。
  写成具体地址（如某个内网 IPv4）就只有那个地址族。**未在 hub 机器上用 `ss -ltnp` 实测**。
- [判断] 双栈监听本身不用改；公网 IPv6 能不能直达取决于 hub 机器的防火墙（`ip6tables` / `nft`）、路由器 / 光猫的 IPv6 入站策略（很多家用光猫默认拦入站 IPv6）。这些不在代码里，文档要写清楚。

### 1.4 路由与鉴权入口 [事实]

- 外层 mux 只挂两样（`server/internal/hub/http.go:19-23`、`http.go:159-160`）：
  - `GET /api/v1/health`：**不鉴权**；
  - `/api/`：全部经 `h.auth`。其他路径落到 Go 默认的纯文本 404。
- 最外层是 `logRequests`（`http.go:160`、`http.go:199-210`）：只记 ≥400 或 >2 秒的请求，格式 `[<id>] 方法 路径 -> 状态 (耗时)`，**不记 query、body、token、来源 IP**；
  而且 **`/api/v1/ws` 整条路径不记**（`http.go:206`），所以 WS 的 401 不会留下任何日志。

### 1.5 请求体 / 消息大小上限 [事实]

| 地方 | 上限 | 位置 |
| --- | --- | --- |
| `proxyBody`（发消息、按键、改名等转发给 node 的 POST） | 32 MB | `http.go:371` |
| 助手接口 | 32 MB | `server/internal/hub/assistant.go:297` |
| AI 小功能 | 64 KB | `server/internal/hub/ai.go:43` |
| 知识库更新 | 1 MB | `server/internal/hub/kb.go:664` |
| 邮件接口 | 1 MB | `server/internal/mail/http.go:125` |
| 上传文件 | 按 `Content-Length`，≤ 200 MB（`proto.MaxUploadBytes`），不给长度 411 | `server/internal/hub/upload.go:23-34`、`upload.go:62`、`server/internal/proto/proto.go`（`MaxUploadBytes = 200 << 20`） |
| WS 单条读 | 64 MB | `http.go:571` |
| 未设上限的 JSON 解码 | resolve（`http.go:90`）、`/devices`（`http.go:106`）、prefs（`http.go:122`）、新会话（`http.go:415`）、`/auth/tokens`（`devicetokens.go:133`） | 都在鉴权之后 |

[判断] 鉴权之后的接口不加上限问题不大；暴露公网后最该补的是**鉴权之前**的：`ReadHeaderTimeout`（防慢速攻击）、`MaxHeaderBytes` 收小、`IdleTimeout`。WS 和上传不能设整体 `ReadTimeout` / `WriteTimeout`（会掐断长连接和大文件），要么只设 `ReadHeaderTimeout`，要么按路由用 `http.ResponseController` 设截止时间。

## 2. 配对与鉴权

### 2.1 `jarvisd hub pair` [事实]

- 入口：`server/cmd/jarvisd/main.go:52-53` → `hub.Pair()`（`server/internal/hub/config.go:153-162`）。
- `Pair()` **不连正在运行的 hub**，自己 `LoadConfig()` 读 `~/.jarvis/hub.json`（没有就按默认值新建一份，`config.go:108-112`：副作用），然后打印：
  ```
  Server: <public_url>
  Token:  <主 token>

  用 Jarvis App 扫码配对：
  <终端二维码>
  ```
  （`config.go:158`），二维码 `qrterminal.GenerateHalfBlock(cfg.PairURL(), qrterminal.L, os.Stdout)`，纠错级别 L（`config.go:159`）。
- 二维码内容：`"jarvis://pair?" + url.Values{"url": {PublicURL}, "token": {Token}}.Encode()`（`config.go:149-151`），即
  `jarvis://pair?token=<主 token>&url=<public_url 百分号编码>`（`Values.Encode` 按键名排序）。**只有一个地址，就是 `public_url`**，不探测本机地址。
- 测试：`server/internal/hub/hub_test.go:164-177`（特殊字符编码）、`hub_test.go:33-34`（默认 public_url 含 `:18660`）。

### 2.2 主 token [事实]

- 永久有效：没有过期、没有轮换命令；只能手改 hub.json 再重启 hub。比较用 `subtle.ConstantTimeCompare`（`server/internal/hub/devicetokens.go:69`），角色 `full`、`ID` 为空。
- 二维码和 `hub pair` 输出里放的就是它。App 配对后会换设备 token 并「不再保存」主 token（见 5.1 / 6.1），但主 token 本身一直有效。

### 2.3 设备 token [事实]

| 动作 | 接口 | 位置 | 说明 |
| --- | --- | --- | --- |
| 发放 | `POST /api/v1/auth/tokens {name, kind}` | `devicetokens.go:128-168` | 调用者必须是 `full`（主 token 或手机 token）。`kind=watch` → 角色 `answer`；`phone` → `full`；其他记成 `other`、`full`。token = `"jd_" + randomHex(24)`（`:160`），库里只存 sha256（`:59-62`、`:162`）；id = `dev_` + 6 字节十六进制；返回 `{id, token, role, name}` |
| 列表 | `GET /api/v1/auth/tokens` | `:169-179` | `{tokens, current}`（`current` 是调用者自己的 id） |
| 撤销 | `DELETE /api/v1/auth/tokens/{id}` | `:180-197` | 库里标记撤销 → `revoked()`（`:200-222`）清缓存、`CloseNow` 这台设备所有 WS |
| 查找 | `authenticate()` | `:65-109` | 先比主 token，再按哈希查库；**结果缓存 30 秒**（`tokenCacheFor`，`:57`、`:80`），`last_seen` 最多 5 分钟写一次（`:99-107`） |
| 权限 | `allowed()` | `:113-124` | `full` 全部；`answer` 只能 GET/HEAD + 正则 `answerWrites` 里的写（resolve、发消息、seen、interrupt、收件箱 done/reply/draft/refresh、`devices`） |

[判断] 任何 `full` 设备 token 都能再发新 token、撤销别的设备（含主 token 之外的所有设备）。公网后一台手机的 token 泄露 = 能自己续命（发新 token）。一次性配对码落地时，可以考虑「只有主 token / 配对码能发 `full` token」。

### 2.4 鉴权失败：没有限流，几乎没有记录 [事实]

- `h.auth`（`server/internal/hub/http.go:163-184`）：取 `Authorization: Bearer`，没有就取 **`?token=`**（`:165-168`）；认不出 → `401 {"error":"invalid token"}`（`:169-172`）；`answer` 角色越权 → 403（`:174-177`）。
- **没有限流、没有锁定、没有退避**：`server/` 里 zoekt `rate.Limit`、`golang.org/x/time` 零命中，`go.mod` 也没有这个依赖。
- **不记来源**：`RemoteAddr` 零命中。`logRequests` 只记 `[<id>] GET /api/v1/... -> 401 (耗时)`；**`/api/v1/ws` 的失败完全不记**（`http.go:206`）。
- **负缓存无上限** [事实 + 判断]：库里查不到的 token 也会以 `c.p == nil` 存进 `devtok.cache`（`devicetokens.go:85-94`），这个 map 只在撤销时删条目（`:203-208`），没有过期清理。公网上被人用随机 token 刷：每个新 token 一次 Postgres 查询 + map 永久多一条 → 内存只增不减。限流要放在查库**之前**，缓存要有上限或定期清。

### 2.5 `?token=` 查询参数 [事实]

- 服务端：对**所有** `/api/` 路由生效（不只 WS），`http.go:165-168`。`docs/protocol.md:7` 写的是「WebSocket 也可用 `?token=`」。
- 客户端**目前没人用**：
  - iOS 的 REST、上传、图片、全文、飞书文档、WS 全部走 `Authorization` 头（`ios/Shared/APIClient.swift:116`、`:150`、`:278`、`:291`；`ios/Shared/LiveConnection.swift:236`；`ios/Jarvis/Views/Feishu/Docs/FeishuDocsComponents.swift:12`），视频用 `AVURLAssetHTTPHeaderFieldsKey` 带头（`ios/Jarvis/Views/Inbox/Lark/LarkMedia.swift:299`）。
  - Android 的 REST 和 WS 也是头（`android/app/src/main/java/jarvis/android/data/ApiClient.kt:43`、`:113`、`:122`；`data/Hub.kt:103`）。
  - zoekt `token=` 只命中配对链接、测试、脚本。
- [判断] 公网化时可以收紧为「只有 WS 握手接受 `?token=`」或干脆去掉（两端都已用头），避免 token 出现在任何反向代理 / 访问日志里。去掉前确认没有旧版 App 依赖（目前代码里没有）。

## 3. 健康检查与 WebSocket

### 3.1 `/api/v1/health` [事实]

- `GET /api/v1/health`，不鉴权，`http.go:21-23`：`{"ok": true, "version": <hub 版本>, "features": proto.Features}`。
- `proto.Features`（`server/internal/proto/proto.go:40-54`）：`client_msg_id`、`send_refusals`、`screen_dialog`、`resumable_interactions`、`secret_prompts`、`request_id`、`terminal_stream`、`terminal_control`、`terminal_scroll`、`terminal_click`、`file_upload`。
- 没有 `hub_id`、没有地址列表、没有 TLS 信息。`docs/protocol.md:15` 写的是 `{"ok":true,"version":"..."}`，没写 `features`（第 16 行另说了 features）。
- [判断] 不鉴权的 health 暴露了版本号和能力位；公网上要不要隐藏版本可以讨论（客户端多地址探测正好要用一个不鉴权、便宜的接口，见 4b）。

### 3.2 WS 握手与 `hello` [事实]

- `GET /api/v1/ws` 挂在鉴权之后（`http.go:154`），`serveWS`（`http.go:566-640`）。
- `websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})`（`http.go:567`）：关掉了 coder/websocket 的 Origin 校验（浏览器跨站发起的 WS 也会被接受，前提是带得出 token）。
- `conn.SetReadLimit(64 << 20)`（`:571`）。
- 连上后依次发（`:624-631`）：
  1. `{"type":"hello","server_version":…,"client_id":<6 字节随机十六进制>,"features":[…]}`；
  2. `{"type":"machines",…}`；
  3. 每台机器一条 `sessions`；
  4. 每个待处理交互一条 `interaction`。
- 客户端能发的类型（`onClientMessage`，`http.go:642-731`）：`terminal_subscribe`、`terminal_input`、`terminal_unsubscribe`、`subscribe`、`unsubscribe`、`register_phone`、`seen`、`tool_result`。
  **没有 `client_hello`、没有 JSON 的 `ping` / `pong`**（`docs/protocol.md:176`、`:189` 写了 `ping` / `pong`，代码里没有；hub-changes.md 4.12 已记）。

### 3.3 心跳、发送队列、慢连接 [事实]

| 项 | 值 | 位置 |
| --- | --- | --- |
| 协议层 ping 间隔 | 20 秒（`time.NewTicker(20 * time.Second)`，写死在 `serveWS` 的写协程里） | `http.go:598` |
| ping 超时 | 15 秒，失败即 `cancel()` 断开 | `http.go:612-620` |
| 单条写超时 | 20 秒，失败即断开 | `http.go:604-611` |
| 每连接发送队列 | `make(chan []byte, 512)`（按条数，不按字节） | `http.go:573` |
| 队列满（`sendJSON`） | `conn.Close(StatusPolicyViolation, "too slow")` 断开，App 重连后补齐 | `http.go:456-467` |
| 队列满（终端帧 `sendTerm`） | 同上，异步关 | `server/internal/hub/terminal.go:481-491` |
| 队列满（`broadcast` / `toSubscribers` / `itemsToSubscribers`） | **静默丢这一条，不断开** | `http.go:469-488`、`:490-509`、`:512-548` |
| 断开清理 | 删客户端、退订（`removeWatcher`）、`terminalDrop`、`CloseNow` | `http.go:579-595` |

- [判断] 广播静默丢消息是「慢连接」最隐蔽的问题：`interaction` / `interaction_resolved` / `sessions` 丢了，连接不断、客户端也不知道，要等下次全量（重连的 hello 之后）才对上。Android 前台服务这种常驻、经常处在弱网的连接更容易碰到。改法：广播队列满时也断开（和 `sendJSON` 一致），或给事件编号让客户端发现缺口。
- 客户端侧（iOS）：每 20 秒 `sendPing`，挂着时每 5 秒，5 秒没 pong 记弱网，45 秒没收到任何东西判死（`ios/Shared/LiveConnection.swift:178-181`、`:249-284`），见第 5 节。

### 3.4 按连接调心跳：改动点 [判断]

- 现在 ping 间隔是写死在 `serveWS` 写协程里的局部 `ticker`（`http.go:598`），`client` 结构（`http.go:440-454`）里没有任何心跳相关字段。
- 改法（改动面小，集中在 `http.go`）：
  1. `client` 加 `pingEvery time.Duration` 和一个 `setPing chan time.Duration`；
  2. 写协程里用这个 ticker，`select` 多一个 `case d := <-c.setPing: ping.Reset(d)`；
  3. `onClientMessage` 加 `case "client_hello"`：读 `heartbeat_seconds`（夹到比如 15–300 秒），写进 `setPing`；顺带记 `platform` / `device_id` / `version`（hub-changes.md 3.3）；
  4. 「死连接判定」现在只靠 ping 失败（15 秒超时），后台长间隔时可以同时放宽 ping 超时。
  5. `features` 加 `client_heartbeat`；实际生效的间隔另回一条（`hello` 在连上时就发了，早于 `client_hello`，见 4e）。旧 hub 不认 `client_hello` 会直接忽略（`json.Unmarshal` 后 switch 落空，`http.go:669-672`），兼容。
- 要注意：hub 侧 ping 是「hub 发、手机回」，后台连接的耗电主要在手机被唤醒收发；Android OkHttp 自己的 `pingInterval` 也要一起调（第 6 节）。NAT / 运营商网关会回收空闲的 TCP 连接，心跳间隔超过它的空闲超时就会被中间设备掐断；这个超时因网络而异，**未实测，不给具体值**。

## 4. 实现「公网直连」服务端要改的地方

> 本节的「改动面」是按读代码估的行数量级，**不是实测**；数字型参数（限流次数、心跳秒数、证书年限）都是建议值，要在真机 / hub 机器上验证后再定。

### 4a. TLS 监听、自签证书、SPKI 指纹

**现状 [事实]**：只有 `srv.ListenAndServe()` 一个明文 listener（`server/internal/hub/hub.go:189-197`）；`Config` 没有任何 TLS 字段（`server/internal/hub/config.go:49-77`）；
`~/.jarvis/` 下已经放着同类的私密文件 `secret.key`、`apns_key.p8`（`docs/server.md:66-74`）；Postgres 占了 `127.0.0.1:18661`（`docs/server.md:48`）。

**建议 [判断]**：
- 新文件 `server/internal/hub/tlscert.go`：`loadOrCreateTLS(dir) (tls.Certificate, pin string, error)`。
  - 存放：`~/.jarvis/tls/hub.key`（PKCS#8，0600）+ `hub.crt`，目录 0700。符合 CLAUDE.md「密钥绝不入库」（不进 Postgres），和 `secret.key` 一样是文件；不是「JSON 状态文件」。
  - 算法：ECDSA P-256 + SHA-256 签名（iOS ATS 的密钥 / 签名要求都满足，见 5.5）；`MinVersion: tls.VersionTLS12`。
  - SAN：放主机名和配置里的域名即可；客户端按 SPKI 钉扎、不校验主机名，地址是 IP 也没关系。
  - 指纹：`base64(sha256(cert.RawSubjectPublicKeyInfo))`（和 HPKP 的 `pin-sha256` 同格式）。**钉公钥不钉证书**：证书快到期时用同一把私钥重签，指纹不变；换私钥 = 所有设备要重新配对（或者先在 hello 里预告 `next_pin`，可选）。
  - 两个进程都可能先生成（`jarvisd hub` 和 `jarvisd hub pair`）：写文件用 `O_EXCL`，失败就读别人写好的。
  - 用户自己有正规证书（比如域名 + Let's Encrypt）时可选 `tls.cert_file` / `tls.key_file`；这种情况下客户端也可以走系统信任，但证书续期常会换私钥，钉扎要钉 CA 或者关掉。第一版可以只做自签。
- `hub.json` 新字段（`config.go:49-77`、`defaultConfig()` 在 `:86-96`）：`tls: {listen, cert_file?, key_file?}`。**端口避开 18661**（Postgres）。
- `Run()`（`hub.go:189-200`）两种做法：
  1. **两个 listener**（简单，推荐第一版）：保留明文 `listen`，另起 `tlsSrv.ListenAndServeTLS("", "")`，共用 `h.routes()`；`Shutdown` 两个。约 30 行。
  2. **同端口嗅探**：自己的 accept 循环，在每连接的 goroutine 里带超时 peek 第一个字节，`0x16` 包成 `tls.Server(conn, cfg)`，否则原样，再从一个 channel 喂给 `srv.Serve(ln)`（不能在 `Accept` 里同步 peek，会被不发数据的连接卡住）。约 100 行；好处是只有一个端口、一条防火墙规则，旧 App 的 `http://<hub>:18660` 和新 App 的 `https://<hub>:18660` 都能用。
- 两个 server 都补上 `ReadHeaderTimeout`（如 10 秒）、`IdleTimeout`（如 120 秒）、`MaxHeaderBytes`（如 64 KB）。不要设整体 `ReadTimeout` / `WriteTimeout`（会掐断 WS 和上传）。
- HTTP/2：`ListenAndServeTLS` 默认开 h2。WS 走 HTTP/1.1 Upgrade（coder/websocket 的 `Accept` 要 `http.Hijacker`），iOS `URLSessionWebSocketTask`、OkHttp 的 WebSocket 都走 HTTP/1.1，REST 走 h2 没问题。想少一个变量可以先 `TLSNextProto: map[string]func(...){}` 关掉 h2。
- **明文端口留不留**：留，但只对非公网来源开放（4d）。理由：旧 App 不会钉自签证书（iOS 用 `URLSession.shared` 系统信任、Android 用系统 CA，握手直接失败），内网 / VPN 里的旧设备和 CLI、`sim.sh`、`mock_hub.py` 的测试都靠它。等新 App 普及后再考虑把默认 `listen` 收到回环 / VPN。新 App 在内网也可以直接走 TLS + 钉扎。
- `hub pair`（`config.go:153-162`）多打印一行 `Fingerprint: sha256/<pin>`，二维码加 `pin=`（`config.go:149-151`）。
- **改动面**：Go 约 150–250 行 + 测试（生成 / 复用 / 指纹稳定性 / 嗅探），`docs/config.md`、`docs/server.md`、`docs/protocol.md`。

### 4b. 多地址下发（IPv6 探测、配置的公网地址 / 域名）

**现状 [事实]**：只有单个 `public_url`，而且只进二维码和日志（1.1）；没有任何地址探测；`golang.org/x/sys` 已经是依赖（`server/go.mod`）；
`server/internal/sysmon/linux.go:95-103` 有「是不是物理网卡」的判断，但它把网桥、bond、VLAN 都算虚拟，hub 的全局 IPv6 常常就在网桥上，**不能直接拿来筛地址**。

**建议 [判断]**：
- 配置：`public_url` 保留（旧二维码、旧 App）；新增 `advertise: ["https://<域名>:<端口>", …]`（手填：域名 / DDNS、路由器端口转发的 IPv4）、`advertise_ipv6`（自动探测开关，默认开）、`advertise_lan`（内网地址，默认开）。
- 探测（新文件 `server/internal/hub/endpoints.go`，纯函数部分可单测）：
  - `net.Interfaces()` + `Addrs()`，保留 up、非回环接口上的 IPv6 **全局单播**；排除 `netip.Addr.IsPrivate()`（ULA，含 Tailscale 的 IPv6）、链路本地、文档前缀 `2001:db8::/32`；
  - Linux 上用 netlink（`unix.NetlinkRIB(RTM_GETADDR)`，x/sys 已有）读 `IFA_F_TEMPORARY` / `IFA_F_DEPRECATED` / `IFA_F_TENTATIVE`，**不下发临时（隐私）地址**，它们一天左右就换；
  - TUN 代理网卡的地址要排除（hub 机器走 TUN 代理，见 CLAUDE.md 经验；这类网卡常见 ULA 或 RFC 2544 的基准测试网段 198.18/15，**未在 hub 机器上实测**）；
  - 公网 IPv4 在 NAT 后面探测不到，只能来自 `advertise`；内网 IPv4 作为 `scope: lan` 候选。
  - 每 60 秒重探一次（或订阅 netlink 的地址变更），变了就记一行日志。
- 每个候选：`{"url": "https://[<v6>]:<端口>", "family": "ipv6|ipv4|name", "scope": "global|lan|configured", "tls": true}`，按「全局 IPv6 → 配置的域名 → 内网 → public_url」排序（客户端照样自己并发探测，见 5.5 / 6.2）。
- 下发：
  - WS `hello`（`http.go:624`）加 `hub_id`、`endpoints`、`tls_pin`；
  - 新的鉴权接口 `GET /api/v1/endpoints`（给只走 HTTP 的手表、Intents）；
  - `/api/v1/health`（不鉴权）只加 `hub_id`（探测时确认是同一台 hub），**不放地址列表**，免得任何能扫到端口的人拿到这台机器的内网 / 其他地址；
  - 二维码：多个 `url=`（或新键 `alt=`）+ `pin=` + `hid=`（4f）+ `code=`（4c）。二维码纠错级别 L、几个 IPv6 地址加起来一两百个字符，终端二维码会变大，可以只放 2–3 个，其余连上后从 hello 拿。
- 变化事件：`h.broadcast({"type":"endpoints","endpoints":[…]})`（`http.go:469-488`）。iPhone 在后台收不到 WS，可以顺带发一条静默推送（`push.go` 已有 `wake`）带新列表，App 写回 Keychain。
- **坑 [判断]**：家宽 IPv6 前缀可能被运营商定期换（PPPoE 重拨），手机记住的地址全部失效时就再也问不到新地址。文档要建议配一个 DDNS 域名（AAAA 记录）作兜底候选，VPN / 内网地址也留着。
- **坑 [判断，未实测]**：hub 机器开了 TUN 代理的 auto-route 时，外部 IPv6 连进来的回包可能被路由进代理，连接建不起来。公网直连前要在 hub 机器上实测（手机用蜂窝网络访问 `https://[<hub 的 IPv6>]:<端口>/api/v1/health`）。光猫 / 路由器默认拦入站 IPv6 也很常见，文档要写放行方法。
- **改动面**：Go 约 150–250 行 + 测试；客户端解析和持久化见第 5、6 节。

### 4c. 一次性配对码代替二维码里的永久主 token

**现状 [事实]**：二维码里就是主 token（`config.go:149-151`）；`Pair()` 是独立进程，只读 hub.json、不连运行中的 hub（`config.go:153-162`）；
hub 的内存状态会被部署清掉（CLAUDE.md 经验：跨请求的流程要自包含或落库）；`POST /api/v1/auth/tokens` 只给 `full` 调（`devicetokens.go:137-140`）。

**建议 [判断]**：
- 新迁移 `pair_codes(hash text primary key, expires_at timestamptz, used_at timestamptz, used_by text, created_at timestamptz)`。迁移号按车道规则挑（现有 `0002`、`0022`、`0031`、`0041`、`0051`、`0061`，挑号前看所有 worktree 和 `jarvis_test` 的 `schema_migrations`，CLAUDE.md）。
- `jarvisd hub pair`（`server/cmd/jarvisd/main.go:52-53`、`config.go:153-162`）：
  - 默认：用 hub.json 里的主 token 调**本机回环上运行中的 hub**：`POST http://127.0.0.1:<listen 端口>/api/v1/auth/pair-codes` → hub 生成、落库、返回 `{code, short_code, expires_at, endpoints, pin, hub_id}`；`hub pair` 打印二维码 `jarvis://pair?v=2&code=…&pin=…&hid=…&url=…&url=…`、手动输入用的短码、指纹、过期时间。
    这样地址探测、证书、hub_id 都只在 hub 进程里一份逻辑，`hub pair` 顺带确认 hub 在跑。
  - `--legacy`：老格式（`url=` + 主 token），给旧 App。
  - hub 没在跑：报错提示先启动（或退回直接写库，`Pair()` 能读到 `database_url`）。
- hub：`POST /api/v1/auth/pair {code, name, kind}`，挂在 `h.auth` **外面**（和 health 一样，`http.go:21`），必须限流（4d）。
  - 原子地用掉：`UPDATE pair_codes SET used_at = now(), used_by = $2 WHERE hash = $1 AND used_at IS NULL AND expires_at > now() RETURNING …`；
  - 发 token 和 `POST /auth/tokens` 共用一个函数（把 `devicetokens.go:145-167` 抽成 `issueToken(ctx, name, kind)`），返回 `{id, token, role, name, hub_id}`；
  - 码的长度：二维码里用 128 位随机；手动输入的短码 8 位 Crockford base32（约 40 位）+ 5 分钟过期 + 限流；
  - `features` 加 `pair_code`。
- 主 token：留给 CLI 和旧 App。建议加 `jarvisd hub rotate-token`（换主 token，旧 App 要重新配对），并考虑「开了公网模式时主 token 只接受回环 / 内网来源」。
- **改动面**：Go 约 200 行（迁移、db 函数、两个路由、`Pair()` 改写、测试）；两端的解析和手动输入页（「地址 + 配对码」，token 输入框收进「高级」）。

### 4d. 拒绝来自公网的明文请求；鉴权失败限流

**现状 [事实]**：服务端完全不看来源地址（`RemoteAddr` 零命中）；鉴权失败不限流、只在非 WS 路径记一行不带来源的日志（2.4）；负缓存无上限（`devicetokens.go:85-94`）。

**建议 [判断]**：
- 来源判断 `clientIP(r)`：`netip.ParseAddrPort(r.RemoteAddr)` 后 `Unmap()`（双栈 socket 上 IPv4 是 `::ffff:` 映射地址，见 1.3）；
  「本地」= `IsLoopback() || IsPrivate() || IsLinkLocalUnicast()` + `100.64.0.0/10`（CGNAT，Tailscale 的 IPv4，`IsPrivate` 不含）+ 配置的 `trusted_cidrs`（VPN 网段）。
- 明文 listener 上 `r.TLS == nil && !本地` → `403 {"error":"…","code":"tls_required"}`；挂在 `routes()` 最外层（`http.go:159-160`）或明文 listener 的包装里。可以只放行不鉴权的 `/api/v1/health`，方便客户端给出「请用 https」的提示。
- **注意**：token 在请求里，hub 拒绝时它已经明文走过公网了。真正的保护在客户端：**公网地址上绝不用 `http://` 发 token**（5.5、6.2）。hub 这一道是让错误配置「立刻坏掉、看得见」。
- **注意**：前面套了反向代理 / 隧道（frp 之类）时来源会变成 `127.0.0.1`，被当成本地。文档要写清楚：用代理就只让代理连 TLS 端口，或者配 `trusted_proxies` 后才读 `X-Forwarded-For`。路由器端口转发（DNAT）一般保留真实来源地址。
- 限流（新文件，比如 `ratelimit.go`，不必引入新依赖）：
  - 键：IPv4 按 /32，**IPv6 按 /64**（攻击者通常握有整个 /64）；
  - 规则示例：同一键 10 分钟内失败 10 次 → 封 15 分钟，返回 `429` + `Retry-After`；再加一个全局失败速率上限，保护数据库。**数字未经实测**，上线前按实际调；
  - 位置：`h.auth`（`http.go:163-184`）里**先查是否被封、再 `authenticate()` 查库**；失败时计数；`POST /auth/pair` 同样处理；
  - 负缓存加上限或定期清理（`devicetokens.go:85-94`）；
  - 日志：失败记一行（来源按 /64 或 /24 打码、路径、计数），**包括 `/api/v1/ws`**：改 `http.go:206` 的条件（比如 401 / 429 时 WS 也记），日志本身也要限速。
- 顺带：`websocket.AcceptOptions{InsecureSkipVerify: true}`（`http.go:567`）关掉了 Origin 校验。WS 握手要 token，浏览器跨站发起又带不了 `Authorization` 头，只有 `?token=` 能做到，风险低；收紧 `?token=`（2.5）后基本无碍。
- **改动面**：约 150 行 + 测试。

### 4e. `client_hello` 里协商心跳间隔

见 3.4。改动集中在 `http.go`（`client` 结构 `:440-454`、写协程 `:597-622`、`onClientMessage` `:642-731`、`hello` `:624`）+ `proto.Features` 加能力位（`server/internal/proto/proto.go:42-54`）+ `docs/protocol.md`。约 40–60 行。
hub-changes.md 3.3 的 `client_hello`（平台、版本、能力）和 3.8 的应用层 ping / pong 可以一起做：客户端发 `{"type":"client_hello","device_id","platform","version","features":[…],"heartbeat_seconds":N}`，hub 回一条确认，带实际生效的间隔。
注意顺序：hub 的 `hello` 是连上就发的（`http.go:624`），`client_hello` 在它之后到，所以「生效值」要另回一条（比如 `{"type":"client_hello_ack","heartbeat_seconds":N}`），或者客户端在握手时用请求头 / 查询参数带上。

### 4f. `hub_id`

**现状 [事实]**：服务端没有 `hub_id`（zoekt 零命中）；`settings` 表是通用的键值表（`server/internal/db/migrations/0002_settings.sql`，`Setting` / `PutSetting` 在 `server/internal/db/db.go:444-468`，`Store` 接口 `hub.go:103-104`）。
**建议 [判断]**：`Run()` 里 `db.Open` 之后（`hub.go:140-145`）读 `settings["hub_id"]`，没有就用 `crypto/rand` 生成 UUIDv4 写回，存在 `Hub` 上。不用新迁移，约 20 行。
下发：`/api/v1/health`（`http.go:21-23`）、WS `hello`（`http.go:624`）、配对码兑换的响应、二维码 `hid=`。
语义：库在，id 就在（换机器带着数据库迁移也不变）；清库就是新 hub，客户端整体清掉本地数据（`docs/redesign/README.md` D6 的作用域）。
TLS 指纹本身也能标识 hub，但内网明文连接没有指纹，数据作用域也不该跟证书走，所以两个都要。

## 5. iOS 客户端

### 5.1 配对信息怎么存 [事实]

- 类型：`ServerConfig { url: URL, token: String }`，`Codable`（`ios/Shared/APIClient.swift:3-27`）；`init?(pairURL:)` 只认 `jarvis://pair?url=…&token=…`（`:7-16`）；体验模式是 `https://demo.invalid`（`:25-26`）。
- Keychain（`ios/Shared/Keychain.swift`）：通用密码，`service` = App 的 bundle id（手表上去掉 `.watchkitapp`，`:6`），`kSecAttrAccessibleAfterFirstUnlock`（`:14`），**没设 access group**。
  - account `server`：手机自己的 `ServerConfig`（`:42-48`），JSON 编码整个结构；
  - account `watch`：给手表换的只读回答 token（`:34-40`）。
- 换 token：`AppModel.pair()`（`ios/Jarvis/App/AppModel.swift:140-156`）先 `client.machines()` 校验，再存 Keychain、清设备凭据、重连、推给手表；
  `upgradeCredentials()`（`ios/Jarvis/App/DeviceCredentials.swift:15-39`）用配对 token 调 `POST /api/v1/auth/tokens kind=phone`，**用新 token 覆盖 Keychain 里的 `server`**（`:22-28`），设备 id 记在 `UserDefaults` 的 `auth.deviceID`（`:10`、`:25`）；再给手表换 `kind=watch`（`:33-37`）。旧 hub 没这个接口就继续用配对 token（`catch { return }`，`:29-31`）。
- 配对入口：
  - 扫码 / 手动：`PairView`（`ios/Jarvis/Views/Settings/SettingsView.swift:367-428`，`connect()` 在 `:415`），手动输入是「服务器地址」+「Token」两个框；
  - 链接：`onOpenURL` 解析成 `ServerConfig` 后**直接** `model.pair(cfg)`（`ios/Jarvis/App/JarvisApp.swift:15-25`），不确认、已配对也会被换掉；
  - DEBUG：`-pairURL` 启动参数（`JarvisApp.swift:46-49`）。

### 5.2 URL 怎么拼 [事实]

| 用途 | 怎么拼 | 用哪个 URLSession | 位置 |
| --- | --- | --- | --- |
| REST | `config.url.appending(path:)` + query | `APIClient.session`，默认 `URLSession.shared` | `APIClient.swift:89`、`:100-105`、`:107-123` |
| 上传 | 同上，`/machines/{mid}/files?name=` | `APIClient.uncachedSession` | `:143-155`、`:93-98` |
| 懒加载图片 / 全文 | `itemURL()` 手拼 `percentEncodedPath`（id 里有 `#` `:`） | `uncachedSession` | `:273-285`、`:288-298`、`:301-308` |
| 飞书文档原始接口 | `url("/api/v1/lark/raw/…")` | `self.session` | `ios/Jarvis/Views/Feishu/Docs/FeishuDocsComponents.swift:10-17` |
| 飞书媒体 | 相对路径走 `api.request`（`uncachedSession`）；绝对 http(s) URL 直接 `uncachedSession.data(from:)`、**不带 token** | `uncachedSession` | `ios/Jarvis/Views/Inbox/Lark/LarkMedia.swift:37-60` |
| 飞书视频 | `AVURLAsset(url: cfg.url.appending(path:), options: [AVURLAssetHTTPHeaderFieldsKey: Authorization])` | AVFoundation 自己的网络栈 | `LarkMedia.swift:296-302` |
| WS | `config.url.appending(path: "/api/v1/ws")`，scheme：`https` → `wss`，否则 `ws`；`Authorization` 头；握手超时 15 秒；单条最大 64 MB | `LiveConnection` 自己的 `URLSession(configuration: .default)` | `ios/Shared/LiveConnection.swift:174`、`:222-247` |

- 所有请求都带 `Authorization: Bearer`，不用 `?token=`（见 2.5）。
- 心跳（`LiveConnection.swift:178-181`、`:249-284`）：每 20 秒 `sendPing`；5 秒没 pong 记弱网（之后每 5 秒 ping）；45 秒没收到任何东西主动断；退避 1→20 秒（`:340-349`）。
  收到第一条消息（hello）才算 `connected`（`:300-306`）。`hello` 里只读 `features`（`AppModel.swift:572-573`）。
- 网络变化：`ConnectionStatus` 用 `NWPathMonitor`（`ios/Jarvis/App/ConnectionStatus.swift:43`、`:65-66`），恢复时 `reconnectNow()`、换网卡时 `probe()`（`:118-120`）。
- 进后台就断 WS（`JarvisApp.swift:27-35`）；静默推送 `jarvis_wake` 唤醒后 `ensureConnected()`，25 秒后断（`JarvisApp.swift:106-120`），hub 那边只等 20 秒（`server/internal/hub/hub.go:737`）。

### 5.3 ATS [事实]

- iPhone App：`NSAppTransportSecurity.NSAllowsArbitraryLoads = true`（`ios/project.yml:84-86`，注释「hub 通常在私有网络里走明文 HTTP」；生成后的 `ios/Jarvis/Resources/Info.plist:38-42`）。
- 手表 App：同样 `NSAllowsArbitraryLoads = true`（`ios/project.yml:144-145`、`ios/JarvisWatch/Info.plist:23-27`）。
- 小组件 / 实时活动扩展 `JarvisWidgets`：没设 ATS（`ios/project.yml:101-128`），它也不联网：实时活动按钮的 intent 在 App 进程里跑（`ios/LiveActivity/FleetIntents.swift:4-6`、`project.yml:127-128` 的注释）。
- `ITSAppUsesNonExemptEncryption: false`（`project.yml:75`）。[判断] 用系统 TLS（URLSession）做钉扎仍属豁免范围，不用改；如果自己实现加密协议才需要重新评估。

### 5.4 其他直连 hub 的地方 [事实]

| 地方 | 怎么拿配置 | 位置 |
| --- | --- | --- |
| 手表 | 手机经 WatchConnectivity 推 `ServerConfig`（JSON，`applicationContext` + `transferUserInfo`，`ios/Shared/WatchSync.swift:35-45`）；手表存进 **`UserDefaults` 的 `config`**（不是 Keychain，`ios/JarvisWatch/JarvisWatchApp.swift:34-43`），自己用 `APIClient`（`URLSession.shared`）直连 hub（`:31`、`:75-145`） | 手表拿的是 `Keychain.watchConfig ?? serverConfig`（`AppModel.swift:113-117`、`:136`） |
| Siri / 快捷指令 | `IntentSupport.client()`：`Keychain.serverConfig` → `APIClient`（`ios/Jarvis/Intents/IntentSupport.swift:133-141`）；错误文案「确认手机能访问 hub（同一个网络或 VPN）」（`:116-126`） | |
| 通知按钮 | `resolveFromNotification`：`Keychain.serverConfig` → `APIClient.resolve`（`JarvisApp.swift:159-176`）；失败文案也用 `host`（`ios/Jarvis/App/NotificationActions.swift:58`） | |
| 实时活动按钮 | `FleetActions.run`：`Keychain.serverConfig` → `APIClient.resolve`（`ios/Jarvis/App/FleetActions.swift:17-21`） | |
| 推送注册 | `PushRegistrar.didRegister` → `AppModel.shared.api` → `POST /api/v1/devices`（`JarvisApp.swift:230-241`） | |
| 设置页显示地址 | `model.config?.url.absoluteString`（`SettingsView.swift:27`） | |
| WKWebView | `LinkSheet`（外部链接）、`MailViews`（邮件 HTML），**没发现**直接加载 hub 地址的 | `ios/Jarvis/Views/Link/LinkSheet.swift`、`ios/Jarvis/Views/Mail/MailViews.swift:189-224` |

### 5.5 要支持「多候选地址 + 证书钉扎」，iOS 要改哪里 [判断]

1. **数据模型**：`ServerConfig`（`APIClient.swift:3-27`）加字段，保持 `Codable` 向后兼容（新字段可选）：
   - `addresses: [URL]`（候选，按优先级；`url` 保留为「上次成功的那个」，旧代码路径不受影响）；
   - `pin: String?`（证书 SPKI 的 sha256，base64）；
   - `hubID: String?`（4f）。
   `init?(pairURL:)` 解析新参数（多个 `url=`、`pin=`、`code=`，见 4a–4c）。Keychain 存的是整份 JSON，旧数据解码后新字段为空即可。
2. **一个统一的、带代理的 URLSession**：现在有三个会话（`URLSession.shared`、`uncachedSession`、`LiveConnection.session`），都没有 delegate，**做不了钉扎**。
   要换成同一个 delegate（实现 `urlSession(_:didReceive:completionHandler:)`，对 `NSURLAuthenticationMethodServerTrust` 取叶子证书公钥算 SPKI sha256，与 `pin` 比对，相等才 `.useCredential`）。改动点：`APIClient.session` 默认值（`:89`）、`uncachedSession`（`:93-98`）、`LiveConnection.session`（`LiveConnection.swift:174`）、上传用的任务级 delegate `UploadProgress`（`:154`，定义在 `:795-802`，只实现了进度回调，认证回调会落到会话级 delegate，不用改）。
3. **AVPlayer 没法挂 URLSession delegate**：`LarkMedia.swift:296-302` 的视频直接走 AVFoundation，自签证书会握手失败。只能先下载到本地再播（`file(_:name:)` 已有，`LarkMedia.swift:68`），或者 `AVAssetResourceLoaderDelegate` + 自定义 scheme 转发。
4. **候选地址选择**：放在一个共享的 `HubEndpoint`（新类型）里，`APIClient.url()`（`:100-105`）、`itemURL()`（`:301-308`）、`LiveConnection.open()`（`:222-247`）、`LarkMedia`（`:298`）都从它取当前地址。
   - 选法：并发探测 `GET /api/v1/health`（不鉴权，便宜；新 hub 返回 `hub_id` 用来确认是同一台），IPv6 先发、IPv4 稍后再发（RFC 8305 Happy Eyeballs 建议的尝试间隔是 250 ms，属设计参数、非实测），第一个通过钉扎和 `hub_id` 校验的胜出；
   - 时机：冷启动、`NWPathMonitor` 路径变化（`ConnectionStatus.swift:65-120`）、WS 断开重连前、静默推送唤醒时（只有约 25 秒，`JarvisApp.swift:116`，探测要并发，不能串行等 15 秒超时）；
   - 手表、Intents、通知按钮、实时活动按钮都在 App 外或后台跑，不能依赖 App 内存里的「当前地址」：要么把选中的地址写回 Keychain，要么每次快速探测。
5. **ATS**：[判断，需真机验证] 用自定义信任评估接受自签证书时，ATS 仍检查 TLS 1.2+、前向保密套件、证书密钥（RSA ≥ 2048 / ECC ≥ 256）和 SHA-256 签名。hub 生成证书时用 ECDSA P-256 + SHA-256 就能满足。
   `NSAllowsArbitraryLoads` 先保留（内网 / VPN 的明文旧 hub 还要能连）；App 里再加一道：`http://` 只接受私有 / VPN / 回环地址和 `.local`，公网地址必须 `https` + 钉扎（与 android.md 5.1 一致）。
6. **手表**：`WatchSync` 传的 `ServerConfig` 自动带上新字段；手表的 `APIClient` 也要用带钉扎的会话。手表 token 现在明文存在手表的 `UserDefaults`（`JarvisWatchApp.swift:34-43`），公网化后建议改存手表 Keychain。
7. **配对链接**：`onOpenURL` 直接配对（`JarvisApp.swift:15-25`）在公网场景风险更大（任何网页 / 消息里的 `jarvis://pair?url=<别人的 hub>` 都能把手机改连过去，之后那个 hub 能调手机工具）。建议加确认页（显示地址、指纹），已配对时不能静默替换。
8. **错误分类**：Intents（`IntentSupport.swift:116-126`）、飞书文档（`FeishuDocsComponents.swift:20-30`）、PairView 的错误要加「证书不匹配」一类，文案去掉「同一个网络或 VPN」。

## 6. Android 第一版（PR #84）

### 6.1 现状 [事实]

（表里 `data/`、`ui/`、`service/` 开头的路径都在 `android/app/src/main/java/jarvis/android/` 下。）

| 项 | 现状 | 位置 |
| --- | --- | --- |
| 配对数据 | `data class Pairing(val url: String, val token: String)` | `android/app/src/main/java/jarvis/android/data/Pairing.kt:7` |
| 存储 | `PairingStore`：`SharedPreferences("pairing", MODE_PRIVATE)`，`url`、`token` **明文**（没走 Keystore） | `Pairing.kt:9-19` |
| 备份 | `android:allowBackup="false"` | `android/app/src/main/AndroidManifest.xml:19` |
| 解析 | `PairingStore.parse()`：scheme `jarvis`、host `pair`、`url` 以 `http` 开头、`token` 非空 | `Pairing.kt:22-30` |
| 链接配对 | manifest 声明 `jarvis://pair`（`AndroidManifest.xml:36-42`）→ `MainActivity.handle` 放进 `pendingPair`（`android/app/src/main/java/jarvis/android/MainActivity.kt:49-54`、`:60`）→ `PairScreen` 自动配对（`ui/PairScreen.kt:82-85`，`LaunchedEffect(link)` 在 `:83`）。`PairScreen` 只在没配对时显示（`ui/Root.kt:16-17`），所以**已配对时链接不会静默替换** | |
| 配对流程 | `ApiClient(url, token).machines()` 校验 → `createToken(deviceName)`（404 = 旧 hub，留配对 token）→ 存 `Pairing(url.trimEnd('/'), own)` → `JarvisApp.connect` | `ui/PairScreen.kt:55-76` |
| 配对报错 | 401 →「token 不对」；其他 →「连不上 …。手机和 hub 要在同一个网络或 VPN 里」；手动输入框 placeholder `http://<hub 地址>:18660`，按钮要求 `url.startsWith("http")` | `PairScreen.kt:69-73`、`:108-120` |
| 连接对象 | `JarvisApp.connect(p)`：每次配对 / 启动建一个 `Hub(ApiClient(p.url, p.token), …)` 并起前台服务 | `android/app/src/main/java/jarvis/android/JarvisApp.kt:29-45`（`connect` 在 `:39`） |
| HTTP 客户端 | `ApiClient(val baseUrl: String, val token: String)`，一个 `OkHttpClient`：connect 10 秒、read 110 秒、`pingInterval(20 s)`，REST 和 WS 共用 | `data/ApiClient.kt:23-28` |
| URL | `url()`：`baseUrl.toHttpUrl()` 逐段 `addPathSegment`；`wsUrl()`：`replaceFirst("http", "ws")`（`https` → `wss` 也成立） | `ApiClient.kt:32-38` |
| 鉴权 | 全部 `Authorization: Bearer`（REST `:43`，图片 `:113`，全文 `:122`，WS `data/Hub.kt:103`），不用 `?token=` | |
| health | `health()` 定义了（只解 `ok`、`version`），**没有调用方** | `ApiClient.kt:67-68` |
| WS | `api.http.newWebSocket`；onOpen 就算 Online、注册手机工具、重订阅；退避 1→30 秒 | `Hub.kt:72-88`、`:102-122` |
| hello | 只取 `server_version`，不看 `features` | `Hub.kt:146` |
| JSON ping | 收到 `{"type":"ping"}` 回 `pong`（`Hub.kt:147`），但 hub 从不发 JSON ping（见 3.2） | |
| 网络变化 | 前台服务 `registerDefaultNetworkCallback`，`onAvailable` → `kick()`（`service/HubService.kt:47-50`）；`kick()` **只在不是 Online 时**才断开重连（`Hub.kt:97-100`） | |
| 通知按钮 | `ActionReceiver` 用当前 `hub.api.resolve`（`service/ActionReceiver.kt:19`、`:39`） | |
| 明文 | `network_security.xml` 只有 `<base-config cleartextTrafficPermitted="true" />`，注释写「hub 在私有网络或 VPN 里讲明文 HTTP」；没有 `trust-anchors` / `pin-set` | `android/app/src/main/res/xml/network_security.xml`；manifest `:22` 引用 |
| SDK | `compileSdk 34`、`targetSdk 34`、`minSdk 26`；OkHttp 4.12.0；没有 Coil / Ktor 等别的网络栈 | `android/app/build.gradle.kts:13-18`、`:57` |

### 6.2 要改哪里 [判断]

1. **`Pairing` / `PairingStore`**（`Pairing.kt`）：加 `addresses`、`pin`、`hubId`；`parse()` 认多 `url=` / `pin=` / `code=`。token 改用 Android Keystore 里的 AES 密钥加密后再存（android.md 已写「Keystore」）；旧数据读出来没有新字段就当单地址、无钉扎。
2. **钉扎只能在代码里做**：`network_security.xml` 的 `<pin-set>` 和 OkHttp 的 `CertificatePinner` 都是在**系统信任链校验通过之后**再比对，自签证书过不了前一步；`<trust-anchors>` 要把证书打包成资源，用户各自的证书放不进去。
   做法：给 hub 专用的 `OkHttpClient` 配自定义 `X509TrustManager`（只认叶子证书 SPKI sha256 = `pin`）+ `hostnameVerifier`（地址是 IP、会变，主机名校验交给钉扎）。只给 hub 用，别影响以后的其他请求（Gmail 登录、FCM 等）。改动点：`ApiClient.kt:24-28`。
3. **候选地址**：`ApiClient.baseUrl` 是 `val`、`Hub.api` 是 `val`（`Hub.kt:44`），换地址要么重建 `Hub`（会丢订阅状态），要么把 `baseUrl` 抽成可变的 `HubEndpoint`。
   探测用已有但没用的 `health()`（`ApiClient.kt:67-68`，加 `hub_id`、`addresses` 字段）；`kick()`（`Hub.kt:97-100`）在网络变化时也要触发重新选址，而不是只在离线时（Wi-Fi 切蜂窝时旧 socket 还「Online」，要等 OkHttp ping 失败）。
4. **明文**：`network_security.xml` 的整体放开保留（兼容内网明文 hub），App 里加判断：`http://` 只给私有 / VPN / 回环 / `.local`，公网地址强制 `https` + 钉扎（android.md 5.1 已有同样的规则）。
5. **心跳**：OkHttp 的 `pingInterval` 是客户端级的、WS 建立后改不了，要换间隔只能 `http.newBuilder().pingInterval(x)` 建新客户端再重连；OkHttp 在下一次 ping 之前没收到 pong 就判失败，所以「ping 间隔」同时也是「超时」。
   和 hub 侧的 `client_hello.heartbeat_seconds`（3.4）配合：前台 20 秒，息屏 / 后台（前台服务常驻时）可以放到 60–120 秒；**耗电和 NAT 超时都没实测**，要在真机上量。
6. **错误分类**：`SSLHandshakeException` / `SSLPeerUnverifiedException` → 「证书不匹配，重新扫码」；`PairScreen.kt:69-73` 的「同一个网络或 VPN」文案要改。
7. **本地网络权限**（升 target 37 时，android.md 5.1）：IPv6 全局地址是否算「本地网络」**未核实**；如果不算，「优先走公网 IPv6」在同一 Wi-Fi 下还能顺带绕开这个权限，值得真机验证。

## 7. 文档

### 7.1 现在写了「只放内网 / VPN」「默认 HTTP」的地方 [事实]

| 位置 | 原文要点 | 公网直连后怎么改 [判断] |
| --- | --- | --- |
| `README.md:53` | 准备表：「手机能访问它的 18660 端口（同一个网络或 VPN）」 | 改成「手机能访问 hub：同一个网络、VPN，或公网直连（IPv6 优先，需要放行 TLS 端口）」 |
| `README.md:68`、`:71` | 第 3 步「填上…手机访问用的地址」；第 5 步扫码 | 第 3 步说明 `public_url` / `advertise` / `tls`；第 5 步说明二维码是 5 分钟有效的一次性配对码，旧 App 用 `--legacy` |
| `README.md:79` | 「hub 默认是 HTTP，只该放在内网或 VPN 里访问，不要直接暴露到公网」 | 改成：TLS 端口（自签证书 + App 钉扎指纹）可以暴露到公网；明文端口只对内网 / VPN 来源开放，公网来的明文请求一律拒绝；鉴权失败限流 |
| `docs/config.md:22` | 同 README 的准备表 | 同上 |
| `docs/config.md:35`、`:88`、`config.example.json:27` | 构建配置 `hub.url` 写 `http://…:18660` | 这是给 `sim.sh` 配对模拟器用的，可以保留内网明文地址；加一句说明 |
| `docs/config.md:50`、`:118-119`、`:138` | hub.json 示例 `listen`、`public_url` 是 `http://`；「第一次运行…对外地址是 `http://<主机名>:18660`」 | 示例和字段表补 `tls`、`advertise`、`advertise_ipv6`、`advertise_lan`、`trusted_cidrs`（`## hub 运行配置` 在 `:114`） |
| `docs/server.md:43` | 「监听 `:18660`」 | 补 TLS 端口；防火墙 / 光猫放行（IPv6 入站）；TUN 代理的回包问题 |
| `docs/server.md:71`、`:77` | hub.json 字段表；配对「打印地址、token 和二维码（`jarvis://pair?url=…&token=…`）」 | 字段表补新字段；配对改成配对码格式，写 `--legacy` |
| `docs/server.md:94` | `curl -H "Authorization: Bearer $TOKEN" http://<hub 地址>:18660/…` | 补 https 示例：`curl --pinnedpubkey 'sha256//<pin>' -k https://…`（`-k` 跳过 CA 校验，钉扎仍然生效） |
| `docs/server.md`「运维」（`:90-100` 一带） | 只记失败和慢请求，不记 query/body/token | 补：限流、封禁、WS 鉴权失败日志的格式，来源打码规则 |
| `docs/protocol.md:3` | 「hub 默认监听 `:18660`」 | 补 TLS 端口 |
| `docs/protocol.md:5-21` | 鉴权：`Bearer`，「WebSocket 也可用 `?token=`」；health 返回 `{"ok":true,"version":"..."}` | 写清楚 `?token=` 的新范围（2.5）；health 加 `features`（已有但没写）、`hub_id`；新增「配对码」「证书钉扎」「候选地址」三小节；限流的 `429` |
| `docs/protocol.md:154-190` | WS 表：`hello` 只写了 `server_version, client_id`；有 `ping` / `pong` 两行（代码里没有） | `hello` 补 `features`、`hub_id`、`endpoints`、`tls_pin`；新增 `endpoints` 事件、`client_hello`；删掉或实现 `ping` / `pong`（hub-changes.md 3.8、4.12） |
| `docs/architecture.md:17-18`、`docs/architecture.svg:3` | 图上写「HTTPS/WS (Bearer token)」「hub 机器 :18660」 | **现在就不准确**（实际是明文 HTTP）。改成「HTTPS（自签 + 钉扎，公网）/ HTTP（内网、VPN）」 |
| `docs/android.md:13` | 配对：`jarvis://pair?url=…&token=…` | 新格式 |
| `docs/android.md:47` | 「hub 默认是 HTTP（内网 / VPN），`network_security.xml` 放开了明文流量；不要把 hub 暴露到公网」 | 改成明文只给内网 / VPN、公网走 TLS + 钉扎；说明为什么钉扎只能在代码里做（6.2） |
| `docs/android.md:84-85` | USB `adb reverse` 联调用 `http://127.0.0.1:18660` | 回环明文继续允许，保留 |
| `docs/intents.md:51`、`:56` | 「手机要在公司网络或 removed 上」「hub 是公司内网的明文 HTTP」 | 改成通用说法；`.background` 模式的理由（网络不可达不拉起 App）仍然成立 |
| `docs/ios.md:306`、`:318-320`、`:343` | 配对信息存 Keychain；手表配对；`jarvis://pair` 链接 | 补：候选地址 / 指纹 / hub_id 也在配对信息里；手表 token 改存 Keychain；链接配对要确认 |
| `docs/testing.md:50`、`scripts/mock/mock_hub.py:6`、各 UI 测试 | `-pairURL "jarvis://pair?url=http%3A%2F%2F127.0.0.1…&token=mock"` | 新规则必须继续允许「回环 + 明文 + token」，否则 UI 测试全挂 |

### 7.2 代码里的同类文案 / 注释 [事实]

- `ios/project.yml:84`（注释「hub 通常在私有网络里走明文 HTTP」）、`android/app/src/main/res/xml/network_security.xml:2`（同义注释）。
- 「同一个网络或 VPN」：`ios/Jarvis/Intents/IntentSupport.swift:123`、`:127`；`ios/Jarvis/Views/Settings/SettingsView.swift:424`；`android/app/src/main/java/jarvis/android/ui/PairScreen.kt:72`。
- 「确认手机能访问 hub」：`ios/Jarvis/Views/Feishu/Docs/FeishuDocsComponents.swift:28`。
- Android 手动输入 placeholder `http://<hub 地址>:18660`：`PairScreen.kt:110`。

### 7.3 新增 [判断]

- 建议新文档 `docs/network.md`（公网直连）：TLS 端口和证书文件、指纹怎么看 / 怎么换、IPv6 放行（hub 防火墙、光猫 / 路由器）、DDNS 兜底、TUN 代理的回包问题、反向代理的来源地址问题、限流参数。写进 CLAUDE.md 文档索引。
- CLAUDE.md「约定」记一条用户要求（2026-10-30：手机经公网直连 hub 要能通，优先探测和使用 IPv6；VPN 用户自己配），按「随时沉淀经验」的规则。
- `docs/redesign/ux.md` 3.7 第 3 点「明文 HTTP 地址：提示『HTTP 不加密，只在内网或 VPN 里用』」和 `docs/redesign/android.md` 5.1「明文 HTTP」与本方案一致；ux.md 3.7 第 5 点、hub-changes.md 4.1 的「一次性配对码」从「可选」升为公网直连的前提。

## 8. 兼容旧客户端要注意什么

1. **旧 App 连不上自签 TLS** [事实 + 判断]：iOS 的三个 URLSession 都没有 delegate（`ios/Shared/APIClient.swift:89`、`:93-98`，`ios/Shared/LiveConnection.swift:174`），Android 用系统 CA（`network_security.xml` 没有 trust-anchors）。所以明文 listener 必须留着给内网 / VPN 的旧设备，`public_url` 的语义（单个、旧 App 能用的地址）也别改。
2. **二维码格式**：旧 iOS 的 `ServerConfig(pairURL:)` 和旧 Android 的 `PairingStore.parse()` 都**要求有 `token=`**（`APIClient.swift:12`、`Pairing.kt:28`）。只带 `code=` 的新二维码，旧 iOS 扫了没反应，旧 Android 提示「不是 Persona 的配对二维码」。所以 `hub pair --legacy` 要保留老格式；新 App 也要继续认老格式（`mock_hub.py`、`sim.sh`、所有 UI 测试都用 `http://127.0.0.1…&token=mock`）。
3. **多个 `url=`**：旧客户端取第一个（iOS `queryItems.first`，`APIClient.swift:11`；Android `getQueryParameter` 也是第一个）。如果想让旧 App 也能扫新码，第一个 `url=` 必须是它能用的明文内网地址，而且还得带 `token=`（主 token），这就和「二维码里不放主 token」冲突。结论：新旧二维码分开。
4. **已配对的新版 App 怎么升级到 TLS**：hello 里下发 `tls_pin` + `endpoints`，App 在现有的已鉴权通道上记下指纹。这是在内网明文 / VPN 上「首次信任」（同一局域网里的攻击者能塞假指纹）；更严格的做法是要求重新扫码。**要拍板**。
5. **WS 新字段 / 新事件**：旧 iOS 对未知 `type` 返回 nil 丢掉（`LiveConnection.swift:130`），旧 Android 的 `when` 没有 else（`data/Hub.kt:145-211`），都能忽略。但 iOS 是先把整条消息解成 `Envelope`（`LiveConnection.swift:42-85`）：**新字段名不能和里面已有的键撞名且类型不同**（`error`、`id`、`state`、`data`、`name`、`status`、`text` 等都是字符串），否则整条消息解码失败、被丢掉（比如 hello 里加一个对象型的 `error` 就会把 hello 弄丢）。
6. **`client_hello`**：旧 hub 的 `onClientMessage` 对未知类型什么都不做（`http.go:672-730` 的 switch 没有 default），新客户端要靠 `features` 里有没有 `client_heartbeat` 判断协商是否生效，不能等回执。
7. **手表**：手机经 WatchConnectivity 推整份 `ServerConfig` JSON（`ios/Shared/WatchSync.swift:35-45`），旧手表 App 解码时忽略新字段、继续用 `url`。如果新手机把 `url` 换成了自签 TLS 地址，旧手表就连不上。手表和 iPhone App 一般随同一个 TestFlight 构建一起更新，但会有时间差：`url` 最好保持「旧代码也能用的地址」，新地址放新字段。
8. **限流误伤** [判断]：两端的 WS 在 401 时都会无限重连（iOS 退避到 20 秒封顶，`LiveConnection.swift:340-349`；Android 到 30 秒，`data/Hub.kt:76-86`），没有「401 就停」的逻辑（只有 Intents 把 401/403 说成「配对已失效」，`ios/Jarvis/Intents/IntentSupport.swift:112-113`）。按这两个退避上限推算，一台被撤销的设备每小时会失败上百次（**未实测**，仅由 20 秒 / 30 秒的退避上限推断）。
   如果限流按 IPv4 /32 计，同一个家庭 NAT 后面的其他设备会被一起封。建议：内网 / VPN 来源不计；公网按「来源前缀 + token 哈希」计；客户端收到 401 停止自动重连、提示重新配对（新版本两端都要改）。
9. **`?token=`**：当前两端都不用（2.5），收紧安全。更早的 App 构建有没有用过**没查**（只看了当前 master）。
10. **调试链路**：`scripts/sim.sh:16-19` 从 hub.json 读主 token、用配置里的 `hub.url` 配对模拟器；`docs/android.md:84-85` 的 `adb reverse` 用 `http://127.0.0.1:18660`。新客户端规则要允许「回环 + 明文 + token」，hub 侧回环来源也必须算本地。
11. **推送、node（SSH）**：不受影响。APNs 不经过 hub 的监听端口；node 走 SSH 标准输入输出。

## 9. 没查清 / 待确认

- **未实测**（按用户规则，下面都不能给数字）：hub 机器上 `:18660` 是否真的双栈（`ss -ltnp`）；手机蜂窝网络能否经 IPv6 直达 hub；hub 机器 TUN 代理对入站连接回包的影响；光猫 / 路由器的入站 IPv6 策略；运营商 / NAT 的 TCP 空闲超时；不同心跳间隔的耗电。
- **待真机验证**：iOS 在 ATS 开启（或保留 `NSAllowsArbitraryLoads`）时，自定义信任评估接受自签证书的具体限制；watchOS 能否直连公网 IPv6 的 hub（手表经手机代理 / Wi-Fi / 蜂窝时行为不同）；Android 本地网络权限对「同一局域网内的全局 IPv6 地址」算不算本地。
- **没查**：更早的 App 构建是否用过 `?token=`；`mail` 子路由、lark-connector 代理、Gmail OAuth 回调在公网暴露后有没有额外风险（它们都在 `h.auth` 之后，OAuth state 已加密自包含，`docs/mail.md:52`，但没逐个审）；hub ↔ node（SSH）不在本次范围。
- **设计上要拍板**：两个端口还是同端口嗅探（4a）；已配对设备是否允许在内网通道上「首次信任」指纹（8.4）；`/api/v1/health` 要不要继续公开版本号和 features（3.1）；开公网后主 token 是否限制为只接受回环 / 内网来源（4c）。
- **既有资料的行号已漂移**：`docs/redesign/research/jarvis-protocol.md:26` 引用的 `config.go:144-147`，现在是 `config.go:148-151`。
