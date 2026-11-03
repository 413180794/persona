# 手机数据工具

`apple/Persona/PhoneTools/`：把 iPhone 能力包装成 MCP 工具，让 Claude 在电脑上操作手机数据。开关和授权在 设置 › 连接器 › 这台 iPhone（2026-11-01 以前叫「手机数据」）。

链路：Claude 调 `phone_*` 工具 → `personad mcp` → node.sock → hub → 在线的 iPhone（WebSocket `tool_call`）→
App 在本地查询/写入 → `tool_result` 原路返回。

工具的描述和 schema 由 App 注册（iOS `apple/Persona/PhoneTools/`，Android `android/platform/phonetools/`（`PhoneToolCatalog.kt`）），不在服务端的 `server/internal/prompts/mcp/`（`docs/prompts.md`「MCP 工具」）。

## 注册与开关

| 入口 | 作用 |
| --- | --- |
| `PhoneToolRegistry.all` | 全部工具（不看开关），测试和设置页用 |
| `PhoneToolRegistry.enabledSpecs()` | 只含用户打开的数据源，连上 hub 后 `register_phone` 发这个 |
| `PhoneToolRegistry.call(name:args:confirm:)` | 执行一次调用，永不抛错：失败变成 `isError=true` + 说明 |
| `PhoneToolRegistry.authorizationStatus(_:)` / `requestAccess(_:)` | 设置页显示状态、请求权限 |

每个数据源（通讯录 / 日历 / 提醒 / 位置 / 健康 / 照片 / 设备 / 剪贴板 / 通知）在设置里可单独关掉；关掉后
`enabledSpecs()` 里不再出现，模型看不到这些工具，调用也会被拒绝（防止旧会话继续调）。

## 工具一览

| 工具 | 需要权限 | 需确认 | 说明 |
| --- | --- | --- | --- |
| `phone_contacts_search {query, limit?}` | 通讯录 | 否 | 按姓名、公司、职位、邮箱、电话号码模糊搜索（号码可带空格/横线）；返回姓名、公司、职位、带标签的电话和邮箱、生日（没有年份时返回 `--MM-DD`） |
| `phone_calendar_events {start?, end?, query?, limit?}` | 日历（完全访问） | 否 | 默认今天 0 点到 7 天后；返回 identifier、标题、起止、全天标记、日历名、地点、备注前 300 字、参与人 |
| `phone_calendar_create_event {title, start, end, location?, notes?, all_day?, calendar?}` | 日历 | **是** | 新建日程，按名字选日历，默认日历兜底 |
| `phone_reminders_list {list?, include_completed?, limit?}` | 提醒事项 | 否 | 默认只列未完成；返回 id、标题、到期、优先级、所属列表、备注前 300 字 |
| `phone_reminders_create {title, due?, notes?, list?, priority?}` | 提醒事项 | **是** | 新建提醒，优先级 0/1–4/5/6–9 |
| `phone_reminders_complete {id}` | 提醒事项 | **是** | 标记完成（用 `phone_reminders_list` 返回的 id） |
| `phone_location_current {}` | 位置（使用期间） | 否 | 一次性定位，最多等 15 秒；返回经纬度、精度、海拔、时间、时区、反向地理编码地址 |
| `phone_health_summary {metrics?, days?}` | 健康（读） | 否 | 默认步数、近 7 天；指标 steps / distance / active_energy / heart_rate / resting_heart_rate / sleep / weight |
| `phone_health_write_sample {metric, value?, start, end?}` | 健康（写） | **是** | 写一条：steps（整数）/ distance（米）/ active_energy（千卡）/ weight（千克，一个时刻）/ sleep（一段睡着的时间，不要 value）；一段最长 24 小时、不能是将来；一次一条，不批量导入、不删数据 |
| `phone_photos_recent {limit?, days?, screenshots_only?}` | 照片（读） | 否 | 最近照片和视频的元数据：本地 id、时间、类型、尺寸、截图/收藏、时长、经纬度 |
| `phone_photo_get {id, max_size?}` | 照片（读） | 否 | 返回 JPEG 图片本身（长边默认 1280，质量 0.7）+ 一段元数据 |
| `phone_device_info {}` | 无 | 否 | 机型标识、iOS 版本、电量与充电状态、低电量模式、存储、网络类型、时区、语言、热状态 |
| `phone_clipboard_get {}` | 无（有系统粘贴提示） | 否 | 读剪贴板文本 |
| `phone_clipboard_set {text}` | 无 | 否 | 覆盖剪贴板文本 |
| `phone_notify {title, body, delay_seconds?}` | 通知 | 否 | 本地通知，可延迟最多 24 小时 |

返回统一是 pretty-printed JSON 文本（`ToolContent.text`），时间一律 ISO 8601 带本机时区偏移，日期字段用
`yyyy-MM-dd`。`phone_photo_get` 额外带一个 `ToolContent.image`。

## 确认流程

写操作（建日程、建提醒、完成提醒、写健康数据）带 `confirmation` 闭包，执行前 App 弹确认卡片，展示「新建日程『…』14:00 → 15:00」
「写入健康：体重 72.5 千克」这种摘要；用户点拒绝时工具返回 `isError=true`，内容是 `The user declined on the phone.`。确认只针对单次调用，
Claude 再调一次会再问一次。

读操作不打扰用户，但每一次调用都会在会话里留下 `tool_call` / `tool_result` 记录。

## 权限

系统权限在两个时机请求：用户在设置页点「授权」（`PhoneToolRegistry.requestAccess`），或者第一次真正用到该工具时。
通知是例外：`phone_notify` 第一次调用时自己弹授权框。其余工具遇到「尚未请求」只会返回错误、告诉模型去让用户授权，
不会在工具执行中途弹系统框（健康数据也只会在授予之后重新请求一次读权限，用来补上新增的类型）。已拒绝的权限只能
去 iOS 设置里改。

| 数据源 | Info.plist key | 授权方式 |
| --- | --- | --- |
| 通讯录 | `NSContactsUsageDescription` | `requestAccess(for: .contacts)`，完整或 limited |
| 日历 | `NSCalendarsFullAccessUsageDescription` | iOS 17+ `requestFullAccessToEvents()` |
| 提醒事项 | `NSRemindersFullAccessUsageDescription` | iOS 17+ `requestFullAccessToReminders()` |
| 位置 | `NSLocationWhenInUseUsageDescription` | `requestWhenInUseAuthorization()` |
| 健康 | `NSHealthShareUsageDescription`、`NSHealthUpdateUsageDescription`（+ entitlement） | `requestAuthorization(toShare: 五种写入类型, read: …)` |
| 照片 | `NSPhotoLibraryUsageDescription` | `.readWrite`，可能是 limited（只能读到用户选中的照片） |
| 通知 | 无（运行时授权） | `requestAuthorization([.alert, .sound, .badge])` |

写健康数据只写他在手机上确认过的那一条：确认卡片的摘要和写入的数据出自同一份参数解析（`HealthSample`，数值最多两位小数，确认卡上显示的就是写进去的），参数不对时卡片直接说「参数有误，不会写入」。写入的样本标成手动输入（`HKMetadataKeyWasUserEntered`）。写权限和读不一样，系统会告诉 App：以前只授权过读的（写入功能之前），第一次写时在他确认之后再弹一次系统的健康授权；被拒绝时返回错误，告诉他去 iOS 设置 › 健康 › 数据访问与设备 › Persona 里打开。

健康有一个 iOS 的硬限制：**系统永远不告诉 App 读权限有没有被授予**（防泄漏）。所以设置页显示的是「是否已经
请求过」，实际的拒绝会表现为查询结果为空。工具描述里已经写明这一点，模型不会把空结果当成「没数据」以外的结论。

安卓的 `phone_health_summary`（#278 做过，#632 合入集成分支时没带过来，重做见 #820）读 Health Connect，工具名、说明、参数和 iOS 一字不差（描述里
「读权限永远不报告给 App」这句也照留，hub 只存一份工具列表）。不同的地方：Health Connect 会告诉 App 哪些读权限给了，
所以设置页能显示「只允许了部分数据」，结果里没授权的指标列在 `not_granted`；一个都没授权时直接返回错误，而不是空结果。
安卓 14 起 Health Connect 是系统自带，8–13 要另装（没有 Google 服务的手机通常没有），这时工具返回「这台手机没有 Health Connect」。

## 实现要点

- 连接复用：每个账号不适用——这里每个工具自己建查询；EventKit 的 `EKEventStore`、`CNContactStore`、
  `HKHealthStore` 都是按需创建，避免跨 actor 传递非 Sendable 对象。
- 所有网络/系统回调转 async：用 `withCheckedContinuation` + 一次性 `SingleResume` 包装，保证回调被多次触发
  （Photos 的降质图 → 原图、HealthKit 的错误回调）时不会重复 resume。
- 定位、网络状态、通讯录枚举这类会阻塞或需要 delegate 的东西放在非 MainActor 的 helper 里
  （`OneShotLocation`、`NetworkProbe`、`ContactsTool.fetch` 走后台队列）。
- EventKit 的对象不是 Sendable，提醒事项在 callback 里就转成值类型 `ReminderRow` 再交回。
- 图片缩放用 `UIGraphicsImageRenderer`，保证长边不超过 `max_size`（默认 1280，最大 2048）。

## iOS 的限制

- **没有 API 读短信 / iMessage / 通话记录**（`MessageUI` 只能写不能读）。
- **没有 API 读备忘录、微信、其他 App 的数据**。要读只能靠「截图 + `phone_photo_get` 看图片」或用户手动贴到剪贴板。
- **App 必须在线**：hub 只能把 `tool_call` 发给已 `register_phone` 的 iPhone。App 在前台或刚切到后台时 WebSocket
  还在，能收到；被系统挂起后收不到，hub 会返回「iPhone 不在线，请打开 Persona」。
- **后台定位不可用**：只有「使用期间」权限，App 不在前台时定位会失败。
- **剪贴板会弹系统提示**：读别的 App 复制的内容时，iOS 会问「允许粘贴吗」，用户必须点允许。
- **健康数据只能按天汇总**：没有逐条记录查询；步数/能量是累计值，心率是当天平均/最小/最大，睡眠是每天睡着时长
  （`asleep` 系列，不含「在床上」）。
- **照片可能是 limited**：用户只授权部分照片时，`phone_photos_recent` 只返回选中的那些。
- **通知只能在 App 有执行机会时发**：延迟通知交给系统调度，会准时；但 Claude 必须在 App 在线时先调用成功。
- 返回内容（包括邮件/照片元数据）会进入 Claude 的上下文，等于交给模型处理。
