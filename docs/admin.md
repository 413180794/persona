# hub 管理界面（`/admin`）

hub 自带的网页管理界面：看 hub 的状态、加 / 删电脑、管设备凭据、装扩展、互联、改网络和访问范围。地址 `<hub 地址>/admin`，
接口前缀 `/api/v1/admin/`，和 App 的接口**分开鉴权**（管理员密码 + 会话 cookie，不认 App 的 token）。
方案见 [plan-hub-mesh.md](plan-hub-mesh.md) 第 5 节，总览 #293，issue #296。以后一键安装（#287）、hub 版本更新（#284）也落在这里。

代码：服务端 `server/internal/hub/admin*.go`（扩展 `admin_extensions.go`）、`server/internal/db/admin.go`（迁移 `0161_admin.sql`），
网页 `web/admin/index.html` + `web/src/admin/`（和网页操作页同一个工程、同一套设计 token，单独一个入口）。

## 访问范围 `admin.access`

`hub.json`：

```json
"admin": {
  "access": "lan",
  "cidrs": ["192.0.2.0/24"],
  "session_hours": 12
}
```

| `access` | 谁能打开 |
| --- | --- |
| `loopback` | 只有 hub 机器本机（`127.0.0.1` / `::1`）。Docker 里是容器自己，等于只能 `docker compose exec` 进去用 |
| `lan`（**默认**，不写也是它） | 本机 + 和 hub 同一网段的私网地址 + `security.trusted_cidrs` |
| `cidrs` | 本机 + `admin.cidrs` 里列的网段 |
| `any` | `lan` 能进的照样能进；此外任何来源**经 TLS 端口**（`public.tls_listen`）都能进。没开 TLS 时实际等于 `lan` |

- **范围外当它不存在**：页面路径交给普通网页（和任何不存在的路径一样），接口交给 App 的接口（没带 token 是 401、带了是 404），
  回包和一个从来没有管理界面的 hub 一模一样，扫描的人看不出来有管理界面。
- **同一网段**：hub 每个网卡的地址前缀里的私网地址（RFC 1918、ULA、链路本地）；IPv6 全局地址只算前缀 /64 或更长、不是 VPN 网卡上的
  （和 [network.md](network.md)「来源判断」一样，家里手机走全局 IPv6 也算家里）。网卡列表 30 秒刷新一次。
  另一个私网网段（公司内网的别的楼层、别的 VLAN）不算；要放进来写 `security.trusted_cidrs` 或用 `cidrs`。
  Tailscale 这类 `100.64.0.0/10` 的地址不是私网地址：要从 VPN 管理，把网段写进 `cidrs`（或 `trusted_cidrs`）。
- **Docker 桥接网络**：容器里只看得到 Docker 自己的网段（`172.x`），看不到 NAS 所在的局域网，所以在容器里（有 `/.dockerenv` 或
  `/run/.containerenv`）`lan` 放宽成「任何私网地址」，**但容器的网关不算**（`admin_gateway.go` 的 `containerGateway`：默认路由的下一跳，
  加上容器每个网段的第一个地址）。原因是经端口映射进来的连接有两种：
  - Linux 上 Docker 默认用 iptables DNAT 转外部来的 IPv4 连接，容器看到的是真实的来源地址，局域网里的电脑照常能进；
  - 经 Docker 的用户态代理（docker-proxy）转的连接，来源一律显示成网关（`172.x.0.1`）：宿主机自己连的、Docker 没开 IPv6 时所有 IPv6 的
    （公网来的也在里面）、Docker Desktop / OrbStack 上**所有**的。网关地址是私网地址，不排除的话公网请求就成了「局域网」，
    一台刚部署、还没设密码的 hub 会被公网上的人抢先设管理员密码。
  所以容器里网关来的请求一律当范围外（hub 日志每 10 分钟最多记一行 `admin: … is this container's gateway`），除非网关写进了
  `security.trusted_cidrs` 或 `access: cidrs` 的 `admin.cidrs`（明确选择：这等于放行所有经端口映射进来的请求）。
  Docker Desktop / OrbStack（开发机上试 Docker 部署）这种所有连接都来自网关的环境，`lan` 下打不开管理界面，可以：
  端口只映射到 `127.0.0.1`（`127.0.0.1:18660:18660`）再把网关写进 `JARVIS_ADMIN_CIDRS`（这时能经映射进来的只有本机）；或用 host 网络；
  或在 hub 机器上 `docker compose exec hub personad hub admin …` 用命令行管。入口脚本发现像 Docker Desktop / OrbStack
  （容器里能解析 `host.docker.internal`）时打印一行这个提示。
  hub 现有的 `sourceOf`（主 token、明文能不能用的判断）有同样的问题，另开 issue 复用 `containerGateway` 修。
- **转发头只用来收紧**：hub 不靠 `X-Forwarded-For` 放行任何人（同 [network.md](network.md)）；但请求带了 `X-Forwarded-For`、`X-Real-IP` 或 `Forwarded` 时，
  里面的每个地址也必须在范围内（解析不了的算范围外）；只带 `Via`、`X-Forwarded-Proto` / `-Host` / `-Port` / `-Server` 而不说来源地址的，
  也算范围外（是代理，又不知道替谁转的）。所以把 hub 放在反向代理后面、代理又把公网请求转进来时，管理界面不会因为代理的地址
  是本机就对公网打开。**什么头都不加的代理挡不住**（nginx 默认的 `proxy_pass` 就是）：对公网的反向代理要么加 `X-Forwarded-For`，
  要么直接拦掉 `/admin` 和 `/api/v1/admin/`。
- **公网来源一律要 TLS**：`cidrs` 里列了公网网段时，这些来源也只能走 TLS 端口（明文是 403 `tls_required`），密码不在公网上明文传。
- **只能在 hub 机器上放宽**：网页里只能收窄（`any` → `lan` / `cidrs` / `loopback`，`lan` → `loopback`，`cidrs` → 更小的 `cidrs` / `loopback`）。
  放宽用命令行 `personad hub admin access <loopback|lan|cidrs|any> [--cidrs a,b]` 或部署脚本（下面「部署时选择」）。
  网页收窄后如果自己不在新范围里，要先确认（接口不带 `force` 时回 409 `would_lock_you_out`）。
- **环境变量优先**：设了 `JARVIS_ADMIN_ACCESS`（和 `JARVIS_ADMIN_CIDRS`，逗号分隔）就以它为准、不写回 hub.json（同别的 `JARVIS_*`
  覆盖，[docker.md](docker.md)）；这时网页里不能改，显示「由部署时的 JARVIS_ADMIN_ACCESS 决定」。
- 改范围即时生效，不用重启：网页改的直接生效；命令行写完 hub.json 后经回环通知正在运行的 hub 重读（hub 没在跑就下次启动生效）。

## 管理员密码和会话

- **首次设置**：没设过密码时，范围内第一次打开显示「设置管理员密码」。只有按地址就在范围里的来源（`lan` / `cidrs` 放进来的，含本机）
  能做首次设置：`any` 下经 TLS 从公网来的人看到的是「请在 hub 所在的局域网里打开，或在 hub 机器上运行
  `personad hub admin reset-password`」，不会被抢先设密码。两个人同时设，只有先写进数据库的那个成功。
- **从本机回环来做首次设置要一次性设置码**：同一台机器上的反向代理（nginx 默认的 `proxy_pass` 不加任何转发头）把公网请求转进来时，
  hub 看到的来源也是 `127.0.0.1`，和坐在 hub 机器前的人分不出来。所以来源是回环（看连接本身的地址，不看头）时，设第一个密码还要填
  设置码：hub 启动时没有密码就在日志里打一行 `admin: first-run setup code XXXX-XXXX`（`journalctl --user -u jarvisd`，Docker 是
  `docker compose logs hub`），过期了以后有人从回环打开设置页时再打一个新的；也可以在 hub 机器上 `personad hub admin setup-code` 现取
  （经回环 + 主 token 向运行中的 hub 要）。码是 8 个字符（不含 0 / O / 1 / I，约 40 位随机），10 分钟有效，用一次就失效，只在 hub 内存里
  （重启后作废，启动时会打新的）；填错计入登录限流。局域网来源照旧不用码；设过密码以后登录不受影响。
- **密码**：至少 8 个字符、最多 256 字节；存 argon2id 哈希（m=19 MiB、t=2、p=1，PHC 格式）到 Postgres 的 `admin_credentials`，
  明文不落盘、不进日志。同时最多算 2 个哈希，多出来的排队（防止用登录请求把内存打满）。
- **会话 cookie**：`jarvis_admin`，32 字节随机值，库里只存 SHA-256；`HttpOnly`、`SameSite=Strict`、`Path=/api/v1/admin/`，
  经 TLS 时加 `Secure`。有效期 `admin.session_hours`（默认 12 小时，从登录算起，不续期）。每个请求都查库，
  所以退出、改密码、`reset-password` 立刻让别的会话失效。
- **CSRF**：会话自带一个 CSRF token，`GET /api/v1/admin/state` 给网页；所有写操作（POST / PUT / DELETE）要带
  `X-Jarvis-CSRF` 头且和会话的一致。另外写操作必须是 `Content-Type: application/json`，浏览器带了 `Sec-Fetch-Site` 时必须是
  `same-origin`，带了 `Origin` 时主机名和端口必须和请求的 `Host` 一致。登录、首次设置也查后两条（那时还没有会话）。
- **限流**：登录（和设置码）失败沿用 `security` 的阈值（`auth_failures` 次 / `auth_window_seconds` 秒，封 `auth_block_seconds` 秒），
  但用一张单独的表、**局域网和本机回环也计数**（密码比设备 token 好猜得多；回环可能是同机反向代理转来的公网请求）。hub 机器的主人
  因此被锁一会儿时，`personad hub admin …` 照样能用。局域网以外的来源另外共用一个十倍阈值的总额度，防很多地址一起猜。
  被封时回 429 和 `Retry-After`。管理界面的失败不影响 App 的鉴权计数，反过来也一样。
- **忘了密码**：在 hub 机器上 `personad hub admin reset-password`：有终端时输两遍新密码（不回显），没终端时从标准输入读一行；
  `--clear` 清掉密码，回到首次设置。两种都会让所有已登录的会话失效。Docker 里：`docker compose exec hub personad hub admin reset-password`。

## 改 hub.json

管理界面改的东西写回 `~/.persona/hub.json`：

- **原子写**：同目录写临时文件、`fsync`、`rename` 覆盖，权限一律 0600；同一时间只有一个写者（进程内的锁 + 对 `~/.persona` 目录加 flock，命令行和 hub 之间也互斥）。
- **保留不认识的字段和顺序**：只改要改的那几个键，别的键（包括这个版本不认识的、别的分支加的）原样保留，顺序不变，缩进统一成两个空格。
- **环境变量的值不写回**：Docker 的 `JARVIS_PUBLIC_URL` 之类只在内存里覆盖，管理界面改的是文件里的值。
- **生效**：

  | 改什么 | 什么时候生效 |
  | --- | --- |
  | hub 名字 `name` | 立刻（hello / health 里的 `hub_name` 下次请求就是新的） |
  | 管理界面访问范围 `admin.access` / `admin.cidrs` | 立刻 |
  | 加电脑（`machines`，kind link / service） | 立刻（加完就开始连） |
  | 删电脑、改一台电脑的设置（`machines`） | 重启 hub 后 |
  | `public_url`、`public.tls_listen`、`public.advertise` | 重启 hub 后 |

  要重启的改动，接口在回包里带 `"restart": {"required": true, "reasons": ["…"]}`，网页顶部一直提示「有 N 项改动重启后生效」，直到重启。
  「重启 hub」按钮只在 hub 有人管着重启时出现（systemd 的 `INVOCATION_ID`、容器里）：hub 正常退出，由 systemd / Docker 拉起来。
  没人管的（手动在终端里跑的）只提示去重启。

## 页面

| 页面 | 路径 | 内容 |
| --- | --- | --- |
| 首次设置 / 登录 | `/admin` | 没设密码时设密码，否则输密码登录 |
| 概览 | `/admin` | hub 名字（可改）、版本、运行时长、监听 / TLS / 指纹、候选地址、电脑和设备数、「运行检查」（`personad hub doctor` 的结果）；除了本机没有别的电脑时，顶部引导「添加电脑」 |
| 电脑 | `/admin/machines` | 「添加电脑」放在最上面，两种：**电脑来连**（kind link：填 id / 名字，生成 jarvis-link 配对链接，给 Mac / Linux / Windows 的装法）和 **hub 来连**（kind service：粘贴那台 `jarvis-link pair --listen` 打印的链接，可改地址、填 socks5 代理）；不再添加 ssh 电脑。每台的状态（在线 / 离线 / 错误、连接方式、node 版本）和**每条路**（方式、经哪台 hub、能不能用、RTT、哪条在用，[mesh.md](mesh.md)「一台电脑多条路」）；service 电脑的连接器被配给别的 hub 时「重新配对」；重连、删除（只在成员上配的电脑不能在这里删） |
| 设备 | `/admin/devices` | 设备凭据列表、撤销、生成手机配对二维码；成员 hub 上只提示去中心的管理界面 |
| 扩展 | `/admin/extensions`（老的 `/admin/plugins` 照样打开） | 这台 hub 的扩展：列表、开关、安装（hub 机器上的目录或包、带 sha256 的 https 链接、上传、remote socket；先预览它能做什么，同意才装）、升级（权限变多明确提示）、回滚、卸载（可留数据），以及扩展市场（登记市场、浏览目录、从市场安装 / 升级），见下面「扩展」和 [extensions.md](extensions.md)「管理界面：扩展」 |
| 网络与访问 | `/admin/network` | 公网地址 / TLS / 候选地址（可改，重启生效）、`security` 的设置（只读）、管理界面的访问范围（只能收窄）、改管理员密码 |
| 高级 › 互联 | `/admin/peers` | 一般用不到（推荐只装一台 hub）：组里的 hub（中心 / 成员、在线、任期，「设为中心」，原中心连不上时先确认）、发现到的 hub、生成 / 输入互联码、链路状态和 RTT、断开（[mesh.md](mesh.md)） |

成员 hub 的每一页顶部都写「这台 hub 是组里的成员，组由中心「X」管理」（中心连不上时多一句，并给「换中心」的入口）：设备、互联这些组的事去中心的管理界面做。

导航是一个列表（`web/src/admin/nav.ts`，带「高级」分组），以后加「更新」（#284）、「设置」（#287）就是加一项。

## 接口

都在 `/api/v1/admin/` 下，先过访问范围，再过会话（`state`、`setup`、`login` 除外）。错误是 `{"error": "…", "code": "…"}`；
没登录或会话过期 401 `login_required`，CSRF 不对 403 `csrf`。写操作的请求体一律 JSON（没有内容也发 `{}`）。

| 接口 | 作用 |
| --- | --- |
| `GET state` | `{"password_set", "logged_in", "can_setup", "setup_code_required", "csrf", "hub_name", "version", "session_expires_at"}`；`csrf` 只在登录后有 |
| `POST setup` `{"password", "setup_code"}` | 首次设密码并登录，回 state；409 `already_set`、403 `setup_local_only` / `setup_code_required`、401 `bad_setup_code`、400 `weak_password`、429 `rate_limited` |
| `POST setup-code` | `personad hub admin setup-code` 用：只认回环 + 主 token，回 `{"code", "expires_at"}`；设过密码 409 `already_set` |
| `POST reload` | `personad hub admin access` 写完 hub.json 后用：只认回环 + 主 token，重读 `admin` 和 `name` |
| `POST login` `{"password"}` | 回 state；401 `bad_password`、429 `rate_limited`（`retry_after` 秒） |
| `POST logout` | 退出 |
| `POST password` `{"current", "new"}` | 改密码，别的会话全部失效 |
| `GET overview` | `hub`（`hub_id`、`name`、`name_set`、`default_name`、`version`、`started_at`、`os`、`arch`、`container`、`can_restart`）、`listen`、`public_url`、`tls`（`listen`、`port`、`fp`；没开是 null）、`endpoints`、`machines`（`total`、`online`）、`devices`、`restart` |
| `PUT hub` `{"name"}` | 改 hub 名字（空串 = 用默认名），立刻生效 |
| `POST doctor` | 跑一遍 `hub doctor`：`{"items": [{"level": "ok｜problem｜info", "item", "detail"}], "next": ["…"], "ran_at"}` |
| `POST restart` | 重启 hub（只在 `can_restart` 时）；409 `not_supervised` |
| `GET machines` | `{"machines": [{id, name, kind, icon, ssh_host, online, error, transport, node_version, node_mismatch, os, last_seen, link, state, tailcat, address, proxy, hub_id, paths}], "tls": bool, "restart"}`；`state`：`active` / `pending_add`（写进 hub.json 了，hub 没在跑时重启后才连）/ `pending_remove`；kind `hub` 是只在成员上配的电脑；`paths` 每条路 `{via, hub_id, id, ok, rtt_ms, active, error}`；`proxy` 不含密码 |
| `POST machines` `{"kind": "link", "id", "name", "icon"}` 或 `{"kind": "service", "id", "name", "icon", "pair_link", "address"?, "proxy"?}` | 加一台，立刻开始连，回 `{"machine", "restart"}`；service 先和连接器配对（凭据加密存库），成功才写 hub.json。400 `invalid_id` / `invalid_name` / `invalid_kind` / `invalid_proxy` / `ssh_unsupported`（不再添加 ssh、tailcat）、409 `exists`、502 `pair_failed`（`error` 是给人看的原因） |
| `POST machines/{id}/connector` `{"pair_link", "address"?}` | service 电脑重新配对连接器：`{"ok", "address", "version", "restart_required"}`（换了地址要重启）；400 `not_service`、502 `pair_failed` |
| `DELETE machines/{id}` | 从 hub.json 删掉，回 `{"ok", "restart"}` |
| `POST machines/{id}/reconnect` | 重连；409 `not_active` |
| `POST machines/{id}/link-pair` | 给 link 电脑的 jarvis-link 生成配对链接：`{"pair_url", "expires_at", "command"}`；409 `tls_required` / `not_active`、400 `not_link` |
| `GET devices` | `{"devices": [{id, name, kind, role, created_at, last_seen, machine_id}]}` |
| `DELETE devices/{id}` | 撤销 |
| `POST devices/pair` | 手机配对：`{"pair_url", "url", "expires_at", "fp", "qr": {"size", "rows": ["0101…"]}}`（`rows` 每行一个 `0` / `1` 串，不含留白） |
| `GET network` | `listen`、`public_url`、`tls`（`listen`、`port`、`fp`）、`advertise`、`advertise_ipv6`、`advertise_lan`、`endpoints`、`security`（`trusted_cidrs`、`allow_public_plaintext`）、`admin`（`access`、`cidrs`、`session_hours`、`from_env`、`container`）、`you`（`address`、`tls`、`allowed`：四种范围各自放不放你进来）、`restart` |
| `PUT network/access` `{"access", "cidrs", "force"}` | 收窄访问范围；403 `widen_cli_only`、409 `from_env` / `would_lock_you_out`、400 `invalid_cidr` |
| `PUT network/public` `{"public_url", "tls_listen", "advertise"}` | 改公网地址 / TLS 监听 / 候选地址（省略的字段不变），重启生效 |
| `GET mesh` | `{"role": "center｜member", "center": {hub_id, name, online}, "term", "hubs": [{hub_id, name, role, online, self, paired}]}` |
| `POST peers/{hub_id}/center` `{"confirm"}` | 设为中心（hub_id 可以是自己）；409 `confirm_required`（原中心连不上，要确认）/ `unreachable`，404 `not_in_group` |
| `…/peers`、`…/routes` | 互联：配过对的 hub（每项 `role`）、发现的 hub、互联码、断开、路由（[mesh.md](mesh.md)「管理接口和命令行」） |
| 成员上的 `devices*` | 409 `member_hub`：设备由中心管 |
| `GET extensions` | `{"extensions": [ExtensionDetail], "can_install", "upload_most"}`：每项是 App 接口的详情（[extensions.md](extensions.md)「hub 的接口」），`upload_most` 是上传的上限（字节） |
| `GET extensions/{id}` | 一个扩展的详情；404 `not_found` |
| `POST extensions/preview` `{"source", "upgrade"?}` | 预览（来源写法同 `personad extension install`），回预览（含 `permissions_digest`、升级时的 `more_permissions` / `added_permissions`）；400 `invalid_plugin`（`error` 是给人看的原因）、409 `busy`、429 `rate_limited` |
| `POST extensions/upload?name=<文件名>&upgrade=<id>` | 请求体是 `.tar.gz` / `.tgz` / `.zip` 本身（`Content-Type: application/octet-stream`，唯一不是 JSON 的写操作，照样要 CSRF 头），最大 200 MB，回预览；413 `too_large`，别的同上 |
| `POST extensions/install` `{"preview_id", "permissions_digest"}` | 安装预览过的那一份，回详情；不带摘要 400 `confirm_required`，摘要对不上、预览过期 400 `install_failed` |
| `POST extensions/{id}/upgrade` `{"preview_id", "permissions_digest"?}` | 升级，权限变多时必须带新的摘要；400 `upgrade_failed` |
| `POST extensions/{id}/rollback` | 回到上一个版本；400 `rollback_failed` |
| `PUT extensions/{id}/enabled` `{"enabled"}` | 开关；hub.json 来的 409 `read_only` |
| `DELETE extensions/{id}` `{"keep_data"}` | 卸载；内置的和 hub.json 来的 409 `uninstall_failed` |
| `GET extensions/-/markets`、`POST extensions/-/markets` `{"spec", "token"?}`、`DELETE extensions/-/markets/{id}`、`POST extensions/-/markets/{id}/refresh`、`PUT extensions/-/markets/{id}/token` `{"token"}` | 扩展市场，和 App 的 `/api/v1/extensions/-/markets…` 同一套（[extensions.md](extensions.md)「扩展市场」）；登记前先读一次索引，读不到 400 `market_failed`（`error` 是给人看的原因），token 只写不回显 |
| `GET extensions/-/catalog?q=&market=&refresh=1`、`POST extensions/-/catalog/preview` `{"market", "extension"}`（老键 `plugin` 也认） | 市场的目录（`{markets, extensions, platform}`），和从市场预览（回同样的预览，多一个 `market`）；拿到预览后照常 `extensions/install` / `extensions/{id}/upgrade` |
| `GET extensions/-/metrics?window=`、`GET extensions/{id}/metrics?window=`、`GET extensions/{id}/logs?since=&q=&limit=`、`GET extensions/{id}/logs/stream?since=` | 扩展的运行情况（调用次数、错误率、延迟、重启）和日志（server-sent events 实时跟），见 [extensions.md](extensions.md)「运行情况」 |
| 没有扩展底座的 hub | `extensions*` 写操作 503 `no_plugins`；更老的 hub 没有这些路由（404），页面提示先升级 hub |
| 老路径 `plugins…` | #1844 之前的路由，hub 照样认，回老字段名（`plugins`、`plugin`、`plugin_name`，市场的个数 `plugins`）；页面先读一次列表，`extensions` 回 404 就整页改用老路由（老 hub，`web/src/admin/api.ts`） |

## 扩展

装扩展就是在 hub 机器上运行新程序。管理界面能加电脑、签发设备凭据，本来就和 hub 主人一样大，所以登录的会话也能装（[extensions.md](extensions.md)「安装、升级、回滚、卸载」），
和 `personad extension` 用同一个注册表、同样的检查，另外：

- **先预览、再同意**：安装和权限变多的升级都要带预览给的 `permissions_digest`，装的就是页面上给人看过的那一份；页面只在拿到预览后才给「同意并安装」。
- **预览限流**：每份预览在暂存目录（`~/.persona/extensions/staging/`）里放最多 200 MB、10 分钟，所以同一时间只跑一个（另一个在跑时 409 `busy`），
  10 分钟内最多 10 次（成功失败都算，超了 429 `rate_limited` 和 `Retry-After`），偷到会话的人也塞不满磁盘。
- **上传**：浏览器把文件原样 POST 上来，hub 边收边写进这次预览的暂存目录（超过 200 MB 就停），解包后删掉原文件；先按 `Content-Length` 拒掉明显过大的。
- **审计**：每一步记进 `plugin_audit`，执行者 `admin UI`；hub 日志里一行 `admin: installed plugin …`。
- **扩展市场**（#1330，页面 `web/src/admin/pages/ExtensionMarket.tsx`，逻辑和文字与网页共用 `web/src/lib/extension-market.ts`）：扩展列表下面两张卡片，只在能装扩展（`can_install`）且 hub 有市场接口时出现（404 / 503 时整节不显示）。
  「扩展市场」列出登记的市场：名字、有没有 token、地址（https / GitHub / git 各一个图标）、几个扩展 · 多久前读过、读不到的原因（红）和索引的警告（橙）；每个市场能重新读取、填 / 换 / 去掉 token（只写，不回显）、删除（就地确认；从它装的扩展留着）。
  「添加市场」填地址和可选的 token：占位文字和说明列出三种写法（`https://…`、`github:owner/repo[@分支]`、`git:<地址>[#分支]`，#1373），页面不查写法，原样交给 hub，hub 读到索引才登记，读不到把原因写在对话框里；
  `git:`（和省略前缀的 `git@host:path.git`）的市场用 hub 机器自己的 SSH 凭据，对话框里 token 不能填，列表里也没有「填 / 换 token」。
  「市场里的扩展」可以搜索、按市场筛选，每行是名字、版本、状态（未安装 / 可升级到 x / 已装 / hub 自带 / 装不了）、发布者、市场、说明、标签和装不了的原因；
  「安装」「升级」先 `extensions/-/catalog/preview`，弹出和别的来源一样的预览（权限、权限变多时的红色提示和「新增」、剩几分钟），同意后走上面的 `extensions/install` / `extensions/{id}/upgrade`（带预览的 id 和摘要），装完列表和目录一起刷新。
  从市场装的扩展和别的已装扩展一样，在列表里升级、回滚、卸载。截图：`/admin/extensions?mock=app`，`&dialog=market` 直接打开第一个能升级的扩展的预览，`&dialog=market-add` 打开添加市场（`web/src/admin/mock.ts`）。

## 部署时选择

| 部署方式 | 怎么选 |
| --- | --- |
| `scripts/deploy.sh` | `--admin-access <范围>`（`cidrs` 时再加 `--admin-cidrs a,b`）或 `JARVIS_ADMIN_ACCESS`；都没给且 hub 上还没选过时，有终端就问，没终端用 `lan` 并打印一行提醒 |
| `install.sh`（公开版） | `curl … \| JARVIS_ADMIN_ACCESS=lan sh` 或 `sh -s -- --admin-access lan`；都没给时，有终端（`/dev/tty`）就问「这台要跑 hub 吗」再问范围，没终端打印一行提醒、不写 hub.json（不写就是 `lan`） |
| Docker | `.env` 的 `JARVIS_ADMIN_ACCESS`（`cidrs` 时加 `JARVIS_ADMIN_CIDRS`）；没设时入口脚本打印一行提醒，用 hub.json 里的（没写就是 `lan`） |

问的时候把四个选项的意思和风险都说清楚（见 `scripts/deploy/install.sh` 的 admin access 一段，`deploy.sh` 原样取用，公开安装器和部署问的是同一段话）。

## 安全取舍

- **为什么默认 `lan` 而不是 `loopback`**：NAS、没接显示器的服务器上，本机浏览器用不上；`lan` 已经挡住了公网和别的网段。
- **为什么 `any` 也放局域网明文**：`any` 是 `lan` 的超集；局域网里明文传密码和 `lan` 下一样，是这个范围本来就接受的。
- **为什么不用 App 的 token**：设备 token 能被撤销、能被窃取后在公网上用；管理界面能加电脑、发凭据、改访问范围，权限比 App 大，单独一套
  密码 + 范围限制，token 泄露了也进不来。
- **为什么不做多管理员 / 两步验证**：自己用的 hub，一个管理员；需要更严的话收窄到 `loopback`，用 ssh 隧道访问。
