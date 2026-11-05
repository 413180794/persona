# 网页版（`web/`）

浏览器里用的 Persona：总览、Agents（各台电脑上 herdr 里的 Claude Code / Codex，连同终端、文件、Git）、Persona 助手（定时任务、记忆、知识库）、
收件箱、机器监控和设置，功能和手机端基本一致（差距见文末「和手机端的差距」，#297、#433 两轮补齐）。视觉和交互照
[Beautiful UI](https://www.beautifului.dev/)（MIT，许可见 `THIRD_PARTY_NOTICES.md`）：设计 token（`--ink` / `--surface` / `--line`…，浅色 / 深色两套）、
关键帧、组件语法（Thinking 的展开轨迹、Tool Chips、Approval Card、Prompt Bar、Sidebar Nav 的滑动高亮、Search、Task Rows、Insight Cards）。
组件是照着官网的 React 版重写的，接真实数据，不是官网的演示组件原样搬过来（那些是自己跑演示动画、不收数据的）。

## 怎么跑

- **由 hub 自己托管**：hub 把 `<hub 数据目录>/web`（默认 `~/.persona/web`）当静态站，和 API 同一个端口（hub 的 `listen`，默认 `:18660`，即所有网卡）。
  同源，所以没有 CORS，WebSocket 和图片也用同一个 token。没有这个目录时非 `/api/` 路径照旧 404。
  不是文件的路径都回 `index.html`（前端路由：`/`（Persona 主对话）、`/overview`（总览）、`/s/<机器>/<会话>`、`/t/<机器>/<空间>/<tab>[/<会话>]`（herdr 的一个 tab，#1963）、`/j`、`/j/<会话>`、`/j/new`、`/history`、`/inbox`、`/inbox/<件>`、`/messages`、`/messages/<会话>`、`/routines`、
  `/kb`、`/kb/<类型>/<条目>`、`/workspace-board`（工作流看板）、`/machines`、`/machines/<机器>`、`/terminal`、`/terminal/<机器>/<终端 id>`、`/assistant`（助手资料页）、`/settings`、`/extension/<扩展>/<页面>`（扩展的整页））；
  `/assets/` 下是带内容哈希的文件，永久缓存，`index.html` 不缓存（`server/internal/hub/web.go`）。
- 部署：`./scripts/deploy/deploy.sh web`（只构建、传网页，不重启 hub）；部署 hub 时也会顺带部署网页：网页和 Go 二进制一起先编好，编不过就打出编译输出（含隐私扫描的命中行）、什么都不装；hub 重启之后再上传。
- 本地开发：`cd web && npm ci && npm run dev`，Vite 把 `/api` 代理到 `JARVIS_HUB_URL`（`eval "$(python3 ../scripts/persona_config.py env)"`）。
  测试 `npm test`（vitest，`src/lib/*.test.ts`），类型检查 `npm run typecheck`。
  侧边栏入口的端到端检查（Persona 进主对话、两个「+」、老链接、窄屏抽屉；Python Playwright 驱动本机 Chrome）：起假 hub 和 `npx vite` 后跑
  `python3 scripts/test/web_sidebar_e2e.py <网页地址> [--shots <目录>]`，用法见脚本开头，`--shots` 顺带拍浅色 / 深色 / 窄屏截图。
  设置入口的端到端检查（#2196：渠道在连接器前、旧扩展链接兼容、渠道与连接器互相跳转、窄屏返回设置）：假 hub 带 `MOCK_CHANNELS=1 MOCK_PLUGINS=1` 起，
  跑 `python3 scripts/test/web_settings_entries_e2e.py <网页地址> [--shots <目录>]`，检查宽屏 / 窄屏 × 浅色 / 深色。
  Agents 列表的「herdr 没在运行」提示行（#1916）：假 hub 的 `GET /mock/herdr?stopped=&start=` 选哪台停着、启动怎么走，
  跑 `python3 scripts/test/web_herdr_notice_e2e.py <网页地址> --hub <假 hub 地址> [--shots <目录>] [--channel msedge]`（宽屏 / 窄屏 × 浅色 / 深色）。
  任务「重试」的端到端检查（#1396：卡片、失败回执、计划卡红字、连点、再失败、离线；桌面 / 手机宽度 × 浅色 / 深色）：假 hub 带 `MOCK_TASKS=1` 起，
  跑 `python3 scripts/test/web_task_retry_e2e.py <网页地址> --hub <假 hub 地址> [--shots <目录>]`（假 hub 的 `GET /mock/retry?mode=|fail|offline` 复原失败的任务、选重试的结果）。
  注入消息的端到端检查（#2360：任务通知、定时任务画成一行，不露出给模型的话，点开任务 / 要求）：假 hub 带 `MOCK_TASKS=1 MOCK_INJECTED=1` 起，
  跑 `python3 scripts/test/web_injected_e2e.py <网页地址> [--shots <目录>]`（桌面 / 手机宽度 × 浅色 / 深色）。
  引用的端到端检查（#2427：引用条、输入框保持空、× 取消、发出去仍是 `> ` 块、气泡和历史消息里的引用卡片能展开）：假 hub 带 `MOCK_QUOTE=1` 起，
  跑 `python3 scripts/test/web_quote_e2e.py <网页地址> [--shots <目录>]`（桌面 / 手机宽度 × 浅色 / 深色；`--before` 只拍改动前的同样几张；任务卡片的引用加 `--tasks`，假 hub 带 `MOCK_TASKS=1` 起）。
  定时任务「跑完推送」的端到端检查（#2570：hub 有 `task_push` 时编辑页的两个选项、选「只在它自己提醒时」藏起「接着原来的对话」并保存 `push`、列表的标记、选回「每次」；桌面 / 手机宽度 × 浅色 / 深色）：起假 hub 和 `npx vite` 后跑
  `python3 scripts/test/web_task_push_e2e.py <网页地址> --hub <假 hub 地址> [--shots <目录>]`，用法见脚本开头。
  侧栏分组收起的端到端检查（「其他 Agent」收起 / 展开 / 刷新后保持，五种分组方式下每组都能收起，触屏常显箭头，#1417）：假 hub 带 `MOCK_SOURCES=1` 起，再跑
  `python3 scripts/test/web_sidebar_fold_e2e.py <网页地址> [--shots <目录>]`。
  侧边栏收成图标栏的端到端检查（#2524：按钮和 Ctrl+Alt+B、刷新后保持、页面 / Agents / 状态分组收起并保持、窄屏还是抽屉）：假 hub 带 `MOCK_SOURCES=1` 起，
  网页用 `npx vite preview`（开发服务器在刷新时偶尔中断模块加载），跑 `python3 scripts/test/web_sidebar_collapse_e2e.py <网页地址> [--shots <目录>]`。
  对话停在最新处的端到端检查（首次打开、刷新、侧栏切换、内容一块块进来、输入框变高、往上翻了不被拉回、慢网分页，主对话和 Agent 对话、宽屏和窄屏，#1340）：
  假 hub 带 `MOCK_TASKS=1 MOCK_LONG_ASSISTANT=1 MOCK_LONG_HISTORY=1` 起，再跑 `python3 scripts/test/web_chat_scroll_e2e.py <网页地址> [--hub <假 hub 地址>] [--shots <目录>]`。
  主对话任务坞的端到端检查（「全部 N」和胶囊同高、上下边对齐，胶囊行没有滚动条、仍能横滑；Chrome 开着常显的滚动条跑，WebKit 可选）：假 hub 带
  `MOCK_TASKS=1 MOCK_TASKS_DOCK=pair|many` 起，再跑 `python3 scripts/test/web_task_dock_e2e.py <网页地址> [--browser chromium|webkit|all] [--shots <目录>]`。
  侧边面板打开正在提问的任务不把整页顶上去的端到端检查（#1282：面板里的卡片像窄屏一样折起、底部区域限高，输入框聚焦不滚祖先）：假 hub 带
  `MOCK_TASKS=1 MOCK_TASKS_DOCK=many` 起，再跑 `python3 scripts/test/web_panel_focus_e2e.py <网页地址> [--size 1280x800] [--shots <目录>]`。
  安装成 App 的端到端检查（Chrome 认为可安装、service worker 接管、缓存里只有外壳和 assets、断网能打开、设置里的安装行、theme-color 跟深浅色）：
  service worker 只在构建里跑，所以先 `npm run build`，用 `npx vite preview`（不是 `vite`）对着假 hub 起，再跑 `python3 scripts/test/web_pwa_e2e.py <网页地址> [--shots <目录>]`。
  终端鼠标的端到端检查（#2441：点击 / 拖动 / 中右键 / ctrl alt 修饰键以 `terminal_input.mouse` 到 hub，Shift 选字和 Shift+右键菜单，「本地选择」，只看时不发，滚轮不变）：
  假 hub 带 `MOCK_TERMINAL_STREAM=1 MOCK_TERMINAL_CONTROL=1 MOCK_TERMINAL_PANES=1` 起，再跑 `python3 scripts/test/web_terminal_mouse_e2e.py <网页地址> --hub <假 hub 地址> [--shots <目录>]`。

## 子路径（#2563）

hub 常常不单独开端口，而是挂在已有网站的一个子路径下：反向代理把 `https://<域名>/<前缀>/` 去掉前缀转给 hub 的明文端口，
`hub.json` 的 `public_url` 写成 `https://<域名>/<前缀>`（App 和 `node --dial` 都在它后面拼 `/api/v1/…`，配对链接也用它）。网页这样也能用：

- **代理带上 `X-Forwarded-Prefix: /<前缀>`**。hub 返回 `index.html` 时把里面的 `<base href="/" />` 改成 `<base href="/<前缀>/" />`
  （`server/internal/hub/web.go` 的 `forwardedPrefix` / `withBase`）。只认 `/` 开头、每段只有字母数字和 `-._~`、不含 `.` / `..` 段、256 字节以内的前缀；
  别的（引号、尖括号、空格、控制字符、百分号转义……）一律忽略，当作根路径。不带这个头时和以前一模一样。
  同一个 `index.html` 按这个头回不同的内容，所以带 `Vary: X-Forwarded-Prefix`（本来就是 `no-cache`，`Last-Modified` 照旧，304 照旧）；只改 `index.html`，别的文件原样。
- **网页从 `<base>` 读前缀**（`src/lib/base.ts` 的 `BASE`，根路径时是空串）：接口（`lib/api.ts`）、WebSocket（`lib/store.ts`）、
  图片 / 文件下载 / 缩略图 / 头像 / 形象包 / 飞书文档的媒体（`imageURL`、`fileDownloadURL`、`fileThumbURL`、`agentAvatarURL`、`personaFileURL`、收件箱的 `withToken`、`feishu-doc` 的 `mediaURL`）、
  实时画面的页面和它的 WebSocket（`/screen/<机器>`）、前端路由都加上它。路由：`href()` 生成带前缀的地址，`parse()` 读的是去掉前缀的路径（`useRoute` 先 `stripBase`），
  `navigate("/settings?…")` 这种字符串参数是 App 内的路径、不带前缀。hub 自己页面的链接（`/s/…`、`/todos?id=…`）也要在前缀之内才在 App 里打开。
  写新代码时：拼 `/api/…` 的 URL 一律走上面这些函数（或在前面加 `BASE`），站内链接用 `href(route)`，不写死 `"/xxx"`。
- **打包按相对路径**（`vite.config.ts` 的 `base: "./"`）：`index.html` 引用 `./assets/…`，图标、manifest 也写相对地址，都相对 `<base>`；
  深层路径（`/<前缀>/s/<机器>/<会话>`）直接刷新也对。`manifest.webmanifest` 的 `start_url` / `scope` / 图标 / 快捷方式都是相对地址（`id` 仍是 `/`，装过的不变）。
- **子路径下不注册 service worker**（`lib/pwa.ts`）：它的规则和缓存都按根路径写；PWA 照样能「添加到主屏幕」，只是没有离线外壳。
- **管理界面（`/admin`）只在根路径**：它不读前缀，经子路径打不开。管理界面按来源地址限制访问（[admin.md](admin.md)），本来就不该经对公网的代理开放，
  在 hub 机器上或局域网里直接访问 hub 的端口。
- **一个源只放一台 hub**：登录（`localStorage`）按源存，同一个域名下两个前缀挂两台 hub 会互相顶掉登录。「切换 hub」的列表可以记带前缀的地址。

nginx 示例（`hub.example.com`、`/persona/` 和端口换成自己的；`map` 写在 `http` 块里，`location` 写在 443 的 `server` 块里）：

```nginx
map $http_upgrade $connection_upgrade { default upgrade; '' close; }

location = /persona { return 301 /persona/; }
location /persona/ {
    proxy_pass http://127.0.0.1:18660/;          # 末尾的 / 去掉前缀
    proxy_set_header X-Forwarded-Prefix /persona;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;      # WebSocket
    proxy_set_header Connection $connection_upgrade;
    proxy_buffering off;
    proxy_read_timeout 1h;
}
```

`hub.json` 里 `"public_url": "https://hub.example.com/persona"`。验证：浏览器打开 `https://hub.example.com/persona/`，开发者工具里所有请求（含 `/api/v1/ws`）都应在 `/persona/` 下。

## 登录

第一次打开要贴 hub 的 token（`personad hub pair` 打印的那个）。网页先用它换一个自己的设备 token（`POST /api/v1/auth/tokens`，`kind: web`，
hub 存为 `other`，角色 full），之后只用自己的，配对 token 不留在浏览器里；手机「设置 › 设备」能单独撤销。也可以直接贴一个 `jd_` 设备 token。
**链接直接登录**（2026-10-31）：打开 `<hub 地址>/?token=<hub token>`（任意页面路径都行，也认 `#token=`，后者不会发到服务器）就直接进，不用贴。
页面一加载先把 token 从地址栏抹掉（`history.replaceState`，在 React 渲染之前），浏览历史、书签、复制出去的链接里都没有它；然后照常换成这个浏览器自己的
设备 token。这个浏览器已经登录过（链接再打开一次）就沿用已有的，不再多建一个设备 token。token 不对时停在登录页并提示。
hub 的访问日志只记路径，不记查询串。注意这个链接本身就是完整权限：别发到群里、别存在别人能看的地方，泄露了在 hub 机器上换 token。
已经登录的浏览器可以在「设置 › 设备 › 连接新设备」给另一台设备生成这样的登录链接（一次性临时码，10 分钟），见下文「设置」。
token 存在 `localStorage`；`<img>` 带不了请求头，图片走 `?token=`（hub 本来就认，日志不记查询串）。
左上角菜单「断开这个浏览器」会撤销这个 token。

**安全**：页面本身不需要鉴权（只是静态文件），所有数据都在 `/api/` 后面要 token。hub 默认是明文 HTTP，token 在网络上可见：
只在内网 / VPN 里用，要放到公网前面必须加 HTTPS 反向代理。

## 页面

- **工作流看板**（`/workspace-board`，#1646）：默认关闭，在设置 › 通用 › Agent 打开（开关存在 hub，每个浏览器一样，#2475）；关闭时侧边栏没有入口，直接打开地址显示「工作流看板未开启」和去设置的按钮，也不读看板、不轮询发现接口。打开后从侧边栏进入，绑定已有工作区或在线机器的真实目录，手工管理列、卡片顺序、置顶和归档；详情关联同目录的 Agents，并打开文件、Git 或显式新建 Agent。卡片和列状态存 Postgres，Agent 状态不迁列，离线保留卡片。操作、权限、升级和来源见 [工作区看板](workspace-board.md)。

- **终端**（`/terminal`、`/terminal/<机器>/<终端 id>`，`src/views/terminal/TerminalPage.tsx`、`src/lib/terminals.ts`，`docs/terminal.md` 第十九节）：不开 Agent 也能开终端。
  机器 → herdr 空间 → 标签页 → 窗格，Agent 带状态点，普通 shell 是终端图标；点一个普通 shell 窗格在右边（窄屏整页）打开成终端，里面就是「侧边面板 › 终端」的那个 `TerminalPanel`，接管 / 只看 / 键栏 / 历史都一样；点 **Agent 的窗格**（Jump To 回车也一样）进它的会话页、中间是终端视图（「终端 ⇄ 对话」，#2419），`/terminal/<机器>/<Agent 的会话 id>` 的老链接照旧能开。
  终端有两种界面，头上的「单窗格 / 完整界面」切换（默认完整界面，下面「侧边面板 › 终端」）。
  空间旁的「+」新建一个 shell 标签页（`POST /machines/{mid}/panes`，电脑上不抢焦点）；列表上面的输入框是 Jump To（标题 / 目录 / 分支 / 机器，↑ ↓ 回车）；每台机器最近打开的 2 个终端挂着，切换立即有画面，
  机器 / 工作区 / 标签页都能折叠，每个节点有右键菜单（「节点菜单」）。标签页进后台时只留正在看的那个，离开页面全部放手。窗格终端等于那台电脑的 shell，只有完整设备能开（手表令牌、扩展令牌都不行）。截图用 `MOCK_TERMINAL_STREAM=1 MOCK_TERMINAL_CONTROL=1 MOCK_TERMINAL_PANES=1` 的假 hub。
  路由为什么是整页而不是侧边面板：侧边面板挨着一段对话，没有对话时没有可挨的；整页有地址，可以收藏和前进后退。

- **侧边栏**（宽屏常驻，窄屏是抽屉，两者同一份）：连接状态和在线电脑数、⌘K 搜索（会话和终端窗格，`>` 是命令，见「快捷键和命令面板」）；第一项是 **Persona**（形象头像、加粗，打开主对话，主对话在工作 / 需要你时带状态点或件数），
  下面是总览 / 收件箱（「需要你」的件数）/ 待办 / 定时任务 / 知识库 / 机器 / 终端（hub 有 `terminal_panes` 才有）/（扩展的页面，见「设置」里的「UI 插槽」）/ 设置；再下面「Persona 对话」（除主对话外最近 5 段，末尾「全部对话」；
  标题右边「+」开一次性的新对话 `/j/new`）、个人 Agent、Agents（标题右边「+」是新建 Agent，⌘K 面板里也有）。
  **为什么默认落在 Persona**（#1239）：和 iOS 一致（App 打开是「助手」，Agents 是第二个 tab），主对话本身带任务坞和「需要你」（#930），
  要处理的事在这里就能看到、能交代；总览更像 Agents 的仪表盘，挪到 `/overview`。主对话只有一个入口：`/`、旧的 `/j`、`/persona`、`/j/<主对话 id>` 都选中 Persona 这一行，
  「Persona 对话」列表和新对话页顶栏不再重复列「主对话」。主对话的顶栏标题固定是「Persona」（形象头像在左），没有副标题行（同 iOS 的 Persona 顶栏），菜单里没有「重命名…」（#1313）；
  旁支对话照旧是对话标题 +「启动器 · 多久以前」。还没有主对话时 `/` 是新对话页，第一条消息带 `main: true` 开出主对话。
  主对话的 ⋯ 菜单还有「压缩上下文」和「新建主对话…」（hub 的 `features` 有 `assistant_compact` / `assistant_main_new` 时才有，#1727，`lib/compaction.ts`）：
  压缩中输入框上方一条「正在压缩上下文…」，压完短暂提示「已压缩上下文」，失败显示原因、可以关掉（只提示这一页看到的变化或刚结束的，不会每次打开都提示旧的）；
  新建要确认（「带上旧对话的摘要」默认开），成功后回到 `/` 的新对话页，旧的在「全部对话 › 已归档」里叫「旧主对话 · 至 M月D日」。
  所有对话里一次压缩画成一条分隔线「以上内容已压缩 · 手动 · 压缩前 N 万 token」，`/compact` 的「压缩上下文」和「已从压缩的上下文继续」并进这条线（悬停看原文）。
  **当前打开的那一行**（#2283）：一直带 `--hover-2` 底色（比悬停的 `--hover` 深一档，深浅色都看得出），`aria-current="page"`；悬停的滑动色块只跟着鼠标走，不再把当前行的底色带走。
  滑动色块的位置除了侧边栏重新渲染时量，列表布局变了也重新量（#2476，`useGlide`）：`ResizeObserver` 盯着列表、它的各块和每一行，加上折叠动画结束、列表里的滚动，一帧最多量一次；
  色块只盖住行露在外面的部分（`lib/glide.ts` 按行和外面各层 `overflow: hidden` 的框求交），行收进折叠的分组、高度为 0 或不在了就淡出，再出现时从原处滑过去。
  当前会话不在「Persona 对话」最近 5 段里（从「全部对话」、⌘K、收件箱、链接打开更早的一段）时补在这 5 段后面；「最近（未运行）」超过 8 条、个人 Agent 超过 5 个时也一样。
  它所在的分组（含默认收起的）在它打开时展开一次，之后照样能手动收起，记下的收起状态不变；列表把它滚到中间，在用户自己滚动、点按或按键之前，上面的内容加载出来把它挤出可视区域也会再滚回来。
  当前行的 ⋯（Agent）和置顶按钮（个人 Agent）常驻，不用悬停；行尾给它留出位置，提示和角标不再让开。
  **整个侧边栏能收成图标栏**（#2524，只在宽屏）：顶部账号行右边的按钮或 ⌘⌥B / Ctrl+Alt+B 收起，变成 56px 宽的一列图标（展开按钮、搜索、Persona、各页面、新建 Agent，底部是连接状态），
  当前页有底色，页面的件数变成图标角上的小圆点（件数在无障碍标签和悬停提示里）。选图标栏而不是整个藏掉：页面还是点一下就到，展开的按钮一直在屏幕上，所以收起状态可以放心记住；
  会话和 Agents 列表收起后用 ⌘K 找。⌘B 已经是右侧面板、⌘⇧B 是浏览器的书签栏，所以沿用 Moshi 键位的 ⌘⌥B（设置里能改）。窄屏还是完整的抽屉，不看这个状态。
  **侧边栏的每个分组都能收起**（#1417）：「页面」（总览…设置，标题是 #2524 加的，Persona 那一行在它上面不收）和整个 Agents 块（分组方式和「+」留在标题行）也能收起，
  这两块不跟「折叠 / 展开全部分组」走，收起全部时还能看到各组标题。Persona 对话、个人 Agent、置顶、正在启动、各种分组方式下的每一组、空闲的空间、其他 Agent、最近（未运行）都是同一个
  `Section`（`folded` / `onToggle` 是必填的），点标题收起 / 展开，收起状态存在这个浏览器（`localStorage` 的 `fold:<键>`），刷新后保持；没有悬停的触屏上折叠箭头常显。
  标题右边是件数，收起时换成里面「需要你 / 完成未看 / 工作中」的状态点，收起不会把要看的藏掉。
  「其他 Agent」（不在任何空间里的 Agent，比如 Codex、DSH 的会话）按空间的规则决定默认：多于 3 个且都没什么要看的（已看、在等）时默认收起，否则展开。
  不照「空闲的空间」一律收起，是因为没用 herdr 的人所有 Agent 都在这一组，一律收起等于把工作中的也藏了。iOS 没有这一组，空间外的 Agent 按电脑归成一个空间，同样能收起。
  Agents 标题右侧可切换「按机器 / 按仓库 / 按项目 / 按状态 / 按待办」，没选过的浏览器默认按机器（#1963）；选过的（包括按待办，#1836）一直记住，下次打开照旧；选了按待办而 hub 不支持待办时暂按机器显示，选择和各组折叠状态保存在当前浏览器。
  - **按机器**（照 herdr 的编排，`src/lib/herdr-tree.ts`）：机器 → Workspace（herdr 的顺序，`space_index`）→ Tab（`slot_index`）。一个会话是一个 Agent pane，同一个 tab 的几个 Agent pane 在会话页顶部用标签（`views/chat/PaneTabs.tsx`）切换；普通 shell pane 不列。
    机器行带在线点和需要你的件数，离线机器默认收起；Workspace 行带状态点和 Agent 数（和机器、快速筛选一样数 Agent，悬停显示几个标签页），⋯ 沿用空间菜单；Tab 行显示 `#N` 或名字、最急的那个 pane 的标题和「N 个 Agent」。每层的状态点取下面最急的（需要你 > 工作中 > 完成未看 > 已看）。
    一个 tab 里有几个 Agent 时（#2320，`docs/agent-states.md`「一个 tab 里的几个 pane」），行尾的「N 个 Agent ›」能展开成每个 Agent 一行（带状态点、Agent 名和自己的 ⋯），
    按 pane 的位置排（`pane_index`，没有时按 herdr 开 pane 的先后，pane id 是 36 进制的计数）：发起者在上、标「发起者」，它分出的 pane 缩进挂在下面一条细线上；
    左上是 shell 时几个 Agent 平级、不标。默认展开（要一眼看出哪几个 Agent 是一组），点「N 个 Agent」收起成一行，收起状态按 `机器/空间名/tab 的编号` 记在这个浏览器（不带 tab 名：同名的两个 tab 各记各的，tab 改名（hub 会把 `#2` 改成任务标题）后照样认得，herdr 重启后编号不变；没有编号的宿主用 tab 的 id）；tab 名太长时截断（最多占一半），不压到「N 个 Agent」上。
    收起时打开的是里面某个 Agent，亮的是 tab 这一行（滑动的高亮和「滚到打开的那行」都落在它上面），展开后才是那个 Agent 自己的行：收起的子行还挂在折叠容器里，但不登记位置。会话页顶部的标签同样按位置排，发起者带「发起」（手机三个标签挤一行时只留这个标记、不写 Agent 名）。
    点 Tab 进 `/t/<机器>/<空间>/<tab>`，默认选最急的 pane；herdr 重启后 id 变了打不开时退回链接里的会话；旧的 `/s/` 链接照常打开。没有 tab 这一层的宿主（Paseo、botmux、runtime）空间下面直接列会话，不在空间里的放到机器下的「其他」。
    折叠和置顶按 `机器/空间名`（herdr 重启后 workspace id 会变）。
    带 `parent_space_id` 的空间（Orca 的子工作区，#2073）照 Orca 侧栏挂在父空间下：父空间的 tab 后面多一行「N 个子项」，默认收起（`<机器/空间名>/子项`），收起时带子项里要看的状态点；
    父空间一行汇总子项状态（需要你 / 工作中 / 完成的件数），自己的状态点和收起时的状态点算上子项。父空间没有在跑的 Agent（不在列表里）或父子成环时，子空间照旧排在顶层。只有「按机器」分组这样折叠。
  - **按仓库**（照着 Orca 的「项目」侧栏）：按机器内的仓库聚合，仓库下面是它的工作树，工作树下面才是 Agent。仓库和工作树都从 cwd 推断（协议不带仓库，`src/lib/agent-repos.ts`）：`.worktrees/`、`.claude/worktrees/`、`.herdr/worktrees/` 前面的那一段是仓库，`~/.worktrees/<仓库>/<工作树>` 这种集中布局取标记后的第一段做仓库，其余路径本身就是仓库。主工作树带 home 图标排第一，没有在跑的 Agent 的工作树写上「空闲」并默认收起；只有一个主工作树的仓库省掉工作树这一层，Agent 直接挂在仓库下面。
  - **按项目**保留原来的 herdr 顺序（机器 → workspace → tab），没有动静的空间收进「空闲的空间」；有 loop、后台工作、运行中子 Agent 的空间不会被收起。
  - **按状态**对齐 iOS `AgentListOrder`：需要你 → 完成未看 → 工作中 → 待唤醒 → 已屏蔽 → 已看，只显示非空组，已看默认折叠。
    屏蔽来自 hub `/mutes`，需要你不受屏蔽降级，普通已看仍归已看；项目内单独开启通知的 Agent 不算屏蔽。
    组内按时间降序、同时间按会话 ID 排序；工作中按 `state_since`（缺失回退 `updated_at`），不随每条输出换位。
    状态分组的行补项目上下文，多机时还补机器名称。
  - **按待办**：每个有挂着 Agent 的未完成待办一段，段头是待办标题和状态，点段头进待办；没挂待办的 Agent 放最后「未归属」一段，上面有待办时默认折叠（里面有需要你的除外）。列表里同时有两种以上宿主（herdr、botmux、Orca…）时，不在 herdr 的行带宿主小标签（`botmux`，从 `pane_id` 前缀判断），只有一种时不显示。置顶、屏蔽、「最近（未运行）」的规则和「按状态」一致。其他分组下挂了待办的行显示待办图标标签。
  - **最近（未运行）**在三种分组下都保留：会话 offline 或机器离线 / 已不在机器列表时只进这里，不重复进入 live 分组；助手不混入 Agents。
    展开先显示 8 条，可继续显示到最近 40 条。折叠内容退出键盘焦点和无障碍树。
  - **快速筛选**（#433，iOS `QuickFilterBar`，`src/lib/agent-filter.ts`）：Agents 标题下「全部 / 需要你 / 未读 / 工作中」胶囊带件数，只露出有 Agent 的状态，和三种分组叠加，存在当前浏览器。筛选这一排最后有「botmux」开关（#2786，有 botmux 会话时出现，已隐藏时也留着方便恢复）：点一下在列表里隐藏 / 重新显示 botmux 会话（划线表示已隐藏），和任何筛选、分组叠加，存在当前浏览器；「飞书群」只有 botmux 会话，所以在「飞书群」下隐藏会回到「全部」，隐藏时选「飞书群」会重新显示；它只管这个列表，机器详情 › 引擎里的「同时显示 botmux 会话」照旧决定那台电脑的 botmux 会话列不列出来。
    计数同 iOS（屏蔽的未读 / 工作中不算，屏蔽的需要你算）；选中时匹配的记下来，点开的未读变已看不会从筛选里跳走，再点胶囊才重算。有筛选时「最近」不显示、空闲的空间不折。
  - **全部标为已读**（iOS `AppModel.markSeen`）：筛「未读」时列表最上面一行、空间标题的菜单、总览「做完了，还没看」右侧，都先确认；本地立刻改成已看（WS `seen`，断线时 `POST …/seen`）。
  - **置顶**（`GET / PUT /pins`，key 同 iOS：`机器/空间名`、`机器/会话 id`）：最上面一段「置顶」，置顶的空间在按项目分组里排最前、空闲也不折；先改本地、失败恢复。
  - **行菜单 / 空间菜单**（iOS `AgentRowMenu` / `SpaceMenuItems`，`src/views/AgentMenus.tsx`）：Agent 行悬停的 ⋯ 或右键（右键在鼠标位置打开）：置顶、重命名、屏蔽通知、交给 Persona、拷贝目录 / 恢复命令，
    再接上每个节点都有的那一组（复制会话 id / herdr id、新建窗格、Git、关闭窗格，见下面「节点菜单」）；
    空间标题的 ⋯：全部标为已读、重命名空间、置顶空间、屏蔽这个空间，再接上折叠、新建 Agent、Git、复制路径。悬停时行尾的提示和角标让开，不被 ⋯ 盖住一半。
    「交给 Persona」（iOS `AgentHandoff`）把这个 Agent 的情况作为上下文卡片发进主对话，走跨页的待发消息（见下面「Persona」），跳过去就看到气泡。
- **待办**（`/todos`，`src/views/Todos.tsx`）：左侧列表右侧详情。列表按未完成（等你 > 待查看 > 进行中 > 待跟进 > 未开始）和创建时间排序，已完成收在底部；每行显示状态、关联 Agent 数和最要紧 Agent 的最后一句。详情可就地修改标题和说明，列出当前关联的 Agents（点击进对话、启动失败可移除）与参与过的历史；可「交给 Agent」（新开一个或交给已有的，带待办上下文卡片和 `todo_id`）、「完成 / 重新打开」或删除。对话页 ⋯ 菜单和会话信息展示「待办：X」；hub 不支持待办接口（404）时隐藏入口。
- **新建 Agent**：首条消息支持选择、粘贴、拖入图片或文件，附件先在本地预览并可移除；图片随消息发送，其他文件后台上传到所选电脑后把路径带入消息。
  点击「启动」即关闭弹窗并打开本地会话，首条文字和附件立即可见；用 `async:true` 创建真实会话后再发 `/messages`，不等待远程启动完成才跳转。
  侧边栏「正在启动」保留后台任务，切去其他页面也继续，完成后不会强制跳走。失败保留内容，可重试；已经有真实会话时只重试上传或发送，消息 ID 不变。
  创建或送达结果不确定时先提示查看列表 / 会话，不自动重复创建或发送。首条消息仍可为空；Codex 文字启动兼容保留，但其创建协议不支持首条附件，界面明确提示。
  文字草稿保存在 localStorage，附件和后台任务只保留在当前页面内存，刷新网页不会恢复未完成任务。
- **总览**：需要你处理的交互、停在电脑上的、正在工作的（计时）、做完还没看的（右侧「全部标为已读」），每个一行（Task Rows），下面是各台电脑。
  同一个 herdr tab 的几个 Agent 收成一行（#2320，`lib/herdr-tree.ts` 的 `homeTaskGroups`）：放在其中最急的那个状态下，标题是发起者的（没有发起者时是第一个 pane 的），
  下面一句是代表它的那个 Agent 的最新一句（不是发起者时前面带 Agent 名），行尾每个 Agent 一个状态点和「N 个 Agent」（窄屏只留点），点开是这个 tab、停在代表它的 pane 上。
  代表它的是这个状态下的几个 Agent 里按这一组的排法排第一的：正在工作取干得最久的（计时和排序都按它），需要你 / 做完未看取最新的（时间、预览和排序都按它）；时间一样时取 pane 靠前的。
  已经有交互卡片的那个 Agent 算在上面的卡片里，不再决定它所在 tab 放哪一组；只剩卡片时也不说「没有需要你处理的」。
  「全部标为已读」照旧标全部完成未看的 Agent（确认框写实际个数），包括收在「停在电脑上」「正在工作」那几行里的：有「做完了，还没看」这组时在它的标题右侧，没有这组但有完成未看的时在顶部那句概况的右侧。搜索（⌘K）、待办，以及侧栏 / Agents 页按机器以外的分组（按仓库 / 项目 / 状态 / 待办）照旧一个 Agent 一行。
- **对话页**（#1963 对齐 cindy 的展示，代码 `src/lib/turns.ts`、`src/lib/work-groups.ts`、`views/chat/`）：每条你的消息一段。段里的工具、思考、中间的话按 cindy 的规则分组：
  进行中的是「正在工作…」+ 计时，露出最近 5 条（不超过 5 条时标题不能点）；一轮做完，最后一段回复留在外面，前面的过程收进「已工作 Xs」（默认收起，全是读和搜的追加「· 读取 N 个文件 · 搜索 N 次」）。
  交付型正文（600 字以上，或有标题、表格、3 项以上列表、图片）和运行中的子 Agent 卡不收进去。请求带 `fold_work`，展开时按 `from_seq` / `through_seq` 取回折叠的部分。
  - **工具行**（`ActionRow`）：`[状态][动词][参数][+N −N][›]`，动词是 编辑 / 创建 / 运行 / 读取 / 搜索 / 访问 / 调用，Bash 有 description 时整句用它，文件参数是只写文件名的小块；命令类就地展开（输入、分隔线、输出，不截断），Edit / Write / MultiEdit 打开 diff 浮层。
  - **思考**（`ThinkingCard`）：「思考 Ns」，有正文才能展开（Claude Code 的思考大多没有正文）；正在思考时末尾临时一行「Thinking」+ 跳动的圆点 + 计时（来自实时状态）。
  - **计划**（`PlanCard`，TodoWrite）：默认展开，头部「完成/总数 · 当前步骤」；卡片滚出视口时输入框上方出现「Step N / M」。
  - **子 Agent**（`SubagentCard`）：Task / Agent 调用就在消息流里画成卡片（类型、时长、状态），不再是标题下和主 Agent 平级的标签；展开看 prompt、结果、最近几步和它再起的子 Agent，「查看完整过程」在侧边面板只读打开 `<父>@<子>`，主对话不动。
  - **用户气泡**靠右、最大 488 宽，超过 14 行收成 10 行；你自己发的（输入框、听写、快捷命令）是 iOS Agents 页的浅蓝（`--user-bubble`：浅色 `#DCEAFF`、深色 `#1D3557`，正文色文字，#2643），另一段 Persona 对话用 conversation_send 转来的仍是中性灰（`--neutral-bubble`，`lib/turns.ts` `isOwnMessage`）；回复不加气泡，正文 15 / 1.6，代码块用 highlight.js 高亮（按需加载）、自动换行。对话列宽 880。
  - **实时状态和流式**：Claude 工作时，输入框上方的运行状态条显示 node 从状态行读到的动词、计时、↓ token（`live` 推送，`docs/protocol.md`「实时状态」；没有时退回「运行中」+ 本地计时），一轮结束停 1 秒淡出。
    新到的回复逐词淡入（打开历史、翻页、展开拿到的不播）；助手主对话的 delta 节流（100–400 ms）、按空行分块只重算最后一块、补全未闭合的 Markdown。「设置 › 外观 › 回复动效」和系统的减弱动态效果可以关掉淡入。
  - **滚动**：在底部时跟着新内容走；滚轮向上、手指下拉、滚动键、拖滚动条一动就停止跟随，离底不到 100 px 往下滚或贴底时恢复；自己发送时回到底部；展开卡片时停 300 ms、按住点的那一行。
    不在底部时出「跳到底部」（有新内容时是「有新消息」）。打开、刷新、从侧栏切过来都停在最新消息（#1340）；滚到顶自动加载更早的两轮，位置不跳。
  - **同一个 tab 的几个 Agent pane**：顶栏下面一排标签（`PaneTabs`），带状态点和 Agent 类型，点了换对话；只有一个时不显示。
  - **按行画**（#433，`chatLines()` / `buildBlocks()` 照 iOS `ChatLines`、`ChatBlock.build`，`views/chat/Lines.tsx`）：工作中插入的消息并进当前这一轮；做完的一轮有几条消息也按消息一来一回地画（#2512）；「已中断」单独一行居中；别的会话发来的消息是「来自 X」卡片，`/loop` 的唤醒是一条小条。
    hub 发给模型的任务通知、定时任务、叫醒（`docs/protocol.md`「Persona 注入的消息」，`lib/injected.ts`）不是用户气泡：居中一行「任务「x」做完了」，点开任务；定时任务那行点开看任务的要求；不拿去认待发消息。
  - **后台任务唤醒**：被唤醒的工作留在原来那一轮，取最长的那段话当回答，下面「N 个后台任务完成 ›」小条，点开弹窗：每次唤醒一组（通知 + 回答），最新在上，中间的步骤折在最后。
    任务通知（iOS `TaskNoteRow`，`lib/tasks.ts`）显示类别、描述、结果胶囊，可展开看命令、「查看输出」在侧边面板打开；发起后台任务的那步工具行带「后台 · 已完成」之类的标记。
  - **离开期间回顾**：几条回顾折成一行，点开看全文（最新在上）；最新一条是对话最后一项时，它在对话末尾展开成全文卡片（`OpenRecapLine`，`openRecap`），和 Agent 的回复在同一个平面：往上翻跟着正文滚走，贴底时连同回顾一起完整露出来，展开 / 收起都在流里撑开或收回（#1360：放在输入框上方时会盖住回复的最后一部分）。点 × 或开始打字就关，关过的记在浏览器（只在 Agent 对话，同 iOS）；同一行折叠的更早几条留成小条放在卡片上面，关掉后整行变回小条（#870）。端到端检查：`MOCK_RECAP_LONG=1` 的假 hub 上跑 `scripts/test/web_recap_e2e.py <网页地址> [--shots <目录>]`（桌面 / 手机宽度 × 浅色 / 深色，用法见脚本开头）。
  - **API 报错卡片**（`lib/api-errors.ts` 照 `APIErrorInfo`）：原因、状态码、临时故障还是要你处理、连续几次，展开看请求 ID 和原文；能重试且是最后一行、Agent 没在工作时有「重试」（发「继续」）。
  - **循环 / 后台标签**：顶栏标题右边「⟳ 下次时间」「⚙ 后台 N」，点开弹窗：循环（频率、下次唤醒、要做的事、「停止循环」先确认再发 iOS 同样的那句话），后台任务列表（命令、已运行多久、看输出）。
  - **子 Agent** 的卡片见上面「对话页」（#1963 起不再有顶栏下的子 Agent 标签条和「全部子 Agent」弹窗）。截图用 `MOCK_SUBAGENT_CARDS=1 MOCK_WORK_GROUPS=1` 的假 hub（`scripts/test/web_chat_trace_e2e.py`）。
  - **消息的菜单**（#433，iOS `MessageMenu`，`views/chat/MessageActions.tsx`）：悬停回复下面出「复制 / 引用」和时间，你的消息在气泡左边出「复制 / 引用 / 编辑」；右键出同样的菜单
    （选中了文字、右键链接 / 图片 / 代码块时仍是浏览器的菜单）；触屏长按出菜单，多一个「选择文本」。引用不往输入框里写 `> …`：输入框上方出一条引用条（左竖线、浅底，「引用 <助手名 / Agent / 我> · 时间」和去掉 Markdown 的两行摘要，× 取消，再引用一条就换掉），输入框保持原样并聚焦；没开着的输入框引用存在 `quote:<草稿键>`。发送时照旧把引用拼成 `> ` 块放在正文前（`lib/drafts.ts` `withQuoteBlock`），助手和电脑上的 Agent 收到的和以前一样，协议不变。用户气泡（含历史消息、合并的气泡）开头的 `> ` 块画成气泡顶部的引用卡片（`views/chat/Quote.tsx`），最多三行，长的点开看全文；编辑一条带引用的消息，引用回到引用条、正文回到输入框。任务完成 / 没开起来的卡片（`views/assistant/Tasks.tsx` `OutcomeReceipt`，#2462）也能复制、引用：悬停时卡片右上角出「复制 / 引用」（不占卡片下面的位置），右键、长按出同样的菜单；引用条写「引用任务「标题」 · 时间」和 Agent 最后说的话的前两行，发出去的 `> ` 块第一行是 `任务「标题」（todo id: …）`，气泡里这一行画成任务标题，不露 todo id。编辑把那条放回输入框（输入框有字先确认）。分享卡片不做。
  - **回退到这里**（#2436，Moshi 的 Rewind to here；`lib/rewind.ts`、`MessageActions.tsx` 的 `rewindActions`）：你的消息的 ⋯ 和右键菜单里有「回退到这里 ▸ 只回退对话 / 回退代码和对话」，
    第二项是红色的、要再点一次「确认回退代码」；等 node 在终端里走完 Claude 的 `/rewind`（最长约一分钟）时那一行转圈，失败的原因显示在菜单里。成功后弹「已回退对话」，
    那条消息放回输入框（输入框里有别的字先确认）。被回退的几轮由 node 在读 transcript 时去掉（`docs/protocol.md`「回退到某条消息」），`history` 变了，所有开着这个会话的网页都重新取，不在网页里单独藏。
    不是 Claude 的会话（Codex…）、会话在工作 / 等回答 / 没在运行时这一项是灰的，右边写原因（仅 Claude / 工作中 / 待回答 / 未运行）；助手对话和子 Agent 没有这一项。
    要对上的是「从最后数第几条一样的话」和「后面还有几条」，对不上 node 什么都不改。假 hub：`MOCK_REWIND=1`（四条消息，两条「继续」）。
  - **图片查看器**（`ImageViewer.tsx`）：整段对话里已加载的图片左右翻（←/→、两侧箭头、左右滑），顶部 n / m，按屏幕大小适配（小图也放大）；缩放（#1753，`lib/image-zoom.ts`，同 Android 的 1–6×）：滚轮、触控板捏合、触屏双指以指针为中心缩放，放大后拖动平移、不拖出边，双击 / 双击屏幕在那一点放大（2.5×，原图更大时到原始大小，最多 6×）或还原，`+` / `-` / `0` 也行；放大时左右滑是平移、点旁边不关闭（双击还原），翻页时复位；
    「编辑」（`ImageEditor.tsx`，canvas）：框选、画笔、6 种颜色、撤销、清除，按原图分辨率导出加到输入框；输入框里的图片附件点一下也能标注。「拷贝」只在 HTTPS / localhost 出现（明文 HTTP 写不了图片剪贴板）。
  - **为什么等你、在飞书打开**（#1752，iOS `AttentionNote`，`lib/format.ts` 的 `sessionPreview` / `externalLink`）：会话「需要你」且 hub 带了 `attention_reason`（botmux 举手的原因，`docs/engines.md`「botmux」）时，
    列表行（总览、机器页、侧边栏的悬停提示）用原因代替最后一句话，输入框上方一条提示写原因；有 `external_url`（只认 https）时提示条、对话 ⋯ 和「会话信息」里有「在飞书打开」（经 `openLink`，新标签页）。列表行还包括知识库详情的会话。没有这两个字段时界面不变。截图用 `MOCK_BOTMUX=1` 的假 hub。
- **会话在等什么**（#1837，[agent-work.md](agent-work.md)）：hub 分诊过的停着的会话，列表行的那一行字换成工作态：做完的「✓ 结论」，在等的「在等什么（N 分钟后叫醒）」；
  hub 会替它等的（自动重试、到点叫醒）排进「待唤醒」；后台任务还在跑的 hub 直接算工作中（#2228）。开关在「设置 › 通用 › 会话分诊」（默认全关）。截图用 `MOCK_WORK=1` 的假 hub。
  - **排队消息**（`QueueStrip.tsx`，`POST …/queue {keep}`，hub 转 node 的 `session.queue.edit`）：输入框上方「排队中 N 条」，每条「撤回编辑」（取回放进输入框末尾）、拷贝、删除；
    已经进了队列的不再同时显示成「发送中」气泡。
  - **「@」**（`MentionMenu.tsx`、`lib/mentions.ts`）：输入框打 `@` 或点工具条的 @：这段对话工具用过的文件、会话目录第一层（带 `/` 逐级进入，`/machines/{mid}/fs`）、其他会话；
    选文件插 `@路径`，选会话不插字、加一个上下文胶囊（随消息作为 `<jarvis-context>` 发出）。邮箱地址里的 @ 不弹。工具步骤折叠时（`fold_work`）没展开的那些文件不在列表里。
  - 交互卡片（提问 / 计划 / 授权 / 终端对话框）在输入框上方，Approval Card 的样子；× 是「交回电脑处理」（`dismiss`）。
  - 输入框（Prompt Bar）：Enter 发送、Shift+Enter 换行、输入法组字时 Enter 不发送；`/` 命令（同 App 的快捷消息，`/clear` 先确认）；
    `/loop`（#1586，`lib/loop-command.ts`，App 还没有）：选中填入 `/loop `，草稿是 `/loop …` 时输入框下面一行说会做什么（「每 10 分钟重复：…」「由 Claude 自己定节奏重复：…」）或还缺什么，
    原样发给 Claude Code；只有 Claude 的 Agent 有，Codex / TRAEX 的会话不显示；
    粘贴 / 拖入 / 选择图片作为附件，别的文件传到会话所在的电脑并把路径插到光标处（`docs/protocol.md`「发送文件」）；
    Claude 输入框里的灰色建议显示成占位文字，Tab 填入；Agent 工作中且没打字时发送键换成红色中断，Esc 也是中断；Persona 的对话（主对话也算）一样（#1467：之前助手的对话一律不给中断，hub 和 node 其实支持，claude -p 结束进程、下一条消息从 transcript 接着）。草稿按会话存在 localStorage。
  - **发送历史和 Ctrl+R 搜索**（#2434，`lib/send-history.ts`、`views/chat/useSendHistory.ts`、`HistorySearch.tsx`，对照 Moshi；只在有精确指针的设备上，触屏没有）：每个会话在这个浏览器里记最近 100 条发出去的消息（`persona.sent.<会话>`，发送成功才记，重复的挪到最后；不上传）。
    输入框为空或正显示某条历史时，↑ 取更老的、↓ 取更新的（走过最新一条回到空框；多行的历史要光标在第一行 / 最后一行才翻，其余时候方向键归输入框）。输入框里打 Ctrl+R（Mac 也是 ⌃，⌘R 仍是刷新）在输入框上方开 `^R` 搜索面板：
    按空格拆词、每个词都要出现（不分大小写），最新在前，最多 100 条，候选来自**所有会话**的历史；↑↓ / Ctrl+R / Ctrl+P 上移，Ctrl+S / Ctrl+N 下移，Enter 或 Tab 把选中的放进输入框（不发送），Esc 或 Ctrl+G 取消；没有匹配时「没有匹配的消息」。输入法组字期间所有键都不算。输入框的悬停提示写着这两个键。
  - 发送带 `client_msg_id`；被拒（`input_draft`）时弹窗显示电脑输入框里的字，能清空的给「清空并发送」（同一个 id 重发，带 `clear_draft`）。
  - 同空间的其他 Agent 在右上角「同空间 N」菜单里，每行显示状态圆点和文字标签（需要你 / 工作中 / 完成未看 / 已看），按 tab 顺序固定；状态随推送更新，不依赖颜色辨认。
    当前 Agent 保留勾选标记；长标题单行截断、悬停可看完整标题和状态，窄屏菜单宽度不超出视口。有别的需要你时按钮带红点。
  - 顶栏右侧：文件（在右侧面板打开，见下面「侧边面板」）、**终端 | 对话** 切换（见下面「终端 ⇄ 对话」）和「⋯」：终端（切到终端视图，再点回对话）/ 文件 / Git、会话信息（`/info`：运行、用量、位置，拷贝 ID 和恢复命令）、
    重命名（herdr 标签名和 Claude 的会话名分两栏，只发改了的，同 iOS）、重命名空间（`/machines/{mid}/spaces/{wid}/rename`）、
    屏蔽通知 / 恢复通知（`PUT /mutes`，Agent 自己的开关叠在空间的屏蔽上：空间屏蔽时单独开启进 `allowed`，`src/lib/session-actions.ts`）、
    拷贝会话 ID / 目录 / 恢复命令，关闭 Agent（先确认），未运行的会话可以删除对话（先确认，只从列表移除，`docs/protocol.md` DELETE `/sessions/{mid}/{sid}`）。
  - Claude Code 的会话在输入框下方有两个胶囊：权限模式、思考强度（`POST …/modes`，先改本地、失败恢复；「全部放行」只能新开时选，不在列表里）。
    Claude Code 终端会话的思考强度胶囊（样子不变）点开是**模型与思考强度浮层**（#2434，`ModelEffortPopover.tsx`、`lib/model-switch.ts`，对照 Moshi 的模型胶囊）：上面是思考强度的几档（该模型不支持的档灰掉），下面是模型列表
    （`GET /machines/{mid}/launchers/{name}/models`，就是新建 Agent 选模型用的那份，按会话的启动器取；当前模型带「当前」）。↑↓ 选模型、←→ 选档、Enter 应用、Esc 取消、点浮层外关闭；应用先 `POST …/modes {"model"}` 再 `{"effort"}`，各自先改本地、失败恢复。
    输入框里只打 `/model` 按 Enter 不发送，改为打开这个浮层（没有模型切换的 Agent 照常发送）。node 收到 `model` 后用和发消息一样的安全路径往 Claude Code 的输入框打 `/model <id>`：先读屏，对话框开着（`dialog_open`）或输入框里有草稿（`input_draft`）就拒绝、什么都不打，
    Agent 正在工作也拒绝；打完再读屏确认出现 `Set model to`，没出现就报错而不是当成功。模型 id 只收一个词（字母数字和 `._:/@[]-`），不是命令也不是参数。注意：新版 Claude Code 的 `/model <id>` 会同时把所选模型存为新会话的默认（本机 transcript 里的输出是「Set model to `…` and saved as your default for new sessions」），没有只改这个会话的写法，浮层里也写了这句。Codex 的线程保持只有思考强度，Persona 的对话在自己的胶囊里换模型，其他 Agent 没有这个入口。
    Codex、TRAEX、runtime 的会话和助手没有。
  - 发送中的气泡把 `<jarvis-context>` 显示成引用卡片，和记录里的一样（以前会露出原始标签）。
  - 打开页面、页面回到前台时发 `seen`。
- **Persona**：`/`（和旧的 `/j`）是主对话，`/j/new` 是新对话（建议 + Prompt Bar，`POST /api/v1/assistant/conversations`），`/j/<id>` 是一段对话，无头助手的 `delta` 逐字流式显示。
  - **形象**（#433，`lib/persona.ts`、`views/assistant/Persona.tsx`，`docs/persona.md`）：hub 的 `/personas` 形象包，选哪个存在当前浏览器；用在新对话页的大图标、对话顶栏标题左边随状态变的圆点
    （状态同 iOS `personaState`，APNG 由浏览器自己播，减弱动态效果时用 canvas 画第一帧）、设置 › 助手的头部、「问 xxx…」占位符。选默认形象（或旧 hub、没有包）时每处都是原来的样子；
    形象的配色不改网页的 token。选择在设置 › 通用 › 形象和资料页。
  - **资料页**（`/assistant`，`views/assistant/Profile.tsx`，照 `AssistantProfileView`）：头像、名字、运行在哪、在线 / 下一件，接下来（定时任务），形象，主动做事 / 了解你 / 运行在，能访问。
    入口：新对话页右上角「资料」、对话 ⋯ 菜单「详细信息」（⋯ 菜单还有知识库、回到主对话、拷贝信息）。
  - **对话里切换启动器**（`lib/assistant-chat.ts`、`AssistantChat.tsx`）：输入框下方启动器胶囊，菜单「下一条消息起使用」列出助手所在电脑能跑助手的启动器（同 iOS `runsAssistant`），
    之后这段对话每条消息带 `launcher`；选择只在这个页面里（iOS 也只在内存里）。新对话页的胶囊选新对话用哪个。
  - **跨页的待发消息**（`lib/outbox.ts`，store 的 `outbox`，照 iOS 的 outbox）：知识库「问 Persona」、收件箱「交给 AI」、Agents「交给 Persona」、新对话页的第一条消息，先跳过去、马上显示气泡，
    等记录里出现同一条才消失（已送达的最多等 60 秒）；失败的留着写原因，给「重试」（同一个 `client_msg_id`）/「删除」，电脑输入框有字能清空时给「清空并发送」。
- **全部对话**（`/history`，`src/views/assistant/History.tsx`）：助手的全部对话，主对话置顶，其余按今天 / 昨天 / 最近 7 天 / 更早分组，搜标题和最后一句；
  行菜单重命名、拷贝 ID、归档 / 取消归档（主对话不能归档），「已归档」一段默认收起（`GET /assistant?archived=1`）。
- **定时任务**（`/routines`，`src/views/assistant/Routines.tsx`，`docs/assistant.md`「定时任务」）：定时任务和一次性跟进两段，行尾开关直接启停；
  编辑浮层改重复方式、提示词、运行方式（全部放行先确认），保存 / 删除 / 立即运行（跳到那段对话）/ 下次开新对话；「新建」可选模板，浮层只有「取消 / 添加」；
  右栏「接下来」和按天分组的运行记录。真实 hub 的 `PUT /assistant/tasks/{id}` 整体替换字段，所以连开关也带完整任务；
  启动器、思考强度只在这次改过才发（统一模型配置那边存的值不被盖回去，`docs/model-config.md`），hub 有统一模型配置时给一个跳过去的链接。
- **收件箱**（`/inbox`、`/inbox/<件>`，`src/views/inbox/`，`src/lib/inbox.ts`，`docs/inbox.md`）：飞书、邮件、云文档、公司圈和连接器（GitHub、Telegram、信息、扩展的）一个列表，
  需要你 / 知悉即可（全部看过）/ 已折叠（全部清除）/ 已处理（收起，可移回）四段；筛选全部 / 飞书 / 每个邮箱 / 每个有条目的连接器 | @我 / 私聊 / 群聊 / 邮件 / Agent，搜标题、发件人、内容、摘要。
  - **等分拣的横幅**（#1773，`views/inbox/TriageBanner.tsx`，文案 `lib/inbox.ts` 的 `triageBanner`，规则见 `docs/inbox.md`「等分拣的横幅」）：列表回包的 `triage` 存在收件箱的 store 里，
    筛选下面、第一段上面一条 `bg-orange-tint` 的横幅（`role="status"`，时钟图标）；「去配置」打开 `/settings?tab=model-config&purpose=inbox.triage`（网页总是完整权限的设备，按钮总在）。
    截图用 `MOCK_INBOX_TRIAGE=unconfigured`、`behind:8:飞书`、`error:7:飞书,邮件,GitHub` 的假 hub。
  - **连接器的条目**（#1324，`?sources=all` 拉列表，按条目的 `connector` 块画，不认具体来源）：标签是 `connector.label`，第二行「example-org/example-repo#12 · 谁：内容」，头像角上是连接器自己的标志（GitHub、Telegram）或灰色的通用标。
    详情（`ConnectorThread.tsx`）先读 `/connectors/<id>/thread`：GitHub 的 PR / issue 画成 GitHub 的时间线（状态、分支、标签、描述、评论、review、代码行评论带 diff、CI 结果和失败日志），
    聊天类（Telegram）画成气泡（图片、贴纸、视频封面经 `/connectors/<id>/media`，语音和文件只描述），可加载更早；会话页带回复动作（`chat.reply`）时底部有输入框，走 `thread/reply`（聊天可引用某条）。
    读不到会话页（CI 失败这种不是会话的、限流、没有 threads 的连接器）就显示 hub 收集的内容（`GET /inbox/<件>` 的 `context`）和「在 X 打开」；限流 / 不可用时上面说一句原因。
    `can_reply` 的才有「AI 草拟回复」（回复走 `/inbox/<件>/reply`，弹窗里写明回复会到哪、谁能看到）；不能回复的有 `open_url` 就换成「在 Telegram 里回复」「在信息里回复」（`sms:` 带上 AI 分身的草稿），都没有就只有「交给 AI」。交给 AI 的标题是「连接器名 · 标题」，正文带上它的链接。
  等你的 Agent（待处理交互，屏蔽的除外）排在需要你最前，点了进那个对话。悬停一行出现「已处理 / 移回」（网页没有左滑），先改本地、失败恢复。
  AI 分身草拟好的行下面是草稿卡片（发送 / 修改），处理中写「AI 分身正在处理：…」。宽时左列表右详情，窄时详情盖住列表；按页面自己的宽度切（容器查询），开着侧边面板也不挤。
  详情：扩展的连接器收进来的事，扩展有 `inbox_item` 时正文由扩展画（见下面「设置」的「UI 插槽」）；其余照旧。
  飞书会话只读（停在第一条新消息，「以下是新消息」分隔，可加载更早 / 更新，`lark` 推送命中时刷新；不做表情、资料卡、群设置——不做第二个飞书）；
  邮件的 HTML 放在不允许脚本的沙箱 iframe 里（`allow-same-origin` 只为量高度、拦链接）；云文档列出提到我的行和状态菜单（`/doc/status`）；公司圈是帖子和评论。
  操作条：✦ 摘要、「AI 草拟回复」（起草、「让 AI 改」重写、发送走 `/reply`）、「交给 AI」（Persona 新对话或最近 8 个在跑的 Agent，默认同时标已处理）。
  侧边栏角标 = 未处理里需要你（含还没分拣的）的件数；`inbox` 推送和重连时重拉。
  - **飞书可见即读**（#433，`lib/lark-read.ts`，照 iOS `LarkPager.readReport`）：消息至少一半在列表里、页面在前台、首屏定位好了、没有对话框盖着才算看到，最后一条看到后 450 ms 合并上报
    （`POST /lark/conversations/{id}/read`：位置 + `badge_count` + 别人未读的消息 id），切后台 / 离开时立刻报；只往前报，失败退回下次重报，话题群不报。
    从收件箱打开时首屏看到的也报（从这件事的第一条新消息起），按 `docs/inbox.md` 2026-10-31 的决定（iOS 代码还是首屏免报的旧行为）。
  - **写邮件 / 回复**（`ComposeDialog.tsx`、`lib/mail.ts`，`POST /mail/send`）：顶栏「写邮件」，邮件详情「回复」（Re: 主题、In-Reply-To / References、引用前 40 行）；纯文本、不带附件（同 iOS）；
    取消丢掉草稿，Esc / 点外面关掉留着（只在页面内存里）。`references` 按 hub 的字符串发。
  - **打开邮件标已读**：没处理、没读过的邮件打开时 `POST /mail/messages/{aid}/{uid}/flags {seen:true}`，失败忽略（同 iOS）。
  - **翻译**（`lib/translate.ts`）：开了翻译时，不是目标语言的邮件打开时 `POST /translate/mail`，正文换成译文、上面「已翻译成… · 显示原文」；hub 预翻好的（`translations[目标]`）直接用在列表行和标题上。
    设置里选中的飞书会话，别人发的外语消息下面一行译文（`POST /translate`，攒批）。语言判断按书写系统 + 常用词（浏览器没有 iOS 的 NaturalLanguage），拉丁语系之间可能判错。
  - **飞书聊天的 Pin**（#1327，`lib/lark-pins.ts`、`inbox/LarkPins.tsx`，照 iOS `LarkChatPins`，`docs/lark-connector-migration.md`「聊天内 Pin 消息」）：飞书详情头部的图钉按钮打开 Pin 列表（对话框），
    `GET /lark/conversations/{id}/pins` 按游标翻页、接近底部自动加载；lark-connector 读列表会把 Pin 标为已读，所以只在打开列表、改了 Pin、列表开着时收到这个会话的 `lark` 推送才读，不预取，只放页面内存。
    点一条先 `GET …/pin-messages/{消息}` 确认能定位，再关列表：普通消息把聊天换成它周围那一页、滚到它并闪一下；话题里的回复在「话题」对话框里打开（话题首条 + 回复，高亮那条）；找不到时留在列表、聊天位置不动。
    消息悬停条的「Pin」（hub 给的 `can_pin`，撤回 / 删除 / 发送中的不给）先确认再 `POST …/pins`，带当前飞书账号；列表里有 `can_unpin` 的可移除（行内确认）。
    写入结果不明（超时、`lark_pin_result_unknown`）不重试，先重新读列表；换号 / 退出登录清空并停住。旧 hub（没有这个路由的 404 / 405）提示升级 Hub，旧 lark-connector 的 426 提示升级 lark-connector，原消息不存在不误报。
- **消息**（`/messages`、`/messages/<会话>`，`src/views/messages/`，`src/lib/lark-chats.ts`，`docs/lark-connector-migration.md`「入口」）：#392 的窄例外，
  只在这个浏览器打开了「设置 › 连接器 › 飞书 › 首页显示消息」（默认关，存 `localStorage` 的 `lark.homeMessages`，和 iOS 存在本机一样，不同步到 hub）时侧边栏才有「消息」；hub 的 features 里还要有 `lark`（#1530）。
  浏览全部私聊和群聊：`GET /lark/conversations?scope=chats&account_bound=1`（60 条一页，滚到底加载更多）和 `GET /lark/pinned?account_bound=1`（置顶一排头像，顺序和未读跟服务端）；
  点进去复用收件箱的飞书聊天页（`LarkThread`：消息流、回复、可见即读、Pin）。搜索只搜已加载的会话。`lark` 推送后重读已加载的分页窗口，整批成功才替换（退出的会话这时消失），失败保留原列表；
  hub 说账号换了就立刻丢掉旧账号的行、迟到的回复不合并。列表只在这个标签页的内存里，不落盘。宽窄同收件箱；右上角进飞书设置。关着时打开这个地址只提示去设置里打开。
  不做通讯录、工作台等页面（不做第二个飞书）。
  多个飞书账号（#1321，`lib/lark-accounts.ts`，`docs/feishu.md`「多账号」）：搜索框下一排账号按钮（带未读数），每个账号一份列表，选哪个记在这个浏览器
  （`localStorage` 的 `lark.account`）；别的账号的会话、收件箱条目所有请求带 `ol_account`，会话头写「群聊 · <名字>」。
- **知识库**（`/kb`、`/kb/<类型>`、`/kb/<类型>/<条目>`，`src/views/knowledge/`，`src/lib/kb.ts`，`docs/knowledge.md`）：类型胶囊、按标签筛选、项目分重点 / 其他进行中 / 已完成 / 不是项目，
  全文搜索按条目合并（最多两处命中、加粗）；条目页：在做它的 Agent、时间线、档案、关联、代码目录、最近 7 天消息（某一天在弹窗里看）、目录路径，
  「打标」改重要级 / 相关性 / 标签 / 项目类型和状态，「问 Persona」发进主对话（没有就新开）。宽窄同收件箱，条目页盖住列表时列表不卸载，滚动和搜索都留着。
  `<jarvis-context>` 的属性按 `kind`、`title`、`url` 的顺序写：iOS 的解析要求这个顺序，否则手机上不显示卡片。
- **个人 Agent**（`/a`、`/a/<整个 id 编码>`，`src/lib/agents.ts`、`src/views/agents/`）：lark-connector 配对的个人 Agent（`/api/v1/agents/*`，
  `docs/protocol.md`）。侧边栏「个人 Agent」按 provider 分组、组内按最近排序，带未读数，会话多于 6 个时可筛选；`/a` 是带搜索的全量列表。
  会话页：打开即 `POST …/read`；「加载更早的消息」用 `nextCursor`，按 `externalId` 去重、按 `position` 排序；tool-call 折叠，未知 block 原样显示 JSON。
  发送带新的 UUID `clientId`，失败可重试（同一个 `clientId`，hub 幂等）；持久化后的消息没有 `clientId`，本地气泡按文本 + 时间收敛；`canSend=false` 时输入框禁用。
  WS `{"type":"agents","session_ids":[...]}` 时刷新列表，打开的会话在 id 命中（或列表为空）时重拉最新一页。

  ![网页版侧边栏的个人 Agent：按来源分组，带头像、未读数、搜索和筛选按钮](screenshots/web-personal-agents.png)

- **设置**（`/settings`，侧边栏和左上角菜单进入，`src/views/Settings.tsx`）：分类是否显示按 hub 能力决定，和 App 的设置同一套接口。渠道是独立分类，紧挨在连接器前（#2196）；设置没有「认识 Persona」入口，资料页仍从对话菜单打开。
  「通用」是外观（跟随系统 / 浅色 / 深色）、形象、Persona 助手的运行位置（模型在「模型配置」tab，#1291）、这个浏览器（hub 地址、hub 名、互联的 hub、断开）；
  「助手」（`settings/AssistantSettings.tsx`）：主动做事（进定时任务）、关于我（`PUT /assistant {notes}`，点保存或离开时存）、
  记忆（`/assistant/memory`：标题先取 MEMORY.md 索引里的中文标题，查看 / 编辑整个文件 / 删除，索引只读）、Skills（`GET /skills`，跑助手那台电脑上的 skill，只读，`settings/Skills.tsx`）、运行在哪（只读；没有模型入口，模型在「模型配置 › 主对话」）；
  「Claude Code」「Codex」（`settings/AgentSettings.tsx`）：选一台在线电脑，白名单设置（开关 / 下拉 / 文字）改了立即 `PUT`，「同时改所有在线机器」默认开，
  每行「⋯」恢复默认（文字项清空也是 null）、推到所有在线机器，失败回滚；
  「连接器」（`settings/Connectors.tsx`）：邮箱（列表、添加 IMAP / 飞书邮箱、删除；Gmail 只能在手机上加：它的 Google 授权客户端是 iOS 类型，回调是只有 App 能接的自定义 scheme）、
  飞书（`/lark/status` 只读）、飞书机器人（`settings/FeishuBot.tsx`，逻辑 `lib/feishu-bot.ts`，`docs/bots.md`；一行状态，点开是弹窗（`?tab=connectors&connector=feishu-bot` 直接打开）：开关、应用凭证「验证并保存」、
  配对码和倒计时（等配对时每 3 秒刷新）、测试消息、解除配对先确认、「能做什么」六个开关、飞书里聊过的 Agent、怎么建机器人；同 iOS `FeishuBotView`）；
  hub 带 `connectors` 时下面是「更多连接器」（`settings/HubConnector.tsx`，逻辑 `lib/connectors.ts`，`&connector=<id>` 直接进详情，#1324）：
  GitHub、Telegram、信息和扩展提供的连接器，一行一个（标志、名字、状态圆点和账号）。详情照 iOS `HubConnectorView`：状态、登录来源、最近一次同步、错误（限流时灰字）和「怎么修好」的步骤；
  能在这里登录的（`login`）有「同步 X」开关、分步登录（按 hub 给的字段填，`secret` 的是密码框；GitHub 的设备码大字显示、可复制、按 `interval` 自动再问；微信这类 `kind: qr` 画 hub 给的二维码、能「保存二维码」、按 `interval` 自动再问，二维码和渠道页共用 `settings/LoginSteps.tsx`；只缺点阵时给可复制的链接；画不了的步骤（不认识的 `kind`、没点阵也没链接、没有字段）说明画不了和下一步、给「重新开始」，不留灰掉的「下一步」，#1693）、进知识库 / 进收件箱、
  删除已存的知识库内容、退出登录、删除已存的数据（都先确认）；刚打开、进程还没报能力的每秒再问一次（最多 15 秒）。GitHub 另有「从哪里登录」（各台电脑上的 gh 或设备码，`/connectors/github/auth`）、
  权限不够时缺哪些 scope 和影响什么、收件箱选项（CI 失败档位、静音仓库，`PUT …/github/settings {options}`）。信息（iMessage）只看状态，「交给 Persona」和删除在手机上；「这台 iPhone」只在手机上有；
  「hub」（`settings/HubSettings.tsx`，逻辑 `lib/hub-update.ts`，`docs/hub-update.md`「App 里」）：版本、渠道、上次检查、检查更新，新版本和说明、回滚过 / 自动更新不装的提示、
  「更新 hub」，等空闲时列出在忙的 Agent 和「现在就更新…」（先确认），Docker 部署给要改的镜像和命令（可复制），自动更新开关、首次「已开启」提示、各台电脑上的 personad 版本、更新记录；
  跟着 `hub_update` 推送刷新，旧 hub（没有这个接口）只显示一句说明；
  「扩展」（`settings/Extensions.tsx`，逻辑 `lib/extensions.ts`，`?tab=extensions`，`&extension=<id>` 直接进详情；hub 的 features 有 `extension_manager`（老 hub 是 `plugin_manager`）才出现，#1207；老链接 `?tab=plugins&plugin=<id>` 照样打开）：
  和 iOS 同一套（`docs/extensions.md`「App：设置 › 扩展」）。列表是名字、状态圆点和文字、还缺什么 / 错误，标题旁写个数和几个要处理；详情是状态 · 版本 · 来源、描述、还缺、错误，
  「启用」开关（立即 `PUT …/-/enabled`），按 `form.fields` 画的表单（文字、数字、开关、选择、文字列表 / 多选，对象单独一组），改了底部浮出「还原 / 保存」，整份 `PUT …/-/config`，
  hub 回 `invalid_config` 时把原因写在那一项下面、草稿留着，数字不是数时不能保存；有未保存的改动时点返回先问。密钥每项只显示已设置 / 未设置，点开在弹窗里输入（`type=password`，不回显），
  已设置的可以清除（再点一次确认）；「提供」「它可以」「关于」（版本、上一个版本、来源、安装自、发布者、https 的主页、警告）。hub.json 来的只读：没有开关、表单、密钥。
  装、升级、卸载在 hub 的管理界面（`/admin`）或 hub 机器上（`personad extension`）；hub 有扩展市场（列表回 `markets: true`）时也能从市场装（下面）。截图用 `MOCK_PLUGINS=1` 的假 hub。
  **新老接口**（#1845，`lib/extension-api.ts`，[extensions.md](extensions.md)「命名和兼容」）：hello 的 features 有 `extensions` 时问 `/api/v1/extensions/…`（字段 `extensions` / `extension` / `extension_name`），
  没有（老 hub，或 hello 之前）问 `/api/v1/plugins/…`（`plugins` / `plugin` / `plugin_name`），两套字段都读；市场预览 `{market, extension, plugin}` 两个键都带。
  事件收 `extension_ui`；hub 有新接口时它也会发一份老的 `plugin_ui` 给老 App，网页只在老 hub 上读 `plugin_ui`。市场的签名状态（#1848 的 `signature`）写在市场那一行（「签名已验证」「未签名」……）。
  **渠道**（#1466、#2196，`settings/Channels.tsx`，逻辑 `lib/channels.ts`，[channels.md](channels.md)；hub 的 features 有 `channels` 才出现）：设置里的独立分类，放在连接器前，不再嵌在扩展列表中。
  点进 `?tab=channels`：分「Persona」（各端）、「已接入」、「可以安装」（市场里的，点了进市场的详情）三组，每行是渠道的标志（飞书用自己的，其余按种类的图标）、名字、账号或说明、状态圆点和文字。
  单个渠道 `?tab=channels&channel=<id>`（旧的 `?tab=extensions&extension=-channels[/<id>]`、`?tab=plugins&plugin=-channels[/<id>]` 链接仍打开对应渠道）：状态、账号、最近一条；「启用」；没绑时「绑定」按 `bind` 分步走（填字段、扫码时画 hub 给的二维码并能「保存二维码」、配对码时大字显示并轮询），绑了是账号和「解除绑定」（先确认）；
  「消息发到」（主对话 / 新对话 / 选一段助手的对话）；按 `features` 画的开关；在等几个回答和提问、最近进过的对话；它能做什么；「发一条测试消息」（只发给自己）；有 `settings_url` 的多一行「更多设置」（飞书机器人进原来的弹窗）。
  只读的（Persona、飞书 /agent、语音）只显示信息。连接器页的微信、飞书机器人顶上有一行链到这里。对话页里从渠道来的消息（`client_msg_id` 是 `<渠道>-…`）气泡下面一行「来自 微信」，
  对话列表在标题旁画最近那个渠道（会话的 `channels`）的小标志。截图用 `MOCK_CHANNELS=1` 的假 hub。

  ![渠道列表（浅色；截图为入口调整前）](screenshots/web-channels-light.png)

  ![微信渠道的设置（深色）](screenshots/web-channel-wechat-dark.png)

  ![扫码绑定微信（浅色）](screenshots/web-channel-bind-qr-light.png)

  ![对话页里来自微信的消息（深色）](screenshots/web-channel-source-dark.png)
  **声明式 UI**（#1330，`settings/ExtensionView.tsx`，逻辑 `lib/extension-ui.ts`，`docs/extensions.md`「声明式 UI」）：扩展详情多一组「页面」（它的 `ui`），点进去是 `&extension=<id>/ui/<页面>`，
  一个组件画全部八种条目：text（四种样式）、metric（连着的几个并排）、progress、row（徽标、可点）、button（连着的放一行，destructive 是红字，submit 是主按钮）、link（只认 https，`openLink` 打开）、field（文字、密钥不回显、数字、开关、选择）、divider；
  tone 换成角色色（neutral → accent / 正文色，positive → green，caution → orange，danger → red）。按钮有 `confirm` 先弹确认框，`submit` 带上这一页全部 field 的值（开关是 `"true"` / `"false"`，数字不是数就不发），
  回答里的新 view 换上、`toast` 弹出、`open_url` 用网页的链接打开方式，什么都没有就重画；画不出来是空状态加「重试」。
  「模型供应商」的启动器详情里，hub 带扩展底座时按 `GET /extensions/-/ui?placement=launcher_section&launcher=<名字>[&machine=<第一台在线的电脑>]` 每个扩展加一节（标题是扩展给的名字）；
  画的时候扩展回 404 `{"code": "unsupported"}`（不写 `hosts` 的一节会列在每个启动器上，扩展说这不是它的网关，#1339）就把这一节整个藏掉，不算出错、不显示错误。
  **UI 插槽**（#1451，`docs/extensions.md`「UI 插槽」；列表和角标 `lib/extension-slots.ts`，三处的画法 `views/extensions/ExtensionSlots.tsx`，图 `views/extensions/ExtensionChart.tsx`，逻辑 `lib/extension-ui.ts`）：
  hub 的 features 有 `extension_ui_slots`（老 hub 是 `plugin_ui_slots`）才问。`GET /extensions/-/ui`（不带 placement）一次拿齐，重连（hello）、在这里启用 / 停用 / 改设置 / 装 / 升级 / 卸载扩展后、标签页隔一分钟以上回到前台时再拿；
  `nav_page` 是侧边栏「机器」和「设置」之间一页一行（固定的八个图标换成网页的，不认识的画成扩展图标；角标按 tone 上色，收到 `extension_ui` 事件（老 hub 是 `plugin_ui`）就地改，不重拉），点进 `/extension/<扩展>/<页面>`；
  `overview_card` 是总览问候语下面一排卡片（一张占满，多张两列），紧凑画法（`context.size: compact`），点卡片空白处或标题打开 `opens`，卡片里的按钮、行、图照常能点；
  `machine_section` 是机器页最后（启动器后面）每个扩展一节，带 `context.machine`。每次画、每个动作都带 `context: {tz, locale, platform: "web", ui_version: 3, …}`；
  不认识的条目跳过、被它们清空的一节拿掉（有 footer 的留着），只有整个 View 读不出来才显示出错。`chart` 是 SVG 柱状图（并排 / 堆叠，柱宽不超过 24px、圆角在自由端、堆叠段之间 2px 间隙）和折线图，
  悬停或方向键看这个标签下每个系列的值，横轴标签隔几个写一个、不重叠，紧凑时只画首尾标签；没 tone 的系列按 `--series-1…4`（第一个是主色，避开 green / orange / red 三个角色色，深浅色两套）取色。
  `stat_grid` 窄时一行 2 个、够宽 3 个、很宽一行放全；`segmented` 选了马上发 `ui.action`（`values: {name: 选的值}`）。`refresh_after` 只在页面可见、组件还在时按时重画，回到前台已经过点就马上画。
  截图用 `MOCK_PLUGINS=1` 的假 hub（`example-gateway` 的用量页、今天的用量卡片、机器页的「网关调用」）。
  **UI 版本 3**（#1850，`docs/extensions.md`「声明式 UI」；画法 `views/extensions/ViewBody.tsx`，加载和跳转 `settings/ExtensionView.tsx`，读法 `lib/extension-ui.ts`）：
  `markdown` 只画安全的一部分（标题、段落、列表、强调、代码、引用、https 链接；不认 HTML 和图片，链接在网页里打开），`table` 窄时横向滚动、单元格不换行，`empty` 是空状态加一个按钮，`loading` 是一行转圈；
  行可以带固定图标（`icon`，换成网页的图标）、头像（https 图片，加载不了画首字母）或缩略图（https，不带任何凭据）；组件的 `key` 用作 React 的 key，重画时列表停在原处。
  View 的 `search` 是顶上的搜索框（停 0.3 秒后发 `values: {q}`），`filter` 是旁边的下拉（选了马上发，连同 `q`）；一节的 `more` 是最后一行「加载更多」，滚到时自动问一次（每个新 View 一次），也能点。
  动作的 `push` 不问扩展，直接在这个视图自己的页面栈上推一页（画那个 `ui`，带 `state`），回答里的 `push` / `pop` 同理；栈最多 8 层，整页时浏览器的后退会退回上一页，顶上一行「‹ 上一页的标题」；
  卡片和别的页面里的一节（总览、机器页、启动器页）没有栈：push 打开扩展的整页 `/extension/<扩展>/<页面>?state=…`。`extension_ui {invalidate}` 让正在显示的页重画（一秒最多一次），不在屏幕上的等回来再画。
  助手回答里的 ```` ```persona-view ```` 和 ```` ```persona-card ```` 用同一套组件画（`ui/AssistantCard.tsx`、`lib/assistant-view.ts`）：前者是一个 View 或一个组件，后者按 `sdk/go/wire/personacard.go` 的规则换成 View，
  `schema/extension/examples/persona-card/` 的每个用例都在单元测试里逐个比对；按钮、字段这类会动的不画，读不出来（还没写完、写错了）的照旧是代码块。截图：`MOCK_PLUGINS=1` 的 `example-gateway` 的「笔记（组件 v3）」页、`MOCK_CARDS=1` 的主对话。
  `inbox_item`（#1681，`docs/extensions.md`「收件箱条目」）：同一个缓存的列表里带 `connector` 的那几项，打开一件事时按条目的 `source` 找（`useInboxItemPlace`），不另外问 hub；
  找到了就由 `views/inbox/InboxExtensionBody.tsx` 在详情的头部下面画扩展的 View（`context.item` 是条目 id），条目的 `version` 变了（来了新消息）就重画、正在填的字段留着；
  hub 回 403 / 404（只读设备、条目没了、连接器不归它）直接画原来的正文，扩展出错时也画原来的正文，上面一行「…的详情没画出来」和「重试」。
  扩展画的时候头部不出 Pin（Pin 作用在原来的会话页上）。按钮回 `open_url: persona://inbox/<这一条>/source`（「在会话里看」）时，正文换成原来的会话页
  （飞书停在这件事的第一条），上面一行「回到<页面标题>」，扩展的页藏起来不卸载、回去原样；别的条目的、别的 `persona:` 链接不打开（`lib/extension-ui.ts` 的 `outcome` / `inboxSourceItem`）。
  截图用 `MOCK_PLUGINS=1 MOCK_INBOX_ITEM_UI=1`：飞书的条目由 `example-chat` 画（回复、在会话里看），群聊的条目扩展出错（看回退）。
  **扩展市场**（`settings/ExtensionMarket.tsx`，逻辑 `lib/extension-market.ts`，`docs/extensions.md`「扩展市场」）：扩展列表下面「扩展市场」一组两行。
  「浏览扩展市场」（`&extension=-market`）：搜索（停顿 250 ms 后查 hub）、按市场筛选的 chip、重新读取全部，读不到的市场在顶上写原因；每行名字、版本、说明、市场、标签和状态（未安装 / 可升级到 x / 已装 x / hub 自带 / 装不了和原因）。
  点进是详情（`&extension=-market/<市场>/<扩展>`）：说明、标签、「它可以」、「提供」、「关于」；「安装」/「升级到 x」先 `POST …/catalog/preview`，弹出预览（权限、升级的 `旧 → 新`、权限变多时的红色提示和「新增」、剩几分钟，过期就不能点），
  「同意并安装」/「升级」/「同意新权限并升级」带着预览的 id 和摘要调 `/extensions/-/install` / `/extensions/<id>/-/upgrade`，装好跳到这个扩展的详情；从这个市场装的有「卸载」（可留数据），扩展详情里也有（`origin` 是 `market:` 的）。
  「管理市场」（`&extension=-markets`）：每个市场的名字、token、地址、几个扩展 · 多久前读过、说明、读不到的原因和警告；重新读取、填 / 换 / 去掉 token（只写；`git:` 的市场没有这个按钮）、删除（就地确认）；「添加市场」填地址和可选 token，占位文字和说明写三种写法（https、`github:owner/repo`、`git:…`，#1373），
  页面不查地址的写法，原样交给 hub，hub 读不到时原因写在对话框里；`git:` 的市场用 hub 机器自己的 SSH 凭据，token 一栏不能填。逻辑和文字（`MARKET_SPEC_HINT`）网页和管理界面共用。
  「模型供应商」是全部接入（搜索；Persona 管理 / 来自 ~/.zshrc / 已隐藏），点开是弹窗：按电脑分组的真实参数（`docs/launchers.md`）、
  「额度 · <网关名>」（非直接 API 的，#1326，`settings/LauncherQuota.tsx`，逻辑 `lib/quota.ts`，照 iOS `LauncherQuotaSection`：预算、各账号 / 供应商的窗口和进度条（≥ 70% 橙、≥ 90% 或用完红）、按模型折叠、这个 Key 的用量，右上刷新带 `refresh=1`，`docs/launchers.md`「额度」）、
  测试可用性、函数原文（折叠）、编辑、删除 / 隐藏；新增支持启动器或直接 API（Messages / Responses），hub 的扩展有供应商预设时多一个「从扩展添加」（`docs/launchers.md`「从扩展添加」）；
  「模型配置」按 hub 的真实用途目录分组/搜索，编辑供应商、具体模型和支持的思考强度，显示继承与当前生效值。保存失败保留草稿，revision 冲突需重新加载（`docs/model-config.md`）；
  「翻译」（`settings/TranslationSettings.tsx`，`?tab=translate`）：自动翻译、翻译模型（跳到模型配置的 `translate` 用途）、目标语言、翻译范围（邮件、选中的飞书会话），
  存在当前浏览器（键名同 iOS 的 AppStorage）。不同步到 hub：hub 的翻译偏好挂在手机的推送设备 id 上（`/devices/{id}/prefs`，iOS 和 Android 都会发，hub 按它收信时预翻），网页没有这个 id；
  网页打开时按需翻，hub 预翻好的直接用。
  「设备」列出各设备的 token（这个浏览器排第一），撤销同样就地确认；hub 连已撤销的一起返回（带 `revoked_at`），它们不算进「设备 N」，
  放在下面的「已撤销」里，灰色、写撤销时间、不能再撤销，和 iOS 一样（#1764）。分类记在 `?tab=`；旧 `?tab=models` 仍打开供应商，新分类为 `?tab=model-config`。
  「设备」标题行右端的「连接新设备」（#2731，`views/settings/ConnectDevice.tsx`）：向 hub 要一个一次性临时配对码（`POST /api/v1/auth/pair-codes`，带 `{"web": 这个页面的地址}`，
  [protocol.md](protocol.md)「临时配对」），不用再登到 hub 机器上跑 `personad hub pair`。弹窗分「Persona App」（配对二维码 + 拷贝配对链接，Mac / Android 粘贴配对）和
  「浏览器」（`<页面地址>#token=jp_…` 登录链接的二维码，手机相机扫了直接打开 + 拷贝登录链接），记住上次选的；倒计时到 0 显示「已过期」和「重新生成」。
  每 3 秒查一次设备列表，出现生成码之后才有的凭据就是用了这个码的设备，显示「已连接：名字」并刷新列表。页面开在 `127.0.0.1` / `localhost` 上时「浏览器」页提示这个地址只有本机能打开。
  码 10 分钟后失效、只能兑换一次，关掉弹窗不作废（hub 没有作废临时码的接口），过期自然失效。
- **机器**：每台电脑一张卡片，CPU / 内存 / 磁盘当前值和最近一小时 CPU 曲线（`/api/v1/metrics`，10 秒刷新；空档断线），离线的能重连；点名字或「详情 ›」进机器页。
- **机器页**（`/machines/<机器>`，`src/views/machines/`）：在线状态、系统、hub 和线路（多台 hub 时）；离线时的错误和「重新连接」（之后最多再查 10 次等它上线）、
  版本不一致时「重新部署」；这台的 Agent 按空间列出；1 / 6 / 24 小时的 CPU、内存、网络曲线（`/machines/{id}/metrics?range=`，三张同步的悬停读数，键盘左右也能移，
  整段没数据时只给一张卡片和「看最近 24 小时」），只在页面可见时刷新（1 小时档 10 秒、另两档每分钟，照 iOS）；系统信息、启动器。
  #433 加了「负载（1 分钟）」曲线（四张图 2×2，虚线标出核数，颜色 token `--chart-load` 取 iOS `MetricColor.load`）和「每个核心」柱状图（`latest.cores` 有两个以上且在线时，即 Linux 的 node，
  右上「最忙 X%」，悬停看每核数值）。
  「这台电脑能做什么」（#2429，`machines/NodeHealth.tsx`，逻辑 `lib/node-health.ts`，[machines.md](machines.md)「自检」）：在引擎后面，把 node 自检按功能读出来（手机审批卡片 / 状态与通知 / 终端与引擎 / MCP 工具 / 技能 / 运行时接入：就绪、部分可用、有问题、未启用），
  「查看全部检查（N）」展开逐项，「重新检查」，生成文件过期或缺失时「修复」（确认一次，走 `POST …/doctor/fix`）；用户自己的同步 PermissionRequest 钩子是一行说明；旧 node 写「node 太旧」并带部署按钮。
  机器卡片和机器页离线时按错误文本给「认证没通过 / 连不上 / 超时 / 版本不一致」和一句下一步。截图用 `MOCK_DOCTOR=…` 的假 hub。
  「引擎」（#2071，`machines/EngineSection.tsx`，逻辑 `lib/engines.ts`，照 iOS `EngineSelectionModel` / `EngineSelectionView`，[engines.md](engines.md)）：在 Agent 下面，选这台电脑的会话从哪个引擎来。
  列出 node 上报的 `engines`（名字、「检测到可用 · 版本」/「没检测到」、`note` 用橙色写为什么用不了，网页还不认识的新引擎也列出来）；离线或老 hub 没有上报时列出已知的五个（herdr、Herdr App、herdr + Herdr App、Orca、Paseo）。
  已知的引擎随时能选（离线也能，连上后生效），不认识的只在 node 报可用时能选；当前的标「当前」。选了别的才出现「还原 / 保存」，保存走 `PUT /machines/{id}/engine`，
  hub 回的机器立刻写进列表（随后的 `machines` 推送是同样的值）；失败时选择保留，红字「保存失败：…」，可以再点保存。没开始改时跟着推送更新。截图用默认的假 hub（`MOCK_ENGINE_ERROR=1` 让保存失败）。
  「操控电脑」（#1326，`machines/ComputerSection.tsx`，逻辑 `lib/computer.ts`，照 iOS `ComputerSection`，[computer-use.md](computer-use.md)）：hub 的扩展 cua 打开、这台是 Mac 时（机器带 `computer`）
  在 Agent 下面多一节：开关「允许助手操控这台电脑」（`PUT …/computer`，离线时不能改），打开后按状态给一行说明和下一步：没装时「安装 Cua Driver」（先弹窗确认）、
  安装中的步骤和「取消」、缺权限时「去授权」、授权窗口弹出后「再请求一次」、可以操控时版本；那台 Mac 开着屏幕共享时有「VNC」（新标签页打开 `/screen/<机器>`），
  没开时提示去哪里打开。打开页面时 `GET …/computer` 问一次，安装 / 授权进行中每 2 秒再问。hub 没有重启这类动作（`…/computer/reboot` 是 404），这里也没有。
  截图用 `MOCK_COMPUTER=permissions|missing|ready` 的假 hub。

### 主对话统一入口（#930）

和 iOS 同一套（[unified-entry.md](unified-entry.md)）：助手对话里排进 hub 的对话动态（`lib/feed.ts` 的 `displayLines`：每条放在它发生的那一轮末尾，
每天第一行上面一行日期，`phone_reminders_create` / `phone_calendar_create_event` 的结果画成「已加提醒 / 已加到日历」，网页上没有撤销，写「在 iPhone 上」）；
卡片在 `views/assistant/Tasks.tsx`：计划卡、任务卡（不是今天的折成一行）、答过的问题、回执、直接对任务说的话、「今天」卡片（主对话每天第一次打开时
`POST …/today`，「准备」「过一遍」替你对 Persona 说一句）、消息卡（发送走收件箱的回复）。输入框上方：任务的 Agent 最早的那个提问（「还有 N 件」）、
任务坞、路由标签（任务卡上点「说一句」，下一条直接发给那个 Agent）。任务坞和 iOS 一样（#1275）：最左边是只有文字的「全部 N」（去待办页），
不跟着滚动，胶囊在它右边单独横向滚动；主对话的坞在任务后面带上手动开的、在做 / 等你的 Agent（`lib/todos.ts` 的 `dockAgents`，规则见
[unified-entry.md](unified-entry.md)「主对话的任务坞里的手动 Agent」），「全部 N」算上它们。任务详情在侧边面板（`kind: "task"`，`TaskPanel` 里就是那个 Agent 的对话；
手动 Agent 的胶囊打开 `kind: "agent"`，`AgentPanel`），换页不关。截图用 `MOCK_TASKS_DOCK=pair`（一个任务 + 一个手动 Agent）/ `many`（九个）。开发时 `?task=<待办 id>` 直接打开某个任务的面板；`MOCK_TASKS=1` 的 mock 有整套假数据。

### 终端 ⇄ 对话（#2419）

Agent 的会话页（`/s/…`、`/t/…`）中间在**对话**和它的**终端**之间原地切换，照 Moshi 的会话页（用户反馈「终端和聊天的切换感觉还是怪怪的」，明确要求照它的形态改；
以前终端是对话右边的一个侧边面板，⌘J 在开 / 关面板和 `/terminal` 页之间来回）。逻辑在 `src/lib/session-view.ts`，层叠在 `views/session/ViewLayer.tsx`，开关在 `views/chat/ChatView.tsx` 的顶栏。

- **开关**：顶栏右侧的「终端 | 对话」（复用对话页现有的 `Segmented`，窄屏 390px 也放得下，单行标签）；Agent 有终端可开（`sessionCan(s, "terminal")` 且不是离线）才显示。
  对话里有等你回答的卡片、而当前在终端视图时，「对话」上有一个红点。「⋯」菜单的「终端」是同一件事（勾选 = 现在是终端视图，再点回到对话）。⌘J 同样切换（输入框里、终端里都行；切完键盘回到新显示的那一边，触屏不弹键盘）。
- **地址**：`?view=term|chat`（`/t/…/<会话>?view=term`），可以收藏和分享；切换用 replace，所以后退是离开这个页面，不是一层层撤销切换。没有 `?view=` 的地址显示这个 Agent **上次显示的**（每个 Agent 各记一个，存在 localStorage 的 `persona.sessionView`，最多 300 个），没记过就是对话。
  从 `/terminal` 页的树、Jump To、侧边栏的终端列表点开 Agent 的窗格，进的是它的会话页 + `?view=term`；普通 shell 没有对话，还是 `/terminal/<机器>/pane:…`。⌘J 在 `/terminal` 页的 Agent 终端上回到它的对话。
- **保活**：第一次切到终端才连（不看终端的人不多连一条流）；之后终端挂在对话下面，大小不变、只是看不见也点不到（不用 `display:none`：xterm 在隐藏盒子里量出 100px 会把窗格缩了，同 `/terminal` 页的做法），流不断，切回来立刻有画面，不重新订阅。
  离开会话页（换到别的 Agent / 页面）或标签页进后台且当前是对话视图时，终端放手、把窗格还给电脑。终端视图里 Esc 属于终端里的程序，不再触发「中断 Agent」。
- **动画**：两层交叉淡入淡出加一小段横向滑动（200 ms，缓动同面板），`prefers-reduced-motion` 下只剩淡入淡出（时长被全局规则压到 1 ms）。
- **文件 / Git 面板**照旧在右边，可以和终端视图并排（#2345 的把文件拖进终端依赖这个）；终端头里的「文件」按钮还在。
- **终端的侧边面板去掉了**：`Panel` 里没有 `terminal` 了（没有别的入口要它），⌘B 没有可恢复的面板时打开这条对话的文件。
- 终端视图里有两条头：会话页的头（标题、状态、切换）和 `TerminalPanel` 自己的头（接管 / 只看、换行、设置、重新连接）；后者没改，它的 × 回到对话。

### 侧边面板

会话的文件、Git，和在页面里打开的链接，都在页面右边的面板里（`src/views/SidePanel.tsx`，选哪个在 `src/lib/panel.ts`）：一次一个，开新的替换旧的。
宽屏（≥ 1024）是一栏、把页面往左推，左边缘能拖宽度（双击恢复，记在 localStorage）；窄屏盖住整页。会话的面板跟着会话走：换到别的页面就关；链接面板留着，直到关掉。
页面里两栏的页（收件箱、知识库）按自己的宽度切单栏，所以开着面板也不挤。

- **终端**（`session/TerminalPanel.tsx`、`session/terminal/`，`docs/terminal.md`；在会话页中间显示，见上面「终端 ⇄ 对话」，不再是侧边面板）：xterm.js（`@xterm/xterm`，打开终端时才加载，单独一块）。
  - **两种界面**（#2357，`docs/terminal.md` 第二十四节；hub 有 `herdr_client` 才有）：头上的分段控件「单窗格 / 完整界面」（窄时只剩图标）。**完整界面是默认**（用户 2026-11-05 要求，像 Moshi）：node 在 PTY 里跑 herdr 自己的客户端，
    浏览器里的 xterm 显示它的侧边栏、标签页和分屏，键、鼠标、改尺寸、复制（OSC 52 → 剪贴板）都通；单窗格就是下面说的那个终端。切换时一个往里滑进来淡入、一个往外滑出淡出（`--ease-out-strong`，220 ms，减弱动态时直接换）；
    记住最近一次选择，设置 › 终端界面可以选默认（完整界面 / 单窗格 / 记住上次，选完整界面不写任何键）；Mod+Alt+F 切换。
    **代价**：完整界面和电脑上的 herdr 窗口共用同一份布局，最近操作的那一边说了算——浏览器连着时电脑上的程序按浏览器的大小排版（首次连上会提示）。
    hub / node / herdr 不支持（旧 hub 没有切换；Windows；这台电脑不是 herdr）或客户端起不来时，自动用单窗格并提示原因。完整界面里键栏、查找条、右键菜单、存好的命令和单窗格是同一批组件。
  hub 有 `terminal_stream` 时订阅实时流：整帧清屏重写、增量按 seq 接上，断档或换了 epoch 就重订（忽略旧流还在路上的帧）；`unsupported` 时每秒读一次屏（`/screen?format=ansi`）。
  默认按面板的列 × 行接管（`terminal_control`），拖面板宽度、缩放窗口后防抖再发新尺寸；电脑上别的程序占着时问「接管 / 只看」，被抢走时给「接管」。
  只有接管时键盘、滚轮（`scroll`，带鼠标所在的格子）、单击（`click`）、粘贴（多行先确认）才发出去；只看时按窗格本身的尺寸画，宽了横向滚动。
  断线、出错、订阅后超过 4 秒没拿到第一屏时，顶部出状态条（上次更新多久前、原因、重试；1、2、4、8 秒退避再每 15 秒），旧画面调暗；窗格只是安静不算过期。配色取页面的 token，跟着深浅色变。
  #433 补上（照 iOS `TerminalKeyBar` / `TerminalFeed` / `TerminalWrap`）：
  - **按键栏**（`terminal/KeyBar.tsx`）：Ctrl / Alt / ⇧（点一下作用于下一个键、双击锁定、按住不放）、Esc、Tab、⌃C、⚡ 快捷命令、粘贴、方向键、回车，字节同 iOS `TerminalKeyBytes`，
    程序开了应用光标键时方向键跟着换编码；修饰键对在终端里打的下一个键也生效。只在接管时能按；触屏默认显示，其他在齿轮「终端设置」里打开。
  - **快捷命令**（`Shortcuts.tsx`）：默认同 iOS，单击输入、↵ 执行，可以添加、排序、删除，存在当前浏览器（iOS 也存本机）。
  - **往上翻 herdr 的历史**：只看时滚到顶再往上（或点「往上滚动加载更早的输出」），`POST …/screen/before`（`anchor` = 已显示的前 20 行、`loaded`、`lines: 200`、`format: ansi`）；
    读历史时画面停住、新帧攒着，「回到最新」时补上；到头提示「已到最前面」或「herdr 只保留最近约 1000 行」。接管时不翻（滚轮照旧发给程序）。
  - **换行开关**：只看时由页面自己画屏幕（`lib/terminal-cells.ts`、`DrawnRows.tsx`），长行在面板宽度折行，续行挂在缩进和列表符号后面，补宽度的横线画到边缘截断。默认关。
  - **中文字之间的空隙**（`terminal/cjk.ts`）：xterm DOM 渲染器给宽字符两格、中文字形只有约 1em，就在后面补字距；用 `FontFace` 以 `local()` 载入系统中文字体、`size-adjust` 放大到正好两格，
    `unicode-range` 只含中日韩，不加依赖；找不到这些字体时保持原样。
  - **字号**：齿轮里 9–20，不占 ⌘+ / ⌘−（浏览器缩放），存在当前浏览器。
  - **粘贴 / 拖入图片和文件、复制**（`lib/terminal-clipboard.ts`、`terminal/clipboard.ts`，`docs/terminal.md` 第十八节）：接管时 ⌘V / Ctrl+V 粘贴的截图或拖进面板的文件，经 `POST /machines/{mid}/files?dest=terminal`
    传到窗格所在的电脑（50 MB 以内），再把转义后的路径加一个空格敲进窗格（括号粘贴），Claude Code / Codex 按路径读图片。传的时候顶部有状态条；传完时已换会话或不再接管就不粘贴并提示。
    有选中文字时 ⌃C / ⌘C 复制（没有选中仍是 ^C），键栏也有复制键；页面不是 HTTPS 时退回 `execCommand` 并说明。载入 `@xterm/addon-clipboard`（程序的 OSC 52 写入剪贴板，读查询一律拒绝），herdr 的流目前不带程序的 OSC 52。
    输入法组字时页面不处理按键，打开 xterm 前等字体。
  - **完整界面里的冲突**（#2700，`docs/terminal.md` 第二十六节，`lib/terminal-conflicts.ts`）：herdr 说了算——右键是 herdr 自己的菜单（Persona 的菜单是 Shift + 右键）、Ctrl+B（herdr 的前缀键）原样发给 herdr 而不是 Persona 的「右侧面板」、鼠标一律先给 herdr、链接要 ⌘ / Ctrl + 点击；
    粘贴 / 拖放图片和文件、Files 面板的 `@path` 拖放也接到完整界面（Windows / Linux 的 Ctrl+V 先看剪贴板有没有文件，没有仍是 `^V`）。表格在 `docs/terminal.md` 第二十六节。
  #2331 补上（对照 Moshi 网页版的细节；逻辑在 `lib/terminal-details.ts`、`lib/terminal-search.ts`，都有单元测试）：
  - **输出里的网址可点**：载入 `@xterm/addon-web-links`，点一下经 `openLink` 打开（和对话里的链接同一条路），只认 http(s)（`webLink`），程序用 OSC 8 写的链接也走同一个过滤；
    点链接不会同时给程序发一次 `terminal_click`。换行模式下页面自己画屏幕，没有链接（同 OSC 8）。
  - **右键菜单**：终端里右键不出浏览器菜单，出一个小菜单——复制（有选中才能点）、粘贴（接管时）、全选、搜索…、清除选区；用 `ui/atoms.tsx` 的 `Menu`，`point` 参数就是鼠标位置（#2329 前是放一个零大小的锚点）。
    按住 Shift 右键仍是浏览器自己的菜单；触屏长按不拦（长按要用来选字）。
  - **鼠标转给程序**（#2441，`lib/terminal-mouse.ts`，`docs/terminal.md` 第二十五节）：接管时按下 / 拖动 / 松开（左中右键，ctrl / alt 修饰）经 `terminal_input.mouse` 到 herdr，
    程序开了鼠标就收到、没开就丢掉（herdr 不告诉我们程序的鼠标模式）。默认「交给程序」：按住 Shift 拖动是页面选字、Shift+右键出上面的菜单（这时右键不再是浏览器菜单）；
    终端设置 › 鼠标 选「本地选择」则回到原来的拖动选字 / 右键菜单，Shift 才交给程序。⌘ 和触屏留在页面。悬停不发。feature `terminal_mouse`。
  - **Mac 行编辑键**（接管时，在 xterm 的自定义按键处理里，`lineEdit`）：⌘⌫ → ^U、⌘← → ^A、⌘→ → ^E，和已有的 Shift+Enter（ESC CR）并列；带其他修饰键的组合不动。
  - **终端设置新增**：光标样式（方块 / 竖线 / 下划线）、光标闪烁、WebGL 渲染（`@xterm/addon-webgl`，第一次打开才加载，关掉或 context 丢失就释放并退回默认渲染）。
    存 `jarvis.terminal.cursorStyle` / `cursorBlink` / `webgl`；默认值（方块、不闪、DOM 渲染）不写键，默认外观一像素不变。光标样式只管 xterm 画的光标，换行模式下页面画的光标不变。
  - **焦点策略**：接管时点面板里任何地方、窗口重新获得焦点，把键盘还给终端——除非有选中的文字、有打开的对话框 / 菜单 / 浮层、焦点在别的输入框里（搜索框也算）、焦点在面板外的控件上、或这个终端被盖在别的终端下面（`lib/terminal-details.ts` 的 `shouldRefocus`；页面的事实在 `terminal/focus.ts`）。只看时不抢。
  - **搜索**（`terminal/FindBar.tsx`、`@xterm/addon-search`）：终端有焦点时 ⌘F（Mac）/ Ctrl+Shift+F（其他系统；没接管时 Ctrl+F 也行，接管时 ^F 是程序的），面板顶上浮出查找条：输入即搜、Enter 下一个、Shift+Enter 上一个、Aa 区分大小写、Esc 或 × 关闭（Esc 不会发给程序）；
    选中了一行文字时用它作为初始搜索词。查找条只在面板里生效，不注册全局快捷键。能搜到什么见 `docs/terminal.md` 第二十节。
  - **接管时的「回到最新」没做**：见 `docs/terminal.md` 第二十节，帧里没有程序视图的滚动位置，页面无从得知。
  - **把文件面板的条目拖进终端**（#2333，`lib/entry-drag.ts`，`docs/terminal.md`「把文件面板的条目拖进终端 / 输入框」）：文件 / 文件夹行可拖（样子不变），放到接管中的终端上敲进 `@相对窗格目录的路径 `（文件夹以 `/` 结尾，目录外用绝对路径，转义同粘贴文件），不上传；只看时提示「接管终端后才能粘贴文件」，另一台电脑的文件提示后不敲。同一个拖动放进对话输入框是光标处的 `@路径 `。独立终端页的窗格头有「文件」按钮，在终端旁打开窗格目录的文件面板。
  没做：只看时打字（iOS 只看时经 `/keys` 能打字）、终端配色和字体选择、键栏自选键 / 自动收起 / 输入框模式 / 补全、触屏手势（双指切 Agent、双击粘贴、捏合）、换行模式下 OSC 8 链接。
- **文件和 Git**（`session/FilesPanel.tsx`、`GitPanel.tsx`、`FileViewer.tsx`、`session/files/`、`src/lib/files.ts`）：从会话目录逐级进入、面包屑、Git 标记、隐藏文件开关；
  **树状视图**（#2439，`session/files/FilesTree.tsx`、`lib/files-tree.ts`，按 Moshi 的形式）：宽屏（≥ 1024 px）默认是可展开的树，文件夹在前、就地展开（缩进一级一级往里）、Git 忽略的行变淡（node 的 `fs.list` 给每项加 `ignored`）、拖动 / 拷贝路径 / Git 标记照旧；
  窄屏仍是逐层进入 + 面包屑（样子没变），面板「⋯」里的「树状视图」可以在两种之间切换（记在浏览器里，`jarvis.files.view`），选了就压过宽窄的默认。展开的文件夹按会话记（`jarvis.files.expanded.<机器>.<会话>`），换会话不带过去；
  列过的文件夹按「机器 + 会话目录」缓存（最近用的 12 个文件夹、总共 8 MB，超了先丢最久没用的），关了再开立刻有内容、同时重新读一遍。
  **自动刷新**（`lib/fs-live.ts`、`lib/use-fs-live.ts`，`docs/protocol.md`「文件自动刷新」）：面板开着时，页面把打开着的文件夹和是否显示 Git 告诉 hub（`fs_watch`），node 轮询这些文件夹和 `git status`，变了推回 `fs_changed`；页面等 200 ms 去抖后只重新读变了的文件夹、Git 状态和打开着的 diff（原位刷新，不出加载动画）。页面隐藏时不看，回来刷新一遍；socket 重连后也刷新一遍。旧 node 没有 `fs.watch`，面板照旧手动刷新。
  Git 面板标题的副标题和文件面板的 Git 按钮显示 `↑N ↓N *`（领先 / 落后上游的提交、有未提交的改动）；
  查看器：代码带行号和轻量着色（注释 / 字符串 / 数字，只渲染看得见的行，两万行也不卡）、Markdown（可看源码，README 的 HTML 转成 Markdown，相对图片从那台机器读）、
  JSON（只重新缩进，不改键序、不丢大数精度）、图片、PDF、表格（见下）、分页「继续加载」、打开到某一行、拷贝路径、下载。二进制文件按 `file?stream=1` 只取描述，
  图片 / PDF 直接用 `file/download?inline=1` 的地址（浏览器按 Range 读），下载走附件，不再把整份 base64 放进页面（`docs/protocol.md`「文件预览」）；旧 hub 不认 `stream` 时照旧用 base64。Git：分支和上游、改动和 diff（暂存 / 未暂存切换）、提交历史和每个提交的 diff，只读。
  对话里 Read / Edit / Write / MultiEdit / NotebookEdit 的路径可点，打开查看器（Read 带 offset 时到那一行）。
- **消息里的文件路径**（#1669，`lib/path-refs.ts`、`lib/path-presence.ts`、`ui/PathLinks.tsx`）：用户消息和回答正文里写到的路径，**确认在 Agent 那台电脑上存在后**才变成链接，点开在文件面板里看（Markdown 渲染、代码带行号和着色），写了行号就打开到那一行；⌘ / Ctrl 点是浏览器新标签里的原文件。
  - **认哪些**（同 iOS `FilePaths`）：`~/…`、至少两段的绝对路径（`/tmp` 一个词不算）、带目录和字母开头扩展名的相对路径（`src/a.ts`；`deepseek/v4.1` 不算），相对路径按会话的 cwd 解析。
    后面可以跟行号：`:12`、`:12:5`、`:12-20`（取起始行）、`#L12`。反引号外只认 ASCII，路径后紧跟中文、全角标点就结束（`/tmp/a.md里`），句末的句号不算进去；反引号里的整段算一个路径（可以有中文和空格），
    整段不在时再看里面的路径（`` `/a.md 和 /b.md` ``）。URL 里的、版本号、`and/or` 不算。回答里反引号包着的路径链接整个代码片段，代码块、已有的链接和公式不动。
  - **先问再链**：一条消息的候选（最多 24 个）交给 `path-presence`，60ms 内出现的按「机器 + cwd」合成一次 `GET /machines/{mid}/stat`；file / dir / denied 链接（denied 打开时说怎么授权），在的记 30 分钟、不在的 2 分钟、没回答的 1 分钟后再问。
    答案回来之前、旧 node 不认 `stat`、断网时都是普通文字。回答下面的媒体 / 文档卡片用同一份缓存，不重复问。
- **表格**（#1310，`session/files/SheetView.tsx`、`lib/sheets.ts`、`lib/sheets.worker.ts`；iOS / Mac 是 #1294）：CSV、TSV、xlsx / xlsm / xlsb / xls、ods、numbers 打开成表格。
  列字母和行号固定、首行冻结（可关，记在浏览器里）、列宽按前 300 行的文字估算（文件设得更宽就用文件的），拖表头右边改宽、双击还原；只画看得见的格子，几十万行照样顺；
  合并单元格、单元格自己的数字 / 日期格式（SheetJS 的格式化文本）；点选、拖选、Shift 扩选、点列字母 / 行号选整列 / 整行、⌘A，选中部分合并块时扩到整块；
  ⌘C 复制成 TSV（能直接贴回表格软件）；选区显示计数，有数字时显示求和、平均；多个工作表在底部切换。CSV / TSV 可切「源文件」看原文。
  - **CSV 不猜类型**：自己按 RFC 4180 解析（引号、`""`、引号里的换行、CRLF、BOM），值原样显示，`001` 不会变成 `1`；分隔符按扩展名，否则看前几行哪个多（`,` `;` 制表符）。
  - **解析放 Worker**：大文件读完（CSV 最多 64 MB，工作簿最多 200 MB）交给 `sheets.worker.ts`，小 CSV 直接在主线程；一个工作簿最多留 2000 万格、每表 50 万行，超出提示只显示了前面。
  - **SheetJS 用官方包**：npm 上的 `xlsx` 停在 0.18.5（有原型污染、ReDoS 的公开漏洞），官方新版只从 cdn.sheetjs.com 发 tarball（Apache-2.0）。
    隐私扫描只认 npm 官方源或仓库里的相对路径，所以放在 `web/vendor/xlsx-0.20.3.tgz`，`package.json` 写 `file:vendor/…`；Docker 构建先拷 `web/vendor/` 再 `npm ci`。升级：下载新 tarball 换掉旧的、`npm i ./vendor/<新文件>`。
  - Worker 那块 SheetJS 约 500 KB，只在打开表格时才加载。
- **Office**（#1345，`session/files/OfficeView.tsx`、`lib/pptx.ts`；iOS / Mac 走 QuickLook，是 #1293）：docx / docm / dotx 用 docx-preview（Apache-2.0）排成分页版面（页眉页脚、表格、图片、脚注），
  pptx / pptm / ppsx / potx 用自己写的 `lib/pptx.ts`：幻灯片尺寸、主题色（含亮度调整）、占位符从版式和母版继承位置和字号、母版上的图形、背景、文字（字号、粗斜体、下划线、颜色、对齐、项目符号和级别）、
  形状（填充、边框、矩形 / 圆角 / 椭圆 / 三角 / 线）、图片、表格（合并、底色）、组合、隐藏页、备注；
  图表从图表部件的缓存里读出系列，柱形 / 条形（含堆积）/ 折线 / 面积 / 饼 / 圆环自己画成 SVG（标题、坐标、图例，字号随图表大小），散点、雷达、组合图等画不了的框里写「这张图表在这里看不了，点右上角『转成 PDF』查看」，SmartArt 同理。顶部写页数，「转成 PDF」随时可点。
  - **字体回退**：文档点名的字体本机常常没有（Calibri、等线），浏览器就退到默认字体，中文是发粗的宋体。docx-preview 写出的每个 `font-family` 后面都补上回退（`lib/fonts.ts`）：点名的是衬线字体（Times、宋体、仿宋、楷体……）才补衬线，否则补系统无衬线（PingFang SC / Hiragino Sans GB / Noto Sans CJK / sans-serif）；文档没写字体的地方也用这套无衬线。
  - **iWork**：.pages / .key 先显示文件里 Apple 软件自己存的预览（`QuickLook/Preview.pdf` 是每一页的 PDF；新格式只有第一页的 `preview.jpg` / `QuickLook/Thumbnail.jpg`），按 HTTP Range 只读 zip 的末尾（中央目录）和那一项（`lib/zip-range.ts`，几百 MB 的 Keynote 也只取几百 KB），取不到再交给 LibreOffice。
    .numbers 照旧先用 SheetJS 打开成表格（比一张图有用），读不了再显示自带预览。
  doc / ppt / rtf / odt / odp 以及渲染失败、没有自带预览的，交给那台电脑上的 LibreOffice 转 PDF（`file/convert`，`docs/protocol.md`「文件预览」），用浏览器的 PDF 查看器打开；没装就写清楚装什么，并给下载。
  - **隔离**：渲染结果放进 `srcdoc` 的 iframe，`sandbox` 只给弹出窗口（网页链接在新标签打开），没有 `allow-scripts` 和 `allow-same-origin`，再加 CSP `default-src 'none'; img-src data:`：文件里的东西不能跑脚本、拿不到网页的 token、也不能联网（外链图片、跟踪像素都加载不了）。
    图片用 data URL（docx-preview 的 `useBase64URL`），因为 iframe 读不了本页的 blob URL；非 http(s) 链接去掉。页宽按文档自己的宽度排，再把整个 iframe 缩小到面板宽（只缩不放）。
  - **为什么不用 pptx-preview**：npm 包是 ISC，但源码不公开、README 另加了源码使用限制，而且它在页面里直接写 `innerHTML`，审不了。自己写的只输出转义过的文字和校验过的颜色、数字、自己生成的 data URL。
- **Mermaid**（#1376，`ui/Mermaid.tsx`、`lib/mermaid.ts`；iOS / Mac 是 #1295）：对话和文件查看器的 Markdown 里 ```mermaid 代码块画成图，Mermaid 支持的图都行（流程、时序、类、状态、甘特、饼、ER、思维导图……）。
  Mermaid（MIT）第一次画图时才加载（约 670 KB），`securityLevel: "strict"`（自带 DOMPurify 清洗，没有点击回调、标签里的 HTML 不执行）；一次只画一张（它画的时候有全局状态），按主题缓存 60 张，滚回去不重画。
  跟随深浅色（换主题重画）；饼图换成 App 的图表色（Mermaid 自带的深色饼几乎是黑的、浅色是粉彩）；最高 460px，大图点「放大」全屏看。流式输出时代码停下 350ms 再画，画出来之前显示源码；语法错误显示源码和「这张图有语法错误（第 N 行）」，Mermaid 的英文原文收在可展开的详情里。悬停有看源码 / 看图、放大、拷贝。
- **Jupyter**（#1407，`session/files/NotebookView.tsx`、`lib/notebook.ts`；iOS / Mac 是 #1296）：.ipynb 渲染成笔记本（nbformat 4，兼容 3 的 worksheets），「笔记本 / 源文件」可切。
  Markdown 单元走同一个 Markdown（公式、Mermaid），单元附件里的图在渲染后换上（react-markdown 不放 data: URL，先写成 `#nb-attachment=` 再替换）；代码单元按内核语言上色、带 `[n]:`；
  每个输出只显示一种：图片（PNG / JPEG / GIF / WebP / SVG）> HTML > Markdown > LaTeX（转成 `$$`）> JSON > 纯文本；相邻同名的 stdout / stderr 合并，stderr 红底；报错显示去掉 ANSI 颜色码的完整 traceback（横向滚动，不折行、不截断）；其他单个文本输出超过 2000 行截断并写剩几行。
  HTML 输出（pandas 表格等）放进 `sandbox=""` 的 srcdoc iframe，CSP 禁联网，配色跟随网页深浅色，高度按行数估、可拖。整个文件最多读 64 MB。
- **HTML 预览**（#1413，`session/files/HtmlPreview.tsx`、`lib/html-preview.ts`；iOS / Mac 是 #1297）：.html / .htm / .xhtml 默认「预览」，可切「源码」。页面放进 srcdoc iframe，**任何时候都不给 `allow-same-origin`**（碰不到本页、存储和 token）；
  默认禁脚本（sandbox 不给 `allow-scripts`）、禁联网（在 doctype 后面注入 CSP `default-src 'none'`，只放 data: / blob: 图片、内联样式）。工具条的「脚本」「联网」各自打开，只对这次打开的这个文件有效，旁边写当前状态；「重新加载」重开一次。
  注入 `<base target="_blank">`，链接在新标签打开。页面引用的同目录文件（图片、样式表和其中的 `@import` / `url()` 字体图片、脚本、图标）经 hub 读出来放进页面本身（data: URL、内联 `<style>` / `<script>`，`lib/html-inline.ts`，用 DOMParser 解析，什么都不加载、不执行），隔离框仍不联网、无同源；最多 150 个、单个 10 MB、合计 40 MB，工具条写读入了几个、几个读不到。本机 dev server 的站点预览另算（端口转发那边）。整个文件最多读 16 MB，再大按源码显示。
- **媒体**（#1422，`ui/Media.tsx`、`chat/FileCards.tsx`）：文件页的视频、音频直接播放，地址是 `file/download?inline=1`，浏览器按 Range 取，拖动不用等下载完；浏览器放不了的编码（Chrome / Firefox 里的 HEVC、多数 MKV）说清楚并给下载。
  SVG 按图片显示（`<img>` 里它的脚本不运行），可切源码。HEIC / HEIF：Safari 直接显示；别的浏览器 `<img>` 报错后换成那台电脑画的预览（`file/thumb`，最长边 1024），再不行提示下载。
  对话里手机发来的 `<jarvis-file kind="video" …/>`（`docs/protocol.md`「发送视频」）解析成卡片：视频 / 音频就地播放，带文件名、大小、时长，其他文件点开在文件面板里看；标签本身不显示，和 iOS 一致；时长以文件读出来的为准（向下取整，和播放器一致）。回答里写到的绝对路径（`/…`、`~/…`）如果是视频、音频、图片或文档，经 `stat` 确认那台电脑上有这个文件后，在回答下面画成同样的卡片（最多 8 个）。
- **对比**（#1437，`session/files/CompareView.tsx`、`lib/compare.ts`；iOS / Mac 是 #1299）：Git 面板里图片、PDF、docx、pptx、xlsx 的改动不再只写「二进制」。
  两个版本从 `git/blob` 取（node 把那个版本写进缓存、回路径，再用 `file/download` 读）：未暂存 = 暂存区 → 工作区，已暂存 = HEAD → 暂存区，提交里的文件 = `<提交>^` → 提交；新增 / 删除的只有一边。
  图片并排或叠在一起拖滑块；文档抽文字（PDF 用 pdf.js，Apache-2.0，逐页；docx 段落，表格一行一行（格子用 ` | ` 连起来，改了的行只把改动的格子标成「旧→新」），按分页符分页；pptx 用 `lib/pptx.ts` 逐张，含表格、图表数据和备注；表格按工作表转成制表符分隔的行），
  用 Myers 行 diff（和 git 同一种）显示增删，每处改动标在第几页 / 第几张 / 哪个工作表，没改的折起来；「打开旧版 / 打开新版」在文件查看器里原样看。文字没变就说可能改了格式或图片；改动太多（6000 处以上）就只给两个打开按钮。
- **这一轮看过的图片**（#1540，`chat/TurnImages.tsx`、`lib/turn-images.ts`；App 是 #1543，同一套设计）：放在这一轮的工作行（「工具 9 · 6 分钟」）和回复正文之间，列出这一轮工具带回来的图片：Read / Codex `view_image` 读的图、截图（Cua / computer）、网页截图（browser / playwright）、MCP 返回的图。用户自己发的图在气泡里，不算。
  来源有两处：展开着的工作里带 `tool_use_id` 的 image item，和折起来的工作的 `fold.images`（`docs/protocol.md`「折叠的工作」：`item_id` 走 items/{id}/image，没有的只有 `path` 走 `file/thumb`）。同一路径一次，没路径的按 mime + 大小 + 宽高去重，按第一次出现排。
  只有缩略图，不加标题或说明文字：多张是高 60px 的横滑条，宽度按图的比例（方到 16:9 之间），超出时边缘渐隐；一张就是一个略大的缩略图（高 88px）。名字（文件名或「截图 · 机器」）和尺寸只在悬停和画廊里出现。点开是画廊：← / → 和底部缩略图切换，Esc 关，下载、拷贝路径、在文件页打开。
  缩略图懒加载；条的高度固定、每张有自己的 key，工作中新图追加在末尾，不让上面的内容移动。
- **浏览器登录**（#1304，`machines/BrowserLogins.tsx`、`lib/cookies.ts`）：扩展 ego 打开、这台 Mac 的 ego lite 做完首次设置后，机器页列出 ego 里有登录的网站（条数、最晚过期，不显示内容），可按网站或全部清除，可导入 cookies.txt / JSON（页面里解析，勾选网站后经 hub 直转那台 Mac，hub 不保存），见 `docs/plan-ego-browser.md`。
- **链接**：见下面「链接在页面里打开」。

### 快捷键和命令面板（#2330）

对照 Moshi 的键位（`src/lib/shortcuts.ts` 是登记表：id、中文名称、默认键、作用范围；App 里 `useShortcuts`（`src/lib/use-shortcuts.ts`）登记每一项做什么并挂唯一的 keydown 监听）。
**每个快捷键都必须带 Mod**（Mac 是 ⌘，Windows / Linux 是 Ctrl；Mac 上的 ⌃ 不算，留给终端），所以单个按键永远不会被抢走。

| 键（Mod = ⌘ / Ctrl） | 做什么 | 对应 Moshi |
| --- | --- | --- |
| Mod+K | 跳转：搜会话，也搜终端窗格；再按一次关闭 | 跳转 |
| Mod+Shift+P | 命令面板（等于在搜索框里先输入 `>`） | 命令面板 |
| Mod+/ | 快捷键帮助（当前的键，改过的显示改过的） | — |
| Mod+, | 设置 | 设置 |
| Mod+1…9 | 切到第 N 台电脑（机器详情页，按侧边栏的顺序） | 切机器 |
| Mod+J | 会话页：中间在对话和终端之间切换（记住每个 Agent 上次的）；终端页：Agent 的窗格回到它的对话（普通 shell 没有对话） | 终端 ⇄ 对话 |
| Mod+Alt+F | 终端面板在前面时：单窗格 ⇄ 完整界面（hub 有 `herdr_client` 才有） | 终端 单窗格 ⇄ 完整界面 |
| Mod+B | 右侧面板收起 / 恢复（恢复上一次关的；没有就开这条对话的文件）。终端的完整界面里，Windows / Linux 的 Ctrl+B 是 herdr 的前缀键，原样发给 herdr（#2700） | 右面板 |
| Mod+Alt+B | 宽屏：侧边栏收成图标栏 / 展开（记在这个浏览器）；窄屏：抽屉 | 左侧栏 |
| Mod+Shift+Enter | 宽屏：只留中间（侧边栏和右侧面板不画，面板里的终端照常挂着）；再按恢复 | 最大化中间 |
| Mod+I | 聚焦这页的输入框（对话），没有就聚焦正在看的终端 | 聚焦输入 |
| Mod+E | 切换侧边栏 Agents 的分组方式（按机器 / 仓库 / 项目 / 状态 / 待办），和分组菜单是同一个设置 | 侧边栏模式 |
| Mod+Alt+↑ / ↓ | 侧边栏上 / 下一行（打开它；折叠起来的不算，到头绕回） | 上 / 下一行 |
| Mod+Alt+Shift+↑ / ↓ | 折叠 / 展开侧边栏的全部分组 | 折叠 / 展开树 |
| Mod+Alt+← / → | 这台电脑上的上 / 下一个标签页（按侧边栏的顺序，到头绕回） | 本机上 / 下一个标签 |

没有搬过来的：Moshi 的 Mod+Alt+1–3（在右面板里选 Web / 文件 / Diff）——Persona 的文件和 Git 是各自的面板，没有一个统一的选项卡；这三个面板还用头上的按钮开。
**浏览器可能先截走**：Mac 的 Chrome 把 ⌘⌥← / → 留给切标签页，网页收不到，需要时在设置里换一个键；其它被浏览器和系统留着的键见下面的保留表。

- **改键**（设置 › 快捷键，`views/settings/ShortcutSettings.tsx`）：点一行，按下新的组合键，Esc 取消；记在当前浏览器（`localStorage` 的 `persona.shortcuts`：`{overrides, disabled, off}`，只存和默认不同的，键写成平台无关的 `Mod+Shift+P`；第一版只存了 overrides 本身，照样读）。
  hub 没有通用的按用户同步的偏好设置（`/agents/prefs` 只管 Agents 列表，`/devices/{id}/prefs` 是单台设备的推送），所以不跨浏览器同步；别的标签页改了经 `storage` 事件跟上。
  提示：「需要带 Ctrl / ⌘」（没带 Mod）、「… 浏览器 / 文字编辑 / 终端 / 系统要用，换一个」（保留表 `RESERVED`：⌘T / W / N / R / L、⌘C / V / X / A / Z、对话框的 Mod+Enter、Windows / Linux 上终端的 Ctrl+W / R / L / U / D / Z…）。
  另一项在用的键（#2586）：行里提示「X 已被「Y」占用」，「替换」把键给这一项、Y 清空，「取消」两边都不动；停用的项也算占用（重新打开时不撞）。
  - **清除**（×）：这一项没有键（存成 `""`），按原来的键什么都不发生，也不回落到默认值；命令面板里照样能点。
  - **停用**（每行的开关）：保留键、不生效；**启用快捷键**（页面顶部的总开关）关掉时所有快捷键都不生效，各项自己的设置保留。分发器、终端放行（`passesToApp`）、帮助、命令面板、侧边栏的提示都只读生效中的键（`getBindings` / `useBindings`，清除和停用的为 `""`）；设置页读设定的键（`getConfigured`）。
  - **恢复默认**：每行（键回到默认、重新启用；默认键已被别的项占用时同样先问替换）和右上角「全部恢复默认」（连总开关一起，清掉存储）。
  录入时全局快捷键暂停（`holdShortcuts`）；Tab 照常移走焦点。第 1–9 台电脑这一项只改修饰键，数字固定。侧边栏搜索框上的 ⌘K 提示在默认键时不变，改了键就显示新的，没有键时不显示。端到端：`scripts/test/web_shortcuts_e2e.py`。
- **组件自己处理的键**（`LOCAL_SHORTCUTS`）：终端里的查找（Mac ⌘F；Windows / Linux Ctrl+Shift+F，没接管时 Ctrl+F 也行，#2350）由终端面板自己判断，不登记成命令、分发器看不到；
  只是列在帮助和设置「终端里（固定）」里，并进保留表（Ctrl+Shift+F）让别的命令占不了。放行函数 `passesToApp` 和终端的 `lineEdit`（⌘⌫ / ⌘← / ⌘→）是两件事，互不干扰。
- **键怎么认**：字母按键面上写的（Dvorak / AZERTY 跟着标签），Mac 的 ⌥ 组合出的字符（⌥B 是 ∫）按物理键认，数字和标点按物理键认；AltGr（Windows 上的 Ctrl+Alt）不算 Mod+Alt；输入法组字中（`isComposing` / keyCode 229）不触发；按住不放的键只触发一次（上 / 下一行除外）。
- **和输入框、终端的关系**：因为都带 Mod，在输入框里照样触发（⌘K 一直如此）。终端里见 `docs/terminal.md`「网页」：已登记的键 xterm 一个字节都不发，Ctrl+K 打开面板而不是 `^K`；没登记的键（Ctrl+C 等）照旧进程序。
  有对话框（`Dialog`）或面板开着时，只有跳转 / 命令面板 / 帮助三个还能用，其余暂停；某项当前用不了（比如没有对话时的 Mod+J）返回 `false`，键原样放行。
- **命令面板**（`views/Palette.tsx`、`lib/palette.ts`）：Mod+K 的搜索框里先输入 `>`（或 Mod+Shift+P）进入命令模式，列出登记了处理函数的命令，按「全局 / 会话页 / 侧边栏」分组、右边是当前的键，再加「新建 Agent」「新对话」，过滤按标题 / 分组 / 关键词；第 1–9 台电脑展开成每台一条。
  Mod+K 的搜索也找终端窗格（用终端页的数据：每台在线电脑的 `GET /machines/{mid}/panes` + `buildTerminalTree`，面板打开时读一次；有会话的 Agent 窗格不重复列），点开是 `/terminal/<机器>/<id>`；什么都没输入时只列前 8 个窗格，排在会话后面。
- **文件搜索**（#2438，`views/palette/FilesSection.tsx`、`lib/file-find.ts`；Palette.tsx 只多几行把这一节的行接进列表）：Mod+K 在会话页（会话 / 标签页的 pane）按会话的 `cwd`、在 `/terminal` 页按打开的窗格的 `cwd` 多出一节「文件」，别的页没有（搜索框的提示语也只在有这一节时多一个「文件」）。
  面板一打开就读 `GET /machines/{mid}/fs/find?cwd=` 建索引（一次请求拿到整个目录的相对路径，30 秒超时；网页里同一目录 20 秒内复用，node 里 10 秒），第一次打字时还没回来就显示「正在建立索引…」；老 node（`unknown method`）或老 hub（404）退回按 `GET /machines/{mid}/fs` 一层层广度优先走（最多 300 个文件夹、2 万个文件，跳过 `.git`、`node_modules` 等缓存目录）。
  匹配是纯函数（`createFileSearch`）：每个词都得匹配（空格 = AND），不分大小写；词首（`/ . - _` 之后）、驼峰的大写字母、连续命中、文件名里的命中加分，文件名完全相同 / 以它开头再加分，路径短的在前；最多列 50 个，命中的字符加粗成强调色；新的查询是上一次的延长时只在上一次的命中里找。
  点开（或 Enter）在右侧面板里预览（`openPanel({ kind: "file" })`，和对话里的路径链接一样走 `/file`），Mod+Enter 复制 `@相对路径`。
- **给别的模块的钩子**：`registerContextCommands(provider)`（`lib/palette.ts`）。provider 在每次列命令时被调用，参数是 `{ route, pane }`（当前页和页面上的对话），返回 `PaletteCommand[]`（`id`、`title`、`section`、`icon`、`chord`、`keywords`、`danger`、`run`）；抛错的 provider 被跳过，重复 id 取第一个，返回值是取消登记的函数。
  这些命令排在命令模式的最前面，用自己的 `section` 当分组标题。用法：`useEffect(() => registerContextCommands((ctx) => ctx.pane ? nodeActions(ctx.pane) : []), [])`。每个节点的操作（重命名、复制路径、置顶、关闭……）由节点菜单那条线提供，合并后在这里接上，不需要改面板。
- **侧边栏的小接口**（`lib/sidebar-api.ts`，`Sidebar.tsx` 只多几行订阅）：`setAllFolded`（`useFolded` 订阅，折叠 / 展开写进同一个 `fold:*` 键）、`cycleGrouping`（写同一个 `jarvis.agentGrouping`，`AgentsBlock` 订阅）、`stepSidebarRow`（在带 `data-sidebar-rows` 的列表里找看得见的行，点它）。
  布局状态（`lib/layout.ts`）：侧边栏收成图标栏记在这个浏览器（`localStorage` 的 `persona.sidebarCollapsed`，刷新后保持，别的标签页跟着变）；
  图标栏上一直有展开按钮，不会因为忘了键而找不到侧边栏。最大化只在当前页面里，刷新就回来。
- 测试：`lib/shortcuts.test.ts`（解析 / 格式化 / 冲突 / 保留 / 事件转键 / 分发 / 终端放行）、`lib/palette.test.ts`、`lib/shortcut-nav.test.ts`、`lib/sidebar-api.test.ts`、`views/Palette.test.tsx`、`lib/file-find.test.ts`（匹配 / 缩小 / 索引缓存 / 老 node 的遍历）、`views/palette/FilesSection.test.tsx`。
  浏览器里用假 hub（`MOCK_FILES=1 MOCK_FILE_FIND=1`，多几个文件给搜索排名）看：`PORT=<端口> MOCK_FILES=1 MOCK_FILE_FIND=1 python3 scripts/mock/mock_hub.py`。

### 节点菜单（#2329）

侧边栏 Agents 树和 `/terminal` 页树上的每个节点（机器、工作区 / 空间、标签页、窗格 / Agent 行）都有同一种上下文菜单，照 Moshi：右键在鼠标位置打开，行尾的 ⋯、菜单键、Shift+F10 在行旁打开
（触屏的入口还是 ⋯，不靠长按；侧边栏的机器标题只有右键 / 菜单键，没加 ⋯，样子不变）。菜单引擎是 `ui/atoms.tsx` 的 `Menu`（老用法不变），纯逻辑在 `ui/menu-model.ts`：

- 键盘：↑ ↓ / Home End 移动，→ 打开子菜单、← 回上一层，Enter 选，Esc 关；行尾的字母直接选那一行（大小写都行，带 Ctrl / ⌘ / Alt 的留给页面，页面上别的输入框里打字不触发）；⌫ / Delete 选「关闭」那一行。
  鼠标悬停子菜单行约 100 ms 后展开；位置会翻到屏幕里（贴右边向左开、贴底向上开）；窄屏照旧不被软键盘盖住。
- 危险的行（`confirm`）要第二次：第一次点 / 按键把这一行变成「确认关闭」，第二次才执行；换到别的行、4 秒没人理都会收回。
- 重命名（`rename`）：选中后那一行变成输入框，Enter 提交，Esc 取消回到菜单，没改或空的不提交。
- 忙和错（`inline`）：行尾转圈，失败的原因显示在菜单里（菜单不关），成功才关。子菜单可以是函数（打开时才读，「读取中…」）。

菜单项由 `src/lib/node-actions.ts` 的 `nodeActions(node, deps)` 生成，所有树共用，命令面板也用它（见下）：

| 节点 | 项 |
| --- | --- |
| 机器 | 折叠 / 展开 Z、刷新 R（终端页）、复制机器 id I |
| 工作区 | 折叠 Z、新建 Agent… N、新建终端 T（终端页）、重命名… R（终端页，行内）、新建 worktree… W（宿主能开 worktree 时，#2431）、Git ▸ G（拉取 P / 推送 U / 获取 F / 复制分支名 B）、复制路径 C、复制 herdr id I |
| 标签页 | 重命名… R（行内；有 Agent 的标签页走原来的重命名接口）、新建窗格 ▸ N（向右 / 向下）、在这里运行 Agent… ▸ A（单个普通 shell 窗格）、Git ▸ G、复制路径 C、复制会话 id S、复制 herdr id I、关闭标签页 ⌫ |
| 窗格 | 新建窗格 N、在这里运行 Agent… A（普通 shell）、Git G、复制路径 C、复制会话 id S、复制 herdr id I、关闭窗格 ⌫ |

改电脑的项（关闭、拆分、重命名标签页、运行 Agent、Git）只在 herdr 宿主、机器在线时出现；只读的复制项到处都有，没有的值（没有分支、没有会话）不出现。
Agent 行的菜单保留原来的全部项（置顶 P、重命名 R、拷贝目录 C……），后面接上这一组；已有重命名对话框的行（Agent 行、侧边栏空间）不再加行内重命名。

接口（都是 `POST /api/v1/machines/{mid}/…`，**只有 `full` 设备**；`answer` 设备和扩展令牌被拒，route scopes 里是「仅凭据」）：`panes/close {pane}`、`panes/split {pane, direction: right|down}`、
`panes/agent {pane, launcher}`、`panes/git {op: pull|push|fetch, pane | workspace_id+kind}`、`tabs/close {tab_id, kind?}`、`tabs/rename {tab_id, label, kind?}`；工作区重命名用原来的 `spaces/{wid}/rename`。
请求只带这些字段（别的会被丢掉），不能指定目录或命令，细节见 `docs/terminal.md` 第二十一节。

- **新建 worktree…**（#2431，W）：工作区的菜单和命令面板里的一项，弹出「分支 / 检出路径」对话框（`views/NewWorktree.tsx`，逻辑 `lib/worktree.ts`）。路由 `worktree/prepare {workspace_id, kind?}`、`worktree/create {workspace_id, branch, path, kind?}`，同样只有 `full` 设备；
  只在 `panes` 里宿主带 `worktree: true` 时出现（旧 node 不带，就没有这一项）。检出路径跟着分支名走、手改后不再跟、清空后恢复；创建成功后跳到新工作区的终端。细节见 `docs/terminal.md` 第二十三节。截图用 `MOCK_TERMINAL_PANES=1` 的假 hub（`worktree/create` 里分支或路径带 `taken` 会被拒绝，看错误提示）。

- **`/terminal` 页**：机器、工作区、标签页三层都能折叠（点标题；左边出一个小箭头，悬停时显示，折叠后一直显示；键 `fold:term:…`，机器 / 工作区名为键，herdr 重启后 id 变了也保留），搜索时不管折叠。
  行尾悬停出 ⋯（触屏一直在）。
- **侧边栏显示终端**（设置 › 通用 › 外观，默认关，只对这个浏览器；`lib/sidebar-terminals.ts`）：Agents 按机器分组里，每台电脑的 Agent 下面多一组「终端」，列它终端里的**普通 shell**（Agent 在上面已经有了），点开是 `/terminal/<机器>/<id>`（有会话的 Agent 窗格进它的会话页的终端视图），同一套菜单。
- **Agent 行显示详情 / 上下文显示为已用**（#2430，设置 › 通用 › 外观，都默认关，只对这个浏览器；`lib/context-usage.ts`）：开了「Agent 行显示详情」，侧边栏每个 Agent 行标题下多一行小字「模型 · 上下文 · 多久没动 · 目录」（放不下的从末尾起折到看不见，不挤别的）。会话顶栏标题行末**始终**有模型名和一条细的上下文条（`views/chat/ContextMeter.tsx`，照 Moshi 的会话头部，用户要求照 Moshi 做；窄屏只剩条和百分比），悬停写「还剩 N% 上下文」；「上下文显示为已用」改成「已用 N%」，两处一起变。已用 ≥ 80% 变警告色（`--orange`）。两个开关都关时侧边栏和以前一像素不差。数字来自会话的 `context_tokens` / `context_window`（node 从 transcript 尾部读，见 `protocol.md`）；没有这两个字段（还没回复、node 不认识的模型、老 node）就不画上下文，只剩模型和其余几项。
  关着时侧边栏和以前逐像素一样。
- **命令面板**：`lib/node-palette.ts` 向 `registerContextCommands` 登记，在 `>` 命令模式里把当前节点的菜单项列在最前（「Workspace · 窗格标题」「Workspace · 工作区名」两节）；
  重命名弹一个小对话框问名字，危险的再确认一次，失败弹 toast。当前节点来自会话页的对话（`ctx.pane`）或终端页的窗格（终端页把读到的树交给它）。
- 测试：`ui/menu-model.test.ts`（键、字母、确认、重命名、右键位置）、`ui/atoms.test.tsx`、`ui/overlay.test.ts`、`lib/node-actions.test.ts`、`lib/node-palette.test.ts`；浏览器里用
  `MOCK_TERMINAL_STREAM=1 MOCK_TERMINAL_CONTROL=1 MOCK_TERMINAL_PANES=1`（可加 `MOCK_SPLIT_TAB=1`，侧边栏出现一个两窗格的标签页）的假 hub，动作记在 `/mock/node_actions`。

### 链接在页面里打开

CLAUDE.md 要求链接在 App 内打开；网页上浏览器本身就是「App」，按链接分：

- **飞书文档 / 知识库 / 表格 / 思维笔记 / 幻灯片 / 文件、公司圈帖子**：hub 的 features 有 `lark` 时在侧边面板里用原生阅读器打开（判断规则照 iOS 的 `FeishuLinks`；没有 `lark` 时当普通网页开新标签页），
  `GET /lark/raw/documents/read?format=markdown`，作者禁止导出或格式不支持时改取节点树（`format=json`）。指针停在链接上就预读，最近 12 篇缓存在内存里。
  头部：评论（未解决的条数）、作为上下文（整篇）、在新标签页打开、拷贝链接、重新载入；读不了时分别说明（hub 没接飞书、没登录、没权限、不存在）。多维表格直接开新标签页（iOS 也只是一页「暂不支持」）。
- **飞书文档的评论**（#1290，同 App 的 #1675、#1729；`src/views/link/DocComments.tsx`、`src/lib/doc-comments.ts`）：
  - 头部的「评论」或正文末尾的「N 条评论」打开评论视图。它盖在文档上，文档的位置不动。
  - 分「未解决 / 已解决」两栏，线程带引用的那段和每条回复，@ 的人高亮。
  - 只读：hub 的飞书插件没声明 `lark_doc_comments` 时只能看，评论从 `/lark/raw/documents/comments` 读。
  - 插件声明了 `lark_doc_comments` 时还能写：
    - 每条线程有「回复」「解决 / 重新打开」；
    - 底部的框评论全文；点了回复，框上写「回复 某某」；
    - 选中正文后，小条里多一个「评论」，评论那一段（带 `quote`，节点树的段落和标题再带 `block_id`）；
    - 输入 @ 和名字找人（`src/ui/LarkPeoplePicker.tsx`，候选来自 `/lark/documents/people`，写成可以复用的组件，飞书聊天要 @ 时直接拿）。
  - 写的规矩：每次写带 `client_id`，重试同样的字不会发两次；没发出去时字留着，说明 hub 的原话。
  - Esc 一步步退：先关 @ 的候选，再从回复 / 评论这段回到评论全文，最后回到文档。按 Esc 不会中断对话里正在工作的 Agent。
  - 只做评论，不改正文。
- **其他网页**：新标签页。大多数站点用 X-Frame-Options / CSP 禁止被嵌进 iframe，页面又判断不出被拦了，嵌进面板只会是白屏。
- **本 hub 的 Agent / 任务页**（#2485）：和当前页面同源的 `/s/<机器>/<会话>`、`/todos?id=<todo>`（助手回答里交代「交给谁去办」时写的就是这种链接）在侧边面板里打开，
  用的就是任务坞的 Agent / 任务面板，当前对话不动；窄屏没有右侧面板，在页面内跳过去。面板头部有「在新窗口打开」。别的来源的同样路径、hub 的其他页面照旧开新标签页。
  助手对话 `/j/<对话 id>`（#2680）在页面内跳过去（路由跳转，不整页刷新）。「本 hub」除了当前页面的 origin，还认 hub 在 hello 的 `endpoints` 里声明的地址
  （`public_url` 排第一，助手写链接用的就是它）：经局域网地址打开网页时，助手写的公网地址链接也在页内打开（`lib/links.ts` `hubOrigins`）。
  只认同源：助手写的地址和浏览器打开网页的地址不是同一个时（比如一个用内网地址、一个用公网域名）不拦。
- **`jarvis://session/…`**：在页面内跳到那个会话。⌘ / Ctrl / 中键点任何链接都交给浏览器开新标签页。
- **选中文字**：阅读器里选中后浮出「作为上下文」「AI 修改」。前者在当前活动的输入框上方加一个上下文胶囊（随下一条消息作为 `<jarvis-context>` 发出），
  后者填要求后立即发出（`<jarvis-context kind="edit">` + 要求，正文格式同 iOS）。活动的输入框是最近挂载或获得焦点、能发送的那个；页面上没有时转到 Persona 主对话。
  代码在 `src/lib/links.ts`、`src/views/link/`、`src/views/chat/Composer.tsx`（胶囊和拼装）。

### 互联（多台 hub）

网页由一台 hub 托管、连的就是这台；互联后它把组里别的 hub 的电脑当成自己的列出来（`docs/plan-hub-mesh.md` 2.6），网页不做多组。
不互联的几台 hub 之间用下面的「切换 hub」来回跳。
机器列表里出现两个以上的 `hub_id` 时，机器卡片（hub 胶囊，悬停看线路）、机器页（hub 和线路）、总览的电脑和任务行、侧边栏的空间 / 机器分组、
对话页顶栏、⌘K 搜索结果、新建 Agent 的电脑都写上 hub 名（`src/lib/mesh.ts`：`hubLabel`、`machineWithHub`、`routeText`）；只有一个 `hub_id`（或旧 hub 没有这个字段）时什么都不加，界面不变。
设置 › 通用 › 这个浏览器显示这台 hub 的名字（hello 的 `hub_name`）和互联的 hub（鉴权后 hello 的 `mesh.hubs`）。

### 切换 hub（#1014）

一个人用着几台互不互联的 hub（自己的、家里人的）时，网页在它们之间切换。Android 是同时配对多台（`docs/android.md`「多 hub」），网页不这么做：
一个页面同时连几台要 CORS 和跨源 token，和同源的设计冲突。所以**切换就是打开那台 hub 自己托管的网页**：

- 每台 hub 的登录照旧存在它自己的源里，token 从不跨源；第一次打开某台要贴它的 token（或用登录链接），之后直接进。
- 列表（`src/lib/hubs.ts`）只有名字和地址（`scheme://host:port`，即 `location.origin`；挂在反向代理子路径下的再带上前缀，见上面「子路径」），存在这个源的 `localStorage`（`persona.hubs`），最多 20 台。
  打开过的 hub 自动记下：登录页记地址，登录后再记 hello 的 `hub_name`。用户起的名字（`label`）优先于 hub 自己的名字。
- 跳转时把列表放进地址的 fragment（`<那台 hub>/#hubs=<JSON>`，不发到服务器）。对面的页面在 React 渲染之前读出来、合并进自己的列表（自己已有的保留顺序和名字），
  再用 `history.replaceState` 从地址栏抹掉，和登录链接里的 token 一样。读不出来的、不是 http(s) 地址的、路径不是一个干净前缀的条目一律丢掉。
- 入口：左上角菜单在外观下面列出其他 hub（点一下切过去）；设置 › 通用 › hub 管理列表（「添加 hub」：地址加可选的名字，没写协议按 https，带用户名、
  查询串的不收，路径只收反向代理的前缀（像页面地址的 `/settings`、`/s/…` 不收），输入时预览将打开的地址；改名只改这个浏览器里的叫法；删除只是从列表里拿掉）；登录页列出「其他 hub」，进了一台还没登录过的 hub 也能回去。
- 列表里没有别的 hub 时，菜单和登录页一像素不变；只有设置里多一节「hub」，要从这里添加第二台。

## 连接

一个 WebSocket（`/api/v1/ws?token=`）：`hello` 后整体刷新一次 REST（会话、交互、助手、机器），之后靠推送（`sessions` / `session` /
`interaction*` / `items` / `delta`）。断线指数退避重连（0.6 秒起，最多 15 秒），超过 70 秒没收到任何消息（hub 会 ping）就当连接已死重连，
页面回到前台、网络恢复时也重连；连不上时顶部出现「连不上 hub，正在重试…」。重连后已打开的对话重新订阅，带 `since_seq`（本地连续拿到的最后一行），
`from_seq` 接不上时 1.5 秒后按 `after_seq` 补洞（`docs/protocol.md`「items 的连续性」），`history` 变了就整页重取。
store 自己不存的推送（`inbox`、`lark`、`terminal`、`terminal_control`、`assistant_schedule`……）由用到的页面按类型订阅（`onHubEvent(type, fn)`，`src/lib/store.ts`）；
hello 的 `features`、`hub_name`、`mesh` 存在 store 里。经 lark-connector 的飞书由扩展提供（feature `lark`，公司圈 `lark_moments`）：依赖 `/api/v1/lark/…` 的地方——「消息」页和侧边栏入口、设置 › 连接器 › 飞书、翻译里的飞书会话、飞书文档 / 公司圈帖子的阅读器——都按 `store.larkOn()` / `useLarkOn()` 显示；hub 还没说 features（hello 之前）时照旧显示。收件箱里带 `lark` / `doc` / `moment` 块的条目照常画（只有扩展在时才有）。知识库的帖子存档带 `feed` 时用它当帖子页标题，没有时叫「公司圈」（#1530）。

## 安装成 App（PWA，#1772）

网页可以装到手机主屏幕 / 电脑桌面，独立窗口打开，图标、名字和 iOS App 一致。

- **manifest**（`public/manifest.webmanifest`）：`standalone`、`start_url` / `scope` 都是 `./`（相对 manifest，根路径时就是 `/`，子路径下是 `/<前缀>/`），背景和 theme 色是浅色的 `--page`；
  长按图标的快捷方式：新对话、收件箱、Agents、机器。hub 给 `.webmanifest` 显式设 `application/manifest+json`（Go 的内置表里没有，又开着 nosniff），
  它和 `sw.js` 跟 `index.html` 一样 `no-cache`（`server/internal/hub/web.go`）。
- **图标**全部从 `AppIcon.appiconset` 缩出来，不重画：`app-icon-192/512.png`（`purpose: any`，桌面安装用 Mac 图标那种带圆角和边距的 `mac-512@2x.png`）；
  `app-icon-maskable-192/512.png`（Android 的遮罩：满版的 `icon.png` 缩到 84%，四周用它自己的边缘延展补满，挥手的线落在中间 80% 的安全圆里）；
  `apple-touch-icon.png`（180，满版 `icon.png`，iOS 自己加圆角）。
- **iOS**：`apple-mobile-web-app-capable`、状态栏 `default`（内容从状态栏下面开始，深浅色都不会白字压浅底）；独立运行时 `#root` 让出底部和横屏两侧的安全区（`styles.css` 的 `display-mode: standalone`）。
  主屏幕上的 App 和 Safari 的存储是分开的，第一次打开要重新登录（贴 token 或用 `?token=` 链接）。
- **theme-color**：两条 `<meta name="theme-color">`（浅色 / 深色的 `--page`），跟系统；设置里选了浅色或深色，`index.html` 的首帧脚本和 `views/theme.ts` 把对应那条的 `media` 改成 `all`，地址栏 / 标题栏的颜色跟着页面走。
- **service worker**（`src/sw.ts`，Vite 单独打成根目录的 `sw.js`，规则在 `lib/sw-routes.ts`，有单测）：
  - 打开页面：先等 hub 的 `index.html`（3.5 秒），拿到就用并存一份；hub 慢或连不上就用存的那份，页面照常起来，显示自己的「连不上 hub」。外壳只按 `/` 一个键存，`?token=` 链接不会进缓存。
  - `/assets/` 下带哈希的文件：缓存优先，没有再取；保留最近 400 个（约三次构建，换版本不会删掉正在用的）。外壳一更新就顺带取它首屏要的脚本和 CSS，下一次断网也能打开。
  - 根目录的图标和 manifest：先取 hub 的，取不到用存的。
  - `/api/`（数据、WebSocket、图片）和 `/admin` 一律不经过它，数据永远是实时的。
  - 只在安全上下文注册（https 或 localhost）：内网用明文 http 打开时就是普通网页，iOS 照样能「添加到主屏幕」，但没有离线外壳；Chrome / Edge 要 https 才能安装（公网 TLS 见 `docs/network.md`）。
    开发时（`npm run dev`）不注册；挂在反向代理的子路径下时也不注册（见上面「子路径」）。
- **设置 › 通用 ›「这个浏览器」›「安装为 App」**（`lib/pwa.ts`）：浏览器给了安装提示（Chrome、Edge）就是一个「安装」按钮；
  iPhone / iPad 提示 Safari 分享菜单的「添加到主屏幕」，Mac 上的 Safari 提示「文件 › 添加到程序坞」，http 下的其他浏览器说明要 https，别的指向浏览器菜单；已经在独立窗口里时显示「已安装」。

## 订阅额度（侧边栏底部，#2432）

侧边栏最下面（Agents 列表之下）：最满的两个订阅账号各一条细条，≥ 80% 变警示色；点开是浮层，每个账号每个窗口一行（长度、已用 %、条、重置倒计时、节奏提示），
标题右边是采样时间。**没有数据时整块不画**。数据来自 `GET /api/v1/usage/windows`（每台电脑的 node 读本机文件，hub 合并同一个账号），
每分钟刷新一次，页面不在前台时不请求。节奏、倒计时的算法和 Claude 额度怎么接入见 [usage-windows.md](usage-windows.md)。
账号来自电脑上读到的文件和 hub 去问的账号池网关（#2595）：浮层里每个账号标题下一行写计划和来源（「本机 · relay-history」，只有一个来源写那一个），
单模型的窗口在长度旁写模型名（不参与排序），网关读不到时写一句「显示的是上次的数字」；侧边栏细条的提示里也写来源。网关在管理界面的「用量来源」里配（`docs/admin.md`）。
代码：`lib/usage-windows.ts`（纯函数，有单测）、`views/UsageBars.tsx`；假 hub 有 `MOCK_USAGE=none` 模拟没有数据、`MOCK_USAGE=gateways` 模拟电脑和网关混合的账号。

## 注意力信号（标签页标题、图标角标、提示音，#2332）

人不盯着网页时，也要知道有 Agent 在等自己。逻辑在 `src/lib/attention.ts`（纯函数，有单测），页面副作用在 `src/lib/attention-signals.ts`，`App.tsx` 里只有一次 `useAttentionSignals(title, pane)` 调用（它接管了原来设 `document.title` 的那个 effect）。

- **数字**：就是侧边栏「总览」红色角标的那个数（`lib/sessions.ts` 的 `stateCounts().badge`，经 `useCounts()`）：状态是 `needs_you` 的 Agent，不含安静任务的（#1731）。不另算一份，标签页和侧边栏不会不一致。
- **标签页标题**：有 N 个需要你时前面加 `(N) `，后面还是原来的页面名（`(2) 设置 · Persona`、`(2) Persona`）；0 个时和原来完全一样。
- **图标角标**：在 `index.html` 里那两条 `<link rel="icon">`（浅色 / 深色各一条，`media` 按系统）的原图上画一个红点（`--status-needs`），1–9 写数字，更多写 `9+`，把两条的 `href` 各换成画好的 data URL；变成 0 时还原成原来的路径。深浅色各用自己的那张图，不新增图标文件。
  图标文件仍经 service worker 的 `static` 规则（先取 hub 的，取不到用存的），所以离线时也能画；画好的 data URL 不走网络、不进缓存，`sw.ts` 不用改。
- **提示音**（设置 › 通用 › 外观 › 「提示音」，默认关，存在当前浏览器 `persona.attentionSound`）：用 WebAudio 合成，没有音频文件。需要你时是上行的两个音，完成时是一个低而轻的音。
  - 浏览器在用户第一次点击 / 按键之前不让出声：`armAudio()` 在第一次手势时建 `AudioContext` 并恢复，在此之前什么都不播、也不排队。
  - 正在看的那个会话（当前页是它的对话）且窗口在前台（可见且有焦点）时不响；窗口在后台或在看别的会话照响。
  - 限频：3 秒内最多一声（被拦下的不排队、不顺延）；同一批里需要你优先于完成。
  - 刚打开页面时第一次拿到的状态只作基线，不响。
- **「完成」怎么判**（照 hub 的完成推送，`server/internal/hub/machine.go` 的 `notifyFinished`、`notify.go` 的 `settle`，见 `docs/agent-states.md`）：一个会话从 `working` 变成 `unread`（完成未看），且没屏蔽通知、不是安静任务。
  变成 `idle`（已看，或 hub 分诊为「做完了」）不响，和 hub 一样：它看 `seen` 就不推。后台命令 / 子 Agent 还在跑的会话 hub 本来就算工作中（`backgroundState`），所以也不会提前响。
  与 hub 的差别：① hub 等 `done_delay` 再复核，网页在 1.5 秒后看它是不是还停着（状态在 working → done → working 间抖动时不响）；② hub 丢掉「后台任务汇报唤醒的一轮」（`WokeByTask`）和「助手刚推过」的，网页看不到这两个信息，照响；
  ③ hub 的推送还看每台设备的「完成」偏好（总是 / 长任务 / 关）和轮次时长，网页只有一个总开关；④ 同一轮回复的去重（`stopDuplicate`）在 hub 里，网页只靠 1.5 秒的复核和限频。
- 需要你的声音不看屏蔽（和筛选一致：需要你的永远显示），安静任务的会话两种都不响（侧边栏也不计）。

## 手机上（#1996）

窄屏（`lg` 以下）和触屏的做法，宽屏桌面保持原样：

- **外壳**：
  - 抽屉在页面任意位置右拖拉出、左拖关上（`lib/drawer.ts`：离水平 45° 以内才算，快甩或者拖过一半才开或关；手指落在能横向滚动的东西上时让它先滚），Esc 和返回键也能关。关着时整块 `inert`，打开后焦点圈在抽屉里。
  - 窄屏抽屉里，总览 … 设置这些页面收成两行图标格，打开就能看到 AGENTS 和机器树。
  - 顶栏中间是「助手 | Agents」切换（和 iOS 新外壳一样，底部不加 tab 栏）。Agents 页是 `/agents`，内容就是侧栏里的 Agents 区块；两页两侧的按钮用同一套 80px 槽位（`views/chat/header.ts`），位置一像素都不差。
- **返回键先关浮层**：抽屉、全屏侧边面板、图片查看器、底部 sheet 打开时各压一条同 URL 的历史记录（`lib/back-close.ts`）。切 pane 用 replace，不新增记录。旧的 `/s/` 链接打开后换成对应的 `/t/`。`/s/` 里的会话 id 可以只写前面至少 8 位（助手、通知里的短 id，#2464）：按这台机器的会话列表解析，只对上一个就原地换成完整 id 的链接，对上多个就列出来让人选，都对不上照原样问 node，找不到时说「找不到这个会话」（`lib/session-link.ts`）；node 只认完整 id。
- **输入区**：
  - 输入框上面那一栏最多占可见高度的约 55%，自己滚动。有多张待处理卡片时只展开第一张，其余收成「还有 N 件 ›」。可见高度不到 500px（横屏、键盘弹起）时，卡片收成一行，计划步数、运行状态、队列也并成一行。
  - 输入框最多 `min(240px, 可见高度 × 35%)`。
  - 软键盘：`lib/viewport.ts` 把可见视口写进 `--kb-top` / `--kb-h`，侧边面板、sheet、菜单都按它定位。
  - 触屏上回车换行，有精确指针（比如 iPad 接了键盘）时回车发送。
- **阅读**：
  - 正文用 `--chat-body` / `--chat-secondary`：宽屏 15 / 14，窄屏 16 / 15。
  - 复制代码、Mermaid 的工具按钮在触屏上一直显示。
  - 图片用 `ImageViewer`，支持捏合、双击、左右滑。
  - Diff 在窄屏全屏显示；宽表格可以横向滚动，列不会挤成一字一行。
  - KaTeX 只在回复里有公式时才加载。公式（rehype-katex）和 Mermaid 图里的公式用同一份 KaTeX：`web/package.json` 的 `overrides` 把它们都指到根上固定的版本，升级 Mermaid 时根上的 katex 跟着它要的版本走，`npm ls katex` 只有一个版本、构建只出一个 `katex-*.js`（#2652）。
- **控件**：
  - 触屏上的点按区域至少 44×44（`touch-hit` 工具类：一块看不见的方形，控件看上去不变大）。
  - 输入框字号至少 16px，否则 iOS 聚焦时会放大页面。
  - Dialog 在窄屏是底部 sheet：正文滚动、底栏固定，焦点圈在里面，返回键能关。
  - 菜单在 pointerdown 时判断点到了外面，长按打开后松手不会立刻关掉。
  - 设置在窄屏是分类列表，点进去是子页，返回回到列表。
- **首屏**：首屏用不到的页面按路由懒加载，主包从 1.68 MB 降到 0.90 MB（gzip 从 516 KB 降到 296 KB）。从来没连上过 hub 时显示「离线」和重试，不会一直转圈。
- **安全区**：浏览器里左右两侧也留安全区（横屏刘海），底部只在装成 App 时留；抽屉自己也留。
- **截图和端到端检查**：`scripts/test/web_mobile_{input,read,shell,pages}_e2e.py`，覆盖 390 / 360 / 844×390、深色和浅色。触摸用 CDP 的真实事件，键盘用缩小视口高度来模拟。用法见各脚本开头。没有真机，iOS Safari 的惯性滚动、长按时系统弹的预览、装成 App 后的启动画面都还没验证。

## 国际化（#2452）

约定（语言、回退、术语表）见 [i18n.md](i18n.md)。网页的做法：

- **加载**：`main.tsx` / `admin/main.tsx` 先 `await initI18n()` 载入当前语言的词条，再动态 import 其余模块，所以 `t()` 在模块顶层也能用。
  语言在页面的整个生命周期里不变；设置 › 通用 › 外观 › 语言改了就 `location.reload()`（选择存在 `localStorage` 的 `persona.language`，没有就跟随系统）。每种语言单独一个 chunk，只下载用到的那种。
  客户端的词条（`locales/<语言>/index.ts`）不含管理后台的 `admin-*` 命名空间；它们在 `locales/<语言>/admin.ts`，只有 `admin/main.tsx` 经 `admin/catalog.ts` 叠加载入
  （`initI18n(undefined, adminCatalog)`，管理后台也用 `common.*` 和共享组件的词条），构建时和管理后台代码一起进 `admin/assets/`。
- **词条**：`src/i18n/locales/<语言>/<命名空间>.json`，平铺的 `"名字": "文字"`，key 写成 `命名空间.名字`。**一个页面或一块功能一个命名空间**（`settings-general`、`inbox`、`admin-devices`…），
  几个人同时改不会撞同一个文件；`common.json` 只放到处都用的（取消、保存、加载中），别往里堆。
- **写法**：`t("inbox.empty")`、`t("inbox.count", { count })`（有 `_one` / `_other` 时按 `Intl.PluralRules` 选）；带元素的句子用 `tRich("ns.key", { link: <a…/> })`，
  整句一个词条，不拼半句。日期、数字、列表用 `src/i18n` 的 `formatDate` / `formatNumber` / `formatRelative` / `formatList`（都是 `Intl`），不手拼「月」「日」。
- **和 hub**：每个请求带 `Accept-Language`（`lib/api.ts`），WebSocket 连上先发 `client_hello.locale`（`lib/store.ts`），hub 据此出错误文案、推送和助手回答的语言（[protocol.md](protocol.md)）。
- **检查**（`src/i18n/catalog.test.ts`）：三种语言的命名空间、key、占位符一致，`_one` 有 `_other`，「Persona」不翻译；代码里 `t("…")` 的 key 都存在；
  客户端词条里没有 `admin-*`、两份合起来正好是全部词条，`admin-*` 的 key 只在 `src/admin/` 下用；
  写死的中文（字符串、JSX 文字，注释和测试不算）按文件只能减少，基线 `src/i18n/hardcoded-baseline.json`，减少后 `UPDATE_I18N_BASELINE=1 npx vitest run src/i18n/catalog.test.ts` 重记。
  必须保留的中文（要和别人写的原文匹配的，如解析 hub / 飞书的输出）在那一行或上一行注释写 `i18n-ignore`。
- **测试**：vitest 的 `setupFiles` 先载入 zh-Hans（客户端和管理后台的词条都载），现有测试照旧按中文断言。
- **key 的坑**：key 以 `_zero` / `_one` / `_two` / `_few` / `_many` / `_other` 结尾会被当成复数形式（`doc_other` 要写成 `doc_cloud`）；
  模块里有局部变量叫 `t` 的要改名，别遮住 `t()`。要和别人写的原文匹配的中文（hub 写进对话的通知、`<jarvis-context>` 里的标记、引用的首行）不翻，标 `i18n-ignore`。

## 细节约定

- 颜色只走 token（`src/styles.css` 的 `:root` / `.dark`），不写死色值；状态色：需要你红（呼吸）、工作中橙（呼吸）、完成未看绿、已看灰。
- 主题：跟随系统 / 浅色 / 深色（左上角菜单），`index.html` 里在首帧前就决定，深色不闪白。
- 品牌标志：侧边栏左上角（32 px、圆角 8）和登录页（44 px、圆角 12）是 iOS App 图标原图（`AppMark`，`public/app-icon.png` / `app-icon-dark.png`，
  从 `AppIcon.appiconset` 的 `icon.png` / `icon-dark.png` 缩出来，不重画），按页面的深浅色换；favicon 是同一份的 64 px 版，按系统深浅色换（`<link media>`），管理页也用它。
  安装成 App 用的图标见下面「安装成 App」。形象里可选的「标志」（黑底白星）是另一回事，不动。
- 按钮文字一律单行（`whitespace-nowrap`），放不下的用图标 + `title`。
- 链接规则和 App 一样（`Links`）：裸链接在空白或非 ASCII 字符处结束，交给 Markdown 前写成 `<url>`（`src/ui/Markdown.tsx` 的 `linkify`），中文里的链接不会吞掉后半句。
- 尊重 `prefers-reduced-motion`：所有动画缩成 1ms。
- 展开 / 折叠（#1124）一律用 `Collapse`：CSS 把 grid 的行在 `0fr` / `1fr` 之间过渡、透明度淡入淡出，箭头转 `transform`，不在 JS 里量高度、不按帧 setState。
  对话里的（工作行、工具步骤、思考、上下文、最新一步的预览）用 `lazy`：展开时内容先在 `0fr` 下排一次版、下一帧才开始过渡（长轨迹第一次排版很重，混进过渡里就一下子开到大半）；
  折叠时内容留到过渡结束再卸载（跟着高度收起，不是直接消失），合上之后才 `inert`。
  对话页按住被点的那一行（`ChatView` 的 `anchor`，`lib/disclosure.ts`）：点下去时记下它在屏幕上的位置，过渡中内容每变一次大小（ResizeObserver，排版后、绘制前）就把滚动补回去，被点的行不动；
  从工作行底部「收起 N 步」折叠时按住这一行的底边，组头落到按钮原来的位置。在最新处时照旧跟随最新处，只有会把这一行推出顶部、或者一帧里要挪超过 64px（折叠的步骤一下子取回来）时才停止跟随、改成按住它。
  滚轮、触摸、按键都会放开；滚动视图本身仍是 `overflow-anchor: none`（浏览器自己挑的锚点可能在展开的行下面，反而把被点的行推走）。
- `useStore(selector)` 的结果按浅比较复用：选择器可以返回新数组，没变就不重渲染（否则 `useSyncExternalStore` 会无限循环）。

## 和手机端的差距（#297、#433，2026-11-03）

以 iOS 为准、Android 参考，逐项对照。#297 做了第一轮，#433 把那一轮标为「不做」的补上。「网页」：有 / 部分 / 没有 / 另有 PR / 不适用（手机专属或浏览器里没有意义）；「轮次」是哪一轮做的。

### Agents 和对话

| 手机端的功能 | 网页 | 轮次 | 说明 |
| --- | --- | --- | --- |
| Agents 列表：按项目 / 状态 / 机器分组、空闲折叠、最近未运行 | 有 | — | 侧边栏 |
| 快速筛选（全部 / 需要你 / 未读 / 工作中）、全部标为已读 | 有 | #433 | 筛选在侧边栏 Agents 标题下；全部标为已读在筛选「未读」、空间菜单、总览 |
| 置顶 Agent / 空间 | 有 | #433 | 侧边栏最上面「置顶」一段，hub `/pins` |
| 屏蔽通知（Agent / 空间） | 有 | #297、#433 | Agent 在对话 ⋯ 和行菜单，空间在空间标题的 ⋯ |
| 需要你的提醒（标签页标题计数、图标角标、可选提示音） | 有 | #2332 | 见上面「注意力信号」；没有推送，只在网页开着时有效 |
| 重命名 Agent（tab 名、会话名）/ 空间 | 有 | #297 | 对话 ⋯、行菜单、空间菜单 |
| 关闭 Agent | 有 | — | 对话 ⋯；行菜单里没有（还有停止循环、实时接入），iOS 的行菜单有 |
| 删除对话（从列表移除） | 有 | #801 | 未运行的会话：对话 ⋯、行菜单、右键（先确认）；iOS / Mac / Android 也有（#1749） |
| 空间页、机器页（MachineSpacesView） | 部分 | #297 | 机器页按空间列出这台的 Agent，没有单独的空间页 |
| 父子空间（Orca 子工作区折叠在父工作区下） | 网页先有 | #2073 | 网页「按机器」分组有；iOS / Mac / Android 还平铺，另开 issue 跟进 |
| 卡片 / 细列表、全部折叠、按机器筛选 | 没有 | — | 侧边栏只有一种密度；按机器看用「按机器」分组 |
| 新建 Agent（机器、目录、启动器、首条消息和附件） | 有 | — | 互联时电脑旁写 hub 名 |
| 「交给 Persona」（Agents 长按菜单） | 有 | #433 | 行菜单，经跨页的待发消息发进主对话 |
| 对话展示（工作行、折叠、轨迹、diff、图片、待办） | 有 | — | |
| 展示逻辑对齐 `ChatLines`：合并的交换、工作中插入、已中断、来自 X、循环唤醒 | 有 | #433 | `chatLines()` / `buildBlocks()` |
| 交互卡片（提问 / 计划 / 授权 / 终端对话框、Codex） | 有 | — | |
| 输入框（/ 命令、附件、建议、中断、草稿、被拒时清空并发送） | 有 | — | |
| 「@」引用文件和其他会话 | 有 | #433 | 工具步骤折叠（`fold_work`）时没展开的文件不在候选里；没有全目录模糊搜索（iOS 也没有） |
| 权限模式、思考强度 | 有 | #297 | 输入框下方两个胶囊 |
| 会话信息 | 有 | #297 | |
| 排队消息：撤回编辑、删除 | 有 | #433 | 输入框上方的排队条；不做乐观更新，等 hub 推回新队列（同 iOS） |
| 循环、后台任务的标签，子 Agent 只读查看 | 有 | #433、#1963 | 网页的子 Agent 是消息流里的卡片，「查看完整过程」在侧边面板读；iOS 仍是标签条 |
| 离开期间回顾、后台任务唤醒的小条和抽屉、API 报错卡片 | 有 | #433 | 抽屉是居中弹窗（iOS 是底部 sheet）；通知详情里的「跳到发起的这一步」没做 |
| 长按消息（复制 / 选择 / 引用 / 编辑 / 分享卡片） | 有 | #433 | 悬停菜单 + 右键，触屏长按；分享卡片不做（手机的分享面板） |
| 图片全屏查看 / 编辑 | 有 | #433、#1753 | 左右翻、框选 / 画笔、滚轮 / 捏合缩放和拖动；「这一轮看过的图片」画廊（`TurnImages.tsx`）还不能缩放 |
| 链接在应用内打开 | 有 | #297 | 见「链接在页面里打开」 |
| 文件（可点的路径、查看器、目录浏览） | 有 | #297、#1669 | 消息正文里的路径先 `/stat` 再内联成链接，带行号跳转（iOS 是消息下方的文件块） |
| Git（改动、diff、提交历史） | 有 | #297 | |
| 终端：实时流、接管 / 只看、打字 / 滚轮 / 点击、断线重试 | 有 | #297 | |
| 终端：键栏、快捷命令、往上翻 herdr 历史、换行开关、字号、中文空隙 | 有 | #433 | 见「侧边面板 › 终端」 |
| 终端 ⇄ 对话：会话页中间原地切换（顶栏「终端 \| 对话」、⌘J、记住每个 Agent 上次的视图、`?view=`） | 有 | #2419 | 照 Moshi 的会话页；iOS / Android 的终端还是单独的 tab |
| 终端：粘贴 / 拖入图片和文件（传到电脑，路径敲进窗格） | 有 | — | iOS / Android 还没有入口，接口可以共用（`docs/terminal.md` 第十八节） |
| 终端：输出里的网址可点、右键菜单、⌘⌫/⌘←/⌘→、光标样式 / 闪烁 / WebGL、焦点策略、搜索（屏幕 + 已加载的历史） | 有 | #2331 | 见「侧边面板 › 终端」；接管时的「回到最新」没有，见 `docs/terminal.md` 第二十节 |
| 终端 / 输入框：把文件面板的条目拖进去，敲进 `@路径 ` | 有 | #2333 | 网页的鼠标拖动；iOS / Android 没有对应的拖放入口 |
| 终端：不开 Agent 的独立终端（机器 → 空间 → 标签页 → 窗格、新建 shell、Jump To） | 有 | — | 见「页面 › 终端」；手机端还没有 |
| 终端：只看时打字、配色 / 字体、键栏自选键 / 自动收起 / 输入框模式 / 补全、触屏手势 | 没有 | — | 电脑上有实体键盘；手机上用 App |
| 端口转发（回答里的 localhost 链接） | 没有 | — | 手机是本机监听再经 hub 转发；浏览器不能监听端口，要 hub 做 HTTP 反向代理，安全边界要重新定（`docs/ports.md`），单独评估 |
| 听写 | 不适用 | — | 用系统输入法 / 浏览器自带的听写 |

### Persona 助手

| 手机端的功能 | 网页 | 轮次 | 说明 |
| --- | --- | --- | --- |
| 新对话页（问候、设备、建议）、主对话、逐字流式 | 有 | — | |
| 历史对话：搜索、主对话置顶、重命名、归档 / 取消归档 | 有 | #297 | `/history` |
| 对话里切换启动器 / 模型 | 有 | #433 | 输入框下方的启动器胶囊；选择不跨刷新（iOS 也只在内存里） |
| 定时任务、一次性跟进、运行记录 | 有 | #297 | `/routines` |
| 记忆、关于我 | 有 | #297 | 设置 › 助手 |
| 知识库（类型、标签、搜索、条目、打标、问 Persona） | 有 | #297 | |
| 知识库条目的「开 Agent」 | 有 | #433 | 项目条目；比 iOS 多一步：项目有代码目录、那台电脑在线时把电脑和目录也填好。首条消息是原样的 `<jarvis-context>`（同 iOS） |
| 形象（名字、头像、状态动画）、资料页 | 有 | #433 | `/assistant`；默认形象是和 iOS 同一份图标坐标画的团子（`MochiFace`），原来的星形标志留作可选项「标志」；iOS 的面罩、汇流没移植；形象的配色不进网页的颜色（网页没有主题色）；侧边栏没有资料卡 |
| 「问 Persona」后在对话里看到发送中的气泡 | 有 | #433 | 跨页的待发消息（store `outbox`），收件箱「交给 AI」、Agents「交给 Persona」、新对话页同样 |

### 收件箱

| 手机端的功能 | 网页 | 轮次 | 说明 |
| --- | --- | --- | --- |
| 一个列表：飞书 / 邮件 / 云文档 / 公司圈，AI 分拣三档，已处理，筛选，搜索 | 有 | #297 | |
| 等你的 Agent 并进「需要你」 | 有 | #297 | |
| AI 草拟回复、交给 AI、AI 分身的草稿卡片 | 有 | #297 | |
| 飞书会话页：消息流与直接回复 | 有 | #297, #950 | 支持直接打字回复、引用某条消息、拷贝、让 Persona 总结；乐观上屏与重试；表情回应、资料卡、群设置不做（不做第二个飞书） |
| 飞书「消息」页（首页显示消息，#392） | 有 | #1327 | 开关存在当前浏览器；列表和置顶只在标签页内存（iOS 按 hub 和设备凭据缓存到本机）；置顶一排横滑，没有 iOS 的下拉展开 |
| 飞书聊天的 Pin：列表、定位、添加 / 移除、升级提示 | 有 | #1327 | 话题里的回复在对话框里打开；撤回的 Pin 只写「消息已撤回」，不按防撤回设置显示原文；加密消息只显示 hub 给的对外展示 |
| 飞书可见即读上报 | 有 | #433 | 首屏也报（按 `docs/inbox.md` 的决定，iOS 代码还是旧行为）；话题群不报（同 iOS） |
| 邮件详情、AI 回复 | 有 | #297 | |
| 写新邮件、手动回复、打开后标已读 | 有 | #433 | 纯文本、不带附件、草稿只在页面内存（同 iOS） |
| 邮件翻译、飞书消息翻译 | 有 | #433 | 语言判断是启发式的（浏览器没有 NaturalLanguage） |
| 云文档提到我的行和改状态 | 有 | #297 | |
| 飞书文档的评论：看、回复、解决 / 重新打开、评论全文或选中的一段、@ 人 | 有 | #1290 | 插件声明 `lark_doc_comments` 才能写；只做评论，不改正文；App 是长按段落，网页是选中文字 |
| 公司圈帖子和评论 | 有 | #297 | 只读 |
| 连接器的条目（GitHub、Telegram、信息、扩展）：来源筛选、会话页、回复或「在 X 里打开」、交给 AI | 有 | #1324 | GitHub 时间线和 Telegram 气泡只读加回复；表情回应、转发、下载附件不做 |

### 机器和设置

- **VNC**（`/screen/<机器 id>`，#1184，[computer-use.md](computer-use.md)「实时画面」）：单独的一页，不走登录和侧栏，noVNC 只在这一页按需加载；经 hub 的端口转发连那台 Mac 的屏幕共享（5900），默认只看，「接管」后键鼠交给它；App 在 WebView 里打开同一页，token 放在 fragment 里只留内存。

| 手机端的功能 | 网页 | 轮次 | 说明 |
| --- | --- | --- | --- |
| 机器列表（卡片、曲线、重连、重新部署） | 有 | — | 卡片点进机器页 |
| 机器详情（Agent、1 / 6 / 24 小时曲线、系统信息、启动器） | 有 | #297 | |
| 机器详情的「操控电脑」（开关、安装 Cua Driver、授权、屏幕共享、VNC） | 有 | #1326 | 「VNC」在新标签页打开网页的 `/screen/<机器>`（iOS 在 App 内的 WebView） |
| 负载曲线、Linux 每核柱状图 | 有 | #433 | |
| 订阅额度（Codex / Claude 的 5 小时、每周窗口，节奏提示） | 有 | #2432 | 侧边栏底部两条细条，点开浮层；手机端暂无 |
| 机器详情的「引擎」（node 检测到的引擎、版本和说明，离线也能选，保存失败可重试） | 有 | #2071 | 在机器页里直接选，不像 iOS 另开一页 |
| Orca 启动器、在 Orca 里新建 Agent | 没有 | — | #254（服务端在 #924 / #929） |
| 外观：跟随系统 / 浅色 / 深色 | 有 | — | |
| 界面风格、主题色、布局 | 不适用 | — | 网页只有 Beautiful UI 一套；新风格按 CLAUDE.md 做成新选项 |
| 形象 | 有 | #433 | 设置 › 通用、资料页 |
| 模型供应商、模型配置 | 有 | — | |
| 模型供应商的「额度」 | 有 | #1326 | 供应商弹窗里，「可用性」上面 |
| 助手（主动做事、关于我、记忆、运行在哪） | 有 | #297 | 换助手所在的机器没做（iOS 也不能改） |
| Claude Code 设置、Codex 设置 | 有 | #297 | |
| 连接器：邮箱 | 有 | #297 | Gmail 只能在手机上加（Google 授权客户端是 iOS 类型） |
| 连接器：飞书 | 有 | #297 | 只读状态 |
| 连接器：飞书 › 首页显示消息 | 有 | #1327 | 只对当前浏览器生效 |
| 连接器：飞书机器人（状态、凭证、配对、能做什么） | 有 | #1325 | 设置 › 连接器，点开是弹窗；拷贝配对码用剪贴板（http 下退回 execCommand） |
| hub 版本和更新（检查、更新、现在就更新、自动更新、记录、Docker 的命令） | 有 | #1325 | 设置 › hub；Agents 列表上方那行 hub 提示没做，提示在设置 › hub 顶部 |
| 助手的 Skills | 有 | #1325 | 设置 › 助手，只读（同 iOS） |
| 连接器：GitHub（登录来源、设备码登录、权限、收件箱选项） | 有 | #1324 | |
| 连接器：Telegram、扩展提供的连接器（开关、分步登录、进知识库 / 进收件箱、退出、删除） | 有 | #1324、#1425 | 品牌、页脚、「在 X 里回复」按连接器 id 查 `lib/connectors.ts` 的一张表；表里没有的扩展连接器用通用图标和通用文案（带它自己的名字）；微信的品牌图未定，先用通用图标 |
| 连接器：信息（iMessage） | 部分 | #1324 | 只看状态；「交给 Persona」开关和删除数据只在手机上 |
| 扩展（列表、开关、按 schema 的表单、只写的密钥） | 有 | #1207 | 安装、升级、卸载在 hub 机器上（同 iOS） |
| 键盘快捷键、命令面板 | 有 | #2330 | 见「快捷键和命令面板」；手机端没有实体键盘 |
| 翻译 | 有 | #433 | 存在当前浏览器、不同步到 hub：hub 的翻译偏好挂在手机的推送设备 id 上，网页没有；网页打开时按需翻 |
| 设备（token 列表、撤销） | 有 | — | |
| 扫码连新设备 | 有 | #2731 | 设置 › 设备 › 连接新设备：App 配对二维码和网页登录链接 / 二维码，一次性临时码 |
| 通知偏好 | 不适用 | — | 网页不注册推送；浏览器通知以后再说 |
| 存储空间 | 不适用 | — | 浏览器自己管缓存 |
| 互联：机器、会话标明所属 hub | 有 | #297 | 见「互联（多台 hub）」；网页不做多组 |

### 手机专属（不适用）

手机数据工具（`phone_*`，「这台 iPhone」连接器）、实时活动 / 灵动岛、Apple Watch、推送通知和通知上的操作、手机 / 手表录音和转写、
Siri / 快捷指令 / Spotlight、拍照、相册保存、iPad / iPhone Duo 的布局、「Mac 上的 Persona 已登录」：都依赖系统能力或硬件，浏览器里没有对应的东西。
体验模式（没有 hub 也能看）网页不做：网页由 hub 托管，打得开就有 hub。
