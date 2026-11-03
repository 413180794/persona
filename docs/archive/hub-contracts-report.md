# hub 契约核对

范围：`server/`，对应 HUB-S03、N39、N13、12.1、13.8；核对日期为 2026-10-31。

| 项目 | 核对结果 |
| --- | --- |
| 飞书回复与幂等（N39） | 普通发送已接收 `client_id`，本次补齐收件箱回复的接收与透传。两入口复用 `client_ops` 保存回执，同 ID 重试返回原状态及正文，不再调用上游；换参数或目标返回 409。旧客户端不带该字段时沿用原行为。 |
| 邮件详情（12.1） | 已满足。`GET /api/v1/mail/messages/{aid}/{uid}?mailbox=INBOX` 返回 `message`，包含收发件人、时间、`text`、`html`、`attachments`、`seen`。附件含 `filename`、`mime`、`size`。GET 不自动已读，页面打开后调用同路径 `/flags`，提交 `mailbox` 和 `seen:true`。附件下载不在现有契约内。 |
| 设备通知偏好（13.8） | 已有 `GET/PUT /api/v1/devices/{id}/prefs`，支持局部更新 `input` 和 `done:off\|long\|always`，返回 hub 的延迟与长任务配置。无需先注册 APNs。本次修复存库失败仍改动内存的问题。Android 通知应消费按 `c.has("ws_notify")` 门控的 `notify` 事件，避免绕过偏好自行从原始 interaction 发通知。 |
| 按设备注册工具（N13） | 已满足。0091 迁移以 `(device_id,name)` 存储工具；单设备替换和清空不会删除另一台的工具。旧 iOS 不传新字段时用鉴权设备 ID，Android 可带独立 ID 和平台；工具合并去重，调用仅选提供该工具的在线设备，优先真机、最近活跃连接。 |
| 会话列表操作（HUB-S03） | 已齐全：`GET/PUT /api/v1/pins`、`GET/PUT /api/v1/mutes`；`POST /api/v1/sessions/{mid}/{sid}/close`、`/rename`。置顶及屏蔽以 `machine/session` 为键；重命名接受 `tab_name` 或 `title`。空间另有 `/api/v1/machines/{mid}/spaces/{wid}/rename`。 |

本次只改 hub 实现、相关测试、`docs/protocol.md` 和本报告，未改 iOS。新增请求字段可选，HTTP 响应结构不变，无新增 WS 事件。复用现有数据库表，无新增迁移，0131 未占用。

实现文件为 `server/internal/hub/{reply_receipts,clientops,lark,inbox,inbox_triage,push}.go`，测试文件为同目录的 `reply_receipts_test.go`、`lark_test.go`。

验证已通过：

- 在 `server/` 执行 `go build -buildvcs=false ./...`。沙箱无法读取构建用的 VCS 状态，因此关闭 VCS stamping。
- hub 定向测试：`TestReplyReceiptsPersist`、`TestLarkSendClientID`、`TestDevicePrefsStorageFailure`、`TestDevicePrefsAPI`、`TestPhoneToolsDeviceRouting`、`TestClientOps`、`TestWSNotifyDone`、`TestWSNotifyInteractionAndAssistant`。
- mail 定向测试：`TestHTTPMessagesShape`、`TestHTTPFlagsAndErrors`、`TestGetMessage`。
- db 定向测试：`TestClientOps`、`TestDevicePrefs`、`TestPhoneToolsDevicePersistence`。
- 所有测试均使用 `-buildvcs=false -count=1 -p 1`；数据库测试使用环境提供的专用库，未跳过。`git diff --check` 通过。

结果不明的执行不会自动重发：已保存的上游错误原样返回；重启遗留且没有结果的执行返回 `202 state=unknown`，需要客户端核对。未连接线上 hub，未做手机端联调及性能实测。
