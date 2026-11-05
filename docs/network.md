# 公网直连 hub

手机经公网直连 hub，优先使用 IPv6；VPN 由用户自行配置。hub 提供独立的 TLS 监听，保留 HTTP 给内网和旧客户端。**公网只放行 TLS 端口**，客户端在发送 token 前必须确认使用 HTTPS / WSS。

本文以 `server/internal/hub/` 和 `server/internal/netaddr/` 的现有实现为准。现有 iOS / Watch 尚不能据此获得自签证书钉扎能力，仍使用原内网 / VPN 地址。DDNS 和 doctor 的独立实现见下文说明。直连都不通时，iOS / macOS App 还可以[经 SSH 隧道连 hub](#经-ssh-隧道连-hub直连不通时的后备)。

## hub.json 网络配置

在 hub 机器的 `~/.persona/hub.json` 合并以下字段，保留已有 token、数据库和机器配置，再重启 hub：

```json
{
  "listen": ":18660",
  "public_url": "http://hub.example.com:18660",
  "public": {
    "tls_listen": ":18662",
    "advertise": ["https://hub.example.com:18662"],
    "advertise_ipv6": true,
    "advertise_lan": true
  },
  "security": {
    "trusted_cidrs": [],
    "allow_public_plaintext": false
  }
}
```

域名都是示例，需要换成自己的配置。HTTP 地址应在内网 / VPN 可达；`18662` 只是本文选用的 TLS 示例端口，没有内置 TLS 默认端口。避免与 HTTP 和 Postgres 端口冲突。修改配置后需要重启，证书文件的刷新行为另见下节。

| 字段 | 默认值及行为 |
| --- | --- |
| `listen` | HTTP 默认 `:18660`；保留给内网 / VPN。`hub pair` 还需要它接受 IPv4 回环连接 |
| `public_url` | 二维码的 `url`，同时加入候选列表；保留旧客户端能访问的地址 |
| `public.tls_listen` | 空字符串，不开启 TLS；填写后增加独立的 HTTPS / WSS 监听 |
| `public.cert_file` / `public.key_file` | 均为空时自动管理自签证书；自带证书时必须成对填写文件路径，建议用绝对路径 |
| `public.advertise` | 额外候选列表；推荐填写完整 HTTPS URL，也接受域名、IP 或带端口的主机地址；省略协议 / 端口时按当前监听补齐。发布地址不会开监听、放行防火墙或更新 DNS |
| `public.advertise_ipv6` | 未设置或 `true` 时自动发布本机全局 IPv6 候选 |
| `public.advertise_lan` | 未设置或 `true` 时自动发布内网 IPv4 / ULA 候选；这些地址仅在鉴权后下发 |
| `public.ddns` | 不写不开启；用域名找回换了前缀的 hub，见 [配置](config.md#ddnspublicddns) |

两个监听共用请求头超时、空闲连接超时和请求头大小限制，不设置覆盖整个请求的读写超时，以免截断 WebSocket 和大文件传输。

两个 `advertise_*` 开关只控制自动发现，不能过滤显式配置的 `public_url` / `public.advertise`。TLS 开启后，自动发现的候选使用 HTTPS 和 TLS 端口；显式 HTTP URL 仍保持原样。

以下数值都是 `config_security.go` 的策略默认值，**未实测，不代表吞吐、性能或防护能力**。整数配置为零或负数时采用默认值。

| `security` 字段 | 默认值 | 含义 |
| --- | --- | --- |
| `trusted_cidrs` | `[]` | 额外信任的来源网段；命中后按内网处理，只填确实由自己控制的网络 |
| `allow_public_plaintext` | `false` | 开启后允许公网明文 API，启动时记录警告；不会解除主 token 的公网禁用规则 |
| `auth_failures` | `10` | 一个计数窗口内累计失败达到此值后封禁来源 |
| `auth_window_seconds` | `60` | 从该来源本轮首次失败起计的窗口，单位秒 |
| `auth_block_seconds` | `900` | 达到阈值后的封禁时长，单位秒 |
| `auth_max_sources` | `4096` | 来源计数表的条目上限；满后清理过期条目，再优先淘汰未封禁且最早到期的条目 |
| `auth_cache_entries` | `2048` | 设备 token 查询缓存的条目上限，包括无效 token；缓存有效期由代码固定为 30 秒 |
| `auth_log_interval_seconds` | `30` | 安全错误日志最短间隔，省略的日志数汇总到 `suppressed` |
| `proxy_protocol` | 不写，不读任何 PROXY 头 | hub 前面是同机端口转发 / TCP 透传时，让 hub 按真实来源判断，见下面「[PROXY protocol](#proxy-protocol)」 |

公网 IPv4 按单个地址计数，IPv6 按 /64 聚合。封禁检查在 token 查库前进行，已封禁来源即使携带有效 token 也会收到 `429` 和秒数形式的 `Retry-After`。触发阈值的那次鉴权失败仍返回 `401`，后续请求才返回 `429`。内网 / VPN 来源不计数。

## TLS 与公钥钉扎

未指定证书文件时，hub 在 `~/.persona/tls/` 创建 ECDSA P-256 身份密钥 `hub.key` 和自签证书 `hub.crt`。目录权限 0700，文件权限 0600。证书只带 `<hub_id>.jarvis.invalid`，不带本机主机名或内网地址。TLS 最低版本是 1.2。

自动证书有效期为 365 天，剩余不足约 30 天时以同一把密钥重签；证书尚未生效或 SAN 不符时也会重签。hub 的证书刷新定时器设为 2 分钟，属于调度配置，**未实测实际刷新延迟**。自带证书会重新读取，但 hub 不替它申请或续签；当前没有自动 ACME 流程。

在 hub 机器运行：

```sh
~/.persona/bin/personad hub pair
```

TLS 已开启时，输出中的「公钥指纹」就是钉扎值 `fp`：证书 SubjectPublicKeyInfo DER 的 SHA-256，再用不带填充的 base64url 编码，长度 43 字符。二维码也携带它。`hub pair` 会生成一个新临时 token，链接和二维码都应当按凭据保管；它不是只读查看指纹的命令。

支持钉扎的客户端通过可信扫码获得 `fp`，在 TLS 握手中核对公钥，并核对 `hub_id`。已配对客户端也可从可信、已鉴权的内网通道读取 `hello` 或 `/api/v1/endpoints`。**公网免鉴权 health 返回的 `fp` 不能用来建立首次信任。**

换证书而保留同一把私钥，钉扎值不变。换私钥会改变钉扎值，所有已钉扎设备都需要重新配对；运行中检测到密钥变化会拒绝替换并记日志，需要重启 hub。不要把删掉 `tls/` 当作普通的证书续期步骤。当前没有 `fp_next` 预告或无感密钥轮换。

默认配对 token 有效期为 10 分钟，只能兑换一次设备 token；兑换后最多再保留 2 分钟，且不超过原到期时间。这是兼容旧 iOS 配对流程的策略，详见 [协议](protocol.md#临时配对)。`hub pair --legacy` 才打印主 token；公网始终拒绝主 token。

电脑链路（jarvis-link，[link.md](link.md)）也只走这个 TLS 端口，同样按配对时拿到的指纹钉扎，内网也不例外；hub 不收它的明文请求（回环除外）。

## 候选地址与 DDNS

候选依次按 `ipv6`、`name`、`lan`、`ipv4` 排列，同类中 HTTPS 在前。Linux 通过 netlink 读取地址标志和生命周期，正常情况下排除临时、废弃、tentative、DAD 失败和已过期的全局 IPv6，以及虚拟网卡、链路本地和文档前缀。ULA 归入 `lan`。公网 IPv4 只从显式配置发布，当前没有自动探测 NAT 或验证公网 IPv4 入站。

非 Linux 平台缺少地址标志，或者 netaddr 读取失败退回普通网卡枚举时，hub 无法保证过滤掉临时 IPv6。`netaddr.Stable` 本身会拒绝未知标志，但 hub 的降级枚举允许这些地址进入候选，不能把它们写成「已验证的稳定地址」。

地址变化由监听通知触发重扫，并有周期重扫兜底。候选只说明本机发现或配置了地址，**不说明公网能连通**。`seen_at` 是本次采集时间，不是连通测试时间。客户端经已鉴权的 `/api/v1/endpoints`、WS `hello` 获取列表，只有声明 `endpoints` 能力的连接会收到地址变化事件。

拨号重连可能改变 IPv6 前缀，旧连接和旧字面地址随之失效。可用域名找回 hub：`public.ddns` 让 hub 自己更新 DNS（见 [配置](config.md#ddnspublicddns)），也可以用外部 DDNS 工具维护 AAAA 记录。两种做法都要把带 TLS 端口的 HTTPS 域名 URL 写进 `public.advertise`，App 才拿得到这个候选；只填 `advertise` 不会更新 DNS。

### 客户端手动候选

hub 发布的候选可能不全：自动发现会漏掉被当成虚拟网卡的物理网卡上的地址，也可能还没配 `public.advertise`。客户端可以自己补候选地址，规则如下：

- 手动候选只存在客户端，按配对保存；hub 下发新的候选列表时，只替换 hub 给的那部分，不覆盖手动候选，也不覆盖客户端的停用设置和用户排的顺序。
- 客户端可以让用户给候选排序，并且照排好的顺序尝试：显示的顺序就是尝试的顺序，不按网络记忆、不暗中调整。hub 新增的候选排到最后，hub 去掉的候选从列表里消失。
- 安全要求和 hub 下发的候选完全一样：HTTPS 候选按可信配对时得到的公钥指纹钉扎，并核对 `hub_id`；没有指纹的配对（hub 自己不开 TLS、在反向代理后面）HTTPS 候选按系统 CA 和主机名校验，见下文「反向代理的子路径（Android）」。明文 HTTP 只用于内网地址，客户端在发出 token 前按实际连接的对端地址再判断一次。
- 手动候选不说明可达，和 `seen_at` 一样不是连通测试；客户端可以用免鉴权的 `GET /api/v1/health` 探测，但只把它当作「这个地址能到达已钉扎的那台 hub」的证据，不从回包里取指纹建立信任。

Android 的做法见 [android.md](android.md#连接地址设置--连接诊断)，电脑链路见 [link.md](link.md)。

## IPv6 入站放行

先确认 hub 的物理网卡有全局 IPv6，并保留一个适合长期发布的稳定地址。不要把隐私临时地址当成固定服务地址；接口标识优先用随机生成的稳定值，避免容易猜测的固定后缀。

光猫和路由器的菜单因型号不同，通常叫「IPv6 防火墙」「入站规则」或「IPv6 访问控制」。按实际拓扑逐层配置：

1. 光猫负责路由时，为 hub 的全局 IPv6 增加 **TCP 目标端口 = TLS 端口** 的入站允许规则；如后面还有路由器防火墙，两层都要允许。光猫桥接时，重点检查拨号路由器的规则。
2. 规则目标选 hub 设备或它的稳定 IPv6，只放示例中的 TCP 18662。IPv6 直连通常用防火墙放行，不照搬 IPv4 的 NAT 端口映射。前缀改变后，检查规则是否仍指向当前地址。
3. 保持其他入站端口关闭，不开放 HTTP 18660、Postgres 18661 或其他管理服务。如果设备只能整体关闭 IPv6 防火墙，先确认家庭中各主机都有入站保护；不能只保护 hub 就认为整个网络安全。
4. hub 的主机防火墙也要允许该 TCP TLS 端口，公网方向保留默认拒绝策略；SSH 管理和内网 HTTP 按既有可信来源规则保留。核查 IPv6 规则确实生效，不要只检查 IPv4。

Docker 发布端口时还要检查容器相关的转发规则，不能只凭 ufw 的入站列表判断端口没有暴露。数据库应继续仅绑定回环。修改防火墙时保留可用的管理连接。

放行后用手机关闭 Wi-Fi、切到蜂窝网络测试，并确认测试网络有 IPv6；在 hub 上访问它自己的地址走本地路径，不能证明光猫已放行。本文未做公网可达性实测，也未测延迟或运营商空闲连接保留时间。

## 来源判断与反向代理

hub 只信任连接的来源地址和本地接收地址，**不读取 `X-Forwarded-For` 或 `Forwarded` 来决定信任**；唯一的例外是配置里指定的上游发来的 PROXY 头（见下面「[PROXY protocol](#proxy-protocol)」）。回环、私网 / ULA、链路本地、与本机接口同链路的 IPv6 前缀（/64 或更长），以及 `trusted_cidrs` 命中来源，按本地网络处理。共享运营商 IPv4 地址空间不会自动受信任，只有落在本机 VPN 接口上的连接或显式白名单才算内网。

**容器网关例外**：hub 跑在容器里时，本容器的网关地址（默认路由的下一跳、各网段的第一个地址，`server/internal/hub/admin_gateway.go`）不算本地网络，按公网处理，除非写进 `trusted_cidrs`。Docker 的用户态代理（docker-proxy）可能把来自内网和公网的连接都显示成网关地址，不排除的话公网请求能用主 token、走明文。Linux 上经 iptables DNAT 进来的局域网电脑保留真实地址，不受影响；`--network host` 部署也不受影响。

**Docker Desktop / OrbStack 的转发来源**不保证是网关：Docker Desktop 的 gvisor 网络可能让本机、局域网客户端都显示成同一个公网形态的地址。hub 仍按实际对端判断，所以明文 API 会回 `403 tls_required`；仅把 `docker network inspect` 中的网关加入白名单不一定有效。客户端连接时，用 `docker compose exec hub netstat -tn` 查看容器 18660 端口对应的远端地址，按 [Docker 排查说明](docker.md#docker-desktop--orbstack-的明文连接) 配置。优先使用 hub 的 TLS；仅本机试用时，先把端口绑定到 `127.0.0.1`，再把观测到的转发来源以单地址 CIDR 写进 `security.trusted_cidrs`。该来源可能代表所有经映射进来的客户端，白名单不会还原原始客户端身份。

本地反向代理终止 TLS 后再连 hub，会让来源变成回环；远端代理也会让来源变成代理地址。这会影响公网明文限制、主 token 限制、health 裁剪和限流。增加转发头不能恢复这些检查。若使用反向代理，需要由代理承担公网 TLS、访问控制和限流，并阻止公网借代理访问仅限本机的配对码接口；优先直接使用 hub 自带 TLS 监听。不终止 TLS、只做 TCP 转发的（端口转发、内网穿透、nginx `stream` 按 SNI 透传到 TLS 端口）可以开 PROXY protocol，把真实来源告诉 hub。

### PROXY protocol

**风险**：hub 前面有一层**同机**的 TCP 转发时（例如内网穿透把公网端口转到本机，再经 nginx `stream` 按 SNI 透传到 hub 的 TLS 端口），hub 看到的每条公网连接都来自 `127.0.0.1`，一律按本机处理：

- 管理界面不受 `admin.access` 限制（回环总在范围内），配了 `cidrs` 公网也打得开；
- 主 token 从公网可用；
- 鉴权失败只对公网来源计数，于是公网猜 token / 管理密码不限流；
- 日志里的来源全是 `127.0.0.0/24`，看不出是谁。

TLS 透传加不了 HTTP 头，转发头也不该用来放行。PROXY protocol（[规范](https://www.haproxy.org/download/2.9/doc/proxy-protocol.txt)）是转发方在连接最前面、TLS 握手之前写的一行（v1 文本）或一小段（v2 二进制）头，说明它替谁转发。hub 支持 v1 和 v2，只认配置里信任的上游：

```json
{
  "security": {
    "proxy_protocol": {
      "from": ["127.0.0.1/32"],
      "listeners": ["tls"]
    }
  }
}
```

| 字段 | 默认值 | 含义 |
| --- | --- | --- |
| `from` | 必填 | 信任的上游，CIDR 或单个地址，写 hub 看到的上游来源地址（同机一般是 `127.0.0.1/32`）。只写代理自己的地址；`0.0.0.0/0`、`::/0` 会被拒绝 |
| `listeners` | `["tls"]` | 作用于哪些监听：`tls`（`public.tls_listen`）、`http`（`listen`）。`from` 含回环时不能加 `http`：本机的 `personad hub pair`、`hub admin`、插件都经回环连明文端口，不带头，会被拒绝 |

不写 `proxy_protocol` 时，hub 不读任何 PROXY 头，行为和以前完全一样。配置有误（`from` 为空或不是地址、未知的 `listeners`、`http` 配回环）时 hub 拒绝启动，`personad hub doctor` 会指出是哪一项；只作用于 TLS 却没开 `public.tls_listen` 时只在日志里提示。改完要重启 hub。

**规则**（`server/internal/proxyproto`）：

- **来自 `from` 的连接必须带合法的头**，hub 把头里的源地址当作这条连接的对端：后面的来源判断、管理界面范围、主 token 限制、限流和日志全都按真实来源。没有头、头格式不对或超时的连接直接关闭，不回任何东西：否则绕过代理直连（或者代理没开 PROXY protocol）的连接又会变成回环。
- **别的来源发来的 PROXY 头不认**：hub 根本不读，这些字节就是连接的开头，TLS 握手失败或 HTTP 回 400。现有客户端完全不受影响，不在 `from` 里的人也没法冒充任何地址。
- **v2 的 `LOCAL`**（代理自己发起的连接，例如健康检查）按规范用连接本身的地址，也就是上游的地址。
- **v1 的 `UNKNOWN`、v2 `PROXY` 命令配 `UNSPEC` 地址族**（上游不知道替谁转发，比如它自己是在 Unix socket 上收到的连接）一律拒绝。规范允许接收方改用连接本身的地址，但那正是上游的回环地址，等于把转发来的连接当成本机。v2 的 UDP、Unix socket 地址族也拒绝。
- **头里的源地址是回环、上游却不是回环**（上游在别的机器上，替它自己本机的程序转发）时，用上游的地址，不把那台机器的「本机」当成 hub 的本机。
- **超时和长度**：头要在 5 秒内到齐（策略默认值，未实测）；v1 最长 107 字节（规范的上限），v2 地址加 TLV 最多 4096 字节，TLV 读过即弃。头在这条连接自己的 goroutine 里、第一次读写或取对端地址时读，不占用 Accept：一个慢的或不说话的连接不会卡住别的连接。
- **本地地址仍用 socket 的**，不用头里的目的地址：目的地址在上游那边（代理的监听地址，可能在另一台机器上），而 hub 用本地地址判断连接是不是从本机的 VPN 网卡进来的（`100.64.0.0/10` 的判断）。经代理来的共享地址空间来源因此按公网处理，只会更严，不会更松；需要时写进 `trusted_cidrs`。
- systemd 的 `jarvisd.socket` 传进来的监听和 hub 自己开的监听一样套上。

**一路传下来**：上游写进头里的，是它看到的来源。上游就是公网入口（直接收公网连接）时，只要让它连 hub 时发 PROXY 头，nginx `stream` 是 `proxy_protocol on;`。前面还有一层转发（内网穿透的客户端、另一个端口转发）连到上游时，上游看到的来源又是回环，这一层也要发 PROXY 头，上游收下并信任它再往下传。nginx `stream` 的写法（需要 `ngx_stream_realip_module`，`nginx -V` 里有 `--with-stream_realip_module`；地址和端口都是示例）：

```nginx
stream {
    map $ssl_preread_server_name $backend {
        hub.example.com 127.0.0.1:18662;   # hub 的 TLS 端口
        default         127.0.0.1:8444;
    }
    server {
        listen 127.0.0.1:8443 proxy_protocol;  # 前面那层转发也发 PROXY 头；nginx 自己就是入口时去掉 proxy_protocol
        set_real_ip_from 127.0.0.1;            # 只信任前面那层发来的地址；nginx 自己就是入口时不要这行
        ssl_preread on;
        proxy_pass $backend;
        proxy_protocol on;                     # 把客户端地址传给 hub
    }
}
```

前面那层怎么开 PROXY protocol 看它自己的文档（常见的名字是「proxy protocol」「send-proxy」，有 v1 / v2 可选，hub 两种都认）。`map` 里分给别的服务的后端也会收到头：它们不认 PROXY protocol 的话，给它们单独一个不带 `proxy_protocol on;` 的 `server`。

**配好以后自查**：

1. 重启 hub，日志里有一行 `hub: PROXY protocol on the TLS listener, from 127.0.0.1/32`；`personad hub doctor` 里 `security.proxy_protocol` 是「正常」。
2. 用手机蜂窝网络从公网连一次。鉴权失败、管理界面登录等日志里的来源不应再是 `127.0.0.0/24`；配了 `admin.access: cidrs` 时，公网打开 `/admin` 看到的是普通网页。
3. 日志出现 `proxy protocol: refused a connection from …`（一分钟最多一行，带累计数）：那个上游的连接没带头，查 `proxy_protocol on;` 有没有生效，或者有别的东西绕过代理直连 TLS 端口。
4. 日志出现 `passed on a connection from loopback`（只打一次）：头里的来源仍是回环，前面那层没把地址传下来，按上面「一路传下来」补上。

**注意**：开了以后，`from` 里的地址连这个端口都必须带头。本机其他程序直连 TLS 端口（例如连 `https://127.0.0.1:<TLS 端口>`）会被拒绝，改连明文端口或局域网地址。hub 在 Docker 里、经用户态代理（docker-proxy）映射端口时，所有映射进来的连接都显示成网关地址，把网关写进 `from` 就意味着这个端口只能经代理访问。

### 反向代理的子路径（Android）

hub 可以放在反向代理的子路径下（`public_url` 写成 `https://<hub 地址>/<前缀>`，代理去掉前缀再转给 hub 的明文端口）。这时 hub 没有自己的 TLS，`hub pair` 给出的链接里没有 `fp`，证书来自代理上的公共 CA。iOS 和网页版本来就按系统信任校验 HTTPS；Android 的做法：

- **地址带前缀**：配对地址、hub 下发的候选可以带路径前缀，所有请求（REST、WebSocket `/api/v1/ws`、媒体和安装包下载）都拼在前缀下面（`core/hub` 的 `hubUrl` / `hubPath` / `hubResolve`），hub 回的 `/api/…` 引用也补上前缀；同源检查同时要求在这个前缀之内。候选的键带上前缀（`https://host:443/jarvis`），根路径的键和以前一样。手动候选仍只能写到端口为止。
- **没有 `fp` 的 HTTPS 用系统信任**：有 `fp` 的配对照旧只认钉住的公钥，不回落到系统 CA；没有 `fp` 的配对，HTTPS 按系统信任库（`network_security.xml`：只信系统 CA，不信用户装的证书）和主机名校验，配对和之后的常驻连接、设置里的测试、更新下载都一样。
- **取舍**：钉扎只信一把公钥；系统信任则信任系统里的所有 CA，任何一家给这个域名签发证书都能冒充 hub（被入侵或误签的 CA、能装系统证书的设备管理）。这和浏览器访问网页版一样，比明文好、比钉扎弱；能用 hub 自带 TLS 和 `fp` 时优先用它。配对码只在扫码 / 粘贴时出现，hub 的身份仍按 `hub_id` 核对。

配对失败时，Android 的「连接诊断」会显示最近一次配对：试的地址、停在哪一步（解析链接 / 连接地址 / TLS 证书 / HTTP 请求 / 配对码校验 / hub 的响应）、错误文字、时间和每个地址的探测结果；只存在内存里，不含配对码。

公网明文 API 默认返回 `403 tls_required`，仅 `GET /api/v1/health` 放行。这个拒绝发生在请求到达之后，不能撤回已经明文发出的 token；客户端必须在发出请求前选对协议。

## 经 SSH 隧道连 hub（直连不通时的后备）

iOS 和 macOS App 可以经一台 SSH 服务器连 hub（#891）：设备上没有能到达 hub 的 VPN、hub 也没有公网入口，但手里有一个能从外面登录的 SSH 入口（跳板机、Warpgate 这类堡垒机、自己的服务器）时用。hub 不用改。它是 [#868](https://github.com/nwverse/persona/issues/868) 的直连 / 内嵌 Tailscale / 内嵌 tailcat 之外的另一种连法。

```text
App ──HTTP / WebSocket──▶ 127.0.0.1:<随机端口>（App 进程里）
     ══SSH（direct-tcpip，一个连接一个通道）══▶ SSH 服务器 ──TCP──▶ hub（隧道目标，默认 127.0.0.1:18660）
```

- **进程内实现**（`apple/Packages/PersonaSSH`，swift-nio-ssh + Network.framework）：不调系统 ssh。TestFlight 的 Mac App 在沙盒里，调不了 `/usr/bin/ssh`，也读不到 `~/.ssh`。App 在本机回环的一个端口上监听（下次优先用同一个端口），每个连进来的连接开一个 `direct-tcpip` 通道，HTTP 和 WebSocket 都走它；一台 hub 一条 SSH 连接。
- **App 里不改地址**：配对、候选、比较用的仍是 hub 自己的地址；只有发出去的请求（`APIClient`、WebSocket、端口转发）在 `HubTransport` 里换成隧道的回环地址。
- **密钥**：App 自己生成 Ed25519 密钥，一台设备一把，私钥只存这台设备的钥匙串（`ThisDeviceOnly`：不同步、不随备份恢复到别的设备、不进日志）。
- **选路**：每次（重新）连接先按原来的方式直连（配对地址和组里的候选，`GET /api/v1/health`，3 秒）；都不应答才走 SSH 隧道。走隧道期间每 2 分钟再问一次直连，通了就换回来、关掉隧道。打开「总是走 SSH」则先走隧道，隧道起不来再试直连。
- **断线**：SSH 连接断了，下一个请求重新拨号；拨号失败按 1、2、4…最多 30 秒退避，退避期间的请求立刻失败（socket 自己也有退避）。TCP keepalive（20 秒空闲后每 5 秒探一次，3 次）发现无声断开的连接，网络说连接不可用时立刻关掉重拨；一个通道 10 秒打不开就当这条 SSH 连接已经死了。
- **iOS 进后台**：隧道和 socket 一起关；回到前台重新选路、重建（模拟器实测约 1 秒）。静默推送唤醒时也会按需建隧道。
- **手表**不支持：手表自己直连 hub，跑不了隧道；hub 只能经 SSH 到达时，手表上直接拉数据的功能不可用，推送不受影响（推送不经过这条连接）。网页版、Android 暂不支持。

### 设置

App 里：设置 › hub › SSH 隧道（每台配对的 hub 各一份）；还没配对时在配对页点「经 SSH 隧道连接」。

| 项 | 说明 |
| --- | --- |
| 主机、端口 | SSH 服务器，端口默认 22 |
| 用户名 | 原样发给服务器；Warpgate 的目标登录写成 `user:target` |
| 隧道目标 | 从 SSH 服务器上看 hub 的地址，默认 `127.0.0.1:18660`（hub 就在这台服务器上） |
| 总是走 SSH | 关：直连不通才走；开：每次都走 |

「复制 authorized_keys 行」拷出的是带限制的一行，贴到服务器上该用户的 `~/.ssh/authorized_keys`：

```text
restrict,port-forwarding,permitopen="127.0.0.1:18660" ssh-ed25519 AAAA… persona-<设备名>
```

`restrict` 关掉 shell、pty、agent / X11 转发和 `~/.ssh/rc`，`port-forwarding` 只把端口转发加回来，`permitopen` 只许转发到隧道目标。服务器的 `sshd_config` 里 `AllowTcpForwarding` 不能是 `no`，也不能设 `DisableForwarding yes`（它们优先于 authorized_keys 的选项）。专门开一个受限用户更好：`Match User <用户>` 里再加 `PermitOpen 127.0.0.1:18660`、`X11Forwarding no`、`PermitTTY no`、`ForceCommand /usr/bin/false`（不影响 `-N` 式的转发）。

「测试连接并保存」会登录一次并打开一次到隧道目标的转发，成功才保存。

### 主机密钥

第一次连接时 App 把服务器的指纹（`ED25519 SHA256:…`，与 `ssh-keygen -lf <主机公钥>` 打印的一致）弹给用户确认，确认后钉住。之后指纹变了一律拒绝连接，设置页和连接详情写「SSH 服务器的指纹变了，已拒绝连接」；用户在「测试连接并保存」弹出的新旧指纹对比里点「信任新指纹」才会接受，不会静默换掉。换了 SSH 服务器（主机或端口）要重新确认。

支持的算法（swift-nio-ssh）：密钥交换 `curve25519-sha256`、`ecdh-sha2-nistp256/384/521`；主机密钥 Ed25519、ECDSA（**不支持 RSA 主机密钥**）；加密 AES-128-GCM、AES-256-GCM（OpenSSH 的 aes128-gcm、aes256-gcm）。现在的 OpenSSH 默认都有。

### 安全说明

- **隧道终点在 SSH 服务器上**：hub 看到的来源是 SSH 服务器（hub 就在这台服务器上时是回环，否则是它的内网地址），**按内网处理**：可以用主 token、走明文 HTTP、拿到 health 的完整字段（[来源判断](#来源判断与反向代理)）。
- 这在 SSH 已经鉴权的前提下可以接受：能到达 hub 的只有持有这台设备私钥、并被服务器放行的人，SSH 段全程加密，App 还钉住了服务器的主机密钥。但前提是**服务器上那把 key 必须用 `permitopen` 限制成只能转发到 hub 的端口**：不加限制的 key 能把这台服务器当跳板打开内网里的任何地址、开 shell。App 给的 authorized_keys 行已经带好了，不要只贴「复制公钥」那一行。
- 隧道段是明文 HTTP 的只有两处：设备自己的回环（只在 App 进程和本机之间），以及 SSH 服务器到 hub 这一跳（隧道目标是回环时不出这台机器；是内网地址时走内网，和内网直连一样）。
- 丢了设备：在服务器的 authorized_keys 里删掉 `persona-<设备名>` 那一行，并在 设置 › 设备 撤销它的 hub 凭据。

### 排查

| 现象（设置页 / 连接详情） | 检查方向 |
| --- | --- |
| 连不上 SSH 服务器 | 主机、端口；这台设备能不能访问它（换网络试） |
| 第一次连这台 SSH 服务器，需要先确认它的指纹 | 去 设置 › hub › SSH 隧道点「测试连接并保存」确认 |
| SSH 服务器的指纹变了，已拒绝连接 | 在服务器上核对 `ssh-keygen -lf`；确认是服务器换了密钥再信任，否则不要连 |
| SSH 服务器没有接受这台设备的公钥 | authorized_keys 那一行加到了对的用户下；文件权限（`StrictModes`）；Warpgate 里把公钥加给这个用户 |
| SSH 服务器打不开到 hub 的转发（administratively prohibited） | `permitopen` 的地址和隧道目标不一致，或服务器禁止了 TCP 转发 |
| SSH 服务器打不开到 hub 的转发（connect failed） | 从 SSH 服务器上看隧道目标不对，或 hub 没在那个端口上监听 |
| SSH 握手失败（没有双方都支持的算法） | 服务器只有 RSA 主机密钥或只开了 chacha20：加一把 Ed25519 主机密钥、放开 AES-GCM |

## 网络排查

先在 hub 机器上跑 `personad hub doctor`。它只读不改：检查 hub.json 的明文 `listen`、`public.tls_listen`、`security.allow_public_plaintext` 和 TLS 证书，向正在运行的 hub 要候选地址，列出本机每个 IPv6 是可发布还是被排除（附原因），在 Linux 上看去往公网 IPv6 的路由是否走 TUN / 虚拟网卡、读 ufw / nft 规则，最后给出下一步：只放行哪个 TLS 端口、用手机蜂窝网络打开哪个地址测一次。它**不做外部可达性探测**：本机连自己的地址走环回，证明不了公网能进来。

TUN 代理开启自动路由后，公网连接的回包可能被送进代理，表现为外部连接超时或握手中断。**此风险未实测**。在获准排查的 hub 上查看到手机来源地址的 IPv6 路由，结合物理网卡和 TUN 网卡抓包判断：请求是否到达、回复是否从正确网卡出去。可临时停用 TUN 做对照；若确认有关，在代理 / 策略路由中为入站连接的回复保留直连路径，不要靠给 hub 加 `trusted_cidrs` 修复回程路由。

| 现象 | 检查方向 |
| --- | --- |
| 同网能通，蜂窝超时 | 手机是否有 IPv6；光猫、路由器、主机的入站规则；发布地址是否已过期；TUN 回程 |
| `403 tls_required` | 改用 TLS 端口和 HTTPS / WSS，不把 token 发往公网 HTTP |
| `403 master_token_local_only` | 公网改用配对临时 token 或设备 token |
| `429 rate_limited` | 按 `Retry-After` 等待，并停止用失效 token 反复重连 |
| 公钥不匹配 | 核对是否换过密钥、连错 hub 或经过了终止 TLS 的代理；重新可信配对，不跳过钉扎 |
| `hub pair` 连不上本机 | hub 是否已运行，HTTP 监听是否支持 IPv4 回环，命令所读配置是否和运行实例一致 |
| 开了 `security.proxy_protocol` 后 TLS 端口连不上 | hub 日志里的 `proxy protocol: refused …`：上游没发 PROXY 头，或有东西绕过它直连（[PROXY protocol](#proxy-protocol)） |

## 命令行验证

自签证书需要钉住可信公钥后再调用 API。curl 的 `sha256//` 使用标准 base64，不能直接贴二维码的 base64url 值。把可信扫码 / 本机 `hub pair` 得到的指纹放入 `FP`，设备 token 放入 `TOKEN`，在自己的外部测试机器执行：

```sh
PIN=$(printf '%s' "$FP" | tr '_-' '/+')=
curl --pinnedpubkey "sha256//$PIN" --insecure \
  -H "Authorization: Bearer $TOKEN" \
  https://hub.example.com:18662/api/v1/endpoints
```

这里 `--insecure` 跳过系统 CA / 主机名校验，`--pinnedpubkey` 仍核对公钥；两者必须一起使用。不要从同一次未验证的公网请求获取 `FP`。IPv6 字面 URL 需要方括号，例如 `https://[2001:db8::1234]:18662`（文档地址，不能实际连接）。响应字段见 [协议](protocol.md#候选地址)。
