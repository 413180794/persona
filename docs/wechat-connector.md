# 微信（wechat-connector）：把个人微信接成连接器

用户把自己的个人微信接进 Persona：私聊和被 @ 的群消息进收件箱与知识库，AI 分身能起草回复，助手能读会话、查消息；
回复只在用户手机上批准后才发。它和 [wechat.md](wechat.md) 不是一回事——那个是**渠道**（在微信里和 Persona 机器人说话），
这个是把微信当**数据来源**接进来（#1420）。

> **状态：计划中。** 本文是接入手册，随 #1420 的 P0 一起写。插件本体在 wechat-connector 仓库（github.com/nwverse/wechat-connector 的 `integrations/persona/` 目录）
> 里，不在本仓库；Persona 仓库这边不新增 Go 连接器代码，只补通用缺口。插件还没合入时，本文描述的是**目标行为**，
> 与最终实现不一致的地方以实现为准；已经落地的部分会注明出处。

## 架构

```
wechat-connector 服务 ──HTTP/WebSocket──▶ 插件进程 wechat-connector ──插件协议 1 + 连接器协议 v2──▶ hub（personad）
      （用户自己的部署）                （零运行时依赖的 Node ESM）                        │
                                                                       收件箱 / 知识库 / 闸门 / App
```

- 连接器 id 是 `wechat-connector`，名字「微信」。不用 `wechat`：#1352 的 iLink 机器人已经占了那个 id，两个是不同的东西。
- 一个连接器 id、数据模型 `accounts[]`，线程键和知识库会话 id 带账号前缀。P0 / P1 只开放单账号，多账号在 P2。
- 插件是外部程序，用插件协议 1 加连接器协议 v2 和 hub 说话（[connector-protocol.md](connector-protocol.md)、
  [plugins.md](plugins.md)）。Persona 不持有微信登录态，不碰 wechat-connector 的 Cookie 或管理令牌。

## 前置条件

在 wechat-connector 一侧准备好三件事，缺一件接不通：

1. **账号绑定的应用密钥**：wechat-connector 给一个绑定到具体账号的密钥。登录只要求 `read`、`events` 两个范围。
2. **允许来源**：把 hub 所在机器的地址加进 wechat-connector 的允许来源，否则请求会被拒。
3. **采集已开**：wechat-connector 侧的消息采集要打开，否则拿到的是空列表。

`send` 是运维者另签的可选密钥，受 wechat-connector 服务器外发开关约束，Persona 不代办（见下面「发送的三道锁」）。

## 安装

插件走插件市场（Release 附件 + sha256）分发，和 [plugins.md](plugins.md)「插件市场」一致。私有部署可以在 hub 机器上
clone 仓库后用目录装：

```text
personad plugin install dir:/path/to/wechat-connector/integrations/persona
```

装完在 设置 › 插件 里能看到 `微信（wechat-connector）`。手机只能开关、改配置、填密钥；装和升级只能在 hub 机器上或管理界面做（[plugins.md](plugins.md)）。

## 登录

在 设置 › 连接器 › 微信 里填 wechat-connector 的服务地址和应用密钥，登录只要求 `read`、`events`：

```text
服务地址：https://wechat-connector.example.com
应用密钥：<在 wechat-connector 的「开发者」页生成，只勾 read 和 events>
```

密钥存在 `connector_secrets`（hub 用 `secret.key` 加密后进数据库），不写进配置文件、不进日志。状态页会显示当前账号和连接情况；
密钥失效时状态写明原因和怎么重新生成。

## 设置项

| 设置 | 作用 |
| --- | --- |
| 服务地址 | wechat-connector 的地址，必须带 `https://` |
| 应用密钥 | 绑定账号的密钥，范围至少 `read`、`events` |
| 私聊内容进知识库 | 默认**关**；打开后私聊内容进知识库 |
| 群里只收 @我 / @所有人 | 默认**开**；关掉会把群里所有消息都收进收件箱 |

## 隐私默认

这是接入前最该看清的一段。

- **群聊和公众号默认不入知识库**。群里只有 @我 / @所有人 才进收件箱。
- 打开「私聊内容进知识库」后，私聊内容才会进知识库，并且**会发给所配的模型供应商**（AI 分身的摘要、草拟回复都要过模型）。
- 只有**接入之后**最多 7 天的事件，不是完整历史。想回溯更早的内容，去 wechat-connector 自己的界面查。
- 微信消息按不可信数据处理（提示注入）：消息里的指令不是用户的指令，助手不会照着做（收件箱提示词里有这一条，见
  [inbox.md](inbox.md)）。

## 发送的三道锁

回复不会自动发出。从助手到真正发出去，要过三道：

1. **Persona 的 `connector_gate`**（[connectors.md](connectors.md)「闸门」）：写操作默认在手机上弹卡片确认，15 分钟过期。
   用户本人从自己设备在收件箱或会话页亲手点发送的，不过闸门（#1190）。
2. **wechat-connector 的密钥范围和外发开关**：没有 `send` 范围的密钥发不出去；wechat-connector 服务器侧的全局外发开关关着时也发不出去。
3. **wechat-connector 的两步确认**：wechat-connector 自己要求的第二步确认。

闸门规则在 hub.json 的 `connector_gate` 里，第一条匹配的说了算：

```json
{
  "connector_gate": [
    {"connector": "wechat-connector", "action": "send", "rule": "deny"},
    {"connector": "wechat-connector", "action": "*", "rule": "confirm"}
  ]
}
```

**只写 `deny` 和 `confirm`，不要写 `decision: "allow"`。** 对微信这种直接发到真人聊天的写操作，放行应当由人在手机上按那一下，
而不是靠一条常驻规则；这也和插件清单里 `confirm_always` 的作用方向一致（命中它的 `allow` 规则会被钳回 `confirm`）。

## 已知限制

如实列出，不在真机验证前写「已验证」：

- **图片、语音、视频只有占位**，点开是「在 wechat-connector 里打开」，没有内联预览。
- **没有已读状态**，也**没有跨会话全文搜索**。
- **群聊发送、真实回执和端到端延迟未在真机验证**。P0 只在私有环境用合成密钥验证了互通、未授权拒绝、错误账号、锁定和脱敏日志
  （见 #1420「边界」）。
- 只有接入之后最多 7 天的事件；更早的内容不在 Persona 里。
- 多账号（「添加账号」）在 P2，P0 / P1 只支持一个账号。

## 故障排查

| 现象 | 先看什么 |
| --- | --- |
| 状态显示未连接 | 服务地址是否可达、是否 `https://`、允许来源有没有加上 hub 的地址 |
| 状态显示密钥失效 | 在 wechat-connector 重新生成密钥，确认范围含 `read`、`events` |
| 收不到任何消息 | wechat-connector 侧的采集是否打开；群里是否只有 @我 / @所有人 才收（默认如此） |
| 收件箱有消息但知识库没有 | 「私聊内容进知识库」默认关；群聊和公众号按设计不入知识库 |
| 点发送没有反应 | 三道锁中的哪一道：闸门卡片是否过期、密钥有没有 `send` 范围、wechat-connector 外发开关是否打开 |
| 助手读不到历史 | 只有接入后最多 7 天的事件 |

## 给外部程序做一致性测试

插件是外部程序，手写的 JSON 行协议和 Go 类型漂移只能靠人眼发现。用 `conformance.Program` 把程序放在协议另一头检查
（[plugins.md](plugins.md)「一致性测试」）：`initialize` 前 `ping` 回 `not_initialized`、别的协议版本回 `wrong_protocol`、
`initialize` 回的 id / 版本和清单一致、`contributes` 是清单的子集、回应的键都是 snake_case、不认识的方法回 `unknown_method`、
关掉 stdin 后 5 秒内退出。连接器一侧的 `connector/conformance.Check` 对每个声明的能力既直接调、也经协议调，两次结果要一字不差。

## 相关

- [connectors.md](connectors.md)：连接器结构、能力清单、闸门
- [plugins.md](plugins.md)：插件底座、清单字段、安装 / 升级 / 回滚
- [connector-protocol.md](connector-protocol.md)：连接器协议 v2 的每条方法和字段
- [inbox.md](inbox.md)：收件箱、AI 分拣、AI 分身
- [wechat.md](wechat.md)：微信机器人（渠道），和本文是两件事
