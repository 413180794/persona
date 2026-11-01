# 服务端 jarvisd

Go，单一二进制，`server/cmd/jarvisd`。模式见 `docs/architecture.md`，协议见 `docs/protocol.md`。

## 代码

| 包 | 内容 |
| --- | --- |
| `internal/proto` | 线上类型 |
| `internal/rpc` | hub↔node 的双向 JSON-lines RPC |
| `internal/transcript` | Claude Code JSONL → `proto.Item`，增量读取、分页 |
| `internal/herdr` | herdr 的 socket API 客户端（CLI 兜底）和事件订阅 |
| `internal/herdrcore` | herdr-core（Herdr 桌面版背后的 `hc`）的 JSON-RPC 客户端和事件订阅，见 `docs/plan-orca-support.md`「herdr-core 宿主」 |
| `internal/node` | node：会话列表、启动、发送、屏幕采样、hook / mcp 的 unix socket |
| `internal/hub` | hub：HTTP / WebSocket、机器连接、交互队列、手机工具转发、APNs |
| `internal/db` | Postgres：连接池、内嵌 SQL 迁移（`migrations/NNNN_*.sql`）、各表读写 |
| `internal/mail` | IMAP/SMTP、加密账号存储、邮件 HTTP 接口和 MCP 工具（`docs/mail.md`） |
| `internal/mcpserver` | `jarvisd mcp`：给 claude 的 stdio MCP server |
| `internal/tunnel` | tailcat：node 的 key 文件和端点、hub 的拨号、经 tailcat 收发新二进制（下面「tailcat 打洞」） |

## 和 herdr 的连接

- node 直连 herdr 的 unix socket（`$HERDR_SOCKET_PATH`，否则 `$XDG_CONFIG_HOME/herdr/herdr.sock`，默认 `~/.config/herdr/herdr.sock`）：
  每次调用新开一条连接，写一行 `{"id","method","params"}`，读一行回复（单行上限 64 MB，超时 10 秒）。herdr CLI 只是同一个 API 的
  薄客户端（每次还要先 ping 一次），结果结构完全一样；socket 连不上或 herdr 太旧不认识方法（`invalid_request` + `unknown variant`）时
  退回 CLI。方法名以 `herdr api schema --json` 为准（`pane run` 对应 `pane.send_input` + `keys:["Enter"]`；`pane.process_info` 必须传
  `pane_id`，传 target 会回答当前聚焦的窗格）。
- `events.subscribe` 一条长连接：生命周期事件（workspace / tab / pane 的增删改）加每个 Agent 窗格的 `pane.agent_status_changed`
  （herdr 只按窗格订阅状态）。任何事件都让 `pollAgents` 马上取一次 snapshot（50ms 合并一串事件）；平时 10 秒兜底一次。
  事件流没连上，或有 TRAEX 窗格（它的状态在标题的转圈字符里，只变转圈不发 `pane.updated`）时仍每秒一次。
- 订阅的坑：同一条连接第二次 subscribe 会被静默忽略，窗格集合变了要带全集重开连接；herdr 会关掉跟不上的订阅者，`events_lost`
  不一定送得到，以连接关闭为准重连（指数退避到 30 秒），重连后先取一次 snapshot 补齐。推来的事件名用下划线（`tab_created`），
  订阅时的类型用点（`tab.created`）。
- 本机实测（herdr 0.9.3，7 个 Agent）：稳定时 30 秒内零事件；tab 改名到 `sessions.changed` 发出 80–140ms（原来最多 1 秒）。
  联调：`./scripts/test-herdr.sh`（独立的 herdr 会话，见 `docs/testing.md`「herdr 联调」）。

## 和 herdr-core 的连接

每台机器的 node 只用一个引擎（`node/engine.go`）：herdr（默认）、herdr-core（`node/hostherdrcore.go`）或 Orca（`node/hostorca.go`，下面「和 Orca 的连接」），用户在 App 里按机器选，
hub 连上时用 `engine.use` 告诉 node（`docs/protocol.md`「引擎」）。用 herdr 时 node 和原来完全一样，不碰 herdr-core（只有 `node.info`
问一次它在不在）。选了 herdr-core 而它没开时，立刻答「没有终端」，不拨号、不记日志。socket 位置、方法、映射和第一期不支持的部分见
`docs/plan-orca-support.md`「herdr-core 宿主」。
联调：`go test -tags hclive -run HerdrCoreLive ./internal/node/ -v`（自己起一个独立的 herdr-core，再只读地列一遍默认实例）。

## 和 Orca 的连接

选了 Orca 的机器，node 经 Orca 桌面版 runtime 的 unix socket 驱动它的终端（客户端 `internal/orcart`，宿主 `node/hostorca.go`）：
读 `orca-runtime.json` 拿 socket 和 token（Orca 每次启动换 token，连不上或 `unauthorized` 时重读），先 `status.get` 查协议版本。
socket 上没有推送，node 每秒轮询一次 `terminal.list` + `worktree.ps`；窗格对应会话靠 claude 进程环境里的 `ORCA_PANE_KEY`。
Orca 没开时立刻答「没有终端」，不记日志。映射和第一期不支持的部分见 `docs/plan-orca-support.md`「Orca 来源」。
联调（只读）：`go test -tags orcalive -run OrcaLive ./internal/node/ -v`。

## 部署

```sh
./scripts/deploy.sh                    # 编译 linux/amd64 + darwin/arm64，装到所有机器，重启 hub
./scripts/deploy.sh <机器 id> <机器 id> # 只装部分机器
```

- 二进制：每台机器 `~/.jarvis/bin/jarvisd`。node 由 hub 经 `ssh <别名> ~/.jarvis/bin/jarvisd node --stdio` 拉起，Mac 上不用常驻服务。
- hub：hub 机器上的 `systemd --user` 服务 `jarvisd`，监听 `:18660`。`journalctl --user -u jarvisd -f` 看日志。
- 换了 node 二进制后 hub 不会自动重连旧连接；`deploy.sh <hub 机器 id>` 会重启 hub，或在 App「机器」页点「重新连接」。

### 轻量 hub 与部署前检查

轻量 hub（例如 2 核 / 2 GB）可以只运行 hub 和 Postgres，把 Agent 放在其他执行机器上。初次部署尚未接入执行机器时，
在 `hub.json` 显式写 `"machines": []`，避免省略后加载默认本机 node；`inbox.agent.enabled` 和 `kb.enabled` 可先设为 false，
接入数据源后再按需开启。以上是部署方式建议，尚无硬件最低配置的压测结论。

Linux 的 systemd 用户服务要在退出 SSH 后持续运行，需由管理员为部署用户执行 `loginctl enable-linger <用户>`。
安装 Docker 后先验证 `postgres:18-alpine` 能拉取；系统软件源可访问不代表 Docker Hub 可访问。拉取失败时不要把数据库初始化
或 hub 部署标记为完成，也不要输出 `hub.json` 中的 token 和数据库连接串。
国内网络若无法访问 Docker Hub，可尝试 AWS 公共仓库的 Docker 官方镜像
`public.ecr.aws/docker/library/postgres:18-alpine`，拉取成功后重新标记为 `postgres:18-alpine` 再运行数据库脚本。

公网入口使用独立域名的 HTTPS 反向代理，hub 和数据库只监听回环地址。Nginx 要转发 WebSocket 的 Upgrade / Connection，
关闭响应缓冲并延长读取超时；访问日志不要记录带 token 的查询字符串。重载 Nginx 后立即请求可能仍命中旧 worker，
应等待新配置生效后再验证证书、鉴权接口和 WSS 101。通过云控制台申请的免费证书需要在到期前重新申请并安装，
不要将已有 certbot 定时器视为这类证书的自动续期。

Windows PowerShell 通过管道传 shell 脚本时，末尾可能额外带 CR，导致远端报 `$'\r': command not found`；
使用 UTF-8 文件上传，或先编码为 base64 再在远端解码执行，避免依赖管道的换行编码。

## 常驻 node（Mac，`scripts/node-launchd.sh`）

hub 通过 ssh 起 node（`jarvisd node --stdio`），node 再起助手的 claude。ssh 会话读不了 macOS 的**登录钥匙串**，所以 API key 存在钥匙串里的
claude（`apiKeyHelper` 用 `security find-generic-password` 取）在助手里会报 401；herdr 里的 Agent 没这个问题，因为 herdr 跑在登录会话里。

- `scripts/node-launchd.sh install`（在那台 Mac 上跑）：装一个 launchd 用户任务 `com.jarvis.node`，登录后常驻 `jarvisd node --attach`，
  监听 `~/.jarvis/node-attach.sock`。
- hub 照旧 ssh 起 `jarvisd node --stdio`；它发现有常驻 node 在听，就只把 ssh 的输入输出转给它（`internal/node/attach.go` 的 `Relay`），
  由常驻 node 处理这条连接。node 起的东西（助手的 claude、它的 key helper）都在登录会话里，能读钥匙串。hub 不用改，也分不出区别。
- 没有常驻 node（没装、退出了、socket 是残留的）时 `--stdio` 自己处理连接，和以前一样。
- 一次只服务一条连接：新连接进来（hub 重连）时旧的那条的 node 退出，等着的 hook 会去问新的（同 ssh 起的 node 被替换时）。同时只能有一个常驻 node。
- `deploy.sh` 换了二进制后会 `node-launchd.sh restart`（ssh 进来也能操作登录会话的 launchd）；hub 要重连一次才会用上新的 node（重启 hub，或 App 里「重新连接」）。
- `scripts/node-launchd.sh uninstall` 回到 ssh 起 node。日志：`~/.jarvis/node-attach.log`。

## tailcat 打洞（`internal/tunnel`，`hub/tailcat.go`）

hub ssh 不进去的电脑（NAT 后面、另一个内网、没有公网 IP）经 [tailcat](https://github.com/tailscale/tailcat) 接入：Tailscale 的数据面
（WireGuard + DERP 中继 + 打洞），不用它的控制面，不需要 Tailscale 账号、root、TUN 或开端口，全在 jarvisd 进程里（用户态 netstack）。

- **退避顺序**：配了 `ssh_host` 又配了 `tailcat_addr` 的机器每次连接先试 ssh（`ConnectTimeout=15`），失败立刻试 tailcat；
  ssh 失败后 5 分钟内的重连先试 tailcat，不再干等 ssh 超时。走 tailcat 期间每 5 分钟 `ssh <host> true` 检查一次，ssh 恢复就断开
  重连回 ssh（有提问 / 计划 / 授权在等手机时不切，重连会取消它们）；切回去又马上失败（ssh 能登录但 node 不回话，比如慢链路上
  40 秒内等不到 `node.info`）时，下一次检查的间隔翻倍，最多 32 倍，ssh 稳定 10 分钟后复原，不会每 5 分钟断一次。`kind: "tailcat"` 只走 tailcat。机器列表的 `transport`
  是当前走哪条，`tailcat_path` 是 tailcat 走的路径（`direct` 打洞成功 / `relay` 经中继，每分钟问一次，同时促使它重新找直连）。
- **断线检测**：tailcat 的用户态 TCP 没有 keepalive（ssh 有 `ServerAliveInterval`），对面睡眠 / 断网时连接不会自己断。hub 每分钟给
  node 发一次 `ping`，连续两次不回就断开重连（旧 node 回「unknown method」也算回了）。这一分钟里有数据进出（读到数据，或大的写
  每 32 KB 发出去一段）就算活着、不 ping：慢中继上一个大页面要传很久，ping 排在它后面会误判。ping 在单独的 goroutine 里等，
  卡在死连接的写上也不耽误判断；判定断线时直接关掉底层连接，卡住的写随之失败。
- **node 侧**：只有常驻 node（`scripts/node-launchd.sh install`，Mac 是 launchd，Linux 是 systemd 用户服务）会开 tailcat：
  有 `~/.jarvis/tailcat.json` 时，在自己的 tailcat 地址上听 `18662`（node 协议，和 ssh 转过来的连接共用「一次服务一条，新的替换旧的」）
  和 `18663`（接收新二进制）。起不来（没网、DERP 连不上）就在后台退避重试，ssh 转发照常服务。
- **配对**：hub 机器上 `jarvisd hub tailcat-key` 打印 hub 的公钥（第一次生成 `~/.jarvis/tailcat-hub.json`）；那台电脑上
  `jarvisd node tailcat init --hub <公钥>` 生成 `~/.jarvis/tailcat.json`（node 私钥、预共享密钥、选定的 DERP 区域、允许的 hub 公钥）
  并打印地址，填进 hub.json 的 `tailcat_addr`；重启那台的常驻 node 和 hub。再跑一次 `init` 可以加别的 hub，地址不变；
  `jarvisd node tailcat addr` 再打印一遍。
- **安全**：node 只回应 `tailcat.json` 里列出的 hub 公钥（tailcat 在列表为空时放所有人进来，所以没有 hub 公钥的文件直接拒绝加载），
  其他 key 连握手都没有；只放行 18662–18663 两个端口。地址里有预共享密钥，和 hub token 一样是密钥：只放 hub.json，日志里不打印。
  两个 key 文件都是 0600（读到权限更宽的会改回 0600）。
- **吊销 / 换密钥**：node 只在启动时读允许的 hub 公钥，tailcat 也不会踢掉已经连上的 peer。去掉一个 hub：从那台的 `tailcat.json`
  的 `hubs` 删掉它，再 `scripts/node-launchd.sh restart`。地址泄露（换预共享密钥和 node 私钥）：删掉 `tailcat.json` 重新 `init`，
  把新地址填进 hub.json 并重启 hub。换 hub 的 key：删掉 hub 机器的 `tailcat-hub.json`，重启 hub 后用新的 `hub tailcat-key` 在每台重新 `init --hub`，
  再从各台 `tailcat.json` 的 `hubs` 删掉旧的、重启常驻 node。
- **地址固定**：地址只写 DERP 区域 id（不内嵌中继服务器），`init` 时选一次最近的区域存起来；否则每次启动重新选区域，地址会变。
- **部署**：走 tailcat 的机器点「重新部署」时，hub 把二进制经 tailcat 的 18663 发过去（一行 `{"size","sha256"}` + gzip）；node 校验
  大小和 sha256、确认新二进制在本机跑得起来（`version`）后才替换自己，回 `{"ok"}` 再退出，由 launchd / systemd 拉起新版本。
  一次只收一个（同时来的第二个立即回「正在上传」）；60 秒没收到数据就放弃，hub 中途重启不会把下一次部署卡住。
  `deploy.sh` 首次安装仍要能 ssh 到那台（或手动放二进制）。
- **代价**：jarvisd 从约 19 MB 变成约 36 MB（gzip 后 8 MB → 15 MB）；冷启动约 7 秒（拉 DERP map、选区域），第一次拨通约 6.5 秒。
  公共中继没有 SLA、走中继时限速，可以用 `--derp-map-url` / hub.json 的 `tailcat.derp_map_url` 换成自建的 derper。tailcat 不承诺 API 稳定，
  升级依赖时跑一遍下面的联调。
- **只管 hub ↔ node**：手机 / 网页到 hub 仍然需要一个能访问到的入口（`public_url`）。
- 联调（要外网）：`JARVIS_TEST_TAILCAT=1 go test ./internal/tunnel -run EndToEnd`（真实中继上的拨号、鉴权、上传），以及
  `internal/hub/tailcat_e2e_test.go` 顶部注释里的命令（两版真实 jarvisd：ssh 不通 → 走 tailcat 上线 → 经 tailcat 部署新版本并重连）。
  `JARVIS_TAILCAT_DEBUG=1` 打开 tailscale 的日志（默认静音，打洞期间每秒好几行）。

## 数据库（Postgres）

hub 机器上的 docker 容器 `jarvis-postgres`（`postgres:18-alpine`，只监听 `127.0.0.1:18661`，数据卷 `jarvis-pgdata`，`--restart unless-stopped`）。
`scripts/db.sh` 幂等地创建 / 启动它，首次创建时生成密码写进 `hub.json` 的 `database_url`；`deploy.sh <hub 机器 id>` 每次都会先跑它。
hub 启动时自动执行未跑过的迁移（`schema_migrations` 记录，advisory lock 防并发）。

| 表 | 内容 |
| --- | --- |
| `live_activities` | 每台设备的实时活动：活动 token、push-to-start token、开始时间（`docs/live-activity.md`） |
| `devices` | 推送设备（APNs token），APNs 返回 410 / BadDeviceToken 时自动删除；`prefs`（jsonb）是这台设备要哪些推送（App 设置 › 通知），注册时不覆盖 |
| `phone_tools` | iPhone 最近一次声明的 phone_* 工具（手机离线时 Claude 也能看到） |
| `node_sessions` | 每台机器上 Jarvis 启动过的会话：pane、启动器、目录（node 经 hub RPC `state.load` / `state.put` 读写，Mac 不直连数据库） |
| `mail_accounts` | 邮箱账号：非敏感字段 jsonb + AES-GCM 密文；密钥 `~/.jarvis/secret.key` 不进数据库 |
| `interactions` | 每次提问 / 计划 / 授权 / 终端交互及结局（resolved / dismissed / expired / cancelled）；hub 重启时把残留的 pending 标成 cancelled |
| `settings` | App 里改的全局设置（如助手的启动器） |

连库：`ssh <hub 机器> 'docker exec -it jarvis-postgres psql -U jarvis -d jarvis'`。
测试：`./scripts/test-server.sh`（同步代码到 hub 机器，在 `jarvis_test` 库上跑全部测试；本机没有 docker，DB 测试在本机会 skip）。

旧版的 `phone_tools.json`、`devices.json`、`mail_accounts.json`、`sessions.json` 首次启动时自动导入，改名为 `*.migrated`。

## 配置（hub 机器 `~/.jarvis/`，0600，不入库）

| 文件 | 内容 |
| --- | --- |
| `hub.json` | `listen`、`token`、`public_url`、`hook_timeout_minutes`、`database_url`、`machines`、`google.client_id`（Gmail 用 Google 账号登录的 iOS OAuth 客户端）、`google.proxy`（Gmail 流量走的 HTTP 代理，如 `http://127.0.0.1:<端口>`），见 `docs/mail.md`；`notify`（推送时机，`docs/config.md`） |
| `secret.key` | 邮箱凭据的加密密钥（32 字节） |
| `apns.json`、`apns_key.p8` | 推送配置和 key；没有就关闭推送 |

每台机器 `~/.jarvis/` 下还有 node 生成的 `claude-settings.json`（hooks）、`mcp.json`、`node.sock`、`uploads/`。

配对：hub 机器上 `~/.jarvis/bin/jarvisd hub pair` 打印地址、token 和二维码（`jarvis://pair?url=…&token=…`）。

## 推送策略（`hub/push.go`、`hub/notify.go`）

- 需要你（提问 / 计划 / 授权 / 终端）：立即推，`time-sensitive`，带 `category: INTERACTION_<KIND>`（App 注册了对应按钮）。
  hook 拦下的对话框电脑上的 TUI 不显示，人在电脑前也答不了，所以不等。
- 做完了：照 Ovis（`github.com/devswha/ovis` 的 `server/push.ts`，MIT）的思路，推送是给不在电脑前的人的：
  - working → done / idle 后等 `notify.done_delay_seconds`（60 秒）再决定；期间恢复工作就当同一轮（开始时间不变），
    变成 idle（在 herdr 里看过）或在 App 里看过（`seen_at`）就不推；
  - 还有后台命令 / 子 Agent 在跑：先不推，等它们回报、Claude 处理完回报的那一轮结束后推一次（`WokeByTask` 的轮次单独出现时不推）；
  - 一轮的时长 = 第一次进入 working 到最后一次结束；每台设备按自己的设置（`prefs.done`：`off` / `long` / `always`）决定收不收，
    `long` 只收不短于 `long_turn_seconds` 的；`prefs.input` 关掉就不收「需要你」。
  - 屏蔽的 Agent、助手这一轮已经用 `notify_user` 推过的，都不推。hub 重启后进行中的轮次从重启后第一次看到它算起。

## 运维

- 机器离线：App 里显示 ssh 的最后一行错误。NAT 后面的电脑要先建好反向隧道（`docs/config.md`）。
- 机器显示在线但所有任务立即报 `rpc: connection closed`：检查启动器和助手接口是否同时返回 502。
  关闭 RPC 不会自动关闭 SSH 的 stdout；连接循环必须同时等待 `peer.Done()`，随后取消旧 SSH 子进程并重连，
  否则会一直保留已关闭的 peer 和过期的在线状态。`TestMachineReconnectReplacesClosedPeer` 用真实子进程覆盖此场景。
- 手动调接口：`curl -H "Authorization: Bearer $TOKEN" http://<hub 地址>:18660/api/v1/machines`。
- 请求 ID：每个响应带 `X-Request-Id`（App 发了 `X-Client-Request-Id` 就沿用它）。hub 只记失败（≥400）和慢（>2 秒）的请求，
  一行 `[<id>] 方法 路径 -> 状态码 (耗时)`，不记 query、body、token。App 的 5xx 错误提示后面带「（请求 <id>）」，拿它在
  `journalctl --user -u jarvisd` 里搜那一行。
- herdr 连不上（socket 没了、herdr 重启）：node 每 30 秒最多记一行 `herdr: … failed`（带这段时间失败了几次），恢复时记一行
  `reachable again`，经 ssh 的 stderr 进 hub 日志。
