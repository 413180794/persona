# 待办（Todos）

记下一件要做的事，把它交给一个或几个 Agent 去做，在待办里看哪些 Agent 在为它干活、各自在做什么、这件事整体到哪一步了；Agents 页也能按待办分组看。

## 为什么需要

一件事常常要开好几个 Agent（不同电脑、不同工作区、不同标签页）。Agents 列表按项目、状态或电脑排列，看不出哪几个是在为同一件事干活，也看不出这件事整体到哪一步了、卡在哪、在等谁。通过待办将分散的 Agent 聚合到一项目标下，便于跟踪进度与协同。

## 数据模型

服务端状态统一存储在 hub 的 Postgres 数据库中：

### 1. todos 表
- `id` (text, PK)：不透明字符串标识符（如 `todo_3f2a1b9c`）。
- `title` (text, NOT NULL)：待办标题。
- `notes` (text, NOT NULL, DEFAULT '')：待办详细说明与要求，交给 Agent 时作为上下文一同发出。
- `done_at` (timestamptz)：完成时间；未完成时为 `NULL`。
- `created_at` (timestamptz, NOT NULL)：创建时间。
- `updated_at` (timestamptz, NOT NULL)：最后修改时间。
- `origin` (text, NOT NULL, DEFAULT '')：从哪段助手对话开的任务（会话 id），手动建的为空（[unified-entry.md](unified-entry.md)）。
- `plan_id` (text, NOT NULL, DEFAULT '')：由哪个计划开的（`task_plans.id`）。
- `done_by` (text, NOT NULL, DEFAULT '')：谁把它标成完成的：空 = 用户点的（或没开起来，见 `start_error`），`auto` = hub 判断任务做完后自动完成（见「自动完成」）。
- `done_reason` (text, NOT NULL, DEFAULT '')：自动完成时判断模型给的理由（如「已合并 PR 并发了 TestFlight」），用户点完成或重新打开时清空。
- `reopened_at` (timestamptz)：用户最近一次把已完成的待办重新打开的时间；在它之后有新的一轮工作，hub 才会再判断要不要自动完成。

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

关联与释放语义：
- 当一个 Agent 被关联到新待办时，如果它当前在其它待办上处于活跃状态，在同一个事务中将原关联记录标为 `released`（记录 `released_at` 时间），旧待办中将其展示为「参与过」。
- 若之前参与过该待办的 Agent 再次手动挂回该待办，清除 `released_at` 和 `dismissed_at`，并刷新 `linked_at` 与快照信息。
- 用户从待办中主动移出 Agent 时，将该行置 `released_at` 与 `dismissed_at`（软移出，接口不返回）。

## 接口概要

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/todos` | 全部待办列表（`created_at` 降序），每个待办包含 `spaces` 列表和 `agents` 列表（过滤掉已移出行；活跃在前按 `linked_at` 降序，参与过在后按 `released_at` 降序） |
| POST | `/api/v1/todos` | 新建待办 `{"title", "notes"?}` → `{"todo": Todo}`；`title` 不能为空（400）；只读角色（手表）403 |
| PATCH | `/api/v1/todos/{id}` | 修改待办 `{"title"?, "notes"?, "done"?}` → `{"todo": Todo}`；只修改传入的字段；用户改完成状态时清掉 `done_by` / `done_reason`，从已完成改回未完成时记 `reopened_at`；不存在 404；只读角色 403 |
| POST | `/api/v1/todos/{id}/done-check` | 只读地跑一次「自动完成」的判断 → `{"verdict", "reason", "would_close", "skip", "replies", "signals"}`，不写库（见「自动完成」）；不存在 404；只读角色 403 |
| DELETE | `/api/v1/todos/{id}` | 删除待办及其关联 → `{"ok": true}`；Agent 会话本身不动；不存在 404；只读角色 403 |
| PUT | `/api/v1/todos/{id}/spaces/{mid}/{space...}` | 整组挂工作区项目到待办 → `{"ok": true, "linked": N}`；`space` 为 URL 编码的项目名（通配路径）；已完成待办不可挂（409）；不存在 404；只读角色 403 |
| DELETE | `/api/v1/todos/{id}/spaces/{mid}/{space...}` | 解除整组挂（物理删除待办下该项目的 space 关联 Agents） → `{"ok": true}`；不存在 404；只读角色 403 |
| PUT | `/api/v1/todos/{id}/agents/{mid}/{sid}` | 将已有 Agent 挂到待办上（不发消息） → `{"ok": true}`；清空 `dismissed_at`；已完成待办不可挂（409）；不存在 404；只读角色 403 |
| DELETE | `/api/v1/todos/{id}/agents/{mid}/{sid}` | 移出 Agent（软移出：置 `released_at` 和 `dismissed_at`，接口不返回且不再自动挂回） → `{"ok": true}`；不存在 404；只读角色 403 |
| POST | `/api/v1/sessions` | 新建会话，支持可选 `todo_id` 参数：创建成功后在同一请求内自动挂上待办；待办不存在 404，已完成 409，只读角色 403 |
| POST | `/api/v1/sessions/{mid}/{sid}/messages` | 发送消息，支持可选 `todo_id` 参数：发送成功后自动挂上待办；待办不存在 404，已完成 409，只读角色 403 |

### 会话装饰字段
在会话列表（`GET /api/v1/sessions` 及 WebSocket 的 `sessions` 推送）中，hub 从内存索引中注入待办字段：
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
- 会话变更推送：当待办或关联发生变化影响会话时，hub 重新计算受影响电脑上的会话列表并推送 `sessions` 事件。

### 状态码与角色控制
- 缺少标题或参数格式错误：400 Bad Request。
- 待办不存在：404 Not Found。旧 hub 未实现待办接口时返回 404，各端客户端据此隐藏待办入口，不弹错。
- 关联到已完成的待办：409 Conflict。
- 只读角色（如配对的 Apple Watch，`role: "answer"`）：对待办所有写接口（POST / PATCH / DELETE / PUT）以及带 `todo_id` 的新建和发消息均返回 403 Forbidden。

## 整组挂与自动挂

### 整组挂
一个 herdr 工作区可以整组挂载到未完成待办上：
- 挂上时，项目内所有当前在线（状态不是 offline）的 Agent 批量挂载到目标待办，`source = "space"`，`via = "<machine_id>/<space>"`。
- 若该项目原已挂在其它待办上，先从旧待办解除（旧待办中属于该项目的 space 关联 Agent 被移除），再转移至新待办。
- 当待办下某 Agent 曾被标记移出（dismissed），整组挂时一并清除 dismissed 状态。
- 解除整组挂时，删去工作区关联，并物理删除该待办下 `source = "space"` 且 `via` 为该项目的 Agent 行（手动挂和派生的 Agent 保持不变）。

### 随项目自动挂
hub 收到某台电脑的会话列表时，检查处于已整组挂项目中的在线 Agent：
1. 该 Agent 当前没有其它活跃待办关联（自动规则不抢占其它待办中的 Agent）；
2. 目标待办中此前未出现过该 Agent 的任何记录（活跃、参与过或曾被手动移出均算出现过，不再自动挂入）；
满足条件时自动挂入该待办（`source = "space"`）。
自动挂在收到会话时首先同步更新内存索引，保证同一次推送的会话列表中立即呈现关联，数据库写入在后台异步完成。
待办标记完成后，自动挂机制停止生效；待办重新打开后恢复。

经 hub 改名（与置顶、屏蔽共用改名接口）时，`todo_spaces` 中的空间名及 `todo_agents` 中对应的 `via` 字段一并迁移。改名先写库再改内存并广播 `{"type":"todos"}`。若改名后的目标名称已被其他待办关联（唯一索引冲突），该项目原有的待办挂靠作废并清理，并记录日志。

## 派生跟随

挂在待办上的 Agent 派出的子 Agent 自动挂入同一个待办，并追踪其来源层级：
1. **父 Agent 标记**：处于未完成待办上的活跃 Agent，其 herdr pane 上带有 `persona_todo_ref` 标记，值为 `<todo_id>@<machine_id>/<session_id>`。
2. **派活方传递标记**：派活方拉起子 Agent 后，读取自身 pane 上的标记，并将值通过 `persona_from` 标记写入子 Agent 的 pane。
3. **node 上报与 hub 关联**：node 在发现会话时将 `persona_from` 填入会话的 `spawn_from` 字段上报给 hub；hub 解析待办 ID 与父 Agent，若待办未完成、子 Agent 无活跃关联且在该待办中无历史记录，则自动挂入该待办（`source = "spawned"`，`via = "<parent_machine>/<parent_session>"`）。父 Agent 是否在线或是否在同一台电脑均不影响跟随。
4. **孙子辈跟随**：子 Agent 挂入待办后，其 pane 同样由 node 打上 `persona_todo_ref`，后续进一步派生的孙子 Agent 依相同机制持续跟随。
5. **消费标记**：子 Agent 成功获得活跃关联后，node 在周期对账中自动清除其 pane 上的 `persona_from` 标记，防止后续被误挂回。

### 派活方约定与示例命令
派活脚本在启动子 Agent（无论在同机还是跨机）后，应执行如下转写命令：
```sh
REF=$(herdr pane get "$HERDR_PANE_ID" | jq -r '.result.pane.tokens.persona_todo_ref // .result.pane.tokens.persona_from // empty')
[ -n "$REF" ] && herdr pane report-metadata "$CHILD_PANE" --source persona --token "persona_from=$REF"
```
注：若子 Agent 启动在另一台机器上，读取父 pane 标记的命令在本地执行，向 `$CHILD_PANE` 写入标记的命令发送给目标机器的 herdr 执行。

## herdr 标记与对账

在 herdr 宿主环境下，待办关联状态会同步映射到 herdr pane 标记中（侧边栏展示）：
- **写入的标记**：挂在未完成待办上的 Agent，其 pane 上由 node 维护两个 metadata token：
  - `persona_todo`：待办标题；
  - `persona_todo_ref`：`<todo_id>@<machine_id>/<session_id>`。
- **清除标记**：当待办完成、待办被删除或 Agent 关联被解除时，node 清空对应的 `persona_todo` 和 `persona_todo_ref` 标记。
- **来源与命名空间保护**：node 写入与清理标记一律使用 `--source persona`。对账操作仅针对 `persona_todo`、`persona_todo_ref` 和 `persona_from` 三个名字，绝不触碰用户或其它插件的 metadata。
- **定期对账机制**：hub 将期望标记集合按会话 ID 下发（RPC 方法 `todo.sync_session_tokens`）；node 仅基于当前活着的会话与宿主实时报上来的 Agent 会话信息动态解析对应的 live pane，对比 herdr 当前标记与期望集合，补齐缺失标记、清理多余标记。两个会话映射到同一 pane 时只认当前存活者。node 每隔 30 秒执行一次对账检查，在 herdr 服务重启、pane ID 变更或热交接后，自动将标记恢复到正确的新 pane 上，已退出的旧会话不会污染当前 pane。

## 状态计算规则

待办的运行状态由各端客户端根据挂着的活跃 Agents 实时计算，不持久化在数据库中。优先级按以下顺序，取第一个命中的状态：

| 优先级 | 状态 | 条件 |
| :--- | :--- | :--- |
| 0 | 没开起来 | 从对话开的任务 Agent 没启动起来，hub 把待办结束掉（`done_at` + `start_error`，#1077）；客户端显示失败、能移除，不算进行中 |
| 1 | 已完成 | 用户点击了完成，或 hub 判断从对话派出去的任务已经做完（`done_at != null`；`done_by = "auto"` 时客户端标「自动完成」） |
| 2 | 等你 | 有挂着的活跃 Agent 处于 `needs_you` 状态（等待用户回答提问、审批计划或授权） |
| 3 | 待查看 | 有挂着的活跃 Agent 处于 `unread` 状态（任务完成，用户尚未查看） |
| 4 | 进行中 | 有挂着的活跃 Agent 处于 `working` 状态（工作中或正在启动） |
| 5 | 待跟进 | 挂着的活跃 Agent 均停滞（`idle` 已查看、`offline` 未运行或启动失败） |
| 6 | 未开始 | 没有挂着的活跃 Agent |

- 待办列表排序：未完成的在前（同状态按 `created_at` 降序），已完成的收起在列表底部。
- 完成即代表该事项结束：事项下所有 Agent 的会话中不再标注当前在为该待办干活；如需继续可「重新打开」。

## 自动完成

从助手对话派出去的任务（`task_plan` 开的待办，`origin` 不为空）做完以后由 hub 自动标成完成，不用用户逐个点；用户手建的待办（`origin` 为空）一律不自动完成，仍然是「用户点了完成」才算完成。

**什么时候判断**：任务的 Agent 全部停下（没有在工作、没有等用户回答、没有待处理的卡片）、hub 往对话里发 done 回执的同一处（`tasks.go` 的 `taskTurnEnded`，一段工作只发一次回执，也只判断一次）。

**怎么判断**：Agent 停下不等于做完了：停在「没有全改完……」「等待审核」的都不该关。hub 用判断模型（模型配置里的「AI 分身判断器」`inbox.judge`，即 hub.json 的 `inbox.judge_ai`，没配时继承 `ai`）看三样东西：任务标题、派活时的要求（待办的 `notes`）、每个活跃 Agent 最后一条完整回复（经 `session.items` 读，读不到时用会话列表里截短的 `last_text`）。模型答五选一：

| 结论 | 含义 | 自动完成 |
| --- | --- | --- |
| `done` | 要求的事做完了，回复交代了结果，不需要用户再做什么，也不等别人 | 是 |
| `not_done` | 只做了一部分、出错、卡住、只是汇报进度或计划 | 否 |
| `waiting` | 自己能做的做完了，在等外部结果（审核、CI、别人回复、发布排队） | 否 |
| `needs_user` | 要用户拍板、提供信息、批准、手动操作或验收 | 否 |
| `unsure` | 看不出来 | 否 |

- 宁可不关：模型不可用、超时、回复不是合法 JSON、结论不是 `done`，都不动待办，只记日志；回执照发。
- 便宜的线索：最后的回复里有 GitHub PR 链接且提到合并（merged / 已合并 / 合入），或提到 TestFlight 加构建号，hub 把这两个线索写进输入，只作提示，不能单凭它判完成。
- 提示词在 `server/internal/prompts/assistant/task_done.md`、`task_done_input.md`（`docs/prompts.md`）。

**能看出来、能撤销**：自动完成写 `done_at`、`done_by = "auto"`、`done_reason`。iOS / Mac / 网页的待办列表行和详情页在已完成的状态旁标「自动完成」，详情页显示理由；「重新打开」和手动完成的一样回到未完成，同时清掉 `done_by`、`done_reason`，记下 `reopened_at`。

**重新打开以后**：用户重新打开过的待办，同一段工作不再被自动关：只有 `reopened_at` 之后任务的某个 Agent 又开始了一轮（用户再给它发消息、它又接着干），这一轮做完时才重新判断。hub 重启后不知道这一轮是什么时候开始的，这时对重新打开过的待办不判断（宁可不关）。写库时再核对一次（`done_at IS NULL` 且 `reopened_at` 早于这段工作的开始），用户在判断途中刚点了完成或重新打开的，以用户的为准。

**完成之后**：和用户点完成走同一条路：会话不再带待办字段，node 对账时清掉 `persona_todo` / `persona_todo_ref` 标记，整组挂和派生跟随停止，收件箱的 AI 分身不再把消息关联到它；助手的实时快照里这个任务标成「已完成（hub 判断做完了自动关的）」，24 小时后不再列出。

**排查**：`POST /api/v1/todos/{id}/done-check` 按同样的输入跑一次判断，只返回结论，不写库（只读角色 403）：

```jsonc
{"verdict": "waiting", "reason": "等 App 审核", "would_close": false, "skip": "", "replies": 1, "signals": {"merged_pr": false, "build": false}}
```

`skip` 不为空时表示这次自动流程不会判断它（`not_a_task` 手建的待办、`done` 已经完成、`reopened` 重新打开后还没有新一轮工作），但接口照样给出模型的结论，方便看。

## 各端交互设计

### 1. 侧边栏与主导航
- 侧边栏提供「待办」入口，展示未完成待办数量角标。

### 2. 待办列表与详情
- 列表页：搜索栏、未完成列表、底部收起的已完成分组。每行展示待办标题、状态点、挂着几个 Agent 以及当前最要紧的 Agent 的最新一句话。点击圆圈或向左滑动可标记完成。
- 详情页：直接编辑标题与说明；展示当前状态；展示当前关联的项目列表（整组挂）与活跃的 Agents 列表（状态点、标题、机器与工作区标签、最新消息摘要、更新时间，点击进入会话；超出会话时间窗的借助快照展示并标注「已结束」；启动失败展示原因并允许移除）；收起的「参与过」列表；底部常驻「交给 Agent」和「完成 / 重新打开」按钮；右上角更多菜单支持删除。
- 「项目」小节位于 Agents 上方，每行显示「项目名 · 机器名」，支持「移出」；没有在线 Agent 的项目标「当前没有 Agent」。Agent 的来源显示「随项目 <项目名>」或「由 <父 Agent 标题> 派出」；缺少项目来源时只写「随项目」，找不到父 Agent 时只写「派生」，手动挂的不标注。

### 3. 交给 Agent
- 支持「新开一个」或「交给已有的」。
- 上下文拼装格式参考收件箱交给 AI：`<jarvis-context kind="context" title="待办 · <标题>" url="">说明</jarvis-context>`，紧接用户输入的任务指令。
- 新开会话带 `todo_id` 参数；交给已有会话发消息带 `todo_id` 与 `client_msg_id`。

### 4. Agents 视图整合
- Agents 页分组模式支持「按待办」：每个有活跃 Agent 的未完成待办作为独立分组段，段头展示待办标题与状态，点击段头直达待办详情；未挂待办的 Agent 汇总于末尾「没有待办」段。
- 其他分组模式下，挂有待办的 Agent 增加待办标题标签。
- 按项目分组的组头展示关联待办标签，点击进入详情；菜单支持「挂到待办…」「改挂到其他待办…」「移出待办」，选择器可选未完成待办或新建后挂上。
- 列表还没加载出来，或非空列表中的待办都不带 `spaces` 数组时，不显示整组入口；成功加载的空列表显示入口。刷新请求进行中或遇到普通错误时保留上次的支持状态，刷新或重连成功后重新判断。整组接口返回「路由不存在」的 404 时隐藏入口，并提示 hub 需要升级；其他 404 按普通错误提示。旧 hub 的返回与旧本地缓存可继续解码。选择器每次打开或目标项目变化时重置输入；会话的当前待办以自己的 `todo` 标记为准。
- Agent 的快捷菜单提供「关联到待办…」与「解除关联」。
- 对话页右上角更多菜单与会话信息弹窗展示「待办：X」，点击可跳转。

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
