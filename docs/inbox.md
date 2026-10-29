# 收件箱

「收件箱」tab 是 Jarvis 的信息入口：飞书消息和各个邮箱的邮件在**一个列表**里，按「件」聚合，AI 先分拣，人只看结论。
（2026-10-27 用户定的方向：不按群浏览、不做第二个飞书，见 `docs/feishu.md`。）

## 一件事 = 一行

| 来源 | 一件事是 | id |
| --- | --- | --- |
| 飞书 | 一个会话里还没处理的新消息（从第一条新消息起，后来的继续并进来） | `lark:<feedId>:<第一条的 position>` |
| 邮件 | 一个邮件线程里的未读邮件（主题去掉 Re: / Fwd: / 回复： / [列表名] 后相同即同一线程） | `mail:<账号>:<线程>:<第一封 uid>` |

- 一个会话 / 线程同时只有一件**未处理**的事；处理后再来的消息开一件新的（只算处理时最后一条之后的）。
- 什么会进来（飞书）：lark-connector 的 `unread` scope（飞书的「未读」，免打扰的会话不在里面）+ `mentions` scope（免打扰群里 @我 也算）。
  私聊 → `dm`，机器人 / AI 私聊 → `bot`，群里 @我 / @所有人 → `mention`，没免打扰的群 → `group`；7 天前的未读不算。
  只看别人发的：自己发的消息不会让一个会话变成「需要你」。
- 邮件：每个账号收件箱最新 40 封里的未读（7 天内）。列表接口只取正文前 2 KB（HTML 邮件常常全是 `<style>`），
  所以新邮件会再读一次全文（IMAP `BODY.PEEK`，不会标成已读）做预览和 AI 的上下文（`BodyOf` 记着读到哪封）。

## AI 分拣

`server/internal/hub/inbox_triage.go`：每件新事（或有新消息的事）交给 hub 的小任务模型（`hub.json` 的 `ai`，默认 DeepSeek flash）分成三档，
附一句不超过 40 字的摘要和一个很短的理由：

| tier | 界面 | 是什么 |
| --- | --- | --- |
| `action` | 需要你 | 要用户本人回复 / 决定 / 处理或尽快知道：直接提问、@了要做事、私聊找、审批确认、相关告警、要回的邮件、临近截止 |
| `fyi` | 知悉即可 | 有关、值得扫一眼、不用做什么：群里的进展、抄送、有信息量的通知 |
| `noise` | 已折叠 | 营销、订阅、newsletter、自动通知、无关闲聊、机器人刷屏 |

- 喂给模型的是类型（私聊 / @了用户 / 群 / 机器人 / 直接发给用户的邮件 / 抄送或列表）+ 这件事的文字（`context`：飞书最近最多 20 条新消息、
  邮件最新一封的全文前 3000 字）。称呼取 `assistant.owner`；`hub.json` 的 `inbox.rules` 是用户自己的分拣规则，附在提示词最后、优先。
- 拿不准 action / fyi 时选 action。模型失败时按规则兜底（私聊 / @我 → action，群 → fyi，直接发来的邮件 → fyi，其余 → noise），理由写「按规则」。
- 每件只在内容变了（`last` 变了）时重新分拣；后台并发 4 个，一轮跑完才开下一轮。分拣前的事在界面上算「需要你」。
- 2026-10-27 第一次跑：51 件里 3 件需要你、29 件噪音（全是营销邮件），摘要像「Sample Contact问 codex 代码是否已合入 1013，等你确认答复」。

## AI 分身（自动处理 + 草拟回复）

三个模型串起来（2026-10-29），全在 hub 后台跑，**只起草、不发送**：

| 步骤 | 模型 | 做什么 | 代码 |
| --- | --- | --- | --- |
| 1 收集 | `inbox.triage_ai`（小模型） | 上面的分拣：每件新事分 action / fyi / noise 并写摘要 | `inbox_triage.go` |
| 2 判断 | `inbox.judge_ai`（小模型） | 只看「需要你」、内容已分拣过的事：要不要交给 Agent（`handle`）、交给它做什么（`task`，一两句指令） | `inbox_agent.go` `judgeOne` |
| 3 处理 | `inbox.agent.launcher`（Claude Code 启动器） | 无头 claude，在助手目录里跑（所以有助手的全部工具：飞书、邮件、Agents、读代码、上网），查清楚后调 MCP `inbox_draft` 交草稿和依据 | `startInboxAgent`、`toolInboxDraft` |

- 状态记在条目的 `agent` 里：`skipped`（模型 2 不交）→ `queued` → `running` → `drafted` / `failed`。
  每件只在内容变了（`last` 变了）时重新判断，新消息来了旧草稿作废、重新走一遍。
- 并发 `inbox.agent.concurrency`（默认 1）；超过 `timeout_minutes`（默认 15）没交草稿，或这一轮结束了却没调 `inbox_draft`，算失败。
  最新一条早于 `max_age_hours`（默认 24）的事不交，打开开关时不会给积压的旧事一口气开一堆 Agent。
- Agent 的提示词禁止它自己发（`lark_send` / `mail_send` / `agent_send`）和用 `ask_user` / `notify_user` 打扰用户；`lark_send`、`mail_send` 本来也要手机批准。
  `inbox_draft` 只认「为某件事启动的那个对话」，普通助手对话调用会报错。
- 它的对话出现在「对话」tab（可以点进去看它查了什么），但跑完不推「完成」（`notifyFinished` 跳过）：结果就是收件箱里的草稿。
- App：行下面一张「AI 分身草拟的回复」卡片，**点「发送」直接发**（`POST /reply`，发完标已处理）；「修改」打开草拟浮层（已填好草稿，
  上方是 Agent 的依据和「查看 AI 分身的处理过程」）。处理中的行显示「AI 分身正在处理：<task>」。详情页操作条的按钮变成「查看草稿」。
  手表打开这件事时直接显示草稿和「发送」。
- 本地验证模型 1、2：`JARVIS_LIVE_AI='{"base_url":"…","api_key_file":"…","model":"…"}'` 跑 `TestInboxModelsLive`
  （经 `GOCMD=… ./scripts/test-server.sh ./internal/hub` 在 hub 机器上跑，key 文件在那台机器上）。

## 生命周期

- hub 后台采集（`server/internal/hub/inbox.go`）：飞书实时事件来了 3 秒后采一次，否则每 2 分钟；邮件每 3 分钟。下拉刷新
  （`POST /api/v1/inbox/refresh`）立刻采。有变化就存库（`inbox_items`）并推 `{"type":"inbox"}`，App 再拉列表。
- **已处理**只记在 Jarvis（`done_at`，`done_by=user`），不回写飞书已读、不改邮件的已读。已处理的保留 3 天（列表底部可移回），
  8 天后从库里删掉（期间用来挡住「飞书里一直未读」的旧消息重新冒出来）。
- 在别处看过（飞书里读了、别的邮件客户端读了）：标 `read`；不是「需要你」的直接算处理完（`done_by=read`），「需要你」的留着。
- 飞书里自己回复了（会话最新一条是我发的、在这件事之后）：算处理完（`done_by=replied`）。

## App

- `InboxStore`（`Jarvis/App/InboxStore.swift`）：列表存在本地（`inbox-items`），冷启动先画缓存；`inbox` 事件、重连、进入页面时拉。
  标已处理先改本地再发请求，失败恢复。Tab 角标 = 未处理里「需要你」的件数。
- 页面（`Jarvis/Views/Inbox/InboxView.swift`）：顶部一行筛选（全部 / 飞书 / 每个邮箱 | @我 / 私聊 / 群聊 / 邮件），搜索标题、发件人、
  内容、摘要。分三段：需要你（展开）、知悉即可（展开，标题右边「全部看过」）、已折叠（收起，「全部清除」），最后是已处理（收起）；
  段标题点一下展开 / 收起。行：头像（右下角来源小标）、标题 + 类型标签（@我 红色）、时间；有摘要时第二行是 ✦ 摘要、第三行小字
  「发件人：内容」，没分拣时直接是「发件人：内容」；件数。左滑到底 = 已处理。
- **等你的 Agent 也在「需要你」**：待处理的交互（提问、计划审批、授权、终端等你，包括助手的 ask_user；免打扰的除外）排在「需要你」最前面
  （`InboxAgentRow`，橙色图标 + 等什么 + 哪个 Agent / 哪台机器），点开跳到那个 Agent 的对话（助手的在「对话」tab）。只在 App 端合并，
  hub 的收件箱不存它们；Agents tab 照旧。筛选里的「Agent」只看它们；选了某个来源时不显示。
- 点开：飞书会话停在这件事的第一条新消息（`LarkChatView`，focus = start_position），邮件打开最新一封；右上角「已处理」。
  导航栏下面是操作条（`InboxActionBar`）：✦ 摘要 +「AI 草拟回复」「交给 AI」。
- DEBUG：`-tab mail`（打开收件箱）、`-openInboxFirst YES`（打开第一件未处理的）、`-showInboxAction draft|handoff`（再打开那个浮层）。

## 对一件事能做什么（`Jarvis/Views/Inbox/InboxActions.swift`）

- **AI 草拟回复**：`POST /api/v1/inbox/{id}/draft {instruction}`，hub 用小任务模型按用户口吻起草（飞书口语、短；邮件按来信的语言和正式程度；
  不知道的事实用【】标出来让用户补，不编；`assistant.notes` 作为「关于用户」附上）。浮层里可以直接改，也可以输入「让 AI 改：…」再重写。
  「发送」走 `POST /api/v1/inbox/{id}/reply {text}`：hub 在原会话发飞书消息，或按回复发邮件（Re: 主题、In-Reply-To / References、
  引用原文前 40 行，用收到它的账号），然后标为已处理（`done_by=replied`）。手机和手表都用这一个接口。
- **交给 AI**：把这件事作为 `<jarvis-context kind="context" title="飞书 · 群名">`（hub 给的 `context` + feedId / 邮件 uid，方便它用工具读更多、
  回复）加上「要它做什么」（默认「总结一下，告诉我要做什么，给出建议的回复」，另有两个预设）发出去：给 Jarvis 就新开一个对话（跳到「对话」），
  给某个 Agent（最近 8 个）就发进它的会话（跳过去）。默认同时标为已处理。
- **已处理**：行上左滑、详情右上角 ✓，或发送 / 交给 AI 时顺带。

## 手表

- 首页「需要你」= 等你的 Agent（原来的「等你处理」）+ 收件箱里 `action` 的事（`WatchInboxRow`：来源 / 类型、标题、AI 摘要）。
  iPhone 推给手表的摘要里带最多 20 件（`WatchSummary.inbox`），手表打开时每 8 秒直接拉 `/api/v1/inbox`；新的一件来了震一下。
- 点开（`WatchInboxItemView`）：摘要、最新一条；「AI 回复」先起草、显示出来，再点「发送」（`/reply`）；「已处理」。
- DEBUG：手表 `-openInboxFirst YES` 打开第一件。
