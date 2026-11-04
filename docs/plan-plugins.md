# 计划：插件底座（#1101）

状态：**第 1 步（底座）的服务端做完**（1a，2026-11-04）。规格（清单、安装、协议、接口、命令行）已经写进 `docs/plugins.md`，以那里为准；
这份留着记设计取舍、实现时和方案的出入、还没做的步骤。issue #1101 定了方向：一个统一底座 + 各模块开放扩展点，骨架学 VS Code（清单 + `contributes` + 按需激活 + 独立进程），
权限学 Chrome（显式声明、安装时展示），WordPress 只借事件通知、不借 filter。

## 一、现状

| | hub 插件（`docs/plugins.md`） | 连接器协议 v2（`docs/connector-protocol.md`） |
| --- | --- | --- |
| 从哪来 | hub.json 的 `plugins[]`，主人手写 | 只有 Telegram：`jarvisd-telegram` 在 personad 旁边就注册，代码里写死 |
| 进程 | hub 起，2 秒起、最长 60 秒退避重启；或只给 `socket`，跑在别处 | `connector/proc` 起，5 秒起、最长 5 分钟退避；连接器关掉就停 |
| 怎么说话 | 插件在 unix socket 上起 HTTP，hub 原样转发 `/api/v1/plugins/<id>/…`；`GET /health` 判活 | stdin / stdout 一行一条 JSON（`internal/rpc`），`init` → 清单 → 各方法；一致性测试 |
| 配置 | `config`（任意 JSON）放进环境变量 `PERSONA_PLUGIN_CONFIG` | `init` 带 hub.json 里它那一节 |
| 密钥 | 自己处理（`key_file`、`key_command`）；还拿到 hub 的完整 token | `secrets.*`：hub 加密存 `connector_secrets`，只给它自己 |
| 权限 | 没有，等于 hub 本身 | 写操作只能经 `actions.do`，过 hub 的闸门 |
| 状态 | 内存 | 开关、选项存 `settings` 表的 `connectorSettingsKey` |

两套各有一半：hub 插件有通用的进程管理和 HTTP 转发，没有清单、密钥、权限；连接器有清单、密钥和协议，但只认一个写死的程序。底座把两边合起来。

## 二、关键决定

1. **一个注册表，三个来源**：内置（personad 自带，清单编进二进制）、hub.json（现有的 `plugins[]`，原样兼容）、安装的（包放在 hub 机器上，登记在数据库）。
   App 和 API 只看到一个列表。
2. **清单是唯一的事实**：能力、配置项、密钥、权限、进程怎么起，全在 `persona-plugin.json`；hub 不运行插件也能列出它、画出配置表单、给用户看权限。
   程序在握手时报的清单只能是它的子集，不一致时以安装时接受的为准（和连接器现在的做法一样）。
3. **进程插件的协议就是连接器 v2 推广出来的**：同一个 `internal/rpc` 一行一条 JSON，握手改名 `initialize`，方法按扩展点分命名空间。
   连接器 v2 的方法原样搬进 `connector.` 命名空间（`status` → `connector.status`），字段和意思一个都不变，原来的一致性测试照跑。
4. **HTTP 转发保留，作为进程插件的第二个出口**：语音要在手机和插件之间直传音频 WebSocket，JSON-RPC 不适合。清单里 `runtime.http: true` 的插件在
   unix socket 上起 HTTP，hub 照旧转发。所以现有的语音插件不用改一行就是一个合法的插件（只有 HTTP、没有 RPC）。
5. **安装只在 hub 机器上和管理界面做，手机不能装**：装插件就是在 hub 机器上跑一个新程序。手机的设备凭据只能看列表、开关、改配置、填密钥；
   安装、升级、卸载要 hub 的主 token（只在 hub 机器的 hub.json 里，命令行 `personad plugin …` 用它走回环）或管理界面的登录会话。
6. **权限第 1 步是「声明 + 展示 + 记录」，不是沙箱**：进程插件以 hub 的系统用户运行，能读写这个用户能读写的一切；网络域名 hub 管不住。
   安装时把这句话和声明的权限一起给人看，接受的那一份存下来；hub 自己提供的能力（给 hub token、以后的知识库读写等）按接受的权限放行，没接受就拒绝。
   真正的隔离（Linux 用 bubblewrap / 网络命名空间，Mac 用 sandbox-exec，或经 hub 的代理出网）以后单独做，见「六」。
7. **配置只支持 JSON Schema 的一个子集**：子集以外的写法装不上，保证每个插件的配置在 App 里都能画成表单，而不是装上以后才发现某个字段没法改。
8. **改了配置、密钥或开关就重启插件进程**：第 1 步不做热更新，少一种状态。连接器自己的「选项」（`connector.Options`）不受影响，照旧热改。

## 三、实现时和方案的出入（1a）

- **管理界面的安装页没做**：安装、升级、回滚、卸载只认主 token 经回环（`personad plugin`）；管理会话那条路等做安装页时一起加。
- **协议多了一个基础方法 `manifest`**：`initialize` 只回 id、版本和贡献，装跑在别处的插件时要完整清单（权限、配置、密钥），所以加了 `manifest`
  （`initialize` 之前也能调；hub 从包里起的插件可以不回）。只有 HTTP 的远端插件回 `GET /manifest`。
- **贡献了连接器的插件由连接器的开关持有**：它的 `on_call` 不算数（否则「关着时登录」会把程序拉起来，和现在的行为不一样）；`on_startup` 照样常驻。
- **连接器程序拿得到用户填的密钥**：`connector/serve.MainWith` 的 `Init.Declared`；原来的 `serve.Main` 不变。
- **内置 Telegram 的版本号是 hub 的版本**（不是语义化版本）：内置插件不过 `Parse`，`engines` 由 `version -v` 的检查代替。
- **列表多了 `can_install`**（hub 有没有数据库），`features` 多了 `plugin_manager`。
- **升级失败自动回滚只看自己会起的插件**（`on_startup`，或连接器开着）：按需起的插件升级时不会为了检查而拉起。
- 迁移号用了 `0291_plugins.sql`（避开其他车道的号段）。

## 四、分步

- [x] **1a：服务端底座**：`internal/plugin`（清单、schema 子集、权限、版本范围、包的暂存和解包、注册表）、`plugin/host`（进程宿主、按需激活、退避、超时、ping、HTTP、远端 socket）、
  `plugin/serve`（程序一侧）、`plugin/conformance`；迁移 `0291`；hub 的接口和 `personad plugin`；语音（hub.json）和 Telegram（内置）迁上来；
  `connector/serve` 认插件开头，`connector/proc` 能经插件进程说话（`Link`）；文档 `docs/plugins.md`、`docs/connector-protocol.md`。
- [ ] **1b：App「设置 › 插件」**（单独的 PR，iOS 先做）：列表（名字、版本、状态、来源）、详情（权限、贡献）、按 `form.fields` 画的配置表单、
  只写的密钥、开关；深浅色截图。Android、网页、管理界面的安装页放到后面。
- [ ] 第 2 步：试点额度 + 模型供应商两个扩展点（`quota.query`、`provider.models`），把写死的供应商额度查询迁成插件。开始前先在这里补两个扩展点的接口。
- [ ] 第 3 步：连接器、链接识别等散落的内部逻辑迁成私有插件，开源仓库里清干净。
- [ ] 第 4 步：声明式 UI（`ui.render` / `ui.action`）和事件钩子（`event.*`，只通知）。

## 五、现有的两套怎么迁上来

### 语音插件（hub.json 的 `plugins[]`）

- hub.json 里的每一项照旧读，hub 替它合成一份清单：`{id, name, version: <hub 的构建号>, runtime: {command, args, env, rpc: "none", http: true}, contributes: {<kind>: [{id}]}, permissions: {hub_api: true}}`；
  只给 `socket` 的是 `remote:`，hub 不起进程。`dir`、`env`、`config`、`disabled`、`command: "personad" / "jarvisd"` 的意思都不变。
- 行为不变：socket 路径、环境变量（包括 hub 的 token）、`/health` 判活、转发时去掉 App 的凭据、手表那种只读设备 403、模型配置的 `plugin.<id>`。
  唯一的变化是退避上限从 60 秒变成 5 分钟。
- `GET /api/v1/plugins` 的老字段不变（Android 的语音入口按 `kind == "voice"` 和 `status == "running"` 找），只多字段。
- 语音的配置（`providers` 是对象数组）超出表单的子集，第 1 步不进注册表的配置，照旧在 hub.json；以后子集支持对象数组了再说。
- 现有的 `server/internal/hub/plugins_test.go` 不改断言，照旧要过。

### Telegram（连接器 v2）

- 成为一个内置插件：清单编进 personad（`id: "telegram"`，`runtime.command` 是 personad 旁边的 `jarvisd-telegram`，`contributes.connectors: [spec.Manifest()]`），
  程序在才注册，和现在一样；`engines` 要求和 hub 同一个构建（现在的 `version -v` 检查保留）。
- 连接器 id、`connector_secrets` 里的会话、连接器设置里的开关和选项、默认关着都不变。内置插件的启停和连接器自己的开关是两层：插件默认启用，用户平时只碰连接器的开关。
- 连接器的一致性测试（GitHub、信息、Telegram）照跑，并对 `connector/serve` 的两种开头各跑一遍；Telegram 的 proc 测试改成经插件宿主起程序。

## 六、不做 / 以后

- 沙箱（文件、网络的真正隔离）、CPU / 内存限制、插件签名、registry：第 1 步都不做，接口留了位置（权限词表、`origin`、`engines`）。
- 插件之间的依赖、插件调别的插件：不做。
- WordPress 的 filter（插件改 hub 的数据）：永远不做；事件钩子只通知。
- hub 互联：插件属于装它的那台 hub，第 1 步不在组内同步；App 经中心 hub 看到的是中心的插件。
- 清单的多语言（`name`、`description`、表单标题）：先只有一种，以后加 `l10n`。
