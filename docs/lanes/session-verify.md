# 会话读取和发消息的真实数据验证（2026-10-31）

用户：「感觉现在 session 读取逻辑不太对，拿我大量的 session 历史来测试下现在 Claude code 和 codex 读取逻辑对不对，发送消息逻辑对不对」，
补充「还有就是发送消息的链路」。语料是 6 台机器上的真实会话，只在本机 `~/jarvis-corpus/verify`，不进仓库；这里只有数字和编造的复现。

## 一、读取：对拍

工具在仓库外（一次性的）：

- 在 hub 机器上按 App 的方式取一个会话（`turns=2&fold_work=1&lazy_images=1`，再 `before_seq` 往前翻到 `has_more: false`，
  每个折叠按 `from_seq/through_seq` 展开），再用 `after_seq` 从 0 整份取一遍；取完再把原始 jsonl 拷回来（文件只会更长，按 `to_seq` 截）。
- 参照解析器用 Python 另写（不复用 Go 的解析）：Claude 的 user / assistant / attachment(queued_command) / system 行，Codex 两种格式
  （旧的 response_item + event_msg，新的 `item_completed`），逐条比角色、类型、正文（去空白、截断的按前缀）、工具 id 配对、seq、时间戳、顺序；
  另外比 App 式分页（折叠展开后）和 after_seq 整份是否逐条相同、有没有重复 id、`has_more` 是否属实。

| 项 | 数 |
| --- | --- |
| 会话 | 315（Claude 174、子 Agent 73、Codex 68），6 台机器，每台挑最长 / 最新 / 情形最多的 + 随机 |
| 原始数据 | 2.9 GB，61.6 万行，node 给出 23.6 万条 |
| 请求 | App 式分页 1,475 页、展开折叠 9,912 次、after_seq 722 页 |
| 覆盖的情形（含该情形的会话数） | compact 26、排队消息 35、图片 42 + Codex 图片 36、打断 15、本地命令 18、斜杠命令 209、思考 167、后台任务通知 151、粘贴 134、resume / fork 78、子 Agent 73、Codex 中断 21 / 压缩 26 |
| 用户消息、回复、工具配对、顺序、时间戳、seq、App 分页 vs 整份 | 除下表外 **0 处不一致** |

| 不一致 | 数量 | 结论 |
| --- | --- | --- |
| 同一个会话 id 在两个 Claude 配置目录都有文件（在另一个 profile 下 resume 过：旧文件停住，新文件接着写），node 按目录顺序取第一个 | 一台机器 45 个 id（19 个近 7 天在用，36 个取到的是旧的）；3 个子 Agent 502「not found」；会话列表里 5 个 id 出现两次 | **已修**：取最近修改的那份，非 hook 告知的路径 30 秒后重新核对；离线扫描同一个 id 只列一次 |
| 往前翻页：两轮之间有大量不出条目的大行（Codex 压缩后的 history 行），一页解析满 24 MB 预算一条没找到，回空页 + `has_more: true`，App 没有 seq 可以接着取，更早的历史看不到 | 一台机器 76 个大 Codex rollout 里 3 个 | **已修**：前面还有内容时一页至少带一条（按条数分页同理）。真实文件复验：58 / 6 / 23 页翻到头，条数等于整份读 |
| 某台机器（hub 到它的 ssh 只有约 15 KB/s）上 87 KB 的页要 5.6 秒，500 条的 after_seq 页超过 hub 的 30 秒 → 502 | 1 台机器 | 网络环境问题；hub 起 node 的 ssh 加了 `Compression=yes`（transcript JSON 实测快 3.6 倍，局域网几乎无开销） |
| 折叠停在一行中间（这一行后面还有一条文字），展开时按行取会多带回那条 | 4 | App 按 id 合并、不会重复显示，**不改** |
| 工具结果没有对应调用 | 1 | 源文件那一行本来就是坏的（写了一半的 JSON） |
| 工具调用没有结果 | 4 | 会话被杀 / 还在跑，正常 |

有意的转换（核对过，不是错）：工具结果里的图片单独成 `user/image`；Codex FileChange 每个文件一个 Edit、commentary 算思考、
本地图片写成 `<attached_images>`；跨会话消息按发送者显示；后台任务通知去重。标题：列表里的 35 个有 custom-title / ai-title 的会话全部一致。

## 一之二、正在进行中的会话（2026-10-31 补）

上面的对拍按会话 id 取：「这个 id 的内容读得对不对」全对，但从来没问「这个窗格对应的是不是这个 id」「它现在是不是在工作」。
线上 bug：一个正在跑的 Codex（回了一句、跑了 3 条命令、正在 compact），App 里那一轮什么都没有、灰点。原因不在读取：

- herdr 没给这些 Codex 窗格报会话 id，node 按「同目录最新、没被认领的 rollout」猜，同目录 5 个 Codex 认错 4 个；
- 正在跑的那个线程六周前开的，rollout 在六周前那天的目录里，node 只扫近两周的日期目录，根本没把它列进候选；
- 于是这个窗格被认成同目录另一个已经结束的会话：内容是那个会话的，`codexTurnOver` 看它结尾是 task_complete 就报完成。

按它真正的 id 去读，这一轮（用户消息、回复、51 个工具、compact 提示）一条不差。修法见 `docs/codex.md`「识别」。

新加的对拍 `scripts/diag/live_panes_check.py`（在 hub 机器上跑，`pane_probe.py` 到每台机器上取真值）：每个 herdr 窗格的真值——Codex 按标题里的线程名，
Claude 按 herdr 报的 id 或 `sessions/<pid>.json`——和 hub 列表里这个窗格的会话 id 比；Codex 这一轮还开着（最后一个回合事件是 task_started）
且两分钟内写过的，列表里必须是 working。修前：70 个窗格（6 台）里 **9 个认错**，全是同一台机器的 Codex（26 个里 9 个），Claude 全对；4 个没法验证
（标题里没有线程名）。

#110 部署后复核：Codex 全对，但另一台机器冒出 2 个 Claude 窗格认错。Jarvis 启动过的会话的窗格记在 hub 的数据库里，node 重启时载回来；
那两个窗格早换了 claude / resume 了别的会话，旧绑定排在进程的 `sessions/<pid>.json` 前面，于是显示几天前的会话
（重启前内存里的绑定已被 hook 纠正过，所以修前那一轮没查出来）。改成进程优先、绑定只给还没写下 pid 文件的刚启动的 claude。
#111 部署后复核：重启后约一分钟内那两个窗格仍是旧的，之后全对（70 个窗格 0 认错）。原因是 node 刚启动时还不知道各个 claude 的配置目录，
找不到 pid 文件，而「没找到」被缓存了一分钟，这一分钟里又是旧绑定说了算；改成没找到的 5 秒后重查。

## 二、发消息：链路

```
App 输入框 ──PendingMessage(UUID, .sending)，存 outbox──┐
  │ 没连上 / 卡住：.waiting，连上后按顺序补发（同一 id）            │
  ▼                                                                 │
POST /api/v1/sessions/{mid}/{sid}/messages  {text, images, client_msg_id}   超时 100 s
  ▼
hub sendMessage（hub/send.go）
  · client_msg_id 去重：内存里的进行中 + Postgres sent_messages（一天）
  · 发送不随 App 断开取消，90 s 期限
  ▼  JSON-lines RPC over `ssh -C … jarvisd node --stdio`（局域网往返约 5 ms）
node session.send
  · 同一会话一条一条来（typingLocks）
  · 会话没在跑：Claude 用原启动器 --resume 拉起（约 5 s），Codex / TRAEX 报错
  · 读屏（ANSI 60 行，约 10 ms）：对话框 → 409 dialog_open；草稿 → 409 input_draft（刚打进去的自己的字等它被收走）
  · 图片存到 ~/.jarvis/uploads，路径写进 <attached_images>
  ▼  herdr unix socket（每次调用 10 s 期限；socket 不通才走 CLI）
herdr agent.prompt（Claude、Codex）：整段粘贴（bracketed paste）→ 约 300 ms → 回车；TRAEX：send_text + 150 ms + enter
  ▼
Claude Code：空闲时立刻开始一轮；工作中进队列（transcript 记 queue-operation，中途吸收时写 queued_command）
Codex：空闲时立刻写 UserMessage；工作中排在本轮之后
  ▼
node 每 250 ms 读新行 → session.items 推给 hub → WebSocket items → App ChatStore.merge
  · 新到的用户消息按文字对上本地气泡就去掉气泡
```

**耗时**（Claude，经 ssh 的一台 Mac，空闲，几十次的中位数）：POST 0.31 s，其中 hub ↔ node RPC 约 5 ms、读屏约 10 ms、
**herdr `agent.prompt` 固定约 300 ms**（粘贴后等一下再回车，直接对 herdr socket 计时 302 ms）；Claude 在点发送后约 0.17 s 写下这条消息
（回车后立刻写，早于 POST 返回）；WebSocket 推到手机约 0.5 s（node 250 ms 轮询 + 推送）。长文本 1.6 万字 / 12 万字：POST 0.46 / 0.63 s，
推送 0.72 s，Claude 数出的行数正确（没截断、没拆成多条）。离线会话 resume：5.5 s。Codex：同样 0.31 s 送达，用户消息在空闲时立刻写进 rollout，
排在一轮后面时要等那一轮结束（实测 10–70 s，模型慢）。

## 三、发消息：情形

| 情形 | 结果（修前） | 修后 |
| --- | --- | --- |
| 空闲、多行、中文 emoji、图片、斜杠命令、同 id 重发 | 恰好一次，顺序对；图片在 Claude 里变成 `[Image #n]`，App 的匹配已兼容 | — |
| 工作中发（排队） | 恰好一次，Claude 排队后在下一轮处理 | — |
| 等权限确认的对话框时发 | 409 dialog_open，什么都没打 | — |
| 提问卡片（hook 拦着）时发 | Claude 排队，回答后在本轮吸收，恰好一次 | — |
| **连发 3 条（同一会话）** | 第 2、3 条 409 input_draft（第 1 条还在输入框里），App 弹「清空并发送」——会清掉用户自己的第 1 条 | node 串行，第 2、3 条正常送达 |
| 两台设备同时发 | Claude：两条都到（碰巧没撞上）；Codex / TRAEX 没有草稿检查，可能拼成一条 | 串行 |
| **App 里「重连」这台机器** | hub 只关了 RPC，ssh 进程还在、连接循环一直等它退出：机器显示在线，**所有调用都回「rpc: connection closed」**，直到 hub 重启（实测 4 分半，手动结束 ssh 子进程才恢复） | 关掉就重连（测试用假 node 子进程复现） |
| **投递途中 node 断线 / hub 重启** | 消息其实到了电脑上，App 收到普通的 502 → 点重发就打第二遍；node 的去重在内存里，重连后的新进程不知道 | hub 落库去重；回 `delivered: unknown`，App 提示「可能已经送到」；手动重发换新 id |
| 发送中 App 切后台 / 断网 | 请求被取消时 hub 也跟着不等了，结果没人记；App 标 waiting 自动重发，同一 id 但 node 若重连过就重复 | hub 不随请求取消、结果落库 |
| 窗格不存在（会话退出） | Claude resume 后送达；但 resume 时 Claude 停在对话框上会把消息打进对话框 | resume 后也先读屏 |
| 超长消息 | 页面里被截断（带 `full_size`），App 对不上本地气泡，**同一条显示两遍**一分钟 | 截断的按开头比对 |
| 一条更早的同文字消息（「继续」）随历史页加载 | 把还没发出去的「继续」气泡当成已到删掉 | 只认这次新到、时间在气泡之后（留 2 分钟钟差）的消息，一条消息只消一个气泡 |
| 排在长任务后面的消息（Codex 看不到队列） | 「已发送」气泡 60 秒后消失，几十秒后才在历史里出现 | Agent 还在工作就留着（最多 15 分钟） |
| 失败的气泡 | 不存盘，App 重启就没了 | 存盘，一天后清 |
| `/cost` 这类打开面板的斜杠命令 | 面板只在电脑上，对话里什么都没有，之后再发被 dialog_open 拦住 | 未改（见下） |

## 没改的

- 打开 TUI 面板的斜杠命令（`/cost`、`/status`、`/model`）：手机上看不到结果，面板还挡住后面的消息。可以在 node 里认出面板、读出内容回给手机并按 Esc，
  要逐个命令适配，单独做。
- 新机器上 claude 第一次启动停在主题选择：`POST /sessions` 等 60 秒报 502，herdr 里留下一个 tab。
- Codex / TRAEX 的草稿检查（输入框里有字时消息会接在后面）。
