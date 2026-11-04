# 计划：插件底座（#1101）

状态：**第 1 步（底座）的服务端做完**（1a，2026-11-04）。规格（清单、安装、协议、接口、命令行）已经写进 `docs/plugins.md`，以那里为准；
这份留着记设计取舍、实现时和方案的出入、还没做的步骤。issue #1101 定了方向：一个统一底座 + 各模块开放扩展点，骨架学 VS Code（清单 + `contributes` + 按需激活 + 独立进程），
权限学 Chrome（显式声明、安装时展示），WordPress 只借事件通知、不借 filter。

## 一、现状

| | hub 插件（`docs/plugins.md`） | 连接器协议 v2（`docs/connector-protocol.md`） |
| --- | --- | --- |
| 从哪来 | hub.json 的 `plugins[]`，主人手写 | 只有 Telegram：`jarvisd-telegram` 在 personad 旁边就注册，代码里写死 |
| 进程 | hub 起，2 秒起、最长 60 秒退避重启；或只给 `socket`，跑在别处 | `connector/proc` 起，5 秒起、最长 5 分钟退避；连接器关掉就停 |
| 怎么说话 | 插件在 unix socket 上起 HTTP，hub 原样转发 `/api/v1/plugins/<id>/…`；`GET /health` 判活 | stdin / stdout 一行一条 JSON（`internal/rpc`），`init` → 清单 → 各方法；一致性测试 |
| 配置 | `config`（任意 JSON）放进环境变量 `PERSONA_PLUGIN_CONFIG` | `init` 带 hub.json 里它那一节 |
| 密钥 | 自己处理（`key_file`、`key_command`）；还拿到 hub 的完整 token | `secrets.*`：hub 加密存 `connector_secrets`，只给它自己 |
| 权限 | 没有，等于 hub 本身 | 写操作只能经 `actions.do`，过 hub 的闸门 |
| 状态 | 内存 | 开关、选项存 `settings` 表的 `connectorSettingsKey` |

两套各有一半：hub 插件有通用的进程管理和 HTTP 转发，没有清单、密钥、权限；连接器有清单、密钥和协议，但只认一个写死的程序。底座把两边合起来。

## 二、关键决定

1. **一个注册表，三个来源**：内置（personad 自带，清单编进二进制）、hub.json（现有的 `plugins[]`，原样兼容）、安装的（包放在 hub 机器上，登记在数据库）。
   App 和 API 只看到一个列表。
2. **清单是唯一的事实**：能力、配置项、密钥、权限、进程怎么起，全在 `persona-plugin.json`；hub 不运行插件也能列出它、画出配置表单、给用户看权限。
   程序在握手时报的清单只能是它的子集，不一致时以安装时接受的为准（和连接器现在的做法一样）。
3. **进程插件的协议就是连接器 v2 推广出来的**：同一个 `internal/rpc` 一行一条 JSON，握手改名 `initialize`，方法按扩展点分命名空间。
   连接器 v2 的方法原样搬进 `connector.` 命名空间（`status` → `connector.status`），字段和意思一个都不变，原来的一致性测试照跑。
4. **HTTP 转发保留，作为进程插件的第二个出口**：语音要在手机和插件之间直传音频 WebSocket，JSON-RPC 不适合。清单里 `runtime.http: true` 的插件在
   unix socket 上起 HTTP，hub 照旧转发。所以现有的语音插件不用改一行就是一个合法的插件（只有 HTTP、没有 RPC）。
5. **安装只在 hub 机器上和管理界面做，手机不能装**：装插件就是在 hub 机器上跑一个新程序。手机的设备凭据只能看列表、开关、改配置、填密钥；
   安装、升级、卸载要 hub 的主 token（只在 hub 机器的 hub.json 里，命令行 `personad plugin …` 用它走回环）或管理界面的登录会话。
6. **权限第 1 步是「声明 + 展示 + 记录」，不是沙箱**：进程插件以 hub 的系统用户运行，能读写这个用户能读写的一切；网络域名 hub 管不住。
   安装时把这句话和声明的权限一起给人看，接受的那一份存下来；hub 自己提供的能力（给 hub token、以后的知识库读写等）按接受的权限放行，没接受就拒绝。
   真正的隔离（Linux 用 bubblewrap / 网络命名空间，Mac 用 sandbox-exec，或经 hub 的代理出网）以后单独做，见「六」。
7. **配置只支持 JSON Schema 的一个子集**：子集以外的写法装不上，保证每个插件的配置在 App 里都能画成表单，而不是装上以后才发现某个字段没法改。
8. **改了配置、密钥或开关就重启插件进程**：第 1 步不做热更新，少一种状态。连接器自己的「选项」（`connector.Options`）不受影响，照旧热改。

## 三、实现时和方案的出入（1a）

- **管理界面的安装页后补（#1208）**：1a 时安装、升级、回滚、卸载只认主 token 经回环（`personad plugin`）；之后加了管理会话那条路
  （`/api/v1/admin/plugins…`，CSRF、预览一次一个 + 10 分钟 10 次，审计记 `admin UI`），还多了浏览器上传（`upload:<文件名>`）和升级预览的 `added_permissions`。
- **协议多了一个基础方法 `manifest`**：`initialize` 只回 id、版本和贡献，装跑在别处的插件时要完整清单（权限、配置、密钥），所以加了 `manifest`
  （`initialize` 之前也能调；hub 从包里起的插件可以不回）。只有 HTTP 的远端插件回 `GET /manifest`。
- **贡献了连接器的插件由连接器的开关持有**：它的 `on_call` 不算数（否则「关着时登录」会把程序拉起来，和现在的行为不一样）；`on_startup` 照样常驻。
- **连接器程序拿得到用户填的密钥**：`connector/serve.MainWith` 的 `Init.Declared`；原来的 `serve.Main` 不变。
- **内置 Telegram 的版本号是 hub 的版本**（不是语义化版本）：内置插件不过 `Parse`，`engines` 由 `version -v` 的检查代替。
- **列表多了 `can_install`**（hub 有没有数据库），`features` 多了 `plugin_manager`。
- **升级失败自动回滚只看自己会起的插件**（`on_startup`，或连接器开着）：按需起的插件升级时不会为了检查而拉起。
- 迁移号用了 `0291_plugins.sql`（避开其他车道的号段）。

## 四、分步

- [x] **1a：服务端底座**：`internal/plugin`（清单、schema 子集、权限、版本范围、包的暂存和解包、注册表）、`plugin/host`（进程宿主、按需激活、退避、超时、ping、HTTP、远端 socket）、
  `plugin/serve`（程序一侧）、`plugin/conformance`；迁移 `0291`；hub 的接口和 `personad plugin`；语音（hub.json）和 Telegram（内置）迁上来；
  `connector/serve` 认插件开头，`connector/proc` 能经插件进程说话（`Link`）；文档 `docs/plugins.md`、`docs/connector-protocol.md`。
- [x] **1b：App「设置 › 插件」**（iOS，Mac 共用页面）：列表、详情（权限、贡献、关于）、按 `form.fields` 画的配置表单、只写的密钥、开关；
  假 hub `MOCK_PLUGINS=1`、UI 测试和单元测试，默认 / BeautifulUI 风格 × 深浅色截图看过（`docs/plugins.md`「App：设置 › 插件」）。
- [x] 管理界面的「插件」页（#1208）：列表、开关、安装（目录 / 包 / https / 上传 / remote，先预览）、升级（权限变多明确提示）、回滚、卸载（可留数据）。
- [x] 网页的插件页（#1207）。
- [x] Android 的插件页（#1206，`docs/android.md`「插件（设置 › 插件）」）。
- [x] **第 2 步（服务端）**：扩展点 `quota`（插件看不到 Key：`quota.query` + 经 hub / node 代发的 `quota.http`，占位符 `${launcher_key}`、同源限制，权限 `launcher_keys`）、
  `providers`（声明供应商，`GET /api/v1/launchers/presets`，新增启动器带 `preset`）；node 的 `launchers.quota` 换成 `launchers.http`；`server/internal/quota` 只留通用部分，
  两家网关的读法迁到私有插件仓库（归属见 `CLAUDE.local.md`）。规格在 `docs/plugins.md`「额度」「模型供应商」。
- [ ] 第 2 步（App，#1259，进行中）：额度的网关名用 hub 给的 `gateway_name`（不再写死），假 hub 的示例网关换成中性的；设置 › 模型供应商 › 新增「从插件添加」。
- [x] 两个额度插件已在私有插件仓库，装在用户的 hub 上实测：经 node 代发查到 `~/.zshrc` 启动器的额度，不认识的网关回「查不了」。
- [x] 第 3 步（服务端）：lark-connector 整条飞书链路（含公司圈）迁成私有插件，hub 自己的那份已删（3c，#1520），见下面「第 3 步的方案」「第 3 步做下来」。TRAEX 留在开源仓库（已拍板）。
- [x] 第 3 步（客户端，3d，#1530）：飞书界面按 `features` 的 `lark` / `lark_moments` 显示（网页 #1542、iOS / Mac #1575、Android #1622），「公司圈」的通用叫法，帖子标题用存档的 feed 名。
- [x] **第 4 步（服务端，#1284）**：扩展点 `ui`（`plugin_page` / `launcher_section`，`ui.render` / `ui.action`，View 是几种固定的条目，hub 按 `plugin.CheckView` 把关；
  启动器页的一节经 `gateway.http` 用 Key 访问网关，插件看不到 Key）和 `events`（`inbox.changed`、`agent.needs_you`、`agent.finished`，只通知，订阅随权限一起同意）。
  规格在 `docs/plugins.md`「声明式 UI」「事件」。
- [ ] 第 4 步（客户端）：iOS / Mac、Android、网页画 View（插件详情里的页面、启动器页上的一节）。
- [x] **UI 插槽（服务端，#1451）**：插槽表（`nav_page` 主导航入口、`overview_card` 总览卡片、`machine_section` 机器详情的一节），渲染上下文（时区、语言、平台、尺寸、UI 版本），
  `chart` / `stat_grid` / `segmented`、`refresh_after`、`ui.badge` 角标，主导航 / 总览 / 机器详情按贡献算进「它可以」和权限摘要，老 App 由 hub 按 UI 版本拿掉新条目。规格在 `docs/plugins.md`「UI 插槽」。
- [ ] UI 插槽（客户端）：网页、Android、iOS / Mac 画新插槽和新条目。
- [x] **插件市场（服务端，#1303）**：仓库 / 网站发布 `persona-plugins.json`，hub 登记市场（GitHub 私有仓库带只读 token），目录、搜索，完整权限的设备从市场装、升级、卸载；
  命令行 `personad plugin market … / search / install market:…`。规格在 `docs/plugins.md`「插件市场」。
- [x] git 仓库作为市场（#1373）：`git:<地址>`，hub 浅克隆、用 hub 机器的 SSH 凭据，给没有 token 读文件接口的私有 git 托管用。
- [ ] 插件市场（客户端）：iOS / Mac、Android、网页、管理界面的「插件市场」页。

## 第 3 步的方案：lark-connector 迁成插件

审计（2026-11-04）：开源仓库里只对特定服务有意义的东西，最大的一块是经 **lark-connector**（第三方、非官方的飞书网页客户端，以用户身份登录）接的整条飞书链路：
`server/internal/lark` 包（约 3000 行）、hub 的 `lark*.go`、公司圈（飞书 Moments）的收件箱 / 知识库爬取和关联、`lark-connector` 工具、`skills/lark-connector`、node 的写闸门、
一批提示词和 golden，以及客户端按 lark-connector 的数据形状画的飞书聊天页。已拍板：**整条迁到私有插件仓库**；开源版保留官方 `lark-cli`、飞书机器人、飞书邮箱这些公开能力。

目标：装上插件，飞书在 App 里和现在一模一样；不装，开源版没有 lark-connector 相关的任何东西（代码、提示词、文档、测试数据），飞书只走 `lark-cli`。

### 新扩展点（开源仓库，通用）

| 扩展点 | 插件提供 | hub 做什么 |
| --- | --- | --- |
| `api`（版本 1） | `{id, prefix, features?, events?}`：一段 App 用的 HTTP 接口，在插件的 HTTP 一侧 | 把 `/api/v1/<prefix>/…` 转给插件（鉴权在 hub 做完，同 `/api/v1/plugins/<id>/…`）；`prefix` 不能和 hub 自己的接口撞；`features` 加进 hello / `/health` 的 `features`（客户端靠它决定显不显示）；插件发 `api.broadcast {type, …}` 推给在线的客户端，`type` 只能是 `events` 里声明的 |
| `connectors`（已有，补两项） | 收件箱种类的说明（`kinds` 带 `{kind, description}`，给模型分拣用，替代开源仓库里的 `prompts/inbox/kinds/<id>.md`）；知识库条目（`items`）已有 | 照旧 |
| `assistant`（版本 1） | 技能目录（包里的 `skills/<name>/`）、助手说明的片段（markdown）、给编程 Agent 的工具说明 | 技能随助手的其他技能一起下发到各台电脑；片段接在助手的 CLAUDE.md 后面（提示词 golden 照旧守开源部分） |

### 分阶段

1. **3a 扩展点**（开源仓库，#1262 已做）：`api`、`assistant`、连接器的种类说明；hub 里飞书相关的入口改成走注册表（没有提供 `lark` 的插件时，飞书相关的接口、工具、收件箱来源都不出现，App 按 `features` 不显示）。
2. **3b 插件**（私有插件仓库 `lark-connector/`）：把 `internal/lark` 和 hub 里 lark-connector 的逻辑搬过去，做成进程插件：`api`（现有 `/api/v1/lark/…` 接口原样，数据形状不变，App 不用改）、`connectors`（工具、收件箱、知识库归档和条目、实时事件）、`assistant`（lark-connector 技能和说明、写闸门规则）、`/agent`（经 `hub_api`）。对着用户 hub 上的真实 lark-connector 验证。
3. **3c 清理**（开源仓库）：删掉 `internal/lark` 里 lark-connector 的部分、hub 的 `lark*.go` / 公司圈、提示词和 golden、`skills/lark-connector`、node 的写闸门、同步脚本、文档和测试数据里的相关内容；`lark-cli`、飞书机器人、飞书邮箱留着。部署，在用户的 hub 上装插件，确认飞书收件箱、聊天、知识库、助手都和以前一样。
4. **3d 客户端**：飞书聊天页按 `features` 显示（插件提供时）；公司圈的界面改成通用的「飞书公司圈（Moments）」，名字和分组来自插件，不写死任何租户的名称。

每一阶段一个 PR；3b、3c 互相依赖，3c 在插件装好验证后再合。

### 第 3 步做下来（2026-11-04）

插件接管的过程中 hub 补了这些通用能力（每条一个 issue / PR；规格在 `docs/plugins.md`、`docs/connector-protocol.md`）：

| 缺口 | 做法 |
| --- | --- |
| 连接器 id `lark` 被 hub 预留，插件装不上 | 可迁出的连接器 id（`connector.MigratableConnectors`）：插件启用时 hub 的让出、停用时放回；3c 之后 hub 没有自己的了，只是不预留（#1320） |
| 转发插件 HTTP：`Cache-Control` 叠成两份、`%2F` 被拆开 | 缓存头以插件的为准；转义的路径原样到插件（#1320） |
| 收件箱条目原有的块（聊天、文档的行、公司圈帖子）和旧条目接续 | 连接器的 thing 带 `extra`，hub 只对可迁出的连接器读；旧条目按块对到 thread 上，新条目用插件给的 `legacy_id`；回复和改状态经 `inbox.reply` 的 `client_id`、`changes`（#1320） |
| 知识库的日程 / 会议 / 文档标题、会话里的附件 | 协议加 `items.list`、`items.doc_title`、`archive.media`（#1372） |
| 公司圈全量进知识库、和我有关的帖子 | 程序调 hub 的 `posts.put` / `posts.comments` / `posts.pending`、`archive.chats`，清单 `archive_sources`；帖子存档、清理、挂项目在 hub（#1444） |
| 助手的写闸门和凭据 | `assistant.gate`（包里的闸门文件）+ `assistant.credentials` / `assistant.summary`；node 的闸门、命令包装、运行时工具按 hub 给的闸门走（#1468） |
| /agent | 开对话带 `purpose`、`machine_id`；`GET /api/v1/kb/chats/{id}/context`；插件接管时 hub 的 `lark_agent_reply` 和 /agent 让出（#1484） |
| 个人 Agent（Agents 页的一个来源） | `agent_sources` 扩展点（#1400）+ 可迁出的来源 id，会话 id 不变（#1497） |
| 发送回执、hub 互联的角色、两份实时事件 | `api` 的 `receipts`；`initialize` 的 `hub.role`，角色变了重启插件；插件接管时 hub 的事件循环停下（#1503） |
| 助手快照和「今天」卡片里的未读会话、今天的会 | 连接器能力 `snapshot`（`snapshot.get`）（#1520） |
| 客户端按插件显示飞书 | 插件的 `api` 报 features `lark`、`lark_moments`（#1530） |

经验：

- **先补能力、在真实 hub 上让插件接管并验证，再删 hub 自己的**：接管期间两份并存，每一处 hub 自己的循环（实时事件、/agent、爬取、个人 Agent）都要有「插件接管时让出」的判断，
  漏一处就会出现两份（/agent 的事件处理就漏过：判断写在了处理之后）。
- **数据的接续靠 id 不变**：连接器 id、知识库的会话 id（`<来源>-<id>`）、收件箱条目 id、个人 Agent 的会话 id、发送回执的派生 id 都照原样，
  插件第一次接手时从 hub 已有的状态接着（`archive.chats`、旧条目按块对上），不重读、不重复、不漏。
- **插件仓库里的代码可以有内部细节，开源仓库只留扩展点**：闸门规则、提示词、技能、客户端数据形状里只对一个服务有意义的部分都放进插件包，hub 只认通用的格式。
- 没装插件时，开源版和「没配 lark-connector」时一样：飞书只走 lark-cli 和机器人；hub.json 里旧的 `lark.container / token / base_url`、`lark_agent`、`kb.moments` 不再读，
  有的话 hub 启动时记一行日志，迁到插件配置（容器名、地址、账号、/agent 开关和启动器、公司圈全量）的步骤见下。

hub.json 的旧配置迁到插件：装上插件后在 设置 › 插件（或 `personad plugin config <id> key=value`）填容器名（或地址 + 密钥里的 token）、账号；
`lark_agent.enabled` / `launcher` / `machine` 对应插件的 `agent` / `agent_launcher` / `agent_machine`；`kb.moments.enabled` 对应 `moments_crawl`。确认插件接管后再删 hub.json 里的这几段。

## 五、现有的两套怎么迁上来

### 语音插件（hub.json 的 `plugins[]`）

- hub.json 里的每一项照旧读，hub 替它合成一份清单：`{id, name, version: <hub 的构建号>, runtime: {command, args, env, rpc: "none", http: true}, contributes: {<kind>: [{id}]}, permissions: {hub_api: true}}`；
  只给 `socket` 的是 `remote:`，hub 不起进程。`dir`、`env`、`config`、`disabled`、`command: "personad" / "jarvisd"` 的意思都不变。
- 行为不变：socket 路径、环境变量（包括 hub 的 token）、`/health` 判活、转发时去掉 App 的凭据、手表那种只读设备 403、模型配置的 `plugin.<id>`。
  唯一的变化是退避上限从 60 秒变成 5 分钟。
- `GET /api/v1/plugins` 的老字段不变（Android 的语音入口按 `kind == "voice"` 和 `status == "running"` 找），只多字段。
- 语音的配置（`providers` 是对象数组）超出表单的子集，第 1 步不进注册表的配置，照旧在 hub.json；以后子集支持对象数组了再说。
- 现有的 `server/internal/hub/plugins_test.go` 不改断言，照旧要过。

### Telegram（连接器 v2）

- 成为一个内置插件：清单编进 personad（`id: "telegram"`，`runtime.command` 是 personad 旁边的 `jarvisd-telegram`，`contributes.connectors: [spec.Manifest()]`），
  程序在才注册，和现在一样；`engines` 要求和 hub 同一个构建（现在的 `version -v` 检查保留）。
- 连接器 id、`connector_secrets` 里的会话、连接器设置里的开关和选项、默认关着都不变。内置插件的启停和连接器自己的开关是两层：插件默认启用，用户平时只碰连接器的开关。
- 连接器的一致性测试（GitHub、信息、Telegram）照跑，并对 `connector/serve` 的两种开头各跑一遍；Telegram 的 proc 测试改成经插件宿主起程序。

## 六、不做 / 以后

- 沙箱（文件、网络的真正隔离）、CPU / 内存限制、插件签名、registry：第 1 步都不做，接口留了位置（权限词表、`origin`、`engines`）。
- 插件之间的依赖、插件调别的插件：不做。
- WordPress 的 filter（插件改 hub 的数据）：永远不做；事件钩子只通知。
- hub 互联：插件属于装它的那台 hub，第 1 步不在组内同步；App 经中心 hub 看到的是中心的插件。
- 清单的多语言（`name`、`description`、表单标题）：先只有一种，以后加 `l10n`。

## 七、插件在指定电脑上运行（node 侧程序，方案，#1830）

**要解决什么**：现在插件的程序只在 hub 上跑（`runtime`），有的能力却只能在某台电脑上做：

- iMessage：读 iMessage 的数据库。现在是写死在 node 里的 5 个方法（`connector.*`），由 hub.json 的 `imessage.machine` 指定一台机器。
- cua / ego：驱动 cua-driver、ego lite。现在是编进 personad 的 node 代码（`node/computer*.go`、`node/browser*.go`），加上只有声明的内置插件。
- 第三方插件：做不了这类事。

### 清单：`node` 段

和 `runtime` 并列。两段可以都有（hub 上一个程序、电脑上一个程序），也可以只有一段。

```json
"node": {
  "command": {"darwin-arm64": "node/bin/helper", "darwin-amd64": "node/bin/helper-x64", "linux-amd64": "node/bin/helper-linux"},
  "args": [], "env": {},
  "machines": "machines",
  "rpc": "stdio",
  "http": true,
  "keep_running": "lease",
  "secrets": ["api_key"],
  "permissions": ["network", "files", "macos_privacy"]
}
```

各字段：

- `command`：和 `runtime.command` 的写法一样，按平台取（`Command.For`）。支持哪些平台就看它有哪些键，不另写平台列表。
- `machines`：用哪个配置字段选机器。这个字段必须是 `type: array`、`items.format: machine`，开关照旧是每台机器一个（`format: machine` 已有的画法）。
  `items` 的 `x-os` 要写，且只能是 `command` 有程序的系统（hub 校验），不支持的机器选不了；只能一台就在这个字段上写 `maxItems: 1`。
- `rpc` / `http`：插件协议 1 走 stdio；要 HTTP 就在 `PERSONA_PLUGIN_SOCKET` 上开，和 hub 侧一样。
- `keep_running`：hub 连不上时这个程序怎么办，见下「node 离线时」。
- `secrets`：发给 node 的是哪几个（`secrets[]` 里声明过的名字），其余只留在 hub。
- `permissions`：node 侧的权限，见下「权限展示」。

校验：

- `machines` 指向的字段要存在、形状对，`x-os` 和程序的平台对得上；
- `secrets` 只能写 `secrets[]` 里有的；
- 装的时候不再要求 hub 的平台有程序，只要 `runtime`（如果有）能在 hub 上跑、`node` 至少有一个平台。

### 安装、升级怎么下发到 node

**包**：

- 同一个包里放它支持的各平台的 node 程序（比如 `node/` 目录），装的时候每个都检查在、可执行。
- 市场不用改：它按 hub 的平台挑一个包，这个包里带着所有电脑平台的程序（包会大一些；以后真嫌大，再给市场条目加按平台的 `{url, sha256}`）。
- 装好后各平台的程序就在 `installed/<id>/<ver>/` 里，hub 给每台机器发它那个平台的。

**下发**：

- 新的 RPC `plugin.node.put`（begin / chunk / commit，1 MB 一块，带 sha256），分块和校验复用 `node.update.*` 的代码；
- 按内容摘要去重，和 skills 下发（`assistant.prepare`）一样，同一版本不重复传；
- node 上放在 `~/.persona/plugins/<id>/<ver>/`，数据目录 `~/.persona/plugins/<id>/data/` 跨版本保留。

**升级**：

- hub 侧先升（现在的流程），再逐台升：下发新版本 → node 停旧的、起新的 → 程序 30 秒内 `initialize` 成功才算好，否则 node 自己退回上一版。
- 和 hub 侧一样只留一个旧版本。
- 每台的结果记下来，设置页里显示。

**版本**：

- node 侧和 hub 侧必须是同一个版本（同一个包）。
- 离线时错过了升级的机器，连上后先补下发。补上之前对它的调用回 409 `plugin_updating`，不拿旧程序硬跑。

**停用、去掉一台、卸载**：

- node 停掉程序、删掉版本目录；
- 卸载时数据目录一起删，停用、去掉一台时留着。

### node 离线时

**对账，不排队**：

- hub 只存「应该是什么样」：Postgres 里插件的 config、版本、选了哪些机器（都已经有）。
- node 每次连上（`Machine.serve` 的重连钩子，和 `refreshComputer` 在同一处），hub 发一次全量对账 `plugin.node.sync`：应该跑哪些插件、哪个版本、摘要。
- node 补下发缺的、停掉不该跑的、删掉不在名单上的。
- 所以离线期间的安装、升级、停用、卸载都不用排队，连上就对齐。

**离线时调用**：

- 和现在 cua / ego 一样，HTTP 回 409「机器离线」，插件的 hub 侧收到错误。
- App 设置页每台机器显示「运行中 / 离线 / 待更新 / 出错」，出错时带原因。

**hub 连不上 node 时，电脑上的程序**：

- `keep_running: "lease"`（默认）：hub 每 10 分钟续一次租约（同 `computer.nolock`），超过 30 分钟没续，node 停掉它，连上再起。
- `keep_running: "always"`：hub 断开时不停，给 iMessage 这种要一直收、hub 重启也不该断的。

**密钥不落 node 的盘**：

- 只在 `plugin.node.start` 里经现有的 hub↔node 连接带过去，在内存里交给程序（`initialize.secrets`）。
- 所以 node 重启后，程序要等 hub 连上才起。`always` 只是「hub 断开时不停」，不是「node 重启后自己起」。

### hub 怎么起停、转发

**宿主**：把 `plugin/host` 里的进程管理（起停、退避重启、关 stdin → SIGTERM → SIGKILL）抽成 hub 和 node 共用的，node 上跑同一套。

**RPC**：

- `plugin.node.start{id, version, config, secrets}`、`plugin.node.stop{id}`、`plugin.node.status`；
- `plugin.node.call{id, method, params}`：转给程序的插件协议方法；
- 程序发的通知（`changed` 等）由 node 转成 `plugin.node.event` 发给 hub，hub 交给 hub 侧程序或连接器框架。

**hub 侧程序调电脑上的程序**：插件协议加 `nodes.call{machine, method, params}`。有 `node` 段就能用，不另加权限。只有 `node` 段的插件，hub 把 App 的请求直接转过去。

**HTTP**：

- 路由 `/api/v1/plugins/{id}/machines/{mid}/…`：hub → 流（`internal/streams`，现在只拨 TCP 端口，加一个「拨插件的 unix socket」的目标）→ node 上程序的 socket。
- 普通请求和 WebSocket 都走流。和 hub 侧的 HTTP 一样，只给 full 设备，去掉 `Authorization`。

### 权限展示

**「运行程序」那行按位置写**（`perm.Explain` 现在固定写「在 hub 机器上运行程序」）：

- 有 `runtime`：「在 hub 机器上运行程序」；
- 有 `node`：「会在这些电脑上运行程序：MacBook、Mac Studio」，列实际选中的机器，没选时写「会在你选的电脑上运行程序」。

**node 侧的权限另起一组**：

- `network`：联网；
- `files`：读写自己数据目录以外的文件；
- `macos_privacy`：会请求完全磁盘访问、辅助功能这类系统权限。

**要重新同意的情况**：

- `perm.Digest` 把 node 段（平台、权限、发给 node 的密钥名）也算进去，变了就要重新同意（现有的 `Added` 列出新增的）。
- 给插件加一台机器也要确认：打开那台的开关时，弹出「将在 <机器> 上运行 <插件> 的程序」（`x-confirm`）。

### cua / ego 怎么迁

**第一步只换路，不换代码**：

- 它们的 node 代码留在 personad 里，注册成「内置的 node 侧程序」：进程内实现同一个接口，不起子进程，清单里 `node.command` 写 `builtin`。
- hub 改用 `plugin.node.call{id: "cua", …}`。旧的 `computer.*`、`browser.*` 方法名保留一个版本，给还没升级的 node。
- `machines` 字段、机器页上的开关、`PUT /machines/{mid}/computer|browser` 都不变，用户看不到变化。

**换来的**：

- 权限页的「会在这些电脑上运行」、离线对账、设置页里每台机器的状态，三个插件共用一套；
- `refreshComputer`、`refreshBrowser`、`tellNoLock` 这类各写一份的重连代码，收进 `plugin.node.sync`。

**以后**：要拆成独立的程序（比如挪到私有仓库）时，再按普通 node 程序发；现在没有这个需要，不拆。

### iMessage 怎么用它

- **成为插件 `imessage`**：`node.command` 只有 `darwin-*`，选机器的字段 `maxItems: 1`，`keep_running: "always"`。
- **node 程序**：就是现在的 Reader（`connector/imessage`），在 stdio 上按连接器协议回答 `connector.list/chats/older/changes/search`。
  hub 侧的连接器框架把调用经 `plugin.node.call` 转过去，把 `changed` 转回来。连接器的一致性测试照跑。
- **配置**：从 hub.json 的 `imessage.machine` 换成插件的 `machines` 字段。迁移时第一次启动读旧值写进去，旧字段保留一个版本、只读。「交给 Persona」的开关不动。
- **完全磁盘访问**：macOS 把子进程的权限算在「负责的进程」，也就是拉起它的 node 头上，所以第一步和现在一样，要给 node 授权。
  `plan-connectors.md` 里「有自己权限的 helper」要做成 LaunchAgent，留作后续：`node.launch: "launchd"`，由 node 用 launchd 起它，第一步不做。
- **以后的发送**：走闸门（`connector_gate`），和现在的规划一样。

### 分步

1. 清单的 `node` 段、校验、权限文案和摘要、包里带 node 程序：只动 hub，有测试。node 还不会起程序之前，有 `node` 段的包装不上（`plugin.NodePrograms`），第 2 步打开。
2. node 侧宿主和 `plugin.node.*`：下发（分块、摘要、去重）、起停、对账、租约，加上 hub 侧的重连钩子；用假 node 和真进程测试。
3. HTTP / WebSocket 转发：流能拨 unix socket。
4. App（iOS、Mac、网页）：每台机器的状态、加机器时的确认、权限页的新行；截图给用户看。
5. cua / ego 换成内置的 node 程序，不改界面。
6. iMessage 迁成插件，迁移 hub.json 里的配置。

第 1–3 步做完就能装第三方的 node 侧插件；第 5、6 步各自一个 PR。

### 拍板结果（2026-11-05，用户四点都按方案定）

- **hub 连不上时，电脑上的程序**：默认停，租约 30 分钟（hub 每 10 分钟续一次）；插件可以声明 `keep_running: "always"`。
- **版本**：node 侧必须和 hub 侧一致（同一个包），不允许范围。
- **只有 `node` 段、没有 hub 侧程序的插件**：允许，App 的请求直接转到 node。
- **node 侧程序能不能调 hub 的 API（`hub_api`）**：不给；要用 hub 的能力，经插件自己的 hub 侧程序。
