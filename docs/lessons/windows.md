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

## Orca

- **Windows 版 Orca 的 runtime 走命名管道**：`orca-runtime.json` 的 `transports` 是 `named-pipe`（`\\.\pipe\orca-<pid>-…`）和 `websocket`，没有 `unix`。
  `orcart` 按平台挑传输：Windows 先 `named-pipe`（`tailscale/go-winio` 拨号）再 `unix`，其他平台只 `unix`。协议（一行一个 JSON）不变。
- Orca 终端里的环境变量和 macOS 一样有 `ORCA_PANE_KEY` / `ORCA_AGENT_PANE`；还有 `ORCA_USER_DATA_PATH`（`%APPDATA%\orca`）。
- `terminal.list` 的 `topologyRevisions` 键里路径分隔符混用（同一台机器上有 `C:/Users/…` 也有 `C:\Users\…`），别拿它和 `worktreePath` 直接比字符串。
