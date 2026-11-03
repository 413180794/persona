# 经验：DSH 与 runtime 接入

接 DSH（`docs/dsh.md`）和 node 的 runtime 接入（`server/internal/node/runtimes.go`、`integrations/runtime-sdk`，规范 `docs/runtimes.md`）时踩过的坑。

- **DSH 的授权 / 提问是一条应答链，不是广播**（`approval/request`、`user-questions/request` 是 Cordis 瀑布，按注册顺序走，谁不调 `next()` 就停在谁那）。
  浏览器那一环只有请求自己的 `signal` 结束才撤卡片，插件没法替它撤：所以手机和 Web GUI 不能「同时弹、先答先得」，插件只能先给手机、
  手机交回电脑或过期再 `next()`。插件要 `prepend: true` 才排得到浏览器前面（浏览器是连上来时才注册监听的）。
- **`user/message` 不全是人说的**：只有 `source.kind = user` 是用户的输入；`time-context`、`skill-catalog`、`runtime-context`、
  `agent-instructions` 是 DSH 每轮注入的上下文，`agent-message` / `subagent-settled` 是子 Agent 的消息。`agent/inbox/spliced` 里排队的消息
  进这一轮时会再记一条 `user/message`，两处都显示就重复了。
- **DSH 的 seq 从 0 开始**，Persona 的 seq 0 表示「什么都还没有」（`since_seq` / `before_seq` 的零值）：映射成 DSH seq + 1。
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
- **列表不是归属**（2026-11-01）：插件只列最近的顶层会话（7 天内、最多 80 个），node 以前只按「当前列表」认归属，掉出列表的 DSH 会话
  （更早的、DSH 重启后插件忘了的）就被当成这台机器上没在跑的 Claude 会话，`claude --resume` 到 herdr 里。DSH 的老会话 id 是纯 uuid、
  和 Claude 的一样，能过 id 校验；新会话（`session-…`）先报 `invalid session id`。现在 node 记着哪个 runtime 列过哪些会话，从没列过的先问 runtime，
  插件把手机碰过的会话一直列着（`e2e/older-session.mjs`：列表上限 1、重启 DSH 后给掉出列表的会话发消息）。
- **数会话先分清父子**（同一天，误判过一次）：`~/.dsh/sessions` 下两千多个目录里 98% 是子 Agent / workflow 派生的会话（会话头
  `origin: "subagent"` 或带 `parentSession`），顶层会话只有几十个，7 天内只有个位数。插件本来就不列子会话，80 的上限远没碰到；
  当时手机上「发不出去的 dsh 会话」其实是 `DSH-Claw` 目录下一个**开在 Claude 桌面版里的 Claude 会话**（`docs/lessons/agents.md`）。
  查「某个会话为什么怎样」先拿到它的 id 和来源，别从总数推。会话头是 zstd 压缩的 jsonl 第一行（`session.v4.jsonl.zstd`，老的是 `v2` / 无版本号）。
- **`runtime.sock` 跟着 hub 连接，一次掉线就要一分半**（2026-11-01，#224）：常驻 node 为每条 hub 连接新建一个 Node，`runtime.sock` 和 runtime 的连接
  也跟着它建、跟着它没。node 重启或 hub 重连时：launchd 约 10 秒拉起 node，hub 经 tailcat 重连几十秒，插件要等 hub 连上 socket 才出现，
  它的重试又退避到 30 秒，合计一分半，期间打开 DSH 会话报 transcript not found。现在 socket 和 runtime 连接归常驻 node 进程（`RuntimeHost`），
  hub 换连接时不断；SDK 在 socket 文件还在时每秒重试。端到端 `runtime-sdk/e2e/resident.mjs`（真的 `--attach`，断开再连 hub、重启常驻 node）。
- **pi-web 只在有会话打开时才加载扩展**（2026-11-01，接 pi 时实测）：一个 pi-web 进程跑所有会话，扩展模块加载一次、每个会话调一次工厂函数，
  但第一个会话打开之前一次都不加载。只靠扩展的话，pi-web 重启后 Persona 一个 pi 会话都看不到。所以 pi 另有一个伴随进程负责磁盘上的会话
  （`docs/pi.md`），两个 runtime 列同一个会话时 node 选正在跑它的那个：pi 的会话文件没有锁，「后连上的说了算」会让手机的消息落到第二个写它的进程上。
- **SDK 的定时器不撑进程，独立跑的 runtime 要自己撑**（2026-11-01，pi 伴随进程）：SDK 的重连、推送定时器都 `unref()`，因为它一般寄生在 DSH、pi 的进程里，
  不该替宿主拖住退出。pi-web 伴随进程是只有 runtime 的独立进程，一断开（node 还没起、在重启、太旧）事件循环就空了，进程 `exit 0`，launchd 反复拉起。
  独立进程自己留一个 `setInterval`（`companion.mjs`）。
- **主动断开要拦住 socket 的 close 回调**（同一天）：遇到太旧的 node 主动断开后，`close` 回调看到没有 peer，又安排了 1 秒后的重连，把「等 node 重启」的计时器盖掉了：
  常驻 node 的日志每秒两行 connected / left。单元测试碰巧过了（只看了某一刻没连上），要数旧 node 收到的 hello 次数才测得出（`fake-node.js` 的 `hellos`）。
- **herdr 里的 pi**（2026-11-01，本机装 herdr 0.9.5 实测）：pi 窗格的 `HERDR_PANE_ID` 就是 `agent list` 里的 `pane_id`（`w1:p1`）；herdr 的 pi 集成报
  `agent_session {kind: "path"}`，值是会话文件，文件名末尾的 uuid 是 pi 的会话 id（以前 node 只认 `kind: "id"`，pi 一直被丢掉）。herdr 的命令行在没有 server 时只回
  `server_not_running`，不会自己起一个；node 会把 `/opt/homebrew/bin` 加进 PATH，所以 brew 装上 herdr 后旧 node 也找得到它的命令行。`herdr integration install claude`
  会改 `~/.claude` 的 hooks，在别人正在用 Claude 的机器上别顺手装。真机验证用 `XDG_CONFIG_HOME=<临时目录> herdr --session <名字> server`，按名字停。
- **测试卡住要看是不是忘了启动**（同一天）：伴随进程的单元测试没调 `host.start()`，`await n.ready` 永远等不到，`npm test` 挂了三分钟，杀掉后留下临时目录。
  跑单测也套一层超时（`perl -e 'alarm 300; exec @ARGV' npm test`）。

- **从手机驱动的会话里调不了 DSH 的插件管理**（2026-11-02）：在插件管理里启用 `jarvis-dsh-runtime` 时，DSH 的热加载用 `AsyncLocalStorage` 标记「正在事务里」，
  插件在事务里建的 socket 把这个标记带进了之后所有的回调。经插件进来的消息（手机发的）启动的每一轮都被当成「还在事务里」，`plugin_manager`
  的 `set_bundle` 一律报 `HMR transactions cannot be nested`，派子 Agent 去调也一样（继承同一个上下文）。从 DSH 网页发的消息不受影响。
  链接安装的插件也不在热加载的监视目录里，换文件不会重新加载。所以插件的更新改成自己做（`docs/dsh.md`「自动更新」），不靠插件管理。
- **同一个进程里重新 import 同一路径拿到的是旧代码**（同一天）：Node 的 ESM 缓存按 URL，文件换了也一样。插件在 DSH 里重新加载时（插件管理关掉再打开），
  之前 import 过的 `lib/dsh.js` 还是第一次的那份，新版本白换。加载器因此从 `<store>/<哈希>/` 加载，随包附带的那份也先拷进去：内容变了路径就变了。
  加载器的单元测试是这样发现的（改了随包文件，同一个进程里再起一个加载器，跑的还是旧版本）。
- **pi 扩展自己发 `/命令` 时，命令不存在就成了发给模型的消息**（2026-11-02，pi 自动更新）：pi 只在命令的上下文里给扩展 `reload()`，扩展要换版本就
  注册一个命令，再 `pi.sendUserMessage("/jarvis-reload", {expandPromptTemplates: true})` 对自己执行。pi 先找扩展命令，找不到就把这串字当用户消息交给模型，
  所以只对确实注册了这个命令的会话发（老 pi 没有 `registerCommand` 就不换）。pi-web 正在回复时会拒绝 reload，要等两轮之间。
