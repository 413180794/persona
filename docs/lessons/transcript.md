# 经验：transcript 解析、同步与分页

踩过的坑、怪异行为和当时的取舍（原来在 `CLAUDE.md`「经验」，索引见 `docs/lessons/README.md`）。新经验写进对应的节，没有合适的就新开一节。

- 会话的「最近编辑时间」要用最后一条真实对话（user / assistant / 工具）的时间：Claude Code 会往闲置会话追加 recap、ai-title、
  mode 等行，文件 mtime 会被刷新；找不到 transcript 的会话时间留空，不要用 now 兜底（会一直显示「刚刚」）。
- 排队消息：transcript 的 queue-operation（enqueue / dequeue / remove / popAll）能精确还原队列；TUI 里 ↑ 会把**全部**排队消息拉回输入框，
  ctrl+u 只清当前行（多行要 ctrl+u + backspace 逐行清）。长文本经 herdr bracketed paste 进去会被包成 `<pasted_content id=…>`，显示前要去掉。
- Claude 工作中输入的消息（电脑或手机）会被「中途吸收」，transcript 里存成 `attachment/queued_command`（commandMode=prompt、
  origin.kind=human）而不是 user 消息，解析器要单独处理，否则手机上看不到。
- 新版 Claude Code 把工具调用之间的叙述写进非空的 `thinking` 块（不是 `text`），界面不能把 thinking 一律藏掉。
- 新会话（无头助手或刚启动的 herdr Agent）在 claude 写下第一行之前没有 transcript：手机一订阅 hub 就要 backlog，`session.items`
  对这种「正在启动」的会话（`startingSession`）要返回空列表而不是 not found，watch 等文件出现后先补发已写的行，否则对话页会闪一下报错。
- 「还有没有更早的消息」（`has_more`）只有从最新一端分页的响应才知道：订阅时带 since_seq 的补发（catch-up）以前发 `has_more: false`，
  它要是比 HTTP 第一页先到，App 就以为历史到头了，往上翻停在已加载的最早一条（2026-10-26）。现在 hub 补发时不带这个字段，
  App 把缺失当未知，而且一批没覆盖到本地最早一条的消息不许把它改成 false。复现：`-showScrollToOldest YES` 一路往上翻并记日志。
- 对话页「不跟随」时列表冻结在当时的消息上（`frozen`，往上翻时内容不动），只有底部哨兵的可见性**变化**会把跟随打开：落点（上次读到的位置、
  完成未看的回复开头）在最后一屏时落下来就是最底部，哨兵一直可见、不会再变，之后来的回复全被扣住，也没有「回到最新」（2026-10-31，TestFlight 50/51
  「停在我发的那条消息」）。规则：停在最新端、手指没按着，就要跟随（`landed` / `contentArrived`）；改跟随逻辑跑 `ChatFollowTests`。
- 带很多截图的会话，一页更早的消息有 1–5 MB（图片 base64 内嵌）：分页在后台线程解码，最早的十几行一出现就预取前一页。
- 后台同步别靠「订阅所有会话」：订阅会让 node 每 250ms 刷 transcript、每 400ms 读一次终端（herdr 进程调用）。改成 IM 式的推拉结合：
  hub 只推每个会话的 `rev`（transcript 字节数），App 按 `after_seq` 拉增量（`docs/ios.md` 后台同步）。整个会话列表约 130 KB，
  别为 rev 变化重发列表，用单独的 `revs` 事件。lark-connector 有实时事件 WebSocket（`/agent/v1/events`，`message.upserted` /
  `conversation.upserted`），飞书的推送从这里来。
- 改 transcript 解析或同步协议，用真实语料验证（`docs/archive/lanes/msg-render-verification.md`）：`corpus_test.go` 让新旧解析器逐文件对比，
  `continuity_corpus_test.go` 按 App 的规则模拟丢推送 / 补洞 / 重写。语料只放 `~/jarvis-corpus`；活跃会话的文件会一边读一边长，对比前先拷一份快照。
  模拟器里的 App 看真实会话用本机的测试 hub（假 HOME、不连 herdr、独立数据库），别连线上 hub。
- 大 transcript 冷打开只解析末尾窗口（`transcript/window.go`）。一行解析成什么只通过三样东西依赖前面的行：已显示过的后台任务通知
  （重复的丢掉）、上一个「! cmd」输入、Codex 同一 item 记两次。改解析器时如果新加了「跨行状态」，必须同时让窗口的起点能重建它
  （`seeded` / `seeNotifications`），否则窗口读和整份读会不一样；`TestFixtureWindowedEqualsFull` 和语料的 `TestCorpusWindowed` 会抓到。
- 用户发的图片有两条路：普通消息里是 user 的 image 块；Claude 工作中发的消息被「中途吸收」成 `attachment/queued_command`，图片在 prompt 的块数组里
  （2026-10-29 以前解析器只取 text 块，气泡里看不到图）。App 里有后台任务唤醒过的轮（`pickAnswerAcrossWakes`）要把用户的图片和消息一起留在原处，别当成工作过程。
- 长会话首屏慢不在 hub / node（冷读 0.12 s），在页太大：一个 Codex 长会话最后两轮 1412 条、1.08 MB，回复只有 6 条，其余是界面上本来就折叠的工具和思考。
  App 请求带 `fold_work`，从末尾往前的页把每段工作换成一条 `kind: fold`（只带计数和范围），展开工作行时按 `from_seq` / `through_seq` 再取
  （`docs/protocol.md`「折叠的工作」）。`after_seq` 补发和实时推送不折叠；还在跑的 tool_use、另一半不在页里的 tool_result 不折叠，否则配不上对。
- 后台任务通知（`<task-notification>`）的 summary / event / result 里 Claude Code 自己写了 `&gt;` `&lt;` `&amp;`，解析时要解码；没写 description 的后台命令
  summary 是整条命令（heredoc 几十行），别把 summary 当 notice 文字：node 解析成 `Item.task`，App 画成工具行样式的一行（`TaskNoteRow`），
  Monitor 事件只带 task-id，要靠 `Monitor started (task <id>` 的 tool_result 对回调用（`TaskIndex`）。
- App 的会话磁盘缓存按 item id 合并，hub 换了某类 item 的内容（比如给 notice 加字段），缓存里的旧 item 不会被新页覆盖：改 item 结构就把
  `ChatCache` 的目录版本号加一（`chats-v3`）。
- 往前翻页有解析预算，但前面还有内容时不能回空页：空页 + `has_more: true` 让 App 没有 seq 可以接着取，更早的历史就再也看不到（大 Codex rollout 两轮之间夹着几十 MB 不出条目的行）。
- `<jarvis-context>` 的解析不能假定属性齐全、顺序固定（2026-11-03，#876）：iOS 曾只认 `kind` `title` `url` 三个都在的，飞书 /agent 的上下文没有 `url`，标签没被认出，气泡里留下整段提示词和大片空白。
  分页截断用户消息也要避开标签：从中间截断会留下没闭合的 `<jarvis-context`，各端都会把半截标签当正文显示（`transcript/contexts.go` 的 `lightContexts` 只削减各块的正文）。

## 尾部窗口里可能一句话都没有（2026-11-04，#1163）

node 列离线会话只读 transcript 最后 384 KB（`quickTail`）取标题和最后一句。Claude Code 现在把 `api-request-blob` 整行写进 transcript，一行 300–500 KB，
一行就能占满尾部窗口；最后一轮要是停在工具调用中间（thinking → tool_use → tool_result），读到的尾部就没有任何文字。`scanOffline` 以前把
「没有标题也没有最后一句」一律当成 hook 探针跳过，连 kept 的主对话也跳了：主对话从所有列表里消失，App 和网页找不到会话，输入框整个点不动。
现在只跳过整个文件都读过（不大于尾部窗口）、确实没有输出的小文件；主对话不论尾部是什么都列。按尾部内容做「过滤」时，先想清楚尾部可能被一行大数据占满。

同一个窗口还让在线会话丢了最后一句（#1583）：Agent 说完一句接着调几十次工具，工具结果、钩子回执（`async_hook_response`）和请求行很快超过 384 KB，
`last_text` 变成空的，任务卡片和主对话快照显示了 7 分钟「还没有动静」。现在 `readQuickMetaSince` 在窗口里没有文字时沿用上一次读到的，
没有上一次（node 刚起来）就往前一段段找（`lastWordsBefore`，最多 8 MB）。凡是「最后一个 X」这类只在尾部找的值，都要想到它可能已经在窗口外面。
- **Claude Code 的 transcript 是棵树**（`uuid` / `parentUuid`）：回退（`/rewind`）、↑ 编辑重发会把下一条消息写成较早那条的兄弟，被回退的几轮留在文件里。读法见
  `transcript/branch.go`（同一个 parent 下两条用户自己的消息、较早那条有过 assistant 的回复 → 较早那条的整棵子树不出 item）和 `docs/protocol.md`「回退到某条消息」；
  **按 Esc 取消一条还没回复的消息再发，写出来的也是同一对兄弟**（#3036，实测：两条同样的话挂在同一行「[Request interrupted by user]」下，前一条没有任何回复），
  它不是回退：旧版本只看「两条兄弟」就把它当回退，`history` 多出 `.r1`、所有客户端丢掉本地内容重取，被打断的那一轮也从对话里消失。
  判断回退要看较早那条有没有被回复过；Persona 自己的回退不靠这个猜（`File.Rewound` 由 node 标出）。
  不要沿 parentUuid 从末尾往回走（并行工具结果是正常分叉，`compact_boundary` 的 parent 是 null）。放弃一个分支只改 `history` 令牌（`.r<n>`），
  item 结构没变，所以 App 的 `ChatCache` 目录版本号不用加；客户端本来就在令牌变时丢掉本地内容重取。验证用真实语料：
  `go test -c ./internal/transcript` 旧版和新版各出一个，`-test.run 'TestCorpus$' -corpus <目录> -out <文件>` 逐文件比 item 条数，再跑 `TestCorpusWindowed`。
