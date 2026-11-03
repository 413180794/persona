# daemon：由 node 去连的 runtime

runtime（`docs/runtimes.md`）一般是自己去连本机 node 的 `~/.persona/runtime.sock`。这个 socket 是 0600，所以 runtime 必须和 node 在同一台机器上、是同一个系统用户。

有些 Agent 运行时做不到这一点，它们是自己有服务端口的常驻服务（daemon），比如：

- 换了一个系统用户跑的 DSH；
- 跑在容器里的 Agent；
- 基于 DSH 二次开发的多用户平台；
- 别的机器上的服务。

这类服务反过来接：**daemon 开一个 WebSocket 端点，node 按配置去连它**。连上以后说的就是原来那套 runtime 协议（`docs/protocol.md`「Runtime ↔ node」），数据模型不变，只是多了一种连法。hub 和 App 不用改，Persona 里也没有哪个具体 daemon 的代码：接入方按本文的规范在自己那边实现。

2026-11-02 起支持（issue #325）。node 侧的代码在 `server/internal/node/daemons.go`，JS 的 daemon 用 SDK 的监听模式（`integrations/runtime-sdk`，见下面「SDK」一节）。

```
 App ── hub ── node ──┬── ~/.persona/runtime.sock ◄── 本机同一用户的 runtime（DSH 插件、pi 扩展…）
                      └── ~/.persona/daemons.json ──► ws(s)://<daemon>/…  daemon（别的用户 / 容器 / 机器）
```

## 规范（daemon 要实现的）

1. **端点**：一个 WebSocket 地址（`ws://` 或 `wss://`），路径由 daemon 自己定。
2. **握手**：
   - node 发过来的请求带 `Authorization: Bearer <token>` 和子协议 `jarvis-runtime.v1`。
   - token 不对时回 401。
   - daemon 必须在 `Sec-WebSocket-Protocol` 里回 `jarvis-runtime.v1`；不回的端点，node 当作不是 daemon，不连。
   - 不需要扩展（压缩）。
3. **帧**：每条 WebSocket 文本消息就是一个 JSON 对象，和 runtime.sock 上的一行一一对应：
   - 请求 `{id, method, params}`；
   - 应答 `{id, result}` 或 `{id, error: {message, code}}`；
   - 事件 `{event, params}`。

   单条消息最大 64 MiB。末尾带不带换行都行。
4. **协议**：和 runtime.sock 上完全一样。
   - daemon 先发 `runtime.hello`，然后推 `runtime.sessions`、`session.items`、`session.delta`，提问时发 `interaction.create`。
   - node 发来的 `session.*` 请求照常回答。
   - 能力照常在 `caps` 里声明。
5. **连接**：
   - 断了以后 node 会重新连，间隔 1 秒起，逐次翻倍，最长 1 分钟；连上过 30 秒以上再断的，又从 1 秒开始。
   - node 每 30 秒发一次 ping，15 秒内没有 pong 就断开重连。
   - 同一个端点来了新连接时，daemon 关掉旧的，以新的为准（node 重启、网络断开后重连都是这种情况）。
   - 一个端点只给一个 node 用：多个 Persona 用户就给每人一个 token（或一个端点）。
6. **会话 id 和交互 id 用 daemon 自己的**：
   - node 转给 hub 时会加上命名空间，转回 daemon 时再去掉，daemon 看不到加过前缀的 id。
   - 会话 id 照 runtime 的规则（`[A-Za-z0-9][A-Za-z0-9_.-]`），加上前缀以后的总长不能超过 128。

## 配置（node 那台机器上）

配置文件是 `runtime.sock` 旁边的 `daemons.json`，也就是 `~/.persona/daemons.json`，权限 0600：

```json
{
  "daemons": [
    {
      "id": "acme",
      "name": "Acme",
      "url": "wss://<daemon 地址>/jarvis/runtime",
      "token_file": "~/.config/acme/jarvis-token",
      "agent": "dsh",
      "launcher": "acme"
    }
  ]
}
```

| 字段 | 说明 |
| --- | --- |
| `id` | 必填，小写字母、数字、`_`、`-`，最长 32，不能有 `.`。用作这个 daemon 的 runtime id 和 `space_kind`，也是会话 id 的前缀。名字由你起，所以两个 daemon、或 daemon 和本机的 runtime 自己报的名字一样也不会撞 |
| `name` | App 里显示的名字，它的会话的 `agent_label` 和卡片都用这个名字。不填时用 daemon 自己报的，daemon 也没报就用 `id` |
| `url` | `ws://` / `wss://`（写 `http://` / `https://` 也一样）。不能在地址里带账号密码 |
| `token` / `token_file` | 二选一。`token_file` 在每次连接时重新读，换 token 不用重启。文件可读的人多于自己、又直接写了 `token` 时，日志里会提醒 |
| `agent` | 可选：它的会话跑的是什么 Agent，填了就不用它自己报的 |
| `launcher` | 可选：它能新建会话（`caps.start`）时，新建表单里这一项的名字，默认用 `id`。不用 daemon 自己报的启动器名 |
| `disabled` | `true` 时停用，但保留这一项 |

- 文件改了以后 node 在 10 秒内重新读，不用重启：
  - 新加的 daemon 去连；
  - 改了的断开重连；
  - 删掉的断开，它的会话从列表里消失，它的 id 不再保留。
- 写错的项会记日志并跳过，其余照常连。同一个 `id` 出现两次时，这两项都不连。
- 由谁去连：
  - 有常驻 node（`personad node --attach`）时由它连，hub 重连期间 daemon 的连接不断；
  - 没有常驻 node 时，由每条 hub 连接自己的 node 去连。
- node 日志里有连上、断开和连不上的原因（token 被拒、不是 WebSocket、没回子协议）。同一个原因只记一次，不会记 token 的值。

## 名字不撞车

- daemon 的 runtime id、显示名、启动器名都以配置为准，`runtime.hello` 里报的这几个字段会被改写。
- 配置里写了的 id 是保留的：
  - 本机 socket 上的 runtime 用这个 id 说 hello，会被拒，错误码 `runtime_conflict`。
  - 反过来，这个 id 已经被本机一个连着的 runtime 占用时，daemon 的 hello 被拒，node 断开它，等那个 runtime 走了再连上。日志里写明原因。
  - 本机 socket 上几个 runtime 之间重名按 `instance` 判断（`docs/protocol.md`「Runtime ↔ node」）：另一个进程用同一个 id 时被拒。daemon 的 `instance` 由 node 定（`daemon:<id>`），daemon 自己报的不算，所以它重连时旧连接被关掉，而不会被当成另一个进程。
- 会话 id：
  - daemon 的会话 `s1` 在 hub 和 App 里是 `<id>.s1`；`space_id` 也加上前缀，变成 `<id>:<原值>`。
  - 两个 daemon、或 daemon 和本机的会话 id 一样也不会串，App 也不会把它们归到同一个空间。
- 交互 id：daemon 给的 id 换成由 daemon 的 `id` 和原 id 算出来的稳定值。它重连后用同一个 id 再问，手机上还是挂在原来那张卡上。

## 隔离：daemon 能做和不能做的

daemon 不一定可信，比如多租户平台、或者容器里的东西被提示注入了。所以 node 在它和 runtime 之间加了一层：

| 能 | 不能 |
| --- | --- |
| 列出、推送、翻页**自己的**会话 | 看到或认领别的会话。node 不知道一个会话归谁时会问每个 runtime，不带这个 daemon 前缀的 id 直接在 node 这边回答，daemon 收不到 |
| 回答发消息、中断、改名、关闭、新建（按 `caps` 声明的） | 声明别的能力：只放行 start / send / interrupt / rename / close。`caps.assistant` 去掉，daemon 不能跑 Persona 的助手（`docs/runtimes.md`「助手」）；以后协议加的新能力也要在 `daemonCaps` 里单独放行 |
| 问手机（提问、计划、授权），卡片上是配置里的名字 | 用 `mcp.*`（手机工具、Persona 工具）：`mcp.tools` 回空列表，`mcp.call` 回 `unsupported` |
| | 借 `pane_id` 认领本机终端的窗格，或者借 `resume_argv` 在本机终端引擎里起命令：这两个字段丢掉，`features` 里也不告诉它有 `resume_argv` |
| | 发消息时回 `code: handover`，让 node 把这条消息转给别的 runtime：node 这边改成 `not_running`，什么都没发；`features` 里也不告诉它有 `handover` |

## SDK

`integrations/runtime-sdk` 的 `createRuntime` 加了 `listen` 选项，driver 不用改，同一个 runtime 两种连法都能用：

```js
import { createRuntime } from '<Persona>/integrations/runtime-sdk/index.js'

// SDK 自己起端点（默认 127.0.0.1、路径 /jarvis/runtime；port 0 表示随便挑一个）：
const rt = createRuntime(driver, { runtime: 'acme', name: 'Acme', listen: { port: 7311, token } })
const addr = await rt.start()

// 或挂到 daemon 已有的 HTTP 服务上：
const rt2 = createRuntime(driver, { runtime: 'acme', listen: true, token: () => readToken() })
server.on('upgrade', (req, socket, head) => { if (req.url === '/api/jarvis') rt2.accept(req, socket, head) })
```

- 握手（token 用常量时间比较、检查子协议）和服务端的 WebSocket 帧（分片、ping、64 MiB 上限、拒绝没有掩码的帧）都在 `lib/listen.js` 里，不依赖别的包。
- `token` 可以是一个函数，每次连接时调用一次。
- `runtime.hello` 里的 `runtime`、`name`、`launcher` 仍然要填，node 会换成配置里的值。
- DSH 插件加了 `listen` 配置（`docs/dsh.md`「配置」）：DSH 跑在别的用户下或容器里时，不用写代码就能当 daemon 接进来。

不用 JS 写的 daemon 照上面「规范」一节自己实现：一个 WebSocket 端点，每条消息一个 JSON。

## 验收

- node 的单测（`go test ./internal/node -run TestDaemon`）：
  - 配置校验，以及错误信息里不出现 token；
  - 名字以配置为准，会话 id 和 `space_id` 加前缀，危险字段丢掉，只放行规定的能力；
  - 推送和提问带上 hub 的 id；
  - 不认识的会话 id 不发给 daemon，拒绝 `mcp.*`；
  - 保留 id，两个方向都验；
  - token 错误、没回子协议时不连，换成 token 文件后能连；
  - 改配置不用重启就生效；
  - daemon 断开后重新连上，常驻 node 下 hub 重连时 daemon 的连接不断；
  - 消息和行的转换。
- SDK 的单测（`cd integrations/runtime-sdk && npm test`，`test/listen.test.js`）：
  - 监听模式和挂到已有 HTTP 服务上两种用法；
  - token、子协议、路径不对时拒绝，日志里不出现 token；
  - 新连接替换旧连接；
  - 分片消息、消息中间夹的 ping、没有掩码的帧。
- 端到端：
  - `JARVISD=<jarvisd> node integrations/runtime-sdk/e2e/daemon.mjs`：用真的 `personad node`，把 echo 例子当 daemon，脚本扮演 hub。覆盖：启动器名、新建、会话 id 前缀、行的字段、发消息（重发只进一次）、授权卡片、逐字输出、翻页、中断、终端调用被拒、本机 runtime 抢保留 id、同一个 runtime 在 socket 上和当 daemon 同时存在、token 错误、改配置生效、daemon 重启后重新连上、从配置里删掉、常驻 node 下 hub 重连。2026-11-02 实测 22/22 通过。
  - `JARVIS_E2E_DAEMON=1 DSH_CHECKOUT=<编译过的 dsh 源码> JARVISD=<jarvisd> node integrations/dsh/e2e/run.mjs`：用真的 `dsh web`，DSH 插件当 daemon（配置里写 `listen`），跑完整的 runtime 验收（`conformance()`：文字回合、批准、带理由的拒绝、提问、逐字输出、中断、推送连续），外加从手机关闭（在 DSH 里归档）和「daemon 拿不到手机工具」。2026-11-02 实测 21/21 通过，原来的 socket 方式同样 21/21。

一个新的 daemon 接好以后，照 `e2e/daemon.mjs` 写自己的端到端：把起 echo daemon 的那段换成起自己的服务。

## 限制

- 只支持 WebSocket，不支持裸 TCP 或 unix socket 上的 HTTP，有需要再加。WebSocket 本来就能走 `wss://` 和转发 Upgrade 的反向代理。
- daemon 不能当助手后端（`caps.assistant`），也没有 `mcp.*`：DSH 插件当 daemon 时没有手机工具。以后有实际场景时单独放行，并写明信任边界。
- daemon 列成 `offline` 的会话，node 不会在本机终端里替它恢复（没有 `resume_argv`）。
