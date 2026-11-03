import AppIntents
import Foundation

/// "Jarvis 状态": how many sessions are working and how many are waiting, plus the first question.
// Compatibility: the system addresses this type by its original module/type identity.
struct JarvisStatusIntent: AppIntent {
    nonisolated static let title: LocalizedStringResource = "Jarvis 状态"
    nonisolated static let description = IntentDescription(
        "有几个 Claude 在工作、几个在等你处理，以及第一个问题是什么。",
        categoryName: "状态",
        searchKeywords: ["状态", "进度", "待处理"]
    )
    nonisolated static var supportedModes: IntentModes { .background }

    nonisolated init() {}

    nonisolated func perform() async throws -> some IntentResult & ProvidesDialog & ReturnsValue<String> {
        do {
            // Every group added up (docs/multi-hub.md「Siri」).
            async let sessionsRequest = IntentSupport.sessions()
            async let interactionsRequest = IntentSupport.interactions()
            let sessions = try await sessionsRequest
            let interactions = try await interactionsRequest
            let text = IntentText.status(
                working: sessions.filter { $0.status == .working }.count,
                waiting: interactions.count,
                first: IntentPicker.pending(interactions).map(IntentText.waitingPrompt)
            )
            return .result(value: text, dialog: IntentSupport.dialog(text))
        } catch {
            throw IntentFailureSentence(IntentFailure.sentence(for: error, host: IntentSupport.host()))
        }
    }
}
