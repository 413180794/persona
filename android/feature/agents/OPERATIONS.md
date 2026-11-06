# 会话与空间操作接线

当前操作接口和实现已通过 App 组合根接通，以下说明以已合入的接线为准。

App 已由 `di/AgentsWiringModule.kt` 和 `AgentsEntries.kt` 提供 `LocalAgentsOperations`：

- 按当前配对作用域创建 `HttpAgentOperations(scope, baseUrl, token, client, isCurrent)`。`client` 使用现有 `HubHttpClient` 的客户端，地址使用当前选路结果；`isCurrent` 在切换配对后返回 false，防止旧请求更新新作用域。
- 提供 `AgentsOperationsBindings(operations, assistant, onSessionClosed)`。`assistant` 使用已有 `AssistantConversations`；会话请求继续使用现有 `MessageSender`，不另建发送队列。
- `onSessionClosed` 在关闭已确认后触发；App 已撤回对应通知；其他返回栈中已打开会话的主动修正仍需补齐。模块内会刷新会话和交互仓储，并同步列表显示。
- 按配对作用域重建页面和依赖；模块用 `operations.scope` 区分 ViewModel。外壳已按配对 identity 重建导航及页面作用域。
- 「交给 Persona」注入共享 `MainConversationSender`，每次交接分配操作 ID，失败重试复用原 ID；入队后选择 Persona tab，等 outbox 绑定真实目标才以助手身份导航。交接完成后再次操作分配新 ID，不提前刷新或创建助手会话。

未接线时，写操作会提示「会话操作尚未接入」，不会退回本机置顶或屏蔽。列表的已读标记仍沿用原来的本机实现。

空间改名使用 workspace ID；置顶和屏蔽使用「机器 ID／空间名」。改名时同步更新本页引用，hub 负责迁移持久设置。写入前重新读取置顶和屏蔽文档，保留其他条目及 `allowed` 例外；服务端接口是整表替换，不提供跨设备并发版本校验。

停止循环沿用现有客户端协议，通过 `MessageSender` 发送停止指令，不提前清除循环状态。文案只确认已提交请求，实际停止由后续 hub 状态确认。

确定失败会恢复操作前显示。连接中断或服务端错误造成结果未知时，恢复原显示并标记待确认，禁止重复同类改名或关闭；下拉刷新读到目标状态后解除。旧 hub 无操作结果查询时，若始终读不到目标状态，需要在电脑端核对，本页不会自行重发。

界面使用 Material 3 主题色，按钮文案均为单行。本轮按快速模式仅做相关模块编译与两条关键测试，未做截图、真机或线上 hub 验证；性能未实测。

本轮 App 接线补充：关闭成功后按会话撤回通知；屏蔽与置顶写入成功后直接发布到共享偏好仓储，使收件箱同步过滤。Agents 草稿读取当前配对的同一份 DraftStore，待发状态读取共享 outbox。现有草稿与 outbox 契约没有附件字段，附件展示暂缺数据源。新建偏好和操作记录使用配对 DataStore，随配对清除。
