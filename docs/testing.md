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

## UI 测试（`scripts/ui-test.sh`）

`ios/JarvisUITests` 在模拟器里驱动真实 App、连配置里的 hub（所以不在默认的单元测试里跑）：
`SIM=<udid> ./scripts/ui-test.sh [machine/session]`。目前有边缘右滑返回（顶栏展开 / 收起两种状态）。
写 UI 测试时先在没有修复的代码上跑一次确认它会失败，再上修复。

## 卡顿测量（掉帧统计）

xctrace 在这台 Mac 上会卡在授权上，改用 App 自己统计：`-showAutoScroll YES`（DEBUG）打开后 12 秒开始，
在对话里以快速轻扫的速度（3000pt / 2.5s）往历史翻 4 段、再翻回 4 段，`FrameStats` 记下每帧间隔，写到
App 数据目录的 `Documents/frames.json`：卡顿次数（晚于半帧）、每秒卡顿毫秒（hitch ratio）、p50 / p95 / p99、最长一帧。

- 用开了优化的 Debug 构建量（`SWIFT_OPTIMIZATION_LEVEL=-O SWIFT_COMPILATION_MODE=wholemodule`，单独的 derivedData），
  Debug 默认 -Onone 的数字没有意义；对照组用 `git worktree` 在旧提交上加同样的统计代码。
- 打开长会话并 `-loadAll YES`；每个版本至少跑 3 次（模拟器波动不小）。
- 看主线程在忙什么：跑的时候 `sample <pid> 18 1 -file out.txt`（系统自带，不需要 Instruments 授权），按「Main Thread」那一段统计。

2026-10-26 一轮优化的结果（长会话 `codex 需求 leader`，3 次）：每秒卡顿 149–245ms → 156–171ms，最长一帧 0.5–2.4s → 0.5–0.9s，
主线程上我们自己代码的采样从 ~1,900 降到 ~400；剩下的是 SwiftUI / CoreText 给新出现的行量文字、画字形。
