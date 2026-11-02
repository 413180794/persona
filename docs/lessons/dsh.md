# 经验：DSH 与 runtime 接入

接 DSH（`docs/dsh.md`）和 node 的 runtime 接入（`server/internal/node/runtimes.go`、`integrations/runtime-sdk`，规范 `docs/runtimes.md`）时踩过的坑。

- **DSH 的授权 / 提问是一条应答链，不是广播**（`approval/request`、`user-questions/request` 是 Cordis 瀑布，按注册顺序走，谁不调 `next()` 就停在谁那）。
  浏览器那一环只有请求自己的 `signal` 结束才撤卡片，插件没法替它撤：所以手机和 Web GUI 不能「同时弹、先答先得」，插件只能先给手机、
  手机交回电脑或过期再 `next()`。插件要 `prepend: true` 才排得到浏览器前面（浏览器是连上来时才注册监听的）。
- **`user/message` 不全是人说的**：只有 `source.kind = user` 是用户的输入；`time-context`、`skill-catalog`、`runtime-context`、
  `agent-instructions` 是 DSH 每轮注入的上下文，`agent-message` / `subagent-settled` 是子 Agent 的消息。`agent/inbox/spliced` 里排队的消息
  进这一轮时会再记一条 `user/message`，两处都显示就重复了。
- **DSH 的 seq 从 0 开始**，Jarvis 的 seq 0 表示「什么都还没有」（`since_seq` / `before_seq` 的零值）：映射成 DSH seq + 1。
- **DSH 的会话日志是一串独立的 zstd 帧**（每批写入一帧），`zlib.zstdDecompressSync` 整个文件只解出第一帧，看起来像「日志只有一行」。
  手工看日志要按帧头魔数 `28 b5 2f fd` 切开逐帧解。插件不读日志，走 `sessionController.page`。
- **没有 herdr 的机器以前从不推会话列表**：`pollAgents` 在宿主（herdr）没响应时直接 `continue`，runtime 的会话（和助手以外的一切）只在连上时列一次。
  现在 herdr 失败照样建列表、推变化。
- **runtime 要在 `runtime.hello` 返回之前就记下这条连接**（SDK 已经这样做）：node 一回完 hello 就可能调过来（`session.watch`），回调里推的东西要有地方发。
- **在 Agent 的 shell 里起的 `dsh web` 用不了 DSH 自己的沙箱**（macOS 不允许 sandbox-exec 套 sandbox-exec），bash 工具全报
  `SANDBOX_UNAVAILABLE`；端到端脚本给测试实例配了一个「不沙箱但要授权」的预设（`sandbox: danger-full-access, approval: ask`）。
- **假模型会被标题生成吃掉**：DSH 的 web profile 第一条消息后会用同一个 provider 起标题。用 `llm-mock-server` 脚本化一轮对话时，
  带 `tools` 的请求（Agent 自己的）和不带的（标题这类旁路调用）要分开回答，否则脚本里的工具调用被标题请求消费掉、顺序全乱。
- **`blocked` 会让 hub 造终端卡**（2026-11-01 合 master 的 Codex 来源时发现）：hub 对 `blocked`、又没有对应卡片的会话会去读屏幕，
  造一张「终端等你处理」的卡。DSH 的问题在它自己的网页里等人时会话就是 `blocked`，`session.screen` 回空屏，手机上于是多出一张空白的终端卡。
  现在没有终端能力（`capabilities.terminal` 为假）的来源一律不造（`hub.syncScreenInteractions`）。harness 当 hub 的端到端测不出这种 hub 侧的问题，
  加 hub 行为时要在 hub 的测试里补一条（`hub/runtime_test.go`）。
- **没标 `agent` 的卡会被画成 Claude 的**：iOS / Android / 网页对没有 `agent` 的授权卡都给「本会话总是允许」，DSH 只能当「允许一次」，按了也会再问。
  runtime 的卡由 node 标上 `agent` / `agent_label`，App 只在 Claude 的卡或带 `always_label` 的卡上给「总是允许」。
