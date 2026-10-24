# CLAUDE.md

Jarvis：只给自己用的 iPhone + Apple Watch 助手。「对话」tab 是能访问所有个人和公司数据的 AI 助手（example-machine 上一个专用目录里的 Claude），
「开发」tab 是在手机上用聊天的方式驱动各台电脑上 herdr 里的 Claude Code（实时流式看过程、
在手机上回答提问 / 审批计划 / 批准工具），并让 Claude 读写 iPhone 本地数据（通讯录、日历、提醒、照片、健康、位置……）和邮箱
（Gmail / 任意 IMAP+SMTP）。服务端 `jarvisd`（Go）部署在 example-machine，经 SSH 接入 example-machine / example-machine / mac-home。
只发 TestFlight（内部测试员 tester@example.com），**不做官网、不上架**。

## 约定（必须遵守）

- **每完成一个功能就自动提交并推送**（`git push origin master`），不用等用户要求。Conventional Commits
  （`feat(ios): ...`、`feat(server): ...`、`fix(watch): ...`、`docs: ...`、`chore(release): ...`、`test(...)`），
  作者 `chyroc <chyroc@qq.com>`（仓库已 `git config`）。只 `git add` 自己改过的文件；push 被拒先 `git pull --rebase` 再推。
- **随时沉淀经验**：踩坑、怪异行为、设计取舍当场写进本文件或 `docs/<主题>.md`。这条规则本身也要保留。
- 代码注释、提交信息用英文；文档用中文；App 界面文案用中文。
- **绝不访问 iCloud 目录**（`~/Library/Mobile Documents/...`）：这台 Mac 未解锁，一访问就卡死。需要里面的东西就直接问用户。
  App Store Connect API key 在 `~/.appstoreconnect/AuthKey_KEYID00001.p8`（不在 iCloud），发布证书在 `~/.appstoreconnect/signing/`。
- **密钥绝不入库**：hub token、数据库密码只存在 example-machine `~/.jarvis/hub.json`（0600）；邮箱凭据 AES-GCM 加密后存 Postgres，密钥文件不进数据库。
- 服务端状态全部在 example-machine 的 Postgres 容器 `jarvis-postgres`（`docs/server.md`），不要再往 `~/.jarvis/` 写 JSON 状态文件。
- 分工：设计、UI、交互、协议、核心链路由 leader（当前 Opus）实现；不吃模型智能的活（脚本、样板、邮件协议封装等）在 herdr
  当前 workspace 新开 tab 用 `cc_ds` 实现，leader 派发和验收。
- 质量标准：按 App Store 榜首水准设计和测试。每个界面都要在模拟器上截图检查（深色 / 浅色），手表界面也要截图，不能只看编译通过。

## 目录

| 路径 | 内容 |
| --- | --- |
| `server/` | Go 服务端 `jarvisd`（hub / node / hook / mcp 四种模式），见 `docs/server.md` |
| `ios/` | XcodeGen 工程：iOS App `Jarvis`、手表 App `JarvisWatch`、共享代码 `Shared/`、单元测试 |
| `scripts/` | 部署（`deploy.sh`）、发布 TestFlight（`release.sh`、`asc_*.py`） |
| `docs/` | 设计文档，索引见下 |

## 文档索引

| 文档 | 内容 |
| --- | --- |
| `docs/architecture.md` | 整体架构、数据流、为什么这样设计（herdr + hooks + transcript） |
| `docs/protocol.md` | hub HTTP / WebSocket API、hub↔node RPC、事件和交互模型（前后端契约） |
| `docs/server.md` | jarvisd 各模式、配置、部署、运维 |
| `docs/claude-integration.md` | 如何在 herdr 里启动 / 驱动 Claude、hook 回答 AskUserQuestion / ExitPlanMode 的实测结论 |
| `docs/mail.md` | 邮箱：IMAP/SMTP、Gmail（应用专用密码 / OAuth XOAUTH2）、凭据存储 |
| `docs/phone-tools.md` | 手机数据工具（phone_*）、权限、需要确认的写操作、iOS 限制 |
| `docs/ios.md` | iOS / watchOS App：信息架构、设计语言、手机数据工具 |
| `docs/intents.md` | Siri / 快捷指令（App Intents）：问 Claude、状态、批准第一个待处理 |
| `docs/release.md` | 签名、TestFlight 发布流程 |
| `docs/testing.md` | 测试命令、模拟器截图检查清单 |

## 机器

| id | 机器 | 接入方式 |
| --- | --- | --- |
| `example-machine` | 公司 example-machine hub-host（hub 所在） | 本机 |
| `example-machine` | 公司里的公司 Mac example-machine（也是开发机） | `ssh example-machine`（example-machine 反向隧道 2222） |
| `example-machine` | 家里的公司 Mac example-machine | `ssh example-machine`（2223） |
| `mac-home` | 家里的个人 Mac | `ssh mac-home`（2224，隧道暂未就绪） |

隧道见 `docs/config.md`。hub 入口 `http://hub.example.com:18660`（手机需在公司网络 / removed）。

## 构建与验证

- 服务端：`./scripts/test-server.sh`（在 example-machine 上连真实 Postgres 跑全部测试）；部署 `./scripts/deploy.sh [example-machine|example-machine|…]`。
- iOS：`./scripts/sim.sh [-openSession <machine>/<session>]` 编译、装到 iPhone 17 Pro 模拟器、自动配对真实 hub；截图 `xcrun simctl io <udid> screenshot`。
- 发布：`docs/release.md`。

## 当前状态（2026-10-24）

- hub 已部署在 example-machine（:18660），example-machine / example-machine / example-machine 在线，mac-home 隧道未装。
- 端到端跑通：手机新建会话、提问、计划审批、手机工具（`docs/testing.md`）。邮件模块上线，还没加账号。
- App Store Connect App ID 1234567890（Jarvis Herd），APNs 已开（`docs/release.md`）。待办：真机验证。
- 2026-10-24 服务端状态迁到 example-machine Postgres 容器 `jarvis-postgres`；新增「对话」tab（Jarvis 助手），TestFlight 202610242210。

## 经验

- herdr 里新开 tab 启动 claude 时，若 cwd 未被信任会先弹「trust this folder」，`agent prompt` 发的文字会被当成选择 → 选了 No 退出。
  node 启动会话后要检查 blocked 屏幕（`docs/claude-integration.md`）。
- AskUserQuestion：PreToolUse hook 返回 `permissionDecision: allow` + `updatedInput.answers` 即可代答（2.1.310 实测）。
- ExitPlanMode：走 PermissionRequest（不走 PreToolUse）。`allow` 必须带 `updatedInput` + `updatedPermissions:[{type:setMode,...}]`
  才生效，单纯 `{behavior:allow}` 仍会弹 TUI 对话框；`deny` + `message` 会把反馈作为 tool_result 交给模型。
- `--settings <file>` 里的 hooks 只在启动时读取，改 matcher 要重启 claude；hook 脚本本身每次执行都重新读。
- `herdr agent wait` 可能在 herdr 识别出 agent 之前就返回 agent_not_found：启动后要轮询 `agent get`。
- example-machine 的 herdr 没装 claude integration，`agent list` 不带 session id（见 `docs/claude-integration.md` 的兜底）。
- 这台 Mac 屏幕锁着时 computer-use 拿不到模拟器窗口：用 DEBUG 启动参数（`docs/ios.md`）直达页面再截图。
- 开了 HealthKit entitlement 就必须有 `NSHealthUpdateUsageDescription`（哪怕只读），否则上传报 90683。
- Go 的 `UnixListener.Close()` 默认会删掉 socket 文件：重新部署时旧 node 退出会把新 node 的 `~/.jarvis/node.sock` 删掉，
  hook / MCP 全部静默失效（hook 连不上就放行给 TUI）。已 `SetUnlinkOnClose(false)` 并每 15 秒自检重建。
- 模拟器里上一次启动遗留的系统授权弹窗，卸载重装 App 也不消失，要 `simctl shutdown` 重启模拟器。
