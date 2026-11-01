import XCTest

/// The ChatGPT-style shell on an iPhone (docs/ios.md「新外壳」), in 体验模式: no tab bar; 助手 | Agents; the drawer with
/// 收件箱 / 设置; the empty chat lists what needs the user; the composer grows while typing; 旧布局 brings the tabs back.
final class ShellTests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() {
        continueAfterFailure = false
    }

    /// `-newAssistant YES` (the empty chat) also picks the Jarvis tab: tests that open another page leave it out.
    @MainActor private func launch(_ extra: [String] = [], emptyChat: Bool = true) {
        app = XCUIApplication()
        app.launchArguments = ["-demo.on", "YES", "-ui.oldLayout", "NO"] + (emptyChat ? ["-newAssistant", "YES"] : []) + extra
        app.launchPinned()
    }

    @MainActor @discardableResult private func see(_ e: XCUIElement, _ what: String, timeout: TimeInterval = 10) -> XCUIElement {
        let found = e.waitForExistence(timeout: timeout)
        if !found {
            let shot = XCTAttachment(screenshot: app.screenshot())
            shot.lifetime = .keepAlways
            add(shot)
            print("SHELLTREE-BEGIN\n\(app.debugDescription)\nSHELLTREE-END")
        }
        XCTAssertTrue(found, "no \(what)")
        return e
    }

    @MainActor private func button(_ id: String) -> XCUIElement { app.buttons[id].firstMatch }

    @MainActor private var oldLayoutSwitch: XCUIElement {
        app.switches.matching(NSPredicate(format: "identifier == 'old-layout' OR label == '旧布局'")).firstMatch
    }

    @MainActor func testSwitchBetweenChatAndWork() {
        launch()
        see(button("shell-assistant"), "the 助手 | Agents switch", timeout: 15)
        XCTAssertFalse(app.tabBars.firstMatch.exists, "the tab bar in the shell")
        // The empty chat: what needs the user (the demo has agents waiting and inbox items to answer).
        see(button("shell-needs-agent"), "an agent waiting, over the composer")
        XCTAssertTrue(button("shell-needs-inbox").exists, "the inbox's 需要你 over the composer")
        see(button("shell-agents"), "Agents").tap()
        see(button("agents-new"), "新建会话 on Agents")
        XCTAssertTrue(app.searchFields.firstMatch.waitForExistence(timeout: 5), "the agents' search on Agents")
        button("shell-assistant").tap()
        see(button("assistant-new"), "新对话 on 助手")
    }

    @MainActor func testDrawerOpensTheInboxAndSettings() {
        launch()
        see(button("assistant-sidebar-button"), "☰", timeout: 15).tapCenter()
        see(button("sidebar-inbox"), "收件箱 in the drawer").tap()
        see(app.navigationBars["收件箱"], "the inbox pushed on 助手")
        // Back once the push has settled (a tap during the push's animation can be lost).
        let inbox = app.navigationBars["收件箱"]
        for _ in 0..<2 where inbox.exists {
            sleep(1)
            inbox.buttons["BackButton"].tap()
            _ = inbox.waitForNonExistence(timeout: 3)
        }
        XCTAssertFalse(inbox.exists, "still on 收件箱 after back")
        see(button("assistant-sidebar-button"), "☰ again").tapCenter()
        // The drawer's hosting view names every element in it "assistant-sidebar": find 设置 by its label.
        see(app.buttons.matching(NSPredicate(format: "label == '设置'")).firstMatch, "设置 at the drawer's foot").tap()
        see(button("settings-close"), "设置's ×").tap()
        XCTAssertTrue(button("shell-assistant").waitForExistence(timeout: 5), "back on 助手 after closing 设置")
    }

    /// 机器 in the drawer: the one machines page (agents and monitoring together, docs/machines.md「App」), and a card
    /// opens the machine with its agents and charts on one page.
    @MainActor func testDrawerOpensMachines() {
        launch()
        see(button("assistant-sidebar-button"), "☰", timeout: 15).tapCenter()
        see(button("sidebar-machines"), "机器 in the drawer").tap()
        see(app.navigationBars["机器"], "机器 pushed on 助手")
        let server = see(app.staticTexts["服务器"], "a machine's card")
        XCTAssertTrue(app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", "个 Agent 工作中")).firstMatch.exists,
                      "the agents counted on the card")
        server.tap()
        see(app.descendants(matching: .any)["metric-range"].firstMatch, "the machine's charts on its page")
        XCTAssertTrue(app.navigationBars["服务器"].exists)
    }

    @MainActor func testComposerGrowsWhileTyping() {
        launch()
        let field = see(app.textViews["shell-composer-field"].firstMatch.exists ? app.textViews["shell-composer-field"].firstMatch
                        : app.textFields["shell-composer-field"].firstMatch, "the composer", timeout: 15)
        XCTAssertFalse(button("shell-composer-modes").exists, "the gauge before typing (one line)")
        field.tap()
        see(button("shell-composer-modes"), "the gauge once the composer opens")
        field.typeText("你好")
        see(button("shell-composer-send"), "send once there is something to send")
    }

    @MainActor func testOldLayoutBringsTheTabsBack() {
        launch(["-tab", "settings", "-openAppearance", "layout"], emptyChat: false)
        let toggle = see(oldLayoutSwitch, "旧布局 in 设置 › 外观 › 布局", timeout: 15)
        toggle.coordinate(withNormalizedOffset: CGVector(dx: 0.9, dy: 0.5)).tap()
        see(app.tabBars.firstMatch, "the tab bar with 旧布局 on")
        let back = see(oldLayoutSwitch, "旧布局, still on 布局")
        back.coordinate(withNormalizedOffset: CGVector(dx: 0.9, dy: 0.5)).tap()
        // Back in the shell, 设置 is a sheet again, still on 布局 (its own pages have a back button; × is on its root).
        see(app.navigationBars["布局"], "the shell's 设置 sheet on 布局 with 旧布局 off")
        XCTAssertFalse(app.tabBars.firstMatch.exists, "a tab bar after turning 旧布局 off")
        app.navigationBars["布局"].buttons["BackButton"].tap()
        see(button("settings-close"), "设置's ×").tap()
        see(button("shell-assistant"), "助手 after closing 设置")
    }
}
