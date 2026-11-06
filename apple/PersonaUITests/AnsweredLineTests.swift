import XCTest

/// A task's answered card in the main conversation (#1231): one line「✓ 任务 · 回答」with a disclosure; a tap opens
/// every question with its answer in full, another folds it. Against scripts/mock/mock_hub.py with MOCK_TASKS=1
/// MOCK_TASKS_NO_ASKS=1 MOCK_TASKS_AT=23 (the answered lines at the bottom) (PERSONA_MOCK_HUB). With PERSONA_SHOTS set it
/// writes the folded and the open line there (PERSONA_SHOTS_APPEARANCE names the simulator's appearance in the file).
final class AnsweredLineTests: XCTestCase {
    private var hub = ""
    private let main = "server/b2e4d6f8-0000-4000-8000-000000000001"

    override func setUpWithError() throws {
        guard let h = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_TASKS=1 MOCK_TASKS_NO_ASKS=1 MOCK_TASKS_AT=23 (PERSONA_MOCK_HUB)")
        }
        hub = h
        continueAfterFailure = false
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) throws {
        let env = ProcessInfo.processInfo.environment
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = name
        a.lifetime = .keepAlways
        add(a)
        guard let dir = env["PERSONA_SHOTS"] else { return }
        let appearance = env["PERSONA_SHOTS_APPEARANCE"] ?? "unknown"
        try app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(dir)/answered-\(appearance)-\(name).png"))
    }

    @MainActor func testAnAnsweredLineOpensToEveryQuestionAndFolds() throws {
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-app.tab", "assistant", "-tab", "assistant", "-openAssistant", main]
        app.launchPinned()

        let any = app.descendants(matching: .any)
        let lines = any.matching(identifier: "answered-line")
        let line = lines.matching(NSPredicate(format: "label CONTAINS %@", "先补请求重试和超时")).firstMatch
        XCTAssertTrue(line.waitForExistence(timeout: 20), "no answered line:\n\(app.debugDescription)")
        sleep(1) // the list settles at the newest
        XCTAssertFalse(any["answered-detail"].exists, "an answered line starts open")
        XCTAssertFalse(any.matching(NSPredicate(format: "label CONTAINS %@", "真实接口有限流")).firstMatch.exists,
                       "a folded line shows the questions")
        try shot(app, "folded")

        line.coordinate(withNormalizedOffset: CGVector(dx: 0.6, dy: 0.5)).tap()
        let detail = any["answered-detail"].firstMatch
        XCTAssertTrue(detail.waitForExistence(timeout: 5), "the line did not open")
        for text in ["网络层的测试先补哪一块？", "用本地假服务器，不连真实接口", "缓存层先不管，下周再补"] {
            XCTAssertTrue(any.matching(NSPredicate(format: "label CONTAINS %@", text)).firstMatch.exists, "the open line lacks 「\(text)」")
        }
        sleep(1)
        try shot(app, "open")

        // A record from before #1231 opens to its line.
        let old = lines.matching(NSPredicate(format: "label CONTAINS %@", "以后同类不再问")).firstMatch
        XCTAssertTrue(old.exists, "no answered line from before #1231")

        line.coordinate(withNormalizedOffset: CGVector(dx: 0.6, dy: 0.5)).tap()
        let folded = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == false"), object: detail)
        XCTAssertEqual(XCTWaiter.wait(for: [folded], timeout: 5), .completed, "the line did not fold")
        app.terminate()
    }
}
