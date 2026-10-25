# iOS / watchOS App

XcodeGen 工程 `ios/project.yml`（`Jarvis.xcodeproj` 不入库）。iOS 26 / watchOS 26，Swift 6，默认 MainActor 隔离。依赖 MarkdownUI。
Bundle ID `com.chirpet.jarvis`、`com.chirpet.jarvis.watchkitapp`。界面中文。

## 信息架构

| Tab | 内容 |
| --- | --- |
| 对话 | **Jarvis 助手**：example-machine 上 `~/.jarvis/assistant` 里**无头运行**的 Claude（`claude -p` + stream-json，不进 herdr；逐字流式；
右上角随时切换启动器 / 模型，下一条消息起用 `--resume` 换过去；首页模型标签决定新对话用哪个，默认在设置里改），CLAUDE.md 由 hub 生成，告诉它怎么访问各台电脑（ssh）、飞书等公司数据（lark-* / provider-* skills）、iPhone（phone_*）和邮件（mail_*）。首页是问候 + 可用设备 + 6 个建议；发出第一句立即显示气泡和「正在准备」，后台起会话。对话里隐藏思考，工具调用折成一行「用了 N 个工具 · 电脑、iPhone…」。左上角历史对话，右上角新对话；首次启动自动接上最近一段 |
| 开发 | 原来的会话列表（不含助手对话）：「需要你处理」卡片 → 进行中 → 刚完成 → 空闲（折叠）→ 最近；搜索、按机器筛选、左滑中断、右滑关闭 |
| 信息 | 飞书会话（经 example-machine 上的 lark-connector agent 接口，`/api/v1/lark/*`）和邮件按时间合并；顶部 全部 / 飞书 / 邮件，左上角只看未读；
飞书会话照 lark-connector 的渲染器还原：富文本、图片、贴纸、表情、文件、视频 / 语音、代码、卡片（含告警卡的列、表格、折叠面板、按钮回调）、合并转发、群名片、回复引用、表情回应（详见下文「飞书消息渲染」）；加载更早、直接回复、打开即标已读、「让 Jarvis 总结」交给助手。邮箱管理和写邮件在右上角菜单。添加邮箱支持「AI 一键添加」：粘贴一段邮箱信息（或从剪贴板识别），hub 用 cc_ds 的 API 识别，齐全就直接连接添加，缺什么就填进表单；合并收件箱 / 单账号，详情（纯文本或 HTML）、回复、写信、「让 Claude 处理」（带上 mail_read 参数新建对话），邮箱管理 |
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
- 展示逻辑（助手和开发统一，`ChatBlock.build`）：按轮次分组。一轮 = 一条用户消息 + 这一轮里 Claude 工作时吸收的排队消息
  （transcript 的 `queued_command`，`Item.queued`）。已完成的轮次显示成：用户消息 1、2、3…连续排在最前 → 一行折叠的
  「过程 · 用了 N 个工具 · 思考 M 段 · 中间回复 K 条」（点开看全部步骤）→ 最后一条回复。轮次中途的上下文压缩 / API 错误归入过程，
  「已中断」跟在轮次末尾；recap 单独成行。正在进行的那一轮（agent 工作中的最后一轮）按到达顺序完整显示每一步。
- 排队消息：Claude 工作中发的消息不进对话记录，显示在输入框上方的「排队中」列表；每条可「撤回编辑」（回到输入框）、拷贝、删除。
  实现照 TUI：↑ 把全部排队消息拉回输入框（transcript 记 popAll），清空输入框后把保留的重新排队（`POST …/queue {"keep":[…]}`）。
- 开发列表：忙碌的空间里只显示需要你 / 工作中 / 完成未看的 agent，空闲的折成一行「还有 N 个空闲」，点开展开。
- 「回到底部」箭头：用列表末尾的哨兵视图判断是否在底部（滚到底就隐藏）。
- 终端：全屏；按键栏只有 ctrl（粘滞，作用于下一个键）、esc、tab 和一个游戏手柄图标，点开浮出方向键 + 回车十字键盘；终端交互面板用同一套按键栏。
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
- 手表配对：iPhone 在 WCSession 激活后再推一次配对（激活前推会被丢掉），同时 transferUserInfo 保证送达；手表未配对页有「从 iPhone 同步」
  （sendMessage 请求，iPhone 回复配对）；iPhone 设置里有「同步到 Apple Watch」。
- 手表：iPhone 经 WatchConnectivity 把配对信息和摘要（运行中会话、待处理交互）同步给手表；手表打开时每 8 秒直接请求 hub 刷新，
  回答 / 发消息 / 中断也直接调 hub。

## DEBUG 启动参数（仅 Debug，Release 检查不含这些字符串）

`-pairURL jarvis://pair?...`（免扫码配对、不弹通知授权）、`-openSession <machine>/<session>`、
`-showNewChat YES`、`-tab dev|mail|settings`、`-openAssistant <machine>/<session>`、`-newAssistant YES`、`-openFirstImage YES`（打开会话里第一张图的查看器）、`-openTerminal YES`、`-showPad YES`、`-lark-connector <feedId>`（直接打开飞书会话，`-lark-connectorName` 标题，`-lark-connectorTopic YES` 话题群）、`-larkFocus <messageId>`（滚到该条消息）、`-larkTop YES`（飞书会话从最早一条开始显示，截长卡片用）。`scripts/sim.sh` 封装了编译、安装、配对、启动（`SIM=<udid>` 换模拟器；多个 agent 同时开发时各用各的模拟器，否则会互相覆盖安装）。
检查历史渲染：`-loadAll YES`（加载全部历史并滚到顶）、`-fromSeq <seq>`（从包含这条用户消息的那一轮开始显示，长历史里滚动定位会漂）、
`-dropUsers YES`（隐藏那一轮的用户气泡，一屏看全过程行和答复）。

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
