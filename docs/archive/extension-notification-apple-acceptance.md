# Apple 扩展通知阶段验收（#1862，部分交付）

日期：2026-11-06。代码冻结：`fe49ab99eb9c0638d0ecf7cced1549934dbbb7aa`。

## 本次交付

- iOS / macOS Notification Service Extension 交付前注册动态通知类别，App 启动时保留扩展类别并合并静态交互类别；前台通知展示前也注册。
- 点通知带原始 `state` 打开完整扩展页面。前台按钮先领取、执行一次，再画结果，外观或页面重建不重放写操作；共用普通页面的链接、导航、失效刷新和结果提示。
- 后台按钮按通知所属 hub 执行，文字输入作为 `values.text` 发送，只将 `toast` 显示为本地通知，不处理链接或导航结果。后台期限结束时取消请求并完成一次代理回调。
- 未知、冲突或已断开的来源不回退到当前 hub；未知按钮、错误字段类型和非法路径不发送请求。去重区分组、通知 id 和可空的动作 id。
- 系统 API 和 UI context 放到 Shared，Watch 目标通过兼容编译；这只是 API 准备，不代表系统位置接线完成。

## 本地验证

Xcode 27.1；所有构建与测试均经 `~/.local/bin/xcode-slot` 顺序运行，模拟器为专用的原版 iOS 27.0 模拟器。

| 验证 | 结果 |
| --- | --- |
| `Persona build-for-testing` | 通过 |
| `PersonaTests` 全量 | 1455 个测试、212 个 suite 全通过 |
| `ExtensionSystemAPITests` / `ExtensionNotificationActionsTests` / `ExtensionNotificationRoutingTests` | 14 个定向测试通过，最终全量也包含它们 |
| `PersonaWatch build`，watchOS Simulator | 通过；最低 watchOS 10 未抬高 |
| `PersonaMac build`，generic macOS | 通过；没有启动 App 或抢焦点截图 |
| `ExtensionNotificationUITests`，液态玻璃 / 浅色 | 3/3 通过 |
| 同一 UI suite，液态玻璃 / 深色 | 3/3 通过 |
| `scripts/test/test_extension_notifications_mock.py` | 4 个测试通过 |
| `scripts/test/test_doc_paths.py` | 7 个测试通过 |
| `scripts/privacy_check.py` | clean |
| `scripts/gen/extension_schema.py --check` | 通过 |

UI 回归实际操作 SpringBoard 通知，覆盖文字回复、前台处理和点通知本身。mock 核对动作只发一次、原始 state 和输入文字、后台不打开页面；前台结果提示的合并无障碍元素实际存在且可见。

已查看通知按钮、前台动作结果、普通点按页面的深浅色截图。保存的普通点按页面证据：

- [液态玻璃 / 浅色](../screenshots/extension-notification-apple-glass-light.png)
- [液态玻璃 / 深色](../screenshots/extension-notification-apple-glass-dark.png)

此前失败没有作为通过：修复了 actor 传递非 Sendable SDK 对象、依赖工厂调用、夹具准备时序、系统回复栏的 TextView 定位、系统页面 toast 缺失及合并无障碍元素定位后，重新执行上述验证。

## 主分支同步后的补验

重基线到 `8520b954c` 后的代码：`79a7d4597e73d7074da4b9c6a2385177d4eeebd2`。唯一手工冲突位于 App 启动处：保留上游 `QuickAction.publish()`，合入紧邻的通知类别注册；其余提交自动重放。

- 同步后 `Persona build-for-testing` 通过；隐私与生成类型一致性检查通过。
- 定向测试补跑在 `xcode-slot` 中排队 40 分钟，日志只有「all 2 slots busy, waiting」，超时退出 124，**测试未开始**。依赖它的浅色 UI 补跑未执行；不能将它们记为同步后通过。
- 本次通知逻辑的完整验证仍以上述冻结的 1455 个单测和深浅色各 3/3 UI 回归为证据。没有用编译通过代替单测，也没有重跑或绕过全机槽位限制。
- PR 的隐私、脚本、schema 等 CI 检查实际为 QUEUED，没有运行或通过；按项目与用户约定，以已完成的本地验证合并。

## 未验证与未完成

- Xcode 26.0 环境不可用，26.0 未实编；不改变最低编译器政策。
- GitHub CI 不作为本地通过结果。项目自建 runner 停用时，按约定以本地验证合并；PR 单独记录 CI 实际状态。
- `simctl push` 不执行 NSE。本地通知共用类别注册逻辑，可证明系统按钮及 App 回调，不能证明真机直连 / relay APNs 首条后台通知的 NSE 送达；该边界仍待真机验证。
- 主屏幕小组件、Watch 表盘组件和 App Intents 还未完成接线。草稿未纳入本次提交，#1862 保持打开。
- 没有发布 TestFlight，没有访问 iCloud，没有改变生产配置、凭据或签名资料，没有发送真实 APNs 通知。
