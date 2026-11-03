# 首页消息开关

iPhone 18 Pro / iOS 27；全部为 mock Hub 数据。开启飞书设置里的「首页显示消息」后，首页增加「消息」页；关闭后恢复原来的两页。

| 页面 | 浅色液态玻璃 | 深色 BeautifulUI |
| --- | --- | --- |
| 飞书设置开关 | [截图](light/native-feishu-settings.png) | [截图](dark/native-feishu-settings.png) |
| 首页消息列表 | [截图](light/native-feishu-list.png) | [截图](dark/native-feishu-list.png) |
| 共用聊天页 | [截图](light/native-feishu-chat.png) | [截图](dark/native-feishu-chat.png) |
| 关闭后首页 | [截图](light/native-feishu-disabled.png) | [截图](dark/native-feishu-disabled.png) |

`LarkEntryTests` 验证开关持久化、聊天右滑返回、相邻页横滑与关闭当前消息页后设置仍可继续操作；`lark-connectorShellTests` 验证分页手势边界；缓存与分页由 `LarkConversationsTests` 覆盖。

2026-11-03 适配主线 Persona 目录后重新回归，截图来自 #530 + #566 + #713 + #764 组合分支。补充从消息行中部右滑回 Agents，再左滑回消息的检查；三页导航的待处理角标移至标签上方，避开文字与相邻页边界。组合分支 672 项单元测试 / 116 个 suite 通过，会话分页、重连取消和壳层相关 16 项定向测试通过。
