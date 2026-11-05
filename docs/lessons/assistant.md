# 经验：助手（无头 Claude / Codex）

踩过的坑、怪异行为和当时的取舍（原来在 `CLAUDE.md`「经验」，索引见 `docs/lessons/README.md`）。新经验写进对应的节，没有合适的就新开一节。

- 助手卡片的边界校验不能只看常见写法（2026-11-05，#2018）：重复 key 的错误曾被后续 chart／markdown 检查的 nil 覆盖；Markdown 引用图片和引用链接也能绕过只查内联语法的正则。共同检查应先拒绝重复 key，再检查组件；安全子集明确拒绝引用链接定义，补图片、autolink、缺 hostname 的 URL 回归。剥离 HTML 后清理空 section，否则 compact 限制会把已经清空的卡片判成过大。活卡片 version 要限制到 JavaScript 安全整数范围，不能只看 Go 的 int64。

- 单会话查询也要用可信的助手分类（2026-11-05，#2019）：列表会按专用目录和是否有 pane 判断 Assistant，原来的 session.get 却直接返回 node 的原始记录，字段通常没填。富 UI 的工具和 outbox 用它检查身份，会误拒绝真实助手。查询时复用 isAssistant，pane 仍优先判成普通 Agent；测试不能只手动填 Assistant=true，要用真实 hub 查询路径验证 headless／pane／普通目录。正文注册也用同一分类。
- Go 的 JSON 解码会忽略字段名大小写（2026-11-05，#2019）：`Fallback` 能填进 Go 的 Fallback，但 Swift／Kotlin 只读 schema 的 `fallback`。助手原始块在转成 Go 类型前必须确认精确字段名，不能拿类型解码成功当跨端合规。
- HTTP 请求过总线时保留编码路径（2026-11-05，#2019）：正文卡片 key 含 `/`，item id 常带 `#`；客户端正确 URL 编码后，hub 若转发 `r.URL.Path`，助手重建请求就把它们当路径分隔符或 fragment，动作全回 404。用 `EscapedPath()`，并测真实 hub→assistant 中继；只测 Service 的路由会漏掉这层错误。

- 无头 `claude -p` 里 AskUserQuestion 不可用（不是 hook 没接上，是工具本身被禁用）：要在手机上问用户，给它一个会阻塞等答案的 MCP 工具
  （`ask_user`，`docs/assistant.md`）。模型会把它和别的工具并行调用，有副作用的工具要自己在 hub 里批准。
- 无头助手（`claude -p`）默认权限模式下，连 Persona 自己的 MCP 工具（notify_user、schedule_followup）都要在手机上批准：夜里到点的跟进、
  Monitor 事件就卡在批准上（2026-10-30 实测）。助手进程带 `--allowedTools mcp__jarvis`，会替用户出面的工具由 hub 自己再问（`docs/assistant.md`）。
- 助手的后台任务和 Monitor 是 claude 进程的子进程：回收空闲进程、systemd 重启 jarvisd（杀整个 cgroup）都会把它们一起杀掉，对话永远等不到通知。
  node 从 stream-json 的 `system/background_tasks_changed` 知道还有什么在跑，有任务就不回收，任务列表随时落库，重启后告诉对话丢了什么。
- 助手按时做的事只能有一张表（2026-10-31，`docs/assistant.md`「定时任务」）：以前定时任务和跟进分两套，重复的巡检只能一轮接一轮地
  `schedule_followup`，跑起来那一刻就从列表消失，卡在审批上的一个多小时里用户看不到、停不了。重复就做成重复（`every`），
  一轮重排自己时 hub 认出是同一个任务；任务的模型 / 权限模式只让用户在 App 里改，助手不能自己提权。`bypassPermissions` 对无头 claude 只能启动时给。
- 助手调飞书用 lark-connector 自带的 lark-connector skill（副本在 `server/internal/assistantskills/`，编进 jarvisd，起助手时装到它的 `.claude/skills`；
  同步上游的脚本；2026-11-04 起技能在接飞书的插件里）。token 只进助手进程的环境变量（#1317 起连环境也不进了，见下面「凭据别放进 Agent 进程的环境」）；
  写操作靠 node 的 Bash PreToolUse 闸门（`node/larkgate.go`），
  只给助手用的那组 hooks（`assistant-hooks` 插件和 `assistant-settings.json`）带这个 hook。闸门读不懂的命令里只要有 `ol POST/PUT/PATCH/DELETE` 也要手机确认，否则 bypass 模式会直接放过
  （凭据在代理的闸门改成直接拒绝）。
  在 hub 上起测试实例用独立 HOME / 端口 / 库（`/tmp/jt`），别碰线上 jarvisd；`ssh … 'pkill -f 模式'` 会匹配到 ssh 自己的命令行把会话杀掉，模式写成 `[/]tmp/…`。
  `[x]` 的写法只在同一条 ssh 命令里别处没再出现原文时才管用：2026-11-01 `pkill -f "[d]rive.py x"` 后面还跟着 `python3 drive.py x …`，
  `pgrep -f jarvis_args` 再按进程组杀，都把 ssh 自己杀了（exit 255）。最稳的是按完整路径锚定（`pgrep -f '^/tmp/jt/…/jarvisd'`）拿 pid 再杀，一条 ssh 只做这一件事。
- 凭据别放进 Agent 进程的环境，再靠读命令拦（#1317，2026-11-05）：`env`、`printenv lark-connector_AGENT_TOKE""N`、`cat /proc/self/env*`、
  `${!OPEN*}`、`python3 -c 'import os…'` 都绕得过字面黑名单，评审又试出 `nohup env`、`compgen -e`、`ps eww`、`eval` 拼名字、`env -u HOME printenv`……补不完；
  更要紧的是拿到 token 的脚本直接调接口，写操作的手机确认就没了。改成凭据留在 node、本机代理按同一份规则查每个实际请求（`node/gateproxy.go`）。
  代理要挂在常驻 node 的整个进程上（`assistantKeeper`），不能挂在每个连接的 `Node` 上：hub 一重连就换一个 `Node`，留下来的对话还拿着旧地址，
  新 `Node` 放行的调用到不了旧代理，确认过的写也发不出去。
  确认过的调用按「方法 + 路径 + 正文」一次一放，所以闸门读不懂的命令（循环、脚本）里的写只能拒绝、让模型单独一条发；别为了它开「一段时间都放行」的窗口，
  那会把同一个对话里接下来别的命令也放过去。
- 助手用 Codex 时是 `codex app-server`（`docs/assistant.md`「Codex」）。踩过的：MCP 工具调用要批准，来的是 `mcpServer/elicitation/request`
  （`_meta.codex_approval_kind: mcp_tool_call`），不是命令审批；MCP 工具默认 60 秒超时（ask_user 要调大 `tool_timeout_sec`）；命令的环境默认去掉
  `*KEY*` / `*TOKEN*` 变量（ol 拿不到 token）；`turn/interrupt` 不停正在跑的命令，要再 `thread/backgroundTerminals/clean`（实验接口，导出协议要
  `generate-ts --experimental` 才看得到）；thread id 由 Codex 定，MCP 进程起来时还不知道会话 id。飞书闸门靠「审批总会来」：别用 `approvalPolicy: never`。
- Codex 助手的记忆和 Claude 共用助手目录的 auto memory（`docs/assistant.md`「Codex」）。Codex 的 `untrusted` 连 `head` 都要批准，
  不单独放行的话每次读记忆都在手机上弹卡片（2026-11-01 实测）；记忆目录在助手目录外，不加进 `writableRoots` 写不进去。
  Codex 自己的 `~/.codex/memories` 是整台机器共用的，别给助手打开。
- Codex 在 macOS 的沙箱拒绝路径里带软链接的可写目录（`/tmp`、`/var` 这类顶层的除外），每条命令执行前就报 `symlinked writable roots are not supported`（#1751）。
  助手目录走旧名 `~/.jarvis`（指向 `.persona`）、`~/.claude` 放在 dotfiles 里都会撞上；cwd 和记忆目录两个可写目录都要解析成真实路径再交给 Codex（`codexDirs`），只改 cwd 不够。
  报错里的 `allow_symlinked_codex_home` 只放行 CODEX_HOME 下面的，别拿它放宽。验证：用 `command/exec` 和 `thread/fork`（`ephemeral: true`，不碰原会话）带同样的参数跑 `pwd`。
- 在 hub 机器上用假 HOME 起测试 hub 时，`~/.local/bin/codex` 包装脚本按 `$HOME/.npm-global` 找安装目录，假 HOME 里要软链过去，否则 app-server 一启动就退出。
- 假 HOME 的测试 hub 不等于隔离：node 读的是启动器指定的 Claude 配置目录，启动器（`claude-aster` 这类）写死了 `CLAUDE_CONFIG_DIR` 时，测试对话的 transcript 会写进真实的配置目录，而且 node 按 cwd 以 `/.jarvis/assistant` 结尾认助手对话，真实的那些也会出现在测试 hub 的列表里、测试助手能改它们（2026-11-01 测对话管理工具时）。做法：假 HOME 里放一份启动器的拷贝，`CLAUDE_CONFIG_DIR` 改到假 HOME 下（settings.json 拷一份），`_claude_launchers` 也写进假 HOME 的 .bashrc；提示词只让它动自己开的对话；测完删掉整个假 HOME（拷贝里有 key）。
- node 的 `assistant.send` 带一个它不认识的 session id 时不报错：找不到 transcript 就用这个 id 起一段**空白**的新对话。要「续上，续不上就告诉我」的调用
  （定时任务自己的对话，2026-11-01）带 `resume_only`：node 找不到进程也找不到 transcript 就报错，由 hub 开新对话并记下（`docs/assistant.md`「定时任务」）。
  判断对话忙不忙看 hub 里的会话状态（working / needs_you），只有在 node 列表里的会话才有：要长期续的对话放进 `keptSessions`，否则一台忙机器上 80 个新 transcript 就把它挤出列表。
- **系统提示里不放每次都变的东西**（2026-11-01，`docs/assistant.md`「实时快照和前缀缓存」）：提示词缓存按前缀匹配，系统提示和工具定义在整段对话前面，
  变一个字节，重启后整段历史就要重写（几十万 token 的对话首字要 10 秒到几分钟）。实时快照随每条消息带。但 Claude Code 续聊时其实一直用第一次记下的系统提示
  （压缩后才换），所以以前的快照是「过时」而不是「失效」；重启后读不到缓存的真正原因是**工具列表变了**（实测少一个 MCP 工具整段重写，schema 键的顺序不影响）。
  判断缓存为什么没中，别猜，复制一份真实对话（换会话 id、放临时目录）一项一项改着重启，看 `message_start` 的 `cache_read_input_tokens`。
- 无头 `claude -p` 会自动压缩（窗口用 `CLAUDE_CODE_AUTO_COMPACT_WINDOW` 调），但 Claude Code 的空闲压缩只在交互式会话里跑；stream-json 发一条 `/compact`
  就能手动压。实测续聊进程的第一条消息有时还不压、下一条才压，别拿一次没压当「不支持」。
- `ssh_host` 是 hub 那台机器 `~/.ssh/config` 里的别名，只在 hub 本机有效：给跑在别的电脑上的东西（助手的说明）写 `ssh <别名>` 等于给了一条执行不了的命令。
- npm 装的 lark-cli 是 `#!/usr/bin/env node` 的脚本，node 和它都在 nvm 这类目录里，systemd 起的 jarvisd 的 PATH 里没有（2026-11-03，#273）：
  按命令名直接执行会「找不到」，找到了也会因为 `env node` 失败。node 先用登录 shell `command -v` 找到绝对路径，执行时把它所在的目录放到 PATH 最前（`node/larkcligate.go`）；助手 PATH 上的包装同理。
- lark-cli 以用户身份发消息要应用开了 `im:message.send_as_user`，默认的应用没有（2026-11-04，#273）：`auth login --domain all` 拿到两百多个权限也没有它，
  `im +messages-send --as user` 回 `missing_scope`，提示的 `auth login --scope …` 也补不上（应用没开的权限用户授权不了）。机器人身份有 `im:message`，
  所以助手发消息、回复一律 `--as bot`，手机卡片按 `--as` 写身份。这次是手机上点了同意之后才在飞书那边失败的：闸门只能确认「要不要做」，做不做得成要看权限。
- 会话的 `last_text` 是 node 截过的预览（空白全压成一个空格、只留 200 个字，`node/sessions.go` 的 `snippet`），不是能渲染的 Markdown（2026-11-04，#1508）：
  任务完成卡片拿它当摘要，列表、标题挤成一行，`**`、反引号可能被截在中间，网页又按纯文本显示，用户看到一串符号。要原文的地方（完成卡片）
  用 `session.items` 读 transcript 里最后一条回复（`lastTurn`），截断用 `clipMarkdown`（按行、不截在标记中间）；只显示一两行的地方
  （任务进度行、推送）把 Markdown 去掉（hub `plainMarkdown`、网页 `lib/plain-markdown.ts`），别原样显示。
- hub 插进对话的 feed 条目是只追加的，「发出去了」这种之后才有的状态不能只放客户端内存（2026-11-04，#1519）：消息卡点了「发送」、评论也发到 GitHub 了，
  网页刷新后又是草稿和「发送」，差点重发。状态要写回条目本身（body 的 `sent_at`），重新广播同一个 id，客户端按 id 换 body，增量拉取也要带上「之后改过的」条目。
  同一次还发现起草提示词明确要求「查不到的用【】标出来」，点一下就发的草稿把【预计今晚 / 几点前】原样发了出去：一键发送的草稿不能留空让人填。
- MCP 的 `tools/list` 失败时不能回空列表（2026-11-04，#504）：`personad mcp` 在 `initialize` 声明了 `listChanged: false`，Claude Code 整段对话只列一次工具，
  node 一时连不上（重启、重连 hub、启动顺序）时回空列表，助手整段对话就没有任何 Persona 工具，也看不出是连接问题。现在 15 秒内重试，仍不行就回 JSON-RPC 错误（`/mcp` 和 MCP 日志里能看到原因）；
  node 回 `{"tools":[],"error":…}`（它连不上 hub）也算失败，正常的空列表才算没有工具。
- 助手成了自己的进程（2026-11-04，#1446，`docs/assistant.md`「进程」）以后，hub 里凡是「问一下助手」的地方都可能等不到：hub 启动时注册连接器要查助手的工具名，
  那时助手还没连上，每个工具名都等满 10 秒（测试里一个连接器测试 60 秒）。hub 启动、注册这类路径只读助手上次报的（`assistantToolsNow`），只有 node 给助手对话要工具列表时才等它一会儿。
  同理，助手自己的定时任务循环要等第一次握手的回答（在哪台机器、时区）再开始，否则一启动就拿着空的回答跑到点的任务，全失败。
- 总线上 hub 发来的事件在读循环里同步交给回调（`internal/rpc`）：处理慢了整条连接都停。助手把事件放进队列按顺序处理，`session.turn_ended`（要问模型、等任务的 Agent 停下）另起；
  测试里要「助手处理完了」就在发完事件后对它做一次调用（同一条连接，排在事件后面），再等它的队列空（`BusHub.Settle`）。
- hub 默认用 `os.Executable() assistant` 起助手：测试里 `os.Executable()` 是测试二进制，起它等于把整个测试再跑一遍。hub 的测试在 `init` 里把 `assistantCommand` 换掉，要测子进程就让测试二进制在 `TestMain` 里认出环境变量、当助手跑。
- `zsh -ic` 会先展开已有 alias 再执行整段脚本（#1674）：用户给 `codex` 起了同名 alias 时，`codex() { … }` 会在解析阶段失败，同串里先 `unalias` 也来不及。
  包装函数用 `function codex { … }`，其他包装里的调用写成 `\codex`，避免 alias 再次展开造成递归；用临时 `ZDOTDIR` 配真实 zsh 回归，不读取或修改用户的 shell 配置。
- Agent 自己的「本会话」授权只活在它的进程里（2026-11-05，#2202）：Claude 的 `addRules … destination: session`、Codex 的 `acceptForSession`、
  飞书闸门的 node 内存，主对话进程一换（空闲回收、部署、换启动器）、node 一重启就没了，`--resume` 不带回来，用户看到的是「点了总是允许还一直问」。
  助手对话里要跨进程的状态都得落到 hub 的库：授权卡带 `always_keys`，hub 记进 `assistant_always` 并自己回答同样的卡（`docs/assistant.md`「本会话总是允许」）。
