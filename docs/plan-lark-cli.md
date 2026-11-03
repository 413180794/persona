# 计划：没有 lark-connector 时用 lark-cli 接飞书

状态：方案（#273），2026-11-03 用户拍板了四项（见「已拍板」），按「分步计划」做，做完的步骤打勾；全部做完后把设计部分并进
`docs/feishu.md`、`docs/knowledge.md`、`docs/inbox.md`，删掉这份计划。

## 背景

现在 persona 的飞书能力全部建立在 lark-connector 上（`docs/feishu.md`）：收件箱、知识库归档、实时事件、AI 分身、飞书里的 `/agent`、
助手的 lark-connector skill。没有 lark-connector 的 hub 就完全没有飞书。

飞书官方的 [lark-cli](https://github.com/larksuite/cli)（npm `@larksuite/cli`，MIT）用开放平台应用 + 用户授权读写飞书，任何人都能装。
目标：只装 lark-cli、不配 lark-connector 的 hub 也能给 Persona 和 AI 分身提供飞书上下文。lark-connector 的现有行为不变，只是多一种来源。

不做：不为飞书做浏览页面（`docs/feishu.md`「不做第二个飞书」）；不替换 lark-connector。

## lark-cli 能做什么（1.0.72 实测，2026-11-02）

全部用用户身份（`--as user`），输出 JSON（`--format json`）。

| 能力 | 命令 | 说明 |
| --- | --- | --- |
| 会话列表 | `im +chat-list --types p2p,group --sort active_time` | 有 `chat_id`、名字、类型、单聊对象；**没有未读数、最后一条消息、@我标记** |
| 读消息 | `im +chat-messages-list`、`im +threads-messages-list`、`im +messages-mget` | `message_id`、`create_time`、发送者（带名字）、`content`（开放平台的 JSON）、话题位置 |
| 搜消息 | `im +messages-search` | 不带关键词也能搜：只给 `--start` 就是这段时间里我能看到的全部消息；`--is-at-me`、`--chat-type p2p`、`--sender`、`--query` 可叠加；结果带会话名、`mentions`、`thread_id` |
| 发送 | `im +messages-send`、`im +messages-reply`（可回在话题里、markdown）、`im reactions create / delete` | 以用户身份 |
| 文档 | `docs +fetch --doc-format markdown`、`drive +search`、评论（`drive`）、`sheets`、`base` | 没有 lark-connector 那种 JSON 节点树、`header`、`ai-summary` |
| 日程 / 会议 / 妙记 | `calendar +agenda`、`vc +search / +detail`、`minutes +search / +detail` | |
| 人 | `contact` | 按姓名搜、按 id 查 |
| 实时事件 | `event consume <EventKey>` | IM 类事件（`im.message.receive_v1` 等）**只推给机器人**；用户身份只有审批、妙记、会议、任务几类 |
| 风险等级 | 每个命令的 `--help` 打印 `Risk: read / write / high-risk-write`（约 50 ms）；`schema <方法> --json` 带 `risk`、`danger` | 闸门可以直接用 |
| 登录 | `auth login`（Device Flow，没有浏览器的机器也能登，手机上点链接完成）、`auth status`、`whoami`（JSON） | access token 2 小时，refresh token 7 天（`auth status` 的 `refreshExpiresAt`）：一周没用过就要重新登录 |
| 领域说明 | `lark-cli skills list / read <名字>` | skill 编进 CLI，和版本同步；Agent 按需读，不用拷进仓库 |

## 对照表：现在用到的 lark-connector 能力

「需要」一栏：未读 = 飞书里的未读 / 已读位置；feed = 会话列表的未读 / @我 / 完成标记；事件 = 实时推送。这几样开放平台都没有。

### 收件箱（`hub/inbox*.go`）

| 现在 | lark-cli | 降级 |
| --- | --- | --- |
| 候选会话：未读会话 + 任意处 @我（`conversations?scope=unread / mentions`，需要 feed、未读） | 每次轮询一次 `+messages-search --start <上次>`，在 hub 里筛：@ 了我、私聊、我参与的话题里的新回复（见「收件箱收什么」） | 收「@我」、私聊、我参与的话题的回复；群里不在这三类的未读不进收件箱。「处理过」照旧只记在 Persona 里 |
| 条目上下文、我回过没有（`Messages` / `threads` + `isMine`） | `+chat-messages-list`、`+threads-messages-list`，发送者是我 | 能做 |
| 文档通知：@我、评论、分享（`documents/notifications`） | 无 | 第一版不做 |
| 文档、表格、多维表格的读写和评论回复（`documents/*`） | `docs`、`sheets`、`base`、`drive` 评论 | 能做，适配层重写 |
| removed（`moments/*`） | 无 | 不做 |
| 发送确认过的回复（`Send` / `Reply`） | `+messages-send` / `+messages-reply` | 能做 |

### 知识库（`hub/kb*.go`）

| 现在 | lark-cli | 降级 |
| --- | --- | --- |
| 最近活跃的 100 个会话（`conversations?scope=chats`） | `+chat-list --sort active_time` | 能做 |
| 新消息、往回补（`Messages` 按 position） | 每次轮询一次 `+messages-search --start <游标>` 拿全部新消息；补旧的用 `+chat-messages-list` 按时间往前翻 | 游标从 position 换成 `create_time` + `message_id` |
| 附件（`media`） | `+messages-resources-download` | 能做 |
| 人、群（来自发送者和会话名） | 同左，`contact` 补全 | 能做 |
| 日程、会议、文档标题（`calendar/week`、`meetings`、`documents/read`） | `calendar +agenda`、`vc +search`、`minutes +search`、`docs +fetch` | 能做 |
| removed | 无 | 不做 |

### 实时事件（`hub/lark_events.go`）

| 现在 | lark-cli | 降级 |
| --- | --- | --- |
| `/events` WebSocket 踢收件箱、知识库、`/agent`，给 App 推 `lark` | 无 | 轮询：收件箱、归档 2 分钟（和现在的兜底间隔一样），`/agent` 20 秒；App 收不到 `lark` 推送，打开页面时自己拉 |

### 飞书里的 `/agent`（`hub/lark_agent*.go`）

| 现在 | lark-cli | 降级 |
| --- | --- | --- |
| 从事件里认出自己发的 `/agent`（需要事件） | `+messages-search --sender <我> --query /agent --start <10 分钟前>`，20 秒一次，按 `message_id` 去重 | 延迟最多约 20 秒，加上搜索索引的延迟（待测） |
| OnIt / DONE 表情 | `reactions create / delete` | 能做 |
| 上下文：话题、前 40 条、引用、文档 | `+threads-messages-list`、`+chat-messages-list`、`+messages-mget`、`docs +fetch` | 能做 |
| 回复（markdown，回在话题里） | `+messages-reply` | 能做 |
| 文档评论里的 `/agent`（`documents/recent` + 评论） | 没有「最近打开的文档」 | 第一版不做 |

### 助手（`node/larkgate.go`、`hub/assistant_tools.go`）

| 现在 | lark-cli | 降级 |
| --- | --- | --- |
| lark-connector skill：`ol METHOD PATH` | `lark-cli <领域> <命令>`；Persona 内置一个短 skill 指路，细节让 Agent 用 `lark-cli skills read` 读 | 能做 |
| 写操作过 `ol` 闸门、手机确认 | 闸门认 `lark-cli`，按它自报的风险等级判断（见下面「闸门」） | 能做 |
| `lark_chats`（未读 / @我 / 最近） | 最近：`+chat-list`；@我：搜索 | 没有「未读」过滤 |
| `lark_messages`、`lark_search`（消息、文档、人、日程、妙记、待办、邮件） | `+chat-messages-list`；`+messages-search`、`drive +search`、`contact`、`calendar`、`minutes`、`task`、`mail` | 能做 |
| 快照：收件箱会话、本周日程 | 最近的 @我 / 私聊 + `calendar +agenda` | 没有未读数 |

### App 的飞书路由（`hub/lark.go`）

| 现在 | lark-cli | 降级 |
| --- | --- | --- |
| 会话页：消息、话题、发送、回复、表情、成员、媒体 | 都有对应命令 | 适配成现在的 JSON，App 不改 |
| 未读数、标签、置顶、已读位置、`read-states` | 无 | 这个来源下不显示 |
| 文档阅读器（`raw/documents/*`） | `docs +fetch --doc-format markdown` | 只有 markdown，没有 JSON 树、摘要 |
| removed、`url-preview`、表情列表、个人 Agent 桥（`agent-bridge`） | 无 | 不做；表情用本地的静态表 |

## 形态

- **连接器**：做成 `docs/plan-connectors.md` 框架上一个内置连接器 `larkcli`。框架里已有 `Accounts`、`ReadTools`、`Actions`、`Inbox`、`Archive`、`Runner`，
  它按需实现这些能力，hub 自动接到知识库、收件箱、助手和 App；框架里还没有的（比如 `Items`）按连接器计划在用到的那一步补上。lark-connector 的管道一行不改，等连接器计划的 M1–M6 再搬；不在各条管道里写 `if lark-connector … else …`。
- **两种来源可以并存**（用户 2026-11-03 定）：lark-connector（`lark.token` / `lark.container`）和 lark-cli（`lark.cli`）可以同时配。规则：
  - **每样能力一个主来源**：`lark.prefer`（默认 `lark-connector`）先用；它没有这项能力（文档评论、妙记…以实际实现为准），或者连不上、掉了飞书登录，就用另一个。
    切换写进日志和设置页，不静默。
  - **收件箱和知识库同一时刻只从一个来源收**，另一个只在主来源不可用时接上，避免同一条消息进来两次。
    两边的 id 不同（开放平台是 `oc_` / `om_`，lark-connector 是 feed id 和 position）：知识库按来源分 id 前缀（lark-connector 照旧，lark-cli 用 `larkcli`），不会相互覆盖；
    切换来源期间的同一条消息按「会话 + 发送者 + 发送时间 + 正文」去重。同一个会话能不能对上两边的 id（lark-connector 的会话信息里有没有开放平台的 `chat_id`）L3 先查，
    对得上就合成一个会话。
  - **助手两个都给**：lark-connector skill 和 lark-cli 都装着时，提示词说明各自擅长什么（lark-connector：未读、feed、removed；lark-cli：开放平台的文档、日程、妙记、审批…），闸门两个都认。
- **调 CLI，不直接调开放平台**：hub 以子进程跑 `lark-cli … --as user --format json`。凭据和刷新都留在 lark-cli 自己的存储里，hub 不碰应用密钥和 token；
  一次调用 100–300 ms，轮询的量够用。所有调用和 JSON 适配集中在一个包（`internal/larkcli`），用录下的 JSON 做契约测试，固定支持的最低版本。
- **装在哪**：hub 所在的机器（也是助手跑的机器）。`lark-cli auth login` 由用户自己在那台机器上做，hub 只读 `auth status`。
- **先支持 lark-cli**：官方、MIT、命令自带风险等级、skill 编进 CLI。社区的 feishu-cli 以后有人要再加一个适配器，连接器不变。

## 配置（`hub.json`）

```json
"lark": {
  "cli": { "bin": "lark-cli", "profile": "" },
  "prefer": "lark-connector"
}
```

- `lark.cli.bin`：命令名或绝对路径，默认在登录 shell 的 PATH 里找（npm 全局装的常在 nvm 目录）。没有 `lark.cli` 就不启用。
- `lark.cli.profile`：可选，lark-cli 的配置档（`--profile`）；空的用它的默认档。hub 和助手的每次调用都带上它。
- `lark.prefer`：两种来源都配了时谁是主来源，`lark-connector`（默认）或 `cli`（见「形态」）。
- 应用凭据不进 `hub.json`：lark-cli 用它自己 `config` 里绑定的应用。
- **权限能开的都开**（用户 2026-11-03 定）：登录用 `lark-cli auth login --domain all`（全部领域的用户权限；没有浏览器的机器加 `--no-wait --json` 走 Device Flow，
  在手机上点链接授权）。要管理员审批的权限审批前拿不到：设置页用 `auth check` 列出还缺哪些，缺了的功能说明是哪个权限，不报成连不上。
- 设置 › 连接器 › 飞书：显示两个来源各自的状态、哪个是当前的主来源、lark-cli 登录的账号、token 什么时候过期、缺的权限；过期时提示在 hub 机器上 `lark-cli auth login`。

## 收件箱收什么（lark-cli 来源）

用户 2026-11-03 定：**@我、和我的私聊、话题群里我参与的话题的新回复**。开放平台没有未读，所以按消息本身判断，每次轮询一次搜索：

1. `+messages-search --start <游标> --as user --page-all` 拿上次以来我能看到的全部新消息（不带关键词），游标是上一轮最新的 `create_time`，往回多搜 1 分钟、按 `message_id` 去重，补上搜索索引的延迟。
2. 去掉我自己发的，留下：
   - `mentions` 里有我（或 @所有人）；
   - 私聊（`chat_type` 是 `p2p`）；
   - `thread_id` 在「我参与的话题」里：我发过、回过或被 @ 过的话题。这个集合从历史搜索（`--sender <我>`）和归档里建起来，之后每看到我在某个话题里发言就加进去；
     话题群（`chat_mode` 是 `topic`）和普通群里的话题回复都算。
3. 同一个会话或话题里连着的几条聚成一件事（和现在的收件箱一样一行一件事），上下文用 `+threads-messages-list` / `+chat-messages-list` 取。
4. 我在这件事里回过（之后有我发的消息）就当已回复，和现在的 `isMine` 判断一样。

一次轮询的搜索可能翻很多页（大群多的租户）：`--page-limit` 封顶，超出的下一轮接着翻，量在 L4 实测。

## 闸门（第一步就做）

助手的每个 Bash 调用都过 `node/larkgate.go` 的 PreToolUse 闸门，现在只认 `ol`。加一支认 `lark-cli`：

| 命令 | 处理 |
| --- | --- |
| `--help`、`--version`、`schema`、`skills`、`--dry-run`、`whoami`、`doctor`、`auth status / check / scopes / list`、`event list / schema / status` | 放行 |
| `auth login / logout`、`config …`、`profile …`、`update`、换 `--profile`、读 lark-cli 凭据文件 | 拒绝：登录、换应用、换配置由用户自己做 |
| `api GET / HEAD …` | 放行 |
| `api` 的其他方法 | 手机确认，卡片上是方法、路径、`--data` |
| 领域命令（`im +messages-send` 等） | 用同样的词加 `--help` 问 lark-cli 的风险等级（按命令缓存）：`read` 放行；`write`、`high-risk-write`、问不出来一律手机确认，卡片上是整条命令 |
| `event consume` | 放行（只读），提示词要求带 `--max-events` / `--timeout` |
| 闸门读不懂的命令（循环、`$(…)`）里有 lark-cli 的写 | 整条命令手机确认（和 `ol` 一样） |

`hub.json` 的 `lark_gate` 规则照样先匹配，lark-cli 的调用写成 `{"method": "CLI", "path": "/im/+messages-send", "action": "allow"}`（命令的词用 `/` 连起来）。
Codex 的 app-server 审批走同一个 `larkCheck`。

## 分步计划

- [ ] **L0 方案**：这份文档（本 PR）。
- [x] **L1 助手**：闸门认 lark-cli（`node/larkcligate.go`）；配置 `lark.cli`、`lark.prefer`；助手 PATH 上的 `lark-cli` 包装；提示词（CLAUDE.md 多一段，
  不另做内置 skill：领域说明让 Agent 用 `lark-cli skills read` 读 CLI 自带的）按配了哪些来源说明用哪个；`docs/feishu.md`、`docs/config.md`、
  `docs/assistant.md`「飞书：lark-cli」，写明 `auth login --domain all`。
  验收：助手能查最近的消息、文档、日程；发消息弹确认卡片，拒绝后不发；配了 lark-connector 的 hub 行为不变（原有 golden 不变）。
- [ ] **L2 连接器骨架**：`internal/larkcli` 适配层 + `larkcli` 连接器的 `Accounts`（`auth status`、过期提醒，接设置页）和 `ReadTools`（`lark_chats` / `lark_messages` / `lark_search` 的 lark-cli 版）。
- [ ] **L3 知识库**：`Archive`（会话、消息、人、群：搜索轮询 + 往回补；附件）、`Items`（日程、会议、妙记、文档标题）。验收：知识库里有飞书会话和人，格式和 lark-connector 来源一样。
- [ ] **L4 收件箱**：`Inbox`：@我、私聊、我参与的话题的回复（「收件箱收什么」）；和 lark-connector 并存时只从主来源收；回复走 `Actions`，经闸门和手机卡片。
- [ ] **L5 `/agent`**：搜索轮询认请求、表情、上下文、回复；文档评论里的 `/agent` 不做。
- [ ] **L6 App**：会话页的消息、发送、回复、表情、媒体接到 lark-cli（同样的 JSON）；文档阅读器用 markdown；未读、标签、removed在这个来源下隐藏。

每一步一个 PR，配了 lark-connector 的 hub 现有测试全过。

## 已拍板（用户 2026-11-03）

| 问题 | 决定 |
| --- | --- |
| 一个 hub 一种来源，还是两种并存 | **允许两种并存**：每样能力一个主来源（`lark.prefer`），主来源不可用时换另一个；收件箱、知识库同一时刻只从一个来源收（「形态」） |
| lark-cli 来源的收件箱收什么 | **@我、和我的私聊、话题群里我参与的话题的新回复**（「收件箱收什么」） |
| 轮询频率 | **先按方案定的**：收件箱、归档 2 分钟，`/agent` 20 秒；开放平台的限流在 L3 实测后再调 |
| 权限 | **能开的都开**：`lark-cli auth login --domain all`；要管理员审批的在设置页列出来（「配置」） |

## 风险

- **搜索索引的延迟**：`/agent` 和收件箱都靠搜索，新消息多久能搜到没测过，L4 / L5 先量。
- **限流**：轮询加上助手的调用，可能碰到开放平台的频率限制；退避 + 合并请求。
- **输出格式随 lark-cli 升级变化**：适配层集中、契约测试、固定最低版本。
- **消息内容格式不同**：开放平台的 `content` 是按 `msg_type` 的 JSON（text / post / interactive …），要转成知识库现在的文本格式。
- **两种来源并存时的重复**：主来源切换的那段时间，同一条消息可能两边都收到；按内容去重有误判的可能（同一人同一秒发两条一样的），L3 / L4 用真实数据验证。
- **token 一周过期**：hub 每天至少调一次保持刷新；真的过期了在设置页和收件箱里提示重新登录，不静默失败。
