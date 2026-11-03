# 新建 Agent 接线

本模块提供列表右上角和空状态入口、机器选择、目录浏览与最近目录、启动器搜索、Agent 类型、权限模式、思考强度和可选首句。创建成功后通过现有 `Navigator` 打开返回的 `ChatRoute`，并刷新列表。选择按 hub 配对作用域和机器保存在 DataStore。

App 已通过 `AgentsWiringModule` 和 `AgentsEntries` 提供 `LocalAgentCreationService`，偏好与操作记录使用当前配对的 `listPreferences`。构造 `HttpAgentCreationService(scope, baseUrl, token, client, isCurrent)`，其中 `scope` 使用当前 `HubScope.path`，地址和 HTTP client 来自当前已验证、已钉扎的连接路由，`isCurrent` 检查配对代次和路由仍有效。每条路由保持实例稳定；换配对或连接路由时更换实例，避免沿用旧凭据和旧地址。未提供服务时入口显示尚未接线的说明。

当前服务端 `POST /api/v1/sessions` 直接调用 `h.startSession`，没有包装 `h.withClientOp("start", h.startSession)`。需要服务端车道补这一处，才能在丢失回执后通过 `GET /api/v1/ops/{id}` 恢复启动结果。不能仅凭全局 `client_ops` 能力位认定启动接口也支持幂等。本车道没有修改服务端。

客户端在 POST 前持久保存 UUID 和完整参数，重复点击、重开页面和进程重启都复用这份记录。重试只查询该 UUID 的回执，不重复 POST；`pending` 和 `unknown` 保持表单锁定，只有确定的失败回执才允许改参数后再创建。旧 hub 查不到回执时保持“待确认”，需要到机器上核对，不提供会再次启动的重发按钮。回执随配对作用域隔离。

现有启动器响应仅包含 Claude 启动函数，默认类型为 Claude Code。接口兼容可选的 `agent_type`，页面只显示机器实际返回的类型，不额外构造 Codex 或 TRAEX 启动命令；这些类型是否可启动由后续服务端能力决定。

验证范围为 `:feature:agents` 编译及新建状态测试、`:core:hub` 传输测试。App 接线已补齐，未做模拟器截图或真实 hub 验收。按钮标签单行，颜色使用 Material 主题，深浅色实际显示待真机验收。

## 异步新建与跟随模型配置（2026-11-02）

- Claude 的新建带 `async: true`：hub 回 `starting` 的会话后先 `SessionsRepository.adopt` 并进列表、马上打开对话；第一条消息不进 `prompt`（改带 `name_hint`），作为普通消息经 `MessageSender` 入队，它的 `client_msg_id` 在新建前和操作 ID 一起写进启动记录（`AgentStartOperation.firstMessageId`），核对回执、进程重启后入队用的都是同一个 id，所以只发一次；入队失败时启动记录保留，「重试核对结果」再入队一次。Codex 不走异步，第一条消息照旧放 `prompt`（必填）。没有 `MessageSender` 时（单测）也照旧同步。
- `AgentCreationService.newAgentModel()` 读 hub 的 `agent.new` 用途：有的话默认「跟随模型配置」（空 launcher），记住的启动器不再自动带上；手选一个启动器是本次覆盖。旧 hub（404）照旧默认记住的或第一个启动器；读失败时不预选，要手选或重新加载。
- `NewAgentRoute` 打开同一张表单的页面版（`NewAgentDestination`），预填机器、目录（那台机器在线才选中）和第一条消息；开好后先清掉启动记录再离开页面（页面出栈会清掉它的 ViewModel 作用域）。
