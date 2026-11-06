import AppIntents
import Foundation

/// "Talk to Persona": starts the hands-free voice conversation on the CarPlay screen (or says where it is not there yet).
/// Runs in the background; the conversation itself is `VoiceConversation(surface: .carPlay)` via `VoiceCalls`.
struct TalkToPersonaIntent: AppIntent {
    nonisolated static let title: LocalizedStringResource = "Talk to Persona"
    nonisolated static let description = IntentDescription(
        "Start a continuous, hands-free voice chat with Persona on CarPlay: you talk, it answers, and you can cut in anytime.",
        categoryName: "Chat",
        searchKeywords: ["Persona", "voice", "talk", "chat", "CarPlay", "voice", "chat"]
    )
    nonisolated static var supportedModes: IntentModes { .background }

    nonisolated init() {}

    #if os(iOS)
    nonisolated func perform() async throws -> some IntentResult & ProvidesDialog {
        // Connected is not enough: the voice screen must be the one in the car while the microphone is on.
        let talk = await MainActor.run { CarPlayVoicePresence.talk }
        guard talk == .start else {
            return .result(dialog: IntentSupport.dialog(talk.sentence))
        }
        // Give Siri a moment to finish its own sentence before the microphone opens; the user may leave meanwhile.
        Task { @MainActor in
            try? await Task.sleep(for: .seconds(1.5))
            guard CarPlayVoicePresence.talk == .start else { return }
            _ = await CarPlayVoiceSession.start()
        }
        return .result(dialog: IntentSupport.dialog(talk.sentence))
    }
    #else
    // macOS has no CarPlay: the same sentence as the unconnected case.
    nonisolated func perform() async throws -> some IntentResult & ProvidesDialog {
        .result(dialog: IntentSupport.dialog(CarPlayTalk.notConnected.sentence))
    }
    #endif
}
