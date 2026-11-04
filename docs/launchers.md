# 启动器与「设置 › 模型供应商」

启动器决定 claude 用哪个网关、哪个模型。以前全是各台电脑 `~/.zshrc` 里调用 claude 的函数或 alias（见下「~/.zshrc 里怎么认」，
node 把名字敲进 herdr 窗格的交互式 shell 里跑，函数和 alias 都会展开）；2026-10-31 起「设置 › 模型供应商」把它们和在 App 里加的放在一起管理。
代码：`server/internal/hub/launchers.go`、`server/internal/node/managed.go`；App `Views/Settings/ModelLaunchersView.swift`。

## 两种来源

| 来源 | 在哪定义 | 能做什么 |
| --- | --- | --- |
| **Persona 管理** | App 里新增：名字、Base URL、API Key、模型、小模型、子 Agent 模型、自定义模型项、额外参数 | 改、删 |
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
- **alias 自带 `--settings`**（`alias work="claude --settings ~/.claude/settings.work.json …"`，网关、Token、模型在那个文件的 `env` 里）：
  Persona 起 claude 时自己也要传一个 `--settings`（把提问、授权、做完转到手机的 hooks），而 Claude Code 只认**最后一个** `--settings`。
  所以 node 启动时把 alias 的那份和 Persona 的合成一个文件（alias 的所有设置 + 两边的 hooks，Persona 的排在后面），写到
  `~/.persona/launcher-settings/<启动器>-<claude|assistant>-settings.json`（0600，里面有 Token），作为最后一个 `--settings` 传进去；
  每次启动重写一遍（`node/launcher_settings.go`）。`~`、`$HOME`、相对路径（按会话目录）、`--settings '{…}'` 都认；文件读不了时退回只传 Persona 的。
  函数写的启动器里的 `--settings` 看不到，照样会被顶掉。

## Persona 管理的怎么跑

- 存在 settings `launchers.managed`；API Key 用 `~/.persona/secret.key`（和邮箱凭据同一把）AES-GCM 加密，**接口从不返回 Key**。
- hub 在 node 连上时、每次改动后把完整设置（含 Key）推给所有 node（`launchers.managed`），node 只放内存。
- 启动时 node 敲进 herdr 窗格（或无头助手执行）的是 `jarvisd launch <名字> …`：这个子命令经本机 node.sock 取设置
  （`launcher.env`），去掉 shell 里已有的 `ANTHROPIC_*` 等变量，设上 `ANTHROPIC_BASE_URL`、`ANTHROPIC_API_KEY`、各档模型
  （`ANTHROPIC_MODEL`、`ANTHROPIC_DEFAULT_*_MODEL`；小模型给 haiku / small fast；子 Agent 模型给 `CLAUDE_CODE_SUBAGENT_MODEL`，留空时跟主模型；
  自定义模型项给 `ANTHROPIC_CUSTOM_MODEL_OPTION` 和它的名字 / 说明），再 exec claude。
  Key 不进文件、不进终端、不在命令行上。终端里也能直接敲 `jarvisd launch <名字>`。
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
环境比，只算启动器改了的（跑 `dsh` 的启动器认成 DSH，不会真把 DSH 跑起来，也不再当成 Claude 去起）。页面上按电脑显示（各台一样就合成一组）：

- 运行：Agent（Claude Code / Codex / TRAEX）、网关、Key 放在哪个变量（值不显示）
- 模型：主模型、Fable / Opus / Sonnet / Haiku、小模型、子 Agent，各自的变量名；Codex 看 `-m`
- 上下文：窗口（`CLAUDE_CODE_MAX_CONTEXT_TOKENS`）、和它不同时的自动压缩窗口、思考强度、请求超时
- 启动参数，以及其他设置（`ANTHROPIC_*`、`CLAUDE_CODE_*`、代理……，像 Key / Token 的值是 •••）
- 函数原文收在折叠里；参数里直接写的 Key（`_claude_proxy … sk-…`、`Bearer …`）也打码

跑一遍会执行启动器在 claude 之前做的事（比如取 token 的脚本），所以每个启动器 10 分钟最多跑一次、15 秒超时（连子进程一起杀），
stdin 是 /dev/null。单测：`launcherprobe_test.go` 用 `testdata/launchers.zsh` 覆盖 helper、带小模型和窗口、helper 前加变量、
子 shell 里 export、Codex 几种写法。

## 测试可用性（`node/launchercheck.go`）

详情页「测试可用性」：在有这个启动器的每台在线电脑上（Persona 管理的在 hub 所在的机器上）真的发一个只回答一个词的请求
（`claude -p … --output-format json --max-turns 1`，Codex 是 `codex exec`），在空目录里跑，90 秒超时；显示 ✓ / ✗、用时、哪个模型答的、
失败的原因（打码）。会花几个 token。`POST /api/v1/launchers/{name}/check?machine=`。

## 删除

详情页的「删除启动器 / 在 Persona 里隐藏」点一下，在按钮那里弹出「确认删除 / 确认隐藏」（popover，2026-10-31 用户要求），再点才执行。

列表里 ~/.zshrc 的启动器左滑「隐藏」，「已隐藏」里的左滑「恢复」（2026-11-01 用户要求），不再确认：只改 Persona 里显不显示，随时能滑回来。
Persona 管理的左滑是「删除」，连 hub 里存的 Key 一起删、不能恢复，所以先弹确认。`LauncherSwipeTests` 对着 mock hub（`MOCK_MANAGED_LAUNCHER=1`）覆盖。

## 接口

| 接口 | 用途 |
| --- | --- |
| `GET /api/v1/launchers` | 全部（先同步各台电脑的 ~/.zshrc） |
| `GET /api/v1/launchers/{name}` | 一个；~/.zshrc 的带 `definitions`（每台电脑的函数，打码）和 `details`（跑一遍得到的参数） |
| `POST /api/v1/launchers/{name}/check?machine=` | 测试可用性 |
| `POST /api/v1/launchers` | 新增 `{name, base_url, model, small_model?, subagent_model?, custom_model?, custom_model_name?, custom_model_description?, args?, api_key, preset?}`；带 `preset` 时没填的从预设补上（见下面「从插件添加」） |
| `GET /api/v1/launchers/presets` | 插件提供的供应商预设（`docs/plugins.md`「模型供应商」）；旧 hub 回 404，App 当成没有 |
| `PUT /api/v1/launchers/{name}` | 改（`api_key` 不填就不变）；只对 Persona 管理的 |
| `DELETE /api/v1/launchers/{name}` | Persona 管理的删除；~/.zshrc 的隐藏 |
| `POST /api/v1/launchers/{name}/restore` | 取消隐藏 |
| `GET /api/v1/launchers/{name}/quota?machine=&refresh=1` | 额度（见下）：`{machine, checked_at, quota}`，查不了时 `{error, unsupported}`；hub 缓存一分钟，`refresh=1` 重读 |
| `GET /api/v1/machines/{mid}/launchers` | 这台能启动的（选择器用）：它的 ~/.zshrc 去掉隐藏的，加上 Persona 管理的；每项带 `source` |

助手的模型只在「模型配置 › 主对话」里选（「设置 › 助手」不再有模型入口，#1291）；定时任务的模型入口打开统一的「模型配置」。供应商负责接入与默认值，用途负责具体选择，见 `docs/model-config.md`。

## 直接 API 供应商

Persona 管理的接入增加 `kind`（`launcher` / `api`，省略时维持旧的启动器行为）和 `api`（`messages` / `responses`）字段。
`kind: api` 由 hub 直接发请求，不下发给 node，也不会出现在新建 Agent 的启动器列表中；小模型、子 Agent 模型、自定义模型项和 CLI 额外参数不用于这类接入。
认证仍使用原有的加密 Key 存储，不需要把 API Key 放进用途配置。原有 `/launchers` 接口与供应商名称保持兼容。

## 从插件添加

插件贡献的模型供应商（`docs/plugins.md`「模型供应商」）在「设置 › 模型供应商 › 新增」里多一个「从插件添加」：hub 的 `GET /api/v1/launchers/presets`
有预设时，新增页顶上是「自己填写 / 从插件添加」的切换（没有预设就和以前一样只有自己填写）。从插件添加：选一个预设（名字、描述、接入方式和地址），
只填名字和 Key，默认模型从预设的 `models` 里选（不选就是预设的默认）；`POST /api/v1/launchers` 只带 `name`、`api_key`、`preset`，选了别的模型才带 `model`，
地址、接入方式、API、小模型由 hub 从预设补上。预设的 `key_hint` 写在 Key 下面，`models_error` 写在模型下面。加好以后和自己填写的一样能编辑。

- iOS：`LauncherPresetModel`（`apple/Persona/Views/Settings/LauncherPresetModel.swift`，单元测试 `LauncherPresetModelTests`）、`LauncherPresetViews.swift`；
  DEBUG `-tab settings -openModelLaunchers YES -openLauncherEditor preset` 直接打开从插件添加（`-openLauncherEditor YES` 打开新增页）。
- 网页：`web/src/lib/launcher-presets.ts`（测试同名 `.test.ts`），新增弹窗里同样的切换。Android 还没有新增供应商的页面，没做。
- 截图：假 hub 自带两个示例预设（`example-gateway/team`、`example-gateway/api`），`POST /api/v1/launchers` 在内存里补全并加进列表。

## 额度

启动器页的「额度」读启动器的 Key 在网关那边还剩多少。**读哪些网关、怎么读是额度插件的事**（`docs/plugins.md`「额度」）：开源仓库里不带任何网关的读法，
没装额度插件时这一节说明查不了，不报错。代码：通用部分 `server/internal/quota`（报告的形状、代发请求），hub 一侧 `hub/launcherquota.go`、`hub/plugin_quota.go`，
node 一侧 `node/launcherquota.go`。

- 插件**看不到 Key**：它回答 `quota.query`，要发的请求经 `quota.http` 交给手里有 Key 的那一方去发，请求里的 `${launcher_key}` 由那一方换成 Key；
  只能发到这个启动器自己的网关（同源，跟随重定向也不出这个源），回答里出现 Key 的地方换回占位符。
- 谁去发：Persona 管理的启动器由 hub 用解封的 Key 发；~/.zshrc 的由**有这个启动器的那台电脑**发（node 的 `launchers.http`：跑一遍探测拿到地址和 Key，就地请求网关），
  Key 不离开那台电脑，地址也是那台电脑能到的地址。优先 hub 自己那台。旧 node 没有 `launchers.http` 时说「node 太旧」。
- 几个额度插件时：声明了 `hosts` 且对得上网关主机名的先问，再问没声明 `hosts` 的；回 `unsupported` 的跳过，第一个回答的为准。都不认识就是「查不了」。
- 结果缓存一分钟（`refresh=1` 重读）。报告的 `gateway` 是插件贡献的 id，`gateway_name` 是给人看的名字。
- App：`LauncherQuotaSection`，放在「可用性」上面；标题是「额度 · <网关名>」，网关名用报告的 `gateway_name`（旧 hub 没有时显示 `gateway`），App 里不写死任何网关的名字。
  用量比例 ≥ 70% 橙色、≥ 90% 或用完红色；右上角刷新。DEBUG `-openLauncherPage <名字>` 直接打开。
  假 hub 的 `cc_main`、`cc_team` 带两个示例网关的额度（`示例网关`、`示例中转`），截图用。

