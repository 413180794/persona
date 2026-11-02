# 启动器与「设置 › 模型供应商」

启动器决定 claude 用哪个网关、哪个模型。以前全是各台电脑 `~/.zshrc` 里调用 claude 的函数或 alias（见下「~/.zshrc 里怎么认」，
node 把名字敲进 herdr 窗格的交互式 shell 里跑，函数和 alias 都会展开）；2026-10-31 起「设置 › 模型供应商」把它们和在 App 里加的放在一起管理。
代码：`server/internal/hub/launchers.go`、`server/internal/node/managed.go`；App `Views/Settings/ModelLaunchersView.swift`。

## 两种来源

| 来源 | 在哪定义 | 能做什么 |
| --- | --- | --- |
| **Jarvis 管理** | App 里新增：名字、Base URL、API Key、模型、小模型、额外参数 | 改、删 |
| **~/.zshrc** | 各台电脑 `~/.zshrc` 里的函数，打开这页时同步（node 缓存 1 分钟） | 看每台电脑上的函数定义（Key / Token 打码）、在 Jarvis 里隐藏 / 恢复 |

- 范围是**全局一份**：Jarvis 管理的每台电脑都能用；~/.zshrc 的标出来自哪几台（同名的合成一行）。离线电脑的列表用上次同步的（settings `launchers.zshrc`）。
- 删除 ~/.zshrc 的启动器**只在 Jarvis 里隐藏**（settings `launchers.hidden`），文件不动；新对话、助手的选择器里不再出现。
- 同名时 Jarvis 管理的优先（~/.zshrc 的那个被盖住）。名字只能是字母、数字、`_`、`-`，不能叫 `claude`。

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
  Jarvis 起 claude 时自己也要传一个 `--settings`（把提问、授权、做完转到手机的 hooks），而 Claude Code 只认**最后一个** `--settings`。
  所以 node 启动时把 alias 的那份和 Jarvis 的合成一个文件（alias 的所有设置 + 两边的 hooks，Jarvis 的排在后面），写到
  `~/.jarvis/launcher-settings/<启动器>-<claude|assistant>-settings.json`（0600，里面有 Token），作为最后一个 `--settings` 传进去；
  每次启动重写一遍（`node/launcher_settings.go`）。`~`、`$HOME`、相对路径（按会话目录）、`--settings '{…}'` 都认；文件读不了时退回只传 Jarvis 的。
  函数写的启动器里的 `--settings` 看不到，照样会被顶掉。

## Jarvis 管理的怎么跑

- 存在 settings `launchers.managed`；API Key 用 `~/.jarvis/secret.key`（和邮箱凭据同一把）AES-GCM 加密，**接口从不返回 Key**。
- hub 在 node 连上时、每次改动后把完整设置（含 Key）推给所有 node（`launchers.managed`），node 只放内存。
- 启动时 node 敲进 herdr 窗格（或无头助手执行）的是 `jarvisd launch <名字> …`：这个子命令经本机 node.sock 取设置
  （`launcher.env`），去掉 shell 里已有的 `ANTHROPIC_*` 等变量，设上 `ANTHROPIC_BASE_URL`、`ANTHROPIC_API_KEY`、各档模型
  （`ANTHROPIC_MODEL`、`ANTHROPIC_DEFAULT_*_MODEL`、`CLAUDE_CODE_SUBAGENT_MODEL`，小模型给 haiku / small fast），再 exec claude。
  Key 不进文件、不进终端、不在命令行上。终端里也能直接敲 `jarvisd launch <名字>`。
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

详情页「测试可用性」：在有这个启动器的每台在线电脑上（Jarvis 管理的在 hub 所在的机器上）真的发一个只回答一个词的请求
（`claude -p … --output-format json --max-turns 1`，Codex 是 `codex exec`），在空目录里跑，90 秒超时；显示 ✓ / ✗、用时、哪个模型答的、
失败的原因（打码）。会花几个 token。`POST /api/v1/launchers/{name}/check?machine=`。

## 删除

详情页的「删除启动器 / 在 Jarvis 里隐藏」点一下，在按钮那里弹出「确认删除 / 确认隐藏」（popover，2026-10-31 用户要求），再点才执行。

列表里 ~/.zshrc 的启动器左滑「隐藏」，「已隐藏」里的左滑「恢复」（2026-11-01 用户要求），不再确认：只改 Jarvis 里显不显示，随时能滑回来。
Jarvis 管理的左滑是「删除」，连 hub 里存的 Key 一起删、不能恢复，所以先弹确认。`LauncherSwipeTests` 对着 mock hub（`MOCK_MANAGED_LAUNCHER=1`）覆盖。

## 接口

| 接口 | 用途 |
| --- | --- |
| `GET /api/v1/launchers` | 全部（先同步各台电脑的 ~/.zshrc） |
| `GET /api/v1/launchers/{name}` | 一个；~/.zshrc 的带 `definitions`（每台电脑的函数，打码）和 `details`（跑一遍得到的参数） |
| `POST /api/v1/launchers/{name}/check?machine=` | 测试可用性 |
| `POST /api/v1/launchers` | 新增 `{name, base_url, model, small_model?, args?, api_key}` |
| `PUT /api/v1/launchers/{name}` | 改（`api_key` 不填就不变）；只对 Jarvis 管理的 |
| `DELETE /api/v1/launchers/{name}` | Jarvis 管理的删除；~/.zshrc 的隐藏 |
| `POST /api/v1/launchers/{name}/restore` | 取消隐藏 |
| `GET /api/v1/launchers/{name}/quota?machine=&refresh=1` | 额度（见下）：`{machine, checked_at, quota}`，查不了时 `{error, unsupported}`；hub 缓存一分钟，`refresh=1` 重读 |
| `GET /api/v1/machines/{mid}/launchers` | 这台能启动的（选择器用）：它的 ~/.zshrc 去掉隐藏的，加上 Jarvis 管理的；每项带 `source` |

「设置 › 助手」中的对话模型入口和定时任务的模型入口都打开统一的「模型配置」。供应商负责接入与默认值，用途负责具体选择，见 `docs/model-config.md`。

## 直接 API 供应商

Jarvis 管理的接入增加 `kind`（`launcher` / `api`，省略时维持旧的启动器行为）和 `api`（`messages` / `responses`）字段。
`kind: api` 由 hub 直接发请求，不下发给 node，也不会出现在新建 Agent 的启动器列表中；小模型和 CLI 额外参数不用于这类接入。
认证仍使用原有的加密 Key 存储，不需要把 API Key 放进用途配置。原有 `/launchers` 接口与供应商名称保持兼容。

## 额度（2026-10-31）

启动器页的「额度」读启动器的 Key 在网关那边还剩多少，支持 **relay-history** 和 **FAKEMODEL**（`server/internal/quota`）：

- relay-history：`GET <网关>/me/usage?timezone=Asia/Shanghai`，用 Key 本身鉴权（relay-history 的 `docs/api-key-self-usage.md`），回 `service: relay-history` 才算。
  取 Key 的预算（`budget`：累计 / 每周美元上限）、它能用的每个账号的窗口（5 小时 / 每周 / 每月…，按模型的窗口不列），和 Key 自己今日 / 本周 / 本月的用量。
  同一 provider 的几个席位名字一样，用账号 ID 的后 6 位区分；同名窗口只留读数最全的那个（一个账号会报两遍 5 小时）；`traex_weekly` 这类带前缀的叫「每周」。
  Key 的「额度可见性」是「不开放」时只显示这一句。
- FAKEMODEL（Key 以 `fm_` 开头）：`POST /auth/key-login {"key"}` 换一个只读会话，`GET /api/quota`，用完 `POST /auth/logout`。
  每个供应商只列**有上限**的窗口（网关自己的计数没有上限，说明不了还剩多少），按模型的放在「按模型 N」里。
- 其他网关、没设网关地址（官方接口、codex）：说明查不了，不报错。
- 谁去读：Jarvis 管理的启动器由 hub 用解封的 Key 读；~/.zshrc 的由**有这个启动器的那台电脑**读（node 的 `launchers.quota`：跑一遍探测拿到地址和 Key，就地请求网关），
  Key 不离开那台电脑，地址也是那台电脑能到的地址。优先 hub 自己那台。
- App：`LauncherQuotaSection`，放在「可用性」上面；用量比例 ≥ 70% 橙色、≥ 90% 或用完红色；右上角刷新。mock：`scripts/mock/mock_hub.py` 的 `cc_kiro` / `example-cli` / `cc_glm`，
  DEBUG `-openLauncherPage <名字>` 直接打开。实测：`TestQuotaLive`（`JARVIS_LIVE_QUOTA=<探测写出的 env 文件>`）。
