# 经验索引

踩过的坑、怪异行为和设计取舍，按主题分文件。动到某一块之前先读对应的文件；新经验当场写进对应的节（没有合适的就新开一节或新开一个文件，并在这里登记），
最常踩、跨模块的几条同时留一行在 `CLAUDE.md`「最常踩的坑」。和某个功能强相关的设计结论写进那个功能的文档（`docs/<主题>.md`），这里只记坑。

| 文件 | 什么时候读 |
| --- | --- |
| `workflow.md` | 多个 Agent 同时改仓库：模拟器、mock 端口、worktree、rebase、迁移号、等后台任务的脚本 |
| `agents.md` | 改 node 里驱动 Claude Code / Codex / TRAEX 的部分：herdr 的行为、认窗格和会话、配置目录、发送、`/effort`、生图 |
| `assistant.md` | 改「Jarvis」tab 的无头助手：ask_user、权限、后台任务、定时任务、飞书写操作闸门 |
| `server.md` | 改 hub / node 的进程、连接、推送、代理、机器监控、远程部署 |
| `transcript.md` | 改 transcript / rollout 解析、会话列表、分页、后台同步、折叠、App 的会话缓存 |
| `lark-mail.md` | 改飞书（lark-connector）和邮件（IMAP / SMTP / Gmail）、云文档通知 |
| `terminal.md` | 改终端 tab：herdr 读屏 / observe / 接管、滚动、键盘输入、宽字符 |
| `ios.md` | 改 iOS 界面：聊天列表与滚动、导航、布局与组件、Swift 与编译、系统能力与页面约定 |
| `macos.md` | 改 Mac App：兼容层、分栏、截图 |
| `testing.md` | 跑 UI 测试、截图、量帧率 / 跳动；模拟器和 XCUITest 的怪行为 |
