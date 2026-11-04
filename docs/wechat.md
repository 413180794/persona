# 微信机器人：在个人微信里和 Persona 对话

用户在个人微信里私聊一个机器人，就是在和 Persona 说话：消息进「Persona」tab 的主对话（或用 `/new` 开的对话），这一轮的回答以机器人的身份发回微信；
Persona 和它驱动的 Agent 要用户回答、批准的，以文字发到微信，回复序号或文字就答了（#1352）。

走的是腾讯给个人微信开放的 AI 机器人接口 **iLink**（`ilinkai.weixin.qq.com`）：用户用微信扫码授权，得到一个机器人的 token，之后长轮询收消息、带
`context_token` 回消息。协议照腾讯开源的 [openclaw-weixin](https://github.com/Tencent/openclaw-weixin)（MIT，见 `THIRD_PARTY_NOTICES.md`）实现。

## 为什么做成插件

只对一个服务有意义的东西做成插件（`docs/plugins.md`），不往 hub 里加：它是 personad 旁边的单独程序 `jarvisd-wechat`，和 Telegram 的一样注册成**内置插件**
`wechat`（同一次构建发布、自动更新时一起换，`version -v` 和 hub 不一样就不启动）。它是一个**渠道**（`docs/channels.md`）：程序只管和微信说话，
「消息进哪个对话、等这一轮答完、答案发回、Agent 的提问」都是 hub 的统一管线（和飞书机器人是同一份）。

| 要做的事 | 用什么 |
| --- | --- |
| 扫码绑定、设置里的状态和开关 | 扩展点 `connectors`，连接器 `wechat` 只有 `status` + `login` 两个能力；登录是二维码步骤（`kind: qr`，#1351）。设置 › 插件 › 渠道 › 微信 里的「绑定」走的是同一个登录 |
| 存 token、游标、主人最新的 `context_token` | `connector.secrets`（hub 用 `secret.key` 加密存数据库），整份一个 JSON |
| 把主人的消息交给 hub、把 hub 给的话发回微信、「正在输入」 | 扩展点 `channel`（#1466）：程序调 hub 的 `channel.receive`，hub 调程序的 `channel.send`、`channel.typing` |
| 访问微信 | 权限 `network: *.weixin.qq.com`（展示用，hub 暂不拦截） |

第一版（#1447）用 `hub_api` + `events` 拼：插件拿着 hub 的 token 每 2 秒轮询会话、自己跟踪这一轮、自己把提问写成文字。#1466 把这些搬进 hub 的渠道管线，
插件不再拿 hub 的 token、不订事件；原来存在插件状态里的「发到哪段对话」、在等的回答和提问，hub 第一次启动新版本时搬过去（`hub/channels_migrate.go`），已经绑定的不用重新扫码。

进程跟着连接器的开关跑：开着才收微信消息。

代码：程序入口 `server/cmd/personad-wechat`（旧名 `jarvisd-wechat` 是别名）；iLink 协议 `server/internal/wechat/ilink`（不存状态、不打日志，只认腾讯的
HTTPS 源）；机器人 `server/internal/wechat`（`wechat.go` 主循环和存储、`login.go` 扫码、`chat.go` 收消息、发消息、「正在输入」）；声明 `server/internal/wechat/spec`
（连接器和渠道）；hub 的注册 `server/internal/hub/wechat.go`。管线在 `server/internal/hub/channels*.go`，规则在 `server/internal/channel`，
发给对话时加的说明 `server/internal/prompts/channel/message.md`。

## 绑定

设置 › 连接器 › 微信：打开「启用 微信」，点「登录 微信」，页面出二维码（hub 画好点阵发给 App），用微信扫一扫、在手机上确认，页面自己变成已连接。
在同一部手机上扫不了自己的屏幕：点「保存二维码」存到相册，在微信里扫一扫、从相册选它。

- 扫码的那个微信就是**主人**（iLink 的 `ilink_user_id`），只有主人的消息会转给 Persona；别人发来只回一句「这个机器人只为绑定它的人服务。」（同一个人 10 分钟一次）。
- 微信有时要输验证码（手机上显示一串数字），这时登录页变成一个输入框。二维码过期了自动换新的（最多 3 次）；开始 5 分钟后还没扫完，下一次问时换一个新的。
- 退出登录：通知 iLink 下线、删掉 token；旧 token 留着最近 10 个，下次授权时带上，iLink 认得是同一个机器人。
- 在别处重新绑定过（iLink 回 `errcode -14`）：自动解绑，状态写明「授权已经失效，重新扫码绑定」。

## 一条消息怎么走

1. 程序长轮询 `getupdates`（游标存在 secrets 里，重启接着收），按消息 id 去重（留最近 200 个）。记下主人最新一条消息的 `context_token`：iLink 发任何消息都要它。
   别人发来的只回一句「这个机器人只为绑定它的人服务。」，不交给 hub。
2. 主人的消息整理成 `channel.Message` 交给 hub（`channel.receive`）：文字（语音用微信自己的转写）、引用、图片（最多 4 张，解密后）；
   文件、视频还转不了，写进 `unsupported`（「文件「…」」）。
3. 之后都是 hub 的（`docs/channels.md`「一条消息怎么走」）：去重、命令、回答在等的提问、发进主对话或 `/new` 开的对话（`client_msg_id` 是 `wechat-<消息 id>`）、
   跟踪这一轮（找到这条消息、等它答完、后台任务答两次、合并、超时）、处理期间每 5 秒叫程序「正在输入」。
4. 答案发回：hub 按 3500 字拆好发给程序（`channel.send`），程序去掉微信显示不好的 Markdown（图片、删除线、五六级标题，中文上的斜体；粗体和代码保留）再发。
   主人还没发过消息（没有 `context_token`）时程序回 `cannot_send`。

## 提问和批准

微信没有卡片（渠道的能力 `asks: text`）。Agent（主对话，或微信正在驱动的对话）要你回答、批准时，hub 把卡片写成文字发到微信，你的下一条消息就是回答
（`channel.AskText` / `channel.ReadAnswer`）：

| 卡片 | 微信里怎么答 |
| --- | --- |
| 批准工具 | 回复「允许」（同意、可以、好…）或「拒绝」；别的字会再提示一次 |
| 计划 | `1` 批准并自动执行、`2` 批准且改文件时自动同意、「拒绝」打回；写别的就是打回并附上这段话 |
| 提问 | 回复选项序号（多选用空格隔开），或直接写回答；几个问题一个一个问 |
| 终端里的对话框（Codex 等） | 回复选项序号 |
| 要在手机上填写的、表单（elicitation）、读不出选项的终端画面 | 说一声「这个要在 App 里处理」 |

- 只转「微信在驱动」的对话：主对话、`/new` 开的对话、在等回答的对话。各台电脑上别的 Agent 的提问照旧只在 App 里。
- 主人还没给机器人发过消息时没有 `context_token`，发不出去：这时不记为「在微信里问了」，App 里照常能答。
- 同时有几个在等时，回复答的是最近发出的那个。

## 命令

hub 的渠道管线统一实现：`/help`、`/new [第一句话]`（之后的消息发给新开的对话）、`/main`（回到主对话）、`/stop`（停下当前目标这一轮，不再等它的回答）、
`/status`（发给谁、在等几个回答、几个问题）。「消息发到」也能在 设置 › 插件 › 渠道 › 微信 里选。

## 和飞书机器人比

| | 飞书机器人（`docs/bots.md`） | 微信机器人（这里） |
| --- | --- | --- |
| 在哪 | hub 里（`hub/bots*.go`） | 内置插件，单独的程序，只管和微信说话（`channel` 扩展点） |
| 跟踪这一轮 | hub 的渠道管线（同一份，`hub/channels_runs.go`） | 同左 |
| 绑定 | 填自建应用的 App ID / Secret，再发配对码 | 扫码 |
| 提问和批准 | 卡片按钮 | 文字往返 |
| 进度 | 进度卡片 | 「对方正在输入」 |
| 驱动各台电脑上的 Agent（`/agents`） | 有 | 没有（第一版） |
| 从这里发起的一轮还推手机通知 | 不推 | 不推（#1466 起和飞书机器人一样） |
| 推送 / 同步主对话 | `pushes` / `sync` | 同样的两个开关（渠道设置里，默认关） |

## 不做（第一版）

群聊（iLink 只有和机器人的私聊）、发语音和文件、微信消息进收件箱 / 知识库、`/agents`。

## 验证

- `go test ./internal/wechat/...`：iLink 协议对着假的 iLink 服务器（扫码各状态、跳转只认腾讯的源、长轮询超时、被顶号、加解密、Markdown 过滤和拆分）；
  程序对着假的 iLink 和假的 hub（绑定、主人的消息交给 hub、陌生人、被顶号解绑、图片和文件、重投去重、没有 `context_token` 时 `cannot_send`、「正在输入」）；连接器一致性测试。
- `go test ./internal/hub -run 'WeChat|Channel'`：hub 注册的清单（连接器、渠道、权限）、程序对着插件协议和渠道协议的一致性测试（`channel/conformance`）、
  统一管线对着假的渠道程序和假的 node（消息进主对话和回答发回、拆分、去重、文字审批、在 App 里答过、发不出去、命令、目标）、旧状态的迁移。
- App：`MOCK_WECHAT=signed_out` 的假 hub 跑 `QRSignInTests`（`docs/testing.md`）。
- 真实的微信：要用户自己的微信扫码，CI 和单元测试都覆盖不到，部署后在用户的 hub 上实测。
