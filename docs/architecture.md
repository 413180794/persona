# 架构

手机直连 hub：公网走 HTTPS / WSS（TLS + 公钥钉扎），内网 / VPN 保留 HTTP / WS。公网优先 IPv6，VPN 由用户自行配置，见 [网络文档](network.md)。

## 目标

- 手机（和手表）上像聊天 App 一样和 Claude 对话，但 Claude 本体始终是某台机器 herdr 里的一个**交互式 Claude Code**：
  在电脑前 `herdr` 打开就是同一个会话，可以无缝接手；手机只是另一个「终端」。
- 启动时可选启动器（`~/.zshrc` 里的 `cc_ds` / `cc_max` / `ccc` …，本质是选模型和网关）。
- 手机实时看到对话过程：文字、思考、工具调用和结果、当前屏幕。
- 需要人参与的环节（AskUserQuestion、执行计划审批、工具权限、信任目录等 TUI 对话框）都能在手机 / 手表上处理。
- Claude 能用 iPhone 的数据（通讯录、日历、提醒、照片、健康、位置、剪贴板……）和邮箱。

## 组件

```
 手机 / 手表 ─┐  HTTPS/WSS（公网，支持钉扎的客户端）
              ┤  HTTP/WS（内网 / VPN，兼容旧 App）
              ┤ ───────────────────────────►  personad hub（HTTP + 可选 TLS）
              │                                   │  ├─ mail（IMAP/SMTP，凭据加密）
              │  WS: tool.call ◄──────────────────┤  ├─ 交互队列（问题 / 计划 / 权限）
              │      (手机数据工具)                │  └─ 会话索引、事件扇出
                                                  │
                     stdio JSON-RPC over `ssh <host> personad node --stdio`
                     （hub 机器本机直接起子进程）
                                                  ▼
                              personad node（每台机器一个，生命周期跟随 hub 的连接）
                               ├─ herdr socket API（~/.config/herdr/herdr.sock，CLI 兜底）：建 tab、启动 claude、prompt、
                               │   send-keys、read、agent 状态；events.subscribe 推变化
                               ├─ tail ~/.claude/projects/*/<session>.jsonl → 结构化事件
                               ├─ unix socket ~/.persona/node.sock ◄── personad hook（Claude Code hooks）
                               │                                 ◄── personad mcp（给 claude 的 MCP 工具）
                               ├─ unix socket ~/.persona/runtime.sock ◄── runtime：没有终端的 Agent 运行时（DSH 的 Web 插件，docs/runtimes.md）
                               └─ 启动器列表：zsh -ic（`_claude_launchers` + 调用 claude 的 alias）
```

hub 保留 `listen`（默认 HTTP `:18660`），TLS 用 `public.tls_listen` 单独开启，端口由用户选择。两个监听共用 API 和来源鉴权规则。主 token 仅允许回环 / 内网来源，公网使用临时配对 token 或设备 token；`health` 的公网响应不含版本、能力和候选地址。

候选地址来自配置和本机网卡，按 IPv6、域名、内网、公网 IPv4 排序。鉴权后经 `/api/v1/endpoints` 和 WS `hello` 下发；地址变化事件仅发给声明 `endpoints` 能力的连接。网卡上有地址不代表光猫、防火墙已放行，仍需从外部验证。现有 iOS / Watch 不改代码时继续使用原内网 / VPN 地址。

单一二进制 `personad`，四种模式：

| 模式 | 运行位置 | 作用 |
| --- | --- | --- |
| `hub` | 一台常开的机器，systemd --user 常驻 | App 的 API、事件扇出、交互队列、邮件、调度各 node |
| `node --stdio` | 每台机器，由 hub 通过 SSH 拉起 | 本机 herdr / claude / transcript / hook 中转 |
| `hook <event>` | Claude Code hooks 调用 | 把事件交给 node；需要人决定的（提问、计划、权限）阻塞等手机回答 |
| `mcp` | Claude Code MCP（stdio） | 暴露手机数据、邮件等工具，调用经 node → hub → 手机 |

## 为什么这样设计

- **不用 `claude -p --output-format stream-json`**：那是一次性无头模式，电脑上不能接手，也没有 TUI 的交互能力。用户要的是
  「服务端 / Mac 上是 herdr 里的一个 claude」。
- **结构化内容来自 transcript JSONL**：Claude Code 每个内容块（thinking / text / tool_use / tool_result）完成就追加一行，
  tail 它得到可靠的结构化流；**逐字的进行中输出**来自 `herdr pane read`（每 400ms 采样屏幕，仅在有订阅且 agent 在工作时），
  App 里显示为「实时屏幕」条。两者结合 = 实时流式。
- **交互走 hooks 而不是模拟按键**：hook 能拿到结构化的问题 / 计划，回答也是结构化的（实测见 `docs/claude-integration.md`）。
  模拟按键（`herdr agent send-keys`）只作兜底：任何 hook 覆盖不到的 TUI 对话框（信任目录、模型选择……），App 显示屏幕快照 +
  按键面板。
- **hooks 只注入 Persona 启动的会话**（只在这次会话加载的插件 `--plugin-dir ~/.persona/claude-hooks`；更老的 Claude Code 用
  `--settings ~/.persona/claude-settings.json`），不改全局 settings，不影响用户自己开的 claude。用插件是因为 `--plugin-dir` 能叠加，
  而 `--settings` 只认最后一个，会顶掉启动器自己的（网关、Key、模型，`docs/claude-integration.md`「Hooks」）。
  不是 Persona 启动的 herdr claude 也能在 App 里看（按 herdr 报告的 session id 找 transcript）、发消息、按键回答。
- **node 经 SSH stdio**：复用 `~/.ssh/config` 里已有的 `ssh <机器>` 这类别名，不开新端口、不新增认证面；连接断了 hub 自动重连。
  ssh 进不去的电脑（NAT 后面、别的内网）退到 tailcat 打洞（`docs/server.md`「tailcat 打洞」），先 ssh、不行再 tailcat。
- **没有终端的 Agent 是 runtime，不是宿主**：宿主是「终端开在哪」（herdr / Herdr App / Orca，一机一引擎，`docs/engines.md`），node 从窗格和
  transcript 认会话。DSH 这种自己管会话、没有终端的 Agent 运行时，经 `runtime.sock` 的 runtime 协议挂进来：自己报会话、推内容、问手机，
  和这台机器选的引擎并存；hub 和 App 不用改，新的 runtime 也不用改 Persona（`docs/runtimes.md`：协议 + SDK + 验收清单）。
  Paseo 是例外：它没有终端、经 runtime 协议接入（node 自己运行它的桥），却是一台机器可选的引擎，新建和接着会话都交给它（`docs/paseo.md`）。
- **hub 在一台常开的机器上**：它常在线、能连 Gmail（已验证 imap.gmail.com:993 / smtp.gmail.com:465 直连可达），所有 Mac 都经它中转。

## hub 只做沟通中心

hub 只管沟通：App 等客户端 ↔ 管理者、hub ↔ hub（互联）、连接（WebSocket、node / 连接器的链路）、配对和鉴权、管理界面、推送投递、各种中转。
业务都在管理者里（`server/internal/assistant`、`devices`、`extmgr`；见 `docs/machines.md`、`docs/extensions.md`「谁管什么」、`docs/assistant.md`）：
助手、收件箱、待办、额度、看板、扩展、设备的数据，还有调模型（分诊、标题、摘要、判断）。hub 只把这些接口原样转给管理者（`assistantwire.Routes`、`devicewire.Routes`、`extwire.Routes`）。

`server/internal/hub/architecture_guard_test.go` 把 hub 现在的样子钉住，往错的方向长就失败：

- **hub 自己回答的路由** = `testdata/hub_routes_allowed.txt`（在 hub 包里用字面量注册、又不在上面三张转发表里的）。新加的业务接口写在管理者里，
  再把路由加进对应的转发表；确实是 hub 自己的（连接、配对 / 鉴权、管理、推送投递、中转）才手动在清单里加一行。删了路由也要删行（多出来的行同样失败）。
  行尾 `# moving to the manager` 的是正在挪进管理者的（工作区看板；额度窗口已经挪完，`docs/usage-windows.md`「谁管这件事」），挪完连行一起删。
- **依赖**：hub 的依赖里不能有管理者的包（`assistant`、`assistantskills`、`devices`、`extmgr`、`extension/host`、`agents`、`mail`、`office`、`feishubot`、`wechat`、`node`、`mcpserver`）
  和 `connector/` 下的连接器实现；`connector`、`kb` 只能经 wire 包间接带进来，hub 自己不 import。
- **不调模型**：hub 只用 `internal/ai` 的 `ai.Config`（读配置、转交），代码里也不出现模型 API 的地址。
- **Store** 的方法 = `testdata/hub_store_methods.txt`：新的业务表放管理者自己的存储，不往 `hub.Store` 加。

清单都是手改的，没有自动重写的开关：改清单的那一行会出现在 PR 的 diff 里，评审时看它是不是真的属于 hub。

## 关键流程

### 新建会话
1. App：选机器 → 选目录（最近的项目 / 浏览）→ 选启动器 → 可选首条消息。
2. hub → node `session.start`：在 herdr workspace `jarvis`（没有就建）里新建 tab（cwd=目录，label=目录名），
   `pane run "<launcher> --session-id <uuid> --mcp-config ~/.persona/mcp.json --plugin-dir ~/.persona/agent-plugin --plugin-dir ~/.persona/claude-hooks"`
   （更老的 Claude Code：`--settings ~/.persona/claude-settings.json` 代替两个插件）。
3. node 等 agent 就绪（`agent wait`）；若 blocked（信任目录等）就把屏幕作为 `screen` 交互推给手机。就绪后发首条消息。
4. node 开始 tail transcript，事件经 hub 推给订阅的 App。

### 回答提问 / 审批计划
1. claude 调 AskUserQuestion → PreToolUse hook → `personad hook` 连 node.sock 发 `interaction` → hub 存入交互队列并推给 App / 手表（含本地通知）。
2. 用户在手机上选 → hub → node → hook 进程输出 `updatedInput.answers` → claude 继续。
3. 超时（默认 30 分钟）或没有任何 App 在线时 hook 立即放行 → TUI 照常显示，电脑前也能答。

### 手机数据工具
claude 调 MCP 工具 `phone_calendar_events` → `personad mcp` → node.sock → hub → 找到在线的 iPhone（WS）→ App 用 EventKit 查询 → 原路返回。
App 不在线时返回错误「iPhone 不在线，请打开 Persona」。
