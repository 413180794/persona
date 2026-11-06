import AppIntents
import Foundation

/// "Approve First Pending": settles the oldest interaction when it is a permission or a plan. Questions and terminal
/// prompts need the screen, so those answer with a hint to open the app.
struct ApproveFirstIntent: AppIntent {
    nonisolated static let title: LocalizedStringResource = "批准第一个待处理"
    nonisolated static let description = IntentDescription(
        "允许最早的那条工具授权，或者批准一个计划（批准后自动执行）。提问和终端需要打开 App。",
        categoryName: "状态",
        searchKeywords: ["批准", "允许", "授权", "计划"]
    )
    nonisolated static var supportedModes: IntentModes { .background }

    nonisolated init() {}

    nonisolated func perform() async throws -> some IntentResult & ProvidesDialog & ReturnsValue<String> {
        do {
            // The oldest of every group's, settled at its own group's hub.
            guard let first = IntentPicker.pending(try await IntentSupport.interactions()) else {
                throw IntentFailure.noPending
            }
            guard let resolution = Self.resolution(for: first) else {
                let hint = IntentText.needsApp(first.kind)
                return .result(value: hint, dialog: IntentSupport.dialog(hint))
            }
            let api = try IntentSupport.client(group: first.group)
            let sessions = try await api.sessions()
            let session = sessions.first { $0.key == first.sessionKey }
            guard session?.canApprove == true else {
                let hint = String(localized: "此会话不允许审批，请打开 App 查看")
                return .result(value: hint, dialog: IntentSupport.dialog(hint))
            }
            try await api.resolve(first.id, resolution)
            let text = Self.doneText(for: first)
            return .result(value: text, dialog: IntentSupport.dialog(text))
        } catch {
            throw IntentFailureSentence(IntentFailure.sentence(for: error, host: IntentSupport.host()))
        }
    }

    /// What to send, or nil when this kind cannot be settled from Siri.
    static func resolution(for ix: Interaction) -> Resolution? {
        guard !ix.requiresInput else { return nil }
        return switch ix.kind {
        case .permission: Resolution(allow: true)
        case .plan: Resolution(approve: true, mode: "bypassPermissions")
        case .question, .screen: nil
        }
    }

    static func doneText(for ix: Interaction) -> String {
        switch ix.kind {
        case .permission: IntentText.approvedPermission(ix.toolName)
        case .plan: IntentText.approvedPlan
        case .question, .screen: IntentText.needsApp(ix.kind)
        }
    }
}
