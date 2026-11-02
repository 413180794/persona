# DSH（DeepSeek Harness）

DSH 的会话出现在「Agents」里：列表和状态、对话内容（文字、思考、工具调用和结果、图片）、逐字输出、发消息、中断、新建会话、
从手机关闭（归档），手机 / 手表上回答 DSH 的工具授权、提问和计划审批；DSH 的 Agent 能用手机工具（`phone_*`）和邮箱工具。
DSH 没在运行时，伴随进程从磁盘列出它的会话，给其中一个发消息就把 DSH 叫起来接着跑。人回到电脑前，在 DSH 的 Web GUI 里接着用的是同一个会话。

2026-10-31 起支持（issue #169）。DSH 是第一个按「runtime」规范接入的（`docs/runtimes.md`）：node 侧是通用的 `server/internal/node/runtimes.go`，
DSH 侧是插件 `integrations/dsh/`，建在通用的 `integrations/runtime-sdk/` 上，插件里只剩 DSH 自己的部分（列会话、翻页、事件映射、两条应答链）。
2026-11-02 补齐了手机工具、关闭、DSH 没在运行时的列表（issue #282）。

## 为什么不走 herdr

DSH 没有 TUI、不发 OSC 7501，会话日志默认 zstd 压缩，它的 Claude Code 兼容 hooks 也答不了提问和计划审批，
所以「herdr 窗格 + transcript + hooks」接不了，也不该做成一个宿主（宿主是终端开在哪，`docs/engines.md`）。它是一个 **runtime**：
自己管会话、没有终端，连 node 的 `~/.jarvis/runtime.sock`，自己报会话、推内容、问手机（规范 `docs/runtimes.md`，协议 `docs/protocol.md`「Runtime ↔ node」）。
DSH 这边是一个装进 `dsh web` 的插件，用的是 Web GUI 自己用的那套服务，DSH 核心一行不改。

```
 App ── hub ── node ──┬── herdr（Claude / Codex / TRAEX）
                      └── ~/.jarvis/runtime.sock ◄── jarvis-dsh-runtime 插件（dsh web 进程里）
                                                    ├─ sessionController：list / page / prompt / cancel / create / rename
                                                    ├─ session/event、agent/assistant-stream、api-session/*
                                                    ├─ approval/request、user-questions/request 两条应答链
                                                    └─ tools.register：hub 的工具（phone_*、mail_*）给 DSH 的 Agent
                                        ◄── jarvis DSH 伴随进程 companion.mjs（runtime `dsh-files`，DSH 没在运行时列磁盘上的会话）
```

## 安装

装在跑 `dsh web` 的那台电脑上，这台电脑要在 hub 的机器列表里（hub 经 ssh 拉起它的 node；不需要装 herdr）。插件和 node 必须是同一个系统用户
（`runtime.sock` 是 0600）。

- **装进 profile**（长期用）：在 DSH 里装 `integrations/dsh` 这个 bundle（Web GUI 的插件管理，或让 DSH 的 Agent 用 `plugin_manager` 的
  `install_bundle`，target 写这个目录的绝对路径）。bundle 自带 `cordis.patch.yml`，装上就插入 `jarvis-dsh-runtime` 一行。
  插件按相对路径用仓库里的 `integrations/runtime-sdk/`，所以从 Jarvis 仓库的检出目录装（本地目录安装是链接，不拷贝），别单独拷走 `integrations/dsh`。
- **不装、先试**：`dsh --profile web --patch <文件>`，文件里插一行，`name` 写 `index.js` 的绝对路径：

  ```yaml
  - insert:
      - id: jarvis-dsh-runtime
        name: /<Jarvis 仓库>/integrations/dsh/index.js
        config:
          interactions: auto
  ```

插件只在 web profile 里有用（要 `sessionController`）；没有 node 在听时它会一直重试（socket 文件在就每秒一次，不在就最长 30 秒一次），不影响 DSH。
常驻 node 的 `runtime.sock` 不跟 hub 连接走：hub 重连时插件不断开，hub 不在时提问直接给 Web GUI。

同一台电脑上接第二个 DSH（另一个 profile 或 `DSH_HOME`）时给它另配 `runtime`、`name`、`launcher`：node 不让两个进程用同一个 runtime id
（后来的那个连不上，日志里说 `runtime_conflict`，`docs/protocol.md`「Runtime ↔ node」），两个都叫 `dsh` 的启动器也只留先连上的那个。

### DSH 没在运行时（伴随进程）

插件跟着 `dsh web` 进程走：DSH 没在跑，它的会话就不在 Jarvis 里。要一直看得到，在同一台电脑上常驻伴随进程（launchd / systemd 保活，和 pi 的一样）：

```
node <Jarvis 仓库>/integrations/dsh/companion.mjs --dsh <DSH 的安装目录或编译过的检出目录> [--home <DSH_HOME>] [--wake '<启动 DSH 的命令>']
```

- 用 DSH 自己的会话存储（`@deepseek-ai/dsh-session-persistence-jsonl`，从 `--dsh` 那个 DSH 里加载）**只读**地列出 `$DSH_HOME/sessions` 里最近的会话
  （和插件同样的 7 天 / 80 个、去掉子 Agent 和已归档的），标题和最后一句取 DSH 的投影缓存，一律列成 `offline`；翻页读会话日志，映射和插件同一份（`map.js`），
  `history` 也一样，所以接手前后手机上是同一段对话。它不写 `DSH_HOME` 里的任何东西（`storages/*.json` 没有跨进程锁，第二个写的进程会弄坏它）。
- 插件连着时它列的会话插件也列，node 认正在跑的那个（`prefer_running`），伴随进程的行退到后面。
- **给离线会话发消息**：伴随进程跑一次 `--wake` 的命令（这台电脑怎么起 DSH，比如 `launchctl kickstart gui/<uid>/<服务名>`、`systemctl --user start <服务>`；
  命令应该很快返回，一分钟内不重复跑），然后把消息**交回** node（`handover`）：node 等插件把这个会话列成运行中，或者问到插件有这个会话，就把消息交给插件发出去，
  最多等 45 秒。没配 `--wake` 时也交回：DSH 在跑就照样送到，不在跑就等到点，回一句「没等到它接手」，消息没有发出。
- 伴随进程不新建会话、不中断、不改名（这些等 DSH 跑起来由插件做），图片要等 DSH 跑起来才看得到。需要 node 支持 `prefer_running` 和 `handover`，旧 node 上它不连。

### 配置

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `interactions` | `auto` | 授权和提问先给谁：`phone` 一律先给手机；`browser` 从不给手机（Jarvis 只看、只发）；`auto` 见下 |
| `socket` | `~/.jarvis/runtime.sock` | node 的 runtime socket；环境变量 `JARVIS_RUNTIME_SOCKET` 也行 |
| `listDays` | `7` | 列出最近几天动过的会话（正在跑、在等人的总会列出） |
| `maxSessions` | `80` | 最多列多少个；手机发过消息、新建过的会话（最近 50 个）不受这两条限制，一直列着 |
| `runtime` / `name` / `launcher` | `dsh` / `DSH` / `dsh` | 这个 DSH 在 Jarvis 里的 runtime id、叫法、启动器名。同一台电脑的第二个 DSH 要另配；改了 id 的会话仍按 DSH 的 Agent 显示（`agent: "dsh"`） |
| `phoneTools` | `true` | 把 hub 的工具（`phone_*`、`mail_*`）给 DSH 的 Agent；`false` 不给 |

## 映射

| Jarvis | DSH |
| --- | --- |
| 会话列表 | `sessionController.list`；去掉子 Agent 的会话、已归档的（`workspaceRegistry.archivedSessionIds`）和还没开始的空会话（手机上新建的除外）。标题用 `title` 投影，没有就用目录名；空间 = 目录 |
| 状态 | 有授权 / 提问等着（插件挂着的，或 `userQuestions` 投影里还开着的）→ `blocked`；在跑 → `working`；其余 `done`（hub 按看没看过分「完成未看 / 已看」） |
| 对话内容 | `sessionController.page`（不会激活冷会话）。`user/message` 只取 `source.kind = user` 的（DSH 注入的时间、技能、指令上下文不显示）；`assistant/message` 的 `reasoning` → 思考、`text` → 回复、`tool-call` → 工具调用；`tool/result` → 结果（compaction 重新挂出来的不重复显示）；中断 / 出错 / 输出上限的轮次、上下文压缩 → 提示。seq = DSH 的 seq + 1，`history` = `dsh:<会话 id>` |
| 工具名 | `bash` → `Bash`、`read` → `Read`、`edit` → `Edit`、`write` → `Write`、`grep`、`glob`、`web_fetch`、`web_search`、`todo_write`、`ask_user_question` 同理；参数名本来就和 Claude 一样（`command`、`file_path`、`old_string`…），App 照常画命令、diff、文件。其他工具原名显示 |
| 逐字 | `agent/assistant-stream` 的 `text-delta`，第一段立刻发，之后最多 120 ms 一次（整段文字） |
| 发消息 | `sessionController.prompt`：`requestId` = App 的 `client_msg_id`（DSH 按它去重，重发不会进两遍）；空闲时 `queue`（开新一轮），工作中 `steer`（插进这一轮，像 Claude 的排队输入）。图片直接带 base64 |
| 中断 | `sessionController.cancel` |
| 新建 | App 新建会话时选启动器 `dsh`（DSH 连着时才有）→ `sessionController.create {cwd}` |
| 改名 | 会话名 → `sessionController.rename`；标签页名忽略 |
| 关闭 | `workspaceRegistry.archiveSession(id, {stopActivity: true})`：和 Web GUI 里归档一样，正在跑的这一轮、子 Agent、后台任务、定时任务一起停；之后不再列出，在 DSH 里可以取消归档 |
| 手机工具 | hub 的工具（node 转来的 `mcp.tools`）注册成 DSH 的工具 `mcp__jarvis__<名字>`，调用经 node 的 `mcp.call` 到 hub，带上调用它的会话；结果的文字是工具结果，出错的结果是工具失败 |

## 授权、提问、计划审批

插件在 DSH 的两条应答链 `approval/request`、`user-questions/request` 上排在最前（`prepend`）。轮到它时：

- **给不给手机**（`interactions: auto`）：最近 6 小时手机给这个会话发过消息或在手机上新建的会话 → 先给手机；没有任何浏览器连着 DSH
  （`typertGateway.hasLiveClient()` 为假）→ 先给手机；否则交给下一个应答方（Web GUI），手机上看不到这张卡。
- **授权**：卡片带工具名、参数（从这一步的 `tool-call` 里取）、要跑的命令（`summary`），DSH 给的原因放 `reason`；node 给卡片标上
  `agent: "dsh"`、`agent_label: "DSH"`，App 按「别的 Agent 的卡」画（「DSH 需要你授权」、同意 / 拒绝，和 Codex 的卡一样），
  不出 Claude 的「本会话总是允许」——DSH 只有「允许一次」，做不到。批准 → `allowed-once`，拒绝 → `rejected`；拒绝时写了话，
  先拒绝，再把这句话 steer 进正在跑的这一轮（DSH 的授权没有理由字段；Codex 也是这样做的），Agent 能读到为什么。
- **提问**：`ask_user_question` 的问题、选项、多选原样上手机；答案按选项文字对回 DSH 的 `{id, selected, custom}`，不在选项里的字当 `custom`。
- **计划审批**：`intent.kind = plan-review` 的问题画成计划卡片，计划正文在 `detail`；批准 = 选它的 `approve` 选项，打回 = 选另一个选项，修改意见放 `custom`。
- **交回电脑 / 过期 / 没有 hub**：调 `next()`，Web GUI 照常弹出。手机拿着卡的时候 Web GUI 看不到它（应答链是一条链，不是广播），
  和 Claude hooks 拦下对话框时电脑上 TUI 不显示是同一个取舍。
- **定时提问**：`ask_user_question` 默认只等 45 秒。到点 DSH 先把「还没回答」交给模型，手机上的卡不撤，之后答了走 `userQuestions.answer`（迟到的回答）。
- **node 重启**：交互的 id 由会话 + 工具调用算出，插件连上下一个 node 后用同一个 id 再问（最多等 2 分钟），手机上还是那张卡。

## 手机工具

hub 连着时（hello 之后、`runtime.hub` 说 hub 回来时，之后每 5 分钟看一次）插件向 node 要 hub 的工具，变了才重新注册，所以 DSH 的 Agent 看到的工具列表
不会无故变动；hub 不在时留着上一份，调用会说 hub 没连上。工具在 DSH 里全局注册，DSH 的所有 Agent（包括子 Agent）都能用；DSH 的工具调用不走 DSH 自己的授权，
会替用户出面的（建日程、发邮件）由 hub 在手机上确认，和 Claude 用这些工具时一样。工具结果里的图片不交给 DSH 的 Agent（结果里说一句）。

## 子 Agent

DSH 委派子 Agent 时把子 Agent 的授权策略钉成 `never`（DSH 的 `captureDelegatedPolicyOverrides`）：子 Agent 要授权的操作直接被拒，`ask_user_question` 以
`DELEGATED_CALLER` 失败，这些请求不经过任何应答链，手机和 Web GUI 都收不到，所以没有需要转给手机的东西，也不会卡住等人。子 Agent 的会话不单独列出：
它被调用的过程和结果在父会话里，完成时有一条「子 Agent 完成」的提示（`map.js`）。

## 限制

- 没有终端（DSH 没有 TUI），App 不显示终端入口。
- 子 Agent 不能单独打开、不能给它发消息（见上一节）。
- 工具结果里的图片不交给 DSH 的 Agent；伴随进程列出的离线会话里的图片，要等 DSH 跑起来才看得到。

## 验证

- `go test ./internal/node -run 'TestBridge|TestPollAgentsPublishesWithoutHerdr'`、`./internal/hub -run TestBridgeLauncher`。
- `cd integrations/dsh && npm test`：映射、分页、发送、watch 的连续性、逐字、授权 / 提问 / 计划 / 迟到的回答、重连；归档和关闭、改名后的 hello / 卡片 id、
  手机工具的注册和调用（`dsh.test.js`）；伴随进程的离线列表、翻页、唤醒和交回、旧 node 上不连（`files.test.js`）。
- node：`go test ./internal/node -run 'TestRuntime'`（含 runtime id 冲突、同一进程重连、启动器只有一个主人、交回的三种结果：`runtimes_identity_test.go`）。
- 端到端：`DSH_CHECKOUT=<编译过的 dsh 源码> JARVISD=<jarvisd> node integrations/dsh/e2e/run.mjs`。在一个临时目录里起真的 `dsh web`
  （独立的 `DSH_HOME`，模型由 DSH 自带的 `llm-mock-server` 扮演）、真的 `jarvisd node --stdio`，脚本自己当 hub：从「手机」新建会话、发消息、
  看思考和回复、翻历史、批准一条命令（真的执行）、回答一个提问、看逐字、中断，核对推送的连续性，再让 DSH 的 Agent 调一个手机工具（hub 收到调用和会话、
  结果回到 Agent）、从手机关闭会话（离开列表、DSH 的归档里有它）。2026-11-02 实测 21/21 通过。
  脚本给 DSH 装了一个只在测试里用的规则（含 `JARVIS_E2E_ASK` 的命令要授权）和一个「不沙箱但要授权」的权限预设：
  在别的沙箱里（比如 Agent 的 shell）起不了 DSH 自己的沙箱。
- 掉出列表的老会话：`node integrations/dsh/e2e/older-session.mjs`（同样两个环境变量）。插件只列 1 个会话，建 A、B 两个，
  重启 DSH 让插件忘掉手机碰过 A，再给 A 发消息，要求送到 DSH、回复推回来、没有走 herdr。2026-11-01：新版 5/5，部署中的旧版 3/5。

- DSH 没在运行时：`node integrations/dsh/e2e/files.mjs`（同样两个环境变量）。真的 `dsh web` 跑一轮后停掉，伴随进程把这个会话从磁盘列成 offline、翻出内容，
  前后 `DSH_HOME` 里没有任何文件变化；从「手机」再发一条，伴随进程用唤醒脚本起 DSH，node 把消息交给插件，回复回来，会话回到插件手里。2026-11-02 实测 7/7 通过。

## 以后

基于 DSH 二次开发、不跑 `dsh web` 的平台，可以在自己那边用同一个 SDK（`integrations/runtime-sdk`）实现 runtime、复用 `integrations/dsh/lib/map.js` 的映射，
Jarvis 这边不用再加代码。
