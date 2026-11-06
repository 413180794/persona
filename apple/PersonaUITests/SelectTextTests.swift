import XCTest

/// Long-press a message › 选择文本: the message opens in a page where any part of it can be selected and copied, not
/// only the whole of it (复制). A reply reads as in the chat, without the Markdown marks; your own message stays as
/// written (its bubble shows it plain: `__init__.py`, `2*3*4`, `# …`, `- …` are kept).
///   PORT=18950 MOCK_CHAT_READ=1 MOCK_SELECT_TEXT=1 python3 scripts/mock/mock_hub.py &
///   TEST_RUNNER_JARVIS_MOCK_HUB=http://127.0.0.1:18950 xcodebuild test … -only-testing:PersonaUITests/SelectTextTests
/// Both styles (TEST_RUNNER_JARVIS_UI_STYLE=beautiful), iPhone and iPad; `TEST_RUNNER_JARVIS_SHOTS=<dir>` saves the
/// page, named by the simulator's appearance (`simctl ui <udid> appearance light|dark`).
final class SelectTextTests: XCTestCase {
    private var app: XCUIApplication!
    private let agentChat = ["-tab", "dev", "-openSession", "macbook/a1f3c2d4-0000-4000-8000-0000000c1ead"]
    private let selectChat = ["-tab", "dev", "-openSession", "macbook/a1f3c2d4-0000-4000-8000-0000005e1ec7"]
    private let mainChat = ["-app.tab", "assistant", "-tab", "assistant", "-openAssistant", "server/b2e4d6f8-0000-4000-8000-000000000001"]

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] != nil else {
            throw XCTSkip("needs scripts/mock/mock_hub.py with MOCK_CHAT_READ=1 MOCK_SELECT_TEXT=1 (JARVIS_MOCK_HUB)")
        }
    }

    @MainActor private func launch(_ args: [String]) {
        let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"]!
        app = XCUIApplication()
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app.launchArguments = ["-pairURL", pair] + args
        app.launchPinned()
    }

    /// Long-presses the message starting with `message` and opens 选择文本; returns the page's text view.
    @MainActor private func openSelection(_ message: String, shot name: String) -> XCUIElement {
        let text = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", message)).firstMatch
        XCTAssertTrue(text.waitForExistence(timeout: 20), "\(message) is not in the chat")
        sleep(3) // the list settles at the newest message; the header folds
        // Rows of the flipped chat list may report flipped frames: press by the centre in the window.
        let window = app.windows.firstMatch.frame
        var f = text.frame
        if !window.contains(CGPoint(x: f.midX, y: f.midY)) { f.origin.y = window.height - f.maxY }
        app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: f.midX, dy: f.midY)).press(forDuration: 0.6)
        let select = app.buttons["选择文本"]
        XCTAssertTrue(select.waitForExistence(timeout: 3), "the bar has no 选择文本:\n\(app.debugDescription)")
        XCTAssertTrue(app.buttons["复制"].exists, "复制 (the whole message) is gone")
        shot(name + "-bar")
        // On its label, the lower part: the message above may be drawn over the bar's top and take touches there
        // (the rows' zIndex does not hold in the lazy list, a bug of its own).
        select.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.85)).tap()
        let page = app.textViews["selectable-text"]
        XCTAssertTrue(page.waitForExistence(timeout: 5), "选择文本 did not open its page")
        sleep(1) // the sheet's animation
        shot(name)
        return page
    }

    /// Double-taps the page's first word (`word`, at the start of its first line), copies it from the edit menu and
    /// checks the pasteboard holds exactly that word.
    @MainActor private func copyFirstWord(of page: XCUIElement, _ word: String) {
        UIPasteboard.general.string = "-"
        page.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: 30, dy: 28)).doubleTap()
        let copy = app.menuItems["拷贝"].exists ? app.menuItems["拷贝"] : app.buttons["拷贝"]
        XCTAssertTrue(copy.waitForExistence(timeout: 3), "no edit menu after a double tap:\n\(app.debugDescription)")
        shot("selected")
        copy.tap()
        sleep(1)
        XCTAssertEqual(pasted(), word)
    }

    /// What the app put on the pasteboard. The runner reading another app's copy asks first ("… would like to paste
    /// from Persona"), and that read blocks until it is answered: read off the main thread and allow it on SpringBoard.
    @MainActor private func pasted() -> String {
        nonisolated(unsafe) var got: String?
        let done = DispatchSemaphore(value: 0)
        DispatchQueue.global().async {
            got = UIPasteboard.general.string
            done.signal()
        }
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        let allow = springboard.buttons.matching(NSPredicate(format: "label IN %@", ["Allow Paste", "允许粘贴"])).firstMatch
        let deadline = Date.now.addingTimeInterval(10)
        while done.wait(timeout: .now() + 0.3) == .timedOut && Date.now < deadline {
            if allow.exists { allow.tap() }
        }
        return got ?? ""
    }

    /// A reply with a fence inside a longer fence, a formula, indented code, a rule and a tab-indented item: the
    /// marks go, those stay as written; one word copies as itself.
    @MainActor func testCopyAWordOfAReply() throws {
        launch(selectChat)
        let page = openSelection("Yes", shot: "reply")
        XCTAssertEqual(page.value as? String, "Yes，2*3*4 没错，是 24。例子：\n\n```swift\nlet a = x*y*z\n```\n\n"
                       + "公式 $x*y*z$ 照原样。\n\n    indented *code*\n\n* * *\n\n• 一\n\t• tab item")
        copyFirstWord(of: page, "Yes")
        XCTAssertTrue(app.buttons["全部拷贝"].exists)
        app.buttons["完成"].tap()
        XCTAssertTrue(page.waitForNonExistence(timeout: 3), "完成 did not close the page")
    }

    /// Your own message: exactly as written, no Markdown applied.
    @MainActor func testYourOwnMessageStaysAsWritten() throws {
        launch(selectChat)
        let page = openSelection("Python", shot: "user")
        XCTAssertEqual(page.value as? String, "Python 里 __init__.py 的 2*3*4 是不是写错了？\n# 不是标题\n- 也不是列表")
        copyFirstWord(of: page, "Python")
    }

    /// An Agents reply with a fenced code block: the page shows the prose and the code without the fence.
    @MainActor func testAnAgentReplyReadsAsInTheChat() throws {
        launch(agentChat)
        let page = openSelection("改好了，测试全过", shot: "agent")
        let all = page.value as? String ?? ""
        XCTAssertTrue(all.hasPrefix("改好了，测试全过。新的写法：\n\n"), all)
        XCTAssertTrue(all.contains("let hours = forecast.hours"), "the code is not on the page: \(all)")
        XCTAssertFalse(all.contains("```"), "the fence is shown: \(all)")
        XCTAssertTrue(all.contains("$S = \\pi r^2$"), "the formula is not as written: \(all)")
    }

    /// Persona's main conversation: bold and a numbered list read as they do in the chat, without `**`; 全部拷贝
    /// copies the message as written.
    @MainActor func testAPersonaReplyReadsAsInTheChat() throws {
        launch(mainChat)
        let page = openSelection("今天有", shot: "persona")
        let all = page.value as? String ?? ""
        XCTAssertTrue(all.hasPrefix("今天有 3 件事需要你："), "the page shows the Markdown marks: \(all)")
        XCTAssertTrue(all.contains("1. 补齐网络层的单元测试"), "the list is not on the page: \(all)")
        UIPasteboard.general.string = "-"
        app.buttons["全部拷贝"].tap()
        XCTAssertTrue(page.waitForNonExistence(timeout: 3), "全部拷贝 did not close the page")
        XCTAssertTrue(pasted().hasPrefix("今天有 **3 件事**需要你："), "全部拷贝 did not copy the message as written")
    }

    @MainActor private func shot(_ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = "select-text \(name) \(UITestStyle.name)"
        a.lifetime = .keepAlways
        add(a)
        guard let dir = ProcessInfo.processInfo.environment["JARVIS_SHOTS"] else { return }
        let mode = XCUIDevice.shared.appearance == .dark ? "dark" : "light"
        let device = UIDevice.current.userInterfaceIdiom == .pad ? "ipad" : "iphone"
        try? app.screenshot().pngRepresentation
            .write(to: URL(fileURLWithPath: dir).appending(path: "select-text-\(name)-\(device)-\(UITestStyle.name)-\(mode).png"))
    }
}
