import Foundation
import UserNotifications

/// What the activity's buttons do (FleetIntents.swift), in the app process: resolve through the hub, take the request
/// off the activity at once, and say so in a notification when it did not work (nobody is looking at the app).
nonisolated enum FleetActions {
    enum Action { case allow, deny, approvePlan }

    static func resolution(_ action: Action) -> Resolution {
        switch action {
        case .allow: NotificationActions.resolution(action: NotificationActions.allow)!
        case .deny: NotificationActions.resolution(action: NotificationActions.deny)!
        case .approvePlan: NotificationActions.resolution(action: NotificationActions.approvePlan)!
        }
    }

    static func run(_ action: Action, _ interactionID: String) async {
        do {
            guard let config = Keychain.serverConfig else { throw IntentFailure.notPaired }
            try await APIClient(config: config).resolve(interactionID, resolution(action))
            await LiveActivityController.shared.resolvedLocally(interactionID)
        } catch {
            if (error as? APIError)?.status == 410 { // already settled elsewhere: just drop it from the activity
                await LiveActivityController.shared.resolvedLocally(interactionID)
            }
            let title = switch action {
            case .allow: NotificationActions.failureTitle(action: NotificationActions.allow)
            case .deny: NotificationActions.failureTitle(action: NotificationActions.deny)
            case .approvePlan: NotificationActions.failureTitle(action: NotificationActions.approvePlan)
            }
            await NotificationActions.reportFailure(id: interactionID, title: title, error: error)
        }
    }
}
