# 计划：Discord 连接器（用户自己的机器人）

状态：方案已定，开工（2026-11-05，#2828）。Discord 是一个**连接器**（数据来源：知识库、收件箱、助手的工具），不是渠道（和 Persona 对话的入口，`docs/channels.md`）；
同一个机器人以后兼做渠道另开 issue。第一版**只读**。框架见 `docs/connectors.md`（能力清单、收件箱、知识库、闸门）和 `docs/connector-protocol.md`（协议 v2），
扩展的清单、权限、沙箱见 `docs/extensions.md`，本文只写 Discord 自己的部分。代码在公开的扩展仓库 `nwverse/persona-extensions`（`extensions/discord/`），和 Telegram 一样（#2057）。
做完后把设计并进 `docs/connectors.md`，这份挪进 `docs/archive/`。

## 能看到什么：Discord 只允许用自己的机器人

Discord 读消息的几条路，只有后两条是条款允许、普通人能用的：

| 路 | 能看到 | 能不能用 |
| --- | --- | --- |
| 用户 token / self-bot（像 Telegram 那样用自己的账号） | 自己能看到的一切，含私聊 | **不能**：违反服务条款和社区准则（自动化的用户账号），会封号 |
| OAuth 读消息（`messages.read`、`dm_channels.read`） | 用户的私聊、群私聊 | **不能**：只给 Discord 批准的合作方 |
| 自己的机器人（bot token） | 机器人被拉进的服务器里、它有权限的频道；别人给机器人的私聊 | 能：拉机器人进服务器要有「管理服务器」权限，所以是自己的服务器，或管理员同意的 |
| 用户安装的应用（User Install）的消息命令「发给 Persona」 | 用户在**任何地方**（含私聊、群私聊、机器人不在的服务器）右键一条消息点它时，那一条消息的全文 | 能：命令的交互里带着完整的消息（`data.resolved.messages`），不需要 Message Content intent；交互走 Gateway，不要公网地址 |
| 数据包（设置 › 数据与隐私 › 申请数据） | 只有自己发的消息，最长等 30 天 | 能，但只有一半对话，以后再看 |

所以 Persona 的 Discord = **自己的机器人看到的服务器频道** + **自己点「发给 Persona」的消息**。别人和用户之间的私聊，用户不点就看不到，这是 Discord 的边界，不绕。

## 形态：扩展 `discord`

- 扩展仓库里的 `extensions/discord/`，Go 写，Discord 客户端用 `disgoorg/disgo`（API v10、用户安装的应用、消息搜索都支持；还没到 1.0，`go.mod` 钉版本）。
- hub 上的进程扩展（`runtime`），贡献一个连接器，hub 经插件协议调（`server/internal/connector/proc`），和 Telegram 一样。
- 只走 HTTPS（REST、CDN）和 WSS（Gateway），所以**能在沙箱里跑**（Telegram 的 MTProto 是 TCP，不行）：`permissions.network` 列出 Discord 的几个域名，出站经 hub 的出口代理。
- 不在 node 上跑：node 上的程序调不了 hub 的方法（`docs/extensions.md`「在电脑上运行的程序」），当不了连接器。

清单的样子：

```json
"connectors": [{
  "id": "discord", "name": "Discord",
  "capabilities": ["status", "tools", "events", "inbox", "archive", "login", "threads", "fallback"],
  "kinds": ["mention", "saved"],
  "kind_descriptions": {
    "mention": "Discord 服务器里 @ 了用户、回复了用户的消息",
    "saved": "用户在 Discord 里把一条消息「发给 Persona」"
  }
}],
"configuration": {"triage_rules": "…（必填，#1730）", "history_days": "第一次往回补几天，默认 30"},
"permissions": {"network": ["discord.com", "*.discord.com", "*.discord.gg", "cdn.discordapp.com", "media.discordapp.net"]}
```

`events` 是 Gateway 来了新消息就调 hub 的 `changed`，hub 再来取 `archive.changes` 和 `inbox.collect`（`docs/connector-protocol.md`）。

## 登录和凭据

用户自己建一个 Discord 应用，机器人和应用都是用户自己的（Discord 那边的开发者就是用户本人）：

1. 在 Discord 开发者后台新建应用：Bot 页打开 **Message Content intent**、关掉 **Public Bot**（只有自己能邀请）；Installation 页勾上 **User Install**。
   自己用的机器人规模很小，开 Message Content intent 不用审核（只有大规模的应用才要）。
2. 在 设置 › 连接器 › Discord 里填 bot token：`login.step` 只有一步，填字段（`docs/connectors.md`「登录支持「打开网页去授权」」三种步骤里的第一种），附开发者后台的步骤说明。
3. 扩展校验：
   - 用 token 调 `GET /users/@me`，401 就是 token 不对；
   - 连 Gateway：关闭码 4014 是 intent 没开，状态的 `guide` 写「到开发者后台 › Bot 打开 Message Content intent」；
   - 用 `GET /oauth2/applications/@me` 取应用的主人（`owner`，应用属于团队时取团队的 `owner_user_id`）：主人就是「我」，用来判断哪些消息是我发的、谁 @ 了我。
4. 状态的 `guide` 给两个链接：
   - 邀请机器人进服务器：`scope=bot applications.commands`，权限只要「查看频道」+「读取消息历史」；
   - 把应用装到我的账号（`integration_type=1`）：之后任何地方的消息右键菜单 › 应用 › 发给 Persona。
5. token 经 `connector.secrets.*` 加密存进 hub 的数据库（`connector_secrets`），App 只显示机器人的名字；退出时删。日志里永远没有 token。

Gateway 要的 intent：`GUILDS`、`GUILD_MESSAGES`、`DIRECT_MESSAGES`、`MESSAGE_CONTENT`、`GUILD_MESSAGE_REACTIONS`（判断已处理，见「收件箱」）。不要 `GUILD_MEMBERS`、`GUILD_PRESENCES`。

## 知识库：读什么

- **会话**：一个频道 / 话题（thread）/ 论坛帖一个会话 `discord-<频道 id>`，名字「服务器 #频道」或「服务器 #频道 › 话题」，`kind: group`。
  机器人和「我」的私聊不收（里面只有测试），别人给机器人的私聊也不收。
- **收哪些频道**：机器人能看到的文字频道、公告频道、话题和论坛帖都收；不想收的频道在 Discord 里不给机器人「查看频道」权限。Persona 里不另做一份频道勾选。
- **第一次**：每个频道往回补 `history_days` 天（默认 30），每个频道最多 500 条；`GET /channels/{id}/messages?before=` 一页 100 条。
  话题只补这段时间里有动静的（进行中的 + 最近归档的）。
- **之后**：Gateway 实时收，`events` 踢 hub 来取；扩展重启、断线后，按游标里每个频道的最后一条消息 id 用 `after=` 补齐。
  游标（`archive.changes` 的 `cursor`，hub 保存）是「频道 → 最后一条消息 id」的表。
- **消息**：发送者的显示名、正文（mention 写成 `@名字`、`#频道`，自定义表情写成 `:名字:`）、回复的引用、嵌入的标题和链接；「我」发的标 `mine`。
  编辑和删除第一版不同步（「用户定的」第 3 条）。
- **不建人**（「用户定的」第 2 条，Discord 开发者政策不许给用户画像）：消息只带发送者的名字，**不带 `sender_id` 和 `sender_handles`**。
  知识库只在消息带 `sender_id` 时才建人、记身份线索（`server/internal/kb/archive.go` 的 `notePerson`），整理时的人物备注也只写会话里已有的人
  （`server/internal/kb/compress.go` 的 `notePersonNote`），所以 hub 不用改；扩展的测试守住每条消息都没有这两项。
- **附件**：Discord 的附件链接带签名、会过期，所以入库时就取（`archive.media`，大小上限照 `ArchiveMediaMost`）；过期了就重新取这条消息，拿新链接。
- **「发给 Persona」的消息**也进知识库：放进它所在频道的会话 `discord-<频道 id>`（私聊、机器人看不到的频道也一样，这种会话里只有用户点过的那几条），
  名字用交互里带的频道信息。hub 取走之前先记在扩展自己的数据目录（`PERSONA_EXTENSION_DATA`），扩展重启不丢。

## 收件箱：什么算「需要我」

| kind | 什么 | 档 |
| --- | --- | --- |
| `mention` | 服务器里 @ 我、回复我的消息、@ 了我所在的身份组 | 要处理（action） |
| `mention` | @everyone / @here | 知会（fyi） |
| `saved` | 我点了「发给 Persona」的消息 | 要处理（action） |

- **已处理**：我之后在同一个频道 / 话题里发了言、给这条消息加了表情，或者在 Persona 里处理掉。Discord 没有给机器人看的已读状态，所以不看已读。
- 8 天前的不再进收件箱（和 Telegram 一样）。机器人发的消息不进收件箱。
- `triage_rules` 是必填的配置（#1730），有 `fallback` 能力，每一条都带 `extra.rule_tier`；`thread` 是频道（话题）id，同一个频道的几条合成一行。
- 「发给 Persona」点下去，扩展在 3 秒内回一条只有我能看到的消息（ephemeral）：「已交给 Persona」。

## 助手的只读工具

| 工具 | 做什么 |
| --- | --- |
| `discord_channels` | 机器人能看到的服务器和频道，最近有动静的在前 |
| `discord_messages` | 一个频道 / 话题的消息，按时间往前翻 |
| `discord_search` | 在一个服务器里搜消息（`GET /guilds/{id}/messages/search`）；Discord 还在建索引（回 202）时退回知识库的搜索，并说明 |

工具的描述和参数说明放扩展自己的 `internal/prompts/mcp/`（`docs/prompts.md` 的规矩）。消息内容按不可信数据处理（提示注入），工具结果里不带 token、不带用户 id。

## 会话页

`threads`：收件箱里的 Discord 会话在 App 里打开成会话页（`thread.page`，经 REST 现取一页），画成群聊的气泡（`kind: group`），头像和图片经 `thread.media`。
第一版没有输入框，只有「在 Discord 里回复」：打开 `https://discord.com/channels/<服务器 id>/<频道 id>/<消息 id>`（私聊的服务器 id 是 `@me`）。

## 网络

有的网络里直连不了 Discord（hub 在这种网络里时 REST 和 Gateway 都不通）：

- **不在沙箱里**（受信任市场来的扩展默认不开沙箱）：扩展的密钥里可以填 `proxy`（`http://`、`https://`、`socks5://`，可以带账号密码，所以是密钥不是配置），REST、Gateway、CDN 都走它。
- **在沙箱里**：扩展只能经 hub 的出口代理出去，出口代理再接 hub 进程自己的 `HTTPS_PROXY`。按扩展分别配上游是 hub 的后续改动（#1106），在那之前只能整个 hub 一起走代理。
- 不在 node 上跑（见「形态」），所以不能「挑一台能上 Discord 的电脑」来跑它；要那样，就在那台电脑上开个代理，让 hub 经它出去。

## Discord 的规矩

Discord 开发者政策和开发者条款里和我们有关的几条：

- **不给用户画像**（政策 §16：不用 API 数据给 Discord 用户、他们的身份和他们之间的关系画像）：所以不从 Discord 的发送者建人、不记身份线索（「知识库」一节）。
- **不拿消息内容训练模型**（政策 §21）：Persona 只把消息交给模型做总结、分拣、回答（和别的来源一样），不训练。
- **按要求删除**：用户在 设置 › 连接器 › Discord 里「删除已存的知识库内容」；退出登录就停止收、删 token。
- **静态加密**：token 加密存数据库；知识库是 hub 机器上的文件（所有来源都一样），靠 hub 机器的磁盘加密，`docs/connectors.md` 写明。
- **限流**：全局每秒 50 个请求，按路由的桶看响应头（disgo 管）；10 分钟里无效请求（401、403、429）超过 1 万次会被临时封 IP，
  所以 401 就停下来等用户重新登录，403 的频道记下来不再取。

## 测试

- 假的 Discord：`httptest` 起 REST 和 Gateway（WebSocket），合成的服务器、频道、消息、交互，不用真账号。
- 扩展 SDK 的一致性测试（`exttest`）：经协议调的结果和直接调一样；`archive.changes` 的游标续得上、断线补齐不重不漏。
- 守住的：消息没有 `sender_id` / `sender_handles`；`@everyone` 是知会；我后来发言、加表情的条目算已处理；token 不出现在日志、状态、工具结果里；
  沙箱里的出站只有清单里的域名。
- 真账号的路径（建应用、邀请、Gateway、「发给 Persona」在私聊里能拿到全文）开工时手动验一遍，PR 里如实写验了什么。

## 分步

一步一个 PR，扩展仓库的 PR 写 `Refs nwverse/persona#2828`：

| 步 | 做什么 |
| --- | --- |
| D1 | 登录、状态、知识库（第一次补、实时、断线补齐）、只读工具、`proxy`、测试 |
| D2 | 收件箱（`mention`，已处理的判断，`triage_rules`） |
| D3 | 「发给 Persona」消息命令（`saved`）：先用真账号验证私聊里的交互能拿到消息全文 |
| D4 | 会话页、附件 |

本仓库：这份方案（第一个 PR）；`docs/connectors.md` 的表在 D1 合入后加一行；Discord 的品牌标志（三端，或等 #1426 品牌数据驱动）。

## 不做 / 以后

- **不做**：self-bot / 用户 token、OAuth 读消息（「能看到什么」）。
- **以后**：以机器人的身份发消息（做成 action，`confirm_always` 过闸门，单条最长 2000 字）；数据包导入；同一个机器人兼做渠道（私聊机器人就是和主对话说话，像飞书机器人）；
  编辑和删除的同步。各自另开 issue。

## 用户定的（2026-11-05）

1. 覆盖范围：只做「自己的机器人 + 发给 Persona」，不做 self-bot。
2. 知识库的人：只归档聊天，不从 Discord 的发送者建人（开发者政策 §16）。扩展不传 `sender_id` / `sender_handles` 即可，不给知识库加开关。
3. 编辑和删除：第一版不同步，靠现有的「删除已存的知识库内容」；同步删除以后再看。
4. 第一版只读，以机器人身份发消息以后再定。
5. 放哪：方案和跟踪 issue 在本仓库（#2828），代码在 `nwverse/persona-extensions`。
