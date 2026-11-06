# 经验：测试与验证

踩过的坑、怪异行为和当时的取舍（原来在 `CLAUDE.md`「经验」，索引见 `docs/lessons/README.md`）。新经验写进对应的节，没有合适的就新开一节。

- 活过测试的 goroutine 不能调 `t.Logf`，也不能读测试结束后会被改掉的全局开关（2026-11-06，#4822）：`computer.nolock` / `computer.cursor` 在自己的 goroutine 里发，测试结束后才写日志会 panic（`Log in goroutine after ... has completed`）；假 Mac 节点在请求时读全局的 `fakeNodesBuiltin`，上一个测试的 cleanup 改了它，迟到的请求就让已结束的测试失败。日志函数用锁加 `over` 标记（`t.Cleanup` 里置位）挡住迟到的调用；假节点在创建时读一次开关并留在自己身上。

- 助手总线会写 `m.sessions` 和 `h.clients`，测试改它们也要拿同一把锁（2026-11-06，#4822）：`fleet.sessions` 由 `takeFleetSessions` 在 `m.mu` 里写，测试直接赋值 `n.m.sessions` 时 `-race -count=30` 报 DATA RACE，还可能被尚未送到的旧列表盖掉。用 `editSessions`（先 `settleAssistant`，再在 `m.mu` 里改）和 `appClient(h)`，不要直接赋值。

- 同一份状态由多个 goroutine 各自生成再发出时，生成和发送要在同一把锁里（2026-11-06，#4822）：助手连上时 `FleetUp` 发各台机器的列表，同时 `Resync` 的快照也会让列表重发；原来两边各自先 `decorated()`、出锁后再 `Tell`，旧列表（还没有快照里的「Persona」标题）偶尔最后到 hub 并一直留着，`TestTheMainConversationIsAlwaysPersona` 约 3% 失败。`publishList` 现在在 `sentMu` 里生成、去重并发送。只在测试里等「新列表到了」不够：等到的可能就是那份旧列表。

- 夹具必须使用真实响应形状（2026-11-06，#2938 / #2017）：助手 feed 的 `view_actions` 是按卡片键索引的对象，空值也是 `{}`。网页和验收驱动都误当成数组时，数组夹具让单测一直通过，真实模型却在重载后丢已答状态。夹具改成协议对象，覆盖空对象、已答快照和分页；真实浏览器还要重载检查最新版本、已答状态和没有再次发送。

- 自主绘制验收先说清用户场景，不能暗中指定工具（2026-11-06，#2017）：只说“进度在同一位置更新”，模型可以先给正文选择卡、选后再开进度卡；驱动却要求第一张就是 live，便不会点选择。全过程同一界面的需求要在自然语言中说明，不提供 schema、工具名或预制卡片。原失败保留，工具列表可用也不等于模型真的调用了工具。

- 耗费真实模型前先启动一次实际浏览器（2026-11-06，#2017）：`python -I` 不加载用户站点，已有 Playwright 不一定能导入。保留导入隔离，只显式加载已安装、可信的依赖路径，不加入工作目录或下载目录，也不恢复 `sitecustomize`。依赖失败后的续跑只允许私有失败记录、同版本无主对话的测试 hub 和尚未提交动作的会话；已经提交的动作不再点击。

- 进度变化不等于闭环完成（2026-11-06，#2017）：检查同一 DOM 和 View id 后，还要等进度到完成、保留已答、重载恢复，再用测试数据库只读核对唯一发件箱与对话回复。模型可以在更新时移除选择按钮；移除或禁用都不能再次提交，但已答状态仍须恢复。

- 固定源码的测试证据和生产防回退分开核对（2026-11-06，#2017）：主线继续有未部署提交，不会自动抹掉固定源码的已通过证据，也不能把旧证据搬给新源码。受控前向更新须确认候选属于正式历史、包含实际安装版本的祖先，并核验生产网页来源、输入树、构建产物、配置和 hash；网页来源未知就不覆盖。保留备份和 CAS 回滚，发布窗口冲突时只继续隔离验证，不默认恢复生产更新。

- 被 guard 拒绝的启动不算受限执行成功（2026-11-06，#2017）：确认拒绝分支在 native CLI 执行前返回后，把它单列为 blocked，保留原日志；仍须有实际受限启动、工具白名单和真实对话工具调用证据。没有成功启动或出现未知启动都失败，不能删掉拒绝日志来凑通过。


- `-race` 只报没有先后关系的读写，测试自己拿一把锁就可能把竞争藏起来（2026-11-05，#2955）：`startAsync` 在 `go func()` 之后才读启动记录的 session，后台启动立刻失败时 `finish` 在锁里改它。现有测试读完会经 `startLog` 的锁去等结果，`finish` 再拿这把锁，检测器认为读在写之前，只有调用方恰好在 `go` 之后被抢占才报（全量 `-race` 偶发一次）。回归测试 `TestStartAsyncAnswersBeforeAFastFailure` 只等记录的 `done` channel，修复前每次都报。写竞争的回归测试时，等结果的办法别和被测代码共用一把锁。

- 测试交互 shell 的固定脚本时，先隔离 `HOME`、`USERPROFILE` 和 `ZDOTDIR`（2026-11-05，#3120）：只替换更新命令还会读取用户的 shell 初始化文件，工具链提示混入 `CombinedOutput`，使精确输出断言失败。三个目录都指向测试临时目录；保留成功、失败和拒绝未知动作的原断言，不改生产 shell 行为或用户配置。

- 模型 stub 只记录自己要测的调用（2026-11-05，#2682）：Agent 命名经 `kb` 继承同一个测试模型，会被误算成完成判定；没有请求体的 GET 探测也能覆盖 `js.input`，让真实判定明明已经成功、测试却读到空输入。命名用独立合成 stub，判定端只接受 POST。回归验证判定前后命名和 GET 都不改计数或输入；中断时直接断言零判定调用，不靠 sleep 后的混合请求数当基线。

- 模型限流夹具只统计模型使用的 POST（2026-11-05，#2763）：把所有 HTTP 方法都计入 sent／inflight／most 时，GET 探测也会改变并发峰值、消耗下一条假响应，测出的就不是模型并发上限。非 POST 在计数前返回 404；用 GET 前后计数和一次真实模型 POST 回归验证。保留原来的上限学习、恢复探测、最多两并发和无失败断言，不改生产 limiter，也不放宽上限或增加 sleep。

- 启动证明要在依赖的资源真正就绪后发布（2026-11-05，#2504）：nodeupdate 夹具先写版本／PID／环境证明，再监听 Unix socket；重执行测试读到新版本后立即连接，偶发报 socket 不存在。定向重复 10 次通过仍不能消除已经确认的竞态。把证明文件移到 net.Listen 成功之后，保留连接超时和真实重执行断言，不靠重跑或放宽期限掩盖时序错误。

- 跑整个脚本的 fixture 测试，超时只是挂死保护，要按负载留余量（2026-11-05，#2264）：hub 部署的 fixture 跑真实 `deploy.sh`，起约 48 个 Python 进程和每跳假 ssh 的 bash，CPU 固定约 7 秒，墙钟随负载涨（18 核 Mac 负载 35 时约 10 秒、45-50 时 24-27 秒、80 时超过 30 秒）。带时间戳的 `bash -x` 显示耗时摊在每一步，连假的 `hostname -s` 也要 1.7 秒，没有哪步挂住。`timeout=30` 因此在忙的机器上偶发 `TimeoutExpired`（6 个并发连跑 50 次挂 40 次）；改成 120 秒的具名常量并写明依据，断言不动。判断是不是这种情况：子进程 rusage 的 CPU 远小于墙钟、`uptime` 负载远超核数、trace 里没有单个长间隔。

- macOS 上第一次执行刚写出来的脚本或程序，可能要等近 10 秒（2026-11-05，#2307）：执行前要先过系统和安全软件的检查，`go test ./...` 开头几十秒在链接、启动几十个新测试程序，检查会排队。
  实测每 0.5 秒执行一次新写的两行 sh 脚本，中位数 0.26 秒、最长 9.5 秒，同时已有的 `/bin/sh`、`zsh` 都在 0.1 秒内。所以单独跑能过、`go test ./...` 全量跑就挂的测试，先看是不是在等一个新写的可执行文件；
  这类期限只是防卡死，给 30 秒。和负载无关：同样的负载下另起一个 `go test` 跑，照样很快。

- 时区扫描里的 `.Local()` 不一定是 `time.Time.Local()`（#2239）：Orca 的终端有同名布尔方法，判断是不是在本机执行。对候选调用查 AST 和返回类型，只有明确返回 bool 时才排除；未知类型仍保守检查，不加整文件豁免。用同一行同时出现布尔调用和时间调用的测试，保证排除时不漏掉真正的时区调用。

- WebKit 就绪探针先等原生加载状态，再执行 JS（2026-11-06，#2025）：`loaded` 在检查 `isLoading` 前就调用带 5 秒 watchdog 的 JS，首次 WebContent 启动超过 5 秒时，8 秒加载窗口还没用完就被 helper 撤页。未开始导航（URL 为 nil）或仍在加载时只等原生状态；之后才读 DOM，保留原超时和断言。每个初次加载、清空页、最终快照用独立阶段标签；watchdog 自己造成的清空／退出日志不能倒推成 App 提前销毁，日志中的启动慢只作线索，不当作已证明根因。

- `@MainActor` 不等于 Swift Testing 串行执行（2026-11-06，#2025）：多个异步 `@Test` 会在 await 处交错，真实 WebView 的旧测试组没有 `.serialized`，与安全组混跑时出现网络正对照页面加载超时。资源测试组声明 `@Suite(.serialized)`，两个 WebKit 组分批验证；保留原页面等待范围、网络正对照和零连接断言，不靠加时或跳过正对照过关。

- 网络隔离回归的监听器也可能被 Agent 端口发现探测（2026-11-06，#2025）：`server/internal/node/ports.go` 按 Agent 进程树找监听端口，并按实际绑定地址发送 `GET /`、`User-Agent: jarvis-ports`；换一个回环地址不保证隔离。无 WebView 的独立监听器也收到相同请求后，才能归属这个噪声。只隔离本任务的测试进程、保留网络可达正对照和零新增连接断言；不按 UA 筛掉请求，不停止或修改真实 node，原失败记录保留。
- 安全验收只覆盖实际测试的源码（2026-11-06，#2025）：Mac 正常矩阵验证的是 #2778 的精确沙箱。候选虽只多了内容进程退出回调的 `loaded != nil` 防重复保护，也不能把旧矩阵写成候选该分支已实测；记录源码 hash 和 diff，未测平台继续 fallback，正常矩阵不等于死循环或真实 OS 终止验收。

- 测试进程脱离 Agent 进程树不等于 WebKit 资源隔离（2026-11-06，#2025）：double-fork 只防本机端口采集误触监听器，WebKit helper 仍由共享登录会话的 XPC 管理。公开 API 无法定向取得／终止该 WebView 的内容进程或设置 CPU 配额；共享用户会话不跑 literal loop，不用全局进程名 kill 清场。需可从外部销毁的一次性 Mac VM／测试登录会话后再验死循环；正常矩阵、公开 delegate 故障注入和真实 OS 终止分别记录，未完成时继续 fallback。
- 外部 watchdog 要覆盖实际测试进程组（2026-11-06，#2025）：外层 driver 与内层测试若各自 `start_new_session`，只停 driver 的 PGID 会漏掉真正的测试组。记录自己启动的 PID／PGID，TERM 后限时 KILL 并 wait；成功、失败和超时都打印终态。每个 listener／JS callback 也有期限，加载不成功立即失败，不能继续执行后把空结果算通过。

- 聊天空态要看呈现内容，不只看 transcript 行（2026-11-06，#2024）：活卡片来自独立 feed，`store.rows` 为空时页面仍有卡片；用它判断空态会在卡片上方多画「还没有消息」，未加载完时还可能多画骨架。空态计算同时检查合成后的聊天行、transcript、待发送／正在到达的消息和流式正文；保留真正空会话的对照测试，不能直接移除空态。

- HTML 截图要等页面内容，不只等原生容器（2026-11-05，#2024）：共享示例的 `assistant-view-card` 已出现时，WebView 可能仍未加载完成，实拍得到白页。截图前等待共享 HTML 的实际按钮出现，并滚到对话顶栏和输入区之间的无遮挡区域；按钮存在却在顶栏后面，会点不到并截出空白页面。发送后聊天会滚到新消息、释放离屏 WebView，重复选择回归要先滚回同一卡片再点击，仍保留一次回复和零额外 POST 的断言。
- 重置 mock 的同一会话不等于重置客户端聊天缓存（2026-11-06，#2024）：正文决策示例留在 `ChatCache` 后，切换到表单示例仍会看到两个卡片，笼统查询 `assistant-view-card` 会报多匹配。合成夹具启动加现有 `-openChatsFresh YES`，卡片查询按具体表单字段收窄；表单重启恢复因此更严格地依赖服务端 feed，仍验证原生确认、一次回复和恢复提交值。

- Runtime CLI 的元素过滤键是 `id`，不是 `identifier`（2026-11-06，#2024）：后者是 XCUITest 的属性名，误传给运行时会漏掉 ID 过滤，不能当作卡片就绪证据。用真实卡片 ID 和可见／在屏范围校验；嵌套 WebView 的正文按钮仍用 XCUITest 验证，不在找不到按钮时退回固定 sleep 截图。

- Swift Testing 宏不要嵌套 `#require`（2026-11-05，#2024）：`#require(JSONSerialization.jsonObject(with: try #require(body)) …)` 在真实编译中报 recursive expansion，先把 body 用一个独立 `#require` 解包，再解析并断言。`#expect(array.allSatisfy(keyPath))` 也可能在宏展开中误判为 throwing closure，改用明确的非 throwing 闭包；保留断言，不靠跳过测试解决。
- 编译槽的检查和实际启动之间可能有另一会话抢先（2026-11-05，#2024）：`pgrep` 只是观察，不是互斥锁；所有 `xcodebuild` 必须通过 `~/.local/bin/xcode-slot xcodebuild …` 获取全机槽位，最多两路，满了排队，不绕过锁或增加 `XCODE_SLOTS`。外层等待带总超时，区分尚未启动、构建失败和成功，不把排队退出码写成测试失败或通过；自己的构建仍串行。

- rebase 解冲突后，`git diff --check` 只检查还没提交的修改，干净工作树不能证明提交里没有残留冲突标记（#2195）。推送 / 合并前还要跑 `git diff origin/master...HEAD --check`，覆盖 PR 的完整差异。

- 设置入口从一层移到另一层时，同时改 DEBUG 直达路径（#2195）：渠道移出扩展后，`-openChannels` / `-openChannel` 不再先压入扩展页。iPad / Mac 分栏会同时保留设置列表和详情，验证详情「没有某行」要把查询限定到详情的列表，不能对整个 App 做不存在断言；滚动也指定要检查的列表，避免滑动了旁边的详情。

- 助手卡片的纯文本替换要跟踪所有代码围栏（2026-11-05，#2024）：只找 `persona-view` 的开头，会把普通代码块里的示例误当成卡片；按 `\n` 拆行再只去横向空白，CRLF 的语言名还会留下 `\r`，最终在手表和 Siri 输出 JSON。先用真实换行字符划分行并保留行尾，记录普通围栏的打开与关闭，只有围栏外的卡片才替换成 fallback。回归要覆盖普通围栏内的示例、未闭合普通围栏、CRLF / CR / LF，以及语言名的大小写。

- macOS 的 Unix socket 回归使用 canonical 的短临时目录（2026-11-03，#277）：系统 `TMPDIR` 的长路径会超过 socket 地址长度；换成 `/tmp` 仍可能因它实际指向 `/private/tmp`，让 Codex fixture 的原始 HOME 与初始化后读回的 canonical HOME 不相等，表现为握手超时。用独立 `/private/tmp/<短目录>` 作为测试 `TMPDIR`，不因此改变运行根目录的词法兼容规则。先保留原失败日志，再在固定源码上复验；端口扫描的真实 `lsof` 缺陷单独按 #541 / #728 修复，不能归为临时目录问题。

- 同一个 worktree 的 `xcodegen` 与 `xcodebuild` 不能同时读写工程（2026-11-04，#1045）：iOS 复验重新生成工程时，Mac 构建读到暂缺的 `project.pbxproj`，退出 74，编译未开始。先完整生成一次工程，再串行构建；需要并行验证则用各自的工程／worktree，不能只隔离 DerivedData。保留原退出码，生成完成后的串行复验单独记结果。

- 灵动岛的像素布局测试不应先等 `ImageRenderer.cgImage` 再复制图片（2026-11-04，#1127）：专用新模拟器上，provider 连续等待 phase 6 超时，前 9 次采样为空；同一日志记录 pipeline 编译约 19 秒，延迟的具体原因未确认。
  对纯 SwiftUI 的符号／文本／几何布局，使用公开 `ImageRenderer.render(rasterizationScale:renderer:)` 直接绘入测试自己的 CGContext，显式保持点到像素的缩放。保留非空、半径、边界和四位数溢出断言，并用已知尺寸／偏移的矩形校准；不因此改变生产界面或放宽阈值。
  参数化用例的 xcresult summary 按方法计数，不是参数展开次数；完整性用 test tree 的 Arguments 节点核对，每档数字都要确实执行。后处理计数错了只纠正读取，原脚本退出码和实际 XCTest 结果分开保留。

- 兼容改名要把显示名称和持久身份分开验证（2026-11-04，#1189）：`persona-link` / `Persona Link` 上线后，压缩安装回归仍断言旧显示名称，完整发布因此退出 1。只更新显示契约，同时验证 `jarvis-link` 的真实文件、旧下载名、新旧可执行命令和安装器的实际调用；不能把兼容身份也盲改成新名字。脚本测试要分别 discover `scripts/test`、`release`、`deploy`、`mock`、`app`，只跑改名相关子集会漏掉其他目录的旧断言。原失败冻结与退出码保留，修复合并后用新构建号重跑。

- 截图验证别靠 computer-use 点模拟器（Mac 锁屏时拿不到窗口，没锁时会抢用户的鼠标）：用 DEBUG 启动参数（`docs/ios.md`）直达页面再截图。
- `xcodebuild test` 会启动不带 `-pairURL` 的 App 申请通知权限，之后这台模拟器的截图都会被授权弹窗挡住，跑完测试要重启模拟器再截图。
  活卡片测试也要先处理弹窗（#2024）：HTTP 已载入卡片不代表 App 已进入前台或完成 WebSocket 连接；推更新前等 mock 的实际升级连接，不能依赖可选的 `client_hello` 声明，因为兼容客户端可能不发送它。
- 新装（或换了签名重装）的 App 第一次带 `-pairURL` 启动就弹通知授权，弹框在的时候 App 一直不是活跃状态，收件箱不刷新：
  新外壳顶栏形象的收件箱角标不出来，`ShellAvatarTests` 报「no inbox mark」，截图整屏发灰（2026-11-04）。`simctl privacy grant` 给不了通知权限
  （Operation not permitted）；UI 测试启动后在 springboard 上点「允许」（`ShellAvatarTests.launch`），手动截图前先跑一次这样的测试。
- 模拟器里上一次启动遗留的系统授权弹窗，卸载重装 App 也不消失，要 `simctl shutdown` 重启模拟器（手动 `simctl launch` 不带 `-pairURL` 就会弹通知授权）。
- `xcrun simctl launch` 对已经在运行的 App 只是拉到前台、新启动参数不生效：截图前用 `--terminate-running-process`。
- `MOCK_PLUGINS=1` 的 mock 把插件的设置、密钥和开关留在进程内存里（2026-11-04，#1711）：浅色跑完 `ExtensionSettingsTests` 后「Example Notes」已经设置好，
  紧接着跑深色，`testSetUpAnExtension` 一开始就找不到「需要设置」而失败，看着像代码坏了。换深浅色、重跑前先按端口重启 mock（和 `MOCK_WECHAT` 一样）。
- UI 测试一律 `app.launchPinned()` 启动（`UITestStyle`）：App 记着上一个测试留下的风格，BeautifulUI 风格的界面又不一样，不说风格的测试会碰上别人留下的样子
  （LongSessionTests 在 BeautifulStyleTests 之后跑就挂过）。默认液态玻璃，`TEST_RUNNER_JARVIS_UI_STYLE=beautiful` 整套换成 BeautifulUI 风格跑；
  只测某一种风格的测试自己传 `-ui.style`。它经 `launchJarvis()` 启动，没选布局的测试照旧跑在四个 tab 的旧布局上。`UITestStyleTests` 查源码（`launch()`、`launchJarvis()` 都算漏）。
- JarvisTests 别跑在配对过真 hub 的模拟器上（`jarvis-sim` / `sim.sh` 配对的那台）：iOS 的测试宿主不跳过钥匙串（只有 Mac 跳过，`Shared/Keychain.swift`），
  一启动就拿配对连 hub、以模拟器身份注册手机工具、弹通知授权。2026-11-02 在这样一台上连着三次「test runner hung before establishing connection」
  （没锁屏，重启模拟器也一样），换一台从没装过 App 的模拟器第一次就过了，卡住的具体原因没再追。用 `simctl list devices` 挑一台没跑过 Persona 的。
- UI 测试锁了模拟器屏幕却没解锁（模拟器上按 Home 不解锁，要上滑），下一次 `xcodebuild test` 报「test runner hung before establishing connection」。
  模拟器系统语言是英文时，系统授权弹窗的按钮叫「Allow」，UI 测试两种都要认。
- 模拟器会被 XCUITest 的 typeText 切成「硬件键盘」模式：之后软键盘不再弹出（键盘按钮显示已弹起，但屏幕上没有键盘），依赖键盘高度的测试全挂。
  这台机器没有 Simulator.app，`defaults write com.apple.iphonesimulator …ConnectHardwareKeyboard` 改不回来：`scripts/app/sim-boot.sh "<新名字>"` 建一台新的跑。
- XCUITest 在键盘弹起的动画中点键栏会点空（键栏正跟着键盘移动）：先等键盘出现、停一下再点。
- XCUITest 的 `typeText` 绕过输入法直接插字，测不到拼音的 marked text：要走输入法就点屏幕键盘的键（`app.keys["y"].tap()`，空格选第一个候选）。
  第三方键盘（2026-11-03，#909）：在仓库外做一个只有键盘扩展的小 App，按钮里用 `textDocumentProxy` 的 `setMarkedText` / `unmarkText` / `insertText` 打一段固定的字；
  `simctl install` 后在 UI 测试里开「设置 › 通用 › 键盘 › 键盘 › 添加新键盘」加上它，回到 App 长按「下一个键盘」从列表选（点一下只在最近两个键盘间切）。
  第三方键盘不在 `app.keyboards` 里，用完卸掉它，否则后面等键盘的测试都报「no keyboard」。
- 按时间排的列表要有确定的次序（同一时间再按 id）：两个空间最后更新时间相同时，每次渲染顺序都可能对调，UI 测试点到的是另一行。
- XCUITest 的启动参数（`-key value`）会盖过 UserDefaults / `@AppStorage` 的读取，App 自己写进去的值读不出来：只能用来固定测试不会改的值
  （新对话上次的机器），会在界面上改的（Agents 的分组、筛选）要在测试开头用界面恢复。`-dev.openChat ""` 让 App 不打开上次的对话。
  反过来用 `-dev.openChat <会话>` 打开对话时要连 `-app.tab chats` 一起给：冷启动只在存着的 tab 是 Agents 时恢复对话，`-tab dev` 切 tab 时已经晚了，
  上一个测试停在别的 tab 时对话就打不开（2026-10-31 `LiveUpdateTests` 的看图用例在别的车道「第一步就找不到过程卡」）。只想打开某个对话用 `-openSession`。
- `-pairURL` 配的 hub 和这台模拟器上次配的不同时，App 从头开始、停在助手页：`-app.tab`、`-dev.openChat`、`-tab dev` 都不起作用（2026-11-01）。
  老测试一直连同一个 mock 地址，碰不到；换了端口、换了 hub 就碰到。要可靠：先只带 `-pairURL` 启动一次、退出，再带页面参数启动（`ScrollUnderPushTests`）。
- `-tab` 只认 `PersonaApp` 里列出的值，不认的值什么都不做，App 落在上一轮存下的 `app.tab` 上（2026-11-04，#1664）：`-tab feishu` 一直没人认，
  四组飞书测试（加密消息、多账号、置顶栏、会话置顶）全靠上一轮停在哪。UI 测试默认是旧布局：带 `-tab settings` 的测试（`SecretFieldTests`）存下 `settings`、没有外壳改回去，
  下一个用新外壳的测试一启动就开着设置 sheet，「消息」点了也被挡住，看起来像「消息页找不到会话」；它失败一次外壳就把 `app.tab` 写回 `assistant`，再跑又好了。
  加新页面时同时给 `-tab` 加上它的值；复现「单跑能过、接在别的测试后面就挂」先看 App 容器里的 `Library/Preferences/<bundle id>.plist`
  （`simctl spawn … defaults write` 写不进 App 的容器）。
- 「推送时不打断」要在手指还在、惯性还在的时候推。停下后再推，测不出滚动中整屏跳：`LiveUpdateTests` 的「滚到中间行不动」一直是绿的，
  真实 hub 上 Agents 列表滚动中跳到 900 pt 以上。逐帧量用 `PerfScroll`（`docs/testing.md`「滚动中推送」）。
- mock hub 要按真 hub 的样子答，尤其是「还没准备好」的那几种：Telegram 关着时真 hub 列不出它的能力（程序没跑过，能力是程序自己报的），
  mock 却一直列着 `login`，UI 测试和截图全绿，真 hub 上的页面却没有开关、写着「在 hub.json 设置」，打不开（2026-11-03，#622）。
  写 mock 前先读一遍真 hub 那条路径在各个状态下回什么，测试从用户一开始看到的状态（关着）走起。
- 要在真实数据上量又不能改用户的状态时，在 hub 前面挡一个只读代理：GET 照转；别的方法回 `{}` 不发出去；WebSocket 只往上转 subscribe、unsubscribe、pong
  （不转 `register_phone`，模拟器就不会接 `phone_*` 调用）。同一份数据开着推送、扣住推送各跑一遍（A/B），差出来的才是推送造成的。
  代理、录下来的推送、截图只放 `/tmp`，不进仓库（里面是真实的会话）。
- 查「这个 body 为什么又跑了」：body 里临时加 `let _ = Self._logChanges()`，再
  `xcrun simctl spawn <udid> log show --info --debug --predicate 'eventMessage CONTAINS "AgentsList:"'`，
  看到的是「哪个属性变了」（`\AppModel.allSessions changed`、`_xxx changed` 是 `@AppStorage`）。
  2026-11-01 靠它找到了 UserDefaults 写入、`onChange` 里读模型这两处漏。查完删掉。
- 逐帧量滚动时，异常按「前 0.6 秒内到的推送」归因只是时间上的巧合：mock 每轮几条推送一起到，几乎每个异常都能对上一条。
  确认是谁造成的，要逐类关掉推送重跑（mock 加临时开关），再看 App 里的时间线（`PerfScroll.mark`）。
- 看不到截图（Read 打开 PNG 什么都没有、模型说「工具没有输出」）不是目录的问题（2026-10-31 试过 `$TMPDIR`、`/tmp`、仓库里的图都一样）：
  是模型网关把工具结果里的图片丢了，只留文字。先用一张已知内容的图分别放在用户消息和 tool_result 里直接请求网关，对比两边能不能读出来；
  界面改动必须真的看到截图再交付，看不到就先修这条链路。具体网关和修法见 `CLAUDE.local.md`。
- 手动给 XCUITest 传环境变量时，命令行使用 `TEST_RUNNER_<变量名>`，测试进程读取去掉前缀后的变量名（2026-11-03，#697）。直接 `JARVIS_MOCK_HUB=... xcodebuild test` 没传进 runner，测试因缺 mock 地址跳过，但整体仍打印 `TEST SUCCEEDED`。验收必须确认用例实际执行、跳过数量和断言结果，不能只看退出码；修好环境后，旧界面的四类配置回归确实失败。
- 手动跑 `xcodebuild test` / `build-for-testing` 前先 `eval "$(python3 scripts/jarvis_config.py env)"`：配置里的 `xcode` 只经它变成 `DEVELOPER_DIR`。
  直接跑用的是系统默认的 Xcode，SDK 旧一档时报 `toolbarVerticalEdge` 之类「no exact matches in call to initializer」（2026-10-31 被当成别人的代码坏了，白查一下午）。
- 量界面的跳动 / 闪动要VNC：`simctl io recordVideo` 录下来、ffmpeg 拆帧，逐帧跟踪一条像素；只闪一帧的跳只有这样看得到。
  App 里记的位置日志可能和屏幕上的不一致（2026-10-31 日志说表头在原位，画面上整页偏了 10pt），以画面为准。
- XCUITest 的 `element.tap()` 在 iOS 27.0 模拟器上点在 frame 左上角往里约 4pt（27.1 上点在圆里）：圆形 `contentShape(.circle)` 的按钮
  （Persona 顶栏的玻璃圆钮「历史对话」「新对话」）点在圆外，什么都不发生，看上去像按钮坏了（2026-10-31 iPhone 17 Pro 上 `testButtonsAndPickingAConversation`
  在 master 上就挂）。手指和 VoiceOver 都点在中间，真机不受影响。这类按钮用 `tapCenter()`（`JarvisUITests/TapCenter.swift`）。
  点完要断言页面真的变了，别只找两个页面都有的东西（新对话页和对话页都有「问 Persona…」，点空了也过，错报到下一步）。
- 量滚动帧率别用 XCUITest 的拖动：它每次手势前要在 App 主线程上抓一次无障碍快照，自己就造成 40–100 ms 的长帧（每 3 秒一次，像是卡顿）。
  用 App 内的匀速滚动（`AutoScroller`，`-showFileAutoScroll`）加帧计数（`FrameMeter`），从一个空闲的会话打开（在跑的会话在后面每秒重画）。
- 带输入框的 alert：输入时键盘弹起，alert 跟着往上挪，紧接着点「保存」会点空（alert 还在、什么都没发生，看起来像保存没生效，2026-11-01 `AssistantTitlesTests` 差点因此去改 App）。
  打完字停一下再点，点完确认 alert 没了；alert 里的输入框光标落在哪是随机的，清空前先点框的右端把光标放到末尾。
- 隐私扫描要在真实构建 / 部署配置齐备后跑：缺配置时检查项不完整，显示 `clean` 不代表完整检查通过；新增配置后重新扫描。
  测试夹具的机器标识也可能与配置碰巧同名，改用中性占位值，不加豁免。
- `xcrun simctl push` 不运行通知服务扩展（2026-11-01，Xcode 27.1 / iOS 27.0）：CoreSimulatorBridge 把推送当本地通知直接加进通知中心，
  扩展进程不起，带 `mutable-content` 的推送原样显示（中继的推送就显示成「有新消息」）。扩展的逻辑放进共享代码、在 App 进程里测
  （`PushCrypto.service`，`NotificationServiceTests`），扩展本身只能真机验。同一个 App 的几条通知在通知中心叠成一组，
  UI 测试找文字前要先点开那组（label 带「已分组」）。
- Duo 外屏会把四个 tab 画成侧边按钮，无障碍树里没有 `TabBar` 父节点：配对完成要断言配对页消失、四个根导航按钮出现，再查 hub 保存的引擎。
  不能因为找不到 `app.tabBars` 就认定配对页没有退出。外屏只有约 678pt 高，Form 最后一行可能还没被懒加载；先滚到「确认并继续」再点。
- Agents 的搜索默认收起后，列表身份用固定的导航菜单或列表内容判断，不能用搜索框是否存在判断是否还在对话里；快速筛选是首行，滚走后也不能拿它判断页面身份。
  需要输入时先下拉，已经展开时不重复下拉触发刷新；空档回归同时检查首行与导航栏的距离和选中胶囊的真实像素。

- 模拟器的钥匙串里留着以前配对过的 hub（比如测 mock 时的 `127.0.0.1:18831`）时，`-demo.on YES` 不生效：`Keychain.serverConfig` 优先，
  App 画的是快照、请求都发去那个死掉的地址，看上去「点了没反应」，toast 是「连不上 hub，刚才的操作没有生效」。跑体验模式的测试前
  `xcrun simctl keychain <udid> reset`。Mac 的验收副本同理（钥匙串按 bundle id 分）：要体验模式用 `-openWelcome YES -demo.on YES`，先清掉这个副本的配对。

## 隐私检查与依赖下载源

- 2026-11-04，#1775：未改写的 Mermaid 资源也可能让短私词碰巧命中公开事件名内部的片段。先从 npm 固定版本下载包，校验 registry 的 SHA-512 integrity，再逐字节比对仓库资源和实际归档；不要把私词或命中原文写进日志。
  `check_bundle_privacy.py` 只有在文件名和固定 SHA-256 都匹配 Mermaid 12.3.0 原始资源时，才将不超过 4 个 ASCII 字母的私词按完整词检测；完整短词、非字母值、长私值及通用邮箱／私网地址检查仍保留。校验和与扫描使用同一次读取的字节快照，不读取可修改的本地白名单。
  任意修改、追加、截断、改名或规范化后才形成的片段都不能使用这个分类；其他 App 文件继续严格子串检测，既有公开 Go 模式不变。升级依赖时重新核实公开来源、复验反例并更新固定摘要，不能只把新文件的摘要抄进去。原失败归档只读留存；合入后用新冻结和构建号完整发布。

- 2026-11-02 App 配好 `app.push_relay` 后，发布方的公开中继 URL 必须进 Info.plist，但它也会被配置里的个人值扫描命中。
  App bundle 只识别配置中完整、无凭据 / 查询参数的 HTTPS `/relay` URL；同一域名出现在其他位置、附加路径 / 参数、邮箱或凭据仍拒绝，
  公开 Go 二进制也不放行。不要从私词表删掉整个域名，回归覆盖这些相邻和扩展 URL。

- 2026-11-02 隐私扫描修好后，公开 jarvisd 仍未部署：引入网络依赖后原始二进制约 35–37 MiB，超过 Workers 静态资源每个文件 25 MiB 的限制；不是 cf 鉴权过期。
  发布 `.gz` 下载包（约 12–14 MiB），先扫描未压缩二进制，安装端先校验压缩包再解压；上传前检查所有资源尺寸。`NO_DEPLOY=1` 也必须过尺寸检查，不能只编译 / 扫描就算部署验证。
  失败不能一概提示重新登录；TestFlight 上传成功、服务端发布失败要分开报告，并继续完成已上传构建的内部 / 外部分发。
- 2026-11-02 公开 jarvisd 的扫描误报：SSH 的全局请求名被当成邮箱，Tailscale 内嵌网页的官方联系链接、RFC1918 完整网段和云元数据常量被当成个人数据。
  Go 会把只读字符串拼在一起，CIDR 后甚至紧接另一个 CIDR，不能按可打印字符串的边界判断。先按真实编译输入追到依赖源码，不把原始命中输出到日志。
  `check_bundle_privacy.py --whole-short` 只对带 `public` 构建标记、未 replace 且 checksum 与 `server/go.sum` 一致的依赖，按精确协议 / HTML / 网段上下文分类；
  `publish.sh` 先 `go mod verify`。配置和私词表仍在原始文本上优先匹配，依赖缺失、校验和不符、上下文变化都拒绝；不能放行整个邮箱域名或整个私网段。
  回归：`python3 -B -m unittest discover -s scripts/release -p 'test*privacy*.py' && python3 -B -m unittest discover -s scripts/test -p 'test_privacy*.py'`，另跑四个平台的 `NO_DEPLOY=1 ./scripts/release/publish.sh`。
- 2026-11-03，#963：Swift 归档里的 SSH 算法名和证书密钥类型名也含 `@`，会被通用邮箱检测误报。先核对完整可打印字段与公开依赖源码，不把命中值输出到日志。
  `check_bundle_privacy.py` 只分类 `swift-nio-ssh` 中已核对的 7 个固定名称，要求原始字段完整匹配；移除 bundle / team ID 等内容后拼出的名称不算。
  这不是邮箱域名豁免，也不改变通用邮箱检测的 TLD 范围。私词照常匹配；前后上下文、URL、变体及同域普通邮箱不参与协议分类。
  用字段级和真实扫描入口回归验证，再只读检查失败归档。原冻结、退出码和已部署／未上传状态保留；后续发布用新冻结和新构建号，不复用失败构建。
- 2026-11-04，#1026：完整 SSH name-list 的兼容不能改成从任意词边界上下文里移除名称；这样会误放行 `mailto:` URL、XML 和规范化后才形成的名称（#975 回归）。
  只分类原始完整字段中的固定名称，或每项都属于固定集合、没有空项／空白的逗号列表；未知项、URL、前后文和移除 bundle / team ID 后形成的片段仍检测。
  先让合成反例在旧实现上失败，再验证字段级、实际扫描入口和私词优先；保留完整 name-list 的正例，不能为修边界又破坏原兼容场景。
- 手表 Release 二进制可能是 arm64 + arm64_32 的通用 Mach-O；签名里的证书邮箱 / 个人名不是业务数据，隐私扫描只排除每个切片中经过结构和边界验证的嵌入签名 blob。
  `LC_CODE_SIGNATURE` 的偏移相对于切片，不能按第一个切片的签名位置截断整个文件；也不能因为是通用文件就把证书扫描误报后删掉私词。
  两个架构的签名前后、签名占位区剩余字节及文件尾照常扫描；畸形签名、越界 / 重叠切片不排除。回归覆盖薄 / 通用 32、64 位及大小端，另用真实手表归档验证。
- `privacy_check: clean` 不代表私有值字典完整：没有本机配置、私词表时，已登记的匹配项可能只剩本机路径。npm 锁文件的下载主机要用独立的公开来源规则拦截，不能只靠私词表；包名是公开的，不代表 `resolved` 下载地址也是公开的。
- 检查暂存内容必须读取 Git 索引，不能只读工作树：工作树里已经修好、索引里仍是旧地址时，提交钩子必须拒绝；反过来索引安全、工作树后来改坏时，不应误拦已暂存的安全内容。
- 锁文件先按完整 JSON 检查，再走普通文本扫描；不要被 NUL/二进制判断跳过，重复键和 JSON 转义也不能绕过检查。错误只输出位置和类型，不能让被拦截的地址进入日志。
- hooks 文件存在不等于启用：`core.hooksPath` 是克隆本地配置，启用后用隔离临时仓库验证真实 `git commit`，不拿主工作区做拒绝测试。网页构建前也检查，避免未启用钩子的克隆直接发布。
- 测试递归失败要明确控制或模拟递归限制，不要假定固定嵌套层数在所有 Python 版本都会报错；Python 回归测试单独运行，`npm test` 不会自动覆盖它。
- `npm ci` 因下载镜像缺少某个固定版本而返回 404 时，先检查命令的 registry；即使锁文件记录公开来源，本机 registry 配置仍可能把请求转到镜像。显式指定公开 registry 和本机配置的代理后重试，不修改锁文件、版本或测试基线来掩盖环境问题。
- 配置热切换触发的异步刷新要带 generation，返回时在状态锁内核对再写缓存。只比较捕获的布尔值拦不住乱序返回，也拦不住「开 → 关 → 开」后旧请求覆盖；新连接握手的刷新也走同一提交检查。
- 静态卡片预览要区分 reply 和 push／pop／open：动作 id 是不透明标识，不能缺少 reply 时拿它当消息正文。用共用示例的详情按钮补回归，导航仅在本地演示或准确说明目标，不调用真实对话。
- 后台构建和测试必须用固定的源码快照，运行期间不在同一 worktree 合分支或编辑文件。2026-11-05 的一次 Go 定向回归先枚举了旧文件，随后 merge 引入新调用方，编译报新增方法未定义；这轮是混合快照，不能据此判断提交好坏，也不能记成通过。用独立 detached worktree 冻结提交后重跑。
- Go 测试 wrapper 嵌入窄 `Store` 接口后，把实际 Postgres 赋给该接口不会增加 wrapper 的方法集。卡片处理器先检查可选持久化接口后，管理页测试因此返回 503；旧处理器先拒绝坏 JSON，曾掩盖这个 fixture 缺口。真实总线回归要给助手显式传入完整 DB，hub 的管理登录仍使用自己的 wrapper，保留状态码断言，不延长等待或把 503 当成通过。

## hub 上的 Go 比仓库旧时（2026-11-03，#984）

- `test-server.sh` 只说「server test environment preparation failed; tests were not started」：`server_test_env.py` 先跑 `go env`，
  hub 机器上的 go 比 `server/go.mod` 要的旧时会去下载工具链，而那台机器 `go env -w` 过 `GOSUMDB=off`，下载出来的工具链也校验不了，`go env` 失败。
  不是数据库配置的问题。先用 `GOPROXY=https://goproxy.cn,direct GOSUMDB=sum.golang.google.cn go version` 在仓库目录里下好工具链，
  再把 `~/go/pkg/mod/golang.org/toolchain@v0.0.1-go<版本>.linux-amd64/bin` 放进 PATH、`GOTOOLCHAIN=local`、`GOPROXY=https://goproxy.cn,direct` 跑同样的命令。
- 只在 hub 上缓存过的依赖能编过：`proxy.golang.org` 不通时其他包报 `[setup failed]`，看着像一半的测试坏了。
- hub 的 Postgres 是系统装的（没有 `jarvis-postgres` 容器）时，`db.sh` 建不了 `jarvis_test`（它的输出被丢掉了），`jarvis` 角色也没有 CREATEDB：
  `internal/db`、`internal/db/dbtest`、`internal/hub` 报 `database "jarvis_test" does not exist`，其他包照常通过。
  这三个包在本机临时起 `postgres:18-alpine`（`-p 127.0.0.1:<端口>:5432`），设 `JARVIS_TEST_DATABASE_URL` 跑，跑完删掉容器。

## 只有 macOS 15 的 Mac 上本地跑 iOS（2026-11-04，#984）

- **最高只能装 Xcode 26.3**：26.4 起要求 macOS 26.2。`xcodes` 下载被 403 拦住，是因为 Apple ID 没在 developer.apple.com 同意过开发者协议；
  xcodes 会把苹果返回的「页面不存在」网页当成 `.xip` 存下来，解压时报 trace trap，先 `file` 看一眼再说。浏览器下好 `.xip` 后用 `xcodes install --path` 最省事。
- **编译 iOS 也要先装 iOS 平台**（模拟器运行时，8.4 GB），不只是跑模拟器需要。`xcodebuild -downloadPlatform iOS` 走系统服务，只认系统代理：
  代理软件把苹果 CDN 走慢线路时只有约 20 KB/s，把 `*.apple.com`、`*.cdn-apple.com` 加进系统代理的例外（`networksetup -setproxybypassdomains`）后有 4 MB/s。
- **Persona scheme 嵌着手表 App**：测试这个 scheme 要先装 watchOS 平台；装了之后，没有配对手表模拟器时测试运行器还是会「timed out while preparing」。
  只跑 iOS 单测：临时生成一份去掉 `PersonaWatch` 依赖、另起名字的工程（`xcodegen --spec <改过的副本> --project apple`），必须生成在 `apple/` 里（Info.plist、xcconfig 都是相对工程的路径），用完删掉。
- **不签名时共享钥匙串组拿不到**：`CODE_SIGNING_ALLOWED=NO` 编的包跑 `NotificationServiceTests` 会失败；模拟器上用 `CODE_SIGN_STYLE=Manual CODE_SIGN_IDENTITY=- DEVELOPMENT_TEAM=` 临时签名就过。
- **Xcode 26.3 下两个现有问题（master 一样）**：
  - `FileViewerTests.pathsAreLinkedOnlyWhenThereAndAskedInOneBatch` 在 `FakeHub` 的回调里触发 Swift 并发的隔离断言，测试宿主崩溃重启，xcodebuild 判失败，
    统计也只到崩溃前（576 个，实际 766 个）。原因是测试结构体标了 `@MainActor`，闭包随之被认定只能在主线程跑，`FakeHub` 却在后台线程调用它；闭包写成 `{ @Sendable req in` 就过；
  - 新外壳的助手抽屉（`AssistantDrawerLayout`）让主线程一直在重算视图，主线程上排队的任务轮不到：形象列表永远不刷新、runtime CLI 只回一次 ping。截图用 `-ui.oldLayout YES`。
- **卸载 App 不清钥匙串**：配对信息在模拟器的钥匙串里，卸载重装后 App 一启动又是已配对，直接进新外壳，测试运行器报「timed out while preparing」。先 `xcrun simctl keychain <udid> reset`。
- **Mac App 的部署目标是 macOS 26**：macOS 15 上只能编译，跑不了 App 和 `PersonaMacTests`。

## 网页分组与折叠

- 对齐 iOS 状态分组时，不能直接按原始 `state` 分桶：idle / unknown 但带 loop、后台工作或运行中子 Agent 的是「待唤醒」；屏蔽读取 `/mutes`，项目屏蔽按机器 / 项目名称匹配，Agent 的单独允许项可覆盖项目屏蔽，但不能覆盖 Agent 自己的屏蔽。
- CSS 的 `grid-template-rows: 0fr` 和 opacity 只把折叠内容藏起来，不会让链接退出键盘焦点和无障碍树；侧边栏折叠容器加 `inert`。组标题内的状态圆点已有同义文字，要 `aria-hidden`，否则按钮会被读成两遍「工作中」。
- 网页截图要先等菜单项真正渲染，不要在点开菜单的同一帧直接截图（可能只拍到外壳）；深浅色验证把主题偏好也存进 localStorage，单独改根节点 class 在 reload 后会回到旧主题。

## WebSocket 握手测试

- `Connection` 是逗号分隔的 token 列表，不能用 `Contains(..., "upgrade")` 判断升级：`notupgrade` 也会命中。遍历 `Header.Values`，拆分、去空白后 `EqualFold` 匹配完整 token，测试大小写、多个字段及子串拒绝（#692）。
- Go 的 printable rodata 可能把独立常量直接拼在一起，产物扫描会看到跨边界的短词（2026-11-03，#692：协议头 token 邻接 VirtualBox 网卡前缀）。先定位脱敏上下文并核对源码，不能当成真的写入了个人值，更不能删掉敏感词或加扫描豁免。品牌前缀按展示大小写存储、比较时规范化，回归确认网卡判断不变，再构建四个平台的实际产物扫描；只改请求头校验并不足以消除这次邻接。

- Node 22 的内置 `WebSocket` 在握手被 HTTP 401 / 400 拒绝时可能只触发 `error`，不触发 `close`，`readyState` 仍是 CONNECTING。
  拒绝测试不要等 `close`，更不能把任意网络错误当成鉴权正确；用 HTTP upgrade 请求直接断言拒绝状态（错误 / 缺失 token 为 401、
  缺失 / 错误子协议为 400、错误路径为 404），意外收到 101 或连接错误均失败，设置总超时并回收 socket。成功握手仍用真实 WebSocket 验证。

## 脚本目录迁移

- 2026-11-05，#1596：Robolectric 的截图输出目录是运行测试时生成的，不在干净 checkout 中；新增 hub 版本和翻译截图后，文档路径检查再次将输出目录当成失效源码引用。
  `test_doc_paths.py` 只登记精确的截图输出，并绑定真实的 Kotlin 截图测试文件及其默认 `build/…` 输出声明；生成器不存在或改了默认名称时，即使本机还留着旧输出目录也拒绝。不要跳过整篇 Android 文档、放行任意 `build/` 路径，或为了检查通过创建空目录。普通仓库文件引用继续检查是否存在，截图本身的生成和视觉验收是另一件事。

- 脚本移到子目录时，文字引用替换不够：同时检查 `__file__` / `$0` 推算根目录、Python 同级 import、跨目录子进程、Go 的路径构造和 SSH 传脚本的调用。
  公共配置和隐私扫描保留顶层；隐私扫描用脚本所属仓库定位根目录，不能因为调用者当前目录不同扫到另一份 checkout。
- `unittest discover -s scripts` 不会自动发现没有包标记的子目录；按 `test/`、`release/`、`deploy/`、`mock/` 分别发现并核对数量。
  兼容 shell 入口只 `exec` 转发，用带空格的目录、空参数、标准输入、环境变量和非零退出码测试，不通过真正发布或重启服务来验证转发。
- 配置增加新旧环境变量别名后，子进程 fixture 必须先清除两者再注入测试配置；只覆盖 `JARVIS_CONFIG` 会继承优先级更高的 `PERSONA_CONFIG`，
  意外读取发布配置。跨目录 import 回归同时覆盖新变量、旧变量回退和两者冲突时新变量优先，所有路径都指向合成 fixture，不修改生产优先级来迁就测试。
- 服务端测试启动器恢复服务根目录，只能用来准备数据库；调用 Go 前清除全部 `PERSONA_*` / `JARVIS_*` 运行变量，只显式注入同一个专用测试库的两套变量，
  同时给测试独立临时 `HOME` / `TMPDIR`，清除 Agent / XDG 的外部配置根。构建缓存和工具链另行保留，不让缺少 fixture 的测试读取真实默认目录。
  `server_test_env.py` 用 URL 解析确认从 `jarvis` 切到 `jarvis_test`；自定义库名、恒等目标或无法解析时失败关闭，绝不靠字符串替换后假定已隔离。
  URL path 也不是驱动的最终库名：pgx 会让 query 的 `dbname` / `database`（包括编码键）覆盖它。测试启动器只允许明确的 TLS / 连接参数，拒绝未知或重复键、service / credential 文件选项和无效 query；清除全部 `PG*` 环境，并显式禁用 OS 用户目录里的隐式密码／TLS 文件。匿名 `pgconn.ParseConfig` 回归精确核对实际 `Database`，不连接数据库；不得由此推断既往真实配置受影响。
  用 fake resolver / SSH / Go 验证启动器本身，不只测清理函数。2026-11-03 的生产根继承使测试 hub 在监听失败前已打开真实数据库、启动数据源和知识库清理，
  至少 14 个旧单帖 JSON 被删除；健康正常、端口占用或最终测试失败都不能证明没有数据变更。保留受保护原始日志，只读调查，不自动恢复或删除生产数据。

## 用像素判断界面状态

- 新外壳哪页在显示原来靠截图里「全部」筛选块的颜色判断（`ShellPixels`）。筛选条搬进列表、搜索框默认收起、BeautifulUI 风格换了筛选块的画法之后，
  像素就跟着变；测试挂了，看起来像 App 坏了，其实一部分是判断方式不对（2026-11-01）。能读状态就读状态：DEBUG 里放一个 1pt 的无障碍元素
  （原来的 `shell-pager-probe`，现在是 `probe.shell`：停在哪页 + 选中的 tab），测试读它的 value。两者要一致才算对，这正好也能抓到 TestFlight 70 那种
  「切换说是 Agents、底下画的是助手」。
- 分析挂掉的 UI 测试先确认它挂在哪一步、那一刻 App 的真实状态（`xcresulttool export attachments` 拿截图和树，或者临时加探针 + NSLog、
  `simctl spawn … log show` 拿时序），再决定是测试过时还是 App 回归：这次六个里只有一个是测试过时（搜索框默认收起是有意改的），
  其余五个是 App 的两个真问题。

- 测点击别用 `element.tap()` 点切换段这类两页都有的控件：无障碍树可能给出隐藏那页的副本，点到的不是手指会点的地方，
  测试过了、用户手指点却没反应（2026-11-02）。用坐标（`ShellFinger`：启动时读一次位置，之后点坐标），点完断言探针。
  探针里放最近的事件（`ShellTrace`，`probe.shell` 的 `trace`，每条带时间），失败信息里直接看到「点击到了没有、路由跑了没有、时间花在哪」，比截图快得多。
- 轮询探针别用会数全部匹配的查询：`app.probe(area, …)` 先 `matching(identifier:).count` 再取值，每次都让 App 交一份整棵无障碍树。
  `ShellStatusRowTests` 每 0.1 秒读一次 `probe.shell` 等状态行打开，状态行占位那一刻 Agents 拉出的搜索框被导航栏收起（#413 的那种），
  三种状态里两三种必挂；只换成 `["probe.shell"].firstMatch.value` 一次查询就全过（#499，2026-11-03）。在 App 里挪探针位置、去掉 `@State` 都没用。
  只有一个的探针（`probe.shell`）用 `firstMatch` 读；两页都有、要按 `page` 挑的才用 `probe(_:as:page:)`，并且别在动画进行中高频轮询。
- `.screenProbe(area, value)` 的 `value` 在没开 `-probes` 时也会在 body 里求值：读了什么，这个视图就跟着什么重画（RootView 的 `connection`
  让整个根视图在连接状态每次变化时都重画一遍，DEBUG 才有）。值要读别的状态时写成 `ScreenProbe.enabled ? Value(…) : Value()`。
  另外读 XCUI 元素的 `label` / `frame` 时它可能刚好消失（toast 到时、搜索框被收起），直接读会让测试当场失败；会消失的东西用 `try? e.snapshot()` 读（2026-11-02）。

- 模拟器的钥匙串里留着一次 mock hub 的配对（`-pairURL`）时，`-demo.on YES` 不管用：`Keychain.serverConfig` 优先于体验模式，App 一直连那个早已停掉的
  mock，界面上「连接中…」不消失、头像是离线的灰色，列表是之前存下的快照（2026-11-02，看起来像体验模式坏了）。跑 UI 测试和截图前
  `xcrun simctl keychain <udid> reset`。
  同一次 xcodebuild 里前面的测试配对过 mock，后面 `-demo.on YES` 的测试连的其实也是它，吃的是 mock 的数据：mock 没起来时（2026-11-03，脚本挪进 `scripts/mock/` 后旧路径起不来）
  一起显示「连接中…」、状态行占 32pt、返回点不到；mock 多了几条待处理时（#622 之后）「需要你处理」的卡片占满 Agents 一屏，测试找不到 agent 行、对话页顶栏被卡片顶走，
  master 上外壳的测试也一样挂。现在 `launchJarvis()` 见到 `-demo.on YES` 而没有 `-pairURL` 就自动加 `-openWelcome YES`（清掉配对和它留下的列表快照），体验模式就是体验模式。
  跑测试的脚本起 mock 后也先 `lsof` 确认端口在听，没在听就直接失败。

## 新加的 UI 测试

- 文件预览的 mock 有两套入口（2026-11-03，#796）：文件树用 `MOCK_TREE`，完整文本 / 分页用 `FILES`。新增 Markdown fixture 要同时更新两者，否则列表里文件名与大小是新内容、点开仍是旧长文，UI 测试找不到新链接。
- `socket.create_connection(timeout=3)` 的超时也留在返回的 socket 上；转发 mock 建连后要 `settimeout(None)`，由 WebSocket 断开结束流。模拟器在创建 WebView / 建立 HTTP 请求前停顿超过 3 秒时，旧 mock 已在等响应的线程中提前关掉 TCP，抽屉正确打开却是白页，不要因此放宽页面加载断言。

- DEBUG 启动参数触发的动作（发一条消息、打开某页）不要写在 `onAppear` 里每次都跑（2026-11-02）：带 `-openSession` 启动时对话页 0.2 秒内出现、消失、再出现三次，
  `-openSendMessage` 就写了三条消息（各自新的 id），`ConnectionTests` 看到 hub 收到三次，像是断网重发的 bug。会产生副作用的触发一次启动只做一次（`ChatView.debugSent`）。
  判断是不是真重复：看 mock `/mock/sent` 里的 `client_msg_id`，同一条消息的重发 id 相同，服务端和 mock 都按它去重。
- 新建的测试文件要先 `xcodegen` 再跑，否则 `-only-testing:` 选中的是空集，xcodebuild 照样 `** TEST SUCCEEDED **`（`Executed 0 tests`）。
  看结果要 grep `Executed N tests`，N 不能是 0（2026-11-01，端口转发的 UI 测试第一次「通过」其实一个都没跑）。
- 排序用例之间，mock 的重置也要恢复 `updated_at` 和 `last_text`，否则前一例的实时推送会改变后一例的顺序。
  原始活动快照要在所有场景改完 fixture 后再取，避免重置时覆盖场景特意准备的内容；读后返回的用例也要选带真实回复的 fixture，不能只开一个有列表元数据、历史为空的会话。
- **UI 测试的 App 用模拟器签名编，不加 `CODE_SIGNING_ALLOWED=NO`**（2026-11-05，#1929）：无签名的可执行文件里没有 `__TEXT,__entitlements`
  （`keychain-access-groups`、`application-identifier`），读钥匙串一直失败，`HubStore.read()` 是 `.unreadable`。`AppModel.init` 把它当「不知道配没配对」，
  `isPaired` 为假，`restoreSnapshot()` / `restoreOutbox()` 都不跑；`protectedDataDidBecomeAvailable` 后的 `retryRegistry()` 照样读不出。
  于是 `ConnectionTests.testWaitingMessageAfterRelaunch` 稳定失败（重开后没有「连上后自动发送」、hub 收到 0 次），`Library/Caches/state/outbox.json`
  里那条 `waiting` 却还在，很像发件箱恢复的 bug，先后有人当产品问题追了半天。用默认的模拟器签名（`sim.sh`，或 `xcodebuild build-for-testing` 不加签名参数）
  在 iOS 27.2 和 26.0 上都通过。重开后配对、发件箱、对话列表都没恢复时先查构建方式：`otool -l <App>/<可执行文件> | grep __entitlements` 没有输出就是无签名。
- 启动一律 `app.launchPinned()`（钉住风格），不是 `launchJarvis()`：`UITestStyleTests` 扫源码拦这个。
- 在 zsh 里写 `for v in "dark shell" …; do set -- $v` 不会按空格拆开（zsh 默认不分词），`$1` 是整串、`$2` 为空：外观、布局参数全错，
  测试还是绿的。这种循环放进 `bash -c '…'` 跑。
- 新端把模型选择移到独立接口后，旧功能的保存请求要省略模型字段，防止旧草稿覆盖新选择；但旧 hub 的 PUT 可能把省略解释为清空，必须先确认统一配置能力，旧 hub 继续携带原字段。用 FakeHub 分别覆盖新旧协议，而不是只测新接口。
- SDK 和模拟器列表里有对应条目，不等于 Xcode 能完成构建：平台安装状态或运行时版本不匹配时，scheme 报目的地不可用，actool 也会报没有兼容运行时。用独立临时工程、单独编译依赖可做 Swift 类型检查，但绕过资源编译不能当作正常 App 构建或截图验收；不要改正式工程来掩盖环境问题。
- **Xcode、SDK、模拟器运行时分开检查**：Xcode 可以搭配兼容的旧版运行时；某份运行时的设备限制不等于 Xcode 或真机的限制。
  `xcrun simctl list runtimes -j` 的 `supportedDeviceTypes` 给出实际设备列表；运行时 `Contents/Resources/profile.plist` 的
  `restrictedToDeviceTypes` 可能限定设备，按设备配置的 `productClass` 核对，不能只凭系统版本猜。创建失败时先查支持范围，再换组合。
- **同版本运行时装好，Xcode 仍提示系统未安装**：`simctl list runtimes` 显示可用、设备也能创建时，核对
  `xcrun simctl runtime match list` 的 SDK → 运行时构建号映射。SDK 默认寻找的构建号可能不在已安装镜像里；
  可用 `xcrun simctl runtime match set <SDK canonical name> <已安装的 runtime build>` 为该 SDK 指定同系统版本的运行时，
  然后用 `xcodebuild -showdestinations` 确认专用设备真正可选。恢复自动选择用同一命令的 `--default`，不要改系统 plist 来伪装已安装。
- **运行时显示 available，创建却报 `runtime path not found`**：先核对 `simctl runtime list -j` 和运行时实际路径，不直接删镜像或改系统 plist。
  已缓存的镜像可用 `xcodebuild -downloadPlatform iOS -buildVersion <系统版本> -architectureVariant arm64 -exportPath <新建空目录>` 导出，
  再用 `xcodebuild -importPlatform <导出的 .exportedBundle>` 重新登记。一次排查中下载命令提示已缓存、导出完成，但退出码为 70；
  不能把它算作成功，需检查导出内容并确认导入退出码为 0。重新导入后，原来无法创建的设备成功创建、启动并运行 App。
  验收按「创建独立设备 → boot / bootstatus → 安装 App → 启动 → 截图」走完；若需运行测试，再确认 Xcode 能启动测试宿主。
  Swift Testing 可能先打印 XCTest 的 `Executed 0 tests`，再实际运行 Swift 测试；以末尾 `Test run with N tests ... passed` 的数量为准，不把前一行当成空跑。
- **`List` 里的 `LabeledContent` 在无障碍树里是一个元素**（2026-11-02）：标签和值合成一个 label（「登录, 来自 MacBook 的 gh 登录」），
  `app.staticTexts["来自 MacBook 的 gh 登录"]` 找不到，测试报失败、截图上却明明有这行字。按值找要用
  `descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", 值))`；同一页里单独的 `Text`（账号、状态）照常是 staticText。
- **小一点的 iPhone 上，列表里的行可能在浮动标签栏底下**（2026-11-02，#488）：`row.swipeLeft()` 从行的中间开始滑，
  落在 Liquid Glass 的标签栏上就成了拖动标签栏的选中项，切到最左边的「Persona」，测试只看到「拉不出关闭按钮」。行这时照样是
  `exists` / `hittable`。同一个测试在 iPhone 18 Pro Max 上过、在 iPhone 18 Pro 上 100% 挂，一度被当成 #382 的回归（#382 之前的提交在 Pro 上一样挂）。
  对行做滑动、点按前先 `app.bringAboveTabBar(row)`（`JarvisUITests/AgentsSearch.swift`）；查「只在某个车道挂」的 UI 测试，先比一下各自模拟器的机型。
- **键盘弹起时 `app.collectionViews.firstMatch` 不是列表**（2026-11-05，#2189）：键盘上方那条 44pt 的候选 / 输入辅助条自己就是一个 collection view，
  排在列表前面，而且不在 `app.keyboards` 的 frame 里（SE 上键盘 frame 从 451 开始，那条在 407–451）。对它 `swipeUp` 列表纹丝不动，
  看着像「筛选条被钉住」，一度被解释成「元素滑动被键盘挡掉」。按列表里的元素找列表（`collectionViews.containing(.button, identifier:)`）；
  元素滑动从整个 frame 的中间开始，小屏上贴着键盘边也滑不动，要在看得见的那段行里用坐标拖（`AgentsListTests.dragListUp`）。
- 模拟器和 Mac 共用回环：App 在模拟器里监听 `127.0.0.1:<端口>` 时，Mac 上已有程序占着这个号就会冲突（端口转发会退到随机端口，正好测到这条路径）。
- **测试里执行的脚本会在开发机上真的跑**（2026-11-01）：`TestDeployScriptInstallsTheBinary` 执行 hub 的部署脚本，脚本最后一句
  `pkill -f '[j]arvisd node --attach'` 把跑测试的那台机器上真在服务的常驻 node 杀了：装了常驻 node 的 Mac 上每跑一次 hub 测试就掉线一次
  （launchd 约 10 秒拉起、hub 经 tailcat 重连几十秒、DSH 插件再重连，合计约一分半，期间打开会话报 transcript not found）。
  以前没人在装了常驻 node 的机器上跑测试，一直没暴露。脚本里会碰进程或服务的命令（`pkill`、`launchctl`、`systemctl`…）在测试里一律用
  PATH 最前面的假命令替掉，只记参数、再断言参数（`devices/deploy_test.go` 的 `fakePkill`）；验证时先记下真进程的 pid，跑完核对没变。
- 新外壳里「从左边缘往右拖」不等于「返回」：Agents 页没开对话时它翻到助手页，在助手页上它拉出抽屉。随机操作的测试拿它当返回，
  最后停在抽屉上，Agents 列表看不见、按设计扣着更新，像是「收敛失败」（2026-11-02，`ScreenTruthTests` seed 42）。回列表用界面上的控件：
  对话的返回按钮、抽屉的关闭、助手页往左拖（`ScreenTruthTests.backToList`）；判断「在列表上」别只看筛选条，它随列表滚走，看有没有点得到的行。
  对话页曾经有两个「返回」：看得见的那层，和看不见的那层（折起后全开的那层滑进状态栏那一条，`isHittable` 也是真，点了不返回），取第一个点得到的会一直点错。
  2026-11-02 起看不见的那层不在树上，任何时候只有一个「返回」，就是屏幕上那个（`ChatHeaderTests`）；折起的细条本身一直在状态栏下面，两种风格都是，
  「标题栏在状态栏那一条」其实是那个看不见的「返回」的 frame。
  BeautifulUI 的新外壳里要知道对话是否开着，用 `probe.chat`（page `agents`），不靠「返回」按钮。
- 看对话内容的 UI 测试一律带 `-openChatsFresh YES`（清掉磁盘上的对话缓存）：mock 每次重置，对话都从同一个 seq 重新开始，前面测试 churn 出来的步骤
  还在 App 的缓存里，占着这次 `/mock/say` 拿到的 seq，新回复「已经有了」就不显示，屏幕上是上一次的「工具 21 · 回复 5」。
  2026-11-02 `ChatFollowTests.testJarvisChatFollows` 在跑过 `ScrollUnderPushTests` / `ScreenTruthTests` 的模拟器上挂，看着像新外壳的 Persona 对话不显示回复，其实是测试环境；真实 hub 的 seq 是 transcript 里的位置，只增不重用。
- 测试按「滑一下停在哪」挑行的，别处改了行高或字号（#245 改了新外壳的字号档）就会滑到另一处，结果跟着变（2026-11-02，#339）：
  `testScrolledListHoldsStillThroughUpdates` 以前碰巧停在不换名次的空间，没测到读者那一屏被换掉；二分落在一个只改字号的提交上，真正的问题是 App 没保住读者那一行。
  二分找到的提交只说明「从这里开始暴露」，先看它改的东西能不能解释现象。找回一行用标题，别用整条无障碍标签（里面有「3 分钟前」，跨分钟就对不上）。
- 数 body 的测试（`RenderBudgetTests`）要等页面停下来再开始量（2026-11-03，#475）：启动后对话还在加载历史、排版，第一个量的推送（`rev`）
  就把这些气泡算到了自己头上，看着像「屏幕上什么都没变的推送重画了 6 个气泡」，换个顺序或多量一次就没了。现在先等计数 1 秒不变（`settled()`）。

## 断开连接不等于拒绝日志已完成（2026-11-05，#2243）

`TestProxyProtocolEndToEnd` 在客户端收到 TLS 连接断开后立即检查拒绝日志，偶发报 `refusal not logged`。`proxyproto.Conn` 的契约是先关闭连接，再调用 `Report`；客户端收到断开时，服务端回调可能尚未写日志。日志缓冲区的互斥锁只能保证读写安全，不能保证回调已经完成。

测试用同一把锁检查目标文本并订阅写入通知，等待真实日志出现，设定总截止时间；无关日志不能重置等待时间，缺失日志仍失败。纯等待回归用 `testing/synctest` 控制时序，覆盖已有文本、延迟和分段写入、多个等待者、无关写入以及超时。真实端到端验证仍检查无头连接被拒绝，再检查拒绝日志；不要调整生产关闭顺序、删掉日志断言，或用固定休眠掩盖竞争。

## 新平台要同步登记静态兼容检查（#1997）

`apple/project.yml` 新增 `PersonaVision` 后，`test_apple_rename.py` 的固定 target 集合仍只有原来的平台，合法的 visionOS target 因此被判断为重命名回归。新增平台时，要把它的 target 和 module 身份一起登记到兼容表，并验证规范源码目录及 scheme；只补允许列表而不检查 module，会漏掉身份回退。

保留完整 target 集合、原有 module、产物路径及旧 scheme 的严格检查，不改为子集包含、不跳过新平台。visionOS 的 target 是 `PersonaVision`，源码在 `apple/Vision/`，共享 module 仍为 `Jarvis`。

## 要 macOS 自带命令的 Go 测试（2026-11-02）

- 信息连接器读 `chat.db` 用的是 macOS 的 `/usr/bin/sqlite3`（jarvisd 不带 cgo），hub 机器（Linux）上没有 `sqlite3`，那几个测试在 hub 上会跳过。
  开发 Mac 上没有 Go：在 hub 上 `GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go test -c -o x.test ./internal/connector/imessage` 交叉编译测试二进制，
  拷回 Mac，**在包目录里**跑（`testdata` 是相对路径）。同样的办法用于 `node` 包里要 macOS 的测试。
- `sqlite3 -json` 没有行时什么都不打印（不是 `[]`），解析前先判空。

## 依赖钟点的 Go 测试（2026-11-03，#656、#664）

- 测试里用 `time.Now()` 推出来的时间（「一小时前」「现在」）又按天或按钟点断言的，一天里总有几分钟会挂：消息跨过本地午夜落进第二天的日文件，
  或者写死 09:00 的每日任务在 09:00 刚过时真的到点、多跑一次。两个都在 hub 全量测试的 01:00 UTC（上海 09:00）挂过，挡了部署。
  测试里的时间要么固定在一天之内（如「昨天中午」），要么离会触发的钟点足够远（如现在往后 12 小时）。
- 不用等到那个钟点也能复现：写一个只有一个偏移量的 TZif 文件，让它的钟「现在」正好读成要的时间，`TZ=/path/to/file` 管 `time.Local`，
  `ZONEINFO=<目录>`（里面放 `Asia/Shanghai`）管 `time.LoadLocation`。偏移量要取整到分钟：带秒的偏移会让按分钟写回的时间对不上，
  `TestBackfill` 等无关的测试跟着挂，看着像改坏了。

## hub 端到端测试的等待窗口要盖过管理者的固定延迟（2026-11-05，#2960）

- 管理者有几个写死的延迟：断开后按 `reconnectGrace` 先显示在线 5 秒（`devices/warm.go`），一条路连续失败按 `pathEnded` 退避 2、4、8 秒……（`devices/paths.go`）。
  `eventually` 的窗口只比它们多一两秒，机器一忙就超时（`TestCenterServiceConnector` 的 10 秒窗口等的是 8 秒的退避）；窗口按要等的延迟留足余量，注释写明在等什么。
- 用 `taskset -c 0,1` 把同一个测试二进制的 8～16 个副本压在两个核上跑，比 `-count=50` 单跑更容易逼出这类问题；
  `TestCenterServiceConnector` 这样还逼出了一个真的竞争（探测晚到的回答盖掉了 4002 的「重新配对」），超时不一定只是窗口太紧，先看日志里状态是不是卡住了。

## Mac 版真窗口验收（2026-11-02，#403）

- 在没签名的模拟器构建上跑 JarvisTests（`CODE_SIGNING_ALLOWED=NO`）时，`NotificationServiceTests.opensWithTheKeyInTheSharedGroup` 一定失败：没签名就没有钥匙串组，
  `PushCrypto.ensureKey()` 返回 nil。master 上同样失败，看到它别当成自己改出来的。
- 验 显示 › 刷新（⌘R）要让 App 在前台、主窗口是 key window：没有 key window 时只按分区刷新 hub 数据（`MacViewCommands.refreshTarget`），
  用 cua 的 `invoke_menu` 在后台调，会看到收件箱 / 知识库「没刷」（假 hub 日志里只有 sessions、machines 那一组），其实是测法不对。
- cua 在这个 App 上：工具栏里的菜单按钮、sheet 窗口在后台用辅助功能点不了（AXPress 报 -25202，sheet 的树读不到），要 `bring_to_front` 后用桌面坐标点；
  `delivery_mode: "foreground"` 点完会切回原来的前台 App，刚弹出的菜单、浮层跟着关掉。sheet 用前台 Esc 关；消息操作条要真的右键（`rightMouseDown`），AXShowMenu 不行。
  触控板左右滑（List 的 swipeActions）cua 模拟不了：它只能发滚轮事件，不带手势阶段。
- cua 的坐标会按最近一次整屏截图的缩放换算：同一个会话里拍过一张缩小的整屏图（`max_image_dimension` 1280，屏幕 1920 宽），
  之后按原始坐标点就被放大了 1.5 倍，点到了墙纸。点墙纸会触发 macOS 的「显示桌面」，把所有窗口推到屏幕边上，再点一次墙纸才回来。
  整屏截图一律用原始分辨率（`max_image_dimension: 0`）；用坐标点之前，先确认它落在自己的窗口或弹出的菜单里。
- 「只在 mock 下坏」先拿体验模式对照，再去看 hub 的代码到底怎么走（#511）：两个假 hub 都可能不像 hub，而且可能一个比 hub 好、一个比 hub 坏。
  新建 Persona 对话：mock 原来根本不写第一句、不推会话列表（App 显示「还没有消息」、新对话不选中）；DemoHub 立刻写好第一句（一切正常）；
  hub 是先推会话列表，node 对还没写 transcript 的会话回空的 items，等 claude 写下第一行（watch 每 0.5 秒看一次）才推。所以真 hub 上也会闪一下「还没有消息」，
  这才是 App 该修的（第一句按「已发送」留在屏幕上）。当初写「体验模式也复现」，其实是副本钥匙串里留着 mock 的配对、`-demo.on YES` 没生效（见上面「钥匙串里留着以前配对过的 hub」）。
- cua 的 `element_token` 只在同一个会话里有效：`cua-driver call` 不带 `session` 时每次调用是一个新会话，上一次读到的 token 下一次就是 `stale_element_token`；
  每次都带同一个 `session`（如 `"session": "qa"`）就能跨调用用，空闲 5 分钟过期（0.38.0 实测，2026-11-04，#1182）。只要一次调用里做完的，用 `run_steps` / `run_script`。
- cua 的 `tree_format: "elements"` 不列只有值、没有标签的静态文字（新对话页的乐观气泡、「Persona 正在准备…」都在里面），拿它判断「没显示」会误判；
  判断某段文字在不在，用 `get_window_state` 带 `query`（树里有 `AXStaticText = "…"`）或截图。
- cua 的 `type_text` 默认在后台用辅助功能插入文字（`AXSelectedText`）：主窗口工具栏的搜索框能筛选，sheet 里 `.searchable` 的框（新建对话 › 启动器）
  字显示出来了、列表却不筛，看着像 App 的 bug；用 `delivery_mode: "foreground"`（真按键）打同样的字就筛了（2026-11-03）。sheet 里打字一律前台投递，
  验收单里写明，否则执行者会报「不通过」。
- cua 里 sheet 自己的 window_id 读不到树（`ax_window_unresolved`），也不能对它定点投递；用主窗口的 window_id 读（sheet 在树里是 `AXSheet`），
  拿到框的 token 再 `type_text` 前台投递。前台投递完 cua 会把前台还给原来的 App，之后别往桌面（`scope: desktop`）发键，会落到别的 App 上。

## 逐像素比对（「只有一组时界面不变」，docs/testing.md「多 hub」）

- 同一个包连拍两次也不会完全一样：液态玻璃按钮、输入栏、卡片的边缘高光每次启动随机（同一次启动内不变），「正在工作」前的系统菊花一直在转，
  Reduce Motion 管不住它们。先用改动前的包连拍三四次当噪声，比较时排除落在噪声里的像素，剩下的差异逐张放大看（2026-11-02，#256；做法写在 testing.md「多 hub」）。
- 要比得稳：固定状态栏时间、冻结 mock 的时钟、每次卸载重装 App、mock 每次重启（相对时间一致），等画面停下再截（不要固定睡几秒：这台机器负载高时 7 秒还可能是白屏）。

- `scripts/test/test-server.sh` 只同步了 `server/`，#630 带进来的 `spec/fixtures` 测试（`TestWireSamples`、续传、resolution）读 `../../../spec` 和 `apple/` 的 Swift 源码，
  在 hub 上全量跑必挂（`open ../../../spec/fixtures/wire: no such file`），看着像自己的改动坏了。现在脚本连 `spec/` 和 `apple/**/*.swift` 一起同步（#642）；
  再有测试读 `server/` 以外的文件，同样要加进脚本（2026-11-03）。
- Go module 的本地 `replace` 也属于远端测试快照（#2028）：`server/go.mod` 引入 `../sdk/go` 后，只复制 `server/` 会在 setup 阶段报 `replacement directory ../sdk/go does not exist`。
  将完整 `sdk/go/`（含嵌入的 schema）同步到同一个 checkout 独立缓存的 `sdk/go/`，保留相对目录，并用 `--delete` 移除该 SDK 快照的过期文件；不要共享别的测试或部署车道的源码目录。
  扩展一致性测试还读取源码根的 `schema/extension/examples`（包含正常样例和 `invalid/` 反例）；同步 SDK 的嵌入 schema 不能代替这份样例快照，`schema/extension/` 也要保持同样相对目录及过期文件清理。
- 文档也算：`connector/conformance` 曾拿一份接入手册和夹具对照（#2725），脚本没带 `docs/`，部署前的全量测试就挂在这里。现在 `docs/**/*.md` 也同步过去（#2769）。
  合成 rsync 不能只创建空目录：假 Go 要读取本次 SDK 源码、嵌入文件及正常 / 非法样例，先测试冷启动，再检查更新后的源码及过期文件删除，避免同步漏项也能假通过。

## 沙箱测试用独立的合成受保护目录（#2052）

隔离测试启动器把 HOME 和 TMPDIR 放在临时目录中，包、数据和运行目录也可能位于其中。bubblewrap 的私有 `/tmp` 会生成这些挂载点的父目录；对隔离启动器 HOME 做 ReadDir 或写文件，可能碰到沙箱内部新建的空目录，而不是宿主 home 的内容。测试应使用一个未绑定的独立合成目录，并预置合成文件；从沙箱内验证文件不可读取、目录不可写，再从宿主侧确认写入标记不存在、原文件没有改变。不要为验证取实际个人 home，也不要将目录存在本身当作宿主内容泄漏。

保留数据目录、私有 tmp 和代理的允许 / 拒绝断言。负向控制只故意绑定这份合成目录：只读绑定应暴露读取，可写绑定应暴露读取和写入，原测试都必须失败；不改生产沙箱策略、不取消根目录只读挂载，不因此跳过真实测试。

## 附件草稿的重启回归（2026-11-03）

- 不把 `typeText` 返回当成输入框已经处理完所有文字：终止 App 前用谓词断言完整 `value`，保留重启前后截图，再检查真实磁盘恢复。一次深色回归在立即终止后只恢复了前两个字符，原测试没有记录终止前的值，无法区分输入与保存阶段；加此前置检查后可明确定位失败阶段。不要靠固定 sleep 或重启时再次注入草稿掩盖问题。

- 三页外壳的右滑回归要在消息行的同一高度结束手势，不能从顶部一行拖到窗口中心。顶栏布局变化后这条轨迹可能超过 45°，实际是在测试垂直滚动；改为行内水平轨迹，再验证目标页，不能把这种失败当成缺少翻页能力。

- 新增 `proto.Features` 能力后，连接 hello / manifest 的共享 wire 样例也要更新（`go test ./internal/hub -run '^TestWireSamples$' -update`），检查差异只有预期能力标记；只跑能力自己的处理器测试会漏掉协议快照失败。

- SwiftUI Form 的 Toggle 无障碍 frame 可能覆盖整行。整行的 0.88 或 0.95 比例点在不同机型上仍会点空（没有 POST）；应取 `toggle.switches.firstMatch` 内部控件，等 enabled 和 hittable 后点击其中心，再断言服务响应后的值。新增系统消息 fixture 后，不要用数组末项定位引用：按被引用消息 ID 查找，避免 mock 自身 500 被误认为实时同步故障。
- Shell 状态行折叠后保留上一条 toast 用于过渡；XCUITest 的节点仍可能 `exists`，录屏却已经不可见。只为等待后续长按不受遮挡时，应断言提示不再 `hittable`，不要要求节点被销毁。

## 起 sshd 做测试（2026-11-03，#891）

- macOS 自带的 `/usr/sbin/sshd` 可以不要 root 跑：`sshd -D -e -f <配置>`，配置里 `UsePAM no`、`StrictModes no`、自己的 `HostKey` / `PidFile` / `AuthorizedKeysFile`，只能登录当前用户。OpenSSH 10 要加 `PerSourcePenalties no`，否则同一来源连得多了会被罚等。
- 端口刚打开就会有别的东西来连（本机的端口探测会发 `GET / HTTP/1.1`），日志里大量 `invalid format [preauth]` 是正常的，过滤掉再看。
- 权限分离下每个会话是两个进程（`sshd-session: user [priv]` 和它的子进程），socket 在子进程里：模拟断线要把监听进程的子孙一起杀；只杀 `[priv]` 那个，连接还在。停 sshd 时不杀子孙，会话会变成父进程为 1 的孤儿一直挂着。
- `swift test` 的输出跑完才出来；卡住的用例要靠写文件的 trace 或 `sample <pid>` 找。Swift Testing 的 `.timeLimit` 管不住卡在不可取消的等待上的用例。
- 系统弹窗（`.alert`）的按钮用无障碍激活点不到，`personactl.py tap` 会退到 idb（本机没装）；要点弹窗的流程写成 XCUITest（`app.alerts[...].buttons[...]`）。

## 删模拟器前先确认 udid 不是空的（2026-11-03）

从文件读 udid 再 `xcrun simctl delete $SIM`：文件不存在时 `$SIM` 是空的，这次只打印了用法；要是写成 `delete "$SIM" all` 之类就会删掉所有人的模拟器。
脚本里读出 udid 后先 `[ -n "$SIM" ] || exit 1`，再做删除、卸载这类动作。

## 截图的模拟器上别跑单元测试（2026-11-03，#930）

在截图用的模拟器上跑过 `xcodebuild test`（测试宿主 App 不带 `-pairURL`，会请求通知权限）以后，这台模拟器上每次重装 App 都弹「想给你发送通知」，
卸载也清不掉；弹框让 App 处于非活跃状态，WebSocket 不连，截出来的任务全是「正在开」、机器名是 id。`simctl privacy` 不支持通知。
单元测试和截图各用一台模拟器；已经弄脏的直接删掉重建。

## 在 UI 测试里选图库里的视频（2026-11-03，#981）

- 测试视频自己合成（这台机器没有 ffmpeg，用 AVAssetWriter 写一段渐变加竖条的 H.264 MOV），`xcrun simctl addmedia <udid> <文件>` 放进模拟器图库。
  模拟器自带的几张风景照也在图库里，按 `label CONTAINS "Video"`（中文系统是「视频」）找格子，选到的就是自己放的那段。
- PHPicker 在另一个进程，但它的元素在 `app` 的树里能找到。模拟器的选择器是英文的：确认是右上角的 ✓，**identifier `Add`、label `Done`**；
  按 label「添加」找会先匹配到被选择器盖住的 App 自己的「+」（它的 label 也是「添加」），点下去什么也没选上，测试只看到「附件没出现」。
  横幅「Private Access to Photos」上还有一个 `Close`，别和播放器的关闭键混了。
- 系统播放器（AVPlayerViewController）左上角的关闭键 label 是 `Close`，等它出现就说明播放器开了。
- 经典布局 + 液态玻璃的对话输入框套着 `layoutIsland`，里面的「+」「发送」曾经不在无障碍树里，测试只能按位置点（#995，已修，见 `lessons/ios.md`）：
  现在按 label 找就行，别再给 island 加 identifier 去按位置点。

## 懒加载的时间线：没滚到的行不存在，同样的字可能在别处（2026-11-04，#1055）

- GitHub thread 页的时间线是 `LazyVStack`，最下面那条评论没滚到之前不在无障碍树里，`waitForExistence` 白等。
- 旧布局里收件箱列表还在树里，那条评论的预览也是同一句话，于是按文字等在液态玻璃下「碰巧」过了，换成 BeautifulUI（预览的写法不同）就挂。
- 做法：先等页面自己的元素（页头），再按行的 identifier（`github.entry.<id>`）一边往上滑一边找；截图前把目标滑到屏幕上半，
  否则「刚好露出一行就算找到」，几张截图拍的是同一屏。
- mock 记 `/mock/sent` 时别借用别的测试要找的键（这次把引用回复记成了 `"item": "github:review12"`，收件箱的测试读到了它）。

## 无头 Chrome 截窄屏（2026-11-04，#1208）

- `Google Chrome --headless=new --window-size=390,844 --screenshot=…` 不会按 390 宽排版：窗口最窄大约 500px，截出来是 500 宽的页面被裁到 390，
  看起来像「页面横向溢出」（管理界面的设备页、插件页都是这样），其实不是页面的问题。窄屏截 `--window-size=500,…`，要真 390 宽用 DevTools 的设备模拟。
- 管理界面的截图不用起 hub：`cd web && npx vite --port <空闲端口>`，打开 `/admin/<页面>?mock=app&theme=dark`（`?dialog=…` 直接打开对话框，见 `web/src/admin/mock.ts`）。
  `--user-data-dir` 指到 `/tmp` 下自己的目录，不碰用户的 Chrome。

## UI 测试里点不到列表行里的按钮（2026-11-04，#1328）

- **容器上的 `accessibilityIdentifier` 盖掉里面每个控件的**：给一行的 `VStack` 加了标识，行里 `Menu` 的 `market-menu-…` 就查不到了。标识只加在要点的控件上，
  或者容器先 `.accessibilityElement(children: .contain)`。
- **sheet 刚关上时第一下点按可能被吞掉**（键盘的焦点还在收）：填完表单「添加」后紧接着点列表里的按钮，什么都没发生、确认框不出来。
  测试里等 sheet 里的输入框 `waitForNonExistence`，再先点一个无害的按钮（刷新），然后才点要测的。
- 图标大小的 `Menu`（20 pt）在 XCUITest 里点了常常不展开：列表行里的几个操作直接做成一排 `.borderless` 的文字按钮，手机和 Mac 上都更清楚。
- **端到端里用 `dir:` 装的假扩展默认跑在沙箱里**（bwrap，只能写自己的数据目录和运行目录）：假的渠道插件往测试目录写收到的消息，写不了就抛异常退出，hub 把它再起起来、
  它从头重放输入，前面那条消息被去重吞掉，看起来像「消息丢了」。假扩展把文件放在 `initialize` 给的 `extension.data_dir` 里（测试去 `~/.persona/extensions/data/<id>/` 读），沙箱照开。

## 测试里的假模型服务器被本机的端口探测打到（2026-11-04）

`TestInboxTriageWaitsOutAnExhaustedQuota` 偶尔报「20 件用了 21 次请求」：多出来的一次是 `GET /`、`User-Agent: jarvis-ports`，
是这台开发机上跑着的 node 在找 Agent 监听的端口（`docs/ports.md`），探到了 `httptest` 起的本机端口。数请求次数的假服务器只数
自己的接口（`POST /v1/messages`），别的路径回 404 不计数（`assistant/inbox_limit_test.go`）；同类的假服务器照这样写，否则在有 node 的机器上会时好时坏。
`TestOpenMacLeavesAgentAlertsToTheApp` 也因为同一个原因偶尔多出一条 token 为 `/` 的「推送」：假 APNs 只数 `POST /3/device/…`（`hub/liveactivity_test.go`）。

## 假模型服务器还会收到 hub 自己的模型调用（2026-11-04）

`TestModelCallStopsWhenTheManagerStopsWaiting` 第一次跑时测试的请求一直没到假服务器：连上助手后 hub 自己的功能（Agent 的阶段判断等）
也用同一个模型配置发请求，被假服务器挂住，同一地址和 Key 的请求共用一道闸（`ai/limit.go`），测试那次在闸前排到超时、根本没发出去；
挂住的那个请求又让 `srv.Close()` 一直等，整个包跑满 10 分钟超时。假服务器先按请求体认出测试自己的那次（输入里带一句独有的话），
别的立刻回一个合法的答案；要挂住的请求也加一个上限（这里 10 秒），收尾不会卡死（当时在 hub 的 inbox_test.go；#2856 把模型配置交给管理者后，这个测试已去掉）。

## 同一组 UI 测试连跑时互相看见对方留下的东西（2026-11-05，#1961）

- App 的状态在模拟器上跨用例保留（飞书发件箱、草稿、对话缓存……），前面的用例在同一个会话里留下的「失败 / 结果未知」气泡，后面的用例用同样的标识去点，
  点到的是别人的那一条（或者一次找到多个元素）。单独跑每条都过，整套连跑才失败。
- 用例开头带上对应的 `-open…Fresh YES` / `-show…Fresh YES` 清掉前面留下的（飞书是 `-showLarkSendFresh`，`LarkSendTests`、`LarkAttachmentFlowTests` 都用）；
  用例里要验证「重启后还在」的那次重新打开**要去掉这个参数**，否则把要验证的东西也清了。
- 加新的 UI 测试类后整套连跑一遍，不只跑新加的那一条。
- `./scripts/test/test-server.sh … -run 'A|B'` 在 hub 机器上变成 `bash: B: command not found`、什么都没跑（2026-11-05，#1830）：参数经 ssh 又被远端的 shell 解析一次，`|` 成了管道。
  模式要多套一层引号：`-run "'A|B'"`。只看退出码会以为是测试挂了，先看日志最后几行。


## 只在某个 MOCK_* 下才有的数据，测试要自己放进去（2026-11-05，#2334）

`PhotoSaveTests` 要打开的带截图的对话原来只有 mock 以 `MOCK_PHOTO=1` 启动时才有，这个要求只写在测试文件的注释里；跑整套 UI 测试、别的用例用的 mock 都不带它，
App 打开一个不存在的空会话（「还没有消息」），两条用例每次都挂在「等 20 秒没有按钮」上，看着像相册或保存坏了。现在测试在 setUp 里 `GET /mock/photo?on=1` 自己放进去、
tearDown 里按原样撤掉（`MOCK_PHOTO=1` 照旧可用）。新写的 UI 测试照这样做：要的数据由测试经 `/mock/…` 准备，不靠启动 mock 的环境变量；实在要环境变量的，在 setUp 里查到没有就 `XCTSkip` 并说清楚。

## 偶发的唯一约束冲突先找同一行的两个事务（2026-11-05，#2704）

`TestTodoBotmuxChatMajority/store` 大约三次挂一次，报 `todo_agents_active` 唯一约束冲突（23505）。一开始猜是多个测试共用测试库，其实单独连跑 30 次就能复现 11 次：
节点报上来的会话让 b2 在后台自动加入 big，紧接着用户手工把它放进 small。两个事务各自锁的是不同的待办行，所以谁也不等谁，读的时候都看不到对方还没提交的链接；
后插入的那个要等前一个提交，等到了就撞上唯一索引。现在同一个 Agent 的链接、移除都先拿按 Agent 计算的 advisory 锁，再按 ID 顺序锁相关的待办行（#2742 的 `lockTodoAgentMembership`），别的 Agent 不受影响；
助手的自动链接走 `AutoLinkUnlinkedTodoAgent`：落库时它已经在别的待办里就什么也不做，不再顶掉手工的选择（直接调 `AutoLinkTodoAgent` 照旧会挪）。内存里那条没写进库的自动链接也要删掉，用户手动移除过的那条保留；撤回时只认它自己那条（同一个 LinkedAt），连带的「正在做哪个待办」也只在撤掉这条时才清，用户刚好手工重挂同一个待办的不能跟着丢。

遇到偶发的约束冲突，先用 `-count=30` 确认能复现，再把会写同一行的事务逐个列出来，看它们锁的是不是同一个东西。不能靠重跑、加长超时或跳过 store 来过。

## 在终端里跑 node 测试挂到超时：交互式 zsh 在后台进程组里被停住（2026-11-05，#3265）

在 hc / herdr 窗格、Terminal 这类真终端里跑 `go test ./internal/node`，`TestLauncherRepair` 一直挂到 10 分钟超时；CI、`test-server.sh`（ssh 不带 `-t`）、Claude Code 的 Bash 工具没有终端，不会挂。
node 跑启动器的命令用的是交互式登录 shell（`zsh -ic`），原来又用 `Setpgid` 把它放进自己的进程组：测试进程有控制终端时，交互式 zsh 会去拿终端，可它的进程组不在前台，
于是被 SIGTTOU / SIGTTIN 停住，永远不退出（goroutine 栈停在 `launcherRepair` → `Cmd.Wait`）。线上在终端里前台跑 `personad node` 时，启动器的探测、健康检查、修复也会这样挂到超时。
现在 `killGroupOnCancel` 用 `osproc.SessionGroupAttr()`（`Setsid`）：子进程在自己的会话里，没有控制终端，整组杀掉照旧。

测试挂住时先给测试进程发 `kill -QUIT`，看是哪个测试、卡在哪个系统调用上，再判断是不是环境问题；要起交互式 shell 的子进程，要么用新会话，要么不开单独的进程组。
## 在 Linux 开发机 上起测试 hub 测扩展（#3136，2026-11-06）

- hub.json 里不写 `machines` 时 hub 当作第一次运行，自动接上本机的 node（会探本机的 herdr / claude）：测试 hub 要写 `"machines": []` 并带 `JARVIS_LOCAL_NODE=0` 启动。
- 扩展默认在沙箱里，出网只能连清单里的 host（经 hub 的出网代理），连不到本机的 `127.0.0.1:<端口>` 代理：GitHub 扩展的「代理」设置指向本机代理时，测试 hub 上要 `personad extension sandbox github off`。
- 扩展仓库的打包脚本（package.sh）打的包在 `dist/`；内网扩展仓库的 `packages/` 是已发布的包，别装成那个（版本号相同时 hub 也不会重装）。
- SDK 的 `ext.Page.Action` 回空结果会被当成「重画整页」：UI 版本 5 的 `lines` / `items` 已经算回答；什么都不用画（标了已看）就回非 nil 的空 `Items`。

## 带程序的内置扩展在测试里跑的是测试二进制本身（2026-11-06，#3500）

内置扩展的 `runtime.command` 是 `personad` 时，manager 用 `os.Executable()` 加清单的 `args` 起它；在 `go test` 里这就是测试二进制（`extmgr.test voice`）。
测试二进制不认识 `voice` 这个参数，会把整个包的测试再跑一遍，套娃到超时。所以 `extmgr` 的 `TestMain` 先看 `os.Args[1]`：是 `voice` 就当 `personad voice` 跑
（`voice.Run`）。以后再加带程序的内置扩展（不只是声明），同样要在 `TestMain` 里分派，测试里才能真的把它跑起来、经 hub 的转发调它。

## Playwright 不支持的 Linux 上截网页图（2026-11-06，#3140）

- 现象：Debian 11 上 `npx playwright install chromium` 报「does not support chromium on debian11-x64」，换 ubuntu20.04 的包也不行；Playwright MCP 去找 `/opt/google/chrome/chrome`，同样起不来。
- 做法：下载 Chrome for Testing 的 `chrome-headless-shell-linux64.zip`（版本号看 `last-known-good-versions.json`），`ldd` 看缺哪些库。没有 sudo 时用 `apt-get download` 下这些库的 deb，`dpkg-deb -x` 解到自己的目录，启动时 `LD_LIBRARY_PATH` 指过去。
  中文字体（如 Noto Sans CJK）也这样解开，写一个 `fonts.conf` 把字体目录加进去，用 `FONTCONFIG_FILE` 指给浏览器，不然中文全是方框。用现成的 `playwright-core`：`chromium.launch({ executablePath, env })`。下载的东西各放一个新目录，不放进仓库。
- 等待：网页一直连着 WebSocket，`waitUntil: "networkidle"` 有时等不到，用 `load` 再等页面上的某段字。假 hub 用 `/?token=mock` 登录后先等侧边栏出来再跳到要拍的页面，不然拍到的是登录页。

## 本机跑 iOS 单测：失败后 xcodebuild 再等十分钟（2026-11-06，#3507）

- 有用例失败时，`xcodebuild test` 在用例跑完后还要去模拟器收诊断，等满 600 秒才退出（日志里是 `Failure collecting diagnostics from simulator: Timed out after 600.0 seconds`），用例本身不到一秒。本机反复跑时加 `-collect-test-diagnostics never`。
- 新加的测试文件要先 `xcodegen generate` 再跑：工程里没有这个文件时，`-only-testing:PersonaTests/<套件>` 不报错，只是一条都不跑（「Test run with 6 tests」只剩别的套件），容易当成通过。
- 模型里的回调都是 `[weak self]` 时，用例里 `let (_, socket) = …` 丢掉模型，模型当场释放，之后送进去的帧全被吞掉、断言莫名失败：用例要自己拿着模型（App 里由 `VoiceCalls.current` 拿着）。

## 隔离 hub 的 e2e 里让假 Claude 调 Persona 的工具（2026-11-06，#5073）

- 收件箱处理对话的结果只能由它调 MCP 工具（`inbox_suggest`）交上来，光回一段文字的假 Claude 测不到。做法：假 Claude 从自己的 `--mcp-config`
  （node 给无头助手对话传的那段 JSON）里取 `mcpServers.jarvis` 的 `command` / `args` / `env`，自己起一个 `personad mcp`，发 `initialize`、
  `notifications/initialized`、`tools/call`；工具按 `JARVIS_SESSION_ID` 认出是哪段对话，和真的一样经 node → hub → 管理者。要调什么由测试写的计划文件决定（按消息里的一段字匹配）。
- `prompts.TestEveryTemplateIsUsed` 按带引号的模板全名（`"inbox/handling/sent"`）在 Go 和模板里找：模板名拼出来（`"inbox/handling/" + name`）的会被当成没用到，常量里写全名。
- 旧的收件箱 e2e（`run.py` 的「管理者停了 hub 还在读来源」几条）在 2026-11-05 来源挪进管理者后就不成立了，master 上也是那 6 条挂：对比时先在 master 上跑一遍。
