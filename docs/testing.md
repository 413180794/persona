# 测试

## 命令

```sh
cd server && go test ./... && go vet ./...                    # 服务端
cd ios && xcodegen generate -q && xcodebuild -project Jarvis.xcodeproj -scheme Jarvis \
  -destination 'platform=iOS Simulator,name=iPhone 17 Pro' test   # iOS 单测（ChatStore、ScreenParser、手机工具注册表）
./scripts/sim.sh [-openSession example-machine/<id>]                    # 模拟器里跑，自动配对真实 hub
```

接口冒烟：`curl -H "Authorization: Bearer $TOKEN" …/api/v1/machines`、`…/api/v1/sessions?machine_id=example-machine`。

## 端到端（2026-10-24 实测通过）

| 场景 | 方法 | 结果 |
| --- | --- | --- |
| 手机新建会话 | `POST /api/v1/sessions` 在 example-machine `~/jarvis-test` 用 cc_ds 启动 | 自动通过信任目录、首条消息送达 |
| 提问 | 首条消息让 claude 调 AskUserQuestion（单题 / 两题含多选） | hub 出现 question 交互，`resolve` 后 claude 拿到答案继续 |
| 计划审批 | ExitPlanMode → deny + 意见 → 改计划 → allow + setMode | 两种分支都对（`docs/claude-integration.md`） |
| 手机工具 | example-machine 的 claude 调 `mcp__jarvis__phone_device_info` | 经 node → hub → 模拟器 App 返回 iPhone 17 Pro / iOS 26.5 |
| 实时流 | App 打开 example-machine 上正在工作的会话 | 工具步骤实时追加，状态条显示 spinner 行 |
| 手表 | 手表模拟器 `-pairURL` | 列出三台机器的运行中会话 |

## 界面检查清单（每次改界面都要截图看）

- 对话列表：浅色 / 深色；有待处理交互、没有会话、连不上 hub 三种状态。
- 对话页：长 markdown（表格、代码块）、折叠的工具组、diff、待办、图片；工作中状态条；提问 / 计划 / 授权 / 终端四种面板。
- 新建对话：机器卡片（离线变灰）、最近目录、目录浏览、启动器搜索。
- 机器、邮件（空状态、列表、详情、写信、添加邮箱）、设置、手机数据授权。
- 手表：列表、交互详情、会话详情。

模拟器限制：屏幕锁着时 computer-use 点不了模拟器，靠 DEBUG 启动参数直达页面；推送、HealthKit 真实数据、WatchConnectivity 同步要真机。
