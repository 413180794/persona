# Jarvis → Persona：兼容迁移

## 不变的身份

产品统一叫 **Persona**，服务端新入口是 `personad`；旧 `jarvisd` 命令、构建入口和发布产物继续可用。改名不是重装：现有 Bundle ID / applicationId、App Store 记录、APNs 和钥匙串 / App Group 身份、数据库账户与库名、Docker 项目与数据卷名称、服务管理器的既有单元身份、公开安装与中继地址都保留。不要为改名字重建数据库、重新配对手机或注册第二个 App。

形象仍然使用既有 `persona` 数据模型与 `personas/` 目录，已有形象 ID 不改。产品入口使用 `PersonaApp` 等完整名字，避免与形象模型混淆。

## 源码位置、构建入口与兼容名字

源码的新名字与已安装产品的身份分别维护。#729 的 Apple 源码 / 构建入口、#750 的连接器模式监听启动竞态修复、#746 的 Android 链接兼容和 #756 的原生品牌 / 输入兼容均已合入。文档复验先后使用 `b5a902f98eb30ddc6ab47e109039b14bc01f9549`、`b8cf3a10d475c12577c43f1c524332b8c1aa8bd8`（包含 #753、#758、#761、#745、#760 和 #771）及 #773 合入后的 `4768b019ac665207ba0e5a7a7f81cddb6879b6c7`。本轮最终文档基准固定为 `09584f34fe6eb29f91ddc2a3c2a8df94017cb3ab`，再包含 #786 的链接输入与静态防回退检查。源码状态不代替 App 构建、真实升级或产物检查；固定快照的全量 open PR 只读审计及后续 08:59 快照的 11 项 delta 复核已完成，见 [PR 改名残留审计记录](rename-pr-audit.md)。后者主线固定为 `0e02f0f7fc6aec48cc73197642917c382294ae81`，未重审未变化 PR，也不抹去既有问题；未修项协作仍待完成，最终冻结尚未开始，不能据此宣称整个 #277 完结。

| 用途 | 推荐入口 | 保留的兼容入口 / 身份 |
| --- | --- | --- |
| Go module | `github.com/nwverse/persona/server` | 这是源码 import 路径；不据此改数据库、协议或服务单元 |
| hub / node / hook / MCP 程序源码 | `server/cmd/personad` | `server/cmd/jarvisd` 指向同一实现；`personad` / `jarvisd` 均保留 |
| Telegram 连接器程序源码 | `server/cmd/personad-telegram` | `server/cmd/jarvisd-telegram` 是旧入口别名 |
| 微信机器人程序源码 | `server/cmd/personad-wechat` | `server/cmd/jarvisd-wechat` 是同样的别名（安装后的程序名是 `jarvisd-wechat`，旁边有 `personad-wechat`） |
| 配置脚本 | `scripts/persona_config.py` | `scripts/jarvis_config.py` 是指向新实现的软链；配置格式、签名值不变 |
| 服务运行根解析器 | `scripts/persona_identity.py --service hub\|node\|link` | 按服务角色恢复已安装根目录；不读取构建配置或凭据，规则见下文 |
| DEBUG App 命令工具 | `scripts/app/personactl.py` | `scripts/app/jarvisctl.py` 仍是同一实现；命令语义和旧缓存位置不变 |
| iOS App 源码 | `apple/Persona/` | 旧 `apple/Jarvis/` 是历史源码位置，不承诺为整个源码目录创建别名 |
| Watch / Widgets / Notify 源码 | `apple/PersonaWatch/`、`apple/PersonaWidgets/`、`apple/PersonaNotify/` | `apple/Shared/`、`apple/LiveActivity/` 仍共用；系统身份见下表 |
| 原生 Mac / 测试源码 | `apple/Mac/`、`apple/PersonaTests/`、`apple/PersonaUITests/`、`apple/PersonaMacTests/` | Mac 壳仍在 `apple/Mac/`，不是把每个路径都改成产品名 |
| XcodeGen 工程 | `apple/Persona.xcodeproj` | `apple/Jarvis.xcodeproj → Persona.xcodeproj`；先生成工程才能使用入口 |
| scheme / target | `Persona`、`PersonaMac`、`PersonaWatch` | 旧 `Jarvis`、`JarvisMac`、`JarvisWatch` scheme 指向新 target；scheme alias 不代表旧 target 仍存在 |
| 扩展 / 测试 target | `PersonaWidgets`、`PersonaNotify`、`PersonaMacNotify`、`PersonaTests`、`PersonaUITests`、`PersonaMacTests` | `-only-testing:` 使用新测试 target；`@testable import Jarvis` 仍按旧 module 编译 |
| 连接器命令 | `persona-link`（安装器建的别名；以这个名字运行或带 `PERSONA_LINK_NAME=persona-link` 时提示里自称 persona-link，#1159） | `jarvis-link` 是真实文件、下载名、systemd / launchd 单元和 Mac App helper 的名字，都不变；hub / 管理页给出的可复制命令暂留 `jarvis-link`（旧版电脑没有新名字），带别名的版本发出一段时间后再换 |
| 菜单栏 App 的 Swift package target | `PersonaLink` | 包装器的旧 `JarvisLink` 进程路径、helper 和服务身份保留；它与原生 Mac App 是两个产品入口 |

更新当前命令示例时使用新路径；历史 SHA、当时的源码行号、日志、测量和验收结论保留。尤其不能把「新目录存在」写成真机升级、已保存快捷指令、真实推送或发布签名已经验证。

#786 的 `scripts/test/test_apple_rename.py` 加入旧源码目录防回退：新文件不能重新落进 `apple/Jarvis{,Watch,Widgets,Notify,Tests,MacTests,UITests}` 或 `apple/PersonaLink/Sources/JarvisLink`（#883 起菜单栏 App 在 `apple/PersonaLink/`，顶层 `mac/` 也不能再出现），但旧 `apple/Jarvis.xcodeproj` 工程软链仍保留。Android 的 MainActivity / PairingLinks 改为共用完整链接判定，`scripts/test/test_persona_identifiers.py` 的静态 guard 同步验证这条调用链。源码目录禁用规则不影响旧 module、Intent / ActivityAttributes 或系统身份。

## 必须保留的系统与持久化身份

| 层 | 保留内容 | 原因 / 检查方法 |
| --- | --- | --- |
| Apple module | 主 App、Watch、Mac 保留 `Jarvis`；Widgets 保留 `JarvisWidgets`；Notify / MacNotify 保留 `JarvisNotify` | target 和产物名字可以变，通知主类如 `JarvisNotify.NotificationService`、AppIntents metadata 不能因此失效；检查实际 bundle，不只 grep 源码 |
| Swift 系统类型 | `AskJarvisIntent`、`JarvisStatusIntent`、`JarvisShortcuts`、`OpenJarvisAssistantIntent`、`OpenJarvisAgentsIntent`、`AskJarvisControl`、`JarvisAgentsControl` | 是快捷指令 / Siri / Control 的既有类型身份；文件可改名，真实类型不能当纯文案替换 |
| ActivityKit / APNs | `JarvisActivityAttributes`、既有 Widget kind、push-start 的 `attributes-type` | hub 实际按该类型寻址；不能只改 Swift struct 或单独改 server 字符串 |
| 安装与签名 | Bundle ID、applicationId、App Store 记录、App Group、钥匙串组、签名 profile、通知扩展身份 | 继续升级同一个 App，沿用配对、公钥和推送权限；不注册第二套身份 |
| Android 序列化 | `AppTab.Jarvis`、已有 `@SerialName`、rawValue 和存储键 | 新源码包为 `persona.android` / `persona.*`，不让 kotlinx.serialization 的默认完整类名变成新的 wire / 数据库值 |
| Android 系统组件 | 旧 Activity / alias、receiver、HubService、ComponentName、PendingIntent、WorkManager Worker 的持久化类名、`Theme.Jarvis` | 老通知、排队任务和 OS 注册仍要能找到组件；不能只看 Gradle 编译或改新 manifest 类名 |
| 线上协议 | HTTP / WebSocket 字段、`jarvis-runtime.v1`、`X-Jarvis-Peer-Source` 等旧头、MCP 工具全名、`jarvis-context` 等消息标签 | 混合版本继续互通；必须按各协议显式兼容，不批量替换 `jarvis` 字符串 |
| 缓存与数据 | UserDefaults / AppStorage / StateCache / ChatCache 键、缓存目录版本、数据库角色 / 库 / 容器 / 数据卷、machine UID、形象 ID | 改品牌不生成第二份账户、数据库或个人数据；根目录的兼容另见下节 |
| 服务与公开地址 | systemd / launchd 单元、helper 进程、安装 / 更新 / 中继 URL、联系邮箱 | 显示名称可以更新，真实运维 / 外部身份按原配置保留；不在文档中放个人值 |

私有 runtime 的 npm `package.json.name` 与自动更新包 ID 不是同一身份。#753 已将源码私有包名改为 `persona-runtime-sdk`、`persona-dsh-runtime`、`persona-pi-runtime`；但 DSH / pi 的更新清单、安装目录和已安装 loader 仍使用 `jarvis-dsh-runtime` / `jarvis-pi-runtime`。SDK 的相对源码引用与 `jarvis-runtime.v1` 子协议也不能按 npm 名称盲改。运行时品牌、#773 的脚本 / PI / 新变量补洞和 #786 的链接输入均已合入，不代表既有未修迁移项已解决或冻结发布已完成；不能据此宣称整个 #277 已无残留。

## URL scheme：接收兼容，生成保旧

下面列出的迁移入口在此固定基准中已接收新旧 scheme。Apple 的 `CFBundleURLTypes`、App 的 URL / 配对解析器、Android 的 intent-filter 和导航解析器，以及 Web / 管理界面的手动输入解析仍要逐入口核验；不能只改一个 `scheme ==`，也不能由某个客户端通过就推断所有入口或真机升级均已验收。

**固定基准 `09584f34` 的集成边界**：

| 入口 | 已合入源码中的行为 | 本轮文档验收边界 |
| --- | --- | --- |
| Apple iOS / Mac 注册与配对输入 | #756 已在 `apple/project.yml` 注册 `[jarvis, persona]`，`apple/Shared/APIClient.swift` 的配对 parser 和 `apple/Mac/App/MacOpenURL.swift` 接收新旧 scheme | 本轮核对已合源码；#756 的构建、全单测和截图验收由原生车道执行，不算本文档车道重跑 |
| Android 配对 / open | #746 已加入新旧 scheme 的 manifest 注册、配对 parser 和路由输入；#758 补齐 canonical Activity 的 Debug metadata 读取及完整配对链接在权限 / 联网前的解析；#770 统一手填配对输入，完整链接要求 `://`，不把 `persona:端口` 这样的普通地址当配对链接；#745 更新人类可见品牌。输出通知路由仍用 `jarvis://open` | 本轮对齐已合源码状态；各 PR 的 Android 构建、真实截图与测试记录属于对应车道，本文档车道不冒充重跑 |
| Web 会话链接 / 管理输入 | #786 的 `web/src/lib/links.ts` 会话链接与 `web/src/admin/logic.ts` connector 输入均接收 `persona://` / `jarvis://`，peer 表单与 mock 的输入检查也已同步 | 本轮核对已合源码及静态契约，不声称重跑 Web 全套测试或浏览器验收；生成仍保留旧 scheme |
| hub / 连接器的 link、pair、connector、peer 输入 | #786 的 `server/internal/link/client.go`、`server/internal/link/passive.go` 与 `server/internal/hub/peer_pair.go` 接收新旧 scheme | `jarvis://link` / `jarvis://pair` / `jarvis://connector` / `jarvis://peer` 的生成保旧；Go 全套验证归独立车道，不由文档检查替代 |

#758 的 Android Debug metadata / 完整链接包装、#770 的手填共用判定、#773 的环境 / 脚本兼容和 #786 的最终链接输入检查均已合入。#761 的匿名官网截图和 #760 的测试误报修复也已合入，各自证据仍归对应 PR；本文档车道没有刷新截图或重跑业务全套测试。#786 的 canonical Go 重跑及独立失败定位已完成，归属和 skip 边界见下文；不能把文档检查说成 Go 全套通过。固定快照审计和后续 11 项 delta 复核已完成，未修项协作仍待处理，最终冻结和发布未完成。

hub / 连接器 / 消息中的配对、会话、互联等链接仍生成 `jarvis://…`，让旧 App 继续可用。新 scheme 是新增的接收别名，不是全量改写已有 URL；`jarvis://pair`、`jarvis://session`、`jarvis://peer`、`jarvis://connector` 等原示例和历史 QR 内容保留。未完成双注册或双解析的入口要列为未验收，不能在文档中宣称已支持。

品牌文本的解析也有输入兼容：会话来源 / 运行时标题中识别产品前缀的 parser 要同时认识旧 `Jarvis` 和新 `Persona`。只改 Persona 自己生成的标题或演示默认名称，不重写用户命名的会话、用户消息、人名，或外部 bot / 联系人的名字。经验见 [改名与兼容身份](lessons/rename.md)。

## 运行目录：一个存储，两种入口

默认通过 `~/.persona` 访问运行数据；`~/.jarvis` 保持兼容。启动 hub / node 和安装器仅创建缺失的目录别名，不递归复制，不搬动已有目录，也不覆盖现有软链。

- 只有旧目录：原地保留，通过 `~/.persona → .jarvis` 接入；已有 Claude / Codex 的会话目录、技能和缓存路径不变。
- 全新安装：也采用 `~/.persona → .jarvis`，实际目录仍是 `.jarvis`。这是有意的兼容布局：旧版助手用工作目录识别会话；保留物理路径可使新装后的回退同样可用，不只是让两边都找得到文件。
- 只有已有的新目录：保留它，创建 `~/.jarvis → .persona`。不搬走用户自己的数据。
- 两个入口已经指向同一个目录：优先返回新入口。只查询 socket 不写目录；SDK 安装包前使用与 Go / 安装器相同的初始化，不能让递归 mkdir 抢先把新路径建成第二份存储。别名创建时遇到竞争要核验两边是否同一对象，不能仅看到 EEXIST 就宣告成功。
- 文件系统不支持软链或用户 HOME 不允许创建新别名：原目录存在且可用就继续使用原存储，不因为辅助别名失败让旧部署停机；安装器的命令别名可降级为薄转发脚本。权限变化、循环、不可用目标仍报错，不生成另一份空存储。
- 两个独立目录都有内容：不自动混合，也不覆盖；默认继续旧存储，防止旧部署升级时悄悄切成另一套 token / machine UID / 知识库。确实要使用另一套时，明确指定 `PERSONA_HOME`。
- 软链指向外部目录：目标存在就原地使用；目标不可用时拒绝启动新存储，先恢复目标。新旧默认名称互指的单向悬空软链可以补齐目标，循环软链不能。

`PERSONA_HOME`（兼容 `JARVIS_HOME`）指定运行根目录，可用绝对路径、`~/…` 或相对于该用户 HOME 的路径；先统一做词法规范化，含 `..` 时安装器与 Go / SDK 不能分别按软链路径解释。不同环境可各用自己的根目录；自定义根不会改用户默认目录的软链。node 的 `data_dir` 是可选的机器信息字段，新 hub 据此使用那台机器的实际助手目录；老 node 未上报时仍用旧默认路径。新旧助手目录都识别为助手，普通项目名为 `assistant` 不算。

node socket 使用同一个存储中较短的旧路径，避免新名字多一个字符使刚好达到 Unix socket 路径长度上限的旧 HOME 升级后无法监听；新路径仍通过目录别名访问同一 socket。

不要通过复制一份 hub.json 来迁移：它会让两边的知识库、machine UID、socket、更新状态和加密密钥逐渐分叉。数据库继续是原来的数据库，目录别名使 token 和 `secret.key` 也是原来的文件。

## 菜单栏 Link 的运行目录

#771 已让 `apple/PersonaLink/Sources/PersonaLink/LinkStatePaths.swift`（#883 前在顶层 `mac/`） 与 helper 使用同一运行目录。菜单栏进程显式提供 `PERSONA_HOME` 或 `JARVIS_HOME` 时按新名优先；没有显式指定时，从旧 `com.jarvis.node.plist` 的 `EnvironmentVariables` 读取已安装服务的根目录。新旧默认入口指向同一对象时使用新入口，两份独立目录存在时仍用原存储。路径按词法规范化，不凭产品改名复制或合并数据。

此修复不改旧 launchd 单元、`JarvisLink` 可执行路径或 helper 身份；也不证明脚本 / PI / 新环境变量的其他调用方已全部补齐。本轮只更新已合源码状态，菜单栏 XCTest / 预览 / 截图属于 #771 的验证记录。

## 部署脚本按服务恢复运行目录

#773 引入的 `scripts/persona_identity.py` 只恢复运行身份，不读构建配置或 hub.json；#837 已补齐外部环境文件的 HOME-only 恢复，merge 为 `62039ebc4d17285a4ffd39e3631e96e18e65c70d`。只提取 `PERSONA_HOME` / `JARVIS_HOME`，其他字段不返回、不记录、不用于认证；没有 HOME 的其他设置不再仅因文件存在而阻止恢复原默认存储。部署 / 测试脚本用 `--service hub`、`--service node`、`--service link` 选择对应服务，避免同机 hub 与 node / Link 使用不同根时借错目录：

- 环境里显式定义 `PERSONA_HOME` 或 `JARVIS_HOME` 时，直接使用该环境；新名优先，包括空值，不再从已安装服务借一个根。
- systemd 从 `XDG_CONFIG_HOME/systemd/user`（未设置时为 `~/.config/systemd/user`）读取对应旧单元。hub 只查 `jarvisd.service`；node 先查 `jarvisd-node.service`、再查 `jarvis-link.service`；link 顺序相反。没有 systemd 单元时，node / link 可按对应角色查询原 launchd plist，不变更单元身份。
- **旧服务存在，但 unit 和外部环境文件都没有 HOME 时，继续原默认存储**；找到该角色单元就不借其他角色或后备服务的根。不会因为同机另一个服务有自定义根，悄悄把旧服务迁过去。
- 解析主 unit 与按文件名排序的 `.d/*.conf`，支持 `Environment=` 清空重置、后续覆盖、行续接，以及 `%h` / `%%`。只保留 `PERSONA_HOME` / `JARVIS_HOME`，不提取其他环境值。自动模式遇到不同独立根会拒绝，要求定向选择服务；两个根经 SameFile 校验指向同一默认存储时不误报冲突。
- `EnvironmentFile=` 支持按顺序覆盖同名 unit 环境、可选文件的 `-` 前缀、通配符以及空赋值重置文件列表，只提取新旧 HOME；不执行 shell 展开，不返回或记录其他字段。`UnsetEnvironment=` 在全部文件之后移除匹配变量，空赋值重置移除列表。必需文件缺失或根字段解析错误时停止；显式 HOME 可以直接指定实际根，不能因无法恢复而切到另一份空存储。
- 查询默认不创建目录；`--prepare` 才初始化缺失的默认别名，保留既有数据、独立根和自定义路径。`--prefix` 将解析器和指定脚本组合给远端，在远端解析并传递新旧根变量，不把本机根误传给远端，也不在生成文本中展开凭据。

## 环境变量

`PERSONA_*` 是新写法，所有已有 `JARVIS_*` 仍识别。**新变量已定义时优先，包括显式空值**，防止空的新 token 又回退到旧凭据。Go 调用统一经 `internal/identity` 读取；给助手 MCP / 插件子进程的受控变量同时写新旧两名，并去掉重复项，避免继承的旧 session ID 或新 session ID 抢走本次设置。

构建配置优先 `PERSONA_CONFIG`，再 `JARVIS_CONFIG`，默认 `~/.config/persona/config.json`，没有新文件时读旧 `~/.config/jarvis/config.json`。读已有默认文件时尽量补齐缺失的目录软链；两个已有配置文件并存时新文件优先，不覆盖旧文件。自定义配置文件不创建全局别名。

配置脚本每次都按选中的配置重算派生的 hub、bundle、团队等导出值，同时导出新旧两名；不会把上一次 `env` 导出的值当作覆盖，否则同一 shell 切换环境会仍操作旧部署。对其他运行时环境变量，只将显式 `PERSONA_*` 用 shell 变量引用桥接到旧名，不打印其值，也不自动为仅有旧名的变量制造新名覆盖；插件 token 等不会出现在导出脚本中。xcconfig 同时保留新旧构建设置，配置里的签名与 App 身份不改变。

运行时 SDK、DSH 和 pi 也认识新旧 socket / HOME 环境变量。pi 的新配置文件叫 `persona.json`，没有新文件时仍读 `jarvis.json`。#773 补齐 `PERSONA_PI_HOST` / `JARVIS_PI_HOST` 和 echo daemon 的 `PERSONA_DAEMON_TOKEN` / `JARVIS_DAEMON_TOKEN` 新名优先读取。App 命令 CLI 的 bundle fallback 优先 `PERSONA_BUNDLE_ID`、兼容 `JARVIS_BUNDLE_ID`；既有 `JARVISCTL_*` 名、命令参数和 `~/.cache/jarvisctl` 保留，不凭工具改名虚构一套 `PERSONACTL_*`。运行时包的旧更新 ID、session 条目与热加载状态身份保留，保证已安装插件可以接收新版而不需要重装。

## 分阶段的契约

对外名字、源码模块 / 类型 / 目录、存储兼容分 PR 落地。旧 URL scheme、HTTP 头、协议字段、MCP 工具全名、缓存 key、序列化类型名属于兼容契约，不做字符串盲替换。新的可见入口可以同时接收新旧名字；需要旧 App / 旧 hub 读取的写入格式继续保留旧表示，直到双方明确协商了新版本。

源码包重命名尤其注意 Android 序列化默认的完整类名、旧通知的 ComponentName / PendingIntent、WorkManager 的持久化 Worker 名、Swift rawValue 与归档缓存。源码换名字，不等于持久化名字可以一起换。

## 验证与收尾

**已合代码车道的验证，不是本文档车道重跑**：#786 最终记录 108 项 Python 迁移 / 脚本测试、448 项 Web 测试及生产构建通过；Go 在短 canonical `/private/tmp` 测试根下退出 0，2,167 个 test / subtest pass 事件、72 个明确 skip、0 fail。Postgres / 平台 / fixture 的 skip 不算真实 Postgres 集成通过。最初的 Codex / 端口失败日志保留：Codex fixture 直接传 `/tmp` 别名绕过正常 Home canonicalization，端口用已合 #728 处理 lsof 的有效输出；独立对照已定位，不删除身份比较，也不延长 timeout。该结论不推广成任意 Home 不匹配都只是测试问题。

前一轮文档只重验当前源码 / 兼容 guard、隔离服务根回归、Markdown 链接和隐私，不冒充重跑上述 Go / Web 全套或原生截图。固定 `09584f34` 的 25 个 open PR 只读审计及固定 `0e02f0f7` 的 11 项 delta 复核已完成；完整历史表、迁移项修复、#764 的继承残留、#837 已合支持和安全 / 发布边界见 [PR 改名残留审计记录](rename-pr-audit.md)。此次追加只做轻量引用和隐私检查，不重跑对应车道的审计或测试。后续 PR 协作与未修迁移项仍由原负责人继续。数据库 URL query 覆盖的隔离问题已由 #879 修复并独立匿名复验，merge 为 `761ce5473289be03808f7d941d8c17f10813cbaf`；这不是 live Postgres 验收。最终冻结必须包含 #812 / #879，并在新隔离启动器下通过完整验证后再部署 / 上传。当前本地实现、固定验证与剩余边界见 [验收记录](rename-acceptance.md)。下面保留发布验证清单，不以文档改动解除生产操作暂停：

- 临时 HOME 下覆盖新装、旧目录、新目录、外部软链、悬空软链、循环、两份独立存储、重复启动、并发首次启动。
- 同一个 token / hub.json 与 node socket 经两种路径访问，内容与文件身份相同；自定义环境之间不串数据。
- 新旧变量单独设置、一起设置、显式空值、子进程重复 session ID；配置导出不打印其他凭据。
- 服务端真实 Postgres 全套测试、各运行时单测与实际新旧入口构建；各客户端编译 / 测试和深浅色截图。
- 所有本地迁移完成后逐个重查 open PR，重点查新建的旧目录文件、旧 module imports、包名、类型名和文案；不要把刻意保留的兼容契约当成遗漏删掉。

用户允许本地验证通过后合并、CI 环境故障不阻塞，但必须记录实际故障与未完成的验证，不能声称失败的测试通过。最后须在隔离暂停解除、未修项处理和冻结验证完成后，从已合并的干净版本统一部署和发布，不能用中间半迁移版本发版。#837 的本地验证与合入不解除生产操作暂停，也不证明完整事故影响已恢复。
