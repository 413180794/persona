# Persona Android：国产系统（ROM）与 Android 版本适配调研

- 调研日期：**2026-10-30**（下文「查询日期」未单独写的，都是这一天）。
- 标记：✅ 事实（有来源 URL）；💭 判断 / 推断（没有直接来源，引用前要复核）；❓ 未查到。
- 前提（用户 2026-10-30 的硬约束）：
  1. 按「手机没有 Google 服务（GMS / Play 服务）」设计，适配主流系统版本；
  2. 手机经公网直连 hub（优先 IPv6），VPN 用户自己配，App 不管（另一份调研负责，本文只在涉及时带过）。
- 性能 / 耗电 / 份额数字只引用有来源的，注明样本和口径；没有就写没有。

---

## 0. 结论速览

1. **minSdk 31**（💭）：中国 StatCounter（页面浏览量）累计覆盖约 91%，Google 全球约 78%；鸿蒙 3/4 的兼容层就是 API 31，31 正好把仍能装 APK 的华为机包进来，33 会把它们和所有 Android 12 手机切掉（中国覆盖降到 74%）。31 起才有锁屏通知按钮「先解锁」、后台起前台服务只有一套规则。圈子里有 Android 10/11 的机器再退到 29。（第 1 节）
2. **主流系统都已是 Android 17 一代**（澎湃 OS 4、ColorOS 17、OriginOS 7、MagicOS 11，2026-08~10 发布），存量主力是 Android 16 一代；华为分两半：鸿蒙 4.x（API 31，能装 APK）和鸿蒙 5/6/7（只能经卓易通容器，首版不承诺）。存量份额：华为 21.6%、苹果 20.9%、OPPO 约 18%、vivo 16.5%、小米 10.1%、荣耀约 7.8%（QuestMobile 2026-06）。（第 2 节）
3. **没有一家提供「查 / 开自启动、省电白名单」的公开接口**，组件名全靠社区，随版本失效。准备清单要「能查的查、查不了的给按系统的路径 + 自检」。几个最容易漏的：小米的悬浮 / 锁屏通知默认关、自启动默认关；华为鸿蒙 4 **本地通知默认按「资讯营销」静默且限频**、「休眠时始终保持网络连接」；荣耀「智能省流量」会断后台流量；vivo 首启通知弹窗只弹一次。（第 3 节）
4. **常驻连接**：`specialUse` 无时长上限；AOSP 里有前台服务的进程在 Doze 下仍能联网、持锁；Android 15/16/17 没有针对它的新限制。风险在厂商：杀进程后 `START_STICKY` 在后台不是豁免（起前台服务会抛异常），关电池优化正好是豁免；冻结 / 断网开关会造成「进程活着但收不到」。没有任何国产系统的公开耗电 / 存活实测，只能自己测。后台心跳建议改成 hub 发、间隔几分钟。（第 4 节）
5. **Live Updates**：ColorOS 16+ 官方表态兼容；澎湃 OS 3.1+ 社区实测支持；OriginOS 国行、MagicOS 未确认；厂商私有岛接口都要白名单，不碰。PR #84 的常驻渠道是 `IMPORTANCE_MIN`，按规则不能提升，要换新渠道。MetricStyle 在国产系统上的表现完全没资料。（第 5 节）
6. **无 GMS 要去掉 / 换掉的**：Health Connect（国行基本没有数据源，建议国行去掉）、FCM（改可选）、Google code scanner、可下载字体、Firebase、Fused Location（换 `LocationManager`）、Google Authorization API（换成回调到 hub 的 Web OAuth）；`Geocoder`、WebView 新能力、照片选择器要降级处理。（第 6 节）
7. **分发**：侧载 + hub 托管 APK 自更新。**澎湃 OS 上静默自更新（`USER_ACTION_NOT_REQUIRED`）会直接 `INSTALL_FAILED_ABORTED`**，要改成弹系统确认；`setRequestUpdateOwnership` 对自更新没用。2027 开发者验证只管 Google 认证设备，国行机基本不受影响。（第 7 节）
8. **测试**：真机 小米（已有）+ 华为鸿蒙 4.x + OPPO 必备，vivo、荣耀用云真机先过跳转；云真机只适合测安装、弹窗、跳转、界面，测不了过夜存活和耗电。（第 8 节）

## 1. Android 版本分布与 minSdk

### 1.1 数据（✅ 事实，2026-10-30 查询）

三份数据口径不同，**没有一份是「中国大陆手机的设备数」**：

| 来源 | 口径 | 时间 |
| --- | --- | --- |
| Google（Android Studio 分布面板，经 composables.com / IT之家 转述） | 7 天内连过 **Play 商店**的活跃设备；Google 只给累计比例，单版本是反推的近似值 | 截至 2026-10（Android Developer Summit 10-28/29 公布；composables 页面标 2026-10-01 更新） |
| StatCounter 全球 | 装了 StatCounter 的网站的**页面浏览量**，不加权 | 2026-09 单月 |
| StatCounter 中国 | 同上，地区 = 中国 | 2026-09 单月 |

| Android（API） | Google 全球 单版本 | Google 全球 累计 | StatCounter 全球 单版本 | StatCounter 全球 累计 | StatCounter 中国 单版本 | StatCounter 中国 累计（💭 我按 CSV 加的） |
| --- | --- | --- | --- | --- | --- | --- |
| 17（37） | 0.9–1.0% | 0.9% | 1.75% | 1.8% | 0.44% | 0.4% |
| 16（36） | 25.4% | 26.3% | 37.65% | 39.4% | **41.65%** | 42.1% |
| 15（35） | 14.7% | 41.0% | 15.93% | 55.3% | 13.10% | 55.2% |
| 14（34） | 15.3% | 56.3% | 12.05% | 67.4% | 10.62% | 65.8% |
| 13（33） | 11.8% | 68.1% | 11.58% | 79.0% | 8.01% | 73.8% |
| 12 / 12L（31/32） | 9.6% | 77.7% | 7.62%（+12.2 的 0.11%） | 86.6% | **17.45%** | 91.3% |
| 11（30） | 10.5% | 88.2% | 6.34% | 92.9% | 3.60% | 94.9% |
| 10（29） | 5.7% | 93.9% | 3.28% | 96.2% | 1.37% | 96.2% |
| 9（28） | 3.1% | 97.0% | 1.74% | 97.9% | 1.46% | 97.7% |
| 8.1 / 8.0（27/26） | 1.5% / 0.5% | 98.5% / 99.0% | 0.85% / 0.28% | 98.8% / 99.1% | 0.70% / 0.21% | 98.4% / 98.6% |

- Google 数据：composables.com 页面（「All statistic data comes from Google's servers」，2026-10-01 更新）与 IT之家 2026-10-30 文章（「7 天内连接过 Play 商店的活跃设备」「谷歌官方只公布累计比例，各版本占比为据此推算的近似值」）。
  来源：https://composables.com/android-distribution-chart ，https://www.ithome.com/1/019/973.htm
- StatCounter 全球 / 中国：官方 CSV 导出（2026-09）。apilevels.com 用同一份全球数据算的累计值（数据月 2026-09，页面 2026-10-29 刷新）和上表一致。
  来源：https://gs.statcounter.com/android-version-market-share/mobile-tablet/worldwide ，https://gs.statcounter.com/android-version-market-share/mobile-tablet/china ，https://apilevels.com/
- 国内统计平台（友盟+、极光、个推、TalkingData）：❓ **没查到 2026 年公开的 Android 版本分布**，能搜到的都是 2014–2018 年的「友盟指数」旧数据。
- **中国的「Android 12」17.45% 明显偏高（全球只有 7.6%）**。✅ 鸿蒙 3.x / 4.x 的 Android 兼容层是 Android 12（API 31）：
  UA 里写 `Android 12; HarmonyOS/4.0.3…`，WebView UA 带 `APILevel/31 HarmonyOS/4.2.0`；开发者实测 `Build.VERSION.SDK_INT` = 31。
  💭 所以中国这 17% 里很大一部分是华为的鸿蒙 3/4 手机。鸿蒙 5/6/7（不能装 APK）在 StatCounter 里不出现。
  来源：https://whatmyuseragent.com/platforms/har/harmonyos/4 ，https://cloud.tencent.com/developer/article/2596538 ，https://github.com/NEORUAA/MeiloX/issues/19 ，https://www.huaweicentral.com/harmonyos-3-0-is-compatible-with-android-12/ ，https://appy.to/blog/iphone-android-huawei-share-by-country
- 鸿蒙 2 的兼容层是 Android 10（API 29）：只有第三方报道（模拟器里显示 Android 10），❓ 没查到官方说法。来源：https://en.linuxadictos.com/in-the-end-harmonyos-turned-out-to-be-a-reworked-android-10.html

### 1.2 各候选 minSdk 失去的能力和要多写的分支

先列和 Persona 有关的 API 级别（✅ 官方文档，见本节末尾来源）：

| API | Persona 用得到的能力 |
| --- | --- |
| 26 | 通知渠道；`startForegroundService` |
| 28 | `FOREGROUND_SERVICE` 权限；`ActivityManager.isBackgroundRestricted()`（能查出用户在系统里限制了后台） |
| 29 | `android:foregroundServiceType` 属性；后台读剪贴板被禁（只有前台 App / 默认输入法能读）；`ACCESS_BACKGROUND_LOCATION` |
| 30 | `WindowInsetsAnimation`（键盘弹起跟手动画）；`LocationManager.getCurrentLocation`；`ApplicationExitInfo`（**能查到进程是怎么被杀的**，诊断 ROM 杀后台有用）；系统照片选择器要靠 Google 系统更新推的模块（没有 GMS 收不到） |
| 31 | **通知按钮 `setAuthenticationRequired`**（锁屏上点「允许 / 批准」先解锁）；动态取色；`RenderEffect` 模糊；从后台启动前台服务受限（target 31 起）；前台服务通知默认最多延迟 10 秒显示，可用 `setForegroundServiceBehavior(IMMEDIATE)`；精确闹钟要 `SCHEDULE_EXACT_ALARM`；`PendingIntent` 必须声明可变性 |
| 33 | `POST_NOTIFICATIONS` 运行时权限；系统照片选择器内置（`MediaStore.ACTION_PICK_IMAGES`）；`Geocoder` 异步接口；按 App 设置语言；AGSL |
| 34 | 前台服务必须声明类型（`specialUse` 从这里开始有）；用户能划掉前台服务的通知；只授权部分照片；`PackageInstaller.setRequestUpdateOwnership`；Health Connect 进系统框架；精确闹钟默认不授予 |
| 35 | target 35：`BOOT_COMPLETED` 不能启动 dataSync / camera / mediaPlayback / phoneCall / mediaProjection / microphone 类前台服务（**不含 `specialUse`**）；dataSync 每 24 小时 6 小时上限；强制 edge-to-edge |
| 36 | Live Updates（`setRequestPromotedOngoing`、ProgressStyle）；JobScheduler 配额对「前台服务运行中」的 App 也生效 |
| 37 | `MetricStyle`；target 37 强制本地网络权限 `ACCESS_LOCAL_NETWORK`（公网直连 hub 不受影响，另一份调研） |

各候选的取舍（覆盖率取「StatCounter 中国累计 / Google 全球累计」；💭 判断部分已标）：

| minSdk | 覆盖 | 和 31 比，多出来的兼容分支 / 失去的能力 |
| --- | --- | --- |
| 26（PR #84 现状） | 98.6% / 99.0% | 下面 28、29、30、31 的全部分支：锁屏通知按钮不能要求解锁（只能不放「允许 / 批准」，或点了先进 App）；没有动态取色、没有模糊；29 以下后台能读剪贴板（行为两套）；30 以下没有 `ApplicationExitInfo`，被杀原因查不到；从后台起前台服务在 31 以下不受限，31 起受限——两套启动逻辑都要测 |
| 28 | 97.7% / 97.0% | 同上，只省掉 Android 8.x 的几处（`FOREGROUND_SERVICE` 权限、`isBackgroundRestricted`）。💭 对 Persona 收益很小 |
| 29 | 96.2% / 93.9% | 省掉剪贴板和前台服务类型属性的分支；还差 30、31 的能力。💭 会把鸿蒙 2 的老华为机算进来（兼容层 Android 10，来源弱） |
| 30 | 94.9% / 88.2% | 有了键盘动画、`getCurrentLocation`、`ApplicationExitInfo`；还差 31 的通知按钮需解锁、动态取色、模糊 |
| **31** | **91.3% / 77.7%** | 基线。**包含鸿蒙 3/4（API 31）**；锁屏「允许 / 批准」能要求解锁；后台启动前台服务只有一套规则。仍要写的分支：33（通知权限、照片选择器、Geocoder）、34（前台服务类型常量、FGS 通知可被划掉、Health Connect 是否内置）、35（BOOT_COMPLETED 规则）、36（Live Updates）、37（MetricStyle、本地网络权限） |
| 33（android-tech.md 原建议） | 73.8% / 68.1% | 少写 31–32 的分支，但**会把鸿蒙 3/4 的华为手机（API 31）和所有 Android 12 手机挡在外面**。💭 在中国不合适 |

**建议（💭 判断）**：

- **minSdk 31**。理由：
  1. 中国页面浏览量口径覆盖约 91%，而且包含仍能装 APK 的鸿蒙 3/4 手机（华为机里的多数，见 2.1）；
  2. 锁屏通知上的「允许 / 批准」能要求先解锁，这是常驻通知直接审批的安全前提；
  3. 后台启动前台服务只剩一套规则（31 起），常驻连接最容易出问题的那块不用写两套。
  - 31 以下的机器（中国约 9%）多是 2020 年前后的机型。如果用户圈子里确实有 Android 10/11 的手机，退到 **29**：代价是锁屏上不放「允许 / 批准」（或点了先打开 App），没有动态取色和模糊，多测一套前台服务启动规则。
- 不建议 33：中国覆盖掉到 74%，而且正好切掉鸿蒙 4。
- 「主流」的口径（💭）：覆盖中国页面浏览量的 90% 以上，并且包含每家国产系统最近三个大版本能升级到的机型。各家当前大版本基于 Android 17、上一代基于 Android 16（第 2 节），再上一代基于 Android 15（这一代本次没单独查来源），都在 31 之上；鸿蒙 4.x 正好是 31。

来源（1.2 的 API 级别）：
- 通知按钮需解锁（API 31）：https://developer.android.com/reference/android/app/Notification.Action.Builder
- 动态取色（Android 12 起）：https://developer.android.com/develop/ui/views/theming/dynamic-colors ；`RenderEffect`（API 31）：https://developer.android.com/reference/android/graphics/RenderEffect
- 前台服务类型（34 起必须声明、`specialUse` 无运行时前提、无时间上限）：https://developer.android.com/develop/background-work/services/fgs/service-types
- 后台启动前台服务的限制和 14 条豁免（31 起）：https://developer.android.com/develop/background-work/services/fgs/restrictions-bg-start
- 前台服务通知最多延迟 10 秒（12 起）：https://developer.android.com/guide/components/foreground-services#notification-immediate ；`ApplicationExitInfo`（30 起）、任务管理器停止：https://developer.android.com/develop/background-work/services/fgs/handle-user-stopping
- 包可见性（target 30 起过滤 `resolveActivity`）：https://developer.android.com/training/package-visibility/automatic
- Android 15 的 `BOOT_COMPLETED` 限制、dataSync 6 小时：https://developer.android.com/about/versions/15/behavior-changes-15
- Android 16 JobScheduler 配额：https://developer.android.com/about/versions/16/behavior-changes-all
- Android 17：https://developer.android.com/about/versions/17/behavior-changes-17 ，https://developer.android.com/about/versions/17/behavior-changes-all
- 照片选择器可用性：https://developer.android.com/training/data-storage/shared/photopicker ；AndroidX 的判断逻辑（33+ 一律走系统选择器，30–32 看 SDK 扩展版本 ≥ 2，否则找系统应用提供的 `androidx.activity.result.contract.action.PICK_IMAGES`，再不行退回 `ACTION_OPEN_DOCUMENT`）：https://raw.githubusercontent.com/androidx/androidx/androidx-main/activity/activity/src/main/java/androidx/activity/result/contract/ActivityResultContracts.kt

## 2. 主流国产系统、份额、基于的 Android 版本

### 2.1 份额（✅ 事实，2026-10-30 查询）

**卖出去的（季度出货 / 销量）**

| 品牌（系统） | Counterpoint 2026Q3 出货份额 | Omdia 2026Q3 出货量 | IDC 2026Q3 |
| --- | --- | --- | --- |
| 华为（鸿蒙） | 21.3% | 1240 万台（约 21%） | 21.1%，第一 |
| Apple | 19.5% | 1160 万台 | 17.8%，第二 |
| vivo / iQOO（OriginOS） | 15.2% | 900 万台 | 第三到五名（OPPO、vivo、小米），具体份额没查到 |
| OPPO / 一加 / 真我（ColorOS） | 15.1%（含一加、真我） | 900 万台 | 同上 |
| 小米 / 红米 / POCO（澎湃 OS） | 13.5% | 800 万台 | 同上 |
| 荣耀（MagicOS） | 12.7% | 没查到 | 唯一环比增长的头部厂商 |
| 合计 | — | 5930 万台（同比 -12%） | 约 5806 万台（同比 -15.2%） |

- 系统层面（Counterpoint 2026Q2 中国销量）：Android 58%、**鸿蒙 24%**（创新高，Q1 是 19%）、iOS 18%。
- 来源：Counterpoint（经 TelecomLead 2026-10-25 转述）https://telecomlead.com/smart-phone/china-smartphone-market-q3-2026-huawei-and-apple-gain-share-as-xiaomi-vivo-oppo-and-honor-decline-128351 ；
  Omdia 2026-10-14 https://www.lightreading.com/smartphones-devices/mainland-china-smartphone-shipments-drop-to-lowest-q3-level-since-2013-omdia ；
  IDC（经新浪 2026-10-14 / 10-27 转述）https://finance.sina.com.cn/tech/digi/2026-10-14/doc-inivcxxt8542914.shtml ，https://finance.sina.com.cn/tech/mobile/n/n/2026-10-27/doc-iniwsfqw3802453.shtml ；
  Counterpoint 系统份额（经多家转述）https://gadgets.beebom.com/news/android-ios-harmonyos-how-mobile-os-share-ranks-in-2026 ，https://www.androidheadlines.com/2026/08/android-vs-ios-vs-harmonyos-heres-how-each-os-ranks-globally-and-in-3-major-markets.html

**在用的（存量，更接近「用户手里是什么」）**：QuestMobile《2026 年智能手机市场研究报告》，数据截至 2026-06，移动互联网活跃设备共 12.82 亿：

| 品牌 | 活跃设备 | 份额 |
| --- | --- | --- |
| 华为 | 2.76 亿 | 21.6%（同比 -4.0%） |
| Apple | 2.68 亿 | 20.9% |
| OPPO（2025 版口径：不含真我、一加） | 2.33 亿 | 约 18.2%（💭 我按 12.82 亿算的） |
| vivo（含 iQOO） | 2.12 亿 | 16.5%（同比 +9.5%） |
| 小米（含红米） | 1.29 亿 | 10.1% |
| 荣耀 | 1.00 亿 | 约 7.8%（💭 我算的） |

来源：https://www.tmtpost.com/8132127.html ，https://www.thepaper.cn/newsDetail_forward_34028314

**华为里能装 APK 的有多少**：

- ✅ 鸿蒙 5/6/7（HarmonyOS NEXT 一系，**不能直接装 APK**）：华为 2026-10-01 宣布「HarmonyOS 6 | HarmonyOS 7 终端设备数突破 9000 万」（口径是「终端设备」，不只手机；HarmonyOS 7 发布后改成两版合计）；IDC 2026Q3 报告说「原生鸿蒙装机量已突破 9500 万台」。
  来源：https://finance.sina.com.cn/tech/roll/2026-08-21/doc-ininysma7899246.shtml （8000 万，8-20），https://www.ithome.com/0/971/420.htm （7000 万，7-02），时间线汇总见 https://www.ithome.com/1/009/012.htm ；IDC 见上。
- 💭 粗算：华为活跃设备 2.76 亿（6 月），原生鸿蒙 ≤ 0.95 亿且含平板 / PC 等，所以**华为手机里至少约三分之二仍在鸿蒙 4.x 及更早**（能装 APK，兼容层 API 31）。这个比例在快速下降（每月新增约 500 万台原生鸿蒙设备），一两年内会反过来。
- ✅ 鸿蒙 5/6 上可以用华为的兼容容器「卓易通」（基于 iSulad 容器）运行 APK：从网上下载 APK 后「用卓易通打开」安装；已经有原生鸿蒙版的应用会被引导去装原生版；部分设备默认开着「纯净模式」要先关。
  卓易通里的应用收通知要在「卓易通 › 我的 › 应用管理 › 该应用 › 通知管理」里打开，并且**应用要在后台运行着**（卡片从多任务里没了就收不到）；卓易通本身可能在资源紧张时被杀，建议加入后台白名单。
  ❓ 前台服务、常驻连接在卓易通里是否有效，**没查到**，必须真机测。
  来源：https://blog.soarli.top/archives/996.html （2026-04-27），https://eu.36kr.com/zh/p/3522460176735107 ，卓易通官网 FAQ（页面靠 JS 渲染，WebFetch 读不到正文，内容来自搜索摘要）https://www.droitong.com/CommonQues.html ，https://blog.csdn.net/weixin_30899023/article/details/166670175

### 2.2 当前版本和 Android 基线（✅ 事实）

| 厂商 | 系统 | 当前大版本（2026-10） | 基于 | 上一代（存量多数） |
| --- | --- | --- | --- | --- |
| 小米 / 红米 / POCO | 澎湃 OS（HyperOS） | **澎湃 OS 4**：2026-08-13 发布，10-15 起首批正式版（小米 17、K90、Pad 8），小米 14 / K70 等年底前 | Android 17 | 澎湃 OS 3 / 3.1：Android 16（2025-10 起推送，持续到 2026-03） |
| OPPO / 一加 / 真我 | ColorOS（海外一加是 OxygenOS） | **ColorOS 17**：2026-09-17 发布，国内 10-08 起推送，老机型排到 2027Q1 | Android 17 | ColorOS 16：Android 16（2025-10 起） |
| vivo / iQOO | OriginOS（2025 年起海外也换成 OriginOS，iQOO 不再用 Funtouch） | **OriginOS 7**：2026-09-16 发布，国内 10 月起升级 | Android 17 | OriginOS 6：Android 16 |
| 荣耀 | MagicOS | **MagicOS 11**：2026-09-15 发布，Magic9 首发 | Android 17 | MagicOS 10：Android 16 |
| 华为 | 鸿蒙 | 鸿蒙 6 / 7（不能装 APK，见上） | 不是 Android | 鸿蒙 4.2 / 4.3：Android 兼容层 = Android 12（API 31） |

- 💭 结论：**这一年里各家「当前大版本」都是 Android 17，存量主力是 Android 16**（中国 StatCounter：16 占 42%）。Persona 真机验证要覆盖 Android 16 和 17 两代 ROM，再加一台鸿蒙 4.x。
- 来源：
  - 澎湃 OS 4：https://www.sohu.com/a/1064544461_115831 ，https://baike.baidu.com/item/%E5%B0%8F%E7%B1%B3%E6%BE%8E%E6%B9%83OS%204/67391378 ；澎湃 OS 3：https://9to5google.com/2025/09/26/xiaomi-android-16-list-schedule/
  - ColorOS 17：https://www.news.cn/photo/20260917/6e4a5b6e027341059efe0d7af0d78d19/c.html ，https://finance.sina.com.cn/tech/roll/2026-10-14/doc-inivcxxn0419857.shtml ；ColorOS 16：https://en.wikipedia.org/wiki/ColorOS
  - OriginOS 7：https://www.androidheadlines.com/2026/09/vivo-just-announced-global-originos-7-based-on-android-17-heres-whats-new.html ，https://www.gizmochina.com/2026/09/19/originos-7-update-timeline-for-vivo-and-iqoo-devices/ ；OriginOS 6：https://www.91mobiles.com/hub/android-16-release-date-eligible-phones/
  - MagicOS 11：https://www.ithome.com/0/999/493.htm ，https://phone.cnmo.com/news/820473.html （MagicOS 10 基于 Android 16：❓ 本次没单独查到来源，是常识性判断）

## 3. 各系统影响 Persona 的限制，以及怎么跳到设置页

> 先说结论（💭）：**没有任何一家国产系统给第三方 App 提供「查询 / 打开自启动、省电白名单」的公开接口**，各家的设置页组件名都是社区逆向出来的、随版本变；官方文档要么只有用户路径，要么停在 MIUI / EMUI 时代。
> 所以 Persona 的做法只能是：能用 API 查的就查（通知、渠道、电池优化、后台限制），查不了的给「去设置」按钮 + 文字步骤 + 自检（锁屏后发测试通知、hub 记录连接断开时长），并把每家每个版本的实际表现写进 `docs/android.md` 的「经验」。

### 3.1 所有系统通用：能查什么、怎么跳

| 项 | 能否用 API 查 | 标准跳转（AOSP） | 来源 |
| --- | --- | --- | --- |
| 通知总开关 | ✅ `NotificationManagerCompat.areNotificationsEnabled()`；33+ 先申请 `POST_NOTIFICATIONS` | `Settings.ACTION_APP_NOTIFICATION_SETTINGS` + `EXTRA_APP_PACKAGE` | XXPermissions `NotificationServicePermission`（下同） |
| 某个渠道 | ✅ `getNotificationChannel(id).importance != IMPORTANCE_NONE` | `Settings.ACTION_CHANNEL_NOTIFICATION_SETTINGS` + `EXTRA_CHANNEL_ID`（渠道还在、总开关开着时才有效） | 同上 |
| 悬浮 / 横幅、锁屏通知（各家自己实现的开关） | ❌ 小米官方：「目前没有接口可以查询权限是否开启」 | 只能进 App 的通知设置页 | 小米开发者 FAQ（转引）https://dev.mi.com/docs/appsmarket/technical_docs/adaptation_FAQ/ |
| 电池优化（Doze 白名单） | ✅ `PowerManager.isIgnoringBatteryOptimizations()` | `ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS` + `package:` URI（需声明 `REQUEST_IGNORE_BATTERY_OPTIMIZATIONS`；不上 Play 没有政策问题）；退路 `ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS` | https://developer.android.com/training/monitoring-device-state/doze-standby |
| App 的耗电页（无限制 / 优化 / 受限） | 部分：`ActivityManager.isBackgroundRestricted()`（28+，小米文档说被限制时返回 true）；`UsageStatsManager.appStandbyBucket`（28+） | `android.settings.VIEW_ADVANCED_POWER_USAGE_DETAIL` + `package:` URI：XXPermissions 实测 **12+ 才跳得过去** | XXPermissions `RequestIgnoreBatteryOptimizationsPermission`；小米 https://dev.mi.com/xiaomihyperos/documentation/detail?pId=1628 |
| 自启动 | ❌ 没有公开接口（MIUI 只有靠隐藏 API 的第三方库 `XomaDev/MIUI-autostart`，只测到 MIUI 14） | 只能用各家组件名（3.2） | https://github.com/XomaDev/MIUI-autostart |
| 最近任务加锁 | ❌ | 无，只能文字说明 | — |
| 被杀原因（事后诊断） | ✅ `ActivityManager.getHistoricalProcessExitReasons()`（30+）：`REASON_USER_REQUESTED`（用户在任务管理器 / 设置里停止）、`REASON_LOW_MEMORY`、`REASON_OTHER` 等 | — | https://developer.android.com/develop/background-work/services/fgs/handle-user-stopping |
| 识别是哪家系统 | ✅ 读系统属性（见下） | — | DeviceCompat 源码 |

- **跳到别家 App 的页面不需要「包可见性」**（✅ 官方：「You can start another app's activity using either an implicit or explicit intent regardless of whether that app is visible to your app」）。但 target 30+ 时 `resolveActivity()` 会被可见性过滤，查不到不代表没有。
  做法：直接 `startActivity` 并捕获 `ActivityNotFoundException`（组件没导出时还可能是 `SecurityException`），按候选列表依次试，最后退到 `ACTION_APPLICATION_DETAILS_SETTINGS`。
  只有想「事先决定按钮显不显示」时，才在 manifest 的 `<queries>` 里列出 `<package>`。
  来源：https://developer.android.com/training/package-visibility/automatic ，https://developer.android.com/training/package-visibility
- **识别系统**（✅ DeviceCompat 2.6 源码里的属性，2026-02 更新）：澎湃 OS `ro.mi.os.version.name` / `ro.mi.os.version.code`（版本 `ro.mi.os.version.incremental`）；MIUI `ro.miui.ui.version.name`；
  ColorOS `ro.build.version.oplusrom`（旧版 `ro.build.version.opporom`）；OriginOS / Funtouch `ro.vivo.os.build.display.id`；MagicOS `msc.config.magic.version` / `ro.build.version.magic`；
  鸿蒙 2–4 `ro.build.ohos.devicetype` / `hw_sc.build.platform.version`；**鸿蒙 NEXT 的安卓兼容环境（卓易通 / 出境易）** `ro.product.anco.devicetype` / `ro.sys.anco.product.software.version`。
  只看 `Build.MANUFACTURER` 不够（小米手机可能刷了别的系统）。来源：https://github.com/getActivity/DeviceCompat （`library/src/main/java/com/hjq/device/compat/DeviceOs.java`）
- 💭 建议在 `client_hello` 里带上系统名和版本（hub 汇总后就知道哪些机型的跳转失效了）——codewithkarani 那篇文章也提了同样的思路（「the only realistic way to find out that a component name has gone stale」）。来源：https://www.codewithkarani.com/blog/oem-autostart-battery-optimisation-android-deep-links （2026-07-29）

### 3.2 逐个系统

#### 小米 / 红米 / POCO：澎湃 OS（HyperOS）

| 项 | 现状 | 来源 / 置信度 |
| --- | --- | --- |
| 新装 App 的通知 | Android 13+ 新装 App 通知默认关，要运行时申请（小米官方适配指南）。在此之上，MIUI 自己实现的开关（悬浮通知、锁屏通知、角标、声音、呼吸灯）**默认都关，只有「允许振动」默认开**；悬浮还要求渠道 `IMPORTANCE_HIGH`、不在勿扰。**PR #84 在澎湃 OS 3（Android 16）真机上看到的一致**：渠道是最高级别，但默认不在通知栏弹出 | ✅ https://dev.mi.com/xiaomihyperos/documentation/detail?pId=1575 ；✅（第三方文章，MIUI 时代，未写版本）https://www.jianshu.com/p/9df4101b83d2 ；✅ `docs/android.md` 真机记录 |
| 通知过滤（重要 / 不重要） | 按「这台设备上该 App 通知的历史点击率」等打分，低于阈值折叠进「不重要通知」；**新 App 前 3 条默认算重要**；用户可以左滑「设为重要」，或在「通知与状态栏」里关掉「通知过滤」。只对国行生效 | ✅ https://dev.mi.com/xiaomihyperos/documentation/detail?pId=1577 （2024-10-09 更新，写的是 MIUI 9/10，❓ 澎湃 OS 是否还是这套没查到） |
| 自启动 | **默认不允许**，用户手动开 | ✅ https://dev.mi.com/xiaomihyperos/documentation/detail?pId=1624 （2024-09-25） |
| 省电策略 | 应用详情 › 省电策略（MIUI）/ 电量消耗（澎湃 OS）› **无限制**。Powerkeeper 在「后台长期持锁、后台短时间耗电过多、长时间后台不用、非法持有传感器」时会释放锁、杀进程；选「无限制」就不限；被限时 `isBackgroundRestricted()` 返回 true | ✅ https://dev.mi.com/xiaomihyperos/documentation/detail?pId=1628 （2022-12-07）；XXPermissions 注释（澎湃 OS 详情页叫「电量消耗」） |
| 系统级杀进程 | MIUI 进程管理有 AutoPowerKill（过度耗电）、AutoThermalKill（发热）；用户划掉后台后，官方建议「引导用户在安全中心中打开自启动开关」 | ✅ https://dev.mi.com/xiaomihyperos/documentation/detail?pId=1607 |
| 冻结 | 有开发者在小米 14 Ultra（澎湃 OS）上发现 GreezeManager 会在无障碍服务没有事件约 1 秒后用 cgroup 冻结它，冻结期间 wakelock 被强制失效、后台白名单无效。**这是无障碍服务，不是前台服务**；前台服务会不会被冻结 ❓ 没查到 | ✅（个人开发者报告）https://github.com/madeye/ad-skipper/releases/tag/v1.1 |
| 加锁 | 最近任务里把卡片下拉（或长按点锁） | ✅ https://dontkillmyapp.com/xiaomi |
| 锁屏清理 | 手机管家 › 省电与电池 › 右上角设置 › 「锁屏后清理内存」（有 1 / 5 / 10 / 30 分钟等档；有文章说默认「从不」）；博主测试说会保留微信、电话、闹钟。Persona 不在保留名单里，要靠加锁 / 无限制 | 💭 自媒体（澎湃 OS 3）https://www.toutiao.com/article/7639927547056112191/ ；其他家「锁屏清理」开关 ❓ 没查到可靠来源 |
| `ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS` | Android 11+ 的 MIUI / 澎湃 OS 把系统弹窗换成了小米自己的页面；返回后要等约 1 秒再查结果（澎湃 OS 2 + Android 15 上 200ms 就行） | ✅ XXPermissions 源码注释 https://github.com/getActivity/XXPermissions |
| 已知跳转 | 自启动：`com.miui.securitycenter/com.miui.permcenter.autostart.AutoStartManagementActivity`（PR #84 在用）；App 权限页：action `miui.intent.action.APP_PERM_EDITOR` + extra `extra_pkgname`；安全中心首页：action `miui.intent.action.SECURITY_CENTER`；省电策略（老）：`com.miui.powerkeeper/.ui.HiddenAppsConfigActivity` + extras `package_name` / `package_label`（MIUI 9–12 时代的博客，澎湃 OS ❓） | ✅ https://github.com/judemanutd/AutoStarter ；XXPermissions `PermissionSettingPage`；CSDN 旧文 https://blog.csdn.net/u010098889/article/details/72823013 |
| 读剪贴板 | 小米把它当成 AppOps 权限管理，商店审核要求「仅前台触发」 | ✅ https://dev.mi.com/xiaomihyperos/documentation/detail?pId=2225 （2025-12-30） |

#### OPPO / 一加 / 真我：ColorOS

| 项 | 现状 | 来源 / 置信度 |
| --- | --- | --- |
| 新装 App 的通知 | ColorOS 13 起按 Android 13 的运行时通知权限走（OPPO 官方适配文档）。更早的 ColorOS 3.0（2018 年前后）默认屏蔽第三方通知、只能用户手动开。**当前版本横幅 / 锁屏的默认值 ❓ 没查到官方说法** | ✅ https://open.oppomobile.com/new/developmentDoc/info?id=11311 ；旧：https://blog.csdn.net/u012571415/article/details/79283390 |
| 自启动 / 后台 | ColorOS 12+：设置 › 应用 › 自启动；单个 App：应用详情 › **耗电管理**：允许唤醒前台 / **允许完全后台行为** / 允许应用自启动 / 允许应用关联启动。社区解释：「允许完全后台行为」≈ 原生「无限制」。另有说法：ColorOS 有自己的电池优化白名单，改原生「电池优化」会被重置 | 💭 社区（V2EX 2023/2024，ColorOS 13/14）https://v2ex.com/t/952213 ，https://s.v2ex.com/t/1037649 ；ColorOS 15 仍有「自启动」入口：https://zeeko.dev/2025/03/android-user-root-upgrade-coloros-15/ |
| `ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS` | ColorOS 15 上弹出的是不透明页面，14 / 16 Beta 上是透明弹窗 | ✅ XXPermissions 源码注释（逐机型测试记录） |
| 应用详情页跳转优化 | 跳 `ACTION_APPLICATION_DETAILS_SETTINGS` 时带 `permissionList`（受阻的权限名）+ `isGetPermission=true`，ColorOS 会直接定位到对应权限（只对运行时权限有用） | ✅ XXPermissions 源码，引 OPPO 文档 https://open.oppomobile.com/new/developmentDoc/info?id=12983 |
| 已知跳转 | `com.coloros.safecenter/.permission.startup.StartupAppListActivity`、`com.coloros.safecenter/.startupapp.StartupAppListActivity`、`com.oppo.safe/.permission.startup.StartupAppListActivity`（都是 ColorOS 11 及更早的社区记录）；安全中心包名还有 `com.oplus.safecenter`、`com.color.safecenter` | ✅ AutoStarter；XXPermissions。❓ ColorOS 15/16/17 上的组件名没查到 |
| 加锁 | 最近任务里锁定 | 💭 社区 |
| 读剪贴板 | ColorOS 12 起读剪贴板有提示；ColorOS 14 在「权限与隐私 › 剪贴板管理」里可以按 App 设读取权限 | 💭 社区 https://bbs.oneplus.com/thread/6223871 |

#### vivo / iQOO：OriginOS（Funtouch 已退出）

| 项 | 现状 | 来源 / 置信度 |
| --- | --- | --- |
| 新装 App 的通知 | 2021-08 之后发布的机型：新装 App **第一次打开时系统弹一次窗**，「允许」= 应用通知、锁屏通知、悬浮通知、桌面角标一起开；「禁止」或直接退出 = 全关，**只弹一次**（重装才重置）。悬浮通知受开关和渠道优先级共同控制；锁屏通知受 App 的「锁屏通知」开关控制。这套和 Android 13 运行时权限怎么叠加 ❓ 没查到 | ✅（第三方转载的 vivo 文档《vivo通知授权弹窗说明》《OriginOS通知样式规范》，2022；原文在 https://dev.vivo.com.cn/documentCenter/doc/577 ，页面靠 JS 渲染读不到）https://www.yimenapp.com/kb-yimen/12014/ ，https://www.yimenapp.com/kb-yimen/11646/ |
| 自启动 | 设置 › 应用与权限 › 权限管理 › 自启动（OriginOS 6 的社区教程；老系统在 i管家） | 💭 社区 https://faq.coros.com/help/641aa9e94187afd985fc53e6/6421412205be2e4057ad100b?locale=zh-CN |
| 后台 | 设置 › 电池 › 后台耗电管理 › App › **允许后台高耗电** | 💭 社区；华为写给 vivo 用户的帮助页也是这条 https://consumer.huawei.com/cn/support/content/zh-cn15977902/ |
| 已知跳转 | `com.vivo.permissionmanager/.activity.BgStartUpManagerActivity`、`com.iqoo.secure/.ui.phoneoptimize.AddWhiteListActivity`、`com.iqoo.secure/.ui.phoneoptimize.BgStartUpManager` | ✅ AutoStarter（老）；❓ OriginOS 6/7 上是否还在没查到 |
| 加锁 | 最近任务里点卡片上的菜单 › 锁定 | ✅ https://dontkillmyapp.com/vivo |

#### 荣耀：MagicOS

| 项 | 现状 | 来源 / 置信度 |
| --- | --- | --- |
| 新装 App 的通知 | ❓ 没查到 MagicOS 的默认值 | — |
| 应用启动管理 | 设置 › 应用 › **应用启动管理** › 关掉该 App 的「自动管理」› 打开允许自启动、允许关联启动、允许后台活动（MagicOS 7.0+）；电池优化 › 不允许；多任务卡片下滑加锁 | ✅ 荣耀官方 https://www.honor.com/cn/support/content/zh-cn00428704/ ；https://consumer.huawei.com/cn/support/content/zh-cn15967050/ |
| **智能省流量** | 「将阻止应用在后台使用移动数据」：设置 › 移动网络 › 流量管理 › 关掉。**对 Persona 的常驻连接是致命项**（流量下后台断网） | ✅ 荣耀官方，同上 |
| 其他 | 电池模式选「均衡 / 性能」，关省电 / 低电量模式；系统管家 › 扩展业务 › 打开「智能维护」 | ✅ 同上 |
| 已知跳转 | 分家前的荣耀机型沿用 `com.huawei.systemmanager/...`；新机型系统管家包名是 `com.hihonor.systemmanager`，具体组件名 ❓ 没查到可靠来源 | ✅（包名，来源弱）https://www.volcengine.com/article/174693 |

#### 华为：鸿蒙 4.x 及更早（能装 APK）

| 项 | 现状 | 来源 / 置信度 |
| --- | --- | --- |
| **本地通知被分类、限频** | 2023-09-15 起华为对「应用自己调系统接口发的通知」按《华为消息分类标准》灰度管控：**没有申请「自分类权益」的 App，本地通知按「资讯营销」处理**——静默（只在下拉通知栏，没有横幅、锁屏、声音），而且和云端推送共用每天 2 或 5 条的资讯营销配额，超了可能「甚至无法弹出」。自分类权益在 AppGallery Connect 申请；申请到后用 `Notification.category` 标类型（融云示例是 `CATEGORY_MESSAGE`）。❓ **不上华为应用市场的 App 能不能申请，没查到** | ✅ 华为公告（经融云、腾讯云转引）https://help.rongcloud.cn/t/topic/1085 ，https://cloud.tencent.com/document/product/548/97927 ；华为原文 https://developer.huawei.com/consumer/cn/doc/HMSCore-Guides/message-classification-0000001149358835 （JS 渲染，未能直接读到） |
| 用户侧补救 | 设置 › 应用管理 › Persona › 通知管理：打开允许通知；按类别打开通知铃声、横幅、锁屏、允许打扰；**关掉「静默通知」** | ✅ 华为官方 https://consumer.huawei.com/cn/support/content/zh-cn15872069/ |
| 应用启动管理 | 设置 › 应用启动管理 › 关掉自动管理 › 打开允许自启动、允许关联启动、允许后台活动 | ✅ 同上（鸿蒙 4.x 及更早） |
| 电池 | 电池优化 › 所有应用 › 不允许；**电池 › 更多电池设置 › 「休眠时始终保持网络连接」打开**（对常驻连接是关键项）；关智能省流量 | ✅ 同上 |
| 已知跳转 | `com.huawei.systemmanager/.startupmgr.ui.StartupNormalAppListActivity`（Android 9+ 的 EMUI，多个来源一致）；`.appcontrol.activity.StartupAppControlActivity`（Android 8）；`.optimize.process.ProtectActivity`（更老）；`.optimize.bootstart.BootStartActivity`（EMUI 5） | ✅ https://gist.github.com/ru-huawei-android/7296148b24e0079c40073efc34f628a4 ，AutoStarter；P-Pass 在鸿蒙 4.2 真机上验证过「应用启动管理」三个开关 https://github.com/hawkeye-xb/P-Pass/pull/1086 （2026-10-21） |
| 加锁 | 多任务卡片下拉加锁 | ✅ 同上 |

#### 华为：鸿蒙 5 / 6 / 7（只能经卓易通容器）

见 2.1：要用户装「卓易通」，App 跑在容器里；通知要在卓易通里单独打开，而且 App 要在后台活着。DeviceCompat 能识别这种环境（`isHarmonyOsNextAndroidCompatible`）。💭 **首版不承诺支持**，识别到时在配对页直接说明「需要在卓易通里允许通知、并保持在后台；体验可能不完整」。

### 3.3 对 Persona 的建议（💭）

1. **准备清单分两类**：
   - 能自动检查的（通知总开关、`input` 渠道级别、电池优化、`isBackgroundRestricted`）：显示真实状态，回到 App（`onResume`）时重查。
   - 不能检查的（悬浮 / 锁屏通知、自启动、无限制 / 允许完全后台行为 / 允许后台高耗电、加锁、华为的静默通知和「休眠时保持网络」、荣耀的智能省流量）：按识别出的系统给出**那一家、那一版**的文字路径 + 「去设置」按钮，让用户点「已完成」；再用「自检」兜底：
     - 锁屏 30 秒后发一条测试通知，问用户「刚才响了吗、亮屏了吗」；
     - hub 记录这台设备 WS 断开的时间段（hub 侧本来就知道），设置页显示「过去 24 小时后台断开 N 次，最长 X 分钟」。
2. **跳转候选按系统分组、写成数据**，按顺序试，失败退到应用详情页；每次跳转结果（成功的组件、失败的异常）进本地日志，`client_hello` 带系统名和版本。
3. **通知渠道的级别建好就改不了（只有用户能改）**：PR #84 的 `后台连接` 渠道是 `IMPORTANCE_MIN`，**不能被提升为 Live Update**（第 5 节），重组时要新建一个渠道 id。
4. **华为鸿蒙 4.x 是通知最难的一家**：静默 + 限频。首版只能靠用户手动关「静默通知」、开横幅和锁屏；「需要你」的提醒同时依赖常驻通知上的计数（常驻通知是前台服务的，不受「资讯营销」配额影响？❓ 要真机测）。

## 4. 前台服务常驻连接

### 4.1 平台规则（✅ 事实）

| 规则 | 内容 | 对 Persona 的含义 |
| --- | --- | --- |
| `specialUse` 类型 | 34 起必须声明类型；`specialUse` 要 `FOREGROUND_SERVICE_SPECIAL_USE`，**没有运行时前提、没有时间上限**（时间上限只有 `shortService` 约 3 分钟、`dataSync` / `mediaProcessing` 每 24 小时 6 小时）；`PROPERTY_SPECIAL_USE_FGS_SUBTYPE` 只在 Play 审核时看 | PR #84 的选择是对的，不上 Play 没有审核问题 |
| 后台启动前台服务（target 31+） | 后台不能起前台服务，除 14 条豁免外抛 `ForegroundServiceStartNotAllowedException`。豁免里和 Persona 有关的：从可见状态转入、**用户点了通知 / 小组件**、**收到 `BOOT_COMPLETED` / `LOCKED_BOOT_COMPLETED` / `MY_PACKAGE_REPLACED`**、**用户关了这个 App 的电池优化** | 开机、升级后都能起；**电池优化白名单不只是为了 Doze，它也是「随时能从后台起前台服务」的豁免** |
| `START_STICKY` | 官方豁免列表里**没有**「系统重启粘性服务」。社区实测：进程被杀后系统把服务拉起在后台，`onStartCommand` 里 `startForeground` 抛 `ForegroundServiceStartNotAllowedException`（`mAllowStartForeground false`），除非当时正好有别的豁免（比如关了电池优化） | 重启时 intent 为 null；`startForeground` 必须 try/catch，失败就 `stopSelf()`，等下一个豁免时机（用户打开 App、点通知、开机、App 升级）；网络变化不是豁免 |
| `BOOT_COMPLETED`（target 35+） | 开机广播里不能起 dataSync、camera、mediaPlayback、phoneCall、mediaProjection、microphone 类前台服务；**`specialUse` 不在列表里** | PR #84 的 `BootReceiver` 在 target 35/36/37 下仍可用。国产系统上收不收得到开机广播取决于「自启动」（小米默认关） |
| Doze 下的网络 | 官方 Doze 文档只写「Suspends network access」，没说前台服务例外；耗电限制总表里「App process is running a foreground service」一行的网络是「No restrictions」。**AOSP 源码**：Doze / 省电模式下放行网络的条件是 `procState <= PROCESS_STATE_BOUND_FOREGROUND_SERVICE`（或有 `POWER_RESTRICTED_NETWORK` 能力）；Doze 下禁用 partial wakelock 的条件也是 `procState > BOUND_FOREGROUND_SERVICE` 且不在白名单 | 原生系统上，**有前台服务的进程在 Doze 里仍能联网、能持锁**。厂商可以改（冻结、断网），所以要真机测 |
| 用户在「活跃应用」里点停止（13+） | 整个 App 从内存移除，前台服务通知消失，**不给任何回调**；定时任务和闹钟照常；下次启动时 `ApplicationExitInfo` 里是 `REASON_USER_REQUESTED`。测试：`adb shell cmd activity stop-app <包名>` | 用户主动停了就不要偷偷拉起；下次打开时提示「后台连接被你停止了」 |
| 前台服务通知可被划掉（14+） | `setOngoing(true)` 的通知用户可以划掉（锁屏时和「全部清除」除外）；服务本身不受影响 | 被划掉后常驻汇总就看不到了；考虑在下次状态变化时重发，或在设置里说明 |
| 前台服务通知延迟（12+） | 系统默认最多等 10 秒才显示前台服务的通知；带操作按钮或 `setForegroundServiceBehavior(FOREGROUND_SERVICE_IMMEDIATE)` 时立即显示 | 常驻通知用 `IMMEDIATE` |
| JobScheduler 配额（16+，所有 App） | 前台服务运行时，同时跑的 job 也受配额约束（以前不受） | outbox 的 WorkManager 补发在 16+ 上不能指望「有前台服务就不限」 |
| 内存上限（17+，所有 App） | 按设备总内存给 App 设上限，超了被杀，退出原因 `REASON_OTHER` + `MemoryLimiter:AnonSwap` | 常驻进程要控内存（缓存、图片） |
| Doze 下的闹钟 | `setAndAllowWhileIdle` / `setExactAndAllowWhileIdle` 每个 App 每 9 分钟最多一次 | 不能靠闹钟做高频心跳 |

来源：
- 类型和时间上限：https://developer.android.com/develop/background-work/services/fgs/service-types
- 后台启动豁免：https://developer.android.com/develop/background-work/services/fgs/restrictions-bg-start
- `START_STICKY` 重启时抛异常（社区，2026-10）：https://github.com/marcocrupi/react-native-notify-kit/issues/81 ；https://github.com/invertase/notifee/issues/470
- Android 15 开机广播限制：https://developer.android.com/about/versions/15/behavior-changes-15
- Doze：https://developer.android.com/training/monitoring-device-state/doze-standby ；耗电限制总表：https://developer.android.com/topic/performance/power/power-details
- AOSP 源码（GitHub 镜像，2025-11 归档，约等于 Android 16）：`NetworkPolicyManager.isProcStateAllowedWhileIdleOrPowerSaveMode` https://raw.githubusercontent.com/aosp-mirror/platform_frameworks_base/main/core/java/android/net/NetworkPolicyManager.java ；`PowerManagerService` 的 Doze wakelock 规则 https://raw.githubusercontent.com/aosp-mirror/platform_frameworks_base/main/services/core/java/com/android/server/power/PowerManagerService.java
- 用户停止：https://developer.android.com/develop/background-work/services/fgs/handle-user-stopping
- 14 的可划掉通知：https://developer.android.com/about/versions/14/behavior-changes-all
- 10 秒延迟：https://developer.android.com/guide/components/foreground-services#notification-immediate
- 16 / 17：https://developer.android.com/about/versions/16/behavior-changes-all ，https://developer.android.com/about/versions/17/behavior-changes-all
- **Android 15 / 16 / 17 没查到任何针对 `specialUse` 或「常驻前台服务」的新限制**（三个版本的行为变更页都看过）。

### 4.2 各家国产系统怎么对待常驻前台服务

| 系统 | 已知行为 | 白名单入口 | 来源 / 置信度 |
| --- | --- | --- | --- |
| 澎湃 OS | 自启动默认关；Powerkeeper 按「后台持锁、后台耗电、长期不用、持有传感器」杀进程，「无限制」后不限；有 GreezeManager / Millet 一类的冻结机制（无障碍服务被冻结的报告）；社区说划掉卡片后进程被杀且不能自启动 | 自启动 + 电量消耗「无限制」+ 加锁 | ✅ 小米文档（见 3.2）；💭 社区：https://www.volcengine.com/article/224461 ，https://xdaforums.com/t/hyperos-3-0-cn-instantly-kills-my-background-apps.4792408/ |
| ColorOS | 「允许完全后台行为」≈ 无限制；社区说关掉 `OKillerAppService` 就不杀 FCM（需要 root / adb，不适用）；ColorOS 13 起「暂停长期未使用的应用活动」；官方宣传 ColorOS 17「全应用保活率提升 55.6%」（口径是 App 切换，不是前台服务） | 自启动 + 耗电管理 + 锁定 | 💭 https://www.v2ex.com/t/1179724 ；宣传：https://finance.sina.com.cn/tech/roll/2026-10-14/doc-inivcxxn0419857.shtml |
| OriginOS | 「允许后台高耗电」；个人测试（OriginOS 1 / Android 11，单机）说长时间灭屏后可能「假死」 | 自启动 + 后台高耗电 + 锁定 | 💭 https://blog.moeworld.tech/2025/10/15/vivo%E6%89%8B%E6%9C%BA-originos-oem%E5%AE%9A%E5%88%B6%E5%AE%89%E5%8D%93%E7%B3%BB%E7%BB%9F-%E5%BA%94%E7%94%A8%E4%BF%9D%E6%B4%BB/ （版本太老，只作参考） |
| MagicOS | 应用启动管理（自动管理会「禁止应用不必要的启动」）；**智能省流量会阻止后台用移动数据**；有用户在 Magic5 Pro 上按客服设置后，导航 App 切走几分钟仍被杀 | 应用启动管理三个开关 + 电池优化 + 加锁 + 关智能省流量 | ✅ 荣耀官方；💭 https://club.honor.com/cn/thread-28539008-1-1.html |
| 鸿蒙 4.x | 应用启动管理；**「休眠时始终保持网络连接」**（默认值 ❓）；本地通知限频 | 应用启动管理 + 电池优化 + 休眠保持网络 + 加锁 | ✅ 华为官方（见 3.2） |
| 鸿蒙 5/6/7 卓易通 | 卓易通本身可能被杀；容器里的前台服务 ❓ | 卓易通加后台白名单 | 💭 见 2.1 |

- ❓ **没有找到任何一家「开了白名单之后前台服务能活多久」的公开量化数据**（官方没有，社区只有零散的「被杀了」）。网上的「后台保活率 92% / 85% / 62%」都是消费向文章测的「切换 App 不重载」，和常驻连接不是一回事，不能引用。
- 💭 dontkillmyapp 的排名（数字越大越差）：小米 5 分、第 2 名；华为 5 分、第 1 名；OPPO 4 分；vivo 3 分。这些页面主要写的是 MIUI / EMUI / ColorOS 6 时代，新系统没更新，只能看出「历来最凶」的是华为和小米。
  来源：https://dontkillmyapp.com/api/v2/xiaomi.json ，https://dontkillmyapp.com/api/v2/huawei.json ，https://dontkillmyapp.com/api/v2/oppo.json ，https://dontkillmyapp.com/api/v2/vivo.json

### 4.3 耗电：有没有公开实测

- ✅ ntfy（开源推送，自托管时用前台服务常驻 JSON 流 / WebSocket）官方 FAQ：常驻连接「consumes about 0-1% of battery in 17h of use (on my phone)」。**只有作者一台手机、没写机型和网络**；更早版本的 FAQ 写的是约 4% / 17 小时。
  ntfy-android v1.25.2 把客户端 WebSocket ping 从 1 分钟改成 3 分钟，说明「should slightly improve battery life」，但没给数字。
  来源：https://docs.ntfy.sh/faq/ ，https://github.com/binwiederhier/ntfy-android/releases/tag/v1.25.2
- ✅ 微信 Mars 团队（2017 年前后的文章）：心跳太密会频繁把基带拉进高功耗状态，建议心跳间隔最短 4.5 分钟；实测某地区移动、联通的 NAT 超时 5 分钟，电信大于 28 分钟；Mars 的自适应心跳区间是 4.5 分钟到 9 分 50 秒。
  来源：https://blog.csdn.net/byeweiyang/article/details/73296619 （转载），https://www.cnblogs.com/xifengcoder/p/15340365.html （源码参数解读）
- ❓ **没有找到国产系统上「前台服务 + WebSocket」的耗电实测**。Persona 只能自己测（`docs/redesign/android.md` 第 10 节已列为 A1 必测项）。

### 4.4 可行性和风险（💭 判断）

- **可行**：原生规则下 `specialUse` 前台服务没有时长上限，Doze 下有前台服务的进程仍能联网；PR #84 已在澎湃 OS 3 真机上跑通了前台交互（锁屏长时间未测）。
- **主要风险**按大小排：
  1. 用户没做完白名单步骤：自启动、无限制、加锁缺一项，就可能「锁屏一会儿就断」。这是体验问题，不是技术问题，靠准备清单和自检兜底。
  2. 进程被杀后拉不起来：`START_STICKY` 在后台不是豁免，国产系统又常常不重启服务。缓解：电池优化白名单（同时是后台起前台服务的豁免）+ 开机广播 + 一个周期 WorkManager 任务（最短 15 分钟）检查服务在不在、不在就起（只有关了电池优化时才允许从后台起；国产系统上进程被杀后 job 能不能跑还取决于自启动）+ 用户下次打开 App 时立即恢复。进程死了以后，网络变化之类的回调都收不到。
  3. 厂商冻结：澎湃 OS、ColorOS 都有 cgroup 冻结，前台服务进程是否豁免没有公开说法。要真机测「锁屏 8 小时后 hub 还能不能推到」。
  4. 网络被单独限制：荣耀「智能省流量」、华为「休眠时始终保持网络连接」这种开关会让连接在流量 / 休眠下断掉，而进程还活着——**表现是「通知不来、App 一打开就恢复」**，最容易误判成 hub 的问题。
  5. 耗电：取决于心跳。现在两边都是 20 秒一次：PR #84 的 OkHttp `pingInterval(20, SECONDS)`（`android/.../data/ApiClient.kt`），hub 侧也每 20 秒发一次 WebSocket ping（`server/internal/hub/http.go`）；在后台太密（每分钟约 3 次唤醒基带）。建议：前台用短 ping 判弱网；**后台改成 hub 发心跳、间隔几分钟**（参照 Mars 的 4.5 分钟起、ntfy 的 3 分钟），手机在 Doze 里不依赖自己的定时器（CPU 睡着时 OkHttp 的计时不走，闹钟又有 9 分钟一次的限制）。公网直连（另一份调研）时还要考虑运营商 NAT / IPv6 防火墙的空闲超时，间隔要实测定。
- **测试方法**（建议写进 A1）：`adb shell dumpsys deviceidle force-idle` 只能模拟原生 Doze，测不出厂商策略；必须真机、不插电、灭屏放 8 小时，hub 侧记录断开区间，手机侧记 `ApplicationExitInfo`，每台手机至少 10 晚样本再下结论（用户约定：样本 < 10 要标注）。

## 5. 锁屏常驻通知 / 灵动岛类能力

### 5.1 标准接口（✅ 事实，来自 android-tech.md 的已查证部分）

- Android 16（API 36）：Live Updates。条件：样式是 Standard / BigText / Call / ProgressStyle（17 起加 MetricStyle）之一；manifest 声明 `POST_PROMOTED_NOTIFICATIONS`；`setRequestPromotedOngoing(true)`；必须 ongoing、有标题；不能有自定义 RemoteViews、不能是分组摘要、不能 colorized；**渠道不能是 `IMPORTANCE_MIN`**。**官方明确写了 OEM 可以加额外条件**。用户可关：`NotificationManager.canPostPromotedNotifications()` 查，`Settings.ACTION_MANAGE_APP_PROMOTED_NOTIFICATIONS` 跳。状态栏胶囊用 `setShortCriticalText`。
- Android 17（API 37）：`Notification.MetricStyle`，最多 3 个指标、3 个按钮，可被提升为 Live Update。
- 来源：https://developer.android.com/develop/ui/views/notifications/live-update ，https://developer.android.com/develop/ui/views/notifications/metric-style ，https://developer.android.com/about/versions/17/features

### 5.2 各家「岛」对标准接口的支持

| 系统 | 名字 | 标准 Live Updates（`setRequestPromotedOngoing`） | 厂商私有接口 | 来源 / 置信度 |
| --- | --- | --- | --- | --- |
| 澎湃 OS 3.0 | 超级岛 | ❌（3.0.300 以前不显示原生 Live Updates） | 焦点通知 / 超级岛模板：要先完成小米开发者认证、选择**已在小米应用商店上架**的应用开通服务、配证书指纹，提交场景预审和完整方案，白名单设备（按 OAID，最多 10 台、30 天过期）测试后再申请上线验证——**审核 + 白名单制，Persona 用不了**（文档 2025-12-05 更新） | ✅ 小米官方 https://dev.mi.com/xiaomihyperos/documentation/detail?pId=2132 ，https://dev.mi.com/xiaomihyperos/documentation/detail?pId=2141 |
| 澎湃 OS 3.1（OS3.0.300+）/ 4 | 超级岛 | ✅ 3.1 起超级岛支持 Android 16 Live Updates，第三方 App 不用小米代码 | 同上 | 💭 新闻 + 开源项目实测（Capsulyric 标「Verified」：「For HyperOS, version 3.0.300+ is required」）：https://www.huaweicentral.com/hyperos-3-1-finally-brings-a-stacked-recent-apps-layout-option/ ，https://github.com/FrancoGiudans/Capsulyric/wiki ；没有小米官方文档 |
| ColorOS 16 / 17 | 流体云 | ✅ **官方表态**：流体云「全面兼容」Android 16 实时活动 API，遵循谷歌规范的应用可直接适配（「双兼容」：标准接口和 OPPO 自己的接口都行） | OPPO 自有接口要申请 serviceId | ✅ IT之家 2025-10-17（OPPO 设计负责人表态）https://www.ithome.com/0/890/345.htm ；💭 社区 https://github.com/huverse/SuperXD/pull/93 |
| OriginOS 6 / 7 | 原子岛 / 原子通知 | 海外版：与 Android 16 Live Updates 深度集成；**国行版对标准接口的支持 ❓ 没查到官方说法**（社区有「原子岛也读标准通知」的说法，未验证） | 国行上第三方要和 vivo 合作接入（京东外卖、12306、高德、支付宝等都是逐个宣布的） | 💭 https://tech.udn.com/tech/story/123151/9075516 ，https://github.com/ccch1mneyyy/dsh-go/issues/70 ，https://finance.sina.com.cn/tech/digi/2025-06-11/doc-ineztfhw4487045.shtml |
| MagicOS（10 / 11） | 灵动胶囊 | 💭 社区：升到 Android 16 后，Live Updates 会显示进灵动胶囊（只在荣耀上测过） | 非公开的 `notification.live.*` extras 协议（社区逆向，在 MagicOS / Android 17 真机上验证过）；官方说胶囊「逐步扩展至第三方应用」 | 💭 https://linux.do/t/topic/3064474 （WebFetch 403，内容来自搜索摘要），https://github.com/The-Brotherhood-of-SCU/Bugaoshan/pull/368 ，https://www.sina.cn/weibo/detail/5357264527428248.html |
| 鸿蒙 4.x | 实况窗 | ❌（Android 16 的接口在 API 31 上不存在） | 社区：本地通知带 `notification.live.*` extras 可以触发实况窗（非公开）；**官方实况窗（Live View Kit）要鸿蒙 5+、要申请准入、只在中国大陆** | 💭 https://github.com/The-Brotherhood-of-SCU/Bugaoshan/issues/363 ；✅ https://developer.huawei.com/consumer/cn/doc/HarmonyOS-Guides/liveview-introduction （搜索摘要） |

- **MetricStyle（Android 17）在这几家上怎么显示：❓ 完全没查到**（官方文档、新闻、开源项目都没有提到国产系统）。
- 💭 普通 App 能用到的程度：
  - ColorOS 16+、澎湃 OS 3.1+：用标准 Live Updates 就能上岛 / 胶囊，**不需要白名单**（ColorOS 有官方表态，小米只有新闻和社区实测）；
  - OriginOS 国行、MagicOS：标准接口大概率能显示在通知栏和锁屏的「提升」位置，能不能上岛要真机看；
  - 厂商私有接口（小米焦点通知、OPPO serviceId、vivo 合作、荣耀 / 华为的 `notification.live.*`）都是白名单或非公开协议，**首版不碰**。

### 5.3 对 Persona 的影响（💭）

1. PR #84 的常驻通知渠道是 `IMPORTANCE_MIN`，**按规则不能被提升**；渠道级别建好后 App 改不了。重组时新建一个 `IMPORTANCE_LOW`（不响、能提升）的「实时状态」渠道，常驻通知改用它；旧渠道删掉。
2. 有活动（需要你 / 工作中）时才请求提升，空闲时取消提升（重设计 5.10 已有），这也减少被厂商「降级」的概率。
3. 「需要你」的提醒继续走普通高优先级通知，不依赖岛（重设计 5.10 已有）。官方用途规范本来就不鼓励「别人触发的活动」和提醒类内容，各家岛也都是按「出行 / 外卖 / 计时」这类场景调的。
4. 真机矩阵（第 8 节）里每台都要截一张「有活动时的状态栏 / 锁屏 / 岛」的图，确认标准接口在这台上的实际样子。

## 6. 没有 Google 服务时的系统能力

总原则（💭）：**凡是包名 `com.google.android.gms` 或「经 Google Play 服务下发 / 更新」的东西，国行手机上都当不存在**。没有 GMS 的手机也收不到「Google Play 系统更新」（Mainline 模块），这些模块只能跟着厂商 OTA 更新。

| 能力 | 国行（无 GMS）现状 | Persona 怎么做 | 来源 / 置信度 |
| --- | --- | --- | --- |
| 推送（FCM） | 没有 | 常驻连接为默认（PR #84 已是），FCM 只给有 GMS 的手机做可选项 | 已定 |
| **Health Connect** | 官方：「requires … Google Play services installed」「exclusive to Android and Google Play」；14+ 是系统的一部分。OPPO 健康**国行版完全没有 Health Connect 集成**（Manifest 没有 health 权限、dex 里没有相关引用，用的是签名级私有接口）；小米 / vivo / 荣耀国行的情况 ❓ 没查到；华为用自己的 Health Kit | `phone_health_summary` 在国行上**基本拿不到数据**：先 `HealthConnectClient.getSdkStatus()`，不可用就不注册这个工具（或回「这台手机没有健康数据来源」）；**建议首版对国行去掉**（iOS 不受影响） | ✅ https://developer.android.com/health-and-fitness/health-connect/availability ；✅（开源项目逆向）https://github.com/svip886/OHSync |
| 系统照片选择器 | AndroidX `PickVisualMedia`：33+ 一律用系统的 `MediaStore.ACTION_PICK_IMAGES`；30–32 要 SDK 扩展版本 ≥ 2（靠 Google 系统更新推的模块，**国行大概率没有**）；再不行找系统应用提供的 `androidx.activity.result.contract.action.PICK_IMAGES`；最后退到 `ACTION_OPEN_DOCUMENT`（选择数量上限失效）。Android 4.4–10 的「经 Play 服务装回移植版」国行用不了 | 直接用 AndroidX（自动退回）。minSdk 31 时，12 / 12L 和鸿蒙 4（API 31）会走文件选择器，界面不同但能用。❓ 33+ 的国产系统有没有改掉 / 替换系统选择器，要真机看 | ✅ https://developer.android.com/training/data-storage/shared/photopicker ；AndroidX 源码 https://raw.githubusercontent.com/androidx/androidx/androidx-main/activity/activity/src/main/java/androidx/activity/result/contract/ActivityResultContracts.kt |
| 定位 | Fused Location Provider（`play-services-location`）没有。`LocationManager` 的 GPS 一定有；`NETWORK_PROVIDER` 靠厂商自己的网络定位服务——老资料说国行原生网络定位常常拿不到（高德 / 百度用自己的 SDK），新系统 ❓ 没查到 | 用 `LocationManagerCompat.getCurrentLocation`，按 `getProviders(true)` 选 provider（12+ 先试平台的 `FUSED_PROVIDER`，再 GPS、NETWORK），带超时；返回里写明坐标系（GPS 是 WGS-84，国内地图用 GCJ-02，要不要换由 hub / AI 决定） | 💭 https://blog.csdn.net/u013040434/article/details/117700746 （旧，5 年前）；坐标系是常识性判断 |
| `Geocoder` | AOSP 文档：后端服务不在核心框架里，没有后端时查询返回空列表，用 `isPresent()` 判断。国行上一般由 GMS 提供，**国行有没有后端 ❓ 没查到** | 先 `isPresent()`，结果为空就只回坐标；以后需要地址再在 hub 侧接一个可配置的逆地理服务（P2） | ✅ AOSP 注释（经 Geocoder 源码 / 文档转述）https://developer.android.com/reference/android/location/Geocoder |
| 系统 WebView | 小米 / OPPO / vivo / 荣耀新机预装 Google 的 Android System WebView，但**有说法称厂商应用商店不提供它的更新，只能跟 OTA**（一年不到 4 次）；也有教程说小米、vivo 商店能搜到——**两种说法冲突 ❓**。华为（鸿蒙 4）由华为浏览器提供 WebView 内核，在华为应用市场更新华为浏览器；鸿蒙 NEXT 用自研内核 | androidx.webkit 的新能力（`startUpWebView`、`prerenderUrlAsync`、`NavigationListener`）一律先 `WebViewFeature.isFeatureSupported()` 判断，不支持就退回普通加载；设置 › 诊断里显示 `WebViewCompat.getCurrentWebViewPackage()` 的包名和版本 | 💭 知乎（403 未能直接读，来自搜索摘要）https://zhuanlan.zhihu.com/p/2077738698548982270 ；https://www.cnblogs.com/reachteam/p/22796594/android-webview-update-guide （2026-09-01） |
| 通讯录 | `ContactsContract` 是 AOSP 提供者，国行都有；没查到国产系统的额外限制 | 照原方案；真机验证读取和搜索 | 💭 |
| 日历 | `CalendarContract` 是 AOSP 提供者。没有 Google 账号时可能没有可写的日历：官方做法是用 sync adapter 身份（URI 带 `CALLER_IS_SYNCADAPTER=true`、`ACCOUNT_TYPE_LOCAL`）建一个本地日历 | 新建日程前先找可写日历，没有就建「Persona」本地日历；❓ 各家日历 App 会不会显示它，要真机看 | ✅ https://developer.android.com/identity/providers/calendar-provider |
| 剪贴板 | AOSP：10 起只有前台 App 能读，12 起读的时候系统提示。小米把读剪贴板当 AppOps 权限管（商店政策：只能前台触发）；ColorOS 14 有按 App 的「剪贴板读取权限」和读取提醒；鸿蒙 4 ❓ | 只在前台读；读到 null 时提示「检查系统设置里的读取剪贴板权限」 | ✅ https://dev.mi.com/xiaomihyperos/documentation/detail?pId=2225 ；💭 https://bbs.oneplus.com/thread/6223871 |
| 扫码 | zxing-android-embedded：只依赖 `zxing:core`，不依赖 GMS（实际要 API 24+，最新 4.3.0，版权年份 2022，更新慢）。另一个选择：ML Kit **bundled** 模型 `com.google.mlkit:barcode-scanning`（模型静态打进 APK，约 +2.4 MB）；**unbundled** 版（`play-services-mlkit-barcode-scanning`）和 Google code scanner 都要 GMS | 保留 zxing（PR #84 已验证）。以后要 Compose 取景框：CameraX + ML Kit bundled（❓ bundled 版在无 GMS 机上是否完全不碰 GMS，官方页面没明说，要真机测） | ✅ https://github.com/journeyapps/zxing-android-embedded ，https://developers.google.com/ml-kit/vision/barcode-scanning/android |
| 可下载字体 | Compose 的 Google Fonts 下载用的 provider 包名就是 `com.google.android.gms`，国行没有 | 5 套风格用到的字体全部打进 APK（`R.font`），不用可下载字体 | ✅ https://developer.android.com/develop/ui/compose/text/fonts |
| Google 登录拿 Gmail 授权 | 重设计 6.1 写的「Google Authorization API」属于 Play 服务的 Identity，国行没有；Google 又不给 Android 客户端用自定义 scheme 回调 | 💭 公网直连后 hub 有了可达的 HTTPS 地址：改用 Web 类型的 OAuth 客户端、回调到 hub（`/oauth/google/callback`），手机只负责用浏览器 / Custom Tabs 打开授权页；退路是应用专用密码走 IMAP。访问 Google 本身要不要代理是用户网络的事 | 💭 依据 `docs/mail.md` 现有流程（iOS 用反向 client ID 的自定义 scheme） |
| Custom Tabs | 需要一个支持 Custom Tabs 协议的浏览器；国产自带浏览器是否支持 ❓ | `CustomTabsIntent` 在不支持时会直接用默认浏览器打开，功能不受影响，只是不在 App 内 | 💭 |
| 崩溃上报 / 统计 | Firebase Crashlytics / Analytics 依赖 Google 的服务端，国内网络也不通 | 💭 不接 Firebase；崩溃日志（`ApplicationExitInfo` + 未捕获异常）下次启动时上传到 hub（以后 iOS 跟进时可共用同一个上报接口） | 💭 |
| WorkManager、CameraX、Biometric、Room、DataStore | 都是 AndroidX，不依赖 GMS | 照常用 | 💭（常识） |

**要换掉 / 去掉的清单**（💭）：
- 去掉（国行）：FCM 默认路径（改为可选）、Health Connect 工具、Google code scanner、可下载字体、Firebase 系列。
- 换掉：Fused Location → `LocationManager`；Authorization API → hub 回调的 Web OAuth；系统照片选择器在 31–32 上退到文件选择器（自动）。
- 保持但要降级处理：`Geocoder`（可能为空）、androidx.webkit 新能力（按 WebView 版本判断）、Custom Tabs（可能退到外部浏览器）。

## 7. 分发（没有 Play）

### 7.1 首次安装：各家的拦截

| 系统 | 障碍 | 来源 / 置信度 |
| --- | --- | --- |
| 所有 Android 8+ | 「安装未知应用」按来源 App 授权（浏览器、文件管理器……） | ✅ AOSP 标准行为 |
| 澎湃 OS | 「安全守护」：「限制安装第三方来源的未经安全检测应用」，关闭要验证身份（锁屏密码 / 小米账号 / 指纹）；安装器自带的安全检测（应用安装检查、安全监控）可在安装界面的设置里关；adb 安装要先在开发者选项开「USB 安装」（要登录小米账号），手机上的确认框几秒不点就自动拒绝（`INSTALL_FAILED_USER_RESTRICTED`） | 💭 小米官方安全守护页面靠 JS 渲染未读到，内容来自搜索摘要 https://app.market.xiaomi.com/hd/apm-h5-cdn/cdn-safe-guard.html ；用户经验 https://wap.zol.com.cn/ask/x_33926474.html ；✅ adb 部分是 `docs/android.md` 的真机记录 |
| 鸿蒙 4.x | 「纯净模式」（设置 › 系统和更新 › 纯净模式）：开「增强防护」时**只能装通过华为应用市场安全检测的 App**；关掉增强防护后只拦高危应用，普通 APK 要锁屏密码确认；另有「外部来源应用安全检测」开关。增强防护默认开不开，各文章说法不一 | 💭 第三方文章（引了疑似华为官方的提示文字）https://www.32r.com/zx/1201.html ，https://blog.csdn.net/NSJim/article/details/133850450 ；❓ 没读到华为官方页面 |
| 鸿蒙 5/6/7 | 只能经「卓易通」安装（下载 APK 后「用卓易通打开」）；部分设备默认开着纯净模式要先关 | 💭 见 2.1 |
| ColorOS / OriginOS / MagicOS | 都有安装前的风险扫描 / 提示；具体开关名和默认值 ❓ 没查到官方说法（网上大量同模板的「纯净模式横评」软文，互相矛盾，不可引用） | ❓ |

💭 对 Persona：首次安装是一次性的，可以接受。配对页 / 下载页写一份「按品牌的安装说明」，并说明为什么会提示风险（没上架应用商店）。

### 7.2 App 内自更新（hub 托管 APK）

**平台规则（✅ AOSP 源码，镜像约等于 Android 16；Android 16 / 17 的新条目源码里没有）**：

- `REQUEST_INSTALL_PACKAGES` + 用户给 Persona 打开「安装未知应用」（`Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES`，`canRequestPackageInstalls()` 可查）。
- `setRequireUserAction(USER_ACTION_NOT_REQUIRED)`（31+）在**同时满足**时不弹确认：
  - 被装的 App 的 target：Android 12 上 ≥ 29、13 上 ≥ 30、14 上 ≥ 31、15 上 ≥ 33（「The target API level requirement will advance in future Android versions」）；
  - 安装者是现有版本的 update owner（开了更新归属时）/ installer of record（没开时）/ **或者在更新它自己（「Updating itself」）**；
  - 安装者声明了 `UPDATE_PACKAGES_WITHOUT_USER_ACTION`。
  - 文档原话：「Session owners should always be prepared to handle STATUS_PENDING_USER_ACTION」。
- `setRequestUpdateOwnership(true)`（34+）：**只在首次安装时有效**，更新时设置是空操作，要 `ENFORCE_UPDATE_OWNERSHIP` 权限；作用是让「别的安装器」更新这个包时必须用户确认。**Persona 第一次是由浏览器 / 文件管理器装的，自己拿不到更新归属，这个 API 对自更新没用**（它是给应用商店用的）。
- 来源：https://raw.githubusercontent.com/aosp-mirror/platform_frameworks_base/main/core/java/android/content/pm/PackageInstaller.java （`SessionParams.setRequireUserAction` / `setRequestUpdateOwnership` 的注释）

**国产系统上的实际表现**：

- ✅（两个开源项目，2026-09 / 10）**澎湃 OS 上请求静默自更新会直接失败**：日志 `MIUILOG- assertCallerAndPackage ... Permission denied`，会话以 `INSTALL_FAILED_ABORTED` 结束，**不弹任何确认框**；原因是小米在 `PKMSImpl` 里加了自己的调用者检查，只放行白名单安装器。解决：识别到小米 / 红米 / POCO 时改用 `USER_ACTION_REQUIRED`，让系统弹确认框（F-Droid 等侧载更新器也是这么做的），并把「ABORTED + Permission denied」当成单独的错误，不要当成用户取消。
  来源：https://github.com/sofianeelhor/PKForge/issues/25 （2026-09-28，POCO X5 Pro，澎湃 OS 2.0.14，Android 14），https://github.com/HRYNdev/kelevra-box/pull/37 （2026-10-01，Redmi Note 14 Pro+，澎湃 OS，Android 16）
- ColorOS / OriginOS / MagicOS / 鸿蒙 4：❓ 没查到。保守做法是全部按「每次都要用户确认」设计，静默只作为原生系统上的优化。

**建议（💭）**：
1. hub 提供 `GET /api/v1/android/latest`（版本号、`versionCode`、APK 的 SHA-256、大小、更新说明）和 APK 下载（带设备 token 鉴权）。App 在前台时检查；下载后先校验 SHA-256，系统还会校验签名必须和已装版本一致。
2. 安装用 `PackageInstaller` 会话：默认 `USER_ACTION_REQUIRED`（弹系统确认），只在原生 / 已验证的系统上试 `NOT_REQUIRED`；处理 `STATUS_PENDING_USER_ACTION`（拉起系统确认页）和 `INSTALL_FAILED_ABORTED`。
3. 更新会杀掉进程：更新完成后常驻服务要靠 `MY_PACKAGE_REPLACED` 广播重新拉起（它也在后台起前台服务的豁免列表里；国产系统上能不能收到同样可能受「自启动」影响，❓ 要真机测）。
4. 不上国内应用商店：工信部 2023 年起要求 App 先备案再上架分发（个人也适用），自用工具走侧载更省事。来源：https://baike.baidu.com/item/%E5%B7%A5%E4%B8%9A%E5%92%8C%E4%BF%A1%E6%81%AF%E5%8C%96%E9%83%A8%E5%85%B3%E4%BA%8E%E5%BC%80%E5%B1%95%E7%A7%BB%E5%8A%A8%E4%BA%92%E8%81%94%E7%BD%91%E5%BA%94%E7%94%A8%E7%A8%8B%E5%BA%8F%E5%A4%87%E6%A1%88%E5%B7%A5%E4%BD%9C%E7%9A%84%E9%80%9A%E7%9F%A5/63291780

### 7.3 Google 2027「开发者验证」对国行机是否适用

- ✅ 官方：要求只在「certified Android devices」上执行；2026-09-30 起先在巴西、印尼、新加坡、泰国，2027 年扩展到全球；「It will apply to all certified Android devices running Android 7 or higher. These updates are delivered through Google Play services」；**adb 安装不受影响**（「The waiting period does not apply to ADB installs」）；未验证开发者的 App 走「高级流程」（开开发者模式、确认没被人指使、重启重新认证、**等 24 小时**后用生物识别确认，之后可选 7 天或永久允许）；有限分发账号（最多 20 台设备、不要证件和费用）2026-08 已上线。
  来源：https://developer.android.com/developer-verification ，https://developer.android.com/developer-verification/guides/faq
- 💭 判断：检查逻辑经 Google Play 服务下发、只在认证设备上跑；**国行机没有 GMS、也不是 Google 认证设备，基本不受影响**。FAQ 没有直接写「无 GMS / 中国」的情况，❓ 有说法称部分国行机隐藏了 GMS 组件（https://www.chinesephoneguy.com/blog/how-to-enable-google-play-on-chinese-phones ，来源弱），即便打开了也通常不是认证设备，要真机确认。
- 💭 对 Persona：给有 GMS 的海外版手机装时才需要注册（有限分发账号够用，重设计第 9 节已有）；国行用户不用管。

## 8. 测试：最小真机矩阵与云真机

### 8.1 最小真机矩阵（💭 判断）

依据：存量份额（QuestMobile 2026-06：华为 21.6%、OPPO 约 18%、vivo 16.5%、小米 10.1%、荣耀约 7.8%）、当前大版本（各家都是 Android 17，存量主力是 Android 16）、minSdk 31 的下限机型（鸿蒙 4，API 31）。

| # | 机型 | 系统 | 为什么要它 | 优先级 |
| --- | --- | --- | --- | --- |
| 1 | 小米 / 红米（已有：Redmi K60 Pro） | 澎湃 OS 3.x，Android 16 → 之后升澎湃 OS 4 / Android 17 | 后台最严的一家；自更新的坑；超级岛（3.1+ 才认标准 Live Updates） | P0（已有） |
| 2 | 华为 | **鸿蒙 4.2 / 4.3**（API 31） | minSdk 下限；本地通知静默 + 限频；「休眠时保持网络」；照片选择器走文件选择器；WebView 是华为浏览器内核 | P0 |
| 3 | OPPO / 一加 | ColorOS 16 或 17 | 份额第二（存量）；流体云官方兼容 Live Updates，可以验证 MetricStyle / 提升通知的样子 | P0 |
| 4 | vivo / iQOO | OriginOS 6 或 7 | 份额前三；首次打开的通知授权弹窗只弹一次；原子岛对标准接口的支持未知 | P1 |
| 5 | 荣耀 | MagicOS 10 或 11 | 智能省流量；应用启动管理；灵动胶囊 | P1 |
| 6 | 华为 | 鸿蒙 5 / 6（卓易通） | 决定「支持 / 不支持」：前台服务和通知在容器里能不能用 | P2（借用即可） |
| — | 模拟器 | Android 12、14、16、17（AOSP / Google API 镜像，不装 Play） | 原生行为基线、截图、`am compat` 开关；**模拟不了任何厂商策略** | 一直用 |

- 💭 顺序：先 2（华为 4.x）和 3（OPPO），它们和已有的小米覆盖了「最严的后台」「最麻烦的通知」「对标准接口最友好」三种极端；4、5 用云真机先过一遍跳转和通知设置页，有问题再买。
- 每台必跑（建议写进 `docs/testing.md`）：
  1. 首次安装（拦截提示）、首次打开（通知授权、准备清单每一项的跳转能不能到对的页面）；
  2. 锁屏 30 秒后的测试通知（响不响、亮不亮屏、锁屏上显不显示）；
  3. 锁屏不插电放 8 小时：hub 记录断开区间，手机记 `ApplicationExitInfo`，**至少 10 晚**；
  4. 每小时耗电（系统电池页 + `dumpsys batterystats`）；
  5. 划掉卡片 / 「活跃应用」里停止 / 重启手机后，服务能不能回来（`adb shell dumpsys package <包名> | grep stopped` 看是不是被强制停止了）；
  6. 自更新一次；
  7. 有活动时截一张状态栏 / 锁屏 / 岛的图（深浅色）；
  8. 照片选择、定位、WebView 版本、扫码。

### 8.2 云真机平台

| 平台 | 能做什么 | 限制 | 来源 / 置信度 |
| --- | --- | --- | --- |
| 小米 TestIt 云测 | 兼容性测试（安装、启动、卸载、Monkey、录屏、CPU / 内存，约 15 分钟）、远程真机调试（小米新机） | **每次最长 1 小时，每天免费 30 次**；预约后 10 分钟内要签到；APK ≤ 2G；测试机**没插 SIM 卡、没插 SD 卡、未 root**；文档没提远程 adb | ✅ https://dev.mi.com/xiaomihyperos/documentation/detail?pId=1523 （2026-04-14 更新，部分内容较旧） |
| 华为 AGC 云调试 | 华为真机（手机到鸿蒙电脑），实时日志、离线日志、Shell 命令 | 博客说 24 小时免费、可续时；有对比表说不支持远程 adb | ✅（官方页面只读到标题）https://developer.huawei.com/consumer/cn/agconnect/cloud-adjust/ ；💭 https://www.cnblogs.com/iofomo/p/18457996 （2024） |
| 荣耀云调试 | 荣耀真机 | 博客（2026 初）：免费 24 小时基础时长、不限次数续时 | 💭 https://blog.csdn.net/weixin_29218963/article/details/159326513 |
| OPPO 云真机 | OPPO / 一加 / 真我真机 | 博客：新用户 12 小时（2023）或 600 分钟免费（2024）；「流畅度一般，不稳定」 | 💭 https://blog.csdn.net/ch1451082329/article/details/132344580 （2023），https://www.cnblogs.com/iofomo/p/18457996 （2024） |
| vivo 云测 | vivo / iQOO 真机，支持日志 | 博客：新用户 6 小时（2023）；不支持远程 adb（2024） | 💭 同上两篇 |
| 腾讯 WeTest 云真机 | 上千台，主流机型免费公有云 | **远程 adb 只对付费用户**；约 1 元 / 分钟 | ✅ https://wetest.qq.com/product/cloudphone ；💭 价格来自博客 |
| Testin 云测 | 2500+ 机型，含鸿蒙 NEXT | 约 3 元 / 分钟，新用户 20 分钟免费 | 💭 https://cloud.tencent.com/developer/article/2586389 （2025 末） |

- 💭 **云真机测不了 Persona 最关心的东西**：会话最长 1 小时（小米），测不了过夜存活；机器通常一直插着电（插电时 Doze 不生效，耗电也没法测，❓ 各家没明说是否插电）；没有 SIM 卡，测不了流量下的后台限制（荣耀的智能省流量这类）；锁屏通知也不好看。
- 💭 **云真机适合测的**：安装拦截、首次打开的通知授权弹窗、准备清单里每个跳转能不能到对的页面（组件名在各版本上还在不在）、界面在各家字体 / 分辨率下的样子、照片选择器和 WebView 版本。这些正好是「组件名会烂掉」的部分，可以每个大版本跑一遍。

## 9. 没查清 / 需要实测

| # | 没查清的 | 怎么补 |
| --- | --- | --- |
| 1 | 国内统计平台（友盟+、极光、个推）2026 年的 Android 版本分布：没找到公开数据；StatCounter 是页面浏览量口径，且鸿蒙 3/4 计入「Android 12」 | 以真机矩阵和用户圈子的实际机型为准；hub 侧从 `client_hello` 统计自己的用户 |
| 2 | 鸿蒙 2 的兼容层是不是 API 29（只有第三方报道） | minSdk 31 时不影响 |
| 3 | 各家当前版本（澎湃 OS 3/4、ColorOS 16/17、OriginOS 6/7、MagicOS 10/11）新装 App 的横幅 / 锁屏默认值；小米「通知过滤」在澎湃 OS 上是否还是那套；vivo 首启弹窗和 Android 13 运行时权限怎么叠加 | 每台真机装一次新包截图 |
| 4 | ColorOS 15+、OriginOS 6+、MagicOS、`com.hihonor.systemmanager` 的自启动 / 耗电设置页组件名 | 云真机上 `adb shell dumpsys activity top` 记组件名（小米 TestIt、华为 / 荣耀云调试） |
| 5 | **前台服务进程会不会被各家冻结 / 断网**（澎湃 OS 的 Millet / GreezeManager、ColorOS 的冻结） | 真机：锁屏 8 小时、hub 记断开区间，≥ 10 晚 |
| 6 | 常驻连接在国产系统上的耗电（只有 ntfy 作者一台手机的「0–1% / 17 小时」） | A1 实测，给 min / p50 / p90 / max / mean |
| 7 | 华为鸿蒙 4.x 本地通知的「自分类权益」不上华为应用市场能不能申请；前台服务的常驻通知算不算进资讯营销配额；超额后是静默还是直接不显示 | 真机连续发通知看第几条开始静默；问华为开发者支持 |
| 8 | 国行版 OriginOS / MagicOS 的岛对标准 Live Updates 的支持；Android 17 `MetricStyle` 在任何一家上的样子 | 真机截图 |
| 9 | 国行上 `NETWORK_PROVIDER`、`Geocoder` 后端、33+ 系统照片选择器是否被厂商改掉 | 真机各跑一次 |
| 10 | 系统 WebView 由谁更新（厂商商店还是只跟 OTA，说法冲突）、各家当前 Chromium 版本 | 真机看 `WebViewCompat.getCurrentWebViewPackage()`；云真机批量看 |
| 11 | `USER_ACTION_NOT_REQUIRED` 自更新在 ColorOS / OriginOS / MagicOS / 鸿蒙 4 上的表现（澎湃 OS 已确认会失败） | 每台真机自更新一次 |
| 12 | 国行机打开隐藏的 GMS 后算不算 Google 认证设备（影响 2027 开发者验证） | 有这类机器时看 Play 商店「设备认证」 |
| 13 | 卓易通容器里前台服务、通知、常驻连接的实际表现 | 借一台鸿蒙 5/6 |
| 14 | 云真机是否一直插电（影响 Doze）、各家云真机 2026 年的免费额度（OPPO、vivo、荣耀、华为只有旧博客） | 实际登录各平台看 |
