# opencode

[opencode](https://opencode.ai) 的会话出现在「Agents」里：herdr 窗格里的 opencode 是 herdr 会话（状态、终端、按键、标签都是 herdr 的），
装了 Persona 的 opencode 插件以后还有对话内容（文字、思考、工具调用和结果、图片）、逐字输出、从手机发消息、中断、改名，
以及在手机上批准 opencode 的权限请求。没装插件时照样列出，只有终端。

2026-11-05 起支持（issue #3070，关联 #1178）。opencode 是第三个按 runtime 规范接入的 herdr 里的 Agent（`docs/runtimes.md`，前面是 pi：`docs/pi.md`）：
`integrations/opencode/` 是一个 opencode 插件，建在 `integrations/runtime-sdk/` 上；node 一侧是 `server/internal/node/opencode.go`。

| opencode 跑在哪 | 在 Persona 里是 | 谁负责什么 |
| --- | --- | --- |
| **herdr 的窗格里**（opencode 的 TUI，连着共用的 `opencode serve`） | herdr 的会话，和 herdr 里的 Claude 一样 | 窗格、空间、标签、状态、终端画面、按键：herdr；对话内容、发消息、中断、改名、卡片、逐字：服务进程里的 Persona 插件 |
| 不在 herdr 里（网页、`opencode run`、别的终端连着同一个服务） | runtime `opencode-<pid>` 的会话（`source_kind: "runtime"`） | 插件；没有终端画面 |
| herdr 窗格里、没装插件 | herdr 的会话，只有终端 | 对话是一条说明，发消息被拒（`opencode_no_extension`），中断按 Esc |

## 结构

```
 App ── hub ── node ── ~/.persona/runtime.sock ◄── Persona 插件（每个 opencode 服务进程一条连接，runtime `opencode-<pid>`）
                │                                      · 列表、对话：只读 opencode.db
                │                                      · 实时：服务进程的事件流
                │                                      · 控制：插件上下文里的 session.prompt / interrupt / update、permission.reply
                └─ herdr ── 窗格里的 opencode TUI（herdr 自己的 opencode 插件报状态、会话 id、标题）
```

- **一个服务进程一条连接**：opencode 把所有 TUI 接到同一个 `opencode serve --service` 上，插件跑在这个进程里，**每个项目目录（location）加载一次**
  （收到该目录的第一个请求时才加载）。所以同一个进程里有很多个插件实例，每个只带自己目录的上下文。插件在 `globalThis` 里留一个宿主，
  每个目录的实例把上下文登记进去；事件流是服务器范围的（每个实例收到的是同样的事件），只由其中一个（primary）读，它走了换下一个，事件按 id 去重。
- **herdr 的一行怎么对上插件的一行**：herdr 的 opencode 插件在窗格里报 `agent_session`（就是 opencode 的会话 id，`ses_…`），标题是 `OC | <标题>`。
  node 用会话 id 把 herdr 的行和插件列的行并成一行（`mergeOpencodeRow`）：标题（插件的是完整标题，herdr 的会被截断）、最后一句、模型、上下文用量、修订号取插件的，
  状态取 herdr 的（插件说 `blocked` 时也算「需要你」）。herdr 没报会话 id 的窗格（旧版集成；服务进程里的插件不知道窗格）不会消失：
  列成 `opencode-<窗格>` 的只有终端的行（和下面「其他 agent 的终端行」一样）。
  这一行是 herdr 的（`source_kind` 为空、有终端）；插件列的那一行不再单独出现。调用按类型分：`session.keys` / `screen` / 终端 / 标签给 herdr，
  其余（内容、发送、中断、改名、卡片）给插件。
- **标题**：herdr 的 `OC | ` 前缀去掉；还没生成标题时 opencode 的终端标题就是 `OpenCode`，不当作标题。
- **通知**：状态来自 herdr，「需要你」「完成」的判断和推送是 hub 现有的逻辑（按会话状态的变化，和 Claude 的 herdr 会话一样），不为 opencode 另写。

## 其他 agent 的终端行

以前 node 对 herdr 报的、自己不认识的 agent（`gemini`、`amp`……）一律丢掉（`if a.Agent != "claude" { continue }`），手机上看不见这个窗格。
现在 herdr 报了 agent 但 Persona 没有读它对话的办法时，**列成只有终端的行**（`otherAgent`）：窗格、空间、标签、状态、标题都是 herdr 的，
会话 id 是 `<agent>-<窗格>`（agent 自己的会话 id 用不上，这一行就是这个窗格），对话是一条说明「Persona 还不能读 <agent> 的对话」，发消息被拒（`terminal_only`，不往不认识的 TUI 里盲打字），
终端画面和按键照常（herdr 的）。理由：用户在 herdr 里开了什么 agent，手机上就该看得到它在不在等你，不能悄悄消失；它的状态本来就是 herdr 报的，成本只是多一行。
herdr 没报 agent 的窗格（`agent: ""`）、botmux 的窗格仍然不列。这一行不能关闭（没有可恢复的会话，`session.retire` 拒绝）。

## 对话从哪来

opencode 的插件接口给的是会话的 context（压缩之后留下的那部分）和事件流，**没有会话列表，也没有完整的消息读取**；opencode 自己的数据库 `opencode.db`
（SQLite，WAL）有全部：`session_v2`（会话：目录、标题、模型、`parent_id`、`revert`……）和 `session_message`（消息：`type`、事件日志里的 `seq`、`data` JSON）。所以：

- 列表和对话**只读**这个数据库（`file:…?mode=ro`、`PRAGMA query_only`、300 ms 的 busy timeout，查询都走 `(session_id, seq)` 索引）；在 opencode 自己的进程里用 `bun:sqlite`，
  测试里用 `node:sqlite`。永远不写、不长时间占着。位置按 `$XDG_DATA_HOME/opencode/opencode.db`（默认 `~/.local/share/opencode`），`PERSONA_OPENCODE_DB` 可以指到别处。
  数据库打不开时列表为空、翻页为空，发送 / 中断 / 改名 / 卡片照常（它们走插件接口）。
- 实时的事情（一轮开始 / 结束、文字、权限）来自插件的事件流（`ctx.event.subscribe`）：`session.execution.started/succeeded/failed/interrupted`、
  `session.inbox.enqueued`、`session.text.delta`、`session.step.ended`、`session.tool.success`、`permission.asked/replied`、`form.created/replied`、`session.renamed/deleted`。
- 控制走插件上下文：`ctx.session.prompt({sessionID, text, files})`、`interrupt({sessionID})`、`update({sessionID, title})`、`ctx.permission.reply({sessionID, requestID, decision})`。
  这些函数的参数是一个对象，路径参数和请求体并在一起（和 HTTP 接口同名）。HTTP 接口要密码，插件不碰它。

### seq 和「一条消息还在写」

`session_message.seq` 是 opencode 事件日志里的序号：同一个会话里唯一、只增、**有空洞**（日志里还有别的事件），所以直接当 item 的 seq。

- 一条 assistant 消息在这一步里是**原地改写**的（工具调用一个个完成），seq 不变；而 SDK 的 `append` 对 `toSeq <= 已推的` 的批次不再推。
  所以**只推已经定稿的消息**：`time.completed` 有了（或有 `finish`）才算；中间状态靠逐字文字和「工作中」。定稿的前缀才算会话的内容：`to_seq`、翻页都截到第一条没定稿的消息之前。
  被打断的一步（没有 `completed`）在一轮结束后 1.5 秒算定稿（`SETTLE_MS`）；服务进程里一直显示在跑却 60 秒没更新的也算（漏了事件）。
  代价：一步里的工具调用要等这一步结束才出现在对话里（长命令跑的时候手机上看到的是「正在工作…」和逐字文字）。
- item 的 id 是 `<seq>#<部分序号×2>`，工具结果是 `<seq>#<部分序号×2+1>`：一个结果完成时同一条消息里后面部分的 id 不会移位。
- 第一次见到一个会话时（它的第一个事件）先记下当时的末尾作为基线，之后只推基线之后定稿的；手机在看的会话才会生成 item，没人看时只更新列表里的修订号。
- `history` = `opencode:<会话 id>`；用户在 opencode 里回退（`revert` 非空）时加上它的哈希，App 整页重取。

### 映射

`integrations/opencode/lib/map.js`，`session_message` → Persona item：

| opencode | Persona |
| --- | --- |
| `user`：`text`、`files` | user `text`；图片是占位（`lazy`，数据走 `session.image`），别的文件一行「［附件 名字］」 |
| `assistant` 的 `reasoning` / `text` / `tool` | `thinking` / `text` / `tool_use`（+ 完成或出错后的 `tool_result`，带 `tool_use_id`、`is_error`） |
| 工具名和参数（按 Claude 的画法） | `shell`→`Bash`（`command`、`description`）、`read`→`Read`（`path`→`file_path`）、`write`→`Write`、`edit`→`Edit`（`oldString`/`newString`→`old_string`/`new_string`）、`grep`→`Grep`、`glob`→`Glob`、`subagent`→`Agent`、`webfetch`→`WebFetch`、`websearch`→`WebSearch`、`skill`→`Skill`，别的（`patch`、`execute`……）用原名 |
| `finish: error` / 被打断（`error.type: aborted`） | notice「出错了：…」/「已中断」 |
| `idle`（`outcome: interrupted`） | notice「已中断」 |
| `compaction` | notice「已压缩上下文」 |
| 用户用 `!` 跑的命令（`shell`） | 一对 `Bash` 的 `tool_use` + `tool_result` |
| `system`、`synthetic`、模型 / agent / 目录切换 | 不出 item（seq 照样往前走） |

子 agent（`subagent` 工具开的子会话，`parent_id` 非空）不单列成会话；它们的权限请求算根会话的（卡片和「需要你」都在根会话上），别的事件忽略。

## 权限和提问

- **权限**（`permission.asked`：`action` 是 `shell` / `edit` / `read`…，`resources` 是命令或路径，`save` 是「总是允许」会记下的规则）：
  手机先答的话（`answerFirst`：`auto` = 手机最近 6 小时驱动过这个会话就先给手机，否则先给 opencode 的 TUI；`phone` / `local` 固定一边）
  卡片是 runtime 自己的（`agent: "opencode"`），工具名按 Claude 的画法（`Bash`、`Edit`…）；允许 = `once`；带 `save` 的卡有「总是允许 <规则>」= `always`；
  拒绝带理由时理由一起交给 opencode（`message`）。TUI 里的对话框一直在，**谁先答谁算**：TUI 里先答了，`permission.replied` 到了就撤掉手机上的卡；
  手机上点「交回电脑」不回答。会话「需要你」的状态在等的时候由插件报 `blocked`。
- **提问**（`form.created`，opencode 的 question 工具）：插件接口回答不了表单，**交回终端**——会话报 `blocked`（「需要你」），在 herdr 的终端里答。
  回答表单要走 HTTP 接口（要服务的密码），插件不去读密码文件，所以不做。以后 opencode 开放了插件接口再接。

## 安装

在跑 opencode 的那台电脑上（这台电脑要在 hub 的机器列表里，node 要能起来）。

1. `node <Persona 仓库>/integrations/opencode/install.mjs`：在 opencode 配置目录（`$XDG_CONFIG_HOME/opencode`，默认 `~/.config/opencode`，`--config-dir` 可改）的 `plugins/` 里
   写一个 `persona.js`，只有一行 `export { default } from "<仓库>/integrations/opencode/index.js"`（所以更新检出目录就是更新插件）。
   已有一个不是它写的同名文件时拒绝，`--force` 覆盖；`--uninstall` 删掉。opencode 自己会加载 `plugins/*.js`，不用改 `opencode.json`。
2. 重启 opencode 服务（`opencode serve --service` 那个进程）一次：插件在服务进程里，已经开着的服务不会自己加载新插件。重启服务会让连着它的 TUI 断一下，按你平时的做法重启。
3. herdr 的 opencode 集成（`herdr integration install opencode`）：报状态、会话 id、标题。已经装了的不用动。

别的 opencode 插件（通知类的）同时订阅事件流互不影响。

### 配置（可选）

`<opencode 配置目录>/persona.json`：

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `answerFirst` | `auto` | 权限卡先给谁（见上） |
| `listDays` | `7` | 没在跑的会话列最近几天的（手机碰过的、在跑的、等着的一定列） |
| `maxSessions` | `80` | 列表最多多少个 |
| `socket` | `~/.persona/runtime.sock` | node 的 runtime socket；`PERSONA_RUNTIME_SOCKET`（兼容 `JARVIS_RUNTIME_SOCKET`）也行 |

环境变量：`PERSONA_OPENCODE_DB`（opencode.db 的位置）、`PERSONA_OPENCODE_DEBUG=1`（插件的日志打到 stderr，在 opencode 服务的输出里看）。

## 限制和以后

- 只有装了插件的服务进程里的会话；一个服务进程的列表是数据库里最近的根会话（别的服务进程跑着的会话也在库里，会被列成空闲，
  node 选正在跑它的那个 runtime；一般只有一个共用的服务）。
- 一步里的工具调用要等这一步结束才出现在对话里（见上「seq」）。
- 提问（`form.created`）和 opencode 的 plan 模式不能在手机上答，交回终端。
- 不能从手机新建 opencode 会话 / 在 herdr 里开新窗格（没有 `start`）；停了的 herdr 会话也不能从手机接着（没有 `resume_argv`）。
- 权限模式、思考强度、换模型：插件没有 `modes`，手机上的这几个选择器对 opencode 会话回「不支持」。
- 自动更新（像 pi 那样把包打进 personad、node 带新版本）没做：插件是检出目录里的一个文件，`git pull` 之后重启服务就换上。以后要做时在 `runtimepkg.Packages` 和 SDK 的 `PACKAGES` 里登记。

## 踩过的坑

- **`setup` 要马上返回**。opencode 在请求里等「Location plugins did not finish activating」；在 `setup` 里 `await` 事件循环会让这个目录的所有请求 503。事件循环放后台（`void`）。
- **`setup` 要返回清理函数**，并让事件订阅跟着中止（`ctx.event.subscribe({signal})`）：服务进程收到 SIGINT 时等插件清理，订阅不停它会一直挂在那，只能 `kill -9`。
- **事件流是服务器范围的，每个目录的实例都会收到每个事件**（有几个实例就重复几遍）：按事件 `id` 去重，别在每个实例里各处理一遍。
- 插件上下文的函数是从 HTTP 接口生成的，参数是**一个对象**（路径参数 + 查询 + 请求体合并），不是位置参数。
- `ctx.session.get` 的 `time.idle` 比 `time.updated` 旧就是在跑（只在刚见到一个会话时用来猜）。
- 默认的公共模型在有些地区 403（`This model is not available in your country`）：端到端用脚本化的假模型（`e2e/mock-llm.mjs`），配成一个自定义 provider。
- v2 的配置键是 `providers` / `permissions` / `plugins`（不是 v1 的 `provider`），插件的类型包 `@opencode-ai/plugin` 1.x 里没有 v2 的 `session` / `event` / `permission`：以运行时探测为准。

## 验证

- `cd integrations/opencode && npm test`：映射（工具名和参数、id 不随结果移位、出错 / 打断 / 压缩 / 用户 shell）、列表（根会话、归档、过期、模型窗口、上下文用量）、翻页（按轮、往前往后、定稿截断）、
  发送（按 `client_msg_id` 只发一次、图片、按目录选上下文）、中断、改名、推送连续性和逐字、事件去重和 primary 交接、权限卡（允许 / 总是 / 拒绝带理由 / 交回 / TUI 先答）、子 agent 的权限、没有数据库、安装脚本。
- node：`server/internal/node/opencode_test.go`（假 herdr）：一行 herdr 会话、状态取 herdr 的 / 插件加 blocked、靠插件的窗格认会话、没装插件、其他 agent 的终端行、标题前缀。
- 端到端：`JARVISD=<personad> node integrations/opencode/e2e/run.mjs`：真的 `opencode serve`（临时的配置 / 数据 / 状态目录，插件由 `install.mjs` 装上）、脚本化的假模型、
  真的 `personad node` 当 hub：插件连上并列出会话、发消息得到回答、重发只进一次、权限卡到手机并允许 / 带理由拒绝、逐字、中断、改名、翻页、服务退出后会话消失。2026-11-05：16/16（opencode 2.0.32）。
- 网页：`MOCK_OPENCODE=1` 的假 hub + `scripts/test/web_opencode_e2e.py`：侧边栏里的 opencode 会话和没装插件 / 不认识的 agent 的终端行、对话里的工具调用、权限卡（宽屏 / 窄屏 × 浅色 / 深色）。
- 没验证的：真的 herdr 里的 TUI 和 herdr 的 opencode 插件一起跑（用的是 herdr 报的数据的样子，不是现场的）、用户自己的 `opencode serve --service`（没有往它的配置里装过）、
  真模型的长对话（压缩、很长的工具输出）。
