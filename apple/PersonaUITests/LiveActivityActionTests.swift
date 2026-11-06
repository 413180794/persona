import XCTest

/// The Fleet activity's Deny button on the lock screen resolves the request: the app starts a sample activity
/// (`-showFleetSample needsYou`, request `ix_sample`), the test locks the phone, taps Deny and asks the mock hub
/// (scripts/mock/mock_hub.py, PERSONA_MOCK_HUB) whether the resolve arrived.
final class LiveActivityActionTests: XCTestCase {
    @MainActor func testDenyFromTheLockScreen() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)")
        }
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "settings", "-showFleetSample", "needsYou"]
        app.launchPinned()
        sleep(4)
        XCUIDevice.shared.press(.home)
        sleep(1)
        XCUIDevice.shared.perform(NSSelectorFromString("pressLockButton"))
        sleep(2)
        XCUIDevice.shared.perform(NSSelectorFromString("pressLockButton"))
        sleep(2)
        let deny = springboard.buttons.matching(NSPredicate(format: "label CONTAINS %@", "拒绝")).firstMatch
        // The system's "allow Live Activities" prompt, first time only. Its Allow is also the activity's own button: with
        // the activity showing (Deny is there), tapping Allow would approve the sample request.
        let allowActivities = springboard.buttons.matching(NSPredicate(format: "label IN %@", ["Allow", "Always Allow", "允许"])).firstMatch
        if !deny.exists, allowActivities.exists { allowActivities.tap(); sleep(1) }

        defer { unlock(springboard) }
        XCTAssertTrue(deny.waitForExistence(timeout: 10), "no 拒绝 on the lock screen activity:\n\(springboard.debugDescription)")
        XCTAssertTrue(springboard.buttons.matching(NSPredicate(format: "label CONTAINS %@", "允许")).firstMatch.exists)
        let shot = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
        shot.name = "lock-actions"
        shot.lifetime = .keepAlways
        add(shot)
        deny.tap()

        let deadline = Date.now.addingTimeInterval(15)
        var last: [String: Any]?
        while Date.now < deadline, last == nil {
            if let data = try? Data(contentsOf: URL(string: hub + "/mock/resolved")!),
               let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
               let list = obj["resolved"] as? [[String: Any]] {
                last = list.last { $0["id"] as? String == "ix_sample" }
            }
            if last == nil { usleep(300_000) }
        }
        let got = try XCTUnwrap(last, "the hub never got the resolve")
        XCTAssertEqual((got["resolution"] as? [String: Any])?["allow"] as? Bool, false)
    }

    @MainActor private func unlock(_ springboard: XCUIApplication) {
        springboard.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.99))
            .press(forDuration: 0.1, thenDragTo: springboard.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.3)))
    }
}
