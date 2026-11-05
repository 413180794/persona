# 订阅额度窗口（#2432）

Codex、Claude Code 这类订阅有「5 小时」「每周」的用量窗口。这一页说 Persona 怎么把它们收上来、合并、画出来，以及为什么有的来源不用。
界面在网页版的侧边栏底部（`docs/web.md`「订阅额度」）、iOS 的机器页和 Mac 的分区列底部（下面「界面（iOS / Mac）」），数据来自各台电脑的 node，以及 hub 去问的账号池网关（比如 relay-history，见下面「账号池网关」，#2595）。

## 只读本机文件，不碰凭据

原则：**本机优先、只读、不读也不发送任何 token**。需要用户登录凭据才能问的来源一律不用，下面写清楚。

| 来源 | 怎么拿 | 用不用 |
| --- | --- | --- |
| Codex rollout | `~/.codex/sessions/…/rollout-*.jsonl` 里每个 `token_count` 事件带 `rate_limits`：`primary` / `secondary` 各有 `used_percent`、`window_minutes`、`resets_at`（秒），另有 `plan_type`、`limit_id`。node 读最近几个 rollout 的末尾（512 KB），取最后一条 `limit_id` 为 `codex`（或没写）的事件。`premium` 之类的单模型额度不算 | 用 |
| Claude Code 状态栏 | Claude Code 把 `rate_limits`（`five_hour` / `seven_day`，各有 `used_percentage`、`resets_at`）放在**状态栏命令的 JSON 输入**里（见它的更新日志），不写进 transcript 或别的文件 | 用，但要用户自己接一下（下一节） |
| Codex 的账号用量接口（ChatGPT 的 `wham/usage`） | 需要 `~/.codex/auth.json` 里的登录 token | **不用**。Moshi 用它，Persona 不读 `auth.json` |
| Claude 的账号用量接口（`/api/oauth/usage`）和响应头 `anthropic-ratelimit-*` | 需要 Claude 的 OAuth token（钥匙串 / `.credentials.json`），响应头还要自己发一次请求 | **不用**，同上 |
| 其他（Kimi、Grok、Antigravity、GLM / MiniMax 的 API Key 套餐） | 都要账号接口或 Key | 不做 |

Moshi 的做法（`moshi-hook usage`）是本地文件加这些账号接口，每台机器各查各的再上报。Persona 取它的本地文件那一半，换成 hub 汇总，接口那一半不做。

### 把 Claude Code 的额度接进来（可选）

Claude Code 只在状态栏命令的输入里给额度，所以 Persona 不能凭空读到。**Persona 不会改你的 `settings.json`**；想看 Claude 的额度，
把状态栏命令包一层，把 `rate_limits` 落到 node 的目录 `~/.persona/usage/claude.json`（多个配置目录用 `claude-<名字>.json`，每个文件算一个来源）：

```sh
#!/bin/sh
# 状态栏命令的包装：存下 rate_limits 给 Persona，再把原样的输入交给原来的状态栏命令
payload=$(cat)
dir="$HOME/.persona/usage"
mkdir -p "$dir" && chmod 700 "$dir"
printf '%s' "$payload" | jq -ec 'select(.rate_limits) | {rate_limits}' > "$dir/claude.json.tmp" 2>/dev/null \
  && mv "$dir/claude.json.tmp" "$dir/claude.json"
printf '%s' "$payload" | <你原来的状态栏命令>
```

文件里只留 `rate_limits`（不带会话、目录、费用）。node 读它的修改时间当作采样时间；`resets_at` 按秒读，毫秒和 RFC 3339 字符串也认。
网关的 `rate_limits.spend_limit`（金额）不是窗口，不读。

> 没验证的：状态栏字段的格式来自 Claude Code 的更新日志（「5-hour and 7-day windows with `used_percentage` and `resets_at`」），
> 单元测试用按它写的样例；没有拿真实 Claude 会话的状态栏输入核对过 `resets_at` 的单位。

## 采集、合并

- **node**（`server/internal/node/usagewindows.go`）：每 5 分钟读一次（第一次在连上 hub 20 秒后，等 hub 的 `node.configure` 先把开关带来），
  每个来源（一个 Codex home、一个 `claude*.json`）记最后一个样本，来源安静了也留着，超过 3 天丢掉。窗口的 `resets_at` 已过的丢掉（那是上个周期的数字），
  一个来源的窗口都过了就不列。`usage.windows` 直接回这份缓存（没有或过期了现读一次）。
- **关**：`hub.json` 的 `machines[].usage_windows`（`docs/config.md`），写 `false` 时那台电脑的 node 一个文件也不读，hub 也不问它；不写是开。
  节点不是这一版（没有 `usage.windows`）时 hub 当它没有数据。
- **hub**（`server/internal/hub/usagewindows.go`）：`GET /api/v1/usage/windows` 时问在线的机器（并行，每台 8 秒；30 秒内的重复请求直接用上一次的合并结果），
  记下每台机器最后一次的回答（只在内存，超过 3 天或机器从 hub.json 去掉就忘），然后合并：
  - **同一个账号只显示一次**：同一个来源（Codex / Claude）的两个样本，只要有一个窗口「长度和重置时间（精确到秒）都一样」就是同一个账号——重置时间是账号的，在哪台电脑上读到都一样，不需要账号 id，也就不碰凭据。
  - 同一个账号的几个样本按窗口长度取**最新样本**的那个窗口，一台很久没用的电脑不会把已经翻篇的 5 小时窗口带回来。
  - 样本超过 30 分钟标 `stale`（排在最后，界面变淡并写「已过期」）；超过 3 天、重置时间已过的窗口丢掉。
  - 重名的账号按 id 编号（「Codex Pro」「Codex Pro 2」），id 取最长窗口的长度和重置时间的摘要，一周内稳定。
  - 按最满的窗口从高到低排。

接口（只读，扩展令牌不能用，`testdata/route_scopes.txt`）：

```json
{"accounts": [{
  "id": "codex-1a2b3c4d", "provider": "codex", "name": "Codex Pro", "plan": "pro",
  "sampled_at": "2026-11-05T09:08:06Z", "stale": false,
  "windows": [{"label": "5h", "window_minutes": 300, "used_percent": 22.0, "resets_at": "2026-11-05T13:08:06Z"}]
}], "checked_at": "2026-11-05T09:08:20Z"}
```

没有数据时 `accounts` 是 `[]`；老 hub 没有这个路由（404），网页当作没有数据。

## 账号池网关（relay-history…，#2595）

账号池网关替你养着好几个 Codex / Claude / 别家的订阅账号，**自己已经知道每个账号的 5 小时、每周额度用了多少**（relay-history 把上游随普通请求回来的响应头存下来）。
这些数字不必每台电脑再各查一遍：hub 去问网关，读它存好的数字，合并进侧边栏。电脑上读不到的账号（网关里养着的、本机没登录的）也能看到。

### 为什么做成 hub 的内置来源，不是扩展

`docs/extensions.md` 的原则是「开源仓库只放通用能力和扩展点，只对某个公司、某个服务有意义的东西做成扩展」。这件事的取舍：

- **已有的 `quota` 扩展点不合适**：它回答的是「这把启动器 Key 在网关里还剩多少预算」，要用启动器的 Key，回 `quota.Report`；这里要的是网关**养着的上游账号**的订阅窗口，用网关的管理 Key，形状完全不同。
- **新开一个扩展点要一整套**：清单字段、schema 生成的四端类型、权限、进程宿主、市场里的包。而网关要的只是「一个地址 + 一把 Key，每 5 分钟读一次，把数字映射成窗口」，hub 里几百行。
- 所以：**来源是 hub 里一个很小的概念**（`hub.json` 的 `usage.sources[]`，一个来源一种 `type`），hub 内置两种适配：
  - `generic`：**Persona 自己定义的通用格式**，任何账号池网关实现这一个 GET 就能接进来（下面）。
  - `relay-history`：relay-history 的管理接口（它是同一个组织的开源兄弟项目，接口公开、稳定），适配器只是把它的两个只读接口映射成同一个形状。
- 以后真有第三种、第四种形状不同的网关，再把适配器抽成扩展点（`usagesource.Fetch` 是唯一的入口，抽的时候不改别处）。**这是一个权衡，不是教条**：relay-history 的适配器写在核心里，是少数的一个具体服务的例外，换来的是装上就能用、不用装扩展。

### 配置

`hub.json`（`docs/config.md`「usage」）；密钥只在这里（0600），接口和日志里都没有：

```json
"usage": {
  "poll_minutes": 5,
  "sources": [
    {"id": "src-1a2b3c4d", "name": "relay-history", "type": "relay-history", "url": "https://gateway.example.com", "key": "<管理 Key>"},
    {"id": "src-5e6f7a8b", "name": "Team pool", "type": "generic", "url": "https://pool.example.com/persona/usage", "enabled": false}
  ]
}
```

| 键 | 意思 |
| --- | --- |
| `usage.enabled` | 写 `false` 是总开关：不再问任何网关（电脑自己读到的不受影响）。不写是开 |
| `usage.poll_minutes` | 每个来源隔多久读一次，默认 5，1 到 1440 |
| `usage.sources[]` | 最多 8 个。`id` 是 hub 起的（`src-…`，管理界面生成），`name` 是侧边栏上的来源名（最长 40 个字），`type` 是 `relay-history` / `generic`，`url` 是 http(s) 地址（不能带用户名密码），`key` 作为 `Authorization: Bearer` 发出，`enabled` 写 `false` 是这一个停用 |

管理界面（`/admin/usage`，`docs/admin.md`）里增删改、开关、「测试连接」，改完立刻生效，不用重启。手改 `hub.json` 的话，下次打开这个页面时 hub 会采用磁盘上的版本。

### 读什么、怎么映射

**只读，永远不碰上游**：

- relay-history：`GET /api/admin/accounts?view=compact`（账号列表：`id`、`name`、`provider`、`status`，**没有邮箱、凭据、代理地址**；更老的网关忽略 `view` 回整个数组，也认，只取这几个字段），
  再 `POST /api/admin/accounts/usage {"ids": […], "refresh": false}`（每次最多 100 个、总共最多 500 个账号）。`refresh` 写死 `false`：网关只读它的库，不会去问上游
  （`refresh=true` 会让网关从它唯一的出口地址探测上游，用掉整个池子的探测额度）。不调用别的路由，更不调用任何写路由。
- **建议用分组 Key**：relay-history 的 `onboarder` / `operator` 分组管理 Key 只看得到本组账号（包括 `/accounts/usage`，见 relay-history 文档的 admin-access 一篇），读不到别的组的；比部署 Key 小得多。
  账号在两次调用之间被换组 / 删掉，用量接口回 404 时这一轮读失败（界面写「这个地址不是网关的管理接口，或网关版本太旧」，下一轮重试）。
- 超时（一次请求 15 秒，一个来源一轮 45 秒）、回答最多 8 MB、不跟重定向（Key 不会被带到别的地址）、错误只留一个代码，不留网关回的文字，也不进日志（日志只在代码变化时写一行 `usage source <id>: <code>`）。

映射（`server/internal/usagesource`）：

| 网关的 | 侧边栏的 |
| --- | --- |
| `provider`（`codex`、`claude`、`kimi`…） | 一样；`codex` / `claude` 能和电脑上读到的同一个账号合并 |
| 账号 `name` | 账号名（网关里有人起的名字，比电脑上的「Codex Pro」好认；合并时用网关的名字） |
| `plan_display_name`（没有就 `plan`） | 计划，写在名字旁边（名字里已经有就不重复） |
| 窗口 `kind` | 窗口长度：`five_hour` / `5h` → 5h，`weekly` / `7d` → 7d，`hourly`、`daily`、`monthly`（30 天）、`<n>m|h|d|w`；`weekly_fable`、`gemini_argon_weekly` 这类把长度当成其中一个词的也认。**读不出长度的（`credits`、`blocked`…）不是时间窗口，不画** |
| `scope: "model"` / `scoped_model` | 单模型的额度：同样长度的窗口旁标出模型名（「7d Fable」），**不参与账号排序，也不用来认同一个账号**（一个模型用完不等于账号用完） |
| `used_ratio`（0–1） | 已用 %（没有 `used_ratio` 但有 `remaining` 和 `limit` 时用它们算；都没有的窗口不画） |
| `resets_at` | 重置时间 |
| 窗口 `checked_at`（最新的，没有就条目的） | 采样时间 |
| `unsupported`、整个账号没有可画的窗口、`status: disabled` | 不列 |
| `stale`、`error` | 标过期；`error` 的账号在浮层里写「网关读这个账号失败，显示的是它存着的数字」（错误文字不带出来） |

网关里 relay-history 的数字来自随普通请求回来的响应头，**账号闲着就不更新**，所以它的样本比电脑上的老得慢：超过 6 小时才标过期（电脑上的样本 30 分钟），界面上的采样时间写的是数字真正的年龄。
没有拿真实的 relay-history 核对过各个 `kind` 的取值：映射表按它源码里的 `quotaKindByWindowMinutes` / `quotaKindByBucket` 写，单元测试用按它的 JSON 标签写的样例（见「限制」）。

### 和电脑上读到的合并

hub 把网关的账号和各台电脑的样本放在一起，按原来的规则合并（上面「采集、合并」）：**同一个来源（provider）、同样长度、重置时间精确到秒都一样**就是同一个账号，不需要账号 id。
几条补充：

- 同一个网关里的两个账号（`id` 不同）**绝不合并**，不管重置时间碰巧是不是一样；两个网关（或网关和电脑）各看到的同一个上游账号会合并。
- 合并后的账号取最新样本的数字，列出来源：`本机`（电脑上读到的）和网关的名字，浮层标题下一行写「本机 · relay-history」；只有一个来源的写那一个。
- 网关**连不上就留着它的上一次回答并标过期**（`problem: "unreachable"`，浮层里写「网关暂时读不到，显示的是上次的数字」），不让账号消失；超过 3 天丢掉。
- 排序不变：按账号的计划窗口里最满的一个从高到低，过期的排最后。没有任何数据（也没有来源）时整块不画。

### 通用格式（`type: "generic"`）

任何账号池网关想把额度送进侧边栏，实现一个 GET：hub 带 `Authorization: Bearer <key>`（没填 Key 就不带）读这个完整地址，回 JSON（就是 `GET /api/v1/usage/windows` 的回答，所以一个 Persona hub 也能喂另一个）：

```json
{"accounts": [{
  "id": "acct-1",
  "provider": "codex",
  "name": "Team seat 3", "plan": "Pro",
  "sampled_at": "2026-11-05T09:08:06Z",
  "windows": [{"label": "5h", "window_minutes": 300, "used_percent": 22.0, "resets_at": "2026-11-05T13:08:06Z", "model": ""}]
}]}
```

`id` 可选（网关里这个账号稳定的标识，用来保证两个账号不被合并）；`label`（`5h`、`7d`、`45m`）读得出长度就用，读不出用 `window_minutes`；`used_percent` 是 0–100，越界按边界算；
`sampled_at` 不写是读取的时刻；每个来源最多 500 个账号、每个账号最多 32 个窗口，`provider` 是空的账号丢掉。


## 界面（网页版）

- **侧边栏底部**：最满的两个账号各一条细条（名字、最满窗口的百分比），≥ 80% 用警示色；过期的变淡；鼠标放上去的提示里写来源。**没有数据时整块不画**，侧边栏和以前一样。
- **点开**：每个账号每个窗口一行：窗口长度（5h / 7d）、已用百分比、一个条、重置倒计时、节奏提示，账号标题右边是采样时间（「12 秒前」）。
  账号标题下一行写计划和来源（「本机 · relay-history」），单模型的窗口在长度旁写模型名，网关读不到时写一句说明（见「账号池网关」）。Esc 或点别处关。
- **重置倒计时**：不到 1 小时写 `45m 后重置`；今天之内写时刻 `13:05 重置`；更久写 `2d 3h 后重置`。
- **节奏**（`lib/usage-windows.ts` 的 `pace`）：窗口长度从标签读（`5h`、`7d`，读不出来用 `window_minutes`），应有进度 = 已过时间 / 窗口长度，
  实际比应有高出超过 5 个百分点是「用得偏快」，低了超过 5 个百分点是「用得偏慢」，之间不写；条上画一根刻度标出应有进度的位置。
  窗口刚开始的 5% 里不判断也不画刻度（刚开始的几分钟，用一点点都算「超前」）。
- 每分钟刷新一次，页面不在前台时不请求。

## 界面（iOS / Mac，#2889）

和网页同一套规则（纯函数在 `apple/Shared/UsageWindows.swift`，单测 `UsageWindowsTests` 跟网页的用例一样；视图在 `Persona/Views/Machines/UsageWindowsViews.swift`）：

- **iOS**：机器页（抽屉 / 设置 / Agents 菜单里的「机器」）的卡片下面一节「订阅额度」，直接列出每个账号每个窗口（手机上没有悬停，不做细条 + 浮层两层）。
  账号标题右边是采样时间，下一行是计划和来源，网关读不到时一句说明；每个窗口一行：长度、已用、偏快 / 偏慢、重置倒计时，下面一根带节奏刻度的条。没有数据时整节不画。
  页面在前台、App 在前台时每分钟问一次（`tabActiveTask`），下拉刷新也问。DEBUG：`-tab dev -openMonitor all`。
- **Mac**：分区列底部、连接状态上面，最满的两个账号各一条细条（`MacUsageFooter`），≥ 80% 用警示色、过期的变淡，悬停的提示写来源；点开是同样的明细浮层（Esc、点别处关）。
  App 在前台时每分钟问一次；机器页里不再重复画。
- 体验模式（`DemoHub`）有一个 Claude、一个 Codex 账号；假 hub 的 `MOCK_USAGE=gateways` 看电脑和网关混合的账号。
- 多组 hub 时每组各问各的，合在一起排（同一个账号在两组 hub 里各出现一次，不跨组合并）；老 hub（404）当作没有数据。

## 限制

- Claude 的额度要用户自己接状态栏（上面），否则只有 Codex。
- Codex 的数字只在用过 Codex 时才更新；整台机器都没用过它，数字就停在最后一次。界面上看采样时间就知道多旧。
- hub 重启后，已经下线的机器的上一次回答没了，要等它上线再问；网关的上一次回答也在内存里，重启后第一轮（几秒内）读到前侧边栏没有它的账号。
- **没有拿真实的 relay-history 验证过**（这台开发机上没有在跑的网关）：适配器按它的源码（`internal/application/accountusage`、`internal/domain/account` 的 JSON 标签、`accountpresentation.Compact`）和它文档的 account-read-views、admin-access 两篇写，
  测试用一个假的 relay-history HTTP 服务（账号列表、用量、错误、过期、分组 Key 的 404、旧网关的裸数组）。真正接上以后用管理界面的「测试连接」看一眼账号数和窗口数。
- 同一个 Claude 账号在电脑上（状态栏给的 `resets_at`，秒）和网关里（响应头或用量接口的时间，可能带小数秒）的重置时间要落在同一秒才合并成一个；差一秒时会显示成两个账号，没有拿真实数据核对过。
- 网关的 `kind` 里读不出长度的窗口不画（上面的映射表）；relay-history 以后加了新的 `kind` 词（比如 `three_hour`）要加进 `usagesource.KindMinutes`。
