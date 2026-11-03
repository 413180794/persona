import XCTest

/// The shell's top-left button is the persona's face (docs/ios.md「新外壳」): it opens the drawer on 助手 and on Agents,
/// and carries the inbox's mark while the inbox has something for the user (体验模式's has). The old layout keeps its ☰
/// and the face in the middle of the bar. The empty-inbox case needs scripts/mock/mock_hub.py started with
/// MOCK_INBOX=empty (JARVIS_EMPTY_INBOX_HUB).
final class ShellAvatarTests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor private func launch(_ arguments: [String]) {
        app = XCUIApplication()
        app.launchArguments = arguments
        app.launchPinned()
    }

    @MainActor private func value(_ e: XCUIElement) -> String { e.value as? String ?? "" }

    /// The drawer is open when its rows are there; a tap on the page's card beside it closes it.
    @MainActor private func openAndCloseDrawer(_ face: XCUIElement, _ page: String) {
        face.tapCenter()
        XCTAssertTrue(app.buttons["sidebar-inbox"].waitForExistence(timeout: 5), "the face on \(page) did not open the drawer")
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.5)).tap()
        XCTAssertTrue(app.buttons["sidebar-inbox"].waitForNonExistence(timeout: 5), "the drawer stayed open on \(page)")
    }

    @MainActor func testFaceOpensTheDrawerOnBothPagesWithTheInboxMark() {
        launch(["-demo.on", "YES", "-ui.oldLayout", "NO", "-newAssistant", "YES"])
        let face = app.buttons["assistant-sidebar-button"].firstMatch
        XCTAssertTrue(face.waitForExistence(timeout: 15), "no face on 助手")
        XCTAssertEqual(face.label, "历史对话")
        // The face's state first, then the inbox's mark: 体验模式's inbox has things that need the user.
        XCTAssertTrue(value(face).hasPrefix("Persona，"), "the face's state: \(value(face))")
        XCTAssertTrue(value(face).contains("收件箱有事等你"), "no inbox mark: \(value(face))")
        openAndCloseDrawer(face, "助手")

        ShellFinger(app).switchTo(agents: true, "Agents tapped after the drawer on 助手")
        let agentsFace = app.buttons["work-sidebar-button"].firstMatch
        XCTAssertTrue(agentsFace.waitForExistence(timeout: 10), "no face on Agents")
        XCTAssertTrue(value(agentsFace).contains("收件箱有事等你"), "no inbox mark on Agents: \(value(agentsFace))")
        openAndCloseDrawer(agentsFace, "Agents")
    }

    /// Nothing in the inbox: the face alone.
    @MainActor func testNoMarkWithAnEmptyInbox() throws {
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_EMPTY_INBOX_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_INBOX=empty (JARVIS_EMPTY_INBOX_HUB)")
        }
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        launch(["-pairURL", pair, "-ui.oldLayout", "NO", "-newAssistant", "YES"])
        let face = app.buttons["assistant-sidebar-button"].firstMatch
        XCTAssertTrue(face.waitForExistence(timeout: 15), "no face on 助手")
        sleep(3) // the inbox has been fetched
        XCTAssertFalse(value(face).contains("收件箱"), "an inbox mark with nothing in the inbox: \(value(face))")
        openAndCloseDrawer(face, "助手")
    }

    /// The old layout: the tab bar, ☰ on the left, the face (its menu) in the middle.
    @MainActor func testOldLayoutKeepsItsBar() {
        launch(["-demo.on", "YES", "-ui.oldLayout", "YES", "-tab", "assistant"])
        XCTAssertTrue(app.tabBars.firstMatch.waitForExistence(timeout: 15), "no tab bar in the old layout")
        XCTAssertTrue(app.buttons["jarvis-avatar"].firstMatch.waitForExistence(timeout: 10), "no face in the old bar's middle")
        let menu = app.buttons["assistant-sidebar-button"].firstMatch
        XCTAssertTrue(menu.exists, "no ☰ in the old bar")
        XCTAssertEqual(value(menu), "", "the old bar's ☰ became the face")
    }
}
