# 连接器 jarvis-link：电脑连 hub，或者 hub 来连电脑

笔记本这类 IP 和网络经常变的电脑，hub 没法稳定地 ssh 进去。2026-10-31 用户要求：不用 Tailscale，也不用反向隧道，
改成由电脑上的客户端主动连 hub、断了自己重连，hub 复用这条链路和这台的 node 通信。这样整条路不需要 SSH，也不需要 VPN。

2026-11-02 用户又定（[plan-hub-center.md](plan-hub-center.md) 第 0、4 节）：hub 和电脑之间一律经电脑上预装的服务连，不用 SSH（权限太大）；
电脑可以主动连 hub，也可以被 hub 连。推荐全家只装一台 hub，其他电脑都装 **node 服务**：常驻 node 加连接器 jarvis-link，一条命令或一个 App 装好。

## 两种模式

同一个 jarvis-link，配对时选，之后随时可以换（`jarvis-link mode active|passive`，菜单栏 App 的配对窗口）。两种模式的配对各存各的，换回去不用重新配对。

| 模式 | 谁连谁 | 适合 | 要什么 |
| --- | --- | --- | --- |
| **主动**（我去连 hub） | jarvis-link 拨 hub 的 WSS `/api/v1/link`（下面「链路」） | 笔记本、在 NAT 后面、经常换网络的电脑 | hub 开着 TLS 端口（`public.tls_listen`），这台连得到 hub |
| **被动**（让 hub 来连） | hub 拨 jarvis-link 在本机监听的 TLS 端口（下面「被动模式」） | 地址固定、hub 连得到的电脑：服务器、example-machine、家里的台式机 | hub 连得到这台的端口（直连或经 SOCKS 代理），这台的防火墙只对 hub 放行这个端口 |

怎么选：这台电脑连得到 hub、hub 连不到它（笔记本、公司内网里的机器连家里的 hub）→ 主动；hub 连得到它、它连不到 hub（hub 在公网不可达的地方，
但能经代理进到这台所在的网络）或者不想让它对外发起连接 → 被动；两边都通时都行，主动模式不用开端口，更省事。

## 装 node 服务

**Linux**（一条命令，`scripts/deploy/install.sh` 的「node」角色）：

```sh
curl -fsSL <站点>/install.sh | sh -s -- --node --pair '<jarvisd hub pair --machine <id> 给的链接>'   # 主动
curl -fsSL <站点>/install.sh | sh -s -- --node --listen [:18663] [--addr <hub 连这台用的地址>]         # 被动
```

- 下载 jarvisd 和 jarvis-link（都按 `SHA256SUMS` 核对）到 `~/.jarvis/bin/`，然后交给 `jarvis-link install`：装常驻 node 的 systemd 用户服务
  `jarvisd-node`（已经有了就保持原样）和 jarvis-link 自己的服务，配对，等它连上（被动模式：等它开始监听，再打印给 hub 管理界面的链接）。
- `--pair` / `--listen` 自带 `--node`；`JARVIS_ROLE=node`、`JARVIS_PAIR`、`JARVIS_LISTEN` 同理。什么都不带、有终端时它会问；
  局域网里已经听到 hub（mDNS `_jarvis-hub._tcp`，`jarvis-link discover`）时先说「全家只要一台 hub，这台装 node 服务就够了」。
- 没有 systemd 的机器照样装好二进制、打印替代做法（见「Linux（headless）」）。
- Mac 上跑 `--node` 只装常驻 node 的 launchd agent 和配对，连接器用菜单栏 App。
- 发布：`scripts/publish.sh` 在 jarvisd 旁边多编四个 `jarvis-link-<os>-<arch>.gz`，进同一份 `SHA256SUMS`。

**Mac**：装菜单栏 App「Jarvis Link」（`./scripts/app/mac-link.sh install`）。App 里带着 jarvisd（`Contents/Helpers/jarvisd`），**第一次运行**就用
`jarvis-link node install --jarvisd <App 里的>` 装好常驻 node 的 LaunchAgent `com.jarvis.node`：没有 `~/.jarvis/bin/jarvisd` 时先复制过去，
已经装了的（比如以前跑过 `scripts/node-launchd.sh install`）保持原样。之后只在菜单里提供「安装本机 node 服务」，用户删掉的不会自己装回来。
App 更新不替换 `~/.jarvis/bin/jarvisd`（经连接器推送 node 更新是 #284）。

**hub 这一侧**：主动模式的电脑在 hub.json 里是 kind `link`，被动模式的是 kind `service`（hub 那一路 #435 实现，管理界面「电脑 › 添加电脑 › hub 来连」粘贴链接）。

## 分工

- **jarvis-link**（`server/cmd/jarvis-link`、`server/internal/link`）：一个单独的小程序，只做三件事：
  - 发现：从候选地址里找到 hub 并连上；
  - 上报：说自己是哪台、什么版本、走的哪条地址；
  - 链路：保持这条连接，并把 hub 在链路上开的流原样转给本机的常驻 node。

  它不承担任何具体功能。
- **node 不变**：功能都在 `jarvisd node` 里。这台电脑要跑常驻 node（上面「装 node 服务」一起装好；单独装是 `jarvis-link node install`
  或 `scripts/node-launchd.sh install`），jarvis-link 只把流转给它的 socket `~/.jarvis/node-attach.sock`。这就是 ssh 起的 `node --stdio` 原来转给它的那条路
  （`internal/node/attach.go`）。替常驻 node 写 systemd / launchd 服务只是打包安装，不是功能：服务的内容和 `scripts/node-launchd.sh` 写的一样。
- **hub**：
  - hub.json 里这台的 kind 是 `link`，hub 不会 ssh 它；
  - 链路一说 hello，hub 就开一条流去连 node，流上跑的 node 协议和 ssh 完全一样（`Machine.connect`，只有拿字节流的 `Machine.dial` 分两种）；
  - 链路一断，这台马上离线。
- **Mac** 上是菜单栏 App「Jarvis Link」（`mac/`）：jarvis-link 作为 helper 打包在 App 里，由 App 启动和看护。
  **Linux**（服务器、NAS、树莓派，不要图形界面）上 `jarvis-link install` 把它自己装成 systemd 用户服务，见「Linux（headless）」。

## 链路（主动模式）

```text
jarvis-link ── WSS /api/v1/link ──▶ hub 的 TLS 端口（public.tls_listen）
  → hello {machine, version, address}          第一条消息；机器 id 和凭据对不上时 hub 以 4003 关闭
  ← endpoints {endpoints, hub_name, mesh_hubs} 连上就发，hub 的地址或互联组变了再发
  ← open {id}                                  hub 要连 node 了
      jarvis-link 先连 ~/.jarvis/node-attach.sock，再开 WSS /api/v1/link/stream?id=<id>，两边原样转发字节
  → open_failed {id, error}                    开不了（常驻 node 没在跑等），hub 把原因记成这台的错误
  ← node {online, error}                       hub 连没连上 node，菜单栏显示
```

**选路**
- 候选地址按「内网 → 公网 IPv6 → 域名 → 公网 IPv4」排，手动加的排在同类前面。
- 每隔 300 ms 开始试下一个，几个同时进行，谁先连上用谁，晚连上的关掉。每个候选 8 秒超时。
- 不在家时内网地址连不上或一直挂着，也只多等 300 ms 就会去试下一个。这些都是配置值，没实测。

**hub 的名字和互联组**
- hub 在 `endpoints` 里带 `hub_name`：hub.json 的 `name`，没填时 hub 机器主机名的短名，Docker 里（主机名是容器 id）是 `hub-<hub_id 前 6 位>`
  （[config.md](config.md)）；和 `mesh_hubs`：hub 所在互联组有几台 hub（含自己）。互联（#294）合进来之前固定是 1，由 `meshHubCount()`
  给出，互联那一路接线；组变了 hub 重发一次 `endpoints`（`sendLinkEndpoints`）。
- jarvis-link 把名字记进 link.json 的 `hub_name`，断开时也知道自己连的是哪台；状态里带 `hub_name`，连着时带 `mesh_hubs`。
- 名字只留一行可打印的文字（控制字符换成空格），最多 80 个字。
- 旧 hub 两个都不发：状态里没有这两项，Mac 菜单照旧，`jarvis-link status` 显示 hub id。只带名字、不带地址的 `endpoints` 不会清掉候选地址。

**TLS**
- 按配对时拿到的 hub 公钥指纹（SubjectPublicKeyInfo 的 SHA-256）钉扎，内网也走 TLS。
- hub 的证书是自签的，信任靠的就是这个指纹，和 App 的公网直连一样（[network.md](network.md)「TLS 与公钥钉扎」）。

**上报**
- 连上后第一条消息 hello，报这台的机器 id、jarvis-link 的版本和用的哪个地址。
- hub 核对机器 id 和凭据绑定的那台一致，然后马上开流连 node，不等 ssh 那样的重试间隔。
- 机器列表里这台多一个 `link` 字段：`{version, address, since}`。

**心跳**
- 两边都每 10 秒 ping 一次，5 秒没有 pong 就断开。死链路约 15 秒内发现。
- hub 在流上也 ping，防止链路还通、流的 TCP 连接却已经悄悄断了。
- 这些都是配置值，没实测。

**重连**
- 指数退避：1 秒起，翻倍，最长 1 分钟；每次在「一半到全部」之间随机。连满 1 分钟的链路断了以后从头算。
- 几种特殊情况：

  | 情况 | 等多久 |
  | --- | --- |
  | 被同一台的新链路顶掉（4001） | 30 秒 |
  | 凭据被拒（401 / 403）或被撤销（4002） | 5 分钟，或者等到 link.json 变了（重新配对）就立刻重试 |
  | hub 重启（1001） | 照常退避 |
  | 手动「立即重连」 | 不等 |

- 同一台电脑只能跑一个 jarvis-link（`~/.jarvis/link.json.run` 文件锁），否则两个会互相顶。

**换网**
- Mac：App 用 `NWPathMonitor`，网络一变就给 helper 发 `SIGUSR1`：
  - 链路原来用的本机地址已经不在了，马上重连；
  - 还在，就立刻 ping 一次（3 秒），不通再重连；
  - 本来就没连着，立刻重试。
- Linux：jarvis-link 自己用 netlink 盯地址变化（`internal/netaddr`）。
- 「立即重连」是 `SIGUSR2`。

**转发**
- 只转给 `~/.jarvis/node-attach.sock`，只连回这条链路用的那个 hub 地址，流 id 只能是字母、数字、`-`、`_`。hub 没法让它去连别的地址。
- 流上开 permessage-deflate 压缩。原来的 ssh 开了 `Compression=yes`，原因见 `hub/machine.go` 的注释。
- 对常驻 node 来说，每条流就是一次新的 attach 连接，新的顶掉旧的，和 ssh 时一样。

## 被动模式（hub 来连）

协议在 [plan-hub-center.md](plan-hub-center.md) 第 4 节定死，这里是细节；hub 那一侧（#435）按这里实现拨号。实现：`server/internal/link/`
（`listen.go` 监听和三个接口、`passive.go` 配对码和链接、`tlsid.go` 身份、`ratelimit.go` 限流），命令行在 `server/cmd/jarvis-link/passive.go`。

### 监听和身份

- 监听 link.json 里的 `passive.listen`，默认 `:18663`（所有地址）；`jarvis-link mode passive --listen <地址>` 或 `pair --listen <地址>` 改。只开这一个端口。
- TLS 1.2 起，ALPN 只有 `http/1.1`。身份是连接器自己的 ECDSA P-256 密钥和自签证书，在 `~/.jarvis/link-tls/`（目录 0700，`link.key`、`link.crt` 0600），
  第一次 `pair --listen` 或第一次监听时生成。证书只带 `connector.jarvis.invalid` 这一个名字，不带主机名和地址（谁连到这个端口都看得到证书）。
- 指纹和 hub 的一样：证书 SubjectPublicKeyInfo DER 的 SHA-256，base64url 不带填充（43 个字符）。hub 按配对链接里的指纹钉扎，不验证书名字。
- 证书一年，剩不到 30 天时用同一把密钥重签（监听中也会，握手时检查），指纹不变。换了密钥（删了 `link-tls/`）就要重新配对。这些是配置值。

### 配对

方向和主动模式反过来（电脑不一定连得到 hub）：

1. 电脑上 `jarvis-link pair --listen [<监听地址>] [--addr <地址>]…`（菜单栏 App：配对窗口选「让 hub 来连」，点「生成配对链接」）。
   它把 link.json 改成被动模式、生成一次性配对码（128 位随机，`jc_` 开头，只存 SHA-256），**标准输出只有链接**，说明在标准错误：

   ```text
   jarvis://connector?addr=<host:port>[&addr=<host:port>…]&fp=<连接器指纹>&code=<一次性码>
   ```

   - `addr` 可以重复，好的在前，都是 `host:port`（不带协议，IPv6 带方括号，整个做了 URL 编码）。没给 `--addr` 时是本机的地址加端口，按主动模式试 hub
     的顺序排：内网（私网 IPv4、ULA）→ 公网 IPv6（Linux 上排除临时、废弃、tentative 的地址）→ 公网 IPv4；不要回环、链路本地、隧道和桥接网卡
     （Docker、VPN、Tailscale……）。全是虚拟网卡时（容器的 eth0、NAS 的网卡挂在虚拟交换机上）退一步用它们，隧道和桥接照样不要。
     一个也没有时 `pair --listen` 报错要 `--addr`；`install --listen` 照样装好服务，提示之后 `pair --listen --addr`。给了 `--addr` 就只有给的那些。
   - 码 10 分钟内有效，只能用一次；再 `pair --listen` 生成新的，旧码作废。已经配对过的 hub 在新 hub 用掉新码之前照常能连。
2. hub 管理界面「电脑 › 添加电脑 › hub 来连」粘贴链接（可以改地址、填代理），hub 按 `fp` 钉扎连过去，`POST /api/v1/connector/pair` 用码换长期凭据：
   凭据是 hub 自己生成的随机串，连接器只存它的 SHA-256。
3. 之后 hub 用这个凭据开 `stream` 连 node，和主动模式一样。

### 接口

除了下面三个什么都没有：没有 shell、文件、端口转发，流只能连到 `~/.jarvis/node-attach.sock`。其他路径、其他方法一律 `404 {"error":"not_found"}`。
错误都是 JSON：`{"error":"<代码>","message":"<给人看的中文>"}`。

| 接口 | 认证 | 请求 | 回答 |
| --- | --- | --- | --- |
| `POST /api/v1/connector/pair` | 一次性码 | body ≤ 16 KiB：`{"code","token","machine","hub_id","hub_name"}`；`code`、`token`、`machine` 必填（`token` 32–512 个可打印 ASCII、不含空白），`hub_id`、`hub_name` 可选但请带上 | `200 {"version"}`；字段不对 `400 bad_request`（不计限流，不作废码）；码错、过期、用过 `401 bad_code`（计限流） |
| `GET /api/v1/connector/info` | `Authorization: Bearer <凭据>` | — | `200 {"machine","version"}`：`machine` 是配对时 hub 给的机器 id；凭据缺或错 `401 unauthorized` |
| `GET /api/v1/connector/stream` | 同上 | WebSocket 升级 | 先认证，再连 node-attach.sock，最后才升级：node 连不上 `503 node_unavailable`（`message` 说原因，hub 可以记成这台的错误）；不是升级请求 `400`；升级后见下 |

- 配对成功会替换之前配对的 hub（「只认配对它的那台」：组里的中心；换中心时由新中心重新配对）：旧凭据马上不认，旧凭据开着的流以关闭码 **4002** 关掉。
- 流：二进制消息，原样是 node 的字节，两个方向都不改。接受 permessage-deflate（context takeover，和主动模式的流、原来 ssh 的 `Compression=yes` 一样）。
  连接器每 10 秒 ping、5 秒没 pong 就断，hub 的 ping 照常回。hub 关流时连接器对 node 半关（写端关掉），node 读完收尾，node 关了流就结束。
- 对常驻 node 来说每条流是一次新的 attach，新的顶掉旧的，和主动模式、ssh 时一样；连接器自己不管哪条是新的。
- 连接器停止或改回主动模式时，流以 **1001** 关；被动模式的配对留在 link.json 里，改回被动后原凭据照样能用。

### 认证和限流

- 凭据只存 hash（link.json 的 `passive.token_sha256`），比较用常数时间。
- 认证失败（码不对、凭据缺或错）按来源计数：IPv4 一个地址、IPv6 一个 /64 算一个来源。**内网和回环也算**（hub 多半就在内网，不能因为在内网就不限）。
  60 秒内失败 10 次封 15 分钟；封禁中的来源先于认证被拒，凭据对也回 `429 {"error":"rate_limited"}` 和 `Retry-After`（秒）。最多记 4096 个来源，
  满了先丢没封的。这些都是配置值，没实测。
- 日志：认证失败、TLS 握手失败每 30 秒最多一行（带省略了几条），来源只记 /24 或 /64。

### 给 hub 实现方的建议

- 链接里的 `addr` 按顺序试（或者先只取第一个），记下连通的那个；用户在管理界面改了地址就用改的。
- 拿到 401 要退避，别拿着坏凭据一直重试（会把自己的来源封掉）；429 按 `Retry-After` 等。
- 流被 4002 关掉：这台被别的 hub 配走了，标成「要重新配对」，别重试；1001 或连不上：照常退避重连。
- 503 `node_unavailable`：链路是通的、常驻 node 没在跑，把 `message` 显示成这台的错误，过一会再试。
- 想知道连接器活着、测 RTT：`GET /api/v1/connector/info`。连接器会记下 hub 最后一次来的时间和来源，显示在菜单栏和 `jarvis-link status` 里。

### link.json 里的被动模式

```json
{"mode":"passive","passive":{"listen":":18663","hub_id":"…","hub_name":"家里","machine":"example-machine",
 "token_sha256":"…","paired_at":"…","code":{"sha256":"…","expires":"…"}}}
```

- 和主动模式的字段（`hub`、`machine`、`fp`、`token`、`endpoints`）分开，谁也不覆盖谁；`mode` 没有或是 `active` 都是主动模式（旧文件照旧能用）。
- `jarvis-link pair <链接>`（主动）把 `mode` 改成 `active`，`pair --listen` 改成 `passive`，`mode` 只改模式。正在跑的 jarvis-link 每秒看一眼 link.json，
  模式变了就换过去（关掉链路或监听，开另一个）；监听地址变了就换端口。
- `jarvis-link uninstall --purge` 连 `link-tls/` 一起删。

### 安全边界

- 能连到这个端口的人：没有码或凭据什么都做不了，只能看到证书（没有主机名和地址）和 404；猜码、猜凭据被限流，而且两者都是 128 位以上的随机数。
- 拿到凭据的 hub：只能开到常驻 node 的流，和 ssh 时 hub 能做的一样（node 协议本身），不多。
- 配对链接在 10 分钟里、用掉之前等于配对权：别贴到不可信的地方。链接里的 `fp` 让 hub 不会连到冒充的连接器。
- 防火墙：只对 hub（或 hub 用的代理）放行这个 TCP 端口，别对所有人开。`pair --listen`、`install --listen`、`mode passive` 都会提示，例如
  `sudo ufw allow from <hub 的地址> to any port 18663 proto tcp`；路由器、云主机的安全组同理。Mac 开着应用防火墙时，第一次监听系统可能问要不要允许
  传入连接（没实测）。

## 凭据（主动模式）

- 是设备凭据的一种：kind `link`、role `link`，绑定 hub.json 里的那台机器（`device_tokens.machine_id`，迁移 0131）。
  - hub 只存 hash。
  - 只能开 `/api/v1/link` 和 `/api/v1/link/stream`，别的接口一律 403。
  - App 的「设置 › 设备」里能看到、能撤销，撤销后链路立刻断开（4002）。
- 两种配对码都能换成它：
  - `jarvisd hub pair --machine <id>`：专给这台的配对码（`pair_tokens.machine_id`）。链接是 `jarvis://link?…&machine=<id>`，
    只能换成这台的链路凭据，别的什么都做不了。标准输出只有链接，说明打在标准错误，方便用管道。
  - App 用的普通配对码（`jarvisd hub pair`），再指定机器 id：命令行 `jarvis-link pair --machine <id>`，App 里填「机器 id」。
- hub 要开着 TLS（`public.tls_listen`），没开时 `--machine` 返回 409。
- 鉴权失败按 hub 原有的规矩限流（公网来源计数）。公网只收 TLS；内网也不收明文，只有回环可以。
- 凭据存在 `~/.jarvis/link.json`（0600）：hub id、机器 id、指纹、凭据、候选地址（hub 下发的加上手动加的）。

## 接入一台电脑（以 Mac 为例）

推荐的做法见上面「装 node 服务」；下面是 Mac 的主动模式一步步来（被动模式第 4、5 步换成配对窗口里的「让 hub 来连」，链接粘贴到 hub 管理界面）。

1. hub.json 的 `machines` 加一条（或把原来的 ssh 那条改成）`{"id":"macbook","name":"MacBook","kind":"link","icon":"laptop"}`，重启 hub。
2. claude 是 shell 包装函数时，要在 hub.json 的 `machines[].launchers` 里配启动器。
3. 装 App：`./scripts/app/mac-link.sh install`。它会编译（连同 jarvisd）、放进 `/Applications`，然后打开；第一次打开就装好常驻 node（`com.jarvis.node`），
   已经装了的不动。
4. 在 hub 机器上生成配对链接：
   - 普通部署：`jarvisd hub pair --machine macbook`；
   - NAS 的 docker 部署：`sudo /usr/local/bin/docker exec jarvisd /opt/jarvis/bin/jarvisd hub pair --machine macbook`。

   把链接粘贴进 菜单栏 › 配对 / 修改连接…。也可以走命令行：
   ```sh
   … | "/Applications/Jarvis Link.app/Contents/Helpers/jarvis-link" pair -
   ```
5. 确认：菜单栏图标变成已连接，菜单里写「本机 node 在线」，App 的「设置 › 机器」里这台在线。
6. 想开机就连上：在菜单里勾「登录时启动」（`SMAppService`）。

Linux 上见下面「Linux（headless）」：编出 `jarvis-link`，`jarvis-link install --pair -` 一条命令配对、装好、等它连上。

## 菜单栏 App（`mac/`）

**构建**
- `./scripts/app/mac-link.sh build|install|snapshot <目录>`。
- SwiftPM 包，用 `xcodebuild` 编：这台开发机上 `swift build` 默认的构建系统报「找不到开发者目录」。
- jarvis-link 和 jarvisd 编进 `Contents/Helpers/`（jarvisd 给常驻 node 用，见「装 node 服务」；App 因此大了约 37 MB）。
- 用本机 ad-hoc 签名（Sign to Run Locally），不写死团队。
- bundle id 默认用占位的 `com.example.jarvis.link`，可以用 `~/.config/jarvis/config.json` 的 `mac.link_bundle_id` 或环境变量
  `JARVIS_LINK_BUNDLE_ID` 覆盖。
- `UNIVERSAL=1` 时同时编 Intel 版。

**系统设置**
- 没有 Dock 图标（`LSUIElement`）。
- 不开沙盒：要读写 `~/.jarvis` 里的 socket 和配置。
- Info.plist 写了 `NSLocalNetworkUsageDescription`：macOS 15 起，连内网地址要本地网络权限，第一次连内网时系统会弹窗问。
  拒绝的话，内网候选会报「不可达」，要去 系统设置 › 隐私与安全性 › 本地网络 里打开。

**helper**
- App 用 `jarvis-link run --parent` 启动它。
- 标准输出一行一个 JSON 状态，App 读最新一行。
- 标准错误追加到 `~/.jarvis/link.log`，超过 1 MB 就清掉重来。
- `--parent` 让 helper 在标准输入关掉时退出，所以 App 不管怎么退出（包括崩溃），helper 都跟着退出。
- helper 退出了，App 会重启它：1 秒起翻倍，最长 30 秒；跑满 1 分钟后从 1 秒重新算。

**图标**：用模板图，深浅色菜单栏上都看得清。

| 状态 | 图标 |
| --- | --- |
| 已连接（被动模式：hub 连进来了） | `personalhotspot` |
| 连接中 | `arrow.triangle.2.circlepath` |
| 被动模式在监听、等 hub 连进来 | `antenna.radiowaves.left.and.right` |
| 未连接或出错（被动模式：监听不了） | `personalhotspot.slash` |
| 未配对 | `link.badge.plus` |
| 链路通了，但 hub 连不上本机 node | `exclamationmark.triangle` |

**菜单**
- 第二行是模式：「我去连 hub（主动）」，或「让 hub 来连（被动）· 监听 :18663」。
- 主动模式：哪条地址（内网 / 公网 IPv6 / 域名）、连了多久、断开的原因（每个候选一行）、本机 node 在不在线。
- 被动模式：hub 连进来了（来自哪个地址、连了多久、本机 node 在不在线）；或者在监听（上一次 hub 什么时候、从哪连进来）；或者等配对（配对链接还有几分钟有效）；
  或者监听不了（端口被占用等，多久后重试）。
- 连着哪台 hub、所在互联组几台：「hub「家里」· 互联组共 3 台 hub」，只有一台时「hub「家里」· 没有互联别的 hub」；没连上时只写名字。
  旧 hub 不发名字时没有这一行，菜单和以前一样。
- 立即重连（被动模式没有：是 hub 来连）、配对 / 修改连接…、安装本机 node 服务（只在没装时出现）、登录时启动、退出。

**配对窗口**
- 顶上选「我去连 hub（主动）」或「让 hub 来连（被动）」，默认是现在的模式；只是看，不切换。切换发生在配对时（主动：粘贴链接配对；被动：生成配对链接），
  或者点「改用这个」（那个模式以前配对过，不用重新配对，`jarvis-link mode`）。
- 主动：粘贴配对链接，必要时填机器 id。App 调 `jarvis-link pair`，成功的结果或错误都显示在窗口里。列出候选地址，正在用的那个标「在用」；
  可以手动加一个地址（`jarvis-link address add`），手动加的可以移除。
- 被动：填监听地址（默认 `:18663`），「生成配对链接」调 `jarvis-link pair --listen`，显示链接（可以选中、有「复制」）、链接里的地址和防火墙提示；
  已经配对过时写着和哪台 hub、这台叫什么。

**截图**
- `./scripts/app/mac-link.sh snapshot <目录>` 用 App 自己的视图离屏画出这些图，深浅色各一套：
  - 菜单的每个状态；
  - 配对窗口；
  - 各个图标。
- 不需要屏幕录制权限，也不会拍进别的东西。

## 状态行（helper → App）

```json
{"state":"connected","machine":"macbook","hub_name":"家里","mesh_hubs":1,"address":"https://192.0.2.10:18662","kind":"lan",
 "since":"…","node":"online","endpoints":[{"url":"https://192.0.2.10:18662","kind":"lan"}]}
```

- `mode`：`active` / `passive`。
- `state`：`unpaired` / `connecting` / `connected` / `waiting` / `refused`；被动模式是 `unpaired`（在监听、等配对）/ `listening`（配对了、hub 现在没连着）/
  `connected`（hub 开着流，`address` 是 hub 的地址、`since` 是流开始的时间）/ `waiting`（监听不了）。
- 被动模式另有 `listen`（在哪监听）、`addrs`（hub 可以用的本机地址，`host:port`，好的在前）、`last_seen` 和 `from`（hub 最后一次来的时间和地址）、
  `code_expires`（`pair --listen` 的码到什么时候）。
- `node`：`online` / `offline` / `unknown`，原因在 `node_error`。
- `waiting` / `refused` 时带 `error`（多个候选的失败用「；」隔开）和 `retry`（下次尝试的时间）。
- `hub_name`、`mesh_hubs`：见「hub 的名字和互联组」，旧 hub 时没有。
- 不带凭据。
- 每一行同时写进 `~/.jarvis/link.status.json`（加上 `pid`、`version`、`updated`；正常退出时写 `"state":"stopped"`），给
  `jarvis-link status` 读：它没法问正在跑的那个。

## Linux（headless）

服务器、NAS、树莓派这类没有图形界面的机器上，`jarvis-link` 自己就够了。

**构建**
- 纯 Go、不要 cgo：`cd server && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o jarvis-link ./cmd/jarvis-link`。
  `amd64`、`arm64`、32 位树莓派（`GOARCH=arm GOARM=7`）都能编；Windows 也能编了（文件锁、信号按平台分文件），但还没有托盘程序（#299）。
- 常驻 node 用同一个提交编的 `jarvisd`（`~/.jarvis/bin/jarvisd`）；`scripts/node-launchd.sh install` 在 Linux 上把它装成 systemd 用户服务 `jarvisd-node`。

**一条命令装好**

```sh
jarvisd hub pair --machine nas                   # 在 hub 机器上；标准输出只有链接
./jarvis-link install --pair - <<< '<链接>'       # 在这台电脑上；链接从标准输入读
```

被动模式：`./jarvis-link install --listen [:18663] [--addr <hub 连这台用的地址>]`，装好后打印给 hub 管理界面的链接。

`install` 依次做：
1. 带 `--pair` 时先配对（同 `jarvis-link pair`；`--pair` 后面不跟链接或跟 `-` 时从标准输入读）。带 `--listen` 时改成被动模式，还没配对过就生成配对链接
   （同 `pair --listen`）；已经配对过就保持原样。两个不能一起给。
2. 把自己复制到 `~/.jarvis/bin/jarvis-link`；已经是它就不复制。拿新版本再跑一次 `install` 就是升级。
3. 常驻 node 的服务（systemd 用户服务 `jarvisd-node`，内容同 `scripts/node-launchd.sh`）还没有就装上并启动：要有 `~/.jarvis/bin/jarvisd`（安装脚本会先放好），
   没有时用 `--jarvisd <路径>` 给一个复制过去；已经装了的保持原样；`--no-node` 不管它。
4. 写 `~/.config/systemd/user/jarvis-link.service`（有 `XDG_CONFIG_HOME` 时在它下面），`daemon-reload`、`enable`、`restart`。
5. 没开 linger 时提示 `sudo loginctl enable-linger <用户>`：不开的话没人登录时用户实例不在，退出登录服务就停，开机也不会起来。
6. 最多等 20 秒，等到连上 hub、hub 查过本机 node，打印一行结论。连上了退出码 0；没连上打印每个候选为什么失败，退出码 1，服务照样装着、一直重试。
   被动模式等到开始监听（或者说清楚为什么监听不了），再打印配对链接和防火墙提示。

单元里不写模式和监听地址：都在 link.json 里，换模式、换端口不用重装服务，在跑的 jarvis-link 一两秒内就换过去。

还没配对就 `install`：服务先跑着（unpaired），之后 `jarvis-link pair` 几秒内它就用上新凭据，不用重启（它每 2 秒看一眼 link.json 变没变）。

写出的 unit：

```ini
[Service]
ExecStart="/home/<用户>/.jarvis/bin/jarvis-link" run --log "/home/<用户>/.jarvis/link.log"
Restart=always
RestartSec=5
StandardOutput=null

[Install]
WantedBy=default.target
```

- 不等 `network-online.target`：用户实例里没有这个 target，jarvis-link 自己会一直重试。
- 状态行是给菜单栏 App 的，不进 journal；日志写 `~/.jarvis/link.log`（`run --log`，满 1 MB 挪到 `link.log.1` 重新开始，这是配置值）。
  journal 不持久的机器上普通用户 `journalctl --user` 看不到用户服务的日志，所以日志不靠它。
- `su`、`sudo -u`、`docker exec` 进来的会话没有 `XDG_RUNTIME_DIR`，`systemctl --user` 找不到用户实例：install / status / uninstall
  在它没设时自己补上 `/run/user/<uid>`（存在才补）。用户实例本身不在（没开 linger、也没人登录）时补了也没用，提示开 linger 或者 ssh 登录这个用户再装。
- 用 root 装会提示：服务以 root 跑、用 `/root/.jarvis`，常驻 node 也要在 root 下。

**没有 systemd**（容器、部分 NAS）

`install` 照样配对、复制二进制，然后打印能直接粘贴的替代做法，退出码 1：
- 现在就在后台跑：`nohup ~/.jarvis/bin/jarvis-link run --log ~/.jarvis/link.log >/dev/null 2>&1 &`
- 开机就跑：cron 的 `@reboot` 跑同一条；
- 群晖：任务计划里加一个「开机」触发的用户脚本，内容是上面 nohup 那一行；
- 容器：让 `jarvis-link run --log …` 当主进程、重启策略 `unless-stopped`、`~/.jarvis` 挂成卷，常驻 node 在同一个容器里；
- supervisord、runit、s6、OpenRC：前台跑 `jarvis-link run --log …`，退出就重启。

常驻 node 同理：`nohup ~/.jarvis/bin/jarvisd node --attach >> ~/.jarvis/node-attach.log 2>&1 &`。

**状态、日志、卸载**

| 命令 | 做什么 |
| --- | --- |
| `jarvis-link status` | 模式、链路（哪条地址、连了多久；没连上时每个候选为什么失败）、hub 的名字和互联组台数、本机 node、机器 id、候选地址、systemd 服务（active / enabled / linger）、pid 和版本、日志在哪。被动模式换成：监听地址、在监听还是 hub 连着、配对的 hub 和什么时候配的、上一次 hub 什么时候从哪连进来、连接器指纹、hub 可以用的本机地址。连着（被动模式：在监听也算）时退出码 0，否则 1 |
| `jarvis-link status --json` | 同上给脚本用：`running`、`mode`、`paired`、`machine`、`hub_id`、`hub_name`、`endpoints`、`link`（最后报的状态）、`passive`（`listen`、`paired`、`hub_id`、`hub_name`、`machine`、`paired_at`、`code_expires`、`fp`）、`service`、`config`、`log`；不含凭据和它的 hash |
| `jarvis-link logs [-f] [-n 行数]` | `~/.jarvis/link.log` 的最后 100 行（`-n` 改），`-f` 一直跟着，日志换文件也跟得上；没有日志文件、装了服务时看 journal |
| `jarvis-link uninstall` | 停掉、禁用、删掉 systemd 用户服务，凭据留着 |
| `jarvis-link uninstall --purge` | 再删凭据、状态、日志和 `~/.jarvis/bin/jarvis-link`；还有别的 jarvis-link 在跑（菜单栏 App、手动起的）时拒绝。hub 那边的凭据去 App「设置 › 设备」撤销 |
| `jarvis-link pair` | 不给链接或给 `-` 时从标准输入读；在终端里提示粘贴，回车就读那一行 |
| `jarvis-link pair --listen [<监听地址>] [--addr <地址>]…` | 被动模式的配对链接（标准输出只有链接，方便管道） |
| `jarvis-link mode [active\|passive] [--listen <地址>]` | 看 / 换模式，两种模式的配对都留着 |
| `jarvis-link node install [--jarvisd <路径>]` / `node status [--json]` / `node uninstall` | 常驻 node 的服务（Linux systemd `jarvisd-node`，Mac launchd `com.jarvis.node`）；已经有的不动；`uninstall` 不删 jarvisd |
| `jarvis-link discover [--wait <秒>] [--json]` | 局域网里听到的 hub（mDNS），只看不配对；听到了退出码 0 |

`status` 的依据：
- 在不在跑：`~/.jarvis/link.json.run` 的文件锁有没有人拿着（谁起的都算：systemd、菜单栏 App、手动）。它要拿一下锁才知道，
  恰好在这一瞬间启动的 jarvis-link 会因为「已经有一个在运行」退出，由 systemd 5 秒后再起。
- 链路状态：`~/.jarvis/link.status.json`（见「状态行」）；没在跑时显示最后一次报告和时间。
- **不去连常驻 node 的 socket** 来判断它在不在：对常驻 node 来说每条连接都是一次新的 attach，会把 hub 正在用的那条顶掉。本机 node 只看 hub 报的。
- Mac 上也能用（菜单栏 App 的 helper 一样写状态文件）；`install` 在 Mac 上只提示去装菜单栏 App。

`scripts/deploy/link-systemd.sh` 留着当薄包装：`install` / `uninstall` / `status` 转给 `~/.jarvis/bin/jarvis-link`，`restart` 是 `systemctl --user restart`。

**怎么验证的**（2026-11-02）：本机 Docker 里的干净 Debian 12 容器（arm64），测试 hub 跑在宿主上、只听 127.0.0.1，容器经
`host.docker.internal` 连它的 TLS 端口，测完都关掉。
- 没有 systemd 的容器：`install --pair -` 配对、复制、打印替代做法（退出码 1）；照提示 `nohup … run --log …` 后 `status` 显示连上、
  `logs` 有日志；起了常驻 node 后本机 node 在线，hub 的机器列表里这台在线。
- 有 systemd 的容器（`--privileged --cgroupns=host`，镜像要有 `libpam-systemd`，否则用户实例起不来；`loginctl enable-linger`）：
  用 `docker exec -u` 进去（没有 `XDG_RUNTIME_DIR`）一条 `install --pair -` 装好并连上；`scripts/node-launchd.sh install` 后本机 node 在线；
  重启容器（相当于开机）后自己连上；关掉 linger 后 `status` 提示开 linger；再跑一次 install（升级）、先 install 后 pair、
  uninstall、uninstall --purge 都按预期。
- amd64 版在 amd64 模拟容器里跑了 version 和 status。没在真的树莓派 / NAS 上试过。

## 出了家门也要用

链路只走 hub 的 TLS 端口。出了家门还要做这几件事：
- DSM 防火墙放行 18662/tcp（18660 不放）。
- 路由器放行到 NAS 的 IPv6 入站。
- 在 hub.json 的 `public.advertise` 写上域名（DDNS 指到 NAS 的 IPv6）。连上一次后，hub 会把它下发给 jarvis-link；也可以在 App 里手动加。

只有 IPv4 的网络连不到家里的 IPv6，这一点和手机一样。

## 排查

| 现象 | 处理 |
| --- | --- |
| 被动模式「监听不了 :18663：端口被占用了」 | 别的程序占了：换个端口 `jarvis-link mode passive --listen :18664`（然后在 hub 管理界面改这台的地址），或者停掉那个程序 |
| 被动模式 hub 连不进来，`status` 里「上一次 hub 连进来」一直不变 | 防火墙 / 路由器没放行这个端口；链接里的地址 hub 连不到（在 hub 管理界面改地址或填代理）；hub 的指纹对不上（删过 `link-tls/`：重新配对） |
| 被动模式 hub 报 429 | 那个来源认证失败太多次（多半是拿着旧凭据重试），等 15 分钟，或者重启 jarvis-link 清掉计数 |
| `pair --listen` 说找不到地址 | 只有回环、隧道或临时地址：`--addr <hub 连这台用的域名或地址>` |
| 菜单写「链路是通的，但 hub 连不上本机的 node」 | 常驻 node 没在跑：`launchctl print gui/$(id -u)/com.jarvis.node`，日志在 `~/.jarvis/node-attach.log` |
| 「hub 不认这台的凭据」 | 凭据被撤销了，或者 hub 换了数据库：重新配对 |
| 「这台电脑在别处又连了一条链路」 | 同一个 link.json 被拷到了别的电脑，或者开了两个 jarvis-link |
| 内网候选「不可达」，网络却是好的 | 看本地网络权限 |
| Linux：`install` 说「连不上你的 systemd 用户实例」 | `su` / `sudo -u` / `docker exec` 进来、又没开 linger：用 ssh 直接登录这个用户，或者先 `sudo loginctl enable-linger <用户>` |
| Linux：退出登录后链路就断，重启后不连 | 没开 linger：`sudo loginctl enable-linger <用户>`（`jarvis-link status` 的「后台服务」一行会说） |
| Linux：链路通了，本机 node 不在线 | 常驻 node 没在跑：`systemctl --user status jarvisd-node`，日志在 `~/.jarvis/node-attach.log` |

日志：
- helper / Linux 的服务：`~/.jarvis/link.log`（`jarvis-link logs -f`）；被动模式是 `passive: …`（开始监听、配对、流开始 / 结束、被拒的请求）；
- hub：`[<机器>] link up …` / `link down …`。

## 被动模式和 node 服务是怎么验证的（2026-11-02）

- Go 测试（`server/internal/link/listen_test.go`、`server/cmd/jarvis-link/passive_test.go`）里有一个按第 4 节行为的假 hub：钉扎链接里的指纹（别的指纹握不了手）、
  用码配对、带凭据开流，连到临时目录里的假 node（回声）；验证 300 KiB 随机字节两个方向原样、错凭据 401、限流后连对的凭据也 429、码只能用一次、过期、
  其他路径 404、重新配对后旧凭据 401 且旧流以 4002 关掉、node 不在时 503、hub 不回 pong 时连接器断流、端口被占时报错重试、主动 / 被动来回切换。
  主动模式原来的测试都照过。
- 真二进制（临时 HOME、只听 127.0.0.1）：`jarvis-link pair --listen` + `jarvis-link run` + 真的 `jarvisd node --attach`，假 hub 配对后在流上发 node 协议的
  `ping`、`node.info`，常驻 node 都回答了。
- 干净的 Debian 12（arm64）容器、有 systemd：`curl … install.sh | sh -s -- --node --listen`（`docker exec -u`，没有 `XDG_RUNTIME_DIR`），两个用户服务都 active、
  打印了链接；宿主上的假 hub 经映射到 127.0.0.1 的端口配对、和容器里的真 node 说上话；重启容器后两个服务自己起来，原凭据照样能用。
  第一次跑时容器里唯一的网卡是虚拟的，没有候选地址、装不下去，于是有了「全是虚拟网卡时退一步」。
- `jarvis-link discover` 在局域网里听到了一个临时广播的假 hub（mDNS，测完就停了）。
- Mac 菜单栏和配对窗口的被动模式截图（深浅色，`mac-link.sh snapshot`）。App 第一次运行装常驻 node 的那段**没在真机上跑过**：这台开发机上有真实在用的
  `com.jarvis.node`，不能碰；`node install` 在 Mac 上的命令序列（`launchctl bootout` / `bootstrap`）只在假命令里测过，plist 过了 `plutil -lint`。
- 没测：真的 hub 拨号侧（#435 在做）、经 SOCKS 代理、Windows（只编译）、公网 IPv6 入站。
