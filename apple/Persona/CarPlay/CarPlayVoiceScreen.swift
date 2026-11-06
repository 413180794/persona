import Foundation

/// The buttons a CarPlay voice state can show. The template takes at most `CPVoiceControlState.maximumActionButtonCount`.
nonisolated enum CarPlayVoiceButton: String, Equatable, Sendable {
    case end, interrupt, resume
}

/// One state of the CarPlay voice control template, as plain data (the CarPlay delegate builds the real states from it).
nonisolated struct CarPlayVoiceStateSpec: Equatable, Sendable {
    var id: String
    /// Longest first: CarPlay picks the variant that fits the screen.
    var titles: [String]
    /// A `Theme.Icon` value.
    var icon: String
    var repeats: Bool
    var buttons: [CarPlayVoiceButton]
}

/// When the CarPlay screen starts the conversation by itself: once after it connects and once each time it comes back to
/// the foreground. Becoming active alone does not count: a notification banner, Siri or a call ending makes the scene
/// resign and become active again, and that must not reopen the microphone after the user pressed 结束 or the
/// conversation ended by itself (idle, no permission, no voice plugin). Then only 继续 or Siri start it again.
nonisolated struct CarPlayAutoStart: Equatable, Sendable {
    private(set) var armed = false

    /// The scene connected or is entering the foreground.
    mutating func arm() { armed = true }

    /// The scene became active: true at most once per `arm()`.
    mutating func takeOnBecomingActive() -> Bool {
        defer { armed = false }
        return armed
    }
}

/// What "跟 Persona 聊聊" (TalkToPersonaIntent) may do. Apple: the voice screen must be on screen while voice is
/// active, so CarPlay being connected is not enough; Persona has to be the app showing in the car.
nonisolated enum CarPlayTalk: Equatable, Sendable {
    case start
    case notConnected
    case notOnScreen

    static func decide(connected: Bool, foreground: Bool) -> CarPlayTalk {
        guard connected else { return .notConnected }
        return foreground ? .start : .notOnScreen
    }

    /// What Siri says back.
    var sentence: String {
        switch self {
        case .start: "好，在 CarPlay 上接着聊"
        case .notConnected: "连上 CarPlay 后打开 Persona 就能一直聊。"
        case .notOnScreen: "先在车里的屏幕上打开 Persona，就能接着聊"
        }
    }
}

/// Whether CarPlay is connected and showing Persona's voice screen right now. The CarPlay scene delegate writes it, the
/// Siri intent (TalkToPersonaIntent) reads it.
@MainActor enum CarPlayVoicePresence {
    static var isConnected = false
    /// The CarPlay scene is in the foreground (between entering the foreground and entering the background).
    static var isForeground = false

    static var talk: CarPlayTalk { CarPlayTalk.decide(connected: isConnected, foreground: isForeground) }
}

/// The CarPlay voice screen as data: its states, which state a phase shows, and the wording of its buttons.
/// Pure (no CarPlay import), so it is testable on any platform.
nonisolated enum CarPlayVoiceScreen {
    /// The ended state's second line when the template shows no buttons (no "继续" to tap): Siri starts it again.
    static let resumeBySiri = "说「跟 Persona 聊聊」再聊"

    /// The template's states. `endedReason` is the sentence shown in the ended state; an empty or missing one falls back
    /// to the default. `buttonsShown` is false where the template cannot show buttons; the ended state then points at Siri.
    /// `initial` goes first: a new template shows its first state, and CarPlay ignores a state activated before the
    /// template is on screen or too soon after another, so the state meant to be seen must already be the default.
    static func states(endedReason: String?, initial: String? = nil, buttonsShown: Bool = true) -> [CarPlayVoiceStateSpec] {
        var all = baseStates(endedReason: endedReason, buttonsShown: buttonsShown)
        if let initial, let index = all.firstIndex(where: { $0.id == initial }) {
            all.insert(all.remove(at: index), at: 0)
        }
        return all
    }

    private static func baseStates(endedReason: String?, buttonsShown: Bool) -> [CarPlayVoiceStateSpec] {
        let reason = endedReason?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        let endedHeadline = reason.isEmpty ? "已结束" : reason
        let endedHint = buttonsShown ? "轻点「继续」再聊" : resumeBySiri
        return [
            CarPlayVoiceStateSpec(id: "connecting", titles: ["正在连接 Persona", "连接中"], icon: Theme.Icon.voiceConnecting,
                                  repeats: true, buttons: [.end]),
            CarPlayVoiceStateSpec(id: "listening", titles: ["我在听，直接说", "我在听"], icon: Theme.Icon.voiceListening,
                                  repeats: true, buttons: [.end]),
            CarPlayVoiceStateSpec(id: "speaking", titles: ["Persona 在说，可以随时插话", "Persona 在说"],
                                  icon: Theme.Icon.voiceSpeaking, repeats: true, buttons: [.interrupt, .end]),
            CarPlayVoiceStateSpec(id: "working", titles: ["Persona 在处理", "处理中"], icon: Theme.Icon.voiceWorking,
                                  repeats: true, buttons: [.end]),
            CarPlayVoiceStateSpec(id: "ended", titles: [endedHeadline, endedHint], icon: Theme.Icon.voiceEnded,
                                  repeats: false, buttons: [.resume]),
        ]
    }

    #if os(iOS)
    /// Which state id a `VoiceConversation` shows. No call yet is the ended state.
    /// `working` when the bound conversation is busy or a tool/ask is open; else the live audio state.
    static func stateID(for phase: VoicePhase?, tools: Int = 0, asking: Bool = false, targetBusy: Bool = false,
                        link: VoiceLink = .live) -> String {
        guard let phase else { return "ended" }
        switch phase {
        case .connecting, .reconnecting:
            return "connecting"
        case .live(let live):
            if link == .paused || targetBusy || tools > 0 || asking { return "working" }
            switch live {
            case .speaking: return "speaking"
            case .listening, .hearing: return "listening"
            }
        case .ended:
            return "ended"
        }
    }

    /// The state a template built on connecting opens on: connecting when the conversation is about to start by itself
    /// (there is no call yet, and would otherwise open on 已结束 for a moment), else the phase's own state.
    static func openingStateID(for phase: VoicePhase?, startingNow: Bool, tools: Int = 0, asking: Bool = false,
                               targetBusy: Bool = false, link: VoiceLink = .live) -> String {
        let active: Bool = {
            guard let phase else { return false }
            return !phase.isEnded
        }()
        if startingNow, !active { return "connecting" }
        return stateID(for: phase, tools: tools, asking: asking, targetBusy: targetBusy, link: link)
    }

    /// The ended state's headline for a `VoiceEnd` (I3 words live on the phone page; CarPlay needs a short line).
    static func endedHeadline(for end: VoiceEnd) -> String {
        switch end {
        case .user: "已结束。轻点「继续」再聊"
        case .left: "Persona 不在屏幕上时不听。回到 Persona 会接着聊"
        case .background: "已结束后台"
        case .replaced: "已切换到别的语音对话"
        case .interrupted: "来电或别的 App 占用了音频，对话已停"
        case .microphoneTaken: "麦克风正在录音或听写，先结束它再聊"
        case .microphoneBusy: "麦克风正在录音或听写，先结束它再聊"
        case .permissionDenied: CarPlayMicGate.denied
        case .audioFailed: "打不开麦克风，对话已停"
        case .audioReset: "音频服务重置，对话已停"
        case .idle: "好久没说话，对话已停"
        case .lost: "连不上 hub，对话已停"
        case .rejected(let status): "hub 拒绝了连接（\(status)）"
        case .incompatible: "语音扩展协议不匹配，请升级 hub 的语音扩展"
        case .modelUnavailable(let why):
            why.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? "这个语音模型现在不可用" : why
        case .noHub: CarPlayMicGate.noHub
        case .failed(_, let message):
            message.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
                ? "语音出错，对话已停"
                : message.trimmingCharacters(in: .whitespacesAndNewlines)
        }
    }
    #endif

    /// The button's label on the voice screen.
    static func title(_ button: CarPlayVoiceButton) -> String {
        switch button {
        case .end: "结束"
        case .interrupt: "打断"
        case .resume: "继续"
        }
    }

    /// The button's image: a `Theme.Icon` value.
    static func icon(_ button: CarPlayVoiceButton) -> String {
        switch button {
        case .end: Theme.Icon.end
        case .interrupt: Theme.Icon.voiceInterruptSpeech
        case .resume: Theme.Icon.voiceResume
        }
    }
}
