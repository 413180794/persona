# 测试

## 命令

```sh
cd server && go test ./... && go vet ./...                    # 服务端
cd ios && xcodegen generate -q && xcodebuild -project Jarvis.xcodeproj -scheme Jarvis \
  -destination 'platform=iOS Simulator,name=iPhone 17 Pro' test   # iOS 单测（ChatStore、ScreenParser、手机工具注册表）
./scripts/sim.sh [-openSession example-machine/<id>]                    # 模拟器里跑，自动配对真实 hub
```

transcript 解析用真实样本做 golden 测试（`server/internal/transcript/testdata/`，`fixtures_test.go`）：每份样本的解析结果、
「边写边读 = 一次读完」、「往前翻页不漏不重」。改了解析跑 `go test ./internal/transcript -run Fixture -update`，把 golden 的 diff 当评审看。
加样本前先 `scripts/redact_transcript.py` 脱敏、人工复核（`testdata/README.md`）；Claude Code / Codex 升级后跑
`scripts/transcript_stats.py` 看有没有解析器不认识的新类型。

接口冒烟：`curl -H "Authorization: Bearer $TOKEN" …/api/v1/machines`、`…/api/v1/sessions?machine_id=example-machine`。

## 端到端（2026-10-24 实测通过）

| 场景 | 方法 | 结果 |
| --- | --- | --- |
| 手机新建会话 | `POST /api/v1/sessions` 在 example-machine `~/jarvis-test` 用 cc_ds 启动 | 自动通过信任目录、首条消息送达 |
| 提问 | 首条消息让 claude 调 AskUserQuestion（单题 / 两题含多选） | hub 出现 question 交互，`resolve` 后 claude 拿到答案继续 |
| 计划审批 | ExitPlanMode → deny + 意见 → 改计划 → allow + setMode | 两种分支都对（`docs/claude-integration.md`） |
| 手机工具 | example-machine 的 claude 调 `mcp__jarvis__phone_device_info` | 经 node → hub → 模拟器 App 返回 iPhone 17 Pro / iOS 26.5 |
| 实时流 | App 打开 example-machine 上正在工作的会话 | 工具步骤实时追加，状态条显示 spinner 行 |
| 手表 | 手表模拟器 `-pairURL` | 「需要你」（等你的 Agent + 收件箱里需要你的事）、运行中会话；`-openInboxFirst YES` 打开第一件 |

## 界面检查清单（每次改界面都要截图看）

- 对话列表：浅色 / 深色；有待处理交互、没有会话、连不上 hub 三种状态。
- 对话页：长 markdown（表格、代码块）、折叠的工具组、diff、待办、图片；工作中状态条；提问 / 计划 / 授权 / 终端四种面板。
- 新建对话：机器卡片（离线变灰）、最近目录、目录浏览、启动器搜索。
- 机器、邮件（空状态、列表、详情、写信、添加邮箱）、设置、手机数据授权。
- 手表：列表、交互详情、收件箱详情（AI 回复）、会话详情。

模拟器限制：屏幕锁着时 computer-use 点不了模拟器，靠 DEBUG 启动参数直达页面；推送、HealthKit 真实数据、WatchConnectivity 同步要真机。

## 用 mock 数据截图（README / 宣传图）

截图不能带真实的业务数据：`scripts/mock_hub.py` 是一个只返回编造数据的假 hub（三台机器、几个项目的 Agent、助手对话、
一个提问、一个计划审批、飞书 + 邮件的收件箱），够主要界面用。

```sh
python3 scripts/mock_hub.py &                                  # 127.0.0.1:18760
SIM=$(xcrun simctl create "Jarvis Shots" com.apple.CoreSimulator.SimDeviceType.iPhone-17-Pro <iOS 26 runtime>)
xcrun simctl status_bar $SIM override --time "$(date +%H:%M)" --batteryState charged --batteryLevel 100 --wifiBars 3 --cellularBars 4
xcrun simctl launch --terminate-running-process $SIM <bundle id> \
  -pairURL "jarvis://pair?url=http%3A%2F%2F127.0.0.1%3A18760&token=mock" [-tab dev|mail] [-openSession <machine>/<session>]
```

- **新建一台模拟器**，别用连过真实 hub 的：App 的磁盘缓存（对话、收件箱）会把真实数据带进截图。
- mock 的条目 id 是固定的；改了 mock 数据（或 id）后要卸载重装 App，否则旧缓存和新数据合在一起，每一步出现两次。
- Agent 对话页默认把工作过程折成一行，`-openWork YES` 展开。`-tab` 没有对话 tab，App 会停在上次的 tab：
  要拍对话 tab 就先卸载重装（新装默认在对话 tab）。
- 首页问候按模拟器的真实时间（「晚上好」），状态栏时间也用当前时间，别固定成 9:41。
- 深色：`xcrun simctl ui $SIM appearance dark`。

## UI 测试（`scripts/ui-test.sh`）

`ios/JarvisUITests` 在模拟器里驱动真实 App、连配置里的 hub（所以不在默认的单元测试里跑）：
`SIM=<udid> ./scripts/ui-test.sh [machine/session]`。目前有边缘右滑返回（顶栏展开 / 收起两种状态）。
长按消息的操作条（`MessageMenuTests`，按 0.4 / 0.8 / 1.5 / 2.5 秒松手都要留下，点列表、滑动会关）连 `scripts/mock_hub.py`，不用真实 hub：
`TEST_RUNNER_JARVIS_MOCK_URL=http://127.0.0.1:18760 xcodebuild test … -only-testing:JarvisUITests/MessageMenuTests`。
写 UI 测试时先在没有修复的代码上跑一次确认它会失败，再上修复。

通知按钮（`NotificationActionTests`）不连真实 hub，连 `scripts/mock_hub.py`：
`PORT=18791 python3 scripts/mock_hub.py &`，再 `TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:18791 xcodebuild test … -only-testing:JarvisUITests/NotificationActionTests`。
它在 SpringBoard 上长按横幅、点「拒绝」，再读 mock 的 `GET /mock/resolved` 确认 hub 收到了 resolve。模拟器系统语言是英文时授权弹窗的按钮叫「Allow」；
授权会保留到下次，所以 App 要等进了后台才排这条通知，否则它在测试还在等弹窗时就弹过去了。

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
