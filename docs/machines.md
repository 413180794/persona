# 机器监控

每台电脑是什么系统、现在忙不忙、过去 24 小时的曲线。node 采集，hub 落 Postgres，App 里在「机器」页和机器详情看（和 Agent 运行数放在一起）。

| 部分 | 代码 |
| --- | --- |
| 采集（解析 /proc、vm_stat、iostat、sw_vers 等） | `server/internal/sysmon/`（`sysmon.go` 纯解析，`linux.go` / `darwin.go` 读系统） |
| node 发送 | `server/internal/node/metrics.go` |
| hub 存储、降采样、清理、API | `server/internal/hub/metrics.go`、`server/internal/db/metrics.go`、迁移 `0051_machine_metrics.sql` |
| App | `apple/Persona/Views/Machines/`（`MachinesView` 机器页、`MachineDetailView` 机器详情、`MetricCharts`、`OSBadge`），模型 `apple/Shared/MachineMetrics.swift` |
| 本机看采集结果 / 开销 | `jarvisd sysmon [n]`：打印系统信息和 n+1 个样本（每 10 秒一个），最后一行是每次采集耗时和 personad 的 CPU |

## 系统信息

node 启动时读一次，随 `node.info` 的 `system` 字段给 hub；hub 存进 `machine_systems`（机器离线、hub 重启后也知道它是什么系统），
放进 `Machine.system` 下发给 App。

| 字段 | Linux | macOS |
| --- | --- | --- |
| `distro` / `like` / `name` / `version` | `/etc/os-release` 的 `ID` / `ID_LIKE` / `PRETTY_NAME` / `VERSION_ID`（没有就读 `/usr/lib/os-release`） | `macos`，`sw_vers` 的 ProductName + ProductVersion、BuildVersion |
| `kernel` | `/proc/sys/kernel/osrelease` | `Darwin ` + `kern.osrelease` |
| `model` | DMI `sys_vendor` + `product_name`（去掉「To be filled by O.E.M.」这类占位） | `system_profiler SPHardwareDataType` 的 Model Name（只取这一行，同一输出里有序列号），失败用 `hw.model` |
| `cpu` | `/proc/cpuinfo` 第一个 `model name`（ARM 板子取 `Hardware` / `Model`） | `machdep.cpu.brand_string` |
| `cores` / `memory` / `boot_time` | `runtime.NumCPU`、`MemTotal`、`/proc/stat` 的 `btime` | `runtime.NumCPU`、`hw.memsize`、`kern.boottime` |

系统图标（`OSBadge`）：macOS 是 SF Symbols 的苹果标，各 Linux 发行版是自己的官方标志（Simple Icons 的矢量图，`Theme.Brand.os`），
染发行版的颜色；不认识的发行版先看 `ID_LIKE`（Pop!_OS → Ubuntu），再不行是通用 Linux 的企鹅。矢量资源，任何尺寸、深浅色都清楚。
（2026-10-31 以前是画的「朋友圈」和字母，用户要求换成官方标志。）

## 采集口径

node 每 10 秒（`proto.MetricEvery`，对齐到整 10 秒，所有机器的点落在同一时刻）采一个样本，发 `metrics` 事件给 hub。
第一个样本没有 CPU 和网络速率（要两次读数相减），字段缺省（不是 0）。

| 指标 | Linux | macOS |
| --- | --- | --- |
| CPU | `/proc/stat` 两次读数之差：忙 = 总 − idle − iowait（等 I/O 算空闲，同 top 的 id + wa；guest 已含在 user 里不重复加）；每核同样算，只放在最新样本里（不入库） | 常驻的 `iostat -n 0 -w 10`，取最新一行的 100 − id（过去 10 秒的平均；第一行是开机以来的平均，丢掉） |
| 负载 | `/proc/loadavg` 第一列 | `sysctl vm.loadavg`（二进制 struct loadavg） |
| 内存 | 已用 = MemTotal − MemAvailable（页缓存算空闲，同 `free` 的 available）；没有 MemAvailable 的老内核用 free + buffers + cached | 常驻的 `vm_stat 10`：已用 = 匿名页 − 可清除页 + wired + 压缩器占用，等于「活动监视器」的「已使用内存」（App 内存 + 联动内存 + 被压缩）；文件缓存算空闲 |
| 交换 | SwapTotal − SwapFree | `sysctl vm.swapusage` |
| 网络 | `/proc/net/dev` 物理网卡的收发字节之差 / 时间 | 路由 socket `NET_RT_IFLIST2`（`if_msghdr2` 的 64 位计数）的物理网卡之差 / 时间 |
| 磁盘 | `/proc/self/mounts` 里每个本地块设备（`/dev/…`，去掉 loop、squashfs / iso9660 这类只读镜像，同一设备的 bind mount 只算一次）各 `statfs` 一次加起来：数据盘常挂在 `/data00` 之类，只看 `/` 一台 5 TB 的机器只显示 118 GB；每个的已用 = 总块 − 空闲块，总量 = 已用 + 普通用户可用（同 df） | `statfs("/System/Volumes/Data")`（`/` 是只读的系统快照，只有几 GB） |

macOS 为什么起两个常驻进程：构建是 `CGO_ENABLED=0`，拿不到 `host_statistics` 这类 Mach 调用；`vm.page_*` 的 sysctl 缺匿名页和压缩器数字。
两个进程每 10 秒各打印一行，比每次采集都起 `vm_stat` / `top` 便宜得多；退出了就一分钟后重起，node 退出时一起结束（`exec.CommandContext`）。

### 哪些网卡算

只算「真正出机器的」流量，虚拟网卡要么是本机内部的（lo、容器），要么在底下的物理网卡上又算了一遍（隧道、代理、网桥）：

- **Linux**：`/sys/class/net/<名字>` 指向 `/sys/devices/virtual/net/` 的都不算。内核把所有软件网卡放在那里：lo、docker0、veth*、br-*、
  virbr*、tun*（TUN 代理）、tailscale0、wg*、bond / VLAN（成员网卡照算，不会重复）。真网卡（包括虚拟机里的 virtio 网卡）挂在总线下面，算。
  没有 `/sys` 时按名字前缀排除（`sysmon.virtualName`：lo、docker、br-、veth、virbr、tailscale、tun、tap、wg、zt、cni、flannel、cali 等）。
- **macOS**：只算以太网类型（IFT_ETHER）、名字是 `en*`、不是回环的：Wi-Fi、网线、雷雳口。utun*（Tailscale、VPN、TUN 代理）、
  bridge*（雷雳网桥，成员 en* 已经算了）、awdl / llw（隔空投送）、anpi、gif、stf、ap 都不算。

两次读数之间计数变小（网卡没了、计数清零）就丢掉这一个点，不画一个巨大的尖峰。

### 开销（2026-10-30 实测，`jarvisd sysmon 30`）

| 机器 | 每次采集耗时 | personad CPU | 另外 |
| --- | --- | --- | --- |
| Mac Studio（M1 Ultra，20 核，macOS 26） | 0.55–0.70 ms | 51 ms / 5 分钟 = 0.017% | iostat + vm_stat 两个进程 2 小时 21 分钟共 0.34 s CPU（0.004%） |
| Linux 虚拟机（32 核，Ubuntu 24.04，6 块网卡） | 0.76 ms | 46 ms / 5 分钟 = 0.015% | 不起进程 |

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
