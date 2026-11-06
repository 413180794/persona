import AppIntents
import Foundation
import Testing
@testable import Persona

struct FleetIntentsTests {
    /// Same rule as the notification buttons: yes needs Face ID, no works locked.
    @Test func sayingYesNeedsUnlock() {
        #expect(AllowPermissionIntent.authenticationPolicy == .requiresAuthentication)
        #expect(ApprovePlanIntent.authenticationPolicy == .requiresAuthentication)
        #expect(DenyPermissionIntent.authenticationPolicy == .alwaysAllowed)
    }

    @Test func resolutionsMatchTheNotificationButtons() {
        #expect(FleetActions.resolution(.allow).allow == true)
        #expect(FleetActions.resolution(.deny).allow == false)
        let plan = FleetActions.resolution(.approvePlan)
        #expect(plan.approve == true)
        #expect(plan.mode == "acceptEdits") // never auto-run everything from the lock screen
    }

    /// Kept out of Shortcuts: they only make sense with an interaction id from the activity.
    @Test func notInShortcuts() {
        #expect(AllowPermissionIntent.isDiscoverable == false)
        #expect(DenyPermissionIntent.isDiscoverable == false)
        #expect(ApprovePlanIntent.isDiscoverable == false)
    }

    /// Background intent execution must not crash when interactionID parameter fails to restore.
    @Test func missingInteractionDoesNotCrash() async throws {
        var allow = AllowPermissionIntent()
        #expect(allow.interactionID == nil)
        _ = try await allow.perform()

        var deny = DenyPermissionIntent()
        #expect(deny.interactionID == nil)
        _ = try await deny.perform()

        var plan = ApprovePlanIntent()
        #expect(plan.interactionID == nil)
        _ = try await plan.perform()
    }

    @Test func explicitInteractionIsPreserved() {
        let allow = AllowPermissionIntent(interactionID: "ix_123")
        #expect(allow.interactionID == "ix_123")

        let deny = DenyPermissionIntent(interactionID: "ix_456")
        #expect(deny.interactionID == "ix_456")

        let plan = ApprovePlanIntent(interactionID: "ix_789")
        #expect(plan.interactionID == "ix_789")
    }
}
