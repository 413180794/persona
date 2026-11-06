# 顶栏与菜单接线

当前 `ChatWiringModule` 已注入配对作用域的 ChatActions、活动连接和导航回调，操作由同一 `data.chats` 跟踪；普通入口 `ShellChat` 与助手内嵌 `ChatDestination` 均已消费。文件／Git、终端和连接诊断入口也已接通，助手保留隐藏文件／Git／终端的限制。

关闭操作使用已有返回回调；Agents 的关闭回调已撤回会话通知，但其他返回栈内已打开会话的主动修正仍缺。共享仓储继续消费后续会话快照；父会话快照未到时，子 Agent 深链先显示通用标题。

屏蔽采用现有 `/mutes` 整表读写协议，保留空间、其他会话、单独允许通知的会话及未知字段。单实例串行写入；不同客户端同时写整表仍可能互相覆盖，服务端目前没有条件写入契约。

操作回执不明时提供「核对」，重读服务端状态；同类操作未核对前不会重复发送。此保护保存在配对作用域实例中，进程重启后的操作日志持久化需后续数据层统一接入。中断回执成功只表示已发出，不表示工作已经停止。

验证范围为相关模块编译和两个关键路径测试，未连接真实 hub，未做真机深浅色截图，性能未实测。界面使用主题表面色与状态 token，按钮文字单行。模块菜单现已支持文件、Git、终端，入口由 App 按会话类型和可用能力提供。

已通过以下定向验证（在 `android/` 执行）：

```sh
./gradlew :feature:chat:compileDebugKotlin :core:hub:compileKotlin
./gradlew -Pkotlin.compiler.execution.strategy=in-process --no-watch-fs :feature:chat:testDebugUnitTest --tests persona.feature.chat.ChatChromeTest :core:hub:test --tests persona.core.hub.HttpChatActionsTest
```

两个测试分别覆盖空间隔离与命令转义，以及请求路径、屏蔽数据保留、关闭结果核对后不重复提交。中文标点扫描命中零处。
