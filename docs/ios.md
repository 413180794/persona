# iOS / watchOS App

XcodeGen 工程 `ios/project.yml`（`Jarvis.xcodeproj` 不入库）。iOS 26 / watchOS 26，Swift 6，默认 MainActor 隔离。依赖 MarkdownUI。
Bundle ID `com.chirpet.jarvis`、`com.chirpet.jarvis.watchkitapp`。界面中文。

## 信息架构

| Tab | 内容 |
| --- | --- |
| 对话 | **Jarvis 助手**：example-machine 上 `~/.jarvis/assistant` 里**无头运行**的 Claude（`claude -p` + stream-json，不进 herdr；逐字流式；
右上角随时切换启动器 / 模型，下一条消息起用 `--resume` 换过去；首页模型标签决定新对话用哪个，默认在设置里改），CLAUDE.md 由 hub 生成，告诉它怎么访问各台电脑（ssh）、飞书等公司数据（lark-* / provider-* skills）、iPhone（phone_*）和邮件（mail_*）。首页是问候 + 可用设备 + 6 个建议；发出第一句立即显示气泡和「正在准备」，后台起会话。对话里隐藏思考，工具调用折成一行「用了 N 个工具 · 电脑、iPhone…」。左上角历史对话，右上角新对话；首次启动自动接上最近一段 |
| 开发 | 左上角菜单选分组方式（`DevGrouping`，记住）：按项目（默认，下面描述的样子）/ 按状态（需要你 → 完成未看 → 工作中 → 已看，已看一段默认折叠）/
按机器（每台机器一段，标题有在线状态和状态计数，段内按状态排、空闲折叠）；不按项目时每行前面带「空间 ›」（按状态时还带机器名）。按项目时：原来的会话列表（不含助手对话）：「需要你处理」卡片 → 置顶的 Agent（空间内的 Agent 按 需要你 → 完成未看 → 工作中 → 空闲 排，同状态按 tab 顺序；`AgentState.priority`，手表同样） → 置顶的空间 → 进行中 → 刚完成 → 屏蔽但有 Agent 在跑的空间（弱显示）→ 空闲的空间（折叠，屏蔽的空闲空间也在这里）；搜索、按机器筛选、左滑置顶 / 中断、右滑关闭；长按空间标题置顶空间。置顶存在 hub（`/api/v1/pins`）。`/loop` 驱动的 Agent 带「⟳ 下次 12:20」标记、不折进空闲；轮次结束但还有后台命令 / 子代理在跑的 Agent 带琥珀色「⚙ 后台 N」，同样不折进空闲，它所在的空间也不算空闲空间，长按可停止循环。改名（空间：标题 ⋯ / 空间页菜单「重命名空间」，同步改 herdr workspace；Agent：长按 / 对话页「…」「重命名…」，一次改 herdr tab 名和 Claude 会话名，Mac 上 herdr 和 claude --resume 看到的一致）。屏蔽（Agent：长按或对话页「…」菜单；空间：标题右侧的 ⋯，或空间页右上角菜单——列表分组标题上的长按菜单触发不了）：不再推送通知（空间屏蔽时，其中的 Agent 可以单独「开启通知」，反之亦然——每个 Agent 的开关叠在空间的设置上，`Pins.togglingMute`）；列表里弱化（状态色变灰、🔕、不透明度 0.8——比工作中的弱，不比空闲的空间淡），在跑的照常显示不折叠，屏蔽且没在跑的空间就是普通的空闲空间；开发 tab 角标不计（`/api/v1/mutes`） |
| 信息 | 照飞书手机端「消息」页：顶部置顶栏（头像 + 未读角标，一行 6 个 + 展开）、消息 / 未读 N / 标记 三个视图；左上角「分组」抽屉：消息、已完成、标记、全部标签，最后是邮件。行样式同飞书：头像右上角角标（免打扰灰色、99+）、名字后的外部 / 公开 / 机器人标签、群聊预览带发送者、[有人@我]、免打扰铃铛。消息列表不重复显示置顶会话、不显示已完成；带标签的会话按标签显示规则（读完即隐去，飞书服务端规则）。右滑「完成」，左滑 标为未读 / 置顶 / 免打扰，长按 置顶、标为未读、标记、标签、免打扰、完成。聊天页同 lark-connector：所有人（包括自己）的消息都在左侧带头像，群聊里连续消息的第一条显示名字，自己的气泡蓝色（#D1E3FF / 深色 #133063）；点头像或名字看资料卡（封面、签名、状态、发消息、部门 / 上级 / 邮箱等，上级可继续点开）；点表情回应切换自己的回应，「☺︎」或长按「表情回复」打开常用 + 全部表情；removed分享链接渲染成卡片，点开看帖子全文、图片和评论。进群从最早一条未读开始（「以下为新消息」分隔线），往下滚加载更新的，只把上过屏的消息上报为已读；「N 条新消息 / 回到最新」「有人@你」跳转。卡片渲染等见下文「飞书消息渲染」；邮件：合并收件箱 / 单账号、详情、回复、写信、「让 Claude 处理」、邮箱管理（右上角菜单），「AI 一键添加」邮箱 |
| 设置 | 助手（默认启动器 / 模型）、Claude Code 设置（各机器 `~/.claude/settings.json` 的白名单项：离开回顾 recap、思考强度、总是思考、回复语言等，可一键应用到所有机器）、机器（二级页：在线状态、会话数、版本、启动器、重连）、手机数据开关和授权、通知、重新配对 |

对话页：

- 消息：用户气泡（珊瑚色）、助手 markdown（无气泡，阅读优先）、思考（折叠一行）、工具步骤组（>2 步折叠成「N 个步骤 · 运行 5 · 读取 3」，
  永远露出最后一步）、待办清单（TodoWrite 直接渲染）、Edit 的红绿 diff、Bash 命令和输出、图片。
- 图片：缩略图点开全屏查看器（`ImageViewer.swift`，UIScrollView 缩放：双指 / 双击放大、拖动、1x 时下拉关闭；同一工具结果里多张图左右翻页；
  保存到相册（仅添加权限）、分享、拷贝）。
- 文件：消息里出现的路径显示成可点的文件标签，工具卡片里 Read / Edit / Write 的路径也能点；在会话所在机器上读取
  （`GET /api/v1/machines/{mid}/file`，最多 2 MB，拒绝 iCloud 目录），Markdown 渲染 / 代码带行号 / 图片 / PDF / 目录浏览。
- 同空间同伴条（开发页）：同一 herdr workspace 的其他 tab 列在顶部，点了直接切换（`docs/agent-states.md`）。
- 离开回顾（recap）：Claude Code 的 away_summary 显示成「离开期间回顾」卡片，去掉了「(disable recaps in /config)」；关掉它在 设置 › Claude Code 设置。
- 展示逻辑（助手和开发统一，`chatLines` + `WorkLine`）：一轮按「消息」切成若干段，每条消息自成一段对话：
  你的消息 → 一行灰色的工作行「5 个工具 · 思考 1 · 回复 1 · 4 分 17 秒 ›」→ Agent 在下一条消息之前说的最后一段话 → 下一条消息……
  进行中和已完成长得一样，只是进行中的那段工作行带转圈、计时、终端按钮（「正在工作 39 秒 · 2 个工具」）。
  已完成的一轮如果有多条消息（排队发的）：收起时多条消息合成一个气泡（浅色短分割线隔开，`MergedUserBubble`），下面一行工作行、一条最终回复；
  点工作行展开后按消息拆开成上面的一来一回，顶部「已按 N 条消息拆开 · 收起」。用户发的图片在消息下方显示成方形缩略图（`SentPhotos`），
  气泡里去掉 Claude 的「[Image #N]」占位。
- 进入位置：完成未看的会话定位到最后一条输入之后（从这次回复的开头读）；其他会话回到上次离开时屏幕最上面那一行（`chat.read.<会话>`，
  离开时在最新处就清掉，下次直接到最新）。
  这样不管收起还是展开，每条输入对应的输出都紧跟在它后面；不再把一轮的多条消息堆在一起，也不再编号。
  - 工作行折叠的是这段里除最后一段话之外的一切：工具调用、思考（新版 Claude Code 把旁白写成 thinking）、系统提示（后台命令完成等）、
    中间的回复。点一下在原地按顺序列出每一步（左侧一条细线），再点同一行收起。
  - 工作行文字不截断：放不下时换成短格式（只留工具数和用时）。
- 布局：列表在上、输入区（交互面板 / 循环 / 后台 / 排队 / 输入框）在下，用 VStack 排、不再叠在列表上；键盘弹起整页变矮，最新消息始终贴着输入框。
  列表上方垫 1pt 空视图，让滚动视图不贴顶部安全区，这样它完全没有安全区（翻转后安全区会落在错的一端），也不必忽略安全区、自己补边距。
- 其他会话发来的消息（`Item.from`）：左侧灰色卡片「来自 名字」。
- 列表是倒置的（`chatScroll`：ScrollView 和每一行各翻转一次）：最新消息在滚动偏移 0，进入、跟随新消息、点 ↓ 都是精确跳到 0，
  不再按懒加载的估算高度去找底部（偶发白屏的根源）；更早的历史加载在另一端，不推动正在看的位置。看历史时（`followsEnd` 关）新来的行先不插进
  列表（`frozenAt`），回到最新再出现，所以不会把正在看的内容挤走。不要用 `defaultScrollAnchor(…, for: .sizeChanges)`：翻转后它锚在远端，
  一变高就把视图带进没渲染的区域（白屏）。倒置后安全区会落错端：滚动视图忽略纵向安全区，用 `contentMargins` 按实际 inset 补留白；
  iOS 26 的边缘模糊会按未翻转的视图算区域、把整页蒙住，所以关掉（`scrollEdgeEffectHidden`）。
- 本地缓存（`StateCache`，Caches/state）：机器 / 会话 / 待处理交互 / 助手设置（`dev`）、信息页各列表 + 置顶 + 标签 + 标记 + 邮件（`inbox`，
  `InboxCache`）、每个飞书会话最近 80 条（`lark-chat-<id>`）。冷启动、断网、切 tab 都先画缓存，后台静默刷新；有缓存就不显示转圈。
- 历史：按轮次分页，首次进入和每次滑到最上面都取两轮完整的「用户输入 + 回答」（`turns=2`），不按条数切；顶部转圈自动加载，
  没有「加载更早」按钮。每个会话的最后两轮存在本机（`ChatCache`，Caches/chats，超过 6 MB 去掉图片），打开 / 切换会话先画缓存再合并服务器数据，
  不出现空白。轮次开始 / 结束、自己发了消息、切回「对话」选项卡时都回到底部（一轮结束会收起，内容骤短，不跳会停在空白处）。
  「贴底」状态（`followsEnd`）：在最底部时开；手指一拖列表就关（看历史时任何更新都不动位置）；回到底部、点 ↓、自己发消息时再开。
  只在贴底时，新内容到来后才跳到底：先按底部边缘跳、再按底部哨兵的 id 对齐，等新行布局完再跳（`settleAtEnd`），哨兵没真正出现在屏幕上就重试
  （单次按 id 跳进没布局过的区域会落在更早的某条输入上——「动不动跳到之前的输入」就是贴底判断写成「滑过就不重试」造成的）。
  进入页面时的定位只做一次，从全屏终端等返回不再重复。LazyVStack 按估算高度定位，行高差异很大时会停在没渲染的地方（白屏）；`atBottom` 初始为 false，
  万一仍白屏，「回到底部」箭头会出现。滑到顶自动加载更早的一页，只在用户手动拖动过列表之后才生效（进入时 LazyVStack 可能先布局顶部那行，
  曾导致一进页面就连锁加载整段历史）。
- 渲染：每条消息、计时行、叙述、工具计数、答复都是 LazyVStack 里的独立行（`ChatLine`）。进行中的一轮可能有几百步，
  做成一个整块 VStack 时整页卡住、空白好几秒。
- 取消不是错误：离开页面 / 新请求替换旧请求造成的取消（iOS 文案「已取消」）一律不提示（`Error.shown` / `isCancellation`）。
- 发送：点发送立即清空输入框、消息以「发送中」气泡上屏（`ChatStore.pending`），请求在后台发；transcript 里出现同样的话
  （或进了排队列表）就换成真消息，失败变红可重发 / 删除。不等网络，任何地方发都不卡。
- 发的图片：hub 把路径用 `<attached_images>` 标签附在消息后面，App 解析出来在文字上方显示方形缩略图（经 file 接口从会话所在机器读，
  内存缓存），点开全屏查看；气泡里不显示这个标签（旧的「相关图片：…」/「（我从手机发来了图片…）」也兼容）。
- 等宽文本（文件、diff、终端，`MonoTextView`）：UIScrollView 里放一个按最长行实测宽度、不滚动的 UITextView，不换行，横竖都能滑，能选字。
- 长按消息（`MessageMenu.swift`）：像微信一样在按下的位置上方弹出深色操作条（复制 / 选择 / 引用 / 分享，自己的消息多一个编辑），
  不再先把消息浮起来；按的位置靠近导航栏时放到下方。点任意处或手指拖动列表关闭（内容流入导致的程序滚动不关）。
  「选择」打开可任意选取的纯文本页；「引用」把消息以 `> ` 引用放到输入框前面；「编辑」把自己的旧消息放回输入框。
- 排队消息：Claude 工作中发的消息不进对话记录，显示在输入框上方的「排队中」列表；每条可「撤回编辑」（回到输入框）、拷贝、删除。
  实现照 TUI：↑ 把全部排队消息拉回输入框（transcript 记 popAll），清空输入框后把保留的重新排队（`POST …/queue {"keep":[…]}`）。
- 开发列表：忙碌的空间里只显示需要你 / 工作中 / 完成未看的 agent，空闲的折成一行「还有 N 个空闲」，点开展开。
- 「回到底部」箭头：用列表末尾的哨兵视图判断是否在底部（滚到底就隐藏）。
- 会话页选项卡（开发会话，`Views/Session/`）：导航栏下一排「对话 · 终端 · Git · 文件」，Git 只在仓库里出现（角标是改动数），
  文件在知道目录时出现；助手对话没有选项卡。
  - 终端（`TerminalPanel`）：任何状态都能看。最后 200 行每秒刷新；往上滑到顶附近自动加载前一段 200 行（瀑布式，不一次取全量），
    一直到 herdr 保留的最前面。每段用「当前最上面 20 行 + 已加载行数」去服务端缓冲里定位（`POST …/screen/before`）：Claude 重绘会把同一屏幕反复推进滚动历史，同样的内容可能出现多处，取离「末尾往前已加载行数」最近的那处；终端仍在输出也不会错位；
    看历史时暂停实时刷新，「回到最新」恢复。herdr 每个窗格只能读出约 1000 行（`pane read` 不管 --lines 多少都封顶，也没有偏移 / 滚动接口），
    到头提示「herdr 只保留最近约 1000 行，更早的过程在「对话」里」——完整历史在 transcript。新版 Claude（Mac）是全屏界面，窗格本身没有滚动历史。
    按键栏只有 ctrl（粘滞，作用于下一个键）、esc、tab 和一个游戏手柄图标，点开浮出方向键 + 回车十字键盘；实时状态条上的终端图标打开同一个面板的全屏版。
  - Git（`GitPane`）：分支、上游 ↑↓、暂存数；改动列表（状态字母、+/− 行数、已暂存 / 部分暂存），点开看 diff（新旧行号、红绿底、不换行横滑，
    同时有暂存和未暂存时可切换）；提交历史（分页加载更早），点开看提交说明和每个文件的 diff。全部只读。
  - 文件（`FilesPane`）：从会话目录开始逐级进入，面包屑跳回任意一级；目录在前，显示大小和修改时间、Git 改动标记（目录里有改动显示圆点）；
    隐藏文件默认不显示（菜单里切换）；点文件用文件查看器打开。
- 循环（`/loop`）：输入框上方「循环中 · 每 30 分钟 · 下次唤醒 11:56（还有 4 分钟）」，点开看循环的任务，「停止循环」确认后给 Agent 发停止指令；
  每次唤醒在对话里是一行「⟳ 循环唤醒 21:11 …」，不是一条用户气泡。
- 子 Agent：Agent 工具启动的子 Agent 不再算后台任务。对话页选项卡下多一排「主 Agent / 运行中的子 Agent」（转圈；只在有子 Agent 运行时出现，完成的不列，从工作行里的「查看子 Agent」进入），点一个就用同一套
  消息 / 工具界面看它的对话（只读，底部「子 Agent · Explore · 运行中，只读 · 回到主 Agent」）；展开的工作行里每个 Agent 调用下有「查看子 Agent」。
  开发列表里有子 Agent 在跑的 Agent 带「👥 子 Agent N」，不算空闲。会话 id 为「<主会话>@<agent id>」，历史 / 实时 / 分页和普通会话完全一样。
- 后台任务：Claude 用 `run_in_background` 跑的命令 / 子代理还没回报时，输入框上方显示「N 个后台任务」（轮次结束后也在），展开看描述、命令和已运行时间，
  点一条打开它的输出文件。
- 底部 tab 栏常驻，不随滚动收起。
- 实时状态条：claude 工作时显示 spinner 行（「Cogitating… 12s · ↓3.2k」），点开看输出预览；右侧进终端。
- 交互面板：贴在输入框上方。提问（多题切换、单选 / 多选 / 自己写，单题单选点一下就提交）、计划（markdown 预览 + 全屏、批准并自动执行 /
  批准并自动接受编辑 / 打回写意见）、授权（允许 / 拒绝 / 本会话总是允许）、终端（屏幕快照 + 解析出的编号选项 + 按键面板 + 发文字）。
- 输入框：照片（最多 4 张，压缩到 1600px）、快捷指令（继续、/compact、/context、提交并推送、汇报进展），工作中变成红色停止键。
- 导航副标题：机器 · 模型 · 状态。

## 设计语言

- 界面风格（设置 › 界面风格，`AppStyle`，存 UserDefaults `ui.style`，默认液态玻璃）：

  | 风格 | 主色 | 表面 / 按钮 | 自己的气泡 | 页面 |
  | --- | --- | --- | --- | --- |
  | 液态玻璃 | 系统蓝 `#007AFF` | Liquid Glass | 浅蓝 `#DCEAFF` 黑字（深色 `#1D3557`） | 系统 |
  | Claude | 珊瑚 `#D4683F` | Liquid Glass | 珊瑚底白字 | 系统 |
  | 黏土 | 薰衣草紫 `#7A5AF8` | 实心软块：左上高光 + 右下彩色阴影、白描边，圆体字，圆角 24 | 淡紫渐变白字 | 淡紫 `#EFEAF7` |
  | Island | 青绿 `#0E9F8E` | 白色悬浮卡片 + 大而柔的阴影，圆角 26 | 墨黑底白字（深色反过来） | 雾青 `#E6F1EF` |
  | 飞书 | 飞书蓝 `#3370FF` | 白卡片 + 发丝描边、无阴影，圆角 10，按钮 bordered | 浅蓝 `#D1E3FF` 黑字 | 浅灰 `#F5F6F7` |

  `Theme` 的颜色按当前风格取值（`Theme.accent(style)` 可取任意风格，设置里的预览卡用它）；面板用 `.surface(tint:interactive:in:)`、
  按钮用 `.themedButton(prominent:)`、列表页面 `.themedPage()`、plain 列表的行 `.themedRow()`，不要直接写 `glassEffect` / `.glass`。
  切换风格时 RootView 以风格为 id 整棵重建（token 是静态的），当前 tab 和打开的会话本来就会记住。飞书页面的框架色
  （`LarkColor.blue` 链接 / 选中 / 新消息线、自己的气泡、卡片底和描边）跟着风格走，卡片内容的飞书具名色板不变。
  调试：`-ui.style glass|claude|clay|island|lark`（UserDefaults 参数域，Release 也认，不算 DEBUG 开关）。
- 状态色不随风格变：等你红、工作中跟主色、完成未看绿、刚完成蓝、未运行灰。
- 触感：发出 / 回答成功 success，新交互 warning，出错 error。
- 图标：夜色背景 + 珊瑚光环 + 四角星（`scripts/make_icon.py`，含 dark / tinted）。

## 数据流

- `AppModel`（`Jarvis/App`）：配对信息存 Keychain；`LiveConnection`（WebSocket，指数退避重连，重连后按 `since_seq` 重新订阅）；
  每个会话一个 `ChatStore`（按 id 去重、按 seq 排序、`buildRows` 生成展示行）。
- 进入对话页 `subscribe`，离开 `unsubscribe`；hub 推 `items` / `screen` / `sessions` / `interaction`。
- 手机工具：hub 推 `tool_call` → `PhoneToolRegistry.call`（写操作先弹确认）→ `tool_result`。连上就 `register_phone` 声明工具（`docs/phone-tools.md`）。
- 推送：配对后申请通知权限并注册 APNs，`POST /api/v1/devices`；hub 有 `apns.json` 时对新交互和 agent 完成推送：会话从 working 变成 done / idle，4 秒后仍未恢复工作就推「✅ 标题」，
  副标题「机器 · 空间 › 标签」，正文是最后一条回复。前台且正在看该会话时不弹。模拟器自动化（`-pairURL`）不申请通知权限、不注册成手机。
  点通知打开对应会话：`UNUserNotificationCenterDelegate` 用 completion handler 版本，切回主队列处理再回调（async 版本在后台执行器上返回，
  UIKit 在非主线程收到完成回调，点通知冷启动会崩）。
- 手表配对：iPhone 在 WCSession 激活后再推一次配对（激活前推会被丢掉），同时 transferUserInfo 保证送达；手表未配对页有「从 iPhone 同步」
  （sendMessage 请求，iPhone 回复配对）；iPhone 设置里有「同步到 Apple Watch」。
- 手表：iPhone 经 WatchConnectivity 把配对信息和摘要（运行中会话、待处理交互）同步给手表；手表打开时每 8 秒直接请求 hub 刷新，
  回答 / 发消息 / 中断也直接调 hub。

## DEBUG 启动参数（仅 Debug，Release 检查不含这些字符串）

`-pairURL jarvis://pair?...`（免扫码配对、不弹通知授权）、`-openSession <machine>/<session>`、
`-showNewChat YES`、`-tab dev|mail|settings`、`-openAssistant <machine>/<session>`、`-newAssistant YES`、`-openFirstImage YES`（打开会话里第一张图的查看器）、`-openTerminal YES`、`-showPad YES`、`-lark-connector <feedId>`（直接打开飞书会话，`-lark-connectorName` 标题，`-lark-connectorTopic YES` 话题群，`-lark-connectorAt <position>` 打开到某条消息）、`-larkFocus <messageId>`（滚到该条消息）、`-inboxDrawer YES`（打开信息页的分组抽屉）、`-larkProfile <userId>`（会话里弹出此人资料卡）、`-larkPicker YES`（对最后一条消息弹表情回复）、`-larkMoments <postId>`（打开removed帖子）、`-tab workbench -openWorkbench <screen>`（工作台直达某页，见 docs/feishu.md）；信息页的分组 / 视图可用 `-inbox.group done|flagged|mail|label:<id>:<名字>`、`-inbox.tab 未读` 直接指定（@AppStorage 读启动参数）、`-larkTop YES`（飞书会话从最早一条开始显示，截长卡片用）、`-addMail YES`（打开管理邮箱 › 添加邮箱）。`scripts/sim.sh` 封装了编译、安装、配对、启动（`SIM=<udid>` 换模拟器；多个 agent 同时开发时各用各的模拟器，否则会互相覆盖安装）。
检查历史渲染：`-loadAll YES`（加载全部历史并滚到顶）、`-fromSeq <seq>`（从包含这条用户消息的那一轮开始显示，长历史里滚动定位会漂）、
`-dropUsers YES`（隐藏那一轮的用户气泡，一屏看全过程行和答复）。`-openPane terminal|git|files` 打开会话页的某个选项卡；
`-simulateTap <machine>/<session>` 启动 4 秒后走一遍「点通知」的处理；`-showMenu YES` 在最后一条回复（配合 `-fromSeq` 是那一轮的回复）上打开长按操作条。

## 飞书消息渲染（`Jarvis/Views/Inbox/Lark/`）

照搬 lark-connector（github.com/Eric-2369/lark-connector，`src/ui/message-renderers/`）的消息渲染，数据是 hub 原样转发的 `parts`：

| 文件 | 对应 lark-connector | 内容 |
| --- | --- | --- |
| `LarkModels.swift` | contracts `MessageView` / `MessagePart` | 线上类型、API |
| `LarkMedia.swift` | `removedimage` / `image-viewer` / `message-video` / `chat-file-preview` | 带鉴权的媒体加载（内存 + 磁盘缓存、ImageIO 降采样）、表情目录、全屏图集、播放器、Quick Look |
| `LarkInline.swift` | `card-style.ts` / `card-markdown-text.ts` | 飞书色板、行内 run（加粗 / 斜体 / 删除线 / 行内代码 / 链接 / 术语虚线 / 内嵌表情图）、流式布局 |
| `LarkMessageContent.swift` | `index.tsx` `renderPart` / `rich-blocks.ts` | 富文本块树（段落、有序 / 无序列表、引用、标题）、图片、贴纸、文件卡、视频、语音、代码块（轻量高亮）、系统消息 |
| `LarkCard.swift` | `card-renderer.tsx` | 卡片元素：header 主题色、markdown、column_set（权重 / 固定 / auto 列宽 + bisect / trisect / stretch 窄屏重排）、table、list、collapsible_panel、note、text_tag、icon、按钮（链接 / 回调）、select / input / picker（只读）、overflow、img、chart（Swift Charts）…… |
| `LarkSummaryCards.swift` | `SummaryCardView` | 合并转发（点开看全部消息）、群 / 个人名片、视频会议、日程、投票等摘要卡 |
| `LarkChatView.swift` | messenger 气泡 | 头像、发送者 + 机器人标、同人 5 分钟内合并、日期分隔、回复引用、表情回应、已编辑；卡片 / 图片 / 文件无气泡；话题群显示话题列表（`is_topic`），「N 条回复」点开看话题里的全部回复 |

要点：
- 行内内容拼成一个 `Text`（`Text("\(a)\(b)")` 插值拼接），表情图用 `Text(Image(uiImage:))` 嵌进行内，未加载时先显示 `[标签]`，
  加载完 `LarkMedia.emojiVersion` 变化触发重绘。
- 卡片里 `plain_text` 在 markdown 上下文要解 `&nbsp;` 等实体和 `<br>` / `<a href>` / `<font color>`（飞书网关留下的标记）。
- 卡片回调按钮只在 `actionMethod == 2` 时可按（走 lark-connector 的 card-actions 接口）；select / input 等表单控件只显示当前值。
- 自定义 `Layout` 拿到的 `proposal.width` 可能是 `.infinity`（ScrollView / 测量 ideal size 时），换算整数前必须判 `isFinite`，否则直接崩。
