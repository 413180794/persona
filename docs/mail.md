# 邮件

`server/internal/mail`：让 Claude 和 App 都能收发用户的邮箱。走标准 IMAP + SMTP，不依赖任何第三方中转服务；
Gmail 用「使用 Google 账号登录」（OAuth2 + XOAUTH2），额外支持用 Gmail 搜索语法（`X-GM-RAW`）检索。凭据加密后存在 hub 的 `dataDir`（默认 `~/.persona`）里。

## 能力

| 能力 | 说明 |
| --- | --- |
| 多账号 | 每个账号独立的 IMAP / SMTP 主机（域名预设自动填），可同时挂 Gmail、QQ、163、Outlook、iCloud…… |
| 测试连接 | 新增账号时先真正登录一次 IMAP 和 SMTP，成功才落盘；失败返回错误，不入库 |
| 邮箱列表 | `LIST`，服务端支持时带 `RETURN (SPECIAL-USE STATUS)`（要有 LIST-EXTENDED 或 IMAP4rev2），映射出 `inbox/sent/drafts/trash/archive/junk` 角色，带未读数与总数；不支持的服务端发普通 `LIST`，角色按名字猜、计数逐个 `STATUS` |
| 收件箱浏览 | 按 UID 倒序分页（`before_uid`），一次取 `limit` 条；只看一个账号时结果带 `next_before_uid`（下一页的 `before_uid`）。摘要从新到旧每批取 50 封，这次操作离 20 秒超时不到 6 秒时不再取下一批，这一页提前结束（`has_more` + `next_before_uid`），163 取旧邮件很慢时靠它不超时 |
| 搜索 | Gmail 用 `X-GM-RAW`（支持 `from:`、`subject:`、`has:attachment`、`newer_than:7d` 等）；其他服务商用 `SEARCH TEXT`。服务端回 0 条或拒绝（NO / BAD）时改在本地匹配邮件头（`headers_only`）：从 `before_uid` 往前每批取 100 封的 ENVELOPE，空格分开的每个词都要出现在主题、发件人或收件人 / 抄送的名字或地址里（不区分大小写，不看正文）；一页最多看 2000 封、6 秒（从这次 IMAP 操作开始算起，建连接、SELECT 和服务端那次空搜都算在内，连接出错重试也不重新计时；到点后看完手上这一批就停，每页至少看一批），没看完就 `has_more`，`next_before_uid` 是接着翻的位置（这时 `messages` 可能为空）。163 的服务端不做关键词搜索，全靠这一步 |
| 摘要 | 只取 2048 字节正文片段，不下载整封信；摘要取正文前 160 个字符 |
| 合并收件箱 | 不传 `account_id` 时并发拉取所有账号的 INBOX，按日期倒序合并 |
| 读信 | `BODY.PEEK` 取全文，go-message 解析纯文本 / HTML / 附件元数据；**不会**自动标记已读 |
| 附件下载 | 稳定 ID 绑定 MIME 顺序、元数据和解码内容；按账号、邮箱、UID 鉴权读取，普通 `BODY.PEEK` 流式解码到临时文件，支持 HTTP Range，不标已读、不要求额外 IMAP 扩展 |
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
  所以网页版加不了 Gmail（回调只有 App 接得住），设置 › 连接器里选 Gmail 时提示在手机上添加；IMAP / 飞书邮箱网页上能加。
  换 token 时只带 `client_id` + `code_verifier`，所以换 token 可以放在 hub 上做，refresh token 不经过手机。
  ASWebAuthenticationSession 拦截回调 scheme，不需要在 Info.plist 里注册 URL scheme，client ID 换了 App 也不用重新发版。
- id_token 是 hub 直接从 Google token 接口（TLS）拿到的，按 OIDC 规范不用再验签，只读 `email` / `email_verified`。
- 凭据：`{client_id, refresh_token}` 用 AES-GCM 加密后存 `mail_accounts.oauth_enc`；access token 只在内存里缓存，过期前 1 分钟刷新。
- 收信：IMAP `AUTHENTICATE XOAUTH2`，Gmail 搜索的临时连接同样走 XOAUTH2。
- **Gmail 流量走代理**：作者的 hub 机器只连得上 Google 的少数 IPv4 地址（`imap.gmail.com` / `smtp.gmail.com` 多数时候解析到 173.194.x.x，
  443 / 993 / 465 / 587 全部超时；HTTPS 能通只是因为走了 IPv6）。`hub.json` 的 `google.proxy`（`http://[user:pass@]host:port`）
  让 Gmail 账号的 IMAP / SMTP（HTTP CONNECT 隧道）、Google 换 token、Gmail API 全部经代理出去；其他服务商照旧直连。
- **发信走 Gmail API**（`POST gmail.googleapis.com/gmail/v1/users/me/messages/send`，raw MIME），不走 SMTP：
  这台机器连不上 `smtp.gmail.com`（465 / 587 都超时，2026-10-25 实测），HTTPS 到 Google 正常；`https://mail.google.com/` 授权同样覆盖 Gmail API
  （项目里已启用 Gmail API）。密送靠在 raw 前面加 `Bcc:` 头，Gmail 投递后会去掉；发出的信 Gmail 自己归进「已发送」。
  添加账号时的检查也从 SMTP 换成 Gmail API 的 `users/me/profile`（并核对邮箱地址一致）。
- refresh token 失效（用户撤销授权、改了 Google 密码、半年没用）时报「Google authorization expired or was revoked」：
  在 App 里再点一次「使用 Google 账号登录」选同一个账号，会原地更新凭据（账号 ID 不变）。
- 以前用应用专用密码加的 Gmail 账号继续能用（服务端仍支持 `auth:"password"`），只是 App 里不再提供这个入口。

### 一次性配置：Google Cloud OAuth 客户端

开源邮件客户端（Thunderbird、K-9、FairEmail、Mailspring、GNOME Online Accounts）都是项目方注册好客户端、写死在源码里，
并通过了 Google 对受限 scope 的验证；Thunderbird 源码里明确写着「别复制这些值，自己注册一个」。Persona 只给自己用，建一个自己的就行。

建好的客户端是同意屏幕「Persona」外部 / 正式版（未验证，登录时会看到「此应用未经 Google 验证」，点「高级 → 转至 Persona」），
client ID 只写在 hub 的 `hub.json`（`google.client_id`），不进仓库和文档。
发布成正式版要求品牌页有首页、隐私政策、服务条款链接，且域名在「已获授权的网域」里，所以建了一个静态站（仓库 `site/`，域名在 `site/wrangler.jsonc`）。

步骤（重建时参考）：

1. <https://console.cloud.google.com/projectcreate> 建项目（如 `jarvis-mail`）。
2. 「Google Auth Platform」→ 品牌：应用名 `Jarvis`，用户类型选「外部」。
3. 「数据访问」里添加 scope `https://mail.google.com/`（受限 scope）。
4. 「品牌塑造」填首页 / 隐私政策 / 服务条款（`https://<站点域名>/`、`/privacy/`、`/terms/`），已获授权的网域加站点的主域名。
   不填这些，「目标对象」里的「发布应用」按钮是灰的，页面也不提示缺什么。
5. 「目标对象」里**发布应用（In production）**。停在 Testing 时 refresh token 7 天就过期；发布后不过期。
   未经验证的应用最多 100 个用户，登录时 Google 会提示「此应用未经验证」，点「高级 → 继续」即可，自己用不需要走验证 / CASA 安全评估。
6. 「客户端」→ 创建 OAuth 客户端，类型 **iOS**，Bundle ID 和团队 ID 填配置里的 `app.bundle_id` / `app.team_id`，得到
   `<数字>-<串>.apps.googleusercontent.com`。
7. 写进 hub 机器的 `~/.persona/hub.json`：`"google": {"client_id": "…apps.googleusercontent.com"}`，重启 hub（`scripts/deploy/deploy.sh <hub 机器 id>`）。
   iOS 客户端的 client ID 是公开信息，不算密钥。

没配 `google.client_id` 时，点按钮会提示「Google sign-in is not configured」。

### Android

hub 的 OAuth 客户端是 iOS 类型，回调是反向 client ID 的自定义 scheme，hub 负责拼授权 URL、换 token，所以 Android 只要打开 URL、
接住回调（`android/…/mail/GoogleSignIn.kt`）：系统浏览器打开 `auth_url`，`ui/mail/OAuthRedirectActivity` 收到
`<scheme>:/oauth2redirect?code&state` 后交给 hub 的 `finish`，再把 App 拉回前台。只认这次流程的 `state`（别的 App 伪造的回调不理）；
流程跨进程：等的 `state` 存在 SharedPreferences，浏览器在前台时进程被杀，回调照样能完成。

- Android 不能在运行时注册任意 scheme：intent filter 的 scheme 来自构建配置 `~/.config/persona/config.json` 的
  `android.google_oauth_scheme`（`scripts/app/android.sh` 传给 gradle；也认环境变量 `JARVIS_GOOGLE_OAUTH_SCHEME`），不进仓库。没配时是占位值，
  App 用 `PackageManager.queryIntentActivities` 判断接不接得住 hub 返回的 `callback_scheme`，接不住就说明原因，
  退到「Gmail + 应用专用密码」（hub 仍支持 `auth: password`）。hub 没配 `google.client_id` 时同样退到这个表单。
- 没在真机上验证过：Google 对 iOS 客户端的自定义 scheme 回调从 Android 浏览器发起是否放行（Android 客户端类型已经禁用自定义 scheme）。
  不行的话要么在 hub 加一个 Android / Web 类型客户端（Web 要 client secret 和 https 回调），要么继续用应用专用密码。
- 从浏览器直接返回（没授权）时，回到页面 1.5 秒内没收到回调就当取消。

## Outlook 接入方式：使用 Microsoft 账号登录（OAuth2 + XOAUTH2，2026-11-03）

个人的 Outlook.com / Hotmail 和公司、学校的 Microsoft 365 邮箱都走这个：App 的「添加邮箱 › Outlook」点「使用 Microsoft 账号登录」→
系统浏览器打开 Microsoft 登录、授权 → 回到 App → 账号加好，不用密码。做法照 Gmail（上一节），区别：

- **client ID 不编进程序**：每个用户用自己在 Microsoft Entra 注册的应用（免费，步骤见下面）。App 第一次点「Outlook」时让你填它的
  client ID，hub 用 `secret.key` 加密后存进 Postgres（settings 表的 `mail.microsoft_client`），这个 hub 以后都复用；hub.json 的
  `microsoft.client_id` 可以给一个默认值（App 里填的优先）。client ID 出现在每个登录 URL 里，不是密钥。
- **回调地址**：固定的自定义 scheme `jarvis-mail-oauth://outlook`，所有 hub 一样（要改就写 hub.json 的 `microsoft.redirect_uri`，Entra 里同步改）。
  App 用 ASWebAuthenticationSession 接住这个 scheme，不需要在 Info.plist 里注册，所以**不用为哪个 hub 单独发版**；
  没用 `msauth.<bundle id>://auth`：它跟着 App 的 bundle ID 走，自己编译的人 bundle ID 各不相同，固定 scheme 对谁都一样。
- **公共客户端 + PKCE**，authority 用 `common`（个人账号和任何公司的账号都能登），没有 client secret；换 token 在 hub 上做，refresh token 不经过手机。
  scope：`https://outlook.office.com/IMAP.AccessAsUser.All https://outlook.office.com/SMTP.Send offline_access openid email`。
- 邮箱地址取 ID token 的 `email`，没有就取 `preferred_username`（工作账号的登录名）。
- **收发**：IMAP `outlook.office365.com:993`、SMTP `smtp.office365.com:587`（STARTTLS），都是 XOAUTH2。Exchange 自己会把发出的信放进
  「已发送」，hub 不再追加一份。
- **refresh token 会轮换**：Microsoft 每次刷新都发一个新的（旧的随之失效），hub 拿到就立刻加密存回 `oauth_enc`（Google 不轮换）。
- 凭据：`{provider: "microsoft", client_id, refresh_token}` 加密存 `mail_accounts.oauth_enc`，同 Gmail。
- 代码：`server/internal/mail/microsoft.go`；App：`apple/Persona/Views/Mail/MailOutlookViews.swift`。
- 接口：`GET` / `PUT /api/v1/mail/oauth/microsoft/config`（`{configured, client_id, source: app|config, redirect_uri}`；PUT `{client_id}`，空串清掉）、
  `POST …/microsoft/start`（`{login_hint?}` → `{auth_url, callback_scheme, state}`）、`POST …/microsoft/finish`（回调的 `{state, code}` 或
  `{state, error, error_description}` → `{account}`）。回调带着错误也交给 hub：它把 Microsoft 的错误码翻成该怎么办（见下面）。
- 真账号还没验证过（2026-11-03 只用假的 token 端点和假的 IMAP / SMTP 测了），由用户自己登录验证。

### 在 Microsoft Entra 注册应用（一次，照着点）

1. 打开 <https://entra.microsoft.com>，用你的微软账号登录（个人账号也可以，第一次进会自动建一个「默认目录」）。
   进不去或看不到菜单，就打开 <https://portal.azure.com> 搜「应用注册」，是同一个东西。
2. 左边「标识」→「应用程序」→「应用注册」→「新注册」。
3. 填三项，然后点「注册」：
   - 名称：随便，比如 `Jarvis Mail`；
   - 「受支持的帐户类型」：选「**任何组织目录(任何 Microsoft Entra ID 租户 - 多租户)中的帐户和个人 Microsoft 帐户(例如，Skype、Xbox)**」；
   - 「重定向 URI」：平台选「**公共客户端/本机(移动和桌面)**」，填 `jarvis-mail-oauth://outlook`（以 App 里显示的为准，可以点复制）。
4. 注册完在「概述」页复制「**应用程序(客户端) ID**」（形如 `xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx`）。
5. 左边「身份验证」：确认「移动和桌面应用程序」下面有 `jarvis-mail-oauth://outlook`；拉到「高级设置」，把「**允许公共客户端流**」设为「是」，点「保存」。
6. 左边「API 权限」→「添加权限」：
   - 「我的组织使用的 API」里搜 `Office 365 Exchange Online` →「委托的权限」→ 勾 `IMAP.AccessAsUser.All` 和 `SMTP.Send` →「添加权限」；
   - 再「添加权限」→「Microsoft Graph」→「委托的权限」→ 勾 `offline_access`、`openid`、`email`（默认的 `User.Read` 留着无妨）。
   个人账号的目录里有时搜不到 Exchange Online：跳过这一步也能用（登录时按 scope 动态申请，同意页上照样列出这些权限）；
   公司账号建议加上，管理员「授予管理员同意」时看的就是这里列的权限。
7. 回到 App：设置 › 连接器 › 邮箱 › 添加邮箱 › Outlook，把 client ID 填进去保存，再点「使用 Microsoft 账号登录」。

### 出错时怎么办（hub 返回的提示就是这些）

| 情况 | 提示里说的办法 |
| --- | --- |
| 公司账号要管理员同意（`AADSTS65001` / `90094` / `90095`） | 请 IT 管理员在 Entra 的「企业应用程序」里找到这个应用 →「权限」→「授予管理员同意」，或允许用户自己同意；也可以改用个人账号 |
| 用户在同意页点了取消（`AADSTS65004` / `access_denied`） | 再点一次登录并同意 |
| client ID 不对，或注册时没选「个人和组织账号」（`AADSTS700016`） | 检查 client ID 和「受支持的帐户类型」 |
| 回调地址对不上（`AADSTS50011`） | 在「身份验证 › 移动和桌面应用程序」里加上 App 显示的回调地址 |
| 没打开「允许公共客户端流」（`AADSTS7000218`） | 在「身份验证 › 高级设置」里打开 |
| 没拿到读邮件的权限（返回的 scope 里没有 IMAP） | 在「API 权限」里加上 `IMAP.AccessAsUser.All`、`SMTP.Send` 再登录 |
| IMAP 登录被拒（`AUTHENTICATE failed`） | 个人账号：outlook.com「设置 › 邮件 › 转发和 IMAP」打开「允许设备和应用使用 IMAP」；工作账号：请管理员在 Exchange 管理中心为你的邮箱启用 IMAP |
| SMTP 认证被关（`5.7.139 SmtpClientAuthentication is disabled`） | 请管理员在 Microsoft 365 管理中心为你的邮箱打开「经过身份验证的 SMTP」；收信不受影响 |
| 授权过期或被撤销（刷新时 `invalid_grant`：`AADSTS70008`、`50173`、要求重新多重验证等） | 在 App 里再点「使用 Microsoft 账号登录」选同一个邮箱，账号原地更新（ID 不变） |

## 让 Persona 添加（2026-11-01）

App 的「添加邮箱」页去掉了「AI 一键添加」（粘贴一段文字让模型认，`POST /api/v1/ai/mail-account` 也删了）。现在直接在 Persona 里说
「帮我添加邮箱」：助手用 `jarvis_settings` 的 `mail.accounts`（`hub/self_settings.go`，只给助手）加，手机卡片让用户填写密码 / 授权码，
确认后 hub 用 `AddAccount` 连一遍再保存，和 App 表单走同一条路。密码不经过模型、不进对话；交互历史只存 `<redacted>`。
需要填写的卡片没有通知快捷允许按钮，手表和 Siri 提示到 iPhone 填写；缺少必填字段的批准不会消耗卡片。Gmail 仍然只能在 App 里登录。

## 飞书邮箱

App 里选「飞书」，只填邮箱和密码（`provider: "feishu"`）。邮箱在公司自己的域名上，按域名猜不出服务器。飞书邮箱有两组服务器，端口相同（IMAP 993、SMTP 465，都是 SSL）：

| | 海外 | 国内 |
| --- | --- | --- |
| 收信 IMAP | `imap.larkoffice.com` | `imap.feishu.cn` |
| 发信 SMTP | `smtp.larkoffice.com` | `smtp.feishu.cn` |

`larkoffice.com` 解析到海外，从中国大陆连过去 TLS 握手要几秒、经常超时；`feishu.cn` 在海外反过来（#1720）。所以添加账号时两组**同时**连一遍，保存先登录成功的那组（`pickFeishuServers`），国内的 hub 自然落到 `feishu.cn`，海外的落到 `larkoffice.com`，不用用户选。两组都失败时，优先报连上了的那组的错（比如授权码不对），两组都连不上才把两边的错误一起报出来。
海外 Lark 账号能不能登录国内服务器没有验证过，所以不把默认直接换成 `feishu.cn`。已经加好的账号不会自动换组；在国内慢的话删掉重新添加一次。

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
- hub 的 `~/.persona` 目录建议 `chmod 700`。

API 响应里只有 `status` / `error`，永远拿不到密码或 refresh token。

## 连接与超时

- 每个账号缓存一条已登录的 IMAP 连接，2 分钟没人用就关掉（30 秒扫一次）。
- 同一条连接上的命令按账号串行（IMAP 的 `SELECT` 状态是连接级的，并发会互相踩）。
- 网络操作 20 秒超时；传输层报错（不是服务的 NO/BAD）时丢弃连接并重试一次，用来扛服务器主动断连。
- 一页的工作都要落在这 20 秒里（`withConnAt` 把截止时间交给 `listOne`，重试时不变）：本地匹配邮件头最多做到操作开始后 6 秒，取摘要每批 50 封、离截止不到 6 秒就不再取下一批。到点的那一页提前结束，给出 `next_before_uid`，不会报超时。
- Gmail 搜索偶尔需要单独开一条临时连接发 `X-GM-RAW`（go-imap/v2 的强类型客户端表达不了这个扩展命令），
  用完即关；失败时自动退回标准 `SEARCH TEXT`。

## 已知限制

- **没有 IDLE 推送**：新邮件要等 App 主动刷新（后续可加轮询或 IMAP IDLE）。
- **发信暂不支持附件**：来信附件可以查看元数据并下载，发信仍不能携带附件。
- **不能删除 / 移动 / 归档**：目前只能改已读和星标。
- **摘要只取前 2 KB 正文**：如果正文开头是超长 base64 内嵌图片，摘要可能是空的或没意义。
- **gzip 压缩的正文**不支持（`Content-Encoding: gzip`），这类信件的正文会缺失。
- **非 Gmail 的发件箱留存是尽力而为**：`APPEND` 失败只打日志；有的服务商不允许 APPEND 到 `Sent`。
- **MCP 发信必须等手机批准**：没批准不会发出；但批准后真的发出去，没有撤销。
- **每账号一条连接**：多账号合并收件箱是并发拉的，但同一账号只能串行；账号多、搜索复杂时首次会比较慢。
- **服务端不做关键词搜索的邮箱只能匹配邮件头**：163 对任何词都回 0 条（2026-11-03 实测 `TEXT` / `BODY` / `SUBJECT` / `FROM`，中英文都一样），只能按主题、发件人、收件人 / 抄送找，正文里的词搜不到；一页最多看 2000 封，163 取信慢，实际一页只看得完几百封，更早的按 `next_before_uid` 接着翻。多账号合并搜索没有接着翻的位置（UID 各账号各算），`mail_search` 会让 Agent 单独搜那个账号。代价：能搜的服务端真没搜到时，同样会在本地多看一遍邮件头（一页最多 2000 封的 ENVELOPE），这时「接着翻」只是接着匹配更早邮件的邮件头，服务端的全文搜索已经搜过了。
- **IMAP `ID` 只在服务端声明了 `ID` 能力时发送**（163/126 需要），不要指望它做客户端标识用途。
- **`LIST` 的 RETURN 选项只在服务端声明了 LIST-EXTENDED（或 IMAP4rev2）时带**：QQ 邮箱三个能力都不声明，收到带 `RETURN` 的 `LIST` 不应答，会一直挂到 20 秒超时（列文件夹、发信后找 `Sent` 都中招，2026-10-30 实测）。
- 邮件内容会原样进入 Claude 的上下文，等于把信件内容交给模型；敏感信箱建议单独建一个账号只给它必要的信。
