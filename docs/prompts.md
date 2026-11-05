# 提示词

personad 写给模型的每一段提示词都在 `server/internal/prompts/`：一个提示词一个文件，Go `text/template` 语法，`//go:embed` 编进二进制。
调用方只准备数据（机器列表、主人称呼、收集到的上下文……），调这个包里带类型的渲染函数拿到文字；Go 代码里不再用 `fmt` / `+` 拼提示词。
`prompts` 不依赖 `hub`、`kb`、`node`，数据结构定义在 `prompts` 里，调用方把自己的类型转过来。

什么算提示词：交给模型的 system / user 文字、写进助手目录的 CLAUDE.md、每条消息开头的快照、发进对话的第一条消息，
以及「把数据放进提示词的那段说明文字」（`<jarvis-context>` 块、「类型：飞书私聊」这类行）。用户的原话、收集到的聊天记录和文档正文是**数据**，不在模板里。
MCP 工具给模型看的描述和参数说明也是提示词，在 `mcp/`（见下面「MCP 工具」）。工具返回给模型的结果文字还在各自的代码里。

## 目录

| 文件 | 是什么 | 谁用 |
| --- | --- | --- |
| `assistant/claude_md.md` | 助手目录 `~/.persona/assistant/CLAUDE.md`：能访问的数据、怎么互动、做事方式；机器列表、关于我、知识库外的笔记目录是可选段 | `assistant/start.go` `claudeMD`（照 hub 给的事实写：`devices.list`、`connectors.signed_in`；知识库目录是助手自己的） |
| `assistant/view_repair.md` | 助手 Stop 首次发现 persona-view 格式错误时的修正反馈：补 fallback、保留已完成操作、不能代替审批 | `hub/assistant_view_check.go`（可信 node 请求；按当前 persona_view 开关） |
| `assistant/snapshot.md`、`snapshot_agents.md` | 实时快照：时间、电脑、Agents、iPhone、知识库重点、飞书未读、今天的会、已安排的跟进（一行一节，node 按节比较） | `assistant/context.go` |
| `assistant/turn_context.md` | 消息开头的 `<persona-context>`：全量，或和上一份比变了的几节 | `node/assistant_context.go` |
| `assistant/view_errors.md` | 接在 `<persona-context>` 后的 `<persona-view-errors>`：前几条回复没通过校验的卡片和原因（已限长、去掉卡片原文），说明不是用户的要求 | `assistant/context_view_errors.go` |
| `assistant/context.md` | 快照外面那层：主对话时开头多一句「这是主对话」 | `hub/machine.go` `assistant.context` |
| `assistant/task_run.md`、`routine_kb_hint.md` | 定时任务 / 跟进到点发给助手的消息（⏰ 行、晚了多久、会自己重复；每天的任务加知识库提示） | `assistant/schedule.go` `taskPrompt` |
| `assistant/task_done.md`、`task_done_input.md` | 从对话派出去的任务停下后判断做没做完（done / not_done / waiting / needs_user / unsure），只有 done 才自动完成待办（`docs/todos.md`「自动完成」）；用判断模型 `inbox.judge` | `assistant/done.go` |
| `assistant/routine_morning.md`、`routine_evening.md` | 默认的早报、晚间回顾任务的内容（存进任务列表后用户可以改） | `defaultRoutines` |
| `assistant/lost_tasks.md` | personad 重启后告诉对话哪些后台任务没了 | `node/assistant.go` |
| `assistant/usage_retry.md` | 网关报了不可能的用量、Claude Code 回「Prompt is too long」，修好 transcript 后续上这一轮（只有 `<persona-context>`，App 不显示，`docs/assistant.md`「网关报的不可能的用量」） | `node/assistant_usage.go` |
| `assistant/main_handoff.md` | 新建主对话后第一条消息前面的 `<jarvis-context>`：旧主对话的 id、最近一次压缩的摘要、之后的几条消息（`docs/assistant.md`「新建主对话」） | `assistant/start.go` `renderHandoff` `startMain` |
| `assistant/codex_memory.md`、`codex_instructions.md` | Codex 跑助手时的 developer instructions：和 Claude 共用的记忆（目录、格式、当前索引；快照随消息带） | `node/assistant_codex.go` |
| `assistant/refused.md` | Codex 的工具调用被拒时塞进当前轮的说明 | `node/assistant_codex.go` |
| `assistant/runtime_instructions.md` | 助手跑在 Agent 运行时（DSH）上时，新对话第一条消息前面的 `<persona-context>`：共用的记忆（目录、格式、当前索引），插件的命令闸门带运行时工具时，说明技能里的命令改用那个工具跑（`{{range .Gates}}`） | `node/assistant_runtime.go` |
| `agents/triage.md`、`triage_input.md` | Agent 一轮停下后的分诊：做完了 / 卡在什么上（等你决定、等另一个 Agent、等外部事件），抽出问题和选项、授权链接、等的事和过多久再看、产物（[agent-work.md](agent-work.md)）；用途 `agent.triage` | `assistant/agents_triage.go` |
| `agents/wake.md` | 到点叫醒在等的 Agent：上一轮说在等什么、好了接着做、推代码 / 部署 / 发消息前先问用户 | `assistant/agents_triage.go` |
| `agents/answer.md` | 用户在分诊开的卡片上的回答，发回给那个 Agent；助手代答的写明是助手按用户的规则答的、为什么、用户可能改答 | `assistant/agents_triage.go` |
| `agents/delegate.md`、`delegate_input.md` | Agent 的提问先由助手判断：内部选项代答（选项和理由），红线（不可逆、社交可见）和目标要变的交给用户，补齐这件事是什么、为什么现在问、推荐、不选的代价（[assistant.md](assistant.md)「提问先由助手代答」）；用途 `agent.delegate` | `assistant/delegate.go` |
| `agents/reanswer.md` | 用户改答助手代答过的提问，新答案发给那个 Agent（带上助手原来答的） | `assistant/fleet_delegate.go` |
| `bot/message.md` | 在飞书里私聊机器人发的一条消息进对话的样子：发给 Persona 时加「回答会发回飞书」的说明，引用的消息、转发的聊天记录、附件路径 | `hub/bots_chat.go` `botSend`（`docs/bots.md`） |
| `inbox/triage.md` | 收件箱分拣（需要你 / 知悉即可 / 噪音）的 system | `assistant/inbox_triage.go` |
| `inbox/tiers.md` | 三档的判断标准，分拣和事件合成共用 | 同上 |
| `inbox/group.md`、`group_input.md` | 事件合成：一个会话的一批新消息各归哪件事，给动到的事写标题、总结、分级；输入里带着这个会话的事和已经调过的工具（`docs/inbox.md`「事件合成」） | `assistant/inbox_group.go` |
| `inbox/item.md` | 一件事给模型看的样子（类型行 + 内容），分拣、代写、判断、AI 分身共用 | 同上 |
| `inbox/draft.md`、`draft_input.md` | 代写回复 | 同上 |
| `inbox/judge.md`、`judge_input.md` | 要不要交给 AI 分身 | `assistant/inbox_agent.go` |
| `inbox/agent.md`、`agent_title.md`、`agent_ref.md` | AI 分身的第一条消息（来源相关的说明、知识库背景） | 同上 |
| `kb/compress.md`、`compress_input.md` | 会话压缩 + 打标 | `kb/compress.go` |
| `kb/project.md`、`project_input.md` | 重写项目档案 | 同上 |
| `kb/items.md`、`items_input.md` | 文档 / 会议 / 日程打标 | `kb/items.go` |
| `kb/recording.md`、`recording_input.md` | 录音转写的摘要、标题和打标（用途 `kb.compress`） | `kb/recordings.go` |
| `kb/known_projects.md` | 「已知项目」列表（名字、别称、一句话），压缩和打标共用 | 同上 |
| `kb/dedupe.md`、`dedupe_input.md`、`dedupe_side.md` | 两个项目是不是同一件事（项目去重） | `kb/dedupe.go` |
| `kb/moments_link.md`、`moments_link_input.md` | 关键词搜到的公司圈帖子里哪些在说这个项目 | `kb/moments_link.go` |
| `kb/digest.md` | 快照里的知识库重点（`Engine.Prompt`） | `kb/items.go` |
| `kb/readme.md` | 知识库目录的 README.md（给读这个目录的 AI 看） | `kb/store.go` |
| `tools/tab_name.md` | 给新 Agent 的 herdr 标签页起名 | `assistant/agents_start.go` |
| `tools/translate*.md` | 翻译短文本 / 一封邮件 | `assistant/inbox_translate.go` |
| `tools/json_only.md` | 所有要 JSON 的请求在 system 末尾加的那句 | `ai/ai.go` `JSONMax` |
| `todo/title.md`、`title_input.md` | 给 `task_plan` / `agent_start` 开的待办起短标题（20 字以内，去掉给 Agent 的指令和来历）；用途 `todo.title`（`docs/todos.md`「待办标题」） | `assistant/todo_titles.go` |
| `todo/decisions.md` | 交给 Agent 时消息前面的 `<jarvis-context>`：这个待办上现在生效的决策（`docs/todos.md`「决策记录」） | `assistant/todo_decisions.go` |
| `tools/launcher_check.md` | 设置 › 模型「测试可用性」发的那一句 | `node/launchercheck.go` |
| `voice/instructions.md` | 内置语音插件的实时模型的 instructions（称呼、说话方式、有没有函数工具、重连时断之前说过的话） | `voice/session.go` |
| `voice/delegated.md`、`report.md`、`handoff.md` | 语音里交给主对话的消息、做完后告诉语音模型的话、交出去时先回的那句 | `voice/session.go`、`delegate.go` |
| `channel/message.md` | 主人在插件渠道（微信等，`docs/channels.md`）里发给机器人的消息进 Persona 对话时的样子：回答发回哪里、什么格式（纯文字或 Markdown）、能不能在那儿答提问，引用的消息、转发的聊天记录、存到电脑上的附件、机器人转不了的东西 | `hub/channels_extension.go` |
| `shared/kb_blocks.md` | 会话档案 + 项目档案的 `<jarvis-context>` 块（飞书 /agent 和 AI 分身共用） | |
| `shared/user_rules.md` | 「用户自己定的规则」段（分拣、判断共用） | |

飞书 `/agent`（会话里 / 文档评论里）的提示词和上下文 #1571 起随 `/agent` 在接飞书的插件里，本仓库没有 `lark/` 目录（`docs/lark-agent.md`）。

Go 那一侧：`prompts.go`（解析、模板函数），`assistant.go`、`inbox.go`、`kb.go`、`tools.go`（每个提示词一个数据结构和一个 `RenderXxx`；飞书 /agent 的 lark.go #1571 起随提示词在插件里）。

## MCP 工具

hub 提供给 Agent / 助手的 MCP 工具，每个一个文件：`mcp/<场景>/<工具名>.md`。文件名就是工具名；内容是描述（一段，给模型看的）、一个空行、
再一个 ```json 代码块，里面是 input schema（参数和每个参数的说明）。这些文件**不是模板**，原样读进来；schema 加载时 `json.Compact`，
格式随便排（短的数组写一行），发出去的字节和排版无关。

| 目录 | 工具 | 谁提供 |
| --- | --- | --- |
| `mcp/assistant/` | `ask_user`、`notify_user`、`jarvis_settings`（设置读写与手机确认） | 助手对话（`assistant/tools.go` 的 `toolNames`） |
| `mcp/schedule/` | `schedule_followup`、`schedule_routine`、`delete_routine`、`list_followups`、`cancel_followup` | 同上 |
| `mcp/agents/` | `agents_list`、`agent_read`、`agent_send`、`agent_start`、`interaction_answer` | 同上 |
| `mcp/inbox/` | `inbox_draft`（AI 分身交草稿） | 同上 |
| `mcp/kb/` | `kb_search`、`kb_list`、`kb_get`、`kb_update` | 同上 |
| `mcp/mail/` | `mail_accounts`、`mail_search`、`mail_read`、`mail_send` | 所有对话（`mail/mcp.go` 的 `Tools`） |
| `mcp/github/` | `github_search`、`github_read`、`github_diff`、`github_checks`（只读）；`github_comment`、`github_review`（写，过闸门） | 助手，GitHub 连接器状态是 `ok` 时（`connector/github/tools.go`；golden `mcp_tools.assistant_with_github`） |
| `mcp/voice/` | `search_knowledge`、`list_agents`、`send_to_agent`、`ask_jarvis` | 内置语音插件开了 `tools` 的上游（`voice/tools.go`，作为 GPT-Live 的函数工具声明在 `session.start` 里） |

#1571 起没有 `mcp/lark/`：`lark_agent_reply`、`lark_chats`、`lark_messages`、`lark_search` 由接飞书的插件提供（`docs/plan-extensions.md`「第 3 步」）。
助手跑在 Agent 运行时上时，`ol` 这类命令的工具也不在这里：名字和描述来自插件命令闸门的 `tool`（`proto.CommandGate.Tool`），
node 的 `gateTools` 经 `runtime.sock` 交给运行时（`node/assistant_runtime.go`）。

工具的**列表和顺序**还在代码里（上面两个函数按名字取 `prompts.Tool(name)`），处理调用的代码也在原处。加一个工具：在对应目录加文件，
把名字加进列表，写处理代码，`UPDATE_GOLDEN=1` 更新 `mcp_tools.*` / `mail_tools` 的 golden。`TestEveryMCPToolIsUsed` 抓没人提供的工具文件。

`phone_*`（通讯录、日历、提醒事项、照片、健康、位置、设备信息、剪贴板、本地通知）**不在服务端**：App 连上 hub 时把自己的工具列表
（名字、描述、schema）发过来，hub 存进 Postgres、原样转给 Agent（`hub/http.go` 的 `setPhoneTools`，`docs/phone-tools.md`）。
描述在 iOS 的 `apple/Persona/PhoneTools/*Tool.swift`（`PhoneToolRegistry.swift` 汇总），Android 的
`android/platform/phonetools/`（`PhoneToolCatalog.kt`），改它们要发 App 新版本。

## 模板的规矩

- 模板名是去掉 `.md` 的路径（`inbox/triage`）；一个提示词引用另一个用 `{{template "shared/user_rules" .}}`。
  同一个 Agent 的几段（主体、可选段、列表）在模板里用 `{{if}}` / `{{range}}` / `{{template}}` 组合，不在 Go 里拼。
- **文件最后一个换行不算**（编辑器总会加）。提示词本身要以换行结尾，就在最后留一个空行。`TestFilesEndWithANewline` 要求每个文件以换行结尾。
- 默认值写在模板里（`{{or .Owner "用户"}}`、没写要求时的那句话），不在调用方。各提示词的称呼默认不一样（「用户」「我」「你」），照原样保留。
- 模板函数（`prompts.go`）：`attr`（XML 属性转义）、`ctx`（放进 `<jarvis-context>` / `<persona-context>` 块里的正文，见下一条）、`join`、`indent`、`blank`（只有空白）、`first`（切片的前 n 个）。
  截断这类整理数据的事（`clip`、`clipRunes`、`oneLine`）在调用方做，传进来的已经是要放进去的值。
- **块里放别人的文字一律经 `ctx`**（#3754）：引用、转发的消息、收件箱条目正文、知识库摘要、快照的各节、记忆索引都可能来自别人（或由别人的话转写），正文里的结束标签会让块提前闭合，后面的话变成用户自己说的。`ctx`（`ContextBody`）先删一遍结束标签，删出来的再把 `<` 写成 `&lt;`，大小写不敏感，斜杠两边和 `>` 前允许空白（读取端都只认字面的 `</jarvis-context>` / `</persona-context>`，这是它们的超集）。
- 包初始化时解析全部模板（`template.Must`，`missingkey=error`）：语法错误启动即崩，测试里先暴露；渲染出错（字段名写错）直接 panic，
  不会把半截提示词交给模型。`TestEveryTemplateIsUsed` 抓没人用的模板。
- 提示词里要写字面的 `{{`：`{{"{{"}}`。
- 写给用户看的输出要求「用中文」的地方换成 `Language` 字段（语言的自称，`English`、`日本語`；调用方用 `assistant.answerLanguage(hello.Locale)`，zh-Hans 传空），
  模板里 `{{if .Language}}用{{.Language}}{{else}}用中文{{end}}`：为空时和原来一字不差，原来的 golden 不变，en / ja 另有 `*.en.golden` / `*.ja.golden`
  （`assistant/claude_md.md`、`inbox/triage.md`、`inbox/group.md`、`inbox/draft.md`、`tools/session_title.md`；`docs/i18n.md`）。
  知识库的提示词（`kb/`）保持中文，见 `docs/i18n.md`「后续工作」。

## 改一个提示词

1. 改 `server/internal/prompts/` 里的文件。
2. 跑金标准测试，看差异是不是想要的：`cd server && go test ./internal/hub ./internal/assistant ./internal/kb ./internal/node -run Golden`。
3. 确认后更新 golden：`UPDATE_GOLDEN=1 go test ./internal/hub ./internal/assistant ./internal/kb ./internal/node -run Golden`，把 `testdata/prompts/*.golden` 的 diff 一起提交。
   `TestGoldenAssistantContextMain` 要 Postgres（`scripts/test/test-server.sh` 里才跑），要更新它就在 hub 机器上带 `UPDATE_GOLDEN=1` 跑
   （`GOCMD='env UPDATE_GOLDEN=1 go test -count=1 -run TestGoldenAssistantContextMain' ./scripts/test/test-server.sh ./internal/hub`），再把那两个文件拷回来。

注意 `assistant/schedule.go` 的 `scheduleHeader` 按 `assistant/task_run.md` 的头几行认出抄回来的旧头部：改那几行的写法要一起改它。

## 加一个提示词

1. 在对应场景的子目录加 `xxx.md`（没有合适的就新开一个子目录，并加进 `prompts.go` 的 `//go:embed`）。
2. 在同场景的 Go 文件里加数据结构和 `RenderXxx`，调用方只填数据。
3. 在调用方的包里加 golden 测试（`*_golden_test.go` 里照现有的写：几组有代表性的输入，可选段有 / 没有、列表空 / 多项），`UPDATE_GOLDEN=1` 生成。
   输入一律用占位数据（`示例用户`、`example.com`），`scripts/privacy_check.py` 会查。

## 金标准测试

MCP 工具的 golden 是 `hub/mcp_tools_golden_test.go`（助手有 / 没有飞书、普通对话三种列表）和 `mail/mcp_golden_test.go`，比的是 `json.Marshal` 发出去的内容（golden 里缩进了方便看，缩进前后一一对应）。

`hub/prompts_golden_test.go`、`assistant/prompts_golden_test.go`、`kb/prompts_golden_test.go`、`node/prompts_golden_test.go` 从调用方那一层测（假的 lark-connector、假的模型记下收到的
system / user、临时目录的知识库），所以数据转换也一起测到。快照里「现在」那行的时钟换成占位后再比。
2026-11-01 搬家前先用原来的代码生成了这些 golden，搬完后同样的输入逐字节一致。
2026-11-04 助手的业务逻辑搬进 `server/internal/assistant`（`docs/assistant.md`「和 hub 的边界」）时，它写的提示词的 golden 跟着搬到 `assistant/testdata/prompts/`，文件原样挪过去，没有重新生成。同一天助手的设置、主对话和开对话也搬过去后，`assistant_claude_md.*`、`main_handoff*` 的 golden 也原样挪到 `assistant/testdata/prompts/`（`assistant/claude_md_test.go`），逐字节一致。同一天收件箱挪到管理者后，`inbox_triage.*`、`inbox_draft.*`、`inbox_judge*`、`inbox_agent.*`、`inbox_group.*` 的 golden 也原样挪到 `assistant/testdata/prompts/`（`assistant/inbox_golden_test.go`），没有重新生成，逐字节一致；翻译跟着挪过去后 `translate_*` 也原样挪到那里（`assistant/inbox_translate_test.go`），同样逐字节一致。
