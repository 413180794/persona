import XCTest

/// Claude's suggested next message over the composer, against scripts/mock/mock_hub.py started with MOCK_SUGGESTION=1
/// (TEST_RUNNER_PERSONA_MOCK_HUB=http://127.0.0.1:<port>).
final class SuggestionTests: XCTestCase {
    @MainActor func testTapFillsTheComposer() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)")
        }
        let suggestion = "把命名问题修完再跑一遍测试，然后推到 review/naming"
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-openSession", "macbook/a1f3c2d4-0000-4000-8000-000000000006"]
        app.launchPinned()
        let pill = app.buttons["Claude 建议：" + suggestion]
        XCTAssertTrue(pill.waitForExistence(timeout: 15), "no suggestion:\n\(app.debugDescription)")
        pill.tap()
        let field = app.textViews.firstMatch.exists ? app.textViews.firstMatch : app.textFields.firstMatch
        XCTAssertEqual(field.value as? String, suggestion)
        XCTAssertFalse(pill.exists, "the pill goes once the box has text")
        // Drafts are kept: leave the box empty for the next run.
        field.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: suggestion.count + 2))
        XCTAssertTrue(pill.waitForExistence(timeout: 3), "the suggestion comes back when the box is empty again; box: \(String(describing: field.value))")
    }
}
