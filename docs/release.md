# TestFlight 发布

TestFlight 内部测试（测试员在配置的 `app_store.testers`），以及用公开链接加入的外部测试（见文末「外部测试」）。

## 账号与标识

都在构建配置里（`docs/config.md`）：`app.bundle_id` / `app.team_id`、`app_store.key_id` / `issuer_id` / `key_path`（API key 文件只放在
仓库外，比如 `~/.appstoreconnect/`）、`app_store.app_id`（App 记录的数字 ID）、`app_store.testers`、`app_store.feedback_email`。

| 项目 | 说明 |
| --- | --- |
| Bundle ID | iOS `<bundle_id>`、手表 `<bundle_id>.watchkitapp`，用 `scripts/asc_signing.py` 注册 |
| App ID 能力 | iOS：`PUSH_NOTIFICATIONS`、`HEALTHKIT`；手表：无 |
| 描述文件 | 「Jarvis App Store」「Jarvis Watch App Store」（`IOS_APP_STORE`）；`asc_signing.py` 会删了重建并装到 `~/Library/MobileDevice/Provisioning Profiles/` |
| 签名身份 | 登录钥匙串里的 `Apple Distribution: …（team_id）` |
| App Store Connect App ID | 网页建好 App 记录后填进配置的 `app_store.app_id` |

API key 的角色是 App Manager，用不了 cloud-managed signing（`-allowProvisioningUpdates` 云签名会失败），所以
归档走自动签名，导出时 `release.sh` 按配置生成 ExportOptions（`scripts/jarvis_config.py export-options`），手动指定两个 App Store 描述文件重签。

## 首次准备（顺序）

1. 填好 `~/.config/jarvis/config.json`（`app`、`app_store` 的 key 部分）。
2. `python3 scripts/asc_signing.py`：注册两个 Bundle ID、给 iOS 打开推送和 HealthKit、建两个 App Store 描述文件并装到本机。幂等，能力变了随时重跑。
3. **网页创建 App 记录**（API 不能建）：App Store Connect › App › 新建 App：iOS、名称、主要语言、Bundle ID、SKU、完全访问权限。
   把 App ID 填进配置的 `app_store.app_id`。
4. `python3 scripts/asc_testflight.py setup`：建内部群组 `Internal`（`hasAccessToAllBuilds`）并把配置里的测试员加进去。
5. `NO_UPLOAD=1 ./scripts/release.sh` 验证归档 + 导出（不需要 App 记录就能跑）。
6. `./scripts/release.sh` 上传第一个构建 → `python3 scripts/asc_testflight.py wait` 等处理完并自动加入 Internal。

没填 `app_store.app_id` 时 `scripts/asc_testflight.py` 会直接报错提示。

## 每次发版

```sh
./scripts/release.sh                      # 构建号 = yyyymmddHHMM；归档、检查、上传、打 testflight/<构建号> 标签
python3 scripts/asc_testflight.py status  # 最近几个构建和处理状态
python3 scripts/asc_testflight.py info "这次的 What to Test"
git push --tags
```

构建号也可以手动给：`./scripts/release.sh 42`。

`release.sh` 做的事：

1. `xcodegen generate`（工程由 `ios/project.yml` 生成，`.xcodeproj` 不入库）。
2. Release 归档到 `~/Library/Developer/Xcode/Archives/<日期>/Jarvis <构建号>.xcarchive`（dSYM 留给 Organizer 符号化），
   同时嵌入手表 App。
3. `check_bundle_strings.py` 检查 `Jarvis.app` 和 `Jarvis.app/Watch/Jarvis.app`：Info.plist 里的 `*UsageDescription`
   不能为空或等于 key，`*.lproj/InfoPlist.strings` 同理（占位符文案会被 App Review 自动拒）。
4. `strings` 检查 Release 二进制里没有 `pairURL`、`openSession`、`showNewChat` 这些只在 `#if DEBUG` 里的模拟器开关。
   `check_bundle_privacy.py` 检查整个 bundle 里没有个人的值（外部测试是公开链接，见文末）。
5. `xcodebuild -exportArchive`：手动签名 + `method=app-store-connect` + 两个描述文件映射。**这一步才是 App Store 签名**：
   归档用的是自动签名的开发描述文件（所以归档里 `aps-environment` 是 `development`），导出时按 App Store 描述文件重签成
   `production`。`NO_UPLOAD=1` 时把 `destination` 从 `upload` 改成 `export`，并额外校验导出的 ipa 里 `aps-environment`
   确实是 `production`。
6. `git tag -f testflight/<构建号>`。

归档之后大约 10–30 分钟构建才会出现在 TestFlight。`asc_testflight.py wait [分钟数] [构建号]` 会每 30 秒查一次，等到
`processingState=VALID` 后写默认的 What to Test 并加入 `Internal` 群组；`INVALID` / `FAILED` 会直接报错退出
（原因在 App Store Connect 的构建页面里）。**要带上 release.sh 打印的构建号**：刚上传完新构建还查不到，不带构建号时
「最新的构建」是上一个（早已 VALID），会把 What to Test 写到旧构建上（2026-10-25 踩过）。

## 为什么这样签名

| 环节 | 签名方式 | 描述文件 |
| --- | --- | --- |
| 归档 `xcodebuild archive` | 自动（`CODE_SIGN_STYLE=Automatic`） | 开发期的 `iOS Team Provisioning Profile` |
| 导出 `xcodebuild -exportArchive` | 手动（`signingStyle=manual`、`signingCertificate=Apple Distribution`） | 「Jarvis App Store」/「Jarvis Watch App Store」 |

App Store Connect 的 API key 只能做 App Manager 能做的事，cloud-managed signing 需要 Admin；手动签名绕开这个限制。
改 Bundle ID 能力（比如再加一个 capability）会让旧描述文件失效，重跑 `scripts/asc_signing.py` 即可。

## 踩坑

- **`bundleIdCapabilities` 不能带自造的 settings**：给 `PUSH_NOTIFICATIONS` 传
  `settings: [{"key": "PUSH_NOTIFICATIONS", ...}]` 会被 409 拒（`settings/0/key` 只接受 `ICLOUD_VERSION`、
  `DATA_PROTECTION_PERMISSION_LEVEL`、`APPLE_ID_AUTH_APP_CONSENT`）。推送和 HealthKit 直接传空 `settings: []`。
- **同名描述文件会挡住重建**：改能力后旧描述文件变成 `INVALID`，同名新文件创建会 409。`asc_signing.py` 先删同名再建。
- **内部测试员要用 `POST /v1/betaTesters` 带群组关系创建**：同一个邮箱在别的 App 下已有测试员记录时，直接关联会 409；
  脚本遇到 409 会提示去网页把这个人加进本 App 的 `Internal` 群组。
- **归档里的 `aps-environment` 总是 `development`**：归档用的是开发描述文件，最终签名在导出环节完成，别在归档产物上检查。
- **推送和生产环境**：`UIBackgroundModes` 用 `remote-notification`（手表 App 没有后台模式）；`ITSAppUsesNonExemptEncryption=false`
  已经在 `ios/project.yml` 里，省掉每次上传的出口合规问答。
- **不用 iCloud 目录**：这台 Mac 没解锁，涉及 `~/Library/Mobile Documents` 的一律跳过。

## 待 leader 处理

（无。2026-10-24：clipJSON 截断中文的问题已修，transcript.go 已 gofmt。）

## 推送（APNs）

- 在 developer.apple.com 建一个 APNs key（Sandbox & Production，Team Scoped），Key ID 和 .p8 文件都不进仓库。
- 文件：hub 机器的 `~/.jarvis/apns_key.p8`（配置 `~/.jarvis/apns.json`），另存一份到密码管理器。key 只能下载一次，丢了就重建。
- 页面提示《Apple Developer Program 许可协议》已更新，要账户持有人在 developer.apple.com 接受（不影响建 App 和传 TestFlight）。

## 外部测试（公开链接）

内部测试员必须是团队成员；给朋友用走外部测试：群组「Friends」（`app_store.external.group`）。2026-10-30 用户定的是**公开链接**
（`public_link: true`，`public_link_limit: 50`）：把 `https://testflight.apple.com/join/…` 发到群里，谁点开都能装，满 50 人后链接不再收人。
当前的公开链接：<https://testflight.apple.com/join/vsjfy3hE>（`asc_testflight.py external` 每次都会打印，变了就更新这里）。
**只私下发给要测的人，不放到官网（`site/`）或任何公开页面上**（2026-10-31 用户要求）。
不用收集邮箱；`app_store.external.testers` 仍然可以写，列出的邮箱另外收到邀请邮件。`public_link: false` 时只有这些邮箱能加入。

```sh
python3 scripts/asc_testflight.py external   # 建群组、按配置开 / 关公开链接、写测试信息、挂最新构建、提交审核；打印公开链接
```

- **公开链接 = 任何拿到链接的人都能装**：构建里不能有任何个人的东西（hub 地址、主机名、邮箱、路径……）。`release.sh` 在上传前跑
  `scripts/check_bundle_privacy.py <Jarvis.app>`（用 `privacy_check.py` 的同一份敏感词，查二进制的字符串、plist、lzfse 元数据；
  bundle id、team id、签名数据本来就在每个构建里，不算）。自己写的发版脚本也要在上传前跑它。
- **每次发版不用另跑命令**：配置里有 `app_store.external` 时，`wait`、`info` 在挂好 Internal 之后会顺带把这个构建挂到 Friends、
  提交审核（有列出的测试员就补邀请）。这一步出错只打印一行（附重试命令 `external`），不会让发版失败。
- 外部测试的构建要过 **Beta App Review**（第一次约一天，同一版本后面的构建多数自动通过）。提交前必须有联系人：
  `app_store.external.review` 的 `first_name` / `last_name` / `phone` / `email`，缺了脚本只挂构建、不提交，也不加测试员。
- 公开链接在没有通过审核的构建之前就能打开（API 直接给出地址），但页面显示「This beta isn't accepting any new testers right now」，
  审核通过后才能加入。
- 审核状态看构建的 `buildBetaDetail.externalBuildState`：`READY_FOR_BETA_SUBMISSION`（挂了没交）→ `WAITING_FOR_BETA_REVIEW` →
  `IN_BETA_REVIEW` → `BETA_APPROVED` / `BETA_REJECTED`。
- 朋友的 hub 是他们自己部署的：没有开发者账号的 APNs key，就收不到推送和实时活动（App 打开时照常实时）；Gmail 登录要他们
  自己建 Google OAuth 客户端、写进自己 hub 的 `hub.json`（`docs/mail.md`）。
- 测试信息要有隐私政策地址（`app_store.external.privacy_url`），中英文说明在 `asc_testflight.py` 的 `BETA_INFO`。
- App 没有 hub 就只有配对页：审核备注（`review.notes`）要讲清楚这是配合自建 hub 的 App，否则容易被问「怎么用」。
