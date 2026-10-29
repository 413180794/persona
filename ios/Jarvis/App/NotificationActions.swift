import Foundation
import UserNotifications

/// Buttons on interaction pushes. The hub sends `category: INTERACTION_<KIND>` (server/internal/hub/push.go); these
/// are the categories it names. Anything that says yes to the agent needs the phone unlocked; saying no does not.
nonisolated enum NotificationActions {
    static let allow = "IX_ALLOW"
    static let deny = "IX_DENY"
    static let approvePlan = "IX_APPROVE_PLAN"
    static let rejectPlan = "IX_REJECT_PLAN"

    static func category(_ kind: InteractionKind) -> String { "INTERACTION_" + kind.rawValue.uppercased() }

    static var categories: Set<UNNotificationCategory> {
        let permission = UNNotificationCategory(
            identifier: category(.permission),
            actions: [
                UNNotificationAction(identifier: allow, title: "允许", options: [.authenticationRequired],
                                     icon: UNNotificationActionIcon(systemImageName: "checkmark")),
                UNNotificationAction(identifier: deny, title: "拒绝", options: [.destructive],
                                     icon: UNNotificationActionIcon(systemImageName: "xmark")),
            ],
            intentIdentifiers: [], options: [])
        let plan = UNNotificationCategory(
            identifier: category(.plan),
            actions: [
                UNNotificationAction(identifier: approvePlan, title: "批准", options: [.authenticationRequired],
                                     icon: UNNotificationActionIcon(systemImageName: "checkmark")),
                UNTextInputNotificationAction(identifier: rejectPlan, title: "打回修改", options: [.authenticationRequired],
                                              icon: UNNotificationActionIcon(systemImageName: "arrow.uturn.backward"),
                                              textInputButtonTitle: "打回", textInputPlaceholder: "告诉 Claude 要怎么改…"),
            ],
            intentIdentifiers: [], options: [])
        // Questions and terminal prompts need the screen: no buttons, a tap opens the session. Registered anyway so
        // the system knows the identifiers the hub sends.
        let question = UNNotificationCategory(identifier: category(.question), actions: [], intentIdentifiers: [], options: [])
        let screen = UNNotificationCategory(identifier: category(.screen), actions: [], intentIdentifiers: [], options: [])
        return [permission, plan, question, screen]
    }

    /// What a button sends to the hub, or nil for a plain tap (and anything we don't know).
    /// Plans approve in acceptEdits: from a notification you have not read the plan in full, so edits go through but
    /// commands still ask. "批准并自动执行" stays an in-app choice.
    static func resolution(action: String, text: String? = nil) -> Resolution? {
        switch action {
        case allow: Resolution(allow: true)
        case deny: Resolution(allow: false)
        case approvePlan: Resolution(approve: true, mode: "acceptEdits")
        case rejectPlan: Resolution(approve: false, feedback: text?.trimmingCharacters(in: .whitespacesAndNewlines) ?? "")
        default: nil
        }
    }

    /// Shown (as a local notification) when a button could not be carried out: the phone was locked in a drawer
    /// while the agent moved on, the hub was unreachable, and so on.
    static func failureTitle(action: String) -> String {
        switch action {
        case allow: "没能允许"
        case deny: "没能拒绝"
        case approvePlan: "没能批准计划"
        case rejectPlan: "没能打回计划"
        default: "没能完成操作"
        }
    }
}
