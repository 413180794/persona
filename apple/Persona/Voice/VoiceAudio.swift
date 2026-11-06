import Foundation
import Observation
#if os(iOS) || os(visionOS)
import AVFoundation
import UIKit
#endif

/// The rates of a call's audio: the microphone's PCM16 goes up at `inputRate`, the voice comes down at `outputRate`
/// (the `ready` frame's `input_rate` / `output_rate`).
nonisolated struct VoiceAudioFormat: Equatable, Hashable, Sendable {
    var inputRate: Int
    var outputRate: Int

    /// What the microphone records at before `ready` says (the extension's defaults); a call remembers the last one.
    static let provisional = VoiceAudioFormat(inputRate: VoiceFrame.defaultInputRate, outputRate: VoiceFrame.defaultOutputRate)
}

/// What the audio tells the call.
nonisolated enum VoiceAudioEvent: Equatable, Sendable {
    /// The voice started (true) or finished (false) playing what it was given.
    case playing(Bool)
    /// The voice's loudness right now, 0…1, for the orb.
    case outputLevel(Float)
    /// A phone call, or another app, took the audio session (docs/voice.md 6.5「来电」).
    case interrupted
    /// The system's media services restarted: every audio object is gone.
    case reset
    /// The route changed (headphones, Bluetooth) and the microphone could not be set up again on the new one.
    case failed
}

nonisolated enum VoiceAudioError: Error, Equatable {
    case unsupported
    case session
    case format
    case engine
}

/// The call's audio: the microphone into the uplink, the voice out of the speaker, `clear` to drop what has not been
/// played (docs/voice.md 6.4). `PhoneVoiceAudio` on iOS; the tests' fake elsewhere.
@MainActor protocol VoiceAudio: AnyObject {
    /// Opens the microphone (PCM16 at `format.inputRate` into `uplink`) and the player (`format.outputRate`).
    func start(_ format: VoiceAudioFormat, uplink: VoiceUplink, events: @escaping @MainActor (VoiceAudioEvent) -> Void) throws
    /// The extension said other rates than the ones it started with.
    func reconfigure(_ format: VoiceAudioFormat) throws
    /// The voice: PCM16 at the output rate.
    func play(_ pcm: Data)
    /// A short sound of the app's own (PCM16 at the output rate) through the same player, so echo cancellation takes it
    /// out of the microphone too; it does not count as the voice speaking.
    func cue(_ pcm: Data)
    /// Drops everything queued and not yet played.
    func clear()
    func stop()
}

/// The microphone and the voice's loudness, about 20 times a second: an object of its own, so only the orb redraws.
@MainActor @Observable final class VoiceLevels {
    private(set) var input: Float = 0
    private(set) var output: Float = 0

    /// Changes smaller than this are not worth a redraw.
    static let step: Float = 0.01

    func set(input value: Float) {
        if abs(value - input) >= Self.step || (value == 0 && input != 0) { input = value }
    }

    func set(output value: Float) {
        if abs(value - output) >= Self.step || (value == 0 && output != 0) { output = value }
    }

    func reset() {
        set(input: 0)
        set(output: 0)
    }
}

/// Asks for the microphone (the system's prompt the first time).
@MainActor protocol MicPermission {
    func request() async -> Bool
}

/// Keeps the screen from locking while a call waits on a long task (docs/voice.md 6.5).
@MainActor protocol IdleTimerControl: AnyObject {
    var isDisabled: Bool { get set }
}

/// Time for the call's timers: the system's, or the tests' that they move on by hand.
nonisolated protocol VoiceClock: Sendable {
    var now: Date { get }
    func sleep(for duration: Duration) async throws
}

nonisolated struct SystemVoiceClock: VoiceClock {
    var now: Date { .now }
    func sleep(for duration: Duration) async throws { try await Task.sleep(for: duration) }
}

struct SystemMicPermission: MicPermission {
    func request() async -> Bool {
        #if os(iOS) || os(visionOS)
        await AVAudioApplication.requestRecordPermission()
        #else
        false
        #endif
    }
}

final class SystemIdleTimer: IdleTimerControl {
    var isDisabled: Bool {
        get {
            #if os(iOS) || os(visionOS)
            UIApplication.shared.isIdleTimerDisabled
            #else
            false
            #endif
        }
        set {
            #if os(iOS) || os(visionOS)
            UIApplication.shared.isIdleTimerDisabled = newValue
            #endif
        }
    }
}

/// Where there is no call audio (the Mac and visionOS for now, docs/voice.md 6.9): starting fails.
final class NoVoiceAudio: VoiceAudio {
    func start(_ format: VoiceAudioFormat, uplink: VoiceUplink, events: @escaping @MainActor (VoiceAudioEvent) -> Void) throws {
        throw VoiceAudioError.unsupported
    }
    func reconfigure(_ format: VoiceAudioFormat) throws { throw VoiceAudioError.unsupported }
    func play(_ pcm: Data) {}
    func cue(_ pcm: Data) {}
    func clear() {}
    func stop() {}
}
