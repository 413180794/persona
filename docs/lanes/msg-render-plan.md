# 消息读取与渲染链路：诊断与改造方案

车道：`lane/msg-render`（从 master cd82927 切出）。2026-10-29 调研，对照 Ovis（herdr 插件，`shared/protocol.ts`、`server/conversation.ts`、
`server/claude-transcript.ts`、`server/codex.ts`、`src/` 的 ChatView）。

标记：**【确认】** = 顺着代码路径走通，或用本机真实 transcript 验证过；**【怀疑】** = 机制成立，需要复现。
「本机样本」指这台开发机 `~/.claude/projects`（208 个 transcript、约 6 万行）和 `~/.codex/sessions`（2 个 rollout）的统计，
统计脚本见文末，样本不入库（入库前脱敏，见阶段 0）。

---

## 1. 现状：端到端链路

```
各台电脑（jarvisd node）                         hub（jarvisd hub）                      iPhone / Watch
──────────────────────────                     ─────────────────                      ──────────────
~/.claude/projects/*/<id>.jsonl ─┐
~/.codex/sessions/**/rollout-*.jsonl ┤ transcript.File.Refresh()        RPC session.items     HTTP GET /sessions/{m}/{s}/items
~/.trae/cli/sessions/...  ───────┘   按字节 offset 追加读整行            (Page/PageTurns/After) ──▶ (turns=2 / before_seq / after_seq)
                                     ParseLine → []proto.Item
                                     （seq = 行号，id = uuid#块序号）
                                     全部 item 常驻内存 n.files
watchLoop: 每 250ms Refresh ───────▶ event session.items ──────▶ itemsToSubscribers ──▶ WS {"type":"items"}
           每 400ms 读终端   ───────▶ event session.screen ─────▶ toSubscribers ──────▶ WS {"type":"screen"}（App 不用）
pollAgents: 每 1s herdr snapshot ──▶ sessions.changed / revs ──▶ broadcast ──────────▶ WS sessions / revs → AgentSync 拉 after_seq
hooks（PreToolUse / PermissionRequest）▶ interaction ─────────▶ 卡片 / 推送
```

- **定位**：herdr claude 集成给 session id → hook 绑定的 pane → `~/.claude/sessions/<pid>.json` → 同目录最新 transcript 兜底
  （`node/sessions.go` `setAgents` / `sessionFromProcess` / `guessSession`）。Codex 用 herdr 的 id 或同目录最新 rollout（`node/codex.go`）。
- **解析**：`server/internal/transcript/transcript.go`（Claude）、`codex.go`（Codex / TRAEX）。一行 JSONL → 0..n 个 `proto.Item`，只追加不修改。
- **分页**：`File.PageTurns`（App 用，按「一问一答」翻）、`File.Page`、`File.After`（增量 / 补发，上限 500）。
- **推送**：hub 第一个订阅者到来时让 node `session.watch(since=hub 记的 lastSeq)`；每个订阅另起 `sendBacklog(since=App 的 lastSeq)`。
- **App**：`ChatStore.merge` 按 id 去重、只追加；磁盘缓存只存最后两轮（`ChatCache`）；倒置 ScrollView + LazyVStack 渲染；
  `ChatBlock.build` 把 item 组成「轮」（工作行 + 答案）。

和 Ovis 的根本差异：Ovis 每次**整页重读**最新一页（turn/part，没有 id），WS 只推「变了」的信号，靠 ETag/304 省流量，
靠 `history_id`（文件 dev-ino + 改写代数 + clear/fork 后缀）发现历史变化后整体丢弃旧页；Jarvis 是**按 seq 增量流式追加**，
没有历史身份、没有连续性校验，任何一次漏发都是永久的。

---

## 2. 问题清单

### A. 增量更新：丢消息 / 断档（最严重）

| # | 问题 | 证据 | 复现 |
| --- | --- | --- | --- |
| A1 | **【确认】WS 补发超过 500 条时静默截断，留下永久断档**。node `After(since, 500)` 返回 `more_after`，hub `sendBacklog` 解码的结构体没有这个字段，直接丢了；App 照常合并，`lastSeq` 前进，之后只收增量；已订阅的会话 AgentSync 也跳过（`AgentSync.swift:71-72`） | `server/internal/hub/http.go:595-621`；`node/node.go:285-289`；node 自己的 catch-up 同样只发 500 条且无标记（`node/control.go:425-428`、`403-405`） | 打开对话页→App 切后台→让 Codex 跑 250+ 次工具调用（每次 2 条）→回前台：中间缺一大段，往上翻也翻不到 |
| A2 | **【确认】`File.After` 会把同一行的多个 item 切开**。`Page`/`PageTurns` 有「不切开同一行」的处理，`After` 没有；Codex 每个工具调用 = 同一 seq 的 use + result 两条，上限恰好落在中间时 result 丢失、工具永远显示「运行中」（下一批从 `last.Seq` 之后开始） | `transcript/transcript.go:927-941`（对比 `869-889`） | A1 同场景；单测可直接构造（阶段 0） |
| A3 | **【确认】冷启动时 HTTP 首页和磁盘缓存之间有断档**。WS 还没收到 hello 时 `subscribe` 走 `loadInitial`（只拿最新两轮），和缓存里的旧两轮合并；HTTP 先回来的话 `didConnect` 用新的 `lastSeq`，中间的轮永远不补；`loadOlder` 从缓存里最早一条往前翻，也跳过断档 | `ios/Jarvis/App/AppModel.swift:187-194`、`205-213`、`215-226`、`230-241` | 杀掉 App→电脑上让 Agent 再跑 3 轮以上→点推送冷启动：「旧两轮 + 新两轮」，中间没了 |
| A4 | **【确认】transcript 被截短 / 替换后，服务端从第 1 行重新编号，App 不知道**。没有历史身份（Ovis 的 `history_id`），App 的 `after_seq` 比新文件行数大就什么都拿不到；`line<seq>#n` 这类兜底 id 还会和旧的撞上被去重吞掉。只判断了「变小」，同大小替换、变大的替换都检测不到 | `transcript.go:834-836`；`ChatStore.swift:278` | 少见（Claude Code 不改写；外部工具、`--fork-session`、手工编辑会触发）。单测构造 |
| A5 | **【确认】`watch(since=0)` 时 Refresh 解析出的新行不推送**：`if since > 0` 才补发，但 Refresh 已经把这批行消费掉了，之后 watchLoop 只推更新的。hub 重启后 `lastSeq` 为 0 时走到这里；App 侧的 `sendBacklog` 通常能补上，但是时序相关 | `node/control.go:424-428` | hub 重启瞬间 Agent 正在输出 |
| A6 | **【确认逻辑，高概率】同一会话开两个对话页时，订阅被后关的那页取消**。App 的 `subscribed` 是 Set、hub 每个客户端每个会话是 bool，都没有引用计数；新页面先 onAppear 订阅，旧页面后 onDisappear 取消，正在看的页不再实时更新，`visibleSession` 也被置空（会照常弹推送） | `AppModel.swift:43`、`187-203`；`ChatView.swift:181-186`；`hub/http.go:553-578` | 对话页→顶部同伴条进空间页→再点同一个 Agent；或在空间页里点这个会话的推送 |
| A7 | **【确认】助手 tab 中间回复显示两次**。`message_stop` 把整段文字作为 delta 发出，`message_start` 只清缓冲不发事件，只有 `result` 才发空串；一条消息是「文字 + tool_use」时 `rows.last` 是工具行，`ChatView` 的去重判断失效，工具运行期间这段话既是正式行又带光标再显示一遍 | `node/assistant.go:315-350`；`ChatView.swift:734-741` | 问助手「先告诉我你要做什么，再查日历」 |
| A8 | **【怀疑】客户端没有心跳**。前台切换网络时 `receive` 可能长时间挂着，`state` 一直 `.connected`，对话页静默停更；hub 侧慢消费者直接断开（`http.go:364-372`），也依赖 App 重连补发（受 A1 影响） | `ios/Shared/LiveConnection.swift:123-126`、`158-189` | 前台时 Wi-Fi ↔ 蜂窝切换 |
| A9 | **【确认】App 内重连后，已处理的交互卡片残留**：`didConnect` 不重拉交互列表，只有回前台的 `refresh` 才纠正 | `AppModel.swift:230-241`；`hub/http.go:521-523` | 前台断网→电脑上回答问题→恢复网络：卡片还在，点了 410 |
| A10 | **【确认，低】排序与缓存截尾的小坑**：同 seq 按 id 字符串排序，`#10` 排在 `#2` 前（`ChatStore.swift:295`）；`ChatCache.tail` 没有「不切开同一行」（`ChatStore.swift:607-614`）；待发送消息用双向 `contains` 匹配，「好」「继续」这类短消息会被任何包含它的消息消掉（`ChatStore.swift:637-641`） | 同左 | 单测 |

### B. 解析：漏掉 / 错认的消息

| # | 问题 | 证据 |
| --- | --- | --- |
| B1 | **【确认】`system/local_command` 整条丢弃**：新版 Claude Code 把斜杠命令（`/model` 等）和它的输出写成 `type:system, subtype:local_command`，内容是 `<command-name>…` / `<local-command-stdout>…`，`parseLine` 的 system 分支 default 返回 nil。Ovis 有专门处理（`claude-transcript.ts:20-50`、`317-322`） | `transcript.go:283-299`；本机样本 6 条（2.1.259） |
| B2 | **【确认】`api_error` 显示成没有内容的「API 错误：」**：错误在 `error.formatted` / `error.message`，不在 `content`；而且每次重试（`retryAttempt` 1..10）都写一条，会刷出一串 | `transcript.go:286-287`；本机样本 1 条 |
| B3 | **【确认，范围修正】同一条后台任务通知显示两次**：同一个 `<task-notification>` 会同时写成 user 记录和 `attachment(queued_command, commandMode=task-notification)`，每处都变成一条 notice。**但同一个 task-id 也会有多次内容不同的真通知**（子 Agent 先报「还在等后台任务」、再报「完成」，阶段 0 的 `claude-notifications.jsonl`），所以不能像 Ovis 那样按 task-id 去重，只合并内容完全相同的 | `transcript.go:242-245`、`321-327`、`353-357`；本机样本 95 个 task-id 出现多次（2026-10-29 Claude Code 自动清理了 30 天前的 transcript，原样本已无法逐条区分哪些是真重复） |
| B4 | **【确认】`system/scheduled_task_fire`（/loop 到点唤醒）不显示**：用户看不到这一轮为什么开始 | `transcript.go:298`；本机样本 6 条 |
| B5 | **【确认】Codex `Extension`（web.search 等）不显示**，FileChange 遍历 Go map 编号，**同一 rollout 两次解析 id ↔ 文件的对应关系不同**（node 重启后 id 指向别的文件） | `transcript/codex.go:260-272`；本机样本 Extension 20 条 |
| B6 | **【确认，低】`continued-in` 不跟随**：会话续写到另一个 session id 时，Ovis 读末尾 64 KiB 跟过去；Jarvis 停在旧文件 | 本机样本 1 条 |
| B7 | **【确认，低】/rewind、Esc 后重发产生的分叉两支都显示**。Ovis 沿 `parentUuid` 跟当前分支，被放弃的那段隐藏 | 本机样本 1 处 |
| B8 | **【确认，覆盖缺口】解析测试全是手写字符串，没有真实样本 fixture**；Claude Code 版本迭代很快（本机样本里已经有 `atis-latch`、`mode`、`api-request-blob`、`cost-state` 等新类型），没有回归保护 | `transcript/transcript_test.go`、`codex_test.go` |

已经做对的（不用改）：queued_command 吸收的消息、非空 thinking 显示、`isSidechain` 过滤 + subagent 单独文件、pasted_content 去包装、
`<bash-input>` / `<bash-stdout>`、跨会话消息、`[Request interrupted…]`、compact 提示、Codex commentary → 思考、TRAEX `history_mutation` 去重。

### C. 分页与大会话性能

| # | 问题 | 证据 |
| --- | --- | --- |
| C1 | **【确认】node 内存只增不减**：`n.files` 缓存每个打开过的会话的全部 item（含 ≤1.5 MB 的 base64 图片），从不回收；App 用 `lazy_images` 后图片只在 `session.image` 时用，却一直常驻 | `node/node.go:35`；`node/sessions.go:229-256`；`transcript.go:441-446` |
| C2 | **【确认】第一次打开大会话要读完并解析整个文件**（`io.ReadAll`，本机最大的 transcript 20 MB+、Codex rollout 21 MB），只为返回最后两轮 | `transcript.go:822-866` |
| C3 | **【确认】长工具输出整段（≤24 KB）随页下发**；一页两轮最多 2000 条，Ovis 是页内 4000 字符 + `output_ref` 按需取全文 | `transcript.go:20-22`；`node.go:291-293` |
| C4 | **【确认】订阅期间 node 每 400 ms 读一次终端（herdr 进程调用）推给 App，App 不读** | `node/control.go:470-487`；`AppModel.swift:289-293`（只写 `store.screen`，无视图读取） |
| C5 | **【确认】App 主线程重活**：每次事件整份重建 rows / blocks / lines；body 里 `store.items.compactMap(\.image)`、待发送消息过滤跑正则；`StreamingText` 每 80 ms 重新解析 Markdown；`CodeBlock` 展开后是一个巨大 Text；`chats` 字典不回收 | `ChatStore.swift:304-313`；`ChatView.swift:127-130`、`166`、`720`；`MessageViews.swift:586-593`；`ToolViews.swift:288-292` |

### D. 状态判断

状态来自 herdr（`agent_status`）+ hook 交互 + hub 的「看过」（`hub/state.go`），机制和 Ovis 一致（Ovis 也以 herdr 为主，另加 CompletionTracker）。
**提问 / 计划 / 授权走 hook 是 Jarvis 比 Ovis 强的地方**（Ovis 全靠屏幕识别 + 模拟按键），保留。没有发现确认的状态错误，记几个风险：

- D1【怀疑】`quickMeta` 只读尾部 384 KB 且跳过大于 8 KB 的 user 行（`node/sessions.go:157-176`）：一轮的最后一件事是大工具结果时
  `UpdatedAt` 比真实结束时间早，「看过」判断（`state.go:24`）会把「在它真正结束前看过」算成看过，该是「完成未看」的显示成「已看」。阶段 1 用 fixture 验证。
- D2 Codex 的提问只能走通用「终端等你处理」卡片；Ovis 的 dialog reader（`prompt.ts`、`dialogs/*.ts`）可以借鉴来做结构化卡片——**不在本车道范围**，记下。
- D3 对话页的 `liveTurnID`、转圈只看列表里的 `session.status`（每秒轮询、按 `listKey` 变化才推），和对话内容不同源，有一拍的延迟；可接受。

### E. 渲染稳定性

| # | 问题 | 证据 |
| --- | --- | --- |
| E1 | **【确认逻辑，怀疑界面】「冻结」锚点的行 id 会变**：工作中的轮收到新回复后，原来的答案行被收进工作行；轮结束时 `w-*` 变成 `mu-` / `wt-`；`firstIndex` 找不到锚点，冻结失效，正在往上看的内容被挤走 | `ChatView.swift:226-260`、`718`、`833` |
| E2 | **【确认】保存的阅读位置一直挂着**：保存的行不在缓存的两轮里时 `entry` 不清，之后手动往上翻加载到那一页，列表被强行拉过去，还关掉跟随 | `ChatView.swift:498-509`、`836` |
| E3 | **【怀疑】流式文字 / 待发送气泡不受冻结约束**，往上翻时也会挤动 | `ChatView.swift:728-741` |
| E4 | **【确认】手表对话页只在打开时拉一次**，不刷新、状态不变，原文里的 `<jarvis-context>` / `<attached_images>` 没去掉 | `ios/JarvisWatch/JarvisWatchApp.swift:370-373` |

---

## 3. 方向：保留增量流，补上「身份 + 连续性」

不照搬 Ovis 的「整页重读 + 位置 key」。原因：手机要后台同步、离线缓存、流量小，按 seq 增量正合适；Claude transcript 本身只追加，
item 一经发出不再变化，这个模型是对的。Ovis 值得借的是三件事：

1. **历史身份**（`history_id`）：文件身份（dev+ino）+ 改写代数，变了就让客户端整体丢弃重来，而不是带着错的 seq 继续。
2. **连续性可验证**：每批 item 声明「接在哪个 seq 之后」，客户端发现接不上就主动补，补不上就整页替换——任何漏发都能自愈，不再是永久断档。
3. **真实样本测试 + 增量读 = 冷读**：同一文件一次性读和逐行追加读得到完全相同的 item（Ovis `claude-incremental.test.ts`）。

---

## 4. 分阶段方案

每个阶段单独提交、单独可回滚；服务端改动保持对旧 App 兼容（新字段可选，旧字段语义不变）。

**进度**（2026-10-29）：阶段 0 已合入（PR #5），阶段 1 已合入（PR #6），阶段 2 已提交在 lane 分支（未合入 master）。阶段 2 和方案的出入：
字段名用 `from_seq` / `to_seq`（`to_seq` 包含没有 item 的行，空批次也推），协议见 `docs/protocol.md`「items 的连续性」；
订阅引用计数只做在 App（hub 每个连接本来就按会话去重）；心跳靠 hub 的 websocket 库自动回 pong。

阶段 3 已提交在 lane 分支（未合入 master），实测（`go test ./internal/transcript -run ColdOpenCost -v -args -cost`，20.8 MB 带截图的 transcript）：
常驻内存 18.7 MB → 0.5 MB（item 不再常驻图片数据，按行偏移重读）；node 只留最近用过的 16 个会话（watch 中的不回收）；
工具输出页内截到 4 KB，末尾一行「…（已截断，点开看全文，共 N 字节）」+ `full_size`，App 点「已截断，看全文」取 `/items/{id}/full`；
App 的 rows / blocks 只重建最后一轮（1000 轮会话每次事件 21 ms → 0.4 ms，模拟器 Debug），对话页不再因每条 item 重算图片列表，内存里只留 8 个不在屏幕上的会话。
**和方案的出入**：冷打开窗口化解析（C2）没做——20.8 MB 首页 236 ms，时间花在逐行 JSON 解析（大头是截图的 base64），
而会话级 Meta（轮数、标题、后台任务、队列）本来就要整份过一遍；窗口化最多省到几十毫秒，代价是 Meta 不完整、分页逻辑复杂，
加上 LRU 后每个会话只在冷打开时付一次。`StreamingText` 按段落缓存 Markdown 解析也没做（只有助手流式时有，单段文字很短）。

真实数据验证（435 个文件、5.8 GB）的数字表和逐类差异说明见 `docs/lanes/msg-render-verification.md`。

阶段 4 已提交在 lane 分支：
- E1：冻结改成「离开最新端时的 item 快照」（seq + 块序号 + 当时在跑的轮），往上翻时对话按快照里的 item 画，之后来的等回到最新端再出现；
  更早的页和补上的断档照常出现。`-showFreezeProbe`（`MOCK_STREAM=1`）实测：读历史时来了 12 条，屏幕中间那行修前移动 319pt，修后 0pt。
- E2：保存的阅读位置在第一页（hub 的最新一页）里找不到就放弃，用户一滚动也放弃。
- E3：冻结时流式文字、「正在思考」、待发送气泡都不插进最新端（自己发消息会先回到最新端，不受影响）。
- E4：手表对话页跟着会话的 rev 重新拉（每 8 秒刷新的会话列表带 rev），状态跟着刷新，`<jarvis-context>` 显示成〔标题〕、附图说明和 `[Image #n]` 显示成〔图片〕。
  **没截手表图**：这台 Mac 没装 watchOS 模拟器运行时（几 GB，没擅自下载），只确认手表 target 编译通过、`WatchText` 有单测。

### 阶段 0：测试基建（先做）

- **改什么**
  - `scripts/redact_transcript.py`：把真实 transcript / rollout 脱敏成 fixture——保留结构、类型、uuid 关系、行序，文字替换成等长占位、
    路径 / 主机名 / 邮箱换成占位、base64 图片换成 1×1 PNG，并打印剩余可疑串供人工复核。
  - `server/internal/transcript/testdata/`：Claude（含 local_command、api_error 重试、task-notification 三处、queued_command、compact、
    scheduled_task_fire、tool_result 带图片、subagent）、Codex 0.15x/0.16x（含 Extension、FileChange 多文件、turn_aborted）、TRAEX 各一份。
  - golden 测试：fixture → item 列表的 JSON 快照（`-update` 刷新）；**增量 = 冷读**测试（逐字节 / 逐行追加，结果一致）；
    `After` / `Page` / `PageTurns` 分页无缝隙无重叠的性质测试。
  - 先把 A2、A5、B1–B5 写成失败的测试。
- **验证**：`./scripts/test-server.sh ./internal/transcript/...`（Go 只在 hub 机器上）。
- **风险**：脱敏不彻底把个人信息带进仓库——脚本白名单式替换（只留 JSON 键和枚举值），提交前人工 grep 一遍；fixture 控制在每份 < 200 KB。

### 阶段 1：服务端解析与分页正确性（不改协议）

- **改什么**
  - `After` 不切开同一行；补发 / 增量统一返回 `more_after`（A2）。node watch 的 catch-up 循环发完（A1 的 node 半边），`since=0` 时也推 Refresh 出来的新行（A5）。
  - 解析：`local_command`（命令名作为用户行、stdout 作为提示，去终端控制序列）（B1）；`api_error` 取 `error.formatted`，
    同一请求的重试合并成一条「API 错误（重试 n 次）」（B2）；task-notification 只合并内容完全相同的（B3）；`scheduled_task_fire` 显示「/loop 到点唤醒」（B4）；
    Codex Extension `web.search` 显示成搜索工具、FileChange 按路径排序编号（B5）。
  - 用 fixture 核实 D1，必要时 quickMeta 的活动时间改为不跳过大行、只跳过解析内容。
- **验证**：阶段 0 的失败测试全部转绿；golden 快照 diff 逐条人工看；部署到本机 node，用 `-openSession` 打开含这些类型的会话，模拟器截图（深 / 浅）。
- **风险**：解析变化让已有 item 的 id / seq 变化 → App 缓存里的旧 item 和新 item 并存重复。约束：**已有 item 的 id 规则不动**，只新增 item；
  去重（B3）只会少发，App 缓存里已有的重复项留着（清缓存后消失），可接受。

### 阶段 2：协议层的身份与连续性（服务端 + App，断档自愈）

- **改什么**
  - 服务端：每个会话一个 `history`（transcript 的 dev:ino + 改写代数；检测「变小 / 同大小 mtime 变 / 文件被替换」时代数 +1，从头重读）（A4）。
    `items` 事件、HTTP 分页响应都带 `history` 和 `after_seq`（这批接在哪个 seq 之后；补发、增量、实时推送都有）。
    hub `sendBacklog` 透传 `more_after`（A1）。
  - App `ChatStore`：记录 `history` 和连续区间。收到一批：`history` 不同 → 清空重来（缓存一起清）；`after_seq` > 本地 `lastSeq` → 有断档，
    先放进待合并区，用 HTTP `after_seq` 循环补齐（`more_after` 时继续），补不上（太多）就替换成最新一页并保留 `hasMore`。
    `loadInitial` 的结果和缓存不连续时同样处理（A3）。排序改成 `(seq, 块序号)` 数字比较（A10）。
  - 订阅改成引用计数（App 每个 key 计数，hub 每个客户端也计数）（A6）。
  - 心跳：App 每 20 s 发 ping，hub 回 pong；两次没回就断开重连（A8）。`didConnect` 时重拉交互列表（A9）。
  - 助手流式：`message_start` 时发空 delta；App 收到包含 liveText 的 item 后清掉 liveText（A7）。
  - 只在有人要的时候读终端：对话页不用 `screen`，watch 默认不采样，需要的地方（终端页）显式订阅（C4）。
- **验证**
  - 服务端：分页 / 补发 / 改写代数的单测（含「补发 > 500」「截短后重写」「同大小替换」）。
  - App：`ChatStoreTests` 加断档、乱序、history 变化、重复批次、冷启动缓存 + 最新页的用例。
  - 端到端：`scripts/mock_hub.py` 加「故意漏发一批 / 只发前 500 条 / 换 history」开关，模拟器跑一遍，对话完整；
    再在真实 hub 上走 A1、A3、A6 的复现步骤，截图对比修前修后。
- **风险**
  - 旧 App + 新 hub：新字段旧 App 不认，行为不变；新 App + 旧 hub：没有 `history` / `after_seq` 时退回现在的逻辑，不能当成断档狂补。
  - 断档补齐的循环要有上限和退避，防止对离线机器反复请求。
  - **和另一个车道冲突面最大**（`server/internal/hub`、`node`）：本阶段开始前先 rebase 到最新 master，改动集中在 `transcript/`、
    `hub/http.go` 的 sendBacklog / 订阅、`node/control.go` 的 watch、`node/node.go` 的 session.items，其余文件不碰。

### 阶段 3：大会话性能

- **改什么**
  - node：`File` 记每行的字节偏移，item 里不常驻图片数据，`session.image` 时按偏移重读那一行（C1）；不在 watch 的 `File` LRU 回收（保留 16 个）。
  - 冷打开从文件尾部倒着找轮次起点，只解析最后一个窗口（参考 Ovis 16 MB 窗口），往前翻再按需解析更早的窗口；增量读结果必须和冷读一致（阶段 0 的测试兜底）（C2）。
  - 长工具输出：页内截到 4 KB，带 `full_size`；新增 `GET /api/v1/sessions/{m}/{s}/items/{id}/full`，App 展开时再取（C3）。
  - App：rows / blocks 只重建最后一轮（之前的轮不变）；`compactMap(\.image)`、待发送消息过滤挪出 body；`StreamingText` 只在段落结束时重新解析 Markdown；
    `CodeBlock` 超长时分段懒加载；`chats` 字典回收不在屏幕上的会话（C5）。
- **验证**：node 端对 20 MB transcript 计时 + 内存（修前 / 修后）；App 用 Instruments Time Profiler 在 1000+ 轮的会话里滚动、流式输出，主线程 hitch 对比；
  `-showScrollToOldest` 一路翻到最早一条不卡、不白屏。
- **风险**：窗口化解析让「从中间开始解析」的 Meta（标题、后台任务、队列）不完整——Meta 仍由 quickMeta / bgScan 单独算，不依赖窗口；
  截断长输出后 App 旧版本看到的是截断文本（加明显的「已截断，点开看全文」提示，旧 App 也能读懂）。

### 阶段 4：渲染稳定性

- **改什么**：冻结锚点改用「这一轮的用户消息 id + 偏移」这种不会变的 id（E1）；阅读位置恢复只在打开后的第一次布局里尝试，用户一滚动就作废（E2）；
  流式文字 / 待发送气泡在冻结时不插入可见区域（E3）；手表对话页订阅 rev 刷新、状态跟随、去掉内部标签（E4）。
- **验证**：模拟器截图（深 / 浅、iPhone 和手表）；脚本批量冷启动打开会话按像素标准差判断白屏（CLAUDE.md 里的方法）；
  Agent 工作中往上翻，录屏确认可见内容不动。
- **风险**：倒置列表的滚动逻辑历史上坑最多（见 CLAUDE.md「经验」），每改一处都要按那些条目回归一遍。

### 阶段 5（可选，看前面效果再定）：会话身份跟随

- `continued-in` 跟到新文件（B6）；窗格里 `/clear` 换了会话时对话页给一条「已切到新会话」并可一键跳过去；/rewind 分叉只显示当前分支（B7）。
- 风险：要改会话列表的身份模型（同一窗格多个 session id），和另一车道的 node 改动重叠，等那边合入后再评估。

---

## 5. 需要用户拍板的点

**已定**（2026-10-29）：1 同意；2 同意，截断处要有旧 App 也看得懂的「已截断，点开看全文」；3 本车道不做，以后再评估。

1. 是否同意「保留增量流 + 补身份和连续性」，而不是改成 Ovis 那种整页重读的 turn/part 模型（后者等于重写协议和 App 渲染层）。
2. 阶段 3 的长工具输出截到 4 KB + 按需取全文：会改变旧版 App 看到的内容（只看到截断）。
3. 阶段 5 是否纳入本车道。

## 附：本机样本统计方法

一次性的 Python 脚本（不入库）：遍历 `~/.claude/projects/*/*.jsonl` 统计 `type/subtype`、content 块类型、attachment 类型、
同一 `<task-id>` 在可见记录里出现的次数、`parentUuid` 重挂的 prompt；遍历 `~/.codex/sessions/**/*.jsonl` 统计 `type/payload.type/item.type`。
阶段 0 会把它整理成 `scripts/transcript_stats.py`，以后 Claude Code / Codex 升级时跑一遍看有没有新类型。
