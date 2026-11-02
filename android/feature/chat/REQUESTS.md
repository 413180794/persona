# 请求草稿和来源

Agents 请求详情和会话面板都使用 `RequestEditor`，不需要修改 App 外壳。Agents 模块增加对 chat 模块的依赖，复用现有交互页和作答规则。

`RequestDraftStore` 通过现有 `ChatLocalStorage` 获取当前配对范围，两个入口共用同一份本地草稿。按配对范围、机器、会话和请求 id 隔离，问题内容或终端 rev 变化时丢弃旧稿；倒计时刷新不丢稿。提交前清除持久草稿，请求撤回和配对切换时也清除。密码请求以及 schema 中标记 `writeOnly` 或 `format: password` 的字段不落盘，退到后台或关闭编辑页后清空。未取得配对范围时仅在当前页面暂存，并显示提示。

协议只补可选字段，修改范围限于 contracts、domain 和两个 feature：

- `Interaction.agent_id / agent_type`：hook 明确提供的子 Agent 信息，显示类型。
- `Interaction.mcp_server_name / mcp_config_source`：明确提供的 MCP 服务名及配置来源。不解析 `mcp__…` 工具名，没有服务名就不显示来源行。
- `Interaction.elicitation`：包含 `mode`、`message`、`requested_schema` 和可选 `url`。支持 form 和 url 模式。form 模式支持字符串、数字、整数、布尔、单选和多选，以及必填、范围、长度、枚举等校验；不支持的 schema 禁止提交，并提示终端处理。字符串格式支持 email、uri、date、date-time 和 password。
- `Resolution.action / content`：分别回传 `accept`、`decline`、`cancel`；只有接受表单时带经过类型转换和校验的对象。
- 终端请求的 `tool_use_id`：优先使用请求自带 `tool_input.command`，否则只在该会话已加载记录里按相同 `tool_use_id` 查完整工具输入。显示完整命令以及请求、工具调用标识，不按相邻记录猜测。

本车道没有修改 hub/node，没有新增 WebSocket 事件或声明 elicitation 能力。真实表单下发仍需服务端接 Elicitation hook、按能力协商降级；来源字段和终端请求关联也需上游提供。没有关联标识或对应记录时保留屏幕原文。URL 模式在 App 内显示并打开完整链接；只有成功打开当前请求指定的 HTTP／HTTPS 链接并关闭网页返回后，才能点「已打开，继续」。打开记录按配对和原请求保存在内存中，不写进草稿；替换请求或重启 App 后需重新打开。打开链接不代表授权成功，仍需用户明确继续。

新增两个关键测试覆盖草稿恢复、隔离、替换、敏感字段，以及表单类型转换、三种结局、校验拒绝和精确命令关联。验证命令与结果见车道交付报告。深浅色使用现有主题语义色，本车道未做模拟器截图验收；性能未实测。

Agents 主列表和全部请求列表直接打开完整交互页。当前配对期间观察到的最近请求移除后保留只读快照，不保存答案，不保留密码请求；可从请求历史进入所属会话继续。历史仅保存在内存，不承诺跨进程恢复或服务端全量历史。

会话和 Agents 使用 `RequestExpiry` 统一期限判断与文案，无效期限同样禁止作答。助手的内嵌入口和 App 外壳均保留终端兜底。完成触感仅在前台可见、定位及窗口核对完成且无遮挡时，观察到当前轮从工作中转完成后给一次系统确认触感；不在后台补发，也不对同轮重复状态反馈。
