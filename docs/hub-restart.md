# hub 重启对客户端无感

用户 2026-11-04：hub 每次部署或重启，iPhone、Mac、网页、安卓上的 Persona 都会掉线，主对话里正在跑的助手后台任务（Monitor、`run_in_background`）也会被停掉。
要求：**服务端重启时，客户端完全感觉不到**。issue #1574。

目标：一次正常部署，所有客户端上都不出现断开提示，不丢消息，不中断正在跑的对话。

## 现状（2026-11-04 核实，改动前，master `2fe55f92`）

代码位置写的是现在的文件和函数（2026-11-06 对过；`2fe55f92` 之后，机器连接（拉起 node、断线处理）从 hub/machine.go、hub/machine_conn.go 搬到了 `devices/`；`hub/machine.go` 还在，转发 node 事件的 `onEvent` 留在那里），行为说的仍是改动前。

### 重启的过程

- `scripts/deploy/deploy.sh:200-215` 部署 hub：`db.sh` → 拷 node 二进制 → `install_bin`（`jarvisd.new` 再 `mv`，原子替换）→ 插件程序 → `hub-systemd.sh`。
  `install_bin` 顺手重启这台的常驻 node（`deploy.sh:126`、`:137`，`node-launchd.sh restart`）。
- `scripts/deploy/hub-systemd.sh:34-55` 每次重写 `~/.config/systemd/user/jarvisd.service`（`Restart=always`、`RestartSec=3`，没写 `KillMode`，
  即 control-group），然后 `systemctl --user restart jarvisd`。除了 `is-active` 没有健康检查。
- 收到 SIGTERM（`cmd/personad/main.go` 的 `main` 里的 `signal.NotifyContext`）：hub 的 ctx 立刻取消，各台机器的连接同时被杀（`hub/hub.go` 的 `runHub` 里 `startDevices(ctx)` 起的连接）。
  `hub/public.go` 的 `serveHTTP` 里的停机顺序：先关监听 → `stopWebSockets` 给每个客户端发 1001「hub stopping」，等 0.5 s 后强关（`hub/shutdown.go` 的 `stopWebSockets`）
  → `http.Server.Shutdown`（每步 2 s，总预算 8 s，`shutdown.go` 的 `defaultShutdownTimeout` / `shutdownStepTimeout`）→ 等 node 连接 → 关数据库。**先关监听再排空**，排空期间端口是关着的。
- 监听是进程自己 `net.Listen`（`public.go` 的 `serveHTTP`，TLS 端口在 `startPublic`），没有 `SO_REUSEPORT`、`LISTEN_FDS` 或 socket activation。
  新进程在 `db.Open`（迁移）、插件、知识库等全部起来以后才监听（`hub.go` 的 `runHub`）。
- 实测（这台开发机 2026-11-04 19:30 那次重启的 journal）：19:30:29.88 开始停，30.93 停完，31.14 新进程开始监听，**端口关了约 1.3 s**，
  这期间的连接被拒绝（connection refused）。

### 各类连接在重启那一刻

| 连接 | 重启时 | 位置 |
| --- | --- | --- |
| 客户端的 WebSocket（`/api/v1/ws`） | 收到 1001 关闭；之后 1.3 s 里重连被拒绝，新进程起来后才连上 | `shutdown.go` 的 `stopWebSockets`，`http.go` 的 `serveWS`（停机中的升级给 503） |
| 进行中的 HTTP 请求 | `Shutdown` 最多等 2 s，超过就强关；这 1.3 s 里新发的请求被拒绝 | `public.go` 的 `serveHTTP` |
| 各台电脑的 node | hub 的 ctx 一取消就杀掉 `jarvisd node --stdio` / `ssh` 子进程（SIGKILL，`devices/machine.go` 的 `open` 里 `WaitDelay` 2 s），**同时**把机器标成离线、广播 `machines`、结束这台机器上不可续的交互（`devices/conn.go` 的 `detach`），这些广播客户端在被断开前就收到了 | `devices/machine.go` 的 `command`、`open` |
| 本机 node | 是 hub 的子进程（`node --stdio`），和 hub 一起被 systemd 的 cgroup 杀掉 | `devices/machine.go` 的 `command`（`local` 那支） |
| 插件（接飞书的扩展、removed 等） | hub 的子进程（stdio RPC），随 hub 的 cgroup 被杀，新 hub 再拉起；登录态在插件自己的目录里，重启只丢 hub 侧的会话和就绪状态 | `extension/host/host.go` 的 `spawn` |
| 连接器程序（telegram、wechat） | 同上，退避 5 s 起 | `connector/proc/proc.go` 的 `Conn.Run`（`restartFirst`） |
| jarvis-link（kind link 的电脑） | 收到 1001，1–60 s 指数退避重连，状态文字「hub 重启了」 | `link/client.go` 的 `backoff`、`summary` |

新 hub 起来后：本机 node 0.2 s 内连上，ssh 的 Mac 要 2–8 s（实测一台 +2.4 s、另一台 +8 s）。这段时间里机器显示离线、它的会话列表是空的。

### 客户端怎么发现断线、怎么恢复

| | iOS / Mac（`apple/Shared/LiveConnection.swift`） | 网页（`web/src/lib/store.ts`） | 安卓（`core/hub/.../OkHttpHubConnection.kt`） |
| --- | --- | --- | --- |
| 发现断线 | receive 失败；20 s ping，pong 晚 5 s 算卡住，45 s 没消息断开（`:266-269`、`:383-411`） | `onclose`；10 s 一次看门狗，70 s 没消息重连（`:217-245`） | `onClosing` / `onFailure`、hello 15 s 超时、前台 45 s 无消息（`:411-486`） |
| 重连 | 1 s 起翻倍到 20 s，无抖动（`:468-477`） | 600 ms 起翻倍到 15 s，±20% 抖动（`:222`） | 1 s 起翻倍到 20 s，×0.8–1.0；同步完成才复位（`:340-378`） |
| 界面 | 非在线状态持续 1.5 s 才显示「连接中…」「更新中…」（`ConnectionStatus.swift:39`、`:127-151`）；hub 自更新时显示「hub 正在更新…」 | 每次断开立刻变「连接中」，第三次失败起「连不上 hub，正在重试…」（`App.tsx:206-212`、`Sidebar.tsx:429`），没有宽限 | 离开 Online 延迟 1.5 s 发布（`:797-813`），超时立刻「连接中」，一连上先显示「更新中…」（`:511-513`） |
| 重连后 | 连接快照（machines、sessions、交互）+ 重拉交互、收件箱、待办、任务等；对话按 `since_seq` 续 | `hello` 后 `refresh()` 重拉 7 个接口 + 收件箱、插件槽、终端；对话按 `since_seq` 续 | 回到 Online 重拉机器、会话、交互；对话按 `since_seq` 续 |
| 断线期间的写操作 | 消息进持久化发件箱，连上后补发（`client_msg_id`）；审批 / 回答乐观移除、失败恢复，不重试、不带 op id | 发消息立刻失败（「没发出去：连不上 hub」）；审批失败弹「没送到」；不带 op id | 消息走持久化发件箱，带 `client_msg_id` 和 `X-Jarvis-Op-Id`，回执核对；审批不带 op id、不重试 |

共同点：**全局的实时事件没有序号**，断线期间的广播（`todos`、`inbox`、`lark`、`revs`、`interaction_resolved`……）全部丢失，只能靠重连后全量重拉；
只有对话内容能按 `since_seq` 续（`http.go` 的 `onClientMessage` 里的 `subscribe` 和 `sendBacklog`、`docs/protocol.md`「items 的连续性」）。hub 已经有幂等回执（`X-Jarvis-Op-Id`，`hub/clientops.go`、
`docs/protocol.md`「操作回执」）和 `client_msg_id` 去重，但只有安卓的发消息用了 op id。

一次重启客户端看到的：网页立刻变「连接中」（约 1.3 s 后连上，全量重拉一遍）；iOS / 安卓超过 1.5 s 才显示，一般刚好看得到「连接中…」或「更新中…」闪一下；
Mac 上的 ssh 机器离线 2–8 s，列表里它的会话先消失再出现。

### hub 里正在进行的工作

- 助手（Persona tab 主对话、副对话、定时任务、AI 分身、飞书 /agent、机器人、频道插件，`hub/assistant*.go`、AI 分身是管理者的 `assistant/inbox_agent.go`、飞书 /agent 在接飞书的插件里（#1571 起，经 hub 的公开接口开对话）、
  飞书机器人 `assistant/feishu.go` 的 `botSend`、频道插件 `assistant/channels_plugin.go` 的 `channelForward`）都走 `assistant.send`（机器人和频道插件现在只有开新对话走它，接着说走 `sendOnce` 的 `session.send_once`），由**助手所在机器的 node** 起无头 claude：
  `zsh -ic "<启动器> \"$@\"" … -p --input-format stream-json --output-format stream-json --include-partial-messages`（`node/assistant.go` 的 `spawnClaude`），
  独立进程组（`Setpgid`），stdin / stdout 是管道。
- 流式输出：`readAssistant` 把 `text_delta` 攒在内存的 `p.partial`，每 80 ms 发一次 `session.delta`（`node/assistant.go` 的 `readAssistant`、`emitDelta`），hub 转给订阅者
  （`hub/machine.go` 的 `onEvent`，`session.delta` 那支）。正在生成的文字只在内存里；写完的消息在 Claude Code 自己的 transcript 里。
- hub 重启时：node 的连接断开 → `Node.Serve` 返回 → `reapAssistants` 停掉所有助手进程（`node/assistant.go` 的 `reapAssistants`）；本机的 node 本来就和 hub 一起被 cgroup 杀掉。
  **常驻 node（`node --attach`）也一样**：每条 hub 连接一个 `Node`，新连接进来旧的就停（`node/attach.go` 的 `Attach`），它的助手进程一起停。
- Monitor、`run_in_background` 的后台命令是 claude 的子进程，随它一起死。node 记着每个对话的后台任务（`background_tasks_changed` → `state.put`），
  重启后 `recoverAssistantTasks` 给这些对话发一条「jarvisd 刚重启过……后台任务停了」（`node/assistant.go`、`prompts/assistant/lost_tasks.md`），让它自己重新布置。
- 用户看到的：正在回答的那条停在半截（流式的部分没了，只剩 transcript 里已写完的块），下一条消息时 `--resume` 接着；后台任务没了，对话收到一条「刚重启过」。
  herdr 里的 Agent 不受影响（herdr 是独立进程，只有观察它的辅助子进程会断）。

## 方案

### 零空窗：systemd socket activation

监听 socket 交给 `jarvisd.socket`（`ListenStream` 是 hub.json 的 `listen`，配了 `public.tls_listen` 再加一个），hub 启动时从 `LISTEN_FDS` 拿
（`hub/listen.go`：按端口对上，拿到就用，拿不到自己 `net.Listen`）。`systemctl --user restart jarvisd` 只重启服务，socket 一直在 systemd 手里：
旧进程停、新进程起的这段时间，新连接在 backlog 里排队等着（不是被拒绝），新进程一 `Accept` 就接上。

为什么选它：

- **SO_REUSEPORT + 新旧进程并存**：要先起新进程再停旧的，systemd 一个服务只有一个主进程，得改成 deploy 脚本在 unit 外面起新进程或者改 `Type=notify` 换主 PID，
  部署方式改动大；两个进程同时接连接时，WebSocket 会分到正在退出的那个。
- **自己传 fd（tableflip 式 re-exec）**：能做到新进程就绪再停旧的，但要 `Type=notify` + `NotifyAccess=all` + `MAINPID`、处理 cgroup 里的子进程，
  出错面大；收益只是省掉新进程启动的约 0.2 s，而这 0.2 s 在 backlog 里排队客户端看不出来。
- **socket activation**：标准、几行代码、对 Docker / launchd / 手动运行没有影响（没有 `LISTEN_FDS` 就自己监听）；回滚就是删掉 socket unit。
  代价：停旧进程那约 1 s 里连接要等，不能立刻得到回应——客户端的宽限远大于这个。

`hub-systemd.sh` 负责写两个 unit：第一次（或地址变了）要停一次服务把端口交给 socket（只这一次有约 1 s 的空窗），以后每次都只 `restart jarvisd`。
`TimeoutStopSec=20`（停机预算 8 s 之外留余量）。

### 蓝绿切换：hub 的入口（`internal/hubedge`，2026-11-06）

参考 relay-history 的做法：入口进程常驻并持有端口，后面挂两个槽位。起因是 2026-11-05 在 Linux 上复现的问题：自更新原地 `syscall.Exec` 新二进制时，systemd 传来的监听 socket 丢了。
启动时清掉了 `LISTEN_FDS`，`net.FileListener` 复制出的 fd 又带 close-on-exec，新进程只能自己 `bind`，端口却还在 `jarvisd.socket` 手里，于是报 `address already in use` 退出，
要等 systemd 用 `Restart=always` 再拉起来，还白白多算一次启动次数。自更新这条路也不走计划内停机，客户端收不到 `hub_restarting`。

现在 `personad hub run`（Linux、macOS）是一个**入口**：

- 入口持有 hub 的端口：systemd 给的 socket，没有就自己监听。整个服务运行期间一直不放。
- hub 本身作为**槽位**运行：`personad hub slot`，入口的子进程，在 `~/.persona/run/slot-<a|b>-<端口>.sock` 上监听。
- 入口逐字节转发每条连接，转发前先写一个 PROXY v2 头说明来源。槽位的 `RemoteAddr` / `LocalAddr` 因此还是客户端和它连上的地址，
  按来源判断信任（回环、局域网、VPN 网卡、用户自己的 PROXY protocol 上游）照旧。
- TLS 证书、公钥钉扎、数据库都归槽位，入口只转发 TCP。

切换一次（自更新、`personad hub switch`、deploy.sh），按顺序：

1. 新二进制作为 **standby** 槽位启动：报出版本和 build 号，自检配置，连得上数据库（`db.Check`，不迁移），然后等着。
   这期间旧槽位照常服务；版本不对、起不来、连不上库，就杀掉新的，旧的继续跑，没有任何中断。
2. 入口开始 **hold**：新连接在入口排队，不拒绝。然后通知旧槽位 **release**，它照计划内停机的流程停下：`hub_restarting`、交出机器和事件日志、1001。
3. 通知新槽位 **activate**：迁移数据库，接管机器，开始监听，报 ready。入口把路由切过去，排着的连接放给它。
4. 新槽位没能接手（迁移失败、退出、超时），就换回上一版：`--prev`，或者自更新时由入口把 pending 标记里的文件换回去。

另外几点：

- 连接在旧槽位的 socket 队列里还没被接、旧槽位就被 release 了：入口把客户端已经发出的字节（最多 64 KiB）原样交给下一个槽位，客户端看不出来。
  槽位崩溃时不这样重放，因为它可能已经处理了没来得及回答的请求。
- 槽位意外退出：入口 hold 住连接，按退避时间重新启动它；连续 5 次起不来，入口自己退出，交给服务管理器。
- `public.tls_listen` 指定的局域网地址已经不在这台机器上（DHCP 换了地址）：入口不持有这个端口，槽位也拿不到它的 socket（`ErrNoSocket`），
  于是关掉 TLS 照常启动，和 hub 自己监听时的做法一样（`docs/onboarding.md` 4.1）。
- 入口本身不跟着 hub 自动更新，只在服务重启时换（unit 变了、或者 `hub switch` 发现入口协议比新二进制旧，退出码 3）。
  所以入口要保持很薄：转发、排队、两个槽位、控制 socket（`~/.persona/run/hub-edge.sock`，0600）。

为什么不做两个 hub 同时服务（relay-history 的并行切换）：hub 里有很多同一时刻只能有一个的东西，机器连接、助手进程、机器人、连接器、扩展，还有数据库迁移。
新旧并存的只有 standby 自检这一段，交接时有 1–2 秒的 hold。实测（NAS，2026-11-06）：一次切换 0.68 秒，期间 4 路并发请求共 607 次，失败 0 次。

不经入口的情况：Windows、Docker（镜像更新）、`PERSONA_HUB_EDGE=0`、数据目录路径太长（Unix socket 地址超限）。这时 hub 照旧自己监听。
这条路上原地 exec 的问题也修了：systemd 给的 fd 保留下来（close-on-exec），exec 前清掉标志，再带上 `LISTEN_FDS` / `LISTEN_PID`（`listen.go` 的 `execListenEnv`）。

### 计划内停机（`hub/restart.go`）

SIGTERM 不再直接取消 hub 的 ctx，而是先 `beginStop`：

1. 标记 `stopping`；
2. 给每个客户端发 `{"type":"hub_restarting","back_in_ms":3000}`，等最多 300 ms 让它发出去；
3. 把此刻各台在线机器的 `NodeInfo` 和会话列表交给下一个 hub（Postgres settings `hub.warm_machines`）；
4. 再取消 ctx，照原来的顺序停：关监听 → 1001 关 WebSocket（原因改成「hub restarting」）→ 排空 HTTP → 等 node 连接 → **交出事件日志** → 关数据库。

停机期间 node 连接断开、机器离线、它的问题结束——这些都是停机本身造成的，**不再广播**（`detach` / `markOffline` 看 `stopping`）：
以前客户端在被断开前先收到一串「机器离线」「交互已结束」，然后再连上看它们回来。

### 事件续传（`hub/eventlog.go`）

- 所有经 `broadcastTo` 发给每个客户端的消息（`machines`、`interaction`、`interaction_resolved`、`todos`、`inbox`、`lark`、`revs`、`task_plan`、
  `assistant_feed`、`fleet`、`hub_update`、`plugin_ui`、`agents`、`mail_accounts`、`assistant_schedule`……）带 `event_seq`，同一条流里每条加一。
  hub 在内存里留最近的（最多 2000 条、8 MB、15 分钟）。
- 每条连接自己的消息不进日志：连接快照（`machines`、`sessions`、待处理交互、`desktops`）本身是完整状态；`sessions_delta` 按连接算；对话的 `items` 有
  `since_seq`，终端有 `epoch` / `after_seq`，各自续。
- 客户端重连时带 `GET /api/v1/ws?event_stream=<流>&event_seq=<最后一条>`。hub 在登记这个连接的同一把锁里（广播也在这把锁里编号和发送）先发 `hello`
  （带 `event_stream`、`event_seq`、`resumed`），`resumed: true` 时紧接着补发漏掉的，然后才是实时的：不会漏，也不会重。
  流不对、漏掉的已经不在日志里、或者超过 400 条（客户端的发送队列 512），`resumed: false`，客户端照旧全量刷新。
- 跨重启：停机时把日志（流的 id、序号、最近的事件）写进 settings `hub.event_log`，新进程启动时**取走**（`DELETE … RETURNING`，只读一次）接着编号。
  10 分钟内交接的才接着用。崩溃时没有交接，新进程开一条新流，客户端全量刷新——不会拿到重复的序号。
- 需要填写的授权卡片（elicitation）只发给声明了能力的客户端；重连补发时客户端还没发 `client_hello`，所以补发跳过它们，靠连接后的待处理交互快照补上。

### 机器和会话（`devices/warm.go`，hub 那头 `hub/devices_link.go` 的 `startDevices`）

2026-11-04 起电脑是管理者连的（[machines.md](machines.md)「谁连电脑」）：管理者是 hub 的子进程，hub 停的时候它也收到 SIGTERM，
先把此刻在线机器的 `NodeInfo` 和会话列表交给下一个（`hub.warm_machines`，键和格式都和以前 hub 交的一样，另加每台的引擎 `engines`），再断开它们（这时不报离线）。
hub 停机时等它退出（和以前等 node 连接一样，在停机预算里）。

新 hub 起来先**读**（不取走）`hub.warm_machines`（2 分钟内交接的），按它显示 hub.json 的机器，在管理者第一次上报之前对这些机器的调用（订阅补发、发消息……）
等它（最多 30 s），而不是立刻返回「离线」。新管理者起来**取走**这一份，给当时在线的机器恢复 `NodeInfo` 和会话列表、**30 s 内**照常显示在线，同时把 node 连回来：
重连上来的客户端看不到 Mac 先离线再回来。30 s 还没连上就按原来的方式显示离线（会话标成未知）并广播。
这段暖启动只是显示：hub 的自更新确认「之前在线的都连回来了」、之后更新各台 node，等的是 node 真连上（管理者报的 `connected`），和以前 hub 自己的暖启动一样。
升级到这一版的那次重启，交接是旧 hub 写的，新管理者照样接得上。

### 正在生成的文字

hub 记住每个会话最后一条 `session.delta`（空串清掉，机器真正离线时清掉），客户端订阅时在补发的 items 之后紧跟一条当前的 `delta`：
重连的客户端马上看到回答接着往下写，不用等下一个 token。

### 对话不中断：常驻 node 托管（`node/assistant_keep.go`）

助手的 claude 不再归某一条 hub 连接：

- hub 所在的机器装**常驻 node**（`scripts/deploy/node-launchd.sh install`，Linux 是 systemd 用户服务 `jarvisd-node`，独立的 cgroup）。
  hub 照旧起 `jarvisd node --stdio`，它发现常驻 node 在听就只做转发（`node/attach.go` 的 `Relay`）。hub 重启只杀掉转发进程。
- 常驻 node 里，一条连接结束时，它的 claude 对话交给进程级的 `assistantKeeper`（Codex 的对话连接属于 `Node`，照旧停掉）；下一条连接的 `Node`
  在加载状态前接管（`adopt`），之后对话的输出（流式文字、回合结束、后台任务变化）都报给新的 `Node`，也就是新的 hub。
  接管时把后台任务列表和正在生成的文字重新报一遍。`Attach` 等上一条连接交接完才服务下一条，不会两个 `Node` 抢。
- 输出缓冲：claude 的 stdout 一直由同一个读协程读（不会因为没有 hub 而阻塞），正在生成的文字在 `p.partial` 里，写完的在 transcript 里；
  没有 hub 的那 1–2 秒里的 delta 发不出去，接管后发一条完整的当前文字补上（delta 本来就是「到目前为止的全文」）。
- Monitor 和后台命令是 claude 的子进程，claude 不停它们就不停，**hub 重启后自动接着跑**，结束时照常唤醒对话。
  `recoverAssistantTasks` 只在对话进程真的没了时才发「刚重启过」（比如 node 自己重启了）。
- **node 自己更新**（部署换了二进制）：`deploy.sh` 改成 `node-launchd.sh restart-idle`，只给 node 主进程发 SIGHUP
  （`systemctl --user kill -s HUP --kill-whom=main`；Mac 用 `launchctl kill SIGHUP`），node 等到没有对话在回答、在等后台任务时，
  停掉空闲的对话进程，`exec` 新的二进制（PID 不变，systemd / launchd 看不出来）。空闲的对话下一条消息时 `--resume`。旧版常驻 node 不认 SIGHUP，会直接退出再被拉起，和以前的 `restart` 一样。

没有常驻 node 的机器（hub 直接 ssh 起 `node --stdio` 并由它自己处理）行为不变：hub 重启时对话照旧中断，后台任务照旧收到「刚重启过」。

### 客户端容忍短时间断线

客户端统一的做法（网页、安卓已做，iOS / Mac 见文末 issue）：

- **静默重连**：断线后先不改界面。收到 `hub_restarting` 或关闭码 1001：150–400 ms 后重连，之后每 0.5–1 s 一次，界面保持「在线」**30 s**；
  其他断线：第一次 300–600 ms 重试，界面保持在线 **8 s**，之后照旧的退避和提示。宽限内连上，界面什么都不变（不闪「连接中」、列表不刷）。
- **续传**：记住 hello 的 `event_stream` 和收到的最大 `event_seq`，重连时带上；`resumed: true` 不全量重拉，只照常重新订阅打开的对话（`since_seq`）；
  `resumed` 不为 true 照旧全量刷新。
- **写操作排队、幂等重放**：发消息、回答 / 审批带 `X-Jarvis-Op-Id`（一次逻辑操作一个 UUID，发消息时和 `client_msg_id` 相同；hub 对打断、关闭、改名、
  切模式、新建会话也认这个头，客户端可以逐步接上）。
  网络错误或不是 hub 自己给的 502 / 503 / 504，保持「发送中」，等连接回来用**同一个 id** 重发，最多 60 s；网页在 WebSocket 也断着时（hub 在重新部署）一直等到「hub 正在更新」的上限 5 分钟，hub 一回来就发（#4174）；hub 按 op id 返回第一次的结果，不会执行两次。
  hub 给出的真实错误（带 `code`）照旧立刻失败。

### node 和插件

- node：本机的常驻 node 和 hub 之间只是转发，hub 重启后 0.2 s 内重新连上并接管对话；ssh 的电脑照旧 2–8 s 重连，靠 warm start 让客户端看不出来。
- node 自己断开又马上回来（常驻 node 空闲时 `exec` 新二进制约 2 s、转发进程被换掉）：运行中的 hub 先把这台照原样显示 **5 s**
  （`reconnectGrace`，管理者 `devices/conn.go` 的 `detach`），回来了就什么都不报，过了 5 s 还没回来才照旧显示离线。jarvis-link 的电脑除外：链路断了就是电脑走了，立刻离线。
- 常驻 node 正在 `exec` 时它的 socket 文件还在、但没人听：`node --stdio` 的 `Relay` 遇到「拒绝连接」会等它最多 8 s（`dialResident`），
  而不是自己接下这条连接——那样助手的对话又会落到 hub 的 cgroup 里。没有 socket 文件（没装常驻 node）时立刻自己处理，和以前一样。
  node 侧状态（会话状态、后台任务）在 Postgres，连上后 `state.load`；hub 重新 `session.watch` 订阅中的会话（`since_seq`）。
- 交互（提问、审批）：可续的照旧等 hook 重连认领（`resumeGrace` 3 分钟）；停机时不再先结束、广播再恢复。
- 插件、连接器程序：仍是 hub 的子进程，随 hub 重启（登录态在插件自己的目录里，新 hub 一起来就拉起）。插件起来之前对它的请求会失败，和以前一样；
  客户端看到的插件状态由新 hub 推的 `plugin_ui` 和连接快照更新。把插件挪出 hub 的 cgroup 要处理新旧两个 hub 同时连同一个插件，这次不做。
- 助手的 MCP 工具（`personad mcp`）经 node 的 `node.sock` 转到 hub。这个 socket 归服务当前 hub 连接的 `Node`，交接那一两秒里没人听：
  `tools/list` 本来就一直重试到 node 起来（#1584）；`tools/call` 用 `mcpserver.callNode`，只在「连不上 socket」（什么都还没发出去）时每 0.5 s 重试，最多 15 s。已经发出去、正好赶上旧 hub 停机的调用
  （包括 hub 经 WebSocket 叫手机做事、等 `tool_result` 的）会失败，claude 拿到工具错误，自己决定要不要重试——不自动重试，因为发邮件这类调用不能做两次。

## 部署和回滚

改了两处部署方式：

| 改动 | 怎么做 | 回滚 |
| --- | --- | --- |
| hub 的监听交给 `jarvisd.socket` | `scripts/deploy/hub-systemd.sh`（deploy.sh 每次部署都跑）：写 `jarvisd.socket`、service 加 `Wants/After=jarvisd.socket` 和 `TimeoutStopSec=20`；第一次停一下服务把端口交给 socket | `PERSONA_HUB_SOCKET=0 scripts/deploy/hub-systemd.sh`：删 socket unit、停 socket、hub 自己监听；或者手动 `systemctl --user stop jarvisd; systemctl --user disable --now jarvisd.socket; rm ~/.config/systemd/user/jarvisd.socket; systemctl --user daemon-reload; systemctl --user start jarvisd` |
| hub 的机器装常驻 node | `scripts/deploy/node-launchd.sh install`（一次），之后 deploy.sh 用 `restart-idle` | `scripts/deploy/node-launchd.sh uninstall`，下次 hub 重连时回到 hub 自己起 `node --stdio` |

不动的：Docker 部署（`deploy/docker/`，没有 `LISTEN_FDS`，照旧自己监听）、Mac 上的 hub（launchd）、数据库和数据。

蓝绿切换（上面「蓝绿切换」）之后：

- deploy.sh 换 hub 的二进制前先把旧的留成 `jarvisd.prev`。
- `hub-systemd.sh` 在 unit 没变、hub 正在运行时，先跑 `jarvisd hub switch --prev …`：
  - 退出码 0：切换完成，什么都不重启；
  - 退出码 1：新版没能接手，旧版照常服务，脚本把 `jarvisd.prev` 放回原位，部署失败；
  - 其他退出码：没有入口（第一次部署这一版、旧二进制），或者入口协议更旧，就照原来的方式重启服务。
- Mac（launchd）也是先切换，不行再 `kickstart`。
- 回滚：在 hub 的机器上跑 `PERSONA_HUB_EDGE=0 scripts/deploy/hub-systemd.sh`。它把这个变量写进 unit 的 `Environment=`，服务重启后 hub 自己监听；
  以后的部署会保留它，直到用 `PERSONA_HUB_EDGE=1` 再跑一次。手动运行时直接设置这个环境变量。

入口要注意：

- `systemctl --user stop jarvisd` 停的是入口，入口先 release 槽位再退出（`TimeoutStopSec=20` 够用）。
  槽位卡住超过 20 秒才会被杀；被杀时扩展各在自己的进程组里，可能留下来，下次启动前看一眼 `ps`。
- 两个槽位交接时写同一个日志，每行前面带 `[slot a]` / `[slot b]`；入口自己的行以 `hub edge:` 开头。

用了 socket 之后要注意：

- **换回旧二进制之前先回滚 socket**：不认识 `LISTEN_FDS` 的旧 hub 会自己 `net.Listen`，端口在 socket 手里，起不来（`address already in use`）、反复重启。
  先 `PERSONA_HUB_SOCKET=0 scripts/deploy/hub-systemd.sh`，再换二进制。
- **`systemctl --user stop jarvisd` 停不住**：socket 还在听，下一个连接会把服务重新拉起来（socket activation 本来的行为）。
  真要停：`systemctl --user stop jarvisd.socket jarvisd`。

## 怎么验证

- 蓝绿切换：
  - `internal/hubedge` 的测试（Linux / macOS）：测试二进制自己扮演槽位。覆盖来源地址、切换期间请求零失败、6 次连续切换下的并发、起不来的新版、
    接手失败回到上一版、槽位自己请求切换、构建号不对被拒、未确认的更新回滚、崩溃后重启。
  - `hub` 的 `TestUpdateHandsOverToTheEdge` 等；`scripts/test/test_hub_switch_deploy.py`（hub-systemd.sh 的三种结果）。
  - 真实 personad 的端到端测试：私有 Postgres，v1 → `hub switch` → v2，同时持续打 `/api/v1/health`，再试一个起不来的二进制。
- 单元 / 集成测试（`server/`）：`TestEventLogSince`、`TestHelloResumes`、`TestStampEvent`、`TestListenInherited`（像 systemd 那样传 fd 3 给子进程）、
  `TestEventLogHandOver`、`TestRestartIsPlanned`（真跑两次 `hub.Run`：客户端先收到 `hub_restarting` 和 1001「hub restarting」，下一个 hub 接着同一条流、`resumed: true`），
  `node` 的 `TestResidentNodeKeepsConversations`（连接结束后对话进程还在，下一条连接的 `Node` 接管后收到它的输出）。
  网页 `src/lib/reconnect.test.ts`、`store-reconnect.test.ts`、`ops.test.ts`；安卓见 `docs/android.md`。
- 反复重启：`scripts/test/hub_restart_seamless.py --restarts N --converse`，在 hub 的机器上跑：
  - 每 100 ms 打一次 `/api/v1/health`，任何一次被拒绝 / 失败都算失败；
  - 一个照网页客户端做法的 WebSocket 客户端：每条广播的 `event_seq` 必须接着上一条（不丢、不重），第一次之后的每次重连都必须 `resumed`、同一条流，
    断开到下一个 hello 的时间不超过客户端的宽限（默认 8 s）；
  - `--converse`：新开一个**副对话**（不碰主对话），让它在后台跑 `sleep 75` 的命令、同时逐行输出 1 到 N；重启穿插在输出过程中，最后检查数字一个不少、
    后台命令的结果回来了、对话没收到「刚重启过」。
  - 最后一行 `PASS` / `FAIL` / `TIMEOUT` 和数字。

## 实测结果（2026-11-04，这台开发机的 hub，#1617 部署后）

部署：hub 切到 `jarvisd.socket`、装常驻 node（`jarvisd-node`），切换那一次停了约 1 s。之后：

| 场景 | 结果 |
| --- | --- |
| `hub_restart_seamless.py --restarts 3`（只挂客户端） | PASS：健康探针 247 次 0 失败（最慢 0.95 s，在 backlog 里等）；4 次连接里 3 次重连全部 `resumed`，同一条流，54 条事件不丢不重；客户端看到的断开 0.73–1.01 s（网页 / 安卓的宽限 8 s、重启时 30 s，界面不变）；每次重启连命令 1.3–1.6 s |
| `--restarts 3 --converse --count 300`（同时一个流式输出的副对话 + `sleep 75` 的后台命令） | PASS：健康探针 827 次 0 失败；3 次重连全部续传，121 条事件不丢不重，断开最长 0.81 s；回答 300 行一行不少，后台命令跑完回报了，对话没收到「刚重启过」；node 日志三次「took over 1 running conversations」（同一个 claude 进程） |
| hub 日志 | 每次重启：`event log: continuing stream … at N`、`warm start: 3 machines shown as before`、本机 node 0.2–1 s、两台 ssh 的 Mac 1–3 s 连回 |
| 常驻 node 空闲重启（`node-launchd.sh restart-idle`） | PID 不变（`exec`），hub 那边本机 node 断开约 2 s——当时会广播一次离线；之后加了上面的 5 s 宽限和 `dialResident`（#1574 的后续 PR） |

改动前的对照（同一台机器 2026-11-04 19:30 那次部署的 journal）：端口关约 1.3 s，期间连接被拒绝；本机 node 和它的助手进程被杀；两台 Mac 2.4 s、8 s 后才连回，期间显示离线。

## iOS / Mac（#1634）

规则和网页、安卓相同，Apple 端的实现（iOS 和 Mac 共用）：

- 纯逻辑在 `apple/Shared/Reconnect.swift`：`LinkDrop`（宽限 8 s / 30 s、重连节奏）、`EventCursor`（续传序号）、`FrameMeta`（解码时顺带读 `event_seq`、`event_stream`、`resumed`、`hub_restarting`）、
  `ReconnectPlan`（`resumed` 时重连后不重拉）、`OpRetry`（同一个 op id 重试，最多 60 s，`Error.isGap` 判断是不是「请求可能没到 hub」）。
- `LiveConnection` 记下断开（`drop`、`inGrace`、`graceOver`），宽限内 `ConnectionStatus` 把它当成连着；关闭码用 `URLSessionWebSocketTask.closeCode`（自己因为心跳超时关的不算 1001）。
- `AppModel.didConnect` 在 `resumed` 时只注册手机和重新订阅；发消息（`SendSafety.sendPending`）和回答（`AppModel.resolve`）带 op id。界面上的行为见 `docs/ios.md`「连接状态」。
- 验证：`PersonaTests/HubRestartTests`（含一个 Network.framework 的回环 WebSocket：`hub_restarting` + 1001 → 宽限、不到 1 秒重连、`resumed`）；
  mock hub 的 `MOCK_RESTART=1`（`GET /mock/restart?gap_ms=…&missed=…&drop_post=1` 模拟一次计划内重启，`docs/testing.md`）配模拟器看界面和请求。
