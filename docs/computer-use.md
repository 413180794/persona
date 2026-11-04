# 操控电脑（Cua Driver）

Persona 助手经 [Cua Driver](https://github.com/trycua/cua)（`libs/cua-driver`，MIT，外部安装，不进仓库）操控用户的 Mac：
看窗口和屏幕、点按、输入、截图。做成 hub 的**内置插件 `cua`**（[plugins.md](plugins.md)），**默认关**，打开后再**按机器**逐台打开。

| 块 | issue | 状态 |
| --- | --- | --- |
| 接入：能力发现、助手的 `computer_state` / `computer_run`、截图 | #1182 | 本文「接入」 |
| 手机上一键安装、引导授权 | #1183 | 本文「一键安装」 |
| 实时画面：Agent 操作电脑时在手机、网页上看，必要时接管 | #1184 | 本文「实时画面」 |

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
| 缺权限 | 「Cua Driver 还没有辅助功能、屏幕录制权限」+「去授权」 |
| 请求授权中，或请求过还缺 | 「那台 Mac 上弹出了授权窗口：在它的屏幕上点「允许」」，请求过的再给「再请求一次」 |
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
- **`ScreenViewer`**：iOS 全屏，Mac 是大 sheet。里面一个 WKWebView 打开 hub 上的 `/screen/<机器>#token=…&embed=1`，走 App 连 hub 的同一条路（SSH 隧道时经隧道）。
  - 网页数据用不落盘的存储（`nonPersistent`）。
  - 页面的滚动关掉了，手势都交给远端画面。
- **以后可以做的**：要更好的手势和性能，可以换原生 VNC 库（RoyalVNCKit，MIT，Swift，支持 ARD 认证），经 App 已有的端口转发（`PortForwarder`）连。

## 怎么验证

- **单元测试**：
  - `server/internal/node/computer_test.go`：解析、就绪判断、用假的 cua-driver 脚本跑 `computer.call`（参数、会话名、截图、拒绝的工具）。
  - `server/internal/node/computer_install_test.go`：假的下载源和安装器，验证三个脚本从磁盘运行、参数和环境变量、关统计、接着授权、不像安装器的不运行、取消。
    `PERSONA_CUA_INSTALLER_CHECK=1` 时对真的 cua.ai 只下载、只检查（2026-11-04 通过）。
  - `server/internal/hub/computer_test.go`：插件默认关、按机器开、工具出现和消失、经假 node 调用、确认被拒。
  - `apple/PersonaTests/ComputerTests.swift`：机器的 `computer` 字段、工具的中文名。
- **真机（node 一侧）**：在装好 Cua Driver 的 Mac 上跑 node 包的 `TestCuaEndToEnd`。
  - 打开方式：`PERSONA_CUA_E2E=1`，可选 `PERSONA_CUA_SHOT=<图片路径>`；测试二进制用 `GOOS=darwin go test -c` 编好拷过来。
  - 内容：用 `run_steps` 在计算器里算 6×7，再读窗口，断言有 42，并写出截图。
  - 2026-11-04 实测：就绪、42、230×408 的 JPEG。会动这台 Mac 的计算器。
- **网页**：`web/src/screen/aes.test.ts`（AES 替身对 FIPS-197 和 WebCrypto）、`screen.test.ts`（路径、fragment 里的 token、WebSocket 地址、预填的账号）。
- **看画面端到端**：mock 带 `MOCK_WEB=web/dist`，托管构建好的页面，并把 5900 转到这台 Mac 自己的屏幕共享。
  - `ComputerSectionTests.testTheLiveScreenAsksForTheMacsSignIn` 点「看画面」，App 的 WebView → 页面 → noVNC → mock 转发 → 本机屏幕共享，一直走到它要求登录（ARD）。
  - 登录那一步要那台 Mac 的密码，测试不做。
- **截图**：mock 的 `MOCK_COMPUTER=ready|permissions|locked|missing|off`（[testing.md](testing.md)），配合 `-tab settings -openMachines YES -openMachineDetail macbook`。
- **部署后验收**：
  1. 在「设置 › 插件」打开「操控电脑（Cua）」。
  2. 在那台 Mac 的机器页打开开关。
  3. 在手机上对助手说「在 <机器> 上用计算器算 6×7，把截图发给我」。

  对话里应出现「操控电脑」和「看电脑」两行，点开有截图，显示 42。
