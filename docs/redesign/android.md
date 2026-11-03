# Android 方案

> **2026-10-31 起，本文讲 Android 外观、布局和交互的部分已被 [Android 界面风格与设计系统](../android-design.md) 取代**：用户要求视觉和交互整体重做，做成玻璃（默认）、晨报、看板三套界面风格。功能、数据、协议、门禁的规格仍以本文为准。

> 草案 v1（2026-10-30），属于 [重设计方案](README.md)。
> 依据：[research/android-tech.md](research/android-tech.md)（选型，版本号都是 2026-10-30 查到的，附来源）、已合入 master 的 PR #84（`android/`，Agents 部分的第一版），以及 [ux.md](ux.md)。

## 1. 起点：PR #84，以及我们的取舍

### 1.1 PR #84 已经做了什么

`android/` 是单模块 `:app`，约 2.7 千行 Kotlin，没改服务端：

- **配对**：扫码（zxing）、`jarvis://pair` 深链、手动输入；换设备 token。
- **Agents 列表**：按状态排，分「需要你处理 / 在跑的 / 最近 7 天」三段。
- **对话页**：
  - `turns=2` 分页；
  - WS 订阅 + 连续性补洞（`history` / `from_seq` / `to_seq` / `covered`）；
  - 工具调用和结果合成一行，懒加载图片，自写的简版 Markdown；
  - 发消息带 `client_msg_id`，处理 `dialog_open` / `input_draft` 拒发；
  - 中断、建议、每秒读一次屏的终端小窗。
- **交互卡片**：四种都有，加密码框和「交给电脑处理」。
- **通知**：不用推送服务，前台服务（`specialUse`）一直连着 WebSocket，本地发通知。通知上能直接允许 / 拒绝、批准 / 打回（带输入），能选提问和屏幕对话框的前 3 个选项；交互在别处处理后撤回通知；一轮做完且 App 不在前台时弹「做完了」。
- **验证**：在模拟器（Android 14）上连真实 hub 验证过；真机是没有 Google 服务的国产手机（Android 16），经 USB 连 hub 验证过。有 10 个单测。

### 1.2 逐项对比

| 维度 | PR #84 | 本方案 | 结论 |
| --- | --- | --- | --- |
| 总路线 | 原生 Kotlin + Compose | 同 | 一致 |
| 工具链 | AGP 8.7.3、Kotlin 2.0.21、Gradle 8.9、Compose BOM 2024.12、compile / target 34、minSdk 26 | AGP 9.5、Kotlin 2.4.21、Gradle 9.8、BOM 2026.09、compile 37；target 先 36、配对流程做完后 37；minSdk 31（2.2 节） | **用本方案**。PR 的 target 34 恰好绕开了 Android 17 的本地网络权限，这点我们分两步走来保留 |
| 模块 | 单模块，`data` / `service` / `ui` 三个包 | 约 18 个模块，核心是纯 Kotlin（第 3 节） | **用本方案**。多个 agent 并行、纯逻辑在 JVM 上测、以后能走 KMP，都需要模块边界 |
| 架构 | Application 里一个全局 `Hub`，Compose 直接读它的 StateFlow；没有 ViewModel 和 DI | 应用级 Store → ViewModel(StateFlow) → Compose；Hilt；Navigation 3；`HubScope` | **用本方案** |
| 连续性和补洞 | 已实现，规则和 iOS 一致；排序按 (seq, 块序号) 数字比较 | 同一规则写进 `spec/fixtures` | **保留 PR 的实现**，搬进 `:core:domain` 并接上夹具测试；补上订阅引用计数、重连后核对交互、事件流溢出时重读 |
| 长会话折叠（`fold_work`） | 不支持：650 条命令的会话首页会逐行画出 1412 个 item | 必须支持（hub 已有，iOS 已用） | **补上**（P0） |
| 对话展示 | 每个工具调用一行，没有轮次折叠和工作行 | 和 iOS 同一套 `chatLines`（夹具保证一致） | **用本方案**。这是 iOS 的核心体验 |
| Markdown | 自写约 120 行，没有表格、有序列表、代码高亮，链接不能点 | mikepenz multiplatform-markdown-renderer（GFM 表格、可替换节点、流式解析） | **用本方案** |
| 存储 | 配对信息明文存 SharedPreferences；没有缓存、没有 outbox | DataStore + Tink 加密 token；Room 存缓存和 outbox；存储分级 | **用本方案** |
| 通知送达 | 前台服务常驻 WebSocket，手机自己判断「做完了」 | 原方案是 FCM 为主 | **采用 PR 的，而且是唯一的方式**（5.8 节）：按「手机没有 Google 服务」设计，不做 FCM；「做完了」改由 hub 判断后经 WS 下发 |
| 通知按钮 | 允许 / 拒绝 / 批准 / 打回（带输入）/ 提问和屏幕卡的选项；处理后撤回 | 「允许 / 批准」需要先解锁；拒绝可以带原因 | **合并**：保留 PR 的选项按钮和撤回（iOS 也该学）；补上需解锁（现在锁屏上就能允许）和拒绝原因输入 |
| 扫码 | zxing-android-embedded（不依赖 Google 服务） | 原方案写的是 Google code scanner | **采用 PR 的**：Google code scanner 需要 Google Play 服务 |
| 明文 HTTP | network security config 的 `base-config` 整体放开 | 原方案写的是「只放行配置的 hub 地址」 | **采用 PR 的**：配置是静态 XML，不支持网段，hub 地址又是配对时才知道的；另在 App 里补一道检查（5.1） |
| 状态色 | 工作中绿、完成未看蓝 | 需要你红、工作中琥珀、完成未看绿（和 iOS、`docs/agent-states.md` 一致） | **用本方案**（从 token 生成）。PR 的配色和 iOS 不一致 |
| 计划审批 | App 内「批准（acceptEdits）/ 批准并全部放行 / 打回」；通知上批准用 acceptEdits | 审批策略表 | 已经和策略表一致，保留 |
| 「看过」上报 | 只在 App 处于前台（RESUMED）时上报 `seen` | 「看过」的门禁规则 | 一致，保留 |
| 配置和脚本 | `android.application_id` 走 `config.json`；`scripts/android.sh install / apk / test` | — | **采用 PR 的**，再扩展：签名、版本号、debug 直达入口、截图脚本 |
| 测试 | 10 个 JUnit | 共享夹具 + Roborazzi + mock hub + debug 直达 | **用本方案** |
| 文档 | `docs/android.md`（现状、通知、构建、真机经验） | 本文是目标方案 | 两份都留：`docs/android.md` 继续写「现在是什么样」和踩坑经验，实现一步，更新一步；本文写目标 |

### 1.3 怎么「覆盖」：以 PR #84 为起点重组，不推倒重写

PR 的代码量小，真机验证过的部分值得保留。重组按下面三张清单来，每一步 App 都能照常使用。

| 保留（原样或小改） | 搬家（逻辑保留，挪进新模块并补测试） | 替换（按本方案重写） |
| --- | --- | --- |
| `HubService` / `Notifier` / `ActionReceiver` / `BootReceiver`（前台服务和通知）→ `:platform:notify`；zxing 扫码；`network_security.xml`；`scripts/android.sh`；`config.json` 的 `android.application_id`；`docs/android.md` 的「经验」 | `Models.kt` → `:core:contracts`；`ChatStore` 的连续性 → `:core:domain` 的 reducer（接上 `spec/fixtures/continuity`）；`ApiClient` / `Hub` → `:core:hub`（按领域拆成窄接口）；`Pairing.parse` → `:core:domain`；`toolVerb` / `toolSubject` → `:core:domain`（和 iOS 的 `ToolPresentation` 对齐） | `Root`（换成 Navigation 3 + 四个 tab）；`ChatScreen`（换成共用的列表引擎、状态行、工作行、待定状态）；`Rows`（换成 `chatLines`）；`Markdown`；`Theme`（换成生成的 token）；`AgentsScreen`（按 iOS 的分组、筛选、置顶 / 屏蔽、菜单）；`InteractionCard`（换成新的交互面板）；`LazyImage`（换成 Coil）；`TerminalPeek`（第二批换成真终端）；`PairingStore`（换成加密存储） |

### 1.4 PR #84 里值得学的（iOS 以后跟进时也该学）

1. **通知上直接选提问和屏幕卡的选项**：单选提问、屏幕对话框的前 3 个选项变成通知按钮。（以后 iOS 跟进时，要在推送前按交互动态注册通知类别，或者用通知内容扩展。）
2. **交互处理后撤回通知**：Android 收到 `interaction_resolved` 就取消对应通知。（以后 iOS 跟进时，App 运行时用 `removeDeliveredNotifications`；App 没运行时要 hub 发静默推送。）
3. **只在前台上报「看过」**：后台开着的对话页不应把「做完了」吞掉。（以后 iOS 跟进时照这条检查一遍。）
4. **国产 ROM 的引导**：设置页给出「电池优化白名单」和「自启动管理」两个入口，找不到就退回应用详情页。

## 2. 技术栈

### 2.1 版本（2026-10-30 查到的稳定版）

| 层 | 选择 | 版本 | 说明 |
| --- | --- | --- | --- |
| 语言 / 构建 | Kotlin（AGP 内置 Kotlin）、版本目录 + `build-logic` 约定插件 | Kotlin 2.4.21、AGP 9.5.0、Gradle 9.8.2、KSP 2.3.12、JDK 17（或 Android Studio 自带的 JBR 21） | AGP 9 不再单独 apply `org.jetbrains.kotlin.android`；新 DSL 默认开。本机的 Android Studio 2025.3 只支持到 AGP 9.2：命令行构建不受影响，用 IDE 要升级到 Rabbit 2 |
| SDK | compileSdk 37；targetSdk 先 36，配对流程（5.1）做完后 37；**minSdk 31** | Android 17 = API 37 | 第 2.2 节 |
| UI | Jetpack Compose + Material 3；自己的 token 层 | BOM 2026.09.00（Compose 1.12.1）、material3 1.4.0 | Expressive 组件等 1.5.0 稳定（现在是 beta）后再用；Compose 1.12 要求 AGP ≥ 9.2 |
| 架构 | 应用级 Store / Repository → ViewModel（StateFlow）→ Compose | Lifecycle 2.11.0、coroutines 1.11.0 | 不用 MVI 框架 |
| DI | Hilt | 2.60.1（要求 AGP 9） | 备选 Metro 1.5.1。core 模块只用构造函数注入，绑定写在 `:app` |
| 导航 | Navigation 3 | 1.2.0 + `lifecycle-viewmodel-navigation3` | 返回栈就是一个列表；支持深链（通知、debug 直达）、列表-详情双栏（折叠屏和平板） |
| 网络 | OkHttp（REST + WebSocket）+ kotlinx.serialization | OkHttp 5.5.0、serialization 1.12.0 | 不用 Retrofit：接口少，手写一层就够 |
| 存储 | Room 2（保守）、DataStore + `datastore-tink`、Tink | Room 2.8.5、DataStore 1.3.0、Tink 1.24.0 | Room 3（3.0.3）只稳定了 4 个月，改了包名，AI 训练语料里几乎都是 Room 2 的写法：先用 2.8，迁移主要是改 import。`security-crypto` 已整体废弃，不用 |
| 图片 | Coil 3（OkHttp 网络层）+ Telephoto（缩放） | Coil 3.6.3（AGP 9.4+ 必须 ≥ 3.6.3）、Telephoto 0.19.0 | 自定义 `Fetcher` 按 `ref` 取对话图片，永久缓存 |
| Markdown / 高亮 | mikepenz multiplatform-markdown-renderer；高亮照搬 iOS 的轻量规则 | 0.46.0 | 表格、任务列表、可替换任意节点（工具卡、上下文引用卡、路径标签）、助手逐字输出用流式解析 |
| 媒体 | 不引入播放器库（Media3） | — | 飞书的视频、语音消息不在 App 里播，显示成卡片，点开交给飞书（5.14） |
| 扫码 | zxing-android-embedded | 4.3.0（PR #84 的选择） | 不依赖 Google 服务；以后要 Compose 原生的取景界面，再换 CameraX + ML Kit 内置模型 |
| WebView | androidx.webkit | 1.18.0 | `startUpWebView` 预启动、`prerenderUrlAsync` 预渲染 |
| 图表 | Compose Canvas 自绘 | — | 断线、裁剪、按住看某一时刻都要自己控制（Vico 不支持断点） |
| 后台 | WorkManager | 2.12.0 | outbox 补发（联网约束）、加急任务 |
| 小组件 | Glance | 1.2.0 | 第二批 |
| 测试 | JUnit、kotlinx-coroutines-test、Turbine、Compose UI 测试、Roborazzi | Roborazzi 1.77.0 | 截图参考图在固定环境录 |
| 性能 | Macrobenchmark + Baseline Profile、JankStats | androidx.benchmark 1.5.0 | — |

### 2.2 SDK 级别

- **minSdk 31**（Android 12），依据见 [research/android-cn-roms.md](research/android-cn-roms.md) 第 1 节：
  - 覆盖：按 StatCounter 中国 2026-09 的页面浏览量算约 91%（Google 全球的 Play 活跃设备口径约 78%）。没有一份数据是「中国大陆手机的设备数」，国内统计平台没有 2026 年的公开数据。
  - 包括还能装 APK 的鸿蒙 3 / 4：它们的 Android 兼容层就是 API 31（中国「Android 12」占比 17%、明显高于全球的 8%，多半是这批华为手机）。minSdk 33 会把它们和所有 Android 12 手机挡在外面。
  - 通知按钮的「需解锁」（`setAuthenticationRequired`）从 31 起才有，这是「允许 / 批准」放在锁屏通知上的安全前提；动态取色、`RenderEffect` 也从 31 起有。
  - 从后台启动前台服务的限制也是 31 起才有，常驻连接最容易出问题的这块只写一套规则。
  - 31 以下（中国约 9%，多是 2020 年前后的机型）：如果要装的人里确实有，退到 29，代价是锁屏上不放「允许 / 批准」、没有动态取色，多测一套前台服务的启动规则。
- **targetSdk 分两步**：
  - 先 36。Android 17 要求 target 37 的 App 申请本地网络权限，target ≤ 36 时这个权限由 INTERNET 隐式授予，官方还要求低 target 不要申请它。先停在 36，PR #84 在 Wi-Fi 下直连内网 hub 的行为不变。
  - 配对流程（5.1）把权限申请、被拒时的状态和提示做完之后，再一起升到 37。
  - 不上 Play 时没有强制的 target 要求，但不该长期停在旧版本。
- **compileSdk 37**：Compose 1.12 要求。

## 3. 工程结构

```
android/
├─ settings.gradle.kts / gradle/libs.versions.toml / gradle/gradle-daemon-jvm.properties
├─ build-logic/convention/   jarvis.android.application / .library / .compose / jarvis.jvm.library / jarvis.hilt / jarvis.screenshot-test
├─ core/
│  ├─ contracts/             纯 Kotlin：线上类型（@Serializable，显式 @SerialName）、WS 事件、能力位、错误码、窄接口
│  ├─ domain/                纯 Kotlin：连续性 reducer、fold、items → Rows → Blocks → Turn → Lines、冻结快照、outbox 状态机、
│  │                         排序 / 分组 / 同伴 / 收件箱选择器、审批策略表、标签解析、屏幕对话框、格式化、Fleet 文案
│  ├─ terminal/              纯 Kotlin：VT 子集网格、折行、滚动检测、键编码、修饰键、帧序 / 接管状态机
│  ├─ hub/                   JVM：OkHttp 的按领域拆的客户端、WebSocket、连接监视（网络状态由 Android 层注入）、错误分类
│  ├─ data/                  Android：Room（items、游标、outbox、op 日志、草稿、缓存快照）、DataStore + Tink、同步引擎、HubScope
│  └─ designsystem/          Android：生成的 Tokens.kt、JarvisTheme、组件（ux.md 第 4 节）
├─ feature/                  chat、agents、assistant、inbox、terminal、readers、machines、settings
├─ platform/                 notify（前台服务、通知、Live Updates）、phonetools、shortcuts（磁贴、快捷方式、分享目标、小组件）
├─ app/                      组合根（Hilt）、NavDisplay、四个 tab、深链；src/debug：直达入口、组件目录、mock 开关
└─ benchmarks/               Macrobenchmark、Baseline Profile
```

- 依赖规则和 iOS 相同（[README.md](README.md) D2）：
  - feature 只依赖 `core:contracts`、`core:domain`、`core:designsystem`，以及 `feature:readers`；
  - feature 不依赖 `core:hub`、`core:data`，服务通过接口注入。
- **纯 Kotlin 模块**（`contracts` / `domain` / `terminal`）用 `jarvis.jvm.library` 插件，不依赖 `android.*`。在 JVM 上跑 `spec/fixtures` 夹具测试，几秒钟出结果；以后转 KMP 只改插件。
- **配置**沿用 PR #84：`~/.config/persona/config.json` 的 `android.application_id`（没写就用 `app.bundle_id`）。新增两项，写进 `docs/config.md` 和 `config.example.json`，不进仓库：
  - `android.signing`：keystore 路径、别名，口令从钥匙串或环境变量读；
  - `android.avd`：截图用的 AVD 名。
- **脚本**：
  - `scripts/android.sh` 扩展出 `lint`（ktlint / detekt / Android lint）、`shots`（启动 AVD、走 debug 直达入口截深浅色图）、`release`（签名包 + 调试开关字符串检查）。
  - 截图和自动化用新的 Android CLI，或者 `emulator -no-window` + `adb`。

## 4. 架构规范

- **单向数据流**：
  - 应用级的 Store / Repository 持有真相，比如每个会话一个 `ChatStore`。后台同步要在页面没打开时也能写它，所以不放进 ViewModel。
  - ViewModel 只派生页面状态：调 `:core:domain` 算出展示行，在 `Dispatchers.Default` 上跑，产出不可变列表，按 `version` 缓存。
  - Compose 用 `collectAsStateWithLifecycle` 收。
- **作用域**：
  - 连接、同步、outbox 跑在 `HubScope` 的 `CoroutineScope(SupervisorJob())` 上，换 hub 或重新配对时整体取消；
  - 页面任务跑在 `viewModelScope` 上。
- **线程**：JSON 解码、展示行派生、Markdown 解析放 `Default`；磁盘 IO 放 `IO`。调度器都注入，测试时换成 `StandardTestDispatcher`。
- **取消不是错误**：`CancellationException` 一律重新抛出，不展示。`runCatching` 要先把它重新抛出去。
- **Compose 性能**：
  - 行模型用 `@Immutable` 数据类 + `kotlinx.collections.immutable`；
  - LazyColumn 每行给稳定的 `key` 和 `contentType`；
  - 每帧变化的值（顶栏收起进度）只在 `graphicsLayer {}` / `offset {}` 的 lambda 里读；
  - 从消息派生的东西（Markdown AST、链接识别）放在组合外按 id 的 LRU 缓存里；
  - 定期跑 Compose 编译器报告，查不稳定的参数。
- **导航**：
  - Navigation 3。每个 tab 一条返回栈；对话页的文件 / Git 用页内的分段切换，不进返回栈。
  - 深链（`jarvis://pair`、`jarvis://open?machine=&session=`）和通知点开走同一个 `Router`。
  - 宽屏用列表-详情双栏，对应 iOS 的 iPhone Duo 双栏。

## 5. 关键子系统

### 5.1 配对与连接：公网直连、多地址选路、证书钉扎（P0）

手机经公网直连 hub，优先 IPv6；VPN 用户自己配，App 不管（依据：[research/public-direct-ipv6.md](research/public-direct-ipv6.md)，hub 侧见 [hub-changes.md](hub-changes.md) 第 1 节）。

- **配对**：
  - 二维码格式不变：`url=` 是内网明文地址，`token=` 是 hub 发的短时临时 token。新参数有 `hub`（hub_id）、`fp`（证书公钥指纹）、`tls`（TLS 端口）、`alt`（一两个外部候选）。
  - 解析后把 `hub_id`、`fp`、候选列表存进加密存储，用临时 token 换设备 token。
  - 旧 hub（没有 `tls` 能力）只能内网明文，设置里提示「hub 版本太旧，不能公网直连」。
  - 已配对时打开配对链接，先显示确认页（地址、指纹），不静默替换。
- **候选地址**：
  - 来自二维码、鉴权后的 `GET /api/v1/endpoints`、WS 的 `endpoints` 事件；
  - 类型有公网 IPv6、DDNS 名字、内网、公网 IPv4（hub 确认可达才发）；
  - 每项带端口和协议。
- **选路**（参考 Happy Eyeballs v2 / RFC 8305 的思路）：
  - 按「公网 IPv6 → 域名 → 内网 → 公网 IPv4」排好，每隔 250ms 多发起一个，并发探测 `GET /api/v1/health`；
  - **TLS 公钥钉扎通过、`hub_id` 也对得上，才算连上**。旧前缀可能已经分给了别人，明文连过去会把 token 交给陌生人。
  - 赢家按网络缓存（Wi-Fi 的 SSID 或网络 id / 蜂窝），下次先试它；
  - 网络变化、断线、收到新候选时重新探测，定期再检查有没有更好的路径。
  - OkHttp 5 的 fast fallback 只在同一个主机名的多个 IP 之间赛跑，不同候选地址要自己赛跑。
- **TLS 钉扎**：
  - hub 专用一个 `OkHttpClient`，配自定义 `X509TrustManager`，只认叶子证书的 SPKI SHA-256 等于 `fp`（或 hub 预告的 `fp_next`）；地址是 IP、会变，所以主机名校验也交给钉扎。
  - REST、WS、图片、上传、下载都走它。
  - `network_security.xml` 的 `<pin-set>` 和 OkHttp 的 `CertificatePinner` 都是在系统信任链校验通过之后才比对，自签证书过不了前一步，所以只能在代码里做。
  - WebView 不直接加载 hub 的资源。
- **明文**：
  - `base-config` 整体放开（PR #84 的做法），保留给内网和调试（回环 + 明文 + token，`adb reverse` 和 mock hub 都靠它）。
  - App 里检查：`http://` 只用于内网、VPN 网段、回环和 `.local`；**公网地址绝不用 `http://` 发 token**。
- **本地网络权限**（升 target 37 时）：
  - 按官方定义和 Android 17 的源码，手机所在网段都算本地网络。在家里 Wi-Fi 上，连内网地址、连同一个 /64 里 hub 的**公网 IPv6**，都要运行时权限 `ACCESS_LOCAL_NETWORK`，改连公网 IPv6 绕不开。经路由器回环连公网 IPv4 不算本地。这几条都要真机验证。
  - 没授权时 TCP 表现为**超时**。所以在家时先判断候选是不是本地网络、权限有没有，再决定：申请权限，或者先用不算本地的候选。
  - 状态上把「被权限拦了」和「hub 不可达」分开显示，前者给「去授权」，不能显示成一直「连接中…」。
  - 测试时可以在 Android 16 上用 `adb shell am compat enable RESTRICT_LOCAL_NETWORK <包名>` 模拟。
- **错误分类**（[ux.md](ux.md) 3.7）：

  | 异常 | 归类 |
  | --- | --- |
  | `UnknownHostException` | 找不到地址 |
  | `ConnectException` | 被拒绝 |
  | `NoRouteToHostException` | 不可达 |
  | `SocketTimeoutException` | 超时（在家时先排除本地网络权限） |
  | `SSLHandshakeException` / `SSLPeerUnverifiedException` / 自定义校验失败 | 证书不符（重新扫码） |
  | HTTP 401 / 403 | 配对已失效 |
  | `code=incompatible`（统一错误体）或缺必要的 features | hub 太旧 |
  | `code=tls_required` | 走了公网明文（不该发生，按客户端缺陷处理） |

- **WebSocket**：
  - 连上后先发 `client_hello`（平台、版本、能力、心跳协商，[hub-changes.md](hub-changes.md) 1.6、3.3），再重新订阅打开的会话（`since_seq` = `covered`），然后对账待处理交互（PR #84 少了这一步）。
  - 弱网判断靠**应用层 ping**（[hub-changes.md](hub-changes.md) 3.8）：OkHttp 没有手动 ping 的 API，也没有 pong 回调。
  - 退避 1→20 秒并带抖动；网络恢复（`registerDefaultNetworkCallback` 的 `onAvailable`）立即重新选路。
  - 收到 401 不再无限重连，提示重新配对：现在两端都会无限重连，被撤销的设备会一直刷 hub 的鉴权失败。
- **连接状态**：iOS 的四态，加 `needsLocalNetworkPermission` 和 `certificateMismatch`，1.5 秒去抖（[ux.md](ux.md) 5.5）。
- **连接诊断页**（设置 › 连接）：
  - 当前路径（公网 IPv6 / 域名 / 内网 / 公网 IPv4）和往返时间；
  - 每个候选最近一次的结果；
  - 手机有没有 IPv6（`LinkProperties` 里有没有全局 IPv6 地址和默认路由）、是不是经 VPN、是不是在强制门户后面；
  - 证书指纹；
  - 当前的心跳间隔；
  - 最近一次失败的分类；
  - 「重新探测」。
- **地址变了以后**：家宽重新拨号时前缀会变，连接当场断开。手机按顺序找回：hub 推过的新候选 → DDNS 名字 → 内网地址。没有域名又不在家时可能找不回来，诊断页明确提示「建议给 hub 配一个 DDNS 名字」。

### 5.2 同步与存储（P0）

- **会话 items**：
  - 连续性规则用 `:core:domain` 的 reducer，接上 `spec/fixtures/continuity`；
  - 请求带 `fold_work`：首页把长串工作折成 fold item，点开时用 `from_seq` / `through_seq` 取回来替换；实时推送和补发不折叠；
  - 订阅按引用计数。
- **Room 表**：
  - `items(scope, machine, session, seq, part, id, json)` 和会话游标 `cursors(scope, machine, session, history, covered, rev, has_more)`：打开会话时先画库里最近两轮，后台同步按 `after_seq` 增量写；
  - `outbox`：消息和飞书回复；
  - `ops`：一次性操作的日志；
  - `drafts`：带版本号；
  - `snapshots`：机器、会话列表、交互、收件箱、飞书会话最近 80 条。
- **加密存储**：
  - 设备 token 用 DataStore + `datastore-tink` 的 `AeadSerializer`，密钥集由 Android Keystore 主密钥包裹；
  - 加密文件要在 `dataExtractionRules` 里排除出云备份和设备迁移，因为 Keystore 的密钥不随备份走，恢复到新手机后会解不开；恢复后重新配对。
  - 保留 PR #84 的 `allowBackup=false`。
- **作用域**：所有表都带 `scope = hub_id + 配对代次`。换 hub 时整体删掉旧数据。
- **后台同步**：只在前台、且连着 hub 时跑，规则同 iOS 的 `AgentSync`：活着的 Agent + 24 小时内用过的会话，最多 40 个，并发 3。

### 5.3 聊天列表（P0）

- `LazyColumn(reverseLayout = true)`，最新一条是 index 0。`scrollToItem` 按实测高度定位，不按估算，所以没有 iOS 那种跳到没渲染区域的白屏。
- 展示行来自 `:core:domain` 的 `chatLines`：每条消息、每个工作步骤都是独立的行，有稳定的 key 和 contentType。
- 冻结快照照搬 iOS：行 id 会随轮次推进变化，按 key 锚定会失效，所以看历史时按离开最新端时的 item 快照来画。
- 进入定位、贴底跟随、阅读位置、已读门禁、「回到最新」的计数口径都按 [ux.md](ux.md) 3.5：
  - 进入定位分两步：先 `scrollToItem`，下一帧读 `layoutInfo` 修正；
  - 贴底跟随只由用户拖动改变：用 `interactionSource` 区分用户拖动和程序滚动；
  - 已读和「看过」的门禁：`snapshotFlow` 读可见项，450ms 合并。
- 输入区放在列表下面的 Column 里，`imePadding()`。
- **不用 Paging 3**：按轮分页、跨页折叠、和实时推送合并，都要自己的 `ChatStore` 来做。

### 5.4 Markdown 与代码（P0）

- mikepenz 渲染器 + 自定义节点：
  - 代码块：轻量高亮（规则照搬 iOS 的 `LarkSyntax`，两端共用夹具），加复制按钮，横向滑动；
  - 表格：横向滑动；
  - 链接：交给 `Router`，在 App 内打开；
  - 文件路径：识别成可点的标签；
  - `<jarvis-context>`：画成引用卡；
  - `<attached_images>`：画成方形缩略图。
- 解析结果按消息 id 缓存，消息加载后在后台预热。助手逐字输出用流式解析，只重解析末尾。
- 文字选择用 `SelectionContainer`，可以在原地跨段落选。长按操作条里不需要「选择」这一项。

### 5.5 工具卡片、diff、等宽文本（P0）

- 工具卡：图标、动词、主语的映射表放 `:core:domain`，和 iOS 的 `ToolPresentation` 共用夹具。PR #84 的 `toolVerb` / `toolSubject` 并进来。
- diff：行模型放 `:core:domain`（新旧行号、± 符号、只给符号和行底色上色）。超过 40 行先显示开头加「展开全部」。
- 横竖都能滑的等宽文本：外层 `horizontalScroll`，里面放一个宽度等于最长行实测宽度（`TextMeasurer` 量）的 `LazyColumn`，每行一个不折行的 `Text`；行号列放在横滑区外面固定住。大文件、大 diff 也只组合可见的行。

### 5.6 链接浮层与 WebView 池（P1）

- M3 的 `ModalBottomSheet` 是独立窗口，背后不能操作。所以在对话页上叠一个同窗口的自绘浮层：`AnchoredDraggable` 三个停靠位（铺满 / 60% / 隐藏）。停在 60% 时只有浮层区域接触摸，上面的对话照常滚动和输入。
- WebView 不接 Compose 的嵌套滚动：下拉收起靠顶部把手或标题栏；也可以让网页在滚到顶（JS 报 `scrollY == 0`）时把下拉交给浮层。要实测。
- 预加载：
  - App 空闲时 `WebViewCompat.startUpWebView` 预启动；
  - 最可能点开的那个链接 `prerenderUrlAsync`；
  - 池子常驻 2–3 个 WebView（LRU，比 iOS 的 6 个少，Android 的 WebView 更吃内存）；
  - 滚动中不创建 WebView。
  - 这些接口按 `WebViewFeature.isFeatureSupported` 检测，不支持就退回普通加载：没有 Google 服务的手机上，系统 WebView 由厂商更新，版本可能较旧（5.18）。
- 选中文字后加「AI 修改 / 作为上下文」：重写 `startActionMode`，往菜单里加项，用 `evaluateJavascript` 取选中的文字。
- 禁止内嵌 WebView 的 OAuth 页面走 Custom Tabs，手机上没有支持它的浏览器时退回系统浏览器。另给「在浏览器打开」。

### 5.7 交互面板与完整页（P0）

- 面板贴在输入框上方，最多占 55% 高度，内部滚动。完整页用全屏的 `ModalBottomSheet`。规则全部按 [ux.md](ux.md) 3.4：拒绝原因、意见和批准互斥、倒计时、来源、按请求保存草稿、「交给电脑处理」。
- PR #84 现在把交互卡片当成列表的第一行（也就是对话最下面），重组时改成固定在输入框上方的面板。好处是滚动看历史时，待处理的请求一直看得见。

### 5.8 通知：只走常驻连接（P0）

按「手机没有 Google 服务」设计，不做 FCM，也不接厂商推送（要开发者账号和实名认证）。通知只有一种送达方式：

- **原理**：前台服务（`specialUse`）一直连着 hub 的 WS，收到事件就在手机上发通知（PR #84 已有）。手机工具的调用也直接送达，不需要唤醒。
- **代价**：
  - 通知栏常驻一条；
  - 耗电**未实测**，A1 要测；
  - 国产系统要手动放开自启动和电池限制（5.18）；
  - 锁屏后长时间的后台连接还没在真机上测。

通知规则：

- **通知渠道**：需要你（高）、做完了（默认）、Persona（助手的 `notify_user`）、实时状态（低）。用户可以在系统设置里分别调。
  - 渠道的级别建好以后 App 就改不了，只有用户能改。PR #84 的「后台连接」渠道是 `IMPORTANCE_MIN`，按规则不能提升成 Live Update（5.10）。重组时新建一个 `IMPORTANCE_LOW` 的「实时状态」渠道（不响、能提升），删掉旧渠道。
  - 常驻通知用 `setForegroundServiceBehavior(FOREGROUND_SERVICE_IMMEDIATE)`：Android 12 起系统默认最多等 10 秒才显示前台服务的通知。
  - Android 14 起用户可以划掉前台服务的通知，服务本身不受影响。被划掉后，下次状态变化时重发。
- **各家系统的默认值**（[research/android-cn-roms.md](research/android-cn-roms.md) 第 3 节）：
  - 小米：悬浮通知、锁屏通知默认关，App 查不到这两个开关（小米官方说没有接口）；
  - 华为鸿蒙 4：没申请「自分类权益」的 App，本地发的通知按「资讯营销」处理：静默（没有横幅、锁屏、声音），每天只有 2–5 条配额。这项权益在华为应用市场后台申请，不上架能不能申请没查到。首版只能引导用户关掉「静默通知」、打开横幅和锁屏；
  - vivo：第一次打开时系统弹一次通知授权，拒绝了就全关，只弹一次。
  - 所以「需要你」的提醒同时显示在常驻通知的计数上，不只靠一条弹出的通知。准备清单逐项引导（5.18）。
- **按钮**：
  - 授权的「允许」、计划的「批准」要先解锁（`setAuthenticationRequired(true)`）；「拒绝」「打回」不用。
  - 提问和屏幕对话框的选项按钮也要先解锁：它们和「允许」一样让 Agent 往下做，屏幕对话框的选项常常就是命令审批（2026-10-31 定，`spec/fixtures/approval/unified.json`）。
  - 「拒绝」「打回」都能用 `RemoteInput` 直接输入原因或意见。
  - 单选提问、屏幕对话框的前 3 个选项也做成按钮（PR #84 已有）。
  - 通知上批准计划只能「只自动接受编辑」（审批策略表）。
- **结果**：按钮在后台直接调 resolve。失败时（已被处理、连不上 hub）发一条通知说明原因，不用 Toast，因为后台的 Toast 不可靠。
- **撤回**：收到 `interaction_resolved` 就取消对应的通知（PR #84 已有）。
- **「做完了」由谁判断**：PR #84 现在是手机自己判断「工作中 → 完成未看」，和 hub 的推送策略（等 60 秒、只推长任务、后台任务结束才推、在电脑上看过就不推、助手已经主动通知过就不推）不一致。改成 hub 判断：hub 在决定给 iPhone 发 APNs 的同一个地方，经 WS 给常驻连接的设备发 `{"type":"notify",...}`（[hub-changes.md](hub-changes.md) 3.9）；手机只负责显示。按设备的通知偏好（`/devices/{id}/prefs`）同样生效，设置页和 iOS 一致。
- **前台抑制**：App 在前台、而且正在看这个会话时，不弹通知。
- **国产系统引导**：沿用 PR #84 设置页的两个入口（电池优化白名单、自启动管理），扩展成按系统、按版本的引导（5.18），并加进配对后的准备清单。

### 5.9 后台

- **前台服务**：
  - 类型 `specialUse`，不是 `dataSync`：target 35+ 的 `dataSync` 在后台 24 小时内合计最多 6 小时，`specialUse` 不受这条限制。
  - 有前台服务的进程在 Doze 里照样能联网、持锁（AOSP 源码）；Android 15–17 都没有针对 `specialUse` 的新限制。风险在厂商的冻结和断网开关（5.18）。
  - 网络变化时立即重新选路、重连。
- **进程被杀以后怎么回来**：Android 12 起，App 在后台不能启动前台服务，只有十几种豁免。**`START_STICKY` 不在豁免里**：进程被杀后系统在后台重启服务，`startForeground` 会抛 `ForegroundServiceStartNotAllowedException`。所以：
  - `startForeground` 一律 try/catch，失败就 `stopSelf()`，等下一个豁免：用户打开 App、点通知、开机（`BOOT_COMPLETED`，`specialUse` 不受 Android 15 的开机限制）、App 升级后（`MY_PACKAGE_REPLACED`）。
  - **关掉电池优化本身就是豁免**：之后随时能从后台起服务。这是准备清单里最重要的一项，不只是为了 Doze。
  - 一个周期 WorkManager 任务（最短 15 分钟）检查服务在不在，不在就起；只有关了电池优化时才起得来，进程被杀后任务能不能跑还取决于各家的自启动开关。
  - 用户在「活跃应用」里点了停止（Android 13+）：系统不给任何回调。下次打开时 `ApplicationExitInfo` 里是 `REASON_USER_REQUESTED`，这时不偷偷拉起，提示「后台连接被你停止了」并给「恢复」。
  - 被杀的原因（`getHistoricalProcessExitReasons`）记进本地日志，显示在连接诊断页。
- **其他平台限制**：Android 16 起前台服务运行时 JobScheduler 也受配额约束，outbox 的 WorkManager 补发不能指望不受限（平时消息直接走常驻连接）；Android 17 起按设备内存给 App 设上限，常驻进程要控制缓存和图片的内存。
- **心跳由 hub 发起，前台和后台分开**（[hub-changes.md](hub-changes.md) 1.6）：
  - **原因**：Android 深睡时，OkHttp 的客户端 ping 不会触发（它的计时用 `System.nanoTime`，深睡时停走）；后台能用的闹钟（`AllowWhileIdle`）最快 9 分钟一次。所以后台的心跳只能靠 hub 发 ping 来维持，手机被网络包唤醒时回 pong。
  - **后台**：协商间隔从 4 分钟起，按网络自动调整，抖动只往下取。每次被唤醒，检查「距上次收到 hub 的数据」是否超时，超时就立即重连。
  - **前台**：20 秒，加上应用层 ping 判断弱网。
  - App 进出前台时，用新的 `client_hello` 改协商值。OkHttp 的 `pingInterval` 建好后改不了，前台和后台用两个 client 设置，或者干脆关掉客户端 ping、全靠 hub。
  - 运营商 NAT 和防火墙的空闲超时、各种间隔下的耗电都**没有实测**，A1 要在真机上测（第 10 节）。
- **outbox 补发**：WorkManager，带联网约束。只补「明确没发出去」的条目，同一个 id。
- **自检**：没有一家国产系统提供「查自启动、查后台白名单」的接口，设了没设只能靠结果判断：
  - 准备清单里的「锁屏测试」：锁屏 30 秒后 hub 发一条测试通知，问用户响没响、亮没亮屏；
  - hub 记录每台设备 WS 断开的时间段（[hub-changes.md](hub-changes.md) 3.10），诊断页显示「过去 24 小时后台断开 N 次，最长 X 分钟」。A1 的过夜存活实测也用这份记录。

### 5.10 锁屏常驻通知 / Live Updates（P1）

- 前台服务的那条通知本来就在。**把它和 Fleet 汇总合成一条**：平时显示「已连接 hub」；有 Agent 在工作或需要你时，显示三个数和第一条可处理的请求。数据直接用 hub 已有的 WS `fleet` 事件，hub 不用改。
- 系统版本不同，形态不同：
  - **Android 17**：用 `Notification.MetricStyle`，最多 3 个指标，正好放「需要你 / 工作中 / 完成未看」，其中「需要你」设为关键指标。有活动时请求提升为 Live Update（`setRequestPromotedOngoing(true)`），状态栏胶囊放 `setShortCriticalText("⚙3")`（6 个字以内最稳）；没有活动时取消提升。
  - **Android 16**：BigText 样式的 Live Update。
  - **更低版本**：普通常驻通知。
  - 国产系统（[research/android-cn-roms.md](research/android-cn-roms.md) 第 5 节）：ColorOS 16+ 的流体云官方表态兼容标准接口；澎湃 OS 3.1+ 的超级岛只有社区实测；OriginOS 国行版、MagicOS 没确认；鸿蒙 4 是 API 31，没有这套接口。MetricStyle 在国产系统上什么样，完全没资料，要真机截图。
  - 各家的私有接口（小米焦点通知、OPPO、vivo 合作接入、荣耀和华为的私有 extras）都要白名单或是非公开协议，首版不碰。
  - 常驻通知要换成 `IMPORTANCE_LOW` 的新渠道才能被提升（5.8）。
- 锁屏隐私：设 `VISIBILITY_PRIVATE`，另给一个只有计数的公开版本，对应 iOS 的「锁屏显示详情」开关。
- 风险：官方的用途规范不鼓励「别人触发的活动」和提醒类内容，厂商可能加限制，用户也能一键关掉。所以「需要你」的提醒始终走普通的高优先级通知，不依赖 Live Updates。

### 5.11 终端（P1）

- `:core:terminal` 移植 iOS 的 `TerminalGrid` / `TerminalWrap` / 键编码 / 修饰键，用 iOS 已有的真实录制帧（`ios/JarvisTests/Fixtures/terminal-*.jsonl`，搬进 `spec/fixtures/terminal/`）做两端共用的测试。
- 渲染：Compose Canvas 按格子画（宽字符两格，框线和块字符画成线和矩形），`TextMeasurer` 按 run 缓存。
- 输入：自建 `InputConnection`（`PlatformTextInputModifierNode`，或 AndroidView 里自定义 View 的 `onCreateInputConnection`），正确处理 `setComposingText` / `finishComposingText` / `commitText`（对应 iOS 的 marked text）。输入类型用 `TYPE_CLASS_TEXT | TYPE_TEXT_FLAG_NO_SUGGESTIONS`，不用 `TYPE_NULL`：后者会让中文输入法组字出问题。
- 修饰键（单击粘滞、双击锁定、按住）、滚轮转发、双指切 Agent，都在 `pointerInput` 里自己判断。
- 不用现成库：Termux 的终端库许可证有灰区；ConnectBot 的 termlib 要的是程序的原始字节，而 herdr 给的是已经仿真好的网格，用不上。
- 首版保留 PR #84 每秒读屏的终端小窗，作为过渡。

### 5.12 手机工具（P1）

| 工具 | Android | 说明 |
| --- | --- | --- |
| 通讯录 | `ContactsContract` | 要 `READ_CONTACTS`；系统的 Contact Picker 不支持关键词搜索，用不上 |
| 日历（读、建） | `CalendarContract` | 新建时要选一个可写的日历。没有 Google 账号时可能一个可写的都没有：用 sync adapter 身份建一个「Persona」本地日历；各家日历 App 显不显示它要真机看 |
| 提醒事项 | **不注册** | Android 没有系统级的提醒事项接口 |
| 位置 | `LocationManagerCompat.getCurrentLocation`，按 `getProviders(true)` 选：12+ 先试平台的 `fused`，再 GPS、网络（不用依赖 Google 服务的 Fused Location），带超时；`Geocoder` 先 `isPresent()` | 国行的网络定位靠厂商服务，可能拿不到；`Geocoder` 可能没有后端，只回坐标。结果写明坐标系（GPS 是 WGS-84，国内地图用 GCJ-02） |
| 健康 | Health Connect，**国行手机首版不注册** | Health Connect 要 Google Play 服务；国行的健康 App（OPPO 健康等）不往里写数据，华为用自己的 Health Kit。先 `HealthConnectClient.getSdkStatus()`，不可用就不注册这个工具 |
| 照片 | `MediaStore`（最近的元数据）+ 取图缩放 | Android 14+ 可以只授权部分照片 |
| 选图（发消息时） | AndroidX `PickVisualMedia` | 33+ 用系统照片选择器；31–32（含鸿蒙 4）系统选择器要 Google 系统更新推送的模块，国行大多没有，自动退回文件选择器，能用但界面不同 |
| 设备信息 | `Build`、`BatteryManager`、`PowerManager` 等 | — |
| 剪贴板 | `ClipboardManager` | **只能在前台读**，后台调用时回「请打开 Persona」；小米、ColorOS 还有自己的剪贴板权限，读到空时提示去系统设置里看 |
| 通知 | `NotificationManager` + WorkManager / AlarmManager | 延迟发送不保证准点 |

- 工具名和参数和 iOS 保持一致；说明文字按 Android 写。
- 写操作照 iOS：执行前先确认。在前台弹卡片；在后台就发一条高优先级通知，带「允许 / 拒绝」按钮。
- hub 要按设备注册和路由工具（[hub-changes.md](hub-changes.md) 2.1），否则会和 iPhone 互相覆盖。
- 常驻连接模式下，后台定位要 `ACCESS_BACKGROUND_LOCATION`，或者前台服务带 `location` 类型，要评估：默认只在前台定位，后台调用时回「请打开 Persona」。

### 5.13 主题（P0）

- 默认风格是「原生（Material You 动态取色）」，对应 iOS 的液态玻璃。PR #84 已经用了动态取色。
- 状态色、语义色、圆角、间距、字体角色全部来自生成的 `Tokens.kt`，不随风格变的状态色要和 iOS 一致（PR #84 的状态色要改）。
- Claude / 黏土 / Island / 飞书四套第二批再做：
  - 软块阴影和高光用 Compose 1.9+ 的 `Modifier.dropShadow` / `innerShadow`；
  - 发丝描边用 `border(Dp.Hairline)`；
  - 磨砂可以用 Haze。
- 不做液态玻璃：要用 AGSL，要 API 33，GPU 开销大，也不是 Android 的设计语言。

### 5.14 图片与媒体（P0 / P1）

- 对话图片：Coil 3 自定义 `Fetcher` + `Keyer`，按 `ref` 永久缓存。行里先按 `width / height` 占好位置（`aspectRatio`），滚到屏幕上才取图。PR #84 现在的做法要换掉：每次都整张解码、不缓存、固定 120dp 占位。
- 全屏查看器（P1）：`HorizontalPager` + Telephoto，浏览整个会话的图片。
- 飞书媒体（第二批）：图片用 Coil，和对话图片共用带鉴权的 OkHttp；文件下载后交给系统打开。**视频、语音不在 App 里播**：显示成卡片（封面、时长），点开交给飞书。在 App 里播飞书视频，对「看清一件事、交给 AI」没有帮助，属于「不做第二个飞书」的范围。

### 5.15 收件箱和飞书（P0 基础，卡片 P2）

- 收件箱列表、分段、筛选、已处理（`SwipeToDismissBox` 满滑）、「等你的 Agent」并入「需要你」。
- 飞书会话页用共用的列表引擎（[ux.md](ux.md) 3.5）：
  - 行内表情用 `InlineTextContent`，占位尺寸固定，表情图晚到也不会重排；
  - 富文本块树先做段落、列表、引用、代码；
  - 卡片先显示摘要加「在飞书中打开」，完整的卡片渲染放第二批。卡片的 `column_set` 用自定义 `Layout`，要判断 `Constraints.hasBoundedWidth`，对应 iOS 宽度可能是无穷的那个坑。
- 邮件详情：WebView 关掉 JS，按内容测高度。

### 5.16 体验模式（P1）

iOS 已经有「体验模式」：App 内一个假的 hub，数据来自 `scripts/mock/mock_hub.py`，由 `scripts/make_demo_data.py` 导出成 `Demo.json`，时间相对当前。Android 复用同一份 `Demo.json`，在 `:core:hub` 下面接一个 `DemoHub`，页面里没有任何体验模式分支。三个用处：

- 没有 hub 的人先看看；
- debug 截图不用起 mock hub；
- 截图测试用固定数据。

### 5.17 大屏和折叠屏（P2）

- 按宽度布局，不按设备类型判断：窄屏单栏；宽屏时 Agents、收件箱、设置用列表-详情双栏（Navigation 3 的 list-detail scene），和 iOS 的 iPhone Duo 双栏一致。
- 对话页正文最大宽度 760dp，和 iOS 一致。

### 5.18 国产系统适配（P0）

依据见 [research/android-cn-roms.md](research/android-cn-roms.md)（份额、版本、各家限制、常驻连接、Live Updates、分发、测试，带来源）。

- **覆盖范围**：
  - 澎湃 OS（小米 / 红米 / POCO）、ColorOS（OPPO / 一加 / 真我）、OriginOS（vivo / iQOO）、MagicOS（荣耀）、鸿蒙 4.x 及更早（华为，兼容层是 API 31）。
  - 这一年各家的当前大版本（澎湃 OS 4、ColorOS 17、OriginOS 7、MagicOS 11）都基于 Android 17，存量主力是 Android 16 那一代。
  - **鸿蒙 5 / 6 / 7 不能直接装 APK**，只能装进华为的「卓易通」容器。前台服务和通知在容器里能不能用没查到，首版不承诺；识别到这种环境时，配对页直接说明「需要在卓易通里允许通知、并保持在后台，体验可能不完整」。
- **识别系统**：读系统属性，不只看 `Build.MANUFACTURER`（小米手机可能刷了别的系统）：
  - 澎湃 OS `ro.mi.os.version.name`；ColorOS `ro.build.version.oplusrom`；OriginOS `ro.vivo.os.build.display.id`；MagicOS `msc.config.magic.version`；鸿蒙 2–4 `hw_sc.build.platform.version`；卓易通 `ro.product.anco.devicetype`。
  - 系统名和版本放进 `client_hello`（[hub-changes.md](hub-changes.md) 3.3），hub 汇总后就知道哪家哪版的跳转失效了。
- **没有一家提供「查自启动、查后台白名单」的公开接口**，所以准备清单分两类：
  - **能查的**：通知总开关、各渠道的级别、电池优化（`isIgnoringBatteryOptimizations`）、后台限制（`isBackgroundRestricted`）。显示真实状态，回到 App 时重查。
  - **查不了的**：悬浮 / 锁屏通知、自启动、各家的「无限制」、最近任务加锁、华为的静默通知和「休眠时始终保持网络连接」、荣耀的「智能省流量」。按识别出的系统给出那一家、那一版的步骤 +「去设置」，用户点「已完成」；再用 5.9 的自检兜底（锁屏测试通知、hub 记录的断开时段）。
- **跳转写成数据**：每家一组候选（组件名或 action），按顺序 `startActivity`，捕获 `ActivityNotFoundException` / `SecurityException`，都失败就退到应用详情页。
  - 跳到别家 App 的页面不需要声明包可见性；但 `resolveActivity()` 会被可见性过滤，查不到不代表没有，所以直接试。
  - 组件名都是社区逆向出来的，随版本失效（ColorOS 15+、OriginOS 6+、荣耀新机的组件名都没查到）。每次跳转的结果记进本地日志；云真机上每个大版本跑一遍。
- **逐家要点**（准备清单的内容）：

  | 系统 | 要用户打开的 | 最容易漏的 |
  | --- | --- | --- |
  | 澎湃 OS | 自启动（默认关）；电量消耗选「无限制」；最近任务里加锁 | 悬浮通知、锁屏通知默认关，而且查不到；关电池优化走的是小米自己的页面，返回后要等约 1 秒再查结果 |
  | ColorOS | 自启动；耗电管理里的「允许完全后台行为」；锁定 | 社区说改原生的「电池优化」会被 ColorOS 自己的白名单重置 |
  | OriginOS | 自启动；「允许后台高耗电」；锁定 | 第一次打开时的通知授权只弹一次，拒绝了就要去设置里开 |
  | MagicOS | 应用启动管理：关掉自动管理，打开自启动、关联启动、后台活动；电池优化选不允许；锁定 | **「智能省流量」会断掉后台的移动数据**：进程活着、通知不来，一打开 App 又好了，最容易被误判成 hub 的问题 |
  | 鸿蒙 4.x | 应用启动管理三个开关；电池优化选不允许；**「休眠时始终保持网络连接」**；锁定 | 本地通知默认静默、限频（5.8），要关「静默通知」、开横幅和锁屏 |

- **测试**：真机至少三台：小米（已有，澎湃 OS 3 / Android 16）、华为鸿蒙 4.x（minSdk 的下限，通知最麻烦）、OPPO ColorOS 16 或 17（对标准接口最友好）。vivo、荣耀先用厂商的云真机测跳转和弹窗，有问题再买。云真机单次最长 1 小时、没插 SIM 卡、通常插着电，测不了过夜存活、耗电和流量下的后台限制。每台的必测项见第 8 节。
- **经验落盘**：每家每个版本的实际表现，写进 `docs/android.md` 的「经验」。

## 6. 平台差异与取舍

### 6.1 换形态

| iOS | Android |
| --- | --- |
| APNs 推送 | 常驻连接（5.8） |
| 锁屏实时活动 + 灵动岛 | 常驻通知 / Live Updates，Android 17 用 MetricStyle（5.10）；数字类指标（TPM、token、负载）放 App 里，不进通知 |
| 静默推送唤醒执行手机工具 | 常驻连接一直在线，不需要唤醒 |
| Siri / 快捷指令 | 快捷设置磁贴（「需要你」的数量、批准第一个）、桌面快捷方式（问 Claude）、Glance 小组件、分享目标。Google Assistant 已经关闭，App Actions 不做；AppFunctions 还是 beta，留作远期 |
| Apple Watch | 首版不做 Wear OS 应用：手机通知（含按钮和 `RemoteInput`）会自动桥接到 Wear OS 手表 |
| HealthKit | Health Connect；国行手机首版不注册健康工具（5.12） |
| 液态玻璃 | 原生 Material You |
| 对话页左划切到文件 / Git | 顶部分段切换：屏幕边缘被系统的返回手势占用 |
| 「选择」弹纯文本页 | 原地选择 |
| Gmail 的 Google 账号登录 | **Android 上不做**：Google 的登录接口依赖 Google Play 服务。邮箱账号存在 hub 上、两端共用：在 iPhone 上加一次，或者用应用专用密码走普通 IMAP。以后 hub 有了域名和公共 CA 的证书，可以改成「网页授权、回调到 hub」，手机只负责用浏览器打开授权页（P2） |

### 6.2 不做

- 提醒事项三个工具（Android 没有系统接口）。
- Wear OS 应用（首版）。
- 液态玻璃风格。
- 语音助手入口。

### 6.3 Android 独有

| 能力 | 建议 |
| --- | --- |
| **分享到 Persona** | **首版就做（P0）**：任何 App 的文字、链接、图片、文件，「分享 → Persona」后选发进主对话还是某个 Agent，作为 `<jarvis-context>` 或附件带上。实现是 `ACTION_SEND` 的 intent-filter，成本低，正合「把各处信息收到一起交给 AI」 |
| 常驻连接 | 已经是默认（5.8） |
| 快捷设置磁贴、小组件 | 第二批 |
| 大屏 / 折叠屏双栏 | P2（5.17） |
| 读其他 App 的通知、短信、通话记录 | **默认不做**：太敏感。以后要做，也是单独开关、单独确认，数据只给 AI 用 |

## 7. 首版范围与切片

首版是 [parity.md](parity.md) 第 1–24 节的 76 个 P0，加上第 25 节标了 Android P0 的新增项。从 PR #84 出发分三个切片。

| 切片 | 内容 | 完成的标准 |
| --- | --- | --- |
| **A0 工程升级**（不改行为） | 升级工具链（target 先 36）；加 `build-logic`、版本目录、daemon JVM 规范；拆出三个纯 Kotlin 模块（contracts / domain / terminal 先空着），把 PR 的 `Models`、`ChatStore` 连续性、`Pairing.parse`、`Rows` 搬进去，接上 `spec/fixtures` 跑夹具；`scripts/android.sh` 加 `lint` 和 `shots` | PR #84 的 10 个测试 + 夹具测试全绿；在真机上配对、发消息、通知按钮作答都和升级前一样 |
| **A1 最小可用切片** | Hilt、Navigation 3、`HubScope`、加密存储、Room 的 items / outbox；**公网直连**：多地址探测、TLS 钉扎、`client_hello` 和心跳协商、连接诊断页（5.1、5.9）；对话页换成共用的列表引擎 + `chatLines` + `fold_work` + 状态行 + 新交互面板 + mikepenz Markdown；通知改成「允许 / 批准」要解锁 + 拒绝原因；生成的 token（状态色和 iOS 一致）；debug 直达入口 + mock hub 截图 | 650 条命令的长会话打开流畅（实测打开到首屏的耗时和滚动掉帧，至少 10 个样本）；手机在蜂窝网络上经公网 IPv6 连上 hub；三家运营商的空闲超时、常驻连接每小时耗电和锁屏存活有实测数字；真机上走通「通知 → 在通知或 App 里批准 → 看到 Agent 继续」 |
| **A2 首版补齐** | 四个 tab：Persona（首页、主对话、历史）、Agents（三种分组、筛选、置顶 / 屏蔽、长按菜单、关闭、新建会话）、收件箱（飞书基础渲染 + 回复、邮件详情）、设置；连接状态（四态 + 本地网络权限 + 证书变了 + 配对失效，这一步升 target 37）；配对的分段进度和准备清单（含国产系统，5.18）；离线 outbox；分享到 Persona；APK 的 App 内更新（第 9 节）；hub 侧「做完了」改为经 WS 下发 | parity.md 里所有 Android P0 至少到「测试 hub 通过」；可以发给自己和测试的人日常用 |
| A3 第二批 | 终端、Live Updates、手机工具、知识库、文件 / Git、链接浮层、飞书文档和卡片、机器监控、其余风格、体验模式 | 按 parity.md 逐项 |

PR #84 对首版 P0 的覆盖（重组时用来判断哪些不用从零写）：

| 已有 | 部分有 | 没有 |
| --- | --- | --- |
| 配对（扫码、深链、手动）、换设备 token、明文 HTTP、历史分页、补洞、拒发处理、交互卡片四种 + 屏幕卡、「看过」的门禁、通知按钮作答、点通知打开会话、断开配对、动态取色 | WS（没有连接状态四态和弱网判断）、Agents 列表（只有按状态这一种分组）、对话页（倒置列表有，定位、冻结、回到底部都没有）、工具行（没有 diff 配色）、图片（懒加载有，缓存和占位没有）、输入框（只能发文字）、草稿（只在页面内保留，不持久）、「做完了」通知（手机自己判断） | 四个 tab、Persona tab、收件箱、飞书、邮件、置顶 / 屏蔽、新建会话、工作行折叠、outbox、本地缓存、链接在 App 内打开、token 和组件库、debug 直达入口 |

## 8. 测试与工具

- **纯 Kotlin 模块**：JUnit + coroutines-test + Turbine，跑 `spec/fixtures`（连续性、行折叠、排序分组、作答、终端帧、协议样本）。CI 和本机都能在几秒内跑完。
- **页面模型**：fake 实现 + `CompletableDeferred` 闸门，确定性地制造迟到、取消、切作用域。不用 mock 库。
- **截图**：Roborazzi 跑组件目录和关键页面。矩阵是深浅色 × 字号（默认 / 大 / 辅助）× 窄屏，数据用编造的。参考图在固定环境录（Mac 和 Linux 的字体渲染不同）。最终验收仍在模拟器上实拍，因为 Robolectric 里的模糊和 WebView 不可信。
- **流程 UI**：Compose UI 测试；仪器测试用 `adb reverse tcp:18660 tcp:18660` 连 `scripts/mock/mock_hub.py`（PR #84 的经验）。
- **debug 直达**：入口只放在 `src/debug/`。
  - 命令：`adb shell am start -n <包名>/.debug.DebugEntryActivity --es openSession m/s`；
  - 配对：`--es pairURL "jarvis://pair?url=…&token=mock"`；
  - 深色：`adb shell cmd uimode night yes`；
  - 固定状态栏：用 SystemUI 的 demo 模式。
  - 开关名用 `debug.open…` 这类不会和标识符撞名的前缀：Kotlin 的 enum 常量名会以字符串留在 dex 里。
- **Release 检查**：解开 APK，用 `apkanalyzer dex` / `aapt2 dump strings` 扫，出现任何调试开关名就失败，对应 iOS 的 `strings` 检查。
- **性能**：Macrobenchmark 覆盖三项（冷启动到画出缓存的列表、打开大会话到首屏、长对话快速滑动的掉帧），用 Baseline Profile 守住；常驻连接模式额外测**每小时耗电**和**锁屏下连接能撑多久**。引用数字必须实测，至少 10 个样本，给 min / p50 / p90 / max / mean。
- **国产系统真机**（5.18）：每台必跑：
  1. 首次安装的拦截提示，首次打开的通知授权，准备清单每一项的跳转到没到对的页面；
  2. 锁屏 30 秒后的测试通知：响不响、亮不亮屏、锁屏上显不显示；
  3. 锁屏不插电放 8 小时：hub 记断开的时段，手机记 `ApplicationExitInfo`，**至少 10 晚**；
  4. 每小时耗电（系统电池页 + `dumpsys batterystats`）；
  5. 划掉卡片、在「活跃应用」里停止、重启手机之后，服务能不能回来；
  6. 自更新一次；
  7. 有活动时，状态栏、锁屏、岛各截一张（深浅色）；
  8. 选图、定位、WebView 版本、扫码。
  - `adb shell dumpsys deviceidle force-idle` 只能模拟原生的 Doze，测不出厂商策略。
- **多 agent 并行**：每个车道一个 worktree，用自己的 AVD 和端口（`emulator -avd <名字> -port <偶数>`，`adb -s emulator-<端口>` 区分）；Gradle 开配置缓存和构建缓存。

## 9. 分发、签名与配置

没有 Play 商店，也不上国内的应用商店（2023 年起 App 要先备案才能上架分发，自用工具走侧载更省事）。依据见 [research/android-cn-roms.md](research/android-cn-roms.md) 第 7 节。

- **开发期**：`scripts/android.sh install`，用 adb 直接装（USB 或无线调试）。PR #84 的小米经验：要先在开发者选项里打开「USB 安装」，还要打开「USB 调试（安全设置）」；手机上的确认框几秒不点就自动拒绝。
- **给自己和测试的人**：签名的 APK 放在 hub 上（[hub-changes.md](hub-changes.md) 2.2）。
- **首次安装**：手机上还没有 App，没有设备 token。
  - `personad hub pair --android` 在二维码旁边再打印一个内网下载链接（带同一个临时 token），用手机浏览器打开下载；也可以用 adb 或别的方式把 APK 传过去。
  - 各家都会拦一下：所有 Android 8+ 都要给浏览器或文件管理器授权「安装未知应用」；澎湃 OS 的「安全守护」要验证身份才能关；鸿蒙 4 开着「纯净模式」的增强防护时只能装华为应用市场检测过的 App；鸿蒙 5+ 只能「用卓易通打开」。
  - 下载页写一份按品牌的安装说明，讲清为什么会提示风险（没上架应用商店）。首次安装只有一次，可以接受。
- **App 内更新**（parity.md N71）：
  - App 在前台时问 hub 有没有新版本（`/api/v1/app/android/latest`），设置里显示「有新版本」和更新说明；
  - 经 TLS 下载（钉同一个公钥），先校验 SHA-256，系统还会校验签名必须和已装的一致；
  - 要用户给 Persona 打开「安装未知应用」（`canRequestPackageInstalls()` 可查，没给就跳 `ACTION_MANAGE_UNKNOWN_APP_SOURCES`）；
  - 用 `PackageInstaller` 会话安装，**默认弹系统确认**（`USER_ACTION_REQUIRED`）：澎湃 OS 上请求静默更新会直接以 `INSTALL_FAILED_ABORTED` 失败、不弹任何框，要把它当成单独的错误，不能当成用户取消。静默更新只在原生系统上试；要处理 `STATUS_PENDING_USER_ACTION`（拉起系统确认页）。
  - `setRequestUpdateOwnership` 只在首次安装时生效，是给应用商店用的，对自更新没用。
  - 更新会杀掉进程：常驻服务靠 `MY_PACKAGE_REPLACED` 重新拉起（它在后台启动前台服务的豁免里；国产系统上收不收得到可能受自启动开关影响，要实测）。
- **签名**：release keystore 放仓库外，路径和别名写进 `config.json` 的 `android.signing`，口令从钥匙串或环境变量读。**密钥丢了就没法给已装的 App 升级**，要备份。
- **版本号**：`versionCode` 上限 2100000000，iOS 的 12 位构建号放不下，用 `yyyymmddHH`（10 位）；`versionName` 跟 iOS 的格式。
- **Google 的开发者验证**（2027 年起全球）：只在 Google 认证设备上执行，检查逻辑经 Google Play 服务下发，adb 安装不受影响。国行手机没有 Google 服务、也不是认证设备，基本不受影响（官方没有直接写这种情况，属于推断）。只有给装了 Google 服务的海外版手机装时，才需要用包名和签名密钥注册一个免费的有限分发账号（最多 20 台设备）。
- **崩溃日志**：不接 Firebase。`ApplicationExitInfo` 和未捕获的异常存在本地，下次启动时上传到 hub（[hub-changes.md](hub-changes.md) 4.15，P2）。
- **隐私**：`scripts/privacy_check.py` 覆盖 `android/` 和 `spec/`。keystore、`local.properties` 都不进仓库，`android/.gitignore` 已经忽略了一部分。

## 10. 风险与待验证

| 风险 | 怎么验证 / 应对 |
| --- | --- |
| 常驻连接的耗电和存活 | A1 在主流国产系统的真机上实测：每小时耗电、锁屏 8 小时后连接还在不在、被系统杀掉后多久拉起；按结果调后台心跳间隔和准备清单 |
| 本地网络权限（target 37） | 升 target 前完成 5.1；在家里连同一个 /64 的公网 IPv6、经 VPN 的行为都要真机实测 |
| 公网直连在用户家里走不通（光猫 IPv6 防火墙、封端口、hub 机器的 TUN 代理） | `personad hub doctor` 自检；App 的诊断页说清原因；推荐 DDNS（[README.md](README.md) D9） |
| 运营商 NAT 和防火墙的空闲超时（4G / 5G 没有公开数据） | A1 实测三家运营商；后台心跳按网络自动调整（5.9） |
| 长会话性能 | A1 用 650 条命令的样本会话实测打开耗时和滚动掉帧 |
| 两端漂移 | 夹具是硬约束；改规则先改夹具 |
| 国产系统限制（通知默认不显示、自启动默认关、进程被杀后 `START_STICKY` 起不来前台服务） | 准备清单逐项检查和引导（5.18）；`startForeground` 失败时等下一个豁免，关电池优化本身就是豁免（5.9）；把各家的表现写进 `docs/android.md` 的「经验」 |
| 华为鸿蒙 4 的本地通知默认静默、每天限 2–5 条 | 引导用户关「静默通知」、开横幅和锁屏；「需要你」同时显示在常驻通知的计数上；真机测常驻通知算不算进配额 |
| 进程活着但收不到（荣耀「智能省流量」、华为「休眠时始终保持网络连接」、厂商冻结） | 准备清单点名这几项；hub 记录断开时段，诊断页显示；锁屏 8 小时的真机实测（至少 10 晚） |
| 跳转设置页的组件名随版本失效 | 跳转写成数据、逐个试、失败退到应用详情页；结果进日志，`client_hello` 带系统名和版本；云真机每个大版本跑一遍 |
| 自更新在各家系统上的表现（澎湃 OS 静默更新已确认会失败） | 默认弹系统确认；每台真机自更新一次 |
| 鸿蒙 5 及以后不能装 APK | 首版不承诺；识别到卓易通环境时在配对页说明 |
| AGP 9.5 需要新版 Android Studio | 命令行构建不受影响；用 IDE 的人升级到 Rabbit 2 |
| Room、Navigation 3、Compose 1.12 这些新 API，AI 写出旧写法 | 在 `CLAUDE.md` / `docs/android.md` 写明版本和惯用法；版本目录锁版本 |
| WebView 在自绘浮层里的手势 | 链接浮层先做原型 |
