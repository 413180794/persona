# 体验模式

`DemoHub` 实现现有的 `ChatTransport`、`MessageTransport`、`InboxAssistantTransport` 和 `HubConnection`，供正式数据仓库使用。页面不用判断体验模式，模块没有网络客户端，也不接受真实传输对象或凭据。

数据来自 `scripts/mock/mock_hub.py` 经 `scripts/mock/make_demo_data.py` 导出的 iOS `Demo.json`。Android 副本保留数据结构，把示例姓名改为角色称呼，地址统一换成 `example.com`。更新已有共享导出后，在仓库根目录运行：

```sh
python3 android/core/demo/tools/export_data.py
```

这一步只读 iOS 文件，只写本模块的 `src/main/assets/demo/Demo.json`。载入时递归换算 `@rel:` 和 `@unix:`，可注入时钟供固定截图使用。每次进入都新建实例、生成独立的 `HubScope`，重建后恢复出厂演示数据。

Agent 和助手消息仅在内存回显并追加体验说明；同一消息标识重复发送不会重复追加。回答交互仅移除本地卡片、更新演示对话。飞书、邮件回复和标记已处理返回 `HubFailure`，其中 `code=demo`、`delivered=no`、`retryable=false`，防止页面误报成功。所有这些操作都会向 `notices` 发出「体验模式下不会真的发送」。未支持的 socket 命令一律拒绝；没有数据的读取返回本地 404，不回退真实网络。媒体接口目前没有飞书媒体素材。

## 后续接线

- **入口和路由**：配对页「没有 hub？先体验一下」调用 `DemoHub.fromAssets(assets, scope)`，进入现有 `AssistantRoute`／`AppTab.Persona`。其余页面继续用 `AgentsRoute`、`InboxRoute`、`SettingsRoute` 和 `ChatRoute`，无需新增体验专用路由。
- **DI**：同一实例绑定上述四个接口；`InboxAssistantConnection` 绑定 `demo.inboxConnection`。继续使用 `HubSessionsRepository`、`HubChatRepository`、`HubInteractionCenter`、`RealAssistantRepository`、`RealInboxRepository`。注册事件订阅后可调用 `reconnect()` 发布初始快照。
- **缓存和发送**：仓库缓存、草稿、收件箱日志和 outbox 必须使用 `demo.hubScope` 的独立存储，发送用这个 scope 构造 `MessageRequest`。不能调用真实配对流程写入演示凭据，也不能复用真实 hub 的缓存和待发消息。退出时关闭演示实例及其仓库作用域、清理演示存储。
- **提示和退出**：应用根部单独收集 `notices`，通过设计系统的提示色 Snackbar 展示；统一错误出口把 `code=demo` 识别为提示，避免再弹红色错误。设置页增加体验标识、「连接我的 hub」和「退出体验模式」；退出回配对页，配对成功关闭演示实例。只持久化是否正在体验，重启时新建实例。
- **设备服务**：体验期间不启动真实连接、通知注册、手机工具或设备凭据同步。其他 A3 模块若新增传输接口，需要在这个实例上补对应本地适配，不能把请求交给真实传输。

本车道没有改 `android/app/`，也没有新增界面。上述入口、DI、存储切换和提示展示留给集成车道；接线时按钮标签保持单行，提示使用设计系统的深浅色语义色。

## 验证

```sh
cd android
./gradlew :core:demo:testDebugUnitTest :core:demo:assembleDebug -Pkotlin.compiler.execution.strategy=in-process
```

两条关键路径测试覆盖实际 assets 解码与相对时间、本地消息去重、交互回显、连接事件、退出重建、双向分页、scope 隔离，以及外部操作拒绝和提示。进程内编译用于避开当前沙箱不可写的 Kotlin daemon 目录。
