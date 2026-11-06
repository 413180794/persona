import XCTest

/// A project's related company moments (「相关公司圈」): the posts the hub linked, and one opened with its comments. Against scripts/mock/mock_hub.py
/// (PERSONA_MOCK_HUB).
final class KnowledgeMomentsTests: XCTestCase {
    @MainActor func testAProjectListsItsPostsAndOpensOne() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (PERSONA_MOCK_HUB)")
        }
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-tab", "assistant", "-openAssistantPage", "knowledge",
                               "-showKnowledgeEntry", "projects/weather-app"]
        app.launchPinned()

        let post = app.buttons.matching(identifier: "kb.moment").firstMatch
        let list = app.collectionViews.firstMatch
        XCTAssertTrue(list.waitForExistence(timeout: 15), "the project did not open:\n\(app.debugDescription)")
        for _ in 0..<8 where !post.isHittable { list.swipeUp(velocity: .slow) }
        XCTAssertTrue(post.isHittable, "no 相关公司圈 row:\n\(app.debugDescription)")
        XCTAssertTrue(post.label.contains("用户反馈降雨条看不清") && post.label.contains("12 条评论"), post.label)
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "/tmp/kb-moments-1.png"))
        post.tap()
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "同感")).firstMatch.waitForExistence(timeout: 10),
                      "the post's comments did not show:\n\(app.debugDescription)")
        try? app.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "/tmp/kb-moments-2.png"))
    }
}
