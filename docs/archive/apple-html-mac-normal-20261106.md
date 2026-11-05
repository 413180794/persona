# Apple HTML：最新 Coordinator 的 Mac 正常矩阵（2026-11-06）

关联 #2025；本记录只覆盖正常／有界探针，不代表 Mac HTML 执行已完成全部安全验收。

## 源码和范围

- 基线：`8756ab8789dc97048e5e81515c7748979b9fac6c`；加本次终止回调回归测试。
- `AssistantHTMLCard.swift` 整文件 SHA256：`a839dae75bb23c48553f83a227db124f6884570b3c3a7b9df779a27e777766a5`。
- 请求类型 11–98 行与 Coordinator 295–525 行原样抽取；抽取代码 SHA256：`7bda24c4dca715a1c23ab139399b938bd2d72504501c81466090f67977ba0522`。
- Apple 安全测试源码 SHA256：`8b07569eba13dd3cff5741a5a9c6e62f432662e87b3280ce9dab7c37dfb98007`。
- 只将 UIKit 窗口替换为禁止激活的隐藏 AppKit 窗口，复用当前测试；没有第二套沙箱实现。另有独立 Mac 网络正对照。

## 实际结果

`python3 scripts/test/mac_html_security.py --output /tmp/richui-final-mac-security-v2`：构建退出 0；正常测试退出 0；13 个测试方法、21 个参数展开用例通过，耗时 38.241 秒。Swift Testing XML 按方法分组：13 个 testcase，0 failure、0 error、0 skipped。

覆盖表单 GET／POST、重定向、外部脚本／字体／CSS／SVG／媒体、系统弹窗、隔离存储、未知 handler／kind、子 frame 和父 bridge、当前 nonce、公开终止回调恢复，以及有界工作／计时器／四 MiB 缓冲的撤页清理。空闲监听器和资源探针各跑五轮；沙箱外网络可达正对照通过，沙箱内零连接断言保留。

新增回归实际覆盖：未 load 的终止 callback 不通知、重复 callback 只通知一次、dismantle 后迟到 callback 不通知。既有相同 HTML 重试、新 nonce 接收和旧 nonce 拒绝的回归也通过。

worker 的 PPID 为 1；没有停真实 node、按 User-Agent 筛请求或修改真实服务。全程没有激活／置前窗口。构建和测试经 `xcode-slot`，使用任务临时 HOME 和缓存。

Xcode 27.1；26.0 不可用、未实编，最低兼容政策不变。

## 失败记录与证据

第一次准备目录仅校验抽取／适配，没有执行测试。首次完整构建因 Mac `callAsyncJavaScript` 回调版使用了 async 版参数标签而失败；回调版改用公开的 `in:in:completionHandler:`，不改探针、正对照或期限。失败目录 `/tmp/richui-final-mac-security` 保留，不能写成通过；成功证据在上述 v2 目录的 manifest、生成源、`build.log`、`test.log`、`results-swift-testing.xml` 和状态文件。

## 最新主线复验

推送前发现主线在隔离规则失败提示中加入 `String(localized:)`；虽不改变网络隔离策略，也不能沿用旧 hash 冒充最新源码。分支 rebase 到 `ee3c0dd62adcd5defa0c5f49acf65b5254ff03dd` 后，在候选 `9ce9da7d289c6e396f28b6caeb0d9aa9b7fe4849` 的新目录 `/tmp/richui-final-mac-security-v3` 完整复验。

- 构建／正常测试退出 0；13 方法、21 展开用例通过，38.155 秒；XML 为 13 个方法、0 failure／error／skipped。
- 整文件 SHA256：`4cbf445016c7e6c609a31d8aa6cfa3dba42919f1a239ed251fbd8d6cd609fc1c`。
- 原样抽取 SHA256：`5fb60664a5f1904fc0be270caf014c02df7a293142d01c7100ee9b1fc997d37d`。
- 适配测试 SHA256：`147394d3a8b65456d21f7e7183d24eea6f6cc091ff4f11098122a2e0e5fd738c`。
- Mac 网络测试 SHA256：`1abc9dbdab13933dd8ef67453e36195e9d0ee7704cfe8831e8485647b4ff148f`。

## 未完成的边界

- 普通矩阵验证 Coordinator，不覆盖 SwiftUI 卡片／`NSViewRepresentable` 的整体运行接线。
- 终止回调是公开 delegate 故障注入，不是实际 OS 杀进程。
- 共享 Mac 登录会话未运行 literal loop；须有可从外部销毁的一次性 Mac VM／测试登录会话后再做。
- 不保证 CPU／物理内存立即回收，不宣称硬资源配额。
- Mac execution gate 仍为 false；生产开关不动，没有真实主对话写入、部署或 TestFlight。

#2025 继续保留上述完整执行验收边界，不因正常矩阵通过而关闭。
