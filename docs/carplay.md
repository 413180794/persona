# CarPlay 语音长聊

在车里和 Persona 语音长聊（双工、随时插话）。Siri 只当入口；通话本身走共享引擎 `VoiceConversation(surface: .carPlay)` / `VoiceCalls`（`docs/voice.md` 6，#3789），绑的是助手主对话。

## 怎么用

- 车上点 CarPlay 里的 Persona 图标，或对 Siri 说「打开 Persona」：App 直接进入聆听，不用先点按钮。
- 对 Siri 说「跟 Persona 聊聊」（`TalkToPersonaIntent`）：Persona 正在车机屏幕上时开始对话；连着 CarPlay 但屏上是别的 App（比如地图）时只说一句「先在车里的屏幕上打开 Persona」，不开麦；没连时说明先连上 CarPlay（`CarPlayTalk`）。
- 界面按钮：结束 / 打断 / 继续。
- 离开 Persona 的界面（比如切到地图）就停：Apple 要求语音对话时语音控制界面在屏上。回到 Persona 界面会接着聊。
- 自己开始只在连上 CarPlay 后、以及每次回到前台时各一次（`CarPlayAutoStart`）。通知横幅、Siri、来电结束只会让界面「失活 → 激活」，不会重新开麦；按过「结束」、空闲停掉或因缺权限 / 没有语音扩展结束后，只有「继续」或 Siri 能再开始。
- 麦克风权限还没决定时不弹窗（弹窗在口袋里的手机上，车上会一直卡在「正在连接」），直接结束并提示先在手机上用一次听写或录音允许麦克风（`CarPlayMicPermission`）。
- 10 分钟没人说话自动停（引擎的空闲规则）。

## 双工语音入口

CarPlay 是共享引擎外面的一层薄壳（`CarPlayVoiceSession`）：

1. 解析助手主对话（`AppModel` 或 `IntentSupport` + `assistant()`）和第一个 `running` 的 voice 扩展（`VoiceAvailability.pick`）。
2. `VoiceCalls.start(..., surface: .carPlay, deps: VoiceDeps.live` 且 `mic` 换成不弹窗的 `CarPlayMicPermission`）。
3. 引擎经 `extensionPath` 连 `/api/v1/extensions/<id>/ws`（旧 hub 仍走 `/api/v1/plugins`），发协议 2 的 `start`，收 `ready`，之后麦克风与播放由 `PhoneVoiceAudio` 负责。

接哪个实时服务是 hub 的配置。需要干活时，扩展把事交给绑定的主对话，结果由快层口头转述（`docs/voice.md`）。

## 音频

- 与手机语音通话共用 `PhoneVoiceAudio`：`.playAndRecord` + `.voiceChat`、回声消除、按代丢弃 `clear` 前的声音。
- 只在对话进行中激活音频会话，结束时 `notifyOthersOnDeactivation`，让音乐恢复。
- 麦克风和录音、听写共用所有权（`RecordingMicrophone`）；录音或听写会结束通话。
- 来电等系统打断会结束对话，不会在后台悄悄接着录。
- 「进后台就结束」只对 `surface == .phone`；CarPlay 只在自己离开语音界面时停（`VoiceCalls.end(.left, from: .carPlay)`）。

## Apple 的限制

以下是 2026-11 查的 CarPlay Developer Guide 的要求：

- entitlement 名是 `com.apple.developer.carplay-voice-based-conversation`，要 iOS 26.4 以上，要 Apple 批准。
- 语音对话类的模板最多 3 层。
- 启动即语音：进入 App 就开始聆听。
- 语音进行时必须显示语音控制界面（`CPVoiceControlTemplate`）。
- 只在用语音时占用音频会话。
- 回答不要显示文字和图片，车里只出声。
- 录音只能配合语音控制模板使用。

## 开启步骤

没批准前别开，否则签名失败。

1. 在 Apple 开发者网站申请 CarPlay 语音对话 entitlement（CarPlay 申请页）。批准后在 Persona 的 App ID 上打开它。
2. 重新生成发版用的 provisioning profile（`docs/release.md`）。
3. 在 config.json 写 `"app": {"carplay_voice": true}`，然后运行 `python3 scripts/persona_config.py xcconfig`。它会生成 `apple/PersonaCarPlay.entitlements`（git 忽略），并让 Persona 用它签名。

没开时 CarPlay 里看不到 Persona，其他一切照旧。

## 编译保护

- `CPVoiceControlState.actionButtons` 是 iOS 26.4 SDK 才有的 Swift 属性。**不能用 `#if compiler(>=6.4)` 包**：Swift 6.4 只出现在 iOS 27 SDK 的 Xcode 上，带 iOS 26.4 及以上 SDK 的 Xcode 26.x（Swift 6.3）也有这个属性，这样包会让所有 Xcode 26 编出来的包都没有按钮。
  现在经 Objective-C 运行时设（`CarPlayVoiceButtons`：`responds(to: setActionButtons:)` + KVC），任何 SDK 都编得过，iOS 26.4 起（用得上语音模板的系统）都有按钮；`CarPlayVoiceButtonsTests` 扫源码防回退。
- 万一运行时没有这个属性，界面不带按钮，「已结束」的第二行改成提示用 Siri 说「跟 Persona 聊聊」再聊。
- 实编验证过的只有 Xcode 27.2（iOS 27.2 SDK）；Xcode 26.x 未实编。

## 怎么验证

- 模拟器：Xcode › Open Developer Tool › Simulator，菜单 I/O › External Displays › CarPlay（或 CarPlay Simulator App）。
- 没有 entitlement 时，CarPlay 里不显示 Persona。
- 单元测试：`CarPlayVoiceScreenTests`、`CarPlayWiringSourceTests`、`CarPlayVoiceButtonsTests`；引擎侧见 `VoiceConversationTests` / `VoiceCallsTests`。

## 没做 / 推迟

- 手机上的语音通话界面（#3508）。
- 选模型和音色：先用扩展默认（`docs/voice.md`）。
- 在别的 App（比如地图）前台时继续聊：Apple 要求语音界面在屏上。
- 仪表盘和方向盘按键控制。
