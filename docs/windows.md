# node on Windows (herdr and Orca engines)

Lets the phone see and drive Claude Code inside herdr or Orca on a Windows computer: that computer runs the resident node (`personad node --attach`) and the connector jarvis-link (active mode, see [link.md](link.md)),
while the hub lives elsewhere (WSL2 / Docker / another machine). Requirements and steps are in #1090; the overview of Windows support is #1179. Pitfalls we hit are in [lessons/windows.md](lessons/windows.md).

## Status

Both engines work: **herdr** (native Windows build since 0.9.5, see "herdr" below) can create and resume sessions from the phone, same as macOS / Linux; **Orca** can only view and drive sessions that are already open.
The table below is for Orca; where the herdr engine differs is in the table in the "herdr" section.

| Capability | Windows (Orca) | Notes |
| --- | --- | --- |
| Build | ✅ | `GOOS=windows go build ./...`; process groups, signals, file locks and CPU time are in `internal/osproc` and each package's `_windows.go` |
| Resident node, jarvis-link active mode | ✅ | `jarvis-link install` installs a scheduled task that starts at logon, restarts on exit and shows no window ("Install and start at logon" below); there is no one-line install script or installer package (#1179) |
| List Orca sessions and state | ✅ | One snapshot per second, as on macOS |
| Read transcripts and session info | ✅ | `%USERPROFILE%\.claude\projects\<encoded directory>\<id>.jsonl`; the directory encoding is the same as Claude Code's (every non-alphanumeric becomes `-`, `C:\Users\…` → `C--Users-…`) |
| Send messages (multi-line as one), keys, interrupt | ✅ | Bracketed paste works under ConPTY |
| Answer dialogs (AskUserQuestion, permissions) | ✅ | Answered by reading the screen, same as Orca on macOS |
| Close a session | ✅ | |
| Create a session from the phone, resume a session that isn't running | ❌ | The Orca engine doesn't support this on any platform yet ([plan-orca-support.md](plan-orca-support.md), "Phase 2"); not a Windows problem |
| Machine monitoring (CPU, memory, swap, network, disk, system info) | ✅ | Win32 calls, no processes spawned; the metrics follow the Windows column of [machines.md](machines.md) ("Collection metrics") (#1943) |
| Port forwarding, Codex / compatible Codex forks discovery | ❌ | Depends on `ps` / `lsof` / `/proc`; silently unavailable on Windows (#1179) |
| Processes the node starts itself: managed launchers, `codex` start | ✅ | `osproc.Exec`: Windows has no exec, so it starts a child in the same console and exits with its exit code ("herdr" below) |
| hub self-update | ❌ | Still `syscall.Exec`; the hub doesn't run natively on Windows (#1179) |
| Updating the resident node through the hub ([node-update.md](node-update.md)) | ❌ | The node reports it as unsupported ("resident node is not managed by launchd/systemd"), and the App shows why; to upgrade, run `jarvis-link install --jarvisd <new personad.exe>` again |
| Passive mode (hub connects to this machine) | Not verified | Builds, and `install --listen` takes the same path; you have to open the port in Windows Firewall yourself (`install` prints the `New-NetFirewallRule` command) |

## Install and start at logon

```powershell
cd server
go build -o personad.exe .\cmd\personad
go build -o jarvis-link.exe .\cmd\jarvis-link
.\jarvis-link.exe install --jarvisd .\personad.exe --pair '<the jarvis://link?… link from the hub>'   # skip --pair if already paired
```

No administrator needed. On Windows, `install` does, in order:

1. With `--pair`, pair first; `--listen` switches to passive mode (same as Linux, see [link.md](link.md), "Linux (headless)").
2. If the scheduled task is running, end it first (a running program file can't be replaced) and wait for jarvis-link to release its run lock. If another jarvis-link is running (say, a foreground `jarvis-link run`), it asks you to stop it.
3. Copy itself to `~/.persona/bin/jarvis-link.exe`; copy the personad given by `--jarvisd` to `~/.persona/bin/jarvisd.exe`. Unlike Linux, if one is given it is always replaced:
   Windows can't update the node through the hub yet, so running `install` again is the upgrade. If none is given and there is no `jarvisd.exe`, only jarvis-link is installed (same as `--no-node`).
4. Put another copy at `~/.persona/bin/jarvis-link-service.exe`: the same program, with only the subsystem in the PE header changed from console to GUI (`guicopy.go`).
5. Write `~/.persona/jarvis-link-task.xml` (UTF-16), register the scheduled task "Persona Link" with `schtasks /Create /XML`, start it with `schtasks /Run`, and wait for it to connect to the hub (same as Linux, up to 20 seconds).

The "Persona Link" scheduled task:

- **Current user, runs with the logon session** (`InteractiveToken`). A standard user can't register a "run whether logged on or not" task (S4U gets access denied on a real machine; the stored-password kind needs an administrator),
  and the node has to be in the user session to see Orca and its agents anyway. The cost: it isn't there when nobody is logged in to Windows, stops at logoff, and starts again at the next logon.
- **Triggers**: at logon, plus every 5 minutes starting from install. The multiple-instances policy is "ignore new": while the supervisor is running it does nothing; if it's gone it is brought back within 5 minutes.
  The task's own "restart on failure" isn't used: it only covers the task failing to start, not the program exiting non-zero (tested on a real machine). "Repeat" on a logon trigger only starts at the next logon, so a separate time trigger is added.
- No run-time limit (by default the task is ended after 72 hours), runs on battery, isn't stopped when idle ends, normal priority (the default 7 is "below normal", lowering both CPU and disk).
- **What runs is the supervisor** `jarvis-link-service.exe supervise` (`supervise_windows.go`): it starts jarvis-link (`run --log ~/.persona/link.log`) with `CREATE_NO_WINDOW`
  and the resident node (`jarvisd.exe node --attach`, log `~/.persona/node-attach.log`, moved to `.1` at 10 MB), and restarts whichever exits after 5 and 10 seconds respectively (same as `RestartSec` on the two Linux units).
  What the supervisor itself does goes to `~/.persona/link-service.log` (jarvis-link's output when it crashes is there too). Only one supervisor runs per computer (file lock on `~/.persona/link-service.run`).
  It starts the node first and waits for it to recreate `node-attach.sock` (it only checks the file's modification time and doesn't connect: each connection is an attach and would kick off the hub's connection; waits up to 10 seconds) before starting jarvis-link.
  The other way round, the hub connects the link first and immediately opens a stream to a node that isn't listening yet, briefly reporting this machine offline, and `install` concludes "the hub can't reach this machine's node" (#1984).
- The supervisor puts itself in a Job that "kills every process inside when the last handle closes": when the supervisor goes away (task ended, another install / uninstall, logoff, crash),
  jarvis-link and the node go with it, so when the scheduled task brings it back it doesn't fight leftover old processes.

**Why a GUI-subsystem copy**: when a scheduled task starts a console program in the user session, Windows opens a console window for it (a Windows Terminal window if Windows Terminal is the default terminal),
which stays on the desktop. A GUI-subsystem program has no window, and the console programs it starts with `CREATE_NO_WINDOW` get a console but no window. It's a copy rather than a separate build: whether `jarvis-link.exe`
is a release or your own `go build`, the supervisor is the same program. Approaches not used: `conhost --headless` (undocumented), a VBScript wrapper (VBScript is being removed from Windows),
`powershell -WindowStyle Hidden` (still flashes a window).

| Command | On Windows |
| --- | --- |
| `jarvis-link status` | An extra line "Background service: scheduled task "Persona Link" · state · starts at logon"; the state is whatever schtasks prints in the system language (Running / 正在运行, the Chinese-locale "Running"…), shown but not parsed |
| `jarvis-link uninstall [--purge]` | Ends and deletes the scheduled task (the node stops with it), deletes `jarvis-link-task.xml` and `jarvis-link-service.exe`; `--purge` is as on Linux and also deletes the supervisor's logs and lock |
| `jarvis-link node install` / `node uninstall` | Not installed separately: the node is in the same scheduled task; use `install` (with node) / `install --no-node` |
| `jarvis-link node status [--json]` | Whether the scheduled task includes the node, and the task's state |
## hub in Docker Desktop on the same computer

During acceptance, the hub and node were on the same Windows machine: the hub ran in Docker Desktop (WSL 2), node and jarvis-link on the Windows side.

- With WSL mirrored networking (`networkingMode=mirrored` in `.wslconfig`), ports published by Docker are only reachable on Windows' `127.0.0.1`; LAN and Tailscale addresses can't connect
  (opening them requires changing the Hyper-V firewall, which needs an administrator).
- The local jarvis-link connects to `127.0.0.1`: if the hub's advertised candidate addresses include no loopback address, add it manually with `jarvis-link address add https://127.0.0.1:<hub TLS port>`.
- The phone connects over Tailscale: this machine forwards the tailnet port to localhost with `tailscale serve --bg --tcp <port> tcp://127.0.0.1:<port>` (plain TCP forwarding; TLS is still done by the hub itself,
  and the pinned certificate is still the hub's), and `public.advertise` in hub.json lists `https://<this machine's Tailscale address>:<port>`.
- To pair the phone with a new hub, use **Settings › hub › Add Hub** in the App, not the root pairing link: that would replace the phone's existing pairing ([lessons/android.md](lessons/android.md)).

## Running it manually (for development and acceptance)

On a machine with the scheduled task installed, first run `schtasks /End /TN "Persona Link"` (or `jarvis-link uninstall`); otherwise the foreground and scheduled-task copies, two jarvis-links and two nodes, will fight.

```powershell
cd server
go build -o personad.exe .\cmd\personad
go build -o jarvis-link.exe .\cmd\jarvis-link
.\personad.exe node --attach            # resident node; keep it open in another window
.\jarvis-link.exe pair '<the jarvis://link?… link from the hub>'
.\jarvis-link.exe run                    # keeps the link up
```

On the hub side: this machine is `kind: link` in `machines` in hub.json, with `public.tls_listen` on; get the pairing link with `personad hub pair --machine <id>` (for a Docker deployment,
`docker compose exec hub jarvisd hub pair --machine <id>`). Once connected, set this machine's engine to Orca in the App (**Settings › Machines › Engine**).

## herdr

herdr has a native Windows build since 0.9.5 (ConPTY panes, [official notes](https://herdr.dev/docs/windows-beta/)). The node drives it only through herdr's socket API, so the herdr engine on Windows behaves as everywhere else:

| Capability | Windows (herdr) | Notes |
| --- | --- | --- |
| List sessions and state, read transcripts | ✅ | Same as macOS / Linux |
| Create a session from the phone, resume a session that isn't running | ✅ | The node types the start command in a new tab, as elsewhere |
| Send messages (multi-line as one), keys, interrupt | ✅ | |
| Answer question / permission cards | ✅ | Via Claude Code hooks, no screen reading |
| Live terminal (`terminal session observe`) | ❌ | herdr has no direct terminal attach on Windows; falls back to reading the screen |

- **socket**: herdr's API on Windows is a named pipe, named `\\.\pipe\` plus the socket path (`\\.\pipe\C:\Users\<you>\AppData\Roaming\herdr\herdr.sock`);
  the config directory is `%APPDATA%\herdr` (or `herdr` under `$XDG_CONFIG_HOME` if that's set). `socket_windows.go` in `internal/herdr` dials by this rule, matching herdr's own
  `config_dir` / interprocess `GenericNamespaced`; `$HERDR_SOCKET_PATH` still takes precedence. The path itself isn't a file, so `SocketUp` checks whether the pipe exists.
- **Shell in the pane**: herdr opens PowerShell by default on Windows, but the start commands the node types are POSIX shell syntax (`'…'` quoting, `A=1 cmd`). Set the pane shell to Git Bash in herdr's
  `%APPDATA%\herdr\config.toml`:

  ```toml
  [terminal]
  default_shell = "C:/Program Files/Git/bin/bash.exe"
  ```

- **herdr has to stay resident in the user's desktop session**: a `herdr server` started from an ssh session exits with that session. Run `herdr.exe server` from a scheduled task that starts at logon (current user,
  `Interactive`, no time limit); it's the same kind as the `jarvis-link install` task.
- **`syscall.Exec`**: `jarvisd launch` (managed launchers) and `codex-start` / `tab-start` replace themselves with claude / codex. Windows has no exec, so `osproc.Exec` starts a child in the same
  console (stdin/stdout inherited as-is, so the pane shows that program), catches Ctrl+C itself without exiting (the child in the same console still receives it; only `signal.Notify` works, `Ignore` doesn't block it), and exits with the child's exit code when it ends.
- **Claude Code's first run**: a freshly installed Claude Code first shows the theme-picking onboarding, so the node never sees the input box ("claude did not become ready"). Run
  `claude` manually once on that computer to finish onboarding, or set `"hasCompletedOnboarding": true` in `%USERPROFILE%\.claude.json`.
## Local sockets

The node's `node.sock`, `runtime.sock` and `node-attach.sock` are still AF_UNIX socket files on Windows (available since Windows 10 1803), under `%USERPROFILE%\.persona\`.
They weren't switched to named pipes: Go's `net` supports them directly, so the hooks, jarvis-link and `--stdio` forwarding code didn't change a line. Listening, connecting, half-close (`CloseWrite`) and replacing a leftover socket file were verified on a real Windows 11 machine.
Limits: the path can't exceed 108 bytes (a very deep `%USERPROFILE%` gives `bind: invalid argument`); permission bits like `0600` mean nothing on Windows, so it relies on the user directory's own ACL.

## Orca

- **Transport**: the Windows build of Orca writes `named-pipe` (`\\.\pipe\orca-<pid>-…`) and `websocket` in `orca-runtime.json` (`%APPDATA%\orca\`), with no `unix`.
  `orcart` picks by platform: on Windows `named-pipe` first, then `unix`; elsewhere only `unix`. Named pipes are dialed with the existing dependency `tailscale/go-winio`. The protocol, token and metadata re-read rules are unchanged.
  The fake runtime used in tests (`orcarttest`) also uses named pipes on Windows, so the `orcart` and `hostorca` tests on Windows exercise the real transport.
- **Identifying panes and sessions**: as on macOS, by `ORCA_PANE_KEY` in the claude / codex process environment (falling back to `ORCA_AGENT_PANE`), then reading `sessions/<pid>.json`.
  Windows has no `ps -E` or `/proc`: `procs_windows.go` lists processes with Toolhelp32 and reads the command line, current directory and environment block from the PEB only for candidates (`claude`, `codex`, `node`, `bun`);
  only the few environment variables needed are kept (Orca's hook token and the like are never kept).
- **In the foreground**: Windows has no terminal foreground process group and no "suspended". Instead, a process counts only if "no ancestor in its parent chain is an agent process in the same pane" (a claude started by claude isn't the one the terminal shows);
  pids get reused, so an ancestor must have started before the process itself. When a pane has several, claude comes first, then by start time (Windows pids don't increase, so they can't be sorted by pid).
- **Screen-reading card**: Orca reports the session as blocked as soon as the AskUserQuestion PreToolUse hook fires, but starting a hook process on Windows takes a few seconds, so the dialog appears late.
  The card the manager built at that moment couldn't read the dialog ("can't read this dialog yet"); after that the manager looks at the screen again every 500 ms (up to 30 seconds) and swaps in a card with options as soon as the dialog appears (`assistant.upgradeScreenCard` in `assistant/fleet_ix.go`, on all platforms).

## shell

When the node runs shell scripts on Windows, such as launcher probing and `claude --version`, it uses Git for Windows' bash (`loginShellProgram`: `CLAUDE_CODE_GIT_BASH_PATH`,
otherwise found from `git`'s location and common install directories), the same as Claude Code itself on Windows. It doesn't use a bare `bash`: in the PATH the service gets, `System32\bash.exe` comes first, and that's WSL.
If Git Bash can't be found, these commands don't start (the machine info has no Claude version); they never end up running in WSL.

## Verification record: herdr engine (2026-11-04, real Windows 10 machine)

- herdr 0.9.5 (official `herdr-windows-x86_64.zip`), Claude Code 2.1.311 (native `claude.exe`), Git for Windows 2.56 (PortableGit); herdr stays resident via a scheduled task that starts at logon,
  with the pane shell set to Git Bash. The hub was on a Linux machine, running this machine's `jarvisd.exe node --stdio` over ssh.
- On the hub this machine was online: `windows/amd64`, engine herdr, herdr 0.9.5. Through the hub APIs: creating a session (`POST /api/v1/sessions`, with a first message) got a reply;
  a multi-line message was submitted as one and got a reply; asking Claude to call AskUserQuestion, the card reached the hub via the hook, and after answering with `POST /api/v1/interactions/{id}/resolve` Claude replied according to the answer;
  after `/exit` the session went offline, and on the next message the node started it with `--resume` in a new tab, and Claude remembered the earlier answer.
- `jarvisd tab-start`: with the argv from a file, started `claude -p` (replied normally), `cmd /c exit 3` (exit code 3), and a command prefixed with `A=1` (the environment variable came through); the file is deleted after use.
- Tests: `internal/herdr` (`TestPipeName`, `TestSocketPathWindows`, and the RPC / subscription tests with the test herdr switched to listening on a named pipe) and `internal/osproc` (`TestExecExitCode`, `TestExecSurvivesCtrlC`: fails when switched back to `signal.Ignore`)
  passed on this Windows machine; the few tests that use shell scripts posing as the herdr executable are skipped on Windows. On Linux, `internal/herdr`, `internal/osproc`, `internal/node` and `internal/hub` passed.
- Not verified: the App UI (the same hub APIs the App calls were used); Codex; herdr's live terminal (herdr doesn't support it on Windows).
## Verification record: install and start at logon (2026-11-04, the same real Windows 11 machine)

- Replaced the previously hand-added registry Run entry with `jarvis-link install --jarvisd personad.exe`: the scheduled task registered and started, connected to the hub within seconds, and the local node came online;
  in the hub's `GET /api/v1/machines` this machine was online with engine Orca, and `GET /api/v1/sessions` had Orca's 10 panes.
- Processes: svchost (scheduled task) → `jarvis-link-service.exe supervise` → `jarvis-link.exe run`, `jarvisd.exe node --attach`; each of the two children has a hidden conhost, and there is no window on the desktop.
- Killed jarvis-link: back after 5 seconds. Killed the node: back after 11 seconds. Killed the supervisor: within 2 seconds jarvis-link and the node were both gone; the next 5-minute time trigger brought them all back and the machine came back online on the hub.
- A probe task had confirmed earlier: a non-administrator can register an `InteractiveToken` task but not an S4U one; the GUI-subsystem copy and the children it starts with `CREATE_NO_WINDOW` have no console window
  and all run in the user session; `RestartOnFailure` does not restart a program that exits with code 1.
- Tests: `go test ./cmd/persona-link/` passes on Windows and on Linux (`golang:1.27` container); `supervise_windows_test.go` fails when the Job is removed (the child outlives the supervisor).
- Not verified: the logon trigger on an actual logoff and logon (that would close the Orca and Claude sessions in use on this machine; the logon trigger was accepted at registration); passive mode; the phone UI (the phone was locked; the same hub APIs the App calls were used; the App UI was checked during the second step below).

## Verification record (2026-11-03, real Windows 11 machine)

- Orca 1.4.242 (runtime protocol 3), Claude Code 2.1.318 (native `claude.exe`); the hub was this repository's Docker image (Docker Desktop), and the phone reached the hub's TLS port via `adb reverse`.
- `go test -tags orcalive -run OrcaLive ./internal/node/` (read-only): all 10 Claude panes in Orca were recognized, session ids matched `sessions/<pid>.json` one to one, one refresh took about 50 ms.
- Through the hub: list sessions and state, read transcripts, send a multi-line message (submitted as one), answer AskUserQuestion through a screen-reading card option, close a session (only the target terminal closed).
- Real-device App (Android): joined the test hub via **Settings › hub › Add Hub** (existing pairing untouched); Agents showed this Windows machine and the sessions above. From the phone: sent messages, answered
  AskUserQuestion twice by tapping options on the screen-reading card (the transcript shows the option the phone picked), closed the Agent from the session menu (the confirmation named the Orca tab, and only that one terminal closed). The first card couldn't read the dialog, which led to the fix in the "screen-reading card" item above.
- Linux (`golang:1.27` container): `orcart`, `osproc`, `plugin`, `transcript`, the whole `node` package, and the screen-reading card / Orca / engine / link / machine tests in `hub`: passed.
- On Windows, all tests in `orcart`, `osproc`, `transcript` and the Orca / attach / process-scanning tests in `node`: passed. A batch of tests in the `node`, `link` and `jarvis-link` packages hard-code
  Unix (shell scripts posing as executables, `/tmp`, permission bits) and fail on Windows; unrelated to this change, left for #1179.
