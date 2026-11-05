# 协议（前后端契约）

本轮核对：2026-10-31，基于提交 `f3a13fdb7672927197cb8ac38bccb9c65671677b` 的 `server/internal/hub/` 路由、事件发送和数据库记录。以下超时、保留期和数量限制均为协议或配置，实际性能未实测。

所有 JSON 字段 snake_case。时间为 RFC3339（UTC）。HTTP `listen` 默认 `:18660`；TLS 通过 `public.tls_listen` 显式开启，提供 HTTPS / WSS。公网优先 IPv6，配置和放行方法见 [network.md](network.md)。

## 鉴权

- 优先使用 `Authorization: Bearer <token>`。当前实现未收紧 URL token：没有 Authorization 值时，所有鉴权 API（含 WebSocket）仍可回退到 `?token=`，不只限回环或 WS。客户端应使用请求头，避免凭据进入 URL。
- 工作区看板（#1646）将 GET `/api/v1/workspace-board` 与 `/api/v1/workspaces` 加入 `answer` 读取允许列表；修改看板仍仅限 `full`，详见「工作区看板」。
- hub.json 的 `token` 是主 token，只接受回环 / 内网等受信任来源；公网即使用 TLS 也返回 `403 master_token_local_only`。公网应使用临时配对 token 或设备 token。默认拒绝公网明文 API，返回 `403 tls_required`，仅 `GET /api/v1/health` 例外。
- 每台设备自己的凭据：`POST /api/v1/auth/tokens {"name":"<设备名称>","kind":"phone"}` → `{id,token,role,name}`。设备 token 以 `jd_` 开头，hub 只存哈希；`kind: watch` 得到 `answer`，其他 kind 得到 `full`。`full` 可以创建设备 token。`answer` 只能做手表用到的事（允许列表，`devicetokens.go`），其余一律 403，GET 也一样：读机器、会话、对话内容、交互和收件箱列表，回答交互，发消息，标已读 / 中断，收件箱标记完成、草拟、回复。文件、启动器、设备注册和 WebSocket 都不行。回答屏幕卡片只能选选项或按 `1`–`9`、`esc`、`enter`，不能打字，也不能选自由输入那一项（「Type something」，它要带文字；在卡片被认领之前就拒，卡片留着给手机）；发消息只能发给列表里的会话，只带文字（去掉控制字符），选不了启动器和模型，也不能带图；回复收件箱不能带文档状态变更，开了 `lark_agent` 时也不能发 `/agent`。发消息和批准计划是它本来的能力，所以手表丢了还是要撤销。新 App 配对后立刻换一个自己的 token，之后只用它；配对的手表再换一个 `kind: watch` 的，手表只拿这个；还没换到时（请求失败、旧 hub）手表不拿任何凭据，提示「还没有手表的凭据」，点「从 iPhone 同步」时手机再换一次（#1185：以前暂用手机那份 `full`，#1128 的限制就落空了）。
- `GET /api/v1/auth/tokens` 返回 `{tokens,current}`，`DELETE /api/v1/auth/tokens/{id}` 撤销凭据、清缓存（认证结果缓存 30 秒，撤销时清掉）并断开该设备的 WebSocket；App 在「设置 › 设备」里管理。旧 hub 无此接口时，旧客户端仍按原配对流程使用凭据。
- 电脑链路的凭据（[link.md](link.md)）：`POST /api/v1/auth/tokens {"kind":"link","machine":"<hub.json 里 kind 为 link 的机器>"}` → `role: link`，
  只能开 `/api/v1/link` 和 `/api/v1/link/stream`，其他接口 403；列表里带 `machine_id`。用给这台生成的配对码（下文 `{"machine"}`）兑换时，
  kind 和 machine 由配对码决定，请求里写别的机器返回 403。
- 扩展的 token（`pext_` 开头，#1855）：hub 按扩展接受了的范围（`permissions.hub`）签发，只开那些范围登记的路由，其余 403 `permission_denied`（缺范围时带 `scope`），
  只认本机来的请求；见 [extensions.md](extensions.md)「权限」「hub 的接口：范围」。
- hub 互联（[mesh.md](mesh.md)）：另一台 hub 的凭据 kind / role 都是 `peer`（`machine_id` 是对方的 hub_id），只能开 `GET /api/v1/peer`；
  互联码 `jx_…` 只能 `POST /api/v1/peer/pair`；别的接口一律 403。组里只有**中心 hub** 认 App 的凭据：成员把 App 的请求原样（带 `Authorization`）
  转给中心，成员加入时把它签过的凭据（hash）交给中心，所以配对在成员上的设备照常能用。中心连不上时成员回
  `503 {"code":"center_unreachable","center":{"hub_id","name"}}`。
- 公网鉴权失败按 IPv4 单地址、IPv6 /64 计数，封禁检查在查库前；`429 rate_limited` 带秒数形式的 `Retry-After`。内网 / VPN 不计数，参数见 [网络配置](network.md#hubjson-网络配置)。
- 服务（助手等独立进程）连 hub 的总线用另一种 token（`jb_…`），只能开 `GET /api/v1/bus`，见下文「总线」。
- 请求可带 `Accept-Language: zh-Hans|en|ja`（浏览器自带的完整列表也认，取第一项按 [i18n.md](i18n.md) 回退）：这次响应里给人看的文字（`error`、状态说明）用这种语言；`code` 不变，客户端仍以 `code` 分支。没带时用用户语言（见「客户端能力与心跳」的 `client_hello.locale`）。
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
- 另有：`assistant_compact`（手动压缩助手对话、会话的 `compaction`）、`assistant_main_new`（新建主对话），见下面「助手」的两个接口；`elicitation`（MCP 表单和链接）、`model_configuration`（按用途配置模型）、`plugins`（[插件](extensions.md)）、`plugin_ui_slots`（插件的 UI 插槽、UI 版本 2、`plugin_ui` 事件，[extensions.md](extensions.md)「UI 插槽」）、`plugin_auto_upgrade`（市场插件自动升级的开关和列表、`plugin_upgrade` 事件，[extensions.md](extensions.md)「自动升级」）、`desktops`（桌面端登记，见下文 `register_desktop`）、`locale`（认 `Accept-Language`、`client_hello.locale`、设备的 `locale`，给人看的文字按语言出，[i18n.md](i18n.md)）、`terminal_panes`（`GET/POST /machines/{mid}/panes` 和 `pane:<窗格 id>` 形式的终端流 id，[terminal.md](terminal.md)第十九节）。

能力位表示实现支持；`tls` 不代表已经开启监听，要同时看 `tls_port` / `fp`。新客户端遇到旧 hub 时按能力位退回旧行为。机器列表的 `node_version` / `node_mismatch` 用来提示 node 与 hub 的部署版本是否一致（那台电脑上的 personad 不是 hub 这次部署的，`deploy.sh` 只部署了一部分；hub 连上时也记一行日志）。新字段都是可选的，App 目前靠字段在不在判断；以后有「旧 hub 会误解」的新请求时先查这里。

`POST /api/v1/machines/{id}/deploy`：hub 把自己这次部署的 node（`deploy.sh` 留在 hub 机器的 `~/.persona/bin/nodes/jarvisd-<os>-<arch>`）经 ssh 装到那台电脑（正走 tailcat 时经 tailcat 发给常驻 node，它换掉自己后退出、由 launchd / systemd 重启），杀掉常驻 node（launchd 用新二进制重启；ssh 里的 `launchctl kickstart` 不总生效）、重新连接，后台做，立即回 `{ok}`（不能开始时 409 + 原因）。过程在机器列表里：`deploying`（进行中）、`deploy_error`（上一次为什么没成）；成功后 `node_mismatch` 消失。App 在「设置 › 机器」里提示并给一个「重新部署」按钮。

Persona Link 的主动 `link` / 被动 `service` 机器，以及经成员 hub 转发的 node，支持更新协议时经现有 node RPC 更新，不要求 SSH。`node.info` 的 `node_update` / `node_update_reason` 表示本机更新能力；机器列表的 `can_deploy` / `deploy_reason` 表示该入口当前能否使用，`deploy_phase` 为 `waiting_idle` / `transferring` / `restarting` / `confirming`。旧 node 先在本机升级一次。部署任务存 Postgres，支持等待空闲、校验安装、常驻进程重启、本地回滚与重新上线确认；详细 RPC 和恢复约定见 [node-update.md](node-update.md)。

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

Session 新增可选字段：`source_kind`（`codex_history` / `codex_app_server` / `identity_conflict`；Agent 运行时的会话是 `runtime`，见「Runtime ↔ node」；Claude Code 的后台会话是 `claude_bg`，只读，见 `docs/claude-integration.md`「后台会话」）、`source_thread_id`、`source_instance_id`、`parent_thread_id`、`read_only_reason`、`attached`、`capabilities`（read/send/interrupt/approve/terminal/attach 六个布尔值）。旧来源缺这些字段维持兼容，新来源缺能力默认拒写。没有终端能力就不显示终端/按键/关闭窗格。
Session 还有可选字段 `pane_index`（#2320）：这个 pane 在它的 tab 里按位置的序号（从 0 起，左上为 0）。herdr 的 node 按快照 `layouts` 的分屏树（`splits`，旧版 herdr 没有时按 rect）算，宿主不给布局、旧 node 或窗格不在布局里时不带；`0` 照样发，不带才是不知道；只有它变了也推 `sessions.changed`。客户端把同一个 tab（`slot_id`）的几个 Agent 按它排成一组，0 号是发起者；旧客户端忽略它（`docs/agent-states.md`「一个 tab 里的几个 pane」）。
Session 的空间还有可选字段 `parent_space_id`（#2073）：这个空间在宿主里挂在哪个空间下面，同机器、同 `space_kind`，顶层空间不带。Orca 宿主按 `worktree.ps` 的 `parentWorktreeId` 填（指向自己的当没有），hub 原样转发；旧客户端忽略它照旧平铺，网页的「按机器」把子空间折叠在父空间下（`docs/web.md`）。

思考强度（`docs/codex.md`「思考强度」）：手机经 app-server 驱动的 Codex 会话（`codex_app_server` 里能发送的，和接上共享守护进程的 herdr 窗格，后者没有 `source_kind`）带 `efforts`：`POST …/modes` 的 `effort` 能取的档位，从弱到强（这个 thread 的模型在 `model/list` 报的，拿不到时是 Codex 的 minimal … ultra）；`effort` 是它现在的值，`permission_mode` 不带。这类会话的 `/modes` 只收 `effort`，带 `permission_mode` 报错，档位不在 `efforts` 里也报错；没改成（app-server 断开、没有及时回应、Codex 拒绝）也报错，不会先回成功。其他会话都不带 `efforts`：Claude Code 和助手照旧用客户端自己的那套档位，别的不能改。客户端按 `efforts` 非空且能发送决定只显示思考强度，不按 Agent 种类猜；旧客户端忽略这个字段。

上下文用量（#2430）：会话带 `context_tokens`（agent 最近一次回复时模型看到的 token：Claude 是 `input + cache_read + cache_creation`，Codex 是 rollout 最近一条 `token_count` 的 `last_token_usage.input_tokens`）和 `context_window`（模型的窗口）；node 从 `readQuickMetaSince` 已经在读的 transcript 尾部算，不多读文件，尾部没有回复时沿用上一次读到的（`node/contextwindow.go`）。窗口：Codex 用 rollout 自己记的 `model_context_window`；Claude 按模型 id——`[1m]` 后缀是 100 万，已经看过超过 20 万 token 的也算 100 万（transcript 里的 id 没有后缀），其余 `claude-*` 是 20 万；不是 Claude 的模型（网关自己的）不知道窗口，只带 `context_tokens`。不知道的字段不带（`omitempty`）：还没有回复、老 node。客户端用 `context_tokens / context_window` 算百分比，缺任何一个都不画。

`POST /api/v1/sessions/{mid}/{sid}/attach`：用户要求实时接入，node 只对原实例仍 loaded、owner 唯一的 thread 请求 resume，且不带运行配置覆盖。发现/普通读历史不调用 resume。加入许可按 thread/稳定实例记在 settings；断线重验 owner，不自动冷恢复或换实例。loaded/list 与 resume 不原子，不能承诺中间没有 unload 竞态。

外部发送同样用 `/messages`，必须带 `client_msg_id` 并成功登记 Postgres 去重记录；不能在存储不可用时无保护地发出。未知发送结果（包括可能发生在 admission 后的通用 server error）返回 `delivered: unknown`，不能自动重发或将 steer 失败改成 start。发送记录不按一天过期，避免恢复很久以前的 outbox 后重复执行；只有明确未发送的记录可忘记。

审批 ID 含 machine/instance/connection generation/thread/原始带类型 request ID。手机明确回答才给原连接回复；未知请求、取消和断线不自动发 error/decline。`serverRequest/resolved` 只将卡片记 dismissed 并撤下，不生成批准。外部 interrupt 不清理所有后台终端；父管子 thread 的直接驱动尚未开放。

## 模型供应商与用途配置

原 `/api/v1/launchers` 保持兼容，供应商新增 `kind`（`launcher` / `api`）和 `api`（`messages` / `responses`）；API-only 不下发给 node。

- `GET /api/v1/model-config` → `{purposes, providers}`。
- `GET /api/v1/model-config/purposes/{id}` → `{purpose, providers}`。
- `PATCH /api/v1/model-config/purposes/{id}` 接收 `{revision, provider?, model?, effort?}`；缺失不改，`null` 清除覆盖，revision 冲突返回 409。成功返回同单项 GET。
  `inbox.triage` 改成调不了的、又有要它的插件开着时返回 422 `{code: "triage_model_in_use", plugins: [名字], error}`（`docs/extensions.md`「收件箱的通用规则」）。
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
 "workspace_cwd":"<node 验证的物理目录>",   // 可选；用于工作区关联，缺失时兼容回退 cwd，不替换原始 cwd
 "status":"idle|working|blocked|done|unknown|offline",   // offline = 不在 herdr 里运行，只有 transcript，可恢复
 "pane_id":"w1:p2","tab_id":"w1:t2","workspace_id":"w1","agent_name":"j-3f2a",
 "pane_index":0,   // 可选（#2320）：这个窗格在它的 tab 里按位置的序号，从 0 起，左上为 0（别的窗格都是从它切出来的，有 Agent 时就是这组的发起者）；
                   // herdr 的 node 从快照的 layouts 算，宿主不给布局、旧 node、窗格不在布局里时不带（0 照样发，不带才是不知道）。见 docs/agent-states.md「一个 tab 里的几个 pane」
 "agent":"claude","agent_label":"",   // agent_label 只在 App 不认识的 Agent 上有：runtime 的叫法（「DSH」，见「Runtime ↔ node」）
 "launcher":"cc_ds","model":"deepseek/v4.1-flash-chain","started_by_jarvis":true,"assistant":false,
 "updated_at":"...","last_text":"最后一条 assistant 文本的前 200 字",
     "turn_error":"API Error: Connection lost mid-response…（停着且这一轮被模型 API 错误打断时才有，否则不带；它的任务算「中断了」，#1516。Claude Code：hub 从最后一句的开头认；Codex：node 从 task_complete.error / 失败的 turn/completed 报，#2185）",
     "turn_error_kind":"transient（可选：agent 自己说了错误种类时才有，Codex 的 codex_error_info：transient 值得「继续」，user 要用户处理；没有就按 turn_error 的文字判，docs/agent-work.md「分诊」）","pending_interactions":1,
 "work":{"phase":"blocked","on":"external","line":"等部署跑完再跑线上测试","wait":"部署跑完","then":"跑线上测试","every_minutes":5,
         "wake_at":"...","attempts":1,"max":6,"turn_at":"...","at":"...","by":"model"},
     // 工作态（docs/agent-work.md）：phase working | blocked | done；blocked 的 on 是 user（等你决定，questions / link /
     // interaction_id 是 Persona 替它开的卡片）| agent（等另一个 Agent，agent:{machine_id,session_id,label}）| external（等部署、CI、时间点，
     // 到 wake_at Persona 叫醒它）| transient（模型服务暂时出错，到 wake_at Persona 发「继续」）；done 带 artifacts:[{kind:pr|commit|doc|build|url,ref}]。
     // 工作中只有 {"phase":"working"}；一轮停下后还没分诊（或看不出来）时不带。做完的、在等 Persona 叫醒的，state 是 idle 不是 unread。
     // 分诊是管理者（助手进程）做的，hub 照它在快照里报的填（只对还停在那一轮上的会话），见下「总线」的「Agents」
 "activity":"在跑测试","activity_at":"...",   // 最近一步是工具调用、之后还没有新文字时才有：这一步在做什么（node 从 transcript 概括，40 字以内）和开始的时间；写出新文字或开始新一轮就没了
 "queued":["工作中输入、还没被取走的消息"],
 "subagents":[{"id":"a849e1f0…","session_id":"<id>@a849e1f0…","type":"Explore","description":"…","tool_use_id":"toolu_…",
                "parent_id":"c27d03b1…","fork":true,   // 谁起的它：起它的子 Agent 的 id（主会话起的没有）；fork = 分叉出来的。父的不在列表里时当顶层
                "running":true,"background":true,"started_at":"...","updated_at":"...","last_text":"…"}],   // 子 Agent 当会话读
 "loop":{"kind":"wakeup|cron","prompt":"/loop …","reason":"…","interval_seconds":1800,"next_at":"...","cron":"*/5 * * * *","ticks":12},
 "goal":{"objective":"…","status":"active|paused|blocked|usage_limited|budget_limited|complete","reason":"…",   // 它在做的 goal（docs/agent-goal.md）
        "tokens_used":12345,"token_budget":500000,"seconds":600,"updated_at":"..."},                              // 没有 goal 时不带
 // Agent 的进程在监听、回 HTTP 的 TCP 端口（docs/ports.md）：一个端口一条；title 是它首页的 <title>（没有就缺省）。旧 node 没有这个字段
 "ports":[{"port":5173,"addr":"127.0.0.1","pid":4242,"process":"node","title":"Vite + React"}],
 // 关联的未完成待办（docs/todos.md）：只在当前有关联（未 released）且待办未完成时出现，由 hub 补齐
 "todo":{"id":"todo_…","title":"天气 App 发 1.3 版"},
 "quiet":true,   // 给安静的任务干活（干过）的 Agent（#1731，docs/unified-entry.md「安静的任务」）：不进任务通知和全局角标，任务做完后也一直带着
 "background":[{"id":"b85polnoj","kind":"shell|agent","description":"…","command":"…",   // 还没回报的后台命令 / 子代理
                "output_file":"/private/tmp/…/b85polnoj.output","started_at":"..."}],
 // 用户最近一次手动压缩（只有助手对话，POST …/compact 之后才有）：running → done | failed（error 是给人看的原因）；node 记着，直到下一次
 "compaction":{"state":"running|done|failed","error":"…","started_at":"...","finished_at":"..."}}

// Todo：待办事项（docs/todos.md）
{"id":"todo_…","title":"天气 App 发 1.3 版","notes":"说明","done_at":null,
 "created_at":"...","updated_at":"...",
 "origin":"<助手对话 id>","plan_id":"plan_…",   // 只有从助手对话开的任务才有（docs/unified-entry.md）
 "start_error":"rpc: connection closed",         // 任务的 Agent 没开起来：hub 同时写 done_at，任务到此结束；重新打开时清空（#1077）
 "quiet":true,   // 主对话派的、当时开着「主对话派的任务只在主对话里通知」：建的时候定下、不再变（#1731）
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
 "injected":{"kind":"task_notice","state":"done",…},   // 仅 user text：这条是 Persona 发给模型的消息（任务通知、定时任务、叫醒……），不是用户写的；hub 下发时补上，见「Persona 注入的消息」
 "compact":{"trigger":"manual|auto","pre_tokens":151467}, // 仅 notice：这里压缩了上下文（Claude Code 的 compact_boundary）；App 画成一条分隔线，紧跟的「已从压缩的上下文继续」和空闲压缩的说明并进它
 "task":{"kind":"shell|monitor|agent|task",              // 仅 notice：后台任务的通知（命令、Monitor 事件 / 结束、子 Agent），text 是一行摘要
         "status":"completed|failed|killed|stopped|event|ended|waiting","title":"description 或命令第一行",
         "task_id":"…","tool_use_id":"toolu_…","exit_code":0,"output_file":"…",
         "detail":"Monitor 的事件 / 子 Agent 的结果 / 整条命令（已解码实体）"},
 "fold":{"items":1294,"tools":647,"thoughts":110,     // 仅 kind=fold（请求带 fold_work 才有）：一段被折叠的工作，见「折叠的工作」；可带 tasks、images
         "until":"...","through_seq":6890,"through_id":"…#0"},
 "image":{"mime":"image/png","data":"base64"}}        // 仅 image（tool_result 里的图片也拆成 image item，带 tool_use_id）

// Interaction：需要人处理的事
{"id":"ix_…","machine_id":"server","session_id":"uuid","session_title":"…","cwd":"…",
 "task_title":"…",   // 提问的 Agent 在为从助手对话开的任务干活时，hub 发给客户端时填上任务名（docs/unified-entry.md）
 "kind":"question|plan|permission|screen|elicitation",
 "created_at":"…","expires_at":"…","tool_name":"AskUserQuestion","tool_use_id":"…",
 "questions":[{"question":"…","header":"…","multi_select":false,
               "options":[{"label":"…","description":"…"}]}],   // question
 "plan":"markdown",                                              // plan
 "tool_input":{...}, "summary":"rm -rf build",                   // permission
 "screen":"终端可见区域文本",                                     // screen（hook 覆盖不到的 TUI 对话框）
 "always_keys":["tool:Bash"],  // node → hub：助手对话的授权卡上「总是允许」放行什么，hub 记下后自己回答同样的卡（docs/assistant.md「本会话总是允许」）；客户端不用
 "agent":"codex", "reason":"…", "always_label":"本会话不再问这个命令"} // node 从 Codex / TRAEX 的 TUI 读出来的卡（「Codex 的提问和审批」）

// Resolution（按 kind）
{"answers":{"<question 原文>":"所选 label；多选用 \", \" 连接；或自定义文本"}}   // question
{"approve":true,"mode":"bypassPermissions|acceptEdits|default"}               // plan 批准
{"approve":false,"feedback":"改成……"}                                        // plan 打回
{"allow":true,"always":false} / {"allow":false,"message":"…"}                // permission
{"keys":["down","enter"],"text":""}                                         // screen：按键 / 文本发到 pane
{"dismiss":true}                                                            // 任意：放弃，交回电脑 TUI 处理
{"answers":{"…":"…"},"answered_by":"assistant"}                               // 助手代答（#1602）：只有 question 可以，App 回复不带 answered_by
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

## 工作区看板

工作流说明、升级和持久化见 [工作区看板](workspace-board.md)。下列接口以当前 hub 的 Postgres 为唯一持久状态，没有内存或 JSON 文件写入降级。`answer` 允许 GET 看板和清单，POST 要求 `full`；`can_write` 表示当前请求的角色能否编辑，不额外授予文件、Git 或 Agent 操作权限。

```text
BoardState { revision: int64, columns: Column[], cards: Card[], can_write: bool }
Column { id, title }
Card { id, machine_id, workspace_id, title, cwd, source, source_id?, branch?,
       column_id, pinned: bool, archived: bool }
Workspace { id, machine_id, source, source_id, title, cwd, branch?,
            online: bool, available: bool, last_seen?: RFC3339 }
```

默认 revision 为 0，列 ID 为 `todo`、`doing`、`review`、`done`。数组顺序是人工顺序，客户端按列、归档状态和置顶分组后保持数组相对顺序。卡片自身 ID 稳定；工作区 ID 由 hub 按机器、来源和来源 ID 限定，不能拿宿主本地 ID 直接匹配另一台机器。

POST 必须显式给出 `revision`，成功后数据库原子递增并返回完整状态。可用 action 与参数如下：

| action | 参数 |
| --- | --- |
| `add_card` | `column_id,machine_id`，另提供 `workspace_id` 或 `cwd`，可选 `title,pinned,archived`；ID 由 hub 生成 |
| `update_card` | `id`；仅更新给出的 `title,pinned,archived` |
| `move_card` | `id,column_id,before_id?`；省略 before 放到目标列末尾 |
| `add_column` | `title`；ID 由 hub 生成 |
| `update_column` | `id,title` |
| `move_column` | `id,before_id?`；省略 before 放到末尾 |
| `delete_column` | `id,move_to?`；含卡片（含归档）时必须提供另一列，不能删除最后一列 |

`add_card` 的工作区必须属于指定机器。hub 重新枚举或核对会话目录，再让 node 以 `workspace.resolve` 验证并解析物理目录；客户端不能写入 `source/source_id/branch`。同一有效机器、同一规范目录只允许一个新绑定，归档也参与判重。路径按远端语法处理（POSIX、Windows 盘符、UNC），不能使用 hub 自己的文件系统解析远端路径。机器 alias 后来被吸收时，仅在响应中投影到当前路由身份，不合并或改写原卡的列与顺序；原始状态继续参与修改和 CAS。

错误：无效 action、路径、引用、重复绑定等返回 400；非 full 写入 403；revision 过期或提交时竞争返回 409 `conflict`；存储不可用返回 503（未实现存储能力时 code 为 `not_supported`）。客户端遇到 409 应先重读，不能自动覆盖最新状态；其他失败恢复上一次确认状态。旧 hub 可返回 404。

GET 清单合并宿主空间、当前会话目录和已绑定目录。宿主未知目录可以为空，客户端不能据此绑定；机器离线或枚举失败时使用最后快照，并标记可用性，发现错误放入 `errors`。部分宿主失败不能清空其他宿主的结果。alias 快照按当前有效机器归组，以最新完整快照为准。卡片在工作区消失、机器离线及 hub 重启后保留。

Session 新增可选 `workspace_cwd`：由 node 验证、解析符号链接的物理目录，只用于工作区关联，原始 `cwd` 保持不变。不存在、不可解析或不允许读取的目录可以不返回该字段；旧客户端忽略它，新客户端优先使用它，缺失时回退 `cwd`。该值经过短期缓存，不构成后续文件操作的权限或存在性保证。

看板默认关闭（#2475）：`GET /api/v1/workspace-board/settings` 返回 `{enabled,can_write}`，值存在 Postgres 的 `settings` 表（key `workspace_board.settings`，`{"enabled":bool}`），整个 hub 一份，所有浏览器和设备一致。从没设过时，`workspace_board` 的 `revision > 0`（看板被改过）就算打开，否则关闭；推断值不回写，第一次 PUT 才存下。PUT `{enabled}` 仅 `full`，成功后 WS 推送 `{"type":"workspace_board_settings","enabled":bool}`。开关只决定客户端显示不显示：关闭时看板和清单接口照常可用，数据不删。旧 hub 没有这个接口返回 404，客户端按打开处理（旧版本对所有人显示）且不显示开关。

移动、归档等看板修改不调用 Agent 或 Git 操作。成功后 WS 推送 `{"type":"workspace_board","revision":<新版本>}`，客户端据此重读。网页还在看板可见期间每 10 秒刷新发现，回到前台时立即刷新，以补足目录和连接状态变化。

## REST（App → hub）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/machines` | `{"machines":[Machine]}` |
| GET | `/api/v1/workspace-board` | `{revision,columns,cards,can_write}`，完整工作流看板；`answer` 可读，见「工作区看板」 |
| POST | `/api/v1/workspace-board` | `{revision,action,...}` → 更新后的完整看板；仅 `full`，过期版本 409 `conflict` |
| GET | `/api/v1/workspace-board/settings` | `{enabled,can_write}`：看板的开关（#2475），整个 hub 一份；没设过时按看板是否改过推断，见「工作区看板」 |
| PUT | `/api/v1/workspace-board/settings` | `{enabled}` → `{enabled,can_write}`；仅 `full`，缺 `enabled` 返回 400 |
| GET | `/api/v1/workspaces` | `{workspaces:[Workspace],errors?:[{machine_id,error}]}`；独立于 Agent 的工作区发现，失败保留历史信息，`answer` 可读 |
| POST | `/api/v1/machines/{mid}/reconnect` | 重连该机器的 node |
| GET | `/api/v1/machines/{mid}/panes` | 终端浏览器的树（feature `terminal_panes`，仅 `full`）：`{hosts:[{kind, stream, control, new_tab, workspaces:[{id,label,number?,cwd?,branch?}], tabs:[{id,workspace_id,label,number?}], panes:[{pane, terminal, tab_id, workspace_id, agent?, status?, title?, cwd?, session_id?}]}]}`；包括没有 Agent 的 shell 窗格。`terminal` 是终端流用的地址 `pane:<pane>`，`session_id` 是跑在那个窗格里的 Agent 会话（有就用它开终端） |
| POST | `/api/v1/machines/{mid}/panes` | `{workspace_id, kind?}`：在这个空间新开一个 shell 标签页（不抢电脑上的焦点，目录是空间自己的，请求不能指定）→ `{pane, terminal, tab_id}`；仅 `full`，只有用户点了才调用 |
| POST | `/api/v1/machines/{mid}/panes/close` · `…/panes/split` · `…/panes/agent` · `…/panes/git` · `…/tabs/close` · `…/tabs/rename` | 终端节点菜单的动作（`docs/terminal.md` 第二十一节），仅 `full`：`{pane}` 关窗格；`{pane, direction: right\|down}` 拆分 → `{pane, terminal}`；`{pane, launcher}` 在 shell 窗格里启动 Agent → `{session_id}`；`{op: pull\|push\|fetch, pane \| workspace_id+kind?}` → `{op, message, output?}`；`{tab_id, kind?}` 关标签页；`{tab_id, label, kind?}` 改标签页名。只认这些字段，不能指定目录或命令 |
| PUT | `/api/v1/machines/{mid}/engine` | `{"engine":"herdr"\|"herdr-core"\|"herdr-both"\|"orca"\|"paseo"}` → 200 更新后的 Machine；不认识的引擎 400 `{"error"}`，没有这台机器 404。离线也能改（存下，连上时生效）；改完照常推 `machines`（下面「引擎」） |
| GET | `/api/v1/machines/{mid}/computer` | 这台 Mac 现在有没有 Cua Driver、能不能被助手操控（hub 当场问 node 的 `computer.status`）→ 200 `Computer`（`{enabled, installed, version?, daemon, accessibility?, screen_recording?, desktop?, screen_sharing, ready, note?, job?}`，[computer-use.md](computer-use.md)）；插件 `cua` 没开、或不是 Mac → 404 |
| PUT | `/api/v1/machines/{mid}/computer` | `{"enabled": bool}`：机器页「允许助手操控这台电脑」开关，写进插件 `cua` 配置里的 `machines` → 200 `Computer`；插件没开、不是 Mac → 404。改完照常推 `machines` |
| POST | `/api/v1/machines/{mid}/computer/{install\|grant\|cancel}` | 机器页的「安装 Cua Driver」「去授权」「取消」：node 在后台跑（`computer.install` / `computer.grant` / `computer.cancel`），→ 200 `Computer`，进度在它的 `job`（`{kind, state: running\|done\|failed\|cancelled, step?, error?, started}`），页面接着 GET。插件没对这台打开 404；离线、已有任务、node 太旧 409 |
| GET / PUT | `/api/v1/machines/{mid}/browser` | 这台 Mac 的 ego lite（插件 `ego`，[plan-ego-browser.md](plan-ego-browser.md)）：GET 当场问 node 的 `browser.status` → `Browser`（`{enabled, installed, version?, set_up, imported, analytics?, ready, note?, screen_sharing, job?}`）；PUT `{"enabled": bool}` 是机器页的开关（插件的 `machines`）。插件没开、不是 Mac → 404 |
| POST | `/api/v1/machines/{mid}/browser/{install\|open\|cancel}` | 机器页的「安装 ego lite」「打开 ego lite」「取消」：node 在后台跑，进度在 `job`。插件没对这台打开 404；离线、已有任务、node 太旧 409 |
| GET | `/api/v1/machines/{mid}/launchers` | `{"launchers":[Launcher]}` |
| GET | `/api/v1/machines/{mid}/launchers/{name}/models?refresh=1` | 这个启动器的 CLI 在 `/model` 里列的模型（新建 Agent 选模型，`docs/model-config.md`「新建 Agent 选模型」）：`{"agent","models":[{"id","name","description"?,"resolved"?,"default"?,"efforts"?,"default_effort"?}],"locked"?}`。`id` 是开始会话时 `model` 填的值；`default` 是不给模型时跑的那个（选它等于不选）；`resolved` 是别名（opus…）按这个启动器的设置对应的模型；`efforts` 是它能用的思考强度，从弱到强；`locked` 是启动器写死了自己的 `--model`、选了也不会生效的原因。没有列表可问的 Agent（TRAEX、runtime 的）回 `models: []`。node 起一次 CLI 去问（几秒），存 10 分钟，`refresh=1` 重新问；列不出来回 502 `{"error"}`；这台电脑的 node 还没有这个功能时回 404 `{"error","code":"node_outdated"}`（和旧 hub 一样，App 不显示模型行） |
| GET | `/api/v1/model-config/providers/{id}/models?refresh=1&machine=` | 模型配置里启动器类供应商（`launcher.<名字>`）的 `/model` 列表（`docs/model-config.md`「模型配置选模型」）：同上一行的回答，多一个 `machine`（问的是哪台）。在有这个启动器的在线电脑里挑一台：`machine` 指定的、助手所在的、hub 本机、其余的。没有这个供应商、插件 / API 类供应商 404；有它的电脑都不在线 503；node 太旧 404 `node_outdated` |
| GET | `/api/v1/todos` | 全部待办（新建的在前）：`{"todos":[Todo]}`，每个待办带 `spaces` 和 `agents`（过滤 dismissed，当前的在前按 `linked_at` 降序，然后是参与过的按 `released_at` 降序） |
| POST | `/api/v1/todos` | 新建待办 `{"title","notes"?}` → 200 `{"todo":Todo}`；`title` 不能为空（400）；只读设备 403 |
| PATCH | `/api/v1/todos/{id}` | 修改待办 `{"title"?,"notes"?,"done"?}` → 200 `{"todo":Todo}`；`done: true` 标记完成，`false` 重新打开；不存在 404；只读设备 403 |
| DELETE | `/api/v1/todos/{id}` | 删除待办及关联（Agent 本身不动） → 200 `{"ok":true}`；不存在 404；只读设备 403 |
| PUT | `/api/v1/todos/{id}/spaces/{mid}/{space...}` | 整组挂工作区项目到待办 → 200 `{"ok":true,"linked":N}`；`space` 为 URL 编码的项目名（通配接住）；已完成 409，不存在 404，只读设备 403 |
| DELETE | `/api/v1/todos/{id}/spaces/{mid}/{space...}` | 解除整组挂（物理删除待办下该项目的 space 关联 Agents） → 200 `{"ok":true}`；不存在 404，只读设备 403 |
| PUT | `/api/v1/todos/{id}/agents/{mid}/{sid}` | 关联已有 Agent → 200 `{"ok":true}`；同一 Agent 原挂在别处标 released，清掉 dismissed，已完成 409，不存在 404，只读设备 403 |
| POST | `/api/v1/todos/{id}/retry` | 重试没开起来的任务（#1396）：照计划里的这个任务（机器、目录、提示词、启动器 / 模型、权限、标题）在同一个待办上再开 Agent；待办马上重新打开（`done_at`、`start_error` 清空，`reopened_at` 记下），计划里这个任务的 `error` 清空并推 `task_plan`，Agent 在后台开 → 200 `{"todo":Todo}`。已经在重开或已经开起来了也是 200（原样返回，不再开一次）；再失败照常写回 `done_at` + `start_error`、更新计划的 `error`，不追加第二条回执。目标机器离线 409 `「某电脑」现在离线，等它连上再重试`（待办不变），不是助手派出的任务或不是启动失败（比如手动完成的）409，不存在 404，只读设备 403。**未完成的任务**不重开：有 Agent 停在模型 API 错误上（`turn_error`，#1516）时给它发「继续」，200 原样返回待办，同一次打断连点只发一次；那台电脑离线 409；没有被打断的 Agent（在做、等你……）200 原样返回、什么都不发 |
| GET | `/api/v1/task-plans/{id}` | 一个计划（`TaskPlan`，见下面「任务和对话动态」） → 200 `{"plan":TaskPlan}`；不存在 404 |
| POST | `/api/v1/task-plans/{id}/start` | 开始计划 `{"tasks"?:[PlanTask],"remember"?:bool}`：`tasks` 按下标覆盖用户在卡片上改过的字段（空的不改），`remember` 记住「这类以后直接开」；每个任务建一个待办（`origin`、`plan_id`）、在后台开 Agent → 200 `{"plan":TaskPlan}`；已开始或不开了 409，不存在 404，只读设备 403 |
| POST | `/api/v1/task-plans/{id}/dismiss` | 不开了 → 200 `{"plan":TaskPlan}`；已开始 409 |
| GET | `/api/v1/assistant/conversations/{id}/feed?since=` | 对话动态：`since`（RFC3339）之后的条目，旧的在前，一次最多 500 → `{"items":[FeedItem],"plans":{"plan_…":TaskPlan},"views":{"view_…":AssistantView},"view_actions":{"<key>":ViewAction},"view_blocks":["<item id>/0"]}`（plans 和 views 是引用对象的当前状态；views、view_actions、view_blocks 总是返回该对话完整快照，不受 since 限制，更新不移动原 feed 位置；view_blocks 只含服务端已注册的正文卡片 key，不重复发送正文） |
| POST | `/api/v1/assistant/conversations/{id}/views/{key}/actions` | 活卡片或已注册正文卡片的一次 reply：`{action,page?,values?,client_msg_id,confirmed?}` → `{action:ViewAction}`；服务端从保存的 View 取 reply 原文，拒绝客户端覆盖和未声明字段。另一提交 409 `already_answered`，附已有 action；同一 id／动作／值重试返回原记录。提交与待发送文字一起落库，pending／sent／failed 通过事件和 feed 同步。key 整段 URL 编码（包括 `/` 和 `#`），只读设备和扩展 token 403；不接受 HTML bridge 的动态文字 |
| POST | `/api/v1/assistant/conversations/{id}/today` | 这段对话今天的「今天」卡片：已有就回那张，没有就建一张（`today` 条目，`at` 是用户那天的零点）→ `{"item":FeedItem,"new":bool}`；只读设备 403 |
| GET | `/api/v1/task-trust` | 「这类以后直接开」记住的几类 → `{"kinds":[{"key","machine","cwd","readonly"}]}` |
| DELETE | `/api/v1/task-trust?key=` | 忘掉一类（以后又要先给计划）→ `{"ok":true}`；没记过 404；只读设备 403 |
| POST | `/api/v1/assistant/conversations/{id}/route` | 直接对任务说话 `{"todo_id","text"}`：hub 发给这个任务最要紧的 Agent（等你 > 工作中 > 完成未看 > 最近的），追加 `routed` 条目 → 200 `{"item":FeedItem}`；不是这段对话的任务、没有在跑的 Agent 409；只读设备 403 |
| DELETE | `/api/v1/todos/{id}/agents/{mid}/{sid}` | 移出 Agent（软移出：置 `released_at` 和 `dismissed_at`，接口不再返回该行且不再自动挂回） → 200 `{"ok":true}`；不存在 404；只读设备 403 |
| GET | `/api/v1/machines/{mid}/dirs?path=` | 目录浏览：`{"path","parent","entries":[{"name","path","is_git"}],"recent":[{"path","name","last_used"}]}`；path 为空时 entries 是 home |
| GET | `/api/v1/sessions?machine_id=` | `{"sessions":[Session]}`，含运行中的（herdr）和最近 7 天的 offline 会话 |
| POST | `/api/v1/sessions` | `{"machine_id","cwd","launcher","agent_type"?,"model"?,"prompt"?,"resume_session_id"?,"workspace_id"?,"permission_mode"?,"effort"?,"tab_name"?,"async"?,"name_hint"?,"todo_id"?}` → `{"session":Session}`。可选 `X-Jarvis-Op-Id` 接入 `start` 持久回执。`model` 可选：新建 Agent 里选的模型（上面 `…/launchers/{name}/models` 的 `id`），只管这一次，盖在「模型配置 › Agents」上（管理者的 `agents.start_params`，见「总线」的「Agents」；它没连着时 hub 等它一会儿，等不到自己套「模型配置 › Agents」，不起标签名）。`todo_id` 可选：新建成功后在同一请求内挂上（不存在 404、已完成 409、只读设备 403）。`tab_name` 是 herdr 标签页的名字；不给时管理者用小任务模型按 `prompt` 起一个短英文名（`assistant/agents_start.go`，约 2 秒，失败或管理者没连着就不起），node 再兜底成 `agent-<n>`，同一空间里重名加 `-2`、`-3`（`node.tabLabel`）。新建空间时它的第一个标签页也用这个名字。助手的 `agent_start` 同样自动起名。`"async": true`（App 都带）：立刻回会话（新会话 id、目录、启动器，`starting: true`），开标签页、等 Claude 就绪在后台做，见下面「新建会话」；这时 hub 不等起名，标签页先叫 `agent-<n>`，名字出来后再 `session.rename`。`name_hint`：App 把第一条消息当普通消息发（马上上屏）时，用它起名、不进 `prompt` |
| POST | `/api/v1/tabs/autoname` | `{"dry_run"?,"all"?}` → `{"tabs":[{machine_id,session_id,space,title,from,to,error?}]}`：把在跑的 Agent 里「没人起过名」的标签页（`jarvis`、`jarvis-N`、herdr 的数字、`agent-N`）按会话标题改成短英文名，规则同新建；`dry_run` 只列出会改成什么，`all` 连用户起过名的也改。模型偶尔回空，会再问一次，仍没有就留原名（`error`）。2026-10-31 在两台机器上跑过一次：61 个标签页里改了 39 个，用户自己起的 21 个没动。管理者答（hub 转），它没连着时 503 `assistant_offline` |
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
| POST | `/api/v1/sessions/{mid}/{sid}/modes` | `{"permission_mode"?, "effort"?}`：改在跑的会话的权限模式 / 思考强度（`docs/ios.md`「思考强度和权限模式」）；会话列表里的 `permission_mode`、`effort` 是当前值。带 `efforts` 的会话（经 app-server 的 Codex）只收 `effort`，取值在 `efforts` 里（「Codex 来源与能力」）。`POST /api/v1/sessions` 和 `POST /api/v1/assistant/conversations` 也收这两个字段，作为启动时的值 |
| POST | `/api/v1/sessions/{mid}/{sid}/rename` | `{"tab_name"?, "title"?}`：tab 用 `herdr tab rename`；会话名给在跑的 claude 发 `/rename <名字>`（它写 `custom-title`），没在跑就直接往 transcript 追加同样的 `custom-title` 行。助手的对话：`title` 同时记成用户起的标题（hub 的 `assistant.titles`，不再被自动标题覆盖，`docs/assistant.md`「对话标题」）；Codex 的助手对话只记在 hub |
| GET / PUT | `/api/v1/mutes` | 屏蔽，结构同 pins，另有 `allowed`（空间被屏蔽时单独开启通知的 Agent）。规则：Agent 自己在 sessions 里→屏蔽；在 allowed 里→不屏蔽；否则看它的空间。被屏蔽的 Agent / 空间不推送任何通知（完成、需要你），手机上弱化显示；hub 内存缓存，PUT 时更新 |
| GET | `/api/v1/hub/update` | hub 的版本和更新状态（`docs/hub-update.md`）：`{version, build, channel, can_update, why_not?, latest?{version, build, released, notes, newer, bad, held?}, state, progress?, busy:[{machine_id, machine, session_id?, title}], error?, auto, auto_notice, last_check?, confirmed}`；`state`：`idle` / `checking` / `available` / `downloading` / `waiting_idle` / `restarting` / `confirming` / `updated` / `rolled_back` / `failed`；`busy` 是重启会打断的（在工作或在等回答的对话、正在重新部署的机器）；`auto_notice`：还没告诉过用户「自动更新已开启」；`latest.held`：更新的这一版自动更新不会装的原因（它的代码不一定包含这个 hub 正在运行的提交，`docs/hub-update.md`「防退回」），手动更新照样可以；Docker 的 hub 另带 `image{name, tag, pinned}`（它跑的镜像，`pinned`：标签是个版本号，要先改 `.env`），`latest` 是 registry 上最新的正式版本（`build` 为 0、没有 `released`），`POST …/check` 去问 registry |
| POST | `/api/v1/hub/update/check` | 立即检查，回同上 |
| POST | `/api/v1/hub/update` | `{"now"?: true}` 开始更新：下载、校验、等助手和 Agent 都空闲、替换、重启；`now` 是二级确认后的「现在就更新」，不等空闲。不是发布版（`dev` / `docker`）、没有更新的版本时 409（`error` 说明，Docker 给出 `docker compose pull` 的命令） |
| PUT | `/api/v1/hub/update/settings` | `{"auto"?: bool, "auto_notice_seen"?: bool}`：自动更新开关（默认开），改了开关也算告诉过了 |
| GET | `/api/v1/hub/update/history` | `{"history":[{from_version, to_version, to_build, started, ended, result: updated\|rolled_back\|failed\|held, reason?, auto}]}`，新的在前，留 50 条；`held`：更新的版本没有自动装（`reason` 写原因），每个版本记一次 |
| GET | `/api/v1/skills` | 跑助手那台机器上的 skill（只读）：`{"machine_id","machine","skills":[{"name","description","source":"user\|builtin"}]}`；用户的在那台机器的 `~/.persona/skills/`（`docs/hub-update.md`）。node 太旧（没有 `skills.list`）或不在线时 502 |
| GET / PUT | `/api/v1/pins` | 开发列表置顶：`{"spaces":["server/app"],"sessions":["server/<session id>"]}`，空间用「机器/空间名」（herdr 重启后 workspace id 会变），存 Postgres settings |
| GET | `/api/v1/machines/{mid}/file?path=&cwd=&offset=` | 读一个文件（文件查看器，`docs/ios.md`「文件查看器」）：`{"path","name","size","mime","text"\|"data","truncated","offset","next","is_dir","entries"?}`。文本文件每次最多 2 MB，从 `offset` 读起，没读完时停在最后一个完整行的末尾，`next` 是下一页的起点（读完为 0）；二进制文件整个给（上限 2 MB），不能从中间读；目录给前 500 项。`~` 是 home，相对路径在 `cwd` 里；拒绝 iCloud。带 `stream=1`（hub 特性 `file_stream`）时二进制文件只回描述 `{"stream":true,"revision",...}`、不带 `data`，内容走 `/file/download`；`mime` 按固定扩展名表、再按开头字节嗅探（见下面「文件预览」） |
| GET | `/api/v1/machines/{mid}/file/download?path=&cwd=&inline=` | 完整文件的原始字节，App 用同一 Bearer 鉴权；网页的 `<video>` / `<audio>` / `<img>` 发不了头，用 `?token=`（hub 从不记录查询串）；hub 用 node 的 `file.chunk` 每次最多 1 MiB 读取，单次请求不把整个文件放进内存。支持 HTTP Range / Content-Range（包括文件末尾范围），附带 Content-Length、ETag 和附件文件名；空路径 400、旧 node 409 `node_outdated`，非普通文件、iCloud 和读取错误拒绝。后续块必须匹配首次读取的文件大小和版本，文件中途变更或请求取消就停止下载。旧 `/file` 的 2 MiB 二进制预览行为不变。`inline=1` 只对图片、音视频、PDF 生效（`Content-Disposition: inline`），其余照旧附件；除 PDF 外一律带 `Content-Security-Policy: sandbox` |
| GET | `/api/v1/machines/{mid}/file/thumb?path=&cwd=&size=` | 文件的缩略图（hub 特性 `file_thumb`，node `file.thumb`）：长边最多 `size` 像素（默认 256，32–1024），不透明的给 JPEG、带透明的给 PNG；`ETag` 是「文件版本-尺寸」，带 `If-None-Match` 回 304，`Cache-Control: private, max-age=86400`。画不了回 404 `no_thumbnail`（客户端显示图标），旧 node 409 `node_outdated` |
| GET | `/api/v1/machines/{mid}/file/convert?path=&cwd=&to=pdf` | 用那台电脑的 LibreOffice 把文档转成 PDF（hub 特性 `file_convert`，node `file.convert`，网页的 Office 回退，#1345）：回 `{"path","size","mime"}`，`path` 是 node 缓存里的 PDF，再用 `/file/download?path=<它>&inline=1` 读。按文件版本缓存，7 天后清；一次只转一个，90 秒超时；输入先拷成固定文件名、用临时配置目录，不和用户自己开着的 LibreOffice 抢。没装 404 `no_converter`，旧 node 409 `node_outdated` |
| GET | `/api/v1/machines/{mid}/git/blob?cwd=&path=&rev=` | 文件在某个版本的内容（hub 特性 `git_blob`，node `git.blob`，网页的对比用，#1437）：`rev` 是 `HEAD`、`INDEX`（暂存区）、提交哈希或「哈希^」（它的父提交），`path` 相对仓库根。回 `{"path","size"}`，`path` 是 node 缓存里按 blob id 命名的文件（带原扩展名），再用 `/file/download` 读；那个版本没有这个文件回 `{"missing":true}`。最大 200 MB，7 天清理 |
| GET / POST / DELETE | `/api/v1/machines/{mid}/browser/cookies` | ego lite 里的登录（插件 ego，#1304，`docs/plan-ego-browser.md`「导入 cookies 做了什么」）：GET 列网站 `{"sites":[{"domain","count","expires"?}]}`（不含名字和值）；POST `{"cookies":[{name,value,domain,path?,expires?,secure?,http_only?,same_site?,host_only?}]}` 导入，回 `imported`、`skipped`；DELETE `?site=a&site=b` 或 `?all=1` 清除，回 `removed`。只认用户自己的设备 token（配对 token 等 403），hub 不保存，目标 Mac 离线 409 |
| GET | `/api/v1/machines/{mid}/stat?paths=&cwd=` | 会话里提到的路径在这台机器上在不在：`paths` 一行一个（也收 `path=a&path=b`），最多 200 个 → `{"stats":[{"path"(原样),"kind":"file"\|"dir"\|"denied"\|"missing","size"?}]}`。`denied` 是 macOS 不让 personad 读的文件夹（打开时会说怎么授权）；相对路径没有 `cwd`、iCloud 里的路径、敏感文件（见下）一律 `missing`；一次最多等 3 秒，没答上的（挂住的网络盘）也是 `missing`。手机只给 file / dir / denied 的路径做可点的文件块，网页内联成链接 |
| GET | `/api/v1/machines/{mid}/fs?path=&cwd=` | 文件树：`{"path","entries":[{"name","dir","symlink","size","mtime","ignored"}],"truncated"}`，目录在前，最多 2000 项；拒绝 iCloud；`ignored`（#2439）：Git 忽略它（node 对这一页名字跑一次 `git check-ignore`，不在仓库里或没有 git 时都不标，不出现这个字段） |
| GET | `/api/v1/machines/{mid}/git/status?cwd=` | `{"is_repo","root","branch","head","upstream","ahead","behind","files":[{"path","orig_path","index","worktree","added","deleted","binary"}],"stashes","commits":[最近 30 条]}`（只读，`GIT_OPTIONAL_LOCKS=0`） |
| POST | `/api/v1/machines/{mid}/git/pull` | `{"cwd"}` → `{"status": GitStatus, "updated": bool}`；只允许有上游的普通分支与干净工作区，执行 `git pull --ff-only --no-rebase --no-autostash --no-recurse-submodules`。分叉、未提交改动、无上游、认证失败或超时均报错，不自动 stash / 合并 / 变基；pull 命令最多 60 秒，node 总计 75 秒、hub 90 秒。仅 full 角色可调用 |
| GET | `/api/v1/machines/{mid}/git/diff?cwd=&path=&staged=0\|1` | `{"path","diff","truncated"}`；未跟踪文件对 /dev/null 出 diff，最多 512 KB |
| GET | `/api/v1/machines/{mid}/git/show?cwd=&hash=` | `{"commit","files","diff","truncated"}` |
| GET | `/api/v1/machines/{mid}/git/log?cwd=&skip=&limit=` | `{"commits":[…]}` 分页 |
| POST | `/api/v1/sessions/{mid}/{sid}/fork` | 分支到新聊天（#2137）：body `{at_item_id}`（某条回复的 item id，空则整段）→ `{session_id}`。node 把 Claude 的 transcript 复制到这条回复所在那一轮结束，换成新会话 id，原会话不动；新会话是离线会话，下一条消息照常 `--resume` 它。会话上 `can_fork: true` 才支持（Claude Code、有 transcript、不是外部来源 / runtime / Codex），否则回 `code: fork_unsupported`；从正在写的那一轮分支回 `fork_turn_running`。边界：只复制对话本身，`/rewind` 用的文件快照（`~/.claude/file-history/<id>/`）和子 Agent 的记录不复制，分支里 `/rewind` 不能回到分支前的改动 |
| POST | `/api/v1/sessions/{mid}/{sid}/close` | 退出 claude 并关闭 tab（herdr 里的任何 Agent，手动启动的也行；分屏的 tab 只关它自己的窗格） |
| DELETE | `/api/v1/sessions/{mid}/{sid}` | 删除对话：只是从各端的会话列表里移除（hub 在 settings `sessions.deleted` 记下时间，`sessions` 推送和 `GET /api/v1/sessions` 都不再带它），电脑上的 transcript 不动；之后这个会话再被用到（`updated_at` 晚于删除时间，或又运行起来）会重新出现。启动失败的（`start_error`）也能删，立即消失。→ `{"ok":true}`；正在运行或还在启动 409，助手的对话 400（用归档），没有这个会话 404，只读设备 403 |
| GET | `/api/v1/assistant` | `{"machine_id","launcher","cwd","timezone","notes"?,"notes_effective","main"?,"main_tasks_quiet","conversations":[Session]}`：`notes` 只在 App 里保存过时有，`main_tasks_quiet` 是「主对话派的任务只在主对话里通知」的生效值（没改过时是 `true`，#1731），`main` 是主对话的会话 id（在 `machine_id` 那台机器上）；已归档的对话默认不列，`?archived=1` 一起列（`archived: true`） |
| PUT | `/api/v1/assistant` | `{"machine_id"?,"launcher"?,"timezone"?,"notes"?,"main"?,"main_tasks_quiet"?}`，存 Postgres settings（`docs/assistant.md`）；只改带了的字段，`notes` 为空串也算改；`main` 为 `"-"` 时清掉主对话（换机器时 App 一起发） |
| POST | `/api/v1/assistant/conversations` | `{"prompt","images"?,"purpose"?,"machine_id"?,"main"?}` → `{"session"}`：写好 CLAUDE.md 后在助手目录新开会话。`purpose`：用哪个模型用途（hub 认识的，如 `lark.agent`，不认识回 400；空着是 `assistant`）；`machine_id`：在 hub 的哪台机器上跑（空着是助手那台；没有这台回 400）。插件的 /agent 用这两个（#1484） |
| POST | `/api/v1/assistant/conversations/{id}/archive` | 归档助手的一段对话（只是不在列表里，数据保留）→ `{"ok","archived":true}`；主对话 400。会话列表（`sessions` 推送）里它带 `archived: true`，App 收在「已归档」；用户再给它发消息（`POST …/messages`）自动取消归档（`docs/assistant.md`「管理自己的对话」） |
| POST | `/api/v1/assistant/conversations/{id}/unarchive` | 取消归档 → `{"ok","archived":false}` |
| POST | `/api/v1/assistant/conversations/{id}/compact` | `{"machine_id"?}`（空着是助手那台）：手动压缩这段对话（Claude 发 `/compact`，进程不在就先 `--resume`；Codex `thread/compact/start`）→ 开始了就回 `{"ok":true}`，进度看会话的 `compaction`。409 + `code`：`busy`（正在回答）、`compacting`（已经在压）、`gone`（记录不在了）、`unsupported`（runtime 跑的对话）；`error` 是给人看的中文 |
| POST | `/api/v1/assistant/main/new` | `{"carry":"summary"\|"none","expect"?}`：新建主对话。`carry`（默认 `summary`）带上旧主对话最近一次压缩的摘要和之后的几条消息；`expect` 是 App 看到的主对话 id，对不上回 409 `main_changed`；旧主对话正在回答 / 压缩时 409 `busy`。成功后主对话清空（`GET /api/v1/assistant` 的 `main` 没了），旧的改名「旧主对话 · 至 M月D日」并归档 → `{"ok","archived":"<旧 id，没有主对话时为空>","carried":bool}`。之后第一条带 `main:true` 的 `POST /api/v1/assistant/conversations`（以及飞书机器人、渠道发给主对话的消息）开新的主对话，带过去的背景作为 `<jarvis-context kind="context" title="接上旧主对话">` 放在第一条消息前，只用一次（`docs/assistant.md`「新建主对话」） |
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
| POST | `/api/v1/interactions/{id}/resolve` | body 为 Resolution；回 `{"ok":true}`。已经结束的（#1673）：已回答的（或交回电脑后再交回）幂等回 200 `{"ok":true,"already":true,"status","resolution"}`，`resolution` 是当时生效的答案（按历史记录脱敏），可能和这次发的不同，App 据此把卡片显示成已回答；过期、撤回、交回电脑的回 410 `{"error","code":"gone","status"}`，查不到结局的 410 不带 `status` |
| POST | `/api/v1/devices` | `{"device_id","name","kind":"iphone|mac|watch","apns_token"?,"apns_env"?,"topic"?,"push_key"?,"relay_url"?,"relay_grant"?,"locale"?}`；`locale` 是这台设备的界面语言（`zh-Hans` / `en` / `ja`），推给它的通知用这种语言，没带用用户语言；后三个是推送中继（手机公钥、中继地址、中继给的凭据，`docs/push-relay.md`），hub 没有 APNs key 时据此推送。互联组里只有中心推送：连成员时这条也被转给中心；推送的 payload（直推的和经中继密封的）顶层带 `hub_id`：中心的（[mesh.md](mesh.md)「凭据和推送」）。`mac` 是 Mac App（同一个 bundle id，同一个 topic）：不收静默唤醒，它连着 hub 时不收「需要你处理」「Agent 做完」（App 自己发本地通知）。hub 记下是哪个设备凭据登记的，吊销那个凭据时连这台设备一起删掉，不再推送 |
| POST | `/api/v1/devices/{id}/live-activity` | 实时活动的 token：`{"push_to_start_token"}`、`{"activity_id","push_token"}`（会轮换，重复发）、`{"activity_id","ended":true}`（`docs/live-activity.md`） |
| GET | `/api/v1/fleet` | `{"fleet": FleetState}`：当前的实时活动内容（App 回到前台时拉），`stats` 带全部数字和每台机器的负载 |
| GET | `/api/v1/usage/windows` | `{"accounts": [{id, provider, name, plan?, windows: [{label, window_minutes, used_percent, resets_at?}], sampled_at, stale?}], "checked_at"}`：各台电脑的 node 从本机文件读到的 Codex / Claude 订阅额度窗口，hub 合并同一个账号、按最满的窗口排序；没有数据是 `[]`（`docs/usage-windows.md`，#2432） |
| GET | `/api/v1/metrics` | `{"machines": [{machine_id, online, system, latest: MetricSample \| 无, points: [MetricPoint]}], "step": 60}`：App「机器」页每张卡片上的现在值和走势，按配置顺序的每台机器，`points` 是最近 1 小时、每分钟一个点；`latest` 只在 30 秒内收到过样本时有（`docs/machines.md`） |
| GET | `/api/v1/machines/{id}/metrics?range=1h\|6h\|24h` | `{"metrics": {同上一台}, "range", "step"}`：机器详情的曲线，步长 10 / 60 / 240 秒（约 360 个点）；没有这台机器 404，range 不对 400 |
| GET | `/api/v1/usage` | `{"usage": {tpm, tokens, output, requests, machines:[{id, tpm, tokens, load}]}}`：所有机器的 token 用量（今天按手机时区算，`load` 是 1 分钟负载 / 核数），见 `docs/live-activity.md`「用量」 |
| GET / PUT | `/api/v1/devices/{id}/prefs` | 这台设备要哪些推送：`{"input":true,"done":"off|long|always"}`，另有 `translate`（设置 › 翻译：`{enabled,target,mail}`，收件箱（助手）据此提前翻译新邮件）、`live_activity`（默认 true）、`lock_details`（锁屏显示详情，默认 false）、`island`（灵动岛两侧：`{"preset":"a|b|c|custom","left","right","machine"}`，默认 `a`）、`mute_while_desktop`（手机静音，默认 false：有桌面端连着时不给这台设备推「需要你处理」「Agent 做完」；助手按你的要求发的提醒照推）；`look`（这台手机的实时活动画哪个形象：`{"design","light","dark","package"?}`，App 换形象时单独 PUT，hub 原样放进这台设备推送的 content-state，见 `docs/live-activity.md`「形象」；design 超过 32 字节或 package 超过 160 字节的不存）；PUT 可以只带要改的字段；返回值另带 hub 的 `long_turn_seconds`、`done_delay_seconds`（只读）。设备还没注册推送时也能设 |
| PUT | `/api/v1/devices/{id}/widgets` | 这台设备屏幕上有哪些扩展的小组件 `{"widgets":[{extension,id,family}]}`（整份替换，最多 64 个）：扩展发 `ui.invalidate` 时 hub 给有这个小组件的设备发后台推送 `extension_widget`（每台每个 15 分钟最多一条），见 `docs/extensions.md`「系统级位置」 |
| POST | `/api/v1/notify` | 给各台设备发一条提醒（给扩展用，见 `docs/extensions.md`「发通知」）：`{"title","body","machine_id"?,"session_id"?,"level"?:"active\|time-sensitive","id"?,"silent"?,"actions"?,"ui"?,"state"?}`，标题和正文至少一个（标题最多 120 字、正文 1000 字），`machine_id` 和 `session_id` 要么都给要么都不给，`level` 默认 `active`，同一个 `id` 在手机上替换上一条（apns-collapse-id，最长 64 字节），`silent: true` 不带声音。扩展凭据可带最多 3 个 `actions`、`ui` 与 `state`（通知按钮规格见 `docs/extensions.md`「通知按钮」）。带会话时点开进那个会话；会话被屏蔽回 `{"sent":false,"reason":"muted"}`，不发。按设备的 `input` 开关（关掉「需要你」就不收），也照「手机静音」和桌面端在线的规则；常驻连接里是 `notify` 事件，`kind` 为 `alert`（没有会话时 `machine_id`、`session_id` 是空字符串）；推送的 collapse id 是 `notify:<id>`，不会和 hub 自己的推送撞。payload 编码后超过 3500 字节、`machine_id` / `session_id` 超过 128 字节回 400。完整权限的凭据与具有 `notify` 范围的扩展凭据可调用，非扩展凭据带 `actions`/`ui`/`state` 回 400。回 `{"sent":true}` 只表示交给了推送，不保证送到 |
| GET | `/api/v1/personas` | 形象（`docs/persona.md`）：`{"personas":[{"id","rev","bytes","manifest":{persona.json 原样}}]}`，personad 内置的包在前（`~/.persona/personas/` 里同 id 的目录优先），再是 `~/.persona/personas/` 下检查通过的包（文件齐全、id 和目录同名、不超大小）；`rev` 是整个包内容的哈希，任何一个字节变了它就变，手机按 rev 缓存。分级形象（奶蛙）只给当前档：`manifest` 是这一档自己的 persona.json（文件在 `tiers/<档>/` 下），`rev` 是这一档文件的哈希，换档就是 rev 变了。没有包时是空列表 |
| GET | `/api/v1/personas/{id}/{file…}` | 包里的一个文件（PNG / persona.json），只给包目录里的普通文件（不跟软链、不给点开头的） |
| GET | `/api/v1/plugins` | hub 的插件：`{"plugins":[{"id","name","kind","status","error"?, "version", "source", "enabled", "contributes", "needs_setup"?, …}], "can_install"}`，`status` 是 `disabled / needs_setup / incompatible / stopped / starting / running / error`（`docs/extensions.md`「hub 的接口」）；`features` 带 `plugins` 时才有 |
| GET / PUT / DELETE | `/api/v1/plugins/{id}/-/…`、`/api/v1/plugins/-/…` | 插件的详情、开关、配置、密钥（完整权限的设备），预览 / 安装 / 升级 / 回滚 / 卸载（只认主 token 经回环）；`features` 带 `plugin_manager` 时才有，见 `docs/extensions.md`「hub 的接口」 |
| GET | `/api/v1/machines/{mid}/ports/{port}` | 端口探测（`docs/ports.md`）：`{"port","listening","http","title"?}`，只看不放行 |
| POST | `/api/v1/machines/{mid}/ports/{port}/open` | 用户在 App 里点开一个不在任何 Agent 名下的端口：有东西在听就放行 24 小时、回同上；没有 404；只读设备 403 |
| GET | `/api/v1/machines/{mid}/ports/{port}/connect` | 端口转发：升级成 WebSocket，二进制消息就是一条 TCP 连接的字节（手机本地监听，浏览器每开一条连接就连一次）。端口必须是这台机器某个会话 `ports` 里的或点开过的，否则 403；没在听 404；只读设备 403；机器离线 503；一台机器同时超过 64 条 429 |
| POST | `/api/v1/phone/runtime-cli` | `{cmd, args, timeout}`：让手机上的 DEBUG 包执行一条 Runtime CLI 命令（`docs/runtime-cli.md`「经 hub 连真机」），回 App 的原样回答 `{"ok": …}`。只给 `full` 设备凭据（只读设备 403）；没有登记了 `phone_runtime_cli` 的 App 在线 503 `phone_offline`（不唤醒）；`timeout` 秒内（最多 120）没回答 504；App 说这次调用失败 502。每次调用 hub 记一行日志（命令、调用方设备 id、状态、耗时），不记参数和回答 |
| 任意 | `/api/v1/plugins/{id}/-/machines/{mid}/{path…}` | 插件在电脑 `mid` 上的程序的 HTTP（`node.http`，`docs/extensions.md`「在电脑上运行的程序」），WebSocket 也是：hub 经 `plugin.node.dial` 开流转发，去掉 `Authorization` 和 `token`；只给完整权限的设备；插件没在这台开 404，机器离线 409，程序不应答 502 |
| 任意 | `/api/v1/plugins/{id}/{path…}` | 原样转发给插件的 `/{path…}`（`/-/` 开头的不转发）（WebSocket 也是），去掉 `Authorization` 和 `token`；只读设备 403。语音插件的契约见 `docs/extensions.md`「语音」 |

**按路径读文件的边界**（`file`、`file/download`、`file/thumb`、`file/convert`、`stat`、`fs`，node 的 `pathguard.go`）：只给已认证的客户端（设备 token）。路径逐段解析符号链接，**每一段在看之前**先检查，所以经链接指过去的也拦得住、且根本不碰目标：
- **iCloud**（任何一段含 `Library/Mobile Documents`，不分大小写）：没解锁的 Mac 上一碰就卡住，`stat` 当不存在，读取拒绝。
- **敏感文件**：home 下的 `.ssh`、`.gnupg`、`.aws`、`.azure`、`.kube`、`.docker`、`.password-store`、`Library/Keychains`、`.local/share/keyrings`、`.config/gcloud`、`.config/gh` 整个目录，
  `.netrc`、`.git-credentials`、`.npmrc`、`.pypirc`、`.pgpass`、`.claude/.credentials.json`、`.codex/auth.json`，任何地方叫 `id_rsa` / `id_ed25519` 等的私钥，`.persona` / `.jarvis` 里的 `hub.json`（连备份 `hub.json.*`）、`apns*` 和 `.key` / `.p8` / `.pem` / `.crt`（知识库 `data/` 照常能读）。`stat` 当不存在，读取、列目录、下载都拒绝。
- 链接最多跟 40 层（环就停）。这是基本防护，不是沙箱：设备 token 本来就能让 Agent 读这些文件。

邮件见下文「邮件 API」。HTTP 错误格式和可选字段见上文「错误体」。

## WebSocket `/api/v1/ws`

信封 `{"type":"…", ...}`。

服务端 → 客户端：

| type | 字段 |
| --- | --- |
| `hub_restarting` | `back_in_ms`：hub 要重启了（部署、`systemctl restart`、更新），接着就是 1001「hub restarting」。客户端立刻重连、界面保持原样（[hub-restart.md](hub-restart.md)）。不带 `event_seq`，不补发 |
| `hub_update` | `update`（同 `GET /api/v1/hub/update`）：检查、下载、等空闲、重启、确认、回滚，状态一变就推；`restarting` 之后连接会断一会儿，是预期的 |
| `hello` | `server_version`, `client_id`, `features`, `hub_id`, `hub_name`, `endpoints`, `tls_port`, `fp`, `mesh`, `channel`, `build`, `protocol`, `min_app_build`（后四个同 `/api/v1/health`），`event_stream`, `event_seq`, `resumed`（见下「事件续传」），`locale`（用户语言，feature `locale`）；每条 WS 首条消息，尚不含协商后的心跳。`mesh`：`{"hubs":[{hub_id,name,online,self,center,endpoints,tls_port,fp}]}`，互联组里所有 hub（含自己，`self: true`；配过对但现在到不了的 `online: false`；`center: true` 是组里的中心，客户端在同组的几台里优先连它），没有互联时只有自己。连成员时 hello 来自中心（`hub_id` 是中心的）（[mesh.md](mesh.md)） |
| `endpoints` | `endpoints`（完整候选列表，仅发给声明同名能力的连接） |
| `client_hello_ack` | `heartbeat: {interval, pong_timeout}`（本次协商后实际采用的秒数） |
| `machines` | `machines` |
| `workspace_board` | `revision`：看板修改已保存，客户端重新读取完整状态；不代表 Agent 状态变化 |
| `workspace_board_settings` | `enabled`：看板的开关改了（`PUT /api/v1/workspace-board/settings`），客户端据此显示或隐藏入口 |
| `todos` | —：待办或关联变了就推，客户端收到后重拉 `GET /api/v1/todos` |
| `plugin_ui` | `plugin`, `id`, `badge`（`{text, tone}`，`null` 是清掉）：插件改了它主导航入口（`nav_page`）的角标，[extensions.md](extensions.md)「UI 插槽」 |
| `assistant_feed` | `conversation`, `item`（`FeedItem`）：对话动态多了一条；kind `view` 是活卡片的原始位置，body.view 是 AssistantView |
| `assistant_view` | `conversation`, `view`（schema 的 AssistantView：id、conversation、version、view、created_at、updated_at）：已存入数据库的活卡片更新；同一卡片每秒最多一次，按 version 忽略旧更新；不另发通知 |
| `assistant_view_block` | `conversation`, `key`：正文卡片已从可信助手 text item 校验并存入数据库，可以启用其一次 reply；重放同一来源不重复广播，进现有事件续传，漏掉后由 feed.view_blocks 快照补齐 |
| `assistant_view_action` | `conversation`, `action`（ViewAction：key、action、page?、values、client_msg_id、status、revision、error?、at、updated_at）：卡片提交及发送状态；按 revision 忽略旧状态，进现有事件续传 |
| `task_plan` | `plan`（`TaskPlan`）：计划交给用户、开始、不开了、某个任务没开起来（`tasks[i].error`） |
| `sessions` | `machine_id`, `sessions`（该机器全量，变化时推，和上一条一模一样就不推；每个会话带 `rev` = transcript 字节数，变了就说明有新内容；可选 `suggestion`：Claude 一轮结束后在输入框里给的灰色「下一条」建议，见 `docs/claude-integration.md`；可选 `attention_reason`：会话在「需要你」时，宿主说的原因（botmux 的 attention reason）；可选 `external_url`：在 Persona 之外打开它的地方（botmux 会话的飞书话题），见 `docs/engines.md`「botmux」） |
| `revs` | `machine_id`, `revs`（`{session_id: rev}`，只含变了的；列表里看得见的字段没变、只是 transcript 长了时发，推给所有客户端，App 据此后台拉增量） |
| `lark` | `feed_ids`（飞书有新消息 / 会话变化的会话）；`settings_changed`（可选布尔值，默认 false，本批包含消息展示设置变化，与会话 ID 独立；客户端同时重读设置）；`account`（这些会话所在的 lark-connector 账号编号）；`ol_account`（只在不是默认账号时有，等于 `account`：读这些会话要带的参数，`docs/feishu.md`「多账号」）。hub 跟随 lark-connector 每个已登录账号的实时事件 `/agent/v1/events`，每个账号每 400ms 最多发一次，推给所有客户端。 |
| `items` | `machine_id`, `session_id`, `items`，以及这批在 transcript 里的位置（见下「items 的连续性」）：`history`, `to_seq`, `from_seq`（接续的批次才有）。只推给订阅了该会话的客户端；没有 item 的行（ai-title 之类）也会推一条空的 `items`，让 App 知道这几行已经看过 |
| `delta` | `machine_id`, `session_id`, `text`（无头助手正在生成的整段文字；空串表示这轮结束）。约每 80 ms 一条；App 每个对话最多每秒画 5 次，停顿后的第一条立刻画、之后画最新的，空串立刻画，消息落进 transcript 或重连时丢掉还没画的（`DeltaPacer`） |
| `live` | `machine_id`, `session_id`, `live`（`SessionLive` 或 `null`）：工作中的 Claude 的状态行，只发给订阅了该会话并声明 `session_live` 的连接，见下「实时状态」 |
| `screen` | `machine_id`, `session_id`, `text`, `status`, `ts`（node 已不再随订阅采样屏幕：终端页自己按需读 `/screen`，旧 App 收不到这条也不影响） |
| `fs_changed` | #2439：`machine_id`, `cwd`, `dirs`（这个页面 `fs_watch` 里变了的文件夹）, `git`（`cwd` 的 Git 状态变了）。只发给 `fs_watch` 过的页面，不进事件日志、重连不补发（页面重连后自己刷新一遍） |
| `terminal_control` | `machine_id`, `session_id`, `state`：`active`（接管了，带 `cols`, `rows`）、`busy`（电脑上别的程序占着，问用户要不要 `takeover`；先镜像着）、`taken`（被别的设备或程序抢走）、`lost`（node 自己放手了）、`unsupported`（旧 node / herdr，只能镜像），`error` 是给人看的原因 |
| `terminal` | `machine_id`, `session_id`，以及一帧 `epoch`, `seq`, `full`, `cols`, `rows`, `data`（base64 的 VT 字节），或者 `error`（流停了：电脑离线、会话不在跑……）+ `unsupported`（旧 node / herdr 不能推流，改用 `/screen` 轮询）。见下「终端流」 |
| `interaction` | `interaction`（新的待处理，推给所有客户端） |
| `interaction_resolved` | `id`；助手代答时另带 `answered_by: "assistant"`（#1602） |
| `fleet` | `fleet`（实时活动汇总，与 HTTP `/api/v1/fleet` 的内容对应） |
| `hook_event` | node hook 事件的原字段，hub 补 `machine_id` 并置 `type: hook_event`；用于刷新会话运行状态；`Stop` 带 `stop_sig`（回复签名，hub 用来给「完成」推送去重，见 `agent-states.md`「hook 的轮次信号」）；全局只报告的 hooks（`claude-integration.md`「全局只报告的 hooks」）带 `report_only: true`，hub 不据此认为会话有 Persona 的 hooks，其余照旧 |
| `tool_call` | `call_id`, `name`, `args`（只发给已 `register_phone` 且提供该工具的手机） |
| `desktops` | `desktops`：连着的桌面端（Mac App），`[{"device","name","since"}]`，`device` 是它的设备凭据 id（用配对凭据连的为空，不能单独退出）。连上时发一次，之后有桌面端连上 / 断开就推给所有客户端（feature `desktops`）。iPhone 据此显示「Mac 上的 Persona 已登录」 |
| `notify` | `kind: interaction / done / assistant`、`machine_id`、`session_id`、`title`、`subtitle`、`body`，交互另带 `interaction_id`；只发给声明 `ws_notify` 的连接，见下文 |
| `lock_test` | `lock_test_id`、`lock_test_due_at`、`lock_test_sent_at`、`lock_test_title`、`lock_test_body`；只发给本设备声明 `lock_test` 的选定连接，见「锁屏测试」 |
| `inbox` | 无内容载荷；列表变化提示，客户端重新拉取收件箱 |
| `personas` | 无内容载荷；形象列表变了（分级形象换了档），客户端重拉 `GET /api/v1/personas`，新的包下载完整后再换（`docs/persona.md`「分级形象」） |
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
| `fs_watch` | #2439（文件和 Git 的自动刷新，下面「文件自动刷新」）：`machine_id`, `cwd`（会话目录）, `paths`（这个页面打开着的文件夹，绝对路径，最多 32 个）, `git`（是否显示 `cwd` 的 Git 状态）。整份替换这个页面对这台机器、这个 `cwd` 的集合；`paths` 空且 `git` 为 false 就是释放（连接断开也释放）。只有完全权限的设备能发，答题设备（手表）忽略 |
| `register_phone` | `device_name`, `tools`（`[{"name","description","input_schema"}]`，即 MCP 工具定义）, `simulator`（模拟器只在没有真机连着时接工具调用）；可选 `device_id`、`platform`。按设备替换注册，空工具列表只清除该设备的供给；缺少 `device_id` 时用鉴权设备 ID，再回退到 `legacy`。调用先筛选提供该工具的在线手机，再优先真机、最近活跃连接。iPhone 的 `device_id` 是推送用的设备 id（同 `POST /api/v1/devices`）：连着的这段时间 hub 不给这台设备的实时活动推 APNs，App 自己画，`docs/live-activity.md`「推送节奏」；老 App 不带。`tools` 里的 `phone_runtime_cli`（DEBUG 包打开了经 hub 的 Runtime CLI）不算手机的工具：hub 把它拿出来，不进 Agent 的工具列表、Agent 调不到，只给 `POST /api/v1/phone/runtime-cli`；只登记了它一个（脚本驱动的模拟器）时这条连接不算手机。 |
| `register_desktop` | `device_name`：Mac App 登记成桌面端，不是手机——不接 `phone_*`，也不挡 hub 推送唤醒 iPhone（`hub/desktops.go`）；`device_id`（推送用的设备 id，同 `POST /api/v1/devices`：连着的这段时间 hub 不给这台 Mac 推「需要你处理」「Agent 做完」，它自己发本地通知；老 App 不带）。退出它就是吊销它的设备凭据（`DELETE /api/v1/auth/tokens/{id}`，hub 随即断开它的连接、删掉它登记的推送） |
| `tool_result` | `call_id`, `content`（`[{"type":"text","text"}|{"type":"image","data","mime_type"}]`）, `is_error` |
| `client_hello` | `device_id`, `platform`, `version`, `build`, `os: {name, version, sdk}`, `features`, `heartbeat: {interval?, pong_timeout?}`, `locale`（`zh-Hans` / `en` / `ja`，界面语言），均可省略，见下文 |
| `ping` | 必须带字符串 `id`，允许空字符串；缺失、null 或非字符串时不回复 |

### 事件续传（`event_seq`，2026-11-04）

hub 发给所有客户端的广播（这张表里除 `hello`、连接快照、`sessions_delta`、`items`、`delta`、`terminal*`、`tool_call`、`notify`、`lock_test`、`pong`、`error`、
`client_hello_ack`、`hub_restarting` 以外的）带 `event_seq`：同一条流（`hello.event_stream`）里每条加一，hub 重启后接着编号。

- 客户端记住 `event_stream` 和收到的最大 `event_seq`，重连时带上：`/api/v1/ws?event_stream=<流>&event_seq=<n>`（两个都带才算）。
- `hello.resumed: true`：紧跟 hello 补发 `event_seq > n` 的广播（按顺序、不重复），之后是实时的；客户端不用全量重拉。连接快照照常发（完整状态，静默应用）。
- `resumed: false`（流变了：hub 崩溃过；漏掉的已经不在 hub 留的最近 2000 条 / 15 分钟里；超过 400 条）或旧 hub 没有这些字段：照以前全量刷新。
  `hello.event_seq` 是此刻的最后一条，全量刷新后从它接着记。
- 需要填写的授权卡片（elicitation 的 `interaction`）不补发，靠连接后的待处理交互快照。
- 订阅（`subscribe`）的补发之后，正在生成的无头助手会话紧跟一条当前的 `delta`。

### 常驻连接通知（ws_notify）

`notify` 是事件名，`ws_notify` 是双方声明的能力名。hub 复用 APNs 的提问和完成决策、屏蔽规则与设备偏好，在 `c.has("ws_notify")` 时才发给该连接。即使没有 APNs token 或密钥也能收到；旧 iOS 不声明就不会收到新事件。

`interaction` 表示需要处理的交互，`done` 表示 Agent 完成，`assistant` 表示助手完成，`alert` 表示扩展经 `POST /api/v1/notify`（或脚本的 `personad notify`，`script-cli.md`）发的提醒（不认识的 kind 客户端忽略）。偏好按鉴权设备 ID 读取 `/api/v1/devices/{id}/prefs`：交互看 `input`，完成看原有完成策略；机器与空间屏蔽沿用已有判断。完成事件由 hub 决策，Android 不应根据 sessions 状态变化再次生成一条。原始 `interaction` 与 `interaction_resolved` 事件照旧发送，用于请求卡和撤回通知，不能把它们都当成新的提醒。

事件不持久回放，也不证明通知实际展示；断线后客户端仍须拉交互和会话核对。通知可靠性、锁屏展示和耗电未实测。

### 客户端能力与心跳

客户端看到服务端支持 `client_hello` 后可发送：

```json
{"type":"client_hello","device_id":"<设备 id>","platform":"android","version":"<版本>","build":1,"os":{"name":"Android","version":"<系统版本>","sdk":36},"features":["endpoints"],"locale":"en","heartbeat":{"interval":240,"pong_timeout":15}}
```

`locale` 是解析后的界面语言（`zh-Hans` / `en` / `ja`，[i18n.md](i18n.md)），不是「跟随系统」。hub 用它出这条连接上的 `notify`、`error` 等文案；`full` 角色的连接报的值同时记成**用户语言**（Postgres `settings.user_locale`），助手回答、injected 消息、任务通知、机器人回复、没有自己 `locale` 的设备的推送都用它。从没有客户端报过时是 `zh-Hans`。省略 `locale` 不改这两样；用户改了语言（或跟随系统时系统语言变了）立刻再发一次 `client_hello`（带上原来的其余字段，省略 `features` 会清空能力集合）。

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
  已知问题（#2425）：App 手上已有的历史留旧副本，看过、变离线不会更新到列表；网页已改成换用回答里的副本，App 待改。
- 兼容：老 App 不带 `features`，照旧收整表（一模一样的不发）；老 hub 不认 `features`，照旧发整表，App 的 `hello.features` 里没有 `sessions_delta`，按整表处理。
  mock（`scripts/mock/mock_hub.py`）同样支持，`MOCK_WHOLE_LISTS=1` 当老 hub。
- 回放（真实 hub 录的 19 分钟）：会话推送每分钟 2605 KB → 79 KB，连上时的快照 602 KB → 227 KB（`docs/testing.md`「推送回放」）。
- 网页同样用它（`web/src/lib/sessions-delta.ts`，与 `SessionPatch` 同一套规则）：`wsQuery` 每次连接都带 `features=sessions_delta`，`store.ts` 按 `upsert` / `remove` 合并，`refresh()` 在连着增量 hub 时只从 `GET /api/v1/sessions` 取 Codex 历史，已有的历史换成回答里的副本（推送不带历史，看过、变离线只能从这里更新，#2425）。此前网页不带 `features`，实测每个连接每秒收约 3 MB 整表、把后面的小事件拖慢（#1707）。

### 实时状态（`session_live`，2026-11-05）

Claude Code 工作时屏幕底部的状态行（`✻ Frosting… (16m 15s · ↓ 22.8k tokens · thinking with xhigh effort)`）有动词、这一轮的用时、输出 token、是否在思考，transcript 里没有。
订阅了会话的网页要显示它，node 从屏幕上读出来推过去。

- 类型 `proto.SessionLive`（`server/internal/proto/live.go`）：`verb`（不带省略号）、`elapsed_s`、`tokens_down`（`↓` 的输出 token；`↑` 是输入，不算）、`thinking`（`thinking…` 段）、
  `thought_s`（`thought for 3s`）、`at`（node 读到的时间，RFC 3339）；没有的字段省略。
- node → hub 事件 `session.live {session_id, live}`，`live` 是 `SessionLive` 或 `null`。
  - 只在这个会话被 watch（有客户端订阅）、是 herdr 里的 Claude（`agent == "claude"`，宿主 herdr；Codex、herdr-core、Orca、runtime、无头助手一律不读）、宿主报 `working` 时读：
    约每秒一次 `pane.read --source visible --lines 60`，单次超时 3 秒，在 watch 自己的 goroutine 里，不拖慢 250 ms 一轮的 transcript 推送（`node/claudestatus.go`）。
  - 解析：从输入框的上边线（屏幕上倒数第二条横线）往上找，跳过空行和缩进的行（提示、待办、右对齐的 `/goal`），第一行顶格的行是状态行或者就没有状态行；
    所以正文、工具输出、用户消息里引用的状态行不会被当成现在的。spinner 字符不限（`· ✢ ✳ ✶ ✻ ✽`…），时长 `2s` / `1m 5s` / `1h 2m 3s`，token 单位 k / M / 无，括号里的段可缺、顺序可变，
    `esc to interrupt` 之类的提示忽略。
  - 和上一条不同才发（不比较 `at`）。停止工作、状态行没了、watch 结束（`session.unwatch`）时补一条 `live: null`，之前发过非空的才补。读屏失败时不发也不记日志，保留上一条。
- hub → 客户端 `{"type":"live","machine_id","session_id","live"}`：只发给订阅了这个会话、并且声明了 `session_live` 的连接——WebSocket URL 的 `?features=`（网页的 `wsQuery`）
  或 `client_hello.features` 都算。hub 不缓存、不补发：新订阅的客户端等下一次变化（工作中约一秒内就有）。旧 App 不声明，收不到，其余事件不变。
- `hello.features` 目前不列 `session_live`：客户端照样声明就行，老 hub 不认识这个事件也就不发。

### 新建会话

开 herdr 标签页、跑启动器、等 Claude 的输入框出来要几秒到一分钟（`~/.zshrc` 慢、信任目录对话框），以前 App 的「开始对话」按钮一直转到 Claude 就绪，
hub 还要先等小模型起完标签名（约 2 秒）。现在 `POST /api/v1/sessions` 带 `async`：

- node 只做便宜的检查（启动器名、目录存在、权限模式），记下会话（`sessionState`，对话页取历史回空列表、订阅等 transcript 出现），马上回会话；
  开标签页、跑启动器、等就绪、发 `prompt` 在后台做（最多 4 分半：shell 跑起 claude 最多等 3 分钟，claude 起来后等输入框最多 1 分钟，#1396）。
- 启动期间会话列表里有它：`status: idle`、`starting: true`（不算一轮，不计时、不推送「完成」）。启动失败就变成 `offline` 加 `start_error`，留 10 分钟。
- 启动期间发来的消息（`session.send`）在 node 上等它就绪再按顺序打进去；hub 给这种发送和发给没在运行、要先恢复的会话的 5 分钟（平时 90 秒）。启动失败时这些发送拿到同一个错误，App 的气泡变成「重发」。
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

### 回退到某条消息（#2436）

`POST /api/v1/sessions/{mid}/{sid}/rewind`，**只有完整权限的设备**能调（会把文件放回去、删掉之后的对话；手表那种只能回答的设备、扩展的 token 都是 403，
`testdata/route_scopes.txt` 里是 `-`）。body 只认这几个字段，别的不会交给 node：

```json
{"prompt": "<那条消息，和对话记录里一样>", "occurrence": 2, "prompts_after": 3, "restore": "conversation"}
```

- `prompt`：要回到的那条用户消息；`occurrence`：从最后往前数，是第几条**一样的话**（1 = 最后一条）；`prompts_after`（可不带）：它后面还有几条用户消息；
  `restore`：`conversation`（只回退对话）或 `code_and_conversation`（文件也放回去）。Claude Code 的列表和 App 的对话都从末尾数，没加载的更早页不影响。
- node 的 `session.rewind` 在会话的终端里打 `/rewind`、按 ↑ 走到那条、选方式、回车，**每一步先读屏**（`server/internal/node/rewind.go`、`screen/rewind.go`）：
  列表里那一行的字、位置（`prompts_after`）、确认页上引用的整条消息、选项的名字（按名字选不按序号：这条之后没有代码改动时只有「Restore conversation」「Summarize…」「Never mind」）
  任何一步和预期不一样，按 Esc 退出面板、什么都不改，回 409。`code_and_conversation` 遇到「之后没有代码改动」就只回退对话，结果里 `code_restored: false`。
- 成功：`{prompt, restore, code_restored, input_cleared}`。Claude 会把那条消息放回自己的输入框；node 把它清掉（一行用 `ctrl+k ctrl+u`，多行用 `ctrl+c`），
  `input_cleared: true` 时 App 把它放进自己的输入框，false（清不掉，比如输入框里是别的字）就只提示一句。
- 拒绝（409，`code` 加说明，什么都没改）：`dialog_open`、`input_draft`（同「发消息」）、`rewind_busy`（在工作或等回答）、`rewind_unsupported`（不是终端里的 Claude 会话）、
  `rewind_nothing`（Claude 里没有可回退的消息）、`rewind_mismatch`（列表里没找到那条、位置对不上、确认页上不是它）、`rewind_failed`（屏幕不是下一步需要的样子，或确认之后面板没关）。
  旧 node 回 409 `node_outdated`。确认之后面板没关时 node 不再按任何键（回退可能已经发生，Esc 也撤不回）。
- **Claude Code 不删对话记录里被回退的那部分**（2.1.318 实测）：文件是棵树（`parentUuid`），回退后的下一条消息挂在目标前一条下面，被回退的几轮还在文件里。
  node 读 transcript 时**跟着没被放弃的那条分支走**（`transcript/branch.go`）：同一个 `parentUuid` 下有两条用户自己的消息，较早那条和从它长出来的所有行是被放弃的分支，不出 item
  （item 的 `seq` 还是文件里的行号，被放弃的行成了空档）。别的分叉不算（并行工具结果、挂在回复旁边的 attachment、压缩的 `compact_boundary`：它 `parentUuid` 为 null、旧链在 `logicalParentUuid`）。
  node 自己回退成功后马上标出这一段（`File.Rewound`），不等下一条消息。客户端怎么知道：`history`（见「items 的连续性」）末尾多一个 `.r<被放弃的分支数>`，变了就丢掉本地内容重新取——
  和文件被改写同一个机制，所以网页、iOS、Android 都不用改（iOS 的 `ChatStore.apply`、Android 的 `reducer.history`、网页的 `useTranscript` 都已经这样处理），
  hub 的助手快照、通知、知识库归档读的也是 node 的 items。令牌只由文件内容决定，node 重启后不变。

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

### 文件预览

文件页和各种预览（表格、Office、媒体、对比）共用的底座，三端一样（#1292）：

- **类型**：node 按固定扩展名表定 `mime`（docx / xlsx / pptx / ipynb / heic / m4a 等，Go 自带的表随机器而变），表里没有再查系统表，再读开头 512 字节嗅探（HEIC、MP4 / MOV、PDF……）。`.key` 开头不是 zip 就当文本（PEM 密钥），是 zip 才是 Keynote。
- **取内容**：先 `GET /file?stream=1`。文本照旧分页给 `text`；二进制只给 `size`、`mime`、`revision`，再按需从 `/file/download` 取：媒体直接把 URL 给 `<video>` / `<audio>`（Range 拖动），PDF / Office / 表格整份取回来交给渲染库，大文件不再整份 base64 进 JSON。`revision` 和下载的 ETag 是同一个值。
- **内联和隔离**：hub 和网页同源，所以 `inline=1` 只放行图片、音视频、PDF；SVG 虽然内联，也带 `Content-Security-Policy: sandbox`，单独打开时脚本不能以网页的身份运行。HTML 不内联，预览时取文本放进沙盒 iframe（M5）。
- **缩略图**：图片（PNG / JPEG / GIF / WebP / BMP / TIFF）由 node 自己解码缩放，SVG 用 oksvg 只画形状；其余交给系统：Mac 上 Quick Look（`qlmanage`，PDF、Office、iWork、HEIC、视频），别的系统装了 `pdftoppm` 给 PDF、装了 `ffmpeg` 给视频。系统工具一次只跑一个，单次 12 秒超时；超时一次（锁屏的 Mac 上 Quick Look 会卡住）之后 5 分钟内不再调用，直接回 `no_thumbnail`，免得一屏文件列表每个都等。node 内存里留最近 128 张。

### 发送文件

App 对话页输入框的「+」›「文件」（Agents 和「对话」tab 的助手都有）：选的文件传到**会话所在那台电脑**的临时目录（node 的
`os.TempDir()`：Linux 一般 `/tmp`，macOS 是 `$TMPDIR`，ssh 进来的 node 没有这个变量时也是 `/tmp`），把绝对路径插到输入框的光标处，
**不自动发送**。助手跑在它所在的机器上（默认 hub 机器），文件就传到那台。图片照旧走图片附件；图库里的视频见下面「发送视频」。

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
- `?dest=terminal`（网页终端的粘贴 / 拖放，`docs/terminal.md` 第十八节）：node 不放系统临时目录，存到自己的 `uploads/terminal/<日期>/<随机>-<文件名>`
  （0700 / 0600，7 天后在 node 启动时清掉），上限 50 MB（`proto.MaxTerminalUploadBytes`，超过同样回 413 `too_large`，带 `limit`）。别的 `dest` 回 400；
  旧 hub / 旧 node 忽略这个参数，照旧放临时目录。

### 发送视频

输入框「+」›「照片」能选图片也能选视频（#981；Agents、助手的对话和新对话的输入框都一样）。**视频当文件发**，不转成图片：
走上面「发送文件」同一个接口，传到会话所在那台电脑的临时目录（助手的就是助手所在的机器）。和「文件」不同的是路径不插进输入框，
视频在输入框上方显示成一张卡片（读取 → 必要时压缩 → 上传，进度条，✕ 取消；传完显示大小和时长，✕ 拿掉），随下一条消息发出。

- 消息里的样子：打的字后面空一行，每个视频一个标签
  `<jarvis-file kind="video" path="/tmp/IMG_0001-20261103-150102.MOV" name="IMG_0001.MOV" size="25270000" duration="12.4"/>`
  （`path` 是那台电脑上的路径，`name` 是手机上的原名，`size` 字节，`duration` 秒）。Agent 读路径；App 解析标签画成文件卡片
  （文件名、大小、时长），不显示标签本身，路径也不再出文件 chip；手表显示〔视频〕。没打字只发视频时正文是「请看视频」。
- 上传中不能发送（发送键灰着；BeautifulUI 的输入框点了会提示「视频还在上传，传完再发送」，字留在框里）。一次最多 4 个，和照片共用选择器的 4 个名额。
- **原样传还是转码**：不超过 200 MB（hub 的上限）**原样传**：不等转码、不损画质，iPhone 拍的 HEVC / H.264 MOV 在 Mac 和 ffmpeg 上都能读。
  选择器用 `.current` 编码，系统不先替我们转一遍。超过 200 MB 才用 AVAssetExportSession 转成 **H.264 MP4**（moov 在前），
  按估算挑 1080p（约 76 MB/分钟）→ 720p（约 54）→ 540p（约 35）里估算放得进 180 MB（留一成余量）的最高一档，转完再核对一次大小，
  超了降一档。iPhone 原片大约 1080p30 HEVC 60 MB/分钟、4K30 170 MB/分钟，所以 1080p 约 3 分钟、4K 约 1 分钟以内原样传。
- **上限**：压缩后也要不超过 200 MB，540p 大约能放 **5 分钟**（估算按码率算，和画面内容关系不大）。更长的不传，提示
  「「IMG_0002.MOV」有 12:34，压缩到 540p 也超过 200 MB，发不了：先在「照片」里剪短再发」。还在 iCloud 里下不下来的提示「有视频读不出来」。
- 点卡片用系统播放器（iOS 是全屏的 AVPlayerViewController，Mac 是 AVKit 的播放器）：这台手机发的直接放本地副本（`Caches/attached-videos`，
  按最近播放保留 1 GB，算在「设置 › 存储空间」的「其他缓存」里）；别的设备发的先从那台电脑下载（`/file/download`，卡片上显示进度，再点一下停止），
  下完也留一份。电脑上的临时文件被清掉以后就放不了了（提示「放不了」）。

### items 的连续性

每批 items（WebSocket `items` 事件、HTTP 的 `/items` 响应、node 的 `session.items`）都说明自己在 transcript 里的位置：

- `history`：这份 transcript 的身份（inode + 第一行的 crc32 + 被改写的次数，Claude 会话回退过时再加 `.r<被放弃的分支数>`，见「回退到某条消息」）。变了说明文件被替换 / 截短重写 / 有轮次被回退掉，App 丢掉本地内容重新取。
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
- `fold.images` 是可选的数组：这段工作看过的图片（`transcript.ViewedImages`），规则见下面「这一轮看过的图片」；没有图片时省略。
  旧客户端忽略这个字段。
- 不折叠：用户消息、回复（text）、通知、recap；结果不在这一页的 tool_use（还在跑）和 tool_use 不在这一页的 tool_result
  （App 要把它们和后来 / 之前的另一半配对）；不到 3 条且不到 4 KB 的短段（展开还要一次往返，不值）。
- App 展开工作行时用 `from_seq=<fold.seq>&through_seq=<fold.through_seq>` 取回这一段（一页约 1000 条，`more` 时从最后一条的 seq + 1 接着取），
  合并进来并去掉 fold。合并一条 fold 时，本地在它范围内（按 seq + 块序号）的工具 / 思考 item 被它替换。
- `after_seq` 的补发和实时推送从不折叠：它们接着 App 已有的内容，里面可能有 App 正等着结果的工具调用。
- 1412 条 / 1.08 MB 的那种页，折叠后是十几条、几 KB（`TestFoldedFirstPageOfALongSession`：1417 条 880 KB → 13 条 1.9 KB）。

### 这一轮看过的图片

App 和网页在一轮的回复下面放一条「这一轮看过的图片」（#1543、#1540）。图片来自这一轮工作里的工具调用，两种来源按同一套规则汇总：
没折叠的工作看 items 本身，折叠的看 `fold.images`（工作行展开后 fold 被真实 items 替换，汇总结果不变）。

- 算哪些：结果里带的图（`kind: image` 且有 `tool_use_id` 的 item：Read 读的图片、computer_state 的截图、browser_look 的截图、
  其他 MCP 工具返回的图片）；Read 读了图片文件（png / jpg / jpeg / gif / webp / heic / heif / bmp / tif / tiff）但结果里没带图的
  （图太大没留下，或 Codex 的 view_image，transcript 里映射成 Read）也算一条，只有路径。用户这一轮发的图不算（在用户气泡里），
  失败的 Read、不是图片的文件、还没有结果的调用不算。
- 顺序和去重：按第一次出现的顺序；同一路径只出一次，没有路径的按 mime + 大小 + 宽高认（同一个屏幕没变又看一次只算一张），
  都没有时按 image item。
- `fold.images` 每项：

  | 字段 | 说明 |
  | --- | --- |
  | `item_id` | image item 的 id，图片用 `GET /api/v1/sessions/{mid}/{sid}/items/{item_id}/image` 取；只有路径的没有这一项 |
  | `tool_use_id`、`tool` | 看图的那次调用和它的工具名（原样，如 `Read`、`mcp__jarvis__computer_state`） |
  | `source` | `read`（Read）、`screenshot`（MCP 的 `computer_*`）、`browser`（MCP 的 `browser_*`）、`mcp`（其他 MCP 工具）、`tool`（其他） |
  | `path` | Read 读的文件（`file_path`）；缩略图用 `GET /api/v1/machines/{mid}/file/thumb`，大图用 `file/download` |
  | `machine` | `screenshot` / `browser`：调用里写的机器（id 或名字），显示成「截图 · <机器名>」 |
  | `mime`、`size`、`width`、`height` | 图片的类型、字节数（约数：base64 长度 × 3 / 4）、像素宽高，和懒加载的 `image` 一样，用来在图到之前占好位置 |

  例如：`"images":[{"item_id":"entry_tool#1","tool_use_id":"call_a","tool":"Read","source":"read","path":"/work/design.png","mime":"image/png","size":48213,"width":1280,"height":800},{"tool_use_id":"call_b","tool":"Read","source":"read","path":"/work/banner.jpg"}]`。

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
- 窗格地址（`features` 有 `terminal_panes`，`docs/terminal.md` 第十九节）：没有 Agent 的窗格没有会话 id，`terminal_subscribe` / `terminal_input` / `terminal_unsubscribe` 的 `session_id`
  可以写 `pane:<窗格 id>`（`/machines/{mid}/panes` 给出的 `terminal`）；`GET …/sessions/{mid}/pane:…/screen`（轮询退路）和 `screen/before` 也认。**只有 `full` 设备能用**：`answer` 设备订阅（连只看也算）
  收到 `error: forbidden…`，HTTP 读屏 403。别的 `session.*` 不认这个地址。
- 手机接管（`features` 有 `terminal_control`，`docs/terminal.md` 第十一节）：订阅带 `control` 时网格是手机的尺寸，帧格式不变；
  键和新尺寸走 `terminal_input`；离开（退订 / 断开）后窗格回到电脑上的尺寸，还有别人在看就改回镜像（新 epoch）。


## 文件自动刷新（#2439）

网页的文件面板和 Git 面板开着时自动刷新（`web/src/lib/fs-live.ts`、`server/internal/hub/fswatch.go`、`server/internal/node/fswatch.go`）：

- 页面发 `fs_watch`（上面的消息表），**每个会话目录一份、整份替换**；同一页面里文件面板和 Git 面板各自的需求在页面里合并，页面隐藏时发一份空的，回来时再发并刷新一遍。
- hub 把所有页面的集合并起来，**整份替换**地调 node 的 `fs.watch`；node 重连后 hub 重发一次。页面连接断开、面板关闭、换页都会释放，最后一个页面走了 node 就停掉看的循环（不留 goroutine）。只有完全权限的设备能发 `fs_watch`。
- node **轮询而不是 inotify / fsnotify**：`go.mod` 里没有 fsnotify，Linux 的 inotify 不能递归，各系统的行为也不同；只看页面打开着的那几个文件夹（每 2 秒一次 `ReadDir`，签名是名字 + 类型 + 大小 + 修改时间），Git 目录每 5 秒一次 `git status --porcelain=v2` 加上列出的改动文件的大小和时间（已改的文件再改一遍，`status` 文本不变，但大小和时间变了）。变了才推 `fs.changed`，没变不发任何东西。
- 网页收到 `fs_changed` 后等 200 ms（再来就重新计时）合并成一次刷新：只重新读变了的文件夹，Git 状态和打开着的 diff 原位刷新，不闪加载动画。
- node 先看 `fs.watch` 里的每个路径都过 `pathguard`（和 `fs.list` 同一道），被拒的路径只是读不出来、签名是 `err`，不会读它的内容。

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
- `node.info` 的 `herdr_running` / `herdr_running_known`：经典 herdr 的 server 现在答不答话，以及 node 能不能判断（用 herdr-core
  的机器没有那个 socket，两个都不带）。老 node 两个都不带，hub 据此区分「没在跑」和「不清楚」（`docs/engines.md`）。
- `node.info` 的 `builtin_programs`：personad 里内置的扩展程序（`["cua", "ego", "imessage"]`，#1830 第 5、6 步；imessage 的方法名照旧是 `connector.*`）。报了的 node，hub 经 `plugin.node.call {id, method, params}` 问它们；
  老 node 不带，hub 照旧用 `computer.*`、`browser.*`（新 node 也照答）。见 `docs/extensions.md`「内置的 node 程序」。
- `herdr.server.start`（hub → node，无参数，回 `{running}`）：用户点了 Agents 列表的「启动」。不受 `herdr_autostart` 约束，
  起不来时按 `engine_missing` 那一套回原因。老 node 没有这个方法，hub 回 502 而不是说 herdr 起不来。
- `usage.windows`（hub → node，无参数，回 `{enabled, collected_at, accounts: [UsageAccount]}`，#2432）：node 读本机 Codex 的 rollout 和 Claude 状态栏落下的文件得到的订阅额度窗口，
  只读本机文件、不碰凭据。`node.configure` 的 `usage_windows`（hub.json `machines[].usage_windows`）为 false 时回 `{enabled: false, accounts: []}`。老 node 没有这个方法，hub 当它没有数据（`docs/usage-windows.md`）。
- 并列时没有窗格的操作（新建、继续会话、改名）固定走经典 herdr（node 的 `defaultHost` / `defaultHostKind`）；
  每个窗格的操作（读屏、发送、按键、关闭）按窗格 id 的 host kind 路由，herdr-core 的窗格只受 herdr-core 能力限制。
- **`paseo`（显示名「Paseo」，`docs/paseo.md`）不是终端宿主**：node 不注册宿主，自己运行 Paseo 的桥（`node/paseobridge.go`）。
  桥是 runtime id 为 `paseo` 的 runtime（下面「Runtime ↔ node」），它的会话就是这台机器的会话。
  - **新建会话**：都交给它。表单里选的是登录 shell 的启动器时，带上 `shell_launcher: true`。
  - **继续未运行的 Claude 会话**：用 `session.start {resume}` 交给它接手，会话 id 不变；接手之后，消息照常用 `session.send` 发过去。
  - **桥没连上时**，新建和继续都回错误 `engine_missing`，什么都不发。
  - **检测**：`engines` 里的 Paseo 在两种情况下算可用。一是桥连着，这时 `version` 是 Paseo 的版本。二是 Paseo home 里 `paseo.pid` 写的地址有回应；
    这时如果桥起不来，`note` 写原因。

## hub ↔ node RPC

> 2026-11-04 起连 node 的是管理者（助手的进程，[machines.md](machines.md)「谁连电脑」）：下面的「hub」在线上是管理者，hub 的调用和 node 的事件经总线转（「总线」下的「电脑（devices.*）」），
> node 看到的协议一个字不变。

`ssh <host> ~/.persona/bin/personad node --stdio`（hub 机器本机直接起子进程，现在是管理者的子进程）。stdin/stdout 每行一个 JSON。
双向都能发请求：`{"id":"h12","method":"…","params":{}}` → `{"id":"h12","result":{}}` / `{"id":"h12","error":{"message":"…"}}`；
单向事件 `{"event":"…","params":{}}`。hub 发的 id 以 `h` 开头，node 发的以 `n` 开头。stderr 是 node 日志。

hub → node 请求：

| method | params → result |
| --- | --- |
| `node.info` | → `{hostname, os, arch, home, herdr_version, claude_version, version, system, engine, engines}`（`system` 见 Machine，node 启动时读一次；`engine` 是 node 现在用的引擎，`engines` 是它找到的，见「引擎」；旧 node 没有这两个）。连上时 `claude_version` 为空（`claude --version` 那次没跑成），hub 隔 1、2、5、10、30 分钟、之后每小时再问一次，拿到就更新机器并推给 App，连接断了就停（#542） |
| `engine.use` | `{engine}` → `{engine}`：换成这个引擎（不认识的报错）；和现在一样时什么都不做，否则换掉宿主、丢掉旧引擎的 Agent（切换前开始、切换后才回来的刷新结果作废）、结束所有终端流（每个推 `terminal.end`，error 说引擎换了）、换事件流，并推一次 `sessions.changed`。多个 `engine.use` 依次执行 |
| `launchers.list` | → `{launchers}` |
| `launchers.models` | `{name, agent_type?, refresh?}` → `{agent, models, locked?}`：同 REST 的 `…/launchers/{name}/models`；`agent_type` 是 hub 配置里这个启动器的类型（不给就按 node 自己认出的） |
| `skills.list` | → `{skills: [{name, description, source}]}`：这台机器上助手拿到的 skill（`~/.persona/skills/` 里用户的，加上内置的；`docs/hub-update.md`）。老 node 没有它时 hub 当作装了 lark-connector（老版本把它编在二进制里） |
| `dirs.list` | `{path}` → 同 REST |
| `workspaces.list` | `{}` → `{workspaces:[Workspace],errors?:[{source,error}]}`；Workspace 同「工作区看板」，node 省略 machine_id，id/source_id 为宿主本地 ID。包含没有运行 Agent 的空间；各来源分别报告错误，全部失败返回 RPC error，不能伪装成空成功 |
| `workspace.resolve` | `{path}` → `Workspace`；校验真实目录、解析符号链接，成功 source 为 `directory`、id/source_id/cwd 为物理路径、available 为 true；支持本机绝对路径或 `~/`，拒绝不存在、非目录及受保护的 iCloud 路径，不执行宿主、Agent 或 Git 操作 |
| `sessions.list` | → `{sessions}` |
| `session.start` | `{cwd, launcher, agent_type?, prompt?, resume_session_id?, workspace_id?, permission_mode?, effort?, tab_name?, async?}` → `{session}`；`async` 立刻回、后台启动（`node/starting.go`）；Codex 启动器同步、必须带 `prompt`（`node/codex_start.go`） |
| `session.send` | `{session_id, text, images?, client_msg_id?}`（`client_msg_id` 见「待发消息的对应」） |
| `session.echoes` | `{echoes: [{session_id, client_msg_id, text, images, sent_at, after_seq, line?}]}` → `{ok}`：hub 交回这台机器一天内记下的发送（node 每次连上时，在 `sessions.list` 之前）；node 已经知道的跳过。旧 node 回 unknown method，hub 不管 |
| `session.keys` | `{session_id, keys, text}` |
| `session.rewind` | `{session_id, prompt, occurrence, prompts_after?, restore}` → `{prompt, restore, code_restored, input_cleared}`：在终端里驱动 Claude Code 的 `/rewind`（「回退到某条消息」）；拒绝带 `code` |
| `session.interrupt` | `{session_id}` |
| `session.screen` | `{session_id, lines, source, format?}` → `{text, status}`（`format: "ansi"` 带颜色） |
| `panes.list` | `{}` → `{hosts:[…]}`，同 REST `/machines/{mid}/panes`：node 用最近一次宿主快照（超过 3 秒才再拍一次）回空间 / 标签页 / 全部窗格，不另起轮询 |
| `panes.new` | `{workspace_id, kind?}` → `{pane, terminal, tab_id}`：宿主的 `CreateSlot` 开一个 shell 标签页（目录取空间自己的）；空间不在、宿主不会开标签页时报错 |
| `panes.close` / `panes.split` / `panes.agent` / `panes.git` / `tabs.close` / `tabs.rename` | 节点菜单的动作，参数同上面的 REST；窗格 / 标签页必须是宿主列出的（没有就先拍一次快照再认），目录取窗格自己的；Agent 的窗格关闭走 `session.close`（先请它退出）；`panes.agent` 复用新建 Agent 的整条启动流程（同一个启动器命令、等会话就绪、绑定），失败时不关这个窗格 |
| `terminal.watch` / `terminal.unwatch` | `{session_id, control?: {cols, rows, takeover}}`；watch → `{cols, rows, control}`，之后 node 推 `terminal.frame`；已经在推时 watch 会重开一条流（新 epoch、从整帧开始）。herdr 没有 observe 时报 `unsupported: …`。带 `control` 时 node 用 `herdr terminal session control` 接管（回包 `control: true`；旧 node 没有这个字段），槽位被占着又没 `takeover` 时报错 `code: attach_busy` |
| `git.pull` | `{cwd}` → `{status, updated}`，同 REST 的安全拉取 |
| `fs.list` / `git.status` / `git.diff` / `git.show` / `git.log` | 同 REST（参数同名） |
| `fs.watch` | #2439：`{dirs: [{path, cwd}], git: [cwd]}` → `{ok}`，**整份替换** node 要看的文件夹和 Git 目录（最多 32 个文件夹、8 个 Git 目录，空集合就停掉看的循环）；旧 node 报 unknown method，hub 只记一行日志，页面照旧手动刷新 |
| `file.read` | `{path, cwd, offset}` → 同 REST 的 `file`（旧 node 不认 `offset`，回包也没有 `offset` / `next`） |
| `fs.stat` | `{paths, cwd}` → `{stats}`，同 REST 的 `stat`（旧 node 报 unknown method，手机就不链接任何路径） |
| `session.items` | `{session_id, before_seq, limit, turns, after_seq, lazy_images, fold_work, from_seq, through_seq}` → `{items, has_more, more_after, history, to_seq, from_seq}`（带 `through_seq` 时是折叠的那几行：`{items, more}`）（`from_seq` 只在 `after_seq` 请求里有；不带 `lazy_images: true` 时 node 把图片数据读回来，兼容旧 hub） |
| `session.image` | `{session_id, item_id}` → `Image{mime, data}`（node 的 item 不常驻图片数据，按记下的行偏移重读那一行） |
| `session.item` | `{session_id, item_id}` → `Item`（全文，不带图片） |
| `session.watch` / `session.unwatch` | `{session_id, since_seq?}`；watch 后 node 每 250ms 读一次新行，推 `session.items`（带 `history` / `from_seq` / `to_seq`）；有 `since_seq` 时先按 500 条一页补发其后的全部内容 |
| `session.close` | `{session_id}` |
| `session.fork` | `{session_id, at_item_id}` → `{session_id}`：见 HTTP 的 `/fork`；旧 node 报 unknown method |
| `session.retire` | `{session_id}` → `{closed}`：做完的任务的 Agent 停着且以后能恢复时关掉它的窗格（同 `session.close`），没在运行时 `closed: false`；在工作、等用户、恢复不了的回 `code: not_retirable`；旧 node 报 unknown method（助手当作拒绝）。见 `docs/todos.md`「做完的任务关掉 Agent」 |
| `file.upload.begin` | `{name, size}` → `{upload_id, chunk_bytes}`（超过上限报 `too_large`） |
| `file.upload.chunk` | `{upload_id, offset, data}`（data 是 base64，offset 必须接着已收到的）→ `{received}`；出错整个上传作废 |
| `file.upload.end` / `file.upload.abort` | `{upload_id}`；end 核对大小、改名 → `{path, name, size}` |
| `interaction.resolve` | `{id, resolution}` |
| `port.probe` | `{port}` → `{port, listening, http, title?}`：GET / 一次（Host 是 `localhost:端口`） |
| `computer.status` | `{}` → `Computer`（`enabled` 由 hub 填）：探测本机 cua-driver（路径、版本、daemon、`permissions status --json`）和桌面会话（`ioreg` 的 IOConsoleUsers：active / locked / away）。只有插件 `cua` 对这台打开时 hub 才问（[computer-use.md](computer-use.md)） |
| `computer.call` | `{session, tool, args}` → `{output?, text?, image?}`：`cua-driver call --screenshot-out-file … <tool>`，参数走 stdin，`session` 填进参数（element_token 跨调用有效）；截图另放 `image`（`{mime, data, width, height}`，比 PNG 小时转 JPEG）。只认读取和 `run_script` / `run_steps`；daemon 没在跑时先 `open -g -b com.trycua.driver`。没装：错误码 `not_installed` |
| `computer.install` / `computer.grant` / `computer.cancel` | `{}` → `Computer`：一个后台任务（一次一个），状态随 `computer.status` 的 `job` 回来。install：三个安装脚本先下载到临时目录、检查后从磁盘跑 `install.sh --no-modify-path`（`CUA_DRIVER_RS_TELEMETRY_ENABLED=0`），装完 `telemetry disable`，接着自动 grant；grant：`cua-driver permissions grant`（Mac 上弹系统窗口）；cancel 只停安装（[computer-use.md](computer-use.md)「一键安装」） |
| `browser.status` / `browser.install` / `browser.open` / `browser.cancel` | `{}` → `Browser`：ego lite 装没装、首次设置、统计、只读探测；安装是下载官方 DMG、校验签名（Team `JGQLC6YQYJ`）和公证后 ditto 进「应用程序」、保留 quarantine；打开是在那台 Mac 上 `open` 它 |
| `browser.call` | `{session, script?, look?, full?, screenshot?, timeout_ms?}` → `{output, image?}`：`ego-browser nodejs`（脚本走 stdin）在名为 `session` 的任务空间里跑助手的脚本（`task`、`page` 现成），或 look 时拍快照；最后一行结果用 `@@persona-browser@@` 标出（不能用 NUL：ego 会丢掉整段输出），截图另放 `image` |
| `plugin.node.have` / `plugin.node.put.begin` / `.chunk` / `.commit` | 插件在这台电脑上的包（`docs/extensions.md`「在电脑上运行的程序」）：`{id, version, digest}` → `{have}`；`begin {id, version, digest, size}` → `{upload_id}`，`chunk {upload_id, offset, data}`（按顺序，1 MB），`commit {upload_id}`：校验 sha256、解包到 `~/.persona/plugins/<id>/<version>/` |
| `plugin.node.start` / `plugin.node.stop` | `start {id, name, version, command, args?, env?, rpc, http?, keep, lease_seconds, init}` → `{id, version, state, error?}`：起它（和正在跑的完全一样就不动，只续租约；新版本 30 秒内没起来回退到上一版并报错）；`init` 是程序的 `initialize`（配置、发给电脑的密钥，不落盘）。`stop {id}` |
| `plugin.node.sync` / `plugin.node.renew` / `plugin.node.status` | `sync {programs: [{id, version, digest}], installed: [id]}` → `{programs: [状态]}`：不在 `programs` 里的停掉、删包，不在 `installed` 里的连数据删；`renew {ids, lease_seconds}`；`status` → `{programs}` |
| `plugin.node.dial` | `{id, plugin}` → `{}`：连上这个插件程序的 HTTP socket，开一条流 `id`（之后同 `port.dial` 用 `stream.*` 事件传字节）；程序没在跑或不开 HTTP 回 `code: not_running` |
| `plugin.node.call` | `{id, method, params?}` → 程序的回答：插件协议的方法（hub 上的程序经 `nodes.call` 调）；程序没在跑回 `code: not_running` |
| `port.dial` | `{id, port}` → `{}`：连上本机这个端口（扫描时它绑的地址，否则 127.0.0.1 再 ::1，只连本机），开一条流 `id`；没在听时报错 `code: port_gone`。之后两边用下面的 `stream.*` 事件传字节 |
| 事件 `plugin.node.state` / `plugin.node.event` | node → hub：`{id, version, state, error?}`（起来了、租约到期停了），`{id, event, params}`（程序发的事件，hub 转给 hub 上的程序） |
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

node → hub 事件：`fs.changed {dirs?, git?}`（#2439：`fs.watch` 里的文件夹的列表内容变了（名字、大小、时间）、或 Git 目录的 `git status` 变了，hub 转成给相关页面的 `fs_changed`）、`terminal.frame {session_id, epoch, seq, full, cols, rows, data}`（相隔不到 30 ms 的帧由 node 合成一帧：diff 的字节接起来，整帧丢掉它前面的 diff）、`terminal.end {session_id, error, control?, taken?}`（流停了且起不来：会话不在跑、herdr 一直拒绝；`control` 时是接管结束了：`taken` 被别人抢走，否则租约到期）、
`session.items {session_id, items, history, from_seq, to_seq}`、`session.live {session_id, live}`（watch 中、工作中的 herdr Claude 的状态行，变了才发，结束补 `null`，见「实时状态」）、`session.echo {session_id, client_msg_id, text, images, sent_at, after_seq, line?}`（node 记下一次发送、认到它的那一行时各推一次，hub 存进 `sent_messages.echo`；已认到的不会被晚到的「还在等」盖掉）、`sessions.changed {sessions}`、
`sessions.revs {revs}`（每秒检查一次，只发变了的 rev；列表本身变了时 rev 跟着 `sessions.changed` 走）、
`usage {buckets:[{minute, model, in, cache_write, cache_read, out, req}], full, load, cpus}`（每 10 秒，只发变了的分钟、每分钟的全量值，
连上后第一次 `full: true` 带最近 26 小时全部；`docs/live-activity.md`「用量」）、
`metrics {sample: MetricSample}`（每 10 秒，对齐到整 10 秒；hub 存进 `machine_metrics`，`docs/machines.md`）、
`codex.interactions {interactions}`（这台机器上所有开着的 Codex / TRAEX 提问和审批，有变化时整份发；hub 增删卡片，旧 hub 不认就照旧是屏幕卡）。

node → hub 请求：
- `interaction.create {interaction}` → `{resolution}`（阻塞到被处理 / 过期；过期返回 `{"dismiss":true}`）
- `mcp.tools {}` → `{tools}`；`mcp.call {name, args, session_id}` → `{content, is_error}`
- `notify {title, body?, level?, silent?, id?, session_id?}` → `{sent, reason?}`（`personad notify`：`machine_id` 是这条连接的机器，不接受扩展的 `actions` / `ui` / `state`；其余同 `POST /api/v1/notify`，静音和「需要你」偏好一样生效；旧 hub 报 unknown method）

### 电脑链路（jarvis-link，kind link）

hub 连不进去的电脑（笔记本）由 jarvis-link 主动连 hub 的 TLS 端口，在这条链路上开流，流里就是上面这套 RPC（字节原样转给那台的常驻 node）。
2026-11-04 起 hub 认完凭据把链路和流原样转给管理者（`devices.accept` + `GET /api/v1/bus/relay`，下面「电脑（devices.*）」），下面 hub 发的消息是管理者发的，关闭码照转；
管理者没连着时 `GET /api/v1/link` 回 `503 assistant_offline`。细节见 [link.md](link.md)，这里只列线上的东西：

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
    `creds.revoked {tokens:[{id,name,kind,role,hash}]}`（其余字段不用）→ `{revoked}`：把已撤销的凭据告诉组里的 hub；
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

## 总线（服务 ↔ hub，`GET /api/v1/bus`）

hub 要变成只是一条通道：助手（以后还有别的服务）是另一个进程，主动连 hub 的总线、一直连着，而不是住在 hub 里。hub 不认识任何服务的业务语义；
默认 hub 把助手当作自己的子进程起起来（`personad assistant`），助手也可以跑在别处（`docs/assistant.md`「进程」）。
通道本身是鉴权、握手、名册、按方法名派发、两个方向的调用；助手经它和 hub 互相调用，见下「助手用的方法」。

**身份和 token**

- 每个服务一个身份。现在只有 `manager` 一种角色（一个管理者进程，即助手；id 形如 `assistant`，小写字母开头，最多 32 个字符）；表里留着角色一列，以后加别的角色不用改表，别的角色的 token 现在在升级时就被拒（401）。
- token 以 `jb_` 开头，只能开 `/api/v1/bus`：设备 token、主 token 都开不了总线，`jb_` 也开不了 App 的接口（401）。hub 只存 sha256（表 `bus_identities`，迁移 0311）。
- 只认 `Authorization: Bearer`，不收 `?token=`。公网来源要 TLS（否则 `403 tls_required`），失败计数和 `429 rate_limited` 同 App 的接口；回环、内网不计数。
- 生成和撤销是 hub 里的函数（`createBusToken`、`revokeBusToken`，在 `server/internal/hub/bustokens.go`）。hub 每次启动给助手生成一个、从子进程的 stdin 交给它；
  助手跑在别处时用 `personad hub assistant-token` 生成（`docs/config.md` 的 `assistant.external`）。没有接口和界面。
  同一身份再生成一次就换掉旧 token：旧的立刻失效，用它连着的连接以 4002 断开。
- 互联的成员 hub 不把总线转给中心：服务连的是哪台 hub，就是哪台的。

**线上格式**：每条 WebSocket 文本消息是一行 `internal/rpc` JSON（和 hub ↔ node 同一套），permessage-deflate，单条最大 64 MiB，两个方向都能调用：

```jsonc
{"id":"s1","method":"manager.hello","params":{"id":"assistant"}}
{"id":"s1","result":{"name":"助手","role":"manager","managers":[{"id":"assistant","name":"助手","role":"manager","online":true}],
                     "owner":"","owner_notes":"","assistant":{"machine_id":"server","machines":["server"]},"done_delay_ms":60000}}
{"id":"s2","error":{"code":"stale_connection","message":"…"}}
{"event":"managers","params":{"managers":[{"id":"assistant","name":"助手","role":"manager","online":false}]}}
```

- 两边每 30 秒 ping，20 秒没有 pong 就断开。连上后 40 秒内没完成握手，hub 以 4003 断开。
- 一个身份的新连接在**握手成功时**替换旧连接，旧的以 4001 断开。
- 握手之前、被替换之后，这条连接上的调用一律回 `stale_connection`，发的事件丢掉；hub 也不调没握手的连接。
- 关闭码：4001 被同一身份的新连接替换、4002 token 撤销或换了、4003 握手不对或超时、1001 hub 停机。

### 握手

`manager.hello {id,host?,os?,arch?,deploy?,version?,apps?,…}`。`id` 必须与 token 一致，否则回 `wrong_identity` 并断开（4003）。
名册里只记 `host`、`os`、`arch`、`deploy`（hub 起的助手报 `child`，别处的报 `external`）、`version`；`apps` 和机器摘要现在不处理。
回答 `{name,role,managers:[{id,name,role,online}],owner,owner_notes,assistant:{machine_id,launcher?,machines,standby?},done_delay_ms,kb}`：

- `name`、`role`：hub 给这个身份记的名字和角色（以 hub 为准，hello 里报的角色不算）；
- `managers`：hub 有 token 的所有管理者，`online` 是现在是否连着；
- 其余是助手要的那份（`assistantwire.Hello`）：hub.json 的 `assistant` 一节，是助手自己的设置的默认值（设置是助手的，见 `docs/assistant.md`「和 hub 的边界」）：`owner` 是 `assistant.owner`，`owner_notes` 是 `assistant.notes`，
  `assistant.machine_id` 是 `assistant.machine`（hub 没有这台就是 hub 自己那台，再没有就是第一台），`assistant.launcher` 是 `assistant.launcher`；`machines` 是 hub 有的机器（助手存的机器不在里面就不用），
  `standby` 这台 hub 是互联组的成员（定时任务由中心跑）；`done_delay_ms` 是 hub.json notify 的完成推送延迟（任务的 Agent 停多久才算停下）；`kb` 是 hub.json 的 `kb` 一节
  `{off?,text_days?,attachment_days?,extra_dirs?}`：助手的知识库照它存，助手跑在别处也一样（`docs/knowledge.md`「谁管什么」）。用户的时区、主对话不在这里：是助手自己的设置，它在快照的 `profile` 里报给 hub。
- 助手记着这份回答。hub 在它可能变了的地方（互联组换中心、机器列表变了）合并 200 毫秒后重算一次，和上次给的不同就发
  `manager.updated`，参数就是新的这份（同上，不带 `name`、`role`、`managers`）；助手收到后重读自己的设置、报一份快照。

同一条连接可以再握手一次（更新名册里的信息），不算替换。

### 名册

hub 在内存里记着每个身份当前的连接（握手成功的那一条）：id、角色、名字、握手时报的机器信息、连上的时间。hub 重启后为空，服务重连、再握手。
管理者上线、下线时，hub 给其他在线的管理者发 `managers {managers:[{id,name,role,online}]}`；刚连上的那个从握手的回答里拿到同一份列表。

### 方法

- 服务 → hub：hub 按方法名登记处理函数（`server/internal/hub/bus.go` 的 `handleBus`，单向事件用 `handleBusEvent`），处理函数所在的文件在 `init` 里登记，hub 没有别的方法清单。
  握手之外内置的只有 `ping {}` → `{}`。没登记的方法回 `unknown_method`，参数解析不了回 `invalid_params`；处理函数返回的 `rpc.Error` 带着它的 `code` 原样到服务。
- hub → 服务：`callBus(身份, 方法, 参数)` 调这个身份当前的连接（调用方没给期限时两分钟后放弃），`notifyBus` 发单向事件；不在线返回「不在线」的错误，服务回的错误码原样带回。

### 助手用的方法

hub 这一侧只有一份实现：`assistantLink`（`server/internal/hub/assistant_link.go`）用 hub 自己的函数回答助手的调用，同一个文件里用 `handleBusCall` 登记的处理函数只做编解码
（参数解成那个方法自己的类型，解不了回 `invalid_params`），不另写一份逻辑。hub 不跑助手的代码（`go list -deps ./internal/hub` 里没有 `internal/assistant`），
两边共用的只有消息的类型、方法名、错误码和助手的路由表（`server/internal/assistantwire`），以及写字用的几个小函数（`server/internal/textfmt`）。方法和参数的意思见 `docs/assistant.md`「和 hub 的边界」；
`app.call` 的交互队列的方法（`interactions.answer`…）只收发数据，用它们的工具（`interaction_answer`）的规则和文字在助手。
收件箱是助手的（2026-11-04，`docs/inbox.md`「谁管什么」），hub 只管它的来源，消息的形状在 `assistantwire/inbox.go`。
知识库 2026-11-04 起是助手的（`docs/knowledge.md`「谁管什么」）：hub 只给来源（`app.call connectors.*`），它自己的功能要读知识库时问助手（`kb.*`）。
扩展也可经自己的 RPC 调用六个公开知识库方法（#1908）：hub 先检查实际获准的 `kb.read` / `kb.write`，再代问助手，
并不是恢复助手旧的 `app.call kb.*` 工具链。字段、分页和权限见 [扩展文档](extensions.md#知识库-rpc1908)。
助手用 `app.call connectors.owner {}` 获取最小的本人来源元数据 `{sources:[{source,handles:[{kind,value}]}]}`，当前由 hub 提供已配置邮箱地址，不返回账户凭据。
助手的专用单向事件 `kb.changed {kind,id,what}`、`kb.forgotten {source}` 只由当前助手连接发送；hub 仅给获准 `kb.read` 的扩展订阅者转成 `event.kb.*`，不广播给 App。

- 助手 → hub：`device.call`、`app.call` → 结果原样；`notify.request {notification}` → `{ok:true}`；`manager.snapshot` → `{ok,seq}`（快照带 `profile`：助手的设置里 hub 自己的功能要用的，hub 留最后一份）；单向事件 `ws.broadcast`（见下）。
  2026-11-04 加的：`device.call` 交给 node 的 `assistant.prepare`、`assistant.warm`、`assistant.memory`、`assistant.compact`、`assistant.handoff`、`launchers.list`、`launchers.probe`（`devices.list` 多了 `kind`、`data_dir`），去掉 hub 自己答的 `assistant.start`；
  `app.call` 的 `models.record`、`models.save`、`models.apply_picked`、`models.launchers`、`models.launcher_check`、`kb.dirs`、`connectors.signed_in`、`plugins.assistant`、`plugins.skills_need`、`settings.list`，去掉 `models.assistant_send`。
  `app.call` 带 `timeout_ms`（助手这次等多久）时 hub 到点也停下（比如一次模型调用）。
  同一天知识库挪进助手（`assistantwire/kb.go`）：去掉 `app.call` 的 `kb.status`、`kb.prompt`、`kb.dirs`、`kb.search`、`kb.list`、`kb.get`、`kb.update`；加上知识库的来源
  `connectors.archives {}` → `{archives:[{id,poll_ms?,sources?,media?}],items:[id]}`、`connectors.changes {id,cursor,limit}` → `{batches,next,more?}`、
  `connectors.archive_media {id,ref}` → `{media:{mime,data}}`、`connectors.items {id}` → `{items}`、`connectors.doc_title {id,url}` → `{title}`；`models.json` 多一个可选的 `max_tokens`。
  同一天收件箱挪进助手：去掉 `app.call` 的 `inbox.replied`、`inbox.draft`；加上收件箱的来源 `inbox.collect {round}`（hub 马上读一轮、交过来再回答：下拉刷新）、`inbox.kick`、`inbox.config`（多了 `extensions`：插件提供的连接器）、
  `inbox.translate_fingerprint {}` → `{fingerprint}`、`inbox.mail_body {account_id,uid,mailbox,readable?}`、`inbox.reply_mail {account_id,uid,mailbox,text,send_id}` → `{already?}`、`inbox.reply {source,reply,send_id}` → `{already?}`
  （同一个 `send_id` hub 只发一次，留一天：再问回 `already`，上次发到一半断了的回 `assistant_in_doubt`、不再发）、`inbox.settle`、`inbox.group_tool`、`inbox.group_fallback`、`inbox.group_append {source,append,append_id}`（同一个 `append_id` 再问用插件上次的回答）；
  单向事件 `inbox.changed {items:[{id,source,kind,title}]}`（hub 在读循环外给插件发 `inbox.changed`、给 App 推 `{"type":"inbox"}`）；快照的 `inbox: {agents:[{machine_id,session_id}]}`（AI 分身的对话：跑完不推「完成」，`null` 时 hub 留着上一份）。
  快照的 `todos` 是待办的报告（`assistantwire.TodoReport`，待办归助手，`docs/todos.md`「谁管什么」）：`{links:[{machine_id,session_id,todo_id,title,task?}],quiet:[{machine_id,session_id}]}`，
  `links` 是未完成的待办上活跃的 Agent（`task`：从对话派出去的任务），`quiet` 是安静的任务的 Agent（未完成或完成不到 7 天的）；hub 照它给会话填 `todo`、`quiet`，给卡片填 `task_title`，挡住安静的推送，哪台机器的这几样变了就推那台的会话列表；`null`（读不出来）时留着上一份。
  hub 的错误带码回去，助手按码认回原来的错误：`no_device`（没有这台机器）、`machine_offline`、`node_closed`（node 的连接断了）、`stale_snapshot`、`model_paused`（`data` 是暂停的 `{reason, until}`：额度用完 `quota` 还是并发满 `busy`）、`model_unconfigured`（这个用途没配模型）、`assistant_in_doubt`（收件箱的回复上次发到一半断了，可能已经发出去）；node 自己回的 `rpc.Error` 原样。
- hub → 助手，调用：`http.request`（下面的转发）、`manager.action`（App 回答了它的提问）、`assistant.tools {}` → `{tools:[…]}`（hub 在它每次连上时要一次、记着，断开后也留着）、
  `assistant.tool {name,arguments,machine_id,session_id}` → `{content,is_error}`、`assistant.context {session_id}` → `{text,…}`（node 每条消息前要）、
  `assistant.todo_handoff {todo_id,text?}` → `{found,title?,done_at?,text?}`（`POST /api/v1/sessions`、`…/messages` 带 `todo_id`：待办在不在、完成没有，`text` 前面拼上生效的决策）、`assistant.todo_link {todo_id,machine_id,session_id,title?,cwd?,agent?}` → `{}`（交出去以后挂上；待办没了、完成了回错误）。
  `assistant.start {text,images?,launcher?,permission_mode?,effort?,purpose?,machine_id?,main?,untitled?}` → `{session}`（hub 的功能开对话；收件箱的 AI 分身、飞书机器人和渠道 2026-11-04 起都由助手自己开，hub 现在不用它）、
  `assistant.send_params {machine_id,session_id,params}` → `{params}`（发给它某段对话的消息跑在哪个启动器和模型上、新主对话第一条前面带的背景）、
  `assistant.rename {machine_id,session_id,params}` → node 的回答（App 给它的对话改名）；这几个请求本身不对时回 `bad_request`，hub 回 App 400。
  知识库（2026-11-04）：`kb.context {chat_id}` → `{context}`（收件箱 AI 分身）、`kb.projects {machine_id,cwd}` → `{projects:[{slug,name}]}`（分诊）、`kb.forget {source}` → `{chats,people}`（删除来源的数据）、
  插件程序交来的帖子 `kb.posts_put {connector,sources,posts}` → `{statuses}`、`kb.posts_comments {connector,id,count,comments}` → `{}`、`kb.posts_pending {connector,limit}` → `{posts}`、`kb.archived_chats {connector,sources,source}` → `{chats}`。
  收件箱的：`inbox.collected {epoch,seq,round,sources,mail?,names}` → `{}`（hub 读完一轮来源交过来，助手合成条目后回答；同一个 `epoch` 里 `seq` 不比用过的大的丢掉；读不出存着的收件箱时回 `inbox_unavailable`，hub 留着下一轮再交）、
  `inbox.item {id}` → `{item,ok}`、`inbox.forget {source}` → `{deleted}`、`inbox.repo_mutes {source,set?,add?,repos?}` → `{repos}`（2026-11-04 去掉 `assistant.tasks`、`assistant.card_sent`、`assistant.cards_sent`：都在助手里了）。
- hub → 助手，单向事件（`conversation.started`、`conversation.renamed` 2026-11-04 去掉：开对话、改名都是助手自己做的）：`session.user_spoke`、`session.turn_started`、`session.turn_ended`、`sessions.changed {machine_id, sessions, connected}`（node 报的那份会话列表，带 `spawn_from`：待办的随项目自动挂、派生跟随、herdr 标记都从它来；`connected` 是 node 这条连接开始的时间，变了说明 node 重连过，助手把 herdr 标记全发一次）、`interaction.resolved`（也用来记待办的决策）、`interaction.raised`（安静的任务的 Agent 在问，#1731）、`todos.space_renamed {machine_id,old,new}`（经 hub 改了工作区的名字）、
  `manager.updated`（上面的握手）、`manager.resync {}`（hub 还没有它的快照或只有不完整的一份，要它报整份：最多 5 秒一次）、
  `kb.kick {}`（知识库的来源有新东西）、`kb.agent_turn {machine_id,session_id,cwd,title?,line}`（一个 Agent 一轮做完，进它目录所属项目的时间线）。助手按到达的顺序一条一条处理，`session.turn_ended` 另起（要问模型、等任务的 Agent 停下）。
- **Agents**（2026-11-04 从 hub 挪进管理者，`docs/agent-work.md`「谁管什么」，类型在 `assistantwire/agents.go`）：
  握手的回答多 `agents`（hub.json 的 `agents` 一节：分诊的上限、开关的初始值）；快照多 `work {on, sessions:[{machine_id, session_id, work}]}`（每个 Agent 最新一轮的工作态，
  `work.turn_at` 是那一轮的结束；`null` 时 hub 留着上一份），`interactions` 里多分诊的卡片（`tool_name: persona_triage`，问的是那个 Agent 的会话，答案照样是 `manager.action`）；
  `device.call sessions.list` 的回答多 `muted`（推送关了的会话 id）；分诊看的项目是助手自己知识库里的（`docs/knowledge.md`）；去掉 `app.call models.tab_name`（标签名是管理者自己起的）。
  hub → 助手的调用：`agents.start_params {machine_id, params, name_from?}` → `{params}`（`POST /api/v1/sessions` 之前：按「模型配置 › Agents」和这一次选的填启动器、模型、指纹，`name_from` 不空时起标签名；
  回 `bad_request` 时 hub 回 App 400，没连着时 hub 照 App 给的开始）、`agents.resume_params {machine_id, session_id, params}` → `{params}`（续上不知道启动器的会话之前；hub 不带 params，把回答里的盖到消息上）、
  `agents.tab_name {prompt}` → `{name}`（后台开始的 Agent、飞书卡片开的）；单向事件 `session.seen {machine_id, session_id, at}`（用户在 App 里看了一个会话）。
  分诊用的 `session.turn_started`、`session.turn_ended` 原来就发。用户答了它的提问或卡片而它没收下（没连着，或回了 `gone` 以外的错误），hub 记下答案，
  它下次报快照之后再发 `manager.action`（没连着时马上、它回了错误的过 10 秒），直到它收下、回 `gone`（这张卡片 / 这个提问已经没了：答了、撤了、过期了，不用再给）或卡片本该过期。
  `agents.start_params` / `agents.resume_params` 之前 hub 等它连上（hub 起来后到 60 秒、它断开后 10 秒），等不到 hub 自己套「模型配置 › Agents」。
  它握手时报的版本和 hub 的不一样（开发版不算），hub 在日志里说一句：两边对分诊归谁的理解可能不一样。
- 助手没连着时：转发的接口回 `503 {"code":"assistant_offline","error":"助手暂时连不上，稍后再试"}`，助手的工具同样失败（`is_error`，同一句话），`assistant.context` 也失败（node 那条消息不带实时快照）；
  给它的事件不发（它连上后报整份快照，要的东西现问），只有 `kb.agent_turn` hub 排着（每个 Agent 最新的一条），连上就发。hub 留着它最后报的快照，会话列表照常显示标题和归档。
  知识库的 `kb.*` 调用同样回 `assistant_offline`：删除来源数据的接口回 503（收件箱的条目照删），收件箱 AI 分身的上下文、分诊的项目名就不带；
  插件交帖子的调用先等助手最多 30 秒。助手连着、知识库还没打开时（时区读不出、数据目录在别的进程手里）回 `kb_unavailable`，hub 同样当 503。
  收件箱的来源 hub 照常读，读到的攒着（合并、有上限）等助手连上一起交；收件箱回复在路上断了（连接断了、超时）回 `502 {"code":"assistant_in_doubt","delivered":"unknown","retryable":false}`：可能已经发出去，再发同一条不会发两次（上面的 `send_id`）。
  助手连着、收件箱读不出存着的东西（数据库没答）时，收件箱的接口回 `503 inbox_unavailable`，在读出来之前它什么都不收、不写。

**转发 App 的请求**：App 打到助手接口的请求（鉴权之后）由 hub 作为 `http.request` 交给助手，它的回答原样写回。助手接口是 `assistantwire.Routes`（助手那边有一个测试保证它服务的正是这些）：
`POST /api/v1/assistant/conversations/{id}/archive`、`…/unarchive`、`…/route`、`…/today`、`…/compact`，`GET …/{id}/feed`，`/api/v1/assistant/tasks…`、`/api/v1/assistant/runs`、`/api/v1/assistant/routines…`、
`/api/v1/assistant/followups…`、`/api/v1/task-plans/{id}…`、`/api/v1/task-trust`、`/api/v1/todos…`（待办的全部接口，含 `done-check`、`retry`、决策），`GET/PUT /api/v1/agents/triage`、`GET /api/v1/agents/work/stats`、`POST /api/v1/tabs/autoname`（2026-11-04，Agents），以及（2026-11-04 起）`GET/PUT /api/v1/assistant`、`POST /api/v1/assistant/warm`、
`GET /api/v1/assistant/memory`、`PUT/DELETE /api/v1/assistant/memory/{name}`、`POST /api/v1/assistant/conversations`、`POST /api/v1/assistant/main/new`，以及知识库的全部接口（2026-11-04 起，下面「知识库 API」），
以及收件箱和翻译的全部接口（2026-11-04，`assistantwire.InboxRoutes`）：`GET /api/v1/inbox`、`GET /api/v1/inbox/{id}`、`POST /api/v1/inbox/{id}/done`、`…/draft`、`…/reply`（hub 照旧在前面记回执；助手没连着时直接回 503，不记；在路上断了回 `assistant_in_doubt`，也不记，再发能到助手）、`…/doc/status`、`POST /api/v1/inbox/refresh`、`POST /api/v1/inbox/done`、`GET/POST /api/v1/inbox/mutes`、`DELETE /api/v1/inbox/mutes/{id}`、`POST /api/v1/translate`、`POST /api/v1/translate/mail`。`/api/v1/skills` 还是 hub 自己的。

```jsonc
// hub → 助手
{"id":"h3","method":"http.request","params":{"method":"POST","path":"/api/v1/assistant/conversations/c1/archive","query":"",
  "header":{"Content-Type":["application/json"],"X-Persona-Role":["full"]},"body":"<base64>"}}
// 助手 → hub
{"id":"h3","result":{"status":200,"header":{"Content-Type":["application/json"]},"body":"<base64>"}}
```

- `path` 保留 URL 百分号编码，hub 用 `EscapedPath()` 转发，助手重建请求后由路由解码一次。正文卡片 key 中的 `/`、`#`、`?` 不能在转发前解码成路径分隔符、fragment 或 query。
- `header` 不带 `Authorization`、`Cookie` 和逐跳的头；`X-Persona-Role` 是 hub 鉴权得出的设备角色（App 自己带的会被换掉）。请求体最大 32 MiB。
- 助手没连着、或在请求中途断开回 `503 assistant_offline`，它回的 `rpc.Error` 变成 `502`（带它的 `code`）；没给期限的请求 5 分钟后放弃。

**发给 App 的消息**：助手发单向事件 `ws.broadcast {message}`，`message` 是一个带字符串 `type` 的 JSON 对象（`assistant_schedule`、`assistant_feed`、`task_plan`），hub 原样发给每个 App 的 WebSocket（加上每条消息都有的 `event_seq`）；没有 `type` 的丢掉。

### 电脑（devices.*）

2026-11-04 起电脑是带着它们的管理者连的（现在是首席管理者 `assistant`，[machines.md](machines.md)「谁连电脑」），消息在 `server/internal/devicewire`。hub 那头 `hub/devices_link.go`、`devices_relay.go`，管理者那头 `internal/devices`。

- 握手的回答多一个 `devices`（只给带着电脑的那个管理者）：`{machines:[hub.json 的条目，原样], hub_id, hub_name, hub_version, endpoints?, mesh_hubs?, tailcat_derp_map_url?, link_ping_ms?, link_ping_timeout_ms?, mesh?}`，
  `mesh` 是 `{member?, center_name?, settle_until?, hubs:{hub_id:{name, reachable, rtt_ms}}, members:[{hub_id, machines:[{id, cfg, kind, avail?, rtt_ms?}]}], path_switch_ms}`；变了（管理界面加了电脑、互联组、hub 的地址和名字）hub 发 `devices.config`，参数同上（合并 200 毫秒）。
- 管理者 → hub：
  - 事件 `devices.state {machines:[{machine, config, info, connected?, connected_at?, warm_until?, derived?, paths?}], absorbed?:{条目 id: 机器 id}, entries?:[{id, kind, avail?, rtt_ms?}]}`：
    全部机器，按 App 列的顺序；`machine` 是 App 看到的那份（除了 hub 加的 `system`、`computer`、`browser`；它的 `online` 把管理者重启后的暖启动也算上），`connected` 是 node 现在真连着、调用能到（hub 的自更新等的、更新 node 发给的都是这个），`connected_at` 变了是 node 重连了，`warm_until` 之前是暖启动；`entries` 是 hub 记录里自己的电脑。变了才发（合并 50 毫秒，`last_seen` 单独变不算），每次握手后发一次整份。
  - 事件 `devices.event {machine_id, event, params}`：node 的单向事件，原样、按顺序（`session.items`、`terminal.frame`、`stream.data`……）；`state.put` 管理者自己存，不转。
  - 调用 `devices.request {machine_id, method, params, request}` → hub 的回答：node 要 hub 答的（`interaction.create`、`assistant.lark`、`mcp.tools`、`mcp.call`）；`request` 是管理者给的键。
  - 事件 `devices.request_end {machine_id, request}`：这条请求来的 node 连接在 hub 回答前断了，hub 不再等它（和 node 自己的连接断了一样：能续上的卡片留着，等 hook 从下一条连接来认领）。
  - 调用 `devices.connected {machine_id, connected_at}` → `{configure?:{键: 值}, calls?:[{method, params}]}`：node 刚连上（或管理者和 hub 的连接新连上，每台在线的再来一次），
    hub 要加进 `node.configure` 的（`codex`）和之后要发的（`launchers.managed`、`session.echoes`），管理者照做后才要会话列表；hub 同时开始它在连上时做的事。
  - 调用 `devices.connector_cred {machine_id}` → `{machine_id, fp, token, version?}`（kind service 的凭据，hub 封着存，`token` 为空是没配对）、`devices.connector_paired {machine_id, fp, token, version}` → `{}`（配对完交给 hub 存）。
- hub → 管理者：
  - 调用 `devices.call {machine_id, method, params, timeout_ms?}` → node 的结果：App 和 hub 的功能对 node 的调用；`session.watch` / `session.unwatch` 管理者也记着，node 换连接时再 watch。
    `timeout_ms` 是 hub 还肯等的时间减一点（十分之一，最多 500 毫秒），管理者的超时回答先到；hub 没给期限的调用（插件的 `nodes.call` 等）也不给，和 hub 自己连 node 时一样等。
    错误码：`no_device`、`machine_offline`（没发出去）、`node_closed`（发出去以后 node 的连接断了，或调用被打断）、`deadline`（发出去以后时间到了）；node 自己带码的错误原样。
    hub 认回原来的错误：离线照旧 503 `node_unreachable`；`node_closed`、`deadline` 和总线在调用途中断了都算「可能已经做了」，发消息的结果是 `unknown`（同一条不会再发），不当成没发出去；
    总线中途断了的别的调用 App 看到的是 503 `assistant_offline`（管理者不在了）。
  - 事件 `devices.notify {machine_id, event, params}`：发给 node 的单向事件（终端输入、转发端口的字节）。管理者按机器排队、各自按顺序写，一台电脑收得慢只耽误它自己；写一条卡住 30 秒、或排着的超过 32 MiB，就重连那台。
  - 事件 `devices.link_rtt {id, rtt_ms}`：hub 在一条 Persona Link 链路上的心跳量出的往返（`id` 是 `devices.accept` 的），管理者比路时用。
  - 调用 `devices.accept {id, kind, machine_id, token_id?, stream_id?, source?}` → `{}`：hub 握着一条给管理者的连接（`kind`：`link` Persona Link 的链路、`link_stream` 它开的流、`node` 中心经这台成员要的 node 流），管理者拨 `GET /api/v1/bus/relay?id=<id>` 接过去，hub 等 10 秒。
  - 调用 `devices.reconnect {machine_id}`、`devices.update_nodes {}`（hub 自更新以后，重新部署不是这个版本的 node）、`devices.pair_connector {link, machine_id, address?, proxy?}` → `{address, version}`（管理界面配对 kind service）。
  - 管理者自己回答的路由（`devicewire.Routes`）经 `http.request` 转：`POST /api/v1/machines/{mid}/reconnect`、`PUT …/engine`、`GET/PUT …/herdr`、`POST …/herdr/start`、`POST …/deploy`、`GET …/claude-dirs`、`PUT …/claude-dirs/blocked`。
- **转发通道** `GET /api/v1/bus/relay`（WebSocket，管理者的总线 token，和 `/api/v1/bus` 同样的鉴权，只有带着电脑的那个管理者能开）：`?id=` 接过 hub 握着的连接；`?hub=<hub_id>&machine=<id>` 要一条经那台 hub 到它电脑的 node 流（hub 打不开时升级前回 502，`error` 是原因）。
  hub 逐条转消息（文本、二进制照原样），一边的关闭码和原因给另一边；Persona Link 那边的心跳是 hub 的，没有 pong 时以 1001「no pong within …」关管理者那边。
  `kind: node`（成员替中心开 node 流）的那条，管理者先打开 node，开好了发一条文本 `open`、开不了以原因关掉，成员这才回中心：`101` 或带原因的 502（最多等 1 分钟）。
- 管理者每次握手（hub 重启了，或总线断过）把上一条连接时 hub 在 node 上开着的东西在 node 那边关掉：转发的流（`port.dial`、`plugin.node.dial`）发 `stream.close`，看着的终端 `terminal.unwatch`；
  hub 在握手时也结束经上一条连接开着的流和终端（终端以「助手暂时连不上，稍后再试」结束，App 重新打开），还在等第一次 watch 的不动。
- 管理者没连着时：机器显示离线（`error`「助手暂时连不上，稍后再试」），要经它的接口回 `503 assistant_offline`，终端流以同一句话结束，转发的端口关掉，`/api/v1/link` 回 503；
  hub 刚起来、管理者还没第一次上报的那 30 秒里，调用等它而不是立刻失败（机器按管理者交接的显示），`/api/v1/link` 和成员的 `/api/v1/peer/node` 最多等 5 秒。
- node 的一条消息超过 60 MiB 时管理者不转（总线一行最多 64 MiB）：事件丢掉，调用回 `too_large`。

### 渠道用的方法（2026-11-04）

飞书机器人和插件渠道「和 Persona 对话」的那一半是助手的（`docs/channels.md`「谁管什么」）：hub 只管收发，主人的消息和按钮交给助手，助手要发、要改的叫 hub 做。
类型在 `assistantwire` 的渠道一节；hub 那头是 `hub/assistant_channels.go`，助手那头是 `assistant/channels*.go`、`feishu*.go`。

- hub → 助手，调用：`channels.receive {channel, message, action}` → `{}`（插件渠道主人的一条消息，`message` 是 `channel.Message`）、`feishu.message {event}` → `{}`（飞书私聊里主人的一条消息，`im.message.receive_v1` 的 `event` 原样）：
  助手按消息 id 去重、收下就回，处理在后台（飞书的先读 `feishu.status`，读不到回 `try_again`，没收下）；`feishu.card_action {event, action}` → 卡片回调的回答（`{toast?, card?}`，飞书等 3 秒，hub 给 2.5 秒）；`feishu.view {}` → `{target?, agents?}`（`GET /api/v1/bots/feishu` 里助手的那两项）。
  `action` 是 hub 发的一次性**主人操作**（`oa_…`，10 分钟内有效；按卡片的只能答那一张卡）：助手答卡片（`interactions.resolve`）要带上它。
  主人的消息 hub 先等助手连上最多 60 秒（同时最多 500 条），调用断了、超时、`try_again` 就再交一次（助手按 id 只收一次）；等不到 hub 自己回主人：飞书「没能发出去：助手暂时连不上，稍后再试」，卡片 toast「助手暂时连不上，稍后再试」，插件渠道 `channel.send`（`kind: note`）「没发出去：助手暂时连不上，稍后再试」（同一条只回一次）。
- hub → 助手，单向事件：`interaction.announced {interaction}`（新的卡片，安静的任务的除外）、`interaction.ended {interaction, resolution?}`（卡片结束了：答了的带回答，撤回、过期的不带；同一张卡先带回答再不带地各来一次）、
  `session.items {machine_id, session_id, items}`、`session.delta {machine_id, session_id, text}`（助手跟着的对话的新条目和正在写的字）、`feishu.owner {paired}`（机器人配对 / 解除配对）、
  `channels.noted {channel, machine_id, session_id}`（渠道来的消息经 App 的接口进了对话：语音的 `voice-…`）。
- 助手 → hub，`device.call`（hub 自己答的）：`session.follow {session_id}` / `session.unfollow {session_id}` → `{}`（跟一段对话：hub 像 App 打开它那样 watch，把新条目和字转过去；助手断开时 hub 全部放掉，助手连回来再跟）、
  `session.send_once {session_id, text, client_msg_id, images?, …}` → `{}`（node 的 `session.send`，带着和 App 一样的只发一次的保护：发过的不再发，上次发到一半断了的拒绝）。
- 助手 → hub，`app.call`：
  - `feishu`：`feishu.status {}` → `{ready, enabled, features, owner?, paired_at?, public_url?}`（`owner` + `paired_at` 是哪一次配对：变了，或没了，助手的聊天状态从头来）；`feishu.post {reply_to?, card, fallback?}` → `{message_id}`（卡片回复那条消息，否则发进主人的私聊；飞书拒收的卡片改发富文本）、
    `feishu.text {reply_to?, text}` → `{message_id}`、`feishu.update_card {message_id, card}`、`feishu.react {message_id, emoji}` → `{reaction_id}`、`feishu.unreact {message_id, reaction_id}`、
    `feishu.messages {message_id}` → `{items}`（引用、合并转发）、`feishu.resource {message_id, key, kind}` → `{data, mime}` 或 `{mime, too_large: true}`（超过 3 MB 的图片）、
    `feishu.save {message_id, key, kind, name, machine_id}` → `{path}`（文件存到那台电脑的临时目录，最多 50 MB，不经总线带内容）、`feishu.user_name {open_id}` → `{name}`、
    `feishu.configure {enabled?, features?: {id: on}}`、`feishu.test {}`。机器人没开、没连上或没配对时（`ready` 是 false）发、改的方法回错误「机器人没打开或还没配对」；只发给配对的主人。
  - `channels`：`channels.list {}` → `{channels: [channel.View]}`（每个渠道的传输状态：不带目标、插件渠道的功能，助手填）、`channels.send {channel, ref, text, kind, reply_to?}` → `{message_id?}`（一段，失败按同一个 `ref` 重试；还没地方发回错误码 `cannot_send`）、
    `channels.typing {channel, on}`、`channels.enable {channel, on}`（插件和它的连接器、飞书机器人的开关；收件箱的分拣模型没配时回 `needs_triage_model`）、`channels.unbind {channel}`。绑定不在这里：`POST /api/v1/channels/{id}/bind` 是 hub 自己的路由（凭证不经过助手）。
  - `interactions`：`interactions.resolve {interaction_id, resolution, action}` → `{ok, status?}`（按用户在渠道里的回答答掉这张卡；`action` 是那一下的主人操作，没有、用过、是别的卡的一律拒绝，记成用户自己答的；已经不在等了 `ok` 是 false）。
- 快照多了 `channels: {marks:[{machine_id, session_id, channels}], turns:[{machine_id, session_id, answered?}]}`：hub 照 `marks` 给会话填 `channels`（变了推那台机器的会话列表），
  照 `turns`（`answered` 空着是正在跟，否则是答完的时间，一小时内的）不推那段对话这一轮的「完成」；`null`（读不出来，快照算不完整）时留着上一份。
- 助手的路由多了 `/api/v1/channels*`，绑定 `POST …/bind` 除外（`docs/channels.md`「接口」）。
- 助手这边的事件队列：`session.delta` 每段对话只排一条（新的字顶掉旧的），队列满了只丢 `session.delta`、`session.items`，别的等有空再进，不丢。

### 服务这一侧（`server/internal/bus`）

`Dial`、`Conn`（WebSocket 和 rpc 行之间的适配）、`Keepalive`，以及 `Client`：拨号、握手（`HelloMethod`、`HelloParams`、`OnHello`）、回答 hub 的调用和事件，断了就重连：
1 秒起翻倍到 1 分钟，连上超过 1 分钟后再断就从 1 秒重新算；token 被拒（401、关闭码 4002 / 4003、握手回 `wrong_identity`）每次都等最长的 1 分钟。
没连上（或握手还没完成）时 `Call`、`Notify` 返回 `ErrOffline`。
助手用的是它包一层的 `assistant.BusHub`（`server/internal/assistant/bus.go`）：记着握手的回答、把 hub 的错误码认回来、按顺序处理 hub 的事件、每次连上报整份快照。

## hook / mcp ↔ node（unix socket `~/.persona/node.sock`）

一次连接一问一答，各一行 JSON：

- `{"kind":"hook","input":<Claude Code 传给 hook 的 stdin JSON>,"pane":"…","attempt":"<hook 进程自己的 id>","resume":<是不是重连>}`
  → `{"output":<要打印到 stdout 的 JSON 或 null>}`，或 `{"retry":true}`（node 正在退出，去问下一个 node）
- `{"kind":"mcp.tools"}` → `{"tools":[…]}`
- `{"kind":"mcp.call","name":"…","args":{},"session_id":"…"}` → `{"content":[…],"is_error":false}`
- `{"kind":"ask","input":{"question","options?","timeout_seconds?","cwd?"},"pane":"…","attempt":"…","resume":…}`
  → `{"status":"answered|denied|dismissed|timeout|unavailable|invalid","answer?","error?"}`，或 `{"retry":true}`（`personad ask`，选项给 `question` 卡、没有选项给 `permission` 卡，`script-cli.md`）
- `{"kind":"notify","input":{"title","body?","silent?","error?"},"pane":"…"}` → `{"sent":true|false,"reason?","error?"}`（`personad notify`）

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
| 请求 `runtime.hello {runtime, name?, agent?, version?, instance?, launcher?, launcher_model?, launchers?, caps: {start, send, interrupt, rename, close, assistant?, modes?, resume?}, package?: {name, hash}}` → `{protocol, node_version, hub, features, update?: {name, hash, version}}` | 必须第一个发；`instance` 是这个进程的标识（SDK 每个 runtime 一个，重连不变）：同一个 runtime id 已有一条活连接、它的 `instance` 不同时，回错误 `code: runtime_conflict`（另一个进程配了同样的 id，连不上，SDK 每 30 秒再试一次、不抢）；相同时是这个进程重连了，旧连接被关掉；没带 `instance` 的旧 runtime 照旧（最新的连接为准）；`hub` 是 node 现在有没有连着 hub（旧 node 不带，当作有）；`features` 是 node 在第一版协议之外支持的能力：`prefer_running`（两个 runtime 列同一个会话时，列成非 `offline` 的那个负责它，所以 runtime 可以把只在磁盘上的会话列成 `offline`）、`resume_argv`（认会话行里的 `resume_argv`）、`handover`（发消息被回 `code: handover` 时交给接手的 runtime，见下面 `session.send`）、`transcript`（认会话行里的 `transcript`）、`launchers`（认 hello 的 `launchers` 和事件 `runtime.launchers`）、`start_params`（`session.start` 带上表单的选择）、`modes`（`session.modes`）、`resume`（`session.start {resume}`）；daemon 只拿到 `prefer_running`、`start_params`。之前的事件 node 一律丢掉。`runtime` 是 id（`[A-Za-z0-9][A-Za-z0-9_.-]*`，如 `dsh`），也是会话的 `space_kind`；`agent` 是它的会话跑的 Agent，不填就是 `runtime` 自己；`name` 是 App 里的叫法（`DSH`），这种会话带 `agent_label: name`，App 不用为新 runtime 发版。`caps` 说明支持哪些操作，没声明的 node 直接回 `unsupported`。`launcher` 非空且 `caps.start` 时，node 的 `launchers.list` 多一项 `{name: launcher, model: launcher_model, source: runtime}`，新建会话选它就交给这个 runtime。两个 runtime 报同一个启动器名时只有先连上的那个提供（另一个记一行日志）；和登录 shell 里的启动器同名时 shell 的那个为准，runtime 的不列。`caps.assistant`（要有 start 和 send）：它的启动器能跑 Persona 的助手，`launchers.list` 里带 `assistant: true`；node 怎么驱动它见 `docs/runtimes.md`「助手」。
`launchers`（要有 `caps.start`）：除了 `launcher` 之外，新建表单里还有这些项。每项是 `{name, model?, agent?, models?}`：
  - 跑多个 Agent 的 runtime（Paseo）一个 provider 一项。
  - `agent` 是这一项起的 Agent：`claude` 时表单给 Claude Code 的选择，App 按 Claude 画。
  - `models` 是 `launchers.models` 返回的模型和它们的思考强度。
  - 名字规则同 shell 的启动器；超过 64 项、每项超过 200 个模型的部分丢掉。

`caps.modes`：接受手机切权限模式 / 思考强度（`session.modes`）。`caps.resume`：能接手一个它没有的 Claude 会话（`session.start {resume}`）。 |
| 事件 `runtime.sessions {sessions: [Session]}` | 它的全部会话，变化时推全量。字段是 `Session` 的子集：`session_id`（`[A-Za-z0-9][A-Za-z0-9_.-]{0,127}`，比 Claude 的 uuid 宽，DSH 是 `session-<uuid>`）、`title`、`cwd`、`status`（`working / blocked / done / idle / offline`，别的当 `unknown`）、`agent`、`model`、`updated_at`、`last_text`、`rev`（变了 App 就拉新内容）、`space_*`、`launcher`、`permission_mode` / `effort`（它现在用的模式，`session.info` 也报这个），以及还在跑的子 Agent 和后台任务 `subagents` / `background`（`docs/runtimes.md`「子 Agent 和后台任务」：子 Agent 的 `session_id` 由 node 写成 `<会话>@<id>`，这个 id 的调用交给父会话所属的 runtime，它推内容也用这个 id；后台任务的 `output_file` 清掉）。node 清掉 `machine_id`、`state` 这些 hub 算的字段，补 `agent` / `agent_label` / `space_kind`，并按「Codex 来源与能力」的同一套字段标来源：`source_kind: "runtime"`、`source_instance_id` 是 runtime id、`capabilities` = `{read: true, send: caps.send, interrupt: caps.interrupt, approve: true, terminal: false, attach: false}`（没声明 `send` 的带 `read_only_reason`），App 和 hub 按它们开关按钮、守发送；合并进 `sessions.list` 立刻推 `sessions.changed`；同一个 id 两条连接都报时，列成非 `offline` 的那条负责，同等时新的为准（`prefer_running`）。两个 node 用、App 看不到的字段：`pane_id`（这个会话跑在这台机器终端引擎的哪个窗格：herdr 窗格里的 pi，node 把它和引擎的窗格合成一行 herdr 会话，调用里终端那部分给引擎，其余给 runtime，`docs/pi.md`）、`resume_argv`（列成 `offline` 的会话怎么在终端引擎里接着：手机发消息时 node 在引擎里开新标签、经 `jarvisd tab-start` 运行它、等 runtime 再列出这个会话为运行中后把消息发过去；引擎不在就拒绝，`resume_argv` 特性）、`transcript`（这个会话的对话就是这台电脑上的一份 Claude 对话记录，值是 Claude 会话 id，`transcript` 特性）。

**带 `transcript` 的会话**（跑 Claude Code 的 runtime，比如 Paseo）：
  - **node 管的部分**：从对话记录给出内容，`session.items` / `item` / `image`、watch、`session.info`、子 Agent 都是 node 的，和终端里的 Claude 会话同一套。
    行上的 `last_text`、`rev`、「在做什么」、后台任务也取自对话记录。
  - **runtime 管的部分**：状态、标题、模型、模式、发送、卡片。它只推逐字文字，推来的 items 被丢掉。
  - **列表**：这份对话记录自己的「未运行」行不再列出。
  - **只算 node 找得到的对话记录**：第一轮刚开始还没写出来、或者在 node 不认识的配置目录里的，内容先归 runtime。
    等文件出现，下一次列表一到，node 就换到对话记录：正在 watch 的改订，`history` 跟着变，App 整页重取。
  - **限制**：daemon 不能带这个字段。连接断开，它的会话就从列表里消失。**列表只是「现在给 App 看哪些」，不是归属**：runtime 只列它最近的会话（DSH 是几千个里最近的 80 个），node 记着每个会话是哪个 runtime 列过的（按 runtime id，重连后还是它），对一个不在列表里的老会话的调用照样交给它；这台机器上什么都没有（没有 Agent、没有 transcript）的会话，`session.send` / `items` / `watch` 这类调用先用 `session.items {session_id, limit: 1}` 问一遍连着的 runtime，答得上来的就归它（问不到的一分钟内不再问）。属于某个 runtime、它却没连着时，回错误 `code: runtime_away`（`delivered: none`），不会当成 Claude 会话去终端里恢复 |
| 事件 `runtime.launchers {launchers}` | 换掉 hello 里的 `launchers`：provider 多了少了，或者它的模型现在才知道 |
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
| `session.watch {session_id, since_seq, transcript?}` / `session.unwatch` | 开始 / 停止推这个会话的新内容；`since_seq > 0` 时先补发其后的全部。node 记着每个被看着的会话推到了哪，runtime 重连后列出它时按那个 seq 再 watch。`transcript: true`：内容 node 从对话记录推，runtime 只推逐字文字 |
| `session.send {session_id, text, images, client_msg_id, assistant?, context?, mode?, memory_dir?}` | 发消息（`assistant` 和后面几个只发给声明了 `caps.assistant` 的 runtime 的助手对话：`docs/runtimes.md`「助手」）；`client_msg_id` 交给 runtime 做幂等（node 自己也按它去重 10 分钟），node 也按它在 runtime 推来的 item 里认出这条消息（「待发消息的对应」）；runtime 自己知道哪个 item 是这条消息时可以直接在 item 上带 `client_msg_id`，node 原样转发。runtime 回错误 `code: handover`（`data.delivered: "none"`）表示它不接这个会话、已经让能跑它的程序起来（只读磁盘的伴随进程）：node 等另一条连接把这个会话列成非 `offline`，或者每 2 秒问一遍别的 runtime 有没有这个会话（`session.items {limit: 1}`），先到的那个收到这条消息；45 秒内都没有就回 `not_running`，什么都没发 |
| `session.interrupt {session_id}`、`session.rename {session_id, title}`、`session.close {session_id}` | 各按 `caps` 放行，没有的回错误 `code: unsupported`（文字以 `unsupported` 开头，同宿主缺能力）。只带 `tab_name` 的改名直接成功（这些会话没有标签页） |
| `session.start {cwd, prompt?, assistant?, launcher?, model?, permission_mode?, effort?, workspace_id?, resume?, shell_launcher?, open_elsewhere?}` → `{session}` | 选了它的 launcher 时，或者它是这台机器的引擎时；返回的会话马上进列表。`assistant: true`：助手用它的启动器开新对话（`cwd` 是助手目录，不带 `prompt`，第一条消息随后用 `session.send` 发）。其余字段只在有值时带，见下 |
| `session.modes {session_id, permission_mode?, effort?}` | 手机切权限模式 / 思考强度（声明了 `caps.modes` 的；值先由 node 校验） |

`session.start` 带的字段，都只在有值时才带：
  - **表单的选择**：`launcher`、`model`、`permission_mode`、`effort`。`workspace_id` 是在它的哪个空间里新建（它的会话的 `space_id`）。
  - **`shell_launcher: true`**：`launcher` 是登录 shell 的启动器，由 runtime 自己挑同名的那个，没有同名的就用它的默认。
  - **`resume`**：要接手的 Claude 会话 id（`caps.resume`，这台机器的引擎接着一个没在运行的会话）。返回的会话 id 必须和它相同。
  - **`open_elsewhere: true`**：有一个活着的 claude 正开着这个会话。不是 runtime 自己的就要拒绝，错误码是 `open_elsewhere`，免得两个进程同时写；
    node 把拒绝换成说明在哪开着的那条提示。

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
{"id":"feed_…","conversation":"…","at":"...","kind":"plan|task|answered|receipt|routed|today|message","ref":"plan_…、todo_…、日期或收件箱条目 id",
 "body":{…}}
// plan：ref 是计划 id，没有 body；task：ref 是待办 id，没有 body
// answered：{"question":"使用 Bash","answer":"批准了 · 以后同类不再问",
//           "items":[{"question":"使用 Bash\nnpm test\n原因：跑一下测试","answer":"批准了 · 以后同类不再问"}]}
//   question / answer 是折叠时那一行的摘要（压成一行、截到 120 字），老客户端和 #1231 之前的老记录只有这两个；
//   items 是完整内容，展开时画：每个问题和回答成对、保留换行，多问题卡片按卡片顺序，没回答的写「没回答」，
//   忽略的写「忽略了」；计划审批带计划正文和拒绝理由 / 批准后的模式，工具批准带工具、摘要和原因，
//   屏幕对话框带选中的选项和在选项里输入的文字，表单字段写「标签：值」。单段 4000 字、整条 12000 字、最多 20 个，超出截断。
//   密码字段（Field.secret、屏幕的 secret、MCP 表单的 password / writeOnly、名字像口令 / token 的）只写「（已填写，不保存）」，值不落库。
// receipt：{"title":"…","state":"done|failed|interrupted","summary":"Agent 最后一条回复（Markdown，按行截到约 600 字，不截在标记中间）","pr":"https://github.com/…/pull/N","machine":"…"}
//   interrupted（#1516）：Agent 这一轮被模型服务出错打断，summary 是「模型服务出错，中断了」，error 是错误原文；客户端画成红色、带「继续」
//   这一轮既没回复也没调工具、又没有报错（模型服务回了 200 但内容是空的，#2349）同样是 interrupted，error 是「这一轮没有任何输出：…」
// routed：{"text":"…"}
// message：ref 是收件箱条目 id；AI 分身给一条和任务有关的消息起草了回复：
//         {"todo_id","source":"lark|mail|…","account","chat","sender","text":"原话（截断）","draft":"草稿"}；发送走收件箱的发送接口
// today：ref 是用户那天的日期；{"date":"2026-11-03","done":[{"id","title"}],"waiting":[{"id","title"}],"working":2,
//         "meetings":[{"start","end","title"}],"at_me":[{"name","unread","preview"}],"unread":5}（飞书没回应的那几项不带）
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
| GET | `/api/v1/imessage` | `{machine, status, error?, guide?:[…], via?, ai}`：信息连接器（读哪台在 设置 › 扩展 › 信息 选，没选时是 hub.json 的 `imessage.machine`）的状态，当场问那台 Mac（刚开了权限，刷新就是新的）；没有完全磁盘访问权限时 `guide` 是在那台 Mac 上一步步怎么开，`via` 是那台 Mac 的 node 怎么跑（`ssh` / `launchd`，两种开法不一样）。没设置是 404 `not_configured` |
| PUT | `/api/v1/imessage/settings` | `{ai: bool}`：「交给 Persona」开关（存在 hub，默认开）；开着时真正的会话进知识库、收件箱，助手有 `imessage_*` 工具；关掉立刻停。回 `GET /api/v1/imessage` 的样子（多一个 `ai`） |
| DELETE | `/api/v1/imessage/data` | 删掉 hub 上存下的信息：知识库里的信息会话和人（连同索引、项目里的引用和它们写的时间线、归档游标，项目标成待重写）、收件箱里的信息条目 → `{chats, people, inbox}`。「交给 Persona」开着时 409 `ai_on`（先关掉，不然下一轮又同步回来）；那台 Mac 上的消息不受影响 |
| GET | `/api/v1/imessage/chats?limit=&before=` | 那台 Mac 的会话，新的在前（node 的 `connector.chats`，字段见下面「hub ↔ node」）。只读、不存、不交给模型。没权限 403 `no_permission`，Mac 不在线 503 `offline`，node 太旧 409 `node_outdated` |
| GET | `/api/v1/imessage/chats/{id}/messages?limit=&before=` | 一个会话的一页消息，旧的在前（node 的 `connector.older`）；`id` 不像会话 id（`imessage-<12 位十六进制>`）的不发给 Mac，404 `no_chat` |
| GET | `/api/v1/connectors?all=1` | `{"connectors":[{id, name, status, account?, via?, error?, guide?:[…], last_sync?, last_event?, capabilities:[…], hosts?:[…], enabled, archive?, inbox?}]}`：hub 上注册的连接器。不带 `all=1` 时不含飞书（`lark`）和邮件（`mail`）：M6 以前的 App 把列表里每一项都画成一行，它们又各有一行，会出现两次；带上的 App 自己把这两项画成原来的飞书、邮箱行（状态还读 `/api/v1/lark/status`、`/api/v1/mail/accounts`）。`guide` 是出问题时一步一句怎么修好（信息连接器没有权限时在那台 Mac 上怎么开）。`status` 是 `ok` / `not_configured` / `not_signed_in` / `no_permission` / `rate_limited` / `error` / `disabled`（用户在设置里关掉了，`enabled` 为 false）；`via` 说登录从哪来（「来自 <机器> 的 gh 登录」）；`error` 是给人看的一句话，不含密钥；`capabilities` 取 `status` / `tools` / `actions` / `events` / `hosts` / `inbox` / `archive` / `login`；`hosts` 是这个连接器的链接所在的主机名（App 用来认链接）；`archive` / `inbox` 是「进知识库」「进收件箱」开没开，只有有 `archive` / `inbox` 能力的才带 |
| POST | `/api/v1/connectors/{id}/login` | 分步登录（有 `login` 能力的，`docs/archive/plan-telegram.md`）：`{"step": "", "values": {}}`，`step` 为空是从头开始，之后带上一步的 `step` 和填的字段 → `{step?, fields?:[{name, label, secret?, placeholder?}], hint?, error?, done?, account?, kind?, url?, user_code?, interval?}`：下一步要填的，或 `done`。
`kind` 是 `device_code` 时这一步不填字段：显示 `user_code`，让用户去 `url` 输入，App 每隔 `interval` 秒用同一个 `step`（不带值）再问一次，直到回来别的步骤或 `done`（GitHub 的设备码登录）。
`kind` 是 `qr` 时同样不填字段、同样轮询：`url` 是要用别的 App 扫的内容（微信），hub 附上它的二维码点阵 `qr: {size, rows}`（`rows` 一行一串 `0` / `1`，不含安静区），App 照着画；旧 App 不认识这个 `kind`，只显示 `hint`。填错了（验证码不对）是同一步带 `error`，表单留着重填；别的错 502。填的值只给连接器，不进日志和任何响应；登录得到的会话加密存 hub（`connector_secrets`） |
| GET | `/api/v1/connectors/github/auth` | GitHub 的登录从哪里来：`{source: "machine" \| "device", machine?, machines: [{id, name, state, account?, scopes?, missing?, error?}], scopes?, scopes_known, missing?: [{scope, affects}]}`。`state` 是 `signed_in` / `not_signed_in` / `no_gh` / `too_old` / `offline` / `error`（hub 当场问每台在线电脑的 gh，再问 GitHub 这个 token 是谁、有哪些 scope；token 不出 hub）；`missing` 是用的 token 少了的 scope 和受影响的功能 |
| PUT | `/api/v1/connectors/github/auth` | `{source: "machine", machine: "<id>"}` 或 `{source: "device"}`：换登录来源，存在 hub 设置里（盖过 hub.json 的 `github.auth.from_machine`），回同上；选电脑时忘掉设备码登录的 token |
| POST | `/api/v1/connectors/{id}/logout` | 退出登录：hub 删掉存着的凭据和登录状态；对方服务里的会话是否同时结束取决于连接器：Telegram 会结束其设备会话，其他连接器一般只让 hub 忘掉自己存的那份（如 GitHub 的设备码登录），要到对方那里撤销授权 → `{"ok": true}` |
| PUT | `/api/v1/connectors/{id}/settings` | `{"enabled"?: bool, "options"?: {…}, "archive"?: bool, "inbox"?: bool}`：`options` 是连接器自己的设置（有 `options` 能力的，GET 里也带着；GitHub 的是 `{ci, muted_repos, ignore_branches}`，`docs/inbox.md`「降噪」），不合法 400，存 hub、重启照旧，改完立刻按新设置收一轮收件箱。`enabled` 是设置 › 连接器 的开关。关掉的连接器不跑、不给工具、不收收件箱、不进知识库，状态是 `disabled`，登录和退出照常；选择存在 hub 的设置里，重启后照旧；没动过开关的连接器是开着的，Telegram 这种要用户自己填凭据的默认关着。`archive` 是「进知识库」：关掉后 hub 不再问它要归档的变化（一个请求都不发），收件箱和助手的工具照常；`inbox` 是「进收件箱」：关掉后不再收新的，已有的条目不动；两个默认都开，没有这个能力的连接器改它回 400，选择存在 hub 的设置里 → 这个连接器的一项（同 `GET`） |
| DELETE | `/api/v1/connectors/{id}/data` | 删除已存的数据：先关掉这个连接器（开关存成关，不然下一轮又同步回来），再删掉 hub 上存下的它的东西：知识库里它的会话和人（连同索引、项目里的引用和它们写的时间线、归档游标）、收件箱里它的条目 → `{chats, people, inbox}`。和退出登录分开（`/logout`）；连接器那边的账号和消息不受影响 |
| GET | `/api/v1/connectors/{id}/thread?thread=&before=&limit=` | 有 `threads` 能力的连接器：收件箱一个会话（条目的 `connector.thread`）的一页消息 → `{chat: {id, name, kind, url?, avatar?, members?, reply?, state?, author?, labels?, branch?, body?, body_format?}, messages: [{id, at, sender: {id?, name, avatar?}, mine?, event?, text?, format?, media?: [{kind, ref?, thumb?, mime?, name?, size?, duration?, width?, height?, emoji?}], reply_to?: {id, from?, text?}, edited?, url?, kind?, review?, code?, check?}], before?}`，旧的在前；`before` 往前翻一页，没有就到头了；`limit` 默认 30、最多 50。`chat.members` 是群的成员数 / 频道的订阅数，只在第一页（不带 `before`）给，不知道时不给（Telegram 见 `docs/archive/plan-telegram.md`「人数」）。没登录 409，限流中 429（`code: rate_limited`，`error` 写明还要多久） |
| GET | `/api/v1/connectors/{id}/media?ref=` | 页面里 `avatar` / `ref` / `thumb` 对应的字节（图片、贴纸、视频缩略图、头像），`Content-Type` 照原样，`Cache-Control: immutable`；hub 存在 `<hub 目录>/cache/connector-media/`（最多 256 MB，旧的先删），同一个 ref 同时有很多请求也只向连接器要一次 |
| POST | `/api/v1/connectors/{id}/thread/reply` | `{thread, action, text, reply_to?}`：用页面 `chat.reply` 给的 action 在会话里回复（参数 `{chat, text, reply_to}`）→ `{ok, result}`。用户自己的设备（full 设备 token）发来的直接做，不过 `connector_gate`（#1190）；配对 token 等其他身份照旧过闸门，被拒 403（`code: declined`）。限流 429 |
| DELETE | `/api/v1/connectors/{id}/archive` | 删除已存的知识库内容：先关掉「进知识库」（存成关，不然下一轮又归档回来），再删掉知识库里它的会话和人（连同索引、项目里的引用和它们写的时间线、归档游标）→ `{chats, people, inbox: 0}`。连接器照常跑，收件箱和工具不受影响；没有 `archive` 能力的回 409 |

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
// 新增时额外字段：password。provider 为 feishu 时忽略传入的服务器，hub 同时试 imap/smtp.larkoffice.com 和 imap/smtp.feishu.cn（993 / 465，SSL），保存先登录成功的那组（docs/mail.md「飞书邮箱」）
// MailSummary
{"account_id","mailbox","uid":123,"message_id","subject","from":{"name","email"},"to":[{"name","email"}],
 "date","snippet","seen":false,"flagged":false,"has_attachments":false}
// MailMessage = MailSummary + {"cc":[…],"text":"…","html":"…","attachments":[{"id","filename","mime","size"}],
//                               "in_reply_to","references":[…]}
```

## 收件箱 API

hub 后台采集飞书、邮件和各个连接器，交给管理者（助手）聚合成「件」（`docs/inbox.md`「谁管什么」：条目在 `server/internal/assistant/inbox.go`，来源在 `server/internal/hub/inbox.go`），这些接口经 hub 转给它，管理者没连着时回 `503 assistant_offline`。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/inbox` | `{items:[InboxItem], triage}`：未处理的 + 3 天内处理过的，按 `updated_at` 倒序；不含 `context`。默认只有飞书和邮件；带 `?sources=all` 才有连接器（GitHub…）的条目（带 `connector` 块的），旧 App、手表、Android 不带，所以不会把 PR 当邮件画。`triage`：`{state, reason?, waiting, sources?:[{source, name, state, reason, waiting}]}`，插件连接器的新条目里因为分拣不了（又没有兜底）或这一轮没轮到、还没进收件箱的有几件、为什么；没有时 `{"state":"ok","waiting":0}`（`docs/inbox.md`「AI 分拣」，旧 hub 没有） |
| GET | `/api/v1/inbox/{id}` | 一件，带 `context`（这段对话 / 邮件的纯文本，给 AI 用）；邮件额外带 `reply_target:{uid,version}` |
| POST | `/api/v1/inbox/{id}/done` | `{done: bool}`：标已处理 / 移回，返回这件 |
| POST | `/api/v1/inbox/done` | `{done: bool, items:[{id, version?}]}` → `{changed:[InboxItem], unchanged:[id], skipped:[{id, reason}]}`：一次标多件（全部标为已处理、全部看过 / 全部清除、它们的撤销），一次写库、一次推 `inbox`。`version` 是列表里这件的 `version`：标已处理时不一致（列出以后又有新消息并进来）就不标，`skipped` 里 `reason=changed`；没有这件 `reason=missing`；已经是这个状态的放进 `unchanged`、不改 `done_at`（重试、另一台设备同时点都不会重复写）。移回不比版本。最多 1000 件，多了 413；没有 `done` 400。只记在 Persona，不回写来源的已读（`docs/inbox.md`「全部标为已处理」）；旧 hub 没有这个接口（405），App 退回逐件 `/{id}/done` |
| GET | `/api/v1/inbox/mutes` | `{mutes:[{id, source, source_name, tag, value, name, created_at}]}`：用户的静音（`tag` 是 `repo` / `chat` / `sender`，`docs/inbox.md`「静音」）。列表里每件事带 `mutes:[{tag, value, name}]`，说它能按什么静音（旧 hub 没有） |
| POST | `/api/v1/inbox/mutes` | `{item_id, tag}` → `{mute, settled}`：静音这件事在 `tag` 下的值（hub 从这件事上读），同一来源里被覆盖的打开条目收掉（`settled` 件），以后的进来就收掉；同一个静音只有一份 |
| DELETE | `/api/v1/inbox/mutes/{id}` | 取消静音：以后的照常进来，已经收掉的不回来 |
| POST | `/api/v1/inbox/{id}/draft` | `{instruction?}` → `{text}`：按用户口吻起草的回复（不发送） |
| POST | `/api/v1/inbox/{id}/reply` | `{text, changes?, client_id?, expected_uid?, expected_version?}`（发到一半和助手断了：502 `assistant_in_doubt`、`delivered:"unknown"`，可能已经发出去，同一条再发不会发两次）：飞书发到原会话 / 邮件按回复发出 / 云文档发成评论（`doc.reply` 指的地方），并标为已处理（`done_by=replied`），返回这件。云文档的 `changes:[{ref,field,value}]` 先改状态再评论；只改状态时 `text` 可以为空。提供 `client_id` 时保存回执，飞书原样透传为上游 `clientId`；不提供时 `clientId` 由这件的 id 和文字算出（UUIDv5），hub 重启后重试也不会重复。飞书的同一件只发一次：同一件的回复排队处理；已经因回复处理掉、文字又相同的直接返回成功，不再发。文字不同照常发。飞书发到哪：私聊发进会话；`lark.thread_kind` 非空时回复 `reply_to` 并落进它的话题；群里 @我（`kind=mention`）的引用 `reply_to` 回复；其它群和没有 `reply_to` 的旧数据照旧发进会话 |
| POST | `/api/v1/inbox/{id}/doc/status` | `{ref, field, value}`：把云文档里这一行的状态改成 `value`（必须是这一列的可选值），先读最新的再写（别人刚改过就 409 失败），返回重新读过的这件 |
| POST | `/api/v1/inbox/refresh` | 立刻采集一次，返回列表和 `triage`（`?sources=all` 同上）；插件连接器的新条目在后台分拣完才进列表，不等它 |
| POST | `/api/v1/translate` | `{texts:[string]≤30, target}` → `{texts}`，顺序不变，模型没给的是空串；target 是 BCP 47（zh-Hans、en、ja…，`docs/ios.md`「翻译」）。和收件箱一样是助手的（经 hub 转），用模型配置的「翻译」用途 |
| POST | `/api/v1/translate/mail` | `{account_id, uid, mailbox, target}` → `{subject, text, clipped?, model_fingerprint?}`：助手请 hub 读这封信（只有 HTML 的转成保留段落和链接的文字），自己翻；收件箱已经用同一个模型翻好的直接给；正文最多 12000 字 |

邮件回复可带详情返回的 `expected_uid` 和／或 `expected_version`。版本绑定账号、邮箱、线程中的 UID、消息标识及发件人；目标变化返回 HTTP 409，`code=mail_reply_target_changed`、`delivered=none`、`retryable=false`，此次没有发送。App 保留草稿，加载最新邮件并让用户确认新收件人后，用新的 `client_id` 发起新请求。网络结果不明时沿用原请求、原目标和原 `client_id` 核对回执。省略校验字段时沿用旧行为，现有 iOS 无需修改；持久回执规则不变。

`InboxItem`：`id`、`source`（lark / mail，或连接器的 id：github）、`kind`（dm / mention / group / bot / mail；连接器的是它自己的：review_requested / mention / team_mention / author / ci_failed / assign）、`account`（`feishu` 或邮箱账号 id）、
`account_name`、`title`（会话名 / 邮件主题）、`sender`、`avatar`、`preview`（最新一条）、`count`、`started_at`、`updated_at`、
`tier`（action / fyi / noise，分拣前为空）、`summary`、`reason`、`read`（在别处看过）、`done_at`、`done_by`（user / read / replied）、`version`（这件现在装着什么，有新消息并进来就变；批量已处理时带回，旧 hub 没有）、
`lark{account_id,ol_account,feed_id,is_group,is_topic,start_position,read_position,mention_position,at_all,reply_to,thread_kind}`（`account_id`：会话所在的 lark-connector 账号编号，旧记录为 0 = 账号 1；`ol_account`：不是默认账号时有，打开会话、调飞书接口要带的 `ol_account`；`read_position`：这个会话在飞书里的已读位置，每次采集都刷新；`reply_to`：最后一条别人发的消息的 id，`thread_kind`：它在哪，空 = 在会话里、`topic` = 话题群的话题、`thread` = 普通群里的话题回复，这两个在采到新消息时刷新，旧数据没有）、`mail{account_id,mailbox,uids,message_id,from,direct,thread}`、
`translations{<目标语言>:{subject,preview}}`（hub 提前翻好的外文邮件，`docs/ios.md`「翻译」）、`doc{url,link,type(docx/doc/sheet/base/other),notice(mention/comment/reply),notice_id,rows:[{ref,table,title,fields:[{name,value}],status:[{field,name,value,options:[{name,color}]}]}],reply{kind(thread/record/cell/doc),label,…},error}`
（`kind=doc`，`docs/inbox.md`「云文档」）；`agent.changes:[{ref,field,value}]` 是 AI 分身随草稿提议的改状态；
`connector{thread,label,url,open_url,reply_to,can_reply,rule_tier}`：连接器的一件（`docs/inbox.md`「连接器：GitHub」），App 只画这一个块、不认识具体来源：
`label` 是行上的标签，`url` 在 App 内打开，`open_url` 交给系统打开（`sms:` 这类），`reply_to` 说回复会发到哪、谁能看到，`can_reply` 为假时不能回复（CI 失败）。
列表变化时 WebSocket 推 `{"type":"inbox"}`（不带内容，App 自己拉）。

## 飞书机器人

`/api/v1/bots/feishu`（读状态 / 改凭证、开关和功能）、`…/pair`（配对码）、`…/owner`（DELETE 解除配对）、`…/test`（测试消息），字段见 `docs/bots.md`「配置和接口」。

## 知识库 API

知识库是管理者（助手）的，这些接口 hub 原样转给它（上面「总线」的转发）：它没连着时回 `503 assistant_offline`。知识库未启用时返回 503；`kind` 支持 `projects`、`chats`、`people`、`docs`、`meetings`、`events`。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/kb/{kind}?tag=` | `{items,tags}`；可按标签筛选 |
| GET | `/api/v1/kb/{kind}/{id}` | `{detail,agents?}`；项目可额外返回相关在线 Agent |
| PUT | `/api/v1/kb/{kind}/{id}` | body 为 `kb.Update`，可更新名称、标签、重要性、摘要及项目属性；返回 `{id}`，字段见 [knowledge.md](knowledge.md) |
| GET | `/api/v1/kb-search?q=&messages=0` | `{hits}`；默认包含消息，`messages=0` 排除消息 |

## 飞书 API（lark-connector）

hub 经 hub 机器上 lark-connector 的 agent 接口（`/agent/v1`，token 从容器环境变量读）代理飞书数据，见 `server/internal/lark`。

下面的路由都认查询参数 `ol_account=<lark-connector 账号编号>`：这次请求是哪个飞书账号的（不合法回 400）；不带就是 hub.json 的 `lark.account`（`docs/feishu.md`「多账号」）。hub 交出的别的账号的媒体 URL 已经带着它。

lark-connector 出错时这些路由回 502 `{"error"}`；其中 lark-connector 自己丢了飞书登录（`AUTH_*`，`lark.ErrNotLoggedIn`）的，回 `{"error","code":"lark_not_logged_in"}`，App 据此提示去 lark-connector 重新登录飞书。`/raw/…` 例外，原样转发 lark-connector 自己的回答。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/lark/accounts` | `{"accounts":[{"id","name"?,"avatar"?,"tenant"?,"user_id"?,"signed_in","primary"?,"unread","quarantined"?}],"default"}`：lark-connector 上登录的飞书账号（按 lark-connector 的顺序，`signed_in` 对被隔离的账号为 false）和默认账号的编号。没有账号列表的旧 lark-connector 只回默认账号；旧 hub 没有这条路由（404），App 不显示账号切换 |
| GET | `/api/v1/lark/status` | `{"configured","connected","name","error","last_event_at"}`：设置 › 连接器 › 飞书。configured = hub.json 有 lark；connected 每次现问 lark-connector 的 `/accounts`；last_event_at 最近一条实时事件（Unix 秒） |
| GET | `/api/v1/lark/message-display` | `{"account_id","anti_recall","show_edit_history"}`：当前选中飞书账号的展示设置；只返回这两个开关和实际用户 ID，不返回脚本/自动发送配置 |
| POST | `/api/v1/lark/message-display` | `{"account_id","key","value","expected"}`：key 为 `anti_recall` 或 `show_edit_history`，value/expected 为布尔值；完整设备权限，body ≤1 KiB，未知字段拒绝；通过 lark-connector 受限 PATCH 保留其他字段。账号/旧值冲突 409，未授权 403，旧服务无接口 404；登录失效 502 + `lark_not_logged_in`。成功返回与 GET 相同结构并推送带 `settings_changed=true` 的 `lark` 事件，不自动重试未知结果 |
| GET | `/api/v1/lark/encryption` | 原生加密状态 `{accountId,configured,locked,unifiedPassword}`。须有独立 native OLK 授权；不返回密钥或凭据，`Cache-Control: no-store`。Hub 声明 `lark_native_encryption`；上游缺少原生接口返回 426 + `lark_lark-connector_upgrade_required`，独立授权缺失返回 403 + `OLK_VAULT_NATIVE_NOT_AUTHORIZED` |
| POST | `/api/v1/lark/encryption/unlock` | `{account_id,password}` → 同上。只允许完整写权限；按实际飞书账号校验，共享网页的密码尝试限制；密码不存储 |
| POST | `/api/v1/lark/conversations/{id}/encryption/covers` | `{account_id}` → `{lines:[string]}`；候选历史外显复用 lark-connector 网页规则 |
| POST | `/api/v1/lark/conversations/{id}/encryption/prepare` | `{account_id,text,cover}` → `{accountId,feedId,messages:[{text}]}`。只生成带外显的密文，不发送；身份不一致、未授权、未解锁或无收件公钥时拒绝 |
| POST | `/api/v1/lark/conversations/{id}/encrypted-messages` | `{account_id,ciphertext,client_id,reply_id?}` → `{ok,message_id?}`。必需稳定 UUID，仅接受 OLK2/OLK3 wire shape，重新校验原生授权与实际账号；上游写入携带 `expectedSenderId`。多段按顺序发送，仅最后一段可引用；每段单独保存去重回执。所有原生加密响应 `no-store`，失败不会改走普通文字接口；协议与限制见 [加密消息](lark-encryption.md) |
| GET | `/api/v1/lark/conversations?scope=&cursor=&limit=` | `{"conversations":[LarkConversation],"next_cursor"}`；scope：`chats`（默认，飞书「消息」：不含已完成 / 折叠，按标签显示规则过滤）、`unread`、`done`（已完成）、`label:<id>`、`inbox`、`mentions`、`muted`、`p2p`、`group`、`thread`、`later` |
| GET | `/api/v1/lark/pinned` | 全部置顶会话（持续读取 inbox + done 的置顶头部，合并去重后按置顶时间排序）；分页异常返回错误，不把截断集合当作成功结果 |
| GET | 上述 `/conversations`、`/pinned` 加 `account_bound=1` | 额外返回实际选中账号 `account_id`，集合为空时为 `[]`，禁止中间缓存。可带预期 `account_id`；请求前后身份变化或预期不匹配返回 409 `lark_account_changed`，不带会话数据。未登录沿用 `lark_not_logged_in`；旧调用不加参数，保持原契约 |
| GET | `/api/v1/lark/conversations/{id}/pins?account_id=&cursor=&limit=` | 聊天内 Pin 列表：`{account_id,items:[{id,message_id,message?:LarkMessage,operator_name?,created_at}],next_cursor,total}`。limit 默认 50、最大 100。完整保留上游分页，重复/缺失游标返回错误；此读取会清除 lark-connector 的 Pin 未读标记，禁止首页预取 |
| GET | `/api/v1/lark/conversations/{id}/pin-messages/{message}?account_id=&topic=` | 定位 Pin：`{account_id,target:{message,root?,page}}`，page 与聊天/话题分页同结构。普通消息返回 around 窗口；回复返回话题根和包含目标的回复窗口（最多 20×100 条）；没有找到目标返回 409 `lark_pin_no_position`，客户端保留当前窗口 |
| POST | `/api/v1/lark/conversations/{id}/pins` | `{account_id,message_id,pin_id?,remove}` → `{account_id,pinned}`。remove=true 时必须有对应 pin_id；写前核实消息 feed、capabilities、Pin 归属和实际账号。未知结果返回 `lark_pin_result_unknown`，必须重新读取确认，不自动重试。三条 Pin API 均 `no-store`，换号 409 `lark_account_changed` 不带数据；旧 lark-connector 路由 426 `lark_lark-connector_upgrade_required`，不存在的原消息为 `lark_pin_not_found` |
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
| POST | `/api/v1/lark/documents/comments` | `{url, content, mentions?:[{user_id,name}], quote?, block_id?, client_id}`：以用户本人在文档上发一条新评论（#1290）。没有 `quote` 是全文评论，有 `quote`（和它所在的块 `block_id`）是对那段正文的评论；`content` 里每个被 @ 的人写成 `@名字`，和发消息一样。回 `{ok, threads, anchored?}`：`threads` 是写完后读回的全部线程（同 `documents/comments` 的形状，读不回时为 null，评论已经发出），对一段正文的评论另带 `anchored`（`false`：找不到那段所在的块，改成在全文评论里引用那段话）。正文锚点走 lark-connector 的 inline 评论，块由插件按 `quote` 在文档的 JSON 里找（App 知道 `block_id` 时直接带上）。插件声明 `lark_doc_comments` 才有这几条路由；是用户自己设备上的写，不过 AI 的闸门；写进回执（`client_id` 派生回执 id，同一个 id 重试不会发两次） |
| POST | `/api/v1/lark/documents/comments/{thread}/replies` | `{url, content, mentions?, client_id}`：回复这条评论线程，回 `{ok, threads}` |
| POST | `/api/v1/lark/documents/comments/{thread}/resolve` | `{url, resolved}`：标为已解决（`true`）或重新打开（`false`），回 `{ok, threads}`；内容不对 400 `comment_invalid`、这种文档不能这样评论 400 `comment_unsupported` |
| GET | `/api/v1/lark/documents/people?url=&q=` | `{members:[{user_id,name,avatar?}]}`：评论里 @ 的候选人，按名字找（和群的 `members` 同一个形状） |
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
 "recalled","retained"?,"edited","reply_count","thread_reply_count","badge_count","at_me","is_read",
 "parts":[MessagePart],                       // lark-connector 原样（见下），媒体 URL 已改写到 /api/v1/lark/
 "olk"?:{…},                                 // 加密投影：状态、公开外显、授权解密正文、密钥分发信息
 "edit_history"?:[{"edited_at","text","parts"?,"olk"?}],
 "reply_to":{"id","sender_name","text","recalled","retained"?,"parts","olk"?},
 "reactions":[{"key":"THUMBSUP","count":2,"mine":true,"users":["示例成员甲","示例成员乙"]}]}
// MessagePart（lark-connector 的 MessagePart，不改字段名）
{"kind":"text|at|link|image|file|code|emotion|media|card|docs|interactive|table",
 "text","url","refId","metadata":{…}}   // 卡片的整棵元素树在 metadata.body / header；富文本块祖先在 metadata.blocks
```

附件发送要求完整设备权限（answer-only 不可用），共用 Hub 绑定的 lark-connector 账号；不接受调用方覆盖账号。Hub 在 hello/health 中声明 `lark_attachments`。
`client_id` 必须为 UUID，同一消息重试保持不变；消息读取额外保留可选 `client_id`、`local_state`，用于消除发送超时后的迟到回显。`local:` 和 pending/failed 副本不算发送确认。

媒体改写规则：parts JSON 里所有 `"/api/media/` → `"/api/v1/lark/media/`，`"/api/messages/` → `"/api/v1/lark/messages/`
（合并转发卡片里嵌套的消息也一并改写）。

`retained=true` 表示 lark-connector 已留存撤回原内容，不是恢复权限；展示时仍保留 `recalled`，不能据此开放回复、表情或卡片写操作。两个展示开关不删除服务端留存，也不限制原有消息读取权限；客户端在设置关闭/未知时隐藏撤回原文和编辑历史入口。历史、引用、转发的 OLK 投影沿用当前授权，锁定时只显示公开内容，手机缓存清除解密正文。

## 个人 Agent 来源状态

未配置 lark-connector（包括仅配置飞书 CLI）时，`GET /api/v1/agents` 返回 `{"sources":[]}`，`GET /api/v1/agents/sessions` 返回 `{"sessions":[]}`，均为 HTTP 200。
配置了 lark-connector 时，`GET /api/v1/agents` 返回 `{"sources":[{"source":"removedbridge","enabled","error?","setup?","providers":[…],"accounts":[…]}]}`。
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
| `<cross-session-message from-name="…">…</cross-session-message>` | Claude Code（会话间消息） | 左侧「来自 名字」卡片，正文按 Markdown 显示 |

## Persona 注入的消息

hub 会以「用户输入」的身份往对话里发一些给模型看的消息（#2360、#2358）。transcript 里它们和用户打的字一样是 `role:"user", kind:"text"`，
但不是用户写的，**客户端不画成用户气泡，也不显示里面给模型的说明**。模型收到的文字不变（模板在 `server/internal/prompts/`），
识别全在下发时做：hub 按模板认出这一行（`prompts.ParseInjected`，和渲染互测），给 item 补上 `injected`。分页（`/items`）、推送（`items` 事件、订阅时的 backlog）、
`/items/{iid}/full` 都带；按文本认，所以老消息、旧 node 写的也带。App 自己带 `client_msg_id` 发的那行永远不认。

```jsonc
"injected": {
  "kind": "task_notice|followup|system",
  "state": "…",            // 见下表
  "title": "README 去掉扩展列表", // 任务名 / 定时任务的标题 / 叫醒时在等的事；可能空
  "machine": "<电脑名>",  // 仅 task_notice：Agent 在哪台电脑（名字，可能空）
  "todo_id": "td_…",        // 仅 task_notice：hub 认得出是哪个任务时（任务还开着、标题唯一）；可能空，客户端再按标题去找同一对话的任务卡
  "line": "任务「README 去掉扩展列表」做完了", // 给人看的一句话，直接显示
  "detail": "…"             // 仅 followup 的 once / daily / interval：任务本身的要求（用户在定时任务里写的那段），可以折叠着给人看；其余为空
}
```

| 来源（模板） | 文字的开头 | `kind` | `state` | `line` |
| --- | --- | --- | --- | --- |
| 主对话派出的安静任务有了结果（`assistant/task_notice.md`） | `🔔 任务「标题」做完了（电脑）`，第二或第三行是「（这是 Persona 的任务通知，……」 | `task_notice` | `done` / `needs` / `interrupted` / `failed` | 任务「x」做完了 / 在等你 / 被打断了 / 没开起来 |
| 定时任务、跟进到时间（`assistant/task_run.md`） | `⏰ 标题（你之前安排的跟进）` / `（定时任务）` / `（定时任务，每 30 分钟）` | `followup` | `once` / `daily` / `interval` | 跟进「x」到时间了 / 定时任务「x」开始运行 |
| 叫醒等待中的 Agent（`agents/wake.md`，Agents 的对话） | `⏰（Persona 叫醒你，第 N 次）` | `followup` | `wake` | Persona 叫醒了它：看看「等的事」好了没有 |
| node 重启丢了后台任务（`assistant/lost_tasks.md`） | `⚠️ 运行你的 node 刚重启过` | `system` | `lost_tasks` | node 重启过，N 个后台任务停了 |

**怎么显示**（各端一样）：

- 画成居中的一行轻量系统提示：图标 + `line`，不是右侧的用户气泡，不露出原文。`task_notice` 点一下打开任务（`todo_id`，没有就按 `title` 找这段对话的任务卡 / 待办；都找不到就不可点）；
  `followup` 的 once / daily / interval 可以点开看 `detail`；其余不可点。
- 主对话里同一个任务的卡片（回执、任务卡）已经在上方时，这一行可以并进卡片，不再单独画；绝不重复 Agent 最后说的话（卡片上有）。
- 不算用户发的消息：不当成一轮的开头去配对待发气泡（`client_msg_id` / 按字认）、不进「你说的」类统计；对话列表的预览、搜索结果、复制「用户的话」时用 `line`，不用原文。
- 队列里还没发进去的任务通知，`sessions` 的 `queued` 里本来就只写「任务「x」的通知」。

**旧 hub 没有 `injected` 时**按文字认（只认 `role:"user", kind:"text"`、没有 `client_msg_id` 的行；先去掉开头的空白和 `<jarvis-context>` 快照块）：

- `task_notice`：第一行匹配 `^🔔 任务「(.+)」(做完了|在等用户|被模型服务出错打断了|没开起来)(?:（(.+)）)?$`，且全文含「这是 Persona 的任务通知」。
  第 2 组映射 `done` / `needs` / `interrupted` / `failed`，第 3 组是 `machine`。
- `followup`：第一行匹配 `^⏰ (.+)（(你之前安排的跟进|定时任务(?:，每 [^）]+)?)）$`：「你之前安排的跟进」是 `once`，「定时任务，每 …」是 `interval`，「定时任务」是 `daily`；
  `detail` 是第一个空行之后的正文（去掉末尾以「（知识库」开头的那段提示）。第一行以 `⏰（Persona 叫醒你，第` 开头的是 `wake`，`title` 取「在等「…」」里的那段。
- `system`：以 `⚠️ 运行你的 node 刚重启过` 开头；`- ` 开头的行数是停掉的任务数。
- 认不出的照旧是用户气泡（宁可漏认，不把用户的话藏起来）。
- 共用夹具 `spec/fixtures/injected/cases.json`（Go 测试 `TestParseInjected` 按模板生成，`UPDATE_GOLDEN=1` 重写）：`input.text` → `expected`（`null` 是不认）；各端的兜底识别都跑这一份。

别的「不是用户写的」消息已经有各自的形状，不归这里：别的对话发来的（`<jarvis-context title="来自对话「x」">`）、接上旧主对话的背景（`<jarvis-context title="接上旧主对话">`）
按上面的标签收成「上下文」；后台任务的通知是 `kind:"notice"` + `task`；`/model`、压缩等是 `kind:"notice"`。

