# iOS / watchOS App

XcodeGen 工程 `ios/project.yml`（`Jarvis.xcodeproj` 不入库）。iOS 26 / watchOS 26，Swift 6，默认 MainActor 隔离。依赖 MarkdownUI。
Bundle ID `com.chirpet.jarvis`、`com.chirpet.jarvis.watchkitapp`。界面中文。

## 信息架构

| Tab | 内容 |
| --- | --- |
| 对话 | **Jarvis 助手**：example-machine 上 `~/.jarvis/assistant` 里**无头运行**的 Claude（`claude -p` + stream-json，不进 herdr；逐字流式；
右上角随时切换启动器 / 模型，下一条消息起用 `--resume` 换过去；首页模型标签决定新对话用哪个，默认在设置里改），CLAUDE.md 由 hub 生成，告诉它怎么访问各台电脑（ssh）、飞书等公司数据（lark-* / provider-* skills）、iPhone（phone_*）和邮件（mail_*）。首页是问候 + 可用设备 + 6 个建议；发出第一句立即显示气泡和「正在准备」，后台起会话。对话里隐藏思考，工具调用折成一行「用了 N 个工具 · 电脑、iPhone…」。左上角历史对话，右上角新对话；首次启动自动接上最近一段 |
| 开发 | 原来的会话列表（不含助手对话）：「需要你处理」卡片 → 进行中 → 刚完成 → 空闲（折叠）→ 最近；搜索、按机器筛选、左滑中断、右滑关闭 |
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
- 展示逻辑（助手和开发统一，`ChatBlock.build` + `TurnView`，参照 Codex）：按轮次分组。一轮 = 一条用户消息 + 这一轮里 Claude 工作时吸收的排队消息
  （transcript 的 `queued_command`，`Item.queued`）。用户消息之后是一条细线标题：进行中「正在工作 39 秒」（每秒走，右侧终端按钮），
  已完成「已工作 5 分 12 秒」，点它展开 / 收起过程。过程里只显示 Claude 的正文（text），两段正文之间的工具调用和思考
  （新版 Claude Code 把工具之间的旁白写成 thinking）合成一行「调用了 3 个工具，思考 2 次 ›」（正在跑的是「正在调用工具 · 第 N 个」带转圈），
  点开才看每一步。
  已完成的轮次默认收起，只留最后一条回复，消息 1、2、3…连续排在最前；进行中的轮次默认展开，消息按发出的位置穿插。
  轮次中途的上下文压缩 / API 错误归入过程，「已中断」跟在轮次末尾；recap 单独成行。底部不再有 spinner / token 状态条。
  注意：新版 Claude Code 会把工具之间的叙述写成非空的 `thinking` 块，不能当思考直接藏掉。
- 历史：按轮次分页，首次进入和每次滑到最上面都取两轮完整的「用户输入 + 回答」（`turns=2`），不按条数切；顶部转圈自动加载，
  没有「加载更早」按钮。每个会话的最后两轮存在本机（`ChatCache`，Caches/chats，超过 6 MB 去掉图片），打开 / 切换会话先画缓存再合并服务器数据，
  不出现空白。轮次开始 / 结束、自己发了消息、切回「对话」选项卡时都回到底部（一轮结束会收起，内容骤短，不跳会停在空白处）。
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
  - 终端（`TerminalPanel`）：任何状态都能看（不只是工作中）。每秒读最后 200 行（`source=recent`）；顶部「加载更早的输出」每次多读 1000 行
    滚动缓冲，直到返回的行数少于请求（「已到最前面」，最多 20000 行），翻历史时暂停实时刷新，底部「回到最新」恢复。长输出按 200 行一块懒渲染。
    按键栏只有 ctrl（粘滞，作用于下一个键）、esc、tab 和一个游戏手柄图标，点开浮出方向键 + 回车十字键盘；实时状态条上的终端图标打开同一个面板的全屏版。
  - Git（`GitPane`）：分支、上游 ↑↓、暂存数；改动列表（状态字母、+/− 行数、已暂存 / 部分暂存），点开看 diff（新旧行号、红绿底、不换行横滑，
    同时有暂存和未暂存时可切换）；提交历史（分页加载更早），点开看提交说明和每个文件的 diff。全部只读。
  - 文件（`FilesPane`）：从会话目录开始逐级进入，面包屑跳回任意一级；目录在前，显示大小和修改时间、Git 改动标记（目录里有改动显示圆点）；
    隐藏文件默认不显示（菜单里切换）；点文件用文件查看器打开。
- 后台任务：Claude 用 `run_in_background` 跑的命令 / 子代理还没回报时，输入框上方显示「N 个后台任务」（轮次结束后也在），展开看描述、命令和已运行时间，
  点一条打开它的输出文件。
- 底部 tab 栏常驻，不随滚动收起。
- 实时状态条：claude 工作时显示 spinner 行（「Cogitating… 12s · ↓3.2k」），点开看输出预览；右侧进终端。
- 交互面板：贴在输入框上方。提问（多题切换、单选 / 多选 / 自己写，单题单选点一下就提交）、计划（markdown 预览 + 全屏、批准并自动执行 /
  批准并自动接受编辑 / 打回写意见）、授权（允许 / 拒绝 / 本会话总是允许）、终端（屏幕快照 + 解析出的编号选项 + 按键面板 + 发文字）。
- 输入框：照片（最多 4 张，压缩到 1600px）、快捷指令（继续、/compact、/context、提交并推送、汇报进展），工作中变成红色停止键。
- 导航副标题：机器 · 模型 · 状态。

## 设计语言

- 主色 Claude 珊瑚 `#D4683F`（深色 `#E8835C`）；状态色：工作中珊瑚、等你琥珀、空闲绿、刚完成蓝、未运行灰（`Shared/Theme.swift`）。
- iOS 26 Liquid Glass：工具栏、交互面板、输入框、提示条用 `glassEffect`；按钮 `.glass` / `.glassProminent`。
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
`-showNewChat YES`、`-tab dev|mail|settings`、`-openAssistant <machine>/<session>`、`-newAssistant YES`、`-openFirstImage YES`（打开会话里第一张图的查看器）、`-openTerminal YES`、`-showPad YES`、`-lark-connector <feedId>`（直接打开飞书会话，`-lark-connectorName` 标题，`-lark-connectorTopic YES` 话题群，`-lark-connectorAt <position>` 打开到某条消息）、`-larkFocus <messageId>`（滚到该条消息）、`-inboxDrawer YES`（打开信息页的分组抽屉）、`-larkProfile <userId>`（会话里弹出此人资料卡）、`-larkPicker YES`（对最后一条消息弹表情回复）、`-larkMoments <postId>`（打开removed帖子）；信息页的分组 / 视图可用 `-inbox.group done|flagged|mail|label:<id>:<名字>`、`-inbox.tab 未读` 直接指定（@AppStorage 读启动参数）、`-larkTop YES`（飞书会话从最早一条开始显示，截长卡片用）。`scripts/sim.sh` 封装了编译、安装、配对、启动（`SIM=<udid>` 换模拟器；多个 agent 同时开发时各用各的模拟器，否则会互相覆盖安装）。
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
