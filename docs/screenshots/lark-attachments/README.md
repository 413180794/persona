# 飞书附件回归（#410）

iPhone 18 Pro，iOS 27。仅 mock Hub / 模拟会话；未向真实账号发送消息。
浅色为液态玻璃，深色为 BeautifulUI。重复出现的同名文件、相同图片来自不同轮次测试，不是单次发送的重复回显。

| 路径 | 浅色 | 深色 |
| --- | --- | --- |
| 发送前图片预览、移除 | [草稿](light/lark-photo-draft.png) | [草稿](dark/lark-photo-draft.png) |
| 图片全屏、下拉关闭 | [预览](light/lark-photo-preview.png) | [预览](dark/lark-photo-preview.png) |
| 图片发送中 | [发送中](light/lark-photo-sending.png) | [发送中](dark/lark-photo-sending.png) |
| 回执确认后替换本机消息 | [已确认](light/lark-photo-sent.png) | [已确认](dark/lark-photo-sent.png) |
| 文件预览、关闭后回到输入框 | [预览](light/lark-file-preview.png) | [预览](dark/lark-file-preview.png) |
| 发送失败仍保留文件 | [失败](light/lark-file-failed.png) | [失败](dark/lark-file-failed.png) |
| 退出聊天再进入、重试成功 | [重试](light/lark-file-retried.png) | [重试](dark/lark-file-retried.png) |

自动化用例：`LarkAttachmentFlowTests` 与主线 `LarkSendTests` 共 10 条流程，覆盖原有文字、引用、话题、草稿恢复、空输入框隐藏发送按钮和气泡旁发送标记。核心数据边界：`LarkAttachmentTests` 和服务端 attachment tests。
文件预览使用系统 Quick Look，测试先等待内容加载完成，再断言关闭成功后操作发送按钮。
发送状态回归可用 `MOCK_LARK_SEND_SECONDS=4` 启动 mock：构建负载下 XCTest 的首次快照可能晚于默认 2 秒回执，导致错过发送中的状态。固定 mock 延迟，不改变 App 的发送行为或断言。

边界：照片转换为 JPEG（最长边 2560 像素），原图或动图可作为文件发送；每批最多 4 个附件，文件最大 100 MiB。
失败重试保留同一 client_id 和私有副本；本阶段离开聊天后仅保留已进入发件箱的内容，尚未发送的附件草稿仅在当前页面保留。文字和引用草稿沿用主线持久化，跨页面附件草稿及重启恢复由 #713 提供。

2026-11-03 主线更名适配后，在 #530 + #566 + #713 + #764 的组合分支重新验证附件流程，更新本页深浅色截图。`PersonaTests` 全量 672 项 / 116 个 suite 通过；独立附件分支的相关单元测试 20 项通过。上游新增的 @ 提及、加载状态、已读回执继续保留。

2026-11-03 合入前复核：附件分支同步 `7d9035e9` 主线，相关单元测试 34 项通过；浅色液态玻璃和深色 BeautifulUI 各 3 条附件流程通过，复核发送回显、预览下拉关闭、失败返回重试与到底位置。截图仍使用上方已有的同流程记录，最新复核截图保存在本机测试产物中。
