# 设置、设备与连接接线

App 已通过 `PairingModule` 提供完整的 `SettingsServices`。旧 Hilt 入口也读取这份服务；`SettingsScreen` 和准备清单入口按 `binding` 订阅当前配对，外壳不需要另建服务。体验模式没有这份绑定，继续使用示例服务。

`DeviceSettingsModule.kt` 新增 `WiredDeviceSettings`，按配对代次和当前受信路由创建 `HttpDeviceSettings`。地址来自现有连接的选路结果，HTTP client 复用其 TLS 和本地地址策略。`preferenceDeviceId` 使用当前凭据的 `deviceId`，与 WebSocket 握手一致；缺少设备 ID 的旧配对明确显示设备服务不可用，需要重新配对。设备服务不会把凭据 ID 当作另一套通知注册 ID。

设置头部订阅当前路由地址。设备列表、撤销，以及通知的 `input`、`done`、`lock_details` 均已接通；保存继续保留未知字段。通知模块通过同一个 `WiredDeviceSettings` 读取偏好。每个 hub `notify` 事件展示前刷新本机偏好，原始 `interaction` 只更新仓储；`done=long/always` 的长任务、已看和会话屏蔽仍由 hub 决策。读不到当前偏好时不展示业务提醒。关闭锁屏详情时使用脱敏公开版本；开启时允许系统展示详情。收紧隐私、关闭提醒或退出配对时撤掉旧提醒，避免旧内容继续出现在锁屏。

## 统一退出

“断开并忘记服务器”“撤销本机”和当前凭据的 401 → Revoked 共用 `ConnectionRuntime` 清理。重新配对也先执行清理。清理会关闭唯一连接、取消并等待连接任务，停止常驻服务和 watchdog，撤回提醒，清凭据，再由 `HubDataStore` 关闭并删除原作用域的缓存、图片、草稿、回复和列表偏好。旧配对的 HTTP 对象不再缓存在配对服务中。

没有配对代次的 `chat-data`、`inbox-downloads`、`inbox-replies`、`list-snapshots`、`chat-images`、旧列表偏好和后台记录会在首次运行接线代码时删除一次，成功后写入 no-backup 标记。仍由兼容入口写入的准备清单、通知动作、后台恢复和平台入口记录在每次退出时清除。删除中断时沿用存储层 tombstone，下次启动继续；撤销清理失败时保留失效状态并记录延后清理，不恢复连接。

准备清单和通知后台记录仍使用兼容的全局存储，但退出时会清除；后续若改为配对作用域存储，应同时替换写入方。其他车道负责的对话和收件箱仓储仍须遵守 `ScopedHubData.coroutineScope` 的生命周期。

## 运行诊断与客户端信息

`ConnectionRuntime` 已实现 `ConnectionDiagnosticsSource`，转发现有 `OkHttpHubConnection` 的诊断流和读取方法。切换配对会取消旧订阅，退出会清空诊断。配对服务的诊断也转发这里，不再提供配对快照。没有新增 WebSocket。

诊断页显示实际路径、收包时间、失败原因、单次候选探测和 hub 已确认的心跳配置；无结果时显示“暂无观测数据”或等待状态。单次探测标注样本数，不作为性能基线。协商值是配置，不是实测收包频率；网络 RTT、实际收包频率和耗电均未实测。

`client_hello.build` 使用宿主包的 `longVersionCode`，`os` 使用准备清单已有的系统属性识别结果，包含系统名、版本和 Android SDK。未改服务端或 iOS 协议。

## 网络与心跳配置

运行时读取默认网络的真实 transport，按 Wi-Fi、蜂窝、其他网络及前后台选择 `HeartbeatPolicy`。切网重建同一条连接并重新发送协商，进出前台也会重新协商；页面只显示 hub ACK 的生效配置。

默认值沿用 `docs/archive/redesign/android.md` 5.9 和 `docs/archive/redesign/hub-changes.md` 1.6：前台请求 20 秒，后台各网络从 240 秒起，pong 超时沿用 15 秒。这些都是设计配置，未实测。`docs/operations/network.md` 没有补充各网络的实测间隔，因此 Wi-Fi 和蜂窝的默认值相同，没有编造差异。

宿主可在 `connection-heartbeat` SharedPreferences 中配置以下键，重启运行时后生效；当前没有设置页入口：

| 键 | 用途 |
| --- | --- |
| `foreground_seconds` | 前台请求间隔，Int |
| `background_wifi_seconds` | Wi-Fi 后台请求间隔，Int |
| `background_cellular_seconds` | 蜂窝后台请求间隔，Int |
| `background_other_seconds` | 其他网络后台请求间隔，Int |
| `pong_timeout_seconds` | 请求的 pong 超时，Int |
| `background_jitter_fraction` | 后台向下抖动比例，Float；默认关闭，有配置时只缩短、不加长 |

各网络差异和抖动幅度都需要真机实测后配置。当前只有选择与重新协商机制，尚无运营商、NAT 空闲超时、Doze、锁屏过夜存活或各 ROM 耗电基线。

## 验证与剩余工作

已在 `android/` 运行并通过 `./gradlew --no-daemon -q -Pkotlin.compiler.execution.strategy=in-process :app:assembleDebug :feature:settings:testDebugUnitTest :platform:notify:testDebugUnitTest :core:hub:test`；设置、通知和 hub 分别通过 9、8、26 项单测。额外参数让 Kotlin 编译在 Gradle 进程内执行，避开沙箱不允许写入的 daemon 目录。新增测试覆盖通知偏好门禁，以及切网重协商和向下抖动。尚未连接真实 hub，也未做真机撤销、设备通知注册、锁屏隐私、厂商后台行为或深浅色界面验收。

本车道没有改外壳、导航、对话和收件箱装配文件。设置模块的最小入口改动包括 `SettingsEntries.kt`、`SettingsScreen.kt`、诊断页和复用 ROM 检测器所需的公开函数；通知模块新增偏好源接口，由 App DI 绑定。手机工具现已由 AppServices 绑定同一活动连接，并按开关、权限和前后台条件注册供给，真实多设备共存仍待验收。

## 平板与折叠屏接线（2026-11-01）

`SettingsScreen` 新增可选的 `onOpen: ((SettingsPage) -> Unit)? = null` 和 `onBack: (() -> Unit)? = null`。传入 `onOpen` 后，分类入口、hub 卡片以及配对和准备清单中的诊断入口都交给导航；手机数据入口也优先交给 `onOpen`。传入 `onBack` 后，子页返回按钮、完成操作和系统返回交给宿主，配对向导的内部返回处理不会拦截系统返回。未传回调时仍使用原来的内部切页和 `onPhoneData` 行为。

`SettingsNavigation.kt` 提供纯函数 `SettingsPage.toInitialPage()` 和 `settingsPageFromInitialPage(String): SettingsPage?`；反向转换遇到根页 `settings` 或未知字符串返回 null。

| SettingsPage | initialPage |
| --- | --- |
| Diagnostics | diag |
| Readiness | ready |
| Notifications | notify |
| PhoneData | phone |
| Appearance | style |
| Devices | devices |
| About | about |
| Pairing | pair |

分类中的配对入口新增 `SettingsPage.Pairing`，使用 `SettingsPageRoute`，没有复用 `PairingRoute`。已有配对深链仍可由原来的 `PairingRoute` 接收。

```kotlin
SettingsScreen(
    services = services,
    initialPage = route.page.toInitialPage(),
    onOpen = { navigator.navigate(SettingsPageRoute(it)) },
    onBack = navigator::back,
)
```

根页使用同一组 `onOpen` / `onBack` 回调，省略 `initialPage`。外壳需继续提供 `LocalDetailKey`：设置行按 `SettingsPageRoute` 高亮；Agents 的共用 `SlotRow` 覆盖主列表和空间页，按 machineId 和 sessionId 识别 ChatRoute、InteractionRoute 和 TerminalRoute；收件箱按飞书 feedId 或邮件 itemId 高亮。

阅读边距已补到 ChatScreen、LarkScreen、LarkDocumentReader、ChatLinkHost 的普通网页和 FullTextReader 的加载态；邮件详情和全文正文已由 JListPage 或 pagePadding 限宽，无须重复加边距。图片沿用父容器约束，代码和表格沿用可用宽度及内部滚动。

本次改动文件：core/navigation/Routes.kt；settings 的 SettingsScreen.kt、SettingsNavigation.kt、SettingsNavigationTest.kt 和本文；agents 的 AgentsScreen.kt、AgentSelection.kt、AgentSelectionTest.kt；inbox 的 InboxScreen.kt、InboxSelection.kt、InboxSelectionTest.kt、LarkScreen.kt、LarkDocumentReader.kt；chat 的 ChatScreen.kt、ChatLinkHost.kt、FullTextReader.kt。

指定五个模块共 85 项单测通过（navigation 2、settings 13、agents 22、inbox 19、chat 29），覆盖新增映射和详情选中规则。App 编译仍被宿主 `app/src/main/java/persona/android/ui/Root.kt:329` 的旧枚举穷举阻塞：需要用 `route.page.toInitialPage()` 替换旧的 `when`，并传入导航回调。该文件属于协调者，未修改。没有缺失的组件参数；未使用模拟器或真机，三套风格、双栏和桌面姿态的视觉验收由协调者完成。
