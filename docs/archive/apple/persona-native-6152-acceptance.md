# Persona 改名：Apple 原生验收（#6152）

记录日期：2026-11-07。本报告是本机编译、测试和隔离模拟器验收，不是生产切换批准。

## 结论与边界

原生编译与常规单测通过，但客户端状态迁移不完整：个人 Agent 的改名偏好键、旧内置形象 ID 没有自动转换；旧版保存的 Shortcut 在升级后显示「未知操作」，需要重建。因此不能据此宣称生产切换验收通过。

没有部署、重启线上服务、运行维护脚本 `apply`、连接生产数据库或上传 TestFlight。没有更改 Bundle ID、App Group、Keychain group、证书、描述文件或 Apple 注册记录。CI 按任务要求未运行，不记为通过。

## 源码与工具链

- 初始 `origin/master`：`788f5075987c4c76da814416e8fc8d02904eb6ff`。
- 最终验证基线：`cec33f864f8698e6555d1ebf514b6a7e9bc99013`，加本 PR 的 source-link 测试修复。其 Apple 源码与先前快照 `ce0859c4c` 相同；后续合并没有改动 Apple 文件。
- 旧版基线：`39a706a33`，主改名提交的父版本。本机重编旧源码，不是来自 TestFlight 的真实设备二进制。
- 实际工具链：Xcode 27.1，build `27A9275`。本机没有 Xcode 26.0，**26.0 未实编**。
- 独立临时 worktree；所有构建和测试命令经 `~/.local/bin/xcode-slot`，自己的 Xcode 构建串行。
- 构建配置只保留现有注册身份，线上地址和服务密钥不进入测试配置；没有修改用户配置。

## XcodeGen 与编译

`python3 scripts/persona_config.py xcconfig` 后运行 `xcodegen generate --spec apple/project.yml`，生成成功。以下最终命令均退出 0、输出 `BUILD SUCCEEDED`：

```sh
cd "$WORKTREE/apple"
BASE="$HOME/.local/bin/xcode-slot"

"$BASE" xcodebuild -project Persona.xcodeproj -scheme Persona \
  -configuration Debug -destination 'generic/platform=iOS Simulator' \
  -derivedDataPath "$DD_IOS" -disableAutomaticPackageResolution \
  -parallel-testing-enabled NO CURRENT_PROJECT_VERSION=2 build

"$BASE" xcodebuild -project Persona.xcodeproj -scheme Persona \
  -configuration Release -destination 'generic/platform=iOS' \
  -derivedDataPath "$DD_RELEASE" -disableAutomaticPackageResolution \
  -parallel-testing-enabled NO CURRENT_PROJECT_VERSION=2 build CODE_SIGNING_ALLOWED=NO

"$BASE" xcodebuild -project Persona.xcodeproj -scheme PersonaMac \
  -configuration Debug -destination 'platform=macOS' \
  -derivedDataPath "$DD_MAC" -disableAutomaticPackageResolution \
  -parallel-testing-enabled NO CURRENT_PROJECT_VERSION=2 build CODE_SIGNING_ALLOWED=NO

"$BASE" xcodebuild -project Persona.xcodeproj -scheme PersonaMac \
  -configuration Release -destination 'platform=macOS' \
  -derivedDataPath "$DD_MAC" -disableAutomaticPackageResolution \
  -parallel-testing-enabled NO CURRENT_PROJECT_VERSION=2 build CODE_SIGNING_ALLOWED=NO

"$BASE" xcodebuild -project Persona.xcodeproj -scheme PersonaVision \
  -configuration Debug -destination 'generic/platform=visionOS' \
  -derivedDataPath "$DD_VISION" -disableAutomaticPackageResolution \
  -parallel-testing-enabled NO CURRENT_PROJECT_VERSION=2 build CODE_SIGNING_ALLOWED=NO
```

| 范围 | 结果 |
| --- | --- |
| iOS App、watch App、widgets、通知与分享扩展 | Debug / Release 编译通过 |
| Mac App、通知扩展 | Debug / Release 编译通过 |
| visionOS App、分享扩展 | Debug 编译通过；没有进行 visionOS 运行时验收 |
| Persona Link 独立 SwiftPM 可执行 target | `swift test` 构建并通过 23 项测试 |

watch 产物 `MinimumOSVersion` 为 `10.0`，完整 `Shared/` 编译通过；这不是 watchOS 10 实机或运行时证明。

## 单测与 UI 测试数量

```sh
"$HOME/.local/bin/xcode-slot" xcodebuild -project Persona.xcodeproj -scheme Persona \
  -configuration Debug -destination "id=$SIM" -derivedDataPath "$DD_IOS" \
  -parallel-testing-enabled NO -collect-test-diagnostics never \
  -resultBundlePath "$IOS_RESULT" -only-testing:PersonaTests \
  CURRENT_PROJECT_VERSION=2 test

"$HOME/.local/bin/xcode-slot" xcodebuild -project Persona.xcodeproj -scheme PersonaMac \
  -configuration Debug -destination 'platform=macOS' -derivedDataPath "$DD_MAC" \
  -parallel-testing-enabled NO -collect-test-diagnostics never \
  -resultBundlePath "$MAC_RESULT" -only-testing:PersonaMacTests \
  CURRENT_PROJECT_VERSION=2 test CODE_SIGNING_ALLOWED=NO

swift test --package-path apple/Packages/PersonaKit --scratch-path "$DD_KIT" -j 2
swift test --package-path apple/PersonaLink --scratch-path "$DD_LINK" -j 2
```

| 测试 | 执行 | 通过 | 失败 | 跳过 |
| --- | ---: | ---: | ---: | ---: |
| iOS 全量单测，修复后 | 2,361 | 2,357 | 0 | 4 |
| Mac 全量单测 | 350 | 350 | 0 | 0 |
| PersonaKit | 111 | 111 | 0 | 0 |
| Persona Link | 23 | 23 | 0 | 0 |
| 构建号 1 → 2 后的系统探针与 onboarding，深浅色 | 9 | 9 | 0 | 0 |
| 精确定位旧 action 的 Shortcut 执行探针 | 1 | 1 | 0 | 0 |

最后一项「探针通过」只表示正确定位、执行尝试并保存截图；**旧 Shortcut 实际显示未知操作，并未成功运行**。不能将这项探针结果解释成兼容升级通过。

iOS 的四个显式跳过：HTML 无限循环隔离、外部 relay 交付 fixture、真实 HTTP 视频读取、原生视频播放。没有用这些跳过项宣称推送或视频端到端通过。Mac 测试使用独立 HOME/CFFIXED_USER_HOME；测试宿主跳过真实 Keychain 读取，不作为 Mac 真正配对升级的证据。

Apple 改名静态检查 19 项、Persona-only 守卫 9 项、文档路径检查 11 项通过。

## 产物检查

- 通知与分享扩展的 Info.plist principal class 对应编译后的正确 Swift 类型；在模拟器进程中实际解析 iOS 两个主类，在本机 Swift 进程中解析 Mac 通知扩展主类。
- 最终产物没有未展开的 plist 变量；Release 没有 DEBUG runtime CLI 残留。
- App Intents 提取元数据存在：iOS 15 个 action、Mac 10 个 action；widgets 的两个 controls 对应新 action。
- 从 iOS、Mac Release 的真实 Mach-O 反射字段记录解析到 `Persona.AskPersonaIntent` 的 `_question`，确认字段名没有被裁掉。没有将 Debug 成功当成 Release 反射证明。
- 编译后的 Info.plist 与三种语言用途字符串检查通过：iOS 主 App 13 项、Mac 主 App 11 项；没有空值或键名占位值。
- 使用已有发布证书与已有描述文件，本地离线签名并验证 iOS App、watch 与三个扩展：`entitlements ok: 5 bundles`，production APNs、App Group、Keychain group 和 time-sensitive entitlement 检查通过。签名副本没有上传。
- Mac Release 的 ad-hoc 签名与 entitlements 检查通过 2 个 bundle；**没有验证 Mac App Store 导出或发布资格**。

## iOS 原位升级

专用原版 iOS 27.0 模拟器，旧构建号 1 → 新构建号 2。同一 Bundle ID 与共享注册身份原位覆盖，不卸载或 erase；新版本首启没有 `openWelcome`、重新注入配对或清缓存参数。

| 项目 | 实际结果 |
| --- | --- |
| App 测试文件与 App Group 测试文件 | 字节保持不变 |
| 普通持久化偏好 | 保留 |
| Keychain 配对及第二次冷启动 | 保留；可连新版本隔离 mock |
| 个人 Agent 改名偏好键 | 没有自动迁移；旧值保留但新 reader 不读取 |
| 旧内置形象选择 ID | 没有自动转换；不能记作选择保留 |
| 旧版系统 Shortcut | 旧 App 在系统 UI 中创建并持久化；原 action 身份不变，新 App 下显示未知操作 |
| 新 Shortcuts / App Intents | 在系统动作目录中出现，可创建新的动作 |
| 新 controls | 系统图库显示 Ask Persona 与 Persona Agents |
| 旧 controls、Live Activities 与任意 Swift 系统归档 | 没有完成迁移验收；不得推断自动保留 |

共享容器测试文件保留不等于所有业务文件内容已转换。没有真实用户数据、未发送草稿、outbox、排队录音或真实加密推送密钥的迁移证明。新 mock 的连接也不代表真实 hub 的认证、TLS pin、公网连接或生产推送通过。

## Phone/watch

原版 iOS 26.5 / watchOS 26.5 的专用系统配对创建成功。旧 phone/watch 构建收到并持久化配置；保持两端 Bundle ID 不变，原位安装两端构建号 2。新版本首启没有重新注入配对：watch 配置保留，匹配版本的 phone/watch 可启动，watch 从新 mock 读取并显示提问，截图已实际查看。

这证明该模拟器组合的配置保留和匹配版本运行，不代表真实手表、watchOS 10、排队录音、或每个 WatchConnectivity 消息的跨版本迁移均通过。watchOS 10 运行时与真实设备不可用，保留为未验证项。

## 截图证据

截图均来自专用模拟器与编造的 mock 数据，已经实际查看；不含真实用户数据。启动图用于证明覆盖安装后的界面与深浅色表现，不作为完整数据、性能或所有页面验收。

| 内容 | 截图 |
| --- | --- |
| 升级后启动，浅色 / 深色 | 浅色 / 深色 |
| 系统中的新 App Intents | Shortcuts 动作目录 |
| 新 controls 注册 | controls 图库 |
| 旧 Shortcut 实际失败 | 未知操作 |
| phone/watch 匹配版本升级 | watch |

## 修复与失败历史

本 PR 只修复 source-link 的测试 fixture：改名把一个负向 URL 变成了合法 canonical 链接，但旧断言仍要求拒绝。补充合法 opaque ID 的正向断言，并用中性 foreign scheme 保留负向检查；不修改正常解析或同条目授权边界。

首次 iOS 全量为 2,356 通过、1 失败、4 跳过；修复后全量为 2,357 通过、0 失败、4 跳过。首次依赖锁解析失败、不可用的模拟器设备匹配、系统 UI 定位器、mock 配对码复用及重复激活已激活配对的失败日志均保留，后续成功没有改写原始结果。

原始日志、`.xcresult`、签名副本、系统数据库 fixture 和原图留在本机私有证据目录，不上传 GitHub。生产切换前还需明确客户端状态单次迁移或重建方案，并由用户批准维护窗口。
