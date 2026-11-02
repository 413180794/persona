# Runtime CLI（DEBUG 构建里的命令通道）

在模拟器里跑着的 DEBUG App 里开一个本地端口，脚本用 `scripts/jarvisctl.py` 发命令：读状态、等画面停下、截图、
导航、切风格，（后续）读无障碍树、点按、注入状态。不用为每个场景加一个启动开关、每张截图重启一次 App。

为什么做、和其他方案（URL scheme、运行时重读启动开关）的取舍、实测数字：见 2026-11-02 的评估（spike 分支
`spike/runtime-cli`）。结论：App 内 JSON 服务是唯一能“问” App 的方案；身份截图实测每张 3.1 s（老方法 9.3 s）。

## 怎么用

```sh
export JARVISCTL_UDID=<模拟器 udid>            # 不设就是 booted
scripts/jarvisctl.py launch --hub http://127.0.0.1:18760   # 结束并重新启动 App（带一次性 token），等它应答
scripts/jarvisctl.py state                      # 当前 tab、打开的会话、风格、主题色、深浅色、连接状态
scripts/jarvisctl.py online                     # 等 hub 的实时连接（2 s 还没连上就主动重连一次）
scripts/jarvisctl.py settle --pixels 600        # 等画面停下（连续几帧变化不超过 600 个像素）
scripts/jarvisctl.py screenshot out.png         # settle 之后 simctl io screenshot
scripts/jarvisctl.py call <命令> k=v …           # 任何 App 认识的命令
```

脚本里：`from jarvisctl import Ctl; c = Ctl(); c.launch(hub); c.call("settle", pixels=600)`。

**`settle` 代替固定的 `sleep`**：截图脚本（身份对比）启动后 `sleep 7` 再截，大部分页面 1–2 s 就画好了，有的页面 7 s
还是空白。启动后 `jarvisctl.py settle` 再截，实测每张 4.3–5.3 s（原来 9.2–11.4 s），截出来和原来逐像素一致。
`--pixels` 是容差：工作中的 Agent 的状态圆点一直在动（约 300 个像素），不给容差就永远等不到“停下”。

## 命令

| 命令 | 参数 | 回答 |
|---|---|---|
| `ping` | | `pong`、`pid` |
| `state` | | `tab`、`openChat`、`assistant`、`visibleSession`、`settingsOpen`、`settingsDepth`、`style`、`color`、`appearance`、`connection`、`paired` |
| `settle` | `timeout`（8 s）、`every`（150 ms）、`stable`（3 次）、`pixels`（0） | `settled`、`frames`、`changed` |
| `online` | `timeout`（15 s） | `nudged`（是否主动重连过）；超时报错并带当前状态 |
| `navigate` | `scene`（home / assistant / agents / chat / inbox / settings / machines），`session`（`<机器>/<会话>`，chat 和 assistant 要），`machine`（machines 下打开某台），`defaults`（只在建页面时读的开关，如 `{"openWork": true}`），`keep`（不换新的界面树） | 落到和对应启动开关一样的页面（`-newAssistant`、`-openAssistant`、`-tab dev`、`-openSession`、`-tab mail`、`-tab settings`、`-openMachines`），默认在一棵新的界面树上（窗口的根换成新的 `RootView`，Agents 页推进去的对话、抽屉、滚动位置都从头来）。回答同 `state` |
| `set-style` | `style`（glass / clay / island / lark / beautiful）、`color`（主题色） | 同 `state`；整棵树按新风格重建 |
| `set-appearance` | `value`（light / dark / system） | 只改 App 的窗口；`jarvisctl.py appearance` 默认改整个模拟器（`simctl ui appearance`，状态栏一起变，截图脚本用这个） |
| `defaults` | `set`（只在这次运行有效，写进注册域，不落盘）、`remove`（删掉存下的键，`-openAgentsFresh` 做的事）、`get` | `values` |

每个回答都有 `ok` 和 `ms`（App 里花的时间）；出错是 `{"ok": false, "error": …}`，`jarvisctl.py` 以非 0 退出。

## 导航和风格

```sh
scripts/jarvisctl.py set-style beautiful            # 整棵树按新风格重建
scripts/jarvisctl.py appearance dark                # 整个模拟器（状态栏一起），--in-app 只改 App
scripts/jarvisctl.py navigate machines              # = -tab settings -openMachines YES，不用重启
scripts/jarvisctl.py navigate chat --session <机器>/<会话> --flag openWork=YES
scripts/jarvisctl.py idle && scripts/jarvisctl.py screenshot out.png
```

`navigate` 换一棵新的界面树，而不是在原来的树上改路由：评估时在原树上导航，Agents 页推进去的对话关不掉、收件箱顶栏的
玻璃底和冷启动不一样。新树要补上 `WindowGroup` 给的 `scenePhase`，不然只在前台工作的页面（机器页的指标，
`.tabActiveTask`）一直不动。

`idle`（只在 `jarvisctl.py` 里）：等 mock hub（`scripts/mock_hub.py` 在 `/mock/hits` 数每个 GET）0.8 s 没有新请求。
页面还在等数据时（机器页的指标）画面是静止的，`settle` 会以为好了，先 `idle` 再 `settle`。

和冷启动逐像素一致的 `reset`（连对话也冷加载）在下一个 PR。

## 协议和安全

- 只在 DEBUG 构建里：`apple/Jarvis/Debug/` 下的文件整个包在 `#if DEBUG && os(iOS)` 里，入口是 `AppDelegate` 原有
  `#if DEBUG` 块里的一行 `RuntimeCLI.startIfAsked()`。
- 默认关：不带 `-runtimeCLI <端口>` 启动就不监听。
- 只听回环：`127.0.0.1:<端口>`（模拟器和 Mac 共用回环）。
- 一次性 token：`launch` 每次随机生成，经启动参数 `-runtimeCLIToken` 传给 App，存在 `~/.cache/jarvisctl/<udid>.json`（0600）；token 不对的请求直接拒绝。
- 一个连接一个请求：一行 JSON `{"token", "cmd", "args"}`，回一行 JSON。命令都在主线程（MainActor）上执行。
- `scripts/release.sh` 检查 Release 二进制的 `strings` 里没有 `runtimeCLI`、`RuntimeCLI`、`_AXSSetAutomationEnabled`。

## 加命令

一类命令一个文件 `apple/Jarvis/Debug/RuntimeCLI+<类>.swift`（同样包在 `#if DEBUG && os(iOS)` 里），在
`RuntimeCLI.commands` 里登记名字；回答里不能有 NaN / 无穷（JSON 写不出来，会变成 `reply is not JSON` 的错误）。
`jarvisctl.py` 不认识的命令用 `call` 就能发，常用的再加成子命令。
