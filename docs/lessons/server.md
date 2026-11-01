# 经验：hub / node 与网络

踩过的坑、怪异行为和当时的取舍（原来在 `CLAUDE.md`「经验」，索引见 `docs/lessons/README.md`）。新经验写进对应的节，没有合适的就新开一节。

- ssh 会话读不了 macOS 的登录钥匙串：hub 经 ssh 起的 node 再起的 claude，如果 API key 从钥匙串取，会报 401（herdr 里的 Agent 没事，herdr 在登录会话里）。
  在那台 Mac 上装常驻 node（`scripts/node-launchd.sh install`，`docs/server.md`「常驻 node」）。
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
  而且当时有一个挂着的 `receive`。`scripts/mock_hub.py` 曾回空 pong，App 的心跳就每分钟判一次连接已死、重连（hub 用的 coder/websocket 没问题）。
- 机器监控（`docs/machines.md`）：darwin 的 jarvisd 是 `CGO_ENABLED=0` 交叉编译的，拿不到 `host_statistics` 等 Mach 调用，
  `vm.page_*` sysctl 又没有匿名页和压缩器数字：macOS 的 CPU / 内存来自常驻的 `iostat -n 0 -w 10` / `vm_stat 10`（每 10 秒一行，别每次采集起进程），
  其余是 sysctl、路由 socket（`NET_RT_IFLIST2`）和 statfs。手解 C 结构体（`if_msghdr2`、`struct loadavg`）前先用一个小 C 程序打印 `offsetof` 确认偏移（有 `#pragma pack(4)`）。
  Linux 判断虚拟网卡看 `/sys/class/net/<名字>` 是否指向 `/sys/devices/virtual/`，比按名字猜可靠（TUN 代理的网卡名五花八门）。
- macOS 的「桌面」「文稿」「下载」受隐私保护（TCC）：jarvisd 经 ssh 在 Mac 上运行，没授权时读这些文件夹报 `operation not permitted`，
  手机上打不开那里的文件。node 把这个错误换成说明（`explainDenied`）：在「完全磁盘访问权限」里打开 sshd-keygen-wrapper，或换个文件夹。
  要给用户看的文件（截图等）别放桌面，放 `~/` 下的普通目录。
- 机器的 node 不是 hub 这一版（`node_mismatch`）时，App 的「重新部署」让 hub 自己装：`deploy.sh` 部署 hub 时把各平台的 node 二进制留在 hub 机器的
  `~/.jarvis/bin/nodes/`，hub 经 ssh 装进去、杀掉常驻 node 让 launchd 重启、重新连接（`hub/deploy.go`）。只部署了某一台、没部署 hub 时那里还是旧版，按钮会装回旧版本。
- hub 关掉 RPC peer 不等于连接结束：ssh 子进程还活着，连接循环要同时等 `peer.Done()`，否则「重连」之后机器一直显示在线、每个调用都回 `rpc: connection closed`（`TestReconnectGivesAWorkingConnection`）。
- tailcat（`docs/server.md`「tailcat 打洞」，2026-11-01）：`Server.AllowedClients` 为空时**所有人**都能连进来，node 的 key 文件里没有 hub 公钥就拒绝加载；
  `Server.TailcatAddr()` 会把 DERP 区域的服务器内嵌进地址、且不设 `RegionID` 时每次启动按延迟重新选区域，地址会变：自己用 `ConnInfo{RegionID}` 拼地址，区域在 `init` 时定下来。
  `ServedTCPPorts` 之外的端口不回 RST 而是超时，拨号要自己带超时。node 重启后 hub 上旧的 tailcat client 第一次拨号会失败，
  连接失败时丢掉这个 client（`Dialer.Reset`）下次重建。tailcat 的连接（gVisor netstack）没有 keepalive，对面消失后读永远阻塞，hub 要自己定时 ping；node 退出时要先关掉给 hub 的连接、`DrainTCP`，最后才关 tailcat 端点，
  顺序反了 FIN 发不出去，hub 要等两次 ping 超时（约 2 分钟）才发现，重新部署因此超时（2026-11-01 联调踩到）。
  经 tailcat 收文件时 `gzip.Reader` 要 `Multistream(false)`，否则读完一段还会等下一段，连接上的回复永远发不出去。

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

