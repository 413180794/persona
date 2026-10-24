# iOS / watchOS App

XcodeGen 工程 `ios/project.yml`（`Jarvis.xcodeproj` 不入库）。iOS 26 / watchOS 26，Swift 6，默认 MainActor 隔离。依赖 MarkdownUI。
Bundle ID `com.chirpet.jarvis`、`com.chirpet.jarvis.watchkitapp`。界面中文。

## 信息架构

| Tab | 内容 |
| --- | --- |
| 对话 | **Jarvis 助手**：example-machine 上 `~/.jarvis/assistant` 里的 Claude（默认启动器 `ccc`，设置里可换），CLAUDE.md 由 hub 生成，告诉它怎么访问各台电脑（ssh）、飞书等公司数据（lark-* / provider-* skills）、iPhone（phone_*）和邮件（mail_*）。首页是问候 + 可用设备 + 6 个建议；发出第一句立即显示气泡和「正在准备」，后台起会话。对话里隐藏思考，工具调用折成一行「用了 N 个工具 · 电脑、iPhone…」。左上角历史对话，右上角新对话；首次启动自动接上最近一段 |
| 开发 | 原来的会话列表（不含助手对话）：「需要你处理」卡片 → 进行中 → 刚完成 → 空闲（折叠）→ 最近；搜索、按机器筛选、左滑中断、右滑关闭 |
| 邮件 | 合并收件箱 / 单账号，详情（纯文本或 HTML）、回复、写信、「让 Claude 处理」（带上 mail_read 参数新建对话），邮箱管理 |
| 设置 | 助手（启动器 / 模型）、机器（二级页：在线状态、会话数、版本、启动器、重连）、手机数据开关和授权、通知、重新配对 |

对话页：

- 消息：用户气泡（珊瑚色）、助手 markdown（无气泡，阅读优先）、思考（折叠一行）、工具步骤组（>2 步折叠成「N 个步骤 · 运行 5 · 读取 3」，
  永远露出最后一步）、待办清单（TodoWrite 直接渲染）、Edit 的红绿 diff、Bash 命令和输出、图片。
- 实时状态条：claude 工作时显示 spinner 行（「Cogitating… 12s · ↓3.2k」），点开看输出预览；右侧进终端。
- 交互面板：贴在输入框上方。提问（多题切换、单选 / 多选 / 自己写，单题单选点一下就提交）、计划（markdown 预览 + 全屏、批准并自动执行 /
  批准并自动接受编辑 / 打回写意见）、授权（允许 / 拒绝 / 本会话总是允许）、终端（屏幕快照 + 解析出的编号选项 + 按键面板 + 发文字）。
- 输入框：照片（最多 4 张，压缩到 1600px）、快捷指令（继续、/compact、/context、提交并推送、汇报进展），工作中变成红色停止键。
- 导航副标题：机器 · 模型 · 状态。

## 设计语言

- 主色 Claude 珊瑚 `#D4683F`（深色 `#E8835C`）；状态色：工作中珊瑚、等你琥珀、空闲绿、刚完成蓝、未运行灰（`Shared/Theme.swift`）。
- iOS 26 Liquid Glass：工具栏、交互面板、输入框、提示条用 `glassEffect`；按钮 `.glass` / `.glassProminent`。
- 触感：发出 / 回答成功 success，新交互 warning，出错 error。
- 图标：夜色背景 + 珊瑚光环 + 四角星（`scripts/make_icon.py`，含 dark / tinted）。

## 数据流

- `AppModel`（`Jarvis/App`）：配对信息存 Keychain；`LiveConnection`（WebSocket，指数退避重连，重连后按 `since_seq` 重新订阅）；
  每个会话一个 `ChatStore`（按 id 去重、按 seq 排序、`buildRows` 生成展示行）。
- 进入对话页 `subscribe`，离开 `unsubscribe`；hub 推 `items` / `screen` / `sessions` / `interaction`。
- 手机工具：hub 推 `tool_call` → `PhoneToolRegistry.call`（写操作先弹确认）→ `tool_result`。连上就 `register_phone` 声明工具（`docs/phone-tools.md`）。
- 推送：配对后申请通知权限并注册 APNs，`POST /api/v1/devices`；hub 有 `apns.json` 时对新交互和「完成了」推送。前台且正在看该会话时不弹。
- 手表：iPhone 经 WatchConnectivity 把配对信息和摘要（运行中会话、待处理交互）同步给手表；手表打开时每 8 秒直接请求 hub 刷新，
  回答 / 发消息 / 中断也直接调 hub。

## DEBUG 启动参数（仅 Debug，Release 检查不含这些字符串）

`-pairURL jarvis://pair?...`（免扫码配对、不弹通知授权）、`-openSession <machine>/<session>`、
`-showNewChat YES`、`-tab dev|mail|settings`、`-openAssistant <machine>/<session>`、`-newAssistant YES`。`scripts/sim.sh` 封装了编译、安装、配对、启动。
