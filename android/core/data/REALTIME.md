DATA-05 续跑报告。依据为 `docs/archive/android/android-p0-status.md` 的 DATA-05、7.9、N14，`android.md` 4、5.2–5.5，`ux.md` 3.2、3.3、5.1、5.4、5.6，以及 Android 对话原型。

保留前次实现，补完 `HubChatRepository.kt`、`HubSessionsRepository.kt` 和 `ChatRealtimeState.kt`。delta 按整段草稿更新，空串结束后保留文字，等最终记录接替；接替时保留展示 ID，全文请求和已读回执转换回真实 ID。临时文字与屏幕行不进入缓存或连续性游标。屏幕行原位更新，清空、轮次结束或新记录到来时撤下。工具事件按调用 ID 更新已有工具行，也能等待稍后到达的记录；没有会话身份且无法匹配的调用不凭空生成行。错误只写入对应会话。

会话列表与机器列表的权威快照共同驱动结束和替换状态。磁盘缓存、机器离线、连接中断和历史重置都不作为结束证据；重连需要重新核对。成功中断后进入 stopping，离开工作状态或收到结束事件后清除。

App 接线仍由协调者完成：用同一个 `data.chats` 调用 `trackActions` 和 `trackComposer`，将包装后的服务分别传给 `ChatChromeBindings.actions` 与 `ChatScreen.composerService`。底层服务继续共用配对作用域，outbox 继续使用原传输实例。本车道没有修改 `android/app/`。

`turnDurationSeconds` 仅由 `recordTurnDuration` 接收真实记录；当前协议没有轮次耗时字段，默认留空，不从消息时间戳估算。旧 hub 没有 screen 事件时维持原行为。未连接 hub、未做真机与视觉验收，性能未实测。

`ChatRepositoriesTest.kt` 新增两个关键路径测试，覆盖流式接替的两种到达顺序、稳定 ID、缓存与已读门禁、屏幕及工具更新、会话错误隔离、历史重置，以及中断、离线、重连和同窗格替换。验证命令在 `android/` 执行：

```sh
./gradlew -Pkotlin.compiler.execution.strategy=in-process --no-watch-fs :core:data:testDebugUnitTest --tests persona.core.data.ChatRepositoriesTest :app:assembleDebug
```

测试与 APK 构建通过，定向测试共五个。首轮发现并修正跨模块属性的 Kotlin 智能转换错误；编译改用进程内模式，避免沙箱外的 Kotlin daemon 标记文件写入。改动限于对话仓储、测试及本报告，收件箱文件未改。
