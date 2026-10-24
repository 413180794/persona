# 邮件

`server/internal/mail`：让 Claude 和 App 都能收发用户的邮箱。走标准 IMAP + SMTP，不依赖任何第三方中转服务；
Gmail 额外支持用 Gmail 搜索语法（`X-GM-RAW`）检索。凭据加密后存在 hub 的 `dataDir`（默认 `~/.jarvis`）里。

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

HTTP 路由、JSON 字段与 `docs/protocol.md` 的「邮件 API」一节严格一致；密码和 OAuth 凭据永远不出现在响应里
（`json:"-"` + 独立存储文件）。

## Gmail 接入方式

Gmail 现在**不接受**账号密码直接登录（会报 `Application-specific password required`），必须从下面两种里选一种。

### 方式一：应用专用密码（推荐，配置最简单）

1. 账号必须开启两步验证：<https://myaccount.google.com/security> →「两步验证」。没开的话下面入口不存在。
2. 打开 <https://myaccount.google.com/apppasswords>，新建一个应用专用密码（名称随便填，比如 `Jarvis`）。
3. 复制生成的 16 位密码——**只在弹窗里出现一次**，关掉就看不到了。
4. 在 Jarvis 里新增账号：邮箱 `you@gmail.com`，认证方式 `password`，密码填这 16 位（去掉空格也行）。

服务端会自动套用 Gmail 预设：`imap.gmail.com:993` TLS、`smtp.gmail.com:465` TLS。

补充：如果 `imap.gmail.com` 登录时报 `IMAP access is disabled`，去 Gmail 网页版「设置 → 转发和 POP/IMAP」启用 IMAP。

### 方式二：OAuth2（XOAUTH2，refreshtoken 长期有效，不受两步验证重置影响）

需要先自己建一个 Google Cloud OAuth 客户端。整个过程大约 10 分钟，做一次的 refresh token 可以长期用。

1. 建项目：<https://console.cloud.google.com/projectcreate>，随便是哪个项目。
2. 启用 Gmail API：<https://console.cloud.google.com/apis/library/gmail.googleapis.com> →「启用」。
   （IMAP/SMTP 的 XOAUTH2 走的是 Gmail API 的授权范围，必须启用。）
3. 配置 OAuth 同意屏幕：<https://console.cloud.google.com/apis/credentials/consent>
   - User Type 选「外部」，填应用名和邮箱；
   - Scopes 里添加 `https://mail.google.com/`（IMAP/SMTP 全套权限）；
   - **Publishing status 改成「In production」**。停在 Testing 状态时 refresh token 只有 7 天有效期，
     过期后账号直接失效；发布后不会过期（未验证应用会显示警告，点「继续」即可，自己用不需要验证）。
   - 如果留在 Testing，记得把自己的 Gmail 加进「Test users」。
4. 建凭据：<https://console.cloud.google.com/apis/credentials> →「创建凭据」→「OAuth 客户端 ID」，
   应用类型选「桌面应用」；建好后记下 `client_id` 和 `client_secret`。
5. 拿授权码（浏览器打开，把三个占位符替换成自己的）：

   ```
   https://accounts.google.com/o/oauth2/v2/auth?client_id=CLIENT_ID&redirect_uri=http://localhost&response_type=code&scope=https://mail.google.com/&access_type=offline&prompt=consent
   ```

   同意后浏览器会跳到 `http://localhost/?code=4/0A...`（页面打不开是正常的，看地址栏）。从地址栏复制 `code` 的值。
   `access_type=offline` 和 `prompt=consent` 都不能省，否则拿不到 refresh token。
6. 用授权码换 refresh token：

   ```
   curl -s https://oauth2.googleapis.com/token \
     -d client_id=CLIENT_ID \
     -d client_secret=CLIENT_SECRET \
     -d code='粘贴上面的 code' \
     -d grant_type=authorization_code \
     -d redirect_uri=http://localhost
   ```

   返回值里的 `refresh_token` 就是长期凭据（同时还会有一个一小时的 `access_token`，服务端会自己刷新，不用管）。
   授权码有效期很短（约 10 分钟）且只能用一次，报 `invalid_grant` 就重新走第 5 步。
7. 在 Jarvis 里新增账号：邮箱 `you@gmail.com`，认证方式 `oauth2`，填三样：
   `client_id`、`client_secret`、`refresh_token`。

服务端拿到 refresh token 后自己换 `access_token`（`POST https://oauth2.googleapis.com/token`，form 编码 +
Basic 认证），内存缓存到过期前 1 分钟再刷；IMAP 用 `AUTHENTICATE XOAUTH2`，SMTP 用 `AUTH XOAUTH2`。

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
| `<dataDir>/secret.key` | 0600 | 32 字节随机主密钥，首次启动时创建，绝不入库、不返回给客户端 |
| `<dataDir>/mail_accounts.json` | 0600 | 账号列表：主机、端口、显示名等明文；`password_enc`、`oauth_enc` 为密文 |

- 算法 AES-256-GCM，随机 12 字节 nonce 拼在密文前面，整体 base64。
- 每次保存都用新 nonce 重新加密（同一份明文不会产生同一段密文）。
- 写文件走「临时文件 + rename」，避免写一半断电留下坏 JSON。
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
