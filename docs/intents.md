# App Intents（Siri / 快捷指令）

`ios/Jarvis/Intents/` 里是 4 个 AppIntent，用来在不解锁、不打开 App 的情况下驱动 hub：问 Claude、看状态、批准第一个待处理。
它们不依赖 App 在前台，也不用 WebSocket：每次调用都现读 `Keychain.serverConfig`，用 `APIClient` 直连 hub 的 REST 接口。

| Intent | 标题 | 作用 | 运行模式 |
| --- | --- | --- | --- |
| `AskClaudeIntent` | 问 Claude | 给某台机器上最近的活会话发一条消息，或按记住的目录/启动器新开一个 | 后台 |
| `JarvisStatusIntent` | Jarvis 状态 | 「N 个在工作，M 个等你处理」+ 第一个待处理的内容 | 后台 |
| `ApproveFirstIntent` | 批准第一个待处理 | 允许最早的一条工具授权，或批准计划（`bypassPermissions`） | 后台 |
| `OpenSessionIntent` | 打开会话 | 把 App 切到对话页并跳到该会话（`AppModel.shared.openChat`） | 前台 |

`OpenSessionIntent` 不是给人单独用的：`AskClaudeIntent` 的返回值里带着它，Siri 结果卡片上的按钮点一下就进那个会话。

## 文件

| 文件 | 内容 |
| --- | --- |
| `IntentSupport.swift` | 文案（`IntentText`）、短语模板（`IntentPhrases`）、错误句子（`IntentFailure`）、取配置（`IntentSupport`）、选择规则（`IntentPicker`） |
| `MachineEntity.swift` | `MachineEntity` + `MachineEntityQuery`（`/api/v1/machines`，在线优先，支持按名字模糊匹配） |
| `OpenSessionIntent.swift` | `SessionEntity` + `SessionEntityQuery`、`OpenSessionIntent` |
| `AskClaudeIntent.swift` / `JarvisStatusIntent.swift` / `ApproveFirstIntent.swift` | 三个业务 intent |
| `JarvisShortcuts.swift` | `AppShortcutsProvider`，中文短语和图标 |

## 选择规则

`AskClaudeIntent`：

- 机器：显式给的 `machine` 参数 → `UserDefaults "newChat.machine"`（新建对话页记住的那台）→ 第一台在线的。手动选的机器离线会直接报错，不会偷偷换一台。
- 不发新会话时：该机器上 `status != offline` 的会话里 `updated_at` 最新的那个（`docs/protocol.md` 的 `Session.updatedAt`）。
- `newSession = true`：用 `UserDefaults "launcher.<machine>"` / `"cwd.<machine>"`（新建对话页每次开始对话都会写）调
  `POST /api/v1/sessions`，把这句话作为 `prompt` 一起发过去 —— 只发一次，避免启动后再补一条造成重复。
- 这两个 key 不存在时提示「先在 App 里新建一次对话」，因为 Siri 里没法选目录。

`ApproveFirstIntent`：

- 待处理按 `created_at` 从早到晚取第一条。
- `permission` → `{allow:true}`；`plan` → `{approve:true, mode:"bypassPermissions"}`（和 App 里「批准并自动执行」一样，
  见 `docs/claude-integration.md`）。
- `question` / `screen` 需要看屏幕，只回一句「需要打开 Jarvis 处理」，不落任何决议。

## 错误文案

全部是中文，一句话说清下一步做什么（`IntentFailure.sentence(for:host:)`）：

| 情况 | 文案 |
| --- | --- |
| 没配对 | 还没配对。先打开 Jarvis 扫 `jarvisd hub pair` 的二维码。 |
| 401 / 403 | 配对已失效，打开 Jarvis 重新配对。 |
| 410 | 这个已经处理过了。 |
| 网络 / 超时 | 连不上 hub（`hub` 地址），手机要在公司网络或 removed 上。 |
| 没有机器 | 现在没有在线的机器。看看 example-machine 上的 jarvisd 和 SSH 隧道。 |
| 该机器没活会话 | `<机器>` 上没有运行中的会话。打开 Jarvis 新建一个，或者说「新会话」。 |
| 没有待处理 | 没有等你处理的事。 |

hub 是公司内网的明文 HTTP：intent 用 `.background` 模式，不会因为网络不可达把 App 拉到前台。

## 短语

`JarvisShortcuts` 里三个快捷指令（Spotlight / Siri / 快捷指令 App 都能看到）：

- 「用 Jarvis 问 Claude」「Jarvis 问 Claude」「用 Jarvis 问一下 Claude」
- 「Jarvis 状态」「Jarvis 现在怎么样」「用 Jarvis 看进度」
- 「用 Jarvis 批准第一个」「Jarvis 批准待处理」

规则（App Intents 的硬要求）：

- 每条短语必须含 `\(.applicationName)`，否则整条被系统丢掉。
- 短语里只能插值 `AppEntity` / `AppEnum` 类型的参数，普通 `String` 参数（比如 `text`）插不进去，会报
  `Invalid parameter type. AppEntity and AppEnum are the only allowed types for text`。所以「问 Claude」的短语不带内容，
  说完之后 Siri 再问一句「要跟 Claude 说什么？」（`@Parameter(requestValueDialog:)`）。
- `shortTitle` / `systemImageName` 必须给，短标题是 Siri 结果卡片和快捷指令 App 里显示的名字。

## 并发

项目开了 `SWIFT_DEFAULT_ACTOR_ISOLATION=MainActor`，而 AppIntent / AppEntity / EntityQuery 的协议要求是 nonisolated。
做法是把协议成员一个个标 `nonisolated`（`init`、`perform`、`title`、`supportedModes`、`parameterSummary`、query 方法），
被它们调用的纯逻辑放在 `nonisolated enum` 里；`@Parameter` 存的是可变存储属性，不能标 `nonisolated`，保持 MainActor 即可。

`OpenSessionIntent` 是唯一碰 `AppModel` 的地方，用 `await MainActor.run { … }` 进去改 `selectedTab` / `openChat`。
它的 `target` 是 `SessionEntity`，`id` 形如 `machine/session`，和 DEBUG 的 `-openSession` 参数同形。

## 测试

```sh
cd ios && xcodegen generate -q && xcodebuild -project Jarvis.xcodeproj -scheme Jarvis \
  -destination 'platform=iOS Simulator,name=iPhone 17 Pro' test -only-testing:JarvisTests/IntentsTests
```

`ios/JarvisTests/IntentsTests.swift` 只测纯函数：按机器 / 会话 / 待处理的选择规则、每种交互类型的文案、错误句子、
`Resolution` 的构造、实体 id。网络部分不测（要真 hub）。

## 编译与元数据

```sh
cd ios && xcodegen generate -q && xcodebuild -project Jarvis.xcodeproj -scheme Jarvis \
  -destination 'generic/platform=iOS Simulator' -derivedDataPath ../build/DerivedDataWorker build
```

编出来的 `Jarvis.app/Metadata.appintents/extract.actionsdata` 里能看到 4 个 action、3 条 autoShortcut；
Siri 是否可用还取决于真机安装后系统索引，模拟器里只能确认元数据生成正确。

## 待 leader 处理

- **project.yml 不用改**：`Jarvis/Intents/` 落在 targets.Jarvis 的 `sources: - path: Jarvis` 下面，XcodeGen 自动收进来；
  AppIntents 的宏和元数据也由 Xcode 自动处理。已实测 `build` 与 `test` 都通过。
- **发布检查**：`scripts/release.sh` 目前只查 `Info.plist` 的用途文案和 DEBUG 字符串。如果想更稳，可以在归档后断言
  `Jarvis.app/Metadata.appintents/extract.actionsdata` 存在（缺了说明 AppIntents 元数据没生成，Siri 就找不到这些动作）。
  这需要改 `scripts/release.sh`，不在本任务允许的路径里。
- **本地化**：intent 的 `title` / `description` / 短语都是中文字面量（`developmentLanguage: zh-Hans`），没有走 String Catalog。
  以后要出英文界面，需要给 `AppShortcutsProvider` 和 intent 的 `LocalizedStringResource` 建 `Localizable.xcstrings`。
- **`newChat.machine`**：Siri 里选中的机器不会写回这个 key（只有 App 的新建对话页会写），所以「上次用的机器」始终以 App 里为准；
  想改成「Siri 也记住」的话，改 `AskClaudeIntent.resolveMachine` 即可，但那会和 App 的默认机器互相覆盖，暂不改。
