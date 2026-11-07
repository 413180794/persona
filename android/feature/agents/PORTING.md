# Agents 移植记录

对照 `origin/master` 的 `AgentSheets.kt`、`SpaceScreen.kt`、`Grouping.kt`，以及 `GroupingTest`、`PinsTest`、`ScheduleTest`、`SetRoutinesTest`。定时任务实现另参考旧 `ApiClient.kt` 和 `AssistantProfile.kt`。

## 合并取舍

本车道和 r2-ag-ops（会话 / 空间操作）、r2-ag-new（新建会话）、r2-as-home（助手资料页）有重叠。合进集成分支时，每个功能只保留一份、一个入口：

| 功能 | 本车道的做法 | 合并后 |
| --- | --- | --- |
| 空间详情 | 两列卡片，按标签顺序展示全部成员（含单独置顶的会话） | **保留**。从空间分组标题的「›」进入；卡片的「⋮」和空间菜单用列表同一套 |
| Agent 操作 | 长按弹底部面板：置顶、屏蔽、重命名、关闭确认 | 用 r2-ag-ops 的长按菜单（另有停止循环、交给 Persona、拷贝恢复命令，改名可分开改会话名和标签名，结果未知时不重复操作），抽成 `AgentSessionMenuItems` 给列表行和空间页共用；底部面板删除 |
| 新建会话 | 底部面板：机器、目录、启动器、权限模式、首条消息；失败后按新出现的会话核对 | 用 r2-ag-new 的新建页（多思考强度、按机器记住选择、启动前落盘操作 ID、只查回执不重发）。本车道的「在此空间开新标签」并入：`NewAgentViewModel.startIn(AgentSpacePreset)` 固定机器和目录，请求带 `workspace_id`；启动请求的读超时也照本车道放宽到 130 秒（hub 给 `session.start` 留了 100 秒） |
| 置顶与屏蔽 | 经 `InboxAssistantTransport` 读写 hub，按配对缓存 | 用 r2-ag-ops 的 `AgentOperations`（写前重读、乐观更新、失败回滚）。本车道的按配对缓存并入：`AgentOperationsState` 先画 `AgentsPreferences.hubCache` 里上次的结果，再以 hub 为准 |
| 空间菜单 | 全部标为已读、置顶 / 屏蔽 / 重命名空间 | 用 r2-ag-ops 的 `AgentSpaceMenu`，补上「全部标为已读」 |
| 定时任务 | Agents 菜单进入的列表和编辑 | 删除。按 `docs/archive/redesign/ux.md` 第 2 节，定时任务在 Persona tab 的资料页（r2-as-home 已做，还有跟进、运行记录、备注和记忆）；保存前重读整表的做法并入了那边 |
| 分组和空间的纯逻辑 | 抽出 `AgentSpace`、`agentSpaces`、置顶 / 屏蔽切换 | **保留**在 `core:domain/sort`，r2-ag-ops 的切换改为调用它。屏蔽切换按 iOS 的规则改过：空间已屏蔽时再次屏蔽某个 Agent 记为它自己的屏蔽，取消空间屏蔽后仍然屏蔽 |

本车道为复用已接线的配对连接，给 `InboxAssistantTransport` 加的 `AgentManagementTransport` 一并删除：操作、新建、助手管理各有独立的接口（`AgentOperations`、`AgentCreationService`、`AssistantManagement`），当前已在 App 组合根按同一个配对作用域构造（`OPERATIONS.md`、`CREATION.md`、`feature/assistant/INTEGRATION.md`）。没接线时，相应入口提示尚未接入，不会退回本机置顶或屏蔽。

## 与旧实现的差异（按 spec）

- 工作中的会话优先按 `state_since` 排序，缺失时才用 `updated_at`；旧 `Grouping.kt` 只看更新时间。
- 快捷筛选时组内忽略 rank，同时间追加完整会话标识作比较。
- 活跃空间列表按状态及时间排序，空间详情按标签顺序。空闲空间展开也保持标签顺序。
- 置顶会话按服务端数组顺序单列一次，置顶离线会话仍可见；普通分组继续过滤离线会话。
- `agentRank` 不变：需要你、完成未看、工作中、待唤醒、已屏蔽、已看；屏蔽不降低需要你的优先级。

验证：domain 排序夹具和 `AgentSpaceTest`，feature 的 `AgentsListTest`、`AgentOperationsTest`、`NewAgentViewModelTest`（含空间新标签）。未做截图及真机联调，未连接线上 hub。已读仍沿用本机标记。性能未实测。
