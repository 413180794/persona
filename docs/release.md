# TestFlight 发布

只发 TestFlight 内部测试（测试员 tester@example.com），不上架、不做官网。

## 账号与标识

| 项目 | 值 |
| --- | --- |
| 团队 | chirpet.com，Team ID `ABCDE12345` |
| Bundle ID | iOS `com.chirpet.jarvis`（API 记录 `BUNDLEID01`）、手表 `com.chirpet.jarvis.watchkitapp`（`BUNDLEID02`），2026-10-24 用 `scripts/asc_signing.py` 注册 |
| App ID 能力 | iOS：`PUSH_NOTIFICATIONS`、`HEALTHKIT`；手表：无 |
| 描述文件 | 「Jarvis App Store」「Jarvis Watch App Store」（`IOS_APP_STORE`），发布证书 `CERTID0001`，2027-10-06 到期；`asc_signing.py` 会删了重建并装到 `~/Library/MobileDevice/Provisioning Profiles/` |
| App Store Connect API key | `~/.appstoreconnect/AuthKey_KEYID00001.p8`，Key ID `KEYID00001`，Issuer `00000000-0000-0000-0000-000000000000`（只在 `~/.appstoreconnect/` 里，不进仓库） |
| 签名身份 | 登录钥匙串里的 `Apple Distribution: Your Name (ABCDE12345)` |
| App Store Connect App ID | **还没建**：等网页建好 App 记录后写进 `scripts/app_id.txt`（一行数字），`asc_testflight.py` 从这里读 |
| SKU（建 App 时填） | `jarvis-001` |
| 名称 / 主要语言 | `Jarvis`，简体中文；只发内部 TestFlight |
| 反馈邮箱 | `person@example.com` |

API key 的角色是 App Manager，用不了 cloud-managed signing（`-allowProvisioningUpdates` 云签名会失败），所以
归档走自动签名，导出时用 `ExportOptions.plist` 手动指定两个 App Store 描述文件重签。

## 首次准备（顺序）

1. ✅ 2026-10-24 `python3 scripts/asc_signing.py`：注册两个 Bundle ID、给 iOS 打开推送和 HealthKit、建两个 App Store
   描述文件并装到本机。幂等，能力变了随时重跑。
2. ⬜ **网页创建 App 记录**（API 不能建）：App Store Connect › App › 新建 App：iOS、名称 `Jarvis`、主要语言简体中文、
   Bundle ID `com.chirpet.jarvis`、SKU `jarvis-001`、完全访问权限。把 App ID 写进 `scripts/app_id.txt`。
3. ⬜ `python3 scripts/asc_testflight.py setup`：建内部群组 `Internal`（`hasAccessToAllBuilds`）并把 tester@example.com 加进去。
4. ⬜ `NO_UPLOAD=1 ./scripts/release.sh` 验证归档 + 导出（不需要 App 记录就能跑）。
5. ⬜ `./scripts/release.sh` 上传第一个构建 → `python3 scripts/asc_testflight.py wait` 等处理完并自动加入 Internal。

第 2 步之前，`scripts/asc_testflight.py` 会直接报错告诉你缺 `scripts/app_id.txt`。

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
5. `xcodebuild -exportArchive`：手动签名 + `method=app-store-connect` + 两个描述文件映射。**这一步才是 App Store 签名**：
   归档用的是自动签名的开发描述文件（所以归档里 `aps-environment` 是 `development`），导出时按 App Store 描述文件重签成
   `production`。`NO_UPLOAD=1` 时把 `destination` 从 `upload` 改成 `export`，并额外校验导出的 ipa 里 `aps-environment`
   确实是 `production`。
6. `git tag -f testflight/<构建号>`。

归档之后大约 10–30 分钟构建才会出现在 TestFlight。`asc_testflight.py wait [分钟数]` 会每 30 秒查一次，等到
`processingState=VALID` 后写默认的 What to Test 并加入 `Internal` 群组；`INVALID` / `FAILED` 会直接报错退出
（原因在 App Store Connect 的构建页面里）。

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

- 暂无。
