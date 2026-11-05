# Apple 扩展模型设置验收（#1861）

2026-11-05 完成代码冻结及本地验证，代码提交 `214395985`。本记录只对应 #1861；已合并的 #1859／#1860 不重做，系统小组件、表盘、通知按钮回调和 App Intents 仍属于 #1862。

## 验证结果

使用 Xcode 27.1；Xcode 26.0 本机不可用、未实编，最低编译器政策不变。

| 验证 | 结果 |
| --- | --- |
| iOS `build-for-testing` | 通过 |
| 全量 `PersonaTests` | 1368 项、199 个 suite 通过 |
| `ExtensionManagementUITests`：玻璃浅色 | 9／9 通过 |
| `ExtensionManagementUITests`：BeautifulUI 深色 | 9／9 通过 |
| 玻璃深色、BeautifulUI 浅色 | 新增到期用例前各 8／8 通过，实际看过截图 |
| `PersonaMac`、`PersonaWatch` 模拟器目标编译 | 通过；Mac 不启动、不抢焦点，不做截图 |
| `scripts/privacy_check.py` | clean |
| `scripts/test/test_doc_paths.py` | 6 项通过 |
| `scripts/test/test_extension_management_mock.py` | 5 项通过 |
| `scripts/gen/extension_schema.py --check` | 通过 |

GitHub Actions runner 在本任务中停用，未运行 CI，不将其写成通过。依据项目的环境异常政策，以本地验证为合并门禁。

模拟器使用独立端口的 `scripts/mock/mock_hub.py`，启用 `MOCK_PLUGINS=1`、`MOCK_EXTENSION_MANAGEMENT=1`、`MOCK_EXTENSION_CARDS=1`。夹具只在内存中开关、安装和卸载，记录每次写请求，未连生产 hub。测试以 `app.launchPinned()` 启动，显式指定风格及深浅色，完成后关闭自己的模拟器。

## 已覆盖的行为

- 贡献独立开关，`can_disable: false` 项不可操作，其他配置草稿不丢。
- 停用和卸载首次被依赖者拦下，取消不发送 `cascade`，第二次明确确认后才执行。
- 合集预览完整列出各成员权限，同一次安装携带每个预览的权限摘要；有未解决依赖时禁止确认。
- 预览保持打开至最早成员到期，出现过期说明、安装按钮禁用，没有安装请求。等待到期只更新一次，不用列表里的空 `TimelineView` 占行。
- 卸载合集返回部分成员成功时，只报告实际成功数量，保留未报告成功的成员。
- 提示仅填输入框，不发送；分拣模板替换前确认，确认后只填草稿，不自动保存。
- 未选择扩展主题时既有主题不变；不合格主题不出现；选择后生效；远程贡献消失后退回默认。
- 远程主题回退造成真实页面重建，未保存的设置草稿和保存按钮状态仍保留。
- 旧 hub 不合成贡献开关，纯声明接口不请求；主题离线缓存、网络失败保留与跨 hub 隔离有单元测试。

## 截图证据

均为假数据，没有生产消息、凭据或测试员身份。已实际查看原图和总览；最终原图确认安装预览空白行已消失。

- `docs/screenshots/extension-management-apple-glass-light.png`
- `docs/screenshots/extension-management-apple-beautiful-dark.png`

## 失败与修正

失败不按 CI 环境异常跳过，修正后重跑：

- 动态角色色的 getter 每次创建新 `Color`，颜色身份不稳定：默认色静态缓存，扩展色在组合风格时创建并复用，原颜色测试保持。
- 历史成功安装用例的固定有效期已过期：改为动态有效期，另保留成员过期拒绝测试。
- 初始为空的 List 行未启动模板加载：由非空的页面加载并传入菜单。
- 原生菜单不保留 SwiftUI 的按钮 identifier：按可见标题定位。
- 输入后立即读取可访问性快照会少一个末字：先等待完整输入稳定；导航后的键盘焦点也要明确等待。
- `TimelineView` 在 List 内没有错误时仍生成空白行：改为整页等待最早到期时间，并增加真实到期 UI 测试。
- 同步后第一次 Mac 编译仍使用旧工程索引，漏编新增的上游来源文件：重新生成工程后 Mac／Watch 编译通过，不改那份源码。
- 上游夹具的隐私命中分别由 #2382、#2537、#2599 修复，Apple 分支同步已有修复；最终整仓 clean，没有接管其他平台的代码。

## 边界

没有访问 iCloud，没有读取或打印真实 token／密钥，没有改生产配置，没有部署 hub，没有发布 TestFlight。Mac 界面只编译，未截图；不能将这些结果推导为 #1862 或原 Apple 总任务已全部完成。
