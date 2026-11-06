# 助手模块接线

当前 App 已在 `ChatWiringModule` 注入当前配对的 `AssistantManagement`、DraftStore、在线机器与手机工具可用性；管理请求跟随活动路由并复用钉扎客户端。`AssistantAppWiring` 已把助手样式和内嵌参数传给共用 `ChatDestination`，通知、深链和内部跳转均归 Persona tab。

主动做事（定时任务、跟进、运行记录）在 `proactive/`，入口 `AssistantProactiveScreen`：设置 › 助手（`settings/`）的「主动做事」一节和 Persona 抽屉的「定时任务」都在原地打开它，打开的对话落到 Persona tab。旧的资料页 `AssistantProfile` 已删除，关于我、记忆和在哪运行在 `settings/`。立即运行使用响应中的机器与会话标识；运行记录缺机器标识时回退到助手配置中的机器。

定时任务 PUT 替换整张列表。迁移时保留了 master 测试要求的默认字段编码：`09:00`、`enabled`、空 `weekdays` 必须发送，运行记录等服务端字段不回写。空列表、空关于我和空记忆内容也必须显式发送。

AS-02 首页首句与建议卡已改用 `MainConversationSender.sendToMain`。`AssistantSendingModule` 提供这个 Hilt 接口，复用 `HubDataStore` 当前配对的 `OutboxMessageSender`，与现有分享队列使用同一份 outbox。主对话解析器由现有 `PlatformRuntime` 安装；助手页面不创建自己的解析器，不在首句入队前请求网络。

输入框只在入队成功后清空，失败保留文字与操作 ID。页面从 outbox 读取待发气泡，重新进入后仍可显示；目标从 `PendingMainConversation` 绑定到真实会话时才跳转，不把本地占位目标交给网络或导航。后续重连、主对话创建去重由数据层负责。显式点「新对话」仍使用原有旁支创建接口，旁支内发送保留当前目标。首页通过 `ChatComposer` 和 `ChatDraftEditor` 复用当前配对的附件服务与 DraftStore，照片、拍照和文件选择均已接入，文件路径按当前光标插入。没有主对话时，上传目标取助手配置中的机器，不预先创建会话。已上传附件随首句离线入队，普通及内嵌对话也已持久化；未上传附件的 URI／结构化元数据恢复仍未补齐。

各入口当前接线：

- 分享队列已有 `owner.data.messages.sendToMain`，无需另改。
- Agents 的 `AgentsViewModel.handoff` 已注入同一 `MainConversationSender`，按稳定操作 ID 入队后切到 Persona，绑定真实目标后再进入对话。
- 收件箱及邮件 AI 已由 `RealInboxRepository.askPersona` 整理上下文并调用同一 `sendToMain`，不先取主对话或创建会话。

知识库扩展也已通过稳定操作 ID 调用 `sendToMain`，入队后切回 Persona。直接调用 `AssistantScreen` 的宿主须传入 `mainConversationSender`；默认的 `ConnectedAssistant` 已完成注入。

首页、抽屉、待发页和资料页使用设计系统的 `PersonaTheme`、间距与形状 token；会话状态使用 `StatusLabel` / `StatusLine`，待发状态使用 `PendingBadge`，按钮、卡片和空态使用共用组件。按钮文字单行，状态色通过共用组件的 `statusTone` 适配深浅色；未知运行结果显示「无结果记录」。待发页提供深浅色 Compose Preview。

验证范围为 `:feature:assistant:testDebugUnitTest` 和 `:app:assembleDebug`。新增关键测试覆盖离线首句与外部消息共用队列、重建发送器后只创建一个主对话，以及入队失败保留输入、原 ID 重试和旁支目标不变。没有连接 hub，没有做深浅色截图或真机验收。未做性能实测。
