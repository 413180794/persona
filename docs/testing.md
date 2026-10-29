# 测试

## 命令

```sh
cd server && go test ./... && go vet ./...                    # 服务端
cd ios && xcodegen generate -q && xcodebuild -project Jarvis.xcodeproj -scheme Jarvis \
  -destination 'platform=iOS Simulator,name=iPhone 17 Pro' test   # iOS 单测（ChatStore、ScreenParser、手机工具注册表）
./scripts/sim.sh [-openSession <machine>/<id>]                    # 模拟器里跑，自动配对真实 hub
```

transcript 解析用真实样本做 golden 测试（`server/internal/transcript/testdata/`，`fixtures_test.go`）：每份样本的解析结果、
「边写边读 = 一次读完」、「往前翻页不漏不重」。改了解析跑 `go test ./internal/transcript -run Fixture -update`，把 golden 的 diff 当评审看。
加样本前先 `scripts/redact_transcript.py` 脱敏、人工复核（`testdata/README.md`）；Claude Code / Codex 升级后跑
`scripts/transcript_stats.py` 看有没有解析器不认识的新类型。

接口冒烟：`curl -H "Authorization: Bearer $TOKEN" …/api/v1/machines`、`…/api/v1/sessions?machine_id=<machine>`。

## 端到端（2026-10-24 实测通过）

| 场景 | 方法 | 结果 |
| --- | --- | --- |
| 手机新建会话 | `POST /api/v1/sessions` 在 hub 机器 `~/jarvis-test` 用启动器启动 | 自动通过信任目录、首条消息送达 |
| 提问 | 首条消息让 claude 调 AskUserQuestion（单题 / 两题含多选） | hub 出现 question 交互，`resolve` 后 claude 拿到答案继续 |
| 计划审批 | ExitPlanMode → deny + 意见 → 改计划 → allow + setMode | 两种分支都对（`docs/claude-integration.md`） |
| 手机工具 | hub 机器上的 claude 调 `mcp__jarvis__phone_device_info` | 经 node → hub → 模拟器 App 返回 iPhone 17 Pro / iOS 26.5 |
| 实时流 | App 打开另一台 Mac 上正在工作的会话 | 工具步骤实时追加，状态条显示 spinner 行 |
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
- 截断的长输出：`-openSession macbook/a1f3c2d4-0000-4000-8000-000000000002 -openWork YES -showFullOutput card`（展开「运行」卡片，
  看「已截断，看全文」）或 `sheet`（再打开全文浮层，mock 的 `/items/{id}/full` 回全文）。
- 往上翻时内容不动（`ChatView` 的冻结）：`MOCK_STREAM=1` 启动 mock（第一个会话打开后每 2 秒来一条新步骤 / 回复，共 12 次），
  `-openSession macbook/a1f3c2d4-0000-4000-8000-000000000001 -openWork YES -showFreezeProbe YES`：App 自己往上翻，每 2 秒记一次屏幕中间那行的位置，
  结果写在 App 容器的 `Documents/freeze.txt`（`xcrun simctl get_app_container $SIM <bundle id> data`），最后一行 `RESULT:`，应为移动 0pt。
- 终端断线：`MOCK_SCREEN_FAIL_AFTER=3:14` 启动 mock（第一次读终端 3 秒后每次都 503，再过 14 秒恢复），
  `-openSession macbook/a1f3c2d4-0000-4000-8000-000000000001 -openPane terminal`，应先出现红色「上次更新 N 秒前 · 重试」，恢复后消失。
  mock 进程启动时还没有读终端的请求，计时从第一次请求算：编译期间旧 App 还在轮询的话，先启动 App 再启动 mock。
- 断档自愈（`docs/protocol.md`「items 的连续性」）：`MOCK_GAP=1` 启动 mock 并卸载重装 App，`-openSession macbook/a1f3c2d4-0000-4000-8000-000000000001`。
  这个会话先只有前 8 行，2 秒后「写入」后 6 条，但只推最后 2 条（`from_seq` 12）；日志里应在约 1.5 秒后出现 `items after_seq=8`，
  对话页的工作过程显示「工具 5」。等日志用带超时的脚本，别写只等成功的 `until grep`。

## UI 测试（`scripts/ui-test.sh`）

`ios/JarvisUITests` 在模拟器里驱动真实 App、连配置里的 hub（所以不在默认的单元测试里跑）：
`SIM=<udid> ./scripts/ui-test.sh [machine/session]`。目前有边缘右滑返回（顶栏展开 / 收起两种状态）。
长按消息的操作条（`MessageMenuTests`，按 0.4 / 0.8 / 1.5 / 2.5 秒松手都要留下，点列表、滑动会关）连 `scripts/mock_hub.py`，不用真实 hub：
`TEST_RUNNER_JARVIS_MOCK_URL=http://127.0.0.1:18760 xcodebuild test … -only-testing:JarvisUITests/MessageMenuTests`。
写 UI 测试时先在没有修复的代码上跑一次确认它会失败，再上修复。

通知按钮（`NotificationActionTests`）不连真实 hub，连 `scripts/mock_hub.py`：
`PORT=18791 python3 scripts/mock_hub.py &`，再 `TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:18791 xcodebuild test … -only-testing:JarvisUITests/NotificationActionTests`。
它在 SpringBoard 上长按横幅、点「拒绝」，再读 mock 的 `GET /mock/resolved` 确认 hub 收到了 resolve。
`LiveActivityActionTests` 同样连 mock：开一个示例实时活动、锁屏、点活动上的「拒绝」。模拟器系统语言是英文时授权弹窗的按钮叫「Allow」；
授权会保留到下次，所以 App 要等进了后台才排这条通知，否则它在测试还在等弹窗时就弹过去了。

发消息和终端对话框的安全检查（`SendSafetyTests`）也连 mock：
`PORT=18771 MOCK_SCREEN_DIALOG=1 MOCK_PROMPT_CHANGED=1 MOCK_SEND_REFUSE=input_draft python3 scripts/mock_hub.py &`，再
`TEST_RUNNER_JARVIS_SAFETY_HUB=http://127.0.0.1:18771 xcodebuild test … -only-testing:JarvisUITests/SendSafetyTests`：
点终端对话框的选项，确认 hub 收到的是 `option` 而不是按键、并提示「对话框已经变了」；电脑上有草稿时点「清空并发送」，确认重发带
`clear_draft` 且 `client_msg_id` 不变（`GET /mock/resolved`、`GET /mock/sent`）。截图：`-openSendMessage <文字>` 打开会话 3 秒后发一条。

## herdr 联调

`./scripts/test-herdr.sh`：起一个独立的 herdr 会话（`herdr --session jarvis-test-<pid> server`，无头，和你正在用的 herdr 互不相干，
结束时按名字停掉），跑带 `herdrlive` 标签的测试：socket 和 CLI 结果一致、事件订阅；再在一个新建的临时目录里起一个 claude，
依次验证信任对话框被认出（发消息拒绝 `dialog_open`）、按 option 回答（rev 核对、键从当前屏幕算）、空输入框放行、草稿拒绝
（`input_draft`）、清空别的字不动、清空看到的那段后放行。本机没有 Go 时在 hub 机器上交叉编译测试二进制再拷回来。
需要本机装了 herdr 和 claude（claude 只启动、不发消息）。

## 隐私扫描

`python3 scripts/privacy_check.py`：整个仓库（文件内容和路径）里不能有这台机器的私有值——`~/.config/jarvis/config.json` 里的
主机、hub 地址、机器名、Apple 的各种 ID、邮箱，`~/.config/jarvis/private-words.txt`（一行一个：内部项目名、人名、内部工具名，
自己维护）和 `CLAUDE.local.md` 里反引号括起来的值；另外任何私网 IPv4（文档网段 192.0.2.0/24 等除外）和真实域名下的邮箱
（`example.*`、公共邮箱、`site/` 里公开的联系邮箱除外）。命中只报类型不报值（免得值进日志）。
不算私有值的：`config.example.json` 里本来就有的默认值（比如外部测试群组名）、仓库自己的 Go 模块路径（里面带 GitHub 用户名）、
`CLAUDE.local.md` 里只是仓库路径 / 命令 / 配置键名的反引号内容（只有像 ID、主机、邮箱、IP 的才算），以及 `site/` 里的联系邮箱和站点域名
（`app_store.feedback_email`、`privacy_url`，App Store 和 OAuth 同意屏幕本来就公开）。文档里说到机器用「hub 机器」「另一台 Mac」，
示例里的机器 id 用 `server` / `laptop` 这类占位符。`release.sh` 打包前跑一遍；
提交前跑：`git config core.hooksPath scripts/hooks`（`pre-commit` 查暂存的内容，`commit-msg` 查提交信息）。

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

## 实时活动截图（`LiveActivityShots`）

`simctl io screenshot` 拍不到灵动岛和锁屏上的活动。UI 测试 `LiveActivityShots` 用 `-showFleetSample` 开一个编造的活动，
回到主屏拍紧凑态、长按灵动岛拍展开态、锁屏再点亮拍锁屏（首次会有系统的「允许实时活动」提示，测试会点允许），最后解锁：

```sh
TEST_RUNNER_JARVIS_FLEET_SAMPLE=needsYou TEST_RUNNER_JARVIS_FLEET_DETAILS=YES xcodebuild test … -only-testing:JarvisUITests/LiveActivityShots \
  -resultBundlePath /tmp/shots.xcresult
xcrun xcresulttool export attachments --path /tmp/shots.xcresult --output-path /tmp/shots
```

`JARVIS_FLEET_SAMPLE=current` 不启动 App，只拍活动现在的样子（配合真实 APNs 推送，见 `docs/live-activity.md` 验证一节）。
