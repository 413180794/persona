# 接入 Agent 运行时（runtime）

Persona 里的 Agent 有两种来路，接法不同：

| | 宿主（host）| 运行时（runtime） |
| --- | --- | --- |
| 是什么 | 终端开在哪：herdr、Herdr App、Orca（`docs/engines.md`） | 自己管会话、没有终端的 Agent 运行时：DSH（`docs/dsh.md`），以后的 pi 扩展、基于 DSH 的平台…… |
| 会话从哪来 | node 读窗格、按进程认会话，读 Claude / Codex 的 transcript | runtime 自己报会话、自己给内容 |
| 交互（提问 / 计划 / 授权） | Claude Code 的 hook | runtime 自己问（`interaction.create`） |
| 代码在哪 | Persona 里一份宿主实现（`internal/host`，`node/host*.go`） | **Persona 里不加代码**：runtime 那边用 SDK 实现同一个协议 |
| 一台机器 | 只选一个引擎 | 任意多个，和引擎并存 |
| App 怎么知道能做什么 | 宿主的能力（终端、按键…） | 会话带 `source_kind: "runtime"` 和 `capabilities`（和 Codex app-server 会话同一套字段），由 runtime 声明的 `caps` 算出 |

Paseo 两边都沾：它没有终端，经 runtime 协议接入，却是一台机器可选的引擎（`docs/paseo.md`）。

这份文档是第二种的规范：一个新的 runtime 怎么接进来、要实现什么、怎么验收。线上协议见 `docs/protocol.md`「Runtime ↔ node」，
node 的实现是 `server/internal/node/runtimes.go`。

## 多台机器怎么汇总

runtime 只连**本机**的 personad node（`~/.persona/runtime.sock`）。多台机器的汇总不是 runtime 的事，是 Persona 本来的结构：

```
 手机 / 手表 / 网页 / Mac ── hub（一个，常开）── node（每台机器一个，ssh 或 tailcat）
                                                 ├─ 引擎：herdr / Herdr App / Orca 里的 Claude、Codex
                                                 └─ runtime.sock ◄── DSH、别的 runtime（这台机器上的）
```

所以「十台机器上的 DSH」= 十台机器各有一个 node、各自的 DSH 插件连自己的 node，hub 把它们合成一个列表（按机器 → 空间分组）。
runtime 不用知道 hub 在哪、不用开端口、不用鉴权（同一个系统用户的 0600 socket），网络（NAT、打洞）全由 node ↔ hub 那一段解决。

连不上本机 socket 的 runtime（自己是常驻服务：别的系统用户、容器里、别的机器上）反过来接，叫 **daemon**（`docs/daemons.md`）：
它开一个 WebSocket 端点，node 按 `~/.persona/daemons.json` 去连，带 token；连上以后说的还是这套协议，SDK 的 driver 一行不改
（`createRuntime(driver, {listen: {port, token}})`）。名字以配置为准、会话 id 加命名空间，daemon 碰不到本机别的会话，也没有 `mcp.*`。

## 要实现什么

用 SDK（`integrations/runtime-sdk`，Node 22+，没有依赖）就只写一个 driver。必须的两个：

| driver 方法 | 返回 |
| --- | --- |
| `listSessions()` | 会话行：`session_id`、`title`、`cwd`、`status`（`working` / `blocked` / `done` / `idle` / `offline`）、`updated_at`、`rev`（内容一变就变，App 据此拉新内容）、`last_text`、`space_id` / `space_name`（同一个空间的会话在 App 里分在一组）、`model`；可选 `permission_mode` / `effort`（现在用的模式）、`transcript`（这个会话的对话就是本机一份 Claude 对话记录：内容归 node，见下「跑 Claude Code 的 runtime」） |
| `items(params)` | 一页 item（参数和返回同 `session.items`：从最后往前 `turns` / `limit`，`before_seq` 往前翻，`after_seq` 往后补，`from_seq` + `through_seq` 取一段），带 `history`、`to_seq` |

可选的（有哪个，SDK 就在 `runtime.hello` 里声明哪个能力；没有的，node 替你回「不支持」，App 照常显示）：

| driver 方法 | 能力 |
| --- | --- |
| `cursor(sessionId)` | 现在的 `to_seq`（不写就用一页 `turns: 1` 的 `to_seq`） |
| `image(params)` / `item(params)` | 图片原始数据 / 被截断的 item 全文 |
| `send({session_id, text, images, client_msg_id})` | 从手机发消息。**按 `client_msg_id` 去重**：App 在重连时会重发 |
| `interrupt` / `rename` / `close` / `start({cwd, prompt})` | 中断、改名、关闭、从手机新建（有 `start` 且配了 `launcher` 时新建表单里多一个启动器） |
| `hasLocalClient()` | 这个 runtime 自己的界面开着没有（决定授权先给谁，见下） |
| `modes({session_id, permission_mode, effort})` | 手机切权限模式 / 思考强度（`caps.modes`；值是 Persona 的：`default` / `acceptEdits` / `plan` / `auto` / `bypassPermissions`，`low` … `max`） |
| `watchChanged(id, watching, {transcript})` | 手机开始 / 停止看一个会话（新的 node 连上、runtime 停下时，所有的都停）：只在有人看时才跟会话的 runtime 用（Paseo 按会话订阅 timeline） |

`createRuntime` 的选项里，跑多个 Agent 的 runtime 还可以给 `launchers: [{name, model, agent, models}]`：除 `launcher` 之外，新建表单还有这些项，一个 provider 一项，
各带它起的 Agent 和模型；以后变了调 `rt.launchersChanged(list)`。`resume: true`：`start` 接 `{resume: <Claude 会话 id>}`，接手一个它没有的 Claude 会话、
沿用这个 id（`caps.resume`，给当引擎的 runtime 接着「未运行」的会话用）。`start` 还会收到表单的选择（`launcher`、`model`、`permission_mode`、`effort`、
`workspace_id`），见 `docs/protocol.md`「Runtime ↔ node」。

事情发生时调 SDK：

| 调用 | 什么时候 |
| --- | --- |
| （不用调） | 列表只列最近的就行：没列出的老会话 node 也认得是你的（列过的记着；从没列过的会问你一次 `items`），调用照样交给你，所以 `send` / `items` 要能处理不在列表里的会话 |
| `rt.sessionsChanged()` | 会话多了、少了、开始 / 结束一轮、改了名 |
| `rt.append(id, {items, fromSeq, toSeq, history})` | 有新内容落盘（没人看着时 SDK 只让列表刷新 rev）。`items` 可以是空的：聊天里不显示的内容也要推进 seq |
| `rt.stream(id, text)` / `rt.streamEnd(id)` | 正在生成的整段文字 / 这段结束（SDK 节流到最多 120 ms 一次） |
| `rt.ask(interaction, {signal})` | 要人决定：卡片到手机，等回答。返回 `Resolution`；`{dismiss: true}` = 交回电脑（手机上点了、过期、没有 hub），交给 runtime 自己的界面。期间会话自动显示「需要你」 |
| `rt.phoneFirst(id)` | 先问手机还是先问本地界面 |
| `rt.tools()` / `rt.callTool({name, args, session_id})` | hub 的工具（手机的 `phone_*`、`mail_*`）给自己的 Agent 用：列出来注册成自己的工具，调用时经 node 交给 hub（`createRuntime` 的 `onHub` 在 hello 之后、hub 来去时回调，正好用来取工具） |
| 发消息时抛 `RpcError(…, 'handover', {delivered: 'none'})` | 只读磁盘的伴随进程：会话不在自己这儿跑，已经让能跑它的程序起来；node 把这条消息交给接手的 runtime（`requires: ['handover']`，DSH 的 `dsh-files`） |

SDK 负责的（每个 runtime 不用再写）：连接 / 重连 / `runtime.hello`（带上这个进程的 `instance`：另一个进程用了同一个 runtime id 时 node 不让连，SDK 记一行日志、每 30 秒再试，不抢）、列表推送、watch 的补发和连续性（`from_seq` / `to_seq` 对不上时如实标出，
手机会补洞）、逐字节流、问手机时会话变「需要你」、node 重启时同一个交互用同一个 id 再问（`interactionId(...)`，最多等 2 分钟）、
被撤回的交互从手机上撤掉、同一个 `client_msg_id` 只交给 driver 一次（重试拿第一次的结果，失败的可以再发）、「谁先回答」的策略（node 说没有 hub 时一律先给本地界面，`rt.hubConnected`）、断了以后尽快连回来（socket 文件在就每秒试一次）。

runtime 之间共用、但不属于协议的，也在 SDK 里，各 runtime 直接用：

| 用什么 | 干什么 |
| --- | --- |
| `rt.recent(rows, {days, max, pinned})`、`rt.touched(id)` | 挑要列的会话：正在跑 / 在等回答的（`pinned`）和手机碰过的（最近 50 个）一定列，其余 `days` 天内的按新旧补到 `max`。手机开着的会话必须一直列着 |
| `page(items, params, {history, toSeq})` | `session.items` 的分页（从最后按回合 / 条数、往前、往后补、按区间） |
| `appToolName(name, own)` | 工具名换成 App 画的那套（`bash`→`Bash`、`read`→`Read`…），runtime 补自己的（DSH 的 `ask_user_question`、pi 的 `find`）；参数名各自转 |
| `testing/model.mjs` 的 `scriptedModel()` | 端到端用的可编排假模型（DSH 的 llm-mock-server，按阶段换剧本） |
| `testing/conformance.mjs` 的 `conformance()` | 端到端的通用验收：文字回合（思考、回复、回合结束、翻页）、批准、带理由的拒绝、提问、逐字、中断、推送连续；runtime 只传自己的工具参数，再查自己特有的 |

没放进 SDK 的（只有一个 runtime 用，或两边读的是两种东西）：各自的会话存储读法、伴随进程的保活。

## 内容

- **seq**：每个会话一条单调递增的序号，从 1 开始（0 表示「还没有」）；一条 item 的 seq 是产生它的那条记录的序号，同一条记录可以出好几个 item，
  id 用 `<seq>#<序号>`。DSH 的事件 seq 从 0 开始，所以映射成 seq + 1。**id 只在会话内唯一**：不同会话、尤其是同一个会话分出来的
  两个分支，同一个 id 是不同的内容。客户端按 id 缓存的东西（渲染结果、解析结果）要连会话一起做 key（#372）。
- **history**：会话内容的身份（`docs/protocol.md`「items 的连续性」）。内容被重写（分支、重置）时换一个，App 会整页重取；只追加时不变。
- **item 种类**照 App 已有的：`text`（user / assistant）、`thinking`、`tool_use`（`tool_name`、`tool_input`、`tool_use_id`）、`tool_result`
  （`tool_use_id`、`is_error`）、`image`（`lazy: true` 占位，数据走 `image`）、`notice`（system：中断、出错、压缩）。
- **工具名对齐 Claude**：App 按 `Bash`、`Read`、`Edit`、`Write`、`Grep`、`Glob`、`WebFetch`、`WebSearch`、`TodoWrite` 画命令、diff、文件，
  参数名也用 Claude 的（`command`、`file_path`、`old_string` / `new_string`、`pattern`）。认不出的原名显示。
- **长度**：页里每条最多 24 KB 文字、工具输出 4 KB，用 SDK 的 `light(item)` 截（带 App 认的「已截断」说明和 `full_size`），全文走 `item`；
  工具输入里的长字符串用 `clipInput`。
- **只放人要看的**：注入给模型的上下文（时间、技能清单、系统指令）、调度用的内部事件不出 item。

## 交互

- 三种：`question`（`questions[{question, header, options[{label, description}], multi_select}]`，答案是「问题原文 → 选项文字」，多选用 `, ` 连，
  不在选项里的就是用户写的字）、`plan`（`plan` 正文；答案 `approve` + `feedback`）、`permission`（`tool_name`、`tool_input`、`summary`；答案 `allow`）。
- **谁先回答**（`answerFirst`）：`auto`（默认）= 手机最近 6 小时给这个会话发过消息或在手机上新建的、或者本地界面没开时先给手机，否则先给本地界面；
  `phone` / `local` 固定一边。手机上「交回电脑」、过期都回 `{dismiss: true}`，runtime 再交给本地界面，所以不会两边都没人管。
- **卡片是 runtime 自己的**：node 给它问的卡标上 `agent`（runtime 跑的 Agent）和 `agent_label`（它的名字），App 按「别的 Agent 的卡」画
  （和 Codex 的卡同一种：「DSH 需要你授权」、同意 / 拒绝、`reason` 放在命令上面），「总是同意」只在卡片带 `always_label`（说清楚总是同意的是什么）时出现，
  runtime 做不到就别给。runtime 跑的是 Claude Code（`agent: "claude"`）时按 Claude 的卡画。
- **拒绝时写的话**：`Resolution.message`。runtime 的授权能带理由就带上；带不了就先拒绝、再把这句话 steer 进这一轮（DSH 这样做）。
- **「需要你」不等于有卡**：会话在本地界面等人时报 `blocked`，手机上显示「需要你」，hub 不会去读屏幕造一张终端卡（没有终端能力的会话一律不造）。
- 有就用 runtime 自己的「定时提问」：本地先超时的，手机上的卡可以留着，晚到的回答再交回 runtime（DSH 的 `userQuestions.answer`）。

## 命名

- `runtime` id：小写，`[A-Za-z0-9][A-Za-z0-9_.-]*`，同时是 `space_kind` 和会话的默认 `agent`。`name` 是 App 里的叫法（会话带 `agent_label`）。
- runtime 里跑的是 App 认识的 Agent（比如一个跑 Claude Code 的平台）时，`agent` 填那个（`claude`），App 按它显示；否则不填。
- runtime id 在一台机器上只能有一个进程用：同一个程序要在一台机器上跑两份（两个 DSH profile），让 id、`name`、`launcher` 可以配置，
  node 拒绝第二个用同样 id 的进程（`runtime_conflict`）。runtime 改了 id 但跑的还是同一种 Agent 时，`agent` 填那种 Agent（改了名的 DSH 填 `dsh`），会话行带上 `agent_label`。
- 会话 id 在这台机器上唯一，最长 128 个字符，不能是路径；`launcher` 名不要和 `~/.zshrc` 里的启动器重名（重名时 shell 的为准，runtime 的不列），
  两个 runtime 的启动器重名时只有先连上的那个提供。

## 最小例子

`integrations/runtime-sdk/examples/echo-runtime.mjs`：会话在内存里，收到什么就先「想」一下再回什么，消息里有 `ask` 时先要一个授权。
一百来行，就是新 runtime 的骨架：把 `sessions` 换成自己的会话存储、`reply` 换成自己的 Agent、item 的映射换成自己的
（参考 `integrations/dsh/lib/map.js`）。

```js
import { createRuntime, light } from '<Persona>/integrations/runtime-sdk/index.js'

const rt = createRuntime({
  listSessions: async () => [...],          // 会话行
  items: async (params) => ({ items, history, to_seq }),
  send: async ({ session_id, text, client_msg_id }) => { /* 交给 Agent，按 client_msg_id 去重 */ },
}, { runtime: 'acme', name: 'Acme', launcher: 'acme' })
rt.start()
// 内容落盘时：rt.append(id, { items: [light(item)], fromSeq, toSeq, history })
// 要人决定时：const res = await rt.ask({ id, kind: 'permission', session_id, tool_name, tool_input })
```

## 跑 Claude Code 的 runtime（可选）

runtime 里跑的是 Claude Code（经 SDK，比如 Paseo，`docs/paseo.md`）时，它的会话本来就有一份本机的 Claude 对话记录。会话行带上
`transcript: <Claude 会话 id>`，node 就从这份对话记录给内容：items、图片、watch、`session.info`、子 Agent、行上的最后一句和「在做什么」，
和 herdr 里的 Claude 会话同一套解析、同一个样子；这份对话记录自己的「未运行」行不再列。runtime 只管状态、发送、卡片、模式，
watch 时收到 `transcript: true`，只推逐字文字（SDK 自动丢掉这种会话的 `append`）。还没写出对话记录的会话照常由 runtime 给内容，
文件出现后 node 自动换过去。daemon 不能这样做（会读到本机别的会话）。

## 子 Agent 和后台任务（可选）

Agent 把活交出去再结束这一轮（后台子 Agent、workflow、后台跑的命令）时，这一轮结束后会话是「完成」，事情还没完。runtime 在会话行上报它们，
字段和 Claude 会话的一样（`docs/protocol.md` 的 `Session.subagents` / `background`），App 和 hub 已经认：

- `subagents`：`[{id, description, type, running, tool_use_id, started_at, updated_at, last_text}]`。`id` 是子 Agent 在 runtime 里的 id（同样的字符集），
  **它的内容按会话 id `<会话>@<id>` 读**：node 把这个 id 的 `session.items` / `item` / `image` / `watch` 交给父会话所属的 runtime，
  runtime 推 `session.items` / `session.delta` 也用这个 id；node 一律把 `session_id` 改写成 `<会话>@<id>`，runtime 写什么都不算。
  `tool_use_id` 是启动它的那次工具调用，App 从那条调用点进去看它（调用名要对成 `Agent`，参数 `{description, prompt}`）。子 Agent 不单列成会话。
- `background`：还在跑、这一轮结束后会回来的任务 `[{id, kind, description, command, started_at}]`，`kind` 是 `shell`（`command` 是命令）或 `agent`；
  `output_file` 是本机文件，runtime 的不给（node 清掉）。前台正在等结果的调用不算。
- 有在跑的时：会话进 App 的「待唤醒」，hub 的「完成」推送等它们跑完、会话被叫醒的那一轮结束后再发。开始、结束时重推 `runtime.sessions`。
- 每行最多 50 个子 Agent、50 个后台任务，文字截到 500 字；不合法的 id 去掉。daemon 的子 Agent id 同样带前缀：`<daemon>.<会话>@<id>`。
- 对子 Agent 发消息、改名这类控制：runtime 不支持就回 `unsupported`（DSH 不支持：子 Agent 归父会话管）。

DSH 的做法见 `docs/dsh.md`「子 Agent 和后台任务」。

## 自动更新（可选）

runtime 是**装在别的程序里的插件**时（DSH 的插件），那个程序加载一次就一直留着它，换文件不会生效，要人去那个程序里重新加载。
为了不用每次去电脑上点，插件分成两层，代码由 node 带过来（#619）：

- **加载器**：宿主加载的那个文件（DSH 的 `integrations/dsh/index.js`）只做「选版本、加载、换版本」，用 SDK 的 `createLoader`
  （`runtime-sdk/lib/loader.js`），不再改它。**实现**（`dsh/lib/plugin.js` 和它 import 的一切）每个版本放在存放目录的
  `<store>/<哈希>/` 下，加载器从那里 import：随包附带的那份也先拷进去再加载，因为宿主的模块缓存按路径记，同一个进程里重新加载插件时，
  路径不变就还是旧代码。存放目录在 node 的 socket 旁边：`~/.persona/runtime-packages/<包名>/`，`current.json` 记着跑的是哪个。
- **包**：一组文件和它们的哈希（按路径排序，每个文件 `路径\0内容的 sha256\n`，再整体 sha256）。哪些文件算进包，Go
  （`server/internal/runtimepkg` 的 `Packages`）和 JS（`runtime-sdk/lib/package.js` 的 `PACKAGES`）各有一份清单，必须一样
  （`TestPackagesMatchTheLoader` 守着）。personad 构建前跑 `go generate ./internal/runtimepkg`，把这些文件从 `integrations/` 拷进来嵌进二进制
  （Dockerfile、`deploy.sh`、`publish.sh` 都跑），所以 node 带的总是和它同一次构建的插件。没跑生成的构建不带包，什么也不提供。
- **流程**：hello 带 `package: {name, hash}`；node 带着同名、哈希不同的包时，回复里 `update: {name, hash, version}`；
  SDK 把它交给 `onUpdate`，加载器用 `runtime.package` 取文件，核对路径、大小和哈希，写到 `<store>/<哈希>/`（先写旁边再改名，不会有写一半的版本），
  然后**等手机上没有这个 runtime 的卡片**（`rt.busy()`，最多等 2 小时），停掉旧的、import 新的、启动；新的起不来就回到旧的，
  这个哈希这次进程里不再试。新的连上 node 时 hello 报新哈希，node 不再提供。
- **以谁为准**：node 带的就是要跑的，不比新旧（node 回退，插件跟着回退）。只有一个例外：随包附带的文件被人换过（手动装了新的一份），
  它的哈希和 `current.json` 记的不一样，就先跑手动装的这份，node 有不同的再提供。
- **安全**：只有经本机 `runtime.sock` 连进来的 runtime 拿得到包（和 node 同一个用户）；daemon 拿不到（link 不转 `package`，
  `runtime.package` 回 `unsupported`），daemon 模式的 DSH 插件默认也不接受更新（`autoUpdate`），要换版本就重启它。
- **第一次**：在没有加载器的老插件上，要在宿主里手动重新加载一次，换成加载器；之后都是「发布 → hub → node → 插件」。加载器自己要改时同样要手动一次，所以它要尽量薄。
- **另一种宿主：自己会重新加载的（pi）**：pi 重新加载扩展时从磁盘读、不缓存模块，所以不用加载器那一层：扩展从存放目录的固定路径
  `<store>/current/…` 装（`current` 是指向 `<哈希>` 的符号链接，`installCurrent` 改名替换），收到提议后取包、装成新的 `current`，
  等空闲时让宿主重新加载（pi 用扩展自己注册的命令），新版本的宿主用同一个进程标识（`createRuntime` 的 `instance`）连上 node 顶替旧的。
  常驻的独立进程（pi 的伴随进程）装好后退出，交给 launchd / systemd 拉起。`selfPackage(import.meta.url, name)` 告诉一个模块它跑的是哪个包、
  在不在存放目录里（不在就不更新）。细节见 `docs/pi.md`「自动更新」。
- 要换版本的实现模块导出 `api`（`LOADER_API`，现在是 1）和 `start(host)`，返回 `{stop, busy, fetchPackage}`（就是 `createRuntime` 的同名方法），
  `host` 里有加载器的 `context`（DSH 是 `{ctx, config}`）和要传给 `createRuntime` 的 `package`、`onUpdate`。加一个这样的 runtime：在两份清单里登记它的文件，
  宿主加载的入口改成 `createLoader`。

## 验收

一个 runtime 接好，要能过这些（`integrations/runtime-sdk/e2e/echo.mjs` 是模板：真的 `personad node`，脚本当 hub；
`integrations/runtime-sdk/testing/node-harness.mjs` 起 node，`testing/fake-node.js` 给单元测试用）：

- [ ] 连上后 `launchers.list` 里有它的启动器（如果有），会话出现在 `sessions.list`，带 `agent`、`agent_label`、`space_kind`、`source_kind: "runtime"` 和 `capabilities`，没有 `pane_id`
- [ ] 从手机新建、发消息（同一个 `client_msg_id` 重发只进一次）、中断
- [ ] 内容：人的消息、思考、回复、工具调用和结果都在；从最后往前翻页、往后补
- [ ] watch 推的每一批都接着上一批（`from_seq` = 上一批的 `to_seq`）
- [ ] 逐字输出
- [ ] 授权 / 提问 / 计划到手机，回答回到 Agent；手机交回时本地界面照常弹
- [ ] 终端类调用回 `unsupported`；runtime 退出后它的会话从列表里消失
- [ ] （有子 Agent / 后台任务的）这一轮结束、它们还在跑时，会话行的 `subagents` / `background` 列着它们；`<会话>@<子 Agent>` 能翻页、watch；跑完后行上说完了
- [ ] （装在别的程序里、用加载器的）node 带另一个版本时自己换上并照常工作，宿主重启后跑换上的那个（`integrations/dsh/e2e/update.mjs` 是模板）

这些检查就是 `testing/conformance.mjs`：DSH（`integrations/dsh/e2e/run.mjs`，真的 `dsh web`）、pi（`integrations/pi/e2e/run.mjs`、
`herdr.mjs`）的端到端都是「起宿主 + 开会话 + `conformance()` + 自己特有的几项」。

## 助手（可选）

runtime 可以当「Persona」tab 的助手、AI 分身、飞书 `/agent`、定时任务的后端（它们都走 node 的 `assistant.send`），和 Claude Code、Codex 并列：
`createRuntime(…, {assistant: true})`（driver 要有 `start` 和 `send`）在 hello 里声明 `caps.assistant`，它的启动器在 `launchers.list` 里带
`assistant: true`，App 的助手启动器里就有它（`Launcher.runsAssistant`），hub 也接受它做 `assistant.launcher`。助手的那一层留在 node
（`server/internal/node/assistant_runtime.go`，代码里没有哪个 runtime 的名字）：对话的状态、每条消息开头的实时快照、权限模式、共用的记忆、
飞书的 token 和闸门。node 用协议里本来就有的调用驱动它：

| node 发 | 带的（都是可选字段，没声明 `caps.assistant` 的 runtime 收不到） |
| --- | --- |
| `session.start {cwd, assistant: true}` | `cwd` 是助手目录（`~/.persona/assistant`，里面有 `AGENTS.md` / `CLAUDE.md`、`.agents/skills/`），不带 `prompt` |
| `session.send {session_id, text, images, client_msg_id, assistant: true, context, mode, memory_dir}` | `context`：放在用户的字前面交给模型的 `<jarvis-context>` 块（实时快照；新对话的第一条还有记忆和飞书的说明），聊天里不显示；`mode`：Persona 的权限模式（`default` / `acceptEdits` / `auto` / `plan` / `bypassPermissions`）；`memory_dir`：记忆目录 |
| `session.interrupt` | 手机上的停止；关闭助手对话 = 停掉正在跑的这一轮，对话留在 runtime 里 |

声明了 `caps.assistant` 的 runtime 要做到：

1. **助手对话**（`assistant: true` 起的，或目录是助手目录的）的授权和提问一律先给手机（有 hub 时），不看本地界面开没开。
2. `mcp.tools {assistant: true}` 拿到的工具（手机的、邮箱的，再加助手专用的 `ask_user`、`schedule_followup`…和 node 自己的 `lark-connector`）
   挂给模型；助手专用的只给助手对话，调用时 `mcp.call {…, assistant: true}`。node 只在「这个 runtime 声明了助手、会话归它、是助手对话」时
   当成助手的调用，否则照普通工具（助手专用的就没有）。
3. 飞书一律走 `lark-connector` 工具：node 先过飞书闸门（读放行、会被别人看到的写在手机上确认、配置的规则拒绝），再在 node 上带 token 跑
   lark-connector skill 的 `ol.sh`；token 不进 runtime。shell 里直接跑 `ol` 拦下并说明。
4. `context` 放在用户的字前面交给模型，聊天里不显示（`<jarvis-context>` 不带属性的块；手机引用的上下文带属性，照常显示）。
5. 按 `mode` 设自己的权限（计划 = 只读）；`memory_dir` 里的改动在非计划模式下不问。

模型和思考强度由 runtime 自己定（DSH 在它的 Web GUI 里选），Persona 的模型配置对这种启动器不选具体模型。DSH 的做法见 `docs/dsh.md`「助手」，
端到端 `integrations/dsh/e2e/assistant.mjs`。

## 现有的 runtime

| runtime | 在哪 | 文档 |
| --- | --- | --- |
| DSH（`dsh`，可配置） | `integrations/dsh`：`dsh web` 的 Cordis 插件；DSH 没在运行时的伴随进程 `companion.mjs`（`dsh-files`） | `docs/dsh.md` |
| pi（`pi-<pid>`、`pi-web`、`pi-files`） | `integrations/pi`：pi 扩展（每个 pi 进程一条连接）+ pi-web 伴随进程 | `docs/pi.md` |
| echo（例子） | `integrations/runtime-sdk/examples/echo-runtime.mjs`（`--listen <端口>` 就是 daemon） | 本文「最小例子」、`docs/daemons.md` |

两个 runtime 列出同一个会话时（pi 的伴随进程从磁盘列、pi-web 或 pi 终端正在跑它），node 让正在跑它的那个负责（列成 `offline` 的排后面），
同等时新连接优先。会话列成 `offline` 就是告诉 node「我能打开它，但它没在我这儿跑」。

分页用 SDK 的 `page(items, params, {history, toSeq})`：runtime 只要按 seq 排好自己的 item。

依赖 node 新能力的 runtime 用 `createRuntime(…, {requires: ['prefer_running']})`：遇到不支持的旧 node 就不连（记一行日志），
等 socket 文件被换掉（常驻 node 重启，比如升级）或 10 分钟后再试；`rt.nodeHas(feature)` 查当前 node。由 node 启动、跑在别处的会话（pi 的伴随进程在 pi-web 里新建）报成 `offline`，
`session.start` 返回的是正在跑它的那个 runtime 的行。

基于 DSH 二次开发、不跑 `dsh web` 的平台（多租户、容器里跑 DSH 的那种）在平台侧实现同一协议，当 daemon 接进来（`docs/daemons.md`），
直接复用 SDK 和 `integrations/dsh/lib/map.js`，Persona 这边不用加代码。
