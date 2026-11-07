<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/readme/banner-dark.zh-CN.svg">
  <img src="docs/assets/readme/banner-light.zh-CN.svg" alt="Persona — 你的 SuperAgent，一个入口，解决所有问题。" width="100%">
</picture>

[English](README.md) · 简体中文

**你的 SuperAgent：一个入口，解决所有问题。**

不只是聊两句，而是把想做的事变成能跟进、能完成的任务。Persona 把 Agent、设备和你的个人上下文连进同一个对话，从最初的提问、下一次决策，一直推进到事情完成。

[![Clients: iPhone · Apple Watch · Mac · Vision Pro · Android · Web](docs/assets/readme/badge-clients.svg)](#文档)

[![Agents: Claude Code · Codex · DSH · pi · opencode](docs/assets/readme/badge-agents.svg)](docs/runtimes.md)

[![Engines: herdr · Herdr App · Orca · Paseo](docs/assets/readme/badge-engines.svg)](docs/engines.md)

[先试试看](#先试试看) · [能做什么](#能做什么) · [怎么工作](#怎么工作) · [文档](#文档)

</div>

## 能做什么

<table>
  <tr>
    <td width="25%" valign="top"><img src="docs/assets/readme/icon-assistant.svg" width="32" alt=""><br><b>一个 SuperAgent</b><br>
      说出来，或打字告诉它。助手结合你的上下文，把事情拆成任务，再交给 Agent 动手。<br><a href="docs/assistant.md">助手</a> · <a href="docs/voice.md">语音</a></td>
    <td width="25%" valign="top"><img src="docs/assets/readme/icon-handoff.svg" width="32" alt=""><br><b>手机电脑，无缝接着干</b><br>
      在手机上看进度、回答问题、批准计划；回到电脑前，接着做的还是同一个会话，不是副本。<br><a href="docs/engines.md">引擎 →</a></td>
    <td width="25%" valign="top"><img src="docs/assets/readme/icon-extensions.svg" width="32" alt=""><br><b>全面的扩展系统</b><br>
      工具、Agent、数据源和 App 内体验都能扩展，配套 SDK 和扩展市场，也能自己动手做。<br><a href="docs/extensions.md">扩展 →</a></td>
    <td width="25%" valign="top"><img src="docs/assets/readme/icon-island.svg" width="32" alt=""><br><b>不打开 App，也能办事</b><br>
      灵动岛看进度，Siri 和快捷指令发起任务，Apple Watch 抬腕就能回答。<br><a href="docs/live-activity.md">灵动岛</a> · <a href="docs/siri.md">Siri</a> · <a href="docs/intents.md">快捷指令</a></td>
  </tr>
  <tr>
    <td width="25%" valign="top"><img src="docs/assets/readme/icon-inbox.svg" width="32" alt=""><br><b>知识库和收件箱</b><br>
      让 Agent 共用上下文，不用一遍遍解释。消息、邮件汇入同一个收件箱，AI 先分拣，再由你安排怎么处理。<br><a href="docs/knowledge.md">知识库</a> · <a href="docs/inbox.md">收件箱</a></td>
    <td width="25%" valign="top"><img src="docs/assets/readme/icon-phone-data.svg" width="32" alt=""><br><b>让 AI 用上手机数据</b><br>
      把日历、提醒、健康、位置、照片带进任务。开放哪些由你决定，建日程、写提醒先请你确认。<br><a href="docs/phone-tools.md">手机工具 →</a></td>
    <td width="25%" valign="top"><img src="docs/assets/readme/icon-connectors.svg" width="32" alt=""><br><b>连接你已有的工具</b><br>
      接入飞书／Lark、邮件、GitHub、iMessage、Telegram，也能通过飞书、微信等聊天渠道直接找 Persona。<br><a href="docs/connectors.md">连接器</a> · <a href="docs/channels.md">渠道</a></td>
    <td width="25%" valign="top"><img src="docs/assets/readme/icon-clients.svg" width="32" alt=""><br><b>每块屏幕，都有它</b><br>
      iPhone、Apple Watch、Mac、Vision Pro、Android、Web，让各端的对话和正在做的事始终连在一起。<br><a href="docs/ios.md">Apple</a> · <a href="docs/mac.md">Mac</a> · <a href="docs/vision.md">Vision</a> · <a href="docs/android.md">Android</a> · <a href="docs/web.md">网页</a></td>
  </tr>
</table>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/readme/hero-dark.webp">
  <img src="docs/assets/readme/hero-light.webp" alt="Persona 的 iPhone App：助手、Agent 实时进度、提问、计划审批、收件箱和电脑" width="100%">
</picture>
<p align="center"><sub>中文界面的演示数据：Agent 问，你回答，它接着干。</sub></p>

## 先试试看

**[加入 TestFlight 公开测试](https://testflight.apple.com/join/vsjfy3hE)** · 配对页点「**没有 hub？先体验一下**」，不需要服务器，也不需要账号。

先在 **Persona** 里跟进一个示例任务，回答 Agent 的问题或批准计划，再逛逛 **Agents**、**收件箱**、**待办**和**知识库**。演示还包括电脑状态、额度窗口、模型设置和外观选择，不是一张只能看的聊天截图。

演示只使用示例数据：回答是模拟的，不会运行真实程序，也不会发消息或邮件。真实 AI、语音、手机数据操作和跨设备能力，需要连接你自己的环境。

想接入自己的数据？**[开始使用](docs/getting-started.md)** 带你装好 hub、配对手机、连上电脑。

## 怎么工作

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/readme/architecture-dark.zh-CN.svg">
  <img src="docs/assets/readme/architecture-light.zh-CN.svg" alt="设备、个人上下文和 Agent 在 Persona 汇合：助手、收件箱、知识库和可跟进的任务" width="100%">
</picture>

**你来决定，Persona 串起上下文、安排工作，Agent 负责动手。** hub 把各部分连起来，Agent 在你的电脑上工作，经过授权的手机操作在手机上执行。技术细节放在 **[架构](docs/architecture.md)** 和 **[网络与安全](docs/network.md)** 里。

## 文档

| 从这里开始 | 继续了解 |
| --- | --- |
| **使用 Persona** | [开始使用](docs/getting-started.md) · [配置](docs/config.md) · [Docker](docs/docker.md) |
| **连接电脑** | [Persona Link](docs/link.md) · [Windows](docs/windows.md) · [引擎](docs/engines.md) · [工作区看板](docs/workspace-board.md) |
| **扩展 Persona** | [扩展](docs/extensions.md) · [连接器](docs/connectors.md) · [Runtime SDK](docs/runtimes.md) · [API](docs/protocol.md) |
| **和 Agent 一起开发** | [约定与完整文档索引](CLAUDE.md) · [issue → 认领 → PR](docs/collaboration.md) · [测试](docs/testing.md) |
| **运维或升级** | [服务端](docs/server.md) · [网络](docs/network.md) · [迁移](docs/rename.md) |

<sub>各端及运行时的支持范围取决于平台和安装的集成，详见上方指南；docs 文档为英文。[第三方组件许可](THIRD_PARTY_NOTICES.md)。</sub>
