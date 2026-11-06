# 启动器健康检查和自动修复（#2873）

各台电脑上的启动器会悄悄坏掉：网关不认裸模型名（`400 Bare Claude model name cannot be routed`，换模型后 `no account match`）、
网关 Key 失效、CLI 太旧，或者起 Agent 一直超时（`context deadline exceeded`）。以前没人知道，派活的 Agent 第一句就失败。
现在 hub 的管理者（`server/internal/assistant/launcher_health*.go`）定期检查每台电脑的每个启动器，派活时绕开坏的，已知的错误自己修，修不好的记成阻塞。
**只记录，不推送**：不发通知、不进灵动岛；App 和看板自己来读。
**默认关**（2026-11-05 用户决定）：hub.json 写 `agents.launcher_health.enabled: true` 才定期检查和自动修；关着时起 Agent 失败照样记、照样绕开。

## 检查

- 用的就是「测试可用性」（`docs/launchers.md`）：node 的 `launchers.check` 在空目录里跑一次 `claude -p … --max-turns 1` / `codex exec`，90 秒超时，会花几个 token。
- 什么时候：每 5 分钟看一次（`healthTick`），在线（`Reachable`）的电脑上，上次检查超过间隔（默认 30 分钟）的启动器重跑一次；
  电脑从离线变成在线时，它的启动器马上全跑一遍。一台电脑上的启动器挨个跑（不同时起一堆 claude），各台电脑并行。
- 查哪些：这台电脑能启动的（`machineLaunchers`：~/.zshrc 的去掉隐藏的，加上 Persona 管理的；`kind: api` 的不在电脑上跑，不查），
  runtime 的（DSH 等，`source` 不是 `zshrc` / `jarvis`）不查。
- 起 Agent 失败也记一次：`agent_start`、任务计划起 Agent（`startTaskAgent`）调 `session.start` 出错时记成失败（来源 `start`），
  再马上补一次检查确认（一次链路抖动不该让启动器一直不可用；检查通过就恢复）。
- 记在 settings `launchers.health`（Postgres），一项一台电脑 × 一个启动器：成功 / 失败、错误文本（打码、截断）、错误分类、
  检查时间、连续失败次数、修复记录、状态。hub 重启后接着用。

## 错误分类（`classifyLauncherError`）

| 分类 | 认什么 | 自动修复 |
| --- | --- | --- |
| `model_route` | `Bare Claude model name`、`cannot be routed`、`no account match`、模型不存在 / 不支持 | 修复 Agent（重新配网关的模型） |
| `auth` | 401 / 403、`invalid … api key`、`authentication_error`、`Not logged in`、token 过期 | 修复 Agent（重新配网关的 Key） |
| `cli_outdated` | `unknown option`、要求升级 / 版本太旧 / 不再支持 | 先 node 自己升级（`claude update`），不行再修复 Agent |
| `timeout` | `context deadline exceeded`、90 秒没回答 | 连续两次才算，交给修复 Agent |
| `other` | 其余 | 同上，连续两次才修 |

## 状态

`ok` → 失败了 `failing` → 正在修 `repairing` → 修好复测通过回到 `ok`，复测还失败 `blocked`。任何时候一次检查通过就回到 `ok`、清掉阻塞。
`failing` / `repairing` / `blocked` 都算**不可用**（最近一次结果 6 小时内）。

- `agents_list`：末尾多一段「不可用的启动器」，每行 电脑 / 启动器 / 状态 / 分类 / 错误。
- 新建 Agent 的启动器列表（`GET /api/v1/machines/{mid}/launchers`）每项带 `health`（`{state, error, kind, checked_at}`），App 可以据此标灰（UI 另做）。
- `agent_start`、`task_plan`：要用的启动器在这台电脑上不可用时，换一台这个启动器健康的在线电脑（目录在原电脑 home 下的换成新电脑的 home），
  回答 / 卡片的「理由」里写明换了、为什么；找不到健康的就照原样起，回答里提醒最近一次检查失败了。任务计划在出卡片时换一次，真正起的时候再看一次。
  健康的优先：最近检查通过的 > 没查过的；没查过的不当成坏的。

## 自动修复

- 一台电脑一次只修一轮（同一台上的几个启动器常是同一个原因：网关配置坏了，全部启动器都 400），一轮最多修两次，之后记 `blocked` 等人；
  阻塞的启动器照常按间隔复测，通过了就自己恢复。
- **升级 CLI**：node 的 `launchers.repair {name, action: "upgrade_cli"}` 只做固定的事：Claude 的启动器跑 `claude update`（登录 shell，5 分钟超时），
  别的回「不支持」。不接受任意命令。旧 node 没有这个方法，当作升级不了。
- **修复 Agent**：hub.json `agents.launcher_health.repair` 配了 `skill` 时，在 `machine`（默认助手所在的电脑）的 `cwd`（默认 home）上用一个健康的启动器起一个 Agent，
  首条消息（`server/internal/prompts/agents/launcher_repair.md`）说明哪台电脑哪些启动器坏了、错误原文和分类、用哪个 skill 修、修完用 `claude -p` 自测。
  它挂在一个**安静的待办**上（`Quiet`、没有来源对话）：hub 不为它推送，Agents 页和待办里看得到。它停下（不在工作、也不在等你）或 45 分钟后，
  管理者复测这一轮的启动器，过了就 `ok`，没过就 `blocked`（原因带上修复 Agent 最后说的话）。没配 `skill` 时只有升级 CLI 这一种修法，其余直接 `blocked`。
  skill 名字、修复用的电脑都是配置，代码里不写具体的工具和网关。

```json
"agents": {
  "launcher_health": {
    "enabled": true,
    "interval_minutes": 30,
    "repair": { "skill": "<修启动器的 skill>", "machine": "<机器 id>", "cwd": "~", "launcher": "", "permission": "" }
  }
}
```

`enabled` 不写或 `false`：不定期检查、不自动修（状态照旧能读，`agent_start` 失败照样记、补测一次、派活绕开）。
`permission` 是修复 Agent 的权限模式，不写是 `bypassPermissions`（2026-11-05 用户决定：它要跑 ssh 改配置，别的模式会停在授权卡片上等人）；
想让它每步都等你批准就写 `default`。mesh 的成员 hub 不查（电脑归中心）。

## 给看板的接口

- `GET /api/v1/launchers/health?state=blocked&machine=`：全部记录（或按状态、电脑筛），`{records: [{machine, launcher, state, kind, error, checked_at, source, fails, repair?, blocked_reason?}]}`。
- `POST /api/v1/launchers/health/check?machine=&launcher=`：马上查（不带参数查全部在线电脑），回这次的结果。
- 状态变了广播一条 `launcher_health` 事件（`{type, record}`，只给连着的客户端，不是推送）。
  「决策 / 阻塞 / 进度」看板读 `state=blocked` 的、或订这个事件，就能把阻塞列出来。

## 没做的

- Agent 起来了但第一轮才失败（网关不认模型名那种，`session.start` 是成功的）不从会话里记；靠定期检查发现。
- App 里标灰不可用的启动器。
- App 里「新建 Agent」（`agents.start_params`，用户自己挑的电脑）不换电脑；飞书机器人起的 Agent 也还没接。
