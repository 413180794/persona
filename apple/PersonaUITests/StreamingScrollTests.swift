import XCTest

/// The chat while a reply streams (the user, 2026-11-04: 「流式输出的时候，滚动就不稳定了」). A long Markdown reply
/// (`/mock/say?preset=markdown`: a code block, a table, a list) streams at the hub's pace, a delta every 80 ms, into the
/// Persona conversation (scripts/mock/mock_hub.py with MOCK_LONG_HISTORY=1, PERSONA_MOCK_HUB).
///
/// The screen is measured from outside: `scripts`' recording of the simulator, decoded frame by frame, and the app's
/// per-frame scroll trace (`-scrollTrace YES`, Documents/scroll-trace.txt). Each case prints `STREAM <case> start|end
/// <epoch>` around the stream, for cutting both to it. What a case asserts is what XCUITest can see at the end.
final class StreamingScrollTests: XCTestCase {
    private var hub = ""
    private let persona = "server/b2e4d6f8-0000-4000-8000-000000000004" // 「整理这个季度的读书笔记」, a long one

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_LONG_HISTORY=1 (PERSONA_MOCK_HUB)")
        }
        self.hub = hub
        _ = try? Data(contentsOf: URL(string: hub + "/mock/reset")!)
    }

    /// The Persona conversation whose turn runs (MOCK_ASSISTANT_RUN=1): steps, and now and then words streamed.
    private let running = "server/b2e4d6f8-0000-4000-8000-000000000001"
    /// An agent at work (MOCK_STREAM=1): a thought, a call or a reply every 2 s.
    private let working = "macbook/a1f3c2d4-0000-4000-8000-000000000001"

    @MainActor private func launch(_ open: [String]? = nil) -> XCUIApplication {
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-dev.openChat", "", "-openChatsFresh", "YES", "-scrollTrace", "YES",
                               ] + (open ?? ["-tab", "assistant", "-openAssistant", persona])
        app.launchPinned()
        // The notification prompt of a fresh install would sit over the chat in the recording.
        let allow = XCUIApplication(bundleIdentifier: "com.apple.springboard").buttons["Allow"]
        if allow.waitForExistence(timeout: 4) { allow.tap() }
        sleep(4) // loaded, landed at the newest
        return app
    }

    private func mark(_ name: String, _ what: String) {
        print(String(format: "STREAM %@ %@ %.3f", name, what, Date.now.timeIntervalSince1970))
    }

    /// The long reply, streamed after a message from the user, then written.
    private func stream(_ user: String, late: Int = 0) {
        var c = URLComponents(string: hub + "/mock/say")!
        c.queryItems = [URLQueryItem(name: "preset", value: "markdown"), URLQueryItem(name: "user", value: user),
                        URLQueryItem(name: "stream", value: "1"), URLQueryItem(name: "steps", value: "120"),
                        URLQueryItem(name: "ms", value: "80"), URLQueryItem(name: "late", value: String(late))]
        _ = try? Data(contentsOf: c.url!)
    }

    /// At the newest end: the reply grows in place, its end just above the composer, frame after frame.
    @MainActor func testFollowingWhileAReplyStreams() {
        let app = launch()
        mark("follow", "start")
        stream("看一下三台机器的凭据")
        sleep(14) // 120 deltas × 80 ms, then the written reply
        mark("follow", "end")
        let end = app.staticTexts.containing(NSPredicate(format: "label CONTAINS %@", "有问题随时说")).firstMatch
        XCTAssertTrue(end.waitForExistence(timeout: 5), "the reply's end shows")
    }

    /// As a real hub can send them: the stream cleared at the end of the turn, the message 600 ms later.
    @MainActor func testFollowingWhenTheClearComesFirst() {
        let app = launch()
        mark("clear", "start")
        stream("看一下三台机器的凭据", late: 600)
        sleep(14)
        mark("clear", "end")
        let end = app.staticTexts.containing(NSPredicate(format: "label CONTAINS %@", "有问题随时说")).firstMatch
        XCTAssertTrue(end.waitForExistence(timeout: 5), "the reply's end shows")
    }

    /// Reading further up: nothing on screen moves while the reply streams in at the newest end, nor when it is written.
    @MainActor func testReadingWhileAReplyStreams() {
        let app = launch()
        for _ in 0..<3 {
            let from = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.35))
            from.press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.75)))
        }
        sleep(3) // the glide over
        mark("read", "start")
        stream("看一下三台机器的凭据")
        sleep(14)
        mark("read", "end")
    }

    /// Fingers drag the list toward older messages, a few times, and the glide ends.
    @MainActor private func readOlder(_ app: XCUIApplication) {
        for _ in 0..<3 {
            let from = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.35))
            from.press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.75)))
        }
        sleep(3)
    }

    /// A running turn at the newest end (MOCK_ASSISTANT_RUN=1): its work line grows, words stream in between.
    @MainActor func testFollowingARunningTurn() {
        _ = launch(["-tab", "assistant", "-openAssistant", running])
        mark("run", "start")
        sleep(30)
        mark("run", "end")
    }

    /// An agent at work (MOCK_STREAM=1), at the newest end.
    @MainActor func testFollowingAnAgentAtWork() {
        _ = launch(["-tab", "dev", "-openSession", working])
        mark("agent", "start")
        sleep(24)
        mark("agent", "end")
    }

    /// The agent at work while reading further up.
    @MainActor func testReadingWhileAnAgentWorks() {
        let app = launch(["-tab", "dev", "-openSession", working])
        readOlder(app)
        mark("agentread", "start")
        sleep(24)
        mark("agentread", "end")
    }

    /// Drawing the list away while the reply streams (the Mac showed it, 2026-11-04): what is under the finger stays
    /// under it; the streamed words did not, they went the moment new content started to wait, and came back on release.
    @MainActor func testDragAwayWhileAReplyStreams() {
        let app = launch()
        mark("drag", "start")
        stream("看一下三台机器的凭据")
        sleep(4)
        let from = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.45))
        from.press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.62)),
                   withVelocity: .slow, thenHoldForDuration: 1.5)
        sleep(9)
        mark("drag", "end")
    }

    /// Back at the newest after reading: ↓ while it streams, then following it.
    @MainActor func testArrowWhileAReplyStreams() {
        let app = launch()
        for _ in 0..<3 {
            let from = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.35))
            from.press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.75)))
        }
        sleep(3)
        mark("arrow", "start")
        stream("看一下三台机器的凭据")
        sleep(4)
        let arrow = app.buttons["回到最新"]
        if arrow.waitForExistence(timeout: 3) { arrow.tap() }
        sleep(10)
        mark("arrow", "end")
    }
}
