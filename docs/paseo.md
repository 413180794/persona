# Paseo 引擎

[Paseo](https://paseo.sh) 是一个多 Agent 管理器：一个常驻的 daemon，加上桌面、手机、网页 App。Claude Code、Codex 和各种 ACP Agent
都由它通过各家 SDK 来跑，不开终端；它也有自己的工作区（项目、worktree）。在 Persona 里，Paseo 和 herdr、Herdr App、Orca 并列，
是一台电脑的**引擎**（`docs/engines.md`）：一台电脑选了 Paseo，Agents 里显示的就是这台电脑上 Paseo 里的 Agent，从手机新建、
接着做、回答卡片也都在 Paseo 里进行。

和另外三个的区别在接入方式：herdr / Herdr App / Orca 是终端，node 读它们的窗格；Paseo 没有终端，node 通过一个**桥**接它。
桥是一个 runtime（`docs/runtimes.md`），一头说 Paseo 的 WebSocket 协议，一头说 Persona 的 runtime 协议；它由 node 自己运行，不用另外安装。

```
 手机 / 网页 / Mac ── hub ── node ──runtime.sock── 桥（node 运行，integrations/paseo）──WebSocket── Paseo daemon ── Claude Code / Codex / ACP…
                              └── 读 ~/.claude 里的对话记录（跑 Claude Code 的 Agent）
```

## 怎么用

1. 电脑上开着 Paseo：打开 Paseo 的桌面 App，或者运行 `paseo daemon start`。
2. 在 App 里打开设置 › 机器，选中这台电脑，把「引擎」改成 **Paseo**。配对时问引擎那一步也可以直接选。
3. 等几秒，Agents 里就会出现 Paseo 里的 Agent。

不用装别的东西。桥要 Node.js 22 以上，node 会用 Paseo 自己的那份：CLI 版用的 node，桌面版用 App 本身的二进制，
以 `ELECTRON_RUN_AS_NODE=1` 运行。

## 显示哪些会话

- **哪些 Agent 会列出**：Paseo 里没归档的 Agent 全部列出，最多 300 个。在跑的和在等你的一定在列。
  Paseo 自己的 App 里看得到的 Agent，Persona 里也看得到；归档了的就不列了。
- **会话 id**：
  - 跑 Claude Code 的 Agent（包括 `extends: "claude"` 的自定义 provider），id 就是它的 Claude 会话 id，和 herdr、「未运行」里的同一段对话是一个 id。
    Paseo 自己新建的 Agent，跑第一轮之前还没有会话 id，这时先不列。
  - 其他 Agent 的 id 是 `paseo-<agent id>`。
  - 从 Persona 新建的、从 Persona 接手的 Agent，id 写在它的标签 `persona.session` 上，以后都不变。
    Claude 那边换了会话（`/clear`）也不变。
- **内容**：
  - **跑 Claude Code 的 Agent**：内容直接读这台电脑上 Claude 的对话记录，和 herdr 里的会话完全一样：工具卡片、子 Agent、后台任务、
    压缩、「在做什么」都有。同一份对话记录不会再多一行「未运行」。对话记录还没写出来（第一轮刚开始），或者 node 不认识它所在的配置目录时，
    改用 Paseo 的 timeline，写出来以后自动换过去。
  - **其他 Agent**：内容来自 Paseo 的 timeline，换成 App 画的那套：命令、读写文件、diff、搜索、网页、子 Agent、待办都按 Claude Code 的工具名画。
- **状态**：

  | Paseo 里 | Persona 里 |
  | --- | --- |
  | 有待处理的请求（授权、提问、计划） | 需要你 |
  | `running`、`initializing` | 工作中 |
  | `idle`：跑过一轮 | 完成 |
  | `idle`：还没跑过 | 空闲 |
  | `error` | 完成，最后一句是出错原因 |
  | `closed`：没在运行，进程退了 | 未运行（发消息时 Paseo 重新载入它） |

- **空间**：Paseo 的工作区。同一个工作区的会话在 App 里分在一组，名字是 Paseo 里的工作区名。
- **逐字**：手机看着一个会话时，桥订阅这个 Agent 的 timeline，正在写的文字逐字显示。没人看着的会话桥不订阅。

## 能做什么

| 能力 | Paseo | 说明 |
| --- | --- | --- |
| 列出会话、显示状态 | ✅ | Paseo 的目录有变化时马上推送 |
| 发消息、发图片 | ✅ | Agent 正在工作时，消息插进这一轮（Paseo 的 `steer`，和 Claude Code 里边做边打字一样）；同一条消息重发只投一次 |
| 中断 | ✅ | |
| 授权、提问、计划卡片 | ✅ | 手机和 Paseo App 都能回答，先答的为准：在 Paseo 里答了，手机上的卡片会撤掉；在手机上点「交回电脑」，就留给 Paseo App 回答 |
| 从手机新建会话 | ✅ | Paseo 里每个能用的 provider 都是一个启动器，带它的模型、思考强度和权限模式（只有 Claude Code 有权限模式） |
| 接着「未运行」的 Claude 会话 | ✅ | Paseo 导入这段对话，会话 id 不变；Paseo 里已经有这个会话就直接用，归档过的会先恢复。别处有一个不归 Paseo 管的 claude 开着它时拒绝，避免两个进程同时写 |
| 改名 | 后端支持 | 改的是 Paseo 里 Agent 的标题。iOS、Android、网页的「重命名」目前只在有终端的会话上显示，Paseo 的会话暂时在 Paseo 里改 |
| 关闭 | 后端支持 | 在 Paseo 里归档这个 Agent，它会停下并从列表里消失。App 的「关闭」按钮和改名一样，暂时只在有终端的会话上显示 |
| 切权限模式、思考强度 | ✅ | Claude Code 的五种模式；思考强度只给这个模型支持的那几档 |
| 按键、读屏、实时终端、手机接管 | ❌ | Paseo 没有终端 |
| 手机工具（`phone_*`、`mail_*`） | ❌ | 还没注入到 Paseo 的 Agent 里 |

登录 shell 里的启动器（`~/.zshrc` 里的）仍然会出现在新建表单里，选它的时候：Paseo 里有同名的 provider 就用那个，
没有就用 Paseo 的默认 provider（Claude Code 自己的那个开着就用它，否则用第一个跑 Claude Code 的）。

## 桥怎么运行

- **桥的代码**：放在 personad 里，是运行时包 `jarvis-paseo-runtime`（`internal/runtimepkg`，构建时从 `integrations/` 生成）。
  引擎换成 Paseo 时，node 把它写到 `runtime.sock` 旁边的 `runtime-packages/jarvis-paseo-runtime/<哈希>/`，然后运行：
  `node bridge.mjs --socket <runtime.sock> --paseo-home <Paseo 的 home>`。
- **用哪个 Node.js**，按顺序取第一个 22 以上的：
  1. `PERSONA_NODEJS`
  2. Paseo daemon 自己用的那个（`paseo.pid` 里的进程）
  3. PATH 上的 `node`
  4. Homebrew、`/usr/local`、volta、nvm 的常见安装位置
- **退出和停止**：
  - 桥退出后 node 会再运行它，间隔从 1 秒开始翻倍，最长 1 分钟；稳定跑满 5 分钟后间隔回到 1 秒。
  - 引擎换走时 node 停掉它：先 SIGTERM 它的进程组，3 秒后 SIGKILL。
  - node 一直开着桥的 stdin；node 突然没了（被 kill），stdin 就断了，桥随之退出。这样不会留下一个旧桥，占着 runtime id 把新 node 的桥挡在外面。
- **日志**：桥的输出逐行写进 node 的日志，前缀 `paseo bridge:`。
- **node.info**：引擎列表里，桥连着时 Paseo 显示可用，版本是 Paseo 的版本；桥没连上但 daemon 有回应时也显示可用，
  桥起不来的原因写在 `note` 里（App 显示在引擎选项下面）。

## 怎么找到 Paseo

- **地址**：只认 Paseo home（`PASEO_HOME`，默认 `~/.paseo`）里 `paseo.pid` 写的地址，和 Paseo 自己的 CLI 一样。不会退回到默认端口，
  因为那个端口上可能是另一个 home 的 daemon。
  - 没有 `paseo.pid`：Paseo 没在运行。
  - 有 `paseo.pid` 但里面还没有地址：Paseo 正在启动。
  - 两种情况都稍后再试。
- **密码**：Paseo 设了密码时，用 home 里的 `local-credential` 登录（Paseo 的 CLI 也是这样）。没设密码时 Paseo 不检查。
- **连接时声明的能力**：桥以 `cli` 客户端连接，声明 `owned_subscriptions`：只收自己请求的回复和自己订阅的消息，不收广播。
  订阅三样：
  - Agent 目录
  - 待处理的请求和 provider 变化
  - 正在看的 Agent 的 timeline
- **Paseo 重启（比如升级）**：桥每秒重连一次，实测约 1.5 秒恢复，期间会话留在列表里。
- **Paseo 停了超过 30 秒**：桥离开 node，引擎显示没检测到。跑 Claude Code 的会话退回「未运行」，对话记录还在；
  这时发消息会提示 Paseo 没连上，什么都不会发出去。

## 映射

- **timeline → items**：seq 用 Paseo timeline 的 seq，各种内容落在哪一个 seq 上如下：
  - 消息、思考、通知：落在它开始的那一行。
  - 工具调用：调用落在开始的那一行，结果落在完成的那一行。
  - 正在写的文字只走逐字，写完才变成 item。
  - Paseo 改写了历史（rewind），`history` 就换掉，App 整页重取。
- **工具名**：

  | Paseo 的 detail | App 里画成 |
  | --- | --- |
  | `shell` | `Bash` |
  | `read` | `Read` |
  | `edit` | `Edit` |
  | `write` | `Write` |
  | `search` | `Grep` / `Glob` / `WebSearch` |
  | `fetch` | `WebFetch` |
  | `sub_agent` | `Task` |
  | `plan` | `ExitPlanMode` |
  | `todo` | `TodoWrite` |
  | 其他 | 原名，参数原样 |

- **卡片**：
  - Paseo 的 `question` → 提问卡。回答时，Claude Code 的按问题原文作 key，其他 Agent 按 Paseo 卡片用的 `header`。
  - `plan` → 计划卡：
    - 批准时选 Paseo 的 `implement`，再切到手机上选的模式；原来是 bypass / auto，就用 `implement_resume` 回到原来的模式。
    - 打回时把意见作为拒绝的理由。
  - 其余 → 授权卡：
    - Claude Code 带建议规则时，卡上有「总是允许」，写明加的是哪条规则、用在哪里。
    - 拒绝时带上你写的理由。
  - 卡片 id 由 Paseo 的请求 id 算出来，桥重启后挂回原来那张卡。
  - Claude Code 交给 Paseo 的计划请求里没有计划正文（`ExitPlanMode` 的 input 是空的），桥从 timeline 里的那次工具调用读出正文。
- **新建**：用 Paseo 的 `agent.create`。Agent id 由桥选，表单选的模型、权限模式、思考强度都带上，第一句话作为 `initialPrompt`。
  - 从某个空间新建，就放进那个 Paseo 工作区。
  - Paseo 25 秒内没建完，先回会话行，后台接着等。
- **接手**：用 Paseo 的 `import_agent`，provider 选这台电脑上最近一个跑 Claude Code 的 Agent 所用的那个。
  标签 `persona.session` 设成原来的会话 id，Paseo 里归档过的 Agent 会被恢复。

## 不做的、已知的限制

- 没有终端：按键、读屏、实时终端画面、手机接管都没有，App 按会话的能力隐藏这些入口。
- 手机工具（`phone_*`、`mail_*`）还没注入到 Paseo 里的 Agent。
- 三端 App 的「重命名」「关闭」按会话有没有终端来显示，Paseo 的会话没有终端，所以暂时不显示。node 和桥已经支持这两个操作，
  要等会话的 `capabilities` 加上改名 / 关闭以后，App 才能按能力显示。
- 只连本机的 Paseo daemon：Paseo 的 relay 和它连着的其他主机不接。另一台电脑上的 Paseo，用那台电脑自己的 node。
- Paseo timeline 里的图片不显示。跑 Claude Code 的会话不受影响，它的图片来自对话记录。
- 机器开了 Codex 历史时，Paseo 里同一段 Codex 对话可能还会多出一行只读的 Codex 历史。
- Paseo 自己的插件、定时任务、hub 执行这些功能不接入。

## 验证

- 单元测试：`cd integrations/paseo && npm test`。用一个假的 Paseo daemon（`test/fake-paseo.js`）和 SDK 的假 node，测列表、内容、
  逐字、发送、卡片、新建、接手、切模式、Paseo 重启和离开。
- 端到端：先 `cd server && go generate ./internal/runtimepkg && go build -o /tmp/personad ./cmd/personad`，再运行
  `JARVISD=/tmp/personad CLAUDE_BIN=<一个 Claude Code 可执行文件> node integrations/paseo/e2e/run.mjs`，一共 29 项检查，需要本机装了 `paseo`。
  - 起一个真的 Paseo daemon，用它自己的 home 和一个空闲端口。provider 是 `extends: "claude"`，模型是 `e2e/model.mjs` 里按剧本回答的
    Anthropic API。
  - 起一个真的 `jarvisd node --stdio`，由 SDK 的测试工具扮演 hub。把引擎设成 Paseo 后，桥由 node 自己运行。
  - 从新建一直测到关闭后接手。
  - 只用临时目录，不碰本机正在用的 Paseo 和 `~/.claude`。
  - 加 `KEEP=1` 保留临时目录，加 `DEBUG=1` 打印 node 日志里桥的那几行。
- 本机实测（2026-11-04，Paseo 0.14.0、Claude Code 2.1.294）：
  - 端到端 29 项全过。
  - 对一台真实在用的 Paseo 做了只读核对：桥连一个假 node，只放行读请求。结果是每个能用的 provider 一个启动器；跑 Claude Code 的会话都指向自己的对话记录，
    绝大多数在本机找到（找不到的在 node 不认识的配置目录里，内容改走 timeline）；没有发出任何写请求。

## 排错

- **引擎显示「没检测到」**：Paseo 没在运行，或者 `paseo.pid` 不在 node 认为的 home 里。用了自定义 home 的，给 personad 设上 `PASEO_HOME`。
- **引擎可用，但下面写着「Paseo 在运行，但 Persona 连不上它：…」**：桥起不来，后面是原因，常见的是找不到 Node.js 22。
  设 `PERSONA_NODEJS` 指向一个 node 22 以上的可执行文件。
- **会话列出来了，内容却是 Paseo 的样子，不是 Claude 的对话记录**：node 没找到这个会话的对话记录，比如 provider 用了别的
  `CLAUDE_CONFIG_DIR`。把那个目录写进 hub.json 里这台机器的 `machines[].claude_config_dirs`（`docs/config.md`）。
- **详细日志**：看 node 的日志里 `paseo bridge:` 开头的行。
