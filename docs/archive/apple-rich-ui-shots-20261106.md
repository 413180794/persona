# Apple 富 UI 收尾：空态和截图（2026-11-06）

关联 #2024。此记录只覆盖本次空态修复、共享示例截图和 Mac 行高修复；不代替真实模型／正式后端的整体验收。

## 修复

- 空态／骨架同时检查合成后的聊天行、transcript、待发送／正在到达的消息和流式正文。只有活卡片时不再画「还没有消息」；真正空会话保留原提示。
- Mac 助手卡片行移除页面级 `.pluginLinks` 包装，避免 `GeometryReader` 把 lazy row 压矮；HTTPS 仍交给父 `ChatView` 的整页链接面板。iOS 原画法不变，没有补写死卡片高度。
- HTML fallback 明确提供可访问性标签和 ID；Mac 仍只展示摘要，不开启执行。
- DEBUG 批量截图一次启动真实 Mac App，使用独立 bundle id、临时 HOME、loopback fixture 和禁止激活策略。完成 feed 读取、正文标记和稳定布局检查后捕获；不置前、不操作鼠标、不为每张图重启。

## iOS 已完成矩阵

六共享示例为决策、比较表、进度、表单、图表和 HTML；每个单元六张，共 60 张。

| 风格 | 浅色 | 深色 |
| --- | --- | --- |
| glass | 通过并看图 | 通过并看图 |
| clay | 通过并看图 | 通过并看图 |
| island | 通过并看图 | 通过并看图 |
| lark | 通过并看图 | 通过并看图 |
| beautiful | 通过并看图 | 通过并看图 |

全部截图来自真实 App 的 `AssistantRichCardTests/testAllSharedSamplesAreVisibleAndCaptured`，HTML 等实际网页按钮进入无遮挡区域，不只等原生容器。实际看过每张内容，没有白页或活卡片上方的错误空态。首次缺失单元是编译槽排队超时，没有启动测试；只续跑缺失单元，原超时保留。

最后一次玻璃／浅色回归为 35 项、0 失败、0 跳过，含真正空会话、六共享示例和既有原生确认／多设备选择／版本冲突／恢复提交值／重复回复锁回归。Xcode 27.1、原版 iOS 26.5 模拟器；26.0 不可用、未实编。

## Mac 已完成截图

同一真实 `PersonaMac` 实例完成六共享示例 × 深浅色，共 12 张；原图为 2560 × 1800，未以组件测试窗口代替实际 App。`mac-shots-v6` 的六浅色／六深色正文区域均已实际查看，表单字段、提交、比较表、进度行、图表和 HTML 摘要完整可见。禁止激活策略未改变，原生窗口没有成为 key，未改真实钥匙串／配对。

较早批次只有标题、正文落在输入区后面，不能算通过；移除行内页面级几何包装后才有完整正文。readiness 还曾因 AX 协议 cast 得不到 frame、进度标签为合并文案、HTML 摘要没有显式 AX 标签而失败；失败日志与旧图都保留。`cacheDisplay` 的玻璃材质和后台系统控件颜色仍有已知限制，不能把布局截图当作屏幕材质／活跃窗口颜色验收。

## 证据与边界

有效 iOS 单元清单、xcresult、附件、源码指纹及失败记录在本任务的 `/tmp/richui-final-2024-evidence`；完成清单为 `completed-iOS-matrix.json`。Mac v6 全 App 原图和 `captures.json` 独立保留。后续主线整合候选的实际结果补记在本记录末尾，不把旧源码证据改写为新源码通过。

Mac 正常安全矩阵已另经 PR #4367 合并，记录见 [Apple HTML 的 Mac 正常矩阵](apple-html-mac-normal-20261106.md)。Mac execution gate 仍为 false，死循环／真实 OS 终止仍需可从外部销毁的隔离环境；没有硬资源配额或立即内存回收的保证。

没有真实主对话写入、生产开关变更、部署或 TestFlight。#4031 的其他交互语义由其原车道处理，不接管。

## 最新整合候选复验

候选 `4fe8f5d50ad9afebbc331dbea678b38b1a81b4c5` 已接入当时最新主线；冲突只保留主线的 `String(localized:)` 空态文案和本次纯计算判断，两边的经验条目均保留。

- `xcode-slot xcodebuild … -scheme Persona … build-for-testing` 退出 0；随后 `test-without-building` 回归 summary 为 35 项、0 失败、0 跳过（含一个两组参数的用例）。最终六共享示例截图再次实际查看，无空白或错误空态。
- `xcode-slot xcodebuild … -scheme PersonaMac … build` 退出 0；同一真实 App 再跑 12 张深浅色全窗口图。每个正文区域再次实际看过，表单、比较表、进度、图表和 HTML fallback 完整可见。
- `xcode-slot xcodebuild … -scheme PersonaWatch … build` 退出 0。手表本轮没有界面改动；已完成的进度／HTML 纯文本 fallback 截图沿用前阶段记录，不宣称重新截图。
- 最终证据：`merged-final/glass-light.xcresult`、`merged-final/glass-light-summary.json`、`mac-shots-final/captures.json` 及原图、`merged-final-*-build.log`，均在上述任务证据目录。

所有构建串行通过全机槽。Xcode 27.1，26.0 未实编；CI 未运行不写成通过。隐私扫描和文档路径测试在 PR 提交前再次检查。
