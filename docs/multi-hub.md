# 一台设备连多组 hub（iOS / Mac / 手表）

#256，总览见 [plan-hub-mesh.md](plan-hub-mesh.md) 第 4 节，hub 给客户端的接口约定见同文第 2.6 节。Android 的同一件事见 #295，语义一致（分组规则、默认组、只有一组时不变）。

## 用户看到的

- 设置 › 服务器里多了一行「hub」（只有一组时也在：添加第二台从这里进；**这是只有一组时唯一的像素变化**），进去是按组列出的已配对 hub：
  添加 hub、给组改名、设为当前 hub、断开某一台（组里还有别的时）、断开整组。
- Agents、机器、需要你处理的交互、收件箱里的 Agent 请求把各组合在一起显示。有多组时机器名后面带组名（「MacBook · 公司」），
  机器页按组分块、块头有该组连接状态的圆点；Agents 左上角菜单多一个「hub」选择，只看某一组。
- Jarvis 助手、收件箱、hub 相关的设置（模型、连接器、设备、通知……）是每台 hub 自己的，**不合并**，按「当前 hub」显示：
  Jarvis 顶栏菜单、收件箱左上角、设置首页顶部可以切换。
- 只有一组时上面这些全都不出现，界面和以前一模一样（截图逐像素对比过，见「测试」）。

## 术语

| 词 | 意思 |
| --- | --- |
| 组 | 已配对的 hub 按互联关系分成的组；一组一条连接 |
| 默认组 | id 为空串 `""` 的那组：旧版本的单份配对迁移成它，或者第一次配对的那组。只有它一组时，所有键、路径和以前逐字节一样 |
| 当前 hub（`AppModel.hubGroup`） | hub 级页面用哪一组。**只决定 hub 级页面**，不影响会话键、机器 id、草稿、置顶、缓存 |

## 存储

- 配对注册表 `HubRegistry`（`apple/Shared/Hubs.swift`）存钥匙串的 `hubs` 项：每台 hub 一条 `PairedHub`（地址 + 本设备在那台的凭据、
  所属组、`hub_id` / `hub_name`、上次 hello 的 `mesh`、本设备的 token id、手表的只答凭据），加上用户给组起的名字。
- 迁移：第一次读注册表时，旧的 `server`（单份 `ServerConfig`）、UserDefaults 的 `auth.deviceID`、钥匙串的 `watch` 一起搬进默认组的那一台。
  旧项只在注册表写进去、读回来逐字节一样之后才删；没写成就留着，这次用内存里的，下次启动再迁。
- 读不出来 ≠ 没配对：钥匙串读出错（开机后第一次解锁前的 `errSecInteractionNotAllowed`，后台被推送唤醒时可能碰上）或者存的东西这个版本解不开，
  `HubStore.read` 返回 `.unreadable`。这时 App 先当未配对显示，但**什么都不写**（`AppModel.saveRegistry` 拦住，空注册表也不会去删钥匙串），
  回到前台或手机解锁（`protectedDataDidBecomeAvailable`）时再读，读到了就照正常启动连上。只读的地方（Siri、通知按钮、推送注册）读不到就当没有，不写。
- 设备凭据按 hub 分开：每台配对过的 hub 各自用配对 token 换本设备自己的（`upgradeCredentials`），手表凭据按组（在组里第一台换）。
- 推送密钥按组分开：默认组用原来的 `push-key`，别的组 `push-key.<组 id>`（`PushCrypto.account(group:)`），断开一组时删掉它的密钥。

## 分组（`HubGrouping.regroup`，纯函数，有单测）

- 两台已配对的 hub，只要一方 hello 的 `mesh.hubs` 里有另一方（按 `hub_id`），就是同一组；传递（A–B、B–C 则三台一组）。同一个 `hub_id` 配对了两次也是一组。
- 组 id 客户端本地生成、保持稳定：第一次配对的那组是默认组 `""`，之后的新组用随机的 `g` + 8 位十六进制（不含 `|`、`/`，键里要用它们分隔）。
  默认组断开后别的组保持原样，以后新配对的组也不会接替默认组（不提拔新主组，plan 第 4 节）。
  合并时保留最早配对的那台所在组的 id；拆开时最早那台所在的部分保留原 id，其余部分用自己成员的旧 id（没被占用的话）或新 id。
- 配对一台 hub 时先请求 `GET /api/v1/health` 拿 `hub_id`：已经配对过同一台（同 `hub_id`，旧 hub 不报时按同一地址）就只换凭据；
  出现在某组的 mesh 里就直接并进那一组，不会先单独显示一组。
- 每次 hello 都更新那台的 `hub_id` / `hub_name` / `mesh`（socket 在一个没配对、只是 mesh 列出的地址上时记在组里第一台上），
  也顺便按 mesh 里的地址认出组里还没连过的已配对 hub。分组变了：消失的组连同它的列表、缓存、草稿、置顶、推送密钥一起清掉，新组建连接。
- 约定里 mesh 列出组里所有 hub（离线的 `online: false` 也列），所以互联链路暂时断开不会让组拆开再合并；只有真的解除互联才会拆。

## 连接和选路（`HubLink`、`HubRoute`）

- 每组一个 `HubLink`：一条 `LiveConnection`、那台 hub 的 hello（features、名字）、Fleet 状态、桌面端列表、置顶 / 静音（`PinsStore`）。
- 候选地址：组里每台已配对的 hub（各用自己的凭据），再加 mesh 列出的、本设备没配对的 hub 的 `endpoints`（用组里第一台的凭据：约定 2.6 凭据组内通用）。
  iOS 原来没有候选地址的选路规则（一台 hub 只有一个地址），所以这里的「现有规则」就是：只有一个候选时什么都不做，和以前完全一样；
  多个候选时每次连接前同时请求各候选的 `GET /api/v1/health`（3 秒），用最先答上来的；都不答就用第一个，交给 socket 自己的退避重试。
- **优先连中心**（[plan-hub-center.md](plan-hub-center.md) 第 5 节，#586）：hello 的 `mesh.hubs[].center` 标出组里的中心，候选里中心排第一。同时探测时中心答了就用中心；成员先答时，再等中心最多 1.5 秒（`HubRoute.centerGrace`），中心的探测失败了就不等；都不答用中心。组里没有中心（中心出现之前的 hub）时和原来一样，谁先答用谁。
- **中心恢复后换回去**：连在成员上时，每 5 分钟（`HubRoute.centerRecheck`，App 在后台时不做）探一次中心，答了就让连接重新选路（`LiveConnection.reroute()`），只会断开重连这一次。连成员也能用，只是多一跳：成员把请求原样转给中心。
- 连着的那台掉了：socket 失败 → 重连时重新选路 → 换到组里别的 hub（`MultiHubTests.testGroupMovesToItsOtherHub` 验证过）。不同组各连各的，互不影响。
- 请求（`APIClient`）走 socket 当前所在的那台（`HubLink.config`）；一个会话、机器、交互的请求一律发给它自己那组（`model.api(group)`），
  hub 级页面用当前 hub（`model.api`）。
- 标题的连接状态（`ConnectionStatus`）：一组时规则不变；多组时只有**所有组都连不上**才显示「连接中…」——一组常年连不上（只在 VPN 里能到的 hub）时，
  不能让每个标题都一直转圈；哪组断了看设置 › hub 和机器页的圆点。

## 键（`SessionKey`、`MachineRef`）

- `SessionKey` 加组（`hubGroup`，默认组为 nil）：`组 + 机器 id + 会话 id`。`Session`、`Machine`、`Interaction` 带客户端自己打的组（JSON 键 `app_hub_group`，
  hub 永远不发）：从哪条连接 / 哪个组的 `APIClient` 来就打哪个组（`ServerEvent.stamped`、`APIClient.stamp`）。
- 字符串形式 `SessionKey.path` / `MachineRef.path`：默认组是原来的 `machine/session`、`machine`，别的组前面加 `组 id|`。
  启动参数、记住的对话（`dev.openChat`、`assistant.current`）、机器筛选（`dev.machineFilter`）、`launcher.<机器>` 之类都用它，所以默认组的这些值不变。
- 按会话 / 机器存的东西跟着组走：`ChatCache`（默认组在目录顶层、文件名和以前一样，别的组在 `group-<组 id>/` 子目录）、草稿和待发消息（JSON 里默认组没有组字段）、
  `StateCache` 快照（默认组 `dev`，别的组 `dev.<组 id>`）、置顶 / 静音（hub 各存各的，本地副本默认组 `dev.pins`，别的组 `dev.pins.<组 id>`）、
  后台同步的 rev、启动器缓存。交互字典按 `Interaction.uid`（别的组 `组|id`），列表的 `ForEach` 用 `uid` / `ref`，两组里同 id 的东西不会撞。
- `ChatCache` 目录版本号加一（`chats-v5`，plan 第 4 节）。item 结构没变，所以第一次用到缓存时把 `chats-v4` 整个挪成 `chats-v5`（文件名就是默认组的键）：
  升级后缓存的对话都还在（#256 的验收）。
- 从外部来的键（深链、通知、Siri、启动参数）不知道组时用 `AppModel.resolve`：说了组且组还在就用它；没说就找哪个组有这个会话，再不行落第一组。
  hub 级页面从自己的 hub 读到的键（定时任务的对话、AI 分身的过程）用 `inHub` 放进当前 hub。

## 推送和通知

- 推送注册：每组在组里第一台注册一次（它签发了凭据，约定 2.6：组里别的 hub 的事件转到它再推，一个设备一份），带该组自己的推送公钥。
- 收到推送先判断是哪组：payload 顶层的 `hub_id`（约定 2.6，L1 加）→ 打开它的是哪组的密钥（通知服务扩展逐把试，打开的那把写进 `jarvis_group`）
  → 哪组有这个会话 → 第一组。通知按钮（允许 / 拒绝 / 批准 / 打回）发给那一组的 hub；点开通知打开那一组的会话；静默唤醒只唤醒那一组的连接。
- 断开一组只是本机忘记（和以前「断开并忘记服务器」一样，不撤销 hub 上的设备）；那台 hub 之后推来的、用已删密钥封的推送只显示中继的「有新消息」。

## 实时活动

- App 在前台时把各组的 Fleet 合成一个：数量相加，各组的请求排在前面；别的组的行 id 和交互 id 前面带 `组|`（`FleetMerge`），
  点行打开那组的会话，「允许 / 拒绝 / 批准」发给那组的 hub。
- 活动的推送 token 只报给第一组的 hub：两台 hub 往同一个活动推会互相覆盖。所以 App 在后台时活动只跟第一组的推送走，
  不带组前缀的 id 都算第一组的。这是取舍：后台合并要 hub 之间合并 Fleet，不在这一轮。

## 手表、Siri、Mac、体验模式

- 手表：手机把每组的凭据和组名一起发过去（`WatchConfigs`；旧版手表 App 仍读 `config`，是默认组的）。手表分别请求各组、合并显示，
  机器名带组名，回答 / 发送 / 中断发给那一组；收件箱只显示第一组的。
- Siri / 快捷指令：机器和会话的实体覆盖所有组（id 用 `path`，机器名带组名）；「问 Claude」发到所选机器那一组；「状态」各组相加；「批准第一个」取各组里最早的，在它自己那组批准。
- Mac App 和 iOS 共用这些逻辑（`Jarvis/` 和 `Shared/`），通知和深链同样按组路由；顺带补上了 Mac 编不过的 `NSScrollView.isTracking`（#243 引入）。
- 体验模式照常：它是一个假的组（默认组），只能单独存在；配对真实 hub 就退出。

## 没做的 / 限制

- 后台时实时活动只反映第一组（见上）。
- 收件箱、Jarvis、知识库、模型配置、通知设置等 hub 级页面一次看一组（约定 2.6：不跨 hub 合并）。
- 断开一组不撤销那台 hub 上本设备的凭据和推送注册。
- 组里第一台 hub 下线时，推送注册还在它那里（约定里推送由签发方发），这段时间那组的推送可能收不到。
- 只靠 mesh 知道的 hub（本设备没配对它）只用它列出的地址：hub 开了 TLS 时那是自签证书的 https 地址，iOS 这边还没有按 `fp` 钉扎，
  这样的地址探测不通、不会被选上（选路只在配对过的 hub 之间换）。要让手机能直接换到组里某台 hub，就也配对它（同组的会并进同一组）。

## 测试

- 单测 `apple/JarvisTests/MultiHubTests.swift`：默认组的键和 JSON 不变、别的组的键、mesh 字段解码、分组（合并 / 单边 / 传递 / 拆分 / 同一台配两次）、
  新组 id、候选地址和选路、hello 解码、事件打组、Fleet 合并、多组的连接状态、各组推送密钥。
- UI 测试 `apple/JarvisUITests/MultiHubTests.swift`：三台假 hub、两组（其中一组两台）：合并列表标明组、允许发到对应的 hub、断开一组另一组不受影响、
  同组一台挂了换到另一台。怎么起假 hub 见 [testing.md](testing.md)「多 hub」。
- 只有一组时的零变化：同一份 mock、同一组启动参数，改动前后各拍 14 个界面 × 深浅色逐像素对比，差异只在系统菊花和玻璃高光的随机噪声里（testing.md「多 hub」）。
