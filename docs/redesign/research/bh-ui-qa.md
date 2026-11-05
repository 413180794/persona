# removed 的界面设计语言、原型评审与测试验收（对照 Persona）

调研日期：2026-10-30。只读调研，没有运行任何构建、测试或脚本。

约定：
- 路径前缀：`BH:` = removed 仓库根目录，`JV:` = Persona 仓库根目录。证据格式为 `前缀:相对路径:行号`，或者写文档的章节名。
- **【事实】** 指代码或文档里能直接查到的内容；**【判断】** 是我的推断或建议。
- 隐私处理：原文里出现的主机名、IP、账号、群名、内部项目名、评审人一律不抄，用占位符代替。性能数字只引用文档里写明是实测的值，并注明出处。文中的 grep 计数是本次统计的「出现次数」，不是性能数据。

---

## 0. 一句话结论（先写，后面展开）

- removed 真正可借鉴的不是它的视觉 token（它的 `DesignSystem` 很薄，只有 4 个文件、约 300 行），而是三样东西：
  ① 一套「先做 HTML 可点原型 → 写评审文档 → 用户认可后原生落地 → 写验收记录」的流程；
  ② 对「结果待确认 / 待核对」这类不确定状态有统一的文案和图标，并且整个流程里都不做假成功、不偷偷自动重发；
  ③ 首次接入拆成几步：表单、密钥库、分段连接进度、逐跳核对指纹、可跳过的组件安装。每一步都说明会做什么、跳过会少什么。
- Persona 的 token 体系比 removed 完整：有风格 × 语义色、状态色表、圆角、间距、字体角色和动效。问题在于没用起来：`Theme.Font` 在视图里只出现 1 次，`Theme.Space` 也只出现 1 次。重设计时 token 应该从一份 JSON 生成 Swift 和 Kotlin 两端代码，再用脚本或测试守住采用率和对比度。

---

## 1. removed 的设计系统【事实为主】

### 1.1 模块边界与规模

- 架构文档给 DesignSystem 定的职责是「颜色、字体、间距、头像、徽标、加载与错误组件，以及通用内容块」，并且规定它不发网络请求（`BH:docs/ARCHITECTURE.md:45`、`:56`；依赖图见 `:28-43`，Features → DesignSystem）。
- 实际只有 4 个文件，合计约 297 行：`WorkspaceStyle.swift`（154 行）、`HerdrStyle.swift`（61 行）、`WorkspaceSheet.swift`（49 行）、`ServiceStatusView.swift`（33 行）（`BH:Packages/removedKit/Sources/DesignSystem/`）。**里面没有字体 token、间距 token，也没有圆角 token**，和架构文档写的职责对不上。
- 【事实，本次 grep，范围是 `BH:Packages/removedKit/Sources`】
  - 固定字号 `font(.system(size:` 出现 567 次，文本样式（`.headline`、`.subheadline` 等）出现 1781 次。
  - `cornerRadius:` 后面跟的字面量按出现次数排：10（78 次）、12（69）、7（34）、8（33）、9（26）、6（20）、5（11）、16（9），至少 8 档。
  - `Color(red:` 字面量 26 次；直接用系统 `.orange` 69 次、`.red` 34 次。
  - `WorkspaceStyle.` 被引用 830 次，`HerdrStyle.` 341 次。

### 1.2 Token

两套互相独立的调色板，按 tab 切换：

1. **WorkspaceStyle**（飞书风格的蓝灰，给消息、通讯录、工作台、我的这几页用），定义在 `BH:.../DesignSystem/WorkspaceStyle.swift:4-34`：
   - 固定色：`blue`、`green`、`red`。
   - 浅深两套的颜色：`canvas`、`field`、`line`、`outgoing`，`surface` 用系统背景色。
   - 用一个 `adaptive(light:dark:)` 函数把十六进制值转成跟随系统外观的颜色，同时兼容 UIKit 和 AppKit。
2. **HerdrStyle**（「森林绿 + 奶油白」，给 Agent 页用），定义在 `BH:.../DesignSystem/HerdrStyle.swift:4-17`：
   - 语义色更完整：`canvas`、`surface`、`field`、`raised`、`line`、`outgoing`、`blue`（实际是绿）、`green`、`ink`、`secondary`、`onAccent`、`orange`、`red`，全部有浅深两个值。
   - 注释写明「Herdr's palette is independent of the Lark gateway workspace chrome」（`:3`）。
- 按 tab 切换的方式（`BH:App/removedApp.swift:199-205`）：
  - tint 按 tab 换：Agent tab 用 `HerdrStyle.blue`，其余用 `WorkspaceStyle.blue`。
  - Agent tab 的深浅色不跟随系统：由 `@AppStorage("herdr.lightAppearance")` 决定，默认深色（`:41`、`:203`）。
  - 深浅色开关放在主机面板的工具栏里（`BH:.../AgentsFeature/AgentFleetView.swift:345-348`）。
- 字体：
  - 没有 token。页面框架（顶栏、tab、行）多用固定字号，例如 tab 图标 22pt、tab 文字 10pt（`BH:App/removedApp.swift:489`、`:498`），「我的」页标题 23pt（`BH:.../ConnectionsFeature/ConnectionsView.swift:164`）。
  - 正文和表单多用文本样式，比如 `.subheadline`、`.footnote`、`.caption`。
  - 【判断】为了和飞书对齐，有意在框架部分用了固定字号；代价是这些地方不跟随动态字体。
- 间距和圆角：
  - 全是字面量。常见的 padding 有 15、16、18、20。
  - 面板圆角主要是 `herdrPanel` 的 14、按钮的 12、行里的 10（`BH:.../HerdrStyle.swift:44-61`）。
- 原型里的 token：三个 HTML 原型各自在 CSS 变量里重抄了一遍调色板，命名也不统一。
  - `herdr-ui-review` 用 `--surface/--ink/--accent/--amber…`，深色挂在 `.phone.dark`。
  - `herdr-onboarding` 用 `--bg/--surface2/--fg/--accent-ink…`。
  - `herdr-connection-progress` 用 `--green/--green-soft/--orange-soft…`。
  - 位置见 `BH:prototypes/*/style.css` 第一条规则。【判断】原型和原生没有共用 token 源，色值只能靠人工保持一致。

### 1.3 组件

| 组件 | 位置 | 要点 |
| --- | --- | --- |
| `WorkspaceAvatar` | `WorkspaceStyle.swift:36-54` | 名字做哈希，从 5 色里取一个渐变圆；里面放首字母或 SF Symbol；`accessibilityHidden` |
| `WorkspaceStatus` | `:56-64` | 5pt 圆点加 11pt 次要文字，用在「已连接」这类状态 |
| `WorkspaceIconButton` | `:66-75` | 40×40 的命中区；必须传 `accessibilityLabel` |
| `WorkspaceChrome` + `.workspaceDetail()` | `:77-111` | 进入详情页时登记，自动隐藏底栏；离开时恢复 |
| `.workspaceTabBar` / `AdaptiveWorkspaceTabs` | `:122-154` | iOS 27.1 起，竖排的系统 tab（Duo 外屏、横屏侧栏）交给系统管；横排时用自绘的飞书式底栏 |
| `.workspaceNavigationBarHidden()` | `:112-119` | 根页面隐藏系统导航栏，改用自绘顶栏 |
| `HerdrPrimaryButtonStyle` | `HerdrStyle.swift:44-54` | 至少 44pt 高、圆角 12、`onAccent` 字；按下时透明度 0.75，禁用时 0.4 |
| `.herdrPanel(cornerRadius:)` | `:56-61` | `field` 底色加 0.5pt `line` 描边，默认圆角 14 |
| `HerdrAgentMark` | `:34-42` | Claude 会话的标记，橙色星号 |
| `ServiceStatusView` | `ServiceStatusView.swift:3-33` | 整页的状态：图标、标题、说明、主按钮 |
| `WorkspaceSheet` | `WorkspaceSheet.swift:3-48` | 统一的浮层头部（58pt 高，返回、标题、附件位、关闭），也可以切到系统导航栏样式 |

还有一些组件散在 Feature 里，没有收进 DesignSystem：

- `AgentNavigationBar`（`BH:.../AgentsFeature/AgentNavigationBar.swift:4-50`）：保留系统返回键，自定义 principal 标题和副标题，以及右侧操作。
- `AgentConnectionProgressView`（连接路径进度条）。
- `SSHKeyFormSection` 和 `SSHKeyErrorNotice`（`BH:.../SSHKeyFeature/SSHKeyLibraryView.swift:153-173`）：分组标题加面板，以及行内错误。
- `AgentListStatus`（`BH:.../AgentsFeature/AgentsView.swift:426-436`），和 `WorkspaceStatus` 几乎一样，是重复实现。

【判断】组件抽取不彻底：同一个「圆点 + 状态字」有两份实现；表单分组、行内错误、待核对列表这些通用件留在 Feature 里。

### 1.4 导航框架（紧凑顶栏、头像、统一导航）

- README 的自我描述：「飞书风格的消息、通讯录、Herdr、我的四个入口，紧凑顶栏、头像与统一导航」（`BH:README.md:11`）。验收文档写的是「统一的蓝灰配色、紧凑顶栏与自定义底部导航；聊天详情隐藏底部标签栏」（`BH:docs/VALIDATION.md`「飞书风格与阅读交互」一节）。
- **根页面的顶栏**一律隐藏系统导航栏，自己画一行：
  - 消息页（`BH:.../MessagingFeature/MessagingView.swift:342-393`）：左侧是 38pt 账号头像，加上姓名（`.title3` semibold）和组织（`.caption`），整块是一个按钮，点了进账号与连接设置，无障碍值读姓名和组织。右侧是搜索和「＋」菜单。实时连接断开时多一个 `wifi.slash` 小图标（`:359`）。
  - 筛选条（`:400-457`）：左侧一个圆形菜单（自定义分组和标签都收在里面），右侧灰色胶囊里固定放三个主范围「消息 / 未读 / 标记」，选中项白底。这一行吸顶。
  - Herdr 首页（`BH:.../AgentsFeature/AgentFleetView.swift:123-145`）：标题「Herdr」，右侧三个图标按钮（主机与连接、通知中心带未读数、新建）。用 `ViewThatFits` 在一行放不下时折成两行。
  - 「我的」页（`BH:.../ConnectionsFeature/ConnectionsView.swift:163-166`）：标题 23pt。
- **详情页**用系统导航栏，并且明确保留系统返回键，注释写「Keep the system back item so UIKit owns edge/content gestures and cancellation」（`BH:.../AgentNavigationBar.swift:5`）。还有一组宿主测试专门检查边缘手势和内容手势的 delegate 没有被换掉（`BH:Packages/removedKit/Tests/removedKitTests/AgentNavigationBarTests.swift`）。
- **底栏**：自绘的 5 个入口，选中时换填充图标、文字加粗。未读角标超过 999 显示「…」，但无障碍标签读完整数字（`BH:App/removedApp.swift:473-503`；规则见 `BH:docs/LARK_INBOX_UI_REVIEW.md:96`）。
- **主机层级下沉**：评审之后，Herdr 首页改成「任务优先」。主机压成一条「主机 x/y 已连接」的连接条（`AgentFleetView.swift:310-325`），主机大卡、连接详情、密钥和外观都收进一个独立的面板 sheet（`:326-352`）。点任务直接推进同一个 NavigationStack，返回一次就回到首页（`BH:docs/HERDR_CINDY_REVIEW.md:83-89`）。
- 【对照】Persona 的 Agents 对话页隐藏了系统导航栏，用自绘的 `ChromeHeader`（跟手收起），再用 `SwipeBack.swift` 把 `interactivePopGestureRecognizer` 重新打开（`JV:docs/ios.md:131-139`）。removed 走的是另一条路：详情页保留系统返回键，再写测试守住。

### 1.5 无障碍（动态字体、辅助字号下的布局）

- 辅助字号（`dynamicTypeSize.isAccessibilitySize`）下切换布局，本次 grep 共 28 处，几种典型做法：
  - 横排的分段筛选在辅助字号下改成竖排：`AnyLayout(VStackLayout)` 和 `AnyLayout(HStackLayout)` 互换，容器圆角也跟着变（`AgentFleetView.swift:414-427`、`MessagingView.swift:424-443`）。
  - 辅助字号下隐藏行首的装饰图标（`AgentFleetView.swift:505-510`、`AgentsView.swift:157-159`）。
  - 辅助字号下放开行数限制：标题从 2 行变成不限，路径从 1 行变成 3 行（`AgentFleetView.swift:490`、`:513`）。
  - 顶栏用 `ViewThatFits` 在一行和两行之间切换（`AgentFleetView.swift:124-135`）。
  - README 提到确认按钮固定在底部，辅助字号下也能直接取消；宽屏按可用空间把操作并排（`BH:README.md:90`）。
- 减少动态效果：连接进度的光点和呼吸动画在 `accessibilityReduceMotion` 打开时停在中间位置（`BH:.../AgentConnectionProgressView.swift:8`、`:26-27`、`:56`、`:68`）。三个原型的 CSS 都有 `@media (prefers-reduced-motion: reduce)`，把动画关掉。
- 读屏：图标按钮都要求带 label（`WorkspaceIconButton`）。计数角标的无障碍标签读完整数字，视觉上省略显示也不影响（见上）。连接进度的节点图对读屏隐藏，只读一行文字状态（`AgentConnectionProgressView.swift:37-40`）。
- 本次 grep 计数：`accessibilityLabel(` 329 次，`accessibilityIdentifier(` 187 次，`@ScaledMetric` 7 次，`ViewThatFits` 8 次。
- 文档自己承认的边界：「动态字体已有自适应布局，实机大字号、真实键盘、边缘手势和帧率仍需实际验收」（`BH:docs/HERDR_CINDY_REVIEW.md:91`）。大字号补验只做到「最大辅助字号检查确认三个主按钮均可见」，并明确说这不等于完整的 VoiceOver 验收（`BH:docs/LARK_INBOX_UI_REVIEW.md:73`）。

### 1.6 空态、错误态、加载态、「待确认」态怎么表达

| 状态 | 做法 | 证据 |
| --- | --- | --- |
| 整页：未配置 / 连不上 | `ServiceStatusView`（图标、标题、说明、一个主按钮：「配置连接」「重试」「前往消息」） | `BH:App/removedApp.swift:261`、`:272`；`MessagingView.swift:22`、`:91`；`AgentsView.swift:76` |
| 列表为空 | 系统 `ContentUnavailableView`（本次 grep 98 处）。文案按筛选范围区分，常在下面补一个能走出空态的按钮，例如「查看全部会话」 | `AgentFleetView.swift:455-459`；没有主机时是「连接你的开发主机」加「添加第一台主机」（`:63-66`） |
| 加载 | 整页 `ProgressView("正在连接飞书网关…")`；局部 `ProgressView("读取密钥库…")`；按钮里的进度：「正在保存…」加禁用 | `MessagingView.swift:73`；`SSHKeyLibraryView.swift:41-42`；`SSHKeyImportView.swift:70-76` |
| 局部错误 | 在出错的位置显示 `Label(错误, systemImage: "exclamationmark.circle/triangle")`，红色或橙色，紧跟一个「重新读取…」按钮；只有操作失败才弹 alert「操作失败」 | `ConnectionsView.swift:119-130`、`:188-190`；`SSHKeyLibraryView.swift:36-40`；`MessagingView.swift:222-239` |
| 缓存态 | 「已显示本机会话记录」或「正在更新本机会话…」，配 `internaldrive` 图标 | `MessagingView.swift:218-221` |
| 破坏性确认 | `confirmationDialog`，正文写清后果和范围（「同时删除此连接保存的登录凭据。主机上的服务和会话会继续运行。」） | `ConnectionsView.swift:170-183`；`SSHKeyDetailView.swift:124-129`（列出这把密钥在哪些主机上用） |
| **待确认 / 待核对** | 词汇固定成「…结果待确认」「待核对」「核对…结果」。图标固定：`clock.badge.questionmark`、`clock.badge.exclamationmark`、`exclamationmark.circle`。颜色用橙色。只提供只读的核对操作，**不自动重发** | 设置行尾的「待核对」橙字（`ConnectionsView.swift:54`、`:91`、`:102`、`:113`）；任务行的「状态待核对」（`AgentFleetView.swift:518`）；工作区里的「待核对操作」列表（「发送结果待确认 · 会话名」等，`AgentsView.swift:191-202`）；「核对会话启动结果」「已在主机核对启动结果…」（`:203-218`）；待发队列的 10 种状态，每种有固定文案和图标（`AgentPromptQueueView.swift:168-187`）；README 原则：「发送结果未知时不自动重发」（`BH:README.md:37`） |

【判断】removed 最值得学的是：每种「结果不确定」都有名字、有文案、有图标、有只读的核对入口。代价是文案很长、术语偏工程化，比如「主机接收结果待确认」「核对结论待确认」。Persona 的用户就是作者自己，可以借这套结构，但文案要压短。

### 1.7 小结【判断】

- 视觉 token：removed 比 Persona 弱。字体、间距、圆角都没有 token，字面量多；两套调色板也没有统一的语义层。
- 组件：靠系统控件加少量自定义件，散在各 Feature 里。
- 强项：信息架构改版有评审依据；状态和恢复的表达统一；无障碍有具体的布局切换手段；对「什么算通过」有明确的分级。

---

## 2. 首次接入引导（连接配置 → 私钥导入 → 主机指纹 → 连接进度 → 组件安装）

### 2.1 removed 的流程【事实】

1. **连接表单**（我的 › 连接 Herdr 主机，`BH:.../ConnectionsFeature/ConnectionsView.swift:280-328`），分五块，每块是「小标题 + 面板」：
   - 「目标主机」：名称、地址、端口、用户名，认证方式固定为 SSH 私钥。
   - 「连接路径」：「通过 SSH 跳板机连接」开关。**下面的说明文字随开关变化**：开着时写「目标地址从跳板机访问；目标为 127.0.0.1 时指跳板机自身；跳板机需允许 TCP 转发」，关着时写「设备直接访问目标地址」（`:304`）。
   - 「安全」：只放说明——分别核对主机指纹、私钥存 Keychain、未选密钥时保留已有凭据（`:307-315`）。
   - 「SSH 密钥」：从密钥库里选，跳板机可以选「共用」或「独立」密钥（`:329-363`）。
   - 「下一步，设置主机组件」提示卡：说清 SSH 验证通过后还要确认安装组件，也可以跳过、只用终端（`:317-324`）。
   - 每个输入框有专门的读屏名称，比如把「地址」读成「主机地址」、把「端口」读成「SSH 端口」，键盘类型按字段区分（`:405-419`）。
   - 消息服务的表单如果填的是 `http://`，会出警告「HTTP 不加密传输，请确认目标网络与服务配置」（`:434-436`）。
2. **私钥导入**（`BH:.../SSHKeyFeature/SSHKeyImportView.swift`）：
   - 用分段控件选「粘贴或输入 / 文件导入」（`:42-57`），有系统 `PasteButton`（`:128-134`）。
   - 输入框失焦后私钥内容自动隐藏，显示「已输入私钥 · 内容已隐藏」（`:142-149`）；输入框加了 `privacySensitive`。
   - 页面关闭时清空内存里的私钥和口令（`:111-119`）。
   - 文件只读前 1 MB，读取放到后台任务、可以取消（`:239-251`）。
   - 错误按原因分别给提示：粘了公钥、不是 OpenSSH 格式、缺 BEGIN/END 行、字面上的 `\n` 等（`BH:Packages/removedKit/Sources/HerdrKit/HerdrContracts/HostTransport.swift:60-75`）。
   - 密钥库（`SSHKeyLibraryView.swift`）：空态是「还没有保存的密钥」加主按钮（`:75-86`）；每行显示算法、指纹，以及这把密钥「用于：主机列表」（`:88-124`）。
   - 详情页可以复制公钥；复制私钥只放本机剪贴板，60 秒后过期（`SSHKeyDetailView.swift:185-207`）。
3. **分段连接进度**：
   - 阶段的枚举按顺序是：准备 → 连接跳板机 → 跳板机认证 → 连接开发机 → 开发机认证 → SSH 就绪 → 检查组件 → 更新组件 → 握手 → 读取会话 → 就绪（`BH:.../HerdrContracts/HostConnectionStage.swift:4-7`）。每个阶段有一句固定文案（`BH:.../AgentsFeature/AgentConnectionProgress.swift:18-32`）。
   - 主机卡片里只占两行：一行「本机 → 跳板机 → 开发机」的小节点路径（连接中光点沿着当前这一段移动，认证中节点呼吸），一行文字状态，再加一个「详情」按钮（`AgentConnectionProgressView.swift:5-73`）。
   - 详情页列出每个阶段和发生时间，有「取消连接」，完成后是「进入工作区 / 继续处理连接」（`:75-107`）。
   - 原型 README 写下的设计约束：未完成时不显示虚构的百分比；等用户确认时动画暂停；进度必须由真实的传输层事件驱动，笼统的 `.connecting` 不够用（`BH:prototypes/herdr-connection-progress/README.md:17`、`:21`）。
4. **逐跳核对指纹**（`BH:.../AgentsFeature/AgentsView.swift:135-150`）：
   - 标题区分两种情况：「核对 SSH 主机指纹」和「SSH 主机密钥已更换」。
   - 写明这一跳是跳板机还是目标主机，加上地址。
   - 指纹用等宽字体显示，可以选中复制。
   - 说明「请与主机提供的指纹核对，确认后才会发送 SSH 认证」。
   - 按钮是「已核对，信任并连接」和「取消」。开了跳板机时先核对跳板机，再核对目标主机（`BH:README.md:156`）。
   - 消息服务的 HTTPS 证书用同一种页面：主机名、主题、SHA-256 指纹、「已核对，信任此服务器证书」；证书不合规时不给信任按钮（`MessagingView.swift:75-89`）。
5. **主机组件安装引导**（`BH:.../AgentsFeature/AgentHostComponentView.swift`）：
   - 标题随状态变化，比如「让手机，更懂你的工作。」「先准备好主机环境。」「主机组件已就绪。」（`:25-34`）。
   - 三条好处（`:56-60`）。
   - 主机环境依赖清单，每项标可用或缺失，并附说明（`:70-87`）。
   - 固定在底部的操作区：「安装并继续」「重新检查主机环境」「暂时跳过，仅用终端」（`:170-194`）。跳过要再确认一次，确认框里写清跳过会少什么（`:104-107`）。
   - 安装进度分 4 步：检查 → 上传 → 安装并验证 → 就绪；没轮到的步骤写「等待上一步完成」（`:138-168`）。
   - 「会在我的主机上做什么？」浮层说明安装位置、hooks、不开新端口、保留现有工作（`:95-98`、`:210-219`）。
6. **错误分类**：
   - SSH 连接失败分 DNS、端口拒绝、网络不可达、超时、认证失败、转发失败、需要核对指纹、跳板机缺凭据，每种给可以照着做的提示（`HostTransport.swift:22-49`）。
   - 失败会标明是哪一跳出的问题（`hopFailed`）。
   - README 强调：网络问题不需要重新导入私钥（`BH:README.md:72`）。
7. 这套流程**先在 HTML 原型里定稿**（`BH:prototypes/herdr-onboarding/`，01 新建连接 → 02 安装引导 → 03 安装与验证 → 04 开始工作，另外有跳过、旧版本、上传中断、缺依赖四种异常场景），用户确认后才做原生（`BH:prototypes/herdr-onboarding/README.md:1-38`）。

### 2.2 Persona 配对的现状【事实】

- `WelcomeView`：App 图标、口号、四条特性介绍、「连接 hub」按钮，下面一行提示「在 hub 所在的机器上运行 `jarvisd hub pair` 显示二维码」（`JV:ios/Jarvis/Views/Settings/SettingsView.swift:220-274`）。
- `PairView`：
  - 用 `DataScannerViewController` 扫码，扫到就立刻连接；同时有「手动输入」，填服务器地址和 Token（`:276-322`）。
  - 失败只有一句：「连接失败：…确认手机能访问这个地址（比如在同一个网络或 VPN 上）。」（`:333`）。
- 配对后，App 用配对 token 换一个本设备专用的 token，之后只用这个；hub 只存哈希（`JV:docs/protocol.md:8-14`）。

### 2.3 Persona 可以借鉴的【判断】

1. **扫码之后显示分段进度，不要只转圈**：解析二维码 → 连上 hub（DNS / TCP / TLS）→ 校验 token → 换本设备 token → 读取机器列表 → 就绪。每段失败给分类过的提示：
   - DNS 失败：「找不到这个地址」。
   - 拒绝连接或超时：检查 hub 是否在跑、手机和 hub 是否在同一网络或 VPN。
   - 401 / 403：配对码已失效，重新运行 `jarvisd hub pair`。
   - 版本不兼容。
   - Android 端照同一套阶段和文案实现，文案放进共享的字符串表。
2. **二维码同时带上 hub 的身份**：比如在配对 URL 里加证书或公钥指纹。扫码时自动比对，对不上就进入 removed 式的「核对指纹」页，写明「已更换 / 首次」，不能悄悄接受。如果是 HTTP（非 TLS）地址，像 removed 那样在表单里明确警告。
3. **说明配对做了什么**：加一个「这台手机会保存什么 / hub 上会多什么」的说明浮层，内容包括设备 token、Keychain、可以在「设置 › 设备」吊销。对应 removed 的「安全」面板和「会在我的主机上做什么？」。
4. **配对之后加一个「准备清单」**：列出各台机器是否在线、herdr 和 Claude 版本、node 是否和 hub 版本一致、通知权限。每项可以跳过，并写清跳过的影响。参照 removed 的依赖清单和「暂时跳过，仅用终端」。
5. **手动输入的表单也按字段分开**：地址和 token 分开填、键盘类型分开、每个框有读屏名称；粘贴整条 `jarvis://pair?...` 时自动拆开。
6. **整个接入流程先做 HTML 原型**（窄屏 320 / 393、深浅色、减少动态效果、各种异常场景），用户确认后再写 iOS 和 Android。

---

## 3. 原型 → 评审文档 → 验收记录

### 3.1 原型的形式【事实】

- 放在 `BH:prototypes/` 下，共 4 个：
  - `herdr-ui-review`：导航、信息层级和会话阅读的评审稿。
  - `herdr-onboarding`：连接加组件安装。
  - `herdr-connection-progress`：连接进度。
  - `herdr-moshi`：早期参照 Moshi 的单文件 Demo，43 KB。
- 形式：纯静态的 `index.html + style.css + app.js`，**没有依赖，也不请求外部资源**。可以直接打开，也可以用 `python3 -m http.server <端口> --bind 127.0.0.1 --directory prototypes/<名>` 起一个本地服务（各 README 开头）。
- 页面版式统一成「工作室」样式：
  - 左栏写设计说明：一句主张、2 到 3 条原则（如「任务优先，少走一层」「正文清楚，过程可展开」「输入和控制各有位置」）、借鉴来源、免责声明「交互草案 · 全部为示例数据 · 不会连接主机、提交审批或发送消息」。
  - 场景按钮：正常流程，加上异常场景（连接较慢、远端失败、等待确认、上传中断、缺依赖）。
  - 右侧是手机框，尺寸 390×812 或 393×852，带状态栏和灵动岛。有深浅色切换，有「窄屏 320」切换，有「重新开始」（`BH:prototypes/herdr-ui-review/index.html:5-27`、`herdr-onboarding/index.html:11-38`、`herdr-connection-progress/index.html:11-50`）。
- 每个 README 都写了四块（`BH:prototypes/*/README.md`）：
  - **范围**：「本轮为…评审稿，不是原生功能完成声明」。
  - **可体验**：能点哪些流程。
  - **原型与原生实现的边界**：哪些是模拟的；正式实现必须由什么真实事件驱动；什么不能算验收。
  - **验证记录**：JS 语法检查；在真实浏览器里点完整个流程；393 深色和 320 浅色宽度下实测 `scrollWidth == clientWidth`（不横向溢出），底部操作始终可见；截图放在被 Git 忽略的目录（`herdr-onboarding/README.md:34-38`）。
- 原型确认之后，原生落地的结果也回写到原型 README，例如「2026-10-26：确认版已接入 App 原生界面……本 HTML 仍是可重播的独立合成演示，不作为真实连接证据」（`herdr-connection-progress/README.md:21`）。

### 3.2 评审文档的三种写法【事实】

1. **借鉴评审：参考一个开源项目**（`BH:docs/HERDR_CINDY_REVIEW.md`）
   - 开头写清依据：参考的是 GitHub 上的公开 Agent 客户端 Cindy，**钉在一个固定提交上**；只读源码，没有运行；「以下为源码 / 契约审阅，不是运行质量评价」。
   - P0 / P1 表格两列：「值得借鉴的实现」和「对本项目的用途与边界」。
   - 「UI 与交互评审」表格三列：「部位 / 当前问题·目标 / 草案的具体安排」（`:67-75`）。
   - 写明借鉴什么、不借鉴什么：借鉴内容优先、稳定导航、输入区和组件一致性；不借鉴对方的单色风格、复杂按钮形变和全套侧边抽屉；对方自己承认没验证过的部分不当作范例（`:77`）。
   - 写明原生的落地顺序和回归要点：导航恢复、主机隔离、草稿、动态字体、键盘（`:81`）。
   - 用户认可后，追加「原生落地」小节，说明这次改了什么、哪些协议和门禁没动（`:83-91`）。
2. **同机对照：和参考 App 并排比**（`BH:docs/LARK_INBOX_UI_REVIEW.md`）
   - 开头写基线提交号和方法：在同一台实体 iPhone 上，通过 Device Hub / CUA 分别看参考 App 和自己的 App，再对照 Swift 源码；声明「本轮仅审阅，没有修改 UI」。
   - 差异表的列是「优先级 / 部位 / 参考实机 / 当前 / 调整方向」，按 P0 / P1 / P2 排（`:7-16`）。
   - 一句话指出主要矛盾：「多出的搜索框 + 更高的行 = 首屏松散，应先改结构与密度，再调颜色、字重、分割线」（`:18`）。
   - **把「需要另外核对的数据问题」和视觉问题分开写**：未读总数不能只改文案；首帧头像缺失不能直接判定为加载失败；列数差异是设备宽度造成的，不是缺陷；配色要在同一主题下再验（`:20-25`）。
   - 写「已有能力与本轮边界」，说明哪些已经做过、本轮不算通过（`:27-33`）。
   - 证据清单：截图文件名，存在本机被忽略的目录，不入库（`:35-44`）。
   - 列出源码位置和建议的落地顺序（`:46-53`）。
   - 后面按日期追加「原生实施进度」小节：做了什么、测试数量、隔离 QA、仍未完成的清单（`:58-107`）。
3. **正文对照与修复清单**（`BH:docs/LARK_CHAT_UI_REVIEW.md`）
   - 表格三列：「范围 / 证据与现状 / 下一步」。
   - 反复强调「不将自己的截图误标为原版」「不冒称逐像素对齐」「不从截图声称 FPS 改善」（`:3`、`:11`、`:31`、`:37`）。

### 3.3 验收记录的写法和「通过」的分级【事实】

- **状态分级**（`BH:docs/FEISHU_PARITY.md:5`）：「已开发、协议通过、模拟器通过、真实账号通过、真机通过是不同状态；页面相似或接口存在都不能算通过」。
- **每页核查清单**（`:12`）：顶栏、导航、字号 / 行高、头像、颜色、间距、分隔线、图标、触摸区域、键盘、滚动位置、空 / 加载 / 失败 / 离线状态、深色模式、动态字体、VoiceOver。
- **每个操作的核查清单**（`:13`）：入口、权限、请求参数、响应、跨设备同步、重复点击、失败 / 取消 / 重连、账号切换。
- 对齐表的列：「项目 / 当前状态 / 后续验收重点·上游依据」（`:34-39`）。
- **单条验收记录**（`BH:docs/VALIDATION.md`，约 1 MB，约 206 个二级标题；`BH:docs/HERDR_PROGRESS.md` 写法相同）：
  - 标题是「主题（日期）」。
  - 正文用固定的小标题分段：范围、配置及模型、实际协议、真实服务、模拟器实际操作、清理、设备包、边界（例：`VALIDATION.md` 开头的「单跳 SSH 连接」一节）。
  - 每条记录必写以下内容：
    - 测试数量，按「iOS 18.5 / 27.1 各 N 项 / M 套」写；
    - 日志路径；
    - 证据目录，在被 Git 忽略的 `.local-services/validation/<主题>/`，里面有 `QA.md`、截图、`*-stats.json`；
    - **什么不算这次通过**（「不代表…」「仍待…」）；
    - QA 模拟器的数据按 SHA256 恢复到测试前；
    - 合成服务的写入计数（例如 `writeAttempts` 为空，证明没有误写真实数据）。
  - 规矩：
    - 先写会失败的回归，确认它失败，再修（`LARK_INBOX_UI_REVIEW.md:73`、`:89`）；
    - 跑错测试选择器、实际跑了 0 项，不算验证（`:73`）；
    - 早期失败的截图不算最终通过（`VALIDATION.md`「加急原消息…」一节）；
    - 自动化统计表只记数量，不把测试数当功能完成数（`VALIDATION.md`「自动化验证」一节）。
- 文档里有一张「尚待验收」清单，专门列真机、网络切换、锁屏恢复、键盘、旋转等（`VALIDATION.md`「尚待验收」一节）。

### 3.4 判断

- 【判断】这套流程适合「需求经常被用户改、视觉要和参考 App 对齐」的项目：原型改得快，评审文档把改什么、为什么、不改什么说清楚，验收分级避免把「模拟器能看」当成「完成」。
- 【判断】缺点：
  - 验收记录太长、太密，VALIDATION.md 有 1 MB，查找成本高；
  - 证据放在本机忽略目录里，换台机器就看不到。这次在本机就没找到 `.local-services/`，QA.md 的模板只能从文档里倒推；
  - 原型的 token 是手抄的。
- 【判断】Persona 用的话，每次评审一个 md 加一个原型目录就够；验收记录压成「分级表 + 每条一段」；证据目录不进仓库，但要固定路径约定，写进 `CLAUDE.local.md`。

---

## 4. 测试金字塔与 fixture

### 4.1 removed 的分层【事实】

| 层 | 例子 | 说明 |
| --- | --- | --- |
| 纯展示 / 规则单测 | `ConversationUnreadBadgePresentationTests.swift` | Swift Testing 参数化，覆盖角标边界：99 / 100 / 601 / 999 / 1000 / Int.max，以及偏好、免打扰、置顶这几种呈现面 |
| 协议客户端单测（假传输） | `NavigationUnreadClientTests.swift` | 断言请求路径和查询参数；断言「请求前后各核对一次账号」；畸形响应（`""`、`{}`、`"7"`、`-1`、`1.5`、溢出）**绝不能变成 0**；超大载荷要拒绝 |
| 模型 / 工作区 / 日志单测 | `*ModelTests`、`*WorkspaceTests`、`*JournalTests`、`AgentSessionTests`（1 万多行） | 状态机、恢复、未知回执、账号隔离 |
| 宿主 SwiftUI 测试（进程内，非 XCUITest） | `AgentNavigationBarTests.swift` | 把真实的 SwiftUI NavigationStack 放进 UIWindow，检查 UINavigationController 的层级、返回键、边缘和内容手势的 delegate 没被换掉；`.serialized`、`.timeLimit`；注释写明「不发送触摸，不证明真机交互式滑动可用」 |
| 本机真实服务的集成测试 | `SSHIntegrationTests.swift` | 起一个回环 NIOSSH 服务器，用真实 `ssh-keygen` 生成的密钥，覆盖指纹核对早于认证、超时和取消隔离等；主机端组件另跑 `node --test host/removed-bridge/test/*.test.mjs`（`BH:README.md:47-53`） |
| 对固定上游的契约测试 | `BH:scripts/check-*-upstream.{mts,mjs}`（22 个） | 直接 import 钉在某个提交的上游源码里的 service 和路由，用 fastify `inject` 跑真实路由；最后打印一行 JSON 证据，形如 `{passed, httpReads, httpMutations, realNetworkCalls:0, …}`（例：`check-workplace-upstream.mts`） |
| fixture 自洽检查 | `BH:scripts/check-*-fixture.mjs`（60 个） | 检查合成服务的各种故障模式（`applied503` / `unapplied503` / `missingReceipt` / `missing`）行为正确，并打印证据 JSON（例：`check-workplace-fixture.mjs`） |
| 合成 UI 验收 | `BH:scripts/fixtures/` 下 72 个模块（入口是网关的 UI 夹具脚本 `*-ui.mjs`）；Debug 启动参数：本次 grep 到 23 个 `--removed-*`，其中 agent 相关 17 个，另有语音、视频、滚动调试等 | 详见 4.2 |
| 真实环境 | 真实账号只读、真实 SSH 主机、真机覆盖安装 | Device Hub / CUA 同机对照；「真实环境已验证」一节（`VALIDATION.md`） |

- 每层都在两代系统上跑：iOS 18.5 和 27.1，外加 macOS。最近一次完整回归的计数（`VALIDATION.md`「自动化验证」表）是：
  - macOS Swift Package：2882 tests / 271 suites；
  - iOS 18.5、27.1：各 2353 / 191；
  - 后续各阶段只跑专项，单独记数。
- **没有 XCUITest target**（`git ls-files` 里没有 UITests），也**没有截图比对库**：`BH:Packages/removedKit/Package.swift:36-41` 的依赖只有 swift-markdown、Citadel、SwiftTerm、swift-nio、swift-nio-ssh、swift-crypto。界面验收靠两样：在隔离的 QA 模拟器里由人或 CUA 操作并截图；进程内的宿主测试。

### 4.2 fixture 的做法【事实】

- **合成服务**（`BH:scripts/fixtures/README.md:1-20`）：
  - 只含虚构的人员、组织和消息，只绑定 127.0.0.1，默认端口 3334。
  - 每个请求记到 `removed-ui-fixture-stats.json`，用来核对重复点击和操作顺序。
  - 有测试控制接口（`POST /__fixture/...`）用来切换故障模式，例如首次 503、延迟、回执丢失、外部删除。
  - 用环境变量开场景，例如 `removed_UI_*_FAIL_FIRST=1`、`*_DELAY_MS`。
  - 覆盖 180 条消息、501 人的成员窗口、分页游标里带 `+` / `=` 的编码陷阱等。
  - 媒体都是程序生成的：静音的 Ogg Opus、色条视频、蓝橙色的测试 PNG。
- **魔法输入**：在 Agent 隔离 fixture 里输入 `100100` 会返回一份 Markdown 样例；输入包含 `900900` 会模拟「结果未知」（`fixtures/README.md`「Herdr 隔离恢复验收」）。
- **隔离状态文件**：每个 fixture 启动参数用自己的一组 runtime / state / reading / navigation 文件，不碰真实数据（`BH:App/removedApp.swift:157-166`）；Release 包不启用这些参数。
- **故障注入**：`DebugAgentReplyFault` 只在模拟器、只对回环主机、只在显式参数下生效，只触发一次，丢掉一次「已提交」回执来制造「结果未知」（`BH:App/DebugAgentFixture.swift:8-60`）。
- **固定上游**：协议依据钉在上游的某个提交，接口清单用 AST 扫描生成（`BH:docs/FEISHU_PARITY.md`「核查基线」；`BH:scripts/audit-*.mjs`（网关接口审计））。

### 4.3 Persona 的对应物【事实】

- 单测：`JV:ios/JarvisTests` 下 24 个 Swift 文件，外加录制的终端帧 `terminal-*.jsonl`；服务端用 Go 测试，transcript 有脱敏样本做 golden 和语料对比（`JV:docs/testing.md:5-15`、`JV:docs/archive/lanes/msg-render-verification.md`）。
- UI 测试：`JV:ios/JarvisUITests` 下 16 个 XCUITest 文件，例如 `MessageMenuTests`、`SendSafetyTests`、`ConnectionTests`、`TerminalKeysTests`、`MachinePagesTests`、`LiveActivityShots`。
  - 多数连 `scripts/mock/mock_hub.py`。mock 用 `MOCK_*` 环境变量开场景：断网、延迟、断档、终端流、上传等；用 `GET /mock/resolved|sent|keys|uploads|hits` 读回收到了什么来做断言（`JV:docs/testing.md:79-139`）。
  - `MachinePagesTests` 按步骤截图，浅色和深色各跑一遍，失败时附截图和视图树（`:129-130`）。
- mock hub 本身是为截图隐私做的：编造的三台机器、Agent、收件箱；配合 DEBUG 启动参数直达页面（`JV:docs/testing.md:40-77`、`JV:docs/ios.md:299-305`）。
- App 内探针：`-showFoldProbe`、`-showFreezeProbe`、`-showAutoScroll` 把结果写到 `Documents/*.txt|json`（`JV:docs/testing.md:61-63`、`:161-173`）。
- 规矩：写 UI 测试先在没修的代码上跑一次，确认它会失败（`JV:docs/testing.md:85`）；有隐私扫描（`:149-159`）。
- 界面检查清单只有 5 行，比 removed 的「每页 / 每操作」清单粗（`JV:docs/testing.md:30-36`）。验收也没有分级。

---

## 5. Persona 设计系统现状，以及和 removed 的逐项对照

### 5.1 Persona 现状【事实】

- **风格 × 语义色**：5 套界面风格 `AppStyle`（液态玻璃 / Claude / 黏土 / Island / 飞书）（`JV:ios/Shared/Theme.swift:5-54`）。
  - 按风格取值的颜色：`accent`、`userBubble`、`userBubbleText`、`pageBackground`、`card`、`cardStroke`（`:66-77`、`:219-269`）。
  - 不随风格变的语义色：`brand`、`danger`、`positive`、`caution` / `cautionText`、`ai`、`loop`、`countBadge`、`monogramFill`，以及 `Diff`、`Hue`（`:80-112`、`:211-216`）。
- **可读性底线写成了规则**（`JV:docs/ios.md:223-225`）：
  - 实心按钮和选中胶囊上的白字 ≥ 3.5:1；
  - 用户气泡上的白字 ≥ 4.5:1；
  - 主色当链接用时对页面 ≥ 3:1；
  - 渐变只往下压暗，不提亮。
- **状态色表**（需要你 / 工作中 / 完成未看 / 已看 / 未运行）：每个状态有三个值 `dot / text / tint`，App、手表和实时活动三个 target 共用一份（`JV:ios/Shared/StatusPalette.swift:7-30`），规则写在 `JV:docs/ios.md:232`。
- **结构 token**：
  - `Radius` 五档：6 / 10 / 14 / 20 / 22（`Theme.swift:116-127`）；
  - `Space`：2 / 4 / 8 / 12 / 16 / 20 / 24 / 32（`:130-139`）；
  - `Fill`：well、chip、pressed 三档，对应系统的 quaternary、tertiary、secondary fill（`:144-154`）；
  - `Font`：按角色取动态字体，有 panelTitle、rowTitle、body、cardBody、preview、meta、chip、micro、code、greeting（`:158-180`）；
  - `Motion`：状态切换 `.snappy 0.25`，布局 `.smooth 0.3`（`:205-208`）；
  - 手表有自己的一组 `Watch` 尺寸（`:184-201`）。
- **风格化修饰符**：`surface(tint:interactive:in:)`、`themedButton(prominent:)`、`themedPage()`、`themedGroupedPage()`、`themedRow()`，外加黏土和 Island 用的 `SoftButtonStyle`，至少 44pt 高（`JV:ios/Jarvis/Views/Styling.swift:6-137`）。切换风格时，RootView 以风格为 id 整棵重建，所以 token 用静态值就够（`JV:docs/ios.md:229`）。
- **组件**（`JV:ios/Jarvis/Views/Components.swift`）：
  - `StatusDot`：「需要你」会呼吸，减少动态效果时停（`:6-38`）；
  - `SessionAvatar`（`:41-69`）；
  - `Chip`：中性 / 着色 / 描边三种样子（`:75-128`）；
  - `CountBadge`：超过 99 显示 99+（`:132-165`）；
  - `StatusLabel`（`:167-178`）；
  - `EmptyStateView`：图标、标题、说明、可选按钮（`:180-206`）；
  - `.card()`（`:208-215`）；
  - `CloseAgentSheet`：破坏性确认浮层，写明是哪个 Agent、在哪、关了会怎样（`:228-333`）。
- **连接状态**放在标题位，不弹横幅（`JV:docs/ios.md:273-297`）。
- **离线时的操作**：消息存进 Outbox，连上后自动发，重发带同一个 `client_msg_id`；一次性操作（批准、回答）不排队，失败就把卡片放回去，并给一条蓝色提示（`:288-293`）。
- **采用率**【事实，本次 grep，范围 `JV:ios`，共 165 个 Swift 文件、41,816 行】：
  - `Theme.Font.` 只出现 1 次，`Theme.Space.` 也只出现 1 次；
  - `Theme.Radius.` 74 次，但 `cornerRadius:` 字面量还有 70 处；
  - `spacing:` 字面量很多，比如 6 出现 113 次、8 出现 87 次；`.padding(数字)` 字面量 464 处；
  - 视图里的 `Color(light:` 在 Theme / StatusPalette 之外还有 37 处，分布在飞书渲染、机器图表、OSBadge、Styling、实时活动；
  - 直接用系统 `.orange/.red/.green/.blue` 49 处；
  - 字号：固定 `font(.system(size:` 60 处，文本样式 646 处；
  - `isAccessibilitySize` 0 处，`dynamicTypeSize(` 上限 16 处，`ViewThatFits` 4 处；
  - `accessibilityIdentifier(` 15 处，`accessibilityLabel(` 124 处；
  - `lineLimit(1)` 303 处，`minimumScaleFactor` 26 处（对应「按钮文字不换行」的约定）。

### 5.2 逐项对照

| 维度 | removed | Persona | 【判断】谁更好 / 借鉴点 |
| --- | --- | --- | --- |
| 颜色 token | 两套调色板（飞书蓝灰、Herdr 森林绿），没有统一的语义层 | 5 风格 × 语义色，状态色单独成表，对比度规则写成文字 | Persona 更完整；缺的是一份机器可读的唯一来源，以及自动的对比度检查 |
| 字体 | 没有 token；框架部分固定字号，正文用文本样式 | 有角色 token，但几乎没人用；固定字号很少 | Persona 方向对，要把角色真正用起来，并用 lint 守住 |
| 间距 / 圆角 | 全是字面量，圆角约 8 档 | 有 token，部分采用 | 两边都需要「token + 检查」 |
| 组件 | 薄；同一功能有重复实现（两份状态行） | `Chip` 和 `CountBadge` 各只有一种画法，规则写进文档（`JV:docs/ios.md:244-246`） | Persona 的「一种东西只有一种画法」值得保留，并扩到状态件和表单件 |
| 导航 | 根页面自绘紧凑顶栏，详情页保留系统返回，用宿主测试守手势 | 对话页自绘可收起的顶栏，再手动恢复边缘返回 | Android 应该交给系统的预测性返回，不要自绘手势 |
| 无障碍 | 辅助字号下切布局（28 处），减少动态效果，计数角标读完整值 | 文本样式和图标上限做得好，但辅助字号下没有切布局 | 借 removed 的「辅助字号 → 竖排 / 隐藏装饰 / 放开行数」三招 |
| 空 / 错 / 载 | `ServiceStatusView` 加系统 `ContentUnavailableView`；错误就近显示加重试 | `EmptyStateView` 加系统组件；取消和离线类错误不显示；先画缓存、不转圈 | 合成一个「页面状态」组件族，两端同名同参数 |
| 待确认 | 词汇、图标、只读核对入口统一，从不自动重发 | Outbox 自动重发，靠 `client_msg_id` 幂等；一次性操作不排队 | 前提不同：Persona 有 hub 能保证幂等，所以可以自动重发。但 removed 那种「有名字的不确定状态」可以收成一个组件，比如「发送中 / 连上后发送 / 未生效」 |
| 原型 | HTML 原型加 README（边界、验证记录） | 没有（只有 `docs/screenshots`） | 引入 |
| 评审 | 借鉴评审 / 同机对照 / 修复清单三种文档 | 「车道」文档偏数据链路（`JV:docs/archive/lanes/`） | 引入 UI 评审模板 |
| 验收分级 | 已开发 → 协议 → 模拟器 → 真实账号 → 真机 | 没有分级；界面清单 5 行 | 引入分级和每页清单（两端各一列） |
| UI 自动化 | 没有 XCUITest；用进程内宿主测试，加隔离 QA 模拟器和 CUA 截图 | 16 个 XCUITest 加 mock hub，逐步截图，深浅色各一遍 | Persona 更自动化；可以补进程内的组件快照 |
| fixture | 合成服务、`/__fixture` 控制接口、请求统计、故障开关、魔法输入、固定上游契约 | `mock_hub.py`、`MOCK_*` 开关、`/mock/*` 读回、真实样本脱敏后做 golden | 两边思路接近。Persona 可以补：mock 的契约要和真实 hub 一起测，比如在 Go 测试里校验 mock 响应的 schema |

---

## 6. 对 Persona 重设计和 Android 复刻的建议【全部是判断】

### 6.1 设计 token：一份 JSON，生成 Swift / Kotlin / CSS

- **唯一来源**放在 `design/tokens/*.json`。格式贴近 W3C Design Tokens 社区组（DTCG）的写法（`$value` / `$type`），具体版本落地时再核实。分几层：
  1. `color.semantic`：
     - 按风格（style）× 外观（light / dark）取值的：`accent`、`userBubble`、`userBubbleText`、`pageBackground`、`card`、`cardStroke`；
     - 不随风格变的：`brand`、`danger`、`positive`、`caution`、`cautionText`、`ai`、`loop`、`countBadge`、`diff.*`、`hue.*`；
     - `fill.well`、`fill.chip`、`fill.pressed`：iOS 现在用系统的 fill，Android 没有对应物，要显式写出色值。
  2. `color.status`：五种状态 × `dot / text / tint`。iOS App、手表、实时活动共用；Android 的通知强调色和桌面小组件也用这一份。
  3. `dimension`：
     - `radius`：6 / 10 / 14 / 20 / 22；
     - `space`：2…32；
     - `minTouch`：**两端分开**，iOS 44pt，Android 按 Material 建议 48dp；
     - 手表的尺寸单独一组。
  4. `type`：按角色定义，比如 `rowTitle` 是「默认 17、semibold」。iOS 映射到文本样式（`.headline`），跟随动态字体；Android 映射到 sp 的 `TextStyle`，跟随 fontScale。固定字号的例外沿用现有的白名单：终端、灵动岛、分享卡片、飞书卡片（`JV:docs/ios.md:238`）。
  5. `motion`：state 0.25s snappy、layout 0.3s smooth；带一个「减少动态效果」开关的语义。
  6. `rules`：**把对比度底线写成机器可查的规则**，例如 `onAccent / accent ≥ 3.5`、`userBubbleText / userBubble ≥ 4.5`、`accent / page ≥ 3`、`status.text / page ≥ 4.5`。在所有风格 × 浅深色上逐一检查，不达标就让生成器失败。这是把 `JV:docs/ios.md:223-225` 从文字变成检查。
- **生成器**：写一个 `scripts/assets/gen_tokens.py`，和仓库现有的 Python 脚本风格一致。输出：
  - `ios/Shared/Generated/Tokens.swift`：纯数据 enum 要标 `nonisolated`，见 CLAUDE.md「`ios/Shared` 默认 MainActor」那条经验；
  - `android/core/designsystem/.../Tokens.kt`：`JarvisColors` 数据类、`LocalJarvisColors`、`Dp` / `TextStyle`，再映射到 `MaterialTheme.colorScheme`；
  - `prototypes/_tokens.css`：给 HTML 原型用，解决 removed 原型手抄色值的问题；
  - `docs/design-tokens.md`：自动生成的色板和尺寸表；
  - 一个 `--check` 模式：重新生成后和仓库里的文件比对，加上对比度检查，接进 pre-commit 或发版脚本。
  - 另一个选择是 Style Dictionary（Node），它有 Swift 和 Compose 的输出，但要多引入一套 Node 工具链。Persona 的工具都是 Python，所以我倾向自己写生成器，几百行以内。
- **守住采用率**：用简单的 grep 规则，配一个白名单文件，挂在 pre-commit 或 CI 上，禁止在 token 文件之外出现：
  - `Color(red:` / `Color(light:`、直接用 `.orange` / `.red` 这类系统色；
  - `cornerRadius: <非零字面量>`；
  - `font(.system(size:`（白名单除外）；
  - Kotlin 里的 `Color(0x…)`、`<数字>.dp` 圆角、`<数字>.sp`。
  - 以 5.1 节的计数为基线，只许减少，不许增加。
- **风格在 Android 上怎么落**：液态玻璃是 iOS 26 才有的材质。建议把「表面处理」也做成 token，取值比如 `glass / solid / soft / hairline`，每端各自实现：Android 用半透明加模糊（Android 12+ 的 RenderEffect）或者色调层级来近似。不要追求两端像素一致，只要求语义和对比度一致。Android 第一版可以只做 2 到 3 套风格，比如飞书和 Claude，先看用户要不要。

### 6.2 组件库

- iOS：把 `Theme`、`StatusPalette`、`Styling`、`Components` 抽成一个本地 Swift Package（比如 `JarvisDesignSystem`），遵守 removed 的边界：不依赖 AppModel，不发网络请求（`BH:docs/ARCHITECTURE.md:45`）。`CloseAgentSheet` 这类依赖 AppModel 的组件改成纯视图加回调。
- Android：建一个 `:core:designsystem` 模块。**组件名、参数、状态和 iOS 一一对应**；每个组件写一页规格（尺寸、状态、读屏名称规则、最小触控区、是否允许换行），放在 `docs/components/`。
- 两端都做一个 **组件目录页**：iOS 用 DEBUG 参数 `-openCatalog YES`，Android 用 debug 构建里的一个 Activity。人工看和截图测试都用它。
- 第一批组件，取两个项目的长处：
  - **基础**：Surface（按风格的表面处理）、Card、Panel、页面背景、细分隔线。
  - **按钮**：Primary / Secondary / Destructive / IconButton。文字单行（CLAUDE.md 的约定）；最小触控区；IconButton 必须带读屏名称（removed 的 `WorkspaceIconButton` 做法）；按钮内的进度态，比如「正在保存…」加禁用（removed 的做法）。
  - **状态**：StatusDot、StatusLabel、Chip（中性 / 着色 / 描边）、CountBadge（视觉上 99+ 或省略，**读屏读完整数字**，借 removed）、SessionAvatar、MonogramAvatar（名字哈希取色，借 removed 的 `WorkspaceAvatar`）。
  - **页面状态族**：EmptyState、ServiceState（整页：未配对、连不上 hub）、InlineError（就近显示加重试）、Notice（caution / info）、CachedNote（「显示的是本机缓存 · N 分钟前」）、ConnectionTitle（标题位显示状态），以及 **PendingState**：把「发送中 / 连上后发送 / 没有生效 / 待核对」的文案和图标定成一张表，两端共用。
  - **流程**：StepProgress（分段进度，带时间戳；不显示假百分比；减少动态效果时停动画；等用户确认时暂停）、ConfirmSheet（`CloseAgentSheet` 的模式：是什么、在哪、后果、红色主按钮、取消）、FingerprintCard（等宽、可复制、区分「首次 / 已更换」）。
  - **表单**：FormSection（小标题加面板，removed 的 `SSHKeyFormSection`）、LabeledField（读屏名称和键盘类型）、SecretField（失焦后隐藏、`privacySensitive`、离开页面清空）。
  - **产品件**留在各自的 feature 模块里，但只准用 token：ComposerRow、WorkLine、四种交互卡片、DiffView、MonoText、TerminalKeyBar。

### 6.3 导航和无障碍的跨端规则

- 根页面可以用紧凑的自绘顶栏，连接状态放标题位（Persona 已有）。详情页用系统导航和返回：
  - iOS 维持现状；新页面尽量像 removed 那样保留系统返回，用宿主测试守住手势。
  - Android 用系统的预测性返回加 Navigation Compose，不自绘边缘手势。
- 「辅助字号」规则写进组件规格，两端同一份：
  - 触发条件：iOS 是 `isAccessibilitySize`，Android 是 fontScale 超过某个阈值，阈值待实测确定。
  - 触发后：横排的分段控件改竖排；隐藏行首装饰图标；放开标题和路径的行数；按钮放不下就改成图标、缩短文案或收进菜单（不许换行）。
- 「减少动态效果」：iOS 用 `accessibilityReduceMotion`；Android 读系统的「移除动画」设置，在 designsystem 里提供一个 `LocalReduceMotion`。
- 读屏：装饰图标隐藏；计数读完整值；分段进度只读一行文字状态（removed 的连接进度就是这样做的）。

### 6.4 测试与截图验收

| 层 | iOS | Android | 共享的东西 |
| --- | --- | --- | --- |
| token | `gen_tokens.py --check` 加对比度检查 | 同一个 | tokens JSON |
| 展示规则单测 | Swift Testing 参数化，像 removed 的角标测试 | JUnit 参数化 | **共享的 golden JSON 用例**：输入是 item 或事件，输出是行或文案；角标规则、`Fmt.relative`、`chatLines` / WorkLine 拆分都可以这样做，两端跑同一批用例，逻辑对齐 |
| 组件快照 | 组件目录 × {浅, 深} × {选定风格} × {默认, 大, 辅助字号} × {窄 320, 常规}。在 UIWindow 里渲染后截图，或者用 swift-snapshot-testing。注意 `ImageRenderer` 画不出 ScrollView（`JV:docs/ios.md:126`） | 同一组参数。JVM 上跑 Roborazzi（Robolectric 渲染，支持交互和对话框），配合 ComposablePreviewScanner 扫 `@Preview`；备选 Paparazzi（layoutlib）或官方的 Compose Preview Screenshot Testing（搜索结果显示还是 alpha）。版本落地时核实 | 只用编造的数据（隐私）；基线更新必须显式执行，比如 `record` |
| 流程 UI 测试 | 现有 XCUITest 加 `mock_hub.py`，用 `/mock/*` 读回断言 | Compose UI test。JVM 上用进程内的假 hub；仪器测试通过 `adb reverse` 连同一个 `mock_hub.py` | **mock hub 和它的开关两端共用**；在 Go 测试里校验 mock 响应符合真实 hub 的 schema，防止 mock 和真实接口漂移（这是 removed「固定上游契约」的思路） |
| 真实链路 | 本机测试 hub（假 HOME、独立库），不连线上 hub | 同一个 | `JV:docs/testing.md:131-136` 的流程 |
| 性能 | App 内探针（fold / freeze / frames） | JankStats 或 Macrobenchmark（落地时核实） | **数字必须实测**；样本 ≥ 10 才能当基线 |
| 人工验收 | 每页清单加分级 | 同一个 | 见 6.5 |

- 截图评审：每个 UI 改动跑一遍截图集（浅深色 × 字号 × 两端），拼成一张对照图给用户看；用户认可后再更新基线。截图只来自 mock，真实数据的截图只放在本机被忽略的目录里，和 removed 一样。
- 沿用 Persona 已有的好规矩：先写会失败的 UI 测试；DEBUG 参数直达页面（Android 对应 `adb shell am start` 加 extras 或 debug deep link）；隐私扫描要覆盖 Android 目录。

### 6.5 评审和验收流程（给 Persona 定的模板）

1. **原型**：`prototypes/<主题>/`，用 `_tokens.css`；手机框做 iPhone 393 和 Android 约 360dp 两种；深浅色；窄屏；减少动态效果；把异常场景做成按钮；README 写四块：范围、可体验、和原生的边界、验证记录。
2. **评审文档**：`docs/reviews/<日期>-<主题>.md`。
   - 写明依据：参考的是什么，钉在哪个版本。
   - P0 / P1 / P2 表，列为「部位 / 现状 / 参考 / 调整方向」。
   - 数据问题和视觉问题分开写。
   - 写明不借鉴什么、本轮不算通过什么、落地顺序。
3. **用户认可**后，iOS 和 Android 落地。
4. **对齐和验收表**：`docs/parity.md`，每行一个页面或功能。
   - 列：iOS 等级、Android 等级、证据位置、待验项。
   - 等级：已开发 → 单测通过 → mock 截图通过（浅深 × 字号）→ 测试 hub 通过 → 真机通过（iPhone / Android / 手表）。
   - 每页核查清单直接用 removed 那份（3.3 节），再加三项：两端一致、按钮不换行、链接在 App 内打开。
5. **证据**放在仓库外的固定目录，路径写进 `CLAUDE.local.md`；仓库里只放数字和 mock 截图。

---

## 7. 没查清 / 待核实

- removed 的 `.local-services/validation/*/QA.md`、截图和 stats 在本机不存在，只能从文档引用里倒推 QA.md 的格式。CUA 怎么操作 QA 模拟器，仓库里没有脚本，看不出能不能复现。
- 没有在浏览器里运行 removed 的 HTML 原型，没看实际效果，也没截图，只读了源码和 README。
- removed 在 iOS 27.1 上的自适应 tab（依赖 `toolbarVerticalEdge`）没有实际验证，只依据代码和 VALIDATION「Duo 主 Tab」一节的记录。
- 没有深入看 removed 的 `MessagingFeature` 聊天正文组件（气泡、富文本），也没看 PeopleFeature 等飞书页面的视觉细节。
- Persona 文档提到的 themes-fix 对比度报告（`JV:docs/ios.md:223`）不在仓库里，对比度数字没有复核。
- Android 截图工具（Roborazzi / Paparazzi / Compose Preview Screenshot Testing）的版本和成熟度只做了一次网页搜索；第三方的速度对比结论互相矛盾，**没有实测**，不引用任何速度数字。DTCG 规范和 Style Dictionary 的现状也没有核实。
- 没有看 Persona 手表端和实时活动的设计细节，只知道它们共用 `StatusPalette`。

## 附：外部参考（网页搜索，2026-10-30）

- Compose 截图测试库对比（第三方文章，结论各异）：<https://medium.com/@natalia.kulbaka/comparing-snapshot-testing-libraries-paparazzi-roborazzi-compose-previews-screenshot-testing-b7c3b47f7f59>、<https://medium.com/@papppali2000/comparison-of-android-snapshot-testing-libraries-paparazzi-vs-roborazzi-vs-compose-preview-3e506d2d9c05>
- 截图测试示例仓库（列出的版本：CPST 0.0.1-alpha16、Paparazzi 2.0.0-alpha04、Roborazzi 1.70.0）：<https://github.com/sergio-sastre/Android-screenshot-testing-playground>
- ComposablePreviewScanner：<https://github.com/sergio-sastre/ComposablePreviewScanner/wiki>

## 附：removed `scripts/` 按用途分组【事实，`git ls-files`】

一共 157 个文件：
- 顶层脚本 84 个：
  - `check-*-fixture.mjs` 60 个：检查合成服务的行为和故障模式是否自洽，打印证据 JSON；
  - `check-*-upstream.{mts,mjs}` 22 个：直接调用钉在某个提交的上游源码，用 fastify inject 做契约对照；
  - 网关接口审计脚本（`audit-*.mjs`）：用 AST 扫描上游源码，生成接口清单，输出到 `docs/audits/*.json`；
  - `make-qr-ui-fixtures.swift`：生成二维码相关的 UI fixture。
- `fixtures/` 下：72 个合成服务模块，加 1 个 `README.md`（各验收场景的开关、控制接口、统计字段说明）。
