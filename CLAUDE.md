# CLAUDE.md

Jarvis：只给自己用的 iPhone + Apple Watch 助手。「对话」tab 是能访问所有个人和公司数据的 AI 助手（example-machine 上一个专用目录里的 Claude），
「开发」tab 是在手机上用聊天的方式驱动各台电脑上 herdr 里的 Claude Code（实时流式看过程、
在手机上回答提问 / 审批计划 / 批准工具），并让 Claude 读写 iPhone 本地数据（通讯录、日历、提醒、照片、健康、位置……）和邮箱
（Gmail / 任意 IMAP+SMTP）。服务端 `jarvisd`（Go）部署在 example-machine，经 SSH 接入 example-machine / example-machine / mac-home。
只发 TestFlight（内部测试员 tester@example.com），**不做官网、不上架**。

## 约定（必须遵守）

- **每完成一个功能就自动提交并推送**（`git push origin master`），不用等用户要求。Conventional Commits
  （`feat(ios): ...`、`feat(server): ...`、`fix(watch): ...`、`docs: ...`、`chore(release): ...`、`test(...)`），
  作者 `chyroc <chyroc@qq.com>`（仓库已 `git config`）。只 `git add` 自己改过的文件；push 被拒先 `git pull --rebase` 再推。
- **随时沉淀经验**：踩坑、怪异行为、设计取舍当场写进本文件或 `docs/<主题>.md`。这条规则本身也要保留。
- 代码注释、提交信息用英文；文档用中文；App 界面文案用中文。
- **绝不访问 iCloud 目录**（`~/Library/Mobile Documents/...`）：这台 Mac 未解锁，一访问就卡死。需要里面的东西就直接问用户。
  App Store Connect API key 在 `~/.appstoreconnect/AuthKey_KEYID00001.p8`（不在 iCloud），发布证书在 `~/.appstoreconnect/signing/`。
- **密钥绝不入库**：hub token、数据库密码只存在 example-machine `~/.jarvis/hub.json`（0600）；邮箱凭据 AES-GCM 加密后存 Postgres，密钥文件不进数据库。
- 服务端状态全部在 example-machine 的 Postgres 容器 `jarvis-postgres`（`docs/server.md`），不要再往 `~/.jarvis/` 写 JSON 状态文件。
- 分工：设计、UI、交互、协议、核心链路由 leader（当前 Opus）实现；其余可以派出去的活在 herdr 当前 workspace 新开 tab，
  **用 `ccc` 启动（Opus 5.5）**，leader 派发和验收。（2026-10-24 用户要求：之前用的 `cc_ds` 不再用于派活。）
- **所有按钮的文字都不能换行**：按钮标签一律单行（`.lineLimit(1)`，空间紧时 `.minimumScaleFactor` 或缩短文案 / 改成图标），
  按键栏这类横排按钮用图标或短词，放不下就收进菜单 / 浮层，不能挤成两行。
- **链接在 App 内打开**（2026-10-26 用户要求）：会话里出现的任何链接（飞书文档、removed、网页……）都在当前 App 里打开，参考 Telegram：
  底部浮起的卡片，上拉全屏、下拉收起，不挡 Agent 的操作（对话照常滚动、输入）。打开速度和顺滑优先，不在乎本地空间：预加载、本地缓存、
  复用 WebView 等手段都要用上。页面里选中文字可以「AI 修改」（选中内容 + 修改提示发给 Agent）或「作为上下文」（作为下一条消息的关联上下文），
  上下文用 XML 标签带在消息里，客户端解析这些标签做相应的 UI。
- 质量标准：按 App Store 榜首水准设计和测试。每个界面都要在模拟器上截图检查（深色 / 浅色），手表界面也要截图，不能只看编译通过。

## 目录

| 路径 | 内容 |
| --- | --- |
| `server/` | Go 服务端 `jarvisd`（hub / node / hook / mcp 四种模式），见 `docs/server.md` |
| `ios/` | XcodeGen 工程：iOS App `Jarvis`、手表 App `JarvisWatch`、共享代码 `Shared/`、单元测试 |
| `scripts/` | 部署（`deploy.sh`）、发布 TestFlight（`release.sh`、`asc_*.py`） |
| `docs/` | 设计文档，索引见下 |
| `site/` | `jarvis.chirpet.com`：只有首页 + 隐私政策 + 服务条款（noindex），Google OAuth 同意屏幕发布正式版必须有；不是官网。部署 `cd site && ~/code/chyroc/chirpet.com/scripts/wrangler.sh deploy` |

## 文档索引

| 文档 | 内容 |
| --- | --- |
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
| `docs/feishu.md` | 飞书信息能力（工作台）：参考飞书源码 `florak/` 的功能、lark-connector 只读代理、各领域接口与分工 |

## 机器

| id | 机器 | 接入方式 |
| --- | --- | --- |
| `example-machine` | 公司 example-machine hub-host（hub 所在） | 本机 |
| `example-machine` | 公司里的公司 Mac example-machine（也是开发机） | `ssh example-machine`（example-machine 反向隧道 2222） |
| `example-machine` | 家里的公司 Mac example-machine | `ssh example-machine`（2223） |
| `mac-home` | 家里的个人 Mac | `ssh mac-home`（2224，隧道暂未就绪） |

隧道见 `docs/config.md`。hub 入口 `http://hub.example.com:18660`（手机需在公司网络 / removed）。

## 构建与验证

- 服务端：`./scripts/test-server.sh`（在 example-machine 上连真实 Postgres 跑全部测试）；部署 `./scripts/deploy.sh [example-machine|example-machine|…]`。
- iOS：`./scripts/sim.sh [-openSession <machine>/<session>]` 编译、装到 iPhone 17 Pro 模拟器、自动配对真实 hub；截图 `xcrun simctl io <udid> screenshot`。
- 发布：`docs/release.md`。

## 当前状态（2026-10-24）

- hub 已部署在 example-machine（:18660），example-machine / example-machine / example-machine 在线，mac-home 隧道未装。
- 端到端跑通：手机新建会话、提问、计划审批、手机工具（`docs/testing.md`）。邮件模块上线，还没加账号。
- App Store Connect App ID 1234567890（Jarvis Herd），APNs 已开（`docs/release.md`）。待办：真机验证。
- 2026-10-25「邮件」tab 重构为「信息」：飞书（lark-connector）+ 邮件；助手改为无头 claude -p。
- 2026-10-24 服务端状态迁到 example-machine Postgres 容器 `jarvis-postgres`；新增「对话」tab（Jarvis 助手），TestFlight 202610242210。

## 经验

- herdr 里新开 tab 启动 claude 时，若 cwd 未被信任会先弹「trust this folder」，`agent prompt` 发的文字会被当成选择 → 选了 No 退出。
  node 启动会话后要检查 blocked 屏幕（`docs/claude-integration.md`）。
- AskUserQuestion：PreToolUse hook 返回 `permissionDecision: allow` + `updatedInput.answers` 即可代答（2.1.310 实测）。
- ExitPlanMode：走 PermissionRequest（不走 PreToolUse）。`allow` 必须带 `updatedInput` + `updatedPermissions:[{type:setMode,...}]`
  才生效，单纯 `{behavior:allow}` 仍会弹 TUI 对话框；`deny` + `message` 会把反馈作为 tool_result 交给模型。
- `--settings <file>` 里的 hooks 只在启动时读取，改 matcher 要重启 claude；hook 脚本本身每次执行都重新读。
- `herdr agent wait` 可能在 herdr 识别出 agent 之前就返回 agent_not_found：启动后要轮询 `agent get`。
- example-machine 的 herdr 没装 claude integration，`agent list` 不带 session id（见 `docs/claude-integration.md` 的兜底）。
- 这台 Mac 屏幕锁着时 computer-use 拿不到模拟器窗口：用 DEBUG 启动参数（`docs/ios.md`）直达页面再截图。
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
- List 的分组标题（Section header）不是单元格，挂在上面的 `.contextMenu` 长按基本触发不了：标题上的操作要给一个看得见的按钮 / Menu。
- 没装 herdr claude 集成的机器（example-machine）认窗格对应哪个会话，用 `herdr pane process-info` 拿 claude 的 pid，再读
  `~/.claude/sessions/<pid>.json` 的 sessionId；别靠「同目录最新的 transcript」猜（app 六个 Agent 在同一个仓库，tab 名全串了）。
- 长列表里不要把一组可能很多的行包成一个子视图（LazyVStack 里的一个 VStack）：几百行一次布局会卡死主线程、页面空白，要拆成独立的懒加载行。
- 被取消的请求 / 任务（离开页面、切换）会抛 URLError.cancelled（「已取消」）或 CancellationError，展示错误前先过滤（`Error.shown`）。
- 新版 Claude Code 把工具调用之间的叙述写进非空的 `thinking` 块（不是 `text`），界面不能把 thinking 一律藏掉。
- SwiftUI 的 `onScrollPhaseChange` 在内容流入、偏移被程序调整时也会进 `.animating`：只想响应用户滚动就判断 `.tracking` / `.interacting`。
- MarkdownUI 的 `.markdownTextStyle` 要写在 `.markdownTheme` 里面（先调用），否则被主题的文字样式盖掉。
- macOS 的 sed 不认 `\b`，整词替换用 `perl -pi -e 's/\bFoo\b/Bar/g'`。
- 模拟器里上一次启动遗留的系统授权弹窗，卸载重装 App 也不消失，要 `simctl shutdown` 重启模拟器（手动 `simctl launch` 不带 `-pairURL` 就会弹通知授权）。
- 飞书消息渲染照搬 lark-connector（`docs/ios.md`「飞书消息渲染」）：hub 原样转发 `parts`，只把媒体 URL 改写到 `/api/v1/lark/media/`；
  看 lark-connector 源码直接 `git clone https://github.com/Eric-2369/lark-connector`（example-machine 上只有部署产物）。
- 自定义 SwiftUI `Layout` 的 `proposal.width` 可能是 `.infinity`，`Int(...)` 前不判 `isFinite` 会直接崩（飞书卡片列布局踩过）。
- lark-connector 会话列表的坑（`docs/protocol.md` 飞书 API）：`isDone` 字段不可靠（done scope 里全是 false），已完成以 scope 为准；
  置顶看 `onTopRankAt`（`settings.isShortcut` 也不准），且置顶会话分散在 inbox 和 done 两个 scope；未知 scope 会被静默当成 inbox。
- 打开飞书会话不能用 `tail=1`（那是「回到最新」）：有未读时要 `aroundPosition=readPosition+1`，已读只上报真正上屏的消息。
- DEBUG 启动参数的名字不能和任何 Swift 标识符同名：枚举 case 名会以反射元数据进 Release 二进制，`release.sh` 的 `strings` 检查会误报
  （`Tab.workbench` 撞了 `-workbench`，已改名 `-openWorkbench`）。新开关用 `open…` / `show…` 这类不会是类型成员的名字，并加进 release.sh 的列表。
- example-machine 连不上 Google 的多数 IPv4 地址（`imap.gmail.com` / `smtp.gmail.com` 常解析到 173.194.x.x，所有端口超时），HTTPS 能通只是走了 IPv6。
  hub 访问 Gmail 一律走 `hub.json` 的 `google.proxy`（比如 hub 机器上的本地代理），别再加直连兜底（`docs/mail.md`）。
- hub 的内存状态随时会被别的 agent 重新部署清掉（2026-10-25 两分钟内被部署三次）：跨请求的流程（比如 OAuth state）要自包含或落库。
- 多个 agent 并行改同一仓库（飞书工作台 6 个 worker，见 `docs/feishu.md`）：每人独立 worktree + 独立克隆模拟器；新增类型带领域前缀
  （`FeishuSearchXxx`…，否则 master 撞名编译失败）；rebase 冲突时自己目录外的文件一律取 origin/master 版本，push 前看
  `git diff origin/master --stat` 只含自己的文件（6b607aa 曾用旧副本覆盖半个仓库，fb4c260 恢复）；主工作区有别人未提交的改动时，
  自己的修复在 `git worktree add --detach /tmp/x origin/master` 里做。
- SwiftUI：给 NavigationStack 推出页面用的环境值（如 `feishuOpen`）要挂在 NavigationStack 本身上，挂在栈根内容上推出的页面拿不到。
- 代理视频 / 音频必须转发 Range / Content-Range（AVPlayer 先要 2 个字节，拿到整个文件就拒播）；`URL.appending(path:)` 会把 `%`
  再编码一次，ID 里带 `/` 的路径要用 `percentEncodedPath` 自己拼。

