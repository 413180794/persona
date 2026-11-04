# 计划：ego lite 浏览器插件（#1304）

让 Persona 的助手和电脑上的 Agent 用 [ego lite](https://lite.ego.app/) 操作浏览器：打开网页、点按、填表、截图，
在手机上看它在干什么、必要时接管，并把登录状态（cookies）导进去。照 Cua 插件的模式做（[computer-use.md](computer-use.md)），
复用它的机器多选开关（#1285）和实时画面（#1184）。**本文是方案，等用户拍板**（文末「要拍板的」）。

ego lite：Chromium 内核的「agent 浏览器」，Citro Labs 出品；App 免费下载，`ego-browser` CLI 和 skill 在
github.com/citrolabs/ego-lite（MIT，外部代码只读，不进仓库）。以下事实来自它的仓库、官网文档和本机装好的 0.6.2（第 7 版构建）（2026-11-04 调研）。

## ego 是什么样的

- **安装**：
  - 官方 DMG 在 `https://cdn.ego.app/setup/macos/{arm64|x64}/egolite-<推广码>.dmg`。URL 里没有版本号，永远是最新版；也没有公布校验和。
  - App 由 Developer ID「CITRO LABS PTE. LIMITED」（Team `JGQLC6YQYJ`）签名并公证。本机实测：
    - `spctl -a -vv -t exec` 结果是「accepted, Notarized Developer ID」；
    - `stapler validate` 通过；
    - `codesign --verify --deep` 通过，但加 `--strict` 会因为一个 FinderInfo 属性失败。
  - 官方安装脚本会去掉 quarantine，我们不照做。
- **首次设置（onboarding）**：第一次打开时问要不要从 Chrome 等浏览器导入数据（登录状态、cookies、书签、密码、扩展），macOS 可能要钥匙串密码。
  - 做完后注册 `~/.local/bin/ego-browser`，并把 skill 写进 `~/.agents/skills/ego-browser`、`~/.claude/skills`。
  - **只能在那台 Mac 的图形界面里做**，没有无人值守的方式。没做完就调 CLI，它会弹出设置窗口，不会执行。
  - 不要辅助功能、屏幕录制权限。
- **`ego-browser`**：不是一条条命令，而是在 ego 里**跑一段 JS**：`ego-browser nodejs`，脚本从 stdin 或 `-e` 给。脚本里能用的：
  - 任务空间：`taskSpace(name)` 按名字建或接着用一个（是浏览器里的工作区，**不是 macOS 的桌面空间**）；
  - 标签：`task.page("p1")`；
  - 快照：`page.snapshot()`，可访问性树文本，节点带 `[ref=N]`；
  - 操作：`click(@21)`、`fill`、`press`；
  - 等待：`waitFor…`；
  - 截图：`page.screenshot({path})`，PNG。
- **跨调用**：每次调用是一个新的 Node 进程，但任务空间、标签、ref 都留着（`~/.ego-browser/state/space-<id>.json`），上一次快照的 ref 下一次还能用。输出就是脚本 `console.log` 的内容；脚本抛错时之前的输出会丢。
- **CDP**：只能在脚本里用：`page.cdp(method, params)`（Page、Network、Input…），以及 `task.cdp()`（Target、Browser）。没有对外的远程调试端口。
- **从别的浏览器导入**：`ego-browser import --browser chrome [--profile Default] --no-default`，不经图形界面（可能弹钥匙串密码）。
- **数据目录**：`~/Library/Application Support/Citro Labs/ego lite`。同一个 profile 的 cookie 是共用的：用户自己的标签和所有任务空间一起用。
- **隐私**：带 Sentry、PostHog 等统计。隐私政策说可选的「使用分析」可能包含访问过的网址、任务输入和页面内容；默认开不开没写。
- 支持 macOS 13 及以上，Apple 芯片和 Intel 各一个 DMG。

## 1. 插件、发现、安装

- **插件**：内置插件 `ego`，纯声明，贡献新的扩展点 `browser`，**默认关**。配置：
  - `machines`：`format: machine`、`x-os: ["darwin"]`，每台 Mac 一个开关；
  - `confirm`：操作前在手机上确认。
- **node 探测**（`browser.status`，只问打开了的机器），机器页显示每一项：

  | 看什么 | 怎么看 |
  | --- | --- |
  | 装了没有 | `/Applications/ego lite.app`（bundle id `com.citrolabs.ego.lite`）及版本 |
  | 签名对不对 | `codesign` 的 Team 是 `JGQLC6YQYJ` |
  | 首次设置做没做 | `~/.local/bin/ego-browser` 在不在，以及数据目录在不在。**不去调 CLI 试**：没做完时调它会在那台 Mac 上弹窗。这个判断要在本机做完首次设置后实测确认 |
  | 版本 | `ego-browser --version` |

- **「安装 ego lite」**（`browser.install`，和 Cua 的安装一样是后台任务，回报进度）：
  1. 按芯片下载 DMG 到临时目录，用 `hdiutil attach -readonly -nobrowse` 挂载。
  2. **校验**：`codesign --verify --deep`（不加 `--strict`）、Team ID 必须是 `JGQLC6YQYJ`、`spctl -a -t exec` 必须是公证过的。不满足就不装。
  3. 用 `ditto` 装进 `/Applications`，装好后再校验一遍。node 自己下载的文件没有 quarantine 属性（和 `curl` 下的一样），所以不靠系统首次打开时再查：上面的 `spctl` 就是 Gatekeeper 的同一项检查，装前装后各做一次。
  4. 卸载挂载的盘，删掉临时文件。
  - 链接用官方 README 给的那个（里面的推广码是 README 公开的，不是谁的账号），第一步写在 node 里（`egoDMG`）；以后要换再挪进 hub 的配置。
  - 做不到的：**第一次打开**时，macOS 的「从互联网下载的 App」确认和 ego 的首次设置，都要人在那台 Mac 上点。所以装完给「去完成首次设置」：
    - node 执行 `open -a "ego lite"`，在那台 Mac 上打开设置窗口；
    - 机器页显示分步说明；屏幕共享开着时，可以经「VNC」（#1184）远程点完。
- **统计**：首次设置里能关的就关，并在引导里提示用户关掉「使用分析」。能不能用命令关，要等做完首次设置后实测。

## 2. 助手的工具；和 Agent 的分工

**助手**（经 hub）的工具，描述放在 `prompts/mcp/browser/`：
- `browser_run {machine, script, timeout_ms?}`：在那台 Mac 上跑一段 ego 的 JS。
  - node 把脚本包一层再交给 `ego-browser nodejs`（经 stdin）：
    - 先 `taskSpace("persona-<对话 id 前 8 位>")`，所以同一对话总在同一个任务空间里，ref 跨调用有效；
    - 再执行用户的脚本，最后输出一段 JSON 结果；
    - 抛错时也把已输出的内容和错误一起交回，不会因为 ego「抛错就丢输出」而丢掉。
- `browser_look {machine, page?, full?}`：当前标签的快照文本，加截图（`page.screenshot` 写进临时文件，node 读回，转 JPEG）。图片的处理和 Cua 一样：作为图片块交给模型，App 在工具详情里画。
- **写操作**（`browser_run`）：默认放行，hub 日志只记脚本字节数和耗时；插件的「操作前在手机上确认」打开后先弹授权卡片。
- **cookies 不做成工具**：模型拿不到 cookie 原文，也不能导入或导出。

**机器上的 Agent**（Claude Code、Codex）：直接用 ego 首次设置时装好的 skill（`~/.claude/skills/ego-browser`），不经过 hub，也不受插件开关管。插件只管助手这一路。

## 3. 看浏览器

| | 做法 | 能看到什么 | 接管 | 状态 |
| --- | --- | --- | --- | --- |
| a) 实时画面（#1184） | macOS 屏幕共享，整屏 | ego 窗口在前台时看得到；**agent 的任务空间在后台时看不到**（它在浏览器里另一个工作区） | 现成 | 已有，不用新做 |
| b) 定时截图 | node 每秒用 `page.screenshot` 截 agent 那个标签，经 hub 推给手机（像实时画面那样一页） | agent 的那个标签，不管 ego 在不在前台 | 点一下：手机上的坐标换成页面坐标，执行 `Input.dispatchMouseEvent`；或者先 `takeOverTaskSpace` 再操作 | ego 的接口都有，**要实测** |
| c) CDP 投屏 | 在一个常驻的 `ego-browser nodejs` 里跑 `Page.startScreencast`，帧流经 hub 推给手机 | 同 b，帧率更高 | 同 b | 文档没提，**要实测**能不能用 |

**建议**：
- 先做 b：一定能做，开销小，1–2 帧每秒够看 agent 在干什么。
- c 实测能用再换。
- a 照常可用，适合要看整个桌面或点系统窗口的时候。
- 入口和 Cua 一样：机器页和对话里的工具详情都给「看浏览器」。

## 4. 导入 cookies（重点）

cookies 就是登录凭据：拿到就等于登录了。下面每一步都按「凭据」来处理。

### 来源

| 来源 | 怎么做 | 适合 |
| --- | --- | --- |
| a) 那台 Mac 上已有的浏览器 | ego 首次设置时从 Chrome 导入；或之后由机器页「从 Chrome 导入」触发 `ego-browser import --browser chrome --profile <选的> --no-default`（可能在那台 Mac 上弹钥匙串密码）。cookie 不经过 hub 和手机 | 最省事：用户在这台 Mac 的 Chrome 里已经登录过的网站 |
| b) 手机或网页上传 | 选一个文件：Netscape `cookies.txt`，或浏览器扩展导出的 JSON（Cookie-Editor / EditThisCookie 的数组格式），可只勾选部分域名 | 登录状态在别的电脑或手机上的时候 |
| c) 另一台电脑的浏览器 | 那台也装了 ego：在它的任务空间里用 CDP 的 `Network.getAllCookies` 导出指定域名，经 hub 转到目标 Mac。没装 ego 的，就在那台导出 b 的文件 | 多台电脑之间同步某个网站的登录 |
| d) 只导指定域名 | a、b、c 都先列出域名和数量，用户勾选（默认一个不勾），只导勾了的 | 都适用 |

### 存储与安全

- **落点**：只落在目标 Mac 的 ego profile 里。
- **经过 hub 时**（b、c）：
  - 解析后用 hub 的密钥 AES-GCM 加密，暂存在内存或数据库，**写入成功或 10 分钟后就删**，不长期保存；
  - 不写日志（只记「哪台、哪些域名、几条」）、不进仓库、不进助手的上下文，App 推送里也不带原文；
  - 文件内容不在手机上缓存。
- **谁能导**：只有用户本人完全权限的设备（不是只读设备），在 App 或网页里明确操作。助手、Agent、插件进程都不能发起；hub 不提供给模型的接口。
- **查看和撤销**：
  - 机器页「浏览器登录」列出 ego 里有 cookie 的域名（条数、最晚过期时间，**不显示值**），数据来自 node 用 `Network.getAllCookies` 汇总；
  - 每个域名可以「清除」（`Network.deleteCookies`），也可以全部清除；
  - 清除和导入都记进审计（谁、哪台、哪些域名、几条）。
- **同一个 profile**：ego 的 cookie 是整个 profile 共用的，导进去的登录用户自己在 ego 里浏览时也会用到。另一种做法见「要拍板的」第 3 条。

### 写入方式

1. 优先 CDP：
   - node 用一段 `ego-browser nodejs` 脚本，在 Persona 的任务空间里调 `page.cdp("Network.setCookies", {cookies})`。cookie 数据放在一个 0600 的临时文件里，脚本读完就删，不进命令行参数；
   - 按字段换成 CDP 的格式：`name`、`value`、`domain`（hostOnly 去掉开头的点）、`path`、`expires`（秒）、`secure`、`httpOnly`、`sameSite`；过期的跳过；
   - 写完再用 `Network.getCookies` 核对条数；
   - 文档的例子用过 `Network.getAllCookies` 和 `deleteCookies`，`setCookies` 应该也行，**要在做完首次设置后实测**；`Storage.setCookies` 是否可用不清楚。
2. 来源 a 不用这条路：ego 自己的 `import` 直接读 Chrome。
3. 用户正在接管那个任务空间时，ego 不让脚本用 CDP（`EGO_TASK_SPACE_USER_IN_CONTROL`）：提示用户先交还，再导入。

## 第一步做了什么（#1304，2026-11-04）

用户拍板：分步、先定时截图、cookies 导进默认 profile、上传的 cookies 手机解析后经 hub 直转（hub 不落盘）、另一台电脑先不做、统计能关就自动关。第一步做了插件、发现、安装、首次设置引导、就绪和两个助手工具；看浏览器和 cookies 是后面几步。

- **插件 `ego`**：内置、纯声明、默认关，贡献扩展点 `browser`。配置 `machines`（`format: machine`、`x-os: ["darwin"]`，插件页每台 Mac 一个开关）和 `confirm`。机器页的「允许助手用这台的浏览器」是同一个值。
- **node `browser.status`**（插件开着时 hub 问所有在线的 Mac）：
  - 装没装：`/Applications/ego lite.app`，版本取自 Info.plist。
  - 首次设置：`Local State` 的 `ego.welcome_page_shown`，加上 `~/.local/bin/ego-browser` 在不在。导没导入数据：`ego.onboarding_imported_browser_data`。
  - 统计：Chromium 的 `Consent To Send Stats` 文件在，就是开着。
  - 就绪：设置做完后跑一次只读探测（`listTaskSpaces()`，不碰任何页面），结果留一分钟。
  - 最后打 `checked`（hub 按它排先后，见 #1387）。
- **安装**（`browser.install`）：下载 README 给的官方 DMG，只读挂载后校验（`fetchEgo`），装进「应用程序」后再校验一遍。node 下载的文件没有 quarantine 属性，Gatekeeper 的检查由 `spctl` 在装前装后各做一次。
  - 校验：`codesign --verify --deep`；`codesign -dv` 的 TeamIdentifier 是 `JGQLC6YQYJ`；`spctl` 是 accepted 并且 Notarized。不加 `--strict`，会因为一个 FinderInfo 失败。
  - 实测：这台 Mac 上装好的 0.6.2 通过这三项。
- **打开**（`browser.open`）：`open` 那个 App，在那台 Mac 上开始首次设置。机器页每 2 秒问一次，做完就变成就绪。
- **首次设置引导**（机器页）：
  1. 点「打开 ego lite」，或者「VNC」在手机上操作；
  2. 选从 Chrome（或别的浏览器）导入数据，macOS 会多次要钥匙串密码，每次都输；可以跳过；
  3. 做完首次设置（会装上 ego-browser）；
  4. 「再检查一次」。

  实测：除了钥匙串，首次设置不要别的授权（不要辅助功能、屏幕录制）。
- **就绪**是绿色。下面打勾列出：首次设置、导没导入数据、使用统计。
  - 统计开着时提示去关：在 ego 的「设置 › 同步和服务」里关掉「Automatically sends usage statistics and crash reports to Citro Labs」。
  - **没做成自动关**：这一项在设置页里藏在收起的分组里，不是标准的开关组件，用脚本点它不稳；ego 运行时改它的偏好文件又会被覆盖。所以退回到引导里提示。
- **助手的工具**（`prompts/mcp/browser/`）：
  - `browser_run {machine, script}`：在对话自己的任务空间（`persona-<对话 id 前 8 位>`）里跑一段 JS，手边有 `task` 和 `page`（标签 p1）；返回值、地址、标题、截图一起回来。
  - `browser_look {machine, full?}`：快照（带 `[ref=N]`）加截图。
  - 写操作默认放行，hub 日志只记字节数；打开「操作前在手机上确认」后先弹卡片。
- **实测**（`TestEgoEndToEnd`，`PERSONA_EGO_E2E=1`，只碰 example.com）：
  - 状态就绪；
  - 打开 example.com，「看浏览器」拿回快照和 1920×849 的截图；
  - 测试结束关掉任务空间。
  - 打开页面约 0.3 秒，快照加截图约 0.1 秒。
- **踩到的坑**：脚本打印的内容里只要有一个 NUL 字符，ego 就把那次的输出整段丢掉。结果行改用可见的标记 `@@persona-browser@@`。

## 分步

1. **插件和发现**：插件 `ego`，`browser.status`，机器页状态，「去完成首次设置」引导。
2. **安装**：下载、校验、装进「应用程序」，装后再校验。
3. **助手工具**：`browser_run`、`browser_look`，确认开关，日志。
4. **看浏览器**：b 定时截图加点一下接管；入口在机器页和工具详情。
5. **cookies**：列出和清除、从 Chrome 导入（a）、上传文件（b）、指定域名（d）；另一台电脑（c）放最后。

每步一个 PR，动界面的先截图。第 3 步起要那台 Mac 做完首次设置才能实测。

## 要拍板的

1. 照上面的分步做，先做 1–3？
2. 看浏览器先做 b（定时截图），c（CDP 投屏）实测能用再换？
3. cookies 导进哪里：
   - **(i) ego 的默认 profile**：和用户自己的 ego 浏览共用，首次设置从 Chrome 导入的登录 agent 直接能用；
   - **(ii) 单独给 Persona 建一个 profile**：互不影响，但 agent 只有导进来的登录。

   建议 (i)，省事，也符合「方便操作」。
4. 上传的 cookies 在 hub 上最多暂存 10 分钟，写入成功即删，可以吗？还是要求只在手机上解析、经 hub 直接转给 node，不落 hub 的数据库？
5. 从另一台电脑导（c）要不要做，还是先只做 a、b、d？
6. ego 的「使用分析」：引导里提示关掉就行，还是能关就由 node 自动关（要实测有没有开关）？
7. **需要用户在这台 Mac 上完成一次 ego 的首次设置**（导入那一步可以先跳过）：之后我才能实测完成标记、CLI、截图、`Network.setCookies` 和投屏。
