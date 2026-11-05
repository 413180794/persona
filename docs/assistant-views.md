# 助手对话里的富 UI

总 issue #2017。本文是实现规格，**不代表四端已经完成**。schema 与共用校验（#2251）、扩展的网页／安卓／Apple 共用渲染器（#1850、#1851、#2299）、活卡片工具（#2286）和一次 reply／outbox（#2328）已合并。助手各端的动作、活卡片和 HTML 对话接线仍待完成；Apple 已合并的手表／Siri fallback 和 HTML 沙箱基础继续复用。

已确认的产品决定：HTML 卡片默认启用，进入可见区域自动运行；安全执行未通过验收时显示 fallback。2026-11-05 冲刺的发布边界更新为：全部合并部署后使用临时测试对话完成端到端验收，先向用户汇报，只有用户明确确认后才打开线上 `assistant.persona_view`。本轮不发 TestFlight，不使用用户的真实主对话测试，也不能根据构建或验收通过自行开启。

## 卡片复用扩展的 View

助手遇到需要比较、选择、填写或观察进度的情况，自己决定画卡片。卡片与扩展使用同一份 `schema/extension/view.schema.json`、同一套生成类型和组件渲染器。正文中的一次性卡片随 transcript 保存；要继续更新的卡片用工具交给 hub 保存。

结论和关键数字仍写在正文里。每张卡片必须有纯文本 `fallback`，供手表、通知、摘要和不支持该卡片的客户端使用。老 `persona-card` 继续按 `sdk/go/wire/personacard.go` 转成 View，不另写一套画法。

```persona-view
{
  "title": "选哪个方案",
  "fallback": "推荐 B：给现有队列加去重，半天。A：重写同步层，3 天；C：先加监控，1 小时。",
  "sections": [{"items": [
    {"type": "row", "title": "A 重写同步层", "subtitle": "3 天，风险高", "action": {"id": "a", "reply": "选 A：重写同步层"}},
    {"type": "row", "title": "B 队列加去重", "subtitle": "半天，推荐", "badge": {"text": "推荐", "tone": "positive"}, "action": {"id": "b", "reply": "选 B：队列加去重"}},
    {"type": "row", "title": "C 先加监控", "subtitle": "1 小时", "action": {"id": "c", "reply": "选 C：先加监控"}}
  ]}]
}
```

`persona-view` 代码块的内容是 View；单组件简写也支持，但仍要带 `fallback`，解析时统一包装成 View。`fallback` 最多 2000 字，整块最多 64 KB。一张卡片最多 8 个命名详情页，每页最多 300 个组件；一次回答最多 3 张卡片，超出的显示 fallback。

组件沿用扩展现有的 text、markdown、row、metric、stat_grid、chart、table、progress、field、segmented、empty、loading 等。列表搜索、筛选、加载更多沿用 View 的 search、filter 和 Section.more。时间线、diff／code、image、tabs 和 pie 在后续小 PR 加到同一个 Component 联合类型里，扩展与助手共用；这是本任务内的后续工作，不能把现有组件冒充这些组件已经完成。

## 卡片身份

| 来源 | key |
| --- | --- |
| 正文代码块 | `<item id>/<n>`，n 是这条 text item 中第几个 persona-view 块，从 0 开始，坏块也计数 |
| 活卡片 | `view_show` 返回的 id，已经带 `view_` 前缀，不再重复拼接 |

key 是不透明字符串，客户端须 URL 编码整个路径段；不能按 `/` 或 `#` 自行拆解。正文流式阶段先只读绘制，只有 hub 确认完整块和稳定来源后才启用持久化动作。客户端提供的 key、会话和动作都不能直接作为权限依据。

正文注册从可信助手的 text item 读取；收到 transcript 条目和一轮回复结束时均检查，没有客户端订阅也会注册最新一轮。长回复先用 `session.item` 读取全文，不能只查被截断的预览。相同来源重放不修改定义或清除已选状态。首次落库后广播 `assistant_view_block {conversation, key}`，feed 的 `view_blocks` 返回该对话已注册 key 的完整快照，漏掉事件后也能恢复。

fence 规则和 key 的共同向量在 `schema/extension/examples/assistant-text/fences.json`：语言名不区分大小写，支持 LF／CRLF／CR、tilde 和长外层 fence；其他代码块中的示例不注册，坏块也占序号，未关闭的块只显示 fallback。当前识别独立 fence，不把列表或引用里的嵌套 Markdown 当成已经支持；每条 text item 校验最多 3 个 persona-view 块，整轮的显示上限仍须各端和 Stop 校验配合。

## 动作与多页

在现有 Action 上加字段，`id` 沿用原字段。助手动作必须且只能选择一种执行方式：

| 字段 | 行为 |
| --- | --- |
| `reply: "文字"` | 以用户身份发回本对话，助手接着处理 |
| `open: "目标"` | App 内打开 URL、对话／Agent、任务、文件或知识库条目 |
| `push: {page: "详情页", title?}` | 打开根 View 的 `pages.<详情页>`，复用渲染器的页面栈 |
| `pop: true` | 返回卡片上一页 |

`confirm` 继续使用原生二次确认；`submit: true` 只配 reply，提交当前页的字段。助手表单不支持 secret 字段，不能用卡片收集密钥或密码。

命名页仍是 View，但不允许再声明嵌套 pages。所有页共享根页的命名页表，push 最多 4 层。动作 id 在同一张卡片所有页里唯一，组件 key、字段 name 在各自页内唯一。

open 只接受 https 或明确支持的 `persona://session/<机器>/<会话>`、`persona://task/<id>`、`persona://kb/<相对路径>`、`persona://file/<机器>/<路径>`（历史 jarvis scheme 继续兼容）。每类目标单独校验段数、转义和路径，拒绝凭据 URL、控制字符、无效转义和知识库路径穿越。不支持的目标显示说明，不能退到系统外部打开。

扩展的 Action.id／push.ui／push.state 及现有动作结果不变。助手专用的 reply、open、push.page、Action.pop、View.pages、html 不授予扩展新能力；hub 降级扩展 View 时拿掉这些字段和组件，并记录降级。

### 持久化提交

接口：`POST /api/v1/assistant/conversations/{conversation}/views/{key}/actions`，参数为生成类型 `ViewActionInput` `{action, page?, values?, client_msg_id, confirmed?, expected_version?}`。**reply 原文和 submit 定义由服务端从已保存的 View 取出，不接受客户端覆盖**。字段和错误响应以 `docs/protocol.md`「助手」为准。

- `expected_version` 是用户看到并确认的整卡版本（正文卡固定 1）。带 confirm 的动作必须带；hub 在锁住卡片的事务里比对，不同就回 409 `view_changed` 和当前卡片，客户端保留草稿、刷新、让用户重新确认，不能自动换成新版本重发。这样确认框打开期间卡片在 hub 上被更新、而更新事件还没到手机的情况，也不会把新定义当成用户确认过的。

- 活卡片从 `assistant_views` 读取；正文卡片只从可信 node 提交的完整 transcript text item 注册，保存来源机器、会话、item id、块序号。用户消息、工具结果和其他对话不能注册成助手卡片。
- hub 校验设备权限、会话归属、卡片来源、动作是否存在、page 是否匹配、字段名／类型／选项／长度以及必需的确认。表单只收当前页允许的字段，拒绝额外字段。
- 同一张卡片只有一次持久化 reply 选择。用数据库约束协调并发；另一设备提交回 409 `already_answered` 和已有结果。同一 `client_msg_id` 的重试返回原记录，不产生第二条消息。
- 记录和待发送内容一起持久化，状态区分 pending、sent、failed；用稳定消息 id 重试，不能先标成已答，再因发送失败永久丢掉回复。跨请求状态不能只存在 hub 内存。
- 发给助手的正文为 reply 原文，加表单的可读内容，末尾带 `<persona-action view="…" action="…">{"values":{…}}</persona-action>`。属性用 XML 转义，JSON 中的 `<` 转义；客户端只隐藏严格识别的末尾标签。
- 事件 `assistant_view_action` 进入现有事件续传；feed 接口返回 `view_actions`。各端显示已选、提交的值及待发送／失败状态。open、push、pop 不锁卡片，仍能使用。

最初草稿中的 repeat 暂不加入协议：它与本任务「不能重复提交」的要求冲突。「再来一批」由助手更新或创建卡片，避免一开始就增加多次提交语义。

## 活卡片

| MCP 工具 | 参数 | 返回 |
| --- | --- | --- |
| `view_show` | `{view}` | `{id, version}` |
| `view_update` | `{id, view, version?}`，替换整张 | `{version}` |
| `view_get` | `{id}` | `{view, version, actions}` |

描述与参数说明放 `server/internal/prompts/mcp/assistant/`。工具调用的可信机器／会话由现有 MCP 路由提供，不能让模型指定要冒充的对话。只能读写本对话创建的卡片；别的对话、普通 Agent 和扩展不能更新。App 只能读卡片、提交动作。

Postgres `assistant_views` 保存内容、归属、版本和时间；创建与 feed 的 view 条目在同一个事务里完成。更新不增加 feed 条目，仍在原位置重画。已提交的结果不会被更新清除，助手也不能重新开启已答卡片。

广播 `assistant_view`，payload 为 `{conversation, view: {id, version, view, updated_at}}`，进入现有 event_seq 续传。feed 返回 `views` 快照，断线、重启后重新获取可恢复；客户端按 version 丢弃旧更新。保留仍存在的组件 key 对应的编辑和导航状态，但已提交的值以服务端为准。

每张最多 64 KB、每个对话最多 200 张。内容先持久化，推送事件同一卡片每秒最多一次，只合并通知，不能只在内存排队等待保存。创建和更新不单独发通知，需要提醒用户时助手用 notify_user。

## HTML 自由绘制

schema 加 `html` 组件：`{type: "html", html, height?, title?}`，HTML 自包含，最多 64 KB，高度 160–640（默认 320）。这是助手专用组件，不在扩展里执行。

用户已选择**默认自动运行**：进入可见区域加载，离开即销毁，不保留后台执行；提供原生「停止／重试」入口。`assistant.html_views` 关闭、平台不支持或安全验收未通过时显示 fallback。

bridge 只允许 `persona.reply(text)` 和 `persona.open(url)`，文本最多 2000 字，与 Action.reply 一致。两种调用都在卡片外的原生确认框显示原文；只有用户确认才执行。reply 走同一动作存储，一张卡片只发一次；拒绝后限制继续弹框。不能把 hub token、cookie、文件访问或任意原生对象暴露给脚本。

接口是独立的 `POST …/views/{key}/html-actions`（生成类型 `AssistantHTMLActionInput`）：`{kind, component_key, page?, expected_version, text? | url?, client_msg_id, confirmed: true}`。只有带 `key` 的 html 组件有 bridge，hub 核对它确实在保存的那一页上；reply 和声明式 reply 共用一次选择／outbox（记录 `kind: html`），open 校验后返回 `{open, version}`，不锁卡片。feature `assistant_html_bridge` 只说明这个接口可用，不代表客户端可以运行 HTML。

### 各端必须验证的安全边界

- Apple：复用 #2034 的非持久 WKWebView、网络规则、主 frame 导航拦截、单一消息 handler；补自动加载、开关和动作接口接线。不共享 App cookie，不开窗口、不下载。
- Android：隔离存储、关闭文件／内容访问，拦截网络及所有导航，包括 data HTML 中的子资源；不得调用全局 clearCookies 影响用户其他 WebView。bridge 仅接受当前实例的主 frame 消息。
- 网页：**普通 `iframe sandbox="allow-scripts"` 加 CSP 不足以保证禁网络**。不透明源可隔离 hub 存储，但 iframe 自身导航、meta refresh 等不能靠 default-src 全面阻止。不得把过时或未支持的 navigate-to 当作安全保证。任意 JS 的网页执行路径在通过网络与导航攻击用例前保持 fallback；优先评估隔离脚本运行时／受控绘制 bridge，仍使用同一个 html 组件，不降低原安全要求。
- CSP 放在所有模型内容之前，限制 fetch、图片、样式、字体、frame、worker、表单；受控运行时不得提供宿主网络函数。App 注入的 bridge 不能被页面重定义，不能接受来自旧实例或其他 frame 的消息。

安全用例必须真实观察请求次数，覆盖 fetch、XHR、图片、CSS、iframe、WebSocket、beacon、跳转、meta refresh、popup、表单、cookie／storage、伪造 bridge 消息和确认框轰炸。用不设沙箱的对照证明观测端确实能收到请求。

### 执行时间的限制不能夸大

一个 5 秒 ready 超时只能发现部分加载问题，**不能强制终止同线程的无限循环**；WebView 独立进程也不能保证浏览器宿主绝不卡顿。网页可执行路径须有可从外部终止的运行时。移动端限制可见卡片数、销毁生命周期、处理内容进程退出，并实测死循环、定时器和大分配；未达到边界的情况显示 fallback，不宣称有硬性 CPU／内存配额。

HTML 可以伪造登录框和文字，离线也不等于不可能经 reply 泄漏。原生外框始终标明「助手画的页面」，reply 原文必须完整展示确认，不能收密码。浏览器漏洞仍是风险，不能写成与任意网页完全等同或已经消除。

## 校验和自修复

共同的 View 语义检查放到 `sdk/go/wire`，hub 扩展与助手均调用；`CheckAssistantView` 在共同检查上增加 fallback、动作组合、页面引用、id 唯一、禁 secret 和大小限制，不复制另一份组件检查。

工具输入不合规直接返回字段路径、原因和修复办法，旧版本仍有效。正文完整块在可信 transcript 接收路径校验，客户端同样校验并显示 fallback；不能因为只加了 Stop hook 就宣称 hub 已经校验全部正文。

Claude 的助手 Stop hook 用 `last_assistant_message` 校验，首次失败返回 `decision: block` 和原因，`stop_hook_active` 为真时不再阻止，避免死循环。node 只检查可信助手的主 Agent，report-only／子 Agent／普通 Agent 不进入此分支；通过 `assistant.view.check` 请求 hub，hub 再确认会话身份并读取当前 persona_view 开关。最多检查 1 MB 的回复，返回最多 4 条、每条 2000 字的错误；调用最多等 5 秒，旧 hub 或断线时保留原 Stop 行为，卡片仍由正文注册／客户端校验兜底。

校验通过或不适用后才记录 Stop 和报告完成；被阻止的第一次尝试不能先广播「完成」。修正说明来自 `server/internal/prompts/assistant/view_repair.md`，不能在 Go 里拼模型提示词。其他运行时的下一轮 `<persona-view-errors>` 反馈仍待接入。

摘要、通知、任务回执和标题中的 persona-view 替换成 fallback，替换在裁剪之前完成；半截或坏 JSON 不打印原始代码。缺 fallback 的历史块可以保留旧只读渲染兼容，新工具和新协议必须校验精确的 `fallback` 字段名。

正文诊断按机器／对话／item 持久化，同一错误重放不重复报告，修正后不再等待反馈。诊断存储和助手专用 Stop 已实现。

### 下一轮反馈

Stop hook 只管 Claude 当轮；Codex、runtime 和 Stop 没拦下的 Claude 回复，诊断随下一条消息走（`assistant/context_view_errors.go`、`node/assistant_context.go`）：

- **放在哪**：`<persona-view-errors>` 接在这条消息的 `<jarvis-context>` 后面，是消息内容，不进系统提示（不破坏前缀缓存）。模板 `prompts/assistant/view_errors.md` 写明它是校验反馈，不是用户的新要求、授权或确认，不为修卡片重做已执行的操作，卡片与当前话题无关就不重画。
- **读不消费**：`assistant.context` 只读（`PeekAssistantViewErrors`），返回 `view_errors` 和每条回复的收据 `{item_id, digest}`。node 在消息真正送出后（Claude 写入 stdin 且进程已启动、Codex `turn/start` / `turn/steer` 成功、runtime `session.send` 成功）才调 turnContext 的 commit，再发 `assistant.context{session_id, ack}` 确认。发送失败、调试读取、重复读取都不标已反馈；确认丢了只会让下一条再说一次。
- **旧收据不吃新错误**：确认只标 item 和 digest（`sha256(errors::text)`）都对得上的行；读完之后同一回复的错误变了，旧收据什么也不标，伪造的 digest 同理。
- **可信隔离**：机器取自连接（hub 的 `machine.go`、助手进程的 `NodeAnswer`），node 传来的 `machine_id` 一律忽略；按对话 + 机器读和确认。`persona_view` 关闭时不读、不确认（开着时拿到的收据，关掉以后再确认也不生效）。
- **体积**：落库每条回复最多 20 个问题、每个 2000 字；每条消息最多 5 条回复、共 20 个问题，每个问题 300 字，超长时从中间截、保留末尾的原因（整块约 6K 字以内）。一条回复要么整条带上，要么整条留到下一条消息：只有问题全部列出的回复才有收据，所以确认从不会标掉模型没看到的问题；第一条回复总放得下（最多 20 个）。
- **不回显卡片原文**：引号里的值只保留简单的字段名，链接、HTML、JSON、句子都换成 `"…"`；校验信息里不带引号的页名、动作 id（`pages.<名字>`、`action.<id>`）按卡片 JSON 认出来，不是 40 字以内的简单名字就换成 `…`；65 字以上不带空格的串也换掉；`<` `>` 换成全角，不能闭合标签。这些在落库前就做（落库只留每个问题前 2000 字，先截再洗会留下没闭合的引号），读的时候再做一遍，没闭合的引号一直洗到末尾。
- **过期**：只有带了完整、校验通过并已注册的卡片的回复才算「改好了」；只提到 persona-view、卡片在示例代码块里、或卡片也坏了的回复都不算。这样一条回复写下的时间记成这段对话（按机器）的通过水位 `assistant_view_error_marks`，只升不降：不晚于水位的未反馈诊断全部清掉，之后才迟到落库的更早回复的诊断也直接记成已处理。各轮登记并发、顺序不定，所以每次写错误行（登记坏的、登记通过、确认）都先锁水位那一行、再碰错误行，同一顺序不会死锁（同一条回复的通过和旧诊断重放曾按相反顺序加锁而死锁）。没有时间戳的重放只清它自己、不动水位；超过 7 天的不再读。都在 Postgres，hub / node 重启后仍在。

## 提示词与开关

沿用 `assistant.persona_view`。关闭时仍教 persona-card；打开时教 persona-view 和工具。`assistant.html_views` 默认启用，但只有 persona_view 开启且平台执行路径通过安全验收时才实际运行，不因此提前改变生产主对话的输出。

提示词教清楚组件选择：决策用选项、比较用 table、数字用 metric／chart、过程用进度活卡片、收集信息用表单；一句话说得清就不画。每张在手机一屏内，细节放命名页或正文，最多 3 张，必须写 fallback，结论仍在正文。必须等待用户答案才能继续的决定、危险操作的授权继续用 ask_user，卡片不得绕过工具审批。

管理界面配置两个开关并预览静态示例。预览不连接真实对话、不发送动作；说明实际客户端支持状态。hello feature `assistant_views` 表示接口与事件可用，各端还要按实际能力选择 renderer 或 fallback。

## 示例和阶段

`schema/extension/examples/assistant/` 提供决策、比较、进度（更新前后）、表单、图表、HTML 各一组。各端读取同一份测试数据，截图只用合成数据。

| 阶段 | issue | 状态与剩余工作 |
| --- | --- | --- |
| 设计 | #2017 | 规格已合并；阶段进展与验收继续写回总 issue |
| schema | #2018 | 基础协议／生成类型／校验已合并；新共享组件仍待小 PR |
| hub | #2019 | 存储、工具、声明式 reply、正文注册和摘要已实现；expected_version、HTML bridge 接口和 feature（`assistant_views`／`assistant_html_bridge`）已合并；端到端验收未完成 |
| 提示词 | #2020 | 专用 Stop、主动组件选择／动作／活卡片提示词和 golden 已实现；下一轮错误反馈见上「下一轮反馈」（#2634），真实对话和 Codex 端到端验收未完成 |
| 网页 | #2021 | 复用 ViewBody 接好正文／活卡片的 reply、open、push／pop、确认、expected_version、409 处理和多设备同步（`web/src/lib/assistant-views.ts`、`web/src/ui/AssistantCard.tsx`），实际提交等 hub 发出 `assistant_views`；HTML 只显示文字 fallback：网页还没有通过网络／导航／死循环验收的隔离运行时，不运行 allow-scripts iframe |
| 安卓 | #2022 | 复用 #1851 组件，动作、活卡片、HTML、三风格深浅色截图 |
| 管理界面 | #2023 | `persona_view` 开关（hub 侧即时生效、助手提示词在下次准备目录时更新，保留 hub.json 其他配置）、各端支持说明、本地示例预览（HTML 只显示 fallback）已在 `/admin/assistant`；`html_views` 开关等 #2019 的配置字段合并后再接 |
| Apple | #2024、#2025 | 接入现有基础代码，动作、活卡片、HTML 自动运行；四端和手表验证 |

每阶段合并后部署前核对当前生产版本与备份，确保能回滚；端到端测试使用独立测试 hub 或授权的临时对话。测试不能发送到真实主对话，不能压缩、替换、删除用户对话。CI 环境异常按 CLAUDE.md 用本地验证合并，PR 如实列出未运行项。线上 persona_view 的开关保持关闭；临时对话端到端验收后先汇报，只有用户明确确认才可打开，本轮不发 TestFlight。
