# 经验：网页界面与交互

## 弹出菜单

- React 事件的 `currentTarget` 只在事件处理期间有效。不要在 `setState` 的函数更新器里读取 `e.currentTarget`：更新器延后执行时会读到 `null`，菜单第一次能打开、关闭后再点却打不开（2026-11-02，同空间菜单的浏览器验证发现）。先在处理器内保存目标元素，再让更新器使用保存的值；回归覆盖延后执行更新器和连续打开、关闭、再打开。
- 给菜单加状态标签后，标题保留单行截断、标签不收缩，并为长标题提供完整提示。菜单的实际宽度先限制到视口减两侧留白，再按实际宽度计算位置，不能只移动左边界：桌面宽度固定的菜单会溢出窄屏。
- 浏览器测试关闭菜单时等节点移除（`detached`），不只等 `hidden`；菜单还在隐藏的初始定位阶段时，`hidden` 会提前通过。断言菜单内容前等待具体行和状态文字出现，避免把渲染尚未完成误报成缺状态。
- 卡片为了画圆角带 `overflow-hidden`（设置页的 `Group`），卡片里的行往外弹的浮层会被裁掉：最后一行的「确认删除」「确认撤销」只露出一条边，点按钮中间点到的是卡片下面的说明（2026-11-04，#1504）。`z-index` 挡不住裁切。行上的操作在行内确认（`RowConfirm`），真要浮层就放到卡片外（portal）；验证要量最后一行，只看第一行看不出来。

## 新建会话与多模态

- 2026-11-02 新建 Agent 不该把跳转放在创建 HTTP 请求之后。先生成本地临时 ID、保留文字和附件、关闭弹窗并展示本地会话；网络创建、图片编码、文件上传在后续任务里做。临时 ID 不发给后端取 transcript；真实会话创建成功后才调用 `/messages`。
- 普通 Agent 创建用 `async:true` 与 `name_hint`，首条内容只在 `/messages` 发一次；创建的 `prompt` 只有字符串，不能靠额外 `images` 字段实现多模态。消息重试保持同一 `client_msg_id`，已创建的会话不可再创建；创建没有幂等，结果不确定要让用户先检查列表。
- 统一默认启动器可能是 Codex，不能按启动器名字猜 Agent。仅在创建明确返回 `codex_needs_prompt` 时走原来的文字同步创建；首条附件不能偷偷变成后续 steer 或伪造初始化消息。失败内容要可修改、移除附件并重试。
- 图片单个 8 MB 仍可能多张累计超过 `/messages` 的 32 MiB JSON 上限；发送前考虑 base64 膨胀检查总量。乐观气泡需要把图一起带过真实会话跳转，收到 transcript 的文字和图片后再清理。
- 浏览器回归用延后返回的创建 / 发送请求，证明弹窗已关闭、本地会话已打开但后端尚未完成；图片选择、移除、草稿冲突同 ID 重试、切页不抢焦点都要检查。截图里的图片不能用透明像素占位；验证 `naturalWidth` 并用可见的演示图。

## 管理界面的独立入口（web/admin，docs/admin.md）

- Vite 多页面：`build.rollupOptions.input` 加 `admin/index.html` 后，dev server 的 SPA 回退仍然给根 `index.html`，`/admin/machines`
  这类前端路由会打开普通网页；`vite.config.ts` 里一个只在 dev 生效的小插件把 `/admin/*` 改写到 `/admin/index.html`。
- 只给管理界面用的代码要输出到 `admin/assets/`（`entryFileNames` / `chunkFileNames` 按模块路径分），hub 才能整个前缀按访问范围拦；
  构建后检查 `dist/assets/` 里没有 `/api/v1/admin` 之类的字符串。
- 无头 Chrome（`--headless=new`）的窗口最小 500 宽，拍 390 宽的窄屏用 CDP 的 `Emulation.setDeviceMetricsOverride`；深色用
  `Emulation.setEmulatedMedia` 设 `prefers-color-scheme`。

## 对齐手机端这一轮（#297，2026-11-02）

### 依赖和开发环境

- 这台开发机的 npm 默认源是公司内部的镜像：`npm i` 会把内部域名写进 `package-lock.json` 的 `resolved`（隐私扫描要拦的东西）。加依赖一律带
  `--registry=https://registry.npmjs.org/`，加完 `grep -o '"resolved": "https://[^/]*' web/package-lock.json | sort | uniq -c` 只能有 registry.npmjs.org。
- xterm 只在打开终端时动态 import：Vite dev 第一次遇到它时才预构建依赖、重新载入页面，那一次面板卡在「正在连接终端」。`vite.config.ts` 的 `optimizeDeps.include` 先把它预构建好（只影响 dev，构建产物没事）。

### xterm.js

- 回滚行数为 0 时 xterm 把滚轮翻成 ↑ / ↓ 经 `onData` 发出去：要 `attachCustomWheelEventHandler` 自己处理滚轮，而且接管、只看都返回 false。
- 自定义按键处理返回 false 时 xterm 不阻止冒泡，要自己 `preventDefault` + `stopPropagation`（Shift+Enter）；xterm 自己处理的键（含 Esc）已经阻止冒泡，
  所以终端里按 Esc 不会触发对话页的「Esc 中断」。
- xterm 6 的滚动层把画面裁成外层元素的大小：只看时把 `.xterm` 的宽高设成 `.xterm-screen` 的，外层 `overflow:auto` 才能横向滚；FitAddon 量父元素，父元素保持面板大小，接管时算出的行列才对。
- DOM 渲染器不会把中文字撑满两格，中文字之间有空隙。

### 渲染和组件

- react-markdown 不渲染原始 HTML（README 里的 `<img>`、飞书导出表格里的 `<br>` 会原样显示成字），默认的 URL 过滤会把 `data:` / `blob:` / `jarvis:` 链接清成空：
  先把 HTML 转成 Markdown / 占位字符，自定义 `urlTransform` 放行需要的协议。
- `pop-in` 这类关键帧动画的是 `transform`，会盖掉行内的 `transform: translate(...)`：浮层定位用 CSS 的 `translate` 属性。
- 只渲染看得见的行的列表，行数变少（JSON 从格式化切回原文）时窗口会落后一帧：先把窗口截到行数以内，否则越界。
- 只在悬停时出现的行内按钮在触屏上要整个藏掉（`@media (hover:hover)`），否则是看不见、一碰就触发的区域。
- 两栏的页（收件箱、知识库）按页面自己的宽度切单栏（Tailwind v4 的 `@container` / `@4xl:`），不按窗口：右侧面板一开，主区变窄，按窗口判断会挤坏。
- SVG 曲线按真实像素宽度画（ResizeObserver），不用 `preserveAspectRatio="none"`：坐标轴上的字会被拉伸。
- 订阅模块级 store、第一次拉取要放 `useEffect`，不能在 render 里做：React 会报「渲染中更新别的组件」。
- `JSON.parse` 会把像整数的键排到最前、让 64 位数字丢精度：只是给人看的格式化，就只重新缩进，不解析值。

### 和 hub 打交道

- `api()` 没显式传 token 时任何 401 都会把浏览器登出；lark-connector 掉了飞书登录也回 401。`/lark/raw`、`/lark/moments` 这类代理的请求显式传 `token: getToken()`。
- `PUT /assistant/tasks/{id}` 整体替换字段（不带 `session_id` 就清空），连只切开关也要带完整任务；Go 的零时间是 `0001-01-01T00:00:00Z`，`at` 没有就省掉，不能发空串。
- 文件分页：后续页用最初请求的 `path` + `cwd` 取，不能用服务端规范化后的 `path`；第一页的 `truncated` 一直是 true，「只显示了开头」看第一页有没有 `next`。
- 收件箱的件 id 含 `:`；「飞书」筛选看 `source`，不看 `account`（真实 hub 的飞书条目 account 是 `feishu`，假 hub 是 `lark`）。
- 邮件 HTML 的 iframe：`sandbox="allow-same-origin"`、不给 `allow-scripts`，能量高度、拦链接，脚本跑不了。
- 知识库打标传空的标签数组 hub 不改（`omitempty`），清不空标签，iOS 一样。
- 别的页面写的 `<jarvis-context>` 属性按 `kind`、`title`、`url` 的顺序：iOS 的解析正则要求三个都在、按这个顺序。

### 浏览器里验证

- 截图和点按用 Playwright（`playwright-core` + 本机 Chrome，`channel: "chrome"`），脚本放仓库外。`page.routeWebSocket` 能改 WS 消息：
  假 hub 没有互联字段时，把 `machines` 推送和 `/machines` 加上两个 `hub_id` 就能看互联的样子，不用改 `mock_hub.py`。
- 多人共用一个假 hub 时，写接口（`/reply`、`/doc/status`、`/reconnect`、改 Claude 设置、打标）会改共享状态：用 `page.route` 拦下来验证请求体，或者测完改回去。
  假 hub 的 `/reconnect` 会真的把离线机器改成在线。
- `page.evaluate(() => import("/src/lib/x.ts"))` 拿到的不一定是页面在用的模块实例（改过文件后 Vite 给导入方加 `?t=`）：从 `performance.getEntriesByType("resource")` 找带 `?t=` 的地址。

## 补齐差距第二轮（#433，2026-11-03）

六条车道并行（对话展示、消息操作、Agents 列表和机器页、终端、助手、收件箱和翻译），合进一个分支后统一在浏览器里验证。

### 合并和验证

- 并行改同一个对话页，冲突集中在 `ChatView.tsx` 的几处：发送中的气泡（本地 `pending`、排队消息要藏掉的、跨页的 `outbox`）各车道各加了一份判断。合并时统一成一个
  「还在路上的」数（`outgoing` = 没进队列的本地气泡 + 没失败的 outbox），「最后一行是不是 live」「能不能重试」都看它，不然排队消息会让最后一段一直转圈。
- 新建 Agent 带预设打开时，「当前电脑不在线就选第一台在线的」那个 effect 和预设的 `setMid` 在同一次提交里先后执行，后一个赢：预设没生效、还看不出来（默认就是第一台）。
  预设待生效时那个 effect 要让路。
- 查看器里的图按 `max-w-full max-h-full` 放，小图（截图缩略、36 px 的图标）就比对话里的缩略图还小：按容器大小算适配尺寸（`fitRect`，允许放大），双击才看原始大小。
- 浏览器验证用 Playwright 的 `locator.click({ button: "right" })` 能测右键菜单；`text=` 选择器会先命中侧边栏里同名的会话（「韩梅梅」），限定在 `main` 里找。
- 假 hub 的终端推送线程在浏览器关掉后写已关闭的 socket 会打一段 `Bad file descriptor` 的栈，同一时刻代理过去的请求可能回 500：看到偶发的 500 先看是不是这个，不是网页的问题。

### 对话页

- 闭包里改的 `let` 变量，TypeScript 在外面的循环里仍按初值收窄（`let x: T | null = null` 在循环里一直是 `null`，`x.id` 报 `never`）：放进一个对象（`const last = { raw: null as T | null }`）。
- `encodeURIComponent` 会把子 Agent 会话 id 里的 `@` 编成 `%40`：真实 hub（Go 的 `PathValue`）会解码，假 hub 的路由要自己 `unquote`。
- 列表每行都传同一个 `now` / `streaming` 会让每秒 / 每个 delta 所有行的 `memo` 都失效：只给正在进行的那一行传。
- 触屏长按要自己计时（iOS Safari 长按不发 `contextmenu`，安卓约 500 ms 后再发一个）：`pointerType === "touch"` 的 `pointerdown` 起 450 ms 计时，移动超过 8 px 取消，开过之后 1 秒内的
  `contextmenu` 只 `preventDefault`。消息加 `select-none` + `-webkit-touch-callout:none`，所以菜单里要有「选择文本」。
- `Menu` 只认元素做锚点：在点击处临时放一个 `position:fixed`、0×0 的 span，关的时候删掉（别在 setState 的更新器里做删除这种副作用）。
- 自己的消息气泡下面原来没有操作行，加一行会改整段对话的间距：悬停条用 `absolute right-full` 放在气泡左边（本来就有 12% 的留白）。
- 浏览器不让顶层导航到 `data:` 地址：先 `fetch(dataURL).blob()` 再 `URL.createObjectURL`。写图片到剪贴板（`ClipboardItem`）只在安全上下文里可用，明文 HTTP 的 hub 上要藏掉按钮。
- 查看器、编辑器里的 Esc 要在 window 的捕获阶段处理并 `stopPropagation`，否则对话页的「Esc 中断」会一起触发。

### 侧边栏、机器页

- 链接行里不能嵌按钮：行的 ⋯ 做成 `<a>` 的兄弟节点，绝对定位、`group-hover/row:flex`（Tailwind v4 的 `group-hover` 本来就只在 `@media (hover:hover)` 生效）；悬停时行尾的提示和角标
  `group-hover/row:invisible` 让开，否则被 ⋯ 盖住一半。右键 / 菜单键走 `<a>` 的 `onContextMenu`。
- `Icon` 是 `aria-hidden` 的、不收 `aria-label`（TS 不检查带连字符的 JSX 属性，传了也不报错）：要读出来就在外面包 `<span title>` + `sr-only` 文字。
- 侧边栏宽约 280 px，四个筛选胶囊（带圆点和件数）按 `px-2 gap-1` 会折成两行：收紧到 `px-[7px] gap-0.5`。
- 曲线图上的参考线标签（负载图的「N 核」）画在线条下面会被数据线穿过：画在线条之后，加一圈页面底色的描边（`paint-order: stroke`）。

### 终端（xterm.js）

- DOM 渲染器的中文空隙来自 `DomRendererRowFactory`：`spacing = width * cellWidth - 量出来的字宽`，写成 `letter-spacing`。让中文字体量出来正好两格就没有空隙：`FontFace` 的 `size-adjust`
  只缩放这一个回退字体，`unicode-range` 限定中日韩，不影响英文的格宽。
- xterm 6 的公开 API 没有「光标是否隐藏」（DECTCEM）：页面自己画屏幕时从 `_core.coreService.isCursorHidden` 读，读不到就不画光标。
- 读历史时要停住画面就不能往 xterm 写（没法「先不显示」），帧要自己攒着；`FrameOrder` 照常前进，攒太多就丢掉、回来时不带 epoch 整帧重订，否则会从没画过的 seq 往后接。
- 在 xterm 上方插入历史后要自己保持滚动位置，并关掉浏览器的滚动锚定（`overflow-anchor: none`），否则两边都加一次会跳。

### 助手、收件箱

- 动的形象就是 APNG：`<img>` 自己会播，但 `prefers-reduced-motion` 管不到它（不是 CSS 动画），要停只能画到 canvas 上（`drawImage` 取第一帧）。
- 别的页面替会话发消息，等 HTTP 返回再跳的话，跳过去以后要等记录推回来才看到这条：先跳，气泡放 store 里（按 `mid/sid`）。新开的助手对话 `POST /assistant/conversations`
  要等对话起来才返回（hub 超时 110 秒），这一段只能在原页面转圈；返回后把提示词记成「已送达」的气泡再跳。
- hub 的 `/mail/send` 的 `references` 是**字符串**（`mail.SendRequest`），iOS 的 `SendMail.references` 是 `[String]`：iOS 带 References 的手动回复会被 hub 以 400 拒掉（看代码推断，没实测）。网页按字符串发。
- 可见即读要有「重新看一眼」的时机：IntersectionObserver 只在交叉比例跨过阈值时回调，页面在后台 / 有对话框时进来的消息，回到前台、对话框关掉后不会再回调。记下当前在视口里的位置，
  `visibilitychange`、`<body>` 子节点变化（对话框是 portal 进 body 的）时再按门禁看一遍；阈值要密（每 0.1），比列表还高的消息才能达到「占列表一半高」。
- 话题回复（展开的 thread）里的消息位置不是会话的位置：只给列表顶层的消息加 `data-seen-pos`。

## 待发气泡和对话记录（2026-11-03，#950）

- 发出去的消息在对话记录里认，不能按全等、不看时间：最近几条里有一条同样的「继续」，新发的气泡立刻被收掉（随后发送失败的话失败也看不到），两条一样的被一条收掉。规则和 iOS / Android 一样（`lib/echo.ts`：归一化、电脑时钟最多慢 2 分钟、截断的按开头、一条对话记录只认一条，已经认过的记在 `claimed` 里）。

## 输入框等消息送到才清空（#1176）

- 对话页先画「发送中」的气泡、再等 `POST …/messages`（hub 把消息交给那台电脑上的 Agent，冷启动要好几秒），输入框以前等它成功才清空，于是同一段字在气泡和输入框里同时挂着，像没发出去。
  现在点发送就清空（文字、图片、上下文），失败才放回；等待期间又打的字接在放回的那段下面（`withUnsent`）。
- 本地复现慢送达和失败：假 hub 的 `MOCK_SEND_DELAY_MS=<毫秒>`、`MOCK_SEND_FAIL=1`。

## 展开 / 折叠不卡、不跳（2026-11-04，#1124）

- 量出来的「跳」几乎都在最新处：内容一挂上就卸载（`{open && …}` 包在 `Collapse` 里），折叠那一帧高度直接塌掉、滚动被夹住，被点的行一帧挪 88px；视口中部、上方展开折叠本来就是 0px（变的都在被点的行下面）。
  量的时候要把「点击前的位置 → 第一帧」也算进单帧位移，只比相邻两帧会漏掉第一帧的跳。
- 长轨迹（650 步）第一次挂上要排版一百多毫秒（4× 降速）：和 `0fr → 1fr` 放在同一帧，过渡的时钟从这一帧算，下一帧就开到大半；先在 `0fr` 下挂上、下一帧再开。
- 合上时给整棵子树加 `inert` 是一次全量样式计算（650 步 34ms，不降速），放在过渡结束、内容卸载之前做，不要和折叠的第一帧挤在一起。
- 补滚动放在 ResizeObserver 回调里（排版后、绘制前），在 rAF 里补会晚一帧、行会抖一下。浏览器自带的滚动锚定（`overflow-anchor`）挑的是视口里的某个元素，常常在展开的内容下面，反而把被点的行推走，所以对话页关着它、自己按住被点的行。
- 跟随最新处时，折叠的步骤取回来会一帧长几千像素：照样贴底就把组头一下子甩到顶上。一帧里要挪超过 64px 就停止跟随、按住这一行。
- 验证：Playwright（`playwright-core` + 缓存的 Chrome for Testing）+ CDP 的 `Emulation.setCPUThrottlingRate`、`long-animation-frame`、`Page.startScreencast` 录帧；Playwright 自带的 ffmpeg 只解 MJPEG，录帧用 jpeg，没有管道就先拼成一个文件再读。
  `recordVideo` 在这台机器上不出文件（ffmpeg 版本号对不上时报错，链过去之后静默）。量开发版（`vite`）没意义：React 开发模式一次展开 700ms，生产构建才是用户看到的。

## 打开、刷新对话停在最新处（2026-11-04，#1340）

- 「是不是还跟着最新处」不能只看滚动事件那一刻离底多远：自己 `scrollTop = scrollHeight` 产生的滚动事件要到下一帧才派发，
  中间又进来内容（图片一张张解码、任务卡、今天卡、一页历史）时，这个事件读到离底几百像素，以前就当成「用户往上翻了」停止跟随，
  刷新后长对话（主对话最明显）就停在最新消息上面。假 hub 的数据同步就排好版，自然刷新复现不了，要让内容一块块进来（每块一个任务、比一帧快）才稳定复现。
- 「滚动位置往上走了」也不等于是用户：输入框重新量高度时先塌一下，视口变高，浏览器把 `scrollTop` 往上夹几像素，事件同样晚一帧到。
  所以只有用户在滚（滚轮向上、手指、滚动键、拖滚动条，之后 700ms 内）且位置往上走，才停止跟随（`followsAfterScroll`）。
- 列表下面的东西（输入框、任务坞、提问卡）变高时内容没变、只是视口变矮，ResizeObserver 要连视口一起观察，否则最新一行被盖住。
- 回归检查：`scripts/test/web_chat_scroll_e2e.py`（说明见 `docs/web.md`「怎么跑」）。

## 表格查看器（2026-11-04，#1310）

- **npm 上的 `xlsx` 是停更的旧版**：SheetJS 2023 年起只从自己的 CDN 发新版，npm 的 0.18.5 有公开漏洞（读不可信文件时的原型污染、ReDoS）。仓库的隐私扫描和 Dockerfile 都只认 registry.npmjs.org，直接 `npm i https://cdn.sheetjs.com/…` 会被 `prebuild` 拦下（`unapproved npm source`）。做法是把 tarball 放进 `web/vendor/`，用 `file:` 相对路径装，Dockerfile 的检查放行 `file:vendor/`。
- **拖选格子会把表格横着滚走**：格子是普通文字，按下拖动时浏览器在做文字选择，碰到边缘就自动滚动滚动容器。按下时 `preventDefault()` 加 `select-none`。
- **半透明的选中底色会透出下面的格子**：行号列是 sticky 的，横向滚动时格子从它下面滑过，选中行号用 `bg-accent/12` 时被盖住的格子文字透了出来。sticky 的那层要不透明：`color-mix(in srgb, var(--accent) 14%, var(--inset))`。
- **估列宽别按平均字宽**：12.5px 的数字和大写字母比平均宽，按 7.2px 一个字符算，`SO-260000`、`2026-08-04` 这类定长值被截成省略号；按 8px 算才放得下。文件里设的列宽（Excel 的字符宽）常常也偏窄，取两者中大的。

## 打开时创建、关掉时 stop() 的模型（#1327 飞书 Pin 列表）

- 用 `useState(() => new XxxModel())` 建、在 effect 里 `refresh()`、清理时 `stop()` 的模型，开发版的 StrictMode 会「挂上 → 清理 → 再挂上」：第二次挂上时模型已经 stop 了，
  列表永远是空的，生产构建却正常。effect 里先 `start()` 再读；被作废的那次读取回来时不能直接 `return`，要接着跑排队的那次刷新，否则 `loading` 一直卡住。
- 无头 Chromium 截中文是方块：这台机器没装中文字体。拿一个 Noto Sans CJK 放进临时目录，写一份 `fonts.conf`（include 系统的再加这个目录），`FONTCONFIG_FILE=…` 启动浏览器。

## Office 预览（2026-11-04，#1345）

- **沙盒 iframe 里跑不了网页自己的 JS**：`sandbox="allow-scripts"` 不带 `allow-same-origin` 时 iframe 是不透明源，模块脚本按 CORS 取，hub 不给 `Access-Control-Allow-Origin` 就加载失败。所以不在 iframe 里跑渲染库，而是在本页的游离节点里渲染，再把结果（data URL 的图、转义过的文字）放进不带脚本的 `srcdoc` iframe。
- **游离节点里的 `innerHTML` 也会执行事件属性**：`<img src=x onerror=…>` 写进一个没挂到文档上的 div，图片照样加载、`onerror` 照样跑。所以不能用在本页里拿文件内容拼 `innerHTML` 的库（pptx-preview 就是，且源码不公开）；docx-preview 用 `createElement` 建节点，图片从包里读，不联网。
- **中文的「发粗宋体」是字体回退**：docx 点名的字体（Calibri、等线）本机没有时，浏览器退到自己的默认字体，中文是宋体。不能只给外层设无衬线：docx-preview 在样式表和元素上都直接写了 `font-family`，要给每一处补上回退列表；衬线只跟着文档点名的衬线字体。
- **iWork 文件是 zip，预览在里面**：新格式（2013 以后）有 `preview.jpg` 等第一页的图，旧一点的有 `QuickLook/Preview.pdf`（每一页）。zip 的目录在文件末尾，按 Range 读末尾 64 KiB 找到中央目录、再读那一项，不用下载整个文件；预览一般是不压缩存的。
- **Playwright 的 `addInitScript` 会注入每个 frame**，包括沙盒 iframe：截图日志里的「document is sandboxed and lacks the allow-same-origin flag」是测试脚本自己读 localStorage 报的，不是页面的错。

## 不在 layer 里的全局样式压过所有 Tailwind 工具类（2026-11-04，#1381）

- `styles.css` 里写在 `@layer` 外面的规则，按层叠规则排在所有 layer 之后，选择器再弱也压过 Tailwind v4 的工具类（都在 `@layer utilities` 里）。
  以前全局的 `* { scrollbar-width: thin }` 就这样让每个 `[scrollbar-width:none]` 都失效：任务坞、子 Agent 标签、终端按键栏的横向滚动行一直画着细滚动条。
  macOS 默认滚动条是浮层、不占地方，开发时看不出来；Linux / Windows 和设成「始终显示滚动条」的 Mac 上，它占了几像素高度，把胶囊往上顶，旁边居中的「全部」就低了一截。
- 全局的元素样式放进 `@layer base`；横着滚、不要滚动条的行用 `scrollbar-none`（`@utility`，带 WebKit 18.2 之前要的 `::-webkit-scrollbar`）。
- 无头 Chrome 默认带 `--hide-scrollbars`，截图里永远没有滚动条：要看滚动条的情形，Playwright 启动时 `ignore_default_args=["--hide-scrollbars"]`（Linux 上就是常显的经典滚动条）。
- 无头 Chrome 不理 CDP 的 `Input.synthesizeScrollGesture`，测触摸横滑用 `Input.dispatchTouchEvent` 发 touchStart / touchMove / touchEnd。
- 没有桌面库的 Linux 开发机上 Playwright 的 WebKit 缺一堆系统库跑不了，WebKit 的检查放到 Mac 上跑（`ssh -R` 把网页端口转过去）。

## Mermaid（2026-11-04，#1376）

- **Mermaid 有全局状态，同时画两张会互相踩**：`initialize` 是全局的，`render` 期间往 body 里塞临时节点。所有渲染排进一个队列一张张画，主题变了才重新 `initialize`。
- **先 `parse` 再 `render`，并开 `suppressErrorRendering`**：不然语法错误时 Mermaid 会在页面底部留下一个「Syntax error」的炸弹图。
- **默认主题的饼图不能用**：dark 主题的扇区接近黑色，default 主题是粉彩，且默认 `pieOpacity` 0.7 在白底上发白。用 `themeVariables` 的 `pie1…pie8` 和 `pieOpacity: "1"`。
- **`ui/Markdown.tsx` 不能 import `views/` 里的组件**：`views/chat/Steps.tsx` 反过来 import Markdown，循环依赖在模块初始化时会拿到 undefined；Mermaid 的放大层自己写了一个。

## Jupyter 笔记本（2026-11-04，#1407）

- **react-markdown 默认滤掉 data: URL**（`defaultUrlTransform` 只放常见协议），笔记本单元附件 `attachment:x.png` 直接换成 data URL 会变成空 src：先改写成 `#nb-attachment=名字` 这种它放行的片段地址，渲染后在 layout effect 里换成真图。
- **traceback 里全是 ANSI 颜色码**，而且按终端宽度排好了列：去掉颜色码后用 `white-space: pre` 横向滚动，折行（pre-wrap）会把 Traceback 那一行拆得乱七八糟。

## HTML 预览（2026-11-04，#1413）

- **CSP 的 meta 要放在 doctype 后面**：放在 `<!DOCTYPE html>` 前面，页面进怪异模式（布局变样）；放在它后面、`<html>` 前面就行，解析器会把开头的 `<meta>` 收进隐式的 `<head>`，排在页面自己加载的任何资源前面。
- **srcdoc 沙盒里只能靠 CSP 断网**：sandbox 没有「禁止联网」的开关，不给脚本也照样加载 `<img src="https://…">`（跟踪像素）。要断网就得注入 `default-src 'none'`。

- **沙盒里的页面要用同目录的文件，就由外面读好塞进去**（#1413）：srcdoc 框没有地址，`<img src="chart.png">` 解析到 `about:srcdoc` 下面，必然 404；放开联网也没用。外层页面（同源、带 token）经 `file/download` 读，转成 data URL / 内联样式和脚本再交给框；样式表里的 `url()` 要按样式表自己的目录解析，`@import` 递归拉进来。内联脚本里的 `</script` 要转义，不然提前结束元素。

## 网页媒体（2026-11-04，#1422）

- **`<video>` / `<audio>` 直接用 `file/download` 的地址就能拖动**：hub 用 `http.ServeContent` 回 Range，浏览器自己按需取；不要先 fetch 成 blob（大视频要等下完、占满内存）。地址里带 `?token=`，因为媒体元素发不了 Authorization 头。
- **HEIC 只有 Safari 认**：Chrome / Firefox 的 `<img>` 直接报错。解码库 libheif 是 LGPL，不在可用许可证里，所以退到那台电脑画的缩略图（Mac 上 Quick Look 能读 HEIC）。

## 文档对比（2026-11-04，#1437）

- **旧版本不要经 JSON 传**：node 把 `git cat-file blob` 的内容写进缓存（按 blob id 命名，带原扩展名，内容不变名字就不变），回一个路径，客户端用已有的 `file/download` 按 Range 读，查看器、PDF、Office 渲染全都能直接复用。
- **pdf.js 6 的接口变了**：`getDocument` 没有 `isEvalSupported` 了，释放用 loading task 的 `destroy()`（文档对象上没有）；worker 用 `pdfjs-dist/build/pdf.worker.min.mjs?url` 交给 Vite 出地址。


## 会话里的文件链接（2026-11-04，#1555）

- Markdown 的 `/workspace/report.md` 是 Agent 所在电脑的路径，不能让浏览器把它当成 hub 的站内网页：答案（含流式文字）和执行过程文字向 Markdown 传入机器与工作目录，普通点击复用文件侧栏；相对路径按会话目录解析。链接的真实 href 走机器的文件下载接口，修饰键 / 中键打开也不会落到站内 404。
- `new URL()` 接受路径里不完整的 `%` 转义，后续 `decodeURIComponent()` 仍会抛 `URIError`。模型输出的 `persona://session/…` / `jarvis://session/…` 在渲染期经过链接分类，解码失败要当作无效链接，不能让整个对话白屏；回归用真实 Markdown 渲染覆盖这个边界。
