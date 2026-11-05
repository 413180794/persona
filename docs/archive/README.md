# 归档：阶段性记录

这里放已经结束的车道方案、真实数据验证、阶段性状态表和验收记录。它们记录的是**当时**的结论和证据，
现行文档、代码注释和经验条目会引用它们说明「为什么这样做」，但不再随代码更新：

- 文中的源码路径、提交号、命令和测试数量都以记录当时的基准提交为准，后来改名或挪动的路径不会回头改；
  要找现在的代码，按名字在仓库里搜。
- 现行的设计和规矩写在 `docs/` 下的主题文档和 `docs/lessons/`；这里的结论和现行文档冲突时以现行文档为准。
- 新的一次性记录（车道方案、验证报告、验收清单、审计快照）做完后直接放这里，不放进 `docs/` 顶层，也不进 CLAUDE.md 的文档索引；
  同时在下表加一行（`scripts/test/test_doc_paths.py` 会查）。

| 文件 | 内容 |
| --- | --- |
| `lanes/live-updates.md` | 2026-10-31 实时更新打断用户操作：三次复现、根因、测法和改前改后的性能数字 |
| `lanes/msg-render-plan.md` | 2026-10-29 消息读取与渲染链路的诊断与改造方案（对照 Ovis） |
| `lanes/msg-render-verification.md` | 2026-10-29 消息链路车道（阶段 2–4）的真实语料验证 |
| `lanes/session-verify.md` | 2026-10-31 会话读取和发消息的真实数据验证、每一跳耗时 |
| `android-p0-status.md` | 2026-10-31 Android 首版 P0 逐项状态（源码路径是包名改成 `persona` 之前的） |
| `android-p0-acceptance.md` | Android P0 第 4 轮联合验收清单 |
| `ios-p0-verification.md` | 2026-10-31 iOS 对 P0 hub 改动的接口验收记录 |
| `hub-contracts-report.md` | 2026-10-31 hub 契约核对 |
| `node-link-update-validation.md` | 2026-11-04 Persona Link 经 node RPC 更新常驻 personad 的验收：校验、空闲检查、原子替换、回滚与版本确认（#1158） |
| `android-capsule-2124.md` | 2026-11-05 Android 悬浮胶囊首版：跨 App 浮层、会话发送、扩展贡献的胶囊内容，验收证据与后续项（#2124） |
| `android-capsule-compact-2124.md` | 2026-11-05 Android 胶囊收成固定小尺寸，展开只留「未读 / 主对话 / 收件箱」三个入口（#2124） |
| `android-capsule-completion-2124.md` | 2026-11-05 Android 胶囊的完成提示：会话做完短暂显示约 4 秒，点击直达，不打断手动卡片（#2124） |
| `android-capsule-drafts-2124.md` | 2026-11-05 Android 胶囊的草稿经过完成提示和自动收回后仍保留（#2124） |
| `extension-model-web-1854.md` | 2026-11-05 扩展模型网页验收：贡献开关、依赖链停用、合集安装 / 卸载确认（#1854） |
| `extension-model-apple-acceptance.md` | 2026-11-05 Apple 扩展模型设置验收：贡献、合集与声明管理，iOS 构建与测试结果（#1861） |
| `extension-notification-apple-acceptance.md` | 2026-11-06 Apple 扩展通知阶段验收：类别注册、点击和前后台动作、深浅色 UI，以及 #1862 尚未完成的系统位置 |
| `assistant-rich-ui-predeployment-2026-11-06.md` | 2026-11-06 主对话富 UI：统一正式源码的真实模型预部署闭环、旧失败记录、SDK looks 未过与生产暂停边界（#2017） |
| `plan-telegram.md` | 2026-11-03 Telegram 连接器的方案（gotd、分步登录、收件箱、`tg` 命令行）；2026-11-05 起它是扩展（#1704；0.1.3 起代码在 `nwverse/persona-extensions`，#2057），不在本仓库 |
