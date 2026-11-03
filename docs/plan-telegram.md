# 计划：Telegram 连接器（用户自己的账号）

状态：方案已定，开工（2026-11-03）。用户定了：用自己的账号（MTProto 用户客户端，不是机器人）；消息文字可以交给 AI，和飞书一样进知识库和收件箱，
助手能查、能总结；设置里留开关。第一版**只读**。框架见 `docs/plan-connectors.md`（Connector 接口、Registry、闸门），本文只写 Telegram 自己的部分。
做完后把设计并进 `docs/connectors.md`（或那份计划的「Telegram」一节），删掉这份。

## 选型：gotd/td（纯 Go），不用 TDLib

| | gotd/td | TDLib |
| --- | --- | --- |
| 语言 | 纯 Go，`CGO_ENABLED=0` 能交叉编译，和现在的 personad 一样四个平台一份代码 | C++ 库，要 cgo 或者带一个 `tdjson` 动态库；四个平台各编一份，personad 的发布方式（publish.sh、自动更新）都得改 |
| 状态 | 自己管：会话（auth key）要我们存，更新的游标我们管 | 自带 SQLite 数据库，存在本地目录，难以加密进 Postgres |
| 维护 | 活跃，跟 Telegram 的 layer 走；有 `tgtest` 内存服务、`auth` 登录流程、`updates` 状态机、`floodwait` | 官方，最全 |
| 体积 | 编进 personad 会多约 35 MB（实测 linux-amd64：39 → 74 MB，gzip 后 14.5 → 23.7 MB，主要是 Telegram 全部类型的生成代码），所以单独一个程序（下一节） | 二进制外再带一个几十 MB 的库 |

选 gotd/td：只要一个 Go 依赖，不碰发布流程；会话由我们存（正好加密进 Postgres）。只用它的原始 API（`tg.Client`）、登录（`auth`）、限流（`tgerr.FloodWait`）。

## 架构：单独一个程序 `jarvisd-telegram`（用户 2026-11-03 定的 B）

gotd 让二进制大 35 MB，所以 Telegram 不编进 personad，单独一个程序，personad 本身不变大。

- **代码**：连接器在 `server/internal/connector/telegram`（用 gotd），程序是 `server/cmd/personad-telegram`，同一个 Go module、同一次构建出来。
  hub 不传任何 Telegram 的配置给它：凭据和会话都在 `connector_secrets` 里，它经 `secrets.*` 读写。
  personad 不 import 它：测试守住 `go list -deps ./cmd/personad` 里没有 gotd。
- **hub 怎么用它**：开关开着时，hub 起 `jarvisd-telegram`（和 personad 放在同一个目录），用 stdin / stdout 跑现有的双向 JSON-lines RPC
  （`internal/rpc`，hub↔node 用的那个）。hub 这边是一个通用的「子进程连接器」适配器（`connector/proc`），把 Connector 的各项能力
  （状态、登录、归档、收件箱、只读工具、事件）一一转成 RPC 调用；子进程那边一个 `connector/serve` 把任意 Go 连接器挂到 stdio 上。
  加密存储反过来：子进程调 hub 的 `secrets.load / save / delete`，会话的明文只在两边的内存里，不落子进程的磁盘。
  子进程退出了 hub 按退避重启；开关关掉就停掉它。以后别的大依赖的连接器也能这样做。
- **版本对齐**：hub 起它之前先跑 `jarvisd-telegram version -v`，版本和 build 号必须和 personad 一样，不一样就不起，状态写「jarvisd-telegram 和 hub 版本不一致」。
- **只在 hub 上**：node 不需要它，`deploy.sh` 只给 hub 那台装；Docker 镜像里也放一份（hub 在容器里跑时用）。
- **发布和自动更新**：
  - `publish.sh` 四个平台各编一份 `jarvisd-telegram-<os>-<arch>.gz`（每个约 10 MB，在 25 MiB 以内），一起过隐私扫描和体积检查；
  - `latest.json` 加 `programs: {"jarvisd-telegram": {"<os>-<arch>": {name, size, sha256, binary_sha256}}}`，老 hub 不认这个字段，照旧只换 personad；
  - hub 自动更新时两个一起下载、校验（`version -v`）、一起换上，旧的各留 `.prev`；新版起不来回滚时两个一起回滚。
- **部署**：`deploy.sh` 和主会话的 `deploy-hub.sh`（写成 `.new`）在 hub 机器上多放这一个。

## 凭据：都由用户在 App 里填（用户 2026-11-03：「Telegram 的 API 凭据也让用户填，不要写到程序里面」）

- **api_id / api_hash**：不放 hub.json、不编进程序、不放构建配置。登录的**第一步**就让用户填，表单上附一句申请指引：
  「去 my.telegram.org，用你的 Telegram 账号登录，进 API development tools 建一个应用，把 api_id 和 api_hash 填到这里」。
- **会话（auth key + 数据中心）**：gotd 的 `session.Storage` 接到 hub。凭据和会话放在一起（一份 JSON），用 `secret.key` AES-GCM 加密
  （和启动器的 Key、邮箱凭据同一个主密钥，`sealKey`），存 Postgres 的 `connector_secrets`（T1）。明文只在两个进程的内存里。
- **显示**：App 只显示 api_id，api_hash 打码（只露最后 4 位）：状态的 `via` 是「api_id 1234567 · api_hash ••••a1b2」。
  日志、错误、响应里永远不出现这两个值（连接器的错误先抹掉它们）。
- **登录**（设置 › 连接器 › Telegram）：api_id + api_hash → 手机号 → 验证码（发到 Telegram 的其他设备上，或短信）→ 有两步验证时再填密码。
  第一步先用填的凭据连一次 Telegram（`help.getConfig`），不对就留在这一步（「api_id 或 api_hash 不对」）。
  之后 hub 一步一步转给 Telegram：`auth.sendCode` → `auth.signIn` →（`SESSION_PASSWORD_NEEDED` 时）`account.getPassword` + SRP 校验密码。
  中间状态（`phone_code_hash`）只在内存，程序重启了就从手机号重新来。密码只经过内存，用完即丢。
- **退出**：`auth.logOut`（Telegram 那边这个会话也没了），再删 `connector_secrets` 那一行：凭据和会话一起删。要换凭据就退出再登录。
- **默认关着**：jarvisd-telegram 在的 hub 都会登记这个连接器，但默认是关的（T1 的 `addConnectorOff`）；用户在设置里打开开关、再按上面的步骤登录。
- hub 的网络：hub 机器直连 Telegram 的数据中心（2026-11-03 测过 TCP 443 和 web.telegram.org 都通）。连不上的环境以后再加代理，这版不做。

## 读什么

- **会话**：私聊、普通群、超级群。**不收**：广播频道（channel）、和机器人的私聊、Saved Messages（拿不准的见下面「要定的」）。
  秘密聊天（secret chat）经 API 拿不到（端到端、只在那台设备上），说明里写清楚。
- **第一次同步**：最近活跃的会话（`messages.getDialogs`，按最后一条时间），每个会话往回取最近 30 天、最多 500 条。更早的不补（以后要的话按知识库的往回补做）。
- **增量**：游标是每个会话已归档的最大消息 id（`Archive.Changes` 的游标里存一个 `会话 → 最大 id` 的表）。每轮 `getDialogs` 看哪些会话的最新一条比游标新，
  再 `messages.getHistory(min_id=游标)` 取新的。实时的 updates 只用来「踢」一下（`Events` → `ScopeChats` / `ScopeInbox`），不靠它记游标：
  超级群的更新要按每个群单独追 pts，按 id 拉更简单，也不会因为断线漏消息。
- **消息内容**：文字原样；回复带被回复那条的开头（「回复「…」：」）；转发写「转发自 某某」；图片、文件、语音、贴纸记一行占位（「[图片]」「[文件 名字 · 大小]」），
  第一版不下载附件。编辑、删除不追（归档是追加的，和飞书一样）。
- **知识库 id**：`chats/telegram-<u|g|c><数字 id>`（用户、普通群、超级群），不用用户名（会改）、不出现手机号。人：`people/telegram-<用户 id>`，名字取显示名。
- **限流**（#958，用户：「Telegram 的频率限制还是要非常尊重的」）：整个连接器只有一道请求闸门（`connector/telegram/gate.go`，
  gotd 的 middleware，所有 RPC 都过它，登录、检查凭据、gotd 自己取配置也是）：
  - 一次只发一个；上一个结束到下一个开始之间随机 0.5–1 秒（和用户自己的 Telethon skill 一样）。归档、收件箱、助手工具同时要读时排队。
  - Telegram 回 `FLOOD_WAIT_X`：X 秒再加 0.5–1 秒之内一个请求都不发；从不重试，碰到它的那一轮到此为止（已经读到的照样归档），
    之后的归档、收件箱刷新、工具调用在闸门前就回「Telegram 限流中，N 秒后再试」，不发请求。恢复后马上又碰到，同样整轮放弃、等下一轮。
  - 状态：已登录的账号在限流期间是 `rate_limited`，写明几点开始、Telegram 要求等多久、还要多久、几点恢复（用户的时区）；
    App 的 Telegram 页黄点、说明是灰字（限流不是出错）。hub 只读 `ok` 的连接器，限流期间不归档、不给助手 Telegram 的工具。
  - 助手工具一次只读一页：`telegram_messages` 最多 50 条、`telegram_search` 最多 30 条；按名字找会话先看最近一次列出的会话列表，
    找不到才去问 Telegram，所以一次调用最多三个请求。
  - 限流状态在 jarvisd-telegram 的内存里：hub 重新部署后程序重启，第一次请求如果又碰到限流，再记下来。

## 收件箱：什么算「需要我」

- 私聊：对方发来、我还没读也没回（`read_inbox_max_id` 之后、最新一条不是我）。
- 群：@ 了我的、回复我的消息的（`unread_mentions_count`、回复到我的消息）。静音的群和归档文件夹里的会话不进收件箱，只进知识库。
- 在手机 / 电脑的 Telegram 上读了且不是「需要你」的 → `done_by=read`；我在 Telegram 上回了 → `done_by=replied`。
- **不回复**（第一版只读）：`CanReply=false`；`OpenURL` 是 `tg://resolve?domain=<用户名>`（有用户名的）或 `https://t.me/…`，在 App 里显示「在 Telegram 里回复」，交给系统打开。
  以后要从这里发：`telegram_send` 作为写操作，必须过闸门（卡片写明发给谁、全文）。

## 助手的只读工具

| 工具 | 做什么 |
| --- | --- |
| `telegram_chats` | 最近的会话：名字、类型、未读数、最后一条的时间和开头 |
| `telegram_messages` | 一个会话的消息（往前翻页，截断） |
| `telegram_search` | 搜消息（`messages.searchGlobal`，或指定会话 `messages.search`） |

描述放 `server/internal/prompts/mcp/telegram/`，golden 加一种「有 Telegram」的工具列表。已经归档的内容也能用知识库的工具查（`kb_*`）。

## tg 命令行和 telegram skill（#988）

用户要求：「参考 feishu-cli 的 cli 设计……再制作一个 skill 来让 agent 在 hub 对接了 telegram 之后，支持用 skill 更灵活地操作（对接成功了才有 skill）」。

- **`personad tg …`**（`server/internal/tgcli`；助手的 PATH 上叫 `tg`）只经过 hub：它和 MCP 一样连本机 node 的 socket（kind `cli.call`），
  node 再调 hub 的 `mcp.call`，最后由 Telegram 连接器发请求，所以每个请求都过 #965 的闸门。CLI 不连 Telegram，也没有 token。
  **谁是助手由 node 判定**（`node/clicall.go`）：socket 对端的进程（Linux `SO_PEERCRED`、macOS `LOCAL_PEERPID`）要是 node 为助手起的进程，
  或者是它的子孙（助手的 Bash），才按助手的身份调；判断不了就拒绝（Agent 读不到用户的私聊）。node 也只转发 `telegram_` 开头的调用。
- **读**：`tg chats` / `unread` / `messages`（分页 `next_before`，`--since` / `--until`）/ `search` / `message` / `chat`（资料）/ `members`。
  **写**：`tg send` / `reply` / `forward` / `react` / `read`（标已读）。写操作是连接器的 actions（`telegram_send`、`telegram_forward`、
  `telegram_react`、`telegram_mark_read`），过 `connector_gate`：默认在手机上确认，卡片写明会话、全文、谁会看到；不另加闸门。
  写操作只认会话的 id 或 @用户名，而且要是读命令或收件箱列过的（只看内存里的，不问 Telegram），所以卡片上的会话就是实际写到的会话。
- 这些工具在 Manifest 的 `Unlisted` 里：不进模型的工具列表（助手照旧只看到三个只读工具），只按名字调。描述和参数在
  `prompts/mcp/telegram/`，实现在 `connector/telegram/cli.go`；读的回 JSON（snake_case，时间是用户时区的 RFC 3339），上限和三个工具一样，一次一页。
- **输出**照 feishu-cli 的做法：stdout 一行 JSON（`--format text` 给人看）；失败时 stderr 一行 `{error, kind, retry_after}`，退出码
  0 成功、1 出错、2 用法、3 用不了（没登录、关着、没 hub、不是助手）、4 限流（`retry_after`）、5 用户拒绝或规则不许。
- **skill**：内置的 `assistantskills/builtin/telegram/SKILL.md`。只有 hub 上 Telegram 是 ok 或限流（开着、登录着）时才装：hub 每次
  `assistant.prepare` 带上登录着的连接器（`connectors`），node 据此装上或撤掉 skill 和 `tg`（`assistantskills.Requires`）。
  退出登录、关掉开关之后，下一条消息之前就撤掉。
- 子进程协议为此加了 `actions.list` / `actions.describe` / `actions.do`（`docs/connector-protocol.md`），2026-11-03 和 ovis 对过。

## 设置 › 连接器 › Telegram

- 状态：关着 / 未登录 / 已登录 · 显示名（@用户名）· 「api_id 1234567 · api_hash ••••a1b2」/ 限流 / 出错。
- 登录：一步一个表单（api_id + api_hash 和申请指引 → 手机号 → 验证码 → 密码），通用：hub 告诉 App 这一步要哪些字段，App 照着画
  （以后别的要分步登录的连接器也用它）；api_hash、密码是 `secret` 字段，输入时遮住。
- **开关「同步 Telegram」**：默认关；关掉就停止同步（不连 Telegram、不进知识库和收件箱、助手没有工具），登录状态保留。
- **删除已存的数据**：知识库里 `telegram-*` 的会话和人、收件箱里 Telegram 的条目、游标；要二次确认。
- **开关「进知识库」「进收件箱」**（#989，通用连接器页按能力显示）：默认开。关掉「进知识库」就不再归档（hub 不调 `Changes`，不发归档的请求），收件箱和助手工具照常；「删除已存的知识库内容」只删知识库里 `telegram-*` 的会话、人和游标，并把「进知识库」存成关。关掉「进收件箱」就不再收新的，已有的条目不动。
- **退出登录**：凭据和会话一起删（见上）；要换凭据就退出再登录。
- 收件箱的行用通用的连接器块（GitHub 已经在用），标志 `Theme.Brand.telegram`（官方标志，加资源）。
- 收件箱：连接器不能回复（只读）的条目，操作栏里是「在 Telegram 里回复」（`open_url`，`tg://` 或 `t.me` 链接；和信息的「在信息里回复」是同一个按钮，`InboxActionBar.openTitle`），不出回复框。
- App 这边是通用的（`ConnectorSignInSections` / `ConnectorSignInModel`，`HubConnector.signsInHere` = 有 `login` 能力）：页面不认 Telegram，
  只照 hub 给的字段画；单元测试 `ConnectorSignInTests`（`FakeHub`），UI 测试 `TelegramConnectorTests`（mock hub 的 `MOCK_TELEGRAM=2fa`：
  凭据 → 手机号 → 错的验证码 → 验证码 → 密码 → 已登录 → 退出），mock 还有 `MOCK_TELEGRAM=off|signed_out|signed_in`、`MOCK_TELEGRAM_INBOX=1`。

## 接口改动（`server/internal/connector`，2026-11-03 和 ovis 对过）

1. **分步登录** `Login`（可选能力）：`LoginStep(ctx, step, values) (Prompt, error)`、`Logout(ctx) error`。
   `Prompt{Step, Fields []Field, Hint, Error, Done, Account}`，`Field{Name, Label, Secret, Placeholder}`；`step` 为空是开始。
   「验证码不对」这类给人看的一句放 `Prompt.Error`，表单留在这一步重填；只有出了不该出的错才返回 `error`。
   计划里的 `Credentials`（一步）就是只有一步的 `Login`，不另做。会话 / token 只走 `Secrets`，永远不进 `Prompt`、`Status`、日志。
   hub：`GET /api/v1/connectors` 每项多 `login`（这一步要的字段）、`POST /api/v1/connectors/{id}/login {step, values}`、`POST …/logout`。
2. **加密存储** `Secrets{Load, Save, Delete}`：hub 按连接器 id 给一个实现（Postgres 新表 `connector_secrets(connector, sealed, updated_at)`，
   一个连接器一个账号；`sealKey` 加密），构造连接器时传进去。
3. **开关**放 `Registry`：`SetEnabled(id, bool)`，新状态 `StateDisabled`，`StatusOf` 对关掉的回 `disabled`
   （工具、收件箱、归档这些按状态判断的地方都不用改）；`Start` 给每个连接器自己的 ctx，关掉时取消它的 `Run` / `Watch`，再打开时重新起。
   开关的持久化在 hub（settings 表）；`Infos` 加 `enabled`。`PUT /api/v1/connectors/{id}/settings {"enabled"}`。
4. **删除数据**放 hub（知识库、收件箱、游标都是 hub 的）：先 `SetEnabled(false)`（免得边删边进），再按前缀删知识库的 `<id>-*` 会话和人、
   收件箱 `Source=<id>` 的条目、`cursors/<id>.json`。`DELETE /api/v1/connectors/{id}/data`。
- 和 ovis 的迁移（M1 #529 加 `Chats` / `Items`、M6 给 `Status` 加 `Guide`、M3 加 `InboxReplier`）不重叠，谁后合谁 rebase。

## 可选：接电脑上已有的 Telegram 服务（先不做）

有的人已经在某台电脑上跑着一个托管了自己账号的 Telegram 客户端（Telethon、TDLib 的 CLI 或服务）。可以像 iMessage 那样走「node 当本地连接器」
（`docs/plan-connectors.md` 第二节第三种）：node 读本机那个服务（只读命令），hub 一侧注册成连接器，凭据和会话一直留在那台电脑上（比如它的钥匙串），不进 hub。
- 好处：不用在 hub 上再登录一次，不多一个 Telegram 设备会话；已经授权过的服务直接用。
- 代价：每种服务的命令和输出各不一样，要一个适配层；只能读那个服务肯给的；那台电脑离线就没有数据；限速由那个服务管。
- 现在不做，先把 jarvisd-telegram 做完；有人要时再按 iMessage 的 `connector.*` RPC 加一个 `telegram` 的本地源。

## 测试

- 不用真账号：Telegram 的调用收在一个小接口后面（`api`：发验证码、登录、密码、会话列表、历史、搜索、退出），单元测试用假的实现（合成的会话和消息，人名用「示例用户」）。
  再试 gotd 自带的 `tgtest`（内存里的 MTProto 服务端）跑一条登录流程，验证真的走了 MTProto 编码；不好用就只用夹具。
- 加密：存进库的是密文（测试里断言库里那一行不含明文 auth key）；退出后那一行没了。
- 隐私扫描照常；日志只记会话 id、条数、错误类型，不记正文、手机号、验证码、密码。
- 真账号的登录由用户自己在 App 里做。之后只在 hub 上看数字（会话数、行数、收件箱条数）。
- **对照真实数据的样子**：用户在一台 Mac 上有一个托管了自己 Telegram 账号的只读服务（Telethon CLI，经本机 unix socket）。只用它的读命令
  （status、dialogs、resolve、messages、search，`--limit` 不超过 200），一次一个请求、间隔 0.5–1 秒、FloodWait 等够，不轮询、不大批量拉；
  **绝不**调用发送类命令。只对照结构（会话类型、群和超级群、回复、@我、静音 / 归档、附件的种类），记录也只写结构：真实的消息、人名、群名、
  手机号、id 不进仓库、夹具、PR、issue 和日志。gotd 用不了它的会话（那是另一份授权）。

## 分步

| 步 | 内容 |
| --- | --- |
| T1 | 框架：`Login`、`Secrets`（`connector_secrets` 表）、`SetEnabled` / `disabled`、hub 的登录 / 退出 / 开关接口（已和 ovis 对过） |
| T2a | 子进程连接器：hub 的 `connector/proc` 和子进程的 `connector/serve`（RPC、版本对齐、退避重启、开关停起），用测试里的假子进程验证 |
| T2b | `jarvisd-telegram`：gotd 客户端、分步登录（凭据 → 手机号 → 验证码 → 密码）、凭据和会话经 hub 加密存、退出、状态（只露 api_id）；personad 不带 gotd 的测试 |
| T2c | 发布和更新：`deploy.sh`、`publish.sh`（编、扫、体积、清单的 `programs`）、hub 自动更新一起换一起回滚、Docker 镜像、`deploy-hub.sh.new` |
| T3 | T3a 知识库归档（会话增量）+ 实时踢；T3b 只读工具 + 提示词；T3c 删除已存数据（知识库、收件箱、游标） |
| T4 | 收件箱 |
| T5 | App：设置里的登录表单、开关、删除、退出；收件箱行的标志；mock hub；两种风格 × 深浅色截图 |

## 用户定的（2026-11-03）

1. **api_id / api_hash**：用户在 App 里登录的第一步自己填（上面「凭据」），不放 hub.json、不编进程序、不放构建配置。
2. **第一次同步**：每个会话最近 30 天、最多 500 条。
3. **收哪些会话**：只收私聊和群（含超级群），不收频道、机器人、Saved Messages；静音和归档的只进知识库、不进收件箱。
4. **账号风险**：用户知道 Telegram 的「设备」里会多一个会话、会遇到限速；会话加密存，只读用。
5. **体积**：选 B，单独一个程序（上面「架构」）。
