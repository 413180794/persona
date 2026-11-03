# 连接器

连接器是 Persona 接一个数据来源的统一方式：GitHub、信息（iMessage）、Telegram、飞书、邮件各是一个连接器，hub 的几条管道
（助手的工具、写操作的闸门、收件箱、知识库、设置 › 连接器 的状态、实时事件）按连接器提供了什么自动接上，加一个来源不用改这些管道。

这份文档分三部分：

- **一、现在的样子（v1）**：飞书、邮件的迁移（M0–M6）和 GitHub、信息、Telegram 已经合入的部分做完以后的实际结构，加新连接器照这个做。
- **二、v2 设计**：对 v1 的 8 条意见，逐条写同意 / 不同意、怎么改、改动多大、动哪些连接器、新旧怎么兼容。
  **2026-11-03 已拍板**（结果在第四部分，正文已按拍板改好）；V1、V3–V6 已做完，V2、V7 决定不做，v2 已收尾。
- **三、迁移顺序**，**四、拍板结果**。

过程和历史（为什么这样拆、每一步的验收）在 `docs/plan-connectors.md`；Telegram 的在 `docs/plan-telegram.md`。

## 一、现在的样子（v1）

### 有哪些连接器

| id | 名字 | 怎么跑 | 知识库 | 收件箱 | 助手的工具 | 写操作 | 登录 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `github` | GitHub | hub 里（`connector/github`） | `Archive`（`updated_at` 游标） | 有，规则定档 + 降噪（`rules.go`） | `github_search/read/diff/checks` | `github_comment`、`github_review`（过闸门） | 读某台电脑上 `gh` 的登录（`credential.gh_token`） |
| `imessage` | 信息 | 读在 Mac 的 node 上，hub 里是个转 RPC 的适配（`connector/imessage`） | `Archive`（ROWID） | 有 | `imessage_chats/read/search` | 没有 | 没有（给 Mac 上的 personad 完全磁盘访问权限） |
| `telegram` | Telegram | 单独的程序 `jarvisd-telegram`，hub 经 stdin / stdout 调（`connector/proc` + `connector/serve`） | `Archive` | 有 | 只读工具；`tg` 命令行的读（`Unlisted`） | `telegram_send/forward/react/mark_read`（`tg` 命令行，`Unlisted`，过闸门） | `Login` 分步（手机号、验证码、两步验证密码），会话加密存 `connector_secrets` |
| `lark` | 飞书 | hub 里（`hub/lark_connector.go`，包住原来的 lark-connector 代码） | `Chats`（每个会话一个游标）+ `Items`（文档、会议、日程） | hub 自己的收件箱块（`inbox_own.go`） | `lark_chats/messages/search` | 走 lark-connector skill + node 的 Bash 闸门（规则见下） | 在 hub 机器的 lark-connector 里登录 |
| `mail` | 邮件 | hub 里（`hub/mail_connector.go`） | `Archive`（每个账号的 UID） | hub 自己的收件箱块 | `mail_accounts/search/read` | `mail_send`（过闸门） | 自己的接口：密码 / Gmail、Outlook 的 OAuth（`/api/v1/mail/…`） |

### 接口：一份清单，按清单分派

`server/internal/connector/connector.go`：`Connector` 有 `ID()`、`Name()`、`Manifest()`。清单（`manifest.go`，v2 · V1 起）一处写明它能做什么：

| 清单的字段 | 意思 |
| --- | --- |
| `Capabilities` | 有类型的能力（`CapStatus`、`CapTools`、`CapActions`、`CapEvents`、`CapInbox`、`CapSettle`、`CapArchive`、`CapChats`、`CapItems`、`CapLogin`、`CapOptions`），每个对应一个接口 |
| `Hosts` | 链接所在的主机名（App 认链接） |
| `AgentTools` | 哪些工具也给 Agent（不只给助手），按名字的通配（邮件是 `mail_*`） |
| `Unlisted` | 哪些工具和写操作不进模型的工具列表，按名字的通配：连接器自己的前端按名字调（Telegram 的 `tg` 命令行和 skill，`docs/plan-telegram.md`「tg 命令行」）。不是权限 |
| `Poll` | 多久问一次 `Archive`（0 是 hub 的默认；邮件 6 分钟） |
| `Kinds` | 收件箱条目的种类；每个种类在 `prompts/inbox/kinds/<id>.md` 里要有自己的说明（`connector.CheckKinds`，各连接器的 `TestKindsAreDescribed`），收到没声明的种类记一行日志 |
| `Proxy` | 它替一个程序回答（子进程的 `proc.Conn`）：实现了全部接口，只有声明的那些是真的 |

hub 只用 `connector.As[接口](c)` 找能力：清单声明了才去断言。`Registry.Add` 用 `connector.Check` 检查清单：声明了没实现、实现了没声明（`Proxy` 除外）、
不认识的能力、种类却没有收件箱，都在注册时报错。各能力的接口：

| 能力 | 接口 | 干什么 | 谁有 |
| --- | --- | --- | --- |
| `status` | `Accounts` | `Status()`：状态、账号、出错时给人看的一句话和 `Guide` | 除飞书、邮件外都有（没有就当 `ok`；`Proxy` 的状态总是问它自己） |
| `tools` / `actions` | `ReadTools` / `Actions` | 只读工具；写操作（`Describe` 出卡片上的话、参数不对时返回错误不弹卡片，`Do` 真去做） | 见上表 |
| `options` | `Options` | 连接器自己的设置（JSON），App 里改 | GitHub |
| `events` | `Events` | 实时事件，`changed(Scope)`；`Scope` 是有类型的常量，子进程报来的先校验 | 各有各的 |
| `archive`、`chats`、`items` | `Archive`、`Chats`、`Items` | 进知识库的三种形状 | 见上表 |
| `inbox`、`settle` | `Inbox`、`InboxSettler` | 收件箱的候选和回复（`Reply` 拿到整条 `InboxReply`：会话、条目、回的是哪一步、正文）；按规则收掉旧的 | GitHub、信息、Telegram；`settle` 只有 GitHub |
| `login` | `Login` | 分步登录 | Telegram |

`Runner`（自己的循环）、`Machines`（某台电脑连上了）是怎么跑的，不是能力，照旧按类型断言。收件箱的档是 `connector.Tier`（`TierAction`、`TierFYI`、`TierNoise`），
hub 收到不认识的档就丢掉、交给模型分拣。

`Registry`（`registry.go`）持有全部连接器：按状态发工具（只有 `ok` 的给）、按前缀分发调用、开关（`SetEnabled`）、启动 `Run` / `Watch`（失败退避重启）。

### 三种跑法

- **在 hub 里**：Go 代码直接实现接口。
- **子进程**（`connector/proc` ↔ `connector/serve`）：第一方、只是依赖太大不想编进 personad 的（Telegram 的 gotd）。hub 起程序，stdin / stdout 上跑 hub↔node 那套
  JSON-lines RPC（`internal/rpc`），方法一一对应接口（`serve.go` 的 `init`、`status`、`archive.changes`、`inbox.collect`、`tools.call`……），
  程序反过来调 hub 的 `secrets.*`。hub 注册时就给 `proc.Conn` 一份静态的清单（`proc.Config.Manifest`；Telegram 的在不依赖 gotd 的
  `connector/telegram/spec`，程序和 hub 共用），所以从没启动过、默认关着的连接器也报得出能力、能登录和退出（#666）；`init` 回的程序清单
  （`serve.Manifest`，线上的字段是 snake_case）拿来核对，不一致记日志、仍用静态的。程序必须和 hub 同一次构建（`version -v` 一字不差）；协议有版本号（v2，`docs/connector-protocol.md`）。
- **node 中转**：数据只在某台 Mac 上（`chat.db`）。node 上有一个读本地数据的 `Reader`，hub 经 node 的 RPC（`connector.list/chats/older/changes/search`，
  `node/node.go` 里写死的 5 个方法）去读，hub 一侧的 `imessage.Remote` 才是连接器。

### 闸门

- 连接器的写操作只有 `Actions` 一条路：`Registry.Call` → `connector/gate.go` 的规则（hub.json `connector_gate`，第一条匹配的说了算）→
  `allow` 直接做、`deny` 拒绝、`confirm`（默认）在手机上弹卡片（`hub.approve`，15 分钟过期）。卡片写明做什么、全文、谁会看到。
- 飞书的写操作不是工具：助手用 lark-connector skill 的 `ol` / lark-cli，node 的 Bash 钩子（`node/larkgate.go`）读命令判断读写，规则是
  `connector_gate` 里 `connector:"lark"` 的（`action` 写「方法 路径」）加上老的 `lark_gate`，再加内置规则（M5）。
- 收件箱里点「发送」本身就是确认，`Reply` 不再弹卡片。

### 进收件箱

连接器给 `InboxThing`（一件事：`Thread` 是它的键，`Kind` 是它自己的种类，`Tier` / `Why` 是它的规则定的档，`Done` 是它看到这件事结束了）；
hub（`hub/inbox_connectors.go` 的 `applyConnector`）把同一个 `Thread` 的后续动作并进一个打开的条目，之后的分拣、AI 分身、处理完、保留多久都是 hub 的，
所有来源一样。条目用一个通用块（`InboxConnector`），App 不用认识来源就能画。

### 进知识库

- `Archive`：一个账号一个游标，`Changes(cursor)` 给出变了的会话，hub 把游标存在 `cursors/<id>.json`，会话 id 是 `<连接器 id>-<会话 id>`。
- `Chats`：飞书这种每个会话自己有游标、能往回补（`kb.Backfiller`）、有附件的，直接给 `kb.Source`，归档、回补、压缩、打标照旧。
- `Items`：飞书的文档、会议、日程。

### 设置 › 连接器

`GET /api/v1/connectors?all=1`（不带 `all` 时不列飞书和邮件，给 M6 以前的 App），一项是 `{id, name, status, account, via, error, guide, capabilities, hosts, enabled, options, archive, inbox}`。
`archive` / `inbox` 是「进知识库」「进收件箱」两个开关（`Registry.SetFeed`，默认开，存在 hub 设置里）：关掉「进知识库」后 hub 不再调 `Archive`，「删除已存的知识库内容」（`DELETE …/{id}/archive`）只删知识库里它的会话和人；关掉「进收件箱」后不再 `Collect`，已有的条目不动。连接器本身照常跑，工具照常给。
App 用这份列表画整节：飞书、邮箱画成原来的行（状态还读各自的接口），信息有自己的页，其余走通用的连接器页。接口细节见 `docs/protocol.md`「连接器 API」。

### 加一个连接器（v1 的做法）

1. `connector/<id>` 一个包，实现 `Connector`（含 `Manifest()`）和清单里声明的能力；工具的描述放 `prompts/mcp/<id>/`，收件箱的种类说明放
   `prompts/inbox/kinds/<id>.md`，种类写进清单，加一个 `TestKindsAreDescribed`。
2. hub.json 加一节配置，`hub/connectors.go` 的 `registerConnectors` 里注册。
3. 写操作只做成 `Actions`，`Describe` 的卡片写明谁会看到。
4. 状态 `Status` 只从内存回答（每次列工具都要问），出错时 `Error` 一句话、能自己修的给 `Guide`。
5. 日志、错误里不出现正文、号码、token。

## 二、v2 设计

对 v1 的评价是「能用、文档好，但不够优雅」。下面 8 条逐条看代码后的判断。改动大小按「小：一两个 PR、几百行，不碰 App」
「中：三到五个 PR，可能碰 App」「大：跨 hub / node / 程序 / App，要分好几步」估。

### 1. 能力的声明：一份显式的清单（Manifest）

**同意。**

现状：可选能力有 19 个接口（`Accounts`、`ReadTools`、`Actions`、`Options`、`AgentTools`、`Validator`、`Events`、`Runner`、`Machines`、`Hosts`、
`Poller`、`Chats`、`Items`、`Archive`、`Inbox`、`InboxReplier`、`InboxSettler`、`Login`、`Capabler`）。问题不只是多：

- 两套声明。hub 里的连接器靠「实现了哪个接口」，子进程靠 `Capabler` 自己报；而 `proc.Conn` 为了能代表任何程序，把能用 RPC 转的方法全实现了，
  hub 的类型断言对它永远成立，正确性靠程序「没有就回空」。`Capabilities()`（`registry.go:309`）只报得出其中 11 个，
  `AgentTools`、`Validator`、`Poller`、`InboxReplier`、`InboxSettler` 这些在 `GET /api/v1/connectors` 里看不到。
- 有几项其实是数据，不是行为：`Hosts`、`AgentTools`、`Poller` 各是一个返回不变的值（常量或配置）的方法。

方案：

```go
type Connector interface {
    Manifest() Manifest // 启动时读一次；子进程在握手时报同一个结构
}

type Manifest struct {
    ID, Name     string
    Protocol     int          // 连接器协议的版本（第 4 条）
    Capabilities []Capability // 有类型的常量：CapStatus、CapTools、CapInbox、CapChanges、CapLogin、CapOptions…
    Hosts        []string
    Tools        []ToolSpec   // 每个工具：名字、只读还是写、给不给 Agent（取代 AgentTools，粒度到工具）
    Inbox        *InboxSpec   // 种类（名字、标签、默认档）、能不能回复、会不会自己收旧条目
    Changes      *ChangesSpec // 多久问一次（取代 Poller）、能不能往回补、有没有附件
    Login        *LoginSpec   // 登录的方式（第 7 条）
}
```

- 类型断言只用来**分派**：清单里声明了 `CapInbox`，hub 才去断言 `Inbox` 接口；声明了却没实现、实现了却没声明，`Registry.Add` 直接报错（启动时就发现，不是运行时回空）。
- 接口合并到 7 组：`Status`（必选，给一个嵌入用的 `connector.Base` 默认 `ok`）、`Run`（合并 `Runner` + `Events` + `Machines`：`Run(ctx, Host)`，
  `Host` 给 `Changed(scope)`、`MachineOnline` 订阅）、`Tools`（`ReadTools` + `Actions`，`Describe` 能回错误，取代 `Validator`）、`Inbox`、`Changes`（第 2 条）、`Login`、`Options`。
- `GET /api/v1/connectors` 的 `capabilities` 从清单来，现有的名字不变，App 不用改。

改动：**小到中**。机械地改 5 个连接器和 hub 的分派，MCP / 收件箱 / 知识库的 golden 必须逐字节不变。

### 2. 进知识库：统一成「按游标取变化」

**不做（2026-11-03 拍板，V7）。** 看完代码以后的结论：

- GitHub、邮件、信息、Telegram 四个 `Archive` 已经是同一种「按游标取变化」，统一它们只剩改名。
- 飞书的 `Items`（文档、会议、日程）是每次全量的快照，不是增量；硬并进游标接口还得另开一个按链接查文档标题的口子。
- 飞书的 `Chats`（每个会话一个游标、往回补、下附件）就是知识库的归档引擎本身，包一层适配器只是把代码挪个地方，
  对外没有任何变化，却是在最大的来源上风险最高的一步。

所以三种形状照旧：`Archive`、`Chats`、`Items`（「一 · 进知识库」）。下面是当时的提案，留作记录。

当时的判断：**大部分同意；往回补和附件不并进去。**

现状三种形状加一个节奏：`Archive`（GitHub、邮件、信息、Telegram，已经是「按游标取变化」）、`Chats`（飞书和removed，每个会话一个游标，
知识库引擎自己挑会话、往回补、下附件）、`Items`（飞书的文档、会议、日程，每次给全量快照）、`Poller`（只是节奏）。

方案：

```go
type Changes interface {
    // cursor 是连接器自己的（一个数、每个账号一个数、每个会话一个数都行），hub 原样存、原样还。
    Changes(ctx context.Context, cursor string, limit int) (Batch, error)
}
type Batch struct {
    Chats    []ChatBatch // 会话的新消息（同现在）
    Items    []kb.Item   // 文档、会议、日程（取代 Items）
    Next     string
    More     bool
    NextPoll time.Duration // 取代 Poller；0 用清单里的
}
```

- 已经是 `Archive` 的 4 个连接器：只改名和返回结构。
- `Items`：并进 `Batch.Items`；`DocTitle`（按链接查文档标题）不是「变化」，留成一个可选的查询（以后并进 v1 计划里的「链接」）。
- 飞书的会话：用一个适配器 `connector.PerChat(kb.Source)` 把「每个会话一个游标」包成一个复合游标，引擎挑会话的逻辑搬进适配器。
  **飞书是最大的来源，这一步风险最高，放最后**，必须过 M0 的对照基线（`hub/connector_baseline_test.go`）和真实数据对照。
- **不并进去的**：往回补（读更早的历史，方向和「从上次到现在」相反）留成可选的 `Backfill`；附件（`Download`）留成可选的 `Media`。
  硬塞进 `Changes` 会让游标同时表示两个方向，更难懂。

改动：**中**（4 个连接器几乎机械）**到大**（飞书）。removed跟飞书一起走。

### 3. 打补丁式的接口和随手写的字符串

**同意，`Kind` 例外。**

- `InboxReplier`：并进 `Inbox.Reply(ctx, InboxReply)`，一个签名。5 个连接器都改；子进程的 `inbox.reply` 只多几个字段，兼容。
- `AgentTools`：变成清单里每个工具的 `ForAgents`（第 1 条）。现在是整个连接器一起给，粒度太粗。
- `Validator`：并进 `Describe` 返回的错误。
- `scope`：已经有常量，但子进程、node 传过来的是字符串。改成有类型的 `Scope`，在边界上校验，认不出的记一行日志丢掉。
- 收件箱的档（`"action"`、`"fyi"`、`"noise"`）：连接器包里没有常量，Telegram、信息、GitHub 各写各的字符串。加 `TierAction` 这几个常量。
- **`Kind` 不做成全局枚举**：它本来就该是连接器自己的词（`review_requested`、`dm`、`ci_failed`），模型靠 `prompts/inbox/kinds/<id>.md` 理解。
  改成在清单里声明（名字、给人看的标签、默认档），hub 遇到没声明的种类就报错，再加一个测试，检查每个声明了的种类都在提示词文件里有说明。

改动：**小**。

### 4. 一个有版本号的协议，在进程里的只是它的一个适配

**同意要有版本号、要统一、要写规范；不同意内置连接器在生产里也走一遍 JSON。**

现状：三种跑法三套约定。

- 子进程：没有版本号，靠「同一次构建」。字段大多没有 JSON 标签，线上的键是 Go 的字段名（`Cursor`、`Batches`），第三方很难照着写。
- node 中转：只为 iMessage 写的 5 个方法，`node/node.go:370` 写死。
- 第三方：`plan-connectors.md` 的「外部进程」一节设计过 HTTP + MCP，没实现。

方案：

- **协议 v2**：JSON-RPC 风格的一行一条消息（沿用 `internal/rpc`，已经在 hub↔node、hub↔子进程上跑着），字段一律 snake_case，方法和第 1–3 条的接口一一对应：
  `initialize`（双方报 `protocol` 版本，hub 支持当前和上一版）→ 程序回清单 → `status`、`changes`、`inbox.collect`、`inbox.reply`、`tools.call`、`action.describe`、`action.do`、`login.step`……；
  程序调 hub：`secrets.*`、`changed`。错误有固定的 code（`not_signed_in`、`rate_limited`、`unknown_method`…）。
- **三种传输共用这一套**（已拍板，第三方也用这一套，不走原计划的 HTTP + MCP）：stdin / stdout（第一方子进程、第三方本地程序）；经 node 中转（hub 发 `connector.call {id, method, params}`，node 原样转给它那边的连接器，
  iMessage 的 `Reader` 变成 node 上跑的一个协议 v2 连接器，不再是写死的 5 个方法）；常驻服务的 WebSocket（第三方的服务自己跑着，hub 去连，和 `docs/daemons.md` 的连法一样）。
- **在进程里的**（已拍板）：生产上照旧直接调 Go 接口（类型检查、不复制大批量的消息）。但测试里有一个「过一遍 JSON」的适配器：同一套**一致性测试**
  （`connector/conformance`）对每个内置连接器跑两遍（直接调 / 编码再解码），保证「在进程里的就是协议的一个实现」这句话是被测试守着的，而不只是写在文档里。
- **工具描述**：第三方的工具描述不在本仓库的 `prompts/`，这是「提示词一律放 prompts/」的明文例外（`plan-connectors.md` 已写）。
  第三方能不能直接挂现成的 MCP server 当「只有读工具的连接器」：可以作为协议的一个可选能力（`tools` 走 MCP），不作为主协议，
  因为 MCP 没有游标、收件箱、登录这些。
- **给第三方的规范**：单写 `docs/connector-protocol.md`：握手、清单、每个方法的参数和结果、错误码、版本规则，加上安全的硬性规定（写操作只能是声明过的 action、
  由 hub 过闸门后调；密钥只能存 hub 给的 `secrets`；日志不写正文和号码；`status` 不联网），以及一致性测试怎么对自己的程序跑。草案的要点就是上面几条。

改动：**大**。Telegram（子进程，跟 personad 一起发，同时升级，最简单）→ 信息（node 上要换实现，旧 node 照旧走 5 个方法）→ 第三方的规范和一致性测试。
飞书、邮件、GitHub 在 hub 里，只跑一致性测试。

### 5. 收件箱条目带结构化属性，合并、静音、自动关闭由 hub 统一做

**同意合并键、发送者种类、标签、取代关系由 hub 统一处理；不同意「怎样算自动解决」写成声明式条件。**

现状：降噪全在 GitHub 连接器里（`connector/github/rules.go`）：CI 按「仓库 + 分支」合并（把 `Thread` 本身做成 `ci:<仓库>@<分支>`）、
认机器人（`isBot`）、静音仓库和忽略分支（GitHub 自己的 `Options`）、后来成功 / 分支删了 / PR 合了就关（`followUpCI` + `Settle`）。
Telegram、信息各自又写一遍「群聊算知悉即可」「机器人不进收件箱」。换一个来源就要再写一遍，App 里也只有 GitHub 能静音。

方案：`InboxThing` 加这些字段：

| 字段 | 意思 | hub 统一做什么 |
| --- | --- | --- |
| `Group` | 合并键（不填就是 `Thread`） | 同一个 `Group` 的打开条目合成一个；`Thread` 只管「回复发到哪」 |
| `From {Name, Kind, Handles}` | 最新一步是谁做的；`Kind` 是 `person` / `bot` / `self` / `system`；`Handles` 见第 6 条 | 规则：机器人默认「知悉即可」，自己的动作不算新消息 |
| `Tags map[string]string` | 筛选和静音用的属性（`repo`、`branch`、`branch_role`、`chat`、`sender`） | 静音规则按标签匹配（支持通配），存在 hub；App 的条目菜单「静音这个仓库 / 会话 / 发件人」对所有来源都一样（已拍板，新界面先截图） |
| `Supersedes []string` | 这件事取代了哪些 `Group`（CI 后来跑成功了：成功的那条取代失败的分组） | 被取代的打开条目收掉，原因写明 |

「怎样算自动解决」保持由连接器观察、上报：分支删了、后来跑成功、PR 合了，hub 不调 GitHub 的接口就判断不了，写成声明式条件也只能让连接器去查。
所以只把上报的方式统一成两条：推（`InboxThing.Done` + `Why`，看到就报）和拉（`InboxSettler.Settle` 改名 `Check(openGroups)`，
hub 定时问还开着的分组结束没有）；静音和规则变了以后的收尾由 hub 自己做，不再需要连接器的 `Settle`。

**GitHub 是第一个用的**：`muted_repos` → 标签 `repo` 的静音规则；`ignore_branches` → 标签 `branch` 的静音规则（通配）；`isBot` → `From.Kind`；
CI 的「仓库 + 分支」→ `Group`；后来成功 → `Supersedes`；CI 模式（关 / 默认分支 / 加上我的 PR / 全部）用标签 `branch_role`（`default` / `mine` / `other`）
在 hub 里判断，App 的 GitHub 设置页不变（只是存的地方从 GitHub 的 options 变成 hub 的静音规则；老的 `github.inbox` 配置第一次启动时转过去）。
验收：用 #584 时的真实数据只读重放，「需要你」还是 13 条，前后一致。

之后：Telegram、信息改用 `From.Kind` 和 `Tags`（删掉各自的机器人 / 群聊判断）；飞书、邮件的收件箱还是 hub 自己的块，
等它们改用 `InboxThing`（v1 没做，是另一件事）再接上。

改动：**中**。hub 的收件箱、GitHub、Telegram、信息；App 的「静音」菜单是新界面，单独一个 PR，截图给你看。

### 6. 发送者带身份线索

**同意，v2 只记录不合并；线索只存 HMAC 哈希，不给模型看，只用来认人（已拍板）。**

现状：知识库的人（`kb/store.go:71` `Person`）只有一个 `Source` + `SourceID`，同一个人在飞书、邮件、信息、Telegram 里各一份。
信息为了不让号码落盘，人的 id 已经是号码 / 邮箱的哈希（`imessage/jarvis.go:92` `PersonKey`）。

方案：

- `Handle{Kind, Hash}`：`Kind` 是 `email` / `phone` / `github` / `telegram` / `lark` 这些；`Hash` 是规范化之后（邮箱小写、手机号 E.164）用 hub 自己的密钥做的 HMAC。
  同一个邮箱在邮件和信息里得到同一个哈希，能认出是同一个人，又不需要把号码写进知识库。
- 放在 `InboxThing.From.Handles` 和 `kb.SourceMessage.SenderHandles`；`Person` 加 `Handles []string`（只增不删）。
- **不给模型看**：MCP 工具的结果、收件箱分拣和知识库压缩的提示词里都不带线索（哈希对模型没有意义，还会白占上下文），golden 守着；
  原值（号码、邮箱）照旧不落盘。
- 各来源能给什么：邮件（地址）、信息（号码 / 邮箱，本来就是它的 id）、GitHub（登录名；邮箱拿不到就不给）、Telegram（用户 id；通讯录里的人才有号码）、
  飞书（用户 id；有权限时加邮箱）。
- **认人（把几份合成一个人）不在 v2**：拿到线索以后单独设计，包括合错了怎么拆、谁来确认。

改动：**小**（各连接器加字段）**到中**（知识库写入）。5 个连接器都动一点。

### 7. 登录支持「打开网页去授权」

**同意。邮件要先支持多账号，放在后面。**

现状：只有 Telegram 走 `Login`（填字段的分步）。Gmail、Outlook 是邮件自己的 `/api/v1/mail/oauth/{google,microsoft}/start|finish`；
GitHub 读某台电脑上 `gh` 的登录。App 里还没有通用的连接器登录页（Telegram 的 App 侧正在做）。

方案：`Prompt` 除了填字段，再加两种步骤：

- `open_url`：`{url, callback_scheme}`。App 用系统的网页登录（`ASWebAuthenticationSession`）打开，拿到回调地址后作为这一步的值交回（`callback_url`）。OAuth 都是这种。
- `device_code`：`{url, user_code, interval}`。App 显示验证码、打开网址，然后每隔 `interval` 秒问一次同一步，直到 `done`。GitHub 的设备码登录是这种。

于是 App 只做**一个**通用登录页（填字段、打开网页、显示验证码三种步骤），Telegram、Gmail、Outlook、GitHub 都用它。

- **邮件**：一个连接器下面有好几个账号，现在的 `Login` 是一个连接器一份会话。要加「账号」这一层（`login.step` 带上 `account`，空的是加一个新账号；`logout` 指定账号）。
  老的 `/api/v1/mail/oauth/*` 留给老 App。
- **GitHub**：设备码登录要一个 GitHub OAuth App 的 client ID（不需要密钥）。**用户自己填**（已拍板）：和 Telegram、Outlook 一样，
  凭据由用户自己注册、自己填，不写进程序。client ID 存 hub（和别的连接器设置一样），登录的第一步没有就先问它。
  「读某台电脑上的 gh 登录」作为另一种方式保留。

  注册 GitHub OAuth App（设置页和这份文档都写这几步）：

  1. GitHub 右上角头像 › Settings › Developer settings › OAuth Apps › New OAuth App。
  2. Application name 随便填（如「Persona」）；Homepage URL 填任意一个你的网址；Authorization callback URL 设备码登录用不到，填同一个网址即可。
  3. 创建后在这个 App 的页面勾上 **Enable Device Flow**，保存。
  4. 复制页面上的 **Client ID**（不要生成 client secret，设备码登录不需要），填进 App 的 GitHub 登录页。
  5. 第一次登录时 GitHub 会列出 Persona 要的权限：只有 `repo`（读写仓库，包括私有仓库的 PR、issue、评论、review、CI 和日志，也包括通知），确认即可；
     以后要撤销在 Settings › Applications › Authorized OAuth Apps。

改动：**中**。hub 的登录接口、App 的通用登录页（新界面，截图给你看）、邮件的多账号、GitHub 的设备码。

### 8. 写操作只以工具提供、必须过闸门；skill 不加额外闸门

**连接器的写操作照旧以工具过 `connector_gate`；skill 不加额外闸门，交给 Claude Code 自己的权限判断（2026-11-03 拍板，V2 不做）。**

现状（不变）：

- 连接器的写操作都是 `Actions`，都过闸门（`github_comment`、`github_review`、`mail_send`）。
- 飞书的写操作是例外：lark-connector skill 的 `ol` 和 lark-cli，node 的 Bash 钩子按命令判断读写（M5 后规则也写在 `connector_gate`），不管权限模式都要确认。
- 用户自己放进 `~/.persona/skills` 的 skill 都会链接进助手的目录；助手调这些 skill 的脚本走 Claude Code 自己的权限流程。

曾经的提案是把 `larkgate.go` 的「按命令判断」推广到所有 skill（skill 在 `jarvis.json` 里声明写脚本，执行时手机上确认，#654 / #657）。
用户的决定：skill 写操作的鉴权 Claude Code 自己已经有了，Claude 会自己判断，需要用户判断的时候会问用户，「这块千万别过度设计」。所以不做，代码没有合入。

要写就做成连接器的 action 这条照旧：Telegram 的发送、回复、转发、reaction、标已读做成了 `telegram_send` 等 actions（#988），
经 `tg` 命令行调，过闸门，卡片写明发给谁、全文；子进程协议为此加了 `actions.*`（`docs/connector-protocol.md`）。

## 三、迁移顺序

每一步一个 issue 一个 PR，外部行为不变的步骤都要过现有的 golden（MCP 工具、收件箱、知识库的 M0 基线），改界面的步骤截图给你看。

| 步 | 内容 | 对应 | 大小 | 动哪些 | 兼容 |
| --- | --- | --- | --- | --- | --- |
| V1 ✓ | 清单（`Manifest`）+ 按清单分派（`As`、`Check`）；档、scope 的常量；`Capabler`、`Hosts`、`Poller`、`AgentTools`、`Validator`、`InboxReplier` 并掉；种类在清单里声明并对照提示词。可选接口从 19 个减到 11 个能力接口加 `Runner`、`Machines`。**没做的**：`Runner` + `Events` + `Machines` 并成 `Run(ctx, Host)`（会把 hub 的 `Watch` 退避重启搬进各连接器，对外有变化，和 V5 的协议一起做）；`ReadTools` + `Actions` 两个能力保留（清单分得开读写，合并没有好处）；`Settle` 在 V3 改，知识库的三种在 V7 并 | 1、3 | 小到中 | 5 个都动（机械） | 对外不变：`GET /api/v1/connectors` 的能力和 MCP 工具的 golden 一致，真实 hub 的列表前后对照 |
| ~~V2~~ | ~~skill 的命令闸门~~：**不做**（2026-11-03 用户决定：交给 Claude Code 自己的权限判断，见第 8 条；#654 关闭） | 8 | — | — | — |
| V3 ✓ | 收件箱的静音由 hub 统一做（#670）：`InboxThing.Tags`（`repo`、`chat`、`sender`、`branch`），所有来源（飞书、邮件、GitHub、信息、Telegram）按「来源 + 标签 + 值」静音，收集时收掉被覆盖的；App 每一行的长按菜单「静音这个仓库 / 会话 / 发件人」和「更多 › 已静音」；GitHub 的 `muted_repos` 变成静音（启动时搬过去，设置页照旧）。**没做**：`Group`、`From`、`Supersedes`——按 `Thread` 合并本来就是 hub 统一做的，GitHub 的 CI 后来成功、分支删了、PR 合了也已经由它自己报 `Done`，等有连接器真要用再加，不过度设计 | 5 | 中 | GitHub、Telegram、信息、hub 收件箱、App | 新字段和新接口；老 App 不画菜单、GitHub 设置页照旧；条目多了 `mutes`，连接器块多了 `tags`（wire 样例已更新） |
| V4 ✓ | 身份线索（只记录，#687）：连接器在消息上带发送者的原始线索（只在内存），知识库用 hub 的密钥（`~/.jarvis/handles.key`）做 HMAC 后存进人的 `handles.json`，不进 `meta.json`、模型看不到；5 个连接器都给（邮件地址、信息的邮箱 / 号码、GitHub 登录名、Telegram 用户、飞书用户 id）。**没做**：收件箱条目上的 `From.Handles`（还没有用的地方）、飞书的邮箱（要额外权限） | 6 | 小 | 5 个都动一点、知识库 | 新文件，老数据不动；`meta.json` 和 golden 不变 |
| V5 ✓ | 协议 v2（#699）：线上字段一律 snake_case，`init` 和清单带 `protocol`（现在是 2，不一致就停），固定的错误码（`not_initialized`、`no_capability`、`unknown_method`、`wrong_protocol`）；规范写在 `docs/connector-protocol.md`；一致性测试 `connector/conformance` 把 GitHub、信息、Telegram 放到协议的程序一侧，逐个能力比直接调和经协议调的结果，`TestWireTypesAreTagged` 检查线上字段都有名字。**没做**：信息的 node 侧换成协议 v2（node 上照旧是写死的 5 个方法，能用，等要动 node 时再换）、常驻服务的 WebSocket 传输（等第一个外部连接器） | 4 | 中 | Telegram、hub 的子进程接法、测试 | 子进程和 hub 同一次构建一起升级；node 不动 |
| V6 ✓ | GitHub 的登录在 App 里选（#704）：**从哪里登录**——列出各台电脑和它们 gh 的登录（账号、权限），用户选一台；或者**设备码登录**（不依赖电脑，填一次自己 OAuth App 的 Client ID，App 显示验证码、自动等授权）。选择存在 hub 设置里（`github_auth`），hub.json 的 `github.auth.from_machine` 只是默认值；换一个就是退出上一个（选电脑时忘掉设备码的 token），所以 GitHub 没有单独的退出登录。**权限**：连接器只用 `repo`（读私有仓库的 PR / issue / 评论 / CI 和日志、搜索、评论和 review，也包括通知；不碰组织和团队，所以不要 `read:org`），设备码登录只申请 `repo`；用的 token 少了 scope（GitHub 的 `X-OAuth-Scopes`）时页面写明少了什么、怎么补（`gh auth refresh -s repo` 或改用设备码）、哪些功能受影响。登录步骤多一种 `device_code`。**没做**：`open_url` 步骤（现在没人用，邮件的 OAuth 照旧）、邮件多账号 | 7 | 中 | GitHub、hub、App | hub.json 照旧生效（没选过时）；老 App 看不到选择区 |
| ~~V7~~ | ~~进知识库统一成 `Changes`~~：**不做**（2026-11-03 用户决定，理由见第 2 条：四个 `Archive` 已经同形，`Items` 是全量快照，飞书的会话就是知识库引擎本身）；#731 | 2 | — | — | — |
| — | 第三方规范 `docs/connector-protocol.md`，等第一个外部连接器再实现 WebSocket 传输 | 4 | — | — | — |

顺序的理由：V1 便宜、后面都要用；V3 是用户最能感觉到的（每个来源都能静音、降噪）；
V5–V7 大而收益在内部，放后面，飞书的会话最后。

**收尾（2026-11-03）**：V1、V3、V4、V5、V6 做完；V2（skill 闸门）和 V7（统一进知识库）用户决定不做。连接器 v2 到此结束。

## 四、拍板结果（2026-11-03）

1. **整体方向和 V1–V7 的顺序**：同意。V1、V7 对外不变，用 golden 和真实数据对照守住。后来 V7 改为不做（见第 2 条）。
2. **第 4 条**：内置连接器生产上照旧直接调 Go 接口，只在测试里过一遍 JSON，一致性测试守住。
3. **第 4 条**：第三方连接器用「一行一条 JSON 的 RPC，走 stdin / stdout 或 WebSocket」，和子进程、node 是一套；不走原计划的 HTTP + MCP。
4. **第 5 条**：收件箱每一行的菜单加「静音这个仓库 / 会话 / 发件人」，所有来源一样。是新界面，做的时候先给截图。
5. **第 6 条**：身份线索只存 HMAC 哈希，不给模型看，只用来认人。
6. **第 7 条**：GitHub 设备码登录用户自己填 client ID（和 Telegram、Outlook 的凭据一样由用户自己填，不写进程序），注册步骤见第 7 条。
7. **第 8 条**：先定的是「skill 闸门默认放行，只拦声明为写的命令，只管助手」；随后改为**不做 skill 闸门**：skill 写操作交给 Claude Code 自己的权限判断，Claude 会自己判断、需要时问用户，不要过度设计。连接器的写操作照旧以工具过 `connector_gate`。
