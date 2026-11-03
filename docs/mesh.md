# hub 互联（mesh）：一台中心

几台 hub 配对后连成一组，组里**有且只有一台中心 hub**：它服务所有 App、保存所有状态、连所有电脑；其余 hub 是**成员**，只做两件事——
把 App 的请求原样转给中心，把自己连着的电脑的 node 流交给中心。电脑经预装的服务（常驻 node + 连接器 jarvis-link）接入，不用 SSH；
同一台电脑有几条路能到时只出现一次，用最好的那条。方案见 [plan-hub-center.md](plan-hub-center.md)（总览 #293，这一路 issue #435），
本文是实现：协议、取舍、怎么验证。

> 推荐的部署是**只装一台 hub**，其他电脑都装 node 服务（[plan-hub-center.md](plan-hub-center.md) 第 0 节）。多台 hub 互联只给少数场景：
> 两处各有一台常开、互相连不通的机器，或者已经装了好几台 hub 想合并。管理界面里「互联」在「高级」下面。

```text
 App / 网页 ──► 中心 hub ◄══ 互联链路（WSS，钉扎）══ 成员 hub ◄── App（连成员也行：成员转给中心）
                 │    ▲                              │    ▲
     hub 来连（service）│    │电脑来连（link）               │    │  成员的电脑：中心经成员的链路开 node 流
                 ▼    │                              ▼    │
              电脑：常驻 node + 连接器 jarvis-link
```

代码在 `server/internal/hub/`：`mesh.go`（记录、视图循环）、`mesh_view.go`（记录和路由）、`mesh_group.go`（中心、任期、角色）、
`mesh_paths.go`（多条路、去重、选路）、`machine_conn.go`（一台电脑的连接：建、用、换）、`service.go`（kind service：拨连接器）、
`peer_*.go`（配对、链路、拨号、转发、node 流、管理接口、命令行）；数据库在 `internal/db/peers.go`（迁移 0151）和 `internal/db/center.go`（迁移 0191）；
发现在 `internal/mdns/`。

第一版（#394，对等互联：每台电脑有归属 hub、hub 之间泛洪事件和订阅、凭据组内通用）已经整个换掉，见文末「第一版」。

## 机器身份 machine_uid

- node 第一次运行生成 `~/.jarvis/machine-uid`（UUIDv4，0600），之后一直用它；`node.info` 带 `machine_uid`（`internal/node/machineuid.go`）。
  文件坏了（不是 UUIDv4）就换一个新的。
- 克隆出来的虚拟机会带着同一个文件：在那台上跑 `jarvisd node reset-uid`，再重启常驻 node。
- 旧 node 没有这个字段：不参与去重，每条路各算一台。

## 配对和信任

- **互联码**：在 hub B 上生成（管理界面「高级 › 互联 › 生成互联码」，或 `jarvisd hub pair --peer`），一次性、10 分钟：
  `jarvis://peer?hub=<hub_id>&name=<名字>&fp=<公钥指纹>&tls=<TLS 端口>&url=<https 候选>…&code=jx_<64 位十六进制>`。
  只存 hash（`peer_codes`）。没开 TLS 的 hub 生成不了，也用不了别人的码（409，提示去开 TLS）。
- **兑换**（A 拿到 B 的码，`POST /api/v1/admin/peers {"code"}` 或 `jarvisd hub peer add`）：
  1. A 先给 B 签一个互联凭据（设备凭据，kind / role 都是 `peer`，`machine_id` 存 B 的 hub_id）；
  2. A 按码里的 `fp` 钉扎、按候选地址赛跑（内网 → 公网 IPv6 → 域名 → 公网 IPv4，每 300 ms 起一个），
     `POST /api/v1/peer/pair`（`Authorization: Bearer jx_…`），带上 A 的 hub_id、名字、版本、指纹、TLS 端口、https 候选和第 1 步的凭据；
  3. B 在一个事务里用掉这个码、给 A 签凭据、撤销以前给 A 签过的、存下 A（`RedeemPeerCode`），回 B 自己的同样几项、给 A 的凭据，
     **和 B 所在组的中心、任期**；
  4. A 核对回来的 hub_id 就是码里的那台，**加入 B 的组**（下面「中心」），存下 B。失败时撤销第 1 步的凭据。
  结果是双向钉扎、双向凭据：两边都存对方的指纹、地址、对方签给自己的凭据（用 `~/.jarvis/secret.key` AES-GCM 加密后进 `peers.token`），
  以及自己签给对方的那个的 id（`peers.credential_id`）。
- `peer` 凭据只能开 `GET /api/v1/peer`，`jx_` 码只能 `POST /api/v1/peer/pair`，别的接口一律 403；码错 / 过期 / 用过回 401，公网来源按
  `security` 现有规矩限流。
- **断开**（`DELETE /api/v1/admin/peers/{hub_id}`、`jarvisd hub peer remove`）：先在链路上告诉对方（`unpair`），再删自己这边、撤销自己签出的凭据、
  关链路（4012）。对方收到后做同样的事；当时连不上的，它下次拨号被拒（401），管理界面显示「对方不认这台的凭据」。断开后一个对端都没有的 hub
  自己当中心（见下）。在「设备」里直接撤销一个互联凭据，等于断开那台。

## 互联链路

- `GET /api/v1/peer`：WebSocket，只走 TLS 端口（明文一律 403 `tls_required`，回环也不例外），按 `fp` 钉扎；`Authorization: Bearer <对方签给我的 peer 凭据>`；
  permessage-deflate。只有配过对的 hub 能开。
- 链路上跑 node 协议那套 JSON 行 RPC（`internal/rpc`）和流（`internal/streams`）：
  - 单向：`rec`（记录）、`stream.data` / `stream.ack` / `stream.close`（流）；
  - 调用：`hello {hub_id,name,version,epoch}`（两边各调一次，核对 hub_id）、`stream.open {id}`、`unpair`、
    `relay {to,from,ttl,method,params}`（转给组里某一台的调用：`creds.import`、`pair.code`，见下）。
  - 流上跑 HTTP/1.1：成员转给中心的 App 请求、中心经成员开的 node 流（`peer_http.go`、`peer_node.go`）。对端有一个只收这些流的 `http.Server`
    （没有监听 socket），所以这些请求只可能来自已鉴权的链路。
- **一对 hub 一条链路**：两边都拨。hub_id 小的一方立刻拨，大的一方每次先等 0.5–2 秒；同时连上两条时保留 hub_id 小的一方拨出的那条，另一条以 4011 关闭。
- 心跳、重连照 jarvis-link：两边每 10 秒 ping、5 秒没 pong 断开；退避 1 秒起翻倍、最长 1 分钟，随机一半到全部；对方不认凭据（401 / 403 / 4012）等 5 分钟。
  都是配置值，没实测。ping 的往返时间就是这条链路的 RTT（管理界面、选路用）。
- 关闭码：4011 重复的链路、4012 断开了互联、4013 发不过来（发送队列满，8192 条）、1001 hub 停机。

## 记录和路由

每台 hub 把自己的情况做成一条**记录**（`hubRecord`，`mesh_view.go`）在组里泛洪：hub_id、名字、版本、启动时刻（epoch）和序号（rev）、
TLS 端口、指纹、https 候选、**连着的链路**（对端 hub_id 和 RTT）、**它认的中心和任期**、hub.json 里的每台电脑（本机 id、给中心用的配置——
启动器、Claude / Codex 的设置，不带 ssh 主机、tailcat 地址、连接器地址和凭据——、它的路现在能不能用 `avail`、它那一段的 RTT）。

- 新的（epoch、rev 更大）替换旧的，收到新的就转给别的链路；链路一上来就把手里所有的记录发给对方。自己的记录在机器、链路、地址、中心变了时
  150 ms 内重建，内容不变就不发；另外每 30 秒重建一次。
- **路由**：两边记录里都列了对方才算一条边；从自己出发按「跳数少 → RTT 和小 → 下一跳 hub_id 小」求最短路（`meshView.routes`）。不要求两两互联：
  A–B、B–C 时 A 经 B 到 C（成员转给中心、中心经成员开流、`relay` 调用都按这个走，TTL 8，到 0 回 508）。
- 记录存在 `settings` 的 `mesh.records`（最多一分钟一次，停机时也存），7 天没见过的不再用。

## 中心

（`mesh_group.go`，状态在 Postgres 的 `mesh_group` 表，迁移 0191；按本 hub 的 hub_id 存，同一个库被别的 hub 用上（恢复备份、共用测试库）时不会把那台当成成员。）

- 每台 hub 记**中心的 hub_id 和任期号**。一台 hub 从没配过对时自己是中心，任期 0。记录里带这两项，**任期大的说了算**，同一任期中心 hub_id 小的说了算
  （只在两边同时改时才会遇到）；收到更大的就采纳并存库。
- **加入组 = 贴对方的互联码**：A 贴 B 的码，A 把中心改成 B 所在组的中心（B 回包里说的），任期取两边见过的最大值再加一。A 的组里别的 hub
  从 A 的记录里看到更大的任期，跟着并入。所以「贴码的一方连同它的组并入发码方的组」。
- **设为中心**（管理界面「高级 › 互联 › 组里的 hub › 设为中心」、`POST /api/v1/admin/peers/{hub_id}/center`、`jarvisd hub peer center [<hub_id>] [--confirm]`）：
  任期加一、中心改成那台。只能设一台现在连得上的 hub（或自己）。原中心在线：它从记录里看到更大的任期，变成成员，把凭据交给新中心（下面「凭据」）。
  **原中心连不上**：要确认（`{"confirm": true}`，否则 409 `confirm_required`），提示原中心回来后会变成成员。
- **中心连不上时不自动换**：自动换会在网络分区时出现两台中心、状态各写一份。成员对 App 回 `503 {"code":"center_unreachable","center":{"hub_id","name"}}`，
  管理界面顶部提示可以手动设为中心。
- **旧中心回来**：它的任期小，链路一上来就从对方的记录里看到更大的任期，变成成员。重启的 hub 有对端时，启动后先等 10 秒或等到第一条链路上来
  （`meshSettling`）才去连自己的电脑，免得一个已经被换下来的旧中心在知道之前抢一下。
- **退出组**：断开后一个对端都不剩的 hub 自己当中心（任期加一）。组被断成两半、又都还有对端时，不含中心的那一半保持「中心连不上」，到管理界面手动设中心。
- 角色变化时：变成员——停止驱动自己的电脑（run 循环停在那里，`parkReason`），断开自己服务着的 App 连接（它们重连后被转给中心），停用自己的服务（下面）；
  变中心——断开正在转给旧中心的请求，开始连所有电脑。

## 成员转发 App

（`peer_http.go`。）

- 成员收到的 `/api/` 请求（含 WebSocket）除了它自己回答的，**原样**（`Authorization` 也在）经链路转给中心（多跳就一跳一跳转）。
  成员自己回答的：`/api/v1/health`、jarvis-link 的 `/api/v1/link`、`/api/v1/link/stream`、hub 之间的 `/api/v1/peer`、`/api/v1/peer/pair`、
  管理界面 `/api/v1/admin/…`、`/api/v1/auth/pair-codes`（`jarvisd hub pair`），以及**它自己的 link 电脑**拿配对码换凭据的 `/api/v1/auth/tokens`
  （码是它发的、带机器 id）。网页静态文件和 `/admin` 页面不转。
- **来源判断在成员这一端做完**：明文从公网来的已经在成员的 gate 里拒掉；成员把结论放进 `X-Jarvis-Peer-Source: <lan|public> <客户端地址> <tls|plain>`，
  中心照单全收（链路两端是配过对的，这个头只能从链路上来，外面带来的同名头和 `X-Forwarded-For` 在转发前去掉）。中心拿这个地址当 `RemoteAddr`，
  所以日志、来源相关的判断都按真实客户端走；回环在成员那边算局域网（主 token 只认中心自己机器上的回环）。
- **限流在成员**：被封的来源成员直接 429；中心回 401 时成员给这个来源记一次失败（`codeWriter`）。中心对转来的请求不再计数，避免两边重复计。
- 中心换了：成员断开正在转的请求（WebSocket 也断），App 重连后到新的中心。
- **App 配对**：成员上的 `jarvisd hub pair` 经链路请中心生成配对码（`pair.code`），二维码里是中心的地址，App 直接配对到中心。
  成员管理界面的「设备」页回 409 `member_hub`，指向中心的管理界面。

## 凭据和推送

- **凭据交给中心**：成员一旦能到中心（每个中心、任期一次，失败 30 秒后再试），把自己所有有效的 App 凭据（role `full` / `answer`：id、名字、
  kind、role、hash、创建时间，不含凭据本身）交给中心（`creds.import`），中心按 id / hash 去重后存进自己的 `device_tokens`。之后只有中心认凭据；
  已经配对在成员上的手机不用重新配对。jarvis-link 和互联的凭据留在各自的 hub（它们只开那台 hub 的链路）。
- 换中心时旧中心同样把它的凭据（包括当初成员交给它的）交给新中心。在中心撤销的凭据，成员那份副本不会跟着撤销：成员不认凭据，所以不影响；
  成员**退出组**以后它的旧副本会重新生效（已知限制）。
- **推送只有中心发**：设备的推送注册经成员转到中心，推送 payload 的 `hub_id` 是中心的。成员的实时活动（Live Activity）不再推。

## 成员停用的服务

成员的数据都留着，退出组（自己当中心）后恢复：定时任务（`scheduleLoop` 不跑到期的任务）、收件箱的收集、飞书事件和 `/agent`、知识库的归档 / 压缩 /
项目刷新、removed抓取、实时活动的推送。成员的助手主对话、知识库这些 hub 级的数据不合并进中心（App 看到的是中心的）。

## 经成员的路（node 流）

（`peer_node.go`。）中心连一台只在成员 hub.json 里的电脑（或者自己也有、但这时成员那条更好）：经链路发一个 HTTP 升级请求
`GET /api/v1/peer/node`，头 `Upgrade: jarvis-node`、`X-Jarvis-Peer-Node: <成员 hub_id>/<它的机器 id>`、`X-Jarvis-Peer-From: <中心>`，一跳一跳转到那台成员；
成员核对请求来自它的中心、自己是成员、hub.json 里有这台，按 hub.json 的方式打开 node（本机 node、ssh、tailcat、这台的 jarvis-link、连接器），
回 `101 Switching Protocols`，从此两边原样拷字节。中心在这条字节流上跑 node 协议，和别的路一样。成员自己不驱动这台电脑。

## 一台电脑多条路

（`mesh_paths.go`、`machine_conn.go`。）

- **路**：中心 hub.json 里的每一项是一条路（`local` / `service` / `link` / `ssh` / `tailcat`，`ssh` 加 `tailcat_addr` 时先 ssh 再 tailcat 还是一条路），
  每台成员记录里的每台电脑是一条「经成员」的路（`hub`）。
- **去重**：一条路连上后 `node.info` 说了它到的是哪台（`machine_uid`），记在 `settings` 的 `mesh.path_uids`（路 → uid）。uid 相同的路合成**一台机器**；
  uid 还不知道的路自己算一台（第一次连上前没法知道：两条都去连会互相顶一次，之后就知道了）。旧 node 没有 uid：不合并。
  连上后发现 uid 变了（成员把这个 id 改指另一台电脑了）就重新分组，那条路换到它真正到的那台。
- **机器 id**：有中心 hub.json 里的路的，用中心的配置 id（几条都在中心时用 hub.json 里靠前的那条，后面那条的 id 也能用，指向同一台）；
  只在成员上配的，用成员的配置 id，和已有的撞名时加 `@<成员名字的 slug>`（再撞加成员 hub_id 前 6 位、再加数字）。给成员电脑起的 id 存在
  `settings` 的 `mesh.machine_ids`，重启不变。
- **选路**：本机 > 直接（service / link / ssh / tailcat）> 经成员；同类里有 RTT 的先、RTT 小的先。能不能用：link 要它的链路在、service 要连接器最近
  一次 info 回答了（或还没问过）、经成员要那台成员连得上而且它说这条能用、ssh / tailcat 只有试了才知道。失败的路按次数退避（2 秒起翻倍、最长 1 分钟），
  连满 1 分钟的断了以后只等 2 秒。
- **粘性**：在用的路健康就不换。断了立刻选下一条能用的。别的路**明显更好**（类更好，或者同类 RTT 小三成以上而且至少小 5 ms）并且一直保持
  `mesh.path_switch_seconds`（默认 60 秒）才换：先经新路连上、node 回了 `node.info`，再放掉旧的（常驻 node 新的 attach 顶掉旧的，旧连接自己结束），
  中间 App 看不到离线（交互、终端按重连处理：可恢复的交互由 hook 接回来，终端提示重新订阅）。换不成功就给那条路记一次失败，接着用原来的。
- **RTT**：在用的路每 30 秒对 node `ping` 一次；没在用的：service 是连接器 info 的往返，link 是 jarvis-link 链路的 ping，经成员是到那台成员的链路 RTT
  加上成员报的它那一段。都是测得的单次值、没取分位，只用来比大小，没在别处引用。
- **给 App 的字段**（[protocol.md](protocol.md)「数据类型」）：`hub_id` / `hub_name` 是这台电脑现在经过的 hub（直接连的是中心自己，经成员的是那台成员）；
  `route {via: local|service|link|ssh|tailcat|hub, hops: 0|1, rtt_ms}`；不止一条路（或只能经成员）时多 `paths`：每条路
  `{via, hub_id, ok, rtt_ms, active}`。
- 管理界面「电脑」每台列出它的路；`GET /api/v1/admin/routes` 是调试用的全貌（每台 hub 怎么到、每台电脑的每条路）。

## kind service：hub 来连

（`service.go`；连接器那一侧见 [link.md](link.md)「被动模式」。）

- hub.json：`{"id":"example-workstation","name":"开发机","kind":"service","address":"<主机>:18663","proxy":"socks5://<主机>:1080"}`，`proxy` 可选。
  连接器的指纹和凭据不在 hub.json，在 Postgres 的 `connector_credentials`（迁移 0191）：凭据用 `~/.jarvis/secret.key` AES-GCM 封好再存，和模型供应商的 key、
  邮箱凭据同一套做法。
- **配对**：电脑上 `jarvis-link pair --listen`（或安装器 `--node --listen`、菜单栏「让 hub 来连」）打印 `jarvis://connector?addr=…&fp=…&code=…`；
  管理界面「电脑 › 添加电脑 › hub 来连」粘贴（可以改地址、填代理）。hub 用 `link.ParseConnectorLink` 解析，按 `fp` 钉扎（TLS 1.2+、ALPN http/1.1、
  只比 SPKI 指纹不验名字），按链接里的地址顺序试，`POST /api/v1/connector/pair {code, token, machine, hub_id, hub_name}`：凭据是 hub 生成的
  `jc_` + 64 位十六进制，连接器只存 hash。成功才写 hub.json，机器马上开始连，不用重启。码错 / 过期 401：「配对码不对…」；钉扎不符：报错，什么都不发。
- **连**：`GET /api/v1/connector/stream`（WebSocket，`Authorization: Bearer`，permessage-deflate），流上就是 node 的字节；hub 每 10 秒 ping、5 秒没 pong 断开。
  503 `node_unavailable`：把连接器给的原因显示成这台的错误，照常退避。
- **探测**：每 30 秒 `GET /api/v1/connector/info`（不 attach，不影响在用的连接），回答了而且 `machine` 是这台就算能用，往返时间就是这条路的 RTT；
  每台 hub 都探自己 hub.json 里的连接器，成员把结果放进记录。
- **退避**：401（连接器不认凭据）5 分钟内不再拨也不再探——连接器按来源计失败，一分钟 10 次封 15 分钟，hub 不能把自己的地址封掉；
  429 按 `Retry-After` 等；流被 **4002** 关（连接器被配给了别的 hub）：标成「连接器已经配对给了别的 hub…重新配对」，**不再试**，直到在管理界面重新配对
  （`POST /api/v1/admin/machines/{id}/connector`）；**1001**（连接器停了或改回主动模式）照常退避重连。
- **代理**：`proxy` 是 `socks5://[user:pass@]host:port`（`golang.org/x/net/proxy`），拨号、配对、探测都经它；管理界面显示时去掉密码。

## SSH / tailcat：兼容

- hub.json 里已有的 kind `ssh` / `tailcat` 照常能用；管理界面不再提供添加（`POST /api/v1/admin/machines` 对 ssh / tailcat 回 400 `ssh_unsupported`），
  电脑列表标「ssh 已不推荐」。以后再删。
- link / service / 经成员的电脑，「重新部署」改成提示到那台电脑上更新 node 服务（经连接器推更新是 #284）。
- **从 ssh 迁到 service（id 不变）**：
  1. 在那台电脑上装 node 服务、让 hub 来连：`install.sh --node --listen`（Linux）或 Jarvis Link 菜单栏 App，`jarvis-link pair --listen` 拿到链接；
  2. 把 hub.json 里这台的 `kind` 改成 `service`，`ssh_host` 换成 `"address": "<主机>:18663"`，重启 hub；
  3. 管理界面这台显示「连接器还没配对…重新配对」，点「重新配对」粘贴链接。
  不在乎 id 变的话更简单：管理界面「添加电脑 › hub 来连」用一个新 id 加上，两条路到的是同一台电脑会自动合成一台（id 用 hub.json 里靠前的那条），
  确认能连后删掉 ssh 那条、重启。
- **迁到 link**（电脑来连）：「添加电脑 › 电脑来连」用新 id 加，或把 kind 改成 `link`、删掉 `ssh_host` 后用 `jarvisd hub pair --machine <id>` 配对。

## 发现（mDNS）

- 有 TLS 的 hub 用 mDNS 广播 `_jarvis-hub._tcp`（实例名是 hub_id），TXT 里只有 `hub_id`、`name`、`tls`（TLS 端口）、`fp`、`v`（版本），没有任何凭据；
  同时浏览同一类型，管理界面「发现的 hub」列出没配过对的（10 分钟内听到过的）。发现只是列出来，不自动互联。
- `internal/mdns`：只用标准库和 `golang.org/x/net`，和系统的 mDNSResponder / avahi 共用 5353；只实现了 RFC 6762 / 6763 的一个子集。不在点对点接口上收发。
- Docker 的桥接网络里组播出不去，只能「添加 hub」填地址（`network_mode: host` 时可以）。关掉：hub.json `mesh.discovery: false`。
- TLS 只监听回环（`127.0.0.1` / `::1` / `localhost`）的 hub 不广播，只收听：局域网里没人连得到它，广播出去的只会是连不上的地址。
- 手动添加先核对：`POST /api/v1/admin/peers/probe {"address"}` 只请求对方的 `GET /api/v1/health`（不跟随重定向、5 秒、响应最多 64 KB）。

## 管理接口和命令行

管理界面（[admin.md](admin.md)）：

| 接口 | 作用 |
| --- | --- |
| `GET /api/v1/admin/mesh` | `{"role":"center"\|"member","center":{"hub_id","name","online"},"term":3,"hubs":[{"hub_id","name","role","online","self","paired"}]}`：`hubs` 是组里每台（含自己） |
| `GET /api/v1/admin/peers` | 配过对的 hub（每项 `role`、`state`、`rtt_ms`、`machines`…）和局域网里发现的 |
| `POST /api/v1/admin/peers/{hub_id}/center` | 设为中心（hub_id 可以是自己）；`{"confirm":true}`：原中心连不上时；409 `confirm_required` / `unreachable`，404 `not_in_group` |
| `POST /api/v1/admin/peers/code`、`POST /api/v1/admin/peers`、`POST …/probe`、`DELETE …/{hub_id}` | 生成互联码、用对方的码加入、核对地址、断开 |
| `GET /api/v1/admin/routes` | 调试：每台 hub 怎么到，每台电脑的每条路 |
| `GET /api/v1/admin/machines` | 每台多 `hub_id`、`paths`、`address`、`proxy`；kind `hub` 是只在成员上配的电脑（不能删） |
| `POST /api/v1/admin/machines` | kind `link`（电脑来连）或 `service`（`pair_link`、可选 `address`、`proxy`）；加完立刻开始连 |
| `POST /api/v1/admin/machines/{id}/connector` | service 电脑重新配对：`{"pair_link","address"?}` |

命令行在 hub 机器上走同一套接口（前缀 `/api/v1/admin/cli/`，只收回环 + 主 token）：`jarvisd hub pair --peer`、`jarvisd hub peer add '<互联码>'|-`、
`jarvisd hub peer list`（先说这台是中心还是成员）、`jarvisd hub peer remove <hub_id>`、`jarvisd hub peer center [<hub_id>] [--confirm]`。
NAS 的 docker 部署：`sudo /usr/local/bin/docker exec jarvisd /opt/jarvis/bin/jarvisd hub …`。

## 配置（hub.json）

```json
{"mesh": {"discovery": true, "path_switch_seconds": 60}}
```

都可以不写。互联关系、中心和任期、连接器的凭据在 Postgres，不在 hub.json。第一版的 `mesh.takeover_seconds` 不再用（写着也无害）。

## 安全边界

- 互联的信任是对等的：配过对的 hub 能经链路把任何 App 请求（带它说的来源）交给中心，能请成员开它电脑的 node 流（成员只认自己的中心）。
  所以只和自己的 hub 互联；一台 hub 被拿下等于整组被拿下。
- 链路只走 TLS 且按指纹钉扎，互联码一次性、10 分钟；凭据只存 hash（签出的）或加密（收到的）。
- 转发用的 HTTP 服务器没有监听，只从已鉴权的链路上收流，所以 `X-Jarvis-Peer-*` 头不能从外面伪造；客户端带来的同名头在成员转发前被覆盖。
- 成员不认凭据：中心是唯一的鉴权方，撤销在中心立刻生效（30 秒的查询缓存以内）。
- 连接器：hub 按配对时的指纹钉扎，凭据加密存库；连接器只开到常驻 node 的流。
- mDNS 只广播公开的东西（这些本来就在不鉴权的 `/api/v1/health` 里）。

## 怎么验证

- 进程内端到端（`server/internal/hub/mesh_center_test.go`，两三台 hub：127.0.0.1、临时目录、TLS、进程内的假 node、内存里的 Store、
  一个按 link.md「被动模式」行为的假连接器），不需要数据库：
  - `TestCenterMemberForwardsEverything`：加入后只有一台中心；成员不驱动自己的电脑，中心经它连；App 连成员（用成员签的、已交给中心的凭据）
    看到两台、订阅 items（补历史 + 实时）、发消息、读目录、成员电脑上的交互（推送到 App、经成员回答到 node）、终端接管；hello 是中心的、
    `mesh.hubs` 标出中心；未知凭据经成员也是 401；管理接口的角色。
  - `TestCenterUnreachable`：中心停了，成员回 503 `center_unreachable`（带中心名字），不自己当中心。
  - `TestCenterSetAndOldCenterReturns`：两边都在时设为中心（任期加一）；中心停了，不确认不能换、确认后换；旧中心带着旧任期重启，变成员。
  - `TestCenterOneComputerTwoPaths`：同一台常驻 node，中心直连一条、经成员一条：只出现一次、用直连；直连断了马上经成员（订阅照常）；
    直连回来后过 `path_switch_seconds` 换回去一次，之后 3 秒内没有再连。
  - `TestCenterTwoEntriesOneComputer`：中心 hub.json 里两项到同一台：一台机器、两条路，第二项的 id 指向同一台。
  - `TestCenterServiceConnector`：没配对时说要配对；配对码错 / 指纹不对被拒；配对后凭据加密存库、经连接器在线、订阅；连接器配给别的 hub（4002）后
    标成重新配对、不再探；库里的凭据不对时说连接器不认。
  - 还有 `TestMeshDuplicateLink`、`TestMeshPairingCode`；`internal/db` 的 `TestPeersAndPeerCodes`、`TestMeshGroupAndConnectors`、
    `TestAppTokensMoveToTheCenter`（要数据库）；`internal/mdns` 的单元测试。
  - 跑：`cd server && go test -count=1 -p 1 -run 'TestCenter|TestMesh|TestPeer' ./internal/hub/`；`JARVIS_MESH_LOG=1` 打印各台 hub 的日志。
- 本机两台真 hub 冒烟（2026-11-02，不碰真实的 `~/.jarvis`）：各自一个临时 HOME，hub.json 用不同的端口、`public.tls_listen` 和测试库里各自的 schema
  （`database_url` 加 `&search_path=<schema>`），`public.advertise` 写 `https://localhost:<TLS 端口>`，`JARVIS_LOCAL_NODE=0`，`env -i` 起；
  第三个临时 HOME 里跑真的 `jarvisd node --attach` 和 `jarvis-link pair --listen 127.0.0.1:<端口>` + `jarvis-link run`。验证了：
  管理接口粘贴连接器链接 → 配对、立刻经 service 在线；App 凭据列出这台、经它读目录；`jarvisd hub pair --peer | jarvisd hub peer add -` 后
  `peer list` 一台中心一台成员；成员签的凭据交给中心后经成员、在中心都能用；成员上 `jarvisd hub pair` 出的是中心的码；中心停了成员回 503；
  `peer center --confirm` 换中心；旧中心重启后变成员，新中心经它（它的连接器）连那台电脑。无头 Chrome 看了中心的「电脑」「互联」、成员的「概览」「电脑」（深浅色）。
  没在这台电脑上起 herdr / claude（会碰到真实环境），所以真连接器那条没测发消息和终端（进程内测试测了）。

## 没做的 / 已知限制

- 中心自动故障转移；经连接器推送 node 更新（#284）；删除 SSH 支持——都不在这一轮（plan 第 8 节）。
- 换中心不迁移中心的数据：助手、收件箱、知识库、定时任务、推送注册、交互历史都留在原来那台；新中心从它自己的数据开始（设备凭据会交接）。
  App 重新注册推送后新中心才推得到。
- 成员退出组后，它交给中心之前的凭据副本重新生效，中心期间做的撤销不会同步回来。
- 第一次见到一条新路时不知道它到哪台电脑，两条路都去连会互相顶一次（之后记住了）。
- 管理界面删电脑要重启才生效（加是立刻生效的）；改 service 电脑的地址也要重启。
- `/api/v1/usage`、手机工具（`phone_*`）只算中心自己的。
- 成员的「概览」里电脑数按它自己的 hub.json 算，显示「由中心连着」的电脑为不在线。

## 第一版（已换掉）

#394 做的是对等互联：每台 hub 都服务 App，组里的电脑由「归属 hub」连着，别的 hub 经它转发；事件、会话列表、交互在组里泛洪；App 的订阅在归属 hub 上
建替身；凭据组内通用（按 hash 问签发方）；推送落在签发凭据的 hub。用户看过后改成单中心（plan-hub-center.md），这些都删掉了：
`mesh_events.go`、`peer_clients.go`、`mesh_owner.go`、`mesh_push.go`、`mesh_metrics.go`、`mesh_ids.go`。保留了机器身份、配对、链路、记录和路由、发现、管理接口。
