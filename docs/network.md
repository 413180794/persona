# 公网直连 hub

手机经公网直连 hub，优先使用 IPv6；VPN 由用户自行配置。hub 提供独立的 TLS 监听，保留 HTTP 给内网和旧客户端。**公网只放行 TLS 端口**，客户端在发送 token 前必须确认使用 HTTPS / WSS。

本文以 `server/internal/hub/` 和 `server/internal/netaddr/` 的现有实现为准。现有 iOS / Watch 尚不能据此获得自签证书钉扎能力，仍使用原内网 / VPN 地址。DDNS 和 doctor 的独立实现见下文说明。

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
- 安全要求和 hub 下发的候选完全一样：HTTPS 候选按可信配对时得到的公钥指纹钉扎，并核对 `hub_id`；没有指纹的配对不能用 HTTPS 候选。明文 HTTP 只用于内网地址，客户端在发出 token 前按实际连接的对端地址再判断一次。
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

hub 只信任连接的来源地址和本地接收地址，**不读取 `X-Forwarded-For` 或 `Forwarded` 来决定信任**。回环、私网 / ULA、链路本地、与本机接口同链路的 IPv6 前缀（/64 或更长），以及 `trusted_cidrs` 命中来源，按本地网络处理。共享运营商 IPv4 地址空间不会自动受信任，只有落在本机 VPN 接口上的连接或显式白名单才算内网。

**容器网关例外**：hub 跑在容器里时，本容器的网关地址（默认路由的下一跳、各网段的第一个地址，`server/internal/hub/admin_gateway.go`）不算本地网络，按公网处理，除非写进 `trusted_cidrs`。Docker 的用户态代理（docker-proxy）把转进来的连接一律显示成网关地址，公网的也一样（Docker 没开 IPv6 时的 IPv6 请求；Docker Desktop、OrbStack 上是所有连接），不排除的话公网请求能用主 token、走明文。Linux 上经 iptables DNAT 进来的局域网电脑保留真实地址，不受影响；`--network host` 部署也不受影响。Docker Desktop 上要让本机明文连，把网关写进 `trusted_cidrs`（只在端口只映射到 127.0.0.1 时安全）。

本地反向代理终止 TLS 后再连 hub，会让来源变成回环；远端代理也会让来源变成代理地址。这会影响公网明文限制、主 token 限制、health 裁剪和限流。增加转发头不能恢复这些检查。若使用反向代理，需要由代理承担公网 TLS、访问控制和限流，并阻止公网借代理访问仅限本机的配对码接口；优先直接使用 hub 自带 TLS 监听。

公网明文 API 默认返回 `403 tls_required`，仅 `GET /api/v1/health` 放行。这个拒绝发生在请求到达之后，不能撤回已经明文发出的 token；客户端必须在发出请求前选对协议。

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

## 命令行验证

自签证书需要钉住可信公钥后再调用 API。curl 的 `sha256//` 使用标准 base64，不能直接贴二维码的 base64url 值。把可信扫码 / 本机 `hub pair` 得到的指纹放入 `FP`，设备 token 放入 `TOKEN`，在自己的外部测试机器执行：

```sh
PIN=$(printf '%s' "$FP" | tr '_-' '/+')=
curl --pinnedpubkey "sha256//$PIN" --insecure \
  -H "Authorization: Bearer $TOKEN" \
  https://hub.example.com:18662/api/v1/endpoints
```

这里 `--insecure` 跳过系统 CA / 主机名校验，`--pinnedpubkey` 仍核对公钥；两者必须一起使用。不要从同一次未验证的公网请求获取 `FP`。IPv6 字面 URL 需要方括号，例如 `https://[2001:db8::1234]:18662`（文档地址，不能实际连接）。响应字段见 [协议](protocol.md#候选地址)。
