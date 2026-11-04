# 操控电脑（Cua Driver）

Persona 助手经 [Cua Driver](https://github.com/trycua/cua)（`libs/cua-driver`，MIT，外部安装，不进仓库）操控用户的 Mac：
看窗口和屏幕、点按、输入、截图。做成 hub 的**内置插件 `cua`**（[plugins.md](plugins.md)），**默认关**，打开后再**按机器**逐台打开。

| 块 | issue | 状态 |
| --- | --- | --- |
| 接入：能力发现、助手的 `computer_state` / `computer_run`、截图 | #1182 | 本文「接入」 |
| 手机上一键安装、引导授权 | #1183 | 本文「一键安装」 |
| 实时画面：Agent 操作电脑时在手机、网页上看，必要时接管 | #1184 | 本文「实时画面」 |
| 锁屏、防睡眠、按 App 批准、点击音效（参考 Codex 桌面版） | #1507 | 方案已拍板，按顺序在做：本文「锁屏、防睡眠、按 App 批准」 |

## 开关

- **插件 `cua`**：personad 自带的纯声明插件（不跑进程），贡献扩展点 `computer`。在「设置 › 插件」里开关（#1191），
  或在 hub 机器上 `personad plugin enable cua`。默认关。
- **按机器**：插件打开后，每台 Mac 的机器页多一节「操控电脑」，开关「允许助手操控这台电脑」
  （`PUT /api/v1/machines/{mid}/computer`），存在插件配置的 `machines` 里。插件页的「能操控的电脑」是同一个值：只列 Mac，每台一个开关（名字、在线与否、Cua 的状态），即时生效（`format: machine`，[plugins.md](plugins.md)）。
- **操作前确认**：插件配置的 `confirm`（「操作前在手机上确认」），默认关，见「写操作」。

**插件关着时**：
- hub 不向任何 node 问 cua-driver（node 自己从不探测）。
- 机器没有 `computer` 字段，机器页什么都不显示。
- 助手没有 `computer_*` 工具。

**插件开着、某台没打开时**：hub 照样问它的 node（连上时、插件改了时），这台 Mac 的 `computer` 带着状态，只是 `enabled` 为 false：机器页只有开关，插件页的那一行写出 Cua 的状态（「在线 · Cua 没装」）。助手的工具只在至少一台打开后才出现，而且只能操作打开了的机器。

关掉插件或某台的开关时，撤掉的是工具和入口；那台 Mac 上装好的 Cua Driver 不动。

## 两条路

| | 助手（Persona 主对话等） | 电脑上的 Agent（Claude Code、Codex……） |
| --- | --- | --- |
| 怎么调 | hub 的 MCP 工具 `computer_state` / `computer_run` → hub → node 的 `computer.call` → `cua-driver call` | 在那台电脑的 Agent 里直接注册 Cua Driver 的 MCP：`cua-driver mcp-config --client claude --no-skills` 打印配置，`cua-driver mcp` 保持一个会话 |
| 能操作哪台 | 插件打开了的任何一台 Mac | 只有自己所在的那台 |
| 开关、确认、日志 | 本插件的 | Cua Driver 自己的，以及 Agent 自己的工具授权 |

Agent 那条路不经过 hub，本插件不管。

## 接入（#1182）

### 能力发现

hub 问 node 的 `computer.status`。只有插件对这台打开时才问，时机：
- 连上时；
- 打开开关时；
- 机器页打开时（`GET …/computer`）；
- 插件开关或配置改了时。

node 的探测：

| 看什么 | 怎么看 |
| --- | --- |
| 装了没有 | `cua-driver` 在 PATH，或在 `~/.local/bin/cua-driver`（安装器默认放这），或在 `/Applications/CuaDriver.app/Contents/MacOS/`。常驻 node（launchd）的 PATH 很短，所以后两处要单独找 |
| 版本 | `cua-driver --version` |
| daemon | `cua-driver status`（「daemon is running」） |
| 权限 | `cua-driver permissions status --json` 的 `accessibility`、`screen_recording`。这两项是 daemon（`com.trycua.driver`）自己的 TCC 身份；daemon 不在时读不到，记为 `unknown` |
| 桌面会话 | `ioreg -n Root -d1` 的 `IOConsoleUsers`：这个用户的会话在不在前台（`kCGSSessionOnConsoleKey`）、锁没锁屏（`CGSSessionScreenIsLocked` / `IOConsoleLocked`）→ `active` / `locked` / `away` |

结果是 `proto.Computer`。`ready` 表示全部就绪；`note` 是给 App 看的中文，说缺什么、怎么办。机器页显示：
- 就绪：「现在可以操控 · Cua Driver 0.38.0」；
- 没就绪：显示 `note`，例如「Cua Driver 还没有屏幕录制权限：在这台 Mac 上运行 cua-driver permissions grant，在弹出的窗口里允许」。

**锁屏、没有桌面会话**：Cua Driver 靠辅助功能和屏幕录制，都要登录着、解锁着的桌面。
- 锁屏时 `note` 是「这台 Mac 锁屏了，解锁后才能操控」。
- 退出登录或切到了别的用户时，是「这台 Mac 现在没有你登录的桌面……」。
- 重启后还停在 FileVault 解锁界面的 Mac 连网络都没有，机器直接离线。

这些情况下工具照样会去调，失败时把 Cua Driver 的原话回给助手。

### 调用路径

助手的两个工具（描述在 `server/internal/prompts/mcp/computer/`）：

- **`computer_state {machine, app?, query?, screenshot?}`**
  - 给 `app`：先用 `run_script` 里的 `cua.getApp(app)` 找到它最前面的窗口（Cua Driver 自己的查找），再 `get_window_state`：无障碍树（markdown）加截图。
  - 不给：`get_accessibility_tree`（屏幕上的 App 和窗口）加 `get_desktop_state`（整屏截图）。
- **`computer_run {machine, steps | script, app?, observe?, timeout_ms?}`**
  - `steps` → `run_steps`：一串动作，按角色和名字找目标，或用 `element_token`；遇到第一个失败就停。
  - `script` → `run_script`：一段 JS，里面可以循环、判断，最长 120 秒。

**会话**：每个对话的调用都带同一个 Cua Driver 会话名 `persona-<对话 id 前 8 位>`。
- 本机实测（Cua Driver 0.38.0）：
  - `cua-driver call` **不带**会话名时，每次调用是一个新会话，上一次读到的 `element_token` 下一次就失效（`stale_element_token`）。
  - **带同一个**会话名时，跨调用有效：读一次拿到「Clear」的 token，下一次 `call` 点它，计算器归零。
  - 带名字的会话空闲 5 分钟过期。
  - 所以 `computer_state` 读到的行号，可以在后面的 `computer_run` 里用。
- **为什么不在 node 上常驻一个 `cua-driver mcp` 子进程**：会话名已经能保住 token。每次一个 `call` 是无状态的：node 重启、hub 被重新部署、两个对话同时用都不受影响。代价是每次多起一个进程（几十毫秒），和 Cua Driver 操作本身（一次点击约 0.3 秒、找窗口约 1.3 秒）比可以忽略。

**参数和工具**：
- 参数经 stdin 给 `cua-driver call`：没有长度限制，也不出现在进程列表里。
- node 只认 7 个工具：`get_desktop_state`、`get_accessibility_tree`、`get_window_state`、`list_apps`、`list_windows`、`run_script`、`run_steps`。Cua Driver 的设置、更新、扩展、剪贴板都不经这里。

**daemon 没在跑时**：node 先 `open -g -b com.trycua.driver`，经 LaunchServices 在后台打开 CuaDriver.app，不弹窗口。实测 1 秒内起来，权限照旧，然后再调用。

### 截图

- `cua-driver call --screenshot-out-file` 把截图写进临时文件，JSON 里就不再带 base64：计算器窗口那次只有约 600 字节。
- node 读回图片，比 PNG 小时转成 JPEG（透明的圆角垫白），放进 `computer.call` 结果的 `image`，然后删掉临时文件。
- 计算器窗口：230×408，JPEG 约 12 KB；整屏会大一些，Cua Driver 按 `max_image_dimension` 限了长边。
- hub 把图作为 MCP 的图片块和文字一起交给模型：模型要看到像素才能操作，MCP 也没有「给一个链接」的图片。
- **不另建媒体缓存**：图进了助手的 transcript 以后，hub 推给 App 时本来就去掉图片数据（`lazy_images`），App 显示时再按条目取。App 在工具调用下面画出这张图：工具行「看电脑 · MacBook · Calculator」，点开就是截图。
- 截图不写日志。工具返回的 JSON 超过 6 万字节就截断（按字符边界），提示用 `query` 缩小。

### 写操作

`computer_state` 只读。`computer_run` 可能点击、输入、按键、打开或关闭 App。

- **默认放行，记日志**。hub 的日志一行：
  ```
  [macbook] computer_run for 1a2b3c4d: run_steps [click click press_key], 1.2s, ok
  ```
  只记动作名和步数（脚本只记字节数），不记输入的文字、点的是什么。
- **插件配置打开「操作前在手机上确认」**（`confirm`）后，每次 `computer_run` 先在手机上弹授权卡片：「在「MacBook」上依次执行：click、click、press_key」或「运行脚本：<第一行>」。
  - 不批准（或 15 分钟没人理），就告诉助手用户拒绝了，不要重试。
  - 和 `connector_gate` 是同一套卡片（`hub.approve`）。

### 安全和隐私

- 仓库和测试里没有真实的截图、窗口内容、人名。测试用造的 PNG 和假的 cua-driver 脚本。
- 真机端到端测试（下面）的截图只落在测试者指定的本地文件。
- 截图只在助手的 transcript 里（和发给助手的照片一样）。不进 hub 日志，也不进插件审计（`plugin_audit` 只记开关、改了配置，不记值）。
- Cua Driver 自己的 telemetry：一键安装时会关掉（`cua-driver telemetry disable`，#1183）；手动装的由用户自己决定。

## 一键安装（#1183）

插件对一台 Mac 打开后，机器页「操控电脑」按状态给按钮，不再让人去那台 Mac 上敲命令：

| 状态 | 机器页 |
| --- | --- |
| 没装 | 「这台 Mac 上还没有装 Cua Driver」+「安装 Cua Driver」 |
| 正在安装 | 转圈 + 当前步骤 +「取消」 |
| 装失败 | 「没装上：<安装器最后几行>」+「重新安装」 |
| 缺权限，或请求授权中 | 分步引导（#1280）：已允许的打勾，没允许的按顺序编号，用 macOS 26 系统设置里的原名（「系统设置 › 隐私与安全性 › 辅助功能」「录屏与系统录音」，旧版叫「屏幕录制」；应用在列表里叫「Cua Driver」）；缺录屏时多一步「系统提示退出重开或直接访问屏幕时选允许」；最后「回到这里点再检查一次」。按钮「去授权」「再检查一次」，屏幕共享开着时加「看画面」 |
| 就绪 | 「现在可以操控 · Cua Driver 0.38.0」 |

页面在有任务、或请求过授权还在等「允许」时每 2 秒问一次。

### 怎么装（node 的 `computer.install`）

一次只跑一个后台任务，状态随 `computer.status` 的 `job` 回来（`{kind: install|grant, state: running|done|failed|cancelled, step, error}`）：

1. **下载安装器**：Cua 官方的 `install.sh` 只是转交给 `_install-rust.sh`，后者再 source `_install-common.sh`。
   - 三个脚本先全部从 `https://cua.ai/driver/` 下载到一个新的临时目录，**从磁盘运行**：`install.sh` 优先用同目录的 helper，helper 优先 source 同目录的 common。所以运行时不会再从网上取别的代码。
   - 运行前检查（`checkScript`）：两个要执行的是 bash 脚本；被 source 的是 shell 文件；`_install-rust.sh` 里还有对照 SHA256SUMS 的校验。不满足就不运行。
   - Cua 没有给安装脚本本身公布哈希，所以这一步只能做到「来自 cua.ai 的 HTTPS、像是它的安装器」，记在这里，别当成签名校验。
2. **下载并校验 Cua Driver，装进「应用程序」**：`/bin/bash install.sh --no-modify-path`，不改 shell 的 rc 文件。
   - 安装器自己把发布包对照 release 的 SHA256SUMS 校验。机器上有 `cosign` 时再加 `--require-signature`（校验 Sigstore 签名）。
   - 不需要 sudo：用 `ditto` 装进 `/Applications`，所以要是管理员账户；`~/.local/bin/cua-driver` 链到 app 里。
   - 已经装着同一个 app 时会替换它：安装器先停掉 daemon，签名要求不变就保留授权。所以已经装好的机器不给「安装」。
3. **关闭 Cua 的使用统计**：
   - 安装时带 `CUA_DRIVER_RS_TELEMETRY_ENABLED=0`，安装器连「装了一次」都不上报。
   - 装完执行 `cua-driver telemetry disable`，持久关掉。
4. 装好以后 node 立刻开始授权任务。

### 授权（node 的 `computer.grant`）

- node 执行 `cua-driver permissions grant`：它经 LaunchServices 打开 CuaDriver.app，弹出辅助功能、屏幕录制的系统窗口。
- **这一步只能在那台 Mac 的屏幕上点「允许」**，App 替不了点。屏幕共享开着时，机器页给「看画面」（下一节），在手机上的画面里远程点。
- **做不到的**：那台 Mac 的屏幕共享也没开时，第一次授权必须本人在机器前操作。之后都能远程。
- 卸载按钮没做，不是必须的。要卸载就运行 Cua 自己的 `https://cua.ai/driver/uninstall.sh`。

### 接口

`POST /api/v1/machines/{mid}/computer/{install|grant|cancel}` → 200 `Computer`（带 `job`）。
- 插件没对这台打开：404。
- 离线、已经有任务在跑、node 太旧：409 `{"error"}`。
- 「取消」只停安装；授权等的是 Mac 上的系统窗口，那是用户的事。

## 实时画面（#1184）

用途：Agent 或助手操作电脑时，在手机、网页上看那台 Mac 的真实桌面。先只看，再支持接管点按。
Cua Driver 在后台操作，不抢焦点，画面里看到的是真实桌面。它画的 agent cursor 是一个浮在最上层的窗口，屏幕上看得到。

### 两条路

| | a) macOS 自带的屏幕共享（VNC，`127.0.0.1:5900`） | b) node 自己抓屏推流 |
| --- | --- | --- |
| 那台 Mac 要做的 | 开一次「系统设置 › 通用 › 共享 › 屏幕共享」 | 给抓屏的程序屏幕录制权限：可以复用 Cua 的那次授权，让 Cua Driver 截屏，node 轮询 |
| 远程打开 | 要 sudo；据 Apple 的说明，macOS 12.1 起用命令行（`kickstart`）打开的远程管理只能观看、不能控制（没有 MDM 时）。实现时实测 | 跟 Cua 的授权走 |
| 延迟、帧率 | 系统的编码，几十帧，低延迟 | 每帧一次截屏加编码，估计每秒几帧 |
| 接管点按 | 协议自带（键鼠） | 要把点按翻成 Cua Driver 的点击，只能做到点、不能拖得顺 |
| 解锁、登录窗口 | 能看到、能操作（锁屏时也能远程解锁） | 锁屏后截不到桌面 |
| 首次授权的鸡和蛋 | 屏幕共享开着时，Cua 的授权弹窗能经它远程点 | 抓屏本身就要授权，第一次只能在机器前 |

**选 a**：
- 一次性操作最少：开屏幕共享和给 Cua 授权，本人在机器前一次做完；此后装 Cua、授权、解锁都能远程。
- 延迟最低，接管现成。
- b 只在用户不愿开屏幕共享时有意义，第一版不做。

### 连接

只经 hub 的隧道，不对外开新端口：
- 客户端 ⇄ hub：WebSocket `GET /api/v1/machines/{mid}/ports/5900/connect?token=…`，二进制帧，原样 TCP 字节（[ports.md](ports.md) 的端口转发）。
  - 插件对这台 Mac 打开时，hub 常设放行 5900（`portAllowed`），只放 5900，不用 Agent 在听、也不用手动 `open`。
  - 接受 noVNC 可能带的 `binary` 子协议。
  - 只读的设备（手表那种）照旧 403。
- hub ⇄ node：`port.dial`，node 只连本机的 `127.0.0.1:5900`。
- `computer.status` 多报一个 `screen_sharing`：node 连一下 `127.0.0.1:5900`，看对方是不是用 RFB 的版本号打招呼（macOS 是 `RFB 003.889`）。机器页据此决定给「看画面」，还是说明怎么打开屏幕共享。
- 单个流的未确认上限仍是 512 KB：按 50 ms 往返算约 10 MB/s，看画面够用，没有改。

**屏幕共享的局限**：
- 它监听所有网卡，局域网里看得到 5900。这是系统的行为，Persona 不另开端口。要只留回环，需要 sudo 加一条 pf 规则，由用户自己决定。
- 它要那台 Mac 的 macOS 账号和密码（Apple 的 ARD 认证），或单独设的 VNC 密码。
  - 页面每次让用户输入，只发给那台 Mac，不进 hub、不进仓库，页面也不保存。
  - 账号名按机器的家目录（`/Users/<名字>`）预填。

### 页面：`/screen/<机器 id>`（网页，`web/src/screen/`）

- **加载**：网页的入口在路径是 `/screen/…` 时不走登录和侧栏，只动态加载这一页。noVNC（[noVNC](https://github.com/novnc/noVNC) 1.7，MPL-2.0，`@novnc/novnc`）在单独的代码块里，不进主包。
- **token**：
  - App 打开时把自己的设备 token 放在 `#token=…&embed=1` 里：URL 的 fragment 不发给服务器；页面一加载就从地址栏去掉，只放在内存里。
    - 还带上机器的显示名 `name=`（Mac Studio），页面在 hub 回答之前就用它，不显示 id。
    - 每个值只做一次百分号编码（`ScreenViewer.url`，`percentEncodedFragment`）。#1498 之前编了两次，`jd_` 的设备 token 到页面成了 `jd%5F…`，hub 回 invalid token。
    - hub 一侧照常：设备 token 走 `Authorization` 或 WebSocket 的 `?token=`，只答不打字的设备（手表）403。
    - 测试：`ScreenViewerTests`、`screen.test.ts`、`TestTheLiveScreenTakesTheAppsDeviceToken`。最后一个用一个替身「屏幕共享」，确认设备 token 能经转发收到它的版本号，错的 token 和编两次的 token 都在拨号之前被拒。
  - 不像网页的登录链接那样换一个新的浏览器 token（那样每打开一次就多一个设备 token）。
  - 网页里已经登录的用户直接用自己的 token。
- **先问 hub**：有没有这台、在不在线、插件开没开、屏幕共享开没开，各给一句中文说明。然后才连。
- **登录**：屏幕共享要登录时，出一个表单「登录这台 Mac 的屏幕共享」（账号、密码、「连接」）。登录失败就说「账号或密码不对」，可以再试。
- **连上以后**：
  - 默认「只看」（`viewOnly`），顶栏的「只看 / 接管」切换后，键盘和鼠标（触屏）都交给那台 Mac。
  - 画面按窗口缩放（`scaleViewport`），不改那台 Mac 的分辨率。
  - 断了给「重新连接」。
- **没有 `crypto.subtle` 时**：ARD 登录要用 AES 加密账号和密码，而 noVNC 的 AES 用的是浏览器的 `crypto.subtle`。HTTP 打开的页面（hub 的内网地址、App 的 WebView）不是安全上下文，没有 `crypto.subtle`。
  - `aes.ts` 在缺它时补一个只会 AES-CBC 加密的替身。
  - 测试：FIPS-197 的三组标准向量，以及和 WebCrypto 逐字节比对。
  - HTTPS 或 localhost 打开时不补，用浏览器自己的。
- **画面**：Cua Driver 在后台操作，不抢焦点，画面里是真实桌面。它画的 agent cursor 是最上层的一个窗口，看得到。

### App

- **机器页「操控电脑」**：
  - 就绪时给「看画面」。
  - 授权中、或请求过还缺权限时，给突出的「看画面」，在手机上远程点「允许」。
  - 屏幕共享没开时，说明在那台 Mac 的「系统设置 › 通用 › 共享」里打开。
- **对话页**：工具详情下面也有「看画面」（`ScreenEntry`）。出现在两类工具下面，并且要那台 Mac 的插件开着、屏幕共享开着：
  - 助手的 `computer_state` / `computer_run`：对应它点名的那台；
  - 电脑上的 Agent 自己注册的 Cua Driver MCP 工具（`mcp__cua-driver__…`）：对应这个 Agent 所在的那台。
- **画面在抽屉里**（#1343，和终端的抽屉一样）：iOS 从底下铺满，拖顶上的把手往下或点把手关掉，没拖够弹回去；Mac 是大 sheet。
  - 顶上：机器名、连接的阶段（检查 / 连接 / 要登录 / 已连上 / 已断开），助手或 Agent 正在操作这台时多一行「正在：点 AC → 6 → × → 7 → =」。
  - 底下：「只看 / 接管」，经 WebView 的桥（`personaScreen.setControl`）切网页的 `viewOnly`；网页在 `embed=1` 时不画自己的顶栏，阶段经 `webkit.messageHandlers.screen` 报给 App。
  - 里面一个 WKWebView 打开 hub 上的 `/screen/<机器>#token=…&embed=1`，走 App 连 hub 的同一条路（SSH 隧道时经隧道）。网页数据用不落盘的存储（`nonPersistent`），页面的滚动关掉了，手势都交给远端画面。
  - 只有一个抽屉，挂在 `RootView`（`AppModel.screenOpen`）：从哪里打开都是同一个。
- **从哪里打开**：
  - **正在操作的条**：对话里最近一次操作电脑的工具（助手的 `computer_*`，Agent 的 `mcp__cua-driver__*`）还在跑、或刚结束 6 秒内，输入框上面的任务条最前面多一个「正在操作 MacBook · 点 …」（`ComputerActivityPill`），点它打开那台的画面。
  - **··· 菜单**：助手顶栏和 Agent 对话页的 ··· 里有「看画面 · <机器>」（`ScreenMenu`），列出插件开着、屏幕共享开着的 Mac，这个对话最近操作的那台排第一。
  - 机器页「操控电脑」和工具详情里的「看画面」（`ScreenEntry`）也打开这个抽屉。
- **工具写成人话**（#1323，`ComputerSteps`）：工具详情先是一行步骤（连续的点按合成「点 AC → 6 → × → 7 → =」，计算器的按键名换成符号），再是截图和「看画面」；原来的 JSON 收在「原始参数」里，点开才看。标题里的机器用它的名字（MacBook），按 id 或名字认（和 hub 一样），认不出才照工具里写的；放不下时截掉尾部（文件路径照旧截中间）。
- **以后可以做的**：要更好的手势和性能，可以换原生 VNC 库（RoyalVNCKit，MIT，Swift，支持 ARD 认证），经 App 已有的端口转发（`PortForwarder`）连。

## 锁屏、防睡眠、按 App 批准（方案，#1507，已拍板）

参考 Codex 桌面版的 Computer Use 设置。主会话只读看了它安装包里的界面文字，没拿代码，看到的设置有：
- 锁屏时也能用（Locked use）；
- 点击音效（无 / 只前台 / 前台和后台）；
- 按 App 的批准清单；
- Office 三件套的 live control；
- 信息（Messages）的读 / 发权限。

### 现在怎样

- **锁屏时用不了**：
  - node 从 `IOConsoleUsers` 读到 `locked`，机器页写「这台 Mac 锁屏了，解锁后才能操控」，助手的工具也这么回。
  - Cua Driver 0.37 起在锁屏时每条通道都拒绝，回 `screen_locked`（[trycua/cua#5483](https://github.com/trycua/cua/pull/5483)）。
  - 实测报告（[trycua/cua#1745](https://github.com/trycua/cua/issues/1745)）：锁屏下辅助功能只够得着菜单栏，窗口截图是 0×0。
  - 锁屏时真的键鼠都落在锁屏上，所以拒绝是对的。
- **macOS 什么时候自己锁**：
  - 屏幕保护程序启动或显示器空闲变暗时就锁（`kLWLockFromDisplayDim`），「需要密码」设成立即的时候尤其如此。
  - 也就是说，Agent 在操作、或者用户在手机上只看不点的时候，Mac 都可能因为「空闲」锁上。
- **Cua Driver 已经做了一半**：
  - 每次工具调用后，它持有一个 `PreventUserIdleDisplaySleep` 断言，最后一次调用后再持续 10 分钟。环境变量 `CUA_DRIVER_KEEP_DISPLAY_AWAKE=<秒>` 可以改时长，`0` 关掉。
  - 它不会叫醒已经睡着的显示器，也过不了锁。
  - 这只管经 cua-driver 的调用。看画面、Agent 想得久（超过 10 分钟没调用）的时候不管。

### a) 锁屏时也能用

三条路，推荐 1 + 2，不做 3。

1. **不让它锁**：就是下面的 b。最简单，随时可撤。代价是不锁的那段时间里，Mac 对坐在它前面的人是敞开的。
2. **解锁入口：用户自己解**：
   - 那台 Mac 锁着时，机器页、抽屉和助手的提示里给「解锁」，打开实时画面。屏幕共享在锁屏下能看能输，用户在锁屏上输密码。现在就能用，只差一个入口。
   - 和 #1322（在这台手机上记住 Mac 的登录密码，存设备钥匙串、要 Face ID）一起做之后，可以改成一键：手机取出密码，经 VNC 的键盘事件打进锁屏的密码框。
     - 密码只过隧道，不在 hub 落盘，不进日志、不进仓库。
     - hub 日志只记「谁、什么时候、解了哪台」。
   - **安全边界**：
     - 手机加 Face ID 就能解开那台 Mac。所以默认关、按 Mac 开，打开时在手机上确认一次。
     - 解开以后不会自动再锁。抽屉里提示「用完记得锁上」，并给一个「锁上」（`pmset displaysleepnow`，显示器一睡就锁）。
   - **要用户在 Mac 上做一次的**：打开屏幕共享，已经是看画面的前提。没有别的了。
   - **管不了的**：
     - 重启后 FileVault 的开机解锁：屏幕共享在那一步连不上。
     - 合盖睡着的 MacBook，node 也是断的。
3. **像 Codex 那样的授权插件**：
   - Codex 装一个 Apple authorization plug-in，参与 macOS 的解锁流程。只在它自己的、进行中的操作里临时解锁，盖住所有屏幕，一有本地键鼠输入就重新锁上（[OpenAI 的说明](https://developers.openai.com/codex/app/computer-use)）。
   - 代价：
     - 要管理员权限安装，改系统的认证规则（`authorizationdb`）。
     - 写错了可能锁不住或解不开。插件本身也成了攻击面，hub 或手机被拿下就等于能解开 Mac。
     - 合盖时也不行。
   - Cua 有同样的打算（#1745 第二阶段：`cua-driver unlock-screen --purpose … --duration 30s`），还没有实现。
   - **不做**。等 Cua 出了这个能力，再评估接它的，不自己写系统插件。

### b) 防睡眠、不自动锁屏（插件 `cua` 的设置，按机器两个开关）

都用 `caffeinate`，不改系统设置（不碰「锁定屏幕」里的时间和「需要密码」）。关掉或 node 退出就恢复原样。

用到的参数：
- `-d`：显示器不睡；
- `-i`：系统不因空闲睡眠；
- `-m`：磁盘不睡；
- `-s`：系统不睡，只在接电源时有效；
- `-u`：声明用户在场，显示器关着就点亮；
- `-t`：时长（秒）；
- `-w <pid>`：那个进程退出就撤。

- **「Agent 操作期间保持唤醒」**：默认开。
  - 什么时候持有 `caffeinate -dimsu -t 600`：
    - 助手的 `computer_*` 调用；
    - 屏幕转发（5900）开着的时候，就是有人在看画面。只看不点不算本地活动，不持有的话看着看着就锁了；
    - 那台 Mac 上有 Agent 正在用自己的 Cua MCP。node 从会话里看得到 `mcp__cua-driver__*` 的调用。
  - 每次有新操作就重新开始计时；最后一次之后 10 分钟，或者看画面的流关掉，就撤掉。
  - node 只留一个 caffeinate 进程，带 `-w <node 的 pid>`，node 没了它也跟着没。
  - 和 Cua 自己的断言叠在一起也没关系。
- **「插件开着时不自动锁屏」**：默认关，打开时在手机上确认。
  - 插件对这台开着、开关也开着时，node 一直持有 `caffeinate -dims -w <node 的 pid>`。
  - 没有 `-u`，所以不会去点亮已经关了的显示器。
  - 什么时候撤：开关或插件关掉、node 退出、node 和 hub 断开 30 分钟（离线时不替用户一直开着）。
  - **管不了的**：
    - 手动锁屏（Control-Command-Q、触发角）；
    - 合盖；
    - 快速切换用户；
    - 公司管理的 Mac 上强制的屏保锁定策略。以实测为准。
  - **安全边界**：这台 Mac 一直不锁，旁边的人能直接用它。机器页和插件页把这一句写在开关下面；hub 日志记开关的时间。
  - **和 Codex 的区别**：Codex 是锁着也能用（要授权插件），这里是不让它锁。实现简单、能完全撤回，但不锁的那段时间 Mac 是敞开的。

### c) 按 App 批准

- **怎么问**：
  - 插件 `cua` 的设置多一项「已允许的 App」，按机器记。
  - 助手第一次在某台 Mac 上操作某个 App 时，手机上问一次：「允许助手操作 MacBook 上的「计算器」吗？」选项是「允许这一次 / 一直允许 / 拒绝」。
  - 选「一直允许」的记进插件配置，设置页能逐个移除。
- **怎么认 App**：
  - `computer_run` 的每一步都写着 `app`，`computer_state` 也有 `app`。
  - 脚本只认写死的 `cua.getApp("…")`。里面有算出来的名字时，认不全，整段脚本每次都问。
  - 名字经 Cua 的 `list_apps` 换成 bundle id 再记，同名的不同 App 不会混。
- **默认不让碰、每次都问的**：终端、系统设置、钥匙串访问、密码管理器、Persona 自己。参考 Codex 不碰终端、管理员提权和安全提示。
- **和现在的「操作前在手机上确认」**：那个是每次都问，这个是每个 App 问一次。两个都开时，以每次都问为准。
- **Agent 自己的 Cua MCP**（`mcp__cua-driver__*`）：
  - 这些调用不经过 hub，走 Agent 的工具审批（hook 把卡片送到手机）。
  - 第二步再接上：已经允许的 App 自动放行，没允许的照样出卡片。

### d) 小配置

- **点击音效**：无 / 只前台点击 / 前台和后台点击，默认无。
  - Cua 的点按分两种：前台是真的鼠标事件，后台是按进程投递，不动光标。
  - node 每点一次放一下系统音效（`afplay /System/Library/Sounds/Tink.aiff`），坐在 Mac 旁边的人听得到 Agent 在动。
- **显示 Agent 的光标**：默认开，看画面时知道它点在哪。
  - 关掉：用 `cua-driver serve --no-overlay` 重启 Cua 的后台进程。有报告说光标浮层空闲时也占不少 CPU。
  - 也能用 `set_agent_cursor_enabled` 按会话关。
- **Office、信息（Messages）的单独权限**：不做。这些我们没有专门的接入；要读信息走以后的 iMessage 连接器（[plan-connectors.md](plan-connectors.md)）。

### 顺序

1. **b**：「Agent 操作期间保持唤醒」，含看画面期间。锁着时给「解锁」入口，打开实时画面。小，先做。
2. **c**：按 App 批准，先管助手，再管 Agent 的 Cua MCP。
3. **a-2**：一键解锁，和 #1322 一起。
4. **b**：「插件开着时不自动锁屏」，等安全边界拍板后再做。
5. **d**：点击音效、光标开关。

每一步都在这台 Mac 上实测，开关、锁屏、恢复都要验证。锁屏的实测要用户在场：锁上以后得有人输密码。

### 拍板结果（2026-11-04，用户：都按建议）

1. **锁屏**：只做「不让它锁」和「解锁入口」，不做系统授权插件。
2. **「插件开着时不自动锁屏」**：默认关，打开时在手机上确认；node 和 hub 断开 30 分钟后自动撤掉。
3. **按 App 批准**：默认开。每个 App 第一次操作时问一次，之后记住，设置里可以移除。
4. **默认拒绝清单**：终端、系统设置、钥匙串访问、密码管理器、Persona，就这些。
5. **一键解锁**：做。Mac 密码存在手机钥匙串，经 VNC 打进锁屏的密码框。默认关、按 Mac 开，和 #1322「在这台手机上记住」一起做。
6. **ego**：插件 `ego` 对那台 Mac 开着时，也放行那台的屏幕共享（看画面），页面也认 ego。

按上面的「顺序」做，每块一个 PR。第 6 条和 b 一起，或者单独一个小 PR 先合。

## 怎么验证

- **单元测试**：
  - `server/internal/node/computer_test.go`：解析、就绪判断、用假的 cua-driver 脚本跑 `computer.call`（参数、会话名、截图、拒绝的工具）。
  - `server/internal/node/computer_install_test.go`：假的下载源和安装器，验证三个脚本从磁盘运行、参数和环境变量、关统计、接着授权、不像安装器的不运行、取消。
    `PERSONA_CUA_INSTALLER_CHECK=1` 时对真的 cua.ai 只下载、只检查（2026-11-04 通过）。
  - `server/internal/hub/computer_test.go`：插件默认关、按机器开、工具出现和消失、经假 node 调用、确认被拒。
  - `apple/PersonaTests/ComputerTests.swift`：机器的 `computer` 字段、工具的中文名、步骤写成人话、最近一次操作在结果回来前算在跑。
- **真机（node 一侧）**：在装好 Cua Driver 的 Mac 上跑 node 包的 `TestCuaEndToEnd`。
  - 打开方式：`PERSONA_CUA_E2E=1`，可选 `PERSONA_CUA_SHOT=<图片路径>`；测试二进制用 `GOOS=darwin go test -c` 编好拷过来。
  - 内容：用 `run_steps` 在计算器里算 6×7，再读窗口，断言有 42，并写出截图。
  - 2026-11-04 实测：就绪、42、230×408 的 JPEG。会动这台 Mac 的计算器。
- **网页**：`web/src/screen/aes.test.ts`（AES 替身对 FIPS-197 和 WebCrypto）、`screen.test.ts`（路径、fragment 里的 token、WebSocket 地址、预填的账号）。
- **看画面端到端**：mock 带 `MOCK_WEB=web/dist`，托管构建好的页面，并把 5900 转到这台 Mac 自己的屏幕共享。
  - `ComputerSectionTests.testTheLiveScreenAsksForTheMacsSignIn` 点「看画面」，App 的 WebView → 页面 → noVNC → mock 转发 → 本机屏幕共享，一直走到它要求登录（ARD）。
  - 登录那一步要那台 Mac 的密码，测试不做。
  - `ScreenDrawerTests`（mock 加 `MOCK_COMPUTER_OPERATING=1`，助手和 Agent 的对话里各有一个在跑的操作）：点助手、Agent 的「正在操作」条和 ··· 菜单打开抽屉，往下拖关掉。
- **截图**：mock 的 `MOCK_COMPUTER=ready|permissions|locked|missing|off`（[testing.md](testing.md)），配合 `-tab settings -openMachines YES -openMachineDetail macbook`。
- **部署后验收**：
  1. 在「设置 › 插件」打开「操控电脑（Cua）」。
  2. 在那台 Mac 的机器页打开开关。
  3. 在手机上对助手说「在 <机器> 上用计算器算 6×7，把截图发给我」。

  对话里应出现「操控电脑」和「看电脑」两行，点开有截图，显示 42。
