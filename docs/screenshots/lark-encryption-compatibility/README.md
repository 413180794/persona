# 加密服务版本与授权提示

iPhone 18 Pro / iOS 27，隔离 mock Hub；无真实账号、密码或发送。

| 场景 | 浅色玻璃 | 深色 BeautifulUI |
| --- | --- | --- |
| Hub 缺少接口 | [升级 Hub](light-unavailable-hub.png) | [升级 Hub](dark-unavailable-hub.png) |
| lark-connector 缺少接口 | [升级 lark-connector](light-unavailable-lark-connector.png) | [升级 lark-connector](dark-unavailable-lark-connector.png) |
| 缺少独立原生授权 | [开启授权](light-unavailable-authorization.png) | [开启授权](dark-unavailable-authorization.png) |
| 升级后重新检查 | [选择外显](light-upgrade-recovered.png) | [选择外显](dark-upgrade-recovered.png) |

8 张截图已逐张检查。UI 回归验证三种失败均在进入外显页前说明原因；恢复服务后重新检查，输入和加密模式保留，再进入候选列表。现有解锁、错误密码、加密发送、失败重试、跨入口草稿保护的 3 个流程也在浅、深色通过。所有写入仅发送至 mock。
