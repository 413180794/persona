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
| Persona | 首页、主对话、历史抽屉和共用对话页；主对话首句可先进入持久 outbox，恢复连接后解析目标；助手身份确定后保留在 Persona tab |
| Agents | 页面最上面是搜索框，下面一排电脑芯片（全部机器 + 每台电脑，带在线状态和计数，可横滑），再下面是快速筛选；默认按电脑 → 空间（herdr workspace）→ 会话三层树，靠缩进、字号字重和细线分层，不给每层垫底板（`docs/android-design.md` 2.4 节），电脑和空间都能折叠、栏头带计数；全是已看会话的空闲空间留在所属电脑下面、排在有动静的空间后面，默认收起、样式弱一点，收起时标出会话数；也可按状态分组；置顶 / 屏蔽、请求卡和作答；会话重命名（标签名走 herdr，会话名走 Claude 的 /rename，入口在会话行菜单、空间页和对话页菜单）和空间重命名（空间栏头菜单）发出后列表、空间页、对话页标题立即更新，失败回滚；置顶、屏蔽、重命名、关闭由 App 的 `AgentsWiringModule` 接到 hub |
| 对话 | 连续性补洞、历史分页、实时 delta、折叠工作过程、Markdown、diff、待办、图片缓存、阅读锚点、待发气泡及人工核对；输入框见「输入框」，会话控制、来源和能力见「会话控制、会话来源和能力」 |
| 收件箱 | 飞书和邮件事项统一列表、筛选持久化、AI 操作、回复日志；飞书有窗口缓存、事件刷新、可见已读和基础消息渲染；邮件正文页已有，但 MailTransport 尚未接到 App |
| 设置 | 配对、准备清单、后台引导、关于和更新；连接诊断里可以手动添加 / 编辑 / 删除 hub 地址、停用候选、拖动排序（列表顺序就是尝试顺序）、逐条测试（见「连接地址」）；设备管理与通知偏好页已有，但 DeviceSettings 尚未注入 |
| 扩展模块 | 终端、文件、机器监控、知识库、手机工具和体验模式已接入 App，入口见「外壳」一节；体验模式没有示例数据的页面显示说明，不连真实 hub |
| 界面 | 三套界面风格：玻璃（默认）、晨报、看板，在设置 › 界面风格切换，立刻生效并存在本机；深浅色跟随系统。设计规范、组件和写页面的规矩见 [Android 界面风格与设计系统](android-design.md) |

## 通知（不用推送服务）

Android 使用 `:platform:notify` 的前台服务共用 hub WebSocket，不依赖 FCM 或厂商推送。通知渠道、开机恢复、网络重连、保活调度和国产系统准备清单已有实现。

真机记录（2026-10-31，ColorOS 16 / Android 16，单次观察）：前台服务在跑，App 切到后台约 6 秒 WebSocket 仍被断开，回前台自动重连；要在系统设置里给 App 打开「允许后台运行」「自启动」并把电池设为不优化（准备清单「后台保活」会引导）。放开后后台能连多久、耗电多少按验收清单 V03 实测，现在未实测。

- 「做完了」由 hub 按与 iOS 相同的完成策略决定，经 `notify` 事件下发；客户端声明 `ws_notify` 后接收，不再自行推算完成。
- 提问、计划、授权和屏幕选项可在通知作答；允许或批准要求解锁，拒绝或打回可输入原因。结果不明时提示待确认，不自动重复批准。
- 点击通知和会话深链走同一个导航入口（冷启动和 App 已打开时相同）。通知链接写明 `tab` 时直接按它落到 Persona 或 Agents，不等会话元数据；没写时等元数据到达再分流。终端兜底动作（`target=terminal`）打开该会话的终端，返回落在原对话。
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

## 输入框（附件、拍照、听写、「/」命令、排队条、被拒消息）

2026-11-02 从主线旧 App 移植（盘点 C1–C4、D1、D7）。对话页和 Persona 的输入框都是 `feature/chat` 的 `ChatComposer`，下面的行为两处一样（排队条只在 Agents 的对话里）。

- 照片和文件：「+ › 照片」最多 4 张，选中立刻上传到那台电脑（`POST /api/v1/machines/{mid}/files`），长边缩到 1600、JPEG 质量 75（和 iOS 一样）。照片在输入框上方排成一行 64dp 方形缩略图（设计系统 `AttachmentThumb`，在后台按缩略图尺寸解码），右上角 ✕「移除这张」；上传中压暗加转圈，失败描红边、标「重试」，点一下重传，原因在缩略图下面写一行；读不出来、或「撤回编辑」恢复的只在电脑上的照片显示占位图标。文件仍一行一个（名字、状态、重试、✕）。选文件时按 `OpenableColumns.SIZE` 预检，超过 200 MB 直接标成失效「「名字」超过 200 MB，传不了」，不拷贝也不上传；读不到大小的照旧边拷边数。只有照片、没有字也没有文件时，发出去的正文是「请看图片」，照片照旧写成 `<attached_images>` 标签。从相机、文件选择器回来时连接常还在补拉，上传先等 `readyToSend` 最多 15 秒（`UPLOAD_READY_WAIT_MILLIS`，配置值，未实测），再报「连接恢复后重试上传」。离线时照片不随消息排队（已拍板不做）。
- 拍照：App 为扫码配对声明了 CAMERA，安卓规定这时要先授权才能拉起别的相机 App。「+ › 拍照」先查授权，没有就 `RequestPermission`；拒绝（含「不再询问」）弹「需要相机权限」，说明可以到系统设置 › 应用 › {App 名} › 权限里打开，「去设置」打开本 App 的系统设置详情页。没有相机 App 时提示「这台手机上没有能拍照的应用」。拍的原图在 `cache/chat-captures`（FileProvider `${applicationId}.chat.files`），只在它离开所属草稿时删除（✕ 移除，或发出后输入框清空）：输入框按附件 id 盯着自己草稿的变化删，从不扫目录（别的会话的草稿可能还引用同一目录里的文件）。代码 `ComposerCamera.kt`。
- 听写：没东西可发时，发送键的位置是麦克风（「听写」）；Agent 工作中且没内容时麦克风在「停止」左边；有内容时是发送。点了打开手机自带的语音输入（`RecognizerIntent.ACTION_RECOGNIZE_SPEECH`，`LANGUAGE_MODEL_FREE_FORM`、`zh-CN`、提示「说吧，说完会放进输入框」），哪家的识别都行，不依赖 Google 服务，App 不录音、不要麦克风权限。没有识别器时不显示按钮（`feature/chat` 的 manifest 声明了 `<queries>`，安卓 11+ 不声明查不到），启动失败提示「这台手机上没有语音输入」。听到的字接在草稿末尾、光标放末尾，不按选区插入（`withDictation`：先去掉草稿尾部空白；草稿末尾是 ASCII 字母数字、且听到的第一个字也是 ASCII 时中间加一个空格，中文直接接上）。代码 `Dictation.kt`。
- 「/」快捷命令：命令表 `SlashCommand`（和 iOS 同一组：继续 `/继续`、`/compact`、`/context`、提交并推送 `/提交`、汇报进展 `/汇报`）加「清空上下文 /clear」。「+」浮层一点即发。输入框里只有一个「/词」（以 `/` 开头、没有空格和换行）时，字段正上方按前缀（不分大小写）列出命令（设计系统 `ComposerSuggestions`：词加粗、右边说明，/clear 用危险色）；点一条把它的文字放进输入框（不发、光标在末尾），草稿正好是唯一匹配的词时列表收起；点 /clear 清空输入框并弹确认。/clear 只给终端引擎窗格里的 Claude（正向判断 `canTypeClaudeCommands`：Claude、能发送、有终端、有窗格、不是 Persona），而且没在工作、没在清时才出现（列表和「+」都是）。确认框：「{Agent} 会开一个新会话，这段对话留在历史里；新会话出现后页面会跳过去。手机里没发出的输入会保留。」，「清空」是危险按钮。发出后最多等 20 秒（`CLEAR_WAIT_MILLIS`），同一窗格出现另一个在跑的会话就打开它；超时提示「已发送 /clear，新会话出现后在列表里打开」并重新给出 /clear（这条 /clear 还没发出去或被拒时不提示，气泡上自己写着）；请求失败立即停止等待。
- 排队条：工作时发的、还没被取走的消息（会话的 `queued`）显示在输入框正上方（iOS `QueueStrip`），只在输入框显示时出现，只在 Agents 的对话里。标题「排队中 N 条，{Agent} 忙完会依次处理」，每条带编号、最多两行，条数多时限高可滚（设计系统 `QueueStrip`）。点一条弹 `JActionSheet`（标题是这条消息）：「撤回编辑」「拷贝」「删除」（危险色）。撤回和删除都走 `ChatActions.editQueue`（`POST …/queue {keep: 除这一条外的全部序号}`；node 把排队的全拉回、再按顺序放回要留的，回原队列；空列表也照发）。撤回编辑取回包里对应序号的原文放回输入框（草稿空就是它，否则换行接在后面，带的照片恢复成附件），提示「已撤回到输入框，改好再发送」；删除「已从队列删除」；拷贝「已拷贝」；失败「没改成：原因」。改的时候转圈、整条不可点，直到会话刷新完；菜单开着时队列变了就关掉。只有能发送的会话（`session.canSend`）可操作。代码 `ChatQueueStrip.kt`。
- 被拒的待发消息：被电脑拒绝（`input_draft`、`dialog_open`…）或人工确认没送达的消息什么都没打进电脑，气泡下的按钮行（`OutboxStatus`，会话状态浮层也用它）多一个「删除」（`DeliveryAction.Delete` → `sender.cancel`，`OutboxMessageSender.cancel` 现在也接受 Refused）；「编辑」放回输入框并删掉原气泡。结果不明的仍要先核对，不能直接删。
- 只读会话：`ChatComposer(enabled = false)` 时「+」和发送不可用，不显示麦克风和「/」列表，占位文字用传入的 `placeholder`；`canInterrupt = false` 时工作中也不给「停止」（见下一节）。
- 组件目录（调试版）有「附件」「输入框建议」「排队条」三节和一个「听写 + 停止」的输入框样例。单测：`SlashCommandsTest`、`DictationTest`、`ChatComposerStateTest`、`ComposerCameraTest`、`ChatDeliveryStateTest`、`ChatQueueStripTest`，`core:designsystem` 的 `ComposerPartsTest`；`core:data` 的 `OutboxMessageSenderTest`（被拒的能删、发出的不能）。
- 截图：`MOCK_QUEUE=1`（工作中的会话 `macbook/a1f3c2d4-…0001` 带两条排队，`POST …/queue` 按 keep 改，`GET /mock/queue_edits` 看记录）、`MOCK_SEND_REFUSE=input_draft`（被拒气泡）、`MOCK_UPLOAD_KBPS=8`（上传中）、`MOCK_UPLOAD_OLD_NODE=1`（上传失败）。草稿按配对存、跨 `am force-stop` 保留：在一个会话里打一次字或加一次附件，再按风格 × 深浅色冷启动轮着截。

## 会话控制、会话来源和能力

2026-11-02 从主线旧 App 移植（盘点 C5–C7、C9、D2–D5）。规则和 iOS 一致；逻辑在 `core:domain` 的 `sort/SessionSource.kt`（能力、来源文案、恢复命令）、`sort/AgentClose.kt`（关闭确认）、`answer/RequestPresentation.kt`（「总是允许」）。

- 会话来源与能力（`docs/protocol.md`「Codex 来源与能力」）：`Session` 带 `source_kind`（`codex_app_server` / `codex_history` / `identity_conflict` / `runtime`）、`source_thread_id`、`read_only_reason`、`capabilities`（read / send / interrupt / approve / terminal / attach）、`attached`、`agent_label`。没有 `source_kind` 的旧会话什么都能做；有来源的只认明确为 true 的能力（缺字段即拒绝）；`canAttach` = 有来源、`attach` 为 true、还没 `attached`。会话元数据还没到时手机不拦（hub / node 照样会拒），只读和能力判断都按已知的元数据。
  - 对话页：不能发送时输入框禁用、占位「此会话仅可查看」，上方一行写来源（`JTag`）和原因（node 写的「仅查看：」前缀不会叠两遍）；网页卡片的「作为上下文 / AI 修改」也不收。没有终端能力时不给终端按钮和入口，不能中断时不给「停止」和菜单「中断」，不能审批时交互面板照常显示请求、但一个按钮都不能按（「交给电脑处理」也不行），说明写「实时接入以后才能在手机上处理这个请求…」（能实时接入时）或「这个会话不允许在手机上审批，请到电脑上处理」。顶栏副标题先写来源（「Codex 实时 / 服务器 / 目录」）。
  - ⋯ 菜单：「实时接入」（`canAttach`，`ChatAction.Attach` → `POST …/attach`，结果不明时按会话列表的 `attached` 核对；接入后 node 允许它审批），「重命名」「关闭 Agent」要有终端，「复制恢复命令」只给有终端的会话（Codex 用 `codex resume <source_thread_id>`）。
  - Agents 列表：有来源的行在事实里写来源（「Codex 实时 / codex-desk」），只读时再加一个「仅查看」；App 不认识的 Agent 显示 hub 给的 `agent_label`（「DSH」），不再一律叫 Claude。长按菜单多「实时接入」（`AgentOperations.attachSession`），重命名、关闭、恢复命令要有终端，「停止循环」要能发送。请求卡、请求详情和 `AgentsViewModel.answer` 不替不能审批的会话作答，请求详情没有终端时不给「打开终端」。来源会话在「历史」里打开时不给「继续这个会话」，写明只读原因。
  - 通知：通知上的允许 / 批准先查这条请求所属会话（冷启动时先刷新一次会话列表），会话不能审批就不回传，提示「不能在手机上审批」（`NotificationActionResult.ReadOnly`）；查不到会话时交给 hub 判断。
- Agent 运行时（DSH）：`Session.agentLabel` 按 iOS：Codex 来源 → Codex、`traex` → TRAEX、空或 `claude` → Claude，其余用 `agent_label`（没有就用 `agent` 原文）；`isClaude` 是正向判断，Claude 专属的控件（模式、`/model`、`/clear`）只给它。`Interaction` 带 `agent` / `agent_label` / `always_label` / `reason`：别的 Agent 的卡标题写它的名字（「Codex 请求使用 Bash」「DSH 有问题等你回答」），授权卡先写它自己说的理由。「本会话总是允许」只给 Claude 的卡；别的 Agent 的卡只有带 `always_label` 才给「总是同意」，按钮下面写「总是同意会放行：…」（`ApprovalPolicy` 的 `always_offered` 要求，共享夹具 `spec/fixtures/approval/unified.json`、`answer/unified.json`）。
- 权限模式和思考强度（`ChatModes.kt`）：输入框左边的「模式」按钮给所有能发送、在运行的 Claude 会话，Persona 的对话也有（助手进程不在时从下一条消息起生效），Codex / TRAEX / runtime 不显示。浮层「文件权限」四项（手动确认 / 自动改文件 / 只做计划 / 自动，各带一行说明）；在跑的会话不能选「全部放行」，但当前就是它（或这个版本不认识的模式，显示原值）时照样列出来并选中，「模式」按钮用危险色。「思考强度」五项（低 / 中 / 高 / 很高 / 最高）。改的是 `POST …/modes`。
- 模型（`/model`）：终端引擎窗格里的 Claude（不是 Persona）在同一个浮层里多一节「模型」：默认（账号默认）/ Opus（最强）/ Sonnet（均衡）/ Haiku（最快），当前项按会话的 `model` 名字里含 opus / sonnet / haiku 判断（网关模型、没有模型算「默认」）。选了别的就单独发一条 `/model <别名>`（和快捷命令同一条路，输入框里的内容留着）。对话已有缓存时 Claude 会先问「Switch model?」，那是电脑上的对话框，手机上变成一张待处理卡片。
- 会话信息（`ChatSessionInfoSheet.kt`）：⋯「会话信息」（`GET …/info`）：标题、「状态 / 机器 / 空间」；运行（模型、启动器、Claude Code / Codex 的版本、权限、思考强度）、用量（上下文 N tokens、压缩 N 次（只在 > 0 时）、对话 N 轮、共生成 N tokens、开始时间）、位置（目录、分支，点一下拷贝）、拷贝（session id、恢复命令，只给有终端的会话）。tokens 写法 950 / 12.3k / 1.20M。读不到时说原因、可重试。点标题仍是新版的「会话状态」浮层（送达、排队、循环、后台任务、恢复），两个入口分工：状态看「现在怎样」，信息看「它是什么、跑在哪」。
- 关闭 Agent：长按菜单、列表行左滑（只对在运行、有终端、不是 Persona 的会话；行弹回原位再弹确认；`SwipeAction(destructive = true)` 画危险色，收件箱的不变）和对话页 ⋯（不给 Persona 的对话）用同一份确认：「关闭这个 Agent？」+「「标题」/ 机器 / 空间 › 标签」+ 会退出哪台机器上的哪个 Agent、关掉哪个标签页、正在做的事会中断、对话记录还在「历史」里。确认时再查一次，别处已经关了就直接收起。
- 对话页标题位的连接状态：连不上 hub 时，顶栏标题下那一行换成「等待网络 / 连接中… / 更新中…」（连接中和更新中标题旁转圈），恢复后立刻换回会话状态；1.5 秒的延迟和 8 秒的补拉上限在 `OkHttpHubConnection` / `ConnectionRuntime` 里，和其他页的标题一致。
- 交互面板细节：提问的选项带编号，问题的 header 和「可多选」显示成标签；终端对话框的 header 显示在问题上方、选项带终端的键，转回对话的那个选项（hub 标成 `kind: chat`，比如「Chat about this」）变成「改在对话里说」按钮（选它就是把这个选项发给终端）。
- 假 hub：`MOCK_SOURCES=1` 多三个会话：「整理部署脚本」（Codex 实时，能发送、能实时接入，没接入前不能审批，带一张有 `always_label` 的 Codex 授权卡）、「迁移日志采集」（Codex 历史，只读，带 `read_only_reason`）、「生成季度报表」（DSH runtime，`agent_label: DSH`，没有终端，带一张没有 `always_label` 的授权卡）；`POST …/attach` 把 `attached` 置真并放开审批，对不能接入的回 403 `codex_read_only`。`/info` 对 Codex 会话回 Codex 的模型和版本。`MOCK_SCREEN_CHAT=1` 多一个带 header（「计划模式」）和转回对话选项的终端对话框（「照片同步脚本支持 HEIC」），看交互面板的标签、编号和「改在对话里说」。想看「全部放行」时的模式按钮，可以直接 `POST /api/v1/sessions/{mid}/{sid}/modes {"permission_mode":"bypassPermissions"}` 给假 hub。
- 单测：`core:domain` 的 `SessionSourceTest`（旧 `SessionSourcesTest` 的用例 + runtime、恢复命令）、`AgentCloseTest`、`AlwaysOfferTest` 和 answer / approval 夹具；`core:contracts` 的 `ModelsTest`（新字段、`agentLabel`、`SessionInfo`）；`core:hub` 的 `HttpChatActionsTest`（attach、queue、info）、`HttpAgentOperationsTest`（attach）；`feature:chat` 的 `SessionModesTest`（旧 `SessionModesTest`、`ModelPickerTest`）、`ChatSessionInfoTest`（旧 `SessionInfoTest`）、`InteractionOptionsTest`；`feature:agents` 的 `AgentsListInfoTest`；`platform:notify` 的 `NotificationExecutionTest`（不能审批的会话不回传）。

## 多 hub（同时连接多组 hub）

#295，方案和各路的接口约定见 [plan-hub-mesh.md](plan-hub-mesh.md) 第 4 节和 2.6 节，iOS 是 #256。手机可以配对多台 hub：
互相连通（hub 互联）的几台算一**组**，同组只保持一条连接、组内换线路；不同组（工作 / 生活这类要隔离的环境）各一条连接，互不影响。
Agents、机器、要你处理的请求把各组合在一起显示；Persona、收件箱和 hub 相关的设置是每组自己的，用「选中的组」。

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
| `InboxDataOwner`（收件箱、Persona 会话列表）、设备设置、检查更新、锁屏测试、分享、手机数据工具、知识库 | 唯一的 hub | 选中的组 |
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
- 后缀**不跟选中组走**：切换 Persona / 收件箱用的组，任何键都不变，打开着的页面、返回栈、通知深链、草稿、置顶、缓存都不错位。主组断开后别的组保持原样，不提拔新主组；全部断开后，下一次配对的组重新成为主组。
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
- 设置 › hub（`HubsScreen`）：每组一节，列出组的状态和每台 hub（已配对的、经互联发现的、正在使用哪台）；组的菜单里「用于 Persona 和收件箱」（选中组）、改名（只在这台手机上）、断开这一组；同组有两台以上已配对的 hub 时可以单独断开一台；「添加 hub」走原来的配对页（跳过欢迎页，完成后回到 hub 列表），配对成功的 hub 按 mesh 并进已有的组或成为新的一组。
- Persona 的「在哪运行」只列选中组的机器。机器监控的总览把各组拼在一起（每台机器 id 带后缀），详情按机器的组取。
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
  - 把「公司」设为 Persona 和收件箱用的组：`/api/v1/assistant`、`/api/v1/inbox`、设备偏好改向公司那台，家里那台的 WebSocket 没有重连，列表里的键不变。
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
- Application ID 走配置：`~/.config/persona/config.json` 的 `android.application_id`，没写就用 `app.bundle_id`，再没有就是 `com.example.jarvis`。
- 同一台手机上并排装两个版本（比如主线和新版对比）：直接调 Gradle，给不同的 `-PjarvisApplicationId` 和应用名 `-PjarvisAppLabel`（默认「Persona」），例如 `./gradlew -PjarvisApplicationId=<包名>.next -PjarvisAppLabel="Persona 新版" :app:assembleDebug`。两个版本都注册 `jarvis://` 深链，扫配对码时系统会问用哪个 App 打开。85 MB 左右的 debug 包经 USB 流式安装时，个别手机会中途断开 adb，重装一次即可。
- 明文 HTTP 仅用于本地或受信任网络；公网使用 HTTPS / WSS 和配对指纹钉扎，优先 IPv6。实际目标由 `HubHttpClient` 校验，见 [network.md](network.md)。
- App 图标和 iOS 是同一张（`apple/Jarvis/Resources/Assets.xcassets/AppIcon.appiconset/icon.png`）：自适应图标的前景是这张图缩进到 108 dp 画布中间的 72 dp 安全区
  （`drawable/ic_launcher_fg.xml` 内缩 18 dp，图在 `drawable-nodpi/ic_launcher_art.png`），背景是图边缘的深棕 `#1B110D`，桌面裁成圆形还是圆角方形都不会切到圆环；
  主题图标（Android 13+）用 iOS 的 tinted 版当遮罩。通知栏小图标 `ic_stat` 是同一个标志（圆环 + 四角星）的白色剪影，放在 `core/designsystem/src/main/res/drawable/`，所有通知（常驻、需要你、做完了、手机工具、锁屏测试）和磁贴都用它。iOS 换图标时这两张 PNG 要重新导出。
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
| `:app` | 应用入口、四 tab、Hilt 装配、配对生命周期、作用域桥接、系统分享和锁屏测试 | `:core:contracts`、`:core:domain`、`:core:terminal`、`:core:navigation`、`:core:hub`、`:core:data`、`:core:chatlist`、`:core:designsystem`、`:core:demo`、`:feature:chat`、`:feature:agents`、`:feature:assistant`、`:feature:inbox`、`:feature:settings`、`:feature:terminal`、`:feature:machines`、`:feature:files`、`:feature:knowledge`、`:platform:notify`、`:platform:phonetools`、`:platform:shortcuts` |
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
| `:feature:chat` | 共用对话、Markdown、工具 / diff / 待办、照片和文件标签、长按菜单、报错卡、看图和标注、交互面板、附件组件和应用内链接阅读 | `:core:contracts`、`:core:domain`、`:core:designsystem`、`:core:chatlist`、`:core:navigation` |
| `:feature:files` | 文件树、文本 / 图片 / Markdown / PDF 预览、Git 和提交详情、路径插入；从 Agent 对话 ⋯ 菜单和消息里的文件标签进入 | `:core:contracts`、`:core:hub`、`:core:designsystem`、`:core:navigation` |
| `:feature:inbox` | 统一收件箱、飞书会话、邮件详情、AI 操作和回复组件；邮件传输仍待装配 | `:core:contracts`、`:core:domain`、`:core:designsystem`、`:core:navigation` |
| `:feature:knowledge` | 知识库列表、阅读和导航；从 Persona 抽屉进入 | `:core:contracts`、`:core:designsystem`、`:core:navigation` |
| `:feature:machines` | 机器 Dashboard、详情和指标图；从 Agents 机器菜单进入 | `:core:contracts`、`:core:designsystem`、`:core:navigation` |
| `:feature:settings` | 配对、准备清单、诊断、设备与通知偏好、关于和 APK 安装；设备设置服务仍待装配 | `:core:contracts`、`:core:domain`、`:core:designsystem`、`:core:navigation` |
| `:feature:terminal` | 终端 Canvas、流式帧消费、接管和输入；从对话页和通知的终端兜底进入 | `:core:contracts`、`:core:terminal`、`:core:designsystem`、`:core:navigation` |
| `:platform:notify` | 前台服务、常驻汇总通知（`FleetFeed`）、通知策略和按钮、操作日志、开机接收与连接保活 | `:core:contracts`、`:core:domain`、`:core:designsystem`、`:core:navigation` |
| `:platform:phonetools` | 手机工具目录、注册、权限与写入确认，通讯录 / 日历 / 照片 / 位置等实现；设置 › 手机数据进入 | `:core:contracts`、`:core:navigation`、`:core:designsystem` |
| `:platform:shortcuts` | 主屏小组件（Glance）、快速设置磁贴、长按图标快捷方式和「批准待处理」对话框，见「常驻通知和桌面入口」 | `:core:contracts`、`:core:domain`、`:core:designsystem`、`:core:navigation`、`:core:hub`、`:platform:notify` |

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
对话场景是手机上最长的一个 Agent 会话（transcript 最大）；第一轮用的 Persona 主对话每次只渲染约 20 帧（没滚动起来），不作数。

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
  要在「设置 › 通知与控制中心 › 通知管理 › Persona」里打开。锁屏后长时间的后台连接还没在真机上测

## 形象（Persona 的脸）

照 iOS（`docs/persona.md`）复刻内置的默认形象「面罩」（#638）：`core/designsystem` 的 `PersonaFace` 用 Canvas 画，几何、配色比例和动作取自 iOS 的 `PersonaFace.swift`，按当前主题色着色，任意尺寸都清楚，不用位图。

- 用在：Persona 页顶栏左上角（`JPersonaButton`，替换原来的「对话列表」图标，点它照旧打开对话列表；读屏读「对话列表」和「Persona，<状态>」），对话列表顶部（原来的字母头像，宽屏常驻的侧栏也是它）。两处显示同一个状态。
- 五个状态：空闲（每 3.6 秒眨眼）、思考中（眼睛看右上，思考气泡三个点轮流亮）、工作中（眼睛左右扫）、等你处理（呼吸的环）、离线（灰，眼睛闭成线）。只有后三个每帧动（约 12 帧/秒），空闲只在眨眼时动；「减少动画」或省电模式时每个状态停在静止的一帧（同 iOS 的低电量模式）。
- 状态的取法（`feature/assistant` 的 `personaState`，规则同 iOS `AppModel.personaState`）：hub 连不上（连接中、等待网络等，「更新中」不算）或这段对话的机器离线是离线；对话等你（`needs_you` 或有待处理的交互）是等你处理；正在跑一轮是工作中；其他空闲。看的是当前打开的对话；新对话页没有对话，只看 Persona 所在的机器在不在线，其他时候空闲。
- 和 iOS 的差别：这一页拿不到对话的最后一条，分不出思考中和工作中，跑着的一轮都显示工作中；等你处理的环用 Android 设计系统的「需要你」色（橙），不是 iOS 的红；晨报风格的主题色就是墨色（近黑 / 近白），面罩和星标会糊掉，所以晨报里脸用次级墨色（中灰），不给这个风格加颜色；iOS 在收件箱有事时给脸挂托盘标记，Android 的收件箱是底部的独立 tab、自带提醒，脸上不再挂。
- 还没做：内置的「汇流」、hub 上的形象包（`~/.persona/personas/`，APNG 帧）、设置 › 外观 › 形象的选择页。
- 截图核对：组件目录（调试包 `--es debug.openPage catalog`）的「形象」一节并排画五个状态。

## 助手设置（设置 › 助手）

用户要求（2026-11-02）：「jarvis 的会话和记忆等等内容，是运行在哪个设备上的，现在也完全没有设置选项」。代码在 `feature/assistant/…/settings/`：页面 `AssistantSettingsScreen`，状态和请求 `AssistantSettingsState`，接口用 `AssistantManagement` 的 `config` / `updateConfig` / `launchers`（`GET` / `PUT /api/v1/assistant`、`GET /api/v1/machines/{id}/launchers`）和原有的记忆接口。

- 入口：设置列表「助手 › Persona」（`SettingsPage.Assistant`，深链 `jarvis://open?target=settings&page=assistant`；宽窗口在设置列表右栏打开，页内的子页也在右栏）。Persona tab 标题菜单的「助手设置」、右上角的「助手资料」按钮、抽屉里的「资料页」「启动器」在 Persona tab 里原地打开同一页。体验模式没有这页，显示说明。
- 在哪运行：机器（在线状态；`kind: local` 的写「hub 所在的机器」）、启动器和模型、工作目录（按那台机器的 home 缩成 `~/.persona/assistant`）。启动器列表取那台机器的 launchers，只列 Claude 类型：助手是无头的 `claude -p`，Codex / TRAEX 的启动器起不了它。保存的启动器不在那台机器的列表里时提示换一个，否则新对话起不来。
- 换机器：先确认，确认框写明「记忆和会话记录在原来那台机器上，不会跟过去」，目标离线时另外写明；确认后 `PUT {"machine_id":…,"main":"-"}`。主对话是原来那台机器上的 transcript，换过去接不上，所以一起清掉，下一条消息在新机器上开新的主对话；原来的会话和记忆留在那台机器上，换回来还在（原来的主对话成了普通会话）。换完刷新 Persona 的会话列表，重新读启动器和记忆。
- 数据存在哪里：会话记录和记忆在助手所在的机器；关于我、定时任务和设置在 hub；知识库在 hub 所在的机器（`~/.persona/data`）。
- 了解你：「关于我」编辑 `notes`。还没在 App 里保存过时打开的是 hub.json 的内容，并说明保存后以这里为准；清空也照样保存（告诉助手什么都不写）。记忆列出 `memory/*.md`，新的在前，标题先取 `MEMORY.md` 索引行，其次 front matter 的 name，类型标成用户 / 反馈 / 项目 / 参考；点开看正文，可以编辑整个文件（含 front matter）或删除（hub 同时删索引行）。`MEMORY.md` 只能看。
- 时区：默认「跟随手机」，和 iOS 一样：打开 Persona tab 或这一页时，hub 的 `timezone` 和手机不同就 `PUT timezone`。关掉以后从列表里挑（当前的和手机的排在最前，可搜城市名、中文名、UTC 偏移），之后不再自动同步。这个选择存在本机（SharedPreferences `jarvis-assistant`），不跟 hub 走。
- 主动做事：「在哪运行」下面一节，一行「定时任务、跟进和运行记录」打开 `AssistantProactiveScreen`（在这一页里原地打开，返回回到这一页），见下文「主动做事」。Persona 抽屉的「定时任务」在 Persona tab 里原地打开同一页；旧的资料页 `AssistantProfile` 已删掉。
- 截图：`debug.assistantSettings` 直达页内一部分（notes、memory、memory-file、memory-edit、memory-delete、machine、launcher、timezone、move 即换机器确认框，proactive 即主动做事），和 `debug.openLink 'jarvis://open?target=settings&page=assistant'` 一起传。假 hub 支持 `PUT /api/v1/assistant` 和 `PUT` / `DELETE /api/v1/assistant/memory/{name}`；`MOCK_MACHINES=1` 多一台离线机器，可以看离线的样子。
- 单测：`AssistantSettingsStateTest`（读写配置、换机器的提示和清主对话、启动器缺失、跟随手机的时区、关于我、记忆的读取 / 修改 / 删除）、`AssistantConfigTest`（`PUT` 只带改了的字段，空的关于我也发）。

## 收件箱的 AI 草拟、交给 AI，知识库的六类、条目页和打标

2026-11-02 从主线旧 App（`81dc6928`）按行为移植，iOS 的同名功能对照着做（`apple/Persona/Views/Inbox/InboxActions.swift`、`KnowledgeView.swift`）。

收件箱（`feature/inbox`）：

- **AI 草拟 / AI 改**（`InboxAiDraft.kt`）：一行「要求」加按钮，没有草稿时叫「AI 草拟」（占位「要怎么回（可以不填）」），有草稿时叫「AI 改」（占位「让 AI 改：…」）。请求 `POST /api/v1/inbox/{id}/draft {"instruction"}`，instruction 是「要求」和「在这版上改：<当前草稿>」两行（都可缺，`draftInstruction`）；hub 等模型写完才回，所以这个请求单独用 120 秒的读超时（配置值，未实测，`HttpInboxActionsTransport.DRAFT_READ_TIMEOUT_SECONDS`）。结果换掉回复的草稿（新的 `client_id`，`ReplyComposer.replaceWithAi`）；回复在发送中或结果待核对时不换。失败写「没草拟出来：<原因>」（hub 自己给了原因就用 hub 的话，连接问题用统一的操作反馈）。入口：飞书会话页输入框上方的「AI 草拟」（打开 `InboxDraftSheet`，和 AI 分身草稿卡的「改一改」是同一个浮层）、邮件页的「回复」浮层里、底部按钮的「AI 草拟」。原来的「总结 / 拟回复」两个快捷钮去掉了：总结是「交给 AI」的第一个预设，拟回复换成在原地草拟（iOS 也是这两个动作）。
- **AI 写的草稿发送前一律确认**（已拍板）：草稿带 `ReplyDraft.ai` 标记（AI 分身的草稿、AI 草拟 / AI 改的结果；在上面改几个字仍算，删空后重写就是自己的话），记进回复日志（`InboxStoredDraft.ai`）。发送 AI 草稿先弹「以你的身份发送？」，写明「发到飞书「<标题>」」或「回复邮件「<标题>」」和全文（`SendAsYouDialog`，`InboxSendConfirm.kt`），列表行内的草稿卡、草稿浮层都一样；自己手打的回复（飞书会话页底部的输入框）照打的发，不另外确认。邮件回复本来就要确认，文案统一成同一个对话框，并写明发给谁、回复对象变了时提醒。结果待核对时按钮是「核对结果」，只核对、不再确认。
- **草稿卡**（`AgentDraftCard.kt`）：AI 分身在处理 / 排队时一行状态，失败时一句「AI 分身处理失败，你可以直接回复」，有 AI 草稿时一张卡（「改一改」「发送」）。列表行只在 AI 分身拟好了、或者有存着的 AI 草稿时才建回复编辑器（每行都建会一次读所有草稿）；自己手打的草稿仍在行里写「草稿 · …」。飞书会话页和邮件页的卡片在页面一打开就建好，AI 草拟的结果马上出现在卡上。从深链先进了邮件页、列表还没来时，AI 分身的草稿等事项到了再填进去（只填没动过、也没存过草稿的回复，`offerAiDraft`）。
- **邮件页**：元信息下面是 AI 摘要（`JNotice`，标题「AI 摘要」）和 AI 分身的草稿卡；卡上的「改一改」打开回复浮层，「发送」走邮件自己的确认和回复对象核对，都会先切回最新一封。回复浮层里有 AI 分身的依据（草稿还是它那版时）和 AI 草拟那一行。
- **交给 AI**（`InboxHandOffSheet.kt`，逻辑在 `InboxHandOff.kt`）：列表行左滑和长按菜单、飞书会话页、邮件页的「交给 AI」打开同一个浮层：「要它做什么」（空着用第一个预设）和三个预设「总结一下，告诉我要做什么，给出建议的回复」「查清楚背景再告诉我」「直接帮我处理，回复前先问我」；「交给谁」：Persona（主对话，已拍板）或最近活跃的 8 个在线 Agent（不含助手和离线的，新的在前；B 组的能力字段合进来以后再排除只读会话），每行写机器名；没有在线 Agent 时说一句；「同时标为已处理」默认开（已处理的事项不显示）。消息和 iOS 一样：`<jarvis-context kind="context" title="飞书 · <标题>|邮件 · <标题>" url="">` + 正文（hub 的事项上下文，没有就「发件人：预览」；邮件页用整封信的头和正文）+ 飞书 feedId 或邮件 account_id / mailbox / uid 的注记 + 空行 + 要求；属性转义，正文里的 `</jarvis-context>` 去掉。给 Persona 走 `MainConversationSender.sendToMain`，给 Agent 走和 Agents 同一个 outbox 的 `MessageSender.send`（`InboxModule` 注入 `data.messages`）；同样的文字发同一个目标时沿用操作 ID（`HandOffAttempt`），重试不会排两份。入队以后关掉浮层、标已处理（失败只提示），切到 Persona tab 或打开那个 Agent 的对话；对话里这条消息是一张上下文卡片加要求的气泡。失败写「没交过去：<原因>」，浮层留着。
- **列表**：类型标签按旧 App：@ 所有人的写「@所有人」（`lark.at_all`），只抄送给我的邮件写「抄送」（hub 省掉的 `mail.direct` 就是 false）。空状态三种：「收件箱是空的」（配好飞书或加邮箱的说明）、搜索无结果「没有找到」、筛选后为空「都看完了」。
- 浮层里放 `ComposerFrame` 时把 `LocalGlassSource` 置空（见「经验」）。

知识库（`feature/knowledge`）：

- **六类**：「项目 / 会话 / 人 / 文档 / 会议 / 日程」，横排 chip，选中的一类带件数；会话包括私聊（旧 App、iOS、`docs/redesign/parity.md` 3.1 都是全部会话；原来只列群聊）。搜索时 chip 收起（搜的是全部类）。项目分「重点 / 其他进行中（没有重点时叫进行中）/ 已完成 / 不是项目」，「不是项目」默认收起，下面写为什么在这、怎么恢复；其他类一段「按重要级和相关性」（带标签时段名是 #标签）。重读同一个视图（下拉、刷新、打标保存后）时先留着旧内容，不闪成加载圈。
- **搜索结果**：按条目合并，`docs/<id>.json` 这种也认得出条目；同一句只出现一次，档案和时间线在资料前；按类分段；每条最多两处命中：来源（档案 / 时间线 / 资料 / 聊天记录 · 日期）和去掉列表符、JSON 键和引号的那一行，查询的每个词加粗；超过两处写「N 处」。列表里没见过的条目先用类名当名字（后台读那一类的列表补上）。
- **条目页**：头部是重要级、项目的短期 / 长期和状态、相关性、全部标签，一句话，「问 Persona」（原有）和项目才有的「开 Agent」；「在做这个项目的 Agent」（hub 的 `agents`，只留在线、非助手的，`AgentRow`，点开进那个 Agent 的对话）；时间线新的在前，每行「时间 · 来源」一行、内容一行，最新一条用强调色，先 6 条，「显示全部 N 条 / 收起」；档案去掉标题行、和一句话重复的行、「重要级 … 相关性」那行，去完是空的就不显示；关联按类分段（关联的项目 / 会话 / 人 / …），每类先 4 个，「全部 N 个 / 收起」；代码目录写路径末几级和机器；「最近 7 天的消息」折叠；「目录」加一句「Persona、AI 分身和各台电脑上的 Agent 都能直接读这个目录。」
- **打标**（`KnowledgeTagEditor.kt`）：右上 # 打开「打标 · <名字>」：重要级 P0–P3（P0、P1 带状态色点）、「与我相关 N」滑杆（0–100，步长 5，设计系统新组件 `JSlider`）、标签（空格分隔）、项目还有类型「短期（需求）/ 长期（使命）」和状态「进行中 / 暂停 / 已完成 / 不是项目」，说明「手动打的标会锁住，hub 自动整理时不再覆盖。」保存 `PUT /api/v1/kb/{kind}/{id}`，只发设置了的字段（空标签列表要发）；保存中「正在保存…」，失败「没有保存：<原因>」留在浮层里；成功后重读条目，列表也重读（`KnowledgeEdits`）。相关性在动滑杆之前保持原值（不按 5 取整），只改别的字段不会改掉它。
- **开 Agent**：给项目条目，带第一个代码目录（`<机器>:<路径>`）和「项目背景 + 空行」作为第一条消息（`KnowledgeAgentStart`）。`knowledgeEntries(…, onNewAgent = …)` 由外壳接到新建 Agent 的预填入口（E 组的 `NewAgentRoute`）；没接时退而求其次：把项目背景放进剪贴板、切到 Agents，并提示在新建会话里粘贴。

假 hub：`MOCK_DRAFT_SECONDS`（AI 草拟要等多久，默认 1.5 秒）、`MOCK_DRAFT_FAIL=1`（草拟失败，502）、`GET /mock/inbox_drafts`（每次草拟的 instruction）；`MOCK_INBOX_LABELS=1` 多一条 @所有人 的群消息，并给一封邮件（周二分享那封）加上 AI 分身的草稿和依据；`MOCK_NO_LIVE_AGENTS=1` 所有 Agent 离线（交给 AI 只剩 Persona）；`MOCK_KB_SAVE_FAIL=1` 打标保存失败（502）；`MOCK_KB_MANY_LINKS=1` 让「天气 App」关联 6 个人（看「全部 6 个 / 收起」）。知识库原有数据里「日程」是空的，可以看空状态；`GET /mock/kb_updates` 看打标发了什么。

单测：`feature/inbox` 的 `InboxHandOffTest`（和 iOS 一样的交接消息、最近在线 Agent、操作 ID、行标签）、`InboxDraftTest`（instruction 拼法、AI 标记的来去、发送中不换草稿、AI 分身草稿只填没动过的回复、确认文案）；`core/data` 的 `InboxHandOffRepositoryTest`（`at_all` / `mail` / `direct` 的解析、AI 草稿和手打草稿的行状态、交给 Persona 和交给 Agent 走哪个 outbox、草拟走 actions）；`core/hub` 的 `InboxActionsTransportTest`（draft 的路径、请求体、空草稿和失败）、`KnowledgeRepositoryTest`（agents 只留在线非助手、PUT 只发设置了的字段）；`feature/knowledge` 的 `KnowledgePresentationTest`（项目分段、六类、命中的条目 / 来源 / 文本、按条目合并、逐词加粗、时间线、档案去重、代码目录、开 Agent 的预填、打标表单和请求体）、`KnowledgeTaggingFlowTest`（保存失败留在浮层、保存后条目和列表都重读、Agent 只留在线非助手）；`core:designsystem` 的 `SliderTest`。

## 常驻通知和桌面入口（2026-11-02 从主线旧 App 移植）

iPhone 的实时活动、小组件和快捷指令在 Android 上的对应物。四处说同样的话：文案和挑选规则在 `core:domain` 的 `FleetSummary` / `FleetText`（照 iOS `FleetText` 和 hub `buildFleet`），数据都来自 `:platform:notify` 的 `FleetFeed`。

- **数据**：`FleetFeed` 收 WS 的 `fleet` 事件，连接每次连上后再 `GET /api/v1/fleet` 一次（hub 只在变化时推），最后一份按配对存在 SharedPreferences `fleet`，进程没跑时小组件和磁贴照样能画；取消配对清掉，换了配对不显示上一个 hub 的。HTTP 和配对标识由 App 的 `FleetModule` 提供（`FleetSource`）。
- **常驻汇总通知**（前台服务那条，`OngoingStatus` / `Notifier.ongoing`）：标题「N 个需要你 / 在工作 / 做完了」，副标题其余数量；「锁屏显示详情」开着时正文是第一个 Agent「名字 · 在做什么」，展开最多三行带「机器 · 空间 › 标签」再加「+N 个」；只剩工作中时走计时；第一个授权「拒绝 / 允许」、第一个计划「查看 / 批准」（`acceptEdits`，允许和批准要解锁，按钮走 `ActionReceiver` + `NotificationActionExecutor`）；只有一个 Agent 时点通知直达它，否则进 Agents。没东西、只剩做完未看满 5 分钟、没连上时退回连接行。「锁屏显示详情」关着（默认）时，正文和展开列表也只用泛化文字（「4 个需要你」「有个授权等你」「2 个授权等你」「1 个在工作」），不放 Agent 名字、位置和请求详情，即使系统设成「锁屏显示全部内容」锁屏上也看不到名字；按钮照旧。细节见 `platform/notify/README.md`。
- **主屏小组件**（`PersonaWidget`，Glance 1.2.0）：「Persona」标志行，带状态色点的标题，其余数量，按高度画 0（< 140dp）/ 1（< 190dp）/ 2（< 240dp）/ 3 个 Agent（色点 + 名字 +「在做什么 · 机器 · 空间」）和「+N 个」（旧 App 在 150dp 高时画两个，会被切掉）；没东西「没有 Agent 需要你」，没配对「还没配对 / 打开 Persona 扫 hub 的二维码」。点某个 Agent 进它的对话，点别处和磁贴一样（唯一的 Agent 或 Agents）；只导航，不能在小组件上批准。尺寸 110×100 / 250×150 / 250×200 / 250×260，最小 110×100dp、默认 3×2 格、可横竖缩放、不定时刷新（`updatePeriodMillis=0`），汇总变化时 `SurfacesRuntime` 让它重画。配色和字号取自设计系统（`SystemSurfaces`：用户选的界面风格的色板和字号，浅色 / 深色一对交给 Glance 跟着系统换），圆角用系统的小组件圆角。
- **快速设置磁贴**（`PersonaTileService`）：标签「Persona」，副标题是汇总标题（「都处理完了」「还没配对」）；需要你或工作中时点亮，没配对不可用；点了和小组件一样打开。
- **长按图标快捷方式**（`ShortcutPublisher`，运行时发布，静态快捷方式要写死 application id）：「问 Persona」打开 Persona tab 并把光标放进输入框、弹出键盘（`jarvis://open?target=tab&tab=jarvis&focus=composer`，冷启动和 App 开着都生效；在内嵌对话里时回到首页的输入框，发出去照样进主对话）；「批准待处理」；后面跟最近活跃的 2 个在线 Agent（不含 Persona 自己，按更新时间），点了直达对话，变了才重发，取消配对时去掉。
- **批准待处理**（`ApproveFirstActivity`，不导出、透明、只显示设计系统的对话框）：从 hub 现拉待处理，取最早的一条。授权问「允许 <摘要>？」，计划问「批准这个计划？」并说明「批准后自动接受编辑，命令仍会先问你」（`ApprovalPolicy` 的快捷入口：计划只用 `acceptEdits`，要先解锁）；下面一行「会话 · 机器」。提问和终端对话框说明要打开 Persona 并给「打开 Persona」（进那个对话）；另有加载中、没有待处理、没配对、出错几种。按钮在发送时不可点，发送结果不明时说「结果待确认」而不是让人重试；成功后 Toast「已允许 Bash」/「已批准计划，开始执行」。

和旧 App 的差异：常驻通知的连接行和汇总共用一个「实时状态」频道（旧 App 连接行另有一个最低优先级频道）；「锁屏显示详情」关着时连通知正文也不放名字（旧 App 只靠公开版本，系统设成显示全部内容时锁屏上会有名字）；数据不再在手机上自己算，而是 hub 算好的 `fleet`；快捷方式批准计划用 `acceptEdits`（旧 App 和 iOS 是 `bypassPermissions`，按统一规格改）；最近 Agent 的快捷方式换成终端提示符图标。

调试（只在 debug 包里）：

```sh
# 「批准待处理」的各个状态：loading、nothing、unpaired、question、screen、permission、plan、sending、error
adb shell am start -n <包名>/persona.platform.shortcuts.ApproveFirstPreviewActivity --es debug.approveState plan --es debug.style board --es debug.theme dark
# 小组件的三种尺寸，像启动器一样托管在壁纸上；先给一次绑定权限
adb shell appwidget grantbind --package <包名> --user 0
adb shell am start -n <包名>/persona.platform.shortcuts.WidgetPreviewActivity
# 磁贴放进快速设置
adb shell cmd statusbar add-tile <包名>/persona.platform.shortcuts.PersonaTileService
# 问 Jarvis
adb shell "am start -a android.intent.action.VIEW -d 'jarvis://open?target=tab&tab=jarvis&focus=composer' <包名>"
```

假 hub：`MOCK_FLEET=1`（或 `all`）连上就推 `fleet`、每次作答后再推，`GET /api/v1/fleet` 总是有；`MOCK_FLEET=permission` 只留授权（第一个请求带「拒绝 / 允许」），`working` 只有工作中和做完未看（计时），`done` 只有做完未看，`one` 只有一个工作中的 Agent（点了直达），`empty` 什么都没有（连接行）。默认数据下第一个请求是计划（「查看 / 批准」）。

验证状态（2026-11-02）：单测 `FleetSummaryTest`、`OngoingStatusTest`、`ShortcutTextTest`、`ApproveFirstModelTest`、`ComposerFocusRequestsTest`、`RouterTest` 和 `:app:assembleDebug` 在车道里跑过一轮；之后几处小改动（小组件按高度排行数、对话框等列表刷新、连接行的通知类别、`FleetState` 存盘往返的单测）没再编译。通知、小组件、磁贴、快捷方式和对话框都还没在模拟器或真机上看过（按协调者要求统一截图验证），Live Update 胶囊要 Android 16 以上的设备。

## 手机数据工具和 iOS 对齐（2026-11-02）

Claude 调 `phone_*` 时 hub 把同名工具合并成一份定义（说明更长的那份），调用可能发给任一台声明了它的手机，所以 Android 的工具和 iOS 逐字一样：名字、英文说明、参数（类型、说明、范围）、必填项都照 `apple/Persona/PhoneTools/*.swift`；数字超范围夹到范围内、多余参数忽略；返回结构用 iOS 的键名；拒绝回 `The user declined on the phone.`，确认 90 秒没人答算拒绝。`phone_notifications_list` 是 Android 自己的工具，保留。对齐用的夹具由 `python3 scripts/assets/export_ios_phone_tools.py` 从 iOS 源码导出（`--check` 检查是否过期），`PhoneToolCatalogTest` 逐个比。有意保留的差异（剪贴板和定位只在前台、视频取图给预览帧、没有可写日历时建本地日历）和全部规则见 `platform/phonetools/README.md`。

照片取图的图片块以前写成 `mimeType`，hub 只认 `mime_type`，图到不了 Claude：已改，`PhotoContentTest` 守着。

假 hub：`GET /mock/tool_call?name=phone_device_info&args=<JSON>` 给声明了这个工具的手机发一次调用（像 hub 那样），`GET /mock/phone` 看手机注册了什么、答了什么。验证状态：单测通过；真机 / 模拟器上的工具往返、权限弹窗、确认对话框和日历写入还没测。

## 看图和标注

对话里的图片全屏看，标注以后作为照片放进输入框（主线旧 App d2d484ef 的移植，2026-11-02）。查看器和编辑器是设计系统的组件（`core:designsystem` 的 `ImageViewer.kt`、`ImageEditor.kt`），收件箱、文件页以后可以直接用；对话的组装在 `feature:chat` 的 `media/`（`ChatImageViewer`、`MediaShare`）。

- 入口：对话页点图片（Agent 的图、工具截图、你发的照片、附图缩略图）打开 `ChatImageViewer(images, initialIndex, onDismiss, onEdited, photoLimitReached)`，由对话页接线，图片按对话里的顺序排。组件本身是 `ImageViewer(images, initialIndex, onDismiss, actions)` 和 `ImageEditor(source, onCancel, onDone)`；一张图是 `ViewerImage(key, description, width, height, load)`，`load` 返回编码后的字节，抛错或空字节算失败；动作是 `ViewerAction(label, icon, onClick)`，`onClick` 拿到页码和原始字节，返回要在查看器里显示的提示（null 不提示）。
- 样子：整页（`JFullScreen`），始终深色，状态栏、导航栏图标是浅色；顶部 ✕ 和「n / m」（多于一张才有）；底部一排动作，玻璃里是浮起的胶囊（玻璃取样的是图片），晨报和看板是贴底的栏；按钮文字单行，放不下先只留第一个（保存）的文字，再放不下全换成图标。读图时转圈（知道宽高时先画一块同比例的底），失败时「图片加载失败」和「重试」。
- 手势：左右滑看别的图；双指 1–6 倍，以两指中间为中心；双击在手指处放大到 2.5 倍，再双击复原；放大后单指拖动，图的边不会拖进画面；单击显示 / 隐藏按钮；1 倍时竖着拖把图拉走，背景跟着变淡，超过 120dp 松手关闭，不够就弹回去。返回键和 ✕ 关闭。
- 解码：在后台线程用 `ImageDecoder`（按 EXIF 方向转正，`BitmapFactory` 不管方向），按 2 的幂降采样到屏幕长边的两倍以内（1024–4096）；原始字节留给动作；最近 8 张的字节留在内存里，来回滑不再读。
- 动作（`ChatImageViewer`）：保存（MediaStore 写进 `Pictures/Jarvis`，不要权限：「已保存到相册」/「保存失败」）、编辑（对话把结果接进输入框时才有）、分享（字节写到 `cache/shared/`，经 `<包名>.chat.files` 的 FileProvider 交给系统分享，失败「分享失败」；超过一天的旧文件顺手删掉）、拷贝（同样的文件以 content URI 放剪贴板：「已拷贝图片」/「拷贝失败」）。这些结果显示在查看器自己的提示条里：`JFullScreen` 是一个对话框窗口，App 统一的提示（`LocalAppFeedback`）画在下面的窗口里，会被盖住。
- 编辑：框选 / 画笔、红黄绿蓝白黑六色（`MarkupColors`，固定的，不随风格变）、撤销、清除。完成时按原图分辨率把标注画进去，笔粗是屏幕上看到的粗细换算成图片像素（`ImageMath.strokeInImage`），画出来什么样，发出去就什么样。结果按长边 1600、JPEG 75 写进 `cache/chat-captures/`（透明的地方先铺白），做成 `ComposerAttachment(image = true)` 交给对话的 `onEdited`，再用 `LocalAppFeedback` 提示「已加到输入框，补一句话就能发」并关掉查看器；输入框里已经有 4 张照片时只提示「一次最多 4 张」。编辑用的图按不小于 1600 的长边解码（成品最多 1600）。
- 纯计算在 `ImageMath`（适配框、屏幕点到图片像素、笔粗、降采样、缩放、缩放和平移、拉走时的透明度、图片类型 PNG / JPEG / GIF / WebP / HEIC / AVIF），单测是 `core:designsystem` 的 `ImageMathTest`；动作的顺序、文案和「最多 4 张」在 `feature:chat` 的 `ChatImageViewerTest`。
- 统一验证时的截图（三套风格 × 深浅色，`debug.style`、`debug.theme`）：
  - 组件目录（`debug.openCatalog true`）拉到最后一节「看图」。「看图（3 张）」打开查看器：每张图要等 0.6 秒，能拍到转圈和同比例的底；第一张横图、第二张竖图（看「n / m」），第三张故意读不出来（失败页）；单击隐藏按钮、双击放大、拉到一半（背景变淡、按钮收起；用 `adb shell input motionevent DOWN / MOVE` 按住不放再截，`input swipe` 会直接松手）；点保存 / 分享 / 拷贝看查看器里的提示条（示例不会真的执行）；「编辑」在查看器上打开编辑器。「标注编辑」直接打开编辑器：框选、画笔、换颜色、撤销以后、点完成。底部四个按钮按估算在 411dp 宽的手机上都放得下文字，窄一些（`adb shell wm density` 调大）就只有「保存」带文字、其余是图标（估算，未上设备看），两种都要截一次。
  - 对话里（对话页接线以后）：假 hub 带 `MOCK_CHAT_IMAGES=1`，「博客迁移」那段对话有两张图（工具截图、回复前的图），点开看多张；编辑完回到输入框，看附件那一行和提示。

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
- 自己画的面板（双栏的右栏、占位、Persona 的左栏）底色要用 `Surface(color = …)`，不能用 `Modifier.background`：只有 Surface 会设 `LocalContentColor`，
  没写颜色的 `Text` 默认是黑的，深色模式下标题、正文黑字压在黑底上看不见（原来的底部浮层、Scaffold 自带 Surface，所以单栏没这个问题）。
- 同一个页面在两种布局里处在 Compose 树的不同位置，位置决定的 `rememberSaveable` 会丢（草稿、Persona 正在看的对话）：按页面用 `SaveableStateHolder.SaveableStateProvider(key)` 包住，
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
- `scripts/mock/mock_hub.py` 带 `MOCK_RICH=1 MOCK_LINKS=1` 时给对话、Persona 主对话、收件箱各放了几个链接。
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
- 两台手机同时连着 hub 时，phone_* 调用发给最后注册的那台：测模拟器上的工具时要关掉真机上的 Persona，不然调用发到真机、它那边没开这个数据源就超时。
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

当前组合根使用 `:core:designsystem` 的 `JarvisTheme`，风格来自 `AppStyleStore`（设置 › 界面风格）。底部固定四个 tab：Persona / Agents / 收件箱 / 设置，三套风格顺序相同、都带文字，样子各按各的风格（玻璃是悬浮坞，晨报和看板是底栏）；dock 属于每个 tab 的第一页，推进去的页面（对话、文档、二级设置）盖住它；访问过的 tab 保持组合，切回来不重建（详见 `docs/android-design.md` 第 3 节）。平板和折叠屏按窗口宽度分布局：expanded 时四个 tab 进左侧导航栏，列表和详情分两栏，桌面姿态把对话放上半屏、输入框放下半屏（第 3.1 节）。四个 tab 各自保留返回栈，Agents 角标统计普通 Agent 的待处理交互数，并按 hub 的会话／空间屏蔽及例外规则过滤；Persona 角标统计助手待处理交互数，同一会话的多个请求分别计数。收件箱角标统计「需要你」分组中的未处理事项和未屏蔽请求。当前 tab、Agents 机器筛选及各 tab 打开的会话保存在配对目录内，正常冷启动恢复；退出、撤销或重新配对时一并删除，已退出的写入者不能重建位置文件。旧版外部 shell 偏好及准备清单等兼容全局记录在退出配对时清除。

连接状态显示在各页自己的标题位（Agents、收件箱、Persona 和对话页），外壳不再在页面上方另加横幅，避免重复和顶部多出空白。轻量刷新失败不另弹提示；用户操作失败由统一出口处理，断网提示共用 4 秒去重窗口。这是提示策略配置，性能未实测。操作结果未知时仍保留会话内的核对入口。

第二批模块入口：

- 对话页终端按钮进入完整终端；助手对话隐藏该按钮。通知的终端兜底（`jarvis://open?target=terminal&machine=…&session=…`）打开同一页面，下面垫着原对话。
- 收件箱事项和飞书会话深链（`target=inbox&item=…`、`target=lark&chat=…`）先等事项列表刷新，再换成收件箱列表会打开的飞书或邮件页面；事项已不在列表时显示说明。
- Agents「列表设置」面板里的「机器监控」进入机器列表；树里每台电脑栏头右边的仪表按钮直接进这台电脑的详情。
- Agents 操作和对话页菜单确认关闭成功后，从所有 tab 移除该会话的聊天、请求和终端页面；结果未知时保留页面供核对。分享目标与 Agents 共用 `agentRank`，同状态再按时间和稳定标识排序。
- Agent 对话页 ⋯ 菜单进入文件；确认是仓库后显示 Git 和改动数。文件和 Git 在原会话内切换，插入路径保留草稿和有效选区，并返回输入框。
- Persona 抽屉的「知识库」进入档案、搜索及详情，链接在应用内打开。
- 设置 › 手机数据进入工具权限和开关。应用前台状态与常驻服务实际运行状态共同决定工具可用性；定位和剪贴板仍要求前台。旧配对若没有设备标识，须重新配对。
- 设置 › 助手（`SettingsPage.Assistant`）是 Persona 的设置页，Persona tab 的标题菜单「助手设置」、右上角头像也打开它，见上文「助手设置」。
- 未配对时可选「没有 hub？先体验一下」。体验模式使用独立数据、缓存及待发队列，设置中可退出；缺少演示数据的终端、机器监控和知识库显示说明，不调用真实 hub。

Debug 直达入口及启动参数解析适配器只存在于 `src/debug/`，复用 `:core:navigation` 的 debug 解析，Release 不注册开发入口。示例中的包名替换为实际构建使用的 Application ID：

```sh
adb -s emulator-5600 shell am start -W -n com.example.jarvis/jarvis.android.debug.DebugEntryActivity --es debug.openTab Agents
adb -s emulator-5600 shell am start -W -n com.example.jarvis/jarvis.android.debug.DebugEntryActivity --es debug.openSession machine/session --es debug.conversationKind agent
adb -s emulator-5600 shell am start -W -n com.example.jarvis/jarvis.android.debug.DebugEntryActivity --es debug.openRequest request --es debug.session machine/session --es debug.conversationKind agent
adb -s emulator-5600 shell am start -W -n com.example.jarvis/jarvis.android.debug.DebugEntryActivity --es debug.openPairing true
adb -s emulator-5600 shell am start -W -n com.example.jarvis/jarvis.android.debug.DebugEntryActivity --es debug.openDiagnostics true
```

每次只传一种 `debug.open…` 目标。`debug.openTab` 的兼容参数值可选 `Jarvis`（Persona 助手页）、`Agents`、`Inbox`、`Settings`；`debug.conversationKind` 可选 agent 或 assistant；`debug.openCatalog true` 打开设计系统组件目录；`debug.openLink '<jarvis:// 链接>'` 按 MainActivity 的规则打开任意深链（终端兜底、通知 tab、收件箱事项、设置子页）；`debug.openPage` 可选 machines、knowledge、phone、catalog，直达没有公开深链的页面。链接里有 `&` 时要再套一层单引号，adb shell 会把整条命令交给设备上的 shell。`debug.theme` 可选 light 或 dark，只影响本应用（含各页面自己套的 JarvisTheme），不更改模拟器系统主题。`debug.style` 可选 glass、brief、board，切换界面风格并保存（和在设置里选一样）。`debug.posture` 可选 tabletop、book，在窗口正中假装有一条半折的铰链（只给截图用：android-35 的 Pixel Fold 模拟器不给 App 报 FoldingFeature）。

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

## 对话内容：照片、文件、长按菜单、工具详情、报错卡

主线旧 App（d2d484ef、4588285d）对话部分的移植（2026-11-02），按新设计系统重画；行为和 iOS 对齐。看图和标注见上文「看图和标注」，文件页见下文「对话里的文件和 Git」。

- 你发的照片：消息里 `<attached_images>` 的路径（以及更早版本附在文末的「相关图片：…」「（我从手机发来了图片，请用 Read 查看：…）」两种说明）画成气泡里的方形缩略图（一张大、多张小，放不下换行），文字里去掉这些说明和 Claude Code 的 `[Image #N]`；只有照片时气泡里只有照片。缩略图从会话所在那台电脑读（`GET /api/v1/machines/{mid}/file`，`AttachedPhotos`，字节和缩略图各在内存里留 40 张），读的时候转圈，读不到（文件没了、不是图片、超过 2 MB 被截断）显示「加载失败」，点一下重试；点开进查看器，翻这条消息的几张。以前 `cleanText` 把整段标签换成「[图片]」，手机发出的照片在历史里只剩「[图片]」；现在只有一行预览（`cleanText`）还写「[图片]」。Persona 的对话也显示（`ChatAppBindings.files`），会话没有目录时用 `~` 去读（路径是绝对路径）。
- 文件路径：消息和回复里像路径的字（`FilePaths`：`~/x`、`/x/y`、带目录和扩展名的相对路径，最多 6 个，链接和 `deepseek/v4.1-flash` 这种模型名不算）在下面画成一排可点的标签（文件名 + 类型图标），点了在文件页里直接打开（`ChatChromeBindings.onOpenPath` → `SessionFilesScreen(initialPath)`，相对路径按会话目录补全）。工具卡片里改动上面的文件头、Read 的路径也能点。只在能打开文件的对话里有：Agent 对话（有会话目录时）；Persona 自己的对话没有文件页，不显示标签。
- 离开期间回顾：Claude Code 每次闲置后被看一眼就写一条回顾，画成一行「离开期间回顾 · 21 分钟前 ›」，连着几条合成一行（「离开期间回顾 3 条 · …」，`foldRecaps`，和 iOS 一样）；点开是底部面板，新的在前，每条写「写于 …」和 Markdown 全文（可以选字）。
- 被截断的回复：hub 把超过 24 KB 的文字截断（`full_size`），回复显示截断前的部分（去掉 hub 加的「…（已截断」那一行）和「看全文（N KB）」，点了取全文（`GET …/items/{id}/full`）原地换上；「加载中…」，失败「加载失败，点一下重试」。
- 长按菜单：长按（鼠标右键也行）你的消息、Agent 的回复（包括工作过程里的中间回复）、别的会话发来的消息，被按的那条变淡，弹出操作面板（`JActionSheet`）：复制、选择（全屏的纯文本，可以任意选中，右上角复制全文）、引用（每行加 `> ` 放到输入框开头，已有的草稿接在后面）、分享、交给 Persona（消息作为 `<jarvis-context>` 加「看看这条消息，告诉我该怎么处理。」交给主对话，切到 Persona tab，主对话确定后打开它；Persona 自己的对话里没有这一项）、作为上下文（放到本对话输入框上方，随下一条消息发出）、编辑（只在你的消息上：文字回到输入框，照片作为已上传的附件一起回来）。为了让长按到得了菜单，消息正文不再包 `SelectionContainer`；思考和回顾还能直接选字。只读的对话（子 Agent、历史）没有引用、作为上下文、编辑（`messageMenuWrites`；B 组给会话加上能力字段后，「不能发送」的会话也在这里关掉）。
- 工具详情：Codex 的文件改动（hub 把 FileChange 写成只带 `diff` 的 `Edit`）按 unified diff 画，带新旧行号，`@@` 和「\ No newline at end of file」这类行变淡；新建、删除的文件 hub 发来的是整份内容（没有 `@@`），画成全部新增 / 全部删除。改动上面一行文件头（文件名、目录、+N −N，能点就打开文件）。计划（ExitPlanMode）按 Markdown 画，提问（AskUserQuestion）列出每个问题的标题、可多选和选项，子任务（Task / Agent）显示类型和说明的前 8 行（长的「看全文」），其余工具照旧是「键: 值」。改文件的工具只在出错时显示它的输出（成功时那句「已更新」没有信息）。
- 图片：对话里的图（Agent 的图、工具截图、你发的照片）保持比例、最高 280dp（`ChatPictureFrame`），竖长截图不会铺满好几屏；点开查看器，左右滑看这段对话的全部图片；读不到「加载失败，点一下重试」。
- API 报错卡片（D6）：Claude Code 放弃时写的「API Error: …」回复（包括「Failed to authenticate. API Error: 401 …」这种前面一句话的）和 hub 的「API 错误：…」通知（在顶层时；工作过程里的仍是一步）画成报错卡（`ErrorCard`）：白话原因（和 iOS 同一套：GPU、过载、登录过期、限流、超时、连不上、参数错、5xx…）、状态码、「临时故障」或「要处理」、同一个错误中间只隔着「继续 / 重试 / continue…」时合并成一张写「连续 N 次」（`foldApiErrors`）；点开看请求 ID 和 Session（可复制）、时间、原文（可选中、可复制）。「重试」只在这张卡是对话的最后一行、Agent 没在工作、没有待发的消息、错误不是「要处理」（登录过期、参数错）时出现，点了发「继续」（走 outbox）。Agent 对话和 Persona 对话都适用。
- 进行中那一轮：工作行在「正在运行 swift test，第 5 步」后面接每秒走的用时（「，1 分 20 秒」，R1）。
- Claude 给的下一条建议（会话的 `suggestion`）：会话空闲、输入框为空、没有附加上下文时，在输入框上方显示一个标签，点一下填进输入框（X1）。
- 代码：`core/domain` 的 `MessageText.kt`（`messageText`、`imagePaths`、`FilePaths`）、`lines/ApiErrors.kt`（`ApiErrorInfo`、`foldApiErrors`、`foldRecaps`；`ChatLine.ApiError` / `ChatLine.Recaps` 在 `LineModels.kt`，`chatLines` 最后做这两步）；`feature/chat` 的 `ChatRowActions.kt`（对话页给每一行的能力：全文、打开路径、照片、查看器、菜单、重试，经 `LocalChatRowActions`）、`AttachedPhotos.kt`、`MessageMenu.kt`、`MessageHandoff.kt`（`handToJarvis`、`rememberHandToJarvis`）、`ToolDetails.kt`、`ApiErrorCard.kt`（报错卡和回顾行）、`MessageExtras.kt`（看全文、相对时间、建议）、`ContentPresentation.kt`（`messageModel`、`patchDiff`、`unifiedDiffLines`）；设计系统的 `ChatParts.kt`（`ChatPictureFrame`、`PhotoThumbnail`、`PhotoRow`、`PathChip`、`ChatNoteChip`、`ErrorCard`、`DetailField`，组件目录「对话里的附件和报错」一节）。App 只在 `ShellChat.kt` 传 `files`、`onOpenPath`、`onHandToJarvis`，在 `ChatWiringModule.kt` 给 `ChatAppBindings.files`。
- 单测：`core/domain` 的 `MessageTextTest`、`lines/ApiErrorsTest`（旧 `ApiErrorTest` 的用例）、`LinesFixtureTest`（新的两种行）；`feature/chat` 的 `MessageContentTest`（照片、文件、Codex diff、提问、走动的用时、截断）、`MessageMenuTest`（菜单项、引用、交给 Persona、重试条件、报错标签、相对时间、建议）、`ChatPicturesTest`（从电脑读照片、查看器的图片列表）。
- 假 hub：`MOCK_CHAT_CONTENT=1` 加一个「看图、文件和计划」会话（MacBook，`/Users/me/code/weather`）：两张照片（一张读不到）、只有照片的消息、带路径的回复、计划 / 提问 / 子任务、一条被截断的长回复；「排查 /orders 接口变慢」（Codex，在跑）多两个文件改动（一个 patch、一个新文件）；Persona 的「帮我约周五和韩梅梅吃饭」多一张照片（服务器上的照片只在不开 `MOCK_FILES` 时读得到）。报错卡用 `MOCK_API_ERRORS=1`（Persona 首页对话连续 3 次 502 带重试，「检查 PR #42」最后是登录过期），回顾用 `MOCK_RECAPS=1`，建议用 `MOCK_SUGGESTION=1`（「检查 PR #42」），对话里的图用 `MOCK_CHAT_IMAGES=1`。
- 统一验证时要截（三套风格 × 深浅色，`debug.style`、`debug.theme`，`debug.openSession <机器>/<会话>` 直达）：带两张照片（一张失败）和只有照片的消息、文件标签和点开后的文件页、计划 / 提问 / 子任务的详情、Codex 的 patch 和新文件、看全文前后、回顾一行和面板、长按你的消息 / Agent 的回复 / 工作过程里的中间回复的菜单、「选择文本」页、作为上下文后输入框上方的卡片、交给 Persona 后落到 Persona tab、报错卡（连续 3 次带重试、登录过期没有重试、展开详情）、跑着的工作行在走的用时、输入框上方的建议标签；组件目录「对话里的附件和报错」一节。

## 假 hub 批量截图

先启动专用 Android 模拟器，再指定序列号和输出目录：

```sh
./scripts/app/android.sh shots -s emulator-5600 --out /tmp/jarvis-android-shots
```

命令构建并安装独立测试包 `com.example.jarvis.shots`，只清理该包的数据。随后用 `scripts/mock/mock_hub.py` 在自动分配的本地端口启动假 hub，通过指定设备的 `adb reverse` 自动配对，逐页冷启动并输出深浅色 PNG。覆盖四个 tab、Agent 对话、助手对话（落在 Persona 首页的内嵌对话）、请求、终端、飞书会话、邮件详情、配对、连接诊断、通知和准备清单两个设置子页、机器监控、知识库、手机数据及组件目录。假 hub 为这些页面提供了飞书窗口 / 话题 / 文档、邮件正文、知识库、会话文件和 Git 的示例数据。自动化启动从各 tab 根页开始、不恢复上一张的位置，也不弹准备清单；脚本以配对目录里的 `shell-position.json` 判断外壳已起来。浮层、菜单和表单（状态浮层、搜索、任务页、全文、完整请求页、文件 / Git、筛选、细列表、空间页、长按菜单、新建、历史、飞书话题和文档、设备、机器详情、知识库详情、体验模式）仍要在页面上点开再截。示例联系人在启动假 hub 前统一匿名化；实际截图内容来自假数据。

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

用户要求（2026-11-02）：「jarvis 的会话和记忆等等内容，是运行在哪个设备上的，现在也完全没有设置选项」。「设置 › 助手」拆成两块，这一块管 Persona 自己主动做的事：定时任务、它替自己安排的跟进、运行记录（iOS 的同名页面在 `ios/Jarvis/Views/Settings/AssistantProactiveViews.swift`）。代码在 `feature/assistant/…/proactive/`，对外只有 `AssistantProactiveScreen(onBack, onOpenConversation)`：`onOpenConversation` 打开一段助手对话（运行开出来的、跟进接着的、立即运行新开的）；接口和协程作用域默认取 Persona tab 宿主（`LocalAssistantHost`）里当前配对的。子页面（某个定时任务、新建、全部跟进、运行记录）和返回在页面里自己管，转场和 App 的页面一样从右边推入。

- 入口：设置 › 助手的「主动做事」一节（在助手设置页里原地打开，打开的对话经 `ChatRoute(kind = Assistant)` 落到 Persona tab）；Persona 抽屉的「定时任务」在 Persona tab 里原地打开同一页，打开的对话直接显示在 Persona 首页。
- 首页：Persona 的一句话（几个定时任务开着、下一件在什么时候）；「接下来」时间线（左边大号时刻，定时任务的下次运行和跟进混排，最多 4 件）；定时任务（规则的人话「每个工作日 上午 9:00」、下次、上次结果和开关；`created_by: "assistant"` 的头像角上有火花并写明「Persona 在对话里建的」，上次没跑起来的角上是红色警告）；已安排的跟进（时间、在哪台机器、接着原对话还是开新对话，⋯ 里立即运行 / 打开原对话 / 取消，取消要确认）；最近 3 次运行。右上角 ＋ 从模板新建（会前准备、周报、下班前盘点，和 iOS 同一套）或空白。长按定时任务：立即运行、编辑、删除。
- 定时任务页：名称、开关、时间（时和分两个滚轮，24 小时，TalkBack 可以像滑块一样调）、周几（一 … 日，快捷「每天 / 工作日 / 周末」，最后一天关不掉）、提示词；已有的还有立即运行（有改动先保存）、查看上次结果、历史记录（最近 5 次，更多进运行记录）、删除（要确认）。改了没保存就返回会先问。
- 运行记录：新的在前，按天分组；已运行 / 没跑起来 / 已跳过，准时或晚了多久、手动运行、原因（hub 常见的英文错误译成中文，其余原样显示）；开出过对话的点开就是那段对话。一个定时任务的历史按 `ref_id` 过滤：`Run` 加了 `ref_id`、`due` 两个字段（hub 本来就返回）。「上次」以运行记录里最新的一次为准：hub 的立即运行不更新 `last_run`，只看定时任务自己的字段会把上一次定时运行的时间和这次手动运行的结果拼在一起。
- 写入：`PUT /api/v1/assistant/routines` 是整表替换。每次改动（开关、编辑、新建、删除）在写之前重读整表，只改这一个定时任务，其余定时任务（包括 Persona 在这期间新建的）和 hub 自己的字段原样带回；编辑只写用户改过的字段，Persona 在这期间从对话里改的（比如时间）留着。新建的 id 在 App 里生成（`rt_` + 8 位十六进制，hub 只给空 id 分配），回包丢了再点「添加」不会建出第二个。读和写排队、一次一个，开始了的写不会因为离开页面被打断；开关先在界面上动，写失败按 hub 上的值恢复。写入或取消跟进失败先重读核对，hub 上已经是想要的样子就不算失败；正在编辑的定时任务在这期间被删了，提示「已经不在了」，不会把它写回去。
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
- 验证（2026-11-02）：单测 `ProactiveStoreTest`（整表 PUT 带回其他定时任务和 hub 字段、开关立即生效和失败恢复、回包丢失不算失败、编辑只写改过的字段、新建重试不重复、被删的不写回、删除、取消跟进和失败、立即运行跟进和定时任务、读取失败保留旧数据、传输层的取消算失败）、`ProactiveTextTest`（规则、时刻和偏移、倒计时、晚了多久、错误、上次运行、跟进去向、时间线和那句话、页面键）。模拟器上连假 hub 把开关、编辑保存、长按立即运行、取消跟进、立即运行跟进、模板新建、删除、未保存返回都点了一遍，hub 收到的内容都对；真实外壳里点过设置 › 助手 › 主动做事、返回、运行记录打开对话（落到 Persona tab）、Persona 抽屉「定时任务」打开和返回；三套风格 × 深浅色、手机和平板都截图看过。真机和真实 hub 没有测；页面性能未实测。
- 设计系统里没有时间选择和周几选择的组件：这两个件写在 `proactive/ProactiveComponents.kt` 里，只用 token（色板、字体、`shapes.chipShape` / `controlShape`、`Tokens.Space`），不按风格分支；以后设计系统有了对应组件再换过去。

## 终端与链接（主线旧 App 移植，2026-11-02）

主线旧 App（单模块）在终端和链接上的功能按新架构补齐，交互和取舍以 iOS 为准。代码在 `feature/terminal`（`README.md`）、`core/terminal`、`feature/chat`（`LINKS.md`）和外壳的 `app/…/ui/ShellLinks.kt`。

终端：

- 键栏多一个「⇧Tab」（Claude Code 切模式，一个键而不是 ⇧ 再 Tab）。所有键是设计系统的 `JKeyCap`：Ctrl / Alt / ⇧ 点一下只管下一个键（着色加描边），按住时实底，快速双击锁住（实底加下划线），和 iOS 一样。
- 接管 / 只读记在手机上（SharedPreferences `jarvis-terminal`，不跟配对走），下次打开沿用；「接管终端？」弹框里选只读只管这一次。
- 只看和读屏降级时也能按键（2026-11-02 拍板）：键栏、键盘、粘贴走 `POST /api/v1/sessions/{m}/{s}/keys`，同一时间一个请求，在途时打的字攒进下一个（先文本后键），发完立刻再读一次屏幕；发失败时丢掉排着的键并提示「按键没有发出去」。滚轮和点击仍只在接管时有。
- 接管时滚出屏幕顶的行攒在手机上（最多 3000 行，`ScrollbackGrid`）：「选择文本」里能看到，node 不支持 `terminal_scroll` 时手指滑动在这些行里本地翻。键盘收起、窗格变高后程序重画出的行会从攒下的里去掉。只看时视图贴着最后一行（以前从第 0 行画，长的镜像屏幕先看到最上面的旧内容）。
- 字形：框线（含圆角、虚线、半线）逐格画，⏺ ⏸ ⏵ ⏹ ⏴ ● ■ ▶ ◀ 按形状画（打包的 JetBrains Mono 没有这几个，系统回落到 emoji 字体会画成两格宽的彩色图），✔ ✳ 等加 U+FE0E，OSC 8 链接画半透明下划线（点了仍是终端单击，iOS 会打开链接，这里没做）。
- 选择文本：顶栏拷贝按钮，整页纯文本，等宽不折行、横竖滑、「全部拷贝」。没做 iOS 那样在格子上直接选。
- 显示面板（顶栏右边）：字号 7–20、步长 0.5、默认 15，单位 dp（不跟系统字体大小，接管时列数稳定），存在手机上；接管时换字号就是换网格。自动换行开关从顶栏挪到了这里。
- 接管中、只看改在标题下的一行说明（「正在接管…」「电脑 172×45」「每秒读一次屏幕」），不再在终端上方放提示卡：提示卡出现和消失会改终端高度，接管时窗格因此多重排两次。

链接：

- 预加载只取最后 40 条里 15 分钟内 Agent 回复的前 3 个链接，主机没有点的不算；池最多 4 个页面。
- WebView 渲染进程崩溃 / 被杀只丢掉那一页（卡片里「重新载入」），第三方 Cookie 打开，内存紧张时销毁不在屏幕上的页面，看过的页面先用磁盘缓存；⋯ 菜单加「分享」；选中菜单的项在 `onPrepareActionMode` 里加，排在「复制」前面。
- 对话以外的网页（外壳的卡片：知识库、Persona 首页之外的页面；收件箱打开的网页）选中文字只有「交给 Persona」：写一句要它做什么（可空），进 Persona 主对话并切到 Persona tab。收件箱网页经 `feature/inbox` 的 `LocalInboxWebSelection` 由外壳提供。
- 对话里的 `jarvis://session|tab|assistant|inbox` 在 App 内跳转（外壳经 `LocalChatAppLinks` 接到导航），`jarvis://pair`、`jarvis://open` 不认；`mailto:` / `tel:` / 其他 App 的链接先确认再交给系统，`intent:` 按旧规则清洗；危险协议不打开。
- `<jarvis-context>` 和 iOS 逐字一致（正文不转义，只去掉 `</jarvis-context>`；多个之间一个换行），只带上下文时正文「参考上面的内容。」，「AI 修改」没写要求时「按上面选中的内容修改。」，「交给 Persona」没写时「看看上面这段。」。

验证：单测 `core:terminal` 的 `BoxDrawingTest`、`ScrollbackGridTest`（iOS 的录制帧 `apple/PersonaTests/Fixtures/terminal-control-scroll.jsonl`），`feature:terminal` 的 `TerminalPortTest`、`TerminalCriticalPathTest`，`core:hub` 的 `HubTerminalTransportTest`，`feature:chat` 的 `ChatLinksTest`。假 hub：

```sh
# 接管、攒行（每 0.7 秒写一行）、键盘；只看时的 /keys 记在 GET /mock/keys
MOCK_TERMINAL_STREAM=1 MOCK_TERMINAL_CONTROL=1 MOCK_TERMINAL_SCROLL=1 MOCK_TERMINAL_INTERVAL=0.7 PORT=<端口> python3 scripts/mock/mock_hub.py
# 只看时镜像真实的 Claude Code 帧（圆角输入框、⏺、⏵⏵）
MOCK_TERMINAL_STREAM=1 MOCK_TERMINAL_CONTROL=1 MOCK_TERMINAL_FIXTURE=apple/PersonaTests/Fixtures/terminal-observe-permission.jsonl MOCK_TERMINAL_INTERVAL=0.2 PORT=<端口> python3 scripts/mock/mock_hub.py
# 各种协议的链接（第一个 Agent 对话最后一条回复）和 Jarvis 主对话 / 收件箱里的链接
MOCK_LINK_SCHEMES=1 MOCK_LINKS=1 PORT=<端口> python3 scripts/mock/mock_hub.py
```

终端页直达：`--es debug.openLink 'jarvis://open?target=terminal&machine=macbook&session=<会话 id>'`。2026-11-02 在模拟器上用上面两个终端开关看过接管 / 只读、修饰键三态、选择文本、显示面板、字号 7 / 15 / 20、只看时键盘走 `/keys`，三套风格 × 深浅色；链接部分没在模拟器上看（各车道停止构建后才写完），选中菜单的实际顺序、渲染进程崩溃后的恢复、拼音输入要在设备上确认。

## 机器、引擎、模型配置与新建 Agent

2026-11-02 从主线旧 App（单模块）按行为移植过来（分组 E：机器页、引擎、Claude Code 设置、模型配置、异步新建、预填的新建页、设备页的已撤销分组）。

- **机器页**（Agents › 列表设置 › 机器监控 › 点一台，或树里电脑栏头的仪表按钮；`MachinesRoute` / `MachineDetailRoute`）：
  - 卡片上的部署行：在线且 `node_mismatch` 或 `deploying` 时出现，「重新部署」= `POST /api/v1/machines/{id}/deploy`；进行中、上次失败（`deploy_error`）跟着 `machines` 推送变，发出失败提示「部署失败：原因」（409 的原因原样显示）。列表和详情是同一张卡片。
  - 「引擎与连接」一组：「引擎」打开引擎选择（`EngineRoute`）；「重新连接」= `POST …/reconnect`，之后最多等 15 秒看这台变成在线，等的时候每 3 秒读一次机器列表（推送丢了也能等到），提示「已连上」「还没连上：<机器的 error>」「重新连接失败：原因」。脚注写接入方式（hub 所在的机器 / hub 通过 ssh <别名> 接入）。
  - 「启动器 N」：`GET …/launchers`，名字等宽、模型取路径最后一段；机器离线不读，上线后自动重读，读失败显示原因。
  - 历史曲线多一张磁盘图（历史里是已用字节，按最新一次的磁盘总量算成百分比，总量未知时不画）；系统信息多开机于、herdr、Claude Code、Persona node 版本。hub 上已经没有这台机器时页面写明。
  - 代码：`feature/machines` 的 `MachineActions.kt`（重连 / 部署 / 启动器，有单测）、`MachineManagementSections.kt`；接口 `core:contracts` 的 `MachineManagement`，实现 `core:hub` 的 `HttpMachineManagement`（走配对连接的 `request`），`app/…/di/MachineManagementModule.kt` 每次调用取当前配对。体验模式没有这些操作。
- **引擎**（[engines.md](engines.md)）：
  - 配对完成后、准备清单之前插入必选的一步（`PairingScreen`）：hub 所在的机器（`kind: local`，没有就第一台）报了 `engines` 才有；没有返回键、不能跳过，保存失败（「保存失败：原因」+「必须选一个引擎才能继续……」）停在本页；预选现在的引擎（没选过是 herdr），不按检测结果猜，推送更新会跟着动，直到用户自己改过。
  - 进程被杀后能恢复：配对时把要问的机器记在 `ReadinessRecords`（`engine-pending`），App 重启后 `OnboardingScreen` 先显示这一步再进准备清单；读不到那台机器时可以重试或「先继续」。
  - 机器页的「引擎」打开同一页（`EngineSelectionScreen`，`EngineRoute` 由 `settingsDetailEntries` 注册）；成功提示「已切换引擎」/「已保存，连上后生效」，返回的 Machine 立刻替换列表里那台（`SessionsRepository.applyMachine`）。规则（选项、能不能选、检测文案、脚注）在 `core:domain` 的 `engines/Engines.kt`。
- **Claude Code 设置**（设置 › 电脑 › Claude Code 设置，`SettingsPage.ClaudeCode`，深链 `page=claudecode`）：选一台在线机器（第一次是 hub 所在那台，离线了退回第一台在线的）和「同时改所有在线机器」（默认开），两项记在本机；开关 / 选项 / 文本三种行，没设过标「默认」，文本清空 = 发 JSON null 回到默认；同时改时逐台写，全部成功提示「已应用到 N 台机器」，有失败逐台列出并重读。
- **模型配置**（设置 › AI › 模型配置，`SettingsPage.ModelConfig`，深链 `page=modelconfig`）：hub 的用途目录按分组列出、可搜索（名称、分组、id、模型），每行写生效的供应商和模型、状态或继承来源；一个用途一页（`ModelPurposeRoute(purposeId)`，语音页可以直接打开 `plugin.<id>`）：当前生效、供应商选择（隐藏的标「已隐藏」，引用了不可用的标「当前引用不可用」，换供应商清掉模型和思考强度）、具体模型、思考强度（Messages API 的供应商不显示）、恢复继承；保存是带 revision 的 PATCH，空值发 null，409 时保留编辑、提示「配置已在别处修改……」并只给「重新加载（丢弃编辑）」。旧 hub（路由 404、没有 JSON 错误）提示先更新服务器，hub 有配置但没有这个用途时说没有。插件用途的「具体模型」暂时是文本框：语音插件的模型目录接口随语音模块（G 组）接入后再换成选择。
- **新建 Agent**：
  - Claude 一律异步新建（`async: true`）：hub 立刻回一个 `starting` 的会话，先并进会话列表（`SessionsRepository.adopt`），对话页马上打开；第一条消息不放 `prompt`，改带 `name_hint` 起标签名，消息本身当普通消息走待发队列，它的 id 和启动的操作 ID 一起存在启动记录里（`AgentCreationStore`），回执丢了、进程重启后核对时用同一个 id 入队，只发一次。Codex 照旧同步新建、第一条消息放 `prompt`（必填）。
  - hub 有「新建 Agent」用途（`agent.new`）时默认「跟随模型配置」（发空 launcher），不再自动带上次的启动器；在「本次启动器」里手选一个是这一次的覆盖。旧 hub 照旧选启动器；读不到这个用途时提示原因，要先选启动器或重新加载。
  - 对话页输入框上方：「正在启动 Claude，消息会在它就绪后发出」/「没有启动起来：原因」（`ChatStartingNote`）。
  - `NewAgentRoute(machineId, cwd, prompt)` 是同一张表单的页面版，给知识库的「开 Agent」：那台机器在线时预选机器和目录（都能改），不在线就选别的并写明；开好后页面关掉，对话在 Agents 打开。
- **设备**（设置 › 设备）：已撤销的凭据单独一组（灰，「撤销于 …」），没用过的设备显示「添加于」，写明凭据的意思；hub 不支持设备凭据（404）、没有数据库（503）时直接说明。
- 调试直达：`--es debug.openPage machine:<机器 id>`、`engine:<机器 id>`、`purpose:<用途 id>`、`new-agent` 或 `new-agent:<机器 id>:<目录>`（预填）。配对后的引擎一步要用配对链接走配对页（`debug.mockAddress` 是静默配对，看不到这一步）。
- 假 hub 开关：`MOCK_NODE_MISMATCH=1`（Mac Studio 的 jarvisd 不是 hub 这一版）、`MOCK_DEPLOY_ERROR=<原因>`（重新部署失败）、`MOCK_ENGINE_ERROR=1`（保存引擎 503）、`MOCK_MODEL_CONFLICT=1`（保存模型配置 409）、`MOCK_START_SECONDS=<秒>`（新会话「正在启动」多久）、`MOCK_START_ERROR=<原因>`（到时间后启动失败，对话页显示「没有启动起来」）、`MOCK_MACHINES=1`（离线的实验室主机，重新连接后上线）。
- 单测：`core:contracts` 的 `MachineManagementContractTest`；`core:hub` 的 `HttpMachineManagementTest`、`HttpModelConfigurationTest`、`AgentCreationModelTest`、`DeviceCredentialsTest`；`core:data` 的 `SessionsAdoptTest`；`core:domain` 的 `EnginesTest`；`feature:machines` 的 `MachineActionsTest`；`feature:settings` 的 `SettingsMachinesTest`（引擎选择、Claude Code 设置、模型配置）、`DeviceCredentialsStateTest`；`feature:agents` 的 `NewAgentAsyncTest`（第一条消息只发一次、跟随模型配置、Codex、预填）；`feature:chat` 的 `ChatStartingNoteTest`。

## 语音对话（hub 语音插件）

和 hub 上的语音插件全双工对话（契约见 [plugins.md](plugins.md)「语音」）：页面开着就一直听，Persona 说话时直接开口就能打断。代码在 `feature/voice`，传输在 `core/hub` 的 `HttpVoicePlugins`，契约 `core/contracts` 的 `VoicePlugins`，路由 `VoiceRoute(pluginId, title)`。

- 入口：模块对外只有两样。`voiceEntries(navigator, plugins, identity, onModelConfiguration = null)`（回调参数是用途 id） 注册 `VoiceRoute`（`Root.kt` 里和机器监控同样写法，体验模式没有这份服务，打开是说明页）；`rememberVoicePlugin(plugins, active)` 在 `active` 为真时取一次 hub 上第一个 `kind == voice && status == running` 的插件（404、失败都当没有，配对换了重取），宿主据此决定出不出现「语音对话」入口（Persona 标题菜单，由宿主接）。服务是 `ShellSession.voice`，和 `metrics` 一样每次调用按当前选路、钉扎的客户端和 token 建 `HttpVoicePlugins`，配对换了就取消。
- 权限：第一次进页面申请 `RECORD_AUDIO`（另声明 `MODIFY_AUDIO_SETTINGS`）。没给时是空状态「语音对话要用麦克风 / 只在这一页打开时听，离开就结束，不在后台录音。」，拒绝后「没有麦克风权限 / 语音对话用不了。可以在系统设置里给 Persona 打开麦克风。」，按钮「允许使用麦克风」：系统还会问就再申请一次，已经不再问（拒绝两次）就打开系统里这个 App 的设置页，回来时重新检查。
- 模型：读模型配置的用途 `plugin.<id>`（`VoicePlugins.modelChoice`）和插件的 `/models`。状态（`status`）有值且不是「未验证」：「语音模型不可用」加原因；配置的模型不在插件目录里：「配置的语音模型不在插件目录中，请在模型配置里重新选择。」；旧 hub（404）用插件默认模型（`/models` 的 `default`，没有就不带 model）；别的读取失败显示「读不到语音模型配置」和原因，只能重试，不当成没有配置（[model-config.md](model-config.md)「生效边界」）。模型只在开始或重新开始时读。模型配置页不在这个模块：宿主传了 `onModelConfiguration(purposeId)`（参数是用途 id `plugin.<插件 id>`，`voicePurposeId`；宿主接到模型配置单项页）才有「模型配置」入口（右上角菜单和「模型不可用」页的按钮），没传时右上角直接是「音色」按钮，「模型不可用」页只有「重试」。
- 音色：右上角「音色」打开浮层，列出这个模型的音色（「女声 / 男声 · 中文、英文 · 默认」），当前的打勾，点一下选中并关闭；拿不到列表时「音色列表没拿到：…」，对话照常用上次选的音色。选择按配对存在本机（配对目录 `hub-data/<配对>/voice.json`，每个插件一份，取消配对时随目录删掉）；插件已经没有这个音色时用默认的，但记录不删（换回别的模型可能还有）。换了音色或模型就重开一次对话。
- 页面：顶栏插件名、副标题「模型 · 音色」；中间的光球（设计系统 `VoiceOrb`：说话时快跳，听时慢呼吸并跟着音量放大，连接中转圈，静音和结束变灰；玻璃是一颗发光的球，晨报是墨色圆盘、说话时荧光笔圈住，看板是状态色方块加五格电平）；状态行（`VoiceStatus`）「正在连接… / 在听 / 正在说，直接开口就能打断 / 已静音 / <工具>… / 对话已结束」，出错时下面一行红字；字幕（`VoiceCaption`）自己的在右边气泡，Persona 的是正文，没定稿时变淡，跟着滚到最新；底部三个带字圆按钮（`VoiceControl`）「静音 / 取消静音」「打断」（只在正在说时可点）「挂断」，结束后左边换成「重新开始」。玻璃的背景光跟着对话变：听时蓝，说时薄荷绿。
- 协议（`VoiceProtocol.kt`，有单测）：连上发 `{"type":"start","model":…,"voice":…}`（选了才带）；`ready` 之后才开录音和播放，采样率以 `ready` 为准（缺省 16000 / 24000，不合理的值也按缺省）；上行 20 ms 一帧 PCM16 单声道二进制，静音时照送全零；下行二进制立即播；`transcript` 同 id 原地替换、空白的新行不加；`tool` 可以同时开几个，显示最新未完成的；`clear` 清掉没播完的；`error` 显示，`fatal` 结束；「打断」发 `{"type":"interrupt"}` 并清播放。WebSocket 没有读超时、15 秒 ping。
- 结束和出错：离开页面、挂断、App 进后台（`ON_STOP`）都结束对话，不在后台录音；对话期间屏幕常亮，结束后放开。关闭码 1001（插件重启）或对话中断线：「连接断了（语音服务可能刚重启，或网络不稳），点下面的按钮重新开始」；升级被拒 404：「hub 上没有语音插件」；开始前连不上：「连不上语音服务：<原因>」（原因按 HTTP 状态和网络错误说成中文：403 是只读设备，5xx 是语音服务没有响应）；麦克风打不开或中途没了：「麦克风打不开」；插件带原因的其他关闭码显示原因。
- 音频（`AndroidVoiceAudio`）：`VOICE_COMMUNICATION` 音源 + 系统回声消除 / 降噪（外放时模型不会听到自己），`MODE_IN_COMMUNICATION`，有线 / 蓝牙 / USB / LE 耳机或助听器优先，否则外放扬声器（不是听筒）；申请通话类的短暂音频焦点（别的 App 的音乐会暂停）；结束时还原音频模式、通信设备和焦点。音频硬件在 `VoiceAudio` 接口后面，状态机（`VoiceSession`）和页面逻辑（`VoicePage`）在 JVM 单测里用假音频、假插件测。
- 和旧 App 的差别：进后台就结束对话（旧 App 页面还在就一直录）；申请音频焦点；助听器也算耳机；拒绝过两次后按钮去系统设置；音色按配对存在配对目录（旧 App 是全局 SharedPreferences）；旧 hub 不再读本机的旧模型选择（新 App 从来没有）；没有模型配置入口时右上角直接是「音色」，不放只有一项的菜单。

截图：假 hub 的 `MOCK_VOICE=1` 提供一个运行中的语音插件、它的音色和模型、模型配置里的 `plugin.voice`，以及按脚本说话的 WebSocket（ready 后 1.5 秒用户字幕、4 秒工具「查日程」、7 秒 Persona 开始说并持续发静音音频、11 秒工具结束、30 秒说完，收到 interrupt 回 `clear`）。别的模式：`drop`（14 秒时用 1001 关闭）、`bad-model`（模型配置说不可用）、`missing-model`（配置的模型不在目录里）、`old-hub`（没有 `plugin.voice`，404）、`error`（ready 后来一个 fatal 错误）、`off`（插件不在运行，WebSocket 404）、`slow`（start 之后 8 秒才 ready，一直「正在连接…」）。`GET /mock/voice?mode=<模式>` 运行中切换模式（重启假 hub 会换 hub_id，配对就失效了），`GET /mock/voice` 看 App 发过的 start、interrupt 和音频帧数。模拟器按规矩用 `-no-audio` 启动，没有麦克风可开，调试包加 `--es debug.voiceAudio fake` 让语音页用假音频（静音上行、丢弃下行、电平是一个缓慢变化的假值），正式包没有这个入口：

```sh
MOCK_VOICE=1 PORT=<端口> python3 scripts/mock/mock_hub.py
adb -s <模拟器> reverse tcp:<端口> tcp:<端口>
# 配对见上文「App 外壳与页面直达」（debug.mockAddress），之后：
adb -s <模拟器> shell pm grant <包名> android.permission.RECORD_AUDIO
adb -s <模拟器> shell am start -n <包名>/persona.android.debug.DebugEntryActivity \
  --es debug.openPage voice --es debug.voiceAudio fake --es debug.style glass --es debug.theme dark
```

`debug.openPage voice` 打开插件 id 为 `voice` 的语音页（落在 Persona tab）。组件目录（调试版）有「语音对话」一节，几种光球、状态行、字幕和按钮都在。

验证（2026-11-02）：单测 `VoiceProtocolTest`（旧 App 的 `VoiceTest` 全部改写过来，加上采样率、缺字段的帧、工具、电平）、`VoiceTextTest`（状态行、光球、音色说明、各种失败的文案、模型规则、声音路由）、`VoiceSessionTest`（ready 之前不录不播、start 只发一次、静音照送全零、字幕和工具、clear 和打断、非致命和致命错误、1001 和断线、开始前失败、麦克风打不开、正常关闭、停了以后什么都不认）、`VoicePageTest`（旧 hub 用插件默认模型、不可用不换别的模型、读不到配置不当成没有、音色按配对保存并重开、插件没有的音色不发、音色列表拿不到照常对话、重新开始重读配置、进后台结束）、`VoicePrefsTest`，`core:hub` 的 `HttpVoicePluginsTest`（MockWebServer：插件列表、音色、模型、模型配置、配对换了取消、WebSocket 双向、关闭只报一次、升级被拒带状态码）。模拟器（API 35，`-no-audio`，假音频）连 `MOCK_VOICE` 假 hub：三套风格 × 深浅色截了没有权限、被拒、连接中、在听、用户字幕、工具、正在说（带字幕和工具）、说完、已静音、音色浮层、断线结束、模型不可用，玻璃深色另有配置的模型不在目录、致命错误、没有语音插件、旧 hub；点过打断（假 hub 收到 interrupt、播放清空）、选音色（新开对话带上音色、`voice.json` 写进配对目录）、进后台再回来（对话已结束）、重新开始、挂断，对话中窗口带 `KEEP_SCREEN_ON`，挂断后去掉。真机才能验的：真实录音和回声消除（外放时模型会不会听到自己）、耳机 / 蓝牙的切换、音频焦点、和真实语音插件的延迟和打断手感；这些都没测，延迟未实测。

## Persona 页（现在要你处理的事、对话历史、交接对话的标题）

2026-11-02 从主线旧 App（和 iOS 的 `ShellNeedsYou`）移植过来。代码在 `feature/assistant`：`NeedsYou.kt`、`AssistantData.kt`、`AssistantScreen.kt`；宿主的数据和跳转在 `app/…/ui/Root.kt`、`app/…/navigation/NeedsYouLinks.kt`。

- 现在要你处理的事：Persona 首页（新对话页）的问候下面、建议卡上面（已拍板：和建议卡并存）。先列等你的 Agent（待处理的提问、计划、授权、终端对话框；不算 Persona 自己的对话，按 hub 的屏蔽规则去掉被屏蔽的会话和空间，空间里单独放行的照样算），按 hub 给的顺序；再列收件箱「需要你」里没处理的事，新的在前。最多 3 行，多出来的写成一行「还有 N 件在收件箱」（N 也算多出来的 Agent，和 iOS 一样），点了去收件箱；一件都没有就整节不出现。每行：左边一个标记（Agent 按请求的种类、用「需要你」的颜色；收件箱按飞书 / 邮件 / 文档、灰色），标题是会话名（没有就目录最后一段，再没有「Agent」）或消息标题，副标题是请求的一句话或消息的摘要（没有摘要用预览），都是单行。点 Agent 的一行打开它的对话（落在 Agents tab），点收件箱的一行打开收件箱列表会打开的那一页（飞书会话或邮件）。纯函数 `needsYou(...)`，单测 `NeedsYouTest`、`NeedsYouLinksTest`。
- 对话历史（抽屉、宽屏的侧栏）：每一行的 ⋯ 和长按是同一个菜单：「置顶 / 取消置顶」（主对话没有）、「拷贝会话 ID」、在跑的对话还有「结束对话」。置顶存在 hub 的 pins（已拍板，和 Agents 共用一份，所以 iOS / 旧 App 存本机的做法没搬）；「置顶」组标题的 ⋯ 里有「全部取消置顶」，只取消 Persona 自己的对话，不动 Agents 的置顶。「最近」组先列 30 个，其余收成一行「查看全部 N 个」（N 是这一组的总数），点开后全部列出；搜索时列出全部结果。
- 标题菜单（点标题「Persona」）：助手设置、知识库、语音对话（hub 有运行中的语音插件时，见上文「语音对话」；每次切到 Persona tab 且连着 hub 时问一次 `GET /api/v1/plugins`）、会话信息，打开着对话时再加一条「拷贝会话 ID」。拷贝后提示「已拷贝会话 ID」。
- 交接对话的标题：从「交给 Persona」开出来的对话，第一句是 `<jarvis-context … title="X">` 标签，hub 按它起标题；历史里显示「交给 Persona：X」（解码 `&quot; &lt; &gt; &amp;`），标签没有 title 时显示「交给 Persona」。没有标题时取最后一句的第一行，先认标签再截到 30 个字（旧 App 先截断，长标题的标签就认不出来了）。单测在 `AssistantTest`。
- 截图：假 hub 默认就有 5 个等你的请求和几件「需要你」的收件箱（三行 + 还有 N 件）；`MOCK_NEEDS=two` 两行、`MOCK_NEEDS=none` 整节不出现；`MOCK_HISTORY=1` 让历史很长（最近 30 个 + 查看全部）、多一段交接对话和一个置顶，并且保存 App 写的置顶。Persona tab 默认打开主对话，要看首页：点左上角「对话列表」→「返回首页」。

## 大屏两栏：可拖的分隔条、空间页、横放的左右留白

2026-11-01 用户要求「两栏中间能拖、右栏别太小」「别只针对我的机型」，旧 App 做过（`ui/shell/Fold.kt`、`Panes.kt`）；2026-11-02 移植到新外壳。

- 接法：material3-adaptive 1.3.0 的 Navigation 3 list-detail 场景（`rememberListDetailSceneStrategy`）带 `paneExpansionState` 和 `paneExpansionDragHandle` 两个参数，每个 tab 一份（`app/…/ui/PaneDividers.kt` 的 `rememberShellListDetail`）。库自带的 `paneExpansionDraggable` 松手时只会吸到锚点（有锚点就一定吸到最近的一个），做不到「离折痕 28dp 以内才吸，否则停在松手处」，所以拖动是自己写的：把手的 `draggable` 每一帧直接写 `PaneExpansionState.setFirstPaneWidth`，只重排两栏、不重组页面。
- 规则（纯函数在 `app/…/navigation/PaneSplit.kt`，单测 `PaneSplitTest`，数值照旧 App）：最窄左栏 280dp、右栏 320dp；没拖过时左栏约 38%，夹在 320～420dp，右栏能留 400dp 就留（771dp 的内屏是 320 / 451，平板横放 1280dp 是 420 / 860；原来是 Material 的固定宽度）；拖的时候左栏跟手，经过平放的折痕轻震一下，松手时离平放折痕 28dp 以内吸上去，否则停在松手处；位置按「占宽度的比例」每个 tab 存一份（SharedPreferences `jarvis-panes` 的 `split.<tab>`，存在本机、不跟配对走，和界面风格一样），窗口宽度变了按比例换算再夹回最窄宽度。
- 收起左栏：只有 Agents，而且旁边开着东西（对话、空间页）时，往左拖过左栏最窄宽度 64dp 以上松手就收起，右栏铺满；把手留在左边缘（变成强调色），拖回来或点一下就展开；右栏关掉（返回）时列表自动回来。收件箱和设置不收（和旧 App 一样）。
- 分开的折痕（书本式半折、双屏有缝）：两栏固定分在折痕处、不出把手（交还给库的 hinge 避让：expansion state 一旦设了宽度就会盖过 `excludedBounds`，所以这时要 `clear()`）；放平后回到拖过的位置。离边太近的折痕不算。
- 把手（设计系统 `PaneDivider`，组件目录「两栏分隔条」一节）：线上一颗小把手，左右各伸出 8dp 都能按住拖；鼠标 / 触控板悬停是左右箭头光标；拖动中变宽变深，快要收起时和收起后是强调色。玻璃是一颗半透明的墨色药丸，晨报是一道细墨线，看板是带描边的小格。TalkBack：「调整左右两栏的宽度」，自定义操作「左栏变宽 / 左栏变窄」（每次 64dp）、可以收起时「收起左栏」、收起后「展开左栏」。
- 空间页进右栏：空间页从盖住整个窗口的 `JFullScreen` 改成路由页 `SpaceRoute(spaceId)`（`feature/agents/…/SpaceEntries.kt`，`ShellPanes.isDetailDestination` 把它算作详情）：宽屏在列表右边打开，手机推入；在空间页里点的会话压在它上面，返回回到空间页。空间页自己建一份 `AgentsViewModel` 和新建会话的 `NewAgentViewModel`（按目的地各一份，菜单和对话框沿用列表的）。
- 横放时的左右留白：手机横放时摄像头挖孔或三键导航条在左边 / 右边，edge-to-edge 下外壳会画到它们下面。外壳的内容（导航栏、两栏、悬浮坞）按 `WindowInsets.safeDrawing` 的左右两边留白，背景（`PageBackdrop`）照样铺满；用 `windowInsetsPadding` 留白会把左右两边消耗掉，页面里再用 `navigationBarsPadding()` 不会重复留。
- 截图要看：书本式折叠屏内屏平放（默认位置、拖到折痕附近吸附、拖到最左收起、收起后点一下展开）、半折（`debug.posture book`，固定在折痕、没有把手）、三折屏、平板横放、横放手机，三套风格 × 深浅色；手机横放挖孔在左 / 在右、三键导航条在侧边。拖动手感要在折叠屏 / 平板模拟器上手试。

## 对话里的文件和 Git

Agent 对话页 ⋯「文件」「Git · N」打开会话目录的文件和 Git（只读，`feature/files`）。它是对话页里的一层，不是导航页：返回键先关预览，再在文件夹里逐级往上（只到面包屑的顶），最后回到对话；顶栏左边的返回直接回对话。助手对话没有这些入口。接口是 hub 转给那台机器 node 的 `GET /api/v1/machines/{mid}/fs|file|git/status|git/diff|git/log|git/show`（`core/hub/.../HubSessionFiles.kt`）。

- 按路径直接打开：`SessionFilesScreen(..., initialPath = 路径)`（参数表最后一个，可选）。给消息里的文件标签、工具卡片的路径和 diff 文件头用：相对路径按会话目录补全，`~` 开头的原样交给 hub（只有 hub 知道 home 在哪）。是文件就直接打开预览，返回直接回对话、不先退到目录列表；路径以 `/` 结尾，或者 hub 回 `is_dir`，就在浏览器里打开那个文件夹，它就是面包屑的顶，从那里返回也回对话。会话目录还没到时，绝对路径和 `~` 路径也能打开。
- 浏览：面包屑是一排 chip（当前一级实心），进去时自动滚到最后；⋯「上一级文件夹」可以一直走到 `/`，面包屑的顶跟着往上；「显示隐藏文件」存在本机（SharedPreferences `jarvis-files`，不跟配对走），隐藏文件的名字变淡；目录在前，大小和相对时间（刚刚 / N 分钟前 / N 小时前 / N 天前 / 日期），文件是 Git 字母、文件夹是「有改动」（里面有冲突时是琥珀色）；去过的文件夹先显示上次的列表再刷新。读不出来时显示 node 自己的原因（macOS 不让 jarvisd 读「桌面」时，说明要在哪里授权），网络问题才说「连不上 hub」；刷新失败时保留上次的列表。
- 预览：Markdown 默认渲染（README 里的 `<img>` `<a>` `<br>` `<p>` 先转成 Markdown，裸链接在第一个非 ASCII 字符处结束），⋯ 切「查看源文件 / 渲染 Markdown」；Markdown 里指向同一台机器上文件的链接在文件页里打开，图片经 hub 读那台机器上的文件（网页图片不取，App 没有图片的网络栈）。长文档按块懒加载。代码和文本等宽带行号、两个方向滑；图片可以缩放；PDF 逐页渲染（`PdfRenderer`，按显示宽度在后台画）；其他「无法预览」并写明类型和大小。顶部一行是大小，超过 2 MB 时写「只显示前 2 MB」。
- ⋯ 菜单：拷贝路径、拷贝内容、分享、刷新。分享：文本按文本分享（超过 10 万字符改成文件，选择器经 binder 传，太大会崩）；图片、PDF 等写进 `cache/files-share/`，经 `FilesShareProvider`（authority `${applicationId}.files.share`）分享；截断了的二进制文件不给分享（打不开）。拷贝都经 `LocalAppFeedback` 提示「已拷贝路径」等。
- Git：分支卡（上游、已同步 / ↑↓、贮藏 N、仓库根）；「改动」标题右边是 +N −N 合计，每行文件名、文件夹（改名时「← 原路径」）、字母、暂存状态、± 或「二进制」，点开看 diff（两半都有时「未暂存 / 已暂存」切换）；「提交历史」每行两行标题、短哈希、作者、相对时间、「HEAD」和分支 / 标签 chip（本地分支突出，远端分支和 tag 是中性的；远端名取自上游，`feat/x` 这种带斜杠的本地分支也认得出），30 条之后「加载更早的提交」。
- 提交详情：标题、说明、作者、完整日期「yyyy年M月d日 HH:mm」、完整提交号（点了拷贝，提示「已拷贝提交号」），「N 个文件」列表，点一个文件看它那一段 diff（`git show` 每个提交只取一次，按 `diff --git` 拆开，路径从行尾取、空格保留，改名按新旧两个名字都找）。整个提交的 diff 超过 512 KB 被截断时，截断之后的文件说「改动太大，没有带回来」，被截在中间的那个文件写「改动太大，只显示了前 512 KB」。diff 页不再显示 `diff --git` / `index` / `---` / `+++` 这几行文件头（标题已经说了是哪个文件；hunk 里以 `---` 开头的是被删的行，照常显示）。
- 时间：node 发的时间带机器自己的时区偏移（`2026-10-31T13:58:40.205893083+08:00`），Android 12 / 13 的 `Instant.parse` 不认，这里统一用 `fileTime`（`OffsetDateTime`）；Go 的零时间（`0001-…`）当没有。
- 代码：`feature/files/src/main/kotlin/jarvis/feature/files/`：`FilesScreen.kt`（入口和浏览器）、`FilesState.kt`（`FilesController`：绝对路径的浏览、页面栈、返回、提交缓存）、`FilePreview.kt`、`FileMarkdown.kt` / `FileMarkdownText.kt`、`PdfPages.kt`、`FileShare.kt`、`GitPages.kt` / `GitText.kt`、`FilesPaths.kt`（路径、大小、时间）、`FilesErrors.kt`、`FilesPreferences.kt`。模块依赖多了 `:core:hub`（`FilesErrors.kt` 读 `HubFailure` 里 node 给的原因）、Markdown 渲染库和 `androidx.core`（FileProvider）。单测：`FilesControllerTest`、`FilesPathsTest`、`GitModelsTest`（按真实 node 回包的形状：带偏移的时间、零时间、refs、改名 / 冲突 / 未跟踪的字母）、`FilesCriticalPathTest`。
- 假 hub（`scripts/mock/mock_hub.py`）：浏览树（`MOCK_TREE`，一直有）里有 `docs/guide.pdf`（两页，脚本里生成）、第三个提交带 tag；home 以上的文件夹（`/`、`/Users`）能列出来；`~/Desktop` 回 node 的 macOS 授权说明（看加载失败）。`MOCK_FILES=1` 时 `…/file` 先由大文件集合回答，不在里面的回落到浏览树（PDF、图片、文件夹也能打开）。`MOCK_TIME_OFFSET=+08:00` 让文件和提交的时间带这个偏移（和真实 node 一样），默认是 `Z`。
- 截图：起假 hub（`PORT=<端口> MOCK_FILES=1 MOCK_TIME_OFFSET=+08:00 python3 scripts/mock/mock_hub.py`），`adb reverse` 后用 `DebugEntryActivity` 的 `debug.mockAddress` 自动配对，`debug.openSession macbook/a1f3c2d4-0000-4000-8000-000000000001`（会话目录 `/Users/me/code/weather`）打开对话，⋯ 里点「文件」。三套风格和深浅色用 `debug.style` / `debug.theme`；按文字点控件用 `uiautomator dump`（属性值里可能有 `>`，用 XML 解析，别用正则截 `<node …>`）。
- 验证（2026-11-02，模拟器 API 35 连假 hub）：上面的路径都点过一遍（隐藏文件开关、走到 `/` 再回来、加载失败、Markdown 渲染和源文件、README 里的图和指向别的文件的链接、分享文本 / 大文本 / PDF、PDF、图片、代码、Git、历史、提交、单文件 diff、拷贝提交号），三套风格 × 深浅色的手机尺寸和平板双栏都截图看过。`initialPath` 是在本地临时让对话页传路径点的（文件、`~` 路径、不带斜杠的文件夹、带斜杠的文件夹、读不到的文件，返回都回到对话），对话页的入口不在这个模块里。真机和真实 hub 没有测；PDF 渲染和长 Markdown 的性能未实测。

## 主线旧 App 新增、待移植到新架构

主线（`origin/master`）在 `81dc6928` 之后给旧单模块 App 加的功能和改动。本分支已经用多模块新架构重做了 Android，下面这些新架构还没有对应实现，逐项待移植，不混进上面的新架构描述。PR 号来自 `git log 81dc6928..origin/master --format='%h %s' -- android docs/android.md`。

- 待办（todos，#427）：侧边栏「待办」（带未完成数）、待办列表和详情、「交给 Agent」（新开或交给已有的，带 `todo_id`）、Agents 的「按待办」分组和关联 / 解除关联、对话和会话信息里显示关联的待办；hub 没有待办接口时入口隐藏。
- 邮件阅读和写信、设置 › 连接器（#320）：邮件详情（线程、锁住的 WebView 渲染 HTML、显示图片后才放行、打开即已读、加星标）、写信（回复 / 回复全部 / 转发、收件人校验、有内容返回要确认）、「交给 AI」和 AI 草拟回复；设置 › 连接器（这台手机、邮箱的添加 / 测试 / 删除、飞书的 lark-connector 状态）；Gmail 用 Google 账号登录，要 `config.json` 的 `android.google_oauth_scheme`（`OAuthRedirectActivity`），没配时提示改用应用专用密码。
- 飞书会话和removed，收件箱里点开进页面（#365）：飞书会话页（富文本、卡片、合并转发、回复、表情回应、话题、`start_position` 分页、已读上报、缓存最后一页）、removed帖子页、图片和表情缓存（`LarkMedia`）、飞书自己的色板和尺寸。
- 手机数据：Health Connect 的 `phone_health_summary`（#278）：读每日汇总（七种指标，按本地自然日切分），权限走 Health Connect 授权框，缺的指标列在结果的 `not_granted` 里；依赖 `connect-client:1.1.0-alpha08`，Android 8–13 要装 Health Connect App。
- 收件箱：在别处已经读过的事项不再标成 @all / 未读（#246）。
- 脚本目录调整（#505）：`scripts/` 按用途分了子目录，本文档统一写成 `scripts/app/android.sh`、`scripts/mock/mock_hub.py`。
