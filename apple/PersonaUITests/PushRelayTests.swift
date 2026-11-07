import XCTest

/// Setup for scripts/test/relay_sim_e2e.sh (docs/operations/push-relay.md): lets the app notify (taps the system's Allow) and has it
/// export its push public key (`-showPushKey`), then leaves it in the background, where the script's pushes arrive.
/// Not paired with any hub: nothing else happens.
final class PushRelayTests: XCTestCase {
    @MainActor func testAllowNotificationsAndExportKey() throws {
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        let app = XCUIApplication()
        app.launchArguments = ["-showPushKey", "YES", "-ui.style", UITestStyle.name]
        app.launch()
        let allow = springboard.alerts.buttons.matching(NSPredicate(format: "label IN %@", ["允许", "Allow"])).firstMatch
        if allow.waitForExistence(timeout: 12) { allow.tap() }
        sleep(2)
        XCUIDevice.shared.press(.home)
    }
}
