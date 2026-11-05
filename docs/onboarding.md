# 新用户上手（onboarding）

给不会命令行的普通用户设计的上手路径：看了广告、下了 App（手机、电脑或网页），怎么装好、连上、接上模型、说上第一句话。关联 #287。

代码依据：origin/master `59e8bae9a`，文中 `路径:行` 都在这个提交上核过。现状调研是 2026-11-06 一轮从零的走查（读代码 + 隔离测试 hub 实测 + 网页截图），结论在第 2 节，原始记录不进仓库。

## 1. 一句话和心智模型

**一句话**：今天第一屏就要求「在 hub 所在的机器上运行 `personad hub pair`」，普通人走不下去；目标是「装一个东西、扫一次码、点几下，第一句话就有回答」，而且任何一步缺了，界面上都是一张「还差什么、点这里」的卡片，不是英文报错，也不是空白。

给用户看的说法（首屏「这是怎么回事？」、官网、安装器、文档用同一段）：

> **Persona 有三部分。**
> - **hub**：家里的总机。装在一台一直开着的电脑或 NAS 上，你的消息、设置、记忆都存在这里，只存在你自己的机器上。全家只要一台。
> - **你的电脑**：Persona 干活的地方，在上面读文件、跑程序、写代码。hub 所在的那台电脑自动算一台。
> - **手机 / 网页**：遥控器。看它在做什么，在它动手前点「允许」。扫一次码就连上，以后自动连。
>
> 还要接一个**模型**，它是 Persona 的大脑：已经在用 Claude Code 的直接用；有 API Key 的粘贴进来；什么都没有的，告诉你去哪开通。

```
   手机 / 网页 ──扫码一次──▶  hub（一直开着的那台）  ◀──Persona Link── 你的笔记本
   （遥控器）                 存东西、收消息、调度          （干活的地方）
                                    │
                               这台自己也算一台电脑
                                    │
                              模型（Claude Code 或 API Key）
```

用词规则（第 8.1 节的文案守门测试会查）：

- 引导、空状态、错误卡片里不出现 node、herdr、launcher、TLS、token、hub.json、zsh、端口、IPv6、`~/.` 路径、命令名。需要时放进「详情」或「高级」。
- 「启动器 / 供应商」对普通用户统一叫「模型」（谁家的、哪个 Key）；运行方式、kind、协议、Bearer / x-api-key 收进「高级」。
- hub 在界面里叫什么（「hub」「主机」「中枢」）由用户拍板（第 6.3 节拍板 13）；定之前保留「hub」，第一次出现时带一句「家里的总机」。

## 2. 现状：新用户今天要走的路

| 步 | 今天要做什么 | 卡在哪 | 依据 |
| --- | --- | --- | --- |
| 拿到 App | iOS 只有 TestFlight，要 iOS 26+；安卓要侧载 APK；官网 FAQ 写「想试用请写信给我们」 | 第一步就筛掉大部分人 | `site/public/index.html`、README「平台」 |
| 装 hub | `install.sh --hub` 只放一个二进制，不建 Postgres、不装服务、不启动，结尾是英文 `jarvisd hub run … then jarvisd hub pair` | 空目录启动直接退出：`database_url is empty (run scripts/deploy/deploy.sh …)`，指向用户手里没有的仓库脚本 | `scripts/deploy/install.sh:421`、`server/internal/db/db.go:37` |
| 第一个配对码 | 四端首屏都写「运行 `personad hub pair`」；网页登录页还教 `jq -r .token ~/.persona/hub.json` | 没有终端的人停在这里。`/admin` 能出码，但本机设密码要日志里的一次性设置码 | `web/src/views/Login.tsx:45-48`、`server/internal/hub/admin_setupcode.go:51-52,75-79` |
| 二维码里的地址 | 默认 `public_url` 是 `http://<完整主机名>:18660` | 手机常常解析不了主机名；改了端口它也不跟着变 | `server/internal/hub/config.go:215` |
| 接电脑 | 只能在 `/admin` 添加；手机被明确禁止给电脑出码；新 hub 不开 TLS，「电脑来连」必然 409 `tls_required`；Mac 的 Persona Link 只能从源码构建 | 普通用户基本停在这里 | `hub/pairing.go:132-135`、`hub/admin_machines.go:307-310`、`docs/persona-link-install.md` |
| 主对话 | 首屏看着能用；发第一句才失败 | 没电脑：502 `{"code":"internal","error":"assistant machine is not configured"}`，网页弹一个几秒就消失的 toast；没装 claude：约 6 秒后出 `zsh:1: command not found: claude`；没登录：一条普通英文气泡「Not logged in · Please run /login」 | `assistant/start.go:29,157-158`、`web/src/views/PersonaAssistant.tsx:65`、`assistantwire/wire.go:383-384` |
| 模型 | 供应商、启动器、用途、kind、协议、Bearer / x-api-key 全露在外面；全新 hub 没有任何预设（`{"presets":[]}`）；名字只能用英文；加完带 Key 的供应商，主对话和 Agents 还是「Claude Code 默认」 | 加了 Key 也接不上；「从 ~/.zshrc 导入」零电脑时一直转圈 | `assistant/models_presets.go:20-28`、`assistant/models_launchers.go:98` |
| 空状态 | 零电脑时总览说「一切就绪」，机器页说「hub 连上后……」却没有「添加电脑」，助手设置渲染成「每段对话在 的 里后台运行」 | 误导，且没有下一步按钮 | `web/src/i18n/locales/zh-Hans/home.json:31`、`machines.json:25` |

几条决定设计的事实：

- **主对话离不开一台在线、装好并登录了 Claude Code 的电脑。** 直接 API 的供应商当主对话时，也是在电脑上用 claude 连它的网关（`assistant/models.go:950-982` `finishModel` 只认 `launcher.<名字>`）。Docker 镜像没有 claude，也没有本机电脑（`Dockerfile:79-81` `JARVIS_LOCAL_NODE=0`），所以 NAS / Docker 用户一开始主对话一定跑不了。
- **今天没有一个接口回答「主对话现在能不能用、缺什么」**。`POST /api/v1/assistant/warm` 不论成败都回 `{ok:true}`（`assistant/profile.go:283-289`）。启动器健康检查默认关（`assistant/launcher_health.go:136`），不能拿来做新手判断。
- **零件大多已经有**：一次性配对码和 hub 画好的二维码（`hub/pairing.go:97`，10 分钟、兑换一次）、网页和 iOS 的「连接新设备」、`/admin` 的添加电脑（运行中生效，`hub/devices_link.go:314` `addMachineLive`）、自签 TLS、启动器一词测试（`assistant/models_launchers.go:748`）、未保存就能测的连接测试（`assistant/models_probe.go:358`）、从预设添加（`presetFill`）、改用途（`assistant/models.go:821`）、Android 的准备清单（`ReadinessRecords.kt`）。缺的是把它们串起来、让已登录的 App 能用它们、给出机器能读的错误码。
- **iOS 的 `"ccc"` 兜底**（`AssistantView.swift:738,921`）只在读 `GET /api/v1/assistant` 失败时出现，不是常规卡点，顺手改。
- **官方扩展市场读不到**：调研时的 403 多半是开发机网络造成的，要改的是「读不到时的降级」，不是「默认报错」。

## 3. 目标路径

### 3.1 理想的最短路径（往这个方向做）

前提：签名公证过的一个 Mac 下载（hub + 本机电脑 + 数据库都在里面），见第 6.3 节拍板 1、2。

| # | 用户做的 | Persona 做的 |
| --- | --- | --- |
| 1 | 官网点「下载 Mac 版」，拖进「应用程序」，打开 | — |
| 2 | 首屏「让这台 Mac 当 hub」点「开始」 | 建数据库、写 hub.json（新默认见 4.1）、装 LaunchAgent；本机自动算一台电脑（`kind: local`，`hub/config.go:208`）；检测本机 Claude Code |
| 3 | 系统弹「允许查找本地网络」点「允许」 | 局域网广播（mDNS，开了 TLS 才发，`hub/mesh_discovery.go:26`） |
| 4 | 用 iPhone 相机扫屏幕上的二维码 | 一次性配对码；没装 App 的落到官网下载页 |
| 5 | App 打开，自动连上 | 深链接配对（三端已接） |
| 6 | 「接一个模型」：检测到已登录的 Claude Code 时只有「就用它」一个按钮；否则选一家、粘 Key | 第 4.3 节 |
| 7 | 点「试一句」 | 真跑一轮主对话；成功后本地显示欢迎和三个可点的小任务 |

大约 8 次点按，不碰命令行。

### 3.2 按「我有什么」分的开局

| 我有什么 | 推荐的 hub 位置 | 本期能走到哪 | 卡在什么上 |
| --- | --- | --- | --- |
| 只有一台 Mac + 手机 | 这台 Mac | 体验模式 +「还没有 hub？」页如实说明；有 Docker Desktop / OrbStack 的可以粘一行命令走 `personad hub setup` | Developer ID 公证、无 Docker 时的数据库（拍板 1、2） |
| Mac + 常开的 Linux / NAS | Linux 或 NAS | 粘一行命令（Linux）或下载 compose（NAS）→ 扫码 → App 里「添加电脑」→ Mac 上装 Persona Link（暂时源码构建）→ 接模型 | Persona Link 安装包（拍板 1） |
| 只有手机，想先看看 | 不装 | 体验模式（iOS / Mac / Android 已有）；退出时落到「还没有 hub？」页，不回到配对页原地 | — |
| 公司电脑（装不了东西、电脑受管控） | 家里的 Linux / NAS 或云服务器 | hub 能装好、手机能连；主对话要一台能装 Claude Code 的电脑，否则只能等「只聊天」模式 | 「只聊天」模式（拍板 9）；云模板（拍板 6） |
| 已经在用 Claude Code、有常开 Linux 的研发 | 那台 Linux | 一行命令 → 扫码 → 自动认出已登录的 Claude Code，直接能聊；`~/.zshrc` 启动器照旧 | — |

### 3.3 hub 放哪：取舍

| 位置 | 好处 | 代价（写在选项下面，大白话） |
| --- | --- | --- |
| 常开的 Linux 台式机 / 小主机 | 24 小时在线；它自己就是第一台电脑，用户不用「添加电脑」 | 这台关机，手机就连不上 |
| NAS（Docker） | 家里已有、常开 | Docker 镜像里没有 claude，AI 不在 NAS 上干活，一定要再加一台电脑 |
| 用户自己的 Mac | 只有一台电脑的人不用另找机器；干活的电脑就是它 | 合上盖子 Persona 也休息，醒来后补收消息、补跑定时任务；推送照常走中继（`docs/push-relay.md`） |
| 云服务器 | 随时随地能连 | 要买服务器，数据在云上；没有电脑时只能等「只聊天」 |
| Windows | — | 不能当 hub（#1179）；Windows 电脑只能当「你的电脑」 |

建议写进官网和安装器：**有常开的电脑就装在它上面**。笔记本当 hub 能用，安装器要直说合盖的后果。

出门在外：第一版不打洞、不做中继隧道（拍板 7）。配对后做一次「出门能不能连」的检查（安卓的蜂窝网络测试已有），连不上时说「在家里 Wi-Fi 下能用；出门要用 VPN，或者让路由器打开 IPv6（图文说明）」。

## 4. 设计

### 4.1 安装器：`personad hub setup`

和 `hub pair`、`hub admin` 同一个命名空间。Go 实现，不依赖仓库和 python。`install.sh --hub` 装完二进制后调它（换掉 `install.sh:421-423` 的英文）。

```
personad hub setup [--yes] [--listen :18660] [--tls :18662] [--outside] [--no-service] [--json]
```

按顺序做，每步一行中文结论；失败给下一步，不往下走：

1. **hub.json**：文件不存在时写一份完整的新文件（新默认见下）；已存在时只补 `token` 这类缺了就跑不起来的字段，**已有值一律不改**。
2. **数据库**：`database_url` 为空时，有 Docker 就照 `scripts/deploy/db.sh` 建容器（同名 `jarvis-postgres`、同镜像、同卷，之后 `deploy.sh` 照样认），生成密码写进 hub.json；没有 Docker 就探测本机 Postgres（#321 的做法）；都没有就停下，打印「这台电脑还缺数据库：装 Docker Desktop 或 OrbStack 后重跑这条命令」。`db/db.go:37` 的报错同步改成「先运行 personad hub setup」。
3. **常驻**：Linux 照 `hub-systemd.sh` 装 socket activation 单元（同一套单元，不另起），`loginctl enable-linger` 要 sudo 时问一次并说明为什么；macOS 写 LaunchAgent（新写，`hub-systemd.sh` 现在只找已有的）。端口被另一个 hub 占着时自动换一个并写回。
4. **启动并自检**：等 `/api/v1/health`，跑一遍 doctor，只打「有问题」的项，排除的虚拟网卡不打。
5. **结尾**（中文）：手机扫的配对二维码（复用 `hub pair` 的终端二维码）；「用手机浏览器打开 `http://<局域网 IP>:18660/admin#setup=<设置码>` 设管理员密码」；设置码由安装器经回环 + 主 token 调 `POST /api/v1/admin/setup-code` 取（`admin_setupcode.go:94-99`），用户不用翻 journalctl。`#setup=` 是片段，不发到服务器，管理页读到后预填。

**新安装的默认值只写进新文件，绝不加到 `defaultConfig()`。** `LoadConfig` 把已有的 hub.json 反序列化到 `defaultConfig()` 上（`hub/config.go:241-278`），`Public` 是值类型（`config.go:169`），往 `defaultConfig` 里加 `TLSListen` 会让每一份没写 `public` 的老 hub.json 都开 TLS。另外 `db.sh` 在 hub 第一次运行前就写了只有 `database_url` 的 hub.json，所以 deploy.sh 路线上文件早就存在。做法：`hub setup` 新建文件时显式写这些字段，或只在 `LoadConfig` 的 `os.IsNotExist` 分支写；两处共用一个 `newInstallConfig()`，有 golden 测试证明老文件加载后的结果不变。

新文件写的字段：

| 字段 | 新安装 | 为什么 |
| --- | --- | --- |
| `public.tls_listen` | 默认只绑局域网 IPv4：`"<局域网 IP>:18662"`；`--outside` 或之后在 App 里打开「出门也要用」才改成 `":18662"`（拍板 8） | Persona Link 只走 TLS（`linkPairable`），mDNS 要 TLS 才广播；只绑 IPv4 局域网地址可以不把端口暴露到公网 IPv6。`tls_listen` 认带地址的写法（`public.go:84` 用 `listenTCP`，`mesh_discovery.go:39` 解析 host）。局域网地址会随 DHCP 变，hub 启动时绑不上要回退并在 App 里提示，实现时补这条 |
| `public_url` | `http://<局域网 IPv4>:<listen 端口>`，多网卡时选默认路由那块 | 今天是完整主机名（`config.go:215`），手机常解析不了 |
| `admin.first_setup` | `"code"`：局域网来源设第一个管理员密码也要设置码 | 今天只有回环要码（`admin_setupcode.go:52`），共享局域网里别人能抢先设密码。老 hub.json 没这个字段，行为不变 |
| `machines` | 有 herdr 或 claude 时保留本机 `kind: local`；Docker 里照旧为空 | 和现在一致 |

**Docker / NAS**：不走 `hub setup`。compose 照旧，另一个小 PR 让入口脚本首次生成 `.env` 缺的值（随机库密码、PUID / PGID），`compose.yaml` 映射 TLS 端口（镜像现在只 `EXPOSE 18660`，`Dockerfile:83`）。NAS 的说明里直说「还要再加一台电脑」。

**Mac 一体化**（拍板 1、2 之后）：Persona Link 加「把这台 Mac 设成 hub」，内部调 `personad hub setup`，装好后本机是 `kind: local`，窗口里直接显示 `pair-codes` 返回的 `qr.rows`。和 Mac App `PersonaMac` 合不合成一个下载另行拍板（拍板 3）。

### 4.2 配对

**原则**：首次信任只来自用户亲眼看到的通道：扫屏幕上的二维码、粘贴自己复制的链接、hub 本机终端。不把长期凭据放进任何二维码、链接或日志；只用一次性码（`jp_…`，10 分钟、兑换一次）。

#### 手机 / 网页 ↔ hub

| 场景 | 第一个码从哪来 |
| --- | --- |
| 刚跑完 `hub setup` | 终端里的二维码；或手机浏览器打开安装器打印的 `/admin#setup=…`，设密码后「配对手机」 |
| 已有一台登录好的设备 | 那台设备「设置 › 设备 › 连接新设备」（网页、iOS 已有；Android 本期补） |
| 同一局域网、hub 开了 TLS（P1） | App 首屏「在附近找 hub」，浏览 `_jarvis-hub._tcp` 列出「家里的 hub『xxx』」。**mDNS 只做发现、不做信任**：广播的 `fp` 不能建立首次信任（`docs/network.md:75`），找到后仍要一次性码，价值是告诉用户「hub 已经装好了，去哪出码」 |

网页登录页（`Login.tsx:45-48`）首选「用已登录的手机扫码登录」（现有 `#token=jp_…` 网页登录码），其次「粘贴配对链接」；贴主 token 的框和 `jq` / `--legacy` 收进「高级」，标注「这是 hub 的总钥匙」。同一浏览器重新登录时复用或替换旧设备，设备名带日期，不再累积一串同名的「网页（Chrome · Linux）」。

#### 你的电脑 ↔ hub

本期目标：**在 App 里点「添加电脑」就够了**，不进 `/admin`、不先开 TLS、不先手动添加。

1. 已登录的 full 设备调 `POST /api/v1/auth/pair-codes` 带 `add`（第 5.3 节）。hub 照 `/admin` 添加电脑的同一段逻辑加一条 `kind: link`（抽出函数两边共用，`addMachineLive` 不用重启），返回电脑的配对链接。
2. 这台电脑处于 **待确认（pending）**，直到用户在 App 里点「是我的电脑」。理由：今天 Persona 管理的 API Key 是「给每一个 node」（`assistant/models_launchers.go:25-29`，`pushManaged` 推给每台在线机器，`:166-177`）；Docker hub 没有本机电脑时，`defaultAssistantMachine` 会兜底选第一台机器（`hub/assistant_profile.go:117-118`）。不加确认就放开，一条被转发或截图的链接就等于 Key 外泄、主对话跑在别人的电脑上。pending 的电脑：
   - 不收任何 Persona 管理的 Key（`pushManaged` 过滤）；
   - 不参与助手机器的选择（`defaultAssistantMachine` 跳过它，助手的 `effective()` 也不认它，`assistant/profile.go:139-142`）；
   - 不出现在新建 Agent、派发、待办的电脑列表里；
   - 24 小时没确认自动删除（hub.json 条目和 Postgres 记录一起）。
   确认就在「等这台电脑连上来」那一屏里：连上以后显示「有一台电脑想加入：<名字>（macOS），是你的吗？」，一个按钮，不多一屏。
3. **兼容**：信任状态存 Postgres 新表，**没有记录 = 已确认**，所以升级时已有的机器、`/admin` 加的、回环 + 主 token（`hub pair --machine`）加的都直接是已确认，行为不变。老 App 看到 pending 机器时，机器的 `error` 写「等你在新版 App 或网页里确认」。
4. **看得见的回执**：设备出了电脑码、新设备兑换了 App 码、新电脑接入，都给其他已配对设备推一条通知，带「不是你？撤销」（撤销接口已有）。今天只写 hub 日志（`pairing.go:187`）。
5. 链接交给那台电脑，按系统给一屏：
   - **Linux**：一行命令，链接嵌在里面（`install.sh --node --pair '<链接>'`，已有），「拷贝」+「在那台电脑上打开终端，粘贴，回车」。
   - **Mac**：「下载 Persona Link」（拍板 1 之前链接到 `docs/persona-link-install.md`，如实写「目前要自己编译」）+「把这条链接发给那台电脑」（系统分享）。Persona Link 注册 `jarvis` 和 `persona` 两个 URL scheme，点链接直接打开配对窗口并预填（今天 `apple/PersonaLink/Info.plist` 没有 URL scheme）。hub 照旧发 `jarvis://link?…`（`pairing.go:57`），老 Persona Link 和 node 都认。
   - **Windows**：照 `docs/windows.md` 给命令，说明托盘程序还在做（#299）。
6. `personad hub pair --machine <id>`：没有这台时加 `--add [--name]` 自动添加，不带时提示「加 --add」，不再回 400 `hub.json has no machine …`。Persona Link 配对窗口、`install.sh` 的提示照这个改。
7. 没开 TLS 的老 hub：409 `tls_required` + 动作 `enable_tls`，App 说「hub 还没打开安全连接，电脑接不进来」，受监管时一键打开并重启。
8. **互联里的成员 hub**：App 的码今天会转给中心（`pairing.go:161-164`），电脑怎么办没定。本期成员 hub 上设备发起的添加电脑回 409 `mesh_member`「在中心 hub 上添加」，新手不会走到这里。

P1：**电脑发起、手机允许**。Persona Link 首屏显示一个二维码（含它的一次性公钥和局域网地址），手机扫后经局域网把完整链接交给电脑，不搬长链接。不做「手机出 6 位短码、电脑输入」：短码要靠局域网发现找 hub，mDNS 能被同网段的人伪造。

### 4.3 模型接入

#### 不依赖 zsh

- 引导里不出现「~/.zshrc」。「从 ~/.zshrc 导入」挪进 设置 › 模型供应商 的「高级」，零电脑时禁用并说明（今天会一直转圈）。
- 研发用户已有的 alias / 函数照旧被发现、照旧可选（`node/sessions.go:1897` `mergeLaunchers`），新手向导不依赖它们。
- 默认启动器 `claude` 今天一律经 `zsh -ic` 起（`node/assistant.go:636-638`），PATH 写在 `.zprofile` 的人会 `command not found`。改成优先用 `claudeExecutable()`（`node/managed.go:104`）找到的绝对路径，找不到再走 shell。这块和 #2854（bash 启动器 / 内置 claude，别人认领中）重叠，先在 #2854 下协调，不重复做。

#### 先检测，再问

配对成功后（或主对话首屏缺模型时）先看就绪度（第 5.1 节），用户看到的是结论：

| 检测到 | 用户看到 | 按钮 |
| --- | --- | --- |
| 有在线电脑，Claude Code 已装且登录了 | 「你的 MacBook 上的 Claude Code 能用」 | 「就用它」→ 试一句 |
| 已装、没登录 | 「Claude Code 还没登录」 | 「我有 API Key」/「怎么登录」（图文：在那台电脑上打开「终端」，输入 claude，按提示登录。本期唯一还要终端的一步） |
| 有电脑，没装 Claude Code | 「这台电脑还没装 Claude Code」 | 「怎么装」（官方说明页）；P1「帮我装」 |
| 没有电脑 | 「先添加一台电脑；接上以后它就能动手」 | 「添加电脑」；P1 有 Key 时「先只聊天」 |
| 已有能用的带 Key 供应商 | 跳过这步 | — |

「登录了吗」的免费检测：node 新增一个 RPC，跑 `claude auth status --json`（不发模型请求）。两条注意：

- 必须在**那个启动器真实的环境里**跑：默认启动器经同一个 shell（`zsh -ic`）或绝对路径，Persona 管理的启动器经 `personad launch <名字>` 的环境。实测在空 HOME 下，环境里的 API Key 会让它报 `loggedIn: true, authMethod: api_key`；不在真实环境里跑会得到假的「能用」或假的「没登录」。
- node 里今天没有任何地方用它。按版本判断，老版本没有这个子命令、或输出读不懂时报 `unknown`，不当失败。`authMethod: api_key` 时显示「用的是环境里的 Key」，状态记「未验证」，等试一句。

#### 三条路

**A. 已经有 Claude Code**：不改配置（默认就是 `claude`，`assistant/profile.go:148-150`），直接试一句。P1：由 node 代跑 `claude setup-token` 之类的无头登录，把登录链接推到手机打开，要先实测，并确认订阅登录这样用符合 Anthropic 条款（拍板 10）。

**B. 只有 API Key**：

1. 选一家（内置预设，见下），只粘 Key；名字自动生成，显示名可以是中文。
2. 自动测试（`POST /api/v1/launchers/test`，没填模型时用预设的默认模型，不再报「要填模型」）。失败沿用已有的大白话（401 / 403 / 404 / 429 / 超时，`server/internal/i18n/locales/zh-Hans/assistant-models_probe.json`），加一个「Key 从哪拿」链接。测试会花一点钱，保留现有提示。
3. 保存时带 `assign`（第 5.4 节）：**只有主对话 / Agents 还是默认值（没人改过）时才改**，返回 `assigned`。向导最后一屏写明「主对话和新开的 Agent 都用它」；就绪度显示已有能用的 Claude Code 时，这个开关默认关，不悄悄换掉一个能用的默认。
4. 有电脑但没装 Claude Code：如实说「Key 存好了；还要在电脑上装 Claude Code，主对话才能用它」，因为 `finishModel` 仍然在电脑上起 claude。P1：node 的 `claude.install`（照 `herdrinstall.go` 的下载、校验、装到 `~/.local/bin`、进度回传），只由用户点按钮触发，下载地址只认官方域名，国内网络下载失败报 `download_blocked`。
5. 没有电脑：P1「只聊天」模式（4.4）直接用这个 Key。

**C. 什么都没有**：一屏「去哪弄一个模型」，内置预设按请求语言排序（中文先列国内能直接开通的，其他语言先列 Anthropic），每家一句说明和「去开通」（App 内浏览器打开 `signup_url`），回来进 B。旁边是「先体验一下」。不放推广链接，推荐谁和顺序是拍板 11。

#### 内置预设

- 在 `server/internal/assistant/` 里新放一个嵌入的 presets_builtin.json，结构就是 `assistantwire.LauncherPreset`（`assistantwire/models.go:61-75`），`plugin` 写 `"builtin"`，id 是 `builtin/<厂商>`。加两个可选字段：`signup_url`、`name_zh`（或按 i18n 键）。
- `launcherPresets()`（`models_presets.go:20`）返回「内置 + 插件」，插件同 id 盖过内置；`presetFill` 不改。
- 首批候选：Anthropic 官方、DeepSeek、Kimi（Moonshot）、智谱 GLM、阿里百炼、MiniMax 的 Anthropic 兼容入口。**地址和模型名写稿时没核**，实现时逐家对官方文档、一词请求实测通过才进列表，PR 里附来源；只放厂商公开地址，不放中转站和公司网关。测试保证每项字段齐全。
- 三端向导第一步直接列预设，「自定义兼容网关」放最后；Base URL、鉴权方式只在自定义里出现。向导里写死在客户端的「Anthropic 官方」（`web/src/lib/providers.ts`、`ProviderWizardModel.swift`）改读预设。Android 补「测试连接」「获取模型列表」（今天不调 `/launchers/test`、`/discover`）。

### 4.4 主对话的就绪度和降级

主对话的状态由 hub 算（`GET /api/v1/assistant/readiness` 的 `mode`，第 5.1 节），四端同一套表现：

| `mode` | 条件 | 首屏 | 发送 |
| --- | --- | --- | --- |
| `full` | 有确认过的在线电脑 + Claude Code 能起 + 模型没报错 | 今天的样子；示例问题按已接上的能力给 | 正常 |
| `setup` | 缺电脑、电脑离线、没装 Claude Code、没登录、Key 无效之一 | 问候照常；输入框上方一张卡片，只画第一个缺的那一步（标题 + 一句原因 + 一个按钮）；示例问题换成「Persona 能做什么」「怎么接模型」，点了打开本地帮助，不发给模型；不再说「也能到各台电脑上动手」 | 输入框能打字、草稿保留；发送按钮变成「先接上模型」（或「先添加电脑」），点了进对应那一步。**不排队发送**，免得接好以后冒出一堆迟到的回答 |
| `chat_only`（P1，拍板 9） | 有 hub 能直接调的带 Key 供应商，但没有可用电脑 | 顶部一条常驻提示「现在只能聊天；接上你的电脑后能替你动手」+「添加电脑」 | hub 直接调 API 回答 |

本期 hub 只回 `full` / `setup`；`chat_only` 先写进协议，客户端现在就按「顶部提示 + 照常发送」处理，P1 落地时不用再发版。

失败不再弹 toast，一律是卡片：

- **发出去才发现失败**（就绪度过时）：卡片钉在那条消息下面（第 4.6 节的文案 + 按钮 +「详情」），消息可「重试」。
- **电脑睡着 / 离线**：「小林的 MacBook 现在不在线，可能睡着了。打开盖子就能继续。」不说成连不上 hub（安卓今天会这样说）。
- **模型出错**：「Not logged in」「Invalid API key」这类 Claude Code 合成的回答，不再显示成普通气泡，显示成「模型没接好：没登录 / Key 不对」卡片 +「去修」。要把 `Not logged in` 加进 `APIErrorMarkers`（`assistantwire/wire.go:383-384`），`turn_error_kind` 归现有的 `user`（`proto/proto.go:822-823`，`auth` 是健康检查的分类，不是轮次的 kind）。`CutOff` 同时驱动 hub 会话列表的 `TurnError`、定时任务和待办的处理（`wire.go:380-382` 的注释），所以这个 PR 要带定时任务和待办的测试，不只是主对话的卡片。

**定时任务**：新 hub 默认开着工作日 09:00 的早报（`assistant/schedule.go:212`），零电脑时到点必然失败。改成：就绪度不是 `full` 时跳过，记为「跳过：还没接好电脑或模型」，不推失败通知；就绪后第一次跑之前在主对话里提一句。默认值本身不改，老 hub 不受影响。

**接好以后**：「试一句」成功时，主对话里出一条本地写好的欢迎（不花模型的钱）和三个可点的小任务，按已接上的能力挑：「看看这台电脑上最大的几个文件夹」「今天有什么安排」（点的时候才弹手机日历授权）「教我接邮箱」。

#### 「只聊天」模式（P1，单独一个 issue，拍板 9）

只有 API Key、没有电脑（Docker / NAS / 公司电脑不让装）的人，今天主对话永远是空的。本期给引导卡，不给死胡同里的空白；P1 做一个受限的模式：

- 基于 hub 已有的直接调模型的客户端（`server/internal/ai/ai.go:110` `Complete`，信息处理 / 筛选在用），供应商按 `fallbackAPIProvider` 的规则挑（`assistant/models.go:370`），只用 Messages 协议。
- 纯文本对话，加 hub 本地就能答的只读上下文（接入状态、知识库摘要、收件箱计数）。不调电脑上的工具、不碰手机写操作、不做工具调用循环。
- 对话存 Postgres 自己的表（迁移号实现时看所有 worktree），和 node 上的主对话分开；界面上常驻提示「现在只能聊天」。接上电脑后切回正常主对话，把这段的摘要带进新进程的第一条上下文。
- 提示词放 `server/internal/prompts/` 下新建的 assistant/chat_only 目录，有 golden 测试；被要求做事时回答「接上电脑后我就能做」。

风险（为什么要拍板）：记忆、transcript、权限会分成两份；工作量在存储、流式、客户端渲染。

### 4.5 各端首启引导和空状态

#### 首屏（还没配对）

四端同一结构：

```
Persona
把你的电脑和手机连在一起，交给 AI 去做事。

[ 扫码连接 hub ]                ← 主按钮
在附近找 hub                    ← P1，mDNS
还没有 hub？                    ← App 内浏览器打开「开始使用」（3.3 的选择题）
先体验一下（数据都是编造的）       ← 网页没有体验模式，这一行不显示
其他方式                        ← 手动输入、SSH 隧道、粘贴链接、命令行写法
```

- 主按钮下的小字从「在 hub 所在的机器上运行 `personad hub pair`」改成「装好 hub 时屏幕上会出二维码；也可以在已经连上的设备里打开 设置 › 设备 › 连接新设备」。
- 网页：能打开网页说明 hub 已经在了，只有「用手机扫码登录」「粘贴配对链接」两项，加「还没有 hub？」链接。
- Android：欢迎页去掉 `JCodeBlock("personad hub pair")`，失败文案里「重新运行 personad hub pair」改成「在已登录的设备上重新出一个码」。
- 体验模式退出时落到「还没有 hub？」页。

#### 配对之后：一张清单

照 Android 准备清单（`ReadinessScreen` / `ReadinessRecords.kt`，可跳过、可回来、记住决定）扩到四端，在手机侧项目前面加四项，全部读就绪度接口：

1. **电脑**：有确认过的在线电脑就 ✓；hub 自己就是电脑时自动 ✓；没有显示「添加电脑」；pending 显示「确认是我的电脑」。
2. **Claude Code**：装了没有。
3. **模型**：登录了没有 / Key 能不能用（4.3 的检测结果和三条路）。
4. **试一句**：真跑一轮主对话（发「你好」，走正常的新建主对话），显示回答；失败显示原因和修法。
5. 手机侧（通知、后台、本地网络、蜂窝网络测试……，已有）。

清单不阻塞：用户随时进主界面，主对话的引导卡就是清单的缩略版。用户点「以后再说」存 Postgres（第 5.1 节 dismiss），之后不再弹整页清单，但就绪度变差时卡片照常出现。

#### 空状态改写

| 位置 | 今天 | 改成 |
| --- | --- | --- |
| 总览（`home.json:31` `all_set`） | 零电脑时「一切就绪」 | 「还差一步：添加你的电脑」+ 按钮 |
| 机器页（`machines.json:25`、`MachinesView.swift:98`、Android 同义句） | 「hub 连上后，这里会显示…」 | 「还没有电脑。添加一台，Persona 就能在上面替你干活」+「添加电脑」 |
| Agents | 「在任意一台电脑的 herdr 里启动 Claude…」 | 零电脑时同机器页；有电脑时「新建会话」 |
| 新建 Agent | 电脑一栏空着、「启动」灰着不说为什么 | 说为什么 + 入口 |
| 收件箱（iOS、网页） | 什么都没接也说「都处理完了」 | 「还没接入消息来源」+ 只列点几下就能接的（邮箱、微信） |
| 助手设置「运行在」 | 「每段对话在 的 里后台运行」 | 机器为空时「还没选在哪台电脑上运行」 |
| 模型配置 | 露出 `legacy.kb` / `legacy.triage` | 只显示中文名 |
| 官方扩展市场读不到 | 红字 +「填 token」 | 灰字「暂时读不到官方扩展，稍后再试」，不让新手填 token |
| 形象空状态（网页） | 「形象包放在 hub 机器的 ~/.persona/personas/ 下」 | 去掉路径 |
| iOS 启动器兜底 | 读不到配置时用 `"ccc"` | 显示「正在读取 / 读不到设置」，不显示一个不存在的名字 |

### 4.6 出错文案

原则：

1. 每个失败 = 错误码 + 一句人话 + 一个按钮。原文和请求 id 收进可展开的「详情」，方便截图求助。
2. 客户端按 `code` 出自己的文案和按钮，四端不各写一套原文匹配；hub 的 `error` 字段也给按请求语言本地化的人话，老客户端至少能看懂。
3. 说是哪台电脑、缺什么、点哪里；不说「操作未完成，请重试」，也不把电脑离线说成连不上 hub。
4. 能自己好的（助手进程重启）自动重试，不打扰。

| code | 什么时候 | 大白话 | 动作 |
| --- | --- | --- | --- |
| `no_machine` | 主对话没有可用的电脑（今天 502 `internal`，`assistant/start.go:29,157-158`；`profile.go:465` 的 memory 用同一句英文） | 「Persona 要在你的一台电脑上干活，现在还没接上电脑。」 | `add_computer` |
| `node_unreachable`（已有） | 电脑离线 | 「<电脑名> 现在不在线，可能睡着了或 Persona Link 没开。」 | `wake_computer`（说明页） |
| `assistant_offline`（已有，503） | 助手进程刚重启 | 「助手正在重启，几秒后会自己好。」 | 自动重试 |
| `machine_pending` | 向待确认的电脑派活或选它当助手机器 | 「这台电脑还没确认是你的。」 | `confirm_computer` |
| `claude_missing` | 退出码 127 且命令是 `claude`，或 `claudeExecutable` 找不到 | 「<电脑名> 上还没装 Claude Code。」 | `install_claude`（本期是说明页）/ `connect_model` |
| `launcher_missing` | 127，缺的是 `~/.zshrc` 里的自定义启动器 | 「在 <电脑名> 上找不到「<名字>」这个模型入口，可能被删了。」 | `connect_model` |
| `not_logged_in` | `auth status` 没登录，或回答以「Not logged in」开头 | 「<电脑名> 上的 Claude Code 还没登录。」 | `login_claude`（图文）/ `connect_model` |
| `auth_failed` | 「Invalid API key」「Failed to authenticate」等（已在 markers 里） | 「模型的 Key 不对或过期了。」 | `edit_provider` |
| `model_route` / `rate_limited` | 模型名不对 / 429（健康检查已有分类） | 「这个模型现在用不了 / 用得太快了，稍后会重试。」 | `connect_model` |
| `tls_required`（已有） | 加电脑但 hub 没开 TLS | 「hub 还没打开安全连接，电脑接不进来。」 | `enable_tls` |
| `mesh_member` | 成员 hub 上从设备添加电脑 | 「这台 hub 是组里的成员，到中心 hub 上添加电脑。」 | — |
| `pair_code_expired` / `pair_code_used` | 配对码过期 / 用过（iOS 今天显示英文 `invalid token`） | 「这个码过期了 / 已经用过了，再出一个。」 | 重新出码 |
| `hub_identity_changed` | 钉扎的指纹不符 | 「hub 换了身份（重装或换了电脑）。如果是你重装的，重新扫一次码。」不提供「忽略并继续」 | 重新扫码 |
| `node_service_down`（机器的 `error_code`） | 链路通、常驻 node 没在跑（今天原文 `…node-attach.sock 连不上：jarvis-link node install…`） | 「这台电脑上的 Persona 后台服务没在运行。打开 Persona Link，让它重新装一次。」 | — |
| `link_offline`（机器的 `error_code`） | Persona Link 断开（今天原文 `link closed: failed to get reader…`） | 「这台电脑离线了：Persona Link 没在运行，或者电脑睡着了。」 | — |
| `db_missing`（命令行） | `database_url` 为空 | 「hub 的数据库还没装好。运行 personad hub setup，它会帮你建好。」 | — |
| `port_in_use`（命令行） | 端口被别的程序占着（今天 `hub pair` 会打到另一个 hub 上，回 401） | 「端口 <n> 被别的程序占着，可能是另一个 Persona。」 | 安装器自动换端口 |

HTTP 状态：新码走 `rpc.Error{Code}`，`apierr.Call` 对 `internal` / `node_unreachable` / `timeout` 以外的码回 409（`apierr/apierr.go:117-131`），所以 `no_machine` 等不能复用这三个名字。注意已有的 `node_unreachable` 带码时是 502，只有 `ErrOffline` 是 503。

### 4.7 预设

| 项 | 今天 | 新手默认 |
| --- | --- | --- |
| 主对话模型 | App 存的 > hub.json `assistant.launcher` > `claude`（`assistant/profile.go:139-150`） | 不变。检测到能用的 Claude Code 就用它；引导里接上的 Key 经 `assign` 只在还是默认值时接过去 |
| 主对话权限 | 默认模式：Bash 等工具每次在手机上批准；Persona 自己的工具已用 `--allowedTools` 放行（`docs/assistant.md`「不用批准的工具」） | 不变。改善审批卡片的说明文字，引导里加一句「它在电脑上做事前会先问你」。「读东西不用问」这一档在 Bash 上没法安全表达（只读命令分辨不出来），先在接好电脑的实测里数一下新手第一次会弹几次审批，再决定（拍板 14） |
| 替用户出面的写操作 | 发邮件、手机写、飞书写都由 hub 先问 | 不变（硬边界） |
| Agents 新会话 | 用户选 | 默认「手动确认」；「全部放行」不进任何新手预设 |
| 启动器健康检查 | 默认关；开了以后每 30 分钟真跑一次请求（`launcher_health.go:58`，花钱），修复 Agent 默认 bypass（`:68`，2026-11-05 用户决定） | 不变。就绪度只用免费的检查，不依赖健康检查 |
| 定时任务 | 早报默认开 | 就绪前跳过（4.4） |
| 示例问题 | 网页 4 个、iOS 6 个，各写各的 | hub 在就绪度里给一份（`suggestions`），按已接上的能力挑，四端只渲染 |
| 内置供应商 | 无 | 4.3 的内置预设 |

## 5. 协议（各端的契约，iOS / macOS 照这个实现）

总规矩：

- 新字段都可选，老客户端忽略；新接口在 `hello` / `GET /api/v1/health` 的 `features` 里登记（`hub/extension_api.go:86`），客户端看到才走新流程，连老 hub 时退回现在的页面。本节新登记：`assistant_readiness`、`machines_add`、`machine_trust`、`builtin_presets`。
- 枚举都是封闭的：客户端不认识的 `state` 按「还差一步 + `title`」显示；不认识的 `action.id` 只显示文字、不画按钮；不认识的 `code` 显示 `error`。
- `title` / `detail` / `error` 由服务端按 `Accept-Language` 本地化（`server/internal/i18n/locales/`）。客户端可以用自己的文案覆盖，但必须能只靠服务端的字运行。
- 不放任何密钥；Key 只回 `has_key`。

### 5.1 主对话就绪度

```
GET /api/v1/assistant/readiness            已登录设备（含只读）
GET /api/v1/assistant/readiness?probe=1    同上，另外真跑一次一词检查（花钱，30 秒内重复调用直接回上次结果）
```

挂在助手服务下：它已经知道机器、启动器和模型配置（`assistant/profile.go`、`models.go`），hub 只是转发 `/api/v1/assistant/*`。助手进程没连着时和其他助手接口一样回 503 `assistant_offline`。

```json
{
  "version": 1,
  "mode": "setup",
  "ready": false,
  "next": "model",
  "machine": {"id": "mbp", "name": "小林的 MacBook"},
  "launcher": "claude",
  "steps": [
    {"id": "computer", "state": "ok", "machine": "mbp"},
    {"id": "claude", "state": "ok", "version": "2.1.308"},
    {"id": "model", "state": "problem", "code": "not_logged_in", "machine": "mbp", "launcher": "claude",
     "title": "Claude Code 还没登录", "detail": "在小林的 MacBook 上登录一次，或者改用 API Key。",
     "action": {"id": "connect_model"}, "alt_actions": [{"id": "login_claude"}]},
    {"id": "first_message", "state": "todo", "action": {"id": "test_assistant"}}
  ],
  "suggestions": [
    {"id": "what_can_you_do", "title": "你能帮我做什么？", "local": true},
    {"id": "today", "title": "今天有什么安排", "prompt": "今天有什么安排？", "needs": ["phone_calendar"]}
  ],
  "probe": {"ok": false, "millis": 2140, "error": "…"},
  "dismissed": false,
  "checked_at": "2026-11-06T09:00:00Z"
}
```

| 字段 | 取值 |
| --- | --- |
| `mode` | `full` / `setup` / `chat_only`（P1 才会出现） |
| `ready` | `mode == "full"` |
| `next` | 第一个不是 `ok` / `skipped` 的步骤 id；都好了为 `null` |
| `machine` | 主对话要用的电脑；没有时 `null`。pending 的电脑永远不会出现在这里 |
| `launcher` | 生效的启动器名（`effective()` 之后再盖上模型配置的主对话用途） |
| `steps[].id` | 固定顺序：`computer`、`claude`、`model`、`first_message` |
| `steps[].state` | `ok` / `todo` / `problem` / `checking` / `unknown` / `skipped` |
| `steps[].code` | `state` 是 `todo` / `problem` 时给，取第 4.6 节的码。`computer`：`no_machine`、`node_unreachable`、`machine_pending`、`node_outdated`；`claude`：`claude_missing`、`launcher_missing`；`model`：`not_logged_in`、`auth_failed`、`model_route`、`rate_limited`、`model_unverified`；`first_message`：`never_tried`，或失败时的码 |
| `steps[].action.id` / `alt_actions[].id` | `add_computer`、`confirm_computer`、`wake_computer`、`install_claude`、`login_claude`、`connect_model`、`edit_provider`、`test_assistant`、`enable_tls`、`open_admin` |
| `suggestions[]` | `local: true` 的由客户端打开本地帮助、不发给模型；`needs` 是点了以后才会弹的系统授权（客户端提前说一句） |
| `probe` | 只有 `?probe=1` 时有，即 `POST /api/v1/launchers/{name}/check` 的 `{ok, model, answer, millis, error}`（`assistant/models_launchers.go:748`） |
| `dismissed` | 用户点过「以后再说」 |

判断顺序和数据来源（全是已有的，加一处新的 node RPC）：

1. `computer`：设备列表和在线状态、信任状态（5.3）。电脑 `ok` 之前，后面的步骤是 `unknown`。
2. `claude`：node 自检的 `claude-version`（`node/doctor.go:454`），助手服务缓存 10 分钟。
3. `model`：新 node RPC `claude.auth_status`（4.3，在启动器真实环境里跑 `claude auth status --json`，缓存 10 分钟）；最近一次 `?probe=1` 或「试一句」的结果；主对话最近一轮的 `turn_error_kind`。默认**不主动花 token**；从没验证过时是 `unknown` + `model_unverified`，不算 `problem`，`mode` 照样可以是 `full`。
4. `first_message`：主对话里有没有过一轮成功的回答。它是 `todo` 不影响 `mode`。

存储：最近一次检查结果和 `dismissed` 存 Postgres 的 settings（一个键 `assistant.readiness`），不写文件。

```
POST /api/v1/assistant/readiness/dismiss   {"dismissed": true}  →  {"ok": true}
```

`POST /api/v1/assistant/warm` 保持回 `{"ok": true}`，另外带 `readiness` 字段（同一结构，不 probe）。客户端什么时候重拉：进 Persona tab、配对成功、添加 / 确认电脑之后、保存供应商之后、收到 `machines` 变化时。第一版不加新的推送事件。

老 hub 上这个接口 404：客户端退回现在的行为（首屏不画卡片，失败时按 `code` 出卡片）。

### 5.2 主对话的结构化错误

`POST /api/v1/assistant/conversations` 和会话发送接口失败时：

```json
{"code": "no_machine", "error": "Persona 要在你的一台电脑上干活，现在还没接上电脑。",
 "detail": "assistant machine is not configured", "action": "add_computer", "request_id": "…"}
```

- `no_machine`、`claude_missing`、`launcher_missing`、`machine_pending` 回 409；`node_unreachable` 不变（带码 502，`ErrOffline` 503）；`assistant_offline` 不变（503）。
- `detail` 是原文，客户端只放进「详情」。
- `turn_error_kind: "user"` 的轮次，会话上另带 `turn_error_code`（`not_logged_in` / `auth_failed` / `model_route` / `rate_limited`），客户端据此画卡片。
- `errNoAssistantMachine`（`start.go:29`）和 `memory()`（`profile.go:465`）用同一个码，别让两处分开漂移；语音提示词的 golden（`server/internal/voice/testdata/prompts/report.failed.golden`）跟着更新。
- 老客户端照旧显示 `error`，只是从英文变成中文。iOS 的 `isQuiet` 判断要确认不把 409 当成静默错误。

### 5.3 已登录设备添加电脑、确认电脑

扩展现有的配对码接口，不另开一套：

```
POST /api/v1/auth/pair-codes      full 设备（不是由配对码临时身份发起的），或回环 + 主 token
{"add": {"name": "小林的 MacBook", "os": "mac", "icon": "laptop"}}
或 {"machine": "example-machine"}            已存在的 link 电脑重出码
```

```json
{
  "token": "jp_…", "expires_at": "…",
  "machine": "example-machine", "trust": "pending",
  "pair_url": "jarvis://link?url=…&token=…&machine=example-machine&hub=…&fp=…&tls=…",
  "command": "jarvis-link pair '<pair_url>'",
  "install": {"linux": "curl -fsSL <站点>/install.sh | sh -s -- --node --pair '<pair_url>'"}
}
```

- `add.name` 可以是中文；id 由名字生成（ASCII 化 + 去重，满足 `/admin` 的 id 规则），用户不填「之后不能改」的 id。`add.os` 只决定客户端显示哪种安装说明。
- `add` 时 hub 照 `POST /api/v1/admin/machines` 的 link 分支写 hub.json 并 `addMachineLive`，同时在 Postgres 写信任记录 `pending`。回环 + 主 token 发起的写 `confirmed`（或不写记录）。
- 已存在、从没连上过的同名 link 电脑：复用它，重出码。
- `<站点>` 来自配置，不写死。
- 错误：403（不是 full 设备，或是配对码换来、还没确认的设备）；409 `tls_required`（`action: "enable_tls"`）；409 `mesh_member`；429 `too_many_pending`（同时最多 3 台未兑换或待确认的电脑）。
- 去掉 `pairing.go:132-135` 对设备的限制，但只放开 `add` / 已存在的 link 电脑这两种。

机器列表（`GET /api/v1/machines` 和 hello 里的机器）每台加两个可选字段：

| 字段 | 取值 |
| --- | --- |
| `trust` | `pending` / 省略（= 已确认） |
| `error_code` | `node_service_down`、`link_offline`、`never_connected`、`node_outdated`；`error` 原文保留 |

```
POST /api/v1/machines/{id}/confirm    full 设备   →  {"ok": true}       pending → confirmed，随后推送 Key
POST /api/v1/machines/{id}/reject     full 设备   →  {"ok": true}       删掉 hub.json 条目和记录，吊销它的 link 凭据
```

pending 的电脑：`pushManaged` 跳过；`defaultAssistantMachine` 和助手的机器候选跳过；新建 Agent、派发、待办的电脑列表不列；24 小时未确认自动 reject。出码、兑换、确认、拒绝、超时删除都写审计（Postgres），并给其他已配对设备推送（「<设备> 给电脑出了一个配对码」「新电脑 <名字> 想加入」）。

`personad hub pair --machine <id> [--add] [--name <名字>]`：走回环 + 主 token，`--add` 时没有就添加，加的直接是已确认。

### 5.4 供应商：预设添加和 `assign`

`GET /api/v1/launchers/presets`：返回「内置 + 插件」，按请求语言排序。`LauncherPreset` 加可选字段：

| 字段 | 说明 |
| --- | --- |
| `plugin` | 内置的是 `"builtin"` |
| `signup_url` | 开通 / 买 Key 的页面，客户端用 App 内浏览器打开（只给内置预设填） |
| `display_name` | 本地化的显示名（中文环境「智谱 GLM」之类） |

`POST /api/v1/launchers`（已有）加两处：

```json
{"preset": "builtin/deepseek", "api_key": "…", "name": "", "display_name": "",
 "assign": ["assistant", "agent.new"]}
→ {"launcher": {…, "name": "deepseek", "display_name": "DeepSeek", "has_key": true},
   "assigned": ["assistant"], "kept": [{"purpose": "agent.new", "reason": "customized"}]}
```

- 带 `preset` 时 `name` 可以空：用厂商 slug，重名加 `-2`，满足 `managedLauncherName`（`models_launchers.go:98`，只认 ASCII）；`display_name` 空时用预设的。没有 `preset` 时 `display_name` 也可以填中文。
- `assign`：保存后对列出的用途各 PATCH 一次，**只在该用途还是默认值（没人改过）时生效**，结果分到 `assigned` 和 `kept`。客户端不用自己「先读 revision 再 PATCH」。
- `POST /api/v1/launchers/test`（已有）带 `preset` 时，没填 `model` 就用预设的默认模型。

### 5.5 命令行

| 命令 | 输出 |
| --- | --- |
| `personad hub setup [--json]` | 每步一行中文；`--json` 时一行一个 `{"step","ok","detail","next"}`，最后一行 `{"done":true,"pair_url","admin_url","setup_code","expires_at"}`，给 Persona Link / Mac 一体化调用 |
| `personad hub pair --machine <id> --add` | 同现在，多一行「已添加：<名字>」 |

## 6. 做 / 暂不做 / 要拍板

### 6.1 本期做

- 服务端：主对话的结构化错误码和中文文案；`Not logged in` 进 `APIErrorMarkers`；就绪度接口和 warm 带就绪度；node 上报 Claude Code 装没装、登录没有；内置预设、中文显示名、预设添加名字可空、`assign`；pending 电脑 + Key 只发确认过的 + 回执推送；已登录设备添加电脑、`hub pair --machine --add`；机器的 `error_code`；就绪前跳过定时任务；`personad hub setup`（新文件写 TLS、局域网 `public_url`、`admin.first_setup`）；`db.go`、`install.sh` 结尾、启动日志的中文下一步；扩展市场读不到时降级。
- 网页：主对话引导卡和失败卡片；登录页去命令行、设备去重；空状态；配对后清单；模型向导从预设开始；添加和确认电脑。
- Android：同网页；补连接新设备、测试连接、获取模型列表、内置预设；`operationFeedback` 按 code 出文案（今天全落成「操作未完成」）。
- 部署：Docker 首次启动补 `.env`、映射 TLS 端口。
- 文档和官网：本文；给普通用户的「开始使用」一页（官网 + App 内打开，截图用体验模式）；README 顶部分「我想用」「我想开发」；`persona-link-install.md`、`link.md` 的配对步骤按「App 里添加电脑」改写；官网首屏放真实的获取方式，curl 收进「命令行安装」。
- iOS / macOS / Persona Link：开 issue（第 7.3 节），协议见第 5 节。

### 6.2 暂不做

| 项 | 原因 |
| --- | --- |
| 「只聊天」模式 | P1 单独 issue：新的运行方式，和 node 上的主对话分成两份记忆和权限，要拍板 9 |
| App 里一键装 Claude Code | P1：在用户电脑上执行下载来的安装脚本，要带确认、校验和国内网络的降级 |
| 不开终端登录 Claude 订阅 | 先 spike `claude setup-token` / `claude auth login` 能否无终端完成，结论写进 `docs/lessons/agents.md`；还要拍板 10 |
| App 里 mDNS 发现 hub | P1：排在默认 TLS 之后；只做发现，首次信任仍靠一次性码 |
| 电脑发起、手机允许的配对 | P1：安全设计单独评审；本期复制 / 分享链接够用 |
| 手机出短码、电脑输入 | mDNS 能被同网段伪造，短码会被骗走 |
| Mac 一体化包、Persona Link 安装包 | 要 Developer ID（拍板 1） |
| 内嵌 Postgres / SQLite | 状态都在 Postgres 是仓库约定；打包第三方 Postgres 二进制要评估（拍板 2） |
| 新手默认开健康检查 / 自动修复 | 每 30 分钟真跑请求要花钱；修复 Agent 默认 bypass 是 2026-11-05 的用户决定，不在新手路径里改 |
| 新手预设里的「全部放行」「读东西不用问」 | 前者风险不可接受；后者在 Bash 上没法安全表达（拍板 14） |
| Windows 当 hub、Windows 托盘 | #1179、#299 另有人跟 |
| 云一键模板、出门直连的中继隧道 | 要注册外部服务或运营基础设施（拍板 6、7） |
| hub 迁移工具（Mac 挪到 NAS） | 先让第一次装好 |
| 网页体验模式 | `docs/web.md` 已定不做，网页本身由 hub 托管 |
| 引导里出现互联、被动模式（hub 来连）、ssh、飞书接入 | 新手永远只有一台 hub；飞书对普通用户今天走不通 |
| Gmail 发布方 OAuth | 要 Google 受限 scope 验证（拍板 12）；引导里只推荐 IMAP / 应用专用密码 |

### 6.3 需要用户拍板

| # | 事项 | 为什么要拍板 | 不拍板时的退路 |
| --- | --- | --- | --- |
| 1 | Developer ID 签名 + 公证：Persona Link，以及将来的 Mac 一体化包 | 付费开发者账号和公证流程 | Mac 电脑只能从源码装；官网如实写，B 档验收（8.3）做不到 |
| 2 | 不装 Docker 的机器上的数据库：随包带 Postgres 二进制或 embedded-postgres | 新依赖、包体积（几十 MB）、升级要管数据目录 | 要求装 Docker Desktop / OrbStack，或本机 Postgres |
| 3 | Mac App（`PersonaMac`）和 Persona Link 合成一个下载 | 产品形态 | 两个 App，首屏讲清区别 |
| 4 | Docker 镜像（ghcr）改公开 | 对外发布口径 | NAS 用户要 `docker login` |
| 5 | Android 公开的签名 APK 下载页，或上 Google Play | 签名密钥、商店注册费 | hub 托管 APK（`hub pair --android`） |
| 6 | 云服务器一键模板 | 要注册云厂商的市场账号 | 云服务器上跑同一行命令 |
| 7 | 出门直连：发布方运营的中继隧道 | 新的流量成本和运维 | 局域网能用；出门靠 VPN 或 IPv6 |
| 8 | 新安装默认 TLS 只绑局域网 IPv4（推荐），还是 `:18662` 全开 | 有公网 IPv6 的机器全开时端口直接暴露到公网（有设备 token 和限流兜底，`docs/network.md`） | 按推荐：只绑局域网，引导最后问「出门也要用吗」 |
| 9 | 做不做「只聊天」模式（P1） | 产品决定：Docker / NAS、只有 Key 的人不再是空白；代价是两套主对话 | 这些用户只看到引导卡，必须加一台电脑 |
| 10 | Claude 订阅的无头登录由 node 代跑并把 token 存进 hub | 是否符合 Anthropic 条款 | 只走 API Key 一条免终端的路 |
| 11 | 「去哪弄模型」推荐哪几家、排序、要不要推广链接；Anthropic 官方放不放第一位（国内可能直连不了） | 商业和合规判断 | 按语言排序列几家，不写推荐语和价格 |
| 12 | Gmail 由发布方提供经验证的 OAuth 客户端 | Google 受限 scope 验证 / CASA，可能付费 | 只推荐 IMAP / 应用专用密码 |
| 13 | 界面里 hub 的叫法：保留「hub」、「主机」还是「中枢」 | 全局术语 | 保留「hub」，首次出现带一句解释 |
| 14 | 新手要不要一个「读东西不用问」的权限档 | 放宽安全默认；Bash 上分辨不了只读命令 | 默认模式，先在实测里数审批次数 |
| 15 | 官网、TestFlight 说明改成面向普通用户（FAQ 去掉「写信给我们」，放公开链接） | 对外口径 | 只改 README 和文档 |
| 16 | 真人测试：谁去招人、录屏同意书模板 | 要找外部的人 | 只做自动化和内部走查 |

## 7. 分工和分步计划

### 7.1 各端分工

| 端 | 本期内容 | 在哪做 |
| --- | --- | --- |
| 服务端 | 错误码、就绪度、node 的 Claude Code 状态、预设和 `assign`、pending 电脑和 Key 过滤、添加电脑、回执推送、定时任务门槛、`hub setup`、命令行文案 | Linux 开发机，连真实 Postgres 跑测试 |
| 网页 | 引导卡、失败卡片、空状态、登录页、设备去重、清单、模型向导、添加 / 确认电脑 | Linux 开发机，Playwright 截图 |
| Android | 清单加四项、按 code 出文案、首屏、连接新设备、测试连接、内置预设、添加 / 确认电脑 | Linux 开发机；没有 KVM 时 PR 里写明未截图 |
| iOS / macOS / Persona Link | 第 7.3 节的 issue | Mac |
| 部署 | Docker 入口脚本和 compose | Linux 开发机 |
| 文档 / 官网 | 开始使用、README、Persona Link 配对步骤、官网首屏和 FAQ（FAQ 等拍板 15） | Linux 开发机，发布按 `docs/site.md` |

### 7.2 PR 顺序

每个 PR 先建 issue（`Refs #287`），一个 PR 一个 issue，按下面的顺序；前面的单独合进去就有用。

| # | 标题（PR 用） | 端 | 内容 | 单独合进去的价值 |
| --- | --- | --- | --- | --- |
| 1 | fix(assistant): say why the main conversation can't start, with a code | server | 5.2 的码和中文；`Not logged in` 进 markers 归 `user`，带定时任务和待办的测试；`start.go` 和 `memory()` 统一；golden 更新 | 老客户端立刻从英文 502 变成中文 409；后面各端按码出卡片 |
| 2 | test: guard onboarding copy and add readiness scenarios to the mock hub | scripts, mock | 8.1 的文案守门测试；`mock_hub.py` / `mock_scenarios.py` 加七种就绪状态和每个错误码的场景 | 之后每个界面 PR 都有守门和截图数据 |
| 3 | feat(node): report whether Claude Code is installed and signed in | server (node) | `claude.auth_status` RPC（启动器真实环境、版本判断、`unknown` 兜底）；默认启动器优先用绝对路径（先在 #2854 协调） | 就绪度能免费判断登录 |
| 4 | feat(assistant): readiness of the main conversation | server | 5.1 全部；warm 带 `readiness`；`features` 登记；`docs/protocol.md` 补接口 | 四端和 iOS issue 的唯一数据源 |
| 5 | feat(web): show what the main conversation still needs | web | 引导卡、发送按钮、失败卡片代替 toast、4.5 的空状态 | 网页新用户第一次不再「看着能用、发了才错」 |
| 6 | fix(web): sign in without commands and keep one device per browser | web | 登录页改写；重新登录复用 / 替换旧设备 | 网页第一屏不再教 `jq` |
| 7 | feat(assistant): built-in provider presets and assign-if-default | server | `presets_builtin.json`（逐家实测）、`signup_url`、`display_name`、名字可空、`assign`、test 带默认模型 | 「只有 Key」三步接上 |
| 8 | feat(web): provider wizard starts from presets | web | 向导第一步列厂商、只粘 Key、自动测试、`assign`；「从 ~/.zshrc 导入」挪进高级 | 网页模型向导对新手可用 |
| 9 | feat(hub): pending machines get no keys until confirmed | server | 信任表（没有记录 = 已确认）、`pushManaged` / 助手机器 / Agent 列表过滤、confirm / reject、24 小时清理、机器的 `trust` / `error_code` | 放开设备添加电脑之前的安全前提；单独合也没有行为变化 |
| 10 | feat(hub): a signed-in device can add a computer | server | pair-codes 的 `add`（抽出 admin 的添加函数）、`hub pair --machine --add`、回执推送、`mesh_member`、`too_many_pending` | 加电脑不再需要 /admin 和设置码 |
| 11 | feat(web): add and confirm a computer from the machines page | web | 4.2 的页面，复用 `connect-device.ts` 的倒计时和轮询；确认在「等它连上来」那一屏 | 网页用户全程不碰 /admin |
| 12 | feat(assistant): routines wait until the main conversation is ready | server | 就绪前跳过、记「跳过」、不推失败 | 零电脑的新 hub 不再到点报错 |
| 13 | feat(hub): personad hub setup | server, scripts | 4.1 全部；`install.sh --hub` 调它；`db.go:37` 改文案；`newInstallConfig()` 和老文件加载的 golden | 官网「一行命令」第一次真的能装好 hub。可以和 5–12 并行 |
| 14 | feat(docker): first start fills .env and maps the TLS port | deploy | 入口脚本、compose | NAS 少两步手工 |
| 15 | feat(android): onboarding checklist, coded errors and first screen | Android | 清单加四项、`operationFeedback` 按码、首屏去命令、空状态、引导卡 | Android 和网页对齐 |
| 16 | feat(android): presets, connection test, connect a device, add a computer | Android | 模型向导、连接新设备、添加 / 确认电脑 | 同上 |
| 17 | docs: getting started for people who don't use a terminal | docs, site | 开始使用、README、Persona Link 配对步骤、官网首屏 | 广告进来的人有一页看得懂的说明 |

P1 各开一个 issue：「只聊天」模式（拍板 9 之后）、node 的 `claude.install`、无头登录 spike、App 内 mDNS 发现、电脑发起的配对、Mac 一体化（拍板 1、2 之后）。

### 7.3 派到 Mac 上做的 iOS / macOS 工作项

每个 issue 引用第 5 节的协议，写明「Linux 开发机 编不了 Swift；Xcode 26.0 未验证时在 PR 里注明」。界面只用设计系统（`.surface`、`EmptyStateView(action:)`、`Theme.*`），`TokenRatchetTests` 不升基线；按钮单行；深浅色截图。

| # | 标题 | 改什么 | 验收 |
| --- | --- | --- | --- |
| M1 | iOS / Mac：主对话按就绪度显示缺什么，失败用卡片 | 读 `GET /api/v1/assistant/readiness`（依赖 PR 4，看 `features` 的 `assistant_readiness`）；输入框上方的引导卡、发送按钮「先接上模型」、草稿保留不排队；5.2 的错误码画卡片，原文进「详情」；`turn_error_code` 的气泡换成卡片；去掉 `"ccc"` 兜底 | 零电脑、电脑离线、pending、没装 Claude Code、没登录、Key 错、一切正常七种状态（mock hub 场景）各截深浅色；首屏不出现英文原文；409 不被 `isQuiet` 吞掉；`AssistantView` 的模型有 FakeHub 单元测试；老 hub（404）上行为和现在一样 |
| M2 | iOS / Mac：首屏和配对后清单 | WelcomeView 改成 4.5 的结构，命令行收进「其他方式」；配对成功后复用 `pairingEngineMachine` 的插入机制加电脑 / Claude Code / 模型 / 试一句四项，可跳过、可回来，「以后再说」调 dismiss；体验模式退出落到「还没有 hub？」 | 首屏和清单里不出现 `personad`、token、hub.json（文案守门测试覆盖 iOS 字符串）；四项各状态截图；跳过后设置顶部有「还有 N 步」；Mac 上粘贴链接照常配对 |
| M3 | iOS / Mac：添加和确认电脑 | 机器页、Agents 空状态、清单的「添加电脑」→ 选系统 → 名字（可中文）→ 调 pair-codes 带 `add`（依赖 PR 10）→ Linux 命令拷贝 / Mac 分享链接 → 倒计时等上线 → 同一屏「是我的电脑」调 confirm；机器列表显示 `trust` 和 `error_code` 的中文；收到「新电脑想加入」推送能直接确认或拒绝 | 从 0 台到 1 台已确认在线全程不出现 hub.json、TLS、node；过期能重出码；pending 电脑不出现在新建 Agent 里；拒绝后列表里消失；老 hub（不支持 `add`）上入口不显示 |
| M4 | iOS / Mac：模型向导从预设开始 | ProviderWizard 第一步读 `GET /api/v1/launchers/presets` 的厂商列表（去掉写死的「Anthropic 官方」），只粘 Key，自动测试，保存带 `assign`；就绪度显示有能用的 Claude Code 时「设为主对话」开关默认关；「我已经在用 Claude Code」走 `?probe=1`；「去哪弄模型」用 App 内浏览器开 `signup_url` | 三条路各截图；测试失败的 401 / 403 / 404 / 429 / 超时文案对照 `assistant-models_probe.json`；中文显示名保存成功；已改过主对话模型时保存后不被覆盖（`kept` 里有它） |
| M5 | Persona Link：点链接就能配对，不再要求运行命令 | 注册 `jarvis` 和 `persona` URL scheme，点 `jarvis://link?…` 打开配对窗口并预填；配对窗口文案改成「在手机 App 的添加电脑里拷贝这一段」；首屏状态页：未配对 / 已连上 / 等你在手机上确认 / 后台服务没在跑（大白话） | 从备忘录、AirDrop、邮件点链接能拉起配对；pending 时显示「在手机上点确认」；未签名构建上的行为在 PR 里写明 |
| M6 | iOS / Mac：配对和接入的回执通知 | 「<设备> 出了一个配对码」「新设备 / 新电脑接入」通知（依赖 PR 10 的推送），带「不是你？撤销」 | 推送到达后点撤销能吊销对应设备或拒绝电脑；通知文案不含 id 和地址 |

## 8. 验证

### 8.1 自动化

- **服务端单测**（Linux 开发机 连真实 Postgres，`go test -p 1 ./...`）：
  - 老 hub.json 加载后的配置和改动前一致（golden，含只有 `database_url` 的 db.sh 文件、没有 `public` 段的手写文件）；`newInstallConfig()` 带 TLS、局域网 `public_url`、`admin.first_setup`。
  - pending：设备码加的电脑是 pending，收不到 Key，不会被选成助手机器，不在新建 Agent 列表里，24 小时清理；升级前的机器、`/admin` 和回环加的都是已确认；手表、link、配对码身份不能出电脑码。
  - `assign` 只改还是默认值的用途；预设添加名字可空、重名加后缀；内置预设字段齐全。
  - 就绪度：每种状态一个用例；默认不发模型请求；`?probe=1` 30 秒内复用结果。
  - `Not logged in` 进 markers 后，定时任务、待办、会话列表的 `TurnError` 的行为有测试。
  - `no_machine` 等码回 409，`error` 是中文。
- **文案守门**：在 `scripts/test/` 里新加 test_onboarding_copy.py，扫引导、空状态、错误码对应的文案（网页 i18n、Android `values-zh`、iOS 字符串、服务端 i18n），禁止出现 `hub.json`、`token`（「配对码」不算）、`jq`、`personad`、`TLS`、`node`、`herdr`、`launcher`、`localhost`、`~/.`。「高级」「详情」里的键在白名单里。和 `privacy_check.py` 一样进测试。
- **故障场景**：`mock_hub.py` / `mock_scenarios.py` 加七种就绪状态（零电脑、离线、pending、没装 Claude Code、没登录、Key 错、一切正常）和第 4.6 节每个码的错误，网页 Playwright、Android `android.sh shots`、iOS 截图共用一套。
- **全新用户端到端**（Linux 开发机）：隔离测试 hub（自己的端口、数据目录、数据库，`JARVIS_LOCAL_NODE=0`）上，Playwright 走网页：登录 → 引导卡出现 → 用假网关加一个预设供应商 → `assign` 生效 → 就绪度的 `model` 变化。断言首屏和引导里没有文案守门禁止的词。
- **升级回归**：拿一份手写 hub.json、`~/.zshrc` 启动器和带 Persona 管理供应商的库，升级后 Key 照常下发、机器都已确认、主对话照常；`./scripts/test/test-server.sh` 全过。

### 8.2 截图

每个界面 PR：上面每种状态深浅色各一张，网页另加 390 宽。Android 在有 KVM 的机器上补截，Linux 开发机 截不了时 PR 里写明。iOS / Mac 按 7.3 的验收在 Mac 上截。

### 8.3 找非技术用户实测

**两档验收**：

- **A 档（本期能达成）**：一台常开的 Linux 小主机或 NAS + 手机。测试员照官网截图粘一行命令（hub 机器上唯一的一次），其余全在 App 里。
- **B 档（拍板 1、2 之后）**：干净的 Mac + iPhone，只下载 App，不碰终端。#287 原始的「10 分钟」验收属于这一档。

**步骤**：

1. **纸面原型先行**：把引导的截图串成可点的点击稿（Wizard of Oz），找 2 个人走一遍，先改文案再写界面。
2. **招 5 个人**：没用过终端，不做研发、产品、测试，有自己的手机。干净的系统用户、卸载重装的 App。
3. **只给目标、不给步骤**：场景卡写「让 Persona 告诉你这台电脑上最大的几个文件夹」。A 档另加「再接上你的笔记本」。模型给一张有余额的测试 Key，或让他自己去开通（看「去哪弄模型」页能不能用）。
4. **网络分两种**：家里 Wi-Fi；最后切到蜂窝网络再问一句（检验出门在外的提示是不是说清楚了）。
5. **故障注入**：每人随机抽 1 个，由观察员事先制造：电脑睡眠、配对码过期、Key 填错、Claude Code 没登录、手机拒绝本地网络权限。看错误卡能不能让人自己修好。
6. **观察员只看不帮**：被问时只说「你觉得应该怎么做」；卡住超过 3 分钟才给提示，记一次「求助」。
7. **记录**：完成与否、总时长、每一步时长、求助次数、看到的每张错误卡、说出的困惑词（「hub 是啥」）。录屏要签同意书，不进仓库；匿名化的结论写进 `docs/archive/onboarding-usability-<日期>.md`。
8. **同时数一件事**：第一次让主对话动电脑时弹了几次审批（决定拍板 14）。

**通过线**：5 人里至少 4 人 15 分钟内独立完成 A 档；没人看到英文原文；求助总数不超过 3；每个故障场景都有人靠卡片上的按钮自己修好。没过的步骤回到纸面原型重改。

## 9. 兼容

- 新默认只写进新生成的 hub.json；已有文件一个字段不改，`defaultConfig()` 不加新默认（4.1）。
- `deploy.sh`、`db.sh`、`hub-systemd.sh`、socket activation、手写 hub.json 照旧；`hub setup` 建的库容器和 `db.sh` 同名同卷。
- `~/.zshrc` 启动器照旧被发现、照旧能选；新手向导只是不提它。
- `jarvisd` 命令名、`jarvis://pair` / `jarvis://link` 链接、`JARVIS_*` 环境变量照旧；Persona Link 两个 scheme 都认。
- 错误从 502 `internal` 变成 409 + code：老客户端都把非 2xx 的 `error` 当文本显示，只是语言变了。
- 老 node、老 Persona Link 不需要懂 pending，过滤全在 hub 和助手服务里。
- 新接口在 `features` 里登记；老 hub 上新客户端退回现在的页面。
- 动 Postgres 的 PR（信任表、就绪度键不需要新表）挑迁移号前先看所有 worktree。
