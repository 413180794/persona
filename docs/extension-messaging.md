# 扩展点 `messaging`：消息来源（#2138）

App 的「消息」入口（会话列表、聊天、发送、置顶、话题、表情回应、已读……）由扩展提供：App 里只有一套**通用的原生消息组件**，
**数据和动作**来自贡献 `messaging` 的扩展。第一个来源是飞书（lark-connector 扩展），以后别的 IM（Telegram……）用同一份协议。

为什么不是网页 / 下发代码：iOS 在运行时强制代码签名（App Store、TestFlight、ad-hoc 都一样），原生代码不能从扩展下载执行；网页 / JavaScript 虽然能下发，
但聊天这种重交互界面体验差，而且扩展代码会跑在手机里、碰得到 App 的凭据，和「扩展不在 App 里跑代码」（`docs/extensions.md`「声明式 UI」）冲突。
所以和声明式 UI 同一个思路：**扩展只给数据和动作，App 用自己的设计系统画**，只是这里的「组件」是消息级的（会话、消息、片段、输入框模式）。

本文是协议规格；数据格式以 `schema/extension/messaging.schema.json` 为准（四端类型从它生成，见 `schema/extension/README.md`）。
计划和分阶段见 #2138；飞书在这份协议上的实现见 persona-removed#45。

## 一、总览

| 谁 | 做什么 |
| --- | --- |
| 扩展 | 清单贡献 `messaging`（一个来源一项）；在自己的 HTTP 一侧实现下面的接口；数据变了发 `messaging.changed`；厂商特有的处理（解码、加解密、卡片转换、显示设置）全在扩展里 |
| hub | 列出来源；把 `/api/v1/messaging/{source}/…` 鉴权后转给扩展；按 schema 查扩展的回答，不合规的不给 App；写操作套发送回执；把事件推给在线的 App |
| App | 有来源就显示「消息」入口（名字、图标来自来源）；会话列表、聊天、输入框、发件箱、草稿、缓存、离线、通知都是通用的；按来源的 `capabilities` 决定显示哪些操作 |

**不在这份协议里**的：设置类页面用现有的声明式 UI（`ui` 的 `extension_page`，来源的 `settings_ui` 指过去）；收件箱条目照旧是连接器的（条目上带会话的定位，见「九」）；
助手的工具、知识库归档照旧是连接器 / `assistant` 的。

## 二、清单

``````

| 字段 | 说明 |
| --- | --- |
| `id` | 来源 id，`^[a-z][a-z0-9_]{0,31}$`，全局唯一（和别的扩展冲突就装不上）。也是接口路径里的 `{source}`、事件里的 `source`、App 缓存和草稿的命名空间 |
| `name` | 入口的名字（底栏、外壳、设置里），1–12 字 |
| `icon` | 固定图标集里的一个（`docs/extensions.md`「UI 版本 3」的图标集），不认识的画 `chat` |
| `capabilities` | 这个来源支持什么（见「三」）；App 只显示支持了的操作。hub 和 App 都不拒绝不认识的（更新的扩展），App 忽略它 |
| `composer_modes` | 输入框除了「普通」以外的模式（见「七」）；最多 6 个 |
| `settings_ui` | 同一个扩展的一个 `ui` 贡献（`extension_page`）的 id：消息页右上角「设置」和设置 › 连接器里这个来源的入口打开它。页面不带账号：有几个账号的来源在一页里按账号分节 |
| `links` | App 里点到这些主机的链接时交给这个来源打开：`opens` 是 `document`（文档阅读，要 `documents`）、`post`（帖子，要 `posts`）或 `ui:<id>`（同一个扩展的整页 UI，`context.item.url` 是那个链接）；`paths?` 可以再限定路径前缀。**按列表顺序第一个对上的为准**（具体的写在前面）。`document` 和 `post` 把整个链接交给扩展：`GET documents?url=<链接>`、`GET posts/<转义后的链接>`（见「十」） |

要求：`runtime.http`（接口走扩展的 HTTP 一侧，和 `api` 一样）、`activation_events` 有 `on_startup`（要实时推送）。
一个扩展可以贡献几个来源（比如工作账号、个人账号是两个系统）；v1 的 App 只显示第一个启用的来源，多来源的入口以后做。

## 三、能力（`capabilities`）

| 能力 | 有了才有的 |
| --- | --- |
| `accounts` | 一个来源下几个账号：`GET accounts`，各接口带 `account`，列表顶上的账号切换 |
| `pinned` | 置顶会话：`GET pinned`，列表顶上的置顶栏 |
| `mentions` | 输入框里 @人：`GET conversations/{id}/members` |
| `attachments` | 发图片、文件 |
| `attachment_replies` | 图片、文件也能引用一条消息、发进话题 |
| `threads` | 消息的话题（回复进话题、话题详情）：`GET messages/{id}/thread` |
| `topics` | 话题群（一个话题一张卡片） |
| `reactions` | 表情回应：`GET emojis`、`GET reactions/quick`、`POST messages/{id}/reactions` |
| `read_receipts` | 我发的消息的已读人数：`POST conversations/{id}/read-states` |
| `chat_pins` | 聊天内 Pin：`GET / POST conversations/{id}/pins` |
| `profiles` | 点头像看人：`GET users/{id}`，「发消息」`POST users/{id}/chat` |
| `url_previews` | 链接预览：`GET messages/{id}/url-preview` |
| `documents` | 文档阅读：`GET documents`（见「十」） |
| `doc_comments` | 文档评论（要 `documents`） |
| `posts` | 帖子（公司圈这类）：`GET posts/{id}`、`GET posts/{id}/comments` |
| `edit_history` | 消息带 `edit_history` 时显示「已编辑」入口 |
| `translation` | 这个来源的会话可以在设置 › 翻译里选，App 用 hub 的 `/api/v1/translate` 翻译 |

## 四、接口

App 调 hub：`/api/v1/messaging` 和 `/api/v1/messaging/{source}/…`。hub 鉴权（只读设备照旧只能读）后把后者原样转给扩展的 HTTP 一侧，路径不变，
头和 `api` 一样（去掉凭据，带 `X-Persona-Device`、`X-Persona-Role`）。扩展没启用、来源不存在回 404 `not_found`。
有 `accounts` 的来源，所有接口都接受 query `account`（不透明字符串，来自 `GET accounts`），不带就是来源的默认账号。

| 方法、路径（`{source}` 之后） | 请求 | 回答 |
| --- | --- | --- |
| `GET /api/v1/messaging` | — | `{sources: [Source]}`：启用的来源，`Source` 是清单那一项加上 `extension`（扩展 id） |
| `GET status` | — | `{configured, connected, signed_in, name?, error?}`：设置页和「没连上」的提示 |
| `GET me` | — | `{account, user_id, name?}`：这个账号现在是谁（发件箱、草稿按它分；见「八」） |
| `GET accounts` | — | `{accounts: [{id, name?, avatar?, tenant?, signed_in, unread?}], default}` |
| `GET conversations` | `cursor?`、`limit?`（≤ 100） | `ConversationPage {account, conversations: [Conversation], next_cursor?}` |
| `GET pinned` | — | `{account, conversations: [Conversation]}` |
| `GET conversations/{id}/messages` | `before?` / `after?` / `around?`（位置）、`limit?`（≤ 100）、`topic?` | `MessagePage`（见「五」） |
| `GET messages/{id}` | — | `{account, message: Message}`：一条（发件箱确认送达用） |
| `GET messages/{id}/thread` | `before?`、`limit?` | `MessagePage`，`root` 是话题的根 |
| `POST conversations/{id}/messages` | `Outgoing`（见「六」） | `SendReceipt {message_id?}` |
| `POST conversations/{id}/attachments` | query：`client_id`、`name`、`mime`、`kind`（`image` / `file`）、`reply_to?`、`thread?`；正文是文件字节 | `SendReceipt` |
| `POST conversations/{id}/compose` | `{mode, text, reply_to?, values?}` | `{ready: true, token}` 或 `{view}`（见「七」） |
| `POST conversations/{id}/read` | `{position, message_ids?}` | `{}` |
| `POST conversations/{id}/read-states` | `{message_ids}`（≤ 100） | `{states: [{message_id, read, unread}]}` |
| `GET conversations/{id}/members` | `q?`、`limit?` | `{members: [Person]}` |
| `GET conversations/{id}/pins` | `cursor?`、`limit?` | `{account, items: [ChatPin], next_cursor?, total?}` |
| `GET conversations/{id}/pins/{message}` | `topic?` | `{account, target: {message, root?, page: MessagePage}}`：跳到 Pin 的那条 |
| `POST conversations/{id}/pins` | `{message_id, pin_id?, remove}` | `{account, pinned}` |
| `POST messages/{id}/reactions` | `{key, remove}` | `{}` |
| `POST messages/{id}/actions/{action}` | `{values?}` | `{view?, toast?, open_url?}`：消息上的动作和卡片按钮（见「五」） |
| `GET messages/{id}/url-preview` | `url` | `URLPreview` |
| `GET emojis` | — | `{emojis: {key: {label, url}}, groups?: [{title, keys}]}` |
| `GET reactions/quick` | — | `{keys: [key]}` |
| `GET users/{id}` | `conversation?` | `Profile` |
| `POST users/{id}/chat` | `{}` | `{conversation_id}` |
| `GET media/{key}` | `size?`（缩略图边长） | 字节；`Cache-Control` 以扩展的为准 |
| `GET documents` | `url` | `Document`（见「十」） |
| `GET documents/comments` | `url` | `{threads: [CommentThread]}` |
| `POST documents/comments`、`…/{thread}/replies` | `CommentWrite`（见「十」） | `{}` |
| `POST documents/comments/{thread}/resolve` | `{url, resolved}`（`false` 是重新打开） | `{}` |
| `GET posts/{id}`、`GET posts/{id}/comments` | — | `Post`、`{comments: [PostComment]}`；`{id}` 可以是帖子 id，也可以是 `links` 交过来的整个链接（路径里转义成一段），扩展自己认 |

**hub 查回答**：上表有 schema 的回答（`ConversationPage`、`MessagePage`、`Message`……）hub 按 `messaging.schema.json` 查（`wire.Validate`），
不合规回 502 `extension_bad_response`（记日志，带扩展 id 和第一条不合规的路径），App 当作这次没读到。不认识的字段原样留着（新扩展给老 hub），
更新的扩展发来 hub 不认识的片段 `kind`，hub 把它换成它 `text` 的文字片段（没有字就去掉），不认识的文档块换成段落，再查一遍（`fitMessagingAnswer`）；其余不认识的枚举值仍是 502，要加就升版本。App 拿到的都是它认得的。字符串、数组有上限（schema 里写着），超了也是 502。

**发送不重复**：hub 不给 `messaging` 的发送套回执（回执会把「先去设置里打开」「加密内容过期」这类可以改正后重试的拒绝也存下来、按同一个 `client_id` 一直重放）。
扩展自己按 `client_id` 去重：同一个 `client_id` 只发出一次，重试回第一次的结果；被拒绝（`input_required`、`unsupported`、`not_signed_in`、`account_changed`、`rate_limited`）的不算发过，改正后用同一个 `client_id` 再发。

**错误**：`{error: "给人看的一句", code?}`，`code` 只用下面这些（App 按它们做事，别的当一般错误，显示 `error`）：

| `code` | 意思 | App |
| --- | --- | --- |
| `not_signed_in` | 来源那边没登录 / 登录过期 | 「请到 hub 所在的机器上重新登录」，不重试 |
| `account_changed` | 请求的 `account` 不是现在这个账号了 | 丢掉按旧账号的状态，重新读 |
| `input_required` | 写之前要用户再给点东西（解锁、选项）；回答带 `view` | 弹出 `view`（声明式 UI），提交后带 `values` 重发同一个请求 |
| `result_unknown` | 写可能成功也可能没有 | 不自动重试，提示「结果未知」，读一次确认 |
| `unsupported` | 这个来源 / 账号不支持这个操作 | 藏起这个操作 |
| `rate_limited` | 太快了 | 稍后重试 |

## 五、会话和消息

**Conversation**：`id`、`name`、`avatar?`、`kind`（`direct` / `group` / `topic_group` / `bot`）、`unread`（数）、`marked_unread?`、`mentioned?`（有未读的 @我）、
`muted?`、`pinned?`、`preview`（一行摘要，扩展已经处理好：解码、加密外显、撤回）、`preview_sender?`、`preview_mine?`、`updated_at`、
`read_position?`、`mention_position?`（打开时定位用）。

**位置**：每条消息的 `position` 是这个会话里的**非负十进制整数串**，越新越大，App 按（长度，字典序）比较；翻页的 `before` / `after` / `around` 都是位置。
扩展的游标不是位置时，放在 `older_cursor` / `newer_cursor` 里，App 下一页把它原样放进 `before`（更早一页）或 `after`（更新一页）；扩展自己分辨收到的是位置还是游标。

**MessagePage**：`account`、`messages`（旧 → 新）、`has_older`、`has_newer?`、`older_cursor?`、`newer_cursor?`、`root?`（话题的根，只在 `thread`）、`topic?`（这个会话是话题群，`messages` 是各话题的根：App 不知道会话种类就打开时，比如从资料页、收件箱，按它画成话题卡片）。

**Message**：

| 字段 | 说明 |
| --- | --- |
| `id`、`position`、`created_at` | |
| `sender` | `{id?, name, avatar?, kind?}`，`kind` 是 `user` / `bot` / `system`（系统消息居中画一行） |
| `mine` | 我发的 |
| `client_id?`、`state?` | 我发的那条的 `client_id`（发件箱对账，见「八」）；`state` 是 `pending` / `failed`（来源那边还没发出去的） |
| `parts` | 正文片段（见下），按顺序画 |
| `text` | 纯文字版（复制、通知、摘要、翻译用）；扩展已经处理好（解码、加密时是对外展示的字） |
| `reply_to?` | 引用：`{id, sender_name, summary, recalled?}` |
| `thread?` | 话题：`{count, latest_at?}`；有 `threads` 能力时显示「N 条回复」，点开话题详情 |
| `reactions?` | `[{key, count, mine?, users?: [name]}]` |
| `edited?`、`edit_history?` | 已编辑；`edit_history: [{edited_at, parts, text}]`（扩展决定给不给：飞书的「编辑历史」开关关着就不给） |
| `recalled?` | 已撤回：有 `parts` 就画正文并标「已撤回」（扩展留存的、开关允许看的），没有就画「消息已撤回」 |
| `decorations?` | `[{text, icon?, tone?}]`：正文下面的小字，比如「[removed] · 已解码」「加密消息 · 对外展示」；最多 3 个 |
| `private?` | 正文不能落盘（解开的加密消息）：App 只放内存，缓存里写 `text` 换成的 `private_text`（对外展示的字） |
| `private_text?` | 见上 |
| `actions?` | 长按菜单里扩展的动作：`[{id, title, icon?, destructive?, confirm?}]`；点了调 `POST messages/{id}/actions/{id}` |
| `can_pin?`、`can_unpin?`、`can_reply?`、`can_react?` | 不写是可以（有对应能力时） |
| `read?` | 别人发的这条我读过没有（报已读用） |

**Part**（片段，按 `kind` 区分；App 不认识的 `kind` 画成 `text` 字段的字，没有就跳过）：

| `kind` | 字段 | 画成 |
| --- | --- | --- |
| `text` | `text`、`marks?`（`bold` / `italic` / `underline` / `strike` / `code`）、`url?`、`block?`（`paragraph` / `heading1…3` / `quote` / `code` / `list_item`，`list?: {ordered, level, start?}`）、`block_id?` | 行内文字；`block_id` 一样的连续片段是同一块（同一段、同一个列表项），不一样就另起一块；不写 `block_id` 时连续的同种块合成一块 |
| `mention` | `text`、`user_id?`、`all?` | @某人（高亮，点了看资料） |
| `link` | `text`、`url` | 链接；`links` 里的主机交给来源打开 |
| `emoji` | `key`、`text` | 行内表情（`emojis` 里的图），没有图就画 `text`。`key` 区分大小写、不透明：`emojis`、`reactions/quick`、消息的 `reactions` 和回应时发回去的都是同一个写法 |
| `sticker` | `media`、`width?`、`height?` | 贴纸 |
| `image` | `media`、`width?`、`height?` | 图片（点开大图） |
| `file` | `name`、`media?`、`size?`、`ext?`、`expired?` | 文件卡片 |
| `video` | `media`、`duration_ms?`、`poster?`（`{media, width, height}`） | 视频 |
| `audio` | `media`、`duration_ms?` | 语音 |
| `code` | `text`、`language?` | 代码块 |
| `divider` | — | 分隔线 |
| `card` | `view`（声明式 View，`view.schema.json`）、`fallback` | 卡片：扩展把来源的卡片转成 View；画不了画 `fallback` |
| `forward` | `title`、`messages: [Message]`（最多 50）或 `previews: [{sender_name, text}]`、`count?` | 合并转发（点开看全部） |
| `share` | `title`、`subtitle?`、`image?`、`fields?: [{label, value}]`、`url?` | 分享的群名片、人名片、日程、会议…… |
| `system` | `text` | 系统提示（居中） |

`media` 是来源下的相对路径（`media/<key>` 或 `messages/<id>/media/<key>`，可以带 query），App 拼成 `/api/v1/messaging/{source}/<media>` 再带凭据和 `account` 去取；绝对的 https 地址不带凭据直接取。

## 六、发送

**Outgoing**：`client_id`（必须，UUID 小写；App 重试不变）、`text`、`mentions?: [{user_id, name}]`、`reply_to?`（被引用的消息 id）、`thread?`（`true`：发进 `reply_to` 的话题）、
`mode?`（输入框模式 id，见「七」）、`prepared?`（`compose` 给的 `token`）、`expect_user?`（`GET me` 的 `user_id`：这个账号现在登录的不是这个人就不发，回 409 `account_changed`，防止换了登录的人以后把旧草稿发出去）。

附件：`POST conversations/{id}/attachments`，一个附件一个请求，各自的 `client_id`；图片和文件都走它（`kind` 区分），扩展自己决定怎么转给来源。

回答 `SendReceipt {message_id?}`：有 `message_id` 是来源确认收下了；没有是接受了但还没有 id（比如文件），App 不自动重发。

## 七、输入框模式

清单的 `composer_modes` 是「普通」以外的模式：`id`、`title`、`icon`、`description?`（选它时输入框上面的一句说明）、`attachments`（能不能带附件）、`mentions`（能不能 @）、`threads?`（能不能发进话题；不写是不能）、`prepare?`。
模式能不能用随账号变（飞书的[removed]要开关开着）：`GET status` 回 `modes: [id]` 是现在能用的，不在里面的菜单里灰掉。

- 不需要 `prepare` 的（[removed]）：App 照常发 `Outgoing`，带 `mode`；扩展在发之前处理（编码），收到的同一条消息扩展再处理回来（解码 + 装饰）。
- 需要 `prepare` 的（加密）：发之前 App 调 `POST conversations/{id}/compose {mode, text, reply_to?, values?}`：
  - `{ready: true, token}`：带 `prepared: token` 发；
  - `{view}`：要用户先做点什么（选对外展示的字、输入密钥库密码）：App 弹出这个 View，用户提交后带 `values` 再调一次 `compose`。这个 View **只能有** `text` / `field`（含 `choice`、`secret`）和带 `submit` 的 `button`：App 只把字段的值带回去，按钮的 `id`、行的动作都不会到扩展（选一项用 `choice`）。`input_required` 错误带的 View 同样。
  - `token` 一次有效，10 分钟过期；过期发送回 409 `input_required`，App 重新 `compose`。
- 草稿记着模式；模式不在 `status.modes` 里时草稿保留、发送按钮灰掉并说明。

## 八、发件箱、草稿和账号

这些都是 App 通用的，扩展只要守住下面几条：

- **`client_id` 幂等**：同一个 `client_id` 只发一次（hub 的回执挡一层，扩展自己也要挡：来源有发件箱的用来源的）。
- **回显对账**：我发的消息读回来时带上 `client_id`（来源存得下的话）；App 用它把本地那条换成真的。存不下时 App 退到「文字 + 时间」对账（只对文字）。
- **送达确认**：`GET messages/{id}` 能读到、`mine`、`state` 不是 `pending` / `failed`，就算送达。
- **账号**：有 `accounts` 时，账号相关的回答都带 `account`（`ConversationPage`、`MessagePage`、Pin……）；App 请求时带的和回答的不一样就当 `account_changed`。
  `GET me` 给发件箱分账号（换了登录的人，旧账号没发出去的不会跟着发到新账号）。

## 九、实时事件和收件箱

**事件**：扩展发 `messaging.changed {source, conversations?: [id], account?, settings_changed?}`（进程协议的事件，所以来源的扩展要说进程协议），
hub 一秒内合并后推 WebSocket `{type: "messaging", source, conversations, account?, settings_changed?}`。App：打开着的会话马上重读最新一页；
列表重读；不在屏幕上的记下来，回到前台、打开时再读；`settings_changed` 重读 `status`（输入框模式）和设置页。没有事件也照样能用（App 前台时 60 秒轮询一次打开的会话）。

**收件箱**：来源的会话变成收件箱条目照旧由同一个扩展的连接器做；条目上的会话定位从飞书专用的 `lark` 字段换成通用的
`conversation: {source, id, account?, start_position?, read_position?, mention_position?}`，App 点条目打开那个会话、定位到 `start_position`。

**通知**：hub 发的推送带同样的 `conversation`，点了打开会话。

## 十、文档和帖子

**Document**（`documents` 能力）：`{title, url, blocks: [Block], permission?: {copy, export}, partial?}`。`Block` 是 `heading`（`level`）、`paragraph`、`list`（`ordered`、`items`）、
`quote`、`code`（`language`）、`callout`、`table`（`rows`）、`image`（`media`）、`divider`、`embed`（`title`、`url`），行内是 `Part` 里的 `text` / `mention` / `link`。
App 原生画，点评论、选中正文发评论（`doc_comments`）。来源的文档格式（飞书的 ProseMirror JSON）在扩展里转成 `Document`。

**评论**：`CommentThread {id, quote?, block_id?, resolved, replies: [{id, author: Person, parts, created_at, edited?}]}`；写评论 `{url, parts, mentions?, quote?, block_id?, client_id}`。

**Post**（`posts` 能力，公司圈这类）：`{id, author: Person, parts, images?: [{media, width, height}], reactions?, comment_count?, created_at, categories?}`；评论 `PostComment {id, author, parts, created_at, hot?}`。

## 十一、App 的入口

- `GET /api/v1/messaging` 有来源（且用户没在设置里关掉）时，底栏 / 外壳多一个入口，名字、图标是来源的；没有来源就没有，旧的「首页显示消息」开关迁成「显示 <来源名>」。
- 设置 › 连接器里来源对应的连接器那一行，进去有 `settings_ui`（声明式页面）和账号列表。
- `links` 里的链接点了交给来源（文档阅读、帖子、或扩展的整页 UI）。
- App 里不再出现某个来源的名字、错误码、事件类型、路径；缓存、草稿、发件箱按 `source` 分目录。

## 十二、版本和兼容

- 扩展点 `messaging` 版本 1；hub 的 `features` 有 `messaging`，App 看到它才走这一套。
- 迁移期间 lark-connector 扩展同时提供老的 `/api/v1/lark`（Mac、Android、网页还在用）和 `messaging`；iOS 有 `messaging` 就用新的，没有就用老的。
  iOS 的老实现在 iOS 验收通过后删掉（#2138 阶段 4）；老接口等四端都迁完再删。
- 加字段向后兼容：老 App 不认识的字段、片段、能力跳过；扩展不能靠新字段表达老 App 必须懂的意思（那就是新版本）。

## 十三、飞书怎么对上（lark-connector 的实现要点）

| 飞书 / lark-connector | 这份协议 |
| --- | --- |
| `ol_account`（数字） | `account`（不透明字符串） |
| `/raw/accounts` 的 `userId` 核对 | `GET me` |
| `/raw/messages/{id}` 送达确认 | `GET messages/{id}` |
| 卡片元素树（`LarkCard`）、摘要卡片（合并转发、分享） | `card`（转成 View）、`forward`、`share` |
| 防撤回、编辑历史开关 | 扩展按开关决定给不给 `recalled` 的正文和 `edit_history`；开关页是 `settings_ui` |
| [removed] | 输入框模式 `brave_climb`；读到时扩展解码，加装饰「[removed] · 已解码」 |
| 加密（OLK） | 输入框模式 `sealed`（`prepare`：选对外展示、解锁）；读到时扩展解开（密钥库开着）并标 `private`、装饰「加密消息」，解不开给对外展示的字 |
| `lark_not_logged_in`、`lark_account_changed`、`OLK_VAULT_LOCKED` | `not_signed_in`、`account_changed`、`input_required`（带解锁的 View） |
| 文档（`/raw/documents/*`） | `GET documents`（扩展转成 `Document`） |
| 公司圈 | `posts` |
| WS `{type: "lark", feed_ids, ol_account}` | `messaging.changed` |
| 收件箱 `InboxLark` | 条目的 `conversation` |
