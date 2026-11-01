# CLAUDE.md

Jarvis：给自己用的 iPhone + Apple Watch 助手。「Jarvis」tab 是能访问个人和工作数据的 AI 助手（hub 机器上一个专用目录里的 Claude，有一个常驻的主对话），
知识库 `~/.jarvis/data` 把各处的聊天、人、群、项目沉淀下来给它和 AI 分身用，
「Agents」tab 是在手机上用聊天的方式驱动各台电脑上 herdr 里的 Claude Code / Codex（实时流式看过程、
在手机上回答提问 / 审批计划 / 批准工具），并让 Claude 读写 iPhone 本地数据（通讯录、日历、提醒、照片、健康、位置……）和邮箱
（Gmail / 任意 IMAP+SMTP）。服务端 `jarvisd`（Go）的 hub 跑在一台常开的机器上，经 SSH 接入其他电脑。通过 TestFlight 分发。

**这台开发机、这个人的具体情况（机器、地址、账号、个人工作方式）在 `CLAUDE.local.md`**（不提交）；没有它就先看 `docs/config.md` 配好。

## 约定（必须遵守）

- **配置化，不写死个人的东西**：机器名、域名、账号、邮箱、路径、启动器、公司内部工具、个人偏好一律走配置
  （构建 / 部署：`~/.config/jarvis/config.json`；运行：hub 的 `~/.jarvis/hub.json`；见 `docs/config.md`），代码、脚本、
  界面文案、测试、文档、本文件里都不出现具体某个人的值（内网域名 / IP、主机名、内部项目名、个人账号、各种 key 的 ID 也算）；界面说「hub 所在的机器」「你的各台电脑」。本机的具体情况写 `CLAUDE.local.md`。
- **随时沉淀经验**：踩坑、怪异行为当场写进 `docs/lessons/<主题>.md`（索引 `docs/lessons/README.md`），设计取舍写进 `docs/<主题>.md`；
  跨模块、最常踩的再在本文件「最常踩的坑」留一行。本文件只放约定和最核心的东西，别再往里堆细节。这条规则本身也要保留。
- 代码注释、提交信息用英文；文档用中文；App 界面文案用中文。
- **不提交任何内网信息或个人信息**（2026-10-28 用户要求；`scripts/privacy_check.py` 自动查，见 `docs/testing.md`「隐私扫描」）：代码、测试数据、文档、截图、提交信息里都不能有内网域名 / IP / URL、
  主机名、内部项目 / 仓库名、真实的公司或个人数据（消息、文档链接、人名），以及个人账号、邮箱、各种 key / 项目的 ID。示例一律用占位符
  （`<hub 地址>`、`me@example.com`）；具体值只写配置或 `CLAUDE.local.md`。Jarvis 支持的产品 / 工具名（飞书、TRAEX、removed 等）不算。
- **密钥绝不入库**：hub token、数据库密码只存在 hub 机器的 `~/.jarvis/hub.json`（0600）；邮箱凭据 AES-GCM 加密后存 Postgres，密钥文件不进数据库。
- 服务端状态全部在 hub 机器的 Postgres 容器 `jarvis-postgres`（`docs/server.md`），不要再往 `~/.jarvis/` 写 JSON 状态文件。
  例外是知识库 `~/.jarvis/data`（2026-10-30 用户要求：所有数据最终沉淀在 `.jarvis`，文件为主、库做索引，`docs/knowledge.md`）。
- **所有按钮的文字都不能换行**：按钮标签一律单行（`.lineLimit(1)`，空间紧时 `.minimumScaleFactor` 或缩短文案 / 改成图标），
  按键栏这类横排按钮用图标或短词，放不下就收进菜单 / 浮层，不能挤成两行。
- **链接在 App 内打开**（2026-10-26 用户要求）：会话里出现的任何链接（飞书文档、removed、网页……）都在当前 App 里打开，参考 Telegram：
  底部浮起的卡片，打开即铺满屏幕（2026-10-28 用户要求，原来半屏太小），下拉到 0.6 时不挡 Agent 的操作（对话照常滚动、输入），再下拉关闭。打开速度和顺滑优先，不在乎本地空间：预加载、本地缓存、
  复用 WebView 等手段都要用上。页面里选中文字可以「AI 修改」（选中内容 + 修改提示发给 Agent）或「作为上下文」（作为下一条消息的关联上下文），
  上下文用 XML 标签带在消息里，客户端解析这些标签做相应的 UI。
- **不做第二个飞书**（2026-10-27 用户纠正）：App 是把各处信息收到一起交给 AI，不是把飞书的功能页面搬过来。飞书只保留
  消息（进收件箱）、链接在 App 内渲染（文档、removed单帖），其余能力只做在 hub / AI 侧（搜索、读取作为上下文），见 `docs/feishu.md`。
  别的数据源同理：不要为某个来源做一套浏览页面。
- **不许直接改已有界面的样子**（2026-10-30 用户要求：BeautifulUI 的工作过程和输入框直接替换了原来的，太丑，已整体 revert）：
  换一种视觉风格（组件库、设计语言）一律做成新的**界面风格**（设置 › 外观 › 界面风格，`AppStyle`），换一套配色做成新的**主题色**（`ThemeColor`），
  只在选了它时生效，默认和其他选项的界面一像素都不变。修 bug、按用户明确要求改某个界面的不算。
- **界面只用设计系统**（2026-10-31 用户要求：设计系统是仓库的约束）：字号、间距、圆角、尺寸、动画、颜色、图标、品牌标志、风格一律取自设计系统，
  页面里不写死数值、系统字号、SF Symbol 名字、十六进制颜色；缺什么先加 token。见下面「设计系统（iOS）」。
- **提示词一律放 `server/internal/prompts/`**（2026-11-01 用户要求，含 MCP 工具的描述和参数说明，在 `mcp/`）：一个提示词一个文件（按场景分子目录），同一个 Agent 的几段用模板组合，不在 Go 代码里 fmt / + 提示词；调用方只准备数据。改了提示词要更新 golden 测试并看 diff，见 `docs/prompts.md`。
- 质量标准：按 App Store 榜首水准设计和测试。每个界面都要在模拟器上截图检查（深色 / 浅色），手表界面也要截图，不能只看编译通过。

## 设计系统（iOS）

界面的每个数值、颜色、图标都取自设计系统；页面里不出现字面量。细节和每档的取值见 `docs/ios.md`「设计语言」。

| 要什么 | 用什么（`apple/Shared/Design`，手表 / 小组件也编译） |
| --- | --- |
| 字号、强调、行高 | `Theme.Font` 的级别（display / title / heading / body / secondary / caption / sectionHeader / label / code）和强调 token（`labelStrong`、`secondaryMedium`…），`.lineHeight(_:)` |
| 间距 | `Theme.Space`：4pt 网格 xxs 2 · xs 4 · sm 6 · s 8 · m 12 · l 16 · xl 24 · xxl 32 |
| 圆角 | `Theme.Radius`：badge 6 · control 10 · card 14 · bubble 20 · panel 22 |
| 尺寸 | `Theme.Size` + `.scaledFrame(…)`：图标框、圆点、头像、点按区域随字号缩放（最多 1.6×）；4pt 及以下的线、条用普通 `frame` |
| 动画 | `Theme.Motion`：`standard` / `state`（0.25 s）、`layout`（0.3 s）、`blink`（默认形象眨眼）；对话里展开的内容 `reveal`（0.28 s）/ `conceal`（0.2 s），用 `.unfolding` |
| 颜色 | `Theme` 的角色色（`accent`、`danger`、`positive`、`caution`、`ai`、形象的主色 `persona`…）、`Theme.Fill`、`Theme.Diff`、`Theme.Hue`；基础色 `Palette`；Agent 状态色 `AgentState.tone`（`StatusPalette`） |
| 图标、标志 | `Theme.Icon`（一个意思一个 SF Symbol）、`Theme.Brand`（官方标志，原样用） |
| 风格、主题色 | `StyleSpec.current`：界面风格（`ComponentSpec`）× 主题色（`ColorSpec`）组合成的一个值；`Shared/Design/Styles/` 每种风格一个文件（连同它自己的颜色）；形象的配色是主题色的一个选项「跟随形象」（`docs/persona.md`） |

- **组件和配方**在 `apple/Jarvis/DesignSystem`：表面 `.surface(…)`、按钮 `.themedButton(…)`、页面 `.themedPage()` / `.themedGroupedPage()`、plain 列表行 `.themedRow()`；
  `Chip`、`CountBadge`、`EmptyStateView`、`Monogram`、`Loadable`。不直接写 `glassEffect` / `.glass`，不在页面里拼一个同样的小组件。
- **缺什么先加**：先在设计系统里加一个有名字的 token 或组件，再用。加风格就是写一个 `ComponentSpec` 加一个 `AppStyle` case，加主题色就是写一个 `ColorSpec` 加一个 `ThemeColor` case，
  别处只读 `StyleSpec.current`、不 `switch AppStyle` / `ThemeColor`。
  换视觉风格只能做成新风格（见上面「不许直接改已有界面的样子」）；BeautifulUI 的组件只在 BeautifulUI 风格下用，库代码不在 Jarvis 里改。
- **检查**（JarvisTests 扫源码）：`TypeScaleTests`（字号级别、强调、竖向间距在网格上）、`IconVocabularyTests`（图标登记）、
  `TokenRatchetTests`（每个文件写死的 padding、横向间距、圆角、十六进制颜色、动画、固定尺寸只能减少；减少后删掉 `TokenBaseline.json` 重跑记下新基线）、
  `StyleSpecTests`（每种风格 × 每种主题色深浅色的对比度）。不许为了过测试加豁免或调高基线。
- **全局**：滚动条隐藏（RootView 的 `.scrollIndicators(.hidden)`，UIKit 的滚动视图自己关）；字号跟随动态字体和设置里的小 / 标准 / 大（`TextSize`）。
- **逻辑层**：「加载 → 显示 → 编辑 → 保存」的页面用 MVVM，一个 `@Observable` 的 `XxxModel` 放在页面旁边，依赖从 `Deps` 传入，单元测试用 `FakeHub`；
  事件多的地方（对话页、分页）先把纯计算抽出来（`ChatLines`、`AgentListOrder`）。见 `docs/ios.md`「逻辑层的选型」。

## 目录

| 路径 | 内容 |
| --- | --- |
| `server/` | Go 服务端 `jarvisd`（hub / node / hook / mcp 四种模式），见 `docs/server.md` |
| `apple/` | XcodeGen 工程：iOS App `Jarvis`、macOS App `JarvisMac`（`Mac/`，`docs/mac.md`）、手表 App `JarvisWatch`、共享代码 `Shared/`、单元测试 |
| `android/` | Android App（Kotlin + Compose），见 `docs/android.md` |
| `web/` | 网页版（Vite + React + Tailwind，Beautiful UI 的设计语言），hub 自己托管，见 `docs/web.md` |
| `scripts/` | 部署（`deploy.sh`）、发布 TestFlight（`release.sh`、`asc_*.py`）和公开的 jarvisd（`publish.sh`、`install.sh`）、读配置（`jarvis_config.py`）、截图用的假 hub（`mock_hub.py`） |
| `config.example.json` | 构建 / 部署配置的模板（`docs/config.md`） |
| `docs/` | 设计文档，索引见下 |
| `site/` | 官网 + 隐私政策 + 服务条款（静态，Cloudflare Workers 托管；Google OAuth 同意屏幕要求有隐私政策）。截图只用体验模式（`-demo.on YES`）拍的演示数据，深浅色各一张放 `site/public/img/` |

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
| `docs/assistant.md` | 「Jarvis」tab 的助手（主对话、知识库工具）：ask_user、定时任务 / 跟进、Agents 与飞书工具、实时快照、预热、记忆、后台唤醒手机 |
| `docs/inbox.md` | 收件箱：飞书和邮件合成一个列表，一行一件事，AI 分拣、已处理、交给 AI；AI 分身（分拣 → 判断 → Agent 处理并草拟回复，点一下发送） |
| `docs/machines.md` | 机器监控：OS 识别、CPU / 内存 / 网络 / 磁盘的采集口径（哪些网卡算）、开销实测、24 小时历史的存储 / 降采样 / 清理、Dashboard 和机器详情 |
| `docs/live-activity.md` | 锁屏 / 灵动岛实时活动（Fleet）：显示什么、ContentState、hub 的 liveactivity 推送和 push-to-start、怎么在模拟器上验证 |
| `docs/terminal.md` | 终端 tab 的真终端方案：herdr observe 流（不 attach、不改窗格尺寸）、续传、换行、iOS 端自写 VT 子集的选型理由 |
| `docs/plugins.md` | hub 插件：hub 跑 / 转发外部进程（实现放别的仓库），`/api/v1/plugins`，语音插件和 App 之间的契约 |
| `docs/android.md` | Android App：已有的页面、不用推送服务的后台通知、构建安装、还没做的部分 |
| `docs/feishu.md` | 飞书在 Jarvis 里的定位（不做第二个飞书）：保留哪些、lark-connector 只读代理、给 AI 用的接口速查 |
| `docs/web.md` | 网页版：hub 托管、登录换设备 token、各页面、连接与补洞、细节约定 |
| `docs/launchers.md` | 启动器与「设置 › 模型」：~/.zshrc 的自动同步 / 隐藏，App 里加的（Key 加密存 hub、`jarvisd launch` 运行），接口 |
| `docs/lark-agent.md` | 飞书里自己发 `/agent …`：hub 收集所在会话 / 话题 / 引用 / 文档 / 知识库的上下文，起 Agent 处理，以我的身份回复 |
| `docs/knowledge.md` | 知识库 `~/.jarvis/data`：会话全量归档、压缩上下文、人 / 群 / 项目、打标，Jarvis / AI 分身 / Agents 怎么共用 |
| `docs/mac.md` | macOS App：和 iOS 共用逻辑层和内容视图、Mac 壳（`apple/Mac/`）、iOS 专用 API 的隔离和兼容层、功能对齐清单 |
| `docs/prompts.md` | 提示词：`server/internal/prompts/` 的目录结构、每个提示词干什么用、模板规矩、怎么改 / 加、golden 测试怎么更新 |
| `docs/persona.md` | 形象：名字、头像、顶栏的动态状态圆点、配色怎么做成一个包（hub 的 `~/.jarvis/personas`），色系怎么接到设计 token，接入新形象的脚本（Codex 生图） |
| `docs/plan-orca-support.md` | 计划：支持 Orca（进行中）：先把宿主（herdr、Orca）和 Agent 分开、抽出宿主接口，再接 Orca 来源；每一步的范围、Orca 能提供什么、风险 |
| `docs/lessons/` | 经验（踩坑）按主题分文件：协作、Agent、助手、服务端、transcript、飞书邮件、终端、iOS、macOS、测试，索引 `docs/lessons/README.md` |

## 构建与验证

- 服务端：`./scripts/test-server.sh`（在 hub 机器上连真实 Postgres 跑全部测试）；部署 `./scripts/deploy.sh [机器 id…]`（默认配置里的全部机器）。
- iOS：`./scripts/sim.sh [-openSession <machine>/<session>]` 编译、装到配置里的模拟器、自动配对 hub；截图 `xcrun simctl io <udid> screenshot`。
  直接用 xcodegen / Xcode 前先 `python3 scripts/jarvis_config.py xcconfig`（生成 bundle id、团队）。
- Android：`./scripts/android.sh [install|apk|test]`（`docs/android.md`）。
- 网页：`cd web && npm test && npm run build`；部署 `./scripts/deploy.sh web`（`docs/web.md`）。
- 发布：`docs/release.md`。**每次发 TestFlight 都内部、外部两个一起发**（2026-10-30 用户要求）：`release.sh` 上传后跑
  `python3 scripts/asc_testflight.py wait 45 <构建号>`（进内部组），再跑 `python3 scripts/asc_testflight.py external`
  （挂到外部组、提交 Beta 审核、打印公开链接），两个都好了再告诉用户构建号和审核状态。

## 最常踩的坑

完整的在 `docs/lessons/`（动哪一块先读哪个文件）；这里只留跨模块、一踩就费半天的。

- 多个 Agent 同时在这个仓库干活：模拟器、mock hub 端口各用各的，起 mock 前 `lsof` 看端口，停 mock 按端口停（别 `pkill -f mock_hub.py`）；
  主工作区有别人未提交的改动时在 `git worktree add --detach /tmp/x origin/master` 里做；只暂存自己的文件，rebase 冲突只改冲突块，
  push 前 `git diff origin/master...HEAD --stat` 只能有自己的文件。数据库迁移号挑之前先看所有 worktree。（`workflow.md`）
- 等后台任务 / 日志的脚本一律带超时，成功、失败、超时都退出并打印一行结论。
- 手动 `xcodebuild` 前先 `eval "$(python3 scripts/jarvis_config.py env)"`（`DEVELOPER_DIR`），否则用的是旧 Xcode，报一堆像别人代码坏了的错。（`testing.md`）
- DEBUG 启动参数的名字不能和任何 Swift 标识符（类型成员、存储属性）同名，否则 `release.sh` 的 strings 检查拦下 TestFlight；新开关用 `open…` / `show…` 并加进 release.sh 的列表。（`ios.md`）
- App 目标默认 MainActor 隔离：交给系统在别的队列上回调的闭包写成 `{ @Sendable … in`，否则运行时崩；`apple/Shared` 的纯数据类型标 `nonisolated`。（`ios.md`）
- 截图验证用 DEBUG 启动参数 + `simctl io screenshot`，不用 computer-use；界面改动必须真的看到截图（看不到是网关丢图，见 `testing.md`）。
- hub 的内存状态随时会被别人重新部署清掉：跨请求的流程要自包含或落库。（`server.md`）
- Claude Code 的配置目录不一定是 `~/.claude`（`CLAUDE_CONFIG_DIR`），同一会话 id 可能在两个目录各有一份，按 id 找一律取最近修改的。（`agents.md`）
- 改 transcript 解析或同步协议用真实语料验证；改了 item 结构把 App 的 `ChatCache` 目录版本号加一。（`transcript.md`）
