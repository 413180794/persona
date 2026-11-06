# Siri 深度集成（问 Persona、个人 Agent、控制中心）

在 `docs/intents.md`（问 Claude / 状态 / 批准第一个 / 打开会话）之上，加了助手和个人 Agent 的 Siri 入口。所有 intent 都在后台直连 hub：
每次现读 `Keychain.serverConfig`（配对时存的 hub 地址 + 设备 token），App 不用在前台、也不用 WebSocket。

## Intent

| Intent | 标题 | 作用 | 运行模式 |
| --- | --- | --- | --- |
| `AskJarvisIntent` | 问 Persona | 把问题发进助手的主对话（`GET /api/v1/assistant` 的 `main`；没有主对话时 `POST /api/v1/assistant/conversations` 带 `main:true` 新开），轮询 `items?after_seq=` 最多约 24 秒，等会话不再 working 后把 assistant 的文本读出来，并显示一张卡片（问题 + 全文）。答得慢就说「已发给 Persona，它还在想」 | 后台 |
| `TalkToPersonaIntent` | 跟 Persona 聊聊 | 已连 CarPlay 时开始双工语音对话（`docs/carplay.md`）；没连时说明「先连上 CarPlay」，不开始 | 后台 |
| `AgentTaskIntent` | 让 Agent 去做 | `POST /api/v1/agents/sessions/{id}/messages`，`clientId` 每次新 UUID（hub 去重 + outbox）。会话用 `PersonalAgentEntity` 选；不选时用列表里第一个 `canSend` 的 | 后台 |
| `AgentsStatusIntent` | 个人 Agent 未读 | `GET /api/v1/agents/sessions`：几个会话共几条未读，列前三个；没有未读就报最近的三个 | 后台 |
| `AddTodoIntent` | 新建待办 | `POST /api/v1/todos`（`{"title", "notes"?}`，见 `docs/todos.md`），建到 App 当前选中的 hub 组（`hub.group`，和待办页一致；没有这组时用第一组）。读出「已记下待办：「…」」，返回值是标题，快捷指令里可以接着用；旧 hub 没有待办接口（404）时说先更新 hub | 后台 |
| `OpenPersonalAgentIntent` | 打开个人 Agent | Spotlight 里点个人 Agent 会话时打开 App 的 Agents tab | 前台 |
| `OpenJarvisAssistantIntent` / `OpenJarvisAgentsIntent` | 打开 Persona 助手 / Agents | 控制中心、锁屏、操作按钮用的控件背后的 intent | 前台 |

`PersonalAgentEntity` 是 `IndexedEntity`：Agents 页每次加载完就 `CSSearchableIndex.default().indexAppEntities(...)`，
并调用 `JarvisShortcuts.updateAppShortcutParameters()`，于是 Spotlight 能搜到会话，带会话名的 Siri 短语也能匹配。

## 短语（`JarvisShortcuts`，装好就能用，不用手动设置）

- 问 Persona：「问 Persona」「用 Persona 问一下」「Ask Persona」「Ask Persona a question」→ Siri 再问「要问 Persona 什么？」
- 让 Agent 去做：「用 Persona 让 Agent 去做」「让 Persona 的 <会话名> 去做」「Persona 交给 Agent」「Have Persona agent do something」「Send a task to <会话名> with Persona」
- 个人 Agent 未读：「Persona 个人 Agent 未读」「Persona Agent 有什么新消息」「Check Persona agents」
- 新建待办：「用 Persona 新建待办」「Persona 记个待办」「Persona 加个待办」「Add a to-do in Persona」「New Persona to-do」→ Siri 再问「要记什么待办？」；说明只在快捷指令 App 里填
- 原有的问 Claude / 状态各加了一条英文短语。

`String` 参数不能出现在短语里（见 `docs/intents.md`），所以问题 / 任务内容都是说完短语后 Siri 追问。

## 操作按钮、控制中心

- 操作按钮（Action Button）：设置 › 操作按钮 › 快捷指令，直接选「问 Persona」「让 Agent 去做」或「新建待办」（App Shortcuts 自动出现）。
- 控制中心 / 锁屏：`JarvisWidgets` 扩展里加了两个 `ControlWidget`（`AskJarvisControl`、`JarvisAgentsControl`），打开 App 的助手页 / Agents 页。
  控件的 intent 放在 `LiveActivity/PersonaControlIntents.swift`（App 和扩展都编译，`perform` 只在 App 里改 `AppModel`）。

## 没做 / 推迟

- `@AppIntent(schema: .assistant...)` 这类 Apple Intelligence 的 assistant schema：没有贴合「问一个私有助手」的 schema（现有的是 mail / photos / books 等领域），没接。
- 点 Spotlight 里的个人 Agent 只切到 Agents tab，不直接推到那段会话（ChatsView 的导航栈还没有外部入口）。
- 问 Persona 的回答有 24 秒上限：长任务读不出全文，去 App 主对话看。
- 车里的长对话见 `docs/carplay.md`（Siri 只当入口，双工语音）。
- 控件只能打开 App；在控件里直接发问需要 App Intents 的文本输入，控件不支持。

## 在手机上验证

1. TestFlight / Xcode 装上后先打开一次 App（配好对），再进一次 Agents › 个人 Agent，让会话被索引。
2. 对 Siri 说「问 Persona」，说一个问题：应读出回答，卡片显示全文；App 里主对话多了这一轮。
3. 说「用 Persona 让 Agent 去做」，选会话、说任务：应回「已交给「…」」；飞书网关里那个会话收到消息。同一任务不会重复（clientId）。
4. 说「Persona Agent 有什么新消息」。
5. 说「用 Persona 新建待办」，说一个标题：应回「已记下待办：「…」」，App 的待办页多了这一条。
6. 快捷指令 App › Persona 能看到这些动作；操作按钮里能选；控制中心 › 添加控件 › Persona 有两个控件。
7. Spotlight 搜个人 Agent 会话名能搜到。

单元测试：`PersonaTests/SiriIntentsTests.swift`（回答提取、轮询结束判断、读出文案、会话选择、未读文案）、`PersonaTests/IntentsTests.swift`（新建待办的文案、旧 hub、读的 hub 组）。
编译后 `Persona.app/Metadata.appintents/extract.actionsdata` 里能看到新的 action 和 autoShortcut。
