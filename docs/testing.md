# 测试

## 命令

```sh
cd server && go test ./... && go vet ./...                    # 服务端
cd apple && xcodegen generate -q && xcodebuild -project Jarvis.xcodeproj -scheme Jarvis \
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
- 跟到最新（`ChatFollowTests`，`MOCK_LONG_HISTORY=1`）：长会话打开在底部；在底部时来回复会跟到底（Jarvis 页的回复先按 `delta` 流式出）；往上翻出现「回到最新」、
  这时来回复不拽人；点了回到底部、之后照常跟随。Agent 对话页分三种打开方式测：普通、完成未看（`-showAsUnread YES`）、停在上次的位置（`-chat.read.<会话> <行 id>`）。
  回复由 mock 的 `GET /mock/say?text=…&user=…&stream=1` 推给所有打开的对话；列表在哪由 App 的 `-showScrollState YES` 报（无障碍标签
  `chat.scrollState`：bottom / follows / frozen / arrow / newestShown / withheld…，`visible` 时画在屏幕上），XCUITest 读不准倒置列表里行的位置。
  `testRealHubChats` 连真实 hub 拍改前改后（`TEST_RUNNER_JARVIS_REAL_PAIR` 等环境变量，不给就跳过；只开自己的会话，打开会标已读）。
- 终端实时流：`MOCK_TERMINAL_STREAM=1` 启动 mock（hello 带 `terminal_stream`，订阅后先发一个编造的 Claude 界面整帧，再每秒一帧让 spinner 走秒；
  再加 `MOCK_TERMINAL_FIXTURE=apple/JarvisTests/Fixtures/terminal-observe.jsonl` 改为回放从 herdr 录下的真实帧：颜色、框里的中文、emoji、链接、200 字的长行），
  `-openPane terminal`，`-terminalWrap NO` 看不换行，`-terminalPages 1` 加载一页（mock 的 `/screen/before` 回一段带颜色的历史）。
- 手机接管终端：再加 `MOCK_TERMINAL_CONTROL=1`（hello 带 `terminal_control`，按 App 给的列 × 行排编造的 Claude 界面，打的字出现在输入框，
  `GET /mock/terminal_input` 看收到的字节），`MOCK_TERMINAL_BUSY=1` 看「电脑上另一个程序正在使用这个终端」的弹窗；`-terminalSize phone|mirror` 选模式。
  UI 测试 `TerminalTypingTests/testDrivingTheTerminalSendsItsBytes` 要连这样的 mock；其余终端 UI 测试固定 `-terminalSize mirror`。
- 文件查看器（`docs/ios.md`「文件查看器」）：`MOCK_FILES=1` 启动 mock，给出一份编造的 `/Users/me/code/weather`：每种语言一个小文件、
  3 MB 的 JSON、2 万行的 Go、4000 多行的 README、5 MB 的日志，`GET …/file`（2 MB 一页）和 `…/stat` 都按它回答；「给天气 App 加主屏幕小组件」
  最后一条回复提到其中几个文件，和两个不在的路径。UI 测试 `FileViewerUITests` 要连这样的 mock。测耗时和帧率：用 `-O` 编一份
  （`SWIFT_OPTIMIZATION_LEVEL=-O`，另一个 DerivedData），带 `JARVIS_FILE_PERF=1` 跑 `FileViewerPerfTests`，再读 App 容器里的
  `Library/Caches/file-perf.log`。滚动用 App 内的匀速滚动测，别用 XCUITest 拖：它每个手势前在 App 主线程上抓一次无障碍快照，自己就是 40–100 ms 的长帧。
  改了 mock 里某条消息的文字要先卸载 App：App 按 item id 缓存会话，同 id 的新文字不会盖掉旧的。
- 形象（`docs/persona.md`）：`MOCK_PERSONAS=demo` 启动 mock 时用 `scripts/persona/make_persona.py demo` 现画一个原创的演示形象「小薄荷」；
  `MOCK_PERSONAS=<目录>` 则列出那个目录里的包（看别的形象的效果，包不进仓库）。UI 测试 `PersonaPickerTests` 要连 `MOCK_PERSONAS=demo` 的 mock。
  `LookPickerTests`（形象、主题色、界面风格三个选择互不影响）要连 `MOCK_PERSONAS=<目录>` 的 mock，目录里要有一个带配色、id 为 `doudou` 的包；
  设了 `JARVIS_LOOK_SHOTS=<目录>` 就把「设置 › 外观」下主题色、界面风格两页和设置首页的截图存进去（深浅色跟模拟器的外观）。它会改 App 自己存的 `ui.style` / `ui.color`，结束时点回液态玻璃 + 跟随界面风格。
  看某个组合直接用启动参数 `-ui.style clay -ui.color persona -openPersona doudou`。
- 接管的真实链路（不用部署）：在 hub 机器上编一个 darwin 的 jarvisd，本机起一个测试 hub（假 HOME、`HERDR_SOCKET_PATH` 指向真 herdr 的 socket、
  单独的库 `jarvis_termlab` 经 SSH 隧道、`machines` 只有一台 `kind: local`），模拟器配对它，开 lab 窗格的终端（`docs/terminal.md` 第十一节的实测就是这么量的）。
- 终端断线：`MOCK_SCREEN_FAIL_AFTER=3:14` 启动 mock（第一次读终端 3 秒后每次都 503，再过 14 秒恢复），
  `-openSession macbook/a1f3c2d4-0000-4000-8000-000000000001 -openPane terminal`，应先出现红色「上次更新 N 秒前 · 重试」，恢复后消失。
  mock 进程启动时还没有读终端的请求，计时从第一次请求算：编译期间旧 App 还在轮询的话，先启动 App 再启动 mock。
- 断档自愈（`docs/protocol.md`「items 的连续性」）：`MOCK_GAP=1` 启动 mock 并卸载重装 App，`-openSession macbook/a1f3c2d4-0000-4000-8000-000000000001`。
  这个会话先只有前 8 行，2 秒后「写入」后 6 条，但只推最后 2 条（`from_seq` 12）；日志里应在约 1.5 秒后出现 `items after_seq=8`，
  对话页的工作过程显示「工具 5」。等日志用带超时的脚本，别写只等成功的 `until grep`。

## UI 测试（`scripts/ui-test.sh`）

`apple/JarvisUITests` 在模拟器里驱动真实 App、连配置里的 hub（所以不在默认的单元测试里跑）：
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
`MOCK_TERMINAL_INTERVAL=0.02` 让 mock 一口气回放录制的帧（默认每秒一帧），配 `MOCK_TERMINAL_FIXTURE=apple/JarvisTests/Fixtures/terminal-observe-mixed.jsonl`
（中英混排长回答）或 `-permission.jsonl`（权限框）截终端的样子。
终端键栏和选择（`TerminalKeysTests`：单击 / 双击锁定 / 按住 ctrl、alt、shift，输入框里打的字带 ctrl，⌃C，发出的键名和顺序；长按出「拷贝」；快捷命令双击直接发送、输入框的补全）也连 mock：
`PORT=18792 python3 scripts/mock_hub.py &`，再 `TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:18792 xcodebuild test … -only-testing:JarvisUITests/TerminalKeysTests`
（mock 的 `GET /mock/keys` 回放收到的按键）。

从搜索结果打开会话只有一行标题、返回后搜索结果还在、搜索时折叠的空间里的 Agent 也搜得到点得开（`SearchOpenTests`，另有不搜索的对照，对照里再右滑返回列表；
测试带 `-openAgentsFresh YES`，不受别的测试留下的折叠影响）连普通 mock：
`PORT=18793 python3 scripts/mock_hub.py &`，`TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:18793 xcodebuild test … -only-testing:JarvisUITests/SearchOpenTests`。

端口转发（`PortForwardTests`，`docs/ports.md`）：会话顶部和回复里的 `:端口` Badge 带页面标题、停掉的变灰，点开在网页卡片里经 mock 的
`…/ports/{port}/connect` 加载 mock 自己起的页面（`GET /mock/ports` 看连接记录），点灰的提示「已经没有在监听」：
`MOCK_PORTS=1 PORT=18791 python3 scripts/mock_hub.py &`，`TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:18791 xcodebuild test … -only-testing:JarvisUITests/PortForwardTests`；
深浅色 × 新外壳 / 旧布局用 `TEST_RUNNER_JARVIS_LAYOUT`、`TEST_RUNNER_SHOT_SUFFIX` 各跑一遍，截图在 `/tmp/jarvis-ports-*`。

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

把机器相关的每个入口真点一遍：设置 › 机器（列表、下拉刷新、卡片 → 详情、详情里的 Agent → 对话、36 个 Agent 的「显示全部」、离线机器的说明、重新连接）、
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
重选筛选才消失）、筛选里「全部标为已读」确认后清空、手机上的筛选条作为列表首行随内容滚动（`testFilterBarHugsTheTop`：打开时搜索收起，下拉显示搜索，筛选条滚出屏幕后不留固定副本；
到底、下拉刷新、进对话再返回、切到后台再回来、切 tab、搜索后回到顶部都不留雾化空档；`testFilterBarHugsTheTopBehindARestoredChat`：冷启动恢复上次的对话再返回，再下拉搜索、滚动）。
顶部静止时按 frame 检查筛选条紧贴导航栏或展开的搜索框，**而且按截图像素确认真画在那里**：筛选条的内容被 UIKit 下移时 XCUITest 的 frame 不变，
只看 frame 会把 TestFlight 35 / 38 的空档放过去。搜索类测试先下拉展开抽屉，不能把收起的搜索框当作没有回到列表。
长列表滚动另用 `XCTOSSignpostMetric` 测量，模拟器只给时长不给掉帧，真机再看。

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
  两个工作中的轮流变成最新——、机器列表、每个打开的对话一个新步骤，每 4 轮夹一句话），`&steps=N` 每轮每个对话加 N 步，`&final=1` 推完后这一轮结束（最后一句回复、工作中的变成完成）；`/mock/churned` 看推到第几轮；`MOCK_CHURN=1` 还在第一个对话的过程里放一张截图、多加 5 台机器（菜单要能滚）；`&inbox=1` 每轮再改一条收件箱、重推等你处理的事，`&waiting=1` 每轮多一个等你的 Agent、
  下一轮又答完（角标每轮变；真实的 hub 一直在推这两样，只推会话列表测不出角标带来的重建，`docs/lanes/live-updates.md`「又回来了」）。
- `TEST_RUNNER_JARVIS_CHURN_HUB=http://127.0.0.1:18797 xcodebuild test … -only-testing:JarvisUITests/LiveUpdateTests`。
- 新外壳和旧布局都要跑：UI 测试默认用旧布局（`launchJarvis` 加 `-ui.oldLayout YES`），设 `TEST_RUNNER_JARVIS_LAYOUT=shell` 改用新外壳（iPhone 的默认），
  `LiveUpdateTests`、`ChatFollowTests`、`ScrollUnderPushTests` 两种都过才算。

### 滚动中推送（`ScrollUnderPushTests`）

手指拖着、惯性滑着的时候 hub 照推，屏幕上的东西只跟着手指和惯性走（`docs/lanes/live-updates.md`「滚动时被迫刷新」）。
Agents 列表、Agent 对话、Jarvis 对话各拖、甩约 20 秒，同时 mock 按真实 hub 的节奏推：`/mock/churn?n=10&ms=2000&inbox=1&waiting=1&real=1`，
每 2 秒一轮。`&real=1` 让工作中的 Agent 最后一句在一行、两行之间换（行变高变矮），并让一个 Agent 每三轮完成一次、再开工（空间换名次）。

- App 以 `-perfCounts YES` 启动时，`PerfScroll` 逐帧看正在拖、正在滑的滚动视图。
  - `List` 看它的 cell，`ScrollView` 看 SwiftUI 画行用的 layer。
  - 记下几类异常：整体跳开（`jumps`，≥ 100 pt 的另记 `big`）、行之间相对挪动（`moved`）、还在屏幕里的东西被拿掉重画（`replaced`）、
    惯性被打断（`stopped`）、内容高度变了（`resized`）。
  - 每次异常记到它前 0.6 秒内到达的那类推送上（`causes`）。
  - 页面刚放开扣住的更新（停下 1.5 秒后一次落地）的 0.3 秒内出的异常另记 `landed`：那是更新按设计在静止时落地，
    UIKit 在下一次手指落下时才排版，不算滚动中被打断。
  - 滚动中算了哪些 body 记在 `bodies`。都在 `perf.counts` 的 `scroll` 里。
- 断言：`big`、`replaced`、`stopped`、`defaults` 都是 0，Agents 列表的 `moved` 也是 0。两种布局、两种风格（`TEST_RUNNER_JARVIS_UI_STYLE=beautiful`）都跑。
  - 小于 100 pt 的跳动不推送也有（懒加载的行自己定高、UI 测试合成的拖动），不算。
  - 机器页那种玻璃卡片的页面，`moved` 没有推送也一直在涨（效果层自己相对内容动），只在列表上看它。
- mock：`PORT=18797 MOCK_AGENTS=1 MOCK_CHURN=1 MOCK_LONG_HISTORY=1 python3 scripts/mock_hub.py &`（两个对话要比屏幕长才滚得动），
  `TEST_RUNNER_JARVIS_CHURN_HUB=http://127.0.0.1:18797`。
- 测试先配对一次、退出，再带要打开的页面启动：配对的 hub 和上次不同时，App 从头开始、停在助手页，`-app.tab`、`-dev.openChat` 都不起作用。

### 体验模式（`DemoModeTests`、`DemoHubTests`）

不用 mock hub：`TEST_RUNNER_JARVIS_SHOTS=<目录> TEST_RUNNER_JARVIS_STYLE=light xcodebuild test … -only-testing:JarvisUITests/DemoModeTests`，深色再跑一遍。
`testWalkThroughAndLeave` 从配对页（`-openWelcome YES`）进体验模式，走一遍 Agents（筛选、细列表、Claude 会话发消息收到演示回复、终端、提问卡片作答、
计划卡片、Codex 会话、表格和链接）、机器页和机器的 24 小时曲线、Jarvis（主对话、侧边栏、定时任务）、收件箱（飞书会话、邮件），
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

## Codex 联调

`./scripts/test-codex.sh`：Codex 的提问和审批卡片（`docs/codex.md`「提问和审批」）对真实 Codex 跑一遍。起一个独立的 herdr 会话（`jarvis-codex-<pid>`，
结束时按名字停掉），在一个新建的临时 git 仓库里起 Codex，用 node 回答卡片的同一段代码（`codexAnswer`）作答，每个用例检查 rollout 和屏幕：
async 提问（空输入框、电脑上有草稿、0.163 的提问面板展开着、电脑上先答了）、审批（直接答、往上翻了、别的 tab 在前台、对话记录 pager 开着时
拒绝并写理由、电脑上先答了）、提问后紧跟审批、Plan 模式的两道题（一道按选项、一道写自己的话）。

- `JARVIS_CODEX_BINS="codex /path/to/other/codex"`：要测的 Codex（每个版本一份，比如 npm 装到临时目录的 `@openai/codex@0.163.0`）；
  `JARVIS_CODEX_ARGS="-m <模型>"`：登录能用的模型（ChatGPT 登录不是每个模型都收）；`JARVIS_CODEX_ROUNDS`（默认 2）；
  `JARVIS_CODEX_COLS=64` 每个版本再在 64 列的小窗口里跑一遍（`stty cols`：无头 herdr 的窗格分屏不会变窄）；`JARVIS_CODEX_CASES=a,b` 只跑这几个。
- 用你自己的 `~/.codex` 登录，会发真实的（很小的）模型请求。信任临时目录会写进 `~/.codex/config.toml`，每个测试线程在 `~/.codex/sessions`
  留一个 rollout：退出时两样都删掉。本机没有 Go 时在 hub 机器上交叉编译测试二进制再拷回来。
- `JARVIS_CODEX_DAEMON_BINS="<0.163 的 codex>"`：守护进程路线（`docs/codex.md`「守护进程上的会话」）。测试自己起一个
  `codex app-server --listen unix://<临时目录>/app.sock`（子进程，能杀掉重启；不碰正在用的守护进程），TUI 用 `--remote … -C <目录> -s read-only`
  挂上去，node 关掉发现、只认这个 app-server，用一个假 hub 答卡片、看撤卡。用例：async / 阻塞提问、命令 / 改文件 / 权限审批、拒绝并写理由、
  电脑先答、手机先答、同时答、守护进程重启、输入框有草稿时发消息，再加 `JARVIS_CODEX_BINS` 的第一个（比如 0.153）在旁边确认它走终端路线。
  权限审批要 `features.request_permissions_tool`，测试的 app-server 会打开它。
- 从手机新建（`docs/codex.md`「从手机新建」，`TestLiveCodexStart`）：按 `session.start` 的路子新建，第一条消息带引号、换行、`$`、反引号、中文和几千字。
  三种：`JARVIS_CODEX_DAEMON_BINS` 的每个经 `--remote` 挂在测试自己的 app-server 上（app-server 路线）、同一个带 `-c` 的（终端路线，看「连接」的说明），
  `JARVIS_CODEX_BINS` 的第一个（0.153）。每轮检查：回答在 60 秒内、带的就是这条消息开的 thread、rollout 里的第一条消息一字不差、这个窗格只有一条会话、
  路由对、Codex 回了第一条也收到手机再发的一条；最后一个找不到命令的启动器要马上报错。新建要一个本机的 jarvisd（`codex-start`），脚本一起编。
  不跑不带参数的 0.163：它会自动启动或加入正在用的守护进程。`JARVIS_CODEX_TESTS="cards daemon start"`（默认三样都跑）挑着跑。
- 每轮每个用例打一行 `LIVE <版本> round <n> <用例>: ok in <秒>`，失败时附上当时的屏幕；最后一行 `RESULT all: passed|FAILED`。
  失败时屏幕上是 Codex 自己的模型服务错误（「Selected model is at capacity」之类，不是 Jarvis 的问题）就记一行 `SERVICE-RETRY` 再跑一次这个用例，
  每个 RESULT 行报重试了几次——报告稳定性时要一起报，不能当成没发生。

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

### npm 下载源拦截

所有目录下的 `package-lock.json` 和 `npm-shrinkwrap.json` 还会单独解析完整 JSON，检查 v1 的嵌套 `dependencies` 和 v2/v3 的
`packages` 中每个 `resolved`。远程下载只接受 npm 官方公开 registry（`https://registry.npmjs.org`，缺省端口或 443），
其他主机、HTTP、URL 用户名/密码、query/fragment、控制字符、反斜杠都拒绝；合法相对 `file:` 依赖和 workspace link 可以通过，
绝对路径、远程 authority 及编码后伪装成远程路径的形式不行。损坏 JSON、重复键、非字符串 `resolved` 都报错，不能用 NUL 让锁文件被当成二进制跳过。
这一项**不依赖本机配置或私词表**，报错只显示文件、行号、问题类型，不输出下载地址。`.invalid` 占位域名用于拒绝测试，不把真实内部域名写进规则或测试。

- 网页手动检查：`cd web && npm run check:privacy`。
- `npm run build` 的 `prebuild` 自动运行全仓隐私扫描，网页部署和公开发布都经过这个入口。
- Python 回归测试：`python3 -B -m unittest discover -s scripts -p 'test_privacy_check.py'`；`npm test` 不包含它。
  测试用隔离 HOME/Git 配置的临时仓库验证真实提交钩子、暂存区与工作树差异，不操作主工作区的暂存区或历史。
- hooksPath 是每个克隆的本地配置；新克隆仍需运行上面的 `git config` 命令，不能因为仓库里有 hooks 文件就以为它已经生效。

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

### Jarvis 页（长主对话）

mock 加 `MOCK_LONG_ASSISTANT=1`（主对话是 60 轮：每轮几个工具、一段带标题 / 列表 / 表格 / 代码块的长回复），`-app.tab assistant -showAutoScroll YES -perfCounts YES`。
`-showAutoScroll` 现在依次量五段，每段一个 `Documents/frames-<段>.json`（每帧间隔、每帧主线程忙了多久 `busy_*`、
50 ms 以上的长帧在第几秒 `long`、这段里各视图 body 的次数 `bodies`、hub 推送的主线程耗时 `push_ms`）：

| 段 | 做什么 | 脚本要配合的 |
| --- | --- | --- |
| idle | 停 8 秒 | 期间 `GET /mock/churn?n=14&ms=500` 推 14 轮 |
| type | 每 120 ms 往草稿里加一个字，共 40 个 | — |
| stream | 6 秒 | `frames-type.json` 一出现就 `GET /mock/say?stream=1&steps=60&ms=80&text=<长回复>`（hub 的节奏：每 80 ms 一个 delta） |
| sweep | 原来的 8 段动画跳转（`scrollTo(point:)`，每段开头那一帧 SwiftUI 要估一大段行高，50–200 ms，手指滑不会这样） | — |
| glide | 直接按显示帧移动列表的 `UIScrollView`，2000 pt/s 往历史 4 秒、回来 4 秒，最像手指滑 | — |

2026-11-01 改前（master e51a1a8）/ 改后，模拟器 iPhone 17 Pro（iOS 26.5），各 3–4 次：

| 段 | 指标 | 改前 | 改后 |
| --- | --- | --- | --- |
| idle（14 轮推送） | 推送的主线程合计 | 190–228 ms | 43–67 ms |
| idle | 主线程超过 16 ms 的帧 | 7–14 | 1–4 |
| idle | 回复 / 气泡 body 次数 | 638 / 603 | 约 30 / 12 |
| stream | 超过 16 ms 的帧 | 21–40 | 7–12 |
| stream | `ChatView` body 次数 | 33 | 1 |
| glide | 掉帧（晚于 1.5 帧） | — | 0 |
| type | 每个字主线程超过 8 ms 的帧（40 个字） | 24–35 | 2–3（输入框放进 `LayoutIsland` 后） |

改了什么：用户气泡、回复、工作行、输入框改成 `Equatable`（页面每次推送都会重算，以前屏幕上和懒加载缓冲里的每一行跟着重算）；
侧边栏关着时不读 `assistantConversations`（它读所有机器的所有会话）；顶栏的状态圆点拆成单独的视图（顶栏和它的菜单不再跟着推送重建）；
流式回复按块切开（`StreamChunks`），写完的块不再重新解析排版，间距按主题的 margin 补；已经停在最新端时流式的「跟到最新」不再写 `ScrollPosition`。
打字：每个字都让整页从根重新排版一次（输入框和列表在同一个 VStack 里，懒加载列表会重新量可见行）。现在输入框在自己的
hosting controller 里排版（`LayoutIsland`，`DesignSystem/LayoutIsland.swift`），页面只在它的高度变了（折行、上面多一个提示）时才重新排；
`ComposerGrowTests` 查多行时它会长高、一直在键盘上面、删回一行会缩回去（新外壳和旧布局各一遍）。

### 整个 App（任意页面）

`-showPagePerf YES`（DEBUG）：启动 10 秒后量 8 秒「停在页面上」（`frames-page-idle.json`；开始时写 `Documents/page-idle-started`，
脚本看到它再 `GET /mock/churn?n=12&ms=500`，推送不会落到别的段里），然后找屏幕上最大的滚动视图，按显示帧往下滑 3 秒、往回 3 秒（2000 pt/s，
`frames-page-glide.json`）。配合打开页面的启动参数：工作页 `-tab dev -app.tab chats`、收件箱 / 机器 `-openShellPage inbox|machines`、
Agent 对话 `-openSession <machine>/<session>`。mock 用 `MOCK_AGENTS=1`（约 130 个 Agent）才看得出列表的开销。

2026-11-01，iPhone 17 Pro 模拟器，各 2 次：

| 页面 | 12 轮推送的主线程合计 | 超过 16 ms 的帧（停 / 滑） |
| --- | --- | --- |
| 工作（Agents 列表，130 个 Agent） | 约 375 ms → 82–88 ms | 15 → 1–4 / 1–2 |
| 收件箱 | 19 ms | 6 / 8 |
| 机器 | 44 ms | 2 / 7 |
| 长 Agent 对话（`MOCK_LONG_HISTORY=1`） | 37 ms | 6 / 6 |

工作页的改动：hub 把每台机器的会话列表分开推（一轮好几条），以前每条都让读会话的页面各重算一遍；现在 25 ms 内到的合成一次
（`AppModel.applyPendingSessions`），一轮推送从约 4 次 SwiftUI 更新变成 1 次。

## 侧边栏拖动（Jarvis tab）

量拖动时每帧主线程的耗时，不用 Instruments（这台 Mac 上 xctrace 卡在授权）：DEBUG 的 `DrawerProbe`（`-showDrawerProbe YES`）在拖动开始到松手后
0.6 秒里，用 run loop 的 afterWaiting / beforeWaiting（排在 Core Animation 提交之后）算每一帧里主线程忙了多久，加上 CADisplayLink 的帧间隔，
拖动中和松手后分开统计，追加到 App 容器的 `Documents/drawer-probe.jsonl`；`DrawerPan` 每次拒绝 / 丢掉一个触摸也记一行（为什么没打开）。

- 手势本身：`DrawerSwipeTests`（mock 加 `MOCK_DRAWER=1`，主对话末尾有一个比屏幕宽的代码块），斜划、气泡上、快甩停下后、代码块上。
- 数字别在 XCUITest 下量：它开着无障碍，SwiftUI 每帧多做很多事。用 `-showDrawerAutoDrag <轮数>`，`simctl launch` 启动、不接 XCUITest：
  每轮拖开松手、拖回、半路松手（弹回）、快甩、点页面关闭，经过和手指一样的 `AssistantDrawer.dragBegan / drag / letGo`，每个显示帧一步。
  用开了优化的 Debug 构建（`SWIFT_OPTIMIZATION_LEVEL=-O SWIFT_COMPILATION_MODE=wholemodule`，单独的 derivedData）。对照组用 `git worktree`
  在旧提交上加同样的 `DrawerProbe.swift` 和三个同名方法。
- 看主线程在忙什么：`sample <pid> 30 1`，pid 要按自己模拟器的设备目录找（`pgrep -f "Devices/<udid>/.*Jarvis.app/Jarvis"`，别的车道的模拟器也在跑 Jarvis）。

2026-10-31 改前（master e4fca3b）/ 改后，模拟器 iPhone 18 Pro Max（60 Hz），长对话，每种 10 次（5 轮 × 2 次启动）：

| 操作 | 版本 | 次数 | 拖动中每帧主线程 p50 | 拖动中 p95 | 第一帧 | 拖动中最长 | 松手后最长 | 掉帧（拖动 / 松手后） |
|---|---|---|---|---|---|---|---|---|
| 拖开 | 改前 | 10 | 6.6 ms | 27.6 ms | 28.5 ms | 107.3 ms | 13.8 ms | 14 / 0 |
| 拖开 | 改后 | 10 | 3.8 ms | 5.2 ms | 2.5 ms | 28.8 ms | 30.7 ms | 2 / 2 |
| 拖回 | 改前 | 10 | 6.7 ms | 9.1 ms | 4.3 ms | 15.2 ms | 29.8 ms | 0 / 18 |
| 拖回 | 改后 | 10 | 4.0 ms | 4.8 ms | 3.8 ms | 15.1 ms | 21.5 ms | 1 / 2 |
| 半路松手 | 改前 | 10 | 6.4 ms | 24.0 ms | 24.3 ms | 35.5 ms | 23.3 ms | 12 / 20 |
| 半路松手 | 改后 | 10 | 3.8 ms | 5.0 ms | 2.2 ms | 14 ms | 8.1 ms | 0 / 0 |
| 快甩 | 改前 | 10 | 10.8 ms | 25.9 ms | 23.2 ms | 35.6 ms | 8.7 ms | 12 / 0 |
| 快甩 | 改后 | 10 | 3.8 ms | 12.8 ms | 2.2 ms | 15.2 ms | 15.1 ms | 1 / 1 |

「第一帧」是拖动开始那一帧，改前在这一帧里建整个侧边栏、藏 tab 栏（改安全区，整页重新布局）；「松手后最长」改前是关上最后一帧把 tab 栏放回来。
掉帧（晚于 1.5 帧）合计 76 → 9。
用 XCUITest 真拖（`testMeasureDrags`，`JARVIS_DRAWER_MEASURE=4`）：改后 16 次拖动每帧主线程 p50 约 4 ms、最长 15 ms，快甩全部打开；
改前那一轮第一次快甩就没打开（松手速度 495pt/s 不到旧阈值 600），第一帧 18.6 ms，关上后 40–50 ms。
模拟器跑在 Mac 的 CPU 上，绝对值比真机小，看相对变化；真机 120 Hz 每帧只有 8.3 ms。

## 实时活动截图（`LiveActivityShots`）

`simctl io screenshot` 拍不到灵动岛和锁屏上的活动。UI 测试 `LiveActivityShots` 用 `-showFleetSample` 开一个编造的活动，
回到主屏拍紧凑态（隔 0.4 秒两张，比对有没有动画）、长按灵动岛拍展开态（长按偶尔不生效，会再试一次存成 `expanded2`）、锁屏再点亮拍锁屏（首次会有系统的「允许实时活动」提示，测试会点允许），最后解锁：

```sh
TEST_RUNNER_JARVIS_FLEET_SAMPLE=needsYou TEST_RUNNER_JARVIS_FLEET_DETAILS=YES TEST_RUNNER_JARVIS_FLEET_ISLAND=custom:assistant,load xcodebuild test … -only-testing:JarvisUITests/LiveActivityShots \
  -resultBundlePath /tmp/shots.xcresult
xcrun xcresulttool export attachments --path /tmp/shots.xcresult --output-path /tmp/shots
```

`JARVIS_FLEET_SAMPLE=current` 不启动 App，只拍活动现在的样子（配合真实 APNs 推送，见 `docs/live-activity.md` 验证一节）。
和别的 App 的活动共用灵动岛：先 `scripts/island_peer.sh <模拟器 udid>` 装示例 App（`apple/IslandPeer`，只有一个「8分」的实时活动），
再加 `TEST_RUNNER_JARVIS_ISLAND_PEER=dev.example.islandpeer`，多拍 `-shared`（两个活动并存）和 `-landscape`（示例 App 在前台横屏，Jarvis 的活动单独在横屏的岛上）；
`TEST_RUNNER_JARVIS_FLEET_COUNT=12` 把编造的数量都改成 12（看两位、三位数）。iPhone 18 Pro 和 17 Pro 的规则不同，两台都要拍（`docs/live-activity.md`「和其他 App 共用灵动岛」）。
