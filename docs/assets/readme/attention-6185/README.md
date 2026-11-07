# Apple 正常待办的配色对照

截图来自 App 内置演示场景或本地 Watch 合成数据，不包含个人账号、真实消息或个人电脑身份。图片已清除非视觉元数据，只保留通用 ICC 色彩配置。

## 改前、改后

| 平台 | 场景 | 浅色 | 深色 |
| --- | --- | --- | --- |
| iOS | Agents | `before/ios-agents-light.png` → `after/ios-agents-light.png` | `before/ios-agents-dark.png` → `after/ios-agents-dark.png` |
| iOS | 计划 | `before/ios-plan-light.png` → `after/ios-plan-light.png` | `before/ios-plan-dark.png` → `after/ios-plan-dark.png` |
| iOS | 权限 | `before/ios-permission-light.png` → `after/ios-permission-light.png` | `before/ios-permission-dark.png` → `after/ios-permission-dark.png` |
| Mac | Agents | `before/mac-agents-light.png` → `after/mac-agents-light.png` | `before/mac-agents-dark.png` → `after/mac-agents-dark.png` |
| Mac | 计划 | `before/mac-plan-light.png` → `after/mac-plan-light.png` | `before/mac-plan-dark.png` → `after/mac-plan-dark.png` |
| Mac | 权限 | `before/mac-permission-light.png` → `after/mac-permission-light.png` | `before/mac-permission-dark.png` → `after/mac-permission-dark.png` |
| Watch | 需要你 | Watch 使用深色界面 | `before/watch-needs-you-dark.png` → `after/watch-needs-you-dark.png` |
| Watch | 权限 | Watch 使用深色界面 | `before/watch-permission-dark.png` → `after/watch-permission-dark.png` |

`after/beautiful/` 是 iOS BeautifulUI 风格的额外浅深色截图。未修改第三方 UI 库源码。

## 捕获方式与验收边界

- iOS：`CalmAttentionShots/testDemoApprovalScenes()` 使用 `launchPinned()`、演示模式、清除聊天恢复状态和显式外观。PNG 从 XCTest 截图附件导出。
- Mac：DEBUG `macSnapshot` JSON 计划中的 `demoScenes` 在同一进程中捕获多个场景和两种外观，不激活窗口。独立 App 标识隔离截图偏好，只复制分栏布局尺寸，不复制账号或内容。
- Watch：既有 DEBUG `pairURL` 和 `openWatchItem permission` 参数连接本机 loopback 合成服务，只返回示例权限。专用 Watch 模拟器拍完关闭并删除。

演示计时和已读标记可能随启动变化。正文、审批协议、分组默认值与界面风格不变；正常待办使用小面积强调色和中性背景，真正失败与危险操作保留红色。

被拒收的捕获尝试仅保留在本机日志。iOS 和 Mac 测试以 `xcresulttool get test-results summary` 的实际结果验收，不仅检查命令退出码。Watch 必须实际查看图片；请求成功不代表数据已正确解码。
