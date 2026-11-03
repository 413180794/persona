# 调研原始笔记

产品名统一为 Persona（原 Jarvis），笔记文件名、源码证据、协议字符串、旧命令和 `~/.jarvis` 路径保留研究基线，属于历史 / 兼容引用，不代表迁移版本的新默认名称。当前名称与配置见 [命名与兼容](../../config.md#命名与兼容)。

> 2026-10-30 由并行的调研 agent 写成，属于 [重设计方案](../README.md) 的证据。只读调研，没有改代码。
> 前 8 份是第一轮；后 3 份是你定下「没有 Google 服务、公网直连优先 IPv6」之后补的第二轮（行号对应 master 的 ec11652）。
> 证据里的行号对应 master 的 77999ab，也就是 PR #84（Android 第一版）和另外约 20 个提交合入之前；之后的变化见 [../parity.md](../parity.md) 第 26 节。
> 笔记里「事实」和「判断」分开标注；性能数字只引用了文档或代码里写明是实测的值，其余都是代码常量或配置。

| 笔记 | 内容 |
| --- | --- |
| [bh-architecture.md](bh-architecture.md) | removed 的整体架构与工程方法：分层、并发模板、结果三态与幂等回执、作用域与代次、能力协商、存储、测试；借鉴 / 不借鉴清单 |
| [bh-herdr.md](bh-herdr.md) | removed 的 Herdr / Claude Code 客户端：会话页、原生交互、写操作安全、阅读工具；和 Persona Agents 的逐项对照 |
| [bh-lark.md](bh-lark.md) | removed 的 lark-connector 飞书客户端：阅读位置、已读门禁、发送幂等、断线补洞；Persona 飞书页的问题；搬 / 不搬清单 |
| [bh-ui-qa.md](bh-ui-qa.md) | removed 的设计系统、首次接入、原型与评审、验收分级、测试金字塔；Persona 设计系统的现状与采用率 |
| [jarvis-ios-arch.md](jarvis-ios-arch.md) | Persona iOS 的现状架构与技术债：`AppModel` 职责、同步、缓存、规则多处实现、平台无关逻辑清单（Android 必须一致的部分） |
| [jarvis-features.md](jarvis-features.md) | Persona iOS 的完整功能清单（213 项）与 Android 对应做法（[../parity.md](../parity.md) 的底稿） |
| [jarvis-protocol.md](jarvis-protocol.md) | hub 协议：鉴权与配对、111 条 HTTP 路由、WebSocket 事件、同步模型、手机工具、推送；Android 需要的服务端改动 |
| [android-tech.md](android-tech.md) | Android 技术选型（版本号附来源）：路线、模块、架构、网络、存储、UI 组件、系统能力、手机工具、测试、分发 |
| [hub-network-current.md](hub-network-current.md) | hub 网络现状核查（带行号）：怎么监听、配对和鉴权、health 和 WS 握手、心跳；实现公网直连要改的地方；兼容现有 iOS 要注意什么（`Envelope` 撞名、二维码格式、401 无限重连） |
| [public-direct-ipv6.md](public-direct-ipv6.md) | 公网直连调研（带来源）：国内家宽和手机的 IPv6 现状、光猫防火墙、前缀变化、封端口；地址发现；DDNS；多地址选路；Android 17 本地网络权限；TLS 和钉扎；加固；后台保活和运营商空闲超时 |
| [android-cn-roms.md](android-cn-roms.md) | 国产系统与版本适配（带来源）：Android 版本分布和 minSdk、各家份额和当前版本、各家的通知和后台限制与设置页跳转、常驻前台服务、Live Updates 支持、没有 Google 服务时的系统能力、分发和自更新、真机矩阵和云真机 |
