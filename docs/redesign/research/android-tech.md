# Jarvis Android 技术选型调研笔记

- 调研日期：**2026-10-30**。版本号都是当天实际查到的，每节末尾附来源 URL。
- 标记：✅ 已查证（有来源）；💭 经验判断 / 推断（未逐条查证，引用前要复核）；⏳ 待补。
- 范围：只做调研和设计，不写代码、不动仓库。

---

## 0. 背景：Jarvis 对 Android 端的要求（摘自 README / CLAUDE.md / docs/ios.md / docs/terminal.md / docs/protocol.md）

- 手机只连自建 hub（Go，`jarvisd hub`，默认 `:18660`），HTTP + WebSocket，Bearer token（每台设备一个 `jd_…` token，手表是 `answer` 角色）。
  hub **常在内网 / VPN 里用明文 HTTP**。JSON 字段一律 snake_case，时间 RFC3339。
- 四个 tab：Jarvis（无头 Claude 助手，逐字流式）/ 开发（Agents 列表 + 对话页）/ 收件箱（飞书 + 邮件，飞书卡片照 lark-connector 渲染、飞书文档原生阅读器）/ 设置。
- 对话页难点：倒置聊天列表（最新在偏移 0），几百步的工作过程每步都是独立懒加载行，轮次折叠（工作行、唤醒、recap、排队消息），
  Markdown（表格、代码块）、工具卡片、红绿 diff（横滑）、图片按需加载 + 全屏查看 / 编辑、长按操作条、分享卡片、子 Agent、
  提问 / 计划 / 授权 / 终端对话框交互面板、发送中气泡 + 离线 outbox（`client_msg_id` 去重，落盘）。
- 终端：herdr observe 帧（VT 子集：CUP、ED、SGR、DECTCEM、2026 同步输出、OSC 8）→ 自写网格（宽字符两格、框线画成线）→ 自绘；
  手机接管（按手机列 × 行）、键栏（粘滞 / 锁定 / 按住的修饰键）、直接打字（IME、拼音 marked text）、滚轮转发、选择复制。
- 链接：App 内底部浮层，打开即铺满，可下拉到 60% 且背后对话可滚动可输入；WebView 预加载池（每条回复预加载前 3 个链接，常驻 6 个，LRU）；
  网页里选中文字「AI 修改 / 作为上下文」。
- 机器监控曲线（1/6/24 小时、断点不连线、按住看时刻）；二维码配对；推送（按钮、需解锁的「允许」、直接输入意见）；
  锁屏实时活动 / 灵动岛（Fleet 汇总：需要你 / 工作中 / 完成未看）；Apple Watch；Siri / 快捷指令（问 Claude、状态、批准第一个待处理）。
- 手机数据工具 `phone_*`：通讯录、日历（读 + 建）、提醒（列 / 建 / 完成）、位置（一次性）、健康（按天汇总：步数、距离、活动能量、心率、静息心率、睡眠、体重）、
  照片（最近元数据 + 取图）、设备信息、剪贴板读写、本地通知；写操作先弹确认。
- 5 套界面风格（液态玻璃默认、Claude、黏土、Island、飞书），token 化主题；状态色一张表不随风格变。
- 连接状态放在标题位（等待网络 / 连接中… / 更新中…，1.5 秒去抖、ping 5 秒判弱网、45 秒无消息判死）；后台推拉同步（`revs` 事件 + `after_seq` 增量）；磁盘缓存先画后刷。
- 质量标准：按 App Store 榜首水准；每个界面深浅色截图检查；DEBUG 启动参数直达页面截图；Release 包不得含调试开关字符串（`release.sh` 用 `strings` 查）。

## 本机环境（协调者 2026-10-30 已核实）

- Apple Silicon Mac；Android Studio **2025.3**（自带 JBR 21）；SDK 平台 android-34 / 36.1 / 37.0 / 37.1；build-tools 37.0.0；
  arm64 系统镜像 API 35 与 37.x；已有几台 AVD；Homebrew 有 openjdk@17。
- 💭 注意：当前稳定版 Studio 已是 **Rabbit 2（2026.2.2）**，本机 2025.3 落后约 3 个大版本。命令行 Gradle 构建不受 IDE 版本影响，
  而 AGP 9.5 需要 Studio Rabbit 2，Panda（2025.3.x）只支持到 AGP 9.0–9.2（兼容表见第 10.4 节）——要用 IDE 就得升级。

---

## 1. 总体路线 ✅（结论已定，细节见下）

### 结论（推荐）

**Android 用纯原生 Kotlin + Jetpack Compose；iOS 保持 SwiftUI，不引入 Kotlin。两端的「行为一致性」靠一套共享的 golden 测试夹具
（JSON 输入 → 期望的展示行 / 状态），而不是共享代码。** Android 的核心模块（协议模型、连接、ChatStore、轮次折叠、outbox、同步）
写成**不依赖 android.* 的纯 Kotlin 模块**（kotlinx.serialization / coroutines / kotlinx-datetime / okio），日后想走 KMP 共享时只需改 Gradle 插件，
不用重写。等 Swift export 到 Beta、且 Android 端跑稳之后，再评估把这些模块给 iOS 用。

### 三条路线对比

| 路线 | 结论 | 理由 |
| --- | --- | --- |
| A. 纯原生 Kotlin + Compose（推荐） | ✅ 采用 | Android 体验最好（Material / 系统能力 / 性能工具都一手）；iOS 4.2 万行 SwiftUI 不受影响；构建链最简单，多个 AI agent 并行时互不拖累；Compose 与 SwiftUI 的心智模型接近，AI 照着 Swift 实现翻译成本低 |
| B. KMP 共享核心（协议 / 模型 / 同步 / transcript 折叠） | ⏸ 暂缓，模块边界先按它设计 | 收益真实（折叠、续传、去重、outbox 这些坑多的逻辑只写一遍），但 iOS 侧要把已经踩平坑、有测试的 Swift 数据层换成 Kotlin：Swift export 仍是 **Alpha**（2.4.0 起，官方「breaking changes are expected」）；走 Obj-C 导出 + SKIE 可用但多一个跟 Kotlin 版本走的第三方编译器插件；Swift 6 严格并发下 Kotlin 导出类型不是 Sendable，要包一层；Kotlin/Native 编译拖慢 Xcode 构建；手表、Widget / 实时活动扩展也要链接 K/N 框架（扩展有内存上限）；跨语言调试 |
| C. Compose Multiplatform 连 UI 一起共享 | ❌ 不采用 | iOS 要 App Store 榜首水准 + iOS 26 液态玻璃：CMP 没有官方的 Liquid Glass；用 UIKit 原生玻璃叠在 Compose 上（1.10 的 `placedAsOverlay`）一遇到滚动内容、弹窗、浮层就失效，只能「SwiftUI 里嵌 Compose」反过来做；CMP 1.11 的原生 iOS 文字输入还是实验性 opt-in。等于为了共享 UI 牺牲 iOS 的手感，和要求相反 |

### KMP 共享核心时 Swift 侧接入成本（✅ 2026-10-30 现状）

- **Swift export**：Kotlin 2.4.0（2026-06）起官方定为 **Alpha**；支持 suspend → `async`、Flow → `AsyncSequence`（`.asAsyncSequence()`）、
  sealed → Swift enum（`.sealedType()`，2.4.20 加入）、enum → 原生 Swift enum、跨语言继承；**泛型基本不支持**（擦除到上界）、
  `List/Set/Map` 的子类型导出时被忽略、只支持「direct integration」接入 Xcode、没有迁移工具。2.4.20 能自动生成 SwiftPM 的 `Package.swift`。
  没找到 Beta / Stable 的时间表。
- **SKIE**（Touchlab，建立在 Obj-C 导出之上）：最新 **0.10.15**（2026-09-25，加入 Kotlin 2.4.20 支持），比 Kotlin 2.4.20（09-07）晚约 3 周——
  每次升 Kotlin 都要等 SKIE 跟上，是 KMP 路线上的版本锁。
- **Compose Multiplatform**：1.11.0（2026-05-13）稳定，原生 iOS 文字输入（实验）、并发渲染默认开；1.12.x（2026-08/09）。无官方 Liquid Glass。
- 💭 若将来要共享，建议顺序：先共享「协议模型 + 轮次折叠（纯函数）」，iOS 侧用一个 Swift facade 包住（不让 Kotlin 类型渗进 SwiftUI 视图），
  WebSocket / 缓存 / 后台仍各自用平台实现；手表和扩展继续用 Swift 的薄模型。

### 风险

- 两套实现会漂移：用共享 golden 夹具兜住（建议在仓库根目录放 `fixtures/`，XCTest 和 JUnit 都读同一批 JSON），hub 侧已有的 transcript fixture 可复用。
- 💭 另一个降重方向：把与 UI 状态无关的「轮次分段 / 回复挑选（pickAnswerAcrossWakes 这类）」上移到 hub（Go）算好下发，两端都变薄；代价是协议变更和旧 App 兼容。

来源：
- Swift export 文档（Alpha、支持矩阵）：https://kotlinlang.org/docs/native-swift-export.html
- Kotlin 2.4.20 新特性：https://kotlinlang.org/docs/whatsnew2420.html ；2.4.0 发布博客：https://blog.jetbrains.com/kotlin/2026/06/kotlin-2-4-0-released/
- SKIE releases：https://github.com/touchlab/SKIE/releases
- CMP 1.11.0：https://blog.jetbrains.com/kotlin/2026/05/compose-multiplatform-1-11-0/ ；CMP 液态玻璃分层问题：https://medium.com/mateedevs/part-2-liquid-glass-components-in-compose-multiplatform-1-10-0-beta01-20f78471d8f7

---

## 2. 工程结构与版本 ✅

### 当前版本（✅ 2026-10-30）

| 组件 | 版本 | 备注 |
| --- | --- | --- |
| Kotlin | **2.4.21**（2026-10-08） | 2.4.0 = 2026-06-03 语言版本；2.5.0 计划 2026-12；从 2.4 起每条线 18 个月安全支持 |
| AGP | **9.5.0**（2026-10，当前稳定；9.4.0 2026-09；9.6 预览中） | 要求 Gradle ≥ 9.6.0、JDK 17、Build Tools 36.0.0；最高支持 API 37；AGP 10 会强制新 Variant API。IDE 要 Studio Rabbit 2 才支持 9.5（见第 10 节） |
| Gradle | **9.8.2**（2026-10-19） | |
| Compose BOM | **2026.09.00** | Compose ui/foundation/runtime 1.12.1、material3 1.4.0、material3-adaptive 1.3.0、material-icons 1.7.8 |
| material3（Expressive） | 稳定 1.4.0；**1.5.0-beta02**（2026-10-21） | 1.4.0 上 Expressive API 还要 `@ExperimentalMaterial3ExpressiveApi`；1.5.0 alpha20–26 把 SplitButton、FloatingToolbar、ButtonGroup、Flexible TopAppBar、列表项、SearchBar、BottomAppBar 等逐个转正 |
| Android Studio | 稳定 **Rabbit 2（2026.2.2）** | 本机装的是 2025.3 |
| Android 平台 | Android 17 = **API 37**（2026-06-16 稳定，QPR1 2026-09-15，含 37.1 minor SDK） | |

### AGP 9 必须知道的变化（✅）

- **内置 Kotlin**：不再 apply `org.jetbrains.kotlin.android`（与新 DSL 不兼容）；AGP 9 运行时依赖 KGP ≥ 2.2.10。
- **新 DSL 默认开**，旧 `applicationVariants` 等改用 `androidComponents.onVariants`；`android.newDsl=false` 的退路 AGP 10 取消。
- **KMP**：KMP 模块必须用 `com.android.kotlin.multiplatform.library`，不能和 `com.android.application` 放同一个子工程（App 要单独一个模块）。
- `targetSdk` 未设时默认等于 compileSdk（建议显式写）；Java 默认 11；R8：strict keep rules（`-keep class A` 不再隐含保留无参构造）、
  优化的资源收缩默认开、只允许 `proguard-android-optimize.txt`。

### 模块化（✅ Now in Android 的做法 + 💭 Jarvis 的切法）

- Now in Android（NiA，官方样板）：`build-logic/`（included build 里的 convention plugins）+ `app`、`app-nia-catalog`（组件目录 App）、`benchmarks`（Macrobenchmark + 生成 Baseline Profile）、
  `core/*`、`feature/*`、`lint`、`sync`、`ui-test-hilt-manifest`；Hilt；不用 mock 库（全用 fake + Hilt 测试 API）；Roborazzi 截图测试（`recordRoborazziDemoDebug` / `verifyRoborazziDemoDebug`，参考图在 CI 的 Linux 上录）；
  `demo` / `prod` 两个 flavor（demo 用本地静态数据）；Compose 编译器报告 `-PenableComposeCompilerMetrics=true -PenableComposeCompilerReports=true`；根目录有 `compose_compiler_config.conf` 和 `AGENTS.md`。
- 💭 Jarvis 建议的模块（按「多个 AI agent 并行、各改各的模块」切）：

| 模块 | 内容 | 依赖 Android？ |
| --- | --- | --- |
| `build-logic/convention` | `jarvis.android.application`、`jarvis.android.library`、`jarvis.android.compose`、`jarvis.jvm.library`、`jarvis.hilt`、`jarvis.screenshot-test` 等插件 | — |
| `core:model` | 协议数据类（Machine / Session / Item / Interaction / FleetState …），kotlinx.serialization | 否（纯 Kotlin，KMP-ready） |
| `core:protocol` | WebSocket 信封、续传（since_seq / epoch+seq）、功能协商 | 否 |
| `core:chat` | ChatStore 合并规则、轮次折叠（工作行、唤醒、recap、排队）、展示行派生 | 否（golden 夹具测试的主战场） |
| `core:terminal` | VT 子集解析 → `TerminalGrid`（宽字符、框线） | 否 |
| `core:network` | OkHttp 实现的 `HubApi` / `HubSocket`、连接状态机、本地网络权限检查 | 是 |
| `core:data` | Repository、Outbox、后台同步、磁盘缓存 | 是 |
| `core:database` / `core:datastore` | Room / DataStore（第 5 节） | 是 |
| `core:designsystem` | 主题 token、5 套风格、玻璃 / 软块表面、状态色表、Chip / CountBadge | 是 |
| `core:ui` | Markdown、代码高亮、diff、等宽文本、图片查看器、链接浮层、WebView 池 | 是 |
| `core:phonetools` | `phone_*` 工具注册表和各数据源实现 | 是 |
| `feature:assistant` / `feature:agents` / `feature:chat` / `feature:terminal` / `feature:inbox` / `feature:lark` / `feature:mail` / `feature:machines` / `feature:settings` / `feature:pairing` | 各页面 | 是 |
| `app` | 导航、通知、FCM、Live Updates、快捷方式、DEBUG 直达入口 | 是 |
| `wear` | Wear OS App（第 7 节） | 是 |
| `benchmarks`、`app-catalog` | Macrobenchmark / Baseline Profile；组件 + 5 套风格的目录 App（截图测试也跑它） | 是 |

- 版本目录：`gradle/libs.versions.toml` 一份；Compose 版本由 BOM 管；convention plugin 里统一 compileSdk / minSdk / JVM target / Compose 编译器选项 / lint，单个模块的 build 文件只剩几行。
- 💭 feature 模块之间不互相依赖（只依赖 core），导航用 Nav3 的 `NavKey` 放在各 feature 的 `api` 小模块或 `core:navigation` 里——NiA 的 api / impl 拆分在本页没看到，按需采用。

来源：https://github.com/android/nowinandroid （README）

### SDK 级别建议（💭 结合 ✅ 的平台事实）

- **compileSdk 37、targetSdk 37**：新 App 一开始就按 Android 17 的规则设计（尤其本地网络权限，见第 4 节）。Play 现在只要求 ≥ 36（2026-08-31 起）。
- **minSdk 33（Android 13）**：AGSL `RuntimeShader`（液态玻璃折射需要）、`POST_NOTIFICATIONS`、系统照片选择器、按 App 语言都从 33 起；
  `RenderEffect` 模糊 31 起；Health Connect 34 起内置（33 上要装 APK）；Live Updates 36 起（低版本降级成普通常驻通知）。
  如果只给自己的新手机用，可以直接 minSdk 35/36，少写分支。

来源：
- Kotlin releases：https://kotlinlang.org/docs/releases.html
- AGP 9.4：https://developer.android.com/build/releases/gradle-plugin ；AGP 9.0 说明：https://developer.android.com/build/releases/agp-9-0-0-release-notes
- Gradle：https://gradle.org/releases/
- Compose BOM 映射：https://developer.android.com/develop/ui/compose/bom/bom-mapping
- Material3 releases：https://developer.android.com/jetpack/androidx/releases/compose-material3
- Android Studio：https://developer.android.com/studio/releases
- Android 17：https://en.wikipedia.org/wiki/Android_17
- Play 目标 API：https://developer.android.com/google/play/requirements/target-sdk

---

## 3. 架构 ✅

### 3.1 状态管理：UDF（单向数据流），不上重型 MVI 框架

- 推荐（💭 设计判断）：**Repository / Store（应用级单例，持有真相）→ ViewModel（派生 UiState，`StateFlow`）→ Compose（`collectAsStateWithLifecycle`）→ 事件回调**。
  - 每个会话一个 `ChatStore`（按 id 去重、按 seq 排序、合并实时推送 / 分页 / 磁盘缓存）放在**应用级**的 `SessionRepository` 里，不放进 ViewModel：
    后台同步（`revs` → `after_seq`）要在页面没打开时也能写它，和 iOS `AppModel` 的 `ChatStore` 一样。
  - ViewModel 只做「展示行」派生（iOS 的 `buildRows` / `chatLines`），在 `Dispatchers.Default` 上算，产出不可变的行列表（`@Immutable` 数据类 + `ImmutableList`），按 `version` 缓存。
  - 一次性事件（toast「刚才的操作没有生效」、震动）尽量建模成状态（带 id 的消息队列），少用 `Channel`。
- 备选：Slack Circuit / Molecule（presenter 用 Compose 写）——适合大团队统一范式；对单人 + AI 并行来说多一层概念，💭 不推荐作默认。

### 3.2 依赖注入对比（✅ 版本 2026-10-30）

| 方案 | 最新版本 | 性质 | 评价 |
| --- | --- | --- | --- |
| **Hilt（Dagger）** | **2.60.1**（2026-07-06） | KSP 注解处理，编译期校验 | **推荐**。Android 上最成熟、Jetpack 官方整合（ViewModel、WorkManager `@HiltWorker`、Compose / Nav3 取 ViewModel），AI 最熟。2.59 起 Hilt Gradle 插件**要求 AGP 9**；2.60 起 Hilt minSdk 23、去掉 multidex。代价：KSP 生成拖慢增量构建、只支持 Android |
| **Metro**（Zac Sweers） | **1.5.1**（2026-10-29）；1.0.0 稳定版 2026-04-27 | Kotlin 编译器插件（FIR/IR），无 KSP/KAPT | **强备选**。构建快、KMP 全平台、`metrox-android` / `metrox-viewmodel(-compose)`、Dagger / Guice / javax / jakarta 互通，1.2.0 起实验性支持 Hilt `@InstallIn` / `@EntryPoint` 互通（能渐进迁移）。风险：绑在 Kotlin 编译器版本上（运行时目标 Kotlin 2.3.0，已测 2.5.0-Beta2），生态比 Hilt 小 |
| Koin | **4.2.2**（2026-06-15） | 运行时 DSL | 不推荐：图错误在运行时才暴露；编译期校验的 Koin Compiler Plugin 还是 **1.0.0-RC1**。有 `koin-compose-navigation3` |
| kotlin-inject | **0.9.0**（2026-01） | KSP | 不推荐：更新慢（2025 年只有一个版本）、基本一人维护 |

- 💭 若要让 core 模块保持「KMP-ready」：core 里只用构造函数注入（`@Inject` 构造或手写工厂），绑定写在 app 层的 Hilt module 里；将来转 KMP 时可换 Metro（Hilt 互通可渐进迁移）。
- KSP 最新 **2.3.12**（2026-09-09），版本号已不再绑 Kotlin 版本；KSP1 在淘汰中（`ksp.useKSP2` 开关已弃用）。

### 3.3 导航：Navigation 3（推荐）vs Navigation Compose 类型安全路由

- **Navigation 3 稳定版 1.2.0（2026-09-23）**，1.0.0 于 2025-11-19 稳定，1.1.0 于 2026-04-08；1.3.0-alpha03（2026-10-21）。要求 minSdk 23、compileSdk 37。
  - 返回栈就是 App 自己持有的状态列表（`rememberNavBackStack`，`@Serializable` 的 `NavKey`，进程被杀可恢复）；`NavDisplay` + `entryProvider { entry<Key> {…} }`。
  - Scene / `SceneStrategy`（列表-详情、`DialogSceneStrategy`、`OverlayScene`）、共享元素、预测性返回（1.2 改进）、结果回传 `ResultEventBus`（1.2）、
    **深链（1.2，`UriDeepLinkMatcher`，可从 Intent 构造请求）**——通知点开会话、DEBUG 直达页面都走它。
  - ViewModel 按 entry 作用域：`androidx.lifecycle:lifecycle-viewmodel-navigation3`（随 Lifecycle **2.11.0**，2026-06-17 稳定）的 `rememberViewModelStoreNavEntryDecorator()`。
  - 对 Jarvis 的好处：iOS 踩过的「视图式 / 值式 NavigationLink 混用把新页面插到下面」「搜索状态下推页面导航栏被强制显示」这类问题，在「返回栈 = 一个 list」的模型里不存在；
    每个 tab 自己一条返回栈（官方 recipes 有多返回栈示例），对话页 → 文件 → Git 用页内 Pager，不进返回栈。
  - 风险：API 仍在演进（1.1 弃用 `then` 链、深链工厂函数改构造器）；UI 层（`navigation3-ui`）只有 Android。
- 备选：Navigation Compose 2.9+ 的 `@Serializable` 类型安全路由——成熟，但返回栈由库持有，做底部浮层 / 多返回栈 / 自定义转场不如 Nav3 直接。

### 3.4 协程与 Flow 规范（💭 规范建议，API 均为稳定 API）

- 版本：kotlinx.coroutines **1.11.0**（2026-05-08）；kotlinx.serialization **1.12.0**（2026-10-19，基于 Kotlin 2.4.10）。
- 作用域：UI 用 `viewModelScope`；连接、同步、outbox 用应用级 `CoroutineScope(SupervisorJob() + Dispatchers.Default)`，
  跟 `ProcessLifecycleOwner` 走（ON_START 连、ON_STOP 断），对应 iOS「进后台就断开 WebSocket，需要时用推送唤醒」。
- 线程：JSON 解码、展示行派生、Markdown 解析一律 `Dispatchers.Default`；磁盘 `Dispatchers.IO`；Dispatcher 注入，测试换 `StandardTestDispatcher`。
- UI 状态 `StateFlow` + `stateIn(scope, WhileSubscribed(5_000), initial)`；高频源（终端帧、spinner 行）用 `conflate()` / 按 30 ms 合帧；
  `CancellationException` 一律不当错误展示（对应 iOS `Error.shown` 过滤「已取消」），`runCatching` 要重抛取消。
- 测试：`kotlinx-coroutines-test` + Turbine。

### 3.5 Compose 稳定性与性能规范

- ✅ Compose 1.12.x（BOM 2026.09.00）；**Compose 1.12 起 compileSdk 37，要求 AGP ≥ 9.2.0**。1.11 起 `LazyLayoutCacheWindow`（预取窗口）逐步转正，1.13 beta 里已稳定并弃用 `LazyListPrefetchStrategy`；
  1.12 的 `SelectionContainer` 拖选可自动滚动；1.13 弃用 `BasicTextField(value, onValueChange)`，统一 `TextFieldState`。
- 💭 规范（对应 iOS 已踩过的坑）：
  - 强跳过（strong skipping）自 Kotlin 2.0.20 起默认开；行模型用 `@Immutable` 数据类 + `kotlinx.collections.immutable`，或在 `compose_compiler_config.conf` 里声明稳定类型。
  - LazyColumn 每行给稳定 `key` 和 `contentType`；几百步的工作过程每步是独立行（和 iOS 一样，别包成一个大 Column）。
  - 「跟手收起顶栏」这类每帧变化的值放进单独的 State，只在 `Modifier.graphicsLayer {}` / `offset {}` 的 lambda 里读（只触发绘制 / 布局阶段，不重组整页）。
  - 从消息文字派生的东西（Markdown AST、链接识别）按消息 id 放在组合外的 LRU 缓存里，后台预热；滚动中不创建 WebView。
  - 用 Compose compiler metrics / reports 检查不稳定参数；Baseline Profile + Macrobenchmark 守住冷启动和滚动（第 9 节）。

来源：
- Navigation 3：https://developer.android.com/jetpack/androidx/releases/navigation3
- Lifecycle：https://developer.android.com/jetpack/androidx/releases/lifecycle
- Dagger / Hilt：https://github.com/google/dagger/releases
- Metro：https://github.com/ZacSweers/metro ，CHANGELOG https://raw.githubusercontent.com/ZacSweers/metro/main/CHANGELOG.md
- Koin：https://github.com/InsertKoinIO/koin/releases
- kotlin-inject：https://github.com/evant/kotlin-inject/releases
- KSP：https://github.com/google/ksp/releases
- kotlinx.coroutines：https://github.com/Kotlin/kotlinx.coroutines/releases ；kotlinx.serialization：https://github.com/Kotlin/kotlinx.serialization/releases
- Compose Foundation：https://developer.android.com/jetpack/androidx/releases/compose-foundation

## 4. 网络 ✅

### Android 17 本地网络权限——对 Jarvis 影响最大的一条平台变化（✅）

- targetSdk ≥ 37 时，访问**本地网络**要运行时权限 **`ACCESS_LOCAL_NETWORK`**（危险级，属于 `NEARBY_DEVICES` 权限组；
  用户已授过同组权限如蓝牙就不再弹）。targetSdk ≤ 36 时仍由 INTERNET 隐式授予，**且官方要求低 target 不要声明 / 申请它**。
- 「本地网络」= 走 Wi-Fi / 以太网这类可广播接口的流量，**不含蜂窝和 VPN**；IPv4 范围：169.254/16、**100.64/10（CGNAT）**、10/8、172.16/12、192.168/16，
  外加多播、广播；IPv6 是链路本地、直连路由等。所有网络 API（OkHttp、Cronet、WebView 继承宿主权限）都受限，`.local` 解析也受限。
- 没权限时 **TCP 表现为超时**（不是立刻拒绝），UDP 是 EPERM——看起来像「hub 连不上」。所以连接前要先检查权限并在 UI 说清楚。
- 对 Jarvis：手机在家 / 公司 Wi-Fi 下直连 hub 的内网地址 → 需要这个权限；经 VPN（如 Tailscale）访问 → 按定义不受限（💭 需在真机实测确认）。
  **配对流程里扫码后就申请**，被拒时连接状态显示成「需要本地网络权限」而不是「连接中…」。
- 测试：Android 16 上 `adb shell am compat enable RESTRICT_LOCAL_NETWORK <pkg>` 后重启即可模拟。
- ✅ **Play 政策**（2026-10-29 刚公布）：target 37+ 申请 `ACCESS_LOCAL_NETWORK` 要在 Play Console 声明「为什么设备选择器（NsdManager）不够」，2027-06 开始要求填写，**2027-08-25 起强制**。
  不上 Play（adb / Firebase 分发）不受此政策约束，但系统层面的运行时权限照样要申请。

### 明文 HTTP：Network Security Config（✅）

- targetSdk ≥ 28 默认禁止明文；`<domain>` 只支持精确主机名（可以写单个 IP）或「主机 + 子域」，**不支持 CIDR / 通配**；`debug-overrides` 不能单独放开明文（只管信任锚）。
- Jarvis 的 hub 地址是扫码配对时才知道的，所以只能 **`<base-config cleartextTrafficPermitted="true">`**；💭 在 App 里补一道自己的校验：
  `http://` 只允许私有 / VPN 地址（10/8、172.16/12、192.168/16、100.64/10、169.254/16、`.local`、loopback、用户明确确认过的主机名），
  否则配对时警告。不要用构建期从 `config.json` 生成 domain-config：换 hub 就得重装，且违背「不写死个人信息」的约定。
- hub 若改成 HTTPS + 自签证书：`<trust-anchors>` 里放 `@raw/` 证书只能编进包里；扫码配对得到的证书要在代码里做（OkHttp 自定义 `X509TrustManager` / 证书固定）。
  Android 17 起 CT 默认开，但自定义信任锚的连接不做 CT 校验；ECH 用 `<domainEncryption>` 配，只影响 TLS。

### HTTP / WebSocket 库（✅ 版本）

| 库 | 最新 | 结论 |
| --- | --- | --- |
| **OkHttp** | **5.5.0**（2026-08-16；5.4.0 2026-06-08） | **推荐**。REST + WebSocket 一个栈；Coil 3 也用它（共用连接池 / 缓存目录策略）；MockWebServer 做测试；5.5 起可选 ECH（只在 Android 17 的 TLS 栈上有效）、新 DNS API。仓库 2026 年随 Commonhaus 迁到 `lysine-dev/okhttp`，包名不变 |
| Ktor client | **3.6.0**（2026-09-16） | 备选：只有将来要 KMP 共享网络层时才值得。Android 上通常还是套 OkHttp 引擎，多一层；OkHttp 引擎下 WebSocket ping 直到 3.5.0（KTOR-4752）才修好 |
| Retrofit | 未查版本（可选） | hub 的 REST 接口少而简单，💭 手写一层 `HubApi`（OkHttp + kotlinx.serialization）就够，不必上 Retrofit |

- **WebSocket 心跳的坑（✅ 已核实）**：OkHttp 的 `pingInterval` 是全自动的——发 ping、下一次 ping 前没收到 pong 就以 `SocketTimeoutException`（"sent ping but didn't receive pong…"）失败连接；
  **没有公开的「手动 ping」API，`WebSocketListener` 也没有 `onPong` 回调**（square/okhttp #7370、#3197、#6146 都是长期未实现的需求），拿不到 RTT。iOS 现在靠 `URLSessionWebSocketTask.sendPing` 测「5 秒内回没回 pong」判弱网。
  Android 要同样的「连接中…（弱网）」判断，建议在协议里加**客户端发起的应用层 ping**（现在只有 hub → App 的 `ping` 和 App → hub 的 `pong`）：
  App 发 `{"type":"ping","id":n}`，hub 原样回 `{"type":"pong","id":n}`，两端都按 id 计时；OkHttp 的 `pingInterval`（如 20 s）只作死连接兜底。
  不改协议的话只能换 Ktor CIO 引擎拿原始控制帧，不划算。
- 重连：指数退避 + 抖动（上限 20 s，与 iOS 一致）；`ConnectivityManager.registerDefaultNetworkCallback`（对应 NWPathMonitor）在 `onAvailable` 立即重连、
  切网络时立刻 ping 一次；`ProcessLifecycleOwner` 前台连、后台断；重连后按 `since_seq` 重新订阅。
- JSON：kotlinx.serialization，`Json { ignoreUnknownKeys = true; explicitNulls = false }`，字段用 `@SerialName` 写 snake_case（`JsonNamingStrategy.SnakeCase` 仍是实验 API，💭 不依赖）；
  WebSocket 信封按 `type` 分发（`@JsonClassDiscriminator("type")` 的 sealed 类，或先读 `type` 再解具体类型，未知 type 忽略）；
  大页（带图片的分页 1–5 MB）在 `Dispatchers.Default` 上用 `kotlinx-serialization-json-okio` 从流里解。

来源：
- Network Security Config：https://developer.android.com/privacy-and-security/security-config
- OkHttp changelog：https://square.github.io/okhttp/changelogs/changelog ，https://github.com/square/okhttp/blob/master/CHANGELOG.md
- Ktor releases：https://github.com/ktorio/ktor/releases
- Jarvis 协议（WebSocket 消息表）：仓库 `docs/protocol.md`

### 其他 Android 17（target 37）网络相关：✅ Certificate Transparency 默认开、ECH（Network Security Config 新增 `<domainEncryption>`）。

来源：
- https://developer.android.com/about/versions/17/behavior-changes-17
- https://developer.android.com/privacy-and-security/local-network-permission
- https://developer.android.com/privacy-and-security/local-network-definition

## 5. 存储 ✅

### 5.1 数据库：Room 3（推荐）vs Room 2 vs SQLDelight

| 方案 | 最新 | 要点 |
| --- | --- | --- |
| **Room 3**（`androidx.room3`） | **3.0.3**（2026-09-09）；3.0.0 稳定于 2026-07-01；3.1.0-alpha03（2026-10-21） | **推荐**（新工程没有迁移包袱）。新包名 / 新 Maven 组；只生成 Kotlin、必须 KSP；**所有数据库操作都是协程 API**（DAO 要么 suspend 要么返回 Flow）；只走 `SQLiteDriver`（不再有 SupportSQLite / Cursor）；KMP 全平台（含 iOS、web）；FTS5（`@Fts5`）；InvalidationTracker 改成 Flow。Paging 要显式注册 `@DaoReturnTypeConverters(PagingSourceDaoReturnTypeConverter::class)` |
| Room 2 | 2.8.5（2026-09-09） | 成熟、资料最多；2.7 起已是 KMP；minSdk 23 |
| SQLDelight | 2.4.1（2026-10-06） | SQL 优先、类型安全、KMP；2.3.2 起兼容 AGP 9 新 DSL / 内置 Kotlin。适合想手写 SQL 的人；和 Jetpack（Paging、WorkManager 示例）的整合不如 Room |

- 💭 Room 3 的风险：稳定才 4 个月，3.0.x 补丁主要在修事务死锁 / 取消后连接状态；AI 训练语料里几乎都是 Room 2 写法（包名、`@TypeConverter` 已改名 `@ColumnTypeConverter`），
  要在 `AGENTS.md` / `CLAUDE.md` 里写明「用 Room 3」。保守选择是 Room 2.8.5，以后再迁（官方说 KMP 用法下迁移主要是改 import）。

### 5.2 放什么、放哪（💭 设计建议，对照 iOS 的 StateCache / ChatCache / Outbox）

| 数据 | Android 去处 | 说明 |
| --- | --- | --- |
| 每个会话的 items（按 seq） | Room 表 `items(machine_id, session_id, seq, id, json)` + 会话游标表（covered / rev / has_more） | 后台同步 40 个会话按 `after_seq` 增量写；打开会话先画库里的最近两轮再合并服务器数据；比 iOS 整份 JSON 文件更适合增量 |
| Outbox（待发消息，`client_msg_id`） | Room 表，放 `filesDir` 下的库 | 必须持久：`cacheDir` 在存储紧张时会被系统清掉 |
| 机器 / 会话列表 / 待处理交互 / 收件箱 / 飞书会话最近 80 条 | Room 或 `cacheDir` 下的 JSON 快照（kotlinx.serialization + okio 原子写） | 冷启动先画缓存、后台静默刷新 |
| 设置、界面风格、终端字号、快捷命令 | **DataStore 1.3.0**（Preferences 或带 kotlinx.serialization 的 typed DataStore） | |
| 配对信息 / 设备 token（`jd_…`） | DataStore + **`datastore-tink` 的 `AeadSerializer`**（1.3.0 新增），密钥集用 Tink 的 Android Keystore 主密钥包裹 | 替代已废弃的 EncryptedSharedPreferences |
| 对话图片、飞书媒体 | Coil 3 的 `DiskCache`（分目录：`chat-images`、`lark-media`），图片不会变，用 `ref` 做缓存 key、永久有效 | 「设置 › 存储空间」按目录统计大小 / 清除 |
| 网页 | WebView 自己的缓存（`app_webview` 目录），清除用 `WebView.clearCache(true)`；登录态在 Cookie / WebStorage，另外清 | |

### 5.3 密钥 / token 加密（✅）

- **`androidx.security:security-crypto` 1.1.0（2025-07-30 稳定）把所有 API 标成废弃**（1.1.0-alpha07 起）：「Deprecated all APIs in favour of existing platform APIs and direct use of Android Keystore」。
  EncryptedSharedPreferences / EncryptedFile / MasterKey 都不要再用。
- 官方新路：**DataStore 1.3.0（2026-10-21）新增 `androidx.datastore:datastore-tink`**，`AeadSerializer` 包住原有 Serializer，`associatedData` 用文件名防密文替换；
  Tink 只支持 JVM / Android。
- **Tink 1.24.0**（2026-10-16，`com.google.crypto.tink:tink-android`）：minSdk 23；1.16 起有 `AndroidKeystore` 类（官方推荐的 Keystore 用法）；1.18 修了 API ≤ 28 上 >128 KB 输入的 Keystore AEAD 问题。
- 💭 坑：Keystore 里的密钥**不随自动备份迁移**，恢复到新手机后加密数据解不开——加密的配对文件要在 `dataExtractionRules` 里排除出云备份 / 设备迁移，
  恢复后走重新配对（iOS 的 Keychain 也有类似的「本机专属」语义）。

来源：
- Room：https://developer.android.com/jetpack/androidx/releases/room ；Room 3：https://developer.android.com/jetpack/androidx/releases/room3
- SQLDelight：https://github.com/sqldelight/sqldelight/releases
- DataStore：https://developer.android.com/jetpack/androidx/releases/datastore
- security-crypto：https://developer.android.com/jetpack/androidx/releases/security
- Tink：https://github.com/tink-crypto/tink-java/releases

## 6. UI 组件 ✅

### 6.1 主题系统：Material 3 打底 + 自己的 token 层（5 套风格）

- 版本：Compose BOM 2026.09.00（Compose 1.12.1、material3 1.4.0）。Expressive 组件（SplitButton、ButtonGroup、FloatingToolbar、Flexible TopAppBar、Expressive 列表项、SearchBar…）
  在 **material3 1.5.0-beta02**（2026-10-21）才陆续去掉实验注解；1.4.0 上用要 `@OptIn(ExperimentalMaterial3ExpressiveApi::class)`。
  💭 建议先用 1.4.0 稳定版，等 1.5.0 稳定（看节奏 1–2 个月内）再切；需要的 Expressive 组件少量 opt-in。
- 💭 结构：`MaterialTheme`（颜色 / 字体 / 形状）只负责系统组件的默认外观；Jarvis 自己的 `JarvisTheme` 用 CompositionLocal 提供
  `AppStyle`（glass / claude / clay / island / lark）+ token（Radius 五档、Fill 三档、Font 角色、Space、Motion）+ `StatusPalette`（不随风格变）。
  页面只调 `Modifier.surface(tint, interactive, shape)`、`themedButton(prominent)`、`themedPage()` 这类封装，和 iOS `Theme` 一一对应；切风格时整棵树按风格 key 重建（同 iOS）。
- 💭 **两端共用一份设计 token**：把 `Theme.swift` / `StatusPalette.swift` 里的色值、圆角、对比度底线抽成仓库里的一份 JSON（或 TOML），
  用脚本生成 Swift 和 Kotlin 常量，避免两端色值漂移；对比度（≥ 3.5 / 4.5 / 3:1）在生成脚本里校验。
- 各风格在 Android 上的画法：
  - **黏土 / Island / 飞书**：Compose 1.9 起有 CSS 式 **`Modifier.dropShadow(shape, Shadow(...))` / `innerShadow`**（左上高光 + 右下彩色阴影、大而柔的阴影都能直接画）、
    发丝描边用 `border(Dp.Hairline)`；完全可还原。
  - **液态玻璃 / Claude（玻璃表面）**：Android 没有系统级 Liquid Glass。可选：
    - **Haze 2.0.1**（2026-09-29，Chris Banes，KMP）：`haze` + `haze-blur`（**所有 Android 版本都能模糊**：31+ 走 RenderEffect，31 以下 RenderScript），
      实验性 `haze-glass`；`HazePerformanceMode` 分档；progressive blur；`haze-blur-material3` 预设。适合顶栏、底栏、输入框胶囊这类「磨砂玻璃」。
    - **Backdrop（Kyant0/AndroidLiquidGlass）2.0.1**（Apache-2.0，`io.github.kyant0:backdrop`，约 4.4k star，KMP）：在背景副本上做 lens（折射）/ vibrancy / blur，
      最接近 iOS 26 的「液态」观感；不带现成组件（catalog 里有 LiquidButton / Toggle / Slider / BottomTabs 示例）；折射靠 AGSL，**需要 API 33+**（社区资料，未在官方文档核实）。
  - 💭 建议：Android 默认风格不要是「液态玻璃」（Android 用户和系统都不是这套语言，且 GPU 开销大），默认用 Claude / 飞书这类实心风格；
    「液态玻璃」作为可选风格：小面积用 Backdrop（按钮、tab 栏、键栏），大面积用 Haze 磨砂；API < 33 降级成半透明 + 模糊。玻璃面别叠玻璃、别盖在大段滚动内容上做全屏模糊，中端机上逐帧测。

### 6.2 聊天列表

- 💭（基于 Compose 公开行为）**`LazyColumn(reverseLayout = true)` 原生就是 iOS 那套「倒置列表」**：index 0 = 最新、贴着底部，`scrollToItem(0)` 永远精确；
  往更早翻是在列表尾部追加，不推动正在看的内容。不需要 iOS 的双重 `scaleEffect` 翻转，也没有翻转后安全区落错端、边缘模糊蒙住整页的问题。
- Compose 的 `scrollToItem(index, offset)` **按目标项实际测量定位，不按估算高度**，所以没有 SwiftUI LazyVStack「跳到远处落在没渲染的区域 → 白屏」那类问题；
  滚动条位置才是估算的。位置保持按 `key`：在看历史时上方（index 0 侧）插入新行，视口留在原来那一行。
  注意：iOS 踩过的「行 id 随轮次推进变化（答案收进工作行、多条消息合并成 `wt-`）」在 Compose 里同样会让按 key 的锚定失效——冻结快照（`ChatView.Frozen` 的做法）仍然需要。
- 贴底跟随：`followsEnd` 时新内容到达后 `scrollToItem(0)`；用户拖动（`isScrollInProgress` + 拖动交互，而非程序滚动）就关；用 `LazyListState.layoutInfo` 判断是否在最新端。
- 每一步都是独立行、稳定 `key` + `contentType`；`LazyLayoutCacheWindow`（1.13 稳定）控制预取范围；历史分页靠在列表尾部放一个哨兵行触发「再取两轮」。
- **不用 Paging 3**（最新 3.5.3，2026-10-12）：Jarvis 按「轮次」分页、展示行要跨页做轮次折叠、实时推送要合并进同一个 store，PagingSource 的模型不合适；
  Paging 3.5 新增的 `asItemSnapshotListFlow` / `Pager.append/prepend` 适合普通列表（如 Git 提交历史），聊天用自己的 `ChatStore`。
- 文字选择：Compose 的 `SelectionContainer` 能跨多个 `Text` 选择（1.12 拖选可自动滚动），所以 Android 上**有机会在原地选择一条消息的 Markdown**（iOS 因 SwiftUI Text 限制只能弹纯文本页）；
  跨懒加载行的选择仍不可靠，长按菜单「选择」保留纯文本页作兜底。

### 6.3 Markdown

| 库 | 最新 | 结论 |
| --- | --- | --- |
| **mikepenz/multiplatform-markdown-renderer** | **0.46.0**（2026-10-13；0.43.0 2026-06-22） | **推荐**。KMP / Compose；GFM（表格、任务列表、删除线、自动链接、GitHub alert）开箱即用；代码高亮走可选的 `-code` 模块（用 Highlights）；`-coil3` 图片；`MarkdownComponents` 可以替换任意节点的渲染（做工具卡片、`<jarvis-context>` 引用卡、文件路径标签）；异步解析（`rememberMarkdownState`）、ViewModel 里预解析（`parseMarkdownFlow`）、**流式增量解析（`rememberStreamingMarkdownState`，只重解析尾部，正好用于助手逐字输出）**、超长文档 `LazyMarkdownSuccess` |
| compose-richtext（halilozercan） | 1.0.0-alpha05（2026-06） | 不推荐：一直是预发布，更新间隔 7–11 个月 |
| Markwon | 4.x（View / Spannable，页面看不出最近发布） | 不推荐：基于 TextView，不是 Compose |

- 💭 自研退路：如果 mikepenz 的版式不够细（iOS 用 MarkdownUI 自定义主题 `.card` 等），可以在 JetBrains `org.jetbrains:markdown` 解析器（mikepenz 底层就是它）上自己写渲染层。
- 💭 解析结果按消息 id 缓存在组合外（对应 iOS `RowMemo` / `TextMemo`），消息加载后后台预热。

### 6.4 代码高亮

- iOS 现状：Jarvis 自己写的轻量高亮（注释 / 字符串 / 数字 / 常见关键字，`LarkSyntax.highlight`），不依赖库。
- 💭 推荐 Android 照搬这套轻量规则（几十行，结果和 iOS 一致，便于共享 golden 测试）；需要更好效果时用 **Highlights 1.1.0**（`dev.snipme:highlights`，KMP，17 种语言、6 套主题、增量 / 异步），
  它**不支持 JSON / YAML / diff / SQL / HTML**，这些仍走轻量规则。

### 6.5 图片：Coil 3

- **Coil 3.6.3**（2026-09-18，**修了 AGP 9.4.0 + R8 构建失败**——AGP 9.4 必须用 ≥ 3.6.3）；3.6.0 起 Kotlin 2.4.10 / Compose 1.12 / compileSdk 37；3.5.0 起 minSdk 23。
- 用 `coil-network-okhttp`（共用 App 的 OkHttpClient，带 Bearer token）；Coil 自己的 `DiskCache`（官方提醒别同时开 OkHttp Cache）。
- 💭 Jarvis 的懒加载图片（`ImagePayload.ref`）：自定义 `Fetcher` / `Keyer`，按 ref 永久缓存；行里先按尺寸占位（不跳），滚到才取。

### 6.6 全屏图片查看

- **Telephoto 0.19.0**（2026-04-02）：`ZoomableImage`（双击 / 双指缩放、超大图分块 sub-sampling）、`me.saket.telephoto:zoomable-image-coil3`（0.14.0 起）、
  `Modifier.zoomable()`（可用于任意内容）、flick-to-dismiss（0.17.0 已有，官方说「还没正式发布」）。外面套 `HorizontalPager` 浏览整个会话的图片。
- 图片编辑（框选 / 画笔 / 颜色 / 撤销）自己用 Canvas 写，同 iOS。

### 6.7 等宽文本 / diff（横竖都能滑）

- 💭 Compose 可以直接做 iOS 上做不到的：`Box(Modifier.horizontalScroll(h))` 里放一个**宽度固定为最长行实测宽度**（`TextMeasurer` 量）的 `LazyColumn`，每行一个 `Text(softWrap = false)`；
  竖向懒加载、横向整体滑。大文件 / 大 diff 也只组合可见行。行号列放在横滑区外面固定。iOS 的 `MonoTextView`（UIScrollView 包 UITextView）在这里不需要。

### 6.8 终端仿真（✅ 许可证 / 现成库现状 + 💭 方案）

| 方案 | 现状 | 结论 |
| --- | --- | --- |
| **自写网格 + Compose Canvas（推荐）** | 把 iOS 的 `TerminalGrid`（herdr observe 用到的 VT 子集：CUP、ED、SGR、DECTCEM、2026、OSC 8）移植到纯 Kotlin 的 `core:terminal`，用 iOS 已录下的真实帧（`ios/JarvisTests/Fixtures/terminal-observe.jsonl`）做两端共用的测试夹具 | 和 iOS 的选型理由完全相同：输入是 herdr 已仿真好的网格，难的部分 herdr 做了；要「网格 172 列、显示按 45 列折行」，现成视图都不支持 |
| ConnectBot **termlib** | 0.3.14，**Apache-2.0**（libvterm MIT，JNI），Compose 组件，256 色 / 真彩、宽字符、组合字符、带放大镜的选择、缩放、复杂文字 shaping，2026-10 仍在更新 | 若将来拿到程序原始字节（真 PTY）可以考虑；现在用不上它的仿真，且要带 native 库 |
| Termux `terminal-emulator` / `terminal-view` | termux-app 整体 **GPLv3**；LICENSE 只在例外里说这两个库含 jackpal 的 Apache-2.0 代码，**没有明确说整个库都是 Apache** | 不推荐：许可证有灰区；View 体系；Termux 用 `TYPE_NULL` 输入类型拿原始按键，中文输入法组字会出问题 |

- 💭 渲染：每格按网格坐标画（宽字符两格、框线 / 块字符画成线和矩形），不靠字体字宽，中文和框线永远对齐（同 iOS）；`TextMeasurer` 按 run 缓存；只画到最后一行有字处或光标处。
- 💭 输入法：直接打字模式用 Compose 的 `PlatformTextInputModifierNode`（或 AndroidView 里自定义 View 的 `onCreateInputConnection`）自建 `InputConnection`，
  正确处理 `setComposingText` / `finishComposingText` / `commitText`（对应 iOS 的 marked text），输入类型用 `TYPE_CLASS_TEXT | TYPE_TEXT_FLAG_NO_SUGGESTIONS`，别用 `TYPE_NULL`；
  修饰键粘滞 / 锁定 / 按住、滚轮转发、双指切 Agent 都在 `pointerInput` 里自己判（不用 `combinedClickable`，Compose 1.11 改写过它的长按行为）。

### 6.9 链接浮层与 WebView

- **浮层**：M3 的 `ModalBottomSheet` 是独立窗口 + 遮罩，**背后不能交互**；💭 用同一窗口里的自绘浮层：`AnchoredDraggable`（三个锚点：全屏 / 60% / 隐藏）叠在对话页上，
  60% 时只有浮层区域吃触摸，上方对话照常滚动、输入；或用 M3 的 `BottomSheetScaffold`（标准浮层，背后可交互，peek 设成 60%）。
  WebView 不实现 Compose 的嵌套滚动协议（💭 需实测），下拉收起靠顶部把手 / 标题栏拖动，或 JS 报告 `scrollY == 0` 时把下拉交给浮层。
- **WebView 加速（✅ androidx.webkit 1.18.0，2026-10-21，minSdk 24）**：
  - `WebViewCompat.startUpWebView(context, WebViewStartUpConfig, receiver)`（1.16 起稳定）：在 App 空闲时异步预启动 WebView，首次打开不卡 UI 线程；
  - `WebViewCompat.prerenderUrlAsync(...)`（1.15 起免 OptIn，只能 UI 线程）：对「最可能点开」的那个链接预渲染，点开即显示；
  - `Profile.prefetchUrlAsync`（仍实验）、`Profile.preconnect` / `enqueuePreconnect`（1.17，不触发 WebView 启动就预连）、`Profile.warmUpRendererProcess`（实验）；
  - `NavigationListener`（1.16 稳定，`WebViewFeature.NAVIGATION_LISTENER`）拿 FCP / LCP，用来量「打开链接多快」；back-forward cache 设置 1.16 有不兼容改动。
  - 💭 池子照 iOS：最多 6 个 WebView（LRU）、共用默认 Profile 的缓存和 Cookie；**滚动中不创建 WebView**（等滚动停了在下一帧建）。
  - 网页里选中文字加「AI 修改 / 作为上下文」：重写 `startActionMode` 往 `ActionMode` 菜单里加项，选中文字用 `evaluateJavascript("getSelection().toString()")` 取。
- **Custom Tabs 的取舍（✅）**：Partial Custom Tabs（Chrome 107+）可以底部半屏、**背后 App 可交互**、拖把手全屏；最小高度 50%；
  但无法注入 JS / 改选中菜单，「AI 修改 / 作为上下文」做不了，也拿不到预加载池的控制权。💭 所以：普通链接用 WebView；**Google 登录这类禁止内嵌 WebView 的 OAuth 必须走 Custom Tabs**；
  另给「在浏览器打开」。

### 6.10 图表（机器监控）

- **Vico 3.3.1**（2026-08-28；3.4.0-next.1 2026-10-28，minSdk 将升到 24）：Compose / M3，多序列、触摸 marker；**发布说明里没有「断点 / null 值」**。
  💭 断开的线按 iOS 的办法做：每一段一个序列（同色），空档就自然不连。也可以直接用 Compose Canvas 自绘（三条线 + 按住看某时刻 + 裁剪），iOS 的规则（断开、裁剪、刻度色）都好控制，💭 倾向自绘。

### 6.11 二维码扫描（配对）

- **Google code scanner** `com.google.android.gms:play-services-code-scanner:16.1.0`：**不需要相机权限**（Play 服务代扫，只回结果），minSdk 23，UI 不可定制，需要 Google Play 服务（模块首次使用时下载，或用 manifest meta-data 安装时下载）。💭 首选：配对是一次性操作，系统 UI 足够。
- 自定义 UI / 无 Play 服务时：**CameraX 1.6.2**（2026-08-26；1.7.0-rc01 2026-10-21）`camera-compose` 的 `CameraXViewfinder`（1.7 起自带捏合缩放、点按对焦）+ `camera-mlkit-vision` 的 `MlKitAnalyzer` + ML Kit 条码（bundled）。
  注意 CameraX 1.7 用于 Compose 要求 AGP ≥ 9.2。

来源：
- Material3：https://developer.android.com/jetpack/androidx/releases/compose-material3
- Compose 阴影：https://developer.android.com/develop/ui/compose/graphics/draw/shadows
- Haze：https://chrisbanes.github.io/haze/latest/ ，changelog https://chrisbanes.github.io/haze/latest/changelog/
- Backdrop：https://github.com/Kyant0/AndroidLiquidGlass ，https://kyant.gitbook.io/backdrop ，https://klibs.io/project/Kyant0/AndroidLiquidGlass
- Paging：https://developer.android.com/jetpack/androidx/releases/paging
- Markdown：https://github.com/mikepenz/multiplatform-markdown-renderer （releases 页）；https://github.com/halilozercan/compose-richtext/releases ；https://github.com/noties/Markwon
- Highlights：https://github.com/SnipMeDev/Highlights
- Coil：https://coil-kt.github.io/coil/changelog/
- Telephoto：https://github.com/saket/telephoto/releases
- termlib：https://github.com/connectbot/termlib ，http://termlib.connectbot.org/ ；Termux 许可证：https://github.com/termux/termux-app/blob/master/LICENSE.md
- WebView：https://developer.android.com/jetpack/androidx/releases/webkit ；Partial Custom Tabs：https://developer.chrome.com/docs/android/custom-tabs/guide-partial-custom-tabs
- Vico：https://github.com/patrykandpatrick/vico/releases
- 扫码：https://developers.google.com/ml-kit/vision/barcode-scanning/code-scanner ；CameraX：https://developer.android.com/jetpack/androidx/releases/camera

## 7. 系统能力 ✅

### 7.1 推送：FCM

- 💭 前提：要 Google Play 服务 + 一个 Firebase 项目；hub（Go）用 FCM HTTP v1（`firebase.google.com/go` Admin SDK 或直接调 REST + 服务账号）发。
  服务账号 JSON 属于密钥，按约定只放 hub 的 `hub.json` 同级（0600），不入库。没有 Play 服务的手机（国行 / 去谷歌）收不到——备选 UnifiedPush（自建 ntfy），💭 先不做。
- ✅ **高优先级**：可唤醒休眠设备、给少量处理时间（「只够立刻渲染一条通知」），之后要做事用**加急 WorkManager**（高优先级消息之后有短暂豁免）；
  **高优先级消息如果没有产生用户可见的通知，会被降级为普通优先级**（按每个安装实例最近 7 天的行为判断）；通知权限关了也会被降级。
  Play 服务可代为显示部分高优先级通知（Android Q+，Beta，只对 HTTP v1）。
- 💭 对 Jarvis：交互（需要你）用高优先级 + 立刻显示通知；「完成未看」「Live Update 刷新」也都对应用户可见的变化，不会被降级；
  纯后台同步（`revs`）别用高优先级推，用普通优先级或不推。
- 通知能力对照 iOS：
  - 通知渠道：需要你（高重要性、可打断）、Agent 完成、Fleet（Live Update）、手机工具确认等，用户可在系统里单独调。
  - 按钮：`NotificationCompat.Action`；**「允许 / 批准」要求解锁**：`Notification.Action.Builder.setAuthenticationRequired(true)`（✅ API 31 起；设备锁着时系统先弹锁屏验证再发 PendingIntent），对应 iOS `.authenticationRequired`；「拒绝」不要求。
  - 直接输入意见（打回计划）：`RemoteInput`（直接回复）。按钮在后台调 hub resolve，失败发一条本地通知说明原因（同 iOS）。
  - Android 13+ 要运行时申请 `POST_NOTIFICATIONS`。

### 7.2 锁屏实时活动 / 灵动岛 → Live Updates（规则见本节末尾「Live Updates」小节，这里是对应关系）

- 映射：Fleet 汇总 → 一条 promoted ongoing 通知。✅ **Android 17（API 37）新增 `Notification.MetricStyle`**：最多 3 个指标（label + value + 可选单位）、`setCriticalMetric(index)` 标出关键那个、最多 3 个按钮，
  可被提升为 Live Update（提升后不必设 contentTitle，指标值就是主信息）——**正好装下「需要你 / 工作中 / 完成未看」三个数**，需要你设为 critical。
  状态栏 chip 用 `setShortCriticalText("⚙3")`（≤ 6 字符）。Android 16 用 BigText 的 Live Update，更低版本降级成普通常驻通知。
  💭 NotificationCompat 是否已封装 MetricStyle 未查到，先直接用平台 API（`SDK_INT >= 37`）。
- iOS 的 push-to-start / push-to-update 在 Android 上没有对应：只能 FCM 数据消息唤醒 App 后本地 `notify()`；所以 hub 侧「每台每分钟最多一次」的节流仍然需要。
- 风险：官方用途规范不鼓励「由别人触发的活动」「提醒类 alert」；OEM 可加限制；用户可一键关掉。Wear OS 7 上 Live Updates 也会桥接到手表。

### 7.3 后台连接：不常驻前台服务（✅ 规则 + 💭 方案）

- ✅ target 35+ 的 `dataSync` / `mediaProcessing` 前台服务在后台**24 小时内合计最多 6 小时**，到点 `onTimeout` 后几秒内不 `stopSelf()` 就崩溃（`RemoteServiceException`）；
  用完额度再启动抛 `ForegroundServiceStartNotAllowedException`；12+ 基本不能从后台启动前台服务。
- 💭 所以照 iOS：**前台连 WebSocket，进后台就断**；需要时靠 FCM 唤醒。`phone_*` 工具调用在 App 不在线时由 hub 回「手机不在线」，
  或者 hub 发一条高优先级 FCM「有工具调用」唤醒 App 后用加急 WorkManager 走 HTTP 执行（不必等 WebSocket）——💭 这比 iOS 的静默推送更可靠，可作为 Android 的增强。
- **WorkManager 2.12.0**（2026-09-23，minSdk 24；需要 23 就停在 2.11.2）：outbox 重发、后台同步兜底（带网络约束）、FCM 之后的加急任务；2.12 新增实验性的 Work Metrics。

### 7.4 小组件：Glance

- **Glance 1.2.0**（2026-08-26）：generated previews、M3（`glance-material3`）、LazyColumn；1.3.0-alpha02（remote compose，要求 AGP ≥ 9.2）。
- ✅ Android 17 target 37：小组件位图内存上限（所有尺寸桶合计超过 `6 × 宽 × 高` 字节，`updateAppWidget` 抛异常）。
- 💭 用途：Fleet 状态小组件（需要你 / 工作中 / 完成未看 + 「批准第一个」按钮），替代 iOS 的锁屏 / 智能叠放入口。

### 7.5 手表：Wear OS

- ✅ **Compose for Wear OS 1.7.1**（2026-10-07；1.8.0-alpha02），**Wear Compose Material 3** 是推荐库（M3 Expressive）；`TransformingLazyColumn`、Nav3 的 `SwipeDismissableSceneStrategy`（1.6）。
- ✅ **Wear OS 7**（2026-05 I/O 发布，基于 Android 17）：**Wear Widgets**（基于 Glance + RemoteCompose，2×1 / 2×2 两种卡片）取代 Tiles（Tiles 1.6、ProtoLayout 1.4 仍支持一段时间；Widgets API 支持 Wear OS 4+）；
  手表上的 **Live Updates 取代 Ongoing Activities**。Play 要求 Wear 应用 target ≥ 35。
- 💭 方案：独立 Wear App（手表直接连 hub，用 `kind: watch` 的 `answer` 角色 token，和 iOS 手表一致），手机经 **Data Layer**（`MessageClient` / `DataClient`）把配对信息推给手表；
  界面：需要你列表 → 交互详情（提问 / 计划 / 授权）→ 会话详情；一个 Wear Widget 显示 Fleet 数量。⚠ 手表连 hub 走手机代理或 Wi-Fi，内网 hub 的可达性要实测。
  AGP 9 已移除「嵌入 Wear App」（`wearApp` 配置），手表 App 单独打包发布。

### 7.6 Siri / 快捷指令的替代

- ✅ **Google Assistant 已于 2026-09-28 对手机用户关闭**（换成 Gemini）——依附 Assistant 的 **App Actions（built-in intents）基本作废**，不要投入。
- ✅ **AppFunctions**（`androidx.appfunctions` **1.0.0-beta01**，2026-10-21；基于 Android 16 平台 API）：把 App 的功能暴露给设备上的 AI 代理（类似本地 MCP）；
  调用方需要 `EXECUTE_APP_FUNCTIONS` 权限，Gemini 集成到 2026-05 还是「受信测试者私测」。💭 可以把「问 Claude / Jarvis 状态 / 批准第一个待处理」做成 AppFunctions，算面向未来的投入，不是现在能依赖的入口。
- 💭 现在就能用的入口：**动态 / 固定快捷方式**（`ShortcutManagerCompat`：长按图标「问 Claude」「批准第一个」）、**快捷设置磁贴**（`TileService`：显示状态、一键批准）、
  **Glance 小组件**、通知按钮、**分享目标**（把文字 / 图片 / 链接分享给 Jarvis 作为上下文）、给 Tasker / 自动化 App 的显式 Intent 接口（≈ 快捷指令）。

### 7.7 生物识别

- ✅ `androidx.biometric` 稳定版仍是 **1.1.0（2021）**；新 API（`registerForAuthenticationResult`、`AuthenticationRequest`、`biometric-compose` 的 `rememberAuthenticationLauncher`、Android 36.1 起最多 4 个自定义 fallback）在 **1.4.0-alpha07**（2026-04-22），alpha 间仍有改名。
- 💭 Jarvis 用到的地方很少：通知「允许」用系统的解锁要求即可；App 内「全部放行」这类高风险操作可选 BiometricPrompt（用稳定版 1.1.0 或 alpha 的 Compose 版）。

来源：
- FCM 优先级：https://firebase.google.com/docs/cloud-messaging/android-message-priority
- 前台服务超时：https://developer.android.com/develop/background-work/services/fgs/timeout
- WorkManager：https://developer.android.com/jetpack/androidx/releases/work
- Glance：https://developer.android.com/jetpack/androidx/releases/glance
- Wear Compose：https://developer.android.com/jetpack/androidx/releases/wear-compose ；Wear OS 7：https://android-developers.googleblog.com/2026/05/whats-new-wear-os-7.html ，https://www.androidauthority.com/google-wear-os-7-wear-widgets-early-partners-availability-features-3670112/
- AppFunctions：https://developer.android.com/jetpack/androidx/releases/appfunctions ，https://developer.android.com/ai/appfunctions ，https://9to5google.com/2026/02/25/android-appfunctions-gemini/
- Assistant 关闭：https://en.wikipedia.org/wiki/Google_Assistant
- Biometric：https://developer.android.com/jetpack/androidx/releases/biometric
- MetricStyle：https://developer.android.com/develop/ui/views/notifications/metric-style ，https://developer.android.com/reference/android/app/Notification.MetricStyle
- 通知按钮需解锁：https://developer.android.com/reference/android/app/Notification.Action.Builder
- OkHttp ping/pong 需求：https://github.com/square/okhttp/issues/7370 ，https://github.com/square/okhttp/issues/3197

### Live Updates（Android 16+，替代锁屏实时活动 / 灵动岛）✅

- 要求：Standard / BigText / Call / **ProgressStyle** / **MetricStyle** 之一；manifest 声明 `POST_PROMOTED_NOTIFICATIONS`（安装时权限）；
  `setRequestPromotedOngoing(true)`；必须 ongoing、有 contentTitle；**不能**有自定义 RemoteViews、不能是分组摘要、不能 colorized、渠道不能是 IMPORTANCE_MIN。
  OEM 可加额外条件。用 `NotificationManager.canPostPromotedNotifications()` 查用户是否关了，`Settings.ACTION_MANAGE_APP_PROMOTED_NOTIFICATIONS` 跳设置。
- 状态栏 chip：小图标 + `setShortCriticalText`（≤ 6 个字符最稳，最宽 96dp）或 `when` 倒计时 / chronometer。
- 用途规范：「ongoing、user-initiated、time-sensitive」，明确**不适合**：广告、聊天消息、提醒类 alert、日历事件、「由别人触发的活动」、环境信息。
  Jarvis 的 Fleet（Agent 工作中、需要你）属于「用户发起的长任务」，💭 勉强符合；「需要你」的提醒更像 alert，应走普通高优先级通知。
- **没有 iOS 那种 push-to-update 通道**：只能 FCM 高优先级数据消息唤醒 App，`onMessageReceived` 里重建通知、同一 id `notify()`。
- 也会桥接到部分 Wear OS 设备。

来源：https://developer.android.com/develop/ui/views/notifications/live-update

## 8. 手机数据工具对应 API ✅

| iOS 工具 | Android API | 权限 / 限制 | 结论 |
| --- | --- | --- | --- |
| `phone_contacts_search` | `ContactsContract`（按姓名 / 公司 / 职位 / 邮箱 / 号码模糊查，`Data` 表 + `PhoneLookup`） | `READ_CONTACTS`（运行时）。✅ Android 17 新增系统 **Contact Picker**；target 37 时 `Data` 视图去掉 `ACCOUNT_NAME` 等列（改 join RawContacts），无权限时查询走严格模式。✅ **Play 政策**：target 37+ 申请 READ_CONTACTS 要在 Play Console 声明「为什么 Contact Picker 不够」（「AI assistants」是列出的合理用途之一），2027-01-27 起强制 | 可做，需完整权限（picker 不能做关键词搜索） |
| `phone_calendar_events` / `create_event` | `CalendarContract`（`Instances` 表按时间范围查，`Events` 插入） | `READ_CALENDAR` / `WRITE_CALENDAR`；插入要选一个访问级别 ≥ CONTRIBUTOR 的日历（同步账号的日历会同步到云端）；也可以用 `ACTION_INSERT` Intent 交给日历 App（无需权限但要跳出去） | 可做，和 iOS 基本对等 |
| `phone_reminders_*` | **Android 没有系统级的「提醒事项 / 待办」提供者**：`CalendarContract.Reminders` 只是日程的提醒闹钟，不是待办 | ✅ DAVx⁵ 的说法：只能写进某个待办 App 自己的 ContentProvider（OpenTasks 的 `TaskContract`、tasks.org、jtx Board），OpenTasks 已基本停更 | 💭 推荐：**改由 hub 用 Google Tasks API**（hub 已有 Google 账号 OAuth，加 `tasks` scope），工具名不变，Android 手机上走 hub；可选再支持已安装的 TaskContract 提供者 |
| `phone_location_current` | Fused Location Provider `getCurrentLocation(Priority.PRIORITY_HIGH_ACCURACY, token)`；反向地理编码 `Geocoder.getFromLocation(…, listener)`（API 33+ 异步） | `ACCESS_FINE_LOCATION`（使用期间）；后台定位不做（同 iOS） | 可做 |
| `phone_health_summary` | **Health Connect**：`StepsRecord`、`DistanceRecord`、`ActiveCaloriesBurnedRecord`、`HeartRateRecord`、`RestingHeartRateRecord`、`SleepSessionRecord`（含 stages）、`WeightRecord`，全部有聚合指标（`COUNT_TOTAL`、`DISTANCE_TOTAL`、`BPM_AVG/MIN/MAX`、`SLEEP_DURATION_TOTAL`…），按天用 `aggregateGroupByPeriod` | 每种数据一个 `READ_*` 权限；**能查到是否已授权**（`getGrantedPermissions()`，和 iOS「读权限永远不告诉你」不同）；默认只能读首次授权前约 30 天，更早要 `READ_HEALTH_DATA_HISTORY`；后台读要 `READ_HEALTH_DATA_IN_BACKGROUND`；Android 14+ 内置，13 及以下要装 Health Connect App；上 Play 要做健康数据声明 | 可做，数据项与 iOS 一一对应，且更透明 |
| `phone_photos_recent` / `phone_photo_get` | `MediaStore` 查询（最近照片的元数据：时间、尺寸、截图、时长、经纬度） | 需要 `READ_MEDIA_IMAGES` / `READ_MEDIA_VIDEO`；Android 14+ 用户可只给部分（`READ_MEDIA_VISUAL_USER_SELECTED`，查询只返回选中的，类似 iOS limited）。✅ **Play 政策**：只有「核心功能需要广泛访问照片」的 App 能申请，否则必须用系统 Photo Picker（2025-05-28 起强制）；不上 Play 则无此限制。读位置元数据还要 `ACCESS_MEDIA_LOCATION` | 💭 自用 / 不上 Play：照做；上 Play：「最近照片」工具大概率过不了审，改成「让用户在 Photo Picker 里选」 |
| 输入框发图片 | **Photo Picker**（`PickMultipleVisualMedia(4)`，Android 11+ 系统组件，旧机经 Play 服务回退） | 无需权限 | 用它 |
| `phone_device_info` | `Build`、`BatteryManager`、`PowerManager`（省电模式、`currentThermalStatus`）、`StorageStatsManager` / `StatFs`、`ConnectivityManager`、时区语言 | 无 | 可做 |
| `phone_clipboard_get` / `set` | `ClipboardManager` | ✅ Android 10+ **只有前台（有焦点）的 App 或默认输入法能读**；Android 12+ 读别的 App 复制的内容时系统弹 toast「已粘贴自剪贴板」（不是 iOS 那种要用户点允许）；写入敏感内容可标 `EXTRA_IS_SENSITIVE` | 💭 只在前台可用；hub 在 App 后台时调用要回「请打开 Jarvis」 |
| `phone_notify` | `NotificationManager`；延迟用 WorkManager（可能被 Doze 推迟）或 `AlarmManager.setWindow`；精确闹钟要 `SCHEDULE_EXACT_ALARM`（Android 14 起默认不授予） | `POST_NOTIFICATIONS` | 可做（延迟不保证准点） |

- 💭 **Android 独有、iOS 做不到的增强**（按需评估隐私）：`NotificationListenerService`（用户在系统设置里授权后可读所有 App 的通知内容，微信等消息也能进收件箱 / 给 AI 用）、
  `UsageStatsManager`（App 使用时长）、通话记录 / 短信（`READ_CALL_LOG` / `READ_SMS`，Play 严格限制；Android 17 对 OTP 短信延迟 3 小时）。这些不上 Play 时都能做。
- 写操作照 iOS：执行前在 App 里弹确认卡片，拒绝时返回 `The user declined on the phone.`。

来源：
- Android 17 行为变化：https://developer.android.com/about/versions/17/behavior-changes-17
- Play 政策（照片 / 通讯录 / 本地网络）：https://support.google.com/googleplay/android-developer/answer/14115180
- 待办提供者现状：https://github.com/bitfireAT/davx5-ose/discussions/2740 ，https://manual.davx5.com/tasks_notes.html ，https://github.com/dmfs/opentasks
- Health Connect 数据类型：https://developer.android.com/health-and-fitness/guides/health-connect/plan/data-types
- Photo Picker：https://developer.android.com/training/data-storage/shared/photopicker
- 剪贴板：https://developer.android.com/privacy-and-security/risks/secure-clipboard-handling ，https://developer.android.com/develop/ui/views/touch-and-input/copy-paste

## 9. 测试与质量 ✅

### 9.1 单元 / UI 测试

- 💭 纯 Kotlin 的 core 模块（model / protocol / chat / terminal）用 JUnit + kotlinx-coroutines-test + Turbine；**两端共用 golden 夹具**（同一批 JSON 输入 → 期望的展示行），
  终端用 iOS 已录的 herdr 真实帧回放；transcript 相关继续以 hub 的 Go golden 测试为准。
- Compose UI 测试：`createAndroidComposeRule` + 语义树断言；NiA 的做法：不用 mock 库，Repository 都有 fake / Test 版本，用 Hilt 测试 API 替换。
- 假 hub：直接复用 `scripts/mock/mock_hub.py`（已有 mock 数据、`MOCK_STREAM` / `MOCK_TERMINAL_*` 开关），模拟器里访问宿主机用模拟器固定的宿主机别名地址（要在 network security config 里允许明文）。

### 9.2 截图测试（✅ 版本 2026-10-30）

| 工具 | 最新 | 结论 |
| --- | --- | --- |
| **Roborazzi** | **1.77.0**（2026-10-22），几乎每周发版 | **推荐**。基于 Robolectric 在 JVM 上截图，能截任意 Compose 内容（含交互后的状态）、Compose Preview 自动生成测试（`ComposePreviewTester`，1.75 起快约 1.6 倍）、UI 树 dump；NiA 在用 |
| Paparazzi | 2.0.0-alpha05.1（2026-09-28；稳定版仍是 1.3.5） | 备选：layoutlib 渲染、不需要 Robolectric；2.0 一直是 alpha，alpha05.1 才迁 AGP 9.0 |
| Compose Preview Screenshot Testing（`com.android.compose.screenshot`） | 0.0.1-alpha16 | ✅ **官方已宣布不再维护这个独立插件**，改为「AGP 9.5+ 的 AGP test suites」——AGP 9.5 目前还在 alpha，💭 暂不采用 |

- 💭 截图矩阵：5 套风格 × 浅 / 深色 × 关键页面（对照 `docs/testing.md` 的界面检查清单），Robolectric 截图做回归；**最终验收仍在模拟器上实拍**（Robolectric 的模糊 / RenderEffect / WebView 不可信）。
  注意参考图要在固定环境（NiA 是 CI 的 Linux）录，Mac 和 Linux 的字体渲染不同。

### 9.3 模拟器自动化截图（替代 iOS 的 DEBUG 启动参数）

- 💭 用 debug 专用的入口 Activity / Intent extras：`adb shell am start -n <pkg>/.debug.DebugEntryActivity --es openSession m/s --ez openWork true`，
  或深链 `adb shell am start -W -a android.intent.action.VIEW -d "jarvis://debug/open?session=m/s" <pkg>`；**这个入口只放在 `src/debug/`**（release 源集里根本没有这些类和字符串，见 10.3）。
- 配对：同理用 `--es pairURL "jarvis://pair?url=…&token=mock"` 免扫码。
- 截图：`adb exec-out screencap -p > shot.png`；深浅色：`adb shell cmd uimode night yes|no`；状态栏固定时间 / 电量：SystemUI demo 模式
  （`adb shell settings put global sysui_demo_allowed 1` 后 `am broadcast -a com.android.systemui.demo -e command clock -e hhmm 1430` 等）。
- 多个 agent 并行：各自用自己的 AVD（`emulator -avd <name> -port <偶数端口>`，`adb -s emulator-<port>` 区分），和 iOS「各用各的模拟器」一致。

### 9.4 性能

- ✅ **androidx.benchmark 1.5.0**（2026-09-09；1.6.0-alpha01）：Macrobenchmark（1.5 起 `MacrobenchmarkScope` 继承 `UiAutomatorTestScope`）、Baseline Profile Gradle 插件（1.5 起 AGP 9 不再要求 `newDsl=false`）、Perfetto v56。
- 💭 要守的指标：冷启动到「画出缓存的会话列表」、打开大会话（`turns=2`）到首屏、聊天列表快速滑动的掉帧（`FrameTimingMetric`）、打开链接到 FCP（WebView `NavigationListener`）。
  按用户约定，引用数字必须实测（≥ 10 个样本、给 min / p50 / p90 / max / mean）。线上可接 JankStats 记录掉帧。

来源：
- Roborazzi：https://github.com/takahirom/roborazzi/releases
- Paparazzi：https://github.com/cashapp/paparazzi/releases
- Compose Preview Screenshot Testing：https://developer.android.com/studio/preview/compose-screenshot-testing
- Benchmark：https://developer.android.com/jetpack/androidx/releases/benchmark
- NiA：https://github.com/android/nowinandroid

## 10. 分发、签名与开发环境 ✅

### 10.1 分发方式（对照 iOS 的 TestFlight 内部测试）

| 方式 | 现状（✅） | 适合 |
| --- | --- | --- |
| **adb 直装**（USB / 无线调试） | **不受开发者验证限制**（官方 FAQ：ADB 安装不需要注册、也不走 advanced flow 的等待期） | 开发期、自己的手机。💭 写一个 `scripts/android-deploy.sh`（构建 + `adb install -r` + 启动），对应 iOS 的 `sim.sh` |
| **Firebase App Distribution** | Gradle 插件 `com.google.firebase.appdistribution` **5.3.1**；`./gradlew assembleRelease appDistributionUploadRelease`；测试者收邮件、用内置测试 App 安装；APK / AAB 都可；构建保留 150 天、邀请 30 天内接受；支持服务账号 / `GOOGLE_APPLICATION_CREDENTIALS` 认证 | 💭 **最接近 TestFlight 内部测试的选择**，且和 FCM 共用一个 Firebase 项目 |
| Google Play 内部测试 | 最多 100 个测试者，第三方资料称上传后几分钟可用、不走完整审核（非官方说法）；要 Play Console 账号（25 美元一次性）、各类权限政策声明、target ≥ 36；2023-11-13 后注册的个人账号上正式版前要 12 人 × 14 天封闭测试（内部测试不算） | 想要 Play 管理更新时再上；注意 READ_CONTACTS、照片、健康、本地网络的政策声明（第 4、8 节） |

- ✅ **Android 开发者验证（影响「直接发 APK」）**：2026-09-30 起在巴西、印尼、新加坡、泰国的认证设备上生效；**2027 年扩展到全球所有认证设备**。
  届时未验证开发者的 APK 只能走「advanced flow」（开发者选项开关、重启、**24 小时等待**、生物识别确认）安装。2026-08 起有**免费的有限分发账号**（学生 / 爱好者，最多 20 台设备，不要证件、不要费用），
  也可以在 Android Developer Console / Play Console 完整注册（Play 上的 App 99% 自动注册）。adb 安装不受影响。
  💭 建议：2027 年前给 Jarvis 的包名 + 签名密钥注册一个有限分发账号（自用足够），Firebase 分发的包才能在新手机上顺利安装。
- 💭 构建号：`versionCode` 上限 2100000000，iOS 的 `yyyymmddHHMM`（12 位）放不下；用 `yyyymmddHH`（10 位，到 2099 年都 < 2.1e9）或 git 提交数；`versionName` 可以继续用 iOS 的格式。

### 10.2 签名

- 💭 release 签名密钥（upload key）放仓库外（如 `~/.config/jarvis/` 下的 `.jks`），路径和口令进 `config.json` 或 macOS 钥匙串，由 Gradle 从属性 / 环境变量读取——和 iOS 一样「个人的东西走配置」。
  **密钥丢了就无法给已装的 App 升级**（只能卸载重装），要备份；走 Play 时用 Play App Signing。开发者验证注册时要登记包名和签名密钥。

### 10.3 R8 与「Release 包不留调试开关字符串」

- ✅ AGP 9 的 R8：strict keep rules 默认开、只允许 `proguard-android-optimize.txt`；**AGP 9.3 起新的 `optimization { enable = true }` DSL**（一个开关同时开代码优化和优化的资源收缩、不用再写默认规则文件；
  自定义规则放 `src/<variant>/keepRules/*.keep`；用新 DSL 至少 9.3.3，修了 variant 里 `isMinifyEnabled` 不对的问题）；`./gradlew :app:analyzeReleaseR8Config` 只跑配置分析、很快。
- 💭 调试开关的做法（对应 iOS「DEBUG 启动参数仅 Debug，Release 检查不含这些字符串」）：
  - 所有直达页面 / mock / 探针（`openSession`、`showFoldProbe`、`simulateOffline`…）的解析和入口 Activity **只放在 `src/debug/`**，release 源集只有一个空实现的接口（`DebugHooks`）。
    这比 `if (BuildConfig.DEBUG)` 靠 R8 删死代码更确定。`ui-tooling`、`ui-test-manifest` 只用 `debugImplementation`。
  - 发版脚本里加检查：解开 APK，用 `dexdump` / `apkanalyzer dex` 和 `aapt2 dump strings` 扫 dex 和资源，出现任何调试开关名就失败（对应 `check_bundle_strings.py`）。
  - iOS 踩过「开关名和 Swift 标识符同名被误报」：Kotlin 的 **enum 常量名会以字符串留在 dex 里**（`name()` 要返回它），R8 混淆不掉——开关名同样要用不会撞名的前缀（如 `debug.open…`）。
  - 界面风格的 `-ui.style` 在 iOS 是 Release 也认的参数；Android 上对应 `am start --es ui.style clay`，放在 main 源集。

### 10.4 本机开发环境（macOS Apple Silicon，结合协调者核实的事实）

- ✅ **当前稳定版 AGP 是 9.5.0（2026-10）**（9.6 预览中），要求 Gradle ≥ 9.6.0、JDK 17、最高 API 37。Studio 与 AGP 的兼容表：
  **Rabbit 2（2026.2.2）支持到 AGP 9.5**；Rabbit 1 / Quail 4 到 9.4；**Panda（2025.3.x）只支持到 9.0–9.2**（Panda 4 = 9.2）；API 37.0 至少要 Panda 3 Patch 1 + AGP 9.1.1。
  而 **Compose 1.12、CameraX 1.7、Glance 1.3 都要求 AGP ≥ 9.2**。→ 💭 本机的 Studio 2025.3 要升级到 Rabbit 2；只用命令行构建时 IDE 版本不影响。
- JDK：AGP 9 最低 JDK 17；可以直接用 Studio 自带的 JBR 21（`/Applications/Android Studio.app/Contents/jbr/Contents/Home`）或 Homebrew 的 openjdk@17 做 `JAVA_HOME`；
  💭 用 Gradle 的 daemon JVM 规范（`gradle/gradle-daemon-jvm.properties`）把版本钉住，免得各个 agent 的 shell 环境不同。
- SDK：本机已有 android-36.1 / 37.0 / 37.1 平台、build-tools 37.0.0（AGP 默认 build-tools 是 36.0.0，💭 显式写 `buildToolsVersion = "37.0.0"` 或让 Gradle 自动下载 36.0.0）。
- 模拟器：arm64-v8a 镜像走 Hypervisor.Framework 加速（本机有 API 35、37.x）。无界面脚本化：
  - ✅ **新的 Android CLI（`android` 命令，面向 AI agent）**：`android emulator create/list/start/stop`，`android emulator start --headless <device>` 会**阻塞到开机完成**；
    `android screen capture`（截屏）/ `screen resolve`（UI 层级）、`android input tap|text|key|swipe|scroll`、**`android preview render`（不需要设备就把 Compose Preview 渲染成图片）**、
    `android analyze`（APK 体积、R8 指标、keep 规则警告）、`android sdk install`、`android init`（给 agent 装 `android-cli` skill，源在 github.com/android/skills）；遥测可用 `--no-metrics` 关。
    官方命令行模拟器文档已把旧的 `emulator` 工具标成 deprecated，推荐用它。💭 适合写进项目的 `AGENTS.md` / `CLAUDE.md`，让各个 agent 用同一套命令截图验证。
  - 旧办法仍可用：`emulator @<avd> -no-window -no-audio -no-boot-anim -no-snapshot-save -gpu swiftshader_indirect -port 5556`（同一 AVD 多开加 `-read-only`），
    `adb -s emulator-5556 exec-out screencap -p > shot.png`；等开机要带超时（按 CLAUDE.md：成功 / 失败 / 超时都打印一行结论）。
- 多个 agent 并行：各自 worktree（Gradle daemon 按目录分开，`~/.gradle/caches` 共用没问题）+ 各自 AVD 和端口；开 Gradle 配置缓存和构建缓存。

来源：
- 开发者验证：https://developer.android.com/developer-verification ，https://www.androidauthority.com/android-sideloading-changes-timeline-3679204/ ，https://www.megamobilecontent.com/news/2026/10/01/android-developer-verification-live-brazil-indonesia-singapore-thailand/
- Firebase App Distribution：https://firebase.google.com/docs/app-distribution/android/distribute-gradle
- Play 测试要求：https://support.google.com/googleplay/android-developer/answer/14151465
- AGP 9.5：https://developer.android.com/build/releases/agp-9-5-0-release-notes ；AGP 9.3：https://developer.android.com/build/releases/agp-9-3-0-release-notes ；Studio / AGP 兼容：https://developer.android.com/build/releases/about-agp
- 模拟器命令行：https://developer.android.com/studio/run/emulator-commandline ；Android CLI：https://developer.android.com/tools/agents/android-cli ，https://developer.android.com/tools/agents/android-cli/commands/emulator_start

---

## 11. 结论汇总

### 11.1 推荐技术栈一览（版本均为 2026-10-30 查到的稳定版，除非注明）

| 层 | 选择 | 版本 |
| --- | --- | --- |
| 语言 / 构建 | Kotlin（K2，AGP 内置 Kotlin）、Gradle 版本目录 + `build-logic` convention plugins | Kotlin 2.4.21、AGP 9.5.0、Gradle 9.8.2（≥ 9.6.0）、KSP 2.3.12、JDK 17（或 Studio 的 JBR 21） |
| SDK | compileSdk 37 / targetSdk 37 / minSdk 33 | Android 17 = API 37 |
| UI | Jetpack Compose + Material 3（自建 5 套风格 token 层） | BOM 2026.09.00（Compose 1.12.1）、material3 1.4.0（Expressive 等 1.5.0，现 beta02） |
| 玻璃效果 | Haze（磨砂）+ Backdrop（液态折射，API 33+） | Haze 2.0.1、Backdrop 2.0.1 |
| 架构 | UDF：应用级 Store / Repository → ViewModel(StateFlow) → Compose | Lifecycle 2.11.0、coroutines 1.11.0 |
| DI | Hilt（备选 Metro） | Dagger/Hilt 2.60.1（Metro 1.5.1） |
| 导航 | Navigation 3 | 1.2.0 + lifecycle-viewmodel-navigation3 2.11.0 |
| 网络 | OkHttp（REST + WebSocket）+ kotlinx.serialization | OkHttp 5.5.0、serialization 1.12.0 |
| 存储 | Room 3（或保守用 Room 2.8.5）、DataStore + datastore-tink、Tink | Room 3.0.3、DataStore 1.3.0、Tink 1.24.0 |
| 图片 | Coil 3（okhttp 网络）+ Telephoto | Coil 3.6.3（AGP 9.4+ 必须 ≥ 3.6.3）、Telephoto 0.19.0 |
| Markdown / 高亮 | mikepenz multiplatform-markdown-renderer；高亮照搬 iOS 轻量规则（可选 Highlights） | 0.46.0；Highlights 1.1.0 |
| 终端 | 自写 VT 子集网格（纯 Kotlin）+ Compose Canvas + 自建 InputConnection | — |
| WebView | androidx.webkit（异步启动、预渲染、预连、NavigationListener）+ 自绘三档浮层；OAuth 用 Custom Tabs | webkit 1.18.0 |
| 图表 | Compose Canvas 自绘（备选 Vico） | Vico 3.3.1 |
| 扫码 | Google code scanner（备选 CameraX + ML Kit） | play-services-code-scanner 16.1.0、CameraX 1.6.2 |
| 推送 / 后台 | FCM 高优先级数据消息、WorkManager；不常驻前台服务 | WorkManager 2.12.0 |
| 锁屏 / 状态栏 | Live Updates（Android 16+）+ MetricStyle（Android 17+） | 平台 API |
| 小组件 / 入口 | Glance 小组件、动态快捷方式、快捷设置磁贴、分享目标；AppFunctions 作远期 | Glance 1.2.0、AppFunctions 1.0.0-beta01 |
| 手表 | Wear OS 独立 App：Compose for Wear OS（M3）+ Data Layer + Wear Widgets | Wear Compose 1.7.1 |
| 测试 | JUnit + coroutines-test + Turbine；Roborazzi 截图；Macrobenchmark + Baseline Profile；两端共用 golden 夹具 | Roborazzi 1.77.0、benchmark 1.5.0 |
| 分发 | 开发 adb；发版 Firebase App Distribution；2027 年前注册开发者验证（有限分发账号即可） | appdistribution 插件 5.3.1 |
| 工具 | Android CLI（`android emulator start --headless`、`screen capture`、`input`、`preview render`） | — |

### 11.2 主要风险

1. **Android 17 本地网络权限**：target 37 后 Wi-Fi 下连内网 hub 要 `ACCESS_LOCAL_NETWORK` 运行时权限，没给时 TCP 表现为超时，极易误判成「hub 挂了」；VPN 不受限但需实测。上 Play 还要政策声明（2027-08-25 起强制）。
2. **开发者验证**：2027 年起全球认证设备上，未注册开发者的 APK 只能走 24 小时等待的 advanced flow；adb 不受影响。
3. **Studio / AGP 版本链**：本机 Studio 2025.3（Panda）最多支持 AGP 9.0–9.2，而 Compose 1.12 / CameraX 1.7 / Glance 1.3 要 AGP ≥ 9.2，AGP 9.5 要 Rabbit 2——需升级 IDE。
4. **液态玻璃在 Android 上只能模仿**：Backdrop 折射要 API 33+、GPU 开销大；Haze 的 glass 还是实验性；中端机掉帧风险。
5. **两套实现漂移**：不共享代码就必须靠共享 golden 夹具 + 设计 token 生成来约束；否则轮次折叠等复杂规则两端会慢慢不一致。
6. **新库的成熟度**：Room 3（稳定 4 个月）、Navigation 3（API 仍在变）、material3 1.5（beta）、biometric（新 API 全在 alpha）、AppFunctions（beta，Gemini 集成私测）。
7. **推送依赖 Google Play 服务**：去谷歌机型收不到 FCM；Live Updates 只能本地发、没有 push-to-update；FCM 高优先级消息不出通知会被降级。
8. **WebSocket 弱网判断**：OkHttp 拿不到 ping RTT，需要 hub 协议加客户端发起的应用层 ping（小改动，但涉及 hub）。
9. **Play 政策**（若上 Play）：照片 / 视频广泛访问基本不批（「最近照片」工具要改成 Photo Picker），READ_CONTACTS（2027-01-27 起）和本地网络都要声明。

### 11.3 没查清 / 需要实测的地方

- 经 VPN（Tailscale 等）访问 100.64/10 地址时本地网络权限的实际行为（官方定义排除 VPN，但 100.64/10 又列为本地网段）；需要 Android 17 真机实测。
- WebView 在 Compose 自绘浮层里的嵌套滚动 / 下拉收起手势协同。
- Backdrop 各效果的确切最低 API、性能数据（文档没写）；Haze `haze-glass` 的成熟度。
- NotificationCompat 是否已封装 `MetricStyle`；Live Updates 在各家 OEM（三星、小米等）上的实际表现。
- Vico 是否原生支持断点（发布说明没提）。
- Play 内部测试「几分钟可用、不审核」只来自第三方资料。
- AGP 9.5+「AGP test suites」做截图测试的具体用法（官方页面 404，只知道独立的 Compose 截图插件已停止维护）。
- Wear OS 7 的实际推送进度、手表直连内网 hub 的可达性。
- Kotlin Swift export 进入 Beta 的时间表（未找到）。
