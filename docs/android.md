# Android App

`android/`：Kotlin + Jetpack Compose 的 Android 客户端，连同一个 hub，接口和 iOS 一样（`docs/protocol.md`），服务端不用改。
minSdk 26（Android 8），targetSdk 34。界面中文。

一句话描述：在 Android 手机上看各台电脑终端引擎（herdr、Herdr App 或 Orca，每台电脑选一个，`docs/engines.md`）里的 Agent、发消息、回答提问 / 审批计划 / 授权、按终端对话框的选项，
不需要任何推送账号。

## 现在有什么

| 页面 | 内容 |
| --- | --- |
| 配对 | 欢迎页（`PairScreen`，照 iOS 的欢迎 / 配对页）：Jarvis 的标志、名字和一句话，「扫码配对」大按钮和要在 hub 机器上跑的命令，下面「或者手动填写」一组地址 / token 输入框和「连接」；hub 不认这台手机时上面一张说明卡片。扫 `jarvisd hub pair` 的二维码（`jarvis://pair?url=…&token=…`，也能从链接打开），或手动填地址和 token；配对后立刻 `POST /api/v1/auth/tokens`（`kind: phone`）换这台手机自己的 token，旧 hub 没有这个接口时继续用配对 token；配对后先问一步「hub 所在的电脑用哪个引擎」（下面「引擎」） |
| 外壳 | 和 iPhone 的新外壳一样（`docs/ios.md`「新外壳」，`ui/shell/`）：没有底部 tab 栏；顶栏是 ☰（收件箱有「需要你」时带红点）· 「助手 \| Agents」切换（Agents 上是等你的 Agent 数）· 右边的圆按钮（助手：新对话，新对话页上置灰；Agents：新建会话），浮在页面上、下面一层渐隐的雾。两页都一直建着（`ShellLayer`，切换不丢位置），藏起来的一页透明、不接触摸。再点一下选中的「助手」弹出知识库 / 认识 Jarvis / 语音对话 / 拷贝会话 ID；再点「Agents」弹出列表选项（分组、全部折叠 / 展开、机器、机器监控）。侧边栏（`Sidebar`）：页面在根部时任意位置横着拖就拉出来（页面成一张圆角卡片被推到右边，侧边栏贴着卡片左边一起走），点卡片、往左拖或返回键关上；内容是大字「Jarvis」和搜索、认识 Jarvis / 收件箱（带数）/ 定时任务 / 知识库 / 机器，然后对话历史（置顶：主对话在最前，长按可置顶别的，存在本机 `ui` 的 `assistant.pinned`；最近：先显示 30 个），底部「新对话」药丸 + 设置。收件箱、定时任务、知识库、机器、空间页从右边推进来；设置从下面升起铺满，右上角 ✕；对话页盖在最上面。Agents 页按返回键回到助手页。弱网时顶栏下面一个小药丸（等待网络 / 连接中…） |
| Agents | 搜索框和快速筛选常驻在顶栏下（不随列表滚动）。筛选「全部 / 需要你 / 未读 / 工作中」带状态色圆点和数量。下面依次是「需要你处理」卡片（每件一行：红色的类型图标、计划等你审批 / Claude 在问你…、会话 · 机器、问题或计划标题；单选提问的选项直接是按钮，点了就答）、置顶的 Agent（只出现一次）、分组，最后是最近 7 天的 offline 会话；不含助手的对话；下拉刷新。分组、机器筛选、折叠在外壳的列表选项里改，记在手机上（`AgentsListOptions`，排序规则在 `data/Grouping.kt` 并有单测）。<br>按项目：置顶的空间 → 其他忙的空间 → 屏蔽但在跑的空间（弱化）→「空闲的空间」（默认折叠）；空间标题是小号灰字的名字、置顶图标、机器、各状态的彩色圆点和数，折叠箭头和 ⋯；空间里的 Agent 是一张圆角卡片，忙的、置顶的、待唤醒的在上，空闲的收成卡片最后一行「还有 N 个空闲」。<br>按状态：需要你 → 完成未看 → 工作中 → 待唤醒 → 已屏蔽 → 已看（「已看」默认折叠）。<br>按机器：按 hub 配置的顺序，标题带在线状态和状态计数。<br>每一行（`AgentRow`，iOS `SlotRow`）：状态圆点（需要你会呼吸、工作中带一圈环）、标题（需要你 / 完成未看加粗）和时间、最新一句、元信息「需要你 · Claude · 空间 › tab · 机器」（只有需要你写成红字）和循环 / 后台 / 子 Agent 的小标签。关闭在跑的 Agent 有三个入口，和 iOS 一样、同一个确认（`CloseAgentDialog`）：长按菜单的「关闭 Agent…」、行上左滑出红色「关闭」（松手后行弹回、弹出确认）、对话页 ⋯ 的「关闭 Agent…」；Agent 运行时（DSH 等）里只读的会话长按有「实时接入」，元信息那行写它从哪来 |
| 空间操作 | 空间标题右边的 ⋯：打开空间、置顶空间、屏蔽这个空间（其中的 Agent 可以单独开启通知）、重命名空间（改 herdr workspace 名，hub 把置顶 / 屏蔽一起改到新名字上） |
| 空间页 | 一个 herdr workspace 里在跑的 Agent，两列卡片按标签页顺序排；最后一张「在这个空间开新标签」，在同一个 workspace、同一个目录里起 claude（`POST /api/v1/sessions` 带 `workspace_id`）。右上角菜单多一个「全部标为已读」。从空间页打开的对话，返回时回到空间页 |
| 搜索和筛选 | 右上角搜索（标题、目录、空间名、标签页名、最后一句，不分大小写）；快捷筛选「全部 / 需要你 / 未读 / 工作中」带数量，筛出来的 Agent 看过以后也留在原位，重新点筛选才刷新（和 iOS 一样）；「全部电脑 / 某一台」只看一台电脑，记住选择；完成未看的不止一个时有「全部标为已读」。搜索和筛选时所有折叠都展开 |
| 对话 | 顶栏照 iOS：左边圆形返回，中间一个胶囊「● 空间 › tab」（状态圆点，需要你 / 工作中写出来），点了弹出「会话信息」，右边 ⋯（终端、文件、Git、拷贝目录、会话信息）。输入框：左边圆形的「+」，胶囊输入框（工作中是「补充说明（会排队发送）」），右边发送；没东西可发时那个位置是麦克风（听写），Agent 工作中、没打字时是红色的中断（麦克风在它左边）。历史分页（`turns=2`，滚到顶自动往前翻）、实时 items（WebSocket `subscribe`，按「items 的连续性」补洞）、图片按需取（`lazy_images`，内存 + 磁盘缓存）、Markdown；离开最新处时右下角「回到最新」；发出的消息马上作为气泡画出来、后台发送（`chat/Outbox.kt`，和 iOS `Outbox` 一样）：hub 连不上时不发请求，气泡带时钟和「连上后自动发送」，存在手机上（`files/outbox.json`，App 被杀也还在），连上并补完 backlog 后按写的顺序发出，每条始终同一个 `client_msg_id`（node 不会打两遍）；transcript 里出现了就收起气泡；点时钟可以「现在重试 / 不发了」。被拒（`dialog_open` / `input_draft`）时弹一次窗说明，单行草稿可以「清空并发送」，气泡上留红色感叹号（清空并发送 / 重发 / 删除）；`delivered: unknown` 说清可能已经送到，不自动重发。连接状态不弹横幅，标题下面那行换成「小转圈 + 等待网络 / 连接中… / 更新中…」（`chat/ConnectionWatch.kt`，`docs/ios.md`「连接状态」：没网 / 没连上或 ping 5 秒没回 / 刚连上在补拉打开着的对话，最多 8 秒；有问题持续 1.5 秒才显示，好了立刻换回）；中断（Esc）；Claude 给的下一条建议点一下填进输入框 |
| 会话信息 | 点对话页标题或 ⋯ 菜单「会话信息」（iOS `SessionInfoSheet`，`GET …/info`）：运行（模型、启动器、Claude Code 版本、权限、思考强度）、用量（上下文、压缩次数、轮数、共生成、开始时间）、位置（目录、分支），「拷贝 session id」「拷贝恢复命令」（`cd <目录> && claude --resume <id>`，Codex / TRAEX 是各自的 resume） |
| 语音输入 | 没东西可发时发送键的位置是麦克风（iOS 的听写；助手页 ChatGPT 式的输入框里麦克风常驻）：打开手机自带的语音输入界面（`RecognizerIntent`，有哪家识别就用哪家，小米的、Google 的都行），识别出的字放到输入框末尾，改完再发；手机上没有语音识别时不显示这个按钮。不需要 App 自己的录音权限 |
| 快捷命令 | 和 iOS 同一组（`SlashCommand`）：「+」菜单里的 继续 / 压缩上下文 /compact / 查看上下文 /context / 提交并推送 / 汇报进展，点了直接发；输入框里打「/」在上方列出来，接着打字会筛（`/c` 只剩 /compact、/context、/clear），点一个把它的消息放进输入框。「清空上下文 /clear」先确认，再等窗格里出现新会话、跳过去（`Hub.clearContext`）；只给 herdr 里的 Claude（Jarvis、Codex / TRAEX 没有） |
| API 报错卡片 | Claude Code 放弃时写的「API Error: 529 …」（回复）和 hub 重试时的「API 错误：…」（提示）画成一张卡（iOS `APIErrorCard`，`ui/chat/ApiError.kt`）：白话原因（过载、限流、登录过期、超时……，和 iOS 同一套文案）、状态码、「临时故障 / 要处理」、「连续 N 次」（同一个错误中间只隔着「继续」就合成一张）；点开是请求 ID、Session、时间、原文和「复制原文」。错误是最后一条、Agent 没在工作、重试有用（不是登录、参数错）时有「重试」，发一句「继续」 |
| 排队中的消息 | Claude 工作时发的、还没被取走的消息列在对话最下面（iOS `QueueStrip`）：编号、每条点开「撤回编辑 / 拷贝 / 删除」。撤回和删除走 `POST …/queue {keep}`：node 把排队的全部拉回、清掉，再把要留的按顺序放回去；撤回的那条放进输入框（已有草稿就接在后面） |
| 权限模式、思考强度和模型 | 对话页输入框上方的标签（iOS `SessionModeBar`）：当前的权限模式、思考强度，点开换（`POST …/modes`），在跑的会话不能选「全部放行」（只能新开时选），是别的模式时照样显示出来；Codex / TRAEX 的会话不显示。Jarvis 的对话也有，改的是它下一次请求起的值。herdr 里的 Claude 还有第三个标签「默认 / Opus / Sonnet / Haiku」（iOS `claudeModels`），点了发 `/model <别名>`；会话的模型名里没有这三个词时显示「默认」。Claude Code 会把选的模型存成新会话的默认（`~/.claude/settings.json` 的 `model`），对话太长时还会先问要不要换（在电脑上的对话框里，手机上会变成待处理的卡片） |
| 交互卡片 | 和 iOS `InteractionPanel` 一样固定在输入框上方（最多半屏，打字时四分之一屏，可以滚），点标题收成一行（「Claude 在问你 · 问题」），⋯ 里「交回电脑处理」（`dismiss`）。提问：选项在一个分组框里，带编号、选中打勾、「推荐」标签，最后一行「其他回答…」；一个单选问题点一下就答，几个问题时「标题 · 1/2」翻页，上一题 / 下一题 / 提交。计划：批准并自动执行 / 自动接受编辑 / 打回修改（可写意见），右上角全屏看计划。授权：允许 / 拒绝（可写原因）/ 本会话总是允许。终端对话框：按 hub 解析出的 `option` 回答（编号选项、「Type something」输入），「改在对话里说」，「查看终端」看原始屏幕和按键；密码框只发一次不保存。`prompt_changed` 时提示看新的再答，别处已经答过（404 / 410）就安静收起 |
| 对话里的工作过程 | 和 iOS 的 `chatLines` 一样（`docs/ios.md`「展示逻辑」，`ui/Rows.kt` 的 `buildRows` + `foldWork`，有单测）：每条你的消息下面，这一轮的工作收成一行「工具 N · 思考 N · 回复 N · 时长 ›」，只留最后一句回复在外面；正在跑的一轮是「正在工作 N 秒」并带终端按钮，跑着时回复后面还有工作就也算工作。点开就在原地一步一行地展开（每步是列表里单独的一行，几百步也不卡），最后一行「收起 N 步」。工具调用和结果合成一行（图标、动词、对象），点开看详情：Bash 是命令和输出，Edit / MultiEdit / Write 是 diff（只给 ± 号和行底色上色，长行左右滑，超过 40 行「展开全部」），Codex 的改动（Edit 带 `diff`）按 unified diff 画；输出超过 10 行折起、被 hub 截断的「看全文」全屏看；TodoWrite 直接画成待办清单；工具截的图在详情里。离开期间回顾（recap）是一行，点开看全文；后台任务的通知说清是什么完成 / 失败了。你的消息里的 `<jarvis-context>` 画成引用卡片，`<attached_images>` 画成缩略图（从那台电脑读），路径变成可点的文件标签，别的会话发来的消息在左边标明来自谁 |
| 消息菜单 | 长按一条消息（你的或 Agent 的回复）在按住的地方弹出深色操作条：复制、选择（全屏可选中的纯文本）、引用（`> ` 放进输入框）、分享、交给 Jarvis（新开一段助手对话，消息作为 `<jarvis-context>`，和收件箱的「交给 Jarvis」同一条路）、作为上下文（输入框上方一个标签，随下一条消息作为 `<jarvis-context>` 发出，✕ 去掉）、编辑（只在你的消息上，内容放回输入框）。滑动列表就收起 |
| 图片 | 点对话里的任何图片全屏看：左右滑看这段对话里的所有图片（「n / m」），双指 / 双击放大、拖动，单击显隐按钮，下拉关闭；保存到相册（`Pictures/Jarvis`，安卓 10 以下要存储权限）、分享、拷贝、编辑（框选 / 画笔、六种颜色、撤销、清除，完成后作为照片放进输入框，和「+ › 照片」一样随消息发出） |
| 文件和 Git | 顶上「文件 · Git」分段切换，路径是一排可点的胶囊，列表都是圆角卡片；Git 的状态字母、领先 / 落后、± 行数用 diff 的颜色。对话页右上角 ⋯ ›「文件」「Git（改动数）」「拷贝目录」，消息里的文件标签和工具里的路径也能点开（`fs.list`、`file.read`、`git.status / diff / show / log`，只读）。文件：面包屑、目录在前、Git 改动的标记、隐藏文件开关；文件查看器：Markdown 渲染（可切源文件）、代码带行号两个方向滑、图片（点开全屏看）、PDF、2 MB 截断提示。Git：分支、领先 / 落后、上游、暂存数；改动列表（点开看 diff，部分暂存的可切「未暂存 / 已暂存」）；提交历史（30 条之后「加载更早的提交」），点开提交看说明和改了哪些文件，再点文件看这一部分的 diff |
| 终端 | 对话页右上角，实时终端（`docs/terminal.md`）：`terminal_subscribe` 收 herdr 的帧，VT 子集画进格子（颜色、宽字符、框线逐格画、OSC 8 链接），断线后带 epoch / seq 续上，丢帧就从整帧重来。默认「手机尺寸」接管终端（`control`），Claude 按手机宽度排版；键盘打的字和键栏（Esc、Tab、Ctrl / Alt / ⇧、方向键、^C、⇧Tab、退格、回车）直接写进终端；Ctrl / Alt / ⇧ 和 iOS 一样：点一下只管下一个键，快速点两下锁住（再点一下松开），按住时一直按着，键盘上打的一个字带着它们变成按键（Ctrl 再打 c 就是 ctrl+c，⇧ 只是大写），上下拖是 herdr 的滚轮，点一下是在那一格单击；电脑上别的程序占着时问「接管 / 只看」，被抢走后顶栏有「接管」。可切「电脑尺寸」只看（按窗格原尺寸镜像，左右滑，键走 `/keys`）。右上角「手机尺寸 / 电脑尺寸」菜单里调字号（A− / A＋，7 – 20、每次 0.5，和 iOS 一样，默认 13，存在手机上）：接管时换字号就是换网格，窗格按新的列 × 行重排。顶栏的拷贝按钮打开「选择文本」：屏幕（接管时连同滚出屏幕顶的行）按行排成等宽、不折行的纯文本，可以左右滑，长按选一段拷贝，或「全部拷贝」（iOS 是直接在格子上选，这里借对话的选择页）。旧 hub / node 不能推流时退回每秒读一次 `/screen?format=ansi`。离开页面或 App 进后台立刻放手，窗格回到电脑上的尺寸 |
| Agent 操作 | 长按一行：置顶 / 取消置顶（置顶的只在顶部「置顶」里出现一次）、屏蔽 / 开启通知（规则和 hub 一样：Agent 自己 → `allowed` → 它的空间；屏蔽的灰点加 🔕、不弹通知）、重命名（herdr 标签页名和 Claude 会话名一起改）、关闭 Agent（先说明会退出哪台电脑上的什么、关掉哪个标签页）。置顶 / 屏蔽存在 hub（`/api/v1/pins`、`/api/v1/mutes`），点了立刻生效，hub 拒绝时退回原样 |
| 新开会话 | 列表右下角 +：选电脑、目录（家目录里列「最近用过」，点一下选中、再点进入）、启动器、权限模式（和 iOS 同一组，「默认」交给启动器）、可选的第一条消息。和 iOS 一样带 `async`：hub 马上回会话、对话页立刻打开，第一条消息不放进 `prompt`、进对话后当普通消息发（马上上屏，起名用 `name_hint`），在电脑上等 Claude 就绪再打进去；输入框上方「正在启动 Claude，消息会在它就绪后发出」，失败时是「没有启动起来：原因」（会话的 `starting` / `start_error`）。旧 hub 不认 `async` 时照旧等它启动完；那时 hub 说没启动起来但新窗格里其实起来了（停在对话框上，或者 node 不认识这个版本的输入框底栏），照样打开新会话 |
| Jarvis（助手） | 外壳的「助手」页。Jarvis 是 hub 配置的那台机器上无头运行的 Claude（`docs/assistant.md`）：打开时进主对话（没有就进最近一段，`AssistantModel`）；历史对话在侧边栏。新对话页（`ShellHome`，iOS 同名）只有输入框，上面是现在要你处理的事：等你的 Agent，然后收件箱的「需要你」，最多 3 行 +「还有 N 件在收件箱」，没有就什么都不显示；发出第一句就显示气泡和「Jarvis 正在准备…」；第一段对话自动成为主对话。打开时 `POST /api/v1/assistant/warm`，第一句不用等 claude 启动。对话里自己的气泡是中性灰（`ShellColors.userBubble`，各风格都一样），输入框是 ChatGPT 式的（`ShellComposer`）：一行「[+] 问 Jarvis [麦克风] [语音对话]」，聚焦或有字时长成卡片，下面一排 [+] … [权限和思考强度] [麦克风] [发送]；麦克风是听写，「语音对话」是 hub 的语音插件（有的话才显示）。回复逐字显示（WebSocket `delta`），没有终端按钮。助手自己的提问不进 Agents 的「需要你处理」，通知标题是「Jarvis 在问你」，点开进助手页 |
| Jarvis 的资料页 | 侧边栏「认识 Jarvis」（「定时任务」直接进定时任务），和 iOS `AssistantProfileView` 对应：接下来（开着的定时任务的下次运行和跟进，最近三件）；定时任务（列表、开关、编辑名称 / 提示词 / 时间 / 星期、删除、立即运行、新建，「Jarvis 建的」有标记）；跟进（现在做、取消）；运行记录（按天分组、新的在上，点开对应的对话）；关于我（编辑后新对话起生效，右上角保存）；记忆（看、改、删，删除先确认；MEMORY.md 是单独一行「索引」，能改不能删）。每一页都是推进来的一页，编辑定时任务 / 记忆在底部浮层里（七天的星期选择 + 每天 / 工作日 / 周末），删除定时任务先确认 |
| 知识库 | 侧边栏「知识库」（或再点一下「助手」的菜单），和 iOS `KnowledgeView` 对应（`docs/knowledge.md`，`ui/knowledge/`、`data/Knowledge.kt`）：一排类型（项目 / 会话 / 人 / 文档 / 会议 / 日程，选中的带数量）；项目分「重点（P0 / P1）/ 其他进行中 / 已完成 / 不是项目（默认收起）」，其余按 hub 的重要级和相关性排序；右上角按标签筛选（hub 给的常用标签）；搜索框常驻、全文搜（档案、时间线、聊天记录，300 ms 防抖），结果按条目合并、搜索词的每个词各自加粗，点开进条目。每行是类型图块（人和会话是首字，颜色跟重要级）、名字、时间、一句话，再一行 P 级 / 长短期 / 相关性和放得下的标签（放不下的整个不显示，不截半个）。条目页：重要级、短期 / 长期、状态、相关性、标签；在做这个项目的 Agent（点开进对话）、时间线（新的在上，6 条以后「显示全部」）、档案（Markdown，去掉和头部重复的行）、关联的会话 / 人 / 文档…（每类 4 个以后「全部」，点开推下一层）、代码目录、最近 7 天的消息、知识库目录。「问 Jarvis」把档案和最近 10 条时间线作为 `<jarvis-context>` 发进主对话（没有主对话就新开一段作为主对话）；「开 Agent」（项目）打开新开会话面板，第一条消息预填这段上下文，电脑和目录预选项目的第一个代码目录；「打标」改重要级、相关性、标签和项目的类型 / 状态（`PUT /api/v1/kb/{kind}/{id}`，手动打的 hub 不再自动覆盖） |
| 收件箱 | 侧边栏「收件箱」推进来的一页（☰ 上的红点、侧边栏里的数都是需要你的件数），`docs/inbox.md` 的安卓版：搜索框常驻，飞书和邮件合成一个列表，hub 的 AI 分成需要你 / 知悉即可 / 已折叠，最后是已处理（没分拣的算需要你）。顶部筛选（全部 / 飞书 / 每个邮箱 / @我 / 私聊 / 群聊 / 邮件，只列有东西的），搜索标题、发件人、内容、摘要；左滑到底是已处理，已处理的可以移回；知悉即可「全部看过」、已折叠「全部清除」。AI 分身草拟好回复的那件，草稿直接画在行里（「AI 分身草拟的回复」小卡片，「修改」打开详情、「发送」确认后以本人身份发出）。点开一件：摘要、hub 给的原文（不打开飞书页面；邮件见下面「邮件」）、AI 分身的草稿和依据；「AI 草拟 / AI 改」（`/draft`）、「发送」先确认再以本人身份发（`/reply`）、「交给 AI」（同 iOS）：选要它做什么（自己写或三个预设）和交给谁（Jarvis 新开一段对话，或最近活跃的 8 个在线 Agent 之一，作为一条消息发进它的会话并打开那个对话），可选同时标为已处理，消息是 iOS 同样格式的 `<jarvis-context>` 加要求；「已处理」。列表在 WebSocket `inbox` 事件、重连、下拉刷新时重新拉 |
| 邮件 | 收件箱里的一封邮件点开就是这封信（iOS `MailDetailView`）：一个线程的每封信，较早的折成一行（谁、什么时候、第一行），最新一封展开：发件人、收件人、抄送、时间，HTML 在锁住的 WebView 里画（`MailWebView`：不跑脚本、不读文件、所有网络请求拦掉，「显示图片」后才放行图片，`MailHtml.sanitize` 再去掉 script / iframe / form / 事件属性 / `javascript:` 链接），没有 HTML 就是可选中的纯文本；附件只列名字和大小（hub 还不能下载）。打开就标已读，☆ 加星标（`/flags`）。「回复 / 回复全部 / 转发」打开写邮件（引用前 40 行，回复全部去掉自己、去重；转发带原信的头）；收件箱右上角也能写新邮件。写邮件是盖在一切之上的一页（一个全屏的窗口，也能盖住收件箱详情的浮层）：发件人（hub 的账户，多个时可选）、收件人 / 抄送 / 密送（逗号分隔，`名字 <地址>` 也行，格式不对标红、不能发）、主题、正文，有内容时返回要确认。信里的链接进 App 内的网页卡片，`mailto:` 打开写邮件。上面照旧是收件箱的摘要，下面照旧是「AI 草拟 / 发送（`/reply`）」「交给 AI」「已处理」 |
| 连接器 | 设置 › 连接器（iOS `ConnectorsSection`）：「这台手机」（手机数据的开关，打开了几项）、「邮箱」（几个账户、有没有出错的）、「飞书」（hub 的 lark-connector：已连接时是登录的名字）。邮箱页：每个账户一行（Gmail 红、飞书蓝、其他 IMAP 主题色），出错的显示错误；⋯ 里「测试连接」「删除」（先确认）；「添加邮箱」：Gmail（使用 Google 账号登录）/ 飞书（邮箱 + 密码，服务器固定）/ 其他 IMAP（服务器可留空自动识别），hub 连上 IMAP 和 SMTP 才保存。「AI 一键添加」没有了（hub 删了 `/api/v1/ai/mail-account`），页面底下提示在 Jarvis 里说「帮我添加邮箱」。飞书页：连接状态、登录的名字、最近一条实时消息、出错原因，「Jarvis 用飞书做什么」和「/agent 模型」（跳到模型配置的 `lark.agent`） |
| 附件 | 对话页输入框左边的「+」：照片（系统的照片选择器，一次最多 4 张，长边缩到 1600、JPEG 0.75，和 iOS 一样）；拍照（系统相机拍一张，写到 App 的 `cache/camera`（`ChatFileProvider`），读回来和选的照片一样处理，用完删掉；App 声明了 CAMERA（配对扫码用），所以第一次拍照要先给相机权限）；照片都作为 `images` 随消息发出，输入框上方是缩略图、可以移除；文件（系统的文件选择器，传到会话所在那台电脑的临时目录，`POST /api/v1/machines/{mid}/files`，超过 200 MB 不传，传完把路径插到输入框末尾、带空格或引号的用单引号包起来，**不自动发送**）。只有图片没有字时发「请看图片」 |
| 手机数据工具 | 设置 › 连接器 › 这台手机：设备信息、剪贴板、通知、通讯录、日历、位置、健康、照片，默认都关着，打开时要对应的安卓权限。打开的才通过 `register_phone` 声明给 hub，Claude 调 `phone_*` 时 hub 发 `tool_call`，手机在本地查、`tool_result` 返回（`jarvis/android/phone/`）。工具名、说明和参数和 iOS（`apple/Jarvis/PhoneTools`）完全一样（`PhoneToolsTest` 拿 iOS 源码里的定义逐个比），因为 hub 只存一份工具列表，哪台手机最后连上就用哪台的，两台不一样的话一换手机 Claude 看到的工具就变了。会改数据的（新建日程）先在手机上问：App 里一张卡片，同时一条带「允许 / 拒绝」的通知，90 秒不答算拒绝；拒绝时 Claude 拿到 `The user declined on the phone.`。照片：`phone_photos_recent` 列系统相册（MediaStore）里最近的照片和视频，`phone_photo_get` 把一张缩到长边最多 2048、JPEG 0.7 交给 Claude 看；id 是 `image:<行号>` / `video:<行号>`，截图按 Screenshots 目录认；安卓不给照片的拍摄位置（要 `ACCESS_MEDIA_LOCATION`，没申请），所以没有 `location` 字段。健康：`phone_health_summary` 读 Health Connect 的每日汇总（`phone/Health.kt`，指标、单位、按本地自然日切分都照 iOS `HealthTool`），只读；权限走 Health Connect 自己的授权框（不是安卓运行时权限），打开开关时请求全部 7 种读权限，只给了一部分时缺的指标列在结果的 `not_granted` 里（Health Connect 和 HealthKit 不同，会告诉 App 授没授权）。Health Connect 要有一个说明页（`HealthRationaleActivity`）它的授权框才会弹出来。安卓 14 起它是系统的一部分，8–13 是 Play 商店里的一个 App，没装或要更新时设置里那一行变成「获取」、工具返回说明而不是报错（没有 Google 服务的手机通常装不上）。依赖固定在 `connect-client:1.1.0-alpha08`：之后的版本要 compileSdk 35 / 36。还没有：提醒事项（iOS 有） |
| 界面风格 | 设置 › 外观 › 界面风格：液态玻璃（默认）、Claude、黏土、Island、飞书，和 iOS 同一组（`docs/ios.md`「设计语言」），浅色 / 深色跟系统。选择页上面是当前风格的一段对话示例，下面五张预览卡，点一下整个 App 立刻换；存在手机上（SharedPreferences `ui`，键 `ui.style`），断开 hub 也保留。见下面「界面风格」 |
| 设置 | 从侧边栏打开，从下面升起铺满、右上角 ✕。iOS 设置那样一组组圆角卡片、行首彩色图标：hub 地址和连接状态；外观（界面风格）；「电脑」：机器（在线数，有 `node_mismatch` 的机器名）、Claude Code 设置、设备；「连接器」（见下面「邮件」）；后台通知（通知、电池优化、自启动）；存储空间；断开 hub。子页面是 `SettingsRoute` 的一个栈（Root 保存），从机器页打开的对话返回时回到机器页。「AI › 模型配置」见下面「模型配置」；宽窗口里是左右两栏（菜单 ‖ 子页面，见「折叠屏和大屏」） |
| 机器 | 设置 › 机器，和 iOS `MachinesView` 对应：每台一张卡片（系统图标、在线、运行中 / 工作中 / 需要你、系统名和 herdr / claude 版本、`node_mismatch` 提示，离线时显示错误）。点开是机器页：卡片、运行中的 Claude（不含助手的对话，点开进对话）、系统与监控、启动器（名字和模型）、重新连接（最多等 15 秒看连没连上）；机器页有「引擎」一行（见「引擎」） |
| 引擎 | 和 iOS「引擎选择」一样（`docs/ios.md`，给用户的说明见 `docs/engines.md`），逻辑在 `data/Engines.kt`（`EnginesTest`），页面是 `ui/machines/EngineScreen.kt`。配对成功（扫码、手动、配对链接）后先为 hub 所在的机器（`kind: local`，没有就第一台）选引擎：选项按 hub 回报的 `engines` 原样列出（名字、`note`），每项写「检测到可用 · 版本」/「没检测到」，离线或没有检测信息时写「离线，暂无检测信息」/「暂无检测信息」，不说成没装；必须选一个才能继续，没有「跳过」、返回键也不跳过：预选这台机器现在的引擎（没选过就是 herdr），「确认并继续」`PUT /api/v1/machines/{id}/engine`，保存失败显示原因、停在这一页重试（2026-11-01 用户要求）；hub 没回报 `engines`（旧 hub）时配对后不进这一步。机器页的「引擎」行显示现在的引擎，点开是同一个选择页，保存后用 PUT 返回的机器立刻更新，之后跟着 `machines` 推送；离线也能存（「机器离线，选择会在连接后生效」）。hub 没回报 `engines`（旧 hub / node）时列出已知的三个；已知引擎随时能选，不认识的只有 hub 说可用才能选。页面说明「只显示所选引擎里的会话」 |
| 模型配置 | 设置 › AI › 模型配置（`ui/settings/ModelConfigurationScreen.kt`，逻辑 `settings/ModelConfiguration.kt`，`ModelConfigurationTest`）：对话、分拣、翻译和各项 AI 功能按用途统一选模型，hub 存（`/api/v1/model-config`）；新开 Agent 的面板在 hub 支持时改成「本次启动器：跟随模型配置 / 某个启动器」；语音页的模型也走这里（「模型配置」打开语音这个用途），配置不可用时页面说明原因并给「模型配置」「重试」。旧 hub 没有这个接口时照旧用启动器和手机上的语音模型 |
| 系统与监控 | 机器页里点开，`docs/machines.md`「机器详情」的安卓版：系统名、在线时长；现在的 CPU / 内存 / 网络 / 负载 / 磁盘 / 交换（偏高、过高标出来）；1 / 6 / 24 小时的 CPU、内存、网络、负载（虚线是核数）、磁盘曲线，hub 给的 `gap` 处断开不连线（`segments`），单调三次曲线不过冲；按住或横拖读某一刻，所有图一起跟；每个核心（Linux）；系统信息（内核、架构、机型、处理器、开机时间、herdr / Claude Code / node 版本）。页面在前台时 1 小时每 10 秒、其余每分钟刷新 |
| Claude Code 设置 | 选一台在线的机器（默认 hub 所在的那台），列出 node 白名单里的 `~/.claude/settings.json` 项（开关、枚举、文本，没设过的标「默认」，文本清空就是回到默认）；「同时改所有在线机器」默认开，每台各写一次，失败的逐台列出来并重新读 |
| 设备 | `GET /api/v1/auth/tokens`：每台设备的凭据（本机、手表「只能回答」），最近使用 / 撤销时间，已撤销的单独一组；撤销先确认，撤销的是这台手机自己就回到配对页 |
| 通知 | 「需要你处理时」开关，「Agent 做完时」不推送 / 只推长任务 / 每次都推，系统里每类通知的设置入口。和 iOS 一样存在 hub 的 `devices/{id}/prefs`（只发 `input` / `done`，iPhone 的实时活动、灵动岛字段不动；id 是这台手机第一次用时生成的 `android-…`），每次连上 hub 时读回来，所以在别处改了也生效；改的时候 hub 连不上，下次连上先把这边的发上去。`Notifier` 按它决定弹不弹（见下面「通知」） |
| 存储空间 | 这台手机上 Jarvis 占的空间按类分（对话、图片、网页、网络缓存……，没有的类不显示）、一条比例条，点一类单独清、或清除全部缓存；设置和配对信息不清 |
| 链接 | 对话（Agents 和 Jarvis）、收件箱详情里的链接都在 App 内的卡片里打开，见下面「链接」 |

## 链接

CLAUDE.md「链接在 App 内打开」的安卓版（`ui/web/`，对应 iOS `Views/Link/LinkSheet.swift`）：

- 打开即铺满屏幕；拖顶部的把手 / 标题栏下拉到 0.6 时，后面的页面挤到上面 40%（`LinkHost` 按卡片高度给内容让出底部），对话照常滚动、能点输入框打字；
  再往下拉或点 ✕、返回键关闭。Jarvis tab 开着卡片时不显示底部 tab 栏（和打字时一样），输入框直接贴着卡片。
- 网页来自 `WebViewPool`：最多 6 个、按最近使用淘汰；对话里 15 分钟内的回复，前三个链接一出现就在后台开始加载（`LinkText.toPreload`），
  翻历史不预加载。不在屏幕上的页面 `onPause()`（预加载完、卡片关掉、App 进后台都会停掉声音和计时器），系统要内存时（`onTrimMemory`）销毁不在屏幕上的。
  第一次打开用 `LOAD_CACHE_ELSE_NETWORK`，看过的页面从磁盘缓存直接出来。深色模式下页面跟着变暗（自带深色的页面用自己的，其余算法变暗）。
- 页面里选中文字，菜单最前面是「AI 修改」「作为上下文」：「AI 修改」弹框写怎么改，选中内容和要求作为一条消息立刻发给这个对话的 Agent（或 Jarvis）；
  「作为上下文」放进这个对话输入框上方的小条（可以点掉），随下一条消息发出。两种都是 iOS 的 `<jarvis-context kind="edit|context" title="…" url="…">` 标签（`ContextRef`）。
  收件箱里没有对应的 Agent，菜单只有「交给 Jarvis」，新开一段助手对话。从收件箱详情打开链接时详情先让开（ModalBottomSheet 是单独的窗口，链接卡片盖不上去），关掉卡片后带着没发的草稿回来。
- 安全（`LinkSafety`、`externalIntent`）：http(s) 进卡片；`jarvis://` 只做跳转（`session/<机器>/<会话>`、`tab/…`、`assistant`、`inbox`），
  `jarvis://pair` 这种会改配对的不认；`mailto:` / `tel:` / `intent:` 交给系统，`intent:` 去掉 component、selector、clip data 和所有 URI 授权标志，
  指向本 App、`jarvis:`、`content:` / `file:` 的一律丢掉；`javascript:` / `file:` / `content:` / `data:` 等不显示成链接、点了也不动。网页里的非 http 跳转只在用户真的点了、页面在屏幕上时才放行。
  页面拿不到 hub 的 token（`loadUrl` 不带任何头，token 只在 OkHttp 的请求里）；不允许文件 / content 访问、不允许混合内容、不允许脚本开新窗口；
  证书只认系统 CA（`network_security.xml` 的 `trust-anchors`，用户装的 CA 不认）。

还没做（iOS 有）：飞书的会话页（收件箱里的飞书消息仍只看 hub 给的原文）、邮件的翻译（设置 › 翻译）、手机数据里的提醒事项、
终端的配色和字体、电脑尺寸下按手机宽度折行、往上翻 `/screen/before` 的历史，手表。协议都在，后续按 iOS 的做法补。

hub 拒绝这台手机的 token（401：在别的设备上撤销了、换了 hub）时不再无限重连，直接回到配对页并说明原因；已配对时打开配对链接也会回到配对页重新配。

## 通知（不用推送服务）

国内卖的安卓手机大多没有 Google 服务（FCM 收不到），各家厂商推送又要开发者账号和实名认证，所以第一版不接推送：

- App 开一个前台服务（`HubService`，类型 `specialUse`）一直连着 hub 的 WebSocket，通知栏常驻一条（下面「常驻通知」：有 Agent 需要你 / 在工作 / 做完未看时是汇总，没有时是「已连接 hub」，最低优先级，不响）。
  网络变了马上重连，断线指数退避到 30 秒；开机后自动启动（`BootReceiver`）。
- 收到 `interaction` 就弹「需要你处理」通知（高优先级），能直接在通知上点：授权「允许 / 拒绝」、计划「批准 / 打回（可输入反馈）」、
  单选提问的前 3 个选项、终端对话框的前 3 个选项。`interaction_resolved` 时收回通知。
- 设置 › 通知（`SettingsPrefs`）：「需要你处理时」关掉就不弹「需要你处理」；「Agent 做完时」不推送 / 每次都推 / 只推长任务（这一轮从开始干到做完超过 hub 的 `long_turn_seconds`，没看到开头的一轮算长）。
- 一轮做完（工作中 → 完成未看）且 App 不在前台时弹「做完了」。对话页只在 App 真的在前台时才上报已看（`/seen`），
  否则放在后台的对话页会把「做完了」吞掉。
- 规则对照 hub 给 iPhone 的推送（`docs/server.md`「推送策略」），但时机在手机上算：需要你立即弹；做完了不等 `done_delay_seconds`。
- 代价是耗电和被系统清理。小米 / 红米（MIUI、HyperOS）等要在系统设置里把这个 App 的省电策略设成「无限制」并打开「自启动」，
  设置页有两个入口按钮（电池优化白名单、MIUI 的自启动管理页，找不到时退回应用详情页）。以后要接厂商推送时，hub 侧加一个和 APNs 并列的发送器即可。

## 常驻通知、小组件、快捷方式

iPhone 的实时活动（`docs/live-activity.md`）和 App Intents（`docs/intents.md`）在安卓上的对应，代码在 `fleet/`、`widget/`、`shortcuts/`。
三处画的是同一份汇总 `FleetState`（`fleet/Fleet.kt`），规则照 hub 的 `buildFleet`：需要你（最早的请求在前）→ 工作中（跑得最久的在前）→
做完未看（最新的在前），不算已看、离线、屏蔽的、`/loop` 等下次唤醒的，助手只在问你时算。手机自己从 WebSocket 的状态算（`FleetStore`，
防抖 300 ms），最后一份存在手机上，小组件和磁贴在进程没跑时也有东西画。

- 常驻通知：就是前台服务那一条（同一个 id），有东西时换到「实时状态」频道（低优先级、不响）：标题「2 个需要你」，副标题其余的数量，
  展开是前三个 Agent（名字 · 在做什么（机器 · 空间 › 标签）），再多是「+N 个」；只剩工作中时计时从最久那一轮开始在手机上走。
  第一个授权带「拒绝 / 允许」、第一个计划带「查看 / 批准」（批准用 `acceptEdits`，和「需要你处理」通知一样）；「允许 / 批准」要先解锁。
  点通知：只有一个 Agent 时进它，否则进 Agents。只剩做完未看的 5 分钟后、全都处理完、hub 连不上时，退回原来那一行「已连接 hub / 正在连接 / 连不上」
  （「后台连接」频道，最低优先级）。锁屏上只显示数量和「有个授权等你」（公开版本），系统「锁屏显示敏感内容」打开时才显示全部。
  在系统设置里关掉「实时状态」频道，就只剩连接那一行；关掉通知权限，什么都不发。
- Android 16：通知带 `android.requestPromotedOngoing` 和 `POST_PROMOTED_NOTIFICATIONS` 权限，申请成为 Live Update（状态栏上的胶囊）。
  App 按 compileSdk 34 编，所以 extra 的名字是写死的字符串；更早的版本就是普通的常驻通知。
- 按钮的 PendingIntent 一个（请求, 按钮）一个，用 data URI 区分、不可变，后来重画的通知改不了用户解锁时系统手里那一个；
  打开 App 的 intent 也各带自己的 data URI（`jarvis-open://session/…`、`jarvis-open://tab/…`），只有 extras 不同的 intent 会被系统当成同一个。
  `FleetActionReceiver` 用 `goAsync` 撑住广播等 hub 回复（最多 9 秒），只为这个广播拉起的进程否则 onReceive 一返回就被冻住、点击丢掉。
- 主屏幕小组件（Glance，`widget/JarvisWidget.kt`）：「✦ Jarvis」、带状态色点的标题、其余数量，按高度画 0 / 2 / 3 个 Agent（名字、在做什么 · 机器），
  再多是「+N 个」。点 Agent 进它，点别处进 Agents；只导航，不能在小组件上批准。数据在 `provideContent` 里面读（`collectAsState`）：
  Glance 保留会话时 `updateAll()` 只重组，在外面读到的值会原样再画一遍。浅色 / 深色跟随系统。
- 快速设置磁贴（`JarvisTileService`）：「Jarvis · 2 个需要你」，有需要你或工作中时亮；点了和点通知一样。没配对时不可用。
- 长按图标的快捷方式（运行时发布，静态的 res/xml 要写死 application id）：问 Jarvis、批准待处理，后面跟着最近活跃的 2 个在线 Agent（点了直接进它的对话，对应 iOS 的 `OpenSessionIntent`；会话列表变了才重新发布，断开 hub 时一起去掉）。「问 Jarvis」进 Jarvis tab 并把光标放进输入框、弹出键盘
  （主对话或首页，等 tab 选好了对话再聚焦）；「批准第一个待处理」弹一个对话框，先从 hub 现拉待处理列表、取最早的一条，
  授权「允许」/ 计划「批准并自动执行」（`bypassPermissions`，和 iOS 的 intent 一样），提问和终端对话框说明要打开 App。
  这个对话框的 Activity 不导出，只有启动器代我们启动的快捷方式能进来。
- 进程在后台被拉起（磁贴、小组件更新、通知按钮）时，Android 12+ 不许开前台服务：`HubService.start` 吞掉这个异常，
  App 下次回到前台（`MainActivity.onStart`）或开机时再开。以前直接崩，连带磁贴 / 小组件 / 按钮一起没了。

## 语音对话

hub 有在跑的语音插件（`docs/plugins.md`）时，助手页的输入框多一个麦克风（再点一下「助手」的菜单里也有「语音对话」），进语音页（`ui/VoiceScreen.kt`、`voice/`）：
全双工，一直在听（静音也照送，什么时候轮到谁说由模型决定），模型说话时直接开口就能打断；底下三个带字的圆按钮静音 / 打断 / 挂断；字幕按 id 原地替换，自己说的在气泡里、Jarvis 说的是正文。
模型和音色在页面右上角的菜单里选，存在本机（每个插件一份），换了就重开一次对话。录音是 `VOICE_COMMUNICATION` 音源 + 系统回声消除 / 降噪，
外放走扬声器（接了耳机 / 蓝牙就走它们）；对话期间屏幕常亮，离开页面就结束（不在后台录音）。

## 构建和安装

```sh
./scripts/android.sh          # 编译 debug 包、装到连着的手机 / 模拟器并打开
./scripts/android.sh apk      # 只出 APK：android/app/build/outputs/apk/debug/app-debug.apk
./scripts/android.sh test     # 单元测试（模型解码、行合并、Markdown、设计系统的基线）
./scripts/android.sh shots    # 各页面的截图（浅色 / 深色，mock_hub.py 的数据）→ android/app/build/shots/<名字>.png
./scripts/android.sh shots Shots   # 只跑一个截图类
```

- 需要 Android SDK（`$ANDROID_HOME`，默认 `~/Library/Android/sdk`）和 JDK 17；没有 JDK 17 时脚本用 Android Studio 自带的。
- Application ID 走配置：`~/.config/jarvis/config.json` 的 `android.application_id`，没写就用 `app.bundle_id`，再没有就是 `com.example.jarvis`。
- Gmail 的「使用 Google 账号登录」要接 Google 的回调：`config.json` 的 `android.google_oauth_scheme`（hub `google.client_id` 的反向 client ID，
  `docs/mail.md`「Android」），`android.sh` 传给 gradle，进 `OAuthRedirectActivity` 的 intent filter。没配时那个 scheme 是占位的，添加 Gmail 会提示改用应用专用密码。
- App 图标和 iOS 是同一张（`apple/Jarvis/Resources/Assets.xcassets/AppIcon.appiconset/icon.png`）：自适应图标的前景是这张图缩进到 108 dp 画布中间的 72 dp 安全区
  （`drawable/ic_launcher_fg.xml` 内缩 18 dp，图在 `drawable-nodpi/ic_launcher_art.png`），背景是图边缘的深棕 `#1B110D`，桌面裁成圆形还是圆角方形都不会切到圆环；
  主题图标（Android 13+）用 iOS 的 tinted 版当遮罩。通知栏小图标 `ic_stat` 是同一个标志（圆环 + 四角星）的白色剪影。iOS 换图标时这两张 PNG 要重新导出。
- hub 默认是 HTTP（内网 / VPN），`network_security.xml` 放开了明文流量；不要把 hub 暴露到公网。
- 装到真机：手机开 USB 调试连上电脑跑 `./scripts/android.sh`，或者把 APK 发到手机上直接装。

## 界面风格

- token 在 `ui/Theme.kt`：`AppStyle`（五种风格、圆角、阴影、是否圆体）和 `JarvisPalette`（主色、页面、卡片、描边、文字三级、填充、自己的气泡、
  danger / positive / caution / ai / brand / loop、浮在页面上的控件底色 `floating`），数值抄 `apple/Shared/Design/Styles/`。`schemeOf` 把它映射成 Material 的
  ColorScheme，Material 自己的组件（菜单、对话框、开关）跟着走；页面读 `JTheme.colors` / `JTheme.type` / `JTheme.style`，不要 `when (style)`。
- 映射约定：`primary` 主色（白字）、`primaryContainer` 自己的气泡、`tertiary` 是 ai（✦ / AI 分身那行）、`surfaceContainerHighest`（Material Card 的底）是风格的卡片、
  `onSurfaceVariant` 是不透明的次要灰（iOS 的半透明 secondaryLabel 在灰页面上只有 3.3:1，`JarvisPalette.secondaryText` 同一个值）、`outline` 加深过。
- 黏土用 Nunito（SIL OFL，`res/font/nunito_variable.ttf`，许可在 `assets/licenses/nunito-OFL.txt`），只有拉丁字母，中文仍是系统字体；字号级别（`JTypeScale`）整套换成它。
- 表面画法（`Modifier.surface`，iOS `.surface(…)`）：液态玻璃 / Claude 是白卡片（深色是抬高的灰）+ 发丝描边，浮在页面上的控件再带一层柔和阴影（安卓不模糊背后，
  没有真正的玻璃）；黏土是软块 + 彩色阴影 + 白边；Island 是大而柔的阴影；飞书是灰色发丝描边、无阴影。展开的工作过程：液态玻璃是时间线（左边一条细线，
  工具的圆形图标坐在线上，工作中间的话是小一号的灰字），其他风格是卡片，同 iOS `StyleSpec.workList`。
- 外壳的中性色（`ShellColors`：助手对话的灰气泡、切换按钮的选中块、输入框、发送键的墨色、侧边栏底色、「新对话」药丸）各风格都一样，同 iOS `Theme.Shell`。
- 可读性底线由 `StylesTest` 查（五种 × 深浅）：主色上的白字 ≥ 3.5:1（飞书深色 `#4C88FF` 是飞书自己的蓝，3.35:1，和 iOS 一样放过）、
  自己气泡的字 ≥ 4.5:1、主色当链接对页面和卡片 ≥ 3:1、正文 / 次要文字在页面、卡片、浅底、错误底上 ≥ 4.5:1。
- 还没做：字号档位（小 / 标准 / 大，iOS `TextSize`；安卓跟随系统字体大小）、形象（persona）。

## 折叠屏和大屏

2026-11-01 用户的安卓机是折叠屏（书本式，内屏接近正方形）；同一天用户又要求两栏中间能拖、右栏别太小，并且「别只针对我的机型」。
所以按窗口宽度和系统报的折痕排版，不看机型（同 iOS 的 iPhone Duo / iPad，`docs/ios.md`「iPhone Duo」）：

- **单栏**（宽度 < 600dp：手机、折叠屏的外屏、窄的分屏 / 小窗）：上面「外壳」那一套。
- **两栏**（≥ 600dp：展开的书本式折叠屏、三折屏、平板、横放的手机、宽的分屏 / 桌面窗口）：顶栏铺满整个宽度（实心，下面一条发丝线），下面左右两栏。
  - 助手：左栏是侧边栏（认识 Jarvis、收件箱、定时任务、知识库、机器、对话历史），右栏是对话或新对话页。
  - Agents：左栏是列表（打开的那个 Agent 那一行灰底），右栏是它的对话（没有返回键，旁边就是列表），没打开时是「选一个 Agent」。
    通知、链接打开的 Agent 对话也落在右栏（自动切到 Agents）。返回键先关掉右栏的对话。
  - Agents 列表里点空间，空间页也在右栏打开；空间页里点的对话压在它上面，返回回到空间页。
  - 收件箱：左栏列表、右栏是点开的那件（详情和回复，和手机上的底部浮层同一份内容，最宽 760dp）；选中的那行灰底。
  - 设置：左栏菜单（右上角 ✕）、右栏是点开的那一页，那一页里再进的页面在右栏里压栈。
  - 左栏点的替换右栏，右栏里点的压栈；右栏的第一页没有返回键（旁边就是列表，`LocalPaneRoot`），压上去的页照常有。
    系统返回一层层退右栏（对话 → 空间页 / 设置子页 → 占位），退到底才离开。右栏空着时是安静的占位：「选一个 Agent」「选一件事」「选一项设置」。
  - ☰ 收起 / 展开左栏（右栏铺满，对话最宽 760dp 居中）；Agents 什么都没打开时列表一直在。没有可拖出的抽屉。
  - 知识库、机器、Jarvis 资料页这些推进来的页面照样盖住整个窗口，内容最宽 760dp 居中（`readableColumn`）。
  - 开合时打开的东西跟着走：收件箱打开的那件、设置的页、空间页（窄屏是推进来的一页，宽屏在右栏）都在外壳里存着（`rememberSaveable`），换布局不丢。
- **中间的分隔条能拖**（`ui/shell/Panes.kt`，同 Material 的 pane expansion、iPad 的分栏）：一条发丝线，中间一颗小药丸；线两边各 12dp 都能按住拖
  （鼠标 / 触控板悬停时是左右箭头光标，TalkBack 有「左栏变宽 / 左栏变窄 / 收起左栏」）。
  - 拖的时候左栏跟手（位置只在排版阶段读，两栏只重排不重组），经过折痕轻震一下。
  - 松手：离平放的折痕 28dp 以内就吸到折痕上，否则停在松手处（弹簧接着手指的速度）；往左拖过左栏最窄宽度 64dp 以上（列表跟着变淡）松手就收起左栏，
    ☰ 再打开。Agents 没打开对话时列表不能收，停在最窄。
  - 位置按「占窗口宽度的比例」每处（助手、Agents、收件箱、设置）各记一份（SharedPreferences `ui` 的 `ui.split.assistant` 等），
    窗口宽度变了（转屏、开合、分屏、平板）照比例换算，再夹回两栏的最窄宽度。
- **默认分在哪里**（`ui/shell/Fold.kt` 的 `paneSplit` / `settleDivider`，`FoldTest`）：
  - 两栏都有最窄宽度，怎么拖都不会更窄：左栏 280dp，右栏 320dp（手机的宽度）。
  - 没拖过：左栏约占 38%，夹在 320–420dp 之间，右栏（对话）能留 400dp 就留 400dp。展开的内屏 771dp → 320 / 451（以前对半分是 385 / 385，
    用户嫌右边小）；Z Fold 那种 700dp → 300 / 400；三折屏全展开 1061dp → 403 / 658；平板横放 1280dp → 420 / 860；横放的手机 800dp → 320 / 480；
    最窄的两栏 600dp → 280 / 320。
  - **平放的折痕**（Jetpack WindowManager 的 `FoldingFeature`，`isSeparating == false`）只当吸附点：两栏可以跨过它（平放时折痕几乎看不见），
    默认位置离它 28dp 以内也吸过去（三折屏的折痕在 1/3、2/3，有的分辨率下默认位置正好落在第一条上）。
  - **分开的折痕**（`isSeparating`：书本式半折成「书」的姿势、两块屏中间有缝的双屏机）：两栏都不跨过它，固定分在折痕处（有缝就空出缝），
    不显示药丸、不能拖；有两条（三折屏半折）取离默认位置近的那条。放平后回到用户拖的位置。
  - 折痕离边太近（某一栏会窄过最窄宽度，比如窗口只盖住屏幕的一部分）就不管它；横向的折痕（翻盖机、书本式横放）不分栏。
  - 系统报不出折痕时（有的厂商没接 WindowManager 的扩展）效果一样，只是少了吸附。
- **开合不重建**：`MainActivity` 自己接 `orientation|screenSize|smallestScreenSize|screenLayout|density|keyboard|keyboardHidden|navigation`，
  开合时只是重新排版：打开的对话、终端（按新的列 × 行重排接管的窗格）、链接卡片都还在。以前每次开合都重建 Activity：打开着的对话关掉、
  从通知进来的那个对话又被打开一次、通知权限又问一次。输入框里没发的字存在 `Drafts`（进程内，按会话），所以对话从单栏挪到右栏（或反过来）字也不丢。
  别的原因重建时（换主题之类）打开的对话存在 `onSaveInstanceState` 里，启动的 intent 只处理一次。
- 横放时侧边的摄像头挖孔、三键导航条：整个外壳按 `WindowInsets.safeDrawing` 的左右留白，折痕位置减掉左边的留白再换成 dp。
- 窄的左栏里，空间标题行先给空间名，剩下的才给置顶图标和机器名（以前机器名和状态数挤掉了空间名，只剩「…」）。
- 为什么不用 Material 3 adaptive 的 `ListDetailPaneScaffold`：它自己管一个导航栈，而这个 App 的导航是外壳里几个状态（通知、小组件、快捷方式、链接都直接改它们），
  没有 NavHost；接进去要么两套栈同步，要么把所有入口改成它的 navigator。这里要的是「按宽度二选一 + 折痕位置 + 能拖的分隔条」，两栏容器（`Panes`）
  和纯函数的分栏规则（`Fold.kt`，能单测）都不大，只多一个 `androidx.window` 依赖（adaptive 库底下也是它）。
- 截图（`FoldShots.kt`，`LocalHingeOverride` 模拟折痕、`ShellRoot(startShares = …)` 模拟拖过的位置）：书本式内屏 2312 × 2504（平放、半折、拖到最右）、
  内屏横放、外屏 1120 × 2528、窄长的内屏 700 × 840dp、三折屏 3184 × 2232（两条折痕）、平板横放 1280 × 800dp、横放的手机 800 × 360dp。
  拖的过程截不了（Paparazzi 只出一帧），松手后落在哪由 `FoldTest` 测。
- 还没做 / 没在真机上看：拖动的手感（这台机器没有 KVM，跑不了模拟器，只有逻辑测试和静态截图）、半折成帐篷 / 笔记本时（横向折痕）的上下分屏、
  开合时的过渡动画。系统设置里如果把这个 App 设成了「原比例」之类的兼容显示，要改回全屏。

## 设计系统

2026-11-01 用户要求「全面升级安卓的交互体验，和 iPhone 版差了很多」：照 iOS 的设计系统（`CLAUDE.md`「设计系统（iOS）」、`docs/ios.md`「设计语言」）
在安卓建了同一套，页面全部改用它。代码在 `ui/design/`：

| 要什么 | 用什么 |
| --- | --- |
| 字号 | `JTheme.type` 的级别（display 28 粗 / title 22 / heading 17 半粗 / body 17 / secondary 15 / caption 13 / sectionHeader 13 半粗 / label 12 / code 13 等宽）和强调（`bodyStrong`、`secondaryMedium`、`captionStrong`、`labelMedium`…），`Type.kt`。单位 sp，跟随系统字体大小；每级带 iOS 的行高，首行顶和末行底裁掉，单行就是字的高度。同一套也映射到 Material 的 Typography |
| 间距 | `JSpace`：4dp 网格 xxs 2 · xs 4 · sm 6 · s 8 · m 12 · l 16 · xl 24 · xxl 32；页面离屏幕边 16（`JSpace.page`） |
| 圆角 | `JRadius`：badge 6 · control 10 · card 14 · bubble 20 · panel 22 · page 32；分组卡片用风格自己的（`JTheme.style.corner`） |
| 尺寸 | `JSize`：线 / 点 / 图标 / 图标框 / 头像 / 点按区域（44）/ 盒子，名字和 iOS `Theme.Size` 一样 |
| 动画 | `JMotion`：state 0.25 s、layout 0.3 s、reveal / conceal、侧边栏的无回弹弹簧 |
| 颜色 | `JTheme.colors` 的角色色；状态色只有一张表 `StatusPalette`（需要你红、工作中琥珀、完成未看绿、已看灰，和 iOS、小组件、通知一样，不随风格变），页面用 `JTheme.tone(state)`；`JTheme.diff`、`JTheme.hue`、`JTheme.shell` |
| 图标 | `JIcons`（`Icons.kt`）：一个意思一个 Material 圆角图标，名字对着 iOS `Theme.Icon`；Jarvis 自己的标志 `JarvisMark`（App 图标同款：夜色底、珊瑚光环、四角星） |
| 组件 | `Modifier.surface`、`Modifier.cardSlice`（长列表逐行画的卡片，只描外沿）、`GroupCard`、`GroupFooter`、`SectionHeader`、`RowDivider`、`GroupRow`、`StatusDot`、`Chip`、`CountBadge`、`ChoiceChip`、`PillButton`（Prominent / Tinted / Neutral / Destructive）、`CircleButton`、`IconAction`、`IconTile`、`EmptyState`、`Monogram`、`LetterTile`、`SearchField`、`JTextField`、`SegmentedControl`、`JSlider`、`RoundControl`、`FittingRow`、`ErrorLine`、`PageLoading`、`PageHeader` / `PageScaffold`、`JSheet`、`TopFog`、`Haptics`（`Components.kt`、`Controls.kt`、`Chrome.kt`、`Surfaces.kt`）；设置类页面用 `ui/settings/SettingsKit.kt`（`KitPage` / `KitGroup` / `KitRow`…，也在设计系统上） |

- 页面里不写死字号、间距、圆角、颜色、图标名，缺什么先在 `ui/design/` 加一个有名字的 token 或组件。`DesignTokensTest` 扫 `ui/` 的源码（`ui/design/`、`Theme.kt` 除外），
  每个文件写死的字号（`fontSize =`、`MaterialTheme.typography`）、`Color(0x…)`、dp 数值、`Icons.…` 只能减少；减少后删掉 `src/test/resources/design-baseline.json`
  重跑一次（`-PrecordDesignBaseline`）记下新的基线，新文件必须是 0。不许为了过测试调高基线。终端（`terminal/`）、图片查看 / 编辑器、图表这类按自己几何画的地方是例外，按基线管着。
- 页面的骨架：推进来的页面用 `PageHeader`（左边圆形返回、标题居中、右边圆按钮）；列表是一组组圆角卡片（`GroupCard` + `SectionHeader`，长列表每行自己画卡片的一段 `CardSlice`）；
  按钮一律 `PillButton`（单行，`maxLines = 1`）；输入框 `JTextField`；浮层 `JSheet`；空状态 `EmptyState`；Material 的 `DropdownMenu`、`AlertDialog`、`Switch`（`jSwitchColors()`）照用。
- 提示（`Toasts.show`）：App 在前台时是外壳里的胶囊（`ToastHost`，iOS 的 toast：状态栏下、带成功 / 出错 / 信息的图标），在后台时退回系统 Toast。

## 代码

| 文件 | 内容 |
| --- | --- |
| `data/Models.kt` | 线上类型（字段全可选，未知字段忽略，Go 的零时间当空） |
| `data/ApiClient.kt` | REST；错误统一成 `HubException(status, message, code, fields)` |
| `data/Engines.kt` | 引擎选择：选项、预选、能不能选、检测说明（`Engines`），选择页的状态和保存（`EngineSelection`），配对后那一步要问的机器（`PairingEngine`） |
| `data/Hub.kt` | WebSocket、重连、machines / sessions / interactions 的状态和事件流 |
| `data/ChatStore.kt` | 一个会话的 items：`covered`、`history`、补洞（`after_seq`）、往前翻页 |
| `service/HubService.kt`、`Notifier.kt`、`ActionReceiver.kt` | 前台服务、通知、通知上的按钮 |
| `chat/` | `Outbox`（发出去还没进 transcript 的消息、离线时存盘排队，连上后按顺序发）、`ConnectionWatch`（标题位的「等待网络 / 连接中… / 更新中…」，看网络回调、OkHttp 的 ping / pong 计数和打开着的对话的 backlog） |
| `terminal/` | 实时终端：`TerminalGrid`（VT 子集，移植自 iOS 的 `TerminalGrid.swift`）、`TerminalFeed`（帧按序应用、接管时自己攒滚出屏幕的行）、`TerminalLink`（订阅 / 续传 / 接管 / 轮询降级）、`TerminalKeys`（键名 → 终端字节）、`BoxDrawing`、`TerminalScreen` |
| `ui/design/` | 设计系统：`Tokens`（JSpace / JRadius / JSize / JMotion）、`Type`（字号级别）、`Colors`（状态色、外壳 / diff / 类别色）、`Icons`（JIcons）、`Surfaces`、`Components`、`Controls`（分段、滑块、通话按钮、错误行…）、`Chrome`（页头、搜索框、浮层、输入框、触感）、`JarvisMark` |
| `ui/shell/` | 外壳：`Shell`（两页、页面栈、设置浮层、路由；宽窗口的两栏 `Panes`）、`Fold`（折痕、分栏规则）、`TopBar`（☰ · 助手 \| Agents · 圆按钮）、`Drawer`（拖出侧边栏）、`Sidebar`、`AssistantModel`（助手的对话列表、当前对话、主对话、置顶）、`AssistantPane`（对话或新对话页 `ShellHome`） |
| `ui/` | Compose 页面：`AgentsScreen`（含 `AgentsListOptions`）、`ChatScreen`、`InteractionCard`、`PairScreen`、`SettingsScreen`、`StylePicker`、`Markdown`（段落、列表悬挂缩进、表格、代码块）；`Theme` 是界面风格的 token；`Rows.kt` 是对话的行模型（工具合并、按轮折叠、消息里的标签） |
| `data/Knowledge.kt`、`ui/knowledge/` | 知识库：线上类型、项目分组、搜索结果合并、时间线解析、「问 Jarvis / 开 Agent」带的上下文（`KnowledgeTest`）；列表页和条目页、打标 |
| `fleet/`、`widget/`、`shortcuts/` | 常驻汇总通知和它的按钮、主屏幕小组件、快速设置磁贴、长按图标的快捷方式 |
| `settings/` | 设置和机器的线上类型、REST（`SettingsApi.kt`，`ApiClient.raw` 上的扩展）、图表的数学（`ChartMath`：刻度、按字节的整数刻度、单调曲线）、通知设置（`SettingsPrefs`）、存储空间统计（`Storage.kt`） |
| `ui/settings/`、`ui/machines/` | 设置的子页面和它们共用的分组、行（`SettingsKit.kt`，颜色取自 MaterialTheme）；机器列表、机器页、监控页和曲线（`MetricCharts.kt`） |
| `ui/shell/` | 外壳（`Shell.kt`）、顶栏、侧边栏、抽屉、助手页；宽窗口的两栏（`Panes.kt`：能拖的分隔条、每处记住的比例）和分栏规则（`Fold.kt`：折痕、默认位置、松手吸附，`FoldTest`） |
| `ui/web/` | 链接卡片：`LinkSheet`（`Links`、`LinkHost`、卡片、上下文小条）、`WebViewPool`（预加载、选中菜单、`externalIntent`）、`LinkRules`（纯逻辑，`LinksTest` 覆盖） |
| `ui/chat/` | 对话的行（`ChatViews`、`ToolViews`：工作行、工具详情、diff、待办）、输入框（`Composer`：Agents 的 `ChatComposer`、助手的 `ShellComposer`）、消息菜单、提示（`Toasts`、`ToastHost`）、`ChatEnv`（行要用的：打开文件 / 图片、当前目录、整段对话的图片） |
| `ui/media/` | 图片：按需加载和缓存（`ChatImageLoader`）、全屏查看器、编辑器、保存 / 分享 / 拷贝（`MediaShare`，分享走自己的 `ChatFileProvider`） |
| `mail/`、`ui/mail/` | 邮件：线上类型和接口（`MailApi`，`ApiClient` 的扩展）、写信的逻辑（`MailCompose`：地址解析、回复 / 转发的草稿）、HTML 的第二道防线（`MailHtml`）、添加邮箱的表单（`MailAccountForm`）、连接器每行的字（`ConnectorText`）、Google 登录（`GoogleSignIn` + `OAuthRedirectActivity`）、进程里共用的账户 / 飞书状态 / 读过的信（`MailStore`）；读信（`MailThread`、`MailWebView`）、写信（`ComposeMail`）、邮箱和添加邮箱（`MailAccountsScreens`）、连接器和飞书页（`Connectors`）。`MailComposeTest`、`MailHtmlTest`、`MailAccountFormTest`，截图 `MailShots`（`MOCK_MAIL=1`） |
| `ui/files/` | 文件和 Git：接口（`FilesApi`）、浏览器、文件查看器、Git 页面、diff 解析（`DiffText`，对话里的 diff 也用它） |

## 验证记录

截图（`./scripts/android.sh shots`，2026-11-01，开发机没有 KVM、跑不了模拟器时就靠它）：Paparazzi 在 JVM 上用 layoutlib 画 Compose，连一个本地起的
`scripts/mock_hub.py`（`shots/MockHub.kt`，随测试进程起停；文件和 Git 的页面另起一个 `MOCK_FILES=1` 的），外壳（助手主对话、新对话页、Agents、侧边栏）、
对话页（提问、计划、工作中、展开的工作过程：液态玻璃的时间线和黏土的卡片）、收件箱、机器、空间页、设置、知识库（列表、条目、搜索、打标）、
Jarvis 资料页（定时任务和编辑、跟进、运行记录、关于我、记忆）、配对、界面风格、语音、文件和 Git、全屏查看器、链接卡片、图标、
对话里的小部件（API 报错卡片、排队中的消息、「/」快捷命令，`ChatPartsShots`）、配对后的选引擎、模型配置、
设置 › 手机数据的「健康」一行（`HealthShots`：关、开且全部授权、部分授权、都没授权、没有 Health Connect 的「获取」；Health Connect 在 Paparazzi 里问不了，状态作为参数传进去）、
折叠屏和大屏（`FoldShots`：两栏的 Agents / 空间页 / 收件箱 / 设置，见「折叠屏和大屏」），各拍浅色 / 深色；
Agents、计划卡片、空间页另在其余四种界面风格下各拍一遍，逐张看过（共 165 张）。底部浮层（ModalBottomSheet）是单独的窗口，Paparazzi 画不出来，
测试把浮层的内容画在页面上的一个浮层框里（「会话信息」「关闭 Agent」的确认只看了代码，它们和别的浮层一样是分组卡片 / 按钮）。手势（侧边栏拖动、拖分隔条、左滑已处理、左滑关闭）、
键盘、拍照、听写、真实网络、通知没在这条路上验证，要真机或模拟器。

模拟器（Android 14）连真实 hub，一台 Linux 跑 hub、一台 Mac 经 SSH 接入：

- 扫码链接配对、换设备 token；两台机器在线，列表显示真实会话
- 手机发消息到 Mac 上 herdr 里的 claude；claude 的 AskUserQuestion 变成卡片（hook 拦下的走 `question`，TUI 里的走 `screen`），在手机上选择后 claude 收到
- 计划模式下 ExitPlanMode 的对话框在手机上批准，claude 继续执行改了文件
- App 在后台：提问通知上直接点选项，claude 收到；一轮做完弹「做完了」
- 实时终端：打开 Mac 上 Claude 的窗格，接管成 46×39（herdr `session control`），打字、退格、^C 写进终端，上下拖滚 Claude 的对话；
  电脑上另一个程序占着时弹「接管 / 只看」；切「电脑尺寸」改成按 120 列镜像；关掉页面后 control 进程退出、窗格回到电脑尺寸
- hub 重启后几秒内自动重连；长会话往前翻到两天前的内容；深色模式截图检查
- 界面风格：模拟器连 `scripts/mock_hub.py`，五种风格 × 浅色 / 深色截了 Agents、两种交互卡片、Jarvis 主对话和抽屉、资料页、收件箱、设置、风格选择页、配对页（切风格可以直接写 `shared_prefs/ui.xml` 后重启 App）

真机（Redmi K60 Pro，Android 16，澎湃 OS 3，没有 Google 服务），手机经 USB（`adb reverse tcp:18660 tcp:18660`）连 hub：

- 配对、列表、对话页正常；claude 的提问在手机上回答，电脑上的 claude 收到
- 「需要你处理」的通知系统里有（channel `input`，重要级别最高），但默认不在通知栏显示：澎湃 OS 给新装的 App 关掉了悬浮 / 锁屏通知，
  要在「设置 › 通知与控制中心 › 通知管理 › Jarvis」里打开。锁屏后长时间的后台连接还没在真机上测
- 2026-10-31 复测（最新版）：「需要你处理」的通知弹出来了（通知栏里一条「android-demo · 请选择 A 或 B？」，常驻那条变成「1 个需要你」），
  点开进对话、在交互卡片上选 A，电脑上的 Claude 收到，通知随之收回
- 实时终端：接管成 54×50；搜狗输入法打拼音时拼音留在输入法自己的候选栏，选「你好」后电脑上的窗格里出现「你好」，退格一次删一个字；关掉后窗格放回电脑

收件箱「交给 AI」（模拟器连真实 hub，hub 库里临时放一件假邮件，测完删掉）：交给 Mac 上的 Agent，对话页打开、Claude 收到并回复，消息显示成上下文卡片；交给 Jarvis 新开对话并回复；件标为已处理。

常驻通知、小组件、磁贴、快捷方式（模拟器 Android 16 base 镜像，连 `scripts/mock_hub.py`）：

- 通知展开是汇总和「查看 / 批准」，点「批准」hub 收到 `{approve:true, mode:"acceptEdits"}`，点「查看」进那个会话；深色 / 浅色截图检查
- 小组件放到主屏幕、点 Agent 进会话；磁贴加进快速设置、显示「2 个需要你」、点了进 App
- 「批准第一个待处理」弹框后批准，hub 收到 `bypassPermissions`；「问 Jarvis」在冷启动和 App 已经开着时都进 Jarvis 并弹出键盘
- 没验证：通知上的「允许 / 拒绝」（假 hub 没有授权请求，代码和「批准」同一条路）；Live Update——这个镜像上通知没有被提升（flags 里没有 PROMOTED_ONGOING），
  要在 Android 16 QPR1 以后的真机上看；真机上的锁屏表现

## 经验

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

- 折叠屏开合会改 `screenSize` / `smallestScreenSize`，没声明 `configChanges` 时 Activity 整个重建：Compose 的 `rememberSaveable` 能恢复，
  但放在 Activity 字段里的状态（`MainActivity.openSession`，通知打开的对话）不会，开合一次对话就关了；重建时还会把启动 intent 再处理一遍，
  重新打开用户已经关掉的通知会话。现在声明了 `configChanges`，打开的对话也写进 `onSaveInstanceState`，启动 intent 只在第一次 `onCreate` 处理。
- 自己画的面板（双栏的右栏、占位、Jarvis 的左栏）底色要用 `Surface(color = …)`，不能用 `Modifier.background`：只有 Surface 会设 `LocalContentColor`，
  没写颜色的 `Text` 默认是黑的，深色模式下标题、正文黑字压在黑底上看不见（原来的底部浮层、Scaffold 自带 Surface，所以单栏没这个问题）。
- 全屏看图别放进 Compose 的 `Dialog`：对话框窗口停在状态栏和导航栏之间（`decorFitsSystemWindows = false`、`FLAG_LAYOUT_NO_LIMITS` 都试过，上下照样露出底下的页面），
  直接画在对话页上面（`ImageViewerOverlay`），状态栏图标打开时改成浅色、关掉时还原。
- `Markdown` 默认包在 `SelectionContainer` 里，长按会变成系统的选中文字，消息菜单弹不出来：对话里的回复传 `selectable = false`，要选文字用菜单的「选择」。
- 倒置列表（`reverseLayout`）的 `verticalArrangement` 写成 `spacedBy(x)` 会顶着上边排，内容少时消息浮在顶上：要 `spacedBy(x, Alignment.Bottom)`。
- 对话页验证用 `scripts/mock_hub.py`：`MOCK_CHAT_IMAGES=1` 给「博客迁移」那段对话加两张截图（查看器、编辑器），文件和 Git 是编出来的 `~/code/weather`。
- `adb shell input swipe` 从屏幕最上方往下滑会拉出系统的快捷设置，测试翻历史时从屏幕中间开始滑。
- 小米 / 澎湃 OS 上 `adb shell input` 要开「开发者选项 › USB 调试（安全设置）」，开完要重连一次 adb（`adb reconnect`）才生效；
  之后偶尔连着几次 `SecurityException: Injecting input events requires … INJECT_EVENTS`，隔几秒再发就好，测试脚本里每次点击被拒就等 3 秒重试。
- herdr server 继承启动它的 shell 的环境：在带代理变量的 shell 里起 herdr，里面的 claude 也会走那个代理。联调时用干净的登录 shell 起 herdr。
- Claude Code 设置里放开了的工具（allow 规则、bypass 模式）不会触发 PermissionRequest，测授权卡片要在没放开的会话里。
- hub 只存一份 phone_* 工具列表（`setPhoneTools`），每台手机 `register_phone` 时整个替换，调用发给最后注册的那台（`phoneClient`）。
  所以安卓声明的工具要和 iOS 一模一样，不能多也不能改参数。安卓 10 以后只有在前台的 App 能读剪贴板，后台调 `phone_clipboard_get` 读到的是空，结果里会说明。
- 两台手机同时连着 hub 时，phone_* 调用发给最后注册的那台：测模拟器上的工具时要关掉真机上的 Jarvis，不然调用发到真机、它那边没开这个数据源就超时。
  Claude Code 只在启动时读一次工具列表，新加的工具要新开会话才看得到。
- 模拟器默认没有能写的日历账号，`phone_calendar_create_event` 会报「no writable calendar」；测的时候用 `content insert` 建一个 LOCAL 日历。
- 已配对时打开配对链接：配对页要等配对做完再清掉 `pendingPair`。先清掉的话 Root 立刻换回正常页面，配对页和它的协程一起被丢掉，链接等于没点。
- `JsonCodec` 的 `encodeDefaults = false` 会把等于默认值的字段省掉不发，而 hub 把缺的字段当成空 / false：定时任务的 `enabled = true`、
  `time = "09:00"` 被省掉，保存时被关掉或者 400。发给 hub、hub 又按「缺了就是空」处理的字段，要加 `@EncodeDefault`（`SetRoutinesTest` 逐字检查请求）。
- OkHttp 不会在一条死掉的连接上重试 WebSocket 升级：hub 前面的转发（adb reverse、ssh -L）断过一次后，每次重连都报 `unexpected end of stream`，直到重启 App。
  WebSocket 失败时要清空连接池（`connectionPool.evictAll()`）。
- 倒置的聊天列表（`reverseLayout`）在前面插入新行时保持第一个可见项不动：回复变长、新回复进来时人在最新处也会被留在原地。人在最新处（或刚发了消息）时要主动滚回 0。
- 新开会话时 node 靠输入框底栏的字样判断 claude 起来了（`server/internal/node/control.go` 的 `inputBoxVisible`）。有的 Claude Code 版本在手动确认模式下底栏是
  `⏸ manual mode on · ← for agents`，node 认不出，等满 1 分钟报「did not become ready」，其实会话已经能用：App 按「启动前没有、启动后在这个目录里的窗格」找到它并打开。
- 小米 / 红米：`adb install` 要先在开发者选项里打开「USB 安装」（要登录小米账号），手机上还会弹「通过 USB 安装」的确认框，几秒内不点就自动拒绝
  （`INSTALL_FAILED_USER_RESTRICTED`）。`adb shell input` 代点屏幕要再打开「USB 调试（安全设置）」，否则报 `INJECT_EVENTS`。
- 无窗口（`-no-window`）的模拟器上 `adb exec-out screencap` 有时拿到的是上一帧：点完马上截图像是没反应，其实已经跳过去了。截两次、中间等一秒。
- 只读地连真实 hub 看界面：配对页会 `POST /api/v1/auth/tokens` 在 hub 上多出一台设备。debug 包可以 `run-as` 直接写 `shared_prefs/pairing.xml`（`url`、`token`）再启动，看完 `pm clear`；别点会写 hub 的开关（Claude Code 设置、通知、手机数据）。
- 曲线的纵轴按字节时要按 1024 取整（`ChartMath.yTicks(binary = true)`），按十进制取整的刻度显示出来是「1.91 MB/s」「3.81 MB/s」；数值标签的宽度按最宽的那个量，固定宽度会被裁掉。
- 手机没法直接访问 hub 时（hub 在内网、手机在流量上），联调可以走 USB：电脑上把本机 18660 转到 hub，再 `adb reverse tcp:18660 tcp:18660`，
  手机配对时用 `http://127.0.0.1:18660`。
- 终端里打字：安卓输入法对每个英文词也走 composition（为了联想），等 commit 再发的话一个词要到空格才出现；所以输入框里的文字（含正在拼的）
  每次变化只发和上次的差（新字，或若干个退格）。按这个规则，拼音选字时终端里先出现拼音，选好后退格换成汉字（模拟器没有中文输入法，还没在真机上试）。安卓的 emoji 字体会把 ⏺ ⏸ ⏵ 画成两格宽的彩色图标，
  加 U+FE0E 也没用，这几个按形状自己画。
- hub 前面的转发断过以后，hub 那边可能还以为 node 在线，但每个调用都回 `rpc: connection closed`（/screen 也一样）。重启常驻 node
  （`launchctl kickstart -k gui/$UID/com.jarvis.node`）再让 hub 重连这台机器就好了，App 这边不用动。
- 假 hub（`scripts/mock_hub.py`）的 WebSocket 连上时不推待处理的交互：进 App 后下拉刷新一次才拿到交互。
- `adb shell am start` 启动一个和当前任务根 intent 一样的 intent 时只是把任务拉到前台（「Activity not started」），onNewIntent 不来；
  验证快捷方式要从启动器长按图标点，或者让 App 先停在别的页面。
- WebView 用 application context 建出来时页面永远是浅色：application context 没有 App 的主题（`isLightTheme` 读成 true），WebView 只在外面的主题是深色时才给页面
  `prefers-color-scheme: dark` / 算法变暗。池里的 WebView 包一层 `ContextThemeWrapper(app, Theme.Jarvis)`，`values-night` 里的 `Theme.Jarvis` 是深色并写明 `isLightTheme=false`。
- 往 WebView 的选中菜单加项：Chromium 在 `onPrepareActionMode` 里（每次选区变化）填菜单，它的项都要求显示成按钮，浮动工具栏先排这种；
  自己的项也要 `SHOW_AS_ACTION_ALWAYS`、order 比它小，才排在 Copy 前面。点了以后先 `evaluateJavascript` 读到选区再 `mode.finish()`，先 finish 的话选区已经清掉，读到空字符串。
- `scripts/mock_hub.py` 带 `MOCK_RICH=1 MOCK_LINKS=1` 时给对话、Jarvis 主对话、收件箱各放了几个链接。
