import XCTest

/// Expand in the shell's composer (#3167): a draft three lines long offers it; the full-page editor holds the same text and
/// quotes, edits there are the box's when it goes down, and its Send sends as the box's does. The assistant's and an
/// agent's conversation, keyboard up and down; iPhone and iPad alike (the iPad draws the same composer, #2779).
/// Against scripts/mock/mock_hub.py or composer_hub.py (TEST_RUNNER_JARVIS_MOCK_HUB=…); TEST_RUNNER_JARVIS_SHOTS=<dir>
/// keeps screenshots, TEST_RUNNER_JARVIS_SHOTS_APPEARANCE names them (light / dark).
final class ComposerExpandTests: XCTestCase {
    private var app: XCUIApplication!
    private var hub = ""
    private let agent = "macbook/a1f3c2d4-0000-4000-8000-000000000001"
    private let assistant = "server/b2e4d6f8-0000-4000-8000-000000000001"

    override func setUpWithError() throws {
        continueAfterFailure = false
        guard let hub = ProcessInfo.processInfo.environment["JARVIS_MOCK_HUB"] else {
            throw XCTSkip("needs scripts/mock/mock_hub.py (JARVIS_MOCK_HUB)")
        }
        self.hub = hub
        // TEST_RUNNER_JARVIS_ORIENTATION=landscape: the same on its side (the folded iPhone Duo's side status bar).
        if ProcessInfo.processInfo.environment["JARVIS_ORIENTATION"] == "landscape" { XCUIDevice.shared.orientation = .landscapeLeft }
    }

    override func tearDown() {
        if let app, (testRun?.totalFailureCount ?? 0) > 0 {
            let tree = XCTAttachment(string: app.debugDescription)
            tree.name = "composer tree"
            tree.lifetime = .keepAlways
            add(tree)
        }
        if ProcessInfo.processInfo.environment["JARVIS_ORIENTATION"] == "landscape" { XCUIDevice.shared.orientation = .portrait }
        super.tearDown()
    }

    @MainActor func testAnAgentsDraftOpensFullPage() { expands(isAssistant: false) }

    @MainActor func testTheAssistantsDraftOpensFullPage() { expands(isAssistant: true) }

    /// One paragraph, no new line, wrapped past three lines by the field's width: Expand all the same.
    @MainActor func testALongParagraphOffersIt() {
        open(isAssistant: true)
        startTyping()
        let line = String(repeating: "a long paragraph wraps ", count: 9) + "end"
        field.typeText(line)
        XCTAssertTrue(expand.waitForExistence(timeout: 5), "no 展开 for a wrapped paragraph")
        XCTAssertFalse((field.value as? String ?? "").contains("\n"))
        expand.tap()
        XCTAssertTrue(page.waitForExistence(timeout: 5))
        XCTAssertEqual(page.value as? String, line)
        shot("assistant-paragraph-page")
        element("composer-page-collapse").tap()
        XCTAssertTrue(page.waitForNonExistence(timeout: 5))
        leaveEmpty()
    }

    /// The cursor goes over and back where it is, not at the end: an edit in the middle on the page, then typing on in
    /// the box goes on from there. A swipe down keeps what was typed on the page, as Collapse does.
    @MainActor func testTheCursorAndASwipeDownCarryOver() {
        // With the quotes: they put the page's text below its bar, where a tap lands in the text (without them the
        // text view starts under the bar).
        open(isAssistant: false, quotes: true)
        startTyping()
        field.typeText("line one\nline two\nline three\nline four")
        XCTAssertTrue(expand.waitForExistence(timeout: 5))
        expand.tap()
        XCTAssertTrue(page.waitForExistence(timeout: 5))
        sleep(1)
        // In the second line: the cursor goes to a word's start or end there, mid-text either way (hardware arrow keys
        // would be exact, but leave the iPad simulator in its hardware-keyboard mode, the on-screen one gone).
        page.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: 30, dy: 50)).tap()
        sleep(1)
        page.typeText("X")
        let edited = page.value as? String ?? ""
        XCTAssertTrue(edited.contains("X") && !edited.hasSuffix("X"),
                      "the tap did not put the cursor mid-text: \(edited.debugDescription)")
        let mid = edited.components(separatedBy: "X")[0] // what is before the cursor
        element("composer-page-collapse").tap()
        XCTAssertTrue(page.waitForNonExistence(timeout: 5))
        XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 5))
        field.typeText("Y")
        let after = field.value as? String ?? ""
        XCTAssertTrue(after.hasPrefix(mid + "XY"), "the box's cursor is not where the page left it: \(after.debugDescription)")
        shot("agent-cursor-mid")

        // A swipe down on the page's bar: the page goes, what was typed there stays.
        guard drags else { return leaveEmptyWithoutQuotes() }
        expand.tap()
        XCTAssertTrue(page.waitForExistence(timeout: 5))
        sleep(1)
        page.typeText(" swiped")
        let bar = element("composer-page-collapse").frame
        let from = app.coordinate(withNormalizedOffset: .zero).withOffset(CGVector(dx: app.frame.midX, dy: bar.midY))
        from.press(forDuration: 0.05, thenDragTo: from.withOffset(CGVector(dx: 0, dy: app.frame.height * 0.7)),
                   withVelocity: .fast, thenHoldForDuration: 0)
        XCTAssertTrue(page.waitForNonExistence(timeout: 5), "the swipe did not take the page down")
        XCTAssertTrue((field.value as? String ?? "").hasPrefix(mid + "XY swiped"), "the swipe lost the page's edit: \(String(describing: field.value))")
        XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 5), "the keyboard did not come back to the box")
        shot("agent-swiped")
        leaveEmptyWithoutQuotes()
    }

    /// `leaveEmpty`, and the quotes taken off: unsent, they would stay with the conversation for the next test.
    @MainActor private func leaveEmptyWithoutQuotes() {
        leaveEmpty()
        for _ in 1...4 {
            guard let x = app.buttons.matching(NSPredicate(format: "label == '取消引用'")).allElementsBoundByIndex.first(where: \.isHittable) else { break }
            x.tap()
            sleep(1)
        }
    }

    /// ⌘⏎ (a hardware keyboard) on the page sends from the page and takes it down, once: the box's own ⌘⏎ under the
    /// sheet is off meanwhile.
    @MainActor func testCommandReturnSendsFromThePage() throws {
        // On the iOS 26.5 simulators here (Xcode 27.2) a synthesized ⌘⏎ never reached the page's shortcut; run it where
        // the simulator has a hardware keyboard: TEST_RUNNER_JARVIS_HARDWARE_KEYS=1.
        guard ProcessInfo.processInfo.environment["JARVIS_HARDWARE_KEYS"] == "1" else {
            throw XCTSkip("needs a hardware keyboard on the simulator (JARVIS_HARDWARE_KEYS=1)")
        }
        open(isAssistant: false)
        startTyping()
        let marker = "expand-cmd-return-\(Int(Date.now.timeIntervalSince1970))"
        field.typeText("line one\nline two\nline three\n\(marker)")
        XCTAssertTrue(expand.waitForExistence(timeout: 5))
        expand.tap()
        XCTAssertTrue(page.waitForExistence(timeout: 5))
        sleep(1)
        page.typeKey("z", modifierFlags: []) // does a synthesized hardware key reach the text view at all?
        let probe = page.value as? String ?? ""
        page.typeKey(XCUIKeyboardKey.return.rawValue, modifierFlags: .command)
        XCTAssertTrue(page.waitForNonExistence(timeout: 5),
                      "⌘⏎ left the page up (a plain key \(probe.hasSuffix("z") ? "did" : "did not") arrive): \((page.value as? String ?? "").debugDescription)")
        let cleared = NSPredicate(format: "value == '' OR value == %@", field.placeholderValue ?? "")
        expectation(for: cleared, evaluatedWith: field)
        waitForExpectations(timeout: 8)
        assertSentOnce(marker)
    }

    /// TEST_RUNNER_JARVIS_NO_DRAG=1: no synthesized drags (the unfolded iPhone Duo upright, where XCUITest takes the
    /// screen for portrait upside down and a drag lands elsewhere); the steps behind them are left out.
    private var drags: Bool { ProcessInfo.processInfo.environment["JARVIS_NO_DRAG"] != "1" }

    private func element(_ id: String) -> XCUIElement { app.descendants(matching: .any).matching(identifier: id).firstMatch }
    private var field: XCUIElement { element("shell-composer-field") }
    private var expand: XCUIElement { element("shell-composer-expand") }
    private var page: XCUIElement { element("composer-page-field") }

    /// `quotes`: `-showSampleQuotes`, only where the test sends (unsent, they stay with the conversation for the next).
    @MainActor private func open(isAssistant: Bool, quotes: Bool = false) {
        let pair = "jarvis://pair?url=" + hub.addingPercentEncoding(withAllowedCharacters: .alphanumerics)! + "&token=mock"
        app = XCUIApplication()
        // Pair first on its own: a first pairing resets the route arguments (as ComposerKeyboardTests).
        app.launchArguments = ["-pairURL", pair, "-ui.oldLayout", "NO"]
        app.launchPinned()
        XCTAssertTrue(app.buttons["Persona"].waitForExistence(timeout: 15) || field.waitForExistence(timeout: 15))
        app.terminate()
        app.launchArguments = ["-pairURL", pair, "-ui.oldLayout", "NO", "-holdChromeOpen", "YES", "-showSampleQuotes", quotes ? "YES" : "NO",
                               "-dev.openChat", "", "-app.tab", isAssistant ? "assistant" : "chats",
                               "-tab", isAssistant ? "assistant" : "dev"]
        app.launchArguments += isAssistant ? ["-openAssistant", assistant] : ["-openSession", agent]
        app.launchPinned()
        XCTAssertTrue(field.waitForExistence(timeout: 20), "the conversation did not open:\n\(app.debugDescription)")
        if quotes { sleep(4) } else { sleep(2) } // `-showSampleQuotes` puts its quotes over the field after 3 s
    }

    /// The keyboard on the box, the box empty (drafts are kept across runs).
    @MainActor private func startTyping() {
        // A second tap when the first only closed something over the box (the unfolded Duo's sidebar, left open).
        for _ in 1...2 where !app.keyboards.firstMatch.exists {
            field.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.9)).tap()
            _ = app.keyboards.firstMatch.waitForExistence(timeout: 4)
        }
        XCTAssertTrue(app.keyboards.firstMatch.exists, "no keyboard")
        clear(field)
    }

    /// No draft left for the next test (it would open as the card); drafts are saved a moment after the last change.
    @MainActor private func leaveEmpty() {
        field.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.9)).tap()
        clear(field)
        sleep(2)
    }

    @MainActor private func expands(isAssistant: Bool) {
        open(isAssistant: isAssistant, quotes: true)
        let name = isAssistant ? "assistant" : "agent"
        startTyping()

        // Two lines: the box is still enough.
        field.typeText("line one\nline two")
        sleep(1)
        XCTAssertFalse(expand.exists, "展开 offered for two lines")
        // Four: Expand in the card's row, with a title and an icon, over the keyboard.
        let marker = "expand-\(name)-\(Int(Date.now.timeIntervalSince1970))"
        field.typeText("\nline three\n\(marker)")
        XCTAssertTrue(expand.waitForExistence(timeout: 5), "no 展开 for four lines")
        XCTAssertEqual(expand.label, "展开输入框")
        XCTAssertTrue(expand.isHittable)
        XCTAssertLessThanOrEqual(expand.frame.maxY, app.keyboards.firstMatch.frame.minY + 1, "展开 under the keyboard")
        XCTAssertGreaterThan(expand.frame.minY, field.frame.maxY - 4, "展开 in the row under the field")
        shot("\(name)-bar-long")
        let draft = "line one\nline two\nline three\n\(marker)"
        XCTAssertEqual(field.value as? String, draft)

        // Expand: the page, filling the window, with the same text and the quotes.
        expand.tap()
        XCTAssertTrue(page.waitForExistence(timeout: 5), "the full-page editor did not open")
        sleep(1)
        XCTAssertEqual(page.value as? String, draft, "the page shows another text than the box")
        XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 5), "the page's editor has no keyboard")
        // Page-sized: down to the keyboard (or the window's end), whatever the screen.
        let bottom = min(app.keyboards.firstMatch.frame.minY, app.frame.maxY)
        XCTAssertGreaterThan(page.frame.maxY, bottom - 80, "the page's editor stops short of the keyboard (\(page.frame), keyboard at \(bottom))")
        XCTAssertTrue(app.buttons.matching(NSPredicate(format: "label == '取消引用'")).allElementsBoundByIndex.contains { $0.isHittable },
                      "the quotes are not on the page")
        XCTAssertTrue(element("composer-page-collapse").isHittable && element("composer-page-send").isHittable)
        XCTAssertEqual(element("composer-page-collapse").label, "收起输入框")
        XCTAssertEqual(element("composer-page-send").label, "发送")
        shot("\(name)-page")

        // An edit at the cursor (the end, where the box left it), then Collapse: the box has it, the keyboard on the box.
        page.typeText(" edited")
        element("composer-page-collapse").tap()
        XCTAssertTrue(page.waitForNonExistence(timeout: 5), "收起 left the page up")
        XCTAssertEqual(field.value as? String, draft + " edited", "the edit did not reach the box")
        XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 5), "the keyboard did not come back to the box")
        field.typeText("!") // the cursor is still at the end
        XCTAssertEqual(field.value as? String, draft + " edited!", "the cursor moved")
        shot("\(name)-collapsed")

        // Keyboard down: Expand is still there, and the page's Send sends what it shows.
        if drags { putKeyboardAway(name) }
        expand.tap()
        XCTAssertTrue(page.waitForExistence(timeout: 5))
        sleep(1)
        XCTAssertEqual(page.value as? String, draft + " edited!")
        shot("\(name)-page-again")
        element("composer-page-send").tap()
        XCTAssertTrue(page.waitForNonExistence(timeout: 5), "发送 left the page up")
        let cleared = NSPredicate(format: "value == '' OR value == %@", field.placeholderValue ?? "")
        expectation(for: cleared, evaluatedWith: field)
        waitForExpectations(timeout: 8)
        assertSentOnce(marker, quotes: true)
        XCTAssertFalse(expand.exists, "展开 still offered for an empty box")
        shot("\(name)-sent")
    }

    /// Drags the chat down until the keyboard goes; Expand must still be there.
    @MainActor private func putKeyboardAway(_ name: String) {
        for y in [0.2, 0.135, 0.1] where app.keyboards.firstMatch.exists { // a drag the list took as a scroll's end leaves it up
            // High on the page: lower down, over the quotes and a long draft, the assistant's task chips (a sideways
            // scroll) took the drag. On the folded Duo's short screen only a strip of the chat is left under its bar.
            let from = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: y))
            from.press(forDuration: 0.05, thenDragTo: from.withOffset(CGVector(dx: 0, dy: 160)), withVelocity: .default,
                       thenHoldForDuration: 0.1)
            _ = app.keyboards.firstMatch.waitForNonExistence(timeout: 3)
        }
        XCTAssertFalse(app.keyboards.firstMatch.exists, "the drag did not put the keyboard away")
        sleep(1)
        XCTAssertTrue(expand.isHittable, "展开 gone with the keyboard")
        shot("\(name)-bar-keyboard-down")
    }

    /// The mock's record of sends (`/mock/sent`) has exactly one message with `marker` in it, still one a few seconds
    /// later; `quotes`: with the quotes `-showSampleQuotes` put over the box.
    private func assertSentOnce(_ marker: String, quotes: Bool = false, file: StaticString = #filePath, line: UInt = #line) {
        func matching() -> [String] {
            guard let data = try? Data(contentsOf: URL(string: hub + "/mock/sent")!),
                  let all = (try? JSONSerialization.jsonObject(with: data) as? [String: Any])?["sent"] as? [Any] else { return [] }
            return all.compactMap { entry in
                guard let d = try? JSONSerialization.data(withJSONObject: entry), let s = String(data: d, encoding: .utf8) else { return nil }
                return s.contains(marker) ? s : nil
            }
        }
        let deadline = Date.now.addingTimeInterval(20)
        while matching().isEmpty, Date.now < deadline { sleep(1) }
        sleep(6) // a second send would have come by now
        let got = matching()
        XCTAssertEqual(got.count, 1, "the hub got \(got.count) messages sent from the page: \(got)", file: file, line: line)
        if quotes {
            XCTAssertTrue(got.first?.contains("WidgetKit") == true, "the quotes did not go with it: \(got)", file: file, line: line)
        }
    }

    /// Empties the field (drafts are kept across runs). A long draft can leave the cursor short of its end (the box
    /// squeezed, scrolled to the middle): again from its end until nothing is left.
    @MainActor private func clear(_ field: XCUIElement) {
        for _ in 1...3 {
            let value = field.value as? String ?? ""
            let typed = value == field.placeholderValue ? 0 : value.count
            guard typed > 0 else { return }
            field.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.9)).tap()
            field.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: typed + 2))
        }
    }

    @MainActor private func shot(_ name: String) {
        let screenshot = app.screenshot()
        let attachment = XCTAttachment(screenshot: screenshot)
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
        guard let dir = ProcessInfo.processInfo.environment["JARVIS_SHOTS"] else { return }
        let appearance = ProcessInfo.processInfo.environment["JARVIS_SHOTS_APPEARANCE"] ?? "light"
        let device = (UIDevice.current.userInterfaceIdiom == .pad ? "ipad" : "iphone")
            + (ProcessInfo.processInfo.environment["JARVIS_ORIENTATION"] == "landscape" ? "-landscape" : "")
        try? screenshot.pngRepresentation
            .write(to: URL(fileURLWithPath: dir).appendingPathComponent("\(device)-\(UITestStyle.name)-\(appearance)-\(name).png"))
    }
}
