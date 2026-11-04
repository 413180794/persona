# Windows 上的坑

改 Windows 上的 node（#1090）、在 Windows 机器上开发或测试 `personad` 之前读。设计和现状见 [windows.md](../windows.md)。

## 开发环境

- **仓库要用 LF 检出**：Git for Windows 默认 `core.autocrlf=true`，检出的 `.go` 全是 CRLF，`gofmt -l` 报一大片，脚本、golden 文件也会对不上。
  克隆后在这个仓库里 `git config core.autocrlf false && git config core.eol lf`，再 `git rm -q --cached -r . && git reset -q --hard` 重新检出（先提交或暂存自己的改动）。
- **Unix socket 路径不能超过 108 个字节**：Go 的 AF_UNIX 在 Windows 上能用，但路径一长 `bind` 就报 `invalid argument`（不是「不支持」）。
  `~/.persona/node-attach.sock` 这类路径没问题；测试里别把 socket 放在很深的临时目录下（Claude Code 的 scratchpad 就超了）。
- **`/tmp` 不是临时目录**：`os.MkdirTemp("/tmp", …)` 在 Windows 上是当前盘符下的 `\tmp`，多半不存在。要短路径的测试按平台分文件（`orcarttest` 的 `listen_*.go`）。

## 进程和 shell

- **`bash` 可能是 WSL**：服务拿到的系统 PATH 里 `C:\Windows\System32\bash.exe` 排在 Git 前面，它会进 WSL 里执行（另一台「Linux」，看到的是那边的 claude 和 `~/.zshrc`）。
  node 在 Windows 上跑 shell 脚本一律用 Git for Windows 的 bash（`loginShellProgram`，和 Claude Code 自己一样，认 `CLAUDE_CODE_GIT_BASH_PATH`），找不到就让命令起不来，不要退回裸的 `bash`。
- **读别的进程的环境变量要读 PEB**：没有 `/proc`、没有 `ps -E`。`procs_windows.go` 用 Toolhelp32 列进程，再对候选进程 `NtQueryInformationProcess` + `ReadProcessMemory`
  读 `RTL_USER_PROCESS_PARAMETERS`（命令行、当前目录、环境块）。PEB 里的指针是对方进程的地址：按字节读出来、按 x/sys 的偏移取字段，
  不要直接读进 `windows.PEB` 这种带 Go 指针字段的结构体（GC 会把对方的地址当成自己的指针）。
- **环境变量的值里有空格**：`envVarRe` 这类按空白切的正则是给 `ps -E` 用的，`C:\Users\A B\.claude` 会被截断。Windows 上直接按名字取整个值（`pickEnv`），名字不分大小写。
- **没有「前台进程组」和「挂起」**：Orca 宿主在 Windows 上把「父进程链上没有同一窗格的 agent 进程」的 claude / codex 当作在前台，见 [windows.md](../windows.md)「Orca」。
- **pid 会很快复用、也不递增**：按 pid 大小排「包装在前」在 Windows 上不成立，按启动时间排；顺着父进程链找祖先时要求祖先比自己先启动。

## 计划任务（开机自启）

`jarvis-link install` 在 Windows 上注册计划任务（[windows.md](../windows.md)「安装和开机自启」）。都是 2026-11-04 在 Windows 11 真机上试出来的：

- **标准用户注册不了「不管是否登录都运行」的任务**：`LogonType` 用 S4U（不存密码）时 `Register-ScheduledTask` 报拒绝访问（没提权的管理员账号也一样）。
  只能 `InteractiveToken`，跟着登录会话跑。
- **「失败后重启」不管程序退出**：`RestartOnFailure` 只在任务起不来时重启；程序以退出码 1 结束后 4 分钟都没再起。要看护就加一个每几分钟一次的触发，
  多实例策略 `IgnoreNew`（在跑就什么都不做）。
- **登录触发器上的「重复」要到下次登录才开始**：刚装好、没重新登录前它不会每 5 分钟起一次。看护用单独的时间触发（`StartBoundary` 写安装那一刻），重启后照样按时触发。
- **控制台程序会弹窗口**：用户会话里的任务起控制台程序，会给它开一个控制台窗口（默认终端是 Windows Terminal 时是 Windows Terminal 的窗口）。
  任务改起 GUI 子系统的程序（把 PE 头的 Subsystem 从 3 改成 2 的一份副本），由它用 `CREATE_NO_WINDOW` 起控制台程序：有控制台、没窗口。
- **默认值要改**：`ExecutionTimeLimit` 默认 72 小时，到点结束任务（写 `PT0S`）；`Priority` 默认 7 是「低于正常」（写 5）；默认用电池时不启动、拔电源就停。
- **schtasks 的输出是系统代码页**（中文系统是 GBK），`/Query` 的状态列是本地化的字（Running / 正在运行）：只拿来显示，不解析。
  XML 文件写成 UTF-16LE 带 BOM，用户名、路径里有中文也不会乱。
- **结束任务只杀守护进程本身**：它起的子进程会留下来。守护进程把自己放进 `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` 的 Job（之后起的进程自动在里面），
  它一没（`/End`、崩溃、被杀）子进程跟着没。
- 跑着的 `.exe` 不能覆盖也不能删：重装前先 `/End` 任务、等 jarvis-link 放开运行锁；卸载时守护进程的文件可能要等一下才删得掉。

## Orca

- **Windows 版 Orca 的 runtime 走命名管道**：`orca-runtime.json` 的 `transports` 是 `named-pipe`（`\\.\pipe\orca-<pid>-…`）和 `websocket`，没有 `unix`。
  `orcart` 按平台挑传输：Windows 先 `named-pipe`（`tailscale/go-winio` 拨号）再 `unix`，其他平台只 `unix`。协议（一行一个 JSON）不变。
- Orca 终端里的环境变量和 macOS 一样有 `ORCA_PANE_KEY` / `ORCA_AGENT_PANE`；还有 `ORCA_USER_DATA_PATH`（`%APPDATA%\orca`）。
- `terminal.list` 的 `topologyRevisions` 键里路径分隔符混用（同一台机器上有 `C:/Users/…` 也有 `C:\Users\…`），别拿它和 `worktreePath` 直接比字符串。
