# 待办（Todos）

记下一件要做的事，把它交给一个或几个 Agent 去做，在待办里看哪些 Agent 在为它干活、各自在做什么、这件事整体到哪一步了；Agents 页也能按待办分组看。

## 为什么需要

一件事常常要开好几个 Agent（不同电脑、不同工作区、不同标签页）。Agents 列表按项目、状态或电脑排列，看不出哪几个是在为同一件事干活，也看不出这件事整体到哪一步了、卡在哪、在等谁。通过待办将分散的 Agent 聚合到一项目标下，便于跟踪进度与协同。

## 谁管什么（2026-11-04 起）

待办是助手的（`server/internal/assistant` 的 `todos.go`、`todo_decisions.go`、`todo_sessions.go`、`tasks_close.go`；以前在 hub，见 `docs/assistant.md`「和 hub 的边界」）：

- **助手**：存储（下面的四张表，还在 hub 的 Postgres 里、没有迁移，只有助手的进程读写）、下面「接口概要」里待办的全部接口、随项目自动挂和派生跟随、herdr 标记的对账、Agent 标题和启动错误的快照、决策记录、自动完成、做完的任务关掉 Agent。
  hub 收到某台电脑的会话列表时发 `sessions.changed`，用户处理了卡片发 `interaction.resolved`，这几件事都从这两个事件来。
- **hub**：只转发待办的接口（`assistantwire.Routes`，调用方的角色一起带过去，只读角色照旧 403），给 App 的会话列表、提问卡片、推送用助手在快照里报的那份（`todos`：每个 Agent 在为哪个未完成的待办干活、哪些 Agent 是安静的，`hub/assistant_todos.go`；安静的只报未完成或完成不到 7 天的安静任务的 Agent（`quietKeep`），让这份报告保持小，更早的任务的 Agent 再干活时照常推送）；
  `POST /api/v1/sessions`、`POST /api/v1/sessions/{mid}/{sid}/messages` 带 `todo_id` 时先问助手这个待办在不在、完成没有、要交给 Agent 的话前面拼上决策、新开的 Agent 再拼上项目背景（`assistant.todo_handoff`），交出去以后让助手挂上（`assistant.todo_link`）；经 hub 改工作区名字时告诉助手（`todos.space_renamed`）。协议见 `docs/protocol.md`「总线」。
- **助手没连着时**（hub 刚起来、助手的进程在重启）：待办的接口回 `503 assistant_offline`「助手暂时连不上，稍后再试」，带 `todo_id` 的新建会话和发消息同样 503（不知道待办在不在）；会话列表上的待办、安静的标记照 hub 留着的最后一份显示（hub 刚起来、助手还没报的那一两秒里没有）；这期间 hub 不发事件，所以自动挂、标记对账、决策记录要等它连上以后的下一份会话列表或下一次回答，经 hub 改名的工作区不迁移。

## 数据模型

服务端状态存储在 hub 的 Postgres 数据库中（读写的是助手，见上一节）：

### 1. todos 表
- `id` (text, PK)：不透明字符串标识符（如 `todo_3f2a1b9c`）。
- `title` (text, NOT NULL)：待办标题。
- `notes` (text, NOT NULL, DEFAULT '')：待办详细说明与要求，交给 Agent 时作为上下文一同发出。
- `done_at` (timestamptz)：完成时间；未完成时为 `NULL`。
- `created_at` (timestamptz, NOT NULL)：创建时间。
- `updated_at` (timestamptz, NOT NULL)：最后修改时间。
- `origin` (text, NOT NULL, DEFAULT '')：从哪段助手对话开的任务（会话 id），手动建的为空（[unified-entry.md](unified-entry.md)）。
- `plan_id` (text, NOT NULL, DEFAULT '')：由哪个计划开的（`task_plans.id`）。
- `done_by` (text, NOT NULL, DEFAULT '')：谁把它标成完成的：空 = 用户点的（或没开起来，见 `start_error`），`auto` = 助手判断任务做完后自动完成（见「自动完成」），`review` = 督办人验收后完成（#3124）。
- `done_reason` (text, NOT NULL, DEFAULT '')：自动完成时判断模型给的理由（如「已合并 PR 并发了 TestFlight」），用户点完成或重新打开时清空。
- `reopened_at` (timestamptz)：用户最近一次把已完成的待办重新打开的时间；在它之后有新的一轮工作，助手才会再判断要不要自动完成。

### 2. todo_spaces 表
记录整组挂到待办的项目工作区（herdr workspace）：
- `todo_id` (text, NOT NULL, FK → `todos(id)` ON DELETE CASCADE)：关联的待办 ID。
- `machine_id` (text, NOT NULL)：机器 ID。
- `space` (text, NOT NULL)：项目名（会话的 `space_name`，没有时用 `space_id`）。
- `linked_at` (timestamptz, NOT NULL)：关联时间。
- 约束：
  - 主键：`(todo_id, machine_id, space)`。
  - 唯一索引：`UNIQUE (machine_id, space)`，保证一个项目同一时间只跟一个待办。

### 3. todo_agents 表
记录待办与 Agent 会话的关联：
- `todo_id` (text, NOT NULL, FK → `todos(id)` ON DELETE CASCADE)：关联的待办 ID。
- `machine_id` (text, NOT NULL)：机器 ID。
- `session_id` (text, NOT NULL)：会话 ID。
- `linked_at` (timestamptz, NOT NULL)：挂上时间。
- `released_at` (timestamptz)：解除活跃关联时间；活跃时为 `NULL`。
- `title` (text, NOT NULL, DEFAULT '')：会话标题快照（超出会话列表时间窗后仍可用于展示）。
- `cwd` (text, NOT NULL, DEFAULT '')：会话工作目录快照。
- `agent` (text, NOT NULL, DEFAULT '')：启动器/Agent 类别快照（`claude` / `codex` 等）。
- `start_error` (text, NOT NULL, DEFAULT '')：异步新建失败的原因（便于在待办中查看与移除）。
- `source` (text, NOT NULL, DEFAULT 'manual')：关联来源：
  - `manual`：手动关联或交给已有 Agent；
  - `space`：随整组工作区项目挂上；
  - `spawned`：由挂在待办上的父 Agent 派生。
- `via` (text, NOT NULL, DEFAULT '')：关联途径：
  - source 为 `space` 时是项目键 `<machine_id>/<space>`；
  - source 为 `spawned` 时是父 Agent 标识 `<machine_id>/<session_id>`。
- `dismissed_at` (timestamptz)：手动移出时间戳。移出时同时置 `released_at` 与 `dismissed_at`。接口不返回已移出的记录，后续会话更新也不会自动挂回。
- 约束：
  - 主键：`(todo_id, machine_id, session_id)`。
  - 部分唯一索引：`UNIQUE (machine_id, session_id) WHERE released_at IS NULL`，保证一个 Agent 同一时间只能为一个待办活跃工作。

### 4. todo_decisions 表
这件事上用户拍过的板（见「决策记录」）：
- `id` (text, PK)：`dec_` 开头的不透明标识。
- `todo_id` (text, NOT NULL, FK → `todos(id)` ON DELETE CASCADE)。
- `question` / `choice` / `reason` (text)：问题、选了什么、原因（原因可空）。
- `source` (text, NOT NULL, DEFAULT 'manual')：`manual` 在待办里手动记的；`card` 用户答 Agent 的提问卡片时自动记的；`outcome` 任务做完时从 Agent 最后的回复里抽的结论、决定和踩坑（见下面「做完记结论」）。
- `source_machine` / `source_session` / `source_message` (text)：出处，`card` 时是提问的 Agent 和那张卡片（interaction id），手动记的为空。
- `supersedes_id` (text, FK → `todo_decisions(id)` ON DELETE SET NULL)：这条改判了哪一条更早的决策。
- `created_at` (timestamptz, NOT NULL)。
- 约束：部分唯一索引 `(todo_id, source_message, question) WHERE source_message <> ''`，同一张卡片的同一个问题只记一次。

### 5. todo_supervision 表
主对话督办的任务的台账（#3124，`docs/unified-entry.md`「督办」），一行一个，没有督办的待办没有行：
- `todo_id` (text, PK, FK → `todos(id)` ON DELETE CASCADE)。
- `supervisor` (text, NOT NULL)：督办人，开它的对话。
- `done_when` (jsonb, NOT NULL, DEFAULT '[]')：验收标准（字符串数组）；`expect_minutes` (integer, NOT NULL, DEFAULT 0)：预计分钟数。
- `state` (text, NOT NULL, DEFAULT '')：`''` 进行中、`pending_review`、`rework`、`needs_user`、`accepted`、`cancelled`；`round` (integer)：返工轮次。
- `reviews` (jsonb, NOT NULL, DEFAULT '[]')：每次验收（`verdict`、逐条 `checks`、`note`），最多留 20 条；`updated_at` (timestamptz)。

关联与释放语义：
- 当一个 Agent 被关联到新待办时，如果它当前在其它待办上处于活跃状态，在同一个事务中将原关联记录标为 `released`（记录 `released_at` 时间），旧待办中将其展示为「参与过」。
- 若之前参与过该待办的 Agent 再次手动挂回该待办，清除 `released_at` 和 `dismissed_at`，并刷新 `linked_at` 与快照信息。
- 用户从待办中主动移出 Agent 时，将该行置 `released_at` 与 `dismissed_at`（软移出，接口不返回）。

## 接口概要

`/api/v1/todos…` 都是助手的接口，hub 原样转发（助手没连着时 `503 assistant_offline`）；最后两行是 hub 自己的接口，`todo_id` 那部分问助手。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/todos` | 全部待办列表（`created_at` 降序），每个待办包含 `spaces` 列表和 `agents` 列表（过滤掉已移出行；活跃在前按 `linked_at` 降序，参与过在后按 `released_at` 降序） |
| POST | `/api/v1/todos` | 新建待办 `{"title", "notes"?}` → `{"todo": Todo}`；`title` 不能为空（400）；只读角色（手表）403 |
| PATCH | `/api/v1/todos/{id}` | 修改待办 `{"title"?, "notes"?, "done"?}` → `{"todo": Todo}`；只修改传入的字段；用户改完成状态时清掉 `done_by` / `done_reason`，从已完成改回未完成时记 `reopened_at`；不存在 404；只读角色 403 |
| POST | `/api/v1/todos/retitle` | 把以前按 prompt 第一行起标题的任务待办改成短标题（见「待办标题」）→ `{"dry_run", "todos": [{"id", "old_title", "new_title"}]}`；`?dry_run=1` 只列不改；只读角色 403 |
| POST | `/api/v1/todos/{id}/done-check` | 只读地跑一次「自动完成」的判断 → `{"verdict", "reason", "would_close", "skip", "replies", "signals"}`，不写库（见「自动完成」）；不存在 404；只读角色 403 |
| DELETE | `/api/v1/todos/{id}` | 删除待办及其关联 → `{"ok": true}`；Agent 会话本身不动；不存在 404；只读角色 403 |
| PUT | `/api/v1/todos/{id}/spaces/{mid}/{space...}` | 整组挂工作区项目到待办 → `{"ok": true, "linked": N}`；`space` 为 URL 编码的项目名（通配路径）；已完成待办不可挂（409）；不存在 404；只读角色 403 |
| DELETE | `/api/v1/todos/{id}/spaces/{mid}/{space...}` | 解除整组挂（物理删除待办下该项目的 space 关联 Agents） → `{"ok": true}`；不存在 404；只读角色 403 |
| PUT | `/api/v1/todos/{id}/agents/{mid}/{sid}` | 将已有 Agent 挂到待办上（不发消息） → `{"ok": true}`；清空 `dismissed_at`；已完成待办不可挂（409）；不存在 404；只读角色 403 |
| DELETE | `/api/v1/todos/{id}/agents/{mid}/{sid}` | 移出 Agent（软移出：置 `released_at` 和 `dismissed_at`，接口不返回且不再自动挂回） → `{"ok": true}`；不存在 404；只读角色 403 |
| GET | `/api/v1/todos/{id}/decisions` | 决策列表 → `{"decisions": [...]}`，按时间先后；默认只给生效的，`?all=1` 连被改判的一起给（被改判的带 `superseded_by`）；没有时是 `[]`；不存在 404 |
| POST | `/api/v1/todos/{id}/decisions` | 手动记一条 `{"question", "choice", "reason"?, "supersedes_id"?}` → `{"decision": Decision}`；问题或选择为空 400，`supersedes_id` 不是这个待办里更早的一条 400；只读角色 403 |
| PATCH | `/api/v1/todos/{id}/decisions/{did}` | 补标改判 `{"supersedes_id"}`（空字符串撤销） → `{"decision": Decision}`；规则同上；不存在 404；只读角色 403 |
| POST | `/api/v1/sessions` | 新建会话，支持可选 `todo_id` 参数：创建成功后在同一请求内自动挂上待办；待办不存在 404，已完成 409，只读角色 403 |
| POST | `/api/v1/sessions/{mid}/{sid}/messages` | 发送消息，支持可选 `todo_id` 参数：发送成功后自动挂上待办；待办不存在 404，已完成 409，只读角色 403 |

### 会话装饰字段
在会话列表（`GET /api/v1/sessions` 及 WebSocket 的 `sessions` 推送）中，hub 按助手在快照里报的那份（`todos.links`）注入待办字段：
```jsonc
"todo": {
  "id": "todo_weather",
  "title": "天气 App 发 1.3 版"
}
```
仅在会话当前有关联（`released_at` 为 `NULL` 且 `dismissed_at` 为 `NULL`）且该待办未完成时出现。当关联解除、被转移或待办完成时，该字段自动清除，客户端无需在前端自行 join。
会话上报中携带的内部 `spawn_from` 字段在 hub 推送给客户端前统一剔除。

### WebSocket 实时推送
- `{"type":"todos"}`：当待办创建、修改、删除、整组挂/解除或 Agent 关联变化时广播，客户端收到后重新拉取 `GET /api/v1/todos`。
- 会话变更推送：当待办或关联发生变化影响会话时，助手报一份新的快照，hub 比对出哪台电脑的会话变了，重新推送那台的 `sessions` 事件。

### 状态码与角色控制
- 缺少标题或参数格式错误：400 Bad Request。
- 待办不存在：404 Not Found。旧 hub 未实现待办接口时返回 404，各端客户端据此隐藏待办入口，不弹错。
- 关联到已完成的待办：409 Conflict。
- 助手没连着：503 `assistant_offline`（待办的接口，以及带 `todo_id` 的新建和发消息）。
- 只读角色（如配对的 Apple Watch，`role: "answer"`）：对待办所有写接口（POST / PATCH / DELETE / PUT）以及带 `todo_id` 的新建和发消息均返回 403 Forbidden。

## 整组挂与自动挂

### 整组挂
一个 herdr 工作区可以整组挂载到未完成待办上：
- 挂上时，项目内所有当前在线（状态不是 offline）的 Agent 批量挂载到目标待办，`source = "space"`，`via = "<machine_id>/<space>"`。
- 若该项目原已挂在其它待办上，先从旧待办解除（旧待办中属于该项目的 space 关联 Agent 被移除），再转移至新待办。
- 当待办下某 Agent 曾被标记移出（dismissed），整组挂时一并清除 dismissed 状态。
- 解除整组挂时，删去工作区关联，并物理删除该待办下 `source = "space"` 且 `via` 为该项目的 Agent 行（手动挂和派生的 Agent 保持不变）。

### 随项目自动挂
助手收到某台电脑的会话列表（`sessions.changed`）时，检查处于已整组挂项目中的在线 Agent：
1. 该 Agent 当前没有其它活跃待办关联（自动规则不抢占其它待办中的 Agent）；
2. 目标待办中此前未出现过该 Agent 的任何记录（活跃、参与过或曾被手动移出均算出现过，不再自动挂入）；
满足条件时自动挂入该待办（`source = "space"`）。
自动挂先改助手的内存、马上报给 hub（hub 再推一次那台电脑的会话列表），数据库写入在后台完成，写不进去就撤回。
以前 hub 自己挂，同一次推送的会话列表里就带着关联；现在要等助手报回来的下一次推送（隔一次总线来回）。
待办标记完成后，自动挂机制停止生效；待办重新打开后恢复。

经 hub 改名（与置顶、屏蔽共用改名接口）时，hub 告诉助手（`todos.space_renamed`），`todo_spaces` 中的空间名及 `todo_agents` 中对应的 `via` 字段一并迁移。助手先写库再改内存并广播 `{"type":"todos"}`。若改名后的目标名称已被其他待办关联（唯一索引冲突），该项目原有的待办挂靠作废并清理，并记录日志。助手没连着时这次改名不迁移（同「已知限制」第 1 条）。

## 派生跟随

挂在待办上的 Agent 派出的子 Agent 自动挂入同一个待办，并追踪其来源层级：
1. **父 Agent 标记**：处于未完成待办上的活跃 Agent，其 herdr pane 上带有 `persona_todo_ref` 标记，值为 `<todo_id>@<machine_id>/<session_id>`。
2. **派活方传递标记**：派活方拉起子 Agent 后，读取自身 pane 上的标记，并将值通过 `persona_from` 标记写入子 Agent 的 pane。
3. **node 上报与助手关联**：node 在发现会话时将 `persona_from` 填入会话的 `spawn_from` 字段上报给 hub，hub 把会话列表原样交给助手（`sessions.changed`）；助手解析待办 ID 与父 Agent，若待办未完成、子 Agent 无活跃关联且在该待办中无历史记录，则自动挂入该待办（`source = "spawned"`，`via = "<parent_machine>/<parent_session>"`）。父 Agent 是否在线或是否在同一台电脑均不影响跟随。
4. **孙子辈跟随**：子 Agent 挂入待办后，其 pane 同样由 node 打上 `persona_todo_ref`，后续进一步派生的孙子 Agent 依相同机制持续跟随。
5. **消费标记**：子 Agent 成功获得活跃关联后，node 在周期对账中自动清除其 pane 上的 `persona_from` 标记，防止后续被误挂回。

### 派活方约定与示例命令
派活脚本在启动子 Agent（无论在同机还是跨机）后，应执行如下转写命令：
```sh
REF=$(herdr pane get "$HERDR_PANE_ID" | jq -r '.result.pane.tokens.persona_todo_ref // .result.pane.tokens.persona_from // empty')
[ -n "$REF" ] && herdr pane report-metadata "$CHILD_PANE" --source persona --token "persona_from=$REF"
```
注：若子 Agent 启动在另一台机器上，读取父 pane 标记的命令在本地执行，向 `$CHILD_PANE` 写入标记的命令发送给目标机器的 herdr 执行。

### botmux 跟随
botmux 会话没有 herdr pane，不带 `persona_from`。node 从 `botmux observe --include-raw` 读每个会话的飞书群（`raw.chatId`）和话题根消息（`raw.rootMessageId`），再读 botmux 数据目录（`SESSION_DATA_DIR`，否则 `~/.botmux/.data-dir` 指的目录，否则 `~/.botmux/data`）里的 `orchestrate-dispatch.json`：它以派出去的话题根消息为键，`orchSessionId` 是派活的会话（`botmux dispatch --session-id`）。这三样随会话报给 hub（`chat`、`topic`、`dispatched`），hub 推给客户端前去掉。助手在前两条规则之后再试两条，都只挂当前没有活跃待办、也没在目标待办里出现过的 Agent：
1. **派活跟随**：会话的话题是某个会话（任一台电脑；离线的也算，只要它还在那台电脑的列表里，即离线不超过 `offlineWindow`）用 `botmux dispatch` 派出来的，派活的会话挂在未完成待办上，就挂进同一个待办（`source = "spawned"`，`via = "<machine>/<session>"`）。
2. **同群跟随**：同一个飞书群里其它在线 botmux Agent 挂的未完成待办，挂得最多的一个至少是第二名的两倍（只有一个待办时直接算），就挂进去（`source = "chat"`，`via` 为群 id，客户端显示「同群」）。打平或差得不够就不挂；挂错了可以手动改挂。靠这条挂进来的 Agent 不参与投票（否则一个待办会从一个 Agent 扩散到整群），10 分钟没报过列表的电脑上的 Agent 也不算。

## herdr 标记与对账

在 herdr 宿主环境下，待办关联状态会同步映射到 herdr pane 标记中（侧边栏展示）：
- **写入的标记**：挂在未完成待办上的 Agent，其 pane 上由 node 维护两个 metadata token：
  - `persona_todo`：待办标题；
  - `persona_todo_ref`：`<todo_id>@<machine_id>/<session_id>`。
- **清除标记**：当待办完成、待办被删除或 Agent 关联被解除时，node 清空对应的 `persona_todo` 和 `persona_todo_ref` 标记。
- **来源与命名空间保护**：node 写入与清理标记一律使用 `--source persona`。对账操作仅针对 `persona_todo`、`persona_todo_ref` 和 `persona_from` 三个名字，绝不触碰用户或其它插件的 metadata。
- **定期对账机制**：助手将期望标记集合按会话 ID 下发（node 的 RPC 方法 `todo.sync_session_tokens`，经 `device.call`）：关联变了的电脑马上下发，没变的不重发；node 和 hub 的连接是新的一条时（`sessions.changed` 的 `connected` 变了：node 可能重启过，收到之前不动标记）新连接上的第一份会话列表再全发一次，和以前 hub 在 node 重连时立刻重发一样；node 仅基于当前活着的会话与宿主实时报上来的 Agent 会话信息动态解析对应的 live pane，对比 herdr 当前标记与期望集合，补齐缺失标记、清理多余标记。两个会话映射到同一 pane 时只认当前存活者。node 每隔 30 秒执行一次对账检查，在 herdr 服务重启、pane ID 变更或热交接后，自动将标记恢复到正确的新 pane 上，已退出的旧会话不会污染当前 pane。

## 状态计算规则

待办的运行状态由各端客户端根据挂着的活跃 Agents 实时计算，不持久化在数据库中。优先级按以下顺序，取第一个命中的状态：

| 优先级 | 状态 | 条件 |
| :--- | :--- | :--- |
| 0 | 没开起来 | 从对话开的任务 Agent 没启动起来，助手把待办结束掉（`done_at` + `start_error`，#1077）；客户端显示失败、能移除，不算进行中 |
| 1 | 已完成 | 用户点击了完成，或助手判断从对话派出去的任务已经做完（`done_at != null`；`done_by = "auto"` 时客户端标「自动完成」） |
| 2 | 等你 | 有挂着的活跃 Agent 处于 `needs_you` 状态（等待用户回答提问、审批计划或授权） |
| 2.5 | 中断了 | 有挂着的活跃 Agent 停着（`unread` / `idle`）且带 `turn_error`：这一轮被模型服务出错打断（连接中断、过载、限流、登录过期……），没做完（#1516）；红色，卡片上「继续」= 给它发「继续」 |
| 3 | 待查看 | 有挂着的活跃 Agent 处于 `unread` 状态（任务完成，用户尚未查看） |
| 4 | 进行中 | 有挂着的活跃 Agent 处于 `working` 状态（工作中或正在启动） |
| 5 | 待跟进 | 挂着的活跃 Agent 均停滞（`idle` 已查看、`offline` 未运行或启动失败） |
| 6 | 未开始 | 没有挂着的活跃 Agent |

- 待办列表排序：未完成的在前（同状态按 `created_at` 降序），已完成的收起在列表底部。
- 完成即代表该事项结束：事项下所有 Agent 的会话中不再标注当前在为该待办干活；如需继续可「重新打开」。
- 任务还在开 Agent 时用户点了完成（机器忙时能开几分钟，`done_at` 先落下）：不算「没开起来」，待办保持已完成、没有 `start_error`；已经开起来的 Agent 挂在这个已完成的待办上，交给「做完的任务关掉 Agent」按原有规则（空闲够久才关）回收（#2954）。

## 待办标题

`task_plan` 和 `agent_start` 开的待办（#2775）以前直接用 prompt 第一行当标题，开头常是给 Agent 的话（「ultrathink ultracode 主人 11-05 19:47 的反馈，已授权直接开工：…」「秘书派活（主人授权过…）」），看不出是什么事。现在分两步（`assistant/todo_titles.go`）：

1. **建待办时**同步给一个清理过的标题：去掉开头的 ultrathink / ultracode / think hard 之类的关键词、「主人 11-05 19:47 的反馈」这种来历和时间戳、「秘书派活（…）」「【…】」这种括号说明、「已授权直接开工」，截到 20 个字（日语 20、其他语言 40 个字符）。不调模型，不挡任务开工。
2. **后台**用小任务模型（模型配置里的「待办标题」`todo.title`，继承「知识整理」那一类，和对话标题 `assistant.title` 一样经 `models.complete`）读 prompt 起一个说清是什么事的短标题，替换第 1 步的。模型不可用、出错、回空就保留第 1 步的。替换时核对标题还是第 1 步给的那个（库里是带条件的 `UPDATE … WHERE title = 旧标题`），用户在这之间改过的不覆盖。提示词是 `prompts/todo/title.md`、`title_input.md`。

`task_plan` 里助手自己给的标题：不超过上限、开头没有上面那些前缀的照用，不调模型；是 prompt 第一行、太长或带前缀的才走上面两步。

**已有待办批量改**：`POST /api/v1/todos/retitle` 只挑任务开的待办（`origin` 不为空），而且标题还是以前派活时截下来的 prompt 第一行：超过上限，并且是 `notes` 的开头（截断、带不带「…」都算）。只是看起来像 prompt 的短标题（「【紧急】修登录」「11/05 反馈：修登录」）当成用户起的，不动（待办没有记谁起的标题，这是能做到的最保守判断）。每个用模型起标题（4 个并发），模型没有就用第 1 步的清理结果；改之前同样核对标题没变。

```sh
curl -X POST '<hub 地址>/api/v1/todos/retitle?dry_run=1' -H 'Authorization: Bearer <token>'   # 先看会改哪些
curl -X POST '<hub 地址>/api/v1/todos/retitle' -H 'Authorization: Bearer <token>'             # 再改
```

## 自动完成

从助手对话派出去的任务（`task_plan` 开的待办，`origin` 不为空）做完以后由助手自动标成完成，不用用户逐个点（主对话督办的任务除外：判断做完时改成「待验收」，由督办人用 `task_review` 验收后才完成，`done_by = review`，见 `docs/unified-entry.md`「督办」）；用户手建的待办（`origin` 为空）一律不自动完成，仍然是「用户点了完成」才算完成。

**什么时候判断**：任务的 Agent 全部停下（没有在工作、没有等用户回答、没有待处理的卡片）、助手往对话里发 done 回执的同一处（`assistant/tasks.go` 的 `taskTurnEnded`，一段工作只发一次回执，也只判断一次）。

**怎么判断**：Agent 停下不等于做完了：停在「没有全改完……」「等待审核」的都不该关。助手用判断模型（经 hub 的模型配置，`app.call models.json`；模型配置里的「AI 分身判断器」`inbox.judge`，即 hub.json 的 `inbox.judge_ai`，没配时继承 `ai`）看三样东西：任务标题、派活时的要求（待办的 `notes`）、每个活跃 Agent 最近一轮的内容（经 `session.items` 读：这一轮里用户给它的话和它最后一条完整回复；读不到时用会话列表里截短的 `last_text`）。用户后来又给 Agent 发了新的要求时，要求以后来的为准（原来的已在之前几轮处理过）；后来的话只是确认、回答它的问题（「好」「👌」「按 A」）时仍以派活时的要求为准（#1116）。模型答五选一：

| 结论 | 含义 | 自动完成 |
| --- | --- | --- |
| `done` | 要求的事做完了，回复交代了结果，不需要用户再做什么，也不等别人 | 是 |
| `not_done` | 只做了一部分、出错、卡住、只是汇报进度或计划 | 否 |
| `waiting` | 自己能做的做完了，在等外部结果（审核、CI、别人回复、发布排队） | 否 |
| `needs_user` | 要用户拍板、提供信息、批准、手动操作或验收 | 否 |
| `unsure` | 看不出来 | 否 |

- 附带说明不影响完成：没截图的平台、没实测的环境、提醒用户的事都不算没做完；交付以后例行的等待（TestFlight 外部组的 Beta 审核、CI 重跑）也不算在等；处理 App Store / Mac App Store 审核意见、提交后在等审核结论的算 `waiting`。
- 宁可不关：模型不可用、超时、回复不是合法 JSON、结论不是 `done`，都不动待办，只记日志；回执照发。
- 被模型服务打断的一轮不判断（#1516）：任务的某个 Agent 停在 API 错误上（会话的 `turn_error`：Claude Code 是它最后一句的「API Error: …」等，Codex 是 node 从这一轮的错误报的，#2185），回执写 `state: "interrupted"`（摘要「模型服务出错，中断了」，错误原文放 `error`），不调判断模型、不关待办；`done-check` 读到的最后一句是这种错误时直接答 `not_done`。
- 没有任何输出的一轮同样不判断（#2349）：最后停下的 Agent 这一轮（node 的 `session.items` `turns: 1`）里没有 assistant 的文字、工具调用或图片，又没有 `turn_error`（模型服务回 200 但内容是空的，Claude Code 补一条看不见的「no visible output」提示重试后静默结束），回执写 `interrupted`（error 是「这一轮没有任何输出：…」），通知用 interrupted，不调判断模型、不关待办。读不到条目（node 调不通、没有条目）和用户自己按 Esc 停的（「已中断」notice）不算。
- 便宜的线索：最后的回复里有 GitHub PR 链接且提到合并（merged / 已合并 / 合入），或提到 TestFlight 加构建号，助手把这两个线索写进输入，只作提示，不能单凭它判完成。
- 提示词在 `server/internal/prompts/assistant/task_done.md`、`task_done_input.md`（`docs/prompts.md`）。

**能看出来、能撤销**：自动完成写 `done_at`、`done_by = "auto"`、`done_reason`。iOS / Mac / 网页的待办列表行和详情页在已完成的状态旁标「自动完成」，详情页显示理由；「重新打开」和手动完成的一样回到未完成，同时清掉 `done_by`、`done_reason`，记下 `reopened_at`。

**重新打开以后**：用户重新打开过的待办，同一段工作不再被自动关：只有 `reopened_at` 之后任务的某个 Agent 又开始了一轮（用户再给它发消息、它又接着干），这一轮做完时才重新判断。助手重启后不知道这一轮是什么时候开始的，这时对重新打开过的待办不判断（宁可不关）。写库时再核对一次（`done_at IS NULL` 且 `reopened_at` 早于这段工作的开始），用户在判断途中刚点了完成或重新打开的，以用户的为准。

**完成之后**：和用户点完成走同一条路：会话不再带待办字段，node 对账时清掉 `persona_todo` / `persona_todo_ref` 标记，整组挂和派生跟随停止，收件箱的 AI 分身不再把消息关联到它；助手的实时快照里这个任务标成「已完成（hub 判断做完了自动关的）」，24 小时后不再列出。

**排查**：`POST /api/v1/todos/{id}/done-check` 按同样的输入跑一次判断，只返回结论，不写库（只读角色 403）：

```jsonc
{"verdict": "waiting", "reason": "等 App 审核", "would_close": false, "skip": "", "replies": 1, "signals": {"merged_pr": false, "build": false}}
```

`skip` 不为空时表示这次自动流程不会判断它（`not_a_task` 手建的待办、`done` 已经完成、`reopened` 重新打开后还没有新一轮工作），但接口照样给出模型的结论，方便看。

## 做完的任务关掉 Agent

从助手对话派出去的任务（`origin` 不为空）完成以后——用户点的完成或助手的自动完成都算——它的 Agent 不再留在 herdr 里占一个标签页：
助手每分钟看一遍（`assistant/tasks_close.go`，会话状态读 hub 给 App 的那份会话列表 `sessions.list`），任务完成且 Agent 静止都满 3 分钟（`taskCloseIdle`，从完成时间和 Agent 最后一次活动里取较晚的算）就让 node 关掉它（`session.retire`，`node/retire.go`）：
关的是它的窗格（标签页里只有它时关整个标签页，和手机上「关闭」一样），Agent 进程随之退出。

**保留什么**：待办里的 Agent 记录（机器、会话 id、目录）不动，不算 released；node 记的启动信息（启动器、目录、模型，hub 的 `node_sessions`）和会话记录都在。
读结果照常从会话记录读（回执、`agent_read`、聊天页），「完成未看」的也可以关。

**怎么接着用**：给已关闭的 Agent 发消息——`agent_send`、任务卡片上直接发给任务（`route`）、聊天页——node 先用原来的启动器在原目录、原工作区开一个新标签页恢复会话（标签名取会话标题），再把消息发进去：

| Agent | 恢复方式 |
| --- | --- |
| Claude Code | `<启动器> --resume <id>`，带上原来的权限模式、思考强度和模型 |
| Codex | 启动器的参数（换上原来的模型）`resume <id> -- <消息>`，经 `personad codex-start` 执行，消息作为参数传进去，不经过 shell、不上屏 |
| TRAEX | `<启动器> resume <id> <消息>` |

恢复要等启动器起来，`route` 和聊天页的发送一样不跟着手机断开而取消，最多等 5 分钟。完成的待办不因为这条消息重新打开；恢复后再次静止满 3 分钟会再被关掉。

**不关**：在工作、等用户（提问、计划、权限、任何对话框、待处理的卡片）、引擎看不出状态的；有排队的消息、后台 shell、在跑的子 Agent、`/loop`、在监听端口（开着开发服务器）的；还挂在别的未完成待办上的；助手对话；别的工具管的会话（外部 Codex app-server、runtime、daemon、pi）；不是 Persona 开的（不知道怎么恢复）；引擎不能新开标签页的（Orca、herdr-core）；Claude 没有会话记录、Codex / TRAEX 不知道启动器的。
这些由 node 拒绝（`not_retirable`），助手记下来，Agent 没变化时 10 分钟内不再问（`taskCloseRetry`，之后重试：node 可能已经升级了）；不认识 `session.retire` 的旧 node 同样当作拒绝，所以 hub 先升级、node 后升级也不会误关，node 升级后最多 10 分钟就接着关。
没挂在任何任务上的 Agent（用户自己开的、`agent_start` 开的、herdr-leader 之类的巡检会话）不归这里管。

## 决策记录

长程任务里最贵的错误是 Agent 忘了用户已经拍过的板：重复问同一件事，或悄悄推翻之前的决定（#1618）。决策散在各个 Agent 的对话里，换一个 Agent 或压缩一次就丢了，所以按待办收在一处：

- **一条决策** = 问题 + 选了什么 + 原因 + 时间 + 出处（哪个 Agent 问的、哪张卡片里答的）。
- **自动记**：用户在 App 里答了 Claude Code 的选项提问（`question` 卡片，即 `AskUserQuestion`），而提问的 Agent 正挂在一个未完成的待办上时，每个答了的问题记一条（`source = "card"`；hub 发 `interaction.resolved`，助手记，助手没连着时这次不记）。授权、计划审批、终端对话框、忽略掉的卡片不记；同一张卡片重复上报只记一次。
- **手动记**：待办详情页「决策」里「记一条」。
- **改判**：新决策可以标「取代」一条更早的（`supersedes_id`），旧的保留、划掉；只有没被任何一条取代的才算生效。写错了能用 PATCH 补标或撤销。只能取代同一个待办里更早的，所以不会绕成圈。
- **交给 Agent**：`POST /api/v1/sessions` 的 `prompt`、`POST …/messages` 的 `text` 带 `todo_id` 时（各端「交给 Agent」都这么发），hub 问助手（`assistant.todo_handoff`），助手把生效的决策拼成一段 `<jarvis-context kind="context" title="待办 · <标题> · 已定的决策">` 放在最前面（提示词 `server/internal/prompts/todo/decisions.md`），各端不用改；没有决策时原样发。读不出来（数据库出错）时照常发，只记日志。
- 记一条、改判都会更新待办的 `updated_at` 并广播 `{"type":"todos"}`，客户端据此重新拉决策。

还没做：Agent 侧按待办随时取决策的 MCP 工具（压缩后重拿）、安卓 / iOS 的决策列表。

## 做完记结论

三循环第 3 步（#2820）：Agent 做完的结论自动沉淀到待办和知识库，用户不用点进 Agent 看；下次派同项目的活自动带上（`server/internal/assistant/todo_outcome.go`）。

- **什么时候记**：主对话派出的任务（待办的 `origin` 不为空）变成完成时：主对话验收通过（`task_review` 的 `accepted`，`reviewDoneTodo`：主对话派出的任务都有督办，不自己关，等验收），自动完成判定做完了（`autoDoneTodo`，没有督办的任务），或用户点了完成（`PATCH /api/v1/todos/{id}` 的 `done: true`）。手动记的待办、启动失败结束的任务、验收时取消（`cancelled`）的任务不记。在后台做，不拖慢完成本身。
- **抽什么**：读这个任务上每个还挂着的 Agent 最后一轮的回复（读不到就用会话列表的 `last_text`；被模型服务打断的不算），用模型（用途 `kb.project`，提示词 `server/internal/prompts/todo/outcome.md`、`outcome_input.md`）抽出：一句话结论（最多 120 字）、PR / 部署链接（最多 3 个）、做过的决定、踩坑（各最多 5 条），回复里没交代的不编。读回复最多 1 分钟、等模型最多 90 秒，写决策和时间线另给 45 秒，模型慢了也不耽误写。
  模型不可用（没配、额度、回复解析不了）时，结论退回回执里的那段文字（最新那个 Agent 的回复压成一行，最多 120 字），链接只取回复里的 GitHub PR，不抽决定和踩坑。
- **写进决策区**：结论（「做完的结论」+ 结论和链接）、每条决定（问题 / 选了 / 原因）、每条踩坑（「踩过的坑」）各记一条决策，`source = "outcome"`，出处是最新那个 Agent（`source_machine` / `source_session`）。
  同一次完成只记一次：`source_message` 是 `outcome:<待办 id>:<完成时间>#<序号>`，同一次完成重复触发（两条路同时走到、hub 重启后再来）先查库里有没有这次的；结论那一条最先写，写进去的才接着写决定、踩坑和时间线（两个助手共用一个库时只有一个写成），待办已经删了也就不写；重新打开再完成是新的一次，另记。网页决策区的出处显示「Agent 做完时记的」；交给 Agent 时这几条也在「已定的决策」里，标着「Agent 做完时记的，不是用户定的」。
- **写进知识库**：每个 Agent 的工作目录对应的项目（`kb.Engine.ProjectsForWork`：先按项目登记的代码目录 `dirs`；一个都对不上时，按目录名找没登记任何代码目录的项目，名字、slug 或别名一样就算，登记了目录的项目不按名字找），
  在 `log.md` 追加一行「结论：<结论> <最多两个链接>」（一行最多 200 字：链接整个保留，结论按剩下的地方截短），来源「待办 · <标题>」（标题去掉 ` · `，最长 60 字），一个项目只记一行。不受 `kb.agent_turn` 每个 Agent 30 分钟一条的限制。知识库还没打开时等 30 秒再试一次，仍不行就只记决策。
- **派活带背景**：开一个新的 Agent 时，按它的目标目录（同上的找法）找到项目，把项目一句话和项目时间线里最近 5 条「待办 ·」结论（新的在前；只认来源以「待办 · 」开头、正文以「结论：」开头的行，同名的群聊记录不算）拼成一段 `<jarvis-context kind="context" title="项目 · <名字> · 背景">`（提示词 `server/internal/prompts/todo/project.md`）放在 prompt 最前面；一个目录属于多个项目时最多带两个。没有项目、项目既没一句话也没结论、知识库没开时原样发。
  - 助手的 `task_plan`（`startTaskAgent`，重试时连待办的决策一起带）、`agent_start`：直接拼；tab 名字仍按原来的标题 / prompt 起。
  - App「交给 Agent → 新开」走 hub：`POST /api/v1/sessions` 带 `todo_id` 和 `prompt` 时，hub 问助手（`assistant.todo_handoff` 带 `new: true`、`machine_id`、`cwd`，`cwd` 按那台电脑的家目录展开 `~/` 和空值），项目背景放在决策前面。
    各端的 Claude 是先后台开、再发第一条消息（`async`，prompt 为空）：hub 记下「这个会话是为这个待办新开的」（内存，15 分钟），它的第一条消息当作新 Agent 的第一句，带上项目背景和决策；iOS / 安卓的第一条消息不带 `todo_id` 也认。发送成功才忘掉这条记录，没发出去（被拒、什么都没打进去）再发时照样带；只答复的设备发的消息不算。hub 在这之间重启过就原样发。
  - 交给已有的 Agent（发消息）只带决策，不带项目背景。

还没做：结论的人工编辑和撤销（现在能在决策区标「取代」）、飞书汇报（#2768）、iOS / 安卓的决策列表。

## 各端交互设计

### 1. 侧边栏与主导航
- 侧边栏提供「待办」入口，展示未完成待办数量角标。

### 2. 待办列表与详情
- 列表页：搜索栏、未完成列表、底部收起的已完成分组。每行展示待办标题、状态点、挂着几个 Agent 以及当前最要紧的 Agent 的最新一句话。点击圆圈或向左滑动可标记完成。
- 详情页：直接编辑标题与说明；展示当前状态；展示当前关联的项目列表（整组挂）与活跃的 Agents 列表（状态点、标题、机器与工作区标签、最新消息摘要、更新时间，点击进入会话；超出会话时间窗的借助快照展示并标注「已结束」；启动失败展示原因并允许移除）；收起的「参与过」列表；底部常驻「交给 Agent」和「完成 / 重新打开」按钮；右上角更多菜单支持删除。
- 「决策」小节位于说明下方（网页）：生效的决策按时间先后列出（问题、选了、原因、时间、出处），每条可「改判」（写一条新的取代它）或在 ⋯ 里补标「改判了哪一条」/「撤销改判」；有被改判的时可「看全历史」，被改判的划掉。hub 不支持（路由 404）时整节不显示。
- 「项目」小节位于 Agents 上方，每行显示「项目名 · 机器名」，支持「移出」；没有在线 Agent 的项目标「当前没有 Agent」：只算当前 hub 组里、所在电脑连着的、状态不是 offline 的会话（别的组里 id 相同的电脑不算；电脑断连时 hub 把它的会话标成 unknown 而不是 offline，也不算，和网页一致）。Agent 的来源显示「随项目 <项目名>」或「由 <父 Agent 标题> 派出」；缺少项目来源时只写「随项目」，找不到父 Agent 时只写「派生」，手动挂的不标注。

### 3. 交给 Agent
- 支持「新开一个」或「交给已有的」。
- 上下文拼装格式参考收件箱交给 AI：`<jarvis-context kind="context" title="待办 · <标题>" url="">说明</jarvis-context>`，紧接用户输入的任务指令。
- 新开会话带 `todo_id` 参数；交给已有会话发消息带 `todo_id` 与 `client_msg_id`。

### 4. Agents 视图整合
- Agents 页分组模式支持「按待办」（#1836；iOS / Mac 没选过分组时默认按仓库，#2108）：每个有活跃 Agent 的未完成待办作为独立分组段，段头展示待办标题与状态，点击段头直达待办详情；未挂待办、在 herdr 里手动开的 Agent（`started_by_jarvis` 为假）单独一段「手动开的」（网页叫「在终端里开的」，#3057），排在「未归属」（网页叫「没挂待办的」）前面、展开（网页 #2608，iOS / Mac #2917：没有派生关系、空间也没挂待办时，没有哪条自动挂的规则认得它们；用行菜单的「关联到待办…」挂；派它的 Agent 不在待办上的派生 herdr Agent 各端都分不出来，也算在这里）；其余未挂待办的 Agent 汇总于末尾「未归属」段（上面有待办时默认折叠，里面有需要你的就展开）。网页记住每个浏览器上次选的分组，选过按待办就一直按待办（没选过默认按机器，#1990）。列表里多于一种宿主时，不在 herdr 的行标宿主名。
- 网页的「按待办」是三层树（#2526）：项目 › 待办 › Agent。项目取待办里 Agent 所在的仓库（规则同「按仓库」的 `repoOf`，跨电脑合并，几个仓库时取 Agent 最多的那个）；派生的 Agent（`source = "spawned"`，`via` 指向同一待办里的另一个 Agent）缩进挂在派它的 Agent 下面，父 Agent 不在列表里或成环时留在顶层。每个节点一行状态「等你 / 在做 / 做完」，取下面最要紧的（等你 > 在做 > 做完，中断的算等你）。已完成的待办、做完且最后活动超过 1 天的分支默认折叠；「手动开的」「未归属」在最后，里面再按项目分。待办页左侧的未完成待办同样按项目分段，没有 Agent 的放最后。Android 的「按待办」也是 项目 › 待办 › Agent（#3156），派生缩进和一天折叠还没做。
- 每个 Agent 行的标题前有来源标签（#2526）：botmux 的是「飞书群」，有话题链接时点一下在飞书里打开；herdr 的是「终端」；其他宿主不标。快速筛选多了「飞书群」「终端」两项，只看这一类。iOS / Mac 同样有标签和这两个筛选；Android 也有（筛选 #3154，标签 #3132，标签在时间下面，#3043）。
- 其他分组模式下，挂有待办的 Agent 增加待办标题标签。
- 按项目分组的组头展示关联待办标签，点击进入详情；菜单支持「挂到待办…」「改挂到其他待办…」「移出待办」，选择器可选未完成待办或新建后挂上。不在 herdr 工作区里的 Agent 分组（没有工作区 id）不显示待办标签，也没有这些菜单项。
- 列表还没加载出来，或非空列表中的待办都不带 `spaces` 数组时，不显示整组入口；成功加载的空列表显示入口。刷新请求进行中或遇到普通错误时保留上次的支持状态，刷新或重连成功后重新判断。整组接口返回「路由不存在」的 404 时隐藏入口，并提示 hub 需要升级；其他 404 按普通错误提示。旧 hub 的返回与旧本地缓存可继续解码。选择器每次打开或目标项目变化时重置输入；会话的当前待办以自己的 `todo` 标记为准。
- Agent 的快捷菜单（网页、iOS / Mac、安卓）：没挂待办时是「关联到待办…」；挂着待办时是「改挂到其他待办…」与「解除关联」，改挂直接挂到新待办，hub 同时放掉旧的关联（#2348）。
- 对话页右上角更多菜单与会话信息弹窗展示「待办：X」，点击可跳转；网页的对话页顶部标题下也显示所属待办，带「换个待办…」（#1836）。

### 5. Siri 与快捷指令（iOS / macOS）
- 「新建待办」（`AddTodoIntent`）：说「用 Persona 新建待办」或在快捷指令 / 操作按钮里用，标题必填、说明可选，后台直接写到 App 当前选中的 hub 组，不用打开 App。见 `docs/siri.md`。

## 设计取舍与理由

1. **不做第二个飞书任务或提醒事项**：Persona 的待办专为驱动和跟踪 Agent 工作流设计，聚焦于「一件事分工给哪些 Agent、各自进展如何」，不做复杂的子任务、截止日、优先级树、日历提醒或多方协同步同。
2. **状态不入库**：待办整体状态完全派生自关联 Agent 的实时运行状态（提问中、工作中、已完成未看等）。Agent 状态变化极为频繁，派生计算避免了双写状态一致性与庞大的状态同步开销。
3. **单待办活跃限制**：通过 `UNIQUE (machine_id, session_id) WHERE released_at IS NULL` 强制一个 Agent 同时只能全心全意服务于一个当前待办。当需要它做别的事时，前一任务自动划为「参与过」，保证会话上下文与执行目标清晰。
4. **自动完成只给从对话派出去的任务，并且由模型判断**：这类任务有明确的要求（派活 prompt）和交付（Agent 的最后回复），做完了还要用户逐个点完成是负担；手建的待办往往是用户自己的打算，hub 看不出做没做完。Agent 停下不等于做完，所以不按状态直接关，而是交给判断模型，拿不准时不关；自动关错的代价（用户以为做完了）远大于漏关（多点一下），所以可撤销、有理由、重新打开后不会被同一段工作再关一次。

## 已知限制

1. **herdr 内部直接改名**：若直接在 herdr 终端内重命名工作区而非通过 hub 接口改名，相当于生成了全新项目；原待办已关联的 Agent 保持不变，但该工作区后续新起的 Agent 将不会自动挂入。
2. **跨组多 hub 连接下的标记覆盖**：一台电脑的常驻 node 一次仅服务一个长连接通道。若同一台 node 被多个不同互联组的独立 hub 轮流或交替连接，后连接的 hub 下发的期望标记会覆盖前一个 hub 的标记。
3. **空工作区改名不迁移**：经 hub 重命名工作区时，若该工作区内没有活跃会话，hub 无法解析出旧工作区名，`todo_spaces` 与置顶、屏蔽一样不会自动迁移。
4. **`/clear` 会话重建**：当 Agent 在对话中执行 `/clear` 后，底层会新建一个 session uuid，新会话不会自动挂载到原待办上，原会话行依然留在列表中。
5. **助手没连着时**：待办的接口、带 `todo_id` 的新建和发消息回 503；这期间经 hub 改名的工作区不迁移、答的卡片不记成决策（见「谁管什么」）。
