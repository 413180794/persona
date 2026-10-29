# 锁屏 / 灵动岛实时活动（Fleet）

整台手机只有一个实时活动「Fleet」：把所有需要你、在工作、做完未看的 Agent 汇总到锁屏和灵动岛。hub 算内容、经 APNs 推送；
App 负责开始活动、上报 token；Widget 扩展（`ios/JarvisWidgets`）负责画。设计依据见车道笔记（ChatGPT Codex / Sand 的活动分析），
这里只写实现和约定。

## 显示什么

| 阶段 `phase` | 条件 | 颜色 | 灵动岛紧凑态 |
| --- | --- | --- | --- |
| `needsYou` | 有待处理交互（提问 / 计划 / 授权 / 终端），或 herdr blocked | 红 | 左：红色气泡（跳动），右：数量 |
| `working` | 没有需要你，有工作中 | 琥珀 | 左：珊瑚四角星，右：最久那个的计时 |
| `done` | 只剩做完未看的 | 绿 | 左：绿勾，右：数量 |

- 不算进来：已看（idle）、离线、屏蔽的 Agent、`/loop` 等下次唤醒的；助手（对话 tab）只有在问你时才算。规则和 Agents 列表一致（`hub/liveactivity.go buildFleet`）。
- 条目顺序：需要你（最早的请求在前）→ 工作中（跑得最久的在前）→ 做完未看（最新的在前）；推送里最多 3 条，界面画 2 条，其余「+N 个」。
- 计时在手机本地走（`Text(timerInterval:)`），起点是 hub 记的这一轮开始时间（`turnTracker`），不为计时推送。
- 锁屏默认只显示数量和「有个授权等你」这类不带名字的一行；设置 › 通知 › 锁屏显示详情 打开后才显示 Agent 名字和内容（按设备存，`prefs.lock_details`，
  hub 推给每台设备时填进 `details`）。灵动岛只在解锁时可见，总是显示详情。
- 视觉：Jarvis 的状态色（不跟随界面风格，扩展读不到）、App 图标的珊瑚四角星；锁屏和展开态都限高约 160pt，所以最多两条。
- Apple Watch：不单独开发，`supplementalActivityFamilies([.small])` 给智能叠放一个单行版本（图标 + 「2 个需要你」+ 计时）。

## ContentState（`FleetState`）

Go `hub.FleetState` 和 Swift `FleetState`（`ios/Shared/FleetState.swift`）逐字段对应，`liveactivity_test.go TestFleetStateJSON` 和
`FleetStateTests` 两边钉住同一段 JSON：

```json
{"phase":"working","needsYou":0,"working":1,"unread":0,
 "items":[{"id":"<machine>/<session>","title":"…","place":"机器 · 空间","kind":"working","detail":"","since":1761700000}],
 "workingSince":1761700000,"details":false}
```

- **时间一律是 Unix 秒（数字）**：ActivityKit 用默认的 JSONDecoder 解推送里的 `content-state`，`Date` 会被当成 2001 年起的秒数。
- Swift 侧每个字段都宽松解码（未知的 phase / kind 不报错），hub 加字段不会让活动坏掉。
- `attributes-type` 是 `JarvisActivityAttributes`，`attributes` 是 `{"kind":"fleet"}`（push-to-start 用），两个名字都不能改。

## 推送（`hub/liveactivity.go`）

- 触发：hub 每次广播 `sessions` / `interaction` / `interaction_resolved` 时重算快照，和上次推的一样就不推。
- 合并：新的「需要你」立刻推（priority 10）；其他变化等 2.5 秒合并后推（priority 5）。不带 alert、不带 category（交互推送已经响过）。
- `stale-date` = 现在 + 15 分钟，内容没变也每 10 分钟重推一次续期；hub 断了活动会变「过期」，界面显示「等待 hub 更新…」（P3 的按钮也会隐藏）。
- 结束：没有可显示的超过 1 分钟，或只剩做完未看的超过 5 分钟，推 `end`（`dismissal-date` = 现在）并忘掉活动 token。
- 开始：有活动 token 就 `update`；没有（App 没开过、上一个结束了）而有 push-to-start token，就在「一轮」开始时推一次 `start`
  （从空 / 只剩做完的变成有需要你或工作中算新的一轮）。用户划掉活动后，这一轮不会再推开。活动超过 8 小时（系统上限）当作已经没了，下一轮重新开始。
- APNs：`apns-push-type: liveactivity`、`apns-topic: <bundle>.push-type.liveactivity`，目标是活动自己的 token（或 push-to-start token），
  环境跟设备注册时一样（Debug 构建是 sandbox）。410 / BadDeviceToken 时删掉那个 token。
- 每台设备一行 `live_activities`（`device_id`、`activity_id`、`push_token`、`push_to_start_token`、`started_at`），内存里有一份，每次变化写库。
- 开着 App 时 hub 还广播 `{"type":"fleet","fleet":{…}}`，App 自己更新活动（比推送快，模拟器上也能用）。

## App（`LiveActivityController`）

- 启动就监听 `pushToStartTokenUpdates`、`activityUpdates`（hub 推开的活动），每个活动监听 `pushTokenUpdates`（会轮换）和结束 / 划掉，
  都上报 `POST /api/v1/devices/{id}/live-activity`。
- 前台收到 `fleet` 事件或回到前台拉 `GET /api/v1/fleet`：有内容就开始（只有前台能 `Activity.request`）或更新；空了等 1 分钟再结束（轮次之间会闪）。
- 点活动打开 `jarvis://open?machine=…&session=…`（第一条，展开态每一行各自的会话）。
- 模拟器自动化（`-pairURL`）不上报 token，免得 hub 把真机的活动当成模拟器的。

## 验证

- 布局：`-showFleetSample needsYou|working|done`（`-fleetDetails YES` 看锁屏详情）用编造的数据开一个活动，不需要 hub。
  `simctl io screenshot` 拍不到灵动岛和锁屏，用 UI 测试 `LiveActivityShots` 拍紧凑 / 展开 / 锁屏三张（`docs/testing.md`）。
- 推送：`xcrun simctl push` 不支持实时活动；但模拟器能收真实的 APNs sandbox 推送。DEBUG 构建在系统日志里打印活动的 push token
  （`subsystem == "jarvis"`），在 hub 机器上 `JARVIS_LA_TOKEN=<token> JARVIS_LA_TOPIC=<bundle id> go test ./internal/hub -run TestManualLiveActivityPush`
  用 hub 的 APNs key 推一条 update，再用 `JARVIS_FLEET_SAMPLE=current` 拍下来（2026-10-29 实测通过：Go 生成的 JSON → APNs → 模拟器活动正确显示）。
- 真机才能验证的：push-to-start（App 被杀后由 hub 开活动）、更新预算、手表智能叠放。

## 坑

- 示例里先 `endAll()` 再 `request` 会撞上：正在结束的活动仍然报 `.active`，新的就没开出来。直接用 `apply`（有就更新）。
- 锁屏 / 展开态超过约 160pt 会被裁掉（顶部或底部），而且不报错：加内容前先拍图看。
- `Text(timerInterval:)` 会占满可用宽度，要给固定宽度，否则把旁边的文字挤成「3…」。
- UI 测试把模拟器锁屏后要解锁，否则下一次 `xcodebuild test` 报「test runner hung before establishing connection」。
