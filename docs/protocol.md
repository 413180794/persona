# 协议（前后端契约）

本轮核对：2026-10-31，基于提交 `f3a13fdb7672927197cb8ac38bccb9c65671677b` 的 `server/internal/hub/` 路由、事件发送和数据库记录。以下超时、保留期和数量限制均为协议或配置，实际性能未实测。

所有 JSON 字段 snake_case。时间为 RFC3339（UTC）。HTTP `listen` 默认 `:18660`；TLS 通过 `public.tls_listen` 显式开启，提供 HTTPS / WSS。公网优先 IPv6，配置和放行方法见 [network.md](network.md)。

## 鉴权

- 优先使用 `Authorization: Bearer <token>`。当前实现未收紧 URL token：没有 Authorization 值时，所有鉴权 API（含 WebSocket）仍可回退到 `?token=`，不只限回环或 WS。客户端应使用请求头，避免凭据进入 URL。
- hub.json 的 `token` 是主 token，只接受回环 / 内网等受信任来源；公网即使用 TLS 也返回 `403 master_token_local_only`。公网应使用临时配对 token 或设备 token。默认拒绝公网明文 API，返回 `403 tls_required`，仅 `GET /api/v1/health` 例外。
- 每台设备自己的凭据：`POST /api/v1/auth/tokens {"name":"<设备名称>","kind":"phone"}` → `{id,token,role,name}`。设备 token 以 `jd_` 开头，hub 只存哈希；`kind: watch` 得到 `answer`，其他 kind 得到 `full`。`full` 可以创建设备 token；`answer` 能读、回答交互、发消息、标已读 / 中断、处理收件箱和注册推送，不能改设置或注册手机工具，其余写操作一律 403，WebSocket 上也不能注册成手机执行 `phone_*` 工具。新 App 配对后立刻换一个自己的 token，之后只用它；配对的手表再换一个 `kind: watch` 的，手表只拿到这个。
- `GET /api/v1/auth/tokens` 返回 `{tokens,current}`，`DELETE /api/v1/auth/tokens/{id}` 撤销凭据、清缓存（认证结果缓存 30 秒，撤销时清掉）并断开该设备的 WebSocket；App 在「设置 › 设备」里管理。旧 hub 无此接口时，旧客户端仍按原配对流程使用凭据。
- 电脑链路的凭据（[link.md](link.md)）：`POST /api/v1/auth/tokens {"kind":"link","machine":"<hub.json 里 kind 为 link 的机器>"}` → `role: link`，
  只能开 `/api/v1/link` 和 `/api/v1/link/stream`，其他接口 403；列表里带 `machine_id`。用给这台生成的配对码（下文 `{"machine"}`）兑换时，
  kind 和 machine 由配对码决定，请求里写别的机器返回 403。
- hub 互联（[mesh.md](mesh.md)）：另一台 hub 的凭据 kind / role 都是 `peer`（`machine_id` 是对方的 hub_id），只能开 `GET /api/v1/peer`；
  互联码 `jx_…` 只能 `POST /api/v1/peer/pair`；别的接口一律 403。组里只有**中心 hub** 认 App 的凭据：成员把 App 的请求原样（带 `Authorization`）
  转给中心，成员加入时把它签过的凭据（hash）交给中心，所以配对在成员上的设备照常能用。中心连不上时成员回
  `503 {"code":"center_unreachable","center":{"hub_id","name"}}`。
- 公网鉴权失败按 IPv4 单地址、IPv6 /64 计数，封禁检查在查库前；`429 rate_limited` 带秒数形式的 `Retry-After`。内网 / VPN 不计数，参数见 [网络配置](network.md#hubjson-网络配置)。
- 每个响应带 `X-Request-Id`；请求可带 `X-Client-Request-Id`（4–32 位十六进制 / `-`），hub 就用它（`docs/server.md`「运维」）。

### 健康检查与能力位

`GET /api/v1/health` 不需要鉴权。公网来源只返回：

```json
{"ok":true,"hub_id":"<hub UUID>","hub_name":"家里","tls_port":18662,"fp":"<SPKI SHA-256 base64url>"}
```

内网 / 回环等受信任来源另外返回 `version` 和 `features`。来源按连接地址判断，与是否带 token 无关；公网 HTTPS health 也会裁剪。所有来源都不返回候选地址。未启用 TLS 时 `tls_port` 为 `0`，`fp` 为空字符串。

受信任来源的健康检查还返回 `channel`（`release` / `docker` / `dev`）、`build`（UTC `YYYYMMDDHHMM`，没有是 0）、`protocol`（App ↔ hub 的大版本号）、`min_app_build`（它能配的最老的 App，`docs/hub-update.md`「兼容」），以及 `update.confirmed`（进程是否已确认完一次更新：没有更新要确认时为真，看门狗等它）。`channel` 和 `build` 说明这个 hub 怎么构建、能不能自己更新；WS `hello` 带同样的字段。

`hub_name` 是这台 hub 给人看的名字（hub.json 的 `name`，[config.md](config.md)）。`hub_id` 是持久化在数据库 `settings` 的 UUID。客户端用它区分 hub，并核对切换候选后仍连接到同一实例；`fp` 必须来自可信扫码或已鉴权的可信通道，不能信任任意公网 health 回包来建立钉扎。

鉴权后的 WS `hello.features` 总是提供服务端能力。当前 `proto.Features` 包括：

- P0 新增：`tls`、`endpoints`、`client_hello`、`heartbeat`、`app_ping`、`error_codes`、`client_ops`、`ws_notify`、`lock_test`、`android_update`。
- 已有：`client_msg_id`、`send_refusals`、`screen_dialog`、`resumable_interactions`、`secret_prompts`、`request_id`、`terminal_stream`、`terminal_control`、`terminal_scroll`、`terminal_click`、`file_upload`。
- 另有：`elicitation`（MCP 表单和链接）、`model_configuration`（按用途配置模型）、`plugins`（[插件](plugins.md)）、`desktops`（桌面端登记，见下文 `register_desktop`）。

能力位表示实现支持；`tls` 不代表已经开启监听，要同时看 `tls_port` / `fp`。新客户端遇到旧 hub 时按能力位退回旧行为。机器列表的 `node_version` / `node_mismatch` 用来提示 node 与 hub 的部署版本是否一致（那台电脑上的 personad 不是 hub 这次部署的，`deploy.sh` 只部署了一部分；hub 连上时也记一行日志）。新字段都是可选的，App 目前靠字段在不在判断；以后有「旧 hub 会误解」的新请求时先查这里。

`POST /api/v1/machines/{id}/deploy`：hub 把自己这次部署的 node（`deploy.sh` 留在 hub 机器的 `~/.persona/bin/nodes/jarvisd-<os>-<arch>`）经 ssh 装到那台电脑（正走 tailcat 时经 tailcat 发给常驻 node，它换掉自己后退出、由 launchd / systemd 重启），杀掉常驻 node（launchd 用新二进制重启；ssh 里的 `launchctl kickstart` 不总生效）、重新连接，后台做，立即回 `{ok}`（不能开始时 409 + 原因）。过程在机器列表里：`deploying`（进行中）、`deploy_error`（上一次为什么没成）；成功后 `node_mismatch` 消失。App 在「设置 › 机器」里提示并给一个「重新部署」按钮。

### 临时配对

`POST /api/v1/auth/pair-codes` 只接受**回环来源 + 主 token**，无须请求体；请求体 `{"machine":"<id>"}` 生成给这台 link 机器的
jarvis-link 用的配对码（返回多一个 `machine`，`pair_url` 是 `jarvis://link?…&machine=<id>`，只能兑换成这台的链路凭据，别的请求都 403；
机器不存在或不是 link 返回 400，hub 没开 TLS 返回 409）。设备 token、临时配对 token 或非回环来源均不能创建配对码。成功为 HTTP 200，返回：

```json
{
  "token":"<临时 token>",
  "expires_at":"<RFC3339 UTC>",
  "url":"http://hub.example.com:18660",
  "hub_id":"<hub UUID>",
  "fp":"<SPKI SHA-256 base64url>",
  "tls_port":18662,
  "alt":["https://hub.example.com:18662"],
  "pair_url":"jarvis://pair?..."
}
```

响应带 `Cache-Control: no-store`。临时 token 为 `jp_` 加 32 字节随机值的十六进制编码，数据库只存哈希。有效期 10 分钟，可作为完整的 `full` Bearer 访问 API / WS，以兼容旧 iOS 先读机器、连 WS、注册推送再换凭据的流程。

它只能通过 `POST /api/v1/auth/tokens` **兑换一次**设备 token，重复兑换返回 `401 invalid_token`。兑换后其他 API 仍可使用至 `min(原到期时间, 兑换时间 + 2 分钟)`；到期后失效，已建立的临时 token WS 也会被取消。有效期与宽限均为协议策略，不是性能实测。新设备配对成功后，hub 尝试发配对通知；推送不可用时记日志。

`personad hub pair` 调用上述本机接口，打印临时链接和二维码；运行中的 hub 必须可经 IPv4 回环 HTTP 访问。`--legacy` 才离线打印主 token 的旧格式。现有 iOS 忽略新参数，可继续在内网扫默认临时码，不必改用 `--legacy`。

二维码保持 `url` / `token`，附加字段遵循 redesign/hub-changes.md 1.3：

```text
jarvis://pair?url=<兼容地址>&token=<临时 token>&hub=<hub_id>&fp=<指纹>&tls=<TLS 端口>&alt=<HTTPS 候选>&alt=<HTTPS 候选>
```

这是结构示意，实际参数值逐项 URL 编码，顺序不重要。

| 参数 | 格式 |
| --- | --- |
| `url` | `public_url`；旧 App 继续使用它，因此应保留可达的内网 / VPN 地址 |
| `token` | 临时 Bearer token；`--legacy` 时为主 token |
| `hub` | hub UUID |
| `fp` | SHA-256(SubjectPublicKeyInfo DER)，base64url 无填充，43 字符；非整张证书的哈希 |
| `tls` | TLS 监听端口，十进制 |
| `alt` | 完整 HTTPS URL，可重复；当前最多两个，按候选顺序选取并排除 `lan` |

未启用 TLS 时二维码省略 `fp` / `tls`；无合适候选时省略 `alt`。API 响应仍有对应字段（空字符串、0 或空数组）。换证书保留同一把密钥不改变 `fp`；换密钥需要重启 hub 并让设备重新可信配对，当前没有 `fp_next`。

### 候选地址

`GET /api/v1/endpoints` 需要鉴权，返回 `{hub_id,tls_port,fp,endpoints}`。每个 endpoint：

```json
{"url":"https://[2001:db8::1234]:18662","kind":"ipv6","port":18662,"scheme":"https","seen_at":"<RFC3339 UTC>"}
```

地址为文档示例。`kind` 为 `ipv6` / `name` / `lan` / `ipv4`，按此顺序排列；同类 HTTPS 优先。`scheme` 为 `https` / `http`，`seen_at` 是采集时间，不是可达性证明。候选合并 `public_url`、`public.advertise` 和符合条件的本机地址；公网 IPv4 只从显式配置发布。TLS 关闭时自动候选使用 HTTP，客户端不得因此向公网明文发送 token。

WS `hello` 同样携带候选。地址列表变化时发送 `{"type":"endpoints","endpoints":[...]}`，只发给本连接在 `client_hello.features` 声明了 `endpoints` 的客户端；仅 `seen_at` 改变不会触发事件。事件是完整候选列表，可为空，客户端应替换旧列表。

### 错误体

主 API 的公共错误处理保留 `error` 文本，新增稳定的 `code`；`retryable` 和 `delivered` **可选**，只有能判断时才给出。主路由的 404 / 405 也返回 JSON；邮件等独立子处理器和流式响应仍按各自协议处理，不能假设它们全部带新字段。

```json
{"error":"invalid token","code":"invalid_token","retryable":false,"delivered":"none"}
```

`delivered` 为 `none` / `unknown` / `yes`，缺省不等于未执行；尤其普通超时不会凭空补 `none`。`retryable` 缺省也不表示可以自动重试。node 返回的 409 细节继续平铺（如 `draft`、`clearable`），不包进新的 `data`。

| code | 常见 HTTP 状态 | 含义 |
| --- | --- | --- |
| `tls_required` | 403 | 公网明文 API 被拒；`delivered: none` |
| `master_token_local_only` | 403 | 公网不能用主 token；`delivered: none` |
| `invalid_token` | 401 | 凭据无效、过期、撤销或临时 token 重复兑换；`retryable: false`、`delivered: none` |
| `forbidden` | 403 | 权限或配对码来源不满足要求；`retryable: false`、`delivered: none` |
| `rate_limited` | 429 | 鉴权失败来源被封禁，按 `Retry-After` 等待 |
| `bad_request` | 400 | 参数 / JSON 错误；`retryable: false` |
| `not_found` / `method_not_allowed` | 404 / 405 | 资源不存在 / 方法不支持；`retryable: false` |
| `gone` | 410 | 交互已不再待处理等；`retryable: false` |
| `conflict` | 409 | 未指定更具体错误码的冲突 |
| `input_draft` / `dialog_open` / `prompt_changed` | 409 | 发送或回答被明确拒绝；`retryable: false`、`delivered: none`，见下文 |
| `op_id_reused` | 409 | 操作 ID 已用于不同设备、目标或参数；不得换 ID 盲目重试 |
| `node_outdated` | 409 | node 不支持请求的功能，如文件上传 |
| `too_large` | 413，或 node RPC 映射的 409 | 请求 / 文件超过限制 |
| `timeout` | 408 / 504，node 调用常为 502 | 超时；执行结果不能仅靠这个码判断 |
| `node_unreachable` | 502 / 503 | node 离线或连接中断；明确离线时 `retryable: true`、`delivered: none`，连接中断未必如此 |
| `center_unreachable` | 503 | 连的是互联组里的成员 hub，组的中心 hub 现在连不上（带 `center: {hub_id, name}`）；成员不会自己接管，换一台或等中心回来（[mesh.md](mesh.md)「中心」） |
| `internal` | 5xx | 其他内部错误 |

表格列出公共处理器和主要业务码；node 的其他明确 RPC 错误码也会透传，通常映射为 409。客户端应以 `code` 分支，未知码保留 `error` 展示，旧 hub 只有 `error` 时按旧行为处理。

## 操作回执（client_ops）

服务端声明 `client_ops` 后，客户端可在以下写请求带 `X-Jarvis-Op-Id: <UUID>`：

| 操作种类 | 路径 |
| --- | --- |
| `message` | `POST /api/v1/sessions/{mid}/{sid}/messages` |
| `start` | `POST /api/v1/sessions` |
| `resolve` | `POST /api/v1/interactions/{id}/resolve` |
| `interrupt` / `close` / `rename` / `modes` | `POST /api/v1/sessions/{mid}/{sid}/interrupt`、`/close`、`/rename`、`/modes` |

UUID 忽略大小写。hub 将操作种类、路径、规范化查询参数和 JSON 参数生成摘要，再原子认领 ID；重复请求只有同一设备、目标和参数才复用回执，冲突返回 `409 {"code":"op_id_reused"}`。消息请求会把 `client_msg_id` 设为操作 UUID，覆盖原 body 的同名值。没有该请求头时保留旧行为，包括旧 iOS 的非 UUID `client_msg_id` 和 `sent_messages` 去重。

`GET /api/v1/ops/{id}` 返回记录 `{id,device_id,kind,target,digest,state,result,created_at,updated_at}`。`target` 含 `path` / `query`；`result` 已记录时为 `{status,body,content_type?}`，其中 `body` 是原 HTTP 响应字节的 base64。不存在或当前设备凭据不属于该记录时返回 404；主 token 没有设备 ID，查询不受设备归属过滤。

| `state` | 含义与重试行为 |
| --- | --- |
| `pending` | 认领后执行尚未完成；重复写请求等待回执，等待配置到期后返回 `202 {"state":"pending"}` |
| `done` | 成功回执已落库；重复写请求重放原 HTTP 状态和正文，不再执行 |
| `failed` | 已确定失败并保存回执；重复写请求仍重放原结果 |
| `unknown` | 执行结果不明；普通操作重复请求返回 `202 {"state":"unknown"}`，需查询或人工核对，不能认作成功或自动重发 |

普通操作的 HTTP 400 / 409 通常记录为 `failed`，其他错误若不能证明未执行则记录为 `unknown`。hub 启动会将遗留 `pending` 标为 `unknown`，不重新执行。完成后即使 HTTP 调用方已断开仍尝试落库；落库失败返回 503 和 `state: unknown`。回执按创建时间保留 30 天，这是保留策略，不能保证超出窗口仍可去重。

飞书发送和收件箱回复另以 body 的 `client_id` 派生回执，见下文「飞书 API」；同样复用 `client_ops`，但已有上游错误响应会原样重放。不要将这种响应处理规则套到所有普通操作。

新建会话带操作 ID 时，hub 会在调用方断开后继续完成已受理的启动并保存回执。同 ID 重放不会再次启动；已有 HTTP 结果即使标为 `unknown` 也原样返回，没有结果的遗留操作仍返回 `202 {"state":"unknown"}`。Android 在发请求前持久保存 ID 和参数，响应丢失时查询该 ID；404 或结果不明均不能证明未启动，继续保留待确认记录，不再 POST。只有明确失败的回执才允许修改表单后新建操作。未带请求头的客户端沿用原行为。

## 设备连接与锁屏测试

### 连接记录

`GET /api/v1/devices/{id}/sessions?since=<RFC3339 UTC>` 返回 `{sessions,summary}`，默认查最近 24 小时，最早裁到最近 14 天；未来或非法时间返回 400。`{id}` 使用设备凭据 ID，与推送注册 ID、客户端自报的 `client_hello.device_id` 区分。该查询走现有通用鉴权，当前未额外限制只能查本机；锁屏测试接口则严格校验当前设备。

每条 session 为 `{id,device_id,connected_at,disconnected_at,reason,source,heartbeat_interval}`。`id` 是数据库数字 ID，仅用于 HTTP；连接尚未断开时 `disconnected_at` 为 null、`reason` 为空。`source` 只保存 IPv4 /24 或 IPv6 /64 前缀，心跳间隔为当前协议配置，不能当作实测发包频率。

- 只有设备 token 的 WS 建立连接记录；主 token 和临时配对 token 不记录。
- `reason` 为 `closed`、`pong_timeout`、`error`、`replaced` 或 `hub_stopped`。hub 启停修复未结束记录时标 `hub_stopped`，不假定能恢复准确崩溃时间。
- `summary.disconnects` 统计窗口内非 `hub_stopped` 的断开记录数；并行连接可分别计数。`summary.longest_gap_seconds` 对重叠连接区间合并后计算最长离线，含截至查询时仍未恢复的间隔，排除 hub 停止造成的间隔；首次已知连接之前的未知时段不推算。
- 记录保留 14 天。连接汇总反映 hub 的观测，不能单独证明手机锁屏或系统杀进程。

### 锁屏测试

服务端能力为 `lock_test`。下列接口的 `{id}` 必须等于当前设备凭据 ID；主 token、临时配对 token 和其他设备凭据均不能代操作。

| 方法 | 路径 | 请求与响应 |
| --- | --- | --- |
| POST | `/api/v1/devices/{id}/lock-tests` | `{delay_seconds?}`，默认 30，允许 1–3600 秒；可不带 body。返回测试记录 |
| GET | `/api/v1/devices/{id}/lock-tests/{test_id}` | 返回当前测试记录，不存在时 404 |
| POST | `/api/v1/devices/{id}/lock-tests/{test_id}/receipt` | `{notification_shown?}`，可不带 body；首次回报后返回记录，重复回报保留首次结果，未发送或无对应测试时 409 |

记录为 `{test_id,device_id,created_at,due_at,status,device_session_id?,sent_at?,received_at?,notification_shown?}`。`device_session_id` 为实际选择的连接记录数字 ID。到期后只选择本设备已声明 `client_hello.features: ["lock_test"]`、有连接记录且最近建立的一条连接，发独立事件：

```json
{"type":"lock_test","lock_test_id":"<测试 ID>","lock_test_due_at":"<RFC3339 UTC>","lock_test_sent_at":"<RFC3339 UTC>","lock_test_title":"锁屏测试","lock_test_body":"已收到测试通知，请回到应用查看结果。"}
```

新增 payload 字段均带 `lock_test_` 前缀，避免与旧 iOS Envelope 类型冲突。客户端展示本地通知后主动回报；`notification_shown` 缺省表示未知，true 也不能证明用户看到了通知或设备当时锁屏。测试由用户主动预约，不受普通通知偏好控制，不要求 `ws_notify` 能力。

| `status` | 含义 |
| --- | --- |
| `scheduled` | 已落库，等待预约到期 |
| `sent` | 记录了发送尝试，尚无设备回报，不能视为送达 |
| `received` | hub 已收到设备回报 |
| `unavailable` | 到期没有符合条件的连接，不再补发 |
| `interrupted` | hub 重启时预约已过期，不补发 |

hub 重启保留未来预约；发送记录与 WS 写入无法原子完成，所以 `sent` 且无回报也可能是中断或写入失败，不会自动重发。记录保留 14 天，连接记录清理后关联可为空。预约延迟是配置，调度精度、后台存活、通知实际展示均未实测。

## Android 安装包

两条接口都需要鉴权，能力位为 `android_update`：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/app/android/latest` | 原样返回已发布 `latest.json`；发布脚本写入 `{version_code,version_name,min_sdk,notes,sha256,size}` |
| GET | `/api/v1/app/android/apk` | 按清单 `sha256` 读取 APK；MIME 为 `application/vnd.android.package-archive`，附件名 `jarvis.apk`，ETag 为带引号的摘要，支持 ServeContent 的范围与条件请求 |

发布文件位于 hub 配置目录的 `apps/android/`，APK 按 `<sha256>.apk` 命名。未发布返回 404，清单摘要无效返回 500。客户端应校验大小与摘要、保持同源下载，安装交给系统确认；同包名同签名升级仍需发布产物后真机验收。

## Codex 来源与能力

外部 Codex 会话继续使用 `(machine_id, session_id)`。固定 legacy home 保持既有 raw thread ID，其他 canonical home 的 thread 使用 `c0de-` 加确定性摘要的 opaque ID；PID、endpoint 或连接代际变化不换会话 ID。`source_thread_id` 是原 thread ID，不可将 opaque ID 放进 `claude --resume`。

Session 新增可选字段：`source_kind`（`codex_history` / `codex_app_server` / `identity_conflict`；Agent 运行时的会话是 `runtime`，见「Runtime ↔ node」）、`source_thread_id`、`source_instance_id`、`parent_thread_id`、`read_only_reason`、`attached`、`capabilities`（read/send/interrupt/approve/terminal/attach 六个布尔值）。旧来源缺这些字段维持兼容，新来源缺能力默认拒写。没有终端能力就不显示终端/按键/关闭窗格。

`POST /api/v1/sessions/{mid}/{sid}/attach`：用户要求实时接入，node 只对原实例仍 loaded、owner 唯一的 thread 请求 resume，且不带运行配置覆盖。发现/普通读历史不调用 resume。加入许可按 thread/稳定实例记在 settings；断线重验 owner，不自动冷恢复或换实例。loaded/list 与 resume 不原子，不能承诺中间没有 unload 竞态。

外部发送同样用 `/messages`，必须带 `client_msg_id` 并成功登记 Postgres 去重记录；不能在存储不可用时无保护地发出。未知发送结果（包括可能发生在 admission 后的通用 server error）返回 `delivered: unknown`，不能自动重发或将 steer 失败改成 start。发送记录不按一天过期，避免恢复很久以前的 outbox 后重复执行；只有明确未发送的记录可忘记。

审批 ID 含 machine/instance/connection generation/thread/原始带类型 request ID。手机明确回答才给原连接回复；未知请求、取消和断线不自动发 error/decline。`serverRequest/resolved` 只将卡片记 dismissed 并撤下，不生成批准。外部 interrupt 不清理所有后台终端；父管子 thread 的直接驱动尚未开放。

## 模型供应商与用途配置

原 `/api/v1/launchers` 保持兼容，供应商新增 `kind`（`launcher` / `api`）和 `api`（`messages` / `responses`）；API-only 不下发给 node。

- `GET /api/v1/model-config` → `{purposes, providers}`。
- `GET /api/v1/model-config/purposes/{id}` → `{purpose, providers}`。
- `PATCH /api/v1/model-config/purposes/{id}` 接收 `{revision, provider?, model?, effort?}`；缺失不改，`null` 清除覆盖，revision 冲突返回 409。成功返回同单项 GET。
- 用途的 `fields`、`provider_ids` 限定可编辑参数和兼容供应商；`override` 与 `effective` 分开，`inherited_from` 标记来源，`status` 为中文状态说明。真实任务/插件使用 `task.<id>` / `plugin.<id>`。

供应商响应只带描述与 `has_key`，不返回秘密；具体默认、执行边界和兼容规则见 `docs/model-config.md`。

## 数据类型

```jsonc
// Machine。kind：local | ssh | link；link 的机器在 jarvis-link 连着时多一个 "link":{"version","address","since"}（docs/link.md）
{"id":"laptop","name":"笔记本","kind":"ssh","ssh_host":"<SSH 配置别名>","online":true,"error":"",
 "hostname":"<机器主机名>","os":"darwin","arch":"arm64","herdr_version":"0.8.0","claude_version":"2.1.310",
 "last_seen":"...",
 // 怎么连的（docs/server.md「tailcat 打洞」）：transport 是 local | ssh | tailcat（离线时没有）；tailcat：配了 tailcat 地址；
 // tailcat_path 只在走 tailcat 时有：direct（打洞成功，点对点）| relay（经 DERP 中继）。旧 hub 没有这几个字段
 "transport":"ssh","tailcat":true,
 // 引擎（下面「引擎」）：用户给这台机器选的终端宿主（存下的选择，不是 node 回报的），hub 总是给出（没选过是 herdr）；engines 是 node 在这台机器上
 // 找到的（离线时为空数组）。旧 hub 没有这两个字段
 "engine":"herdr",
 "engines":[{"id":"herdr","name":"herdr","available":true,"version":"0.9.3"},
            {"id":"herdr-core","name":"Herdr App","available":false},
            {"id":"herdr-both","name":"herdr + Herdr App","available":false}],
 // 系统信息（docs/machines.md）：node 最近一次连上时报的，存在 hub 的数据库里，离线也有；旧 node 没有这个字段
 "system":{"os":"darwin","distro":"macos","like":"","name":"macOS 26.1","version":"26.1","build":"25B78",
           "kernel":"Darwin 25.1.0","arch":"arm64","model":"MacBook Pro","cpu":"Apple M4 Pro","cores":14,
           "memory":51539607552,"boot_time":1790000000},
 // 中心怎么连这台（docs/mesh.md「一台电脑多条路」，plan-hub-center.md 5）：hub_id / hub_name 是现在经过的 hub（直接连的是中心自己，
 // 经成员的是那台成员）；machine_uid 是电脑自己的 uid（旧 node 没有）；route.via 是 local | service | link | ssh | tailcat（hops 0）或
 // hub（经成员，hops 1），rtt_ms 没测到时省略。没有互联时也有（都是本 hub）。id：中心 hub.json 里有它就用中心的 id，只在成员上配的用成员的
 // id，撞名的加 @<成员名>。kind 是 hub 的：只在成员上配的电脑。旧 hub 没有这几个字段
 "hub_id":"<hub UUID>","hub_name":"家里","machine_uid":"<UUID>","route":{"via":"service","hops":0,"rtt_ms":12},
 // 可选：不止一条路（或只能经成员）时，每条路一项：方式、经哪台 hub（中心自己的路是中心的 hub_id）、现在能不能用、最近的 RTT、是不是在用的那条
 "paths":[{"via":"service","hub_id":"<hub UUID>","ok":true,"rtt_ms":12,"active":true},{"via":"hub","hub_id":"<成员 hub UUID>","ok":true,"rtt_ms":40}]}

// MetricSample：一次读数（每 10 秒）。没测到的字段缺省（第一次读数没有 cpu / net_*），不是 0；cores 只有 Linux 有
{"t":1790003600,"cpu":42.5,"cores":[40,45],"load":3.1,"mem_used":34359738368,"mem_total":68719476736,
 "swap_used":0,"swap_total":0,"net_rx":1024.5,"net_tx":512,"disk_used":10737418240,"disk_total":107374182400}

// MetricPoint：历史里的一步（该步内的平均）；gap 表示这里断开了，画图不要连线。mem / swap / disk 是已用字节
{"t":1790000000,"cpu":10.2,"load":1.25,"mem":34359738368,"swap":0,"rx":2048,"tx":512,"disk":10737418240}
{"t":1790000240,"gap":true}

// Launcher（来自 zsh：_claude_launchers 列出的函数，以及调用 claude 的 alias；docs/launchers.md）。GET …/launchers 每项另带 agent_type
// （claude / codex / traex / runtime 的 agent，旧 hub 没有时按 Claude 处理）：没写的按 agent、再按名字（codex、traex）推断，hub.json 的
// machines[].launchers 可以添加包装函数或覆盖类型，例如 [{"name":"example_wrapper","agent_type":"codex"}]，同名配置保留未覆盖的模型和网关字段
// （hub/launchers.go 的 sessionLaunchers）。Android 按类型筛选启动器。
{"name":"example_launcher","base_url":"https://example.com","model":"<模型名>","agent_type":"claude"}
// Codex 的启动器（PATH 上的 codex、跑 codex 的 alias）带 agent，新建时必须带 prompt、不能 async（docs/codex.md「从手机新建」）
{"name":"codex","model":"默认","agent":"codex","agent_type":"codex"}
// 新建请求可带 agent_type，包装函数按声明的类型启动：Claude 沿用原参数，Codex／TRAEX 不带 Claude 专用参数，并通过已有 herdr／进程识别
// 取得真实会话 ID；首句按单个参数传给启动器。权限模式和思考强度暂只适用于 Claude，Android 的新建表单选择其他类型时隐藏这两项。

// Session：(machine_id, session_id) 唯一。session_id 就是 Claude Code 的 session uuid。
{"machine_id":"server","session_id":"uuid","title":"AI 生成的标题或目录名","cwd":"<项目目录>",
 "status":"idle|working|blocked|done|unknown|offline",   // offline = 不在 herdr 里运行，只有 transcript，可恢复
 "pane_id":"w1:p2","tab_id":"w1:t2","workspace_id":"w1","agent_name":"j-3f2a",
 "agent":"claude","agent_label":"",   // agent_label 只在 App 不认识的 Agent 上有：runtime 的叫法（「DSH」，见「Runtime ↔ node」）
 "launcher":"cc_ds","model":"deepseek/v4.1-flash-chain","started_by_jarvis":true,"assistant":false,
 "updated_at":"...","last_text":"最后一条 assistant 文本的前 200 字","pending_interactions":1,
 "queued":["工作中输入、还没被取走的消息"],
 "subagents":[{"id":"a849e1f0…","session_id":"<id>@a849e1f0…","type":"Explore","description":"…","tool_use_id":"toolu_…",
                "running":true,"background":true,"started_at":"...","updated_at":"...","last_text":"…"}],   // 子 Agent 当会话读
 "loop":{"kind":"wakeup|cron","prompt":"/loop …","reason":"…","interval_seconds":1800,"next_at":"...","cron":"*/5 * * * *","ticks":12},
 // Agent 的进程在监听、回 HTTP 的 TCP 端口（docs/ports.md）：一个端口一条；title 是它首页的 <title>（没有就缺省）。旧 node 没有这个字段
 "ports":[{"port":5173,"addr":"127.0.0.1","pid":4242,"process":"node","title":"Vite + React"}],
 // 关联的未完成待办（docs/todos.md）：只在当前有关联（未 released）且待办未完成时出现，由 hub 补齐
 "todo":{"id":"todo_…","title":"天气 App 发 1.3 版"},
 "background":[{"id":"b85polnoj","kind":"shell|agent","description":"…","command":"…",   // 还没回报的后台命令 / 子代理
                "output_file":"/private/tmp/…/b85polnoj.output","started_at":"..."}]}

// Todo：待办事项（docs/todos.md）
{"id":"todo_…","title":"天气 App 发 1.3 版","notes":"说明","done_at":null,
 "created_at":"...","updated_at":"...",
 "origin":"<助手对话 id>","plan_id":"plan_…",   // 只有从助手对话开的任务才有（docs/unified-entry.md）
 "spaces":[{"machine_id":"…","space":"group/alpha","linked_at":"..."}],
 "agents":[{"machine_id":"…","session_id":"…","linked_at":"...","released_at":null,
           "title":"…","cwd":"…","agent":"claude","start_error":"","source":"manual|space|spawned","via":""}]}

// Item：transcript 规范化后的一条（一个内容块一条）
{"id":"<entry uuid>#<block idx>","seq":123,            // seq = transcript 行号，单调递增，用于增量和分页
 "ts":"...","role":"user|assistant|system",
 "kind":"text|thinking|tool_use|tool_result|notice|image|fold|turn_duration",
 "text":"…",                                           // text / thinking / notice / tool_result 的文本
 "tool_use_id":"toolu_…",                             // tool_use 与 tool_result 共用，App 据此合并成一张卡片
 "tool_name":"Bash", "tool_input":{...},              // 仅 tool_use
 "is_error":false,                                    // 仅 tool_result
 "full_size":4874,                                    // text 被截断了（页和推送里工具输出最多 4 KB、其他文字 24 KB），全文的字节数；
                                                      // 截断的 text 末尾是一行说明：工具输出是「…（已截断，点开看全文，共 N 字节）」（App 有「看全文」按钮），
                                                      // 其他文字是「…（已截断，共 N 字节）」；旧 App 照样能看懂
 "queued":true,                                       // 仅 user text：工作中输入、被中途吸收的消息
 "client_msg_id":"…",                                 // 这条来自 App 带这个 client_msg_id 发的消息（文字、图片，或 /compact 变成的 notice），
                                                      // 由 node 认出，见「待发消息的对应」；旧 hub / node 不带
 "wake":true,                                         // 仅 notice：后台任务完成时 Claude 已空闲，这条通知开启了新的一轮
 "task":{"kind":"shell|monitor|agent|task",              // 仅 notice：后台任务的通知（命令、Monitor 事件 / 结束、子 Agent），text 是一行摘要
         "status":"completed|failed|killed|stopped|event|ended|waiting","title":"description 或命令第一行",
         "task_id":"…","tool_use_id":"toolu_…","exit_code":0,"output_file":"…",
         "detail":"Monitor 的事件 / 子 Agent 的结果 / 整条命令（已解码实体）"},
 "fold":{"items":1294,"tools":647,"thoughts":110,     // 仅 kind=fold（请求带 fold_work 才有）：一段被折叠的工作，见「折叠的工作」
         "until":"...","through_seq":6890,"through_id":"…#0"},
 "image":{"mime":"image/png","data":"base64"}}        // 仅 image（tool_result 里的图片也拆成 image item，带 tool_use_id）

// Interaction：需要人处理的事
{"id":"ix_…","machine_id":"server","session_id":"uuid","session_title":"…","cwd":"…",
 "kind":"question|plan|permission|screen|elicitation",
 "created_at":"…","expires_at":"…","tool_name":"AskUserQuestion","tool_use_id":"…",
 "questions":[{"question":"…","header":"…","multi_select":false,
               "options":[{"label":"…","description":"…"}]}],   // question
 "plan":"markdown",                                              // plan
 "tool_input":{...}, "summary":"rm -rf build",                   // permission
 "screen":"终端可见区域文本",                                     // screen（hook 覆盖不到的 TUI 对话框）
 "agent":"codex", "reason":"…", "always_label":"本会话不再问这个命令"} // node 从 Codex / TRAEX 的 TUI 读出来的卡（「Codex 的提问和审批」）

// Resolution（按 kind）
{"answers":{"<question 原文>":"所选 label；多选用 \", \" 连接；或自定义文本"}}   // question
{"approve":true,"mode":"bypassPermissions|acceptEdits|default"}               // plan 批准
{"approve":false,"feedback":"改成……"}                                        // plan 打回
{"allow":true,"always":false} / {"allow":false,"message":"…"}                // permission
{"keys":["down","enter"],"text":""}                                         // screen：按键 / 文本发到 pane
{"dismiss":true}                                                            // 任意：放弃，交回电脑 TUI 处理
```

### MCP elicitation

`hello.features` 新增 `elicitation`。客户端需在 `client_hello.features` 明确声明，Android 默认声明。现有 `interaction` 事件的 `interaction` 对象新增以下可选字段，信封顶层不增加键：

```json
{
  "kind": "elicitation",
  "mcp_server_name": "example-mcp",
  "agent_id": "agent_example",
  "agent_type": "worker",
  "elicitation": {
    "mode": "form",
    "message": "请选择",
    "requested_schema": {
      "type": "object",
      "properties": {"choice": {"type": "string", "enum": ["yes", "no"]}}
    }
  }
}
```

URL 模式的 `elicitation` 为 `{"mode":"url","message":"请打开链接完成确认","url":"https://example.com/confirm","elicitation_id":"request_example"}`。`mcp_config_source`、`tool_name`、`tool_use_id` 均可选，只有来源提供时才有；Claude Code 2.1.306 的 Elicitation 输入不提供工具关联和配置来源，缺失时不推断。

Resolution 新增可选 `action`、`content`。`{"action":"accept","content":{"choice":"yes"}}` 提交 form；URL 接受只带 `{"action":"accept"}`；拒绝和取消分别为 `{"action":"decline"}`、`{"action":"cancel"}`。`{"dismiss":true}` 仍表示放弃手机接管、交给终端。缺少合法 action 的答复返回 400，请求保持待处理。

下发和降级规则：

- 广播和连接初始化都过滤未声明能力的连接。首次声明能力后补发仍待处理的 elicitation，重复 hello 不重复补发。
- REST 待处理列表及历史列表也过滤。只有相同认证凭据、相同 `X-Jarvis-Client` 平台／版本存在已声明能力的活动连接，才返回该类型；HTTP 请求头本身不能声明能力。
- 创建时没有兼容连接，立即交回终端，不留下等待手机作答的请求。已有交互的超时、撤销及重启恢复规则保持适用。
- elicitation 通知只走同时声明 `ws_notify` 和 `elicitation` 的连接，不发旧 iOS 的 APNs 交互类别；共享 Fleet 继续使用现有终端提示，不传入新 kind。

新字段随现有交互 JSON payload 存储，无需数据库迁移。包内协议核对和未验收项见 [Claude 集成文档](claude-integration.md#mcp-elicitation-hooks)。

## REST（App → hub）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/machines` | `{"machines":[Machine]}` |
| POST | `/api/v1/machines/{mid}/reconnect` | 重连该机器的 node |
| PUT | `/api/v1/machines/{mid}/engine` | `{"engine":"herdr"\|"herdr-core"\|"herdr-both"\|"orca"}` → 200 更新后的 Machine；不认识的引擎 400 `{"error"}`，没有这台机器 404。离线也能改（存下，连上时生效）；改完照常推 `machines`（下面「引擎」） |
| GET | `/api/v1/machines/{mid}/launchers` | `{"launchers":[Launcher]}` |
| GET | `/api/v1/machines/{mid}/launchers/{name}/models?refresh=1` | 这个启动器的 CLI 在 `/model` 里列的模型（新建 Agent 选模型，`docs/model-config.md`「新建 Agent 选模型」）：`{"agent","models":[{"id","name","description"?,"resolved"?,"default"?,"efforts"?,"default_effort"?}],"locked"?}`。`id` 是开始会话时 `model` 填的值；`default` 是不给模型时跑的那个（选它等于不选）；`resolved` 是别名（opus…）按这个启动器的设置对应的模型；`efforts` 是它能用的思考强度，从弱到强；`locked` 是启动器写死了自己的 `--model`、选了也不会生效的原因。没有列表可问的 Agent（TRAEX、runtime 的）回 `models: []`。node 起一次 CLI 去问（几秒），存 10 分钟，`refresh=1` 重新问；列不出来回 502 `{"error"}`；这台电脑的 node 还没有这个功能时回 404 `{"error","code":"node_outdated"}`（和旧 hub 一样，App 不显示模型行） |
| GET | `/api/v1/todos` | 全部待办（新建的在前）：`{"todos":[Todo]}`，每个待办带 `spaces` 和 `agents`（过滤 dismissed，当前的在前按 `linked_at` 降序，然后是参与过的按 `released_at` 降序） |
| POST | `/api/v1/todos` | 新建待办 `{"title","notes"?}` → 200 `{"todo":Todo}`；`title` 不能为空（400）；只读设备 403 |
| PATCH | `/api/v1/todos/{id}` | 修改待办 `{"title"?,"notes"?,"done"?}` → 200 `{"todo":Todo}`；`done: true` 标记完成，`false` 重新打开；不存在 404；只读设备 403 |
| DELETE | `/api/v1/todos/{id}` | 删除待办及关联（Agent 本身不动） → 200 `{"ok":true}`；不存在 404；只读设备 403 |
| PUT | `/api/v1/todos/{id}/spaces/{mid}/{space...}` | 整组挂工作区项目到待办 → 200 `{"ok":true,"linked":N}`；`space` 为 URL 编码的项目名（通配接住）；已完成 409，不存在 404，只读设备 403 |
| DELETE | `/api/v1/todos/{id}/spaces/{mid}/{space...}` | 解除整组挂（物理删除待办下该项目的 space 关联 Agents） → 200 `{"ok":true}`；不存在 404，只读设备 403 |
| PUT | `/api/v1/todos/{id}/agents/{mid}/{sid}` | 关联已有 Agent → 200 `{"ok":true}`；同一 Agent 原挂在别处标 released，清掉 dismissed，已完成 409，不存在 404，只读设备 403 |
| GET | `/api/v1/task-plans/{id}` | 一个计划（`TaskPlan`，见下面「任务和对话动态」） → 200 `{"plan":TaskPlan}`；不存在 404 |
| POST | `/api/v1/task-plans/{id}/start` | 开始计划 `{"tasks"?:[PlanTask],"remember"?:bool}`：`tasks` 按下标覆盖用户在卡片上改过的字段（空的不改），`remember` 记住「这类以后直接开」；每个任务建一个待办（`origin`、`plan_id`）、在后台开 Agent → 200 `{"plan":TaskPlan}`；已开始或不开了 409，不存在 404，只读设备 403 |
| POST | `/api/v1/task-plans/{id}/dismiss` | 不开了 → 200 `{"plan":TaskPlan}`；已开始 409 |
| GET | `/api/v1/assistant/conversations/{id}/feed?since=` | 对话动态：`since`（RFC3339）之后的条目，旧的在前，一次最多 500 → `{"items":[FeedItem],"plans":{"plan_…":TaskPlan}}`（`plans` 是 `plan` 条目引用的计划的当前状态） |
| POST | `/api/v1/assistant/conversations/{id}/route` | 直接对任务说话 `{"todo_id","text"}`：hub 发给这个任务最要紧的 Agent（等你 > 工作中 > 完成未看 > 最近的），追加 `routed` 条目 → 200 `{"item":FeedItem}`；不是这段对话的任务、没有在跑的 Agent 409；只读设备 403 |
| DELETE | `/api/v1/todos/{id}/agents/{mid}/{sid}` | 移出 Agent（软移出：置 `released_at` 和 `dismissed_at`，接口不再返回该行且不再自动挂回） → 200 `{"ok":true}`；不存在 404；只读设备 403 |
| GET | `/api/v1/machines/{mid}/dirs?path=` | 目录浏览：`{"path","parent","entries":[{"name","path","is_git"}],"recent":[{"path","name","last_used"}]}`；path 为空时 entries 是 home |
| GET | `/api/v1/sessions?machine_id=` | `{"sessions":[Session]}`，含运行中的（herdr）和最近 7 天的 offline 会话 |
| POST | `/api/v1/sessions` | `{"machine_id","cwd","launcher","agent_type"?,"model"?,"prompt"?,"resume_session_id"?,"workspace_id"?,"permission_mode"?,"effort"?,"tab_name"?,"async"?,"name_hint"?,"todo_id"?}` → `{"session":Session}`。可选 `X-Jarvis-Op-Id` 接入 `start` 持久回执。`model` 可选：新建 Agent 里选的模型（上面 `…/launchers/{name}/models` 的 `id`），只管这一次，盖在「模型配置 › Agents」上（`hub.applyModel`）。`todo_id` 可选：新建成功后在同一请求内挂上（不存在 404、已完成 409、只读设备 403）。`tab_name` 是 herdr 标签页的名字；不给时 hub 用小任务模型按 `prompt` 起一个短英文名（`hub/tabname.go`，约 2 秒，失败就不起），node 再兜底成 `agent-<n>`，同一空间里重名加 `-2`、`-3`（`node.tabLabel`）。新建空间时它的第一个标签页也用这个名字。助手的 `agent_start` 同样自动起名。`"async": true`（App 都带）：立刻回会话（新会话 id、目录、启动器，`starting: true`），开标签页、等 Claude 就绪在后台做，见下面「新建会话」；这时 hub 不等起名，标签页先叫 `agent-<n>`，名字出来后再 `session.rename`。`name_hint`：App 把第一条消息当普通消息发（马上上屏）时，用它起名、不进 `prompt` |
| POST | `/api/v1/tabs/autoname` | `{"dry_run"?,"all"?}` → `{"tabs":[{machine_id,session_id,space,title,from,to,error?}]}`：把在跑的 Agent 里「没人起过名」的标签页（`jarvis`、`jarvis-N`、herdr 的数字、`agent-N`）按会话标题改成短英文名，规则同新建；`dry_run` 只列出会改成什么，`all` 连用户起过名的也改。模型偶尔回空，会再问一次，仍没有就留原名（`error`）。2026-10-31 在两台机器上跑过一次：61 个标签页里改了 39 个，用户自己起的 21 个没动 |
| GET | `/api/v1/sessions/{mid}/{sid}/items?before_seq=&turns=2` | 历史分页 `{"items":[Item],"has_more":bool}`（按 seq 升序）。`turns`：往前取够 N 轮完整的「用户输入 + 回答」，页首一定是一条用户输入（最多 2000 条；一轮特别长、往前要解析几十 MB 时这一页会短一些，`has_more: true`，再往前翻接着取；但只要前面还有内容，一页至少带一条，空页加 `has_more: true` 会让 App 没有 seq 可以接着往前取）；旧客户端的 `limit` 仍按条数。带 `lazy_images=1` 时图片不含数据（`image: {mime, lazy:true, size, width, height}`），显示时再取。带 `after_seq=N` 时返回 seq > N 的新内容（后台同步用，约 500 条一页，从不切开同一行的多个 item；`more_after: true` 表示后面还有，接着用最后一条的 seq 再取，或改取最新一页）。带 `fold_work=1` 时从末尾往前的页把工作折叠起来；带 `from_seq=&through_seq=` 时返回这几行的 items（展开折叠用，约 1000 条一页，`more: true` 表示还没到 `through_seq`）。见「折叠的工作」 |
| GET | `/api/v1/sessions/{mid}/{sid}/items/{item id}/image` | 一条 item 的图片原始字节（Content-Type 为图片类型，可永久缓存）；item id 含 `#`，要百分号编码 |
| GET | `/api/v1/sessions/{mid}/{sid}/items/{item id}/full` | 一条 item 的全文（`Item`，不带图片、没有 `full_size`；最多 1 MB）：App 点「看全文」时取。node 重新读那一行解析 |
| POST | `/api/v1/sessions/{mid}/{sid}/messages` | `{"text":"…","images":[{"mime","data"}]?,"client_msg_id"?,"clear_draft"?,"todo_id"?}`；`todo_id` 可选：发送成功后挂上（不存在 404、已完成 409、只读设备 403）；会话 offline 时先用原启动器 `--resume` 拉起；被拒见下面「发消息」 |
| GET | `/api/v1/sessions/{mid}/{sid}/info` | 会话信息：`{session_id,transcript_path,cwd,model,launcher,git_branch,version,permission_mode,effort,context_tokens,output_tokens,compactions,turns,started_at,last_activity}` |
| POST | `/api/v1/sessions/{mid}/{sid}/queue` | `{keep:[索引]}`：取回原排队输入并仅保留指定项，返回 `{queue}`，用于编辑或删除；不走 client_ops |
| POST | `/api/v1/sessions/{mid}/{sid}/seen` | 标记会话已看，返回 `{ok:true}`；WS 也可发 `seen` |
| POST | `/api/v1/sessions/{mid}/{sid}/interrupt` | 发 Esc |
| POST | `/api/v1/sessions/{mid}/{sid}/keys` | `{"keys":["esc"],"text":""}` |
| GET | `/api/v1/sessions/{mid}/{sid}/screen?lines=60&source=visible` | `{"text":"…","status":"working"}`；`source=recent` 连同窗格滚动缓冲一起读（lines 最多 20000，返回行数少于 lines 说明到头了）；`format=ansi` 带颜色（SGR，行尾 `\r\n`；旧 node 不认，照旧回纯文本） |
| POST | `/api/v1/sessions/{mid}/{sid}/screen/before` | `{"anchor":["屏幕上最上面几行"],"lines":200,"format"?:"ansi"}` → `{"text","has_more","at_limit"}`：锚点之前的一段滚动历史（按内容定位，锚点按纯文本比；herdr 只保留约 1000 行，`at_limit` 表示到头） |
| POST | `/api/v1/machines/{mid}/spaces/{wid}/rename` | `{"name"}`：`herdr workspace rename`，并把置顶 / 屏蔽里的「机器/旧名」改成新名 |
| POST | `/api/v1/sessions/{mid}/{sid}/modes` | `{"permission_mode"?, "effort"?}`：改在跑的会话的权限模式 / 思考强度（`docs/ios.md`「思考强度和权限模式」）；会话列表里的 `permission_mode`、`effort` 是当前值。`POST /api/v1/sessions` 和 `POST /api/v1/assistant/conversations` 也收这两个字段，作为启动时的值 |
| POST | `/api/v1/sessions/{mid}/{sid}/rename` | `{"tab_name"?, "title"?}`：tab 用 `herdr tab rename`；会话名给在跑的 claude 发 `/rename <名字>`（它写 `custom-title`），没在跑就直接往 transcript 追加同样的 `custom-title` 行。助手的对话：`title` 同时记成用户起的标题（hub 的 `assistant.titles`，不再被自动标题覆盖，`docs/assistant.md`「对话标题」）；Codex 的助手对话只记在 hub |
| GET / PUT | `/api/v1/mutes` | 屏蔽，结构同 pins，另有 `allowed`（空间被屏蔽时单独开启通知的 Agent）。规则：Agent 自己在 sessions 里→屏蔽；在 allowed 里→不屏蔽；否则看它的空间。被屏蔽的 Agent / 空间不推送任何通知（完成、需要你），手机上弱化显示；hub 内存缓存，PUT 时更新 |
| GET | `/api/v1/hub/update` | hub 的版本和更新状态（`docs/hub-update.md`）：`{version, build, channel, can_update, why_not?, latest?{version, build, released, notes, newer, bad, held?}, state, progress?, busy:[{machine_id, machine, session_id?, title}], error?, auto, auto_notice, last_check?, confirmed}`；`state`：`idle` / `checking` / `available` / `downloading` / `waiting_idle` / `restarting` / `confirming` / `updated` / `rolled_back` / `failed`；`busy` 是重启会打断的（在工作或在等回答的对话、正在重新部署的机器）；`auto_notice`：还没告诉过用户「自动更新已开启」；`latest.held`：更新的这一版自动更新不会装的原因（它的代码不一定包含这个 hub 正在运行的提交，`docs/hub-update.md`「防退回」），手动更新照样可以；Docker 的 hub 另带 `image{name, tag, pinned}`（它跑的镜像，`pinned`：标签是个版本号，要先改 `.env`），`latest` 是 registry 上最新的正式版本（`build` 为 0、没有 `released`），`POST …/check` 去问 registry |
| POST | `/api/v1/hub/update/check` | 立即检查，回同上 |
| POST | `/api/v1/hub/update` | `{"now"?: true}` 开始更新：下载、校验、等助手和 Agent 都空闲、替换、重启；`now` 是二级确认后的「现在就更新」，不等空闲。不是发布版（`dev` / `docker`）、没有更新的版本时 409（`error` 说明，Docker 给出 `docker compose pull` 的命令） |
| PUT | `/api/v1/hub/update/settings` | `{"auto"?: bool, "auto_notice_seen"?: bool}`：自动更新开关（默认开），改了开关也算告诉过了 |
| GET | `/api/v1/hub/update/history` | `{"history":[{from_version, to_version, to_build, started, ended, result: updated\|rolled_back\|failed\|held, reason?, auto}]}`，新的在前，留 50 条；`held`：更新的版本没有自动装（`reason` 写原因），每个版本记一次 |
| GET | `/api/v1/skills` | 跑助手那台机器上的 skill（只读）：`{"machine_id","machine","skills":[{"name","description","source":"user\|builtin"}]}`；用户的在那台机器的 `~/.persona/skills/`（`docs/hub-update.md`）。node 太旧（没有 `skills.list`）或不在线时 502 |
| GET / PUT | `/api/v1/pins` | 开发列表置顶：`{"spaces":["server/app"],"sessions":["server/<session id>"]}`，空间用「机器/空间名」（herdr 重启后 workspace id 会变），存 Postgres settings |
| GET | `/api/v1/machines/{mid}/file?path=&cwd=&offset=` | 读一个文件（文件查看器，`docs/ios.md`「文件查看器」）：`{"path","name","size","mime","text"\|"data","truncated","offset","next","is_dir","entries"?}`。文本文件每次最多 2 MB，从 `offset` 读起，没读完时停在最后一个完整行的末尾，`next` 是下一页的起点（读完为 0）；二进制文件整个给（上限 2 MB），不能从中间读；目录给前 500 项。`~` 是 home，相对路径在 `cwd` 里；拒绝 iCloud |
| GET | `/api/v1/machines/{mid}/file/download?path=&cwd=` | 完整文件的原始字节，使用同一 Bearer 鉴权，不把 token 放进 URL；hub 用 node 的 `file.chunk` 每次最多 1 MiB 读取，单次请求不把整个文件放进内存。支持 HTTP Range / Content-Range（包括文件末尾范围），附带 Content-Length、ETag 和附件文件名；空路径 400、旧 node 409 `node_outdated`，非普通文件、iCloud 和读取错误拒绝。后续块必须匹配首次读取的文件大小和版本，文件中途变更或请求取消就停止下载。旧 `/file` 的 2 MiB 二进制预览行为不变 |
| GET | `/api/v1/machines/{mid}/stat?paths=&cwd=` | 会话里提到的路径在这台机器上在不在：`paths` 一行一个（也收 `path=a&path=b`），最多 200 个 → `{"stats":[{"path"(原样),"kind":"file"\|"dir"\|"denied"\|"missing","size"?}]}`。`denied` 是 macOS 不让 personad 读的文件夹（打开时会说怎么授权）；相对路径没有 `cwd`、iCloud 里的路径一律 `missing`。手机只给 file / dir / denied 的路径做可点的文件块 |
| GET | `/api/v1/machines/{mid}/fs?path=&cwd=` | 文件树：`{"path","entries":[{"name","dir","symlink","size","mtime"}],"truncated"}`，目录在前，最多 2000 项；拒绝 iCloud |
| GET | `/api/v1/machines/{mid}/git/status?cwd=` | `{"is_repo","root","branch","head","upstream","ahead","behind","files":[{"path","orig_path","index","worktree","added","deleted","binary"}],"stashes","commits":[最近 30 条]}`（只读，`GIT_OPTIONAL_LOCKS=0`） |
| POST | `/api/v1/machines/{mid}/git/pull` | `{"cwd"}` → `{"status": GitStatus, "updated": bool}`；只允许有上游的普通分支与干净工作区，执行 `git pull --ff-only --no-rebase --no-autostash --no-recurse-submodules`。分叉、未提交改动、无上游、认证失败或超时均报错，不自动 stash / 合并 / 变基；pull 命令最多 60 秒，node 总计 75 秒、hub 90 秒。仅 full 角色可调用 |
| GET | `/api/v1/machines/{mid}/git/diff?cwd=&path=&staged=0\|1` | `{"path","diff","truncated"}`；未跟踪文件对 /dev/null 出 diff，最多 512 KB |
| GET | `/api/v1/machines/{mid}/git/show?cwd=&hash=` | `{"commit","files","diff","truncated"}` |
| GET | `/api/v1/machines/{mid}/git/log?cwd=&skip=&limit=` | `{"commits":[…]}` 分页 |
| POST | `/api/v1/sessions/{mid}/{sid}/close` | 退出 claude 并关闭 tab（herdr 里的任何 Agent，手动启动的也行；分屏的 tab 只关它自己的窗格） |
| DELETE | `/api/v1/sessions/{mid}/{sid}` | 删除对话：只是从各端的会话列表里移除（hub 在 settings `sessions.deleted` 记下时间，`sessions` 推送和 `GET /api/v1/sessions` 都不再带它），电脑上的 transcript 不动；之后这个会话再被用到（`updated_at` 晚于删除时间，或又运行起来）会重新出现。启动失败的（`start_error`）也能删，立即消失。→ `{"ok":true}`；正在运行或还在启动 409，助手的对话 400（用归档），没有这个会话 404，只读设备 403 |
| GET | `/api/v1/assistant` | `{"machine_id","launcher","cwd","timezone","notes"?,"notes_effective","main"?,"conversations":[Session]}`：`notes` 只在 App 里保存过时有，`main` 是主对话的会话 id（在 `machine_id` 那台机器上）；已归档的对话默认不列，`?archived=1` 一起列（`archived: true`） |
| PUT | `/api/v1/assistant` | `{"machine_id"?,"launcher"?,"timezone"?,"notes"?,"main"?}`，存 Postgres settings（`docs/assistant.md`）；只改带了的字段，`notes` 为空串也算改；`main` 为 `"-"` 时清掉主对话（换机器时 App 一起发） |
| POST | `/api/v1/assistant/conversations` | `{"prompt","images"?}` → `{"session"}`：写好 CLAUDE.md 后在助手目录新开会话 |
| POST | `/api/v1/assistant/conversations/{id}/archive` | 归档助手的一段对话（只是不在列表里，数据保留）→ `{"ok","archived":true}`；主对话 400。会话列表（`sessions` 推送）里它带 `archived: true`，App 收在「已归档」；用户再给它发消息（`POST …/messages`）自动取消归档（`docs/assistant.md`「管理自己的对话」） |
| POST | `/api/v1/assistant/conversations/{id}/unarchive` | 取消归档 → `{"ok","archived":false}` |
| POST | `/api/v1/assistant/warm` | `{"launcher"?}`：先起一个备用 claude 进程，新对话直接用 |
| GET | `/api/v1/assistant/tasks` | 定时任务（一张表，`docs/assistant.md`「定时任务」）：`{"tasks":[Task]}`，已经跑过的一次性跟进不列。`Task` = `{id,title,prompt,repeat:"once"\|"daily"\|"interval",at,time:"HH:MM",weekdays:[1..7],every_minutes,enabled,machine_id,session_id,launcher,permission_mode,effort,created_by,created_at,last_run,last_session_id,last_status,last_error,next_run,run_machine_id,run_session_id,run_launcher}`：`once` 用 `at`，`daily` 用 `time` + `weekdays`（空 = 每天），`interval` 每 `every_minutes`（5 分钟到 7 天）跑一次、`at` 是下一次；`session_id` 有值时发进那段对话，空 = 任务自己的对话（`run_session_id`，hub 记的：第一次运行开，之后接着用；空 = 下一次开）；`launcher` / `permission_mode`（`default` / `acceptEdits` / `plan` / `auto` / `bypassPermissions`）/ `effort`（`low`…`max`）空 = 助手的默认，只能在 App 里改；`created_by: "assistant"` 是助手在对话里建的 |
| POST | `/api/v1/assistant/tasks` | 新建 `Task`（`session_id` 忽略，用任务自己的对话）→ `{"task"}`；不合法 400，消息是给用户看的中文 |
| PUT | `/api/v1/assistant/tasks/{id}` | 保存用户能改的字段（`title,prompt,repeat,at,time,weekdays,every_minutes,enabled,launcher,permission_mode,effort`，`""` = 默认）→ `{"task"}`；`session_id` 只能清空（改成用任务自己的对话）；`run_*` 只由 hub 写；`interval` 的下一次在间隔或规则变了、或重新开启时从现在算起 |
| DELETE | `/api/v1/assistant/tasks/{id}` | 删除 |
| POST | `/api/v1/assistant/tasks/{id}/run` | 立即运行 → `{"session_id","machine_id"}`；一次性的跑完就结束，重复的不改时间；上一轮还在跑时 409（记一条跳过） |
| POST | `/api/v1/assistant/tasks/{id}/fresh` | 下次开新对话：清掉 `run_*`，下一次运行开一段新对话并记下 → `{"task"}` |
| GET / PUT | `/api/v1/assistant/routines` | 旧版 App 用（2026-10-31 前）：`daily` 的任务，形状同 `Task`；PUT 整表替换 `daily` 的那些，别的任务和各任务的启动器 / 权限模式不动 |
| GET / DELETE | `/api/v1/assistant/followups[/{id}]` | 旧版 App 用：开着的 `once` 和 `interval` 任务（`at` = 下一次）；DELETE 删任务 |
| POST | `/api/v1/assistant/routines/{id}/run`、`/api/v1/assistant/followups/{id}/run` | 旧版 App 用，同 `tasks/{id}/run` |
| GET | `/api/v1/assistant/runs[?ref=<id>]` | 运行记录，新的在前：`{"runs":[{id,kind:"routine"\|"followup"（一次性的是 followup）,ref_id（任务 id）,title,due,at,status,error,manual,machine_id,session_id}]}` |
| GET | `/api/v1/assistant/memory` | `{"files":[{name,content,updated_at}],"dir"}` |
| PUT / DELETE | `/api/v1/assistant/memory/{name}` | `{"content"}`；删除时顺带删 MEMORY.md 里的索引行 |
| GET / PUT | `/api/v1/machines/{mid}/claude-settings` | 白名单的 Claude Code 设置；PUT `{"key","value"}`（value 为 null 恢复默认） |
| GET | `/api/v1/machines/{mid}/claude-dirs` | 这台机器的 Claude Code 配置目录和屏蔽规则：`{"dirs":[{"path","default"?,"blocked"?,"blocked_by"?,"source","running","sessions"?}],"blocked":[…],"home","live"}`（`docs/claude-integration.md`「屏蔽配置目录」） |
| PUT | `/api/v1/machines/{mid}/claude-dirs/blocked` | 整份替换屏蔽规则 `{"blocked":["~/.automation/*", …]}`（路径或通配，以 `/` 或 `~/` 开头，否则 400）；回同 GET 的状态 |
| GET | `/api/v1/interactions/history?machine_id=&session_id=&limit=` | `{"interactions":[{"interaction","status","resolution","resolved_at"}]}` |
| GET | `/api/v1/interactions` | `{"interactions":[Interaction]}`（待处理） |
| POST | `/api/v1/interactions/{id}/resolve` | body 为 Resolution |
| POST | `/api/v1/devices` | `{"device_id","name","kind":"iphone|mac|watch","apns_token"?,"apns_env"?,"topic"?,"push_key"?,"relay_url"?,"relay_grant"?}`；后三个是推送中继（手机公钥、中继地址、中继给的凭据，`docs/push-relay.md`），hub 没有 APNs key 时据此推送。互联组里只有中心推送：连成员时这条也被转给中心；推送的 payload（直推的和经中继密封的）顶层带 `hub_id`：中心的（[mesh.md](mesh.md)「凭据和推送」）。`mac` 是 Mac App（同一个 bundle id，同一个 topic）：不收静默唤醒，它连着 hub 时不收「需要你处理」「Agent 做完」（App 自己发本地通知）。hub 记下是哪个设备凭据登记的，吊销那个凭据时连这台设备一起删掉，不再推送 |
| POST | `/api/v1/devices/{id}/live-activity` | 实时活动的 token：`{"push_to_start_token"}`、`{"activity_id","push_token"}`（会轮换，重复发）、`{"activity_id","ended":true}`（`docs/live-activity.md`） |
| GET | `/api/v1/fleet` | `{"fleet": FleetState}`：当前的实时活动内容（App 回到前台时拉），`stats` 带全部数字和每台机器的负载 |
| GET | `/api/v1/metrics` | `{"machines": [{machine_id, online, system, latest: MetricSample \| 无, points: [MetricPoint]}], "step": 60}`：App「机器」页每张卡片上的现在值和走势，按配置顺序的每台机器，`points` 是最近 1 小时、每分钟一个点；`latest` 只在 30 秒内收到过样本时有（`docs/machines.md`） |
| GET | `/api/v1/machines/{id}/metrics?range=1h\|6h\|24h` | `{"metrics": {同上一台}, "range", "step"}`：机器详情的曲线，步长 10 / 60 / 240 秒（约 360 个点）；没有这台机器 404，range 不对 400 |
| GET | `/api/v1/usage` | `{"usage": {tpm, tokens, output, requests, machines:[{id, tpm, tokens, load}]}}`：所有机器的 token 用量（今天按手机时区算，`load` 是 1 分钟负载 / 核数），见 `docs/live-activity.md`「用量」 |
| GET / PUT | `/api/v1/devices/{id}/prefs` | 这台设备要哪些推送：`{"input":true,"done":"off|long|always"}`，另有 `translate`（设置 › 翻译：`{enabled,target,mail}`，hub 据此提前翻译新邮件）、`live_activity`（默认 true）、`lock_details`（锁屏显示详情，默认 false）、`island`（灵动岛两侧：`{"preset":"a|b|c|custom","left","right","machine"}`，默认 `a`）、`mute_while_desktop`（手机静音，默认 false：有桌面端连着时不给这台设备推「需要你处理」「Agent 做完」；助手按你的要求发的提醒照推）；`look`（这台手机的实时活动画哪个形象：`{"design","light","dark","package"?}`，App 换形象时单独 PUT，hub 原样放进这台设备推送的 content-state，见 `docs/live-activity.md`「形象」；design 超过 32 字节或 package 超过 160 字节的不存）；PUT 可以只带要改的字段；返回值另带 hub 的 `long_turn_seconds`、`done_delay_seconds`（只读）。设备还没注册推送时也能设 |
| GET | `/api/v1/personas` | 形象（`docs/persona.md`）：`{"personas":[{"id","rev","bytes","manifest":{persona.json 原样}}]}`，hub 机器 `~/.persona/personas/` 下检查通过的包（文件齐全、id 和目录同名、不超大小）；`rev` 是整个包内容的哈希，任何一个字节变了它就变，手机按 rev 缓存。没有目录时是空列表 |
| GET | `/api/v1/personas/{id}/{file…}` | 包里的一个文件（PNG / persona.json），只给包目录里的普通文件（不跟软链、不给点开头的） |
| GET | `/api/v1/plugins` | hub 的插件：`{"plugins":[{"id","name","kind","status","error"?}]}`，`status` 是 `starting / running / error / disabled`（`docs/plugins.md`）；`features` 带 `plugins` 时才有 |
| GET | `/api/v1/machines/{mid}/ports/{port}` | 端口探测（`docs/ports.md`）：`{"port","listening","http","title"?}`，只看不放行 |
| POST | `/api/v1/machines/{mid}/ports/{port}/open` | 用户在 App 里点开一个不在任何 Agent 名下的端口：有东西在听就放行 24 小时、回同上；没有 404；只读设备 403 |
| GET | `/api/v1/machines/{mid}/ports/{port}/connect` | 端口转发：升级成 WebSocket，二进制消息就是一条 TCP 连接的字节（手机本地监听，浏览器每开一条连接就连一次）。端口必须是这台机器某个会话 `ports` 里的或点开过的，否则 403；没在听 404；只读设备 403；机器离线 503；一台机器同时超过 64 条 429 |
| POST | `/api/v1/phone/runtime-cli` | `{cmd, args, timeout}`：让手机上的 DEBUG 包执行一条 Runtime CLI 命令（`docs/runtime-cli.md`「经 hub 连真机」），回 App 的原样回答 `{"ok": …}`。只给 `full` 设备凭据（只读设备 403）；没有登记了 `phone_runtime_cli` 的 App 在线 503 `phone_offline`（不唤醒）；`timeout` 秒内（最多 120）没回答 504；App 说这次调用失败 502。每次调用 hub 记一行日志（命令、调用方设备 id、状态、耗时），不记参数和回答 |
| 任意 | `/api/v1/plugins/{id}/{path…}` | 原样转发给插件的 `/{path…}`（WebSocket 也是），去掉 `Authorization` 和 `token`；只读设备 403。语音插件的契约见 `docs/plugins.md`「语音」 |

邮件见下文「邮件 API」。HTTP 错误格式和可选字段见上文「错误体」。

## WebSocket `/api/v1/ws`

信封 `{"type":"…", ...}`。

服务端 → 客户端：

| type | 字段 |
| --- | --- |
| `hub_update` | `update`（同 `GET /api/v1/hub/update`）：检查、下载、等空闲、重启、确认、回滚，状态一变就推；`restarting` 之后连接会断一会儿，是预期的 |
| `hello` | `server_version`, `client_id`, `features`, `hub_id`, `hub_name`, `endpoints`, `tls_port`, `fp`, `mesh`, `channel`, `build`, `protocol`, `min_app_build`（后四个同 `/api/v1/health`）；每条 WS 首条消息，尚不含协商后的心跳。`mesh`：`{"hubs":[{hub_id,name,online,self,center,endpoints,tls_port,fp}]}`，互联组里所有 hub（含自己，`self: true`；配过对但现在到不了的 `online: false`；`center: true` 是组里的中心，客户端在同组的几台里优先连它），没有互联时只有自己。连成员时 hello 来自中心（`hub_id` 是中心的）（[mesh.md](mesh.md)） |
| `endpoints` | `endpoints`（完整候选列表，仅发给声明同名能力的连接） |
| `client_hello_ack` | `heartbeat: {interval, pong_timeout}`（本次协商后实际采用的秒数） |
| `machines` | `machines` |
| `todos` | —：待办或关联变了就推，客户端收到后重拉 `GET /api/v1/todos` |
| `assistant_feed` | `conversation`, `item`（`FeedItem`）：对话动态多了一条 |
| `task_plan` | `plan`（`TaskPlan`）：计划交给用户、开始、不开了、某个任务没开起来（`tasks[i].error`） |
| `sessions` | `machine_id`, `sessions`（该机器全量，变化时推，和上一条一模一样就不推；每个会话带 `rev` = transcript 字节数，变了就说明有新内容；可选 `suggestion`：Claude 一轮结束后在输入框里给的灰色「下一条」建议，见 `docs/claude-integration.md`） |
| `revs` | `machine_id`, `revs`（`{session_id: rev}`，只含变了的；列表里看得见的字段没变、只是 transcript 长了时发，推给所有客户端，App 据此后台拉增量） |
| `lark` | `feed_ids`（飞书有新消息 / 会话变化的会话；hub 跟随 lark-connector 的实时事件 `/agent/v1/events`，每 400ms 最多发一次，推给所有客户端） |
| `items` | `machine_id`, `session_id`, `items`，以及这批在 transcript 里的位置（见下「items 的连续性」）：`history`, `to_seq`, `from_seq`（接续的批次才有）。只推给订阅了该会话的客户端；没有 item 的行（ai-title 之类）也会推一条空的 `items`，让 App 知道这几行已经看过 |
| `delta` | `machine_id`, `session_id`, `text`（无头助手正在生成的整段文字；空串表示这轮结束）。约每 80 ms 一条；App 每个对话最多每秒画 5 次，停顿后的第一条立刻画、之后画最新的，空串立刻画，消息落进 transcript 或重连时丢掉还没画的（`DeltaPacer`） |
| `screen` | `machine_id`, `session_id`, `text`, `status`, `ts`（node 已不再随订阅采样屏幕：终端页自己按需读 `/screen`，旧 App 收不到这条也不影响） |
| `terminal_control` | `machine_id`, `session_id`, `state`：`active`（接管了，带 `cols`, `rows`）、`busy`（电脑上别的程序占着，问用户要不要 `takeover`；先镜像着）、`taken`（被别的设备或程序抢走）、`lost`（node 自己放手了）、`unsupported`（旧 node / herdr，只能镜像），`error` 是给人看的原因 |
| `terminal` | `machine_id`, `session_id`，以及一帧 `epoch`, `seq`, `full`, `cols`, `rows`, `data`（base64 的 VT 字节），或者 `error`（流停了：电脑离线、会话不在跑……）+ `unsupported`（旧 node / herdr 不能推流，改用 `/screen` 轮询）。见下「终端流」 |
| `interaction` | `interaction`（新的待处理，推给所有客户端） |
| `interaction_resolved` | `id` |
| `fleet` | `fleet`（实时活动汇总，与 HTTP `/api/v1/fleet` 的内容对应） |
| `hook_event` | node hook 事件的原字段，hub 补 `machine_id` 并置 `type: hook_event`；用于刷新会话运行状态 |
| `tool_call` | `call_id`, `name`, `args`（只发给已 `register_phone` 且提供该工具的手机） |
| `desktops` | `desktops`：连着的桌面端（Mac App），`[{"device","name","since"}]`，`device` 是它的设备凭据 id（用配对凭据连的为空，不能单独退出）。连上时发一次，之后有桌面端连上 / 断开就推给所有客户端（feature `desktops`）。iPhone 据此显示「Mac 上的 Persona 已登录」 |
| `notify` | `kind: interaction / done / assistant`、`machine_id`、`session_id`、`title`、`subtitle`、`body`，交互另带 `interaction_id`；只发给声明 `ws_notify` 的连接，见下文 |
| `lock_test` | `lock_test_id`、`lock_test_due_at`、`lock_test_sent_at`、`lock_test_title`、`lock_test_body`；只发给本设备声明 `lock_test` 的选定连接，见「锁屏测试」 |
| `inbox` | 无内容载荷；列表变化提示，客户端重新拉取收件箱 |
| `error` | `machine_id`、`session_id`、`error`；例如订阅补发失败，客户端据此核对会话 |
| `pong` | `id`（字符串，原样回应客户端应用层 ping） |

客户端 → 服务端：

| type | 字段 |
| --- | --- |
| `subscribe` | `machine_id`, `session_id`, `since_seq`（可选，App 传本地的 `covered`；有则补发其后的全部 items——每页一条 `items`（`backlog: true`），最多 8 页 / 4000 条，超出时最后一条带 `more_after: true`；否则补发最后两轮），`lazy_images`（可选；true 后这个连接收到的 items 都不带图片数据），`fold_work`（可选；true 后这个连接从末尾补发的最后两轮折叠工作，`since_seq` 的补发不折叠） |
| `seen` | `machine_id`, `session_id`；与 HTTP `/seen` 同义 |
| `unsubscribe` | `machine_id`, `session_id` |
| `terminal_subscribe` | `machine_id`, `session_id`, `epoch`（可选）, `after_seq`（可选）：开始收这个会话终端的实时帧（hub `features` 里有 `terminal_stream` 才发）；`control: {cols, rows}`（可选，`terminal_control`）：手机按这个尺寸接管终端，`takeover: true` 连别的程序占着的也抢 |
| `terminal_input` | `machine_id`, `session_id`, `text`（终端字节，原样写进去）, `control: {cols, rows}`（可选，新尺寸）, `scroll`（可选，滚轮的行数，> 0 往回、< 0 往新，feature `terminal_scroll`）, `at`（可选，`[column, row]` 从 0 起，滚轮落在屏幕的哪一格：手指按下的地方；不带时 node 用屏幕正中间）, `click`（可选，`[column, row]` 从 0 起，屏幕上那一格左键单击，feature `terminal_click`）：只有正在接管的那个连接发的才转给 node |
| `terminal_unsubscribe` | `machine_id`, `session_id` |
| `register_phone` | `device_name`, `tools`（`[{"name","description","input_schema"}]`，即 MCP 工具定义）, `simulator`（模拟器只在没有真机连着时接工具调用）；可选 `device_id`、`platform`。按设备替换注册，空工具列表只清除该设备的供给；缺少 `device_id` 时用鉴权设备 ID，再回退到 `legacy`。调用先筛选提供该工具的在线手机，再优先真机、最近活跃连接。iPhone 的 `device_id` 是推送用的设备 id（同 `POST /api/v1/devices`）：连着的这段时间 hub 不给这台设备的实时活动推 APNs，App 自己画，`docs/live-activity.md`「推送节奏」；老 App 不带。`tools` 里的 `phone_runtime_cli`（DEBUG 包打开了经 hub 的 Runtime CLI）不算手机的工具：hub 把它拿出来，不进 Agent 的工具列表、Agent 调不到，只给 `POST /api/v1/phone/runtime-cli`；只登记了它一个（脚本驱动的模拟器）时这条连接不算手机。 |
| `register_desktop` | `device_name`：Mac App 登记成桌面端，不是手机——不接 `phone_*`，也不挡 hub 推送唤醒 iPhone（`hub/desktops.go`）；`device_id`（推送用的设备 id，同 `POST /api/v1/devices`：连着的这段时间 hub 不给这台 Mac 推「需要你处理」「Agent 做完」，它自己发本地通知；老 App 不带）。退出它就是吊销它的设备凭据（`DELETE /api/v1/auth/tokens/{id}`，hub 随即断开它的连接、删掉它登记的推送） |
| `tool_result` | `call_id`, `content`（`[{"type":"text","text"}|{"type":"image","data","mime_type"}]`）, `is_error` |
| `client_hello` | `device_id`, `platform`, `version`, `build`, `os: {name, version, sdk}`, `features`, `heartbeat: {interval?, pong_timeout?}`，均可省略，见下文 |
| `ping` | 必须带字符串 `id`，允许空字符串；缺失、null 或非字符串时不回复 |

### 常驻连接通知（ws_notify）

`notify` 是事件名，`ws_notify` 是双方声明的能力名。hub 复用 APNs 的提问和完成决策、屏蔽规则与设备偏好，在 `c.has("ws_notify")` 时才发给该连接。即使没有 APNs token 或密钥也能收到；旧 iOS 不声明就不会收到新事件。

`interaction` 表示需要处理的交互，`done` 表示 Agent 完成，`assistant` 表示助手完成。偏好按鉴权设备 ID 读取 `/api/v1/devices/{id}/prefs`：交互看 `input`，完成看原有完成策略；机器与空间屏蔽沿用已有判断。完成事件由 hub 决策，Android 不应根据 sessions 状态变化再次生成一条。原始 `interaction` 与 `interaction_resolved` 事件照旧发送，用于请求卡和撤回通知，不能把它们都当成新的提醒。

事件不持久回放，也不证明通知实际展示；断线后客户端仍须拉交互和会话核对。通知可靠性、锁屏展示和耗电未实测。

### 客户端能力与心跳

客户端看到服务端支持 `client_hello` 后可发送：

```json
{"type":"client_hello","device_id":"<设备 id>","platform":"android","version":"<版本>","build":1,"os":{"name":"Android","version":"<系统版本>","sdk":36},"features":["endpoints"],"heartbeat":{"interval":240,"pong_timeout":15}}
```

这份元数据只记录在当前连接，不改变已鉴权的设备身份或权限。`build` 当前接收任意 JSON。每次 `client_hello` 替换客户端的能力集合，省略 `features` 等于清空集合；不要用服务端能力列表代替客户端自己支持的能力。

下面的秒数均为代码中的协商边界和定时器配置，**未实测网络延迟、实际发包频率、耗电或运营商空闲超时**。

- 未协商时维持旧行为：hub 发 WebSocket 协议 ping 的间隔配置为 20 秒，等待协议 pong 的超时配置为 15 秒；不改变旧连接的 TCP keepalive 设置。
- `heartbeat.interval` 为整数秒，先夹到 10–900，再仅向下抖动：减去 `0..min(interval-10, floor(interval/10))` 的随机整数。每次协商只抽取一次，`client_hello_ack` 返回实际采用的值。
- `heartbeat.pong_timeout` 为整数秒，夹到 5–60；省略某项时保留上一值或默认值。提供空 `heartbeat` 对象也会标记已协商；整个字段缺省则保留原协商状态。
- 写协程应用新配置后返回 `{"type":"client_hello_ack","heartbeat":{"interval":<实际值>,"pong_timeout":<实际值>}}`。客户端重复发送可再次协商；即使只发送元数据、不带 heartbeat，也会收到当前值的 ack。
- 协商后同步尝试调整 TCP keepalive：Idle 用 interval、Interval 用 pong_timeout、Count 为 3；取决于底层连接是否支持。协议 pong 超时会关闭连接。同一时刻只等一个协议 ping，不阻塞应用层回复。

应用层探测另用客户端发起的 JSON 消息：

```json
{"type":"ping","id":"probe-1"}
{"type":"pong","id":"probe-1"}
```

第一条是客户端发，第二条是 hub 回，`id` 原样回显。先检查服务端的 `app_ping` 能力；它与 WebSocket 协议控制帧的 ping / pong 独立，不能互相替代。hub 不会主动发送 JSON `ping`，也不处理客户端 JSON `pong`。旧客户端不发 `client_hello` 时不会收到新增的 `endpoints` 事件或协商回执。

新字段必须可选，且不能与现有 iOS `Envelope` 的同名键使用冲突类型；例如 `id` 保持字符串，不能为了新协议改成对象。

### Codex 的提问和审批

Codex / TRAEX 在终端里没有 hook，node 自己从 rollout 读提问（`request_user_input_async` / `request_user_input`）、从屏幕读审批，做成普通的
`question` / `permission` 交互，带 `agent: "codex" | "traex"`；授权卡还带 `reason`（Codex 说的理由）和 `always_label`（「总是同意」会放行什么，
没有这个字段就没有「总是同意」）。回答格式和 Claude 的一样：`{"answers":…}`，`{"allow":true,"always":…}`，`{"allow":false,"message":"…"}`
（有 `message` 时停下这一轮、把它作为下一条消息发进去）。回答是同步的：hub 把它交给 node（`codex.answer`），node 确认电脑上还是这道题 / 这个审批、
TUI 回到了能作答的状态，输入后再确认 Codex 收下了才回成功；否则回 409 `input_draft` / `dialog_open` / `prompt_changed`（什么都没输入），
卡片留着（`prompt_changed` 除外）。细节见 `docs/codex.md`「提问和审批」。

### 需要填写的授权卡片

`permission` 交互可带 `fields: [{key,label,placeholder?,secret?,optional?}]`，App 用安全输入框填写密码，批准时发送
`{"allow":true,"values":{"password":"…"}}`。必填字段缺失时 hub 拒绝本次批准并保留卡片；交互历史的 secret 字段只记录 `<redacted>`，
原始值只交给等待中的工具处理。此类卡片的通知分类是 `INTERACTION_INPUT`（无快捷允许按钮），列表、手表和 Siri 不直接批准。

### 终端对话框（screen 交互）

Agent blocked 而没有 hook 交互时，hub 读一次屏幕（`session.screen`，40 行）建一张 `screen` 卡：

- 什么时候建：没有 Jarvis hook 的 Claude Code（用户自己的 profile 启动的）一 blocked 就读屏幕建卡；有 hook 的会话（这次 hub 启动后发过 hook 事件，
  或者是 Jarvis 起的）先等 1.5 s，让 hook 自己的卡先到（Codex / TRAEX 等 3 s）。hook 的卡晚到时照旧替掉屏幕卡。
- 答完一张屏幕卡后 hub 每 250 ms 再看一次屏幕，最多 2 s：Agent 接着又问（AskUserQuestion 的下一道题、下一个授权）时 herdr 可能一直是 blocked、
  没有状态变化，靠这一下给新的对话框建卡；看到的还是刚答的那个（按键还没落地）或没有对话框就接着看，Agent 不再 blocked 或已有别的卡就停。
- 整条链路的实测和不限流的约定见 `docs/server.md`「卡片的快车道」（#751）。

- hub 用 `server/internal/screen` 解析对话框，放进 `interaction.dialog`：`header`、`question`、`detail`、
  `options[{key,label,detail,kind,keys}]`（`kind`：`type_something` / `chat` / 空）、`rev`（问题和选项的哈希，不含光标和转圈）。
  有编号的选项按数字；Claude 2.1.312 信任目录那种不带编号的列表按光标算 `up/down` + `enter`。卡片 id 带上 `rev`：对话框换了就是新卡，
  上一张卡里选的不会带过来。
- App 回答带 `option`（`type_something` 再带 `text`），不再自己猜按键；hub 把 `rev` 和 `option` 交给 node 的 `session.keys`，
  node **按键之前重读屏幕、重新解析、比对 `rev`**，不一致就什么都不按，回 409 `{"code":"prompt_changed"}`，hub 随即按当前屏幕再建一张卡。
  按键也在 node 上按当时的屏幕算（光标在电脑上被挪过也不会选错）。
- 兼容：旧 App 不认 `dialog`，照旧自己解析 `screen`、发 `keys`——hub 也会带上 `rev`，同样先核对；旧 node 不认 `rev` / `option`，
  hub 同时传算好的 `keys`，行为同以前。没解析出对话框的卡（只有终端和按键栏）不核对。
- 屏幕卡的回答是同步的：hub 等 node 按完才回 App（以前是先回成功再在后台按）。
- `rev` 由 hub 和 node 各自用同一份解析代码算：改了 `internal/screen` 的解析就要 hub 和所有 node 一起部署，否则新卡永远对不上（每次都 `prompt_changed`）。
- 手表有 `dialog` 时也按 `option` 回答（Siri「批准第一个待处理」不处理屏幕卡）。
- **多选**（Claude Code 2.1.316 的 AskUserQuestion `multiSelect`，`docs/claude-integration.md`「多选提问的屏幕卡」）：`dialog` 多带
  `multi: true`、`submit`（`Submit`，后面还有题时是 `Next`），选项多带 `checked`（电脑上已经勾了）、`text`（自由回答框里已经写的字）；
  多个问题时 `steps[{label,done}]` 是顶上的标签栏；提交前的确认页是 `review: true`（`incomplete: true`：还有题没答），选项是「Submit answers / Cancel」。
  `checked`、光标、写的字、标签的勾都不进 `rev`。
  App 回答带 `options`（要勾上的选项的 key，没列的会被取消）和 `text`（自由回答，空就不勾它）。hub 交给 node 的 `session.keys`
  （`multi: true`、`options`、`free_text`），node 一次按一个键、每按一次重读屏幕，最后按 Submit / Next；确认页上所有题都答了就再按「Submit answers」。
  旧 App 对多选卡只发一个 `option`：当成只勾这一项。
- 密码：屏幕最后一行是密码提示（`[sudo] password for …:`、`Password:`、ssh 口令…，规则刻意收窄，正文里提到 password 不算）时卡片带
  `secret: true`，App 用密码框（不进草稿）。不管是不是密码，终端卡回答里的 `text` 在数据库里一律存成 `<redacted>`；
  终端面板的按键接口（`/keys`）本来就不落库，hub 的请求日志也只记路径和状态码。

### 会话增量（`sessions_delta`，2026-11-02）

一台机器的会话列表有上百个会话、每个约 1 KB，一次推送通常只变一个会话；整表推送让手机每分钟下载、解码约 2.6 MB（`docs/server.md`「推送给 App 的量」）。

- App 连 WebSocket 时在 URL 上带 `?features=sessions_delta`。hub 有这个能力（`hello.features` 里有 `sessions_delta`）就：
  1. 连上时每台机器发一次整表 `sessions` 当快照，**不带 Codex 历史会话**（`source_kind: codex_history`，App 从 `GET /api/v1/sessions` 拿）；
  2. 之后只发 `{"type":"sessions_delta","machine_id","upsert":[Session…],"remove":[session_id…]}`：新的或变了的会话（整个对象，不是半个），和没了的 id；
     什么都没变就不发。Codex 历史会话只有 App 已经有它时才在增量里（实时的 Codex 会话结束、同一个 id 变成历史）。
- hub 按连接记着每个 App 手里每台机器的每个会话（`client.have`），快照和增量在同一把锁里算、入队，所以增量永远对着 App 真有的东西算。
- 断线重连就是新的快照，漏掉的变化靠它补齐。
- App（`SessionPatch`、`AppModel.applySessions`）：快照替换这台机器的列表，但留着已有的 Codex 历史；增量在原位换掉变了的、新的放在后面、删掉没了的；
  socket 连着时 `GET /api/v1/sessions` 的回答比推送旧，只从它拿 Codex 历史（历史里没了的删掉），不覆盖推送来的会话。
- 兼容：老 App 不带 `features`，照旧收整表（一模一样的不发）；老 hub 不认 `features`，照旧发整表，App 的 `hello.features` 里没有 `sessions_delta`，按整表处理。
  mock（`scripts/mock/mock_hub.py`）同样支持，`MOCK_WHOLE_LISTS=1` 当老 hub。
- 回放（真实 hub 录的 19 分钟）：会话推送每分钟 2605 KB → 79 KB，连上时的快照 602 KB → 227 KB（`docs/testing.md`「推送回放」）。

### 新建会话

开 herdr 标签页、跑启动器、等 Claude 的输入框出来要几秒到一分钟（`~/.zshrc` 慢、信任目录对话框），以前 App 的「开始对话」按钮一直转到 Claude 就绪，
hub 还要先等小模型起完标签名（约 2 秒）。现在 `POST /api/v1/sessions` 带 `async`：

- node 只做便宜的检查（启动器名、目录存在、权限模式），记下会话（`sessionState`，对话页取历史回空列表、订阅等 transcript 出现），马上回会话；
  开标签页、跑启动器、等就绪、发 `prompt` 在后台做（最多 2 分钟）。
- 启动期间会话列表里有它：`status: idle`、`starting: true`（不算一轮，不计时、不推送「完成」）。启动失败就变成 `offline` 加 `start_error`，留 10 分钟。
- 启动期间发来的消息（`session.send`）在 node 上等它就绪再按顺序打进去；hub 给这种发送 3 分钟（平时 90 秒）。启动失败时这些发送拿到同一个错误，App 的气泡变成「重发」。
- App 的第一条消息不放在 `prompt` 里，而是进对话后当普通消息发（马上上屏、带发送状态），起名用 `name_hint`。对话页输入框上方显示「正在启动 Claude…」或失败原因（`StartingNote`）。
- node 重启会丢掉还在启动的记录：这时会话既不在列表里也没有 transcript，App 那边的发送会失败、可以重发。
- Codex 的启动器（`agent: "codex"`）不走 `async`：Codex 收到第一条消息才建 thread、定下会话 id，所以 App 把第一条消息放在 `prompt` 里同步新建，
  node 等到这个 thread 出现才回（最多 60 秒），回的就是它的会话。拒绝的 `code`：`codex_needs_prompt`（没带 `prompt`，或带了 `async`）、
  `codex_launcher`（跑 Codex 的 shell 函数）、`codex_resume`（想用 Codex 启动器恢复会话）。见 `docs/codex.md`「从手机新建」。

### 发消息

往别人的终端里打字是这个 App 风险最大的操作。node 在往正在跑的 Agent 窗格里打字之前先读一次屏幕（`server/internal/node/sendsafe.go`、
`server/internal/screen`），以下情况**什么都不打**，返回 409：

| `code` | 什么时候 | 其他字段 |
| --- | --- | --- |
| `dialog_open` | 有对话框占着键盘：权限确认、信任目录、模型选择、rewind…（底部有 `Esc to cancel` / `Enter to confirm`，或输入框下面有带光标的编号选项） | `delivered: "none"` |
| `input_draft` | Claude 的输入框里有电脑上没发出去的字、bash 模式（`!`）、粘贴占位、被窗格截掉的长草稿 | `delivered: "none"`、`draft`（输入框里的字）、`clearable`（只有一行字时才有） |
| `open_elsewhere` | 要恢复的 Claude 会话正在别处开着：有活着的 claude 在 `sessions/<pid>.json` 里开着它（Claude 桌面版、Persona 不驱动的终端）。再 `claude --resume` 会让两个进程同时写这个会话 | `delivered: "none"`、`pid` |
| `engine_missing` | 要新开或恢复会话，这台电脑的引擎（herdr）却没在运行或没装 | `delivered: "none"`、`detail`（引擎自己的报错） |
| `runtime_away` | 会话属于一个 Agent 运行时（DSH…），它现在没连上 node（「Runtime ↔ node」） | `delivered: "none"` |

- 灰色的建议输入（Claude 预测的下一句、新会话的 `Try "…"`）不算草稿：要用带颜色的读屏（`pane.read` 的 `format: ansi`）看 SGR 2（dim），
  纯文本里两者一模一样。读不到颜色时一律当草稿。规则移植自 Ovis 的 `claude-input.ts`（MIT）。夹具：`server/internal/screen/testdata`。
- `clear_draft`：用户在 App 里看到草稿后选「清空并发送」，App 带上收到的 `clearable` 重发同一条（同一个 `client_msg_id`）；
  node 只在输入框里**还是这段字**时按 `ctrl+k ctrl+u` 清掉，再读一次确认空了才发。多行草稿只能在电脑上处理（这两个键一次只清一行）。
- `client_msg_id`：App 每条待发消息一个 UUID，自动重发（没收到回应、断网后补发）沿用。**去重在 hub**（`hub/send.go`，Postgres 表
  `sent_messages`，留一天）：发送中再来同一个 id 等第一次的结果；已送达的直接回成功；被拒或离线（什么都没打）的忘掉，可以再发；
  打字途中断了（node 回了 `delivered: "unknown"`，或请求已发给 node 却没等到回应：连接断、超时）回 502 + `delivered: "unknown"`，
  同一个 id 再发只回同样的错误，不会打第二遍。hub 重启、node 重连之后也是这样（以前去重只在 node 进程的内存里，重连后的新进程什么都不知道，
  2026-10-31 实测重连途中发的消息到了电脑上、App 却收到「rpc: connection closed」）。用户看过对话、确认没到再点重发时，App 换一个新 id。
  App 等不及断开了，hub 照样等 node 的结果记下来（发送不随请求取消）。node 自己也记 10 分钟（同一条连接内的重试）。
- 同一个会话的发送一条一条来（node 的 `typingLocks`）：两条同时进来（连点、两台设备），以前第二条读屏时第一条还在输入框里（herdr
  `agent.prompt` 先粘贴、约 300 ms 后才回车），被当成草稿拒掉，还提示「清空并发送」——清掉的是用户自己的第一条。读屏看到的是 5 秒内刚打进去的字
  （或长文本的 `[Pasted text #1 …]`）时，等它被收走（最多 2 秒）。
- 离线会话 `--resume` 拉起后同样先读屏：Claude 起来就停在对话框上时回 `dialog_open`，不往对话框里打字。
- Codex / TRAEX 只查对话框（`› 1.` 这样的编号选择），不查草稿：输入框里有字时消息会接在后面。旧 App 只认 `error` 字段，看到的就是拒绝原因。
- 链路和耗时见 `docs/archive/lanes/session-verify.md`。

### 待发消息的对应

App 发出的消息在对话记录里出现之前显示一个待发气泡。Claude Code、Codex、runtime 给用户那一行的 item id 都是自己的，不是
`client_msg_id`，以前三端只能按字认（文字被改写、一样的话连发、`/compact` 变成通知时只能靠「送达 60 秒后收起」兜底）。
现在由 node 认出那一行，给它的每个 item 带上 `client_msg_id`（`server/internal/node/echo.go`），推送（`items` 事件）和分页（`/items`）都带：

- **什么时候记**：`session.send` 带 `client_msg_id`、送到了（或 `delivered: "unknown"`，可能送到了）时，node 记下这次发送：
  会话、发送前 transcript 的最后一行（打字前先读一遍新行）、文字的开头 200 字（去掉 `<jarvis-context>`、图片说明这些附加的东西）、图片张数、时间。
  runtime 的会话没有本地 transcript，「最后一行」是 node 正在推送时推到的 seq，没有就只看时间。
- **怎么认**：按发送的顺序，各自取发送之后、还没被认领的第一行**用户行**里字对得上的：文字少于 12 个字（去掉空白）要完全一样，
  否则比前 12 个字（页会截长文本，Claude 把图片变成 `[Image #1]`）；只有图片的消息对没有字的那一行；只发了命令（`/model`）的对
  在终端里补了参数的那条（`/model opus`）；`/compact` 对它变成的「压缩上下文」通知。用户行包括 mid-turn 被吸收的 `queued` 那条，
  不包括别的会话发来的（`from`）和后台任务通知。时间早于发送 5 秒以上、或晚于发送 2 小时的行不算（Codex 在一轮结束时才写排队的消息）；
  2 小时没认到就不再找。
- **一条消息拆成几个 item**：同一行的 item 共用 id 里 `#` 前面那段（Claude 的 uuid、Codex 的 item id），这一行的文字、图片都带上。
- **没落成用户行**：`/compact` 之外没有对应行的（别的命令、被丢掉的输入）认不到，item 不带字段，App 照旧兜底；两条一样的话、
  前一条没落成用户行时，后一条那一行会被前一条认走（只是气泡收早了）。
- **node 重启**：node 把记下的和认到的告诉 hub（事件 `session.echo`），hub 跟这次发送一起存在 `sent_messages.echo`（一天）；
  node 连上时 hub 先把这台机器一天内的交回去（`session.echoes`），再列会话、恢复推送。所以 node 重启（ssh 的 node 随 hub 重启）后
  认过的行照样带字段，重启前还没认到的接着认。hub 没有数据库时只在 node 进程的内存里。
- **App 怎么用**：带 `client_msg_id` 的 item 收起 id 相同的那条待发消息（不管 role、kind），也不会再按字去认另一条；
  不带的照旧按字认（旧 hub、node 没认出来的）。iOS `PendingMessage.tied`、Android `MessageEcho.tag`、网页 `lib/echo.ts`。

### 发送文件

App 对话页输入框的「+」›「文件」（Agents 和「对话」tab 的助手都有）：选的文件传到**会话所在那台电脑**的临时目录（node 的
`os.TempDir()`：Linux 一般 `/tmp`，macOS 是 `$TMPDIR`，ssh 进来的 node 没有这个变量时也是 `/tmp`），把绝对路径插到输入框的光标处，
**不自动发送**。助手跑在它所在的机器上（默认 hub 机器），文件就传到那台。图片照旧走图片附件。

- `POST /api/v1/machines/{mid}/files?name=<原文件名>`，body 是文件本身（必须有 `Content-Length`）→ `{path, name, size}`。
  上限 200 MB（`proto.MaxUploadBytes`）：超过回 413 `code: too_large`（App 选完就先查大小，不会传）。
- hub 边读 body 边转：每 1 MB 一个 `file.upload.chunk`（base64 在一行 JSON 里），整个文件不进内存、也不塞进一条 RPC。
  App 取消上传（或断线）时 hub 发 `file.upload.abort`，node 删掉写了一半的文件；node 2 分钟收不到块也自己删。
- node 先写 `.jarvis-upload-*` 隐藏文件（0600），收齐后核对字节数，再改名为 `原名-YYYYMMDD-HHMMSS.扩展名`（`.tar.gz` 这类整体算扩展名），
  同一秒重名再加 `-xxxx` 随机串。文件名里的 `/`、`\`、控制字符换成 `_`，超长的截短。
- 旧 node 不认 `file.upload.*`：409 `code: node_outdated`（「<机器> 上的 personad 太旧…需要重新部署」）；旧 hub 没有这个接口（404），
  hello 的 `features` 里也没有 `file_upload`：App 提示更新 hub。
- 路径带空格或引号时 App 用单引号包起来，多个路径空格分隔。输入框没在编辑时插到末尾（未聚焦的 TextField 报的选区不可靠）。
- 前面挡着反向代理时注意它的请求体上限（有的 CDN 免费版只放 100 MB）。

### items 的连续性

每批 items（WebSocket `items` 事件、HTTP 的 `/items` 响应、node 的 `session.items`）都说明自己在 transcript 里的位置：

- `history`：这份 transcript 的身份（inode + 第一行的 crc32 + 被改写的次数）。变了说明文件被替换 / 截短重写，App 丢掉本地内容重新取。
- `to_seq`：这批读到的最后一行（包括没有 item 的行）；`from_seq`：这批接在哪一行之后（只有接续的批次——补发、实时推送、`after_seq`——才有）。
- App 记 `covered`（本地从头连续拿到的最后一行）：`from_seq <= covered` 就把 `covered` 推到 `to_seq`；`from_seq > covered` 说明中间丢了一批，
  稍等 1.5 秒（订阅时的补发可能还在路上）再用 `after_seq=covered` 补齐，超过 8 页改取最新一页。重连订阅带 `since_seq=covered`。
- 老 hub 不带这些字段：App 照旧按 id 合并。新 hub 的字段对老 App 只是多出来的键。

### 真实记录的轮次用时

轮次结束记录可生成一条 `role: system`、`kind: turn_duration` 的元数据 item。它不带展示文本，沿用现有 HTTP 历史／增量页、WebSocket `items` 和会话缓存路径，不新增事件或 Envelope 顶层字段。`turn_duration` 字段可选；旧 iOS 将未知 kind 解为 `unknown` 并跳过展示，其余内容照常解码。

```json
{"id":"duration_example#0","seq":9,"ts":"2026-01-01T00:00:00Z","role":"system","kind":"turn_duration","turn_duration":{"duration_ms":12345,"source":"claude_turn_duration","parent_uuid":"reply_example"}}
```

| 来源 | 原始记录 | 关联字段 |
| --- | --- | --- |
| `claude_turn_duration` | Claude Code 的 `type: system`、`subtype: turn_duration` 行中的 `durationMs` | `parent_uuid` 原样保留 `parentUuid`，指向该轮收尾消息的 UUID（对应 item id 中 `#` 之前的部分） |
| `codex_task_complete` | Codex／兼容 rollout 的 `event_msg`、`payload.type: task_complete` 中的 `duration_ms` | `turn_id` 原样保留原生轮次 ID；同轮已解析到用户输入时，另带 `user_item_id`，指向该输入的规范化 item id |

仅接受明确记录的非负整数毫秒数及有效关联标识；缺失、负值、非整数、溢出或已知轮次 ID 不匹配时不生成耗时 item。`0` 是有效记录值。不会用时间戳、`started_at`／`completed_at` 的差值或 timeout 补值，也不将压缩、思考、工具或子任务的耗时当作整轮耗时。

Android 按关联消息定位已加载历史中的用户轮次或后台唤醒段，工作行显示「用时 N 秒」（毫秒向下取整）。关联消息未加载、关联跨过了新用户轮次／唤醒／回顾边界时先隐藏；翻页补齐后重新关联。新轮次没有自己的记录时，状态行留空，不沿用上一轮用时。字段／item 缺失表示没有可用的真实记录，不代表零耗时；旧 node 或旧缓存也可能没有该信息。折叠工作保留耗时元数据 item；展开折叠、重连补发和缓存恢复沿用同一关联规则。

### 折叠的工作

对话里一轮的工具调用和思考平时折叠成一行（「工具 502 · 思考 81」），可一个很长的 Codex 会话，最后两轮这一页有 1412 条、1.08 MB，
真正的回复只有 6 条：弱网下对话页顶上一直转圈。请求带 `fold_work`（HTTP `fold_work=1`、订阅时 `fold_work: true`）时，从末尾往前的页
（`turns` / `before_seq` / 订阅时补发的最后两轮）把每一段连续的工作——成对的 tool_use + tool_result（连同工具的图片）和 thinking——
换成一条 `kind: fold` 的 item（`transcript.FoldWork`）：

- `id` 是 `fold:` + 这段第一条的 id，`seq` / `ts` 也是第一条的；`fold.through_seq` / `through_id` 是最后一条，`until` 是最后一条的时间；
  `items` 条数，`tools` 工具调用数，`thoughts` 有字的思考数（空的思考 App 本来就不显示）。
- `fold.tasks` 是可选的有序任务清单快照数组，仅收录本段中成功的 TodoWrite / TaskCreate / TaskUpdate / TaskList 结果，
  不携带原始工具输入和输出；无可用快照时省略。没有结果、`is_error: true`、结果中 `success: false` 或非 null 的 `error` 都不计入；
  格式错误的清单不会清空之前的任务。旧 iOS 忽略这个嵌套字段，fold 原有计数、范围和展开方式不变。
  每次成功结果保留一个快照及其替换边界，按结果在 transcript 中的顺序应用，避免部分更新丢掉已加载的任务状态：
  - `entries`：任务数组，每项有 `id`（`todo:<下标>` 或 `task:<工具任务 ID>`），可选 `subject`、`description`、`status`。
    状态为 `pending` / `in_progress` / `completed` / `unknown`；`deleted` 表示删除该任务。
  - `replace: "todo"`：TodoWrite 的完整清单，先移除旧的 `todo:` 项；空数组表示清空。
    `replace: "task"`：TaskList 的完整清单，移除不在新清单内的 `task:` 项，已有项保留未提供的说明和状态；空数组表示清空。
    省略 `replace` 表示 TaskCreate / TaskUpdate 的增量结果，未提供的字段沿用旧值；无旧值的标题显示「任务 #ID」、状态为未知。
    TaskCreate 另带 `fallback_status: "pending"`，仅在结果和旧任务都没有状态时使用。
  - `source_item_id`、`source_tool_use_id`、`source_seq`：原始工具调用的 item ID、工具调用 ID 和 seq，供「查看相关记录」定位。
    Android 不展开即可重建进度；查看记录时先展开所属工作行，再按范围取回原记录并定位，后续真实结果按原规则继续更新。

  例如：`"tasks":[{"replace":"todo","entries":[{"id":"todo:0","subject":"检查表单","status":"in_progress"}],"source_item_id":"entry_tool#0","source_tool_use_id":"call_example","source_seq":3}]`。
- 不折叠：用户消息、回复（text）、通知、recap；结果不在这一页的 tool_use（还在跑）和 tool_use 不在这一页的 tool_result
  （App 要把它们和后来 / 之前的另一半配对）；不到 3 条且不到 4 KB 的短段（展开还要一次往返，不值）。
- App 展开工作行时用 `from_seq=<fold.seq>&through_seq=<fold.through_seq>` 取回这一段（一页约 1000 条，`more` 时从最后一条的 seq + 1 接着取），
  合并进来并去掉 fold。合并一条 fold 时，本地在它范围内（按 seq + 块序号）的工具 / 思考 item 被它替换。
- `after_seq` 的补发和实时推送从不折叠：它们接着 App 已有的内容，里面可能有 App 正等着结果的工具调用。
- 1412 条 / 1.08 MB 的那种页，折叠后是十几条、几 KB（`TestFoldedFirstPageOfALongSession`：1417 条 880 KB → 13 条 1.9 KB）。

### 终端流

终端 tab 的实时画面（`docs/terminal.md`）。node 用 `herdr terminal session observe` 按窗格本身的尺寸镜像 herdr 画好的网格，
每帧是一段 VT 字节：`full` 的帧是整屏（`ESC[2J` 开头），其余只重画变了的格子，最后是光标位置和显隐。App 用一个 `cols × rows`
的网格依次套用即可；网格永远等于电脑上窗格的尺寸，手机的尺寸不回传。

- `epoch` 标识一条流：node 每次（重新）开 observe 都换一个，第一帧一定是 `full`；窗格变大小、herdr 中断、整帧之后的增量超过 256 KB 时 node 会重开。
  同一 `epoch` 里 `seq` 从 1 连续递增。
- hub 给每个被看的终端留「最近一个整帧 + 之后的增量」。`terminal_subscribe` 带上 App 手里的 `epoch` 和最后的 `seq`：
  还在同一条流、而且留着的帧能接上，就只补之后的帧；否则从整帧开始重放。之后的新帧实时推。
- App 收到的帧 `seq` 不连续（中间丢了）或者 `epoch` 变了却不是 `full`，就丢掉本地网格、重新 `terminal_subscribe`（不带 epoch）。
- 收到 `error`：hub 已经丢掉这条流；`unsupported` 时改用 `/screen` 轮询，否则过几秒再订阅。WebSocket 断了重连后重新订阅（带 epoch / seq 续上）。
- 第一个订阅的 App 让 hub 发 `terminal.watch`，最后一个退订（或断开）时发 `terminal.unwatch`。旧 App 不发这些消息，照旧轮询 `/screen`。
- 手机接管（`features` 有 `terminal_control`，`docs/terminal.md` 第十一节）：订阅带 `control` 时网格是手机的尺寸，帧格式不变；
  键和新尺寸走 `terminal_input`；离开（退订 / 断开）后窗格回到电脑上的尺寸，还有别人在看就改回镜像（新 epoch）。

## 引擎

每台机器的 node 驱动一个「引擎」，即一组终端宿主：`herdr`（经典的 herdr 命令行，默认，原来的机器不受影响）、
`herdr-core`（Herdr 桌面版背后的运行时，显示名「Herdr App」）、`orca`（Orca 桌面版的 runtime，显示名「Orca」，`docs/plan-orca-support.md`「Orca 来源」），
以及 `herdr-both`（显示名「herdr + Herdr App」）：node **同时注册 herdr 和 herdr-core 两个宿主**，两边会话合并显示，
给「本地用 herdr、Herdr App 再 remote 连到同一台机器」的场景用。单选时几个宿主不同时用：同时跑会互相拖慢刷新；
并列时刷新并行、事件按宿主隔离，不会互相拖慢（实现见 `node/engine.go`、`node/sessions.go` 的 `refreshHosts`、`node/herdrevents.go`，
取舍见 `docs/plan-orca-support.md`「herdr-core 宿主」）。

- 用户在 App 里按机器选（配对时、设置 › 机器），`PUT /api/v1/machines/{mid}/engine`。hub 存在 Postgres 的 settings 里
  （一个键 `machine_engines`：机器 id → 引擎），不写 hub.json。
- hub 每次连上 node，**第一个调用**就是 `engine.use`（在 `node.info` 之前），但只对存过选择的机器发；没选过的机器不发，
  node 照旧用 herdr，旧 node 也不会收到不认识的方法。用户改了且机器在线时马上发；同一台机器的 `engine.use` 排队发，每次发的是发送时存的值。
- node 连上后先不轮询、不订阅任何引擎的事件，等到引擎定下来：收到 `engine.use`，或者 hub 不发它而直接 `sessions.list`（没选过 / 旧 hub），
  最多等 3 秒，之后按 herdr 开始。所以选了 herdr-core 的机器连接时不会闪出 herdr 的会话，也不会去问 herdr。
- `engine` 是存下的选择。node 拒绝 `engine.use`（太旧）时 hub 记一行日志，PUT 照样回 200，选择照样存着，等 node 更新后生效。
- `engines` 来自 `node.info`，每项 `{id, name, available, version?, note?}`：herdr 能用 = 它答话，或者 socket 文件在，或者 PATH 上有 `herdr`；herdr-core 能用 = 它的 socket 答 `hello`
  （用 herdr 的 node 只在 `node.info` 时这样问一次 herdr-core，别的时候不碰它）；`herdr-both` 能用 = 前两个都能用（不带 version）；
  Orca 能用 = `orca-runtime.json` 在、它的 socket 答 `status.get`
  且协议版本兼容（不兼容时 `available` 为 false，`note` 是给用户看的一句中文原因，`version` 照样带；同样只在 `node.info` 时问一次）。
  `version` 是各自报的版本，没有就不带。旧 node 只报前两个。
- 并列时没有窗格的操作（新建、继续会话、改名）固定走经典 herdr（node 的 `defaultHost` / `defaultHostKind`）；
  每个窗格的操作（读屏、发送、按键、关闭）按窗格 id 的 host kind 路由，herdr-core 的窗格只受 herdr-core 能力限制。

## hub ↔ node RPC

`ssh <host> ~/.persona/bin/personad node --stdio`（hub 机器本机直接起子进程）。stdin/stdout 每行一个 JSON。
双向都能发请求：`{"id":"h12","method":"…","params":{}}` → `{"id":"h12","result":{}}` / `{"id":"h12","error":{"message":"…"}}`；
单向事件 `{"event":"…","params":{}}`。hub 发的 id 以 `h` 开头，node 发的以 `n` 开头。stderr 是 node 日志。

hub → node 请求：

| method | params → result |
| --- | --- |
| `node.info` | → `{hostname, os, arch, home, herdr_version, claude_version, version, system, engine, engines}`（`system` 见 Machine，node 启动时读一次；`engine` 是 node 现在用的引擎，`engines` 是它找到的，见「引擎」；旧 node 没有这两个） |
| `engine.use` | `{engine}` → `{engine}`：换成这个引擎（不认识的报错）；和现在一样时什么都不做，否则换掉宿主、丢掉旧引擎的 Agent（切换前开始、切换后才回来的刷新结果作废）、结束所有终端流（每个推 `terminal.end`，error 说引擎换了）、换事件流，并推一次 `sessions.changed`。多个 `engine.use` 依次执行 |
| `launchers.list` | → `{launchers}` |
| `launchers.models` | `{name, agent_type?, refresh?}` → `{agent, models, locked?}`：同 REST 的 `…/launchers/{name}/models`；`agent_type` 是 hub 配置里这个启动器的类型（不给就按 node 自己认出的） |
| `skills.list` | → `{skills: [{name, description, source}]}`：这台机器上助手拿到的 skill（`~/.persona/skills/` 里用户的，加上内置的；`docs/hub-update.md`）。老 node 没有它时 hub 当作装了 lark-connector（老版本把它编在二进制里） |
| `dirs.list` | `{path}` → 同 REST |
| `sessions.list` | → `{sessions}` |
| `session.start` | `{cwd, launcher, agent_type?, prompt?, resume_session_id?, workspace_id?, permission_mode?, effort?, tab_name?, async?}` → `{session}`；`async` 立刻回、后台启动（`node/starting.go`）；Codex 启动器同步、必须带 `prompt`（`node/codex_start.go`） |
| `session.send` | `{session_id, text, images?, client_msg_id?}`（`client_msg_id` 见「待发消息的对应」） |
| `session.echoes` | `{echoes: [{session_id, client_msg_id, text, images, sent_at, after_seq, line?}]}` → `{ok}`：hub 交回这台机器一天内记下的发送（node 每次连上时，在 `sessions.list` 之前）；node 已经知道的跳过。旧 node 回 unknown method，hub 不管 |
| `session.keys` | `{session_id, keys, text}` |
| `session.interrupt` | `{session_id}` |
| `session.screen` | `{session_id, lines, source, format?}` → `{text, status}`（`format: "ansi"` 带颜色） |
| `terminal.watch` / `terminal.unwatch` | `{session_id, control?: {cols, rows, takeover}}`；watch → `{cols, rows, control}`，之后 node 推 `terminal.frame`；已经在推时 watch 会重开一条流（新 epoch、从整帧开始）。herdr 没有 observe 时报 `unsupported: …`。带 `control` 时 node 用 `herdr terminal session control` 接管（回包 `control: true`；旧 node 没有这个字段），槽位被占着又没 `takeover` 时报错 `code: attach_busy` |
| `git.pull` | `{cwd}` → `{status, updated}`，同 REST 的安全拉取 |
| `fs.list` / `git.status` / `git.diff` / `git.show` / `git.log` | 同 REST（参数同名） |
| `file.read` | `{path, cwd, offset}` → 同 REST 的 `file`（旧 node 不认 `offset`，回包也没有 `offset` / `next`） |
| `fs.stat` | `{paths, cwd}` → `{stats}`，同 REST 的 `stat`（旧 node 报 unknown method，手机就不链接任何路径） |
| `session.items` | `{session_id, before_seq, limit, turns, after_seq, lazy_images, fold_work, from_seq, through_seq}` → `{items, has_more, more_after, history, to_seq, from_seq}`（带 `through_seq` 时是折叠的那几行：`{items, more}`）（`from_seq` 只在 `after_seq` 请求里有；不带 `lazy_images: true` 时 node 把图片数据读回来，兼容旧 hub） |
| `session.image` | `{session_id, item_id}` → `Image{mime, data}`（node 的 item 不常驻图片数据，按记下的行偏移重读那一行） |
| `session.item` | `{session_id, item_id}` → `Item`（全文，不带图片） |
| `session.watch` / `session.unwatch` | `{session_id, since_seq?}`；watch 后 node 每 250ms 读一次新行，推 `session.items`（带 `history` / `from_seq` / `to_seq`）；有 `since_seq` 时先按 500 条一页补发其后的全部内容 |
| `session.close` | `{session_id}` |
| `file.upload.begin` | `{name, size}` → `{upload_id, chunk_bytes}`（超过上限报 `too_large`） |
| `file.upload.chunk` | `{upload_id, offset, data}`（data 是 base64，offset 必须接着已收到的）→ `{received}`；出错整个上传作废 |
| `file.upload.end` / `file.upload.abort` | `{upload_id}`；end 核对大小、改名 → `{path, name, size}` |
| `interaction.resolve` | `{id, resolution}` |
| `port.probe` | `{port}` → `{port, listening, http, title?}`：GET / 一次（Host 是 `localhost:端口`） |
| `port.dial` | `{id, port}` → `{}`：连上本机这个端口（扫描时它绑的地址，否则 127.0.0.1 再 ::1，只连本机），开一条流 `id`；没在听时报错 `code: port_gone`。之后两边用下面的 `stream.*` 事件传字节 |
| `credential.gh_token` | `{host}` → `{token}`：这台机器上非交互地跑 `gh auth token --hostname <host>`，给 GitHub 连接器（`github.auth.from_machine`）。只有 hub 能调（hook / MCP 的 `node.sock` 不认）；token 不写日志。没装 gh 报 `code: no_gh`，没登录（或登录在钥匙串里、经 ssh 读不到）报 `code: not_signed_in`；旧 node 报 unknown method |
| `connector.list` | → `{connectors:[{id, status, error?, guide?, via}]}`：这台电脑自己能提供的连接器（现在只有 Mac 上的 `imessage`；别的系统回空列表）。`status` 是 `ok` / `not_configured`（没有「信息」的数据）/ `no_permission`（没有完全磁盘访问权限，`guide` 是在这台 Mac 上一步步怎么开，按 `via` 是 `ssh` 还是 `launchd` 常驻 node 分两种说法）/ `error` |
| `connector.chats` | `{id, limit?, before?, keep?}` → `{chats:[{id, name, group?, members:[{handle, name?}], services, last, preview?, from_me?, unread?}], more?, before?}`：会话，新的在前；一个人在 iMessage / 短信 / RCS 上的几个会话算一个，群各算一个；`id` 是哈希（`imessage-<12 位>`），不含号码。只读。`keep: contacts`（收件箱、助手用）只列交给 Persona 的会话（见 `connector.changes`），App 自己看时不带 |
| `connector.older` | `{id, chat, limit?, before?, keep?}` → `{chat, messages:[{id, at, kind: message\|event, from_me?, sender?, text?, service?, attachments?:[{kind, name, mime, size}], reply_to?, edited?, unsent?, unread?, undecoded?}], more?, before?}`：一个会话最新的一页或 `before` 之前的一页，旧的在前；点按回应不算消息，附件只有描述。没有权限报 `code: no_permission`，没有这个会话（或 `keep: contacts` 时它不交给 Persona）报 `code: no_chat`。只有 hub 能调、不写日志（不记号码和正文）；旧 node 报 unknown method |
| `connector.changes` | `{id, cursor?, limit?, days?, keep?}` → `{batches:[{chat, messages}], next, more?}`：`cursor`（消息 ROWID）之后进来的消息，按会话分批、旧的在前，给 hub 的知识库和收件箱（「交给 Persona」开着时）。第一次（没有 `cursor`）从 `days`（默认 30）天前开始；`keep` 默认 `contacts`：只给成员里有通讯录里的人、或者我说过话的会话，`all` 全给；验证码打码；`next` 越过所有读过的（包括没给的） |
| `connector.search` | `{id, query, limit?, days?, keep?}` → `{hits:[{chat, message}]}`：最近 `days`（默认 365）天里文字含 `query` 的消息，新的在前（助手的 `imessage_search`）；`keep`、打码同上。只匹配，不把 `query` 拼进 SQL |
| `codex.answer` | `{session_id, id, resolution}`：回答 node 列出的 Codex 卡片（`docs/codex.md`「提问和审批」）；拒绝时是带 `code` 的错误，什么都没输入 |

流（`internal/streams`，两个方向都有）：`stream.data {id, data}`（data 是 base64，一次最多 32 KB）、`stream.ack {id, n}`（写出去了 n 字节；
每个方向没确认的最多 512 KB，发的一方到了就停下等）、`stream.close {id, error?}`（这一端的连接断了；对方把已收到的写完再关）。hub 先登记流再发 `port.dial`，node 连上、登记好才回包，所以字节不会先于流到达。

hub → node 事件（按发出顺序处理）：`terminal.input {session_id, text}`、`terminal.resize {session_id, cols, rows}`、`terminal.scroll {session_id, lines, up, column?, row?}`（滚轮：herdr 交给接鼠标的程序，比如 Claude Code 的全屏渲染、Codex，否则滚 herdr 自己的历史；
node 拆成 `lines` 个一行的事件，落在 `column` / `row` 那一格，没给就落在屏幕正中间，`docs/terminal.md` 第十六节）、`terminal.click {session_id, column, row}`（左键按下 + 松开）、`terminal.keepalive {session_ids}`（每 20 s，接管中的终端；node 75 s 没听到就放手）。

node → hub 事件：`terminal.frame {session_id, epoch, seq, full, cols, rows, data}`（相隔不到 30 ms 的帧由 node 合成一帧：diff 的字节接起来，整帧丢掉它前面的 diff）、`terminal.end {session_id, error, control?, taken?}`（流停了且起不来：会话不在跑、herdr 一直拒绝；`control` 时是接管结束了：`taken` 被别人抢走，否则租约到期）、
`session.items {session_id, items, history, from_seq, to_seq}`、`session.echo {session_id, client_msg_id, text, images, sent_at, after_seq, line?}`（node 记下一次发送、认到它的那一行时各推一次，hub 存进 `sent_messages.echo`；已认到的不会被晚到的「还在等」盖掉）、`sessions.changed {sessions}`、
`sessions.revs {revs}`（每秒检查一次，只发变了的 rev；列表本身变了时 rev 跟着 `sessions.changed` 走）、
`usage {buckets:[{minute, model, in, cache_write, cache_read, out, req}], full, load, cpus}`（每 10 秒，只发变了的分钟、每分钟的全量值，
连上后第一次 `full: true` 带最近 26 小时全部；`docs/live-activity.md`「用量」）、
`metrics {sample: MetricSample}`（每 10 秒，对齐到整 10 秒；hub 存进 `machine_metrics`，`docs/machines.md`）、
`codex.interactions {interactions}`（这台机器上所有开着的 Codex / TRAEX 提问和审批，有变化时整份发；hub 增删卡片，旧 hub 不认就照旧是屏幕卡）。

node → hub 请求：
- `interaction.create {interaction}` → `{resolution}`（阻塞到被处理 / 过期；过期返回 `{"dismiss":true}`）
- `mcp.tools {}` → `{tools}`；`mcp.call {name, args, session_id}` → `{content, is_error}`

### 电脑链路（jarvis-link，kind link）

hub 连不进去的电脑（笔记本）由 jarvis-link 主动连 hub 的 TLS 端口，hub 在这条链路上开流，流里就是上面这套 RPC（字节原样转给那台的常驻 node）。
细节见 [link.md](link.md)，这里只列线上的东西：

- `GET /api/v1/link`（WebSocket，`role: link` 的凭据，只走 TLS，回环例外）。文本消息，每条一个 JSON：
  - jarvis-link → hub：`{"type":"hello","machine","version","address"}`（第一条，10 秒内）、`{"type":"open_failed","id","error"}`；
  - hub → jarvis-link：`{"type":"endpoints","endpoints":[{"url","kind"}]}`（只有 https 的候选）、`{"type":"open","id"}`、
    `{"type":"node","online","error"}`。
  - 关闭码：4001 被同一台的新链路顶掉、4002 凭据被撤销、4003 hello 和凭据对不上、1001 hub 停机。两边每 10 秒 ping，5 秒没 pong 断开（配置值）。
- `GET /api/v1/link/stream?id=<open 的 id>`（WebSocket，同一凭据，permessage-deflate）：二进制消息，内容是 RPC 的字节流；
  hub 15 秒内没等到就当失败。链路断了，链路上的流一起断。

## hub ↔ hub（互联，[mesh.md](mesh.md)）

- `POST /api/v1/peer/pair`（`Authorization: Bearer jx_…`，对方的互联码）：`{hub_id,name,version,fp,tls_port,endpoints,token,center,term}` →
  同样几项（`token` 是回给对方的互联凭据，`center` / `term` 是发码方所在组的中心和任期：用码的一方并入这一组）。码无效 / 用过 / 过期 401；本 hub 没开 TLS 409。
- `GET /api/v1/peer`（WebSocket，只走 TLS，`role: peer` 的凭据，permessage-deflate）：二进制消息，内容是上面 hub ↔ node 那套 JSON 行 RPC 的字节流：
  - 调用：`hello {hub_id,name,version,epoch}`（两边各一次）、`stream.open {id}`（流上是一条到对方 peer 服务器的 HTTP/1.1 连接）、
    `relay {to,from,ttl,method,params}`（`creds.import {tokens:[{id,name,kind,role,created_at,last_seen,hash}]}` → `{added}`：成员把凭据交给中心；
    `pair.code {}` → 配对码：成员上的 `personad hub pair`）、`unpair {}`；
  - 单向：`rec`（hub 的记录：链路、`center`、`term`、hub.json 里的电脑）、`stream.data` / `stream.ack` / `stream.close`。
  - 关闭码：4011 重复的链路、4012 断开了互联、4013 发不过来、1001 hub 停机。两边每 10 秒 ping，5 秒没 pong 断开（配置值）。
- 流上的 HTTP 都带 `X-Jarvis-Peer-Ttl`（8 起，每跳减一，到 0 回 `508 mesh_loop`），一跳一跳转到要去的 hub：
  - 成员转给中心的 App 请求：原样（含 `Authorization`），加 `X-Jarvis-Peer-Source: <lan|public> <客户端地址> <tls|plain>`（成员对来源的判断）。
    转不过去 `503 center_unreachable`，转发失败 `502 hub_unreachable`。
  - 中心经成员开的 node 流：`GET /api/v1/peer/node`，`Upgrade: jarvis-node`、`X-Jarvis-Peer-Node: <成员 hub_id>/<机器 id>`、`X-Jarvis-Peer-From: <中心 hub_id>`；
    成员回 `101`，之后是 node 的字节流；不是它的中心来要 403，hub.json 里没有 404，打不开 502（`error` 是原因）。
- 管理接口（管理界面登录后，[admin.md](admin.md)）：`GET /api/v1/admin/mesh`、`GET /api/v1/admin/peers`、`POST /api/v1/admin/peers/code`、`POST /api/v1/admin/peers`、
  `POST /api/v1/admin/peers/probe`、`DELETE /api/v1/admin/peers/{hub_id}`、`POST /api/v1/admin/peers/{hub_id}/center`、`GET /api/v1/admin/routes`；
  同一套在 `/api/v1/admin/cli/…` 下给 hub 机器上的命令行用（回环 + 主 token）。

## hub → 连接器（kind service，[link.md](link.md)「被动模式」、[mesh.md](mesh.md)「kind service」）

- hub 按配对链接里的 `fp` 钉扎连 `<address>`（可经 `socks5://` 代理）：`POST /api/v1/connector/pair {code,token,machine,hub_id,hub_name}` 一次；
  之后 `GET /api/v1/connector/info`（每 30 秒，探活和 RTT）、`GET /api/v1/connector/stream`（WebSocket，node 的字节流），都带 `Authorization: Bearer <凭据>`。
- 401：5 分钟内不再拨；429：按 `Retry-After`；503 `node_unavailable`：显示原因、照常退避；流以 4002 关：要重新配对，不再试；1001：照常退避。

## hook / mcp ↔ node（unix socket `~/.persona/node.sock`）

一次连接一问一答，各一行 JSON：

- `{"kind":"hook","input":<Claude Code 传给 hook 的 stdin JSON>,"pane":"…","attempt":"<hook 进程自己的 id>","resume":<是不是重连>}`
  → `{"output":<要打印到 stdout 的 JSON 或 null>}`，或 `{"retry":true}`（node 正在退出，去问下一个 node）
- `{"kind":"mcp.tools"}` → `{"tools":[…]}`
- `{"kind":"mcp.call","name":"…","args":{},"session_id":"…"}` → `{"content":[…],"is_error":false}`

连不上 node（hub 不在线）时 hook 立即退出 0、不输出，即 TUI 正常处理；mcp 返回错误文本。

### 交互挺过重启

部署频繁（hub 重启、换 node 二进制），以前手机上正在答的提问 / 计划 / 授权会消失，退回电脑 TUI。现在：

- **hook**：已经连上 node、还没拿到回答时 node 没了（连接断开或回 `retry`），每秒重连 `node.sock`、带同一个 `attempt` 重发，
  最多 2 分钟（`node.ResumeWait`）；一开始就连不上 node 照旧立即交给 TUI。
- **node**：interaction id 是确定的：`session_id + 事件 + 工具名 + tool_use_id` 的哈希（没有 tool_use_id 用 hook 的 attempt），
  重连的 hook 得到同一个 id；带 attempt 的交互标 `resumable: true`，重连的那次不再发 `hook.event`。
- **hub**：`resumable` 的交互在 node 连接断开时不撤销，留 3 分钟（`resumeGrace`）等 hook 重新认领；同 id 的 `interaction.create`
  挂回原来的那张卡（不重复推送，App 按 id 去重），这期间手机上答了的，回来的 hook 直接从数据库拿到答案。启动时数据库里
  pending 的 `resumable` 交互重新载入（同样等 3 分钟），其余标 cancelled。旧 node 不带 `resumable`，行为不变。
- 本机实测（假 hub + 沙箱 node + 真 `personad hook`）：提问时杀掉 node、2 秒后起新 node，hook 3.1 秒后拿到手机的回答，hub 只看到一个 id。

## Runtime ↔ node（unix socket `~/.persona/runtime.sock`，或 node 去连的 daemon）

**Runtime** 是没有终端、自己管会话的 Agent 运行时（目前是 DSH，`docs/dsh.md`）。它不是「宿主」：宿主是终端开在哪（herdr / Herdr App / Orca，
见「引擎」），node 从窗格和 transcript 认会话；runtime 自己报会话、自己给内容，和这台机器选的引擎并存。怎么接一个新的 runtime、
SDK 和验收清单见 `docs/runtimes.md`；这里是线上的协议（`server/internal/node/runtimes.go`）。

runtime 连 node 的 `~/.persona/runtime.sock`（0600，所以必须和 node 是同一个系统用户），一条长连接，帧格式和 hub ↔ node 完全一样（`internal/rpc`：
每行一个 JSON，请求 / 应答 / 单向事件，两边都能发请求）。hub 和 App 不知道有 runtime：看到的就是普通会话、items 和交互卡片。
**socket 跟着谁**：常驻 node（`personad node --attach`）一启动就开着 `runtime.sock`，hub 的每条连接共用它（`node/runtimehost.go`）：
runtime 不用等 hub，hub 重连期间连接也不断，下一条连接的第一份会话列表里就有它的会话。没有常驻 node、由 `--stdio` 自己服务时，socket
只在这条 hub 连接期间存在，和以前一样。

**没有 hub 时**：runtime 照样连得上、hello 照样回（`hub: false`），列表照收；推给手机的内容丢掉（hub 回来后会按手机的进度重新 watch）；
`interaction.create` 回错误 `code: node_gone`，runtime 等 hub 回来后用同一个 id 再问。hub 来了、走了 node 都推一次 `runtime.hub`，runtime 据此
不去等一个答不了的手机（SDK：没有 hub 时一律先给本地界面）。node 进程自己没了（重启、换二进制）时 runtime 重连：socket 文件还在就每秒
试一次，文件不在就退避到最长 30 秒。

runtime → node：

| 消息 | 说明 |
| --- | --- |
| 请求 `runtime.hello {runtime, name?, agent?, version?, instance?, launcher?, launcher_model?, caps: {start, send, interrupt, rename, close, assistant?}, package?: {name, hash}}` → `{protocol, node_version, hub, features, update?: {name, hash, version}}` | 必须第一个发；`instance` 是这个进程的标识（SDK 每个 runtime 一个，重连不变）：同一个 runtime id 已有一条活连接、它的 `instance` 不同时，回错误 `code: runtime_conflict`（另一个进程配了同样的 id，连不上，SDK 每 30 秒再试一次、不抢）；相同时是这个进程重连了，旧连接被关掉；没带 `instance` 的旧 runtime 照旧（最新的连接为准）；`hub` 是 node 现在有没有连着 hub（旧 node 不带，当作有）；`features` 是 node 在第一版协议之外支持的能力：`prefer_running`（两个 runtime 列同一个会话时，列成非 `offline` 的那个负责它，所以 runtime 可以把只在磁盘上的会话列成 `offline`）、`resume_argv`（认会话行里的 `resume_argv`）、`handover`（发消息被回 `code: handover` 时交给接手的 runtime，见下面 `session.send`）；之前的事件 node 一律丢掉。`runtime` 是 id（`[A-Za-z0-9][A-Za-z0-9_.-]*`，如 `dsh`），也是会话的 `space_kind`；`agent` 是它的会话跑的 Agent，不填就是 `runtime` 自己；`name` 是 App 里的叫法（`DSH`），这种会话带 `agent_label: name`，App 不用为新 runtime 发版。`caps` 说明支持哪些操作，没声明的 node 直接回 `unsupported`。`launcher` 非空且 `caps.start` 时，node 的 `launchers.list` 多一项 `{name: launcher, model: launcher_model, source: runtime}`，新建会话选它就交给这个 runtime。两个 runtime 报同一个启动器名时只有先连上的那个提供（另一个记一行日志）；和登录 shell 里的启动器同名时 shell 的那个为准，runtime 的不列。`caps.assistant`（要有 start 和 send）：它的启动器能跑 Persona 的助手，`launchers.list` 里带 `assistant: true`；node 怎么驱动它见 `docs/runtimes.md`「助手」 |
| 事件 `runtime.sessions {sessions: [Session]}` | 它的全部会话，变化时推全量。字段是 `Session` 的子集：`session_id`（`[A-Za-z0-9][A-Za-z0-9_.-]{0,127}`，比 Claude 的 uuid 宽，DSH 是 `session-<uuid>`）、`title`、`cwd`、`status`（`working / blocked / done / idle / offline`，别的当 `unknown`）、`agent`、`model`、`updated_at`、`last_text`、`rev`（变了 App 就拉新内容）、`space_*`，以及还在跑的子 Agent 和后台任务 `subagents` / `background`（`docs/runtimes.md`「子 Agent 和后台任务」：子 Agent 的 `session_id` 由 node 写成 `<会话>@<id>`，这个 id 的调用交给父会话所属的 runtime，它推内容也用这个 id；后台任务的 `output_file` 清掉）。node 清掉 `machine_id`、`state` 这些 hub 算的字段，补 `agent` / `agent_label` / `space_kind`，并按「Codex 来源与能力」的同一套字段标来源：`source_kind: "runtime"`、`source_instance_id` 是 runtime id、`capabilities` = `{read: true, send: caps.send, interrupt: caps.interrupt, approve: true, terminal: false, attach: false}`（没声明 `send` 的带 `read_only_reason`），App 和 hub 按它们开关按钮、守发送；合并进 `sessions.list` 立刻推 `sessions.changed`；同一个 id 两条连接都报时，列成非 `offline` 的那条负责，同等时新的为准（`prefer_running`）。两个 node 用、App 看不到的字段：`pane_id`（这个会话跑在这台机器终端引擎的哪个窗格：herdr 窗格里的 pi，node 把它和引擎的窗格合成一行 herdr 会话，调用里终端那部分给引擎，其余给 runtime，`docs/pi.md`）、`resume_argv`（列成 `offline` 的会话怎么在终端引擎里接着：手机发消息时 node 在引擎里开新标签、经 `jarvisd tab-start` 运行它、等 runtime 再列出这个会话为运行中后把消息发过去；引擎不在就拒绝，`resume_argv` 特性）。连接断开，它的会话就从列表里消失。**列表只是「现在给 App 看哪些」，不是归属**：runtime 只列它最近的会话（DSH 是几千个里最近的 80 个），node 记着每个会话是哪个 runtime 列过的（按 runtime id，重连后还是它），对一个不在列表里的老会话的调用照样交给它；这台机器上什么都没有（没有 Agent、没有 transcript）的会话，`session.send` / `items` / `watch` 这类调用先用 `session.items {session_id, limit: 1}` 问一遍连着的 runtime，答得上来的就归它（问不到的一分钟内不再问）。属于某个 runtime、它却没连着时，回错误 `code: runtime_away`（`delivered: none`），不会当成 Claude 会话去终端里恢复 |
| 事件 `session.items {session_id, items, history, from_seq, to_seq}` | 被看着的会话的新内容，原样转给 hub（形状同 node → hub 的 `session.items`，连续性规则见「items 的连续性」）。它没列出的会话不转 |
| 事件 `session.delta {session_id, text}` | 正在生成的整段文字，空串表示这段结束；原样转给 hub，App 画成逐字输出 |
| 请求 `interaction.create {interaction, timeout_seconds?}` → `Resolution` | 提问（`question`）、计划（`plan`）、授权（`permission`）三种，形状同上面的 `Interaction`；node 补 `created_at` / `expires_at`（默认同 hook 的 30 分钟，`timeout_seconds` 只能更短）、会话标题、目录，以及 `agent` / `agent_label`（runtime 跑的 Agent 和它的名字；跑 Claude Code 的不标，App 画成 Claude 的卡），经 hub 推给手机，阻塞到回答。没人回答（过期、手机「交回电脑」、没有 hub）回 `{dismiss: true}`，runtime 就交给自己的界面。带 `tool_use_id` 的交互 `resumable`：runtime 给的 `id`（或 node 按 runtime + 会话 + 种类 + tool_use_id 算的）不变，node 中途没了时回错误 `code: node_gone`，runtime 连上下一个 node 后用同一个 id 再问，hub 挂回原来的卡 |
| 事件 `interaction.cancel {id}` | runtime 那边这次交互没了（工具调用被取消）：撤掉手机上的卡 |
| 请求 `runtime.package {name}` → `{name, hash, version, files: [{path, data}]}` | 自动更新（`docs/runtimes.md`「自动更新」）：hello 里 `package` 说 runtime 跑的代码（包名和哈希）；node 带着这个名字、哈希不同的包时，回复带 `update`，runtime 用这个请求取文件（`data` 是 base64），自己核对哈希后换上。包在 personad 里（`internal/runtimepkg`，构建时从 `integrations/` 生成）；daemon 拿不到（不带 `update`，这个请求回 `unsupported`）；没有这个包回 `not_found` |
| 请求 `mcp.tools {assistant?}` → `{tools: [{name, description, input_schema}]}`、`mcp.call {name, args, session_id, assistant?}` → `ToolResult` | hub 的工具（手机的 `phone_*`、`mail_*`），转给 hub；没有 hub 时 `mcp.tools` 是空的、`mcp.call` 回一个出错的结果。runtime 把它们给自己的 Agent 用（DSH：`docs/dsh.md`「手机工具」）。`assistant: true` 只对声明了 `caps.assistant` 的 runtime 有用：`mcp.tools` 多出助手专用的工具和 node 自己的 `lark-connector`；`mcp.call` 只在会话归这个 runtime、而且是助手对话时按助手的调用处理（否则当普通调用），`lark-connector` 由 node 自己跑（`docs/runtimes.md`「助手」） |

node → runtime 的事件：`runtime.hub {connected}`，hub 连上 / 断开时发给每个 runtime。

node → runtime 的请求（只发给这个会话所属的那条连接；hub 对这些会话的 `session.*` 调用都走这里）：

| 请求 | 说明 |
| --- | --- |
| `session.items` | 参数和返回同 hub → node 的 `session.items`（`before_seq` / `turns` / `limit` / `after_seq` / `from_seq` + `through_seq`），图片一律是占位（`lazy`），内容从 `session.image` 取。`fold_work` 由 node 在返回上做（`transcript.FoldWork`），runtime 不管 |
| `session.image {session_id, item_id}` → `{mime, data}`、`session.item {session_id, item_id}` → `Item` | 图片原始数据、被截断的 item 全文 |
| `session.watch {session_id, since_seq}` / `session.unwatch` | 开始 / 停止推这个会话的新内容；`since_seq > 0` 时先补发其后的全部。node 记着每个被看着的会话推到了哪，runtime 重连后列出它时按那个 seq 再 watch |
| `session.send {session_id, text, images, client_msg_id, assistant?, context?, mode?, memory_dir?}` | 发消息（`assistant` 和后面几个只发给声明了 `caps.assistant` 的 runtime 的助手对话：`docs/runtimes.md`「助手」）；`client_msg_id` 交给 runtime 做幂等（node 自己也按它去重 10 分钟），node 也按它在 runtime 推来的 item 里认出这条消息（「待发消息的对应」）；runtime 自己知道哪个 item 是这条消息时可以直接在 item 上带 `client_msg_id`，node 原样转发。runtime 回错误 `code: handover`（`data.delivered: "none"`）表示它不接这个会话、已经让能跑它的程序起来（只读磁盘的伴随进程）：node 等另一条连接把这个会话列成非 `offline`，或者每 2 秒问一遍别的 runtime 有没有这个会话（`session.items {limit: 1}`），先到的那个收到这条消息；45 秒内都没有就回 `not_running`，什么都没发 |
| `session.interrupt {session_id}`、`session.rename {session_id, title}`、`session.close {session_id}` | 各按 `caps` 放行，没有的回错误 `code: unsupported`（文字以 `unsupported` 开头，同宿主缺能力）。只带 `tab_name` 的改名直接成功（这些会话没有标签页） |
| `session.start {cwd, prompt?, assistant?}` → `{session}` | 选了它的 launcher 时；返回的会话马上进列表。`assistant: true`：助手用它的启动器开新对话（`cwd` 是助手目录，不带 `prompt`，第一条消息随后用 `session.send` 发） |

终端相关的调用（`session.keys`、`session.screen.before`、`terminal.watch`）对 runtime 的会话回 `unsupported`，`session.screen` 回空屏；
这些会话没有 `pane_id`，App 本来就不显示终端。

**daemon**（`docs/daemons.md`，`server/internal/node/daemons.go`）：自己是常驻服务、连不上本机 socket 的 runtime，由 node 去连。

- **连法**：
  - node 按 `runtime.sock` 旁边的 `daemons.json` 去连 daemon 的 WebSocket 端点，请求带 `Authorization: Bearer <token>` 和子协议 `jarvis-runtime.v1`；daemon 必须回这个子协议。
  - 连上以后，一条文本消息就是上面的一行，协议和本机 socket 上完全一样。
- **node 在中间改写的东西**：
  - hello 里的 `runtime`、`name`、`launcher` 换成配置里的值，`agent` 也一样（配置填了的话）；`instance` 由 node 定成 `daemon:<id>`（它重连时旧连接被关掉）；`caps` 只留 start / send / interrupt / rename / close，没有 `assistant`。
  - 会话 id 加上 `<id>.`，`space_id` 加上 `<id>:`，交互 id 换成稳定的哈希；回给 daemon 时再去掉前缀。
  - 会话行里的 `pane_id`、`resume_argv`、`agent_label` 丢掉，`features` 里不告诉它有 `resume_argv` 和 `handover`；它对 `session.send` 回 `code: handover` 时，node 改成 `not_running`，不转给别的 runtime。
- **daemon 看不到、用不了的**：
  - 调用的会话 id 不带它的前缀时（node 在问「这个会话是谁的」），node 自己回 `not_found`，不发给 daemon；
  - `mcp.tools` 回空，`mcp.call` 回 `unsupported`。
- **id 冲突**：
  - 本机 socket 上的 runtime 用 `daemons.json` 里的 id 说 hello，回错误 `code: runtime_conflict`；
  - 某个 id 已被连着的 runtime 占用时，同 id 的 daemon 的 hello 也回 `runtime_conflict`，node 随后断开它，稍后再连。

### 任务和对话动态

从助手对话派发的任务（[unified-entry.md](unified-entry.md)）。助手用 `task_plan` 交出计划；只读的、用户记住过的直接开始，其余等手机上的「开始」。

```jsonc
// TaskPlan
{"id":"plan_…","conversation":"<助手对话 id>","state":"open|started|dismissed","created_at":"...","updated_at":"...",
 "tasks":[{"title":"修登录崩溃","prompt":"给 Agent 的第一条消息","machine":"<机器 id>","cwd":"/abs/path",
           "launcher":"","model":"","permission":"acceptEdits","readonly":false,"why":"一行理由",
           "todo_id":"todo_…",          // 开始后才有
           "error":""}]}                 // Agent 没开起来的原因

// FeedItem：hub 插进助手对话的条目，不在 transcript 里；客户端按 at 和 transcript 合在一起画
{"id":"feed_…","conversation":"…","at":"...","kind":"plan|task|answered|receipt|routed","ref":"plan_… 或 todo_…",
 "body":{…}}
// plan：ref 是计划 id，没有 body；task：ref 是待办 id，没有 body
// answered：{"question":"使用 Bash","answer":"批准了 · 以后同类不再问"}
// receipt：{"title":"…","state":"done|failed","summary":"Agent 最后一段话","pr":"https://github.com/…/pull/N","machine":"…"}
// routed：{"text":"…"}
```

任务卡、计划卡的实时状态不在动态里：客户端按 `ref` 读计划（`task_plan` 推送）和待办 + 挂着的 Agent 的会话状态（状态规则同 [todos.md](todos.md)）。
任务的 Agent 的提问照常是交互，客户端按待办把它画在对话底部。

## MCP 工具（`personad mcp`）

| 工具 | 执行方 | 说明 |
| --- | --- | --- |
| `phone_*` | iPhone App | 由 App `register_phone` 声明（通讯录、日历、提醒、位置、健康、照片、剪贴板、设备信息、通知……） |
| `mail_accounts` / `mail_search` / `mail_read` | hub | 只读 |
| `mail_send` | hub（邮件连接器） | 写：过 `connector_gate`，默认发送前创建 `permission` 交互，手机批准后才发；参数不对时不弹卡片直接拒绝 |
| `github_search` / `github_read` / `github_diff` / `github_checks` | hub（GitHub 连接器） | 只读，见下一行 |
| `github_comment` / `github_review` | hub（GitHub 连接器） | 写：过 `connector_gate`，默认手机上逐次确认；卡片写明仓库#号「标题」、全文、谁会看到，approve 开头是【批准】 |
| `<连接器 id>_*` | hub（连接器） | 只给助手、只给状态是 `ok` 的连接器。只读工具直接执行；写操作先过 hub.json 的 `connector_gate`，默认创建 `permission` 交互（卡片上写明谁会看到），手机批准后才执行（`server/internal/connector`） |

## 连接器 API

特性 `connectors`（`/health` 和 WebSocket hello 的 `features`），旧 hub 没有这个接口。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/imessage` | `{machine, status, error?, guide?:[…], via?, ai}`：信息连接器（hub.json `imessage.machine`）的状态，当场问那台 Mac（刚开了权限，刷新就是新的）；没有完全磁盘访问权限时 `guide` 是在那台 Mac 上一步步怎么开，`via` 是那台 Mac 的 node 怎么跑（`ssh` / `launchd`，两种开法不一样）。没设置是 404 `not_configured` |
| PUT | `/api/v1/imessage/settings` | `{ai: bool}`：「交给 Persona」开关（存在 hub，默认开）；开着时真正的会话进知识库、收件箱，助手有 `imessage_*` 工具；关掉立刻停。回 `GET /api/v1/imessage` 的样子（多一个 `ai`） |
| DELETE | `/api/v1/imessage/data` | 删掉 hub 上存下的信息：知识库里的信息会话和人（连同索引、项目里的引用和它们写的时间线、归档游标，项目标成待重写）、收件箱里的信息条目 → `{chats, people, inbox}`。「交给 Persona」开着时 409 `ai_on`（先关掉，不然下一轮又同步回来）；那台 Mac 上的消息不受影响 |
| GET | `/api/v1/imessage/chats?limit=&before=` | 那台 Mac 的会话，新的在前（node 的 `connector.chats`，字段见下面「hub ↔ node」）。只读、不存、不交给模型。没权限 403 `no_permission`，Mac 不在线 503 `offline`，node 太旧 409 `node_outdated` |
| GET | `/api/v1/imessage/chats/{id}/messages?limit=&before=` | 一个会话的一页消息，旧的在前（node 的 `connector.older`）；`id` 不像会话 id（`imessage-<12 位十六进制>`）的不发给 Mac，404 `no_chat` |
| GET | `/api/v1/connectors?all=1` | `{"connectors":[{id, name, status, account?, via?, error?, guide?:[…], last_sync?, last_event?, capabilities:[…], hosts?:[…], enabled}]}`：hub 上注册的连接器。不带 `all=1` 时不含飞书（`lark`）和邮件（`mail`）：M6 以前的 App 把列表里每一项都画成一行，它们又各有一行，会出现两次；带上的 App 自己把这两项画成原来的飞书、邮箱行（状态还读 `/api/v1/lark/status`、`/api/v1/mail/accounts`）。`guide` 是出问题时一步一句怎么修好（信息连接器没有权限时在那台 Mac 上怎么开）。`status` 是 `ok` / `not_configured` / `not_signed_in` / `no_permission` / `rate_limited` / `error` / `disabled`（用户在设置里关掉了，`enabled` 为 false）；`via` 说登录从哪来（「来自 <机器> 的 gh 登录」）；`error` 是给人看的一句话，不含密钥；`capabilities` 取 `status` / `tools` / `actions` / `events` / `hosts` / `inbox` / `archive` / `login`；`hosts` 是这个连接器的链接所在的主机名（App 用来认链接） |
| POST | `/api/v1/connectors/{id}/login` | 分步登录（有 `login` 能力的，`docs/plan-telegram.md`）：`{"step": "", "values": {}}`，`step` 为空是从头开始，之后带上一步的 `step` 和填的字段 → `{step?, fields?:[{name, label, secret?, placeholder?}], hint?, error?, done?, account?, kind?, url?, user_code?, interval?}`：下一步要填的，或 `done`。
`kind` 是 `device_code` 时这一步不填字段：显示 `user_code`，让用户去 `url` 输入，App 每隔 `interval` 秒用同一个 `step`（不带值）再问一次，直到回来别的步骤或 `done`（GitHub 的设备码登录）。填错了（验证码不对）是同一步带 `error`，表单留着重填；别的错 502。填的值只给连接器，不进日志和任何响应；登录得到的会话加密存 hub（`connector_secrets`） |
| GET | `/api/v1/connectors/github/auth` | GitHub 的登录从哪里来：`{source: "machine" \| "device", machine?, machines: [{id, name, state, account?, scopes?, missing?, error?}], scopes?, scopes_known, missing?: [{scope, affects}]}`。`state` 是 `signed_in` / `not_signed_in` / `no_gh` / `too_old` / `offline` / `error`（hub 当场问每台在线电脑的 gh，再问 GitHub 这个 token 是谁、有哪些 scope；token 不出 hub）；`missing` 是用的 token 少了的 scope 和受影响的功能 |
| PUT | `/api/v1/connectors/github/auth` | `{source: "machine", machine: "<id>"}` 或 `{source: "device"}`：换登录来源，存在 hub 设置里（盖过 hub.json 的 `github.auth.from_machine`），回同上；选电脑时忘掉设备码登录的 token |
| POST | `/api/v1/connectors/{id}/logout` | 退出登录：连接器那边结束会话，hub 删掉存着的会话 → `{"ok": true}` |
| PUT | `/api/v1/connectors/{id}/settings` | `{"enabled"?: bool, "options"?: {…}}`：`options` 是连接器自己的设置（有 `options` 能力的，GET 里也带着；GitHub 的是 `{ci, muted_repos, ignore_branches}`，`docs/inbox.md`「降噪」），不合法 400，存 hub、重启照旧，改完立刻按新设置收一轮收件箱。`enabled` 是设置 › 连接器 的开关。关掉的连接器不跑、不给工具、不收收件箱、不进知识库，状态是 `disabled`，登录和退出照常；选择存在 hub 的设置里，重启后照旧；没动过开关的连接器是开着的，Telegram 这种要用户自己填凭据的默认关着 → 这个连接器的一项（同 `GET`） |
| DELETE | `/api/v1/connectors/{id}/data` | 删除已存的数据：先关掉这个连接器（开关存成关，不然下一轮又同步回来），再删掉 hub 上存下的它的东西：知识库里它的会话和人（连同索引、项目里的引用和它们写的时间线、归档游标）、收件箱里它的条目 → `{chats, people, inbox}`。和退出登录分开（`/logout`）；连接器那边的账号和消息不受影响 |

## 邮件 API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/mail/accounts` | `{"accounts":[MailAccount]}`（不含密码） |
| POST | `/api/v1/mail/accounts` | 新增（密码 / 授权码）：测试连接成功才保存 → `{"account"}`；`auth:"oauth2"` 一律 400 |
| POST | `/api/v1/mail/oauth/google/start` | Gmail「使用 Google 账号登录」：`{"login_hint"?}` → `{"auth_url","callback_scheme","state"}`；App 用 ASWebAuthenticationSession 打开 `auth_url`，等 `callback_scheme` 回调；hub 未配置 `google.client_id` 时 400 |
| POST | `/api/v1/mail/oauth/google/finish` | 回调 URL 里的 `{"state","code"}`（或 `{"state","error"}`）→ hub 换 refresh token、验证 IMAP + Gmail API（发信走 Gmail API）后保存 → `{"account"}`；同一邮箱再登录一次 = 更新凭据（ID 不变）；state 一次性、15 分钟过期 |
| GET / PUT | `/api/v1/mail/oauth/microsoft/config` | Outlook 登录的设置：`{configured, client_id?, source?: app\|config, redirect_uri}`；PUT `{client_id}` 存用户在 Microsoft Entra 注册的 client ID（加密存 hub，空串清掉；格式不对 400）（`docs/mail.md`「Outlook 接入方式」） |
| POST | `/api/v1/mail/oauth/microsoft/start` | `{"login_hint"?}` → `{"auth_url","callback_scheme","state"}`；没有 client ID 时 400 |
| POST | `/api/v1/mail/oauth/microsoft/finish` | 回调的 `{state, code}` 或 `{state, error, error_description}` → `{"account"}`；错误翻成该怎么办（管理员同意、IMAP / SMTP AUTH 被关、授权过期…）。同一个邮箱再登录 = 更新凭据 |
| DELETE | `/api/v1/mail/accounts/{aid}` | |
| POST | `/api/v1/mail/accounts/{aid}/test` | `{"ok":true}` / `{"error"}` |
| GET | `/api/v1/mail/accounts/{aid}/mailboxes` | `{"mailboxes":[{"name","role","unseen","total"}]}`，role ∈ inbox/sent/drafts/trash/archive/junk/"" |
| GET | `/api/v1/mail/messages?account_id=&mailbox=INBOX&limit=50&before_uid=&q=` | `{"messages":[MailSummary],"has_more","next_before_uid"?,"headers_only"?}`；`account_id` 为空 = 所有账号收件箱合并（按日期倒序，不带 `next_before_uid`）。`next_before_uid`：`has_more` 时下一页的 `before_uid`；本地匹配（见下）一页没看完时 `messages` 可以为空、`has_more` 仍为真。`headers_only`：服务端对 `q` 什么也没搜到（163 之类不做关键词搜索），结果来自在 hub 上匹配主题、发件人、收件人 / 抄送，不看正文（`docs/mail.md`「搜索」） |
| GET | `/api/v1/mail/messages/{aid}/{uid}?mailbox=INBOX` | `{"message":MailMessage}` |
| GET | `/api/v1/mail/messages/{aid}/{uid}/attachments/{attachment_id}?mailbox=INBOX` | 按账号、邮箱、UID 和详情中的稳定附件 ID 下载；Bearer 鉴权与邮件读取一致；支持 `Range`／`If-Range`，返回 `Accept-Ranges`／`Content-Range`、206 或 416 |
| POST | `/api/v1/mail/messages/{aid}/{uid}/flags` | `{"mailbox","seen"?,"flagged"?}` |
| POST | `/api/v1/mail/send` | `{"account_id","to":[],"cc":[],"bcc":[],"subject","text","html"?,"in_reply_to"?,"references"?}`；`references` 是空格分隔的 Message-ID 字符串，也收字符串数组（旧版 iOS 这样发，hub 用空格拼起来） |

邮件详情页先读取正文和附件清单，再用 `/flags` 提交 `{"mailbox":"INBOX","seen":true}`。详情 GET 使用 `BODY.PEEK`，保留旧客户端的行为，不自动标已读。附件包含稳定的 `id`、文件名、MIME 类型和解码后大小；ID 绑定 MIME 顺序、文件元数据与解码内容，同名附件可以区分。下载同样使用只读 `BODY.PEEK`，不依赖 BINARY 等扩展；服务端将解码内容流式写入临时文件，再按 HTTP Range 返回解码后的字节区间。未知账号、附件或内容变化返回 404。

```jsonc
// MailAccount
{"id":"acc_…","name":"Gmail","email":"me@example.com","provider":"gmail|feishu|imap","auth":"password|oauth2",
 "username":"…","imap_host":"imap.gmail.com","imap_port":993,"smtp_host":"smtp.gmail.com","smtp_port":465,
 "smtp_security":"tls|starttls","status":"ok|error","error":""}
// 新增时额外字段：password。provider 为 feishu 时服务器固定为 imap/smtp.larkoffice.com（993 / 465，SSL）
// MailSummary
{"account_id","mailbox","uid":123,"message_id","subject","from":{"name","email"},"to":[{"name","email"}],
 "date","snippet","seen":false,"flagged":false,"has_attachments":false}
// MailMessage = MailSummary + {"cc":[…],"text":"…","html":"…","attachments":[{"id","filename","mime","size"}],
//                               "in_reply_to","references":[…]}
```

## 收件箱 API

hub 后台采集飞书和邮件，聚合成「件」（`docs/inbox.md`，`server/internal/hub/inbox.go`）。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/inbox` | `{items:[InboxItem]}`：未处理的 + 3 天内处理过的，按 `updated_at` 倒序；不含 `context`。默认只有飞书和邮件；带 `?sources=all` 才有连接器（GitHub…）的条目（带 `connector` 块的），旧 App、手表、Android 不带，所以不会把 PR 当邮件画 |
| GET | `/api/v1/inbox/{id}` | 一件，带 `context`（这段对话 / 邮件的纯文本，给 AI 用）；邮件额外带 `reply_target:{uid,version}` |
| POST | `/api/v1/inbox/{id}/done` | `{done: bool}`：标已处理 / 移回，返回这件 |
| GET | `/api/v1/inbox/mutes` | `{mutes:[{id, source, source_name, tag, value, name, created_at}]}`：用户的静音（`tag` 是 `repo` / `chat` / `sender`，`docs/inbox.md`「静音」）。列表里每件事带 `mutes:[{tag, value, name}]`，说它能按什么静音（旧 hub 没有） |
| POST | `/api/v1/inbox/mutes` | `{item_id, tag}` → `{mute, settled}`：静音这件事在 `tag` 下的值（hub 从这件事上读），同一来源里被覆盖的打开条目收掉（`settled` 件），以后的进来就收掉；同一个静音只有一份 |
| DELETE | `/api/v1/inbox/mutes/{id}` | 取消静音：以后的照常进来，已经收掉的不回来 |
| POST | `/api/v1/inbox/{id}/draft` | `{instruction?}` → `{text}`：按用户口吻起草的回复（不发送） |
| POST | `/api/v1/inbox/{id}/reply` | `{text, changes?, client_id?, expected_uid?, expected_version?}`：飞书发到原会话 / 邮件按回复发出 / 云文档发成评论（`doc.reply` 指的地方），并标为已处理（`done_by=replied`），返回这件。云文档的 `changes:[{ref,field,value}]` 先改状态再评论；只改状态时 `text` 可以为空。提供 `client_id` 时保存回执，飞书原样透传为上游 `clientId`；不提供时 `clientId` 由这件的 id 和文字算出（UUIDv5），hub 重启后重试也不会重复。飞书的同一件只发一次：同一件的回复排队处理；已经因回复处理掉、文字又相同的直接返回成功，不再发。文字不同照常发。飞书发到哪：私聊发进会话；`lark.thread_kind` 非空时回复 `reply_to` 并落进它的话题；群里 @我（`kind=mention`）的引用 `reply_to` 回复；其它群和没有 `reply_to` 的旧数据照旧发进会话 |
| POST | `/api/v1/inbox/{id}/doc/status` | `{ref, field, value}`：把云文档里这一行的状态改成 `value`（必须是这一列的可选值），先读最新的再写（别人刚改过就 409 失败），返回重新读过的这件 |
| POST | `/api/v1/inbox/refresh` | 立刻采集一次，返回列表（`?sources=all` 同上） |
| POST | `/api/v1/translate` | `{texts:[string]≤30, target}` → `{texts}`，顺序不变，模型没给的是空串；target 是 BCP 47（zh-Hans、en、ja…，`docs/ios.md`「翻译」） |
| POST | `/api/v1/translate/mail` | `{account_id, uid, mailbox, target}` → `{subject, text, clipped?}`：hub 读这封信自己翻，正文最多 12000 字 |

邮件回复可带详情返回的 `expected_uid` 和／或 `expected_version`。版本绑定账号、邮箱、线程中的 UID、消息标识及发件人；目标变化返回 HTTP 409，`code=mail_reply_target_changed`、`delivered=none`、`retryable=false`，此次没有发送。App 保留草稿，加载最新邮件并让用户确认新收件人后，用新的 `client_id` 发起新请求。网络结果不明时沿用原请求、原目标和原 `client_id` 核对回执。省略校验字段时沿用旧行为，现有 iOS 无需修改；持久回执规则不变。

`InboxItem`：`id`、`source`（lark / mail，或连接器的 id：github）、`kind`（dm / mention / group / bot / mail；连接器的是它自己的：review_requested / mention / team_mention / author / ci_failed / assign）、`account`（`feishu` 或邮箱账号 id）、
`account_name`、`title`（会话名 / 邮件主题）、`sender`、`avatar`、`preview`（最新一条）、`count`、`started_at`、`updated_at`、
`tier`（action / fyi / noise，分拣前为空）、`summary`、`reason`、`read`（在别处看过）、`done_at`、`done_by`（user / read / replied）、
`lark{feed_id,is_group,is_topic,start_position,read_position,mention_position,at_all,reply_to,thread_kind}`（`read_position`：这个会话在飞书里的已读位置，每次采集都刷新；`reply_to`：最后一条别人发的消息的 id，`thread_kind`：它在哪，空 = 在会话里、`topic` = 话题群的话题、`thread` = 普通群里的话题回复，这两个在采到新消息时刷新，旧数据没有）、`mail{account_id,mailbox,uids,message_id,from,direct,thread}`、
`translations{<目标语言>:{subject,preview}}`（hub 提前翻好的外文邮件，`docs/ios.md`「翻译」）、`doc{url,link,type(docx/doc/sheet/base/other),notice(mention/comment/reply),notice_id,rows:[{ref,table,title,fields:[{name,value}],status:[{field,name,value,options:[{name,color}]}]}],reply{kind(thread/record/cell/doc),label,…},error}`
（`kind=doc`，`docs/inbox.md`「云文档」）；`agent.changes:[{ref,field,value}]` 是 AI 分身随草稿提议的改状态；
`connector{thread,label,url,open_url,reply_to,can_reply,rule_tier}`：连接器的一件（`docs/inbox.md`「连接器：GitHub」），App 只画这一个块、不认识具体来源：
`label` 是行上的标签，`url` 在 App 内打开，`open_url` 交给系统打开（`sms:` 这类），`reply_to` 说回复会发到哪、谁能看到，`can_reply` 为假时不能回复（CI 失败）。
列表变化时 WebSocket 推 `{"type":"inbox"}`（不带内容，App 自己拉）。

## 飞书机器人

`/api/v1/bots/feishu`（读状态 / 改凭证、开关和功能）、`…/pair`（配对码）、`…/owner`（DELETE 解除配对）、`…/test`（测试消息），字段见 `docs/bots.md`「配置和接口」。

## 知识库 API

知识库未启用时返回 503；`kind` 支持 `projects`、`chats`、`people`、`docs`、`meetings`、`events`。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/kb/{kind}?tag=` | `{items,tags}`；可按标签筛选 |
| GET | `/api/v1/kb/{kind}/{id}` | `{detail,agents?}`；项目可额外返回相关在线 Agent |
| PUT | `/api/v1/kb/{kind}/{id}` | body 为 `kb.Update`，可更新名称、标签、重要性、摘要及项目属性；返回 `{id}`，字段见 [knowledge.md](knowledge.md) |
| GET | `/api/v1/kb-search?q=&messages=0` | `{hits}`；默认包含消息，`messages=0` 排除消息 |

## 飞书 API（lark-connector）

hub 经 hub 机器上 lark-connector 的 agent 接口（`/agent/v1`，token 从容器环境变量读）代理飞书数据，见 `server/internal/lark`。

lark-connector 出错时这些路由回 502 `{"error"}`；其中 lark-connector 自己丢了飞书登录（`AUTH_*`，`lark.ErrNotLoggedIn`）的，回 `{"error","code":"lark_not_logged_in"}`，App 据此提示去 lark-connector 重新登录飞书。`/raw/…` 例外，原样转发 lark-connector 自己的回答。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/lark/status` | `{"configured","connected","name","error","last_event_at"}`：设置 › 连接器 › 飞书。configured = hub.json 有 lark；connected 每次现问 lark-connector 的 `/accounts`；last_event_at 最近一条实时事件（Unix 秒） |
| GET | `/api/v1/lark/conversations?scope=&cursor=&limit=` | `{"conversations":[LarkConversation],"next_cursor"}`；scope：`chats`（默认，飞书「消息」：不含已完成 / 折叠，按标签显示规则过滤）、`unread`、`done`（已完成）、`label:<id>`、`inbox`、`mentions`、`muted`、`p2p`、`group`、`thread`、`later` |
| GET | `/api/v1/lark/pinned` | 全部置顶会话（读 inbox + done 两个 scope 的头部合并，按置顶顺序） |
| GET | `/api/v1/lark/labels` | `{"labels":[{"id","name","count"}]}` |
| GET | `/api/v1/lark/unread-count` | `{"count"}`：「未读 N」（不含免打扰） |
| GET | `/api/v1/lark/flags` | 标记：`{"conversations":[LarkConversation],"messages":[{"feed_id","chat_name","chat_avatar","flagged_at","message":LarkMessage}]}` |
| GET | `/api/v1/lark/conversations/{id}/labels` | `{"label_ids":[…],"labels":[{"id","name"}]}`（这个会话在哪些标签里 / 可选标签） |
| POST | `/api/v1/lark/conversations/{id}/settings` | `{"done"?,"pinned"?,"flagged"?,"muted"?,"label_ids"?}` → lark-connector `PATCH settings`（isArchived / isShortcut / isFlagged / isMuted / labelIds，labelIds 是全量） |
| POST | `/api/v1/lark/conversations/{id}/unread` | `{"unread":true}` 标为未读 / `false` 清除未读 |
| GET | `/api/v1/lark/conversations/{id}/messages?before=&after=&around=&limit=&topic=` | `{"messages":[LarkMessage],"has_older","has_newer","older_cursor","newer_cursor"}`，旧→新；不带游标 = 最新一页；`around=<读到的位置+1>` 从第一条未读打开；`topic=1` 时取话题群的话题列表（只能往前翻） |
| GET | `/api/v1/lark/messages/{id}/thread?before=&limit=` | 话题 / 回复串里的消息，格式同上；只支持 `before` 前翻，无游标时读取最新页；不支持 `after`，上游未提供更新方向分页与游标，见 [话题分页限制](feishu.md#话题分页限制h06) |
| POST | `/api/v1/lark/conversations/{id}/messages` | `{"text","client_id"?,"mentions"?}` 以本人身份发送，回 `{"ok":true,"message_id"?}`。`client_id` 是 App 生成的 UUID，原样交给 lark-connector 的 `clientId` 并在 hub 保存回执：同一条消息重发时带同一个，只会发出一条；不传或不是 UUID 时 hub 自己生成。`mentions` 是 `[{"user_id","name"}]`（最多 100 个，超过或缺 `user_id` / `name` 回 400），`text` 里要有字面的 `@name`，hub 不改写文字，原样交给 lark-connector 的 `mentions`；@所有人是 `{"user_id":"all","name":"所有人"}` |
| POST | `/api/v1/lark/messages/{id}/replies` | `{"text","thread"?,"client_id"?,"mentions"?}` 以本人身份回复这条消息，回 `{"ok":true,"message_id"?}`。`thread=false`（默认）在原会话里引用这条；`thread=true` 发进这条所在的话题（是普通群里的顶层消息时以它为根开一个话题；话题群里落进它的话题）。代理 lark-connector `POST /messages/{id}/replies`，`client_id` 原样透传为 `clientId` 并保存回执，`mentions` 同上一行；目标可以是根消息或话题中的回复，由上游解析话题归属。`text` 为空 400 |
| GET | `/api/v1/lark/conversations/{id}/members?q=&limit=` | @人用的成员：`{"members":[{"user_id","name","avatar"?}]}`。`q` 为空列出成员，不为空按名字 / 群昵称 / 拼音搜；`limit` 默认 50，最多 200；`name` 群昵称优先，没有时用显示名 |
| POST | `/api/v1/lark/conversations/{id}/images` | `{"client_id","name","mime","data"}`；data 为 base64，解码后最大 7.5 MiB，校验图片格式；回 `{"message_id"}` |
| POST | `/api/v1/lark/conversations/{id}/files?client_id=&name=&mime=` | `application/octet-stream` 原始字节，必须有 Content-Length，1 字节至 100 MiB；回 `{"message_id"}`；Hub 流式转发 |
| POST | `/api/v1/lark/conversations/{id}/read` | `{"position","badge_count"?,"message_ids"?}`：上报看到的消息（最高位置、该条的 badge_count、其间别人发的未读消息 id），只在消息真正上屏后发 |
| POST | `/api/v1/lark/conversations/{id}/read-states` | `{"message_ids":[…]}`（最多 100 个，超过 400）：这些消息的已读回执，回 `{"states":[{"message_id","read_count","unread_count"}]}`。lark-connector 没给回执的（还在发送、不认识的）不出现在 `states` 里；旧 hub 没有这条路由时 App 不显示回执 |
| GET | `/api/v1/lark/media/{key}?size=` | 图片 / 贴纸 / 头像 / 视频 / 文件原样转发（`size` 为 24 的倍数、≤624 时返回缩略图） |
| GET | `/api/v1/lark/messages/{id}/media/{key}` | 消息作用域的媒体（同上） |
| GET | `/api/v1/lark/emojis` | `{"emojis":{"<小写 key>":{"label","url"}}}`，含肤色变体 key；hub 缓存一天 |
| POST | `/api/v1/lark/messages/{id}/card-actions/{actionId}` | 以本人身份按下卡片回调按钮（body 原样转给 lark-connector） |
| GET / POST | `/api/v1/lark/raw/{path...}` | lark-connector `/agent/v1/<path>` 的只读代理，转发查询与允许的只读 POST；不作为通用写接口 |
| GET | `/api/v1/lark/reactions/quick` | `{keys}`：快捷表情 |
| POST | `/api/v1/lark/messages/{id}/reactions` | `{key,remove?}`：添加或撤销表情，返回 `{ok:true}` |
| GET | `/api/v1/lark/users/{id}?feed_id=` | 用户资料，原样转发上游 JSON |
| POST | `/api/v1/lark/users/{id}/chat` | 打开私聊，返回 `{feed_id}` |
| GET | `/api/v1/lark/messages/{id}/url-preview?url=` | 消息内链接预览，原样转发上游 JSON |
| GET | `/api/v1/lark/moments/posts/{id}` | 单帖内容，原样转发上游 JSON |
| GET | `/api/v1/lark/moments/posts/{id}/comments` | 单帖评论，原样转发上游 JSON |

飞书发送、话题回复和收件箱回复的可选 `client_id` 可直接用于重试，不要求额外请求头。hub 按操作种类和原始 ID 派生内部回执 UUID，复用 Postgres `client_ops`；同 ID、同目标与参数返回原 HTTP 状态和响应正文，不再发送，即使收件箱事项已变化或 hub 已重启。同 ID 换参数、目标或鉴权设备返回 `409 {"code":"op_id_reused"}`。已记录的上游错误也原样返回，避免结果不明时重复发送；执行尚未完成返回 `202 {"state":"pending"}`，重启时遗留的执行没有结果则返回 `202 {"state":"unknown"}`，客户端需核对。回执沿用 `client_ops` 的保留策略。请求同时携带 `client_id` 和 `X-Jarvis-Op-Id` 时，以 `client_id` 派生的回执为准。

话题回复使用独立的操作种类 `lark_thread_reply`，沿用上述回执规则。Android 发送前持久保存话题草稿及原请求；收到 `202` 或丢失回执时保留待确认状态，再次提交相同目标、文本和 `client_id` 核对原结果，不能生成新 ID 重发。`ok:true` 表示上游已接受，话题列表随后重新读取。

不提供 `client_id` 的旧客户端维持原行为：飞书会话发送由上游客户端生成 ID，收件箱飞书回复由 hub 按事项生成稳定 ID。HTTP 响应结构不变，不增加 WebSocket 事件。

```jsonc
// LarkConversation
{"id":"<feedId>","name","avatar":"/api/v1/lark/media/…","is_group","is_topic","unread","marked_unread","at_me","at_all","muted",
 "pinned","flagged","done","tags":["外部"|"公开"|"会议"|"机器人"|"AI"…],"preview","preview_sender","is_mine","updated_at",
 "read_position","mention_position"}
// LarkMessage
{"id","position","sender_id","sender_name","sender_avatar","sender_type":"user|bot","is_mine",
 "type":"TEXT|POST|IMAGE|CARD|SYSTEM|FILE|MEDIA|STICKER|MERGE_FORWARD…","text":"纯文本预览","created_at",
 "recalled","edited","reply_count","thread_reply_count","badge_count","at_me","is_read",
 "parts":[MessagePart],                       // lark-connector 原样（见下），媒体 URL 已改写到 /api/v1/lark/
 "reply_to":{"id","sender_name","text","recalled","parts"},
 "reactions":[{"key":"THUMBSUP","count":2,"mine":true,"users":["示例成员甲","示例成员乙"]}]}
// MessagePart（lark-connector 的 MessagePart，不改字段名）
{"kind":"text|at|link|image|file|code|emotion|media|card|docs|interactive|table",
 "text","url","refId","metadata":{…}}   // 卡片的整棵元素树在 metadata.body / header；富文本块祖先在 metadata.blocks
```

附件发送要求完整设备权限（answer-only 不可用），共用 Hub 绑定的 lark-connector 账号；不接受调用方覆盖账号。Hub 在 hello/health 中声明 `lark_attachments`。
`client_id` 必须为 UUID，同一消息重试保持不变；消息读取额外保留可选 `client_id`、`local_state`，用于消除发送超时后的迟到回显。`local:` 和 pending/failed 副本不算发送确认。

媒体改写规则：parts JSON 里所有 `"/api/media/` → `"/api/v1/lark/media/`，`"/api/messages/` → `"/api/v1/lark/messages/`
（合并转发卡片里嵌套的消息也一并改写）。

## 个人 Agent 来源状态

`GET /api/v1/agents` 返回 `{"sources":[{"source":"removedbridge","enabled","error?","setup?","providers":[…],"accounts":[…]}]}`。
`setup` 说明来源能连上、但还没有可列的东西的原因，客户端在会话列表为空时把它换成一句提示：
`missing`（lark-connector 版本没有 agent-bridge，hub 不再把 404 当错误，会话列表返回空）、`disabled`（没设置 `lark-connector_AGENT_BRIDGE=true`）、
`no_accounts`（开了但还没连接账号）。lark-connector 连不上之类的其他失败仍然放在 `error` 里，会话接口照常报错。

## 个人 Agent 列表偏好

`GET /api/v1/agents/prefs` / `PUT /api/v1/agents/prefs`（整份替换），存在 hub 的 settings 表（key `agents.prefs`），Web 和 iOS 共用：

```json
{"pins":["<sessionId>"],
 "view":{"group":"date|provider|none","sort":"updated|unread|name","show":"all|unread|sendable","providers":["grok_bot"]},
 "sections":[{"id":"s1","name":"工作"}],
 "assign":{"<sessionId>":"s1"},
 "updatedAt":"…"}
```

- 字段缺省 = 默认（按日期分组、按最近更新排序、显示全部、所有来源）。`pins` 有序、去重；`assign` 里指向不存在分组的条目会被丢掉。
- 置顶的会话总在最上面的「已置顶」，然后是自定义分组，再按 `view.group` 分组。
- PUT 成功后广播 `{"type":"agents.prefs"}`，其他端重新 GET。旧 hub 没有这个接口（404）时客户端只存本地。

## 消息里的标签（客户端解析）

| 标签 | 谁写 | 含义 / 显示 |
| --- | --- | --- |
| `<attached_images>` 每行一个路径 `</attached_images>` | hub（手机发图） | 消息附带的图片；气泡上方方形缩略图 |
| `<jarvis-context kind="context\|edit\|doc\|instructions" title="…" url="…">文字</jarvis-context>` | 手机（链接卡片里选中文字）、hub（飞书 /agent 的说明和上下文） | 不是用户写的：所有块收成气泡旁一行「上下文 · N 项」（只有一块时「上下文 · 标题」），默认折叠；展开是标题列表，每项再点开看正文和链接。属性顺序任意、可以缺（`url` 常缺）；标签外的文字才是用户写的 |
| `<cross-session-message from-name="…">…</cross-session-message>` | Claude Code（会话间消息） | 左侧「来自 名字」卡片 |

