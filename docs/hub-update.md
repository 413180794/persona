# hub 的版本、检测更新和自动更新

用户 2026-11-02：「手机端能不能管理 hub 的版本，hub 是否有检测更新，自动更新的能力？……App 进入的时候，提示一键更新 hub，或者 hub 自动更新」。
issue #284。方案已拍板（见文末「拍板」）：skill 从二进制里拿出来，只有一种二进制，所有 hub 都自动更新到发布者手动发布的版本，不做灰度、不做自动发布。
分 4 步实现，做完一步把对应的「方案」改成现状，保留取舍。

## 实现进度

- **第 1 步（一种二进制和版本清单）已做**（这一节以下的「现状」是开工前的样子）：
  - skill 拿出二进制：`internal/assistantskills` 只放内置 skill（现在为空）；node 每次起助手时 `Sync`：链接 `~/.jarvis/skills/` 里用户的 skill，
    删掉失效的链接，旧版本装的 lark-connector 自动搬过去（`TestSyncMovesTheOldCopy`）；`ol` 只在有 lark-connector 时出现（`TestOlOnlyWithTheSkill`）；
    助手的 CLAUDE.md 在没有 lark-connector 时只讲 MCP 的 `lark_*`（golden `assistant_claude_md.no_lark-connector`），hub 问 node 的 `skills.list`；
    `GET /api/v1/skills` 给 App 的只读列表。lark-connector 的副本挪到仓库的 `skills/lark-connector`。
  - 一种二进制：没有按 `public` 区分的文件了（`TestOneBuild`）；publish.sh 留着 `-tags public` 只当隐私扫描的标记（扫描靠它认出可以跳过的依赖常量）。
  - 渠道和 build 号：`main.channel`、`main.build`、`main.updateURL`；publish.sh、deploy.sh 打 `release`，Docker 打 `docker`；
    `jarvisd version -v` 显示渠道和 build 号；`/api/v1/health`、`hello` 带 `channel`、`build`、`protocol`、`min_app_build`。
  - 版本清单：`internal/update`（清单、`CheckNext`、不降级 `TestNoDowngrade`），`jarvisd release manifest|check-next`；
    publish.sh 发 `latest.json`（各文件的 sha256），发布前检查 build 号递增、线上那版的 commit 是这次的祖先。
    第 1 步里原有 ed25519 签名，用户改主意后在第 2 步删掉（见「安全」）。
- **第 2 步（hub 更新和回滚）已做**：`hub/selfupdate.go`（检测、下载校验、等空闲和「现在就更新」、替换、确认或回滚、坏版本、更新记录、node 跟着更新）、
  `internal/update` 的下载 / 替换 / 看门狗、`jarvisd update-watch`；接口见第 12 节。
- **第 3 步（App）已做**：`apple/Shared/HubUpdate.swift`（状态、记录、Skills、兼容判断 `HubCompat`、提示条选哪一条 `HubNotice`）、
  `Views/Settings/HubUpdateViews.swift`（设置 › hub、提示条、Skills 页）；hub 在自动更新装好、回滚后推一条普通通知。第 10 节已改成现状。
- **第 4 步（Docker 检测和提示）已做**：Docker 的 hub 每 6 小时问一次 registry 它的镜像有哪些标签（`internal/update/image.go`），
  最新的正式版本比自己新就提示，App 里给出要运行的命令（钉了版本的先改 `.env`）。见「Docker」一节。

## 现状（2026-11-02 核实）

- 版本：`jarvisd version` 是 `-X main.version` 写进去的字符串。
  - `deploy.sh`、`publish.sh`：`git describe --always --dirty` 加上分钟级时间，形如 `v0.1.1-23-gabc1234-202611021530`；
  - Docker 镜像：推 `v*` tag 时由 CI 构建，版本就是 tag 去掉 `v`。
- hub：
  - `GET /api/v1/health` 返回 `{ok, version, features}`，WebSocket 的 `hello` 带 `server_version`；
  - node 版本和 hub 不同时机器列表标出来，可以「重新部署」（`hub/deploy.go`，用 hub 机器上 `~/.jarvis/bin/nodes/` 里的二进制）。
- 完整版和公开版**唯一的区别**是 `-tags public`：
  - 完整版用 `go:embed` 把 lark-connector skill 编进二进制（`internal/assistantskills`，从上游拷来，不能再分发）；
  - node 起助手进程时把它装到助手目录的 `.claude/skills/`，并把 `ol` 放进 PATH（`node/larkgate.go`）；
  - 公开版不带。
- 发布：`publish.sh` 构建四个平台的公开版，gzip 后连同 `SHA256SUMS`、`VERSION` 用 wrangler 发到 `publish.site_url` 的 `dl/`（Cloudflare Workers 静态资源），
  只保留最新一版。wrangler 用的是 OAuth 登录，大约一小时过期，没法无人值守。
- `install.sh` 只放二进制（`~/.jarvis/bin/jarvisd`），不装服务；装出来的 hub 机器上没有 `~/.jarvis/bin/nodes`，所以公开版点「重新部署」会报错。
- 我们自己的 hub 用 ssh 部署（`deploy.sh`，以及主会话的部署脚本），门槛有三条：
  - 在 hub 机器上对真实 Postgres 跑完全部测试；
  - 助手有一轮在跑就等；
  - 保留上一版，重启后各机器都连上才算完。
- App：设置里只显示 App 自己的版本；机器详情里有 node 的版本。没有任何检测更新或自更新。

## 原则

1. **只有一种二进制**：skill 不在二进制里，所有 hub（包括我们自己的）用同一个发布版，都能自动更新。
2. 用户的 skill 放在自己的空间里，更新永远不碰它。
3. 发布者手动发布（`publish.sh`），各个 hub（包括我们的）空闲时自己更新，再更新自己的 node。急需 master 改动时可以直接部署，不会被旧的公开版降级。
4. 装之前核对 sha256、新二进制自检；任何一步不对就不动现在的二进制。
5. 有对话在跑或在等回答就不重启。
6. 新版本起不来、机器连不上就自动回到上一版；回滚过的版本不再自动重试。
7. 不做灰度：发布前的门槛必须全过，发布后所有 hub 同时更新，各自出问题各自回滚。

## 1. skill 从二进制里拿出来

- **用户的 skill 空间**：跑助手的那台机器上的 `~/.jarvis/skills/<name>/`。
  - 每个目录是一个 skill（里面有 `SKILL.md`），用户自己放、自己改，jarvisd 只读不写；
  - 跑助手的通常是 hub 机器的本机 node，助手配置到别的机器时就是那台。
- **同步到助手目录**：node 每次起助手进程（Claude 和 Codex 的助手都算，`/agent`、AI 分身用的也是助手目录）时：
  1. 对 `~/.jarvis/skills/` 下每个 skill，在 `<助手目录>/.claude/skills/<name>` 建符号链接。
     用户改了 skill，下一次起助手就生效；Claude Code 读 skill 只读文件，符号链接没问题。
  2. 删掉指向 `~/.jarvis/skills/` 但已经没了的链接；用户直接放在 `.claude/skills/` 里的真目录不碰。
  3. 二进制里自带的内置 skill（只放我们能再分发的，现在是空的）照旧按版本戳复制进去；用户空间里同名的优先。
  4. `ol` 只在装了 `lark-connector` skill 时才放进 PATH；助手的提示词（`prompts/assistant/claude_md.md`）里关于 lark-connector 的那段，也只在装了时才写进去。
- **迁移**：两种方式，都只做一次。
  - **自动**：node 起助手时，`~/.jarvis/skills/lark-connector` 不存在，而 `<助手目录>/.claude/skills/lark-connector` 是旧 jarvisd 装的真目录（旁边有 `.jarvis-skills` 版本戳），
    就把它搬到 `~/.jarvis/skills/lark-connector`，原地换成符号链接，打一行日志。
  - **手动**（我们的 hub 上，主会话执行）：
    `mkdir -p ~/.jarvis/skills && cp -R <仓库>/skills/lark-connector ~/.jarvis/skills/lark-connector`，
    或者从助手目录的 `.claude/skills/lark-connector` 拷过去。
  - 这样新版本起来时助手照常有 lark-connector。lark-connector 的副本挪到仓库的 `skills/lark-connector`（同步上游的脚本照旧，改成写那里），不进任何二进制；
    公开版没有它也不要紧，用户把它（或自己的 skill）放进 `~/.jarvis/skills/` 就行。
- **去掉 `-tags public`**：两边没有区别了，`files_public.go` / `files_bundled.go` 合成一个只放内置 skill 的 embed。
  - 发布前的隐私扫描照旧，还要检查二进制里没有 `name: lark-connector`；
  - 测试守住：内置 skill 目录里只能有登记为可再分发的 skill。
- **App**：设置 › 助手 › Skills，只读列表（名字、`SKILL.md` 里的说明、来自「我的 skill」还是「内置」）。
  接口 `GET /api/v1/skills`，hub 问跑助手的那台 node 拿。管理（增删改）以后再说。

## 2. 构建渠道

二进制里多三个 `-X` 变量（`cmd/jarvisd`）：

| 变量 | 值 |
| --- | --- |
| `main.channel` | `release`（发布的，会自动更新）、`docker`（镜像里的，只提示拉新镜像）、`dev`（不写时的默认：本地构建、测试） |
| `main.build` | 单调的 build 号，见「版本号」 |
| `main.updateURL` | 发布地址的 `dl/`（`publish.site_url`，本来就公开）。没写就不检查 |

- 只有 `release` 会自己换二进制；`docker` 提示用户拉镜像；`dev` 只显示版本。
- 直接部署（`deploy.sh` 和主会话的部署脚本）也打 `release` 和编译时间做 build 号：比当时的公开版新，不会被降级；发布者发了更新的版本，hub 自己接手（第 6 节）。
- `/api/v1/health` 和 `hello` 多带 `channel`、`build`、`protocol`。

## 3. 版本号：发布时间做 build 号

- 比较新旧用**发布时间**（UTC，`YYYYMMDDHHMM`，整数），版本字符串照旧，只给人看（用户 2026-11-02 拍板）。
- 理由：
  - 每个版本字符串末尾本来就是这个时间，不用新增要维护的计数器，也不要求每次都打 tag；
  - 只有一个发布方，时间天然递增；
  - 不用人判断这次算 minor 还是 patch。
- 防止不单调：`publish.sh` 读线上的清单，两种情况拒绝发布：
  - 新 build 号不比线上的大；
  - 线上那版的 commit 不是这次的祖先。
- Docker 沿用镜像 tag 的 semver，只在 docker 渠道里比较。

## 4. 版本清单

`dl/` 里多一个文件 `latest.json`：

```json
{
  "version": "v0.1.1-30-gabc1234-202611021530",
  "build": 202611021530,
  "commit": "abc1234…",
  "released": "2026-11-02T07:30:00Z",
  "min_app_build": 95,
  "min_node_build": 202611010000,
  "protocol": 3,
  "notes": "这一版的要点",
  "files": {
    "linux-amd64": {"name": "jarvisd-linux-amd64.gz", "size": 12345678, "sha256": "<压缩包的>", "binary_sha256": "<解压后的>"},
    "linux-arm64": {}, "darwin-arm64": {}, "darwin-amd64": {}
  }
}
```

- 下载的文件要同时对得上 `sha256`（压缩包）和 `binary_sha256`（解压后），新二进制的 `version -v` 还要报出清单里的版本和 build 号。
  `SHA256SUMS` 照旧给 install.sh 用。
- `min_app_build`：低于它的 App 用不了这版 hub。`min_node_build`：低于它的 node 要先更新（hub 会自动重新部署）。`protocol` 见「兼容」。

## 5. 发布（手动，publish.sh；用户 2026-11-02）

不做自动发布：发布者照旧在自己的开发机上手动跑 `publish.sh`，网站的 Worker 在发布者的 Cloudflare 账号下。
- 推送中继（`docs/push-relay.md`，`/relay/*`）和下载包在同一个 Worker 里，`publish.sh` 每次重新部署网站会把中继一起部署
  （Worker 部署是原子切换，不会半新半旧；中继代码有改动时也随这次发布上线）。
- 发布前照旧要过测试，加上 `publish.sh` 自己的检查（任何一项不过就不发布）：
  1. 读线上的 `latest.json`（有的话）：新 build 号必须更大，线上那版的 commit 必须是这次的祖先（`git merge-base --is-ancestor`）；
  2. 构建四个平台（`channel=release`、`build`、`updateURL` 都写进去），隐私扫描（二进制里不能有 lark-connector）；
  3. 生成 `latest.json`（版本、build 号、commit、发布时间、各平台压缩包和解压后的 sha256、`min_app_build`、`min_node_build`、`protocol`）；
  4. 发布后读回 `VERSION`、`SHA256SUMS`、`latest.json`，和本地的比一遍。

### 安全：不签名（用户 2026-11-02 拍板）

用户：「为啥还要签名，Linux 服务就不签了吧，搞太复杂」。取舍写清楚：
- **防的**：下载传坏、被截断（sha256 对不上就删掉、不装），以及装上一个跑不起来或不是这个版本的二进制（`version -v` 自检，起来后还要过确认，不行就回滚）。
  下载走 HTTPS，传输途中被篡改要先攻破 TLS。
- **不防的**：发布站点（`publish.site_url` 的 Worker、发布者的 Cloudflare 账号）被冒充或被入侵。那样攻击者可以同时换掉二进制和 `latest.json` 里的 sha256，
  所有开着自动更新的 hub 都会装上，而且是以运行 hub 的用户身份执行。
- 用户知情，决定不做签名。以后要加：在清单旁边放一个签名、公钥编进二进制，hub 只装验签通过的（第 1 步曾经这样实现过，见 git 历史）。
- 不想承担这个风险的 hub：在 App 里把自动更新关掉，或者用 Docker 部署（拉镜像时自己决定）。

## 6. 直接部署（急需 master 改动时）

原来从 master 编、直接部署的那套（`deploy.sh`，和主会话的部署脚本）保留，用在等不及发布的时候。
- 编出来的二进制也写 `channel=release`、`build`（编译时间，UTC `YYYYMMDDHHMM`）和 `updateURL`：
  - 它比当时线上的公开版新，hub 自动更新看到旧的公开版不会装，**不会降级**；
  - 等发布者发了更新的版本（build 号更大），hub 再自己接手；
  - `TestNoDowngrade` 守住：清单的 build 号不比自己大就不装，等于也不装。
- 主会话的部署脚本在仓库外，要在它的 `go build` 里加上同样的三个 `-X`（回报里会给出要加的那一行）。
- 不做灰度：发布后所有 hub（包括我们的）在空闲时同时更新；出了问题靠各 hub 自己的回滚和坏版本标记。

## 7. 检测更新

- **发布版**：
  - hub 启动后 1 分钟检查一次，之后每 6 小时；App 可以「立即检查」（`POST /api/v1/hub/update/check`）；
  - 只取清单（几 KB）：build 号比自己大、不在坏版本列表里，才算有更新；结果广播给开着的 App。
- **Docker 版**：
  - 6 小时一次，查镜像仓库的 tag（GHCR 匿名的 `tags/list`），取最大的正式 semver，比自己大就提示；
  - hub 不装任何东西，拉镜像的是 Docker，按 digest 校验；
  - 镜像名由 CI 构建时写进二进制，代码里不写死。
- **dev**：不检查，只显示版本。
- 网络不通、清单坏了：记日志，App 里「检查更新」显示原因，不重试轰炸（照 6 小时）。

## 8. 更新（发布版；自动和手动是同一个流程）

```
下载 → 校验 → 准备 → 等空闲 → 替换 → 重启 → 确认 → （失败）回滚
```

1. **下载**：
   - 下到 `~/.jarvis/update/<build>/`：hub 自己平台的压缩包，加上当前连着的各台机器平台的 node 二进制（node 跟着一起更新用）；
   - 断了重下，最多 3 次；临时文件下完才改名。
2. **校验**：
   - 两个 sha256 都对上清单才继续，不对就删掉下载、报错，现在的二进制不动；
   - 解压后跑一次 `<新> version`，必须报出清单里的版本和 `release` 渠道。
3. **准备**：
   - node 二进制放进 `~/.jarvis/bin/nodes/`：公开安装的 hub 也就能「重新部署」了；
   - 写 `~/.jarvis/update/pending.json`：从哪版到哪版、上一版的路径、开始时间、重启前在线的机器、启动次数。
   - 这是「服务端状态都进 Postgres」的唯一例外：回滚要在新版本连不上数据库、甚至起不来时也能做，所以标记放在二进制旁边，更新结束就删。
     更新记录和坏版本仍然进 Postgres。
4. **等空闲**（每 30 秒看一次；正在重新部署某台机器时也不重启）：自动和手动一样，**助手和所有 Agent 都空闲才重启**，不限时段（用户 2026-11-02）。
   - 空闲 = 没有任何会话在工作中或在等用户回答（助手对话和 herdr 里的 Agent 都算）。一直不空闲就一直不装，下载好的留着。
   - 手动更新时，App 显示「在等哪些还在忙」（哪几个对话、哪台机器），并在二级确认里给「现在就更新」：
     确认文案写明「会中断正在等 hub 的操作（助手这一轮、正在上传的文件、终端连接）；herdr 里的 Agent 不受影响，会自己连回来」。
     确认后跳过等空闲，其余步骤（校验、回滚）照旧。
5. **替换**：现在的 `jarvisd` 复制成 `jarvisd.prev`；新的先放 `jarvisd.new`，再 `rename` 成 `jarvisd`（同一目录，原子）。
6. **重启**：
   - 先起看门狗（见下），再 `syscall.Exec` 新的二进制，参数和环境不变；
   - 同一个进程号重新开始：systemd、launchd 和手动 `jarvisd hub run` 都一样，不依赖服务管理器；
   - 本机 node 是 hub 的子进程，随之退出，新 hub 用新二进制起它。
7. **确认**：新 hub 启动时看到 `pending.json`，满足下面三条才算成功（删 pending、写更新记录、广播「已更新到 …」）：
   - HTTP 起来；
   - 连上数据库；
   - 重启前在线的机器在 2 分钟内全部连回来（离线的不算）。
8. **回滚**，两层：
   - **新进程自己**：2 分钟内没满足条件，或者启动次数超过 3 次（在服务管理器下反复崩），就把 `.prev` 换回去再 `exec` 它。
   - **看门狗**（旧二进制的代码，`jarvisd update-watch`，exec 之前另起、脱离进程组）：
     3 分钟内没从本机 `/api/v1/health` 看到新 build 号和「已确认」，就自己回滚：
     - 在 systemd（有 `INVOCATION_ID`）或 launchd 下：换回 `.prev`，杀掉主进程，由服务管理器用旧二进制拉起；
     - 没有服务管理器：换回去后自己用原来的参数后台起旧的 hub。
   - 回滚后，这个 build 号记进**坏版本**列表：自动更新不再试它，等更新的版本出来再装。用户手动点仍然可以装，App 提示「这一版上次装完起不来，已经回滚过」。
9. **node 跟着更新**：
   - hub 确认成功后，对每台在线、版本不同的机器走 `deploy.go`（推二进制、重启常驻 node、等它连回来），一次一台；
   - 失败的机器留着「重新部署」，不影响 hub；离线的下次连上时照旧提示；
   - `deploy.go` 现在只走 ssh：经别的机器中转（`via`）和只能经 tailcat 连的机器要补上，第 2 步一起做。
10. **记录**：每次更新（自动或手动）一条：从哪个版本到哪个版本、开始和结束时间、结果（已更新 / 已回滚及原因 / 失败及原因）、自动还是手动。
    存 Postgres（settings 的 `hub_update_history`，留最近 50 条）。

## 9. 三种运行方式

| | 发布版（我们的 hub、install.sh 装的） | Docker | dev |
| --- | --- | --- | --- |
| 检测 | 版本清单 | registry 上镜像的标签（见「Docker」） | 不检测 |
| 更新 | 自动（默认开）/ App 一键 | 提示 `docker compose pull && docker compose up -d`（钉了版本的先改 `.env`） | 无 |
| 重启 | 自己 `exec` 新二进制 | 用户重建容器 | — |
| 回滚 | 自动（`.prev`，坏版本不再试） | 换回上一个 tag | — |
| node | 跟着 hub 自动重新部署 | 镜像自带 node 二进制，重建后照旧「重新部署」 | — |
| 急需 master | 直接部署（打 `release` 和编译时间，旧的公开版不会把它降级，更新的公开版自动接手） | — | — |

### Docker（第 4 步）

- **查哪个镜像**：compose 把 `.env` 的 `JARVIS_IMAGE` 传进容器，hub 查的就是它；没传（老的 compose 文件）用构建时写进二进制的
  `main.image`（`hub-image.yml` 传 `ghcr.io/<owner>/<repo>`，不写死）。
- **怎么查**：registry 的 `GET /v2/<repo>/tags/list`，按它回的 `WWW-Authenticate` 先拿一个拉取 token（公开镜像匿名就给，ghcr.io、
  Docker Hub 都是这样），跟着 `Link` 翻页。只认 `X.Y.Z`，取最大的；比自己的版本新才算「有新镜像」。自己的版本也得是 `X.Y.Z`
  （或 `-rc1` 这类预发布）：手动跑出来的 `edge` 镜像版本是 `git describe`，比 tag 新，比不出来，就只显示最新的正式版本。
- **私有镜像**：仓库私有时 ghcr 上的镜像默认也私有，匿名拿不到 token。`.env` 里设 `JARVIS_REGISTRY_TOKEN`（能读这个镜像的 token），
  或者把镜像改成公开；没设时设置 › hub 里说查不了、该设什么。不读 NAS 上 `docker login` 存的凭据（在宿主上，容器里没有）。
- **App**：Agents 页顶上「hub 有新镜像 X」，只有 ×（按版本记，下个版本再提示）；设置 › hub 里是命令，标签钉在某个版本时先给改 `.env` 的那一行。
- 不做：替用户拉镜像、重建容器（要挂 Docker socket，等于把 NAS 交给 hub）。

## 10. App 里

- **设置 › hub**（「服务器」一组里新的一行，右边是版本和状态；`HubSettingsPage`）：
  - 版本、渠道（「发布版」/「Docker」/「开发版」）、上次检查、「检查更新」（下拉也是检查）；
  - 更新：有新版本时显示版本和说明、「更新 hub」；这一版回滚过的标出来（自动更新不会再装它）；
    更新中显示进度（下载中 / 等空闲 / 正在重启 / 新版本正在确认）；在等空闲时列出在忙的对话和机器，下面一个「现在就更新…」，
    二级确认说明会中断什么（助手这一轮、正在上传的文件、终端连接；herdr 里的 Agent 不受影响）；
    开发版说明为什么不会自己更新；Docker 显示要在 NAS 上运行的那行命令，可复制；
  - 开关「自动更新 hub」（只有发布版有，默认开）；
  - 各台电脑上的 jarvisd：版本，和 hub 不是同一次部署的标「版本不同」，点进机器页「重新部署」；
  - 「更新记录」：最近 10 次，结果（已更新 / 已回滚 / 没更新成功）、自动的标出来、从哪版到哪版、时间、回滚的原因。
  - 老 hub（没有 `/api/v1/hub/update`）：说明要在 hub 所在的机器上用 install.sh 更新一次。
- **Agents 页顶上的一行**（`HubNoticeRow`，和「Mac 上的 Jarvis 已登录」同一个位置和样子，搜索时不出现）：同一时间只有一条，最要紧的在前：
  1. App 太旧（App 的 build 号低于 hub 报的 `min_app_build`）：「App 版本太旧，配不上这个 hub：请到 TestFlight 更新」，不能关；
  2. hub 太旧（`protocol` 比 App 要的低，老 hub 不报就是 0）：「hub 版本太旧，部分功能用不了」，× 关掉后不再提示；
  3. 更新中：「hub 正在更新：等 2 个在忙的结束」之类；
  4. 回滚了：原因，× 关掉（按这一版记）；
  5. 第一次告知：「hub 会在空闲时自动更新」，带「关闭」（关掉自动更新）和「知道了」，点任一个 hub 记下 `auto_notice_seen`，不再出现；
  6. 有新版本而自动更新关着：「hub 有新版本 X」，带「更新」和 ×（这一版不再提示）。自动更新开着时不提示，它会自己装；
     Docker 的 hub 是「hub 有新镜像 X」，只有 ×。关掉一条后下一条照常出现，App 记得最近关掉的几条。
  点这一行别的地方打开 hub 页（sheet）。不弹窗、不挡操作，按钮文字单行；行里几个按钮各管各的（`.borderless`，`HubNoticeUITests`）。
  只放 Agents 页：助手页是一段对话，顶上多一行会把对话往下推；hub 的状态和机器、Agent 放在一起看。
- **通知**：自动更新装好推一条「hub 已更新到 X」，回滚了推「hub 更新没成功」和原因（hub 的 `updater.notify`）；手动点的更新在 App 里看得到，不另推。
- **重启期间**：连接断开是预期的，标题照常「连接中…」变成「hub 正在更新…」，点开的说明讲清是在重启、新版本起不来会自动回到上一版；连回来后显示新版本。
- **设置 › 助手 › Skills**（`SkillsPage`）：跑助手那台电脑上的 skill（`GET /api/v1/skills`），只读，标出是自己放的还是内置的，说明放在哪、什么时候生效。
- 都走设计系统，两种风格（液态玻璃、BeautifulUI）× 深浅色都截图看过。

## 11. 兼容

- `protocol`：App ↔ hub 的大版本号（`proto.Protocol`），只有不兼容的改动才加一；细粒度的能力照旧用 `features`。
  hub 在 `/api/v1/health`、`hello` 里报 `protocol`、`min_app_build`；App 里写着自己最低要的 `protocol`。
- 老 App 不认这些字段，照旧工作，只是看不到更新相关的东西。
- 老 hub 没有这些接口：App 显示版本，「检查更新」说「这个 hub 太旧，不支持在 App 里更新」。
  公开安装的老 hub 要手动用 install.sh 装一次带更新能力的版本；我们的 hub 由主会话最后一次用 ssh 部署上去。

## 12. 接口（草案）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/hub/update` | `{version, build, channel, can_update, latest?{version, build, released, notes, bad}, state, progress, busy[]{machine, session, title}, error, auto, auto_notice, last_check}` |
| POST | `/api/v1/hub/update/check` | 立即检查 |
| POST | `/api/v1/hub/update` | 开始更新（非发布版 409，说明原因）；`{"now": true}` 是二级确认后的「现在就更新」，跳过等空闲 |
| PUT | `/api/v1/hub/update/settings` | `{auto}`；`{auto_notice_seen: true}` |
| GET | `/api/v1/hub/update/history` | 更新记录，新的在前 |
| GET | `/api/v1/skills` | 跑助手那台机器上的 skill：`[{name, description, source: user\|builtin}]` |
| WS 事件 | `hub_update` | 状态和进度变化时推给开着的 App |

`state`：`idle` / `checking` / `available` / `downloading` / `waiting_idle` / `restarting` / `confirming` / `updated` / `rolled_back` / `failed`。

## 13. 测试

- Go：
  - 下载不对：sha256 不对上就删掉、不装，传到一半断了重下；
  - 下载中断后重试；sha256 不对时删掉、不替换；
  - 新版本起不来：用测试里编的一个故意起不来的二进制，确认新进程自检和看门狗都能回滚，坏版本记下、不再自动试；
  - 等空闲：助手或任何 Agent 忙时不重启（自动和手动都是），`now` 跳过等待；
  - skill：符号链接建立和清理、旧安装的自动迁移、同名时用户的优先、`ol` 只在有 lark-connector 时出现；内置 skill 目录只有可再分发的；
  - dev / docker 渠道不管清单多新都不安装（接口 409）；
  - **不降级**（`TestNoDowngrade`）：直接部署的二进制（build 号是编译时间）看到 build 号更小或相等的清单不装；更大的才装；
- publish.sh：build 号不递增、线上 commit 不是祖先，都不发布。
- App：用 FakeHub 测设置里的 hub 一行、第一次告知、提示条（出现、点 × 不再出现、进度、断开期间的说明、不兼容的两种提示）、更新记录、Skills 列表。
  截图：两种风格 × 深浅色。

## 14. 分步实现

1. **一种二进制和版本清单**：skill 拿出来（用户空间、符号链接、迁移、`ol` 和提示词按需、只读列表的接口）、去掉 `-tags public`、
   渠道和 build 号（publish.sh、deploy.sh、Docker）、`internal/update`（清单、发布前检查）、publish.sh 发清单并做两项单调检查；
   `/api/v1/health` 带新字段。部署前把 lark-connector 放进我们 hub 的 `~/.jarvis/skills/`（或者靠自动迁移）。
2. **hub 更新和回滚**：检测、下载校验、等空闲（「现在就更新」）、替换、看门狗、坏版本、记录、不降级；node 跟着更新（补 `via` 和 tailcat）。
3. **App**：设置的 hub 一行、第一次告知、提示条、进度、在等哪些、更新记录、不兼容提示、Skills 列表。
4. **Docker 检测和提示**。

## 拍板（2026-11-02）

1. skill 从二进制里拿出来，放进用户自己的空间（`~/.jarvis/skills/`），更新不覆盖；完整版和公开版合成一个，所有 hub 都自动更新，
   我们自己的也不例外（用户：「skill 肯定是需要非常自定义的」）。ssh 部署以后只留作应急。
2. 自动更新**默认开**；第一次要明确告诉用户并给关闭的入口；每次更新留记录；回滚过的版本不再自动重试。
3. 不限时段：助手和 Agent 都空闲就装。有对话在跑或在等回答就不重启。
4. 版本号用发布时间（UTC `YYYYMMDDHHMM`）做 build 号。
5. **不做自动发布**：发布者照旧手动跑 publish.sh（Worker 在他的账号下）；publish.sh 加上清单、build 号和 commit 祖先检查。
   我们的 hub 也自动更新到他发布的版本；直接部署保留，用于急需 master 的时候，不会被降级。
6. 不做灰度，发布后所有 hub 同时更新；各 hub 自己回滚。
9. **不签名**（用户：「Linux 服务就不签了吧，搞太复杂」）：清单只有 sha256，走 HTTPS，对上就自动装；只防传坏，不防发布站点被冒充或入侵（见「安全」）。
7. 手动更新也等助手和 Agent 都空闲；App 显示在等哪些，二级确认里给「现在就更新」。
8. skill 空间放在跑助手的那台机器上的 `~/.jarvis/skills/`。
