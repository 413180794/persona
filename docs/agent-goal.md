# Agent 的 goal（目标）

#2420。Claude Code 的 `/goal <条件>` 和 Codex 的 thread goal 都是「给 Agent 一个完成条件，没达成就不停」：Claude Code 每次要停下时由一个
Stop hook 判断条件成没成立，没成立就接着做；Codex 记一个目标和它的状态（进行中、暂停、受阻、用量到了上限、完成）。
以前 Persona 只把当时敲的 `/goal …` 当成一条普通消息显示，往上翻久了就找不到，达成、清除、没达成的理由都看不到（这一节之前的样子）。

## 用户看到的

- **对话页顶部**（手机的展开标题、iPad / 分栏的侧栏标题、Mac 对话上方那一行、Android 顶栏下面）：有 goal 时多一行「目标」条：图标 + 目标的第一行 + 状态
  （进行中 / 已达成 / 已暂停 / 受阻 / 用量到上限）。点按打开详情：目标全文、状态、最近一次判断的理由（Claude Code）、已用的 token 和时间、
  token 预算（Codex）。没有 goal 时这一行不出现，界面和以前一样。
- **对话里**：goal 达成时插一条通知「目标已达成」（Claude Code 带判断的理由）；Codex 的 goal 变成暂停、受阻、到了用量上限时各插一条。
  设置和清除在 Claude Code 里本来就有「Goal set: …」「Goal cleared: …」两条命令输出的通知，照旧。
- 手机上标题折叠时（默认往下读时就折叠），折叠的标题栏右边有一个目标图标（颜色同状态），点按直接打开详情。
- 网页的对话页同样在顶部的活动行显示，点开是同样的详情。
- 已达成的 goal 继续显示（状态「已达成」），直到设置新的 goal 或清除；清除后这一行消失。

## 两种来源的格式（2026-11-05 用本机真实 transcript / rollout 核对）

### Claude Code（transcript 的 jsonl）

goal 的状态写成 `type: "attachment"` 的一行，`attachment.type` 是 `goal_status`：

| 什么时候 | 字段 |
| --- | --- |
| 设置（`/goal <条件>`） | `{condition, met: false, sentinel: true}`，写在命令那一行之前 |
| 要停下、判断没达成 | `{condition, met: false, reason}` |
| 判断达成 | `{condition, met: true, reason, iterations, durationMs, tokens}`；之后 hook 自己清掉 |
| 清除（`/goal clear`） | `{condition, met: true, sentinel: true}`，接着是命令输出 `Goal cleared: <条件>` |

- 命令和输出：新版写成 `type: "system", subtype: "local_command"`，老版写成 user 行；输出是 `<local-command-stdout>Goal set: <条件>` /
  `<local-command-stdout>Goal cleared: <条件>`。有的老版本只有这两行、没有 `goal_status`，所以两者都认：「Goal set」设一个进行中的 goal，
  「Goal cleared」清掉。
- 设置时还有一条 `isMeta` 的 user 行（「A session-scoped Stop hook is now active…」），是给模型看的，照旧不显示。

### Codex（rollout 的 jsonl）

`type: "event_msg"`、`payload.type: "thread_goal_updated"`，`payload.goal` 是整份当前状态：
`{threadId, objective, status, tokensUsed, timeUsedSeconds, tokenBudget?, createdAt, updatedAt}`；`status` 是
`active` / `paused` / `blocked` / `usage_limited` / `budget_limited` / `complete`（同 `~/.codex/goals_*.sqlite` 的 `thread_goals` 表）。
用量变了也会再写一条。`goal` 是 `null` 时当作清除。本机语料（Codex 0.166）里只见过 `active`，其余状态的取值来自那张表的约束，没有实测。

## 怎么做

- **解析**（`server/internal/transcript`）：`Meta.Goal`（`proto.GoalInfo`）跟着读到的行更新，以最新的一条为准；Claude Code 的 `met`
  映射成 `complete`、`met: false` 是 `active`，判断理由放 `reason`。达成和 Codex 的非进行中状态在对话里生成 `notice`，只在状态变了时生成。
- **node**：goal 可能设在一份几百 MB 的 transcript 的开头，只读尾部 384 KB 的 quick meta 看不到。所以和 `/loop`、后台任务一起由 `bgScan`
  读：整份读一次，之后只读追加的部分，只解析含标记的行（`goal_status`、`Goal set: `、`Goal cleared`、`thread_goal_updated`）。
  会话列表上带 `goal`，列表的签名（`node/sessions.go` 的 `listKey` / `listKeySized`）里带上目标、状态和理由，变了就推一次。
- **协议**：`Session.goal`，见 `docs/protocol.md`。只加字段，旧客户端不认就不显示。
- **客户端**：iOS / Mac 的 `GoalBar`（`Views/Session/SessionPanes.swift`）和详情 `GoalSheet`；网页 `views/chat/Activity.tsx` 的活动行。
  Android（#2888）：`core:contracts` 的 `Session.goal`（`GoalInfo`），对话页顶栏下面的 `ChatGoalBar`（`feature/chat/ChatGoal.kt`，设计系统的 `JActivityBar`），点按打开 `ChatGoalSheet`；
  对话里的达成 / 停下通知是 hub 生成的 `notice`，照普通通知显示。顶栏不折叠，所以没有 iOS 折叠标题里的那个目标图标。
  对话条目（item）的结构没有变，`ChatCache` 的版本号不用加。

## 截图

假 hub 加 `MOCK_GOAL=1`：MacBook 上「给天气 App 加主屏幕小组件」有一个进行中的 goal（带最近一次检查的理由），「检查 PR #42 的改动」的 goal 已达成；
`MOCK_GOAL=stopped` 时小组件那个 goal 用完了 token 预算。

## 不做的

- 不在 App 里设置、暂停或清除 goal：在终端里或者在输入框里发 `/goal …` 就行（Claude Code 的命令照常发过去）。
