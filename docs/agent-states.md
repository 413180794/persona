# Agent 状态与空间模型

## 统一状态

App、手表、推送都只认这一套状态（`proto.Session.state`，hub 计算）。来源是宿主（目前是 herdr）的状态，加上 Persona 自己知道的两件事：
有没有待处理的交互、用户有没有在 App 里看过。

| 状态 | 含义 | 来自 herdr | Persona 补充 | 颜色 |
| --- | --- | --- | --- | --- |
| `needs_you` 需要你 | 卡在需要人参与的地方 | `blocked`（TUI 里的提问 / 确认框） | 有待处理的提问、计划、授权、终端交互（hook 拦下来时 herdr 还显示 working） | 红，呼吸 |
| `working` 工作中 | 正在跑 | `working` | — | 琥珀，呼吸 |
| `unread` 完成未看 | 一轮做完，结果还没看 | `done`（herdr 没被聚焦过） | 且 App 里在它完成之后没看过 | 绿 |
| `idle` 已看 | 做完且看过，等输入 | `idle`（herdr 里看过） | 或 herdr 还是 `done` 但 App 里看过 | 灰 |
| `offline` 未运行 | 不在宿主里，只有 transcript，发消息会 `--resume` | — | — | 浅灰 |
| `interrupted` 被重启中断 | 重启前在跑，重启把 herdr 的窗格一起带走了（#1917） | — | 记录里有窗格、启动时刻早于本次 node 启动，且现在没有活着的 agent | 浅灰，带「继续」 |
| `unknown` 未知 | 宿主认出了 agent 但判断不了状态 | `unknown` | — | 灰 |

排序优先级：需要你 → 工作中 → 完成未看 → 已看 → 未知 → 未运行。

**工作态**（2026-11-04，#1686，[agent-work.md](agent-work.md)）：上面是宿主视角，「完成未看」只说明这一轮停了。hub 在一轮停下后分诊，
给会话加上工作态 `work`：工作中 / 卡在具名的前置条件上（等你决定、等另一个 Agent、等外部事件、瞬时错误）/ 做完了，带一行「在等什么」或结论。
算状态时多看一眼它：**做完了**、**卡在 hub 会替它等的事上**（hub 自动重试、到点叫醒）的不再是 `unread`，是 `idle`；
等你决定的 hub 开一张提问卡片，于是是 `needs_you`。没分诊出来（刚停、模型失败、看不出来）的照旧是 `unread`。
颜色只有一份：`apple/Shared/StatusPalette.swift`，App、手表和实时活动共用（`docs/ios.md`「设计语言」）。
Agents 列表另有「待唤醒」：状态是已看，但 `/loop` 还没到点、或后台命令 / 子代理还在跑（`Session.isWaiting`），之后会有新消息。
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

DSH（`docs/dsh.md`）：`space_kind` 是 `dsh`，空间是会话的目录（`space_id` = 目录、`space_name` = 目录名），没有槽位；
状态由插件给：在跑 → working，有授权 / 提问等着 → blocked，其余 done（hub 照常按看没看过分完成未看 / 已看）。

node 用 herdr 的 `session.snapshot`（一次拿到 workspace、tab、agent）填这些字段：herdr 经 `events.subscribe` 推来变化时取一次
（列表最多每秒重建一次，进入 / 离开 blocked 的不等，见 `docs/server.md`「卡片的快车道」），
平时 10 秒兜底一次（事件流断开、或有 TRAEX 窗格时每秒一次，见 `docs/server.md`「和 herdr 的连接」）。tab 名是默认数字时 App 显示 `#2`。

App 里的表现：

- **开发列表按空间分组**：有动静的空间（含需要你 / 工作中 / 完成未看）每个一张卡片，头部是空间名、机器、各状态数量，
  里面按 tab 顺序列每个 agent（tab 名、标题、状态、最新一句）；全是已看的空间收进「空闲的空间」，一行一个、可展开；
  不在运行的会话在「最近（未运行）」。Android（2026-11-02 起）是电脑 → 空间 → 会话的树，不再有「空闲的空间」这个分组和标题：
  空闲的空间留在所属电脑下面，排在有动静的空间后面，默认收起、样式弱一点，收起时标出里面有几个会话（`docs/android-design.md` 2.4 节）。
- **对话页顶栏的同伴菜单**：同一空间有多个 agent 时，顶栏第二行最左边的按钮列出它们（tab 名 + 状态；有别的 agent 需要你时按钮带红点），点一下直接切过去，不用返回列表。
  副标题是「机器 · 空间 › tab · 状态」。
- **空间页**：点空间卡片头部进入，网格展示每个 tab 的状态、标题、最新进展、待处理数量；可以「在这个空间开新标签」，
  会在同一个 herdr workspace 里新建 tab（`session.start` 的 `workspace_id`）。
