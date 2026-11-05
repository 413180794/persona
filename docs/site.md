# 官网：托管、香港入口、以后的分线路

`<官网域名>` 是配置里 `publish.site_url` 的域名（具体值在构建配置和 `CLAUDE.local.md`）。`<旧域名>` 是早期构建写死的那个，2026-11-05 起（#2389）
不再连 Worker，整站 301 到 `<官网域名>` 的同一路径（见下面「旧域名」）。

官网（`site/`）本身是 Cloudflare 上的 Worker `jarvis-site`：静态页面、公开的 personad 下载（`/dl/`）、安装脚本（`/install.sh`）、
推送中继的入口（`/relay/*`，`docs/push-relay.md`）都在它上面。只经 `scripts/release/publish.sh` 部署（`docs/release.md`）。

## 现在的走法（2026-11-04 起，#1134）

```
任何人 ──► <官网域名>（A 记录，仅 DNS）──► 阿里云香港 ECS 上的 nginx ──► origin.<官网域名>（Worker 自定义域名）──► Worker
旧构建 / 已部署的 hub ──► <旧域名> ──► 301 到 <官网域名> 的同一路径
```

- **为什么**：Cloudflare 免费版在大陆没有节点，国内访问被分到欧洲、日本的机房，往返约 300ms、丢包明显，页面时常卡十几秒。
  阿里云香港对大陆线路好（Globalping 国内 20 个探针 ping 中位数 38ms、丢包 0%），首页打开中位数从约 0.9 秒降到约 0.23 秒、不再超时。
  海外每次请求多绕一跳香港，慢 0.5–1.5 秒；现在的用户都在国内，先这样。
- **香港入口**：阿里云 `cn-hongkong` 的一台最小 ECS（实例、IP、账号写在 `CLAUDE.local.md`），Debian + nginx，安全组只开 80 / 443 / ICMP，
  不开 SSH，管理用阿里云云助手（`ecs RunCommand`）。nginx 把所有请求转给 `origin.<官网域名>`（按 60 秒的缓存动态解析，
  Cloudflare 换 IP 不用重启）；页面和静态文件缓存 60 秒、Worker 不可用时继续给旧的；`/relay/`、`/dl/`、`/install.sh` 不缓存、边下边转。
- **客户端地址**：经入口转发后 `cf-connecting-ip` 是入口自己的地址。入口带上 `x-persona-edge-secret`（Worker secret `EDGE_SECRET`）
  和 `X-Forwarded-For: <客户端>`，`site/worker.mjs` 只有密钥对上时才信 `X-Forwarded-For`，推送中继按它限流；密钥不对、没配都照旧。
  密钥在 1Password，入口那份在 `/etc/nginx/persona/edge-secret.conf`（0600）。
- **证书**：入口自己的 Let's Encrypt 证书，certbot 用 HTTP 验证、systemd 定时器自动续期；Worker 的自定义域名（只剩 `origin.<官网域名>`）的证书 Cloudflare 自管。
- **`site/wrangler.jsonc` 里没有 `<官网域名>`**：部署会把配置里没有的自定义域名去掉、把列着的重新占上；
  <官网域名> 由 DNS 指向入口，列进去会被部署抢回 Worker。`origin.<官网域名>` 必须列着（入口靠它回源）。
- **退路**：入口出问题时，把 <官网域名> 改回 Worker 自定义域名（`PUT /accounts/<id>/workers/domains`，hostname <官网域名>，
  service jarvis-site），几分钟恢复。
- **旧域名**：不在 `site/wrangler.jsonc` 里（部署会把它从 Worker 上摘掉），由它所在 zone 的一条 proxied 占位 DNS 记录（AAAA `100::`）
  加一条 Single Redirect 规则整站 301 到 `<官网域名>`，路径和 query 原样带上；规则和记录只在 Cloudflare 上，不进仓库。
  代价是用户接受的：301 之后客户端不会原样重发 POST，还写着旧域名的旧版 App / hub 的推送中继会断；检查更新、下载是 GET，跟随跳转的客户端照常，
  但不保证每个旧版本都跟随。要恢复只能升级到写着新域名的构建。

## 为什么没用别的办法（都实测过）

| 办法 | 结果 |
| --- | --- |
| 阿里云 CDN「全球（不含中国内地）」 | 国内被分到香港节点，但大陆过去的线路没优化，建连 200–390ms，中位数比 Cloudflare 还慢；海外回源慢 10 倍 |
| 阿里云函数计算（香港）当入口 | 线路好（国内 37ms），但自定义域名必须 CNAME 到它，根域名不能 CNAME，绑不上 |
| Cloudflare 负载均衡按国家分流（仅 DNS） | 分流本身可行（要把 `location_strategy.mode` 设成 `resolver_ip`，默认 `pop` 按收到查询的机房判断，国内永远判不成中国）；但海外那一路回到 Worker 会 `error 1000`，要靠 Cloudflare for SaaS，而域名本身在 Cloudflare 上时（O2O）自定义主机名必须是指向 SaaS 区域的 CNAME 才能生效，和负载均衡记录冲突 |
| 轻量应用服务器 | 香港所有档位缺货（2026-11-04） |

## 以后：国内走香港、海外走 Cloudflare（一个域名）

业界常用做法：域名的 DNS 放在支持分线路解析的服务商（DNSPod / 阿里云 DNS），境内线路 → 香港入口，境外 / 默认线路 → CNAME 到
Cloudflare for SaaS（chirpet.com 上已开 SaaS，回退源 `saas-fallback.chirpet.com`）。这时 <官网域名> 不在 Cloudflare 的 DNS 上，
自定义主机名不再是 O2O，TXT 预验证可用。

做不了的原因：<官网域名> 注册在 Cloudflare Registrar，**只能用 Cloudflare 的 DNS**；按 ICANN 规定注册满 60 天才能转出
（<官网域名> 注册于 2026-11-03，2027-01-02 之后）。转到别的注册商后：

1. DNS 放到 DNSPod 或阿里云 DNS，搬全现有记录（MX / SPF / DKIM 等邮件转发记录、验证用的 TXT）。
2. 在 chirpet.com 的 SaaS 下登记 <官网域名> 为自定义主机名（TXT 验证），chirpet.com 加 Worker 路由 `*/*` → jarvis-site，
   并加不挂 Worker 的排除路由 `chirpet.com/*`、`*.chirpet.com/*`（先加排除、再加通配，chirpet.com 自己的子域名不受影响）。
3. 分线路：境内 → 香港入口 A 记录；境外和默认 → CNAME 到 `saas-fallback.chirpet.com`。
