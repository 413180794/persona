import XCTest

/// Interaction pushes carry buttons (NotificationActions): the app posts a look-alike local notification
/// (`-showTestNotification`), the test expands it on SpringBoard and taps 「拒绝」, and the mock hub
/// (scripts/mock/mock_hub.py, PERSONA_MOCK_HUB=http://127.0.0.1:<port>) must have received the resolve.
final class NotificationActionTests: XCTestCase {
    @MainActor func testDenyFromTheNotification() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)")
        }
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let app = XCUIApplication()
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-showTestNotification", "permission"]
        app.launchPinned()

        // The system's permission alert (worded in the simulator's language).
        let allowAlert = springboard.alerts.buttons.matching(NSPredicate(format: "label IN %@", ["允许", "Allow"])).firstMatch
        if allowAlert.waitForExistence(timeout: 12) { allowAlert.tap() }
        sleep(1)
        XCUIDevice.shared.press(.home) // the push arrives while the app is in the background

        let banner = springboard.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", "需要你授权")).firstMatch
        XCTAssertTrue(banner.waitForExistence(timeout: 15), "no notification banner:\n\(springboard.debugDescription)")
        banner.press(forDuration: 1.5)

        let deny = springboard.buttons["拒绝"]
        XCTAssertTrue(deny.waitForExistence(timeout: 5), "the expanded notification has no 拒绝 button")
        XCTAssertTrue(springboard.buttons["允许"].exists, "the expanded notification has no 允许 button")
        let shot = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
        shot.name = "notification-actions"
        shot.lifetime = .keepAlways
        add(shot)
        deny.tap()

        let deadline = Date.now.addingTimeInterval(10)
        var resolved: [[String: Any]] = []
        while Date.now < deadline {
            if let data = try? Data(contentsOf: URL(string: hub + "/mock/resolved")!),
               let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
               let list = obj["resolved"] as? [[String: Any]], !list.isEmpty {
                resolved = list
                break
            }
            usleep(300_000)
        }
        let last = try XCTUnwrap(resolved.last, "the hub never got the resolve")
        XCTAssertEqual(last["id"] as? String, "ix_test_permission")
        XCTAssertEqual((last["resolution"] as? [String: Any])?["allow"] as? Bool, false)
    }
}
