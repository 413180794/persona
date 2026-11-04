# 风灵月影：防撤回与编辑历史

iPhone 18 Pro / iOS 27，浅色玻璃、深色 BeautifulUI。全部为 `MOCK_LARK_TRAINER=1` 生成的数据，未使用真实聊天截图。

| 场景 | 浅色 | 深色 |
| --- | --- | --- |
| 两个展示开关 | [设置](light/trainer-settings-on.png) | [设置](dark/trainer-settings-on.png) |
| 开启：留存正文与撤回标记 | [聊天](light/trainer-chat-on.png) | [聊天](dark/trainer-chat-on.png) |
| 关闭：正文与引用隐藏 | [聊天](light/trainer-chat-off.png) | [聊天](dark/trainer-chat-off.png) |
| 历史版本及锁定的公开外显 | [历史](light/trainer-edit-history.png) | [历史](dark/trainer-edit-history.png) |

`LarkMessageDisplayFlowTests` 从飞书设置进入、开关打开与关闭、重启进入聊天；历史面板保持打开时从 mock 改设置，验证立即隐藏；防撤回变化时同时检查正文和引用。第二轮增加长按留存消息无回复/表情操作，以及当前聊天收到撤回事件立即隐藏正文。最终复验增加已撤回系统消息：保持居中提示，不展示其原文；两个主题均完整通过。

没有发送者的系统撤回提示使用「消息已撤回」，不尝试显示原文；浅深色回归同时覆盖有名和匿名系统消息。

账号冲突、只读连接、换 Hub、离线和旧缓存清理通过单元/接口测试验证；不将这些说成真实账号写入测试。媒体、发件箱仍沿用已有实现。

合并主分支交互式引用卡片后，浅色玻璃与深色 BeautifulUI 再次完整通过；截图已更新为共享 `QuoteCard` 的外观。引用保留点击定位，关闭防撤回时标题仍显示发送者，摘要变为「消息已撤回」。
