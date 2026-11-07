# Apple 原生端可逆兼容验收（#6152）

2026-11-07。此报告记录原生端修复及本地证据，不取代真实设备或服务端全链路验收。原先的硬切换报告保留在 [原生验收记录](persona-native-6152-acceptance.md)，本次采用新的双向兼容要求。

## 结论与边界

- 偏好、旧形象选择、未发送数据和 Shortcut 的原位升级与回装已验证；迁移不删除旧存储。
- 旧 Shortcut 的系统操作身份已恢复：旧版通过系统 UI 保存的操作，在新版实际执行，原问题没有补填，合成 hub 收到完全相同的文本；结果卡片显示预期答案。action blob 在升级、回装及执行后的 SHA-256 均未改变。
- **旧 Live Activity ID 保留未通过**。模拟器在安装阶段丢弃原 ID；重复安装完全相同的旧构建也出现相同结果。新建活动及真实类型注册不算旧 ID 保留证据。未宣称真机 App Store 升级或已安装控制中心控件的连续性通过。
- 只使用原注册 Bundle ID、App Group 和配置中的访问组，构建号 1 → 2 → 回装 1 → 再装 2；没有 uninstall、erase，也没有注入新配对来伪装升级保留。
- 没有部署、重启线上、执行维护 apply、发 TestFlight 或恢复发布 loop。网络/协议兼容在 #6277 接入，不能从本报告推导为全链路已完成。

## 实现清单

| 对象 | 兼容策略 | 验证范围 |
| --- | --- | --- |
| 个人 Agent 偏好 | 有效新键优先，缺失/损坏时读取旧键；启动只补缺；显式保存镜像旧键 | App 与 App Group 两域分离，冲突不覆盖，旧数据不删除 |
| 形象、终端主题 | 历史内置 ID 精确归一化，保留 revision 和自定义 ID；保存镜像旧值 | 新端遵守 Mochi/Milk Frog 产品策略；旧原值仍在，回装旧版可读 |
| 推送密钥 | 双向只补缺失账户，保留字节、访问组及保护属性，读回确认；冲突保留两份 | Fake Keychain 的幂等、锁定、写入/读回失败；不会生成替代身份来绕过失败 |
| 加密推送 | 两种 HPKE context，各用独立 recipient；沿用同一时效检查 | 错误密钥、无效 sealed envelope 及外层路由字段均不能影响已认证的路由；NSE 解密成功后重建 userInfo |
| App Intents | 公开操作保持稳定 ID；隐藏适配器接纳中间版本身份 | 编译 metadata 读回，四个中间身份均不 discoverable；真实旧 Shortcut 执行 |
| Controls | 主要控件保持旧稳定 kind；中间 kind 继续注册，共用绘制/操作配方 | 严格源码守卫和构建；已安装系统实例/真机连续性未验收 |
| Live Activities | 保留真实旧 nominal attributes 类型，同时接纳中间类型；新建及 push-to-start 使用稳定类型 | 构建、运行新活动；覆盖前旧 ID 与同构建安装对照均明确记录为不保留 |
| 草稿、发件箱、录音 | 不递归重写或删除；既有格式继续读取 | 实际 draft/outbox JSON、未送达 AAC 段与录音清单的解码及字节校验 |
| Session 快照 | 保留 canonical encoder，额外写出旧 started-by 字段 | 共享 Session 编码兼容；网络 lane 独立检查真实 JSON，此处不冒称重复实测 |
| 系统归档 | 不递归改写或清理 | 项目生产路径未直接使用 NSKeyedArchiver/NSKeyedUnarchiver/NSSecureCoding；系统 Shortcut blob 实测，未枚举全部框架私有归档 |

历史字面量与真实兼容类型集中在 `apple/Shared/NativeStateMigration.swift`，守卫只允许这一个物理源文件；独立 target 可编译其 storage-only 子集，不复制第二份常量，也不放宽全局旧名扫描。

## 同身份运行证据

1. 旧构建保留已有合成配对，保存一条带具体问题的 Shortcut，准备真实格式的偏好、草稿、发件箱及录音段。
2. 新构建覆盖后，从真实 App 的只读探针确认原配对、置顶 Agent、29 字符草稿、一条发件箱消息、一份录音及一个未送达段。App/Group 两域的新旧偏好及旧形象原值均保留。
3. 冷启动第二次，再回装旧构建，原偏好、旧形象和未发送数据仍可读取，监测文件校验和不变；再次升级仍可用。
4. Shortcut 的保存和执行分别由系统 UI 完成；执行不补填参数，不改 action blob。系统结果卡片由独立 Shortcuts UI 服务渲染，验收用屏幕文字识别及实际截图确认，不把宿主 App 的 AX 树缺少结果误判为操作失败。
5. 另做旧构建 → 相同旧构建安装对照，旧活动 ID 同样丢失。系统记录活动在安装阶段被视为 uninstalled。此结果是限制，不是通过项。

存储验收的合成 WebSocket 故意离线，避免队列自动重放；Intent 的 HTTP 请求仍可执行。截图中出现的活动是新样例，不是旧 ID 保留证据。

## 构建与测试

- 工具链：Xcode 27.1；所有 Xcode build/test 使用 `xcode-slot`。Xcode 26.0 不可用、未实编；最低编译器与 watchOS 10 政策未改变。
- 最终代码冻结 `d221efa5743e7b0f6ca540eae98be4fe8343e001`（之后只补文档/截图）：iOS 全量 **2411 通过、1 失败、4 跳过**，原始 xcodebuild 退出 65。唯一失败是 WatchPet 已有的两条状态文案重复；下面给出干净 master 的独立复现，不将这次全量写成全绿。用户明确允许此精确基线失败不阻塞 #6296，其他新增失败仍须修复。
- Mac 全量在同一代码冻结上 **371/371，通过、0 跳过**。
- 旧 Shortcut 系统执行：1/1，通过、0 跳过；原问题完整到达合成 hub，系统结果卡片及执行后 blob hash 均验证。
- 单测 host 图标隔离：相关集中回归 36 通过、0 失败、1 项既有 relay opt-in 跳过；真实系统图标 UI 1/1，通过、0 跳过，已查看形象页和主屏幕截图。
- 改名静态守卫 21/21、文档路径守卫 11/11，通过。
- 附加外部服务引用扫描：完整仓库 2 通过、1 失败，三处命中均在已有 WatchPet 测试和素材生成脚本；干净 master `4cc38f6344880f52de93c7791fa96c399fca3115` 用同一 pytest 命令复现相同三处。本 PR 改动文件的原扫描谓词命中为 0，未修改扫描器或该模块。完整扫描未写成通过。
- CI：不宣称已运行或通过。

四项既有可选集成测试没有配置其专用运行条件：`AssistantHTMLSecurityTests` 的真实无限循环回收、`NotificationServiceTests` 的 relay 产物读回、`VideoFileResourceTests` 的真实 HTTP 范围读取、`VideoFileTests` 的原生播放。未改变它们的启用条件或断言，未将跳过写成通过。

第一次 iOS 全量在 runner 建立连接前挂起，实际单测未执行；模拟器队列等待 20 分钟超时后也没有启动测试。后续两条独立运行采样定位到真实系统图标队列阻塞，非 Native/Keychain/Swift once。DEBUG 单测 host 隔离该系统访问，业务测试仍使用 FakeSystem，UI 测试目标 App 和 Release 保持真实 UIApplication 路径。

## 唯一已有基线失败

`StateNamesTests.areDefinedOnce()` 报告 WatchPet 两条中文计数状态词重复。问题已在 [#6272](https://github.com/nwverse/persona/issues/6272) 交给原认领负责人，未新建重复 issue，也未修改 WatchPet 或放宽守卫。

本任务自行创建干净 detached `origin/master`，SHA `4cc38f6344880f52de93c7791fa96c399fca3115`，开始与结束的 `git status --porcelain` 均为空。用原测试的 suffix、names、old、count、Foundation catalog 读取及扫描谓词重放，仅把测试框架的 unwrap/assert 换为 guard/exit。编译退出 0，扫描退出 1；这不是完整 master Xcode 单测。

复现入口：[Foundation 谓词重放脚本](reproduce-state-names-baseline.py)。在设置了有效 `DEVELOPER_DIR` 的本机运行：

```sh
git worktree add --detach /tmp/persona-state-baseline 4cc38f6344880f52de93c7791fa96c399fca3115
git -C /tmp/persona-state-baseline status --porcelain
python3 -I docs/archive/apple/reproduce-state-names-baseline.py /tmp/persona-state-baseline
```

输出（退出 1）：

```text
%lld finished [Watch Pet]: %1$lld 个做完了
%lld waiting for you [Watch Pet]: %1$lld 个在等你
```

另一个任务的独立 clean-master 证据见 [#6277 评论](https://github.com/nwverse/persona/pull/6277#issuecomment-6703275514)，冻结 `f3b760a85db187564c0c2a4010c97bd5ff0d9c9e`，结论相同。没有把 peer 的专项重放冒称为本任务 Xcode 验收。

## 已检查的合成截图

Development images were removed from the repository. The historical observations below remain as text-only evidence; see the associated PR for available attachments.

原位升级后的同一 App 容器中，个人 Agent 偏好的置顶 `fixture-agent` 仍显示为已置顶；筛选按钮保持非默认状态，菜单里的自定义分组删除入口仍可用。截图测试没有注入新配对或修改偏好，合成来源的 prefs 接口固定返回 404，让 App 读取保存的本机旧值。此项系统 UI 测试 1/1 通过。




新版形象精简策略将已移除的 Visor 在显示层回落到 Mochi，但不销毁原始选择；回装旧版后，设置页仍显示 Visor。以下为同身份回装与再次升级的实际截图，不代表新版本恢复了已移除的形象列表。



截图内容均为合成数据，已实际查看；发布副本移除个人元数据。失败日志、采样、JSON、校验和及 xcresult 留在本机受限权限证据目录，原始附件不进入仓库。
