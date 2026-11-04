# 经 Persona Link 更新 node

机器通过 Persona Link 主动连接 hub（`kind: link`），或由 hub 连接电脑（`kind: service`）时，使用现有 node RPC 更新常驻 personad。Link 继续只转发字节流，不新增监听端口、不要求 SSH，也不负责安装程序。

本能力更新常驻 node 使用的可执行文件；不更新连接器 jarvis-link 或 Mac 菜单栏 App 本体。相关实现：`server/internal/hub/deploy_rpc.go`、`server/internal/node/update.go`、`server/internal/nodeupdate/`。旧的 SSH / tailcat 部署入口继续保留。

## 使用

Web 机器页或机器详情出现版本不一致时，点「重新部署」。页面依次显示「等待空闲」「传输中」「重启中」「确认中」。传完文件不代表成功：机器必须重新连接，并实际运行目标版本，hub 才确认更新完成。

- hub 从既有的平台产物目录选择与这台电脑 OS / 架构一致的 node，目标是 hub 自己运行的版本。
- 正常更新等待该机器的 Agent 空闲，包括等待用户回答和仍在运行的后台任务；node 在安装前再检查一次，防止传输期间开始了新任务。
- 同一台机器只接受一个更新事务；连接断开不会丢失事务互斥。
- hub 自更新后逐台更新在线 node；当时离线的 Link 机器保存待更新目标，下次连接后继续。不会为更新而改变连接方式。
- hub 的任务目标、进度和结果存入 Postgres settings（`node.deploy.<machine id>`）。hub 重启后，已接受的任务可以继续；若 hub 版本已经变化，停止旧目标，明确提示重新更新，避免安装过时目标。

## 首次升级旧 node

新增协议无法让不认识协议的旧 node 更新自己。机器未上报更新能力时，Web 页面不提供必定失败的按钮，而是说明先在电脑上升级一次常驻 node。

1. 取得包含此能力、适合本机系统和架构的 personad，确认它包含本机正在使用的功能和定制改动。
2. 在电脑上查看常驻 node 的实际安装位置：`jarvis-link node status --json` 的 `bin`。遵守现有服务的运行根、启动参数和环境，不能仅根据默认目录猜测。
3. 使用原安装或部署渠道替换该二进制，再重启对应的常驻 node 服务。Linux 的标准用户服务为 `jarvisd-node`；Mac 的标准 LaunchAgent label 为 `com.jarvis.node`。
4. 回到机器页确认 node 已连接并支持远程更新，后续升级即可从客户端操作。

Mac 上重新安装 Persona Link App 目前不会自动替换已安装的常驻 node；不能把升级菜单栏 App 当作完成此步骤。首次迁移应使用包含此能力的 node 产物。

只对具备受支持进程恢复方式的常驻 node 开放更新。直接运行的 stdio node、手工启动且没有受支持服务管理器的常驻进程，应显示不能远程更新的原因。旧 hub 不具备新编排逻辑，也需要先通过原渠道升级 hub。

## 协议和安装边界

`node.info` 增加可选的 `node_update` 和 `node_update_reason`。旧 node 不上报，按不支持处理。现有 `POST /api/v1/machines/{mid}/deploy` 不变；机器模型增加 `can_deploy`、`deploy_reason` 和 `deploy_phase`，老客户端忽略新增字段仍能工作。

node RPC：

| 方法 | 参数 | 结果 / 含义 |
| --- | --- | --- |
| `node.update.begin` | `size`、`sha256`、`version` | `id`、已接收的 `offset`；相同目标可重试 |
| `node.update.chunk` | `id`、`offset`、`data`（JSON base64） | 确认后的 `offset`；检查顺序和大小 |
| `node.update.abort` | `id` | 放弃未提交的暂存事务 |
| `node.update.commit` | `id` | 完整性、自检和空闲检查通过后接受安装；成功响应写出后才重启 |
| `node.update.confirm` | `version` | 新 node 连回 hub 后确认；必须匹配正在运行的版本与待确认目标，可重复调用 |

安装目标由 node 根据真实可执行文件及软链接确定，hub 不能通过参数指定任意覆盖路径。新文件在同目录暂存，检查大小、SHA-256、系统 / 架构及执行 `version` 的结果，再保留旧版、原子替换。损坏、截断或版本不对的文件不会替换现有程序。完整性校验沿用现有发布信任模型，不新增发布签名要求。

确认调用沿用已配对 hub 的信任边界；HTTP 部署权限仍由现有认证与角色过滤控制，不因为新增 node RPC 而扩大客户端权限。

## 重启与回滚

更新管理器属于常驻进程，所有 attach 连接共享。一次 RPC 连接结束只会断开流，不能代替常驻 node 的整体重启。正常切换先回复提交请求，再关闭连接和监听、排空常驻服务，使用原参数和环境启动新可执行文件。

旧版独立恢复进程保留在可执行文件旁，负责在新版崩溃、启动失败或确认超时后恢复备份。Linux 使用独立的 systemd transient 用户服务，使它不随 node 的 cgroup 清理而退出；Mac 使用脱离进程组的恢复进程。恢复进程就绪后才允许替换。恢复动作不依赖新 node 成功启动，也不依赖 hub 此时在线。必要的本机恢复事务标记与二进制相邻保存，理由与 hub 自更新相同：数据库和远端连接不可用时仍须恢复；hub 的业务状态和更新任务不写本机 JSON。

hub 重连后先确认 node 具备正常读取会话的能力，再确认目标版本；即便原更新任务因 hub 重启而消失，仍可以完成确认。未确认上线时，客户端保留失败或确认中状态，不把文件发送成功当作更新完成。单台 node 更新失败不触发整个 hub 回滚。

## 验证

- hub：`TestLinkedDeploy*`、`TestUpdateNodesQueuesOfflineLinkedMachine`，验证分块、恢复位置、确认位置异常、忙碌期间不更新、提交前变忙、目标版本确认、hub 重启恢复和离线目标。
- node / 本机更新器：隔离可执行文件与临时 HOME，验证完整包、损坏包、错误版本、并发请求、常驻进程重启、确认和失败回滚。不得安装或停止开发机真实的 `com.jarvis.node` 服务。
- Web：`machine-deploy.test.ts` 验证新旧 hub、更新能力和各阶段；匿名数据在浅色 / 深色、列表 / 详情和窄屏实际截图检查。
- 服务端全套使用真实 Postgres 的隔离测试库，遵守 `docs/testing.md` 的测试环境隔离要求。
