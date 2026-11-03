# 经验：Claude Code、Codex 与 herdr

踩过的坑、怪异行为和当时的取舍（原来在 `CLAUDE.md`「经验」，索引见 `docs/lessons/README.md`）。新经验写进对应的节，没有合适的就新开一节。

- herdr 里新开 tab 启动 claude 时，若 cwd 未被信任会先弹「trust this folder」，`agent prompt` 发的文字会被当成选择 → 选了 No 退出。
  node 启动会话后要检查 blocked 屏幕（`docs/claude-integration.md`）。
- Claude 输入框里的灰字（预测的下一句、新会话的 `Try "…"`）在纯文本读屏里和用户打的字一模一样：判断有没有草稿要用 ansi 读屏看 dim（SGR 2）。
  2.1.312 的信任目录对话框选项不带编号（`❯ No, exit`），认对话框靠底部的 `Enter to confirm · Esc to cancel`（`server/internal/screen`）。
- Claude Code 的 `--settings` 只认最后一个，不合并（2026-11-02 实测：`--settings A --settings B` 时 A 的 `env` 全没了）。启动器 alias 自己带
  `--settings`、凭据在那个文件的 `env` 里时，Persona 再追加一个 `--settings` 就把它顶掉，从手机起的会话和助手每条消息都回
  `Not logged in · Please run /login`（transcript 里是 `isApiErrorMessage` + `authentication_failed`），在 herdr 里自己敲同一个 alias 却正常。
  node 现在把两份合成一个文件再传（`docs/launchers.md`「~/.zshrc 里怎么认」）。
- 用户自己起的 claude（启动器、profile 目录）没有 Persona 的 hooks，AskUserQuestion 只能靠读屏幕的卡片答。读屏卡得认得 2.1.316 的多选：`[ ]`/`[✔]` 是状态不是文字，数字只勾不交，要挪到 Submit 行按 enter 再在确认页按 1；光标在自由回答行上按数字会写进去，在 Submit 行上按数字没反应；几个键一次发会丢，一次一个（`docs/claude-integration.md`「多选提问的屏幕卡」）。
- AskUserQuestion：PreToolUse hook 返回 `permissionDecision: allow` + `updatedInput.answers` 即可代答（2.1.310 实测）。
- ExitPlanMode：走 PermissionRequest（不走 PreToolUse）。`allow` 必须带 `updatedInput` + `updatedPermissions:[{type:setMode,...}]`
  才生效，单纯 `{behavior:allow}` 仍会弹 TUI 对话框；`deny` + `message` 会把反馈作为 tool_result 交给模型。
- `herdr` 中批准计划后，`PermissionRequest` hook 不一定及时退出，`interaction.cancel` 也可能没有到 hub（2026-11-01：transcript 已有成功的 `ExitPlanMode` tool_result，会话 working，手机仍停在 needs_you）。hub 对待处理计划在 sessions / revs 变化时读有限的 transcript 分页，按对应 tool_use_id 的 tool_result 收尾；旧 hook 没传 id 时，只接受创建前 30 秒内唯一的 ExitPlanMode 调用，且结果不能早于卡片创建。匹配不明、节点离线、只有「working」或模型文字都不清除。30 秒定时兜底覆盖丢事件，单卡限频和单飞避免重复读盘。
  收尾只移除过期卡片、广播 `interaction_resolved`，历史记 dismissed，不伪造手机批准、权限模式或回答；还在等的 hook 仅收到 Dismiss，不返回 allow。新计划、提问 / 工具批准不受旧计划的结果影响。回归见 `hub/plan_sync_test.go`（遗漏 tool_use_id、跨页、本地拒绝 / 中断、伪批准文字、新旧卡片竞态、App 推送和持久化）。
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
  反过来，自动化脚本用自己的 `CLAUDE_CONFIG_DIR` 跑 claude 时，扫描会把它找出来、hub 记住，列表被上百条离线会话淹没；删 `claude_dirs/<机器>` 的记录没用
  （下一次扫描又报上来），要用「屏蔽配置目录」（`docs/claude-integration.md`，#676）。
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
  按 id 对拍内容测不出这种错，要拿窗格的真值比（`scripts/diag/live_panes_check.py`）。
  Claude 窗格同理：Persona 启动过的会话的窗格绑定存在 hub 数据库里、node 重启就载回来，可能早已过时；`sessions/<pid>.json` 优先于绑定。
  改了认窗格的逻辑，部署（重启）后要再跑一次复核，重启前后的结果可能不一样。
- herdr `agent.prompt` 先粘贴、约 300 ms 后才回车：同一会话的两次发送要串行，否则第二次读屏会把第一条当成输入框里的草稿。发送去重放在 hub 的 Postgres（`sent_messages`）：
  node 的内存去重在重连 / hub 重启后就没了，而 App 恰恰在这时候自动重发。会话读取和发送的完整验证、每一跳的耗时见 `docs/archive/lanes/session-verify.md`。
- `/model <别名>` 和 `/effort` 一样会写进 `~/.claude/settings.json`（`model` / `effortLevel`）当以后新会话的默认，不只是这一个会话；
  对话已经被缓存时它先弹「Switch model?」确认（换模型要重读整段历史）。测试时别在自己常用的配置里试，试了要把这两个键改回去。
- Claude Code 2.1.206 的 `/effort` 换成了滑块（「←/→ to adjust · Enter to confirm」，最左是 low、max 右边还有 ultracode），不再有旧列表的「for this session only」；
  node 的 `tuiSetEffort` 两种都认（`effortPicker`）。滑块确认后会写进 `~/.claude/settings.json` 的 `effortLevel`（「saved as your default for new sessions」），
  不只是这一个会话。环境里有 `CLAUDE_CODE_EFFORT_LEVEL`（settings.json 的 `env` 或启动器）时它优先，Claude 只回「… overrides this session」、什么都不改，App 会如实提示。
- 带图的消息有一次停在了 Claude 的输入框里没发出去（2026-10-31，Claude Code 2.1.206：框里是 `[Image #1]` 和文字，Agent `done`，在电脑上按回车才发出），
  之后在单独的 herdr 会话里试了 42 次（小 PNG、1.8 MB JPEG、CPU 跑满）都没再出现，原因没找到；11-01 用手机拍的照片又出现一次。node 的兜底（`ensureSubmitted`）：发完 2 秒后
  看一次输入框，还是刚发的那段（`[Image #n]` / `[Pasted text #n]` 去掉后比开头几个字）就补一次回车；框空了、或者是别的内容，什么都不按。
  只看等完以后的那一次：第一版发完马上就读、一看不对就放弃，11-01 那次就是这样漏掉的（刚粘贴完的一瞬间框里可能还不是这段）。
- Codex app-server 接入（2026-11-02）：Unix socket 中是 WebSocket，不是 JSON-lines；外部 stdio 没有第二 attach 口。目录发现不 resume，控制只回原 loaded owner；未知/取消请求不发 error/decline，否则也可能消费桌面的审批 callback。`serverRequest/resolved` 撤卡记 dismissed，不生成批准；代际和带类型 request ID 一起去重。发送 admission 后的通用 server error 也不能当「确定未发送」自动重试，旧 outbox 的操作标识不按一天过期（`docs/codex-app-server.md`）。
- `codex app-server` 的协议以导出的为准（`codex app-server generate-ts --out <目录>`），实验接口（`thread/backgroundTerminals/*`、`process/*`）
  要加 `--experimental` 才导出来，用的时候 `initialize` 要带 `capabilities.experimentalApi: true`。Codex 助手的其他坑见 `assistant.md`。
  Codex 的 assistant rollout 开头是几十 KB 的指令（AGENTS.md、快照），第一条用户消息在 70 KB 之后：只扫开头 32 KB 找标题会找不到（`readQuickMeta`）。
- Codex 生图（`scripts/assets/make_persona.py generate`）：`codex exec` 的内置 `image_gen` 用 Codex 自己的 ChatGPT 登录就能出透明背景的 PNG，
  但它的 HTTP 客户端不认 `socks5h://` 代理（对话请求认），要给 HTTP CONNECT 代理；`-i` 吃多个值，提示词从 stdin 给；ChatGPT 登录只能用部分模型。
  生图卡住（一张十几分钟、并行全停）先量代理隧道的吞吐，别先怪限流：2026-10-31 是 hub 机器外网只剩 11 KB/s，换一台外网快的机器开 `ssh -D` 后四个形象 6 分钟画完（`docs/persona.md`）。
- Orca 宿主（`docs/plan-orca-support.md`「Orca 来源」）：`terminal.send` 的 `client` 必须带 `id`，只给 `type` 时每次发送都回 `invalid_argument`
  （第一版就是这样，假 runtime 没校验参数所以测试全过；现在 `orcarttest` 按 Orca 的 schema 校验）。Orca 认不出前台的 Agent 时会把「提示」原样写进
  壳层再回车，`accepted:false` 只在带 `requireAgentStatus` 时才有：发之前自己用进程表 + `terminal.agentStatus` 确认 Agent 在前台；
  Orca 的 `agentPrompt` 只给见过在工作的 Agent 粘贴，新开的 claude 收到原样文字，多行会在第一个换行处提交：Persona 自己发括号粘贴再单独回车；
  `worktree.ps` 行上的 `state` 把子 Agent 合进去了（主 Agent 这轮结束、后台子 Agent 在跑时是 working），窗格自己的状态看 `mainAgent.state`；
  `agentIdentity` 在 Agent 退出后可能还在，「是不是 Agent」以 `ORCA_PANE_KEY` 对得上的 claude / codex 进程为准。node 的测试包把 `ORCA_USER_DATA_PATH`
  指到空目录，别让测试碰到正在用的 Orca（它的 socket 能完全控制 Orca）。
- 启动器（`docs/launchers.md`，2026-11-01）：以前只认 `_claude_launchers` 这个辅助函数的输出，`~/.zshrc` 里全是 alias 的机器（一个用户 41 个）
  列表里只剩 `claude`。alias 交给 zsh 自己拆词（`${(@Q)${(z)value}}`），不在 Go 里解析引号。`zsh -c` 里同一段脚本定义的 alias 不会在这段
  脚本里展开（整段先解析），测试要像 node 一样 `zsh -ic` 并把定义写进临时的 `$ZDOTDIR/.zshrc`。`~/.zshrc` 里有 `alias claude=…` 时，
  探测脚本 `claude() { … }` 这种定义会先被 alias 展开、整段语法错误，桩函数一律写成 `function claude { … }`。
- 跑启动器或探测脚本的测试给最小环境（`HOME`、`ZDOTDIR`、`PATH`），失败信息不要整段打印输出：探测输出里是完整的进程环境，开发机上
  有各种真实的 Key。

- Codex 的提问有两种（2026-11-02 读源码 + 实测 0.153.4 / 0.163.0）：Default 模式下现在的模型拿到的是 `request_user_input_async`，调用马上回
  `{"accepted":true}`、回合照常结束，答案以后作为一条带 `<send_user_message_question_reply>` 的用户消息回来——「有 call 没有 output」认不出它，
  要看 `item_completed` 里 `delivery: "async"` 的 AgentMessage；0.163 的 TUI 在回合结束时就把提问面板清掉了，题只剩一段文字。Plan 模式才是阻塞的
  `request_user_input`（覆盖层，output 在答完之后）。覆盖层上按「None of the above」的数字会直接把这句当答案交上去，写自己的话要光标移过去再 Tab。
- herdr 对 Codex：有没答的 async 提问时报 blocked，`agent prompt` 回 `agent_blocked`；`agent prompt` 会把文字接在输入框里的草稿后面一起发出去
  （以前 Codex 的发消息不查草稿）。往上翻过的窗格 `pane read --source visible` 读到的是翻上去的内容，`--source recent` 才是输出末尾；
  按键不受滚动影响。0.153 的 Ctrl+T pager 会盖住之后来的审批（pager 里 `esc` 是编辑上一条消息，关它用 `q`）；0.163 的 Ctrl+T 只是展开细节。
  无头 herdr 分屏不会让窗格变窄，测小窗口用 `stty cols <n>` 再起 Codex。`pane.send_text` 原样转发，括号粘贴（`\x1b[200~…\x1b[201~`）在 Codex 里有效。
- 在本机测 Codex 时信任一个目录会写进 `~/.codex/config.toml`（`codex exec` 也会写），`-c projects.…trust_level` 压不住信任对话框；测完删掉自己加的。
- 0.163 的 app-server control socket（守护进程的 `<CODEX_HOME>/app-server-control/app-server-control.sock`，`--listen unix://<路径>` 也一样）
  是个符号链接，真正的 socket 在 `<规范化的 /tmp>/codex-daemon-<uid>/<别名规范路径的 SHA-256>`。按「Lstat 是 socket、不是符号链接」去认，
  守护进程永远发现不了也连不上（2026-11-02 之前就是这样）；只跟随 Codex 自己算得出的那个目标（`codexSocketPath`）。0.153 是普通 socket 文件。
  守护进程挂了，挂在上面的 TUI 显示「Reconnecting to server…」，新的起来后自己重连并重新加载 thread。npm 装的 `codex` 是个 node 包装脚本，
  会再起一个原生进程：测试里停 app-server 要杀整个进程组。
- `request_user_input_async` 的 `function_call` 不等于问了：参数解析失败（模型把 `title` 写成 `question`）时 output 是错误、模型马上重试，
  只有被接受的调用才写 `delivery: "async"` 的 AgentMessage。#214 按 `function_call` 开卡，留下过一张永远答不掉的卡，3 轮实测第 1 轮才暴露（2026-11-02）。
- 按目录认 Codex 的 rollout 时，`session_meta` 的 `cwd` 可能是解析过符号链接的路径（Codex 用 shell 的工作目录），也可能是 `-C` 原样传的；
  两种都要认。macOS 的临时目录 `/var/…` 其实是 `/private/var/…`：只认解析后的，在 Linux 上测都过、到 Mac 上就 60 秒超时（2026-11-02 从手机新建 Codex）。
- 往 herdr 窗格里打命令起 Codex 时，别把 alias 展开后的词打进去：alias 里可能带 Key，会上屏、进 shell 历史。启动器的词和第一条消息一起写进 0600 文件，
  窗格里只打 `jarvisd codex-start <文件>`（`codex_start.go`）。
- **开在 Claude 桌面版（或别的终端）里的 Claude 会话**（2026-11-01，没装 herdr 的 Mac）：Claude 桌面版跑的是真的 `claude` 进程，也写
  `~/.claude/projects` 的 transcript 和 `sessions/<pid>.json`（`entrypoint: "claude-desktop"`）。Persona 没有终端宿主时看不到它在跑，按 transcript
  列成「未运行」，从手机发消息就去 herdr 里 `claude --resume`：装了 herdr 会起第二个进程同时写同一个会话，没装就报
  「herdr is not running on this machine: herdr workspace list: exec: "herdr"…」。现在恢复前先看有没有活着的 claude 在 `sessions/<pid>.json` 里开着这个会话，
  有就拒绝（`open_elsewhere`，说是在哪开着）；引擎起不来时拒绝的话也换成人话（`engine_missing`）。列表里仍显示「未运行」，还没有「在别处开着」的状态。
- Codex / TRAEX 的子 Agent 会在主对话旁边写自己的 rollout，同一个目录、同一个 cwd，而且更新；TRAEX 执行工具前的自动批准评估（guardian）
  由同一个进程写，还有自己的 owner 文件。按「最新的 rollout / 最新的 owner 文件」认会话，App 显示的是审核对话，和终端里看到的不一样（2026-11-01）。
  认会话一律看 `session_meta` 的 `thread_source` / `source.subagent`，子 Agent 用 `session_id` 换成它的主对话（`docs/codex.md`）。
- **经典 herdr 和 Herdr App 是两套不互通的运行时，可能同时活在一台机器上**（2026-11-03）：Herdr App 从别的电脑 remote 连过来时，
  本机跑的是 `herdr-core serve`（socket 在 `$XDG_RUNTIME_DIR/herdr-core/<session>.sock`，状态在 `~/.local/state/herdr-core/`），
  旁边还有 `herdr remote-client-bridge` 进程和 `peers.json`；终端标签在 `doc.get("herdr-app")` 里。它和终端里的 `herdr server`
  （`~/.config/herdr/herdr.sock`）互不可见。「App remote 连到本机开的会话找不到」时先看 herdr-core 的 `terminal.list`，别只查经典 socket。
  一台机器两套都用时引擎选 `herdr-both`：node 并行刷两个宿主、事件按宿主隔离（`watcherSignals`），新建 / 恢复会话固定走经典 herdr
  （`docs/engines.md`「并列」、`docs/plan-orca-support.md`「并列引擎」）。别再退回「刷新顺序问所有宿主」——herdr-core 慢 2 秒会拖慢 herdr。
