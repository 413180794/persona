import XCTest

/// Searching Agents in the ChatGPT-style shell (docs/ios.md「新外壳」): the shell's top bar (☰, 助手 | Agents, 新建会话)
/// floats over the navigation bar, and the active search field rises into that bar. Drawn together they covered each
/// other (TestFlight 70): while searching the top bar makes way, and comes back on 取消.
final class ShellSearchTests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() {
        continueAfterFailure = false
    }

    @MainActor @discardableResult private func see(_ e: XCUIElement, _ what: String, timeout: TimeInterval = 10) -> XCUIElement {
        let found = e.waitForExistence(timeout: timeout)
        if !found { attachScreen("missing") }
        XCTAssertTrue(found, "no \(what)")
        return e
    }

    @MainActor private func attachScreen(_ name: String) {
        let shot = XCTAttachment(screenshot: app.screenshot())
        shot.name = name
        shot.lifetime = .keepAlways
        add(shot)
    }

    /// The shell's controls on Agents' top bar.
    @MainActor private var topBar: [(String, XCUIElement)] {
        [("☰", app.buttons["work-sidebar-button"].firstMatch), ("助手", app.buttons["shell-assistant"].firstMatch),
         ("Agents", app.buttons["shell-agents"].firstMatch), ("新建会话", app.buttons["agents-new"].firstMatch)]
    }

    /// No top-bar control that is on screen overlaps the search field.
    @MainActor private func assertClear(of field: XCUIElement, _ when: String) {
        let f = field.frame
        for (name, e) in topBar where e.exists && e.isHittable {
            XCTAssertFalse(e.frame.intersects(f), "\(name) \(e.frame) over the search field \(f) \(when)")
        }
    }

    @MainActor func testSearchTakesTheTopBarAndGivesItBack() {
        app = XCUIApplication()
        app.launchArguments = ["-demo.on", "YES", "-ui.oldLayout", "NO", "-app.tab", "chats", "-dev.openChat", ""]
        app.launchPinned()
        // Agents (a stored tab from another test, 设置, would open its sheet over the shell).
        see(app.buttons["agents-new"].firstMatch, "新建会话 on Agents", timeout: 15)
        let field = app.revealAgentsSearch()
        assertClear(of: field, "before searching")
        attachScreen("before searching")

        field.tapCenter()
        XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 5), "no keyboard for the search")
        field.typeText("zqxj")
        XCTAssertEqual(field.value as? String, "zqxj", "the search field takes what is typed")
        // No match says so (it used to show 开始第一个开发会话 and its 新建会话).
        see(app.staticTexts["没有找到「zqxj」"].firstMatch, "the no-match state")
        // The field has risen into the bar; the shell's controls must not be drawn over it (or under it).
        sleep(1)
        attachScreen("searching")
        assertClear(of: field, "while searching")
        XCTAssertFalse(app.buttons["agents-new"].firstMatch.isHittable, "新建会话 still over the bar while searching")

        let cancel = app.navigationBars.buttons.matching(
            NSPredicate(format: "label IN {'取消', 'Cancel', '关闭', 'Close'} OR identifier == 'Cancel'")).firstMatch
        see(cancel, "取消 beside the search field").tap()
        // The top bar is back, every control on it reachable, and the field back under it.
        for (name, e) in topBar {
            XCTAssertTrue(e.waitForExistence(timeout: 5) && e.isHittable, "\(name) not back after 取消")
        }
        XCTAssertFalse(app.keyboards.firstMatch.exists, "the keyboard still up after 取消")
        sleep(1)
        attachScreen("after cancel")
        assertClear(of: app.searchFields.firstMatch, "after 取消")
    }
    /// 收件箱 and 知识库, pushed on 助手, search in their own navigation bar: the shell's top bar is not over them.
    @MainActor func testSearchOnPagesPushedOnTheAssistant() {
        for (page, title) in [("inbox", "收件箱"), ("knowledge", "知识库")] {
            app = XCUIApplication()
            app.launchArguments = ["-demo.on", "YES", "-ui.oldLayout", "NO", "-newAssistant", "YES", "-openShellPage", page]
            app.launchPinned()
            see(app.navigationBars[title], "\(title) pushed on 助手", timeout: 15)
            let field = see(app.searchFields.firstMatch, "\(title)'s search")
            field.tapCenter()
            XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 5), "no keyboard for \(title)'s search")
            field.typeText("zqxj")
            XCTAssertEqual(field.value as? String, "zqxj", "\(title)'s search takes what is typed")
            sleep(1)
            attachScreen("\(page) searching")
            for id in ["assistant-sidebar-button", "assistant-new", "shell-assistant", "shell-agents"] {
                let e = app.buttons[id].firstMatch
                XCTAssertFalse(e.exists && e.isHittable && e.frame.intersects(field.frame), "\(id) over \(title)'s search field")
            }
            app.terminate()
        }
    }
}
