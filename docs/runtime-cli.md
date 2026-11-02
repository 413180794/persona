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
| `settle` | `timeout`（8 s）、`every`（150 ms）、`stable`（3 次）、`pixels`（0）、`ignore`（元素过滤，一组或几组，它们的 frame 不比较）、`ignorePad`（6 pt）、`ignoreRects`（`[[x, y, w, h]]`，pt） | `settled`、`frames`、`changed`、`box`（最后两帧还不一样的地方） |
| `freeze` | `on`（默认 true，false 恢复）、`reduceMotion`（默认 true）、`layers`（strip / pause / none，默认 strip） | `frozen`、`reduceMotion`、`paused`；见「一直在动的画面」 |
| `online` | `timeout`（15 s） | `nudged`（是否主动重连过）；超时报错并带当前状态 |
| `reset` | `coldChats`（内存里不留对话，和启动后一样从磁盘缓存再到 hub 重新加载）、`dropChatCache`（磁盘缓存也清掉，= `-openChatsFresh YES`） | 新的界面树，路由不动，和刚启动一样。回答同 `state` |
| `tree` | 元素过滤（下面） | `elements`：每个无障碍元素的 `id`（accessibilityIdentifier）、`label`、`value`、`traits`、`frame`（屏幕坐标，pt）、`visible` |
| `find` | 元素过滤、`index` | `count`、`element`（没有 `index` 时取第一个在屏幕上的） |
| `wait` | 元素过滤、`gone`、`timeout`（10 s） | 每 100 ms 查一次，等到有（或没了）；超时报错 |
| `perf` | | `counts`：各视图 body 次数、事件数、主线程耗时（要 `-perfCounts YES` 启动，`LiveUpdateTests` 读的同一份） |
| `refresh` | | 向 hub 拉一次（下拉刷新做的事） |
| `activate` | 元素过滤 | 对找到的元素做 VoiceOver 双击做的事（按钮的动作、打开菜单）；`activated` 为 false 说明它不吃（菜单项） |
| `scroll` | `pages`（屏数，负数往上，每屏 0.8 个高度）或 `to`（top / bottom） | 屏幕上最大的那个滚动视图直接设偏移量，没有惯性；`offset`、`contentHeight` |
| `type` | `text`，可带元素过滤（先激活它拿焦点） | 往有焦点的输入框里打字 |
| `dismiss-keyboard` | | 收起键盘 |
| `snapshot` | `path` 或不带（回 base64），`scale` | App 窗口的 PNG（不含状态栏；要和老截图逐像素比用 `jarvisctl.py screenshot`） |
| `navigate` | `scene`（home / assistant / agents / chat / inbox / settings / machines），`session`（`<机器>/<会话>`，chat 和 assistant 要），`machine`（machines 下打开某台），`defaults`（只在建页面时读的开关，如 `{"openWork": true}`），`keep`（不换新的界面树），`coldChats`、`dropChatCache`（同 `reset`） | 落到和对应启动开关一样的页面（`-newAssistant`、`-openAssistant`、`-tab dev`、`-openSession`、`-tab mail`、`-tab settings`、`-openMachines`），默认在一棵新的界面树上（窗口的根换成新的 `RootView`，Agents 页推进去的对话、抽屉、滚动位置都从头来）。回答同 `state` |
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

## 一直在动的画面

`settle` 等“连续几帧不变”，有的页面自己停不下来。2026-11-02 实测（`settle --pixels 0`，4 s，液态玻璃和 BeautifulUI 各一遍）：

| 页面 | 一直在动的是什么 | 怎么办 |
|---|---|---|
| Agents 列表 | “需要你”的圆点在呼吸（`StatusDot`，TimelineView） | `freeze`：打开“减弱动态效果”，圆点不动 |
| BeautifulUI 的对话 | 尊重减弱动态效果的动画 | `freeze` |
| 液态玻璃的对话 | 工作中的步骤里的转圈（UIKit），透过玻璃顶栏也在变 | `freeze`（`layers` strip / pause；减弱动态效果管不到） |
| 机器页 | 实时指标隔几秒来一次，数字和曲线跟着变 | `settle --ignore-label-contains 在线、`（机器卡片）或 `--ignore-rect` |
| 行里的“N 分钟前” | TimelineView 每 30 s 重画 | 一张图里不动；几遍之间不同，身份对比里算噪声 |

- `freeze` 打开模拟器的“减弱动态效果”（私有的 `_AXSSetReduceMotionEnabled`，和打开无障碍树的开关在同一个库里），尊重它的视图
  画成静止的样子；`layers` 管减弱动态效果管不到的 Core Animation 动画：`strip`（默认）把正在跑的动画一次拿掉，图层直接到终点，
  几遍之间像素一样；`pause` 停住窗口的动画时钟，停在哪一相不一定，而且之后开始的转场不会结束，**导航前先 `freeze --off`**。
  `freeze --off` 把减弱动态效果改回原样。
- 打开减弱动态效果后“需要你”的圆点画法不同（没有外圈），所以 freeze 拍的图只和同样 freeze 拍的比，不和 `id-old.sh` 比。
- 实测（每种 3 遍，两两比，任一通道差超过 2 算不同）：液态玻璃的对话不 freeze 时 4 s 内停不下、freeze 后 3/3 停下且 0 差；
  BeautifulUI 的对话不 freeze 3 遍都停不下（几遍之间差 2026–8578 px），freeze 后 3/3 停下、只差顶栏的时间（748–1292 px）；
  Agents 列表剩下的差别只在“N 分钟前”；机器页剩下的是实时指标。机器页（`scenario machines`）要求 6 s 不变：不忽略时要 55–69 帧
  （中间有指标进来），`--ignore-label-contains 在线、` 时 41 帧（最少）。
- `settle` 回答里的 `box` 是最后两帧还不一样的地方（pt），用来找是什么在动。

## 一次启动截完所有场景（身份对比）

`scripts/jarvisctl-scenes/id-cli.py <输出目录> glass:light beautiful:dark …`：启动一次，然后每个“风格 × 深浅 × 场景”
（`scenes-id1.txt` 的 7 个）`online` → `set-style` → `navigate --cold-chats` → `idle` → `settle` → `simctl io screenshot`
（`FREEZE=1`：settle 前先 `freeze`，见上一节）。
`id-old.sh` 是原来的拍法（每张重启 + 启动开关 + 固定 sleep）。`id-judge.py --old <目录>… --cli <目录>…` 判断两种拍法
截的是否一样：规则同 `tools/shotdiff.py`（任一通道差超过 2 算不同，噪声向外扩 6 px），噪声取每种方法自己几遍之间的所有两两差
（一直在动的地方：工作中的圆点、机器曲线，拍的遍数越多覆盖的相位越全）。要 numpy 和 Pillow。

要和冷启动一样，有三件事（都踩过）：
- **新的界面树**：在原树上导航，Agents 页推进去的对话、收件箱顶栏的玻璃底都留着。
- **对话冷加载**（`coldChats`）：已在内存里的对话打开时内容一次到位，冷启动是先出页面、内容后到，两者滚动不一样。
  只清内存不清磁盘：启动本来就读磁盘缓存。
- **按页面自己的计时等**：对话的顶栏闲置 3 s 自己折叠（`ChatView.chromeIdleSeconds`），老方法 7 s 后截一定是折叠的；
  `settle` 1–2 s 就停了，所以对话场景至少等 3.6 s（`id-cli.py` 的 `AT_LEAST`）。

## 读状态（无障碍树）

开着 CLI 时 App 调一次私有的 `_AXSSetAutomationEnabled(1)`（XCUITest 开的就是它；没有辅助技术在跑时 SwiftUI 不建无障碍元素），
`tree` / `find` / `wait` 读的就是 XCUITest 看到的那棵树：`accessibilityIdentifier`、label、frame、traits。

```sh
scripts/jarvisctl.py find --id agents.filter.unread          # {"count": 1, "element": {"label": "未读 47", "frame": [...], ...}}
scripts/jarvisctl.py wait --label-contains 收起过程 --timeout 5
scripts/jarvisctl.py tree --id-prefix agents. --visible
```

元素过滤：`--id`（全等）、`--id-prefix`、`--label`（全等）、`--label-contains`、`--label-prefix`、`--trait`（button / text /
header / selected / search / image / disabled）、`--visible`、`--index`。

- 行的文字是一整个按钮的 label（“完成未看、platform 任务 4、12 分钟前、…”），不像 XCUITest 能再取里面的 staticText：
  按 `--label-contains "、名字、"` 找。
- `visible` 只看 frame 在不在屏幕里：外壳把两页都挂在树里，被盖住的那页也算可见。
- frame 可能是 NaN（布局中的元素），回答里写成 0。

## 动作和截图

```sh
scripts/jarvisctl.py tap --id agents.filter.unread          # 无障碍激活
scripts/jarvisctl.py tap --id shell-agents && scripts/jarvisctl.py tap --label 细列表 --trait button   # 菜单项：自动退回真实触摸
scripts/jarvisctl.py scroll --pages 2 ; scripts/jarvisctl.py scroll --to top
scripts/jarvisctl.py type --trait search --text platform ; scripts/jarvisctl.py dismiss-keyboard
scripts/jarvisctl.py snapshot app.png
```

- `tap` 先做无障碍激活；元素不吃（菜单项）或带 `--hid` 时，等画面停下、重新取 frame，用 `idb ui tap` 在中心点一下真的触摸
  （要装 `idb` 和 `idb_companion`，第一次调用可能要等 companion 起来，脚本会重试一次）。
- 没有惯性、长按、横滑、拖动：要这些的用例（`LiveUpdateTests` 的长按菜单、横滑行，`ChatFollowTests` 的手指滚动）留在 XCUITest。

## 注入状态（mock 的场景）

`scripts/mock_hub.py` 启动时按 `MOCK_*` 环境变量建一次数据，所以以前每种状态（大量 Agent、机器、出错、长对话）都要换 mock
重启 App。`scripts/mock_scenarios.py` 把它当子进程跑在 hub 端口上，另开一个控制端口（默认 hub 端口 - 1，只听回环），
运行中换数据集：停掉子进程、按场景的变量重起、等它应答；App 自己重连。`mock_hub.py` 一行没改，照旧能单独跑。

```sh
python3 scripts/mock_scenarios.py --port 18897 --scenario default &   # 控制端口 18896
scripts/jarvisctl.py launch --hub http://127.0.0.1:18897
scripts/jarvisctl.py scenario agents        # ~120 个 Agent（AgentsListTests 的数据），等 App 重连上
scripts/jarvisctl.py scenario offline       # 没有 hub：顶栏下出现 连接中… / 等待网络
scripts/jarvisctl.py scenario --env MOCK_MACHINES=1,MOCK_LONG_OS=1   # 任意 MOCK_* 组合
scripts/jarvisctl.py scenario               # 现在跑的是哪个
```

场景：default、quiet、agents、churn（`/mock/churn` 按需推送，LiveUpdateTests）、machines、errors（身份截图的出错卡片）、
long（长对话）、big、rich、tasks、offline。和 UI 测试用的是同一套数据，名字见 `mock_scenarios.py` 的 `SCENARIOS`。
mock 自己的 `/mock/reset`、`/mock/seen`、`/mock/churn` 照旧直接请求 hub 端口。

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
