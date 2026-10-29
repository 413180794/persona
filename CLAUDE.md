# CLAUDE.md

Jarvis：给自己用的 iPhone + Apple Watch 助手。「对话」tab 是能访问个人和工作数据的 AI 助手（hub 机器上一个专用目录里的 Claude），
「Agents」tab 是在手机上用聊天的方式驱动各台电脑上 herdr 里的 Claude Code / Codex（实时流式看过程、
在手机上回答提问 / 审批计划 / 批准工具），并让 Claude 读写 iPhone 本地数据（通讯录、日历、提醒、照片、健康、位置……）和邮箱
（Gmail / 任意 IMAP+SMTP）。服务端 `jarvisd`（Go）的 hub 跑在一台常开的机器上，经 SSH 接入其他电脑。通过 TestFlight 分发。

**这台开发机、这个人的具体情况（机器、地址、账号、个人工作方式）在 `CLAUDE.local.md`**（不提交）；没有它就先看 `docs/config.md` 配好。

## 约定（必须遵守）

- **配置化，不写死个人的东西**：机器名、域名、账号、邮箱、路径、启动器、公司内部工具、个人偏好一律走配置
  （构建 / 部署：`~/.config/jarvis/config.json`；运行：hub 的 `~/.jarvis/hub.json`；见 `docs/config.md`），代码、脚本、
  界面文案、测试、文档、本文件里都不出现具体某个人的值（内网域名 / IP、主机名、内部项目名、个人账号、各种 key 的 ID 也算）；界面说「hub 所在的机器」「你的各台电脑」。本机的具体情况写 `CLAUDE.local.md`。
- **随时沉淀经验**：踩坑、怪异行为、设计取舍当场写进本文件或 `docs/<主题>.md`。这条规则本身也要保留。
- 代码注释、提交信息用英文；文档用中文；App 界面文案用中文。
- **不提交任何内网信息或个人信息**（2026-10-28 用户要求；`scripts/privacy_check.py` 自动查，见 `docs/testing.md`「隐私扫描」）：代码、测试数据、文档、截图、提交信息里都不能有内网域名 / IP / URL、
  主机名、内部项目 / 仓库名、真实的公司或个人数据（消息、文档链接、人名），以及个人账号、邮箱、各种 key / 项目的 ID。示例一律用占位符
  （`<hub 地址>`、`me@example.com`）；具体值只写配置或 `CLAUDE.local.md`。Jarvis 支持的产品 / 工具名（飞书、TRAEX、removed 等）不算。
- **密钥绝不入库**：hub token、数据库密码只存在 hub 机器的 `~/.jarvis/hub.json`（0600）；邮箱凭据 AES-GCM 加密后存 Postgres，密钥文件不进数据库。
- 服务端状态全部在 hub 机器的 Postgres 容器 `jarvis-postgres`（`docs/server.md`），不要再往 `~/.jarvis/` 写 JSON 状态文件。
- **所有按钮的文字都不能换行**：按钮标签一律单行（`.lineLimit(1)`，空间紧时 `.minimumScaleFactor` 或缩短文案 / 改成图标），
  按键栏这类横排按钮用图标或短词，放不下就收进菜单 / 浮层，不能挤成两行。
- **链接在 App 内打开**（2026-10-26 用户要求）：会话里出现的任何链接（飞书文档、removed、网页……）都在当前 App 里打开，参考 Telegram：
  底部浮起的卡片，打开即铺满屏幕（2026-10-28 用户要求，原来半屏太小），下拉到 0.6 时不挡 Agent 的操作（对话照常滚动、输入），再下拉关闭。打开速度和顺滑优先，不在乎本地空间：预加载、本地缓存、
  复用 WebView 等手段都要用上。页面里选中文字可以「AI 修改」（选中内容 + 修改提示发给 Agent）或「作为上下文」（作为下一条消息的关联上下文），
  上下文用 XML 标签带在消息里，客户端解析这些标签做相应的 UI。
- **不做第二个飞书**（2026-10-27 用户纠正）：App 是把各处信息收到一起交给 AI，不是把飞书的功能页面搬过来。飞书只保留
  消息（进收件箱）、链接在 App 内渲染（文档、removed单帖），其余能力只做在 hub / AI 侧（搜索、读取作为上下文），见 `docs/feishu.md`。
  别的数据源同理：不要为某个来源做一套浏览页面。
- 质量标准：按 App Store 榜首水准设计和测试。每个界面都要在模拟器上截图检查（深色 / 浅色），手表界面也要截图，不能只看编译通过。

## 目录

| 路径 | 内容 |
| --- | --- |
| `server/` | Go 服务端 `jarvisd`（hub / node / hook / mcp 四种模式），见 `docs/server.md` |
| `ios/` | XcodeGen 工程：iOS App `Jarvis`、手表 App `JarvisWatch`、共享代码 `Shared/`、单元测试 |
| `scripts/` | 部署（`deploy.sh`）、发布 TestFlight（`release.sh`、`asc_*.py`）、读配置（`jarvis_config.py`）、截图用的假 hub（`mock_hub.py`） |
| `config.example.json` | 构建 / 部署配置的模板（`docs/config.md`） |
| `docs/` | 设计文档，索引见下 |
| `site/` | 静态站：只有首页 + 隐私政策 + 服务条款（noindex），Google OAuth 同意屏幕发布正式版必须有；不是官网 |

## 文档索引

| 文档 | 内容 |
| --- | --- |
| `docs/config.md` | 配置：构建 / 部署配置（config.json）、hub 运行配置（hub.json），哪些值不能写进代码 |
| `docs/architecture.md` | 整体架构、数据流、为什么这样设计（herdr + hooks + transcript） |
| `docs/protocol.md` | hub HTTP / WebSocket API、hub↔node RPC、事件和交互模型（前后端契约） |
| `docs/server.md` | jarvisd 各模式、配置、部署、运维 |
| `docs/claude-integration.md` | 如何在 herdr 里启动 / 驱动 Claude、hook 回答 AskUserQuestion / ExitPlanMode 的实测结论 |
| `docs/mail.md` | 邮箱：IMAP/SMTP、Gmail（使用 Google 账号登录，OAuth2 + XOAUTH2）、飞书邮箱（固定 larkoffice.com 服务器）、凭据存储 |
| `docs/phone-tools.md` | 手机数据工具（phone_*）、权限、需要确认的写操作、iOS 限制 |
| `docs/agent-states.md` | 统一的 Agent 状态（需要你 / 工作中 / 完成未看 / 已看）和 herdr 的对应，空间 / 槽位模型 |
| `docs/ios.md` | iOS / watchOS App：信息架构、设计语言、手机数据工具 |
| `docs/intents.md` | Siri / 快捷指令（App Intents）：问 Claude、状态、批准第一个待处理 |
| `docs/release.md` | 签名、TestFlight 发布流程 |
| `docs/testing.md` | 测试命令、模拟器截图检查清单 |
| `docs/codex.md` | Codex 和 TRAEX（内部 Codex 分支）支持：怎么识别、rollout 格式怎么映射成对话、还不支持什么 |
| `docs/assistant.md` | 「对话」tab 的助手：ask_user、定时任务 / 跟进、Agents 与飞书工具、实时快照、预热、记忆、后台唤醒手机 |
| `docs/inbox.md` | 收件箱：飞书和邮件合成一个列表，一行一件事，AI 分拣、已处理、交给 AI |
| `docs/live-activity.md` | 锁屏 / 灵动岛实时活动（Fleet）：显示什么、ContentState、hub 的 liveactivity 推送和 push-to-start、怎么在模拟器上验证 |
| `docs/terminal.md` | 终端 tab 的真终端方案：herdr observe 流（不 attach、不改窗格尺寸）、续传、换行、iOS 端自写 VT 子集的选型理由 |
| `docs/feishu.md` | 飞书在 Jarvis 里的定位（不做第二个飞书）：保留哪些、lark-connector 只读代理、给 AI 用的接口速查 |

## 构建与验证

- 服务端：`./scripts/test-server.sh`（在 hub 机器上连真实 Postgres 跑全部测试）；部署 `./scripts/deploy.sh [机器 id…]`（默认配置里的全部机器）。
- iOS：`./scripts/sim.sh [-openSession <machine>/<session>]` 编译、装到配置里的模拟器、自动配对 hub；截图 `xcrun simctl io <udid> screenshot`。
  直接用 xcodegen / Xcode 前先 `python3 scripts/jarvis_config.py xcconfig`（生成 bundle id、团队）。
- 发布：`docs/release.md`。

## 经验

- herdr 里新开 tab 启动 claude 时，若 cwd 未被信任会先弹「trust this folder」，`agent prompt` 发的文字会被当成选择 → 选了 No 退出。
  node 启动会话后要检查 blocked 屏幕（`docs/claude-integration.md`）。
- Claude 输入框里的灰字（预测的下一句、新会话的 `Try "…"`）在纯文本读屏里和用户打的字一模一样：判断有没有草稿要用 ansi 读屏看 dim（SGR 2）。
  2.1.312 的信任目录对话框选项不带编号（`❯ No, exit`），认对话框靠底部的 `Enter to confirm · Esc to cancel`（`server/internal/screen`）。
- AskUserQuestion：PreToolUse hook 返回 `permissionDecision: allow` + `updatedInput.answers` 即可代答（2.1.310 实测）。
- ExitPlanMode：走 PermissionRequest（不走 PreToolUse）。`allow` 必须带 `updatedInput` + `updatedPermissions:[{type:setMode,...}]`
  才生效，单纯 `{behavior:allow}` 仍会弹 TUI 对话框；`deny` + `message` 会把反馈作为 tool_result 交给模型。
- `--settings <file>` 里的 hooks 只在启动时读取，改 matcher 要重启 claude；hook 脚本本身每次执行都重新读。
- `herdr agent wait` 可能在 herdr 识别出 agent 之前就返回 agent_not_found：启动后要轮询 `agent get`。
- 没装 herdr claude integration 的机器，`agent list` 不带 session id（见 `docs/claude-integration.md` 的兜底）。
- 截图验证别靠 computer-use 点模拟器（Mac 锁屏时拿不到窗口，没锁时会抢用户的鼠标）：用 DEBUG 启动参数（`docs/ios.md`）直达页面再截图。
- 开了 HealthKit entitlement 就必须有 `NSHealthUpdateUsageDescription`（哪怕只读），否则上传报 90683。
- Go 的 `UnixListener.Close()` 默认会删掉 socket 文件：重新部署时旧 node 退出会把新 node 的 `~/.jarvis/node.sock` 删掉，
  hook / MCP 全部静默失效（hook 连不上就放行给 TUI）。已 `SetUnlinkOnClose(false)` 并每 15 秒自检重建。
- 会话的「最近编辑时间」要用最后一条真实对话（user / assistant / 工具）的时间：Claude Code 会往闲置会话追加 recap、ai-title、
  mode 等行，文件 mtime 会被刷新；找不到 transcript 的会话时间留空，不要用 now 兜底（会一直显示「刚刚」）。
- 排队消息：transcript 的 queue-operation（enqueue / dequeue / remove / popAll）能精确还原队列；TUI 里 ↑ 会把**全部**排队消息拉回输入框，
  ctrl+u 只清当前行（多行要 ctrl+u + backspace 逐行清）。长文本经 herdr bracketed paste 进去会被包成 `<pasted_content id=…>`，显示前要去掉。
- WatchConnectivity 在 activate 完成前 updateApplicationContext 会被丢弃：配对信息要在 activationDidComplete 后再推。
- Claude 工作中输入的消息（电脑或手机）会被「中途吸收」，transcript 里存成 `attachment/queued_command`（commandMode=prompt、
  origin.kind=human）而不是 user 消息，解析器要单独处理，否则手机上看不到。
- 多个 agent 同时在这个仓库干活：模拟器各用各的（`SIM=<udid> scripts/sim.sh`），别人没写完的文件编译不过时在 `git worktree` 里构建自己的改动；
  提交只暂存自己的文件 / hunk。
- `ScrollView` + `defaultScrollAnchor(.bottom)` + `LazyVStack`：跳到很远的 id 会因为上方懒加载视图实际高度与估算不同而漂移，验证历史渲染用 `-fromSeq`。
- `UNUserNotificationCenterDelegate` 不要用 async 版本的 didReceive / willPresent：返回时在后台执行器上，UIKit 收到非主线程的完成回调，
  点通知进 App 会崩。用 completion handler 版本，`DispatchQueue.main.async` 里处理完再调 completion。
- `xcodebuild test` 会启动不带 `-pairURL` 的 App 申请通知权限，之后这台模拟器的截图都会被授权弹窗挡住，跑完测试要重启模拟器再截图。
- 不要写「PrimitiveButtonStyle 里 `Button(configuration).buttonStyle(.glass)`」来包装系统按钮样式：玻璃样式下递归到爆栈，
  打开对话页就崩（崩溃栈全在 Swift 运行时的类型解析里，看不到业务代码）。按风格切样式用 View 扩展在调用处直接选（`.themedButton()`）。
- 横竖都能滑的等宽文本别用 SwiftUI「横向 ScrollView + LazyVStack」（内容宽度算不出来，只能竖滑），也别用自己滚动的 UITextView
  （TextKit 2 无论容器多宽都会折行）：用 UIScrollView 包一个不滚动、按最长行实测宽度的 UITextView（`MonoTextView`）。
- 多个 agent 同时往 master 推：rebase 冲突时不要用自己的旧副本整文件覆盖别人的改动（2026-10-25 一次 rebase 把浅蓝气泡、通知跳转修复整个盖回去了）；
  解决冲突只改冲突块，push 前 `git diff origin/master...HEAD --stat` 看看有没有动到不是自己的文件。打 TestFlight 前确认 master 是修好的那一版。
- herdr `pane read --source recent` 最多返回约 1000 行（--lines 再大也一样），没有偏移或滚动接口，也不落盘：终端历史只能分段读这 1000 行，全量在 transcript。
- 翻转的聊天列表别让它贴安全区：上方垫 1pt 空视图、输入框放在下方的 VStack 里；用 `ignoresSafeArea` + 按 inset 补边距会在键盘弹起时白屏、顶部错位。
- `ScrollViewReader.scrollTo` 会连带滚动外层容器：在顶部横排条里做 `initial: true` 的 scrollTo 把整个顶部区域推上了导航栏。
- 聊天列表用倒置（ScrollView 和每行各 `scaleEffect(x:1,y:-1)`）：最新消息在偏移 0，跳到最新永远精确。倒置后要自己处理安全区
  （`ignoresSafeArea(.container, edges: .vertical)` + `contentMargins` 按实际 inset），并关掉 iOS 26 的 `scrollEdgeEffect`（否则整页发雾）。
  只往偏移 0（最新端）跳；往远端用代码跳、或加 `defaultScrollAnchor(..., for: .sizeChanges)`，懒加载估算不准时照样白屏。
- 聊天列表（LazyVStack + 底部锚定）偶发白屏：按估算高度跳到底会落在没渲染的区域。跳转要按哨兵 id、等布局完成再跳，并在哨兵真正可见前重试；
  `atBottom` 不要默认 true（白屏时就没有箭头可点）。验证：脚本批量冷启动打开会话，按对话区像素标准差判断白屏。
- 列表上挂的 `.simultaneousGesture(TapGesture())`（点空白关闭浮层）会把长按松手也认成一次点击（按住超过约 2 秒才不认）：
  长按刚打开的消息操作条一松手就被关掉。打开它的那次按压在按住和松手后 0.3 秒内不许关闭（`MessageMenu.pressChanged`）。
- List 的分组标题（Section header）不是单元格，挂在上面的 `.contextMenu` 长按基本触发不了：标题上的操作要给一个看得见的按钮 / Menu。
- 没装 herdr claude 集成的机器认窗格对应哪个会话，用 `herdr pane process-info` 拿 claude 的 pid，再读
  `~/.claude/sessions/<pid>.json` 的 sessionId；别靠「同目录最新的 transcript」猜（同一个仓库里开六个 Agent，tab 名全串了）。
- 长列表里不要把一组可能很多的行包成一个子视图（LazyVStack 里的一个 VStack）：几百行一次布局会卡死主线程、页面空白，要拆成独立的懒加载行。
- 被取消的请求 / 任务（离开页面、切换）会抛 URLError.cancelled（「已取消」）或 CancellationError，展示错误前先过滤（`Error.shown`）。
- 新版 Claude Code 把工具调用之间的叙述写进非空的 `thinking` 块（不是 `text`），界面不能把 thinking 一律藏掉。
- SwiftUI 的 `onScrollPhaseChange` 在内容流入、偏移被程序调整时也会进 `.animating`：只想响应用户滚动就判断 `.tracking` / `.interacting`。
- MarkdownUI 的 `.markdownTextStyle` 要写在 `.markdownTheme` 里面（先调用），否则被主题的文字样式盖掉。
- macOS 的 sed 不认 `\b`，整词替换用 `perl -pi -e 's/\bFoo\b/Bar/g'`。
- 模拟器里上一次启动遗留的系统授权弹窗，卸载重装 App 也不消失，要 `simctl shutdown` 重启模拟器（手动 `simctl launch` 不带 `-pairURL` 就会弹通知授权）。
- 飞书消息渲染照搬 lark-connector（`docs/ios.md`「飞书消息渲染」）：hub 原样转发 `parts`，只把媒体 URL 改写到 `/api/v1/lark/media/`；
  看 lark-connector 源码直接 `git clone https://github.com/Eric-2369/lark-connector`（hub 机器上只有部署产物）。
- 自定义 SwiftUI `Layout` 的 `proposal.width` 可能是 `.infinity`，`Int(...)` 前不判 `isFinite` 会直接崩（飞书卡片列布局踩过）。
- lark-connector 会话列表的坑（`docs/protocol.md` 飞书 API）：`isDone` 字段不可靠（done scope 里全是 false），已完成以 scope 为准；
  置顶看 `onTopRankAt`（`settings.isShortcut` 也不准），且置顶会话分散在 inbox 和 done 两个 scope；未知 scope 会被静默当成 inbox。
- 打开飞书会话不能用 `tail=1`（那是「回到最新」）：有未读时要 `aroundPosition=readPosition+1`，已读只上报真正上屏的消息。
- DEBUG 启动参数的名字不能和任何 Swift 标识符同名：枚举 case 名会以反射元数据进 Release 二进制，`release.sh` 的 `strings` 检查会误报
  （`Tab.workbench` 撞了 `-workbench`，已改名 `-openWorkbench`）。新开关用 `open…` / `show…` 这类不会是类型成员的名字，并加进 release.sh 的列表。
- 有的网络（作者的 hub 机器）连不上 Google 的多数 IPv4 地址（`imap.gmail.com` / `smtp.gmail.com` 常解析到 173.194.x.x，所有端口超时），HTTPS 能通只是走了 IPv6。
  hub 访问 Gmail 一律走 `hub.json` 的 `google.proxy`（比如 hub 机器上的本地代理），别再加直连兜底（`docs/mail.md`）。
- hub 的内存状态随时会被别的 agent 重新部署清掉（2026-10-25 两分钟内被部署三次）：跨请求的流程（比如 OAuth state）要自包含或落库。
- 多个 agent 并行改同一仓库（飞书工作台 6 个 worker，见 `docs/feishu.md`）：每人独立 worktree + 独立克隆模拟器；新增类型带领域前缀
  （`FeishuSearchXxx`…，否则 master 撞名编译失败）；rebase 冲突时自己目录外的文件一律取 origin/master 版本，push 前看
  `git diff origin/master --stat` 只含自己的文件（14fe469 曾用旧副本覆盖半个仓库，941d067 恢复）；主工作区有别人未提交的改动时，
  自己的修复在 `git worktree add --detach /tmp/x origin/master` 里做。
- SwiftUI：给 NavigationStack 推出页面用的环境值（如 `feishuOpen`）要挂在 NavigationStack 本身上，挂在栈根内容上推出的页面拿不到。
- 代理视频 / 音频必须转发 Range / Content-Range（AVPlayer 先要 2 个字节，拿到整个文件就拒播）；`URL.appending(path:)` 会把 `%`
  再编码一次，ID 里带 `/` 的路径要用 `percentEncodedPath` 自己拼。
- 无头 `claude -p` 里 AskUserQuestion 不可用（不是 hook 没接上，是工具本身被禁用）：要在手机上问用户，给它一个会阻塞等答案的 MCP 工具
  （`ask_user`，`docs/assistant.md`）。模型会把它和别的工具并行调用，有副作用的工具要自己在 hub 里批准。
- App 进后台不断开 WebSocket 的话，挂起的 socket 在 hub 看来还活着，`phone_*` 调用会干等到超时；现在进后台就断开，需要时用静默推送唤醒。
- 新会话（无头助手或刚启动的 herdr Agent）在 claude 写下第一行之前没有 transcript：手机一订阅 hub 就要 backlog，`session.items`
  对这种「正在启动」的会话（`startingSession`）要返回空列表而不是 not found，watch 等文件出现后先补发已写的行，否则对话页会闪一下报错。
- 跟手的收起 / 展开（Safari 式工具栏）不要做成「切换状态 + 动画」：把进度（0…1）放在单独的 `@Observable` 里、只给那一小块视图读，
  滚动回调每帧写它；动起来的那块浮在内容上（overlay），别让内容的 frame 跟着变，否则每帧重排整页、消息跟着跳。松手再吸附。
- 隐藏系统导航栏（`.toolbarVisibility(.hidden, for: .navigationBar)`）会顺带关掉边缘右滑返回，要自己把 `interactivePopGestureRecognizer` 打开。
- `xcrun simctl launch` 对已经在运行的 App 只是拉到前台、新启动参数不生效：截图前用 `--terminate-running-process`。
- 对话行的 body 会被反复求值（滚动、流式更新）：从消息文字推出来的东西（Markdown 解析、路径 / 链接识别、去掉标签后的文字）
  按消息 id 缓存（`RowMemo`，`TextMemo`），并在消息加载后放到后台预热；别在 body 里跑正则 / NSDataDetector（实测是主线程最大的热点）。
- 滚动时别在主线程建 WKWebView：链接预加载只给刚出来的回复（15 分钟内），翻历史时每条旧回复都去预加载曾造成 1–2 秒的冻结。
- 滚动中写入的簿记（可见行、手指是否按着）放在不被观察的对象里（`ChatScratch`），写 @State 会让整页重算；派生的行列表按
  `ChatStore.version` 等缓存。WebSocket 消息在回调线程上解码，别回到主线程再解。
- 「还有没有更早的消息」（`has_more`）只有从最新一端分页的响应才知道：订阅时带 since_seq 的补发（catch-up）以前发 `has_more: false`，
  它要是比 HTTP 第一页先到，App 就以为历史到头了，往上翻停在已加载的最早一条（2026-10-26）。现在 hub 补发时不带这个字段，
  App 把缺失当未知，而且一批没覆盖到本地最早一条的消息不许把它改成 false。复现：`-showScrollToOldest YES` 一路往上翻并记日志。
- 带很多截图的会话，一页更早的消息有 1–5 MB（图片 base64 内嵌）：分页在后台线程解码，最早的十几行一出现就预取前一页。
- 后台同步别靠「订阅所有会话」：订阅会让 node 每 250ms 刷 transcript、每 400ms 读一次终端（herdr 进程调用）。改成 IM 式的推拉结合：
  hub 只推每个会话的 `rev`（transcript 字节数），App 按 `after_seq` 拉增量（`docs/ios.md` 后台同步）。整个会话列表约 130 KB，
  别为 rev 变化重发列表，用单独的 `revs` 事件。lark-connector 有实时事件 WebSocket（`/agent/v1/events`，`message.upserted` /
  `conversation.upserted`），飞书的推送从这里来。
- hub 的 API 默认回 `Cache-Control: no-store`：不加的话 iOS 的 URLCache 会把每个 JSON 分页存一份（后台同步十几 MB，从来不读）。
  App 自己做磁盘缓存的东西（对话图片、飞书媒体）用 `APIClient.uncachedSession` 取，否则同一份字节存两遍。
- 聊天列表用 `ScrollPosition.scrollTo(y:)` 会把横向偏移也归零：左右边距是 `contentMargins(.horizontal)`，整页会贴到屏幕边。
  竖向跳转用 `scrollTo(point:)` 带上当前的 x（`ChatView.scrollList`）。`scrollTo(id:anchor:)` 跳到没画过的行只按估算高度落点，
  要精确停在某处就跳过去后按实测位置再修正一两次。倒置列表里的行，XCUITest 拿到的 frame 不对（报在屏幕外），量位置用 App 内的 DEBUG 探针。
- herdr 不认识的 Agent（TRAEX）：herdr 只报它内置识别的 Agent，`agent prompt` 对其他窗格报 not found。node 用 owner 文件（pid → 会话）
  加 pid 环境变量里的 `HERDR_PANE_ID`（pid → 窗格）自己认，状态看标题转圈字符和屏幕（`docs/codex.md` TRAEX 一节）。
- Claude Code 的配置目录不一定是 `~/.claude`：启动器设了 `CLAUDE_CONFIG_DIR`（多套 profile）时 transcript、`sessions/<pid>.json`、`settings.json`
  都在那个目录。node 每 30 秒扫一次正在跑的 claude 进程的环境变量（Linux 读 `/proc`，macOS 一次 `ps -E`），所有目录一起找，
  找到的目录记进数据库；平时不跑的 profile 写进 `hub.json` 的 `machines[].claude_config_dirs`（`server/internal/node/claudedirs.go`）。
  以前靠把 `~/.claude/projects`、`sessions` 软链到 profile 绕过，软链留着也没事（按真实路径去重），不用再加。
  漏掉目录的症状：助手续聊报「Session ID … is already in use」、App 里 transcript not found、herdr 里的 Agent 认不出会话。
- 多个配置目录全被 node 看到后（PR #12），一台跑十几个 Agent、transcript 上百 MB 的机器上 node 持续读盘约 20 MB/s：每个 herdr 事件都重建会话列表，
  每次都把每个在增长的 transcript 尾部 384 KB 重读一遍。现在列表重建每秒最多一次，增长中的文件 meta 最多 3 秒重读一次（`metaMinInterval`），
  后台任务扫描流式读（原来第一次整份读进内存），而且大文件的第一次读放到后台逐个做：建会话列表曾等它读完几 GB，node 重启后那台机器好几分钟一个会话都不显示。冷打开几百 MB 的 transcript 要整份解析（317 MB 约 20 秒，hub 15/30 秒超时 502），
  node 启动后会逐个预热正在跑的会话；量延迟要用真实的大会话按 App 冷打开的参数（`items?turns=2`）测 P50 / P99。
- `scripts/test-server.sh` 在两个 checkout 里同时跑会共用同一个 `jarvis_test` 库，DB 测试会互相干扰随机失败：并发跑时单独重跑失败的包确认。
- hub 机器走 TUN 代理时，系统自动升级会重启 systemd-networkd，它默认删掉别的程序加的 ip rule（代理的路由规则没了，经代理的连接全部超时；
  Tailscale 会自己补回，代理不会）：在那台机器上用 networkd 的 `ManageForeignRoutingPolicyRules=no` 防止（2026-10-29 推送因此断了约 3 小时）。
  APNs 是一条长连的 HTTP/2，网络断过以后 Go 不会自己发现连接已死，推送请求出错时要关掉空闲连接，下一次重新拨号（`push.go`）。
- Claude Code 默认清理 30 天前的 transcript（`cleanupPeriodDays`，启动时做，2026-10-29 一次删掉了这台机器 200 多个会话）：
  想留作解析样本的会话要及时脱敏存成 fixture（`server/internal/transcript/testdata/README.md`）。脱敏只能白名单式，
  「短数字原样保留」这种放行会把 IP 的每一段都放过去（踩过）。
- 实时活动（`docs/live-activity.md`）：ActivityKit 用默认 JSONDecoder 解推送的 `content-state`，`Date` 会按 2001 年起算，时间一律传 Unix 秒数字；
  锁屏 / 展开态限高约 160pt，超出会被静默裁掉；`simctl io screenshot` 拍不到灵动岛和锁屏上的活动（用 UI 测试 `LiveActivityShots`），
  `simctl push` 不支持实时活动推送，但模拟器能收真实的 APNs sandbox 推送（`TestManualLiveActivityPush`）。
  重复的 symbol 动画（`.rotate` / `.pulse` + `.repeating`）在活动里不动（像素比对验证过），别设计「转动的齿轮」这种效果。
  灵动岛上的数字（TPM、token、负载）会占推送预算：hub 只推设备显示的项、取两位有效数字、每台最多每分钟一次。
- UI 测试锁了模拟器屏幕却没解锁（模拟器上按 Home 不解锁，要上滑），下一次 `xcodebuild test` 报「test runner hung before establishing connection」。
  模拟器系统语言是英文时，系统授权弹窗的按钮叫「Allow」，UI 测试两种都要认。
- 倒置聊天列表往上翻时的「冻结」不能按行 id 截：行 id 会随轮次推进变（答案收进工作行、多条消息的轮合并成 `wt-`），
  找不到 id 冻结就失效、可见内容被顶走（修前实测 319pt）。按「离开最新端时的 item 快照」重建行（`ChatView.Frozen`），验证用 `-showFreezeProbe`。
- 改 transcript 解析或同步协议，用真实语料验证（`docs/lanes/msg-render-verification.md`）：`corpus_test.go` 让新旧解析器逐文件对比，
  `continuity_corpus_test.go` 按 App 的规则模拟丢推送 / 补洞 / 重写。语料只放 `~/jarvis-corpus`；活跃会话的文件会一边读一边长，对比前先拷一份快照。
  模拟器里的 App 看真实会话用本机的测试 hub（假 HOME、不连 herdr、独立数据库），别连线上 hub。
- 大 transcript 冷打开只解析末尾窗口（`transcript/window.go`）。一行解析成什么只通过三样东西依赖前面的行：已显示过的后台任务通知
  （重复的丢掉）、上一个「! cmd」输入、Codex 同一 item 记两次。改解析器时如果新加了「跨行状态」，必须同时让窗口的起点能重建它
  （`seeded` / `seeNotifications`），否则窗口读和整份读会不一样；`TestFixtureWindowedEqualsFull` 和语料的 `TestCorpusWindowed` 会抓到。
- 用户发的图片有两条路：普通消息里是 user 的 image 块；Claude 工作中发的消息被「中途吸收」成 `attachment/queued_command`，图片在 prompt 的块数组里
  （2026-10-29 以前解析器只取 text 块，气泡里看不到图）。App 里有后台任务唤醒过的轮（`pickAnswerAcrossWakes`）要把用户的图片和消息一起留在原处，别当成工作过程。
- `URLSessionWebSocketTask.sendPing` 的回调只在两个条件都满足时才来：对端的 pong 原样带回 ping 的负载（空 pong 不算），
  而且当时有一个挂着的 `receive`。`scripts/mock_hub.py` 曾回空 pong，App 的心跳就每分钟判一次连接已死、重连（hub 用的 coder/websocket 没问题）。
- 要分清单击 / 快速双击 / 按住的键（终端键栏的修饰键）别用 `DragGesture(minimumDistance: 0)` 记按下松开：快速双击的第二下它收不到
  （XCUITest 的 doubleTap 只来一对 down / up）。用一个覆盖在上面的 UIView 重写 touchesBegan / Ended（`TerminalKeys.swift` 的 `TouchArea`）。
- Swift 的 `dict[k, default: X].method()`：值是 class（`CGMutablePath`）时只是读，默认值不会存进字典，改动全丢（终端框线整条消失过）。
- 网格里写宽字符要分两步：左半照常写，右半（宽 0 的占位格）不能走「覆盖宽字符右半就清掉左半」的规则。
  否则在同一位置重写一个宽字符时，刚写的字会被清掉。herdr 流式重画中文正是这样写的，手机上整段缺字（`TerminalGrid.putRightHalf`）。
  改终端解析一律用录制的真实帧回放，和 `herdr pane read` 逐行比（`TerminalWrapTests`）。
- 自定义视图直接收键盘（`UITextInput`）时，`hasText` 要恒为 true，否则空文档时键盘会自动切大写；拼音靠 marked text。
  选定候选时可能来 `insertText`，也可能来 `setMarkedText` + `unmarkText`，两种都要当提交处理（`TerminalInput.swift`）。
- 终端流的帧率上限是 Agent 自己的重画节奏，不在我们的链路上。Claude Code 2.1.312 流式时约 110 ms 重画一次（主要是转圈那一行），
  正文分几次大块整屏出；herdr observe 最快每 16 ms 一帧；node / hub 原样转发，只把 30 ms 内的帧合成一帧（`termMinGap`）。
  分段实测见 `docs/terminal.md` 第十节。
- XCUITest 在键盘弹起的动画中点键栏会点空（键栏正跟着键盘移动）：先等键盘出现、停一下再点。
- herdr 的 observe 流要按窗格本身的尺寸开（`--cols/--rows` 小了是裁左上角，不是重排），窗格变尺寸后要重开；`terminal attach` 会改窗格尺寸、占唯一槽位，别用（`docs/terminal.md`）。
- 等后台任务 / 日志的脚本一律带超时，成功、失败、超时都退出并打印一行结论（2026-10-29 一个只等成功的 `until grep` 空等了五个多小时）。
- `TextField(text:selection:)` 的选区：没在编辑（未聚焦）时也会报一个选区，冷启动恢复草稿后它指在草稿中间；而且它的
  `String.Index` 可能来自另一份字符串，越界的 `utf16Offset(in:)` 直接崩。插入文字（发送文件的路径）只在聚焦时用选区，先夹到 `endIndex` 再换算（`PathInsert`）。
  UI 测试里草稿会跨次运行保留，测输入框内容前先清掉。
- 2026-10-28 改写过一次全部 git 历史（去掉内网信息和个人信息，GitHub 仓库删了重建）：此前的提交号全部失效，
  别的机器上的旧克隆要删掉重新 clone，不能 push，否则旧历史会被推回去。
