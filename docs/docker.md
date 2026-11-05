# 用 Docker 跑 hub（NAS 等）

产品名是 Persona，迁移版本的二进制使用 `personad`（`jarvisd` 兼容），默认运行目录使用 `~/.persona`（`~/.jarvis` 兼容）。本文的 `.env` / compose 变量保留实际模板中的旧名；已迁移的 `PERSONA_*` 优先，`JARVIS_*` 继续兼容。镜像中的用户、数据库角色 / 库名、容器和持久化卷不能按产品名替换，见 [配置与兼容说明](config.md#命名与兼容)。

hub 有两种部署方式，功能一样，按 hub 所在的机器挑一种：

| 方式 | 适合 | 怎么装、怎么升级 |
| --- | --- | --- |
| **原生二进制**（`docs/server.md`「部署」） | 一台 Linux 服务器；hub 所在的机器自己也要跑 Agent（`kind: local`） | 开发机上 `./scripts/deploy/deploy.sh`（`systemd --user` 服务 + Postgres 容器）；或 `install.sh` 装好二进制后自己配服务。升级再跑一次 `deploy.sh` |
| **Docker**（本文） | NAS（群晖、威联通、Unraid…）或任何只当 hub 的机器 | `docker compose up -d`（hub + Postgres 两个容器）。升级 `docker compose pull && docker compose up -d` |

两种方式的 hub 是同一个 `personad`，`hub.json`、数据库、协议、App 都一样；从一种换到另一种只要搬 `~/.persona` 和数据库。

## 容器里的 hub 有什么不同

镜像里没有 herdr 和 Claude Code，所以 **hub 所在的这台机器不能同时当干活的电脑**：
- 第一次启动写的 `hub.json` 里 `"machines": []`（不是原生方式的「本机一台 `kind: local`」），每台电脑都要加进去。
- 「Persona」助手跑在某台电脑上：`assistant.machine` 填它的 id（不填就用 `machines` 里的第一台）。
- 飞书用 `lark.token`，不能用 `lark.container`（那要在 hub 里跑 `docker inspect`）。
- 插件（`docs/extensions.md`）是 hub 机器上的外部程序，镜像里没有，暂不支持。

电脑怎么接进来：
- **tailcat（推荐）**：NAS 不用 ssh 到任何电脑，电脑在哪个内网都行（`docs/server.md`「tailcat 打洞」）。
- **ssh**：把 NAS 上用户的 `~/.ssh` 只读挂进容器（compose 里有注释掉的一行），里面要有私钥、`config` 里的别名和 `known_hosts`
  （hub 用 `BatchMode=yes`，不认识的主机直接失败，不会问）。

## 镜像

`ghcr.io/<owner>/persona`（镜像名跟着仓库走：`<owner>` 是仓库所属账号或组织，`persona` 是仓库名；填进 `.env` 的 `JARVIS_IMAGE`），linux/amd64 和 linux/arm64，由 `.github/workflows/hub-image.yml` 构建：

| 标签 | 什么时候有 |
| --- | --- |
| `<版本>` | 推一个 `v*` tag（`v1.2.0` → `1.2.0`），见 `docs/release.md`「发布 hub 镜像」 |
| `latest` | 同上，但预发布 tag（带 `-`，如 `v1.3.0-rc1`）不动它 |
| `edge`、`sha-<提交>` | 在 Actions 里手动运行这个 workflow（当前分支） |

每次运行都先构建 amd64 镜像、用 compose 起一遍冒烟（健康检查、配对、tailcat key、网页版、`hub.json` 的属主和权限），过了才推。

仓库是私有的，ghcr 上的镜像默认也是私有：NAS 上先 `docker login ghcr.io`（用户名是 GitHub 账号，密码是一个有
`read:packages` 权限的 Personal Access Token；NAS 的 Docker 配置不在默认位置时用 `docker --config <目录> login`）。也可以在仓库的
Packages 设置里把它改成公开。Postgres 用的是 Docker Hub 上的 `postgres:18-alpine`：拉不到 Docker Hub 的网络（国内常见）在 `.env`
里用 `JARVIS_POSTGRES_IMAGE` 换成镜像代理或本地仓库里的同一个镜像（如 `<仓库>/library/postgres:18-alpine`）。

镜像里有：
- `personad`（hub 用 `linux/<架构>` 那个）和四个平台的 node 构建（`/opt/jarvis/nodes`，`JARVIS_NODE_BINARIES`）：App 里「重新部署」
  直接从这里取，hub 和 node 永远是同一个版本；
- 嵌进二进制的网页版（`docs/web.md`），打开 hub 的地址就是；
- `openssh-client`、证书、时区数据。

镜像里的 personad 不带任何 skill（和所有二进制一样）：助手的 skill 放在跑助手那台机器的 `~/.persona/skills/`（`docs/hub-update.md`「skill 从二进制里拿出来」）。
镜像里的 hub 是 `docker` 渠道：不会自己换二进制，有新镜像时提示拉取（见「升级」）。构建时把镜像名（`ghcr.io/<owner>/<repo>`）写进二进制，
compose 传进来的 `JARVIS_IMAGE` 优先。

## 装

先说清楚：**NAS 只当 hub，AI 不在 NAS 上干活**。镜像里没有 Claude Code，助手和 Agent 都跑在你的另一台电脑上，所以装完 hub 还要加一台电脑（下面第 5 步），在那之前 App 能配对、能看，主对话还开不了。

1. NAS 上建一个目录（例如 `/volume1/docker/persona`），里面再建一个 `data` 子目录，都用 NAS 的文件管理器建：`data/` 归你，hub 的文件就归你（`data/` 让 Docker 自己建的话归 root，文件就都归 1000，见下表）。目录里放进 `deploy/docker/compose.yaml` 和 `deploy/docker/.env.example`，把后者改名成 `.env`，填两行：
   - `JARVIS_IMAGE`：镜像，`ghcr.io/<owner>/persona:latest`（见上「镜像」；私有镜像先 `docker login ghcr.io`）。
   - `JARVIS_PUBLIC_URL`：手机怎么访问这台 NAS，一般是 `http://<NAS 的局域网地址>:18660`。
2. 在这个目录里运行 `docker compose up -d`。
3. 配对手机：`docker compose exec hub personad hub pair`，用 App 扫终端里的二维码。
4. 设管理员密码（可选，加电脑、管设备用得到）：浏览器打开 `<JARVIS_PUBLIC_URL>/admin`，填 `docker compose logs hub` 里那行 `first-run setup code`（过期了用 `docker compose exec hub personad hub admin setup-code` 再要一个）。
5. 加一台干活的电脑：网页版（浏览器打开 `JARVIS_PUBLIC_URL`）的「机器」页点「添加电脑」，或管理界面的「电脑 › 添加电脑」，照着在那台电脑上装 Persona Link（`docs/link.md`），它连上来后确认是你的电脑。

第一次启动自动补上的（`.env` 里不用写，写了以写的为准）：

| 什么 | 没写时 | 存在哪 |
| --- | --- | --- |
| 数据库密码（`JARVIS_DB_PASSWORD`） | postgres 第一次启动（还没有数据库）时随机生成 48 位十六进制；数据库已经在了而密码文件没了（只恢复了 `postgres/`、删了 `data/`、照新模板重写了 `.env`）时不生成，日志里提醒把原来的密码写进 `JARVIS_DB_PASSWORD`。不写密码要用这个版本以后构建的镜像，`JARVIS_IMAGE` 钉在更早的版本时照旧写上 | `data/docker/db-password`（0600）；postgres 容器只挂这一个子目录，看不到 hub 的密钥。镜像里的 `personad` 每次运行（含 `docker compose exec`）从这里读，拼进数据库地址 |
| `PUID` / `PGID` | `data/` 目录的属主；Docker 自己建的目录归 root，这时用 1000 | 日志里打一行用的是哪个 |
| `hub.json` | 写一份容器用的新安装默认值（见下） | `data/hub.json` |

`.env` 是 compose 在宿主机上读的，容器启动时已经读完了，改不了它，所以生成的值放在 `data/` 里，不回写 `.env`。备份 `data/` 就带上了密码。

第一次启动（`data/` 里还没有 `hub.json`）时入口脚本写的 `hub.json`，hub 读到后补上 token 和其余默认值（和 `personad hub setup` 的新安装默认值同一个思路，`docs/onboarding.md` 4.1；已经有 `hub.json` 的一个字都不改）：

| 字段 | 值 | 为什么 |
| --- | --- | --- |
| `public_url` | `""` | 容器在桥接网络里看不到 NAS 的局域网地址，猜出来的（容器的主机名）手机连不上。以 `.env` 的 `JARVIS_PUBLIC_URL` 为准；没设时日志里提醒，`hub pair` 也直说要设它，或者在管理界面的「网络」里填 |
| `public.tls_listen` | `":18662"` | Persona Link 和局域网发现只走 TLS。容器里绑所有地址，NAS 上开在哪些地址由 compose 的端口映射决定（`JARVIS_TLS_PORT`） |
| `admin.first_setup` | `"code"` | 容器里任何私网地址都算局域网（见下「管理界面」），不要设置码的话，同一网络里谁都能抢先设管理员密码 |
| `machines` | `[]` | 镜像里没有 herdr 和 Claude Code，电脑都从外面加 |

其他说明：
- `JARVIS_PUBLIC_URL` 也可以是 VPN 地址，或 NAS 自带的反向代理给的 HTTPS 地址。反向代理要转发 WebSocket 的 Upgrade / Connection、关闭响应缓冲、加长读取超时（`docs/server.md`「轻量 hub 与部署前检查」）。
- 端口：18660 是 HTTP，18662 是 TLS，compose 默认都映射到 NAS 的所有地址。TLS 端口的宿主端口要保持 18662（hub 告诉 App 的就是这个）；只想开在一个地址上就写 `JARVIS_TLS_PORT=<NAS 局域网地址>:18662`。NAS 上 18662 已被占用时 `docker compose up` 会报端口冲突：给 hub 换一个 `public.tls_listen` 端口并同样映射。
- `PUID` / `PGID`：容器以 root 启动，把 `jarvis` 用户改成这个 uid / gid（镜像里别的同 uid 用户先删掉，否则 ssh 找到的是它的家目录）、把 `data/` 里不归它的文件都交给它（例如用 sudo 拷进来的旧 `~/.jarvis`），再降权运行；
  `docker compose exec hub personad …` 也以这个用户运行，建出来的文件不会归 root。不能是 0。不要用 compose 的 `user:` 指定用户：
  那样入口脚本改不了用户，PUID / PGID 不生效，ssh 也跑不起来（日志里会有一行警告）。
- 数据库密码自己设时只用字母和数字（它原样拼进数据库地址），`openssl rand -hex 24` 即可。Postgres 只在第一次建数据目录时用它，
  之后改 `.env`（或删掉 `data/docker/db-password`）会连不上库，要改得同时在库里 `ALTER USER jarvis PASSWORD '…'`。
- hub 容器带 `init: true`：personad 起的 ssh 被杀掉后，它的 ProxyCommand / ProxyJump 子进程由 init 回收，不留僵尸进程。
- 目录：`data/` 是 hub 的 `~/.persona`（`hub.json`、密钥、tailcat key、知识库 `data/data`、生成的库密码 `data/docker/`），`postgres/` 是数据库；都在 compose 旁边，
  NAS 的文件管理器里看得到，备份这两个目录即可。`hub.json` 和密钥文件是 0600。
- 已有的部署（`.env` 里有 `JARVIS_DB_PASSWORD`、`PUID` / `PGID`，`data/` 里有 `hub.json`）换上新的 `compose.yaml` 后行为不变：用 `.env` 的密码，不生成、不改 `hub.json`；多出来的只有一个空的 `data/docker/` 目录和 18662 的端口映射（`hub.json` 没开 TLS 时那个端口没人听）。

接一台电脑也可以不用 App，走 tailcat（电脑在别的内网、NAS 连不过去时）：

```sh
docker compose exec hub personad hub tailcat-key          # NAS 上：hub 的公钥
~/.persona/bin/personad node tailcat init --hub <公钥>     # 那台电脑上：打印它的地址
./scripts/deploy/node-launchd.sh install                        # 那台电脑上：常驻 node
```

把地址写进 `data/hub.json` 的 `machines`（`{"id":"laptop","name":"笔记本","kind":"tailcat","tailcat_addr":"tc…"}`），
`docker compose restart hub`。

## 环境变量（覆盖 hub.json，不写回文件）

| 变量 | 作用 |
| --- | --- |
| `JARVIS_DATABASE_URL` | 数据库地址（compose 里拼好了，指向 `postgres` 服务；`.env` 没设密码时镜像里的 `personad` 从 `data/docker/db-password` 补上） |
| `JARVIS_PUBLIC_URL` | 手机访问 hub 的地址，配对二维码里的就是它；没设时用 `hub.json` 的 `public_url`（第一次启动写的是空的） |
| `JARVIS_DB_PASSWORD` | compose 用：数据库密码；不设时第一次启动生成，见上「装」 |
| `JARVIS_LISTEN` | hub 监听的地址（默认 `:18660`；改它要同时改 compose 的端口映射和镜像的健康检查） |
| `JARVIS_LOCAL_NODE=0` | 第一次启动不写「本机」那台机器（镜像里已设） |
| `JARVIS_ADMIN_ACCESS`、`JARVIS_ADMIN_CIDRS` | 管理界面（`/admin`）谁能打开：`loopback` / `lan` / `cidrs`（网段逗号分隔）/ `any`，见下面「管理界面」。没设时入口脚本打印一行提醒，按 hub.json 的 `admin.access`（没写就是 `lan`） |
| `JARVIS_NODE_BINARIES` | 「重新部署」用的 node 构建在哪（镜像里是 `/opt/jarvis/nodes`；原生方式是 `~/.persona/bin/nodes`） |
| `PUID` / `PGID`、`TZ` | 见上（`PUID` / `PGID` 不设时取 `data/` 的属主） |
| `JARVIS_IMAGE`、`JARVIS_POSTGRES_IMAGE`、`JARVIS_PORT`、`JARVIS_TLS_PORT` | compose 用：两个镜像从哪拉、hub 的 HTTP / TLS 端口映射到宿主的哪个端口。`JARVIS_IMAGE` 也传进 hub 容器：hub 查它的标签，看有没有新镜像（见下「升级」） |
| `JARVIS_REGISTRY_TOKEN` | 镜像是私有的时候，一个能读它的 token（GitHub：`read:packages` 的 Personal Access Token），hub 拿它查新镜像；公开的镜像不用 |

原生方式也认这些变量（例如 systemd 的 `Environment=`），只是平时用不着。

## 升级

hub 不会自己换镜像，有新镜像时 App 里提示（`docs/hub-update.md`「Docker」）：hub 每 6 小时（设置 › hub 里「检查更新」随时）问一次 registry
`JARVIS_IMAGE` 的标签，最新的正式版本（`X.Y.Z`，不算 `-rc`、`edge`、`sha-…`）比自己新，Agents 页顶上出现「hub 有新镜像 X」，
设置 › hub 里是要运行的命令（可复制）：

```sh
docker compose pull && docker compose up -d
```

`.env` 里 `JARVIS_IMAGE` 钉在某个版本（`…:1.2.0`）的，`pull` 拉不到新的：先把标签改成新版本（App 里给出整行），再运行上面的命令。
手动跑出来的 `edge`、`sha-…` 镜像版本号是 `git describe`，比不出新旧，只显示最新的正式版本是哪个。
镜像是私有的、又没设 `JARVIS_REGISTRY_TOKEN`，设置 › hub 里说查不了、该设什么。
从 0.1.1 及更早的版本升上来，App 里不会提示，要手动换：那时仓库还叫 jarvis，镜像在 `ghcr.io/<owner>/jarvis`，0.1.2 起在
`ghcr.io/<owner>/persona`，旧名字下不会再有新版本。把 `.env` 的 `JARVIS_IMAGE` 改成新名字和版本，`compose.yaml` 换成新的
（它把 `JARVIS_IMAGE`、`JARVIS_REGISTRY_TOKEN` 传给 hub，之后的新版本才提示得出来），再运行上面的命令。

## 网络

- 容器默认在 Docker 的桥接网络里：tailcat 打洞多隔了一层 NAT，大多数情况仍能直连，不行就走中继。想更容易直连可以给 hub 用
  `network_mode: host`（同时去掉 `ports`）。
- hub 默认是 HTTP，只该在内网、VPN 或 HTTPS 反向代理后面访问，不要把 18660 直接暴露到公网。

### 同一台机器上的反向代理

hub 把自己容器的网关当公网（[network.md](network.md)「来源判断与反向代理」）：公网来的明文请求回 `403 tls_required`、不能用主 token。
NAS 上常见的两种反向代理接法，连 hub 时来源都会变成网关，要按下面接：

- **代理在另一个容器里**（Caddy、nginx、Nginx Proxy Manager…）：不要经宿主机映射的端口连 hub（`<NAS 地址>:18660`）。代理容器在别的
  Docker 网络里，转过去时源地址被 MASQUERADE 成 hub 所在网络的网关。让 hub 也加入代理的网络，代理按别名连：

  ```yaml
  services:
    hub:
      networks:
        default:            # 不能漏：hub 经它连 Postgres
        proxy:
          aliases: [persona-hub]
  networks:
    proxy:
      name: <代理所在的网络>  # docker network ls
      external: true
  ```

  代理里写 `persona-hub:18660`，hub 看到的是代理容器的私网地址。
- **代理跑在宿主机上**（NAS 系统自带的反向代理）：连 NAS 自己的局域网地址（`http://<NAS 局域网地址>:18660`），不要连 `127.0.0.1` / `localhost`：
  后者经 docker-proxy 转进去，来源是网关；前者经 iptables DNAT，来源是 NAS 自己的局域网地址。
- **代理上拦住外网的 `/admin`**：这样接好以后，经代理来的请求 hub 都当局域网，而管理界面首次设管理员密码对局域网不要验证码
  （[admin.md](admin.md)「首次设置」）。只让局域网的客户端打开 `/admin`，例如 Caddy：

  ```caddyfile
  @admin_outside {
  	path /admin /admin/*
  	not client_ip private_ranges
  }
  respond @admin_outside 403
  reverse_proxy persona-hub:18660 {
  	flush_interval -1
  }
  ```

- **在 NAS 上调 hub 的接口**同理：`curl http://127.0.0.1:18660/…` 会被当公网，换成局域网地址，或进容器调
  （`docker compose exec hub wget -qO- --header "Authorization: Bearer <token>" http://127.0.0.1:18660/api/v1/machines`）。

### Docker Desktop / OrbStack 的明文连接

如果健康检查正常，但 App、网页或内网反向代理请求返回 `403 tls_required`，先检查容器看到的真实对端。Docker Desktop 的 gvisor 网络可能把所有端口映射连接显示成同一个公网形态的地址，而不是 Docker 网络的网关；本机和局域网客户端也会如此。入口脚本识别到 Desktop 环境时会提示这一点，提示独立于 `JARVIS_ADMIN_ACCESS`。

保持客户端连接，在 compose 目录运行：

```sh
docker compose exec hub netstat -tn
docker compose logs hub
```

在 `netstat` 的本地地址列找 18660 端口，读取对应的 **Foreign Address**（去掉端口）。短连接可反复请求并重复查看；`docker network inspect` 的 `Gateway` 不足以确定转发来源，认证日志的 `source` 又是脱敏后的网段，不能直接拿来放行整个网段。

优先走 TLS：新装的 hub 已开 TLS、compose 已映射 18662；更早装的在 `data/hub.json` 加 `public.tls_listen`（[network.md](network.md)）。仅本机 HTTP 试用时：

- 先在 `.env` 里设置 `JARVIS_PORT=127.0.0.1:18660`，运行 `docker compose up -d` 重新创建端口映射。
- 把观察到的远端地址写进 `data/hub.json` 的 `security.trusted_cidrs`（IPv4 用单地址 `/32`，IPv6 用 `/128`，保留已有配置），再 `docker compose restart hub`。
- 需要管理界面时，另设 `JARVIS_ADMIN_ACCESS=cidrs`、`JARVIS_ADMIN_CIDRS=<实际转发来源>/32`（IPv6 为 `/128`），运行 `docker compose up -d`。管理界面的白名单和 API 的 `security.trusted_cidrs` 各管各的。

需要局域网客户端访问时，可把 `JARVIS_PORT` 绑定到明确的局域网地址，并用宿主机防火墙限定受控客户端后再信任转发来源；检查 Docker 实际转发规则，确保公网不能经该端口或反向代理进入。转发来源可能同时代表公网连接，单凭它是固定地址就加入白名单会放行这些连接；保持公网使用 TLS，也不要用 `allow_public_plaintext` 解决来源折叠。

## 管理界面

`<JARVIS_PUBLIC_URL>/admin` 是 hub 自带的管理界面（[admin.md](admin.md)）：第一次打开设管理员密码，之后在网页上加电脑、管设备、改网络。

- **访问范围**：在 `.env` 里选（`.env.example` 写了四个选项的意思），设了就以它为准，网页里不能改。
- **容器里的 `lan` 是「任何私网地址」，但网关除外**：桥接网络下容器只看得到 Docker 自己的网段（`172.x`），看不到 NAS 所在的局域网，
  分不出「同一网段」，所以放宽成任何私网地址；而 Docker 的用户态代理（docker-proxy）转进来的连接一律显示成网关地址（`172.x.0.1`），
  公网来的也一样（Docker 没开 IPv6 时的 IPv6 请求、宿主机上的反向代理转进来的），所以网关永远不算局域网。Linux 上局域网里的电脑经
  iptables DNAT 进来，容器看到的是它的真实地址，照常能打开。App 的接口同理：网关来的请求按公网处理（不能用主 token、明文接口回 `403 tls_required`），
  见 [network.md](network.md)「来源判断与反向代理」。
- **Docker Desktop / OrbStack**：转发来源可能是网关，也可能是 gvisor 提供的公网形态地址。按上面的「Docker Desktop / OrbStack 的明文连接」查实际对端；仅本机试用时，端口绑定 `127.0.0.1`，再把实际来源填入 `JARVIS_ADMIN_CIDRS`。API 的白名单需另设 `security.trusted_cidrs`。
- **`loopback`** 在容器里是容器自己：`docker compose exec hub wget -qO- http://127.0.0.1:18660/admin` 这类用法，平时基本用不上。
- **`any`** 要 hub 开 TLS（`data/hub.json` 的 `public.tls_listen`；第一次启动写的 `hub.json` 已经开了，compose 也映射了 18662；更早装的要自己加上）。
- 忘了密码：`docker compose exec hub personad hub admin reset-password`。
- 「重启 hub」按钮在容器里可用：hub 退出后由 `restart: unless-stopped` 拉起来。

## 从原生方式迁过来

1. 原来的机器上停 hub（`systemctl --user stop jarvisd`），导出数据库：
   `docker exec jarvis-postgres pg_dump -U jarvis -Fc jarvis > jarvis.dump`。
2. 把原来的实际运行目录（新默认 `~/.persona`，已有部署也可能仍是兼容目录 `~/.jarvis`）拷成 NAS 上的 `data/`（`bin/` 可以不要），`hub.json` 里删掉 `kind: local` 那台（那台机器要继续跑 Agent
   的话改成 `ssh` 或 `tailcat`），`database_url` 不用管（环境变量覆盖）。
3. `docker compose up -d postgres`，导入：`docker compose exec -T postgres pg_restore -U jarvis -d jarvis --clean --if-exists < jarvis.dump`，
   再 `docker compose up -d`。token 不变，已配对的手机只要把地址改成新的（或重新扫码）。

## 验证

`.github/workflows/hub-image.yml` 在每个改到服务端（含 `sdk/go`）、网页或镜像的 PR 上构建镜像并用 compose 起一遍：健康检查、配对、
tailcat key、网页版、`hub.json` 的属主。PR 上只构建 linux/amd64 那个 node（`--build-arg NODE_PLATFORMS=linux/amd64`，冒烟只跑它），
发布和本机默认四个都构建。本机（OrbStack / Docker Desktop）同样可以：

```sh
docker build --build-arg VERSION=dev-local -t jarvis-hub .
cd deploy/docker && cp .env.example .env    # JARVIS_IMAGE=jarvis-hub，JARVIS_PUBLIC_URL 填本机地址
docker compose up -d
```
