# 邮件

`server/internal/mail`：让 Claude 和 App 都能收发用户的邮箱。走标准 IMAP + SMTP，不依赖任何第三方中转服务；
Gmail 用「使用 Google 账号登录」（OAuth2 + XOAUTH2），额外支持用 Gmail 搜索语法（`X-GM-RAW`）检索。凭据加密后存在 hub 的 `dataDir`（默认 `~/.jarvis`）里。

## 能力

| 能力 | 说明 |
| --- | --- |
| 多账号 | 每个账号独立的 IMAP / SMTP 主机（域名预设自动填），可同时挂 Gmail、QQ、163、Outlook、iCloud…… |
| 测试连接 | 新增账号时先真正登录一次 IMAP 和 SMTP，成功才落盘；失败返回错误，不入库 |
| 邮箱列表 | `LIST` + SPECIAL-USE，映射出 `inbox/sent/drafts/trash/archive/junk` 角色，带未读数与总数 |
| 收件箱浏览 | 按 UID 倒序分页（`before_uid`），一次取 `limit` 条 |
| 搜索 | Gmail 用 `X-GM-RAW`（支持 `from:`、`subject:`、`has:attachment`、`newer_than:7d` 等）；其他服务商用 `SEARCH TEXT` |
| 摘要 | 只取 2048 字节正文片段，不下载整封信；摘要取正文前 160 个字符 |
| 合并收件箱 | 不传 `account_id` 时并发拉取所有账号的 INBOX，按日期倒序合并 |
| 读信 | `BODY.PEEK` 取全文，go-message 解析纯文本 / HTML / 附件元数据；**不会**自动标记已读 |
| 已读 / 星标 | 只改 `\Seen`、`\Flagged` 两个 flag |
| 发信 | go-message 生成 MIME，UTF-8，中文主题和中文姓名都按 RFC 2047 编码；支持 `in_reply_to` / `references` 串线程 |
| 发件箱留存 | 非 Gmail 账号（SMTP 不会自动保存副本）发完后 `APPEND` 到 `\Sent`；失败只记日志，不影响发信结果 |
| MCP 工具 | `mail_accounts`、`mail_search`、`mail_read`、`mail_send`；发信前一定先让用户在手机上批准 |

HTTP 路由、JSON 字段与 `docs/protocol.md` 的「邮件 API」一节严格一致；密码永远不出现在响应里
（`json:"-"` + 独立存储文件）。

## Gmail 接入方式：使用 Google 账号登录（OAuth2 + XOAUTH2）

Google 已经不让第三方客户端用 Google 账号密码登录 Gmail（帮助中心「将 Gmail 添加到其他电子邮件客户端」：改用
「使用 Google 账号登录」）。所以 Gmail 跟 Apple Mail、Thunderbird、Spark 这类客户端一样走 OAuth：
App 里点「使用 Google 账号登录」→ 系统浏览器打开 Google 选账号、授权 → 回到 App → 账号添加好，不用填任何密码。
（2026-10-24 曾经移除过 OAuth：当时的做法要用户自己建 Google Cloud 客户端、手工换 refresh token；
2026-10-25 用户要求改回点按钮跳 Google 授权的方式，应用专用密码入口从 App 里去掉了。）

### 流程

```
App                         hub                                Google
 │ POST oauth/google/start   │                                   │
 │──────────────────────────▶│ 生成 state + PKCE verifier（内存，15 分钟）
 │◀── auth_url, scheme ──────│                                   │
 │ ASWebAuthenticationSession 打开 auth_url ────────────────────▶│ 选账号、同意
 │◀──────────── com.googleusercontent.apps.<id>:/oauth2redirect?code&state
 │ POST oauth/google/finish {state, code}                         │
 │──────────────────────────▶│ code + verifier 换 token ────────▶│
 │                           │◀── refresh_token, access_token, id_token
 │                           │ id_token 取邮箱，XOAUTH2 登 IMAP + Gmail API 验证，加密保存
 │◀── {account} ─────────────│                                   │
```

- 授权范围：`https://mail.google.com/`（IMAP / SMTP 唯一能用的 scope）+ `openid email`（拿邮箱地址）。
  `access_type=offline` + `prompt=select_account consent`：每次都给 refresh token，也能换账号。
- state 不存内存：PKCE verifier 和创建时间用 `secret.key` 加密（AES-GCM）后整个放进 state，hub 在用户停留在 Google 页面期间重启 / 重新部署也不影响
  （2026-10-25 实测踩过：别的 agent 连着部署了三次，内存里的 state 全丢，回调报「start again」）。同一个 state 在进程内只能用一次。
- 客户端类型是 **iOS**：没有 client secret，回调用反向 client ID 的自定义 scheme（Google 对 Android / Chrome 应用停了自定义 scheme，iOS 仍然支持）。
  换 token 时只带 `client_id` + `code_verifier`，所以换 token 可以放在 hub 上做，refresh token 不经过手机。
  ASWebAuthenticationSession 拦截回调 scheme，不需要在 Info.plist 里注册 URL scheme，client ID 换了 App 也不用重新发版。
- id_token 是 hub 直接从 Google token 接口（TLS）拿到的，按 OIDC 规范不用再验签，只读 `email` / `email_verified`。
- 凭据：`{client_id, refresh_token}` 用 AES-GCM 加密后存 `mail_accounts.oauth_enc`；access token 只在内存里缓存，过期前 1 分钟刷新。
- 收信：IMAP `AUTHENTICATE XOAUTH2`，Gmail 搜索的临时连接同样走 XOAUTH2。
- **Gmail 流量走代理**：example-machine 只连得上 Google 的少数 IPv4 地址（`imap.gmail.com` / `smtp.gmail.com` 多数时候解析到 173.194.x.x，
  443 / 993 / 465 / 587 全部超时；HTTPS 能通只是因为走了 IPv6）。`hub.json` 的 `google.proxy`（`http://[user:pass@]host:port`）
  让 Gmail 账号的 IMAP / SMTP（HTTP CONNECT 隧道）、Google 换 token、Gmail API 全部经代理出去；其他服务商照旧直连。
- **发信走 Gmail API**（`POST gmail.googleapis.com/gmail/v1/users/me/messages/send`，raw MIME），不走 SMTP：
  example-machine 连不上 `smtp.gmail.com`（465 / 587 都超时，2026-10-25 实测），HTTPS 到 Google 正常；`https://mail.google.com/` 授权同样覆盖 Gmail API
  （项目里已启用 Gmail API）。密送靠在 raw 前面加 `Bcc:` 头，Gmail 投递后会去掉；发出的信 Gmail 自己归进「已发送」。
  添加账号时的检查也从 SMTP 换成 Gmail API 的 `users/me/profile`（并核对邮箱地址一致）。
- refresh token 失效（用户撤销授权、改了 Google 密码、半年没用）时报「Google authorization expired or was revoked」：
  在 App 里再点一次「使用 Google 账号登录」选同一个账号，会原地更新凭据（账号 ID 不变）。
- 以前用应用专用密码加的 Gmail 账号继续能用（服务端仍支持 `auth:"password"`），只是 App 里不再提供这个入口。

### 一次性配置：Google Cloud OAuth 客户端

开源邮件客户端（Thunderbird、K-9、FairEmail、Mailspring、GNOME Online Accounts）都是项目方注册好客户端、写死在源码里，
并通过了 Google 对受限 scope 的验证；Thunderbird 源码里明确写着「别复制这些值，自己注册一个」。Jarvis 只给自己用，建一个自己的就行。

建好的客户端是同意屏幕「Jarvis」外部 / 正式版（未验证，登录时会看到「此应用未经 Google 验证」，点「高级 → 转至 Jarvis」），
client ID 只写在 hub 的 `hub.json`（`google.client_id`），不进仓库和文档。
发布成正式版要求品牌页有首页、隐私政策、服务条款链接，且域名在「已获授权的网域」里，所以建了 `jarvis.chirpet.com`（仓库 `site/`）。

步骤（重建时参考）：

1. <https://console.cloud.google.com/projectcreate> 建项目（如 `jarvis-mail`）。
2. 「Google Auth Platform」→ 品牌：应用名 `Jarvis`，用户类型选「外部」。
3. 「数据访问」里添加 scope `https://mail.google.com/`（受限 scope）。
4. 「品牌塑造」填首页 / 隐私政策 / 服务条款（`https://jarvis.chirpet.com/`、`/privacy/`、`/terms/`），已获授权的网域加 `chirpet.com`。
   不填这些，「目标对象」里的「发布应用」按钮是灰的，页面也不提示缺什么。
5. 「目标对象」里**发布应用（In production）**。停在 Testing 时 refresh token 7 天就过期；发布后不过期。
   未经验证的应用最多 100 个用户，登录时 Google 会提示「此应用未经验证」，点「高级 → 继续」即可，自己用不需要走验证 / CASA 安全评估。
6. 「客户端」→ 创建 OAuth 客户端，类型 **iOS**，Bundle ID 和团队 ID 填配置里的 `app.bundle_id` / `app.team_id`，得到
   `<数字>-<串>.apps.googleusercontent.com`。
7. 写进 hub 机器的 `~/.jarvis/hub.json`：`"google": {"client_id": "…apps.googleusercontent.com"}`，重启 hub（`scripts/deploy.sh <hub 机器 id>`）。
   iOS 客户端的 client ID 是公开信息，不算密钥。

没配 `google.client_id` 时，点按钮会提示「Google sign-in is not configured」。

## 飞书邮箱

App 里选「飞书」，只填邮箱和密码（`provider: "feishu"`）。邮箱在公司自己的域名上，按域名猜不出服务器，所以服务器固定：

| | 服务器 | 端口 |
| --- | --- | --- |
| 收信 IMAP | `imap.larkoffice.com` | 993，SSL |
| 发信 SMTP | `smtp.larkoffice.com` | 465，SSL |

选了飞书时，请求里带的 host / port 会被忽略。密码是飞书邮箱设置里为第三方客户端生成的登录密码（服务器叫它「授权码」，填错会报 `wrong authorization code`）。

## 其他服务商

按域名自动填服务器，显式传了 `imap_host` / `imap_port` / `smtp_host` / `smtp_port` 就以传入值为准：

| 域名 | IMAP | SMTP |
| --- | --- | --- |
| gmail.com / googlemail.com | imap.gmail.com:993 TLS | smtp.gmail.com:465 TLS |
| qq.com / foxmail.com | imap.qq.com:993 TLS | smtp.qq.com:465 TLS |
| 163.com | imap.163.com:993 TLS | smtp.163.com:465 TLS |
| 126.com | imap.126.com:993 TLS | smtp.126.com:465 TLS |
| outlook.com / hotmail.com / live.com | outlook.office365.com:993 TLS | smtp-mail.outlook.com:587 STARTTLS |
| icloud.com / me.com | imap.mail.me.com:993 TLS | smtp.mail.me.com:587 STARTTLS |
| yahoo.com / yahoo.co.jp | imap.mail.yahoo.com:993 TLS | smtp.mail.yahoo.com:465 TLS |

QQ / 163 / 126 之类的密码是**授权码**而不是登录密码，需要先在网页版邮箱设置里开启 IMAP/SMTP 并生成授权码。
163、126 会在登录前要求客户端先发 IMAP `ID`（RFC 2971），服务端已经会发。

安全模式：IMAP 不给 `imap_security` 时按端口推断（993 = TLS，143 = STARTTLS，其余明文，明文只给本地测试用）；
SMTP 不给时按端口推断（465 = TLS，587/25 = STARTTLS，其余 TLS）。企业邮箱端口特殊时显式传。

## 凭据存储

| 文件 | 权限 | 内容 |
| --- | --- | --- |
| `<dataDir>/secret.key` | 0600 | 32 字节随机主密钥，首次启动时创建，绝不进数据库、不返回给客户端 |
| Postgres `mail_accounts` 表 | — | 主机、端口、显示名等非敏感字段（jsonb）；`password_enc`（密码）、`oauth_enc`（Gmail 的 client_id + refresh token）为密文 |

- 算法 AES-256-GCM，随机 12 字节 nonce 拼在密文前面，整体 base64。
- 每次保存都用新 nonce 重新加密（同一份明文不会产生同一段密文）。
- 整组账号在一个事务里替换写入。
- 换机器 / 重装 hub 时，只要 `secret.key` 丢了，所有密码都要重填。
- hub 的 `~/.jarvis` 目录建议 `chmod 700`。

API 响应里只有 `status` / `error`，永远拿不到密码或 refresh token。

## 连接与超时

- 每个账号缓存一条已登录的 IMAP 连接，2 分钟没人用就关掉（30 秒扫一次）。
- 同一条连接上的命令按账号串行（IMAP 的 `SELECT` 状态是连接级的，并发会互相踩）。
- 网络操作 20 秒超时；传输层报错（不是服务的 NO/BAD）时丢弃连接并重试一次，用来扛服务器主动断连。
- Gmail 搜索偶尔需要单独开一条临时连接发 `X-GM-RAW`（go-imap/v2 的强类型客户端表达不了这个扩展命令），
  用完即关；失败时自动退回标准 `SEARCH TEXT`。

## 已知限制

- **没有 IDLE 推送**：新邮件要等 App 主动刷新（后续可加轮询或 IMAP IDLE）。
- **不收发附件**：附件只能看到文件名 / 类型 / 大小，不能下载，也不能在发信时带附件。
- **不能删除 / 移动 / 归档**：目前只能改已读和星标。
- **摘要只取前 2 KB 正文**：如果正文开头是超长 base64 内嵌图片，摘要可能是空的或没意义。
- **gzip 压缩的正文**不支持（`Content-Encoding: gzip`），这类信件的正文会缺失。
- **非 Gmail 的发件箱留存是尽力而为**：`APPEND` 失败只打日志；有的服务商不允许 APPEND 到 `Sent`。
- **MCP 发信必须等手机批准**：没批准不会发出；但批准后真的发出去，没有撤销。
- **每账号一条连接**：多账号合并收件箱是并发拉的，但同一账号只能串行；账号多、搜索复杂时首次会比较慢。
- **IMAP `ID` 只在服务端声明了 `ID` 能力时发送**（163/126 需要），不要指望它做客户端标识用途。
- 邮件内容会原样进入 Claude 的上下文，等于把信件内容交给模型；敏感信箱建议单独建一个账号只给它必要的信。
