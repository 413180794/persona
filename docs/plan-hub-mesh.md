# 计划：hub 互联、电脑接入、多 hub 客户端、管理界面、网页对齐

> **第二版已取代本文第 2 节的对等互联部分（归属、路由通告、透明转发）和第 3 节的 ssh 部分**：一台中心 hub、预装服务接入、一台电脑多条路，见 [plan-hub-center.md](plan-hub-center.md)。

总览 issue #293。2026-11-02 用户提出调整服务连接的架构，本文定下整体方案和**各路之间的接口约定**，各路按约定并行开发，
细节（协议的每个字段、重试参数、界面）由各路在自己的文档里补全。基线是集成分支 `feat/jolly-cerf-dfb019`，各路 PR 合回它。

## 目标

1. **hub 互联**：hub 之间互相探测，连上后双向通信；客户端连互联组里**任意一台** hub，都能看到组里所有 hub 接的所有电脑和终端，
   同一台电脑只出现一次，经最优线路访问。
2. **node**：hub 主动 ssh 连（现状不变）。
3. **links**：没有暴露、不方便被连的电脑主动连 hub，hub 复用这条链路（现状见 [link.md](link.md)）；Mac 状态栏、Windows 托盘、Linux headless。
4. **客户端同时连多组互不相连的 hub**：工作 / 生活这类要互相隔离的环境，列表合并显示、标明来自哪一组。
5. **hub 管理界面**：hub 自带网页管理界面，默认只允许 hub 所在局域网访问，部署时提醒用户选访问范围。
6. **网页操作页**：功能和手机端基本一致。

## 拓扑和术语

```text
 手机 / 网页 ──► hub A ◄══ 互联链路 ══► hub B ◄── 手机（另一组时互不相连）
                  │  ▲                    │
          ssh 起 node │ └─ link（电脑主动连）  └─ ssh / link ……
                  ▼
                电脑
```

| 词 | 意思 |
| --- | --- |
| **互联组** | 一组经互联链路（直接或间接）连在一起的 hub。组是连通关系算出来的，没有单独的「组」配置 |
| **直连** | hub 自己连着这台电脑的 node（`local` / `ssh` / `tailcat` / `link`） |
| **归属 hub**（owner） | 一台电脑在组里同一时刻只由一台 hub 真正连着，其余 hub 经它访问（见「归属」） |
| **组**（客户端） | 客户端配对过的 hub 按互联关系分的组；同组只保持一条连接，不同组各一条 |

## 1. 机器身份 `machine_uid`

去重要知道「两台 hub 连的是同一台电脑」。机器 id（`hub.json` 的 `machines[].id`）是各 hub 自己起的名字，不能用。

- node 第一次运行时生成 `~/.jarvis/machine-uid`（UUIDv4，0600），之后一直用它；`node.info` 带 `machine_uid`。
- 旧 node 没有这个字段：这台电脑不参与去重，按「hub_id + 机器 id」当成独立的一台。
- 克隆出来的虚拟机会带着同一个文件：`jarvisd node reset-uid` 重新生成，文档里写明。

## 2. hub 互联（服务端）

### 2.1 探测

- **局域网自动发现**：每台 hub 用 mDNS 广播 `_jarvis-hub._tcp`，TXT 里只放 `hub_id`、名字、TLS 端口、公钥指纹 `fp`、版本，不放任何凭据。
  Docker 用桥接网络时组播出不去，只能手动添加（文档写明；`network_mode: host` 时可以）。
- **手动添加**：填对方地址，先请求对方的 `GET /api/v1/health` 拿 `hub_id`、`tls_port`、`fp` 给用户核对。
- 发现只是列出来，**不自动互联**：互联必须在管理界面里确认并输入对方的互联码。

### 2.2 配对和信任

- 在 hub B 上生成**互联码**（管理界面「互联 › 生成互联码」，或 `jarvisd hub pair --peer`）：一次性、10 分钟有效，链接里带 B 的地址候选、`hub_id`、`fp`。
- 在 hub A 的管理界面粘贴互联码：A 按 `fp` 钉扎连 B，用码换取 B 发给 A 的互联凭据，同时把 A 的 `fp` 和 A 发给 B 的互联凭据交给 B。
  结果是**双向钉扎、双向凭据**，两边都能主动拨号。
- 互联凭据是设备凭据的一种：kind `peer`、role `peer`，绑定对端 `hub_id`，只能开 `/api/v1/peer*`，别的接口一律 403；
  hub 只存 hash。任一侧在管理界面「断开」就撤销自己发出的凭据并通知对方，链路立刻断开。
- 互联关系和凭据存 Postgres（迁移 **0151**），不写 `hub.json`。

### 2.3 互联链路

- WSS `/api/v1/peer`，只走 TLS 端口（`public.tls_listen`），按 `fp` 钉扎，内网也不例外；没开 TLS 的 hub 不能互联（管理界面提示去开）。
- 每对 hub 只保留一条链路：两边都可以拨；同时连上两条时，保留 `hub_id` 较小的一方拨出的那条。
- 链路上跑两类东西：
  - **控制消息**（JSON）：hello、路由通告、事件转发、心跳；
  - **多路复用的流**：转发 HTTP 请求 / WebSocket（复用 `internal/streams` 或 link 的做法），客户端对远端机器的所有操作都走它。
- 心跳、重连退避照 link 的做法（[link.md](link.md)「心跳」「重连」）。

### 2.4 路由和去重

- 每台 hub 通告自己**直连**的电脑：`machine_uid`、机器 id、名字、是否在线、hub→node 的 RTT；也转发从别的 hub 学到的，带经过的 hub 路径（防环）。
  不要求两两互联：A–B、B–C 互联时，A 也能经 B 看到 C 的电脑。
- 去重按 `machine_uid`；选线路：跳数少的优先 → RTT 小的优先 → `hub_id` 字典序（保证稳定）。

### 2.5 归属（同一台电脑被两台 hub 直连时）

常驻 node 同时只接受一个 attach，新的顶掉旧的（[link.md](link.md)「转发」）。两台 hub 都配置了同一台电脑时，如果都去连，会互相顶、来回掉线。所以：

- 同一个 `machine_uid` 在组里只有一台**归属 hub** 真正连着 node，其余 hub 不连，经归属 hub 访问。
- 选归属：配置了这台电脑的 hub 里，在线、hub→node RTT 最小的那台；**有粘性**：当前归属健康就不换，掉线超过一段时间（配置值）才由别的 hub 接管。不能来回抖。
- 第一次连上才知道 `machine_uid`：连上后发现组里已有别的 hub 是这台的归属，就主动断开、转为备用。

### 2.6 对客户端的样子（**各客户端按这个开发**）

hub 把组里别的 hub 的电脑当成自己的机器列出来，客户端对它的所有请求由 hub 经互联链路转给归属 hub。客户端**不用知道互联的细节**，
现有的接口和推送照常用。

- **机器 id 在组里唯一且稳定**：一台电脑的 id 取「配置了它的 hub 里 `hub_id` 最小的那台」给它起的配置 id；不同电脑撞名时，后者加后缀 `@<hub 名 slug>`。
  这个规则只看配置，不看谁在线，所以不会随着线路切换变来变去。代价：同一台电脑在两台 hub 上起了不同的名字时，有一台的客户端会看到名字变了。
- `GET /api/v1/machines`（和 WS 的 `machines` 推送）每台多四个字段（不和现有的键撞名）：

  ```json
  {"id": "macbook", "hub_id": "<归属 hub 的 hub_id>", "hub_name": "家里", "machine_uid": "…",
   "route": {"via": "local|ssh|tailcat|link|peer", "hops": 0, "rtt_ms": 12}}
  ```

  `hops` 是从客户端连的这台 hub 到归属 hub 要经过几条互联链路（0 = 本 hub 直连）；`rtt_ms` 没测到时省略。
- 会话、交互、items、终端、文件、端口转发、上传：形状不变，机器 id 用上面的规则；对远端机器的请求由 hub 透明转发。
- **hub 名字**：`hub.json` 顶层新字段 `name`（管理界面里能改）；没填时用 hub 机器短主机名（去掉 `.local` / 域名后缀）；
  短主机名是 Docker 容器 id 的样子（12 位十六进制）或 `JARVIS_LOCAL_NODE=0` 时用 `hub-<hub_id 前 6 位>`。
  代码里一律调 `func (h *Hub) hubName() string`（`server/internal/hub/hub_info.go`，L6 先加），互联组的 hub 数调 `meshHubCount()`（L6 先写成返回 1，L1 接线）。
- hello 和 `GET /api/v1/health` 多 `hub_name`；**鉴权后的** hello 多 `mesh`（不鉴权的 health 不给）：

  ```json
  {"mesh": {"hubs": [{"hub_id": "…", "name": "家里", "online": true, "self": true,
                      "endpoints": ["https://…"], "tls_port": 18662, "fp": "…"}]}}
  ```

  列出组里所有 hub（含自己，`self: true`），客户端用它分组和选线路。没有互联时只有自己一项。
- **凭据在组里通用**：客户端的设备凭据在组里任一台 hub 上都能用。hub 遇到不认识的 `jd_` 凭据时经互联链路问别的 hub（结果和现在一样缓存 30 秒），
  撤销以签发它的 hub 为准。这样客户端能在同组 hub 之间换线路，不用每台都配对。
- **推送**：设备的推送注册一律落在签发它凭据的 hub（在别的 hub 上调 `POST /api/v1/devices` 时转给签发方）；组里任何 hub 上的交互、完成事件
  经互联链路转到那台 hub 再推。一个设备只收一份推送。推送内容（直推的 APNs payload 和经中继密封的那份）顶层带 `hub_id`：发这条推送的 hub
  （即签发方）的 hub_id，客户端据此认出是哪一组；里面的机器 id 是这台 hub 视角下的组内 id。
- **不跨 hub 合并的**：Jarvis 助手主对话、知识库、收件箱、邮箱、形象、模型配置、连接器都是每台 hub 自己的，客户端连哪台就显示哪台的；
  客户端里这类「hub 级」页面按组 / hub 选。

### 2.7 管理接口（管理界面的「互联」页按这个开发）

挂在管理界面的鉴权后面（第 5 节），前缀 `/api/v1/admin/peers`：

| 接口 | 作用 |
| --- | --- |
| `GET /api/v1/admin/peers` | `{"peers":[{hub_id,name,state,address,rtt_ms,since,error,machines}],"discovered":[{hub_id,name,addresses,fp,version}]}`；`state`: `connected` / `connecting` / `offline` / `refused`（对方不认我们的凭据，界面按离线 + 错误显示）；`machines` 是那台 hub 直连的电脑数；没有的字段省略 |
| `POST /api/v1/admin/peers/probe` | 手动添加前核对：`{"address":"https://<地址>:<端口>"}`（或 `<host>:<port>`）→ `{hub_id,name,tls_port,fp,version}`；只请求对方的 `/api/v1/health`，不跟随重定向、有超时、限响应大小；对方没开 TLS 时 `tls_port` 为 0、`fp` 为空 |
| `POST /api/v1/admin/peers/code` | 生成本 hub 的互联码：`{"code":"jarvis://peer?…","expires_at":"…"}` |
| `POST /api/v1/admin/peers` | `{"code":"…"}` 用对方的互联码互联；成功返回那一项 peer |
| `DELETE /api/v1/admin/peers/{hub_id}` | 断开并撤销 |
| `GET /api/v1/admin/routes` | 调试用：组里所有电脑、各自的归属 hub、候选线路 |

接线：互联这一路把处理函数写成 `func (h *Hub) registerPeerAdmin(mux *http.ServeMux)`；管理界面这一路的管理路由调用它。
两路谁后合并谁负责接上；先合并的互联在管理路由还没有时，临时挂在只收回环来源 + 主 token 的路由上。

## 3. 电脑接入

- **ssh 的 node**：不变。
- **link**：现状见 [link.md](link.md)。这一轮：
  - **Linux headless**：`jarvis-link` 不依赖图形界面，一条命令装好（`jarvis-link install`：写 systemd 用户服务，提示 `loginctl enable-linger`；
    没有 systemd 时给出替代做法），`status` / `logs` / `uninstall`，`pair` 支持从标准输入读链接；服务器、NAS、树莓派上能跑。
  - **Mac 状态栏**：菜单里显示连着哪台 hub（`hub_name`）、所在互联组有几台 hub；其余不变。
  - **Windows 托盘**：移交给 Windows 环境的会话做（#299）。jarvisd 现在在 Windows 上编译不过，herdr 也没有 Windows 版，那边先定方案。
- link 只连它配对的那台 hub；那台 hub 挂了时转到组里别的 hub，**不在这一轮**。

## 4. 客户端多 hub（iOS #256、Android）

- 可以配对多台 hub。配对信息、设备凭据、推送密钥都按 hub 分开存；旧的单份配置自动迁移成第一台。
- **分组**：两台已配对的 hub，只要一方 hello 的 `mesh.hubs` 里有另一方，就是同一组。组 id 由客户端本地生成、保持稳定（组合并 / 拆开时重新算成员，不改已有的 id）。
  配对一台已经在某组里的 hub，自动并进这一组，不重复显示。
- **连接**：每组只保持一条连接，选最优线路：组里各 hub 的候选地址一起参与现有的选路规则，连得上、延迟低的优先；连着的那台掉了换组里别的 hub（凭据组内通用）。
  不同组各一条连接，互不影响。
- **数据**：会话键要能区分组（iOS 给 `SessionKey` 加组 id，`ChatCache` 目录版本号加一；Android 给非主组的机器 id 加稳定的 `~<组 id>` 后缀，由门面在发给 hub 前去掉）。**主组**（旧的单份配对迁移来的组，或第一次配对的组）的键和现在逐字节一样；主组断开后别的组保持原样，不提拔新主组。
- **选中组**只决定 hub 级页面用哪一组，不影响任何会话键、机器 id：切换选中组时，打开着的页面、通知深链、草稿 / 置顶 / 缓存都不能错位。
- **界面**：Agents、机器、交互等列表把各组合在一起显示，每条标明来自哪一组，可以按组筛选；**只有一组时界面一像素不变**。
  hub 级页面（Jarvis 助手、收件箱、hub 相关的设置）按组切换。设置里新增「hub」列表：添加、改名、断开某一组 / 某一台。
  设置里进 hub 列表的那一行只有一组时也显示（要从这里添加第二台），这是只有一组时**唯一**允许的像素变化。
- **iOS 还要**：推送、通知操作、实时活动、手表、Siri / 快捷指令都带上组 / hub 标识，路由到对应的 hub（#256 的方案）。
- 体验模式照常：它是一个假的组。

## 5. hub 管理界面

- 地址 `<hub 地址>/admin`，接口前缀 `/api/v1/admin/`，和 App 的接口**分开鉴权**；页面和网页操作页同一个 `web/` 工程（代码放 `web/src/admin/`）。
- **访问范围** `hub.json` 的 `admin.access`：

  | 值 | 谁能访问 |
  | --- | --- |
  | `loopback` | 只有 hub 机器本机 |
  | `lan`（**默认**） | 本机 + 和 hub 同一局域网的私网地址（+ `security.trusted_cidrs`） |
  | `cidrs` | 本机 + `admin.cidrs` 里列的网段 |
  | `any` | 任何来源，但必须走 TLS |

  范围外的请求（页面和接口）一律 404，不暴露管理界面的存在。放宽到 `any` 只能在 hub 机器上用命令行或部署脚本改，网页里只能收窄。
- **首次设置**：范围内第一次打开时设管理员密码，之后用密码登录（HttpOnly、Secure 的会话 cookie，有效期是配置值）；登录失败按 `security` 现有规矩限流。
  忘了密码在 hub 机器上 `jarvisd hub admin reset-password`。管理员凭据存 Postgres（迁移 **0161**）。
- **来源不可信的两种情况**：容器里，本容器的网关地址（经 docker-proxy 进来的连接，可能来自公网）不算局域网，除非写进 `admin.cidrs` / `security.trusted_cidrs`；
  来源是回环时（同机反向代理不加转发头时，公网请求也来自回环），首次设置要输一次性设置码（启动日志里有，`jarvisd hub admin setup-code` 现取）。
- **部署时提醒选择**：`scripts/deploy.sh`、公开版 `install.sh`、Docker（`.env` 的 `JARVIS_ADMIN_ACCESS`，没设时入口脚本打印提醒并用默认值）都要让用户选访问范围；
  有终端时交互式询问，没有终端时用参数 / 环境变量，没给就用 `lan` 并打印一行提醒。
- **页面**：
  - 概览：版本、运行时长、监听 / TLS / 候选地址、doctor 检查结果；
  - 电脑：列表和状态、添加 ssh 电脑、给 link 电脑生成配对链接、重连、删除；
  - 互联：发现到的 hub、生成 / 输入互联码、链路状态和 RTT、断开（接口见 2.7）；
  - 设备：凭据列表、撤销、生成手机配对二维码；
  - 网络与访问：公网 / TLS / 候选地址、管理界面的访问范围（只能收窄）。
- 改 `hub.json`：原子写、保留不认识的字段、权限 0600；能即时生效的即时生效，要重启的明确提示。
- 和别的 issue 的关系：管理界面是 #287（简化配置）、#284（hub 版本更新）以后的落脚点之一，本轮不做一键安装和更新。

## 6. 网页操作页

- 现有 `web/`（[web.md](web.md)）已有 Agents、Jarvis 助手、个人 Agent、机器、设置。先列出和手机端（以 iOS 为准，Android 参考）的差距表，写进 web.md，再按表补齐。
- 网页由一台 hub 托管，连的就是这台 hub，互联后天然看到整个组；机器、会话标明所属 hub（`hub_name`，只有一台 hub 时不显示）。网页里不做多组。
- 不重复别人在 master 上开着的 PR：#241（设置里扫码连新设备）、#254（终端引擎切换和 Orca 启动器）。

## 7. 分工

| 路 | 内容 | issue | worktree（`~/code/self/jarvis-mesh/`） | 独占资源 |
| --- | --- | --- | --- | --- |
| L1 | hub 互联（第 1、2 节） | #294 | `l1-mesh` | 迁移 0151、测试库 `jarvis_test_mesh` |
| L2 | iOS 多 hub（第 4 节） | #256 | `l2-ios` | 自己的模拟器、mock hub 端口 18760–18762；`scripts/mock/mock_hub.py` 的互联字段由它加 |
| L3 | Android 多 hub（第 4 节） | #295 | `l3-android` | 自己的 AVD、mock hub 端口 18770–18772；不改 `mock_hub.py`（要什么找 L2 / 主持人） |
| L4 | 管理界面 + 部署时选范围（第 5 节） | #296 | `l4-admin` | 迁移 0161、测试库 `jarvis_test_admin`、`web/src/admin/`、dev server 端口 5274 |
| L5 | 网页操作页对齐（第 6 节） | #297 | `l5-web` | dev server 端口 5275、mock hub 端口 18780 |
| L6 | links：Linux headless、Mac 状态栏（第 3 节） | #298 | `l6-link` | — |
| — | links：Windows 托盘 | #299 | 移交 Windows 环境的会话 | — |

- 各路只依赖本文的约定，不依赖别的路的实现；需要对方还没做的东西时，按约定写桩或 mock。
- 共享文件（`server/internal/hub/http.go`、`config.go`、`web/` 的路由入口、`CLAUDE.md`、`docs/` 索引）只做最小改动，新代码放新文件；冲突在合并时只改冲突块。
- 改约定要先改本文（单独提交），再通知别的路。

## 8. 不在这一轮

- link 在归属 hub 挂了时转到组里别的 hub；
- 跨 hub 合并助手主对话、收件箱、知识库；
- Windows 托盘的实现（移交）；
- 一键安装（#287）、hub 版本更新（#284）。
