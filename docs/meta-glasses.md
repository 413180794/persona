# Meta 智能眼镜

让 Persona 用上 Meta 的智能眼镜（Ray-Ban Meta 一代 / 二代、Meta Ray-Ban Display、Oakley Meta HSTN / Vanguard）：
眼镜的麦克风当录音的输入，眼镜的摄像头拍一张当对话的上下文。不做第二套录音产品，也不做眼镜上的界面。issue #6。

## 一句话

- **眼镜麦克风**：知识库 › 录音里的手机录音（`RecordingCaptureControls`，#3911 之前是「录音上下文」）多一个「麦克风」选项（手机麦克风 / 眼镜或蓝牙耳机），走标准蓝牙 HFP，不需要 Meta 的 SDK，默认关着的都不受影响。
- **眼镜拍照**：按配置接入 Meta 的 Wearables Device Access Toolkit（DAT），输入框的附件菜单里「眼镜拍照」，看着实时画面拍一张，附进消息。默认不编进 App。

## Meta 给第三方的接入方式（2026-11 核实）

| 能力 | 怎么拿 | 状态 |
| --- | --- | --- |
| 麦克风（给人说话用） | 标准蓝牙 HFP：`AVAudioSession` 的 `.playAndRecord` + `.allowBluetoothHFP`，选 `.bluetoothHFP` 输入 | 系统能力，和普通蓝牙耳机一样；16 kHz 或 8 kHz 单声道，做了波束成形，只突出戴眼镜的人自己的声音 |
| 摄像头视频流 | DAT `MWDATCamera`：`DeviceSession` → `addCamera(config:)` → `camera.stream`，360×640 / 504×896 / 720×1280，2–30 fps | 正式 API |
| 拍照 | 流开着时 `stream.capturePhoto(format: .jpeg)`，结果从 `photoDataPublisher` 来；更高清的独立拍照 `camera.photo` | 前者正式，后者实验 |
| 随视频流一起的音频（不做波束成形） | `StreamConfiguration.audioCodec` + `audioFramePublisher` | 实验，不能发布，而且要摄像头流一直开着 |
| 眼镜按键、IMU、眼镜上的语音转写、「Hey Meta」唤起 | `MWDATInputs`、`MWDATMotion`、`MWDATSpeech`、`VoiceInvocationsStream` | 全是实验，要开发者中心单独批 |

要求和限制：

- SDK 是 `https://github.com/facebook/meta-wearables-dat-ios`（SPM），1.0 起要 **Xcode 26.4+ / Swift 6.3+**、iOS 17.2+。
- 眼镜要先在 **Meta AI app** 里配对；开发时在 Meta AI 里打开 Developer Mode，不用在 Meta 那边建应用（`MetaAppID` 为 0 就跳过校验）。
  给别人用要在 Wearables Developer Center 建应用、拿 App ID 和 Client Token，经它的发布渠道（release channel）发给测试的人。
- App 先在 Meta AI 里「注册」（会跳到 Meta AI，再回到 Persona），摄像头权限也在 Meta AI 里给，都会切 App。
- SDK 用 ExternalAccessory：Meta 文档提醒上 App Store 可能被 MFi 审核拦下；外部 TestFlight 的 Beta 审核同样有风险。
- 文档没说后台行为；官方示例在 App 进后台时结束预览会话。

参考：[iOS 集成](https://wearables.developer.meta.com/docs/develop/dat/build-integration-ios/)、
[麦克风和扬声器](https://wearables.developer.meta.com/docs/develop/dat/microphones-and-speakers/)、
[FAQ](https://developers.meta.com/wearables/faq/)、[SDK 仓库和 CHANGELOG](https://github.com/facebook/meta-wearables-dat-ios)。

## 为什么不叠在滚动录音上

滚动录音（#5，PR #2）是手表一直录、只留 6 小时、心率书签。眼镜不适合当它的音源：

- 第三方只能经 HFP 拿眼镜麦克风：做了波束成形，周围别人说话很小，录会议、录现场不如手机和手表；
  一直开着 HFP 要持续耗眼镜的电池（眼镜本身就只撑几个小时），而且蓝牙输出会从 A2DP 掉到通话音质，听歌变差。
- 不做波束成形的音频只在 DAT 的摄像头流里有，是实验功能（不能发布），还要求摄像头流一直开着，耗电发热。
- 滚动录音的价值在一直戴着、带心率的手表；眼镜没有健康数据，书签那一半用不上。
- PR #2 还没合并，叠上去要基于它的分支，两边会互相拖着。

所以这次基于 master：录音这边只改「用哪个麦克风」，这个选择放在 `RecordingCapture` 里。以后 #2 合并、如果手机也要能开滚动录音，
眼镜麦克风只是同一个输入选项，不用另做一套。

## 一、眼镜麦克风

- `RecordingInput`（`apple/Shared/Recording/RecordingInput.swift`）：`.phone` / `.bluetooth`，选择存在 `recording.input`。
- 录音页只在当前音频线路里有蓝牙设备（HFP / A2DP / LE）时才显示「麦克风」选项；没连蓝牙时界面和原来一样。
- 选了眼镜：会话用 `.playAndRecord` + `.allowBluetoothHFP`，激活后最多等 2 秒等 HFP 输入出现（Meta 文档：线路稳定后再用），
  选为首选输入；等不到就退回手机麦克风并提示，不让录音失败。录音中显示正在用的麦克风名字。
- 写入不变：分段引擎按输入格式转成 16 kHz 单声道再编码，HFP 的 8 / 16 kHz 都能接。中途眼镜摘下、折叠，线路变化走现有的
  `AVAudioEngineConfigurationChange` 处理（重开引擎，失败则暂停）。
- 不涉及 Meta 的 SDK：任何蓝牙耳机都能这样用（AirPods 也是 HFP）。

## 二、眼镜拍照（可选编译）

### 怎么打开

1. 配置 `app.meta_wearables.enabled = true`（`docs/config.md`），要正式发给别人再填 `app_id`、`client_token`。
2. 用 Xcode 26.4 以上（配置里的 `xcode`）。`persona_config.py env` 导出 `PERSONA_META_WEARABLES=YES`，
   `xcodegen` 才把 `apple/meta-wearables.yml` 并进工程：SPM 包 `MWDATCore`、`MWDATCamera`，和 SDK 要的 Info.plist 键
   （`MWDAT`、`UISupportedExternalAccessoryProtocols`、蓝牙 / 外设后台模式、蓝牙和本地网络的用途说明、`NSBonjourServices`）。
   回调用 App 已经注册的 `persona://`。
3. 不打开时：没有这个包、没有这些键，`MetaGlasses.isBuiltIn` 为 false，菜单里没有「眼镜拍照」，Xcode 26.0 照样能编。
4. `apple/Persona/Resources/Info.plist` 是 xcodegen 按 `project.yml` 生成、又提交在仓库里的：打开配置后生成的版本会多出上面的键，
   这些改动不要提交（提交的始终是关着的版本）。

### 代码

- `apple/Persona/Glasses/MetaGlasses.swift`：`#if canImport(MWDATCore) && canImport(MWDATCamera)` 才是真实现，否则是空壳。
  启动时 `Wearables.configure()`，`onOpenURL` 里先交给它（只认带 `metaWearablesAction` 的回调）。会话用 `AutoDeviceSelector`，
  预览 `.raw` / 504×896 / 24 fps，拍照用正式的 `stream.capturePhoto(format: .jpeg)`，拿不到就退回当前预览帧。关页面就停会话。
- `apple/Persona/Glasses/GlassesCaptureSheet.swift`：没注册 →「连接 Meta 眼镜」（跳 Meta AI）；没给摄像头权限 →「去允许」（同样先说明会跳走）；
  好了 → 实时画面 +「拍一张」→「用这张」/「重拍」。用这张就像相机拍的照片一样进输入框，点发送才交给 Agent。
- 只在对话页的输入框加了入口（三种输入框都加）；助手主对话的入口以后再加。

## 不做的

- 不在眼镜上显示东西（Display 眼镜的显示、Web Apps）、不接按键和「Hey Meta」：都是实验功能，不能发布。
- 不录眼镜视频、不长时间开摄像头流：耗电发热，也没有用户要。
- 原始音频、照片不自动上传：和 `docs/recording.md` 一样，用户点发送才交给 Agent。

## 要在 Mac 和真机上验证的

- Xcode 26.0 下不开配置能编（iOS、Mac、visionOS、手表；visionOS 不走蓝牙麦克风）；Xcode 26.4+ 开配置能编，`xcodegen` 合并 `meta-wearables.yml` 后
  `UIBackgroundModes` 是追加不是覆盖（看生成的 Info.plist）。
- 单元测试 `RecordingInputTests`；录音页深色 / 浅色截图（连着和没连蓝牙两种）。
- 真机：眼镜当麦克风录 5 分钟，转写质量、别人说话的音量、录音中摘下眼镜；注册 → 摄像头权限 → 拍一张 → 发给 Agent 的全流程；
  App 切后台再回来的预览。
- 开了配置的构建传外部 TestFlight 能不能过 Beta 审核（ExternalAccessory）。过不了就只给内部组，或者留在本机构建。
