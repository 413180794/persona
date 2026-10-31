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
| 手表 | 手表模拟器 `-pairURL` | 「需要你」（等你的 Agent + 收件箱里需要你的事）、运行中会话；`-openInboxFirst YES` 打开第一件，`-openWatchItem question\|plan\|permission\|screen\|session` 打开第一个这类请求 / 第一个运行中的会话（`running`：列表不显示「需要你」，截运行中的会话行；`inbox`：「需要你」里只留收件箱的行） |

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
- 终端实时流：`MOCK_TERMINAL_STREAM=1` 启动 mock（hello 带 `terminal_stream`，订阅后先发一个编造的 Claude 界面整帧，再每秒一帧让 spinner 走秒；
  再加 `MOCK_TERMINAL_FIXTURE=ios/JarvisTests/Fixtures/terminal-observe.jsonl` 改为回放从 herdr 录下的真实帧：颜色、框里的中文、emoji、链接、200 字的长行），
  `-openPane terminal`，`-terminalWrap NO` 看不换行，`-terminalPages 1` 加载一页（mock 的 `/screen/before` 回一段带颜色的历史）。
- 手机接管终端：再加 `MOCK_TERMINAL_CONTROL=1`（hello 带 `terminal_control`，按 App 给的列 × 行排编造的 Claude 界面，打的字出现在输入框，
  `GET /mock/terminal_input` 看收到的字节），`MOCK_TERMINAL_BUSY=1` 看「电脑上另一个程序正在使用这个终端」的弹窗；`-terminalSize phone|mirror` 选模式。
  UI 测试 `TerminalTypingTests/testDrivingTheTerminalSendsItsBytes` 要连这样的 mock；其余终端 UI 测试固定 `-terminalSize mirror`。
- 形象（`docs/persona.md`）：`MOCK_PERSONAS=demo` 启动 mock 时用 `scripts/persona/make_persona.py demo` 现画一个原创的演示形象「小薄荷」；
  `MOCK_PERSONAS=<目录>` 则列出那个目录里的包（看别的形象的效果，包不进仓库）。UI 测试 `PersonaPickerTests` 要连 `MOCK_PERSONAS=demo` 的 mock。
- 接管的真实链路（不用部署）：在 hub 机器上编一个 darwin 的 jarvisd，本机起一个测试 hub（假 HOME、`HERDR_SOCKET_PATH` 指向真 herdr 的 socket、
  单独的库 `jarvis_termlab` 经 SSH 隧道、`machines` 只有一台 `kind: local`），模拟器配对它，开 lab 窗格的终端（`docs/terminal.md` 第十一节的实测就是这么量的）。
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

终端直接打字（`TerminalTypingTests`：点终端弹出键盘、打的字和回车按顺序发出且被合并成少数几个请求、键栏的 ctrl 作用于键盘打的下一个字、键盘按钮收起键盘）同样连 mock。
`MOCK_TERMINAL_INTERVAL=0.02` 让 mock 一口气回放录制的帧（默认每秒一帧），配 `MOCK_TERMINAL_FIXTURE=ios/JarvisTests/Fixtures/terminal-observe-mixed.jsonl`
（中英混排长回答）或 `-permission.jsonl`（权限框）截终端的样子。
终端键栏和选择（`TerminalKeysTests`：单击 / 双击锁定 / 按住 ctrl、alt、shift，输入框里打的字带 ctrl，⌃C，发出的键名和顺序；长按出「拷贝」；快捷命令双击直接发送、输入框的补全）也连 mock：
`PORT=18792 python3 scripts/mock_hub.py &`，再 `TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:18792 xcodebuild test … -only-testing:JarvisUITests/TerminalKeysTests`
（mock 的 `GET /mock/keys` 回放收到的按键）。

从搜索结果打开会话只有一行标题、返回后搜索结果还在（`SearchOpenTests`，另有不搜索的对照）连普通 mock：
`PORT=18793 python3 scripts/mock_hub.py &`，`TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:18793 xcodebuild test … -only-testing:JarvisUITests/SearchOpenTests`。

发消息和终端对话框的安全检查（`SendSafetyTests`）也连 mock：
`PORT=18771 MOCK_SCREEN_DIALOG=1 MOCK_PROMPT_CHANGED=1 MOCK_SEND_REFUSE=input_draft python3 scripts/mock_hub.py &`，再
`TEST_RUNNER_JARVIS_SAFETY_HUB=http://127.0.0.1:18771 xcodebuild test … -only-testing:JarvisUITests/SendSafetyTests`：
点终端对话框的选项，确认 hub 收到的是 `option` 而不是按键、并提示「对话框已经变了」；电脑上有草稿时点「清空并发送」，确认重发带
`clear_draft` 且 `client_msg_id` 不变（`GET /mock/resolved`、`GET /mock/sent`）。截图：`-openSendMessage <文字>` 打开会话 3 秒后发一条（`-openSendDelay <秒>` 改这个等待）。

弱网 / 断网（`ConnectionTests`，`docs/ios.md`「连接状态」）连普通 mock：`PORT=18760 python3 scripts/mock_hub.py &`，
`TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:18760 xcodebuild test … -only-testing:JarvisUITests/ConnectionTests`：hub 断 10 秒标题变「连接中…」、
恢复后换回；断 0.2 秒（重连约一秒）标题完全不变；断网时发的消息显示「连上后自动发送」，恢复后 hub 只收到一次。
mock 的开关：`GET /mock/down?s=<秒>`（这段时间里 WebSocket 全断、新连接不应答就关）、`GET /mock/delay?s=<秒>`（之后每个请求和 pong 都晚这么久，0 关掉；
启动时也可以 `MOCK_DELAY=<秒>`）。手动测真断开：只杀监听端口的进程 `kill $(lsof -i TCP:18760 -sTCP:LISTEN -t)`——
`lsof -i :18760` 还会列出连着它的模拟器 App，一起杀掉 App 就没了。截图：`-showConnection …`（见 `docs/ios.md`）。

发送文件（`FileUploadTests`：「+」菜单里有「文件」；上传时输入框上方有进度和取消，传完路径插在打的字后面、不发送）也连 mock：
`PORT=18771 MOCK_UPLOAD_KBPS=1024 python3 scripts/mock_hub.py &`，再 `TEST_RUNNER_JARVIS_UPLOAD_HUB=http://127.0.0.1:18771 xcodebuild test … -only-testing:JarvisUITests/FileUploadTests`
（`GET /mock/uploads` 是收到的文件；`MOCK_UPLOAD_OLD_NODE=1` 模拟没升级的 node）。截图：`-openUploadDemo <文件名>` 打开会话 3 秒后传一个 24 MB 的假文件。
系统的文件选择器 XCUITest 点不稳，选文件这一步没自动化。

### 机器页面（`MachinePagesTests`、`MachinePagesLiveTests`）

把机器相关的每个入口真点一遍：设置 › 机器（列表、下拉刷新、卡片 → 详情、详情里的会话 → 对话、离线机器的说明、重新连接）、
Agents 左上角菜单（按机器分组、按机器筛选、全部机器）、项目页的机器名 → 机器的项目页（项目 → 项目页、ⓘ → 详情、离线时的空状态）、
新对话（离线机器不能选、目录浏览、最近目录、启动器、开始后 POST 的内容、失败时错误显示在按钮上方）。

- mock：`PORT=18773 MOCK_MACHINES=1 python3 scripts/mock_hub.py &`（多一台离线的「实验室主机」，`POST …/reconnect` 后变在线；
  Mac Studio 有 36 个 Agent；`GET /mock/hits`、`/mock/reconnects`、`/mock/started`，`/mock/reset` 恢复初始状态；cwd 以 `/broken` 结尾的新会话回 502），
  再 `TEST_RUNNER_JARVIS_MACHINES_HUB=http://127.0.0.1:18773 TEST_RUNNER_JARVIS_SHOTS=<目录> TEST_RUNNER_JARVIS_STYLE=light xcodebuild test … -only-testing:JarvisUITests/MachinePagesTests`，
  深色再跑一遍（`simctl ui <udid> appearance dark`、`JARVIS_STYLE=dark`），每一步的截图存到 `JARVIS_SHOTS`，失败时附截图和视图树。
- 真实接口：本机起一个测试 hub，**不要连线上 hub 做重连、新建、关闭**。在 hub 机器上 `GOOS=darwin` 编 jarvisd 拷回来；
  `docker exec jarvis-postgres createdb -U jarvis <自己的库>`，经 `ssh -L <本地端口>:127.0.0.1:18661` 连；假 HOME 里的 `~/.jarvis/hub.json`
  （0600）只放 `listen`、`token`、`database_url` 和两台机器：一台 `kind: local`，一台连不上的 ssh；`herdr --session <名字> server` 起一个独立的 herdr，
  `HERDR_SOCKET_PATH` 指向它（别指向正在用的那个）。假 HOME 里建 `code/demo`，然后
  `TEST_RUNNER_JARVIS_LIVE_MACHINES_HUB=… TEST_RUNNER_JARVIS_LIVE_MACHINES_TOKEN=… TEST_RUNNER_JARVIS_LIVE_MACHINES_CODE=<假 HOME>/code xcodebuild test … -only-testing:JarvisUITests/MachinePagesLiveTests`。
  node 的「最近目录」来自这台机器真实的 Claude 记录，所以测试用 `-cwd.<机器 id>` 指定目录浏览的起点。跑完关掉测试 hub、隧道和 herdr 会话，删掉库。
- 别的车道收尾时会 `pkill -f mock_hub.py`：长时间跑的 mock 复制一份改个名再起（`/tmp/<车道>_fakehub.py`），套个 while 循环被杀了自己起来。
- 测试中途失败时 XCTest 会结束 App，App 的请求被取消：hub 日志里那次 `POST /api/v1/sessions` 记 502，其实 node 已经把会话起好了（node 不随 hub 的请求取消）。
  看到孤立的 502 先查是不是测试自己杀了 App。

### Agents 列表（`AgentsListTests`）

快速筛选（全部 / 需要你 / 未读 / 工作中，计数随机器筛选变）、细列表（行数至少是卡片的 2.5 倍，重启后记住）、点空间标题折叠（折叠后标题仍报各状态计数，
重启后记住；全部折叠 / 全部展开；折叠的空间从 ⋯「打开空间」进空间页）、空间 ⋯「全部标为已读（N）」、未读筛选下一路往下看（打开过的留在原位，
重选筛选才消失）、筛选里「全部标为已读」确认后清空、筛选条在各种状态下紧贴搜索框不留空档（`testFilterBarHugsTheTop`：滚动、到底、下拉刷新、进对话再返回、切到后台再回来、切 tab、搜索；
`testFilterBarHugsTheTopBehindARestoredChat`：冷启动恢复上次的对话再返回。搜索框必须在，筛选条按 frame 紧贴其下，**而且按截图像素确认真画在那里**：
筛选条的内容被 UIKit 下移时 XCUITest 的 frame 不变，只看 frame 会把 TestFlight 35 / 38 的空档放过去），以及长列表滚动（`XCTOSSignpostMetric`，模拟器只给时长不给掉帧，真机再看）。

- mock：`PORT=18775 MOCK_AGENTS=1 python3 scripts/mock_hub.py &`（约 130 个会话：platform 空间 3 需要你 / 35 完成未看 / 12 工作中，
  另有空闲多的空间和全空闲的空间；`seen`（WebSocket 或 `POST …/seen`）把完成未看改成已看，`GET /mock/seen` 列出，`/mock/reset` 复原），
  `TEST_RUNNER_JARVIS_AGENTS_HUB=http://127.0.0.1:18775 TEST_RUNNER_JARVIS_SHOTS=<目录> TEST_RUNNER_JARVIS_STYLE=light … -only-testing:JarvisUITests/AgentsListTests`，深色再跑一遍。
- `-openAgentsFresh YES` 启动时清掉列表记住的样子（机器筛选、分组、细列表、折叠）：这些值测试里要在界面上改，不能用启动参数固定（启动参数会盖过 App 写进去的值）。
- 列表行（`NavigationLink`）上的 `accessibilityIdentifier` 在视图树里出现两次（cell 和里面的 button），数行数、取第 n 行用 `app.buttons.matching(identifier:)`。
- `SearchOpenTests` 以前不带 `-dev.openChat ""`：上一次测试留下打开的对话，启动后恢复进去，右滑返回回不到列表，两个用例都挂（master 上也一样）。
- 细列表截图（`CompactListShots`，不用 mock，用体验模式的数据）：全部（空间多）、Mac Studio（未读多、空闲的空间）、未读筛选、需要你筛选、Mac mini（全部空闲）、
  按状态分组、卡片对照，每种三档字号（`-ui.textSize small|standard|large`；`TEST_RUNNER_JARVIS_TEXT_SIZES=standard` 只拍一档）。
  `TEST_RUNNER_JARVIS_SHOTS=<目录> TEST_RUNNER_JARVIS_STYLE=light … -only-testing:JarvisUITests/CompactListShots`，深色再跑一遍。
  改前 / 改后对比：在 `git worktree add --detach <目录> origin/master` 里放进同一个测试文件拍改前，两边同一份数据。
  带 `-openWelcome YES` 是为了去掉别的测试留下的配对（Keychain 里的 hub 优先于体验模式），`-demo.on YES` 让它直接进体验模式。

### 实时更新不打断操作（`LiveUpdateTests`）

用户正在做的事经得起 hub 一直推更新：Agents 的 ⋯ 菜单滚到机器那里开着（不跳、不回顶）、行的长按菜单开着、列表滚到中间（行不动）、
在搜索框里打字（字和键盘都在）、行左滑出「关闭」；对话里工作中的 Agent 的过程卡展开着、过程里的截图全屏看着（不被关掉）。
每个用例报每轮推送的 body 次数和主线程耗时（附件和日志里的 `PERF` 行，改前改后的对比见 `docs/lanes/live-updates.md`）。

- mock：`PORT=18797 MOCK_AGENTS=1 MOCK_CHURN=1 python3 scripts/mock_hub.py &`，`GET /mock/churn?n=12&ms=400` 推 n 轮（每轮：各机器的会话列表——工作中的换一句、
  两个工作中的轮流变成最新——、机器列表、每个打开的对话一个新步骤，每 4 轮夹一句话），`&steps=N` 每轮每个对话加 N 步，`&final=1` 推完后这一轮结束（最后一句回复、工作中的变成完成）；`/mock/churned` 看推到第几轮；`MOCK_CHURN=1` 还在第一个对话的过程里放一张截图、多加 5 台机器（菜单要能滚）。
- `TEST_RUNNER_JARVIS_CHURN_HUB=http://127.0.0.1:18797 xcodebuild test … -only-testing:JarvisUITests/LiveUpdateTests`。

### 体验模式（`DemoModeTests`、`DemoHubTests`）

不用 mock hub：`TEST_RUNNER_JARVIS_SHOTS=<目录> TEST_RUNNER_JARVIS_STYLE=light xcodebuild test … -only-testing:JarvisUITests/DemoModeTests`，深色再跑一遍。
`testWalkThroughAndLeave` 从配对页（`-openWelcome YES`）进体验模式，走一遍 Agents（筛选、细列表、Claude 会话发消息收到演示回复、终端、提问卡片作答、
计划卡片、Codex 会话、表格和链接）、机器监控和 24 小时曲线、Jarvis（主对话、侧边栏、定时任务）、收件箱（飞书会话、邮件），
每页断言没有「体验模式里没有这项数据」，最后从设置退出、重启后还在配对页；`testStaysAcrossLaunches` 断言体验模式重启后还在。
跑完看模拟器日志里有没有页面要了 `DemoHub` 没有的接口：
`xcrun simctl spawn <udid> log show --last 10m --predicate 'eventMessage CONTAINS "DemoHub miss"'`（应为空）。
`DemoHubTests`（单元测试）让每个接口经 `APIClient` 解码，并检查副作用被拒、消息有回显和回复。

- 列表行被快速筛选条（`safeAreaBar`）盖住时 XCUITest 仍说它 `isHittable`，点下去点到的是筛选条（「需要你」被选上）：点行之前把它滚到筛选条以下、tab 栏以上，
  而且一次只拖三分之一屏（`swipeUp` 会甩过头）。
- 对话页有两个「终端」按钮（展开的页头在时钟下面、折叠后的小页头里），取 `frame.minY > 40` 且能点的那个。

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
回到主屏拍紧凑态（隔 0.4 秒两张，比对有没有动画）、长按灵动岛拍展开态（长按偶尔不生效，会再试一次存成 `expanded2`）、锁屏再点亮拍锁屏（首次会有系统的「允许实时活动」提示，测试会点允许），最后解锁：

```sh
TEST_RUNNER_JARVIS_FLEET_SAMPLE=needsYou TEST_RUNNER_JARVIS_FLEET_DETAILS=YES TEST_RUNNER_JARVIS_FLEET_ISLAND=custom:assistant,load xcodebuild test … -only-testing:JarvisUITests/LiveActivityShots \
  -resultBundlePath /tmp/shots.xcresult
xcrun xcresulttool export attachments --path /tmp/shots.xcresult --output-path /tmp/shots
```

`JARVIS_FLEET_SAMPLE=current` 不启动 App，只拍活动现在的样子（配合真实 APNs 推送，见 `docs/live-activity.md` 验证一节）。
