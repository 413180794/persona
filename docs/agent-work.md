# Agent 的工作态：工作中 / 卡在具名的前置条件上 / 做完了（#1686）

用户原话（2026-11-04）：「现在 persona 不够主动，我看到有好多未读的 session……每个 session 要么是在工作，要么是明确在等什么前置需要，
要么就是做完了……尽量压缩等待的时间」。

## 为什么

`docs/agent-states.md` 的统一状态是**宿主视角**：需要你 / 工作中 / 完成未看 / 已看。「完成未看」只说明这一轮结束了、用户没看，
不说明这个会话**在工作上**到了哪一步。于是 Agents 页堆一排绿点，要一个个点进去读才知道：

- 哪个已经做完、可以收尾；
- 哪个在等用户拍板（「要不要 push 到 beta」「D2 / D4 / D10 选哪个」），但列表里只是一个绿点；
- 哪个在等外部事件（等部署跑完再测、等 CI、等 OAuth 链接），却没有任何东西在事件发生时叫醒它；
- 哪个只是 `API Error: 503` 这类瞬时错误停住了，其实发一句「继续」就行；
- 要用户点的授权链接几分钟就过期，看到时早失效了。

## 模型

每个会话在工作意义上只处于三态之一（`proto.Session.work.phase`）：

| phase | 是什么 | 列表的一行 |
| --- | --- | --- |
| `working` 工作中 | 宿主在跑（`state = working`） | 沿用 `activity` / `last_text` |
| `blocked` 卡住 | 停下了，在等一个**具名**的前置条件（`on`） | 「在等什么」 |
| `done` 做完了 | 这件事做完并交付了 | 一句话结论 + 产物 |

`blocked` 的前置条件分四类（`work.on`）：

| on | 例子 | 谁来推进 |
| --- | --- | --- |
| `user` 等你决定 | 选方案、要不要推到 beta、补信息、点授权链接、登录过期 | 用户：Persona 把问题和选项做成「需要你」卡片，一键选完把答案发回给 Agent |
| `agent` 等另一个 Agent | 「等隔壁那个合完我再 rebase」 | Persona：那个 Agent 停下时叫醒它 |
| `external` 等外部事件 | 等部署 / CI / 审核 / 某个时间点；后台命令、`/loop` 自己会醒 | Persona：到点叫醒它自己去查（后台命令、`/loop` 不用叫） |
| `transient` 瞬时错误 | `API Error: 503 / 529 overloaded / Connection error` | Persona：退避后自动发「继续」，有上限 |

和统一状态的关系：`state` 的取值**不变**（老 App 照常显示），只是算法多看一眼工作态：

| 工作态（这一轮的） | `state` | 说明 |
| --- | --- | --- |
| `done` | `idle` | 不再占「完成未看」；结论在 `work.line`，推送也带结论 |
| `blocked` · `user`，有选项 | `needs_you` | Persona 建了一张提问卡片（`pending_interactions` 加一） |
| `blocked` · `agent` / `external` / `transient`，Persona 在等（`wake_at` 或等的 Agent） | `idle` | 新 App 显示成「待唤醒」+ 在等什么；老 App 是已看 |
| 没分诊、分诊失败、看不出来（`unsure`） | 照旧（`unread`） | 宁可留着绿点，也不把没看清的事藏起来 |

「完成未看」于是只剩分诊还没出结果（一轮结束后十几秒）和模型看不出来的。

## 谁管什么（2026-11-04 起：在管理者里）

分诊这件事是**管理者**的（现在只有一个，就是助手进程 `personad assistant`，`server/internal/assistant/agents_*.go`），hub 只是通道
（`hub/agents_link.go`，总线上的方法见 `docs/protocol.md`「总线」的「Agents」）：

| 谁 | 做什么 |
| --- | --- |
| 管理者 | 一轮停下后的分诊（规则 + 模型 `agent.triage`，经 hub 的 `app.call models.json`）、自动继续、叫醒、卡片和卡片的答案、四个开关（settings `agents.triage`）、`session_work` / `session_work_events` 两张表、统计；新 Agent 的标签名（`agent.title`）、新建和续上的 Agent 跑在哪个启动器和模型上（下面「新建」） |
| hub | 告诉管理者发生了什么：一轮开始 / 结束（`session.turn_started` / `session.turn_ended`，原来就发）、用户在 App 里看了一个会话（`session.seen`）；照管理者的快照显示：会话列表的 `work`（快照的 `work`）、它的卡片（快照的 `interactions`，和助手的提问一样显示、推送、到期，答案经 `manager.action` 回去）；它要发给 Agent 的经 `device.call session.send` 转给 node |

- **会话列表的 `work`**：管理者在快照里报每个会话最新一轮的工作态（`work.turn_at` 是那一轮结束的时间），hub 只在会话还停在那一轮上时显示它；
  工作中（`{"phase":"working"}`）、Agent 自己在问（它的卡片排在管理者的卡片前面）、终端里有对话框这三种是 hub 照会话当时的样子说的，不等管理者。总开关关着时没有 `work`。
- **新建**：`POST /api/v1/sessions` 之前 hub 问管理者 `agents.start_params`：它按「模型配置 › Agents」填启动器、模型、思考强度和指纹（这一次选的启动器、模型盖在上面，`docs/model-config.md`），
  再从第一条消息起标签名；后台开始的（`async`）和飞书卡片开的 Agent 之后经 `agents.tab_name` 要名字。给一个不知道启动器的停着的会话发消息（续上）之前问 `agents.resume_params`。
  `POST /api/v1/tabs/autoname` 也是管理者的。

**管理者没连着的时候**（hub 刚起来、它还没握手；它重启的那一两秒；崩了在被拉起来）：

- 新建 Agent、续上不知道启动器的会话之前先等它一会儿：hub 起来后它还没连上过的，等到 hub 起来 60 秒；连上过、刚断开的，等 10 秒（都不超过这次请求自己的期限）。
  hub 一重启就有排在端口上的请求和 App 的重放进来，这样它们照样由管理者填。
- 等不到就由 hub 自己套「模型配置 › Agents」（模型配置现在还在 hub，`applyModel`）：启动器、模型、思考强度和指纹跟有管理者时一样，这一次选的照样盖在上面；
  只是标签名不起，是 node 的 `agent-<n>`。续上的会话也一样套它（套不了就是 `claude`）。
- 分诊暂停：轮次结束的事件不发给它（hub 不排队），到点的继续 / 叫醒等它回来再发（发之前照样重新检查）。它（重新）连上以后补上：下一次检查（15 秒内）开始把这期间结束、
  还是完成未看、24 小时内的轮次分诊掉（每次最多 3 个，之后每分钟接着补；hub 刚起来时也一样：它的总线最后才开，node 先连上）；这期间用户已经看过（App 里、或在电脑上 herdr 里）的不补，和以前 hub 重启后一样。
  这期间又开始工作的会话，它的卡片在下一次检查（15 秒一次）撤掉。
- 会话列表留着它最后报的 `work`（照样只对还停在那一轮上的会话显示）；里面的叫醒时间过了 5 分钟还没人叫，就不再算「待唤醒」（列表里去掉 `wake_at`，又是完成未看）。
- 它的卡片照样挂着、能答：答案 hub 先记下，它回来、报了快照以后再交给它；它一时收不下（还没读回自己的状态之类）的也留着，过 10 秒再给，直到它收下、
  说那张卡片已经没了，或者卡片本该过期。hub 也重启了的话这条答案丢了，卡片会再挂出来。
- 开关和统计的接口（`/api/v1/agents/triage`、`/api/v1/agents/work/stats`、`/api/v1/tabs/autoname`）回 `503 assistant_offline`。
- 只有管理者重启：hub 还挂着的卡片照旧挂着，不再推一次，和分诊在 hub 里时一样只在答了、Agent 又开始工作、过期时撤。
- hub 重启（管理者是它的子进程，跟着重启）：管理者报第一份快照之前的一两秒，列表里没有 `work`（分诊过的那一两秒又显示成完成未看）；卡片在那台电脑重新连上后重新挂上（同一个 id，会再推一次，和以前 hub 重启一样）。

卡片答了以后，发给 Agent 没发出去（那台电脑刚断开）：卡片换一个 id 重新挂上、再推一次，用户可以再答（以前是 App 那次回答报错）。
管理者在快照里还报着一张答案已经交给它的卡片（它没收下），过 30 秒 hub 再把它挂出来。

## 分诊

### 什么时候

- 一轮结束（`setSessions` 里 working → done / idle，和推送「完成」同一处），等 `triage_settle_seconds`（默认 15 秒）没再动，开始分诊。
  这期间又开始工作（Stop hook 让它接着干、马上又发了一条），这一轮不分诊。
- 管理者每次连上后、之后每分钟补一次：停着、24 小时内结束、还是完成未看、这一轮还没分诊过的会话（部署后、管理者断开期间结束的轮次也就分掉了，见下「谁管什么」）。
- 不分诊：助手的对话（`assistant`，含 AI 分身、飞书 /agent）、没在宿主里的（`offline`）、免打扰的会话照样分诊但不建卡片。

### 先规则，后模型

按顺序，命中就不调模型：

1. 有待处理的交互 / 宿主 `blocked`：已经是需要你，`blocked · user`，`line` 用交互的问题。
2. 最后一句是模型 API 错误（`turn_error`）：
   - 瞬时的（5xx、`overloaded`、`Connection error`、`timed out`、`socket hang up`、`ECONNRESET`、`fetch failed`、`no eligible upstream`、429）→ `transient`，安排自动「继续」；
   - 其余（`Failed to authenticate`、`OAuth token has expired`、`Invalid API key`、401 / 403、选的模型不可用）→ `blocked · user`，卡片「模型服务需要你处理：<原文>」，选项「已处理，继续」「先不管」。
3. 后台命令 / 子 Agent 还在跑（`background`、`subagents` 有 running）→ `external`，「后台 N 个任务在跑」，不登记叫醒（它自己会被结果叫醒）。
4. `/loop` 在跑（`loop`）→ `external`，「定时循环，下次 HH:MM」，不登记叫醒。
5. 其余：模型。

### 模型

- 用途 `agent.triage`「会话分诊」（「设置 › 模型配置」，挂在「信息筛选」下面，默认继承收件箱分拣用的便宜模型）。
- 输入：Agent 的名字、目录 / 项目、这一轮用户的要求（`lastTurn` 的 ask）、最后的回复（首尾各 1500 字）、同一台电脑上在工作的其他 Agent（名字 + 短 id）、现在的时间。
- 输出 JSON（`server/internal/prompts/agents/triage.md`）：

  ```json
  {"phase":"done|blocked|unsure", "on":"user|agent|external",
   "line":"不超过 30 字：结论，或在等什么",
   "questions":[{"question":"…","header":"…","options":[{"label":"…","description":"…"}],"multi_select":false}],
   "link":{"url":"https://…","minutes":10},
   "wait":{"what":"等的事","then":"等到以后要做的事","check_minutes":10,"agent":"<短 id>","risky":false},
   "artifacts":[{"kind":"pr|commit|doc|build|url","ref":"…"}]}
  ```

- 判断的原则写在提示词里：
  - 末尾「要不要我顺便……」这种可选的提议不算卡住：事情做完了就是 `done`，提议放进结论。
  - 下一步是**推代码、部署 / 发布、给别人发消息 / 评论、合并或改别人的 MR / PR**，不管 Agent 说它要不要等什么，一律 `blocked · user`，问用户要不要做（见下面「安全边界」）。
  - 拿不准做没做完就 `unsure`，不选 `done`。

### 结论写到哪

- `session_work` 表（下面「存储」），随会话列表下发成 `work`；
- 做完了：知识库里这个目录对应的项目时间线加一行「完成：<结论>」（`kbAgentDone` 原来每轮记最后一句，现在有结论就记结论）；
- 推送「完成」的正文换成结论；主对话的实时快照多两节「做完的」「在等的」（下面「Persona 知道」）。

## 自动动作

### 瞬时错误：自动「继续」

- 退避：30 秒、2 分钟、8 分钟，最多 3 次（`agents.triage.max_retries`）。一轮正常结束（不是错误）后清零。
- 3 次都失败：变成 `blocked · user` 的卡片「模型服务一直出错，自动重试了 3 次：<原文>」，选项「再试一次」「先不管」。
- 发出去的就是 App 错误卡片上「重试」发的那句「继续」（`continueText`）。

### 等待：到点叫醒

- `external`：按模型给的 `check_minutes`（2 分钟到 4 小时，没给就 10 分钟）登记 `wake_at`；之后每次还在等就翻倍（最长 1 小时），最多叫 6 次（`max_wakes`）。
  到点 Persona 给它发一条叫醒消息（`prompts/agents/wake.md`）：上一轮说要等什么、等到以后做什么，请它自己查；好了就接着做，没好就说还要多久。
  叫醒以后它这一轮结束时照常分诊，还在等就再登记。叫满 6 次还没好，变成卡片「等「X」已经叫了 6 次还没好」，选项「再等等」「不等了」。
- `agent`：模型从给它看的在工作的 Agent 里认出等的是谁（短 id），那个 Agent 一轮结束（或离线）时叫醒它；同时按 `check_minutes` 兜底。

### 等你决定：卡片

- Persona 建一张 `question` 交互（`tool_name: persona_triage`，`summary` 是 `line`；管理者在快照的 `interactions` 里报，hub 照它显示、推送、到期），问题和选项来自模型；App、网页、安卓、手表、飞书卡片、收件箱的「等你的 Agent」、主对话的任务坞都已经会画、会答，不用改客户端。
  推送和别的提问一样（时间敏感）。
- 选完（或「其他回答」打字）以后管理者（答案经 `manager.action` 到它）把答案组织成一条消息（`prompts/agents/answer.md`：「关于你上面的问题：Q → A」）用 `session.send` 发给那个 Agent。
- 带授权链接的（`link`）：卡片的第一个问题就是链接和有效期，选项「已授权，继续」「先不」；分诊一出来就推（一轮停下后 15 秒加模型的一两秒），不等「做完了」推送的 60 秒。
- 卡片在这些时候撤掉（不发任何东西）：Agent 又开始工作（你在电脑上或 App 里直接回了）、开始了新的一轮、会话没了、24 小时没人答。

## 安全边界

Persona **自己会发**给 Agent 的只有三种，都不带新的意图：

1. 瞬时错误后的「继续」：和 App 错误卡片上的「重试」一样，接着做它被打断的事；
2. 登记过的等待到点后的叫醒：只请它查一下等的事好了没有，并且明说「接下来要推代码、部署、给别人发消息、合并或改别人的 MR 时先停下来问用户，不要自己做」；
3. 用户在卡片上选的答案。

凡是**推代码、部署 / 发布、给别人发消息、碰别人的 MR / PR** 的下一步，只能变成「等你决定」：

- 提示词要求模型把这类下一步判成 `blocked · user`（`wait.risky` 为真时也一样）；
- 代码兜底：`wait.then` / `line` 里有 push、deploy、部署、发布、上线、merge、合并、发消息、回复、评论、testflight、release 这些词时，不登记叫醒，改成卡片问用户「等「X」以后它要「Y」，要它去做吗」；
- 用户在卡片上选了「做」，发给 Agent 的就是用户的回答，算用户授权。

## 不和用户手动操作打架

每个自动动作（继续、叫醒、建卡片）**动手前一刻**重新读会话，下面都满足才做：

- 还是分诊时那一轮（`updated_at` 没变）、停着（不在工作）、没有待处理的交互、没有排着的输入（`queued`）；
- 宿主是 `done` 不是 `idle`：herdr 里你聚焦过这个 tab（在电脑前看着它）就不动它，只记下工作态。停下后一直报 `idle` 的宿主也就不会有自动动作（宁可不动）；
- 这台电脑在线。

你自己发了消息、在电脑上打了字，会话就开始工作：这一轮的重试、叫醒、卡片全部作废；新的一轮结束再分诊。
App 里点了错误卡片的「重试」也一样（会话开始工作，排着的自动重试作废）。Persona 发的「继续」和叫醒都记在 `session_work` 里，同一轮只发一次。

## 开关（2026-11-05 用户要求：默认全关）

分诊每停一轮就调一次模型，「继续」和叫醒还会让 Agent 用它自己的模型再跑一轮，都花用户的 token。所以**全部默认关**，
用户在「设置 › 会话分诊」（网页、安卓；iOS 后做）里自己打开，四个开关存在 settings `agents.triage`（只有管理者读写）：

| 开关 | 管什么 | 花谁的 token |
| --- | --- | --- |
| `enabled` 会话分诊 | 总开关：一轮停下后分诊，列表显示在等什么，做完的不再算完成未看 | 分诊模型（「设置 › 模型配置 › 会话分诊」，`agent.triage`，默认跟收件箱分拣一样） |
| `decide` 要你决定的开卡片 | 等你拍板的做成「需要你」卡片 | 不另花 |
| `auto_continue` 出错自动继续 | 瞬时的模型 API 错误后自动发「继续」 | Agent 的模型（接着跑被打断的那一轮） |
| `wake` 到点叫醒 | 等部署 / CI / 另一个 Agent 的，到点叫醒它自己查 | Agent 的模型（每叫一次跑一轮） |

- 子开关只在总开关开着时起作用。关掉某个开关立刻生效：Persona 开的卡片撤掉，排着的重试 / 叫醒不发。
- `GET / PUT /api/v1/agents/triage`（`{enabled, auto_continue, wake, decide}`，hub 转给管理者），改了广播 `{"type":"agents.triage"}`。
- hub.json 的 `agents.triage` 只剩上限（`max_retries` 默认 3、`max_wakes` 默认 6、`settle_seconds` 默认 15）；
  它的四个开关是从没在 App 里保存过时的初始值（不写就是关）。这一节经握手的回答交给管理者（`assistantwire.Hello.Agents`）。

## 存储

进程随时会被重新部署清掉，所以分诊结果、重试次数、叫醒时间、卡片都落库（`0331_session_work.sql`，只有管理者读写）：

- `session_work`：每个会话一行，最新这一轮的工作态（`work` 的 JSON）、这一轮的结束时间 `turn_at`、下次动作 `wake_at`、重试 / 叫醒次数、卡片 id。
  管理者起来时读回来：到点的接着做，没答的卡片重新挂上（还是同一轮、还停着的才挂，同一个 id；那台电脑还没连上的等它连上再挂）。
- `session_work_events`：分诊、自动继续、叫醒、建卡片、答卡片、等待结束，每件一行（30 天），给下面的评估用。

## Persona 知道

主对话的实时快照（`prompts/assistant/snapshot*.md`）：

- 「需要你」每个 Agent 后面带它在等什么；
- 新增「在等的」（Persona 在替它等：重试中、等部署、等另一个 Agent）和「做完的（24 小时内）」：名字 + 结论；
- 「完成未看」只剩没分诊出来的。

从主对话派的任务（`internal/assistant/tasks.go`）：Agent 停在瞬时错误上、Persona 还会自动继续（`work.on = transient` 且有 `wake_at`）时，先不写「中断了」的回执，等重试用完再写。

## 协议

`proto.Session` 多一个字段 `work`（`docs/protocol.md`「数据类型」）：

```json
"work": {"phase":"blocked","on":"external","line":"等部署跑完再跑线上测试",
         "wait":"部署跑完","then":"跑线上测试","wake_at":"…","attempts":1,"max":6,
         "agent":{"machine_id":"…","session_id":"…","label":"…"},
         "link":{"url":"…","expires_at":"…"},
         "artifacts":[{"kind":"pr","ref":"https://github.com/…/pull/1"}],
         "turn_at":"…","at":"…","by":"rule|model"}
```

- 工作中只有 `{"phase":"working"}`；待处理的交互是 `{"phase":"blocked","on":"user","line":<问题>}`；没分诊过的不带 `work`。
- `GET /api/v1/agents/work/stats?hours=24`：评估用的数字（下面）。

## 评估

`GET /api/v1/agents/work/stats`（hub 转给管理者）和管理者的日志（hub 日志里 `assistant:` 开头，`triage <机器>/<会话>: done (model 1.2s): …`）：

- 一轮结束后的分诊结果分布（done / 各种 blocked / unsure / 失败），规则和模型各多少；
- 「完成未看」的数量：管理者每 5 分钟采一次非助手会话里 `state = unread` 的个数（`session_work_events` 的 `sample`），给出这段时间的平均、最大和现在；
- 等待时长：一轮结束到你处理（看过、回了、答了卡片）的中位数 / p90，分「做完了」「等你决定」「没分诊」；
- 自动继续：发了几次、之后正常结束的比例；叫醒：几次、叫醒后做完的比例；
- 卡片：建了几张、答了几张、答之前等了多久、被撤掉（你直接回了）几张。

上线前的基线（2026-11-04 晚，hub 上的非助手会话）：完成未看 5 个（24 小时内 4 个），中位已经等了约 10 分钟。

## 客户端

- 网页（#1837）：列表行的那一行字（`sessionPreview`：总览、机器页、知识库详情，侧边栏的悬停提示）在会话停着且 hub 分诊过时换成工作态：
  做完的「✓ 结论」，在等的「在等什么（N 分钟后叫醒 / 马上重试）」；hub 会替它等的排进「待唤醒」（`hubWaits`，`web/src/lib/work.ts`）。
  「设置 › 通用 › 会话分诊」四个开关，脚注里能跳到「模型配置 › 会话分诊」选模型。截图用 `MOCK_WORK=1` 的假 hub。
- 安卓（#1837）：Agents 列表、知识库的「在做这个项目的 Agent」行用同样的一行（`Session.workLine`，`core:domain` 的 `WorkLine.kt`），hub 会替它等的排进「待唤醒」；
  「设置 › AI › 会话分诊」一页：总开关、分诊模型（跳到模型配置）、三个动作的开关。
- iOS / 手表：之后再做。

## 分步

| 步 | 内容 |
| --- | --- |
| 1 | 本文档；hub（2026-11-04 起挪到管理者，见「谁管什么」）：`session_work` 表、分诊（规则 + 模型）、自动继续、叫醒、卡片、`state` 的算法、`work` 字段、统计接口；快照；任务的中断回执 |
| 2 | 开关改成 App 里的设置、默认全关；网页显示 `work.line`、待唤醒、开关（#1837） |
| 3 | 安卓（#1837）；之后 iOS / 手表 |
