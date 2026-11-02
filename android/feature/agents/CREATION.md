# 新建 Agent 接线

本模块提供列表右上角和空状态入口、机器选择、目录浏览与最近目录、启动器搜索、Agent 类型、权限模式、思考强度和可选首句。创建成功后通过现有 `Navigator` 打开返回的 `ChatRoute`，并刷新列表。选择按 hub 配对作用域和机器保存在 DataStore。

App 已通过 `AgentsWiringModule` 和 `AgentsEntries` 提供 `LocalAgentCreationService`，偏好与操作记录使用当前配对的 `listPreferences`。构造 `HttpAgentCreationService(scope, baseUrl, token, client, isCurrent)`，其中 `scope` 使用当前 `HubScope.path`，地址和 HTTP client 来自当前已验证、已钉扎的连接路由，`isCurrent` 检查配对代次和路由仍有效。每条路由保持实例稳定；换配对或连接路由时更换实例，避免沿用旧凭据和旧地址。未提供服务时入口显示尚未接线的说明。

当前服务端 `POST /api/v1/sessions` 直接调用 `h.startSession`，没有包装 `h.withClientOp("start", h.startSession)`。需要服务端车道补这一处，才能在丢失回执后通过 `GET /api/v1/ops/{id}` 恢复启动结果。不能仅凭全局 `client_ops` 能力位认定启动接口也支持幂等。本车道没有修改服务端。

客户端在 POST 前持久保存 UUID 和完整参数，重复点击、重开页面和进程重启都复用这份记录。重试只查询该 UUID 的回执，不重复 POST；`pending` 和 `unknown` 保持表单锁定，只有确定的失败回执才允许改参数后再创建。旧 hub 查不到回执时保持“待确认”，需要到机器上核对，不提供会再次启动的重发按钮。回执随配对作用域隔离。

现有启动器响应仅包含 Claude 启动函数，默认类型为 Claude Code。接口兼容可选的 `agent_type`，页面只显示机器实际返回的类型，不额外构造 Codex 或 TRAEX 启动命令；这些类型是否可启动由后续服务端能力决定。

验证范围为 `:feature:agents` 编译及新建状态测试、`:core:hub` 传输测试。App 接线已补齐，未做模拟器截图或真实 hub 验收。按钮标签单行，颜色使用 Material 主题，深浅色实际显示待真机验收。
