import AppIntents
import Foundation
import OSLog

// The buttons on the Fleet activity (docs/live-activity.md). LiveActivityIntent runs `perform` in the app's process
// (launched in the background if needed), so the extension only needs the types to draw `Button(intent:)`; the bodies
// that talk to the hub are app-only. Saying yes needs the phone unlocked, saying no does not, as on notifications.

struct AllowPermissionIntent: LiveActivityIntent {
    nonisolated static let title: LocalizedStringResource = "允许"
    nonisolated static let isDiscoverable = false
    nonisolated static var authenticationPolicy: IntentAuthenticationPolicy { .requiresAuthentication }

    @Parameter(title: "交互") var interactionID: String?

    nonisolated init() {}
    nonisolated init(interactionID: String?) { self.interactionID = interactionID }

    nonisolated func perform() async throws -> some IntentResult {
        #if !WIDGET_EXTENSION
        guard let interactionID, !interactionID.isEmpty else {
            Logger(subsystem: "jarvis", category: "live-activity").warning("AllowPermissionIntent performed without interactionID")
            return .result()
        }
        await FleetActions.run(.allow, interactionID)
        #endif
        return .result()
    }
}

struct DenyPermissionIntent: LiveActivityIntent {
    nonisolated static let title: LocalizedStringResource = "拒绝"
    nonisolated static let isDiscoverable = false
    nonisolated static var authenticationPolicy: IntentAuthenticationPolicy { .alwaysAllowed }

    @Parameter(title: "交互") var interactionID: String?

    nonisolated init() {}
    nonisolated init(interactionID: String?) { self.interactionID = interactionID }

    nonisolated func perform() async throws -> some IntentResult {
        #if !WIDGET_EXTENSION
        guard let interactionID, !interactionID.isEmpty else {
            Logger(subsystem: "jarvis", category: "live-activity").warning("DenyPermissionIntent performed without interactionID")
            return .result()
        }
        await FleetActions.run(.deny, interactionID)
        #endif
        return .result()
    }
}

/// Approves a plan in acceptEdits, like the notification button: from the lock screen you have not read the plan in
/// full, so commands still ask. "批准并自动执行" stays in the app.
struct ApprovePlanIntent: LiveActivityIntent {
    nonisolated static let title: LocalizedStringResource = "批准"
    nonisolated static let isDiscoverable = false
    nonisolated static var authenticationPolicy: IntentAuthenticationPolicy { .requiresAuthentication }

    @Parameter(title: "交互") var interactionID: String?

    nonisolated init() {}
    nonisolated init(interactionID: String?) { self.interactionID = interactionID }

    nonisolated func perform() async throws -> some IntentResult {
        #if !WIDGET_EXTENSION
        guard let interactionID, !interactionID.isEmpty else {
            Logger(subsystem: "jarvis", category: "live-activity").warning("ApprovePlanIntent performed without interactionID")
            return .result()
        }
        await FleetActions.run(.approvePlan, interactionID)
        #endif
        return .result()
    }
}
