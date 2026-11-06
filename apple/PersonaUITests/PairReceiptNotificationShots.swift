import XCTest

/// The pairing receipts' buttons on the expanded notification (#4199), for the PR's screenshots: the app asks for
/// notifications (`-showPushKey YES`) and goes to the background, the test writes `TEST_RUNNER_PAIR_SHOT_READY` and
/// waits; the script then sends the push with `xcrun simctl push`, and the test expands the banner and keeps a shot.
/// TEST_RUNNER_PAIR_SHOT_TEXT: a piece of the push's body, to find its banner; TEST_RUNNER_PAIR_SHOT_BUTTON: a button
/// the expanded notification must show.
final class PairReceiptNotificationShots: XCTestCase {
    @MainActor func testExpandedPairReceipt() throws {
        let env = ProcessInfo.processInfo.environment
        guard let ready = env["PAIR_SHOT_READY"], let text = env["PAIR_SHOT_TEXT"], let button = env["PAIR_SHOT_BUTTON"] else {
            throw XCTSkip("run with TEST_RUNNER_PAIR_SHOT_READY / _TEXT / _BUTTON and a simctl push")
        }
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        let app = XCUIApplication()
        app.launchArguments = ["-showPushKey", "YES", "-AppleLanguages", "(zh-Hans)", "-AppleLocale", "zh_CN"]
        app.launchPinned()
        let allow = springboard.alerts.buttons.matching(NSPredicate(format: "label IN %@", ["允许", "Allow"])).firstMatch
        if allow.waitForExistence(timeout: 12) { allow.tap() }
        sleep(2) // the categories are registered at launch
        XCUIDevice.shared.press(.home)
        try Data("ready".utf8).write(to: URL(fileURLWithPath: ready))

        let banner = springboard.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch
        XCTAssertTrue(banner.waitForExistence(timeout: 60), "no banner:\n\(springboard.debugDescription)")
        banner.press(forDuration: 1.5)
        XCTAssertTrue(springboard.buttons[button].waitForExistence(timeout: 5), "the expanded notification has no \(button)")
        sleep(1)
        let shot = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
        shot.name = "pair-receipt"
        shot.lifetime = .keepAlways
        add(shot)
        // Leave it without pressing a button: the shots use made-up data and no hub.
        XCUIDevice.shared.press(.home)
    }
}
