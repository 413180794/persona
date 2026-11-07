# Persona Android: research on Chinese vendor ROMs and Android-version compatibility

- Research date: **2026-10-30** (all “queried” dates below mean this date unless specified otherwise).
- Markers: ✅ fact (source URL available); 💭 assessment / inference (no direct source; recheck before citing); ❓ not found.
- Premises (hard requirements requested 2026-10-30):
  1. Design for **phones without Google services (GMS / Play services)**, supporting mainstream OS versions.
  2. Phones connect directly to hub over the public internet (IPv6 preferred); users configure their own VPNs, outside the app's scope (covered by separate research; mentioned here only where relevant).
- Cite performance / power / market-share figures only with sources, stating sample and measurement basis; otherwise say none is available.

---

## 0. Findings at a glance

1. **minSdk 31** (💭): About 91% cumulative coverage in China StatCounter pageviews, about 78% in Google's global data. HarmonyOS 3/4's compatibility layer is API 31, so 31 includes Huawei devices that can still install APKs; 33 excludes them and all Android 12 phones (China coverage falls to 74%). API 31 introduces unlock-before-action for lock-screen notification buttons and one background foreground-service startup rule set. Fall back to 29 if users actually have Android 10/11 devices. (Section 1)
2. **Mainstream ROMs are already in the Android 17 generation** (HyperOS 4, ColorOS 17, OriginOS 7, MagicOS 11, released 2026-08~10), while Android 16 is the main installed generation. Huawei splits into HarmonyOS 4.x (API 31, APK-capable) and HarmonyOS 5/6/7 (only through the Droitong container; no first-release commitment). Installed-base shares: Huawei 21.6%, Apple 20.9%, OPPO about 18%, vivo 16.5%, Xiaomi 10.1%, Honor about 7.8% (QuestMobile 2026-06). (Section 2)
3. **No vendor offers a public API to query / enable autostart or power-saving allowlists**. Component names come entirely from the community and break across versions. The readiness checklist must query what it can and give OS-specific paths + self-tests for the rest. Easy-to-miss settings: Xiaomi floating / lock-screen notifications and autostart default off; HarmonyOS 4 **local notifications default to silent, rate-limited “Information and marketing”**, plus “Stay connected when device sleeps”; Honor's “Smart data saver” cuts background data; vivo shows its first-launch notification prompt only once. (Section 3)
4. **Persistent connection**: `specialUse` has no duration cap. In AOSP, processes with foreground services can still access the network and hold locks during Doze; Android 15/16/17 adds no new restrictions for it. Vendors are the risk: after a process kill, `START_STICKY` is not a background-start exemption (foreground-service startup throws); disabling battery optimization is an exemption. Freezing / network switches can leave the process alive but unable to receive. No public survival / battery measurements exist for Chinese ROMs; measure them ourselves. Background heartbeats should be sent by hub, spaced several minutes apart. (Section 4)
5. **Live Updates**: ColorOS 16+ officially claims compatibility; community tests show support on HyperOS 3.1+; China-market OriginOS and MagicOS are unconfirmed. Vendor-private island APIs all require allowlisting; avoid them. PR #84's persistent channel is `IMPORTANCE_MIN` and cannot be promoted under the rules; create a new channel. No information exists on MetricStyle behavior on Chinese ROMs. (Section 5)
6. **Remove / replace for no-GMS devices**: Health Connect (almost no data sources on China-market devices; recommend removing there), FCM (make optional), Google code scanner, downloadable fonts, Firebase, Fused Location (replace with `LocationManager`), Google Authorization API (replace with Web OAuth returning to hub). `Geocoder`, new WebView features, and the photo picker need fallbacks. (Section 6)
7. **Distribution**: sideloading + APK self-updates hosted by hub. **Silent self-updates (`USER_ACTION_NOT_REQUIRED`) fail immediately with `INSTALL_FAILED_ABORTED` on HyperOS**; use system confirmation instead. `setRequestUpdateOwnership` does not help self-updates. Developer verification in 2027 applies only to Google-certified devices; China-market phones are largely unaffected. (Section 7)
8. **Testing**: physical Xiaomi (already available) + Huawei HarmonyOS 4.x + OPPO are required; use cloud devices for vivo/Honor settings-navigation checks first. Cloud devices are useful only for installation, prompts, navigation, and UI, not overnight survival or battery measurements. (Section 8)

## 1. Android-version distribution and minSdk

### 1.1 Data (✅ facts, queried 2026-10-30)

These three datasets have different measurement bases; **none counts phone devices in mainland China**:

| Source | Measurement basis | Period |
| --- | --- | --- |
| Google (Android Studio distribution panel, reported by composables.com / ITHome) | Active devices that connected to the **Play Store** within 7 days; Google provides only cumulative shares, with individual-version shares approximately derived | As of 2026-10 (announced at Android Developer Summit 10-28/29; composables page marked updated 2026-10-01) |
| StatCounter worldwide | Unweighted **pageviews** on websites using StatCounter | 2026-09 only |
| StatCounter China | Same, region = China | 2026-09 only |

| Android (API) | Google global, individual version | Google global, cumulative | StatCounter global, individual version | StatCounter global, cumulative | StatCounter China, individual version | StatCounter China, cumulative (💭 summed from CSV) |
| --- | --- | --- | --- | --- | --- | --- |
| 17 (37) | 0.9–1.0% | 0.9% | 1.75% | 1.8% | 0.44% | 0.4% |
| 16 (36) | 25.4% | 26.3% | 37.65% | 39.4% | **41.65%** | 42.1% |
| 15 (35) | 14.7% | 41.0% | 15.93% | 55.3% | 13.10% | 55.2% |
| 14 (34) | 15.3% | 56.3% | 12.05% | 67.4% | 10.62% | 65.8% |
| 13 (33) | 11.8% | 68.1% | 11.58% | 79.0% | 8.01% | 73.8% |
| 12 / 12L (31/32) | 9.6% | 77.7% | 7.62% (+ 0.11% for 12.2) | 86.6% | **17.45%** | 91.3% |
| 11 (30) | 10.5% | 88.2% | 6.34% | 92.9% | 3.60% | 94.9% |
| 10 (29) | 5.7% | 93.9% | 3.28% | 96.2% | 1.37% | 96.2% |
| 9 (28) | 3.1% | 97.0% | 1.74% | 97.9% | 1.46% | 97.7% |
| 8.1 / 8.0 (27/26) | 1.5% / 0.5% | 98.5% / 99.0% | 0.85% / 0.28% | 98.8% / 99.1% | 0.70% / 0.21% | 98.4% / 98.6% |

- Google data: composables.com page (“All statistic data comes from Google's servers,” updated 2026-10-01) and ITHome's 2026-10-30 article (“active devices connected to Play Store within 7 days,” “Google officially publishes only cumulative shares; individual-version shares are approximations calculated from them”).
  Sources: https://composables.com/android-distribution-chart , https://www.ithome.com/1/019/973.htm
- StatCounter global / China: official CSV exports (2026-09). Cumulative shares calculated by apilevels.com from the same global data (data month 2026-09, page refreshed 2026-10-29) match the table.
  Sources: https://gs.statcounter.com/android-version-market-share/mobile-tablet/worldwide , https://gs.statcounter.com/android-version-market-share/mobile-tablet/china , https://apilevels.com/
- Chinese analytics platforms (Umeng+, Jiguang, Getui, TalkingData): ❓ **No public Android-version distribution for 2026 found**; searchable results are old “Umeng Index” data from 2014–2018.
- **China's “Android 12” share of 17.45% is conspicuously high (only 7.6% globally)**. ✅ HarmonyOS 3.x / 4.x's Android compatibility layer is Android 12 (API 31):
  UA includes `Android 12; HarmonyOS/4.0.3…`; WebView UA includes `APILevel/31 HarmonyOS/4.2.0`; developers measured `Build.VERSION.SDK_INT` = 31.
  💭 Much of China's 17% therefore likely represents Huawei HarmonyOS 3/4 phones. HarmonyOS 5/6/7 (unable to install APKs) does not appear in StatCounter.
  Sources: https://whatmyuseragent.com/platforms/har/harmonyos/4 , https://cloud.tencent.com/developer/article/2596538 , https://github.com/NEORUAA/MeiloX/issues/19 , https://www.huaweicentral.com/harmonyos-3-0-is-compatible-with-android-12/ , https://appy.to/blog/iphone-android-huawei-share-by-country
- HarmonyOS 2's compatibility layer is Android 10 (API 29): only third-party reporting (the emulator displays Android 10); ❓ no official statement found. Source: https://en.linuxadictos.com/in-the-end-harmonyos-turned-out-to-be-a-reworked-android-10.html

### 1.2 Capabilities lost and extra branches for each minSdk candidate

First, API levels relevant to Persona (✅ official documentation; sources at the end of this section):

| API | Capabilities Persona can use |
| --- | --- |
| 26 | Notification channels; `startForegroundService` |
| 28 | `FOREGROUND_SERVICE` permission; `ActivityManager.isBackgroundRestricted()` (detects user-imposed background restrictions) |
| 29 | `android:foregroundServiceType`; background clipboard reads prohibited (only foreground apps / default input method may read); `ACCESS_BACKGROUND_LOCATION` |
| 30 | `WindowInsetsAnimation` (keyboard-following animation); `LocationManager.getCurrentLocation`; `ApplicationExitInfo` (**reveals why a process was killed**, useful for diagnosing ROM background kills); system photo picker requires a Google System Update module (unavailable without GMS) |
| 31 | **Notification action `setAuthenticationRequired`** (unlock before lock-screen “Allow / Approve”); dynamic colors; `RenderEffect` blur; background foreground-service starts restricted (target 31+); foreground-service notifications delayed up to 10 seconds by default, override with `setForegroundServiceBehavior(IMMEDIATE)`; exact alarms require `SCHEDULE_EXACT_ALARM`; `PendingIntent` must declare mutability |
| 33 | Runtime `POST_NOTIFICATIONS` permission; built-in system photo picker (`MediaStore.ACTION_PICK_IMAGES`); asynchronous `Geocoder`; per-app language; AGSL |
| 34 | Foreground-service types mandatory (`specialUse` introduced); users can dismiss foreground-service notifications; partial-photo access; `PackageInstaller.setRequestUpdateOwnership`; Health Connect integrated into system framework; exact-alarm permission not granted by default |
| 35 | target 35: `BOOT_COMPLETED` cannot start dataSync / camera / mediaPlayback / phoneCall / mediaProjection / microphone foreground services (**not `specialUse`**); dataSync capped at 6 hours per 24 hours; mandatory edge-to-edge |
| 36 | Live Updates (`setRequestPromotedOngoing`, ProgressStyle); JobScheduler quotas also apply to apps with running foreground services |
| 37 | `MetricStyle`; target 37 requires local-network permission `ACCESS_LOCAL_NETWORK` (public-internet direct hub connections unaffected; separate research) |

Candidate trade-offs (coverage = “StatCounter China cumulative / Google global cumulative”; 💭 assessments marked):

| minSdk | Coverage | Extra compatibility branches / lost capabilities compared with 31 |
| --- | --- | --- |
| 26 (current PR #84) | 98.6% / 99.0% | All branches for 28, 29, 30, 31 below: lock-screen actions cannot require unlock (omit “Allow / Approve” or open the app first); no dynamic colors or blur; below 29 background clipboard reads are allowed (two behaviors); below 30 no `ApplicationExitInfo`, so kill reasons are unavailable; background foreground-service starts unrestricted below 31, restricted from 31—test both startup paths |
| 28 | 97.7% / 97.0% | Same, except a few Android 8.x branches (`FOREGROUND_SERVICE`, `isBackgroundRestricted`). 💭 Little benefit for Persona |
| 29 | 96.2% / 93.9% | Removes clipboard and foreground-service-type-attribute branches; still lacks capabilities from 30 and 31. 💭 Includes older Huawei HarmonyOS 2 devices (Android 10 compatibility layer; weak source) |
| 30 | 94.9% / 88.2% | Adds keyboard animation, `getCurrentLocation`, `ApplicationExitInfo`; still lacks 31's authenticated notification actions, dynamic colors, blur |
| **31** | **91.3% / 77.7%** | Baseline. **Includes HarmonyOS 3/4 (API 31)**; lock-screen “Allow / Approve” can require unlock; one background foreground-service startup rule set. Remaining branches: 33 (notification permission, photo picker, Geocoder), 34 (foreground-service type constants, dismissible FGS notifications, built-in Health Connect), 35 (BOOT_COMPLETED rules), 36 (Live Updates), 37 (MetricStyle, local-network permission) |
| 33 (original android-tech.md recommendation) | 73.8% / 68.1% | Fewer 31–32 branches, but **excludes Huawei HarmonyOS 3/4 phones (API 31) and every Android 12 phone**. 💭 Unsuitable for China |

**Recommendation (💭 assessment)**:

- **minSdk 31**. Reasons:
  1. About 91% of China pageviews, including HarmonyOS 3/4 phones that can still install APKs (most Huawei phones; see 2.1).
  2. Lock-screen “Allow / Approve” actions can require unlock, a security prerequisite for direct approvals from a persistent notification.
  3. One background foreground-service startup rule set (31+), avoiding two paths in the most failure-prone part of persistent connections.
  - Devices below 31 (about 9% in China) are mostly models from around 2020. If users actually have Android 10/11 phones, lower to **29**: omit lock-screen “Allow / Approve” (or open the app first), lose dynamic colors and blur, and test an extra foreground-service startup rule set.
- Do not recommend 33: China coverage falls to 74%, specifically excluding HarmonyOS 4.
- Definition of “mainstream” (💭): above 90% of China pageviews, including devices eligible for each Chinese ROM's latest three major versions. Current versions are based on Android 17, previous versions on Android 16 (Section 2), and the generation before that on Android 15 (not separately sourced in this research), all above 31; HarmonyOS 4.x is exactly 31.

Sources (API levels in 1.2):
- Authenticated notification actions (API 31): https://developer.android.com/reference/android/app/Notification.Action.Builder
- Dynamic colors (Android 12+): https://developer.android.com/develop/ui/views/theming/dynamic-colors ; `RenderEffect` (API 31): https://developer.android.com/reference/android/graphics/RenderEffect
- Foreground-service types (mandatory from 34; `specialUse` has no runtime prerequisites or duration cap): https://developer.android.com/develop/background-work/services/fgs/service-types
- Background foreground-service-start restrictions and 14 exemptions (31+): https://developer.android.com/develop/background-work/services/fgs/restrictions-bg-start
- Foreground-service notification delay up to 10 seconds (12+): https://developer.android.com/guide/components/foreground-services#notification-immediate ; `ApplicationExitInfo` (30+), stopping through task manager: https://developer.android.com/develop/background-work/services/fgs/handle-user-stopping
- Package visibility (filters `resolveActivity` from target 30): https://developer.android.com/training/package-visibility/automatic
- Android 15 `BOOT_COMPLETED` restrictions, 6-hour dataSync cap: https://developer.android.com/about/versions/15/behavior-changes-15
- Android 16 JobScheduler quotas: https://developer.android.com/about/versions/16/behavior-changes-all
- Android 17: https://developer.android.com/about/versions/17/behavior-changes-17 , https://developer.android.com/about/versions/17/behavior-changes-all
- Photo-picker availability: https://developer.android.com/training/data-storage/shared/photopicker ; AndroidX selection logic (33+ always uses system picker; 30–32 checks SDK extension version ≥ 2, otherwise looks for a system app providing `androidx.activity.result.contract.action.PICK_IMAGES`, then falls back to `ACTION_OPEN_DOCUMENT`): https://raw.githubusercontent.com/androidx/androidx/androidx-main/activity/activity/src/main/java/androidx/activity/result/contract/ActivityResultContracts.kt

## 2. Mainstream Chinese ROMs, market shares, and Android bases

### 2.1 Shares (✅ facts, queried 2026-10-30)

**Sold devices (quarterly shipments / sales)**

| Brand (OS) | Counterpoint 2026Q3 shipment share | Omdia 2026Q3 shipments | IDC 2026Q3 |
| --- | --- | --- | --- |
| Huawei (HarmonyOS) | 21.3% | 12.4 million (about 21%) | 21.1%, first |
| Apple | 19.5% | 11.6 million | 17.8%, second |
| vivo / iQOO (OriginOS) | 15.2% | 9 million | Third through fifth (OPPO, vivo, Xiaomi), exact shares not found |
| OPPO / OnePlus / realme (ColorOS) | 15.1% (including OnePlus, realme) | 9 million | Same |
| Xiaomi / Redmi / POCO (HyperOS) | 13.5% | 8 million | Same |
| Honor (MagicOS) | 12.7% | Not found | Only leading vendor with quarter-on-quarter growth |
| Total | — | 59.3 million (-12% YoY) | About 58.06 million (-15.2% YoY) |

- OS shares (Counterpoint 2026Q2 China sales): Android 58%, **HarmonyOS 24%** (record high, versus 19% in Q1), iOS 18%.
- Sources: Counterpoint (reported by TelecomLead 2026-10-25) https://telecomlead.com/smart-phone/china-smartphone-market-q3-2026-huawei-and-apple-gain-share-as-xiaomi-vivo-oppo-and-honor-decline-128351 ;
  Omdia 2026-10-14 https://www.lightreading.com/smartphones-devices/mainland-china-smartphone-shipments-drop-to-lowest-q3-level-since-2013-omdia ;
  IDC (reported by Sina 2026-10-14 / 10-27) https://finance.sina.com.cn/tech/digi/2026-10-14/doc-inivcxxt8542914.shtml , https://finance.sina.com.cn/tech/mobile/n/n/2026-10-27/doc-iniwsfqw3802453.shtml ;
  Counterpoint OS shares (reported by multiple outlets) https://gadgets.beebom.com/news/android-ios-harmonyos-how-mobile-os-share-ranks-in-2026 , https://www.androidheadlines.com/2026/08/android-vs-ios-vs-harmonyos-heres-how-each-os-ranks-globally-and-in-3-major-markets.html

**In-use devices (installed base, closer to what users actually own)**: QuestMobile's *2026 Smartphone Market Research Report*, data through 2026-06, with 1.282 billion active mobile-internet devices:

| Brand | Active devices | Share |
| --- | --- | --- |
| Huawei | 276 million | 21.6% (-4.0% YoY) |
| Apple | 268 million | 20.9% |
| OPPO (2025 definition: excludes realme and OnePlus) | 233 million | About 18.2% (💭 calculated from 1.282 billion) |
| vivo (including iQOO) | 212 million | 16.5% (+9.5% YoY) |
| Xiaomi (including Redmi) | 129 million | 10.1% |
| Honor | 100 million | About 7.8% (💭 calculated) |

Sources: https://www.tmtpost.com/8132127.html , https://www.thepaper.cn/newsDetail_forward_34028314

**How many Huawei devices can install APKs?**

- ✅ HarmonyOS 5/6/7 (HarmonyOS NEXT family, **cannot install APKs directly**): Huawei announced on 2026-10-01 that “HarmonyOS 6 | HarmonyOS 7 devices exceeded 90 million” (device count includes more than phones; changed to the combined two-version total after HarmonyOS 7 launched). IDC's 2026Q3 report says “native HarmonyOS installations exceeded 95 million.”
  Sources: https://finance.sina.com.cn/tech/roll/2026-08-21/doc-ininysma7899246.shtml (80 million, 8-20), https://www.ithome.com/0/971/420.htm (70 million, 7-02), timeline summary https://www.ithome.com/1/009/012.htm ; IDC above.
- 💭 Rough estimate: 276 million active Huawei devices (June), native HarmonyOS ≤ 95 million including tablets / PCs, so **at least roughly two-thirds of Huawei phones still run HarmonyOS 4.x or earlier** (APK-capable, compatibility-layer API 31). This proportion is falling quickly (about 5 million new native HarmonyOS devices each month) and will reverse in one or two years.
- ✅ HarmonyOS 5/6 can run APKs through Huawei's “Droitong” compatibility container (based on iSulad): download an APK online, then install with “Open with Droitong.” Apps already offering native HarmonyOS versions are directed to those versions; some devices have “Pure mode” enabled by default and must disable it first.
  To receive notifications, enable them in “Droitong › Me › App management › App › Notification management,” and **the app must remain running in the background** (once its card disappears from recents, notifications stop). Droitong itself may be killed under resource pressure; background allowlisting is recommended.
  ❓ Whether foreground services and persistent connections work inside Droitong **was not found**; physical-device testing is required.
  Sources: https://blog.soarli.top/archives/996.html (2026-04-27), https://eu.36kr.com/zh/p/3522460176735107 , official Droitong FAQ (JS-rendered page; WebFetch could not read the body, content came from search snippets) https://www.droitong.com/CommonQues.html , https://blog.csdn.net/weixin_30899023/article/details/166670175

### 2.2 Current versions and Android baselines (✅ facts)

| Vendor | OS | Current major version (2026-10) | Based on | Previous generation (most installed devices) |
| --- | --- | --- | --- | --- |
| Xiaomi / Redmi / POCO | HyperOS | **HyperOS 4**: released 2026-08-13; first stable releases from 10-15 (Xiaomi 17, K90, Pad 8), Xiaomi 14 / K70 etc. by year end | Android 17 | HyperOS 3 / 3.1: Android 16 (rollout from 2025-10 through 2026-03) |
| OPPO / OnePlus / realme | ColorOS (OxygenOS for OnePlus internationally) | **ColorOS 17**: released 2026-09-17, China rollout from 10-08, older models scheduled through 2027Q1 | Android 17 | ColorOS 16: Android 16 (from 2025-10) |
| vivo / iQOO | OriginOS (international devices also switched in 2025; iQOO no longer uses Funtouch) | **OriginOS 7**: released 2026-09-16, China upgrades from October | Android 17 | OriginOS 6: Android 16 |
| Honor | MagicOS | **MagicOS 11**: released 2026-09-15, debut on Magic9 | Android 17 | MagicOS 10: Android 16 |
| Huawei | HarmonyOS | HarmonyOS 6 / 7 (cannot install APKs; see above) | Not Android | HarmonyOS 4.2 / 4.3: Android compatibility layer = Android 12 (API 31) |

- 💭 Conclusion: **Each vendor's current major release this year is Android 17, while Android 16 dominates the installed base** (China StatCounter: 16 at 42%). Persona physical-device validation must cover both Android 16 and 17 ROM generations, plus one HarmonyOS 4.x device.
- Sources:
  - HyperOS 4: https://www.sohu.com/a/1064544461_115831 , https://baike.baidu.com/item/%E5%B0%8F%E7%B1%B3%E6%BE%8E%E6%B9%83OS%204/67391378 ; HyperOS 3: https://9to5google.com/2025/09/26/xiaomi-android-16-list-schedule/
  - ColorOS 17: https://www.news.cn/photo/20260917/6e4a5b6e027341059efe0d7af0d78d19/c.html , https://finance.sina.com.cn/tech/roll/2026-10-14/doc-inivcxxn0419857.shtml ; ColorOS 16: https://en.wikipedia.org/wiki/ColorOS
  - OriginOS 7: https://www.androidheadlines.com/2026/09/vivo-just-announced-global-originos-7-based-on-android-17-heres-whats-new.html , https://www.gizmochina.com/2026/09/19/originos-7-update-timeline-for-vivo-and-iqoo-devices/ ; OriginOS 6: https://www.91mobiles.com/hub/android-16-release-date-eligible-phones/
  - MagicOS 11: https://www.ithome.com/0/999/493.htm , https://phone.cnmo.com/news/820473.html (MagicOS 10 based on Android 16: ❓ not separately sourced in this research; a general-knowledge assessment)

## 3. ROM restrictions affecting Persona and navigation to settings

> Conclusion first (💭): **No Chinese ROM provides third-party apps a public API to query / enable autostart or power-saving allowlists**. Vendor settings component names are reverse-engineered by the community and change across versions; official documentation gives only user navigation paths or stops at MIUI / EMUI.
> Persona can therefore only query available APIs (notifications, channels, battery optimization, background restrictions), and otherwise provide an “Open settings” button + text instructions + self-tests (send a test notification after locking, record connection downtime on hub). Record each vendor/version's actual behavior in `docs/android/android.md`, “Lessons.”

### 3.1 Common to all ROMs: queries and navigation

| Item | Queryable through API? | Standard navigation (AOSP) | Source |
| --- | --- | --- | --- |
| Overall notification toggle | ✅ `NotificationManagerCompat.areNotificationsEnabled()`; request `POST_NOTIFICATIONS` first on 33+ | `Settings.ACTION_APP_NOTIFICATION_SETTINGS` + `EXTRA_APP_PACKAGE` | XXPermissions `NotificationServicePermission` (also below) |
| Individual channel | ✅ `getNotificationChannel(id).importance != IMPORTANCE_NONE` | `Settings.ACTION_CHANNEL_NOTIFICATION_SETTINGS` + `EXTRA_CHANNEL_ID` (works only while channel exists and overall notifications are enabled) | Same |
| Floating / banner, lock-screen notifications (vendor-specific toggles) | ❌ Xiaomi officially: “There is currently no API to query whether the permission is enabled” | Only the app's notification settings page | Xiaomi developer FAQ (quoted) https://dev.mi.com/docs/appsmarket/technical_docs/adaptation_FAQ/ |
| Battery optimization (Doze allowlist) | ✅ `PowerManager.isIgnoringBatteryOptimizations()` | `ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS` + `package:` URI (declare `REQUEST_IGNORE_BATTERY_OPTIMIZATIONS`; no Play policy issue when not on Play); fallback `ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS` | https://developer.android.com/training/monitoring-device-state/doze-standby |
| App battery page (Unrestricted / Optimized / Restricted) | Partly: `ActivityManager.isBackgroundRestricted()` (28+; Xiaomi docs say true when restricted), `UsageStatsManager.appStandbyBucket` (28+) | `android.settings.VIEW_ADVANCED_POWER_USAGE_DETAIL` + `package:` URI: XXPermissions testing shows it works only on **12+** | XXPermissions `RequestIgnoreBatteryOptimizationsPermission`; Xiaomi https://dev.mi.com/xiaomihyperos/documentation/detail?pId=1628 |
| Autostart | ❌ No public API (MIUI has third-party `XomaDev/MIUI-autostart` using hidden APIs, tested only through MIUI 14) | Vendor component names only (3.2) | https://github.com/XomaDev/MIUI-autostart |
| Lock in recents | ❌ | None; text instructions only | — |
| Kill reason (postmortem diagnosis) | ✅ `ActivityManager.getHistoricalProcessExitReasons()` (30+): `REASON_USER_REQUESTED` (stopped in task manager / settings), `REASON_LOW_MEMORY`, `REASON_OTHER`, etc. | — | https://developer.android.com/develop/background-work/services/fgs/handle-user-stopping |
| Identify ROM | ✅ Read system properties (below) | — | DeviceCompat source |

- **Opening another app's page does not require package visibility** (✅ official: “You can start another app's activity using either an implicit or explicit intent regardless of whether that app is visible to your app”). But `resolveActivity()` is visibility-filtered on target 30+, so a failed lookup does not mean the activity is absent.
  Call `startActivity` directly and catch `ActivityNotFoundException` (possibly `SecurityException` for unexported components), trying candidates in order and finally falling back to `ACTION_APPLICATION_DETAILS_SETTINGS`.
  Declare `<package>` in manifest `<queries>` only if deciding beforehand whether to show the button.
  Sources: https://developer.android.com/training/package-visibility/automatic , https://developer.android.com/training/package-visibility
- **ROM identification** (✅ properties in DeviceCompat 2.6 source, updated 2026-02): HyperOS `ro.mi.os.version.name` / `ro.mi.os.version.code` (version `ro.mi.os.version.incremental`); MIUI `ro.miui.ui.version.name`;
  ColorOS `ro.build.version.oplusrom` (old `ro.build.version.opporom`); OriginOS / Funtouch `ro.vivo.os.build.display.id`; MagicOS `msc.config.magic.version` / `ro.build.version.magic`;
  HarmonyOS 2–4 `ro.build.ohos.devicetype` / `hw_sc.build.platform.version`; **HarmonyOS NEXT's Android-compatible environments (Droitong / Chujingyi)** `ro.product.anco.devicetype` / `ro.sys.anco.product.software.version`.
  `Build.MANUFACTURER` alone is insufficient (a Xiaomi phone may run another flashed ROM). Source: https://github.com/getActivity/DeviceCompat (`library/src/main/java/com/hjq/device/compat/DeviceOs.java`)
- 💭 Include ROM name and version in `client_hello` (hub aggregation can reveal which models have broken settings links). The codewithkarani article suggests the same approach (“the only realistic way to find out that a component name has gone stale”). Source: https://www.codewithkarani.com/blog/oem-autostart-battery-optimisation-android-deep-links (2026-07-29)

### 3.2 Individual ROMs

#### Xiaomi / Redmi / POCO: HyperOS

| Item | Current behavior | Source / confidence |
| --- | --- | --- |
| Notifications for newly installed apps | Android 13+ disables notifications for new apps by default; request runtime permission (Xiaomi's official compatibility guide). MIUI-specific toggles (floating, lock-screen, badges, sound, breathing light) **all default off, except “Allow vibration”**. Floating also requires `IMPORTANCE_HIGH` and no Do Not Disturb. **Matches PR #84's physical HyperOS 3 (Android 16) results**: highest channel importance, but no popup from the notification bar by default | ✅ https://dev.mi.com/xiaomihyperos/documentation/detail?pId=1575 ; ✅ (third-party article from MIUI era, no version) https://www.jianshu.com/p/9df4101b83d2 ; ✅ physical-device record in `docs/android/android.md` |
| Notification filtering (important / unimportant) | Scores factors such as the app's historical notification click rate on this device, collapsing low scores into “Unimportant notifications”; **a new app's first 3 notifications are important by default**. Users can swipe left to “Mark as important,” or disable “Notification filtering” under “Notifications & status bar.” China-market devices only | ✅ https://dev.mi.com/xiaomihyperos/documentation/detail?pId=1577 (updated 2024-10-09, describes MIUI 9/10; ❓ whether HyperOS still uses this was not found) |
| Autostart | **Disallowed by default**; user must enable manually | ✅ https://dev.mi.com/xiaomihyperos/documentation/detail?pId=1624 (2024-09-25) |
| Power-saving policy | App details › Battery saver (MIUI) / Battery usage (HyperOS) › **Unrestricted**. Powerkeeper releases locks and kills processes for prolonged background locks, excessive short-term background power usage, long background inactivity, or unauthorized sensor use. “Unrestricted” removes limits; `isBackgroundRestricted()` returns true when restricted | ✅ https://dev.mi.com/xiaomihyperos/documentation/detail?pId=1628 (2022-12-07); XXPermissions comments (HyperOS details page calls it “Battery usage”) |
| System-level process killing | MIUI process management has AutoPowerKill (excessive power), AutoThermalKill (heat); after users swipe away background tasks, official advice is to guide them to enable autostart in Security | ✅ https://dev.mi.com/xiaomihyperos/documentation/detail?pId=1607 |
| Freezing | A developer found GreezeManager on Xiaomi 14 Ultra (HyperOS) freezing an accessibility service via cgroup after about 1 second without events. Wakelocks are forcibly invalidated while frozen; background allowlisting does not help. **This is an accessibility service, not a foreground service**; ❓ whether foreground services are frozen was not found | ✅ (individual developer report) https://github.com/madeye/ad-skipper/releases/tag/v1.1 |
| Lock in recents | Pull the card down (or long-press and tap the lock) | ✅ https://dontkillmyapp.com/xiaomi |
| Lock-screen cleanup | Security › Battery › top-right Settings › “Clear memory after locking” (1 / 5 / 10 / 30 minutes, etc.; one article says “Never” by default). Blogger tests say WeChat, calls, and alarms are retained. Persona is not on that list and needs locking / Unrestricted | 💭 Blogger (HyperOS 3) https://www.toutiao.com/article/7639927547056112191/ ; ❓ no reliable sources found for other vendors' lock-screen cleanup switches |
| `ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS` | MIUI / HyperOS on Android 11+ replaces the system dialog with Xiaomi's own page. Wait about 1 second after returning before checking (200ms suffices on HyperOS 2 + Android 15) | ✅ XXPermissions source comments https://github.com/getActivity/XXPermissions |
| Known navigation targets | Autostart: `com.miui.securitycenter/com.miui.permcenter.autostart.AutoStartManagementActivity` (used by PR #84); app permissions: action `miui.intent.action.APP_PERM_EDITOR` + extra `extra_pkgname`; Security home: action `miui.intent.action.SECURITY_CENTER`; old battery policy: `com.miui.powerkeeper/.ui.HiddenAppsConfigActivity` + extras `package_name` / `package_label` (MIUI 9–12 blog posts; HyperOS ❓) | ✅ https://github.com/judemanutd/AutoStarter ; XXPermissions `PermissionSettingPage`; old CSDN article https://blog.csdn.net/u010098889/article/details/72823013 |
| Clipboard reads | Xiaomi manages them as an AppOps permission; store review requires foreground-only triggers | ✅ https://dev.mi.com/xiaomihyperos/documentation/detail?pId=2225 (2025-12-30) |

#### OPPO / OnePlus / realme: ColorOS

| Item | Current behavior | Source / confidence |
| --- | --- | --- |
| Notifications for newly installed apps | ColorOS 13+ uses Android 13's runtime notification permission (official OPPO compatibility docs). Earlier ColorOS 3.0 (around 2018) blocked third-party notifications by default, requiring manual enablement. **❓ No official statement found on current banner / lock-screen defaults** | ✅ https://open.oppomobile.com/new/developmentDoc/info?id=11311 ; old: https://blog.csdn.net/u012571415/article/details/79283390 |
| Autostart / background | ColorOS 12+: Settings › Apps › Autostart; per app: App details › **Battery usage management**: Allow foreground wake-up / **Allow full background activity** / Allow app autostart / Allow associated launch. Community explanation: “Allow full background activity” ≈ stock “Unrestricted.” Another claim: ColorOS has its own battery-optimization allowlist and resets changes to stock battery optimization | 💭 Community (V2EX 2023/2024, ColorOS 13/14) https://v2ex.com/t/952213 , https://s.v2ex.com/t/1037649 ; ColorOS 15 still has Autostart: https://zeeko.dev/2025/03/android-user-root-upgrade-coloros-15/ |
| `ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS` | Opaque page on ColorOS 15, transparent dialog on 14 / 16 Beta | ✅ XXPermissions source comments (per-device test records) |
| Improved app-details navigation | Include `permissionList` (blocked permission names) + `isGetPermission=true` with `ACTION_APPLICATION_DETAILS_SETTINGS`; ColorOS jumps directly to the permission (runtime permissions only) | ✅ XXPermissions source, citing OPPO docs https://open.oppomobile.com/new/developmentDoc/info?id=12983 |
| Known navigation targets | `com.coloros.safecenter/.permission.startup.StartupAppListActivity`, `com.coloros.safecenter/.startupapp.StartupAppListActivity`, `com.oppo.safe/.permission.startup.StartupAppListActivity` (all community records for ColorOS 11 or earlier); Security package names also include `com.oplus.safecenter`, `com.color.safecenter` | ✅ AutoStarter; XXPermissions. ❓ No component names found for ColorOS 15/16/17 |
| Lock in recents | Lock the app in recents | 💭 Community |
| Clipboard reads | ColorOS 12+ prompts on clipboard reads; ColorOS 14 can set access per app under “Permissions & privacy › Clipboard management” | 💭 Community https://bbs.oneplus.com/thread/6223871 |

#### vivo / iQOO: OriginOS (Funtouch retired)

| Item | Current behavior | Source / confidence |
| --- | --- | --- |
| Notifications for newly installed apps | Models released after 2021-08: the system prompts **once on a new app's first launch**. “Allow” enables app notifications, lock-screen notifications, floating notifications, and launcher badges together; “Deny” or exiting disables all. **Only one prompt** (reinstallation resets it). Floating notifications depend on both the toggle and channel priority; lock-screen notifications depend on the app's lock-screen toggle. ❓ Interaction with Android 13 runtime permission was not found | ✅ (third-party reposts of vivo's *Notification Authorization Dialog* and *OriginOS Notification Style Specification*, 2022; original https://dev.vivo.com.cn/documentCenter/doc/577 is JS-rendered and unreadable) https://www.yimenapp.com/kb-yimen/12014/ , https://www.yimenapp.com/kb-yimen/11646/ |
| Autostart | Settings › Apps & permissions › Permission management › Autostart (community OriginOS 6 guide; iManager on older systems) | 💭 Community https://faq.coros.com/help/641aa9e94187afd985fc53e6/6421412205be2e4057ad100b?locale=zh-CN |
| Background | Settings › Battery › Background power usage management › App › **Allow high background power usage** | 💭 Community; Huawei's help for vivo users gives the same path https://consumer.huawei.com/cn/support/content/zh-cn15977902/ |
| Known navigation targets | `com.vivo.permissionmanager/.activity.BgStartUpManagerActivity`, `com.iqoo.secure/.ui.phoneoptimize.AddWhiteListActivity`, `com.iqoo.secure/.ui.phoneoptimize.BgStartUpManager` | ✅ AutoStarter (old); ❓ whether they remain in OriginOS 6/7 was not found |
| Lock in recents | Card menu in recents › Lock | ✅ https://dontkillmyapp.com/vivo |

#### Honor: MagicOS

| Item | Current behavior | Source / confidence |
| --- | --- | --- |
| Notifications for newly installed apps | ❓ MagicOS defaults not found | — |
| App launch management | Settings › Apps › **App launch management** › disable “Manage automatically” for the app › enable Allow autostart, Allow associated launch, Allow background activity (MagicOS 7.0+); Battery optimization › Don't allow; pull multitasking card down to lock | ✅ Honor official https://www.honor.com/cn/support/content/zh-cn00428704/ ; https://consumer.huawei.com/cn/support/content/zh-cn15967050/ |
| **Smart data saver** | “Prevents apps from using mobile data in the background”: Settings › Mobile network › Data usage › disable it. **Fatal to Persona's persistent connection** (background network is cut on mobile data) | ✅ Honor official, same as above |
| Other | Use Balanced / Performance battery mode; disable power-saving / low-battery modes; System Manager › Extended services › enable “Smart maintenance” | ✅ Same |
| Known navigation targets | Pre-split Honor devices reuse `com.huawei.systemmanager/...`; newer devices use System Manager package `com.hihonor.systemmanager`, but ❓ no reliable source found for component names | ✅ (package name, weak source) https://www.volcengine.com/article/174693 |

#### Huawei: HarmonyOS 4.x and earlier (APK-capable)

| Item | Current behavior | Source / confidence |
| --- | --- | --- |
| **Local notifications classified and rate-limited** | Since 2023-09-15, Huawei has gradually controlled notifications apps send through system APIs under its *Message Classification Standard*. **Apps without “Self-classification entitlement” have local notifications treated as “Information and marketing”**: silent (notification shade only, no banner, lock screen, or sound), sharing the cloud-push marketing quota of 2 or 5 per day; exceeding it may prevent display entirely. Apply in AppGallery Connect; once granted, set type with `Notification.category` (RongCloud's example uses `CATEGORY_MESSAGE`). ❓ **Whether apps outside Huawei AppGallery may apply was not found** | ✅ Huawei announcement quoted by RongCloud and Tencent Cloud https://help.rongcloud.cn/t/topic/1085 , https://cloud.tencent.com/document/product/548/97927 ; Huawei original https://developer.huawei.com/consumer/cn/doc/HMSCore-Guides/message-classification-0000001149358835 (JS-rendered, not directly readable) |
| User workaround | Settings › App management › Persona › Notification management: enable notifications; enable sound, banners, lock screen, and interruption per category; **disable “Silent notifications”** | ✅ Huawei official https://consumer.huawei.com/cn/support/content/zh-cn15872069/ |
| App launch management | Settings › App launch management › disable Manage automatically › enable Allow autostart, Allow associated launch, Allow background activity | ✅ Same (HarmonyOS 4.x and earlier) |
| Battery | Battery optimization › All apps › Don't allow; **Battery › More battery settings › enable “Stay connected when device sleeps”** (critical for persistent connections); disable Smart data saver | ✅ Same |
| Known navigation targets | `com.huawei.systemmanager/.startupmgr.ui.StartupNormalAppListActivity` (Android 9+ EMUI, several sources agree); `.appcontrol.activity.StartupAppControlActivity` (Android 8); `.optimize.process.ProtectActivity` (older); `.optimize.bootstart.BootStartActivity` (EMUI 5) | ✅ https://gist.github.com/ru-huawei-android/7296148b24e0079c40073efc34f628a4 , AutoStarter; P-Pass verified the three App launch management toggles on a physical HarmonyOS 4.2 device https://github.com/hawkeye-xb/P-Pass/pull/1086 (2026-10-21) |
| Lock in recents | Pull the multitasking card down to lock | ✅ Same |

#### Huawei: HarmonyOS 5 / 6 / 7 (Droitong container only)

See 2.1: users must install Droitong, with the app running in a container. Notifications must be enabled separately in Droitong, and the app must remain alive in the background. DeviceCompat recognizes this environment (`isHarmonyOsNextAndroidCompatible`). 💭 **No support commitment for the first release**. When detected, the pairing page should explain “Allow notifications in Droitong and keep the app running in the background; some features may not work fully.”

### 3.3 Recommendations for Persona (💭)

1. **Two readiness-checklist categories**:
   - Automatically queryable (overall notifications, `input` channel importance, battery optimization, `isBackgroundRestricted`): show real status and recheck on return to the app (`onResume`).
   - Not queryable (floating / lock-screen notifications, autostart, Unrestricted / Allow full background activity / Allow high background power usage, recents locking, Huawei silent notifications and sleep-time connectivity, Honor Smart data saver): provide text paths **for that vendor and version** + “Open settings,” let users mark “Done,” then backstop with self-tests:
     - Send a test notification after 30 seconds locked and ask “Did it sound or wake the screen?”
     - hub records this device's WS-disconnection intervals (already known on hub); settings shows “N background disconnections in the last 24 hours; longest X minutes.”
2. **Group navigation candidates by ROM and define them as data**. Try in order, falling back to app details. Log each attempt locally (successful component or failure exception), and include ROM name/version in `client_hello`.
3. **Notification-channel importance cannot change after creation (only the user can change it)**: PR #84's `后台连接` (“Background connection”) channel is `IMPORTANCE_MIN` and **cannot be promoted to a Live Update** (Section 5). Create a new channel ID during reorganization.
4. **Huawei HarmonyOS 4.x has the hardest notification behavior**: silent + rate-limited. The first release can rely only on users disabling Silent notifications and enabling banners and lock-screen display. “Needs you” alerts also rely on the persistent notification's count (foreground-service notifications may be exempt from the information/marketing quota; ❓ physical-device testing required).

## 4. Persistent connection through a foreground service

### 4.1 Platform rules (✅ facts)

| Rule | Behavior | Meaning for Persona |
| --- | --- | --- |
| `specialUse` type | Types mandatory from 34; `specialUse` requires `FOREGROUND_SERVICE_SPECIAL_USE`, with **no runtime prerequisites or duration cap** (only `shortService` at about 3 minutes and `dataSync` / `mediaProcessing` at 6 hours per 24 hours have caps); `PROPERTY_SPECIAL_USE_FGS_SUBTYPE` matters only for Play review | PR #84's choice is correct; no review issue when not on Play |
| Background foreground-service start (target 31+) | Prohibited unless one of 14 exemptions applies, otherwise `ForegroundServiceStartNotAllowedException`. Relevant exemptions: transition from visible state, **user taps a notification / widget**, **receiving `BOOT_COMPLETED` / `LOCKED_BOOT_COMPLETED` / `MY_PACKAGE_REPLACED`**, **user disables battery optimization for the app** | Can start after boot and upgrade; **battery-optimization allowlisting is not only for Doze, but also permits foreground-service starts from the background at any time** |
| `START_STICKY` | Official exemptions do **not** include system restarts of sticky services. Community tests: after a kill, the system restarts the service in the background; `startForeground` in `onStartCommand` throws `ForegroundServiceStartNotAllowedException` (`mAllowStartForeground false`) unless another exemption happens to apply (such as disabled battery optimization) | Restart intent is null; wrap `startForeground` in try/catch and call `stopSelf()` on failure, waiting for the next exempt opportunity (user opens app, taps notification, boot, app update); network changes are not exempt |
| `BOOT_COMPLETED` (target 35+) | Cannot start dataSync, camera, mediaPlayback, phoneCall, mediaProjection, microphone foreground services from boot broadcasts; **`specialUse` is not listed** | PR #84's `BootReceiver` still works at target 35/36/37. Receiving boot broadcasts on Chinese ROMs depends on autostart (off by default on Xiaomi) |
| Network during Doze | Official Doze docs say only “Suspends network access,” without mentioning a foreground-service exception; the power-restriction table says “No restrictions” for network when “App process is running a foreground service.” **AOSP source** allows network during Doze / power saving when `procState <= PROCESS_STATE_BOUND_FOREGROUND_SERVICE` (or with `POWER_RESTRICTED_NETWORK` capability); partial wakelocks are disabled during Doze only when `procState > BOUND_FOREGROUND_SERVICE` and not allowlisted | On stock Android, **processes with foreground services can still network and hold locks during Doze**. Vendors may change this (freezing, network cuts), so test physically |
| User taps Stop in “Active apps” (13+) | Entire app removed from memory, foreground-service notification disappears, **no callbacks**; scheduled jobs and alarms remain; next launch has `REASON_USER_REQUESTED` in `ApplicationExitInfo`. Test: `adb shell cmd activity stop-app <package>` | Do not secretly restart after an explicit user stop; explain “You stopped the background connection” on next open |
| Dismissible foreground-service notifications (14+) | Users can swipe away `setOngoing(true)` notifications (except while locked or through Clear all); service itself is unaffected | Persistent summary disappears; consider reposting on the next state change or explaining in settings |
| Foreground-service notification delay (12+) | System waits up to 10 seconds by default; action buttons or `setForegroundServiceBehavior(FOREGROUND_SERVICE_IMMEDIATE)` make it immediate | Use `IMMEDIATE` for the persistent notification |
| JobScheduler quotas (16+, all apps) | Jobs running alongside foreground services are quota-limited too (previously exempt) | WorkManager outbox retries on 16+ cannot assume a foreground service removes quotas |
| Memory cap (17+, all apps) | App limit based on total device memory; exceeding it kills the app with `REASON_OTHER` + `MemoryLimiter:AnonSwap` | Control resident-process memory (caches, images) |
| Alarms during Doze | `setAndAllowWhileIdle` / `setExactAndAllowWhileIdle` at most once per app per 9 minutes | Alarms cannot drive frequent heartbeats |

Sources:
- Types and duration caps: https://developer.android.com/develop/background-work/services/fgs/service-types
- Background-start exemptions: https://developer.android.com/develop/background-work/services/fgs/restrictions-bg-start
- Exceptions on `START_STICKY` restart (community, 2026-10): https://github.com/marcocrupi/react-native-notify-kit/issues/81 ; https://github.com/invertase/notifee/issues/470
- Android 15 boot-broadcast restrictions: https://developer.android.com/about/versions/15/behavior-changes-15
- Doze: https://developer.android.com/training/monitoring-device-state/doze-standby ; power-restriction table: https://developer.android.com/topic/performance/power/power-details
- AOSP source (GitHub mirror archived 2025-11, roughly Android 16): `NetworkPolicyManager.isProcStateAllowedWhileIdleOrPowerSaveMode` https://raw.githubusercontent.com/aosp-mirror/platform_frameworks_base/main/core/java/android/net/NetworkPolicyManager.java ; Doze wakelock rules in `PowerManagerService` https://raw.githubusercontent.com/aosp-mirror/platform_frameworks_base/main/services/core/java/com/android/server/power/PowerManagerService.java
- User stop: https://developer.android.com/develop/background-work/services/fgs/handle-user-stopping
- Android 14 dismissible notifications: https://developer.android.com/about/versions/14/behavior-changes-all
- 10-second delay: https://developer.android.com/guide/components/foreground-services#notification-immediate
- 16 / 17: https://developer.android.com/about/versions/16/behavior-changes-all , https://developer.android.com/about/versions/17/behavior-changes-all
- **No new restrictions targeting `specialUse` or persistent foreground services were found in Android 15 / 16 / 17** (behavior-change pages for all three reviewed).

### 4.2 How Chinese ROMs handle persistent foreground services

| ROM | Known behavior | Allowlisting path | Source / confidence |
| --- | --- | --- | --- |
| HyperOS | Autostart off by default; Powerkeeper kills for background locks, power use, long inactivity, sensors, with limits removed by Unrestricted. Freezing mechanisms such as GreezeManager / Millet (reports of frozen accessibility services); community says swiping away the card kills the process and prevents autostart | Autostart + Battery usage “Unrestricted” + recents lock | ✅ Xiaomi docs (3.2); 💭 community: https://www.volcengine.com/article/224461 , https://xdaforums.com/t/hyperos-3-0-cn-instantly-kills-my-background-apps.4792408/ |
| ColorOS | “Allow full background activity” ≈ Unrestricted; community says disabling `OKillerAppService` prevents FCM kills (requires root / adb, not applicable). ColorOS 13+ can pause long-unused apps. Official ColorOS 17 promotion claims “55.6% improvement in all-app retention” (app switching, not foreground services) | Autostart + Battery usage management + recents lock | 💭 https://www.v2ex.com/t/1179724 ; promotion: https://finance.sina.com.cn/tech/roll/2026-10-14/doc-inivcxxn0419857.shtml |
| OriginOS | “Allow high background power usage”; individual test (OriginOS 1 / Android 11, one device) reports possible unresponsiveness after prolonged screen-off time | Autostart + high background power usage + recents lock | 💭 https://blog.moeworld.tech/2025/10/15/vivo%E6%89%8B%E6%9C%BA-originos-oem%E5%AE%9A%E5%88%B6%E5%AE%89%E5%8D%93%E7%B3%BB%E7%BB%9F-%E5%BA%94%E7%94%A8%E4%BF%9D%E6%B4%BB/ (very old version; reference only) |
| MagicOS | App launch management (automatic management blocks unnecessary starts); **Smart data saver blocks background mobile data**. A Magic5 Pro user reports navigation killed minutes after switching away despite support-recommended settings | All three App launch toggles + Battery optimization + recents lock + disable Smart data saver | ✅ Honor official; 💭 https://club.honor.com/cn/thread-28539008-1-1.html |
| HarmonyOS 4.x | App launch management; **“Stay connected when device sleeps”** (default ❓); local-notification rate limits | App launch management + Battery optimization + sleep-time connectivity + recents lock | ✅ Huawei official (3.2) |
| HarmonyOS 5/6/7 Droitong | Droitong itself may be killed; foreground services inside the container ❓ | Background-allowlist Droitong | 💭 See 2.1 |

- ❓ **No public quantitative data was found for how long foreground services survive after allowlisting on any vendor ROM** (none official; only scattered community reports of kills). Online “92% / 85% / 62% background retention” measures consumer app switching without reload, not persistent connections, and must not be cited here.
- 💭 dontkillmyapp rankings (higher numbers are worse): Xiaomi 5 points, rank 2; Huawei 5, rank 1; OPPO 4; vivo 3. These pages mainly describe MIUI / EMUI / ColorOS 6 and have not been updated for new ROMs; they show only that Huawei and Xiaomi have historically been the most aggressive.
  Sources: https://dontkillmyapp.com/api/v2/xiaomi.json , https://dontkillmyapp.com/api/v2/huawei.json , https://dontkillmyapp.com/api/v2/oppo.json , https://dontkillmyapp.com/api/v2/vivo.json

### 4.3 Battery use: public measurements?

- ✅ ntfy (open-source push, using a persistent foreground-service JSON stream / WebSocket when self-hosted) officially says in its FAQ that the persistent connection “consumes about 0-1% of battery in 17h of use (on my phone).” **One phone owned by the author, with no model or network specified**; older FAQs said about 4% / 17 hours.
  ntfy-android v1.25.2 changed client WebSocket ping from 1 minute to 3 minutes, saying it “should slightly improve battery life,” without numbers.
  Sources: https://docs.ntfy.sh/faq/ , https://github.com/binwiederhier/ntfy-android/releases/tag/v1.25.2
- ✅ WeChat Mars team (articles around 2017): dense heartbeats repeatedly put the modem into a high-power state; recommended minimum interval 4.5 minutes. Measured NAT timeouts in one region: 5 minutes for China Mobile and China Unicom, over 28 minutes for China Telecom. Mars adaptive heartbeats range from 4.5 minutes to 9 minutes 50 seconds.
  Sources: https://blog.csdn.net/byeweiyang/article/details/73296619 (repost), https://www.cnblogs.com/xifengcoder/p/15340365.html (source-parameter explanation)
- ❓ **No battery measurements for foreground service + WebSocket on Chinese ROMs were found**. Persona must measure this itself (`docs/archive/redesign/android.md`, Section 10, already requires it in A1).

### 4.4 Feasibility and risks (💭 assessment)

- **Feasible**: Under stock rules, `specialUse` foreground services have no duration cap and their processes retain network access during Doze. PR #84 already passed foreground interaction on a physical HyperOS 3 device (not prolonged locking).
- **Main risks**, highest first:
  1. Incomplete user allowlisting: missing autostart, Unrestricted, or recents locking can make the connection fail shortly after locking. This is an experience problem, not a technical one; readiness checklists and self-tests provide the safety net.
  2. No restart after a kill: `START_STICKY` is not a background-start exemption, and Chinese ROMs often do not restart services. Mitigate with battery-optimization allowlisting (also a background foreground-service-start exemption), boot broadcasts, a periodic WorkManager job (minimum 15 minutes) checking/restarting the service (background start allowed only with battery optimization disabled; whether jobs run after vendor kills also depends on autostart), and immediate recovery when the user next opens the app. Network-change callbacks cannot arrive after the process dies.
  3. Vendor freezing: HyperOS and ColorOS use cgroup freezing, with no public statement on foreground-service-process exemptions. Physically test whether hub can still push after 8 hours locked.
  4. Separate network restrictions: Honor Smart data saver and Huawei Stay connected when device sleeps can break connections on mobile data / during sleep while the process stays alive. **Symptoms: no notifications, immediate recovery when the app opens**, easily misdiagnosed as a hub problem.
  5. Battery use: depends on heartbeats. Both sides currently ping every 20 seconds: PR #84's OkHttp `pingInterval(20, SECONDS)` (`android/.../data/ApiClient.kt`) and hub WebSocket ping (`server/internal/hub/http.go`). Too frequent in background (about 3 modem wakeups per minute). Use short foreground pings for weak-network detection, but **hub-sent background heartbeats several minutes apart** (Mars starts at 4.5 minutes, ntfy uses 3). Do not depend on the phone's own timer during Doze (OkHttp timers stop while CPU sleeps; alarms are limited to once per 9 minutes). Public-internet direct connections (separate research) also require measuring carrier NAT / IPv6-firewall idle timeouts to choose the interval.
- **Test method** (add to A1): `adb shell dumpsys deviceidle force-idle` simulates only stock Doze, not vendor policies. Use physical phones, unplugged, screen off for 8 hours; hub records disconnected intervals, the phone records `ApplicationExitInfo`. Require at least 10 nights per phone before conclusions (user convention: label samples < 10).

## 5. Persistent lock-screen notification / Dynamic Island-like features

### 5.1 Standard APIs (✅ facts, verified material from android-tech.md)

- Android 16 (API 36): Live Updates. Requirements: Standard / BigText / Call / ProgressStyle (MetricStyle added in 17); manifest `POST_PROMOTED_NOTIFICATIONS`; `setRequestPromotedOngoing(true)`; ongoing with a title; no custom RemoteViews, group summary, or colorized notification; **channel must not be `IMPORTANCE_MIN`**. **Official docs explicitly permit OEMs to add conditions**. Users may disable it: query `NotificationManager.canPostPromotedNotifications()`, navigate with `Settings.ACTION_MANAGE_APP_PROMOTED_NOTIFICATIONS`. Status-bar capsule uses `setShortCriticalText`.
- Android 17 (API 37): `Notification.MetricStyle`, up to 3 metrics and 3 buttons, promotable to Live Update.
- Sources: https://developer.android.com/develop/ui/views/notifications/live-update , https://developer.android.com/develop/ui/views/notifications/metric-style , https://developer.android.com/about/versions/17/features

### 5.2 Vendor islands' support for standard APIs

| ROM | Name | Standard Live Updates (`setRequestPromotedOngoing`) | Vendor-private API | Source / confidence |
| --- | --- | --- | --- | --- |
| HyperOS 3.0 | HyperIsland | ❌ (no native Live Updates before 3.0.300) | Focus notifications / HyperIsland templates require Xiaomi developer verification, selecting an app **already published in Xiaomi's app store**, certificate fingerprint, scenario pre-review and full proposal, allowlisted-device testing (OAID, at most 10 devices, expiry 30 days), then launch verification. **Review + allowlisting; unavailable to Persona** (docs updated 2025-12-05) | ✅ Xiaomi official https://dev.mi.com/xiaomihyperos/documentation/detail?pId=2132 , https://dev.mi.com/xiaomihyperos/documentation/detail?pId=2141 |
| HyperOS 3.1 (OS3.0.300+) / 4 | HyperIsland | ✅ From 3.1, HyperIsland supports Android 16 Live Updates; third-party apps need no Xiaomi code | Same | 💭 News + open-source testing (Capsulyric marks “Verified”: “For HyperOS, version 3.0.300+ is required”): https://www.huaweicentral.com/hyperos-3-1-finally-brings-a-stacked-recent-apps-layout-option/ , https://github.com/FrancoGiudans/Capsulyric/wiki ; no Xiaomi official docs |
| ColorOS 16 / 17 | Fluid Cloud | ✅ **Official statement**: Fluid Cloud is fully compatible with Android 16's live-activity API; apps following Google standards adapt directly (“dual compatibility”: standard and OPPO APIs both work) | OPPO's API requires a serviceId application | ✅ ITHome 2025-10-17 (statement by OPPO design lead) https://www.ithome.com/0/890/345.htm ; 💭 community https://github.com/huverse/SuperXD/pull/93 |
| OriginOS 6 / 7 | Atomic Island / Atomic Notifications | International version: deeply integrated with Android 16 Live Updates; **❓ no official statement found on standard-API support in China-market version** (community claims Atomic Island reads standard notifications too; unverified) | Third parties in China require vivo partnerships (JD Food Delivery, 12306, Amap, Alipay, etc., announced individually) | 💭 https://tech.udn.com/tech/story/123151/9075516 , https://github.com/ccch1mneyyy/dsh-go/issues/70 , https://finance.sina.com.cn/tech/digi/2025-06-11/doc-ineztfhw4487045.shtml |
| MagicOS (10 / 11) | Magic Capsule | 💭 Community: after upgrading to Android 16, Live Updates appear in Magic Capsule (tested only on Honor) | Nonpublic `notification.live.*` extras protocol (community reverse-engineered, tested on physical MagicOS / Android 17); official claim: capsule gradually expanding to third-party apps | 💭 https://linux.do/t/topic/3064474 (WebFetch 403, content from search snippets), https://github.com/The-Brotherhood-of-SCU/Bugaoshan/pull/368 , https://www.sina.cn/weibo/detail/5357264527428248.html |
| HarmonyOS 4.x | Live View | ❌ (Android 16 APIs absent on API 31) | Community: local notifications with `notification.live.*` extras can trigger Live View (nonpublic); **official Live View Kit requires HarmonyOS 5+, admission approval, and mainland China** | 💭 https://github.com/The-Brotherhood-of-SCU/Bugaoshan/issues/363 ; ✅ https://developer.huawei.com/consumer/cn/doc/HarmonyOS-Guides/liveview-introduction (search snippets) |

- **MetricStyle (Android 17) display on these ROMs: ❓ nothing found** (no mention of Chinese ROMs in official docs, news, or open-source projects).
- 💭 What ordinary apps can use:
  - ColorOS 16+, HyperOS 3.1+: standard Live Updates reach islands / capsules, **without allowlisting** (official ColorOS statement; Xiaomi has only news and community tests).
  - China-market OriginOS, MagicOS: standard APIs likely display in promoted notification-shade and lock-screen positions; island display needs physical testing.
  - Vendor-private APIs (Xiaomi focus notifications, OPPO serviceId, vivo partnerships, Honor / Huawei `notification.live.*`) are allowlisted or nonpublic; **avoid in the first release**.

### 5.3 Impact on Persona (💭)

1. PR #84's persistent channel is `IMPORTANCE_MIN`, **ineligible for promotion**; apps cannot change channel importance after creation. Create an `IMPORTANCE_LOW` (silent, promotable) “Live status” channel during reorganization, move the persistent notification there, and delete the old channel.
2. Request promotion only while active (Needs you / Working), cancel it while idle (already in redesign 5.10). This also reduces the chance of vendor downgrading.
3. “Needs you” alerts still use regular high-priority notifications, not the island (already in redesign 5.10). Official usage guidelines discourage activities triggered by others and reminder content; vendor islands are tuned for travel / delivery / timers.
4. For every device in the physical matrix (Section 8), capture active status bar / lock screen / island to verify the standard API's actual appearance.

## 6. System capabilities without Google services

General rule (💭): **Treat everything in `com.google.android.gms` or delivered / updated through Google Play services as absent on China-market phones**. Phones without GMS also receive no Google Play System Updates (Mainline modules); modules update only through vendor OTA.

| Capability | China-market status (no GMS) | Persona approach | Source / confidence |
| --- | --- | --- | --- |
| Push (FCM) | Absent | Persistent connection by default (already PR #84); FCM optional only for GMS phones | Decided |
| **Health Connect** | Official: “requires … Google Play services installed,” “exclusive to Android and Google Play”; part of the system on 14+. OPPO Health **China-market version has no Health Connect integration at all** (no health permissions in Manifest or dex references; uses signature-level private APIs). Xiaomi / vivo / Honor China-market behavior ❓; Huawei uses its own Health Kit | `phone_health_summary` will **generally have no data** on China-market devices. First check `HealthConnectClient.getSdkStatus()`; if unavailable, do not register the tool (or return “This phone has no health data source”). **Recommend removing it for China-market devices in the first release** (iOS unaffected) | ✅ https://developer.android.com/health-and-fitness/health-connect/availability ; ✅ (open-source reverse engineering) https://github.com/svip886/OHSync |
| System photo picker | AndroidX `PickVisualMedia`: 33+ always uses `MediaStore.ACTION_PICK_IMAGES`; 30–32 needs SDK extension ≥ 2 (Google System Update module, **likely absent on China-market devices**), otherwise system-app `androidx.activity.result.contract.action.PICK_IMAGES`, then `ACTION_OPEN_DOCUMENT` (selection-count limit lost). Play-services-installed backport on Android 4.4–10 unavailable in China-market devices | Use AndroidX directly (automatic fallback). With minSdk 31, Android 12 / 12L and HarmonyOS 4 (API 31) use a file picker: different UI, still functional. ❓ Whether vendors change/replace the 33+ system picker needs physical testing | ✅ https://developer.android.com/training/data-storage/shared/photopicker ; AndroidX source https://raw.githubusercontent.com/androidx/androidx/androidx-main/activity/activity/src/main/java/androidx/activity/result/contract/ActivityResultContracts.kt |
| Location | No Fused Location Provider (`play-services-location`). `LocationManager` GPS always exists; `NETWORK_PROVIDER` relies on vendor network-location services. Old sources say native network location often fails in China-market devices (Amap / Baidu use their own SDKs); new ROMs ❓ | Use `LocationManagerCompat.getCurrentLocation`, select from `getProviders(true)` (12+ try platform `FUSED_PROVIDER`, then GPS, NETWORK), with timeout. State coordinate system in results (GPS WGS-84; Chinese maps GCJ-02; hub / AI decides whether to convert) | 💭 https://blog.csdn.net/u013040434/article/details/117700746 (old, 5 years ago); coordinate systems are a general-knowledge assessment |
| `Geocoder` | AOSP docs: backend is outside the core framework; missing backend yields empty lists; query with `isPresent()`. Generally provided by GMS; **❓ whether China-market phones have a backend was not found** | Check `isPresent()` first; empty results return only coordinates. Later, add configurable reverse geocoding on hub if addresses are needed (P2) | ✅ AOSP comments quoted in Geocoder source / docs https://developer.android.com/reference/android/location/Geocoder |
| System WebView | New Xiaomi / OPPO / vivo / Honor devices preload Google's Android System WebView, but **some claim vendor stores do not update it, leaving OTA-only updates** (fewer than 4 per year); other guides say Xiaomi/vivo stores list it—**conflicting claims ❓**. Huawei HarmonyOS 4 uses Huawei Browser's WebView engine, updated through AppGallery; HarmonyOS NEXT uses its own engine | Gate every new androidx.webkit feature (`startUpWebView`, `prerenderUrlAsync`, `NavigationListener`) with `WebViewFeature.isFeatureSupported()`; fall back to regular loading. Settings › Diagnostics shows package/version from `WebViewCompat.getCurrentWebViewPackage()` | 💭 Zhihu (403, content from search snippets) https://zhuanlan.zhihu.com/p/2077738698548982270 ; https://www.cnblogs.com/reachteam/p/22796594/android-webview-update-guide (2026-09-01) |
| Contacts | `ContactsContract` is an AOSP provider present on China-market phones; no extra vendor restrictions found | Original plan; physically verify reads/search | 💭 |
| Calendar | `CalendarContract` is an AOSP provider. Without a Google account, no writable calendar may exist. Official approach: create a local calendar as a sync adapter (URI `CALLER_IS_SYNCADAPTER=true`, `ACCOUNT_TYPE_LOCAL`) | Find a writable calendar before creating events; otherwise create a local “Persona” calendar. ❓ Whether vendor calendar apps show it needs testing | ✅ https://developer.android.com/identity/providers/calendar-provider |
| Clipboard | AOSP: foreground-only reads from 10, system read prompts from 12. Xiaomi manages reads through AppOps (store policy: foreground triggers only); ColorOS 14 has per-app clipboard access and read reminders; HarmonyOS 4 ❓ | Read only in foreground; null results prompt “Check clipboard access in system settings” | ✅ https://dev.mi.com/xiaomihyperos/documentation/detail?pId=2225 ; 💭 https://bbs.oneplus.com/thread/6223871 |
| QR scanning | zxing-android-embedded depends only on `zxing:core`, not GMS (actually API 24+, latest 4.3.0, copyright 2022, slow updates). Alternative: ML Kit **bundled** `com.google.mlkit:barcode-scanning` (model included in APK, about +2.4 MB); **unbundled** (`play-services-mlkit-barcode-scanning`) and Google code scanner require GMS | Keep zxing (verified by PR #84). For a future Compose viewfinder: CameraX + ML Kit bundled (❓ official docs do not explicitly guarantee no GMS interaction on no-GMS phones; test physically) | ✅ https://github.com/journeyapps/zxing-android-embedded , https://developers.google.com/ml-kit/vision/barcode-scanning/android |
| Downloadable fonts | Compose Google Fonts download provider is `com.google.android.gms`, absent on China-market devices | Bundle fonts for all 5 styles in the APK (`R.font`), no downloadable fonts | ✅ https://developer.android.com/develop/ui/compose/text/fonts |
| Google login for Gmail authorization | Redesign 6.1's Google Authorization API belongs to Play-services Identity, absent in China-market devices; Google disallows custom-scheme callbacks for Android clients | 💭 Public direct connectivity gives hub a reachable HTTPS address: use Web OAuth client, return to hub (`/oauth/google/callback`); phone opens authorization through browser / Custom Tabs only. Fallback: app password over IMAP. Whether accessing Google needs a proxy is the user's network concern | 💭 Based on existing `docs/extensions/mail.md` flow (iOS uses reversed-client-ID custom scheme) |
| Custom Tabs | Requires a browser supporting the Custom Tabs protocol; vendor-browser support ❓ | `CustomTabsIntent` opens the default browser when unsupported; function remains, just outside the app | 💭 |
| Crash reporting / analytics | Firebase Crashlytics / Analytics depends on Google servers, unreachable on domestic networks too | 💭 No Firebase; upload crash logs (`ApplicationExitInfo` + uncaught exceptions) to hub next launch (future iOS support can share the endpoint) | 💭 |
| WorkManager, CameraX, Biometric, Room, DataStore | All AndroidX, no GMS dependency | Use normally | 💭 (general knowledge) |

**Removal / replacement list** (💭):
- Remove for China-market devices: default FCM path (make optional), Health Connect tools, Google code scanner, downloadable fonts, Firebase family.
- Replace: Fused Location → `LocationManager`; Authorization API → Web OAuth with hub callback; system photo picker on 31–32 → file picker (automatic).
- Keep with fallbacks: `Geocoder` (possibly empty), new androidx.webkit features (WebView-version checks), Custom Tabs (possibly external browser).

## 7. Distribution (without Play)

### 7.1 First installation: vendor blockers

| ROM | Obstacle | Source / confidence |
| --- | --- | --- |
| All Android 8+ | “Install unknown apps” is authorized per source app (browser, file manager…) | ✅ Standard AOSP behavior |
| HyperOS | “Security guard” restricts unscanned apps from third-party sources; disabling requires identity verification (lock-screen password / Xiaomi account / fingerprint). Installer security checks (installation checks, security monitoring) can be disabled in its settings. adb requires “Install via USB” in Developer options (Xiaomi login required); phone confirmation auto-rejects after a few seconds (`INSTALL_FAILED_USER_RESTRICTED`) | 💭 Xiaomi Security guard page was JS-rendered and unreadable, content from search snippets https://app.market.xiaomi.com/hd/apm-h5-cdn/cdn-safe-guard.html ; user experience https://wap.zol.com.cn/ask/x_33926474.html ; ✅ adb findings from physical-device records in `docs/android/android.md` |
| HarmonyOS 4.x | “Pure mode” (Settings › System & updates › Pure mode): with Enhanced protection on, **only apps passing AppGallery safety checks may install**. With it off, only high-risk apps are blocked; ordinary APKs require lock-screen-password confirmation. Also has an external-source app safety-check toggle. Articles disagree on Enhanced protection defaults | 💭 Third-party articles (quoting what appears to be official Huawei prompt text) https://www.32r.com/zx/1201.html , https://blog.csdn.net/NSJim/article/details/133850450 ; ❓ official Huawei page not read |
| HarmonyOS 5/6/7 | Droitong only (download APK, “Open with Droitong”); some devices have Pure mode enabled by default and must disable it first | 💭 See 2.1 |
| ColorOS / OriginOS / MagicOS | All have preinstallation risk scans / prompts; ❓ no official source found for exact toggle names/defaults (many templated Pure-mode comparison advertorials conflict and cannot be cited) | ❓ |

💭 For Persona: first installation is one-time and acceptable. Provide brand-specific installation instructions on pairing / download pages, explaining risk prompts (not published in an app store).

### 7.2 In-app self-updates (hub-hosted APK)

**Platform rules (✅ AOSP source, mirror roughly Android 16; new Android 16 / 17 entries absent from that source)**:

- `REQUEST_INSTALL_PACKAGES` + user enables Install unknown apps for Persona (`Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES`, query `canRequestPackageInstalls()`).
- `setRequireUserAction(USER_ACTION_NOT_REQUIRED)` (31+) avoids confirmation only when **all** hold:
  - Installed app target: ≥ 29 on Android 12, ≥ 30 on 13, ≥ 31 on 14, ≥ 33 on 15 (“The target API level requirement will advance in future Android versions”).
  - Installer is current version's update owner (if ownership enabled) / installer of record (otherwise) / **or updating itself (“Updating itself”)**.
  - Installer declares `UPDATE_PACKAGES_WITHOUT_USER_ACTION`.
  - Docs explicitly: “Session owners should always be prepared to handle STATUS_PENDING_USER_ACTION.”
- `setRequestUpdateOwnership(true)` (34+) is **effective only on initial install**, a no-op on update, and requires `ENFORCE_UPDATE_OWNERSHIP`. It makes other installers require user confirmation to update the package. **Persona is first installed by a browser / file manager and cannot acquire ownership itself; this API does not help self-updates** (intended for app stores).
- Source: https://raw.githubusercontent.com/aosp-mirror/platform_frameworks_base/main/core/java/android/content/pm/PackageInstaller.java (`SessionParams.setRequireUserAction` / `setRequestUpdateOwnership` comments)

**Actual behavior on Chinese ROMs**:

- ✅ (two open-source projects, 2026-09 / 10) **Requesting silent self-updates fails directly on HyperOS**: log `MIUILOG- assertCallerAndPackage ... Permission denied`, session ends with `INSTALL_FAILED_ABORTED`, **without any confirmation dialog**. Xiaomi added its own caller check in `PKMSImpl`, accepting only allowlisted installers. On Xiaomi / Redmi / POCO, use `USER_ACTION_REQUIRED` for system confirmation (as sideload updaters such as F-Droid do), and classify “ABORTED + Permission denied” separately, not as user cancellation.
  Sources: https://github.com/sofianeelhor/PKForge/issues/25 (2026-09-28, POCO X5 Pro, HyperOS 2.0.14, Android 14), https://github.com/HRYNdev/kelevra-box/pull/37 (2026-10-01, Redmi Note 14 Pro+, HyperOS, Android 16)
- ColorOS / OriginOS / MagicOS / HarmonyOS 4: ❓ not found. Conservatively design for confirmation every time; silent updates are only a stock-Android optimization.

**Recommendations (💭)**:
1. hub provides `GET /api/v1/android/latest` (version, `versionCode`, APK SHA-256, size, release notes) and APK downloads authenticated by device token. Check in foreground; verify SHA-256 after download. The system also verifies the signing identity matches the installed version.
2. Use `PackageInstaller` sessions, default `USER_ACTION_REQUIRED` (system confirmation). Try `NOT_REQUIRED` only on stock / verified ROMs; handle `STATUS_PENDING_USER_ACTION` (open system confirmation) and `INSTALL_FAILED_ABORTED`.
3. Updates kill the process: restore the persistent service through `MY_PACKAGE_REPLACED` afterward (also a background foreground-service-start exemption; receiving it on vendor ROMs may depend on autostart, ❓ physical testing required).
4. Avoid domestic app stores: MIIT has required app registration before store distribution since 2023 (including individuals); sideloading is simpler for a personal tool. Source: https://baike.baidu.com/item/%E5%B7%A5%E4%B8%9A%E5%92%8C%E4%BF%A1%E6%81%AF%E5%8C%96%E9%83%A8%E5%85%B3%E4%BA%8E%E5%BC%80%E5%B1%95%E7%A7%BB%E5%8A%A8%E4%BA%92%E8%81%94%E7%BD%91%E5%BA%94%E7%94%A8%E7%A8%8B%E5%BA%8F%E5%A4%87%E6%A1%88%E5%B7%A5%E4%BD%9C%E7%9A%84%E9%80%9A%E7%9F%A5/63291780

### 7.3 Does Google's 2027 developer verification apply to China-market phones?

- ✅ Official: enforced only on “certified Android devices,” starting 2026-09-30 in Brazil, Indonesia, Singapore, Thailand, expanding globally in 2027. “It will apply to all certified Android devices running Android 7 or higher. These updates are delivered through Google Play services.” **adb installations are unaffected** (“The waiting period does not apply to ADB installs”). Apps from unverified developers use an advanced flow (enable Developer mode, confirm nobody instructed the user, restart and reauthenticate, **wait 24 hours**, then biometric confirmation; allow for 7 days or permanently). Limited-distribution accounts (at most 20 devices, no ID or fee) launched 2026-08.
  Sources: https://developer.android.com/developer-verification , https://developer.android.com/developer-verification/guides/faq
- 💭 Assessment: checks are delivered through Google Play services and run only on certified devices. **China-market devices lack GMS and are not Google-certified, so they are largely unaffected**. FAQ does not explicitly cover no-GMS / China. ❓ Some claim certain China-market devices hide GMS components (https://www.chinesephoneguy.com/blog/how-to-enable-google-play-on-chinese-phones , weak source); even enabled, they are usually not certified. Confirm physically.
- 💭 For Persona: registration is needed only for installation on GMS-equipped international phones (limited distribution suffices, already in redesign Section 9); China-market users need not handle it.

## 8. Testing: minimum physical-device matrix and cloud devices

### 8.1 Minimum physical-device matrix (💭 assessment)

Basis: installed-base shares (QuestMobile 2026-06: Huawei 21.6%, OPPO about 18%, vivo 16.5%, Xiaomi 10.1%, Honor about 7.8%), current releases (all Android 17, installed base mainly Android 16), and minSdk 31's lowest supported devices (HarmonyOS 4, API 31).

| # | Device | OS | Why required | Priority |
| --- | --- | --- | --- | --- |
| 1 | Xiaomi / Redmi (already available: Redmi K60 Pro) | HyperOS 3.x, Android 16 → later HyperOS 4 / Android 17 | One of the strictest background policies; self-update pitfall; HyperIsland (standard Live Updates only from 3.1+) | P0 (available) |
| 2 | Huawei | **HarmonyOS 4.2 / 4.3** (API 31) | minSdk floor; silent + rate-limited local notifications; sleep-time connectivity; file-picker fallback; Huawei Browser WebView engine | P0 |
| 3 | OPPO / OnePlus | ColorOS 16 or 17 | Second installed-base share; official Live Updates compatibility in Fluid Cloud; validate MetricStyle / promoted-notification appearance | P0 |
| 4 | vivo / iQOO | OriginOS 6 or 7 | Top-three share; one-time first-launch notification dialog; unknown Atomic Island standard-API support | P1 |
| 5 | Honor | MagicOS 10 or 11 | Smart data saver; App launch management; Magic Capsule | P1 |
| 6 | Huawei | HarmonyOS 5 / 6 (Droitong) | Decide support: whether foreground services and notifications work in the container | P2 (borrow one) |
| — | Emulator | Android 12, 14, 16, 17 (AOSP / Google API images, no Play) | Stock-behavior baseline, screenshots, `am compat` switches; **cannot simulate vendor policies** | Ongoing |

- 💭 Order: acquire 2 (Huawei 4.x) and 3 (OPPO) first. With existing Xiaomi, they cover three extremes: strictest background, hardest notifications, friendliest standard APIs. Test 4/5 navigation and notification settings on cloud devices before buying if needed.
- Required on each device (add to `docs/operations/testing.md`):
  1. Initial install (blocking prompts), first launch (notification permission and every readiness-checklist link reaching the correct page).
  2. Test notification after 30 seconds locked (sound, screen wake, lock-screen display).
  3. Locked, unplugged for 8 hours: hub records disconnected intervals, phone records `ApplicationExitInfo`, **at least 10 nights**.
  4. Battery use per hour (system battery page + `dumpsys batterystats`).
  5. Service recovery after swiping away the card / stopping in Active apps / restarting the phone (`adb shell dumpsys package <package> | grep stopped` to check force-stop state).
  6. One self-update.
  7. Active status bar / lock screen / island screenshots (light and dark).
  8. Photo selection, location, WebView version, QR scanning.

### 8.2 Cloud-device platforms

| Platform | Capabilities | Limits | Source / confidence |
| --- | --- | --- | --- |
| Xiaomi TestIt | Compatibility testing (install, launch, uninstall, Monkey, screen recording, CPU / memory, about 15 minutes), remote physical-device debugging (new Xiaomi models) | **At most 1 hour per session, 30 free sessions daily**; check in within 10 minutes of reservation; APK ≤ 2G; devices **have no SIM or SD card and are not rooted**; docs do not mention remote adb | ✅ https://dev.mi.com/xiaomihyperos/documentation/detail?pId=1523 (updated 2026-04-14, some old content) |
| Huawei AGC Cloud Debugging | Physical Huawei devices (phones through HarmonyOS computers), live/offline logs, Shell commands | Blog says 24 hours free, extendable; one comparison says no remote adb | ✅ (official page only title readable) https://developer.huawei.com/consumer/cn/agconnect/cloud-adjust/ ; 💭 https://www.cnblogs.com/iofomo/p/18457996 (2024) |
| Honor Cloud Debugging | Physical Honor devices | Blog (early 2026): 24-hour free base allocation, unlimited extensions | 💭 https://blog.csdn.net/weixin_29218963/article/details/159326513 |
| OPPO Cloud Devices | Physical OPPO / OnePlus / realme devices | Blogs: 12 hours for new users (2023) or 600 free minutes (2024); “mediocre smoothness, unstable” | 💭 https://blog.csdn.net/ch1451082329/article/details/132344580 (2023), https://www.cnblogs.com/iofomo/p/18457996 (2024) |
| vivo Cloud Testing | Physical vivo / iQOO devices, logs | Blogs: 6 hours for new users (2023); no remote adb (2024) | 💭 Same two articles |
| Tencent WeTest Cloud Devices | Thousands of devices, free public cloud for mainstream models | **Remote adb only for paid users**; about CNY 1 / minute | ✅ https://wetest.qq.com/product/cloudphone ; 💭 price from blog |
| Testin | 2500+ models, including HarmonyOS NEXT | About CNY 3 / minute, 20 free minutes for new users | 💭 https://cloud.tencent.com/developer/article/2586389 (late 2025) |

- 💭 **Cloud devices cannot test what matters most to Persona**: one-hour sessions (Xiaomi) cannot measure overnight survival; devices are usually always charging (Doze does not apply and battery use cannot be measured; ❓ vendors do not explicitly state power connection). No SIM means no mobile-data background-policy tests (such as Honor Smart data saver); lock-screen notifications are also hard to inspect.
- 💭 **Suitable cloud tests**: installation blockers, first-launch notification dialogs, every readiness-checklist link reaching the right page (component names surviving across versions), UI under vendor fonts / resolutions, photo pickers, and WebView versions. These are exactly the components whose names become stale; rerun on each major release.

## 9. Unresolved questions / measurements needed

| # | Unresolved | How to resolve |
| --- | --- | --- |
| 1 | No public 2026 Android-version distribution from Chinese analytics platforms (Umeng+, Jiguang, Getui); StatCounter measures pageviews and includes HarmonyOS 3/4 under Android 12 | Use physical matrix and actual users' devices; hub gathers its own user statistics from `client_hello` |
| 2 | Whether HarmonyOS 2 compatibility is API 29 (third-party reports only) | Irrelevant with minSdk 31 |
| 3 | New-app banner / lock-screen defaults on current HyperOS 3/4, ColorOS 16/17, OriginOS 6/7, MagicOS 10/11; whether Xiaomi notification filtering is unchanged in HyperOS; interaction between vivo's first-launch prompt and Android 13 permission | Install a fresh build and capture each physical device |
| 4 | Autostart / battery-page components on ColorOS 15+, OriginOS 6+, MagicOS, `com.hihonor.systemmanager` | Record components using `adb shell dumpsys activity top` on cloud devices (Xiaomi TestIt, Huawei / Honor cloud debugging) |
| 5 | **Whether foreground-service processes are frozen / disconnected** (HyperOS Millet / GreezeManager, ColorOS freezing) | Physical devices locked 8 hours, hub downtime records, ≥ 10 nights |
| 6 | Persistent-connection battery use on Chinese ROMs (only ntfy author's one-phone 0–1% / 17 hours) | A1 measurements: min / p50 / p90 / max / mean |
| 7 | Whether HarmonyOS 4.x local-notification self-classification entitlement is available outside AppGallery; whether persistent foreground-service notifications count toward marketing quota; whether exceeding it silences or hides notifications | Send notifications continuously on a physical device to find the first silent one; ask Huawei developer support |
| 8 | China-market OriginOS / MagicOS island support for standard Live Updates; Android 17 `MetricStyle` appearance on any vendor ROM | Physical-device screenshots |
| 9 | China-market `NETWORK_PROVIDER`, `Geocoder` backend, vendor changes to the 33+ system photo picker | Run each once per physical device |
| 10 | Who updates System WebView (vendor store or OTA only; conflicting claims), current Chromium version by vendor | Inspect `WebViewCompat.getCurrentWebViewPackage()` physically; batch-check cloud devices |
| 11 | `USER_ACTION_NOT_REQUIRED` self-update behavior on ColorOS / OriginOS / MagicOS / HarmonyOS 4 (HyperOS failure confirmed) | One self-update per physical device |
| 12 | Whether China-market devices with hidden GMS enabled count as Google-certified (affecting 2027 verification) | Check Play Store device certification when such a device is available |
| 13 | Actual foreground-service, notification, and persistent-connection behavior inside Droitong | Borrow HarmonyOS 5/6 |
| 14 | Whether cloud devices always charge (Doze impact), 2026 free quotas by vendor (only old blogs for OPPO, vivo, Honor, Huawei) | Log into each platform and check |
