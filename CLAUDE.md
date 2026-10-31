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
- **随时沉淀经验**：踩坑、怪异行为、设计取舍当场写进本文件或 `docs/<主题>.md`。这条规则本身也要保留。
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
  换一种视觉风格（组件库、设计语言、整套配色）一律做成新的**主题**（设置 › 界面风格，`AppStyle`），只在选了它时生效，
  默认和其他主题的界面一像素都不变。修 bug、按用户明确要求改某个界面的不算。
- **界面只用设计系统**（2026-10-31 用户要求：设计系统是仓库的约束）：字号、间距、圆角、尺寸、动画、颜色、图标、品牌标志、风格一律取自设计系统，
  页面里不写死数值、系统字号、SF Symbol 名字、十六进制颜色；缺什么先加 token。见下面「设计系统（iOS）」。
- 质量标准：按 App Store 榜首水准设计和测试。每个界面都要在模拟器上截图检查（深色 / 浅色），手表界面也要截图，不能只看编译通过。

## 设计系统（iOS）

界面的每个数值、颜色、图标都取自设计系统；页面里不出现字面量。细节和每档的取值见 `docs/ios.md`「设计语言」。

| 要什么 | 用什么（`ios/Shared/Design`，手表 / 小组件也编译） |
| --- | --- |
| 字号、强调、行高 | `Theme.Font` 的级别（display / title / heading / body / secondary / caption / sectionHeader / label / code）和强调 token（`labelStrong`、`secondaryMedium`…），`.lineHeight(_:)` |
| 间距 | `Theme.Space`：4pt 网格 xxs 2 · xs 4 · sm 6 · s 8 · m 12 · l 16 · xl 24 · xxl 32 |
| 圆角 | `Theme.Radius`：badge 6 · control 10 · card 14 · bubble 20 · panel 22 |
| 尺寸 | `Theme.Size` + `.scaledFrame(…)`：图标框、圆点、头像、点按区域随字号缩放（最多 1.6×）；4pt 及以下的线、条用普通 `frame` |
| 动画 | `Theme.Motion`：`standard` / `state`（0.25 s）、`layout`（0.3 s）、`blink`（默认形象眨眼）；对话里展开的内容 `reveal`（0.28 s）/ `conceal`（0.2 s），用 `.unfolding` |
| 颜色 | `Theme` 的角色色（`accent`、`danger`、`positive`、`caution`、`ai`、形象的主色 `persona`…）、`Theme.Fill`、`Theme.Diff`、`Theme.Hue`；基础色 `Palette`；Agent 状态色 `AgentState.tone`（`StatusPalette`） |
| 图标、标志 | `Theme.Icon`（一个意思一个 SF Symbol）、`Theme.Brand`（官方标志，原样用） |
| 风格 | `StyleSpec.current`；`Shared/Design/Styles/` 每种风格一个文件；形象的配色叠在上面（`StyleSpec.tinted`，`docs/persona.md`） |

- **组件和配方**在 `ios/Jarvis/DesignSystem`：表面 `.surface(…)`、按钮 `.themedButton(…)`、页面 `.themedPage()` / `.themedGroupedPage()`、plain 列表行 `.themedRow()`；
  `Chip`、`CountBadge`、`EmptyStateView`、`Monogram`、`Loadable`。不直接写 `glassEffect` / `.glass`，不在页面里拼一个同样的小组件。
- **缺什么先加**：先在设计系统里加一个有名字的 token 或组件，再用。加风格就是写一个 `StyleSpec` 加一个 `AppStyle` case，别处只读 `StyleSpec.current`、不 `switch AppStyle`。
  换视觉风格只能做成新风格（见上面「不许直接改已有界面的样子」）；BeautifulUI 的组件只在 BeautifulUI 风格下用，库代码不在 Jarvis 里改。
- **检查**（JarvisTests 扫源码）：`TypeScaleTests`（字号级别、强调、竖向间距在网格上）、`IconVocabularyTests`（图标登记）、
  `TokenRatchetTests`（每个文件写死的 padding、横向间距、圆角、十六进制颜色、动画、固定尺寸只能减少；减少后删掉 `TokenBaseline.json` 重跑记下新基线）、
  `StyleSpecTests`（各风格深浅色的对比度）。不许为了过测试加豁免或调高基线。
- **全局**：滚动条隐藏（RootView 的 `.scrollIndicators(.hidden)`，UIKit 的滚动视图自己关）；字号跟随动态字体和设置里的小 / 标准 / 大（`TextSize`）。
- **逻辑层**：「加载 → 显示 → 编辑 → 保存」的页面用 MVVM，一个 `@Observable` 的 `XxxModel` 放在页面旁边，依赖从 `Deps` 传入，单元测试用 `FakeHub`；
  事件多的地方（对话页、分页）先把纯计算抽出来（`ChatLines`、`AgentListOrder`）。见 `docs/ios.md`「逻辑层的选型」。

## 目录

| 路径 | 内容 |
| --- | --- |
| `server/` | Go 服务端 `jarvisd`（hub / node / hook / mcp 四种模式），见 `docs/server.md` |
| `ios/` | XcodeGen 工程：iOS App `Jarvis`、手表 App `JarvisWatch`、共享代码 `Shared/`、单元测试 |
| `android/` | Android App（Kotlin + Compose），见 `docs/android.md` |
| `web/` | 网页版（Vite + React + Tailwind，Beautiful UI 的设计语言），hub 自己托管，见 `docs/web.md` |
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
| `docs/persona.md` | 形象：名字、头像、顶栏的动态状态圆点、配色怎么做成一个包（hub 的 `~/.jarvis/personas`），色系怎么接到设计 token，接入新形象的脚本（Codex 生图） |

## 构建与验证

- 服务端：`./scripts/test-server.sh`（在 hub 机器上连真实 Postgres 跑全部测试）；部署 `./scripts/deploy.sh [机器 id…]`（默认配置里的全部机器）。
- iOS：`./scripts/sim.sh [-openSession <machine>/<session>]` 编译、装到配置里的模拟器、自动配对 hub；截图 `xcrun simctl io <udid> screenshot`。
  直接用 xcodegen / Xcode 前先 `python3 scripts/jarvis_config.py xcconfig`（生成 bundle id、团队）。
- Android：`./scripts/android.sh [install|apk|test]`（`docs/android.md`）。
- 网页：`cd web && npm test && npm run build`；部署 `./scripts/deploy.sh web`（`docs/web.md`）。
- 发布：`docs/release.md`。**每次发 TestFlight 都内部、外部两个一起发**（2026-10-30 用户要求）：`release.sh` 上传后跑
  `python3 scripts/asc_testflight.py wait 45 <构建号>`（进内部组），再跑 `python3 scripts/asc_testflight.py external`
  （挂到外部组、提交 Beta 审核、打印公开链接），两个都好了再告诉用户构建号和审核状态。

## 经验

按主题分组；新经验写进对应的组，没有合适的组就新开一组。

### 协作与流程

- 多个 agent 同时在这个仓库干活：模拟器各用各的（`SIM=<udid> scripts/sim.sh`），别人没写完的文件编译不过时在 `git worktree` 里构建自己的改动；
  提交只暂存自己的文件 / hunk。
- 多个 agent 同时往 master 推：rebase 冲突时不要用自己的旧副本整文件覆盖别人的改动（2026-10-25 一次 rebase 把浅蓝气泡、通知跳转修复整个盖回去了）；
  解决冲突只改冲突块，push 前 `git diff origin/master...HEAD --stat` 看看有没有动到不是自己的文件。打 TestFlight 前确认 master 是修好的那一版。
- 多个 agent 并行改同一仓库（飞书工作台 6 个 worker，见 `docs/feishu.md`）：每人独立 worktree + 独立克隆模拟器；新增类型带领域前缀
  （`FeishuSearchXxx`…，否则 master 撞名编译失败）；rebase 冲突时自己目录外的文件一律取 origin/master 版本，push 前看
  `git diff origin/master --stat` 只含自己的文件（14fe469 曾用旧副本覆盖半个仓库，941d067 恢复）；主工作区有别人未提交的改动时，
  自己的修复在 `git worktree add --detach /tmp/x origin/master` 里做。
- 数据库迁移号按车道分段（00x1…），挑号前先看所有 worktree 的 `server/internal/db/migrations/` 和 `jarvis_test` 库的 `schema_migrations`：
  别的车道没合的迁移可能已经占了号（2026-10-30 0051 撞号，改用 0061）。
- 2026-10-28 改写过一次全部 git 历史（去掉内网信息和个人信息，GitHub 仓库删了重建）：此前的提交号全部失效，
  别的机器上的旧克隆要删掉重新 clone，不能 push，否则旧历史会被推回去。
- 等后台任务 / 日志的脚本一律带超时，成功、失败、超时都退出并打印一行结论（2026-10-29 一个只等成功的 `until grep` 空等了五个多小时）。
- macOS 的 sed 不认 `\b`，整词替换用 `perl -pi -e 's/\bFoo\b/Bar/g'`。
- `scripts/test-server.sh` 在两个 checkout 里同时跑会共用同一个 `jarvis_test` 库，DB 测试会互相干扰随机失败：并发跑时单独重跑失败的包确认。

### Claude Code、Codex 与 herdr

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
- 没装 herdr claude 集成的机器认窗格对应哪个会话，用 `herdr pane process-info` 拿 claude 的 pid，再读
  `~/.claude/sessions/<pid>.json` 的 sessionId；别靠「同目录最新的 transcript」猜（同一个仓库里开六个 Agent，tab 名全串了）。
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
- Claude Code 默认清理 30 天前的 transcript（`cleanupPeriodDays`，启动时做，2026-10-29 一次删掉了这台机器 200 多个会话）：
  想留作解析样本的会话要及时脱敏存成 fixture（`server/internal/transcript/testdata/README.md`）。脱敏只能白名单式，
  「短数字原样保留」这种放行会把 IP 的每一段都放过去（踩过）。
- 新开的 herdr 标签页名字（2026-10-31 用户要求）：以前往已有空间里加的标签页一律写死叫 `jarvis`。现在新对话表单可填「标签名」，
  不填就由 hub 按第一条消息起短英文名（`hub/tabname.go`）。gpt-6.1-terra 这类推理模型先花隐藏的思考 token：`max_tokens` 给 20 时回的 content 是空的
  （`stop_reason: max_tokens`），给 200 才有名字；小任务里给模型的 token 上限别卡太紧。
- herdr 会把早就结束的 Codex 会话报成 working（2026-10-30：两天前结束的一轮显示「正在工作 45 小时」八分钟，rollout 没动过；App 的计时从那一轮的用户消息算起）。
  node 以 rollout 为准：结尾是 task_complete / turn_aborted、30 秒没写过，就当 done（`codexTurnOver`）。
- 同一个 Claude 会话 id 可能在两个配置目录里各有一个文件（在另一个 profile 下 resume 过：旧的停住，新的接着写）。按目录顺序取第一个会读到旧的：
  在跑的会话在手机上停在两天前、子 Agent 报 not found、列表里同一个会话出现两次（2026-10-31，一台机器 45 个）。按 id 找文件一律取最近修改的那份（`newestFirst`）。
- Codex 窗格认会话别靠「同目录最新的 rollout」（和 Claude 那条一样的坑）：终端标题是「线程名 | 目录」，按 `session_index.jsonl` 的线程名认；
  rollout 在线程开始那天的目录里一直写，按日期目录只扫近两周会漏掉老线程（2026-10-31 一个正在跑的 Codex 显示成别的会话、灰点）。
  按 id 对拍内容测不出这种错，要拿窗格的真值比（`scripts/live_panes_check.py`）。
  Claude 窗格同理：Jarvis 启动过的会话的窗格绑定存在 hub 数据库里、node 重启就载回来，可能早已过时；`sessions/<pid>.json` 优先于绑定。
  改了认窗格的逻辑，部署（重启）后要再跑一次复核，重启前后的结果可能不一样。
- herdr `agent.prompt` 先粘贴、约 300 ms 后才回车：同一会话的两次发送要串行，否则第二次读屏会把第一条当成输入框里的草稿。发送去重放在 hub 的 Postgres（`sent_messages`）：
  node 的内存去重在重连 / hub 重启后就没了，而 App 恰恰在这时候自动重发。会话读取和发送的完整验证、每一跳的耗时见 `docs/lanes/session-verify.md`。
- 带图的消息有一次停在了 Claude 的输入框里没发出去（2026-10-31，Claude Code 2.1.206：框里是 `[Image #1]` 和文字，Agent `done`，在电脑上按回车才发出），
  之后在单独的 herdr 会话里试了 42 次（小 PNG、1.8 MB JPEG、CPU 跑满）都没再出现，原因没找到。node 的兜底（`ensureSubmitted`）：发完 1.5 秒后
  输入框里还是刚发的那段（`[Image #n]` / `[Pasted text #n]` 去掉后比开头几个字）就补一次回车；框空了、或者是别的内容，什么都不按。
- Codex 生图（`scripts/persona/make_persona.py generate`）：`codex exec` 的内置 `image_gen` 用 Codex 自己的 ChatGPT 登录就能出透明背景的 PNG，
  但它的 HTTP 客户端不认 `socks5h://` 代理（对话请求认），要给 HTTP CONNECT 代理；`-i` 吃多个值，提示词从 stdin 给；ChatGPT 登录只能用部分模型。
  生图卡住（一张十几分钟、并行全停）先量代理隧道的吞吐，别先怪限流：2026-10-31 是 hub 机器外网只剩 11 KB/s，换一台外网快的机器开 `ssh -D` 后四个形象 6 分钟画完（`docs/persona.md`）。

### 助手（无头 Claude）

- 无头 `claude -p` 里 AskUserQuestion 不可用（不是 hook 没接上，是工具本身被禁用）：要在手机上问用户，给它一个会阻塞等答案的 MCP 工具
  （`ask_user`，`docs/assistant.md`）。模型会把它和别的工具并行调用，有副作用的工具要自己在 hub 里批准。
- 无头助手（`claude -p`）默认权限模式下，连 Jarvis 自己的 MCP 工具（notify_user、schedule_followup）都要在手机上批准：夜里到点的跟进、
  Monitor 事件就卡在批准上（2026-10-30 实测）。助手进程带 `--allowedTools mcp__jarvis`，会替用户出面的工具由 hub 自己再问（`docs/assistant.md`）。
- 助手的后台任务和 Monitor 是 claude 进程的子进程：回收空闲进程、systemd 重启 jarvisd（杀整个 cgroup）都会把它们一起杀掉，对话永远等不到通知。
  node 从 stream-json 的 `system/background_tasks_changed` 知道还有什么在跑，有任务就不回收，任务列表随时落库，重启后告诉对话丢了什么。
- 助手按时做的事只能有一张表（2026-10-31，`docs/assistant.md`「定时任务」）：以前定时任务和跟进分两套，重复的巡检只能一轮接一轮地
  `schedule_followup`，跑起来那一刻就从列表消失，卡在审批上的一个多小时里用户看不到、停不了。重复就做成重复（`every`），
  一轮重排自己时 hub 认出是同一个任务；任务的模型 / 权限模式只让用户在 App 里改，助手不能自己提权。`bypassPermissions` 对无头 claude 只能启动时给。
- 助手调飞书用 lark-connector 自带的 lark-connector skill（副本在 `server/internal/assistantskills/`，编进 jarvisd，起助手时装到它的 `.claude/skills`；
  `scripts/sync_lark-connector_skill.sh` 同步上游）。token 只进助手进程的环境变量；写操作靠 node 的 Bash PreToolUse 闸门（`node/larkgate.go`），
  只给助手用的 `assistant-settings.json` 带这个 hook。闸门读不懂的命令里只要有 `ol POST/PUT/PATCH/DELETE` 也要手机确认，否则 bypass 模式会直接放过。
  在 hub 上起测试实例用独立 HOME / 端口 / 库（`/tmp/jt`），别碰线上 jarvisd；`ssh … 'pkill -f 模式'` 会匹配到 ssh 自己的命令行把会话杀掉，模式写成 `[/]tmp/…`。

### hub / node 与网络

- ssh 会话读不了 macOS 的登录钥匙串：hub 经 ssh 起的 node 再起的 claude，如果 API key 从钥匙串取，会报 401（herdr 里的 Agent 没事，herdr 在登录会话里）。
  在那台 Mac 上装常驻 node（`scripts/node-launchd.sh install`，`docs/server.md`「常驻 node」）。
- Go 的 `UnixListener.Close()` 默认会删掉 socket 文件：重新部署时旧 node 退出会把新 node 的 `~/.jarvis/node.sock` 删掉，
  hook / MCP 全部静默失效（hook 连不上就放行给 TUI）。已 `SetUnlinkOnClose(false)` 并每 15 秒自检重建。
- hub 的内存状态随时会被别的 agent 重新部署清掉（2026-10-25 两分钟内被部署三次）：跨请求的流程（比如 OAuth state）要自包含或落库。
- 代理视频 / 音频必须转发 Range / Content-Range（AVPlayer 先要 2 个字节，拿到整个文件就拒播）；`URL.appending(path:)` 会把 `%`
  再编码一次，ID 里带 `/` 的路径要用 `percentEncodedPath` 自己拼。
- App 进后台不断开 WebSocket 的话，挂起的 socket 在 hub 看来还活着，`phone_*` 调用会干等到超时；现在进后台就断开，需要时用静默推送唤醒。
- hub 的 API 默认回 `Cache-Control: no-store`：不加的话 iOS 的 URLCache 会把每个 JSON 分页存一份（后台同步十几 MB，从来不读）。
  App 自己做磁盘缓存的东西（对话图片、飞书媒体）用 `APIClient.uncachedSession` 取，否则同一份字节存两遍。
- hub 机器走 TUN 代理时，系统自动升级会重启 systemd-networkd，它默认删掉别的程序加的 ip rule（代理的路由规则没了，经代理的连接全部超时；
  Tailscale 会自己补回，代理不会）：在那台机器上用 networkd 的 `ManageForeignRoutingPolicyRules=no` 防止（2026-10-29 推送因此断了约 3 小时）。
  APNs 是一条长连的 HTTP/2，网络断过以后 Go 不会自己发现连接已死，推送请求出错时要关掉空闲连接，下一次重新拨号（`push.go`）。
- `URLSessionWebSocketTask.sendPing` 的回调只在两个条件都满足时才来：对端的 pong 原样带回 ping 的负载（空 pong 不算），
  而且当时有一个挂着的 `receive`。`scripts/mock_hub.py` 曾回空 pong，App 的心跳就每分钟判一次连接已死、重连（hub 用的 coder/websocket 没问题）。
- 机器监控（`docs/machines.md`）：darwin 的 jarvisd 是 `CGO_ENABLED=0` 交叉编译的，拿不到 `host_statistics` 等 Mach 调用，
  `vm.page_*` sysctl 又没有匿名页和压缩器数字：macOS 的 CPU / 内存来自常驻的 `iostat -n 0 -w 10` / `vm_stat 10`（每 10 秒一行，别每次采集起进程），
  其余是 sysctl、路由 socket（`NET_RT_IFLIST2`）和 statfs。手解 C 结构体（`if_msghdr2`、`struct loadavg`）前先用一个小 C 程序打印 `offsetof` 确认偏移（有 `#pragma pack(4)`）。
  Linux 判断虚拟网卡看 `/sys/class/net/<名字>` 是否指向 `/sys/devices/virtual/`，比按名字猜可靠（TUN 代理的网卡名五花八门）。
- macOS 的「桌面」「文稿」「下载」受隐私保护（TCC）：jarvisd 经 ssh 在 Mac 上运行，没授权时读这些文件夹报 `operation not permitted`，
  手机上打不开那里的文件。node 把这个错误换成说明（`explainDenied`）：在「完全磁盘访问权限」里打开 sshd-keygen-wrapper，或换个文件夹。
  要给用户看的文件（截图等）别放桌面，放 `~/` 下的普通目录。
- hub 关掉 RPC peer 不等于连接结束：ssh 子进程还活着，连接循环要同时等 `peer.Done()`，否则「重连」之后机器一直显示在线、每个调用都回 `rpc: connection closed`（`TestReconnectGivesAWorkingConnection`）。

### transcript 解析、同步与分页

- 会话的「最近编辑时间」要用最后一条真实对话（user / assistant / 工具）的时间：Claude Code 会往闲置会话追加 recap、ai-title、
  mode 等行，文件 mtime 会被刷新；找不到 transcript 的会话时间留空，不要用 now 兜底（会一直显示「刚刚」）。
- 排队消息：transcript 的 queue-operation（enqueue / dequeue / remove / popAll）能精确还原队列；TUI 里 ↑ 会把**全部**排队消息拉回输入框，
  ctrl+u 只清当前行（多行要 ctrl+u + backspace 逐行清）。长文本经 herdr bracketed paste 进去会被包成 `<pasted_content id=…>`，显示前要去掉。
- Claude 工作中输入的消息（电脑或手机）会被「中途吸收」，transcript 里存成 `attachment/queued_command`（commandMode=prompt、
  origin.kind=human）而不是 user 消息，解析器要单独处理，否则手机上看不到。
- 新版 Claude Code 把工具调用之间的叙述写进非空的 `thinking` 块（不是 `text`），界面不能把 thinking 一律藏掉。
- 新会话（无头助手或刚启动的 herdr Agent）在 claude 写下第一行之前没有 transcript：手机一订阅 hub 就要 backlog，`session.items`
  对这种「正在启动」的会话（`startingSession`）要返回空列表而不是 not found，watch 等文件出现后先补发已写的行，否则对话页会闪一下报错。
- 「还有没有更早的消息」（`has_more`）只有从最新一端分页的响应才知道：订阅时带 since_seq 的补发（catch-up）以前发 `has_more: false`，
  它要是比 HTTP 第一页先到，App 就以为历史到头了，往上翻停在已加载的最早一条（2026-10-26）。现在 hub 补发时不带这个字段，
  App 把缺失当未知，而且一批没覆盖到本地最早一条的消息不许把它改成 false。复现：`-showScrollToOldest YES` 一路往上翻并记日志。
- 带很多截图的会话，一页更早的消息有 1–5 MB（图片 base64 内嵌）：分页在后台线程解码，最早的十几行一出现就预取前一页。
- 后台同步别靠「订阅所有会话」：订阅会让 node 每 250ms 刷 transcript、每 400ms 读一次终端（herdr 进程调用）。改成 IM 式的推拉结合：
  hub 只推每个会话的 `rev`（transcript 字节数），App 按 `after_seq` 拉增量（`docs/ios.md` 后台同步）。整个会话列表约 130 KB，
  别为 rev 变化重发列表，用单独的 `revs` 事件。lark-connector 有实时事件 WebSocket（`/agent/v1/events`，`message.upserted` /
  `conversation.upserted`），飞书的推送从这里来。
- 改 transcript 解析或同步协议，用真实语料验证（`docs/lanes/msg-render-verification.md`）：`corpus_test.go` 让新旧解析器逐文件对比，
  `continuity_corpus_test.go` 按 App 的规则模拟丢推送 / 补洞 / 重写。语料只放 `~/jarvis-corpus`；活跃会话的文件会一边读一边长，对比前先拷一份快照。
  模拟器里的 App 看真实会话用本机的测试 hub（假 HOME、不连 herdr、独立数据库），别连线上 hub。
- 大 transcript 冷打开只解析末尾窗口（`transcript/window.go`）。一行解析成什么只通过三样东西依赖前面的行：已显示过的后台任务通知
  （重复的丢掉）、上一个「! cmd」输入、Codex 同一 item 记两次。改解析器时如果新加了「跨行状态」，必须同时让窗口的起点能重建它
  （`seeded` / `seeNotifications`），否则窗口读和整份读会不一样；`TestFixtureWindowedEqualsFull` 和语料的 `TestCorpusWindowed` 会抓到。
- 用户发的图片有两条路：普通消息里是 user 的 image 块；Claude 工作中发的消息被「中途吸收」成 `attachment/queued_command`，图片在 prompt 的块数组里
  （2026-10-29 以前解析器只取 text 块，气泡里看不到图）。App 里有后台任务唤醒过的轮（`pickAnswerAcrossWakes`）要把用户的图片和消息一起留在原处，别当成工作过程。
- 长会话首屏慢不在 hub / node（冷读 0.12 s），在页太大：一个 Codex 长会话最后两轮 1412 条、1.08 MB，回复只有 6 条，其余是界面上本来就折叠的工具和思考。
  App 请求带 `fold_work`，从末尾往前的页把每段工作换成一条 `kind: fold`（只带计数和范围），展开工作行时按 `from_seq` / `through_seq` 再取
  （`docs/protocol.md`「折叠的工作」）。`after_seq` 补发和实时推送不折叠；还在跑的 tool_use、另一半不在页里的 tool_result 不折叠，否则配不上对。
- 后台任务通知（`<task-notification>`）的 summary / event / result 里 Claude Code 自己写了 `&gt;` `&lt;` `&amp;`，解析时要解码；没写 description 的后台命令
  summary 是整条命令（heredoc 几十行），别把 summary 当 notice 文字：node 解析成 `Item.task`，App 画成工具行样式的一行（`TaskNoteRow`），
  Monitor 事件只带 task-id，要靠 `Monitor started (task <id>` 的 tool_result 对回调用（`TaskIndex`）。
- App 的会话磁盘缓存按 item id 合并，hub 换了某类 item 的内容（比如给 notice 加字段），缓存里的旧 item 不会被新页覆盖：改 item 结构就把
  `ChatCache` 的目录版本号加一（`chats-v3`）。
- 往前翻页有解析预算，但前面还有内容时不能回空页：空页 + `has_more: true` 让 App 没有 seq 可以接着取，更早的历史就再也看不到（大 Codex rollout 两轮之间夹着几十 MB 不出条目的行）。

### 飞书与邮件

- 飞书消息渲染照搬 lark-connector（`docs/ios.md`「飞书消息渲染」）：hub 原样转发 `parts`，只把媒体 URL 改写到 `/api/v1/lark/media/`；
  看 lark-connector 源码直接 `git clone https://github.com/Eric-2369/lark-connector`（hub 机器上只有部署产物）。
- lark-connector 会话列表的坑（`docs/protocol.md` 飞书 API）：`isDone` 字段不可靠（done scope 里全是 false），已完成以 scope 为准；
  置顶看 `onTopRankAt`（`settings.isShortcut` 也不准），且置顶会话分散在 inbox 和 done 两个 scope；未知 scope 会被静默当成 inbox。
- 打开飞书会话不能用 `tail=1`（那是「回到最新」）：有未读时要 `aroundPosition=readPosition+1`，已读只上报真正上屏的消息。
- 有的网络（作者的 hub 机器）连不上 Google 的多数 IPv4 地址（`imap.gmail.com` / `smtp.gmail.com` 常解析到 173.194.x.x，所有端口超时），HTTPS 能通只是走了 IPv6。
  hub 访问 Gmail 一律走 `hub.json` 的 `google.proxy`（比如 hub 机器上的本地代理），别再加直连兜底（`docs/mail.md`）。
- IMAP 扩展要先看服务端声明的能力再用：QQ 邮箱不声明 LIST-EXTENDED，收到带 `RETURN` 的 `LIST` 直接不回，只能等超时（`docs/mail.md`「注意事项」）。
- 飞书文档 / 表格 / 多维表格里 @我、评论我，不进 IM 会话列表（没有「云文档助手」会话），只在 lark-connector 的文档通知中心
  `documents/notifications`，而且卡片只说「谁提及了你」，不说哪一行：要自己读表按「@名字」找（`docs/inbox.md`「云文档」）。
  表格里带下拉的格子 `sheet/edit` 一律报 `DOCUMENT_EDIT_UNSUPPORTED`，改值要用 `sheet/structure` 的 `choose`；多维表格 `base/edit` 的
  `expect` 指纹要按当前值算（`BaseCell.Fingerprint`），人员字段的 id 格式和 `accounts` 给的 userId 不一样（写不进去，读得出）。
  测试只拿自己建的 demo 文档（链接放环境变量），别动用户给的真表。

### 终端

- herdr `pane read --source recent` 最多返回约 1000 行（--lines 再大也一样），没有偏移或滚动接口，也不落盘：终端历史只能分段读这 1000 行，全量在 transcript。
- 要分清单击 / 快速双击 / 按住的键（终端键栏的修饰键）别用 `DragGesture(minimumDistance: 0)` 记按下松开：快速双击的第二下它收不到
  （XCUITest 的 doubleTap 只来一对 down / up）。用一个覆盖在上面的 UIView 重写 touchesBegan / Ended（`TerminalKeys.swift` 的 `TouchArea`）。
- 网格里写宽字符要分两步：左半照常写，右半（宽 0 的占位格）不能走「覆盖宽字符右半就清掉左半」的规则。
  否则在同一位置重写一个宽字符时，刚写的字会被清掉。herdr 流式重画中文正是这样写的，手机上整段缺字（`TerminalGrid.putRightHalf`）。
  改终端解析一律用录制的真实帧回放，和 `herdr pane read` 逐行比（`TerminalWrapTests`）。
- 自定义视图直接收键盘（`UITextInput`）时，`hasText` 要恒为 true，否则空文档时键盘会自动切大写；拼音靠 marked text。
  选定候选时可能来 `insertText`，也可能来 `setMarkedText` + `unmarkText`，两种都要当提交处理（`TerminalInput.swift`）。
- 终端流的帧率上限是 Agent 自己的重画节奏，不在我们的链路上。Claude Code 2.1.312 流式时约 110 ms 重画一次（主要是转圈那一行），
  正文分几次大块整屏出；herdr observe 最快每 16 ms 一帧；node / hub 原样转发，只把 30 ms 内的帧合成一帧（`termMinGap`）。
  分段实测见 `docs/terminal.md` 第十节。
- 手机接管终端用 `herdr terminal session control <pane> --cols --rows`（管道，不要 PTY，和 `terminal attach` 共用一个槽位）：
  stdin 关掉、进程被 kill（包括 -9）herdr 都会把窗格放回原尺寸，所以接管进程的生死跟着 node 走就不会卡住电脑。
  herdr 从不给程序的原始字节（attach 也是它重画的网格），接管期间也没有滚动历史：历史只能手机按帧比对自己攒（`TerminalGrid.scrolled`），
  别指望换一个终端仿真库解决。键要走 RPC 的事件（method 在对端并发处理，会乱序）。`docs/terminal.md` 第十一节。
- Claude Code（2.1.312，默认模式）窗口一改尺寸就清掉终端的滚动历史、只重画当前屏；接管期间它的输出也不进 herdr 的历史（回复 70 行，herdr 历史仍是 0），
  用户认为接管前的历史被清掉是正常的，别再去拼 transcript（试过，用户不要）。滚动照 Moshi：手指滑 = 滚轮，经 hub → node → herdr `terminal.scroll`，
  herdr 交给开了鼠标上报的程序（Claude 的全屏渲染会自己往回画对话、出「Jump to bottom」；Claude 2.1.312 的 fullscreen trial 不用配置就可能打开），否则滚 herdr 自己的历史。
  别跟用户说「要配 fullscreen」：Moshi 什么都不用配，是 herdr 客户端在接鼠标。`docs/terminal.md` 第十二节。
  herdr 的 `terminal.scroll` 对开了鼠标上报的程序**一条命令只发一个滚轮事件（`lines` 不管用）**，不给 `column` / `row` 就落在 1;1：
  Codex 把本轮提问钉在第 0 行、那里不接滚轮，所以手机滑了也纹丝不动。node 按行拆成多条、落在手指按下的那一格（没有就屏幕中间），第十六节。
  接管要等终端视图第一次布局量出手机的列 × 行才订阅：第一帧到之前也必须把终端视图放上去（转圈只能盖在上面），
  build 21 用转圈顶替了视图，已连上 hub 时点「终端」永远转圈；UI 测试用 `-openPane terminal` 直接开时先走了 HTTP 读屏，测不出来
  （`TerminalTypingTests/testOpeningTheTerminalWhenConnectedDrivesIt` 先进会话、连上后再点「终端」）。
- herdr 的 observe 流要按窗格本身的尺寸开（`--cols/--rows` 小了是裁左上角，不是重排），窗格变尺寸后要重开；`terminal attach` 会改窗格尺寸、占唯一槽位，别用（`docs/terminal.md`）。

### iOS：聊天列表与滚动

- `ScrollView` + `defaultScrollAnchor(.bottom)` + `LazyVStack`：跳到很远的 id 会因为上方懒加载视图实际高度与估算不同而漂移，验证历史渲染用 `-fromSeq`。
- 翻转的聊天列表别让它贴安全区：上方垫 1pt 空视图、输入框放在下方的 VStack 里；用 `ignoresSafeArea` + 按 inset 补边距会在键盘弹起时白屏、顶部错位。
- 聊天列表用倒置（ScrollView 和每行各 `scaleEffect(x:1,y:-1)`）：最新消息在偏移 0，跳到最新永远精确。倒置后要自己处理安全区
  （`ignoresSafeArea(.container, edges: .vertical)` + `contentMargins` 按实际 inset），并关掉 iOS 26 的 `scrollEdgeEffect`（否则整页发雾）。
  只往偏移 0（最新端）跳；往远端用代码跳、或加 `defaultScrollAnchor(..., for: .sizeChanges)`，懒加载估算不准时照样白屏。
- 聊天列表（LazyVStack + 底部锚定）偶发白屏：按估算高度跳到底会落在没渲染的区域。跳转要按哨兵 id、等布局完成再跳，并在哨兵真正可见前重试；
  `atBottom` 不要默认 true（白屏时就没有箭头可点）。验证：脚本批量冷启动打开会话，按对话区像素标准差判断白屏。
- 长列表里不要把一组可能很多的行包成一个子视图（LazyVStack 里的一个 VStack）：几百行一次布局会卡死主线程、页面空白，要拆成独立的懒加载行。
- SwiftUI 的 `onScrollPhaseChange` 在内容流入、偏移被程序调整时也会进 `.animating`：只想响应用户滚动就判断 `.tracking` / `.interacting`。
- 跟手的收起 / 展开（Safari 式工具栏）不要做成「切换状态 + 动画」：把进度（0…1）放在单独的 `@Observable` 里、只给那一小块视图读，
  滚动回调每帧写它；动起来的那块浮在内容上（overlay），别让内容的 frame 跟着变，否则每帧重排整页、消息跟着跳。松手再吸附。
- 对话行的 body 会被反复求值（滚动、流式更新）：从消息文字推出来的东西（Markdown 解析、路径 / 链接识别、去掉标签后的文字）
  按消息 id 缓存（`RowMemo`，`TextMemo`），并在消息加载后放到后台预热；别在 body 里跑正则 / NSDataDetector（实测是主线程最大的热点）。
- 滚动时别在主线程建 WKWebView：链接预加载只给刚出来的回复（15 分钟内），翻历史时每条旧回复都去预加载曾造成 1–2 秒的冻结。
- 滚动中写入的簿记（可见行、手指是否按着）放在不被观察的对象里（`ChatScratch`），写 @State 会让整页重算；派生的行列表按
  `ChatStore.version` 等缓存。WebSocket 消息在回调线程上解码，别回到主线程再解。
- 聊天列表用 `ScrollPosition.scrollTo(y:)` 会把横向偏移也归零：左右边距是 `contentMargins(.horizontal)`，整页会贴到屏幕边。
  竖向跳转用 `scrollTo(point:)` 带上当前的 x（`ChatView.scrollList`）。`scrollTo(id:anchor:)` 跳到没画过的行只按估算高度落点，
  要精确停在某处就跳过去后按实测位置再修正一两次。倒置列表里的行，XCUITest 拿到的 frame 不对（报在屏幕外），量位置用 App 内的 DEBUG 探针。
- 倒置聊天列表往上翻时的「冻结」不能按行 id 截：行 id 会随轮次推进变（答案收进工作行、多条消息的轮合并成 `wt-`），
  找不到 id 冻结就失效、可见内容被顶走（修前实测 319pt）。按「离开最新端时的 item 快照」重建行（`ChatView.Frozen`），验证用 `-showFreezeProbe`。
- BeautifulUI 的组件放进聊天列表前，先看它每次出现时做了什么。懒加载列表滚动时会反复重建行，每次出现都起 `.task`、挂入场动画（`buiEntrance`）的组件，
  在长工作卡里滑动会明显掉帧：ThinkingState 原样放进 650 步的工作卡，Debug 模拟器上卡顿时长从约 11 涨到约 21 ms/s，去掉这两样后约 13.7。
  这类问题改在 BeautifulUI 仓库里，再整个拷进 `ios/Packages/BeautifulUI`（`VENDORED.md`），不在 Jarvis 里改库代码。
- 倒置聊天列表里点开 / 收起的东西要按住表头（`ChatPin`，2026-10-31）：长出来的内容会把表头和它上面的一切往上推（思考跳 40pt，工作卡跳约 440pt、第二下点到别的步骤上）。
  表头按「同一次布局里的屏幕位置 + 内容坐标」报位置（内容坐标不随滚动变），偏移 = 点下时的偏移 + 表头在内容里挪了多少；别用回报的 offset 算（晚一帧就过冲、来回跳）。
  `ScrollPosition.scrollTo(point:)` 先落在要的点、过一会儿又差一个内容边距（10pt）、再回来；存着的 `ScrollPosition`（`edge: .top`）在内容变化时会被重新套用，
  工作步骤从 hub 到达时列表跳回最新端一帧。按住期间直接设列表 `UIScrollView` 的 `contentOffset`（`ChatPin.Finder`），开始时清掉存着的位置。
  `scrollTo(id:anchor:)` 的 anchor 对的是整行，打开的工作卡整张算一行，钉不住表头。只有比屏幕高的对话能完全按住（`MOCK_LONG_HISTORY=1`）。
- hub 一直在推（会话列表、机器、对话的新步骤），每次推送都会重算读了这些数据的视图，正在用的东西别挂在它们上面（`docs/lanes/live-updates.md`）：
  工具栏写在跟着数据重算的视图上，每次推送都重新交给 UIKit，打开的 ⋯ 菜单滚动位置被重置（菜单内容不变也一样）：`ChatsView` 只管导航和工具栏，列表是 `AgentsList`
  （拆开后 `ChatsView` 的 body 要读一下 `searching`，否则从搜索结果打开的对话又顶着系统导航栏，`SearchOpenTests`）；
  body 里写的闭包环境值（`.environment(\.openFile) { … }`）每次都是新值，读它的行全部重算（每次推送重算所有可见的过程步骤），用一次建好的 `ChatActions`；
  `model.session(key)` 按会话单独观察（`SessionCell`），别让一个会话的页面跟着所有机器的列表重算；全屏查看器、行的展开状态放在页面上，别放在懒加载的行里。
  量法：`LiveUpdateTests`（mock 的 `/mock/churn` 连续推，`-perfCounts YES` 报每轮的 body 次数和主线程耗时）。

### iOS：导航

- NavigationStack 根视图上的 `.task` / `onAppear` 在 push 子页面再返回时会重新跑：别在里面无条件写回保存的默认值，否则会覆盖刚在子页面选的值（新对话选了启动器又被改回上次的）。
- SwiftUI：给 NavigationStack 推出页面用的环境值（如 `feishuOpen`）要挂在 NavigationStack 本身上，挂在栈根内容上推出的页面拿不到。
- 隐藏系统导航栏（`.toolbarVisibility(.hidden, for: .navigationBar)`）会顺带关掉边缘右滑返回，要自己把 `interactivePopGestureRecognizer` 打开。
- 列表的 `.searchable` 处于搜索状态时推出的页面，系统导航栏会被强制显示（隐藏导航栏的对话页出现两行标题）；推出之后再关搜索也没用。
  要先关搜索（`searchable(isPresented:)` 置 false）、下一轮 run loop 再推（`ChatsView.pushing`），搜索词和结果保留。关搜索会清空系统搜索框里显示的字（`query` 不变，没人写回去），
  返回列表时结果还在、框是空的：推之前记下搜索词，回到列表时重新赋一次（`searchedBeforePush`）。`SearchOpenTests` 覆盖（含框里的字）。
  「关了搜索再推」不能按固定时间等：关搜索只是开始收起，收起途中推的页面被导航栈丢掉，而 path 里已经有这个对话（屏幕上还是列表，
  状态和画面对不上）；`isSearching` 在点下后几毫秒就变 false，不能当「收完了」。列表为关掉的搜索重画慢时（2026-10-31 实测点下后主线程忙约 440ms），
  #128 的 350ms 就不够，「从搜索结果点开会话打不开」。现在看系统自己的 `UISearchController`：不再 active、不在 dismiss、没有转场了再推
  （`SearchCloseWatcher`，最多等 2 秒）。搜索时折叠的空间 / 分组也要展开（`isFolded`），否则匹配的 Agent 被折在标题下面，搜得到点不到。
- XCUITest 的边缘右滑在 iOS 27 模拟器上要按手指的速度拖（按住 0.1 秒、`.slow`、末尾停 0.1 秒）：0.05 秒的快甩不被当成边缘手势，页面不动，
  看起来像「右滑返回坏了」（手动是好的，`SearchOpenTests.back`）。列表里的标题 / 行在小一点的手机上可能在屏幕外、懒加载还没建出来：
  测试先滚到它（`AgentsListTests.reach`），并在 6.3 寸的机型（iPhone 17 Pro）上也跑一遍，不能只在 Pro Max 上跑。
- 同一个 NavigationStack 里，用 `NavigationLink { 目标 }`（视图式）推出来的页面再用 `NavigationLink(value:)`（值式）推下一页，
  新页面会被插到它**下面**：点了像没反应，返回才看到（设置 › 机器 › 卡片、机器详情 › 会话都踩过）。一条链上要么全用值式（`MachineListRoute`、
  `MachineDetailRoute` + `navigationDestination(for:)`），要么全用视图式。`navigationDestination(isPresented:)` 也算视图式。
- `NavigationPath([x])` 里的 `x` 如果是 `AnyHashable`，路径记的类型就是 AnyHashable，`navigationDestination(for: 具体类型)` 找不到，
  页面是一个黄色 ⚠️：要按具体类型 append（`SettingsView.openDebugPage`）。
- `NavigationSplitView` 右栏里推出的页面，是用左栏登记的 `navigationDestination` 在左栏的环境里建的：给右栏页面的环境值
  要挂在整个分栏视图上，挂在右栏的 NavigationStack 上拿不到（`SplitNavigation`）。
- iPhone Duo（`docs/ios.md`「iPhone Duo」）：按宽度（size class）分布局，不看机型；模拟器开合只能在 Device Hub 里切，
  截图要 `--display=1/3` 指定屏幕，否则可能拍到熄灭的那块（全黑）。
- 推出的页面别用 `.toolbar(.hidden, for: .tabBar)`（`.toolbarVisibility` 一样）藏 tab 栏：它在 push 的第一帧就把 tab 栏拿掉，
  底下列表的导航栏和搜索框同一帧跟着消失，点进对话「闪一下」（2026-10-31）。用 UIKit 的 `setTabBarHidden(_:animated:)` 跟着页面出现 / 消失
  （`hidesTabBarWhenShown`）；`hidesBottomBarWhenPushed` 在 SwiftUI 里设得太晚，不生效。这种闪动 XCUITest 看不出来：
  `xcrun simctl io <udid> recordVideo` 录下来，用 ffmpeg 把点击后 0.3 秒的帧拼成一排看。

### iOS：布局与组件

- 不要写「PrimitiveButtonStyle 里 `Button(configuration).buttonStyle(.glass)`」来包装系统按钮样式：玻璃样式下递归到爆栈，
  打开对话页就崩（崩溃栈全在 Swift 运行时的类型解析里，看不到业务代码）。按风格切样式用 View 扩展在调用处直接选（`.themedButton()`）。
- 横竖都能滑的等宽文本别用 SwiftUI「横向 ScrollView + LazyVStack」（内容宽度算不出来，只能竖滑），也别用自己滚动的 UITextView
  （TextKit 2 无论容器多宽都会折行）：用 UIScrollView 包一个不滚动、按最长行实测宽度的 UITextView（`MonoTextView`）。
- `ScrollViewReader.scrollTo` 会连带滚动外层容器：在顶部横排条里做 `initial: true` 的 scrollTo 把整个顶部区域推上了导航栏。
- 列表上挂的 `.simultaneousGesture(TapGesture())`（点空白关闭浮层）会把长按松手也认成一次点击（按住超过约 2 秒才不认）：
  长按刚打开的消息操作条一松手就被关掉。打开它的那次按压在按住和松手后 0.3 秒内不许关闭（`MessageMenu.pressChanged`）。
- List 的分组标题（Section header）不是单元格，挂在上面的 `.contextMenu` 长按基本触发不了：标题上的操作要给一个看得见的按钮 / Menu。
- MarkdownUI 的 `.markdownTextStyle` 要写在 `.markdownTheme` 里面（先调用），否则被主题的文字样式盖掉。
- 链接在哪结束只有一个规则（`Links`）：空白、任何非 ASCII 字符（中文、全角标点）处结束，去掉结尾的 `.,;:!?` 和不成对的 `)` `]`。
  MarkdownUI 用的 cmark-gfm 自动链接只在空白处结束，中文里一个链接会吞掉后面整段（2026-10-30）：所有交给 MarkdownUI 的文字先过
  `Links.markdown`（把裸链接写成 `<url>`），回复、计划、文件、飞书消息、知识库都一样；路径识别（`FilePaths`）也按 ASCII 结束。
- 回复里的表格不能按内容撑宽（MarkdownUI 的 Grid 每列取最宽的格子，长中文列直接出屏幕）：`FittedTable` 按可用宽度分配列宽，
  短列保持原宽、长列折行，只有最小宽度都放不下才横滑，右边渐隐提示还能滑（`TablePlan`）。
- 自定义 SwiftUI `Layout` 的 `proposal.width` 可能是 `.infinity`，`Int(...)` 前不判 `isFinite` 会直接崩（飞书卡片列布局踩过）。
- List 的 Section header 会把层级样式（`.primary` / `.secondary`）再调淡一级：放在 header 里的卡片名字变灰、说明文字几乎看不见。
  要和别处一样的颜色就写 `Color.primary` / `Color.secondary`（`MachineCard`）。
- List 里空的 Section（折叠后只剩标题）照样占一个标题的上下边距加段间距，「全部折叠」一屏只放得下五六个：折叠的空间改成一行
  （标题当普通行，`listRowBackground(.clear)`，`listSectionSpacing(.custom(0))`，`ChatsView.spaceSection`）。
- `.safeAreaBar(edge: .top)`（Agents 列表的快速筛选条）别和会随滚动收起的 `.searchable`（导航栏抽屉默认 `.automatic`）一起用：
  真机（TestFlight 35）上滚动收起搜索框后，筛选条还停在搜索框展开时的位置，中间空出约 120pt 雾化的列表（「顶部一大坨」）；
  现在搜索框 `.navigationBarDrawer(displayMode: .always)` 常显，顶部高度不随滚动变。同样的「一大坨」还有一个来源，见下面横向 `ScrollView` 那条。
- 列表上方的横向 `ScrollView`（Agents 的筛选条，放在 safeAreaBar 或 VStack 里都一样）可能被导航栏当成这一页的滚动视图，内容整体下移
  整个顶部（状态栏 + 导航栏 + 搜索框，约 170pt），中间露出雾化的列表：`NavigationSplitView` 左栏（Duo 打开 / 半折）一定出；普通 iPhone 上
  冷启动恢复上次打开的对话、列表第一次布局在对话下面时也出，返回列表就是 TestFlight 35 / 38 截图的「顶部一大坨」。**XCUITest 读到的 frame
  仍是没下移的位置**（是 UIKit 挪的内容，SwiftUI 的布局和无障碍 frame 都不知道），以前「模拟器复现不了」就是只看了 frame：
  这种问题要按截图像素量（`AgentsListTests.drawn` 看选中的「全部」是不是真画在 frame 那里）。导航栈下面的页面顶部**别放横向 ScrollView**，筛选条放不下先收窄，
  再换行（`QuickFilterBar`，`ViewThatFits`）。`testFilterBarHugsTheTop` / `testFilterBarHugsTheTopBehindARestoredChat` 覆盖。
- `contentMargins` 作用于它下面所有的滚动视图，包括 `safeAreaInset` 里的：Jarvis 页给悬浮顶栏留的上边距写在输入框的 inset 之后，
  输入框上面那行横向滚动的「权限 / 思考」也多了顶栏的高度，约 66pt 页面色挡住建议卡片，所有风格都有（2026-10-31，#114）。边距写在要它的滚动视图上、inset 之前。
- UI 测试的「返回」别用 `navigationBars.buttons.firstMatch` 兜底：已经回到列表时导航栏上第一个按钮是「新建会话」，点了就弹出新建浮层，
  后面的查找全跑进浮层里，报的是「找不到某一行」，像是演示数据过时（2026-10-31 `DemoModeTests`）。先等目标页可点，再排除页面自己的栏按钮。
- `TextField(text:selection:)` 的选区：没在编辑（未聚焦）时也会报一个选区，冷启动恢复草稿后它指在草稿中间；而且它的
  `String.Index` 可能来自另一份字符串，越界的 `utf16Offset(in:)` 直接崩。插入文字（发送文件的路径）只在聚焦时用选区，先夹到 `endIndex` 再换算（`PathInsert`）。
  UI 测试里草稿会跨次运行保留，测输入框内容前先清掉。
- Swift Charts 的线要在空档处断开：给每段一个不同的 `series:` 值（「指标#段号」），同一个 series 的点总会连起来。
  `AxisValueLabel` 不显式给颜色时，日期刻度会染成 App 的 tint（蓝色），要写 `.foregroundStyle(Color(.secondaryLabel))`。
  落在 `chartXScale(domain:)` 之外的点照样画（线会伸出纵轴、卡片边），要 `.chartPlotStyle { $0.clipped() }`。
- SwiftUI `Menu` 的 label 上挂的东西（`.overlay`、`.surface` 玻璃）都会变成菜单按钮的一部分：玻璃铺满整个 label（头像连名字套进一个大玻璃框），
  overlay 里的文字 XCUITest 也找不到（按钮只报它第一次的 label）。要临时盖在按钮上的提示放在和 Menu 并列的 ZStack 里（`AssistantTopBar.face`）。
- 自己画的浮动顶栏要盖住状态栏的雾，背景用 GeometryReader 读 `safeAreaInsets.top` 往上偏移，直接 `ignoresSafeArea` 的背景高度会被算短。
  整页横向拖拽（Jarvis 的侧边栏）用 UIKit pan + `gestureRecognizerShouldBegin` 在开始时判方向、让给能横向滚动的子视图，SwiftUI 的 DragGesture 做不到这种让位。
- `Chip` 是 `fixedSize` 的，不会缩：放进窄的网格列（空间页一行两张卡）里，长标签名会把卡片撑得比列宽，整个 `LazyVGrid` 挤出屏幕、
  第二行错位（2026-10-31）。窄处的一排 Chip 用 `ViewThatFits` 逐级去掉次要的、最后让主标签截断（`SlotCard.header`），验证用 mock 的 `MOCK_LONG_SLOTS=1`。
- 几万行的文本别放进一个 UITextView（`MonoTextView` 整篇排版，随大小线性变慢）：文件查看器用 `CodeLinesView`，只把屏幕上的行做成 label、滚动时复用，
  行号固定在左边，2 万行的 Go 打开约 0.3 秒、滚动 60 fps。MarkdownUI 渲染很长的文档要切成段放进 LazyVStack（`MarkdownChunks`），
  而且别开 `.textSelection(.enabled)`：实测打开慢约 40%，每段进屏的长帧从约 40 ms 变成约 60 ms（`docs/ios.md`「文件查看器」）。
- 消息里的路径只在那台机器上真的有时才做成可点的文件块（`FilePresence` 批量问 `GET …/stat`，不确定就不链接）：以前别的机器的路径（hub 的
  `~/.jarvis/…`）也能点，点开只说「打不开这个文件」。

### iOS：Swift 与编译

- 被取消的请求 / 任务（离开页面、切换）会抛 URLError.cancelled（「已取消」）或 CancellationError，展示错误前先过滤（`Error.shown`）。
- Swift 的 `dict[k, default: X].method()`：值是 class（`CGMutablePath`）时只是读，默认值不会存进字典，改动全丢（终端框线整条消失过）。
- Swift 的尾随闭包按位置匹配第一个闭包参数：一个类型有两个可选闭包属性（`SpaceMenuItems` 的 `rename`、`open`），
  新加的那个放在前面，原来 `SpaceMenuItems(group:) { 重命名 }` 的调用就悄悄变成了另一个。新加的可选闭包放最后，调用处写标签。
- 中文环境下 SwiftUI 的 `.dateTime.hour(.twoDigits(amPM: .omitted))` 是 12 小时制，17:00 显示成「05:00」：要 24 小时的时刻用固定 `HH:mm` 的格式（`AssistantScheduleText.hm`）。
- `ios/Shared` 里的类型默认是 MainActor 隔离：新加的纯数据 enum（被 nonisolated 的 struct 用到）要标 `nonisolated`，否则报 isolated conformance 错。
  JarvisTests 目标没有默认隔离：测试里用到 App 的类型（静态方法、属性、key path）时整个测试 struct 标 `@MainActor`（和 `ChatStoreTests` 等一样），
  别靠在 App 里去掉隔离来迁就测试；提交前跑一遍整个 JarvisTests，一个文件编译不过整个目标都跑不起来（2026-10-31 `KnowledgeTests`）。
- App 目标默认 MainActor 隔离：交给系统、由系统在自己队列上调用的闭包（`PHPhotoLibrary.performChanges`、`SFSpeechRecognizer.requestAuthorization`、
  `fetchReminders`、`PHImageManager`、`WCSession.sendMessage` 的 reply / error……），SDK 没标 Sendable 的，编译器不报错，闭包被当成主线程代码，
  运行时隔离检查一失败就崩（`dispatch_assert_queue_fail`，2026-10-31 保存图片必崩）。这类闭包一律写成 `{ @Sendable … in`，`BackgroundCallbackTests` 按源码检查；
  SDK 已标 `NS_SWIFT_SENDABLE` 的（HealthKit 查询、URLSession）和在主线程回调的代理不用管。真点系统功能的 UI 测试（`PhotoSaveTests`）才测得出来。
- 发版机器的 Swift（6.3.3）和别的开发机不一样，Release 归档会报「unable to type-check this expression in reasonable time」：
  长修饰符链里的多语句闭包（`.task { … }`）会和整条链一起推断类型，报错的行号还会跟着改动乱跳。把闭包体挪成方法、id 挪成计算属性（`ChatView.prewarmRows`）。
- Swift 按位置先读出参数再调用后面的参数：`emit(s, i, word(…, &i, …))` 里传给 `emit` 的 `i` 是 `word` 改它之前的值（文件查看器的宏 `println!` 少了 `!`）。
  一个参数要改另一个参数用到的变量时，先把结果存进变量。

### iOS：系统能力与页面约定

- 开了 HealthKit entitlement 就必须有 `NSHealthUpdateUsageDescription`（哪怕只读），否则上传报 90683。
- WatchConnectivity 在 activate 完成前 updateApplicationContext 会被丢弃：配对信息要在 activationDidComplete 后再推。
- `UNUserNotificationCenterDelegate` 不要用 async 版本的 didReceive / willPresent：返回时在后台执行器上，UIKit 收到非主线程的完成回调，
  点通知进 App 会崩。用 completion handler 版本，`DispatchQueue.main.async` 里处理完再调 completion。
- DEBUG 启动参数的名字不能和任何 Swift 标识符同名：枚举 case 名会以反射元数据进 Release 二进制，`release.sh` 的 `strings` 检查会误报
  （`Tab.workbench` 撞了 `-workbench`，已改名 `-openWorkbench`）。新开关用 `open…` / `show…` 这类不会是类型成员的名字，并加进 release.sh 的列表。
  存储属性名也一样（2026-10-31：`AgentsListMenu.Actions` 的 `openMonitor` 属性撞了 `-openMonitor`，TestFlight 50 停在这一步）。改了带新属性的代码，
  先本地 `xcodebuild build -configuration Release -destination generic/platform=iOS CODE_SIGNING_ALLOWED=NO`，对 `Release-iphoneos/Jarvis.app/Jarvis` 跑一遍 release.sh 里的 strings 检查。
- 实时活动（`docs/live-activity.md`）：ActivityKit 用默认 JSONDecoder 解推送的 `content-state`，`Date` 会按 2001 年起算，时间一律传 Unix 秒数字；
  锁屏 / 展开态限高约 160pt，超出会被静默裁掉；`simctl io screenshot` 拍不到灵动岛和锁屏上的活动（用 UI 测试 `LiveActivityShots`），
  `simctl push` 不支持实时活动推送，但模拟器能收真实的 APNs sandbox 推送（`TestManualLiveActivityPush`）。
  重复的 symbol 动画（`.rotate` / `.pulse` + `.repeating`）在活动里不动（像素比对验证过），别设计「转动的齿轮」这种效果。
  灵动岛上的数字（TPM、token、负载）会占推送预算：hub 只推设备显示的项、取两位有效数字、每台最多每分钟一次。
- 机器页面的计数和列表不算「对话」tab 助手的无头会话（`MachineDetailView.agents`，和 Agents 列表一致）：它们跑在 hub 那台机器上，
  以前「服务器」卡片的「运行中」把它们也算进去了。
- 体验模式（`docs/ios.md`「体验模式」）挂在 `APIClient` / `LiveConnection` 下面（`DemoHub`），页面代码不写 `if demo`：新页面要的接口在
  `DemoHub` 里没有就是 404「体验模式里没有这项数据」，加接口时顺手在 `DemoHub` 回一份、跑 `DemoModeTests` 看 `DemoHub miss` 日志。
  演示数据改 `scripts/mock_hub.py` 后重跑 `scripts/make_demo_data.py`。会碰到电脑 / 飞书 / 邮件的操作一律不真的做（`DemoText.notSent`）。
- 弱网 / 断网不弹横幅，标题位换成「转圈 + 等待网络 / 连接中… / 更新中…」（`docs/ios.md`「连接状态」）。`.toolbar(removing: .title)`
  会连 `.principal` 的工具栏项一起藏掉：没有标题的页面要显示状态，就去掉它、只放一个平时为空的 principal 项（不会露出 navigationTitle）。
  大标题页面的 principal 项平时也要留空，否则标题出现两遍。弱网判断只认及时回来的 pong（晚到的 pong 也算慢），不然标题每次 ping 都闪一下。
- 形象（`docs/persona.md`）可能是第三方角色：角色的图和生成物只放 hub 的 `~/.jarvis/personas`，不进仓库、不进 App 包。
  换形象和换风格一样按 `AppModel.look` 重建整棵视图树，所以选择界面放在设置列表里（`PersonaPicker`），放在推出来的页面上会被重建关掉。

### 测试与验证

- 截图验证别靠 computer-use 点模拟器（Mac 锁屏时拿不到窗口，没锁时会抢用户的鼠标）：用 DEBUG 启动参数（`docs/ios.md`）直达页面再截图。
- `xcodebuild test` 会启动不带 `-pairURL` 的 App 申请通知权限，之后这台模拟器的截图都会被授权弹窗挡住，跑完测试要重启模拟器再截图。
- 模拟器里上一次启动遗留的系统授权弹窗，卸载重装 App 也不消失，要 `simctl shutdown` 重启模拟器（手动 `simctl launch` 不带 `-pairURL` 就会弹通知授权）。
- `xcrun simctl launch` 对已经在运行的 App 只是拉到前台、新启动参数不生效：截图前用 `--terminate-running-process`。
- UI 测试锁了模拟器屏幕却没解锁（模拟器上按 Home 不解锁，要上滑），下一次 `xcodebuild test` 报「test runner hung before establishing connection」。
  模拟器系统语言是英文时，系统授权弹窗的按钮叫「Allow」，UI 测试两种都要认。
- 模拟器会被 XCUITest 的 typeText 切成「硬件键盘」模式：之后软键盘不再弹出（键盘按钮显示已弹起，但屏幕上没有键盘），依赖键盘高度的测试全挂。
  这台机器没有 Simulator.app，`defaults write com.apple.iphonesimulator …ConnectHardwareKeyboard` 改不回来：`simctl create` 一台新的跑。
- XCUITest 在键盘弹起的动画中点键栏会点空（键栏正跟着键盘移动）：先等键盘出现、停一下再点。
- 按时间排的列表要有确定的次序（同一时间再按 id）：两个空间最后更新时间相同时，每次渲染顺序都可能对调，UI 测试点到的是另一行。
- XCUITest 的启动参数（`-key value`）会盖过 UserDefaults / `@AppStorage` 的读取，App 自己写进去的值读不出来：只能用来固定测试不会改的值
  （新对话上次的机器），会在界面上改的（Agents 的分组、筛选）要在测试开头用界面恢复。`-dev.openChat ""` 让 App 不打开上次的对话。
  反过来用 `-dev.openChat <会话>` 打开对话时要连 `-app.tab chats` 一起给：冷启动只在存着的 tab 是 Agents 时恢复对话，`-tab dev` 切 tab 时已经晚了，
  上一个测试停在别的 tab 时对话就打不开（2026-10-31 `LiveUpdateTests` 的看图用例在别的车道「第一步就找不到过程卡」）。只想打开某个对话用 `-openSession`。
- 看不到截图（Read 打开 PNG 什么都没有、模型说「工具没有输出」）不是目录的问题（2026-10-31 试过 `$TMPDIR`、`/tmp`、仓库里的图都一样）：
  是模型网关把工具结果里的图片丢了，只留文字。先用一张已知内容的图分别放在用户消息和 tool_result 里直接请求网关，对比两边能不能读出来；
  界面改动必须真的看到截图再交付，看不到就先修这条链路。具体网关和修法见 `CLAUDE.local.md`。
- 手动跑 `xcodebuild test` / `build-for-testing` 前先 `eval "$(python3 scripts/jarvis_config.py env)"`：配置里的 `xcode` 只经它变成 `DEVELOPER_DIR`。
  直接跑用的是系统默认的 Xcode，SDK 旧一档时报 `toolbarVerticalEdge` 之类「no exact matches in call to initializer」（2026-10-31 被当成别人的代码坏了，白查一下午）。
- 量界面的跳动 / 闪动要看画面：`simctl io recordVideo` 录下来、ffmpeg 拆帧，逐帧跟踪一条像素；只闪一帧的跳只有这样看得到。
  App 里记的位置日志可能和屏幕上的不一致（2026-10-31 日志说表头在原位，画面上整页偏了 10pt），以画面为准。
- 量滚动帧率别用 XCUITest 的拖动：它每次手势前要在 App 主线程上抓一次无障碍快照，自己就造成 40–100 ms 的长帧（每 3 秒一次，像是卡顿）。
  用 App 内的匀速滚动（`AutoScroller`，`-showFileAutoScroll`）加帧计数（`FrameMeter`），从一个空闲的会话打开（在跑的会话在后面每秒重画）。
