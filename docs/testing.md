# 测试

## 命令

```sh
cd server && go test ./... && go vet ./...                    # 服务端
cd apple && xcodegen generate -q && xcodebuild -project Persona.xcodeproj -scheme Persona \
  -destination 'platform=iOS Simulator,name=iPhone 17 Pro' test   # iOS 单测（ChatStore、ScreenParser、手机工具注册表）
./scripts/app/sim.sh [-openSession <machine>/<id>]                    # 模拟器里跑，自动配对真实 hub
```

transcript 解析用真实样本做 golden 测试（`server/internal/transcript/testdata/`，`fixtures_test.go`）：每份样本的解析结果、
「边写边读 = 一次读完」、「往前翻页不漏不重」。改了解析跑 `go test ./internal/transcript -run Fixture -update`，把 golden 的 diff 当评审看。
加样本前先 `scripts/diag/redact_transcript.py` 脱敏、人工复核（`testdata/README.md`）；Claude Code / Codex 升级后跑
`scripts/diag/transcript_stats.py` 看有没有解析器不认识的新类型。

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

## 安卓截图

`./scripts/app/android.sh shots -s emulator-<端口> --out <目录>`：在专用模拟器上装独立的测试包，用匿名化的 `scripts/mock/mock_hub.py` 自动配对，
四个 tab 和主要页面按浅色、深色各截一张（`docs/android.md`「假 hub 批量截图」；只挪代码不改样子的重构用同一节的「改前改后逐像素比对」，比对用 `scripts/app/android_shots_diff.py`）。
改了界面还要在三套界面风格（玻璃 / 晨报 / 看板）下看（`docs/android-design.md`），平板 / 折叠屏的截图办法见 `docs/lessons/android.md`。
手势、键盘、通知、真实网络要在真机或模拟器上看。

## 用 mock 数据截图（README / 宣传图）

截图不能带真实的业务数据：`scripts/mock/mock_hub.py` 是一个只返回编造数据的假 hub（三台机器、几个项目的 Agent、助手对话、
一个提问、一个计划审批、飞书 + 邮件的收件箱），够主要界面用。

真 hub 的收件箱端到端（分拣、事件合成、AI 分身的判断）用 `scripts/mock/mock_model.py`：一个假的 Anthropic Messages 接口（`POST /v1/messages`），
按提示词回固定的 JSON（事件合成把这一批都并进输入里第一件开着的事，没有就新开一件；有问号的算需要你），`POST /_mode` 切成失败、没额度、
不合格的结果，`GET /_calls` 看收到了什么。hub.json 的 `inbox.triage_ai`（事件合成也用它）和 `judge_ai` 指过去，用法写在文件开头；`scripts/mock/test_mock_model.py` 管它。

```sh
python3 scripts/mock/mock_hub.py &                                  # 127.0.0.1:18760
SIM=$(xcrun simctl create "Jarvis Shots" com.apple.CoreSimulator.SimDeviceType.iPhone-17-Pro <iOS 26 runtime>)
xcrun simctl status_bar $SIM override --time "$(date +%H:%M)" --batteryState charged --batteryLevel 100 --wifiBars 3 --cellularBars 4
xcrun simctl launch --terminate-running-process $SIM <bundle id> \
  -pairURL "jarvis://pair?url=http%3A%2F%2F127.0.0.1%3A18760&token=mock" [-tab dev|mail] [-openSession <machine>/<session>]
```

- **新建一台模拟器**，别用连过真实 hub 的：App 的磁盘缓存（对话、收件箱）会把真实数据带进截图。
- mock 的条目 id 是固定的；改了 mock 数据（或 id）后要卸载重装 App，否则旧缓存和新数据合在一起，每一步出现两次。
- Agent 对话页默认把工作过程折成一行，`-openAllWork YES` 展开。`-tab` 没有对话 tab，App 会停在上次的 tab：
  要拍对话 tab 就先卸载重装（新装默认在对话 tab）。
- 首页问候按模拟器的真实时间（「晚上好」），状态栏时间也用当前时间，别固定成 9:41。
- 深色：`xcrun simctl ui $SIM appearance dark`。
- 操控电脑（docs/computer-use.md）：`MOCK_COMPUTER=ready|permissions|both|locked|missing` 是插件 `cua` 打开、MacBook 已打开（就绪 / 缺屏幕录制权限 / 两项都缺 / 锁屏 / 没装：「安装 Cua Driver」按钮按时间走完下载、校验、关统计、授权、点了允许，每步 `MOCK_COMPUTER_STEP` 秒，默认 2）；加 `MOCK_WEB=web/dist`（先 `npm run build`）托管网页的VNC页，并把 MacBook 的 5900 转到这台 Mac 自己的屏幕共享（UI 测试带 `TEST_RUNNER_JARVIS_COMPUTER_SCREEN=1` 才跑这一条），Mac Studio 还没打开；`=off` 是插件开着、哪台都没打开。再加 `MOCK_PLUGINS=1`，插件列表里有「操控电脑（Cua）」，它的「能操控的电脑」跟着各台 Mac 的 `computer.enabled` 双向同步（`-tab settings -openPlugin cua` 直接打开）。`-tab settings -openMachines YES -openMachineDetail macbook` 直接打开 MacBook 的机器页。再加 `MOCK_COMPUTER_OPERATING=1`，助手的「用计算器算 6×7」和 Agent widget 的对话里各有一个还在跑的操作，输入框上面出「正在操作 MacBook · …」（`ScreenDrawerTests`）。再加 `MOCK_DEVICE_TOKEN=jd_…`（UI 测试带同样的 `TEST_RUNNER_JARVIS_DEVICE_TOKEN`，`UITestToken`），mock 的 `/api/` 只认这一个 token（`Authorization` 或 `?token=`），别的回 401 invalid token，和 hub 一样（#1498：VNC的页面拿到编了两次的 token）。`MOCK_COMPUTER_APPS=1`：MacBook 有两个已允许助手操作的 App（计算器、整个屏幕），机器页列出来，可以移除（`ComputerSectionTests.testAllowedAppsCanBeTakenBack`，带 `TEST_RUNNER_JARVIS_COMPUTER_APPS=1`）。「插件开着时不自动锁屏」：机器页的开关打开前先问（`ComputerSectionTests.testNoAutoLockAsksFirst`），插件页的按机器开关看 `x-confirm` 先问（`PluginMachineSwitchesTests.testAListWithAConfirmAsksFirst`，`MOCK_PLUGINS=1`），mock 把插件页的 `no_auto_lock` 同步到各台的 `computer.no_auto_lock`。`MOCK_SCREEN=fake`：5900 不转到这台 Mac 的屏幕共享，而是 mock 自己的一个（像 macOS 一样先给 ARD 登录（有 openssl 时），再给 VeNCrypt Plain，账号 `me`、密码 `mock-pass`，`GET /mock/screen-password?p=…` 改密码，`&clear=1` 清掉记下的按键；它记下登录后按的键，收到正确的密码和回车就把锁屏的 MacBook 改成解开），连上后画一帧桌面，客户端要 ZRLE 就给 ZRLE；记下指针、点击、剪贴板（node 的 `GET/POST …/computer/clipboard` 读写的也是这一份），`?clip=…` 往客户端发剪贴板，`MOCK_SCREEN_ANIMATE=1` 画面里一个方块约每秒 30 帧地动（`ScreenPasswordTests`、`VNCTests`、原生客户端，UI 测试带 `TEST_RUNNER_JARVIS_FAKE_SCREEN=1`；模拟器没有画中画，「VNC」退回整页，小窗只能真机看）。
- 浏览器（docs/plan-ego-browser.md）：`MOCK_BROWSER=missing|setup|ready` 是插件 `ego` 打开、MacBook 已打开（没装 / 装了没做首次设置 / 就绪，统计开着；`MOCK_BROWSER_ANALYTICS=off` 是关着），Mac Studio 没打开、没装；「安装 ego lite」「打开 ego lite」按时间往前走（每步 `MOCK_COMPUTER_STEP` 秒）。加 `MOCK_PLUGINS=1`，插件列表里有「浏览器（ego lite）」（`-tab settings -openPlugin ego`）。
- 主对话统一入口（docs/unified-entry.md）：`MOCK_TASKS=1` 启动 mock，主对话里有开始了的计划、在做 / 等你 / 做完的三个任务、一个 Agent 没开起来的任务（红色卡片和回执，#1077）、
  答过的问题、回执、直接对任务说的话和最后一张待开始的计划卡；`MOCK_TASKS_AT=<分钟>` 把对话截到那么多分钟以前（更新的条目不要），
  不用滑动就能在屏幕底部看到更早的部分。`MOCK_TASKS_NO_ASKS=1` 让任务的 Agent 不提问，输入框上方不再被问题卡占住。`MOCK_TASKS_DOCK=2` 只留两个在做的任务（任务坞两个任务胶囊，后面还有两个手动开的 Agent），`=pair` 只留一个任务和一个等你的手动 Agent（两个胶囊），`=many` 再加四个任务（任务坞比屏幕宽，看「全部」固定在最左边）；不设时是四个没做完的任务加两个手动开的 Agent（#1113）。`-showTaskDetail <待办 id>`（如 `todo_task_widget`）打开任务详情，`-showTaskBoard YES` 打开任务板。
  任务提问的回答记录（#1231）：mock 里「补齐网络层的单元测试」那行有三个问题的完整问答，「修小组件横屏崩溃」那行是只有摘要的老记录；`MOCK_TASKS=1 MOCK_TASKS_NO_ASKS=1 MOCK_TASKS_AT=23` 让它们停在底部，`AnsweredLineTests`（`JARVIS_SHOTS` 写折叠 / 展开两张图）点开再收起。
- 离开期间回顾在消息流里（#1360）：`MOCK_RECAP_LONG=1` 启动 mock，「检查 PR #42 的改动」前面多出八轮对话、比一屏高，结尾是一条回顾；`TEST_RUNNER_JARVIS_MOCK_HUB=<地址> xcodebuild test … -only-testing:PersonaUITests/RecapInFlowTests` 查展开的回顾在回复和输入框之间、往上翻跟着滚走、点了收成小条、重开还是关着（`-openRecapsFresh YES` 忘掉关过的回顾）；`TEST_RUNNER_JARVIS_SHOTS=<目录>` 和 `TEST_RUNNER_JARVIS_SHOTS_APPEARANCE=light|dark` 写展开 / 收起 × 贴底 / 往上翻四张图。`MOCK_RECAPS=1` 看三条折成一行时更早的两条留成小条。
- 截断的长输出：`-openSession macbook/a1f3c2d4-0000-4000-8000-000000000002 -openAllWork YES -showFullOutput card`（展开「运行」卡片，
  看「已截断，看全文」）或 `sheet`（再打开全文浮层，mock 的 `/items/{id}/full` 回全文）。
- 往上翻时内容不动（`ChatView` 的冻结）：`MOCK_STREAM=1` 启动 mock（第一个会话打开后每 2 秒来一条新步骤 / 回复，共 12 次），
  `-openSession macbook/a1f3c2d4-0000-4000-8000-000000000001 -openAllWork YES -showFreezeProbe YES`：App 自己往上翻，每 2 秒记一次屏幕中间那行的位置，
  结果写在 App 容器的 `Documents/freeze.txt`（`xcrun simctl get_app_container $SIM <bundle id> data`），最后一行 `RESULT:`，应为移动 0pt。
- 新建 Persona 对话（#511）：mock 照 hub 的样子做，会话列表立刻推，transcript 先是空的，`MOCK_ASSISTANT_TRANSCRIPT_SECONDS`（默认 1）秒后才有第一句并推给打开的对话。
  App 里发出后对话页一直是这句（不出「还没有消息」）、侧栏的新对话选中、带工作中转圈。`MOCK_ASSISTANT_START_SECONDS=<秒>` 让这个 POST 晚回，
  等待中看气泡和「Persona 正在准备…」。`scripts/mock/test_assistant_mock.py` 管着 mock 的这几件事。
- 飞书多账号（#1321）：mock 加 `MOCK_LARK_ACCOUNTS=1`，`/api/v1/lark/accounts` 有「我」「工作号」和一个未登录的账号，`ol_account=2` 读到工作号自己的会话，收件箱最上面一件来自工作号；`TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:<端口>` 跑 `LarkAccountsFlowTests`（「消息」页切换账号、打开工作号的会话、设置 › 飞书的账号列表；`JARVIS_SHOTS` 存截图）；网页用 `scripts/test/web_lark_accounts_e2e.py <网页地址> [--shots <目录>]`（用法见脚本开头）。
- 飞书加密消息（#689、#1664）：mock 加 `MOCK_LARK_NATIVE_OLK=1`，`TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:<端口>` 跑 `LarkNativeEncryptionFlowTests`（「消息」页打开「设计评审」、选对外展示、解锁、发送、失败重发、收件箱不能把加密草稿当普通消息发；解锁后查不弹「保存密码」）；`JARVIS_SHOTS` 存截图（`docs/screenshots/lark-native-encryption/`）。多账号的用例里第二个账号置顶了「季度规划」，它只在置顶栏（`feishu-pin-w1`），不在列表里（#1342）。
- 没装 lark-connector 插件的 hub（#1530）：mock 默认在 features 里带 `lark`、`lark_moments`（插件给的）；加 `MOCK_NO_LARK=1` 去掉它们和 `lark_attachments`，`/api/v1/lark/…` 一律 404、连接器里没有飞书，看「消息」页、设置 › 连接器 › 飞书、翻译的「飞书会话」都不出现，飞书文档 / 公司圈链接照普通网页打开。
- 飞书 /agent 的上下文（#876）：mock 加 `MOCK_LARK_AGENT=1`，「帮我约周五…」变成一条飞书 /agent 请求（说明和 7 项上下文在 `<jarvis-context>` 里，标签外只有请求）；`-openSession server/b2e4d6f8-0000-4000-8000-000000000003`，`-showContexts YES` 让上下文组和第一项打开。
- Persona 侧栏：mock 加 `MOCK_ASSISTANT_SIDEBAR=1`，「最近」多 36 个对话（超过显示的 30 个，出「查看全部」），前三个依次是需要你（红点）、未读（蓝点）、工作中（转圈）。
- 跟到最新（`ChatFollowTests`，`MOCK_LONG_HISTORY=1`）：长会话打开在底部；在底部时来回复会跟到底（Persona 页的回复先按 `delta` 流式出）；往上翻出现「回到最新」、
  这时来回复不拽人；点了回到底部、之后照常跟随。Agent 对话页分四种打开方式测：普通、完成未看（`-showAsUnread YES`）、停在上次的位置（`-chat.read.<会话> <行 id>`），以及有磁盘缓存时冷启动回到离末尾很远的位置（mock 开 `/mock/paging?on=1` 像 hub 一样分页、`/mock/delay?s=3` 让缓存先上屏，#597；`/mock/reset` 会把两者关掉）。
  回复由 mock 的 `GET /mock/say?text=…&user=…&stream=1` 推给所有打开的对话；列表在哪由 App 的 `-showScrollState YES` 报（无障碍标签
  `chat.scrollState`：bottom / follows / frozen / arrow / newestShown / withheld…，`visible` 时画在屏幕上），XCUITest 读不准倒置列表里行的位置。
  `testRealHubChats` 连真实 hub 拍改前改后（`TEST_RUNNER_JARVIS_REAL_PAIR` 等环境变量，不给就跳过；只开自己的会话，打开会标已读）。
- 终端实时流：`MOCK_TERMINAL_STREAM=1` 启动 mock（hello 带 `terminal_stream`，订阅后先发一个编造的 Claude 界面整帧，再每秒一帧让 spinner 走秒；
  再加 `MOCK_TERMINAL_FIXTURE=apple/PersonaTests/Fixtures/terminal-observe.jsonl` 改为回放从 herdr 录下的真实帧：颜色、框里的中文、emoji、链接、200 字的长行），
  `-openPane terminal`，`-terminalWrap NO` 看不换行，`-terminalPages 1` 加载一页（mock 的 `/screen/before` 回一段带颜色的历史）。
- 手机接管终端：再加 `MOCK_TERMINAL_CONTROL=1`（hello 带 `terminal_control`，按 App 给的列 × 行排编造的 Claude 界面，打的字出现在输入框，
  `GET /mock/terminal_input` 看收到的字节），`MOCK_TERMINAL_BUSY=1` 看「电脑上另一个程序正在使用这个终端」的弹窗；`-terminalSize phone|mirror` 选模式。
  UI 测试 `TerminalTypingTests/testDrivingTheTerminalSendsItsBytes` 要连这样的 mock；其余终端 UI 测试固定 `-terminalSize mirror`。
- 设置 › 插件：`MOCK_PLUGINS=1 PORT=<独立端口> python3 scripts/mock/mock_hub.py`；`TEST_RUNNER_JARVIS_PLUGINS_HUB=http://127.0.0.1:<端口>` 跑 `PluginSettingsTests`（列表、填表被 hub 拒绝、填密钥后变成运行中）、`PluginSlotsTests`（UI 插槽：设置首页的卡片和入口、用量页、机器上的一节），
  每轮前重启 mock（状态在内存里）。截图同上：`TEST_RUNNER_JARVIS_SHOTS`、`simctl ui … appearance`、`TEST_RUNNER_JARVIS_PLUGINS_APPEARANCE` 命名，两种界面风格都跑。
  新模拟器第一次会弹通知授权，测试自己点掉。
- Git 拉取：`MOCK_FILES=1 MOCK_GIT_PULL=1 PORT=<独立端口> python3 scripts/mock/mock_hub.py`；`TEST_RUNNER_JARVIS_GIT_HUB=http://127.0.0.1:<端口>` 跑 `GitPullUITests`，从落后 28 个提交到成功刷新、再次检查和失败后重试。
  `TEST_RUNNER_JARVIS_SHOTS=<目录>` 保存截图，分别用 `simctl ui <udid> appearance light|dark` 切换实际外观，两种界面风格都跑；`TEST_RUNNER_JARVIS_GIT_APPEARANCE` 仅用于截图命名。
  `GitPullTests` 使用 `FakeHub` 检查 POST 请求、结果解码、反馈与重复点击；服务端 `TestGitPull*` 使用隔离 HOME / Git 配置的临时本地远端，验证未 fetch 的最新提交、fast-forward、分叉、已修改 / 已暂存 / 未跟踪文件、已有 stash 和失败状态，不拉取真实工作区。
- 文件查看器（`docs/ios.md`「文件查看器」）：`MOCK_FILES=1` 启动 mock，给出一份编造的 `/Users/me/code/weather`：每种语言一个小文件、
  3 MB 的 JSON、2 万行的 Go、4000 多行的 README、5 MB 的日志，`GET …/file`（2 MB 一页）和 `…/stat` 都按它回答；「给天气 App 加主屏幕小组件」
  最后一条回复提到其中几个文件，和两个不在的路径。UI 测试 `FileViewerUITests` 要连这样的 mock。测耗时和帧率：用 `-O` 编一份
  （`SWIFT_OPTIMIZATION_LEVEL=-O`，另一个 DerivedData），带 `JARVIS_FILE_PERF=1` 跑 `FileViewerPerfTests`，再读 App 容器里的
  `Library/Caches/file-perf.log`。滚动用 App 内的匀速滚动测，别用 XCUITest 拖：它每个手势前在 App 主线程上抓一次无障碍快照，自己就是 40–100 ms 的长帧。
  改了 mock 里某条消息的文字要先卸载 App：App 按 item id 缓存会话，同 id 的新文字不会盖掉旧的。
- 表格查看器（网页 #1310）：mock 加 `MOCK_FILES=1 MOCK_SHEETS=1`，会话目录多出 `data/stations.csv`（3000 行，带引号、换行、中文）和 `reports/季度报告.xlsx`（三个工作表、合并标题、列宽、数字和百分比格式），文件面板里点开；二进制在 `file?stream=1` 时只回描述，内容从 `file/download` 取。
- Office 预览（网页 #1345）：mock 再加 `MOCK_OFFICE=1`，`reports/` 下多出手写的 `季度报告.docx`（标题、表格、图片、页脚）、`季度回顾.pptx`（主题、母版、版式占位符、项目符号、图片、形状、表格、图表、备注）、`旧版说明.doc`（mock 的 `file/convert` 回一份 PDF）和 `路线图.pages`（回 `no_converter`）。
- Mermaid（网页 #1376）：mock 加 `MOCK_MERMAID=1`，widget 对话末尾多一条带流程图、时序图、饼图和一张写错的图的回复。
- Jupyter（网页 #1407）：mock 加 `MOCK_NOTEBOOK=1`，`notebooks/收入分析.ipynb` 有 Markdown + 公式、HTML 表格、stdout / stderr、PNG 图和带 ANSI 颜色的报错。
- HTML 预览（网页 #1413）：mock 加 `MOCK_HTML=1`，`reports/周报.html` 带样式、一段改文字的脚本（默认显示「脚本没有运行」）、一张远程图片（应被拦）和同目录的 `chart.png`、`周报.css`（应读入，工具条写「读入了 2 个」）。
- 媒体（网页 #1422）：mock 加 `MOCK_MEDIA=1`（`media/` 下一段 WAV、一个带脚本的 SVG、一个浏览器画不了的 HEIC，`file/thumb` 给它画一张替身），再配 `MOCK_VIDEO_FILE=<webm/mp4>`、`MOCK_SENT_VIDEO=<同上>` 看文件页的视频和对话里的视频卡片（无头 Chrome 放 WebM 最稳）。
- 对比（网页 #1437）：mock 加 `MOCK_COMPARE=1`，Git 面板多出改过的 `reports/季度报告.docx`、`reports/季度回顾.pptx`、`assets/banner.png`，`git/blob` 回它们暂存区 / HEAD 的旧版。
- 这一轮看过的图片（网页 #1540）：mock 加 `MOCK_TURN_IMAGES_WEB=1`：「检查 PR #42」最后一轮折起来的工作里有 9 张（Read、截图、网页截图、MCP、Codex view_image），「整理发布说明」最后一轮 1 张（卡片），widget 对话正在工作的一轮 2 张。
- 文件预览里的链接（`FilePreviewLinkTests`）：`MOCK_FILES=1 MOCK_FILE_LINKS=1 MOCK_PORTS=1 PORT=<独立端口> python3 scripts/mock/mock_hub.py`，`TEST_RUNNER_JARVIS_FILE_LINK_HUB=http://127.0.0.1:<端口>`。从旧布局的会话文件块（固定 `-ui.oldLayout YES -tab dev`，避免隐藏助手响应全局 DEBUG 参数）与默认新外壳的「文件」页打开 README，链接必须在关闭文件前立即可点；关闭后返回原文件位置，重复打开同一链接正常，关闭文件后不补弹旧链接。开发预览还要实际读到经 hub 转发的网页文字。
  两种风格分别切实际 light / dark 外观运行；`TEST_RUNNER_JARVIS_SHOTS=<目录>` 保存截图，`TEST_RUNNER_JARVIS_FILE_LINK_APPEARANCE` 仅负责文件命名。
- 视频预览：合成一个大于 2 MiB 的 H.264/AAC MP4（例如 `ffmpeg -f lavfi -i testsrc2=size=640x360:rate=30 -f lavfi -i sine=frequency=440:sample_rate=44100 -t 12 -c:v libx264 -b:v 3M -pix_fmt yuv420p -c:a aac -movflags +faststart /tmp/preview.mp4`）。mock 加 `MOCK_FILES=1 MOCK_VIDEO_FILE=/tmp/preview.mp4`，`VideoFileUITests` 检查原生播放器打开和重开（`TEST_RUNNER_JARVIS_VIDEO_HUB` 可单独指定视频 mock，避免新增视频回复改变原文件回归的可见区域）；用 `-openFilePath /Users/me/code/weather/media/preview.mp4` 单独检查两种风格的深浅色截图。用 `xcrun simctl ui <udid> appearance light|dark` 真正切换模拟器外观，再分别跑视频 UI 测试；`TEST_RUNNER_JARVIS_VIDEO_APPEARANCE` 只给截图命名，不改变外观，不能用 iOS 不生效的 `AppleInterfaceStyle` 启动默认值冒充切换。单测环境加 `TEST_RUNNER_JARVIS_VIDEO_FIXTURE=/tmp/preview.mp4`，`VideoFileTests/nativeVideoPlaysSeeksAndCleansUp` 验证完整字节、播放时钟推进、跳到 8 秒和临时文件清理；不传文件时该集成用例跳过，其他类型、下载及失败测试照常跑。node/hub 的 `TestDownload*` 验证大文件有界读取、Range、旧 node、无效范围、文件变化和取消。
- 发送视频（#981，docs/protocol.md「发送视频」）：用 AVAssetWriter 合成一段 6 秒的 H.264 MOV（没有 ffmpeg 时也行，画面是渐变加一道扫过的竖条，不用真实视频），`xcrun simctl addmedia <udid> <文件>` 放进模拟器图库，
  另一段给 mock 的 `MOCK_SENT_VIDEO=<文件>`（对话里多一条「别的设备发的」视频卡片，文件只在电脑上）；mock 收到的视频上传会留在它的文件里，可以再下载。
  `VideoSendTests` 走真实的 PHPicker：Agents 对话和助手新对话各选一次视频、看上传进度和传完的卡片、发送后查 `/mock/sent` 里的 `<jarvis-file>` 标签、点消息里的卡片打开系统播放器；
  另一条点只在电脑上的卡片，先下载再播放。环境变量 `TEST_RUNNER_JARVIS_VIDEO_SEND_HUB`，`TEST_RUNNER_JARVIS_LAYOUT=shell` / `TEST_RUNNER_JARVIS_UI_STYLE=beautiful` 换布局和风格，
  `TEST_RUNNER_JARVIS_SHOTS=<目录>` 存截图；`MOCK_UPLOAD_KBPS=640` 让进度条看得见。单测 `VideoAttachTests` 合成一秒的视频，验证原样传、转出来是 H.264 MP4 且时长不变、放不下时拒绝。
  视频修复最终验证（2026-11-03）：两种风格各 544 个 iOS 单测、4 个目标文件/视频 UI 用例通过；视频从前台对话文件块打开，另在实际切到浅色/深色的四个风格组合下验证关闭和重开，截图已实际检查。4,116,551 字节的合成视频完整下载、播放时钟推进、seek 到 8 秒、临时文件清理通过。最终服务端全套在隔离真实 Postgres 上通过；iOS（含手表/扩展构建依赖）和 Mac 在 Xcode 27.1 编译通过。精确 Xcode 26.0、真机及用户原始视频未验收，不作为已发布或原 case 已修复的证明。初轮无签名构建的钥匙串测试失败、新外壳隐藏助手误执行 `openFile` 导致旧文件 UI 失败（已补到 #412）、合并 fixture 挤走文件块，以及外观标签与实际截图不符，都保留为无效或失败记录，没有改断言掩盖。
- 形象（`docs/persona.md`）：`MOCK_PERSONAS=demo` 启动 mock 时用 `scripts/assets/make_persona.py demo` 现画一个原创的演示形象「小薄荷」；
  `MOCK_PERSONAS=<目录>` 则列出那个目录里的包（看别的形象的效果，包不进仓库）。UI 测试 `PersonaPickerTests` 要连 `MOCK_PERSONAS=demo` 的 mock。
  `LookPickerTests`（形象、主题色、界面风格三个选择互不影响）要连 `MOCK_PERSONAS=<目录>` 的 mock，目录里要有一个带配色、id 为 `doudou` 的包；
  设了 `JARVIS_LOOK_SHOTS=<目录>` 就把「设置 › 外观」下主题色、界面风格两页和设置首页的截图存进去（深浅色跟模拟器的外观）。它会改 App 自己存的 `ui.style` / `ui.color`，结束时点回液态玻璃 + 跟随界面风格。
  看某个组合直接用启动参数 `-ui.style clay -ui.color persona -openPersona doudou`。
- hub 的版本和更新（`docs/hub-update.md`「App 里」）：`MOCK_HUB_UPDATE=available|waiting|notice|docker|docker-pinned` 启动 mock：有新版本（自动更新关着）、
  更新在等两个在忙的 Agent、第一次告知「自动更新已开启」、Docker 的 hub 有新镜像（用 `:latest`，或钉在某个版本的标签上）；不设时 `GET /api/v1/hub/update` 是 404（老 hub，哪里都不出现）。
  `GET /mock/hub_update?mode=…` 运行中换一种并从头开始。看页面用 `-tab settings -openHubUpdate YES`、`-openSkills YES`，提示条在 Agents 页。
  UI 测试 `HubNoticeUITests`（提示条上的按钮各管各的、「现在就更新」先二级确认）要 `TEST_RUNNER_JARVIS_HUB_UPDATE_HUB=<mock 地址>`。
- 接管的真实链路（不用部署）：在 hub 机器上编一个 darwin 的 personad，本机起一个测试 hub（假 HOME、`HERDR_SOCKET_PATH` 指向真 herdr 的 socket、
  单独的库 `jarvis_termlab` 经 SSH 隧道、`machines` 只有一台 `kind: local`），模拟器配对它，开 lab 窗格的终端（`docs/terminal.md` 第十一节的实测就是这么量的）。
- 终端断线：`MOCK_SCREEN_FAIL_AFTER=3:14` 启动 mock（第一次读终端 3 秒后每次都 503，再过 14 秒恢复），
  `-openSession macbook/a1f3c2d4-0000-4000-8000-000000000001 -openPane terminal`，应先出现红色「上次更新 N 秒前 · 重试」，恢复后消失。
  mock 进程启动时还没有读终端的请求，计时从第一次请求算：编译期间旧 App 还在轮询的话，先启动 App 再启动 mock。
- 断档自愈（`docs/protocol.md`「items 的连续性」）：`MOCK_GAP=1` 启动 mock 并卸载重装 App，`-openSession macbook/a1f3c2d4-0000-4000-8000-000000000001`。
  这个会话先只有前 8 行，2 秒后「写入」后 6 条，但只推最后 2 条（`from_seq` 12）；日志里应在约 1.5 秒后出现 `items after_seq=8`，
  对话页的工作过程显示「工具 5」。等日志用带超时的脚本，别写只等成功的 `until grep`。
- GitHub 的 thread 页（#1055）：mock 带 `MOCK_GITHUB_INBOX=1` 起，`-tab mail -openInboxItem github:review12` 打开编的 PR example-org/example-repo#12：
  页头（状态、分支、三个标签）、Markdown 描述、要求修改和批准的 review、行内评论和它的回复、三项检查（test 失败带日志）、事件行；
  发的评论在 `/mock/sent`，并加到时间线末尾。`github:ci7`（CI 失败）不是 PR，打开是 hub 收集的文本。数据在 `mock_hub.py` 的 `gh_thread_page`。
- 飞书会话页：`-tab mail -openInboxItem lark:f3:300`（收件箱的「周末徒步群」）打开一个画全了的会话：三天的消息（日期分隔）、第一条未读前不止一页（往上翻、`has_older`）、
  图片、引用回复、表情回复、机器人、一个 75 条回复的话题（话题浮层翻页）；从第 300 条起 12 条未读，已读到 299（`read_position`）。
  `MOCK_LARK_LOGGED_OUT=1` 启动 mock 时会话列表、会话和话题都回 502「lark-connector 未登录飞书」（带 `code: lark_not_logged_in`，和 hub 一样），看错误态。数据全是编的，在 `mock_hub.py` 的 `_lark_f3`。
  已读回执（`POST …/read-states`）：f3 里我的消息 282 全部已读、296 部分已读、306 都未读；私聊 f1「韩梅梅」（`-openInboxItem lark:f1:100`）里我的消息有已读、未读（103）和没有回执（104）三种，mock 运行中新发的消息算都未读。
  头像：f3 里 Tom 的头像能加载，天气助手的头像地址 404（看加载失败退回首字），其他人没有头像。
  @人：f2、f3 有成员（`GET …/members`，Tom 和 Mia 有头像，阿杰的群昵称是「杰哥」）；带 `mentions` 发送或回复时，mock 把文字里的 `@name` 存成 `at` 片段。
  话题群（#1087）：`MOCK_LARK_TOPIC=1` 启动 mock，`-tab mail -openInboxItem lark:f4:405` 打开「版本发布」话题群（消息页里也有）：发布机器人隔两分钟连发三个话题（各自一张卡片）、一个高过 320 pt 要收起的长卡片、我发的话题、带链接的话题，0 / 1 / 2 / 6 条回复的话题，还有撤回的话题（lark-connector 留存了和没留存的各一个）和编辑过的话题（最新一条回复是撤回的），给「风灵月影」用；话题的回复和 lark-connector 一样先回话题本身、再回最新的 `limit` 条。不要和 `MOCK_LARK=1` 同开：那时飞书的读取先由它接管，「版本发布」不出现。

## UI 测试（`scripts/app/ui-test.sh`）

`apple/PersonaUITests` 在模拟器里驱动真实 App、连配置里的 hub（所以不在默认的单元测试里跑）：
`SIM=<udid> ./scripts/app/ui-test.sh [machine/session]`。目前有边缘右滑返回（顶栏展开 / 收起两种状态）。
长按消息的操作条（`MessageMenuTests`，按 0.4 / 0.8 / 1.5 / 2.5 秒松手都要留下，点列表、滑动会关）连 `scripts/mock/mock_hub.py`，不用真实 hub：
`TEST_RUNNER_JARVIS_MOCK_URL=http://127.0.0.1:18760 xcodebuild test … -only-testing:PersonaUITests/MessageMenuTests`。
写 UI 测试时先在没有修复的代码上跑一次确认它会失败，再上修复。

通知按钮（`NotificationActionTests`）不连真实 hub，连 `scripts/mock/mock_hub.py`：
`PORT=18791 python3 scripts/mock/mock_hub.py &`，再 `TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:18791 xcodebuild test … -only-testing:PersonaUITests/NotificationActionTests`。
它在 SpringBoard 上长按横幅、点「拒绝」，再读 mock 的 `GET /mock/resolved` 确认 hub 收到了 resolve。
`LiveActivityActionTests` 同样连 mock：开一个示例实时活动、锁屏、点活动上的「拒绝」。模拟器系统语言是英文时授权弹窗的按钮叫「Allow」；
授权会保留到下次，所以 App 要等进了后台才排这条通知，否则它在测试还在等弹窗时就弹过去了。

终端直接打字（`TerminalTypingTests`：点终端弹出键盘、打的字和回车按顺序发出且被合并成少数几个请求、键栏的 ctrl 作用于键盘打的下一个字、键盘按钮收起键盘）同样连 mock。
`MOCK_TERMINAL_INTERVAL=0.02` 让 mock 一口气回放录制的帧（默认每秒一帧），配 `MOCK_TERMINAL_FIXTURE=apple/PersonaTests/Fixtures/terminal-observe-mixed.jsonl`
（中英混排长回答）或 `-permission.jsonl`（权限框）截终端的样子。
终端键栏和选择（`TerminalKeysTests`：单击 / 双击锁定 / 按住 ctrl、alt、shift，输入框里打的字带 ctrl，⌃C，发出的键名和顺序；长按出「拷贝」；快捷命令双击直接发送、输入框的补全）也连 mock：
`PORT=18792 python3 scripts/mock/mock_hub.py &`，再 `TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:18792 xcodebuild test … -only-testing:PersonaUITests/TerminalKeysTests`
（mock 的 `GET /mock/keys` 回放收到的按键）。

从搜索结果打开会话只有一行标题、返回后搜索结果还在、搜索时折叠的空间里的 Agent 也搜得到点得开（`SearchOpenTests`，另有不搜索的对照，对照里再右滑返回列表；
测试带 `-openAgentsFresh YES`，不受别的测试留下的折叠影响）连普通 mock：
`PORT=18793 python3 scripts/mock/mock_hub.py &`，`TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:18793 xcodebuild test … -only-testing:PersonaUITests/SearchOpenTests`。

助手的 `ask_user` 在对话里画成「提问」（`AskUserRowTests`，#677）：工具行是「提问 + 第一个问题」不是原名，展开后列出问题和选项、勾上用户选的（含多选），
用户没回答的写「没有回答」；测试带 `-openAllWork YES -showToolDetails YES` 把工作行和工具行都展开（倒着的对话列表里的行点不准）：
`MOCK_ASK_USER=1 PORT=18797 python3 scripts/mock/mock_hub.py &`，`TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:18797 xcodebuild test … -only-testing:PersonaUITests/AskUserRowTests`，
截图在 `/tmp/persona-ask-user-*`（`TEST_RUNNER_SHOT_SUFFIX` 区分深浅色和风格）。

待办详情的「当前没有 Agent」（#1110）：`MOCK_TODO_OFFLINE=1` 给「天气 App 发 1.3 版」多挂一个断连的 NAS 上的项目 backup，那里的会话状态是 unknown；
`-openSession macbook/a1f3c2d4-0000-4000-8000-000000000002 -openTodoDetail YES` 打开详情，backup 那行标「当前没有 Agent」，weather 那行不标。

端口转发（`PortForwardTests`，`docs/ports.md`）：会话顶部和回复里的 `:端口` Badge 带页面标题、停掉的变灰，点开在网页卡片里经 mock 的
`…/ports/{port}/connect` 加载 mock 自己起的页面（`GET /mock/ports` 看连接记录），点灰的提示「已经没有在监听」：
`MOCK_PORTS=1 PORT=18791 python3 scripts/mock/mock_hub.py &`，`TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:18791 xcodebuild test … -only-testing:PersonaUITests/PortForwardTests`；
深浅色 × 新外壳 / 旧布局用 `TEST_RUNNER_JARVIS_LAYOUT`、`TEST_RUNNER_SHOT_SUFFIX` 各跑一遍，截图在 `/tmp/jarvis-ports-*`。

Telegram 会话页的回复框（`ConnectorThreadReplyTests`，#1190）：发出去马上出现在页面里，页面上没有「确认卡片」的提示、也没有「允许」：
`MOCK_TELEGRAM=signed_in MOCK_TELEGRAM_INBOX=1 PORT=18857 python3 scripts/mock/mock_hub.py &`，
`TEST_RUNNER_JARVIS_MOCK_HUB_TELEGRAM_THREAD=http://127.0.0.1:18857 xcodebuild test … -only-testing:PersonaUITests/ConnectorThreadReplyTests`。

扫码登录（`QRSignInTests`，#1351）：`MOCK_WECHAT=signed_out PORT=<独立端口> python3 scripts/mock/mock_hub.py &`，
`TEST_RUNNER_JARVIS_MOCK_HUB_QR=http://127.0.0.1:<端口> xcodebuild test … -only-testing:PersonaUITests/QRSignInTests`：设置 › 微信 › 登录出二维码和「保存二维码」，
mock 第六次问时确认，页面自己变成已连接。mock 的登录状态留在进程里，再跑一次（比如换深色）先重启 mock；`MOCK_WECHAT_WAIT=1` 让它一直停在二维码，方便截图。`MOCK_WECHAT_QR=link` 只去掉点阵（只剩链接），`none` 连链接也去掉（画不了的步骤，#1693）。App 画不了的步骤（`testAStepItCannotDrawSaysSo`）：再起一个 `MOCK_WECHAT=signed_out MOCK_WECHAT_QR=none` 的 mock，带 `TEST_RUNNER_JARVIS_MOCK_HUB_QR_NONE=http://127.0.0.1:<端口>`，页面说明画不了、给「重新开始」，没有「下一步」。
用户自己的回复不弹卡片是 hub 定的（mock 不分设备），hub 一侧见 `TestConnectorThreadReplyFromTheUsersDevice`。

填密钥的框不弹「保存密码？」（`SecretFieldTests`，#1598）：`MOCK_PLUGINS=1 MOCK_BOT=off MOCK_SCREEN_SECRET=1 PORT=<独立端口> python3 scripts/mock/mock_hub.py &`，
`TEST_RUNNER_JARVIS_SECRETS_HUB=http://127.0.0.1:<端口> xcodebuild test … -only-testing:PersonaUITests/SecretFieldTests`：模型供应商的 API Key（自己填写、从插件添加）、飞书机器人的 App Secret、
邮箱的密码 / 授权码、终端要的密码，填完离开后系统都不弹「保存密码？」（`PasswordPrompt.assertNone`），值照样到了 mock。终端那张卡答完就没了，再跑要重启 mock。

渠道（`ChannelSettingsTests`，#1529，`docs/channels.md`「界面」）：`MOCK_CHANNELS=1 PORT=<独立端口> python3 scripts/mock/mock_hub.py &`，
`TEST_RUNNER_JARVIS_CHANNELS_HUB=http://127.0.0.1:<端口> xcodebuild test … -only-testing:PersonaUITests/ChannelSettingsTests`：设置 › 插件 的「渠道」行和摘要、
三节列表（Persona / 已接入 / 可以安装）、微信的页面（账号、消息发到、功能）、用令牌绑定（错的令牌原地报错）、飞书机器人解绑后用 6 位配对码重新配对、
主对话第一条下面的「来自 微信」。mock 的绑定状态留在进程里，换深色再跑先重启 mock；`-openChannels YES` / `-openChannel <id>` 直接打开列表 / 某个渠道。

浏览器登录（#1304）：`MOCK_BROWSER=ready PORT=<独立端口> python3 scripts/mock/mock_hub.py`，`TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:<端口>` 跑 `BrowserLoginsTests`：机器页列网站、选一个 cookies.txt（`-openCookieFile <路径>` 代替系统的文件选择器）后默认一个不勾、过期的勾不了、导入、清除；`TEST_RUNNER_JARVIS_SHOTS=<目录>` 存截图。

已知会挂、修好之前不进验收的 UI 测试先跳过，`TEST_RUNNER_JARVIS_KNOWN_ISSUES=1` 才跑，跳过的说明里写 issue 号；现在没有（`ShellRoomScrollTests` 在 #413 修好后改成常驻）。

发消息和终端对话框的安全检查（`SendSafetyTests`）也连 mock：
`PORT=18771 MOCK_SCREEN_DIALOG=1 MOCK_PROMPT_CHANGED=1 MOCK_SEND_REFUSE=input_draft python3 scripts/mock/mock_hub.py &`，再
`TEST_RUNNER_JARVIS_SAFETY_HUB=http://127.0.0.1:18771 xcodebuild test … -only-testing:PersonaUITests/SendSafetyTests`：
点终端对话框的选项，确认 hub 收到的是 `option` 而不是按键、并提示「对话框已经变了」；电脑上有草稿时点「清空并发送」，确认重发带
`clear_draft` 且 `client_msg_id` 不变（`GET /mock/resolved`、`GET /mock/sent`）。截图：`-openSendMessage <文字>` 打开会话 3 秒后发一条（`-openSendDelay <秒>` 改这个等待）。

从别的页面新建 Persona 对话（`NewConversationEntryTests`，#527）：收件箱「交给 AI」发出后，对话页立刻是这条已发送的消息，不是「还没有消息」；
transcript 晚到以后只显示一次。连 mock 并让第一行晚写：`PORT=18927 MOCK_ASSISTANT_TRANSCRIPT_SECONDS=8 python3 scripts/mock/mock_hub.py &`，
`TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:18927 xcodebuild test … -only-testing:PersonaUITests/NewConversationEntryTests`（新外壳和旧布局各一个用例）。

弱网 / 断网（`ConnectionTests`，`docs/ios.md`「连接状态」）连普通 mock：`PORT=18760 python3 scripts/mock/mock_hub.py &`，
`TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:18760 xcodebuild test … -only-testing:PersonaUITests/ConnectionTests`：hub 断 10 秒标题变「连接中…」、
恢复后换回；断 0.2 秒（重连约一秒）标题完全不变；断网时发的消息显示「连上后自动发送」，恢复后 hub 只收到一次：
中间再断几次、进后台（后台不重连，hub 回来了也要等回到前台才发）、退出 App 再打开（发件箱在磁盘上）都恰好一次。hub 重启后的去重在服务端测试（`TestSendRetryAfterRestartAndRefusal`）。
mock 的开关：`GET /mock/down?s=<秒>`（这段时间里 WebSocket 全断、新连接不应答就关）、`GET /mock/delay?s=<秒>`（之后每个请求和 pong 都晚这么久，0 关掉；
启动时也可以 `MOCK_DELAY=<秒>`）。手动测真断开：只杀监听端口的进程 `kill $(lsof -i TCP:18760 -sTCP:LISTEN -t)`——
`lsof -i :18760` 还会列出连着它的模拟器 App，一起杀掉 App 就没了。截图：`-showConnection …`（见 `docs/ios.md`）。

发送文件（`FileUploadTests`：「+」菜单里有「文件」；上传时输入框上方有进度和取消，传完路径插在打的字后面、不发送）也连 mock：
`PORT=18771 MOCK_UPLOAD_KBPS=1024 python3 scripts/mock/mock_hub.py &`，再 `TEST_RUNNER_JARVIS_UPLOAD_HUB=http://127.0.0.1:18771 xcodebuild test … -only-testing:PersonaUITests/FileUploadTests`
（`GET /mock/uploads` 是收到的文件；`MOCK_UPLOAD_OLD_NODE=1` 模拟没升级的 node）。截图：`-openUploadDemo <文件名>` 打开会话 3 秒后传一个 24 MB 的假文件。
系统的文件选择器 XCUITest 点不稳，选文件这一步没自动化。

### 机器页面（`MachinePagesTests`、`MachinePagesLiveTests`）

把机器相关的每个入口真点一遍：设置 › 机器（列表、下拉刷新、卡片 → 详情、详情里的 Agent → 对话、36 个 Agent 的「显示全部」、离线机器的说明、重新连接）、
Agents 左上角菜单（按机器分组、按机器筛选、全部机器）、项目页的机器名 → 机器的项目页（项目 → 项目页、ⓘ → 详情、离线时的空状态）、
新对话（离线机器不能选、目录浏览、最近目录、启动器、开始后 POST 的内容、失败时错误显示在按钮上方；模型：`/model` 列表、搜索、选中后只列它支持的思考强度、写死 `--model` 的启动器锁住、Codex 选模型不带思考强度，mock 的 `…/launchers/{name}/models` 见 `MOCK_MODELS_SECONDS`）。

- mock：`PORT=18773 MOCK_MACHINES=1 python3 scripts/mock/mock_hub.py &`（多一台离线的「实验室主机」，`POST …/reconnect` 后变在线；
  Mac Studio 有 36 个 Agent；`GET /mock/hits`、`/mock/reconnects`、`/mock/started`，`/mock/reset` 恢复初始状态；cwd 以 `/broken` 结尾的新会话回 502），
  再 `TEST_RUNNER_JARVIS_MACHINES_HUB=http://127.0.0.1:18773 TEST_RUNNER_JARVIS_SHOTS=<目录> TEST_RUNNER_JARVIS_STYLE=light xcodebuild test … -only-testing:PersonaUITests/MachinePagesTests`，
  深色再跑一遍（`simctl ui <udid> appearance dark`、`JARVIS_STYLE=dark`），每一步的截图存到 `JARVIS_SHOTS`，失败时附截图和视图树。
- 真实接口：本机起一个测试 hub，**不要连线上 hub 做重连、新建、关闭**。在 hub 机器上 `GOOS=darwin` 编 personad 拷回来；
  `docker exec jarvis-postgres createdb -U jarvis <自己的库>`，经 `ssh -L <本地端口>:127.0.0.1:18661` 连；假 HOME 里的 `~/.persona/hub.json`
  （0600）只放 `listen`、`token`、`database_url` 和两台机器：一台 `kind: local`，一台连不上的 ssh；`herdr --session <名字> server` 起一个独立的 herdr，
  `HERDR_SOCKET_PATH` 指向它（别指向正在用的那个）。假 HOME 里建 `code/demo`，然后
  `TEST_RUNNER_JARVIS_LIVE_MACHINES_HUB=… TEST_RUNNER_JARVIS_LIVE_MACHINES_TOKEN=… TEST_RUNNER_JARVIS_LIVE_MACHINES_CODE=<假 HOME>/code xcodebuild test … -only-testing:PersonaUITests/MachinePagesLiveTests`。
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

- mock：`PORT=18775 MOCK_AGENTS=1 python3 scripts/mock/mock_hub.py &`（约 130 个会话：platform 空间 3 需要你 / 35 完成未看 / 12 工作中，
  另有空闲多的空间和全空闲的空间；`seen`（WebSocket 或 `POST …/seen`）把完成未看改成已看，`GET /mock/seen` 列出，`/mock/reset` 复原），
  `TEST_RUNNER_JARVIS_AGENTS_HUB=http://127.0.0.1:18775 TEST_RUNNER_JARVIS_SHOTS=<目录> TEST_RUNNER_JARVIS_STYLE=light … -only-testing:PersonaUITests/AgentsListTests`，深色再跑一遍。
- `-openAgentsFresh YES` 启动时清掉列表记住的样子（机器筛选、分组、细列表、折叠）：这些值测试里要在界面上改，不能用启动参数固定（启动参数会盖过 App 写进去的值）。
- 列表行（`NavigationLink`）上的 `accessibilityIdentifier` 在视图树里出现两次（cell 和里面的 button），数行数、取第 n 行用 `app.buttons.matching(identifier:)`。
- `SearchOpenTests` 以前不带 `-dev.openChat ""`：上一次测试留下打开的对话，启动后恢复进去，右滑返回回不到列表，两个用例都挂（master 上也一样）。
- `SearchOpenTests` 两个用例在 master 上找不到 weather 的分组头和「给天气」那一行（#1051）：mock 的待处理多到 5 条后，「需要你处理」的卡片占满第一屏，懒加载的列表还没建它们。先 `bringAboveTabBar` 滚到它们，搜索前再滚回顶上；返回后用那一行判断回到了列表（列表停在原处，顶上的筛选条不在树里）。
- `ShellAvatarTests` 以前也不带 `-dev.openChat ""`：模拟器上之前跑过的测试在 Agents 留下打开的对话，切到 Agents 时恢复进去，列表的顶栏不在，找不到 Agents 页的头像（master 上也挂）。
- 细列表截图（`CompactListShots`，不用 mock，用体验模式的数据）：全部（空间多）、Mac Studio（未读多、空闲的空间）、未读筛选、需要你筛选、Mac mini（全部空闲）、
  按状态分组、卡片对照，每种三档字号（`-ui.textSize small|standard|large`；`TEST_RUNNER_JARVIS_TEXT_SIZES=standard` 只拍一档）。
  `TEST_RUNNER_JARVIS_SHOTS=<目录> TEST_RUNNER_JARVIS_STYLE=light … -only-testing:PersonaUITests/CompactListShots`，深色再跑一遍。
  改前 / 改后对比：在 `git worktree add --detach <目录> origin/master` 里放进同一个测试文件拍改前，两边同一份数据。
  带 `-openWelcome YES` 是为了去掉别的测试留下的配对（Keychain 里的 hub 优先于体验模式），`-demo.on YES` 让它直接进体验模式。
  所有 `launchJarvis()` / `launchPinned()` 的启动都一样：带 `-demo.on YES`、没带 `-pairURL` 时自动加 `-openWelcome YES`，不会接着用前面测试配对的 mock。

### 空间标签的停止关闭（`SiblingTabsTests`）

- 普通 mock，独立端口与模拟器：`PORT=<端口> python3 scripts/mock/mock_hub.py`，然后
  `TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:<端口> TEST_RUNNER_JARVIS_LAYOUT=shell xcodebuild test … -only-testing:PersonaUITests/SiblingTabsTests`。
  传给 `xcodebuild` 的变量要带 `TEST_RUNNER_`；测试进程读的是去掉前缀后的 `JARVIS_MOCK_HUB`，直接传后者会让整套用例跳过。
- 验证左滑按钮、长按菜单、工作中取消、关闭失败时保留标签与确认卡片、关闭当前标签切到相邻标签、关闭其他标签不切换、最后一个标签关闭后返回。
  原有的新建标签和点击切换回归一起跑；浅色、深色都看截图。截图以 `sibling-*` 附件保存在 `.xcresult`，
  用 `xcrun xcresulttool export attachments --path <结果> --output-path <目录>` 导出；不要吞掉测试进程直接写宿主 `/tmp` 的失败。
- mock 的 `GET /mock/session_close` 返回尝试和已关闭的目标；`?fail=1` 返回 503 且不改变会话。`/mock/reset` 恢复状态和记录。
  `python3 -I scripts/mock/test_session_close_mock.py` 验证历史保留、其他 Agent 不变、失败和重置。
- 不使用真实 hub 的会话测试关闭，也不以测试命令退出 0 代替检查实际执行／跳过数量。

### 实时更新不打断操作（`LiveUpdateTests`）

用户正在做的事经得起 hub 一直推更新：Agents 的 ⋯ 菜单滚到机器那里开着（不跳、不回顶）、行的长按菜单开着、列表滚到中间（行不动）、
在搜索框里打字（字和键盘都在）、行左滑出「关闭」；对话里工作中的 Agent 的过程卡展开着、过程里的截图全屏看着（不被关掉）。
每个用例报每轮推送的 body 次数和主线程耗时（附件和日志里的 `PERF` 行，改前改后的对比见 `docs/archive/lanes/live-updates.md`）。

- mock：`PORT=18797 MOCK_AGENTS=1 MOCK_CHURN=1 python3 scripts/mock/mock_hub.py &`，`GET /mock/churn?n=12&ms=400` 推 n 轮（每轮：各机器的会话列表——工作中的换一句、
  两个工作中的轮流变成最新——、机器列表、每个打开的对话一个新步骤，每 4 轮夹一句话），`&steps=N` 每轮每个对话加 N 步，`&final=1` 推完后这一轮结束（最后一句回复、工作中的变成完成）；`/mock/churned` 看推到第几轮；`MOCK_CHURN=1` 还在第一个对话的过程里放一张截图、多加 5 台机器（菜单要能滚）；`&inbox=1` 每轮再改一条收件箱、重推等你处理的事，`&waiting=1` 每轮多一个等你的 Agent、
  下一轮又答完（角标每轮变；真实的 hub 一直在推这两样，只推会话列表测不出角标带来的重建，`docs/archive/lanes/live-updates.md`「又回来了」）。
- `TEST_RUNNER_JARVIS_CHURN_HUB=http://127.0.0.1:18797 xcodebuild test … -only-testing:PersonaUITests/LiveUpdateTests`。
- 新外壳和旧布局都要跑：UI 测试默认用旧布局（`launchJarvis` 加 `-ui.oldLayout YES`），设 `TEST_RUNNER_JARVIS_LAYOUT=shell` 改用新外壳（iPhone 的默认），
  `LiveUpdateTests`、`ChatFollowTests`、`ScrollUnderPushTests` 两种都过才算。

### 滚动中推送（`ScrollUnderPushTests`）

手指拖着、惯性滑着的时候 hub 照推，屏幕上的东西只跟着手指和惯性走（`docs/archive/lanes/live-updates.md`「滚动时被迫刷新」）。
Agents 列表、Agent 对话、Persona 对话各拖、甩约 20 秒，同时 mock 按真实 hub 的节奏推：`/mock/churn?n=10&ms=2000&inbox=1&waiting=1&real=1`，
每 2 秒一轮。`&real=1` 让工作中的 Agent 最后一句在一行、两行之间换（行变高变矮），并让一个 Agent 每三轮完成一次、再开工（空间换名次）。

- App 以 `-perfCounts YES` 启动时，`PerfScroll` 逐帧看正在拖、正在滑的滚动视图。
  - `List` 看它的 cell，`ScrollView` 看 SwiftUI 画行用的 layer。
  - 记下几类异常：整体跳开（`jumps`，≥ 100 pt 的另记 `big`）、行之间相对挪动（`moved`）、还在屏幕里的东西被拿掉重画（`replaced`）、
    惯性被打断（`stopped`）、内容高度变了（`resized`）。
  - 每次异常记到它前 0.6 秒内到达的那类推送上（`causes`）。
  - 页面刚放开扣住的更新（停下 0.5 秒后，或关键更新立刻）的 0.3 秒内出的异常另记 `landed`：那是更新按设计在静止时落地，
    UIKit 在下一次手指落下时才排版，不算滚动中被打断。
  - 滚动中算了哪些 body 记在 `bodies`。都在 `perf.counts` 的 `scroll` 里。
- 断言：`big`、`replaced`、`stopped`、`defaults` 都是 0，Agents 列表的 `moved` 也是 0。两种布局、两种风格（`TEST_RUNNER_JARVIS_UI_STYLE=beautiful`）都跑。
  - 小于 100 pt 的跳动不推送也有（懒加载的行自己定高、UI 测试合成的拖动），不算。
  - 机器页那种玻璃卡片的页面，`moved` 没有推送也一直在涨（效果层自己相对内容动），只在列表上看它。
- mock：`PORT=18797 MOCK_AGENTS=1 MOCK_CHURN=1 MOCK_LONG_HISTORY=1 python3 scripts/mock/mock_hub.py &`（两个对话要比屏幕长才滚得动），
  `TEST_RUNNER_JARVIS_CHURN_HUB=http://127.0.0.1:18797`。
- 测试先配对一次、退出，再带要打开的页面启动：配对的 hub 和上次不同时，App 从头开始、停在助手页，`-app.tab`、`-dev.openChat` 都不起作用。

### 进对话再返回不动（`AgentsReturnTests`）

- 体验模式、`-probes YES`，读 `probe.agents-scroll`（`docs/ios-state.md`「探针」）。
- 新外壳和旧布局各一个用例，每个依次走四趟：搜索框收着 / 拉开 × 点「返回」/ 边缘右滑。
- 每趟返回的每一帧（从返回开始到之后 1 秒）都要满足：
  - 第一行、导航栏不动（范围、相邻帧跳动都不超过 1pt），等于离开时的位置；
  - 搜索框保持离开时的样子（收着还是拉开）；
  - 导航容器的 y 和顶部 safe area 不变。
- 两种风格各跑一遍（`TEST_RUNNER_JARVIS_UI_STYLE=beautiful`）。#340：以前新外壳返回时列表先下滑、搜索框自己展开，再上弹 52pt。

### 真实性与及时性（`ScreenTruthTests`）

屏幕上说的是 hub 的真实状态，而且及时（`docs/ios-state.md`「真实性与及时性」）。

- App 以 `-probes YES` 启动，`probe.agents-list` 导出列表显示的每个 Agent 的状态、最后一句和列着的提问；`probe.connection` 导出标题的连接状态。
  每个探针带 `at`（最近一次变化的时刻），和测试发推送的时刻比。
- mock：
  - `/mock/truth` 给 hub 此刻的真实数据；
  - `/mock/bump?what=rev|last|needs|ask|resolve` 一次只改一样，像真实 hub 一样推（整台机器的会话列表、或一个提问）。
- 用例和上限：
  - 静止时一致；
  - 手指按着时，需要你 / 提问 1 s 内上屏；
  - 最后一句在手指抬起后 0.5 s 内落地（断言 0.8 s，含读探针的时间）；
  - 一直按着也 5 s 内落地（断言 5.5 s）；
  - 固定种子的随机序列（滚动、推送、churn、进出对话、前后台、断网时推送）之后 8 s 内收敛；`TEST_RUNNER_JARVIS_SEED` 重放；
  - 甩完立刻点行，打开的是看到的那个 Agent；
  - 断网时标题 3 s 内显示，连回 25 s 内消失：重连按 1、2、4…最多 20 s 退避，断 8 s 后下一次大约在 15 s。
- 两种布局、两种风格各跑，种子 7、21、42 各一轮。

### 体验模式（`DemoModeTests`、`DemoHubTests`）

不用 mock hub：`TEST_RUNNER_JARVIS_SHOTS=<目录> TEST_RUNNER_JARVIS_STYLE=light xcodebuild test … -only-testing:PersonaUITests/DemoModeTests`，深色再跑一遍。
`testWalkThroughAndLeave` 从配对页（`-openWelcome YES`）进体验模式，走一遍 Agents（筛选、细列表、Claude 会话发消息收到演示回复、终端、提问卡片作答、
计划卡片、Codex 会话、表格和链接）、机器页和机器的 24 小时曲线、Persona（主对话、侧边栏、定时任务）、收件箱（飞书会话、邮件），
每页断言没有「体验模式里没有这项数据」，最后从设置退出、重启后还在配对页；`testStaysAcrossLaunches` 断言体验模式重启后还在。
跑完看模拟器日志里有没有页面要了 `DemoHub` 没有的接口：
`xcrun simctl spawn <udid> log show --last 10m --predicate 'eventMessage CONTAINS "DemoHub miss"'`（应为空）。
`DemoHubTests`（单元测试）让每个接口经 `APIClient` 解码，并检查副作用被拒、消息有回显和回复。

- 列表行被快速筛选条（`safeAreaBar`）盖住时 XCUITest 仍说它 `isHittable`，点下去点到的是筛选条（「需要你」被选上）：点行之前把它滚到筛选条以下、tab 栏以上，
  而且一次只拖三分之一屏（`swipeUp` 会甩过头）。
- 对话页有两个「终端」按钮（展开的页头在时钟下面、折叠后的小页头里），取 `frame.minY > 40` 且能点的那个。

### 多 hub（`MultiHubTests`，[multi-hub.md](multi-hub.md)）

三台假 hub 模拟「两组、其中一组两台」：A 单独一组（家里）；B、C 互联成一组（公司），C 拥有 MacBook。两组都有同 id 的机器、会话和交互，
发错 hub 一眼就能看出来。`scripts/mock/mock_hub.py` 的互联字段（`MOCK_HUB_ID`、`MOCK_HUB_NAME`、`MOCK_MESH_PEERS`、`MOCK_MACHINE_OWNER`、
`MOCK_MACHINE_NAMES`、`MOCK_MACHINES_KEEP`，`GET /mock/mesh`）见文件头的注释，不设时 mock 的输出和以前完全一样（`scripts/mock/test_mock_hub_mesh.py`）。

```sh
PORT=18760 MOCK_HUB_ID=hub-a MOCK_HUB_NAME=家里 python3 scripts/mock/mock_hub.py &
PORT=18761 MOCK_HUB_ID=hub-b MOCK_HUB_NAME=公司 \
  MOCK_MESH_PEERS='[{"hub_id":"hub-c","name":"公司 2","endpoints":["http://127.0.0.1:18762"]}]' \
  MOCK_MACHINE_OWNER='{"macbook":"hub-c"}' MOCK_MACHINES_KEEP=server,macbook \
  MOCK_MACHINE_NAMES='{"server":"工作服务器","macbook":"工作 MacBook"}' python3 scripts/mock/mock_hub.py &
PORT=18762 MOCK_HUB_ID=hub-c MOCK_HUB_NAME="公司 2" \
  MOCK_MESH_PEERS='[{"hub_id":"hub-b","name":"公司","endpoints":["http://127.0.0.1:18761"]}]' \
  MOCK_MACHINE_OWNER='{"server":"hub-b"}' MOCK_MACHINES_KEEP=server,macbook \
  MOCK_MACHINE_NAMES='{"server":"工作服务器","macbook":"工作 MacBook"}' python3 scripts/mock/mock_hub.py &
TEST_RUNNER_JARVIS_HUB_A=http://127.0.0.1:18760 TEST_RUNNER_JARVIS_HUB_B=http://127.0.0.1:18761 \
  TEST_RUNNER_JARVIS_HUB_C=http://127.0.0.1:18762 xcodebuild test … -only-testing:PersonaUITests/MultiHubTests
```

- 截图：`-pairURL <A> -pairURL2 <B> -pairURL3 <C>` 配三台（DEBUG；同一地址和凭据的 hub 重启后保持原来的组 id），再加页面参数：
  `-tab dev`（合并的 Agents）、`-tab settings -openHubs YES`（设置 › hub）、`-tab settings -openMachines YES`（按组分块的机器页）、
  `-openHub <组 id 或序号>`（当前 hub）。`-openSession` 可以写 `组 id|机器/会话`，不写组时按会话找。
- 同组换 hub：在设置 › hub 看哪台标「在用」，对它 `GET /mock/down?s=40`（或按端口停掉它），十几秒内「在用」换到组里另一台，标题不变。
- 只有一组时零变化：改动前在 `git worktree add --detach` 的副本里编一份 DEBUG 包，用同一份 mock（同一组启动参数、固定状态栏和时钟）拍 14 个界面 × 深浅色，
  改动后再拍一遍逐像素比；同一个包连拍几次得到噪声（系统菊花、液态玻璃的边缘高光每次启动都不一样），比较时排除噪声，剩下的差异要逐张看。
  设置首页「服务器」一节多出的「hub」一行是唯一允许的变化。

## SSH 隧道（`apple/Packages/PersonaSSH`，[network.md](network.md#经-ssh-隧道连-hub直连不通时的后备)）

**package 测试**（真 sshd + mock hub，不要 root、不碰本机的 ssh 配置）：

```sh
cd apple/Packages/PersonaSSH
DEVELOPER_DIR=/Applications/Xcode.app/Contents/Developer swift test   # Command Line Tools 里没有 Testing 模块
```

每个用例在临时目录里起一个 `/usr/sbin/sshd -D`（当前用户、随机回环端口、自己的主机密钥和 authorized_keys，带 `restrict,port-forwarding,permitopen=` 的那一行），
再起 `scripts/mock/mock_hub.py`（`PORT=0`）。覆盖：连接成功和 20 个并发请求、首次连接要确认指纹（指纹和 `ssh-keygen -lf` 一致）、指纹变了被拒且不自动重试、
公钥未授权、目标端口不通、permitopen 不许的地址、服务器不可达、会话被杀后自动重拨、服务器停了再起来（退避后恢复、本机端口不变）、WebSocket 流式（经隧道收 hello 等推送、发 ping 收 pong）、停止后不再监听。
全套约 7 秒。`swift test` 的输出要等跑完才出来，看进度设 `PERSONA_SSH_TRACE=<文件>`（夹具逐行写起停）。

**端到端**（真 hub）：本机另起一个专用 sshd（同上的配置，`ListenAddress 127.0.0.1`，authorized_keys 只放带 `permitopen="<hub 地址>:18660"` 的行），
模拟器里的 App 用 DEBUG 参数指过去：

```sh
# SSHRoute 的 JSON（字段见 Shared/HubTransport.swift）base64 后传，直接传 JSON 会被当成 plist 解析掉
SEED=$(python3 -c 'import base64,json;print(base64.b64encode(json.dumps({"host":"127.0.0.1","port":<sshd 端口>,"user":"<用户>",
  "target_host":"<hub 地址>","target_port":18660,"host_key":"SHA256:…","always":False}).encode()).decode())')
scripts/app/personactl.py launch -- -pairURL "jarvis://pair?url=<直连不通的地址>&token=<token>" -seedSSHTunnel "$SEED" -tab settings -openSSHTunnel YES
```

App 自己生成的公钥从 `personactl.py find --id ssh.publicKey` 读（或用 `-seedSSHKey` 给一把测试密钥）。配对地址故意写成直连不通的（如 `http://hub.invalid:18660`）就是测后备；`"always": true` 加真 hub 地址测「总是走 SSH」。
杀掉 sshd 的会话进程（`pgrep -P <sshd>` 及其子进程，权限分离下 socket 在子进程里）测重连；换 sshd 的主机密钥测拒绝。

**经隧道配对的 UI 测试**（`SSHTunnelPairingTests`，没有下列环境变量就跳过）：配对页填地址和 token → 「经 SSH 隧道连接」填服务器 → 系统弹窗里核对指纹、点「信任并保存」→「连接」。

```sh
TEST_RUNNER_PERSONA_SSH_HOST=127.0.0.1 TEST_RUNNER_PERSONA_SSH_PORT=<sshd 端口> TEST_RUNNER_PERSONA_SSH_USER=<用户> \
TEST_RUNNER_PERSONA_SSH_TARGET_HOST=<hub 地址> TEST_RUNNER_PERSONA_SSH_TARGET_PORT=18660 TEST_RUNNER_PERSONA_SSH_FP=SHA256:… \
TEST_RUNNER_PERSONA_SSH_KEY=<测试密钥种子 base64> TEST_RUNNER_PERSONA_SSH_HUB_URL=http://hub.invalid:18660 TEST_RUNNER_PERSONA_SSH_HUB_TOKEN=<token> \
xcodebuild test … -only-testing:PersonaUITests/SSHTunnelPairingTests
```

## iOS 兼容验证（App 不改代码，验 hub 的改动）

改 hub 的配对、鉴权、WebSocket 时，用当前的 iOS App 原样连一个本机测试 hub 验一遍（2026-10-31 P0 这样验过，`docs/archive/ios-p0-verification.md`）：

- 构建按模拟器 id 来（`-destination id=<udid>`，和 `scripts/sim.sh` 一样）。别加 `-sdk iphonesimulator`：它会把手表 target 也按 iPhone 模拟器编，
  两个产物都叫 `Jarvis.app`，报「Multiple commands produce」。没有签名团队时加 `CODE_SIGN_STYLE=Manual CODE_SIGN_IDENTITY=- DEVELOPMENT_TEAM=`。
- DEBUG 版带 `-pairURL` 启动会跳过「用配对 token 换设备 token」（`DeviceCredentials.swift`）。要验换凭据：先带 `-pairURL` 启动一次
  （配置写进 Keychain），再不带参数冷启动，App 就走真实的 `upgradeCredentials()`；查 hub 库里 `device_tokens` 有没有新行、`pair_tokens.used_at` 有没有填。
- iOS 27 上 `simctl openurl jarvis://pair?...` 会弹「在 Persona 中打开？」确认框，脚本点不了，别用这条路配对。
- 模拟器的 Keychain 卸载 App 后还在：同一台模拟器上前后两次配对，第二次 App 可能先拿到上一次的凭据，要么换一台新模拟器，要么先在 App 里断开。

## herdr 联调

`./scripts/test/test-herdr.sh`：起一个独立的 herdr 会话（`herdr --session jarvis-test-<pid> server`，无头，和你正在用的 herdr 互不相干，
结束时按名字停掉），跑带 `herdrlive` 标签的测试：socket 和 CLI 结果一致、事件订阅；再在一个新建的临时目录里起一个 claude，
依次验证信任对话框被认出（发消息拒绝 `dialog_open`）、按 option 回答（rev 核对、键从当前屏幕算）、空输入框放行、草稿拒绝
（`input_draft`）、清空别的字不动、清空看到的那段后放行。本机没有 Go 时在 hub 机器上交叉编译测试二进制再拷回来。
需要本机装了 herdr 和 claude（claude 只启动、不发消息）。

## Codex 联调

`./scripts/test/test-codex.sh`：Codex 的提问和审批卡片（`docs/codex.md`「提问和审批」）对真实 Codex 跑一遍。起一个独立的 herdr 会话（`jarvis-codex-<pid>`，
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
  路由对、Codex 回了第一条也收到手机再发的一条；最后一个找不到命令的启动器要马上报错。新建要一个本机的 personad（`codex-start`），脚本一起编。
  不跑不带参数的 0.163：它会自动启动或加入正在用的守护进程。`JARVIS_CODEX_TESTS="cards daemon start"`（默认三样都跑）挑着跑。
- 每轮每个用例打一行 `LIVE <版本> round <n> <用例>: ok in <秒>`，失败时附上当时的屏幕；最后一行 `RESULT all: passed|FAILED`。
  失败时屏幕上是 Codex 自己的模型服务错误（「Selected model is at capacity」之类，不是 Persona 的问题）就记一行 `SERVICE-RETRY` 再跑一次这个用例，
  每个 RESULT 行报重试了几次——报告稳定性时要一起报，不能当成没发生。

## 隐私扫描

`python3 scripts/privacy_check.py`：整个仓库（文件内容和路径）里不能有这台机器的私有值——`~/.config/persona/config.json` 里的
主机、hub 地址、机器名、Apple 的各种 ID、邮箱，`~/.config/persona/private-words.txt`（一行一个：内部项目名、人名、内部工具名，
自己维护）和 `CLAUDE.local.md` 里反引号括起来的值；另外任何私网 IPv4（文档网段 192.0.2.0/24 等除外）和真实域名下的邮箱
（`example.*`、公共邮箱、`site/` 里公开的联系邮箱除外）。命中只报类型不报值（免得值进日志）。
不算私有值的：回环地址（`127.0.0.0/8`、`::1`、`localhost`，带端口、带协议的也一样，config 或 `CLAUDE.local.md` 里写了也不算，比如 hub 地址是 `http://127.0.0.1:<端口>`、数据库是 `127.0.0.1:<端口>`；私网地址带端口照报）、`config.example.json` 里本来就有的默认值（比如外部测试群组名）、仓库自己的 Go 模块路径（里面带 GitHub 用户名）、
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
- Python 回归测试：`python3 -B -m unittest discover -s scripts/release -p 'test*privacy*.py' && python3 -B -m unittest discover -s scripts/test -p 'test_privacy*.py'`；`npm test` 不包含它。
  包含公开 Go 二进制的依赖常量分类、依赖校验 / replace / 工具失败时拒绝，以及真实个人值不被公共常量掩盖的测试。
  测试用隔离 HOME/Git 配置的临时仓库验证真实提交钩子、暂存区与工作树差异，不操作主工作区的暂存区或历史。
- hooksPath 是每个克隆的本地配置；新克隆仍需运行上面的 `git config` 命令，不能因为仓库里有 hooks 文件就以为它已经生效。

## 流式时的滚动（`StreamingScrollTests`，录屏逐帧量）

看「流式时稳不稳」，截图和探针都不够（探针读的是 SwiftUI 的位置，会和屏幕差一拍，docs/lessons/ios.md）：录模拟器的屏，逐帧量。
`SIM=<udid> OUT=/tmp/stream-shots scripts/diag/stream-scroll/run-ios.sh [用例…]` 每个用例起一个 mock hub、录屏、取回 App 的 `-scrollTrace`；
`scripts/diag/stream-scroll/analyze.sh /tmp/stream-shots` 量每帧的位移、往回跳、一帧里的大变（回复消失一帧这种），跟随的用例再量最后一行到输入框的距离；
`vidpick.swift` 按时间取出某几帧看。用例：跟随时长回复流式、先清空后到正式消息、看历史时流式、点 ↓、流式中往上拖、运行中的一轮、Agents 工作中。Mac 见 docs/mac.md「滚动实验」。

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

### Persona 页（长主对话）

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
hosting controller 里排版（旧布局、BeautifulUI 以外的风格使用 `LayoutIsland`，`DesignSystem/LayoutIsland.swift`；BeautifulUI 的输入框菜单挂在输入框外面，放进去就点不到；页面的环境按值传进去，整份覆盖会让里面的控件不在无障碍树里，`ComposerAccessibilityTests`），页面只在它的高度变了（折行、上面多一个提示）时才重新排；
新外壳的输入框保留原来的视图树：它从单行切成带按键栏的卡片，放进独立 hosting controller 后长文字会让无障碍快照卡死，不能套用旧布局的优化。
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

### 飞书页（会话页、收件箱，#674）

同样用 `-showPagePerf YES -perfCounts YES`。mock 加 `MOCK_LARK_RICH=1`：f3 的历史里每 3 条有 1 条换成卡片、图片或富文本，像一个忙的工作群。
会话页用 `-tab mail -openInboxItem lark:f3:300`，收件箱用 `-tab mail`。脚本看到 `page-idle-started` 后推
`GET /mock/lark_churn?feed=f3&n=14&ms=500&new=1`：按 hub 的方式推 `{"type":"lark"}`，每轮先加一条别人发的消息；收件箱那页加 `&inbox=1`。
`GET /mock/lark_churned` 返回已经推了几轮。

2026-11-03 P-19（打开的会话每次推送都重拉最新一页，并在主线程解码 40 条消息）。iPhone 17 模拟器，同一段时间里改前、改后各跑 3 次：

| 段 | 指标 | 改前 | 改后 |
| --- | --- | --- | --- |
| idle（14 轮推送，每轮一条新消息） | 主线程超过 16 ms 的帧 | 2 / 4 / 0 | 0 / 1 / 0 |
| idle | 主线程超过 8 ms 的帧 | 16 / 13 / 12 | 4 / 8 / 7 |
| idle | 掉帧（晚于 1.5 帧） | 3 / 0 / 0 | 0 / 1 / 0 |
| idle | 主线程上解码 `LarkMessages` 的采样数（`sample`，1 ms 一次） | 43 | 0 |
| glide | 掉帧 | 8 / 7 / 13 | 7 / 9 / 8（这次没改滑动） |

改了什么：会话和话题的消息页改在后台线程解码（`APIClient.getDecodedOffMain`）；
打开的会话最多每秒收到一次自己的变化通知（`LarkSync.chatGap`、`LarkChangeGate`），一阵推送合成一次，安静之后的第一条立刻送到。

2026-11-03 P-20（会话页滑动时，每一行出现和消失都会写页面的 `@State`，整页 body 跟着重算）。master `995e33a5`（#777 之后），
改前、改后交替跑 2 轮，每轮各 3 次，负载 16–60：

| 段 | 指标 | 改前 | 改后 |
| --- | --- | --- | --- |
| glide | 掉帧 | 17 / 10 / 21，8 / 10 / 13 | 6 / 4 / 5，3 / 3 / 5 |
| glide | 主线程超过 16 ms 的帧（6 次合计） | 48 | 11 |
| glide | 主线程忙 p95 | 10.6–15.4 ms | 6.9–7.8 ms |
| idle（14 轮推送） | 掉帧 | 1 / 0 / 0，0 / 1 / 0 | 全是 0 |

改了什么：已看到的记录、上报已读的计时、回执的记账，都放进 `@State` 里的一个 class（`LarkChatBook`），写它不会让视图失效；
会显示的（「N 条新消息」计数、回执）仍然放 state，只在真的变了时写。两次测量里 mock 收到的已读上报完全一样。
剩下的掉帧主要是 SwiftUI 给新进来的卡片行排版：只放卡片时 5–7，只放图片或富文本时 0–4。

## 侧边栏拖动（Persona tab）

量拖动时每帧主线程的耗时，不用 Instruments（这台 Mac 上 xctrace 卡在授权）：DEBUG 的 `DrawerProbe`（`-showDrawerProbe YES`）在拖动开始到松手后
0.6 秒里，用 run loop 的 afterWaiting / beforeWaiting（排在 Core Animation 提交之后）算每一帧里主线程忙了多久，加上 CADisplayLink 的帧间隔，
拖动中和松手后分开统计，追加到 App 容器的 `Documents/drawer-probe.jsonl`；`DrawerPan` 每次拒绝 / 丢掉一个触摸也记一行（为什么没打开）。

- 手势本身：`DrawerSwipeTests`（mock 加 `MOCK_DRAWER=1`，主对话末尾有一个比屏幕宽的代码块），斜划、气泡上、快甩停下后、代码块上。
- 数字别在 XCUITest 下量：它开着无障碍，SwiftUI 每帧多做很多事。用 `-showDrawerAutoDrag <轮数>`，`simctl launch` 启动、不接 XCUITest：
  每轮拖开松手、拖回、半路松手（弹回）、快甩、点页面关闭，经过和手指一样的 `AssistantDrawer.dragBegan / drag / letGo`，每个显示帧一步。
  用开了优化的 Debug 构建（`SWIFT_OPTIMIZATION_LEVEL=-O SWIFT_COMPILATION_MODE=wholemodule`，单独的 derivedData）。对照组用 `git worktree`
  在旧提交上加同样的 `DrawerProbe.swift` 和三个同名方法。
- 看主线程在忙什么：`sample <pid> 30 1`，pid 要按自己模拟器的设备目录找（`pgrep -f "Devices/<udid>/.*Persona.app/Persona"`，别的车道的模拟器也在跑 Persona）。

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

## 推送回放（`TestPushReplay`）

改 hub 推给 App 的东西（会话列表、实时活动的节奏）之前和之后，用同一段真实推送比：

1. 在 hub 机器上录：`cd server && go run ./cmd/pushrec 20m ~/push.jsonl`。它像 App 一样连 hub（用 `~/.persona/hub.json` 的 token，不打印），
   不注册成手机 / 桌面、不打开对话，每条消息一行 `{"t": 毫秒, "msg": 原文}`。文件里是真实的会话（标题、最后一句），0600，**只留在那台机器上**，不拷走、不进仓库。
2. 回放：`JARVIS_PUSH_RECORDING=~/push.jsonl go test ./internal/hub -run TestPushReplay -v`。打印每种消息改前（录到的）和改后（这份代码会发的）
   每分钟的条数和字节；实时活动按录到的时间把每次变化喂给合并和节流，打印 App 不开时每小时推几条、灵动岛和 hub 不一致连续最长多久，超过上限就失败。
3. 会话列表用真的 `broadcastSessions` 决定发不发，并核对和「按录到的字节比」一致（解码不丢字段）；同时挂一个收增量的 App（`sessions_delta`），数它收到的。
4. App 后台拉对话（`AgentSync`）的次数：`python3 scripts/diag/agentsync_replay.py ~/push.jsonl`，按录到的 `rev` 变化和当时的状态算（每分钟 45 次 → 9 次，见 `docs/ios.md`「后台同步」）。

2026-11-02 录的 19 分钟（耗电排查，`docs/live-activity.md`「推送节奏」、`docs/server.md`「推送给 App 的量」）：

| | 改前 | 改后 |
| --- | --- | --- |
| `sessions`（老 App：整表，一样的不发） | 36.9 条 / 分钟，2605 KB / 分钟 | 29.7 条，2452 KB |
| `sessions`（新 App：增量） | 同上 | 29.7 条，79 KB |
| 全部消息（新 App） | 104.9 条 / 分钟，2616 KB / 分钟 | 97.7 条，90 KB |
| 连上时的会话快照 | 602 KB | 227 KB（不带 Codex 历史） |
| 实时活动（App 不开） | 188 条 / 小时，不一致最长 2.5 秒 | 115 条，最长 29.2 秒（60 秒节流：60 条，58.8 秒） |
| 实时活动（App 开着） | 同上 | 0 |

## 实时活动截图（`LiveActivityShots`）

`simctl io screenshot` 拍不到灵动岛和锁屏上的活动。UI 测试 `LiveActivityShots` 用 `-showFleetSample` 开一个编造的活动，
回到主屏拍紧凑态（隔 0.4 秒两张，比对有没有动画）、长按灵动岛拍展开态（长按偶尔不生效，会再试一次存成 `expanded2`）、锁屏再点亮拍锁屏（首次会有系统的「允许实时活动」提示，测试会点允许），最后解锁：

```sh
TEST_RUNNER_JARVIS_FLEET_SAMPLE=needsYou TEST_RUNNER_JARVIS_FLEET_DETAILS=YES TEST_RUNNER_JARVIS_FLEET_ISLAND=custom:assistant,load xcodebuild test … -only-testing:PersonaUITests/LiveActivityShots \
  -resultBundlePath /tmp/shots.xcresult
xcrun xcresulttool export attachments --path /tmp/shots.xcresult --output-path /tmp/shots
```

`JARVIS_FLEET_SAMPLE=current` 不启动 App，只拍活动现在的样子（配合真实 APNs 推送，见 `docs/live-activity.md` 验证一节）。
和别的 App 的活动共用灵动岛：先 `scripts/app/island_peer.sh <模拟器 udid>` 装示例 App（`apple/IslandPeer`，只有一个「8分」的实时活动），
再加 `TEST_RUNNER_JARVIS_ISLAND_PEER=dev.example.islandpeer`，多拍 `-shared`（两个活动并存）和 `-landscape`（示例 App 在前台横屏，Persona 的活动单独在横屏的岛上）；
`TEST_RUNNER_JARVIS_FLEET_COUNT=12` 把编造的数量都改成 12（看两位、三位数）。iPhone 18 Pro 和 17 Pro 的规则不同，两台都要拍（`docs/live-activity.md`「和其他 App 共用灵动岛」）。
