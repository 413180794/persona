# Codex app-server：多实例接入研究

## 范围与结论

2026-11-01。目标是让 Persona 收进一台机器上的**多个已有 app-server、每个 server 中的多个 Agent / thread**，电脑上的桌面 App 继续正常使用，手机能查看、发消息、处理提问与批准。app-server 是 Agent 的协议入口，不是新的终端引擎；不能让用户为了用它把整台机器从 herdr 切走。

研究阶段只做源码研究，没有接管、重启现有 app-server，没有发送真实消息、改变权限或安装第三方服务。2026-11-02 的第一版实现与验收边界见文末；前面的源码行号仍固定在研究基线。`repos/` 已被 `.gitignore` 忽略，上游代码不进入 Persona 的提交；所有个人目录 / endpoint 都应来自配置或发现结果，不写在代码里。

### 研究版本

| 对象 | 固定版本 | 使用方式 |
| --- | --- | --- |
| 上游源码 | `openai/codex` main，`67f0994b532b8740f14b5b738af7b7b2a915b1d6`（2026-10-31） | `repos/codex/`，只读，未构建 / 执行源码 |
| 桌面 App 协议对照 | `rust-v0.163.0-alpha.10.2`，`d9151c94a70013f176ff370fa93da02734662e41` | 用 Git 读取少量启动 / 协议资料，不在用户进程里试验 |
| Persona 现有文档基线 | codex-cli `0.159.3`（`docs/assistant.md`） | 不能把新 main 的全部能力当成现有客户端已有 |
| Persona 研究基线 | `7d44afae9de547078627e3f4b77fafa7230a7153` | 本文 Persona 行号对应这一版 |

下文 `codex-rs/…` 均相对于 `repos/codex/` 的固定 main commit；版本 tag 证据会单独写出。源码链接可以用 `https://github.com/openai/codex/blob/<固定提交>/<相对路径>#L<行号>`，不依赖会移动的 main。

### 已确认的核心判断

1. **能接入，但前提是拥有可连接入口**：上游支持单 server 多连接、多 thread；已经开启的 Unix/WebSocket server 可以让桌面和 Persona 各自连接。一个普通 stdio 进程没有“第二客户端 attach”口。
2. **Unix socket 里面也是 WebSocket**，不是换行 JSON。不能直接复用 Persona 的 unix JSON-lines RPC 客户端去拨它。
3. **不能新起 app-server、resume 同一 thread 来模拟接管**：同 home 的 writer 文件锁会拒绝第二个写者；锁不提供 owner endpoint，不是抢占协议。不要删除锁或尝试强行 resume。
4. **没有通用无副作用订阅 RPC**：read/list 不加载 thread；实时订阅由 start/resume/fork 建立，resume 可能冷启动、重配或派发已有队列。发现历史时不要顺手 resume。
5. **同一审批可以送给多个订阅者**，先响应的 callback 被消费；response/error 都可能消费请求。`serverRequest/resolved` 只证明请求已结束，不包含“批准”结论。手机必须撤下已在电脑回答的卡片，不得据通知制造授权。
6. **进程、home、thread、连接代际必须分开建模**：磁盘看得到不等于这个 server 正在运行它；从错误 server 读取的状态还可能把另一进程的未完成轮次显示成 Interrupted。

### 当前桌面版本抽查（不是对全部 main 功能的兼容保证）

以下行号相对于 `rust-v0.163.0-alpha.10.2`，用 `git show <tag>:<路径>` 核对：

- `codex-rs/cli/src/main.rs:583–594` 已列出 `--listen stdio://|unix://|unix://PATH|ws://IP:PORT|off`，默认 stdio；`app-server/src/lib.rs:793–837` 实际分派三种 transport。
- `app-server-transport/src/transport/unix_socket.rs:176` 已用 `accept_hdr_async_with_config` 做 WebSocket upgrade；不是只在新 main 才这样。
- `app-server-protocol/src/protocol/common.rs:580,853` 已有 unsubscribe / loaded/list；方法表未找到独立 thread/subscribe。
- `rollout/src/writer_lock.rs:17,65–70` 已使用 home 下 writer lock 和 `try_lock`；`app-server/tests/suite/v2/thread_resume.rs:319–383` 有两个进程争同 thread 的 legacy/paginated 回归。
- `app-server/src/request_processors/thread_processor.rs:4569–4612` 已有 warm resume 配置差异 / subscriber 条件；`thread_queue.rs:430` 已有 cold resume 派发持久队列的测试。
- `app-server/src/outgoing_message.rs:473,567–585` 已在回应时取走 callback；`thread_lifecycle.rs:963` 已在 resume 后重放请求；`thread_resume.rs:5577,5714` 有命令 / 文件审批重放测试。

这些是静态证据，测试没有执行。main 的协议仍在变化（与 tag 的方法表已有新增），HumanCommand / 用户验证等特殊资格规则必须按版本适配，不因都叫 V2 就认为 wire shape 和能力一样；初始化成功也不是所有 experimental 方法都支持的承诺。

## 连接入口：怎样共用，怎样不能共用

| 入口 | 实际语义与推荐 | main 实现证据 |
| --- | --- | --- |
| stdio | 一个固定 stdin/stdout、一个 ConnectionId；没有 accept/attach。桌面进程未开放其他入口时只同步本地历史，控制能力禁用。不能抢管道、注入初始化或 kill 后替换进程。 | `codex-rs/app-server-transport/src/transport/stdio.rs:52–95` |
| Unix | 推荐的本机受保护入口。默认路径为 `<CODEX_HOME>/app-server-control/app-server-control.sock`；使用 WebSocket upgrade/frames，每条连接独立。长路径还有安全 rendezvous 处理，不凭字符串猜 socket。 | `app-server-transport/src/transport/mod.rs:56–73`；`codex-rs/app-server-transport/src/transport/unix_socket.rs:148–245` |
| TCP WebSocket | 每连接独立握手和响应；node 可拨 loopback。main 拒绝带 Origin 的 HTTP 请求，非 loopback 无认证不能启动；不要让浏览器 / 手机直接拨裸 Codex 服务。认证在 upgrade 检查，撤销后不能假设存量连接即时失效。 | `codex-rs/app-server-transport/src/transport/websocket.rs:99–127,160–184`；`websocket-auth/src/lib.rs:286–318` |
| managed daemon | daemon 启动 `app-server --listen unix://`，默认实例按 home 分开；不是把已运行的桌面 stdio 进程自动收编。连接现有 daemon 和“确保 daemon 存在、必要时启动”是两种操作，发现阶段只能前者。 | `app-server-daemon/src/backend/pid.rs:392–405`；`app-server-daemon/src/lib.rs:793–840` |
| OpenAI remote-control | 另一个账户 / relay 授权体系，不是 local WebSocket 的别名。依赖 ChatGPT 账户、backend/managed requirements 等；不作为 Persona 自动发现的捷径。`remoteControl/enable` 改的是已存在 controller 的状态，不给外部 stdio 进程添加本地 listener。 | `app-server-transport/src/transport/remote_control/auth.rs:100–143`；`app-server/src/request_processors/remote_control_processor.rs:41–60` |

Unix socket 文件模式为 0600，目录与 rendezvous 也做所有者/权限检查（`codex-rs/app-server-transport/src/transport/unix_socket.rs:79–98,325–340,497–503`）。Persona 不能为了连接去放宽权限。源码查到的本地 transport 是启动时选择，未发现运行中新增本地 listener 的 RPC。

**当前桌面是 stdio 时的落地办法**：在桌面应用支持的前提下，让下一次正常启动使用可共享的官方 Unix/daemon 通道，桌面和 node 同连原 server；这需要桌面端确实支持相应连接设置。不能只给旧桌面启动命令加 `--listen unix://`，却还让它继续向 stdin 写请求。若桌面端没有共享连接配置，需在桌面端/启动集成里增加合作式通道；普通 TCP 字节转发不能代替多客户端 JSON-RPC 路由、一次初始化、响应/事件/审批分发。没有这种合作时，Persona 明确只读，不承诺已“接手”。

## thread、状态与发送语义

| API / 机制 | 已确认行为 | main 实现证据 |
| --- | --- | --- |
| `thread/list` | 持久化摘要，turns 为空；默认过滤 provider、interactive source、archived。只给**本进程** loaded thread 补运行态；不是全机器活跃 Agent 列表。可有 metadata reconcile/read-repair，不是物理零写入。 | `app-server/src/request_processors/thread_processor.rs:2734–2901,5991–6058`；`rollout/src/recorder.rs:654–676` |
| `thread/loaded/list` | 当前进程 ThreadManager 的内存 thread ID，分页；loaded 包括空闲，不等于 running；过滤部分 internal/sample threads。只应据它建立当前 server 的运行归属，不拿 cwd / 最近文件猜 owner。 | `thread_processor.rs:3026–3074`；`core/src/thread_manager.rs:1855–1893` |
| `thread/read` | 不加载、不订阅，读持久化记录并叠加本进程状态。includeTurns 的历史可能涉及 flush/persist；ephemeral、未物化的 thread 有不同限制。读返回的 Interrupted 可能只是当前 server 没有该活跃 runtime，并非电脑真的停止了。 | `thread_processor.rs:3103–3327`；`thread_lifecycle.rs:1361–1374` |
| `thread/resume` | warm 通常复用 runtime 并加订阅，但无订阅、Idle、非 Running、可直接输入且配置有差异时可能 shutdown/rebuild。cold 会建立 runtime/writer；resume 的 idle 生命周期可自动派发已有队列。不是发现 API。 | `thread_processor.rs:4335–4427,4687–4757`；`codex-rs/app-server/tests/suite/v2/thread_queue.rs:430` |
| 订阅与重连 | 没有独立 thread/subscribe；start/resume/fork 附订阅，unsubscribe 移除当前连接。warm resume 在 listener 顺序中发 active turn 快照/响应，随后 usage 和 pending 请求；不是任意历史事件重播。delta 没有全局序号或重放 cursor，重连后靠快照 / 历史补洞。 | `app-server-protocol/src/protocol/common.rs:555–599`；`thread_lifecycle.rs:1019–1083,1144–1264`；`codex-rs/app-server-protocol/src/protocol/v2/item.rs:1542–1547` |
| `turn/start` | 同 thread 的变更请求按 thread ID 串行，不同 thread 独立。start 实际调用 `start_or_steer_turn`，活跃时可能插入原轮次；不能把“start 请求成功”当另开了一轮。 | `app-server/src/request_serialization.rs:25–29,71–74`；`codex-rs/app-server/src/request_processors/turn_processor.rs:690–725` |
| `turn/steer` | 必须带 observed `expectedTurnId`；core 会核对活跃轮次，对 review/compaction 等场景拒绝。不要把任意失败变成 start；断线/超时先核对是否已接收，不能盲重发。 | `core/src/session/turn_input.rs:878–908` |
| `turn/interrupt` | app-server 会检查 turn ID，再提交无 turn ID 的 core Op::Interrupt；不是全链路原子 CAS。不能因此默认再清空该 thread 的所有后台命令。 | `codex-rs/app-server/src/request_processors/turn_processor.rs:1470–1513` |
| `thread/queue/*` | experimental。CRUD 可作用于持久化 unloaded thread；queue/start 必须本进程已加载、空闲 admission 成功才消费队列，busy 时保留。dispatch 锁为进程内 per-thread Mutex，不能声称崩溃窗口 exactly-once。 | `thread_queue_processor.rs:186–228,253–310`；`ext/queue/src/service.rs:368–447` |
| 子 Agent | 单 server 内部是多个 thread；parentThreadId/sessionId（树）与 forkedFromId（复制）不同。main 部分 V2 child 由 parent 管理，不能直接 turn input；需按版本明确能力。 | `core/src/thread_manager.rs:520–539`；`codex-rs/app-server-protocol/src/protocol/v2/thread_data.rs:205–294`；`thread_processor.rs:4092–4142` |

实时接入必须是**已验证原 owner 的 warm resume，不带 model/cwd/sandbox/developerInstructions 等覆盖**；也要认识“增加订阅”不是绝对无副作用，不在初次目录发现时对所有 thread 批量 resume。操作 unloaded history 需要明确的续聊/加载授权，不能偷偷启另一个 server 去续用户的活跃 thread。

## 多 server 共用 home：锁不是接管协议

main 的 `codex-rs/rollout/src/writer_lock.rs:19–87` 明确在 `<CODEX_HOME>/thread-writer-locks/<threadId>.lock` 上 `File::try_lock()`，持有 guard 至写入结束；`thread-store/src/local/live_writer.rs:56–81` 的创建和恢复会取得它。它是跨进程 writer 锁，不只是 ThreadManager 内的 Mutex，也不是 SQLite 锁或会自动过期的 TTL lease。

因此多个 app-server 可以共用一个 home 管不同 thread，但不能让两个进程同时拥有同 thread 的 writer。`codex-rs/app-server/tests/suite/v2/thread_resume.rs:321–326` 专门覆盖：另一进程有 active writer 时第二进程 resume 失败；原进程 shutdown 后才可成功。这个锁不提供 owner 的 socket，不用它探测路由、不删除它、不把换 home 或重命名 thread 当绕过方式。

不同 home 不共享锁命名空间，path alias、外部 rollout 或非 LocalThreadStore 的行为本次未实测。Persona 仍需自己的 owner 冲突检测和 fail-closed，不把上游锁当作覆盖一切的安全网。

## 审批、提问和跨客户端收尾

- 服务端按 thread 的当前订阅者快照发送请求（`app-server/src/request_processors/thread_lifecycle.rs:563–592`）。数字 request ID 来自 server 内递增计数，重启会复用；客户端请求则由 `(ConnectionId,RequestId)` 区分（`outgoing_message.rs:57–62,277–280`）。
- `outgoing_message.rs:657–685` 在锁里消费 callback，**先 response/error 的客户端获胜，不是“先批准”获胜**。HumanCommand 和 user verification 有额外资格/owner 条件；普通审批不能套用这些特殊规则，订阅不是只读 ACL。
- `serverRequest/resolved` 只有 threadId/requestId，没有批准结果（`codex-rs/app-server-protocol/src/protocol/v2/notification.rs:71–76`）。监听器排序后向当前订阅者发出（`thread_lifecycle.rs:1314–1328`）；手机只撤卡，不能据此伪造 Allow/Approve 或提升权限。
- connect/initialize 不直接回放审批。resume 后回放**仍 pending** 的请求且保持原 ID（`thread_lifecycle.rs:1230–1259`、`outgoing_message.rs:525–560,688–701`）；user verification 不重放，原 owner 断开则取消。恢复后的卡片需去重，已回答 / 已过期的旧卡不能回到可批准状态。
- 只读观察者不能自动对未知 server request 回 error 或 decline：那也可能抢先消费共享 callback、使桌面端失去回答机会。现有 Persona 单 owner 的 unsupported-method 错误路径（`assistant_codex.go:802`）不能直接照搬到共享连接。dynamic tool 等只交给真正具有 handler 的客户端；普通手机审批只有用户明确点了才回复。
- `connection_cleanup.rs:19–39` 关闭 admission 后异步清理连接；非 stdio 的 server 不因最后客户端断开立刻退出，thread 无订阅且不活跃达到超时才 unload（`thread_lifecycle.rs:56–60`）。stdio 的 stdin 关闭则退出主循环（`app-server/src/lib.rs:1196–1218`）。断开 Persona 不能自动杀外部进程，但也不能承诺最后 observer 离开对其生命周期完全无影响。
- WS 慢客户端会被断开；stdio 队列满会等待（`app-server/src/transport.rs:141–178,219–241`）。在 node 回调线程上解码、按 thread 限队列、后台处理分页/磁盘，不在收消息处等待手机审批。

## Persona 已经有什么，缺什么

| 现状 | 证据 | 接入时的含义 |
| --- | --- | --- |
| 一段助手对话一个自启 app-server、一个 thread | `docs/assistant.md:218`；`server/internal/node/assistant_codex.go:46` 的 `codexConn` 只有一个 `threadID` / `turnID` | 已有连接封装可复用，但连接状态与 thread 状态必须解耦 |
| 数字请求 ID、pending 表、返回 / 错误 / 超时处理 | `assistant_codex.go:92` | 请求 ID 只在所属连接内配对，不建全局数字 ID 表 |
| initialize、thread/start/resume、start/steer、审批、流式输出 | `assistant_codex.go:266`、`:356`、`:683` | 改成多 thread 路由后复用，不重写整套协议 |
| initialize 返回 `codexHome`，目前没有保存使用 | `assistant_codex.go:268`；返回的 rollout `path` 暂存 `n.paths`（`:310`） | 单次活跃连接能读到别的 home，不代表重启后的目录发现已经解决 |
| Codex 文件查找只有默认 `~/.codex` | `server/internal/node/codex.go:52` | 要像 Claude 多目录发现一样支持多个 home，不能写死桌面 App 的目录名 |
| 离线列表扫描的是 Claude projects | `server/internal/node/sessions.go:559` | 能按已知 ID 读某份 Codex rollout，不等于所有桌面 Codex 历史会自动出现在列表 |
| Node 多个缓存 / 控制表按 session ID；hub / App / DB 是 machine + session | `server/internal/node/node.go:43`；`server/internal/proto/proto.go:279`；`server/internal/db/migrations/0001_init.sql:22` | 多来源同 ID 会冲突，新增 source 身份不能只加在显示文案里 |
| 子 thread 通知被忽略 | `assistant_codex.go:526` | 不能直接让一个连接承载多个 thread，然后继续复用这个过滤条件 |
| 审批归属使用单进程 `p.id`；ID 哈希不含机器 / 连接代际 | `assistant_codex.go:690`、`:948` | 必须按真正的 request/thread/turn 路由，不能把其他 Agent 的请求给当前助手批准 |
| steer 任意错误都会 fallback start；interrupt 之后清理后台终端 | `assistant_codex.go:377`、`:408` | 外部 server 不沿用这两个默认：只有确定本轮结束的错误才允许改走 start；停止轮次与清理所有后台命令要分开 |

可借用目录发现、realpath 去重与落库机制：`server/internal/node/claudedirs.go:176`、`server/internal/hub/claudedirs.go:19`。只提取 allowlist 中的环境字段（如 `CODEX_HOME`），不把进程全部环境变量、token、账号配置发到 hub 或打印日志。Codex 自己的 `find_codex_home` 会规范化显式 home（`codex-rs/core/src/config/mod.rs:5239`），Persona 也应按规范化目录去重。

## 协议内容不能只支持文本

上游 main `codex-rs/app-server-protocol/src/protocol/v2/turn.rs:474` 的 `UserInput` 包含 text（带 text_elements）、image、localImage、audio、localAudio、skill、mention。输入字段按实际版本能力降级；手机文件先通过已有 upload 到对应 node，再由该 node 转成可访问路径，不把手机路径当成电脑路径。

`codex-rs/app-server-protocol/src/protocol/v2/item.rs:238` 的 ThreadItem 带可选开始 / 完成时间，payload（`:299`）包括用户消息、Agent 消息（phase）、reasoning、命令、文件变更、MCP、dynamic tool、collabAgentToolCall（sender / receiver thread IDs）、subagent activity、搜索与图片等。不要用最后一个 text 覆盖同一轮的实际答案；hook / 后台通知也不能当人类的新输入或批准。

未知方法 / item 保留原始协议记录，在 UI 标为暂未支持的事件，不静默丢弃，不因为看不懂请求就自动批准。Dynamic tool 的 `item/tool/call` 是**要求客户端执行工具**，与审批不同；一个观察客户端不能假装拥有桌面端的动态工具实现。

## 接入调用顺序（设计示例，未对真实会话执行）

1. 在 node 上发现所有允许访问的 app-server 进程：只记录 pid/启动时间、可执行文件、`CODEX_HOME`、已配置/已开启的 endpoint。home 规范化去重，进程与 endpoint 不按“同目录最新”猜；持久化已知 home，离线后历史仍能找回。
2. 对**已有且许可的** Unix/WS endpoint 建立独立连接。Unix 先完成 WebSocket handshake，stdio 仅用于 Persona 自己创建并拥有的 server。每条连接 `initialize` 一次（clientInfo / 所需且支持的 capabilities）再 `initialized`，保留返回的 codexHome 和版本/能力信息。
3. 每实例调用 `thread/loaded/list` 取完整分页，建立 owner 路由。每 home 的历史另取 `thread/list`；处理默认 provider/source/archive 过滤，不把磁盘历史误认为 loaded runtime。`sourceKinds` 在 main 包含 cli/vscode/exec/appServer/subAgent 等（`codex-rs/app-server-protocol/src/protocol/v2/thread.rs:1552`），子 Agent 来源和父子关系也要收进来。
4. 先 `thread/read {threadId, includeTurns:false}` 取摘要/状态，打开对话时才分页取历史；长对话采用版本支持的 turns/items 分页或本地 rollout 窗口，不对每个 Agent 全量 read(includeTurns:true)。metadata-only 对 fresh/ephemeral 的限制用可见状态说明，不能当作不存在。
5. 用户选择实时接入/处理时，先核对 thread 仍在原实例的 loaded/list，再无配置覆盖地 `thread/resume {threadId, excludeTurns:true}` 加入该 runtime；收到响应/快照后接收事件和仍 pending 的审批。若 thread 已 unloaded，只降为可读历史；需要明确续聊才能 cold resume，不能失败后偷偷改到另一个 server。
6. 空闲时 `turn/start`，正在运行时 `turn/steer` 带 observed expectedTurnId。完整输入保留 text_elements、图片/文件/skill/mention 等；原 App 仍拥有相同 runtime。每个 thread 串行发送，失败是未发送/已发送未知/明确拒绝要区分，超时不盲重试。
7. 收到 server request 后按 `(instance, generation, requestId)` 登记手机交互；用户真正回答时才返回该 request ID。收到 `serverRequest/resolved` 立即撤卡，保留“别处已结束”的历史，不合成批准。只观察模式不响应动态工具请求，不默认发送错误抢占桌面的处理。
8. 断线后清理连接本地 pending/临时 delta，不杀 external process，不清用户后台任务。重新连接→initialize→owner确认→授权范围内warm resume→快照/历史收敛。server/generation变化时旧请求失效，不能复用整数 ID；最后订阅者离开后的上游 unload 行为要对 UI 说明。

若想由 Persona **新建自己的**共享服务，官方启动形式可以是 `CODEX_HOME=<已配置目录> codex app-server --listen unix://`，或显式私有 Unix socket。该操作与“接入已经运行的桌面 server”不同，需要独立的启动/续聊授权；不能执行一遍这个命令就声称接管了原 stdio 进程。

## 推荐数据模型

以下是待实现的设计，不代表已经存在的接口。

- `CodexHome`：machine + 规范化 home，存历史来源 / 已知目录，文件为原始记录，不写第三方 SQLite。
- `CodexServer`：machine + 配置 / 注册实例 ID；含 owner（Persona 管理 / 外部）、home、transport、endpoint、能力、连接状态。PID + 进程启动时间仅是当前运行代际，不能当永远稳定的主键。
- `CodexThread`：conversation identity = machine + home/source + 原始 thread ID；运行路由另带 server + connection generation，不把 PID 塞进永久对话 ID。
- 同 home、同 thread 在两个 server 加载：同一份历史可以去重，但运行 route 必须都保留；未验证 owner 时禁止自动挑任意 server 写入。不同 home 的相同 thread ID 不能合并。
- Interaction identity：machine + server/generation + 原始 server request ID（支持数字和字符串）+ thread；附 turn/item/approval ID，回答必须回到提出它的连接。断连后的旧 response 不能送到重启后的新连接。
- UI 将来源显示为桌面 App / app-server，不伪造终端 pane。没有终端就没有终端接管、按键、窗格关闭；提供的动作必须由 capabilities 决定。
- 连接只发生在所属 node：Unix socket 或回环 endpoint，手机依旧通过已鉴权的 hub/node 通道操作；不把上游端口直接暴露公网，不为接入自动开启 OpenAI remote-control，不扩张账号 / token 权限。
- 原始协议与日志按数据分类存储：认证 / token / 用户验证 proof 不进普通对话或日志；命令和消息只进入用户自己的会话记录，不能作为公共测试 fixture。未知事件可保留脱敏结构及本地原始记录引用，而不是无差别落全量 JSON。

优先在服务端保留现有 machine/session 路由，给 app-server 来源生成可逆映射或持久化的 opaque session key，另外传原始 thread/source 字段；改现有校验、缓存、持久化、订阅、图片、Agent 工具和客户端类型时一起迁移。不能只让列表多显示一个名字，却仍用旧 thread ID 控制另一份同名对话。

## 实施和验收边界

1. **发现与只读历史**：多个 home / 进程发现、持久化、离线目录扫描、正确来源标记。外部 stdio 没有可连接入口时，也应能显示历史，但明确“仅查看”，不能承诺立即发送。
2. **连接和实时路由**：同机多个 endpoint / thread 的协议路由，history 与实时合并，断线恢复，读者与执行者权限隔离。先用合成 fake server；只有独立假 HOME 的自有测试实例才做真实协议测试，不碰用户现有桌面会话。
3. **发送与交互**：支持版本允许的输入、start / steer / interrupt 和队列；接入提问 / 审批 / 其他客户端已回答的收尾。多个客户端只认真实用户操作；事件和恢复不能制造批准。
4. **客户端验收**：iOS/macOS/Android/web 的来源与能力显示，流式、刷新、重开、图片与新结果不丢，深浅色截图。没有可用 Mac 时明确未验证，不用 Go 通过代替 UI 验收。

验证矩阵至少包括：两个 server 不同 home；相同 home 不同 thread；相同 home 同 thread 的歧义；PID / endpoint 重用；同线程两个客户端重复 / 相反审批；断线后待批恢复；旧连接迟到结果；用户在电脑插话；手机重发 / request 超时；子 Agent 线程；长 rollout 增量 / 重写 / 部分行；最后观察者离开时无意停止工作。性能要求是列表与 rev 推送轻量化，不把全部 thread 的全文按秒重读。

实施顺序和复用点：

| 环节 | 建议落点 | 必须一起处理的边界 |
| --- | --- | --- |
| 多目录 / 多 server 发现 | 在 node 增加 Codex 专用 registry / discovery，借鉴 `claudedirs.go`；`hub/config.go`、`hub/claudedirs.go` 的配置下发 / settings 记忆模式 | server inventory 不受终端引擎选择影响；home 可以没有 endpoint；权限不够只标不可访问 |
| 连接与 thread 分离 | 从 `node/assistant_codex.go` 抽出 transport、RPC pending、per-thread state 和审批路由；自有助手继续走原入口 | 自有进程和外部进程的 ownership 不共用 stop / reap；未接入的外部 stdio 不能自动起替代进程 |
| 列表与控制 | `node/sessions.go` 增加 app-server 来源；`server/internal/node/control.go:307` 在找 pane 之前根据来源路由 | 不让 app-server 会话落进 Claude resume 兜底；没有 pane 时不显示终端相关动作 |
| 持久化与协议 | `proto/proto.go`、DB `node_sessions` / settings；hub HTTP/WS 与 Agent MCP 工具 | 同源 / 跨源身份、重启恢复、会话路由、客户端兼容；新增迁移前按车道检查号码 |
| 历史与实时 | 复用 `transcript/codex.go` 和 File 分页；必要时为协议 history 提供同等分页 / span | 原始 source/thread/turn/item ID 为合并键；重新订阅后丢弃无法证明完整的局部 delta，以快照补洞，不继续盲拼 |
| 客户端 | `apple/Shared/Models.swift`、ChatStore、会话菜单，Android/web 对应模型 | source、capabilities、清晰的只读原因；旧缓存版本迁移，新结果不能被自动通知覆盖 |

不要把 CLI 的伪终端、独立 process/exec 命令入口和 thread 的 Agent 命令当成同一类控制。`process/exec` 本身会在电脑执行命令，不是只读附加历史的方式；没有用户请求就不调用。

## 上游测试证据与仍待验证的部分

这些测试**只阅读了源码，没有执行**；它们说明上游哪些行为有回归约束，不能代替 Persona 接入后的实测。

| 测试 | main 位置 | 证明的范围 |
| --- | --- | --- |
| `websocket_transport_routes_per_connection_handshake_and_responses` | `codex-rs/app-server/tests/suite/v2/connection_handling_websocket.rs:73` | 两条连接使用相同请求 ID 仍分开握手和响应 |
| `control_socket_acceptor_upgrades_and_forwards_websocket_text_messages_and_pings` | `app-server-transport/src/transport/unix_socket_tests.rs:107` | Unix 上 WebSocket message/ping，而不是自定义 newline TCP |
| `thread_loaded_list_paginates` | `app-server/tests/suite/v2/thread_loaded_list.rs:43` | 一个 server 多 loaded thread 的分页 |
| `thread_resume_rejects_legacy_writer_owned_by_another_process` / paginated 对应项 | `app-server/tests/suite/v2/thread_resume.rs:321,326` | 双进程共 home 的 writer 排斥，不能 cold-resume 接管活跃线程 |
| `thread_resume_replays_pending_command_execution_request_approval` / file-change 对应项 | `thread_resume.rs:5833,5970` | reconnect/resume 可重放待批请求、保留原 ID |
| `thread_resume_keeps_in_flight_turn_streaming` | `thread_resume.rs:5128` | warm rejoin 时原轮次继续流式运行 |
| `cold_thread_resume_dispatches_a_persisted_queued_submission` | `app-server/tests/suite/v2/thread_queue.rs:430` | resume 可能启动既有工作；read/list 本身不加载 thread |
| `queue_start_while_active_returns_busy_and_preserves_the_queue` | `thread_queue.rs:701` | busy 时不能误消费持久队列 |
| `verification_cancels_when_its_owner_disconnects` | `app-server/src/user_verification_ownership_tests.rs:84` | 用户验证 owner 与普通审批重放不能混用 |

未证明：双订阅者同时发相反决定的完整 Persona UI 竞态；任意旧事件精确重放；跨进程队列的崩溃 exactly-once；外部 rollout / 网络文件系统的 writer 锁；当前桌面前端是否支持改用共享 endpoint。这些必须用独立测试实例验证，不能在用户活跃会话上试探。

## 研究阶段验证状态

- 已直接 `git clone` 到 `repos/codex`；`git check-ignore repos/codex` 通过，上游工作区保持干净。
- 已核对 main 实现和上游回归源码，并抽查当前桌面 tag 的 transport、writer 锁、warm resume、持久队列和审批重放；没有宣称跑过上游测试。
- 已核对 Persona 现有客户端 / 目录 / 路由源码，研究不是完整实现，尚未新增运行中的 server 接入。
- 未编译或运行上游源码、未进行真实多客户端写入测试，未部署、未发布 TestFlight。

## 第一版实现（2026-11-02）

入口在 `node/codex_registry.go`、`codex_discovery.go`、`codex_remote.go`；复用 `assistant_codex.go` 的 RPC envelope/pending，通过可插拔消息写入和分发接外部 WebSocket，不让外部实例进入助手进程的 stop/reap。

- 自动发现多个 home、当前用户的运行进程/允许字段和已存在的 listen/control socket；配置项见 `docs/config.md`。只有选中进程的已打开 rollout 文件名会用于标记 stdio 来源仍在运行，不能根据最近文件或 cwd 猜 owner。无法证明工作状态时显示 unknown，而不是误报已退出或一直 working。
- 同 home/thread 的终端、历史、协议入口用统一 resolver；默认 legacy home 保留旧 ID，非默认生成 `c0de-` opaque ID。PID、连接代际不改变历史身份。已知 home/命名实例/逐 thread 加入许可存在 hub settings，默认扫描和历史展示不依赖终端 engine。
- 每实例一条 Unix-WebSocket 或 literal loopback WS 连接，一连接多 thread；完整分页 loaded/list 建 owner。共享 home 同 thread 的多 owner 拒写。initialize 返回 home 不一致则断开，初始化不覆盖桌面技能/模型/权限。
- 普通查看同步本地 rollout，用户「实时接入」或首次发送才请求加入；不对发现的所有 thread 批量 resume，不自动冷恢复，不另起替代进程。发送支持文本和已上传的本地图片；工作中 steer 带 observed turn ID，任意失败不改走 start。停止不清后台终端。
- 外部提问/命令/文件/MCP 工具及权限审批使用原手机卡片，但没有助手的自动批准策略。请求 ID 包含来源和代际，回放去重；外部 resolved 撤卡记 dismissed。未知动态工具/表单/用户验证不自动回复，也不假装支持桌面 handler。
- Apple/Android/web 解析可选来源能力，显示只读原因、隐藏无能力的终端/恢复/关闭；发出和重试再次核验能力。旧客户端即使仍显示按钮，服务端也拒绝只读和错误入口。

仍有明确限制：父 Agent 管理的子 thread 不直接发 turn 输入；queue 管理、动态工具执行、音频/skill/mention 的专用输入未开放；没有本地 rollout 的 thread 只保证可见元数据与已加入后的新实时输出，不承诺完整历史分页。Unix 长路径 rendezvous、认证 WS/远程入口尚未适配，不能靠放宽权限或扩张账号解决。`loaded/list` 与 resume 不是原子操作，加入本身不是严格零副作用订阅；没有证明的竞态与上游 exactly-once 仍保持前文限制。

合成回归覆盖多 home 同号、多 owner、loaded 分页、Unix handshake、旧 ID 保持、重启映射、只读零控制、steer 失败不重发、unknown delivery、审批回放/外部收尾零回复、多 thread delta 及 payload 包装。`JARVIS_CODEX_NODE_BINARY=<已构建 jarvisd> go test ./internal/node -run CodexRemoteNodeProcess` 还会实际启动独立 HOME 的 node，通过其 stdio RPC 驱动测试 WebSocket，不碰用户会话。

上游源码仍只读；未运行它的 Rust 测试，也未在用户桌面线程试发/审批。Apple/Android 平台不可用时，构建和深浅色截图必须报告未运行，不用 Go/web 测试代替。

### 本轮验收记录

服务端全量测试连真实测试 Postgres、Codex/发送相关 race 多次回归、独立 HOME 的实际 node 进程协议驱动，以及 web 16 项测试和 build 均已验证。新未知事件回归覆盖旧 parser overlay，旧实现确实失败；只用合成数据，不改共享工作区。

Apple 使用独立源码目录/模拟器尝试 xcodebuild test，退出 70：虽然 simctl 有 iOS 26.0、SDK 列出 26.2，Xcode 仍报 iOS 26.2 平台未安装、目标不可用；实际 Swift 测试执行 0，未取得 iOS/watchOS 深浅色截图。Android 无现成 SDK/Gradle，未编译或运行测试；不安装平台、不改签名、不发布 TestFlight。

真实语料验证尝试从用户的独立 corpus 目录挑 Codex 快照，但没有符合条件的文件，因此 corpus window/continuity 本轮未运行，不能把跳过记成通过。长历史依赖既有 fixture/window 回归；UI 与真实双客户端审批竞态等限制仍保留。
