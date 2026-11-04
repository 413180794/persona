# 经验：飞书与邮件

踩过的坑、怪异行为和当时的取舍（原来在 `CLAUDE.md`「经验」，索引见 `docs/lessons/README.md`）。新经验写进对应的节，没有合适的就新开一节。

## 附件发送

- 不要把文字/图片的 client_id 幂等性推断到文件：lark-connector 的直接文件上传不进入 outbox，未知结果不能重新上传。API 返回成功却丢失或无法解析回执也属于未知；只有明确拒绝才允许重试。测试 mock 必须区分 429 拒绝与已接受后回执丢失，不能一律返回 502 再假设重试安全。
- 强退恢复时，草稿清空与待发送记录必须在同一个原子快照里；媒体先复制，再提交引用。保存失败不能清空输入或开始网络请求。回执已收到但落盘失败，不得把已经发送的消息改成“发送失败”。

- 附件乐观回显不能沿用纯文本的「文字相同 + 20 秒超时删除」：图片文字可能为空，同名文件也不是同一条。
  保留 client_id 与 message_id，确认回显后替换；lark-connector 的 local: 暂存消息不能当成发送确认。请求超时以后到达的确认推送仍要合并，不能一直留着失败气泡。
- 原生发送队列不能串行等待消息回显的轮询延迟；只串行网络提交，回显同步独立进行。文件选择后持有私有副本直到移除或回显确认，重试不再读取可能变化的原始文件。

- iOS 27 的 Quick Look 先显示关闭按钮，再加载文件内容；初始占位阶段的点击可能没有关闭预览。UI 回归要等待文件内容工具栏出现，并断言预览确已消失，再操作聊天输入区，避免点到预览的共享按钮。

- 验证新外壳收件箱的返回路径，要从实际列表点进聊天。`-openInboxItem` 会直接重建路径为聊天，省略收件箱父页面；
  拿这个截图快捷参数测「返回收件箱再重试」会误判。附件回归从真实收件箱行进入，再验证退出和恢复。

- 附件的本机行用 UUID，服务器回显用消息 ID。发送后若只定位本机行，回显替换会丢失滚动目标；
  对刚发送的消息跟随底边，确认替换后再次对齐。用户开始拖动就停止跟随，避免把阅读历史的人拉回底部。

### 收件箱摘要与附件回显的滚动位置

收件箱聊天上方的摘要改变了 ScrollView 的 top inset。用 `contentOffset + containerSize` 判断到底会漏算这部分：图片已经完整在输入框上方，仍显示「回到最新」。使用 `visibleRect.maxY` 和 `contentSize.height` 比较。附件的本机 UUID 在确认回显后消失，发送时跟随底部边缘；确认替换后重新定位，用户开始滚动就停止跟随。UI 回归需断言已确认图片后没有回到底部按钮，手动上翻后按钮重新出现，不能只断言「发送中」消失。

发送行上不要给整个 `LarkMessageRow` 加同一个 accessibilityIdentifier：SwiftUI 会把它传播给子节点，覆盖气泡旁发送 / 失败标记自己的 identifier。附件确认回显断言本机预览按钮消失，发送状态继续使用主线的 `lark.outgoing.sending` / `lark.outgoing.failed`。

主线空输入框隐藏发送按钮时，附件草稿也算可发送内容：仅照片 / 文件仍应有按钮，移除最后一个附件后隐藏；准备附件时禁用。原有文字、引用、话题输入框和已读标记保留。

## 消息与文档

- 飞书消息渲染照搬 lark-connector（`docs/ios.md`「飞书消息渲染」）：hub 原样转发 `parts`，只把媒体 URL 改写到 `/api/v1/lark/media/`；
  看 lark-connector 源码直接 `git clone https://github.com/Eric-2369/lark-connector`（hub 机器上只有部署产物）。
- 个人 Agent 的来源先看 hub 的 `GET /api/v1/agents`：未配置 lark-connector 或仅配置飞书 CLI 时返回 `{"sources":[]}`，会话接口返回 `{"sessions":[]}`，均为 HTTP 200；飞书 CLI 不提供个人 Agent bridge。
  Bridge 只在 `lark-connectorConfigured()` 为真时注册；无条件注册会使未配置时的列表请求因「lark-connector is not configured」返回 502。配置了 lark-connector 后，其 agent-bridge 默认仍是关的：要在部署里设置 `lark-connector_AGENT_BRIDGE=true` 并重启，否则 `/agent-bridge` 返回 `enabled:false`，
  会话为空；更早的 lark-connector 版本没有这个接口，hub 拿到的是 404。开了以后还要在 lark-connector 的设置里添加 Cue / Grok Bot 账号（只有主人登录的网页接口能加，
  Agent API 只读）。来源已注册时，`setup` 是 `missing` / `disabled` / `no_accounts` 就是没配好，`lastError` 是账号同步失败。
  lark-connector 自动更新后检查部署配置里这个开关还在不在。
- lark-connector Agent API 的资源地址使用 `/agent/v1/media/`、`/agent/v1/messages/`；hub 改写要同时兼容这些前缀和旧的 `/api/` 地址，保留查询参数。
  只检查 HTTP 200 不够：未改写的图片路径会落入 hub 网页并返回 HTML，手机还会在解码前把它缓存。验证消息返回的新 URL、图片 MIME 和实际字节；改写为正确代理地址也会改变手机缓存键。
- lark-connector 会话列表的坑（`docs/protocol.md` 飞书 API）：`isDone` 字段不可靠（done scope 里全是 false），已完成以 scope 为准；
  置顶看 `onTopRankAt`（`settings.isShortcut` 也不准），且置顶会话分散在 inbox 和 done 两个 scope；未知 scope 会被静默当成 inbox。
- 打开飞书会话不能用 `tail=1`（那是「回到最新」）：有未读时要 `aroundPosition=readPosition+1`，已读只上报真正上屏的消息。
- 话题群（`chatMode: "topic"`）的 `GET /conversations/F/messages` 多半只有 position 0 的「创建了话题群」系统消息、`hasOlder=false`（实测 5 个话题群里 4 个如此，tail、around、before 都一样），内容在 `…/threads`（话题根消息，`nextOlderCursor` 往前翻），回复在 `GET /messages/ROOT/thread`。不能指望调用方带对 `topic`（人的名片进来、旧的收件箱条目、网页都没带）：hub 用 `lark.ChatPage` 自己按 chatMode 判断（会话列表里记下，没见过的查一次 `GET /conversations/F`），返回带 `topic: true`。
- 有的网络（作者的 hub 机器）连不上 Google 的多数 IPv4 地址（`imap.gmail.com` / `smtp.gmail.com` 常解析到 173.194.x.x，所有端口超时），HTTPS 能通只是走了 IPv6。
  hub 访问 Gmail 一律走 `hub.json` 的 `google.proxy`（比如 hub 机器上的本地代理），别再加直连兜底（`docs/mail.md`）。
- 客户端和 hub 的 JSON 形状对不上，Go 的解码器直接 400，模拟器上不点那条路径就看不出来：iOS 回复邮件时把 `references` 发成数组，
  hub 的 `SendRequest` 只收字符串，手动回复会被 400 拒掉（#587，读代码发现）。改请求体的字段类型时 hub 两种都收（`mail.MessageIDs`），
  并加一个按各端真实形状 POST 的 HTTP 测试（`TestHTTPSendReferences`）。
- IMAP 扩展要先看服务端声明的能力再用：QQ 邮箱不声明 LIST-EXTENDED，收到带 `RETURN` 的 `LIST` 直接不回，只能等超时（`docs/mail.md`「注意事项」）。
- 服务端回「搜索成功、0 条」不代表真的没有：163 对任何关键词 `SEARCH`（`TEXT`、`BODY`、`SUBJECT`、`FROM`，中英文、加不加 `CHARSET UTF-8`）都回 0 条，只有 `ALL`、日期这类条件生效（2026-11-03 实测）。所以关键词搜索回 0 条时要在本地匹配邮件头（`docs/mail.md`「搜索」）；测试里用 `blindSearch` 模拟这种服务端。
- 163 取信也慢，越旧越慢、波动大：每页 200 封摘要，最新一页 4 秒，往后 6～20 秒不等，第 1000 封附近那页超过 20 秒（2026-11-03 实测）。一次 IMAP 操作只有 20 秒（`withConn`），所以找邮件头、取摘要都要分小批，时间从这次操作开始就算，建连接也在内（#858）；一批 500 封、时间从扫描开始才算时会超时。
- 飞书文档 / 表格 / 多维表格里 @我、评论我，不进 IM 会话列表（没有「云文档助手」会话），只在 lark-connector 的文档通知中心
  `documents/notifications`，而且卡片只说「谁提及了你」，不说哪一行：要自己读表按「@名字」找（`docs/inbox.md`「云文档」）。
  表格里带下拉的格子 `sheet/edit` 一律报 `DOCUMENT_EDIT_UNSUPPORTED`，改值要用 `sheet/structure` 的 `choose`；多维表格 `base/edit` 的
  `expect` 指纹要按当前值算（`BaseCell.Fingerprint`），人员字段的 id 格式和 `accounts` 给的 userId 不一样（写不进去，读得出）。
  测试只拿自己建的 demo 文档（链接放环境变量），别动用户给的真表。

- 新会话入口要让 mock / 体验模式也走真实模型解码：会话 fixture 曾遗漏服务端必回的 `is_mine`，收件箱自己组装会话所以没暴露。
  补齐 fixture，并验证体验模式返回的会话都能打开消息；不为测试放松真实协议。

## OLK 展示与本机缓存

Agent 返回的普通文字不能用于猜测是否加密。新增的 `olk` 投影要贯穿 Hub、消息及引用模型：正文、公开外显和密钥分发是三个不同的展示部分。旧 `parts` 继续保留给旧客户端兼容，原生 UI 使用投影，避免把公开表情和分发提示拼入正文。

禁止只在主消息缓存处删除解密正文：引用也会进入草稿／发件箱。消息和引用的 Encodable 实现同时清除 legacy 文本、parts 与投影正文，持久化仅保留公开内容。默认 MainActor 隔离工程中，nonisolated 模型在另一文件的 extension 内实现 encode 时仍需显式 nonisolated，否则无法安全满足 Encodable。

审查补充：可选投影不是整页消息的必填条件。Hub 将缺失 / null 的公开内容规范成空数组，App 对未知版本、缺失状态及不完整字段安全降级，不读取旧压平正文。离线外显必须标注为公开内容，避免被当作原文。上述编码保护仅覆盖 iOS 消息 / 引用快照；Hub 收件箱和 Agent 上下文仍有独立的压平持久化路径，不应概括为全链路无明文存储。

- 加密消息的外显标记必须覆盖气泡、聊天引用和输入框引用三条路径。只在气泡里加标记，恢复引用草稿后仍会把公开伪装内容误当作原文；统一使用引用的展示文本，并分别验证锁定、缓存和解密状态。

- 原生加密发送先准备密文，再进入现有持久化队列。重试若重新调用 encrypt 会生成不同信封，已有的 client ID 幂等性就失效；多条密钥分发也要稳定派生 ID。作者自己的加密草稿、乐观回显正文与密文同属一份 AES-GCM 快照，不能只清理收到的消息缓存。钥匙串暂不可用应阻止覆盖，不当作空草稿。
- 消息与收件箱共用草稿：只把模式切换放进消息页还不够，收件箱恢复加密草稿时必须防止降级普通发送。跨页面、强退恢复和普通草稿的老格式要一起回归。新外壳保留隐藏页面，scenePhase 变化时隐藏聊天仍可能写回旧输入；只允许当前页保存，在可见性离开边界刷新一次，重新出现时从共用发件箱恢复。

## 飞书开放平台机器人（`docs/bots.md`）

- 卡片 JSON 2.0 **多一个不认识的字段整张卡片被拒**（1.0 是忽略）：2.0 没有 `action` 模块和 `note`（按钮横排用 `column_set` + `flex_mode: flow`，
  小字用 `div` + `text_size: notation`），按钮的回传要写在 `behaviors` 里；卡片回调里返回的新卡片也必须是 2.0，否则报 200830。写之前对着 2.0 的组件文档逐个字段核对。
- 1.0 的 Markdown 不支持标题、表格、引用，模型的回答直接塞进去会显示成原始符号；要用 2.0。卡片会把 `<at>`、`<font>` 这类当成自己的标签：正文里的 `<` 先转义。
- 卡片的链接按钮文档只写了 http(s) 和 AppLink，自定义 scheme（`jarvis://`）不要放，免得整张卡片被拒。
- 长连接的事件要在 3 秒内回执，不然飞书重发同一事件：处理放后台，按 message_id 去重。
- 一条消息发给正在忙的对话会排在当前这一轮后面：那一轮的结束不是它的回答，要在 transcript 里找到这条消息再取它后面的回答（`botRun.Probe`）。

- lark-connector 文件发送的 201 回执可能只有 `fileKey/name/size`，`messageId` 是可选字段，不能因此把已发送文件报成失败并鼓励重发。图片 JSON 的 base64 斜杠也可能被全部转义，HTTP body 上限要覆盖这个合法编码，实际图片大小仍按解码后的 7.5 MiB 限制。
- 未发送附件是用户数据，选取后的私有副本放 Application Support 并排除备份；不能放在「清除缓存」会删除的 tmp/uploads 或 tmp/lark-drafts。

## 全部会话的分页刷新（#530 审查）

- 刷新只替换第一页而分页总让旧值优先，会让尾页未读数永久过期；按已加载页数重新读取窗口，成功后原子替换，游标停在窗口末尾。失败保持旧快照，不落半批缓存。
- 页尾的加载请求遇到刷新不能直接丢弃：游标不变时 SwiftUI 不会重启 task。等待刷新结束再用新游标继续；排队刷新仍在页面所属任务内，避免离开后另起后台任务。
- 会话列表不等于消息订阅名单；列出全部群不应让所有活跃群都持续预取聊天内容。

- 持久化发送队列要单独记录是否已尝试提交。重启时只有已开始提交、结果未知的文件需要检查；排队中从未发出的文件应保留显式重试。离线、DNS 或建连失败也不能误判为已上传但未知。
- lark-connector 成功文件回执允许省略消息 ID；客户端和 Hub 都必须承认成功，不能在客户端再次把可选字段变为发送失败。

- Application Support 的选取临时副本不会被系统自动清理；进程重启后先清理旧 UUID 目录，再开始新的选取。清理只运行一次，与选取共用锁，不能删除当前进程新建的副本或独立的持久化媒体目录。
- 流式上传的超时包含手机上传时间，应按文件大小给手机和 Hub 相同预算；lark-connector 的 pending / failed 本地消息即使已从本机发件箱删除，也不能变成普通已发送气泡。

## 风灵月影与账号设置

整份 local settings PUT 会覆盖网页或另一客户端刚改的字段。手机只通过受限 PATCH 修改两个展示开关，并携带旧值；未知结果先重读，不能自动重试。Hub 返回实际选中账号的用户 ID，不能用账号列表里另一个 primary 用户代替。前后身份校验可以拒绝换号响应，但不是跨登录操作的数据库事务。

防撤回的原内容就在 lark-connector 的 parts 中，必须另看 hasRetainedRecallContent，不能把系统撤回提示误当原文。展示与权限分离：保留 recalled 标记；未开启时正文、引用和转发都走撤回提示。编辑版本有自己的 OLK 投影，不能只清主消息和引用的磁盘明文而漏掉 editHistory。

合并转发卡片的 metadata 同时包含 messages 和复制出来的 previews；不能只处理展开后的正文。预览按原记录的撤回状态/OLK 投影生成；落盘递归去除记录及历史的解密正文，再重建预览，避免无 OLK 标记的复制文本漏进缓存。编辑时间可能包含毫秒，沿用 JSON.parseDate，不能仅用默认 ISO8601DateFormatter。

设置变更推送可能早于保存请求的 HTTP 回执，也可能在旧账号的读取尚未结束时到达。不能因为 `loading` / `saving` 就丢掉刷新：合并期间的通知，在当前请求结束后用最近一次客户端补读，不发布已被通知取代的旧响应。补读由设置模型持有，离开先前页面或取消该页面的等待不能同时取消新的实时变更；写操作仍只提交一次。

Hub 在 400ms 内合并事件时，设置变化必须独立累积为 `settings_changed`，不能依赖空 `feed_ids` 表示；同一批有新消息时仍须重读设置。未登录错误沿用 `lark_not_logged_in`，指向 Hub 所在机器的 lark-connector 登录入口，不能建议无效的反复重读。

这两个开关控制展示，沿用 lark-connector 的语义，不是留存或访问授权：关闭后并不删除服务端留存，Hub 仍向已有消息读取权限的客户端返回原内容与版本，普通未加密内容仍可能进入本地缓存。OLK 解密正文与此独立，持久化必须清理。现有收件箱分拣及助手引用仍可能使用已撤回的 `Text`；其 AI 上下文策略在 #882 后续单独统一，本阶段不宣称已改变。

## 消息列表行间距

SwiftUI List 默认行 inset 会叠加内容自己的 padding。#957 在 iOS 27 实测：46 pt 头像 + 上下各 8 pt 内容留白，最终行高却是 92 pt；显式把行上下 inset 设为 0 并用现有 48 pt 头像 token 后，普通行是 64 pt。不要用固定高度掩盖重复留白，动态字体应允许增高。仅在消息列表的 NavigationLink 上设定，避免改变共用收件箱。

- When merging message-display controls with interactive quotes, keep the shared `QuoteCard` and `larkJump` action. Apply recall visibility to the card summary before rendering; UI tests should query the quote button, not the former static text.

## 采集读消息时条目被标处理（#1105）

- `applyLark` 先在锁里找到这个会话打开的那件，放开锁去读消息，再上锁写进去。中间用户把这件标成已处理（左滑、全部标为已处理），
  以前新消息照样写进那件已处理的条目，`done_at` 不变，于是谁都看不到。现在重新上锁后先看这件还开着没有，已处理就把它当成「上一件」，新消息另开一件。
  别的来源（邮件、文档、removed、连接器）的合并都在一把锁里做完，没有这个空档；新加采集时也别在锁外读完再往锁外拿到的条目里写。
- 批量已处理带列表里的 `version`，在锁里比：用户看到的那份之后又并进来新消息的那件不标。
