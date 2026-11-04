# 快捷指令「问 Persona」App 意外退出（#990）

在 iOS 27.0 模拟器（24A5390f，和出问题的手机同一版本）上，用 **Release** 配置编译，运行快捷指令「问 Persona」（问题写死「你在干什么」）。App 连的是 `scripts/mock/mock_hub.py`，回答是假 hub 里写死的一句话。

| 文件 | 内容 |
| --- | --- |
| `before-fix.mp4` | 修复前（Release 去掉了反射名字）：报「“Persona” App 意外退出」，崩溃记录解到 `AskJarvisIntent.question.getter`（`AskPersonaIntent.swift:42`） |
| `after-fix.mp4` | 修复后（Release 保留反射名字）：不崩溃，卡片显示问题和回答，消息送到 hub |

原因和复现方法见 `docs/lessons/ios.md`「快捷指令运行「问 Persona」，App 意外退出」。
