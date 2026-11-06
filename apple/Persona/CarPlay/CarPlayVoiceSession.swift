#if os(iOS)
import AVFoundation
import Foundation

/// Why CarPlay will not open the microphone prompt on the phone (in a pocket while driving).
nonisolated enum CarPlayMicGate {
    static let undecided = "先在手机上的 Persona 里用一次听写或录音，允许使用麦克风"
    static let denied = "没有麦克风权限，到 设置 › Persona 打开"
    static let noHub = "还没配对 hub，先在手机上打开 Persona"
    static let noMain = "先在手机上打开 Persona，等主对话建好再聊"
    static let noExtension = "hub 上还没有在运行的语音扩展，先在 hub 配好双工语音入口"

    /// Why a call cannot start, or nil when it can. Undetermined ends it too: nobody can answer the phone's prompt from the car.
    static func blockReason() -> String? {
        switch AVAudioApplication.shared.recordPermission {
        case .granted: nil
        case .denied: denied
        case .undetermined: undecided
        @unknown default: undecided
        }
    }
}

/// Mic permission that never prompts: CarPlay must not hang on the phone's dialog (docs/carplay.md).
struct CarPlayMicPermission: MicPermission {
    func request() async -> Bool {
        AVAudioApplication.shared.recordPermission == .granted
    }
}

/// CarPlay's thin shell over `VoiceCalls` / `VoiceConversation(surface: .carPlay)` (docs/voice.md 6.4, #3789).
@MainActor enum CarPlayVoiceSession {
    /// Starts (or restarts) the car's call on the assistant's main conversation. Returns a Chinese sentence when it
    /// cannot start; nil once `VoiceCalls` owns the call (it may still end itself with `VoiceEnd`).
    static func start() async -> String? {
        if let reason = CarPlayMicGate.blockReason() { return reason }
        do {
            let (key, extensionID) = try await resolve()
            await VoiceCalls.shared.start(key, extensionID: extensionID, surface: .carPlay, deps: deps(group: key.group))
            if let call = VoiceCalls.shared.current, call.surface == .carPlay, case .ended(let end) = call.phase {
                return CarPlayVoiceScreen.endedHeadline(for: end)
            }
            return nil
        } catch let error as CarPlayVoiceStartError {
            return error.sentence
        } catch {
            return IntentFailure.sentence(for: error, host: IntentSupport.host())
        }
    }

    /// Continue on an ended car call, or a fresh start when there is none.
    static func resume() async -> String? {
        if let call = VoiceCalls.shared.current, call.surface == .carPlay, call.phase.isEnded {
            await VoiceCalls.shared.resume(call.key)
            if let call = VoiceCalls.shared.current, call.surface == .carPlay, case .ended(let end) = call.phase {
                return CarPlayVoiceScreen.endedHeadline(for: end)
            }
            return nil
        }
        return await start()
    }

    static func endFromUser() {
        VoiceCalls.shared.end(.user, from: .carPlay)
    }

    /// Leaving the CarPlay voice screen (Apple: voice only while the template is on screen).
    static func endFromBackground() {
        VoiceCalls.shared.end(.left, from: .carPlay)
    }

    static func interrupt() {
        guard let call = VoiceCalls.shared.current, call.surface == .carPlay else { return }
        call.interruptSpeech()
    }

    /// A live car call (not ended). Used so auto-start does not reopen after End.
    static var isRunning: Bool {
        guard let call = VoiceCalls.shared.current, call.surface == .carPlay else { return false }
        return !call.phase.isEnded
    }

    /// The car call to mirror on the template, if any (live or ended).
    static var call: VoiceConversation? {
        guard let call = VoiceCalls.shared.current, call.surface == .carPlay else { return nil }
        return call
    }

    private static func deps(group: String) -> VoiceDeps {
        var d = VoiceDeps.live(group: group)
        d.mic = CarPlayMicPermission()
        return d
    }

    private static func resolve() async throws -> (SessionKey, String) {
        let api: APIClient
        let key: SessionKey
        if let existing = AppModel.shared.assistantCurrent ?? AppModel.shared.assistantMain,
           let client = AppModel.shared.api(existing.group) {
            api = client
            key = existing
        } else {
            do {
                api = try IntentSupport.client()
            } catch {
                throw CarPlayVoiceStartError.noHub
            }
            let st = try await api.assistant()
            guard let main = st.main, !main.isEmpty else { throw CarPlayVoiceStartError.noMain }
            key = SessionKey(group: api.group, machineID: st.machineID, sessionID: main)
        }
        let list = try await api.pluginList()
        guard let voice = VoiceAvailability.pick(list.plugins) else { throw CarPlayVoiceStartError.noExtension }
        return (key, voice.id)
    }
}

nonisolated enum CarPlayVoiceStartError: Error {
    case noHub, noMain, noExtension

    var sentence: String {
        switch self {
        case .noHub: CarPlayMicGate.noHub
        case .noMain: CarPlayMicGate.noMain
        case .noExtension: CarPlayMicGate.noExtension
        }
    }
}
#endif
