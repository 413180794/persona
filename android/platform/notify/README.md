# 通知接入说明

通知动作由 `core:domain` 的 `ApprovalPolicy` 决定，`NotificationPolicy` 只映射按钮文案、输入和提交内容。正向动作需要解锁，拒绝和打回可在锁屏输入原因。按钮使用系统通知样式，移除文案中的换行，颜色跟随系统主题。

`HubService` 只用 hub 下发的 `notify` 事件创建提醒。原始 `interaction` 仍由共享仓储更新状态，但不直接弹通知，避免绕过设备的输入偏好和会话屏蔽。完成提醒也只消费 hub 的决定。沿用共享连接的 `ws_notify` 能力声明，无需新增 HTTP 接口。

扩展的 `kind: alert` 使用高优先级「提醒」频道，受同一个 `input` 开关控制。有完整会话标识时，点开按会话的助手归属进入 Persona 或 Agents；没有会话时只展示消息，不创建会话跳转。每条扩展提醒独立展示，同一会话的不同提醒不会互相覆盖；关闭输入提醒、收紧锁屏偏好或退出配对时会一起撤回。

执行前刷新待处理请求，再检查有效期、动作和锁屏状态。缺失有效期兼容旧请求，非法有效期按过期处理。发送前同步保存请求摘要和随机操作编号，不保存答案、原因或选项。并发点按和进程重启后再次点按均不能重发；结果不明时提示用户到会话核对。记录保存在模块自己的 `notification_actions` 偏好文件中。

## 下一轮 App 接线

- 保留 `Notify.install(application, connection.events)` 和 `Notify.viewing(session)`，继续使用共享连接与当前可见会话。
- 冷启动和 `onNewIntent` 均用 `core:navigation` 的 `NotificationRoute.parse(uri)` 读取通知链接。`tab=persona` 回助手，`tab=agents` 回 Agents；使用 `chat` 打开对应会话。明确给出的 tab 应优先于旧返回栈的归属判断。
- 未拿到会话元数据时，通知不会猜测 tab。此时 `route.tab` 为 null，App 需按会话元数据确定归属后再打开，避免先进入错误的 tab。
- App 设置车道已通过 `DeviceSettingsModule` 绑定 `NotificationPreferencesSource`，与设置页共用当前配对的设备 ID 和受信 HTTP client。展示前刷新本机 hub 偏好：`input=false` 拦截交互提醒，`done=off` 拦截完成提醒；`long/always` 的条件继续由 hub 决策。读取失败或当前设备服务尚不可用时，不展示业务提醒。

## 验证范围

通知模块单元测试覆盖统一策略夹具、有效期、刷新后已处理、锁屏限制、并发点按、响应丢失后的防重发、hub 通知门禁及路由归属。持久存储的进程重启行为、深浅色按钮显示、锁屏、Doze、厂商后台恢复仍需真机验证，性能未实测。

## 常驻汇总通知（相当于 iPhone 的实时活动）

前台服务的常驻通知（编号 1，「实时状态」频道）有东西可看时显示 hub 的 `fleet` 汇总，否则是连接行（「已连接 hub / 连接中… / 等待网络 …」，`ongoingTitle`）。规则和 iOS 的实时活动一致（`core:domain` 的 `FleetSummary` / `FleetText`，小组件和磁贴共用）：

- 标题是「N 个需要你」/「N 个在工作」/「N 个做完了」，副标题是其余数量（「3 个在工作 · 1 个做完了」）；「锁屏显示详情」开着时正文是第一个 Agent「名字 · 在做什么」，有按钮时则是按钮要处理的那个授权或计划（更早的提问不会站在别人的「允许」上面，`OngoingStatusTest.theBodyAboveTheButtonsIsTheRequestTheySettle`），展开后最多三行「名字 · 在做什么（机器 · 空间 › 标签）」，再多一行「+N 个」（关着时见下面「锁屏」）。「在做什么」：授权「授权：<摘要>」、计划「计划：<首行>」、提问是问题、终端「终端等你处理」、工作中、做完未看是最后一句。
- 只剩工作中时，从跑得最久那一轮开始走计时（chronometer）。
- 第一个授权有「拒绝 / 允许」，第一个计划有「查看 / 批准」：按 `ApprovalPolicy` 的锁屏入口取动作，批准是 `acceptEdits`，「允许 / 批准」要先解锁，「拒绝」不用。按钮走和「需要你处理」通知相同的 `ActionReceiver` + `NotificationActionExecutor`（刷新、有效期、解锁、每个请求一次的持久认领），只是不另发「核对中」提示；成功后收回同一请求的「需要你处理」通知，失败照常给结果提示。「查看」直接打开对话。
- 点通知：总数正好一个时进它的对话，否则进 Agents。
- 退回连接行：什么都没有、只剩做完未看满 5 分钟（hub 的 `fleetDoneLinger` 只结束 iOS 的活动，`fleet` 广播不会变空，所以手机自己算）、hub 没连上（不显示旧摘要）。
- 锁屏（2026-11-02 协调者定，按更严的来）：「锁屏显示详情」**关着**（默认）时，通知正文和展开列表也只用泛化文字，不放 Agent 名字、位置和请求详情：标题「4 个需要你」，正文「有个计划等你批准」，副标题「2 个在工作 · 1 个做完了」，展开是按种类数的请求（「有个计划等你批准 / 2 个授权等你 / +1 个」）；只剩工作中或做完未看时只有标题和数量。这样即使系统设成「锁屏显示全部内容」（很多手机的默认），锁屏上也看不到名字。通知仍是 `VISIBILITY_PRIVATE`，公开版本只有数量和「有个授权等你」。按钮照旧（「拒绝 / 允许」「查看 / 批准」本身不带内容）。**开着**时私有版本显示前三个 Agent 的名字、在做什么和位置，通知仍是 `VISIBILITY_PRIVATE`、公开版本只有数量：锁屏上显示哪个由系统的「锁屏显示敏感内容」决定，系统设成隐藏敏感内容时锁屏只有数量（原来开着时整条公开，盖过了系统的这项设置，2026-11-02 统一验证时改）。规则在 `OngoingStatus.from(details = …)`，`OngoingStatusTest.withoutLockScreenDetailsNoNameOrRequestShowsAnywhere` 守着。

数据来自 `FleetFeed`：收 WS 的 `fleet` 事件，连接每次连上后再 `GET /api/v1/fleet` 一次（hub 只在变化时推，刚连上的手机会一直是 0），最后一份按配对存在 SharedPreferences `fleet`，给进程没跑时的小组件和磁贴读；取消配对时清掉，换了配对不显示上一个 hub 的。`FleetSource`（HTTP 和配对标识）由 App 的 `FleetModule` 提供。只按可见内容去重、2.5 秒合并（`throttleOngoing`），用量等不显示的字段变化不触发刷新。

「锁屏实时状态（Live Updates）」默认开启，复用 hub 按设备保存的 `live_activity`。有活动且开关、系统均允许时，Android 16 申请 promoted ongoing 并给状态栏胶囊（需要你 💬N、工作中 ⚙N、做完 ●N），Android 17 及以上换成 `MetricStyle` 的三项计数。低版本、关闭开关或系统调用失败时保留普通常驻通知。系统是否实际提升、国产系统的胶囊和锁屏隐私效果仍需真机验收。

## 设备偏好与退出

`Notifier` 读取同一偏好源的 `lock_details`。关闭时，交互与完成提醒使用脱敏公开版本，常驻通知只公开连接状态和计数；开启时允许系统展示详情。偏好收紧或配对清空后撤掉旧提醒，避免沿用旧内容。系统用户设置仍可进一步限制显示，锁屏效果尚未真机验收。

`Notify.disconnect()` 供统一退出调用，停止常驻服务与 watchdog、撤回提醒并清可见会话。App 在断开、撤销、401 失效和重新配对时调用；设备偏好源按配对代次和选路切换，继续复用唯一 WebSocket。
