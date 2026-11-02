# 接入 Agent 运行时（runtime）

Jarvis 里的 Agent 有两种来路，接法不同：

| | 宿主（host）| 运行时（runtime） |
| --- | --- | --- |
| 是什么 | 终端开在哪：herdr、Herdr App、Orca（`docs/engines.md`） | 自己管会话、没有终端的 Agent 运行时：DSH（`docs/dsh.md`），以后的 pi 扩展、基于 DSH 的平台…… |
| 会话从哪来 | node 读窗格、按进程认会话，读 Claude / Codex 的 transcript | runtime 自己报会话、自己给内容 |
| 交互（提问 / 计划 / 授权） | Claude Code 的 hook | runtime 自己问（`interaction.create`） |
| 代码在哪 | Jarvis 里一份宿主实现（`internal/host`，`node/host*.go`） | **Jarvis 里不加代码**：runtime 那边用 SDK 实现同一个协议 |
| 一台机器 | 只选一个引擎 | 任意多个，和引擎并存 |
| App 怎么知道能做什么 | 宿主的能力（终端、按键…） | 会话带 `source_kind: "runtime"` 和 `capabilities`（和 Codex app-server 会话同一套字段），由 runtime 声明的 `caps` 算出 |

这份文档是第二种的规范：一个新的 runtime 怎么接进来、要实现什么、怎么验收。线上协议见 `docs/protocol.md`「Runtime ↔ node」，
node 的实现是 `server/internal/node/runtimes.go`。

## 多台机器怎么汇总

runtime 只连**本机**的 jarvisd node（`~/.jarvis/runtime.sock`）。多台机器的汇总不是 runtime 的事，是 Jarvis 本来的结构：

```
 手机 / 手表 / 网页 / Mac ── hub（一个，常开）── node（每台机器一个，ssh 或 tailcat）
                                                 ├─ 引擎：herdr / Herdr App / Orca 里的 Claude、Codex
                                                 └─ runtime.sock ◄── DSH、别的 runtime（这台机器上的）
```

所以「十台机器上的 DSH」= 十台机器各有一个 node、各自的 DSH 插件连自己的 node，hub 把它们合成一个列表（按机器 → 空间分组）。
runtime 不用知道 hub 在哪、不用开端口、不用鉴权（同一个系统用户的 0600 socket），网络（NAT、打洞）全由 node ↔ hub 那一段解决。

## 要实现什么

用 SDK（`integrations/runtime-sdk`，Node 22+，没有依赖）就只写一个 driver。必须的两个：

| driver 方法 | 返回 |
| --- | --- |
| `listSessions()` | 会话行：`session_id`、`title`、`cwd`、`status`（`working` / `blocked` / `done` / `idle` / `offline`）、`updated_at`、`rev`（内容一变就变，App 据此拉新内容）、`last_text`、`space_id` / `space_name`（同一个空间的会话在 App 里分在一组）、`model` |
| `items(params)` | 一页 item（参数和返回同 `session.items`：从最后往前 `turns` / `limit`，`before_seq` 往前翻，`after_seq` 往后补，`from_seq` + `through_seq` 取一段），带 `history`、`to_seq` |

可选的（有哪个，SDK 就在 `runtime.hello` 里声明哪个能力；没有的，node 替你回「不支持」，App 照常显示）：

| driver 方法 | 能力 |
| --- | --- |
| `cursor(sessionId)` | 现在的 `to_seq`（不写就用一页 `turns: 1` 的 `to_seq`） |
| `image(params)` / `item(params)` | 图片原始数据 / 被截断的 item 全文 |
| `send({session_id, text, images, client_msg_id})` | 从手机发消息。**按 `client_msg_id` 去重**：App 在重连时会重发 |
| `interrupt` / `rename` / `close` / `start({cwd, prompt})` | 中断、改名、关闭、从手机新建（有 `start` 且配了 `launcher` 时新建表单里多一个启动器） |
| `hasLocalClient()` | 这个 runtime 自己的界面开着没有（决定授权先给谁，见下） |

事情发生时调 SDK：

| 调用 | 什么时候 |
| --- | --- |
| （不用调） | 列表只列最近的就行：没列出的老会话 node 也认得是你的（列过的记着；从没列过的会问你一次 `items`），调用照样交给你，所以 `send` / `items` 要能处理不在列表里的会话 |
| `rt.sessionsChanged()` | 会话多了、少了、开始 / 结束一轮、改了名 |
| `rt.append(id, {items, fromSeq, toSeq, history})` | 有新内容落盘（没人看着时 SDK 只让列表刷新 rev）。`items` 可以是空的：聊天里不显示的内容也要推进 seq |
| `rt.stream(id, text)` / `rt.streamEnd(id)` | 正在生成的整段文字 / 这段结束（SDK 节流到最多 120 ms 一次） |
| `rt.ask(interaction, {signal})` | 要人决定：卡片到手机，等回答。返回 `Resolution`；`{dismiss: true}` = 交回电脑（手机上点了、过期、没有 hub），交给 runtime 自己的界面。期间会话自动显示「需要你」 |
| `rt.phoneFirst(id)` | 先问手机还是先问本地界面 |

SDK 负责的（每个 runtime 不用再写）：连接 / 重连 / `runtime.hello`、列表推送、watch 的补发和连续性（`from_seq` / `to_seq` 对不上时如实标出，
手机会补洞）、逐字节流、问手机时会话变「需要你」、node 重启时同一个交互用同一个 id 再问（`interactionId(...)`，最多等 2 分钟）、
被撤回的交互从手机上撤掉、「谁先回答」的策略（node 说没有 hub 时一律先给本地界面，`rt.hubConnected`）、断了以后尽快连回来（socket 文件在就每秒试一次）。

## 内容

- **seq**：每个会话一条单调递增的序号，从 1 开始（0 表示「还没有」）；一条 item 的 seq 是产生它的那条记录的序号，同一条记录可以出好几个 item，
  id 用 `<seq>#<序号>`。DSH 的事件 seq 从 0 开始，所以映射成 seq + 1。
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
- 会话 id 在这台机器上唯一，最长 128 个字符，不能是路径；`launcher` 名不要和 `~/.zshrc` 里的启动器重名。

## 最小例子

`integrations/runtime-sdk/examples/echo-runtime.mjs`：会话在内存里，收到什么就先「想」一下再回什么，消息里有 `ask` 时先要一个授权。
一百来行，就是新 runtime 的骨架：把 `sessions` 换成自己的会话存储、`reply` 换成自己的 Agent、item 的映射换成自己的
（参考 `integrations/dsh/lib/map.js`）。

```js
import { createRuntime, light } from '<Jarvis>/integrations/runtime-sdk/index.js'

const rt = createRuntime({
  listSessions: async () => [...],          // 会话行
  items: async (params) => ({ items, history, to_seq }),
  send: async ({ session_id, text, client_msg_id }) => { /* 交给 Agent，按 client_msg_id 去重 */ },
}, { runtime: 'acme', name: 'Acme', launcher: 'acme' })
rt.start()
// 内容落盘时：rt.append(id, { items: [light(item)], fromSeq, toSeq, history })
// 要人决定时：const res = await rt.ask({ id, kind: 'permission', session_id, tool_name, tool_input })
```

## 验收

一个 runtime 接好，要能过这些（`integrations/runtime-sdk/e2e/echo.mjs` 是模板：真的 `jarvisd node`，脚本当 hub；
`integrations/runtime-sdk/testing/node-harness.mjs` 起 node，`testing/fake-node.js` 给单元测试用）：

- [ ] 连上后 `launchers.list` 里有它的启动器（如果有），会话出现在 `sessions.list`，带 `agent`、`agent_label`、`space_kind`、`source_kind: "runtime"` 和 `capabilities`，没有 `pane_id`
- [ ] 从手机新建、发消息（同一个 `client_msg_id` 重发只进一次）、中断
- [ ] 内容：人的消息、思考、回复、工具调用和结果都在；从最后往前翻页、往后补
- [ ] watch 推的每一批都接着上一批（`from_seq` = 上一批的 `to_seq`）
- [ ] 逐字输出
- [ ] 授权 / 提问 / 计划到手机，回答回到 Agent；手机交回时本地界面照常弹
- [ ] 终端类调用回 `unsupported`；runtime 退出后它的会话从列表里消失

DSH 的端到端（`integrations/dsh/e2e/run.mjs`，真的 `dsh web` + 假模型）是同一套检查加上 DSH 自己的部分。

## 现有的 runtime

| runtime | 在哪 | 文档 |
| --- | --- | --- |
| DSH（`dsh`） | `integrations/dsh`：`dsh web` 的 Cordis 插件 | `docs/dsh.md` |
| echo（例子） | `integrations/runtime-sdk/examples/echo-runtime.mjs` | 本文「最小例子」 |

计划中：pi 扩展（issue #170 第 2 步）；基于 DSH 二次开发、不跑 `dsh web` 的平台（多租户、容器里跑 DSH 的那种）在平台侧实现同一协议，
直接复用 SDK 和 `integrations/dsh/lib/map.js`，Jarvis 这边不用加代码。
