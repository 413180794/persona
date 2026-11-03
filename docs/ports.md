# 端口转发：在手机上看 Agent 起的网页

Agent 在某台电脑上起了 dev server（vite、next dev、`python -m http.server`……），手机上点一下就能在 App 里看到页面效果，
热更新照常工作。参考的是 Moshi（iOS 上的 SSH / mosh 终端）的 Browser preview，只借鉴交互。

## 一图看懂

```
手机 App                              hub                                node（那台电脑）
WKWebView ─ http://localhost:5173 ─▶ 本机 127.0.0.1:5173（App 监听）
                                      │ 每条 TCP 连接一个 WebSocket
                                      ▼
                         GET /api/v1/machines/{mid}/ports/5173/connect（设备 token）
                                      │ 已有的 hub↔node 连接上开一条流（stream.*）
                                      ▼
                                                       连本机 127.0.0.1:5173（或扫描时它绑的地址）
```

## 发现端口（node，`server/internal/node/ports.go`）

- **属于谁**：每个活着的 Agent 的 pane，取宿主给的进程（herdr 的前台进程，第一个是 claude / codex 本身），
  往下找全部子孙进程，再加上同一个终端（tty）上的进程（`npm run dev &` 之后外层 shell 退出、被 init 收养的也算）。
  claude / codex 自己的监听（IDE 桥、MCP）不算。
- **怎么扫**：Linux 直接读 `/proc`（`/proc/*/stat` 得进程树，`/proc/net/tcp{,6}` 里 LISTEN 的 socket 按 inode 对上进程的 fd），
  不起子进程；macOS 用 `ps -Ao pid=,ppid=,tty=,comm=` 加 `lsof -a -p <这些进程> -nP -iTCP -sTCP:LISTEN -Fpcn`。
  `lsof` 在某个 pid 已退出 / 是僵尸时退出 1、结果照常给，按结果算；扫描真出错时同一个错误只记一行日志。
  macOS 上一次约 0.17 s CPU（约 3,700 个进程的机器，`ps` 占大头）。
- **哪些算**：对每个 `pid:端口` 发一次 `GET /`（`Host: localhost:端口`，dev server 的 host 检查认这个），回 HTTP 的才列出来，
  顺便取首页的 `<title>`（最多 60 字）。接受连接但 3 秒没回话的（next dev 第一次编译）先列出、不带标题，下次扫描再问，最多 5 次。
  回的不是 HTTP（ssh、数据库）的不列。结果按 `pid:端口` 缓存，进程或端口没了就丢。
- **多久扫一次**：有活着的 Agent 时每 15 秒一次；Agent 的 transcript 变了（多半是跑了工具，起 / 停服务器就是一次工具调用）
  或者转发时发现端口不在了，4 秒内补扫一次。没有 Agent 不扫。
- **怎么推**：结果放在 `Session.ports`，跟会话列表一起走；端口集合（端口、pid、标题）没变就不推（`listKey` 带上了它），
  不会因为扫描多出推送（#178 的教训：没变的东西不推、不让界面重算）。

## 识别回答里的链接（App，`apple/Jarvis/Code/PortLinks.swift`）

- `http(s)://localhost|127.0.0.1|0.0.0.0|[::1]|*.localhost|这台机器的主机名（带不带 .local）:端口/路径` 都算「这台机器的端口 + 路径」，
  也认服务器常打印的不带 scheme 的 `localhost:3000/x`。没写端口的、局域网地址、别的主机都不算（局域网地址照常当网页打开）。
- 「这台机器」是会话所在的机器（助手对话就是 hub 机器）。
- 出现在：Agent 的回复（消息下面一排 Badge）、Bash 工具的输出（展开后输出下面）、会话顶部（`Session.ports`），
  以及点消息里 / 终端里的这类链接本身（`ChatActions.openLink` 先认端口链接）。这类链接不再预加载（预加载打开的是手机自己的 localhost）。

## 转发

### 为什么是「手机本地监听」

| 方案 | 问题 | 结论 |
| --- | --- | --- |
| 每个端口一个子域名（`p5173-mac.<hub 域名>`） | 要通配 DNS 和通配证书，得改 DNS | 不选（要用户拍板的改动） |
| hub 给每个转发开一个独立端口 | 新开公网端口，每个都要放行防火墙 | 不选 |
| 路径前缀 + 改写（`/api/v1/…/ports/5173/…`） | SPA 的绝对路径（`/@vite/client`、`/_next/…`）、HMR 的 ws 地址、Cookie 的 Path、重定向的 Location 都要改写；改 HTML / JS 不可靠 | 不选 |
| **App 在手机 127.0.0.1 / ::1 上监听，每条连接走一个到 hub 的 WebSocket** | 只有 App（iOS、Mac）能用，网页版不行（浏览器不能开监听） | **选** |

和 Moshi 一样：页面在手机上就是 `http://localhost:5173/`，和在电脑上一模一样，字节原样转发，HTTP / WebSocket（HMR）/
重定向 / Cookie 都不用改写；不改 DNS、不开新端口，走的是 App 已经在用的 hub 地址和设备 token。

### 手机（`apple/Jarvis/Code/PortForwarder.swift`）

- 每个（机器，端口）一个转发：先在 `127.0.0.1` 绑同一个端口号，被占（手机上别的转发、模拟器所在的 Mac 上已有程序）就随机一个；
  再在 `::1` 绑同一个号（`localhost` 先解析成 ::1，没有它 WebKit 也会退回 127.0.0.1）。只绑回环，局域网里谁也连不到。
- 每条进来的连接开一个 `wss://<hub>/api/v1/machines/{mid}/ports/{port}/connect`（`Authorization: Bearer <设备 token>`），
  两边各自「发完上一块再读下一块」，所以慢的一方会拖住快的一方，不会在内存里堆。
- 监听一直留着；App 被挂起后监听坏了，下次打开时重建（尽量用原来的号，页面的 HMR 会自己重连）。
- 打开前先问 hub：端口在某个 Agent 的 `ports` 里就直接开；不在就 `POST …/open`（用户明确点开），没在听就提示「端口 :N 已经没有在监听」，
  Badge 变灰。
- 网页卡片就是普通链接的那个（`LinkSheet`），标题栏下面写「机器名 :端口」；菜单里没有「在 Safari 打开」「分享」
  （手机回环上的地址对别的 App 没意义）。dev server 的页面不走磁盘缓存（`reloadIgnoringLocalCacheData`），每次都是新的。
- Mac App 用同一套代码；沙盒要 `com.apple.security.network.server`（只在回环上监听）。

### hub（`server/internal/hub/ports.go`）和流（`server/internal/streams`）

- `…/connect` 先检查、后升级：端口要在这台机器某个会话的 `ports` 里，或用户 24 小时内 `POST …/open` 过；否则 403。
  然后登记一条流、请 node `port.dial`，连不上就 404（还没升级，App 拿得到状态码），连上了才升级成 WebSocket。
- 一条流就是 hub↔node 那条 JSON-RPC 连接上的 `stream.data / stream.ack / stream.close` 事件（`docs/protocol.md`）。每个方向最多 512 KB
  没确认，发的一方到了就等：网页的大文件不会占满这条连接、饿死别的 RPC，读得慢的一方也不会让另一端无限缓存。
  事件处理（`Mux.Event`）从不阻塞，因为它跑在 RPC 的读循环里。
- 一台机器同时最多 64 条（一个页面一般 6 条左右）；机器断开时它的流全部关掉。
- 慢请求日志不记 `…/connect`（它本来就长）。

## 安全边界

- 鉴权和别的 API 一样：设备 token（或配对 token）。只读设备（手表）一律 403，哪怕是 Agent 的端口。
- 只能连**配置里的机器自己**的端口：node 只拨扫描时那个监听绑的地址（本机的）或 127.0.0.1 / ::1，不接受主机名，不是一个能打开任意内网地址的代理。
- 端口只能是 Agent 名下的（node 扫出来的）或用户在 App 里点开过的；随便猜一个端口号连会 403。点开过的记在 hub 内存里 24 小时，
  hub 重启后清空（再点一次就行）。
- 探测（`GET …/ports/{port}`）只回「有没有在听、是不是 HTTP、标题」，不放行。

## 还没做的

- 网页版不能用（见上面的取舍）。
- https 的 dev server：字节照转，但 WKWebView 不认它的自签证书，打不开。
- 旧布局（`-ui.oldLayout`）的 iPhone 顶栏不显示会话的端口行，只有消息里的 Badge；新外壳和 iPad 侧栏都有。
