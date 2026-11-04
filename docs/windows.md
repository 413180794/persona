# Windows 上的 node（Orca 引擎）

让手机看到、驱动一台 Windows 电脑上 Orca 里的 Claude Code：这台电脑跑常驻 node（`personad node --attach`）和连接器 jarvis-link（主动模式，见 [link.md](link.md)），
hub 在别处（WSL2 / Docker / 另一台机器）。需求和分步见 #1090，Windows 支持的总览见 #1179。踩过的坑在 [lessons/windows.md](lessons/windows.md)。

## 现状

| 能力 | Windows（Orca） | 说明 |
| --- | --- | --- |
| 编译 | ✅ | `GOOS=windows go build ./...`；进程组、信号、文件锁、CPU 时间在 `internal/osproc` 和各包的 `_windows.go` |
| 常驻 node、jarvis-link 主动模式 | ✅ | `jarvis-link install` 装成登录时启动的计划任务，退出了自动重启、不弹窗口（下面「安装和开机自启」）；没有一键安装脚本和安装包（#1179） |
| 列出 Orca 会话、状态 | ✅ | 和 macOS 一样每秒一次快照 |
| 读对话记录、会话信息 | ✅ | `%USERPROFILE%\.claude\projects\<编码后的目录>\<id>.jsonl`，目录编码规则和 Claude Code 一样（非字母数字都换成 `-`，`C:\Users\…` → `C--Users-…`） |
| 发消息（多行一整条）、按键、打断 | ✅ | ConPTY 下括号粘贴照常 |
| 回答对话框（AskUserQuestion、权限） | ✅ | 读屏回答，同 macOS 上的 Orca |
| 关闭会话 | ✅ | |
| 从手机新建会话、继续未运行的会话 | ❌ | Orca 引擎在所有平台上都还不支持（[plan-orca-support.md](plan-orca-support.md)「第二期」），不是 Windows 的问题 |
| 端口转发、机器监控、Codex / TRAEX 发现 | ❌ | 依赖 `ps` / `lsof` / `/proc`，Windows 上安静地不可用（#1179） |
| node 自己起的进程：受管启动器、`codex` 启动、自更新 | ❌ | 用 `syscall.Exec`，Windows 上能编译但跑不通；Orca 引擎用不到 |
| 经 hub 更新常驻 node（[node-update.md](node-update.md)） | ❌ | node 报告不支持（「常驻 node 未由 launchd/systemd 管理」），App 里看得到原因；升级是再跑一次 `jarvis-link install --jarvisd <新的 personad.exe>` |
| 被动模式（hub 来连这台） | 没验证 | 能编译、`install --listen` 走同一条路；Windows 防火墙要自己放行端口（`install` 打印 `New-NetFirewallRule` 的写法） |

## 安装和开机自启

```powershell
cd server
go build -o personad.exe .\cmd\personad
go build -o jarvis-link.exe .\cmd\jarvis-link
.\jarvis-link.exe install --jarvisd .\personad.exe --pair '<hub 给的 jarvis://link?… 链接>'   # 配对过的不用 --pair
```

不要管理员。`install` 在 Windows 上依次做：

1. 带 `--pair` 先配对，`--listen` 改成被动模式（同 Linux，见 [link.md](link.md)「Linux（headless）」）。
2. 计划任务在跑就先结束它（跑着的程序文件换不掉），等 jarvis-link 放开运行锁。还有别的 jarvis-link 在跑（比如前台的 `jarvis-link run`）时提示停掉它。
3. 把自己复制成 `~/.persona/bin/jarvis-link.exe`；`--jarvisd` 给的 personad 复制成 `~/.persona/bin/jarvisd.exe`。和 Linux 不同，给了就换：
   Windows 上还没有经 hub 更新 node，再跑一次 `install` 就是升级。没给又没有 `jarvisd.exe` 时只装 jarvis-link（`--no-node` 同）。
4. 再放一份 `~/.persona/bin/jarvis-link-service.exe`：同一个程序，只把 PE 头里的子系统从控制台改成 GUI（`guicopy.go`）。
5. 写 `~/.persona/jarvis-link-task.xml`（UTF-16），`schtasks /Create /XML` 注册计划任务「Persona Link」，`schtasks /Run` 启动，等连上 hub（同 Linux，最多 20 秒）。

计划任务「Persona Link」：

- **当前用户，跟着登录会话跑**（`InteractiveToken`）。标准用户注册不了「不管是否登录都运行」的任务（S4U，真机上拒绝访问；存密码的那种要管理员），
  node 也得在用户会话里才看得到 Orca 和它的 agent。代价：没登录 Windows 时不在，注销就停，再登录时自己起来。
- **触发**：登录时；外加从安装那一刻起每 5 分钟一次。多实例策略是「忽略新的」：守护进程在跑时什么都不做，没了 5 分钟内拉回来。
  不用计划任务自己的「失败后重启」：它只管任务起不来，程序以非 0 退出不算（真机上试过）。登录触发器上的「重复」要到下次登录才开始，所以另加时间触发。
- 不限运行时间（默认 72 小时就结束任务）、用电池也跑、空闲结束也不停、正常优先级（默认 7 是「低于正常」，CPU 和磁盘都低）。
- **跑的是守护进程** `jarvis-link-service.exe supervise`（`supervise_windows.go`）：以 `CREATE_NO_WINDOW` 拉起 jarvis-link（`run --log ~/.persona/link.log`）
  和常驻 node（`jarvisd.exe node --attach`，日志 `~/.persona/node-attach.log`，满 10 MB 挪到 `.1`），谁退出了分别 5 秒、10 秒后重启（同 Linux 两个单元的 `RestartSec`）；
  它自己干了什么写 `~/.persona/link-service.log`（jarvis-link 崩溃时的输出也在这里）。一台电脑只跑一个守护进程（`~/.persona/link-service.run` 的文件锁）。
- 守护进程把自己放进一个「最后一个句柄关掉就杀掉里面所有进程」的 Job：守护进程没了（结束任务、再 install / uninstall、注销、崩溃），
  jarvis-link 和 node 跟着没，计划任务再拉起它时不会和留下的旧进程抢。

**为什么要一份 GUI 子系统的副本**：计划任务在用户会话里起控制台程序，会给它开一个控制台窗口（默认终端是 Windows Terminal 时开一个 Windows Terminal 窗口），
一直挂在桌面上；GUI 子系统的程序没有窗口，它再用 `CREATE_NO_WINDOW` 起的控制台程序有控制台、没有窗口。复制而不是另编一个：不管 `jarvis-link.exe`
是发布的还是自己 `go build` 的，守护进程都是同一个程序。没用的办法：`conhost --headless`（没有文档）、VBScript 包一层（VBScript 正在被 Windows 移除）、
`powershell -WindowStyle Hidden`（还是会闪一下窗口）。

| 命令 | Windows 上 |
| --- | --- |
| `jarvis-link status` | 多一行「后台服务：计划任务「Persona Link」· 状态 · 登录时启动」，状态是 schtasks 按系统语言给的（Running / 正在运行…），只显示不解析 |
| `jarvis-link uninstall [--purge]` | 结束并删掉计划任务（node 一起停），删 `jarvis-link-task.xml` 和 `jarvis-link-service.exe`；`--purge` 同 Linux，另删守护进程的日志和锁 |
| `jarvis-link node install` / `node uninstall` | 不单独装：node 在同一个计划任务里，用 `install`（带 node）/ `install --no-node` |
| `jarvis-link node status [--json]` | 计划任务里有没有 node，任务的状态 |

## hub 在这台电脑的 Docker Desktop 里

验收时 hub 和 node 在同一台 Windows 上：hub 用 Docker Desktop（WSL 2）跑，node 和 jarvis-link 在 Windows 这边。

- WSL 开了镜像网络（`.wslconfig` 里 `networkingMode=mirrored`）时，Docker 发布的端口只在 Windows 的 `127.0.0.1` 上能连，局域网和 Tailscale 的地址都连不上
  （要放开得改 Hyper-V 防火墙，要管理员）。
- 本机的 jarvis-link 连 `127.0.0.1`：hub 广告的候选地址里没有回环地址时手动加 `jarvis-link address add https://127.0.0.1:<hub 的 TLS 端口>`。
- 手机经 Tailscale 连：这台把 tailnet 上的端口转到本机，`tailscale serve --bg --tcp <端口> tcp://127.0.0.1:<端口>`（TCP 原样转发，TLS 仍由 hub 自己做，
  钉的还是 hub 的证书），hub.json 的 `public.advertise` 写上 `https://<这台的 Tailscale 地址>:<端口>`。
- 给手机配对新 hub 用 App 的「设置 › hub › 添加 hub」，不要点根配对链接：那会替换掉手机上原有的配对（[lessons/android.md](lessons/android.md)）。

## 怎么跑（手动，开发和验收用）

装了计划任务的机器先 `schtasks /End /TN "Persona Link"`（或者 `jarvis-link uninstall`），否则前台的和计划任务里的两个 jarvis-link、两个 node 会抢。

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

## 验证记录：安装和开机自启（2026-11-04，同一台 Windows 11 真机）

- 用 `jarvis-link install --jarvisd personad.exe` 换掉之前手工加的注册表 Run 项：计划任务注册、启动，几秒内连上 hub、本机 node 在线；
  hub 的 `GET /api/v1/machines` 里这台在线、引擎 Orca，`GET /api/v1/sessions` 里有 Orca 的 10 个窗格。
- 进程：svchost（计划任务）→ `jarvis-link-service.exe supervise` → `jarvis-link.exe run`、`jarvisd.exe node --attach`；两个子进程各有一个隐藏的 conhost，桌面上没有窗口。
- 杀掉 jarvis-link，5 秒后回来；杀掉 node，11 秒后回来；杀掉守护进程，2 秒内 jarvis-link 和 node 都没了，下一次 5 分钟的时间触发把它们一起拉回来，hub 那边这台重新上线。
- 之前用一个探测任务确认过：非管理员能注册 `InteractiveToken` 的任务、注册不了 S4U；GUI 子系统的副本和它用 `CREATE_NO_WINDOW` 起的子进程都没有控制台窗口，
  都在用户的会话里；退出码 1 的程序 `RestartOnFailure` 不会重启。
- 测试：`go test ./cmd/jarvis-link/` 在 Windows 和 Linux（`golang:1.27` 容器）上都过；`supervise_windows_test.go` 去掉 Job 时失败（子进程活过了守护进程）。
- 没验证：真的注销再登录时的登录触发（会关掉这台机器上在用的 Orca 和 Claude 会话；任务注册时登录触发器被接受了）；被动模式；手机界面（手机锁着，用的是 App 调的同一套 hub 接口；App 界面在下面第二步时验过）。

## 验证记录（2026-11-03，Windows 11 真机）

- Orca 1.4.242（runtime protocol 3）、Claude Code 2.1.318（原生 `claude.exe`）；hub 是本仓库的 Docker 镜像（Docker Desktop），手机经 `adb reverse` 连 hub 的 TLS 端口。
- `go test -tags orcalive -run OrcaLive ./internal/node/`（只读）：10 个 Orca 里的 Claude 窗格全部认出，会话 id 和 `sessions/<pid>.json` 一一对上，一次刷新约 50 毫秒。
- 经 hub：列出会话和状态、读对话记录、发一条多行消息（一整条提交）、AskUserQuestion 用读屏卡选项回答、关闭会话（只关了目标终端）。
- 真机 App（Android）：在设置 › hub › 添加 hub 加入测试 hub（原有配对不动），Agents 里看到这台 Windows 和上面的会话；在手机上发消息、两次在读屏卡上点选项回答
  AskUserQuestion（对话记录里是手机选的那项）、从会话菜单关闭 Agent（确认框写的是 Orca 的标签页名，只关了那一个终端）。第一次的卡读不出对话框，由此修了上面「读屏卡」那条。
- Linux（`golang:1.27` 容器）：`orcart`、`osproc`、`plugin`、`transcript`、整个 `node` 包，`hub` 里读屏卡 / Orca / 引擎 / link / 机器相关的测试：通过。
- Windows 上跑 `orcart`、`osproc`、`transcript` 的全部测试和 `node` 里 Orca / attach / 进程扫描相关的测试：通过。`node`、`link`、`jarvis-link` 包里还有一批测试本身写死了
  Unix（shell 脚本冒充可执行文件、`/tmp`、文件权限位），在 Windows 上失败，和这次改动无关，留给 #1179。
