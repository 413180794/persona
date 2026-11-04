# 飞书首页置顶会话

关联 #949 第一阶段。全部截图来自隔离 mock（虚构会话和账号），不包含真实飞书内容。

2026-11-04 在 iPhone 18 Pro / iOS 27 模拟器、BeautifulUI 下完成浅色和深色两轮。每轮 4 个 `LarkPinnedShelfFlowTests` 流程通过，截图逐张查看；深色另外 3 个 `LarkEntryTests` 验证设置入口、开启/关闭、共享聊天跳转和重启。Xcode 26 真机签名构建通过。

| 截图 | 检查点 |
|---|---|
| `pins-compact` / `pins-expanded` | 默认一排、展开完整集合、头像与未读、按钮不换行 |
| `pins-return-collapsed` / `pins-chat-return` | 上滑越过置顶后自动收起，立即回滑仍收起；共享聊天右滑返回 |
| `pins-single` / `pins-empty` | 单个不出现展开，无置顶不留空白 |
| `pins-live-reordered` | Socket 推动顺序与未读更新，无手动刷新 |
| `pins-offline-cache` / `pins-new-account` | 离线重启先显缓存；换号清掉旧账号 |
| `pins-large-*` | 最大辅助字体、80 个置顶、多屏滚动；顶部入口仍可操作 |
| `pins-reader-before` / `pins-reader-after` | 移除置顶、顶部插入新会话后，阅读行的位置相差小于 2 pt |

展开后再次下拉不增加读取请求，也不显示“松开刷新消息”。默认字号会话行高 64 pt。最大辅助字号下，既有普通会话行仍会截短标题/摘要，这是原有列表布局限制；本次置顶区和顶部切换按钮均可触达。

复现（替换设备 ID，先生成本机配置和 Xcode 项目）：

```sh
MOCK_LARK_PINS=1 PORT=18826 python3 scripts/mock/mock_hub.py
xcrun simctl ui <simulator-id> appearance light
TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:18826 \
TEST_RUNNER_JARVIS_UI_STYLE=beautiful \
TEST_RUNNER_JARVIS_SHOTS=/tmp/pins-light \
xcodebuild -project apple/Persona.xcodeproj -scheme Persona \
  -destination 'platform=iOS Simulator,id=<simulator-id>' \
  -parallel-testing-enabled NO \
  -only-testing:PersonaTests/LarkConversationsTests \
  -only-testing:PersonaTests/LarkPinnedInteractionTests \
  -only-testing:PersonaUITests/LarkPinnedShelfFlowTests test
```

切到 `appearance dark`、更换截图目录后再跑一轮。真实飞书发送、已读、置顶编辑均未用于这些回归。

账号绑定快照需要新 Hub；旧 Hub 不返回账号标识时显示更新提示，不将无归属数据写入缓存。升级旧客户端仍沿用原接口契约。聊天内 Pin 属于后续独立迁移，不在本次验收范围。
