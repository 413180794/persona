# 配置（让别人也能用这个仓库）

仓库里不写死任何一个人的机器、账号、域名或偏好。个人的东西都在两份配置里，代码和脚本只读配置：

| 配置 | 位置 | 谁读 | 管什么 |
| --- | --- | --- | --- |
| 构建 / 部署配置 | 开发机上的 `~/.config/jarvis/config.json`（可用 `JARVIS_CONFIG` 指到别处），模板是仓库根目录的 `config.example.json` | `scripts/*`（经 `scripts/jarvis_config.py`）、XcodeGen（经生成的 `apple/Config.xcconfig`） | App 的 bundle id 和团队、App Store Connect 账号、TestFlight 测试员、hub 在哪台机器 / 地址、要部署的机器、模拟器 |
| hub 运行配置 | hub 所在机器的 `~/.jarvis/hub.json`（0600，含 token、数据库密码，第一次运行 `jarvisd hub` 自动生成） | `jarvisd hub` | 监听地址、对外地址、接入哪些机器、助手是谁的助手和它该知道什么、Gmail 代理、AI 网关、lark-connector |

配置放在仓库外面：发布在干净的 worktree 里跑（`docs/release.md`），worktree 里没有仓库外的东西就会缺；也避免误提交。
`apple/Config.xcconfig` 和 `CLAUDE.local.md` 在 `.gitignore` 里。

## 快速开始：接入多台机器

一台常开的 Linux 机器跑 hub，其他电脑都由 hub 通过 SSH 接入。其他电脑上不用常驻服务：hub 连上时执行
`ssh <主机> ~/.jarvis/bin/jarvisd node --stdio`，在 SSH 的标准输入输出上跑 node 协议，断了自动重连（`server/internal/hub/machine.go`）。

### 准备

| 机器 | 要有 |
| --- | --- |
| hub 机器（Linux） | docker（Postgres 容器；不想装 Docker 就用系统自带的 Postgres，见 `docs/server.md`「数据库」）、systemd 用户服务、herdr；手机能访问它的 18660 端口（同一个网络或 VPN）。放在 NAS 上、或只当 hub 不跑 Agent 时，也可以整个用 Docker 跑（`docs/docker.md`），下面的第 1、2 步只用来往各台电脑装 `jarvisd` |
| 每台要接入的电脑 | herdr，以及 Claude Code（或 Codex）；`~/.jarvis/bin/jarvisd`（第 2 步部署脚本会拷过去） |
| SSH | hub 机器能**免密码**登录每台电脑（hub 用 `BatchMode=yes`，只能用密钥），在 hub 机器的 `~/.ssh/config` 里给每台起个别名；开发机也要能 SSH 到每台（部署用） |

在 NAT 后面、hub 连不进去的电脑（比如家里的 Mac、另一个内网里的机器），用 **tailcat**（`docs/server.md`「tailcat 打洞」）：
那台电脑上装常驻 node、跑一次 `jarvisd node tailcat init`，把打印的地址填进 hub.json 那台的 `tailcat_addr`。两边都不需要公网 IP、
开端口或 Tailscale 账号；hub 先试 ssh，连不上再走 tailcat。也可以照旧从那台电脑建一条到 hub 机器的反向隧道
（`ssh -R 2222:localhost:22 hub机器`，用 launchd / autossh 保活），hub 机器的 `~/.ssh/config` 里把别名指到 `localhost:2222`。

### 步骤

1. **开发机：写构建 / 部署配置** `~/.config/jarvis/config.json`（从 `config.example.json` 复制，字段见下一节）。
   和接入机器有关的是这两段：

   ```json
   "hub": { "machine": "server", "url": "http://server.example.com:18660" },
   "machines": [
     { "id": "server", "ssh": "server", "os": "linux" },
     { "id": "laptop", "ssh": "laptop", "os": "darwin", "hostnames": ["MyMacBook"] }
   ]
   ```

   `ssh` 是**开发机** SSH 到这台的主机名；`os` 决定装哪个二进制；`hostnames` 只在部署脚本就跑在这台上时有用（直接本地拷贝，不走 SSH）。
   开发机连不上、只有另一台（通常是 hub 机器）连得上的电脑（密钥只在那台上、路由不通）写 `"via": "<那台的 id>"`：
   部署时先拷到那台、再从那台装过去，这时 `ssh` 是**那台**上的主机名。

2. **部署**：`./scripts/deploy.sh`。编译 Linux / macOS 两份 `jarvisd`，按 `os` 装到每台电脑的 `~/.jarvis/bin/`；在 hub 机器上
   起 Postgres 容器、把 `jarvisd hub` 装成 systemd 用户服务并启动。

3. **hub 机器：告诉 hub 接入哪些电脑**。第一次启动会生成 `~/.jarvis/hub.json`（0600），里面只有它自己。补上其他电脑和手机访问的地址：

   ```json
   "public_url": "http://server.example.com:18660",
   "machines": [
     { "id": "server", "name": "服务器", "kind": "local", "icon": "server" },
     { "id": "laptop", "name": "笔记本", "kind": "ssh", "ssh_host": "laptop", "icon": "laptop" }
   ]
   ```

   `ssh_host` 是 **hub 机器** SSH 到这台的主机名（两边各自的 `~/.ssh/config`，可以和第 1 步的 `ssh` 不同）；`id` 最好和第 1 步一致；
   `name` 是 App 里显示的名字；`icon` 取 `server` / `laptop` / `desktop`。`jarvisd` 不在 `~/.jarvis/bin/jarvisd` 时用 `remote_bin` 指定。
   顺便可以填 `assistant`（见下文 hub.json 一节）。

4. **重启 hub 让它读到新配置**：`./scripts/deploy.sh <hub 机器 id>`（或在 hub 机器上 `systemctl --user restart jarvisd`）。

5. **配对手机**：在 hub 机器上跑 `~/.jarvis/bin/jarvisd hub pair`，用 App 扫二维码（或手动填地址和 token）。

6. **检查**：App「设置 › 机器」每台显示在线；「Agents」tab 里能看到每台电脑上 herdr 里的会话。某台不在线时先在 hub 机器上手动跑
   `ssh -o BatchMode=yes <ssh_host> '~/.jarvis/bin/jarvisd node --stdio'`，看是 SSH 不通还是二进制不在。

之后加一台电脑：两份配置的 `machines` 各加一行，跑 `./scripts/deploy.sh <新机器 id> <hub 机器 id>`。

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
| `app_store.*` | `release.sh` / `asc*.py`：API key（`key_id`、`issuer_id`、`key_path`）、App 记录的数字 ID、内部测试员、TestFlight 反馈邮箱；`external` 是外部测试（群组名、公开链接开关和人数上限、隐私政策地址、另外按邮箱邀请的测试员、Beta App Review 联系人和备注，`docs/release.md`）。环境变量 `ASC_KEY_ID` 等仍可覆盖 |
| `hub.machine` | hub 跑在哪台机器（`machines` 里的 id）：`deploy.sh` 在它上面起 Postgres 和 systemd 服务，`test-server.sh` 在它上面跑测试，`sim.sh` 从它上面读 token |
| `hub.url` | 手机访问 hub 的地址（`sim.sh` 配对模拟器用） |
| `machines[]` | `deploy.sh` 要装 `jarvisd` 的机器：`ssh` 是 ssh 主机名，`os` 决定装哪个二进制（`linux` / `darwin`），`hostnames` 是这台机器自己的主机名（在它上面跑部署时直接拷贝，不走 ssh） |
| `simulator` | `sim.sh` 默认的模拟器 UDID（`SIM=<udid>` 覆盖） |
| `publish.site_url` | `publish.sh` 把 `jarvisd` 和 `install.sh` 发布到哪（`site/` 那个 worker 的域名，`docs/release.md`「发布 jarvisd」）；不写就不发布 |
| `publish.wrangler` | 部署那个 worker 用的 wrangler 命令（比如包了鉴权的脚本）；不写就 `npx wrangler` |
| `xcode` | 可选：编译和发版用哪个 Xcode（`.app` 的路径），`jarvis_config.py env` 据此导出 `DEVELOPER_DIR`（环境里已有的优先）。适配 iPhone Duo 要 Xcode 27 及以上，旧版本编出来的 App 在 Duo 上不会延伸到状态栏和摄像头下面 |
| `android.application_id` | Android App 的 application id（`scripts/android.sh`，`docs/android.md`）；不写就用 `app.bundle_id` |

`python3 scripts/jarvis_config.py env` 打印 shell 变量（脚本开头都 `eval` 它），`xcconfig` 生成 `apple/Config.xcconfig`；
`sim.sh` / `release.sh` 会自动生成。直接跑 `xcodegen` 或打开 Xcode 之前先跑一次 `python3 scripts/jarvis_config.py xcconfig`。
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

- 第一次运行只写本机一台（`kind: local`，id 是主机名），对外地址是 `http://<主机名>:18660`；其他机器、域名自己加。
- `machines[].tailcat_addr`（可选）：那台电脑 `jarvisd node tailcat init` 打印的 `tc…` 地址，hub ssh 不过去时经 tailcat 连
  （`docs/server.md`「tailcat 打洞」）。地址是密钥（带 WireGuard 的预共享密钥），和 token 一样只放 hub.json。`kind: "tailcat"` 表示
  只走 tailcat、不试 ssh（没有 `ssh_host`）；`kind: "ssh"` 加 `tailcat_addr` 是先 ssh、不行再 tailcat。
- 环境变量 `JARVIS_DATABASE_URL`、`JARVIS_PUBLIC_URL`、`JARVIS_LISTEN` 覆盖同名的设置（不写回文件），Docker 部署靠它们
  （`docs/docker.md`「环境变量」）；`JARVIS_LOCAL_NODE=0` 时第一次运行不写本机那台。
- `tailcat.derp_map_url`（可选）：用自建的 DERP 中继（derper，做法见 `docs/server.md`「自建中继」）代替 Tailscale 的公共中继时，它的 DERP map 地址；各台电脑
  `init` 时要带同一个 `--derp-map-url`。
- `machines[].claude_config_dirs`（可选）：这台机器上除 `~/.claude` 以外的 Claude Code 配置目录（启动器设的 `CLAUDE_CONFIG_DIR`，
  比如 `["~/.config/claude-code/profiles/work"]`，`~/` 是那台机器的 home）。正在跑的 claude 用的目录 node 自己会找到并记进数据库，
  这里只需要写「平时不开、但想在列表里看到它的历史会话」的目录。
- `lark.token` / `lark.container`：lark-connector Agent API 的 token。优先写 `lark.token`；不写时才用 `lark.container`
  （lark-connector 所在的 docker 容器名，从它的环境变量 `lark-connector_AGENT_TOKEN` 读），两个都不写就不接飞书，没有默认容器名。
  配置的 token 整个进程期间都保留：lark-connector 未登录飞书时回 401 + `AUTH_*`（如 `AUTH_SESSION_EXPIRED`），这不是 token 错，
  hub 照样用这个 token 重试，用户在 lark-connector 里重新登录后收件箱和实时事件自己恢复，不用重启。
- `lark.account`（可选整数）：lark-connector 账号编号，`1` 为主账号，`2` 为第二账号；省略或 `0` 使用主账号。
  例如 `"lark": {"base_url": "http://127.0.0.1:<隧道端口>/agent/v1", "token": "<Agent Token>", "account": 2}`。
  账号编号来自 lark-connector `/accounts` 的 `id`，不是飞书用户 ID。允许范围为 1–999999999，账号不存在时上游报错，不会回退到主账号。
  修改后重启 hub（`systemctl --user restart jarvisd`），收件箱、默认飞书请求、媒体和实时事件统一切换，不需要更新 iOS。
  助手的 `ol` 在新进程启动时继承所选账号；已有助手进程保留启动时的环境，需要重新启动才切换。
  不同账号的服务端收件箱记录隔离并保留，切回后可继续查看；不会同时聚合多个账号。
- `lark_gate`（可选）：助手用 lark-connector skill 写飞书时的闸门规则，排在内置规则前面，第一条匹配的说了算：
  `[{"method":"POST","path":"/conversations/*/messages","action":"allow"}]`（`action` 是 `allow` / `confirm` / `deny`，可加 `body` 按顶层字段匹配；
  `docs/assistant.md`「飞书：lark-connector skill」）。
- `assistant.owner`：「对话」tab 的助手称呼你什么；`assistant.notes`：附在助手说明（`~/.jarvis/assistant/CLAUDE.md`）最后的
  Markdown，写只有你这套环境才有的东西：代码在哪、用哪些 skills / CLI 访问工作数据、哪些目录不能碰。
- 模型设置的日常入口是 **设置 › 模型供应商 / 模型配置**（`docs/model-config.md`）：前者管理连接和凭据，后者管理对话、分类、翻译、知识处理、`/agent`、定时任务等用途。旧 `ai`、`inbox.*_ai`、`kb.ai`、各启动器默认作为兼容基线保留；统一用途覆盖存 Postgres，不写回 `hub.json`。没有覆盖时保持原模型，不会因为打开设置而迁移成其他供应商。
- `assistant.machine` / `assistant.launcher`：助手默认跑在哪台机器、用哪个启动器（默认 hub 自己那台、`claude`）；App 设置里改过以后以 App 为准。
- `ai`（2026-10-31 起没有默认值，不写就关掉这些功能，调用时报「no model configured」）：App 里几个直接调模型 API（不经过 Claude Code）的小功能用的模型：收件箱分拣、代写回复、粘贴文字识别邮箱配置。
  `api` 取 `messages`（Anthropic Messages，默认）或 `responses`（OpenAI Responses，总是流式请求，兼容只接受 `stream: true` 的转发服务）；
  `base_url` 拼上 `/v1/messages` 或 `/v1/responses`；`api_key_file` 是 hub 机器上的 key 文件（0600，调用时才读）；`model`；
  `reasoning_effort` 只对 `responses` 生效（比如 `low`）。
- `notify`：「做完了」推送的时机（`hub/notify.go`，不写就用默认值）：`done_delay_seconds`（做完后等多久没人看才推，默认 60）、
  `long_turn_seconds`（多长的一轮算「长任务」，默认 60）、`background_hold_minutes`（等后台任务结束最多等多久，默认 120）、
  `live_metrics_seconds`（实时活动只有数字变了时，给一台设备推送的最短间隔，默认 60；`docs/live-activity.md`「推送预算」）。
  每台设备要哪些推送是 App 里各自设置的（设置 › 通知，存在 hub 的 `devices.prefs`），不在这里。
- `inbox.triage_ai` / `inbox.judge_ai`（可选，字段同 `ai`，没写的字段取 `ai` 的）：AI 分身的模型 1（分拣）和模型 2（要不要交给 Agent）；
  不写就都用 `ai`。`inbox.agent`：模型 3，`enabled`、`launcher`（Claude Code 启动器，默认助手的）、`machine`（默认助手的机器）、
  `concurrency`（1）、`timeout_minutes`（15）、`max_age_hours`（24）、`rules`（给模型 2 的个人规则）。见 `docs/inbox.md`「AI 分身」。

  ```json
  "inbox": {
    "triage_ai": { "base_url": "<网关地址>", "api_key_file": "removed-secret-value", "model": "<模型>" },
    "judge_ai":  { "base_url": "<网关地址>", "api_key_file": "removed-secret-value", "model": "<模型>" },
    "agent": { "enabled": true, "launcher": "<启动器>" }
  }
  ```
- `kb`：知识库（`docs/knowledge.md`）：`enabled`（默认开）、`text_days`（文字保留天数，365）、`attachment_days`（附件，30）、
  `ai`（压缩和打标的模型，字段同 `ai`；不写用 `inbox.triage_ai`，再没有用 `ai`）、`extra_dirs`（hub 机器上你自己维护的笔记目录，
  比如一个记事实 / 经验的 git 仓库：`kb_search` 也搜这些目录里的 Markdown（只读，跳过 `.git` 这类点开头的目录），助手的 CLAUDE.md 会提到它们；`~/` 是那台的家目录）。
- `inbox.rules`：收件箱 AI 分拣的个人规则（自然语言，比如「某某找我的都算需要我」「某告警群只算噪音」），附在分拣提示词最后、优先于默认判断（`docs/inbox.md`）。
- `plugins`：hub 跑起来、转发给 App 的插件（语音对话等，实现在各自的仓库），`id`、`kind`、`command` + `args`（或只给 `socket`：跑在别处的插件），见 `docs/plugins.md`。

## 给写代码的人

- 新功能里出现「某个人才有」的值（机器名、域名、账号、邮箱、路径、启动器、公司内部工具），加到上面两份配置之一，不要写进代码、
  文案或测试；界面文案说「hub 所在的机器」「你的各台电脑」，不说具体机器。
- 这台开发机的具体情况（有哪些机器、hub 地址、账号在哪）写在仓库根目录的 `CLAUDE.local.md`（不提交），不写进 `CLAUDE.md`。
