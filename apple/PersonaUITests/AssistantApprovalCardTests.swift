import XCTest

/// Persona's own approval cards in its conversation (#2558): a hook's (Bash) and a connector's send the hub asks about
/// itself, both of the conversation's session, are drawn above the composer with Allow / Deny. The hub's own used to
/// come with no session and showed only in Needs You, so the conversation looked like it was waiting on nothing.
///   PORT=18911 MOCK_ASSISTANT_CARDS=1 python3 scripts/mock/mock_hub.py &
///   TEST_RUNNER_PERSONA_MOCK_HUB=http://127.0.0.1:18911 xcodebuild test … -only-testing:PersonaUITests/AssistantApprovalCardTests
final class AssistantApprovalCardTests: XCTestCase {
    /// The mock's main Persona conversation.
    private let main = "server/b2e4d6f8-0000-4000-8000-000000000001"

    @MainActor func testTheAssistantsApprovalCardsAreInItsConversation() throws {
        guard let hub = ProcessInfo.processInfo.environment["PERSONA_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_ASSISTANT_CARDS=1 (PERSONA_MOCK_HUB)")
        }
        let app = XCUIApplication()
        let pair = "persona://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair, "-app.tab", "assistant", "-tab", "assistant", "-openAssistant", main]
        app.launchPinned()

        let any = app.descendants(matching: .any)
        let send = any.matching(NSPredicate(format: "label CONTAINS %@", "给 me@example.com 发一封邮件")).firstMatch
        XCTAssertTrue(send.waitForExistence(timeout: 20), "the connector's card is not in the conversation")
        XCTAssertTrue(any.matching(NSPredicate(format: "label == %@", "ls ~/notes")).firstMatch.exists, "the hook's card is not in the conversation")
        XCTAssertTrue(app.buttons["允许"].firstMatch.exists, "no 允许 on the cards")

        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = "cards"
        a.lifetime = .keepAlways
        add(a)
    }
}
