# 经验：hub / node 与网络

踩过的坑、怪异行为和当时的取舍（原来在 `CLAUDE.md`「经验」，索引见 `docs/lessons/README.md`）。新经验写进对应的节，没有合适的就新开一节。

- ssh 会话读不了 macOS 的登录钥匙串：hub 经 ssh 起的 node 再起的 claude，如果 API key 从钥匙串取，会报 401（herdr 里的 Agent 没事，herdr 在登录会话里）。
  在那台 Mac 上装常驻 node（`scripts/deploy/node-launchd.sh install`，`docs/server.md`「常驻 node」）。
- Go 的 `UnixListener.Close()` 默认会删掉 socket 文件：重新部署时旧 node 退出会把新 node 的 `~/.jarvis/node.sock` 删掉，
  hook / MCP 全部静默失效（hook 连不上就放行给 TUI）。已 `SetUnlinkOnClose(false)` 并每 15 秒自检重建。
- hub 的内存状态随时会被别的 agent 重新部署清掉（2026-10-25 两分钟内被部署三次）：跨请求的流程（比如 OAuth state）要自包含或落库。
- 代理视频 / 音频必须转发 Range / Content-Range（AVPlayer 先要 2 个字节，拿到整个文件就拒播）；`URL.appending(path:)` 会把 `%`
  再编码一次，ID 里带 `/` 的路径要用 `percentEncodedPath` 自己拼。
- App 进后台不断开 WebSocket 的话，挂起的 socket 在 hub 看来还活着，`phone_*` 调用会干等到超时；现在进后台就断开，需要时用静默推送唤醒。
- hub 的 API 默认回 `Cache-Control: no-store`：不加的话 iOS 的 URLCache 会把每个 JSON 分页存一份（后台同步十几 MB，从来不读）。
  App 自己做磁盘缓存的东西（对话图片、飞书媒体）用 `APIClient.uncachedSession` 取，否则同一份字节存两遍。
- hub 机器走 TUN 代理时，系统自动升级会重启 systemd-networkd，它默认删掉别的程序加的 ip rule（代理的路由规则没了，经代理的连接全部超时；
  Tailscale 会自己补回，代理不会）：在那台机器上用 networkd 的 `ManageForeignRoutingPolicyRules=no` 防止（2026-10-29 推送因此断了约 3 小时）。
  APNs 是一条长连的 HTTP/2，网络断过以后 Go 不会自己发现连接已死，推送请求出错时要关掉空闲连接，下一次重新拨号（`push.go`）。
- `URLSessionWebSocketTask.sendPing` 的回调只在两个条件都满足时才来：对端的 pong 原样带回 ping 的负载（空 pong 不算），
  而且当时有一个挂着的 `receive`。`scripts/mock/mock_hub.py` 曾回空 pong，App 的心跳就每分钟判一次连接已死、重连（hub 用的 coder/websocket 没问题）。
- 机器监控（`docs/machines.md`）：darwin 的 jarvisd 是 `CGO_ENABLED=0` 交叉编译的，拿不到 `host_statistics` 等 Mach 调用，
  `vm.page_*` sysctl 又没有匿名页和压缩器数字：macOS 的 CPU / 内存来自常驻的 `iostat -n 0 -w 10` / `vm_stat 10`（每 10 秒一行，别每次采集起进程），
  其余是 sysctl、路由 socket（`NET_RT_IFLIST2`）和 statfs。手解 C 结构体（`if_msghdr2`、`struct loadavg`）前先用一个小 C 程序打印 `offsetof` 确认偏移（有 `#pragma pack(4)`）。
  Linux 判断虚拟网卡看 `/sys/class/net/<名字>` 是否指向 `/sys/devices/virtual/`，比按名字猜可靠（TUN 代理的网卡名五花八门）。
- macOS 的「桌面」「文稿」「下载」受隐私保护（TCC）：jarvisd 经 ssh 在 Mac 上运行，没授权时读这些文件夹报 `operation not permitted`，
  手机上打不开那里的文件。node 把这个错误换成说明（`explainDenied`）：在「完全磁盘访问权限」里打开 sshd-keygen-wrapper，或换个文件夹。
  要给用户看的文件（截图等）别放桌面，放 `~/` 下的普通目录。
  TCC 的权限算在负责进程头上：经 ssh 起的进程（node 和它的子进程）一律算 `sshd-keygen-wrapper`，给 jarvisd 二进制本身授权没用（而且每次部署换二进制、授权就掉）。
  给了它 = 任何 ssh 登录都有完全磁盘访问权限（能读 `~/Library/Messages/chat.db` 等）。只想给一个程序，就让 launchd 起它（LaunchAgent），见 `docs/plan-connectors.md`。
- 机器的 node 不是 hub 这一版（`node_mismatch`）时，App 的「重新部署」让 hub 自己装：`deploy.sh` 部署 hub 时把各平台的 node 二进制留在 hub 机器的
  `~/.jarvis/bin/nodes/`，hub 经 ssh 装进去、杀掉常驻 node 让 launchd 重启、重新连接（`hub/deploy.go`）。只部署了某一台、没部署 hub 时那里还是旧版，按钮会装回旧版本。
- 国内网络的 hub 机器上编译 jarvisd：`proxy.golang.org` 可能完全不通（TCP 超时，系统软件源 / Docker Hub 能不能用都不代表它能用），
  构建前 `go env -w GOPROXY=https://goproxy.cn,direct`。Postgres 镜像拉不下来同理，用 `public.ecr.aws/docker/library/postgres:18-alpine`（`docs/server.md`）。
- 云主机的公网 IP / EIP 前面通常有外层防火墙或 NAT（实测 UCloud 机器：22、8080 等所有端口从公网都连不上，但本机 ss 显示在监听，
  本机自己 curl 公网 IP 也不通——不能用 hairpin 自连判断端口有没有开）。手机接 hub 优先让它走公司 VPN / 内网地址，或在云控制台放行端口；
  判断公网可达性用外部探测（如 check-host.net 的 TCP 检查），不要只在机器上试。
- hub 关掉 RPC peer 不等于连接结束：ssh 子进程还活着，连接循环要同时等 `peer.Done()`，否则「重连」之后机器一直显示在线、每个调用都回 `rpc: connection closed`（`TestReconnectGivesAWorkingConnection`）。
- tailcat（`docs/server.md`「tailcat 打洞」，2026-11-01）：`Server.AllowedClients` 为空时**所有人**都能连进来，node 的 key 文件里没有 hub 公钥就拒绝加载；
  `Server.TailcatAddr()` 会把 DERP 区域的服务器内嵌进地址、且不设 `RegionID` 时每次启动按延迟重新选区域，地址会变：自己用 `ConnInfo{RegionID}` 拼地址，区域在 `init` 时定下来。
  `ServedTCPPorts` 之外的端口不回 RST 而是超时，拨号要自己带超时。node 重启后 hub 上旧的 tailcat client 第一次拨号会失败，
  连接失败时丢掉这个 client（`Dialer.Reset`）下次重建。tailcat 的连接（gVisor netstack）没有 keepalive，对面消失后读永远阻塞，hub 要自己定时 ping；node 退出时要先关掉给 hub 的连接、`DrainTCP`，最后才关 tailcat 端点，
  顺序反了 FIN 发不出去，hub 要等两次 ping 超时（约 2 分钟）才发现，重新部署因此超时（2026-11-01 联调踩到）。
  经 tailcat 收文件时 `gzip.Reader` 要 `Multistream(false)`，否则读完一段还会等下一段，连接上的回复永远发不出去。
- 自建 derper（`docs/server.md`「自建中继」，2026-11-02）：`-certmode manual -hostname <IP>` 缺证书时 derper 自己生成一张自签证书，
  在日志里打印配 map 用的节点（带 `CertName`），看着省事，但那张只有 1 年；tailscale 的 `tlsdial` 校验固定证书时照样检查 `NotAfter`，
  到期后连不上中继，所以要自己生成一张长期的。
  derper 不发 DERP map，`--derp-map-url` 是 tailcat 用 HTTP GET 取的，要另起静态服务。以 root 跑、没给 `-c` 时，derper 把私钥写到 `/var/lib/derper/derper.key`。
  换 map 后 `init` 会重新选区域，地址跟着变（密钥不变）。

## Cloudflare Tunnel 推送中继（2026-11-02）

- `systemctl --user` 的 `active` 只表示 cloudflared 进程在运行，不表示已经连上边缘或加载远程 ingress。验收要看 Tunnel 连接、
  官网健康检查和源站拒绝直连三个结果；正常出口下 URLSession 的 User-Agent 也要能访问。
- UDP/7844 被网络限制时，用 `--protocol http2`，同时确认 TCP/7844 能出站；某一个边缘地址超时，cloudflared 会重试其他地址，
  不要因为第一条超时日志就判定整个 Tunnel 失败。Token 用 `--token-file`，不放 `--token` 参数，日志级别不设 debug。
- Python urllib 的默认 User-Agent 可能先被 Cloudflare 边缘以 1010 拒绝，甚至没到 Worker；检查失败时要区分边缘拒绝、
  Worker 的 503 和源站的 `not through the proxy` 403。不要为了一个测试客户端关闭站点安全规则；用实际 App User-Agent 复测。
- 上线代理只更新 `proxy_secret` 和 Worker 同名密钥，保留 grant 的 `relay.secret`；源站健康检查需要带代理头，官网检查不需要。
  新建凭据和现有 grant 密钥的备份集中存密码管理器，临时文件权限 0600，值不写进参数或日志。

## 统一模型配置

- 「供应商」不等于可运行启动器：直接 API 接入不能下发 node 或进入新建会话列表；用途选择按 API / 启动器 / 插件能力过滤。只改 UI 或 `ANTHROPIC_MODEL` 无法保证模型生效，shell 启动器可能清空环境变量，必须验证参数转发并给真实执行器传模型。
- 模型缓存与进程复用要包含有效配置指纹：同名供应商修改 URL、凭据或模型后不能复用旧翻译结果和预热进程。测试分别覆盖同配置命中与配置变更失效，不能更换 fake API endpoint 后仍当成同配置。
- node 的精确模型元数据和 `node_sessions` 必须原子保存；复用 Postgres `settings` 的 `node_model.<machine>.<session>` 键与 SQL CTE，无需本地 JSON 状态文件。单用途编辑用数据库 revision 条件更新，不把 PATCH 的读改写误当成并发安全。
- 模型切换不能杀掉仍有后台任务的助手；下一轮在安全边界生效，忙时明确拒绝发送。普通任务页未提交模型字段时要保留已有模型选择，防止时间/开关编辑覆盖统一模型页刚保存的值。

- 旧入口修改助手启动器时也要写统一用途配置，不能只写旧 `assistant.launcher`：已有 `model_config.assistant` 会覆盖它，界面显示成功但下一段对话仍用旧值。
  修改「关于我」等非模型字段则从 `assistantConfigLegacy` 取基底，避免把有效模型选择抄进旧配置、以后恢复继承时又冒出来；审批期间用 revision 检查并发修改。

## 上游限流（2026-11-02）

- 中转的额度用完时回 `HTTP 429 account_quota_exhausted`，并发满时回 `HTTP 503 account_concurrency_full`。以前每个调用方各自把它当普通失败：知识处理和收件箱分拣一个条目一个条目地试，15 分钟打了 429 次请求、429 行日志，收件箱的条目还被当成「按规则」分过，额度恢复后也不再分。
  限流要在共用的客户端那一层按连接处理（`ai/limit.go`，`docs/model-config.md`「上游限流」）：暂停期间直接返回 `ai.PausedError`、不发请求；调用方遇到 `ai.IsPaused` 就停下这一轮、不打日志、不把条目标成处理过。新加循环调用模型的地方也要这样处理。

## 时区

- hub 跑在 UTC，用户不在（2026-11-01「时区也都修一下」）：服务端给人看、给 AI 读的时间、按天分的东西，一律按用户的时区（`Hub.userLocation()`，知识库是 `kb.Store.Zone()`），
  别用 `.Local()` / `time.Local` / `time.Now().Format`。`internal/tzguard` 的测试扫全部非测试代码，node（跑在用户自己的电脑上）和「还不知道用户时区」时的兜底在白名单里，写明理由。
- 测试别依赖跑测试的机器在哪个时区：时间用 UTC 造，读的时区固定传进去（hub 的 `testUserZone`、知识库 golden 的 `goldenZone`）；
  改了时间相关的代码，在 hub 上用 `TZ=UTC`、`TZ=Asia/Shanghai`、`TZ=America/Los_Angeles` 各跑一遍 `go test ./...`。
- 只记钟点的文件（知识库的天文件、时间线）换时区要整体搬，还要记下自己是哪个时区，不能直接改写入时用的时区：
  否则同一个文件里新旧两种钟点混在一起，`Since` 按新时区解析旧行会差 8 小时，漏读压缩窗口里的消息（`docs/knowledge.md`「时区」）。
- 发给别的程序的时区用 IANA 名字（`Hub.userTZName()`）：`time.Local.String()` 是 "Local"，不是时区名。
- hub 的 Docker 镜像（`docs/docker.md`，2026-11-01）：锁文件的 `resolved` 指向别的 npm 源时，别的网络里（GitHub Actions、NAS）`npm ci`
  卡一分多钟后只报 npm 自己的「Exit handler never called!」，看不出是网络问题。锁文件的下载源由 `privacy_check.py` 拦
  （`docs/testing.md`「npm 下载源拦截」），`Dockerfile` 装依赖前也查一次。镜像里的网页构建用 `--ignore-scripts` 跳过 `prebuild` 的
  隐私检查（构建上下文里没有 `scripts/` 和 git），这项检查在 workflow 里构建之前、对完整的 checkout 跑。
- 容器里 ssh 认不出没有 passwd 条目的 uid（`No user exists for uid`），同 uid 有两个条目时用前一个的家目录（alpine 的 guest 是 405，
  家目录 `/dev/null`，挂进来的 `~/.ssh` 就没用了）：镜像的入口脚本按 PUID / PGID 改 `jarvis` 用户本身并删掉撞号的条目，而不是
  `docker run --user <uid>`；`docker compose exec` 默认是 root，镜像里的 `jarvisd` 包一层降到 `jarvis`，否则建出来的 tailcat key 归 root，hub 读不了。

## 知识库（docs/knowledge.md）

- 保留期要卡在写入口，不能只靠每天的清理：会话第一次归档读的是「最新 100 条」，安静的会话这一页能早到几年前，
  清理刚删完，6 秒后就又写出 47 个超期的日文件（2026-11-02 线上）。所有写日文件的路径（`Archive`、`Ingest`、`Backfill`）共用
  `Engine.textCutoff()`（清理保留的最早那天的零点），清理走 `Engine.Prune`，两边是同一份天数；新加写入路径也要用它，`TestArchiveKeepsToTheTextRetention` 用「写完马上清理，删掉的天数应为 0」来查。

## 端口转发（docs/ports.md）

- RPC 的 `OnEvent` 跑在读循环里：在里面阻塞（比如把收到的字节直接写给一个读得慢的 WebSocket）会卡住这台机器的所有 RPC。
  转发的字节先进每条流自己的队列，由单独的 goroutine 写出；队列的上限靠窗口（发送方等 ack）保证，不靠丢数据。
- coder/websocket 默认一条消息最多读 32 KB（`read limited at 32769 bytes`，对方收到 `StatusMessageTooBig`）：转发的连接要 `SetReadLimit`，
  测试里自己写的 WebSocket 客户端 / 服务端也一样，否则大一点的 HMR 消息报的错像是转发坏了。
- dev server 第一次请求可能要编译好几秒（next dev）：探测「是不是 HTTP」时，连上了却超时没回话的不能当成「不是 HTTP」，先列出来再问。

## 异步结果晚到（2026-11-03，#556）

- 定时任务的一次运行在结束时才记结果（`recordRun`），而运行是异步的：一个慢的失败（助手要一会儿才放弃）可能在下一次运行或「错过了」的跳过之后才记，
  以前会把任务的最新状态盖成旧的那次。现在每次运行带着它被派出时任务的 `LastRun`（`ScheduleRun.of`），任务已经有更新的运行就只进历史、不改状态。
  别处异步写「最新状态」的地方同理：写之前看自己还是不是最新的那次。测试里等 `runDue` 派出的运行（`h.sched.inflight.Wait()`）再往下看。
