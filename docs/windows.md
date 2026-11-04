# Windows 上的 node（Orca 引擎）

让手机看到、驱动一台 Windows 电脑上 Orca 里的 Claude Code：这台电脑跑常驻 node（`personad node --attach`）和连接器 jarvis-link（主动模式，见 [link.md](link.md)），
hub 在别处（WSL2 / Docker / 另一台机器）。需求和分步见 #1090，Windows 支持的总览见 #1179。踩过的坑在 [lessons/windows.md](lessons/windows.md)。

## 现状

| 能力 | Windows（Orca） | 说明 |
| --- | --- | --- |
| 编译 | ✅ | `GOOS=windows go build ./...`；进程组、信号、文件锁、CPU 时间在 `internal/osproc` 和各包的 `_windows.go` |
| 常驻 node、jarvis-link 主动模式 | ✅ | 手动在前台跑（下面「怎么跑」）；开机自启、安装器还没有（#1090 第三步） |
| 列出 Orca 会话、状态 | ✅ | 和 macOS 一样每秒一次快照 |
| 读对话记录、会话信息 | ✅ | `%USERPROFILE%\.claude\projects\<编码后的目录>\<id>.jsonl`，目录编码规则和 Claude Code 一样（非字母数字都换成 `-`，`C:\Users\…` → `C--Users-…`） |
| 发消息（多行一整条）、按键、打断 | ✅ | ConPTY 下括号粘贴照常 |
| 回答对话框（AskUserQuestion、权限） | ✅ | 读屏回答，同 macOS 上的 Orca |
| 关闭会话 | ✅ | |
| 从手机新建会话、继续未运行的会话 | ❌ | Orca 引擎在所有平台上都还不支持（[plan-orca-support.md](plan-orca-support.md)「第二期」），不是 Windows 的问题 |
| 端口转发、机器监控、Codex / TRAEX 发现 | ❌ | 依赖 `ps` / `lsof` / `/proc`，Windows 上安静地不可用（#1179） |
| node 自己起的进程：受管启动器、`codex` 启动、自更新 | ❌ | 用 `syscall.Exec`，Windows 上能编译但跑不通；Orca 引擎用不到 |

## 怎么跑（手动，开发和验收用）

```powershell
cd server
go build -o personad.exe .\cmd\personad
go build -o jarvis-link.exe .\cmd\jarvis-link
.\personad.exe node --attach            # 常驻 node，另开一个窗口一直开着
.\jarvis-link.exe pair '<hub 给的 jarvis://link?… 链接>'
.\jarvis-link.exe run                    # 保持链路
```

hub 那边：hub.json 的 `machines` 里这台是 `kind: link`，开着 `public.tls_listen`；配对链接用 `personad hub pair --machine <id>`（Docker 部署是
`docker compose exec hub jarvisd hub pair --machine <id>`）。连上后在 App 里把这台的引擎选成 Orca（设置 › 机器 › 引擎）。

## 本地 socket

node 的 `node.sock`、`runtime.sock`、`node-attach.sock` 在 Windows 上照旧是 AF_UNIX 的 socket 文件（Windows 10 1803 起有），放在 `%USERPROFILE%\.persona\`。
没换成命名管道：Go 的 `net` 直接支持，hooks、jarvis-link、`--stdio` 转发的代码一行不改；Windows 11 真机上验证过监听、连接、半关闭（`CloseWrite`）、替换残留的 socket 文件。
限制：路径不能超过 108 字节（`%USERPROFILE%` 很深时会 `bind: invalid argument`）；`0600` 这类权限位在 Windows 上没有意义，靠用户目录本身的 ACL。

## Orca

- **传输**：Windows 版 Orca 的 `orca-runtime.json`（`%APPDATA%\orca\`）写的是 `named-pipe`（`\\.\pipe\orca-<pid>-…`）和 `websocket`，没有 `unix`。
  `orcart` 按平台挑：Windows 先 `named-pipe` 再 `unix`，其他平台只 `unix`；命名管道用已有依赖 `tailscale/go-winio` 拨号。协议、token、重读元数据的规则都不变。
  测试用的假 runtime（`orcarttest`）在 Windows 上也走命名管道，所以 `orcart` 和 `hostorca` 的测试在 Windows 上测的就是真实传输。
- **认窗格、认会话**：和 macOS 一样靠 claude / codex 进程环境变量里的 `ORCA_PANE_KEY`（缺了看 `ORCA_AGENT_PANE`），再读 `sessions/<pid>.json`。
  Windows 没有 `ps -E` 和 `/proc`：`procs_windows.go` 用 Toolhelp32 列进程，只对候选（`claude`、`codex`、`node`、`bun`）读 PEB 里的命令行、当前目录和环境块，
  环境里只留要用的几个变量（Orca 的 hook token 等一概不留）。
- **在前台**：Windows 没有终端的前台进程组，也没有「挂起」。改成「父进程链上没有同一窗格的 agent 进程」的才算（claude 自己起的 claude 不是终端显示的那个）；
  pid 会复用，所以祖先必须比自己先启动。同一窗格多个时 claude 在前、按启动时间排（Windows 的 pid 不递增，不能按 pid 排）。
- **读屏卡**：Orca 在 AskUserQuestion 的 PreToolUse hook 一触发就把会话报成 blocked，而 Windows 上起 hook 进程要几秒，对话框晚出来。
  hub 当时建的卡读不出对话框（「暂时读不出这个对话框」），之后会每 500 毫秒再看一次屏幕（最多 30 秒），对话框一出现就换成带选项的卡（`hub.upgradeScreenCard`，各平台通用）。

## shell

node 在 Windows 上跑启动器探测、`claude --version` 这类 shell 脚本时用 Git for Windows 的 bash（`loginShellProgram`：`CLAUDE_CODE_GIT_BASH_PATH`，
否则从 `git` 的位置和常见安装目录找），和 Claude Code 自己在 Windows 上一样。不用裸的 `bash`：服务拿到的 PATH 里 `System32\bash.exe` 在前，它是 WSL。
找不到 Git Bash 时这些命令起不来（机器信息里没有 Claude 版本），不会跑到 WSL 里去。

## 验证记录（2026-11-03，Windows 11 真机）

- Orca 1.4.242（runtime protocol 3）、Claude Code 2.1.318（原生 `claude.exe`）；hub 是本仓库的 Docker 镜像（Docker Desktop），手机经 `adb reverse` 连 hub 的 TLS 端口。
- `go test -tags orcalive -run OrcaLive ./internal/node/`（只读）：10 个 Orca 里的 Claude 窗格全部认出，会话 id 和 `sessions/<pid>.json` 一一对上，一次刷新约 50 毫秒。
- 经 hub：列出会话和状态、读对话记录、发一条多行消息（一整条提交）、AskUserQuestion 用读屏卡选项回答、关闭会话（只关了目标终端）。
- 真机 App（Android）：在设置 › hub › 添加 hub 加入测试 hub（原有配对不动），Agents 里看到这台 Windows 和上面的会话；在手机上发消息、两次在读屏卡上点选项回答
  AskUserQuestion（对话记录里是手机选的那项）、从会话菜单关闭 Agent（确认框写的是 Orca 的标签页名，只关了那一个终端）。第一次的卡读不出对话框，由此修了上面「读屏卡」那条。
- Linux（`golang:1.27` 容器）：`orcart`、`osproc`、`plugin`、`transcript`、整个 `node` 包，`hub` 里读屏卡 / Orca / 引擎 / link / 机器相关的测试：通过。
- Windows 上跑 `orcart`、`osproc`、`transcript` 的全部测试和 `node` 里 Orca / attach / 进程扫描相关的测试：通过。`node`、`link`、`jarvis-link` 包里还有一批测试本身写死了
  Unix（shell 脚本冒充可执行文件、`/tmp`、文件权限位），在 Windows 上失败，和这次改动无关，留给 #1179。
