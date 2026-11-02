# 配对作用域存储接线

`HubDataStore.restore()` 恢复同一个 `ScopedHubData` 作用域，`pair()` 每次生成新代次。`close()` 只关闭资源；`unpair()`、换 hub 和再次配对会先关闭旧资源，再删除旧作用域在 files、cache、no-backup 下的整个目录。中断的删除由已有 tombstone 在下次启动时继续完成。

| 入口 | 用途 |
| --- | --- |
| `scope.path` | `hub_id/配对代次`，作为传输层的本地 scope 标识 |
| `items` | 共用的对话 items 缓存库 |
| `cacheDirectory`、`imageCacheDirectory`、`inboxMediaDirectory` | 可重建缓存、聊天图片、收件箱附件 |
| `filesDirectory`、`noBackupDirectory` | 同作用域的其他文件；敏感内容优先放 no-backup |
| `listPreferences` | 列表分组、筛选、置顶及已看标记等偏好 DataStore |
| `readingPositions` | 阅读位置 DataStore |
| `backgroundChecks` | 后台自检记录 DataStore |
| `inboxReplies` | 收件箱与飞书共用回复 journal，只接受当前 `scope.path` |
| `coroutineScope` | 配对期间的任务生命周期，删除旧目录前取消并等待结束 |

当前 App 已通过 `ConnectionRuntime`、`ChatDataModule` 和 `InboxModule` 传递同一 `ScopedHubData`；聊天仓储、草稿、图片、Agents 偏好、回复 journal 与媒体缓存均使用当前配对作用域，仓储任务随配对取消。准备清单等兼容全局记录由 `PairingCleanup` 在退出时清理。

剩余边界：外部 shell 导航偏好尚未随退出删除；飞书锚点目录虽已在退出时删除，`LarkReadingStore` 的进程级写入仍未随配对取消，存在迟到重建风险。后台记录继续分布在作用域与兼容入口中，后续需统一存储分级。

目录 getter 不会创建目录，写文件的一方按需创建，并在配对的 `coroutineScope` 中完成工作。DataStore 由作用域持有，调用方不要对同一路径另建实例。回复 journal 在关闭后拒绝读写，附件落盘与作用域关闭同步，避免迟到的回调重新创建旧目录。

旧构造仍供兼容和体验模式使用。`PairingCleanup` 已一次性清理无可靠配对代次的旧图片、偏好、后台记录及 `chat-data`、`inbox-downloads`、`inbox-replies` 等目录，不能把它们认作当前作用域数据。

验证入口为 `:core:data:testDebugUnitTest` 和 `:core:data:connectedDebugAndroidTest`。两个设备测试覆盖正常关闭后恢复、同一 hub 两次配对清掉旧文件，以及冷启动尚未打开作用域时直接退出清理。
