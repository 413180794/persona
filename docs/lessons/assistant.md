# 经验：助手（无头 Claude / Codex）

踩过的坑、怪异行为和当时的取舍（原来在 `CLAUDE.md`「经验」，索引见 `docs/lessons/README.md`）。新经验写进对应的节，没有合适的就新开一节。

- 无头 `claude -p` 里 AskUserQuestion 不可用（不是 hook 没接上，是工具本身被禁用）：要在手机上问用户，给它一个会阻塞等答案的 MCP 工具
  （`ask_user`，`docs/assistant.md`）。模型会把它和别的工具并行调用，有副作用的工具要自己在 hub 里批准。
- 无头助手（`claude -p`）默认权限模式下，连 Jarvis 自己的 MCP 工具（notify_user、schedule_followup）都要在手机上批准：夜里到点的跟进、
  Monitor 事件就卡在批准上（2026-10-30 实测）。助手进程带 `--allowedTools mcp__jarvis`，会替用户出面的工具由 hub 自己再问（`docs/assistant.md`）。
- 助手的后台任务和 Monitor 是 claude 进程的子进程：回收空闲进程、systemd 重启 jarvisd（杀整个 cgroup）都会把它们一起杀掉，对话永远等不到通知。
  node 从 stream-json 的 `system/background_tasks_changed` 知道还有什么在跑，有任务就不回收，任务列表随时落库，重启后告诉对话丢了什么。
- 助手按时做的事只能有一张表（2026-10-31，`docs/assistant.md`「定时任务」）：以前定时任务和跟进分两套，重复的巡检只能一轮接一轮地
  `schedule_followup`，跑起来那一刻就从列表消失，卡在审批上的一个多小时里用户看不到、停不了。重复就做成重复（`every`），
  一轮重排自己时 hub 认出是同一个任务；任务的模型 / 权限模式只让用户在 App 里改，助手不能自己提权。`bypassPermissions` 对无头 claude 只能启动时给。
- 助手调飞书用 lark-connector 自带的 lark-connector skill（副本在 `server/internal/assistantskills/`，编进 jarvisd，起助手时装到它的 `.claude/skills`；
  `scripts/sync_lark-connector_skill.sh` 同步上游）。token 只进助手进程的环境变量；写操作靠 node 的 Bash PreToolUse 闸门（`node/larkgate.go`），
  只给助手用的 `assistant-settings.json` 带这个 hook。闸门读不懂的命令里只要有 `ol POST/PUT/PATCH/DELETE` 也要手机确认，否则 bypass 模式会直接放过。
  在 hub 上起测试实例用独立 HOME / 端口 / 库（`/tmp/jt`），别碰线上 jarvisd；`ssh … 'pkill -f 模式'` 会匹配到 ssh 自己的命令行把会话杀掉，模式写成 `[/]tmp/…`。
  `[x]` 的写法只在同一条 ssh 命令里别处没再出现原文时才管用：2026-11-01 `pkill -f "[d]rive.py x"` 后面还跟着 `python3 drive.py x …`，
  `pgrep -f jarvis_args` 再按进程组杀，都把 ssh 自己杀了（exit 255）。最稳的是按完整路径锚定（`pgrep -f '^/tmp/jt/…/jarvisd'`）拿 pid 再杀，一条 ssh 只做这一件事。
- 助手用 Codex 时是 `codex app-server`（`docs/assistant.md`「Codex」）。踩过的：MCP 工具调用要批准，来的是 `mcpServer/elicitation/request`
  （`_meta.codex_approval_kind: mcp_tool_call`），不是命令审批；MCP 工具默认 60 秒超时（ask_user 要调大 `tool_timeout_sec`）；命令的环境默认去掉
  `*KEY*` / `*TOKEN*` 变量（ol 拿不到 token）；`turn/interrupt` 不停正在跑的命令，要再 `thread/backgroundTerminals/clean`（实验接口，导出协议要
  `generate-ts --experimental` 才看得到）；thread id 由 Codex 定，MCP 进程起来时还不知道会话 id。飞书闸门靠「审批总会来」：别用 `approvalPolicy: never`。
- Codex 助手的记忆和 Claude 共用助手目录的 auto memory（`docs/assistant.md`「Codex」）。Codex 的 `untrusted` 连 `head` 都要批准，
  不单独放行的话每次读记忆都在手机上弹卡片（2026-11-01 实测）；记忆目录在助手目录外，不加进 `writableRoots` 写不进去。
  Codex 自己的 `~/.codex/memories` 是整台机器共用的，别给助手打开。
- 在 hub 机器上用假 HOME 起测试 hub 时，`~/.local/bin/codex` 包装脚本按 `$HOME/.npm-global` 找安装目录，假 HOME 里要软链过去，否则 app-server 一启动就退出。
- node 的 `assistant.send` 带一个它不认识的 session id 时不报错：找不到 transcript 就用这个 id 起一段**空白**的新对话。要「续上，续不上就告诉我」的调用
  （定时任务自己的对话，2026-11-01）带 `resume_only`：node 找不到进程也找不到 transcript 就报错，由 hub 开新对话并记下（`docs/assistant.md`「定时任务」）。
  判断对话忙不忙看 hub 里的会话状态（working / needs_you），只有在 node 列表里的会话才有：要长期续的对话放进 `keptSessions`，否则一台忙机器上 80 个新 transcript 就把它挤出列表。
- **系统提示里不放每次都变的东西**（2026-11-01，`docs/assistant.md`「实时快照和前缀缓存」）：提示词缓存按前缀匹配，系统提示和工具定义在整段对话前面，
  变一个字节，重启后整段历史就要重写（几十万 token 的对话首字要 10 秒到几分钟）。实时快照随每条消息带。但 Claude Code 续聊时其实一直用第一次记下的系统提示
  （压缩后才换），所以以前的快照是「过时」而不是「失效」；重启后读不到缓存的真正原因是**工具列表变了**（实测少一个 MCP 工具整段重写，schema 键的顺序不影响）。
  判断缓存为什么没中，别猜，复制一份真实对话（换会话 id、放临时目录）一项一项改着重启，看 `message_start` 的 `cache_read_input_tokens`。
- 无头 `claude -p` 会自动压缩（窗口用 `CLAUDE_CODE_AUTO_COMPACT_WINDOW` 调），但 Claude Code 的空闲压缩只在交互式会话里跑；stream-json 发一条 `/compact`
  就能手动压。实测续聊进程的第一条消息有时还不压、下一条才压，别拿一次没压当「不支持」。
