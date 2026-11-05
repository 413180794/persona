# 订阅额度窗口（#2432）

Codex、Claude Code 这类订阅有「5 小时」「每周」的用量窗口。这一页说 Persona 怎么把它们收上来、合并、画出来，以及为什么有的来源不用。
界面在网页版的侧边栏底部（`docs/web.md`「订阅额度」），数据来自各台电脑的 node。

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

## 界面（网页版）

- **侧边栏底部**：最满的两个账号各一条细条（名字、最满窗口的百分比），≥ 80% 用警示色；过期的变淡。**没有数据时整块不画**，侧边栏和以前一样。
- **点开**：每个账号每个窗口一行：窗口长度（5h / 7d）、已用百分比、一个条、重置倒计时、节奏提示，账号标题右边是采样时间（「12 秒前」）。
  Esc 或点别处关。
- **重置倒计时**：不到 1 小时写 `45m 后重置`；今天之内写时刻 `13:05 重置`；更久写 `2d 3h 后重置`。
- **节奏**（`lib/usage-windows.ts` 的 `pace`）：窗口长度从标签读（`5h`、`7d`，读不出来用 `window_minutes`），应有进度 = 已过时间 / 窗口长度，
  实际比应有高出超过 5 个百分点是「用得偏快」，低了超过 5 个百分点是「用得偏慢」，之间不写；条上画一根刻度标出应有进度的位置。
  窗口刚开始的 5% 里不判断也不画刻度（刚开始的几分钟，用一点点都算「超前」）。
- 每分钟刷新一次，页面不在前台时不请求。

## 限制

- Claude 的额度要用户自己接状态栏（上面），否则只有 Codex。
- Codex 的数字只在用过 Codex 时才更新；整台机器都没用过它，数字就停在最后一次。界面上看采样时间就知道多旧。
- hub 重启后，已经下线的机器的上一次回答没了，要等它上线再问。
