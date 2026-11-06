# 模型供应商与模型配置

## 三个入口各管什么

- **设置 › 模型供应商**：接入方式、API 协议、网关、加密凭据、默认模型，以及各台电脑已有的启动器。原来的「模型」页在这里，已有同步、隐藏、详情和额度能力保留。
- **设置 › 模型配置**：只配置四类默认模型：主对话、Agents、信息处理、信息筛选（2026-11-05 起多一类「提问代答」，见下表）。每类选择供应商、具体模型和支持的思考强度，所属用途统一跟随，不提供用途或任务级覆盖。助手用哪个启动器 / 模型只在这里的「主对话」里选。
- **设置 › 助手**：Persona 本身：主动做事（定时任务、运行记录）、了解你（关于我、记忆）、Skills、运行在哪台机器和工作目录。不显示、也不配置模型（#1291）：设置首页的「助手」行不带启动器名，页里没有「对话模型」入口；iOS、Mac、网页、安卓一致。单段对话在自己的菜单里临时换启动器不算配置，照旧。

首页四行可编辑；下方「适用范围」默认折叠，展开后只读说明各类对应的情况，没有用途编辑、搜索或重复的供应商管理入口。目录和说明由管理者提供（hub 转发，见下「谁管模型」）；模型名为主，供应商详情在编辑页，「未验证」不重复铺在首页，真实异常和限流仍显示。

| 分类 | 保留的配置 ID | 适用范围 |
| --- | --- | --- |
| 主对话 | `assistant` | 与当前 persona 的直接对话、定时任务 |
| Agents | `agent.new` | 新建 Agent、AI 分身执行、飞书 `/agent` |
| 信息处理 | `kb` | 翻译、草拟回复、知识整理与会话压缩、项目整理与事项提取、对话标题与 Agent 标签名 |
| 信息筛选 | `inbox.triage` | 收件箱分拣与摘要、重要性判断、判断是否需要回复或交给 Agent |
| 提问代答 | `agent.delegate` | Agent 的提问先判断要不要替用户答（#3029，`docs/assistant.md`「提问先由助手代答」）。2026-11-05 加的第五类：判断要比分拣准，不能跟着信息筛选的便宜模型走。没选过时用信息筛选的接入、模型换成 Opus 5.5（`claude-opus-5-5`；那个接入不是 Claude 的模型就照用它自己的），在这里换供应商或模型。「继承来源」写实际的接入名和模型（`inheritedLabel`，2026-11-06 前一律写「模型用 Opus 5.5」，接入不是 Claude 的、或自己换了模型时都不对）；自己选了接入就不显示继承来源 |

「分拣与摘要」目前是一次调用，整体归信息筛选，不为分类拆成两次调用。persona 不新增独立模型覆盖。语音插件的上游音频模型仍在插件自己的范围内，不能混入启动器或文本 API 四类配置；语音交给主对话的文本任务使用主对话模型。这里不是接管 Claude Code 外部 Skill 或用户已有的外部会话。

## 供应商与执行方式

| 执行方式 | 用在哪里 | 约束 |
| --- | --- | --- |
| 直接 API | 分拣、判断、翻译、草拟、知识处理、标题 | Messages 或 Responses；管理者读取 / 解封凭据、自己发请求 |
| 启动器 | Claude Code / Codex 对话和 Agent | 必须在执行机器上存在，具体模型由 CLI/app-server 真正接收 |
| 插件模型 | 已接入的语音服务 | 只能选择该插件提供的模型，不修改插件自己的供应商或凭据 |

原 `/launchers` API 保留。新增接入的 `kind` 是 `launcher` 或 `api`；旧条目省略时还是启动器。Messages 协议的 API 接入（`api` 为空或 `messages`）也能选给主对话和 Agents：node 用它的地址和 Key 起 claude（#2455，环境变量见 `docs/claude-integration.md`）；Responses 协议的只能直接调用，`finishModel` 拒绝把它当启动器。旧 `hub.json` 的 API 连接显示为兼容来源，认证文件路径和值不返回客户端。

## 编辑与继承

每类同时显示「当前生效」和「模型设置」：

- 供应商留空：继承；具体模型留空：使用该来源的默认模型。
- 更换供应商时清除旧的模型和思考强度，避免把不同供应商的字段拼在一起。
- 「恢复默认」只清除该分类的选择，按「保存」后生效，所属用途一同恢复。
- 供应商无法发现模型目录时可填合法模型 ID；保存不是付费可用性测试。
- 离线、协议不匹配、供应商缺失等状态明确显示，显式配置失效不会偷偷换一家供应商。
- 两台设备同时修改同一项时用 revision 检查；冲突保留草稿，需重新加载再保存。

API 类用途（分拣、判断、草拟、翻译、知识处理、标题）没有覆盖、旧 `hub.json` 的 `ai` 也没配时，自动用 hub 上能直接调用的接入：先找第一个 `kind: api` 的，再找第一个带 Key、Messages 协议的 Persona 管理启动器（隐藏的、缺地址 / 默认模型 / Key 的不算），模型用它的默认模型。「继承来源」显示「自动选择」，不写入覆盖，之后在这里选的照样优先。一个都没有时状态和报错都说清楚去哪配（#388，2026-11-02：只配了「Claude Code 默认」时草拟回复直接报 no model configured）。

「Claude Code 默认」这类启动器只是各台电脑上的 CLI，没有 API 地址和 Key，不能直接调用，所以不会出现在 API 类用途的选项里。

## 旧配置兼容与统一生效（#697）

不复制配置、不批量改库：复用 `model_config.assistant`、`model_config.agent.new`、`model_config.kb`、`model_config.inbox.triage` 四个原有记录作为分类设置，保留 revision 和原子并发检查。没有显式选择时，主对话使用助手原有默认；Agents 使用原有新 Agent 默认（Claude Code）；信息处理使用旧 `kb.ai` / 兼容的分拣 API 基线；信息筛选使用旧分拣 API 基线。API 基线没有接入时继续自动选择 hub 可调用的接入。

其他用途的 `model_config.*` 记录，以及旧 `inbox.agent` / `lark.agent` 专属启动器、判断专属 API、定时任务的 `Launcher/Model/Effort` 不再构成隐藏覆盖。原记录保留供追溯，但解析只读所属分类，所以升级后这些用途会统一跟随对应分类，不能声称每个旧用途都原样保留。

任务的新建、普通修改不接受独立模型设置，返回任务视图不展示旧模型字段；定时任务（用途 `schedule`、`task.<任务 id>`）归「主对话」分类（#1544，用户 2026-11-04 拍板；#697 时归 Agents）：它跑在助手的工作目录里，用的是助手的说明、记忆和工具，本来就是助手的对话，不是新建的 Agent；归 Agents 时没配置就是默认的 Claude Code，在没登录它的 hub 机器上「早报」每次都报 `Not logged in`。任务新建运行、续自己的对话两条路径解析主对话分类，不能让任务字段后置覆盖；用户在任务自己的对话里发消息、跟进发回这段对话时，也和主对话一样每条都按「主对话」，不沿用它上一轮在跑的启动器。发回来源对话（助手用 `schedule_followup` 给自己定的跟进）是那段助手对话的一轮，和手机上发的消息走同一套规则：主对话和定时任务自己的对话按「主对话」分类，助手的其他对话沿用自己在跑的启动器和模型（#1518：原来按 Agents 分类，默认的 Claude Code 在 hub 机器上没登录，一条跟进就把主对话切了过去，之后每条消息都报 `Not logged in`）。任务权限、运行历史、会话关联保持不变。旧用途 GET 返回只读信息；旧用途 PATCH 拒绝。Apple 的旧用途编辑链接会打开所属分类编辑页，并明确说明分类内统一生效。四类目录尚未支持的旧 hub 会提示更新，而不是把不同生效模型伪装成四类默认。

## 谁管模型（2026-11-05）

模型配置是**管理者**的（助手的进程 `personad assistant`，`docs/assistant.md`「和 hub 的边界」），hub 不用模型、不存模型配置：

- 管理者有：四类的记录 `model_config.*`、Persona 里加的供应商和它们封好的 Key（settings `launchers.managed`）、隐藏的 ~/.zshrc 启动器（`launchers.hidden`）、
  各台电脑上次报的 ~/.zshrc 启动器（`launchers.zshrc`，离线的电脑照旧显示上次的）、预设、限流暂停，代码在 `server/internal/assistant/models*.go`。键和表都没变，不迁数据。
- 路由：`/api/v1/model-config…`、`/api/v1/launchers` 的增删改查和 `…/check`、`…/restore`、`/api/v1/launchers/presets`、`…/quota`、`…/test`、`…/discover`、`…/import`、新建 Agent 的选择器
  `GET /api/v1/machines/{mid}/launchers…` 是管理者的（`assistantwire.Routes`），hub 原样转发，路径、字段、错误不变，App 不用改。
- 每个用模型的地方都直接调管理者自己的配置：分拣、判断、草拟、翻译、知识库、标题、Agent 的标签名和分诊、新建 Agent（`applyModel`）、飞书卡片上开的 Agent。
- Persona 里加的启动器由管理者发给各台 node（改动后、它连上时、某台 node 的连接是新的时，`launchers.managed`），Key 经 hub 到 node 的连接转过去，hub 不存也不解。
- hub 只把它知道的事实交过去：hub.json 的 `ai`、`kb.ai`、`inbox.triage_ai` / `judge_ai`、`inbox.agent.machine`、各台电脑的 `machines[].launchers`
  随握手给（`manager.hello` 的 `models`，变了发 `manager.updated`），继承规则照旧；插件的供应商预设、语音插件的模型、哪些开着的插件要「信息筛选」
  由管理者经 `app.call plugins.presets` / `plugins.voice` / `plugins.quota` / `plugins.need_triage` 问（插件系统挪进管理者后这三个去掉）。
  `api_key_file` 是路径，由管理者在它那台机器上读：默认的子进程和 hub 同一台、同一个用户，照旧能读。
- hub 的插件要问的两件事交给管理者回答：「信息筛选」现在调不调得了（`models.usable`，要它的插件能不能启用），和额度插件、插件的启动器面板要带 Key 发的请求
  （`models.launcher_http`：Persona 里加的由管理者用 Key 发，~/.zshrc 的交给那台电脑的 node 发，Key 不离开那台电脑）。额度的路由 `…/launchers/{name}/quota` 和它一分钟的缓存也是管理者的：它定下网关和机器，请 hub 的额度插件读（`app.call plugins.quota`），插件要发的请求再经 `models.launcher_http` 回到管理者。

**Key 的加密**：`server/internal/secretkey`。管理者用 hub 数据目录里的 `secret.key`（`~/.persona/secret.key`，和 hub.json 同一个目录；hub 以前封 Key 用的就是它，所以已有的 Key 照样打得开）
封、解供应商的 Key，没有这个文件时建一个（两个进程同时建也只有一个写成）。hub 自己的密钥（飞书机器人、连接器、插件、互联的凭据）照旧用同一个文件、同一个包，但不碰供应商的 Key。
管理者跑在别处（`assistant.external`）时要把 `secret.key` 拷到它那台的数据目录（0600），否则旧 Key 解不开，状态写「供应商密钥解密失败」，在 设置 › 模型供应商 里重填即可。

**管理者不在时**（重启中、`assistant.external` 那台断了）：上面的路由回 `503 assistant_offline`，回来后照常；配置在 Postgres 里，hub 和管理者重启都不丢。
额度回 `503 assistant_offline`；要「信息筛选」的插件不能启用（错误同上），hub.json 的这类插件先不收件。用模型的功能本来就在管理者里，跟它一起停。

## 谁来解析 Agents 分类（2026-11-04）

新建 Agent 跑在哪个启动器和模型上是 Agents 这件事的一部分，归**管理者**（助手进程，`docs/agent-work.md`「谁管什么」）：`POST /api/v1/sessions` 之前 hub 问它
`agents.start_params`，给一个不知道启动器的停着的会话发消息之前问 `agents.resume_params`；助手自己派的 Agent（`agents_start`、任务）本来就在它那里。
它用自己的模型配置算（`applyModel agent.new`：分类的选择、这一次选的启动器 / 模型 / 思考强度盖在上面、执行指纹），App 看到的生效结果、保存和继承都不变。
标签名（用途 `agent.title`）、会话分诊（`agent.triage`）也是它自己调的模型。

管理者没连着时 hub 只在自己刚起来、它还没连上过的时候等它（最多到起来后 60 秒）；它连上过、现在不在就不等：电脑是管理者连的（`docs/machines.md`「谁连电脑」），
没有它新建和续上都开不了，回 `503 assistant_offline`（以前 hub 自己套「模型配置 › Agents」照样开）。AI 分身执行经 `assistant.start` 开在助手那边，本来就是它解析；飞书机器人卡片上开的 Agent 也在助手那边（渠道是它的）。

## 主对话的运行方式（#2455）

「主对话」多一项「运行方式」：`cli`（默认，`claude -p` 流式 JSON）或 `agentsdk`（经 Agent SDK 的桥，`docs/claude-integration.md`）。只有 `assistant` 有这一项，定时任务跟着它（继承主对话）；Agents 和 API 用途不收，PATCH 带了回 400。

- 存在 `model_config.assistant` 的 binding 里（`runtime`），和供应商、模型无关：换供应商、在对话菜单里临时选启动器或模型、预热另一个启动器，都还是这个运行方式。
- `finishModel` 把它填进给 node 的 params（`runtime: "agentsdk"`；CLI 不填），并算进 `model_fingerprint`：改了 node 重启进程、`--resume` 接回原会话。显式的 `cli` 和不填算同一个指纹，不会白白重启。
- node 跑不了 SDK（没有 Node 22+、桥或 claude）时退回 CLI，会话状态里带 `runtime_fallback`。

## 从 ~/.zshrc 导入供应商（#2455）

`GET /api/v1/launchers/import?machine=` 预览、`POST` 确认（接口见 `docs/protocol.md`「模型供应商与用途配置」）。管理者调那台机器 node 的 `launchers.zshrc`（`assistant/models_import.go`），拿到调 claude 的 alias 和它们的环境变量，用纯函数 `mergeZshrcAliases`（同一个文件）合并：

- 一组 `ANTHROPIC_BASE_URL + key`（`ANTHROPIC_AUTH_TOKEN` 优先，按 bearer；否则 `ANTHROPIC_API_KEY`）是一个供应商，名字和其余设置取第一个 alias；每个 alias 的 `ANTHROPIC_MODEL` 合进 `models`。没有网关地址的（普通 Claude Code）跳过。
- 模型名带 `[1m]` 的去掉后缀，上下文窗口记成 1000000（node 起的时候按窗口再加回去）。
- 预览不带 key 和请求头，只有 `has_key` / `has_headers`；确认时管理者重新读一次，key 和请求头只在管理者里转手，和手动添加的一样加密保存。
- 已经有同样 `base_url + key` 的供应商（`existing`）只补模型，别的设置以 Persona 里的为准。

## 启动器选择器的默认值

`GET /api/v1/machines/{mid}/launchers` 带 `default: {provider, model, effort, runtime?}`，是对应用途在「模型配置」里的生效值（`?purpose=`，默认 `agent.new`；主对话的选择器传 `assistant`，多一个 `runtime`）。客户端拿它显示「默认：…」，不必再单独查模型配置。

## 新建 Agent 选模型（#794）

新建 Agent 选好启动器之后，可以在「模型」里从列表选这一次用哪个模型；列表就是这台电脑上这个启动器跑 Claude Code / Codex 时，在终端里输入 `/model` 看到的那份。

- **列表从 CLI 要，不读配置文件**：settings.json、config.toml、启动器的环境变量里只有默认模型和网关地址，列表是 CLI 把自己的别名和网关 `/models` 的回答拼出来的，自己解析配置会漏掉 ~/.zshrc 启动器设的变量和别名映射。node 按启动器原样起一次 CLI（`node/launcher_models.go`）：Claude Code 用无界面的流式 JSON 模式发 `initialize` 控制请求，回包里的 `models` 就是 `/model` 菜单（default / opus / sonnet / haiku 按这个启动器的设置解析成实际模型，加上网关的全部模型）；Codex 用 `codex app-server` 的 `model/list`（翻页，`hidden` 的不列）。拿到就杀掉整个进程组，不发任何消息、不调模型；Claude 这次带 `disableAllHooks`（和启动器自己的 `--settings` 合并），Persona 和用户的 hook 都不会以为有会话开始。
- **缓存**：node 按「启动器 + 托管启动器的设置」存 10 分钟，同时来的几个请求只起一次 CLI；`?refresh=1`（App 里下拉刷新、出错重试）重新问。实测 Claude Code 约 4 秒、Codex 不到 1 秒。
- **选了能不能生效**：同时用 `launcherModelCheck`（`model_probe.go`）看启动器会不会把 `--model` 原样转给 CLI；自己写死 `--model` 的启动器回 `locked`，App 把模型行画成锁住并说明原因，不发模型（发了 node 也会拒绝启动）。
- **只是这一次**：选中的模型随 `POST /api/v1/sessions` 的 `model` 走，盖在「模型配置 › Agents」上（`applyModel`），node 启动时给 CLI 加 `--model`（Claude Code、Codex 都是）。不记住、不改全局默认；换机器或启动器就回到「默认模型」。「默认」就是不发模型：跑启动器自己的默认，启动器正是「模型配置」里那个时跑配置的模型（App 第二行写「模型配置：…」）。
- **思考强度跟着模型**：Claude 的每个模型带它支持的档位（只留 node 认的 low…max），App 的思考强度只列这些，当前的不在里面就回到「跟随启动器」；没有档位的模型不能选思考强度（这一项变灰或写明不支持）。没选模型时按会跑的那个：「模型配置」的模型（列表里按 id 找不到就当作不知道，各档都列），否则默认模型。Codex 新建不带思考强度（上面「生效边界」）。
- **没有列表的**：TRAEX、runtime 的启动器（DSH…）回 `models: []`，App 不显示模型行；旧 hub 回 404，同样不显示；hub 新、这台电脑的 node 旧时 hub 也回 404（`code: node_outdated`），不给一个永远失败的重试。列不出来（CLI 不在、没登录、超时）时模型行显示原因和重试，「默认模型」照样能开始。

## 模型配置选模型（#943）

「模型配置」里选了 Claude Code / Codex 启动器类的供应商时，「具体模型」也从这个启动器的 `/model` 列表里选，和新建 Agent 是同一份列表、同一个选择面板（网页 `ui/ModelPicker.tsx`、安卓设计系统的 `JModelSheet`）。

- **问哪台电脑**：模型配置是管理者的，不属于某台电脑，所以 `GET /api/v1/model-config/providers/{id}/models` 在有这个启动器的电脑里挑一台在线的去问 node 的 `launchers.models`：`?machine=` 指定的、助手所在的、hub 本机、其余的，按这个顺序（`assistant/models_launchers.go` 的 `pickMachine`）。~/.zshrc 里的启动器只在定义它的电脑上有；Persona 托管的（`jarvisd launch`）和「Claude Code 默认」每台都能跑。回答带 `machine`，页面写明列表来自哪台。
- **「默认」是留空**：留空在这里是继承。没选供应商、也没单独设模型时，「默认」那行写继承来的模型（「当前生效」里那个）；选了供应商时是这个供应商的默认模型（列表的默认项）。
- **列表外的 ID 照样能用**：列表不一定全（网关新上的模型、别的电脑上不同的设置），搜索框里输入的 ID 没有完全对上的条目时，多一行「使用「…」」；已经配着的列表外的模型照样显示，标「列表里没有」。保存仍不是付费可用性测试。
- **没有列表时**：插件的列表跟着插件走（原来就有），API 类的没有列表；旧 hub、node 太旧（404）或列不出来时退回原来的文本框，列不出来的原因和「重试」写在下面。
- 思考强度：网页的候选只列所选模型支持的档位；安卓仍是自由输入。

## 生效边界

- API 用途在下一次调用开始时解析配置；知识库使用按用途的可注入模型，不再固化 hub 启动时的旧对象。
- 对话和 Agent 的具体主模型走 CLI 参数或 Codex app-server model 字段，不只写可能被启动器清掉的环境变量。
- 预热/进程复用带执行指纹，同名启动器换了模型或凭据也不使用过期实例。
- 保存不杀当前轮次或后台任务。需要换进程而当前仍忙时拒绝切换，待安全边界再发起；不宣称 Claude/Codex 跨执行器无损续聊。
- 不重跑历史分拣、不重写已有知识。翻译缓存区分模型配置，下一次翻译不会误用旧配置的缓存。
- 新 Agent 默认「跟随模型配置」，手选启动器是本次覆盖，不再把客户端上次选的启动器自动盖在全局配置上。Claude Code 和 Codex 都保留各自的新建路径；Codex 新建需要首条消息，具体模型用明确的 `--model` 覆盖，思考强度仍由它自己的启动器管理，不把 Claude 的权限/思考参数混进去。
- 语音模型只在下一次语音会话开始/重新开始时读取。旧 hub 404 时可兼容读取旧客户端默认；新 hub 读取失败不能当作没有配置而回退。
- 模型变化不改变权限模式，也不授权自动发送消息。

## 上游限流

直连 API 的用途（知识处理、收件箱分拣 / 判断 / 草拟、翻译、标题）都在管理者里调，共用 `server/internal/ai` 的客户端，按连接（地址 + Key，不管哪个用途在用）各有一道闸（`ai/limit.go`）：

| 上游的回答 | 认作 | 怎么停 |
| --- | --- | --- |
| HTTP 429，正文提到 quota / usage limit / exhausted 之类（中转的 `account_quota_exhausted`） | 额度用完 | 暂停 30 分钟；有 `Retry-After` 按它 |
| HTTP 429 / 503 提到 concurrency（`account_concurrency_full`），或别的 429、503、529 | 并发满 | 指数退避（5 秒起，每连一次翻倍，最多 5 分钟），有 `Retry-After` 取较长的；同时给这条连接加并发上限 |

- **暂停期间不发请求**：调用直接拿到 `ai.PausedError`（`ai.IsPaused`），文案就是 App 里显示的「额度用完，23 分钟后重试」。调用方把它当「延后」：知识处理的循环直接停（会话保持待压缩、项目保持待重写、条目保持未打标），收件箱分拣不把条目当分过（新条目先放在按规则的分组里，恢复后重新分），判断、翻译留到下一轮；都不打日志。
- **只有闸打日志**：进入时一行（地址、上游的 code 和 message、暂停到几点），退出时一行（停了多久、延后了多少个请求）。
- **暂停结束先放一个**：第一个请求去探路，别的继续延后；探路成功才放开，又被拒就再停一轮（不再打日志）。暂停前已经发出去的请求回来的同样答复不算数，不会把退避叠上去。
- **并发上限**：第一次并发满时取当时在途请求数的一半（至少 1），再满再减半；之后每成功 10 次加 1，超过 8 就不再限。超过上限的请求排队等空位，等到自己的超时也算延后。
- **App 里看得到**：模型配置接口里，连接被暂停的用途带 `limit`（`reason`: `quota` / `busy`，`until`），`status` 就是上面那句文案（不认 `limit` 的 App、网页、安卓照样显示这句）。
  App 在「设置 › 模型配置」的用途行和详情里把它画成警示（`Theme.caution` 的符号 + `cautionText` 的字），页面开着时按 `until` 倒数；截图用 mock 的 `MOCK_MODEL_LIMIT=1`。

## 接口

`GET /api/v1/model-config` 的 `purposes` 只返回按顺序排列的分类配置（四类加「提问代答」）及 `providers`；`GET /api/v1/model-config/purposes/{id}` 返回单项 `purpose` 与可用 `providers`，兼容查询旧用途。

`Purpose` 包含 `id/name/group/kind`、`category_id`、`override`、`effective`、`revision`、`inherited_from/status`、连接被暂停时的 `limit`（见「上游限流」），以及限定表单的 `fields/provider_ids`。四类的 `category_id` 等于自身 ID，另有 `uses: [{"name":"只读用途说明"}]`。其他旧用途和 `task.<任务 id>` 的 `category_id` 指向所属分类，`override` 为空，`fields` 为 `[]`，版本与生效模型来自分类；真实任务名称不再出现在配置首页。`plugin.<插件 id>` 的模型协议仍独立保留，不加入四类目录。响应不包含秘密。

单项保存：

```json
{
  "revision": 0,
  "provider": "launcher.<供应商名称>",
  "model": "<具体模型 ID>",
  "effort": null,
  "runtime": "agentsdk"
}
```

`runtime` 只有 `assistant` 能写（见「主对话的运行方式」）。

「信息筛选」（`inbox.triage`）还管着插件：用收件箱又没有自带兜底的插件要它能调才能启用；有这样的插件开着时，把它改成调不了的返回 422 `triage_model_in_use`，
列出插件名（去掉选择后退回的 hub.json 配置或自动选择仍然调得了的，照常保存；`docs/extensions.md`「收件箱的通用规则」）。

发送至 `PATCH /api/v1/model-config/purposes/{id}`，仅四类配置和独立插件可以写；其他旧用途或任务 ID 返回 400（用途模型只读，请修改所属分类）。缺失字段不变，`null` 清除选择，非空值覆盖；成功返回与 GET 单项相同的结构。仅支持该分类列出的字段；revision 不匹配返回 409，未知用途不会被临时创建。

## 验证

- 服务端使用真实 Postgres 验证存取、分类并发更新、四类目录、所有用途统一解析、旧覆盖不参与且不删除、任务三条执行路径（发回来源对话不换那段对话的启动器）、权限不变、缓存与执行器参数。
- Apple 用 `FakeHub` 测分类与只读范围解码、旧用途跳转所属分类、旧 hub 更新提示、PATCH 的 null 语义及草稿/冲突；UI 用 `ModelConfigurationTests` 验证只显示四行、没有搜索、范围默认折叠且只读、真实分类保存。DEBUG 使用 `-openModelConfiguration YES [-openModelPurpose <分类或旧用途 id>]`。
- 网页和 Android 有相同协议的纯逻辑测试，目录由管理者收敛为四类。
- `python3 -I scripts/mock/test_model_config_mock.py` 启独立 mock，覆盖四类目录、只读用途继承和写入拒绝、保存、冲突、恢复默认与未知用途；不会调用真实模型。
