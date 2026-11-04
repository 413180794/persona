# 聊天内 Pin 消息

验证使用 `MOCK_LARK_CHAT_PINS=1` 的隔离 mock 与 `LarkChatPinFlowTests`。不读取真实账号的 Pin、不发送真实消息、不修改真实 Pin。

场景：聊天打开不预取 Pin；列表分页、加密正文与撤回占位、权限隐藏；关闭保留聊天位置；普通消息和话题回复定位；空列表、离线保留、旧 Hub、旧 lark-connector、账号切换；分页失败重试、定位失败；添加/移除确认、未知写回执只查列表不重发；最大辅助字号。

2026-11-04：iPhone 18 Pro / iOS 27 模拟器，默认玻璃风格，浅色、深色各完整运行 5 个 UI 测试，均通过。每轮 16 张截图逐张检查；下表保留主要画面。已修正空占位重复文案和最大辅助字号下的头像文字溢出。

| 场景 | 浅色 | 深色 |
| --- | --- | --- |
| 列表、加密摘要、撤回占位与权限 | [截图](light-list.png) | [截图](dark-list.png) |
| 定位话题回复 | [截图](light-thread-target.png) | [截图](dark-thread-target.png) |
| 添加确认 | [截图](light-add-confirm.png) | [截图](dark-add-confirm.png) |
| 写入结果未知，不自动重发 | [截图](light-lost-receipt.png) | [截图](dark-lost-receipt.png) |
| Hub 缺少接口 | [截图](light-upgrade-hub.png) | [截图](dark-upgrade-hub.png) |
| lark-connector 缺少接口 | [截图](light-upgrade-lark-connector.png) | [截图](dark-upgrade-lark-connector.png) |
| 最大辅助字号 | [截图](light-large.png) | [截图](dark-large.png) |

同轮还验证了空列表、移除确认、普通聊天定位、关闭后滚动位置不变、自动分页、分页失败重试、离线保留旧列表、账号切换清空、定位失败保留当前聊天。

模型测试覆盖分页去重、游标循环、完整页原子替换、实时刷新合并、账号隔离、页面关闭后的迟到结果、未知写回执不重试和 OLK 正文编码脱敏。服务端测试覆盖角色权限、写前权限复核、Pin/消息/会话关联、账号切换、no-store、正常/话题定位以及资源不存在与旧接口的区别。

复现：用 `MOCK_LARK_CHAT_PINS=1` 启动 `scripts/mock/mock_hub.py`，将 `TEST_RUNNER_JARVIS_MOCK_HUB` 指向该隔离实例，再运行 `PersonaUITests/LarkChatPinFlowTests`。可用 `TEST_RUNNER_JARVIS_SHOTS` 指定截图目录；使用 `simctl ui <模拟器> appearance light|dark` 分别运行。

lark-connector 读取聊天 Pin 会清除该聊天的未读 Pin 标记，因此真实账号未用这条接口做冒烟；真实消息、Pin 写入及已读行为未执行。本目录均为合成数据，不包含真实聊天内容。
