# 经验：Claude Code、Codex 与 herdr

踩过的坑、怪异行为和当时的取舍（原来在 `CLAUDE.md`「经验」，索引见 `docs/lessons/README.md`）。新经验写进对应的节，没有合适的就新开一节。

- herdr 里新开 tab 启动 claude 时，若 cwd 未被信任会先弹「trust this folder」，`agent prompt` 发的文字会被当成选择 → 选了 No 退出。
  node 启动会话后要检查 blocked 屏幕（`docs/claude-integration.md`）。
- Claude 输入框里的灰字（预测的下一句、新会话的 `Try "…"`）在纯文本读屏里和用户打的字一模一样：判断有没有草稿要用 ansi 读屏看 dim（SGR 2）。
  2.1.312 的信任目录对话框选项不带编号（`❯ No, exit`），认对话框靠底部的 `Enter to confirm · Esc to cancel`（`server/internal/screen`）。
- AskUserQuestion：PreToolUse hook 返回 `permissionDecision: allow` + `updatedInput.answers` 即可代答（2.1.310 实测）。
- ExitPlanMode：走 PermissionRequest（不走 PreToolUse）。`allow` 必须带 `updatedInput` + `updatedPermissions:[{type:setMode,...}]`
  才生效，单纯 `{behavior:allow}` 仍会弹 TUI 对话框；`deny` + `message` 会把反馈作为 tool_result 交给模型。
- `--settings <file>` 里的 hooks 只在启动时读取，改 matcher 要重启 claude；hook 脚本本身每次执行都重新读。
- `herdr agent wait` 可能在 herdr 识别出 agent 之前就返回 agent_not_found：启动后要轮询 `agent get`。
- 没装 herdr claude integration 的机器，`agent list` 不带 session id（见 `docs/claude-integration.md` 的兜底）。
- 没装 herdr claude 集成的机器认窗格对应哪个会话，用 `herdr pane process-info` 拿 claude 的 pid，再读
  `~/.claude/sessions/<pid>.json` 的 sessionId；别靠「同目录最新的 transcript」猜（同一个仓库里开六个 Agent，tab 名全串了）。
- herdr 不认识的 Agent（TRAEX）：herdr 只报它内置识别的 Agent，`agent prompt` 对其他窗格报 not found。node 用 owner 文件（pid → 会话）
  加 pid 环境变量里的 `HERDR_PANE_ID`（pid → 窗格）自己认，状态看标题转圈字符和屏幕（`docs/codex.md` TRAEX 一节）。
- Claude Code 的配置目录不一定是 `~/.claude`：启动器设了 `CLAUDE_CONFIG_DIR`（多套 profile）时 transcript、`sessions/<pid>.json`、`settings.json`
  都在那个目录。node 每 30 秒扫一次正在跑的 claude 进程的环境变量（Linux 读 `/proc`，macOS 一次 `ps -E`），所有目录一起找，
  找到的目录记进数据库；平时不跑的 profile 写进 `hub.json` 的 `machines[].claude_config_dirs`（`server/internal/node/claudedirs.go`）。
  以前靠把 `~/.claude/projects`、`sessions` 软链到 profile 绕过，软链留着也没事（按真实路径去重），不用再加。
  漏掉目录的症状：助手续聊报「Session ID … is already in use」、App 里 transcript not found、herdr 里的 Agent 认不出会话。
- 多个配置目录全被 node 看到后（PR #12），一台跑十几个 Agent、transcript 上百 MB 的机器上 node 持续读盘约 20 MB/s：每个 herdr 事件都重建会话列表，
  每次都把每个在增长的 transcript 尾部 384 KB 重读一遍。现在列表重建每秒最多一次，增长中的文件 meta 最多 3 秒重读一次（`metaMinInterval`），
  后台任务扫描流式读（原来第一次整份读进内存），而且大文件的第一次读放到后台逐个做：建会话列表曾等它读完几 GB，node 重启后那台机器好几分钟一个会话都不显示。冷打开几百 MB 的 transcript 要整份解析（317 MB 约 20 秒，hub 15/30 秒超时 502），
  node 启动后会逐个预热正在跑的会话；量延迟要用真实的大会话按 App 冷打开的参数（`items?turns=2`）测 P50 / P99。
- Claude Code 默认清理 30 天前的 transcript（`cleanupPeriodDays`，启动时做，2026-10-29 一次删掉了这台机器 200 多个会话）：
  想留作解析样本的会话要及时脱敏存成 fixture（`server/internal/transcript/testdata/README.md`）。脱敏只能白名单式，
  「短数字原样保留」这种放行会把 IP 的每一段都放过去（踩过）。
- 新开的 herdr 标签页名字（2026-10-31 用户要求）：以前往已有空间里加的标签页一律写死叫 `jarvis`。现在新对话表单可填「标签名」，
  不填就由 hub 按第一条消息起短英文名（`hub/tabname.go`）。gpt-6.1-terra 这类推理模型先花隐藏的思考 token：`max_tokens` 给 20 时回的 content 是空的
  （`stop_reason: max_tokens`），给 200 才有名字；小任务里给模型的 token 上限别卡太紧。
- herdr 会把早就结束的 Codex 会话报成 working（2026-10-30：两天前结束的一轮显示「正在工作 45 小时」八分钟，rollout 没动过；App 的计时从那一轮的用户消息算起）。
  node 以 rollout 为准：结尾是 task_complete / turn_aborted、30 秒没写过，就当 done（`codexTurnOver`）。
- 同一个 Claude 会话 id 可能在两个配置目录里各有一个文件（在另一个 profile 下 resume 过：旧的停住，新的接着写）。按目录顺序取第一个会读到旧的：
  在跑的会话在手机上停在两天前、子 Agent 报 not found、列表里同一个会话出现两次（2026-10-31，一台机器 45 个）。按 id 找文件一律取最近修改的那份（`newestFirst`）。
- Codex 窗格认会话别靠「同目录最新的 rollout」（和 Claude 那条一样的坑）：终端标题是「线程名 | 目录」，按 `session_index.jsonl` 的线程名认；
  rollout 在线程开始那天的目录里一直写，按日期目录只扫近两周会漏掉老线程（2026-10-31 一个正在跑的 Codex 显示成别的会话、灰点）。
  按 id 对拍内容测不出这种错，要拿窗格的真值比（`scripts/live_panes_check.py`）。
  Claude 窗格同理：Jarvis 启动过的会话的窗格绑定存在 hub 数据库里、node 重启就载回来，可能早已过时；`sessions/<pid>.json` 优先于绑定。
  改了认窗格的逻辑，部署（重启）后要再跑一次复核，重启前后的结果可能不一样。
- herdr `agent.prompt` 先粘贴、约 300 ms 后才回车：同一会话的两次发送要串行，否则第二次读屏会把第一条当成输入框里的草稿。发送去重放在 hub 的 Postgres（`sent_messages`）：
  node 的内存去重在重连 / hub 重启后就没了，而 App 恰恰在这时候自动重发。会话读取和发送的完整验证、每一跳的耗时见 `docs/lanes/session-verify.md`。
- Claude Code 2.1.206 的 `/effort` 换成了滑块（「←/→ to adjust · Enter to confirm」，最左是 low、max 右边还有 ultracode），不再有旧列表的「for this session only」；
  node 的 `tuiSetEffort` 两种都认（`effortPicker`）。滑块确认后会写进 `~/.claude/settings.json` 的 `effortLevel`（「saved as your default for new sessions」），
  不只是这一个会话。环境里有 `CLAUDE_CODE_EFFORT_LEVEL`（settings.json 的 `env` 或启动器）时它优先，Claude 只回「… overrides this session」、什么都不改，App 会如实提示。
- 带图的消息有一次停在了 Claude 的输入框里没发出去（2026-10-31，Claude Code 2.1.206：框里是 `[Image #1]` 和文字，Agent `done`，在电脑上按回车才发出），
  之后在单独的 herdr 会话里试了 42 次（小 PNG、1.8 MB JPEG、CPU 跑满）都没再出现，原因没找到。node 的兜底（`ensureSubmitted`）：发完 1.5 秒后
  输入框里还是刚发的那段（`[Image #n]` / `[Pasted text #n]` 去掉后比开头几个字）就补一次回车；框空了、或者是别的内容，什么都不按。
- Codex 生图（`scripts/persona/make_persona.py generate`）：`codex exec` 的内置 `image_gen` 用 Codex 自己的 ChatGPT 登录就能出透明背景的 PNG，
  但它的 HTTP 客户端不认 `socks5h://` 代理（对话请求认），要给 HTTP CONNECT 代理；`-i` 吃多个值，提示词从 stdin 给；ChatGPT 登录只能用部分模型。
  生图卡住（一张十几分钟、并行全停）先量代理隧道的吞吐，别先怪限流：2026-10-31 是 hub 机器外网只剩 11 KB/s，换一台外网快的机器开 `ssh -D` 后四个形象 6 分钟画完（`docs/persona.md`）。
