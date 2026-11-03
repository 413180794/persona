# Persona 客户端重设计与 Android 版：设计方案

> 状态：**草案 v3，等待审阅**（2026-10-30）。到目前为止只做了调研和设计，没有改任何代码。
> 第 12 节的问题 2026-10-30 已按建议定；hub 有没有可以做 DDNS 的域名、手头有哪些真机，等你补充。动手按第 10 节的阶段。
>
> **这一轮的范围**（2026-10-30 你定的）：
> - **只做 Android 和 hub，iOS 只读不改**。iOS 只当参考：读它的实现和测试来写 Android。凡是「两端」的规格，这一轮只在 Android 上落地，iOS 以后再跟进。
> - **手机没有 Google 服务**。
> - **手机经公网直连 hub，优先 IPv6**；VPN 用户自己配。
> - hub 的所有改动都必须让**现有的 iOS App 不改代码也照常工作**。
>
> **v3 相对 v2 改了什么**：
> - 按上面的范围重写：通知只走常驻连接，FCM、Firebase、Google 登录都去掉（D8）；新增公网直连（D9，[hub-changes.md](hub-changes.md) 第 1 节重写）；iOS 的改动全部挪到「以后」。
> - 新增国产系统适配（[android.md](android.md) 5.18）：minSdk 31 有了数据依据，各家的后台和通知限制、准备清单、自检、分发和 App 内更新。
> - 新增 [flows.md](flows.md)（15 张主要功能流程图）和界面预览 `prototypes/android/`；新增 3 份调研笔记。

这一组文档回答四件事：

1. removed（作者的另一个原生 iOS 项目：lark-connector 飞书客户端 + 手机经 SSH 驱动 herdr 里的 Claude Code）有哪些长处值得搬；
2. 重设计后的目标结构和交互：这一轮先在 Android 上落地，iOS 以后再跟进；
3. 怎么照着重设计后的样子做一个 Android 版；
4. 已经合入 master 的 Android 第一版（PR #84，Agents 部分）哪些选型比本方案好、哪些要按本方案替换（[android.md](android.md) 第 1 节）。

| 文档 | 内容 | 建议 |
| --- | --- | --- |
| 本文 | 结论、关键决策、目标架构、路线、风险、待决问题 | 先读（约 20 分钟） |
| [ux.md](ux.md) | 交互与界面规格（两端共用）：信息架构、各页改动、组件、状态机、文案词表、设计 token | 想看「改成什么样」时读 |
| [android.md](android.md) | Android 技术方案：和 PR #84 的逐项对比、技术栈、模块、关键子系统、平台映射、首版范围、测试与分发 | 想看 Android 怎么做时读 |
| [ios-refactor.md](ios-refactor.md) | iOS 以后跟进时的分阶段步骤（**这一轮暂不执行**） | 以后再看 |
| [hub-changes.md](hub-changes.md) | 服务端要改的东西（必须 / 建议 / 可选） | 想看要动 hub 多少时读 |
| [parity.md](parity.md) | 功能对齐矩阵：iOS 现有 213 项 + 重设计新增项，每项的 Android 做法和优先级 | 作为验收清单 |
| [flows.md](flows.md) | 主要功能的流程图（Mermaid）：配对、选路、常驻连接、通知审批、发消息、对话同步、收件箱 | 想看「怎么走」时读 |
| [`prototypes/android/`](../../prototypes/android/) | Android 界面预览（HTML，可点，深浅色） | 先看这个 |
| [research/](research/) | 11 份调研原始笔记（带代码出处和来源） | 需要核对证据时查 |

---

## 0. 一页结论

- **一份产品规格，两个原生客户端；这一轮只落地 Android。** Android 用 Kotlin + Jetpack Compose 原生开发，iOS 维持现状、不改。两端不共享代码，共享三样东西：
  - **行为规格和夹具**：同一批 JSON 输入、同一批期望输出。这一轮读 iOS 的实现和测试（只读），在 Android 侧写成夹具；连续性规则用 Go 端已有的仿真。以后 iOS 跟进时接上同一批夹具；
  - **设计 token**：一份 JSON，值取自 iOS 现在的 `Theme` / `StatusPalette`，这一轮只生成 Kotlin；
  - **hub 协议**：由 Go 测试导出的样本。

  Android 的核心模块写成不依赖 Android 的纯 Kotlin，以后真想用 KMP 共享，只改构建配置。
- **Android 按这套模块分层做。** 借 removed 的「契约 / 实现 / 页面」分层，但只分十几个模块。
  - 层次：`Contracts`（线上类型和窄接口）→ `Core`（纯逻辑）→ `HubClient`（网络）→ `Data`（持久化和同步）→ `DesignSystem` → 各 `Feature` → `App`（组合根）。
  - 页面只依赖接口，靠编译期保证。
  - iOS 以后跟进时照同一套拆，包括把 `AppModel` 这个承担 22 类职责的大对象拆成十来个服务（[ios-refactor.md](ios-refactor.md)，这一轮不做）。
- **可靠性借 removed 的「结果三态」，但重活放在 hub 上。**
  - 三态是：成功 / 明确没发出 / 结果未知。
  - 所有有副作用的请求都带操作 id，hub 把回执落进 Postgres。
  - 「没发出去」自动重发；「发出去没收到回音」用同一个 id 重试，由 hub 去重；电脑上「打字打到一半出错」就进入「待核对」，用对话记录自动核对。
  - 这就不需要 removed 那套手机侧的持久队列和人工核对体系。
- **交互上保留 Persona 的强项，补上 removed 的阅读工具和原生交互的完整度。**
  - 保留：信息架构、倒置列表、工作行折叠、终端、推送。
  - 阅读工具：会话内搜索、任务进度、全文阅读器、折叠时也显示失败数。
  - 原生交互：拒绝原因、计划意见和批准互斥、倒计时、MCP 表单、子 Agent 来源、多个请求进收件箱。
  - 一处状态：会话页只在输入区上方一条状态行说「现在怎样」，「待核对」是正式状态。
- **Android 以已合入的 PR #84 为起点重组，不推倒重写。**
  - 它约 2.7 千行，是单模块，真机验证过，本机 10 个单测全绿。
  - 它比本方案原稿更好、直接采用的：通知不靠推送服务，用前台服务常驻 WebSocket，国内手机大多没有 Google 服务；扫码用 zxing，不依赖 Google；明文 HTTP 整体放开（network security config 不支持网段）；target 停在旧版本，绕开 Android 17 的本地网络权限；通知上能直接选提问的选项，处理完撤回通知。
  - 按本方案替换的：工具链和 SDK 级别、模块和架构、存储（token 现在明文存）、对话展示（没有工作行折叠，也不支持 `fold_work`）、Markdown、状态色（和 iOS 不一致）、通知「允许」不需要解锁。
  - 逐项对比和三张清单（保留、搬家、替换）见 [android.md](android.md) 第 1 节。
- **Android 首版 = iOS 的 76 个 P0 功能，外加「分享到 Persona」、公网直连、国产系统适配和 App 内更新。**
  - 包括：配对、连接状态和诊断、缓存、离线发送、Agents 列表、对话页、四种交互面板、Persona 主对话、收件箱基础功能、带按钮的通知、一套原生风格主题。
  - 终端、Live Updates（锁屏常驻通知）、手机工具放第二批；手表和语音助手不做。
- **Android 按「手机没有 Google 服务」设计。**
  - 通知只走常驻连接：前台服务一直连着 hub 的 WebSocket，在手机上发通知。PR #84 已经在用，不做 FCM。
  - 任何依赖 Google Play 服务的库都不引入。扫码用 zxing，定位用系统的 `LocationManager`，Gmail 不在 Android 上做 Google 账号登录。
  - 要适配主流的 Android 版本和国产系统（[android.md](android.md) 5.18）：
    - minSdk 31：按 StatCounter 中国的页面浏览量算覆盖约 91%，包括还能装 APK 的鸿蒙 3 / 4；鸿蒙 5 及以后不能直接装 APK，首版不承诺。
    - 各家都默认限制后台和通知（小米自启动默认关、华为鸿蒙 4 的本地通知默认静默限频、荣耀「智能省流量」断后台流量……），而且没有一家提供能查这些开关的接口。所以准备清单按系统逐项引导，再用「锁屏测试通知 + hub 记录的断开时段」自检。
    - 没有 Play 商店：APK 放在 hub 上，App 内检查更新，安装时弹系统确认。
  - 建议 hub 做两处小改动：「做完了」由 hub 判断后经 WS 下发，手机不再自己算，和 iPhone 的推送策略一致；手机工具按设备注册，iPhone 和 Android 同时在线时不再互相覆盖。
  - 另外建议同时做：消息和操作的幂等回执落库、飞书发送幂等、客户端也上报自己的能力、统一错误体、应用层 ping。
- **手机经公网直连 hub，优先 IPv6（D9）。**
  - hub 新开一个 TLS 端口，用自己的密钥签证书，二维码里带公钥指纹，手机钉扎；
  - hub 自己发现能用的地址（公网 IPv6、DDNS 名字、内网、确认可达的公网 IPv4）并下发；手机错峰并发探测，证书和 `hub_id` 都对上才算连上；
  - 二维码格式不变，明文端口留给内网，现有 iOS 照常用。
  - 门槛在家里：光猫的 IPv6 入站防火墙、家宽前缀会变、部分地区封端口。App 和 hub 解决不了，只能检测和引导（`personad hub doctor`、App 的连接诊断页）。强烈建议给 hub 配一个 DDNS 名字。
- **执行顺序：**
  1. 先定规格：读 iOS 的实现和测试，写出夹具和 token JSON；hub 做公网直连等改动，每一步都兼容现有 iOS。
  2. Android 先在 PR #84 上升级工具链、拆出纯 Kotlin 核心并接上夹具，再换对话页、交互和通知。先验证三个最大的风险：长对话滚动性能、常驻连接的耗电与存活、手机在蜂窝网络上经公网 IPv6 连上 hub。
  3. 之后按模块分车道，多个 agent 并行。
- **第 12 节的问题已按建议定（2026-10-30）。** 还差你补充两件事，都不挡 P0：
  - hub 有没有可以做 DDNS 的域名？没有的话，人在外面遇到家宽换前缀，手机会找不回 hub。
  - 手头有哪些真机能测（品牌、系统版本）？决定 A1 的耗电、存活、运营商空闲超时这些实测先在哪几台上做。

---

## 1. 背景与目标

### 1.1 现状

| | Persona iOS | Persona Android（PR #84，已合入） | removed |
| --- | --- | --- | --- |
| 规模 | 约 4.2 万行 Swift（生产约 3.76 万），单个 XcodeGen 工程，`Shared/` 以源码形式编进 App、手表、小组件三个 target | 约 2.7 千行 Kotlin，单模块 `:app`，只有 Agents 部分 | 约 17 万行 Swift（源码约 9 万 + 测试约 6.6 万）+ 主机组件（Node）约 1.3 万行；一个本地 SPM 包，39 个 target |
| 后端 | 自己的 hub（Go + Postgres），手机只连 hub；hub 经 SSH 起各台电脑上的 node | 同一个 hub，没改服务端 | 没有中转：手机直连 lark-connector（HTTP + WS）和各台主机（SSH exec + PTY） |
| 测试 | 24 个单测文件约 2.7 千行；16 个 UI 测试 + Python 写的 mock hub；约 80 个 DEBUG 启动参数 | 10 个 JUnit（本机已跑通）；模拟器 + 一台没有 Google 服务的真机手动验证 | 173 个测试文件约 6.6 万行，没有 UI 测试；用合成服务 + 隔离模拟器 + 契约脚本验收 |
| 突出的地方 | 实时推拉同步、推送和锁屏实时活动、多机器状态模型、对话展示、终端、无头助手 | 不靠推送服务的通知（前台服务常驻连接）、通知上直接作答 | 可靠性工程（结果三态、幂等回执）、编译期分层、聊天列表的正确性、原生交互的完整度、评审和验收方法 |

### 1.2 目标

1. **Android 版**：
   - 核心路径（看进度、在手机上拍板、处理收件箱、和 Persona 对话）体验不输 iOS，达到「Play 商店榜首」的完成度；
   - 在没有 Google 服务的主流国产手机上能用；
   - 经公网直连 hub。
2. **hub 改动**：支撑公网直连和 Android，同时现有 iOS App 不改代码也照常工作。
3. **以后**：iOS 跟进同一套结构和交互（[ios-refactor.md](ios-refactor.md)）；两端长期一起演进，新功能写一次规格，两端各自实现，用同一批夹具和同一张对齐表验收。

### 1.3 非目标

- 不改服务端架构：hub 中转保留，不做 removed 式的手机直连 SSH（手机不碰私钥、主机指纹、跳板机）。
- 不改「不做第二个飞书」的约束：removed 的飞书首页、通讯录、搜索、审批、工作台等一概不搬（`docs/feishu.md`）。
- 不共享 UI 代码（不用 Compose Multiplatform）。
- Android 首版不做 Wear OS 应用和语音助手入口（换形态的方案见 [android.md](android.md) 第 6 节）。

---

## 2. 调研结论（摘要）

详细证据见 [research/](research/)，下面每条都能在笔记里找到代码出处。

### 2.1 removed 值得借鉴的

| # | 借什么 | 它在 removed 里是什么样 | 用到 Persona 哪里 |
| --- | --- | --- | --- |
| 1 | **编译期强制的分层** | 模型和窄协议放 `*Contracts`，实现放 `*Client`；`*Feature` 只依赖 Contracts、从不依赖 Client；只有 App 同时看到实现和页面，负责注入 | 两端同构的模块划分（第 4 节）；拆掉 `AppModel` 和 755 行的 `APIClient` |
| 2 | **结果三态 + 写前日志 + 只读核对** | 写请求在传输层出错一律算「结果未知」，不自动重放；主机用「排他创建的回执文件 + 参数摘要」做持久幂等 | 改成 hub 落库的幂等回执，客户端因此可以放心自动重试（决策 D3） |
| 3 | **作用域 + 代次** | 凭据、缓存、草稿、日志都按「连接 → 账号 → 会话」分区并带代次；传输层拒绝重连之前的旧请求；await 回来先比对版本号 | 本地数据按 `hub_id + 配对代次` 分区；页面模型统一用「版本号守卫」（决策 D6） |
| 4 | **双向能力协商 + 宽容解码** | 主机在 hello 里报协议版本和能力；客户端在请求里声明自己认识哪些卡片类型；可选字段解码失败只丢这一个字段 | 客户端也上报平台、版本、能力（决策 D5），iOS 和 Android 多个版本并存时 hub 可以降级 |
| 5 | **并发模板** | 模型持有并取消自己的任务；忙时来的刷新合并成一次补读；多个页面共用一个订阅；事件流有上限，溢出就整体重读；「意图 + 确认 token」两步提交，过期的确认框提交不了 | 写进两端的页面模型规范（[ux.md](ux.md) 第 6 节，[android.md](android.md) 第 4 节） |
| 6 | **原生交互的完整度** | 拒绝可以写原因，填了就不能点允许；计划写了意见就不能直接批准；有倒计时；显示 MCP 来自哪个服务器；支持 MCP 弹出的表单和链接；标出子 Agent 来源；没提交的输入按请求保存；权限卡挂在记录末尾，多个请求有汇总 | 交互面板升级（[ux.md](ux.md) 3.4）。其中拒绝原因 hub 早就支持，App 从来没传过 |
| 7 | **阅读工具** | 会话内搜索（连工具输入输出一起搜，命中后高亮，能一键回到搜索前的位置）；任务进度汇总页；长文本全屏分页阅读器（可在文内搜索）；工具步骤折叠时也显示「N 项失败」 | 对话页新增（[ux.md](ux.md) 3.3） |
| 8 | **聊天列表的正确性** | 阅读锚点 =（消息 id，行内偏移）；只在前台、定位完成、没被遮挡时才上报已读；发送带客户端 id、确认只认 id 不比文字；断线后从锚点向两边补读，只有服务端标明这一页完整才删本地的行 | 飞书会话页（现在有发送不幂等、刷新可能缺段、重连不刷新等问题）和 Agents 对话页共用（[ux.md](ux.md) 3.5） |
| 9 | **不确定状态的统一表达** | 「结果待确认 / 待核对」有固定的词、图标、颜色，配一个只读的核对入口 | 新增「待定状态」组件族和文案词表（[ux.md](ux.md) 第 5 节） |
| 10 | **首次接入拆细** | 连接进度分段显示真实阶段，不给假百分比；失败按 DNS / 拒绝 / 不可达 / 超时 / 认证分类，每类给可照做的提示 | 配对流程（[ux.md](ux.md) 3.7） |
| 11 | **评审和验收的方法** | 先做 HTML 可点原型 → 写评审文档 → 用户认可 → 原生落地 → 写验收记录；「通过」分级（已开发 / 协议 / 模拟器 / 真实账号 / 真机）；跨语言的固定测试向量；从源码生成「接口 → UI」对照表 | 第 9 节的质量与验收 |

### 2.2 不借的

- **手机直连 SSH 那一整套**：私钥库、主机指纹、跳板机、主机组件安装器。Persona 的手机只连 hub。
- **手机侧第二套持久队列和人工核对体系**：主机 SQLite 队列、后台服务、每条消息十几种状态。removed 是因为没有常开的中转才需要这些；Persona 交给 hub。
- **轮询式观察**：会话页每秒、主机列表每 8 秒轮询一次。Persona 已经有 WebSocket + rev 增量同步。
- **每种操作一个手写日志类**：removed 有 31 个、共 4,088 行。Persona 用一个通用的操作日志。
- **39 个过细的 target**，以及对不可信上游那种逐字段的防御式校验。Persona 只信任自己的 hub，校验集中在 hub 边界和契约测试。
- **60 行 VStack 窗口渲染**：Persona 的倒置列表 + 独立懒加载行在性能和「输入紧跟输出」上更好。
- **视觉 token**：removed 的设计系统只有约 300 行，字体、间距、圆角都没有 token，比 Persona 弱。

### 2.3 Persona iOS 现在的主要问题

| 问题 | 证据（详见 [research/jarvis-ios-arch.md](research/jarvis-ios-arch.md)） |
| --- | --- |
| `AppModel` 是上帝对象 | 998 行加 3 个扩展文件，承担 22 类职责；和 `AgentSync` / `LarkSync` / `InboxStore` 通过 `.shared` 互相调用，形成环 |
| 核心逻辑在 View 里，没有单测 | 聊天行折叠 `chatLines` 约 285 行，是 `ChatView` 的 private 方法；Agents 排序 `rank` 在 `ChatsView`；收件箱分组在 `InboxView`；飞书已读上报在 `LarkChatView` 的 `@State` 里 |
| 同一规则多处实现，已经不一致 | 状态排序 3 套（代码、手表、`docs/agent-states.md` 互相矛盾）；计划审批 5 个入口用了 3 种模式（通知和实时活动是 acceptEdits，Siri 和手表是 bypassPermissions）；深链路由 3 份；发消息只有 1 条路径走 outbox，另外至少 5 条直连 hub |
| 协议没有单一来源 | Go、Swift、`docs/protocol.md` 三份手写，已经漂移（hub 广播的 `assistant_schedule` App 不认；文档里的 `session` / `ping` 事件 hub 并不发） |
| 可靠性有缺口 | node 对 `client_msg_id` 的去重只在内存里保留 10 分钟（`server/internal/node/sendsafe.go`）；App 把**超时**也当「没网」自动重发（`SendSafety.swift`），hub 或 node 重新部署后可能打两遍；outbox 和草稿存在 Caches 目录、`try?` 吞错、不按 hub 分区 |
| 飞书会话页的问题 | 发送不幂等：hub 每次随机生成 clientId（`server/internal/lark/lark.go`）；刷新时把最新一页直接拼到末尾，两次刷新间来的消息超过一页就会缺一段；重连后已打开的会话页要等 60 秒轮询；从收件箱打开时没有新消息分界线、不先显示缓存 |
| 大文件是并行开发的冲突源 | 一周内 `ChatView.swift` 被 38% 的 iOS 提交改到（243 个里 93 个） |

### 2.4 Persona 更强、重设计必须保留的

- hub 推拉同步 + items 连续性（`history` / `from_seq` / `to_seq`，断档自愈），见 `docs/lanes/msg-render-plan.md`。
- 推送（授权和计划能在通知上直接批准）、锁屏实时活动。
- 多机器 + 统一的状态模型：需要你 / 完成未看 / 工作中 / 待唤醒 / 已屏蔽 / 已看。
- 对话展示：每条消息自成一段、工作行原地展开成独立的懒加载行、倒置列表精确贴底。
- 终端：hub 中转的实时流和续传、手机尺寸接管、滚轮转发。
- 无头助手、知识库、收件箱的 AI 分拣。
- 屏幕卡：任何 TUI 对话框都能变成原生卡片，按键前比对 `rev`。

### 2.5 PR #84（Android 第一版）的评估

结论是**以它为起点重组，不推倒重写**：真机验证过的部分保留，结构、界面和存储按本方案替换。

| 比本方案原稿好、直接采用 | 按本方案替换 |
| --- | --- |
| 前台服务常驻 WebSocket，本地发通知：国内手机大多没有 Google 服务，FCM 收不到；厂商推送又要开发者账号 | 工具链和 SDK 级别（AGP 8.7 / Kotlin 2.0 / target 34 / minSdk 26） |
| 扫码用 zxing，不依赖 Google Play 服务 | 单模块、全局 `Hub`、没有 ViewModel 和 DI |
| 明文 HTTP 用 `base-config` 整体放开：network security config 不支持网段，hub 地址又是配对时才知道的 | 配对 token 明文存 SharedPreferences；没有缓存、没有 outbox |
| 通知上直接选提问和屏幕对话框的选项；交互处理后撤回通知 | 每个工具调用一行，没有工作行折叠；不支持 `fold_work` |
| 只在前台上报「看过」 | 自写的 Markdown（没有表格、代码高亮） |
| `config.json` 的 `android.application_id`、`scripts/android.sh` | 状态色（工作中绿、完成未看蓝，和 iOS 不一致）；通知上的「允许」不用解锁；「做完了」由手机自己判断 |

逐项对比和怎么重组见 [android.md](android.md) 第 1 节。

> 调研基于 master 的 77999ab，即 PR #84 和另外约 20 个提交合入之前。这之后 iOS 又多了几样：
> - 长会话首页折叠工作过程（`fold_work`）；
> - 体验模式（App 内假 hub）；
> - iPhone Duo 双栏；
> - Agents 列表的快速筛选、细列表、折叠空间；
> - Persona tab 的顶栏和侧边栏；
> - 助手写飞书前在手机上确认。
>
> 这些已补进 [parity.md](parity.md) 第 26 节；research 笔记里的行号对应 77999ab。

---

## 3. 关键决策

### D1. 代码共享：两端原生 + 共享规格（推荐）

| 方案 | 结论 | 理由 |
| --- | --- | --- |
| **A. 两端原生，共享规格和夹具** | **采用** | Android 体验最好，系统能力和性能工具都是一手的；iOS 4.2 万行 SwiftUI 不受影响；构建链最简单，多个 agent 并行时互不拖累。需要两端行为一致的纯逻辑只有约 5 千行，而且项目已有「一份规格 + Go 仿真 + 夹具」的先例（`server/internal/transcript/continuity_corpus_test.go` 就重写了一遍 App 的连续性规则） |
| B. KMP 共享核心 | 暂缓，但 Android 核心按它的边界写 | 收益真实，但 iOS 要把已经踩平坑、有测试的 Swift 数据层换成 Kotlin。Swift export 截至 2026-10 仍是 Alpha；走 SKIE 的话，每次升 Kotlin 都要等它跟上；Swift 6 严格并发下 Kotlin 导出的类型不是 Sendable；手表和实时活动扩展也要链接 K/N 框架，而扩展有内存上限 |
| C. Compose Multiplatform 连 UI 一起共享 | 不采用 | 没有官方的液态玻璃，iOS 原生文字输入也还是实验特性，等于为了共享 UI 牺牲 iOS 的手感 |
| D. 把「轮次归组 / 挑选回复」上移到 hub | 不采用 | 看起来能让三端都变薄，但「真正的回复」取唤醒后最长的那段（`Turn.pickAnswerAcrossWakes`），新内容到来时会**回头改变**前面 item 的归属，和「item 只追加、按 seq 增量」的协议模型冲突 |

落地规则：

- 仓库根目录新建 `spec/`：
  - `spec/fixtures/`：放夹具。这一轮读 iOS 的 `chatLines`、`ChatStoreTests`、`RebuildTests` 和终端录制帧（只读，不改 iOS），在 Android 侧写成 JSON 用例；连续性规则用 Go 的仿真（`continuity_corpus_test.go`）导出。
  - `spec/tokens/`：放设计 token。
  - `spec/README.md`：每条行为规则在哪个夹具里。
- 这一轮只有 JUnit 读这批 JSON；以后 iOS 跟进时，XCTest 读同一批。任何一边改了规则，先改夹具。
- Android 的 `:core:contracts`、`:core:domain`、`:core:terminal` 是纯 Kotlin/JVM 模块，只依赖 kotlinx.serialization、coroutines 和 datetime。
- 重新评估 KMP 的条件：Swift export 进入 Beta，并且 Android 已经稳定跑过一个版本。

### D2. 分层与模块（两端同构）

```
App（组合根、路由、HubScope 容器、DEBUG 钩子）
 ├─ Feature：Chat · Agents · Assistant · Inbox · Terminal · Readers · Machines · Settings
 │     只依赖 ↓ Contracts / Core / DesignSystem（和少量可复用 Feature）
 ├─ Platform：推送 · 实时活动 / Live Updates · 手机工具 · 快捷入口 · 手表
 ├─ Data：安全存储 · 操作日志（outbox / 草稿）· 缓存 · 同步引擎
 ├─ HubClient：按领域拆的 HTTP 客户端 · WebSocket · 连接监视 · 错误分类
 ├─ DesignSystem：生成的 token + 组件
 ├─ Core：纯逻辑（连续性、轮次折叠、排序分组、审批策略、标签解析、屏幕对话框、格式化）
 ├─ TerminalCore：VT 子集网格 · 折行 · 键编码 · 帧序与接管状态机
 └─ Contracts：线上类型 · 事件 · 能力位 · 错误码 · 窄接口（protocol / interface）
```

- 依赖方向固定：`App → Feature → Contracts ← HubClient / Data`。Feature 想直接用 HubClient 的实现会编译不过。
- **只有 App 能同时看到实现和页面**，在组合根里构造并注入。iOS 是 `AppContainer`，Android 是 Hilt 或手写的组合根。
- `HubScope(hub_id, 配对代次)` 持有这个作用域内的客户端、存储、子模型和所有长任务。重新配对等于丢弃整个 scope。
- Android 是约 18 个 Gradle 模块；iOS 以后跟进时拆成本地 SPM 包里同名的约 16 个 target，一一对应，见第 4 节。比 removed 的 39 个少得多，但足够让每个 Feature 有自己的命名空间。按模块分开后，多个 agent 并行就不会撞名或改到同一个大文件。

### D3. 可靠性：结果三态，重活放 hub

| 写操作类别 | 例子 | 怎么保证 |
| --- | --- | --- |
| **消息** | Agent / 助手对话发消息、飞书回复、收件箱回复、邮件发送、AI 分身草稿发送 | 客户端生成 `client_msg_id`，**先写进本地 outbox 再清空输入框**；hub 把它落进 Postgres 的 `client_ops` 表（id、设备、种类、目标、参数摘要、状态、结果），同 id 同参数直接返回原结果，同 id 不同参数拒绝；没网时排队，连上后按顺序自动发 |
| **一次性操作** | 回答交互、中断、改名、关闭 Agent、改模式、停止循环、`/clear` | 带 `client_op_id`，hub 同样落库去重；**断网时不排队**（电脑上的状态可能已经变了，沿用现有规则）；结果未知时先重读权威状态（交互列表、会话状态）或查 `GET /api/v1/ops/{id}`，卡片上显示「结果待确认」，不报错 |
| **设置类** | 置顶、屏蔽、通知偏好、定时任务 | 整表 PUT、后写的覆盖先写的，乐观更新、失败回滚；不需要操作日志 |

客户端怎么判定结果（两端同一张表，详见 [ux.md](ux.md) 5.2）：

- **请求根本没发出去**（没网、DNS 失败、连接被拒）→ 等网络恢复后自动重发。
- **发出去了但没收到回音**（超时、连接中途断开）→ hub 支持 `client_ops` 时，用同一个 id 自动重试是安全的；旧 hub 上显示「结果待确认」，再看对话记录里有没有出现这条。这一条改掉的就是现在「超时当没网，盲目重发」的问题。
- **hub 回了确定的拒绝**（409 `input_draft` / `dialog_open` / `op_id_reused` 等）→ 显示原因和可选的操作（比如「清空并发送」）。
- **电脑上打字打到一半出错**（502 `delivered: unknown`）→ 进入「待核对」：自动在对话记录里找这条消息，找到就算送达；超过时限还没出现，交给用户选「重发 / 不发了」。

可选（P2）：所有针对某个 Agent 的写操作都带上**目标实例指纹**（机器 + pane + 进程启动标识 + session id），由 node 核对，不一致就返回确定的错误 `stale_session`，App 再问「会话已在别处重开，要发到新进程吗？」。这对应 removed 的进程绑定，但比它宽松：removed 发现进程变了就冻结一切。

### D4. 同步：沿用现有协议，写成规格

- Agents 对话沿用 `history` / `from_seq` / `to_seq` / `covered` 的连续性规则（`docs/protocol.md`「items 的连续性」）。规则写成 `spec/fixtures/continuity/*.json`，iOS 的 `ChatStore.apply`、Android 的 reducer、Go 的仿真共用这一批。
- 订阅按引用计数；补洞有上限（8 页）再换成最新一页；重连后重拉待处理交互；后台同步只拉 rev 变了的会话（`AgentSync`）。这些规则原样保留，并写进规格。
- 飞书会话补上缺口检测：
  - hub 透传上游的 `syncState`、消息的 `clientId` 和 `localState`；
  - App 刷新时从最后一条往后读（`after`），追不上就整屏换成最新一页并提示；
  - 只有上游标明这一页完整时，才删本地的行；
  - 重连后通知已打开的会话页做一次核对。
- App 内部的事件分发用有上限的缓冲，缓冲满了就发一个「需要全量核对」的信号，不默默丢事件（removed 的做法）。

### D5. 能力协商与兼容

- **hub → 客户端**：保留现有的 `features`，在 `/health` 和 WS `hello` 里下发。
- **客户端 → hub**：新增两处。WebSocket 连上后发 `{"type":"client_hello","platform":"android|ios|watch","version","build","features":[…]}`；HTTP 请求带 `X-Jarvis-Client: <platform>/<version>`。hub 用它们做三件事：
  - 推送和手机工具按平台路由；
  - 新的交互类型不发给不认识它的客户端，退回成屏幕卡；
  - 写进日志，方便排查。
- **统一错误体**：`{"error": "给人看的话", "code": "机器码", "retryable": bool, "delivered": "none|unknown|yes"}`。保留现有的 `{error}` 和 409 平铺字段，兼容旧 App。客户端只认 `code`，不再匹配文案，现在 `APIError.isTimeoutText` 靠文案判断超时。
- **宽容解码**：两端规则一致。
  - 未知的枚举值降级成 unknown；
  - 可选的增强字段解码失败，只丢这一个字段；
  - 未知的 WS 事件忽略；
  - 早于 2000 年的时间当空值，Go 的零时间是 `0001-01-01`。

### D6. 本地数据与作用域

| 类别 | iOS | Android | 规则 |
| --- | --- | --- | --- |
| 秘密（设备 token） | Keychain（`WhenUnlockedThisDeviceOnly`） | Keystore 生成的密钥 + Tink 加密（`security-crypto` 已弃用，不用） | 退出配对时清掉 |
| 持久数据（outbox、操作日志、草稿、阅读位置、导航状态） | Application Support | `filesDir`（Room 或原子文件） | 带版本的封装格式：新版能读旧版，旧版拒绝覆盖新版；**读不出来就保留原文件、停止写入并提示，绝不悄悄换成空的** |
| 缓存（列表快照、对话末尾两轮、图片、网页） | Caches | `cacheDir` | 可以随时丢；存储页可清除 |

- 所有路径都放在 `<hub_id>/<配对代次>/` 下。hub 第一次运行时生成实例 UUID，通过 `/health` 和 `hello` 下发。换 hub 或重新配对就是新的作用域，旧的整体清掉，现在重新配对不会清缓存。
- （以后 iOS 跟进时）缓存目录名、UserDefaults 键、DEBUG 启动参数名在 iOS 重构期间**都不改**。要改就换新名字，并兼容读旧格式一个版本，否则升级后第一次打开是空白。

### D7. 设计系统：一份 token，两端生成

- 唯一来源是 `spec/tokens/*.json`，写法贴近 W3C 设计 token 社区的格式。内容包括：
  - 风格 × 深浅色的语义色；
  - 状态色（dot / text / tint）；
  - 圆角、间距、字体角色、动效；
  - **对比度规则**：把 `docs/ios.md` 里的可读性底线变成机器能检查的规则。
- `scripts/assets/gen_tokens.py` 生成以下产物；`--check` 模式重新生成后比对，并检查所有风格 × 深浅色的对比度，接进 pre-commit：
  - iOS：`Tokens.swift`（以后 iOS 跟进时才生成），纯数据类型标 `nonisolated`；
  - Android：`Tokens.kt`；
  - 原型用的 CSS；
  - 文档里的色板表。
- **守住采用率**：用 grep 白名单禁止在 token 文件之外写色值、圆角字面量和固定字号，以现在的计数为基线，只许减少。iOS 现状是 `Theme.Font` 和 `Theme.Space` 在视图里各只用了 1 次，圆角字面量还有 70 处。
- **Android 的风格**：每个平台的默认风格就用它的原生材质。
  - iOS 默认是「液态玻璃」，Android 默认是「原生（Material You 动态取色）」。
  - Claude / 黏土 / Island / 飞书四套是两端共用的 token 组合，Android 第二批再做。
  - 液态玻璃不在 Android 上模仿：要用 AGSL 着色器，低端机会掉帧，而且不是 Android 的设计语言。
- 无障碍规则写进组件规格，两端同一份：
  - 辅助字号下横排改竖排、隐藏装饰图标、放开行数；
  - 打开减少动态效果时动画全停；
  - 计数角标视觉上可以缩写，读屏读完整数字；
  - 最小触控区 iOS 44pt、Android 48dp。

### D8. Android 的通知：只走常驻连接，不依赖 Google 服务

按你定的前提，手机没有 Google Play 服务，FCM 收不到；厂商推送又要开发者账号和实名认证。所以 Android 的通知只有一种送达方式：**常驻连接**（PR #84 已有，[android.md](android.md) 5.8）。

- **原理**：前台服务（`specialUse`）一直连着 hub 的 WebSocket，收到事件就在手机上发通知。手机工具的调用也直接送达，不需要唤醒。
- **hub 改动**：不用改就能用。建议「做完了」改由 hub 判断后经 WS 下发（[hub-changes.md](hub-changes.md) 3.9）；后台连接的心跳间隔按连接协商，省电（[hub-changes.md](hub-changes.md) 1.6）。
- **代价**：
  - 通知栏常驻一条；
  - 耗电**未实测**，A1 要测；
  - 国产系统要手动放开自启动和电池限制，准备清单逐项引导。
- **通知规则**：
  - 通知由 App 自己组装：「允许 / 批准」和提问、屏幕对话框的选项按钮要先解锁（`setAuthenticationRequired`），「拒绝 / 打回」不用；
  - 用 `RemoteInput` 直接在通知里输入拒绝原因或计划意见；
  - 单选提问和屏幕对话框的选项做成按钮；
  - 交互被处理后撤回通知；
  - 前台正在看这个会话时不弹通知。
- **通知偏好**：按设备的通知偏好（`/devices/{id}/prefs`）同样生效。
- **锁屏汇总**：常驻连接的那条通知同时承担锁屏汇总（需要你 / 工作中 / 完成未看）。Android 16+ 在有活动时申请提升为 Live Update。

### D9. 手机经公网直连 hub，优先 IPv6

按你定的前提，手机在外面（蜂窝网络、别人的 Wi-Fi）也要能直接连上家里的 hub，不靠 VPN；VPN 用户自己配，App 不管。依据见 [research/public-direct-ipv6.md](research/public-direct-ipv6.md)（国内 IPv6 现状、地址发现、TLS、加固、保活，带来源）和 [research/hub-network-current.md](research/hub-network-current.md)（hub 现有网络代码，带行号）。

- **为什么可行**：
  - 国内手机流量的 IPv6 已经普及（2025 年底移动网络的流量 70.85% 是 IPv6）。运营商拦的是连进手机的 IPv6，手机主动连出去不受影响。
  - 家宽大多能拿到全局 IPv6，hub 只需要自己一个稳定的地址。
  - 公网 IPv4 很多家宽没有（运营商 NAT），只在确认外面连得进来时才作为候选。
- **门槛在家里**，App 和 hub 只能检测和引导：
  - 光猫 / 路由器的 IPv6 入站防火墙默认开着，很多只有开 / 关两档；
  - 前缀会变，有的地区一天两次以上；
  - 部分地区封 80 / 443 等端口。
- **做法**（[hub-changes.md](hub-changes.md) 第 1 节、[android.md](android.md) 5.1）：
  1. hub 新开一个 TLS 端口，明文端口原样保留给内网和现有 iOS。证书用 hub 自己的密钥签，二维码里带公钥指纹，手机钉扎公钥；有域名时可以换成公共 CA 的证书，照样钉公钥。
  2. hub 自己发现可用的地址：公网 IPv6、DDNS 名字、内网、确认可达的公网 IPv4。鉴权后下发，变化时推送。
  3. 手机按「公网 IPv6 → 域名 → 内网 → 公网 IPv4」错峰并发探测。**证书和 `hub_id` 都对上才算连上**，因为旧前缀可能已经分给了别人。按网络记住赢家。
  4. 加固：公网来源拒绝明文；鉴权失败按 IPv6 /64 前缀限流；二维码改发短时临时 token，不再印永久有效的主 token。
  5. 说清楚为什么连不上：hub 端用 `personad hub doctor`，App 端用连接诊断页。
- **推荐配一个 DDNS 名字**：家宽换前缀时旧连接当场断开。Android 没有 FCM 这样的旁路，人在外面就收不到新地址，只能靠域名找回。没有域名只能算降级模式。
- **后台心跳由 hub 发起**：Android 深睡时客户端的 ping 不触发，后台心跳只能由 hub 发，按连接协商间隔。后台从 4 分钟起按网络自动调整；前台 20 秒加应用层 ping（[hub-changes.md](hub-changes.md) 1.6）。
- **兼容现有 iOS**：
  - 二维码格式不变，新信息放附加参数；
  - 明文端口保留；
  - 不协商心跳的连接保持现在的 20 秒；
  - `hello` 的新字段和 iOS 解码用的字段不撞名（核对过）。
  - 唯一的行为变化：公网来源的明文请求被拒。文档一直要求 hub 只放内网 / VPN，正常用法不受影响；另留一个 `allow_public_plaintext` 开关恢复旧行为。
- **Android 17 的本地网络权限绕不开**：在家里 Wi-Fi 上，连内网地址、连同一个 /64 里 hub 的公网 IPv6 都算本地网络，要 `ACCESS_LOCAL_NETWORK`。升 target 37 时在配对和连接流程里申请，「被权限拦了」和「hub 不可达」分开显示（[android.md](android.md) 5.1）。
- **iOS 以后跟进**：解析新二维码参数，加证书钉扎和多地址探测；后台继续用 APNs，地址变了 hub 可以经 APNs 把新地址送到 iPhone。飞书视频改成先下载再播，因为 AVPlayer 接不上钉扎（[ios-refactor.md](ios-refactor.md) R5）。

---

## 4. 目标架构

### 4.1 模块对照（Android ↔ 以后的 iOS）

| 职责 | iOS（本地 SPM 包 `ios/JarvisKit`） | Android（Gradle） | 内容 |
| --- | --- | --- | --- |
| 契约 | `JarvisContracts` | `:core:contracts`（纯 Kotlin） | 线上类型（Codable / `@Serializable`，显式字段名）、WS 事件、能力位、错误码、窄接口（`SessionsReading`、`MessageSending`、`InteractionResolving`…） |
| 纯逻辑 | `JarvisCore` | `:core:domain`（纯 Kotlin） | 连续性 reducer；items → Rows → Blocks → Turn → Lines 折叠；冻结快照；outbox 状态机；排序 / 分组 / 同伴 / 收件箱选择器；审批策略表；消息标签解析；屏幕对话框解析；格式化；Fleet / 快捷入口文案 |
| 终端核心 | `TerminalCore` | `:core:terminal`（纯 Kotlin） | VT 子集网格、宽字符、折行、滚动检测、键名 → 字节、修饰键状态机、帧序和接管状态机 |
| 网络 | `HubClient` | `:core:hub` | 按领域拆的 HTTP 客户端（会话、交互、机器、文件、收件箱、飞书、邮件、助手、知识库、设备）、WebSocket、连接监视、错误分类 |
| 数据 | `JarvisData` | `:core:data` | 安全存储、通用操作日志（outbox / op / 草稿）、缓存、媒体缓存、同步引擎（会话订阅、AgentSync、LarkSync、InboxSync）、`HubScope` |
| 设计系统 | `DesignSystem` | `:core:designsystem` | 生成的 token、组件（第 5 节） |
| 对话 | `ChatFeature` | `:feature:chat` | 列表引擎、消息行、工作行、交互面板、输入框、搜索、任务进度；Agents 和助手共用 |
| Agents | `AgentsFeature` | `:feature:agents` | 列表、空间页、机器页、新建会话、请求收件箱 |
| 助手 | `AssistantFeature` | `:feature:assistant` | 首页、主对话 / 历史、知识库、资料页 / 定时任务 / 记忆 |
| 收件箱 | `InboxFeature` | `:feature:inbox` | 统一列表、飞书会话页和卡片、邮件 |
| 终端 | `TerminalFeature` | `:feature:terminal` | 终端视图、键栏、手势、设置 |
| 阅读器 | `ReadersFeature` | `:feature:readers` | 链接浮层 + WebView 池、飞书文档、文件查看器、图片查看器和编辑器、全文阅读器 |
| 机器 | `MachinesFeature` | `:feature:machines` | 机器详情、监控 Dashboard、曲线 |
| 设置 | `SettingsFeature` | `:feature:settings` | 配对 / 首次接入、设备、通知、手机数据、存储、风格 |
| 平台 | `PlatformKit` | `:platform:notify`（前台服务、通知、Live Updates）、`:platform:phonetools`、`:platform:shortcuts` | 推送注册和通知动作、实时活动 / Live Updates、手机工具、快捷入口 / 分享目标、触感 |
| 组合根 | App target（`AppContainer`、`Router`、`DebugHooks`） | `:app`（DI、NavHost、`src/debug` 的调试入口） | 唯一知道所有实现的地方 |
| 手表 / 扩展 | Watch 和 Widgets target 只链接 `JarvisContracts` + `JarvisCore` + 所需的 HubClient 和 token | — | 不再把整个 `Shared/` 源码编进去 |

### 4.2 `AppModel` 拆成什么

| 新服务 | 接手的职责（现在 `AppModel` 的第几类） |
| --- | --- |
| `SessionDirectory` | 机器、会话、索引、置顶 / 屏蔽、选择器（4、16、17） |
| `ChatRepository` | `ChatStore` 注册表和 LRU、订阅引用计数、补洞调度、历史分页（8、9、10） |
| `InteractionCenter` | 待处理交互、resolve、**审批策略表**（一张表定义每个入口用什么模式，5 个入口共用）（13） |
| `MessageSender` | **唯一的发送用例**：所有入口（对话、助手首句、交给 Persona、飞书、收件箱、Siri、手表）都走它，统一幂等、等网和拒发处理（14） |
| `TerminalStreams` | 终端流路由、hub 能力位（11） |
| `AssistantService` | 助手配置、主对话、预热、换启动器（7） |
| `PhoneToolHost` | 手机工具注册和执行、确认弹窗（12） |
| `ConnectionMonitor` | WS 生命周期、连接状态、catch-up（2、3） |
| `Router` | tab、打开会话、深链；`AppModel.open`、`InboxView.open`、`OpenSessionIntent` 三份合成一份（5） |
| `Feedback` | toast、触感、错误展示策略（6、22） |
| `PairingService` | 配对、凭据、换设备 token、`HubScope` 切换（1） |

页面模型规范：

- iOS 用 `@MainActor @Observable` 的模型，配合 actor 服务。
- Android 用 `ViewModel` + `StateFlow`，可变共享状态用 `Mutex` 或单线程调度器保护。

两端都遵守：

- await 前记下版本号，回来先比对，不一致就丢掉结果。
- 模型持有并负责取消自己的任务。
- 忙时来的刷新合并成一次补读。
- 多个页面共用一个订阅。
- 事件流有上限，溢出就整体重读。

---

## 5. 交互与界面重设计（摘要）

完整规格见 [ux.md](ux.md)。原则：**信息架构不变，补阅读和拍板的能力；不确定的状态说清楚；同一个东西只有一种画法。**

| 区域 | 主要改动 | 优先级 |
| --- | --- | --- |
| 全局 | 四个 tab 不变。新增「待定状态」组件族（发送中 / 连上后发送 / 结果待确认 / 待核对 / 没有生效），统一文案词表 | P0 |
| Agents 列表 | 行上显示**草稿摘要**和**待核对标记**；「需要你处理」卡片升级成**跨会话请求收件箱**（每项带剩余时间，点开直接是完整交互页）；新增「历史」范围，按项目浏览没在运行的会话：先只读打开，点「继续这个会话」才恢复 | P1 |
| 对话页 | 输入区上方**唯一一条状态行**：左边是确定的状态，右边附实况转圈的文字；「待核对」「正在同步」是正式状态；顶栏只放身份 | P0 |
| 对话页 | 工作行**折叠时也显示失败数**；展开后顶部吸附「收起」条；Edit 的 diff 加「上一处 / 下一处」；工具卡统一的「原始输入 / 原始输出 / 复制」 | P1 |
| 对话页 | **会话内搜索**（正文、思考、工具输入输出、系统事件），命中后高亮，顶部出现「返回搜索前位置」；**任务进度**横条和全屏页；**全文阅读器**（分页、文内搜索、完整复制） | P1 |
| 交互 | 普通工具的权限卡挂在**记录末尾**，复杂请求打开完整页；多个请求进收件箱；**倒计时**；**拒绝原因**，填了就不能点允许；**计划意见**和批准互斥，「批准并自动执行」加风险说明；MCP 服务器来源；**MCP 表单 / 链接**；子 Agent 来源；没提交的输入按请求保存 | P0（拒绝原因、互斥、倒计时）/ P1（其余） |
| 草稿 | 草稿带附件持久化，带版本号，回执清草稿前先比对版本；列表行显示「草稿 · …」 | P1 |
| 飞书会话页 | 换成和对话页同一套倒置列表引擎；阅读锚点；收件箱入口显示「这件事从这里开始」分界线；已读上报加门禁；「回到最新」只数确实在下方的已加载消息，后面没加载完显示「N+」；发送走 outbox，带 clientId，确认只认 id；草稿持久化；缺口检测 | P0（发送幂等、缺口）/ P1（其余） |
| 配对与连接 | 扫码后**分段显示进度**（解析 → 探测地址 → 校验证书和 hub → 换设备 token → 读机器列表），失败按原因分类提示（找不到地址 / 被拒绝 / 超时 / 证书不符 / 本地网络权限 / hub 太旧）；说明配对会在手机和 hub 上留下什么；配对后给一份「准备清单」（各台机器在线情况、版本、通知权限、电池和自启动设置），每项可跳过并写明后果；设置里新增**连接诊断页**（当前路径、各个地址的探测结果、证书指纹、心跳间隔） | P0 |
| 设计系统 | token 生成、组件目录页（两端都有，DEBUG 可直达）、无障碍规则 | P0 |

---

## 6. Android 方案（摘要）

完整方案见 [android.md](android.md)。

- **起点**：PR #84 的代码，按「保留 / 搬家 / 替换」三张清单重组（android.md 1.3），每一步 App 都能照常用。
- **技术栈**：
  - 工具链：Kotlin 2.4 + AGP 9.5 + Gradle 9.8；compileSdk 37；targetSdk 先 36，配对流程做完本地网络权限后升 37。
  - minSdk 31（Android 12）：锁屏通知上的「允许 / 批准」能要求先解锁；按 StatCounter 中国的页面浏览量算覆盖约 91%，包括还能装 APK 的鸿蒙 3 / 4（兼容层就是 API 31）。
  - 界面和架构：Jetpack Compose（BOM 2026.09）+ Material 3；`ViewModel` + `StateFlow`；Hilt；Navigation 3。
  - 网络和存储：OkHttp 5（HTTP + WebSocket，hub 专用的 client 用自定义 TrustManager 钉公钥）+ kotlinx.serialization；Room 2.8 + DataStore；token 用 Tink 加密。
  - 媒体：Coil 3；不引入播放器库，飞书的视频、语音消息显示成卡片，点开交给飞书。Markdown 用 mikepenz 渲染器；扫码用 zxing（沿用 PR #84）。
  - 通知：常驻连接（前台服务），不用 FCM；测试：Roborazzi。
  - 不引入任何依赖 Google Play 服务的库（`com.google.android.gms:*`、Firebase）。
  - 具体版本号和取舍理由见 android.md 第 2 节。
- **难点和对策**：
  - **聊天列表**：`LazyColumn(reverseLayout = true)` 天然就是倒置列表，iOS 上为翻转列表、安全区、懒加载估算高度绕的那些路，大多不用照搬。仍然要做「每步一行」、稳定 key、冻结快照、进入时的定位；长会话要用 `fold_work`。
  - **终端**：把 iOS 自写的 VT 网格、折行、键编码移植成纯 Kotlin，带着真实录制帧夹具一起移植。渲染用 Canvas，输入法自建 `InputConnection`；这是 Android 版最大的一块重写，放第二批，在那之前沿用 PR #84 每秒读屏的终端小窗。
  - **Markdown**：mikepenz 渲染器支持表格，节点可以替换成工具卡、引用卡、路径标签；代码高亮照搬 iOS 的轻量规则。Compose 的 `SelectionContainer` 能跨段落原地选择，iOS 上「选择」要弹纯文本页的限制在 Android 上不存在。
  - **链接浮层**：同一窗口里的自绘浮层，有两个停靠位置（铺满 / 60%），停在 60% 时后面的对话照常可用；WebView 池常驻 2–3 个（Android WebView 更吃内存）。
- **要换形态的**：
  - APNs → 常驻连接（D8）。
  - 灵动岛和锁屏实时活动 → 常驻通知；Android 17 用 MetricStyle 放三个数，Android 16+ 有活动时提升为 Live Update。
  - 手表 → 首版不做，通知会自动桥接到 Wear OS，按钮也能用。
  - Siri → 快速设置磁贴、桌面快捷方式、「分享到 Persona」。
  - HealthKit → Health Connect：它要 Google Play 服务，国行的健康 App 也不往里写数据，国行手机首版不注册健康工具。
  - 提醒事项 → Android 没有系统接口，去掉这 3 个工具，或者在 hub 侧换成 Google Tasks。
  - 剪贴板 → 只能在前台读。
  - Gmail 的 Google 账号登录 → Android 上不做：Google 的登录接口依赖 Google Play 服务。邮箱账号存在 hub 上、两端共用，在 iPhone 上加一次，或者用应用专用密码走普通 IMAP。
- **Android 独有的机会**：
  - 「分享到 Persona」：任何 App 的文字、链接、图片、文件发进主对话，成本低，正合「把各处信息收到一起」的定位。建议首版就做。
  - 平板和折叠屏上列表、对话双栏（对应 iOS 的 iPhone Duo 双栏）。
  - 短信、通话记录、通知监听：很敏感，不做（第 12 节第 10 题）。
- **两个平台限制**：
  - Android 17（target 37）访问本地网络要运行时权限 `ACCESS_LOCAL_NETWORK`。在家里 Wi-Fi 上，连内网地址、连同一个 /64 里 hub 的公网 IPv6 都算本地网络，改连公网 IPv6 也绕不开。没授权时 TCP 表现为**超时**，看起来像「连不上 hub」。所以先停在 target 36，配对和连接流程把权限申请做好再升；经 VPN 时的行为要真机实测。
  - Android 9+ 默认禁止明文 HTTP。network security config 是静态 XML、不支持网段，所以整体放开（PR #84 的做法），再在 App 里检查：`http://` 只用于内网、VPN 网段和回环，**公网地址一律走 TLS 并钉扎公钥**。

---

## 7. iOS 重构路线（这一轮暂不执行）

这一轮 iOS 只读不改。[ios-refactor.md](ios-refactor.md) 的 R0–R5 留作以后 iOS 跟进时的参考：
- 先抽纯逻辑；
- 建本地包；
- 拆 `APIClient` 和同步器；
- 拆 `AppModel`；
- 拆 `ChatView`；
- 落地新交互和清理。

到时候 iOS 直接接上这一轮在 Android 侧写好的夹具和 token。

---

## 8. hub 改动（摘要）

完整清单见 [hub-changes.md](hub-changes.md)。在内网里，Android 的基础功能（Agents、交互、常驻连接的通知）**不改 hub 就能用**，PR #84 就是这样做的。下面的改动支撑公网直连，让两端行为一致、更可靠。**前提：现有 iOS App 不改代码也照常工作**（内网明文连接、旧格式二维码、单地址都保留）。

| 级别 | 改动 |
| --- | --- |
| **必须**（公网直连，D9） | 新开 TLS 端口（hub 自己的密钥签证书、公钥指纹、同一把密钥续签）；地址发现和下发（`/endpoints`、`hello` 新字段、`endpoints` 事件）；二维码改发短时临时 token（格式不变）；公网来源拒绝明文、鉴权失败按 /64 限流；`hub_id`；心跳按连接协商；`personad hub doctor`；DDNS（可选，推荐）；新文档 `docs/network.md` |
| **必须**（Android 首版） | 手机工具按设备注册和路由：现在 `register_phone` 整表覆盖，调用发给最后注册的那台，iPhone 和 Android 同时在线时互相覆盖；Android 安装包由 hub 托管，App 内检查更新；记录每台设备的连接和断开时段（后台自检和过夜实测要用） |
| **建议**（和 Android 首版一起做） | 「做完了」等通知由 hub 判断后经 WS 下发给常驻连接的设备（`notify` 事件），手机不再自己算；应用层 ping / pong（判断弱网）；`client_hello` 和 `X-Jarvis-Client`；统一错误体 |
| **建议**（可靠性，P3 和两端的 outbox 一起做） | `client_ops` 表 + `GET /api/v1/ops/{id}`，消息和一次性操作持久去重；飞书发送透传 clientId，消息带 `clientId` / `localState`，分页带 `syncState` |
| **可选**（其他） | 文案里写死的「iPhone」改成「手机」；iOS 上交互处理后撤回已弹出的通知；`docs/protocol.md` 补齐漏写的接口和事件；把客户端里的提示词模板挪到 hub |

---

## 9. 质量与验收

- **测试分层**（两端对应）：

  | 层 | iOS | Android | 共享的东西 |
  | --- | --- | --- | --- |
  | 纯逻辑 | Swift Testing，跑在 SPM 包里，不启动 App | JUnit，跑在 JVM 上 | `spec/fixtures` |
  | 协议 | 解码 Go 导出的样本 | 同左 | `spec/fixtures/wire`，由 Go 测试生成 |
  | 页面模型 | 用 continuation 做闸门的假实现，确定性地制造迟到、取消、切作用域 | coroutines-test + `CompletableDeferred` 闸门 | 场景清单 |
  | 组件快照 | 组件目录 × 深浅色 × 字号 × 窄屏 | Roborazzi（JVM 上渲染） | 编造的数据（隐私） |
  | 流程 UI | XCUITest + `scripts/mock/mock_hub.py` | Compose UI 测试；仪器测试用 `adb reverse` 连同一个 mock hub | mock hub 和它的故障开关；在 Go 测试里校验 mock 的响应格式，防止和真实 hub 漂移；新增「已执行但丢掉回执」开关 |
  | 真实链路 | 本机测试 hub（假 HOME、独立数据库） | 同左 | `docs/testing.md` 的流程 |

- **验收分级**：每个功能在 [parity.md](parity.md) 里有 iOS 和 Android 各一列（这一轮只填 Android 列，iOS 列是现状），按 已开发 → 单测通过 → mock 截图通过（深浅色 × 字号）→ 测试 hub 通过 → 真机通过 标级。每页核查清单沿用 removed 那份，再加三项：两端一致、按钮不换行、链接在 App 内打开。
- **截图**：两端都用调试入口直达页面再截图。iOS 用启动参数，Android 用 `adb shell am start` 带 extras，代码只在 debug 源集。Release 包检查不能含调试开关字符串：iOS 用 `strings`，Android 扫 APK。
- **性能**：凡是写进文档的数字都要实测，至少 10 个样本，给 min / p50 / p90 / max / mean。首批基线：
  - 冷启动打开大会话的耗时；
  - 长对话滚动的掉帧；
  - 推送从 hub 发出到手机弹出的延迟；
  - 唤醒执行手机工具的端到端耗时。
- **隐私**：`scripts/privacy_check.py` 覆盖 `android/` 和 `spec/`；夹具一律用脱敏脚本生成（白名单式），提交前人工过一遍。
- **新交互先做 HTML 原型**：放在 `prototypes/<主题>/`，用生成的 token CSS，手机框做 iPhone 393 和 Android 约 360dp 两种，覆盖深浅色、窄屏、减少动态效果和异常场景。你认可后再写两端原生。第一批建议做原型的：状态行、交互完整页和请求收件箱、会话内搜索、配对进度。

---

## 10. 里程碑与并行

| 阶段 | 内容 | 完成的标准 |
| --- | --- | --- |
| **P0 定规格**（先做） | 读 iOS 写出 `spec/fixtures` 和 token JSON；hub：公网直连（TLS、多地址、短时临时 token 的二维码、限流、心跳协商、`hub doctor`）、`client_hello`、错误体、`hub_id`、应用层 ping；**Android A0**：在 PR #84 上升级工具链（target 先 36），拆出纯 Kotlin 核心模块，把 PR 的连续性、模型、行合并搬进去并接上夹具；第一批 HTML 原型 | 夹具在 Android 上全绿；PR #84 原有的真机流程（配对、发消息、通知按钮作答）照常；**现有 iOS App 不改代码连新 hub 照常工作**（跑一遍 iOS 的 UI 测试和真机配对） |
| **A1 Android 最小可用切片** | Hilt、Navigation 3、加密存储、Room；公网直连（多地址探测、IPv6、证书钉扎）；对话页换成共用的列表引擎 + `chatLines` + `fold_work` + 状态行 + 新交互面板；通知的「允许 / 批准」要先解锁、拒绝可以带原因；状态色和 iOS 一致；debug 直达入口 | 650 条命令的长会话打开和滚动有实测数字（至少 10 个样本）；常驻连接在主流国产系统上的每小时耗电和锁屏存活有实测数字；三家运营商蜂窝网络的空闲超时有实测数字，后台心跳间隔据此定；手机走蜂窝网络经公网 IPv6 连上 hub；真机上走通「通知 → 批准 → Agent 继续」 |
| **A2 Android 首版补齐** | 四个 tab（Persona、Agents 完整版、收件箱、设置）、连接状态、配对的分段进度、离线 outbox、分享到 Persona、国产系统准备清单；hub 的 `notify` 事件和手机工具按设备路由 | Android 首版的 P0 在 parity.md 里至少到「测试 hub 通过」；可以发给自己和测试的人日常用 |
| **A3 Android 新功能和第二批** | 第 5 节的交互改进；hub 可靠性改造（`client_ops`、飞书幂等）；终端、Live Updates、手机工具、知识库、文件 / Git、链接浮层、飞书文档和卡片、机器监控、其余风格、体验模式 | 按 parity.md 逐项 |
| （以后）iOS 跟进 | [ios-refactor.md](ios-refactor.md) | 这一轮不做 |

并行规则（沿用 CLAUDE.md 的经验）：

- 按模块分车道，每个车道一个独立的 worktree，并用自己的模拟器；Android 每个车道一台单独的 AVD。
- 车道之间只通过 `spec/` 和 `Contracts` 交接。改 `spec/` 的车道先合入，别人再 rebase。
- Android 的 A0 动的是 PR #84 的全部文件，期间别的车道不碰 `android/`。
- **谁都不改 `ios/`**；派 subagent 时在 prompt 里写明。
- push 前看 `git diff origin/master --stat`，确认只含自己的文件。

---

## 11. 风险

| 风险 | 影响 | 对策 |
| --- | --- | --- |
| 常驻连接的耗电和存活（国产系统后台限制严，锁屏后长时间连接 PR #84 还没在真机上测） | 通知不及时，或者耗电明显 | A1 在主流国产系统的真机上实测每小时耗电和锁屏存活；后台心跳由 hub 发、间隔按网络协商；准备清单逐项引导放开自启动和电池限制 |
| Android 17 本地网络权限 | 升 target 37 后不处理的话，在家里 Wi-Fi 下连 hub（内网地址或同一 /64 的公网 IPv6）会一直「连接中」 | 先停在 target 36；配对和连接流程做完权限申请再升；被拒时状态显示「需要本地网络权限」并给出入口；经 VPN 的情况要真机实测 |
| 光猫 / 路由器的 IPv6 入站防火墙关不掉，或只有整体开关 | 公网直连做不成；或者为了放行把整个家庭网络都暴露了 | 引导改桥接、在自己的路由器上只放行 hub 的地址和端口；hub 机器的防火墙兜底；`hub doctor` 自检 |
| 家宽前缀一天变几次，又没有域名 | 人在外面时手机找不回 hub | 推荐 DDNS；手机按「hub 推过的新地址 → 域名 → 内网」找回；诊断页提示 |
| 运营商封端口，各地策略不同 | 有的用户端口不可达 | 端口可配，避开 80 / 443 / 8080 / 8443；`hub doctor` 给出明确诊断 |
| 4G / 5G 的运营商 NAT 和防火墙空闲超时比 4 分钟还短（国内只有约 2015 年 2G / 3G 的数据） | 后台连接频繁断开，重连耗电 | 心跳按网络自适应下调；A1 实测三家运营商 |
| hub 机器开着 TUN 代理 | 从外面连进来的 IPv6 回包被路由进代理，公网入站静默失败 | `hub doctor` 检测；`docs/network.md` 写排除规则 |
| hub 的身份密钥丢了 | 所有设备要重新扫码配对 | 密钥文件写进备份说明；`hello` 里可以预告下一把密钥的指纹 |
| 华为鸿蒙 5 及以后不能装 APK | 这部分华为用户用不了 | 鸿蒙 4.x 及更早（兼容层 API 31）能用；鸿蒙 5+ 的兼容容器（卓易通）能不能常驻收通知要真机测，不承诺 |
| 国产系统默认限制后台和通知，又没有接口能查 | 用户没设对，就是「锁屏一会儿就收不到」 | 准备清单按系统、按版本逐项引导；`startForeground` 失败时等下一个豁免，关电池优化本身就是豁免；锁屏测试通知 + hub 记录的断开时段自检 |
| 华为鸿蒙 4 的本地通知默认按「资讯营销」处理：静默，每天 2–5 条 | 「需要你」的提醒不响、不亮屏 | 引导用户关「静默通知」、开横幅和锁屏；「需要你」同时显示在常驻通知的计数上；真机测常驻通知算不算进配额 |
| 进程活着但收不到（荣耀「智能省流量」、华为「休眠时始终保持网络连接」、厂商冻结） | 通知不来，一打开 App 又好了，容易误判成 hub 的问题 | 准备清单点名这几项；诊断页显示断开记录；锁屏 8 小时的真机实测，至少 10 晚 |
| Android 和 iOS 的行为不一致 | 同一个对话在两端折叠得不一样 | 夹具照着 iOS 的实现和测试写（只读），并在模拟器上用同一个会话对照截图；以后 iOS 跟进时接上同一批夹具 |
| hub 改动打断现有 iOS App | iPhone 连不上或配不了对 | 所有改动向后兼容：明文端口留给内网和 VPN、`hub pair --legacy` 出旧格式二维码、新字段可选；每次 hub 改动都用不改代码的 iOS App 跑一遍 UI 测试和真机配对 |
| Compose 下长对话的性能 | 几百步的工作过程卡顿 | A1 就验证，并实测掉帧；每步一行、稳定 key、`contentType`、派生数据按 id 缓存 |
| 终端在 Android 上的输入法和手势 | 拼音、修饰键、选择的体验不如 iOS | 放第二批；先把 VT 核心和录制帧测试移植好；输入法单独做原型 |
| Live Updates 只在 Android 16+ 有完整效果，厂商可能加限制 | 锁屏汇总不如灵动岛 | 降级成普通常驻通知；「需要你」走高优先级通知，不依赖它 |
| Android 上不能用 Google 账号登录 Gmail（登录接口依赖 Google Play 服务） | 只用 Android 的人加不了 Gmail 的 Google 登录 | 邮箱账号存在 hub 上、两端共用：在 iPhone 上加一次，或者用应用专用密码走普通 IMAP |
| 推理网关不稳定（这次调研中途 8 个 agent 同时 503） | 多 agent 并行时中断 | 车道任务写成能断点续做的；笔记和产出边做边落盘 |

---

## 12. 待决问题的结论

2026-10-30 你定：全部按建议来；DDNS 域名和测试用的真机，你另外告诉我。

| # | 问题 | 结论 |
| --- | --- | --- |
| 1 | hub 有没有可以做 DDNS 的域名（Cloudflare、阿里云 DNS 或 DNSPod） | 按建议：有就配上，hub 自动更新记录，证书用通配名，免得具体的名字进证书透明日志被人扫；没有就先靠「hub 推送的新地址 + 内网兜底」，诊断页上提示。**等你补充有没有域名** |
| 2 | 测试用的真机 | 按建议的最小矩阵：小米（已有，澎湃 OS 3 / Android 16）、华为鸿蒙 4.x、OPPO ColorOS 16 或 17；vivo、荣耀先用厂商的云真机；再备三张不同运营商的卡。**等你补充手头的机型** |
| 3 | iPhone 和 Android 会不会同时用 | 按「会同时用」设计：手机工具按设备路由，「做完了」两边都发 |
| 4 | 分发方式 | APK 自己签名，放在 hub 上，App 内检查更新、弹系统确认安装；签名证书放配置里，不进仓库（[android.md](android.md) 第 9 节） |
| 5 | 怎么「覆盖」PR #84 | 在它的代码上按三张清单重组（android.md 1.3），不推倒重写；A0 期间 `android/` 只由一个车道改；和 PR #84 作者的协调由你来 |
| 6 | Android 默认风格 | 原生（Material You 动态取色），状态色改成和 iOS 一致；Claude / 黏土 / Island / 飞书四套第二批 |
| 7 | Android 首版范围 | iOS 的 76 个 P0 + 分享到 Persona + 公网直连、国产系统适配、App 内更新；终端、Live Updates、手机工具放第二批 |
| 8 | hub 改动 | 公网直连（hub-changes.md 第 1 节）放 P0；「做完了经 WS 下发」「手机工具按设备路由」「设备连接记录」随 Android 首版做；`client_ops` 和 Android 的 outbox 一起做 |
| 9 | 公网直连的细节 | TLS 用单独的端口（同端口识别 TLS 留作以后的选项）；PR #84 版本配对过、没有指纹的手机，在内网上经已鉴权的通道取一次指纹并钉住；hub 换身份密钥时所有设备重新扫码，`hello` 预告下一把密钥只用于计划内的轮换；开了公网直连后主 token 只收回环和内网来源；免鉴权的 `/health` 对公网来源只回 `ok`、`hub_id`、TLS 端口和指纹，内网照旧 |
| 10 | Android 独有能力 | 短信、通话记录、读取其他 App 的通知都不做；以后要做也是单独开关、单独确认 |
| 11 | 手表 | Android 首版不做 Wear OS 应用，通知自动桥接到手表 |
