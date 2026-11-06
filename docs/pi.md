# pi 和 pi-web

[pi](https://pi.dev)（`@earendil-works/pi-coding-agent`）和它的网页版 pi-web（`@agegr/pi-web`）的会话出现在「Agents」里：
列表和状态、对话内容（文字、思考、工具调用和结果、图片）、逐字输出、从手机发消息、中断、改名、在 pi-web 里新建会话，
以及在手机上回答 pi 的提问（`ask_user`）和批准你指定的工具。

2026-11-01 起支持（issue #170）。pi 是第二个按 runtime 规范接入的（`docs/runtimes.md`）：`integrations/pi/` 是一个 pi 扩展加一个小伴随进程，
都建在 `integrations/runtime-sdk/` 上。谁管理 pi，pi 就走谁的路：

| pi 跑在哪 | 在 Persona 里是 | 谁负责什么 |
| --- | --- | --- |
| **herdr 的窗格里**（pi 的 TUI） | herdr 的会话，和 herdr 里的 Claude 一样 | 窗格、空间、标签、终端画面、按键：herdr；对话内容、发消息、中断、卡片、逐字：同一个 pi 进程里的扩展（`server/internal/node/pi.go`） |
| **pi-web 里** | runtime `pi-web` 的会话 | 扩展（pi-web 的服务进程里）+ 伴随进程（磁盘上的、新建、唤醒） |
| 别的终端里（不是 herdr） | runtime `pi-<pid>` 的会话 | 扩展；没有终端画面 |

## 结构

```
 App ── hub ── node ── ~/.persona/runtime.sock ◄─┬─ jarvis 扩展（每个 pi 进程一条连接）
                                                 │    · pi 终端 / pi --mode rpc：runtime `pi-<pid>`，只管这个进程里正在跑的会话
                                                 │    · pi-web 的服务进程：runtime `pi-web`，管 pi-web 里正在跑的会话
                                                 └─ pi-web 伴随进程 companion.mjs：runtime `pi-files`
                                                      · 列出磁盘上最近的 pi 会话（7 天内、最多 80 个），从文件翻页
                                                      · 新建会话、给不在运行的会话发消息：都交给 pi-web 的 HTTP 接口
```

- **为什么一个进程一条连接**：pi-web 在一个进程里跑很多会话（内嵌 SDK，不开子进程）；pi 只加载一次扩展模块、每个会话调一次工厂函数（实测）。
  扩展在模块级（`globalThis`）留一个宿主，每个会话把自己的扩展 API 登记进去。
- **为什么要伴随进程**：pi-web 只在有会话打开时才加载扩展。重启后、没人在网页里打开会话之前，只靠扩展的话 Persona 一个 pi 会话都看不到，
  也没法从手机新建。伴随进程用的是同一份宿主代码（`createPiHost({listDisk: true})`）和 pi 自己的 `SessionManager`。
- **herdr 里的 pi 怎么变成一行**：herdr 报的 Agent 是 `pi`；它的 pi 集成（`herdr integration install pi`）还报会话文件（`agent_session {kind: "path"}`，
  文件名 `<时间>_<会话 id>.jsonl`）。没装 herdr 的集成时，扩展在列表里报它所在的窗格（`pane_id` = `HERDR_PANE_ID`），node 照样对上。
  这一行是 herdr 的（`source_kind` 为空、有终端），标题、最后一句、模型、修订号取扩展列的；扩展列的那一行不再单独出现。调用按类型分：
  `session.keys` / `screen` / `terminal.watch` / 关闭 / 标签名给 herdr，其余（内容、发送、中断、改标题、卡片）给扩展。
- **没装扩展的 pi 窗格**：照样列出，只有终端；对话是一条说明，发消息被拒（`pi_no_extension`：不往 pi 的输入框里盲打字），中断按 Esc。
- **同一个会话两边都列**：伴随进程把磁盘上的会话（包括它刚让 pi-web 新建的）列成 `offline`，正在跑它的那个进程（pi-web 里的扩展，或某个 pi 终端）列成运行中。
  node 选正在跑的那个（`runtimeRegistry.recomputeOwners`）：pi 的会话文件没有锁，发消息绝不能落到第二个写它的进程上。
  pi-web 把空闲的会话回收后，伴随进程接手；再发消息时由它让 pi-web 重新打开，扩展又接回去。

## 谁管理会话，离线后去哪接着

扩展在会话开始时用 `pi.appendEntry("jarvis-host", {host})` 记一笔它在哪跑（`herdr`、`terminal`、`pi-web`；只在变了的时候记，不进模型上下文；
`pi --mode rpc`、`pi -p` 这类别的程序起的不记）。会话停了以后，伴随进程从磁盘列出它，按最后一笔决定：

- 在 herdr 或终端里跑过的：行里带 `resume_argv: ["pi", "--session", <文件>]`。手机发消息时 node 在 herdr 里这个目录的空间开一个新标签，
  经 `jarvisd tab-start <0600 文件>` 运行它（命令和路径不经 shell、不上屏），等新起的 pi 的扩展列出这个会话，再把消息发过去。
  herdr 不在时拒绝（`engine_missing`），**不会**改到 pi-web 里打开；node 太旧不认 `resume_argv` 时伴随进程也拒绝，不唤醒 pi-web。
- 在 pi-web 里跑过的、没有记录的（装扩展之前的会话）：交给 pi-web 唤醒，和以前一样。

## 安装

在跑 pi / pi-web 的那台电脑上（这台电脑要在 hub 的机器列表里，node 要能起来）。伴随进程要 node 支持 `prefer_running`（这个 PR 之后的 personad），
旧 node 上它不连，等 node 重启（升级）后自己连上；扩展照常工作（只是没有磁盘上的会话、不能从手机新建）：

1. 装 pi 和 pi-web：`npm i -g @earendil-works/pi-coding-agent @agegr/pi-web`，配好模型（`~/.pi/agent/models.json`，见 pi 的文档）。
2. 装成能自动更新的样子（下面「自动更新」）：`node <Persona 仓库>/integrations/pi/install.mjs`，把这个包装进
   `~/.persona/runtime-packages/jarvis-pi-runtime/current/`，它会打印下面两步的确切路径。
   再 `pi install ~/.persona/runtime-packages/jarvis-pi-runtime/current/pi`（它按相对路径用旁边的 `runtime-sdk`），
   pi 终端、`pi --mode rpc`、pi-web 都会加载它。装完重启 pi-web。
   也可以直接 `pi install <Persona 仓库>/integrations/pi`（本地目录是链接，不拷贝），但那样不会自动更新，要自己更新检出目录、重启 pi-web。
3. 常驻伴随进程：`node ~/.persona/runtime-packages/jarvis-pi-runtime/current/pi/companion.mjs [--port <pi-web 端口>]`，用 launchd（`KeepAlive`）/
   systemd（`Restart=always`）保活：自动更新时它装好新版本就退出，等保活把它用新代码拉起来。
   磁盘上的会话（停了的 herdr 会话、pi-web 回收的会话）都靠它列出；只在 herdr 里用 pi 也装上它。
4. 在 herdr 里用 pi：`herdr integration install pi`（herdr 自己的 pi 集成，报状态和会话文件；不装也行，扩展会报窗格）。

### 配置（可选）

`~/.pi/agent/jarvis.json`（`PI_CODING_AGENT_DIR` 下）：

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `approve` | `[]` | 要在手机上（或 pi 自己的对话框里）批准才执行的工具，比如 `["bash", "write"]`。pi 本身没有权限确认，默认什么都不拦。手机上的卡有「总是同意」（「本会话不再问 Bash」）：点了之后这个会话里这个工具不再问，记在会话文件里（自定义条目 `jarvis-allow`，不进模型上下文），换一个 pi 进程接着这个会话也算；只允许一次、拒绝都不记 |
| `askUser` | `true` | 给模型一个 `ask_user` 工具：问用户一个问题（可带选项），手机或 pi 的对话框作答 |
| `answerFirst` | `auto` | 授权和提问先给谁：`auto` = 手机最近 6 小时驱动过这个会话就先给手机，否则先给本地（pi 终端 / pi-web 网页）；`phone` / `local` 固定一边 |
| `socket` | `~/.persona/runtime.sock` | node 的 runtime socket；环境变量 `JARVIS_RUNTIME_SOCKET` 也行 |
| `autoUpdate` | `true` | 从存放目录装的扩展接受 node 带来的版本（「自动更新」）；`false` 不换。伴随进程用 `--no-update` |

### 自动更新

personad 构建时把这个包（`integrations/pi` 的扩展、伴随进程和 `runtime-sdk`，`internal/runtimepkg` 的 `jarvis-pi-runtime`）打进二进制，
node 带的就是要跑的（通用做法见 `docs/runtimes.md`「自动更新」）。从存放目录装的（pi 加载 `<存放目录>/current/pi`）才会更新：

- **扩展**（pi 终端、`pi --mode rpc`、pi-web 的服务进程）：连上 node 时报自己的包哈希；node 带的不一样，它取包、核对、装成新的 `current`（符号链接改名替换），
  然后等这个进程里**所有会话都在两轮之间、手机上没有它的卡片**，停掉自己的连接，让每个会话对自己执行 `/jarvis-reload`。这个命令是扩展注册的，
  里面调 pi 的 `ctx.reload()`（pi 只在命令里给扩展这个能力），所以用 `sendUserMessage("/jarvis-reload", {expandPromptTemplates: true})` 当命令执行，
  不会发给模型。pi 重新加载扩展时从磁盘读（jiti、不缓存模块），读到的是新的 `current`：同一个进程、同一个会话，换上新代码，新版本的宿主用同一个
  进程标识（`instance`）连上 node，顶替旧的连接。pi-web 正在回复时不让重新加载，所以要等空闲。
  pi-web 里一个会话都没开时没有可重新加载的：下一个会话打开时 pi 用的还是缓存的旧代码，它打开后马上换。
- **伴随进程**：同样取包、装好，然后退出，launchd / systemd 用新代码把它拉起来（不保活的话它就停在那，所以一定要保活）。
- 换不上（不能注册命令的老 pi、重新加载回来的还是旧版本）：留在旧版本，这个版本在本进程里不再试，pi 重启后再试。
- 跑的是哪个版本：node 日志 `runtime pi-web <版本> #N connected`；扩展的日志要 `JARVIS_PI_DEBUG=1`。

第一次（从没有这套机制的版本、或从检出目录装的）要手动切换一次：`install.mjs`、`pi install` 新路径、伴随进程改从新路径跑、重启 pi-web 和伴随进程。
都是命令行，在那台电脑的终端里做（或者让 Agent 做），不用去任何界面里点。

伴随进程的 `--port` / `--host` 跟 pi-web 一致（默认 `127.0.0.1:30141`）；pi-web 设了 `PI_WEB_PASSWORD` 时伴随进程要有同一个环境变量。

## 映射

`integrations/pi/lib/map.js`，pi 会话文件里的条目 → Persona item：

| pi | Persona |
| --- | --- |
| user 消息（文字、图片） | user `text` / `image`（图片是占位，数据走 `session.image`） |
| assistant 消息：`thinking` / `text` / `toolCall` | `thinking` / `text` / `tool_use`；工具名和参数按 Claude 的画法：`bash`→`Bash`、`read`→`Read`（`path`→`file_path`）、`edit`→`Edit`（一处改动）/ `MultiEdit`（多处）、`write`→`Write`…… |
| `stopReason: aborted / error` | notice「已中断」/「出错了：…」 |
| `stopReason: length`（输出达到上限） | notice「输出达到上限，这一轮停了」 |
| `toolResult` | `tool_result`（带 `tool_use_id`、`is_error`），里面的图片另成 `image`（带同一个 `tool_use_id`，App 画在这次调用的卡片上） |
| 用户在 pi 里 `!` 跑的命令（`bashExecution`） | 一对 `Bash` 的 `tool_use` + `tool_result` |
| compaction、分支摘要、显示的自定义消息 | notice |
| 系统提示、模型 / 思考档位变化、标签、会话名、用量 | 不出 item（seq 照样往前走） |

- **seq** 是条目在会话文件里的位置（从 1 开始）：pi 只追加，所以稳定、递增。
- **分支**：pi 的会话是树（「从这里编辑」会分出一支）。一页只放当前分支上的条目；`history` = 会话 id + 分支离开文件顺序的那个条目，
  只追加时不变，换到另一支时变，App 就整页重取，不会把两支拼在一起。
- 发消息：会话在跑时 `pi.sendUserMessage(…, {deliverAs: "steer"})` 插进这一轮，空闲时开新的一轮；中断 `ctx.abort()`；改名 `pi.setSessionName`。
  同一个 `client_msg_id` 只发一次。

## 限制

- 只看得到装了这个扩展的 pi 进程里正在跑的会话，加上（有伴随进程时）磁盘上最近 7 天的。没开 pi-web 时，不在运行的会话能看不能发。
- 不在 herdr 里的 pi 终端没有终端画面，也不能从手机关掉；在 herdr 里的有。
- 还不能从手机在 herdr 里新开一个 pi（新建走 pi-web）；herdr 里停了的 pi 会话能从手机接着。
- pi 自己没有计划审批，没有对应的卡片。
- pi-web 的 HTTP 接口不是公开的稳定接口（伴随进程只用 `POST /api/agent/new` 和 `POST /api/agent/<id>` 两个），升级 pi-web 后跑一遍下面的端到端。

## 验证

- `cd integrations/pi && npm test`：映射、列表、翻页、发送（steer、图片）、中断、改名、推送连续性和分支、伴随进程的磁盘列表和 pi-web 接口、
  授权（手机 / 本地对话框 / 没人答就拦下）、`ask_user`、herdr 里报窗格、记录在哪跑、`resume_argv` 和「herdr 的会话不交给 pi-web」。
- node：`server/internal/node/pi_test.go`（假 herdr）：一行 herdr 会话、调用怎么分、靠扩展的窗格认会话、没装扩展、在 herdr 里接着、没有 herdr 时拒绝。
- 端到端（`DSH_CHECKOUT` 只是为了用它的假模型服务）：
  - 三个都用 SDK 的通用验收（`runtime-sdk/testing/conformance.mjs`：文字回合、批准、带理由的拒绝、提问、逐字、中断、推送连续）。
  - `node integrations/pi/e2e/herdr.mjs`：真的 herdr（独立的具名会话，配置目录在临时目录，装了 herdr 的 pi 集成）里的 pi TUI、真的 `personad node`、伴随进程：
    一行 herdr 会话、扩展在服务、画面来自窗格、通用验收、手机的消息没打进窗格、`/quit` 后从磁盘列出、再发一条在 herdr 同一空间的新标签里接着并回答。2026-11-01：23/23。
  - `node integrations/pi/e2e/run.mjs`：真的 `pi --mode rpc` 加扩展、真的 `personad node`：列出、通用验收。2026-11-01：16/16。
  - `node integrations/pi/e2e/update.mjs`：自动更新。pi-web 和伴随进程从存放目录装（伴随进程由脚本像 launchd 一样保活），node 的包来自一个目录
    （`JARVIS_RUNTIME_PACKAGES`）：同版本时什么都不提供；换成带新版本的 node 后，pi-web 里的扩展原地换上（同一个 pi-web 进程、同一个会话），
    伴随进程装好、退出、以新版本回来，`current` 指向新版本，换完从手机发消息照常回复，`/jarvis-reload` 没有进对话。2026-11-02：9/9。
    单测：`integrations/pi/test/pi.test.js` 末尾五项（等空闲再换、没有命令就不换也不发消息、伴随进程装完退出、换回同一版本时恢复且不再试、哈希不对不装）。
  - `node integrations/pi/e2e/pi-web.mjs`：真的 pi-web（隔离的配置目录，按 `packages` 装上这个包，空闲回收调到 6 秒）+ 伴随进程：
    没有会话时就有启动器、从手机新建、发消息、回收后从磁盘列出和翻页、再发消息唤醒、扩展接回、网页里建的会话也列出。2026-11-01：11/11。
