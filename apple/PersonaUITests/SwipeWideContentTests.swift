import XCTest

/// A drag to the left on a reply quotes it (#1058), but not over something in it that scrolls sideways and can still go
/// on: a wide table or code block scrolls instead (#5737). At its right end, on a table that fits, or on a plain
/// reply, the drag quotes as before.
///   PORT=18798 MOCK_WIDE=1 python3 scripts/mock/mock_hub.py &
///   TEST_RUNNER_PERSONA_MOCK_HUB=http://127.0.0.1:18798 xcodebuild test … -only-testing:PersonaUITests/SwipeWideContentTests
final class SwipeWideContentTests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] != nil else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_WIDE=1 (PERSONA_MOCK_HUB)")
        }
    }

    @MainActor private func launch() {
        let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"]!
        app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        let scheme = ProcessInfo.processInfo.environment["PERSONA_SHOTS_SCHEME"] ?? "light"
        app.launchArguments = ["-pairURL", pair, "-tab", "dev", "-openSession", "macbook/a1f3c2d4-0000-4000-8000-0000000005e1", "-openChatsFresh", "YES", "-ui.scheme", scheme]
        app.launchPinned()
        acceptIconAlert()
        dropKeptQuotes()
    }

    /// Quotes are kept with the chat across launches: an earlier test's (or run's) quote is dropped first.
    @MainActor private func dropKeptQuotes() {
        let bar = app.descendants(matching: .any).matching(identifier: "composer.quotes").firstMatch
        guard bar.waitForExistence(timeout: 3) else { return }
        for _ in 0..<5 where bar.exists { bar.buttons.firstMatch.tap(); usleep(400_000) }
    }

    @MainActor private func shot(_ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = "wide \(name)"
        a.lifetime = .keepAlways
        add(a)
    }

    @MainActor private func text(_ prefix: String) -> XCUIElement {
        app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", prefix)).firstMatch
    }

    @MainActor private var quotes: XCUIElement { app.descendants(matching: .any).matching(identifier: "composer.quotes").firstMatch }

    /// What is drawn just above each text the tests drag on, in the same block.
    private let above = ["上海": "北京", "let wideCodeLine": "宽代码块", "窄表第一行": "项", "就这些": "还有吗"]

    /// The frame of a text in the window. Rows of the flipped chat list may report flipped frames, and not all of them
    /// alike (an iPad's did not match its neighbours'): when the text drawn above it reports a lower frame, it is
    /// mirrored back.
    @MainActor private func frame(_ prefix: String) -> CGRect {
        let window = app.windows.firstMatch.frame
        var f = text(prefix).frame
        if let ref = above.first(where: { prefix.hasPrefix($0.key) })?.value, text(ref).frame.minY > f.minY {
            f.origin.y = window.height - f.maxY
        }
        return f
    }

    /// The chat open, settled at the newest message with its header folded (a touch on a list still moving only stops it).
    @MainActor private func settled() {
        launch()
        XCTAssertTrue(text("宽表城市").waitForExistence(timeout: 20), "the reply did not open:\n\(app.debugDescription)")
        XCTAssertTrue(text("就这些").waitForExistence(timeout: 5))
        let slim = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "显示标题和标签")).firstMatch
        let deadline = Date.now.addingTimeInterval(10)
        while !(slim.exists && slim.isHittable) && Date.now < deadline { usleep(200_000) }
        sleep(1)
    }

    /// The chat scrolled until the row of `prefix` is well inside the window.
    @MainActor private func bringOnScreen(_ prefix: String) {
        let window = app.windows.firstMatch.frame
        for _ in 0..<4 where !window.insetBy(dx: 0, dy: 140).contains(CGPoint(x: window.midX, y: frame(prefix).midY)) {
            let up = frame(prefix).midY < window.midY
            let mid = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5))
            mid.press(forDuration: 0.05, thenDragTo: mid.withOffset(CGVector(dx: 0, dy: up ? 200 : -200)), withVelocity: .slow, thenHoldForDuration: 0.3)
            sleep(1)
        }
    }

    /// A drag to the left from three quarters across the window, on the row of `prefix` (brought on screen first).
    @MainActor private func dragLeft(on prefix: String, by dx: CGFloat = -160) {
        bringOnScreen(prefix)
        let window = app.windows.firstMatch.frame
        let from = app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: window.width * 0.75, dy: frame(prefix).midY))
        from.press(forDuration: 0.05, thenDragTo: from.withOffset(CGVector(dx: dx, dy: 0)), withVelocity: .default, thenHoldForDuration: 0.4)
        sleep(1)
    }

    /// The wide table moves under the finger, and nothing is quoted; dragged on to its right end, the next drag quotes.
    @MainActor func testWideTableScrollsThenQuotesAtItsEnd() {
        settled()
        bringOnScreen("上海")
        let before = frame("宽表城市").minX
        dragLeft(on: "上海")
        shot("table scrolled")
        XCTAssertLessThan(frame("宽表城市").minX, before - 40, "the wide table did not scroll")
        XCTAssertFalse(quotes.exists, "a drag on the wide table quoted the reply")
        // On to its right end: while it says there is more (the fade, `table-more`), a drag scrolls it and quotes nothing.
        let more = app.otherElements["table-more"]
        for _ in 0..<8 where more.exists {
            dragLeft(on: "上海", by: -300)
            XCTAssertFalse(quotes.exists, "a drag on the wide table, with more of it to the right, quoted the reply")
        }
        XCTAssertFalse(more.exists, "the wide table never got to its right end")
        dragLeft(on: "上海")
        XCTAssertTrue(quotes.waitForExistence(timeout: 3), "at its right end, a drag on the table did not quote")
        shot("table end quoted")
    }

    /// A code block with a long line scrolls the same way.
    @MainActor func testWideCodeBlockScrolls() {
        settled()
        bringOnScreen("let wideCodeLine")
        let before = frame("let wideCodeLine").minX
        dragLeft(on: "let wideCodeLine")
        XCTAssertLessThan(frame("let wideCodeLine").minX, before - 40, "the wide code block did not scroll")
        XCTAssertFalse(quotes.exists, "a drag on the wide code block quoted the reply")
    }

    /// A table that fits does not scroll: the drag quotes.
    @MainActor func testNarrowTableQuotes() {
        settled()
        dragLeft(on: "窄表第一行")
        XCTAssertTrue(quotes.waitForExistence(timeout: 3), "a drag on a table that fits did not quote")
    }

    /// A plain reply quotes as before.
    @MainActor func testPlainReplyQuotes() {
        settled()
        dragLeft(on: "就这些")
        XCTAssertTrue(quotes.waitForExistence(timeout: 3), "a drag on a plain reply did not quote")
    }
}
