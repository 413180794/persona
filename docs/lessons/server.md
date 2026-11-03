# 经验：hub / node 与网络

踩过的坑、怪异行为和当时的取舍（原来在 `CLAUDE.md`「经验」，索引见 `docs/lessons/README.md`）。新经验写进对应的节，没有合适的就新开一节。

- 连接器模式监听不能只记启动时的文件时间，等以后变化才核对模式（2026-11-03，#748）：运行循环先选 active/passive，再异步启动监听；用户在监听 goroutine 起跑前切换时，新配置会被当成初始快照，从此漏掉第一次切换。采样初始时间后立即核对实际模式，不同就取消旧模式。双向回归直接模拟“配置已变、监听才开始”，不延长等待掩盖；观察到初始状态不等于后台监听已就绪。

- 将用途模型收敛成分类默认时，不能只隐藏列表或让 resolver 忽略旧记录（2026-11-03，#697）：任务新建运行、续自己的会话、回来源会话仍可能把旧 `Launcher/Model/Effort` 放回执行参数，`applyModel` 又会把它们认作显式选择。执行入口必须停止传这些旧用途参数，续聊是否换 launcher 也只比较分类结果；回归同时验证三条路径的真实 RPC 参数、权限不变、旧用途 PATCH 拒绝和原始记录保留。
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
- App 更新页提示「这个 hub 不会自己更新」：查 `GET /api/v1/hub/update` 的 `can_update`，`Build.SelfUpdates()` 要求
  channel=release、build 号合法（YYYYMMDDHHMM）、编译时编入了 updateURL（`<发布站>/dl`）三个条件都满足。
  `deploy.sh` 从构建配置的 `publish.site_url` 经 ldflags 注入它；手动 go build 部署最容易漏——配置里缺 `publish.site_url`
  就没有 `-X main.updateURL=...`，补上配置、照 deploy.sh 的 ldflags 重编即可。Docker 渠道只更新镜像、不替换二进制，
  提示换镜像是正常的（`docs/hub-update.md`）。
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
- hub 关停别指望 `http.Server.Shutdown` 收尾 WebSocket：它不等已经 hijack 的连接，而 `pgxpool.Close` 会一直等所有借出的连接归还、没有上限。2026-10-31 一次 NAS 升级，旧 hub 收到 SIGTERM 后卡到被 SIGKILL，停机约 46 秒（平时约 1 秒）。现在先关监听、给 WebSocket 发 1001 并限时排空，每一步有超时、整个关停有总预算（`shutdown_timeout_seconds`，默认 8 秒），见 `docs/server.md`「关停」；外部的 `docker stop -t` 要比总预算长。
- coder/websocket 的读、写、ping 只要用的 context 结束，就直接关掉整条连接（`context.AfterFunc`），不发关闭帧。
  要带关闭码收尾的连接（jarvis-link 被顶掉 4001、撤销 4002），别用会先被取消的 context 去写或读：hub 的链路先 cancel 自己的 ctx 再 Close，
  正在写的一条消息就把连接掐了，客户端收到 -1 而不是 4002（`hub/link.go` 的 `send`、`keepAlive`，`TestLinkReplacedAndRevoked`）。

## jarvis-link 在 Linux 上（docs/link.md「Linux（headless）」，2026-11-02）

- `errors.Join()` 什么都没给时返回 nil：jarvis-link 选路的 `race` 在所有候选都被取消（停服务时正好在拨号）时回了「没赢家、也没错误」，
  调用方拿着 nil 链路就崩了。全部被取消时要明确返回 `ctx.Err()`（`TestRaceCalledOffIsAnError`）。
- `su`、`sudo -u`、`docker exec -u` 进来的会话没有 `XDG_RUNTIME_DIR`，`systemctl --user` 报 `Failed to connect to bus`：
  用户实例其实在跑（开了 linger），补上 `/run/user/<uid>` 就能连。用户实例本身不在（没开 linger 又没人登录）时补也没用；
  关掉 linger 的那一刻没有会话的用户实例就停了。
- 用户实例里没有 `network-online.target`，用户 unit 写 `After=/Wants=network-online.target` 什么也不等；靠程序自己重试。
- journal 不持久（`/run/log/journal`）时普通用户 `journalctl --user` 看不到自己服务的日志：jarvis-link 的日志写自己的文件、自己轮转，
  `jarvis-link logs` 读文件，journal 只是兜底。
- 判断常驻 node 在不在，**不能**去连 `~/.jarvis/node-attach.sock` 试试：每条连接都是一次新的 attach，会把 hub 正在用的那条顶掉。只看 hub 报的。
- 在 Docker Desktop（Mac）里跑带 systemd 的容器：`--privileged --cgroupns=host -v /sys/fs/cgroup:/sys/fs/cgroup:rw --tmpfs /run --tmpfs /run/lock`，
  PID 1 是 `/lib/systemd/systemd`；镜像里要有 `libpam-systemd`，没有时 `user@<uid>.service` 起不来（日志是
  「Trying to run as user instance, but $XDG_RUNTIME_DIR is not set」）。宿主只听 127.0.0.1 的服务，容器经 `host.docker.internal` 就连得到，
  测试 hub 不用开到局域网。
- `docker pull --platform linux/amd64 <镜像>` 会把本地同名标签换成 amd64 的，之后不带 `--platform` 的 `docker run` 都在模拟里跑；
  测完 `docker pull --platform linux/arm64 <镜像>` 换回来。

## jarvis-link 被动模式和 node 服务（docs/link.md「被动模式」「装 node 服务」，2026-11-02）

- **launchd 的 label 不跟着 HOME 走**：测 `jarvis-link node install` 时换了临时 HOME 也没用，`launchctl bootout gui/<uid>/com.jarvis.node` 停的就是这台 Mac
  上真实在用的常驻 node。Mac 上的服务安装只在假命令（`env.command`）里测，绝不在开发机上真的跑；要真跑就换一个 label。
- Docker 容器里的 `eth0` 在 sysfs 里是 `/sys/devices/virtual/net/eth0`，netaddr 把它当虚拟网卡：按「不要虚拟网卡」挑配对链接里的地址，容器里一个都挑不出来，
  `install --listen` 直接失败（NAS 的网卡挂在虚拟交换机上也一样）。没有别的地址时退一步用虚拟网卡，隧道和桥接（`docker0`、`veth`、`utun`、`tailscale`……）照样不要；
  挑不出地址也只是不打印链接，服务照装。
- 被动模式的流要带关闭码结束（换 hub 时 4002、停止时 1001），和主动模式一样不能先取消 `websocket.NetConn` 的 context：它用 `context.Background()`，
  结束时直接 `ws.Close(code, …)`，再关 node 的 socket。
- `docker build` 的 `FROM <镜像 id>` 会去 registry 找名叫这个 id 的镜像（报 `registry-1.docker.io … EOF`）：先 `docker tag <id> <名字>` 再 `FROM <名字>`。
  注意给悬空镜像打的标签删掉时镜像也跟着删了。

## Git 面板的安全拉取（2026-11-03，#684）

- `git status` 的落后计数只看本机已缓存的远端引用，显示已同步不代表远端没有新提交；已同步时也要保留拉取入口。
- `git pull --ff-only` 仍要显式禁止变基和自动 stash，避免用户的 `pull.rebase`、`branch.<name>.rebase`、`merge.autoStash` / `rebase.autoStash` 改变行为；干净工作区检查加上 Git 自己的保护，不使用 reset、自动 stash 或合并。
- 新的网络命令不要复用默认 15 秒的只读命令超时：pull 60 秒、node 全程 75 秒、hub 90 秒、客户端 100 秒，禁用终端凭据询问并限制取消后的管道等待。测试用本地 bare 远端与隔离 Git 配置，不操作用户的真实仓库。
- Git 的「无上游」原来用了不存在的 SF Symbol `cloud.slash`，登记到设计系统后 `everyNameIsASymbol` 才发现；正确的带斜线云朵符号是 `icloud.slash`。图标登记与有效性检查不要豁免。
- 直接测试 node 的 RPC `handle` 时，最小 fixture 也要初始化 `state: newState()`；方法分发之前会查询会话状态，仅有 `home` 会在进入 Git 方法前空指针崩溃。

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

## 管理界面（docs/admin.md）

- **来源地址别当真两次**：Docker 的用户态代理（docker-proxy）把经端口映射进来的连接都换成网桥网关的地址（`172.x.0.1`，私网）：
  宿主机自己连的、Docker 没开 IPv6 时所有 IPv6 的、Docker Desktop / OrbStack 上所有的，公网来的也在里面。按「私网 = 局域网」判断，
  公网请求就成了局域网，刚部署的 hub 会被抢先设管理员密码。容器里网关一律不算局域网（`containerGateway`）；Linux 上局域网电脑经
  iptables DNAT 进来，地址是真的，不受影响。同机反向代理（nginx 默认不加转发头）让公网请求看起来来自 `127.0.0.1`，所以回环来源做
  首次设置要日志里的一次性设置码。2026-11-02 在本机 Docker Desktop 实测：Mac 经映射进来是网关、同网桥的容器是自己的地址、`docker exec` 是回环。
- **转发头只能用来收紧**：不读 `X-Forwarded-For` 放行任何人；带了就要求里面每个地址也在范围内，只说是代理（`Via`、
  `X-Forwarded-Proto` …）不说替谁转的算范围外。
- **范围外要像不存在**：返回 404 反而暴露「这里有个特殊路径」（别的 `/api/` 路径没 token 是 401、页面路径回网页的 `index.html`）。
  范围外的请求原样交给普通网页和 App 的接口去答。页面路径按小写匹配 `/admin`：大小写不敏感的文件系统（macOS）上 `/ADMIN/index.html`
  会被普通网页的静态文件服务直接读出来。
- **改 hub.json 别经过 map**：`map[string]any` 重新序列化会把键按字母排序；用保留键顺序的对象只改要改的键。原子写的锁加在目录上，
  别在 hub.json 旁边留锁文件（`rename` 换掉的是文件本身，锁 hub.json 的 fd 锁不住下一个写者）。
- **`sync.Once` 懒加载的状态**：测试在第一次读之前直接改字段，会被随后的 `Once` 覆盖回配置值；测试先调一次初始化再改。
- **测试里的私网地址**：`privacy_check.py` 拦私网 IPv4 字面量（文档网段 192.0.2.0/24 等除外）；要测私网行为就用数字拼
  （`netip.AddrFrom4`，见 `ingress_test.go`、`admin_test.go` 的 `ip4`），注释里也别写。
- **`curl … | sh` 也能问问题**：标准输入是脚本本身，从 `/dev/tty` 读（`(: </dev/tty) 2>/dev/null` 判断有没有终端，CI 和 cron 里没有）。
  本机验证交互分支用 `script -q /dev/null <命令>` 给它一个 pty，把答案从标准输入喂进去。

## hub 互联（docs/mesh.md，2026-11-02）

- （第一版，对等互联时的）给 App 的机器 id 要在**序列化之前**换：`Hub.broadcast` 原来先 `json.Marshal` 再看消息，换了 map 却没换已经编好的字节。
  第二版（单中心）里中心的机器对象的 id 就是给 App 的 id（成员的电脑在中心建一个 kind `hub` 的机器），这层翻译整个删掉了。
- `httptest.NewTLSServer` 起的所有服务器用的是同一张内置测试证书：测试里几台 hub 的指纹一样，钉扎测不出「连错了 hub」，要靠 hello 里核对 hub_id。
- 同一台常驻 node 有两条路（两项配置、或中心一条经成员一条）时，第一次（还不知道 uid）两条都会去连，node 只留一个 attach、后连的顶掉先连的。
  第二版只有中心连，按 uid 把路合成一台以后只用一条；路 → uid 落库（`mesh.path_uids`），重启后不用再顶一次。测试「不抖」要等连接数稳定一段、
  再看几秒内没有新的连接，不能看到一瞬间对了就算数。
- 有对端的 hub 重启后，在链路上来之前它不知道自己是不是已经被换成成员（任期在别人那里）：先等 10 秒或第一条链路（`meshSettling`）再去连自己的电脑，
  否则一个被换下来的旧中心回来时会抢一下它的电脑。
- 链路上的 RPC 事件在读循环里处理：收到更大任期的记录时角色切换（断开 App、停掉自己的电脑）都只是关连接、入队，不在读循环里等 node。
- 经成员开 node 流时，成员打开 ssh 用的 context 不能是「打开的那一步」的超时 context：ssh 进程跟着 context 活，那一步一结束进程就被杀了；
  用一个跟着这条流走的 context。HTTP handler 劫持连接以后，请求的 context 也别再当这条流的寿命。
- 本机同时测两台 hub 的管理界面（都在 127.0.0.1、不同端口）：cookie 不分端口，两台的 `jarvis_admin` 会互相覆盖，无头浏览器要一台一台地登录、截图。
- 连接器（jarvis-link 被动模式）按来源计失败、一分钟 10 次封 15 分钟：hub 拿着坏凭据按普通退避重试再加上每 30 秒的探测，会把自己的地址封掉；
  401 之后整条路（拨号和探测）停 5 分钟。
- 两边同时拨号时，被拒的那一条在拨的一方看到的可能是 4011，也可能是 hello 的 `rpc: connection closed`（对方先完成了自己的 hello 再关）：两种都只是「已有链路」，
  拨号循环回到开头等那条断了再说，别当成错误刷日志。
- 管理界面的 `/api/v1/admin/` 写操作只收 `Content-Type: application/json`（CSRF 防护），不带 body 的 DELETE 也要带这个头，命令行走 `/api/v1/admin/cli/` 时同样。
- 本机测互联用两台临时 hub（各自的临时 HOME、端口、测试库里各自的 schema：`database_url` 加 `&search_path=<schema>`）：回环地址不进候选地址，
  互联码里会没有地址，要在 hub.json 的 `public.advertise` 写 `https://localhost:<TLS 端口>`。
- **只该有一份的状态要按 hub_id 存**（2026-11-03）：`mesh_group` 起初是单行表，hub 的测试和 db 的测试共用一个测试库，db 测试写进去的「中心是 hub-a」让后面一台毫不相干的测试 hub 以为自己是成员，客户端全部拿到 `503 center_unreachable`。改成以 hub_id 为主键：恢复到别的机器的备份同理。共用库的测试写完要清掉自己写的行。
- **只监听回环的 hub 别在局域网广播**：mDNS 的 A / AAAA 是本机所有网卡的地址，TLS 只听 127.0.0.1 时，别的 hub 听到的全是连不上的地址。
## 异步结果晚到（2026-11-03，#556）

- 定时任务的一次运行在结束时才记结果（`recordRun`），而运行是异步的：一个慢的失败（助手要一会儿才放弃）可能在下一次运行或「错过了」的跳过之后才记，
  以前会把任务的最新状态盖成旧的那次。现在每次运行带着它被派出时任务的 `LastRun`（`ScheduleRun.of`），任务已经有更新的运行就只进历史、不改状态。
  别处异步写「最新状态」的地方同理：写之前看自己还是不是最新的那次。测试里等 `runDue` 派出的运行（`h.sched.inflight.Wait()`）再往下看。

## 卡片慢了 4–10 秒（2026-11-03，#751）

- **`select` 自己吃掉了标记**：`pollAgents` 用一个 `reflect.Select` 同时等计时器和各个宿主的事件通道（缓冲 1），叫醒以后再逐个 `drained()` 看「谁有变化」。
  叫醒它的那个通道的标记已经被 select 取走了，单独一个事件（Agent 变成 blocked）于是谁也没认领，这次不取 snapshot，只剩 10 秒兜底。
  #611 把单个 `case <-kick: kicked = true` 改成按宿主的多路 select 时丢了这一步，所有状态变化都晚 0–10 秒，用户只觉得「同步变慢了」。
  多路 select 选中的那一路要记下来（`chosen` 对回它的宿主），别指望再读一次通道。
- **herdr 0.9 的事件名带点**：`pane.agent_status_changed`，和订阅时一样；代码和文档原来写的下划线（`pane_agent_status_changed`）是更老的 herdr。
  要按事件名区分时两种都认，测试里两种都喂。拿不准就直接连 socket 订一次看原始输出（`events.subscribe` 一行请求，后面一行一个事件）。
- **固定的「先等一下」要问清楚在等谁**：hub 给每张屏幕卡先等 1.5 s，等 hook 的卡先到；没有 hook 的会话（用户自己的 profile）永远等不到，每张卡白等。
  按会话判断（这次 hub 启动后发过 hook 事件，或 Jarvis 起的）。
- 量链路：在 hub 包里起一个真的 node（同一进程、自己的 HOME，`~/.claude` 链到真的那份），和 hub 按 SSH 那样用管道接起来，对隔离 herdr 里的真实 Claude
  逐跳打时间戳（`hub/cardlatency_live_test.go`）。时间线里所有上报都落在整秒的同一个零头上（.57 s、.58 s），说明走的是每秒的 tick，不是事件。
