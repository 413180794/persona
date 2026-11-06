import XCTest

/// A message that failed to send stays where it was written (#6023): one sent while the hub cannot reach the computer
/// fails; the next goes through and lands below it, not above. Against scripts/mock/mock_hub.py with MOCK_AGENTS=1
/// MOCK_ECHO_SENT=1 (PERSONA_MOCK_HUB). The failed one is deleted at the end: the outbox would keep it for a day.
final class FailedSendPlacementTests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws { continueAfterFailure = false }

    private func mock(_ hub: String, _ what: String) {
        _ = try? Data(contentsOf: URL(string: hub + "/mock/" + what)!)
    }

    @MainActor private func shot(_ name: String) {
        let s = app.screenshot()
        let a = XCTAttachment(screenshot: s)
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        guard let dir = ProcessInfo.processInfo.environment["PERSONA_SHOTS"] else { return }
        let look = ProcessInfo.processInfo.environment["PERSONA_APPEARANCE"] ?? "light"
        try? s.pngRepresentation.write(to: URL(fileURLWithPath: dir).appending(path: "failed-send-\(name)-\(look).png"))
    }

    @MainActor private func send(_ text: String) {
        XCTAssertTrue(app.textFields.firstMatch.waitForExistence(timeout: 15), "no composer:\n\(app.debugDescription)")
        let field = app.textFields.allElementsBoundByIndex.last ?? app.textFields.firstMatch
        field.tap()
        field.typeText(text)
        let send = app.buttons.matching(NSPredicate(format: "identifier == %@ OR label == %@", "shell-composer-send", "发送")).firstMatch
        XCTAssertTrue(send.waitForExistence(timeout: 3), "no send button")
        send.tap()
    }

    @MainActor func testAFailedMessageStaysWhereItWasWritten() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_AGENTS=1 MOCK_ECHO_SENT=1 (PERSONA_MOCK_HUB)")
        }
        mock(hub, "send-fail?on=1")
        defer { mock(hub, "send-fail?on=0") }
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-openSession", "macbook/a1f3c2d4-0000-4000-8000-000000000006"]
        app.launchPinned()
        send("这条没发出去")
        let failed = app.descendants(matching: .any).matching(NSPredicate(format: "label == %@", "发送失败，重发或删除")).firstMatch
        XCTAssertTrue(failed.waitForExistence(timeout: 20), "the send did not fail:\n\(app.debugDescription)")
        shot("1-failed")
        mock(hub, "send-fail?on=0")
        send("后来这条发出去了")
        // Its copy from the transcript (MOCK_ECHO_SENT) replaces the bubble sent from here; the failed one stays.
        let sent = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "后来这条发出去了")).firstMatch
        XCTAssertTrue(sent.waitForExistence(timeout: 15), "the second message is not in the chat:\n\(app.debugDescription)")
        sleep(4)
        XCTAssertTrue(failed.exists, "the failed message went away")
        // Above the later message on screen. Rows of the upside-down list may report flipped frames: the session's
        // first message, at the top, tells which way they run.
        let lost = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "这条没发出去")).firstMatch
        let oldest = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "看一下 PR #42")).firstMatch
        XCTAssertTrue(lost.exists && oldest.exists, "\(app.debugDescription)")
        let flipped = oldest.frame.midY > max(lost.frame.midY, sent.frame.midY)
        XCTAssertTrue(flipped ? lost.frame.midY > sent.frame.midY : lost.frame.midY < sent.frame.midY,
                      "the failed message is not above the one sent after it: \(lost.frame) \(sent.frame) (flipped \(flipped))")
        shot("2-after")
        // Delete it: it would come back at every launch for a day.
        failed.tap()
        let delete = app.buttons["删除"].firstMatch
        XCTAssertTrue(delete.waitForExistence(timeout: 5), "no Delete in its menu:\n\(app.debugDescription)")
        delete.tap()
        XCTAssertTrue(lost.waitForNonExistence(timeout: 5), "the failed message stayed")
    }
}
