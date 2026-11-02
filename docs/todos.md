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

### 2. todo_agents 表
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
- 约束：
  - 主键：`(todo_id, machine_id, session_id)`。
  - 部分唯一索引：`UNIQUE (machine_id, session_id) WHERE released_at IS NULL`，保证一个 Agent 同一时间只能为一个待办活跃工作。

关联与释放语义：
- 当一个 Agent 被关联到新待办时，如果它当前在其它待办上处于活跃状态，在同一个事务中将原关联记录标为 `released`（记录 `released_at` 时间），旧待办中将其展示为「参与过」。
- 若之前参与过该待办的 Agent 再次挂回该待办，清除 `released_at` 并刷新 `linked_at` 与快照信息。
- 用户主动解除关联则物理删除该行记录。

## 接口概要

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/todos` | 全部待办列表（`created_at` 降序），每个待办包含 `agents` 数组（活跃在前按 `linked_at` 降序，参与过在后按 `released_at` 降序） |
| POST | `/api/v1/todos` | 新建待办 `{"title", "notes"?}` → `{"todo": Todo}`；`title` 不能为空（400）；只读角色（手表）403 |
| PATCH | `/api/v1/todos/{id}` | 修改待办 `{"title"?, "notes"?, "done"?}` → `{"todo": Todo}`；只修改传入的字段；不存在 404；只读角色 403 |
| DELETE | `/api/v1/todos/{id}` | 删除待办及其关联 → `{"ok": true}`；Agent 会话本身不动；不存在 404；只读角色 403 |
| PUT | `/api/v1/todos/{id}/agents/{mid}/{sid}` | 将已有 Agent 挂到待办上（不发消息） → `{"ok": true}`；已完成待办不可挂（409）；不存在 404；只读角色 403 |
| DELETE | `/api/v1/todos/{id}/agents/{mid}/{sid}` | 解除关联（物理删除该行） → `{"ok": true}`；不存在 404；只读角色 403 |
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
仅在会话当前有关联（`released_at` 为 `NULL`）且该待办未完成时出现。当关联解除、被转移或待办完成时，该字段自动清除，客户端无需在前端自行 join。

### WebSocket 实时推送
- `{"type":"todos"}`：当待办创建、修改、删除或 Agent 关联变化时广播，客户端收到后重新拉取 `GET /api/v1/todos`。
- 会话变更推送：当待办或关联发生变化影响会话时，hub 重新计算受影响电脑上的会话列表并推送 `sessions` 事件。

### 状态码与角色控制
- 缺少标题或参数格式错误：400 Bad Request。
- 待办不存在：404 Not Found。旧 hub 未实现待办接口时返回 404，各端客户端据此隐藏待办入口，不弹错。
- 关联到已完成的待办：409 Conflict。
- 只读角色（如配对的 Apple Watch，`role: "answer"`）：对待办所有写接口（POST / PATCH / DELETE / PUT）以及带 `todo_id` 的新建和发消息均返回 403 Forbidden。

## 状态计算规则

待办的运行状态由各端客户端根据挂着的活跃 Agents 实时计算，不持久化在数据库中。优先级按以下顺序，取第一个命中的状态：

| 优先级 | 状态 | 条件 |
| :--- | :--- | :--- |
| 1 | 已完成 | 用户点击了完成（`done_at != null`） |
| 2 | 等你 | 有挂着的活跃 Agent 处于 `needs_you` 状态（等待用户回答提问、审批计划或授权） |
| 3 | 待查看 | 有挂着的活跃 Agent 处于 `unread` 状态（任务完成，用户尚未查看） |
| 4 | 进行中 | 有挂着的活跃 Agent 处于 `working` 状态（工作中或正在启动） |
| 5 | 待跟进 | 挂着的活跃 Agent 均停滞（`idle` 已查看、`offline` 未运行或启动失败） |
| 6 | 未开始 | 没有挂着的活跃 Agent |

- 待办列表排序：未完成的在前（同状态按 `created_at` 降序），已完成的收起在列表底部。
- 完成即代表该事项结束：事项下所有 Agent 的会话中不再标注当前在为该待办干活；如需继续可「重新打开」。

## 各端交互设计

### 1. 侧边栏与主导航
- 侧边栏增加「待办」入口，展示未完成待办数量角标。

### 2. 待办列表与详情
- 列表页：搜索栏、未完成列表、底部收起的已完成分组。每行展示待办标题、状态点、挂着几个 Agent 以及当前最要紧的 Agent 的最新一句话。点击圆圈或向左滑动可标记完成。
- 详情页：直接编辑标题与说明；展示当前状态；展示当前活跃的 Agents 列表（状态点、标题、机器与工作区标签、最新消息摘要、更新时间，点击进入会话；超出会话时间窗的借助快照展示并标注「已结束」；启动失败展示原因并允许移除）；收起的「参与过」列表；底部常驻「交给 Agent」和「完成 / 重新打开」按钮；右上角更多菜单支持删除。

### 3. 交给 Agent
- 支持「新开一个」或「交给已有的」。
- 上下文拼装格式参考收件箱交给 AI：`<jarvis-context kind="context" title="待办 · <标题>" url="">说明</jarvis-context>`，紧接用户输入的任务指令。
- 新开会话带 `todo_id` 参数；交给已有会话发消息带 `todo_id` 与 `client_msg_id`。

### 4. Agents 视图整合
- Agents 页分组模式新增「按待办」：每个有活跃 Agent 的未完成待办作为独立分组段，段头展示待办标题与状态，点击段头直达待办详情；未挂待办的 Agent 汇总于末尾「没有待办」段。
- 其他分组模式下，挂有待办的 Agent 增加待办标题标签。
- Agent 的快捷菜单增加「关联到待办…」与「解除关联」。
- 对话页右上角更多菜单与会话信息弹窗展示「待办：X」，点击可跳转。

## 设计取舍与理由

1. **不做第二个飞书任务或提醒事项**：Jarvis 的待办专为驱动和跟踪 Agent 工作流设计，聚焦于「一件事分工给哪些 Agent、各自进展如何」，不做复杂的子任务、截止日、优先级树、日历提醒或多方协同步同。
2. **状态不入库**：待办整体状态完全派生自关联 Agent 的实时运行状态（提问中、工作中、已完成未看等）。Agent 状态变化极为频繁，派生计算避免了双写状态一致性与庞大的状态同步开销。
3. **单待办活跃限制**：通过 `UNIQUE (machine_id, session_id) WHERE released_at IS NULL` 强制一个 Agent 同时只能全心全意服务于一个当前待办。当需要它做别的事时，前一任务自动划为「参与过」，保证会话上下文与执行目标清晰。

## 已知限制

当 Agent 在对话中执行 `/clear` 后，底层会新建一个 session uuid，新会话不会自动挂载到原待办上，原会话行依然留在列表中。
