# 网页版（`web/`）

浏览器里用的 Jarvis：总览、Agents（各台电脑上 herdr 里的 Claude Code / Codex）、Jarvis 助手、机器监控。视觉和交互照
[Beautiful UI](https://www.beautifului.dev/)（MIT，许可见 `THIRD_PARTY_NOTICES.md`）：设计 token（`--ink` / `--surface` / `--line`…，浅色 / 深色两套）、
关键帧、组件语法（Thinking 的展开轨迹、Tool Chips、Approval Card、Prompt Bar、Sidebar Nav 的滑动高亮、Search、Task Rows、Insight Cards）。
组件是照着官网的 React 版重写的，接真实数据，不是官网的演示组件原样搬过来（那些是自己跑演示动画、不收数据的）。

## 怎么跑

- **由 hub 自己托管**：hub 把 `<hub 数据目录>/web`（默认 `~/.jarvis/web`）当静态站，和 API 同一个端口（hub 的 `listen`，默认 `:18660`，即所有网卡）。
  同源，所以没有 CORS，WebSocket 和图片也用同一个 token。没有这个目录时非 `/api/` 路径照旧 404。
  不是文件的路径都回 `index.html`（前端路由：`/`、`/s/<机器>/<会话>`、`/j`、`/j/<会话>`、`/machines`、`/settings`）；
  `/assets/` 下是带内容哈希的文件，永久缓存，`index.html` 不缓存（`server/internal/hub/web.go`）。
- 部署：`./scripts/deploy.sh web`（只构建、传网页，不重启 hub）；部署 hub 时也会顺带部署网页。
- 本地开发：`cd web && npm ci && npm run dev`，Vite 把 `/api` 代理到 `JARVIS_HUB_URL`（`eval "$(python3 ../scripts/jarvis_config.py env)"`）。
  测试 `npm test`（vitest，`src/lib/*.test.ts`），类型检查 `npm run typecheck`。

## 登录

第一次打开要贴 hub 的 token（`jarvisd hub pair` 打印的那个）。网页先用它换一个自己的设备 token（`POST /api/v1/auth/tokens`，`kind: web`，
hub 存为 `other`，角色 full），之后只用自己的，配对 token 不留在浏览器里；手机「设置 › 设备」能单独撤销。也可以直接贴一个 `jd_` 设备 token。
**链接直接登录**（2026-10-31）：打开 `<hub 地址>/?token=<hub token>`（任意页面路径都行，也认 `#token=`，后者不会发到服务器）就直接进，不用贴。
页面一加载先把 token 从地址栏抹掉（`history.replaceState`，在 React 渲染之前），浏览历史、书签、复制出去的链接里都没有它；然后照常换成这个浏览器自己的
设备 token。这个浏览器已经登录过（链接再打开一次）就沿用已有的，不再多建一个设备 token。token 不对时停在登录页并提示。
hub 的访问日志只记路径，不记查询串。注意这个链接本身就是完整权限：别发到群里、别存在别人能看的地方，泄露了在 hub 机器上换 token。
token 存在 `localStorage`；`<img>` 带不了请求头，图片走 `?token=`（hub 本来就认，日志不记查询串）。
左上角菜单「断开这个浏览器」会撤销这个 token。

**安全**：页面本身不需要鉴权（只是静态文件），所有数据都在 `/api/` 后面要 token。hub 默认是明文 HTTP，token 在网络上可见：
只在内网 / VPN 里用，要放到公网前面必须加 HTTPS 反向代理。

## 页面

- **侧边栏**（宽屏常驻，窄屏是抽屉）：连接状态和在线电脑数、⌘K 搜索、新建 Agent、总览 / Jarvis / 待办 / 机器、Jarvis 的对话（主对话在最前）。
  Agents 标题右侧可切换「按项目 / 按状态 / 按机器 / 按待办」，默认按项目，选择和各组折叠状态保存在当前浏览器。
  - **按项目**保留原来的 herdr 顺序（机器 → workspace → tab），没有动静的空间收进「空闲的空间」；有 loop、后台工作、运行中子 Agent 的空间不会被收起。
  - **按状态**对齐 iOS `AgentListOrder`：需要你 → 完成未看 → 工作中 → 待唤醒 → 已屏蔽 → 已看，只显示非空组，已看默认折叠。
    屏蔽来自 hub `/mutes`，需要你不受屏蔽降级，普通已看仍归已看；项目内单独开启通知的 Agent 不算屏蔽。
    组内按时间降序、同时间按会话 ID 排序；工作中按 `state_since`（缺失回退 `updated_at`），不随每条输出换位。
  - **按机器**沿用 hub 的机器数组顺序，组内按状态优先级 → 时间 → 会话 ID 排序；普通空闲行默认收起，待唤醒仍露在外面。
    状态 / 机器分组的行补项目上下文，状态分组在多机时还补机器名称。
  - **按待办**：每个有挂着 Agent 的未完成待办一段，段头是待办标题和状态，点段头进待办；没挂待办的 Agent 放最后「没有待办」一段。置顶、屏蔽、「最近（未运行）」的规则和「按状态」一致。其他分组下挂了待办的行显示待办图标标签。
  - **最近（未运行）**在三种分组下都保留：会话 offline 或机器离线 / 已不在机器列表时只进这里，不重复进入 live 分组；助手不混入 Agents。
    展开先显示 8 条，可继续显示到最近 40 条。折叠内容退出键盘焦点和无障碍树。
- **待办**（`/todos`，`src/views/Todos.tsx`）：左侧列表右侧详情。列表按未完成（等你 > 待查看 > 进行中 > 待跟进 > 未开始）和创建时间排序，已完成收在底部；每行显示状态、关联 Agent 数和最要紧 Agent 的最后一句。详情可就地修改标题和说明，列出当前关联的 Agents（点击进对话、启动失败可移除）与参与过的历史；可「交给 Agent」（新开一个或交给已有的，带待办上下文卡片和 `todo_id`）、「完成 / 重新打开」或删除。对话页 ⋯ 菜单和会话信息展示「待办：X」；hub 不支持待办接口（404）时隐藏入口。
- **新建 Agent**：首条消息支持选择、粘贴、拖入图片或文件，附件先在本地预览并可移除；图片随消息发送，其他文件后台上传到所选电脑后把路径带入消息。
  点击「启动」即关闭弹窗并打开本地会话，首条文字和附件立即可见；用 `async:true` 创建真实会话后再发 `/messages`，不等待远程启动完成才跳转。
  侧边栏「正在启动」保留后台任务，切去其他页面也继续，完成后不会强制跳走。失败保留内容，可重试；已经有真实会话时只重试上传或发送，消息 ID 不变。
  创建或送达结果不确定时先提示查看列表 / 会话，不自动重复创建或发送。首条消息仍可为空；Codex 文字启动兼容保留，但其创建协议不支持首条附件，界面明确提示。
  文字草稿保存在 localStorage，附件和后台任务只保留在当前页面内存，刷新网页不会恢复未完成任务。
- **总览**：需要你处理的交互、停在电脑上的、正在工作的（计时）、做完还没看的，每个一行（Task Rows），下面是各台电脑。
- **对话页**：和 App 的展示逻辑一致（`docs/ios.md`「展示逻辑」，代码 `src/lib/turns.ts`）：每条你的消息一段，段里的工具 / 思考 / 通知 / 中间的话
  折成一行工作行，点开是带左侧细线的轨迹（每个工具一行，点开看命令、diff、输出、图片），最后一段话是回复。
  进行中的那段是像素格 loader + 闪光文字 + 计时，最新一步露在下面。请求带 `fold_work`，展开工作行时按 `from_seq` / `through_seq` 取回折叠的部分。
  滚到顶自动加载更早的两轮，位置不跳；在底部时跟着新内容走，往上翻了就不动，出现「回到最新」。
  - 交互卡片（提问 / 计划 / 授权 / 终端对话框）在输入框上方，Approval Card 的样子；× 是「交回电脑处理」（`dismiss`）。
  - 输入框（Prompt Bar）：Enter 发送、Shift+Enter 换行、输入法组字时 Enter 不发送；`/` 命令（同 App 的快捷消息，`/clear` 先确认）；
    粘贴 / 拖入 / 选择图片作为附件，别的文件传到会话所在的电脑并把路径插到光标处（`docs/protocol.md`「发送文件」）；
    Claude 输入框里的灰色建议显示成占位文字，Tab 填入；Agent 工作中且没打字时发送键换成红色中断，Esc 也是中断。草稿按会话存在 localStorage。
  - 发送带 `client_msg_id`；被拒（`input_draft`）时弹窗显示电脑输入框里的字，能清空的给「清空并发送」（同一个 id 重发，带 `clear_draft`）。
  - 同空间的其他 Agent 在右上角「同空间 N」菜单里，每行显示状态圆点和文字标签（需要你 / 工作中 / 完成未看 / 已看），按 tab 顺序固定；状态随推送更新，不依赖颜色辨认。
    当前 Agent 保留勾选标记；长标题单行截断、悬停可看完整标题和状态，窄屏菜单宽度不超出视口。有别的需要你时按钮带红点。「⋯」：拷贝会话 ID / 目录 / 恢复命令，关闭 Agent（先确认）。
  - 打开页面、页面回到前台时发 `seen`。
- **Jarvis**：`/j` 是新对话（建议 + Prompt Bar，`POST /api/v1/assistant/conversations`），`/j/<id>` 是一段对话，无头助手的 `delta` 逐字流式显示。
- **个人 Agent**（`/a`、`/a/<整个 id 编码>`，`src/lib/agents.ts`、`src/views/agents/`）：lark-connector 配对的个人 Agent（`/api/v1/agents/*`，
  `docs/protocol.md`）。侧边栏「个人 Agent」按 provider 分组、组内按最近排序，带未读数，会话多于 6 个时可筛选；`/a` 是带搜索的全量列表。
  会话页：打开即 `POST …/read`；「加载更早的消息」用 `nextCursor`，按 `externalId` 去重、按 `position` 排序；tool-call 折叠，未知 block 原样显示 JSON。
  发送带新的 UUID `clientId`，失败可重试（同一个 `clientId`，hub 幂等）；持久化后的消息没有 `clientId`，本地气泡按文本 + 时间收敛；`canSend=false` 时输入框禁用。
  WS `{"type":"agents","session_ids":[...]}` 时刷新列表，打开的会话在 id 命中（或列表为空）时重拉最新一页。

  ![网页版侧边栏的个人 Agent：按来源分组，带头像、未读数、搜索和筛选按钮](screenshots/web-personal-agents.png)

- **设置**（`/settings`，侧边栏和左上角菜单进入，`src/views/Settings.tsx`）：四个分类，和 App 的设置同一套接口。
  「通用」是外观（跟随系统 / 浅色 / 深色）、Jarvis 助手的运行位置和统一模型配置入口、这个浏览器（hub 地址、断开）；
  「模型供应商」是全部接入（搜索；Jarvis 管理 / 来自 ~/.zshrc / 已隐藏），点开是弹窗：按电脑分组的真实参数（`docs/launchers.md`）、
  测试可用性、函数原文（折叠）、编辑、删除 / 隐藏；新增支持启动器或直接 API（Messages / Responses）；
  「模型配置」按 hub 的真实用途目录分组/搜索，编辑供应商、具体模型和支持的思考强度，显示继承与当前生效值。保存失败保留草稿，revision 冲突需重新加载（`docs/model-config.md`）；
  「设备」列出各设备的 token（这个浏览器排第一），撤销同样就地确认。分类记在 `?tab=`；旧 `?tab=models` 仍打开供应商，新分类为 `?tab=model-config`。
- **机器**：每台电脑一张卡片，CPU / 内存 / 磁盘当前值和最近一小时 CPU 曲线（`/api/v1/metrics`，10 秒刷新；空档断线），离线的能重连。

## 连接

一个 WebSocket（`/api/v1/ws?token=`）：`hello` 后整体刷新一次 REST（会话、交互、助手、机器），之后靠推送（`sessions` / `session` /
`interaction*` / `items` / `delta`）。断线指数退避重连（0.6 秒起，最多 15 秒），超过 70 秒没收到任何消息（hub 会 ping）就当连接已死重连，
页面回到前台、网络恢复时也重连；连不上时顶部出现「连不上 hub，正在重试…」。重连后已打开的对话重新订阅，带 `since_seq`（本地连续拿到的最后一行），
`from_seq` 接不上时 1.5 秒后按 `after_seq` 补洞（`docs/protocol.md`「items 的连续性」），`history` 变了就整页重取。

## 细节约定

- 颜色只走 token（`src/styles.css` 的 `:root` / `.dark`），不写死色值；状态色：需要你红（呼吸）、工作中橙（呼吸）、完成未看绿、已看灰。
- 主题：跟随系统 / 浅色 / 深色（左上角菜单），`index.html` 里在首帧前就决定，深色不闪白。
- 按钮文字一律单行（`whitespace-nowrap`），放不下的用图标 + `title`。
- 链接规则和 App 一样（`Links`）：裸链接在空白或非 ASCII 字符处结束，交给 Markdown 前写成 `<url>`（`src/ui/Markdown.tsx` 的 `linkify`），中文里的链接不会吞掉后半句。
- 尊重 `prefers-reduced-motion`：所有动画缩成 1ms。
- `useStore(selector)` 的结果按浅比较复用：选择器可以返回新数组，没变就不重渲染（否则 `useSyncExternalStore` 会无限循环）。
