# TestFlight 发布

TestFlight 内部测试（测试员在配置的 `app_store.testers`），以及用公开链接加入的外部测试（见文末「外部测试」）。

## 账号与标识

产品名为 Persona（原 Jarvis），但已有 Bundle ID、App Group、钥匙串组、签名 profile 名称和 App Store Connect App 记录不变；不要因改名重新创建 App。
App Store Connect 里的商店名 2026-11-03 起是 **Persona Side**（原「Jarvis Herd」；桌面上显示的仍是 `CFBundleDisplayName` 的 Persona），隐私政策地址同 `app_store.external.privacy_url`（官网的隐私政策页）。商店名在 App 信息的本地化（`appInfoLocalizations` 的 `name`）里，版本还没提交审核时可以用 API 直接改，见下面「改商店名」。下文的「Jarvis App Store」「Jarvis Watch App Store」是已有签名 profile 名，保留不改；当前生成工程 / target 和 archive / `.app` 产物使用 Persona 名称，旧工程 / scheme 别名与 module 身份见 [兼容迁移](rename.md)。历史构建记录保留当时名称。新默认配置和 `personad` / `jarvisd` 的兼容见 [命名与兼容](config.md#命名与兼容)。

都在构建配置里（`docs/config.md`）：`app.bundle_id` / `app.team_id`、`app_store.key_id` / `issuer_id` / `key_path`（API key 文件只放在
仓库外，比如 `~/.appstoreconnect/`）、`app_store.app_id`（App 记录的数字 ID）、`app_store.testers`、`app_store.feedback_email`。

| 项目 | 说明 |
| --- | --- |
| Bundle ID | iOS `<bundle_id>`、手表 `<bundle_id>.watchkitapp`、Live Activity `<bundle_id>.liveactivity`、通知服务扩展 `<bundle_id>.notify`（推送中继，`docs/push-relay.md`），用 `scripts/release/asc_signing.py` 注册 |
| App ID 能力 | iOS：`PUSH_NOTIFICATIONS`、`HEALTHKIT`、`APP_GROUPS`；Live Activity：`APP_GROUPS`；手表：无。App Group 本身只能在网站上建、挂到 App ID 上（下面「App Group」） |
| 描述文件 | 「Jarvis App Store」「Jarvis Watch App Store」（`IOS_APP_STORE`）；`asc_signing.py` 会删了重建并装到 `~/Library/MobileDevice/Provisioning Profiles/` |
| 签名身份 | 登录钥匙串里的 `Apple Distribution: …（team_id）` |
| App Store Connect App ID | 网页建好 App 记录后填进配置的 `app_store.app_id` |

API key 的角色是 App Manager，用不了 cloud-managed signing（`-allowProvisioningUpdates` 云签名会失败），所以
归档走自动签名，导出时 `release.sh` 按配置生成 ExportOptions（`scripts/persona_config.py export-options`），手动指定两个 App Store 描述文件重签。

## 首次准备（顺序）

1. 填好 `~/.config/persona/config.json`（`app`、`app_store` 的 key 部分）。
2. `python3 scripts/release/asc_signing.py`：注册两个 Bundle ID、给 iOS 打开推送和 HealthKit、建两个 App Store 描述文件并装到本机。幂等，能力变了随时重跑。
3. **网页创建 App 记录**（API 不能建）：App Store Connect › App › 新建 App：iOS、名称、主要语言、Bundle ID、SKU、完全访问权限。
   把 App ID 填进配置的 `app_store.app_id`。
4. `python3 scripts/release/asc_testflight.py setup`：建内部群组 `Internal`（`hasAccessToAllBuilds`）并把配置里的测试员加进去。
5. `NO_UPLOAD=1 ./scripts/release/release.sh` 验证归档 + 导出（不需要 App 记录就能跑）。
6. `./scripts/release/release.sh` 上传第一个构建 → `python3 scripts/release/asc_testflight.py wait` 等处理完并自动加入 Internal。

没填 `app_store.app_id` 时 `scripts/release/asc_testflight.py` 会直接报错提示。

## 每次发版

```sh
./scripts/release/release.sh                      # 构建号 = yyyymmddHHMM；归档、检查、上传、打 testflight/<构建号> 标签
python3 scripts/release/asc_testflight.py status  # 最近几个构建和处理状态
python3 scripts/release/asc_testflight.py info "这次的 What to Test"
git push --tags
```

构建号也可以手动给：`./scripts/release/release.sh 42`。

`release.sh` 做的事：

1. `xcodegen generate`（源头是 `apple/project.yml`，生成 `Persona.xcodeproj`；旧 `Jarvis.xcodeproj` 是工程入口软链，生成内容不入库）。
2. Release 归档到 `~/Library/Developer/Xcode/Archives/<日期>/Persona <构建号>.xcarchive`（dSYM 留给 Organizer 符号化），
   同时嵌入手表 App。
3. `check_bundle_strings.py` 检查 `Persona.app` 和 `Persona.app/Watch/Persona.app`：Info.plist 里的 `*UsageDescription`
   不能为空或等于 key，`*.lproj/InfoPlist.strings` 同理（占位符文案会被 App Review 自动拒）。
4. `strings` 检查 Release 二进制里没有 `pairURL`、`openSession`、`showNewChat` 这些只在 `#if DEBUG` 里的模拟器开关。
   `check_bundle_privacy.py` 检查整个 bundle 里没有个人的值（外部测试是公开链接，见文末）。
5. `xcodebuild -exportArchive`：手动签名 + `method=app-store-connect` + 两个描述文件映射。**这一步才是 App Store 签名**：
   归档用的是自动签名的开发描述文件（所以归档里 `aps-environment` 是 `development`），导出时按 App Store 描述文件重签成
   `production`。`NO_UPLOAD=1` 时把 `destination` 从 `upload` 改成 `export`，并额外校验导出的 ipa 里 `aps-environment`
   确实是 `production`。
6. `scripts/release/entitlements.py check`：归档里（`NO_UPLOAD=1` 时再加导出的 ipa）每个 bundle 都带着它 target 的钥匙串组，Info.plist 里的组名也在其中，见下面「entitlements」。
7. `git tag -f testflight/<构建号>`。

归档之后大约 10–30 分钟构建才会出现在 TestFlight。`asc_testflight.py wait [分钟数] [构建号]` 会每 30 秒查一次，等到
`processingState=VALID` 后写默认的 What to Test 并加入 `Internal` 群组；`INVALID` / `FAILED` 会直接报错退出
（原因在 App Store Connect 的构建页面里）。**要带上 release.sh 打印的构建号**：刚上传完新构建还查不到，不带构建号时
「最新的构建」是上一个（早已 VALID），会把 What to Test 写到旧构建上（2026-10-25 踩过）。

## 用 asc 发版

`scripts/release/asc_cli.py` 用你自己的 App Store Connect API key 跑 [App Store Connect CLI](https://github.com/rorkai/App-Store-Connect-CLI)（`asc`），上传、TestFlight、提审、元数据、签名都能做。
key 和上面的发版脚本是同一份配置，配一次就都能用。脚本 pin 了 asc 的版本和 sha256，第一次运行时下载到 `~/.cache/persona/asc/<版本>/`，校验不过就不装。遥测始终关闭。

1. **配 key**：在 App Store Connect › 用户和访问 › 集成 › App Store Connect API 建一个团队 key（角色 App Manager 就够传构建和提审），下载 `AuthKey_<Key ID>.p8`
   放到仓库外（如 `~/.appstoreconnect/`，`chmod 600`）。把 Key ID、Issuer ID、文件路径和 App 记录的数字 ID 填进 `~/.config/persona/config.json` 的
   `app_store.key_id` / `issuer_id` / `key_path` / `app_id`。也可以用环境变量 `ASC_KEY_ID`、`ASC_ISSUER_ID`、`ASC_PRIVATE_KEY_PATH`（或 `ASC_KEY_PATH`）、`ASC_APP_ID`，环境变量优先。
   仓库的 `.gitignore` 忽略 `*.p8` 和 `.asc/`。
2. **检查**：`python3 scripts/release/asc_cli.py check`。只在本机读配置，不发任何请求：列出 asc 的版本和位置、Key ID 的末四位、key 文件和它的权限、App ID，最后一行是 `ok`。
   缺什么就报出环境变量名和配置项名。要确认 key 在 Apple 那边有效，跑 `python3 scripts/release/asc_cli.py auth status --validate`（会发请求）。
3. **用**：
   ```sh
   UPLOADER=asc ./scripts/release/release.sh                         # 归档、导出、检查 ipa，用 asc 上传到 TestFlight
   python3 scripts/release/asc_testflight.py wait 45 <构建号>           # 之后照旧：进 Internal 组、写 What to Test、挂外部组
   python3 scripts/release/asc_cli.py review submit --version <版本> --build-number <构建号> --dry-run   # 提审前先看要做哪几步，不改任何东西
   python3 scripts/release/asc_cli.py review submit --version <版本> --build-number <构建号> --confirm   # 把已上传的构建挂到 App Store 版本上，提交审核
   python3 scripts/release/asc_cli.py <任何 asc 命令>                 # 例如 builds list --output table、review status、validate --version <版本>
   ```
   `UPLOADER=asc` 走 `NO_UPLOAD=1` 的导出和 ipa 检查（`aps-environment` 是 `production`、entitlements），再用 `asc builds upload` 上传；不设时仍是 `xcodebuild` 导出时直接上传。
   asc 自己的帮助：`python3 scripts/release/asc_cli.py --help`、`… <命令> --help`（不需要 key）。

**二开**：改 asc 本身就 fork 上游，在 fork 里 `go build -o ~/bin/asc .`（要 `go.mod` 写的 Go 版本），然后 `ASC_BIN=~/bin/asc python3 scripts/release/asc_cli.py …`，key 和遥测的处理不变。
Persona 自己的发版逻辑（测试组、What to Test、外部测试）继续写在 `asc_testflight.py` 这些脚本里，或者在 `asc_cli.py` 里组合 asc 命令。
**升级 asc**：改 `asc_cli.py` 的 `VERSION` 和 `SHA256`（取自 release 里的 `asc_<版本>_checksums.txt`），跑 `python3 -B -m unittest discover -s scripts/release -p 'test_asc_cli.py'`。

asc 只用 Persona 配置里的这把 key：`asc_cli.py` 运行时去掉 `ASC_PROFILE`、`ASC_CONFIG_PATH`、`ASC_BYPASS_KEYCHAIN`、`ASC_PRIVATE_KEY`、`ASC_PRIVATE_KEY_B64`，
`asc auth login` 存进钥匙串的 profile 不会和它混用。App Store 版本（版本号、截图、描述）要先在网页或用 `asc versions`、`asc metadata` 准备好，`review submit` 才能提交。

## 为什么这样签名

| 环节 | 签名方式 | 描述文件 |
| --- | --- | --- |
| 归档 `xcodebuild archive` | 自动（`CODE_SIGN_STYLE=Automatic`） | 开发期的 `iOS Team Provisioning Profile` |
| 导出 `xcodebuild -exportArchive` | 手动（`signingStyle=manual`、`signingCertificate=Apple Distribution`） | 「Jarvis App Store」/「Jarvis Watch App Store」 |

App Store Connect 的 API key 只能做 App Manager 能做的事，cloud-managed signing 需要 Admin；手动签名绕开这个限制。
改 Bundle ID 能力（比如再加一个 capability）会让旧描述文件失效，重跑 `scripts/release/asc_signing.py` 即可。

## entitlements（2026-11-02）

App 和扩展靠共享的组交东西：推送中继的私钥走钥匙串组（`<team>.<bundle id>.push`，App → JarvisNotify），灵动岛的形象和形象包的图走 App Group（`group.<bundle id>`，App → JarvisWidgets，下一节）。
每个 bundle 的签名里都得有它 target 的 `keychain-access-groups` 和 `com.apple.security.application-groups`，Info.plist 里告诉代码用哪个组（`JarvisPushKeychainGroup`、`JarvisAppGroup`）也得是其中之一，缺一样就静默退回默认值。
App Group 的 ID 本身不带团队前缀，Info.plist 里直接写 `$(JARVIS_APP_GROUP)`，无签名归档里也有值。

- **Info.plist 的组名用 `$(DEVELOPMENT_TEAM).`，不用 `$(AppIdentifierPrefix)`**：后者只在 Xcode 签名时才有值，无签名归档里是空的。entitlements 文件里照旧写 `$(AppIdentifierPrefix)`。
- **无签名归档（`CODE_SIGNING_ALLOWED=NO`，这台 Mac 没有发布证书、导出时云签名）**：归档里的 bundle 没有 entitlements，导出按 bundle 里有什么来签。
  导出前用 `python3 scripts/release/entitlements.py sign <归档>` 由内到外 ad hoc 签名，每个 bundle 带上它 target 的 entitlements（从 `xcodebuild -showBuildSettings` 找，
  构建变量展开，`$(AppIdentifierPrefix)` 是团队的；展不开就报错，不留空）。只对 App 用 `codesign --entitlements <原始文件>`、扩展不带的话，
  导出会把 App 的 `$(JARVIS_BUNDLE_ID)` 展成空、扩展一个组都没有（build 92 就是这样，灵动岛的形象不跟随设置，`docs/live-activity.md`）。
- **导出后查**：`python3 scripts/release/entitlements.py check <ipa>`，有问题逐条列出并非零退出；对 build 92 的 ipa 报 9 处。
  App Group 同样查：每个 bundle 的签名里有它 target 的组，Info.plist 的 `JarvisAppGroup` 是其中之一。
  导出的 ipa / pkg 里推送 entitlement（`aps-environment`，Mac 是 `com.apple.developer.aps-environment`）必须是 `production`。Mac 的 .pkg 同样能查。

## App Group（2026-11-02）

App 和灵动岛扩展（JarvisWidgets）靠 App Group 容器里的文件交形象和形象包的图（`docs/live-activity.md`「形象」；实时活动的扩展在手机上读不了钥匙串）。
组名默认 `group.<bundle id>`，配置的 `app.app_group` 可以改（`docs/config.md`），写进两个 target 的 entitlements（`com.apple.security.application-groups`）和 Info.plist 的 `JarvisAppGroup`。
2026-11-02 已在开发者网站上注册，挂到了 App 和灵动岛扩展的 App ID 上；之后发 TestFlight 的流程（无签名归档 → `entitlements.py sign` → API key 云签名导出）导出的 ipa 里两个 bundle 都带着这个组。

**API key 注册不了 App Group**（2026-11-02 查过）：
- App Store Connect API 没有 App Group 的接口（`GET /v1/appGroups` 是 404）。`bundleIdCapabilities` 只能打开 `APP_GROUPS` 这个能力，挂不上具体的组（settings 只认三个键，见下面「踩坑」）。
  fastlane 的 `produce group` / `associate_group` 走的是 Apple ID 登录（网页的 cookie），不是 API key。
- 导出时的 API key + `-allowProvisioningUpdates` 云签名按 App ID 现有的能力重新生成描述文件，但建不了组、也挂不上组；没挂上就导出失败。
- 所以换一个账号或 bundle id 时，**先在网站上做完，再构建带 App Group entitlements 的包**。

开发者网站上的步骤（developer.apple.com › Account › Certificates, IDs & Profiles，账户持有人或管理员）：
1. Identifiers › 「+」› 选 **App Groups** › Continue › Description 填 `Persona`，Identifier 填 `group.<bundle id>`（和配置的 `app.app_group` 一致）› Continue › Register。
2. Identifiers › 右上角的筛选换回 App IDs › 点 `<bundle id>` › Capabilities 里勾 **App Groups** › 点它旁边的 Configure（已经勾过的是 Edit）›
   勾上 `group.<bundle id>` › Continue › Save › 弹窗里 Confirm（改了能力，这个 App ID 现有的描述文件会失效）。
3. 对灵动岛扩展的 App ID `<bundle id>.liveactivity` 同样做第 2 步。手表和通知扩展不用。
4. 描述文件：云签名的导出（自动签名）会自己重新生成；`release.sh` 的手动签名要重跑 `python3 scripts/release/asc_signing.py`，
   它检查每个描述文件里有没有这个组，没有就打印上面的步骤并非零退出。
5. 发版导出后 `scripts/release/entitlements.py check` 查 ipa 里 App 和 JarvisWidgets 的签名都带着这个组。

## 改商店名

商店名（App Store / TestFlight 里显示的名字，和桌面上的 `CFBundleDisplayName` 无关）要全局唯一，在 App 信息的本地化里。版本还在 `PREPARE_FOR_SUBMISSION` 时 App Manager 的 API key 就能改：

```sh
python3 scripts/release/asc.py GET /v1/apps/<app id>/appInfos                    # 取 appInfo 的 id
python3 scripts/release/asc.py GET /v1/appInfos/<appInfo id>/appInfoLocalizations # 每种语言一条，取 id
# PATCH /v1/appInfoLocalizations/<id>，attributes 写 name（和 privacyPolicyUrl），名字被占用时报 409
```

改名不换 App 记录：Bundle ID、测试组、公开链接、已发的构建和已经装了的人都不受影响。

## 踩坑

- **`bundleIdCapabilities` 不能带自造的 settings**：给 `PUSH_NOTIFICATIONS` 传
  `settings: [{"key": "PUSH_NOTIFICATIONS", ...}]` 会被 409 拒（`settings/0/key` 只接受 `ICLOUD_VERSION`、
  `DATA_PROTECTION_PERMISSION_LEVEL`、`APPLE_ID_AUTH_APP_CONSENT`）。推送和 HealthKit 直接传空 `settings: []`。
- **同名描述文件会挡住重建**：改能力后旧描述文件变成 `INVALID`，同名新文件创建会 409。`asc_signing.py` 先删同名再建。
- **测试组的列表偶尔不完整**（#945）：`GET /v1/apps/{id}/betaGroups` 有时返回空列表、缺几个组或者读超时，脚本以前当成组不存在就新建，
  2026-11-03 一天多出 4 个同名的 Friends，构建挂进了空组、测试员却都在最早那个组里。现在按名字和内外部找组，同名的取最早建的；
  列表要问三次都没有才算没有；只有 `setup`、`external` 会新建，`wait` / `info` / `catch-up` 找不到就报错（发版时只打印一行，附重试命令）。
  已经多出来的空组脚本不删，删不删由人定。
- **内部测试员要用 `POST /v1/betaTesters` 带群组关系创建**：同一个邮箱在别的 App 下已有测试员记录时，直接关联会 409；
  脚本遇到 409 会提示去网页把这个人加进本 App 的 `Internal` 群组。
- **归档里的 `aps-environment` 总是 `development`**：归档用的是开发描述文件，最终签名在导出环节完成，别在归档产物上检查。
- **推送和生产环境**：`UIBackgroundModes` 用 `remote-notification`（手表 App 没有后台模式）；`ITSAppUsesNonExemptEncryption=false`
  已经在 `apple/project.yml` 里，省掉每次上传的出口合规问答。
- **不用 iCloud 目录**：这台 Mac 没解锁，涉及 `~/Library/Mobile Documents` 的一律跳过。

## 待 leader 处理

（无。2026-10-24：clipJSON 截断中文的问题已修，transcript.go 已 gofmt。）

## 推送（APNs）

- 在 developer.apple.com 建一个 APNs key（Sandbox & Production，Team Scoped），Key ID 和 .p8 文件都不进仓库。
- 文件：hub 机器的 `~/.persona/apns_key.p8`（配置 `~/.persona/apns.json`），另存一份到密码管理器。key 只能下载一次，丢了就重建。
- 页面提示《Apple Developer Program 许可协议》已更新，要账户持有人在 developer.apple.com 接受（不影响建 App 和传 TestFlight）。

## 外部测试（公开链接）

内部测试员必须是团队成员；给朋友用走外部测试：群组「Friends」（`app_store.external.group`）。2026-10-30 用户定的是**公开链接**
（`public_link: true`）：把 `https://testflight.apple.com/join/…` 发到群里，谁点开都能装。2026-11-03 起**不限人数**（`public_link_limit: 0`，原来是 50，满了以后链接不再收人）：`public_link_limit` 为 0 或不写时关掉人数上限，只受 Apple 每个 App 最多 10,000 名外部测试员的限制；写正数就是上限。
当前的公开链接：<https://testflight.apple.com/join/vsjfy3hE>（`asc_testflight.py external` 每次都会打印，变了就更新这里；2026-11-03 核对，是最早那个 Friends 组的链接）。
iPhone 和 Mac 是同一个链接（同一个 Friends 组），在 Mac 上用 TestFlight 打开它就是 Mac 版。
这个链接 2026-11-03 起可以明文写进仓库（用户要求，README 的安装方式就写着它）；官网（`site/`）上放不放另外问用户，现在不放。链接换了（比如组被重建）要把 README 和这里一起改。
不用收集邮箱；`app_store.external.testers` 仍然可以写，列出的邮箱另外收到邀请邮件。`public_link: false` 时只有这些邮箱能加入。

```sh
python3 scripts/release/asc_testflight.py external   # 建群组、按配置开 / 关公开链接、写测试信息、挂最新构建、提交审核；打印公开链接
```

- **公开链接 = 任何拿到链接的人都能装**：构建里不能有任何个人的东西（hub 地址、主机名、邮箱、路径……）。`release.sh` 在上传前跑
  `scripts/release/check_bundle_privacy.py <Persona.app>`（用 `privacy_check.py` 的同一份敏感词，查二进制的字符串、plist、lzfse 元数据；
  bundle id、team id、签名数据本来就在每个构建里，不算）。自己写的发版脚本也要在上传前跑它。
- **每次发版不用另跑命令**：配置里有 `app_store.external` 时，`wait`、`info` 在挂好 Internal 之后会顺带把这个构建挂到 Friends、
  提交审核（有列出的测试员就补邀请）。这一步出错只打印一行（附重试命令 `external`），不会让发版失败。
- **同一版本有构建在审时，新构建交不上去**（422 `ANOTHER_BUILD_IN_REVIEW`，「审完再交」），也没有东西会在审完后再交：
  Mac 110 在审的那段时间，111–127 都挂在 Friends、停在 `READY_FOR_BETA_SUBMISSION`，外部测试员一直拿不到（2026-11-03，#792）。
  `asc_testflight.py [--platform mac] catch-up`：最新的 VALID 构建还没交就挂 Friends、提交审核（不改它的「测试什么」），
  有别的在审就说一声等下一次，已经交过 / 过审 / 被拒的什么都不写。本机的 auto-tf 每轮（等新提交的每 10 分钟）对 iPhone 和 Mac 各跑一次。
- 外部测试的构建要过 **Beta App Review**（第一次约一天，同一版本后面的构建多数自动通过）。提交前必须有联系人：
  `app_store.external.review` 的 `first_name` / `last_name` / `phone` / `email`，缺了脚本只挂构建、不提交，也不加测试员。
- 公开链接在没有通过审核的构建之前就能打开（API 直接给出地址），但页面显示「This beta isn't accepting any new testers right now」，
  审核通过后才能加入。
- 审核状态看构建的 `buildBetaDetail.externalBuildState`：`READY_FOR_BETA_SUBMISSION`（挂了没交）→ `WAITING_FOR_BETA_REVIEW` →
  `IN_BETA_REVIEW` → `BETA_APPROVED` / `BETA_REJECTED`。
- 朋友的 hub 是他们自己部署的：没有开发者账号的 APNs key 时，新版 App 通过发布方的推送中继收通知和实时活动
  （需发版配置填 `app.push_relay`、设备登记成功，见 `docs/push-relay.md`）；未配置中继的旧版只能在 App 打开时实时接收。
  Gmail 登录要他们自己建 Google OAuth 客户端、写进自己 hub 的 `hub.json`（`docs/mail.md`）。
- 测试信息要有隐私政策地址（`app_store.external.privacy_url`），中英文说明在 `asc_testflight.py` 的 `BETA_INFO`。
- App 没有 hub 就只有配对页：审核备注（`review.notes`）要讲清楚这是配合自建 hub 的 App，否则容易被问「怎么用」。

## Mac 版（2026-11-02）

Mac App（target `PersonaMac`，旧 `JarvisMac` scheme 兼容）和 iPhone 用同一个 bundle id，所以是 App Store Connect 里**同一个 App 记录**（加了 macOS 平台，版本 1.0），
进同一个 TestFlight：同样的 Internal / Friends 组、同一个公开链接；测试员在 Mac 上装 TestFlight 就能看到 Mac 构建。

- **构建号**：一个号在每个平台里只能用一次，不同平台可以重号。规则：**一次发布的 Mac 构建和这次的 iPhone 构建用同一个号**
  （同一个号 = 同一个提交，查问题时一眼对上）；iPhone 的号、单独发的 Mac 都取**两个平台里最大的号加一**
  （`python3 scripts/release/asc_testflight.py next-build`），这样不会有一个号在两个平台上指两个不同的提交。
- **`asc_testflight.py --platform mac`**：`status` / `wait` / `info` / `external` / `catch-up` 都只看 Mac 的构建；不带时只看 iPhone 的。
  以前不分平台，有了 Mac 构建以后「最新的 VALID 构建」可能是另一个平台的，「测试什么」会写错地方。
- **图标**：系统不给 Mac 图标切圆角，图标自己带圆角方块和阴影（macOS 的网格：1024 里居中 824）。`AppIcon` 图标集里 iOS 的单尺寸图之外
  另有 `idiom: mac` 的 16–512（1x / 2x），由 `python3 scripts/assets/make_icon.py mac` 从 iPhone 的图生成（全部重画时也会一起写）。
  缺了它 Mac 包里没有图标，Mac App Store 拒收。Xcode 26 编出来的 `AppIcon.icns` 只有 16 和 128 两档，完整的各档在 `Assets.car` 里
  （系统按 `CFBundleIconName` 去那里取），所以检查看的是 `Assets.car`（`xcrun assetutil --info`）里有没有 512 和 512@2x。
- **Info.plist**：`LSApplicationCategoryType`（效率）、各项权限说明都在 `apple/project.yml` 的 `PersonaMac` target 源头里。
- **沙盒**：`com.apple.security.app-sandbox` 加网络（客户端、端口转发的本机服务端）、用户选的文件、通讯录 / 日历 / 位置 / 照片、
  麦克风 / 摄像头（`project.yml` 的 entitlements）。
- **推送**（2026-11-02 起，`docs/mac.md`「远程推送（Mac）」）：`com.apple.developer.aps-environment`（macOS 上的名字，iOS 是 `aps-environment`）。
  App ID 是 UNIVERSAL、早就开着 Push Notifications，开发者账号不用改；归档里是 `development`，App Store 导出时云签名改成 `production`。
  包里另有通知服务扩展 `PersonaNotify.appex`（target `PersonaMacNotify`，module / 通知主类仍用 `JarvisNotify`，`Contents/PlugIns/`，沙盒 + `.push` 钥匙串组，#432），
  bundle id 和 iPhone 的扩展同一个（UNIVERSAL 的 App ID），导出时云签名给它建 Mac 的描述文件；`entitlements.py sign / check` 一起签、一起查。
- **发布前检查**和 iPhone 一样：权限说明（`check_bundle_strings.py`）、隐私扫描（`check_bundle_privacy.py`，认 Mac 的
  `Contents/` 结构，跳过 `embedded.provisionprofile`，通用二进制整个扫）、Release 里没有 DEBUG 开关；另外查分类和图标里有 512 / 512@2x。
- 导出是 `.pkg`（ExportOptions 的 `method` 同样是 `app-store-connect`），和 iPhone 一样云签名：App 是 Apple Distribution，
  `.pkg` 是 3rd Party Mac Developer Installer（`pkgutil --check-signature` 会写「issued by Apple (Development)」，这类证书都这么写）。
  上传 `altool -t macos`。本机的发版脚本和 iPhone 一样先 `entitlements.py sign` 再导出，导出后 `entitlements.py check <.pkg>`：
  每个 bundle 带着它 target 的 entitlements，推送是 `production`（`check` 认 Mac 的 `Contents/` 结构和 `.pkg`，iPhone 的 ipa 也查推送环境）。
- **包里的文件要别的用户也能读**（Mac 110，2026-11-02）：`.pkg` 里有只有属主能读的文件，上传时报 ITMS-90255
  「files that are only readable by the root user」（macOS 以运行 App 的用户身份验签名）。自动发版（auto-tf）的 umask 是 077，
  脚本里 `cp -R` 复制的归档全成了 0700 / 0600；在交互 shell（umask 022）里试永远看不出来。发版脚本开头 `umask 022`、复制归档用 `ditto`（保留原权限），
  导出前对 `.app`、导出后对 `.pkg` 展开的 App 查一遍：所有文件 a+r、所有目录 a+rx，不满足就不传。iPhone 的脚本也一样改了（ipa 不查这个，110 照样过）。
- **自动发布**：发完 iPhone 构建后，这次的提交里有 Mac App 用到的改动（`apple/Mac`、`apple/Shared`、`apple/Persona` 除了只属于 iOS 的
  几个文件、`apple/Packages`、`project.yml`、`Package.resolved`）就用同一个号接着发 Mac；Mac 的结果另外报，失败不影响 iPhone 那次。

## 发布 personad（`scripts/release/publish.sh`，2026-10-31）

让别人不拿源代码也能装服务端：编好的 `personad` 和安装脚本放在 `site/` 那个 worker 上（配置的 `publish.site_url`）。

```sh
curl -fsSL <publish.site_url>/install.sh | sh   # 装到 ~/.persona/bin/personad，按 SHA256SUMS 校验，不对就不装
```

- 装好 `personad` 后，这台电脑上找不到 herdr（PATH 和 node 会找的 `~/.local/bin` 等目录）就用 herdr 官方安装器（`https://herdr.dev/install.sh`）
  一起装上：首次安装完 Agents 里就能看到会话。装不上只打一行警告，不影响 `personad`；用 Orca / Herdr App 的电脑 `JARVIS_SKIP_HERDR=1` 跳过。
  `deploy.sh` 往每台电脑装 `personad` 时做同样的事（`scripts/deploy/ensure-herdr.sh`，两处的步骤保持一致）。

- `release.sh` 传完 TestFlight 后接着跑它，版本号是 `<提交>-<构建号>`，和这一版 TestFlight 对得上；`NO_PUBLISH=1` 只发 TestFlight，
  配置里没有 `publish.site_url` 也不发。单独跑：`./scripts/release/publish.sh`，`NO_DEPLOY=1` 只编译和检查（产物在 `build-publish/`）。
- 编四个平台（linux / darwin × amd64 / arm64），`-tags public -trimpath -s -w`，写进 `channel=release`、build 号（UTC `YYYYMMDDHHMM`）和 `updateURL`：
  - **网页打进二进制**（`server/internal/hub/webdist`，`go:embed`）：hub 机器上没有 `~/.persona/web` 时就用二进制里的；有就照旧用目录里的。
  - **不带任何 skill**：lark-connector 是从私有的上游仓库拷来的（GPL-3.0），不能再分发；skill 放在各台机器的 `~/.persona/skills/`，
    所以只有一种二进制，我们自己的 hub 也用它（`docs/hub-update.md`）。脚本会确认二进制里没有 lark-connector。
    `-tags public` 不改变任何代码（`TestOneBuild`），只是给隐私扫描的标记。
  - **版本清单** `dl/latest.json`（各平台压缩包和解压后的 sha256、build 号、commit），不签名（用户的决定，取舍见 `docs/hub-update.md`「安全」）；
    发布前检查 build 号比线上的大、线上那版的 commit 是这次的祖先，server/、web/ 没有没提交的改动。清单带着这次包含的所有提交（`ancestors`）：
    各 hub 读它自动更新，但只在它包含自己正在运行的提交时才自己装（`docs/hub-update.md`「防退回」）。
  - **一起发布的程序**：`jarvisd-telegram`（Telegram 连接器，单独一个程序，`docs/plan-telegram.md`）四个平台各编一份，和 personad 同一套
    `-X`、同一次隐私扫描和体积检查，`version -v` 必须和 personad 的一样；清单的 `programs` 列出它们，hub 自动更新时一起换。
  - 上传前跑 `check_bundle_privacy.py --whole-short`：二进制里不能有配置里的任何值（Go 的模块路径除外；四个字以内的值按整词比，
    Unicode 表和压缩过的 JS 里到处是短的字母组合）。公开依赖中的少量协议名、官方联系链接和标准网络常量按精确上下文分类，不放行域名或私网段；
    要求二进制的 `public` 标记、未 replace 的依赖及 checksum 与 `server/go.sum` 一致，构建前 `go mod verify`。已登记的个人值始终优先拦截，见 `docs/lessons/testing.md`。
- 同样四个平台再编连接器 `jarvis-link-<os>-<arch>`（2026-11-02，node 服务，`install.sh --node`，见 `docs/link.md`「装 node 服务」），一样过隐私扫描、压缩、进 `SHA256SUMS`。
- 下载包是 `jarvisd-<os>-<arch>.gz`：原始二进制先过隐私扫描，再用 `gzip -n -9` 压缩，`SHA256SUMS` 校验压缩包；安装脚本校验通过后解压安装，校验失败 / 解压失败不覆盖旧版本。
  Workers 静态资源每个文件最多 25 MiB（[官方限制](https://developers.cloudflare.com/workers/platform/limits/#static-assets)），上传前 `check_asset_sizes.py` 检查整个发布目录；本地 `build-publish/bin` 保留未压缩二进制供排查，但不上传。
  回归：`python3 -B -m unittest discover -s scripts/deploy -p 'test_compressed_publish.py'`。
- 只托管最新一版：每次部署整个替换 `dl/`。部署用配置的 `publish.wrangler`（没有就 `npx wrangler`）；cf 的 OAuth 一小时过期，
  报鉴权错就 `cf auth login` 再跑。部署后会读回 `dl/VERSION`、`dl/SHA256SUMS`、`dl/latest.json` 确认上去的就是刚编的。
  推送中继在同一个 Worker 里（`/relay/*`），每次发布会把它一起重新部署。
- 推送中继已部署（2026-11-02）：常驻 `personad relay` + Cloudflare Tunnel + 官网 Worker 转发，公网健康、源站保护、登记和鉴权已验证。
  发版配置需填 `app.push_relay`，安装带通知扩展的新 TestFlight 才会登记；真实设备上的通知解密仍要单独验收，见 `docs/push-relay.md`。
- 还没有的（别人能装但还不顺手，见 `docs/config.md`）：一键 `jarvisd setup`（现在 hub 要自己准备 Postgres、`hub.json`）、
  macOS 二进制的 Developer ID 公证（用浏览器下载会被 Gatekeeper 拦，`curl` 不会）。

## 发布 hub 镜像（`.github/workflows/hub-image.yml`，2026-11-01）

Docker 部署的 hub（`docs/docker.md`）用 ghcr 上的镜像，linux/amd64 + linux/arm64：

```sh
git tag v1.2.0 && git push origin v1.2.0   # -> ghcr.io/<owner>/<repo>:1.2.0 和 :latest
```

- 先冒烟（amd64 + compose），过了才构建两个架构并推；带 `-` 的预发布 tag（`v1.3.0-rc1`）只推版本号，不动 `latest`。

- 不打 tag 想要一个当前分支的镜像：Actions 里手动运行 hub image（`edge`、`sha-<提交>`）。
- PR 上只冒烟，不推（推送那一步才有 `packages: write`）。
- 镜像里的 personad 用 tag 去掉 `v` 当版本（`personad version`）；node 跟它不一样时 App 提示「重新部署」，用的是镜像里带的 node 构建。
- 镜像里的 personad 是 `docker` 渠道：不带 skill（和所有二进制一样），不自己更新，有新镜像时提示拉取（`docs/hub-update.md`）。
- 仓库私有时包也私有，NAS 要 `docker login ghcr.io`（`docs/docker.md`「镜像」）。

