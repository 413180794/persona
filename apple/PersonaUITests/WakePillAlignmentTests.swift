import XCTest

/// MOCK_WAKES=1 MOCK_WAKE_ALIGNMENT=1; opens the real completion sheet with short and overflowing Monitor labels.
final class WakePillAlignmentTests: XCTestCase {
    override func setUp() { continueAfterFailure = false }

    @MainActor func testCompletionSheetKeepsEventPillsLeading() throws {
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_WAKES=1 MOCK_WAKE_ALIGNMENT=1")
        }
        let app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-AppleLanguages", "(zh-Hans)", "-AppleLocale", "zh_CN", "-ui.oldLayout", "NO", "-openChatsFresh", "YES"]
        app.launchPinned()
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        let allow = springboard.buttons.matching(NSPredicate(format: "label IN %@", ["Allow", "允许"])).firstMatch
        if allow.waitForExistence(timeout: 3) { allow.tap() }
        app.terminate()
        app.launchArguments += ["-openSession", "macbook/a1f3c2d4-0000-4000-8000-00000000000b"]
        app.launchPinned()
        let chip = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "后台任务完成，查看之后的消息")).firstMatch
        XCTAssertTrue(chip.waitForExistence(timeout: 20), "no completion chip:\n\(app.debugDescription)")
        chip.tapCenter()
        let title = app.navigationBars["3 个后台任务完成"]
        XCTAssertTrue(title.waitForExistence(timeout: 5), "the completion sheet did not open")
        // Open to the large detent so every event and its reply is visible in one screenshot.
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.55))
            .press(forDuration: 0.1, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.15)))
        let short = app.staticTexts["Monitor 事件 · == upload"]
        let medium = app.staticTexts["Monitor 事件 · uploaded 0.1.0 (2)"]
        let long = app.staticTexts["Monitor 事件 · upload validation completed successfully; all artifacts and checksums are ready for verification"]
        let reply = app.staticTexts["仍在处理。"]
        for label in [short, medium, long, reply] {
            XCTAssertTrue(label.waitForExistence(timeout: 5) && label.isHittable, "missing visible event or reply: \(label)")
        }
        // Text follows the bell, while the capsule itself aligns with the reply (also checked by pixel unit tests).
        XCTAssertEqual(short.frame.minX, long.frame.minX, accuracy: 1)
        XCTAssertEqual(medium.frame.minX, long.frame.minX, accuracy: 1)
        XCTAssertGreaterThan(short.frame.minX, reply.frame.minX)
        XCTAssertLessThan(short.frame.minX - reply.frame.minX, 45)
        let screenshot = app.screenshot()
        let appearance = ProcessInfo.processInfo.environment["JARVIS_SHOTS_APPEARANCE"] ?? "light"
        let attachment = XCTAttachment(screenshot: screenshot)
        attachment.name = "wake-pills-\(appearance)"
        attachment.lifetime = .keepAlways
        add(attachment)
        if let dir = ProcessInfo.processInfo.environment["JARVIS_SHOTS"] {
            try screenshot.pngRepresentation.write(to: URL(fileURLWithPath: dir).appending(path: "wake-pills-\(appearance).png"))
        }
    }
}
