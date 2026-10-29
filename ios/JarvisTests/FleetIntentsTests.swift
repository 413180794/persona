import AppIntents
import Foundation
import Testing
@testable import Jarvis

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
}
