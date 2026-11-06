# 脚本命令：`personad ask` / `personad notify`

让脚本、CI、长任务、不在 herdr 里跑的程序直接找手机：

- `personad ask "问题"`：阻塞到手机上有人回答，答案变成退出码和 stdout。
- `personad notify "标题" [正文]`：发一条通知，默认尽力而为。

灵感来自 moshi-hook 的同名命令。和 `personad hook` 一样，两条命令都只和本机的 node 说话（`~/.persona/node.sock`，0600，同一个系统用户），
不碰 hub 的 token，也不需要网络配置：node 连着 hub 就能用，没连就按下面的退出码处理。

## 命令和退出码

```
personad ask "问题" [--options a,b,c] [--timeout 10m] [--require-remote]
personad notify "标题" [正文] [--silent] [--error] [--strict]
```

`ask` 的参数可以写在问题前面或后面；问题以 `-` 开头时放在 `--` 后面。`--timeout` 写秒数（`90`）或时长（`10m`），默认 30 分钟（同 hook），最长 24 小时。
`--options` 用逗号分隔，所以选项本身不能带逗号；空项和重复的项去掉，最多 12 项、每项最多 100 个字。

| 退出码 | `ask` | `notify` |
| --- | --- | --- |
| 0 | 批准（无选项）或选了某个选项（选项打印在 stdout，一行） | 总是 0（尽力而为）；`--strict` 时表示 hub 收下了 |
| 1 | 拒绝；或手机上点了「交回电脑」而终端里没人可问 | 只有 `--strict`：hub 没收下（node 不在、hub 没连、被静音、hub 太旧） |
| 2 | 用法错误（没有问题、选项太多、`--timeout` 不合法……） | 用法错误（没有标题、多于两个参数……）。这是写脚本时的错，不算「尽力而为」 |
| 3 | 超时，没人回答 | - |
| 4 | 联系不上手机：没有 node、node 没连 hub、hub 没收下 | - |
| 130 | Ctrl-C / SIGTERM；手机上的卡片随之撤掉 | - |

- 退出码 4 或「交回电脑」时，如果 stdin 是真终端（不是 `/dev/null`、管道）且没有 `--require-remote`，退回终端里问：选项编号列表（输入编号或选项原文），
  无选项是 `[y/N]`。EOF 算拒绝。超时和拒绝是回答，不退回终端。
- 没有 `--strict` 时失败只在 stderr 写一行「没发出去：原因」，退出码仍是 0，`set -e` 的脚本不会被通知打断。
- `--strict` 的「确认」是 hub 收下并发出（`{"sent":true}`），不是手机已经展示：通知的送达本来就没有回执（见 `protocol.md`「常驻连接通知」）。

## 链路

```
personad ask / notify
  └─ node.sock  {"kind":"ask"|"notify","input":{…},"pane":"…","attempt":"…"}   一次连接一问一答，各一行 JSON
       └─ node（node/scriptcli.go）
            ├─ ask    → hub 请求 interaction.create（和 hook 同一条路）→ 阻塞到回答 / 过期
            └─ notify → hub 请求 notify（hub/notify_api.go 的 deliverNotify，和 POST /api/v1/notify 共用）
```

node 的回答：

- `ask`：`{"status":"answered|denied|dismissed|timeout|unavailable|invalid","answer":"…","error":"…","retry":true}`。
  `retry` 是 node 正在退出（hub 或 node 重启），命令带同一个 `attempt` 重连下一个 node，最多 `node.ResumeWait`（2 分钟），做法和 hook 完全一样。
- `notify`：`{"sent":true|false,"reason":"muted","error":"…"}`。

旧 node 不认 `ask` / `notify`（回 `{"error":"unknown kind"}`），命令当作联系不上；node 新、hub 旧时 `notify` 请求报 unknown method，同样当作没发出去。

### `ask` 在手机上是什么

复用已有的卡片形状，**Android / 网页不用改**（iOS / Mac / 手表上没挂会话的卡片怎么答见下面「归属」，#4059）：

| 命令 | `interaction.kind` | 卡片 |
| --- | --- | --- |
| 有 `--options` | `question`，一个 `Question`（`header` 是「脚本」，`multi_select: false`，每个选项一个 `label`） | 和 AskUserQuestion 一样的提问卡；回答是 `answers[<问题原文>]`，选项或手写的内容（卡片支持「其他」时） |
| 没有 `--options` | `permission`，`summary` 是问题 | 允许 / 拒绝卡片，通知上的按钮也能直接答 |

两种都带 `tool_name: "personad_ask"`、`agent: "script"`、`agent_label: "脚本"`（runtime 的卡片就是这样标「谁在问」，`runtimes.md`）：
通知标题是「脚本 在问你」/「脚本 需要你授权」，卡片不提供「始终允许」，和 Claude 自己的授权卡一眼分得开。
`resumable: true`，id 由 `attempt` 算出：hub 或 node 重启时手机上的卡片不丢，重连的命令挂回同一张（`protocol.md`「交互挺过重启」）。
命令被杀（Ctrl-C）时 node 发 `interaction.cancel`，卡片撤掉。

### 归属

命令用 `node.HookPane` 读调用方所在的窗格（`HERDR_PANE_ID`，其次 herdr-core 的 `HC_TERMINAL_ID`、Orca 的 `ORCA_PANE_KEY`），随请求发给 node。
node 用 hook 学到的「窗格 → 会话」表（`bindPane`）找到那个窗格里的 Claude 会话：

- 找到：卡片 / 通知挂在那个会话下（`session_id`），卡片的副标题是会话标题；点通知打开那个会话；那个会话被静音时 `notify` 不发。
- 找不到（窗格里没跑过 Claude Code，或还没触发过 hook，或不在 herdr 里）：`session_id` 为空，卡片副标题用调用目录的名字，通知副标题是机器名。
  iOS / Mac / 手表照样能答（#4059）：没挂会话的卡片不看会话来源的审批权限（`Interaction.canAnswer`），Agents 的「需要你处理」也列出它（标「脚本」和目录名，和角标同一口径），
  在那里或收件箱里点它直接弹出卡片（没有对话可开）。
- `ask` 另带调用目录（`cwd`）。`notify` 不带路径。

### 偏好和静音

`notify` 走 hub 现有的「发通知」路径（和扩展的 `POST /api/v1/notify` 同一个函数 `deliverNotify`）：设备关掉「需要你」（`prefs.input`）就收不到，会话 / 空间静音就不发（`reason: "muted"`），
通过 APNs（直连或推送中继）和 `ws_notify` 的常驻连接事件（`kind: "alert"`）送达。

- `--error`：`interruption-level: time-sensitive`，标题加「⚠️ 」；默认 `active`。
- `--silent`：`aps` 里不带 `sound`。`notifyRequest` 因此多了一个可选字段 `silent`，HTTP 接口也认。
- 标题最多 120 字、正文最多 1000 字，超出的在 node 截断，不报错。

## 客户端

- 通知：`kind: "alert"` 和 `aps` 已有，无需改动。
- 提问 / 授权卡：形状已有，无需改动。可以做、但不是必须的：卡片上若按 `agent_label` 显示「脚本」，请确认 iOS 的 `Interaction.Agent` 对未知值走 `agent_label`（runtime 卡片已经这样）。
- **没有真机 / 模拟器上看过**：下面「未验证」列出了要看的几点。

## 安全和隐私

- node.sock 是 0600 的 unix socket，能连它的只有同一个系统用户的进程：和 hook、MCP 一样的信任边界。任何这个用户的进程都能用 `ask` / `notify` 在手机上弹东西，所以卡片明确标「脚本」。
- 带选项的 `ask` 是普通的提问卡：助手的 `interaction_answer` 对这类卡片的既有规则同样适用（助手在被允许时可以替用户答，`docs/assistant.md`），回答里 `answered_by` 命令看不到。要确保只有人能答，用不带选项的形式（授权卡助手不能答）。
- 问题、正文和调用目录的名字会进手机的通知（经推送中继时端到端加密，`push-relay.md`）和交互历史，不要放密钥。`notify` 不带路径。

## 验证

- 单元测试：`server/internal/scriptcli`（退出码、参数、终端回退、Ctrl-C、`--strict`）；`server/internal/node/scriptcli_test.go`（假 hub + 假 socket：卡片形状、归属、超时、撤回、重启重连、`notify` 的参数和截断）；
  `server/internal/hub/notify_api_test.go`（`silent`、node 来的 `notify`：静音、偏好、不带扩展字段）。
- 真 `personad` 二进制对着假 node socket 跑过 `ask` / `notify` 的退出码和线上的 JSON（不碰真实 node 和手机）。
- 没有做：对着真实 hub + 手机的端到端（按约定测试时不发真实通知）。

未验证：iOS / Android / 网页上「脚本」卡片的实际显示；`session_id` 为空的卡片在 Agents 页 / 收件箱的位置（hub 侧按 `machine_id + id` 处理，客户端没看过）；
APNs 下 `--silent`（不带 `sound`）在真机上是否静默；通知的送达没有回执。
