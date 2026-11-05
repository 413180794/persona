# Agent 状态与空间模型

## 统一状态

App、手表、推送都只认这一套状态（`proto.Session.state`，hub 计算）。来源是宿主（目前是 herdr）的状态，加上 Persona 自己知道的两件事：
有没有待处理的交互、用户有没有在 App 里看过。

| 状态 | 含义 | 来自 herdr | Persona 补充 | 颜色 |
| --- | --- | --- | --- | --- |
| `needs_you` 需要你 | 卡在需要人参与的地方 | `blocked`（TUI 里的提问 / 确认框） | 有待处理的提问、计划、授权、终端交互（hook 拦下来时 herdr 还显示 working） | 红，呼吸 |
| `working` 工作中 | 正在跑 | `working` | Claude：herdr 报 `done` / `idle` / `unknown`，但会话有 hook（Persona 的，或全局只报告的）时 hook 的轮次计数说这一轮没结束（见下「hook 的轮次信号」）；没有 hook 的会话是终端标题是 Claude Code 工作中的转圈字形（◐◓◑◒，旧版是盲文点阵），或 transcript 里这一轮已经开始、还没结束（没有 `turn_duration`、没被中断）且 45 秒内写过（#2069；transcript 里这一轮已经结束时照 herdr 的）；或这一轮停了、但它的后台命令 / 子 Agent 还在跑（#2228：跑完会把结果交给它、它接着做；不含助手对话） | 琥珀，呼吸 |
| `unread` 完成未看 | 一轮做完，结果还没看 | `done`（herdr 没被聚焦过） | 且 App 里在它完成之后没看过 | 绿 |
| `idle` 已看 | 做完且看过，等输入 | `idle`（herdr 里看过） | 或 herdr 还是 `done` 但 App 里看过 | 灰 |
| `offline` 未运行 | 不在宿主里，只有 transcript，发消息会 `--resume` | — | — | 浅灰 |
| `interrupted` 被重启中断 | 重启前在跑，重启把 herdr 的窗格一起带走了（#1917） | — | 记录里有窗格、启动时刻早于本次 node 启动，且现在没有活着的 agent | 浅灰，带「继续」 |
| `unknown` 未知 | 宿主认出了 agent 但判断不了状态 | `unknown` | — | 灰 |

排序优先级：需要你 → 工作中 → 完成未看 → 已看 → 未知 → 未运行。

### hook 的轮次信号

herdr 的状态常常不对（`done` 还在干活，#2069），标题字形和 transcript 的修改时间只是推断。Persona 启动的 Claude 会话带 Persona 的 hook，用户自己起的会话在打开 `claude_global_hooks` 后由全局只报告的 hook（`claude-integration.md`「全局只报告的 hooks」）
报同样的 `UserPromptSubmit` / `Stop`，两路进同一套计数：它们直接说一轮什么时候开始、什么时候结束，node 据此判断（`server/internal/node/hookturn.go` 的 `judgeHookTurn`，`claudeStatus` 调用）。
借鉴 moshi-hook 的做法，每个会话在 node 内存里记：

| 字段 | 来源 | 用途 |
| --- | --- | --- |
| `prompts` | 每个 `UserPromptSubmit` 加一 | 开了几轮 |
| `completed` | `Stop` 时置为当时的 `prompts` | 答完了几轮 |
| 进程 pid + 启动时刻 | hook 进程往上找到 claude（`~/.claude/sessions/<pid>.json` 的 `sessionId` 对得上的那个祖先），Linux 取 `/proc/<pid>/stat` 第 22 项，存成 `linux:<starttime>`；macOS 取不到启动时刻，只存 pid | 分清活着的 claude 和被复用的 pid |
| 回复签名 | `Stop` 的 `last_assistant_message` 去空白后的 SHA-256 前 8 字节 | hub 给「完成」推送去重 |

两路来源的区别只在两点，别的都一样：Persona 启动的会话（含助手）只算插件 hook 的那一份，全局那一份在计数**之前**就丢掉，否则一次提交数两次；
全局的事件带 `report_only`，hub 不据此认为会话有 Persona 的 hook（否则屏幕卡片会为一张不来的 hook 卡片白等）。

判断（herdr 说 `done` / `idle` / `unknown` 时；`blocked`、`working` 照 herdr）：

- **终端上开着对话框**（#2553，`node/dialogstatus.go`）：上面的判断之后仍是 `done` / `idle` / `unknown` 的 Claude（不含 botmux、Claude 的 agents 列表里的、Persona 自己在操作选择器的），
  herdr 窗格里的，node 每 5 秒在后台读一次它的屏幕（列表不等读屏），底部是对话框（`screen.Choosing`）就记为 `blocked`，`attention_reason` 是对话框第一行，App 列表和助手 `agents_list` 的「最后说」显示这一行。
  起因：Claude Code 2.1.309 的 auto mode 计费通知挡住了 3 个 Agent 好几分钟，herdr 报 done，Persona 显示成完成未看。

- **有 hook 且 `prompts > completed`**：工作中，覆盖 herdr 的 `done`。
- **有 hook 且 `prompts == completed` 并且见过 `Stop`**：没在工作，终端标题里残留的转圈字形也不算。例外：transcript 里有一轮在 `Stop` 之后（加 5 秒宽限，让收尾的几行写完）才写的、还没结束的，
  是 hook 没通知的新一轮（后台任务或 `/loop` 唤醒 Claude、Stop hook 又让它接着干），这一轮按没有 hook 的规则判断。
- **什么都没数到**（node 刚重启、只收到 `SessionStart`）：hook 没有发言，照没有 hook 的会话判断。计数在内存里，重启后第一个 `UserPromptSubmit` / `Stop` 起重新有效；这是安全的方向，
  所以不进 Postgres、没有迁移。
- **agent 进程不在了或启动时刻对不上**（pid 被别的程序复用）：丢掉这个会话的 hook 状态，一个死进程留下的「没结束的一轮」不再算工作中，改走没有 hook 的规则。
  没直接判成未运行，因为同一个会话可能已经被手动 `claude --resume` 在同一个窗格里接着跑（那时没有 hook）；窗格里真没有 agent 时，herdr 本来就不列它。

**中断（Esc）不触发 `Stop`**，所以 `prompts > completed` 会一直不平。按这个顺序结束这一轮：

1. transcript 末尾在这次 prompt **之后**写了结束标记（`[Request interrupted by user]` 或 `turn_duration`）：结束。
2. 标题没有转圈字形，且 transcript 自 prompt 或最后一次写入起安静了 8 秒，标题是 Claude 的等待标记 ✳：结束（✳ 是 Claude 自己画的「在等输入」，证据足）。
3. 同上但标题认不出来（旧版本、用户自己改了标题）：安静 90 秒（`2 × claudeFresh`）才结束，因为长工具调用可以很久不写 transcript。
4. 标题有转圈字形：一直算工作中，长时间没输出的工具调用不会被误判。

有 `UserPromptSubmit` 却没有 `Stop` 的情况（本地命令是否如此没有实测）走同一条路。

**完成推送去重**：推送由 hub 在状态 working → done 时发出，状态抖动（working → done → working → done）可能给同一轮发两次。node 把 `Stop` 的回复签名放进 `hook.event`（`stop_sig`），
hub 在发「完成」推送前比较：同一个会话、同一个签名、中间没有新的 `UserPromptSubmit`，就不发第二次（`Machine.stopDuplicate`）。
全局只报告的 `Stop` 也带签名，照样去重。没有签名（没有 hook、`Stop` 没带回复）的不拦；新的 prompt 清掉记录，所以两轮碰巧回复一样（「好的」）也都会推。hub 重启后记录丢失，最坏多推一次。

**同一套防 pid 复用的做法也用在「别处已打开」的判断上**（`openelsewhere.go`）：Claude Code 自己写的 `sessions/<pid>.json` 带 `procStart`（Linux 上就是 `/proc/<pid>/stat` 第 22 项，实测一致），
留着的旧文件对应的 pid 被别的程序占用时，以前被当成「这个会话在别处开着」而拒绝从手机恢复；现在启动时刻对不上就算那个 claude 已经退出。

未验证 / 已知局限：Claude Code 的后台任务唤醒、`/loop` 到点是否触发 `UserPromptSubmit` 没有实测（按触发和不触发两种情况都能判对，见上面的例外）；
macOS 上 pid 复用的防护只有「进程是否还活着」；Windows 上找不到祖先进程，不记 pid，其余信号照常。
Claude Code 自己写的 `sessions/<pid>.json` 还有 `status`（busy / idle）和 `statusUpdatedAt`，今后可以当作另一路信号（本次没用）。

**工作态**（2026-11-04，#1686，[agent-work.md](agent-work.md)）：上面是宿主视角，「完成未看」只说明这一轮停了。hub 在一轮停下后分诊，
给会话加上工作态 `work`：工作中 / 卡在具名的前置条件上（等你决定、等另一个 Agent、等外部事件、瞬时错误）/ 做完了，带一行「在等什么」或结论。
算状态时多看一眼它：**做完了**、**卡在 hub 会替它等的事上**（hub 自动重试、到点叫醒）的不再是 `unread`，是 `idle`；
等你决定的 hub 开一张提问卡片，于是是 `needs_you`。没分诊出来（刚停、模型失败、看不出来）的照旧是 `unread`。
颜色只有一份：`apple/Shared/StatusPalette.swift`，App、手表和实时活动共用（`docs/ios.md`「设计语言」）。
Agents 列表另有「待唤醒」：状态是已看，但 `/loop` 还没到点（`Session.isWaiting`），之后会有新消息。后台命令 / 子 Agent 还在跑的，hub 直接算工作中（#2228，`backgroundState`），不再进待唤醒；助手对话的后台任务和 Monitor 常驻，不算。
后台起的常驻进程（如开发服务器）会让 Agent 一直是工作中，直到它退出（node 不再列出超过 24 小时的后台任务）。「完成」推送、hub 自更新等一轮结束的判断看宿主状态，不受这一条影响。
列表的排序（三种分组都一样）：需要你 → 完成未看 → 工作中 → 待唤醒 → 已屏蔽 → 已看，同状态按最后更新时间（`docs/ios.md`）。

### 「看过」

- herdr 的看过 = 在电脑上聚焦那个 tab（CLI 读取不算）。Persona **不会**为了标已看去聚焦 tab（会抢电脑上的焦点），所以手机上看过之后，
  电脑上 herdr 侧边栏的绿点仍在。
- App 的看过：打开对话页、在对话页里看着它完成、在列表里左滑「标为已看」、「全部标为已读」、手表打开会话。App 发 WS `{"type":"seen"}`
  （连接卡住时、或者 WS 发出 5 秒后 hub 还没记下时，改发 `POST /api/v1/sessions/{mid}/{sid}/seen`），hub 存进 Postgres `session_views`。
  WS 消息没有回执，连接半开时会悄悄丢掉（#1588），所以 App 把还没被 hub 记下的已读放在 `SeenOutbox`：hub 发来的列表里这个会话的
  `seen_at` 比标记时晚，才算记下；这期间它照旧显示已看，重新连上时再发一次；它又干了一轮、开始工作或需要你、从列表里消失、
  或者 10 分钟还没记下，就不再等。
- 判定：herdr `done` 且 `seen_at` 早于会话最后更新时间（transcript 的修改时间，即这一轮结束的时间）→ `unread`，否则 `idle`。
  所以看过之后它又干了一轮，会重新变成完成未看。

### 排序

- 列表按状态分组，组内新的在前。一般用 `updated_at`，但工作中的 agent 每走一步 `updated_at` 都会变，组内会跳来跳去，
  所以「工作中」用 `state_since`：hub 记下每个 agent 进入当前状态的时间（内存里，hub 重启后从重启那一刻算）。

## 空间 / 槽位（多 tab 协作）

一个宿主里「一组相关的 agent」抽象为**空间（space）**，组里的每个位置是**槽位（slot）**：

| 字段 | herdr 映射 | 以后接入别的工具 |
| --- | --- | --- |
| `space_kind` | `herdr` | 工具名 |
| `space_id` / `space_name` / `space_index` | workspace id / label / number | 项目、房间、会话组 |
| `slot_id` / `slot_name` / `slot_index` | tab id / label / number | 角色、窗口、频道 |
| `pane_index`（可选，#2320） | 窗格在 tab 里按位置的序号（从 0 起，左上为 0） | 槽位里的第几个位置 |
| `parent_space_id`（可选，#2073） | 不填 | 这个空间挂在哪个空间下面（同机器、同 `space_kind`）：Orca 的子工作区填父工作区的 `worktreeId`（`worktree.ps` 的 `parentWorktreeId`） |

DSH（`docs/dsh.md`）：`space_kind` 是 `dsh`，空间是会话的目录（`space_id` = 目录、`space_name` = 目录名），没有槽位；
状态由插件给：在跑 → working，有授权 / 提问等着 → blocked，其余 done（hub 照常按看没看过分完成未看 / 已看）。

node 用 herdr 的 `session.snapshot`（一次拿到 workspace、tab、agent）填这些字段：herdr 经 `events.subscribe` 推来变化时取一次
（列表最多每秒重建一次，进入 / 离开 blocked 的不等，见 `docs/server.md`「卡片的快车道」），
平时 10 秒兜底一次（事件流断开、或有 TRAEX 窗格时每秒一次，见 `docs/server.md`「和 herdr 的连接」）。tab 名是默认数字时 App 显示 `#2`。

### 一个 tab 里的几个 pane

常见的用法是一个 Agent 在自己的 tab 里 `herdr pane split`，再在新 pane 里起 Claude / Codex 分头做同一件事。同一个 tab（`slot_id`）里的几个 Agent 是**一组**，不要求 Agent 额外写什么：

- herdr 不记 pane 是谁起的，只有位置：快照的 `layouts` 里每个 tab 有每个 pane 的 `rect` 和分屏树 `splits`。node 按分屏树排
  （`server/internal/herdr/layout.go`），给每个 Agent 会话填 `pane_index`（从 0 起）：
  `splits` 每项是树上的一个节点 `{id, direction, ratio, rect}`，不列子节点；`direction` 是 `right`（左右切）或 `down`（上下切），`rect` 是它切的那块。
  node 从整个 tab 开始，找 `rect` 正好是这块的节点、按它的方向切成两半（左 / 上那半在前；同方向有几条能切的线时取两半各自正好是一个 pane 或一个节点的那条），两半各自再这样排。
  这样先上下再各自左右切出来的 2×2（A、B 在上，C、D 在下）排成 A、B、C、D；只看 rect 的话两条线都能切，分不出先后。
  没有 `splits`（旧版 herdr）或某块找不到节点时退回只看 rect：找一条不穿过任何 pane 的直线，先试左右、再试上下。
  zoom 时 herdr 照样给每个 pane 原来的 rect；rect 互相重叠、切不开时按 herdr 自己列的顺序。宿主不给布局（herdr-core、Orca 等）或 pane 不在布局里就不带 `pane_index`。
  只有 `pane_index` 变了（`herdr pane swap`、关掉左上的 shell）node 也推 `sessions.changed`：列表的变化判断里有它（有没有、是几）。
- 组里按 `pane_index` 排，0 号（左上，分屏都是从它切出来的）是**发起者**，其余是它起的分支；没有 `pane_index` 时退回原来的顺序（会话 id）。
  左上是 shell 不是 Agent 时，组里没有 0 号，几个 Agent 平级（网页不标发起者）。
- 不推断跨 tab 的父子关系（Agent 把子任务开在别的 tab / workspace）；以后需要时再约定显式标记（类似待办的 `persona_from`）。

网页的表现见 `docs/web.md`；Android 见 `docs/android.md`「同一个 tab 里的几个 Agent」（#2515）；iOS 还按空间列每个会话（网页验收后再跟上）。

App 里的表现：

- **开发列表按空间分组**：有动静的空间（含需要你 / 工作中 / 完成未看）每个一张卡片，头部是空间名、机器、各状态数量，
  里面按 tab 顺序列每个 agent（tab 名、标题、状态、最新一句）；全是已看的空间收进「空闲的空间」，一行一个、可展开；
  不在运行的会话在「最近（未运行）」。Android（2026-11-02 起）是电脑 → 空间 → 会话的树，不再有「空闲的空间」这个分组和标题：
  空闲的空间留在所属电脑下面，排在有动静的空间后面，默认收起、样式弱一点，收起时标出里面有几个会话（`docs/android-design.md` 2.4 节）。
- **对话页顶栏的同伴菜单**：同一空间有多个 agent 时，顶栏第二行最左边的按钮列出它们（tab 名 + 状态；有别的 agent 需要你时按钮带红点），点一下直接切过去，不用返回列表。
  副标题是「机器 · 空间 › tab · 状态」。
- **空间页**：点空间卡片头部进入，网格展示每个 tab 的状态、标题、最新进展、待处理数量；可以「在这个空间开新标签」，
  会在同一个 herdr workspace 里新建 tab（`session.start` 的 `workspace_id`）。
