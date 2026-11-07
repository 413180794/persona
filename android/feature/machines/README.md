# 机器监控

独立模块 `:feature:machines`，实现机器卡片、详情、运行中的会话列表，以及最近 1、6、24 小时的监控图。复用现有 `SessionsRepository`，不改 `android/app/`、Agents 页或 iOS。

机器计数和会话列表排除助手会话。CPU、内存、磁盘和负载告警沿用 `docs/server/machines.md` 的阈值；网络直接显示 node 汇总的物理网卡速率。历史内存字段按字节读取，不当成百分比。缺失采样不补零，过期或离线采样不显示为当前值。HTTP 404 会提示检查机器及升级 hub、node。

图表使用 Compose Canvas。每遇到 `gap`、缺失指标或超出允许间隔，就另起路径；长按选值不会跨过空档。绘图区域用 `clipRect` 裁剪，坐标文字显式使用主题的次要文字色。CPU、内存、下行、上行和负载沿用 iOS 的深浅色系列色。四张图共享选中时间，网络保留双线图例，负载虚线标记核数，Linux 显示每核柱状。

页面仅在生命周期为 `RESUMED` 时轮询，刷新间隔来自协议设计配置，不能当作实测调用频率。运行性能未实测。

## 当前 App 接线

App 已依赖 `:feature:machines`，`Root` 注册总览／详情，Agents 机器菜单已有「机器监控」；`AppServices` 每次操作按当前活动路由创建指标 API，复用钉扎客户端、当前凭据及共享 SessionsRepository。真实指标、图表和会话跳转仍待验收。

本模块没有增加机器管理操作（启动器列表、重新连接），这些不属于监控和会话列表范围。

## 验证

相关模块编译；`MachinesTest` 覆盖空档、缺省值、过期采样、告警、会话过滤、范围切换及取消轮询。`MachineMetricsApiTest` 覆盖鉴权、路径编码、历史步长、旧 hub 和失效配对。仅使用本地假服务，不连接线上 hub。

构建时使用 Android Studio 自带的 JDK 21。沙箱无法写 Kotlin daemon 的默认目录时，可在命令行加 `-Pkotlin.compiler.execution.strategy=in-process`，不需要改公共构建配置。
