# 推送中继（端到端加密）

自己搭的 hub 没有 App 所属 Apple Team 的 APNs key，发不了推送。推送中继是 App 发布方跑的一个 `jarvisd relay`：
它持有 APNs key，替各家 hub 把推送交给 APNs。通知正文由 hub 用手机的公钥加密，中继只转发密文，
手机在「通知服务扩展」里解密后再显示。自己有 APNs key 的 hub（`~/.jarvis/apns.json`）照旧直连 APNs，不经过中继。

```
hub ──(HPKE 密文 + 授权凭据)──▶ 中继 ──(APNs key)──▶ APNs ──▶ 手机：通知服务扩展解密 → 显示
 ▲                                                               │
 └──────────── 手机公钥、中继凭据（经已配对的 hub API） ◀─────────┘
```

## 谁知道什么

| | 手机 | hub | 中继 |
| --- | --- | --- | --- |
| 私钥 | 有（钥匙串，只在本机） | 无 | 无 |
| 公钥 | 有 | 有（配对后经 hub API 拿到） | **无**：公钥从不经过中继 |
| APNs 设备 token | 有 | 直连时有；走中继时不需要 | 有（封在凭据里） |
| 通知正文 | 解密后有 | 有 | **无**（只有密文） |
| Live Activity 内容 | 有 | 有 | **有**（明文，见下） |
| 发给哪台设备、什么时间、密文大小 | | | 有 |

- 公钥只走「手机 → 自己的 hub」这条已鉴权的链路（设备 token + 配对时的地址），中继不在路上，替换不了公钥，也就冒充不了手机去解密。
- 中继能做的最坏的事：不转发、重放旧密文、或者用 APNs key 直接发一条明文通知（它本来就有 key，任何中继方案都防不住）。
  它造不出一条能解开的密文：只有 hub 有手机的公钥。重放的密文超过 24 小时手机不再显示正文（`iat`）。

## 加密

- HPKE（RFC 9180）base 模式：DHKEM(P-256, HKDF-SHA256) + HKDF-SHA256 + AES-256-GCM，`info = "jarvis push v1"`。
  Go 用标准库 `crypto/hpke`，iOS 用 CryptoKit `HPKE`（`P256_SHA256_AES_GCM_256`）。密文 = `enc`（65 字节）‖ AEAD 输出，base64 放进推送的 `e` 字段。
- 明文就是直连时 hub 会发的那份 JSON（`aps.alert` 的标题 / 副标题 / 正文、`category`、`thread-id`、`interruption-level`、
  `interaction_id` / `machine_id` / `session_id`、`jarvis_wake`），加一个 `iat`（Unix 秒）。
- 手机的私钥：`P256.KeyAgreement.PrivateKey`，存在 App 和通知服务扩展共用的钥匙串访问组里，`AfterFirstUnlockThisDeviceOnly`
  （开机后解锁过一次，锁屏时扩展也能读；不进 iCloud 备份，换手机重新生成）。第一次需要时生成，之后不变。

## 中继收到什么、发出什么

中继自己拼 APNs 的 `aps`，hub 只能给密文和少量不含内容的参数，所以拿到凭据也塞不进明文通知：

| hub 发的 `type` | 中继发给 APNs |
| --- | --- |
| `alert` | `{"aps":{"alert":{"title":"Jarvis","body":"有新消息"},"mutable-content":1,"sound":"default"},"e":"<密文>"}`，push type `alert` |
| `background` | `{"aps":{"content-available":1},"e":"<密文>"}`，push type `background`，优先级 5 |
| `liveactivity` | hub 给的 `aps`（只允许 Live Activity 用到的键），push type `liveactivity`，topic 加 `.push-type.liveactivity` |

- 「有新消息」是兜底：扩展解密失败、超时、开机后还没解锁过（读不到私钥）、密文过期，都显示它，**不会退回明文**。
- `collapse_id` 由 hub 先做 SHA-256 再给中继（原值是 interaction id）。

### Live Activity 是明文

Live Activity 的远程更新由 iOS 直接读推送里的 `content-state`，没有扩展可以插手解密，所以走中继时**中继能看到它的内容**：
阶段、计数、计时、TPM / 负载，以及锁屏卡片和展开态显示的条目（会话标题、「机器 · 空间 › 标签」、在问什么 / 命令摘要）。
2026-11-01 用户决定：不为此砍掉锁屏 / 灵动岛的功能，照原样转发；在意的人自己搭中继（下面「自己搭中继」）。
普通通知、ask_user、Agent 完成、静默唤醒都是密文。

## 防滥用

- **凭据**：手机直接向中继登记 `{apns_token, env, topic}`，换回一个凭据（grant）：中继用自己的密钥（`secret_path`）
  AES-GCM 封装的 `{设备 token, 环境, topic, 签发时间}`。中继不存状态，靠解开凭据知道发给谁。手机把凭据交给自己的 hub，
  hub 推送时带 `Authorization: Bearer <grant>`。没有凭据、凭据被改过、topic 不在中继允许的列表里，一律 401 / 403。
- **只发自己 App 的**：凭据里的 topic 必须在 `relay.json` 的 `topics` 里（App 的 bundle id），中继不替别的 App 推。
- **限流**：每台设备每分钟 120 条（突发 30）；登记按来源 IP 每分钟 20 次。请求体最大 8 KB，密文最多 3800 个 base64 字符
  （加上外层不超过 APNs 的 4 KB）；hub 加密前把放不下的标题 / 正文截短（完整内容在 App 里）。
- **吊销**：APNs 报 410 / `BadDeviceToken` / `Unregistered` 时中继回 410，hub 删掉这台设备（和直连一样）；
  要让所有凭据失效，换掉中继的 `secret_path`：hub 推送时收到 401，会用它记着的设备 token 重新登记一次换新凭据再推，不用等手机。
- 登记本身不验证设备真伪，这是已知的缺口，要堵上得用 App Attest（还没做）：
  - 知道某台 iPhone 的 APNs 设备 token 的人可以登记到凭据，给它推「有新消息」；
  - 任何人都能登记到一张凭据，而 Live Activity 的目标 token 不绑在凭据上（它会轮换，中继不认识），所以知道某台手机
    Live Activity token（或 push-to-start token）的人可以往那台手机的锁屏 / 灵动岛写任意内容；提醒只允许跟着 `start` 一起。
  - 这些 token 和设备 token 一样只在手机、它的 hub 和 APNs 之间，不公开；topic 被凭据钉死，推不了别的 App。

## 接口

中继（`jarvisd relay`，配置 `~/.jarvis/relay.json`）：

```
POST /relay/v1/register   {"apns_token":"<hex>","env":"production|sandbox","topic":"<bundle id>"}
                          → {"grant":"<base64url>"}
POST /relay/v1/push       Authorization: Bearer <grant>
                          {"type":"alert|background|liveactivity","priority":"10|5","collapse_id":"<hex>",
                           "sealed":"<base64>",                      // alert / background
                           "token":"<hex>","aps":{…}}                // liveactivity：活动或 push-to-start 的 token
                          → 200 {"ok":true} | 410 {"reason":"Unregistered"} | 4xx/5xx {"error":"…"}
GET  /relay/v1/health     → {"ok":true,"apns":true}
```

中继地址只认 `https`（hub 也只认 `https`，或 `http` 到本机回环地址给端到端脚本用）：凭据和 Live Activity 内容不能明文过网。

hub：`POST /api/v1/devices` 多三个字段 `push_key`（手机公钥，X9.63 未压缩点的 base64）、`relay_url`、`relay_grant`。

hub 选路（`push.go`）：有 `apns.json` → 直连（和以前一模一样）；没有 → 设备带了 `relay_url` + `relay_grant` + `push_key` 就走中继；都没有就不推。

## App

- 构建配置 `app.push_relay`（`config.json`，比如 `https://<中继域名>/relay`）进 Info.plist 的 `JarvisPushRelay`；为空就不登记中继。
- 拿到 APNs 设备 token 后：确保有推送密钥 → 向中继登记换凭据（按设备 token 缓存在钥匙串，token 变了重新登记）→
  把 `apns_token`、`push_key`、`relay_url`、`relay_grant` 一起报给 hub。
- 通知服务扩展 `JarvisNotify`：有 `e` 就解密并换上标题 / 副标题 / 正文 / category（通知上的按钮）/ thread / 打断级别 / userInfo；
  没有 `e`（直连的推送本来就不带 `mutable-content`，扩展不会被调用）原样显示。
- 静默推送（`background`）在 App 进程里解密，`jarvis_wake` 照旧唤醒连接。

## 部署（发布方）

1. 在一台能出外网的机器上放 `~/.jarvis/relay.json`（0600）：

   ```json
   {
     "listen": "127.0.0.1:18670",
     "apns": { "key_id": "<Key ID>", "team_id": "<Team ID>", "key_path": "~/.jarvis/AuthKey_<Key ID>.p8" },
     "topics": ["<App 的 bundle id>"],
     "secret_path": "~/.jarvis/relay.secret",
     "proxy_secret": "<随机长串，和 Worker 的 RELAY_PROXY_SECRET 一样>"
   }
   ```

   `secret_path` 不存在时第一次启动生成（32 字节随机数，0600）；**丢了它所有手机要重新登记**（App 启动时自动做）。
   `proxy_secret`：设了以后，不带 `X-Relay-Proxy-Secret` 的请求一律 403（绕不过 Worker 直连源站），登记的限流按 Worker 给的
   `X-Forwarded-For`（客户端地址）算；不设就按直连地址算，适合不经 Worker、自己配 TLS 直接对外的部署。
2. 跑 `jarvisd relay`（systemd / launchd 常驻），本机请求 `http://127.0.0.1:18670/relay/v1/health` 应看到 `"apns":true`。
   配置了 `proxy_secret` 时，健康检查同样受保护：本机请求要带 `X-Relay-Proxy-Secret`，否则回 403；官网请求由 Worker 自动补这个头。
3. 挂到官网域名下：官网是 Cloudflare Workers 的静态站（`site/`），`site/worker.mjs` 把 `/relay/*` 转给中继
   （`wrangler.jsonc` 的 `run_worker_first`），其余照旧是静态文件。中继的地址和密钥不进仓库：
   用 Cloudflare Tunnel（`cloudflared`）把本机 18670 暴露成一个只给 Worker 用的主机名，再在 `site/` 里
   `npx wrangler secret put RELAY_ORIGIN`（值如 `https://<tunnel 主机名>`）和 `npx wrangler secret put RELAY_PROXY_SECRET`
   （同 `proxy_secret`），然后照常部署 `site/`（`scripts/publish.sh` 或 `npx wrangler deploy`，wrangler 4 要 Node 22+）。
   两个都没设时 `/relay/*` 回 503，站点其余部分不受影响。部署后 `curl https://<官网域名>/relay/v1/health` 应回 `{"ok":true,"apns":true}`。
4. 发 App：`python3 scripts/asc_signing.py --only <bundle id>.notify` 注册通知服务扩展的 bundle id、建「Jarvis Notify App Store」
   profile（`release.sh` 导出时要用）；`config.json` 填 `app.push_relay`（如 `https://<官网域名>/relay`）；照常发 TestFlight。
   旧版本的 App 没有扩展、不登记中继，自建 hub 照旧收不到推送，要装这之后的版本。

## 自己搭中继

只有持有 App 所属 Team 的 APNs key 才能给这个 App 推送，所以「自己搭中继」= 自己签名装 App（自己的 Team、自己的 bundle id）+
自己的 APNs key。那样直接把 key 放进自己 hub 的 `apns.json` 走直连更简单，Live Activity 也不经过任何第三方。

## 验证

- `server/internal/relay`：凭据封装 / 篡改 / topic、限流、拼出的 APNs 请求（假 APNs）、410 透传。
- `server/internal/hub`：没有 `apns.json` 时普通通知 / 交互 / 完成 / 静默唤醒 / Live Activity 都打到假中继、请求里没有明文；有 `apns.json` 时照旧直连。
- `server/internal/pushcrypto` 生成的密文由 JarvisTests 用 CryptoKit 解开（跨语言测试向量）。
- `site/worker.test.mjs`（`node --test`）：静态路径不变、没配置时 503、转发时带上密钥和客户端地址、丢掉伪造的密钥和 cookie。
- 模拟器端到端：`scripts/relay_sim_e2e.sh` 用 App 导出的公钥，由 hub 的 Go 代码加密 → 本机 `jarvisd relay` → 假 APNs
  收下请求（检查里面没有明文）→ App 进程里跑扩展的逻辑（`PushCrypto.service`，`NotificationServiceTests`）逐条解开；
  改坏一个字节的仍是「有新消息」。
  **`simctl push` 不运行通知服务扩展**（模拟器把它当本地通知直接加进通知中心，2026-11-01 Xcode 27.1 / iOS 27.0 实测：
  扩展进程没有起，带 `mutable-content` 的推送原样显示成「有新消息」），所以扩展本身（`JarvisNotify`）只能在真机上验证。
- 真 APNs、真机上的扩展：要发布方的 key 和带扩展的签名包，在发布方部署后验证（`docs/release.md`）。
