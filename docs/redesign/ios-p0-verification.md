验收日期：2026-10-31。分支：`p0/v-ios`，代码版本：`2606f3d`。

**结论：接口检查通过，iOS P0 验收未完成。** 当前沙箱无法访问 CoreSimulator，未能创建模拟器、安装 App 或截图。下面的接口检查和 Swift 配对链接解析检查，不能替代 App 实际连接及 UI 测试。README 第 10 节要求的真机配对也未执行。

| 任务步骤 | 实际结果 | 证据（本机临时文件） |
| --- | --- | --- |
| 临时配置 | 已在临时 HOME 创建最小构建配置，bundle id 为 `com.example.jarvis`，未填团队；hub 使用环境变量提供的专用数据库、指定的两个回环监听端口和空机器列表 | `/tmp/jarvis-ios-verify/home/` |
| 独立模拟器 | 执行 `xcrun simctl create Jarvis-P0-v-ios com.apple.CoreSimulator.SimDeviceType.iPhone-17-Pro`，退出码 1；CoreSimulatorService 连接被拒绝，simdiskimaged 不可用，未返回设备 ID | `/tmp/jarvis-ios-verify/simulator-create.log` |
| hub 构建与启动 | 原命令因 VCS 状态读取失败而退出；加 `-buildvcs=false` 后构建成功。假 HOME 下 HTTP、TLS 启动成功 | `/tmp/jarvis-ios-verify/go-build.log`、`go-build-no-vcs.log`、`hub-isolated.log` |
| iOS 构建 | 将受版本控制的 iOS 文件复制到临时目录，配置生成与 XcodeGen 成功，使用 `CODE_SIGNING_ALLOWED=NO` 尝试模拟器构建；CoreSimulator 不可用，Swift Package 拉取又发生 HTTP/2 中断，未得到构建成功结果 | `/tmp/jarvis-ios-verify/source/ios/`、`ios-build.log` |
| 旧格式配对 | `jarvisd hub pair --legacy` 成功；该命令只打印地址、主 token 和二维码，探针按源码同一编码规则构造链接。HTTP 和 WebSocket 检查通过，未在 App 中启动 | `/tmp/jarvis-ios-verify/probe.log`、`protocol-results.json` |
| 新格式配对 | `jarvisd hub pair` 成功，探针直接使用命令输出的链接；附加字段包括 `hub`、`fp`、`tls`，HTTP 和 WebSocket 检查通过，未在 App 中启动 | 同上 |
| 原有 Swift 链接解析 | 从 `ios/Shared/APIClient.swift` 原样提取 `ServerConfig`，在临时 Swift 程序中解析两种实际链接，均成功；新参数未影响 `url`、`token` 读取 | `/tmp/jarvis-ios-verify/PairParser.swift`、`swift-parser.log` |
| 两个连接 UI 用例 | 启动仓库原有 mock，指定 `ConnectionTests/testOutageShowsConnectingThenRecovers`、`ConnectionTests/testShortDropDoesNotFlash`；Xcode 在执行测试前退出，退出码 74。禁用自动解析后的尝试还报告缺少 `Package.resolved` | `/tmp/jarvis-ios-verify/ui-tests.log`、`mock-hub.log` |
| 截图及界面状态 | 无截图；“已连接”、无错误弹窗、App 内机器列表均未验证 | 无截图路径，未生成图片 |
| 清理 | 本车道测试 hub 和 mock 均已退出，三个监听端口均已关闭；模拟器创建失败，无设备可删除，未操作现有模拟器 | `/tmp/jarvis-ios-verify/cleanup.json` |

接口检查按照现有 App 的配对顺序执行，但请求由临时 Python 探针发送，没有冒充 App 端到端结果。两种链接均得到以下结果：

- `GET /api/v1/machines` 返回 HTTP 200，`machines` 为 `[]`。
- `GET /api/v1/ws` 携带 Bearer 完成 HTTP 101 升级，并收到 `hello` 和 `machines` 事件，后者的 `machines` 为 `[]`。探针未发送 `client_hello`。本次没有得到可归属于 iOS App 的 hub 连接日志；握手和事件记录只能证明探针连通。
- `POST /api/v1/auth/tokens` 返回 HTTP 200，包含字符串字段 `id`、`token`、`role`；使用换得的设备 token 再请求机器列表仍为 HTTP 200、空列表。

发现的问题和验收边界：

- `hub.json.public_url` 需要显式设为测试 HTTP 地址。只设置 `listen` 不会同步更新它；旧配对二维码的 `url` 来自 `PublicURL`，否则会沿用默认主机地址和默认端口。本次只补充了临时配置。
- 空 `machines` 不会关闭 Lark 后台任务。未提供 `lark.token` 时，`server/internal/lark/lark.go` 的 `agentToken` 会尝试 `docker inspect` 默认容器；初次启动日志显示读取失败。发现后已停掉进程，将临时 `lark.base_url` 指向本机 mock，并填入测试 token 后重新启动。未手动操作 Docker；无法据此声称最小配置完全没有尝试访问容器。mock 不实现 `/agent/v1`，后续后台 404 属于这项隔离配置的已知结果，不应报为配对接口故障。
- 本次检查未发现上述配对接口或字段的兼容性失败，但没有在真实 iOS `Envelope` 上解码整条 WS 消息，也未验证新事件的长期能力门控，不能给出全面兼容结论。
- UI 用例未运行，无法确认 mock 改动没有打断它们。需在可访问 CoreSimulator 的执行环境中补跑上述两个用例，并完成两种链接的 App 配对与截图。
- Xcode 构建已发送中断；另一次直接终止进程的尝试被沙箱拒绝，未确认其最终退出状态。测试 hub 和 mock 的退出及端口关闭已单独确认。

仅新增本报告，`ios/`、`server/` 均无改动；构建副本、探针和日志全部位于 `/tmp/jarvis-ios-verify/`。临时配置及标记为 `private` 的文件含测试凭据，不纳入版本控制。未执行 git add、commit、push、stash、部署或线上 hub 连接。性能未实测，本报告不作性能结论。

## 补充：App 层验证（2026-10-31，沙箱外）

**结论：没有改过的 iOS App 对新 hub 兼容，P0 的 iOS 验收通过（模拟器；真机配对仍待做）。**

- 环境：集成分支的 hub 起在回环（HTTP + TLS 两个监听、专用测试库、空机器列表、Lark 指向不可达地址）；
  iOS App 用当前 `ios/` 原样构建（Debug，无签名团队），装在新建的 iPhone 17 Pro 模拟器（iOS 27.0）上。
- **两种链接都能配对、进主界面**：旧格式（`--legacy`，主 token）和默认新格式（`jp_` 临时码 + `hub` / `fp` / `tls` / `alt`）。
  App 带着凭据访问了 hub（只有依赖 Lark 的请求返回 502，属于测试配置）。
- **换凭据**（旧 App 扫新码后能长期用的关键）：
  - DEBUG 版用 `-pairURL` 启动时会跳过换凭据（`DeviceCredentials.swift`），所以测法是：先带 `-pairURL` 启动一次，配置写进 Keychain；
    再不带参数冷启动，App 走真实的 `upgradeCredentials()`。
  - 结果：新格式下临时码换出 `phone/full` 设备令牌，临时码标记为已兑换，App 随后用设备令牌连上 hub。
    旧格式下主 token 同样换出了设备令牌。
- `simctl openurl` 打开 `jarvis://pair` 时，iOS 27 会弹「在 Jarvis 中打开？」确认框，脚本点不了，所以没用这条路。
- 发现并修掉一个问题：用临时码连上的几秒里，App 注册手机工具，hub 拿临时凭据去查设备令牌表，失败并记错误日志。
  现在临时码连接只在内存里标记手机，换成设备令牌后重新注册时再保存（`server/internal/hub/http.go` 的 `register_phone`）。
- 每次冷启动，App 都有一两个请求返回 401，就在启动的那一秒（hub 日志只记来源和状态，不记路径）。对配对和换凭据没有影响，没再深查。
- 没做：真机配对、公网 TLS 直连（现有 iOS 不支持钉扎，按设计继续走内网 / VPN）、手表。
