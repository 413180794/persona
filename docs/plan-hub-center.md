# 计划（第二版）：单中心 hub、预装服务接入、一台电脑多条路

总览 issue #293。2026-11-02 用户看过第一版（[plan-hub-mesh.md](plan-hub-mesh.md)，对等互联已合进集成分支，见 [mesh.md](mesh.md)）后纠正：

1. **一定有且只有一台中心 hub**。hub 之间仍互相发现、互联，但连上后选一台当中心，其余 hub 退化成 node，把自己的电脑交给中心；
   客户端连组里哪台 hub 都转到中心。
2. **hub 和电脑之间通过预装的服务连接，不用 SSH**（SSH 权限太大）。电脑可以主动连 hub，也可以被 hub 连。
3. **去重和选最优线路**指：同一台电脑有多条路能到时，只出现一次，用最好的那条，断了换另一条。

本文取代 plan-hub-mesh.md 第 2 节里的「路由通告、归属、对等透明转发」和第 3 节里的 ssh 部分；它的第 1 节（`machine_uid`）、
2.1–2.3（发现、互联码配对、互联链路）、第 4–6 节（多 hub 客户端、管理界面、网页）照旧。基线和合并方式不变：集成分支 `feat/jolly-cerf-dfb019`。

## 0. 推荐的部署：只装一台 hub，其他电脑装 node 服务

用户 2026-11-02 补充：**推荐用户只安装一台 hub，其他电脑都使用 node 服务**。

- **hub**：装在一台常开的机器上（NAS、家里的服务器），全家只要这一台。它所在的机器本身也是一台电脑（本机 node）。
- **node 服务**：其他每台电脑装一次，里面是常驻 node 加连接器，**一个安装命令或一个 App 装好**，配对后主动或被动连 hub（第 4 节）：
  - Linux：`install.sh` 选「node」，一条命令装好两样 systemd 用户服务并配对；
  - Mac：Jarvis Link 菜单栏 App 自带 jarvisd，首次运行就装好常驻 node（不用再单独跑 `scripts/node-launchd.sh`）；
  - Windows：#299。
- **多台 hub 互联**（中心 + 成员）只给少数需要的场景：比如两处各有一台常开、互相连不通的机器，或者已经装了好几台 hub 想合并。
  文档、安装器、管理界面都按「一台 hub + node 服务」引导：管理界面以「添加电脑」为主，「互联」放进「高级」；
  安装器在机器上已经能找到同网段的 hub 时，提示「这台装 node 服务就够了」。

## 1. 角色

```text
 手机 / 网页 ──► 中心 hub ◄══ 互联链路 ══ 成员 hub ◄── 手机（连成员也行：成员把请求转给中心）
                  │    ▲                    │    ▲
     hub 来连（被动）│    │电脑来连（主动）      │    │       成员连着的电脑，中心经成员的链路去连
                  ▼    │                    ▼    │
                 电脑上预装的服务：常驻 node + 连接器 jarvis-link
```

| 角色 | 做什么 |
| --- | --- |
| **中心 hub** | 组里唯一。服务所有客户端，所有状态都在它这里（会话索引、交互、推送、设备凭据、助手、收件箱、知识库、模型配置、定时任务……），连所有电脑（直接，或经成员 hub） |
| **成员 hub** | 只做两件事：把客户端请求原样转给中心；把它连着的电脑的 node 流转给中心。自己不再驱动 node、不再服务客户端，自己的助手 / 收件箱 / 定时任务停用（数据留着，退出组后恢复） |
| **电脑** | 预装常驻 node（`jarvisd node`，功能都在它里面）和连接器 jarvis-link（只管发现、上报、链路，原样转给 `~/.jarvis/node-attach.sock`，不承担任何功能，沿用用户 2026-10-31 的要求） |

只有一台 hub、没有互联时，它就是中心，一切和现在一样。

## 2. 选中心

- **互联码就是「邀请加入我的组」**：A 粘贴 B 的互联码，A（连同 A 所在组的全部 hub）并入 B 所在的组，中心是 B 组的中心。
- **手动换中心**：管理界面「设为中心」。原中心在线时双方交接；原中心离线时要用户确认，并提示原中心回来后会变成成员。
- **中心不可达时不自动换**（避免同时出现两个中心）：成员对客户端回 `503 center_unreachable`（带中心名字），管理界面提示可以手动「设为中心」。
- **任期**：每台 hub 记组成员、中心的 hub_id 和一个递增的任期号；手动换中心时任期号加一。任期号大的说了算：离线的旧中心回来，看到更高的任期就降为成员。
- 状态存 Postgres（新迁移，号码开工前看所有 worktree 和分支，取最大的再加 10）。

## 3. 成员转发客户端

- 成员收到的客户端请求（`/api/` 全部，含 WebSocket；网页静态文件除外）经互联链路原样转给中心，保留 `Authorization`。
  来源判断（公网 / 局域网、要不要 TLS、限流）在成员这一端做完，再把结论随请求告诉中心，中心信任成员的结论（成员是配过对的）。
- 管理界面 `/admin` 不转：每台 hub 的管理界面管它自己；成员的管理界面顶部写「这台是成员，组由 <中心> 管理」，组的事去中心的管理界面做。
- **设备凭据**：成员加入组时，把自己签发过的设备凭据（hash、名字、角色、设备 id）交给中心，中心接管、以后由中心签发和撤销；
  成员从此不再自己认凭据（全部转给中心）。已经配对在成员上的手机不用重新配对。
- **推送**：只有中心发；设备在中心注册；推送里的 `hub_id` 是中心的。

## 4. 电脑接入：预装服务，不用 SSH

- 每台电脑预装两样：常驻 node（launchd / systemd 服务，Windows 见 #299）和连接器 jarvis-link（Mac 菜单栏 App、Linux headless、Windows 托盘里带的就是它）。
- **同一个连接器，两种模式**（配对时选，之后可以改）：

  | 模式 | 谁连谁 | 适合 |
  | --- | --- | --- |
  | **主动**（现状，[link.md](link.md)） | 连接器拨 hub 的 WSS `/api/v1/link` | 笔记本、NAT 后面、换网络的电脑 |
  | **被动**（新） | hub 拨连接器在本机监听的 TLS 端口 | 固定地址、hub 能连到的电脑（服务器、example-machine） |

- **被动模式的协议**（两路都按这个做，细节由连接器那一路补进 link.md）：
  - 连接器监听 `listen`（默认 `:18663`），TLS 用连接器自己的 ECDSA P-256 身份密钥和自签证书（`~/.jarvis/link-tls/`，0700 / 0600），
    指纹和 hub 的算法一样（SPKI DER 的 SHA-256，base64url 不带填充）。
  - `GET /api/v1/connector/info`：`{"machine": "<hub 给它的机器 id>", "version": "…"}`。
  - `GET /api/v1/connector/stream`：WebSocket 升级后原样转给 `~/.jarvis/node-attach.sock`（permessage-deflate，两边都 ping），和主动模式的流一样。
  - `POST /api/v1/connector/pair`（配对时用，唯一不要长期凭据的接口）：`{"code":"<一次性码>","token":"<hub 生成的长期凭据>","machine":"<机器 id>","hub_id":"…","hub_name":"…"}`
    → 码对就存下 token 的 hash 和 hub 信息、作废这个码，回 `{"version":"…"}`；码错或过期 401（计入限流）。已经配对过的连接器再配对会替换旧的 hub。
  - info、stream 两个接口都要 `Authorization: Bearer <hub 的凭据>`；连接器只存凭据的 hash，只认配对它的那台 hub（组里的中心；换中心时由新中心重新配对）。
    认证失败按来源限流。除了这两个接口什么都不提供：没有 shell、文件、端口转发，只能连 node-attach.sock。
  - hub 按配对时记下的连接器指纹钉扎。
- **被动模式的配对方向反过来**（电脑不一定连得到 hub）：
  1. 电脑上 `jarvis-link pair --listen [:18663]`（菜单栏 / 托盘里选「让 hub 来连」）：生成一次性配对码，打印链接
     `jarvis://connector?addr=<候选地址>&fp=<连接器指纹>&code=<一次性码>`，10 分钟有效。
  2. 在 hub 管理界面「电脑 › 添加电脑 › hub 来连」粘贴链接（可以改地址、填代理）：hub 按指纹钉扎连过去，用一次性码换成长期凭据
     （hub 生成随机凭据交给连接器，连接器存 hash），然后照常开流连 node。
  3. hub 把凭据加密存 Postgres（和邮箱凭据同一套 AES-GCM 做法），不写 `hub.json`。
- **hub.json 的 `machines`**：kind `link`（主动，现状）、新 kind `service`（被动：`address`，可选 `proxy`（`socks5://…`，hub 经代理去连，
  例如 example-machine 只能经某台机器上的 SOCKS 到达））。
- **SSH**：不再推荐，管理界面不再提供「添加 ssh 电脑」；已有的 kind `ssh` / `tailcat` 机器继续能用（兼容），文档写迁移到 service / link 的步骤。以后再删。
- **node 更新**：不能再经 SSH 推二进制。本轮：service / link 机器的「重新部署」改成提示「到这台电脑上更新」；经连接器推更新留给 #284（hub 版本更新）。

## 5. 一台电脑多条路：去重和选路

- 中心按 `machine_uid` 去重。同一台电脑可以有多条路：配置里的几条（例如同时配了 service 和 link）、经各台成员 hub 的。机器列表里只出现一次。
- 同一时刻只用一条（常驻 node 只接一个 attach，新的顶掉旧的）。选路：本机 > 直接（service / link / ssh / tailcat）> 经成员 hub；同类里按最近测到的 RTT。
  **有粘性**：当前这条健康就不换；断了立刻换下一条；更好的路要稳定一段时间（配置值）才换过去。不能来回抖。
- **机器 id**：中心 `hub.json` 里配的电脑用它的配置 id；只在成员上配的电脑用成员的配置 id，和中心撞名时加 `@<成员名>`。旧 node 没有 `machine_uid`：不去重。
- **机器列表字段**（客户端约定，取代 plan-hub-mesh 2.6 里 `hub_id` / `route` 的含义，键名不变）：
  - `hub_id` / `hub_name`：这台电脑当前经过的 hub（直接连的是中心自己，经成员的是那台成员）；
  - `route`：`{"via": "local|service|link|ssh|tailcat|hub", "hops": 0|1, "rtt_ms": 12}`；
  - 新增可选 `paths`：每条路一项 `{"via": "…", "hub_id": "…", "ok": true, "rtt_ms": 12, "active": true}`，给管理界面和机器详情用。
- hello 的 `mesh.hubs[]` 每项加 `"center": true|false`；客户端在同组的几台里优先连中心（连成员也能用，多一跳）。

## 6. 管理接口补充（接在 plan-hub-mesh 2.7 上）

| 接口 | 作用 |
| --- | --- |
| `GET /api/v1/admin/mesh` | `{"role":"center|member","center":{"hub_id","name","online"},"term":3}` |
| `GET /api/v1/admin/peers` | 每项加 `role` |
| `POST /api/v1/admin/peers/{hub_id}/center` | 设为中心（原中心离线时要 `{"confirm":true}`） |
| `POST /api/v1/admin/machines` | 支持 kind `service`：`{"pair_link":"jarvis://connector?…","id","name","address"?,"proxy"?}` |
| `GET /api/v1/admin/machines` | 每台带 `paths` |

## 7. 分工

| 路 | 内容 | issue | worktree（`~/code/self/jarvis-mesh/`） |
| --- | --- | --- | --- |
| H | hub 侧：多路径机器模型和选路；kind `service`（拨连接器、钉扎、凭据加密存、socks5 代理）；SSH 降为兼容；单中心改造（选中心、任期、成员转发客户端、成员把电脑交给中心、停用成员自己的服务、凭据迁到中心、设为中心）；管理接口和管理界面（`web/src/admin/`）对应页面；文档（`mesh.md` 改写、`server.md`、`config.md`、`protocol.md`） | #435 | `h-center` |
| C | 连接器：被动模式（监听、TLS 身份、配对码、认证和限流、只转 node-attach.sock）、`pair --listen`、模式切换；Mac 菜单栏显示模式和监听地址；Linux headless 的 install 支持被动模式；文档 `link.md`。不碰 `server/internal/hub/` | #436 | `c-connector` |

- 被动模式的握手以本文第 4 节为准；连接器那一路把细节写进 `link.md`，hub 那一路按它实现拨号侧。改协议先改本文。
- Windows（#299）：被动模式同样适用，已补充给 Windows 那边的会话。
- 客户端：「同组优先连中心」是小改动，等 H 合并后再做（iOS 已合并、Android #396 待合并）。

## 8. 不在这一轮

- 中心自动故障转移；经连接器推送 node 更新（#284）；删除 SSH 支持。
