# hub 改动清单

> 草案 v1（2026-10-30），属于 [重设计方案](README.md)。依据见 [research/jarvis-protocol.md](research/jarvis-protocol.md)。
> 原则：所有改动**对旧 App 兼容**（新字段可选，旧字段语义不变）；新 App 遇到旧 hub 时按能力位退回旧行为。
> **这一轮 iOS 只读不改**：每一项改动都要让现有的 iOS App 不改代码也照常工作，包括内网明文连接、旧格式的配对二维码、单个地址。每次 hub 改动都用现有 iOS App 跑一遍 UI 测试和真机配对。
> 迁移号按车道分段（CLAUDE.md），挑号前先看所有 worktree 的 `server/internal/db/migrations/` 和 `jarvis_test` 库的 `schema_migrations`。

## 1. 公网直连（必须）

> 2026-10-30 定：手机经公网直连 hub 要能通，优先探测和使用 IPv6；VPN 用户自己配。依据见 [research/public-direct-ipv6.md](research/public-direct-ipv6.md)（国内 IPv6 现状、地址发现、TLS、加固、保活，带来源）和 [research/hub-network-current.md](research/hub-network-current.md)（hub 现有网络代码，带行号）。
> 原稿的「FCM」一节已删除：按「手机没有 Google 服务」设计，不做 FCM（[README.md](README.md) D8）。

**现状**：
- hub 只有一个明文监听（`listen` 默认 `:18660`），没设任何超时；
- 鉴权失败不限流、不记来源；查不到的 token 也进缓存，而且不清理；
- 二维码里是**永久有效的主 token**，只有一个地址（`public_url`）；
- 文档要求「只放在内网或 VPN 里」。

Go 对通配地址开的是双栈 socket，IPv6 本来就能连进来；能不能从公网连进来，取决于光猫、路由器和 hub 机器的防火墙。

**兼容现有 iOS 的总原则**：
- 明文端口原样保留，只对**公网来源**拒绝明文；
- 二维码格式不变（`url=` + `token=`），新信息放在附加参数里；
- 不发心跳协商的连接保持现在的 20 秒；
- 候选地址不放进免鉴权的接口。

### 1.1 TLS 监听

- **端口**：新开一个 TLS 端口，可配。避开 80 / 443 / 8080 / 8443（部分地区家宽封这些端口），也避开 18661（Postgres 占用）。明文 `listen` 保留，给内网、VPN 和现有 iOS。
  - 已定用两个端口（README 第 12 节第 9 题）。同端口嗅探（第一个字节是 `0x16` 就走 TLS）留作以后的选项：用户只需在路由器上放行一个端口，代价是约 100 行自己写的接收循环。
- **证书**：
  - hub 的身份密钥是 ECDSA P-256，0600，放 `~/.persona/tls/`，不进数据库（CLAUDE.md「密钥绝不入库」）。
  - 用它签自签证书；证书快到期时用**同一把密钥**重签，所以指纹不变。
  - 指纹是 SPKI 的 SHA-256，打印进 `personad hub pair`，也放进二维码。
  - 换密钥时所有设备重新扫码（已定）；计划内的轮换可以在 `hello` 里提前预告下一把密钥的指纹（`fp_next`）。
- **有域名时（可选）**：用 ACME DNS-01，以同一把密钥换公共 CA 签的通配证书。通配证书避免具体的名字出现在 CT 日志里被扫描。客户端照样钉公钥。
- **不做 IP 地址证书**：Let's Encrypt 的 IP 证书 2026-01 才正式提供，有效期只有 6 天，而且只能用 HTTP-01 / TLS-ALPN-01 验证，要开 80 / 443。
- **服务器参数**：两个 server 都补上 `ReadHeaderTimeout`、`IdleTimeout`、收小 `MaxHeaderBytes`。不设整体的 `ReadTimeout` / `WriteTimeout`，那会掐断 WS 和大文件上传。
- **改动面**：新文件 `tlscert.go`；`hub.go` 的 `Run()` 起第二个 listener；`hub.json` 加 `tls: {listen, cert_file?, key_file?}`。约 150–250 行（估计）。

### 1.2 地址发现与下发

- **发现**（新文件 `endpoints.go`）：
  - 用 netlink 读带 `IFA_FLAGS` 的地址。`/proc/net/if_inet6` 看不到 0x100 以上的标志。
  - 排除临时（隐私）地址、已废弃地址、ULA、链路本地、文档前缀，以及 docker、TUN 之类的虚拟网卡。
  - 订阅地址变化，再加每几分钟一次全量重扫。
- **候选类型**：

  | 类型 | 内容 | 什么时候发布 |
  | --- | --- | --- |
  | `ipv6` | 全局、稳定的 IPv6 | 探测到就发布 |
  | `name` | DDNS 名字或配置的 `public_url` / `advertise` | 配了就发布 |
  | `lan` | 内网 IPv4 / ULA | 探测到就发布 |
  | `ipv4` | 公网 IPv4 | 只在确认外部连得进来时：UPnP / NAT-PMP / PCP 查 WAN 口，加 STUN 或 HTTPS 回显，判断有没有运营商 NAT |

  每项带端口、协议（`https` / `http`）、`seen_at`。
- **可达性 hub 自己测不出来**：本机连自己的地址走的是环回，不经过光猫。要靠手机在蜂窝网络上探测，或者一台不在家的 node 帮忙测。
- **下发**：
  - 鉴权后的 `GET /api/v1/endpoints`；
  - WS `hello` 的新字段（`hub_id`、`endpoints`、`tls_port`、`fp`）。现有 iOS 先把每条 WS 消息整体解成一个 `Envelope`（`ios/Shared/LiveConnection.swift`），新字段名如果和它已有的键撞名而类型不同（`id`、`state`、`data`、`name`、`error`、`text` 等都是字符串），整条 `hello` 会解码失败、被丢掉。上面四个都不撞（已核对），以后加字段也要先查这张表；
  - 地址变化时推 `{"type":"endpoints"}`，只发给在 `client_hello` 里声明了 `endpoints` 能力的连接；
  - `features` 加 `endpoints`、`tls`、`heartbeat`、`app_ping`。
- **免鉴权的 `/api/v1/health`** 只加不敏感的字段（`hub_id`、TLS 端口、`fp`），**候选地址不放这里**，免得任何扫到端口的人拿到家里的其他地址。它现在还公开版本号和能力位：对公网来源只回 `ok`、`hub_id`、TLS 端口和指纹，内网来源照旧，现有 iOS 在内网读它（已定，[README.md](README.md) 第 12 节第 9 题）。
- **TUN 代理的坑**（未实测）：hub 机器开了 TUN 代理的自动路由时，从外面连进来的 IPv6 连接，回包可能被路由进代理，连接建不起来。部署前要用蜂窝网络的手机实测，`hub doctor`（1.8）也要检查。

### 1.3 配对：二维码改发短时临时 token（格式不变）

- `personad hub pair` 默认不再打印主 token。它经本机回环调运行中的 hub，生成一个**高熵、短时有效（比如 10 分钟）、只能换一次**的临时 token，落库（hub 的内存状态会被部署清掉，所以必须落库），二维码格式不变：

  `jarvis://pair?url=<内网明文地址>&token=<临时 token>&hub=<hub_id>&fp=<指纹>&tls=<TLS 端口>&alt=<一两个外部候选>`

- **参数格式**（各端照这个实现）：
  - `hub`：hub 实例 id（1.5）；
  - `fp`：证书公钥的钉扎值，SHA-256 作用于证书的 SubjectPublicKeyInfo（DER），再用 base64url（RFC 4648 §5）编码、不带填充，43 个字符；
  - `tls`：TLS 端口（十进制）；
  - `alt`：完整的 `https://` 地址，可以出现一到两次，好的在前；
  - `url`、`token` 的含义不变。
- **为什么这样兼容现有 iOS**：iOS 的 `ServerConfig(pairURL:)` 只按名字取 `url` 和 `token`，忽略其他参数。它配对时先 `GET /machines`，再连 WS、注册推送，最后 `POST /auth/tokens` 换自己的设备 token。所以临时 token 在有效期内要当完整的 Bearer 用；换过设备 token 后，再宽限一小会儿才作废。
- 新 App（Android）读附加参数：钉住 `fp`，记下 `hub_id`，把 `alt` 加进候选。二维码里外部候选只放一两个，免得二维码太密，其余配对后从 `/endpoints` 取。
- 已经配对过、手里没有指纹的设备（PR #84 版本的 Android）：在内网上经已鉴权的通道（`hello` 或 `/endpoints`）取一次指纹并钉住，和现在内网明文的信任程度相同（已定，README 第 12 节第 9 题）。
- `--legacy` 照旧打印主 token，留给 CLI 和特殊情况。
- **公网直连开启后，主 token 只接受回环和内网来源**。
- 任何 `full` 设备 token 现在都能再发新 token：配对时给用户推一条「有新设备配对」的通知，方便发现异常。

### 1.4 加固

- **公网来源拒绝明文**：
  - 明文监听上来自公网地址的请求一律回 `403 {"code":"tls_required"}`，只放行 `/api/v1/health` 以便客户端提示「请用 https」。
  - 「公网」的判断要看连接从哪块本机网卡进来，不能只看地址：100.64/10 既是 Tailscale 的网段，也是运营商 NAT 的网段。可以配 `trusted_cidrs` 加白名单。
  - 已经在经公网明文用 hub 的人升级后会连不上，留一个 `allow_public_plaintext` 开关（默认关），`hub doctor` 和日志里给出提示。
  - 注意：token 是随请求明文发出去的，hub 拒绝时它已经暴露了。真正的保护在客户端：**公网地址上绝不用 `http://` 发 token**。
- **鉴权失败限流**：
  - IPv4 按单个地址、**IPv6 按 /64 前缀**聚合计数，按单个 IPv6 地址限等于没限；
  - 限流放在查库之前；超限回 `429` + `Retry-After`；
  - 内网和 VPN 来源不计数，免得同一个家庭网络里的设备互相连累；
  - 查不到的 token 的缓存加上限；
  - WS 的鉴权失败也要记日志（现在完全不记），来源打码，日志本身也限速。
- **主机防火墙兜底**：光猫多数只能整体开关 IPv6 防火墙，一关就把整个家庭网络都暴露了；Docker 发布的端口还会绕过 ufw。文档写清楚 hub 机器上只放行 TLS 端口。
- IPv6 接口标识用随机值，别用 `::1` 这类好猜的后缀（文档里写给用户）。

### 1.5 hub 实例 id

`Run()` 打开数据库后读 `settings` 表的 `hub_id`，没有就生成 UUID 写回，不用新迁移。在 `health`、`hello`、配对二维码里下发。客户端用它确认连上的是同一台 hub（旧前缀可能已经分给了别人），并用它给本地数据分作用域（[README.md](README.md) D6）。原 3.5 节并入这里。

### 1.6 心跳按连接协商

- **为什么必须由 hub 发起**：Android 深睡时，OkHttp 的客户端 ping 不会触发（它的计时用 `System.nanoTime`，深睡时停走）；后台能用的闹钟（`AllowWhileIdle`）最快 9 分钟一次。后台常驻连接的心跳只能由 hub 发，按连接协商间隔。
- **协议**：
  - 客户端连上后在 `client_hello`（3.3）里带 `heartbeat: {interval, pong_timeout}`；
  - hub 调整这条连接的 WS ping 间隔、pong 超时和 TCP keepalive，并回一条 `client_hello_ack`，写明实际生效的值。hub 的 `hello` 是一连上就发的，早于 `client_hello`，所以生效值要另回一条。
- **取值**：
  - 后台从 **4 分钟**起，低于已知的约 5 分钟运营商 NAT 超时（这个数据来自约 2015 年的 2G/3G 测量，4G/5G 的值**没查到**）；按网络自动调整，抖动只往下取。
  - 前台 20 秒，加上应用层 ping（3.8）判断弱网。
  - 不发协商的旧连接（现有 iOS）保持 20 秒 / 15 秒不变。
- **改动面**：集中在 `http.go`（`client` 结构、写协程的 ticker、`onClientMessage`），约 50 行（估计）。耗电和运营商的空闲超时都要实测（[android.md](android.md) 第 10 节）。

### 1.7 DDNS（可选，推荐）

- **为什么推荐**：拨号重连时 IPv4 和 IPv6 前缀一起变，旧连接当场断开。没有 FCM 的 Android 这时收不到新地址；有的地区一天换两次前缀。所以「没有域名」只能算降级模式。
- **支持的接口**：Cloudflare、阿里云 DNS、DNSPod，或者一个通用的回调 URL。
- **发布规则**：只发布 1.2 规则选出的稳定地址；A 记录只在确认外部可达时发布；DNS 令牌和 ACME 共用，权限收窄到一个 zone。
- **手机怎么用**：国内两家 DNS 免费版的 TTL 最低 600 秒，所以手机优先用 hub 下发的字面地址，DNS 名字只用来找回 hub。

### 1.8 `personad hub doctor`

一条自检命令，列出：
- 监听的端口；
- 主机防火墙是否放行；
- TUN 代理的风险；
- 当前发布的候选地址；
- 证书指纹和到期时间。

它还请一个外部点（不在家的 node，或手机）测一遍可达性，给出可以照做的结论，例如「IPv6 入站被拦，去光猫或路由器放行 TLS 端口」。

### 1.9 文档

- 新增 `docs/network.md`，写公网直连：TLS 端口和证书、指纹怎么看和怎么换、光猫和路由器怎么放行 IPv6、DDNS、TUN 代理的回包问题、反向代理会把来源地址变成本机、限流参数。
- 改掉 README、`docs/config.md`、`docs/server.md`、`docs/protocol.md`、`docs/architecture.md` 里「只放内网 / VPN」「默认 HTTP」的写法。`docs/architecture.md` 图上写的「HTTPS」现在就不准确。逐处的位置见 research/hub-network-current.md 第 7 节。
- 在 CLAUDE.md「约定」里记下这条用户要求。

## 2. 必须（Android 首版）

### 2.1 手机工具按设备注册和路由

- 现在 `register_phone` 整表覆盖 `h.phoneTools` 和 `phone_tools` 表（`hub.go`），调用发给「真机优先、最近注册」的那台（`http.go` 的 `phoneClient`）。两台手机都在线时互相覆盖，调用落在哪台不确定。而且 Android 没有 `phone_reminders_*`，调用落到 Android 上就是「未知工具」。
- 改法：
  - `register_phone` 带 `device_id` 和 `platform`；
  - 工具表按设备存（新迁移：`phone_tools` 加 `device_id`）；
  - `mcpTools()` 合并去重（同名工具取能力最多的那份描述）；
  - `callPhone` 选设备的顺序：有这个工具 → 前台在线 → 最近活跃 → 唤醒有这个工具的设备。
- 撤销设备凭据时，顺带删掉它的推送注册和工具注册（可选项 4.5 的一部分）。

### 2.2 Android 安装包托管和更新检查

没有 Play 商店，Android 的安装包由 hub 托管（[android.md](android.md) 第 9 节）。

- **存放**：`~/.persona/apps/android/` 下放 APK 和一份 `latest.json`（`version_code`、`version_name`、`sha256`、`size`、`min_sdk`、`notes`）。发布脚本签好 APK 后经 SSH 传上去，不进数据库。
- **接口**（都要鉴权，走 TLS 端口或内网）：
  - `GET /api/v1/app/android/latest`：回 `latest.json`；
  - `GET /api/v1/app/android/apk`：下载，支持 Range（断点续传）。
- **首次安装**：手机上还没有 App 时没有设备 token。`personad hub pair --android` 在二维码旁边再打印一个内网下载链接，带同一个临时 token（10 分钟），用手机浏览器打开下载；装好后再扫配对二维码。也可以用 adb 或别的方式把 APK 传到手机上。
- 能力位：`features` 加 `android_update`。

## 3. 建议（可靠性和兼容，和 Android 一起做）

### 3.1 操作回执落库（`client_ops`）

- 新迁移，`client_ops` 表：

  | 字段 | 说明 |
  | --- | --- |
  | `id` | 客户端生成的 UUID |
  | `device_id` | 发起的设备 |
  | `kind` | `message` / `resolve` / `interrupt` / `rename` / `close` / `modes` / `stop_loop` / `clear` / `lark_send` / `inbox_reply` / `mail_send` |
  | `target` | 目标的 JSON |
  | `digest` | `sha256(kind + 规范化后的参数)` |
  | `state` | `pending` / `done` / `failed` / `unknown` |
  | `result` | JSON |
  | `created_at`、`updated_at` | |

- 语义（照 removed 主机端的 `once()`，但落在 Postgres）：
  - 同 id、同摘要、已完成 → 原样返回结果或原错误，**不再执行**；
  - 同 id、同摘要、还在 `pending` → 等它完成（有上限），超时返回 `202 {state:"pending"}`；
  - 同 id、不同摘要 → `409 {code:"op_id_reused"}`；
  - 只有「可以证明没执行」的确定错误（`input_draft`、`dialog_open`、`invalid`、`stale_session`…）记为已完成的失败，**其他异常保持 `unknown`**。
- 新接口 `GET /api/v1/ops/{id}`：只读查询，客户端在结果未知时用。
- 消息的 `client_msg_id` 就是 op id。node 的内存去重（`server/internal/node/sendsafe.go`，10 分钟）继续保留，挡住 hub 和 node 之间的重试；hub 这层挡住部署、重启和超过 10 分钟的重试。
- `delivered: unknown`（node 打字到一半出错）：hub 记成 `unknown`，并在对话记录里找同一条消息（文字 + 时间窗）做**正向证明**，找到就改成 `done`。找不到的交给客户端「待核对」（[ux.md](ux.md) 5.2）。
- 清理：保留 30 天。
- 能力位：`features` 加 `client_ops`。

### 3.2 飞书发送幂等和页完整性

- `lark.Send`（`server/internal/lark/lark.go`）现在每次随机生成 clientId。改成接收调用方给的 clientId：
  - 会话页发送：手机生成；
  - 收件箱回复、AI 分身发送、助手工具 `lark_send`：hub 按件或按工具调用生成并落库。
- 飞书网关本来就按 clientId 幂等（已发出的直接返回原消息 id）。
- 消息投影（`lark.go`）透传 `clientId` 和 `localState`，App 用它们精确地消掉自己的出站气泡。
- 分页结果透传上游的 `syncState`（`complete` / `local` / `gap`），不再丢掉。App 只在 `complete` 时删本地的行。

### 3.3 客户端上报自己的能力（双向协商）

- WS：客户端连上后发 `{"type":"client_hello","device_id","platform","version","build","os":{"name","version","sdk"},"features":[…],"heartbeat":{…}}`（心跳协商见 1.6）。`os` 是识别出的系统（澎湃 OS、ColorOS、鸿蒙 4…）和版本，hub 汇总后就知道哪家哪版的设置页跳转失效了（[android.md](android.md) 5.18）。
- HTTP：请求头带 `X-Jarvis-Client: <platform>/<version>`。
- hub 记在连接上，用于：
  - 推送和手机工具按平台路由；
  - 不认识新交互类型（比如 `elicitation`）的客户端收到降级的屏幕卡；
  - 日志排查。
- 旧客户端不发 `client_hello`，按 iOS 的旧版本处理。

### 3.4 统一错误体

- `{"error":"给人看的话","code":"机器码","retryable":true|false,"delivered":"none|unknown|yes"}`，保留现有的 `{error}` 和 409 平铺字段（`http.go`）。
- 主 API 的 404 / 405 也回 JSON。现在是 Go 默认的纯文本，只有邮件子路由改写成了 JSON。
- 客户端只认 `code`。

### 3.5 hub 实例 id

已并入 1.5。

### 3.6 Fleet（锁屏汇总）的 Android 分支

**不用改**：Android 只走常驻连接，hub 已经经 WS 广播 `fleet` 事件（`liveactivity.go`），前台服务直接用它更新那条常驻通知。原稿里「按设备类型发 FCM `type=fleet`」不做。

### 3.7 交互的来源信息和新类型

- 子 Agent 来源：hook 输入里有 `agent_id` / `agent_type` 时透传到 Interaction。
- MCP 来源：PermissionRequest 针对 `mcp__*` 工具时，带上服务器名和配置来源。拿不到就不带，客户端显示「未提供」。
- 新交互类型 `elicitation`（MCP 表单 / 链接）：
  - node 接 Claude Code 的 Elicitation hook；
  - Resolution 增加 `action: accept|decline|cancel` 和 `content`；
  - 只发给声明了 `elicitation` 能力的客户端，其他客户端退回成「去终端处理」。
- 计划审批：保持现有的 `mode`。App 侧统一审批策略表，hub 不需要改。

### 3.8 应用层 ping / pong（Android 判断弱网要用）

- OkHttp 的 WebSocket 心跳全自动：没有手动 ping 的 API，也没有 pong 回调，拿不到往返时间。iOS 靠「5 秒内回没回 pong」判断弱网，Android 做不到。
- 加一条客户端发起的 JSON 心跳：App 发 `{"type":"ping","id":"<n>"}`，hub 原样回 `{"type":"pong","id":"<n>"}`。`id` 用字符串：iOS 的 `Envelope` 里 `id` 是字符串（1.2）。两端都按 id 计时，弱网判断的规则和 iOS 一致（[ux.md](ux.md) 5.5）。协议层的 ping 继续保留，用来发现死连接。
- 能力位：`features` 加 `app_ping`。旧 hub 没有这个能力时，Android 只能在连接断了以后才知道。

### 3.9 通知事件经 WS 下发给常驻连接的设备

- PR #84 现在由手机自己判断「做完了」：看到「工作中 → 完成未看」且 App 不在前台就弹。这和 hub 给 iPhone 的推送策略不一致。hub 的策略（`notify.go`）是：
  - 等 `done_delay_seconds`；
  - 只推长任务；
  - 后台任务结束才推；
  - 在电脑上或 App 里看过就不推；
  - 屏蔽的不推；
  - 助手刚 `notify_user` 过就不推。
- 改法：hub 在决定给 iPhone 发 APNs 的同一个地方，给声明了 `ws_notify` 能力、而且连着的设备发
  `{"type":"notify","kind":"done|interaction|assistant","machine_id","session_id","interaction_id","title","subtitle","body"}`。设备的通知偏好（`prefs.input`、`prefs.done`）照样生效。手机只负责显示，不再自己算。
- 交互的通知仍然用现有的 `interaction` / `interaction_resolved` 事件（Android 收到就发通知、处理后撤回），不用改。
- 和 APNs 共用同一套「发什么、发给谁」的判断，只是送达的通道不同。

### 3.10 设备连接记录（Android 的后台自检要用）

- 国产系统没有「查自启动、查后台白名单」的接口，设没设对只能看结果：常驻连接断了多久。hub 本来就知道每条 WS 什么时候连上、什么时候断，把它记下来。
- 新迁移，`device_sessions` 表：`device_id`、`connected_at`、`disconnected_at`、断开原因（pong 超时 / 对端关闭 / 被新连接顶掉 / hub 重启）、来源前缀（打码）、协商的心跳间隔。保留 14 天。
- hub 会被别的 agent 随时重新部署（CLAUDE.md），所以要落库；hub 重启造成的断开单独标出来，不算在手机头上。
- 新接口 `GET /api/v1/devices/{id}/sessions?since=`：诊断页显示「过去 24 小时后台断开 N 次，最长 X 分钟」；A1 的过夜存活实测也用它（[android.md](android.md) 5.9、第 8 节）。
- 只记时间和原因，不记内容。

## 4. 可选

| # | 改动 | 涉及 |
| --- | --- | --- |
| 4.1 | ~~一次性配对码~~：改成 1.3 的「短时临时 token，二维码格式不变」，兼容现有 iOS | — |
| 4.2 | **文案平台中立**：工具失败提示、助手 CLAUDE.md 里的说明、实时快照、`ask_user` 工具描述里写死的「iPhone」，改成「手机」或按在线手机的平台说 | `hub.go`、`assistant.go`、`assistant_snapshot.go`、`assistant_tools.go` |
| 4.3 | **交互处理后撤回通知**：`Pusher.clear` 现在是空函数。Android 推一条 `NORMAL` 的 `type=interaction_resolved`，App 取消对应通知 | `push.go`、`hub.go` |
| 4.4 | ~~推送直接带工具调用~~：不做。Android 常驻连接不需要唤醒；iOS 以后跟进时再看 | — |
| 4.5 | **设备凭据和推送设备关联**：`device_tokens` 记平台和推送 `device_id`，撤销时一起清理 | `devicetokens.go`、`db.go`、迁移 |
| 4.6 | ~~Gmail 的 Android 登录~~：不做。Google 的登录接口依赖 Google Play 服务；邮箱账号存在 hub 上、两端共用，在 iPhone 上加，或用应用专用密码走普通 IMAP | — |
| 4.7 | ~~UnifiedPush~~：不做。Android 只走常驻连接 | — |
| 4.8 | **目标实例指纹**：针对某个 Agent 的写操作带「机器 + pane + 进程启动标识 + session id」，node 执行前核对，不一致就返回 `stale_session` | `node/*`、`proto` |
| 4.9 | **屏幕卡用完整命令**：herdr 报 blocked、对话记录里正好有一个没完成的 tool_use 时，把完整的 tool_input 附到屏幕卡上，免得显示被截断的屏幕文字 | `server/internal/screen`、`node` |
| 4.10 | **提示词模板下沉**：「交给 Persona」「飞书总结」「停止循环」这几段提示词现在写在客户端（`KnowledgeView`、`LarkChatView`、`AppModel`），挪到 hub，两端就不用各维护一份 | 新接口或助手配置 |
| 4.11 | **会话全文搜索**：`GET /api/v1/sessions/{m}/{s}/search?q=`，node 在对话记录里搜，返回 seq 和摘要；客户端连续向前加载到那里 | `node`、`transcript` |
| 4.12 | **文档对齐**：`docs/protocol.md` 补上漏写的接口（`queue`、`info`、`seen`、`file`、知识库、`lark/raw` 等）和事件（`assistant_schedule`、`hook_event`、`error`、客户端发的 `seen`），删掉代码里没有的 `session` / `ping` / `pong`；核对 `docs/architecture.md` 里「没有 App 在线时 hook 立即放行」（代码里没找到这段逻辑） | 文档 |
| 4.13 | **协议样本导出**：Go 测试把典型的线上 JSON（会话、items 各种类型、交互四种、Fleet、收件箱、飞书消息）写到 `spec/fixtures/wire/`（`-update` 刷新），两端的解码测试读同一批 | `server/internal/proto` 测试 |
| 4.14 | **mock hub 对齐**：在 Go 测试里校验 `scripts/mock/mock_hub.py` 的响应格式；mock 增加「已执行但丢掉回执」「地址变了」「证书不符」的故障开关，以及 TLS 监听 | `scripts/mock/mock_hub.py`、Go 测试 |
| 4.15 | **崩溃日志上传**：不接 Firebase。App 把 `ApplicationExitInfo` 和未捕获的异常存在本地，下次启动时 `POST /api/v1/devices/{id}/crashes`，hub 落库、设置里能看；以后 iOS 跟进时共用 | 新接口、迁移 |

## 5. 不需要改的

会话、items、交互、终端、文件、收件箱、飞书、邮件（Gmail 登录除外）、知识库、监控、pins / mutes、每台设备的通知设置：都是平台无关的 JSON，Android 直接用。`prefs.island` 的结构原样复用为「状态栏胶囊显示什么」。

## 6. 顺序

1. 第 1 节（公网直连）和 3.3、3.4、3.8 放在规格阶段（P0）做：Android 重组和公网直连都要用。每一步都用现有 iOS App 验证不受影响。
2. 2.1、2.2 和 3.9 和 Android 首版（A2）一起做：2.1 避免 iPhone 的手机工具被覆盖，2.2 让测试的人能在 App 里升级，3.9 让「做完了」和 iPhone 一致。
3. 3.10 和 Android 的 A1 一起做：过夜存活的实测要用它。
4. 3.1、3.2 和 Android 的 outbox 改造一起做（A3）。
5. 4.x 按需。
