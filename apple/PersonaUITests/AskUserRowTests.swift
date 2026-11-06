import XCTest

/// The assistant's ask_user in the chat (#677): the tool row reads Question with its question, and opened it keeps what was
/// asked and what the user picked (the card is gone once answered); a card the user let go says No answer. It used to be
/// the tool's raw name beside an empty chip.
///   PORT=18797 MOCK_ASK_USER=1 python3 scripts/mock/mock_hub.py &
///   TEST_RUNNER_PERSONA_MOCK_HUB=http://127.0.0.1:18797 xcodebuild test … -only-testing:PersonaUITests/AskUserRowTests
final class AskUserRowTests: XCTestCase {
    private var hub = ""
    /// The mock's main Persona conversation.
    private let main = "server/b2e4d6f8-0000-4000-8000-000000000001"

    override func setUpWithError() throws {
        guard let h = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_ASK_USER=1 (PERSONA_MOCK_HUB)")
        }
        hub = h
        continueAfterFailure = true
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        let suffix = ProcessInfo.processInfo.environment["SHOT_SUFFIX"].map { "-" + $0 } ?? ""
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "/tmp/persona-ask-user-\(name)\(suffix).png"))
    }

    @MainActor func testAnAnsweredAskUserShowsTheQuestionAndThePick() {
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        // Every work line and tool row open: rows in the upside-down chat list do not take XCUITest's taps reliably.
        app.launchArguments = ["-pairURL", pair, "-app.tab", "assistant", "-tab", "assistant", "-openAssistant", main,
                               "-openAllWork", "YES", "-showToolDetails", "YES"]
        app.launchPinned()

        let any = app.descendants(matching: .any)
        let detail = any["ask-user-detail"].firstMatch
        XCTAssertTrue(detail.waitForExistence(timeout: 20), "no opened ask_user in the chat")
        sleep(1) // the list settles at the newest
        shot(app, "chat")
        XCTAssertFalse(any.matching(NSPredicate(format: "label == %@", "ask_user")).firstMatch.exists, "the row still reads ask_user")
        XCTAssertTrue(any.matching(NSPredicate(format: "label CONTAINS %@", "提问")).firstMatch.exists, "no 提问 row")
        XCTAssertTrue(any.matching(NSPredicate(format: "label == %@", "要把明早的会挪到下午吗？")).firstMatch.exists,
                      "the unanswered question is not in the chat")
        XCTAssertTrue(any.matching(NSPredicate(format: "label == %@", "没有回答")).firstMatch.exists, "the let-go card does not say 没有回答")

        // The answered one is further up: short drags until its question is in the middle of the screen (isHittable
        // is no guide in the upside-down list; a swipe flings it to the top).
        let first = any.matching(NSPredicate(format: "label == %@", "先处理哪项？")).firstMatch
        let screen = app.windows.firstMatch.frame
        let inView = { first.exists && first.frame.minY > screen.height * 0.2 && first.frame.minY < screen.height * 0.6 }
        let window = app.windows.firstMatch
        for _ in 0..<12 where !inView() {
            window.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.45))
                .press(forDuration: 0.05, thenDragTo: window.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.6)))
        }
        sleep(1)
        XCTAssertTrue(inView(), "the answered question \(first.frame) did not come into view")
        shot(app, "answered")
        let picked = { (label: String) in any.matching(NSPredicate(format: "label == %@", label)).firstMatch.isSelected }
        XCTAssertTrue(picked("看未读消息"), "the pick is not ticked")
        XCTAssertFalse(picked("看日程"), "an option not picked is ticked")
        XCTAssertTrue(picked("组里") && picked("只给我"), "the multi-select picks are not ticked")
        XCTAssertFalse(picked("老板"), "a multi-select option not picked is ticked")
        app.terminate()
    }
}
