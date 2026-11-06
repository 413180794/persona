import XCTest

/// Knowledge search: a hit opens its entry at what was found, and coming back shows the results again (no error).
/// Against scripts/mock/mock_hub.py (PERSONA_MOCK_HUB); PERSONA_MOCK_TOKEN and PERSONA_KB_QUERY point it at another hub.
final class KnowledgeSearchTests: XCTestCase {
    @MainActor func testAHitOpensAtTheMatchAndBackKeepsTheResults() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)")
        }
        let env = ProcessInfo.processInfo.environment
        let token = env["PERSONA_MOCK_TOKEN"] ?? "mock"
        let query = env["PERSONA_KB_QUERY"] ?? "灰度"
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)!
            + "&token=" + token.addingPercentEncoding(withAllowedCharacters: .alphanumerics)!
        app.launchArguments = ["-pairURL", pair, "-tab", "assistant", "-openAssistantPage", "knowledge"]
        app.launchPinned()

        let search = app.searchFields.firstMatch
        XCTAssertTrue(search.waitForExistence(timeout: 15), "no search field:\n\(app.debugDescription)")
        search.tap()
        search.typeText(query)
        let hit = app.buttons.matching(identifier: "kb.hit").firstMatch
        XCTAssertTrue(hit.waitForExistence(timeout: 15), "no hit:\n\(app.debugDescription)")
        shot(app, "kb-1-results")
        hit.tap()

        // The entry opens with what was found at the top.
        let found = app.descendants(matching: .any).matching(identifier: "kb.matches").firstMatch
        let showsMatch = found.waitForExistence(timeout: 15)
        shot(app, "kb-2-entry")

        let match = app.buttons.matching(identifier: "kb.match").firstMatch
        if match.waitForExistence(timeout: 3) {
            match.tap()
            XCTAssertTrue(app.descendants(matching: .any).matching(identifier: "kb.focus").firstMatch.waitForExistence(timeout: 15),
                          "the day did not open at the message:\n\(app.debugDescription)")
            sleep(1)
            shot(app, "kb-3-day")
            back(app)
            sleep(1)
        }

        back(app)
        XCTAssertTrue(hit.waitForExistence(timeout: 10), "back, the results are gone:\n\(app.debugDescription)")
        sleep(2)
        shot(app, "kb-4-back")
        XCTAssertEqual(search.value as? String, query, "the field lost the query")
        XCTAssertFalse(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "已取消")).firstMatch.exists, "an error after back")
        XCTAssertTrue(showsMatch, "the entry did not show the match")
        XCTAssertFalse(app.images.matching(NSPredicate(format: "label CONTAINS %@", "Warning")).firstMatch.exists)
    }

    @MainActor private func back(_ app: XCUIApplication) {
        let b = app.navigationBars.buttons.element(boundBy: 0)
        if b.waitForExistence(timeout: 5) { b.tap() }
    }

    @MainActor private func shot(_ app: XCUIApplication, _ name: String) {
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "/tmp/\(name).png"))
    }
}
