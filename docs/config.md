# 配置（让别人也能用这个仓库）

仓库里不写死任何一个人的机器、账号、域名或偏好。个人的东西都在两份配置里，代码和脚本只读配置：

| 配置 | 位置 | 谁读 | 管什么 |
| --- | --- | --- | --- |
| 构建 / 部署配置 | 开发机上的 `~/.config/persona/config.json`（可用 `PERSONA_CONFIG` 指到别处，旧 `JARVIS_CONFIG` 兼容），模板是仓库根目录的 `config.example.json` | `scripts/*`（经 `scripts/persona_config.py`）、XcodeGen（经生成的 `apple/Config.xcconfig`） | App 的 bundle id 和团队、App Store Connect 账号、TestFlight 测试员、hub 在哪台机器 / 地址、要部署的机器、模拟器 |
| hub 运行配置 | hub 所在机器的 `~/.persona/hub.json`（0600，含 token、数据库密码，第一次运行 `personad hub` 自动生成） | `personad hub` | 监听地址、对外地址、接入哪些机器、助手是谁的助手和它该知道什么、Gmail 代理、AI 网关、lark-connector |

配置放在仓库外面：发布在干净的 worktree 里跑（`docs/release.md`），worktree 里没有仓库外的东西就会缺；也避免误提交。
`apple/Config.xcconfig` 和 `CLAUDE.local.md` 在 `.gitignore` 里。

## 命名与兼容

产品名统一为 **Persona**（原 Jarvis），仓库为 `nwverse/persona`。本文的新默认名称适用于 #277 的迁移版本；仅更新文档不会改变已经安装的二进制或脚本，旧版本仍使用旧入口。

| 用途 | 新名称（优先） | 兼容名称 |
| --- | --- | --- |
| 服务端二进制 | `personad` | `jarvisd`；现有子命令语义不变 |
| 默认运行目录 | `~/.persona` | `~/.jarvis`；升级时保留原配置、密钥、知识库和配对身份 |
| 默认构建 / 部署配置 | `~/.config/persona/config.json` | `~/.config/jarvis/config.json` |
| 配置文件覆盖 | `PERSONA_CONFIG` | `JARVIS_CONFIG` |
| 已迁移的环境变量 | 对应的 `PERSONA_*` | `JARVIS_*`；新旧同时提供时新名优先，包括显式空值 |
| 配置脚本 | `scripts/persona_config.py` | `scripts/jarvis_config.py` 软链到同一实现 |
| 服务运行根解析器 | `scripts/persona_identity.py --service hub\|node\|link` | 恢复该角色已安装服务的根，不读构建配置；环境文件只提取 HOME，旧无 HOME 的服务继续原默认存储 |
| DEBUG App 命令工具 | `scripts/app/personactl.py` | `scripts/app/jarvisctl.py` 同一实现，原命令与缓存位置保留 |

源码路径、Xcode 工程 / scheme 与系统身份的完整清单见 [兼容迁移](rename.md#源码位置构建入口与兼容名字)。#773 的 [按服务恢复运行目录](rename.md#部署脚本按服务恢复运行目录) 说明 role、reset / drop-in / XDG、SameFile 和外部 EnvironmentFile / UnsetEnvironment 的边界；后两者只处理 HOME 字段与移除规则，不执行或返回其他环境字段；必需文件缺失或根字段无法解析时停止，也可显式指定实际根。新旧 URL scheme 的接收与旧格式生成分别验证，不以文档改名代替客户端验收。

旧目录的兼容和迁移由运行时 / 安装器负责。升级前备份实际使用的数据目录与 Postgres；不要仅为改名手动删除旧目录、合并两份目录或重新生成密钥。两套目录已经同时存在、或者使用自定义路径时，先确认实际加载的是哪一份，避免启动一个空 hub。文档中的历史测量、签名 profile、源码路径、包 ID、协议字段、缓存键以及 systemd / launchd 名称仍可能保留 Jarvis；它们不是新的对外产品名，也不应靠全文替换迁移。

Bundle ID、App Group、钥匙串组、签名 profile、App Store 记录、数据库角色 / 库 / 容器 / 数据卷、公开域名、安装 / 中继地址均保留已有身份。形象（`persona`）还是名字 / 头像 / 状态圆点的概念，不随产品改名重命名。

下文仍列出旧 `JARVIS_*`、xcconfig / Gradle 标识符和协议名供兼容配置及查源码；这不表示每一个代码标识符都能机械换成 `PERSONA_*`。具体入口以迁移版本实现为准。

## 快速开始：接入多台机器

一台常开的 Linux 机器跑 hub，其他电脑都由 hub 通过 SSH 接入。其他电脑上不用常驻服务：hub 连上时执行
`ssh <主机> ~/.persona/bin/personad node --stdio`，在 SSH 的标准输入输出上跑 node 协议，断了自动重连（`server/internal/hub/machine.go`）。

### 准备

| 机器 | 要有 |
| --- | --- |
| hub 机器（Linux） | docker（Postgres 容器；不想装 Docker 就用系统自带的 Postgres，见 `docs/server.md`「数据库」）、systemd 用户服务、herdr；手机能访问 hub：同一网络、用户自配 VPN，或公网直连（优先 IPv6，放行 TLS 端口）。放在 NAS 上、或只当 hub 不跑 Agent 时，也可以整个用 Docker 跑（`docs/docker.md`），下面的第 1、2 步只用来往各台电脑装 `jarvisd` |
| 每台要接入的电脑 | Claude Code（或 Codex）；`~/.persona/bin/personad` 和 herdr（第 2 步部署脚本会拷过去；herdr 没有的话用它的官方安装器装到 `~/.local/bin`，用别的引擎不想装就 `JARVIS_SKIP_HERDR=1`） |
| SSH | hub 机器能**免密码**登录每台电脑（hub 用 `BatchMode=yes`，只能用密钥），在 hub 机器的 `~/.ssh/config` 里给每台起个别名；开发机也要能 SSH 到每台（部署用） |

在 NAT 后面、hub 连不进去的电脑（比如家里的 Mac、另一个内网里的机器），用 **tailcat**（`docs/server.md`「tailcat 打洞」）：
那台电脑上装常驻 node、跑一次 `personad node tailcat init`，把打印的地址填进 hub.json 那台的 `tailcat_addr`。两边都不需要公网 IP、
开端口或 Tailscale 账号；hub 先试 ssh，连不上再走 tailcat。也可以照旧从那台电脑建一条到 hub 机器的反向隧道
（`ssh -R 2222:localhost:22 hub机器`，用 launchd / autossh 保活），hub 机器的 `~/.ssh/config` 里把别名指到 `localhost:2222`。

### 步骤

1. **开发机：写构建 / 部署配置** `~/.config/persona/config.json`（从 `config.example.json` 复制，字段见下一节）。
   和接入机器有关的是这两段：

   ```json
   "hub": { "machine": "server", "url": "http://server.example.com:18660" },
   "machines": [
     { "id": "server", "ssh": "server", "os": "linux" },
     { "id": "laptop", "ssh": "laptop", "os": "darwin", "hostnames": ["MyMacBook"] }
   ]
   ```

   `ssh` 是**开发机** SSH 到这台的主机名；`os` 决定装哪个二进制；`hostnames` 是这台自己的主机名（`hostname -s`，macOS 上也可以写 LocalHostName），部署脚本跑在这台上时据此直接本地拷贝、不走 SSH。
   **在哪台上跑部署，就给哪台写上 `hostnames`**：没写的机器一律当成远端经 SSH 安装（跑部署的这台没写，就 SSH 连自己）。
   开发机连不上、只有另一台（通常是 hub 机器）连得上的电脑（密钥只在那台上、路由不通）写 `"via": "<那台的 id>"`：
   部署时先拷到那台、再从那台装过去，这时 `ssh` 是**那台**上的主机名。

2. **部署**：`./scripts/deploy/deploy.sh`。编译 Linux / macOS 两份 `personad`，按 `os` 装到每台电脑的 `~/.persona/bin/`；在 hub 机器上
   起 Postgres 容器、把 `personad hub` 装成 systemd 用户服务并启动。

3. **hub 机器：告诉 hub 接入哪些电脑**。第一次启动会生成 `~/.persona/hub.json`（0600），里面只有它自己。补上其他电脑和手机访问的地址：

   ```json
   "public_url": "http://server.example.com:18660",
   "machines": [
     { "id": "server", "name": "服务器", "kind": "local", "icon": "server" },
     { "id": "laptop", "name": "笔记本", "kind": "ssh", "ssh_host": "laptop", "icon": "laptop" }
   ]
   ```

   `ssh_host` 是 **hub 机器** SSH 到这台的主机名（两边各自的 `~/.ssh/config`，可以和第 1 步的 `ssh` 不同）；`id` 最好和第 1 步一致；
   `name` 是 App 里显示的名字；`icon` 取 `server` / `laptop` / `desktop`。`personad` 不在 `~/.persona/bin/personad` 时用 `remote_bin` 指定。
   顺便可以填 `assistant`（见下文 hub.json 一节）。公网直连另加 `public.tls_listen`、`public.advertise`，按 [网络文档](network.md) 放行 TLS 端口；`public_url` 保留旧 App 能用的地址。

4. **重启 hub 让它读到新配置**：`./scripts/deploy/deploy.sh <hub 机器 id>`（或在 hub 机器上 `systemctl --user restart jarvisd`）。

5. **配对手机**：在 hub 机器上跑 `~/.persona/bin/personad hub pair`，用 App 扫二维码。命令通过本机运行中的 hub 生成临时 token；有效期 10 分钟，只能兑换一次设备 token，兑换后最多宽限 2 分钟供配对中的请求使用。`--legacy` 才会打印主 token。

6. **检查**：App「设置 › 机器」每台显示在线；「Agents」tab 里能看到每台电脑上 herdr 里的会话。某台不在线时先在 hub 机器上手动跑
   `ssh -o BatchMode=yes <ssh_host> '~/.persona/bin/personad node --stdio'`，看是 SSH 不通还是二进制不在。

之后加一台电脑：两份配置的 `machines` 各加一行，跑 `./scripts/deploy/deploy.sh <新机器 id> <hub 机器 id>`。

**推荐：其他电脑装 node 服务**（2026-11-02，[plan-hub-center.md](plan-hub-center.md) 第 0 节）：全家只装一台 hub，其他电脑不走 SSH，
装常驻 node 加连接器 jarvis-link，一条命令或一个 App 装好，见 [link.md](link.md)「装 node 服务」：

- Linux：`curl -fsSL <站点>/install.sh | sh -s -- --node --pair '<personad hub pair --machine <id> 给的链接>'`（这台去连 hub，`machines` 里是
  `{"id":"laptop","name":"笔记本","kind":"link","icon":"laptop"}`，hub 要开 `public.tls_listen`），或者 `… --node --listen`（让 hub 来连，
  打印的链接粘贴到 hub 管理界面「电脑 › 添加电脑 › hub 来连」，`machines` 里是 kind `service`）。
- Mac：装菜单栏 App「Persona Link」（`./scripts/app/mac-link.sh install`），它带着 personad，第一次运行就装好常驻 node；配对在它的菜单里。

不写 `ssh_host`，也不用给 hub 配 SSH。上面 1–6 步的 SSH 接入还能用，以后只当兼容。

### 目前的限制

- 部署脚本只会把 hub 装在 Linux 上（systemd + docker），hub 暂时不能放在 Mac 上。
- 机器列表要写两处：开发机的 `config.json` 管「部署到哪」，hub 的 `hub.json` 管「hub 接入哪些」。

## 构建 / 部署配置（config.json）

```json
{
  "app": { "bundle_id": "com.example.jarvis", "team_id": "ABCDE12345" },
  "app_store": {
    "app_id": "1234567890",
    "key_id": "ABC123DEFG",
    "issuer_id": "00000000-0000-0000-0000-000000000000",
    "key_path": "~/.appstoreconnect/AuthKey_ABC123DEFG.p8",
    "testers": [{ "email": "you@example.com", "first_name": "You" }],
    "feedback_email": "jarvis@example.com"
  },
  "hub": { "machine": "server", "url": "http://hub.example.com:18660" },
  "machines": [
    { "id": "server", "ssh": "server", "os": "linux" },
    { "id": "laptop", "ssh": "laptop", "os": "darwin", "hostnames": ["MyMacBook"] }
  ],
  "simulator": "",
  "xcode": ""
}
```

| 字段 | 用途 |
| --- | --- |
| `app.bundle_id` | iPhone App 的 bundle id；手表是 `<id>.watchkitapp`，测试是 `<id>.tests`。写进 `apple/Config.xcconfig` 的 `JARVIS_BUNDLE_ID` |
| `app.team_id` | 开发者团队，写进 `Config.xcconfig` 的 `DEVELOPMENT_TEAM`；导出时的 ExportOptions 也用它 |
| `app.app_group` | App 和灵动岛扩展共享文件的 App Group（形象和形象包的图，`docs/live-activity.md`「形象」），写进 `Config.xcconfig` 的 `JARVIS_APP_GROUP`。不写就是 `group.<app.bundle_id>`；要先在开发者网站上注册（`docs/release.md`「App Group」） |
| `app.push_relay` | 推送中继的地址（比如 `https://<中继域名>/relay`），写进 `Config.xcconfig` 的 `JARVIS_PUSH_RELAY`、Info.plist 的 `JarvisPushRelay`：App 向它登记，没有 APNs key 的 hub 经它推送（`docs/push-relay.md`）。不写就不登记，只有带 `apns.json` 的 hub 能推 |
| `app_store.*` | `release.sh` / `asc*.py`（`asc_cli.py` 把它们交给 App Store Connect CLI，`docs/release.md`「用 asc 发版」）：API key（`key_id`、`issuer_id`、`key_path`）、App 记录的数字 ID、内部测试员、TestFlight 反馈邮箱；`external` 是外部测试（群组名、公开链接开关和人数上限（`public_link_limit`，0 为不限）、隐私政策地址、另外按邮箱邀请的测试员、Beta App Review 联系人和备注，`docs/release.md`）。环境变量 `ASC_KEY_ID` 等仍可覆盖（key 文件路径 `ASC_KEY_PATH` 或 `ASC_PRIVATE_KEY_PATH`） |
| `hub.machine` | hub 跑在哪台机器（`machines` 里的 id）：`deploy.sh` 在它上面起 Postgres 和 systemd 服务，`test-server.sh` 在它上面跑测试，`sim.sh` 从它上面读 token |
| `hub.url` | `sim.sh` 配对模拟器用的地址；示例中的 HTTP 仅用于内网 / VPN，公网入口另配 `hub.json` 的 `public` |
| `machines[]` | `deploy.sh` 要装 `personad` 的机器：`ssh` 是 ssh 主机名，`os` 决定装哪个二进制（`linux` / `darwin`），`hostnames` 是这台机器自己的主机名（在它上面跑部署时直接拷贝，不走 ssh；跑部署的那台要写上，没写的一律走 ssh） |
| `simulator` | `sim.sh` 默认的模拟器 UDID（`SIM=<udid>` 覆盖） |
| `publish.site_url` | `publish.sh` 把 `personad` 和 `install.sh` 发布到哪（`site/` 那个 worker 的域名，`docs/release.md`「发布 personad」）；不写就不发布 |
| `publish.wrangler` | 部署那个 worker 用的 wrangler 命令（比如包了鉴权的脚本）；不写就 `npx wrangler` |
| `xcode` | 可选：编译和发版用哪个 Xcode（`.app` 的路径），`persona_config.py env` 据此导出 `DEVELOPER_DIR`（环境里已有的优先）。适配 iPhone Duo 要 Xcode 27 及以上，旧版本编出来的 App 在 Duo 上不会延伸到状态栏和摄像头下面 |
| `android.application_id` | Android App 的 application id（`scripts/app/android.sh`，`docs/android.md`）；不写就用 `app.bundle_id` |
| `android.google_oauth_scheme` | Android App 接 Gmail 的 Google 登录回调用的 scheme：hub `google.client_id` 的反向 client ID（`docs/mail.md`「Android」）；不写就只能用应用专用密码加 Gmail |

`python3 scripts/persona_config.py env` 打印 shell 变量（脚本开头都 `eval` 它），`xcconfig` 生成 `apple/Config.xcconfig`；
`sim.sh` / `release.sh` 会自动生成。直接跑 `xcodegen` 或打开 Xcode 之前先跑一次 `python3 scripts/persona_config.py xcconfig`。
仓库里提交的 `project.yml` 和 `Info.plist` 只写 `$(JARVIS_BUNDLE_ID)` / `$(PRODUCT_BUNDLE_IDENTIFIER)`，钥匙串的 service 用运行时的 bundle id。

## Codex app-server 多实例

`hub.json` 的每台机器可以配置 `codex_homes`（目录数组）、`codex_servers`（`id`、`name`、`home`、`endpoint`）和可选的 `codex_discovery`（默认自动发现，设 false 只使用登记的来源）。例如：

```json
{
  "codex_homes": ["~/.codex-profile"],
  "codex_servers": [
    {"id": "desktop", "name": "桌面实例", "home": "~/.codex-profile", "endpoint": "unix://<socket 的绝对路径>"}
  ]
}
```

无需硬编码应用名：node 每 30 秒发现当前用户的 Codex/app-server 进程、`CODEX_HOME` 和已开启的 listen 入口，已知 home 下存在的官方 control socket 也可发现。已发现目录和命名实例由 hub 的 Postgres settings 记忆；进程/连接代际不持久化。私有路径只放运行配置，不提交配置实值。

Unix socket 与直接父目录必须属于当前用户且不对组/其他用户开放；里面走 WebSocket。TCP 入口只允许字面回环 IP，禁止外部主机、用户信息和查询凭据，也不使用系统 HTTP 代理。没有入口的 stdio 只读；不会启动 daemon、替换桌面应用或放宽文件权限。更多限制见 `codex-app-server.md`。

## hub 运行配置（hub.json）

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
  "security": { "allow_public_plaintext": false, "trusted_cidrs": [] },
  "name": "家里",
  "admin": { "access": "lan" },
  "token": "…",
  "database_url": "…",
  "machines": [
    { "id": "server", "name": "服务器", "kind": "local", "icon": "server" },
    { "id": "laptop", "name": "笔记本", "kind": "ssh", "ssh_host": "laptop", "icon": "laptop" }
  ],
  "assistant": {
    "owner": "你的名字",
    "notes": "- 代码在 `~/code`。\n- 工作数据用 `xxx-*` skills。",
    "machine": "server",
    "launcher": "claude"
  },
  "google": { "client_id": "…", "proxy": "" },
  "ai": {},
  "lark": {}
}
```

- 第一次运行只写本机一台（`kind: local`，id 是主机名），生成的 `public_url` 是本机主机名对应的 HTTP 地址；这只是内网兼容入口。TLS 默认不开启，上例显式开启 `:18662`（示例端口，可改），其他机器和域名自己配置。
- `listen` 是保留的 HTTP 监听，默认 `:18660`；`public_url` 会进入二维码的 `url`，也会进入候选列表。现有 iOS / Watch 仍需它指向可访问的内网 / VPN 地址。
- `public` 和 `security` 的逐项说明、默认值及放行方法见 [网络配置](network.md#hubjson-网络配置)。候选地址只在鉴权后下发。
- `security.proxy_protocol`（可选，默认不写）：hub 前面是同机的端口转发、内网穿透或 nginx `stream` 透传时，公网连接在 hub 看来都来自回环。
  写上 `{"from": ["127.0.0.1/32"]}`，这些上游的连接必须带 PROXY 头（v1 / v2），hub 按头里的真实来源判断；`listeners` 默认只有 `tls`。
  写法、规则和自查见 [network.md](network.md#proxy-protocol)。
- `name`（可选）：这台 hub 给人看的名字（比如「家里」），电脑的 jarvis-link（Mac 菜单栏）和以后的互联、多 hub 客户端都显示它。
  不写时用 hub 机器主机名的短名；主机名是 Docker 容器 id 或者 `JARVIS_LOCAL_NODE=0` 时用 `hub-<hub_id 前 6 位>`。手改文件要重启 hub；
  在管理界面「概览」里改的立刻生效（[admin.md](admin.md)）。
- `admin`（可选）：hub 管理界面（`<hub 地址>/admin`，[admin.md](admin.md)）。`access` 是谁能打开：`loopback` / `lan`（默认）/ `cidrs` / `any`
  （必须 TLS）；`cidrs` 是 `access: cidrs` 时放进来的网段；`session_hours` 是登录多久过期（默认 12）。放宽只能在 hub 机器上
  `personad hub admin access <范围>`（或部署脚本），网页里只能收窄；环境变量 `JARVIS_ADMIN_ACCESS` / `JARVIS_ADMIN_CIDRS` 优先于它。
  管理界面改 hub.json 时原子写、保留不认识的字段（[admin.md](admin.md)「改 hub.json」）。
- `mesh`（可选，hub 互联，[mesh.md](mesh.md)）：`discovery`（默认 true：在局域网里用 mDNS 广播自己、列出发现的 hub；Docker 桥接网络里收不到）、
  `path_switch_seconds`（默认 60：一台电脑有更好的路时，要一直更好这么久中心才换过去；断了的路马上换，不等）。互联的对端、组的中心和任期存在
  Postgres，不在这里。第一版的 `takeover_seconds` 不再用。
- `machines[].kind`：`local`（hub 自己这台）、`service`（hub 拨这台连接器的被动端口，「hub 来连」，见下一条）、`link`（这台的 jarvis-link
  主动连 hub，「电脑来连」，[link.md](link.md)）；`ssh`（hub 经 `ssh_host` 连过去）、`tailcat`（只走 tailcat，见下下一条）只为已有的配置保留，
  管理界面不再添加，迁移步骤见 [mesh.md](mesh.md)「SSH / tailcat：兼容」。同一台电脑可以配几项（比如 service 和 link 各一项），hub 按
  machine_uid 认出是同一台，只列一次、用最好的那条（[mesh.md](mesh.md)「一台电脑多条路」）。
- `machines[].address`、`machines[].proxy`（kind `service`）：连接器的 `主机:端口`（`jarvis-link pair --listen` 的链接里带着，管理界面「添加电脑 ›
  hub 来连」粘贴链接就填好）；`proxy` 可选，`socks5://[用户:密码@]主机:端口`，hub 经这个 SOCKS 代理去连（这台只能从某台机器后面到达时）。
  连接器的公钥指纹和 hub 给它的凭据不在 hub.json：配对时存进 Postgres（凭据用 `~/.persona/secret.key` 加密）。
- `machines[].tailcat_addr`（可选）：那台电脑 `personad node tailcat init` 打印的 `tc…` 地址，hub ssh 不过去时经 tailcat 连
  （`docs/server.md`「tailcat 打洞」）。地址是密钥（带 WireGuard 的预共享密钥），和 token 一样只放 hub.json。`kind: "tailcat"` 表示
  只走 tailcat、不试 ssh（没有 `ssh_host`）；`kind: "ssh"` 加 `tailcat_addr` 是先 ssh、不行再 tailcat。
- 环境变量 `JARVIS_DATABASE_URL`、`JARVIS_PUBLIC_URL`、`JARVIS_LISTEN`、`JARVIS_ADMIN_ACCESS` 覆盖同名的设置（不写回文件），Docker 部署靠它们
  （`docs/docker.md`「环境变量」）；`JARVIS_LOCAL_NODE=0` 时第一次运行不写本机那台。
- `tailcat.derp_map_url`（可选）：用自建的 DERP 中继（derper，做法见 `docs/server.md`「自建中继」）代替 Tailscale 的公共中继时，它的 DERP map 地址；各台电脑
  `init` 时要带同一个 `--derp-map-url`。
- `machines[].claude_config_dirs`（可选）：这台机器上除 `~/.claude` 以外的 Claude Code 配置目录（启动器设的 `CLAUDE_CONFIG_DIR`，
  比如 `["~/.config/claude-code/profiles/work"]`，`~/` 是那台机器的 home）。正在跑的 claude 用的目录 node 自己会找到并记进数据库，
  这里只需要写「平时不开、但想在列表里看到它的历史会话」的目录。反过来，不想看到的目录（自动化脚本自己的 `CLAUDE_CONFIG_DIR`）
  在 App 的设置 › Claude Code ›「屏蔽的配置目录」里屏蔽，存在数据库，不写 hub.json（`docs/claude-integration.md`「屏蔽配置目录」）。
- `machines[].claude_start_dir`（可选）：Persona 用 `claude` 启动器在这台机器上开 Agent（新建 Agent、待办派发、助手的 agent_start、助手对话）时带的
  `CLAUDE_CONFIG_DIR`，比如 `"~/.config/claude-code/profiles/work"`（`~/` 是那台机器的 home）。给「claude 登录在某个 profile 目录、不在 `~/.claude`」的电脑用：
  不写的话 Persona 开的 claude 用默认目录，那里没登录，一开口就回 `Not logged in · Please run /login`（#1459）。只管 `claude` 这一个启动器，
  `~/.zshrc` 的 alias / 函数和 Persona 管理的启动器照旧用它们自己的设置；这个目录也会自动加进上面找会话的目录。node 也要是这一版；手改 hub.json 要重启 hub，重连时生效。
- `machines[].botmux`（可选）：写 `false` 时那台电脑的 node 不列 botmux 的会话，也不跑 botmux（不跑 `botmux observe`、`botmux dashboard current`）；
  不写就和以前一样，装了 botmux 就列（`docs/engines.md`「botmux」）。node 也要是这一版；手改 hub.json 要重启 hub，重连时生效。
- `machines[].herdr_autostart`（可选）：写 `false` 时那台电脑的 node 不在 herdr 没运行时自己拉起它的 server；不写是开
  （`docs/engines.md`「herdr 没在运行时」）。**只是默认值**：用户在「设置 › 机器 › 引擎」里按机器选过之后，以那次选择为准
  （存在 hub 的 settings，`herdr_autostart/<机器 id>`），再改这里不影响已经选过的机器。
- `microsoft`（可选）：Outlook 的「使用 Microsoft 账号登录」（`docs/mail.md`「Outlook 接入方式」）。`client_id` 是默认的 client ID
  （一般不用写：用户在 App 里填自己在 Microsoft Entra 注册的那个，加密存 hub，优先于这里）；`redirect_uri` 默认
  `jarvis-mail-oauth://outlook`，改了要和 Entra 里登记的一致。
- `lark.token` / `lark.container`：lark-connector Agent API 的 token。优先写 `lark.token`；不写时才用 `lark.container`
  （lark-connector 所在的 docker 容器名，从它的环境变量 `lark-connector_AGENT_TOKEN` 读），两个都不写就不接飞书，没有默认容器名。
  配置的 token 整个进程期间都保留：lark-connector 未登录飞书时回 401 + `AUTH_*`（如 `AUTH_SESSION_EXPIRED`），这不是 token 错，
  hub 照样用这个 token 重试，用户在 lark-connector 里重新登录后收件箱和实时事件自己恢复，不用重启。
- `lark.account`（可选整数）：默认账号的 lark-connector 账号编号，`1` 为主账号，`2` 为第二账号；省略或 `0` 使用主账号。
  例如 `"lark": {"base_url": "http://127.0.0.1:<隧道端口>/agent/v1", "token": "<Agent Token>", "account": 2}`。
  账号编号来自 lark-connector `/accounts` 的 `id`，不是飞书用户 ID。允许范围为 1–999999999，账号不存在时上游报错，不会回退到主账号。
  lark-connector 上登录的其他账号不用配置，hub 都会读（收件箱、实时事件、App 的「消息」页切换，见 `docs/feishu.md`「多账号」）；
  默认账号是不带 `ol_account` 的请求用的那个，也是助手、知识库、`/agent` 用的那个。修改后重启 hub（`systemctl --user restart jarvisd`）。
  助手的 `ol` 在新进程启动时继承所选账号；已有助手进程保留启动时的环境，需要重新启动才切换。
  不同账号的服务端收件箱记录隔离并保留，切回后可继续查看；不会同时聚合多个账号。
- `lark.native_olk_token`（可选）：手机解锁与加密发送的独立授权，默认关闭，不从普通 Agent token 自动派生或读取。
  必须与 lark-connector 的 `lark-connector_NATIVE_OLK_TOKEN` 相同，并在 lark-connector 绑定实际飞书用户 ID；不会下发给 App 或助手。
  需要两侧升级并重启，详见 [加密消息部署与授权](lark-encryption.md#部署与授权)。
- `lark.cli`（可选）：飞书官方的开放平台命令行 lark-cli，作为 lark-connector 之外的第二种飞书来源（`docs/plan-lark-cli.md`）：
  `"lark": {"cli": {"bin": "lark-cli", "profile": ""}}`。`bin` 是命令名或绝对路径，默认 `lark-cli`，不是绝对路径时在登录 shell 里找
  （npm 全局装的常在 nvm 这类目录下，systemd 的 PATH 里没有）；`profile` 是 lark-cli 的 `--profile`，空的用它的默认档。
  应用和用户的登录都留在 lark-cli 自己那里（`~/.lark-cli`），不进 `hub.json`：在助手所在的机器上自己跑一次
  `lark-cli auth login --domain all`（全部领域的用户权限；没有浏览器加 `--no-wait --json`，在手机上点链接授权），一周内没用过要重新登录。
  现在（L1）只给助手用：它的 PATH 上有 `lark-cli`（带上配置的 `profile`），写操作过闸门（`docs/assistant.md`「飞书：lark-cli」）。
- `lark.prefer`（可选）：lark-connector 和 lark-cli 都配了时谁是主来源，`lark-connector`（默认）或 `cli`；现在只影响助手的提示词怎么分工。
- `lark_gate`（可选，老写法，照样生效；新规则写进下面的 `connector_gate`，`connector` 写 `lark`）：助手用 lark-connector skill 写飞书时的闸门规则，排在内置规则前面，第一条匹配的说了算：
  `[{"method":"POST","path":"/conversations/*/messages","action":"allow"}]`（`action` 是 `allow` / `confirm` / `deny`，可加 `body` 按顶层字段匹配；
  `docs/assistant.md`「飞书：lark-connector skill」）。lark-cli 的命令写成 `{"method":"CLI","path":"/im/+messages-send","action":"allow"}`
  （`path` 是命令的词用 `/` 连起来，`*` / `**` 通配同上），匹配上就不再看 lark-cli 自报的风险等级。
- `connector_gate`（可选）：连接器写操作（`github_comment` 这类）的闸门规则，第一条匹配的说了算，没有匹配的就在手机上逐次确认：
  `[{"connector":"github","action":"github_comment","match":{"repo":"example-org/*"},"decision":"allow"}]`
  （`connector`、`action` 可用 `*` 通配；`match` 按工具参数的顶层字段匹配，字符串也可以通配；`decision` 是 `allow` / `confirm` / `deny`，
  写错了 hub 不启动；`server/internal/connector/gate.go`，`docs/plan-connectors.md`）。插件清单里 `confirm_always` 命中的写操作，`allow` 规则也会被钳成 `confirm`（`docs/extensions.md`）。
  助手写飞书的规则也写这里：`connector` 写 `lark`，`action` 写「方法 路径」，如 `{"connector":"lark","action":"POST /conversations/*/messages","decision":"allow"}`、
  lark-cli 的 `{"connector":"lark","action":"CLI /im/+messages-send","decision":"allow"}`（路径里 `*` 一段、`**` 剩下的全部，`"*"` 是任何调用；`match` 按 body 的顶层字段）。
  它们排在老的 `lark_gate` 前面，再往后是内置规则（`docs/assistant.md`「飞书：lark-connector skill」）。
- `github`（可选）：GitHub 连接器。`{"auth": {"from_machine": "<机器 id>"}}`：用那台机器上 `gh` 的登录（hub 经 node 跑 `gh auth token`，
  token 只在 hub 内存里，不落库、不写日志、不给 App 和助手）。那台机器要保持 `gh` 登录，而且登录存在 `~/.config/gh/hosts.yml`
  （钥匙串里的经 ssh 读不到：`gh auth login --insecure-storage`）。`auth.host` 默认 `github.com`，GitHub Enterprise 的主机名写这里（还没做）。
  状态看 设置 › 连接器 › GitHub。这里的 `from_machine` 只是默认值：每个 hub 都有 GitHub，在 设置 › 连接器 › GitHub 的「从哪里登录」里
  可以换一台电脑的 gh 登录，或者用设备码登录（用内置的 GitHub CLI Client ID，用户不用注册 OAuth App，授权页上显示的是「GitHub CLI」，见 `docs/connectors.md`「二 · 7」）；
  App 里选过以后以 App 的为准（存在 hub 的设置里）。连接器要 `repo` 权限：gh 的默认登录就有，少了时页面会提示 `gh auth refresh -s repo`。
  `github.proxy`（可选）：发往 GitHub 的全部请求（设备码登录的取码和轮询在 `github.com`，API 在 `api.github.com`，还有 CI 日志）走的 HTTP 代理，
  `http://[用户名:密码@]主机:端口`，和 `google.proxy` 同一格式；不填就直连（不读进程的 `HTTPS_PROXY`）。有的网络只通 `api.github.com`、不通 `github.com`，
  这时设备码登录拿不到验证码，就要配它。填错了不会退回直连：GitHub 的每个请求都失败，设置页写明「hub.json 的 github.proxy 不对」。具体地址只写在 hub 自己的 hub.json 里。
  `github.inbox`（可选）是收件箱的默认规则（App 里改过以 App 为准，`docs/inbox.md`「降噪」）：`ci`（`off` / `default_branch` / `mine`（默认）/ `all`）、
  `muted_repos`（`["owner/name"]`）、`ignore_branches`（不提醒的分支，`path.Match` 模式，默认 `["train/*"]`）。
- `imessage`（可选）：信息连接器。`{"machine": "<机器 id>"}`：App 里能看哪台 Mac 的「信息」（经那台 Mac 的 node 只读 `~/Library/Messages/chat.db`，
  要给它完全磁盘访问权限，App 的 设置 › 连接器 › 信息 会说怎么开）。只写一台（同一个 Apple ID 的几台 Mac 消息是同一份）。
  App 里能看所有会话。开关「交给 Persona」（设置 › 连接器 › 信息，默认开，存在 hub）开着时，真正的会话（成员里有通讯录里的人、或者你说过话的）
  按 ROWID 增量进知识库和收件箱（和飞书一样分拣），助手有 `imessage_chats` / `imessage_read` / `imessage_search`；验证码打码。关掉就都停，
  hub 上已经存下的另外可以删。日志里没有号码和正文（`docs/plan-connectors.md`「五」）。
- `assistant.owner`：「对话」tab 的助手称呼你什么；`assistant.notes`：附在助手说明（`~/.persona/assistant/CLAUDE.md`）最后的
  Markdown，写只有你这套环境才有的东西：代码在哪、用哪些 skills / CLI 访问工作数据、哪些目录不能碰。
- 模型设置的日常入口是 **设置 › 模型供应商 / 模型配置**（`docs/model-config.md`）：前者管理连接和凭据，后者管理对话、分类、翻译、知识处理、`/agent`、定时任务等用途。旧 `ai`、`inbox.*_ai`、`kb.ai`、各启动器默认作为兼容基线保留；统一用途覆盖存 Postgres，不写回 `hub.json`。没有覆盖时保持原模型，不会因为打开设置而迁移成其他供应商。
- `assistant.machine` / `assistant.launcher`：助手默认跑在哪台机器、用哪个启动器（默认 hub 自己那台、`claude`）；App 设置里改过以后以 App 为准。
  `owner`、`notes`、`machine`、`launcher` 都是 hub 在握手时交给助手的默认值：改过的设置是助手自己的（settings `assistant`，只有它读写，`docs/assistant.md`「和 hub 的边界」）。
- `assistant.external`（默认 `false`）：助手的进程（`personad assistant`，`docs/assistant.md`「进程」）由别人起。默认 hub 自己把它当子进程起起来、退出了再起，
  每次启动给它一个新的总线 token（经 stdin）；设成 `true` 后 hub 不起它，在要跑它的机器上先用 `personad hub assistant-token` 生成一个 token（打印一次；再生成一次就换掉旧的），再
  `PERSONA_ASSISTANT_TOKEN=<token> PERSONA_ASSISTANT_HUB=<hub 地址> personad assistant`。它读的 hub.json 和环境变量同 hub：数据库是 `database_url`（`PERSONA_DATABASE_URL` 优先），
  不给 `PERSONA_ASSISTANT_HUB` 时连本机 `listen` 的端口。助手没连着时它的接口回 `503 assistant_offline`。
- `assistant.persona_view`（默认 `false`）：助手回答里的卡片写成 ```` ```persona-view ````（扩展的组件，`docs/extensions.md`「UI 版本 3」），不再写 `persona-card`。
  各端（iOS / Mac、安卓、网页）都画得了 UI 版本 3 以后再打开；`persona-card` 在所有端都照样能画。
- `ai`（2026-10-31 起没有默认值）：App 里几个直接调模型 API（不经过 Claude Code）的小功能用的模型：收件箱分拣、代写回复、粘贴文字识别邮箱配置。
  不写时这些用途自动用 hub 上能直接调用的接入（「设置 › 模型供应商」里带 Key 的，见 `docs/model-config.md`「编辑与继承」）；一个都没有才报「还没有能直接调用的模型」。
  `api` 取 `messages`（Anthropic Messages，默认）或 `responses`（OpenAI Responses，总是流式请求，兼容只接受 `stream: true` 的转发服务）；
  `base_url` 拼上 `/v1/messages` 或 `/v1/responses`；`api_key_file` 是 hub 机器上的 key 文件（0600，调用时才读）；`model`；
  `reasoning_effort` 只对 `responses` 生效（比如 `low`）。
- `agents.triage`：一轮停下后的分诊（[agent-work.md](agent-work.md)「开关」）。开关在 App 的「设置 › 会话分诊」里（存 hub 的 settings，默认全关），
  这里只放上限：`max_retries`（自动继续最多几次，默认 3）、`max_wakes`（叫醒最多几次，默认 6）、`settle_seconds`（停下多久才分诊，默认 15）；
  `enabled` / `auto_continue` / `wake` / `decide` 只是 App 里从没保存过时的初始值（不写就是关）。分诊是管理者（助手进程）做的，这一节经握手交给它，改了重启 hub 后生效。
- `notify`：「做完了」推送的时机（`hub/notify.go`，不写就用默认值）：`done_delay_seconds`（做完后等多久没人看才推，默认 60）、
  `long_turn_seconds`（多长的一轮算「长任务」，默认 60）、`background_hold_minutes`（等后台任务结束最多等多久，默认 120）、
  `live_metrics_seconds`（实时活动只有数字变了时，给一台设备推送的最短间隔，默认 60；`docs/live-activity.md`「推送预算」）。
  每台设备要哪些推送是 App 里各自设置的（设置 › 通知，存在 hub 的 `devices.prefs`），不在这里。
- `inbox.triage_ai` / `inbox.judge_ai`（可选，字段同 `ai`，没写的字段取 `ai` 的）：AI 分身的模型 1（分拣）和模型 2（要不要交给 Agent）；
  不写就都用 `ai`。`inbox.agent`：模型 3，`enabled`、`launcher`（Claude Code 启动器，默认助手的）、`machine`（默认助手的机器）、
  `concurrency`（1）、`timeout_minutes`（15）、`max_age_hours`（24）、`rules`（给模型 2 的个人规则）。见 `docs/inbox.md`「AI 分身」。

  ```json
  "inbox": {
    "triage_ai": { "base_url": "<网关地址>", "api_key_file": "~/.persona/inbox-ai.key", "model": "<模型>" },
    "judge_ai":  { "base_url": "<网关地址>", "api_key_file": "~/.persona/inbox-ai.key", "model": "<模型>" },
    "agent": { "enabled": true, "launcher": "<启动器>" }
  }
  ```
- `kb`：知识库（`docs/knowledge.md`）：`enabled`（默认开）、`text_days`（文字保留天数，365）、`attachment_days`（附件，30）、
  `ai`（压缩和打标的模型，字段同 `ai`；不写用 `inbox.triage_ai`，再没有用 `ai`）、`extra_dirs`（hub 机器上你自己维护的笔记目录，
  比如一个记事实 / 经验的 git 仓库：`kb_search` 也搜这些目录里的 Markdown（只读，跳过 `.git` 这类点开头的目录），助手的 CLAUDE.md 会提到它们；`~/` 是那台的家目录）。
  知识库是助手（管理者）的，hub 在握手时把这一节交给它（`ai` 除外：那是 hub 的模型配置读的），助手跑在别处也以 hub 这份为准；
  `assistant.external` 时 `extra_dirs` 是助手那台机器上的路径（`docs/knowledge.md`「谁管什么」）。
- `inbox.rules`：收件箱 AI 分拣的个人规则（自然语言，比如「某某找我的都算需要我」「某告警群只算噪音」），附在分拣提示词最后、优先于默认判断（`docs/inbox.md`）。
  连接器的条目带了自己的规则（`rules`，如插件设置的 `triage_rules`）时用那一份，不用这里的。
- `bots.feishu`（可选）：在飞书里和 Persona 对话的机器人的应用凭证 `app_id`、`app_secret`、`domain`（`feishu` / `lark` / 开放平台地址），
  给不想在 App 里填的人用；App 里（设置 › 连接器 › 飞书机器人）存过的优先，开关、配对、各项功能都在 App 里（`docs/bots.md`）。
- `plugins`：手写在 hub.json 里、hub 跑起来、转发给 App 的插件（语音对话等），`id`、`kind`、`command` + `args`（或只给 `socket`：跑在别处的插件）、`config`（插件自己的设置），见 `docs/extensions.md`「hub.json 里的插件」；用 `personad plugin install` 装的插件不写这里，登记在数据库；
  personad 自带的语音插件（GPT-Live）写 `"command": "jarvisd", "args": ["voice"]`，上游放 `config.providers`（`docs/extensions.md`「内置语音插件」）。

## DDNS（public.ddns）

拨号重连换了 IPv6 前缀后，用一个固定域名找回 hub（`docs/network.md`）。不写 `public.ddns` 就不开启。

```json
"public": {
  "tls_listen": ":18662",
  "advertise": ["https://hub.example.com:18662"],
  "ddns": {"provider": "cloudflare", "record": "hub.example.com", "zone_id": "<zone id>", "token_file": "<hub 机器上的 token 文件>"}
}
```

- `provider`：
  - `cloudflare`：更新 AAAA / A 记录，必须有 `zone_id` 和 `token_file`；`ttl` 是 1（自动，默认）或 30–86400。
  - `webhook`：向 `webhook_url` POST `{"record","ipv6","ipv4"}`；有 `token_file` 时作为 `Authorization: Bearer` 发送。
  - 阿里云 DNS、DNSPod 还没做。
- `record`：完整域名。
- `token_file`：只放 token 的文件，必须是普通文件、权限 0600。token 不写进 hub.json。
- **发布什么地址**：
  - IPv6 和候选列表用同一套规则：只发本机稳定的全局 IPv6，已经发布的地址只要还稳定就不换。
  - IPv4 只在 `public.advertise` 里显式写了公网 IPv4 时才发：本机看到的 IPv4 证明不了公网能连进来。
- **什么时候更新**：启动时更新一次，之后地址变化时再更新。失败从 1 秒开始翻倍退避，最长 5 分钟。日志只写失败原因，不写 token、URL 和响应内容。
- **它只更新 DNS**：不开监听、不放行防火墙。要让 App 用上这个域名，还要把带 TLS 端口的 HTTPS URL 写进 `public.advertise`，候选列表和二维码的 `alt` 里才有它。

## 给写代码的人

- 新功能里出现「某个人才有」的值（机器名、域名、账号、邮箱、路径、启动器、公司内部工具），加到上面两份配置之一，不要写进代码、
  文案或测试；界面文案说「hub 所在的机器」「你的各台电脑」，不说具体机器。
- 这台开发机的具体情况（有哪些机器、hub 地址、账号在哪）写在仓库根目录的 `CLAUDE.local.md`（不提交），不写进 `CLAUDE.md`。
