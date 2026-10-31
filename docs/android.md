# Android App

`android/`：Kotlin + Jetpack Compose 的 Android 客户端，连同一个 hub，接口和 iOS 一样（`docs/protocol.md`），服务端不用改。
minSdk 26（Android 8），targetSdk 34。界面中文。

一句话描述：在 Android 手机上看各台电脑 herdr 里的 Agent、发消息、回答提问 / 审批计划 / 授权、按终端对话框的选项，
不需要任何推送账号。

## 现在有什么

| 页面 | 内容 |
| --- | --- |
| 配对 | 扫 `jarvisd hub pair` 的二维码（`jarvis://pair?url=…&token=…`，也能从链接打开），或手动填地址和 token；配对后立刻 `POST /api/v1/auth/tokens`（`kind: phone`）换这台手机自己的 token，旧 hub 没有这个接口时继续用配对 token |
| Agents | 顶部切换「按项目 / 按状态 / 按机器」（记住选择），排序规则和 iOS `ChatsView` 一样，写在 `data/Grouping.kt` 里并有单测。上面固定是「需要你处理」卡片和置顶的 Agent（只出现一次），下面是分组，最后是最近 7 天的 offline 会话；不含助手的对话；下拉刷新。<br>按项目：置顶的空间 → 其他忙的空间 → 屏蔽但在跑的空间（弱化）→「空闲的空间」（默认折叠）；空间里忙的、置顶的、待唤醒的在上，空闲的收成一行，点开展开。<br>按状态：需要你 → 完成未看 → 工作中 → 待唤醒 → 已屏蔽 → 已看（「已看」默认折叠）。<br>按机器：按 hub 配置的顺序，标题带在线状态和状态计数。<br>每一段都能点标题折叠，折叠状态存在手机上 |
| 空间操作 | 空间标题右边的 ⋯：打开空间、置顶空间、屏蔽这个空间（其中的 Agent 可以单独开启通知）、重命名空间（改 herdr workspace 名，hub 把置顶 / 屏蔽一起改到新名字上） |
| 空间页 | 一个 herdr workspace 里在跑的 Agent，两列卡片按标签页顺序排；最后一张「在这个空间开新标签」，在同一个 workspace、同一个目录里起 claude（`POST /api/v1/sessions` 带 `workspace_id`）。右上角菜单多一个「全部标为已读」。从空间页打开的对话，返回时回到空间页 |
| 搜索和筛选 | 右上角搜索（标题、目录、空间名、标签页名、最后一句，不分大小写）；快捷筛选「全部 / 需要你 / 未读 / 工作中」带数量，筛出来的 Agent 看过以后也留在原位，重新点筛选才刷新（和 iOS 一样）；「全部电脑 / 某一台」只看一台电脑，记住选择；完成未看的不止一个时有「全部标为已读」。搜索和筛选时所有折叠都展开 |
| 对话 | 历史分页（`turns=2`，滚到顶自动往前翻）、实时 items（WebSocket `subscribe`，按「items 的连续性」补洞）、工具调用和结果合成一行（点开看命令 / diff / 输出，截断的点「看全文」）、图片按需取（`lazy_images`）、Markdown；发消息带 `client_msg_id`，被拒（`dialog_open` / `input_draft`）时弹窗说明，单行草稿可以「清空并发送」；中断（Esc）；Claude 给的下一条建议点一下填进输入框 |
| 交互卡片 | 提问（单选直接点、多选 / 自定义文本再提交）、计划（批准 / 批准并全部放行 / 打回带反馈）、授权（允许 / 总是允许 / 拒绝）、终端对话框（按 hub 解析出的 `option` 回答，`prompt_changed` 时提示）、密码框；都能「交给电脑处理」（`dismiss`） |
| 终端 | 对话页右上角，实时终端（`docs/terminal.md`）：`terminal_subscribe` 收 herdr 的帧，VT 子集画进格子（颜色、宽字符、框线逐格画、OSC 8 链接），断线后带 epoch / seq 续上，丢帧就从整帧重来。默认「手机尺寸」接管终端（`control`），Claude 按手机宽度排版；键盘打的字和键栏（Esc、Tab、Ctrl、方向键、^C、⇧Tab、退格、回车）直接写进终端，上下拖是 herdr 的滚轮，点一下是在那一格单击；电脑上别的程序占着时问「接管 / 只看」，被抢走后顶栏有「接管」。可切「电脑尺寸」只看（按窗格原尺寸镜像，左右滑，键走 `/keys`）。旧 hub / node 不能推流时退回每秒读一次 `/screen?format=ansi`。离开页面或 App 进后台立刻放手，窗格回到电脑上的尺寸 |
| Agent 操作 | 长按一行：置顶 / 取消置顶（置顶的只在顶部「置顶」里出现一次）、屏蔽 / 开启通知（规则和 hub 一样：Agent 自己 → `allowed` → 它的空间；屏蔽的灰点加 🔕、不弹通知）、重命名（herdr 标签页名和 Claude 会话名一起改）、关闭 Agent（先说明会退出哪台电脑上的什么、关掉哪个标签页）。置顶 / 屏蔽存在 hub（`/api/v1/pins`、`/api/v1/mutes`），点了立刻生效，hub 拒绝时退回原样 |
| 新开会话 | 列表右下角 +：选电脑、目录（家目录里列「最近用过」，点一下选中、再点进入）、启动器、权限模式（和 iOS 同一组，「默认」交给启动器）、可选的第一条消息。hub 说没启动起来但新窗格里其实起来了（停在对话框上，或者 node 不认识这个版本的输入框底栏）时，照样打开新会话 |
| Jarvis（助手） | 底部 tab「Jarvis / Agents」。Jarvis 是 hub 配置的那台机器上无头运行的 Claude（`docs/assistant.md`）：打开时进主对话（没有就进最近一段）；左上角菜单是历史对话和「新对话」，主对话带星标。新对话从首页开始：问候、5 个和 iOS 一样的建议、输入框，发出第一句就显示气泡和「正在准备」；第一段对话自动成为主对话。打开时 `POST /api/v1/assistant/warm`，第一句不用等 claude 启动。回复逐字显示（WebSocket `delta`），没有终端按钮。助手自己的提问不进 Agents 的「需要你处理」，通知标题是「Jarvis 在问你」，点开进 Jarvis tab |
| Jarvis 的资料页 | Jarvis 左上角菜单「定时任务、记忆、关于我」，和 iOS `AssistantProfileView` 对应：接下来（开着的定时任务的下次运行和跟进，最近三件）；定时任务（列表、开关、编辑名称 / 提示词 / 时间 / 星期、删除、立即运行、新建，「Jarvis 建的」有标记）；跟进（现在做、取消）；运行记录（点开对应的对话）；关于我（编辑后新对话起生效）；记忆（看、改、删） |
| 知识库 | Jarvis 左上角菜单「知识库」，和 iOS `KnowledgeView` 对应（`docs/knowledge.md`，`ui/knowledge/`、`data/Knowledge.kt`）：一排类型（项目 / 会话 / 人 / 文档 / 会议 / 日程，选中的带数量）；项目分「重点（P0 / P1）/ 其他进行中 / 已完成 / 不是项目（默认收起）」，其余按 hub 的重要级和相关性排序；右上角按标签筛选（hub 给的常用标签）；搜索框全文搜（档案、时间线、聊天记录，300 ms 防抖），结果按条目合并、命中的词加粗，点开进条目。条目页：重要级、短期 / 长期、状态、相关性、标签；在做这个项目的 Agent（点开进对话）、时间线（新的在上，6 条以后「显示全部」）、档案（Markdown，去掉和头部重复的行）、关联的会话 / 人 / 文档…（每类 4 个以后「全部」，点开推下一层）、代码目录、最近 7 天的消息、知识库目录。「问 Jarvis」把档案和最近 10 条时间线作为 `<jarvis-context>` 发进主对话（没有主对话就新开一段作为主对话）；「开 Agent」（项目）打开新开会话面板，第一条消息预填这段上下文，电脑和目录预选项目的第一个代码目录；「打标」改重要级、相关性、标签和项目的类型 / 状态（`PUT /api/v1/kb/{kind}/{id}`，手动打的 hub 不再自动覆盖） |
| 收件箱 | 第三个 tab，`docs/inbox.md` 的安卓版：飞书和邮件合成一个列表，hub 的 AI 分成需要你 / 知悉即可 / 已折叠，最后是已处理（没分拣的算需要你）。顶部筛选（全部 / 飞书 / 每个邮箱 / @我 / 私聊 / 群聊 / 邮件，只列有东西的），搜索标题、发件人、内容、摘要；左滑到底是已处理，已处理的可以移回；知悉即可「全部看过」、已折叠「全部清除」；tab 角标是需要你的件数。点开一件：摘要、hub 给的原文（不打开飞书页面）、AI 分身的草稿和依据；「AI 草拟 / AI 改」（`/draft`）、「发送」先确认再以本人身份发（`/reply`）、「交给 Jarvis」把这件事作为 `<jarvis-context>` 新开一段助手对话、「已处理」。列表在 WebSocket `inbox` 事件、重连、下拉刷新时重新拉 |
| 附件 | 对话页输入框左边的「+」：照片（系统的照片选择器，一次最多 4 张，长边缩到 1600、JPEG 0.75，和 iOS 一样，作为 `images` 随消息发出，输入框上方是缩略图、可以移除）；文件（系统的文件选择器，传到会话所在那台电脑的临时目录，`POST /api/v1/machines/{mid}/files`，超过 200 MB 不传，传完把路径插到输入框末尾、带空格或引号的用单引号包起来，**不自动发送**）。只有图片没有字时发「请看图片」 |
| 手机数据工具 | 设置 › 手机数据：设备信息、剪贴板、通知、通讯录、日历、位置，默认都关着，打开时要对应的安卓权限。打开的才通过 `register_phone` 声明给 hub，Claude 调 `phone_*` 时 hub 发 `tool_call`，手机在本地查、`tool_result` 返回（`jarvis/android/phone/`）。工具名、说明和参数和 iOS（`ios/Jarvis/PhoneTools`）完全一样（`PhoneToolsTest` 拿 iOS 源码里的定义逐个比），因为 hub 只存一份工具列表，哪台手机最后连上就用哪台的，两台不一样的话一换手机 Claude 看到的工具就变了。会改数据的（新建日程）先在手机上问：App 里一张卡片，同时一条带「允许 / 拒绝」的通知，90 秒不答算拒绝；拒绝时 Claude 拿到 `The user declined on the phone.`。还没有：提醒事项、照片、健康（iOS 有） |
| 设置 | hub 地址和连接状态、各台机器（在线、版本、`node_mismatch` 提示、重新连接）、后台通知设置入口、断开 hub |

还没做（iOS 有）：收件箱里的「交给某个 Agent」、邮件和飞书的单独页面、手机数据里的提醒事项 / 照片 / 健康、
终端的选择复制（现在只能拷贝整屏）、字号和主题、电脑尺寸下按手机宽度折行、往上翻 `/screen/before` 的历史，手表。协议都在，后续按 iOS 的做法补。

hub 拒绝这台手机的 token（401：在别的设备上撤销了、换了 hub）时不再无限重连，直接回到配对页并说明原因；已配对时打开配对链接也会回到配对页重新配。

## 通知（不用推送服务）

国内卖的安卓手机大多没有 Google 服务（FCM 收不到），各家厂商推送又要开发者账号和实名认证，所以第一版不接推送：

- App 开一个前台服务（`HubService`，类型 `specialUse`）一直连着 hub 的 WebSocket，通知栏常驻一条「已连接 hub / N 件事需要你」（最低优先级，不响）。
  网络变了马上重连，断线指数退避到 30 秒；开机后自动启动（`BootReceiver`）。
- 收到 `interaction` 就弹「需要你处理」通知（高优先级），能直接在通知上点：授权「允许 / 拒绝」、计划「批准 / 打回（可输入反馈）」、
  单选提问的前 3 个选项、终端对话框的前 3 个选项。`interaction_resolved` 时收回通知。
- 一轮做完（工作中 → 完成未看）且 App 不在前台时弹「做完了」。对话页只在 App 真的在前台时才上报已看（`/seen`），
  否则放在后台的对话页会把「做完了」吞掉。
- 规则对照 hub 给 iPhone 的推送（`docs/server.md`「推送策略」），但时机在手机上算：需要你立即弹；做完了不等 `done_delay_seconds`。
- 代价是耗电和被系统清理。小米 / 红米（MIUI、HyperOS）等要在系统设置里把这个 App 的省电策略设成「无限制」并打开「自启动」，
  设置页有两个入口按钮（电池优化白名单、MIUI 的自启动管理页，找不到时退回应用详情页）。以后要接厂商推送时，hub 侧加一个和 APNs 并列的发送器即可。

## 构建和安装

```sh
./scripts/android.sh          # 编译 debug 包、装到连着的手机 / 模拟器并打开
./scripts/android.sh apk      # 只出 APK：android/app/build/outputs/apk/debug/app-debug.apk
./scripts/android.sh test     # 单元测试（模型解码、行合并、Markdown）
```

- 需要 Android SDK（`$ANDROID_HOME`，默认 `~/Library/Android/sdk`）和 JDK 17；没有 JDK 17 时脚本用 Android Studio 自带的。
- Application ID 走配置：`~/.config/jarvis/config.json` 的 `android.application_id`，没写就用 `app.bundle_id`，再没有就是 `com.example.jarvis`。
- hub 默认是 HTTP（内网 / VPN），`network_security.xml` 放开了明文流量；不要把 hub 暴露到公网。
- 装到真机：手机开 USB 调试连上电脑跑 `./scripts/android.sh`，或者把 APK 发到手机上直接装。

## 代码

| 文件 | 内容 |
| --- | --- |
| `data/Models.kt` | 线上类型（字段全可选，未知字段忽略，Go 的零时间当空） |
| `data/ApiClient.kt` | REST；错误统一成 `HubException(status, message, code, fields)` |
| `data/Hub.kt` | WebSocket、重连、machines / sessions / interactions 的状态和事件流 |
| `data/ChatStore.kt` | 一个会话的 items：`covered`、`history`、补洞（`after_seq`）、往前翻页 |
| `service/HubService.kt`、`Notifier.kt`、`ActionReceiver.kt` | 前台服务、通知、通知上的按钮 |
| `terminal/` | 实时终端：`TerminalGrid`（VT 子集，移植自 iOS 的 `TerminalGrid.swift`）、`TerminalFeed`（帧按序应用、接管时自己攒滚出屏幕的行）、`TerminalLink`（订阅 / 续传 / 接管 / 轮询降级）、`TerminalKeys`（键名 → 终端字节）、`BoxDrawing`、`TerminalScreen` |
| `ui/` | Compose 页面：`AgentsScreen`、`ChatScreen`、`InteractionCard`、`PairScreen`、`SettingsScreen`、`Markdown` |
| `data/Knowledge.kt`、`ui/knowledge/` | 知识库：线上类型、项目分组、搜索结果合并、时间线解析、「问 Jarvis / 开 Agent」带的上下文（`KnowledgeTest`）；列表页和条目页、打标 |

## 验证记录

模拟器（Android 14）连真实 hub，一台 Linux 跑 hub、一台 Mac 经 SSH 接入：

- 扫码链接配对、换设备 token；两台机器在线，列表显示真实会话
- 手机发消息到 Mac 上 herdr 里的 claude；claude 的 AskUserQuestion 变成卡片（hook 拦下的走 `question`，TUI 里的走 `screen`），在手机上选择后 claude 收到
- 计划模式下 ExitPlanMode 的对话框在手机上批准，claude 继续执行改了文件
- App 在后台：提问通知上直接点选项，claude 收到；一轮做完弹「做完了」
- 实时终端：打开 Mac 上 Claude 的窗格，接管成 46×39（herdr `session control`），打字、退格、^C 写进终端，上下拖滚 Claude 的对话；
  电脑上另一个程序占着时弹「接管 / 只看」；切「电脑尺寸」改成按 120 列镜像；关掉页面后 control 进程退出、窗格回到电脑尺寸
- hub 重启后几秒内自动重连；长会话往前翻到两天前的内容；深色模式截图检查

真机（Redmi K60 Pro，Android 16，澎湃 OS 3，没有 Google 服务），手机经 USB（`adb reverse tcp:18660 tcp:18660`）连 hub：

- 配对、列表、对话页正常；claude 的提问在手机上回答，电脑上的 claude 收到
- 「需要你处理」的通知系统里有（channel `input`，重要级别最高），但默认不在通知栏显示：澎湃 OS 给新装的 App 关掉了悬浮 / 锁屏通知，
  要在「设置 › 通知与控制中心 › 通知管理 › Jarvis」里打开。锁屏后长时间的后台连接还没在真机上测

## 经验

- `adb shell input swipe` 从屏幕最上方往下滑会拉出系统的快捷设置，测试翻历史时从屏幕中间开始滑。
- herdr server 继承启动它的 shell 的环境：在带代理变量的 shell 里起 herdr，里面的 claude 也会走那个代理。联调时用干净的登录 shell 起 herdr。
- Claude Code 设置里放开了的工具（allow 规则、bypass 模式）不会触发 PermissionRequest，测授权卡片要在没放开的会话里。
- hub 只存一份 phone_* 工具列表（`setPhoneTools`），每台手机 `register_phone` 时整个替换，调用发给最后注册的那台（`phoneClient`）。
  所以安卓声明的工具要和 iOS 一模一样，不能多也不能改参数。安卓 10 以后只有在前台的 App 能读剪贴板，后台调 `phone_clipboard_get` 读到的是空，结果里会说明。
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
- 手机没法直接访问 hub 时（hub 在内网、手机在流量上），联调可以走 USB：电脑上把本机 18660 转到 hub，再 `adb reverse tcp:18660 tcp:18660`，
  手机配对时用 `http://127.0.0.1:18660`。
- 终端里打字：安卓输入法对每个英文词也走 composition（为了联想），等 commit 再发的话一个词要到空格才出现；所以输入框里的文字（含正在拼的）
  每次变化只发和上次的差（新字，或若干个退格）。按这个规则，拼音选字时终端里先出现拼音，选好后退格换成汉字（模拟器没有中文输入法，还没在真机上试）。安卓的 emoji 字体会把 ⏺ ⏸ ⏵ 画成两格宽的彩色图标，
  加 U+FE0E 也没用，这几个按形状自己画。
- hub 前面的转发断过以后，hub 那边可能还以为 node 在线，但每个调用都回 `rpc: connection closed`（/screen 也一样）。重启常驻 node
  （`launchctl kickstart -k gui/$UID/com.jarvis.node`）再让 hub 重连这台机器就好了，App 这边不用动。
