# 计划：支持 Antigravity（issue #281）

状态：第 0 步调研，**等维护者定**（见文末「需要维护者定的点」）。第 1、2 步还没人认领，定下来以后再动代码。
定了以后这份计划改成实施顺序，做完把设计部分并进新的 Antigravity 文档（docs/antigravity.md）和 `docs/assistant.md`，删掉这份。

## 结论先说

- **Antigravity 有两样东西**：桌面 App（Antigravity 2.0 / IDE，会话在 `~/.gemini/antigravity`）和 **Antigravity CLI（`agy`）**。
  能让外部程序接入的口子都在 CLI 上，这份计划只谈 CLI；桌面 App 没有公开的本地接口，只有 Google 自己的 Remote Control 网页能远程用它 [D7]。
- **`agy` 有 TUI，是 herdr 里的一种 Agent，不是 runtime**。herdr 0.9.5 已经内置了 `agy` 的识别规则（工作中 / 授权框 / 提问框 / 信任目录框）和一个只报会话 id 的集成 hook [L3]。
  但「从手机新建、离线后接着、助手」这些没有终端的场景，`agy acp`（1.5.0 起的 ACP 服务）几乎是一个现成的 runtime 后端：流式、续聊、中断、插话、授权请求、提问表单、图片都有 [D4][C1]。
  所以建议**两条路并用**，结构照 pi（`docs/pi.md`）：herdr 窗格里的 `agy` 归 herdr，没有终端的会话归一个伴随进程 runtime，它背后跑 `agy acp`。
- **无头调用选 `agy acp`，不选 `agy -p`**。`-p --output-format stream-json` 能常驻、能流式、能续聊，但没有中断和插话的控制消息、输入只收文字、要人批准的调用一律「软拒绝」不会问出来 [D3]，
  手机审批只能靠 hook 绕。ACP 这几样都有。
- **最大的风险是账号条款**，不是技术：官方 FAQ 写明个人计划下「把 `agy` 包进第三方应用」不受支持，用第三方工具访问 Antigravity 违反条款、可能封号；
  第三方接入建议用 Gemini Enterprise 或 AI Studio 的 API key [D1][F1]。Persona 就是第三方应用。要不要接个人账号，需要维护者定。

## 调研怎么做的

- 本机：`agy` 1.4.10（macOS arm64），**只读**看了 `--help` 和各子命令的 help、`~/.gemini/antigravity-cli/` 的目录结构、SQLite 表结构、`transcript.jsonl` 的字段名
  （统计字段和工具名，没有读对话正文）、二进制里的字符串（RPC 方法名、环境变量名）。没有登录、没有起 agy 会话、没有跑 `agy models`（要联网带凭据）、没有读 token 文件。
- 本机这版没有 `acp` 子命令（`agy acp --help` 打出的是主帮助），ACP 是 1.5.0 加的 [C1]。所以 ACP 那一列全部来自官方文档，**没有实测**。
- 外部资料：antigravity.google 的官方文档、GitHub 上的 CHANGELOG 和 issue、Google AI 开发者论坛。出处编号在文末。

## 1. 能接入的口子

| 口子 | 是什么 | 能拿来做什么 | 出处 |
| --- | --- | --- | --- |
| 交互 TUI（`agy`） | 终端里的全屏界面 | 放进 herdr 窗格；herdr 认得它的状态和对话框 | [L1][L3] |
| 无头（`agy -p`） | 一次一个提示词；`--input-format stream-json` 时一个进程里多轮 | 流式文字、工具调用事件；续聊用 `--conversation <id>` | [D3][L1] |
| ACP（`agy acp`，≥ 1.5.0） | stdio 上的 JSON-RPC，Agent Client Protocol v1，给 Zed / JetBrains / Xcode 这类编辑器用 | 会话的新建 / 载入 / 续上 / 列出 / 关闭、流式、插话、取消、授权请求、提问表单、图片、模型和思考强度、`default` / `accept-edits` / `plan` 三种模式 | [D4][D5][C1] |
| hooks | `SessionStart`、`PreToolUse`、`PostToolUse`、`PreInvocation`、`PostInvocation`、`Stop`；命令型，stdin 进 JSON、stdout 出决定 | `PreToolUse` 能 `allow` / `auto_approve` / `deny`（带 `reason`）/ `ask` / `force_ask`，还能 `overwrite` 改参数；`SessionStart` / `PreInvocation` 能注入消息 | [D6] |
| MCP | `~/.gemini/config/mcp_config.json`（全局）、`<工作目录>/.agents/mcp_config.json`（工作目录）、插件里的 | 挂 Persona 的工具（`ask_user` 等）；`timeoutSeconds: -1` 关掉超时 | [D8] |
| 插件 | `agy plugin install`，打包 hooks、MCP、skills、rules、子 Agent | 一次装好 Persona 要的 hook 和 MCP；`agy plugin import claude` 能导入 Claude Code 插件 | [L1][C1] |
| 规则和 skill | `AGENTS.md` / `GEMINI.md` / `.agents/rules/`；`.agents/skills/` | 助手目录现成的 `AGENTS.md` 和 `.agents/skills/` 它都认 | [D10][D11] |
| Remote Control | `agy --remote-control` / `agy remote-control start`：反向隧道到 Google 的网页控制台 | 只给 Google 自己的网页用，没有给第三方的接口；不用 | [D7] |
| 内部 language server | 二进制里有 `LanguageServerService` 的几百个 gRPC 方法（`StartCascade`、`HandleCascadeUserInteraction`、`StreamCascadeReactiveUpdates`…），要 `ANTIGRAVITY_LS_ADDRESS` 和 CSRF token | 私有接口、没文档、跟版本变；论坛里也说直接调后端不受支持。**不用** | [L2][F2] |
| Python SDK（`google-antigravity`） | 自己写 Agent 的库，只认 `GEMINI_API_KEY` 或 Vertex | 跟用户电脑上的 `agy` 会话无关，不适合「Agents」tab；助手可以算一个备选，但要重写一套工具和权限 | [D9] |

## 2. 会话存在哪、长什么样（本机 1.4.10）

都在 `~/.gemini/antigravity-cli/`（herdr 认 `ANTIGRAVITY_CLI_CONFIG_DIR` 改这个目录 [L3]）：

| 路径 | 内容 |
| --- | --- |
| `conversations/<会话 id>.db` | 每个会话一个 SQLite（WAL）。`steps` 表一步一行（`idx`、`step_type`、`status`、protobuf 的 `step_payload` / `metadata` / `permissions`…），另有 `trajectory_meta`、`gen_metadata` 等。payload 是没公开的 protobuf，**不读** [L2] |
| `brain/<会话 id>/.system_generated/logs/transcript.jsonl` | **对话记录，一步一行 JSON**，是要读的那份（hook 的 stdin 里 `transcriptPath` 就指它 [D6]）。旁边的 `transcript_full.jsonl` 是不截断的全文 |
| `brain/<会话 id>/.system_generated/{steps,tasks,messages,terminals}/` | 工具的完整输出（`steps/<序号>/output.txt`）、后台任务日志、子 Agent 消息 |
| `brain/<会话 id>/` 下的 `.md`、`scratch/`、`.tempmediaStorage/` | Agent 产出的计划 / 总结等工件、截图、图片 |
| `cache/conversation_metadata.json` | 每个会话的 `summary`（标题）、`last_modified_time`、`is_internal` |
| `cache/last_conversations.json` | 目录 → 这个目录最近的会话 id（`--continue` 用它） |
| `presence/<会话 id>.lock` | 每个会话一个锁文件，像是「谁开着它」，**含义没验证** |
| `history.jsonl`、`settings.json`、`log/`、`antigravity-oauth-token` | 输入历史、设置、日志、登录凭据（0600，没读） |

`transcript.jsonl` 每行的字段（本机 16 个会话、约 2600 行统计出来的）：`step_index`、`type`、`status`（`DONE` / `RUNNING` / `ERROR`）、`source`（`USER_EXPLICIT` / `MODEL` / `SYSTEM`）、`created_at`、
`content`、`truncated_fields`；`PLANNER_RESPONSE` 另有 `thinking`、`tool_calls[{name, args}]` 和 token 数；`GENERIC` 有 `media[{mime_type, uri}]`、`error`。映射成 Persona 的 item：

| transcript 的 `type` | Persona item |
| --- | --- |
| `USER_INPUT` | `text`（user） |
| `PLANNER_RESPONSE` | `thinking` + `text`（assistant）+ 每个 `tool_calls` 一条 `tool_use` |
| `GENERIC` | 上一个工具调用的 `tool_result`（**没有调用 id**，只能按顺序配对，要实测确认）；有 `media` 的出 `image` |
| `SYSTEM_MESSAGE` / `CHECKPOINT` / `ERROR_MESSAGE` | `notice` 或不显示（系统注入的不出 item，`docs/runtimes.md`「内容」） |

工具名要换成 App 画的那套：`run_command`（参数 `CommandLine`、`Cwd`）→ `Bash`，`view_file` / `read_file` → `Read`，`write_to_file` / `write_file` → `Write`，
`replace_file_content` → `Edit`，`grep_search` → `Grep`，`find_by_name` → `Glob`，`search_web` → `WebSearch`，`read_url_content` → `WebFetch`，`ask_question`（参数 `questions`）→ 提问；
参数名各自转（`appToolName` + 自己的表）。存储格式没有公开说明，CHANGELOG 里改过好几次（压缩、分支、WAL）[C1]，读法要有语料测试守着。

## 3. 会话、消息、工具调用、审批、提问各在哪、怎么答

| 要做的 | herdr 窗格里的 `agy`（TUI） | `agy acp` 跑的会话 | 出处 |
| --- | --- | --- | --- |
| 会话列表、标题 | herdr 报 Agent `agy` 和会话 id（装了 `herdr integration install antigravity-cli` 时）；标题读 `conversation_metadata.json` | ACP 会话列表、自动标题 | [L3][D5] |
| 状态 | herdr 读屏：spinner + `esc to cancel` = 工作中，授权框 / 提问框 / 信任框 = 需要你 | ACP 的 turn 开始结束 | [L3] |
| 对话内容 | 读 `transcript.jsonl` | ACP 的 session update，或同样读 `transcript.jsonl` | [L2][D4] |
| 逐字 | 没有（只有落盘的整步） | ACP 流式 | [D4] |
| 发消息 | 经 herdr 输进窗格（和 Claude 一样） | `session/prompt`；跑着时再发 = 插进这一轮 | [D5] |
| 中断 | herdr 按 Esc | `session/cancel`：结束正在跑的工具调用、放弃等着的授权 | [D5] |
| 工具授权 | `PreToolUse` hook：hook 先问手机，答了回 `auto_approve` / `deny`+`reason`，交回电脑回 `ask`；或者读屏回答授权框 | 授权请求发给客户端（Allow / Deny），runtime 转 `rt.ask` | [D6][D5] |
| 提问（`ask_question`） | 读屏回答（herdr 认得提问框）；hook 的 `overwrite` 能不能代答**没验证** | ACP elicitation 表单 | [L3][D5] |
| 计划审批 | `plan` 模式先列步骤再写代码；TUI 里怎么批**没查清** | 没写明是否走授权请求 | [D2] |
| 新建 / 续上 | herdr 新标签跑 `agy --conversation <id>`（pi 的 `resume_argv` 那条路） | `session/new` / `session/resume`（不重放历史） | [D3][C1] |

同一个会话 id 两边通用：ACP 里建的会话，人回到电脑前 `agy --conversation <id>` 就是同一个会话（存储是同一份）。但**同一个会话被两个进程同时开着会怎样没查清**：
`presence/*.lock` 看起来是锁，要实测它会不会拒绝第二个进程。

## 4. 它是 herdr 的 Agent 还是 runtime

按规范，有终端的是 herdr 的 Agent。`agy` 的主形态是 TUI，herdr 已经认它，所以**窗格里的 `agy` 归 herdr**：终端、按键、读屏、关闭、空间都是 herdr 的，和 Claude、Codex 一样。

缺的是 Claude 那一套 hook 给的东西（会话 id、授权到手机），和 node 读 transcript 的代码。issue 希望「node 侧尽量不加代码」，所以建议照 pi 的结构拆：

```
 App ── hub ── node ─┬─ herdr 宿主：窗格里的 agy（Agent "agy"，herdr 读屏给状态）
                     └─ runtime.sock ◄── Antigravity 伴随进程：runtime `antigravity`
                          · 从磁盘列最近的 agy 会话、从 transcript.jsonl 给内容
                          · 在 herdr 窗格里跑着的，行上带 pane_id，node 合成一行（同 pi）
                          · 不在跑的：从手机发消息时要么 resume_argv 在 herdr 里开 `agy --conversation`，要么在自己的 `agy acp` 里接着跑
                          · 从手机新建、助手：在 `agy acp` 里跑（没有终端的会话，source_kind: runtime）
                          · 授权：窗格里的走 Persona 的 PreToolUse hook → 伴随进程 → rt.ask；acp 里的走 ACP 授权请求 → rt.ask
```

和 pi 不一样的地方：pi 的扩展跑在 pi 进程里，能直接给它发消息；`agy` 没有进程内扩展（插件只有 hooks / MCP / skills / rules），
所以窗格里的 `agy` 发消息只能经 herdr 往输入框里输（herdr 有 `agent.prompt`），这一段要 node 帮忙。伴随进程报的行上只要带 pane，node 现有的「runtime 行 + herdr 窗格合并」能不能直接用、要不要改，第 1 步开工前要读 `node/pi.go` 和 `runtimes.go` 确认。

## 5. 无头调用对照 Codex 的能力表

对照 `docs/assistant.md`「Codex」那张表。✅ = 文档写明有；❓ = 文档没写或没实测；❌ = 文档写明没有。

| 能力 | `agy -p --input-format stream-json` | `agy acp` | 出处 |
| --- | --- | --- | --- |
| 常驻 | ✅ 一个进程多轮，关 stdin 结束 | ✅ 编辑器的长连接；`--process-per-session` 每个会话一个子进程 | [D3][D5] |
| 流式输出 | ✅ `step_update` 的 `text_delta` | ✅ | [D3][D4] |
| 续聊 / 重启后 | ✅ `--conversation <id>` | ✅ `session/resume`、`session/load` | [D3][C1] |
| 停止 | ❌ 没有控制消息（`control_request` 直接报错退出），只能 SIGINT 杀进程 | ✅ `session/cancel` | [D3][D5] |
| 回复中插话 | ❌ 每条输入是新的一轮 | ✅ 跑着时发的提示词插进这一轮 | [D3][D5] |
| Persona MCP 工具 | ✅ 工作目录的 `.agents/mcp_config.json` 或全局配置；没配过的 MCP 工具默认要批准，加 `mcp(jarvis/*)` 规则放行 | ✅ 同左；`session/new` 带的 `mcpServers` 认不认没写明（只说编辑器配的 HTTP MCP 要 `--process-per-session`）❓ | [D8][D5] |
| `ask_user` 等手机 | ✅ MCP 工具调用默认 180 秒超时，`timeoutSeconds: -1` 关掉 | 同左 | [D8] |
| 它自己的提问 | ❌ 无头时自己定主意，不问 | ✅ elicitation 表单 | [C2][D5] |
| 命令 / 改文件审批到手机 | ❌ 要批准的一律软拒绝（stderr 一行 + `denied_actions`），只能靠 `PreToolUse` hook 去问手机 | ✅ 授权请求给客户端 | [D3][C2][D5] |
| 飞书写操作经闸门 | `PreToolUse` hook 匹配 `run_command`，跑 node 的 `larkCheck`，写操作问手机，拒绝回 `deny` + `reason`（理由交给模型） | 同左（hook 在 ACP 里也生效**没验证**）；或者照 DSH 走 `lark-connector` 工具 | [D6] |
| 权限模式 | `--mode accept-edits / auto / plan`，或默认；`--dangerously-skip-permissions` 全放行 | 只有 `default` / `accept-edits` / `plan`；`auto` 只给个人账号、要沙箱 | [D2][C1] |
| 思考强度 | `--effort low…max`（本机 help 写到 `xhigh` / `max`） | 每个会话可设 | [L1][D5] |
| 图片 | ❌ 输入只收 `text` 块，别的直接报错退出 | ✅ | [D3][D5] |
| 指令文件 | ✅ 工作目录的 `AGENTS.md` / `GEMINI.md`；还会读用户自己的 `~/.gemini/AGENTS.md` 等 | 同左 | [D10] |
| lark-connector skill | ✅ 工作目录的 `.agents/skills/`；没有「额外 skill 目录」的选项 | 同左 | [D11] |
| 记忆（和 Claude / Codex 共用一份） | 照 Codex 的做法写进指令；agy 自己有 memory / knowledge（二进制里有 `GetCascadeMemories`、目录里有 `knowledge/`），怎么关**没查清** | 同左 | [L2] |
| 后台任务 | 1.5.0 起没设 `--print-timeout` 时等后台任务跑完；有 `schedule` 工具 | ❓ | [C1][L2] |
| 预热 | 预先起进程 | 预先起 `agy acp`、`session/new` | — |

hook 默认 30 秒超时，可以按 handler 设 `timeout`，最长多少、超时后算允许还是拒绝，文档没写 [D6]：等手机回答要先实测这一条。
hook 的位置只有工作目录（`.agents/hooks.json`）、全局（`~/.gemini/config/hooks.json`）和插件三处，`--settings` 不带 hooks [D6]：
助手可以只写在助手目录里；窗格里的 `agy` 要授权到手机，就得装到全局或做成插件，会改用户自己的 Antigravity 配置。

## 6. 账号和模型

- **登录方式** [D1][D5]：个人 Google 账号（浏览器 OAuth，SSH 里给链接、把码贴回来，`agy acp --login` 同理）；Gemini Enterprise / Google Cloud（SSO，要开 `aiplatform.googleapis.com`）；
  ADC（`gcloud auth application-default login` + `AGY_ADC_AUTH=true`）；`GEMINI_API_KEY`（只有 CLI 支持，还要在 `settings.json` 里设 `"modelProvider": "gemini"`，只设环境变量没用）。
- **凭据存哪**：文档说存系统钥匙串（macOS Keychain、Linux Secret Service / dbus）[D1]；本机 1.4.10 另有一个 0600 的 `antigravity-oauth-token` 文件，版本不同可能不一样。
  hub 机器如果是没有桌面会话的 Linux，钥匙串可能用不了，API key 或 ADC 更稳（没实测）。
- **跑在 hub 机器上要什么**：装 `agy`（≥ 1.5.0 才有 ACP），登录一次（无头时没登录直接报 `authentication required`，ACP 回 `-32000`）[D3][D5]；
  个人账号的 `auto` 模式要终端沙箱（macOS `sandbox-exec`、Linux 非特权 user namespace，容器里常被禁）[D2]。
- **模型**（2026-11 的文档）[D12]：Gemini 4 Argon、Gemini 3.8 / 3.7 / 3.6 Flash、Gemini 3.1 Pro、Claude Sonnet / Opus 5.5、Claude Sonnet / Opus 4.6、GPT-OSS 120B；
  免费档没有 Argon 和 Claude 5.5，Claude 4.6 和 GPT-OSS 11-18 下线；Gemini 和 Claude / GPT 的额度分开算（每周 + 每 5 小时）。API key 只给 Gemini 模型，Claude 模型在 ADC 下有 [C1]。
  所以 Persona 的「模型配置」对这种启动器只能列 `agy models` 给的，不能填网关地址；`GOOGLE_GEMINI_BASE_URL` 能指到兼容 Gemini 的端点 [D1]。
- **启动器探测**：`docs/launchers.md` 现在只认最后跑 `claude` / `codex` 的 alias；要多认 `agy`。用户的 alias 常带 `--dangerously-skip-permissions` 和代理环境变量，助手要像 Codex 那样把启动器自己的参数和 Persona 的参数拼起来。
- **条款**（见「结论先说」）：FAQ 的原话是 `agy -p`「intended for direct scripting and CI workflows」，把 `agy` 包进第三方 coding agent 或应用在个人计划下不受支持，
  用第三方软件访问 Antigravity 违反服务条款、可能被暂停或终止账号，第三方接入推荐 Gemini Enterprise 或 AI Studio API key [D1]。
  论坛上 10-16 一位以公司口吻回复的人说 FAQ 优先于更早的论坛回复，个人计划下包进第三方应用「No」[F1]；这个账号没有官方标识。
  ACP 本身就是给 Zed、JetBrains 这类第三方编辑器用的，但没有一条官方说法讲 ACP 客户端算不算「第三方应用」[F1][G2]。GitHub 上问同类问题的 issue 还没有官方答复 [G2]。

## 7. 建议的做法（供讨论，定了再拆成第 1、2 步）

1. **runtime `antigravity`**（放在 integrations/antigravity，建在 runtime-sdk 上）：伴随进程，读 `transcript.jsonl` 给列表和内容；没有终端的会话（手机新建、手机接着、助手）在它管的 `agy acp` 里跑，
   授权、提问、逐字、中断、插话都从 ACP 转到 SDK。最低版本 1.5.0，低了就只读不跑，并在列表上说明。
2. **窗格里的 `agy`**：herdr 已经认；node 认 Agent `agy`，和伴随进程的行合成一行（照 pi）；发消息经 herdr；授权到手机靠一个 Persona 的 Antigravity 插件（`PreToolUse` hook 问伴随进程）。
3. **助手**：伴随进程声明 `caps.assistant`，走现成的 `assistant_runtime.go`（DSH 那条路），飞书走 `lark-connector` 工具、`run_command` 里的 `ol` 由 hook 拦下；
   这样 node 里不加按 Antigravity 写的代码。issue 原文写的是「像 Codex 一样单独一个文件」，两种做法的取舍放在下一节。
4. 先做不碰账号条款的部分：存储读法和语料测试、映射、herdr 合并；真的跑 `agy` 的端到端用 API key 或 Enterprise 账号。

## 需要维护者定的点

1. **个人 Google 账号接不接**（最要紧）。选项：a) 只支持 `GEMINI_API_KEY` / Gemini Enterprise / ADC，个人 OAuth 账号的会话只读不驱动；b) 都支持，在文档和启动器上写明封号风险；c) 先问 Google 拿到明确答复再做。
   建议 a：只读本机的对话记录不算「包进第三方应用」，驱动它才算；API key 下只有 Gemini 模型，这个差别写进文档。
2. **形态**：a) herdr Agent + 伴随进程 runtime（上面第 7 节，建议）；b) 只做 runtime，窗格里的 `agy` 只显示成「未运行」的对话记录；c) 只做 herdr Agent，node 里写读 transcript 的 Go 代码。
3. **助手后端**：a) 伴随进程声明 `caps.assistant`、走 `assistant_runtime.go`（不加 Go 代码，飞书走 `lark-connector` 工具，建议）；b) 照 issue 写 `assistant_antigravity.go`，node 直接起 `agy acp`（和 Codex 对称，多一套 Go 代码，飞书闸门要在 ACP 授权和 hook 两处接）。
4. **最低版本**：要求 ≥ 1.5.0（ACP）。低于它时只列会话、只读，还是完全不接。
5. **窗格里授权到手机要不要改用户的全局配置**：a) 发一个 Antigravity 插件，用户自己 `agy plugin install`（建议，可卸）；b) node 自动写 `~/.gemini/config/hooks.json`；c) 不做，只靠读屏回答授权框。
6. **同一个会话两边都开着**：如果 agy 不拒绝第二个进程，手机发到 ACP 的消息和电脑上 TUI 会同时写一份存储。是一律以 herdr 窗格为准、ACP 不碰正在 TUI 里开着的会话（建议），还是别的规则。
7. **Antigravity 桌面 App 不在这个 issue 里**：它的会话在另一个目录、没有公开接口。同意的话 issue 标题和验收里写明「Antigravity CLI」。
8. **agy 自己的记忆**：助手用 Persona 共用的那份时，要不要关掉 agy 自己的 memory / knowledge（怎么关还要查）。

## 第 1 步开工前要实测的

- `agy acp`（1.5.0）的方法名和消息形状：授权请求、elicitation、`session/new` 的 `mcpServers`、取消时等着的授权怎么收尾、计划模式怎么批。
- hook：`timeout` 能设多大、超时算什么；`ask_question` 能不能用 `overwrite` 代答；hooks 在 `agy acp` 里触不触发。
- `presence/*.lock`：同一个会话第二个进程打开时的行为。
- `transcript.jsonl`：`GENERIC` 和工具调用的配对、子 Agent 的记录、压缩和分支后会不会重写（决定 `history` 什么时候换）。
- Linux hub 上的登录：钥匙串不可用时 OAuth 能不能存住。

## 出处

本机（`agy` 1.4.10、herdr 0.9.5，2026-11-05 只读查看）：

- [L1] `agy --help`、`agy mcp add --help`、`agy plugin`、`agy remote-control start --help`。
- [L2] `~/.gemini/antigravity-cli/` 的目录结构、`conversations/*.db` 的表结构、`transcript.jsonl` 的字段和工具名统计；`agy` 二进制里的 gRPC 方法名和环境变量名。
- [L3] herdr 0.9.5 内置的 `agy` 识别规则（`id = "agy"`，aliases `antigravity` / `antigravity-cli`）和 `herdr integration install antigravity-cli` 装的 hook（只报 `conversationId` 和 `transcriptPath`）；`herdr integration status`。

官方文档（antigravity.google）：

- [D1] FAQ：https://antigravity.google/docs/faq/ ；登录：https://antigravity.google/docs/install/
- [D2] 执行模式：https://antigravity.google/docs/cli/modes/
- [D3] 无头模式：https://antigravity.google/docs/cli/headless/
- [D4] ACP（CLI）：https://antigravity.google/docs/cli/acp/
- [D5] ACP（其他编辑器，能力、参数、登录方式）：https://antigravity.google/docs/ide/extensions/acp
- [D6] Hooks：https://antigravity.google/docs/hooks/
- [D7] Remote Control：https://antigravity.google/docs/remote-control?tab=cli
- [D8] MCP：https://antigravity.google/docs/mcp/
- [D9] SDK：https://antigravity.google/docs/sdk/overview/
- [D10] 规则：https://antigravity.google/docs/rules/
- [D11] Skills：https://antigravity.google/docs/skills/
- [D12] 模型：https://antigravity.google/docs/models/ ；CLI 参考：https://antigravity.google/docs/cli/reference/

CHANGELOG、issue、论坛：

- [C1] https://github.com/google-antigravity/antigravity-cli/blob/main/CHANGELOG.md （最新 1.5.0：`agy acp`、ACP 的模式、`session/resume`、`--login`；1.4.11：ADC 下的计费选择；1.3.6：ADC 下有 Claude 5.5）
- [C2] 同上，较早部分（1.1.3 无头软拒绝、1.1.12 无头不再停在提问上、1.1.27 `denied_actions`、1.2.0 无头自动过计划审批）
- [G1] `agy -p` 在非 TTY 下丢输出（1.1.1 已修，提示词作参数时关掉 stdin）：https://github.com/google-antigravity/antigravity-cli/issues/76
- [G2] 官方 agent 能否经 shell / hook 调 `agy -p` 的条款问题（2026-11-02 提，未答）：https://github.com/google-antigravity/antigravity-cli/issues/1392
- [F1] 论坛：个人计划下从本地子进程调官方 `agy`：https://discuss.ai.google.dev/t/clarification-on-consumer-plan-policy-for-invoking-official-agy-via-local-subprocess/189905
- [F2] 论坛：外部编排无头模式（09-15 的回复说支持，被上面 10-16 的回复推翻）：https://discuss.ai.google.dev/t/is-external-orchestration-of-antigravity-cli-headless-mode-supported-with-account-based-usage/183051
