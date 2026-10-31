# iOS / watchOS App

XcodeGen 工程 `ios/project.yml`（`Jarvis.xcodeproj` 不入库）。iOS 26 / watchOS 26，Swift 6，默认 MainActor 隔离。依赖 MarkdownUI。
Bundle ID 来自配置（`app.bundle_id`，手表 `<id>.watchkitapp`，`docs/config.md`）。界面中文。

## 信息架构

| Tab | 内容 |
| --- | --- |
| Jarvis | **Jarvis 助手**（2026-10-30 从「对话」改名）：hub 机器上 `~/.jarvis/assistant` 里**无头运行**的 Claude（`claude -p` + stream-json，不进 herdr；逐字流式；
点顶栏正中的头像（菜单里「启动器：…」）随时切换启动器 / 模型，下一条消息起用 `--resume` 换过去，同一个菜单里还有「会话信息」；首页模型标签决定新对话用哪个，默认在设置里改），CLAUDE.md 由 hub 生成，告诉它怎么访问各台电脑（ssh）、飞书等公司数据（lark-* 等 skills）、iPhone（phone_*）和邮件（mail_*）。首页是问候 + 可用设备 + 6 个建议；发出第一句立即显示气泡和「正在准备」，后台起会话。对话里隐藏思考，工具调用折成一行「工具 N · 电脑、iPhone…」。顶栏和侧边栏见下面「Jarvis 顶栏和侧边栏」；知识库（`KnowledgeView`，2026-10-31 重做：顶部一排类型胶囊（项目 / 会话 / 人 / 文档 / 会议 / 日程，选中的带数量），右上角漏斗按标签筛选；项目分「重点」（P0 / P1）「其他进行中」「已完成」，「不是项目」收起；每行是类型图块（人和会话是首字，颜色跟重要级）+ 名字 + 一句话 + P 级 / 长短期 / 相关性 / 放得下的标签；全文搜索按条目合并，一条一行，带名字和最多两处命中、加粗搜索词，点开就是那个条目。详情顶部是图块、名字、徽章、一句话、全部标签和「问 Jarvis」「开 Agent」；下面依次是在做这个项目的 Agent、竖线时间线（新的在上，默认 6 条）、档案、按类型分组的关联（每类 4 个，可展开）、代码目录、最近 7 天的消息、目录路径；右上角「打标」手动改重要级 / 相关性 / 标签 / 项目类型和状态。截图参数：`-tab assistant -openAssistantPage knowledge`，加 `-showKnowledgeEntry <kind>/<id>` 打开某条、`-showKnowledgeLower YES` 去掉头部直接看时间线、`-showKnowledgeSearch <词>` 搜索）；第一段对话就是**主对话**（`assistant.main`，历史里标「主对话」），之后的新对话是旁支；首次启动打开主对话。其他页面「交给 Jarvis」（Agents 长按菜单、知识库「问 Jarvis」）都发进主对话（`AppModel.askJarvis`，没有主对话就新建一个） |
| 开发 | 搜索框常显（不随滚动收起，见 CLAUDE.md「safeAreaBar」）；搜索框下一排快速筛选（`AgentFilter`：全部 / 需要你 / 未读 / 工作中，带计数，点一下生效，和机器筛选、搜索叠加；不记住，回来是全部）。筛选下列出选中那一刻符合的 Agent（`kept`），之后看过的不消失，留在原位（灰点），空间和行只按时间排，方便一路往下看；重选筛选或切机器才重算。未读筛选顶部「全部标为已读」（先确认）。需要你处理卡片只在全部 / 需要你下显示，选了机器就只显示那台的。左上角菜单里「卡片 / 细列表」（`dev.compact`，记住；细列表 `CompactSlotRow` 一行：状态点、标题、tab（不按项目时「空间 › tab」）、时间。
细列表整体一套尺度 `CompactList`（2026-10-31 用户要求「整体更协调，不要一些大一些小」）：三级字号（分组标题 = 空间名 / 需要你处理 / 空闲的空间 / 状态 / 机器用 `heading`，行标题 `secondary`，
元信息 = tab、时间、计数、空闲 tab 名用 `caption`）；每一行（Agent、「还有 N 个空闲」、「收起」、空闲的空间、全部标为已读）一样高、一样的内边距，行首记号（状态点、折叠箭头、空间图标）在同一列，标题左缘对齐；
行右侧只放时间，所以时间右对齐成一列；空闲的 tab 名是一行小字，不用胶囊；空闲的空间是一行（名字、几个标签、时间），点开在下面列出它的 Agent，不用 DisclosureGroup（它的行高和箭头和别的行不一样）；
空间标题的 ⋯ 是灰色 `ellipsis`，和计数一样安静；需要你处理也是一行（`CompactInteractionRow`：红色的类型图标、计划等你审批 / Claude 在问你…、会话 · 机器、时间、›，点进对话作答；卡片模式仍是带快捷按钮的卡片）。只有一行的分组是胶囊形，是 iOS 26 分组列表自己的圆角（自定义行背景也会被它裁成胶囊），和「设置」一致，用户 2026-10-31 接受）、「全部折叠 / 全部展开」。点空间标题折叠 / 展开（`dev.folded`，记住；折叠的空间是一行标题，仍显示各状态计数），按状态 / 按机器的分组标题也能折叠；空间页从标题 ⋯ 的「打开空间」进，同一个菜单有「全部标为已读（N）」。左上角菜单选分组方式（`DevGrouping`，记住）：按项目（默认，下面描述的样子）/ 按状态（需要你 → 完成未看 → 工作中 → 待唤醒 → 已屏蔽 → 已看，已看一段默认折叠；「待唤醒」是没有新消息要看、但 `/loop` 还没到点或后台命令 / 子代理还在跑的 Agent，过几分钟可能有新消息）/
按机器（每台机器一段，标题有在线状态和状态计数，段内按状态排、同状态按最后更新时间、空闲折叠）；不按项目时行末的元信息带「空间 › tab」（按状态时还带机器名）；三种分组用同一个顺序（`ChatsView.rank`）：需要你 → 完成未看 → 工作中 → 待唤醒 → 已屏蔽 → 已看，同一状态按最后更新时间排（新的在上）；屏蔽了的工作中 / 完成未看 / 待唤醒的 Agent 算「已屏蔽」（需要你的照旧排第一）。按项目时：原来的会话列表（不含助手对话）：「需要你处理」卡片 → 置顶的 Agent（置顶的 Agent 只在这里出现一次、和别的行一样只靠圆点表达状态，不再出现在空间 / 状态 / 机器分组里；空间内的 Agent 按上面的状态顺序排（空间页 `SpaceView` 仍按 tab 顺序，和 herdr 一致）；手表仍按 `AgentState.priority`） → 置顶的空间 → 进行中 → 刚完成 → 屏蔽但有 Agent 在跑的空间（弱显示）→ 空闲的空间（折叠，屏蔽的空闲空间也在这里）；搜索、按机器筛选、左滑置顶 / 中断、右滑关闭（长按菜单、对话页「…」也有「关闭 Agent…」，herdr 里的任何 Agent 都能关，先从底部弹出确认卡片 `CloseAgentSheet`：哪个 Agent、在哪台机器哪个标签页、关了会怎样，「关闭 Agent」/「取消」）；空间标题 ⋯ 置顶空间。置顶存在 hub（`/api/v1/pins`）。`/loop` 驱动的 Agent 带「⟳ 下次 12:20」标记、不折进空闲；轮次结束但还有后台命令 / 子代理在跑的 Agent 带琥珀色「⚙ 后台 N」，同样不折进空闲，它所在的空间也不算空闲空间，长按可停止循环。改名（空间：标题 ⋯ / 空间页菜单「重命名空间」，同步改 herdr workspace；Agent：长按 / 对话页「…」「重命名…」，一次改 herdr tab 名和 Claude 会话名，Mac 上 herdr 和 claude --resume 看到的一致）。屏蔽（Agent：长按或对话页「…」菜单；空间：标题右侧的 ⋯，或空间页右上角菜单——列表分组标题上的长按菜单触发不了）：不再推送通知（空间屏蔽时，其中的 Agent 可以单独「开启通知」，反之亦然——每个 Agent 的开关叠在空间的设置上，`Pins.togglingMute`）；列表里弱化（状态色变灰、🔕、不透明度 0.8——比工作中的弱，不比空闲的空间淡），在跑的照常显示不折叠，屏蔽且没在跑的空间就是普通的空闲空间；Agents tab 角标不计（`/api/v1/mutes`） |
| 收件箱 | 飞书消息和邮件在一个列表里，一行一件事（一个会话的新消息、一个邮件线程），AI 分拣，左滑已处理；筛选来源 / 账号 / 类型（`docs/inbox.md`）。点开飞书是会话页，同 lark-connector：所有人（包括自己）的消息都在左侧带头像，群聊里连续消息的第一条显示名字，自己的气泡蓝色（#D1E3FF / 深色 #133063）；点头像或名字看资料卡；点表情回应切换自己的回应，「☺︎」或长按「表情回复」打开常用 + 全部表情；removed分享链接渲染成卡片，点开看帖子全文、图片和评论。打开时停在这件事的第一条新消息，只把上过屏的消息上报为已读。卡片渲染等见下文「飞书消息渲染」；邮件：详情、回复、写信、「让 Claude 处理」、邮箱管理（右上角菜单），「AI 一键添加」邮箱 |
| 设置 | 助手（默认启动器 / 模型；「主动做事和记忆」四页，`AssistantProactiveViews.swift`：定时任务——列表行尾开关直接启停，编辑已有的离开页面即保存，新建（＋，可选会前准备 / 周报 / Agent 巡检 / 下班前盘点模板）是带「取消 / 添加」的浮层，避免试一下模板就建出来；立即运行 / 查看上次结果切到「对话」打开那段对话；一次性跟进、按间隔重复的任务和定时任务在同一页、同一个编辑页（运行方式：模型、权限模式、思考强度，全部放行先确认）；记忆——解析 frontmatter，标题优先取 MEMORY.md 索引里的中文标题，类型标签，详情 Markdown 渲染，「编辑」改整个文件，左滑删除，索引单独一行只读；关于我——编辑 notes，离开即保存）、Claude Code 设置（各机器 `~/.claude/settings.json` 的白名单项：离开回顾 recap、思考强度、总是思考、回复语言等，可一键应用到所有机器）、机器（二级页：在线状态、会话数、版本、启动器、重连）、手机数据开关和授权、通知、重新配对 |

对话页：

- 消息：用户气泡（珊瑚色）、助手 markdown（无气泡，阅读优先）、思考（折叠一行）、工具步骤组（>2 步折叠成「N 个步骤 · 运行 5 · 读取 3」，
  永远露出最后一步）、待办清单（TodoWrite 直接渲染）、Edit 的红绿 diff、Bash 命令和输出、图片。
- 展开的工作过程是一张卡片：每一步仍是聊天列表里单独的懒加载行（一轮可能几百步，包成一个视图会卡死），各行自己画卡片的一段
  （`WorkCardSlice`：第一步上圆角、「收起 N 步」那行下圆角，背景伸进行距的一半和邻行接上），步与步之间是从文字列开始的细分隔线；工具行 44pt 高。
- diff 只有一个 `DiffView`（Claude 的 Edit / MultiEdit / Write 给新旧文本，Codex 给 unified diff），用 Git 页的行模型（`DiffText.Line` / `parse`）：
  头部是文件名、目录、+N −N（点开文件）；只有 ± 符号和行底色上色，代码是正文色；长行横滑，右侧渐隐提示还有；超过 40 行先显示开头和「展开全部」。
  Git 页整份文件的 diff 仍用 `MonoTextView` 画（大 diff 的性能），行模型和配色一样。工具出错时「出错了」写在输出上方，不再盖住输出的第一行。
- 图片：任何图片点开全屏查看器（`ImageViewer.swift`），左右滑浏览整个会话的所有图片（`chatImages`），顶部「n / m」和关闭，点图片或下拉退出；
  双指 / 双击缩放。底部：保存（仅添加权限）、编辑、分享、拷贝。编辑（`ImageEditor.swift`）：框选 / 画笔、6 种颜色、撤销 / 清除，完成后「加到输入框」，
  作为下一条消息的附件（`AppModel.composerInbox`），补一句话就能发。输入框里待发的图片点一下也能先编辑再发。
- 后台同步（推拉结合，照 IM 的做法；只在 App 前台且连着 hub 时）：hub 推「哪个会话变了」，App 自己按游标拉增量。
  Agents（`AgentSync`）：会话列表 / `revs` 事件里的 `rev` 和本地记下的不同，就按 `after_seq` 拉新内容，内存里有这个对话就合并进去，
  没有就写进磁盘缓存（`chats-v2`），打开时直接就是最新的；范围是活着的 Agent + 24 小时内用过的会话（最多 40 个，最近的先拉，并发 3），
  正在看的对话靠自己的订阅，不重复拉。飞书（`LarkSync`）：hub 转发 lark-connector 的实时事件（`lark`），收件箱里的飞书会话和打开的会话收到就刷新
  （轮询只作兜底：列表 90 秒、会话 60 秒）；收件箱里有变化的会话在后台取最新一页写进 `lark-chat-<id>` 缓存，连上 hub 时把最近 20 个
  补一遍；未读会话的缓存覆盖到第一条未读时也直接用缓存打开。
- 设置 › 存储空间（`StorageView`）：App 在这台手机上占用的空间，按类别（Agent 对话、对话图片、飞书图片和文件、列表和飞书会话、网页、
  网络缓存、其他、App 数据）显示大小和占比条，左滑 / 长按清除一类，或「清除全部缓存」（设置和配对信息保留；网页只清缓存不清登录）。
  清除后内存里的副本一起丢掉，后台同步重新拉。DEBUG：`-tab settings -openStorage YES`。
- 图片按需加载：历史分页和实时推送都不带图片数据（`lazy_images`，一页从几 MB 降到几百 KB），只带尺寸；行滚到屏幕上才按
  `ImagePayload.ref` 取图（`ChatImageLoader`：同一张只下载一次，内存 NSCache + `Caches/chat-images` 磁盘缓存，图片不会变，永久有效），
  取到之前按尺寸占好位置，列表不跳。缩略图、全屏查看器都走它；本地发出的图片仍是内嵌 base64。
- 链接（`Views/Link/LinkSheet.swift`）：会话里的 http(s) 链接在 App 内的底部卡片打开（Telegram 式）：打开即铺满屏幕，下拉到 60% 时对话照常可用、再下拉收起，
  卡片升到 60% 时后面的对话照常可滚动、可输入（`presentationBackgroundInteraction`）。飞书文档 / 知识库走原生阅读器（`FeishuDocReader`），
  其他网页用 `WebViewPool`：每条回复出现时预加载其中前 3 个链接，最多常驻 6 个 WKWebView（LRU），共用默认的磁盘网站数据（缓存、登录态），
  请求用 `returnCacheDataElseLoad`，打开时直接拿已加载好的视图。网页里选中文字，系统菜单多「AI 修改」（填修改要求后立即发给 Agent）和
  「作为上下文」（输入框上方出现上下文标签，随下一条消息发送）；原生飞书文档用标题栏的「作为上下文」带整篇。上下文在消息里是
  `<jarvis-context kind="context|edit" title="…" url="…">选中文字</jarvis-context>`，气泡里解析成引用卡片（点开回到那个链接），原始标签不显示。
- 文件：消息里出现的路径显示成可点的文件标签，工具卡片里 Read / Edit / Write 的路径也能点；在会话所在机器上读取
  （`GET /api/v1/machines/{mid}/file`，最多 2 MB，拒绝 iCloud 目录），Markdown 渲染 / 代码带行号 / 图片 / PDF / 目录浏览。
- 同空间的其他 Agent（Agents 页）：顶栏第二行是 tab 条（`SiblingTabs`，2026-10-31 用户要求恢复平铺）：每个 tab 一个胶囊，胶囊的底色就是状态色
  （需要你红、工作中橙、未读绿，已看 / 空闲灰；不用会闪的圆点），当前的加粗、带一圈描边，宽度放得下几个就显示当前 tab 和它两边的几个，左右滑能看到全部；最右边「⋯」弹出全部 tab 的列表（状态圆点、名字、状态字，
  当前的打勾，别的 Agent 需要你时「⋯」右上角一个红点），第一项「空间 X 的所有 Agent」进空间页。对话页「…」→「空间里的所有 Agent」同样。
  10-29 曾改成只剩一个按钮的菜单（`SiblingMenu`），看不到 tab 和状态，用户不喜欢。
  对话本来就是从这个空间页点进来的，就直接返回，不再叠一层（ChatsView 记着哪一层是哪个空间页：`spaceAt`）。
  空间页顶部的机器按钮（「公司 Mac ›」）进机器页（`MachineSpacesView`）：这台机器的在线状态和各状态数量，进行中的项目（每个带最需要注意的
  那个 Agent 的最新一句）和空闲的项目，点进各自的空间页；不属于任何 herdr 空间的 Agent 直接列出；右上角 ⓘ 是机器详情。
  DEBUG：`-tab dev -openMachinePage <机器 id>`。
- 机器监控（`docs/machines.md`）：Agents 列表左上角机器菜单最下面的「机器监控」是所有机器一屏的 Dashboard（系统图标、在线、CPU / 内存 / 网络
  当前值和最近 1 小时迷你曲线、异常高亮），点卡片进机器的曲线页（1 / 6 / 24 小时，按住看某一时刻）；设置 › 机器 右上角、机器详情的「系统与监控」也能进。
  机器卡片的图标换成了系统图标（`OSBadge`）。DEBUG：`-tab dev -openMonitor all|<机器 id> [-openMonitorRange 6h|24h] [-openMonitorScrub 0.55]`。
- 离开回顾（recap）：Claude Code 的 away_summary 显示成「离开期间回顾」卡片，去掉了「(disable recaps in /config)」；关掉它在 设置 › Claude Code 设置。
- 展示逻辑（助手和开发统一，`chatLines` + `WorkLine`）：一轮按「消息」切成若干段，每条消息自成一段对话：
  你的消息 → 一行灰色的工作行「工具 5 · 思考 1 · 回复 1 · 4 分 17 秒 ›」（计数一律「名称 数字」）→ Agent 在下一条消息之前说的最后一段话 → 下一条消息……
  进行中和已完成长得一样，只是进行中的那段工作行带转圈、计时、终端按钮（「正在工作 39 秒 · 工具 2」）。
  已完成的一轮如果有多条消息（排队发的）：收起时多条消息合成一个气泡（浅色短分割线隔开，`MergedUserBubble`），下面一行工作行、一条最终回复；
  点工作行展开后按消息拆开成上面的一来一回，顶部「已按 N 条消息拆开 · 收起」。用户发的图片在消息上方显示成方形缩略图（`SentPhotos`，和发送中的样子一致），
  气泡里去掉 Claude 的「[Image #N]」占位。
- 进入位置：完成未看的会话顶部只露出你最后一条消息的最后一行，下面就是 Agent 这次做了什么和回复（`landUnderInput`：先按 id 跳到消息，再按实测位置修正；顶栏同时收成细条，按细条的高度排）；其他会话回到上次离开时屏幕最上面那一行（`chat.read.<会话>`，
  离开时在最新处就清掉，下次直接到最新）。
  这样不管收起还是展开，每条输入对应的输出都紧跟在它后面；不再把一轮的多条消息堆在一起，也不再编号。
  - 工作行折叠的是这段里除最后一段话之外的一切：工具调用、思考（新版 Claude Code 把旁白写成 thinking）、系统提示（后台命令完成等）、
    中间的回复。点一下在原地按顺序列出每一步（左侧一条细线），再点同一行收起；最后一步下面也有「收起 N 步」，几百步时不用滑回顶部。
    展开后每一步是列表里独立的一行（`ChatLine.workStep` / `.workEnd`，几百步也不会变成一整块大视图）。收起 / 展开不动正在看的内容
    （`toggleWork`，不带动画）：从底部「收起」时，倒置列表的最新一端本来就不动，下面的回复留在原处、折好的工作行落在「收起」原来的位置；
    从标题点时标题留在手指下（先按 id 跳过去，布局完成后按实测位置修正偏移）。DEBUG `-showFoldProbe YES` 自动做一遍并把各自移动了多少
    写进 `Documents/fold.txt`（应全是 0）。
  - 后台任务把空闲的 Claude 唤醒（`wake` 通知）：还是同一轮，不切开。唤醒把这一轮分成几段，每段以 Agent 的一段话收尾，
    「真正的回复」取其中最长的那段（「总结 → 构建已上传」取总结；「等它的结果 → 总结」取总结，`Turn.pickAnswerAcrossWakes`）。
    回复之前的一切（含过渡的话、唤醒本身——算一个工具）合成一行工作行；回复后面一个小的「🔔 后台命令完成 · <描述> ›」，不展开，
    点了从下面拉起抽屉（`WakeDrawer`），只看结果：按顺序列出每个后台任务的通知和 Agent 对它的回复，中间的工具、思考、过程中的话收成末尾一条默认折叠的工作行（2026-10-31 用户要求：展开不要同时铺开 n 段过程）。唤醒后还在干活时，小条显示「后台任务完成 · 正在处理」。
    工作中途完成的后台任务（不是唤醒）仍然只是工作行里的一步。
    坑：Claude Code 在空闲会话里会写「离开期间回顾」（recap），它会把这一轮收住；之后来的唤醒要把上一轮重新打开并入，
    回顾卡片留在原位，否则唤醒后的回复又直接露出来（`ChatBlock.build` 的 `lastRaw` / `reopenedAt`）。
  - 离开期间回顾（recap）和后台任务一样：只显示一个小条「↺ 离开期间回顾 · 21 分钟前 ›」，点了从下面拉起抽屉看全文（`RecapCard` / `RecapDrawer`）。
  - 顶部自动收起只认用户的拖动和拖动之后的惯性滑动；打开会话时程序定位产生的 decelerating 不算，否则一打开就收起。
  - 工作行文字不截断：放不下时换成短格式（只留工具数和用时）。
- 「连不上 hub」横幅：连续 5 秒连不上才出现（网络抖动、回到前台的重连都在 5 秒内完成，原来一闪而过），连上立刻消失。
- 布局：列表在上、输入区（交互面板 / 循环 / 后台 / 排队 / 输入框）在下，用 VStack 排、不再叠在列表上；键盘弹起整页变矮，最新消息始终贴着输入框。
  列表上方垫 1pt 空视图，让滚动视图不贴顶部安全区，这样它完全没有安全区（翻转后安全区会落在错的一端），也不必忽略安全区、自己补边距。
- 其他会话发来的消息（`Item.from`）：左侧灰色卡片「来自 名字」。
- 列表是倒置的（`chatScroll`：ScrollView 和每一行各翻转一次）：最新消息在滚动偏移 0，进入、跟随新消息、点 ↓ 都是精确跳到 0，
  不再按懒加载的估算高度去找底部（偶发白屏的根源）；更早的历史加载在另一端，不推动正在看的位置。看历史时（`followsEnd` 关）新来的行先不插进
  列表（`frozenAt`），回到最新再出现，所以不会把正在看的内容挤走。不要用 `defaultScrollAnchor(…, for: .sizeChanges)`：翻转后它锚在远端，
  一变高就把视图带进没渲染的区域（白屏）。倒置后安全区会落错端：滚动视图忽略纵向安全区，用 `contentMargins` 按实际 inset 补留白；
  iOS 26 的边缘模糊会按未翻转的视图算区域、把整页蒙住，所以关掉（`scrollEdgeEffectHidden`）。
- 本地缓存（`StateCache`，Caches/state）：机器 / 会话 / 待处理交互 / 助手设置（`dev`）、收件箱（`inbox-items`，`InboxStore`）、
  邮箱账号（`mail-accounts`）、每个飞书会话最近 80 条（`lark-chat-<id>`）。冷启动、断网、切 tab 都先画缓存，后台静默刷新；有缓存就不显示转圈。
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
- 输入框：Agent 对话（`Composer`）和助手的新对话（`StarterComposer`）共用 `ComposerRow`：+（照片 / 拍照 / 文件 / 快捷指令）、玻璃胶囊输入框、
  发送（没东西可发时淡掉），Agent 工作中且没打字时是红色的中断。
- 「BeautifulUI」风格（设置 › 界面风格，`AppStyle.beautiful`，2026-10-30）下，输入框换成 `PromptComposer`（助手新对话是 `BeautifulStarterComposer`），
  是 BeautifulUI 的 Prompt Bar（`layout: .stacked`）。其他风格完全是上面的样子（CLAUDE.md：新样子做成新主题）：
  上面一个圆角框里是多行输入，下面一行工具栏：+（照片 / 拍照 / 文件）、@、/、模型、听写，右边发送；Agent 工作中且没打字时发送换成红色的中断。
  底色是当前风格的 `.surface`（库自己不画），按钮 34pt。待发的图片条是 `ComposerImages`。草稿、发送安全检查、上传都还在各自的 composer 里。
  - 草稿：输入框直接编辑 `model.drafts`（`Options.text`），不要让 bar 和宿主互相回写，否则拼音输入法正在拼的字会被提前上屏。
  - 「@」：本会话的工具读写过的文件（最近 12 个，打 `@相对路径`，Claude Code 和 Codex 都认），再是其他会话（最近活跃的 8 个，选中后作为一段
    `<jarvis-context>` 引用附在消息前，显示成输入框上方的引用胶囊）。只列已载入的步骤里的文件：长会话里收起的工作卡没展开过就没有。
  - 「/」：`SlashCommand.all`，快捷消息用短词（/继续、/提交、/汇报），选中后把要发的话填进输入框，点发送才发出（以前点菜单就直接发）；
    /compact、/context 原样；能清空时有 /clear，点了先弹确认。
  - 模型：Agent 对话里是 Claude Code 的 `/model` 别名（默认 / Opus / Sonnet / Haiku），选中就发 `/model <别名>`；Codex、TRAEX、无头助手没有这个按钮。
    助手首页是启动器列表（原来输入框上方的模型标签挪进来了）。
  - 听写：`Dictation`（`SFSpeechRecognizer` 中文 + `AVAudioEngine`），听到的字实时接在草稿后面，再点一下停；发送时自动停。
  - 输入框上方的权限、思考强度标签（`SessionModeBar` / `NewSessionModePicker`）留在原处，不并进工具栏，理由见 PLAN「PromptBar v2」。
  - DEBUG：`-showPromptMenu mention|command|attach|model` 几秒后打开对应菜单（截图用）。
- 模型 API 报错（`APIErrorCard`）：Claude Code 放弃重试后写的「API Error: 502 …」回复、hub 的「API 错误：…」通知，都画成一张报错卡，
  不再把原文当正文铺开。第一行是一句人话的原因（`APIErrorInfo.explain`，比如「推理服务暂时没有可用的 GPU」「登录过期了，要在电脑上重新 /login」），
  下面是 BeautifulUI 的 chip：状态码（错误样式）、「临时故障」或「要处理」、连续次数。点开看请求 ID、Session、时间和原文，都能复制。
  同一个报错再来一次、中间只有「继续」这类重试时，合成一张卡加次数（`ChatView.foldAPIErrors`）。报错是最后一条、Agent 空闲、而且是临时性的才有「重试」（发「继续」）。
  现在从文字里解析；Claude Code 在这条消息上还带了 `apiErrorStatus`、`error`、`requestId` 等字段，hub 读出来以后以字段为准。DEBUG：`-showErrorDetails YES` 卡片默认展开。
  截图：mock 加 `MOCK_API_ERRORS=1`，看助手主对话和 review 会话。
- Claude 的建议（`SuggestionPill`）：会话有 `suggestion`、Agent 没在工作、输入框为空时，输入框上方一条胶囊「✦ 建议 … 采用」（✦ 是 AI 的紫色，不是主色），占位文字也换成这条建议；
  点一下填进输入框（可以改了再发），长按菜单「直接发送」。一打字就消失。无头助手没有 TUI，没有建议。截图：mock 加 `MOCK_SUGGESTION=1`，打开 review 会话。
- 发送：点发送立即清空输入框、消息以「发送中」气泡上屏（`ChatStore.pending`），请求在后台发；transcript 里出现同样的话
  （或进了排队列表）就换成真消息，失败变红可重发 / 删除。不等网络，任何地方发都不卡。
- 发的图片：hub 把路径用 `<attached_images>` 标签附在消息后面，App 解析出来在文字上方显示方形缩略图（经 file 接口从会话所在机器读，
  内存缓存），点开全屏查看；气泡里不显示这个标签（旧的「相关图片：…」/「（我从手机发来了图片…）」也兼容）。
- 等宽文本（文件、diff、终端，`MonoTextView`）：UIScrollView 里放一个按最长行实测宽度、不滚动的 UITextView，不换行，横竖都能滑，能选字。
- 长按消息（`MessageMenu.swift`）：像微信一样在按下的位置上方弹出深色操作条（复制 / 选择 / 引用 / 分享，自己的消息多一个编辑），
  不再先把消息浮起来；按的位置靠近导航栏时放到下方。点任意处或手指拖动列表关闭（内容流入导致的程序滚动不关）。
  「选择」打开可任意选取的纯文本页；「引用」把消息以 `> ` 引用放到输入框前面；「编辑」把自己的旧消息放回输入框。
  操作条的图标固定 22pt 高（各 SF Symbol 高度不同，不固定文字就上下错开）。
  - 「选择」为什么不在原地选：消息是 MarkdownUI 排的一组 SwiftUI Text（段落、列表、表格各是一块），SwiftUI 的 Text 在 iOS 上
    只能整段拷贝、不能拖手柄选一段，更不能跨块；原地选要换成 UITextView 重排，表格 / 代码块的样式会丢。技术限制，保持弹出纯文本页。
  - 「分享」（`ShareCard.swift`）：把消息排成一张卡片（渐变底 + 白 / 深色卡片：Claude 或「我」、来源「app › leader」、时间、正文、
    Jarvis 标记），浅色 / 深色可切，「保存图片」进相册（仅添加权限）、「分享图片」走系统分享，右边的按钮分享纯文字。
    `ImageRenderer` 画不出 ScrollView：卡片用单独的 Markdown 主题 `.card`（代码块折行、表格不横滑），否则表格和它后面的内容全是空白；
    渲染要设 `proposedSize` 宽度，不然高度按别的宽度量，卡片底下多出一大块空白。超过 3000 字截断。
- 顶栏（Agents 对话页）：① 返回 / 标题 + 副标题 / …，副标题是「● 状态（工作中带这一轮的用时，按分钟）· 机器 · 空间 › tab」（2026-10-31 起「已看」「未运行」这类平静的状态不写字，只留圆点；点标题从底部弹出「会话信息」浮层：运行 / 用量 / 位置分组、拷贝 id 和恢复命令，下滑或点上方关闭，原来从标题下拉的卡片去掉了），状态字用状态色，
  放不下先省机器、再省位置；② 有同一空间的其他 Agent 或循环 / 后台任务时才有第二行：同伴菜单、循环和后台的小标签（只写下次时间和个数，点开看详情）。
  原来标题、同伴条、tab、活动标签各占一行；文件和 Git 左滑或从 ··· 菜单打开。
- 顶部自动收起（Agents 对话页，像 Safari 的工具栏）：这个顶栏是自绘的
  （`ChromeHeader`，系统导航栏在 Agents 对话页隐藏），浮在列表上方；**跟手**：手指往新消息方向推，顶栏按同样的距离往上滑走、同时淡出，
  细条（返回 +「● app › leader · 对话」）在后半段淡入；往历史方向拉就一点点滑回来。松手后不会停在半截：在最新处收起，
  否则按离哪头近吸附到全开或全收（`.smooth` 0.3 秒）。跟着新内容自动滚动时也收起（用户动过列表之后）；点细条、切 tab 展开。
  停住 3 秒（`ChatView.chromeIdleSeconds`：没滑列表、没碰顶栏、信息卡没开）也自动收起；碰顶栏（横滑 Agent 条等）、点细条展开、切 tab 都重新计时。
  - 为什么顺：折叠程度放在单独的 `@Observable`（`ChatChrome`），每帧只重画顶栏；顶栏浮在列表上、列表的 frame 不变（只在最老那头
    留出顶栏高度的 contentMargins），所以收起展开时消息一像素都不动。以前是开关式：系统导航栏和两条标签各自做动画、列表跟着变高，看着突兀。
  - 只认用户的拖动和拖完的惯性（`onScrollPhaseChange` 的 tracking / interacting，及紧跟其后的 decelerating）；键盘弹起改变列表高度时的偏移变化不算。
  - 系统导航栏隐藏后 UIKit 会关掉边缘右滑返回：`SwipeBack.swift` 把 `interactivePopGestureRecognizer` 的 delegate 接过来重新打开。
- 排队消息：Claude 工作中发的消息不进对话记录，显示在输入框上方的「排队中」列表；每条可「撤回编辑」（回到输入框）、拷贝、删除。
  实现照 TUI：↑ 把全部排队消息拉回输入框（transcript 记 popAll），清空输入框后把保留的重新排队（`POST …/queue {"keep":[…]}`）。
- 开发列表：忙碌的空间里只显示需要你 / 工作中 / 完成未看的 agent，空闲的折成一行「还有 N 个空闲」，点开展开。
- 「回到底部」箭头：用列表末尾的哨兵视图判断是否在底部（滚到底就隐藏）。
- 会话页的终端、文件、Git（开发会话，`Views/Session/`）：**没有选项卡**（2026-10-30 用户要求去掉「对话 · 终端 · Git · 文件」那一排）。
  终端在顶栏右上角的 ··· 菜单里（2026-10-31 用户要求去掉顶栏上单独的终端按钮，收起的细栏也不再有）；工作中的那段工作行上仍有终端按钮；文件和 Git 从对话**往左划**进入（对话 → 文件 → Git，往右划回去），
  或者 ··· 菜单里点「文件」「Git（改动数）」；在文件 / Git 里返回键先回到对话。Git 只在仓库里出现，文件在知道目录时出现；助手对话都没有。
  - 终端（`TerminalPanel`，`Views/Session/Terminal/`，方案见 `docs/terminal.md`）：任何状态都能看。点终端按钮**从底部弹出盖住整页**（`TerminalCover`，2026-10-30 用户要求）：没有标题和选项卡，顶上只有一条细栏（左边尺寸、中间把手、右边设置），
    按住细栏往下拖跟手、拖过 140pt 或快速下划就关掉（点把手也关）；终端的尺寸只随键盘变，不随页头变（换尺寸就是给接管的窗格改尺寸，Claude 要重排）。
    底下的键栏照 Moshi：一条矮的深色胶囊装下所有键（30pt 高的扁平小键，Ctrl / Esc / Tab 等宽小字，其余是图标），把高度留给终端。hub 有 `terminal_stream` 时订阅实时流
    （`TerminalFeed`：herdr observe 的帧套到窗格尺寸的网格 `TerminalGrid` 上，丢帧 / 换流就从整帧重来，重连带 epoch / seq 续上）；
    hub / node / herdr 不能推流时每秒读一次带颜色的屏幕（`format=ansi`）。
    默认「手机尺寸」（设置 › 尺寸）：hub 有 `terminal_control` 时手机按自己的列 × 行接管终端，Claude 按手机宽度排版，键直接写进终端；
    表头显示「手机尺寸 45×29」，被别的程序占着先问「接管 / 只看」，被抢走后显示「只看 · 接管」。离开 tab、进后台、断线后窗格回到电脑的尺寸（`docs/terminal.md` 第十一节）。画面由 `TerminalView` 按格子画：每个字符画在自己的格子里
    （宽字符两格），框线和块字符画成线和矩形，所以中文和框线永远对齐；屏幕只画到最后一行有字的地方或光标处。默认按手机宽度折行，
    右上角「换行 / 不换行 · 左右滑」切换（`@AppStorage("terminalWrap")`）；配色跟随浅 / 深色（`TerminalTheme`：Jarvis、Solarized、深夜），
    字号 `terminalFontSize`。长按选词、拖动扩选，菜单「拷贝 / 全选」；点 OSC 8 链接或行里的网址在 App 内打开。
    往上滑到顶附近自动加载前一段 200 行（带颜色，瀑布式，不一次取全量），
    一直到 herdr 保留的最前面。每段用「当前最上面 20 行 + 已加载行数」去服务端缓冲里定位（`POST …/screen/before`）：Claude 重绘会把同一屏幕反复推进滚动历史，同样的内容可能出现多处，取离「末尾往前已加载行数」最近的那处；终端仍在输出也不会错位；
    看历史时屏幕停住，「回到最新」恢复（上面没有历史——全屏的 Claude——就不停）。herdr 每个窗格只能读出约 1000 行（`pane read` 不管 --lines 多少都封顶，也没有偏移 / 滚动接口），
    到头提示「herdr 只保留最近约 1000 行，更早的过程在「对话」里」——完整历史在 transcript。新版 Claude（Mac）是全屏界面，窗格本身没有滚动历史。
    连不上 hub、流停了、读屏失败或超过 4 秒没拿到新屏幕时，终端顶部出现状态条（`TerminalLinkStatus`）：「上次更新 N 秒前」+「N 秒后重试 · 原因」+「重试」，
    失败后按 1、2、4、8 秒退避，之后每 15 秒一次；恢复后状态条自动消失。旧内容不能看起来像还在实时刷新。
    按键栏（`TerminalKeyBar`）：ctrl / alt / ⇧ 三个修饰键照搬 Moshi——单击粘滞（只作用于下一个键）、快速双击锁定（再点一下解锁，键上填满色并带一道横线）、
    按住期间一直按着（松手时没按别的键就变成粘滞）；修饰键也作用于下面输入框里打的字：按下修饰键时打的下一个字符立刻作为组合键发出
    （ctrl、c → `ctrl+c`；只有 shift 时发大写字符），输入框描边变色、占位文字提示；发送按钮的回车也带上修饰键（shift+enter 在 Claude 里换行）。
    然后是 esc、tab、直达的 ⌃C 和游戏手柄图标（点开浮出方向键 + 回车十字键盘，方向键也带修饰键）。
    操作层：捏合调字号（7–20，存 `terminalFontSize`）；双指左右滑切到同一空间的上 / 下一个 Agent（herdr 的 tab），上下滑切到同一台电脑的相邻空间；
    双击终端粘贴剪贴板（不带回车，多行先确认）；输入框左边的闪电是快捷命令（单击填入、双击直接发送，`terminalShortcuts`）；
    终端右上角的调节按钮打开「终端设置」：编辑快捷命令、键栏显示哪些键（ctrl 一直在）、「不打字时收起键栏」、配色、字号、换行。
    打字时输入框上方出补全：之前在提示符（`$ ` `❯ ` `➜ ` …）后敲过的命令、屏幕上出现过的路径 / 名字，**只用窗格里的内容，不读电脑的 shell 历史**。
    听写用系统键盘的麦克风（App 里没有单独的语音输入）。键名就是 herdr `send-keys` 的写法，
    组合顺序 ctrl+alt+shift+键，herdr 会编码（ctrl+shift+a 发 CSI u），不认识的键名整组拒绝（`invalid_key`）。终端里的按键按按下的顺序串行发送。
    实时状态条上的终端图标打开同一个面板的全屏版。
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
- 终端等你处理（hook 没接住的 AskUserQuestion、权限确认等，`ScreenForm`）：从屏幕上读出对话框（`ScreenParser.dialog`：☐ 标题、│ 引用的问题、
  编号选项和下面缩进的说明、「Type something.」「Chat about this」），排成和提问卡片一样的样子，标题「终端在问你」；点选项发编号键，
  「其他回答…」先按「Type something.」的编号再输入并回车（`Resolution.preKeys`），「改在对话里说」发 Chat about this 的编号；原始终端、
  按键栏和输入框收在「查看终端」里。读不出对话框时直接显示终端。DEBUG：`-showSampleScreen YES`。
- 交互面板：贴在输入框上方。提问（`QuestionForm`，iOS 分组列表的样子：题目 + 一个圆角框里的选项，前面是终端那样的编号圆点、选中变勾，「(Recommended)」显示成「推荐」标签但回答仍发原文；「其他回答…」是框里最后一行，点了才变输入框；单题单选点一下就提交，多题顶部「标题 · 1/2」、底部小号「上一题 / 下一题 / 提交」，单选后自动翻到下一题；`-showSampleQuestion YES` 塞一个示例截图用）、计划（markdown 预览 + 全屏、批准并自动执行 /
  批准并自动接受编辑 / 打回写意见）、授权（允许 / 拒绝 / 本会话总是允许）、终端（屏幕快照 + 解析出的编号选项 + 按键面板 + 发文字）。
- 输入框：见上面「输入框」一条（照片最多 4 张，压缩到 1600px；「/」里是快捷消息和指令；工作中发送换成红色中断）。
- 导航副标题：机器 · 模型 · 状态。

## Jarvis 顶栏和侧边栏（2026-10-30）

参考 dot 和 ChatGPT App（用户给的截图）。代码在 `Jarvis/Views/Assistant/AssistantTopBar.swift`、`AssistantSidebar.swift`。

- **顶栏**浮在对话上方，没有系统导航栏，也没有实心的栏：下面是一层由上往下渐隐的雾（`TopFog`：页面底色 + 材质，延伸到状态栏下），对话从下面滚过去。
  对话列表顶端留出 `AssistantTopBar.height`（70pt），首页用 `contentMargins(.top)` 留出同样的高度。滚动时顶栏不动（不像 Agents 的页头会折叠）。
- 正中是 **Jarvis 头像 + 名字胶囊**（主对话和新对话页是形象的名字，默认「Jarvis」；旁支对话是对话标题）。头像是当前形象的**状态圆点**
  （`PersonaDot`）：空闲 / 思考中 / 工作中 / 等你处理 / 离线各有自己的动画，见 `docs/persona.md`「状态」。点它弹出菜单：知识库、详细信息（资料页
  `AssistantProfileView`）、回到主对话（旁支时）；拷贝目录（助手的工作目录）、拷贝信息（对话名、会话 ID、所在机器、工作目录、启动器 / 模型、
  `claude --resume` 命令，方便贴给别的 Agent，`AssistantCopyText`）、拷贝会话 ID；启动器子菜单、会话信息卡片、工作中时的「中断」。
  拷贝后名字胶囊上盖一个「✓ 已拷贝…」3 秒（不用全局 toast：它正好压在头像上）。弱网 / 断网时胶囊里的名字换成「转圈 + 等待网络 / 连接中… / 更新中…」，
  菜单第一项变成「连接详情」。
- 左边圆按钮 **历史对话** 打开侧边栏，右边圆按钮 **新对话**（新对话页上置灰）。两个都是纯图标。
- **侧边栏**从左边推出来，占屏宽 82%（最宽 380pt），对话页被推到右边缩成一张圆角卡片（深色模式加一圈细边，否则和侧边栏分不开），点卡片或往左拖关闭。
  内容（2026-10-30 重新设计）：顶部是搜索框和「新对话」圆按钮；下面一张 Jarvis 卡片（头像、在线 / 离线和所在机器，点开资料页），
  「定时任务」「知识库」两个并排的小块；然后是对话：「置顶」（主对话固定在这里、标「主对话」，长按别的对话可置顶，存在本机
  `assistant.pinned`），其余按天分组「今天 / 昨天 / 最近 7 天 / 更早」（`AssistantSidebarGroups`，更早的先显示 8 个，「查看全部」展开）。
  每行是标题 + 最后一条消息的一行预览，右边是时间和状态点（需要你红点、未读蓝点、工作中转圈），当前对话用浅主色底。
  搜索时只列结果（「N 个对话」）。底部只在抽屉里有「设置」一行（分栏时 tab 栏在旁边，不放）。长按对话还能拷贝会话 ID、结束。
- **手势**：页面任何位置向右拖都能拉出侧边栏，手指跟手、松手按位置和速度吸附（`AssistantDrawer.settlesOpen`）；和按钮打开是同一个状态。
  用 UIKit 的 pan（`DrawerPan`），开始时就判断：只认横向（竖着滑归列表），关着时只认向右；手指下面有能横向滚动、而且没在最左边的东西
  （宽代码块、表格）时让给它。推出了别的页面（资料、知识库、定时任务）时不生效，那里是系统的返回手势；底部 tab 栏不在这个视图里，不受影响。
- 进度（0…1）放在单独的 `@Observable`（`AssistantDrawer`）里，拖动时每帧只改位移和圆角，不重算对话。
- 测试：`AssistantTopBarTests`（头像菜单和拷贝、左右按钮、按钮和手势两条路打开侧边栏、选对话后关闭并切过去、长对话滚动时顶栏位置不变、连接状态）。
  `-openAssistantSidebar YES` 启动时侧边栏就是开着的（截图）。

## iPhone Duo（2026-10-30）

iPhone Duo 是折叠屏：合上用外屏（约 466×678 点，compact 宽度），打开用内屏（约 669×951 点，regular 宽度），还能半折、横放、立着。
外屏和内屏横放时，**系统把状态栏、导航栏、工具栏、tab 栏竖着放到屏幕侧边**；内屏竖放时仍是水平的栏。官方指南：
`developer.apple.com/iphone-duo`、HIG「Designing for iPhone Duo」。要用 Xcode 27 及以上编译（配置 `xcode`，`docs/config.md`），
旧 Xcode 编出来的 App 在 Duo 上不会延伸到状态栏和摄像头下面。

- **按宽度分两种布局，不看机型**（`Jarvis/Views/SplitNavigation.swift`）。compact（所有普通 iPhone、Duo 合上）还是原来的单个
  NavigationStack，行为一点不变；regular（Duo 打开）用 `NavigationSplitView`：左栏是列表，右栏是它打开的页面，像 Mail。
  两种布局共用同一个 `path`，开合时正在看的页面不丢；半折时系统自动让两栏避开折痕。Agents、收件箱、设置都用它。
  - 左栏的 `NavigationLink(value:)` 替换右栏（左栏自己有一个永远不推的栈，把要推的值转给右栏）；列表里用代码打开的页面也要替换：
    `path = NavigationPath([值])`（单栏时列表在栈底，和 append 一样；Agents 的 `openFromList`）。详情页里再打开的页面照常 append。
  - 右栏第一页没有返回按钮（旁边就是列表）：目标页加 `.splitDetailPage()`；Agents 对话页自绘的返回键看 `\.splitDetail` 隐藏（保留占位，标题居中）。
  - 分栏时对话页不隐藏 tab 栏（单栏时隐藏，好让对话占满）。
  - 设置页的入口全部改成按值（`SettingsPage`），视图式的 `NavigationLink { 页面 }` 会推进左栏自己的栈，在窄栏里打开。
- **Jarvis**：regular 下侧边栏是 `NavigationSplitView` 的左栏（没有「设置」齿轮：tab 栏一直在旁边），左上角按钮显示 / 隐藏左栏；
  compact 下仍是抽屉。
- **可读宽度**：对话和 Jarvis 首页最宽 760pt（`ChatView.readableWidth`），更宽时居中，顶栏仍然铺满。
- **不用屏幕尺寸**：两块屏尺寸、比例都不同，`UIScreen.main` 一律不用。长按消息的操作条夹在对话区域里（`MessageMenu.area`
  坐标空间），交互卡片的表单最高为所在容器的 55%（`containerRelativeFrame`），图片缩放按视图的 `traitCollection.displayScale`。
- **工具栏按钮都要有标题和图标**（`Label("新建会话", systemImage: …)`）：竖排时只显示图标，放不下收进溢出菜单时显示标题；
  只有图标的按钮在溢出菜单里是空白。
- **侧边竖排栏**：系统把栏竖着放到侧边时（外屏，内屏横放；`toolbarVerticalEdge` 不为 nil，iOS 27.1 起），Agents 对话页不再画自己的页头按钮，
  把返回、终端、Git、文件、⋯ 交给系统工具栏，它们排进状态栏下面那一列（`ChatView.sideBarItems`，每个都有标题和图标）；页面顶部只留一条不折叠的标题
  （点开会话信息）和同空间的 Agent、循环 / 后台任务、子 Agent（`sideHeader`）。水平栏时（普通 iPhone、内屏竖放）照旧是自绘的可折叠页头。
  Jarvis 的顶栏仍是自绘的、在内容顶部。
- **在模拟器上验证**：iPhone Duo 只有 iOS 27.1 运行时（这个运行时也只支持它）。开合 / 半折只能在 Xcode 的 Device Hub 里切，
  没有命令行。截图要指定屏幕：`xcrun simctl io <udid> screenshot --display=1`（外屏）/ `--display=3`（内屏，横放时是 2853×2007），
  不指定时可能拍到熄灭的那块（全黑）；先算一下亮度确认拍到的是亮着的屏。启动参数照常用（`-tab`、`-openSession` 等）。

## 思考强度和权限模式

对话页输入框上方两个小标签：权限模式（手动确认 / 自动改文件 / 只做计划 / 自动 / 全部放行）和思考强度（低 … 最高），
相当于 Claude Code 终端底栏，点开就能改（`SessionModeBar`，文案和图标在 `SessionModes`）。新开 Agent 的表单、助手首页的输入框上方
也能选（不选就是「跟随启动器」）。Codex / TRAEX 的会话不显示。

- 显示：transcript 每条 user 行带 `permissionMode`、每条回复带 `effort`；在 App 里改过的值在 transcript 写下更新的值之前优先
  （node/modes.go）。终端里 shift+tab 改的模式要等下一条消息才写进 transcript，开发信息卡片读终端底栏拿实时值。
- 新开：`--permission-mode` / `--effort`。
- 改在跑的会话：助手（无头）往 stdin 发 control request（`set_permission_mode`、`apply_flag_settings {effortLevel}`），下一次请求就生效；
  herdr 里的 Agent 像人一样操作：shift+tab 循环到目标模式（default → acceptEdits → plan → auto），思考强度打开 `/effort` 选择器移到目标档位
  按 `s`（只对本会话生效）。**别发 `/effort <档位>`**：它会顺手存成这个模型以后所有新会话的默认值（写进 settings 的 `modelSettings`）。
- 改思考强度要等这一轮结束、终端输入框是空的（否则 `/effort` 会接在别人没发出的草稿后面）；「全部放行」只能新开时选，循环里没有它。
- 选择器开着时 herdr 把 Agent 报成 blocked，hub 1.5 秒后会把它当成要处理的终端对话框推给手机：node 操作期间不报 blocked。

## 翻译（设置 › 翻译，2026-10-31）

开了「自动翻译」以后，内容不是目标语言（默认简体中文）就自动翻译，随时可以切回原文。

- 范围：**邮件**（默认开）；**飞书会话**默认一个都不翻（消息太多），只翻设置里选中的会话（`TranslationChatPicker`：置顶 + 最近的会话，可搜索），
  而且只翻别人发的。设置存在本机（`@AppStorage` 的 `translate.*`，`TranslationPrefs`）。
- 要不要翻在手机上判断（NaturalLanguage 的 `NLLanguageRecognizer`，取前 1500 字；字母少于 6 个不判；简繁中文算同一种语言，短文本里它分不清）。
  结果按文本记在 `Translator` 里，行的 body 里不跑识别。
- 翻译在 hub（`server/internal/hub/translate.go`），用处理小任务的模型 `inbox.triage_ai`（用户定的：不需要强模型，现在是 gpt-6.1-terra，两句话约 2 秒）。
  邮件按引用翻（`POST /api/v1/translate/mail {account_id, uid, mailbox, target}`，hub 自己读信，HTML 信用 `mail.ReadableText` 转成保留段落的文本，
  正文最多 12000 字，超出的标 `clipped`）；飞书消息批量翻（`POST /api/v1/translate {texts≤30, target}`，App 把 300 ms 内出现的行攒成一批）。
  hub 在内存里按内容缓存，App 在本次运行里缓存。
- 界面：邮件页标题和正文换成译文，正文上方一行「已翻译成简体中文 · 显示原文」；飞书消息气泡下方一行灰色译文（`LarkTranslationLine`）。
- 截图 / 测试：`mock_hub.py` 的 504 号邮件是英文、「设计评审」（f2）会话里有英文消息，`/api/v1/translate` 回「【译】原文」；
  UI 测试 `TranslationTests`；DEBUG `-openTranslationSettings YES` 打开设置页。

## 设计语言

- 代码结构（2026-10-31，方案见 bui-adopt 的 ARCH-PLAN）：
  - `Shared/Design/`：token，手表也编译。`Theme`（字体级别和强调、间距、圆角、行高、动画、颜色角色）、`Icons`（`Theme.Icon` 和 `Theme.Brand`）、
    `Palette`（基础色，小组件 / 实时活动也编译它）、`StyleSpec` 和 `Styles/` 下每种风格一个文件。
  - `Jarvis/DesignSystem/`：只有 iOS 的配方和组件：`Styling`（表面、按钮、页面底色按 `spec.surface` / `spec.buttons` 画）、`Components`
    （Chip、CountBadge、EmptyStateView、Monogram、`Loadable` / `LoadableContent`……）、`TextSize`。
  - **加一种风格**：在 `Shared/Design/Styles/` 写一个 `StyleSpec`（主色、气泡、页面、卡片、描边、表面画法、按钮、字体设计、组件套件、飞书的颜色），
    `AppStyle` 加一个 case，`spec` 里对上。别的代码只读 `StyleSpec.current`，不要 `switch AppStyle`。`StyleSpecTests` 会查新风格深浅色的对比度。
  - 检查（JarvisTests，扫源码）：`TypeScaleTests`（不写系统字号、不写死字号、强调用 `labelStrong` 这类 token 而不是在级别上 `.weight(`、
    竖向间距在网格上）、`IconVocabularyTests`、`TokenRatchetTests`（每个文件写死的 padding、横向间距、圆角、十六进制颜色、动画、固定尺寸只能减少；
    减少后删掉 `TokenBaseline.json` 重跑一次记下新的基线）、`StyleSpecTests`。Debug 编译对超过 150ms 的类型检查报警告（发版机器会超时）。
- 界面风格（设置 › 界面风格，`AppStyle`，存 UserDefaults `ui.style`，默认液态玻璃）：

  | 风格 | 主色 | 表面 / 按钮 | 自己的气泡 | 页面 |
  | --- | --- | --- | --- | --- |
  | 液态玻璃 | 系统蓝 `#007AFF` | Liquid Glass | 浅蓝 `#DCEAFF` 黑字（深色 `#1D3557`） | 系统 |
  | Claude | 珊瑚 `#D4683F`（深色 `#CD6E45`） | Liquid Glass | 深珊瑚 `#B24C25` 白字 | 系统 |
  | 黏土 | 薰衣草紫 `#7A5AF8`（深色 `#8979EB`） | 实心软块：左上高光 + 右下彩色阴影、白描边，圆体字，圆角 24 | 紫 `#6759D3` 白字 | 淡紫 `#EFEAF7`（深色 `#0A0510`） |
  | Island | 青 `#27747F`（深色 `#2A9798`） | 白色悬浮卡片 + 大而柔的阴影，圆角 26 | 墨黑底白字（深色反过来） | 雾青 `#E6F1EF` |
  | 飞书 | 飞书蓝 `#3370FF` | 白卡片 + 发丝描边、无阴影，圆角 10，按钮 bordered | 浅蓝 `#D1E3FF` 黑字 | 浅灰 `#F5F6F7` |

  各风格的可读性底线（2026-10-30 修过一轮，数字见 jarvis-p1 的 themes-fix 报告）：实心按钮和选中胶囊的白字 ≥ 3.5:1（所以深色主色不能太亮），
  自己气泡的白字 ≥ 4.5:1，主色当链接对页面 ≥ 3:1；彩色气泡的渐变只往下压暗（`Theme.deepening`），不能提亮顶部。黏土 / Island 的实心按钮
  （`SoftButtonStyle`）用当前 tint 填色、至少 44pt 高：调用处 `.tint(Theme.danger)` 的中断、关闭 Agent 在每套风格下都是红色。
  自己画背景的滚动页用 `.themedGroupedPage()`，List / Form 用 `.themedPage()`，否则黏土 / Island / 飞书下切页时底色会跳。
  `Theme` 的颜色按当前风格取值（`Theme.accent(style)` 可取任意风格，设置里的预览卡用它）；面板用 `.surface(tint:interactive:in:)`、
  按钮用 `.themedButton(prominent:)`、列表页面 `.themedPage()`、plain 列表的行 `.themedRow()`，不要直接写 `glassEffect` / `.glass`。
  切换风格时 RootView 以风格为 id 整棵重建（token 是静态的），当前 tab 和打开的会话本来就会记住。飞书页面的框架色
  （`LarkColor.blue` 链接 / 选中 / 新消息线、自己的气泡、卡片底和描边）跟着风格走，卡片内容的飞书具名色板不变。
  调试：`-ui.style glass|claude|clay|island|lark`（UserDefaults 参数域，Release 也认，不算 DEBUG 开关）。
- 状态色不随风格变，只有一张表 `Shared/StatusPalette.swift`（App、手表、实时活动都编译它）：需要你红、工作中琥珀、完成未看绿、已看灰、未运行浅灰，和 herdr 侧边栏一致（`docs/agent-states.md`）。每种状态三个值：`dot`（圆点、图标、填充）、`text`（同色相加深，小字对白底 ≥ 4.5:1）、`tint`（浅底）；界面用 `AgentState.tone`。提问 / 计划 / 授权 / 终端对话框都算需要你，一律用红；工作中的转圈用琥珀，旁边的字用次要灰。不是 Agent 状态的东西不借状态色，用 `Theme` 里各自的 token：`Theme.Diff`（diff 和 Git 的 ± 符号、计数、浅底，正文保持黑字）、`caution`（连接中、过期、截断、版本不一致这类提醒，配三角图标，不画成圆点）、`ai`（✦ AI 摘要）、`loop`（循环和待唤醒）、`positive`（成功、已连接、在线、做完的待办）、`Hue`（只用来区分类别，比如记忆类型）。权限模式标签是中性的，只有「全部放行」用红。
- Agents 列表的一行（`SlotRow`）：状态点 + 标题 + 时间；最新一句；元信息一行「需要你 · Claude · 空间 › tab · 机器」，普通字体、次要灰。只有需要你写成字（红），其余状态只看圆点（完成未看另把标题加粗）；以前 tab 名着色胶囊、状态字、描边的 Agent 种类挤在第一行，把状态说了三遍。空间标题的状态计数是彩色圆点 + 次要灰数字。
- Token（`Theme.swift`，页面只引用）：圆角 `Theme.Radius` 五档（badge 6 / control 10 / card 14 / bubble 20 / panel 22，外加胶囊和圆）；
  背景灰阶 `Theme.Fill` 三档（well / chip / pressed，就是系统的 quaternary / tertiary / secondary fill，深色下也看得见，别再写 `Color.primary.opacity(…)`）；
  - 字体 `Theme.Font` 只有这几级（默认字号下）：display 28 粗、title 22、heading 17 粗、body 17、secondary 15、caption 13、sectionHeader 13 粗（配次要色）、
    label 12、code 13 等宽。页面里不直接写 `.subheadline` 这类系统样式，也不写死 `.system(size:)`。
  - 和 BeautifulUI 一样，层次主要靠字重和颜色：行标题是 body 大小加粗，分组头是 caption 大小加粗、次要色，比行标题小；
    Markdown 的一级标题是 title，其余标题是 heading，靠上方的空白分开；代码块是 code。
  - 间距 `Theme.Space` 是 4pt 网格（2 4 6 8 12 16 24 32），竖向的 `VStack(spacing:)` 都用它；横向间距在按钮文字旁边，没有统一改。
  - 尺寸 `Theme.Size`（2026-10-31 用户要求）：图标框、圆点、头像、点按区域、缩略图、标签列宽这类挨着文字的尺寸都有名字，默认字号下和原来的点数一样，
    用 `.scaledFrame(width:height:)` / `.scaledFrame(minWidth:…)` 随字号缩放，到辅助功能大字号最多 1.6×（44pt 的按钮不至于占掉三分之一屏幕）。
    4pt 及以下的线、分隔条、进度条不缩，用普通 `frame`；按视图自己的几何算出来的尺寸照旧；手表不跟 App 的字号，保留自己的尺寸。
  - 字号跟随动态字体；设置 › 界面风格 › 字号（小 / 标准 / 大，`TextSize`，`ui.textSize`）在系统档位上再挪一档：RootView 设 `.dynamicTypeSize`，
    所以文本样式、`@ScaledMetric`、MarkdownUI 和 BeautifulUI 的字号一起变，比例不变。
  - 飞书消息和卡片保留飞书自己的字号（落在上面的级别上），通过 `LarkTextBase.scale`（`Theme.textScale`）一样跟随。
  - 固定尺寸框里的图标加 `dynamicTypeSize(...)` 上限，免得长出框；AI 回复的 Markdown 基准 17pt（MarkdownUI 自己按动态字体缩放）。
  - 例外：终端网格（用户自己调字号）、灵动岛格子、导出的分享卡片（固定版式的图片）、小圆圈里的序号和角标。
  - 等宽只给要逐字符对照或拷贝的东西：代码、命令、路径、分支、哈希和 ID、版本号、终端和工具输出。中文说明和值用系统字体
    （会话信息页按行选：模型、分支、目录、版本等宽，其余系统字体 + 等宽数字；记忆编辑器、知识库的最近消息是系统字体）。
  - 图标 `Theme.Icon`（`Shared/Design/Icons.swift`）：一个意思一个 SF Symbol，页面写 `Theme.Icon.copy`，不写 `"doc.on.doc"`。
    两个文件都用到的符号必须登记；出错一律是三角（`error` / `errorFilled`），八角只给机器的严重告警（`critical`）；拷贝 `doc.on.doc`、
    粘贴 `doc.on.clipboard`；中断 `stop.fill`；模型 / 启动器 `cube`，`cpu` 只是处理器；代码文件 `curlybraces`，`</>` 只是 Agents。
    `IconVocabularyTests` 扫源码：登记过的符号写了名字、两个文件共用却没登记、退役的出错符号回来，都会失败。
    别家的图标名（飞书卡片的 icon token）照飞书的画，那一行以 `// icon: <原因>` 结尾。
  - 品牌标志 `Theme.Brand`（Assets 的 Brands 文件夹，来源和条款见 `THIRD_PARTY_NOTICES.md`）：Claude、OpenAI（Codex）、飞书、
    Gmail、Google 的 G、各 Linux 发行版。按官方原样用，不改颜色、不重画；发行版是单色模板，染发行版的颜色。用在：分享卡的头像
    （Claude / Codex，TRAEX 没有公开标志，仍是绿底终端）、收件箱的来源角标和手表收件箱（飞书）、邮箱账号（Gmail / 飞书邮箱）、
    「使用 Google 账号登录」按钮（照 Google 的规范：浅色白底灰边、深色 #131314，不用主色）、机器的系统图标（`OSBadge`）。
  锁屏实时活动限到 `.large`（高度约 160pt，超出会被裁掉）。
- 动效（`Theme.Motion`：状态切换 `.snappy` 0.25、布局 `.smooth` 0.3）：一直在动的只有「需要你」圆点的呼吸和工作中的系统转圈，
  打开「减少动态效果」时圆点也停（`accessibilityReduceMotion`，加载占位和「正在准备」同样停）；工作中的圆点是一圈静止的外环。
  不再有图标摆动、齿轮旋转、头像脉冲、待办转圈。计数用 `.numericText` 过渡。`.surface` 外面不再加 `.shadow`（玻璃自己有，
  黏土 / Island 风格的实心面也有，叠上去成了双层阴影）；相邻的玻璃（终端键栏的每个键、顶栏的返回和 ⋯）放进一个 `GlassEffectContainer`。
- 标签和计数各只有一种画法（`Components.swift`）：`Chip` 是不能点的小标签，三种样子——中性（次要灰字 + 浅灰底：模型、版本、tab 名、私聊）、
  着色（某个含义的字 + 它的浅底：状态用 `.tone`、@我 红、「推荐」用 AI 的紫色）、描边（Claude / Codex 这类种类）；`CountBadge` 是计数（白字压在
  `Theme.countBadge` 灰或主色上，或者状态的字压在状态浅底上，过 99 写 99+）。能点的东西（筛选、选项卡、模式选择、子 Agent 条）是控件，不用它们。
- BeautifulUI（`ios/Packages/BeautifulUI`，BeautifulUI 仓库拷入的副本，来源和更新方法见包里的 `VENDORED.md`，不在这里改库代码）：
  **只在「BeautifulUI」风格下用**（`AppStyle.usesBeautifulUI`，颜色和表面同液态玻璃），其他风格的思考过程、工具行、工作卡、输入框都是 Jarvis 原来的。
  2026-10-30 它曾直接替换原来的界面（#93、#94），用户觉得太丑，整体 revert 后改成这个主题。这个风格下：
  - 对话页的思考过程是它的 `ThinkingState`（✦ + 首行，展开后正文在一条引导线右边）。
  - 展开的工作卡里，连续两个以上的工具调用合成一行 chip（`ChatLine.toolRun`、`ChatView.runLines`、`ToolRunRow`）。
    每种工具一个 chip：一个时写主语（「读取 WeatherStore.swift」），多个时写数量（「运行 6 条命令」）；编辑带 +N −N（和 diff 同一份 `ToolDiffMemo`）；出错的 chip 红底。
    点一下展开成逐条工具行（`ToolChipRow`），再点一条才看参数和输出。只有一个调用的段直接是一行工具行。
    转到后台的调用不进组，单独一行，好让它的「后台 · 已完成 / 失败」一直看得见（`ChatView.foldsIntoRun`）。
    从后台任务通知跳到某个调用时，先展开它所在的组。
  - diff、命令和输出、Markdown 代码块仍是 Jarvis 自己的（`DiffView`、`CodeBlock`）。
  - 颜色、圆角、字号由 `BUITheme.jarvis(style)`（`BeautifulUIBridge.swift`）从 `Theme` 生成，在 RootView 注入：
    - 库的第三级文字也用 secondaryLabel（tertiaryLabel 在卡片上只有 1.7:1）；
    - 代码关键字用 `Theme.Hue.blue`；
    - 字号 ×1.2（库是桌面字号），代码保持 ×1；
    - 入场动画关掉。
- 触感：发出 / 回答成功 success，新交互 warning，出错 error。
- 图标：夜色背景 + 珊瑚光环 + 四角星（`scripts/make_icon.py`，含 dark / tinted）。
- 品牌色：App 图标的珊瑚色（`Theme.brand`）只给 Jarvis 自己的标志：设置和对话首页的图标、实时活动锁屏的四角星。能点的东西一律用当前风格的主色 `Theme.accent`。
  两份 AccentColor 资源（手表的标题和 tint、系统弹窗这类读资源的地方）是系统蓝；实时活动读不到风格，描边和按钮固定用系统蓝（按钮是压深的 `#0071E3`，白字 4.7:1）。

## 数据流

- 逻辑层的选型（2026-10-31）：「加载 → 显示 → 编辑 → 保存」的页面用 MVVM：一个 `@Observable` 的 `XxxModel` 放在页面旁边，
  hub、报错、toast、跳转从 `Deps` 传进来（`live` 接 `AppModel`），视图只留自己的状态（正在编辑哪个、弹出了什么）。第一个是
  `AssistantRoutinesModel`。事件多、顺序出过问题的地方（对话页、飞书会话的分页）先把纯计算抽出来：`ChatLines`（消息 → 行）、
  `AgentListOrder`（Agents 列表的排序和分组），再改成 reducer。
- 单元测试不连 hub：`APIClient.transport` 换成 `FakeHub`（JarvisTests）按「方法 路径」回固定的 JSON，记下发了什么，view model 走真实的请求和解码。
  体验模式仍用全局的 `APIClient.local`。
- `AppModel`（`Jarvis/App`）：配对信息存 Keychain；`LiveConnection`（WebSocket，指数退避重连，重连后按 `since_seq` 重新订阅）；
  每个会话一个 `ChatStore`（按 id 去重、按 seq 排序、`buildRows` 生成展示行）。
- 进入对话页 `subscribe`，离开 `unsubscribe`；hub 推 `items` / `screen` / `sessions` / `interaction`。
- 手机工具：hub 推 `tool_call` → `PhoneToolRegistry.call`（写操作先弹确认）→ `tool_result`。连上就 `register_phone` 声明工具（`docs/phone-tools.md`）。
- 推送：配对后申请通知权限并注册 APNs，`POST /api/v1/devices`；hub 有 `apns.json` 时对新交互和 agent 完成推送（完成推送等 1 分钟、在电脑或 App 里看过就不推、后台任务结束才推，`docs/server.md` 推送策略）「✅ 标题」，
  副标题「机器 · 空间 › 标签」，正文是最后一条回复。前台且正在看该会话时不弹。模拟器自动化（`-pairURL`）不申请通知权限、不注册成手机。
  交互推送带按钮（`NotificationActions`，类别名就是 hub 发的 `INTERACTION_<KIND>`）：授权「允许 / 拒绝」，计划「批准 / 打回修改」（打回可以直接输入意见）。
  说「是」的按钮要先解锁（`.authenticationRequired`），「拒绝」不用；计划从通知批准用 `acceptEdits`（没看全计划，命令仍要逐个授权），
  「批准并自动执行」只在 App 里。按钮在后台直接调 resolve，不打开 App；失败（已被处理、连不上 hub）发一条本地通知说明原因。提问、终端没有按钮，点开处理。
  设置 › 通知（`NotificationSettingsView`）：「需要你处理时」开关、「Agent 做完时」不推送 / 只推长任务 / 每次都推、实时活动（锁屏和灵动岛 / 锁屏显示详情 / 灵动岛两侧显示什么 `IslandSettingsView`，`docs/live-activity.md`），存在 hub 上按设备分开（`PushRegistrar.deviceID`）。
  点通知打开对应会话：`UNUserNotificationCenterDelegate` 用 completion handler 版本，切回主队列处理再回调（async 版本在后台执行器上返回，
  UIKit 在非主线程收到完成回调，点通知冷启动会崩）。
- 手表配对：iPhone 在 WCSession 激活后再推一次配对（激活前推会被丢掉），同时 transferUserInfo 保证送达；手表未配对页有「从 iPhone 同步」
  （sendMessage 请求，iPhone 回复配对）；iPhone 设置里有「同步到 Apple Watch」。
- 手表：iPhone 经 WatchConnectivity 把配对信息和摘要（运行中会话、待处理交互、收件箱里需要你的事）同步给手表；手表打开时每 8 秒直接请求 hub 刷新，
  回答 / 发消息 / 中断也直接调 hub。
  界面层级：每行的主体（问什么、会话名、收件箱标题）用 headline，类型和位置用 footnote；消息和计划去掉 Markdown 标记再显示（`WatchText.shown`），
  气泡跟着字宽、自己的靠右；没拿到过数据时显示「加载中」，连不上 hub 时最上面一行「连不上 hub」+「重试」（有旧数据就写明是多久前的）。

## 体验模式（没有 hub 也能看全部功能，2026-10-30）

TestFlight 开了公开链接、要过 Beta App Review：没配对时原来只有配对页，审核员和还没搭 hub 的朋友什么都看不到（可能被以 2.1 拒）。
配对页第二个按钮「没有 hub？先体验一下」进入体验模式：App 用内置的假数据跑，不连任何服务器（Release 里可用，不是 DEBUG 开关）。

- **挂在数据层下面，页面不分叉**：体验模式的配对信息是 `ServerConfig.demo`（`https://demo.invalid`，永远解析不了，漏网的请求也到不了网络）。
  `APIClient.request`（以及 `image` / `fullItem`）对它不走 URLSession，交给 `APIClient.local`；`LiveConnection` 对它不开 WebSocket，
  用 `LiveConnection.makeLocal` 做的本地 socket。两者都由 `DemoHub`（`Jarvis/App/DemoHub.swift`）注册和回答，回的是和 hub 一样的 JSON，
  所以页面、`ChatStore`、`AppModel.handle` 都不知道自己在体验模式里。没有的接口回 404「体验模式里没有这项数据」，DEBUG 下记一行 `DemoHub miss`。
- **数据**：`scripts/make_demo_data.py` 从 `scripts/mock_hub.py`（UI 测试用的假 hub）导出 `ios/Jarvis/Resources/Demo.json`，再加几个 Agent
  （每种状态、几个空间都有）、飞书会话的消息、邮件正文、知识库条目，终端是按手机宽度排好的 Claude 屏幕（`phone_screen`，96 列的帧在手机上每行都折）。
  全是编造的；时间存成相对值（`@rel:<秒>`、`@unix:<秒>`），载入时换成「现在」附近，演示永远是新的。机器监控的曲线由 `DemoHub` 现算（`mock_hub.metric_at` 的移植）。
  改了 mock 的数据就重跑脚本、提交 JSON；`DemoHubTests` 让每个接口都经 `APIClient` 解码成 App 的模型，模型改了形状会在这里挂。
- **副作用**：给 Agent / Jarvis 发消息在本地回显，1.5 秒后「Agent」回一段「这是体验模式……」；回答提问 / 批准计划时卡片消失、Agent 接着「做」再回同一段话，
  另弹「体验模式下不会真的发送」；Jarvis 新对话本地新建。其余会碰到电脑、飞书、邮件的操作（新建会话、中断、关闭、终端按键、发邮件、立即运行……）
  以 `APIError(code: "demo")` 失败，`AppModel.show` 把它显示成蓝色提示「体验模式下不会真的发送」而不是红色报错，页面也不会显示「已关闭会话」这类成功提示。
  只改本地状态的（已读、置顶、免打扰、定时任务开关、记忆、收件箱已处理）照常生效，退出后丢掉。
- **标识和退出**：设置 tab 带「体验」角标；设置顶部写「体验模式 · 数据都是编造的」，下面一节「连接我的 hub」/「退出体验模式」，隐藏服务器和手表那两节。
  退出回到配对页；配对成功（扫码、手动、`jarvis://pair` 链接）自动离开体验模式。体验模式记在 `demo.on`，重启 App 仍在体验模式里，进入时总从发货时的数据重新开始。
  进入 / 离开时清掉演示会话的草稿和对话缓存（演示的会话 id 就是 mock 的 id，UI 测试的模拟器上会撞到别的测试留下的草稿）、收件箱，Jarvis tab 打开演示的主对话。
- 体验模式下不申请通知权限、不注册 APNs、不 `register_phone`、不推配对信息给手表、不换设备凭据。
- 审核备注（`app_store.external.review.notes`）写了从配对页进体验模式。

## 连接状态（弱网 / 断网）

参考 Telegram：网络有问题时不弹横幅，**页面标题位直接换成「小转圈 + 状态」**，好了以后无声换回（2026-10-29 用户要求，原来列表顶上的
`ConnectionBanner` 横幅已删掉）。

- 三种状态（`Jarvis/App/ConnectionStatus.swift`）：**等待网络**（`NWPathMonitor` 说手机没网）、**连接中…**（有网但 WebSocket 没连上、
  正在重连，或者连着但 ping 5 秒没回 / 回得比 5 秒还慢 = 弱网）、**更新中…**（刚连上，正在补拉：待处理交互和打开着的对话的 backlog 都到了才算完，
  最多 8 秒）。没配对、App 在后台（socket 是主动断开的）不算问题。
- 去抖：从正常变成有问题后要持续 1.5 秒才显示，回到前台、切网络这种一秒内完成的重连什么都不显示；恢复时立刻换回标题。
- 弱网：只有及时回来的 pong 能解除「连接中…」（晚到的 pong 也算慢），慢的时候每 5 秒 ping 一次，好了很快就能看出来；
  否则标题会每 20 秒闪一下（实测过）。socket 照旧 45 秒没任何消息才判死重连。
- 网络恢复（path 从没网变成有网）立刻重连，不等退避（最长 20 秒）；切换网络（Wi-Fi ↔ 蜂窝）时马上 ping 一次，旧接口上的死连接几秒内就会显示出来。
- 显示位置（`ConnectionTitle` / `ConnectionLabel`）：Agents 列表（平时没有标题，标题位留空）、收件箱、设置 › 机器
  的导航栏中间（`.principal`）；「Jarvis」tab 是头像下的名字胶囊换成状态（菜单里多一项「连接详情」）；对话页展开的页头保留会话名、把副标题换成状态（Telegram 的做法），折叠后的小胶囊整个换成状态。
  点状态弹出说明（`ConnectionDetails`：原因、上次失败、几秒后重试、上次连上、几条消息在等、「立即重试」）。
- 断网时的操作：
  - **发消息**：连不上时不发请求，气泡带时钟和「连上后自动发送」（点时钟可「现在重试 / 不发了」）；请求因为没网 / 超时失败也一样转成等待，
    不弹错。重连且「更新中…」结束后按写的顺序自动发出（没打开的对话先拉一页最新消息，已经送到的会在 transcript 里出现、自动去掉）。
    同一条消息重发带同一个 `client_msg_id`，不会打两遍。等待中的消息存盘（`Outbox.swift`），App 被杀掉也不丢。
  - **批准 / 回答 / 其他一次性操作**：不排队（电脑上的状态可能已经变了）。没网时卡片原样不动；失败时卡片放回去，
    弹一条蓝色提示「连不上 hub / 没有网络，刚才的操作没有生效」（4 秒内最多一条），不用红色报错。
  - 没网 / 连不上 hub 的错误（`Error.isOffline`）和超时一样不显示在页面里（`Error.shown` 为 nil）；后台刷新、往上翻历史失败什么都不弹，标题已经在说了。
    手表没有这个标题，手表上照旧显示。
- 调试：`-showConnection waiting|connecting|updating` 固定显示某个状态，再加 `-openConnectionDetails YES` 打开说明；
  `-simulateOffline YES` 让 NWPathMonitor 视为没网（模拟器总是有 Mac 的网）。mock 的断网 / 弱网开关见 `docs/testing.md`。

## DEBUG 启动参数（仅 Debug，Release 检查不含这些字符串）

`-pairURL jarvis://pair?...`（免扫码配对、不弹通知授权）、`-openSession <machine>/<session>`、
`-showNewChat YES`、`-tab dev|mail|settings`、`-openAssistant <machine>/<session>`、`-newAssistant YES`、`-openAssistantSidebar YES`（Jarvis tab 的侧边栏开着）、`-openFirstImage YES`（打开会话里第一张图的查看器）、`-openTerminal YES`、`-showPad YES`、`-showKeyMods locked,once,held`（终端键栏的 ctrl / alt / shift 置成这些状态）、`-terminalWrap NO`、`-terminalTheme ink|midnight|jarvis|solarized`、`-terminalInputMode direct|field`（终端直接打字 / 输入框）、`-terminalTipsSeen YES`（不出「终端小技巧」）、`-openTerminalTips YES`（再出一次）、`-focusTerminalInput YES`（直接打字模式下是弹出键盘）、`-openShortcutsPanel YES`、`-openTerminalSettings YES`、`-lark-connector <feedId>`（直接打开飞书会话，`-lark-connectorName` 标题，`-lark-connectorTopic YES` 话题群，`-lark-connectorAt <position>` 打开到某条消息）、`-larkFocus <messageId>`（滚到该条消息）、`-larkProfile <userId>`（会话里弹出此人资料卡）、`-larkPicker YES`（对最后一条消息弹表情回复）、`-larkMoments <postId>`（打开removed帖子）、`-openInboxFirst YES`（收件箱打开第一件未处理的）、`-larkTop YES`（飞书会话从最早一条开始显示，截长卡片用）、`-addMail YES`（打开管理邮箱 › 添加邮箱）、`-tab settings -openAssistantPage assistant|routines|routine|intervalTask|followup|bypassTask|newRoutine|memory|memoryFile|memoryEdit|about`（设置 › 助手的各页；`routine` 打开第一个定时任务，`intervalTask` / `followup` 第一个按间隔的 / 一次性的，`bypassTask` 在编辑页弹出「全部放行？」确认，`newRoutine` 弹出「下班前盘点」模板的新建浮层，`memoryFile` / `memoryEdit` 打开第一条记忆 / 它的编辑器）。`scripts/sim.sh` 封装了编译、安装、配对、启动（`SIM=<udid>` 换模拟器；多个 agent 同时开发时各用各的模拟器，否则会互相覆盖安装）。
检查历史渲染：`-loadAll YES`（加载全部历史并滚到顶）、`-fromSeq <seq>`（从包含这条用户消息的那一轮开始显示，长历史里滚动定位会漂）、
`-dropUsers YES`（隐藏那一轮的用户气泡，一屏看全过程行和答复）。`-openPane terminal|git|files` 打开会话页的终端（从底部弹出）/ Git / 文件；
`-simulateTap <machine>/<session>` 启动 4 秒后走一遍「点通知」的处理；`-tab settings -openNotificationSettings YES`（设置 › 通知，再加 `-openIslandSettings YES` 进灵动岛）；`-showFleetSample needsYou|working|done`（`-fleetDetails YES`、`-fleetIsland b|c|custom:<左>,<右>`）用编造的数据开实时活动；`-showTestNotification permission|plan` 进后台 3 秒后弹一条和交互推送一样的本地通知（UI 测试用它试按钮）；`-showMenu YES` 在最后一条回复（配合 `-fromSeq` 是那一轮的回复）上打开长按操作条；`-openWelcome YES` 启动时忘掉配对、退出体验模式，停在配对页（`DemoModeTests`）。

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
