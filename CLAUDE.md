# CLAUDE.md

Persona：给自己用的 iPhone + Apple Watch 助手。「Persona」tab 是能访问个人和工作数据的 AI 助手（hub 机器上一个专用目录里的 Claude，有一个常驻的主对话），
知识库 `~/.persona/data` 把各处的聊天、人、群、项目沉淀下来给它和 AI 分身用，
「Agents」tab 是在手机上用聊天的方式驱动各台电脑上 herdr 里的 Claude Code / Codex（实时流式看过程、
在手机上回答提问 / 审批计划 / 批准工具），并让 Claude 读写 iPhone 本地数据（通讯录、日历、提醒、照片、健康、位置……）和邮箱
（Gmail / 任意 IMAP+SMTP）。服务端 `personad`（Go）的 hub 跑在一台常开的机器上，经 SSH 接入其他电脑。通过 TestFlight 分发。

**这台开发机、这个人的具体情况（机器、地址、账号、个人工作方式）在 `CLAUDE.local.md`**（不提交）；没有它就先看 `docs/config.md` 配好。

**名称与兼容**：产品 Persona（原 Jarvis），服务端 `personad`（`jarvisd` 兼容别名），迁移版本的新默认目录是 `~/.persona`、构建配置是 `~/.config/persona/config.json`；`PERSONA_*` 优先，`JARVIS_*` 及旧目录继续兼容，见 `docs/config.md`「命名与兼容」。下文的源码路径、target / scheme、签名 profile、数据库 / 容器身份、协议和持久化键按实际实现保留，不按产品名盲替换；形象的 `persona` 术语不变。

## 约定（必须遵守）

### 流程

- **所有改动都走「issue → 认领 → PR → 自行合并」**（步骤见下面「开发工作流」）：需求和 fix 都先有 issue，
  代码、文档、配置等仓库内的所有改动一律在独立分支 / worktree 上 commit，只 push 到该分支，再提 PR 关联 issue。禁止在主分支上直接 commit 或 push。
  本约定取代 `CLAUDE.local.md` 等文件中直接提交、推送到主分支的旧流程；自动提交和发布仍按原约定执行，但必须先通过 PR 合并。
- **先认领 issue 再动手**（多人和各自的 Agent 同时在改，出现过两人修同一个 bug）：开工前查同一件事的 open issue / PR，
  别人认领了、24 小时内有更新就不碰（可以补充信息）；认领写成 issue 评论，谁在做以评论的 GitHub 账号为准，不用 assign；
  PR 不关联 issue 会被 Action 关闭。流程、评论模板和 gh 命令见 `docs/collaboration.md`。
- **TestFlight 来的反馈要标出来源**（2026-11-04 用户要求）：崩溃报告、测试员的文字 / 截图反馈建 issue 时一律加 `testflight` 标签，没有加标签的权限就在标题前加 `[TestFlight]`；
  正文脱敏（不放测试员身份、设备 / 崩溃标识、路径、内存地址、原始附件），末尾带去重标记，见 `docs/collaboration.md`「TestFlight 反馈」。
- **所有 commit 必须用英文**：commit 的标题和正文，以及 PR 的标题和正文都用英文，
  因为 squash 合并直接把 PR 标题和正文当成 master 上的 commit 信息。代码注释也用英文；文档、issue 用中文；App 界面文案用中文。
- **随时沉淀经验**：踩坑、怪异行为当场写进 `docs/lessons/<主题>.md`（索引 `docs/lessons/README.md`），设计取舍写进 `docs/<主题>.md`；
  跨模块、最常踩的再在本文件「最常踩的坑」留一行。本文件只放约定和最核心的东西，别再往里堆细节。这条规则本身也要保留。
  一次性的车道方案、验证 / 验收报告、审计快照做完放 `docs/archive/`，不进顶层和文档索引；挪了文件同时改文档里的路径（`scripts/test/test_doc_paths.py` 会查）。
- **质量标准**：按 App Store 榜首水准设计和测试。每个界面都要在模拟器上截图检查（深色 / 浅色），手表界面也要截图，不能只看编译通过。

### 隐私和密钥

- **配置化，不写死个人的东西**：机器名、域名、账号、邮箱、路径、启动器、公司内部工具、个人偏好一律走配置
  （构建 / 部署：`~/.config/persona/config.json`；运行：hub 的 `~/.persona/hub.json`；见 `docs/config.md`），代码、脚本、
  界面文案、测试、文档、本文件里都不出现具体某个人的值（内网域名 / IP、主机名、内部项目名、个人账号、各种 key 的 ID 也算）；界面说「hub 所在的机器」「你的各台电脑」。本机的具体情况写 `CLAUDE.local.md`。
- **公网直连**（2026-10-30 用户要求）：手机经公网直连 hub 要能通，优先探测和使用 IPv6；VPN 由用户自己配置。公网用 TLS，内网兼容现有客户端，见 `docs/network.md`。
- **不提交任何内网信息或个人信息**（2026-10-28 用户要求；`scripts/privacy_check.py` 自动查，见 `docs/testing.md`「隐私扫描」）：代码、测试数据、文档、截图、提交信息里都不能有内网域名 / IP / URL、
  主机名、内部项目 / 仓库名、真实的公司或个人数据（消息、文档链接、人名），以及个人账号、邮箱、各种 key / 项目的 ID。示例一律用占位符
  （`<hub 地址>`、`me@example.com`）；具体值只写配置或 `CLAUDE.local.md`。Persona 支持的产品 / 工具名（飞书、TRAEX、removed 等）不算。
- **密钥绝不入库**：hub token、数据库密码只存在 hub 机器的 `~/.persona/hub.json`（0600）；邮箱凭据 AES-GCM 加密后存 Postgres，密钥文件不进数据库。

### 服务端和数据

- **服务端状态全部在 hub 机器的 Postgres 容器** `jarvis-postgres`（`docs/server.md`），不要再往 `~/.persona/` 写 JSON 状态文件。
  例外是知识库 `~/.persona/data`：所有数据最终沉淀在 `.persona`，文件为主、库做索引（`docs/knowledge.md`）。
- **提示词一律放 `server/internal/prompts/`**（含 MCP 工具的描述和参数说明，在 `mcp/`）：一个提示词一个文件（按场景分子目录），同一个 Agent 的几段用模板组合，不在 Go 代码里 fmt / + 提示词；调用方只准备数据。改了提示词要更新 golden 测试并看 diff，见 `docs/prompts.md`。

### 产品和界面

- **不做第二个飞书**：App 是把各处信息收到一起交给 AI，不是把飞书的功能页面搬过来。飞书只保留
  消息（进收件箱）、链接在 App 内渲染（文档、公司圈单帖），其余能力只做在 hub / AI 侧（搜索、读取作为上下文），见 `docs/feishu.md`。
  用户明确允许的窄例外（#392）：飞书设置里默认关闭的「首页显示消息」开关，开启后增加「消息」页，浏览全部私聊和群聊；
  复用收件箱的账号、聊天页、发件箱和同步，不另做客户端，不恢复通讯录、工作台等页面。见 `docs/lark-connector-migration.md`。
  别的数据源同理：不要为某个来源做一套浏览页面。
- **链接在 App 内打开**：会话里出现的任何链接（飞书文档、公司圈、网页……）都在当前 App 里打开，参考 Telegram：
  底部浮起的卡片，打开即铺满屏幕（不是半屏），下拉到 0.6 时不挡 Agent 的操作（对话照常滚动、输入），再下拉关闭。打开速度和顺滑优先，不在乎本地空间：预加载、本地缓存、
  复用 WebView 等手段都要用上。页面里选中文字可以「AI 修改」（选中内容 + 修改提示发给 Agent）或「作为上下文」（作为下一条消息的关联上下文），
  上下文用 XML 标签带在消息里，客户端解析这些标签做相应的 UI。
- **不许直接改已有界面的样子**：
  换一种视觉风格（组件库、设计语言）一律做成新的**界面风格**（设置 › 外观 › 界面风格，`AppStyle`），换一套配色做成新的**主题色**（`ThemeColor`），
  只在选了它时生效，默认和其他选项的界面一像素都不变。修 bug、按用户明确要求改某个界面的不算。
  Android 2026-10-31 按用户要求整体重做成三套界面风格（玻璃默认、晨报、看板），页面只写一套，差别全在设计系统的组件和外壳里，见 `docs/android-design.md`。
- **界面只用设计系统**（2026-10-31 用户要求：设计系统是仓库的约束）：字号、间距、圆角、尺寸、动画、颜色、图标、品牌标志、风格一律取自设计系统，
  页面里不写死数值、系统字号、SF Symbol 名字、十六进制颜色；缺什么先加 token。见下面「设计系统（iOS）」；Android 见 `docs/android-design.md`。
- **所有按钮的文字都不能换行**：按钮标签一律单行（`.lineLimit(1)`，空间紧时 `.minimumScaleFactor` 或缩短文案 / 改成图标），
  按键栏这类横排按钮用图标或短词，放不下就收进菜单 / 浮层，不能挤成两行。

## 开发工作流

人和 Agent 都按这个流程做。每个人和自己的 Agent 用自己的 GitHub 账号，**谁在做以 issue 里的认领评论为准（评论作者即负责人），不用 assign**。
认领的细节、评论模板和可以照抄的 gh 命令见 `docs/collaboration.md`。

1. **先查重**：`gh issue list --state all --search "<关键词>"` / `gh pr list --state all --search "<关键词>"`，搜同一件事的 issue 和 PR
   （连已关闭的、已合并的一起看，别重复做已经做完的）。
2. **已有就直接认领，没有才建**：查到同样意思的 issue（不管谁建的）就在它上面认领（第 3 步；别人认领着的按第 4 步），不另建一个；
   确实没有才 `gh issue create`，写清楚要做什么、为什么；需求加 `enhancement` 标签，fix 加 `bug`。
3. **动手前认领**：在 issue 里评论「认领：在做什么 / 打算：怎么改 / 预计：多久」，Agent 加一行「Agent：是（<机器 id> / <分支或会话>）」。
   机器 id 用配置里给机器起的 id，不写主机名。
4. **已经被别人认领**（最新的认领 / 接手评论不是你，也没有放弃）：
   - 负责人 24 小时内有更新（issue 里的新评论、关联 PR 的推送或评论）就不碰，可以在下面补充信息、复现、线索。
   - 超过 24 小时没有任何更新视为放弃：评论「接手：」写明原因后接着做。自己还在做的，发一条「进展：」续期。
5. **做**：在独立分支 / worktree 上改，分支名带 issue 号（如 `fix/123-xxx`）。做不下去要放弃时，在 issue 里评论「放弃：」说明做到哪一步。
6. **提 PR**：一个 PR 对应一个 issue，标题和正文用英文（会变成 master 上的 commit），正文写 `Closes #<号>`（fix 写 `Fixes #<号>`，只关联不关闭写 `Refs #<号>`），
   合并时 GitHub 自动关闭 issue。描述里没有关联 issue 的 PR 会被 Action（`.github/workflows/pr-needs-issue.yml`）评论并关闭，补上后自动重新打开。
7. **自行合并**：不需要别人 review，**质量自己保证**：按「构建与验证」跑过测试，界面改动看过截图，正常情况下等 CI 通过后
   `gh pr merge <号> --squash --delete-branch`，确认 issue 已经关闭。
   **CI 环境异常时，本地验证通过即可合并**（如 runner、付款 / 额度或服务故障导致 CI 无法正常运行；**CI 一直排不上**——job 长时间排队不开始、runner 积压——也算，2026-11-04 用户要求），不必另等 CI 恢复或逐次询问豁免；
   PR 中写明异常原因、本地验证命令和结果、未运行项，不能把 CI 未运行写成通过。代码或测试本身失败仍须修复，不能按环境异常跳过。

## 构建与验证

- 服务端：`./scripts/test/test-server.sh`（在 hub 机器上连真实 Postgres 跑全部测试）；部署 `./scripts/deploy/deploy.sh [机器 id…]`（默认配置里的全部机器）。
- iOS：`./scripts/app/sim.sh [-openSession <machine>/<session>]` 编译、装到配置里的模拟器、自动配对 hub；截图 `xcrun simctl io <udid> screenshot`。
  直接用 xcodegen / Xcode 前先 `python3 scripts/persona_config.py xcconfig`（生成 bundle id、团队）。
- Android：`./scripts/app/android.sh [install|apk|test|lint]`（`docs/android.md`）。
- 网页：`cd web && npm test && npm run build`；部署 `./scripts/deploy/deploy.sh web`（`docs/web.md`）。
- 发布：`docs/release.md`。**每次发 TestFlight 都内部、外部两个一起发**（2026-10-30 用户要求）：`release.sh` 上传后跑
  `python3 scripts/release/asc_testflight.py wait 45 <构建号>`（进内部组），再跑 `python3 scripts/release/asc_testflight.py external`
  （挂到外部组、提交 Beta 审核、打印公开链接），两个都好了再告诉用户构建号和审核状态。

## 最常踩的坑

完整的在 `docs/lessons/`（动哪一块先读哪个文件）；这里只留跨模块、一踩就费半天的。

- 多个 Agent 同时在这个仓库干活：模拟器、mock hub 端口各用各的，起 mock 前 `lsof` 看端口，停 mock 按端口停（别 `pkill -f mock_hub.py`）；
  主工作区有别人未提交的改动时在 `git worktree add --detach /tmp/x origin/master` 里做；只暂存自己的文件，rebase 冲突只改冲突块，
  push 前 `git diff origin/master...HEAD --stat` 只能有自己的文件。数据库迁移号挑之前先看所有 worktree。（`workflow.md`）
- 等后台任务 / 日志的脚本一律带超时，成功、失败、超时都退出并打印一行结论。
- 手动 `xcodebuild` 前先 `eval "$(python3 scripts/persona_config.py env)"`（`DEVELOPER_DIR`），否则用的是旧 Xcode，报一堆像别人代码坏了的错。（`testing.md`）
- UI 测试一律 `app.launchPinned()` 启动：固定风格（默认液态玻璃，`TEST_RUNNER_JARVIS_UI_STYLE=beautiful` 整套换），布局同 `launchPersona()`（`launchJarvis()` 兼容入口保留）。（`testing.md`）
- DEBUG 启动参数的名字不能和任何 Swift 标识符（类型成员、存储属性）同名，否则 `release.sh` 的 strings 检查拦下 TestFlight；新开关用 `open…` / `show…` 并加进 release.sh 的列表。（`ios.md`）
- Release 的 `SWIFT_REFLECTION_METADATA_LEVEL` 必须是 `all`：App Intents 靠反射里的属性名给 `@Parameter` 传值，去掉名字后快捷指令 / Siri 的参数全是空的、App 崩溃，Debug 和模拟器上看不出来（`lessons/ios.md`「快捷指令运行「问 Persona」」）。
- App 和扩展共享的钥匙串组（灵动岛的形象、推送私钥）缺了就静默退回默认值、模拟器上却总是好的：发版的 ipa 要过 `scripts/release/entitlements.py check`，Info.plist 的组名用 `$(DEVELOPMENT_TEAM).` 不用 `$(AppIdentifierPrefix)`。（`release.md`）
- 实时活动要画的东西放进 ContentState（hub 的推送里也带上），放不下的图放 App Group 容器的文件：实时活动的扩展在手机上读写不了钥匙串，模拟器上却可以（`live-activity.md`「为什么 build 103 的形象包还不对」）。
- 常变的状态别写 UserDefaults：每写一次，App 里每个 `@AppStorage` 视图都重算（2026-11-01，每次推送都写，Agents 列表滚动中跟着重排、内容跳），放 `StateCache`。Mac 上拖窗口时分栏自己每帧写一次，重的视图用 `@StoredValue`（只为自己的键重算，#1337）。（`ios.md`、`lessons/macos.md`）
- App 目标默认 MainActor 隔离：交给系统在别的队列上回调的闭包写成 `{ @Sendable … in`，否则运行时崩；`apple/Shared` 的纯数据类型标 `nonisolated`。（`ios.md`）
- 轮询和上报已读只该在页面在前台时做：新外壳的两页和不摘窗的容器里，藏起来的页不会触发 `onDisappear`，用 `.tabActiveTask`、`.onTabVisibility`；消息订阅、听写、计时不用。（`ios.md`）
- 截图验证用 DEBUG 启动参数 + `simctl io screenshot`，不用 computer-use；界面改动必须真的看到截图（看不到是网关丢图，见 `testing.md`）。
- 共用页面里一直在动的 `TimelineView(.animation)` 在 Mac 上可能每帧整窗重新布局（实测：「需要你」圆点的呼吸圈让 Agents 页空闲占一个核的 14%）：Mac 上常驻动画交给 Core Animation（`MacBreathingRing`）。（`lessons/macos.md`）
- hub 的内存状态随时会被别人重新部署清掉：跨请求的流程要自包含或落库。（`server.md`）
- hub 的端口归 systemd 的 `jarvisd.socket`（socket activation）：换回不认识 `LISTEN_FDS` 的旧二进制前先 `PERSONA_HUB_SOCKET=0 scripts/deploy/hub-systemd.sh`，否则抢不到端口反复重启；`stop jarvisd` 后来一个连接又会被拉起，要停连 `jarvisd.socket` 一起停。（`hub-restart.md`）
- 按来源地址判断信任时：容器里经端口映射进来的连接常常都显示成网桥网关（私网地址，公网来的也是），同机反向代理让公网请求像回环；网关用 `containerGateway` 排除，转发头只用来收紧。（`server.md`「管理界面」）
- Claude Code 的配置目录不一定是 `~/.claude`（`CLAUDE_CONFIG_DIR`），同一会话 id 可能在两个目录各有一份，按 id 找一律取最近修改的。（`agents.md`）
- 改 transcript 解析或同步协议用真实语料验证；改了 item 结构把 App 的 `ChatCache` 目录版本号加一。（`transcript.md`）
- **最低兼容编译器仍是 Xcode 26.0（Swift 6.2）**：保留 iOS、Mac、手表和扩展的兼容目标，新 Xcode 编过不能宣称已验证 26.0。
  只有新 SDK 才有的 API（`isDynamicIslandLimitedInWidth`、`toolbarVerticalEdge`、`ShowsSnippetView` / `view:` 都踩过）用 `#if compiler(>=6.4)` 包起来（Xcode 26.4–26.6 是 Swift 6.3 + iOS 26 SDK，不能用 6.3 判断，#453）、`#else` 走旧行为；
  `#available` / `@available` 只管运行时，符号编译时找不到照样报错。**有可用的 Xcode 26.0 环境时合入前编一遍；没有该环境时不强制实编，不因此阻塞合并或发布**，
  用现有工具链完成构建与测试，PR 中注明实际 Xcode 版本和「26.0 未验证」，不必逐次询问豁免（`DEVELOPER_DIR` 见 `persona_config.py env`）；
  要抬高最低版本仍是产品决定，先问用户，并同步改这一条和 `docs/lessons/ios.md`。（`ios.md`「最低兼容编译器」）
- 手表 App 最低 watchOS 10（不少旧表停在 10.6，2026-11-02 用户的表装不上），`Shared/` 整个编进手表：里面只能用 watchOS 10 就有的 API，
  新的要 `#available` 兜底（`Color.mix` 是 watchOS 11，用 `Color.blended`）。

## 设计系统（iOS）

界面的每个数值、颜色、图标都取自设计系统；页面里不出现字面量。细节和每档的取值见 `docs/ios.md`「设计语言」。

| 要什么 | 用什么（`apple/Shared/Design`，手表 / 小组件也编译） |
| --- | --- |
| 字号、强调、行高 | `Theme.Font` 的级别（display / title / heading / body / secondary / caption / sectionHeader / label / code）和强调 token（`labelStrong`、`secondaryMedium`…），`.lineHeight(_:)` |
| 间距 | `Theme.Space`：4pt 网格 xxs 2 · xs 4 · sm 6 · s 8 · m 12 · l 16 · xl 24 · xxl 32；页面的左右边 `page`（16，搜索框、筛选 chip、分组标题、卡片都对齐它） |
| 圆角 | `Theme.Radius`：badge 6 · control 10 · card 14 · bubble 20 · panel 22 |
| 尺寸 | `Theme.Size` + `.scaledFrame(…)`：图标框、圆点、头像、点按区域随字号缩放（最多 1.6×）；4pt 及以下的线、条用普通 `frame` |
| 动画 | `Theme.Motion`：`standard` / `state`（0.25 s）、`layout`（0.3 s）、`blink`（默认形象眨眼）；对话里展开的内容 `reveal`（0.28 s）/ `conceal`（0.2 s），用 `.unfolding` |
| 颜色 | `Theme` 的角色色（`accent`、`danger`、`positive`、`caution`、`ai`、形象的主色 `persona`…）、`Theme.Fill`、`Theme.Diff`、`Theme.Hue`；基础色 `Palette`；Agent 状态色 `AgentState.tone`（`StatusPalette`） |
| 图标、标志 | `Theme.Icon`（一个意思一个 SF Symbol）、`Theme.Brand`（官方标志，原样用；画出来一律经 `BrandMark` / `BrandTile` / `BrandBadge`，同样大小、同一中心） |
| 风格、主题色 | `StyleSpec.current`：界面风格（`ComponentSpec`）× 主题色（`ColorSpec`）组合成的一个值；`Shared/Design/Styles/` 每种风格一个文件（连同它自己的颜色）；形象的配色是主题色的一个选项「跟随形象」（`docs/persona.md`） |

- **组件和配方**在 `apple/Persona/DesignSystem`：表面 `.surface(…)`、按钮 `.themedButton(…)`、页面 `.themedPage()` / `.themedGroupedPage()`、plain 列表行 `.themedRow()`；
  `Chip`、`CountBadge`、`EmptyStateView`、`Monogram`、`Loadable`、`SecretField`（token、API Key、密钥：iOS 不弹「保存密码？」，不直接写 `SecureField`）。不直接写 `glassEffect` / `.glass`，不在页面里拼一个同样的小组件。
- **缺什么先加**：先在设计系统里加一个有名字的 token 或组件，再用。加风格就是写一个 `ComponentSpec` 加一个 `AppStyle` case，加主题色就是写一个 `ColorSpec` 加一个 `ThemeColor` case，
  别处只读 `StyleSpec.current`、不 `switch AppStyle` / `ThemeColor`。
  换视觉风格只能做成新风格（见上面「不许直接改已有界面的样子」）；BeautifulUI 的组件只在 BeautifulUI 风格下用，库代码不在 Persona 里改。
  做法：视图里按 `AppStyle.current.usesBeautifulUI` 二选一，原来的画法原样留着（`plain`）。能放进设计系统组件的就放在组件里，
  页面不用各自判断：`Chip`、`.card()`、`EmptyStateView`、`LoadableContent` 在 BeautifulUI 风格下画 `BUIPill`、BUI 卡片、`BUIEmptyState`、`LoadingState`。
- **检查**（PersonaTests 扫源码）：`TypeScaleTests`（字号级别、强调、竖向间距在网格上）、`IconVocabularyTests`（图标登记）、
  `TokenRatchetTests`（每个文件写死的 padding、横向间距、圆角、十六进制颜色、动画、固定尺寸只能减少；减少后删掉 `TokenBaseline.json` 重跑记下新基线）、
  `StyleSpecTests`（每种风格 × 每种主题色深浅色的对比度）。不许为了过测试加豁免或调高基线。
- **全局**：滚动条隐藏（RootView 的 `.scrollIndicators(.hidden)`，UIKit 的滚动视图自己关）；字号跟随动态字体和设置里的小 / 标准 / 大（`TextSize`）。
- **安卓**有自己的设计系统（`android/core/designsystem`，三套界面风格，`docs/android-design.md`）：页面只用它的组件和 token，不写死数值、颜色、字号，
  `scripts/app/android_token_baseline.py` 管各模块写死数值的基线（只能减少，`docs/android.md`「设计 token 采用率基线」）；界面改动用
  `./scripts/app/android.sh shots`（专用模拟器 + 假 hub）截图，三套风格 × 深浅色都看。
- **逻辑层**：「加载 → 显示 → 编辑 → 保存」的页面用 MVVM，一个 `@Observable` 的 `XxxModel` 放在页面旁边，依赖从 `Deps` 传入，单元测试用 `FakeHub`；
  事件多的地方（对话页、分页）先把纯计算抽出来（`ChatLines`、`AgentListOrder`）。见 `docs/ios.md`「逻辑层的选型」。

## 目录

| 路径 | 内容 |
| --- | --- |
| `server/` | Go 服务端 `personad`（hub / node / hook / mcp 四种模式），见 `docs/server.md` |
| `apple/` | XcodeGen 工程：Persona 的 iOS / macOS / 手表 App；target / scheme `Persona`、`PersonaMac`（`Mac/`，`docs/mac.md`）、`PersonaWatch`，旧 `Jarvis` 系列 scheme 和工程入口保留，共享代码 `Shared/`、单元测试 |
| `apple/PersonaLink/` | Mac 菜单栏 App「Persona Link」（和 Mac App `PersonaMac` 是两个东西；独立的 SwiftPM 包，不在 `project.yml` 里，`scripts/app/mac-link.sh` 构建）：装着 jarvis-link（`server/cmd/jarvis-link`）和 personad，这台 Mac 的 node 服务（主动连 hub 或让 hub 来连），见 `docs/link.md` |
| `android/` | Kotlin + Compose 多模块：`:app` 装配，`core` 承载协议 / 网络 / 存储 / 列表 / 设计系统，`feature` 承载页面，`platform` 承载通知和手机工具；部分扩展模块尚未接入 App，见 `docs/android.md` |
| `web/` | 网页版（Vite + React + Tailwind，Beautiful UI 的设计语言），hub 自己托管，见 `docs/web.md` |
| `integrations/` | 接入 Agent 运行时（runtime）：`runtime-sdk/` 是通用 SDK（协议客户端、daemon 的监听模式、测试工具、echo 例子），`dsh/` 是 DSH（DeepSeek Harness）的 Web 插件，`pi/` 是 pi / pi-web 的扩展和伴随进程，`paseo/` 是 Paseo 引擎的桥（node 自己运行），见 `docs/runtimes.md`、`docs/daemons.md`、`docs/dsh.md`、`docs/pi.md`、`docs/paseo.md` |
| `scripts/` | 部署（`deploy/deploy.sh`）、发布 TestFlight（`release/release.sh`、`release/asc_*.py`）和公开的 personad（`release/publish.sh`、`deploy/install.sh`）、读配置（`persona_config.py`，旧 `jarvis_config.py` 软链兼容）、截图用的假 hub（`mock/mock_hub.py`） |
| `config.example.json` | 构建 / 部署配置的模板（`docs/config.md`） |
| `docs/` | 设计文档，索引见下 |
| `deploy/docker/` | hub 的 Docker 部署：`compose.yaml`、`.env.example`、镜像的入口脚本（镜像是仓库根的 `Dockerfile`，`.github/workflows/hub-image.yml` 构建推到 ghcr），见 `docs/docker.md` |
| `site/` | 官网 + 隐私政策 + 服务条款（静态，Cloudflare Workers 托管；Google OAuth 同意屏幕要求有隐私政策）。截图只用体验模式（`-demo.on YES`）拍的演示数据，深浅色各一张放 `site/public/img/` |

## 文档索引

### 总体

| 文档 | 内容 |
| --- | --- |
| `docs/collaboration.md` | 多人 / 多 Agent 协作：开工前查重、在 issue 里认领再动手、24 小时没更新视为放弃（续期 / 接手 / 放弃）、PR 必须关联 issue（Action 自动关闭没关联的），评论模板和 gh 命令 |
| `docs/config.md` | 配置：构建 / 部署配置（config.json）、hub 运行配置（hub.json），哪些值不能写进代码 |
| `docs/rename.md` | Jarvis → Persona 兼容迁移：哪些身份不变、新旧名字和目录的对应、防回退检查；验收和发布边界见 `docs/rename-acceptance.md`，open PR 审计快照 `docs/rename-pr-audit.md` |
| `docs/architecture.md` | 整体架构、数据流、为什么这样设计（herdr + hooks + transcript） |
| `docs/protocol.md` | hub HTTP / WebSocket API、hub↔node RPC、事件和交互模型（前后端契约） |
| `docs/network.md` | 公网直连：TLS、公钥钉扎、IPv6 入站、配对、来源判断和限流；iOS / Mac 经 SSH 隧道连 hub（直连不通时的后备） |

### 服务端和部署

| 文档 | 内容 |
| --- | --- |
| `docs/server.md` | personad 各模式、配置、部署、运维 |
| `docs/site.md` | 官网：Worker 托管、国内经阿里云香港入口（nginx 转发到 Worker 的回源域名）、入口密钥和客户端地址、证书、为什么不用 CDN / 函数计算 / 负载均衡、以后转出注册商后改分线路 |
| `docs/docker.md` | hub 的两种部署方式（原生二进制 / Docker）、用 Docker 跑 hub（NAS）：compose、镜像标签、PUID / PGID、环境变量、迁移 |
| `docs/hub-restart.md` | hub 重启对客户端无感：socket activation 零空窗、计划内停机、事件序号和续传（`event_seq`）、warm start、常驻 node 托管助手对话、客户端的静默重连和幂等重放、反复重启的验证脚本 |
| `docs/hub-update.md` | hub 的版本、检测更新和自动更新（方案）：skill 从二进制拿出来、一种二进制、签名的版本清单、自动发布和灰度、下载 → 校验 → 等空闲 → 重启 → 回滚、App 里怎么显示 |
| `docs/push-relay.md` | 推送中继（端到端加密）：没有 APNs key 的 hub 经发布方的 `personad relay` 推送，正文用手机公钥加密、通知服务扩展解密；谁知道什么、防滥用、接口、部署 |
| `docs/plugins.md` | 插件底座：内置 / hub.json / 安装的三种来源、扩展点、清单 `persona-plugin.json`（字段、配置表单的 schema 子集、权限）、安装 / 升级 / 回滚 / 卸载和 `personad plugin`、市场插件的自动升级、按需激活和崩溃降级、配置和密钥怎么到插件、`/api/v1/plugins` 接口、进程插件协议和一致性测试；语音插件和 App 之间的契约；内置的 GPT-Live 语音插件 `personad voice` |
| `docs/ports.md` | 端口转发：node 找出 Agent 在监听的端口、App 认出回答里的 localhost 链接，手机本地监听 + WebSocket 经 hub 转到那台机器，取舍和安全边界 |
| `docs/prompts.md` | 提示词：`server/internal/prompts/` 的目录结构、每个提示词干什么用、模板规矩、怎么改 / 加、golden 测试怎么更新 |
| `docs/machines.md` | 机器监控：OS 识别、CPU / 内存 / 网络 / 磁盘的采集口径（哪些网卡算）、开销实测、24 小时历史的存储 / 降采样 / 清理、App 的「机器」页（Agent 和监控合在一起）和机器详情 |
| `docs/admin.md` | hub 管理界面（`/admin`）：访问范围 `admin.access`（loopback / lan / cidrs / any）和来源判断、管理员密码 / 会话 / CSRF / 限流、改 hub.json、页面和接口、部署时怎么选 |
| `docs/mesh.md` | hub 互联（单中心）：互联码配对、链路、中心和任期（加入组、设为中心、中心不可达）、成员把 App 请求和电脑交给中心、凭据交接、一台电脑多条路的去重和选路、kind service（hub 拨连接器）、ssh 迁移、管理接口和命令行、怎么验证 |
| `docs/link.md` | 连接器 Persona Link（命令 `persona-link`，旧名 jarvis-link 照样认）和 node 服务：一条命令 / 一个 App 装好常驻 node + 连接器；主动模式（电脑连 hub 的链路）和被动模式（hub 连电脑：监听、配对码、接口、限流、安全边界）；Mac 菜单栏 App「Persona Link」；Linux headless 的 `jarvis-link install / status / mode / node / logs / uninstall` |
| `docs/windows.md` | Windows 上的 node（Orca 引擎）：现状和不支持的、`jarvis-link install` 装成登录时启动的计划任务（不弹窗的守护进程、崩溃重启）、hub 在同机 Docker Desktop 时手机怎么连、手动怎么跑、AF_UNIX socket、Orca 的命名管道、PEB 进程扫描和「在前台」、Git Bash、验证记录 |
| `docs/persona-link-install.md` | 给仓库里其他人的指南：在 Mac 上构建、安装 Persona Link，配对、升级、卸载、常见问题 |
| `docs/node-update.md` | 经 Persona Link 更新常驻 node：更新能力、首次本机升级、分块校验、空闲调度、重启与回滚、重新上线确认 |

### Agent、运行时和终端

| 文档 | 内容 |
| --- | --- |
| `docs/claude-integration.md` | 如何在 herdr 里启动 / 驱动 Claude、hook 回答 AskUserQuestion / ExitPlanMode 的实测结论 |
| `docs/codex.md` | Codex 和 TRAEX（内部 Codex 分支）支持：怎么识别、rollout 格式怎么映射成对话、提问和审批卡片（读 rollout 和屏幕、各种界面状态下怎么作答）、改走 app-server 和审批 hook 的调研、还不支持什么 |
| `docs/codex-app-server.md` | Codex app-server 多实例接入的研究：一台机器上多个已有 app-server 和 thread 怎么收进来、桌面 App 照常用、断线重连和授权边界 |
| `docs/runtimes.md` | 接入 Agent 运行时（runtime）的规范：和宿主的区别、多机怎么汇总、driver 要实现什么、内容和交互的约定、命名、SDK、验收清单 |
| `docs/dsh.md` | DSH（DeepSeek Harness）支持：第一个 runtime，DSH Web 插件的安装、事件映射、授权 / 提问怎么到手机、限制、端到端验证 |
| `docs/daemons.md` | daemon：自己是常驻服务、连不上本机 runtime.sock 的 runtime（别的用户、容器、别的机器），node 按 `daemons.json` 去连它的 WebSocket 端点（token + `jarvis-runtime.v1`）：规范、配置、名字和会话 id 不撞车、隔离、SDK 的监听模式、验收 |
| `docs/pi.md` | pi 和 pi-web 支持：herdr 里的 pi 是 herdr 会话（窗格归 herdr、内容和控制归扩展），pi-web 的走 pi-web；扩展 + 伴随进程，离线后去哪接着，安装、配置、映射、端到端验证 |
| `docs/agent-states.md` | 统一的 Agent 状态（需要你 / 工作中 / 完成未看 / 已看）和 herdr 的对应，空间 / 槽位模型 |
| `docs/engines.md` | 引擎（herdr / Herdr App / Orca / Paseo）：一机一引擎，怎么切换、切换后会怎样、各引擎能做什么、选不了时查什么 |
| `docs/paseo.md` | Paseo 引擎：node 运行的桥（runtime）、怎么找到 Paseo、会话 id 和对话记录、卡片 / 新建 / 接手的映射、限制、端到端验证、排错 |
| `docs/terminal.md` | 终端 tab 的真终端方案：herdr observe 流（不 attach、不改窗格尺寸）、续传、换行、iOS 端自写 VT 子集的选型理由 |
| `docs/launchers.md` | 启动器与「设置 › 模型供应商」：~/.zshrc 自动同步 / 隐藏、Persona 管理的接入（Key 加密存 hub）、直接 API 与启动器、接口 |
| `docs/model-config.md` | 统一模型配置：按用途选择供应商、具体模型和选项，继承、持久化、下一次运行生效、任务与插件边界 |

### 助手、数据源和知识库

| 文档 | 内容 |
| --- | --- |
| `docs/assistant.md` | 「Persona」tab 的助手（主对话、知识库工具）：ask_user、定时任务 / 跟进、Agents 与飞书工具、实时快照、预热、记忆、后台唤醒手机 |
| `docs/unified-entry.md` | 主对话统一入口（#930）：在助手里派发、盯住、回答所有任务；三层结构（对话流 / 任务坞 / 需要你）、计划卡和三档自动开始、任务 = 待办、对话动态、钩子、分步；原型 `docs/assets/unified-entry-prototype.html`。Agents 页不动 |
| `docs/todos.md` | 待办：记下要做的事并分工给多个 Agent，在待办中看各 Agent 进展，Agents 页按待办分组；整组挂、派生跟随、herdr 标记、状态派生与接口；做完的任务关掉 Agent、下一条消息恢复会话 |
| `docs/knowledge.md` | 知识库 `~/.persona/data`：会话全量归档、压缩上下文、人 / 群 / 项目、打标，Persona / AI 分身 / Agents 怎么共用 |
| `docs/connectors.md` | 连接器：现在的结构（接口、三种跑法、闸门、收件箱、知识库、设置页、怎么加一个），v2 设计（能力清单、统一的增量、有版本号的协议、收件箱的结构化属性、身份线索、网页授权登录、写操作只走工具和闸门）、迁移顺序 V1–V7（V2、V7 不做）和拍板结果 |
| `docs/connector-protocol.md` | 连接器协议 v2：子进程 / 第三方连接器和 hub 之间的一行一条 JSON：握手和清单、每个方法的参数和结果、程序调 hub 的 `secrets.*` 和 `changed`、错误码、硬性规定、版本规则、一致性测试 |
| `docs/inbox.md` | 收件箱：飞书和邮件合成一个列表，一行一件事，AI 分拣、已处理、交给 AI；AI 分身（分拣 → 判断 → Agent 处理并草拟回复，点一下发送） |
| `docs/mail.md` | 邮箱：IMAP/SMTP、Gmail（使用 Google 账号登录，OAuth2 + XOAUTH2）、飞书邮箱（固定 larkoffice.com 服务器）、凭据存储 |
| `docs/lark-connector-migration.md` | 飞书迁移的授权范围：可选消息页与现有收件箱共用能力 |
| `docs/feishu.md` | 飞书在 Persona 里的定位（不做第二个飞书）：保留哪些、经插件的飞书（`/api/v1/lark/…` 由插件提供）、给 AI 用的接口速查 |
| `docs/lark-attachments.md` | iOS 飞书聊天发图片和文件（#410）：选择、预览、发送和确认 |
| `docs/channels.md` | 渠道（channel）：所有和 Persona 对话的入口（App、微信、飞书机器人、飞书 /agent、语音）的盘点和统一抽象：渠道的接口和能力、hub 的统一管线（进对话、跟踪这一轮、答案发回、文字 / 卡片提问、去重、来源标记）、插件的 `channel` 扩展点和协议、兼容迁移、`/api/v1/channels`、设置 › 插件 › 渠道 |
| `docs/bots.md` | 飞书机器人：私聊机器人就是和 Persona 主对话说话，回答发回飞书，提问 / 批准在飞书里点卡片，推送和主对话同步，在飞书里驱动各台电脑上的 Agent；开放平台长连接、配对、只认主人、卡片 JSON 2.0 的坑 |
| `docs/wechat.md` | 微信机器人（内置插件 `jarvisd-wechat`，腾讯 iLink）：扫码绑定，私聊机器人就是和主对话说话，回答发回微信，提问 / 批准用文字往返；插件只管和微信说话（`channel` 扩展点），其余走 hub 的渠道管线；和飞书机器人比 |
| `docs/wechat-connector.md` | 微信（wechat-connector）连接器接入手册（#1420）：把 wechat-connector 当外部 API 服务接入，个人微信作为数据来源进收件箱与知识库；前置条件（服务地址、应用密钥范围、允许来源、事件推送）、插件包安装（`file:` / 管理界面上传）、登录、设置项与隐私默认、发送的三道锁、闸门示例、状态含义、已知限制、故障排查 |
| `docs/lark-agent.md` | 飞书里自己发 `/agent …`（由接飞书的插件做，hub 提供开对话、知识库上下文的接口）：收集所在会话 / 话题 / 引用 / 文档 / 知识库的上下文，起 Agent 处理，以我的身份回复 |
| `docs/phone-tools.md` | 手机数据工具（phone_*）、权限、需要确认的写操作、iOS 限制 |
| `docs/computer-use.md` | 操控电脑：内置插件 `cua`（默认关、按机器开），助手经 hub → node → Cua Driver 看窗口、点按、截图（`computer_state` / `computer_run`），能力发现、会话、截图、写操作的日志和确认；一键安装和实时画面（屏幕共享经 hub 隧道）的方案 |
| `docs/office.md` | 生成 Word / PowerPoint / Excel（`personad office`，M9）：Markdown 生成 docx / pptx，JSON / CSV 生成 xlsx（写时算好公式的值），check 读回结构；为什么做进 personad、支持什么、QuickLook 的限制 |
| `docs/recording.md` | 手机和 Apple Watch 录音作为 Persona 上下文：分段、本机转写、可靠回传、确认发送与验证边界 |

### 客户端

| 文档 | 内容 |
| --- | --- |
| `docs/ios.md` | iOS / watchOS App：信息架构、设计语言、手机数据工具 |
| `docs/ios-state.md` | App 的状态和重绘：推送怎么走到屏幕、一次推送的代价、根因、分步根治；真实性 / 及时性的标准和探针（`ScreenProbe`） |
| `docs/intents.md` | Siri / 快捷指令（App Intents）：问 Claude、状态、批准第一个待处理 |
| `docs/siri.md` | Siri 深度集成：问 Persona（助手主对话）、让个人 Agent 去做、未读、控制中心 / 操作按钮、Spotlight 索引 |
| `docs/live-activity.md` | 锁屏 / 灵动岛实时活动（Fleet）：显示什么、和其他 App 共用灵动岛（限宽 / 最小态）、ContentState、hub 的 liveactivity 推送和 push-to-start、怎么在模拟器上验证 |
| `docs/shell.md` | 新外壳的状态模型：哪一页在前的唯一来源 `ShellState`、切换段 / 横条 / 路由怎么同步、不在前的页、一致和及时的验收标准与测试 |
| `docs/persona.md` | 形象：名字、头像、顶栏的动态状态圆点、配色怎么做成一个包（hub 的 `~/.persona/personas`），色系怎么接到设计 token，接入新形象的脚本（Codex 生图） |
| `docs/multi-hub.md` | 一台设备连多组 hub：配对注册表和迁移、按 mesh 分组、每组一条连接和选路、按组的键（默认组不变）、合并显示和按组筛选、推送 / 实时活动 / 手表 / Siri 按组路由 |
| `docs/mac.md` | macOS App：和 iOS 共用逻辑层和内容视图、Mac 壳（`apple/Mac/`）、iOS 专用 API 的隔离和兼容层、功能对齐清单 |
| `docs/android.md` | Android App：模块职责与依赖、入口接线、常驻连接通知、多 hub（分组、组后缀的会话键、组内换线路）、构建 / 测试 / 假 hub 截图命令 |
| `docs/android-design.md` | Android 三套界面风格（玻璃 / 晨报 / 看板）、设计 token、组件清单和写页面的规矩、玻璃的降级 |
| `docs/web.md` | 网页版：hub 托管、登录换设备 token、各页面（收件箱、终端 / 文件 / Git 的侧边面板、链接、互联的 hub 标注）、连接与补洞、细节约定、和手机端的差距表 |

### 测试和发布

| 文档 | 内容 |
| --- | --- |
| `docs/testing.md` | 测试命令、模拟器截图检查清单 |
| `docs/runtime-cli.md` | DEBUG 构建里的命令通道（`scripts/app/personactl.py`，旧 `jarvisctl.py` 兼容）：运行中读状态、等画面停下、截图，协议和安全约束 |
| `docs/release.md` | 签名、TestFlight 发布流程 |

### 计划和经验

| 文档 | 内容 |
| --- | --- |
| `docs/plan-orca-support.md` | 计划：支持 Orca（进行中）：先把宿主（herdr、Orca）和 Agent 分开、抽出宿主接口，再接 Orca 来源；每一步的范围、Orca 能提供什么、风险 |
| `docs/plan-connectors.md` | 计划：连接器（先接 GitHub 和 iMessage）：能力可选的 Connector 接口、内置 / 外部进程 / node 本地三种形态、飞书邮件的迁移步骤、隐私和拍板项 |
| `docs/plan-hub-mesh.md` | 计划：hub 互联（探测、双向链路、组内电脑去重和选路、归属）、links、多 hub 客户端、hub 管理界面（访问范围）、网页对齐；各路之间的接口约定和分工 |
| `docs/plan-hub-center.md` | 计划第二版：有且只有一台中心 hub（其余 hub 当成员、把电脑交给中心）、电脑经预装服务接入（连接器主动 / 被动两种模式，不用 SSH）、一台电脑多条路去重选路；两路分工和被动模式协议 |
| `docs/redesign/` | 客户端重设计与 Android 首版的方案（2026-10-30 草案）：功能对齐矩阵、交互规格、流程图、hub 改动；外观部分已被 `docs/android-design.md` 取代，调研笔记在 `research/` |
| `docs/plan-telegram.md` | 计划：Telegram 连接器（用户自己的账号，第一版只读）：gotd/td 选型、分步登录和加密存会话、收哪些会话和增量、收件箱、只读工具、设置的开关 / 删除 / 退出，要用户定的 |
| `docs/plan-lark-cli.md` | 计划：没有 lark-connector 时用官方 lark-cli 接飞书：能力对照和降级、两种来源并存、做成连接器、闸门认 lark-cli、收件箱收什么、分步计划和已拍板项 |
| `docs/plan-ego-browser.md` | 计划：ego lite 浏览器插件（#1304）：照 Cua 插件的模式接入（默认关、按 Mac 开、安装校验、首次设置引导），助手的 `browser_*` 工具和 Agent 用自带 skill 的分工，看浏览器（实时画面 / 定时截图 / CDP 投屏），导入 cookies 的来源、存储与安全、写入方式，要拍板的 |
| `docs/plan-plugins.md` | 计划：插件系统（#1101）：关键取舍（只在 hub 机器上装、权限不是沙箱、表单子集）、实现和方案的出入、分步进度（1a 服务端底座已做，1b App 设置页、第 2–4 步待做）、语音和连接器 v2 怎么迁上来 |
| `docs/lessons/` | 经验（踩坑）按主题分文件：协作、Agent、助手、服务端、transcript、飞书邮件、终端、iOS、Android、macOS、测试，索引 `docs/lessons/README.md` |
| `docs/archive/` | 已结束的车道方案、真实数据验证、阶段状态表和验收记录（实时更新打断操作、消息链路、会话读取验证、Android P0），只作证据引用，不随代码更新；新的一次性记录做完放这里 |
