# 机器监控

每台电脑是什么系统、现在忙不忙、过去 24 小时的曲线。node 采集，hub 落 Postgres，App 里在「机器」页和机器详情看（和 Agent 运行数放在一起）。

| 部分 | 代码 |
| --- | --- |
| 采集（解析 /proc、vm_stat、iostat、sw_vers、Win32 的返回等） | `server/internal/sysmon/`（`sysmon.go`、`winparse.go` 纯解析，`linux.go` / `darwin.go` / `windows.go` 读系统） |
| node 发送 | `server/internal/node/metrics.go` |
| hub 存储、降采样、清理、API | `server/internal/hub/metrics.go`、`server/internal/db/metrics.go`、迁移 `0051_machine_metrics.sql` |
| App | `apple/Persona/Views/Machines/`（`MachinesView` 机器页、`MachineDetailView` 机器详情、`MetricCharts`、`OSBadge`），模型 `apple/Shared/MachineMetrics.swift` |
| 本机看采集结果 / 开销 | `jarvisd sysmon [n]`：打印系统信息和 n+1 个样本（每 10 秒一个），最后一行是每次采集耗时和 personad 的 CPU |

## 谁连电脑（2026-11-04）

用户拍板：**电脑是管理者的，由管理者自己去连**；hub 看不到任何一台电脑，总线上也就没有要在电脑和管理者之间把关的东西（物理隔离）。迁移期间只有一个管理者，就是首席管理者（助手的进程 `personad assistant`），所以它带着全部电脑。

| 部分 | 代码 |
| --- | --- |
| 管理者这一侧：连接、每台的状态、选路、Persona Link 的协议、kind service、tailcat、warm start、引擎、herdr 自动启动、Claude 配置目录、常驻列出的会话、node 更新 | `server/internal/devices/`（`devices.go` 入口和上报、`machine.go`、`conn.go`、`paths.go`、`link.go`、`relay.go`、`service.go`、`tailcat.go`、`warm.go`、`engine.go`、`herdr.go`、`claudedirs.go`、`deploy*.go`、`bus.go`） |
| 两边共用的消息 | `server/internal/devicewire/`（方法名、参数、错误码、管理者自己回答的路由表 `Routes`） |
| hub 这一侧：转发、它留的那份、它自己的功能在连上时要做的 | `server/internal/hub/devices_link.go`、`devices_relay.go`、`machines.go`、`machine.go`（`Machine` 现在是管理者报来的那份加上 hub 自己的东西） |
| 助手进程里接起来 | `server/internal/assistant/main.go`、`devices.go`（`bus.go` 里几处只调它） |

**管理者做的**（`internal/devices`，hub 不 import 它，它也不 import hub）：

- 按 hub.json 的 `machines` 连每一台：hub 机器自己的 node（`node --stdio` 子进程，现在是管理者的子进程）、ssh、tailcat（hub 的 tailcat key 文件，同一个 HOME）、kind service（拨连接器；凭据还是 hub 用自己的密钥封着存，管理者经总线要 `devices.connector_cred`，配对完交给 hub 存 `devices.connector_paired`）、
  kind link（电脑上的 Persona Link 照旧连 hub 的地址，hub 原样转给管理者，见下）、互联组里经成员 hub 到的电脑（选路、去重、换路都在管理者，`paths.go`；经别的 hub 的那条流由 hub 打开再转过来）。
- 每台的状态：在不在线、`node.info`、会话列表和 revs、哪些会话被 App 看着（node 换了连接时再 `session.watch`，从最后一条 item 续）、warm start、引擎（settings `machine_engines`）、herdr 自动启动、Claude 配置目录（及屏蔽的）、
  node 不管多久没动都要列出的会话（助手的 profile 的 `keep`，助手的快照一出来就告诉各台 node，`SetKeep`）、node 更新（`node.deploy.<id>`）、node 的 `state.put` / `state.load`（表 `node_sessions`）。
  键和表都和以前一样，不迁数据。
- 管理者自己回答的路由（`devicewire.Routes`，hub 照助手的路由那样作为 `http.request` 转过来）：`POST …/reconnect`、`PUT …/engine`、`GET/PUT …/herdr`、`POST …/herdr/start`、`POST …/deploy`、`GET …/claude-dirs`、`PUT …/claude-dirs/blocked`。
- node 发来的请求：`state.load` 和助手自己的（`assistant.model`、`assistant.context`，进程内直接问助手）它答，别的（`interaction.create`、`assistant.lark`、`mcp.tools`、`mcp.call`）交给 hub（`devices.request`）。
  请求来的那条 node 连接要是在 hub 回答前断了，管理者告诉 hub（`devices.request_end`），hub 就不再等它，和以前 node 自己的连接断了一样：要批准的卡片能续上的留着，hook 从新连接再来认领时拿到回答，不会被已经没人读的那次等待吃掉。
  node 发来的每个单向事件都原样交给 hub（`devices.event`，顺序不变）。
- hub 发给 node 的单向事件（`devices.notify`）管理者按机器排队、各写各的，一台电脑收得慢只耽误它自己，总线和别的电脑照常；一台写一条卡住 30 秒或者排着的超过 32 MiB，就重连它。
- hub.json 里一台的条目变了（hub 改了配置重启，管理者还在），管理者换一个新的机器对象重新连它（App 正在看的会话照样再 watch），不在跑着的连接底下改配置。
- 助手自己对机器的调用（`device.call` 里 node 的方法）直接在进程里走，不再绕 hub；hub 自己答的（`assistantwire.HubDeviceMethods`：`devices.list` 带手机在不在线、`interactions.list`、`sessions.list`、`session.get`，渠道跟对话的 `session.follow` / `session.unfollow`、只发一次的 `session.send_once`）照旧问 hub。Agents 的分诊发「继续」、助手开 Agent、定时任务开对话这些对 node 的调用都在进程里直接走。

**hub 做的**：

- 把 hub.json 的 `machines`（加上管理界面运行中加的）作为默认值交给管理者：握手回答里的 `devices`，变了（加了电脑、互联组变了、hub 的地址或名字变了）发 `devices.config`。
- 留着管理者报的那份（`devices.state`：每台的快照、`node.info`、连没连着、这条连接从什么时候开始、warm 到什么时候、它的几条路），按它的顺序给 App 列，再加上 hub 自己的：`system`（监控，本页上文）、`computer` / `browser`（Cua、ego 插件的状态）。
- App 对机器的调用和流照旧经 hub：调用是 `devices.call`（带期限），发给 node 的单向事件（终端输入、转发端口的字节）是 `devices.notify`，node 的事件（items、终端帧、端口的字节……）从 `devices.event` 回来，
  hub 用原来的代码处理（推给订阅的 App、交互、推送、收件箱、分诊……）。所以终端、端口转发、屏幕、下载都走总线这一条连接，不另开通道。
  调用的期限管理者那头少一点（十分之一，最多 500 毫秒），它的「超时」先回来；没期限的调用（插件的 `nodes.call`）也不加期限。发出去以后才出的事——node 的连接断了、时间到了、总线断了——都算「可能已经做了」：
  发消息的结果是 `unknown`，同一条消息不会再发一遍（和以前 hub 直连 node 时一样），只有确定没发出去的（离线、管理者不在）才算没发。
- hub 的自更新确认「之前在线的机器都连回来了」、之后让管理者更新各台的 node，看的是报来的 `connected`（node 真连着），不是 App 看到的在线（管理者重启后的暖启动也显示在线）。
- 管理者和 hub 的连接每次新握手，经上一条连接开着的转发流和终端两边都结束：hub 结束它那边（终端以「助手暂时连不上，稍后再试」结束，App 重新打开），管理者在 node 上关掉（`stream.close`、`terminal.unwatch`），免得 hub 重启后 node 还开着没人要的端口连接、终端还一直推帧。
  App 正在看的会话 hub 让新连接再 watch，同时最多 8 个，一台慢的电脑不耽误别的。
- node 一连上，管理者先问 hub 它的那部分（`devices.connected`）：要加进 `node.configure` 的（Codex 的设置）和紧接着要发的（托管的启动器、hub 记着的发送），然后才问会话列表；hub 同时开始它在连上时要做的（Cua / ego 的状态、扩展在电脑上的程序、监控的系统信息、连接器）。
  管理者和 hub 的连接每次新连上，这一步对每台在线的机器再做一遍。
- 一台机器的连接断了（报来的 `connected` 变成 false 或者 `connected_at` 换了），hub 结束这条连接上的：终端流、正在生成的文字、还在等的不可续交互（和以前 detach 一样）。

**流的转发**：App ↔ hub ↔ 管理者 ↔ node，hub 和管理者之间是本机回环上的总线。实测（隔离的 hub，hub 自己那台，转发端口上 32 字节来回 300 次，2026-11-04）：master 中位数 0.3–1.2 ms、p95 约 1.5 ms；这一版中位数 0.9–1.4 ms、p95 约 1.9 ms，多了约 0.2–0.4 ms（多一跳回环加一次编解码）。
终端帧走的是同一条路（node 的事件 → 管理者 → hub → App，输入反过来）。

**Persona Link（主动模式）**：电脑上的 Persona Link 照旧连 hub 的地址（配对码、链路凭据都不变）。hub 认凭据（必须是 kind link 那台的、走 TLS），然后把这条 WebSocket 原样交给管理者：
`devices.accept {id, kind: link, machine_id, token_id}`，管理者用自己的总线 token 拨 `GET /api/v1/bus/relay?id=<id>`，hub 两边逐条转发消息（文本是 Persona Link 的消息，二进制是 node 的字节流），关闭码和原因原样过去。
hello、`endpoints`、`open`、`node` 这些都是管理者说的；Persona Link 开的流（`/api/v1/link/stream`）同样转。心跳（ping）是 hub 和 Persona Link 之间的，没有 pong 时 hub 断开两边，原因交给管理者。
凭据撤销时 hub 以 4002 关掉它转着的链路；管理者没连着时 hub 对 `/api/v1/link` 回 503，Persona Link 照常退避重连，管理者回来后自己连上。

**互联（mesh）**：组、中心、记录、凭据交接、成员转发 App 还是 hub 的（`docs/mesh.md`）；电脑和路是管理者的：hub 在 `devices.config` 的 `mesh` 里给它组里的样子（这台是不是成员、中心叫什么、刚起来要等多久、别的 hub 到不到得了和多远、成员的电脑），
管理者据此去重、选路、换路；经成员到的那条路由中心 hub 开（`GET /api/v1/bus/relay?hub=<成员>&machine=<id>`，hub 经互联链路开 node 流再转给管理者）；成员 hub 收到中心要的 node 流（`/api/v1/peer/node`）交给自己的管理者去开（`devices.accept` kind node），
管理者开好了 node 成员才回 `101`，开不了就带着原因回 502，中心在那台机器上显示这个原因（和以前成员自己开 node 时一样）。
Persona Link 链路的往返是 hub 的心跳量的，hub 告诉管理者（`devices.link_rtt`），选路和记录里的远近都用它。
hub 的记录里自己的电脑到不到得了、多远，用管理者报的（`devices.state` 的 `entries`）。

**管理者不在的时候**（进程在重启、崩了）：

- 机器全部显示离线，`error` 是「助手暂时连不上，稍后再试」，会话标成未知（hub 留着最后的列表）；终端流以同一句话结束，转发的端口关掉，不可续的交互结束。
- 要经管理者的接口（items、发消息、新建、文件、git、终端、端口、管理者自己的路由……）回 `503 {"code":"assistant_offline"}`；`GET /api/v1/machines`、`GET /api/v1/sessions` 照常回 hub 留的那份。
- 管理者回来（hub 1 秒起退避重启它）后自己把所有电脑连回来，hub 让它把 App 正在看的会话再 watch 上，不用谁动手；Persona Link 在下一次重试时连上。
  它刚起来时对一台还在连的机器的调用（hub 替它留着的卡片回答、App 新开的 Agent）等这台第一次连完（最多 20 秒），而不是马上回「离线」。
- 管理者不在时开不了新的 Agent、发不了消息、看不到 Agent 的一轮结束：这些都要经它到 node（以前 hub 自己连着电脑，助手不在时 Agent 照样能开）。

**总线上的大小**：hub 和管理者之间一条消息写完之前别的消息（包括心跳）要等，所以大的东西都分块：下载、上传、node 更新包每块最多 1 MiB，转发端口的字节每块 32 KB；
一页 items、一张图、一张截图是一条消息（和以前在 node 的连接上一样）。总线一行最多 64 MiB，node 的一条消息超过 60 MiB 时管理者不转（事件丢掉并记日志，调用回 `too_large`），免得整条总线断掉。

**密钥**：Persona Link 的凭据只到 hub（hub 认完只告诉管理者凭据的 id 和绑定的机器）；配对码、配对都在 hub。kind service 的连接器凭据是管理者拨号要用的：
配对时它生成、交给 hub 封存，之后它要一次、记在内存里（连接器不认了、重新配对、被别的 hub 配走时才再要），不是每次探测都过一遍总线。

**hub 重启**：管理者是 hub 的子进程，跟着重启。它停的时候先把在线机器的 `node.info` 和会话列表交给下一个（settings `hub.warm_machines`，和以前 hub 交的同一个键、同一个格式，所以升级后第一次重启也接得上），再断开连接；
新的 hub 起来先读这一份（不取走）显示机器（引擎也在里面），在管理者第一次上报之前对这些机器的调用等它（最多 30 秒），管理者起来取走这一份、30 秒内照常显示在线，同时把 node 连回来。
hub 的监听比管理者先起来：这几秒里来的 App 请求、管理者自己的路由等它（同样最多 30 秒），Persona Link 的链路和成员 hub 收到的中心的 node 流最多等 5 秒（Persona Link 一次握手给 8 秒），
等不到才回 503（Persona Link 照常退避再连）。过了这一段，管理者不在就立刻回 503，不再等。

和以前能看出来的差别：

- 管理者不在时机器离线、接口 503 assistant_offline（以前 hub 自己连着电脑，不受助手重启影响）。
- 一台机器连上时，Cua / ego 状态和扩展的程序比以前晚一点（hub 在 `devices.connected` 回答以后才开始）；`waitNodeIdle`（更新 node 前等空闲）看 node 列出的会话和管理者转给 hub 还没答的提问，不再看 hub 卡片上的「需要你」。
- `assistant.external`（管理者跑在别的机器上）时，电脑由那台去连：ssh 配置、tailcat key、`node --stdio` 的二进制、node 更新包都是那台的；
  hub 重启时它不跟着重启（电脑一直连着），hub 起来到它重新连上总线的那一两秒里，机器按 hub.json 显示为离线。

## 系统信息

node 启动时读一次，随 `node.info` 的 `system` 字段给 hub；hub 存进 `machine_systems`（机器离线、hub 重启后也知道它是什么系统），
放进 `Machine.system` 下发给 App。

| 字段 | Linux | macOS | Windows |
| --- | --- | --- | --- |
| `distro` / `like` / `name` / `version` | `/etc/os-release` 的 `ID` / `ID_LIKE` / `PRETTY_NAME` / `VERSION_ID`（没有就读 `/usr/lib/os-release`） | `macos`，`sw_vers` 的 ProductName + ProductVersion、BuildVersion | `windows`，注册表 `HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion` 的 ProductName + DisplayVersion（「Windows 11 Home 25H2」；Windows 11 的 ProductName 还写着 Windows 10，按 CurrentBuild ≥ 22000 改过来），`build` 是 CurrentBuild.UBR |
| `kernel` | `/proc/sys/kernel/osrelease` | `Darwin ` + `kern.osrelease` | `NT ` + `RtlGetVersion` 的版本号（`NT 10.0.26200`） |
| `model` | DMI `sys_vendor` + `product_name`（去掉「To be filled by O.E.M.」这类占位） | `system_profiler SPHardwareDataType` 的 Model Name（只取这一行，同一输出里有序列号），失败用 `hw.model` | 注册表 `HARDWARE\DESCRIPTION\System\BIOS` 的 SystemManufacturer + SystemProductName（同样去掉占位） |
| `cpu` | `/proc/cpuinfo` 第一个 `model name`（ARM 板子取 `Hardware` / `Model`） | `machdep.cpu.brand_string` | 注册表 `CentralProcessor\0` 的 ProcessorNameString |
| `cores` / `memory` / `boot_time` | `runtime.NumCPU`、`MemTotal`、`/proc/stat` 的 `btime` | `runtime.NumCPU`、`hw.memsize`、`kern.boottime` | `runtime.NumCPU`、`GlobalMemoryStatusEx` 的总物理内存、现在减去开机以来的毫秒数 |

系统图标（`OSBadge`）：macOS 是 SF Symbols 的苹果标，各 Linux 发行版是自己的官方标志（Simple Icons 的矢量图，`Theme.Brand.os`），
染发行版的颜色；不认识的发行版先看 `ID_LIKE`（Pop!_OS → Ubuntu），再不行是通用 Linux 的企鹅。矢量资源，任何尺寸、深浅色都清楚。
（2026-10-31 以前是画的「朋友圈」和字母，用户要求换成官方标志。）

## 采集口径

node 每 10 秒（`proto.MetricEvery`，对齐到整 10 秒，所有机器的点落在同一时刻）采一个样本，发 `metrics` 事件给 hub。
第一个样本没有 CPU 和网络速率（要两次读数相减），字段缺省（不是 0）。

| 指标 | Linux | macOS | Windows |
| --- | --- | --- | --- |
| CPU | `/proc/stat` 两次读数之差：忙 = 总 − idle − iowait（等 I/O 算空闲，同 top 的 id + wa；guest 已含在 user 里不重复加）；每核同样算，只放在最新样本里（不入库） | 常驻的 `iostat -n 0 -w 10`，取最新一行的 100 − id（过去 10 秒的平均；第一行是开机以来的平均，丢掉） | `GetSystemTimes` 两次读数之差：忙 = 内核 + 用户 − 空闲（内核时间里含空闲、DPC 和中断）；每核用 `NtQuerySystemInformation(SystemProcessorPerformanceInformation)` 同样算，只放在最新样本里；超过 64 个逻辑处理器（处理器组）时没有每核 |
| 负载 | `/proc/loadavg` 第一列 | `sysctl vm.loadavg`（二进制 struct loadavg） | 没有（Windows 没有负载平均），字段缺省 |
| 内存 | 已用 = MemTotal − MemAvailable（页缓存算空闲，同 `free` 的 available）；没有 MemAvailable 的老内核用 free + buffers + cached | 常驻的 `vm_stat 10`：已用 = 匿名页 − 可清除页 + wired + 压缩器占用，等于「活动监视器」的「已使用内存」（App 内存 + 联动内存 + 被压缩）；文件缓存算空闲 | `GlobalMemoryStatusEx`：已用 = 总量 − 可用（备用列表里的文件缓存算可用，同 Linux 的 MemAvailable） |
| 交换 | SwapTotal − SwapFree | `sysctl vm.swapusage` | 页面文件：`NtQuerySystemInformation(SystemPageFileInformation)` 各页面文件的大小和在用量之和 |
| 网络 | `/proc/net/dev` 物理网卡的收发字节之差 / 时间 | 路由 socket `NET_RT_IFLIST2`（`if_msghdr2` 的 64 位计数）的物理网卡之差 / 时间 | `GetIfEntry2Ex` 的 InOctets / OutOctets，物理网卡之差 / 时间；算哪些网卡每分钟列一次（列网卡要约 7 ms，读计数约 0.2 ms），网卡集合一变就丢掉这一个点 |
| 磁盘 | `/proc/self/mounts` 里每个本地块设备（`/dev/…`，去掉 loop、squashfs / iso9660 这类只读镜像，同一设备的 bind mount 只算一次）各 `statfs` 一次加起来：数据盘常挂在 `/data00` 之类，只看 `/` 一台 5 TB 的机器只显示 118 GB；每个的已用 = 总块 − 空闲块，总量 = 已用 + 普通用户可用（同 df） | `statfs("/System/Volumes/Data")`（`/` 是只读的系统快照，只有几 GB） | 每个落在磁盘上的固定盘（`GetDriveType` = `DRIVE_FIXED`，卷在某块磁盘上有长度大于 0 的区段，同一个卷只算一次）各 `GetDiskFreeSpaceEx` 一次加起来，已用 = 总量 − 空闲，总量 = 已用 + 用户可用（同 df）；网络盘、U 盘、光驱不算，Google Drive 这类同步客户端的虚拟盘也不算（它自称 FAT32 固定盘、容量照抄 C 盘，但卷在磁盘上的区段长度是 0）；盘符每分钟列一次 |

macOS 为什么起两个常驻进程：构建是 `CGO_ENABLED=0`，拿不到 `host_statistics` 这类 Mach 调用；`vm.page_*` 的 sysctl 缺匿名页和压缩器数字。
两个进程每 10 秒各打印一行，比每次采集都起 `vm_stat` / `top` 便宜得多；退出了就一分钟后重起，node 退出时一起结束（`exec.CommandContext`）。

Windows 不起进程：同样是 `CGO_ENABLED=0`，但 Win32 的这些调用纯 Go 就能做（`golang.org/x/sys/windows`，`GetSystemTimes` 和 `GlobalMemoryStatusEx` 经 kernel32 直接调）。

### 哪些网卡算

只算「真正出机器的」流量，虚拟网卡要么是本机内部的（lo、容器），要么在底下的物理网卡上又算了一遍（隧道、代理、网桥）：

- **Linux**：`/sys/class/net/<名字>` 指向 `/sys/devices/virtual/net/` 的都不算。内核把所有软件网卡放在那里：lo、docker0、veth*、br-*、
  virbr*、tun*（TUN 代理）、tailscale0、wg*、bond / VLAN（成员网卡照算，不会重复）。真网卡（包括虚拟机里的 virtio 网卡）挂在总线下面，算。
  没有 `/sys` 时按名字前缀排除（`sysmon.virtualName`：lo、docker、br-、veth、virbr、tailscale、tun、tap、wg、zt、cni、flannel、cali 等）。
- **macOS**：只算以太网类型（IFT_ETHER）、名字是 `en*`、不是回环的：Wi-Fi、网线、雷雳口。utun*（Tailscale、VPN、TUN 代理）、
  bridge*（雷雳网桥，成员 en* 已经算了）、awdl / llw（隔空投送）、anpi、gif、stf、ap 都不算。
- **Windows**：`MIB_IF_ROW2` 的 `HardwareInterface` 标志置位、`FilterInterface` 没置位的才算：Wi-Fi、网线、USB 共享网络。
  Tailscale（wintun）、VPN 适配器、Hyper-V / WSL 的 vEthernet、Wi-Fi Direct 虚拟网卡、蓝牙个人局域网、回环都是软件网卡，不算；
  叠在物理网卡上的过滤层（WFP、QoS）会重复它的计数，也不算。

两次读数之间计数变小（网卡没了、计数清零）就丢掉这一个点，不画一个巨大的尖峰。

### 开销（2026-10-30 实测，`jarvisd sysmon 30`）

| 机器 | 每次采集耗时 | personad CPU | 另外 |
| --- | --- | --- | --- |
| Mac Studio（M1 Ultra，20 核，macOS 26） | 0.55–0.70 ms | 51 ms / 5 分钟 = 0.017% | iostat + vm_stat 两个进程 2 小时 21 分钟共 0.34 s CPU（0.004%） |
| Linux 虚拟机（32 核，Ubuntu 24.04，6 块网卡） | 0.76 ms | 46 ms / 5 分钟 = 0.015% | 不起进程 |
| Windows 11 台式机（16 线程，4 个盘符、7 块网卡，2026-11-04） | 1.8 ms（每分钟列一次网卡和盘符约 8 ms，其余每次约 0.5 ms） | 47 ms / 5 分钟 = 0.016% | 不起进程 |

node 启动 5 分钟后在日志里记一次平均耗时（`metrics: a sample takes …`）。刚启动的 node 同时在扫 transcript，这个数会偏高
（同一台 Linux 上记到过 4.7 ms，单独跑 `jarvisd sysmon` 是 0.76 ms）。

## 存储和保留

hub 收到样本：最新一个留在内存（Dashboard 的「现在」，超过 30 秒没新样本就不算现在），同时写进 `machine_metrics`（`step = 10` 的原始行）。
`metricsLoop` 每分钟：

1. **降采样**：一小时前的原始行按机器、按分钟合成一行（`step = 60`）：平均值，总量（内存总量、磁盘总量等）取最大。
   删和插是一条语句（`WITH moved AS (DELETE … RETURNING *) INSERT … SELECT … GROUP BY`），同一分钟不会合两次。
2. **清理**：删掉 24 小时 10 分钟以前的所有行（多留 10 分钟，24 小时的图一打开左边就是满的）。

查询时按图的步长把两种行按覆盖时长加权平均（原始行权重 10、合成行 60），所以跨过「一小时前」那条线的曲线是连着的。

### 量

每台机器稳定在：最近 1 小时 360 行原始 + 之前 23 小时 10 分钟约 1390 行合成 ≈ **1750 行**；6 台约 **1.05 万行**。
实测一行数据 116 字节（`pg_column_size`），加上行头、行指针和主键索引约 200 字节，整表约 **2 MB**。写入：每台每天 8640 行原始 + 1440 行合成，
6 台每天约 6 万次插入、5 万次删除（autovacuum 会回收，表不会涨）。

## 断开

机器没发样本的时段在图上是断开的，不连线：

- hub 返回的 `points` 里，相邻两点隔了超过 3 个步长，就在中间插一个 `{"t": …, "gap": true}`；最后一个点离现在超过 4 个步长（机器现在离线）也在末尾插一个。
  比 3 个步长短的空档（偶尔丢一个样本）照常连上。各范围的步长：1 小时 10 秒、6 小时 1 分钟、24 小时 4 分钟（每张图约 360 个点）。
- App 画图时每段（两个 gap 之间）是一条单独的线（详情页 Swift Charts 的 `series:` 用「指标#段号」，卡片上的迷你曲线按同样的段号分成几条），所以线不会跨过空档；按住图看数值时，落在空档里不显示值。
- 卡片上的迷你曲线不用 Swift Charts，在 `Canvas` 上自己画（`Sparkline` / `SparklineGeometry`：单调三次曲线，和图表的 `.monotone` 同一种，一条线时下面带一层浅色）：
  一页十几个图表，每个图表自己的布局在 Mac 上每次进机器页要多花约 130 ms；向量化的 `LinePlot` / `AreaPlot` 也一样慢（开销在每个图表，不在点数，#390）。样子和原来一样，截图对照过。

## App

原来「机器」（Agent 运行数）和「机器监控」（Dashboard）是两个页面，机器详情里还要再点一层「系统与监控」才看到曲线；2026-11-01 按用户要求合成一个「机器」页，
「机器监控」这个名字和入口都去掉了。

- **入口**（都进同一个 `MachinesView`）：新外壳抽屉里的「机器」；设置 › 机器；Agents 列表左上角菜单最下面的「机器」（`MachineListRoute`，iPad 分栏里开在右栏）；
  Mac 侧边栏的「机器」（⌘5）。项目页 / 机器的项目页右上角 ⓘ 进机器详情（`MachineDetailRoute`）。
- **机器页**（`MachinesView`）：顶部一行「N/M 台在线 · K 台用量偏高 · J 个 Agent 需要你」。每台一张卡（`MachineCard`）：系统图标、名字、系统 · 核数 · 内存、
  在线 / 离线（离线显示最后在线时间和 ssh 的错误）；告警标签（下面「异常」）；Agent 数「需要你 / 工作中 / 空闲」（空闲 = 完成未看 + 已看 + 未知，
  只算活着的、不含助手的对话，和 Agents 列表一致）；CPU / 内存 / 网络的当前值和最近 1 小时的迷你曲线；personad 和 hub 不是同一次部署时的「重新部署」。
  iPad / Mac 上卡片按宽度排成多列。点卡片进机器详情。
  Web 的部署入口按 `can_deploy` 显示：旧 Link node 提示先本机升级一次，支持时显示等待空闲 / 传输 / 重启 / 版本确认；细节见 [node-update.md](node-update.md)。
- **排序**（`MachineConcern`）：需要注意的在前，依次是 离线 → 严重告警 → 注意告警 → 有 Agent 需要你 → 版本不一致；同一类和其余的按 hub 配置里的顺序，
  只有卡片上显示的东西变了才会换位置（换位置有动画）。
- **机器详情**（`MachineDetailView`）：一页看完，常用的在上面。顶部是同一张卡（不带数字，下面有）；离线时紧接着是「hub 通过 ssh … 接入」和「重新连接」。
  然后是 **Agent**：需要你的在前、工作中其次，最多先列 5 个，其余点「显示全部 N 个」，点一个进对话；下面一行「引擎」（这台显示哪个引擎的会话，点开选，`EngineSelectionView`）。然后是 **监控**：现在的 CPU / 内存 / 网络 / 负载 / 磁盘 / 交换，
  1 小时 / 6 小时 / 24 小时切换，CPU、内存、网络（下行 + 上行）、负载四张曲线（负载图上虚线标出核数），Linux 还有每个核心的柱子；
  按住任一张图拖动看某一时刻，四张图跟着同一时刻（共享选中时间），标题行显示那一刻的值和时间。然后是 **系统**（系统、内核、架构、机型、处理器、
  核心、内存、主机名、开机时间和已运行多久、herdr / Claude Code / node 版本），**启动器**，在线时最后是接入方式和「重新连接」。
- **更新**：机器的在线状态和 Agent 随 hub 推送实时变；数字在页面在屏幕上且 App 在前台时轮询：机器页每 10 秒，详情 1 小时范围每 10 秒、其余每分钟；
  进后台就停（`task(id: scenePhase)`）。下拉刷新（Mac 上是 ⌘R，`macRefreshes`）连机器列表一起重新取。
- **异常**（`MetricAlert`）：CPU ≥ 75% 注意、≥ 90% 严重；内存 ≥ 80% / 90%；磁盘 ≥ 80% / 90%；负载 ≥ 1 / 2 倍核数。卡片加一圈状态色的边，
  标签是图标（状态色）+ 文字（正文色），不单靠颜色。
- 旧 hub 没有这些接口（404）时机器页的卡片不带数字（Agent 数照常），详情的监控说「部署新版本的 hub 和 node 后再看」；旧 node 不发样本，数字是「—」。

### 图

按 dataviz 的规则：

- 一个指标一个颜色，机器页和详情一致；浅色 / 深色是各自选的色阶（`MetricColor`），用 `validate_palette.js` 验过两种模式
  （CPU 蓝、内存青绿、下行橙、上行紫、负载品红；网络两条线的色盲区分 ΔE > 25）。
- 线 2pt（迷你图 1.5pt），单条线下面铺 10% 的同色面积，两条线不铺；网格是一像素的细线；纵轴在右边、最多 3 个刻度，横轴最多 4 个时间。
- 数值用正文色，颜色只在线和图例的圆点上；两条线的图（网络）有图例（圆点 + 名字），一条线的图标题就是名字。
- 状态色（注意黄、严重红）只用于异常，不当普通系列色用。
- 每张图有 VoiceOver 描述（现在多少、最高多少）。

## 自检：这台电脑能做什么（#2429）

网页版机器页有一张「这台电脑能做什么」（照 Moshi 的 Doctor 页），把 `personad node doctor` 的结果（`docs/claude-integration.md`「自检」）按功能读给人看，而不是丢一串检查名。

- **取数**：hub 路由 `GET /api/v1/machines/{mid}/doctor`（只读）和 `POST …/doctor/fix`（重写 Persona 自己生成的文件，写完再检查一次，返回新的报告），都**只有 `full` 设备**（报告里有路径和用户自己的钩子命令，fix 会写电脑上的文件）；node RPC 是 `node.doctor`（只读，`{"fix":true}` 才写）和 `node.doctor.fix`（`node/doctor_rpc.go`），检查在 node 进程里跑，用的就是命令行的 `RunDoctor`，JSON 模型是同一个 `DoctorReport`。hub 的 GET 从不把 `fix` 传给 node。
- **按功能归并**（`web/src/lib/node-health.ts` 的 `FEATURES`）：手机审批卡片（`node.sock`、claude / assistant 的 settings 和 hooks、`command-paths`）、状态与通知（`node.sock`、`command-paths`）、终端与引擎（`herdr`）、MCP 工具（`node.sock`、`mcp.json`、`command-paths`）、技能（`agent-plugin`、`claude-version`）、运行时接入（`runtime.sock`）。
  一项功能取它那几个检查里最差的：有 `fail` 是「有问题」，有 `warn` 是「部分可用」，都正常是「就绪」。「终端与引擎」和「运行时接入」是可选的：只有 `warn` 时是「未启用」（写在「没有设置：…」里，不算需要处理），
  但「终端与引擎」在这台电脑的引擎是 herdr（含 herdr + Herdr App，没选过也算 herdr）时，herdr 连不上是「有问题」。node 多报的、没归进任何功能的检查，不正常时进「其他检查」，不会藏起来。
  `global-permission-hooks`（用户自己的同步 PermissionRequest 钩子）不算功能，作为一行说明列在功能下面，只看不改。
- **界面**：顶部「一切正常」或「N 项功能需要处理」（Moshi 的 Everything works / N feature(s) need attention），下面一行「没有设置：…」；「查看全部检查（N）」展开逐项（状态、名字、发现了什么、怎么办、哪些文件不同）；「重新检查」；
  有 `stale` / `missing` 的生成文件时出现「修复」，点了先确认一次（说明会重写哪几个文件、不改用户自己的 Claude Code 设置）。打开页面时检查一次，不轮询。
- **旧 node**：没有这个 RPC 的 node 回 `unknown method`，卡片写「这台电脑的 node 太旧，更新后可以在这里自检」，旁边是机器页已有的部署按钮（同一个 `POST …/deploy`，同样的 `can_deploy` 判断）；旧 hub 没有这条路由（404）和非 `full` 设备（403）各有一句说明。离线的电脑不发请求。
- **连不上时的分类**（`classifyConnection`）：机器卡片和机器页离线时，按 hub 记下的错误文本分「认证没通过 / 连不上这台电脑 / 连接超时 / 版本不一致」，各配一句下一步（ssh 接入和 Persona Link 的措辞不同）；
  文本认不出来就什么都不加，不猜。计划内的重启（部署中）不算。
- 截图用假 hub：`MOCK_DOCTOR=healthy|stale|broken|mixed|old|error|slow`、`MOCK_DOCTOR_OLD=<机器 id,…>`（`scripts/mock/mock_hub.py`）。
- 测试：`node/doctor_rpc_test.go`（临时目录；不带 fix 时不写任何文件、fix 只重写生成的文件且不碰用户的 settings.json、检查名和网页的归并表对得上）、`hub/node_doctor_test.go`（权限）、`web/src/lib/node-health.test.ts`（归并、总结、修复、旧 node、连接分类）。

## API

见 `docs/protocol.md`：`GET /api/v1/metrics`（机器页）、`GET /api/v1/machines/{id}/metrics?range=1h|6h|24h`（详情），
node → hub 的 `metrics` 事件，`node.info` / `Machine` 里的 `system`。

## 验证

- Go：`sysmon` 的夹具测试（`testdata/` 里的 /proc/stat、meminfo、net/dev、os-release、cpuinfo、vm_stat、iostat、sw_vers、system_profiler），
  `db.TestMachineMetrics`（降采样、加权平均、空档没有点、清理），`hub.TestMetricGaps`（断开标记）；`./scripts/test/test-server.sh`。
- 真实采集：本机起一个测试 hub（假 HOME、独立数据库经 SSH 隧道，一台 `kind: local` 的 Mac 加一台 ssh 的 Linux），模拟器配对它看机器页和详情。
- 假数据：`scripts/mock/mock_hub.py` 有 4 台机器：服务器最近 25 分钟高负载（CPU / 内存 / 负载告警），Mac Studio 早上断开过两小时（空档），
  NAS 三小时前离线。UI 测试 `MachinesPageTests`：从机器菜单打开机器页（离线的 NAS 在最前、服务器的告警、MacBook 的「需要你」）、进服务器详情
  （Agent 在曲线上面、没有「系统与监控」这一层）、切到 24 小时、按住网络图、看系统信息、点 Agent 进对话；离线机器的详情（「重新连接」在第一屏）。
  排序的单元测试 `MachineConcernTests`。36 个 Agent 的「显示全部」在 `MachinePagesTests.testMachineProjects`，「重新部署」在 `DeployButtonTests`。
- 截图：`-tab dev -openMonitor all`（机器页）、`-openMonitor <机器 id> [-openMonitorRange 6h|24h] [-openMonitorScrub 0.55]`（详情，按住在范围的 55% 处）；
  `-tab settings -openMachines YES [-openMachineDetail <机器 id>]` 从设置进。
