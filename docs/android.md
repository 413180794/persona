# Android App

`android/`：Kotlin + Jetpack Compose 的 Android 客户端，连同一个 hub，接口和 iOS 一样（`docs/protocol.md`），服务端不用改。
minSdk 31（Android 12），compileSdk 37，targetSdk 36。界面中文。

一句话描述：在 Android 手机上看各台电脑终端引擎（herdr、Herdr App 或 Orca，每台电脑选一个，`docs/engines.md`）里的 Agent、发消息、回答提问 / 审批计划 / 授权、按终端对话框的选项，
不需要任何推送账号。

## 当前实现

本页按 2026-10-31 的提交 `f3a13fdb7672927197cb8ac38bccb9c65671677b` 核对。当前是多模块重设计版本；逐项状态见 [Android P0 状态](redesign/android-p0-status.md)，旧版验证记录留在后文，不能代替本版验收。

| 入口 | 当前实现 |
| --- | --- |
| 配对 | 扫码、手填、配对深链，临时 token 换设备 token；TLS 公钥钉扎、候选探测和重新配对；完成后进入准备清单 |
| Jarvis | 首页、主对话、历史抽屉和共用对话页；主对话首句可先进入持久 outbox，恢复连接后解析目标；助手身份确定后保留在 Jarvis tab |
| Agents | 页面最上面是搜索框，下面一排电脑芯片（全部机器 + 每台电脑，带在线状态和计数，可横滑），再下面是快速筛选；默认按电脑 → 空间（herdr workspace）→ 会话三层树，靠缩进、字号字重和细线分层，不给每层垫底板（`docs/android-design.md` 2.4 节），电脑和空间都能折叠、栏头带计数；全是已看会话的空闲空间留在所属电脑下面、排在有动静的空间后面，默认收起、样式弱一点，收起时标出会话数；也可按状态分组；置顶 / 屏蔽、请求卡和作答；会话重命名（标签名走 herdr，会话名走 Claude 的 /rename，入口在会话行菜单、空间页和对话页菜单）和空间重命名（空间栏头菜单）发出后列表、空间页、对话页标题立即更新，失败回滚；置顶、屏蔽、重命名、关闭由 App 的 `AgentsWiringModule` 接到 hub |
| 对话 | 连续性补洞、历史分页、实时 delta、折叠工作过程、Markdown、diff、待办、图片缓存、阅读锚点、待发气泡及人工核对；附件和会话控制需要补齐服务注入 |
| 收件箱 | 飞书和邮件事项统一列表、筛选持久化、AI 操作、回复日志；飞书有窗口缓存、事件刷新、可见已读和基础消息渲染；邮件正文页已有，但 MailTransport 尚未接到 App |
| 设置 | 配对、准备清单、后台引导、关于和更新；连接诊断里可以手动添加 / 编辑 / 删除 hub 地址、停用候选、拖动排序（列表顺序就是尝试顺序）、逐条测试（见「连接地址」）；设备管理与通知偏好页已有，但 DeviceSettings 尚未注入 |
| 扩展模块 | 终端、文件、机器监控、知识库、手机工具和体验模式已接入 App，入口见「外壳」一节；体验模式没有示例数据的页面显示说明，不连真实 hub |
| 界面 | 三套界面风格：玻璃（默认）、晨报、看板，在设置 › 界面风格切换，立刻生效并存在本机；深浅色跟随系统。设计规范、组件和写页面的规矩见 [Android 界面风格与设计系统](android-design.md) |

## 通知（不用推送服务）

Android 使用 `:platform:notify` 的前台服务共用 hub WebSocket，不依赖 FCM 或厂商推送。通知渠道、开机恢复、网络重连、保活调度和国产系统准备清单已有实现。

真机记录（2026-10-31，ColorOS 16 / Android 16，单次观察）：前台服务在跑，App 切到后台约 6 秒 WebSocket 仍被断开，回前台自动重连；要在系统设置里给 App 打开「允许后台运行」「自启动」并把电池设为不优化（准备清单「后台保活」会引导）。放开后后台能连多久、耗电多少按验收清单 V03 实测，现在未实测。

- 「做完了」由 hub 按与 iOS 相同的完成策略决定，经 `notify` 事件下发；客户端声明 `ws_notify` 后接收，不再自行推算完成。
- 提问、计划、授权和屏幕选项可在通知作答；允许或批准要求解锁，拒绝或打回可输入原因。结果不明时提示待确认，不自动重复批准。
- 点击通知和会话深链走同一个导航入口（冷启动和 App 已打开时相同）。通知链接写明 `tab` 时直接按它落到 Jarvis 或 Agents，不等会话元数据；没写时等元数据到达再分流。终端兜底动作（`target=terminal`）打开该会话的终端，返回落在原对话。
- 准备清单提供通知权限、电池限制、自启动等系统入口，以及锁屏测试预约、展示和回报。各 ROM 的锁屏存活、通知展示、重启恢复和耗电均未实测。

## 连接地址（设置 › 连接诊断）

2026-11-01 用户要求：「当前连接」和「候选地址」要能添加和编辑。hub 不一定发布自己的全部地址（比如 NAS 的网卡挂在虚拟交换机上，被 hub 当成虚拟网卡，它的 IPv6 和内网 https 地址就不会下发），这时在手机上手动补。语义和电脑链路的「手动加地址」一样（[link.md](link.md)），区别是 Android 上这些设置跟着配对走。

候选地址有三个来源，列表里逐条标明：

- 配对时：配对链接里的 `url`、`alt` 和由 `url` + `tls` 拼出的 https 地址，以及配对时读到的 `/api/v1/endpoints`；
- hub 下发：鉴权后的 `/api/v1/endpoints`、WS `hello` 和 `endpoints` 事件（[协议](protocol.md#候选地址)）。hub 的列表一到就替换配对时的那批；
- 手动添加：只存在这台手机上，hub 的列表替换不到它们。

能做的事：

- 添加：`https://域名:端口`、`https://[IPv6]:端口`、`https://IPv4:端口`。没写协议按 https，端口必须写，路径、`?`、`#`、用户名都不收，IPv6 要加方括号。校验不查 DNS、不联网（`core:hub` 的 `checkEndpoint`），错误用中文显示在输入框下面，合法时预览将保存的规范写法（`scheme://host:port`，端口总是写出来）。每个配对最多手动加 10 条，和已有候选重复（按规范写法比较，`https://host:port/` 和 `https://HOST:port` 算同一个）的不收。
- 安全规则不变：https 地址和配对时一样按配对时的公钥指纹钉扎；配对没有指纹（hub 没开 TLS）时不能加 https 地址。`http://` 只能是内网：IP 字面量要过 `LocalAddressPolicy`（回环、私网、链路本地、ULA），名字只认 `localhost` 和 `.local`；运行时 `HubHttpClient` 照旧再按解析出的对端地址拦一次（[network.md](network.md)）。
- 手动加的可以编辑（位置和停用标记不变）、删除（要确认）；配对时的和 hub 下发的不能改，只能停用、重新启用；手动加的也能停用。至少要留一条启用的地址。「正在使用」显示当前连着的那条。
- 每条都能「测试」：用新的连接池带钉扎请求 `GET /api/v1/health` 并核对 hub_id，显示结果和耗时。耗时包含建立连接，https 还包含 TLS 握手；是单次样本，界面标「单次」，不作为性能基线。结果只留在内存里。列表里每条还显示连接自己选路时的探测结果（标「自动探测」），两者显示较新的那个。

**列表的顺序就是尝试的顺序**（2026-11-02 用户要求：「候选地址要支持用户拖动，显示顺序就是尝试的优先级」）。列表怎么排，连接就按什么顺序试，没有看不见的重排：原来的「优先使用」和「重连时先试上次在同一网络连上的那条」都去掉了。

- 调顺序：按住行左边的手柄直接拖，或者长按一行拖；拖到列表上下边缘会自动滚动。行的菜单里有「上移」「下移」，无障碍服务里也有同名操作。手柄上的数字是这一条第几个尝试（停用的不编号）。
- 没调过顺序时按默认规则排（`orderEndpoints`）：公网 IPv6 → 域名 → 内网 → 公网 IPv4 → 其他；同类里 https 在 http 前；再同类里手动添加的在配对时 / hub 下发的前；最后按原来的顺序（手动的按添加先后，其余按 hub 给的顺序）。
- 调过以后以用户的顺序为准（`arrangeEndpoints`）：hub 新发来的地址和新加的手动地址排到最后；hub 不再提供的地址从列表里消失（IPv6 前缀变了的地址算新地址，排到最后）。「恢复默认顺序」（要确认）回到默认规则。
- 停用的地址留在原位、变灰、标「已停用」，连接时跳过；仍然可以拖，重新启用后还在原来的位置。

连接怎么试：只试启用的地址，严格按列表顺序起步。每一条等前一条失败、或者前一条开始满 1 秒（`HubRouteSelector.STEP_MILLIS`）再开始，谁先通过身份校验用谁；所以排在后面的只有在前面那条单独试过 1 秒以后才可能抢先。连上以后每 5 分钟（`OkHttpHubConnection.RECHECK_MILLIS`）看一次排在当前地址前面的那些地址，每条最多等 1 秒；有一条答上来就重新连接（这时整轮竞速选出第一个答上来的），连接自己从不往后换。拖动或恢复默认顺序只改顺序，不会马上探测或切换：连接在下一次重连或下一次 5 分钟复查时按新顺序走，拖动不会把正在用的连接断掉（2026-11-02 用户定）；只有在用的那条被停用、删除或改掉时才直接重连。1 秒和 5 分钟都是配置值，未实测。

存储：手动地址、停用标记和用户的顺序按配对保存（`ScopedHubData.endpoints`，配对目录里的 `endpoints.enc`，和凭据一样用 Tink AEAD 加密），重启后还在；hub 下发新列表不改它们；退出、撤销、重新配对时随配对目录一起删除（`eraseScope`）。文件读不出来时按「默认顺序、没有手动地址」处理，不挡住整个配对（凭据文件读不出来则保留原文件并报错）。上一版存的「优先使用」读出来变成「这一条排第一，其余按默认规则」的顺序。

代码：校验、规范写法、类型和顺序在 `core/hub/.../HubEndpoints.kt`；竞速在 `HubRouteSelector`，复查在 `OkHttpHubConnection`；设置页的控制器 `HubEndpointSettings`（实现 `core:contracts` 的 `ConnectionEndpoints`），由 `ConnectionRuntime.endpoints` 交给设置（`PairingModule`）；存储 `core/data/.../EndpointOverridesStore.kt`；页面 `feature/settings/.../EndpointsSection.kt`、`EndpointEditorState.kt` 和拖动 `EndpointDrag.kt`（候选行用 `JRow` 的插槽写法，三套风格的边距自动对齐）。体验模式没有这份服务，连接诊断保持只读。

截图：`MOCK_ENDPOINTS=1 python3 scripts/mock/mock_hub.py --tls-port 0` 让假 hub 额外下发两条连不上的文档地址（`[2001:db8::10]` 和 `hub.example.com`），用来看失败的测试和停用；带指纹配对要用 `jarvis://pair?url=…&token=mock&hub=<hub_id>&fp=<fp>&tls=<TLS 端口>`，`hub_id` 读假 hub 的 `/api/v1/health`，`fp` 和端口看它启动时打印的「TLS 端口=… fp=…」（`debug.mockAddress` 只配明文地址，没有指纹就加不了 https 地址）。

## 多 hub（同时连接多组 hub）

#295，方案和各路的接口约定见 [plan-hub-mesh.md](plan-hub-mesh.md) 第 4 节和 2.6 节，iOS 是 #256。手机可以配对多台 hub：
互相连通（hub 互联）的几台算一**组**，同组只保持一条连接、组内换线路；不同组（工作 / 生活这类要隔离的环境）各一条连接，互不影响。
Agents、机器、要你处理的请求把各组合在一起显示；Jarvis、收件箱和 hub 相关的设置是每组自己的，用「选中的组」。

### 原来默认只有一台 hub 的地方

改之前（2026-11-02，集成分支 `679b227f`）App 处处假定只有一份配对。逐项和现在的做法：

| 地方 | 原来 | 现在 |
| --- | --- | --- |
| `core:contracts` 的 `HubScope` | 一份配对一个 `hubId/generation`，数据目录 `hub-data/<hubId>/<generation>/` | 不变；每组的数据放在一个这样的目录（组的 storage），凭据和候选地址仍按配对存 |
| `EncryptedCredentials` / `HubDataStore` | `active.enc` 指向唯一的配对；`pair()` 丢掉旧的再配新的；`restore()` 返回唯一的作用域 | 新的组注册表 `hub-data/groups.enc`（`HubGroupRegistry.kt`），同时打开每组的作用域（`openGroup` / `closeGroup`）；`restore()` 返回选中的组；`pair()` / `unpair()` 签名不变 |
| 凭据、候选地址、钉扎指纹（`credentials.enc`、`endpoints.enc`） | 一份 | 每台已配对的 hub 一份（它自己的配对目录） |
| `ConnectionRuntime` | 一条 `OkHttpHubConnection`、一份 `ScopedHubData`、一个兼容用的 `Hub` | 每组一个 `GroupRuntime`（`di/GroupRuntime.kt`）；原来的公开 API 不变，指向选中的组 |
| `OkHttpHubConnection` / `HubHttpClient` | 一台 hub 的 id、指纹、token | 组内多台：`HubConnectionConfig.members`，每台自己的 id、指纹、token 和候选地址（`HubMembers.kt`） |
| `PairingModule` | 配对就是替换 | 原来的配对页仍是替换；设置 › hub › 添加 hub 是 `WiredPairingService.addHub` |
| `ChatDataOwner`、`AgentsWiringModule`、`AppServices` 的终端 / 文件 / 动作 / 机器监控 | 按唯一的作用域构造 | 每组一份（`GroupStacks`），再合并（`MergedServices.kt`） |
| `InboxDataOwner`（收件箱、Jarvis 会话列表）、设备设置、检查更新、锁屏测试、分享、手机数据工具、知识库 | 唯一的 hub | 选中的组 |
| 连接诊断的候选地址 | 唯一的配对 | 选中的组自己的 hub（组里第一台已配对的 hub）；同组别的 hub 的候选也参与选路，诊断里一起列出 |
| `Notify.install(connection.events)`、前台服务 | 一条 WebSocket 的事件 | 各组事件合成一条流（ID 已加后缀，见下）；常驻通知的状态是选中组的，Agent 计数各组相加 |
| `ScopedListPreferences`（Agents 列表设置）、对话阅读位置 | 唯一的作用域 | 第一组的作用域：切换选中组不会动它们 |

### 分组和组 id

- 组注册表（`GroupsDocument`）记每组的 id、名字（用户改的）、storage 作用域、已配对的 hub（`hubs`，第一台是这组连接自己的 hub）、
  互联发现但没配对的 hub（`mesh`：地址、指纹、名字），以及 `primary`（主组）和 `selected`（选中组）。加密方式和凭据一样（Tink AEAD），读不出来时保留原文件并报错。
- **分组**：鉴权后的 hello 带 `mesh.hubs`（plan 2.6），是这台 hub 所在连通分量的全部 hub。收到一次就更新各 hub 的 peers：列表里的互相列为 peers，列表外的去掉它们列过的列表成员（新的 hello 能纠正别的 hub 留下的旧信息）。
  两台已配对的 hub 只要一方的 peers 里有另一方就是同一组，组就是连通分量（`learn` / `regroup`，纯函数，`HubGroupRegistryTest`）。没有 `mesh` 字段的 hub（L1 合并前的 hub）只更新名字，自成一组。
- **组 id** 在手机上生成（`g` + 8 位十六进制），稳定：合并时保留最早的 id（含主组成员的一方优先拿主组 id），拆开时含原组第一台 hub 的一方保留 id，拿不到 id 的一方用新 id 和新的空作用域。
- **合并不丢东西**（主持人 2026-11-02 要求）：被并掉的组的数据先不删，在注册表里记一条 `PendingMove`（它的作用域、并进了哪一组）。等它关掉、合并后的组打开，就把它**还没发出的消息**（除了已显示、已取消的都算；正在发送中被打断的记为「结果不明」，先向 hub 核对再决定重发）和**有内容的草稿**搬进合并后的组：消息保留原来的 `client_msg_id`（hub 按它去重，送达语义不变），机器和会话 id 不改（互联后机器 id 在组里稳定，plan 2.6），合并后的组里同一会话已经有内容的草稿不会被覆盖。搬完才删那个作用域（它的已配对 hub 保留凭据）；缓存不搬，重新拉。搬失败（比如数据库读不出来）就留着，下次注册表变化或下次启动再试，不会静默清掉；搬到一半崩了，重搬不会重复（消息按 `client_msg_id` 去重，草稿不覆盖）。用户自己断开的组和吊销的组不搬（断开时确认框写明会清除本地数据）。代码：`GroupMerge.kt`、`HubDataStore.unsentAt`、`ConnectionRuntime.moveUnsentLocked`。
- 拆开（一组变两组）时，拆出去那一组用新的空作用域；还在原组待发队列里、属于拆出去那台 hub 的电脑的消息不搬，发送会失败并显示出来（不会静默消失）。
- **配对一台已经在某组里的 hub**（它在某组的 `mesh` 里，或某台 hub 的 peers 里有它）自动并进那一组，不重复显示；同一台 hub 再配对只换它的凭据，组和数据都留着。
- **迁移**：第一次启动新版本时，旧的单份配对原地变成主组：storage 和唯一的 hub 都是原来的配对目录，不搬文件。数据库表本来就按配对作用域分（`scope` 列）、每组一个目录，所以**没有 Room 的表结构迁移**，`ItemsDatabase` / `PersistentDatabase` 的版本号不变；迁移测的是注册表（`HubGroupRegistryTest`、仪器测试 `HubGroupsDataStoreTest`）。

### 键：机器 id 的组后缀

plan 第 4 节「会话键要能区分组」的 Android 做法（主持人 2026-11-02 定）：

- **主组**（旧的单份配对迁移来的组，或第一次配对的组）的机器 id、会话键、路由、深链、通知和现在逐字节一样。**其余每组**的机器 id 在合并列表里带稳定的 `~<组 id>`（`GroupKeys`，如 `macbook~g0a1b2c3d`），会话键因此是「组 + 机器 + 会话」。后缀只认合法的组 id，hub 自己的撞名后缀 `@<hub>` 是机器 id 的一部分，照样保留。
- 后缀**不跟选中组走**：切换 Jarvis / 收件箱用的组，任何键都不变，打开着的页面、返回栈、通知深链、草稿、置顶、缓存都不错位。主组断开后别的组保持原样，不提拔新主组；全部断开后，下一次配对的组重新成为主组。
- 交互（要你定的事）的 id 是各台 hub 自己起的，两台 hub 可能用同一个（两个假 hub 的夹具就完全一样，列表因为 LazyColumn 的 key 重复直接崩过）：非主组的交互 id 也带同样的后缀，作答时去掉；`interaction_resolved`、`notify` 的 `interaction_id`、fleet 条目里的交互 id 一起改。
- 每组自己的仓库、数据库、传输只用不带后缀的 id。门面（`GroupNames`、`MergedServices.kt`、`GroupTranslation.kt`）在往上给列表时加后缀，在把调用交给那一组时去掉。这样 `Session` / `Interaction` / `SessionKey` / `ChatActions` 的签名都不用改，feature 页面、路由、深链、通知几乎不用动。
- 取舍：没有像 iOS 那样给 `SessionKey` 加字段。那样每个从路由、深链、通知构造键的地方都要带上组，漏一处就串组；后缀把组编进机器 id，经过哪里都不丢。代价是门面要逐个接口翻译（列表、事件、偏好路径、草稿键、创建会话的结果……），新增带机器 id 的接口时要记得在 `GroupNames` 里补上。

### 连接和换线路

- 每组一条 `OkHttpHubConnection`。组里第一台已配对的 hub 是它自己的 hub，其余已配对的和互联发现的 hub 是 `members`：它们的候选地址排在自己的后面，一起走现有的选路规则（严格按顺序、1 秒步进，5 分钟复查只会往前换，`HubRouteSelector`）。连着的那台掉了，下一轮选路就落到组里别的 hub；它恢复后，复查把连接换回去。
- 每个候选按它那台 hub 的指纹钉扎、核对 hub_id；成员的指纹来自鉴权后的 hello（可信），组里别的 hub 的证书冒充不了这一台（握手时按 host:port 核对，OkHttp 对自签证书不给 `handshake.peerCertificates`）。请求和 WebSocket 用当前线路那台 hub 的凭据：配对过的用自己的，互联发现的借用组凭据（plan 2.6「凭据在组里通用」）。
- 一台 hub 拒绝自己的凭据（401）：只把它从这组里去掉（注册表里删掉它的配对、重开这一组），别的 hub 照用；一台都不剩才算这组失效。只拒绝借用的组凭据的 hub 只是不再尝试，不删任何东西。最后一组失效时和原来只有一份配对时一样：清掉全部数据，状态是「配对失效」。
- 不同组各一条连接，前台服务一直开着的是这几条。断开一组只关它的连接、清它的数据，别的组不受影响。

### 界面

- **只有一组时界面一像素不变**，唯一的例外是设置首页「连接」一节多了一行「hub」（要从这里添加第二台，主持人 2026-11-02 定）。三套风格 × 深浅色逐像素比过，见下面的验证。
- 多于一组时：Agents 的电脑栏头多一个组标签（`AgentTreeHeader(tag)`）；会话行的机器、请求卡、电脑芯片的名字写成「MacBook（家里）」；搜索框下面多一排组筛选（全部 hub + 每组，带在线点和会话数），筛选同时作用于列表和「等你定」。组筛选只在这次打开的页面里有效，不存。
- 设置 › hub（`HubsScreen`）：每组一节，列出组的状态和每台 hub（已配对的、经互联发现的、正在使用哪台）；组的菜单里「用于 Jarvis 和收件箱」（选中组）、改名（只在这台手机上）、断开这一组；同组有两台以上已配对的 hub 时可以单独断开一台；「添加 hub」走原来的配对页（跳过欢迎页，完成后回到 hub 列表），配对成功的 hub 按 mesh 并进已有的组或成为新的一组。
- Jarvis 的「在哪运行」只列选中组的机器。机器监控的总览把各组拼在一起（每台机器 id 带后缀），详情按机器的组取。
- 切换选中组时，外壳按新组重建（和以前换配对一样，各 tab 回到首页）：hub 级页面换成新组的，合并列表里的键一个都不变，各组的连接也不重连。
- 一组在页面开着时没了（并进别的组、被断开）：那一组先从列表里拿掉、等各页面不再用它的数据再关（`GroupStacks.awaitDropped`）；还在路上的调用当作「连接已切换」的取消，不当失败报；开着的对话页换到同一会话的新 id（合并后机器 id 去掉了后缀），找不到就返回。
- 对话页、终端、文件、分享到某个会话、会话里上传附件都按会话所在的组走（`ChatWiringSource.bindingsFor`、`ShellChat`、`ShareQueue`）；以前它们直接用唯一配对的服务，多组时会把别组的会话发到选中组的 hub 上（实测踩过：对话页的草稿版本拿选中组的仓库去读别组会话，选中组的 hub 收到了带后缀的请求）。

### 还没做 / 已知限制

- hub 互联（L1，#294）没合进来时，真实 hub 的 hello 没有 `mesh`，每台 hub 自成一组；组内换线路只在单测和下面的假 hub 包装上验证过。
- 设备设置（通知偏好、设备列表）、检查更新、手机数据工具、锁屏测试、分享只对选中的组；设备记录在组里第一台 hub 上，连着别的 hub 时这些页面可能读不到。
- `platform/shortcuts`（port/surfaces 的小组件、磁贴）按配对存的快照在多 hub 时怎么处理：后续。
- 通知按 merged id 发出，点开和作答都按组路由；常驻通知的连接状态只反映选中组。

### 验证

- 合并时搬待发消息和草稿：`HubGroupRegistryTest`（合并记 `PendingMove` 不删数据、搬完才删、搬动跟着再次合并的组走、断开组时一起删；哪些消息和草稿要搬、改作用域、打断的发送变「结果不明」、不覆盖已有草稿），仪器测试 `HubGroupsDataStoreTest`（真实 Room：b 组有一条没发的消息和一段草稿，合并后搬进 a 组的待发队列和草稿，`client_msg_id` 不变；再搬一次不重复；搬完才删 b 的数据、保留 b 的凭据）。
- 单测：`GroupKeysTest`（后缀、主组不变、hello 的 mesh）、`HubGroupConnectionTest`（组内换 hub、各用各的 token、借用凭据、复查切回、成员 401 只排除它、最后一台 401 才失效、成员的地址答成别的 hub 不用、成员的候选只进 `memberEndpoints`、TLS 指纹不能冒充）、`HubGroupRegistryTest`（迁移、learn 的合并 / 拆分 / 名字、id 稳定、主组不提拔、添加的三种去向、断开的擦除清单、选中组的去向）、`GroupRoutingTest`（ID 进出、草稿键、创建会话、合并的置顶只写变了的组且不覆盖读不到的组、fleet 相加）、`AgentGroupsTest`（一组不标、多组标组、筛选）。仪器测试 `HubGroupsDataStoreTest`（迁移 + 同时开两个组作用域）。
- 截图：只有一组时，三套风格 × 深浅色各 36 张逐像素比对（`SHOTS_STYLE=… SHOTS_MOCK_PORT=18771 SHOTS_FROZEN=2026-11-02T04:00:00+00:00 SHOTS_SETTLE=3`），每套只有设置首页深浅色两张不同（新加的「hub」一行，下面的内容整体下移），其余 34 张逐像素相同（连接诊断里实测耗时那一行按上面的办法忽略）。
- 多组（模拟器，下面的假 hub 包装，2026-11-02）：
  - 两组（家里、公司，两个假 hub 数据完全一样）：列表合在一起，组筛选、电脑栏头的组标签、「MacBook（公司）」都对；在「公司」的请求卡上点「允许」，只有公司那台假 hub 收到 `POST /api/v1/interactions/ix_tests_build/resolve`（不带后缀），家里那台什么都没收到；打开公司的会话，只有公司那台收到 `/items` 和 `/seen`（不带后缀）。
  - 把「公司」设为 Jarvis 和收件箱用的组：`/api/v1/assistant`、`/api/v1/inbox`、设备偏好改向公司那台，家里那台的 WebSocket 没有重连，列表里的键不变。
  - 断开「公司」：它的数据目录被删掉，列表回到只有一组的样子（没有组筛选和组标签），家里那台的 WebSocket 没有重连。
  - 两组合并（先各自成组，公司那台停掉时在公司的会话里发一条、再写一段草稿，然后两台的 mesh 互相列出对方）：合并后对话页没有崩，草稿出现在合并后的同一会话里；那条消息在一次运行里由公司那台在合并前发出，在另一次（合并时 App 崩过、重启后补搬）里搬进合并后的组、带原来的 `client_msg_id` 发给了家里那台，都只发了一次。
  - 同组两台（家里 A、家里 B 的 mesh 互相列出对方，只配对了 A）：hub 列表里 B 显示「经互联发现」；停掉 A，连接换到 B（B 收到 health 和 WebSocket，hub 列表里 B「正在使用」，单次观察约 2 秒，未实测）。A 恢复后靠 5 分钟一次的复查换回，没有等。
  - 通知的点开和作答没有在模拟器上点过（逻辑上走的是合并后的 id，见上）。
- 假 hub 的互联字段由 iOS 那一路加进 `scripts/mock/mock_hub.py`；在那之前用一个不改它的包装：照 `android.sh shots` 的做法 exec 它的源码，换掉 `identity()`（health 和 hello 都用它），加上固定的 `hub_id`、`hub_name` 和 `mesh`。
  两组：两个假 hub（端口各一，不带 mesh），一个用 `debug.mockAddress` 配对，另一个用 `--es debug.addMockAddress http://<回环地址>:<端口>`（debug 包才有，等于设置里的「添加 hub」）。同组两台：两个假 hub 的 mesh 互相列出对方，只配对一台；停掉它，连接换到另一台。

## 构建和安装

```sh
./scripts/app/android.sh          # 编译 debug 包、装到连着的手机 / 模拟器并打开
./scripts/app/android.sh apk      # 只出 APK：android/app/build/outputs/apk/debug/app-debug.apk
./scripts/app/android.sh test     # 全部模块的单元测试（含共享夹具入口）
./scripts/app/android.sh lint     # Android lint
```

- 需要 Android SDK（`$ANDROID_HOME`，默认 `~/Library/Android/sdk`），安装 API 37 平台和对应 build-tools；需要 JDK 21，可用 Android Studio 自带的 JBR 21。
- 工具链为 Gradle 9.8.2、AGP 9.5.0、Kotlin 2.4.21、Compose BOM 2026.09.00。版本集中在 `android/gradle/libs.versions.toml`；wrapper 与 daemon 配置也提交进仓库。
- AGP 9 使用内置 Kotlin，不再 apply `org.jetbrains.kotlin.android`。`build-logic/convention` 提供 `jarvis.android.application`、`jarvis.android.library`、`jarvis.jvm.library`、`jarvis.compose`，统一 SDK 和 JVM 配置；JVM 字节码仍为 17。
- 脚本会检查启动 JVM；`JAVA_HOME` 指向 JDK 8 等旧版本时，回退到 Android Studio 的 JBR。`gradle/gradle-daemon-jvm.properties` 另将 daemon 固定在 JDK 21，IDE 也应选同一版本。
- `test` 用 Gradle 的任务选择器执行各模块单测；`lint` 只跑 Android lint，尚未引入 ktlint / detekt。构建缓存和配置缓存已开启。
- `app/lint-baseline.xml` 只记录两处原有的通知权限误报：调用前已有 `allowed()` 检查，调用本身在 `runCatching` 内。保留原通知代码，其余错误仍阻断构建，已有 warning 继续显示。
- target 暂停在 36，本轮不申请 Android 17 的本地网络权限；配对与连接流程补齐权限处理后再升到 37。
- Application ID 走配置：`~/.config/jarvis/config.json` 的 `android.application_id`，没写就用 `app.bundle_id`，再没有就是 `com.example.jarvis`。
- 同一台手机上并排装两个版本（比如主线和新版对比）：直接调 Gradle，给不同的 `-PjarvisApplicationId` 和应用名 `-PjarvisAppLabel`（默认「Jarvis」），例如 `./gradlew -PjarvisApplicationId=<包名>.next -PjarvisAppLabel="Jarvis 新版" :app:assembleDebug`。两个版本都注册 `jarvis://` 深链，扫配对码时系统会问用哪个 App 打开。85 MB 左右的 debug 包经 USB 流式安装时，个别手机会中途断开 adb，重装一次即可。
- 明文 HTTP 仅用于本地或受信任网络；公网使用 HTTPS / WSS 和配对指纹钉扎，优先 IPv6。实际目标由 `HubHttpClient` 校验，见 [network.md](network.md)。
- App 图标和 iOS 是同一张（`apple/Jarvis/Resources/Assets.xcassets/AppIcon.appiconset/icon.png`）：自适应图标的前景是这张图缩进到 108 dp 画布中间的 72 dp 安全区
  （`drawable/ic_launcher_fg.xml` 内缩 18 dp，图在 `drawable-nodpi/ic_launcher_art.png`），背景是图边缘的深棕 `#1B110D`，桌面裁成圆形还是圆角方形都不会切到圆环；
  主题图标（Android 13+）用 iOS 的 tinted 版当遮罩。通知栏小图标 `ic_stat` 是同一个标志（圆环 + 四角星）的白色剪影。iOS 换图标时这两张 PNG 要重新导出。
- 装到真机：手机开 USB 调试连上电脑跑 `./scripts/app/android.sh`，或者把 APK 发到手机上直接装。

## 缓存体积预算

`HubDataStore` 的 `cacheBudgets = CacheBudgets(...)` 可分别配置 items、对话图片和收件箱文件的预算，单位为字节。默认依次为 32 MiB、128 MiB、128 MiB；这些值是初始策略，**未实测**，不代表实际内存占用或设备容量评估结果。

- items 仍保留末两轮，去掉图片内嵌原始字节，按数据库中 JSON 的 UTF-8 字节数计费。超过预算时，按最近读写时间整段淘汰会话，同时删除游标，避免留下虚假的连续性记录。页面订阅期间保护该会话；受保护的数据单独超额时暂缓淘汰，最后一个订阅释放后补清理。SQLite 页、索引和 WAL 不计入 JSON 预算，数据库文件可能保留可复用的空闲页；这不是数据库文件长度的硬上限。
- 对话图片按 Coil 的显示尺寸等比降采样，经方向校正后编码成 PNG，再原子写入配对作用域。缓存键包含会话、图片 ID 和目标尺寸，避免复用不合适的缩略图；没有明确尺寸时用当前屏幕尺寸。图片按文件实际字节数计费，读取更新 LRU，整段可见会话的图片都受保护。单图或受保护文件导致无法容纳新图时，拒绝写入并由现有图片失败提示反馈。
- 收件箱文件按实际落盘字节数计费，命中直接复用，超额淘汰最久未用的文件。系统查看器通过 FileProvider 打开的文件描述符持有保护引用，关闭后解除；单个文件超过预算，或已打开文件占满预算时，下载返回错误。附件保持原始内容，不把文件下载改成缩略图。

预算不涉及 outbox、草稿和持久偏好。飞书行内图片沿用已有编码字节内存缓存，不新增原图磁盘副本。图片解码峰值、SQLite 物理占用、冷启动及滚动性能均未实测，仍需真机验收。

## 设计 token 采用率基线

在仓库根目录运行独立检查，无需 Android SDK：

```sh
python3 scripts/app/android_token_baseline.py
python3 scripts/app/android_token_baseline.py --report /tmp/android-token-report.json
python3 scripts/app/test_android_token_baseline.py
```

基线保存在 `android/core/designsystem/tools/token-baseline.json`。脚本遍历 `android/feature/*/src/` 的生产 Kotlin 源码，跳过测试目录，忽略注释、字符串和 import 声明，并识别显式导入及别名。按模块统计颜色、字号和 dp 三类的硬编码次数、token 次数与采用率；采用率为 `token / (token + 硬编码)`，没有用量时为 `null`。

颜色统计数值 `Color(...)`、命名常量和 `parseColor(...)`，字号统计数值 `.sp`，dp 统计所有数值 `.dp`，包含间距、尺寸和圆角。token 包括相应的 `Tokens`、`JarvisTheme.colors`、`JarvisTypography` 和主题映射下的 `MaterialTheme` 引用。这是源码引用计数，不是运行时样式覆盖率；变量传递、通配成员导入、字符串模板内的表达式与自定义包装函数不做完整语义分析。

2026-10-31 界面重做后，`JarvisTheme.palette` / `type` / `shapes` 也算 token；feature 里的颜色和字号已经全部走 token，页面之间的间距仍是 dp 数值（各风格的内边距在设计系统组件里），基线按重做后的代码更新过一次。

每个文件、每类硬编码数量都不能超过基线，新文件从零开始。终端等允许固定尺寸的现有代码也计入基线，新增用量同样受检查。减少另一个文件的硬编码不会抵消本文件的新增。确需接受变更时，运行 `python3 scripts/app/android_token_baseline.py --update-baseline`，在评审中检查基线差异；普通检查不会重写基线。多车道合并后先运行检查，优先修正新增硬编码，再决定是否更新基线。Swift 出口不在本次范围内。

## 模块与代码

模块以 `android/settings.gradle.kts` 为准，仓库内直接依赖以各模块的 `build.gradle.kts` 为准。下表箭头从使用方指向被依赖方，只列生产代码的 `project(...)` 依赖；外部库与测试依赖不列入。

| 模块 | 职责 | 直接依赖 |
| --- | --- | --- |
| `:app` | 应用入口、四 tab、Hilt 装配、配对生命周期、作用域桥接、系统分享和锁屏测试 | `:core:contracts`、`:core:domain`、`:core:terminal`、`:core:navigation`、`:core:hub`、`:core:data`、`:core:chatlist`、`:core:designsystem`、`:core:demo`、`:feature:chat`、`:feature:agents`、`:feature:assistant`、`:feature:inbox`、`:feature:settings`、`:feature:terminal`、`:feature:machines`、`:feature:files`、`:feature:knowledge`、`:platform:notify`、`:platform:phonetools` |
| `:core:chatlist` | 倒置聊天列表、冻结、回最新、阅读锚点及分页效果；当前主要由 Agent / 助手对话消费 | 无仓库内模块依赖 |
| `:core:contracts` | 纯 JVM：线上模型、事件解码、HubScope、仓储和传输接口 | 无仓库内模块依赖 |
| `:core:data` | Room items / outbox / 草稿、DataStore / Tink 凭据、作用域、缓存及真实仓储 | `:core:contracts`、`:core:domain`、`:feature:inbox` |
| `:core:demo` | 本地演示数据和 DemoHub，使用 assets 中的示例夹具 | `:core:contracts`、`:core:hub` |
| `:core:designsystem` | 三套界面风格的 token（色板、字体、形状、动效）、组件皮肤（页面脚手架、列表、按钮、要你定的事、对话、收件箱）、外壳导航组件、玻璃（Haze）；打包字体（OFL）和 Phosphor 图标 | `:core:domain`、`:core:hub` |
| `:core:domain` | 纯 JVM：连续性 reducer、chatLines、排序、收件箱分段、作答策略和配对解析 | `:core:contracts` |
| `:core:hub` | 纯 JVM：OkHttp HTTP / WS、TLS 钉扎、选路、心跳、配对及各业务传输实现 | `:core:contracts` |
| `:core:navigation` | Navigation 3 路由类型、Navigator、通知和机器页面路由 | 无仓库内模块依赖 |
| `:core:terminal` | 纯 JVM：VT 网格、Unicode 宽度、折行和终端输入编码 | 无仓库内模块依赖 |
| `:feature:agents` | 电脑 → 空间 → 会话的列表、请求卡、新建与关闭确认、空间页、会话和空间的重命名（本地立即生效） | `:core:contracts`、`:core:domain`、`:core:designsystem`、`:core:navigation` |
| `:feature:assistant` | 首页、历史抽屉、主对话发送、资料与定时任务页；管理服务和专用聊天样式仍待装配 | `:core:contracts`、`:core:data`、`:core:domain`、`:core:designsystem`、`:core:navigation` |
| `:feature:chat` | 共用对话、Markdown、工具 / diff / 待办、交互面板、附件组件和应用内链接阅读 | `:core:contracts`、`:core:domain`、`:core:designsystem`、`:core:chatlist`、`:core:navigation` |
| `:feature:files` | 文件树、文本 / 图片预览和路径插入；从 Agent 对话 ⋯ 菜单进入 | `:core:contracts`、`:core:designsystem`、`:core:navigation` |
| `:feature:inbox` | 统一收件箱、飞书会话、邮件详情、AI 操作和回复组件；邮件传输仍待装配 | `:core:contracts`、`:core:domain`、`:core:designsystem`、`:core:navigation` |
| `:feature:knowledge` | 知识库列表、阅读和导航；从 Jarvis 抽屉进入 | `:core:contracts`、`:core:designsystem`、`:core:navigation` |
| `:feature:machines` | 机器 Dashboard、详情和指标图；从 Agents 机器菜单进入 | `:core:contracts`、`:core:designsystem`、`:core:navigation` |
| `:feature:settings` | 配对、准备清单、诊断、设备与通知偏好、关于和 APK 安装；设备设置服务仍待装配 | `:core:contracts`、`:core:domain`、`:core:designsystem`、`:core:navigation` |
| `:feature:terminal` | 终端 Canvas、流式帧消费、接管和输入；从对话页和通知的终端兜底进入 | `:core:contracts`、`:core:terminal`、`:core:designsystem`、`:core:navigation` |
| `:platform:notify` | 前台服务、通知策略和按钮、操作日志、开机接收与连接保活 | `:core:contracts`、`:core:domain`、`:core:designsystem`、`:core:navigation` |
| `:platform:phonetools` | 手机工具目录、注册、权限与写入确认，通讯录 / 日历 / 照片 / 位置等实现；设置 › 手机数据进入 | `:core:contracts`、`:core:navigation`、`:core:designsystem` |

依赖的主要方向为 `:app → feature / platform / core`，功能层通过 `:core:contracts` 使用接口，由 App 的 Hilt 模块装配真实实现。当前仍有两处跨层依赖：`:core:data → :feature:inbox`、`:feature:assistant → :core:data`，这是现有构建图，尚未完全符合设计中的单向分层。`:core:domain → :core:contracts`，`:core:hub → :core:contracts`；`:core:terminal` 不依赖其他项目模块。

`build-logic/convention` 通过 `includeBuild("build-logic")` 引入，不是应用模块。它提供 application、library、JVM、Compose、Hilt 约定插件；Hilt 已接入，Room / KSP、DataStore 和 Tink 在 data 模块。版本见 `gradle/libs.versions.toml`。

`app/di/ConnectionRuntime.kt` 持有连接与配对作用域；`ChatDataModule`、`InboxModule` 装配真实仓储。HTTP 与 WS 共用当前候选和凭据，重配时整体切换 `HubScope(hub_id, pairingGeneration)`，关闭旧协程并删除旧作用域数据。普通与助手输入框尚未接上 `DraftStore`，不能把存储类存在写成草稿已持久化。

### 构建、测试和截图

在仓库根目录运行：

```sh
./scripts/app/android.sh install  # 与不传参数相同：构建、安装并打开
./scripts/app/android.sh apk      # 只构建 debug APK
./scripts/app/android.sh test     # 各模块单元测试
./scripts/app/android.sh lint     # Android lint
```

脚本支持 `install|apk|test|lint|shots`（`shots` 见下文「假 hub 批量截图」），不透传任意 Gradle 任务。只验证有关模块时直接调用 wrapper，例如：

```sh
cd android
./gradlew :core:domain:test :core:hub:test
./gradlew :feature:chat:testDebugUnitTest
```

`core:designsystem` 有按像素检查组件的单测（`EnabledStateTest`），用 Robolectric 原生渲染（`@GraphicsMode(NATIVE)`），第一次跑会从 Maven Central 下载 Robolectric 的 Android 运行库（SDK 35）。共享规格在 `spec/fixtures/`，相关测试按连续性、行、作答、排序、收件箱和终端读取。约定插件传入仓库根目录，也可用 `-PjarvisRepoRoot=<仓库根>` 指定。本页未运行这些命令，不据此声明测试通过。

截图使用专用模拟器、假 hub 和 adb；先在单独终端运行 `python3 scripts/mock/mock_hub.py`，再执行以下命令。包名与 `scripts/app/android.sh` 的配置一致；替换占位符，不连接线上 hub。

```sh
./scripts/app/android.sh apk
export ANDROID_SERIAL='<专用模拟器序列号>'
JARVIS_PACKAGE='<配置中的 application_id>'
ADB="${ANDROID_HOME}/platform-tools/adb"
"$ADB" install -r android/app/build/outputs/apk/debug/app-debug.apk
"$ADB" reverse tcp:18760 tcp:18760
"$ADB" shell "am start -a android.intent.action.VIEW -d 'jarvis://pair?url=http%3A%2F%2Flocalhost%3A18760&token=mock' $JARVIS_PACKAGE"
```

在配对页确认连接、处理权限和准备清单后，切换到要检查的页面。深浅色各截一张；直接跳转会话时，使用假 hub 的机器和会话 ID：

```sh
"$ADB" shell cmd uimode night no
"$ADB" shell am force-stop "$JARVIS_PACKAGE"
"$ADB" shell "am start -a android.intent.action.VIEW -d 'jarvis://open?machine=<示例机器ID>&session=<示例会话ID>' $JARVIS_PACKAGE"
"$ADB" exec-out screencap -p > /tmp/jarvis-android-light.png
"$ADB" shell cmd uimode night yes
"$ADB" shell am force-stop "$JARVIS_PACKAGE"
"$ADB" shell "am start -a android.intent.action.VIEW -d 'jarvis://open?machine=<示例机器ID>&session=<示例会话ID>' $JARVIS_PACKAGE"
"$ADB" exec-out screencap -p > /tmp/jarvis-android-dark.png
```

页面就绪后再截图。批量截图用 `./scripts/app/android.sh shots`，debug 直达参数见下文；它们都不连线上 hub。滚动性能见「界面风格的性能」。

## 界面风格的性能

2026-10-31 在测试手机（Android，ColorOS，连真实 hub）上用 `scripts/diag/android_scroll_perf.py` 实测：debug 包，经调试入口打开页面
（`debug.style` 切风格，`debug.automation` 关掉掉帧降级，量的是玻璃本身的开销），等 3.5 秒后清零 `dumpsys gfxinfo`，上下各滑两次，
读 framestats。帧时间 = `FrameCompleted − IntendedVsync`，每次取 framestats 里最后约 120 帧（未完成和异常的行剔除），每个场景 10 次。
卡顿帧占比是 gfxinfo 自己的「Janky frames ÷ Total frames」，10 次合计，括号里是单次的 min / p50 / max。
对话场景是手机上最长的一个 Agent 会话（transcript 最大）；第一轮用的 Jarvis 主对话每次只渲染约 20 帧（没滚动起来），不作数。

模糊输入是 Haze 默认分辨率（约 0.78）时：

| 场景 | 风格 | 样本帧 | min | p50 | p90 | max | mean（ms） | 卡顿帧占比 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Agents 列表 | 晨报 | 874 | 2.6 | 8.5 | 16.7 | 33.6 | 9.45 | 4.46%（3.4 / 4.5 / 6.1） |
| Agents 列表 | 看板 | 993 | 2.8 | 7.1 | 10.3 | 32.2 | 8.03 | 1.02%（0.2 / 2.3 / 4.6） |
| Agents 列表 | 玻璃 | 988 | 5.2 | 10.9 | 15.5 | 51.1 | 11.31 | 1.75%（0.3 / 2.3 / 4.4） |
| 收件箱 | 晨报 | 763 | 2.6 | 8.6 | 17.7 | 54.0 | 10.46 | 7.08%（4.1 / 6.8 / 9.2） |
| 收件箱 | 看板 | 992 | 2.6 | 7.0 | 10.9 | 43.5 | 8.34 | 1.38%（0.0 / 3.1 / 5.3） |
| 收件箱 | 玻璃 | 772 | 4.6 | 11.2 | 18.8 | 54.2 | 12.99 | 6.59%（5.1 / 6.4 / 7.9） |
| 对话 | 晨报 | 1183 | 2.8 | 8.2 | 12.5 | 83.3 | 9.27 | 5.53%（3.6 / 5.5 / 8.4） |
| 对话 | 看板 | 1182 | 2.4 | 8.1 | 13.7 | 90.5 | 9.39 | 5.32%（3.9 / 5.1 / 9.2） |
| 对话 | 玻璃 | 1186 | 7.3 | 16.3 | 22.3 | 60.2 | 17.46 | 4.39%（2.0 / 4.9 / 8.8） |

结论：列表页玻璃的帧时间中位数比另外两套多 2–4 ms，卡顿帧占比和晨报相当；对话页浮层最多（顶栏、任务条、状态行、输入框和两条滚动边缘），
玻璃的帧时间中位数是另外两套的两倍。于是把模糊的输入降到四分之一分辨率（`LocalHazePerformanceMode` = `Performance`，`JarvisTheme`），
截图对比看不出差别。降分辨率之后的手机数据：未实测（手机中途断开 adb）。

模拟器只作参考（GPU 是模拟的，RenderEffect 很慢）：玻璃的帧时间中位数 Agents 166 ms、对话 261 ms，晨报和看板约 17 ms；
降到四分之一分辨率后玻璃是 103 / 126 ms。这种机器上平时会由下面的降级接手。

降级：`GlassFrameMonitor` 在滑动时连续掉帧（240 帧里超过 30% 超过 1.5 倍帧预算）就本次运行降一级（折射 → 模糊 → 不透明）；
省电模式和「减少透明」直接用不透明面板（模拟器上拔电开省电模式验证过：悬浮坞和顶栏变成不透明，滚动边缘换成渐变）。耗电：未实测。

## 验证记录

以下是旧版 App 的历史记录，本次文档核对未复测。

模拟器（Android 14）连真实 hub，一台 Linux 跑 hub、一台 Mac 经 SSH 接入：

- 扫码链接配对、换设备 token；两台机器在线，列表显示真实会话
- 手机发消息到 Mac 上 herdr 里的 claude；claude 的 AskUserQuestion 变成卡片（hook 拦下的走 `question`，TUI 里的走 `screen`），在手机上选择后 claude 收到
- 计划模式下 ExitPlanMode 的对话框在手机上批准，claude 继续执行改了文件
- App 在后台：提问通知上直接点选项，claude 收到；一轮做完弹「做完了」
- hub 重启后几秒内自动重连；长会话往前翻到两天前的内容；深色模式截图检查

真机（Redmi K60 Pro，Android 16，澎湃 OS 3，没有 Google 服务），手机经 USB（`adb reverse tcp:18660 tcp:18660`）连 hub：

- 配对、列表、对话页正常；claude 的提问在手机上回答，电脑上的 claude 收到
- 「需要你处理」的通知系统里有（channel `input`，重要级别最高），但默认不在通知栏显示：澎湃 OS 给新装的 App 关掉了悬浮 / 锁屏通知，
  要在「设置 › 通知与控制中心 › 通知管理 › Jarvis」里打开。锁屏后长时间的后台连接还没在真机上测

## 形象（Jarvis 的脸）

照 iOS（`docs/persona.md`）复刻内置的默认形象「面罩」（#638）：`core/designsystem` 的 `PersonaFace` 用 Canvas 画，几何、配色比例和动作取自 iOS 的 `PersonaFace.swift`，按当前主题色着色，任意尺寸都清楚，不用位图。

- 用在：Jarvis 页顶栏左上角（`JPersonaButton`，替换原来的「对话列表」图标，点它照旧打开对话列表；读屏读「对话列表」和「Jarvis，<状态>」），对话列表顶部（原来的字母头像，宽屏常驻的侧栏也是它）。两处显示同一个状态。
- 五个状态：空闲（每 3.6 秒眨眼）、思考中（眼睛看右上，思考气泡三个点轮流亮）、工作中（眼睛左右扫）、等你处理（呼吸的环）、离线（灰，眼睛闭成线）。只有后三个每帧动（约 12 帧/秒），空闲只在眨眼时动；「减少动画」或省电模式时每个状态停在静止的一帧（同 iOS 的低电量模式）。
- 状态的取法（`feature/assistant` 的 `personaState`，规则同 iOS `AppModel.personaState`）：hub 连不上（连接中、等待网络等，「更新中」不算）或这段对话的机器离线是离线；对话等你（`needs_you` 或有待处理的交互）是等你处理；正在跑一轮是工作中；其他空闲。看的是当前打开的对话；新对话页没有对话，只看 Jarvis 所在的机器在不在线，其他时候空闲。
- 和 iOS 的差别：这一页拿不到对话的最后一条，分不出思考中和工作中，跑着的一轮都显示工作中；等你处理的环用 Android 设计系统的「需要你」色（橙），不是 iOS 的红；晨报风格的主题色就是墨色（近黑 / 近白），面罩和星标会糊掉，所以晨报里脸用次级墨色（中灰），不给这个风格加颜色；iOS 在收件箱有事时给脸挂托盘标记，Android 的收件箱是底部的独立 tab、自带提醒，脸上不再挂。
- 还没做：内置的「汇流」、hub 上的形象包（`~/.jarvis/personas/`，APNG 帧）、设置 › 外观 › 形象的选择页。
- 截图核对：组件目录（调试包 `--es debug.openPage catalog`）的「形象」一节并排画五个状态。

## 助手设置（设置 › 助手）

用户要求（2026-11-02）：「jarvis 的会话和记忆等等内容，是运行在哪个设备上的，现在也完全没有设置选项」。代码在 `feature/assistant/…/settings/`：页面 `AssistantSettingsScreen`，状态和请求 `AssistantSettingsState`，接口用 `AssistantManagement` 的 `config` / `updateConfig` / `launchers`（`GET` / `PUT /api/v1/assistant`、`GET /api/v1/machines/{id}/launchers`）和原有的记忆接口。

- 入口：设置列表「助手 › Jarvis」（`SettingsPage.Assistant`，深链 `jarvis://open?target=settings&page=assistant`；宽窗口在设置列表右栏打开，页内的子页也在右栏）。Jarvis tab 标题菜单的「助手设置」、右上角的「助手资料」按钮、抽屉里的「资料页」「启动器」在 Jarvis tab 里原地打开同一页。体验模式没有这页，显示说明。
- 在哪运行：机器（在线状态；`kind: local` 的写「hub 所在的机器」）、启动器和模型、工作目录（按那台机器的 home 缩成 `~/.jarvis/assistant`）。启动器列表取那台机器的 launchers，只列 Claude 类型：助手是无头的 `claude -p`，Codex / TRAEX 的启动器起不了它。保存的启动器不在那台机器的列表里时提示换一个，否则新对话起不来。
- 换机器：先确认，确认框写明「记忆和会话记录在原来那台机器上，不会跟过去」，目标离线时另外写明；确认后 `PUT {"machine_id":…,"main":"-"}`。主对话是原来那台机器上的 transcript，换过去接不上，所以一起清掉，下一条消息在新机器上开新的主对话；原来的会话和记忆留在那台机器上，换回来还在（原来的主对话成了普通会话）。换完刷新 Jarvis 的会话列表，重新读启动器和记忆。
- 数据存在哪里：会话记录和记忆在助手所在的机器；关于我、定时任务和设置在 hub；知识库在 hub 所在的机器（`~/.jarvis/data`）。
- 了解你：「关于我」编辑 `notes`。还没在 App 里保存过时打开的是 hub.json 的内容，并说明保存后以这里为准；清空也照样保存（告诉助手什么都不写）。记忆列出 `memory/*.md`，新的在前，标题先取 `MEMORY.md` 索引行，其次 front matter 的 name，类型标成用户 / 反馈 / 项目 / 参考；点开看正文，可以编辑整个文件（含 front matter）或删除（hub 同时删索引行）。`MEMORY.md` 只能看。
- 时区：默认「跟随手机」，和 iOS 一样：打开 Jarvis tab 或这一页时，hub 的 `timezone` 和手机不同就 `PUT timezone`。关掉以后从列表里挑（当前的和手机的排在最前，可搜城市名、中文名、UTC 偏移），之后不再自动同步。这个选择存在本机（SharedPreferences `jarvis-assistant`），不跟 hub 走。
- 主动做事：「在哪运行」下面一节，一行「定时任务、跟进和运行记录」打开 `AssistantProactiveScreen`（在这一页里原地打开，返回回到这一页），见下文「主动做事」。Jarvis 抽屉的「定时任务」在 Jarvis tab 里原地打开同一页；旧的资料页 `AssistantProfile` 已删掉。
- 截图：`debug.assistantSettings` 直达页内一部分（notes、memory、memory-file、memory-edit、memory-delete、machine、launcher、timezone、move 即换机器确认框，proactive 即主动做事），和 `debug.openLink 'jarvis://open?target=settings&page=assistant'` 一起传。假 hub 支持 `PUT /api/v1/assistant` 和 `PUT` / `DELETE /api/v1/assistant/memory/{name}`；`MOCK_MACHINES=1` 多一台离线机器，可以看离线的样子。
- 单测：`AssistantSettingsStateTest`（读写配置、换机器的提示和清主对话、启动器缺失、跟随手机的时区、关于我、记忆的读取 / 修改 / 删除）、`AssistantConfigTest`（`PUT` 只带改了的字段，空的关于我也发）。

## 经验

- 传输层不能在主线程读响应体：配对页在主线程的协程里发起请求，读 body 时 StrictMode 直接抛 `NetworkOnMainThreadException`，
  界面上只看到笼统的「连接失败」「连接没有完成」（logcat 的 `JarvisPairing` 有原因）。JVM 单测测不出来；有没有触发还要看 body
  是否和响应头一起到达：经 `adb reverse` 的 127.0.0.1 常常分两段到，所以失败，模拟器经宿主机别名地址（Android 模拟器文档里的固定地址）连过去的小响应侥幸能过。
  现在 `awaitResponse()` 默认在 OkHttp 的线程里把 body 读完，只有 APK 下载传 `buffered = false`，并在 `Dispatchers.IO` 里流式读。
- 模拟器截图检查（深浅色都要，`docs/testing.md`「界面检查清单」）：
  - 用专用的 AVD，固定端口（`emulator -avd <名字> -port 5600`），所有 adb 命令都带 `-s emulator-5600`，别动别的模拟器。
  - 数据用 `scripts/mock/mock_hub.py`，配对用深链。URL 里的 `&` 会被设备上的 shell 截断，要整条命令再套一层引号：
    `adb shell "am start -a android.intent.action.VIEW -d 'jarvis://pair?url=...&token=mock' <包名>"`，然后在确认页点「连接」。
  - 每张图前先 `am force-stop` 再冷启动，否则各 tab 自己的返回栈会留着上一步推进去的页面；对话页用 `jarvis://open?machine=&session=` 直达。
  - 按文字点控件用 `uiautomator dump` 找节点坐标。同名的文字（「设置」既是底部 tab，也是 Agents 顶栏齿轮的说明）要按坐标点。
- `adb shell input swipe` 从屏幕最上方往下滑会拉出系统的快捷设置，测试翻历史时从屏幕中间开始滑。
- 新建的 AVD 第一次开机偶尔没有网络（`ip addr` 里 eth0 是 DOWN，`dumpsys connectivity` 里没有任何 NetworkAgent），App 配对后一直显示「显示的是本机缓存」：关掉模拟器再开一次就好。
- 一次按键（TAB、BACK、`input text`）会让屏幕退出触摸模式，之后冷启动的 App 会把键盘焦点放在第一个能拿焦点的控件上（手机上是 Agents 顶栏的「新建会话」，截图里多一个焦点框）。截图脚本在冷启动前点一下桌面的状态栏，回到触摸模式。
- `HubService` 是用 `startForegroundService` 拉起的。它在调 `startForeground` 之前被停掉（自己 `stopSelf` 或外面 `stopService`），系统就让 App 崩在 `ForegroundServiceDidNotStartInTimeException`（logcat 先有「Bringing down service while still waiting for start foreground」）。截图机负载很高时配对后常崩就是这个（2026-11-02 查实）。以前有三条路会这样：没配对或用户停过时 `onStartCommand` 一上来就 `stopSelf`；我们发出的启动还没进前台，`NotificationRuntime` 因为 `isPaired` 一真一假就 `stopService`；系统发起的启动（新进程里重启服务、`am start-foreground-service`）还没进前台，`Application.onCreate` 里先跑了停止。现在服务一律先进前台再决定去留，用 `stopSelf(startId)` 不误停更新的启动；`HubService.stop()` 只在服务已经在前台时 `stopService`，我们发的启动还没进前台就把停止记下交给它（`ForegroundStarts`），其余交给服务自己判断。复现：模拟器 `adb root` 后在没配对的 App 上跑 `am start-foreground-service -n <包名>/jarvis.platform.notify.HubService`，修复前必崩，修复后服务进前台再自己停。
- herdr server 继承启动它的 shell 的环境：在带代理变量的 shell 里起 herdr，里面的 claude 也会走那个代理。联调时用干净的登录 shell 起 herdr。
- Claude Code 设置里放开了的工具（allow 规则、bypass 模式）不会触发 PermissionRequest，测授权卡片要在没放开的会话里。
- `JsonCodec` 的 `encodeDefaults = false` 会把等于默认值的字段省掉不发，而 hub 把缺的字段当成空 / false：定时任务的 `enabled = true`、
  `time = "09:00"` 被省掉，保存时被关掉或者 400。发给 hub、hub 又按「缺了就是空」处理的字段，要加 `@EncodeDefault`（`SetRoutinesTest` 逐字检查请求）。
- OkHttp 不会在一条死掉的连接上重试 WebSocket 升级：hub 前面的转发（adb reverse、ssh -L）断过一次后，每次重连都报 `unexpected end of stream`，直到重启 App。
  WebSocket 失败时要清空连接池（`connectionPool.evictAll()`）。
- 倒置的聊天列表（`reverseLayout`）在前面插入新行时保持第一个可见项不动：回复变长、新回复进来时人在最新处也会被留在原地。人在最新处（或刚发了消息）时要主动滚回 0。
- 列表里拖动排序（连接诊断的候选地址，`EndpointDrag.kt`）：行被拿起时不能改变 `JGroupItem` 的 `separate`，看板风格的 separate 走另一个 `Column` 分支，切换会重建整行，手指下的拖动手势随之断掉（只改分组位置）。要长按拖动的行，`JRow` 的 `onLongClick` 传 null，长按检测放在插槽里：插槽是行的点击的子节点，按下先到这里；手柄自己消费按下，所以手柄和长按不会同时生效。同一条拖动别同时挂两个手势（会走双倍距离）。
- 会变的透明度不要用 `Modifier.alpha`（2026-11-02 查明 `JButton` 先禁用、后来变成可用时只剩一行字的根因）：`alpha(1f)` 不加图层，别的值加一层 graphicsLayer，于是按钮从禁用（0.4）变可用（1）时，修饰符链中间的这一层被删掉。它前面是 `clip` 和 `background`（按钮的底色、输入框的底和边框、决定选项的底），删层之后 Compose UI 1.12.1 不再画这段背景，直到背景自己再变一次才补上；文字在子节点里，照画。在模拟器和 Robolectric 上逐项试过：去掉 `clip`、或者让这层一直在（`graphicsLayer { alpha = … }`，或者 alpha 用 0.999）都不再丢；去掉 `clickable` 照样丢；按住时 `scale` 那层在链头增删不受影响。设计系统里禁用变淡一律用 `Modifier.stableAlpha`（层一直在，只改 alpha）：`JButton`、`JIconButton`、`JTextField`、`JSwitch`、`JCheckbox`、`JRadio`、`JRow`、`DecisionOption` 和 `JActionSheet` 的行；页面上不用再 `key(enabled) { JButton(…) }`。`core:designsystem` 的 `EnabledStateTest` 用 Robolectric 原生渲染按像素查底色和边框，在旧代码上会失败。Agents 的会话行（`AgentRow`）屏蔽时的变淡也换成了 `stableAlpha`（看板样式在 `clip` + `background` 后面原来是 `alpha(if (muted) 0.75f else 1f)`）。`AgentRowTest` 守着取消屏蔽后底、色块和细线都在、和屏蔽前逐像素一样。不过旧写法在 Agents 行上没有复现丢底：2026-11-02 在模拟器上从列表菜单屏蔽再取消屏蔽（看板、玻璃，深浅色），取消屏蔽后的行和屏蔽前逐像素一样；Robolectric 上包括只有变淡在变、状态色不变的情况也一样。所以这个测试在旧写法上也能过，只是守住取消屏蔽之后的样子。
- 双栏时左栏的高亮以前从来没亮过（点左栏、深链、调试入口都一样）：外壳用 `paneMetadata(key) == ListDetailSceneStrategy.detailPane()` 判断「这一页是不是详情」，而 `detailPane()` 每次新建一个没有 `equals` 的 `DetailMetadata`，两边永远不相等，`LocalDetailKey` 一直是 null。现在按路由判断（`app/…/navigation/ShellPanes.kt` 的 `isDetailDestination` / `detailOf`；`PairingRoute`、`PhoneToolsRoute` 换成对应的设置页），`ShellPanesTest` 覆盖深链、点击、返回和别的 tab。别拿 Navigation 3 / adaptive 的 metadata 做相等比较。晨报的行没有左右边距，选中的底板和左边那道墨线以前会压在图标上：晨报里底板往两边各伸出 12dp（`selectedRow`）。
- 新开会话时 node 靠输入框底栏的字样判断 claude 起来了（`server/internal/node/control.go` 的 `inputBoxVisible`）。有的 Claude Code 版本在手动确认模式下底栏是
  `⏸ manual mode on · ← for agents`，node 认不出，等满 1 分钟报「did not become ready」，其实会话已经能用：App 按「启动前没有、启动后在这个目录里的窗格」找到它并打开。
- 小米 / 红米：`adb install` 要先在开发者选项里打开「USB 安装」（要登录小米账号），手机上还会弹「通过 USB 安装」的确认框，几秒内不点就自动拒绝
  （`INSTALL_FAILED_USER_RESTRICTED`）。`adb shell input` 代点屏幕要再打开「USB 调试（安全设置）」，否则报 `INJECT_EVENTS`。
- 手机没法直接访问 hub 时（hub 在内网、手机在流量上），联调可以走 USB：电脑上把本机 18660 转到 hub，再 `adb reverse tcp:18660 tcp:18660`，
  手机配对时用 `http://127.0.0.1:18660`。
- 两栏布局（material3-adaptive 的 `ThreePaneScaffold`）默认在当前窗格变化时把焦点移进那个窗格（`PaneScaffoldDirective.shouldAutoFocusCurrentDestination`），焦点落到窗格里第一个能拿焦点的东西上：Agents 和收件箱是搜索框，带硬件键盘的平板 / 折叠屏一打开就弹出输入法条，第一次按返回只收起输入法（触摸模式下也会，手机单栏没有这个问题）。`Root.kt` 用完整的构造函数把它关掉；`PaneScaffoldDirective.copy()` 没有这个参数，复制出来的又是默认的 true。查焦点从哪来：在文本框上挂 `onFocusChanged` 打 `Throwable()` 的栈，拿焦点是同步发生的，栈里就是发起者。

### 主线旧 App 的经验

2026-11-02 合并主线时并入：这些是主线那个单模块旧 App 踩过的坑，提到的类名、文件名是旧 App 的（读原文用 `git show origin/master:docs/android.md`，代码在合并前的主线历史里）。新架构写同类功能时对照。

- 折叠屏开合会改 `screenSize` / `smallestScreenSize`，没声明 `configChanges` 时 Activity 整个重建：Compose 的 `rememberSaveable` 能恢复，
  但放在 Activity 字段里的状态（`MainActivity.openSession`，通知打开的对话）不会，开合一次对话就关了；重建时还会把启动 intent 再处理一遍，
  重新打开用户已经关掉的通知会话。现在声明了 `configChanges`，打开的对话也写进 `onSaveInstanceState`，启动 intent 只在第一次 `onCreate` 处理。
- 自己画的面板（双栏的右栏、占位、Jarvis 的左栏）底色要用 `Surface(color = …)`，不能用 `Modifier.background`：只有 Surface 会设 `LocalContentColor`，
  没写颜色的 `Text` 默认是黑的，深色模式下标题、正文黑字压在黑底上看不见（原来的底部浮层、Scaffold 自带 Surface，所以单栏没这个问题）。
- 同一个页面在两种布局里处在 Compose 树的不同位置，位置决定的 `rememberSaveable` 会丢（草稿、Jarvis 正在看的对话）：按页面用 `SaveableStateHolder.SaveableStateProvider(key)` 包住，
  不再画的页面 `removeState`，否则关掉再开的对话会回到上次的滚动位置，而不是最新处。
- 全屏看图别放进 Compose 的 `Dialog`：对话框窗口停在状态栏和导航栏之间（`decorFitsSystemWindows = false`、`FLAG_LAYOUT_NO_LIMITS` 都试过，上下照样露出底下的页面），
  直接画在对话页上面（`ImageViewerOverlay`），状态栏图标打开时改成浅色、关掉时还原。
- `Markdown` 默认包在 `SelectionContainer` 里，长按会变成系统的选中文字，消息菜单弹不出来：对话里的回复传 `selectable = false`，要选文字用菜单的「选择」。
- 倒置列表（`reverseLayout`）的 `verticalArrangement` 写成 `spacedBy(x)` 会顶着上边排，内容少时消息浮在顶上：要 `spacedBy(x, Alignment.Bottom)`。
- 对话页验证用 `scripts/mock/mock_hub.py`：`MOCK_CHAT_IMAGES=1` 给「博客迁移」那段对话加两张截图（查看器、编辑器），文件和 Git 是编出来的 `~/code/weather`。
- 小米 / 澎湃 OS 上 `adb shell input` 要开「开发者选项 › USB 调试（安全设置）」，开完要重连一次 adb（`adb reconnect`）才生效；
  之后偶尔连着几次 `SecurityException: Injecting input events requires … INJECT_EVENTS`，隔几秒再发就好，测试脚本里每次点击被拒就等 3 秒重试。
- 终端里打字：安卓输入法对每个英文词也走 composition（为了联想），等 commit 再发的话一个词要到空格才出现；所以输入框里的文字（含正在拼的）
  每次变化只发和上次的差（新字，或若干个退格）。按这个规则，拼音选字时终端里先出现拼音，选好后退格换成汉字（模拟器没有中文输入法，还没在真机上试）。安卓的 emoji 字体会把 ⏺ ⏸ ⏵ 画成两格宽的彩色图标，
  加 U+FE0E 也没用，这几个按形状自己画。
- hub 前面的转发断过以后，hub 那边可能还以为 node 在线，但每个调用都回 `rpc: connection closed`（/screen 也一样）。重启常驻 node
  （`launchctl kickstart -k gui/$UID/com.jarvis.node`）再让 hub 重连这台机器就好了，App 这边不用动。
- `adb shell am start` 启动一个和当前任务根 intent 一样的 intent 时只是把任务拉到前台（「Activity not started」），onNewIntent 不来；
  验证快捷方式要从启动器长按图标点，或者让 App 先停在别的页面。
- WebView 用 application context 建出来时页面永远是浅色：application context 没有 App 的主题（`isLightTheme` 读成 true），WebView 只在外面的主题是深色时才给页面
  `prefers-color-scheme: dark` / 算法变暗。池里的 WebView 包一层 `ContextThemeWrapper(app, Theme.Jarvis)`，`values-night` 里的 `Theme.Jarvis` 是深色并写明 `isLightTheme=false`。
- 往 WebView 的选中菜单加项：Chromium 在 `onPrepareActionMode` 里（每次选区变化）填菜单，它的项都要求显示成按钮，浮动工具栏先排这种；
  自己的项也要 `SHOW_AS_ACTION_ALWAYS`、order 比它小，才排在 Copy 前面。点了以后先 `evaluateJavascript` 读到选区再 `mode.finish()`，先 finish 的话选区已经清掉，读到空字符串。
- `scripts/mock/mock_hub.py` 带 `MOCK_RICH=1 MOCK_LINKS=1` 时给对话、Jarvis 主对话、收件箱各放了几个链接。
- 截图测试（Paparazzi）只画一帧：`LaunchedEffect` 里同步改的状态画得出来，要等网络或 `delay` 的画不出来。所以页面要能先装好数据再画：hub 的状态在 `Hub` 的
  StateFlow 里（测试里先 `refresh()`），对话在 `hub.chat(key)`（先等它 `loading = false`），助手的状态在 `AssistantModel`（测试里先 `reload()` 再传给 `ShellRoot`）。
  对话页离开时会 `hub.release(key)` 把对话还掉，同一个测试拍第二张（深色）前要再装一次。页面自己的网络请求有时又会在拍之前回来，盖掉装进去的数据：
  装进去的要和 mock 返回的一样。Paparazzi 的报告目录会留着旧的运行，按名字拷图前先删掉（`android.sh shots` 会删）。
- Paparazzi（layoutlib）里的 `WebView` 是个桩：类在，`getSettings()` 没有，一调就 `NoSuchMethodError`；它也不设 `LocalInspectionMode`、`isInEditMode`。
  要在截图里出现的 WebView（读信）先用反射看 `getSettings` 在不在，不在就画替身（`MailWebView` 画 `MailHtml.toText` 的字）。
- Paparazzi 下没有 Activity：`rememberLauncherForActivityResult` 要 `LocalActivityResultRegistryOwner`，`BackHandler` 要 `LocalOnBackPressedDispatcherOwner`，
  `ShotHost` 给了假的；`JarvisApp.instance` 没有（页面里在组合时读它的要 `runCatching`）；`PowerManager.isIgnoringBatteryOptimizations` 这类系统服务会抛 `Unsupported Service`。
- 两页常驻的外壳：上面那页的根上要有一个 `pointerInput`，否则点在它空白处的手指会落到下面那页；藏起来的那页 `clearAndSetSemantics`，免得读屏读到。
  同一个 Box 里后画的在上面，`zIndex` 会改这个顺序：给页面 `zIndex(1)` 之后顶栏要 `zIndex(2)`，不然被页面盖住（一开始顶栏就是这样不见的）。
- 一行里两个 `weight` 会平分剩下的宽度：标题 `weight(1f, fill = false)` 再加一个 `Spacer(Modifier.weight(1f))`，标题最多只能占一半，短屏上被截断。
  标题和它旁边的小东西放进一个 `weight(1f)` 的 Row，时间放在外面。
- 中文行比英文字符高（Noto CJK 的行距大），列表的「•」和中文同一个字号时会浮在上面：标记和文字都 `alignByBaseline()`。
- `WindowInsets.statusBars.asPaddingValues()` 读的是原始值，不管外面有没有 consume：页面自己用它算顶部留白时（`AgentsScreen`、外壳里的对话），
  要放在从窗口顶部开始的窗格里；用 `statusBarsPadding()` 的页面（`PageHeader`、对话的顶栏）放在只让开了外壳顶栏高度的窗格里，状态栏的那一截它自己补。
- 页面在组合树里换了位置（单栏 ↔ 两栏），`remember` / `rememberSaveable` 的状态都跟着丢：要跨布局留下的东西（草稿）放在页面外面。

- hub 只存一份 phone_* 工具列表（`setPhoneTools`），每台手机 `register_phone` 时整个替换，调用发给最后注册的那台（`phoneClient`）。
  所以安卓声明的工具要和 iOS 一模一样，不能多也不能改参数。安卓 10 以后只有在前台的 App 能读剪贴板，后台调 `phone_clipboard_get` 读到的是空，结果里会说明。
- 两台手机同时连着 hub 时，phone_* 调用发给最后注册的那台：测模拟器上的工具时要关掉真机上的 Jarvis，不然调用发到真机、它那边没开这个数据源就超时。
  Claude Code 只在启动时读一次工具列表，新加的工具要新开会话才看得到。
- 模拟器默认没有能写的日历账号，`phone_calendar_create_event` 会报「no writable calendar」；测的时候用 `content insert` 建一个 LOCAL 日历。
- 已配对时打开配对链接：配对页要等配对做完再清掉 `pendingPair`。先清掉的话 Root 立刻换回正常页面，配对页和它的协程一起被丢掉，链接等于没点。
- 无窗口（`-no-window`）的模拟器上 `adb exec-out screencap` 有时拿到的是上一帧：点完马上截图像是没反应，其实已经跳过去了。截两次、中间等一秒。
- 只读地连真实 hub 看界面：配对页会 `POST /api/v1/auth/tokens` 在 hub 上多出一台设备。debug 包可以 `run-as` 直接写 `shared_prefs/pairing.xml`（`url`、`token`）再启动，看完 `pm clear`；别点会写 hub 的开关（Claude Code 设置、通知、手机数据）。
- 曲线的纵轴按字节时要按 1024 取整（`ChartMath.yTicks(binary = true)`），按十进制取整的刻度显示出来是「1.91 MB/s」「3.81 MB/s」；数值标签的宽度按最宽的那个量，固定宽度会被裁掉。
- 假 hub（`scripts/mock/mock_hub.py`）的 WebSocket 连上时不推待处理的交互：进 App 后下拉刷新一次才拿到交互。
- 飞书页面的截图（`LarkShots`）要 `MOCK_LARK=1` 的假 hub（会话里各种消息、表情库、图片）：图片在后台解码，第一帧拍不到。先用同一个名字拍一次让页面把要的图都发出去，等 `LarkMedia.settle()`（没有在下载的图），再拍真的；链接预览这种「拿回来的东西里还有图」的要来回两三次。默认的假 hub 里 f2、f3 是另一份数据（iOS 的截图、TranslationTests 和 f3 那段画全了的会话用），`MOCK_LARK=1` 时这几个会话换成这一份，两份互不影响。
- 收件箱的详情是 `JSheet`（ModalBottomSheet，单独的窗口），链接卡片和看图盖不上去：会话这种要点链接、看图的页面推成页面画在列表上面，不放进浮层；浮层里的资料卡、话题、removed帖子点链接或图片时先关掉自己（`LarkActions.closing`）。

## App 外壳与页面直达

当前组合根使用 `:core:designsystem` 的 `JarvisTheme`，风格来自 `AppStyleStore`（设置 › 界面风格）。底部固定四个 tab：Jarvis / Agents / 收件箱 / 设置，三套风格顺序相同、都带文字，样子各按各的风格（玻璃是悬浮坞，晨报和看板是底栏）；dock 属于每个 tab 的第一页，推进去的页面（对话、文档、二级设置）盖住它；访问过的 tab 保持组合，切回来不重建（详见 `docs/android-design.md` 第 3 节）。平板和折叠屏按窗口宽度分布局：expanded 时四个 tab 进左侧导航栏，列表和详情分两栏，桌面姿态把对话放上半屏、输入框放下半屏（第 3.1 节）。四个 tab 各自保留返回栈，Agents 角标统计普通 Agent 的待处理交互数，并按 hub 的会话／空间屏蔽及例外规则过滤；Jarvis 角标统计助手待处理交互数，同一会话的多个请求分别计数。收件箱角标统计「需要你」分组中的未处理事项和未屏蔽请求。当前 tab、Agents 机器筛选及各 tab 打开的会话保存在配对目录内，正常冷启动恢复；退出、撤销或重新配对时一并删除，已退出的写入者不能重建位置文件。旧版外部 shell 偏好及准备清单等兼容全局记录在退出配对时清除。

连接状态显示在各页自己的标题位（Agents、收件箱、Jarvis 和对话页），外壳不再在页面上方另加横幅，避免重复和顶部多出空白。轻量刷新失败不另弹提示；用户操作失败由统一出口处理，断网提示共用 4 秒去重窗口。这是提示策略配置，性能未实测。操作结果未知时仍保留会话内的核对入口。

第二批模块入口：

- 对话页终端按钮进入完整终端；助手对话隐藏该按钮。通知的终端兜底（`jarvis://open?target=terminal&machine=…&session=…`）打开同一页面，下面垫着原对话。
- 收件箱事项和飞书会话深链（`target=inbox&item=…`、`target=lark&chat=…`）先等事项列表刷新，再换成收件箱列表会打开的飞书或邮件页面；事项已不在列表时显示说明。
- Agents「列表设置」面板里的「机器监控」进入机器列表；树里每台电脑栏头右边的仪表按钮直接进这台电脑的详情。
- Agents 操作和对话页菜单确认关闭成功后，从所有 tab 移除该会话的聊天、请求和终端页面；结果未知时保留页面供核对。分享目标与 Agents 共用 `agentRank`，同状态再按时间和稳定标识排序。
- Agent 对话页 ⋯ 菜单进入文件；确认是仓库后显示 Git 和改动数。文件和 Git 在原会话内切换，插入路径保留草稿和有效选区，并返回输入框。
- Jarvis 抽屉的「知识库」进入档案、搜索及详情，链接在应用内打开。
- 设置 › 手机数据进入工具权限和开关。应用前台状态与常驻服务实际运行状态共同决定工具可用性；定位和剪贴板仍要求前台。旧配对若没有设备标识，须重新配对。
- 设置 › 助手（`SettingsPage.Assistant`）是 Jarvis 的设置页，Jarvis tab 的标题菜单「助手设置」、右上角头像也打开它，见上文「助手设置」。
- 未配对时可选「没有 hub？先体验一下」。体验模式使用独立数据、缓存及待发队列，设置中可退出；缺少演示数据的终端、机器监控和知识库显示说明，不调用真实 hub。

Debug 直达入口及启动参数解析适配器只存在于 `src/debug/`，复用 `:core:navigation` 的 debug 解析，Release 不注册开发入口。示例中的包名替换为实际构建使用的 Application ID：

```sh
adb -s emulator-5600 shell am start -W -n com.example.jarvis/jarvis.android.debug.DebugEntryActivity --es debug.openTab Agents
adb -s emulator-5600 shell am start -W -n com.example.jarvis/jarvis.android.debug.DebugEntryActivity --es debug.openSession machine/session --es debug.conversationKind agent
adb -s emulator-5600 shell am start -W -n com.example.jarvis/jarvis.android.debug.DebugEntryActivity --es debug.openRequest request --es debug.session machine/session --es debug.conversationKind agent
adb -s emulator-5600 shell am start -W -n com.example.jarvis/jarvis.android.debug.DebugEntryActivity --es debug.openPairing true
adb -s emulator-5600 shell am start -W -n com.example.jarvis/jarvis.android.debug.DebugEntryActivity --es debug.openDiagnostics true
```

每次只传一种 `debug.open…` 目标。`debug.openTab` 可选 Jarvis、Agents、Inbox、Settings；`debug.conversationKind` 可选 agent 或 assistant；`debug.openCatalog true` 打开设计系统组件目录；`debug.openLink '<jarvis:// 链接>'` 按 MainActivity 的规则打开任意深链（终端兜底、通知 tab、收件箱事项、设置子页）；`debug.openPage` 可选 machines、knowledge、phone、catalog，直达没有公开深链的页面。链接里有 `&` 时要再套一层单引号，adb shell 会把整条命令交给设备上的 shell。`debug.theme` 可选 light 或 dark，只影响本应用（含各页面自己套的 JarvisTheme），不更改模拟器系统主题。`debug.style` 可选 glass、brief、board，切换界面风格并保存（和在设置里选一样）。`debug.posture` 可选 tabletop、book，在窗口正中假装有一条半折的铰链（只给截图用：android-35 的 Pixel Fold 模拟器不给 App 报 FoldingFeature）。

状态预览使用 `debug.state`，只在 debug 包解析。请使用未配对的独立测试包；入口会自动进入本地体验模式并跳过准备清单，不访问真实 hub。已配对时不自动退出或替换配对，需要改用独立测试包。状态固定，刷新不会结束预览；不传该参数重新启动即可回到普通体验模式。可同时传 `debug.openTab`、`debug.openSession` 和 `debug.theme` 等既有目标及主题参数，每次仍只选一个目标，不能与 `debug.mockAddress` 同传。

| `debug.state` | 默认目标 | 可截图状态 |
| --- | --- | --- |
| `offline` | Agents | 等待网络，保留演示列表 |
| `connecting` | Agents | 正在连接 |
| `updating` | Agents | 正在更新 |
| `pairing-expired` | Agents | 配对失效提示，保留缓存示例 |
| `certificate-mismatch` | Agents | 证书变化提示 |
| `local-permission` | Agents | 本地网络权限提示 |
| `empty` | Agents | 空会话列表；可指定 Inbox 查看空收件箱 |
| `loading` | Inbox | 持续加载；也可指定会话目标 |
| `load-failed` | Inbox | 加载失败及重试入口；也可指定会话目标 |

```sh
adb -s emulator-5600 shell am force-stop com.example.jarvis
adb -s emulator-5600 shell am start -W -n com.example.jarvis/jarvis.android.debug.DebugEntryActivity --es debug.state offline --es debug.theme dark
adb -s emulator-5600 exec-out screencap -p > /tmp/jarvis-state-offline-dark.png
```

状态夹具及参数名都在 `app/src/debug/`，正式构建不注册状态入口。截图前等待目标页面出现；这些固定状态不用于测量连接或加载性能，性能未实测。

本轮验证：`./gradlew --no-daemon -q :app:assembleDebug :app:testDebugUnitTest` 通过，App 单测共 14 项。进程内 Kotlin 编译复核及独立包构建也通过。在专用模拟器上完成以上 9 个状态的深浅色截图，共 18 张，输出 `/tmp/jarvis-p5-root-shots/`；抽查离线、空列表、配对失效和加载失败页面。Release 产物检查、真实关闭会话和退出配对仍待集成验收。

Debug 包的设置页底部另有一行「组件目录（调试版）」，自动化启动（`debug.automation`）时隐藏。组件目录的路由和页面只在 `src/debug/` 编译，Release 包里没有。兼容 `--es openSession machine/session`。通知、公开深链和 debug 参数最终汇入同一个路由器；旧通知需要等待会话元数据加载后再分流。

## 假 hub 批量截图

先启动专用 Android 模拟器，再指定序列号和输出目录：

```sh
./scripts/app/android.sh shots -s emulator-5600 --out /tmp/jarvis-android-shots
```

命令构建并安装独立测试包 `com.example.jarvis.shots`，只清理该包的数据。随后用 `scripts/mock/mock_hub.py` 在自动分配的本地端口启动假 hub，通过指定设备的 `adb reverse` 自动配对，逐页冷启动并输出深浅色 PNG。覆盖四个 tab、Agent 对话、助手对话（落在 Jarvis 首页的内嵌对话）、请求、终端、飞书会话、邮件详情、配对、连接诊断、通知和准备清单两个设置子页、机器监控、知识库、手机数据及组件目录。假 hub 为这些页面提供了飞书窗口 / 话题 / 文档、邮件正文、知识库、会话文件和 Git 的示例数据。自动化启动从各 tab 根页开始、不恢复上一张的位置，也不弹准备清单；脚本以配对目录里的 `shell-position.json` 判断外壳已起来。浮层、菜单和表单（状态浮层、搜索、任务页、全文、完整请求页、文件 / Git、筛选、细列表、空间页、长按菜单、新建、历史、飞书话题和文档、设备、机器详情、知识库详情、体验模式）仍要在页面上点开再截。示例联系人在启动假 hub 前统一匿名化；实际截图内容来自假数据。

测试不读取用户的 hub 配对，也不连接用户 hub；自动化入口不声明可用手机工具。结束或失败时停止本次假 hub、停止测试包并移除本次端口转发。输出目录还包含假 hub 日志和夹具标识，便于排查缺页。截图等待时间只是脚本配置，不代表页面加载性能；性能未实测。手机权限、国产系统后台存活和真实终端输入法仍需真机验证。

### 改前改后逐像素比对

只挪代码、不许改样子的重构（比如 2026-11-02 把 Agents 树的组件挪进 designsystem），用改前、改后两个包在同一台模拟器上按同样的步骤截图，逐像素比。要比得准，先把会自己变的东西都定住：

- 动画：`settings put global animator_duration_scale 0`（App 据此进入减少动效，转圈和呼吸光都画成静止的样子）。
- 时间：假 hub 的「现在」和模拟器的时钟固定在同一时刻（hub 里的 `datetime.now()` 返回固定值；模拟器 `adb root`、`settings put global auto_time 0`，冷启动时和每次截图前用 `date -u` 设一次）。晨报的问候、循环的「下次 HH:MM」、「N 分钟前」都跟着时钟走，不定住就会在改前改后之间变。
- 状态栏：每轮截图前重新进 SystemUI 演示模式（先 `exit` 再 `enter`），只留时钟和电量；不退出就重复进入会留着上一次的图标（实测前一轮两个 Wi-Fi 图标，后一轮一个）。
- 比对：别用 Pillow 的 `ImageChops.difference(a, b).getbbox()` 判断相同。RGBA 图的 `getbbox()` 只看 alpha 通道，截图的 alpha 全是 255，永远报「相同」（踩过）。直接比 `tobytes()`，差异按通道算。

`android.sh shots` 可以把这些都做掉（2026-11-02，多 hub 那次加的；不设变量时行为不变）：

```sh
SHOTS_STYLE=glass SHOTS_MOCK_PORT=<端口> SHOTS_FROZEN=2026-11-02T04:00:00+00:00 SHOTS_SETTLE=3 \
  ./scripts/app/android.sh shots -s <模拟器> --out /tmp/shots-before-glass
python3 scripts/app/android_shots_diff.py /tmp/shots-before-glass /tmp/shots-after-glass --diff /tmp/shots-diff-glass
```

- `SHOTS_STYLE`：每次启动都带 `debug.style`；`SHOTS_MOCK_PORT`：假 hub 用固定端口（地址会写进配对，改前改后必须同一个端口）；`SHOTS_SETTLE`：冷启动后等几秒再截（之后还会等到连续 3 帧相同，最多 20 秒）。
- `SHOTS_FROZEN`：假 hub 的 `datetime` 和 `time` 换成定住的时钟、`uuid4` 用固定种子（hub_id 每次一样），都在 `android.sh` 的包装里做，不改 `mock_hub.py`；模拟器 `adb root`、关自动时间，整个运行期间每 0.1 秒设一次时钟（机器页的图按秒取时间）。动画倍率设 0，状态栏进演示模式；结束时恢复。
- 比对脚本直接比像素，给出差异像素数、外接矩形，`--diff` 输出差异图；`--tolerance` / `--ignore '<文件名通配>=x0,y0,x1,y1'` 默认关。
- 同一个包连拍两次，仍会不同的只有两处：连接诊断里「已通过身份校验，N 毫秒」（App 实测的探测耗时，单次样本）和玻璃浅色飞书页底部 49 行差 1 的像素（原因未查明）。比对时这两处用 `--tolerance 1 --ignore '*-diagnostics.png=0,Y0,1079,Y1'` 去掉（Y 按风格：玻璃 1385–1425，晨报 1360–1410，看板 1305–1355）。

## 主动做事（设置 › 助手 › 主动做事）

用户要求（2026-11-02）：「jarvis 的会话和记忆等等内容，是运行在哪个设备上的，现在也完全没有设置选项」。「设置 › 助手」拆成两块，这一块管 Jarvis 自己主动做的事：定时任务、它替自己安排的跟进、运行记录（iOS 的同名页面在 `ios/Jarvis/Views/Settings/AssistantProactiveViews.swift`）。代码在 `feature/assistant/…/proactive/`，对外只有 `AssistantProactiveScreen(onBack, onOpenConversation)`：`onOpenConversation` 打开一段助手对话（运行开出来的、跟进接着的、立即运行新开的）；接口和协程作用域默认取 Jarvis tab 宿主（`LocalAssistantHost`）里当前配对的。子页面（某个定时任务、新建、全部跟进、运行记录）和返回在页面里自己管，转场和 App 的页面一样从右边推入。

- 入口：设置 › 助手的「主动做事」一节（在助手设置页里原地打开，打开的对话经 `ChatRoute(kind = Assistant)` 落到 Jarvis tab）；Jarvis 抽屉的「定时任务」在 Jarvis tab 里原地打开同一页，打开的对话直接显示在 Jarvis 首页。
- 首页：Jarvis 的一句话（几个定时任务开着、下一件在什么时候）；「接下来」时间线（左边大号时刻，定时任务的下次运行和跟进混排，最多 4 件）；定时任务（规则的人话「每个工作日 上午 9:00」、下次、上次结果和开关；`created_by: "assistant"` 的头像角上有火花并写明「Jarvis 在对话里建的」，上次没跑起来的角上是红色警告）；已安排的跟进（时间、在哪台机器、接着原对话还是开新对话，⋯ 里立即运行 / 打开原对话 / 取消，取消要确认）；最近 3 次运行。右上角 ＋ 从模板新建（会前准备、周报、下班前盘点，和 iOS 同一套）或空白。长按定时任务：立即运行、编辑、删除。
- 定时任务页：名称、开关、时间（时和分两个滚轮，24 小时，TalkBack 可以像滑块一样调）、周几（一 … 日，快捷「每天 / 工作日 / 周末」，最后一天关不掉）、提示词；已有的还有立即运行（有改动先保存）、查看上次结果、历史记录（最近 5 次，更多进运行记录）、删除（要确认）。改了没保存就返回会先问。
- 运行记录：新的在前，按天分组；已运行 / 没跑起来 / 已跳过，准时或晚了多久、手动运行、原因（hub 常见的英文错误译成中文，其余原样显示）；开出过对话的点开就是那段对话。一个定时任务的历史按 `ref_id` 过滤：`Run` 加了 `ref_id`、`due` 两个字段（hub 本来就返回）。「上次」以运行记录里最新的一次为准：hub 的立即运行不更新 `last_run`，只看定时任务自己的字段会把上一次定时运行的时间和这次手动运行的结果拼在一起。
- 写入：`PUT /api/v1/assistant/routines` 是整表替换。每次改动（开关、编辑、新建、删除）在写之前重读整表，只改这一个定时任务，其余定时任务（包括 Jarvis 在这期间新建的）和 hub 自己的字段原样带回；编辑只写用户改过的字段，Jarvis 在这期间从对话里改的（比如时间）留着。新建的 id 在 App 里生成（`rt_` + 8 位十六进制，hub 只给空 id 分配），回包丢了再点「添加」不会建出第二个。读和写排队、一次一个，开始了的写不会因为离开页面被打断；开关先在界面上动，写失败按 hub 上的值恢复。写入或取消跟进失败先重读核对，hub 上已经是想要的样子就不算失败；正在编辑的定时任务在这期间被删了，提示「已经不在了」，不会把它写回去。
- 立即运行：定时任务 `POST …/routines/{id}/run`、跟进 `POST …/followups/{id}/run`，hub 要等对话开起来才回（它自己最多等 2 分钟），所以这两个请求的读超时是 130 秒（`HttpAssistantManagement`，和新建 Agent 一样；其他请求仍是 15 秒）。成功后打开新对话；跟进跑了就从列表里拿掉（hub 是先拿掉再跑，所以失败后也重读）。运行和取消挂在配对的作用域上，离开页面不会中断。
- 失败提示：用户操作的失败交给 App 的统一出口（`LocalAppFeedback.notice`），没有时（调试入口）显示在页面底部；传输层在配对或线路变了时抛的取消算这次请求失败（「连接刚换过，请再试一次」），页面不会卡在「正在保存」。读取失败显示在对应的区块里，可以重试，已经读到的内容留着。相对时间每 30 秒刷新，最近的一件到点 5 秒后自动重读（按上次读取的时刻算，时钟跳动不会把这次重读取消）。
- 时间：hub 的 `next_run` 按用户时区写、带偏移（`+08:00`）；Android 12 / 13 的 `Instant.parse` 只认 `Z`，这一页统一用 `instantOf`（先 `Instant`，再 `OffsetDateTime`）。`core:contracts` 的 `parseTime` 还是只用 `Instant.parse`，别的页面遇到带偏移的时间在这两个版本上会读不出来。hub 也接受 `9:00` 这样的一位小时，编辑页按 `09:00` 显示和写回。数字一律按 `Locale.ROOT` 格式化。
- 调试入口（只在 debug 包里，`app/src/debug/…/ProactiveDebugActivity.kt`）：直接连假 hub，不读 App 自己的配对，只接受本机回环地址：

  ```sh
  PORT=0 python3 scripts/mock/mock_hub.py        # 打印 mock hub on <回环地址>:<端口>
  adb -s <模拟器> reverse tcp:<端口> tcp:<端口>
  adb -s <模拟器> shell am start -n <包名>/jarvis.android.debug.ProactiveDebugActivity \
    --es debug.mockAddress http://<回环地址>:<端口> --es debug.theme dark --es debug.style board --es debug.page routine:morning
  ```

  `debug.page` 可选 `routine:<id>`、`new`、`new:<模板序号>`、`followups`、`runs`、`runs:<定时任务 id>`，不传是首页；`debug.theme`、`debug.style` 和 App 的调试参数一样。点到要打开对话的地方，调试入口只弹框说明会打开哪段对话。接进设置以后，也可以在真实外壳里直达：配对假 hub 后 `--es debug.openLink 'jarvis://open?target=settings&page=assistant' --es debug.assistantSettings proactive`。
- 假 hub：示例数据按 UTC+8 的规则排（上次运行就是规则的上一次，截图用的模拟器设成这个时区），`next_run` 和 hub 一样带 `+08:00`；`MOCK_SCHEDULE=empty` 什么都没有（空状态）；`PUT` 和 hub 一样校验名称、提示词和 HH:MM，按 id 保留 hub 的字段、算下次运行；立即运行和取消对不存在的 id 回 404，每次立即运行记一条手动运行、更新定时任务的上次状态（不动 `last_run`）；`GET /mock/schedule` 看立即运行了哪些，`GET /mock/schedule/reset` 恢复初始数据。
- 验证（2026-11-02）：单测 `ProactiveStoreTest`（整表 PUT 带回其他定时任务和 hub 字段、开关立即生效和失败恢复、回包丢失不算失败、编辑只写改过的字段、新建重试不重复、被删的不写回、删除、取消跟进和失败、立即运行跟进和定时任务、读取失败保留旧数据、传输层的取消算失败）、`ProactiveTextTest`（规则、时刻和偏移、倒计时、晚了多久、错误、上次运行、跟进去向、时间线和那句话、页面键）。模拟器上连假 hub 把开关、编辑保存、长按立即运行、取消跟进、立即运行跟进、模板新建、删除、未保存返回都点了一遍，hub 收到的内容都对；真实外壳里点过设置 › 助手 › 主动做事、返回、运行记录打开对话（落到 Jarvis tab）、Jarvis 抽屉「定时任务」打开和返回；三套风格 × 深浅色、手机和平板都截图看过。真机和真实 hub 没有测；页面性能未实测。
- 设计系统里没有时间选择和周几选择的组件：这两个件写在 `proactive/ProactiveComponents.kt` 里，只用 token（色板、字体、`shapes.chipShape` / `controlShape`、`Tokens.Space`），不按风格分支；以后设计系统有了对应组件再换过去。

## 主线旧 App 新增、待移植到新架构

主线（`origin/master`）在 `81dc6928` 之后给旧单模块 App 加的功能和改动。本分支已经用多模块新架构重做了 Android，下面这些新架构还没有对应实现，逐项待移植，不混进上面的新架构描述。PR 号来自 `git log 81dc6928..origin/master --format='%h %s' -- android docs/android.md`。

- 待办（todos，#427）：侧边栏「待办」（带未完成数）、待办列表和详情、「交给 Agent」（新开或交给已有的，带 `todo_id`）、Agents 的「按待办」分组和关联 / 解除关联、对话和会话信息里显示关联的待办；hub 没有待办接口时入口隐藏。
- 邮件阅读和写信、设置 › 连接器（#320）：邮件详情（线程、锁住的 WebView 渲染 HTML、显示图片后才放行、打开即已读、加星标）、写信（回复 / 回复全部 / 转发、收件人校验、有内容返回要确认）、「交给 AI」和 AI 草拟回复；设置 › 连接器（这台手机、邮箱的添加 / 测试 / 删除、飞书的 lark-connector 状态）；Gmail 用 Google 账号登录，要 `config.json` 的 `android.google_oauth_scheme`（`OAuthRedirectActivity`），没配时提示改用应用专用密码。
- 飞书会话和removed，收件箱里点开进页面（#365）：飞书会话页（富文本、卡片、合并转发、回复、表情回应、话题、`start_position` 分页、已读上报、缓存最后一页）、removed帖子页、图片和表情缓存（`LarkMedia`）、飞书自己的色板和尺寸。
- 手机数据：Health Connect 的 `phone_health_summary`（#278）：读每日汇总（七种指标，按本地自然日切分），权限走 Health Connect 授权框，缺的指标列在结果的 `not_granted` 里；依赖 `connect-client:1.1.0-alpha08`，Android 8–13 要装 Health Connect App。
- 收件箱：在别处已经读过的事项不再标成 @all / 未读（#246）。
- 脚本目录调整（#505）：`scripts/` 按用途分了子目录，本文档统一写成 `scripts/app/android.sh`、`scripts/mock/mock_hub.py`。

### 旧版已有、新架构文档还没逐项写的

这些在主线旧 App 里已经有（PR 号这里没有逐个核对，要查就看 `git show origin/master:docs/android.md` 的对应小节）：

- 配对欢迎页：扫 `jarvisd hub pair` 的二维码（`jarvis://pair?url=…&token=…`）或手填地址和 token；配对后用 `POST /api/v1/auth/tokens`（`kind: phone`）换这台手机自己的 token，旧 hub 没有时沿用配对 token。
- 外壳：顶栏 ☰ ·「助手 | Agents」切换 · 圆按钮；侧边栏（横向拖出、对话历史置顶 / 最近、「新对话」药丸）；弱网时顶栏下的小药丸（等待网络 / 连接中…）。
- Agents 列表：需要你卡片、按项目 / 状态 / 机器分组、置顶和屏蔽、左滑关闭、长按菜单、搜索和「全部 / 需要你 / 未读 / 工作中」筛选；空间页（在同一个空间开新标签）；空间标题的 ⋯（置顶、屏蔽、重命名）。
- 对话：顶栏胶囊「空间 › tab」和会话信息（`GET …/info`，拷贝 session id 和恢复命令）；麦克风（系统语音识别器，`RecognizerIntent`，手机没有语音识别时不显示）；快捷命令（/compact、/context、/clear 等，`/clear` 先确认）；API 报错卡片（白话原因、连续计数、「重试」）；排队中的消息（撤回编辑 / 删除，`POST …/queue {keep}`）；权限模式、思考强度和 herdr 里 Claude 的模型标签（`POST …/modes`、`/model <别名>`）。
- 对话：工作过程折叠（每轮「工具 · 思考 · 回复 · 时长」，点开逐步展开）、消息长按菜单（复制、选择、引用、分享、交给 Jarvis、作为上下文、编辑）；图片全屏查看、左右滑、保存相册、框选和画笔编辑；附件（照片、拍照、文件；文件不自动发送）。
- 对话里的实时终端：VT 子集、手机尺寸接管、键栏和 Ctrl / Alt / ⇧ 锁定、电脑尺寸只看、字号、选择文本拷贝（`docs/terminal.md`）；文件和 Git（只读：目录、查看器、分支、改动、diff、提交历史）。
- 新开会话：选电脑、目录（最近用过）、启动器、权限模式、可选第一条消息；`async` 立即打开对话，在电脑上等 Claude 就绪再发。
- 引擎选择（配对后为 hub 所在的机器选，`PUT /api/v1/machines/{id}/engine`，机器页的「引擎」行）；机器列表、机器页和重新连接。
- Jarvis：资料页（定时任务、跟进、运行记录、关于我、记忆）；知识库（类型、项目分组、标签筛选、全文搜索、条目页、「问 Jarvis / 开 Agent」、打标）。
- 链接卡片（`ui/web/`）：铺满屏幕、下拉到 0.6 时后面的页面挤到上面 40%、`WebViewPool` 预加载、选中文字「AI 修改」「作为上下文」（`<jarvis-context kind="edit|context">`）、`LinkSafety` 的 scheme 规则、证书只认系统 CA。
- 设置 › AI › 模型配置（按用途选模型，`/api/v1/model-config`）；语音对话（主线 `docs/android.md` 的「语音对话」一节）。
- 系统与监控：1 / 6 / 24 小时曲线、按住读数、每个核心、系统信息；Claude Code 设置（`~/.claude/settings.json` 的白名单，同时改所有在线机器）；设备列表（撤销 token）；通知偏好（需要你时 / 做完时，`devices/{id}/prefs`）；存储空间（按类清理）。
- 界面风格：旧版是五种（液态玻璃、Claude、黏土、Island、飞书，和 iOS 同一组，`SharedPreferences` 的 `ui.style`），新架构是三种（玻璃、晨报、看板）。两边是否对齐，待定。
- 常驻通知、小组件、磁贴、快捷方式（`fleet/`、`widget/`、`shortcuts/`，排序规则照 hub 的 `buildFleet`）。旧版验证过：通知展开的「批准」hub 收到 `{approve:true, mode:"acceptEdits"}`，小组件和磁贴，「批准第一个待处理」，「问 Jarvis」冷启动也能进去；没验证：通知上的「允许 / 拒绝」、Live Update、真机锁屏。
- 折叠屏和大屏：两栏（Agents / 空间页 / 收件箱 / 设置），`Fold.kt` 的折痕和分栏规则，`Panes.kt` 可拖的分隔条（新架构有 expanded 布局，细节待对）。
- 界面风格的截图（旧版，2026-11-01 之后）：模拟器连 `scripts/mock/mock_hub.py`，五种风格 × 浅色 / 深色截了 Agents、两种交互卡片、Jarvis 主对话和抽屉、资料页、收件箱、设置、风格选择页、配对页（切风格可以直接写 `shared_prefs/ui.xml` 后重启 App）。
- 旧版截图验证（2026-11-01）：Paparazzi 在 JVM 上用 layoutlib 画 Compose，连 `scripts/mock/mock_hub.py`，命令 `./scripts/app/android.sh shots`（`scripts/app/android_shots.py`）。覆盖外壳、对话、收件箱、机器、空间页、设置、知识库、Jarvis 资料页、配对、风格、语音、文件和 Git、小组件等，深浅色各拍一遍。手势、键盘、拍照、听写、真实网络、通知没在这条路上验证。
