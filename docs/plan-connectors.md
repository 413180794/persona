# 计划：连接器（先接 GitHub 和 iMessage）

状态：进行中。用户 2026-11-01：「派个车道先写连接器的计划，先接 GitHub 和 iMessage」；2026-11-02 开始做：先做框架（C1），再接 GitHub，再接 iMessage（iMessage 交给 terminal 车道，#449），
飞书和邮件的迁移（M0–M6）放后面。还没拍板的两项先按保守的默认值做，留好开关（见「需要用户拍板」2、7）。
用户对本计划点头后按「分步计划」做，做完的步骤打勾；全部做完后把设计部分并进 `docs/connectors.md`（新）、`docs/knowledge.md`、`docs/inbox.md`，删掉这份计划。

## 背景：现在各接各的

| 管道 | 飞书 | removed | 邮件 |
| --- | --- | --- | --- |
| 知识库归档 | `larkSource`（`kb.Source` + `kb.Backfiller`，`hub/kb.go`） | `momentsSource`（同上，`Older` 是空的） | `mailSource` 是空壳；真正的归档是 `hub.kbMail` 自己一个循环，游标在 `mail-cursors.json`，调 `kb.Engine.Ingest` |
| 文档 / 会议 / 日程 | `larkItems`（`kb.ItemSource`） | — | — |
| 收件箱 | `collectLark`、`collectDocs`（`inbox.go`、`inbox_docs.go`） | `collectMoments`（`inbox_moments.go`） | `collectMail`（`inbox.go`） |
| 收件箱条目 | `Source: "lark"` 写死，`Lark` / `Doc` / `Moment` 各一个块 | 同左 | `Source: "mail"`，`Mail` 块 |
| 回复 | `replyInbox` 里按 `Moment` / `Doc` / `Lark` / `Mail` 哪个不为空走 `switch` | 同左 | 同左 |
| AI 的读工具 | `lark_chats` / `lark_messages` / `lark_search`（`assistantTools(withLark, withMail)` 写死名字），lark-connector skill | 走 lark-connector skill | `mail_*`（`mail/mcp.go`） |
| 写操作的闸门 | node 的 Bash 闸门（`node/larkgate.go`，hub.json `lark_gate` 规则） | 同左 | `hub.approve`（`mail_send` 自己调） |
| 实时 | lark-connector 事件 WebSocket（`lark_events.go`）踢收件箱和归档 | 跟飞书一起轮询 | 轮询（收件箱 3 分钟、归档 6 分钟） |
| App 链接 | `FeishuLinks.route` → 文档 / removed原生，其余 `LinkSheet` 网页 | 同左 | — |
| 设置 › 连接器 | `FeishuConnectorView`（`GET /api/v1/lark/status`） | — | 邮箱页 |

每接一个来源，就要在这六七处各写一套，还要记得在每个 `switch` 里加一支。客户端也写死了来源：iOS 的 `InboxItem.isLark`、Android 的 `source == "lark"`，
**不是 lark 的都当邮件显示**。所以直接塞一个 `source: "github"` 的条目进去，旧 App 会把它画成邮件，点「发送」走邮件回复。

## 目标

一个来源实现一个**连接器**：能力按需实现，全部可选。hub 看它实现了哪些，就自动接到知识库、收件箱、助手（MCP 工具）和 App 上。
加一个来源不再改这些管道的代码。

不做的：不为任何来源做浏览页面（`docs/feishu.md`「不做第二个飞书」同理适用于 GitHub、信息）；不改现有来源的行为。

## 一、Connector 接口

代码放 `server/internal/connector`（接口、`Registry`、通用的闸门和状态），各内置连接器一个子包（`connector/github`、`connector/imessage`），
飞书、removed、邮件迁移时先留在 `hub` 里用适配器包一层，避免一次搬太多。`connector` 依赖 `kb` 的数据类型（`SourceChat`、`SourceMessage`、`Item`），
不依赖 `hub`；`hub` 持有 `Registry`。

核心只有身份：

```go
type Connector interface {
    ID() string   // "lark" "mail" "github" "imessage"：配置的键、MCP 工具名前缀、InboxItem.Source
    Name() string // 给人看的：飞书 / 邮件 / GitHub / 信息
}
```

其余能力用接口断言判断有没有（和 `host` 包的可选能力一个写法）：

### 1. 账号与健康（`Accounts`）

```go
type Status struct {
    State     string    // ok | not_configured | not_signed_in | no_permission | rate_limited | error
    Account   string    // 显示的账号名（飞书的名字、GitHub 的 login、「这台 Mac 上的信息」）
    Error     string    // 给人看的一句话（「lark-connector 未登录飞书」「没有完全磁盘访问权限」）
    LastSync  time.Time
    LastEvent time.Time
}
type Accounts interface {
    Status(ctx context.Context) Status
}
// 需要用户提供凭据的连接器再实现：
type Credentials interface {
    Fields() []CredentialField           // 要什么（GitHub：token、可选 base_url）
    Verify(ctx context.Context, v map[string]string) (account string, err error) // 存之前先试一次
}
```

- 凭据存 hub：用 `secret.key` AES-GCM 加密（和启动器的 Key、邮箱凭据同一个主密钥，`hub/launchers.go` 的 `sealKey`），存 Postgres 新表
  `connector_accounts(connector, account, config jsonb, secret_enc text, created_at, updated_at)`；新迁移号挑之前先看所有 worktree。
  明文只在内存，任何接口都不返回（只回「已设置」和账号名）。也可以写 hub.json 的 `<connector>.token_file`（0600），给不想经过 App 的人用。
- `not_signed_in` 这类状态统一出现在：设置 › 连接器、收件箱顶部的错误条、助手快照（「GitHub 未登录」时它就不会去调工具）。
- 飞书的 `lark.ErrNotLoggedIn` → `not_signed_in`，`/api/v1/lark/status` 保留（旧 App 用），数据从同一个 `Status` 来。

### 2. 会话（`Conversations`）和往回补（`Backfill`）

增量有两种天然的单位，都要支持：

```go
// 按会话的游标（飞书、removed）：就是现在的 kb.Source + kb.Backfiller，原样搬过来。
type ChatSource interface {
    Chats(ctx, limit) ([]kb.SourceChat, error)
    Messages(ctx, chat kb.SourceChat, after string, limit int) (msgs []kb.SourceMessage, newer string, more bool, err error)
}
type Backfiller interface {
    Older(ctx, chat kb.SourceChat, before string, limit int) ([]kb.SourceMessage, string, bool, error)
}

// 按账号的游标（邮件的 UID、iMessage 的 ROWID、GitHub 的 updated_at）：一次拿到「自从游标以来」所有会话的新消息。
type StreamSource interface {
    Changes(ctx, cursor string, limit int) (batches []ChatBatch, next string, more bool, err error)
}
type ChatBatch struct {
    Chat     kb.SourceChat
    Messages []kb.SourceMessage // 旧的在前
}

type Attachments interface { Download(ctx, media, dst string) error } // 现在 kb.Source 里的 Download 拆出来
```

- 一个连接器可以有几路会话（飞书连接器 = 飞书会话 + removed帖子），所以是 `ChatSources() []ChatSource` / `Streams() []StreamSource`，
  每路有自己的 `Name()`（`lark`、`moments`），它是知识库 id 的前缀（`chats/lark-…`、`chats/moments-…`），**迁移时不能变**。
- 流式游标存在知识库目录 `cursors/<connector>-<stream>.json`（现在 `mail-cursors.json` 迁过去，读旧文件兼容一版）；按会话的游标照旧在每个会话的 `meta.json`。
- `kb.Engine.Archive` 对 `ChatSource` 照旧；对 `StreamSource` 调 `Changes` 后逐批 `Ingest`。回填对两种都只用 `Backfiller`，按相关性挑会话不变。
- 要注意的现有行为：`archiveChat` 跳过「不比 `LastAt` 新」的消息。iCloud 同步晚到的旧消息、GitHub 改过的评论会比 `LastAt` 早，
  流式路径要像 `Backfill` 那样按日期插进对应的天文件（`AppendMessages` 已经按天分；要补的是「插到这一天里正确的位置」和按 id 去重）。

### 3. 条目（`Items`）

文档、任务、PR、日程。现在的 `kb.ItemSource`（`Items` + `DocTitle`）原样搬过来，`Item.Kind` 从固定的 doc / meeting / event 放开成连接器自己的
（`pr`、`issue`），知识库目录 `items/<kind>/`（现有的 `docs/`、`meetings/`、`events/` 不搬，`kind` 不变）。打标、挂项目走同一个 `TagItems`。
另加一个可选的 `Links() []LinkPattern`：消息里出现这种链接就记成条目（现在 `archive.go` 的 `docLink` 是写死的飞书文档正则）。

### 4. 事件（`Events`）

```go
type Events interface {
    // Watch 一直跑到 ctx 结束，有变化就调 changed（scope：chats | inbox | items）；返回错误时 hub 按退避重启它。
    Watch(ctx context.Context, changed func(scope string)) error
}
type Poller interface {
    NextPoll() time.Duration // 没有实时推送的：hub 按它说的间隔轮询（GitHub 每次按 X-Poll-Interval 改）
}
```

`changed` 就是现在的 `kickInbox` / `h.kb.kick`（带防抖）。飞书的事件 WebSocket、iMessage 的「chat.db-wal 变了」都是 `Watch`；邮件、GitHub 是 `Poller`。

### 5. 收件箱（`Inbox`）

「和我有关、需要我」的判断留在连接器里，产出统一的 `InboxItem`：

```go
type Inbox interface {
    // Collect 返回这一轮有变化的事；open 找这个线程还没处理的那件（现在的 inbox.openFor）。
    Collect(ctx context.Context, open func(thread string) *InboxItem) ([]*InboxItem, error)
    // Reply 把（用户确认过的）文字发回去：飞书发消息、邮件回信、GitHub 评论。
    Reply(ctx context.Context, it InboxItem, text string) error
}
```

- `InboxItem.Source` = 连接器的 `ID()`，不再写死。已有的 `lark` / `mail` 值、`Lark` / `Mail` / `Doc` / `Moment` 块**原样保留**（App 和库里的老数据都靠它们）。
- 新连接器统一用一个新块 `Connector *InboxConnector`：`{thread, label（类型标签：「请你 review」「@我」「CI 失败」）, icon, url（App 内打开）,
  open_url（`sms:` 这类交给系统的）, reply_to（回复会发到哪，一句话）, can_reply}`。App 只画这一个块，不认识具体来源。
- 生命周期（已处理、`done_by=replied/read`、3 天 / 8 天的保留、分拣、AI 分身、翻译）是 hub 通用的，不进连接器。
- 分拣和 AI 分身的提示词里「类型行」按 `kind` 取（`prompts/inbox/item.md`），新 kind 的说明放 `prompts/inbox/kinds/<connector>.md`，不在 Go 里拼。
- **向后兼容**：`GET /api/v1/inbox` 默认只回 `lark` / `mail`；带 `?sources=all` 才回别的来源。新 App、新手表才带，旧 App 不会把 PR 当邮件画。
  `{"type":"inbox"}` 推送不变。

### 6. 读工具（`ReadTools`）

```go
type ReadTools interface {
    Tools() []string                                                   // 工具名，必须以 "<id>_" 开头
    Call(ctx context.Context, name string, args json.RawMessage) (string, error)
}
```

- 描述和 schema 放 `server/internal/prompts/mcp/<connector>/<工具名>.md`（`docs/prompts.md`「MCP 工具」的格式不变），`prompts.Tool(name)` 读。
- hub 的工具列表从 `Registry` 来：状态是 `ok` 的连接器才给工具（现在 `assistantTools(withLark, withMail)` 的两个布尔就是这个意思的特例）；
  `callTool` / `runAssistantTool` 按前缀分给连接器。`mcp_tools.*` 的 golden 迁移前后逐字节一致。
- 只读工具在 `--allowedTools mcp__jarvis` 下不用批准（和现在 `lark_*` 一样）；写操作不放在这里。

### 7. 写操作（`Actions`）

```go
type Action struct {
    Name    string // "github_comment"、"github_review"、"imessage_send"
    Visible string // 谁会看到（「example-org/example-repo#12 的所有参与者」）：卡片上必须写
}
type Actions interface {
    Actions() []string
    // Describe 把参数变成手机卡片上的话（会话名、对象、要发的全文）；不联网也要能出结果（出不来就原样列参数）。
    Describe(ctx context.Context, name string, args json.RawMessage) (Action, string)
    Do(ctx context.Context, name string, args json.RawMessage) (string, error)
}
```

- **统一过闸门**（`connector/gate.go`）：hub 收到写操作 → 查规则 → `allow` 直接做、`deny` 拒绝并把理由还给模型、`confirm`（默认）
  走现有的 `hub.approve`（`IxPermission` 卡片，15 分钟过期，「本会话总是允许」）。规则是 hub.json 的 `connector_gate: [{connector, action, match?, decision}]`，
  第一条匹配的说了算，和 `lark_gate` 一个写法；`mail_send` 迁过来后行为不变。
- 收件箱里「发送」按钮本身就是用户的确认，`Reply` 不再弹卡片（和现在飞书 / 邮件一样）；AI 分身只能起草（`inbox_draft`），它的提示词照旧禁止直接发。
- 飞书的写操作还是 lark-connector skill + node 的 Bash 闸门（命令行里才知道是读是写），不搬；只把规则的存放和卡片的样子统一。

### 8. 链接（`Links`）

- hub 一侧：上面「条目」里的 `LinkPattern`，消息里的链接记成条目、挂到会话和项目上。
- App 一侧：任何链接本来就在 App 内打开（`LinkSheet` 网页卡片），新连接器**不做原生页面**。`GET /api/v1/connectors` 带上每个连接器认的主机名
  （比如自建 GitHub Enterprise 的域名只在配置里，不能写进 App），App 用它决定卡片上的图标和「在 GitHub 打开」这类文字。
  `FeishuLinks` 不动。

### hub 怎么接

```text
Registry ──┬─ ChatSources / Streams / Backfiller / Attachments → kb.Engine（归档、回填、压缩、打标照旧）
           ├─ Items / LinkPattern                              → kb.CollectItems / TagItems
           ├─ Events / Poller                                  → 踢 kbLoop、inboxLoop
           ├─ Inbox                                            → collectInbox、replyInbox
           ├─ ReadTools / Actions                              → 助手和 Agent 的 MCP 工具，Actions 先过闸门
           └─ Accounts / Credentials                           → GET /api/v1/connectors、设置 › 连接器、快照
```

新接口 `GET /api/v1/connectors` → `{connectors:[{id, name, status, account, error, last_sync, capabilities:[…], hosts:[…], credential_fields:[…]}]}`，
`PUT /api/v1/connectors/{id}/credentials`、`DELETE …/credentials`。`features` 里加 `connectors`，旧 App 看不到这个特性就不显示新行。

## 二、三种形态

### 内置（Go，在 hub 里）

飞书、removed、邮件、GitHub。直接实现上面的接口，跟 jarvisd 一起发布，提示词在本仓库、受 golden 测试管。

### 外部进程（沿用插件机制，代码在别的仓库）

`docs/plugins.md` 的插件加一个 `kind: "connector"`：hub 照旧启动进程、给 `JARVIS_PLUGIN_SOCKET`、健康检查、退避重启；
只连 socket 的远端插件（`ssh -R`）也照旧能用。插件在 socket 上提供：

| 路径 | 用途 |
| --- | --- |
| `GET /manifest` | `{id, name, capabilities, actions:[{name, visible}], hosts}`：hub 据此注册能力；**没在 actions 里声明的工具一律当只读之外的东西处理**（见下） |
| `POST /mcp` | MCP（streamable HTTP，JSON-RPC 的 `tools/list`、`tools/call`）：给 AI 用的工具。hub 当 MCP 客户端，把工具加上 `<id>_` 前缀转给助手 |
| `GET /status` | `Status` |
| `GET /sync/chats`、`/sync/messages?chat=&after=&limit=`、`/sync/older?…`、`/sync/changes?cursor=&limit=` | 会话 / 往回补 / 流式（和 Go 接口一一对应的 JSON） |
| `GET /sync/items`、`GET /sync/inbox` | 条目；收件箱的候选（hub 负责 `open` / 合并 / 生命周期） |
| `GET /events` | SSE：`{"scope":"chats"}` 这样的一行一个；没有就按 manifest 里的 `poll_seconds` 轮询 |
| `GET /media?path=` | 附件 |
| `POST /actions/{name}` | 写操作：**只在 hub 的闸门放行之后**由 hub 调 |

**评估：可行，建议这样做。**

- 光用 MCP 不够：MCP 有 resources + 订阅，但没有游标、往回补的约定，现成的 MCP server 大多也不实现；知识库要的是「从上次到现在的增量」。
  所以 MCP 只管「给 AI 用的工具」，增量用一个很小的 REST 扩展（上表 `/sync/*`），两边各干各的、都简单。
- 闸门不能信插件：MCP 的 `readOnlyHint` 只是提示。hub 只把 manifest 里**没**列在 actions、且 `annotations.readOnlyHint == true` 的工具当只读直接放行，
  其余全部按写操作过闸门。插件拿不到用户的确认，也绕不过去（App 的请求不会到插件，插件只能被 hub 调）。
- 好处：现成的 MCP server（比如某个服务官方的）可以零改动当「只有读工具」的连接器挂上；以后要收件箱 / 知识库再补 `/sync/*`。
- 代价：hub 要多一个 MCP 客户端（`tools/list`、`tools/call` 两个方法，几百行）；外部连接器的工具描述不在本仓库的 `prompts/mcp/`，不受 golden 管，
  这是「提示词一律放 `prompts/`」的一个明文例外（只适用于不在本仓库的代码）。
- 现在没有第一个外部连接器，**这一形态先只定协议，等真有一个再实现**（见「需要用户拍板」）。

### 第三种：node 当本地连接器（Mac 上的数据）

有些数据只在某台电脑上（iMessage 的 `chat.db`、以后的本地备忘录、照片……），hub 读不到。node 本来就经 ssh 连着 hub、有双向 RPC，
所以由 node 读本机数据、hub 一侧注册成连接器（`connector/remote.go`：一个把调用转成 RPC 的适配器）。

hub → node 新方法（旧 node 回 `unknown method`，hub 当它没有本地连接器）：

| method | params → result |
| --- | --- |
| `connector.list` | → `{connectors:[{id, status, error}]}`：这台机器能提供哪些、能不能用（比如没有完全磁盘访问权限就是 `no_permission`） |
| `connector.changes` | `{id, stream, cursor, limit}` → `{batches, next, more}` |
| `connector.older` | `{id, chat, before, limit}` → `{messages, before, more}` |
| `connector.media` | `{id, path, offset}` → 分块读（同 `file.read` 的分块方式，单个 20 MB 封顶） |
| `connector.action` | `{id, name, args, approval}` → `{result}`：只在 hub 闸门放行后发，`approval` 是那张卡片的 id（node 记日志用） |
| `credential.gh_token` | `{host}` → `{token}`：跑 `gh auth token`，给 GitHub 连接器的 `from_machine` 用（「四 · 凭据」）；只回给 hub，不写日志 |

node → hub 事件：`connector.changed {id, scope}`。

一个连接器只认**一台**机器（配置里写 `imessage.machine`），不在多台 Mac 上各读一份（iCloud 同步的数据是同一份，读两遍只会重复）。

## 三、迁移：飞书、removed、邮件搬到新接口（只重构，不改行为）

每一步一个 PR，合入前同一组验证：

- **现有测试全绿**：`./scripts/test-server.sh`（hub 机器、真 Postgres）、`go test ./...`、App 的 `JarvisTests` 和收件箱 / 知识库的 UI 测试。
- **golden 逐字节不变**：提示词、MCP 工具列表（`mcp_tools.*`、`mail_tools`）。
- **新增的对照测试**（第 M0 步先写，用旧代码生成基线）：假 lark-connector + 假邮箱（合成数据）跑一轮归档 + 收件箱采集，
  把结果（知识库目录树和每个文件的内容、`InboxItem` 的 JSON）存成 golden；之后每步都必须一致。
- **真实数据对照**（只读、只打印数字）：`jarvisd dev connector-diff`（只在 DEBUG 构建 / 测试里），在 hub 机器上用真实的 lark-connector 和邮箱，
  旧代码和新代码各跑一轮，写到两个临时目录（不碰 `~/.jarvis/data`、不写库、不标已读），比较：每个会话的行数和内容哈希、收件箱条目的 id / kind / 线程归属。
  只打印「一致 / 第几个会话不一致、差几行」，不打印内容。对照完删掉临时目录。

| 步 | 做什么 | 验收（除了上面那组） |
| --- | --- | --- |
| M0 | 写对照测试和 `connector-diff`，用当前 master 生成基线 | 基线入库（合成数据）；真实对照在当前代码上自己和自己比一致 |
| M1 | 加 `connector` 包和 `Registry`；飞书连接器 = 现在的 `larkSource` + `momentsSource` + `larkItems` + 事件 WebSocket 的包装；`kb.Engine.Sources` 从 Registry 来 | 知识库 id 前缀不变；真实对照一致 |
| M2 | 邮件实现 `StreamSource`（UID 游标），删掉空壳 `mailSource` 和 `kbMail` 的循环；`mail-cursors.json` 迁到 `cursors/` 并兼容读旧文件 | 迁移前后同一批邮件得到同样的会话和行；游标不回退（不会重复归档） |
| M3 | 收件箱：`collectLark` / `collectDocs` / `collectMoments` / `collectMail` 包成 `Inbox`；`replyInbox` 的 `switch` 换成 `Reply`；加 `?sources=` | 收件箱对照一致；旧 App（不带 `sources`）看到的列表和以前一样 |
| M4 | MCP：`lark_*`、`mail_*` 从 Registry 注册，`callTool` 按前缀分发 | 三种工具列表的 golden 逐字节一致；`TestEveryMCPToolIsUsed` 过 |
| M5 | 闸门：`mail_send` 的批准、`lark_gate` 的规则读取搬进 `connector/gate.go`，加 `connector_gate` 配置 | 卡片文字和以前一样（截图对比）；`lark_gate` 老配置照样生效 |
| M6 | `GET /api/v1/connectors`；App 的设置 › 连接器用它画新行，邮箱 / 飞书两行不变 | `/api/v1/lark/status` 还在；设置页截图深浅色和改前一样（多了的行除外） |

M1–M6 合完、部署后在 hub 上看一天日志（归档行数、收件箱条数、分拣数和前一天同量级），再开始接新来源。

## 四、GitHub 连接器（第一个新来源，内置）

### 收件箱：什么算「需要我」

轮询 `GET /notifications`（只看未读、`participating` 不加，原因看 `reason`），一个 notification 线程 = 一件事（`github:<thread id>:<updated_at>`），
线程后来又有动静就并进还没处理的那件，处理后再来的另开一件（和飞书会话一样的规则）。

| 情形 | 怎么认 | 默认分拣 |
| --- | --- | --- |
| 等我 review 的 PR | `reason = review_requested`，且我还在 requested reviewers 里（团队请求的看我是不是那个团队的） | 需要你 |
| @ 我 | `mention`；`team_mention` 默认「知悉即可」 | 需要你 |
| 我的 PR 有新评论 / review | `author` + `subject.type = PullRequest`，最新一条不是我 | 交给模型（「请你改」的是需要你，「LGTM」是知悉即可） |
| 我的 PR 的 CI 挂了 | `ci_activity`（我触发的 Actions 跑完了），再查这个提交的 check runs 有没有 failure；我开着的 PR 的 head 提交也查一次（别人推的 CI 不发通知） | 需要你 |
| 分配给我的 issue | `assign` | 需要你 |
| 其余（`subscribed`、`state_change`、`comment` 等） | 不进收件箱，只在知识库里（如果这个线程已经在） | — |

- 自己在 GitHub 上回了（线程最新一条评论是我、在这件事之后）→ `done_by=replied`；PR 合了 / 关了、review 请求撤了 → `done_by=read`（不是「需要你」的直接算处理完）。
- **已处理只记在 Jarvis，不回写 GitHub 的已读**（和飞书、邮件的规则一致；要不要改见「需要用户拍板」）。
- 回复：在 PR / issue 下发评论（`POST /repos/{o}/{r}/issues/{n}/comments`）；`reply_to` 写「评论到 example-org/example-repo#12（仓库可见的人都能看到）」。
- App 行：GitHub 标志（`Theme.Brand.github`，要加官方标志资源）、标签（「请你 review」「@我」「CI 失败」「分配给你」）、标题是 PR / issue 标题，
  第二行 ✦ 摘要，第三行「仓库 #号 · 最新评论人：内容」。点开是通用详情：这件事的上下文（hub 的 `Context`：描述、最近的评论 / review、失败的检查名）
  +「在 GitHub 打开」（App 内网页卡片）+ 操作条（AI 草拟回复 / 交给 AI / 已处理）。**没有** PR 浏览页、diff 页、仓库页。

### 知识库

- **会话**：一个 PR / issue 一个会话 `chats/github-<owner>-<repo>-<号>`（`kind: pr | issue`），描述是第一条，后面是 issue 评论、review（「alice 批准了」「bob 要求修改：…」）、
  行内评论（带文件名:行号）按时间排；机器人（`[bot]` 账号、CI 评论）的标成机器人，`HasPeople` 不把它们算真人。
- **哪些进**：只收「和我有关」的线程（通知里出现过的 + 搜索 `involves:@me` 最近 30 天更新过的），不爬整个仓库。游标是线程的 `updated_at`
  + 每类评论的 `since`（`StreamSource`），往回补就是第一次读全部评论（和removed一样，不需要 `Older`）。
- **仓库挂项目**：项目的 `dirs`（`<机器>:<路径>`）是代码目录；node 的 `git.status` 加一个可选字段 `remotes`（旧 node 没有就跳过），
  hub 把 `github.com/<owner>/<repo>`（或配置的 GHE 主机）对上项目，存进项目 `meta.json` 的 `repos`。这个仓库的 PR 会话压缩时把项目直接给模型（不用它猜），
  时间线记「GitHub · PR #号 · 一句话」。没对上项目的仓库照常由模型按内容挂。
- 消息里出现的 PR / issue 链接（飞书、邮件里）记成条目（`LinkPattern`），挂到那个会话的项目上。

### AI 的工具

| 工具 | 只读 / 写 | 做什么 |
| --- | --- | --- |
| `github_search` | 只读 | 搜 issue / PR（GitHub 搜索语法，默认加 `involves:@me` 的提示写在描述里）、搜代码 |
| `github_read` | 只读 | 一个 PR / issue：描述、状态、reviewers、评论和 review（截断，最新的优先） |
| `github_diff` | 只读 | PR 的改动文件列表 + 指定文件的 patch（每个文件截断，太大的只给统计） |
| `github_checks` | 只读 | 一个提交 / PR 的检查结果；失败的 job 日志最后 N KB（Actions 的 logs 接口） |
| `github_comment` | 写，过闸门 | 评论 PR / issue；卡片写明仓库、编号、标题、全文、「仓库可见的人都能看到」 |
| `github_review` | 写，过闸门 | approve / request changes / comment；approve 卡片单独强调 |

描述放 `prompts/mcp/github/`，golden 加一种「有 GitHub」的工具列表。

### 事件：轮询

hub 没有公网入口，webhook 收不到。`GET /notifications` 带上次的 `If-Modified-Since`（原样回传上次的 `Last-Modified`），
304 不算速率限制；两次请求的间隔按响应头 `X-Poll-Interval`（一般 60 秒，GitHub 忙时会变大，必须遵守），`Poller.NextPoll` 每次更新。
有新通知再去读对应的 PR / issue / check runs（每轮封顶 30 个线程，剩下的下一轮）。`X-RateLimit-Remaining` 低于 10% 时状态变 `rate_limited`、
只保留通知轮询。

### 凭据

**已定（2026-11-01 用户）：从某台机器的 `gh` 登录读。** 用户原话：「github 能不能支持自动从 gh cli 读，从某台机器上读取来授权登录」。

- **配置**：hub.json `"github": {"auth": {"from_machine": "<机器 id>", "host": "github.com"}}`（`host` 可省，默认 github.com；自建的 GitHub Enterprise 写它的主机名，只在配置里）。
- **取 token**：node 加一个 RPC `credential.gh_token {host}` → `{token}`：在那台机器上非交互地跑 `gh auth token [-h <host>]`。
  - 只有 hub 能调：它是 hub → node 的 RPC，走 hub 和 node 之间的 ssh 通道；不开 REST 接口，hook / MCP 的 `node.sock` 不认这个方法，助手和 Agent 拿不到。
  - `gh` 把登录存在 `~/.config/gh/hosts.yml` 时 ssh 里能读；存在钥匙串里时，ssh 会话打不开钥匙串，报「那台机器的 gh 登录在钥匙串里，经 ssh 读不到」，
    给出两条路：那台机器上 `gh auth login --insecure-storage`，或者改用手动粘贴。旧 node 不认这个方法，报「<机器> 的 jarvisd 太旧」。
- **token 只在 hub 的内存里**：不落库、不写日志、不进任何响应和错误信息（错误里出现 token 样式的串先抹掉）；也不放进助手 / Agent 的环境变量。
  - 请求回 401：重新向那台机器取一次（那边可能刚重新登录过），还是 401 就是 `not_signed_in`：「<机器> 上的 gh 登录失效了，在那台机器上运行 gh auth login」。
  - 那台机器离线：用内存里上次取到的；hub 重启后它还离线、内存里又没有，状态是「GitHub 未授权（<机器> 离线）」，它连上时自动再取。
- **App**：设置 › 连接器 › GitHub 显示「来自 <机器> 的 gh 登录 · <账号>」（账号来自 `GET /user`）。
- **备选：手动粘贴 token**（加密存 hub，同「一、账号与健康」）。没配 `from_machine` 时用它；两个都配了以 `from_machine` 为准。
- **权限要说清**：gh 的 token 权限大（实测 scopes 是 `gist`、`read:org`、`repo`、`workflow`：能推代码、改 CI 配置）。连接器自己只调读接口；
  写操作只有 `github_comment`、`github_review` 两个，照旧过闸门；不拿它做 git 推送、不碰 workflow、不给别的连接器用。
- **主会话实测（2026-11-01，不打印 token）**：两台 Mac 上的 gh 都登录了 github.com、是同一个账号，token 在 `hosts.yml` 里，经 ssh 非交互 `gh auth token`
  拿得到（40 个字符）；另一台 Mac 的 gh 登录已过期（具体哪台记在车道报告里）。用这个 token 调 `/notifications` 回 200，
  `X-Accepted-OAuth-Scopes` 是 `notifications, repo`（有 `repo` 就够），`X-Poll-Interval` 是 60；搜索 `review-requested:@me` 也是 200。

为什么之前写的两种给法不用了：通知接口只支持经典 PAT 和 OAuth 的 token（细粒度 PAT、GitHub App 的 token 不行），gh 登录拿到的就是 OAuth token，
带 `repo` 已经够用，用户也不用再去生成 token。

## 五、iMessage 连接器（第二个，Mac 本地）

### 调研结果（2026-11-01，只看文件在不在和行数，没读任何内容）

- hub 连着的 Mac 里有三台有 `~/Library/Messages/chat.db`，都是 macOS 26。其中两台的消息数、会话数、附件数完全一样（同一个 Apple ID 开了 iCloud 信息同步），
  另一台少很多、最新一条停在半个多月前。具体是哪几台记在车道报告里，不进仓库。
- 经 ssh 起的进程在这三台上都读得了 `chat.db`：这几台的「完全磁盘访问权限」里已经打开了 `sshd-keygen-wrapper`（「远程登录 › 允许远程用户完全访问磁盘」），
  见 `docs/lessons/server.md` 的 TCC 一条。
- 大约一半的消息 `text` 是空的、只有 `attributedBody`：不解析 typedstream 就丢一半。
- `date` 是 2001-01-01 起的纳秒数（老系统是秒，按数量级判断）。会话的服务有 iMessage、SMS、RCS 三种，SMS 占多数（大多是验证码、通知类）。

### 完全磁盘访问权限（TCC）

`chat.db` 受 TCC 保护，权限算在**负责进程**（responsible process）头上，不是读文件的那个二进制：

- node 是 hub 经 `ssh … jarvisd node --stdio` 起的，负责进程是 `sshd-keygen-wrapper`。给它完全磁盘访问权限 = **任何 ssh 登录这台 Mac 的会话都有**，
  不只是 jarvisd。这几台现在就是这样（为了读「桌面」「文稿」早就打开了）。
- 直接给 `jarvisd` 本身授权没用：经 ssh 起的子进程仍算在 sshd 头上；而且 jarvisd 每次部署都换二进制、ad-hoc 签名，授权会失效。
- 最小、最稳的做法是一个**独立的小帮手**：`jarvisd messages-helper`，由用户的 LaunchAgent（`launchd` 起，负责进程是它自己）运行，
  只在本机 unix socket 上提供「读 chat.db / 通讯录、拿附件」，node 连这个 socket。授权只给它一个；签名要稳定（Developer ID，按团队 ID 认，换版本不掉授权），
  所以它不随 jarvisd 每次部署更新，单独发版。
- 建议：**第一期用现成的 ssh 授权**（已经打开、零配置，`connector.list` 读不了时报 `no_permission` 并给出说明），
  帮手放到后面、和发送（要「自动化」权限，也得在图形会话里授权）一起做。这条要用户拍板。

### 读 chat.db

已实现（#449，`server/internal/connector/imessage`，node 的 `connector.*` RPC 见 `docs/protocol.md`）：会话按人合并（一个号码在 iMessage / 短信 / RCS 上的几个 `chat` 是一个会话，群按 `guid`），
id 是哈希；通讯录按号码后 10 位对，对不上再按后 7 位（只有一个人有时）；没有权限时 `connector.list` 按 node 怎么跑给出两种说法：经 ssh 起的打开「远程登录 › 允许远程用户完全访问磁盘」，
launchd 常驻的 node（`scripts/node-launchd.sh`）给 jarvisd 本身授权（jarvisd 每次部署换二进制，授权可能要重开，帮手那一步解决）。
测试数据是 `testdata/home`（合成的 `chat.db` 和通讯录，表结构是 macOS 26 的；`testdata/gen` 下的 `blobs.swift` + `make.py` 生成），读真数据只用 `TestRealMessagesCounts` 打印条数。

- 只读打开：`sqlite3 -readonly -json`（macOS 自带的 `/usr/bin/sqlite3`，jarvisd 是 `CGO_ENABLED=0` 交叉编译的，不引入 SQLite 驱动；
  `attributedBody` 用 `hex()` 取出来）。不加 `immutable=1`（会读不到还在 WAL 里的新消息）。不复制、不缓存整个库。
- 用到的表：`message`（`ROWID`、`guid`、`text`、`attributedBody`、`handle_id`、`is_from_me`、`date`、`date_read`、`is_read`、`cache_has_attachments`、
  `associated_message_type` / `associated_message_guid`（点按回应）、`thread_originator_guid`（行内回复）、`date_edited`、`date_retracted`、`item_type`（群事件））、
  `chat`（`guid`、`chat_identifier`、`display_name`、`style`、`service_name`）、`handle`、`chat_message_join`、`chat_handle_join`、`attachment`、`message_attachment_join`。
- **增量游标：ROWID**。它只增不减，iCloud 晚到的旧消息也拿到新的 ROWID，所以按 ROWID 不会漏；归档时按 `date` 放进对应的天文件
  （见上面「要注意的现有行为」）。`date` 不能当游标：晚到的旧消息会被跳过。编辑、撤回第一期不追（归档是追加的，和飞书一样）；
  后面要的话每轮再看最近 7 天 `date_edited` / `date_retracted` 变了的。
- **typedstream**（`attributedBody`）：是 `NSArchiver` 的 streamtyped 格式（`\x04\x0bstreamtyped` 开头），里面是 `NSAttributedString`，第一个 `NSString` 就是正文。
  写一个最小的 typedstream 解码器（`connector/imessage/typedstream.go`）：按格式读类链和对象，取出第一个字符串（长度前缀：小于 0x80 是一个字节；
  `0x81` 后面 2 字节、`0x82` 后面 4 字节，小端），属性里的 `__kIMMentionConfirmedMention`（@ 了谁）、链接也读出来。
  解不出来的消息记一行「[无法解析的消息]」并计数（状态里显示），**不打印、不记录原始数据**。
- 测试数据：在 Mac 上用一个 Swift 小脚本（`NSArchiver` 归档 `NSAttributedString`，正文是合成的「示例消息」）生成几种 blob（纯文字、长文字、带 @、带链接、emoji），
  连同一个只有用到的表、全是合成数据的 `chat.db` 一起放 `testdata/`，生成脚本也入库。
- 点按回应（`associated_message_type` 2000–3006）不当消息，第一期直接跳过；行内回复带上被回复的那条的开头（「回复「…」：」）；群事件（改名、加人）记成系统行。

### 附件

- 第一期**只记一行占位**：「[图片]」「[文件 名字 · 大小]」「[语音]」，不拷文件。
- 第二期（要用户同意）：图片走 `connector.media` 拷到 hub 的 `attachments/<日期>/`（HEIC 在 node 上用 `sips` 转 JPEG），单个 20 MB 封顶，和飞书一样 30 天删；
  文件、语音、视频仍只占位。`attachment.filename` 是 `~/Library/Messages/Attachments/…` 的路径，只认这个目录下的（防止被诱导读别处）。

### 群聊和联系人名字

`handle.id` 只有电话号码 / 邮箱，群可能没名字。两条路：

- **Mac 的通讯录**：同一台 Mac 的 `~/Library/Application Support/AddressBook/Sources/*/AddressBook-v22.abcddb`（也是 SQLite，同样的完全磁盘访问权限），
  电话号码规范化（去空格、`+86`）后对上名字。离线可用、和信息同一个 Apple ID。
- **手机的 `phone_contacts`**：已有的工具，但要手机在线（或被静默推送唤醒），批量查几十个号码不合适。
- 建议用 Mac 通讯录为主，对不上的再在压缩时把号码交给 `phone_contacts` 补一次，结果缓存在知识库的人上（`people/imessage-<号码哈希>`）。
  没名字的群用成员名拼（「张三、李四等 4 人」）。

### 身份和 id

- 会话 id 用 `chat.guid` 的哈希（`imessage-<sha256 前 12 位>`），**不用**电话号码 / `chat_identifier`：目录名、日志、URL 里都不出现号码。人同理。
- 号码只存在 `meta.json` 里（hub 机器的知识库，本来就是个人数据的地方），日志里只出现 id。

### 收件箱：什么算「需要我」

- 私聊里对方发来、我还没回的（最新一条不是我）；群里 @ 了我的。一个会话同时一件未处理，规则同飞书。
- 在 iPhone / Mac 上读过（`is_read`）且不是「需要你」的 → `done_by=read`；我回了 → `done_by=replied`。
- 回复，第一期**不从 Mac 发**：App 的「发送」变成「在信息里回复」，打开 `sms:<号码>&body=<草稿>`（系统「信息」，草稿填好，用户自己按发送）；群聊没有可靠的 URL，只给「打开信息」。
  `open_url` 由 hub 给（号码不进 App 的日志）。
- 第二期：`imessage_send`（AppleScript `tell application "Messages"`），**必须过闸门**，卡片写明发给谁（名字 + 号码后四位）、全文；
  需要「自动化」权限（控制「信息」），要在图形会话里授权一次，所以放在帮手那一步做。

### 事件

node 用 kqueue 盯 `chat.db-wal` 的写入（不轮询数据库），防抖 3 秒后推 `connector.changed`；另外每 5 分钟兜底查一次 `max(ROWID)`。

## 六、App 侧

遵守「不做第二个某某 App」：不给 GitHub 做浏览页面（没有仓库列表、PR 页、diff 页）。**信息是例外**（用户 2026-11-02「App 里先做最小可用的一版：会话列表加消息查看」）：
消息默认不交给 AI（`imessage.model` 默认关），用户只能在 App 里自己看，所以有会话列表和消息查看（只读，经 hub 读那台 Mac 的 `chat.db`，消息不复制到 hub）。
已实现（#449）：设置 › 连接器 › 信息（读的是哪台 Mac、状态，没有完全磁盘访问权限时按那台 Mac 的 node 怎么跑一步步说怎么开、「重新检查」）› 会话（新的在前，未读数，搜名字或号码）› 一个会话
（像「信息」那样：隔一小时或换一天出时间，群里换人出名字，短信 / iMessage 换了标出来，回复带被回复的开头，附件只有描述，编辑、撤回、读不出来的都说明）；
`Views/Messages/`，DEBUG `-openConnectorMessages YES [-openMessagesChats YES]`、`-openMessagesChat <会话 id>`，mock 的 `MOCK_IMESSAGE=denied`，UI 测试 `MessagesConnectorTests`。其余只做三处：

1. **收件箱的条目**：通用的 `InboxConnector` 块画行和详情（上面「GitHub」「iMessage」里写的样子）；筛选条按 `GET /api/v1/connectors` 加「GitHub」「信息」；
   手表的「需要你」也显示（标签 + 标题 + 摘要）。App 请求列表时带 `?sources=all`。Android、网页这一轮不做，它们拿不到新来源（不带参数）。
2. **链接在 App 内打开**：GitHub 链接本来就走 `LinkSheet` 网页卡片，不改；卡片标题栏按 `hosts` 显示 GitHub 标志。iMessage 没有链接，只有 `sms:`。
3. **设置 › 连接器的状态**：在现有的「这台 iPhone / 邮箱 / 飞书」下面按 `/api/v1/connectors` 加行：
   - GitHub：状态、账号、粘贴 token（A / B 两种给法的说明，`SecureField`，存了只显示「已设置」）、`base_url`、最近一次同步、速率限制剩余。
   - 信息：读的是哪台 Mac、权限状态（没权限时一步步说怎么开）、收哪些会话（只收通讯录里的人 / 全部）、排除列表、保留天数。
   - DEBUG 入口 `openConnectorGitHub`、`openConnectorMessages`（和 `openConnectorFeishu` 一样，名字不和 Swift 标识符重名，加进 `release.sh` 的列表）。
   - mock hub（`scripts/mock_hub.py`）加两个连接器和几件合成的条目，UI 测试和深浅色截图都用它。

## 七、隐私

- **仓库里只有合成数据**：测试、golden、截图、提交信息里不出现真实的消息、PR 内容、人名、仓库名、电话号码、邮箱、GHE 域名。
  GitHub 用 `example-org/example-repo`、`alice`；信息用 `+1 555 0100` 这类保留号段、「示例消息」。`scripts/privacy_check.py` 照常跑，
  加两条规则：像真实手机号的串、`github.com/<非 example 的组织>`。
- **日志**：不记消息正文、号码、token；只记连接器、id、条数、错误类型。`connector-diff` 只打印数字。
- **GitHub**：token 加密存 hub，不回给任何客户端；只收和我有关的线程；写操作全部过闸门。
- **iMessage 是最敏感的个人数据**，默认关，打开时在设置里逐项选：
  - **哪些进知识库**：默认只收**通讯录里有的人**的私聊和群（验证码、银行、快递、营销这类短号码自动排除）；可以改成「全部」或「只收白名单」。
    hub.json / 设置里的 `imessage: {machine, mode: contacts|all|allowlist, include: [], exclude: [], text_days, attachments: false, model: false}`（`model` 默认关，见拍板 7）；
    排除列表按会话（App 里在收件箱详情点「不再收这个会话」也加进去），**加进排除时立刻删掉已归档的内容**。
  - **验证码**：即使在收的会话里，像验证码的消息（「验证码」「code」附近的 4–8 位数字）把数字换成「••••」再存。
  - **保留多久**：文字默认跟 `kb.text_days`（365 天），可以单独设短（建议 90 天，见拍板）；附件第一期不存。
  - **给不给模型**：压缩、打标、分拣、AI 分身都会把文字发给 hub 配的模型（`kb.ai` / `ai`，可能是第三方服务）。`model: false` 时只归档、可搜索，
    不压缩、不进收件箱的分拣（按规则兜底），也不进快照。
  - 读是只读的：不改已读、不删消息、不写 `chat.db`。

## 八、分步计划和验收

| 步 | 内容 | 验收 |
| --- | --- | --- |
| P0 | 本计划 | 用户点头；拍板项有结论 |
| C1 ✓ | 框架（#414）：`connector` 包（`Connector` 和可选能力 `Accounts` / `ReadTools` / `Actions` / `Events` / `Hosts`、`Registry`、闸门 `connector_gate`）；hub 持有 Registry，`GET /api/v1/connectors`、特性 `connectors`，连接器的工具只给助手、按前缀分发，写操作过闸门、默认走手机卡片；`Events` 踢收件箱 / 知识库。`Inbox`、`StreamSource`、凭据、`Poller` 随第一个用到它们的连接器（G1–G4）一起加 | 不注册连接器时 MCP golden 逐字节不变；假连接器的单元测试（工具只来自 `ok` 的、写操作等卡片、闸门 allow / deny、Watch 退避重启） |
| M0–M6 | 迁移（第三节） | 每步：现有测试 + golden 逐字节 + 合成对照 + 真实数据对照一致；部署后一天日志同量级 |
| G1 ✓ | GitHub（#421）：`credential.gh_token` RPC、`from_machine` 取 token（内存）、~~粘贴备选~~（先不做，等真有人不用 gh 时再加）、状态、`/api/v1/connectors`、设置页的 GitHub 行 | 设置页显示「来自 <机器> 的 gh 登录 · 账号」；那台机器离线时用内存里的、hub 重启后离线报「未授权（<机器> 离线）」、连上自动恢复；401 重新取一次；旧 node 报太旧；token 不出现在数据库、日志、响应、助手环境里（grep 验证）；粘贴假 token 报错；设置页深浅色截图 |
| G2 ✓ | 只读工具 `github_search/read/diff/checks` | golden；对 mock GitHub（合成 JSON）的单元测试；用户的真 token 在 hub 上读一个自己的 PR（只看结果条数和截断是否生效） |
| G3 ✓ | 通知轮询 + 收件箱（四种情形）+ `?sources=all` + App 的通用行和详情 | 合成通知的单元测试覆盖每种 `reason`、并入 / 另开、`done_by`；304 和 `X-Poll-Interval` 被遵守（测试里的假服务器计数）；旧 App 列表不变；新 App 截图 |
| G4 ✓ | 知识库（#476）：PR / issue 会话（`connector.Archive`，游标在 `cursors/<id>.json`）。`remotes` 对项目、链接成条目（G4b）暂不做：2026-11-02 hub 上 40 个在用的项目没有一个填了 `dirs`，对上了也挂不到项目；链接本来就在压缩的上下文里。等项目普遍有 `dirs` 再做 | 合成数据的归档 golden；一个真实 PR 归档后行数和评论数对得上（只看数字） |
| G5 ✓ | 写（#480；实测等用户在自己的测试仓库做）：`github_comment`、`github_review` 过闸门；收件箱「发送」= 评论 | 闸门测试（confirm / allow / deny）；用户在自己的测试仓库实测一次评论（卡片截图里只有合成内容的测试仓库） |
| I0 | 在选定的 Mac 上确认权限和规模（只读行数，已完成一部分） | 报告里有：哪台、权限是否够、消息量 |
| I1 | node：`connector.list/changes`、typedstream 解码、ROWID 游标、kqueue（#449 的第一步先做了 `connector.list` / `connector.chats` / `connector.older`、typedstream、Mac 通讯录对名字，给 App 的会话列表和消息查看用；`changes`、kqueue 等进知识库时再做） | 合成 `chat.db` + 合成 blob 的单元测试（包括解不出来的）；旧 hub ↔ 新 node、新 hub ↔ 旧 node 都不报错 |
| I2 | hub：远程连接器适配、知识库归档（只收通讯录里的人）、联系人名字、验证码打码、排除即删除（#449 先做了适配：hub.json `imessage.machine`、状态和权限引导 `GET /api/v1/imessage`、App 读会话和消息的两个只读接口，不存、不给模型；归档等 AI 开关有结论再做） | 在真 Mac 上跑一轮：只打印会话数、行数、被排除数、解码失败数；抽查由用户自己在 App 里看 |
| I3 | 收件箱 + `sms:` 回复 + 设置里的「信息」行（#449 先做了「信息」行和会话 / 消息查看；收件箱等 AI 开关有结论，用 G3 的 `InboxThing.OpenURL` 和通用块的 `open_url` 按钮） | 合成数据单元测试；mock hub 截图；真机上点「在信息里回复」草稿已填好 |
| I4 | 附件（图片，可选） | 拷贝只在 Attachments 目录下；30 天清理 |
| I5 | 帮手（LaunchAgent + 自己的授权）+ `imessage_send` 过闸门 | 关掉 ssh 的完全磁盘访问后仍能读；发送卡片截图；拒绝时不发 |
| X1 | 外部进程连接器（`kind: connector`、MCP 客户端、`/sync/*`） | 有第一个外部连接器时再做；用一个合成的示例插件跑通 |

GitHub 和迁移可以并行（G1、G2 只依赖 M1、M4）；iMessage 依赖 M1、M2（流式归档）和 M3（收件箱接口）。

## 需要用户拍板

1. ~~**GitHub token 怎么给**~~：已定（2026-11-01）——从 `github.auth.from_machine` 那台机器的 gh 登录读，只留在 hub 内存；手动粘贴作备选（见「四 · 凭据」）。
2. **GitHub 是 github.com 还是公司自建的 GHE**（或者两个都要）？自建的地址只写配置。
   暂按默认（2026-11-02 主会话）：先接 github.com；`github.auth.host` 留成配置项，GHE 现在不做。
3. **GitHub 进知识库的范围**：只收和我有关的线程（推荐），还是指定仓库全部 PR / issue？
4. **已处理要不要回写 GitHub 的已读**：现在飞书、邮件都不回写（推荐保持一致）。
5. **iMessage 读哪台 Mac**：数据一致的两台里选一台常开的（推荐），另一台不读。
6. **权限**：第一期用现成的「ssh 有完全磁盘访问权限」（推荐，代价是任何 ssh 会话都有这个权限，现在已经如此），还是一开始就做独立帮手、把 ssh 那个开关关掉？
7. **iMessage 收哪些**：只收通讯录里的人（推荐）/ 全部 / 白名单；SMS（验证码、通知）要不要收；文字保留多久（推荐 90 天）；
   **能不能把信息的文字发给模型**（压缩、分拣、AI 分身；不能的话只归档和搜索）。
   暂按默认（2026-11-02 主会话）：读取放在 Mac 的 node 本地；**默认不把消息文字交给 AI**，只在 App 里给用户自己看；`imessage.model` 是开关，默认关。
8. **联系人名字**：Mac 通讯录为主、手机补（推荐），还是只用手机的 `phone_contacts`？
9. **iMessage 回复第一期**：用 `sms:` 打开系统「信息」由你按发送（推荐），还是直接做从 Mac 发（要「自动化」权限，必须过闸门）？
10. **外部进程连接器**：同意「MCP 管工具 + `/sync/*` 管增量」的方向，并且等有第一个外部连接器再实现？
11. **Android、网页**这一轮不显示新来源，可以吗？

## 需要用户做的事

- GitHub：在 hub.json 写 `github.auth.from_machine`（用哪台机器的 gh 登录，选 gh 登录有效、常开的那台）；那台机器保持 `gh` 登录，
  过期了就在那台机器上 `gh auth login`（登录要存在 `hosts.yml`，不要只存钥匙串）。用自建 GHE 的话写上 `host`。不想用 gh 的登录，再在 App 里粘贴 token。
- iMessage：选定的那台 Mac 保持登录「信息」、开着 iCloud 信息同步；第一期确认「远程登录 › 允许远程用户完全访问磁盘」保持打开；
  做帮手那一步时在那台 Mac 的图形界面里给帮手授权一次（完全磁盘访问 + 控制「信息」）。
- 写操作的实测（G5、I5）用你自己的测试仓库 / 发给自己的号码，测试内容只用合成的话。
