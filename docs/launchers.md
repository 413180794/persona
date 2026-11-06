# 启动器与「设置 › 模型供应商」

启动器决定 claude 用哪个网关、哪个模型。以前全是各台电脑 `~/.zshrc` 里调用 claude 的函数或 alias（见下「~/.zshrc 里怎么认」，
node 把名字敲进 herdr 窗格的交互式 shell 里跑，函数和 alias 都会展开，前面带 `HERDR_AGENT=claude`，见下「herdr 怎么认出启动器起的 Agent」）；2026-10-31 起「设置 › 模型供应商」把它们和在 App 里加的放在一起管理。
代码：`server/internal/assistant/models_launchers.go`（管理者的，hub 只转发这些路由，`docs/model-config.md`「谁管模型」）、`server/internal/node/managed.go`；App `Views/Settings/ModelLaunchersView.swift`。

## 两种来源

| 来源 | 在哪定义 | 能做什么 |
| --- | --- | --- |
| **Persona 管理** | App 里新增：名字、Base URL、API Key、认证方式、模型、小模型、子 Agent 模型、自定义模型项、额外参数 | 改、删 |
| **~/.zshrc** | 各台电脑 `~/.zshrc` 里的函数，打开这页时同步（node 缓存 1 分钟） | 看每台电脑上的函数定义（Key / Token 打码）、在 Persona 里隐藏 / 恢复 |

- 范围是**全局一份**：Persona 管理的每台电脑都能用；~/.zshrc 的标出来自哪几台（同名的合成一行）。离线电脑的列表用上次同步的（settings `launchers.zshrc`）。
- 删除 ~/.zshrc 的启动器**只在 Persona 里隐藏**（settings `launchers.hidden`），文件不动；新对话、助手的选择器里不再出现。
- 同名时 Persona 管理的优先（~/.zshrc 的那个被盖住）。名字只能是字母、数字、`_`、`-`，不能叫 `claude`。

## ~/.zshrc 里怎么认（`node/sessions.go` 的 `launcherScript`）

node 在登录 shell（`zsh -ic`，先加载 `~/.zshrc`）里一次取两样，缓存 1 分钟：

- **`_claude_launchers` 的输出**：有这个辅助函数的 `~/.zshrc`（每行名字、网关、模型，定宽）照旧，排在最前。
- **alias**（2026-11-01 起）：zsh 自己把每个 alias 拆成词、去掉引号（`${(@Q)${(z)…}}`），node 只认**最后跑的是 `claude`** 的：
  前面可以有变量（`alias dmcc="ANTHROPIC_BASE_URL=… ANTHROPIC_AUTH_TOKEN=… claude --dangerously-skip-permissions"`）、经过
  `env`（含 `-u` / `-i`）/ `command` / `exec` / `noglob`、写绝对路径，或者同一个 alias 里先 `export …;`。网关取 `ANTHROPIC_BASE_URL`，
  模型取 `ANTHROPIC_MODEL`，没有就看 `--model`。按名字排序（shell 不保留定义顺序）；`_claude_launchers` 已经列出的同名 alias 不重复。
- **Codex**（2026-11-02 起）：PATH 上有 `codex` 时多一个 `codex`，再加上只跑一条 `codex` 命令的 alias，排在 Claude 的后面，带 `agent: "codex"`；
  从手机新建 Codex 会话见 `docs/codex.md`「从手机新建」。
- 不算启动器的：名字就叫 `claude` 的 alias、跑 `traex` 的 alias、做了别的事的 Codex alias（`cd x && codex`）、
  跟 Agent 无关的（`ll`、`gst`…）。都没有时只有默认的 `claude`。
- 列表里只有名字、网关、模型；alias 里的 Key / Token 不出 node。「查看定义」对 alias 显示的是 `whence -f` 给的原文，Key 照样打码。
- 函数写的启动器还是要靠 `_claude_launchers` 列出来：函数体里最后跑什么，不真跑一遍看不出来（见下「启动器页面上的参数」）。
- **启动器自带 `--settings`**（`alias work="claude --settings ~/.claude/settings.work.json …"`，或者函数里这样调 claude；网关、Token、模型在那个文件的 `env` 里）：
  Claude Code 只认**最后一个** `--settings`，所以 Persona 不拿 `--settings` 带自己的 hooks（把提问、授权、做完转到手机的那些）。本机 Claude Code ≥ 2.1 时
  hooks 是只在这次会话加载的插件（`--plugin-dir ~/.persona/claude-hooks`，助手是 `assistant-hooks`，`node/hooksplugin.go`），启动器自己的 `--settings`
  原样生效，alias 和函数都一样；Persona 只给它管理的启动器传 `--settings`（见下「Persona 管理的怎么跑」）。
  更老的 Claude Code（或读不到版本）只能把 hooks 放进 `--settings`：node 启动时把 alias 的那份和 Persona 的合成一个文件（alias 的所有设置 + 两边的 hooks，
  Persona 的排在后面），写到 `~/.persona/launcher-settings/<启动器>-<claude|assistant>-settings.json`（0600，里面有 Token），作为最后一个 `--settings` 传进去；
  每次启动重写一遍（`node/launcher_settings.go`）。`~`、`$HOME`、相对路径（按会话目录）、`--settings '{…}'` 都认；文件读不了时退回只传 Persona 的。
  函数写的启动器里的 `--settings` 看不到，在这种 claude 上照样会被顶掉。
- 列模型（`node/launcher_models.go`）为了不触发 hooks 会传 `--settings '{"disableAllHooks":true}'`（alias、托管启动器的照样合并进去），函数里自带的
  `--settings` 在这里仍会被顶掉，列出来的是没有它时的模型：关掉 hooks 只能写在 `--settings` 里，要合并就得先读出函数传的那份（#1411）。

## herdr 怎么认出启动器起的 Agent（`HERDR_AGENT`，#3454）

herdr 按窗格前台进程的名字认 Agent（basename 是 `claude` / `claude-code`、`codex`），认不出就不报 Agent。启动器是个包装、exec 的是另一个名字的程序时
（比如一个 Python 脚本 exec 按版本号命名的原生二进制 `claude-2.1.318`），Claude 的输入框已经在屏幕上，node 却等不到 herdr 说「这是 claude」，
等满 3 分钟才失败。herdr 给包装的办法是 `HERDR_AGENT=<agent>`：看名字之前先读前台进程 exec 时的环境（Linux `/proc/<pid>/environ`，macOS `KERN_PROCARGS2`），
herdr 0.7.1（Linux）/ 0.7.5（macOS）起支持，更老的 herdr 和 Windows 上忽略。

- node 在 herdr 窗格里起 Claude 时敲的是 `HERDR_AGENT=claude <启动器> --session-id … --mcp-config …`（`claude_start_dir` 时是
  `HERDR_AGENT=claude CLAUDE_CONFIG_DIR=… claude …`，Persona 管理的是 `HERDR_AGENT=claude jarvisd launch <名字> …`），「在这里运行 Agent」选的窗格也一样；
  herdr 重启后恢复用的 `resume_argv`（`eval "… --resume <id> …"`，`node/herdrresume.go`）带同样的前缀。hub 声明跑 Codex 的启动器敲 `HERDR_AGENT=codex <启动器> …`，
  从手机新建的 Codex 见 `docs/codex.md`「从手机新建」；TRAEX、pi 不带（herdr 没有给它们的名字）。别的引擎（Orca…）和无头运行（助手、测试可用性、列模型）不带。
- 只敲给认 `VAR=val cmd` 的 shell：bash、zsh、fish（3.1 起）、sh、dash、ksh、mksh。读这行字的 shell，新标签页按 herdr 开窗格用的 shell 算
  （herdr 用它自己的 `$SHELL`：node 起的 herdr 是用户的登录 shell，`node/herdrstart_unix.go` `userShell`；不看 herdr 配置里的 `[terminal] default_shell`），
  `resume_argv` 也按它（herdr 恢复时开新窗格、把 `eval …` 敲进去）；「在这里运行 Agent」按那个窗格前台的 shell。tcsh / csh、pwsh、nu、elvish、xonsh
  或认不出的 shell 照旧敲不带前缀的命令，herdr 认不认得出和以前一样。Windows 上 node 不敲（herdr 不读，`codex-start` 也不设）。
- 只放在 Agent 那条命令前面：shell 只把它交给这条命令起的程序，跑完不留在 shell 里。**不要** `export` 到窗格的 shell，也不要写进 herdr 标签 / 工作区的环境：
  shell 自己环境里的 `HERDR_AGENT` 对 herdr 是权威的（之后 `unset` 也去不掉），窗格里以后跑的任何东西都会被当成 Claude。也不用 `env HERDR_AGENT=claude <启动器>`：
  `env` 找不到函数和 alias。
- 带得到 claude 的（实测 bash 5.2 / 5.3、zsh 5.9、fish 4.6、dash）：PATH 上的程序、函数、alias（含前面带变量的 `alias x='A=1 claude …'`）、`eval` 同一段文字。
  `node/herdragent_shell_test.go` 在真 shell 里跑 node 敲的命令（bash；装了 zsh、fish 时也跑），断言 claude 拿到 `HERDR_AGENT=claude` 和参数、之后 shell 里和下一个程序都没有它。
- 带不到的（和以前一样认不出，不会更糟）：alias 展开后先跑别的命令（`alias x='export A=1; claude'`、`alias x='cd ~/w && claude'`），前缀给了 `export` / `cd`。
  zsh 开了 `POSIX_BUILTINS`（或 bash `--posix`）时，`export` 前面的它在 shell 里留成一个没导出的变量，无害。
- zsh 开了 `POSIX_BUILTINS` 而启动器是函数时，`HERDR_AGENT=claude` 会**留在那个窗格的 shell 里，而且是导出的**：claude 退出后在这个窗格里再跑的程序，herdr 也会当成 Claude。
  这个选项很少开；开了的把启动器写成 alias 或脚本。
- 包装 exec claude 时 argv[0] 仍是 `claude` 的，herdr 本来就认得，前缀不改变什么。
- 还是认不出时 node 的报错见 `docs/lessons/agents.md`「claude did not become ready…」。

## Persona 管理的怎么跑

- 存在 settings `launchers.managed`；API Key 由管理者用 `~/.persona/secret.key`（和邮箱凭据同一把，`server/internal/secretkey`）AES-GCM 加密，**接口从不返回 Key**。
- 管理者在 node 连上时、每次改动后把完整设置（含 Key）经 hub 推给所有 node（`launchers.managed`），node 只放内存；hub 不存也不解。
- 启动时 node 敲进 herdr 窗格（或无头助手执行）的是 `jarvisd launch <名字> …`：这个子命令经本机 node.sock 取设置
  （`launcher.env`），去掉 shell 里已有的 `ANTHROPIC_*` 等变量，设上 `ANTHROPIC_BASE_URL`、Key（见下「认证方式」）、各档模型
  （`ANTHROPIC_MODEL`、`ANTHROPIC_DEFAULT_*_MODEL`；小模型给 haiku / small fast；子 Agent 模型给 `CLAUDE_CODE_SUBAGENT_MODEL`，留空时跟主模型；
  自定义模型项给 `ANTHROPIC_CUSTOM_MODEL_OPTION` 和它的名字 / 说明），再 exec claude。
  Key 不进终端、不在命令行上。终端里也能直接敲 `jarvisd launch <名字>`。
- settings 里的 `env` 压过进程环境，用户全局 `~/.claude/settings.json` 的 `env` 里有另一个网关时会把上面这些盖掉，所以 Persona 起它时还把同样的变量（含 Key）
  写进 `~/.persona/launcher-settings/<名字>-env-settings.json`（0600），作为 `--settings` 传进去（`node/launcher_settings.go`）；更老的 Claude Code 上它和
  Persona 的 hooks 合在 `<名字>-<claude|assistant>-settings.json` 里。这些文件每次启动都重写，手改的会被冲掉。
- **Artifacts 一律关**：Persona 管理的启动器起的 claude 带 `CLAUDE_CODE_DISABLE_ARTIFACT=1`（`jarvisd launch` 的环境和上面 settings 的 `env` 都有），
  settings 文件里还写 `"enableArtifact": false`（Claude Code 认 `--settings` 里的关）。Artifacts 是发布到 claude.ai 的，走网关 + API Key 本来就用不了；
  Claude Code 从 2.1.26x 起默认打开它，交互式会话会在 system 消息里带上 Artifacts 的技能说明，有的网关在能力检查里直接拒
  （`400 request is not representable on this edge: Claude Code Artifacts are enabled`），herdr 里新开的 Agent 第一轮就失败，`claude -p` 不受影响。
- **认证方式**（`auth`，2026-11-04）：Key 放哪个变量。`api_key`（默认，已有的启动器都是它）放 `ANTHROPIC_API_KEY`，Claude Code 发
  `x-api-key`，官方 API 只认这个；`bearer` 放 `ANTHROPIC_AUTH_TOKEN`，发 `Authorization: Bearer`，给只认 Bearer 的兼容网关用
  （relay-history 两种都认，不用改）。管理者拿启动器直接调 Messages API（分拣、翻译等用途）也按它发头；Responses 协议本来就用 Bearer，不用选。
  改启动器时请求里没带 `auth`（还没升级的 App）就保留原来的值。
- **另一个认证变量设成空**：Claude Code 把各处 settings 的 `env` 合在一起，启动器**没设**的变量照样从用户的 `~/.claude/settings.json` 来。
  那里放着别的网关的 `ANTHROPIC_AUTH_TOKEN` 时，Claude 会把它当 `Authorization: Bearer` 和启动器的 `x-api-key` 一起发；relay-history 收到两个凭据，
  除非恰好一个对得上，否则 401 `invalid or missing API key`。所以有 Key 的启动器总把没用的那个（`ANTHROPIC_AUTH_TOKEN` 或 `ANTHROPIC_API_KEY`）
  设成空，环境和上面的 `--settings` 里都有；设成空的变量不会再被用户的 settings 补上（2026-11-04 用 Claude Code 2.1.308 实测）。没有 Key 时两个都不设，照旧。
- 交互模式的 Claude Code 第一次见到 `ANTHROPIC_API_KEY` 里的 Key 会问「Do you want to use this API key?」，默认选中「No (recommended)」，
  回车（或手机上的对话框卡片点了默认项）就把这个 Key 记成拒绝，之后这个启动器起的会话都没有凭据。Key 是用户在 设置 › 模型 里自己填的，
  所以 `jarvisd launch` 在 exec claude 前把它记成已允许，和在对话框里选 Yes 一样：Key 的最后 20 个字符（Claude Code 用它认 Key）写进
  Claude 全局配置（`$CLAUDE_CONFIG_DIR` 或 home 下的 `.claude.json`，旧版的 `~/.claude/.config.json` 在用时写它）的
  `customApiKeyResponses.approved`，并从 `rejected` 里去掉（`node/apikeyapproval.go`）。写的时候拿 Claude 自己的配置锁
  （`<配置路径>.lock` 目录，超过 10 秒算过期），不会和正在运行的 claude 同时写。已经允许过就不写；还没有配置文件（第一次运行，
  Claude 自己会走引导）也不写；写失败只在 stderr 提示一行，照常启动。无头的 `-p`（助手）本来就不问。
- hub 重启或 node 重连之前没收到设置时，`jarvisd launch` 报「hub 还没发过来」。

## 启动器页面上的参数（`node/launcherprobe.go`）

只读 ~/.zshrc 里函数的原文看不出 `_claude_proxy URL TOKEN MODEL WINDOW` 这类 helper、局部变量最后设了什么，所以 node 把启动器在
子 shell 里真跑一遍：`claude` / `codex` / `traex` / `dsh` / `command` / `env` 都换成桩，只打印 Agent 会拿到的环境变量和参数，再和 shell 自己的
环境比，只算启动器改了的（跑 `dsh` 的启动器认成 DSH，不会真把 DSH 跑起来，也不再当成 Claude 去起）。
Claude 的参数里有 `--settings`（`alias work="claude --settings ~/.claude/settings.work.json"`，最后一个为准，文件或内联 JSON）时，
那份 settings 的 `env` 盖在探测到的环境上（和 Claude Code 一样 settings 优先）：网关和 Key 常写在那里，不读的话页面看不到网关、「额度」以为没有网关（#1378）。
读不了就照旧。

探测会在登录 shell 里运行 `( <名字> )`，所以 hub 来问的探测、额度（`launchers.http`）、`--model` 检查和模型列表只对这台电脑认得的启动器做
（~/.zshrc 列出的、托管的、runtime 的、`claude` / `traex`），或者 hub.json 里写了的（hub 带 `configured` 担保）；别的名字一律「unknown launcher」，
不会被当成命令运行（比如 `reboot`，#1185）。所以 hub.json 里加的包装脚本，要先部署 hub 再部署 node，否则旧 hub 不带 `configured`，新 node 不认它。

页面上按电脑显示（各台一样就合成一组）：

- 运行：Agent（Claude Code / Codex / TRAEX）、网关、Key 放在哪个变量（值不显示）
- 模型：主模型、Fable / Opus / Sonnet / Haiku、小模型、子 Agent，各自的变量名；Codex 看 `-m`
- 上下文：窗口（`CLAUDE_CODE_MAX_CONTEXT_TOKENS`）、和它不同时的自动压缩窗口、思考强度、请求超时
- 启动参数，以及其他设置（`ANTHROPIC_*`、`CLAUDE_CODE_*`、代理……，像 Key / Token 的值是 •••）
- 函数原文收在折叠里；参数里直接写的 Key（`_claude_proxy … sk-…`、`Bearer …`）也打码

**截不到的启动器**（#1489）：函数调用另一个脚本、脚本再按绝对路径 `exec …/claude` 的，桩函数换不掉，探测看不到 Agent 起来。
- 页面上显示「看不出这个启动器的网关和模型……照常能用」，不当成坏了。
- 带模型的发送和新建（`launcherModelCheck`）照探测之前的老路径走：模型用 `--model` 交给启动器，不再 502。
- 能截到、又写死了别的模型的，照旧拒绝。
- 注意：这种启动器探测时真的会跑到那个脚本（桩换不掉它），脚本里的 claude 会在 15 秒后被杀掉。

跑一遍会执行启动器在 claude 之前做的事（比如取 token 的脚本），所以每个启动器 10 分钟最多跑一次、15 秒超时（连子进程一起杀），
stdin 是 /dev/null。单测：`launcherprobe_test.go` 用 `testdata/launchers.zsh` 覆盖 helper、带小模型和窗口、helper 前加变量、
子 shell 里 export、Codex 几种写法，`--settings` 的 env（`withSettingsEnv`）。

## 测试可用性（`node/launchercheck.go`）

详情页「测试可用性」：在有这个启动器的每台在线电脑上（Persona 管理的在 hub 所在的机器上，管理者挑）真的发一个只回答一个词的请求
（`claude -p … --output-format json --max-turns 1`，Codex 是 `codex exec`），在空目录里跑，90 秒超时；不带 hooks（`--settings` 里 `disableAllHooks`，托管启动器的 env、alias 自己的 `--settings` 照样合并进去：Persona 的 hooks 在有的电脑上会把它卡到超时，#3412）；显示 ✓ / ✗、用时、哪个模型答的、
失败的原因（打码）。会花几个 token。`POST /api/v1/launchers/{name}/check?machine=`。
hub 也拿它定期检查每台电脑上的每个启动器，坏了派活时绕开、能修的自动修，见 `docs/launcher-health.md`。

## 删除

详情页的「删除启动器 / 在 Persona 里隐藏」点一下，在按钮那里弹出「确认删除 / 确认隐藏」（popover，2026-10-31 用户要求），再点才执行。

列表里 ~/.zshrc 的启动器左滑「隐藏」，「已隐藏」里的左滑「恢复」（2026-11-01 用户要求），不再确认：只改 Persona 里显不显示，随时能滑回来。
Persona 管理的左滑是「删除」，连存着的 Key 一起删、不能恢复，所以先弹确认。在详情页隐藏、恢复、删除之后列表跟着变：改动都经 `AppModel.launchersChanged()`，列表按 `launchersRevision` 重拉（#1433）。`LauncherSwipeTests` 对着 mock hub（`MOCK_MANAGED_LAUNCHER=1`）覆盖左滑和详情页隐藏。

## 接口

| 接口 | 用途 |
| --- | --- |
| `GET /api/v1/launchers` | 全部（先同步各台电脑的 ~/.zshrc） |
| `GET /api/v1/launchers/{name}` | 一个；~/.zshrc 的带 `definitions`（每台电脑的函数，打码）和 `details`（跑一遍得到的参数） |
| `POST /api/v1/launchers/{name}/check?machine=` | 测试可用性 |
| `POST /api/v1/launchers` | 新增 `{name, base_url, model, small_model?, subagent_model?, custom_model?, custom_model_name?, custom_model_description?, args?, api_key, auth?, preset?, display_name?, assign?}`；`auth` 是 `api_key`（默认）或 `bearer`；带 `preset` 时没填的从预设补上，`name` 也可以空（见下面「内置预设」「从扩展添加」）；带 `assign` 时回 `{launcher, assigned, kept}` |
| `GET /api/v1/launchers/presets` | 供应商预设：内置的（`plugin: "builtin"`）加扩展的（`docs/extensions.md`「模型供应商」），按请求语言排序；旧 hub 回 404，App 当成没有 |
| `PUT /api/v1/launchers/{name}` | 改（`api_key` 不填就不变）；只对 Persona 管理的。下面「供应商的模型和网关参数」里的字段不带就保留原值 |
| `POST /api/v1/launchers/test` | 测试连接（不经 node，见下）：`{name?, base_url?, auth?, api_key?, headers?, model, preset?}` → `{ok, ms, model, error?, code?}`（`code: "region_blocked"` 是 Anthropic 按地区拦下，换 Key 没用，见 `docs/protocol.md`）；带 `preset` 时没填的地址、模型用预设的（默认模型） |
| `POST /api/v1/launchers/discover` | 获取模型：`{name?, base_url?, auth?, api_key?, headers?}` → `{models:[{id, name?, context_window?, efforts?, default_effort?}], error?, code?}` |
| `DELETE /api/v1/launchers/{name}` | Persona 管理的删除；~/.zshrc 的隐藏 |
| `POST /api/v1/launchers/{name}/restore` | 取消隐藏 |
| `GET /api/v1/launchers/{name}/quota?machine=&refresh=1` | 额度（见下）：`{machine, checked_at, quota}`，查不了时 `{error, unsupported}`；管理者缓存一分钟，`refresh=1` 重读 |
| `GET /api/v1/machines/{mid}/launchers` | 这台能启动的（选择器用）：它的 ~/.zshrc 去掉隐藏的，加上 Persona 管理的；每项带 `source` |

助手的模型只在「模型配置 › 主对话」里选（「设置 › 助手」不再有模型入口，#1291）；定时任务的模型入口打开统一的「模型配置」。供应商负责接入与默认值，用途负责具体选择，见 `docs/model-config.md`。

## 供应商的模型和网关参数（#2455）

Persona 管理的供应商照 cindy 的模型多了这些字段（`proto.ManagedLauncher`，都可选，旧数据照旧能读）。node 怎么把它们变成 env 见 `docs/claude-integration.md`。

| 字段 | 含义 |
| --- | --- |
| `models` | 网关的模型 `[{id, name?, context_window?, efforts?, default_effort?}]`；窗口大于 200K 的以 `<id>[1m]` 运行 |
| `opus_model` / `sonnet_model` / `haiku_model` / `fable_model` | 各档位的模型（`ANTHROPIC_DEFAULT_*_MODEL`），空就是 `model` |
| `subagent_force` | `CLAUDE_CODE_SUBAGENT_MODEL_FORCE=1` |
| `headers` | 自定义请求头（`ANTHROPIC_CUSTOM_HEADERS`），一行一个「名字: 值」 |
| `proxy` / `no_proxy` | 网关的 `HTTP(S)_PROXY` / `NO_PROXY`；代理要是 http(s) 或 socks5 地址 |
| `timeout_ms` / `compact_window` / `max_context` | `API_TIMEOUT_MS` / `CLAUDE_CODE_AUTO_COMPACT_WINDOW` / `CLAUDE_CODE_MAX_CONTEXT_TOKENS`，0 是默认 |
| `effort` | 默认思考档位 |

- **请求头和 Key 一样是机密**：管理者用同一把密钥加密存成 `headers_sealed`，只在下发给 node 时解开；GET 只给 `has_headers`。
  PUT 不带 `headers` 保留原值，带空串清掉。其他新字段也一样，不带就不变（旧版 App 改供应商不会把它们清空）。
- **写入时校验**：Base URL 合法；思考档位只能是 low / medium / high / xhigh / max，模型的默认档位要在它的档位里；数值不能为负；模型 ID 不能为空或重复；请求头每行要有「:」。
- 读取时 `source` 仍是 `jarvis` / `zshrc`，供应商自己的来源（`zshrc:<alias>@<机器>`、`preset:<id>`）放在 `origin`。
- 名字 `test`、`discover`、`import`、`presets`、`claude` 留给接口，不能用。

**测试连接和获取模型**由管理者直接请求网关（`assistant/models_probe.go`）（照 cindy 的 `buildProbeRequest` / `provider-model-fetch`），新增供应商还没保存时也能用：

- 带 `name` 时用已存的 Base URL、Key、请求头、默认模型和代理，请求里给了的覆盖已存的（`headers: ""` 就是这次不带请求头）。
- 测试连接发一次 `POST <base>/v1/messages`（`max_tokens: 1`，内容 `ping`）；获取模型发 `GET <base>/v1/models`。Base URL 已经以 `/v1` 结尾时不重复。
  `x-api-key` 和 `Authorization: Bearer` 两个头都带（Key 开头的 `Bearer ` 去掉），外加 `anthropic-version: 2023-06-01` 和自定义请求头。
- 超时 30 秒。失败给一句人话（401 认证失败、403 没权限、404 地址不对、429 限流、超时、连不上），后面跟网关自己的错误信息；
  Key 和请求头的值一律换成 `***`。这两个接口本身总是 200，结果看 `ok` / `error`。
- 模型列表认三种形状：`{data:[…]}`、`{models:[…]}`、裸数组（元素是对象或字符串）；ID 取 `id` / `slug` / `key` / `model_name` / `model_id`，
  名字取 `display_name` / `name`，窗口取 `context_length` / `context_window` / `max_context_length` / `max_input_tokens`。去重，保持顺序。
- 网关没说的用**内置目录**补全：`server/internal/assistant/models_catalog.json`，取自 cindy 的 `model-registry.json`（Apache-2.0，见 `THIRD_PARTY_NOTICES.md`）：
  Claude 系列，以及 GLM、Kimi、DeepSeek V4、Qwen3、MiniMax、Seed、MiMo 等网关常见的模型，只有 id、name、context_window、efforts、default_effort。
  查的时候不分大小写，去掉厂商前缀（`anthropic/…`）、`[1m]` 和日期后缀；目录里没有、但 ID 以 `[1m]` 结尾的记成 1M 窗口。网关自己说了的优先。
  更新目录：照 cindy 的新版重新生成，只收 registry 里写明了的值，不猜。

## 直接 API 供应商

Persona 管理的接入增加 `kind`（`launcher` / `api`，省略时维持旧的启动器行为）和 `api`（`messages` / `responses`）字段。
`kind: api` 由管理者直接发请求，不出现在新建 Agent 的启动器列表中；小模型、子 Agent 模型、自定义模型项和 CLI 额外参数不用于这类接入。
`api` 为空或 `messages` 的也随启动器一起下发给 node（#2455），主对话可以用它的 Base URL 和 Key 起 claude；`responses` 的不下发。
认证仍使用原有的加密 Key 存储，不需要把 API Key 放进用途配置。原有 `/launchers` 接口与供应商名称保持兼容。

## 内置预设

hub 自带一份常见厂商的 Anthropic 兼容入口（`server/internal/assistant/presets_builtin.json`，嵌进二进制），新手「只有 API Key」时选一家、粘 Key 就能接上（新用户上手设计 #287 的 4.3、5.4 节）。
`GET /api/v1/launchers/presets` 先列内置的、再列扩展的；扩展的预设 id 和内置的一样时盖掉内置那一项（位置不变）。

- 每项：`id`（`builtin/<厂商>`）、`plugin: "builtin"`、`name`（英文的平名）、`display_name` 和 `description`（按请求语言，词条在 `assistant-models_presets`）、`kind: launcher`、`api: messages`、
  `base_url`、`auth`（`api_key` / `bearer`，按厂商 Claude Code 说明里用的）、`model`（默认模型）、`small_model`、`models`、`signup_url`（开通 / 拿 Key 的页面）。`auth`、`signup_url`、`display_name` 只有内置的填。
- 排序：中文先列国内能直接开通的（DeepSeek、Kimi、智谱 GLM、阿里云百炼、MiniMax），再列 Anthropic 和国际版；其他语言先列 Anthropic、DeepSeek 和国际版。文件里每项的 `region`（`cn` / `global` / `both`）决定分组，组内按文件顺序。
- 只放厂商官方文档里写明的公开地址（按量付费的那个入口，不放编程套餐、中转站、公司网关）。地址、认证方式和模型名都对过官方文档；没有 Key，没有发过真实请求。厂商改了模型名就改这个文件，`TestBuiltinPresetsAreComplete` 查每项字段齐全。
  阿里云百炼用 `dashscope.aliyuncs.com/apps/anthropic`（按量计费的 `sk-` Key；Coding Plan、Token Plan 的 Key 不能用在这个地址）。
- 加的时候 `POST /api/v1/launchers {preset, api_key, name: "", display_name: ""}`：`name` 空时用预设 id 的厂商部分（`builtin/glm` → `glm`），不合规则的字符换成 `-`，重名加 `-2`、`-3`；`name` 由 hub 起、`display_name` 又是空的时用预设的（「智谱 GLM」），名字加了 `-2` 的显示名也带上同一个数（「智谱 GLM 2」），模型配置里分得开。
  自己填了 `name`、没填 `display_name` 的（包括老客户端从扩展添加的）不补显示名，照旧显示 `name`。
  没有 `preset` 时 `display_name` 也可以填（任何语言，最多 64 个字），`name` 仍然必填。`display_name` 存在供应商上，列表和模型配置的供应商名显示它，`name` 仍是启动时用的 id。
- `assign: ["assistant", "agent.new"]`：保存后对列出的用途各试一次，只在它还是默认值时换成新供应商——没在模型配置里选过供应商或模型、主对话的启动器也还是默认的 `claude`（设置 › 助手、hub.json 没改过）。
  结果 `assigned: [用途]`、`kept: [{purpose, reason}]`，`reason` 是 `customized`（有人选过）、`incompatible`（这一项用不了它）或 `failed`（读写配置出错）。只认这两个用途，别的回 400，什么都不存。

- hub 在 `features` 里登记 `builtin_presets`：客户端看到它才发空 `name`、`assign`；老 hub 上空 `name` 回 400、`assign` 不认。
- `POST /api/v1/launchers/discover` 也认 `preset`：没填 `base_url` 时用预设的地址，填了的优先。
- 新建 Agent 的 `/model` 列表只给「从这个内置预设加的」供应商补预设的模型（按 `preset` 认）；扩展的预设照旧也按网关地址认，内置的不按地址认，免得 ~/.zshrc 里一个指向 `api.anthropic.com` 的启动器多出一串模型。
- 兼容的代价：老 App 的「从插件添加」也会列出内置预设，说明里写「由扩展「builtin」提供」、标题是英文平名（老客户端不读 `display_name`）。加的路径能用（它们本来就发 `name` + Key + `preset`），文案的问题跟进在 #3621。

## 从扩展添加

扩展贡献的模型供应商（`docs/extensions.md`「模型供应商」）在「设置 › 模型供应商 › 新增」里多一个「从扩展添加」：hub 的 `GET /api/v1/launchers/presets`
有预设时，新增页顶上是「自己填写 / 从扩展添加」的切换（没有预设就和以前一样只有自己填写）。从扩展添加：选一个预设（名字、描述、接入方式和地址），
只填名字和 Key，默认模型从预设的 `models` 里选（不选就是预设的默认）；`POST /api/v1/launchers` 只带 `name`、`api_key`、`preset`，选了别的模型才带 `model`，
地址、接入方式、API、小模型由管理者从预设补上（预设是扩展的，管理者经 `app.call plugins.presets` 向 hub 要）。预设的 `key_hint` 写在 Key 下面，`models_error` 写在模型下面。加好以后和自己填写的一样能编辑。

新建 Agent 选模型时（`GET /api/v1/machines/{mid}/launchers/{name}/models`），走扩展网关的启动器除了 Claude Code `/model` 自己列的几项，
还列出预设的全部模型（`assistant/models_presets.go` 的 `presetModels`）：Persona 管理的按它记着的预设，没记的（有扩展预设之前自己填写的）和 ~/.zshrc 的
按网关地址对上预设（大小写、末尾的 `/` 不算）。只认 `kind: launcher` 的预设、只给 Claude Code 的启动器；`/model` 已有的同名项不重复，
补上的项 `description` 是预设的名字。这样一个网关一个启动器就能选到它的所有模型，不用一个模型建一个；扩展的 `dynamic_models` 更新了，选择器跟着变。

- iOS：`LauncherPresetModel`（`apple/Persona/Views/Settings/LauncherPresetModel.swift`，单元测试 `LauncherPresetModelTests`）、`LauncherPresetViews.swift`；
  DEBUG `-tab settings -openModelLaunchers YES -openLauncherEditor preset` 直接打开从扩展添加（`-openLauncherEditor YES` 打开新增页）。
- 网页（#3693，新用户上手 #287 的 4.3「三条路」B）：新增弹窗的第一步直接列 `GET /api/v1/launchers/presets` 的全部预设（内置的在前，按 hub 的顺序；扩展的带「扩展」标记），
  「自定义兼容网关」放最后，地址、鉴权方式只在它里面出现；客户端不再写死「Anthropic 官方」，用内置预设 `builtin/anthropic`。选了预设只粘 Key：Key 停下 0.7 秒（至少 8 个字符）
  自动 `POST /api/v1/launchers/test {preset, api_key, auth}` 测一次（直接 API 里 `api: "responses"` 的预设不测：hub 的测试只发 `/v1/messages`，改成一行说明），失败写 hub 的大白话并能「重新测试」，`signup_url` 是「Key 从哪拿」（新标签页打开）；名称（空时 hub 起）、默认模型收在「更多选项」。
  保存带 `assign: ["assistant", "agent.new"]`（开关默认开，「主对话和新开的 Agent 都用它」），最后一屏按 `assigned` / `kept` 写哪些用途换成了它、哪些没改和原因。
  缺口：设计要求「已有能用的 Claude Code 时开关默认关」，要等 `assistant_readiness`（第 4 个 PR）；在那之前开关总是默认开，hub 的 assign-if-default 只保护用户自己选过的用途。
  hub 的 `features` 没有 `builtin_presets`（老 hub）时，名称必填、放在 Key 上面，不发 `assign`、没有开关。「从 ~/.zshrc 导入」挪到列表页底下的「高级」，没有电脑时按钮禁用并写明原因。
  逻辑 `web/src/lib/launcher-presets.ts`（测试同名 `.test.ts`），界面 `web/src/views/settings/ProviderWizard.tsx`（测试 `ProviderWizard.test.tsx`）。
- Android（#3048）：设置 › AI › 模型供应商列表页（分组、搜索、详情、测试可用性、隐藏 / 恢复 / 删除、从 ~/.zshrc 导入），见 `docs/android.md`「设置的其他对齐项」。
- Android（#1331）：「新增模型供应商」页（`feature/settings` 的 `NewProviderScreen.kt`、`NewProviderState.kt`），入口在设置 › 模型配置右上角的「+」和用途页选供应商弹层里的「新增模型供应商…」（从用途页加的保存后直接选上，仍要点保存）；见 `docs/android.md`「新增模型供应商」。
- 截图：假 hub 自带两个示例预设（`example-gateway/team`、`example-gateway/api`），`POST /api/v1/launchers` 在内存里补全并加进列表。

## 额度

启动器页的「额度」读启动器的 Key 在网关那边还剩多少。**读哪些网关、怎么读是额度扩展的事**（`docs/extensions.md`「额度」）：开源仓库里不带任何网关的读法，
没装额度扩展时这一节说明查不了，不报错。代码：通用部分 `server/internal/quota`（报告的形状、代发请求），管理者一侧 `assistant/models_quota.go`（路由、缓存、定网关），hub 一侧 `extmgr/extension_quota.go`（扩展读额度，`app.call plugins.quota`），
node 一侧 `node/launcherquota.go`。

- 扩展**看不到 Key**：它回答 `quota.query`，要发的请求经 `quota.http` 交给手里有 Key 的那一方去发，请求里的 `${launcher_key}` 由那一方换成 Key；
  只能发到这个启动器自己的网关（同源，跟随重定向也不出这个源），回答里出现 Key 的地方换回占位符。
- 谁去发：Persona 管理的启动器由管理者用解封的 Key 发（hub 经 `models.launcher_http` 交给它，管理者不在时额度回 `503 assistant_offline`）；~/.zshrc 的由**有这个启动器的那台电脑**发（node 的 `launchers.http`：跑一遍探测拿到地址和 Key，就地请求网关），
  Key 不离开那台电脑，地址也是那台电脑能到的地址。优先 hub 自己那台。旧 node 没有 `launchers.http` 时说「node 太旧」。
- 几个额度扩展时：声明了 `hosts` 且对得上网关主机名的先问，再问没声明 `hosts` 的；回 `unsupported` 的跳过，第一个回答的为准。都不认识就是「查不了」。
- 结果缓存一分钟（`refresh=1` 重读）。报告的 `gateway` 是扩展贡献的 id，`gateway_name` 是给人看的名字。
- App：`LauncherQuotaSection`，放在「可用性」上面（网页同样，`web/src/views/settings/LauncherQuota.tsx`；Android 在机器页进的启动器页上，带 `machine=` 那台，见 `docs/android.md`「扩展画的界面和扩展市场」）；标题是「额度 · <网关名>」，网关名用报告的 `gateway_name`（旧 hub 没有时显示 `gateway`），App 里不写死任何网关的名字。
  用量比例 ≥ 70% 橙色、≥ 90% 或用完红色；右上角刷新。DEBUG `-openLauncherPage <名字>` 直接打开。
  假 hub 的 `cc_main`、`cc_team` 带两个示例网关的额度（`示例网关`、`示例中转`），截图用。

