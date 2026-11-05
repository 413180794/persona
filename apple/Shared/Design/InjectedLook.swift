import SwiftUI

/// How a message Persona typed in for the model (`Injected`) is marked, the same on the phone, the Mac and the watch:
/// a task done green, waiting for the user in the accent, cut off or not started red; a scheduled run or a wake-up in
/// the accent; anything else (a node restart) quiet.
nonisolated enum InjectedLook {
    static func icon(_ n: Injected) -> String {
        switch n.kind {
        case "task_notice":
            switch n.state {
            case "needs": Theme.Icon.waitsForYou
            case "interrupted", "failed": Theme.Icon.errorFilled
            default: Theme.Icon.successFilled
            }
        case "followup": n.state == "wake" ? Theme.Icon.wakeUp : Theme.Icon.schedule
        default: Theme.Icon.infoFilled
        }
    }

    @MainActor static func tint(_ n: Injected) -> Color {
        switch n.kind {
        case "task_notice": n.state == "interrupted" || n.state == "failed" ? Theme.danger : n.state == "needs" ? Theme.accent : Theme.positive
        case "followup": Theme.accent
        default: .secondary
        }
    }
}
