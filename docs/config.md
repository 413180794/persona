# 配置（让别人也能用这个仓库）

仓库里不写死任何一个人的机器、账号、域名或偏好。个人的东西都在两份配置里，代码和脚本只读配置：

| 配置 | 位置 | 谁读 | 管什么 |
| --- | --- | --- | --- |
| 构建 / 部署配置 | 开发机上的 `~/.config/jarvis/config.json`（可用 `JARVIS_CONFIG` 指到别处），模板是仓库根目录的 `config.example.json` | `scripts/*`（经 `scripts/jarvis_config.py`）、XcodeGen（经生成的 `ios/Config.xcconfig`） | App 的 bundle id 和团队、App Store Connect 账号、TestFlight 测试员、hub 在哪台机器 / 地址、要部署的机器、模拟器 |
| hub 运行配置 | hub 所在机器的 `~/.jarvis/hub.json`（0600，含 token、数据库密码，第一次运行 `jarvisd hub` 自动生成） | `jarvisd hub` | 监听地址、对外地址、接入哪些机器、助手是谁的助手和它该知道什么、Gmail 代理、AI 网关、lark-connector |

配置放在仓库外面：发布在干净的 worktree 里跑（`docs/release.md`），worktree 里没有仓库外的东西就会缺；也避免误提交。
`ios/Config.xcconfig` 和 `CLAUDE.local.md` 在 `.gitignore` 里。

## 快速开始：接入多台机器

一台常开的 Linux 机器跑 hub，其他电脑都由 hub 通过 SSH 接入。其他电脑上不用常驻服务：hub 连上时执行
`ssh <主机> ~/.jarvis/bin/jarvisd node --stdio`，在 SSH 的标准输入输出上跑 node 协议，断了自动重连（`server/internal/hub/machine.go`）。

### 准备

| 机器 | 要有 |
| --- | --- |
| hub 机器（Linux） | docker（Postgres 容器）、systemd 用户服务、herdr；手机能访问它的 18660 端口（同一个网络或 VPN） |
| 每台要接入的电脑 | herdr，以及 Claude Code（或 Codex）；`~/.jarvis/bin/jarvisd`（第 2 步部署脚本会拷过去） |
| SSH | hub 机器能**免密码**登录每台电脑（hub 用 `BatchMode=yes`，只能用密钥），在 hub 机器的 `~/.ssh/config` 里给每台起个别名；开发机也要能 SSH 到每台（部署用） |

在 NAT 后面、hub 连不进去的电脑（比如家里的 Mac），从它那边建一条到 hub 机器的反向隧道（`ssh -R 2222:localhost:22 hub机器`，
用 launchd / autossh 保活），hub 机器的 `~/.ssh/config` 里把别名指到 `localhost:2222`。

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
| `app.bundle_id` | iPhone App 的 bundle id；手表是 `<id>.watchkitapp`，测试是 `<id>.tests`。写进 `ios/Config.xcconfig` 的 `JARVIS_BUNDLE_ID` |
| `app.team_id` | 开发者团队，写进 `Config.xcconfig` 的 `DEVELOPMENT_TEAM`；导出时的 ExportOptions 也用它 |
| `app_store.*` | `release.sh` / `asc*.py`：API key（`key_id`、`issuer_id`、`key_path`）、App 记录的数字 ID、内部测试员、TestFlight 反馈邮箱；`external` 是外部测试（群组名、公开链接开关和人数上限、隐私政策地址、另外按邮箱邀请的测试员、Beta App Review 联系人和备注，`docs/release.md`）。环境变量 `ASC_KEY_ID` 等仍可覆盖 |
| `hub.machine` | hub 跑在哪台机器（`machines` 里的 id）：`deploy.sh` 在它上面起 Postgres 和 systemd 服务，`test-server.sh` 在它上面跑测试，`sim.sh` 从它上面读 token |
| `hub.url` | 手机访问 hub 的地址（`sim.sh` 配对模拟器用） |
| `machines[]` | `deploy.sh` 要装 `jarvisd` 的机器：`ssh` 是 ssh 主机名，`os` 决定装哪个二进制（`linux` / `darwin`），`hostnames` 是这台机器自己的主机名（在它上面跑部署时直接拷贝，不走 ssh） |
| `simulator` | `sim.sh` 默认的模拟器 UDID（`SIM=<udid>` 覆盖） |
| `xcode` | 可选：编译和发版用哪个 Xcode（`.app` 的路径），`jarvis_config.py env` 据此导出 `DEVELOPER_DIR`（环境里已有的优先）。适配 iPhone Duo 要 Xcode 27 及以上，旧版本编出来的 App 在 Duo 上不会延伸到状态栏和摄像头下面 |
| `android.application_id` | Android App 的 application id（`scripts/android.sh`，`docs/android.md`）；不写就用 `app.bundle_id` |

`python3 scripts/jarvis_config.py env` 打印 shell 变量（脚本开头都 `eval` 它），`xcconfig` 生成 `ios/Config.xcconfig`；
`sim.sh` / `release.sh` 会自动生成。直接跑 `xcodegen` 或打开 Xcode 之前先跑一次 `python3 scripts/jarvis_config.py xcconfig`。
仓库里提交的 `project.yml` 和 `Info.plist` 只写 `$(JARVIS_BUNDLE_ID)` / `$(PRODUCT_BUNDLE_IDENTIFIER)`，钥匙串的 service 用运行时的 bundle id。

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
- `machines[].claude_config_dirs`（可选）：这台机器上除 `~/.claude` 以外的 Claude Code 配置目录（启动器设的 `CLAUDE_CONFIG_DIR`，
  比如 `["~/.config/claude-code/profiles/work"]`，`~/` 是那台机器的 home）。正在跑的 claude 用的目录 node 自己会找到并记进数据库，
  这里只需要写「平时不开、但想在列表里看到它的历史会话」的目录。
- `lark_gate`（可选）：助手用 lark-connector skill 写飞书时的闸门规则，排在内置规则前面，第一条匹配的说了算：
  `[{"method":"POST","path":"/conversations/*/messages","action":"allow"}]`（`action` 是 `allow` / `confirm` / `deny`，可加 `body` 按顶层字段匹配；
  `docs/assistant.md`「飞书：lark-connector skill」）。
- `assistant.owner`：「对话」tab 的助手称呼你什么；`assistant.notes`：附在助手说明（`~/.jarvis/assistant/CLAUDE.md`）最后的
  Markdown，写只有你这套环境才有的东西：代码在哪、用哪些 skills / CLI 访问工作数据、哪些目录不能碰。
- `assistant.machine` / `assistant.launcher`：助手默认跑在哪台机器、用哪个启动器（默认 hub 自己那台、`claude`）；App 设置里改过以后以 App 为准。
- `ai`：App 里几个直接调模型 API（不经过 Claude Code）的小功能用的模型：收件箱分拣、代写回复、粘贴文字识别邮箱配置。
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
  `ai`（压缩和打标的模型，字段同 `ai`；不写用 `inbox.triage_ai`，再没有用 `ai`）。
- `inbox.rules`：收件箱 AI 分拣的个人规则（自然语言，比如「某某找我的都算需要我」「某告警群只算噪音」），附在分拣提示词最后、优先于默认判断（`docs/inbox.md`）。

## 给写代码的人

- 新功能里出现「某个人才有」的值（机器名、域名、账号、邮箱、路径、启动器、公司内部工具），加到上面两份配置之一，不要写进代码、
  文案或测试；界面文案说「hub 所在的机器」「你的各台电脑」，不说具体机器。
- 这台开发机的具体情况（有哪些机器、hub 地址、账号在哪）写在仓库根目录的 `CLAUDE.local.md`（不提交），不写进 `CLAUDE.md`。
