# 飞书多账号

关联 #1321 / #1399。全部截图来自隔离 mock（`MOCK_LARK_ACCOUNTS=1`，虚构的账号和会话），不包含真实飞书内容。

2026-11-04 在 iPhone 18 Pro / iOS 27.2 模拟器上，浅色 / 深色 × 默认（液态玻璃）/ BeautifulUI 四轮，每轮 `LarkAccountsFlowTests` 两个流程都通过，截图逐张查看。

| 截图 | 检查点 |
|---|---|
| `accounts-own` | 「消息」页顶部一排账号胶囊（带各账号未读数），选中默认账号「我」时列出它的会话；未登录的账号不出现 |
| `accounts-second` | 切到「工作号」：只列出工作号自己的会话和置顶，默认账号的会话不混入 |
| `accounts-second-chat` | 打开工作号的会话：消息按工作号读取，副标题「飞书 · 工作号」标明以哪个身份发送 |
| `accounts-settings` | 设置 › 连接器 › 飞书 的「账号」：默认账号标记、未读数、未登录的账号标红 |

复现（替换设备 ID，先生成本机配置和 Xcode 项目）：

```sh
MOCK_LARK_ACCOUNTS=1 PORT=<端口> python3 scripts/mock/mock_hub.py
xcrun simctl ui <simulator-id> appearance light      # 或 dark
TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:<端口> \
TEST_RUNNER_JARVIS_SHOTS=/tmp/lark-accounts \
TEST_RUNNER_JARVIS_UI_STYLE=beautiful \               # 默认风格时去掉这一行
xcodebuild test -project apple/Persona.xcodeproj -scheme Persona \
  -destination 'platform=iOS Simulator,id=<simulator-id>' \
  -only-testing:PersonaUITests/LarkAccountsFlowTests
```
