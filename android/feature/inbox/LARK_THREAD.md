# 飞书话题补齐

消息下方显示 `reply_count` 与 `thread_reply_count` 中较大的回复数，点击后从底部打开话题。浮层复用会话气泡、设计系统按钮与主题语义色；按钮单行。浮层遮住会话时暂停底层可见已读上报，话题消息本身不推进会话已读位置。

`LarkThreadLoader` 首次读取最新一页，按 `older_cursor` 向前翻。已加载内容可上下滚动；点击「最新回复」重新读取末页，替换当前窗口，避免把不连续的两页直接拼接。翻页按消息 ID 去重，保留服务端次序，失败保留当前内容；游标缺失、重复或循环时报错并允许重试。

读取走已有 `GET /api/v1/lark/messages/{id}/thread?before=&limit=`。`LarkThreadSource` 随消息携带绑定原配对的读取能力，穿过现有 App 仓储包装，不需要修改 `android/app/`。为此最小修改了 `core/data/RealInboxRepository.kt` 的消息映射，以及 `core/hub` 的两个传输实现；读取契约独立放在 `core/contracts/LarkThreadTransport.kt`。

## 话题发送

浮层已接入 `POST /api/v1/lark/messages/{id}/replies`，请求带 `thread: true`，hub 转发 `replyMode: "thread"`（不带时是在会话里引用回复），由飞书网关解析原消息的话题归属。`HttpInboxAssistantTransport` 与会话包装传输都提供 `LarkThreadReplyTransport`；本模块新增对 `:core:hub` 的依赖，复用随消息绑定的配对传输，不需要修改 App 或共享契约。

编辑器复用 `ReplyComposer`，按根消息隔离草稿。发送前通过当前配对的草稿存储保存文本和 `client_id`，另存一份待确认请求；这两个存储键只用于本地保存，不调用普通会话发送。丢回执、关闭浮层或进程中断后恢复原请求，锁定编辑，点击「核对结果」以相同目标、文本和 ID 请求 hub 重放回执。`202` 保持待确认，成功后清理对应草稿并重读话题末页。上游错误保留原请求，核对会重放原错误，不会生成新 ID 冒险重发。

话题读取尚不支持向后网络分页：现有 hub thread 路由只读取 `before`，不接收 `after`。当前支持浏览已加载的后续回复，以及重新读取最新页。

## 验证范围

本轮通过 `:feature:inbox:compileDebugKotlin`，以及 `LarkThreadTest` 和 `LarkThreadTransportTest` 两组定向测试，覆盖分页、丢回执恢复、原 ID 核对、待确认回执、路径编码和配对失效。未连接真实 hub，未做深浅色截图或真机联调，性能未实测。

```sh
cd android
./gradlew :feature:inbox:compileDebugKotlin :feature:inbox:testDebugUnitTest --tests persona.feature.inbox.LarkThreadTest :core:hub:test --tests persona.core.hub.LarkThreadTransportTest
```

服务端通过 `go build -buildvcs=false ./...` 和 `go test -count=1 -p 1 ./internal/hub -run 'TestLark(SendClientID|ThreadReplyReceipt)$|TestReplyReceiptsPersist$'`。持久回执测试使用 `PERSONA_TEST_DATABASE_URL`，覆盖模拟 hub 重启后的原结果重放；话题路由测试覆盖上游参数、成功与错误回执重放、目标冲突和空文本拒绝。构建关闭 VCS 版本标记，以适应 worktree 沙箱。
