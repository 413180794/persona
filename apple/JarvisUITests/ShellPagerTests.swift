import XCTest

/// The shell's 助手 | Agents strip (docs/ios.md「新外壳」, `ShellPager`): a drag to the left on 助手 slides to Agents, a
/// drag to the right on Agents slides back, a drag to the right on 助手 is still the drawer; pushed pages keep the
/// system's back swipe, the list scrolls up and down without paging, and a row's own swipe (置顶…) stays the row's.
/// Which page shows is checked by pixels (`agentsDrawn`): the hidden page stays in the accessibility tree.
final class ShellPagerTests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor private func launch(_ extra: [String] = []) {
        app = XCUIApplication()
        // Not a chat an earlier test left open on Agents (it comes back when Agents is first built).
        app.launchArguments = ["-demo.on", "YES", "-ui.oldLayout", "NO", "-newAssistant", "YES", "-dev.openChat", ""] + extra
        app.launchJarvis()
        XCTAssertTrue(app.buttons["assistant-sidebar-button"].firstMatch.waitForExistence(timeout: 15), "no shell")
        sleep(1)
    }

    @MainActor private func attachScreen(_ name: String) {
        let shot = XCTAttachment(screenshot: app.screenshot())
        shot.name = name
        shot.lifetime = .keepAlways
        add(shot)
    }

    /// A finger's drag between two points of the screen (fractions of its size).
    @MainActor private func drag(from a: CGVector, to b: CGVector, fast: Bool = false) {
        let w = app.windows.firstMatch
        w.coordinate(withNormalizedOffset: a)
            .press(forDuration: 0.05, thenDragTo: w.coordinate(withNormalizedOffset: b),
                   withVelocity: fast ? .fast : .default, thenHoldForDuration: 0)
    }

    /// Back from a pushed page with the edge swipe, as a finger does it (held, slow).
    @MainActor private func edgeSwipeBack() {
        let w = app.windows.firstMatch
        w.coordinate(withNormalizedOffset: CGVector(dx: 0.005, dy: 0.5))
            .press(forDuration: 0.1, thenDragTo: w.coordinate(withNormalizedOffset: CGVector(dx: 0.9, dy: 0.5)),
                   withVelocity: .slow, thenHoldForDuration: 0.1)
    }

    @MainActor private func expectAgents(_ shown: Bool, _ what: String) {
        let ok = app.waitAgentsDrawn(shown)
        if !ok { attachScreen(what) }
        XCTAssertTrue(ok, shown ? "Agents is not drawn: \(what)" : "Agents still drawn: \(what)")
    }

    @MainActor private var drawerOpen: Bool { app.descendants(matching: .any)["assistant-drawer-close"].firstMatch.exists }

    @MainActor private func toAgents() {
        drag(from: CGVector(dx: 0.85, dy: 0.45), to: CGVector(dx: 0.1, dy: 0.45))
        expectAgents(true, "a drag to the left on 助手")
        XCTAssertTrue(app.buttons["agents-new"].firstMatch.waitForExistence(timeout: 3), "no 新建会话 on Agents")
    }

    @MainActor func testDragLeftOnTheAssistantAndRightOnAgents() {
        launch()
        expectAgents(false, "launch on 助手")
        toAgents()
        XCTAssertFalse(drawerOpen, "the drawer came out on a drag to the left")
        // Back from the edge band (over a row there too: the edge is the strip's).
        drag(from: CGVector(dx: 0.02, dy: 0.6), to: CGVector(dx: 0.85, dy: 0.6))
        expectAgents(false, "a drag to the right from Agents' left edge")
        XCTAssertFalse(drawerOpen, "the drawer came out instead of 助手")
        // A slanted one (30°), and back from the filter bar (not a row: anywhere along it).
        drag(from: CGVector(dx: 0.85, dy: 0.5), to: CGVector(dx: 0.25, dy: 0.5 + 0.6 * 0.46 * 0.577))
        expectAgents(true, "a 30° drag to the left on 助手")
        let chips = app.buttons["agents.filter.all"].firstMatch
        let y = chips.frame.midY / app.windows.firstMatch.frame.height
        drag(from: CGVector(dx: 0.5, dy: y), to: CGVector(dx: 0.95, dy: y))
        expectAgents(false, "a drag to the right along Agents' filter bar")
        XCTAssertTrue(app.descendants(matching: .any)["shell-composer-field"].firstMatch.isHittable, "助手's composer not in reach")
    }

    @MainActor func testDragRightOnTheAssistantIsTheDrawer() {
        launch()
        drag(from: CGVector(dx: 0.3, dy: 0.45), to: CGVector(dx: 0.9, dy: 0.45))
        let close = app.descendants(matching: .any)["assistant-drawer-close"].firstMatch
        XCTAssertTrue(close.waitForExistence(timeout: 3), "a drag to the right on 助手 did not open the drawer")
        expectAgents(false, "the drawer on 助手")
        close.tap()
        XCTAssertTrue(close.waitForNonExistence(timeout: 3), "the drawer did not close")
        // And a drag to the left with the drawer out closes it, it does not page.
        drag(from: CGVector(dx: 0.3, dy: 0.45), to: CGVector(dx: 0.9, dy: 0.45))
        XCTAssertTrue(close.waitForExistence(timeout: 3), "the drawer again")
        drag(from: CGVector(dx: 0.9, dy: 0.45), to: CGVector(dx: 0.2, dy: 0.45))
        XCTAssertTrue(close.waitForNonExistence(timeout: 3), "a drag to the left did not close the drawer")
        expectAgents(false, "closing the drawer paged to Agents")
    }

    @MainActor func testRowSwipeStaysTheRows() {
        launch()
        toAgents()
        let row = app.descendants(matching: .any).matching(identifier: "agents.row").firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 5), "no row")
        let f = row.frame, h = app.windows.firstMatch.frame
        drag(from: CGVector(dx: 0.4, dy: f.midY / h.height), to: CGVector(dx: 0.8, dy: f.midY / h.height))
        let pin = app.buttons.matching(NSPredicate(format: "label IN {'置顶', '取消置顶'}")).firstMatch
        XCTAssertTrue(pin.waitForExistence(timeout: 3), "the row's 置顶 did not come out")
        expectAgents(true, "a row's swipe paged away")
        attachScreen("row swipe")
    }

    @MainActor func testPushedPagesSwipeBack() {
        launch()
        // 助手: 知识库 from the drawer, the edge swipe goes back to 助手 (not Agents, not the drawer).
        app.buttons["assistant-sidebar-button"].firstMatch.tapCenter()
        XCTAssertTrue(app.buttons["sidebar-知识库"].firstMatch.waitForExistence(timeout: 5), "no 知识库 in the drawer")
        app.buttons["sidebar-知识库"].firstMatch.tap()
        let kb = app.navigationBars["知识库"]
        XCTAssertTrue(kb.waitForExistence(timeout: 5), "知识库 not pushed")
        sleep(1)
        edgeSwipeBack()
        XCTAssertTrue(kb.waitForNonExistence(timeout: 5), "the swipe did not go back from 知识库")
        expectAgents(false, "back from 知识库")
        XCTAssertFalse(drawerOpen, "the drawer came out instead of going back")
        // Agents: a chat pushed, the edge swipe goes back to the list (not to 助手).
        toAgents()
        let row = app.descendants(matching: .any).matching(identifier: "agents.row").firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 5), "no row")
        row.tap()
        XCTAssertTrue(app.buttons["agents-new"].firstMatch.waitForNonExistence(timeout: 5), "the chat did not open")
        sleep(1)
        edgeSwipeBack()
        XCTAssertTrue(app.buttons["agents-new"].firstMatch.waitForExistence(timeout: 5), "the swipe did not go back to the list")
        expectAgents(true, "back from a chat on Agents")
    }

    @MainActor func testScrollingDoesNotPage() {
        launch()
        toAgents()
        app.swipeUp()
        app.swipeDown()
        expectAgents(true, "the list scrolled up and down")
        // Steeper than 45° is the list's, either way.
        drag(from: CGVector(dx: 0.3, dy: 0.7), to: CGVector(dx: 0.5, dy: 0.35))
        drag(from: CGVector(dx: 0.6, dy: 0.35), to: CGVector(dx: 0.4, dy: 0.7))
        expectAgents(true, "a steep drag on the list")
    }

    /// Stills of the strip part way (`-showShellPagerAt`), for the PR: both pages beside each other, the switch's pill
    /// between them, the top bar in place.
    @MainActor func testShotsPartWay() {
        for at in ["0.3", "0.7"] {
            launch(["-showShellPagerAt", at])
            sleep(2)
            attachScreen("strip at \(at)")
            app.terminate()
        }
    }
}
