# 锁屏测试接口

客户端先在 `client_hello.features` 中声明 `lock_test`，再预约测试。hub 的 `hello.features` 也会声明该能力。已有客户端无需新增字段。

接口路径中的 `{id}` 必须是当前设备凭据的 ID，与 `device_sessions.device_id` 一致。它不是推送注册 ID，也不采用 `client_hello.device_id`。这些新接口要求设备凭据，主 token、临时配对 token 和其他设备的凭据均不能代为操作。

| 请求 | 请求体 | 返回 |
| --- | --- | --- |
| `POST /api/v1/devices/{id}/lock-tests` | `{"delay_seconds":30}`，字段可省略，默认 30，允许 1 到 3600 秒 | 测试记录，含 `test_id`、`due_at` 和 `status` |
| `GET /api/v1/devices/{id}/lock-tests/{test_id}` | 无 | 当前测试记录 |
| `POST /api/v1/devices/{id}/lock-tests/{test_id}/receipt` | `{"notification_shown":true}`，字段可省略 | 首次回报后的测试记录 |

到期时，hub 只选择这台设备已声明能力且有连接记录的一条连接；有多条时选择最近建立的连接。事件格式如下，`type` 沿用原有字符串类型，其余字段均使用独立前缀：

```json
{
  "type": "lock_test",
  "lock_test_id": "<test_id>",
  "lock_test_due_at": "<RFC3339>",
  "lock_test_sent_at": "<RFC3339>",
  "lock_test_title": "锁屏测试",
  "lock_test_body": "已收到测试通知，请回到应用查看结果。"
}
```

设备收到后展示本地通知，并调用回报接口。`notification_shown` 可记录客户端报告的展示结果；未提供时保持未知。它不能证明用户看到通知，也不能证明设备当时处于锁屏状态。重复回报返回首次结果，不覆盖时间或展示结果。通知测试由用户主动预约，不受普通任务通知偏好影响。

| `status` | 含义 |
| --- | --- |
| `scheduled` | 已落库，等待到期 |
| `sent` | 已记录一次发送尝试，尚无设备回报；不能视为送达成功 |
| `received` | 设备已回报，`received_at` 为 hub 收到回报的时间 |
| `unavailable` | 到期时没有符合条件的连接，不再补发 |
| `interrupted` | hub 启动时预约已经过期，测试被中断，不补发 |

hub 不按秒轮询数据库：派发器睡到最早一条 `scheduled` 测试的 `due_at`，预约新测试时立即重新定时；每次最多睡一分钟就复查，防止系统时钟跳变或别的进程写入的测试被漏掉（#2944）。

`device_session_id` 关联实际发送所选的 `device_sessions` 记录；未发送时省略。`sent_at`、`received_at`、`notification_shown` 未知时省略。hub 重启后保留未来预约；发送尝试与 WS 写入无法原子完成，因此 `sent` 但没有回报也可能是中断或写入失败，hub 不重发。记录保留 14 天，连接记录先被清理时关联字段置空。

连接汇总复用 `GET /api/v1/devices/{id}/sessions?since=<RFC3339>`，默认统计最近 24 小时：`summary.disconnects` 为窗口内非 `hub_stopped` 的连接断开记录数，`summary.longest_gap_seconds` 为连接区间合并后的最长离线时间，含截至查询时仍未恢复的时段。并行连接可能各记一次断开，但连接重叠不会制造离线时段；hub 重启造成的离线时段不计入最长值。

调度精度、后台存活和通知展示均未实测，需 Android 真机验收。服务端关键测试使用 `PERSONA_TEST_DATABASE_URL`，命令为 `go test -count=1 -p 1 ./internal/hub ./internal/db -run 'TestLockTest|TestDeviceSessions$'`。
