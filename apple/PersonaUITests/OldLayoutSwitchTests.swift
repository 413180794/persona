import XCTest

/// The user's flow (2026-11-02, docs/apple/shell.md) in Old Layout: Agents → an agent's chat → back (the chevron or the edge
/// swipe, at once or a while later) → Persona → Agents, the tab bar tapped where a finger taps it. Each tab must be the
/// one selected and its page the one in reach.
final class OldLayoutSwitchTests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() {
        continueAfterFailure = true
    }

    @MainActor private func attachScreen(_ name: String) {
        let shot = XCTAttachment(screenshot: app.screenshot())
        shot.name = name
        shot.lifetime = .keepAlways
        add(shot)
    }

    @MainActor private func point(_ e: XCUIElement) -> XCUICoordinate {
        app.windows.firstMatch.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: e.frame.midX, dy: e.frame.midY))
    }

    /// Waits until the tab is the selected one.
    @MainActor @discardableResult private func expectTab(_ name: String, _ what: String) -> Bool {
        let tab = app.tabBars.buttons[name].firstMatch
        let ok = XCTWaiter().wait(for: [XCTNSPredicateExpectation(predicate: NSPredicate(format: "selected == true"), object: tab)],
                                  timeout: 1.5) == .completed
        if !ok { attachScreen(what); XCTFail("\(name) is not the selected tab: \(what)") }
        return ok
    }

    @MainActor func testBackFromAChatThenSwitchTabs() {
        let row = "补齐网络层的单元测试"
        for (swipe, delay) in [(false, 0.0), (false, 0.4), (true, 0.0), (true, 1.0)] {
            let what = "\(swipe ? "swiped" : "tapped") back, tabs tapped after \(delay)s"
            app = XCUIApplication()
            app.launchArguments = ["-demo.on", "YES", "-ui.oldLayout", "YES", "-dev.openChat", "", "-tab", "assistant", "-dev.folded", ""]
            app.launchPinned()
            let persona = app.tabBars.buttons["Persona"].firstMatch, agents = app.tabBars.buttons["Agents"].firstMatch
            XCTAssertTrue(persona.waitForExistence(timeout: 15) && agents.exists, "no tab bar")
            let personaAt = point(persona), agentsAt = point(agents)
            agentsAt.tap()
            guard expectTab("Agents", "Agents before (\(what))") else { app.terminate(); continue }
            let title = app.staticTexts[row].firstMatch
            XCTAssertTrue(title.waitForExistence(timeout: 5), "no row")
            point(title).tap()
            XCTAssertTrue(app.tabBars.firstMatch.waitForNonExistence(timeout: 5) || !title.isHittable, "the chat did not open (\(what))")
            sleep(1)
            let w = app.windows.firstMatch
            if swipe {
                w.coordinate(withNormalizedOffset: CGVector(dx: 0.005, dy: 0.5))
                    .press(forDuration: 0.1, thenDragTo: w.coordinate(withNormalizedOffset: CGVector(dx: 0.9, dy: 0.5)),
                           withVelocity: .fast, thenHoldForDuration: 0)
            } else {
                w.coordinate(withNormalizedOffset: CGVector(dx: 0.065, dy: 0.094)).tap()
            }
            if delay > 0 { usleep(useconds_t(delay * 1_000_000)) }
            // The tab bar comes back with the list; tapped as soon as it is there.
            XCTAssertTrue(persona.waitForExistence(timeout: 3), "no tab bar back (\(what))")
            personaAt.tap()
            expectTab("Persona", "Persona tapped (\(what))")
            agentsAt.tap()
            if expectTab("Agents", "Agents tapped (\(what))") {
                XCTAssertTrue(title.waitForExistence(timeout: 3) && title.isHittable, "the Agents list is not in reach (\(what))")
            }
            app.terminate()
        }
    }
}
