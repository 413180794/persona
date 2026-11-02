# 会话状态浮层接线

对应 UX 3.3「会话状态浮层」。浮层从 `ChatStatusPanel.kt` 移到 `ChatStatusSheet.kt`，沿用页面已有入口和已读遮挡门禁。`ChatScreen.kt` 只增加 `statusBindings` 参数及传递，不修改 `ChatChrome*.kt`、Agents 页面或 `android/app/`。

人工确认继续调用现有 `ChatDeliveryActions` 和 `MessageSender.confirm`，使用原消息标识。确认没送达时，把原文及图片追加回当前输入，不覆盖后来写的草稿，不自动重发。自动核对说明直接取发件箱状态，不编造核对次数或耗时。

当前 `ChatWiringModule` 已在配对作用域提供 `HttpChatRecoveryService(activeConnection)`；`ShellChat` 和默认 `ChatDestination` 均传入 `ChatStatusBindings`，复用活动连接的鉴权与选路，草稿读写错误来自同一 `ChatDraftEditor`。

恢复调用已有会话创建接口，携带原目录、启动器和 `resume_session_id`，不发送消息。结果未知或请求被取消后，同一实例只读取会话列表核对，不重复创建。打开新会话和已恢复会话沿用 `chrome.onOpenSession`。未提供服务、原目录或启动器时禁用恢复，保留复制恢复命令的入口；助手和子 Agent 不执行普通 Agent 恢复。

重连调用当前电脑的既有重连接口，成功提示只表示请求已发出。再次操作刷新会话与电脑列表，不把 HTTP 成功当作电脑已经上线。上次更新时间取电脑的 `lastSeen`，缺失时明确显示暂无记录。

`draftSaveError` 由草稿持久化方传入面向用户的失败原因，保存成功后清除。不要传原始异常、路径或凭据。即使普通状态行已收起，保存失败仍有可点入口。当前 App 已接入这个参数，普通及助手内嵌聊天可显示保存错误。

所有新按钮复用设计系统单行组件，卡片使用主题表面色。未做真机深浅色截图，也未连接真实 hub。恢复请求的防重状态只保存在配对实例内，进程重启后的持久操作日志仍需数据层接入；性能未实测。

已通过 `ChatStatusRecoveryTest`、`ChatDeliveryStateTest` 和 `HttpChatRecoveryServiceTest`，共四个测试用例；`:app:assembleDebug` 构建成功。测试覆盖并发操作门禁、恢复回执丢失后只读核对、重连目标路径，以及人工确认后保留原输入。
