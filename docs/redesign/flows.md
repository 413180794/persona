# 主要功能流程图

> **2026-10-31 起，本文讲 Android 外观、布局和交互的部分已被 [Android 界面风格与设计系统](../android-design.md) 取代**：用户要求视觉和交互整体重做，做成玻璃（默认）、晨报、看板三套界面风格。功能、数据、协议、门禁的规格仍以本文为准。

> 草案（2026-10-30），属于 [重设计方案](README.md)。这一轮只在 Android 和 hub 上落地，iOS 不改。
> 图用 Mermaid 写，GitHub 和多数 Markdown 预览能直接画出来；同一批图也放在界面预览里（[`prototypes/android/flows.html`](../../prototypes/android/flows.html)）。
> 每张图下面标了对应的规格章节；图和规格不一致时以规格为准，并回来改图。

| # | 流程 | 规格 |
| --- | --- | --- |
| 1 | [整体：谁连谁](#1-整体谁连谁) | README D8、D9 |
| 2 | [配对](#2-配对) | ux.md 3.7，hub-changes.md 1.3 |
| 3 | [配对失败怎么提示](#3-配对失败怎么提示) | ux.md 3.7，android.md 5.1 |
| 4 | [选路：多地址探测](#4-选路多地址探测) | android.md 5.1，hub-changes.md 1.2 |
| 5 | [连接状态](#5-连接状态) | ux.md 5.5 |
| 6 | [常驻连接和心跳](#6-常驻连接和心跳) | android.md 5.8、5.9，hub-changes.md 1.6、3.8 |
| 7 | [地址变了以后怎么找回](#7-地址变了以后怎么找回) | android.md 5.1，hub-changes.md 1.2、1.7 |
| 8 | [通知 → 审批 → Agent 继续](#8-通知--审批--agent-继续) | ux.md 3.4、9，android.md 5.8 |
| 9 | [「做完了」通知](#9-做完了通知) | hub-changes.md 3.9 |
| 10 | [发消息：outbox 和结果三态](#10-发消息outbox-和结果三态) | ux.md 5.2，README D3 |
| 11 | [打开对话：同步和连续性](#11-打开对话同步和连续性) | README D4，`docs/protocol.md`「items 的连续性」「折叠的工作」 |
| 12 | [收件箱和 AI 分身](#12-收件箱和-ai-分身) | ux.md 3.6，`docs/inbox.md` |
| 13 | [分享到 Persona](#13-分享到-persona) | android.md 6.3 |
| 14 | [首次准备清单（国产系统）](#14-首次准备清单国产系统) | ux.md 3.7，android.md 5.18 |
| 15 | [App 内更新](#15-app-内更新) | android.md 第 9 节 |

## 1. 整体：谁连谁

Android 只连 hub：在外面经公网（优先 IPv6）走 TLS 端口并钉公钥；在家里也可以走内网。现有 iOS 照旧走明文端口（内网或 VPN），推送走 APNs。

```mermaid
flowchart LR
  A["Android App<br/>前台服务常驻 WebSocket"] -- "HTTPS / WSS<br/>TLS 端口 · 钉公钥<br/>优先公网 IPv6" --> H["personad hub<br/>家里常开的机器"]
  I["现有 iOS App（这一轮不改）<br/>推送走 APNs"] -- "HTTP / WS<br/>明文端口<br/>内网或 VPN" --> H
  H --- P[("Postgres")]
  H -- "SSH" --> N1["personad node<br/>电脑 A"]
  H -- "SSH" --> N2["personad node<br/>电脑 B"]
  N1 --> C1["herdr 里的<br/>Claude Code / Codex"]
  N2 --> C2["herdr 里的<br/>Claude Code / Codex"]
  H --> X["飞书<br/>邮箱（IMAP / SMTP）"]
```

## 2. 配对

二维码格式不变（现有 iOS 照样能扫），token 换成 10 分钟内有效、只能用一次的临时 token；新参数给 Android 用。

```mermaid
sequenceDiagram
  autonumber
  actor U as 你
  participant CLI as personad hub pair<br/>（hub 机器上）
  participant H as hub
  participant A as Android App
  participant O as 已配对的其他设备
  U->>CLI: 运行
  CLI->>H: 经本机回环申请临时 token
  H-->>CLI: 临时 token（落库，10 分钟，只能换一次）
  CLI-->>U: 二维码 url · token · hub · fp · tls · alt
  U->>A: 扫码
  A->>A: 记下 hub_id、公钥指纹、候选地址
  par 错峰 250 ms 并发探测
    A->>H: TLS 握手 + GET /health（公网 IPv6）
  and
    A->>H: TLS 握手 + GET /health（域名 / 内网 / 公网 IPv4）
  end
  H-->>A: 证书 + hub_id
  A->>A: 公钥指纹等于 fp 且 hub_id 一致，选定这条路径
  A->>H: POST /auth/tokens（带临时 token）
  H-->>A: 本设备 token（临时 token 作废）
  H--)O: 通知「有新设备配对」
  A->>H: GET /machines，连 WebSocket，发 client_hello
  A-->>U: 准备清单（流程 14）
```

## 3. 配对失败怎么提示

不给假进度，失败按原因给能照做的提示。在家里 Wi-Fi 上超时，先排除本地网络权限（Android 17 起没授权时表现为超时）。

```mermaid
flowchart TD
  S["扫码 / 粘贴配对链接"] --> P{"能解析？"}
  P -- "否" --> E0["不是 Persona 的配对二维码"]
  P -- "是" --> V{"带 fp 和 tls？"}
  V -- "否（旧格式 / 旧 hub）" --> LAN["只用内网明文地址<br/>设置里提示：hub 太旧，不能公网直连"]
  V -- "是" --> R["探测全部候选（流程 4）"]
  LAN --> T
  R --> W{"有候选通过<br/>证书 + hub_id 校验？"}
  W -- "是" --> T["用临时 token 换本设备 token"]
  W -- "否" --> C{"按原因分类"}
  C -- "DNS 失败" --> E1["找不到这个地址<br/>【重新探测】"]
  C -- "被拒绝" --> E2["hub 没在这个端口监听<br/>【重新探测】"]
  C -- "超时，在家里 Wi-Fi 上<br/>没有本地网络权限" --> E3["在家里连 hub<br/>需要本地网络权限<br/>【去授权】"]
  C -- "超时" --> E4["在家：是否同一个网络<br/>在外：路由器是否允许<br/>从外网连进来<br/>【连接诊断】"]
  C -- "证书不符" --> E5["不是你配对的 hub<br/>或 hub 换了证书<br/>【重新扫码】"]
  T --> X{"hub 的回应"}
  X -- "401 / 403" --> E6["配对码已失效或已被用过<br/>【重新扫码】"]
  X -- "code = incompatible" --> E7["请更新 hub"]
  X -- "成功" --> OK["读机器列表 → 准备清单"]
```

## 4. 选路：多地址探测

参考 Happy Eyeballs v2（RFC 8305）：错峰并发，先通过身份校验的赢。旧前缀可能已经分给了别人，所以「连得上」不够，证书和 `hub_id` 都要对上。

```mermaid
flowchart TD
  T["触发<br/>启动 · 断线 · 换网络<br/>收到新候选 · 定期复查"] --> K{"这个网络上<br/>有记住的赢家？"}
  K -- "有" --> F["先试它"]
  F -- "成功" --> D
  F -- "失败" --> O
  K -- "没有" --> O["排序<br/>公网 IPv6 → 域名 → 内网 → 公网 IPv4<br/>（公网 IPv4 只有 hub 确认可达才下发）"]
  O --> S["每隔 250 ms 多发起一个<br/>TLS 握手 + GET /health"]
  S --> J{"公钥指纹 = fp<br/>且 hub_id 一致？"}
  J -- "最先通过的" --> D["选定路径，停掉其余探测<br/>按网络（Wi-Fi / 蜂窝）记住赢家"]
  J -- "全部失败" --> L{"在家里 Wi-Fi 上，<br/>本地网络权限没给？"}
  L -- "是" --> PERM["状态：需要本地网络权限"]
  L -- "否" --> B["状态：连接中…<br/>退避 1→20 秒再来"]
  D --> WS["连 WebSocket → client_hello<br/>→ 重新订阅打开的会话 → 对账待处理交互"]
```

## 5. 连接状态

iOS 的四态照搬，Android 多三个要你处理的状态（橙色，不自动重试：授权后回到「连接中」，重新扫码后从头配对）。变坏要持续 1.5 秒才显示，恢复立刻显示。

```mermaid
flowchart LR
  U(["未配对"]) -- "配对成功" --> C["连接中…"]
  C -- "WebSocket 连上" --> UP["更新中…"]
  UP -- "待处理交互和打开的<br/>对话补齐（最多 8 秒）" --> O(["在线"])
  O -. "断线 · ping 5 秒没回<br/>· 判定连接已死" .-> C
  O -- "系统报告没网" --> W["等待网络"]
  C -- "系统报告没网" --> W
  W -. "网络恢复，立即重连" .-> C
  C -- "在家里 Wi-Fi 上<br/>超时且没授权" --> P["需要本地网络权限<br/>【去授权】"]
  C -- "公钥指纹不符" --> X["hub 的证书变了<br/>【重新扫码】"]
  C -- "401" --> R["配对已失效<br/>【重新扫码】"]
  classDef you fill:#FFF4E0,stroke:#D9922B,color:#1C1C1E
  class P,X,R you
```

## 6. 常驻连接和心跳

Android 深睡时客户端的 ping 不触发（OkHttp 的计时用 `System.nanoTime`，深睡时停走），所以后台心跳由 hub 发，按连接协商间隔。旧连接（现有 iOS）不协商，保持 20 秒。

```mermaid
sequenceDiagram
  participant S as Android 前台服务
  participant H as hub
  Note over S,H: App 在前台
  S->>H: client_hello（心跳 20 秒）
  H-->>S: client_hello_ack（实际生效的值）
  loop 每 20 秒
    H->>S: WebSocket ping
    S-->>H: pong
  end
  S->>H: 应用层 ping（id）
  H-->>S: pong（同一个 id），5 秒内回来才算网络好
  Note over S,H: App 进后台
  S->>H: client_hello（心跳从 4 分钟起，按网络调整）
  H-->>S: client_hello_ack
  loop 约每 4 分钟（抖动只往下取）
    H->>S: WebSocket ping（网络包唤醒手机）
    S-->>H: pong
  end
  H->>S: interaction / notify 事件
  S->>S: 发通知
  Note over S: 每次被唤醒，距上次收到 hub 的数据超过<br/>「间隔 + pong 超时」就立即重连
  Note over S: 网络变化时立即重新选路、重连
```

## 7. 地址变了以后怎么找回

家宽重新拨号时 IPv6 前缀会变，旧连接当场断开。Android 没有 FCM 这样的旁路，人在外面只能靠域名找回，所以推荐配 DDNS。

```mermaid
sequenceDiagram
  participant A as Android
  participant H as hub
  participant D as DNS（DDNS）
  Note over H: 家宽重新拨号，IPv6 前缀变了
  H--xA: 旧连接断开
  H->>H: netlink 收到地址变化，重算候选
  H->>D: 更新 AAAA 记录（配了 DDNS 时）
  A->>A: 断线，重新选路（流程 4）
  alt 平滑换前缀，旧连接还活着
    H->>A: endpoints 事件（新地址）
  else 人在外面，配了 DDNS
    A->>D: 解析 hub 的域名
    D-->>A: 新地址（TTL 最低 600 秒，可能还是旧的）
    A->>H: 探测新地址（钉公钥 + 核对 hub_id）
  else 人在外面，没有域名
    A->>A: 等回家连上内网
    Note over A: 诊断页提示：建议给 hub 配一个 DDNS 名字
  end
```

## 8. 通知 → 审批 → Agent 继续

「允许 / 批准」要先解锁；拒绝原因和计划意见可以直接在通知里输入。处理完（不管在哪处理的）撤回通知。

```mermaid
sequenceDiagram
  autonumber
  participant C as Claude Code（电脑上）
  participant N as personad node
  participant H as hub
  participant S as Android 前台服务
  actor U as 你
  C->>N: PermissionRequest hook（要运行一条命令）
  N->>H: 新交互
  H->>S: WebSocket interaction 事件
  S->>U: 通知：命令摘要 · 允许 · 拒绝 · 打开
  alt 点「允许」
    U->>S: 先解锁（setAuthenticationRequired）
    S->>H: resolve allow（带 client_op_id）
  else 点「拒绝」并写原因
    U->>S: 在通知里输入原因（RemoteInput）
    S->>H: resolve deny + 原因
  end
  H->>N: 转交决定
  N-->>C: hook 返回，Agent 继续
  H->>S: interaction_resolved
  S->>S: 撤回这条通知
  Note over H,S: 结果未知（超时 / 断线）时<br/>用同一个 op id 重试，由 hub 去重<br/>或先重读交互列表<br/>卡片显示「结果待确认」
```

## 9. 「做完了」通知

由 hub 判断发不发，iPhone 和 Android 用同一套规则，只是送达的通道不同。手机只负责显示，不再自己算（PR #84 现在是手机自己算）。

```mermaid
sequenceDiagram
  participant N as node（电脑上的 Agent）
  participant H as hub
  participant I as iPhone
  participant S as Android 前台服务
  N->>H: 会话状态：工作中 → 完成未看
  H->>H: notify.go 的规则：等 done_delay、只推长任务、<br/>后台任务结束才推、看过不推、屏蔽的不推
  par 同一套判断，两个通道
    H->>I: APNs
  and
    H->>S: WebSocket notify（kind = done）
  end
  S->>S: 正在前台看这个会话就不弹
```

## 10. 发消息：outbox 和结果三态

先写进 outbox 再清空输入框，同一条消息始终用同一个 `client_msg_id`。「发出去没回音」不再当没网盲目重发。

```mermaid
flowchart TD
  S(["点发送"]) --> Q["写进 outbox，再清空输入框<br/>生成 client_msg_id"]
  Q --> N{"在线？"}
  N -- "否" --> WN["连上后自动发送<br/>连上且「更新中」结束后按顺序发"]
  N -- "是" --> SD["发送中"]
  WN -. "连上了" .-> SD
  SD --> R{"结果"}
  R -- "hub 收下，node 打完字" --> OK(["已送达"])
  R -- "请求没发出去<br/>DNS / 被拒 / 没网" --> WN
  R -- "确定的拒绝<br/>input_draft / dialog_open 等" --> RF["没有发出：原因<br/>【清空并发送】【编辑】"]
  R -- "发出去没回音<br/>超时 / 断开" --> UK{"hub 支持<br/>client_ops？"}
  UK -- "是" --> RT["同一个 id 自动重试<br/>hub 去重，返回原结果"]
  RT -. "重试" .-> SD
  UK -- "否" --> VF["正在核对<br/>等对话记录里出现"]
  R -- "502 delivered = unknown<br/>电脑上打字到一半出错" --> VF
  VF -- "出现了" --> OK
  VF -- "超过时限" --> NR["没能确认是否送达<br/>【重发】【不发了】"]
  classDef pending fill:#FFF4E0,stroke:#D9922B,color:#1C1C1E
  class WN,RF,VF,NR pending
```

## 11. 打开对话：同步和连续性

规则沿用 `docs/protocol.md`「items 的连续性」和「折叠的工作」，写进 `spec/fixtures/continuity/`，Android 的 reducer 和 Go 的仿真共用。

```mermaid
flowchart TD
  O["打开会话"] --> C["先画本地缓存（末尾两轮）"]
  C --> Sub["subscribe：since_seq = covered，fold_work = true"]
  Sub --> B["收到一批 items（from_seq、to_seq、history）"]
  B --> H{"history 变了？"}
  H -- "是（文件被替换 / 重写）" --> RE["丢掉本地内容，重新取最新一页"]
  H -- "否" --> G{"from_seq ≤ covered？"}
  G -- "是" --> M["合并，covered 推到 to_seq"]
  G -- "否（中间丢了一批）" --> W["等 1.5 秒（补发可能还在路上）"]
  W --> F["after_seq = covered 补洞"]
  F -- "补齐" --> M
  F -- "超过 8 页" --> RE
  M --> V["渲染：每条消息一行，工作折叠成一行"]
  RE --> V
  V --> E{"点开折叠的工作？"}
  E -- "是" --> FW["按 from_seq … through_seq 取回这一段<br/>替换掉 fold"]
  FW --> V
```

## 12. 收件箱和 AI 分身

飞书消息和邮件在一个列表里，一行一件事。AI 分身只起草、不发送，发送要你点一下；发送带 clientId，重试不会发两遍。

```mermaid
flowchart TD
  F["飞书消息"] --> H["hub 采集"]
  M["邮件"] --> H
  H --> T["模型 1：分拣<br/>需要你 / 知悉即可 / 噪音 + 一句摘要"]
  T --> J{"模型 2：要不要<br/>交给 Agent？"}
  J -- "交，附一两句任务" --> G["模型 3：无头 Agent 查清楚<br/>交草稿和依据（inbox_draft）"]
  J -- "不交" --> I
  G --> I["收件箱：一行一件事<br/>草稿挂在行下面"]
  I --> U{"你"}
  U -- "已处理" --> D["移到已处理<br/>3 天内可以移回"]
  U -- "打开" --> R["App 内打开<br/>飞书会话页 / 邮件"]
  U -- "点「发送」" --> S["POST /reply（带 clientId）<br/>发完标已处理"]
  U -- "修改" --> E["草拟浮层：草稿 + 依据<br/>+ 查看 AI 分身的处理过程"]
  E --> S
```

## 13. 分享到 Persona

Android 独有的入口：任何 App 的文字、链接、图片、文件发进 Persona，正合「把各处信息收到一起交给 AI」。

```mermaid
flowchart LR
  X["任意 App 的分享菜单"] --> J["Persona（分享目标）"]
  J --> P{"发到哪里"}
  P -- "默认" --> M["Persona 主对话"]
  P -- "选一个" --> AG["某个 Agent 会话"]
  M --> OB["走 outbox：先存再发<br/>带 client_msg_id（流程 10）"]
  AG --> OB
  OB --> UP["图片、文件先上传，<br/>消息里带路径"]
```

## 14. 首次准备清单（国产系统）

配对后逐项检查，每项都能跳过，并写明跳过会少什么。没有一家国产系统提供「查自启动、查后台白名单」的接口，所以能查的显示真实状态，查不了的按系统给步骤，再用锁屏测试通知兜底。各家的入口见 [android.md](android.md) 5.18。

```mermaid
flowchart TD
  S["配对完成"] --> ID["读系统属性：哪家系统、哪个版本"]
  ID --> A{"能查的都打开了？<br/>通知总开关 · 各渠道级别<br/>电池优化 · 后台限制"}
  A -- "没有" --> A1["逐项「去设置」<br/>回到 App 时重查"]
  A -- "是" --> C
  A1 --> C["查不了的：按这家、这一版给步骤<br/>自启动 · 无限制 · 最近任务加锁<br/>华为：关静默通知、休眠时保持网络<br/>荣耀：关智能省流量"]
  C --> D["你点「已完成」<br/>跳转失败就退到应用详情页"]
  D --> T["自检：锁屏 30 秒后<br/>hub 发一条测试通知"]
  T --> Q{"响了、亮屏了？"}
  Q -- "没有" --> C
  Q -- "是" --> L{"target 37：在家里连 hub<br/>要本地网络权限"}
  L -- "没给" --> L1["申请；被拒就说明<br/>在家里只能用不算本地网络的地址"]
  L -- "已给 / 不需要" --> E
  L1 --> E["在外面能连上吗？<br/>关掉 Wi-Fi 用蜂窝网络试一次"]
  E -- "能" --> OK(["完成<br/>之后诊断页显示断开记录"])
  E -- "不能" --> DG["连接诊断<br/>光猫放行 / DDNS 建议"]
  DG --> OK
```

## 15. App 内更新

没有 Play 商店。APK 放在 hub 上，经 TLS 下载（钉同一个公钥），用系统安装器升级。澎湃 OS 上请求静默安装会直接失败（`INSTALL_FAILED_ABORTED`，不弹任何框），所以国产系统一律弹系统确认。第一次安装没有设备 token，用 `personad hub pair --android` 打印的内网下载链接，或 adb。

```mermaid
flowchart TD
  C["App 在前台时问 hub<br/>有没有新版本"] --> H{"有新版本？"}
  H -- "没有" --> Z["什么都不做"]
  H -- "有" --> T["设置里出现「有新版本」<br/>和更新说明"]
  T --> D["经 TLS 下载 APK（钉公钥）<br/>校验 SHA-256"]
  D --> P{"有「安装未知应用」授权？"}
  P -- "没有" --> P1["跳到系统设置授权"]
  P1 --> K
  P -- "有" --> K{"原生系统？"}
  K -- "是" --> SI["先试静默安装"]
  K -- "否（国产系统）" --> UI["弹系统确认安装"]
  SI -- "失败" --> UI
  SI -- "成功" --> OK
  UI -- "确认" --> OK["签名一致，覆盖安装，数据保留"]
  UI -- "取消" --> L["下次打开时再提示"]
  OK --> RS["进程被杀<br/>MY_PACKAGE_REPLACED 重新拉起常驻服务"]
```
